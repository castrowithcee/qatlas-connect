package googlebusinessprofile

import (
	"errors"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Forms of the targets and of the path arguments. An ID is plain decimal digits without a leading zero, so
// one object has exactly one spelling and a comparison with the target cannot be dodged. accounts/- is
// Google's name for the accounts of the consenting person; only an unbound connection may list it.
const (
	formAccount  = "accounts/ACCOUNT"
	formLocation = "locations/LOCATION"

	idPattern         = `[1-9][0-9]{0,29}`
	accountPattern    = `^accounts/` + idPattern + `$`
	accountAnyPattern = `^accounts/(` + idPattern + `|-)$`
	locationPattern   = `^locations/` + idPattern + `$`
	ownAccounts       = "accounts/-"
)

var (
	accountForm  = regexp.MustCompile(accountPattern)
	locationForm = regexp.MustCompile(locationPattern)
)

const targetRule = "a Google Business Profile target must be accounts/ACCOUNT or locations/LOCATION with a decimal ID"

const outsideTarget = "lies outside the target of this connection"

// scope is the target of one connection: nothing (the reach of the credential), one account, or one
// location, each as its resource name.
type scope struct{ account, location string }

func (s scope) bound() bool { return s.account != "" || s.location != "" }

// binding is what a cursor is bound to besides the arguments.
func (s scope) binding() []any { return []any{s.account, s.location} }

func parseTarget(raw string) (scope, error) {
	switch {
	case accountForm.MatchString(raw):
		return scope{account: raw}, nil
	case locationForm.MatchString(raw):
		return scope{location: raw}, nil
	}
	return scope{}, errors.New(targetRule)
}

func validateTarget(raw string) error {
	_, err := parseTarget(raw)
	return err
}

// boundScope reads the target of the connection before any secret is resolved. No error quotes a value.
func boundScope(resolved *config.Resolved) (scope, error) {
	targets := provider.TargetsOf(resolved)
	switch {
	case len(targets) == 0:
		return scope{}, nil
	case len(targets) > 1:
		return scope{}, provider.Fail("open", "a Google Business Profile connection takes at most one target")
	}
	bound, err := parseTarget(strings.TrimSpace(targets[0]))
	if err != nil {
		return scope{}, provider.Fail("open", err.Error())
	}
	return bound, nil
}

// accountOf settles the account of a call: the argument, or the account of the target when it is left out.
// A location target names no account, so a call that needs one is refused. own admits accounts/- on an
// unbound connection.
func (s scope) accountOf(name, given string, own bool) (string, error) {
	switch {
	case s.location != "":
		return "", invalidArgument(name + " is not available on a connection bound to a location")
	case given == "" && s.account == "":
		return "", invalidArgument(name + " is required unless the connection is bound to an account")
	case given == "":
		return s.account, nil
	case !accountForm.MatchString(given) && !(own && given == ownAccounts):
		if own {
			return "", invalidArgument(name + " must be accounts/ACCOUNT with a decimal ID, or accounts/-")
		}
		return "", invalidArgument(name + " must be accounts/ACCOUNT with a decimal ID")
	case s.account != "" && given != s.account:
		return "", invalidArgument(name + " " + outsideTarget)
	}
	return given, nil
}

// locationOf checks the location of a call: the argument, or the location of the target when it is left
// out. A location target refuses every other location; an account target proves the location before it is
// read (see handleLocationsGet).
func (s scope) locationOf(given string) (string, error) {
	switch {
	case given == "" && s.location == "":
		return "", invalidArgument("location is required unless the connection is bound to a location")
	case given == "":
		return s.location, nil
	case !locationForm.MatchString(given):
		return "", invalidArgument("location must be locations/LOCATION with a decimal ID")
	case s.location != "" && given != s.location:
		return "", invalidArgument("location " + outsideTarget)
	}
	return given, nil
}

func accountPath(account string) string { return account }

func locationPath(location string) string { return location }

func locationsOfPath(account string) string { return account + "/locations" }

// accountTools are the tools that act on an account and have no meaning for a location target.
var accountTools = map[string]bool{
	accountsListID: true, accountsGetID: true, accountsUpdateID: true,
	accountAdminsListID: true, accountAdminsUpdateID: true, accountAdminsDeleteID: true,
	invitationsListID: true, invitationsDeclineID: true, notificationsGetID: true,
}

// targetReaches withholds the account tools from a connection bound to a location: they act on accounts,
// and a location names none.
func targetReaches(tool string, targets []string) (bool, []string) {
	if !accountTools[tool] {
		return true, nil
	}
	for _, raw := range targets {
		if bound, err := parseTarget(strings.TrimSpace(raw)); err == nil && bound.location != "" {
			return false, []string{formAccount}
		}
	}
	return true, nil
}

var targetMetadata = config.TargetMetadata{
	Label: "target",
	Description: "optional: one account (also a location group) or one location; without a target a connection " +
		"reaches every account and location the consenting account manages",
	Kinds: []config.TargetKind{{
		Name: "account", Description: "one Business Profile account, such as a location group, and its locations",
		Forms: []string{formAccount},
	}, {
		Name: "location", Description: "one business location; the account tools are not available with it",
		Forms: []string{formLocation}, Parents: []string{"account"}, Top: true,
	}},
	Validate: validateTarget,
	Reaches:  targetReaches,
}
