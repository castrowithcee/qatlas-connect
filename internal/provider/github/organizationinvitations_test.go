package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	orgInviteArguments  = `{"owner":"orgs/octo-org","username":"octocat"}`
	teamInviteArguments = `{"owner":"orgs/octo-org","team":"design","username":"octocat"}`
	inviteProviderText  = "provider-text-canary-91c2"
	inviteLinkText      = "invitation-link-canary-77d1"
)

// inviteServer scripts the routes of the invitation tools for organization octo-org.
type inviteServer struct {
	account    string // body of GET /users/octocat
	membership int    // status of the organization membership read, 404 by default
	state      string // state the membership read reports
	teams      string // body of the team list
	teamStatus int    // status of the read of team design
	listStatus int    // status of the team list, 200 by default
	listNext   bool   // whether every team list page announces a following page
	post       func(http.ResponseWriter, *http.Request)
	put        func(http.ResponseWriter, *http.Request)
}

func inviteCore(t *testing.T, s inviteServer) (*fakeGitHub, *int, *application.Core) {
	t.Helper()
	if s.account == "" {
		s.account = octocatAccount
	}
	if s.membership == 0 {
		s.membership = http.StatusNotFound
	}
	if s.teams == "" {
		s.teams = `[{"id":42,"slug":"design"},{"id":43,"slug":"docs"}]`
	}
	if s.teamStatus == 0 {
		s.teamStatus = http.StatusOK
	}
	if s.post == nil {
		s.post = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }
	}
	if s.put == nil {
		s.put = func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"state":"pending","role":"member"}`) }
	}
	failure := func(w http.ResponseWriter, status int) {
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"message":%q}`, inviteProviderText)
	}
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/v3")
		switch {
		case path == "/users/octocat" || path == "/users/OctoCat":
			fmt.Fprint(w, s.account)
		case path == "/orgs/octo-org/memberships/octocat":
			if s.membership >= 400 {
				failure(w, s.membership)
				return true
			}
			fmt.Fprintf(w, `{"state":%q,"role":"member"}`, s.state)
		case path == "/orgs/octo-org/teams" && r.Method == http.MethodGet:
			if s.listStatus >= 400 {
				failure(w, s.listStatus)
				return true
			}
			if s.listNext {
				w.Header().Set("Link", `<https://example.invalid/next>; rel="next"`)
			}
			fmt.Fprint(w, s.teams)
		case path == "/orgs/octo-org/teams/design" && r.Method == http.MethodGet:
			if s.teamStatus >= 400 {
				failure(w, s.teamStatus)
				return true
			}
			fmt.Fprint(w, `{"id":42,"slug":"design","name":"Design"}`)
		case path == "/orgs/octo-org/invitations" && r.Method == http.MethodPost:
			s.post(w, r)
		case path == "/orgs/octo-org/teams/design/memberships/octocat" && r.Method == http.MethodPut:
			s.put(w, r)
		default:
			return false
		}
		return true
	}
	base := serve(t, f)
	cfg := coreConfig(base)
	all := config.Effects()
	add := func(name string, targets, excludes []string, tools ...string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Targets: targets,
			Exclude: excludes, Effects: all, Tools: tools}
	}
	tools := []string{organizationInvitationsCreate.ID, teamMembersInvite.ID}
	add("listed", []string{"orgs/octo-org", "users/octocat"}, nil, tools...)
	add("mixed case", []string{"orgs/octo-org", "users/OctoCat"}, nil, tools...)
	add("unlisted", []string{"orgs/octo-org", "users/octocat"}, nil)
	add("no user", []string{"orgs/octo-org"}, nil, tools...)
	add("org as user", []string{"orgs/octo-org", "orgs/octocat"}, nil, tools...)
	add("other user", []string{"orgs/octo-org", "users/hubot"}, nil, tools...)
	add("no org", []string{"users/octocat"}, nil, tools...)
	add("repositories only", []string{"repos/octo-org/*", "users/octocat"}, nil, tools...)
	add("other org", []string{"orgs/other-org", "users/octocat"}, nil, tools...)
	reads := 0
	red := &redact.Redactor{}
	return f, &reads, application.New(registry(t), cfg, resolver(red, &reads), red)
}

// inviteTools lists both tools with an argument set that works for each.
var inviteTools = map[string]string{
	organizationInvitationsCreate.ID: orgInviteArguments,
	teamMembersInvite.ID:             teamInviteArguments,
}

