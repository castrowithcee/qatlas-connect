package github

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const teamProviderCanary = "team-provider-text-canary-5e1f"

// fakeTeams answers the team routes of organization octo-org through the failure hook of fakeGitHub. Members
// of the organization are kept by login with the state of their membership.
type fakeTeams struct {
	mu sync.Mutex
	// status, when set, answers every team route, but not the organization membership read, with this status
	// and GitHub's own text.
	status int
	// membership is the organization membership state by login; a login it lacks is not a member (404).
	membership map[string]string
}

const fullTeamJSON = `{"id":42,"node_id":"T_kwDO","url":"https://api.github.com/organizations/1/team/42",` +
	`"html_url":"https://github.com/orgs/octo-org/teams/design","name":"Design","slug":"design",` +
	`"description":"Design team","privacy":"closed","notification_setting":"notifications_enabled",` +
	`"permission":"pull","members_url":"https://api.github.com/organizations/1/team/42/members{/member}",` +
	`"repositories_url":"https://api.github.com/organizations/1/team/42/repos","parent":null,"members_count":3,` +
	`"repos_count":2,"created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-03T03:04:05Z","type":"organization",` +
	`"organization_id":1,"ldap_dn":"cn=design",` +
	`"organization":{"login":"octo-org","id":1,"node_id":"O_kgDO","name":"Octo Org","billing_email":"billing-contact",` +
	`"plan":{"name":"team","space":100,"private_repos":5,"filled_seats":3,"seats":10},` +
	`"html_url":"https://github.com/octo-org","url":"https://api.github.com/orgs/octo-org","type":"Organization",` +
	`"members_can_create_pages":true,"two_factor_requirement_enabled":false}}`

const teamRepoJSON = `{"id":1296269,"node_id":"R_kgDO","name":"example","full_name":"octo-org/example",` +
	`"private":true,"html_url":"https://github.com/octo-org/example","permissions":{"admin":false,"push":true,"pull":true},` +
	`"role_name":"write","temp_clone_token":"clone-token-canary","owner":{"login":"octo-org","id":1}}`

const teamOutsideRepoJSON = `{"id":1296270,"node_id":"R_kgDP","name":"secret","full_name":"octo-org/secret",` +
	`"private":true,"html_url":"https://github.com/octo-org/secret","description":"hidden description"}`

func (tm *fakeTeams) route(w http.ResponseWriter, req *http.Request) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	path := strings.TrimPrefix(req.URL.EscapedPath(), "/api/v3")
	rest, ok := strings.CutPrefix(path, "/orgs/octo-org/")
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if login, found := strings.CutPrefix(rest, "memberships/"); found {
		state, member := tm.membership[login]
		if !member {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return true
		}
		fmt.Fprintf(w, `{"state":%q,"role":"member"}`, state)
		return true
	}
	if rest != "teams" && !strings.HasPrefix(rest, "teams/") {
		return false
	}
	if tm.status != 0 {
		w.WriteHeader(tm.status)
		fmt.Fprintf(w, `{"message":%q}`, teamProviderCanary)
		return true
	}
	switch {
	case req.Method == http.MethodPost && rest == "teams":
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, fullTeamJSON)
	case req.Method == http.MethodGet && rest == "teams/design", req.Method == http.MethodPatch && rest == "teams/design":
		fmt.Fprint(w, fullTeamJSON)
	case req.Method == http.MethodGet && rest == "teams/design/repos":
		w.Header().Set("Link", `<https://api.github.com/orgs/octo-org/teams/design/repos?page=2>; rel="next"`)
		fmt.Fprint(w, "["+teamRepoJSON+","+teamOutsideRepoJSON+"]")
	case req.Method == http.MethodGet && rest == "teams/design/memberships/octocat":
		fmt.Fprint(w, `{"url":"https://api.github.com/teams/42/memberships/octocat","role":"member","state":"active"}`)
	case req.Method == http.MethodPut && rest == "teams/design/memberships/octocat":
		fmt.Fprint(w, `{"url":"https://api.github.com/teams/42/memberships/octocat","role":"maintainer","state":"active"}`)
	case req.Method == http.MethodDelete || req.Method == http.MethodPut:
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}
	return true
}

