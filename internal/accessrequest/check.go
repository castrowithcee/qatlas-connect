package accessrequest

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// Tool is one registered tool with the provider it belongs to.
type Tool struct {
	config.ToolMetadata
	Provider string
}

// Env is what judging and pruning need: the whole configuration (not a project's view, so a request of a
// connection bound to another project is kept), every registered tool, and the time.
type Env struct {
	Config *config.Config
	Tools  []Tool
	Now    time.Time
}

// InvalidError reports a request that cannot be stored. It names the tools and why, never the reason text.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

// BlockedError reports tools a person blocked on the connection: asking again is pointless.
type BlockedError struct {
	Connection string
	Tools      []string
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("the user blocked requests for %s on connection %q; do not request them again, "+
		"and do not ask the user for them unless the user brings them up", strings.Join(quoteAll(e.Tools), ", "), e.Connection)
}

func quoteAll(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprintf("%q", shorten(v))
	}
	return out
}

// shorten bounds an agent-written tool string echoed in a message.
func shorten(s string) string {
	if r := []rune(s); len(r) > 80 {
		return string(r[:79]) + "…"
	}
	return s
}

// ProviderOf returns the provider a tool ID or prefix names: the provider of a registered ID, or for a
// prefix the longest registered provider whose ID, plus a dot, starts it.
func ProviderOf(tools []Tool, entry string) (string, bool) {
	if config.IsToolPrefix(entry) {
		best := ""
		for _, tool := range tools {
			if len(tool.Provider) > len(best) && strings.HasPrefix(entry, tool.Provider+".") {
				best = tool.Provider
			}
		}
		return best, best != ""
	}
	for _, tool := range tools {
		if tool.ID == entry {
			return tool.Provider, true
		}
	}
	return "", false
}

type state int

const (
	stateRequestable state = iota
	stateOffered
	stateBlocked
	stateRefused
)

// verdict is the state of one tool on a connection and, where it is refused, the stable reason.
type verdict struct {
	state  state
	reason config.Refusal
}

// judge classifies a registered tool for a request on the connection name. A tool is requestable when the
// connection does not offer it for a reason a person can change by deciding the request (its tools list,
// its effects, or the allow-list) and nothing else would still keep it from being offered afterwards.
func (e Env) judge(clone func() *config.Config, name string, tool config.ToolMetadata) verdict {
	cfg := e.Config
	switch refusal := cfg.ConnectionRefusal(name, tool, e.Now); {
	case refusal == "" || refusal == config.RefusalDisabled:
		return verdict{state: stateOffered}
	case cfg.BlocksTool(name, tool.ID):
		return verdict{state: stateBlocked}
	case refusal != config.RefusalToolsList && refusal != config.RefusalEffect && refusal != config.RefusalToolAllowList:
		return verdict{state: stateRefused, reason: refusal}
	}
	// Judge the connection as if its tools list named this tool: what still refuses it would outlast a decision.
	probe := clone()
	conn := probe.Connections[name]
	conn.Tools = []string{tool.ID}
	probe.Connections[name] = conn
	if after := probe.ConnectionRefusal(name, tool, e.Now); after != "" && after != config.RefusalDisabled {
		return verdict{state: stateRefused, reason: after}
	}
	return verdict{state: stateRequestable}
}

var reasonText = map[config.Refusal]string{
	config.RefusalToolsList:       "the connection offers selected tools and this is not among them",
	config.RefusalEffect:          "the connection does not allow the effect of this tool",
	config.RefusalToolAllowList:   "the tool needs the allow-list",
	config.RefusalNoLocalFiles:    "the connection releases no directory for this tool's local files",
	config.RefusalOtherProvider:   "the tool belongs to another provider than the connection",
	config.RefusalOutsideTargets:  "the connection's targets do not reach this tool",
	config.RefusalTargetsOutdated: "the connection's targets are outdated",
	config.RefusalNoConnection:    "the connection does not exist",
	config.RefusalDisabled:        "the connection is disabled",
}