func writes(f *fakeGitHub) int { return methods(f, http.MethodPost) + methods(f, http.MethodPut) }

func TestOrganizationInvitationsCreateInvitesAfterTheReadsAndOnePost(t *testing.T) {
	f, _, core := inviteCore(t, inviteServer{post: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		// A server that mirrors the invitation must not leak its ID or link.
		fmt.Fprintf(w, `{"id":987654,"html_url":"https://example.test/%s","invitation_teams_url":"u",`+
			`"login":"octocat","role":"direct_member"}`, inviteLinkText)
	}})
	for _, connection := range []string{"listed", "mixed case"} {
		result, err := invoke(t, core, organizationInvitationsCreate.ID, connection, orgInviteArguments, true)
		var answer map[string]any
		if err != nil || json.Unmarshal(result, &answer) != nil || answer["result"] != "invited" ||
			answer["username"] != "octocat" || answer["role"] != "direct_member" ||
			answer["owner"] != "orgs/octo-org" || len(answer) != 4 {
			t.Fatalf("%s result = %s, %v", connection, result, err)
		}
		if strings.Contains(string(result), "987654") || strings.Contains(string(result), inviteLinkText) ||
			strings.Contains(string(result), "http") {
			t.Errorf("result leaks an invitation: %s", result)
		}
	}
	requests := f.recorded()
	if len(requests) != 6 || requests[2].method != http.MethodPost || requests[2].path != "/api/v3/orgs/octo-org/invitations" ||
		requests[2].body["invitee_id"] != float64(583231) || requests[2].body["role"] != "direct_member" ||
		len(requests[2].body) != 2 {
		t.Errorf("requests = %+v, want two reads and one POST per call", requests)
	}
}

func TestOrganizationInvitationsCreateSendsRoleAndVerifiedTeams(t *testing.T) {
	f, _, core := inviteCore(t, inviteServer{})
	result, err := invoke(t, core, organizationInvitationsCreate.ID, "listed",
		`{"owner":"orgs/octo-org","username":"OCTOCAT","role":"admin","team_ids":[42,43]}`, true)
	if err != nil || !strings.Contains(string(result), `"username":"octocat"`) ||
		!strings.Contains(string(result), `"role":"admin"`) {
		t.Fatalf("result = %s, %v", result, err)
	}
	requests := f.recorded()
	last := requests[len(requests)-1]
	if len(requests) != 4 || last.method != http.MethodPost || last.body["role"] != "admin" ||
		fmt.Sprint(last.body["team_ids"]) != "[42 43]" {
		t.Errorf("requests = %+v, want the user, membership, and team reads before the POST", requests)
	}
}

func TestOrganizationInvitationsCreateRefusesTeamsOutsideTheOrganization(t *testing.T) {
	for name, teams := range map[string]string{"other id": `[{"id":42}]`, "empty": `[]`} {
		f, _, core := inviteCore(t, inviteServer{teams: teams})
		_, err := invoke(t, core, organizationInvitationsCreate.ID, "listed",
			`{"owner":"orgs/octo-org","username":"octocat","team_ids":[42,99]}`, true)
		if !isInvalidRequest(err) || writes(f) != 0 {
			t.Errorf("%s = %v, writes %d", name, err, writes(f))
		}
	}
	f, _, core := inviteCore(t, inviteServer{})
	_, err := invoke(t, core, organizationInvitationsCreate.ID, "listed",
		`{"owner":"orgs/octo-org","username":"octocat","team_ids":[42]}`, true)
	if err != nil || writes(f) != 1 {
		t.Errorf("known team = %v, writes %d", err, writes(f))
	}
}