func serveTeams(t *testing.T) (*fakeGitHub, *fakeTeams, string) {
	t.Helper()
	tm := &fakeTeams{membership: map[string]string{"octocat": "active", "newcomer": "pending"}}
	f := &fakeGitHub{}
	f.failure = tm.route
	return f, tm, serve(t, f)
}

var allTeamTools = append([]string(nil), teamToolIDs...)

func teamsConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["team-listed"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Exclude: []string{"repos/octo-org/secret"},
		Effects: all, Tools: allTeamTools}
	cfg.Connections["team-unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Effects: all}
	cfg.Connections["team-org-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: all, Tools: allTeamTools}
	cfg.Connections["team-repo-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: all, Tools: allTeamTools}
	cfg.Connections["team-excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: all, Tools: allTeamTools}
	cfg.Connections["team-user"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"users/octocat"}, Effects: all, Tools: allTeamTools}
	return cfg
}

func teamsCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), teamsConfig(base), resolver(red, reads), red)
}

const teamOwner = `"owner":"orgs/octo-org"`

// teamCalls holds one valid call of every tool, with the requests it must send.
var teamCalls = []struct {
	id, arguments string
	change        bool
	// requests are method and path of every request, in order.
	requests [][2]string
	want     []string
}{
	{teamsGet.ID, `{"team":"design"}`, false, [][2]string{{"GET", "/api/v3/orgs/octo-org/teams/design"}},
		[]string{`"team":"design"`, `"members_count":3`, `"repos_count":2`, `"ldap_dn":"cn=design"`,
			`"billing_email":"billing-contact"`, `"filled_seats":3`, `"owner":"orgs/octo-org"`}},
	{teamsCreate.ID, `{"name":"Design","privacy":"closed","parent_team_id":7}`, true,
		[][2]string{{"POST", "/api/v3/orgs/octo-org/teams"}}, []string{`"team":"design"`, `"created_at"`}},
	{teamsUpdate.ID, `{"team":"design","description":"New","parent_team_id":null}`, true,
		[][2]string{{"PATCH", "/api/v3/orgs/octo-org/teams/design"}}, []string{`"team":"design"`}},
	{teamsDelete.ID, `{"team":"design"}`, true, [][2]string{{"DELETE", "/api/v3/orgs/octo-org/teams/design"}},
		[]string{`"deleted":true`, `"team":"design"`}},
	{teamMembersAdd.ID, `{"team":"design","username":"octocat","role":"maintainer"}`, true,
		[][2]string{{"GET", "/api/v3/orgs/octo-org/memberships/octocat"},
			{"PUT", "/api/v3/orgs/octo-org/teams/design/memberships/octocat"}},
		[]string{`"role":"maintainer"`, `"state":"active"`, `"username":"octocat"`}},
	{teamMembersRemove.ID, `{"team":"design","username":"octocat"}`, true,
		[][2]string{{"DELETE", "/api/v3/orgs/octo-org/teams/design/memberships/octocat"}},
		[]string{`"removed":true`, `"username":"octocat"`}},
	{teamMembershipsGet.ID, `{"team":"design","username":"octocat"}`, false,
		[][2]string{{"GET", "/api/v3/orgs/octo-org/teams/design/memberships/octocat"}},
		[]string{`"role":"member"`, `"state":"active"`}},
	{teamRepositoriesList.ID, `{"team":"design"}`, false,
		[][2]string{{"GET", "/api/v3/orgs/octo-org/teams/design/repos"}},
		[]string{`"full_name":"octo-org/example"`, `"role_name":"write"`, `"has_more":true`}},
	{teamRepositoriesSet.ID, `{"team":"design","repository":"octo-org/example","permission":"push"}`, true,
		[][2]string{{"PUT", "/api/v3/orgs/octo-org/teams/design/repos/octo-org/example"}},
		[]string{`"set":true`, `"permission":"push"`, `"repository":"octo-org/example"`}},
	{teamRepositoriesRemove.ID, `{"team":"design","repository":"octo-org/example"}`, true,
		[][2]string{{"DELETE", "/api/v3/orgs/octo-org/teams/design/repos/octo-org/example"}},
		[]string{`"removed":true`, `"repository":"octo-org/example"`}},
}

