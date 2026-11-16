package github

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// fakeIssueType is one issue type of the organization octo-org declares.
type fakeIssueType struct {
	id                       int64
	name, description, color string
	enabled                  bool
}

// github.issue-types.list reads every issue type of an organization an explicit connection allows: an
// explicit owner target, a repository target of that organization, or a connection without targets allow
// the call; the result names the organization it read, the way every owner tool does.
func TestIssueTypesListSatisfiesItsContractThroughTheApplicationCore(t *testing.T) {
	f := &fakeGitHub{issueTypes: []fakeIssueType{
		{id: 1, name: "Bug", description: "Something broken", color: "red", enabled: true},
		{id: 2, name: "Task", enabled: true},
	}}
	base := serve(t, f)
	red := &redact.Redactor{}
	core := application.New(registry(t), discoveryConfig(base), resolver(red, nil), red)

	// An owner target of the connection is the default.
	result, err := invoke(t, core, issueTypesList.ID, "org", `{}`, false)
	if err != nil || !strings.Contains(string(result), `"id":1`) || !strings.Contains(string(result), `"name":"Bug"`) ||
		!strings.Contains(string(result), `"description":"Something broken"`) ||
		!strings.Contains(string(result), `"color":"red"`) || !strings.Contains(string(result), `"is_enabled":true`) ||
		!strings.Contains(string(result), `"name":"Task"`) || !strings.Contains(string(result), `"owner":"orgs/octo-org"`) {
		t.Fatalf("%s = %s, %v", issueTypesList.ID, result, err)
	}

	// A connection without targets allows any organization named explicitly.
	result, err = invoke(t, core, issueTypesList.ID, "open", `{"owner":"orgs/octo-org"}`, false)
	if err != nil || !strings.Contains(string(result), `"name":"Bug"`) {
		t.Fatalf("open connection = %s, %v", result, err)
	}

	// A repository target of the organization allows the call as well, the way it allows
	// github.repositories.list to list the owner's repositories.
	result, err = invoke(t, core, issueTypesList.ID, "starrepo", `{"owner":"orgs/octo-org"}`, false)
	if err != nil || !strings.Contains(string(result), `"name":"Bug"`) {
		t.Fatalf("repository-target connection = %s, %v", result, err)
	}
}