func TestInvitationToolsRefuseBeforeAnySecretOrRequest(t *testing.T) {
	org := func(extra string) string { return `{"owner":"orgs/octo-org","username":"octocat"` + extra + `}` }
	team := func(extra string) string {
		return `{"owner":"orgs/octo-org","team":"design","username":"octocat"` + extra + `}`
	}
	type attempt struct{ connection, arguments string }
	cases := map[string]map[string]attempt{
		organizationInvitationsCreate.ID: {
			"bad role":         {"listed", org(`,"role":"billing_manager"`)},
			"reinstate":        {"listed", org(`,"role":"reinstate"`)},
			"zero team":        {"listed", org(`,"team_ids":[0]`)},
			"duplicate team":   {"listed", org(`,"team_ids":[42,42]`)},
			"too many teams":   {"listed", org(`,"team_ids":[` + strings.TrimSuffix(strings.Repeat("1,", 21), ",") + `]`)},
			"empty team list":  {"listed", org(`,"team_ids":[]`)},
			"team of a repo":   {"listed", org(`,"team":"design"`)},
			"invitee id given": {"listed", `{"owner":"orgs/octo-org","invitee_id":583231}`},
		},
		teamMembersInvite.ID: {
			"bad role":     {"listed", team(`,"role":"owner"`)},
			"slash team":   {"listed", `{"owner":"orgs/octo-org","team":"a/b","username":"octocat"}`},
			"missing team": {"listed", `{"owner":"orgs/octo-org","username":"octocat"}`},
			"team ids":     {"listed", team(`,"team_ids":[42]`)},
		},
	}
	for id, arguments := range inviteTools {
		base := map[string]attempt{
			"login not named":      {"other user", arguments},
			"no user target":       {"no user", arguments},
			"organization as user": {"org as user", arguments},
			"organization outside": {"other org", arguments},
			"no organization":      {"no org", arguments},
			"repositories only":    {"repositories only", arguments},
			"user as owner":        {"listed", strings.Replace(arguments, "orgs/octo-org", "users/octocat", 1)},
			"no owner given":       {"listed", strings.Replace(arguments, `"owner":"orgs/octo-org",`, "", 1)},
			"e-mail address":       {"listed", strings.Replace(arguments, "octocat", "octo@example.com", 1)},
			"at sign":              {"listed", strings.Replace(arguments, `"octocat"`, `"@octocat"`, 1)},
			"digits only":          {"listed", strings.Replace(arguments, `"octocat"`, `"583231"`, 1)},
			"slash login":          {"listed", strings.Replace(arguments, `"octocat"`, `"a/b"`, 1)},
			"empty login":          {"listed", strings.Replace(arguments, `"octocat"`, `""`, 1)},
		}
		for name, tt := range cases[id] {
			base[name] = tt
		}
		for name, tt := range base {
			f, reads, core := inviteCore(t, inviteServer{})
			_, err := invoke(t, core, id, tt.connection, tt.arguments, true)
			if !isInvalidRequest(err) || *reads != 0 || len(f.recorded()) != 0 {
				t.Errorf("%s %s = %v, reads %d, requests %d", id, name, err, *reads, len(f.recorded()))
			}
		}
	}
}

func TestInvitationToolsRefuseAnAccountThatIsNoMatchingUserWithoutAWrite(t *testing.T) {
	for id, arguments := range inviteTools {
		for name, account := range map[string]string{
			"organization": `{"login":"octocat","id":1,"type":"Organization"}`,
			"bot":          `{"login":"octocat","id":1,"type":"Bot"}`,
			"other login":  `{"login":"someone-else","id":1,"type":"User"}`,
			"no id":        `{"login":"octocat","type":"User"}`,
			"empty":        `{}`,
		} {
			f, _, core := inviteCore(t, inviteServer{account: account})
			_, err := invoke(t, core, id, "listed", arguments, true)
			if !isInvalidRequest(err) || writes(f) != 0 || len(f.recorded()) != 1 {
				t.Errorf("%s %s = %v, requests %+v", id, name, err, f.recorded())
			}
		}
	}
}

func TestInvitationToolsRefuseMembersAndOpenInvitationsWithoutAWrite(t *testing.T) {
	maintenance := map[string]string{
		organizationInvitationsCreate.ID: "github.organization-members.set-role",
		teamMembersInvite.ID:             "github.team-members.add",
	}
	for id, arguments := range inviteTools {
		f, _, core := inviteCore(t, inviteServer{membership: http.StatusOK, state: "active"})
		_, err := invoke(t, core, id, "listed", arguments, true)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), maintenance[id]) || writes(f) != 0 {
			t.Errorf("%s member = %v, writes %d", id, err, writes(f))
		}
		f, _, core = inviteCore(t, inviteServer{membership: http.StatusOK, state: "pending"})
		_, err = invoke(t, core, id, "listed", arguments, true)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "open invitation") || writes(f) != 0 {
			t.Errorf("%s open invitation = %v, writes %d", id, err, writes(f))
		}
	}
}

