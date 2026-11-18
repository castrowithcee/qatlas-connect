package googleroute

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	kindProject   = "project"
	prefixProject = "projects/"
)

var (
	numberPattern  = regexp.MustCompile(`^[0-9]{1,20}$`)
	projectIDShape = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
)

// validProject reports whether value is a project ID or a project number, the two names a project has in the
// name of an operation.
func validProject(value string) bool {
	return numberPattern.MatchString(value) || projectIDShape.MatchString(value)
}

// parseTarget reads one target and returns its project. The error never quotes the value.
func parseTarget(raw string) (string, error) {
	id, ok := strings.CutPrefix(raw, prefixProject)
	if !ok {
		return "", errors.New("a Google Route Optimization target must be projects/PROJECT")
	}
	if !validProject(id) {
		return "", errors.New("a Google Route Optimization target holds a project in a form Google does not use")
	}
	return id, nil
}

// allowlist is the projects of one connection. Without targets the connection is unbound and reaches what its
// credential reaches; the credential stays the upper bound either way. A project ID is never resolved to its
// number: a name matches a target only when both spell the project the same way.
type allowlist []string

func allowlistOf(resolved *config.Resolved) (allowlist, error) {
	return parseAllowlist(provider.TargetsOf(resolved))
}

func parseAllowlist(values []string) (allowlist, error) {
	list := make(allowlist, 0, len(values))
	for _, value := range values {
		project, err := parseTarget(value)
		if err != nil {
			return nil, err
		}
		if slices.Contains(list, project) {
			return nil, errors.New("the Google Route Optimization target list names a project more than once")
		}
		list = append(list, project)
	}
	return list, nil
}

// allows reports whether the connection may read operations of the project.
func (a allowlist) allows(project string) bool {
	return len(a) == 0 || slices.Contains(a, project)
}

var targetMetadata = config.TargetMetadata{
	Label:    "project",
	Multiple: true,
	Description: "optional allow-list of projects/PROJECT, by project ID or project number; a project ID is not " +
		"resolved to its number, so an operation name matches a target only when both spell the project the same " +
		"way; without targets a connection reaches whatever its credential reaches",
	Kinds: []config.TargetKind{{
		Name: kindProject, Description: "one project, by its ID or its number",
		Forms: []string{"projects/PROJECT"},
	}},
	Validate: func(raw string) error {
		_, err := parseTarget(raw)
		return err
	},
	ValidateSet: func(values []string) error {
		_, err := parseAllowlist(values)
		return err
	},
}
