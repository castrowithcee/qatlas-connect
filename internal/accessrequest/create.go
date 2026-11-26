package accessrequest

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// CreateInput is what a request to create a connection names. It has no field for a credential, a service,
// or any secret: a person chooses those when setting the connection up.
type CreateInput struct {
	Provider string
	Name     string
	Targets  []string
	Tools    []string
	// Reason is judged for a known secret only; it is never stored from here.
	Reason string
}

// CheckCreate judges a request to create a connection, all or nothing, and returns it without duplicates,
// tools and targets sorted. The provider must be registered, the name valid, short, and unused by any
// connection of the whole configuration, the tools registered tools of the provider (or prefixes covering at
// least one), the targets valid for the provider, and every tool reached by them. A value that the redactor
// of Env would change fails the whole request. Every problem is an *InvalidError that never quotes a value
// unbounded.
func CheckCreate(env Env, in CreateInput) (CreateInput, error) {
	if !env.registered(in.Provider) {
		return CreateInput{}, &InvalidError{"the provider is not registered; name a provider of the tools"}
	}
	meta, _ := env.Config.ProviderMetadata(in.Provider)
	if env.Redact != nil {
		values := append([]string{in.Name, in.Reason}, append(slices.Clone(in.Targets), in.Tools...)...)
		for _, v := range values {
			if env.Redact(v) != v {
				return CreateInput{}, &InvalidError{"a value of the request matches a known secret; a request never carries one"}
			}
		}
	}
	var problems []string
	switch name := in.Name; {
	case utf8.RuneCountInString(name) > MaxNameRunes:
		problems = append(problems, fmt.Sprintf("the name is longer than %d characters", MaxNameRunes))
	case config.ValidateName(name) != nil:
		problems = append(problems, "name "+config.ValidateName(name).Error())
	default:
		if _, taken := env.Config.Connections[name]; taken {
			problems = append(problems, fmt.Sprintf("a connection named %q exists; choose another name", name))
		}
	}

	targets, targetProblems := checkTargets(meta.Target, in.Provider, in.Targets)
	problems = append(problems, targetProblems...)

	if len(in.Tools) == 0 {
		problems = append(problems, "a request names at least one tool")
	}
	if len(in.Tools) > MaxTools {
		return CreateInput{}, &InvalidError{fmt.Sprintf("a request names at most %d tools, not %d", MaxTools, len(in.Tools))}
	}
	own := ownTools{provider: in.Provider, byID: map[string]config.ToolMetadata{}}
	for _, tool := range env.Tools {
		if tool.Provider == in.Provider {
			own.byID[tool.ID] = tool.ToolMetadata
			own.list = append(own.list, tool.ToolMetadata)
		}
	}
	var unique []string
	for _, entry := range in.Tools {
		if slices.Contains(unique, entry) {
			continue
		}
		unique = append(unique, entry)
		covered, skipped, problem := own.resolve(env, entry)
		if problem == "" && len(covered) == 0 {
			problem = fmt.Sprintf("prefix %q covers no tool a request can name", shorten(entry)) + allowListNote(entry, skipped)
		}
		if problem != "" {
			problems = append(problems, problem)
			continue
		}
		if len(targetProblems) > 0 {
			continue
		}
		// An exact tool must be reached; a prefix needs one tool the targets reach.
		var hint string
		reached := false
		for _, tool := range covered {
			ok, why := meta.Target.Reach(tool.ID, targets)
			reached, hint = reached || ok, why
		}
		if !reached {
			problem := fmt.Sprintf("tool %q: %s, %s", shorten(entry), config.RefusalOutsideTargets, reasonText[config.RefusalOutsideTargets])
			if hint != "" {
				problem += " (" + hint + ")"
			}
			problems = append(problems, problem)
		}
	}
	if _, err := finish(problems, nil); err != nil {
		return CreateInput{}, err
	}
	sort.Strings(unique)
	return CreateInput{Provider: in.Provider, Name: in.Name, Targets: targets, Tools: unique, Reason: in.Reason}, nil
}

// acceptsTargets reports whether the provider describes any target a connection can bind to.
func acceptsTargets(t config.TargetMetadata) bool {
	return t.Label != "" || len(t.Kinds) > 0 || t.Validate != nil || t.ValidateSet != nil || t.Reaches != nil
}

// checkTargets judges the requested targets by the provider's metadata, like the configuration judges the
// targets of a connection, and returns them without duplicates, sorted.
func checkTargets(t config.TargetMetadata, provider string, targets []string) ([]string, []string) {
	if len(targets) == 0 {
		return nil, nil
	}
	if !acceptsTargets(t) {
		return nil, []string{fmt.Sprintf("provider %q accepts no targets", provider)}
	}
	if len(targets) > MaxTargets {
		return nil, []string{fmt.Sprintf("a request names at most %d targets, not %d", MaxTargets, len(targets))}
	}
	var problems, unique []string
	for _, target := range targets {
		if slices.Contains(unique, target) {
			continue
		}
		unique = append(unique, target)
		var err error
		switch {
		case target == "" || strings.TrimSpace(target) != target:
			err = fmt.Errorf("a target must not be empty or padded with space")
		case utf8.RuneCountInString(target) > MaxTargetRunes:
			err = fmt.Errorf("a target is longer than %d characters", MaxTargetRunes)
		case !validTarget(t, target):
			err = fmt.Errorf("target %q is not valid for provider %q", shorten(target), provider)
		}
		if err != nil {
			problems = append(problems, err.Error())
		}
	}
	switch {
	case len(problems) > 0:
	case len(unique) > 1 && !t.Multiple:
		problems = append(problems, fmt.Sprintf("provider %q accepts only one target", provider))
	case t.Wildcard != "" && len(unique) > 1 && slices.Contains(unique, t.Wildcard):
		problems = append(problems, fmt.Sprintf("wildcard %q must be the only target", t.Wildcard))
	case t.ValidateSet != nil && t.ValidateSet(unique) != nil:
		problems = append(problems, fmt.Sprintf("the targets are not valid together for provider %q", provider))
	}
	sort.Strings(unique)
	return unique, problems
}

// validTarget is the form check of one target: a target kind spells it, or, for a provider without kinds, its
// own Validate accepts it.
func validTarget(t config.TargetMetadata, target string) bool {
	if len(t.Kinds) == 0 {
		return t.Validate == nil || t.Validate(target) == nil
	}
	return slices.ContainsFunc(t.Kinds, func(kind config.TargetKind) bool { return t.ValidTarget(kind.Name, target) })
}

// registered reports whether a registered tool belongs to the provider.
func (e Env) registered(provider string) bool {
	return provider != "" && slices.ContainsFunc(e.Tools, func(t Tool) bool { return t.Provider == provider })
}