func TestInvitationToolsAreOfferedOnlyWhereListedAndNeedConfirmation(t *testing.T) {
	for id, arguments := range inviteTools {
		f, reads, core := inviteCore(t, inviteServer{})
		var unsupported *capability.UnsupportedError
		if _, err := invoke(t, core, id, "unlisted", arguments, true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools entry = %v, want unsupported-capability", id, err)
		}
		if _, err := invoke(t, core, id, "listed", arguments, false); err == nil || *reads != 0 ||
			len(f.recorded()) != 0 {
			t.Errorf("%s unconfirmed = %v, reads %d, requests %d", id, err, *reads, len(f.recorded()))
		}
	}
}

func TestInvitationToolsMapRefusalsWithoutProviderText(t *testing.T) {
	for id, arguments := range inviteTools {
		for _, tt := range []struct {
			name   string
			status int
			class  provider.Class
			want   string
		}{
			{"forbidden", http.StatusForbidden, provider.ClassPermission, "admin:org"},
			{"not found", http.StatusNotFound, provider.ClassNotFound, ""},
			{"unprocessable", http.StatusUnprocessableEntity, "", "refused this invitation"},
		} {
			answer := func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprintf(w, `{"message":%q}`, inviteProviderText)
			}
			f, _, core := inviteCore(t, inviteServer{post: answer, put: answer})
			_, err := invoke(t, core, id, "listed", arguments, true)
			if err == nil || strings.Contains(err.Error(), inviteProviderText) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("%s %s = %v, want %q without provider text", id, tt.name, err, tt.want)
			}
			if tt.status == http.StatusUnprocessableEntity && !isInvalidRequest(err) {
				t.Errorf("%s 422 = %v, want an invalid request", id, err)
			}
			if tt.class != "" && classOf(err) != tt.class {
				t.Errorf("%s %s class = %s, want %s", id, tt.name, classOf(err), tt.class)
			}
			if writes(f) != 1 {
				t.Errorf("%s %s writes = %d, want 1", id, tt.name, writes(f))
			}
		}
	}
}

func TestInvitationToolsMapRefusedReadsWithoutAWrite(t *testing.T) {
	for id, arguments := range inviteTools {
		f, _, core := inviteCore(t, inviteServer{membership: http.StatusForbidden})
		_, err := invoke(t, core, id, "listed", arguments, true)
		if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), inviteProviderText) ||
			writes(f) != 0 {
			t.Errorf("%s membership read = %v, writes %d", id, err, writes(f))
		}
	}
	f, _, core := inviteCore(t, inviteServer{teamStatus: http.StatusNotFound})
	_, err := invoke(t, core, teamMembersInvite.ID, "listed", teamInviteArguments, true)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), inviteProviderText) || writes(f) != 0 {
		t.Errorf("unknown team = %v, writes %d", err, writes(f))
	}
	f, _, core = inviteCore(t, inviteServer{teamStatus: http.StatusForbidden})
	_, err = invoke(t, core, teamMembersInvite.ID, "listed", teamInviteArguments, true)
	if classOf(err) != provider.ClassPermission || writes(f) != 0 {
		t.Errorf("refused team read = %v, writes %d", err, writes(f))
	}
}

