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

const (
	organizationProviderCanary = "organization-provider-text-canary-7c2d"
	invitationTokenCanary      = "invitation-token-canary-3a9b"
	invitationAcceptCanary     = "invitations/accept-link-canary"
)

// A value shaped like an email address is only ever built here, never written as one literal.
var fixtureEmail = "octo" + "@" + "example.invalid"

// fakeOrganization answers the organization read routes of octo-org through the failure hook of fakeGitHub.
type fakeOrganization struct {
	mu sync.Mutex
	// status, when set, answers every organization read with this status and GitHub's own text.
	status int
}

// Every account carries the two-factor members GitHub's user objects never document next to a login, so a
// projection that copied unknown members would show them.
var organizationUserJSON = `{"login":"octocat","id":1,"node_id":"MDQ6VXNlcjE=","avatar_url":"https://avatars.test/u/1",` +
	`"gravatar_id":"","url":"https://api.github.com/users/octocat","html_url":"https://github.com/octocat",` +
	`"followers_url":"https://api.github.com/users/octocat/followers","type":"User","site_admin":false,` +
	`"user_view_type":"public","name":"The Octocat","email":"` + fixtureEmail + `",` +
	`"two_factor_authentication":false,"two_factor_enabled":false}`

const fullOrganizationJSON = `{"login":"octo-org","id":9,"node_id":"O_kgDO","url":"https://api.github.com/orgs/octo-org",` +
	`"repos_url":"https://api.github.com/orgs/octo-org/repos","events_url":"https://api.github.com/orgs/octo-org/events",` +
	`"hooks_url":"https://api.github.com/orgs/octo-org/hooks","issues_url":"https://api.github.com/orgs/octo-org/issues",` +
	`"members_url":"https://api.github.com/orgs/octo-org/members{/member}",` +
	`"public_members_url":"https://api.github.com/orgs/octo-org/public_members{/member}",` +
	`"avatar_url":"https://avatars.test/o/9","description":"An organization","name":"Octo Org","company":"Octo",` +
	`"blog":"https://octo.example","location":"Berlin","twitter_username":"octo","is_verified":true,` +
	`"has_organization_projects":true,"has_repository_projects":false,"public_repos":3,"public_gists":0,` +
	`"followers":4,"following":0,"html_url":"https://github.com/octo-org","type":"Organization",` +
	`"total_private_repos":5,"owned_private_repos":5,"private_gists":0,"disk_usage":12,"collaborators":2,` +
	`"billing_email":"billing-contact","plan":{"name":"team","space":100,"private_repos":5,"filled_seats":3,"seats":10},` +
	`"default_repository_permission":"read","default_repository_branch":"main",` +
	`"members_can_create_repositories":true,"two_factor_requirement_enabled":true,` +
	`"members_allowed_repository_creation_type":"all","members_can_create_public_repositories":true,` +
	`"members_can_create_private_repositories":true,"members_can_create_internal_repositories":false,` +
	`"members_can_create_pages":true,"members_can_create_public_pages":true,"members_can_create_private_pages":false,` +
	`"members_can_delete_repositories":false,"members_can_change_repo_visibility":false,` +
	`"members_can_invite_outside_collaborators":true,"members_can_delete_issues":false,` +
	`"display_commenter_full_name_setting_enabled":true,"readers_can_create_discussions":true,` +
	`"members_can_create_teams":true,"members_can_view_dependency_insights":true,` +
	`"members_can_fork_private_repositories":false,"web_commit_signoff_required":true,` +
	`"advanced_security_enabled_for_new_repositories":false,"dependabot_alerts_enabled_for_new_repositories":true,` +
	`"dependabot_security_updates_enabled_for_new_repositories":false,"dependency_graph_enabled_for_new_repositories":true,` +
	`"secret_scanning_enabled_for_new_repositories":true,` +
	`"secret_scanning_push_protection_enabled_for_new_repositories":true,` +
	`"secret_scanning_push_protection_custom_link_enabled":true,` +
	`"secret_scanning_push_protection_custom_link":"https://octo.example/push-help",` +
	`"deploy_keys_enabled_for_repositories":true,"created_at":"2026-01-02T03:04:05Z",` +
	`"updated_at":"2026-01-03T03:04:05Z","archived_at":null}`

