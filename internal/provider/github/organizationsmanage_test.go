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

const organizationChangeCanary = "organization-change-text-canary-8d4e"

// fakeOrganizationChange answers the organization change routes of octo-org through the failure hook of
// fakeGitHub. Memberships are kept by login with their state; a login it lacks is not a member (404).
type fakeOrganizationChange struct {
	mu sync.Mutex
	// status, when set, answers every change route, but not the membership read, with this status and GitHub's
	// own text.
	status     int
	membership map[string]string
}

func (o *fakeOrganizationChange) route(w http.ResponseWriter, req *http.Request) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	path := strings.TrimPrefix(req.URL.EscapedPath(), "/api/v3")
	rest, ok := strings.CutPrefix(path, "/orgs/octo-org")
	if !ok {
		return false
	}
	rest = strings.TrimPrefix(rest, "/")
	w.Header().Set("Content-Type", "application/json")
	if login, found := strings.CutPrefix(rest, "memberships/"); found && req.Method == http.MethodGet {
		state, member := o.membership[login]
		if !member {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return true
		}
		fmt.Fprintf(w, `{"state":%q,"role":"member"}`, state)
		return true
	}
	if req.Method == http.MethodGet {
		return false
	}
	if o.status != 0 {
		w.WriteHeader(o.status)
		fmt.Fprintf(w, `{"message":%q}`, organizationChangeCanary)
		return true
	}
	switch {
	case req.Method == http.MethodPatch && rest == "":
		fmt.Fprint(w, fullOrganizationJSON)
	case req.Method == http.MethodPut && strings.HasPrefix(rest, "memberships/"):
		fmt.Fprint(w, organizationMembershipJSON)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
	return true
}

func serveOrganizationChange(t *testing.T) (*fakeGitHub, *fakeOrganizationChange, string) {
	t.Helper()
	o := &fakeOrganizationChange{membership: map[string]string{"octocat": "active", "newcomer": "pending"}}
	f := &fakeGitHub{}
	f.failure = o.route
	return f, o, serve(t, f)
}

func organizationChangeConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["orgc-listed"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Effects: all, Tools: organizationChangeToolIDs}
	cfg.Connections["orgc-unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: all}
	cfg.Connections["orgc-repo-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: all, Tools: organizationChangeToolIDs}
	cfg.Connections["orgc-excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: all, Tools: organizationChangeToolIDs}
	cfg.Connections["orgc-user"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"users/octocat"}, Effects: all, Tools: organizationChangeToolIDs}
	return cfg
}

func organizationChangeCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), organizationChangeConfig(base), resolver(red, reads), red)
}

const orgBase = "/api/v3/orgs/octo-org"

// organizationChangeCalls holds one valid call of every change tool with the requests it must send.
var organizationChangeCalls = []struct {
	id, arguments string
	requests      [][2]string
	want          []string
}{
	{organizationsUpdate.ID, `{"description":"New","members_can_create_pages":false}`,
		[][2]string{{"PATCH", orgBase}}, []string{`"login":"octo-org"`, `"two_factor_requirement_enabled":true`}},
	{organizationMembersSetRole.ID, `{"username":"octocat","role":"admin"}`,
		[][2]string{{"GET", orgBase + "/memberships/octocat"}, {"PUT", orgBase + "/memberships/octocat"}},
		[]string{`"username":"octocat"`, `"state":"active"`, `"role":"admin"`}},
	{organizationMembersRemove.ID, `{"username":"octocat"}`, [][2]string{{"DELETE", orgBase + "/members/octocat"}},
		[]string{`"removed":true`, `"username":"octocat"`}},
	{organizationRolesAssign.ID, `{"username":"octocat","role_id":9001}`,
		[][2]string{{"GET", orgBase + "/memberships/octocat"},
			{"PUT", orgBase + "/organization-roles/users/octocat/9001"}},
		[]string{`"assigned":true`, `"username":"octocat"`, `"role_id":9001`}},
	{outsideCollaboratorsRemove.ID, `{"username":"octocat"}`,
		[][2]string{{"DELETE", orgBase + "/outside_collaborators/octocat"}},
		[]string{`"removed":true`, `"username":"octocat"`}},
	{organizationInvitationsCancel.ID, `{"invitation_id":77}`, [][2]string{{"DELETE", orgBase + "/invitations/77"}},
		[]string{`"cancelled":true`, `"invitation_id":77`}},
}

func withOrganizationChangeOwner(arguments string) string {
	return `{` + teamOwner + `,` + arguments[1:]
}

