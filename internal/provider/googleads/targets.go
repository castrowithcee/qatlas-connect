package googleads

import (
	"errors"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

const (
	kindManager    = "manager"
	kindCustomer   = "customer"
	prefixManager  = "managers/"
	prefixCustomer = "customers/"
	// maxTargets bounds the targets of one connection, and with it the membership reads of one call.
	maxTargets = 100
)

const targetForm = "a Google Ads target must be managers/MANAGER_ID or customers/CUSTOMER_ID with a ten-digit ID " +
	"without hyphens"

// outsideTargets refuses an account outside the binding without naming it.
const outsideTargets = "customer_id is not available to this connection"

// parseTarget reads one target into its kind and ten-digit ID. The error never quotes the value.
func parseTarget(raw string) (kind, id string, err error) {
	if id, ok := strings.CutPrefix(raw, prefixManager); ok && customerIDPattern.MatchString(id) {
		return kindManager, id, nil
	}
	if id, ok := strings.CutPrefix(raw, prefixCustomer); ok && customerIDPattern.MatchString(id) {
		return kindCustomer, id, nil
	}
	return "", "", errors.New(targetForm)
}

// binding is the boundary of one connection: its manager and customer targets, sorted. Without either the
// connection reaches what its credential reaches. A manager target reaches the manager and every account below
// it; a customer target reaches that one account.
type binding struct {
	managers, customers []string
}

func parseBinding(values []string) (binding, error) {
	if len(values) > maxTargets {
		return binding{}, errors.New("a Google Ads connection binds at most 100 targets")
	}
	var b binding
	seen := map[string]bool{}
	for _, value := range values {
		kind, id, err := parseTarget(value)
		if err != nil {
			return binding{}, err
		}
		if seen[id] {
			return binding{}, errors.New("the Google Ads target list names an account more than once")
		}
		seen[id] = true
		if kind == kindManager {
			b.managers = append(b.managers, id)
		} else {
			b.customers = append(b.customers, id)
		}
	}
	slices.Sort(b.managers)
	slices.Sort(b.customers)
	return b, nil
}

func (b binding) bound() bool { return len(b.managers) > 0 || len(b.customers) > 0 }

// key is the part of a cursor binding that ties it to the targets of the connection.
func (b binding) key() []any { return []any{b.managers, b.customers} }

// reach says how a connection reaches one account.
type reach int

const (
	// reachNone is an account no target names. Below a manager target it may still be reached, which only a
	// membership read can show.
	reachNone reach = iota
	// reachAll is any account of a connection without targets.
	reachAll
	// reachManager is a manager target or an account proved to be below one, with everything below it.
	reachManager
	// reachCustomer is a customer target: the account alone.
	reachCustomer
)

// direct is the reach of an account that needs no request to tell.
func (b binding) direct(id string) reach {
	switch {
	case !b.bound():
		return reachAll
	case slices.Contains(b.managers, id):
		return reachManager
	case slices.Contains(b.customers, id):
		return reachCustomer
	}
	return reachNone
}

// names reports whether a target of either kind names the account.
func (b binding) names(id string) bool {
	return slices.Contains(b.managers, id) || slices.Contains(b.customers, id)
}

var targetMetadata = config.TargetMetadata{
	Label:    "account",
	Multiple: true,
	Description: "optional allow-list of Google Ads accounts by their ten-digit customer ID without hyphens: " +
		"managers/MANAGER_ID for a manager account with every account below it, customers/CUSTOMER_ID for one " +
		"account; an account below a manager target is proved with one read of the manager's hierarchy before a " +
		"tool acts on it; without targets a connection reaches whatever its credential reaches",
	Kinds: []config.TargetKind{{
		Name: kindManager, Nested: true, Forms: []string{"managers/MANAGER_ID"},
		Description: "a manager account with every manager and client account below it",
	}, {
		Name: kindCustomer, Parents: []string{kindManager}, Top: true, Forms: []string{"customers/CUSTOMER_ID"},
		Description: "one account on its own, without the accounts below it",
	}},
	Validate: func(raw string) error {
		_, _, err := parseTarget(raw)
		return err
	},
	ValidateSet: func(values []string) error {
		_, err := parseBinding(values)
		return err
	},
}
