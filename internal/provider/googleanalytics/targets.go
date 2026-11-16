package googleanalytics

import (
	"errors"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Resource names of the Admin API: a collection and a numeric ID without a leading zero, so one resource has
// exactly one spelling and a comparison of names is a comparison of resources.
const (
	accountPrefix  = "accounts/"
	propertyPrefix = "properties/"
	// maxIDDigits bounds an ID; Analytics IDs are 64-bit integers.
	maxIDDigits = 19
	// maxTargets bounds the targets one connection may be bound to.
	maxTargets = 100
)

const (
	kindAccount  = "account"
	kindProperty = "property"
)

// targetKinds are the two optional target kinds. A property also stands on its own, since a credential may
// see a property without its account.
var targetKinds = []config.TargetKind{
	{Name: kindAccount, Description: "an Analytics account with all its properties", Forms: []string{"accounts/ID"}},
	{Name: kindProperty, Description: "one Analytics property", Forms: []string{"properties/ID"},
		Parents: []string{kindAccount}, Top: true},
}

const targetForm = "a Google Analytics target must be accounts/ID or properties/ID with a numeric ID"

// outsideTargets refuses a resource outside the binding without naming it.
const outsideTargets = "the resource is not available to this connection"

// validID reports whether id is a positive decimal number without a leading zero.
func validID(id string) bool {
	if id == "" || len(id) > maxIDDigits || id[0] == '0' {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

// isName reports whether value is the resource name prefix followed by a valid ID.
func isName(prefix, value string) bool {
	id, ok := strings.CutPrefix(value, prefix)
	return ok && validID(id)
}

// checkAccountName checks an argument that names an account. The refusal names the argument, never the value.
func checkAccountName(argument, value string) (string, error) {
	if !isName(accountPrefix, value) {
		return "", badRequest(argument + " must be accounts/ followed by the numeric account ID")
	}
	return value, nil
}

// checkPropertyName checks an argument that names a property. The refusal names the argument, never the value.
func checkPropertyName(argument, value string) (string, error) {
	if !isName(propertyPrefix, value) {
		return "", badRequest(argument + " must be properties/ followed by the numeric property ID")
	}
	return value, nil
}

// validateTarget checks the form of one configured target without quoting it.
func validateTarget(target string) error {
	if isName(accountPrefix, target) || isName(propertyPrefix, target) {
		return nil
	}
	return errors.New(targetForm)
}

// binding is the boundary of one connection: the bound accounts and properties, sorted and each once. Without
// either, the connection reaches what the credential reaches.
type binding struct {
	accounts, properties []string
}

// bindingOf checks the origin and the targets of a connection and returns its binding. The messages never
// quote a value.
func bindingOf(resolved *config.Resolved) (binding, error) {
	if resolved == nil {
		return binding{}, failure("open", "no connection was selected")
	}
	if !isDefaultRoot(resolved.BaseURL) {
		return binding{}, failure("open", "a Google Analytics service has a fixed origin; leave the base URL as "+adminRoot)
	}
	if len(provider.ExcludesOf(resolved)) > 0 {
		return binding{}, failure("open", "a Google Analytics connection takes no exclusions")
	}
	targets := provider.TargetsOf(resolved)
	if len(targets) > maxTargets {
		return binding{}, failure("open", "a Google Analytics connection binds at most 100 targets")
	}
	var b binding
	for _, target := range targets {
		switch {
		case isName(accountPrefix, target):
			b.accounts = append(b.accounts, target)
		case isName(propertyPrefix, target):
			b.properties = append(b.properties, target)
		default:
			return binding{}, failure("open", targetForm)
		}
	}
	slices.Sort(b.accounts)
	slices.Sort(b.properties)
	b.accounts, b.properties = slices.Compact(b.accounts), slices.Compact(b.properties)
	return b, nil
}

// bound reports whether the connection names any target.
func (b binding) bound() bool { return len(b.accounts) > 0 || len(b.properties) > 0 }

// key is the part of a cursor binding that ties it to the targets of the connection.
func (b binding) key() []any { return []any{b.accounts, b.properties} }

// account checks before any request that a tool may act on the account name names: an account-wide tool runs
// only on a connection without targets or one that binds the account itself.
func (b binding) account(name string) error {
	if !b.bound() || slices.Contains(b.accounts, name) {
		return nil
	}
	return badRequest(outsideTargets)
}

// property checks before any request whether a tool may act on the property name names. A bound property and
// any property of an unbound connection pass. A property under a bound account passes only after one proof
// read shows its account (proof true); without bound accounts it is refused at once.
func (b binding) property(name string) (proof bool, err error) {
	switch {
	case !b.bound(), slices.Contains(b.properties, name):
		return false, nil
	case len(b.accounts) > 0:
		return true, nil
	}
	return false, badRequest(outsideTargets)
}

// ownsProperty reports whether a property read as proof belongs to a bound account. Its parent names the
// account; a property below another property (a subproperty) names its account in account.
func (b binding) ownsProperty(p property) bool {
	owner := string(p.Parent)
	if !isName(accountPrefix, owner) {
		owner = string(p.Account)
	}
	return isName(accountPrefix, owner) && slices.Contains(b.accounts, owner)
}

// summaries keeps of a page of account summaries what the binding reaches, as the API shows it to a user who
// may see exactly these resources: a bound account with all its properties, otherwise an account only with
// its bound properties, and nothing of an account without one. A summary whose names are not in their
// documented form cannot be matched and is dropped.
func (b binding) summaries(list []accountSummary) []accountSummary {
	if !b.bound() {
		return list
	}
	kept := []accountSummary{}
	for _, summary := range list {
		if slices.Contains(b.accounts, string(summary.Account)) {
			kept = append(kept, summary)
			continue
		}
		var properties []propertySummary
		for _, p := range summary.PropertySummaries {
			if slices.Contains(b.properties, string(p.Property)) {
				properties = append(properties, p)
			}
		}
		if len(properties) > 0 {
			summary.PropertySummaries = properties
			kept = append(kept, summary)
		}
	}
	return kept
}