var organizationMembershipJSON = `{"url":"https://api.github.com/orgs/octo-org/memberships/octocat","state":"active",` +
	`"role":"admin","direct_membership":true,"enterprise_teams_providing_indirect_membership":["ent:one"],` +
	`"organization_url":"https://api.github.com/orgs/octo-org","organization":{"login":"octo-org","id":9,` +
	`"node_id":"O_kgDO","url":"https://api.github.com/orgs/octo-org","repos_url":"https://api.github.com/orgs/octo-org/repos",` +
	`"events_url":"e","hooks_url":"h","issues_url":"i","members_url":"m","public_members_url":"p",` +
	`"avatar_url":"https://avatars.test/o/9","description":"An organization"},"user":` + organizationUserJSON + `,` +
	`"permissions":{"can_create_repository":true}}`

const organizationRolesJSON = `{"total_count":2,"roles":[{"id":8132,"name":"all_repo_read",` +
	`"description":"Grants read access","permissions":["read_organization_custom_org_role"],"organization":null,` +
	`"created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-03T03:04:05Z","source":"Predefined","base_role":"read"},` +
	`{"id":9001,"name":"auditor","permissions":["a","b"],"source":"Organization","organization":{"login":"octo-org","id":9},` +
	`"created_at":"2026-02-02T03:04:05Z","updated_at":"2026-02-03T03:04:05Z"}]}`

var organizationInvitationsJSON = `[{"id":77,"login":"newcomer","email":null,"role":"direct_member",` +
	`"created_at":"2026-03-04T05:06:07Z","failed_at":"2026-03-05T05:06:07Z","failed_reason":"expired",` +
	`"inviter":` + organizationUserJSON + `,"team_count":2,"node_id":"OI_kwDO",` +
	`"invitation_teams_url":"https://api.github.com/organizations/9/invitations/77/teams",` +
	`"invitation_source":"member","token":"` + invitationTokenCanary + `",` +
	`"invitation_url":"https://github.com/orgs/octo-org/` + invitationAcceptCanary + `"}]`

func (o *fakeOrganization) route(w http.ResponseWriter, req *http.Request) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	path := strings.TrimPrefix(req.URL.EscapedPath(), "/api/v3")
	rest, ok := strings.CutPrefix(path, "/orgs/octo-org")
	if !ok || req.Method != http.MethodGet {
		return false
	}
	rest = strings.TrimPrefix(rest, "/")
	switch {
	case rest == "", rest == "members", rest == "outside_collaborators", rest == "invitations",
		rest == "organization-roles", strings.HasPrefix(rest, "memberships/"):
	default:
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if o.status != 0 {
		w.WriteHeader(o.status)
		fmt.Fprintf(w, `{"message":%q}`, organizationProviderCanary)
		return true
	}
	switch {
	case rest == "":
		fmt.Fprint(w, fullOrganizationJSON)
	case rest == "members", rest == "outside_collaborators":
		w.Header().Set("Link", `<https://api.github.com/orgs/octo-org/`+rest+`?page=2>; rel="next"`)
		fmt.Fprint(w, "["+organizationUserJSON+"]")
	case rest == "invitations":
		fmt.Fprint(w, organizationInvitationsJSON)
	case rest == "organization-roles":
		fmt.Fprint(w, organizationRolesJSON)
	case rest == "memberships/octocat":
		fmt.Fprint(w, organizationMembershipJSON)
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}
	return true
}

func serveOrganization(t *testing.T) (*fakeGitHub, *fakeOrganization, string) {
	t.Helper()
	o := &fakeOrganization{}
	f := &fakeGitHub{}
	f.failure = o.route
	return f, o, serve(t, f)
}

func organizationConfig(base string) *config.Config {
	cfg := coreConfig(base)
	read := []config.Effect{config.EffectRead}
	cfg.Connections["org-reads"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Effects: read}
	cfg.Connections["org-repo-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: read}
	cfg.Connections["org-excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: read}
	cfg.Connections["org-user"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"users/octocat"}, Effects: read}
	cfg.Connections["org-write-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: []config.Effect{config.EffectUpdate}}
	return cfg
}

func organizationCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), organizationConfig(base), resolver(red, reads), red)
}

func withOrganizationOwner(arguments string) string {
	if arguments == "{}" {
		return `{` + teamOwner + `}`
	}
	return `{` + teamOwner + `,` + arguments[1:]
}