func withTeamOwner(arguments string) string { return `{` + teamOwner + `,` + arguments[1:] }

// Every team tool satisfies its output contract through the application core and sends its changes as exactly
// the documented requests, once.
func TestTeamToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, _, base := serveTeams(t)
	core := teamsCore(t, base, nil)
	for _, call := range teamCalls {
		before := len(f.recorded())
		result, err := invoke(t, core, call.id, "team-listed", withTeamOwner(call.arguments), call.change)
		if err != nil {
			t.Fatalf("%s = %v", call.id, err)
		}
		for _, want := range append(call.want, `"owner":"orgs/octo-org"`) {
			if !strings.Contains(string(result), want) {
				t.Errorf("%s = %s, want %s", call.id, result, want)
			}
		}
		requests := f.recorded()[before:]
		if len(requests) != len(call.requests) {
			t.Fatalf("%s sent %+v, want %v", call.id, requests, call.requests)
		}
		for i, want := range call.requests {
			if requests[i].method != want[0] || requests[i].path != want[1] {
				t.Errorf("%s request %d = %s %s, want %s %s", call.id, i, requests[i].method, requests[i].path, want[0], want[1])
			}
		}
	}
}

// The bodies carry only what was named, and a parent can be cleared with null.
func TestTeamChangeBodies(t *testing.T) {
	f, _, base := serveTeams(t)
	core := teamsCore(t, base, nil)
	for _, tt := range []struct {
		id, arguments string
		want          map[string]any
	}{
		{teamsCreate.ID, `{"name":"Design","description":"d","notification_setting":"notifications_disabled"}`,
			map[string]any{"name": "Design", "description": "d", "notification_setting": "notifications_disabled"}},
		{teamsUpdate.ID, `{"team":"design","parent_team_id":null}`, map[string]any{"parent_team_id": nil}},
		{teamsUpdate.ID, `{"team":"design","privacy":"secret"}`, map[string]any{"privacy": "secret"}},
		{teamMembersAdd.ID, `{"team":"design","username":"octocat"}`, map[string]any{}},
		{teamRepositoriesSet.ID, `{"team":"design","repository":"octo-org/example","permission":"admin"}`,
			map[string]any{"permission": "admin"}},
	} {
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, "team-listed", withTeamOwner(tt.arguments), true); err != nil {
			t.Fatalf("%s = %v", tt.id, err)
		}
		requests := f.recorded()[before:]
		sent := requests[len(requests)-1].body
		if len(sent) != len(tt.want) {
			t.Errorf("%s sent %v, want %v", tt.id, sent, tt.want)
		}
		for key, want := range tt.want {
			if value, present := sent[key]; !present || value != want {
				t.Errorf("%s sent %v, want %s = %v", tt.id, sent, key, want)
			}
		}
	}
}

