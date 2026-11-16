package googletagmanager

import (
	"errors"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Forms of the targets and of the path arguments. An ID is plain decimal digits without a leading zero, so
// one object has exactly one spelling and a comparison with the target cannot be dodged.
const (
	formAccount   = "accounts/ACCOUNT"
	formContainer = "accounts/ACCOUNT/containers/CONTAINER"

	idPattern        = `[1-9][0-9]{0,19}`
	accountPattern   = `^accounts/` + idPattern + `$`
	containerPattern = `^accounts/` + idPattern + `/containers/` + idPattern + `$`
	workspacePattern = `^accounts/` + idPattern + `/containers/` + idPattern + `/workspaces/` + idPattern + `$`
	maxPathBytes     = 120
)

var (
	accountPath   = regexp.MustCompile(accountPattern)
	containerPath = regexp.MustCompile(containerPattern)
	workspacePath = regexp.MustCompile(workspacePattern)
)

const targetRule = "a Google Tag Manager target must be accounts/ACCOUNT or accounts/ACCOUNT/containers/CONTAINER " +
	"with decimal IDs"

const outsideTarget = "lies outside the target of this connection"

// gtmPath is an account, container, or workspace path split into its IDs; the deeper IDs of a shallower path
// are empty.
type gtmPath struct{ account, container, workspace string }

func (p gtmPath) String() string {
	out := "accounts/" + p.account
	if p.container != "" {
		out += "/containers/" + p.container
	}
	if p.workspace != "" {
		out += "/workspaces/" + p.workspace
	}
	return out
}

// parsePath reads raw as a path of exactly the form pattern describes.
func parsePath(raw string, pattern *regexp.Regexp) (gtmPath, bool) {
	if len(raw) > maxPathBytes || !pattern.MatchString(raw) {
		return gtmPath{}, false
	}
	parts := strings.Split(raw, "/")
	p := gtmPath{account: parts[1]}
	if len(parts) > 3 {
		p.container = parts[3]
	}
	if len(parts) > 5 {
		p.workspace = parts[5]
	}
	return p, true
}

// scope is the target of one connection: nothing (the reach of the credential), one account, or one container.
type scope struct{ account, container string }

func (s scope) bound() bool { return s.account != "" }

// allows reports whether p lies inside the target. A container binding allows the path of its own account,
// so the account it sits in can be read, and nothing else of that account.
func (s scope) allows(p gtmPath) bool {
	switch {
	case !s.bound():
		return true
	case p.account != s.account:
		return false
	case s.container == "":
		return true
	}
	return p.container == "" || p.container == s.container
}

func (s scope) binding() []any { return []any{s.account, s.container} }

func parseTarget(raw string) (scope, error) {
	if p, ok := parsePath(raw, accountPath); ok {
		return scope{account: p.account}, nil
	}
	if p, ok := parsePath(raw, containerPath); ok {
		return scope{account: p.account, container: p.container}, nil
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
		return scope{}, provider.Fail("open", "a Google Tag Manager connection takes at most one target")
	}
	bound, err := parseTarget(strings.TrimSpace(targets[0]))
	if err != nil {
		return scope{}, provider.Fail("open", err.Error())
	}
	return bound, nil
}

func invalidArgument(message string) error { return &provider.InvalidRequestError{Message: message} }

// accountOf settles the account of a call: the argument, or the account of the target when it is left out.
func (s scope) accountOf(given string) (gtmPath, error) {
	if given == "" {
		if !s.bound() {
			return gtmPath{}, invalidArgument("account is required unless the connection is bound to an account or a container")
		}
		return gtmPath{account: s.account}, nil
	}
	p, ok := parsePath(given, accountPath)
	if !ok {
		return gtmPath{}, invalidArgument("account must be accounts/ACCOUNT with a decimal ID")
	}
	if !s.allows(p) {
		return gtmPath{}, invalidArgument("account " + outsideTarget)
	}
	return p, nil
}

// containerOf settles the container of a call: the argument, or the container of a container target.
func (s scope) containerOf(given string) (gtmPath, error) {
	if given == "" {
		if s.container == "" {
			return gtmPath{}, invalidArgument("container is required unless the connection is bound to a container")
		}
		return gtmPath{account: s.account, container: s.container}, nil
	}
	p, ok := parsePath(given, containerPath)
	if !ok {
		return gtmPath{}, invalidArgument("container must be accounts/ACCOUNT/containers/CONTAINER with decimal IDs")
	}
	if !s.allows(p) {
		return gtmPath{}, invalidArgument("container " + outsideTarget)
	}
	return p, nil
}

func (s scope) workspaceOf(given string) (gtmPath, error) {
	p, ok := parsePath(given, workspacePath)
	if !ok {
		return gtmPath{}, invalidArgument("workspace must be accounts/ACCOUNT/containers/CONTAINER/workspaces/WORKSPACE " +
			"with decimal IDs")
	}
	if !s.allows(p) {
		return gtmPath{}, invalidArgument("workspace " + outsideTarget)
	}
	return p, nil
}