// Every change tool satisfies its output contract through the application core, sends exactly the documented
// requests, once, and shows neither a two-factor member of another account nor an invitation token.
func TestOrganizationChangeToolsSatisfyTheirContract(t *testing.T) {
	f, _, base := serveOrganizationChange(t)
	core := organizationChangeCore(t, base, nil)
	for _, call := range organizationChangeCalls {
		before := len(f.recorded())
		result, err := invoke(t, core, call.id, "orgc-listed", withOrganizationChangeOwner(call.arguments), true)
		if err != nil {
			t.Fatalf("%s = %v", call.id, err)
		}
		for _, want := range append(call.want, `"owner":"orgs/octo-org"`) {
			if !strings.Contains(string(result), want) {
				t.Errorf("%s = %s, want %s", call.id, result, want)
			}
		}
		for _, unwanted := range []string{"two_factor_authentication", "two_factor_enabled", invitationTokenCanary,
			invitationAcceptCanary, `"token"`, "invitation_url"} {
			if strings.Contains(string(result), unwanted) {
				t.Errorf("%s carries %q: %s", call.id, unwanted, result)
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

// The update body carries only the settings that were named, and no billing contact is ever sent.
func TestOrganizationUpdateBodyCarriesOnlyNamedSettings(t *testing.T) {
	f, _, base := serveOrganizationChange(t)
	core := organizationChangeCore(t, base, nil)
	for _, tt := range []struct {
		arguments string
		want      map[string]any
	}{
		{`{"description":"New"}`, map[string]any{"description": "New"}},
		{`{"description":""}`, map[string]any{"description": ""}},
		{`{"default_repository_permission":"read","members_can_create_repositories":false}`,
			map[string]any{"default_repository_permission": "read", "members_can_create_repositories": false}},
		{`{"web_commit_signoff_required":true,"name":"Octo"}`,
			map[string]any{"web_commit_signoff_required": true, "name": "Octo"}},
	} {
		before := len(f.recorded())
		if _, err := invoke(t, core, organizationsUpdate.ID, "orgc-listed", withOrganizationChangeOwner(tt.arguments), true); err != nil {
			t.Fatalf("%s = %v", tt.arguments, err)
		}
		sent := f.recorded()[before:][0].body
		if len(sent) != len(tt.want) {
			t.Errorf("%s sent %v, want %v", tt.arguments, sent, tt.want)
		}
		for key, want := range tt.want {
			if value, present := sent[key]; !present || value != want {
				t.Errorf("%s sent %v, want %s = %v", tt.arguments, sent, key, want)
			}
		}
	}
	descriptor, err := core.Describe(application.DescribeRequest{Operation: organizationsUpdate.ID, Connection: "orgc-listed"})
	if err != nil || strings.Contains(string(descriptor.Operation.InputSchema), "billing_email") {
		t.Errorf("update schema offers billing_email: %v", err)
	}
	if _, err := invoke(t, core, organizationsUpdate.ID, "orgc-listed",
		`{`+teamOwner+`,"billing_email":"billing-contact"}`, true); err == nil {
		t.Error("update accepted billing_email")
	}
}

// An account that is no active member never gets a role: the tool reads the membership and refuses without a
// single change, so GitHub never creates an invitation.
func TestOrganizationRoleToolsRefuseAccountsThatAreNotActiveMembers(t *testing.T) {
	f, o, base := serveOrganizationChange(t)
	core := organizationChangeCore(t, base, nil)
	o.membership["odd"] = "unknown"
	for _, tool := range []struct{ id, arguments string }{
		{organizationMembersSetRole.ID, `"role":"member"`},
		{organizationRolesAssign.ID, `"role_id":9001`},
	} {
		for _, username := range []string{"stranger", "newcomer", "odd"} {
			before := len(f.recorded())
			_, err := invoke(t, core, tool.id, "orgc-listed",
				`{`+teamOwner+`,"username":"`+username+`",`+tool.arguments+`}`, true)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "never invites") {
				t.Errorf("%s %s = %v, want an invalid request naming the invitation", tool.id, username, err)
			}
			requests := f.recorded()[before:]
			if len(requests) != 1 || requests[0].method != http.MethodGet ||
				requests[0].path != orgBase+"/memberships/"+username {
				t.Errorf("%s %s sent %+v, want the membership read alone", tool.id, username, requests)
			}
		}
	}
	// A refused read of the membership stops the change as well.
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/memberships/") && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, `{"message":%q}`, organizationChangeCanary)
			return true
		}
		return o.route(w, r)
	}
	before := len(f.recorded())
	_, err := invoke(t, core, organizationMembersSetRole.ID, "orgc-listed",
		`{`+teamOwner+`,"username":"octocat","role":"admin"}`, true)
	if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), organizationChangeCanary) {
		t.Errorf("forbidden membership read = %v, want a permission refusal without GitHub's text", err)
	}
	if requests := f.recorded()[before:]; len(requests) != 1 || requests[0].method != http.MethodGet {
		t.Errorf("forbidden membership read sent %+v, want the read alone", requests)
	}
}