// An account that is no active member of the organization never reaches a team: the tool reads the membership
// and refuses without a single change, so GitHub never creates an invitation.
func TestTeamMembersAddRefusesAccountsThatAreNotActiveMembers(t *testing.T) {
	f, tm, base := serveTeams(t)
	core := teamsCore(t, base, nil)
	tm.membership["odd"] = "unknown"
	for _, username := range []string{"stranger", "newcomer", "odd"} {
		before := len(f.recorded())
		_, err := invoke(t, core, teamMembersAdd.ID, "team-listed",
			`{`+teamOwner+`,"team":"design","username":"`+username+`"}`, true)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "invite") {
			t.Errorf("%s = %v, want an invalid request naming the invitation", username, err)
		}
		requests := f.recorded()[before:]
		if len(requests) != 1 || requests[0].method != http.MethodGet ||
			requests[0].path != "/api/v3/orgs/octo-org/memberships/"+username {
			t.Errorf("%s sent %+v, want the membership read alone", username, requests)
		}
	}
	// A refused read of the membership stops the change as well.
	tm.status = 0
	f2 := f
	f2.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/memberships/") && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, `{"message":%q}`, teamProviderCanary)
			return true
		}
		return tm.route(w, r)
	}
	before := len(f.recorded())
	_, err := invoke(t, core, teamMembersAdd.ID, "team-listed",
		`{`+teamOwner+`,"team":"design","username":"octocat"}`, true)
	if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), teamProviderCanary) {
		t.Errorf("forbidden membership read = %v, want a permission refusal without GitHub's text", err)
	}
	if requests := f.recorded()[before:]; len(requests) != 1 || requests[0].method != http.MethodGet {
		t.Errorf("forbidden membership read sent %+v, want the read alone", requests)
	}
}

// The organization, the repository, and the slugs are settled before a credential is read or GitHub is
// contacted.
func TestTeamToolRefusalsBeforeIO(t *testing.T) {
	f, _, base := serveTeams(t)
	reads := 0
	core := teamsCore(t, base, &reads)
	for _, tt := range []struct {
		name, id, connection, arguments, message string
	}{
		{"owner outside the targets", teamsGet.ID, "team-listed", `{"owner":"orgs/other","team":"design"}`,
			"outside the targets"},
		{"excluded organization", teamsDelete.ID, "team-excluded", `{"owner":"orgs/octo-org","team":"design"}`, ""},
		{"user owner", teamsCreate.ID, "team-user", `{"name":"x"}`, "teams belong to an organization"},
		{"no owner target", teamsGet.ID, "team-repo-only", `{"team":"design"}`, ""},
		{"set outside the targets", teamRepositoriesSet.ID, "team-org-only",
			`{"owner":"orgs/octo-org","team":"design","repository":"octo-org/example","permission":"push"}`, ""},
		{"remove outside the targets", teamRepositoriesRemove.ID, "team-org-only",
			`{"owner":"orgs/octo-org","team":"design","repository":"octo-org/example"}`, ""},
		{"set on an excluded repository", teamRepositoriesSet.ID, "team-listed",
			`{"owner":"orgs/octo-org","team":"design","repository":"octo-org/secret","permission":"push"}`, ""},
		{"set on a repository of another owner", teamRepositoriesSet.ID, "team-listed",
			`{"owner":"orgs/octo-org","team":"design","repository":"hubot/example","permission":"push"}`, ""},
		{"path injection in the slug", teamsGet.ID, "team-listed", `{"owner":"orgs/octo-org","team":"../x"}`, ""},
		{"path injection in the slug of a change", teamsDelete.ID, "team-listed",
			`{"owner":"orgs/octo-org","team":"design/members"}`, ""},
		{"path injection in the username", teamMembersAdd.ID, "team-listed",
			`{"owner":"orgs/octo-org","team":"design","username":"a/../b"}`, ""},
		{"query in the username", teamMembershipsGet.ID, "team-listed",
			`{"owner":"orgs/octo-org","team":"design","username":"a?x=1"}`, ""},
		{"unknown permission", teamRepositoriesSet.ID, "team-listed",
			`{"owner":"orgs/octo-org","team":"design","repository":"octo-org/example","permission":"owner"}`, ""},
		{"unknown role", teamMembersAdd.ID, "team-listed",
			`{"owner":"orgs/octo-org","team":"design","username":"octocat","role":"owner"}`, ""},
		{"empty update", teamsUpdate.ID, "team-listed", `{"owner":"orgs/octo-org","team":"design"}`, "at least one"},
		{"control character in the name", teamsCreate.ID, "team-listed", `{"owner":"orgs/octo-org","name":"a\nb"}`, ""},
		{"null parent on create", teamsCreate.ID, "team-listed", `{"owner":"orgs/octo-org","name":"a","parent_team_id":null}`, ""},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		var unsupported *capability.UnsupportedError
		if err == nil || errors.As(err, &unsupported) ||
			(tt.message != "" && !strings.Contains(err.Error(), tt.message)) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential (%d) or GitHub", tt.name, reads)
		}
	}
}