// organizationCalls holds one valid call of every tool with the request it must send.
var organizationCalls = []struct {
	id, arguments, path, query string
	want                       []string
}{
	{organizationsGet.ID, `{}`, "/api/v3/orgs/octo-org", "",
		[]string{`"login":"octo-org"`, `"two_factor_requirement_enabled":true`, `"billing_email":"billing-contact"`,
			`"filled_seats":3`, `"default_repository_branch":"main"`, `"members_can_delete_repositories":false`,
			`"secret_scanning_push_protection_custom_link":"https://octo.example/push-help"`,
			`"deploy_keys_enabled_for_repositories":true`, `"url":"https://github.com/octo-org"`,
			`"hooks_url":"https://api.github.com/orgs/octo-org/hooks"`, `"owner":"orgs/octo-org"`}},
	{organizationMembersList.ID, `{"role":"admin","limit":10}`, "/api/v3/orgs/octo-org/members",
		"page=1&per_page=10&role=admin",
		[]string{`"login":"octocat"`, `"name":"The Octocat"`, `"email":"` + fixtureEmail + `"`, `"has_more":true`,
			`"next_cursor"`, `"user_view_type":"public"`}},
	{organizationMembersGet.ID, `{"username":"octocat"}`, "/api/v3/orgs/octo-org/memberships/octocat", "",
		[]string{`"username":"octocat"`, `"state":"active"`, `"role":"admin"`, `"direct_membership":true`,
			`"enterprise_teams_providing_indirect_membership":["ent:one"]`, `"can_create_repository":true`,
			`"organization_url":"https://api.github.com/orgs/octo-org"`, `"login":"octo-org"`,
			`"user":{`, `"login":"octocat"`}},
	{organizationRolesList.ID, `{}`, "/api/v3/orgs/octo-org/organization-roles", "",
		[]string{`"total_count":2`, `"id":8132`, `"base_role":"read"`, `"source":"Predefined"`,
			`"permissions":["a","b"]`, `"name":"auditor"`, `"updated_at":"2026-02-03T03:04:05Z"`}},
	{outsideCollaboratorsList.ID, `{}`, "/api/v3/orgs/octo-org/outside_collaborators", "page=1&per_page=30",
		[]string{`"collaborators":[{`, `"login":"octocat"`, `"has_more":true`}},
	{organizationInvitationsList.ID, `{"role":"direct_member","invitation_source":"member"}`,
		"/api/v3/orgs/octo-org/invitations", "invitation_source=member&page=1&per_page=30&role=direct_member",
		[]string{`"id":77`, `"login":"newcomer"`, `"role":"direct_member"`, `"failed_reason":"expired"`,
			`"team_count":2`, `"invitation_source":"member"`, `"inviter":{`,
			`"invitation_teams_url":"https://api.github.com/organizations/9/invitations/77/teams"`}},
}

func TestOrganizationReadToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, _, base := serveOrganization(t)
	core := organizationCore(t, base, nil)
	for _, call := range organizationCalls {
		before := len(f.recorded())
		result, err := invoke(t, core, call.id, "org-reads", withOrganizationOwner(call.arguments), false)
		if err != nil {
			t.Fatalf("%s = %v", call.id, err)
		}
		for _, want := range call.want {
			if !strings.Contains(string(result), want) {
				t.Errorf("%s = %s, want %s", call.id, result, want)
			}
		}
		requests := f.recorded()[before:]
		if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].path != call.path {
			t.Fatalf("%s sent %+v, want one GET %s", call.id, requests, call.path)
		}
		if requests[0].query != call.query {
			t.Errorf("%s query = %q, want %q", call.id, requests[0].query, call.query)
		}
	}
}