func TestTeamMembersInviteInvitesAfterThreeReadsAndOnePut(t *testing.T) {
	f, _, core := inviteCore(t, inviteServer{put: func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"url":"https://example.test/%s","state":"pending","role":"maintainer"}`, inviteLinkText)
	}})
	result, err := invoke(t, core, teamMembersInvite.ID, "listed",
		`{"owner":"orgs/octo-org","team":"design","username":"OCTOCAT","role":"maintainer"}`, true)
	var answer map[string]any
	if err != nil || json.Unmarshal(result, &answer) != nil || answer["result"] != "invited" ||
		answer["team"] != "design" || answer["username"] != "octocat" || answer["role"] != "maintainer" ||
		answer["owner"] != "orgs/octo-org" || len(answer) != 5 || strings.Contains(string(result), "http") {
		t.Fatalf("result = %s, %v", result, err)
	}
	requests := f.recorded()
	if len(requests) != 4 || requests[3].method != http.MethodPut || len(requests[3].body) != 1 ||
		requests[3].body["role"] != "maintainer" ||
		requests[3].path != "/api/v3/orgs/octo-org/teams/design/memberships/octocat" {
		t.Errorf("requests = %+v, want three reads and one PUT", requests)
	}
	result, err = invoke(t, core, teamMembersInvite.ID, "listed", teamInviteArguments, true)
	if err != nil || !strings.Contains(string(result), `"role":"member"`) {
		t.Errorf("default role = %s, %v", result, err)
	}
}

func TestInvitationToolsNeverRepeatAnUnclearChange(t *testing.T) {
	for id, arguments := range inviteTools {
		for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
			"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
			"dropped connection": func(w http.ResponseWriter, _ *http.Request) {
				if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
					connection.Close()
				}
			},
		} {
			f, _, core := inviteCore(t, inviteServer{post: answer, put: answer})
			_, err := invoke(t, core, id, "listed", arguments, true)
			if err == nil || !strings.Contains(err.Error(), "may have been applied") || writes(f) != 1 {
				t.Errorf("%s %s = %v, writes %d, want an unclear outcome after one write", id, name, err, writes(f))
			}
		}
	}
	f, _, core := inviteCore(t, inviteServer{post: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}})
	_, err := invoke(t, core, organizationInvitationsCreate.ID, "listed", orgInviteArguments, true)
	if err == nil || !strings.Contains(err.Error(), "may have been applied") || writes(f) != 1 {
		t.Errorf("unexpected success = %v, writes %d", err, writes(f))
	}
}

func TestInvitationToolsReportATimeoutOnceAndUncertain(t *testing.T) {
	stall := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}
	for id, arguments := range inviteTools {
		f, _, core := inviteCore(t, inviteServer{post: stall, put: stall})
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := core.Invoke(ctx, application.InvokeRequest{Operation: id, Connection: "listed",
			Arguments: json.RawMessage(arguments), Confirmed: true})
		cancel()
		if classOf(err) != provider.ClassTimeout || !strings.Contains(err.Error(), "may have been applied") ||
			writes(f) != 1 {
			t.Errorf("%s = %v, writes %d, want a timeout with an unclear outcome after one write", id, err, writes(f))
		}
	}
}

func TestOrganizationInvitationsCreateFailsClosedWhenTeamsCannotBeConfirmed(t *testing.T) {
	const arguments = `{"owner":"orgs/octo-org","username":"octocat","team_ids":[99]}`
	for name, status := range map[string]int{"forbidden": http.StatusForbidden, "server error": http.StatusBadGateway} {
		f, _, core := inviteCore(t, inviteServer{listStatus: status})
		_, err := invoke(t, core, organizationInvitationsCreate.ID, "listed", arguments, true)
		if err == nil || strings.Contains(err.Error(), inviteProviderText) || writes(f) != 0 {
			t.Errorf("%s = %v, writes %d, want a refusal without provider text and without a write", name, err, writes(f))
		}
		if status == http.StatusForbidden && classOf(err) != provider.ClassPermission {
			t.Errorf("%s class = %s, want permission", name, classOf(err))
		}
	}
	// Every page announces a following one and the id is on none of them: the id stays unconfirmed.
	f, _, core := inviteCore(t, inviteServer{teams: `[{"id":1}]`, listNext: true})
	_, err := invoke(t, core, organizationInvitationsCreate.ID, "listed", arguments, true)
	pages := 0
	for _, r := range f.recorded() {
		if r.path == "/api/v3/orgs/octo-org/teams" {
			pages++
		}
	}
	if !isInvalidRequest(err) || strings.Contains(err.Error(), inviteProviderText) || writes(f) != 0 || pages != maxTeamListPages {
		t.Errorf("endless list = %v, writes %d, pages %d, want a refusal after %d pages", err, writes(f), pages,
			maxTeamListPages)
	}
}

// An exclusion that equals an owner target is no valid configuration, and one for a repository or project
// never touches an owner target, so no valid connection can exclude the organization or the login of an
// invitation. The error comes before any secret read.
func TestInvitationToolsStopOnAnExclusionThatEqualsAnOwnerTarget(t *testing.T) {
	for _, exclude := range []string{"orgs/octo-org", "users/octocat"} {
		for id, arguments := range inviteTools {
			reads := new(int)
			cfg := coreConfig("https://example.invalid/api/v3")
			cfg.Connections["bad"] = config.Connection{Service: "gh", Credential: "gh-reader",
				Targets: []string{"orgs/octo-org", "users/octocat"}, Exclude: []string{exclude},
				Effects: config.Effects(), Tools: []string{id}}
			red := &redact.Redactor{}
			core := application.New(registry(t), cfg, resolver(red, reads), red)
			_, err := invoke(t, core, id, "bad", arguments, true)
			if err == nil || *reads != 0 {
				t.Errorf("%s exclude %s = %v, reads %d", id, exclude, err, *reads)
			}
		}
	}
}