// Every tool names the failures of GitHub without quoting GitHub's text, sends exactly one request for a change,
// and never repeats a change that timed out.
func TestTeamToolFailuresAreNamed(t *testing.T) {
	f, tm, base := serveTeams(t)
	core := teamsCore(t, base, nil)
	for _, call := range teamCalls {
		for _, tt := range []struct {
			status    int
			class     provider.Class
			uncertain bool
		}{
			{http.StatusForbidden, provider.ClassPermission, false},
			{http.StatusNotFound, provider.ClassNotFound, false},
			{http.StatusUnprocessableEntity, provider.ClassProviderError, false},
			{http.StatusGatewayTimeout, provider.ClassTimeout, call.change},
		} {
			tm.status = tt.status
			before := len(f.recorded())
			_, err := invoke(t, core, call.id, "team-listed", withTeamOwner(call.arguments), call.change)
			if classOf(err) != tt.class || strings.Contains(err.Error(), uncertain) != tt.uncertain ||
				strings.Contains(err.Error(), teamProviderCanary) {
				t.Errorf("%s answered %d = %v, want %s without GitHub's text", call.id, tt.status, err, tt.class)
			}
			want := len(call.requests)
			if got := len(f.recorded()[before:]); got != want {
				t.Errorf("%s answered %d sent %d requests, want %d without a retry", call.id, tt.status, got, want)
			}
			if tt.status == http.StatusForbidden && !strings.Contains(err.Error(), ":") &&
				!strings.Contains(err.Error(), "admin:org") && !strings.Contains(err.Error(), "read:org") {
				t.Errorf("%s forbidden = %v, want the token right named", call.id, err)
			}
		}
		tm.status = 0
	}
}

// A change tool is offered only where the connection's tools list names it; a read is offered by effect.
func TestTeamChangeToolsAreListedOnlyAndNeedConfirmation(t *testing.T) {
	f, _, base := serveTeams(t)
	reads := 0
	core := teamsCore(t, base, &reads)
	var unsupported *capability.UnsupportedError
	for _, call := range teamCalls {
		_, err := core.Describe(application.DescribeRequest{Operation: call.id, Connection: "team-unlisted"})
		if call.change == errors.Is(err, nil) {
			t.Errorf("describe %s without a tools list: err = %v, want offered = %t", call.id, err, !call.change)
		}
		if call.change {
			if !errors.As(err, &unsupported) {
				t.Errorf("describe %s = %v, want unsupported", call.id, err)
			}
			if _, err := invoke(t, core, call.id, "team-unlisted", withTeamOwner(call.arguments), true); !errors.As(err, &unsupported) {
				t.Errorf("invoke %s = %v, want unsupported", call.id, err)
			}
			unconfirmed := &application.ConfirmationRequiredError{}
			if _, err := invoke(t, core, call.id, "team-listed", withTeamOwner(call.arguments), false); !errors.As(err, &unconfirmed) {
				t.Errorf("invoke %s without confirmation = %v, want confirmation-required", call.id, err)
			}
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}

// A repository outside the targets keeps only its identifiers, and the clone token never appears.
func TestTeamRepositoriesListProjection(t *testing.T) {
	_, _, base := serveTeams(t)
	core := teamsCore(t, base, nil)
	result, err := invoke(t, core, teamRepositoriesList.ID, "team-listed", `{`+teamOwner+`,"team":"design"}`, false)
	text := string(result)
	if err != nil || strings.Contains(text, "clone-token-canary") || strings.Contains(text, "hidden description") ||
		!strings.Contains(text, `"full_name":"octo-org/secret"`) || !strings.Contains(text, `"push":true`) {
		t.Fatalf("list = %s, %v", result, err)
	}
	if !strings.Contains(text, `"next_cursor"`) {
		t.Errorf("list = %s, want a cursor", result)
	}
}