// Check judges the tools of a request for the connection name, all or nothing, and returns them without
// duplicates, sorted. A blocked tool is reported as *BlockedError, anything else that cannot be requested
// as *InvalidError naming the tool and the reason.
func Check(env Env, name string, entries []string) ([]string, error) {
	if len(entries) == 0 {
		return nil, &InvalidError{"a request names at least one tool"}
	}
	if len(entries) > MaxTools {
		return nil, &InvalidError{fmt.Sprintf("a request names at most %d tools, not %d", MaxTools, len(entries))}
	}
	conn, ok := env.Config.Connections[name]
	if !ok {
		return nil, &InvalidError{fmt.Sprintf("connection %q is not configured", name)}
	}
	provider := env.Config.Services[conn.Service].Provider
	own := map[string]config.ToolMetadata{}
	var ownList []config.ToolMetadata
	for _, tool := range env.Tools {
		if tool.Provider == provider {
			own[tool.ID] = tool.ToolMetadata
			ownList = append(ownList, tool.ToolMetadata)
		}
	}
	var cloned *config.Config
	clone := func() *config.Config {
		if cloned == nil {
			cloned = env.Config.Clone()
		}
		return cloned
	}

	var blocked, problems, unique []string
	for _, entry := range entries {
		if slices.Contains(unique, entry) {
			continue
		}
		unique = append(unique, entry)
		var tools []config.ToolMetadata
		var covered, skipped int
		if config.IsToolPrefix(entry) {
			stem, valid := config.ToolPrefixStem(provider, entry)
			if !valid {
				if owner, known := ProviderOf(env.Tools, entry); known && owner != provider {
					problems = append(problems, fmt.Sprintf("tool %q: %s, %s", shorten(entry), config.RefusalOtherProvider, reasonText[config.RefusalOtherProvider]))
				} else {
					problems = append(problems, fmt.Sprintf("%q is not a valid tool prefix; a prefix is <provider>.<segment>.*", shorten(entry)))
				}
				continue
			}
			for _, tool := range ownList {
				if !strings.HasPrefix(tool.ID, stem) {
					continue
				}
				covered++
				if tool.RequiresToolAllowList {
					skipped++
					continue
				}
				tools = append(tools, tool)
			}
			if covered == 0 {
				problems = append(problems, fmt.Sprintf("prefix %q matches no tool of the connection's provider", shorten(entry)))
				continue
			}
		} else {
			tool, known := own[entry]
			if !known {
				if owner, other := ProviderOf(env.Tools, entry); other && owner != provider {
					problems = append(problems, fmt.Sprintf("tool %q: %s, %s", shorten(entry), config.RefusalOtherProvider, reasonText[config.RefusalOtherProvider]))
				} else {
					problems = append(problems, fmt.Sprintf("tool %q is not a registered tool", shorten(entry)))
				}
				continue
			}
			tools = []config.ToolMetadata{tool}
		}

		counts := map[state]int{}
		var first verdict
		for _, tool := range tools {
			v := env.judge(clone, name, tool)
			counts[v.state]++
			if v.state == stateRefused && first.reason == "" {
				first = v
			}
		}
		switch {
		case counts[stateRequestable] > 0:
		case counts[stateBlocked] > 0:
			blocked = append(blocked, entry)
		case first.reason != "":
			problems = append(problems, fmt.Sprintf("tool %q: %s, %s", shorten(entry), first.reason, reasonText[first.reason])+allowListNote(entry, skipped))
		case counts[stateOffered] > 0:
			problems = append(problems, fmt.Sprintf("tool %q is already offered by connection %q", shorten(entry), name)+allowListNote(entry, skipped))
		default:
			problems = append(problems, fmt.Sprintf("prefix %q covers no tool a request can name", shorten(entry))+allowListNote(entry, skipped))
		}
	}
	if len(blocked) > 0 {
		return nil, &BlockedError{Connection: name, Tools: blocked}
	}
	if len(problems) > 0 {
		more := ""
		if len(problems) > 5 {
			more = fmt.Sprintf("; and %d more", len(problems)-5)
			problems = problems[:5]
		}
		return nil, &InvalidError{fmt.Sprintf("nothing was requested: %s%s", strings.Join(problems, "; "), more)}
	}
	sort.Strings(unique)
	return unique, nil
}

// allowListNote says that a prefix does not cover the tools that name themselves in the allow-list.
func allowListNote(entry string, skipped int) string {
	if skipped == 0 || !config.IsToolPrefix(entry) {
		return ""
	}
	return fmt.Sprintf(" (a prefix does not cover the %d tool(s) below it that require the allow-list; request those by their exact ID)", skipped)
}

// alive reports whether a stored entry is still worth waiting for: a registered tool, or a prefix over one,
// that the connection neither offers nor blocks now.
func (e Env) alive(name, provider, entry string) bool {
	stem := ""
	if config.IsToolPrefix(entry) {
		var ok bool
		if stem, ok = config.ToolPrefixStem(provider, entry); !ok {
			return false
		}
	}
	for _, tool := range e.Tools {
		if tool.Provider != provider {
			continue
		}
		if stem == "" && tool.ID != entry || stem != "" && (tool.RequiresToolAllowList || !strings.HasPrefix(tool.ID, stem)) {
			continue
		}
		// Only what a person's decision on the list, the effects, or the allow-list can still offer is alive.
		switch e.Config.ConnectionRefusal(name, tool.ToolMetadata, e.Now) {
		case config.RefusalToolsList, config.RefusalEffect, config.RefusalToolAllowList, config.RefusalTargetsOutdated:
			if !e.Config.BlocksTool(name, tool.ID) {
				return true
			}
		}
	}
	return false
}