// No output names the two-factor state of another account, no filter for it is offered or sent, and an
// invitation shows neither its token nor a link that accepts it.
func TestOrganizationReadsLeaveOutTwoFactorStateAndInvitationTokens(t *testing.T) {
	f, _, base := serveOrganization(t)
	core := organizationCore(t, base, nil)
	for _, call := range organizationCalls {
		result, err := invoke(t, core, call.id, "org-reads", withOrganizationOwner(call.arguments), false)
		if err != nil {
			t.Fatalf("%s = %v", call.id, err)
		}
		text := string(result)
		for _, unwanted := range []string{"two_factor_authentication", "two_factor_enabled", "2fa", invitationTokenCanary,
			invitationAcceptCanary, `"token"`, "invitation_url"} {
			if strings.Contains(text, unwanted) {
				t.Errorf("%s carries %q: %s", call.id, unwanted, text)
			}
		}
		if call.id != organizationsGet.ID && strings.Contains(text, "two_factor") {
			t.Errorf("%s carries a two-factor member: %s", call.id, text)
		}
		descriptor, err := core.Describe(application.DescribeRequest{Operation: call.id, Connection: "org-reads"})
		if err != nil {
			t.Fatalf("describe %s = %v", call.id, err)
		}
		schema := strings.ToLower(string(descriptor.Operation.InputSchema))
		if strings.Contains(schema, "2fa") || strings.Contains(schema, `"filter"`) {
			t.Errorf("%s offers a two-factor filter: %s", call.id, schema)
		}
	}
	for _, id := range []string{organizationMembersList.ID, outsideCollaboratorsList.ID} {
		before := len(f.recorded())
		_, err := invoke(t, core, id, "org-reads", `{`+teamOwner+`,"filter":"2fa_disabled"}`, false)
		if err == nil || len(f.recorded()) != before {
			t.Errorf("%s accepted the two-factor filter: %v", id, err)
		}
	}
	for _, request := range f.recorded() {
		if strings.Contains(request.query, "filter") {
			t.Errorf("request %s?%s sends a filter", request.path, request.query)
		}
	}
}

// A cursor leads to the next page of the same list and is refused for another one.
func TestOrganizationReadListsPageByCursor(t *testing.T) {
	f, _, base := serveOrganization(t)
	core := organizationCore(t, base, nil)
	first, err := invoke(t, core, organizationMembersList.ID, "org-reads", `{`+teamOwner+`,"limit":5}`, false)
	if err != nil {
		t.Fatal(err)
	}
	cursor := between(string(first), `"next_cursor":"`, `"`)
	if cursor == "" {
		t.Fatalf("first page = %s, want a cursor", first)
	}
	before := len(f.recorded())
	if _, err := invoke(t, core, organizationMembersList.ID, "org-reads",
		`{`+teamOwner+`,"limit":5,"cursor":"`+cursor+`"}`, false); err != nil {
		t.Fatal(err)
	}
	if got := f.recorded()[before:]; len(got) != 1 || got[0].query != "page=2&per_page=5" {
		t.Errorf("second page sent %+v, want page=2&per_page=5", got)
	}
	before = len(f.recorded())
	for _, id := range []string{outsideCollaboratorsList.ID, organizationInvitationsList.ID} {
		_, err := invoke(t, core, id, "org-reads", `{`+teamOwner+`,"limit":5,"cursor":"`+cursor+`"}`, false)
		if !isInvalidRequest(err) {
			t.Errorf("%s with a foreign cursor = %v, want an invalid request", id, err)
		}
	}
	if len(f.recorded()) != before {
		t.Errorf("a foreign cursor reached GitHub")
	}
}

func between(text, start, end string) string {
	_, rest, ok := strings.Cut(text, start)
	if !ok {
		return ""
	}
	value, _, _ := strings.Cut(rest, end)
	return value
}