// The organization, the username, and the values are settled before a credential is read or GitHub is
// contacted.
func TestOrganizationChangeRefusalsBeforeIO(t *testing.T) {
	f, _, base := serveOrganizationChange(t)
	reads := 0
	core := organizationChangeCore(t, base, &reads)
	for _, tt := range []struct {
		name, id, connection, arguments, message string
	}{
		{"owner outside the targets", organizationsUpdate.ID, "orgc-listed",
			`{"owner":"orgs/other","description":"x"}`, "outside the targets"},
		{"foreign target for a removal", organizationMembersRemove.ID, "orgc-listed",
			`{"owner":"orgs/other","username":"octocat"}`, "outside the targets"},
		{"excluded organization", organizationMembersRemove.ID, "orgc-excluded",
			`{"owner":"orgs/octo-org","username":"octocat"}`, ""},
		{"excluded organization for a role", organizationMembersSetRole.ID, "orgc-excluded",
			`{"owner":"orgs/octo-org","username":"octocat","role":"admin"}`, ""},
		{"user owner", organizationInvitationsCancel.ID, "orgc-user",
			`{"owner":"users/octocat","invitation_id":1}`, "organization"},
		{"repository target only", outsideCollaboratorsRemove.ID, "orgc-repo-only", `{"username":"octocat"}`, ""},
		{"path injection in the username", organizationMembersRemove.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","username":"a/../b"}`, ""},
		{"query in the username", outsideCollaboratorsRemove.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","username":"a?x=1"}`, ""},
		{"empty username", organizationMembersSetRole.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","username":"","role":"admin"}`, ""},
		{"unknown role", organizationMembersSetRole.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","username":"octocat","role":"billing_manager"}`, ""},
		{"missing role", organizationMembersSetRole.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","username":"octocat"}`, ""},
		{"zero role id", organizationRolesAssign.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","username":"octocat","role_id":0}`, ""},
		{"negative role id", organizationRolesAssign.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","username":"octocat","role_id":-4}`, ""},
		{"missing invitation id", organizationInvitationsCancel.ID, "orgc-listed", `{"owner":"orgs/octo-org"}`, ""},
		{"fractional invitation id", organizationInvitationsCancel.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","invitation_id":1.5}`, ""},
		{"empty update", organizationsUpdate.ID, "orgc-listed", `{"owner":"orgs/octo-org"}`, "at least one"},
		{"unknown default permission", organizationsUpdate.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","default_repository_permission":"owner"}`, ""},
		{"control character in the name", organizationsUpdate.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","name":"a\nb"}`, ""},
		{"too long description", organizationsUpdate.ID, "orgc-listed",
			`{"owner":"orgs/octo-org","description":"` + strings.Repeat("x", 256) + `"}`, ""},
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

// Every tool names the failures of GitHub without quoting GitHub's text, never repeats a change, and reports a
// timeout as uncertain.
func TestOrganizationChangeFailuresAreNamed(t *testing.T) {
	f, o, base := serveOrganizationChange(t)
	core := organizationChangeCore(t, base, nil)
	for _, call := range organizationChangeCalls {
		for _, tt := range []struct {
			status int
			class  provider.Class
			unsure bool
		}{
			{http.StatusForbidden, provider.ClassPermission, false},
			{http.StatusNotFound, provider.ClassNotFound, false},
			{http.StatusUnprocessableEntity, provider.ClassProviderError, false},
			{http.StatusBadGateway, provider.ClassProviderError, true},
			{http.StatusGatewayTimeout, provider.ClassTimeout, true},
		} {
			o.status = tt.status
			before := len(f.recorded())
			_, err := invoke(t, core, call.id, "orgc-listed", withOrganizationChangeOwner(call.arguments), true)
			if err == nil || classOf(err) != tt.class || strings.Contains(err.Error(), uncertain) != tt.unsure ||
				strings.Contains(err.Error(), organizationChangeCanary) {
				t.Errorf("%s answered %d = %v, want %s without GitHub's text", call.id, tt.status, err, tt.class)
				continue
			}
			if got := len(f.recorded()[before:]); got != len(call.requests) {
				t.Errorf("%s answered %d sent %d requests, want %d without a retry", call.id, tt.status, got,
					len(call.requests))
			}
			if tt.status == http.StatusForbidden && !strings.Contains(err.Error(), "admin:org") {
				t.Errorf("%s forbidden = %v, want the token right named", call.id, err)
			}
		}
		o.status = 0
	}
}

// A change tool is offered only where the connection's tools list names it, and needs confirmation.
func TestOrganizationChangeToolsAreListedOnlyAndNeedConfirmation(t *testing.T) {
	f, _, base := serveOrganizationChange(t)
	reads := 0
	core := organizationChangeCore(t, base, &reads)
	var unsupported *capability.UnsupportedError
	for _, call := range organizationChangeCalls {
		if _, err := core.Describe(application.DescribeRequest{Operation: call.id, Connection: "orgc-unlisted"}); !errors.As(err, &unsupported) {
			t.Errorf("describe %s without a tools list = %v, want unsupported", call.id, err)
		}
		if _, err := invoke(t, core, call.id, "orgc-unlisted", withOrganizationChangeOwner(call.arguments), true); !errors.As(err, &unsupported) {
			t.Errorf("invoke %s without a tools list = %v, want unsupported", call.id, err)
		}
		unconfirmed := &application.ConfirmationRequiredError{}
		if _, err := invoke(t, core, call.id, "orgc-listed", withOrganizationChangeOwner(call.arguments), false); !errors.As(err, &unconfirmed) {
			t.Errorf("invoke %s without confirmation = %v, want confirmation-required", call.id, err)
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}
