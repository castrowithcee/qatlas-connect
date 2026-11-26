package manage

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
)

// OpenRequest is one open request as a person reads it. Reason and Project are untrusted text of the
// requester, cleaned and shortened: secrets are redacted and control characters are shown escaped.
type OpenRequest struct {
	ID         string
	Kind       accessrequest.Kind
	Connection string
	Provider   string
	// Entries are the requested tools and prefixes with their groups, in the order of the groups of the
	// provider (entries without a group last), then by name.
	Entries     []RequestTool
	Reason      string
	Project     string
	RequestedAt time.Time
	ExpiresAt   time.Time
}

// OpenRequests returns every open request of every connection, newest first. It first removes the expired
// and obsolete requests and logs that, like opening the queue anywhere else. Without a request source, or
// without a configuration file, there are none.
func (s *Service) OpenRequests() ([]OpenRequest, error) {
	if s.requests.Queue == nil {
		return nil, nil
	}
	cfg, _, err := s.store.LoadVersioned()
	if err != nil {
		var missing *config.NotFoundError
		if errors.As(err, &missing) || errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	env := accessrequest.Env{Config: cfg, Tools: s.requests.Tools, Now: s.requestNow()}
	requests, events, err := s.requests.Queue.Open(env)
	if len(events) > 0 {
		recorder := connlog.NewRecorder(s.surface, s.store.Path(), s.vault(), s.processOK)
		s.recordRequestEvents(recorder, events)
	}
	if err != nil {
		return nil, err
	}
	out := make([]OpenRequest, len(requests))
	for i, r := range requests {
		metadata, _ := cfg.ProviderMetadata(r.Provider)
		out[i] = OpenRequest{
			ID: r.ID, Kind: r.Kind, Connection: r.Connection, Provider: r.Provider, Entries: orderedEntries(metadata, r.Tools),
			Reason: s.cleanText(r.Reason, accessrequest.MaxReasonRunes), Project: s.cleanText(r.Project, 0),
			RequestedAt: r.RequestedAt, ExpiresAt: r.ExpiresAt,
		}
	}
	return out, nil
}

// orderedEntries labels the entries with their groups and puts them in the order of the provider's groups.
func orderedEntries(metadata config.ProviderMetadata, tools []string) []RequestTool {
	rank := map[string]int{}
	var all []string
	for _, tool := range metadata.Tools {
		all = append(all, tool.ID)
	}
	if grouping := config.GroupTools(metadata, all, ""); grouping != nil {
		for i, g := range grouping.Groups {
			rank[g.ID] = i
		}
	}
	out := make([]RequestTool, len(tools))
	for i, tool := range tools {
		out[i] = RequestTool{Tool: tool}
		if group, ok := config.GroupOfEntry(metadata, tool); ok {
			out[i].Group, out[i].GroupTitle = group.ID, group.Title
		}
	}
	order := func(t RequestTool) int {
		if t.Group == "" {
			return len(rank) + 1
		}
		return rank[t.Group]
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := order(out[i]), order(out[j]); a != b {
			return a < b
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

// cleanText makes untrusted text of a requester safe to show: known secrets are redacted before the cut,
// invalid UTF-8 and control, format and separator characters become visible escapes, and the text is cut at
// limit runes (0 is 200).
func (s *Service) cleanText(text string, limit int) string {
	if limit <= 0 {
		limit = 200
	}
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s.redact(text), "�") {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp):
			if r > 0xFFFF {
				fmt.Fprintf(&b, `\U%08x`, r)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	clean := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(clean) <= limit {
		return clean
	}
	return string([]rune(clean)[:limit-1]) + "…"
}