// Every refusal of the owner or its scope settles before a secret is read and before GitHub is contacted; a
// project-only connection refuses the call, the engere Auslegung of the target rule.
func TestIssueTypesListRefusalsBeforeIO(t *testing.T) {
	f := &fakeGitHub{}
	reads := 0
	red := &redact.Redactor{}
	core := application.New(registry(t), discoveryConfig(serve(t, f)), resolver(red, &reads), red)

	for _, tt := range []struct{ name, connection, arguments, message string }{
		{"no owner and none the targets name exactly", "open", `{}`, "owner is required"},
		{"a user owner", "user", `{}`, "for a repository of a user"},
		{"a user owner argument on an open connection", "open", `{"owner":"users/octocat"}`,
			"for a repository of a user"},
		{"an owner outside the targets", "org", `{"owner":"orgs/other"}`, "outside the targets"},
		{"a project-only connection", "planning", `{"owner":"orgs/octo-org"}`, "outside the targets"},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, issueTypesList.ID, tt.connection, tt.arguments, false)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), tt.message) {
			t.Errorf("%s: err = %v, want an invalid request naming %q", tt.name, err, tt.message)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// A token without read:org is refused with a message naming what reading the issue types of an organization
// needs.
func TestIssueTypesListReportsThePermissionItNeeds(t *testing.T) {
	f := &fakeGitHub{issueTypesForbidden: true}
	base := serve(t, f)
	red := &redact.Redactor{}
	core := application.New(registry(t), discoveryConfig(base), resolver(red, nil), red)

	_, err := invoke(t, core, issueTypesList.ID, "org", `{}`, false)
	if classOf(err) != provider.ClassPermission || !strings.Contains(err.Error(), "read:org") {
		t.Errorf("%s = %v, want a permission refusal naming read:org", issueTypesList.ID, err)
	}
}

// github.issue-types.list reports every member GitHub documents for an issue type; a type without the new
// members keeps its earlier output exactly. The result is encoded with sorted keys.
func TestIssueTypesListReportsEveryDocumentedMember(t *testing.T) {
	const documented = `[{"id":410,"node_id":"IT_kwDNAd3NAZo","name":"Task","description":"A specific piece of work",` +
		`"created_at":"2024-12-11T14:39:09Z","updated_at":"2024-12-11T14:40:00Z","color":"blue","is_enabled":true}]`
	const want = `{"issue_types":[{"color":"blue","created_at":"2024-12-11T14:39:09Z",` +
		`"description":"A specific piece of work","id":410,"is_enabled":true,"name":"Task",` +
		`"node_id":"IT_kwDNAd3NAZo","updated_at":"2024-12-11T14:40:00Z"}],"owner":"orgs/octo-org"}`
	const oldBody = `[{"id":410,"name":"Task","description":null,"color":"blue","is_enabled":true}]`
	const oldWant = `{"issue_types":[{"color":"blue","id":410,"is_enabled":true,"name":"Task"}],"owner":"orgs/octo-org"}`
	red := &redact.Redactor{}
	for _, c := range []struct{ body, want string }{{documented, want}, {oldBody, oldWant}} {
		base := serveRoute(t, "/api/v3/orgs/octo-org/issue-types", c.body)
		core := application.New(registry(t), discoveryConfig(base), resolver(red, nil), red)
		got, err := invoke(t, core, issueTypesList.ID, "org", `{}`, false)
		if err != nil || string(got) != c.want {
			t.Errorf("issue-types.list = %s, %v, want %s", got, err, c.want)
		}
	}
}

// repositoryIssueTypesPage answers github.issue-types.list's GraphQL query of a repository: the types octo-org
// declares, as GraphQL names them, for any repository except the one a test names missing.
func (f *fakeGitHub) repositoryIssueTypesPage(w http.ResponseWriter, document string, variables map[string]any) bool {
	if !strings.Contains(document, "issueTypes(first") {
		return false
	}
	if variables["name"] == "missing" {
		fmt.Fprint(w, `{"data":{"repository":null}}`)
		return true
	}
	nodes := []string{}
	for _, it := range f.issueTypes {
		description := "null"
		if it.description != "" {
			description = fmt.Sprintf("%q", it.description)
		}
		nodes = append(nodes, fmt.Sprintf(`{"id":"IT_%d","name":%q,"description":%s,"color":%q,"isEnabled":%t}`,
			it.id, it.name, description, strings.ToUpper(it.color), it.enabled))
	}
	fmt.Fprintf(w, `{"data":{"repository":{"issueTypes":{"pageInfo":{"hasNextPage":false,"endCursor":""},`+
		`"nodes":[%s]}}}}`, strings.Join(nodes, ","))
	return true
}

// A repository of a user account lists its issue types through GraphQL, with the members GraphQL documents:
// no numeric id and no timestamps, the color in lower case, and the repository the call read.
func TestIssueTypesListReadsTheRepositoryOfAUser(t *testing.T) {
	f := &fakeGitHub{issueTypes: []fakeIssueType{
		{id: 1, name: "Bug", description: "Something broken", color: "red", enabled: true},
		{id: 2, name: "Task", enabled: false},
	}}
	red := &redact.Redactor{}
	core := application.New(registry(t), discoveryConfig(serve(t, f)), resolver(red, nil), red)

	const want = `{"issue_types":[{"color":"red","description":"Something broken","is_enabled":true,` +
		`"name":"Bug","node_id":"IT_1"},{"is_enabled":false,"name":"Task","node_id":"IT_2"}],` +
		`"repository":"octocat/example"}`
	got, err := invoke(t, core, issueTypesList.ID, "open", `{"repository":"octocat/example"}`, false)
	if err != nil || string(got) != want {
		t.Errorf("repository read = %s, %v, want %s", got, err, want)
	}
	// A repository target of the connection allows the call.
	got, err = invoke(t, core, issueTypesList.ID, "starrepo", `{"repository":"octo-org/example"}`, false)
	if err != nil || !strings.Contains(string(got), `"repository":"octo-org/example"`) {
		t.Errorf("starrepo repository read = %s, %v", got, err)
	}
	for _, request := range f.recorded() {
		if request.path != "/api/graphql" {
			t.Errorf("a repository read called %s, want only GraphQL", request.path)
		}
	}

	if _, err := invoke(t, core, issueTypesList.ID, "open", `{"repository":"octocat/missing"}`, false); classOf(err) != provider.ClassNotFound {
		t.Errorf("a missing repository = %v, want not-found", err)
	}
}

// The owner call of an organization keeps its REST route and its result, and owner and repository exclude
// each other; a repository outside the targets settles before a secret is read or GitHub is contacted.
func TestIssueTypesListKeepsOrganizationsAndRefusesRepositoriesBeforeIO(t *testing.T) {
	f := &fakeGitHub{issueTypes: []fakeIssueType{{id: 1, name: "Bug", enabled: true}}}
	reads := 0
	red := &redact.Redactor{}
	core := application.New(registry(t), discoveryConfig(serve(t, f)), resolver(red, &reads), red)

	got, err := invoke(t, core, issueTypesList.ID, "open", `{"owner":"orgs/octo-org"}`, false)
	if err != nil || !strings.Contains(string(got), `"id":1`) || !strings.Contains(string(got), `"owner":"orgs/octo-org"`) ||
		strings.Contains(string(got), `"repository"`) {
		t.Fatalf("organization read = %s, %v", got, err)
	}
	for _, request := range f.recorded() {
		if request.path != "/api/v3/orgs/octo-org/issue-types" {
			t.Errorf("an organization read called %s, want only the REST route", request.path)
		}
	}

	for _, tt := range []struct{ name, connection, arguments, message string }{
		{"owner and repository", "open", `{"owner":"orgs/octo-org","repository":"octocat/example"}`, "never both"},
		{"a repository outside the targets", "starrepo", `{"repository":"octocat/example"}`, "outside the targets"},
		{"a malformed repository", "open", `{"repository":"example"}`, "OWNER/REPO"},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, issueTypesList.ID, tt.connection, tt.arguments, false)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), tt.message) {
			t.Errorf("%s: err = %v, want an invalid request naming %q", tt.name, err, tt.message)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}
