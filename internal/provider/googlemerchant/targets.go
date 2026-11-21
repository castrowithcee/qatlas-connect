package googlemerchant

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Target kinds. An advanced account aggregates sub-accounts and also stands for itself; an account is one
// Merchant Center account, a sub-account of an advanced account or a standalone one.
const (
	kindAdvanced = "advanced-account"
	kindAccount  = "account"

	formAdvanced = "advanced-accounts/ACCOUNT"
	formAccount  = "accounts/ACCOUNT"

	advancedPrefix = "advanced-accounts/"
	accountPrefix  = "accounts/"
)

// An account ID is plain decimal digits without a leading zero, so one account has exactly one spelling and
// a comparison with a target cannot be dodged. Merchant Center IDs are 64-bit integers.
const (
	idPattern = `[1-9][0-9]{0,18}`
	// maxTargets bounds the targets one connection may be bound to.
	maxTargets = 100
)

var idForm = regexp.MustCompile(`^` + idPattern + `$`)

const targetRule = "a Google Merchant target must be accounts/ACCOUNT or advanced-accounts/ACCOUNT with a decimal ID"

// outsideTargets refuses an account outside the binding without naming it.
const outsideTargets = "lies outside the targets of this connection"

// targetKinds are the two optional kinds. Both forms hold the plain account ID, so an account has the same
// target value whichever way it is chosen. Membership of an account in an advanced account is not part of
// the spelling: it is proven by reading the sub-accounts of the advanced account.
var targetKinds = []config.TargetKind{
	{Name: kindAdvanced, Description: "one advanced account: itself, its direct sub-accounts, and the tools that act " +
		"on the advanced account as a whole", Forms: []string{formAdvanced}, Nested: true},
	{Name: kindAccount, Description: "one Merchant Center account: a sub-account of an advanced account or a " +
		"standalone account; the tools of the advanced account itself are not reached through it",
		Forms: []string{formAccount}, Parents: []string{kindAdvanced}, Top: true},
}

// validID reports whether raw is a decimal account ID without a leading zero.
func validID(raw string) bool { return idForm.MatchString(raw) }

func parseTarget(raw string) (kind, id string, err error) {
	if id, ok := strings.CutPrefix(raw, advancedPrefix); ok && validID(id) {
		return kindAdvanced, id, nil
	}
	if id, ok := strings.CutPrefix(raw, accountPrefix); ok && validID(id) {
		return kindAccount, id, nil
	}
	return "", "", errors.New(targetRule)
}

func validateTarget(raw string) error {
	_, _, err := parseTarget(raw)
	return err
}

func validateTargetSet(targets []string) error {
	if len(targets) > maxTargets {
		return errors.New("a Google Merchant connection binds at most 100 targets")
	}
	return nil
}

// scope is the binding of one connection: the advanced accounts and the accounts it names, each ID once and
// sorted. Without either, the connection reaches what the credential reaches.
type scope struct{ advanced, accounts []string }

func (s scope) bound() bool { return len(s.advanced) > 0 || len(s.accounts) > 0 }

// key is the part of a cursor binding that ties it to the targets of the connection.
func (s scope) key() []any { return []any{s.advanced, s.accounts} }

// reach says how an account relates to the binding.
type reach int

const (
	// reachNone lies outside the binding and cannot lie inside it.
	reachNone reach = iota
	// reachNamed is an account the binding names, as itself or as an advanced account.
	reachNamed
	// reachProof is not named; it belongs to the binding only if it is a sub-account of a bound advanced account,
	// which only Google can tell.
	reachProof
)

// reachOf settles an account ID without any request. An account named by the binding is inside, another one
// needs the proof when an advanced account is bound, and is outside otherwise.
func (s scope) reachOf(id string) reach {
	switch {
	case !s.bound(), slices.Contains(s.advanced, id), slices.Contains(s.accounts, id):
		return reachNamed
	case len(s.advanced) > 0:
		return reachProof
	}
	return reachNone
}

// isAdvanced reports whether the binding names the account as an advanced account, which the tools that act
// on the advanced account as a whole require. An unbound connection reaches everything its credential does.
func (s scope) isAdvanced(id string) bool { return !s.bound() || slices.Contains(s.advanced, id) }

// only is the single account of a binding with exactly one target, which a call without an account argument
// uses.
func (s scope) only() (string, bool) {
	switch {
	case len(s.advanced)+len(s.accounts) != 1:
		return "", false
	case len(s.advanced) == 1:
		return s.advanced[0], true
	}
	return s.accounts[0], true
}

// boundScope reads the targets of the connection before any secret is resolved. No error quotes a value.
func boundScope(resolved *config.Resolved) (scope, error) {
	if len(provider.ExcludesOf(resolved)) > 0 {
		return scope{}, provider.Fail("open", "a Google Merchant connection takes no exclusions")
	}
	targets := provider.TargetsOf(resolved)
	if len(targets) > maxTargets {
		return scope{}, provider.Fail("open", "a Google Merchant connection binds at most 100 targets")
	}
	var s scope
	for _, target := range targets {
		kind, id, err := parseTarget(strings.TrimSpace(target))
		switch {
		case err != nil:
			return scope{}, provider.Fail("open", targetRule)
		case kind == kindAdvanced:
			s.advanced = append(s.advanced, id)
		default:
			s.accounts = append(s.accounts, id)
		}
	}
	for _, ids := range []*[]string{&s.advanced, &s.accounts} {
		slices.Sort(*ids)
		*ids = slices.Compact(*ids)
	}
	return s, nil
}

func invalidArgument(message string) error { return &provider.InvalidRequestError{Message: message} }

// accountOf settles the account argument of a call: the argument checked against the ID form and the
// binding, or the one account of a binding with a single target when the argument is left out. Nothing is
// requested; an account that needs the sub-account proof says so.
func (s scope) accountOf(given string) (id string, needsProof bool, err error) {
	if given == "" {
		only, ok := s.only()
		if !ok {
			return "", false, invalidArgument("account is required unless the connection is bound to exactly one target")
		}
		return only, false, nil
	}
	if !validID(given) {
		return "", false, invalidArgument("account must be a decimal Merchant Center account ID")
	}
	switch s.reachOf(given) {
	case reachNone:
		return "", false, invalidArgument("account " + outsideTargets)
	case reachProof:
		return given, true, nil
	}
	return given, false, nil
}

// advancedOf settles the account argument of a tool that acts on an advanced account as a whole: on a bound
// connection it must be an advanced account the binding names, a sub-account or an account target is not
// enough. Without an argument the one advanced account of the binding is used.
func (s scope) advancedOf(given string) (string, error) {
	if given == "" {
		switch {
		case len(s.advanced) == 1:
			return s.advanced[0], nil
		case s.bound():
			return "", invalidArgument("account is required unless the connection is bound to exactly one advanced account")
		}
		return "", invalidArgument("account is required")
	}
	if !validID(given) {
		return "", invalidArgument("account must be a decimal Merchant Center account ID")
	}
	if !s.isAdvanced(given) {
		return "", invalidArgument("account must be an advanced account this connection is bound to")
	}
	return given, nil
}

// reaches withholds the tools that act on an advanced account as a whole from a connection bound without
// one: a sub-account or account target does not bind the advanced account.
func reaches(tool string, targets []string) (bool, []string) {
	if (!advancedOnlyTools(tool) && !strings.HasPrefix(tool, usersToolPrefix)) || len(targets) == 0 {
		return true, nil
	}
	for _, raw := range targets {
		if kind, _, err := parseTarget(raw); err == nil && kind == kindAdvanced {
			return true, nil
		}
	}
	return false, []string{formAdvanced}
}