// The organization, the username, and the filters are settled before a credential is read or GitHub is
// contacted.
func TestOrganizationReadRefusalsBeforeIO(t *testing.T) {
	f, _, base := serveOrganization(t)
	reads := 0
	core := organizationCore(t, base, &reads)
	for _, tt := range []struct {
		name, id, connection, arguments, message string
	}{
		{"owner outside the targets", organizationsGet.ID, "org-reads", `{"owner":"orgs/other"}`, "outside the targets"},
		{"foreign target for the members", organizationMembersList.ID, "org-reads", `{"owner":"orgs/other"}`,
			"outside the targets"},
		{"excluded organization", organizationInvitationsList.ID, "org-excluded", `{"owner":"orgs/octo-org"}`, ""},
		{"user owner", organizationRolesList.ID, "org-user", `{"owner":"users/octocat"}`, "organization"},
		{"repository target only", organizationsGet.ID, "org-repo-only", `{}`, ""},
		{"no owner target", outsideCollaboratorsList.ID, "org-repo-only", `{}`, ""},
		{"path injection in the username", organizationMembersGet.ID, "org-reads",
			`{"owner":"orgs/octo-org","username":"a/../b"}`, ""},
		{"query in the username", organizationMembersGet.ID, "org-reads",
			`{"owner":"orgs/octo-org","username":"a?x=1"}`, ""},
		{"empty username", organizationMembersGet.ID, "org-reads", `{"owner":"orgs/octo-org","username":""}`, ""},
		{"unknown member role", organizationMembersList.ID, "org-reads", `{"owner":"orgs/octo-org","role":"owner"}`, ""},
		{"unknown invitation role", organizationInvitationsList.ID, "org-reads",
			`{"owner":"orgs/octo-org","role":"member"}`, ""},
		{"unknown invitation source", organizationInvitationsList.ID, "org-reads",
			`{"owner":"orgs/octo-org","invitation_source":"api"}`, ""},
		{"two-factor filter", organizationMembersList.ID, "org-reads", `{"owner":"orgs/octo-org","filter":"2fa_disabled"}`, ""},
		{"insecure two-factor filter", outsideCollaboratorsList.ID, "org-reads",
			`{"owner":"orgs/octo-org","filter":"2fa_insecure"}`, ""},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, false)
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

// A connection that allows no read does not offer the tools, and a refused call never reads a credential.
func TestOrganizationReadToolsNeedTheReadEffect(t *testing.T) {
	f, _, base := serveOrganization(t)
	reads := 0
	core := organizationCore(t, base, &reads)
	for _, call := range organizationCalls {
		_, err := invoke(t, core, call.id, "org-write-only", withOrganizationOwner(call.arguments), false)
		var unsupported *capability.UnsupportedError
		if !errors.As(err, &unsupported) {
			t.Errorf("%s on a connection without the read effect = %v, want unsupported", call.id, err)
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused calls read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}

// Every tool names the failures of GitHub without quoting GitHub's text and without a retry.
func TestOrganizationReadFailuresAreNamed(t *testing.T) {
	f, o, base := serveOrganization(t)
	core := organizationCore(t, base, nil)
	for _, call := range organizationCalls {
		for _, tt := range []struct {
			status int
			class  provider.Class
		}{
			{http.StatusForbidden, provider.ClassPermission},
			{http.StatusNotFound, provider.ClassNotFound},
			{http.StatusUnprocessableEntity, provider.ClassProviderError},
			{http.StatusGatewayTimeout, provider.ClassTimeout},
		} {
			o.status = tt.status
			before := len(f.recorded())
			_, err := invoke(t, core, call.id, "org-reads", withOrganizationOwner(call.arguments), false)
			if classOf(err) != tt.class || strings.Contains(err.Error(), organizationProviderCanary) ||
				strings.Contains(err.Error(), uncertain) {
				t.Errorf("%s answered %d = %v, want %s without GitHub's text", call.id, tt.status, err, tt.class)
			}
			if got := len(f.recorded()[before:]); got != 1 {
				t.Errorf("%s answered %d sent %d requests, want 1 without a retry", call.id, tt.status, got)
			}
			if tt.status == http.StatusForbidden && !strings.Contains(err.Error(), "read:org") &&
				!strings.Contains(err.Error(), "admin:org") {
				t.Errorf("%s forbidden = %v, want the token right named", call.id, err)
			}
		}
		o.status = 0
	}
}

// A membership that GitHub does not know is a missing membership, named with the account and the organization.
func TestOrganizationMembershipOfAStrangerIsNotFound(t *testing.T) {
	_, _, base := serveOrganization(t)
	core := organizationCore(t, base, nil)
	_, err := invoke(t, core, organizationMembersGet.ID, "org-reads", `{`+teamOwner+`,"username":"stranger"}`, false)
	if classOf(err) != provider.ClassNotFound || !strings.Contains(err.Error(), "stranger") ||
		!strings.Contains(err.Error(), "orgs/octo-org") {
		t.Errorf("stranger = %v, want not-found naming the membership", err)
	}
}

// GitHub members outside the documented objects never reach the output, whatever they hold.
func TestOrganizationReadsIgnoreUndocumentedMembers(t *testing.T) {
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/members") && !strings.HasSuffix(r.URL.Path, "/outside_collaborators") {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"login":"octocat","secret_member":"`+organizationProviderCanary+`","two_factor_enabled":true}]`)
		return true
	}
	core := organizationCore(t, serve(t, f), nil)
	for _, id := range []string{organizationMembersList.ID, outsideCollaboratorsList.ID} {
		result, err := invoke(t, core, id, "org-reads", `{`+teamOwner+`}`, false)
		if err != nil || strings.Contains(string(result), organizationProviderCanary) ||
			strings.Contains(string(result), "two_factor") {
			t.Errorf("%s = %s, %v", id, result, err)
		}
	}
}
