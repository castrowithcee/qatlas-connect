package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/redact"
)

const (
	environmentsRoute = "/api/v3/repos/octo-org/example/environments"
	providerCanary    = "provider-text-canary-7d2c"
)

// fakeEnvironment is one environment of the fake repository; reviewers hold GitHub's type and identifier.
type fakeEnvironment struct {
	name              string
	waitTimer         float64
	preventSelfReview bool
	reviewers         []map[string]any
	policy            map[string]any
	branchPolicies    []fakeBranchPolicy
}

type fakeBranchPolicy struct {
	id         int64
	name, kind string
}

// fakeEnvironments answers the environment, deployment branch policy, user, and team routes through the
// failure hook of fakeGitHub. rawPaths keeps the escaped path of every environment request, since the
// recorded path is decoded.
type fakeEnvironments struct {
	mu       sync.Mutex
	f        *fakeGitHub
	envs     map[string]*fakeEnvironment
	order    []string
	nextID   int64
	rawPaths []string
	// fail, when set, answers a request before the fake does, for a refusal or an unclear outcome.
	fail func(http.ResponseWriter, *http.Request) bool
}

func serveEnvironments(t *testing.T) (*fakeGitHub, *fakeEnvironments, string) {
	t.Helper()
	e := &fakeEnvironments{nextID: 361471, envs: map[string]*fakeEnvironment{
		"production": {name: "production", waitTimer: 30, preventSelfReview: true,
			reviewers: []map[string]any{{"type": "User", "id": float64(1)}, {"type": "Team", "id": float64(42)}},
			policy:    map[string]any{"protected_branches": false, "custom_branch_policies": true},
			branchPolicies: []fakeBranchPolicy{{id: 361470, name: "release/*", kind: "branch"},
				{id: 361469, name: "v*", kind: "tag"}}},
		"prod/eu west": {name: "prod/eu west"},
	}, order: []string{"production", "prod/eu west"}}
	f := &fakeGitHub{}
	e.f = f
	f.failure = e.route
	return f, e, serve(t, f)
}

func simpleUserJSON(login string, id int64, kind string) string {
	base := "https://api.github.com/users/" + login
	return fmt.Sprintf(`{"login":%q,"id":%d,"node_id":"MDQ6VXNlcjE=","avatar_url":"https://avatars.example/u/%d",`+
		`"gravatar_id":"","url":%q,"html_url":"https://github.com/%s","followers_url":"%s/followers",`+
		`"following_url":"%s/following{/other_user}","gists_url":"%s/gists{/gist_id}",`+
		`"starred_url":"%s/starred{/owner}{/repo}","subscriptions_url":"%s/subscriptions",`+
		`"organizations_url":"%s/orgs","repos_url":"%s/repos","events_url":"%s/events{/privacy}",`+
		`"received_events_url":"%s/received_events","type":%q,"site_admin":false,"name":"Mona Octocat",`+
		`"email":"octocat@example.com","starred_at":"2026-07-09T00:17:55Z","user_view_type":"public"}`,
		login, id, id, base, login, base, base, base, base, base, base, base, base, base, kind)
}

const teamFields = `"node_id":"MDQ6VGVhbTQy","url":"https://api.github.com/teams/42",` +
	`"html_url":"https://github.com/orgs/octo-org/teams/release-managers","name":"Release managers",` +
	`"slug":"release-managers","description":"Approve releases","privacy":"closed",` +
	`"notification_setting":"notifications_enabled","permission":"push","members_url":` +
	`"https://api.github.com/teams/42/members{/member}","repositories_url":"https://api.github.com/teams/42/repos",` +
	`"type":"organization","organization_id":7,"enterprise_id":8`

const teamJSON = `{"id":42,` + teamFields + `,"access_source":"direct","permissions":{"pull":true,"triage":true,` +
	`"push":true,"maintain":false,"admin":false},"parent":{"id":2,"node_id":"MDQ6VGVhbTI=",` +
	`"url":"https://api.github.com/teams/2","members_url":"https://api.github.com/teams/2/members{/member}",` +
	`"name":"Engineering","description":"All engineers","permission":"pull","privacy":"closed",` +
	`"notification_setting":"notifications_enabled","html_url":"https://github.com/orgs/octo-org/teams/eng",` +
	`"repositories_url":"https://api.github.com/teams/2/repos","slug":"eng",` +
	`"ldap_dn":"cn=eng,ou=teams,dc=example,dc=com","type":"organization","organization_id":7,"enterprise_id":8}}`

// environmentJSON answers an environment with every member GitHub documents for it.
func environmentJSON(e *fakeEnvironment) string {
	escaped := url.PathEscape(e.name)
	rules := []string{}
	if e.waitTimer > 0 {
		rules = append(rules, fmt.Sprintf(`{"id":3736,"node_id":"MDQ6R2F0ZTM3MzY=","type":"wait_timer","wait_timer":%d}`,
			int(e.waitTimer)))
	}
	if len(e.reviewers) > 0 {
		reviewers := []string{}
		for _, reviewer := range e.reviewers {
			if reviewer["type"] == "Team" {
				reviewers = append(reviewers, `{"type":"Team","reviewer":`+teamJSON+`}`)
				continue
			}
			reviewers = append(reviewers, `{"type":"User","reviewer":`+simpleUserJSON("octocat", 1, "User")+`}`)
		}
		rules = append(rules, fmt.Sprintf(`{"id":3755,"node_id":"MDQ6R2F0ZTM3NTU=","prevent_self_review":%t,`+
			`"type":"required_reviewers","reviewers":[%s]}`, e.preventSelfReview, strings.Join(reviewers, ",")))
	}
	policy := "null"
	if e.policy != nil {
		encoded, _ := json.Marshal(e.policy)
		policy = string(encoded)
		rules = append(rules, `{"id":3756,"node_id":"MDQ6R2F0ZTM3NTY=","type":"branch_policy"}`)
	}
	name, _ := json.Marshal(e.name)
	return fmt.Sprintf(`{"id":161088068,"node_id":"MDExOkVudmlyb25tZW50MTYxMDg4MDY4","name":%s,`+
		`"url":"https://api.github.com/repos/octo-org/example/environments/%s",`+
		`"html_url":"https://github.com/octo-org/example/deployments/activity_log?environments_filter=%s",`+
		`"created_at":"2026-01-23T22:00:40Z","updated_at":"2026-01-24T22:00:40Z","protection_rules":[%s],`+
		`"deployment_branch_policy":%s}`, name, escaped, escaped, strings.Join(rules, ","), policy)
}

func branchPolicyJSON(p fakeBranchPolicy) string {
	return fmt.Sprintf(`{"id":%d,"node_id":"MDE2OkdhdGVCcmFuY2hQb2xpY3kxNzY5NTUwMQ==","name":%q,"type":%q}`,
		p.id, p.name, p.kind)
}

func notFoundAnswer(w http.ResponseWriter) bool {
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprint(w, `{"message":"Not Found `+providerCanary+`"}`)
	return true
}

func (e *fakeEnvironments) route(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.EscapedPath()
	isUser := strings.HasPrefix(path, "/api/v3/users/")
	isTeam := strings.HasPrefix(path, "/api/v3/orgs/")
	if !strings.HasPrefix(path, environmentsRoute) && !isUser && !isTeam {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rawPaths = append(e.rawPaths, r.Method+" "+path)
	if e.fail != nil && e.fail(w, r) {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case isUser:
		switch strings.TrimPrefix(path, "/api/v3/users/") {
		case "octocat":
			fmt.Fprint(w, simpleUserJSON("octocat", 1, "User"))
		case "octo-org":
			fmt.Fprint(w, simpleUserJSON("octo-org", 9919, "Organization"))
		default:
			return notFoundAnswer(w)
		}
		return true
	case isTeam:
		if path != "/api/v3/orgs/octo-org/teams/release-managers" {
			return notFoundAnswer(w)
		}
		fmt.Fprint(w, teamJSON)
		return true
	}
	var body map[string]any
	if requests := e.f.recorded(); r.Method != http.MethodGet && len(requests) > 0 {
		body = requests[len(requests)-1].body
	}
	segments := strings.Split(strings.TrimPrefix(strings.TrimPrefix(path, environmentsRoute), "/"), "/")
	if segments[0] == "" {
		if r.Method != http.MethodGet {
			return false
		}
		entries := []string{}
		for _, key := range e.order {
			entries = append(entries, environmentJSON(e.envs[key]))
		}
		w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
		fmt.Fprintf(w, `{"total_count":%d,"environments":[%s]}`, len(entries), strings.Join(entries, ","))
		return true
	}
	name, err := url.PathUnescape(segments[0])
	if err != nil {
		return notFoundAnswer(w)
	}
	key := strings.ToLower(name)
	env := e.envs[key]
	if len(segments) == 1 {
		switch r.Method {
		case http.MethodGet:
			if env == nil {
				return notFoundAnswer(w)
			}
			fmt.Fprint(w, environmentJSON(env))
		case http.MethodPut:
			if env == nil {
				env = &fakeEnvironment{name: name}
				e.envs[key] = env
				e.order = append(e.order, key)
			}
			env.waitTimer, _ = body["wait_timer"].(float64)
			env.preventSelfReview, _ = body["prevent_self_review"].(bool)
			env.reviewers = nil
			list, _ := body["reviewers"].([]any)
			for _, item := range list {
				reviewer, _ := item.(map[string]any)
				env.reviewers = append(env.reviewers, reviewer)
			}
			env.policy, _ = body["deployment_branch_policy"].(map[string]any)
			fmt.Fprint(w, environmentJSON(env))
		case http.MethodDelete:
			if env == nil {
				return notFoundAnswer(w)
			}
			delete(e.envs, key)
			for i, k := range e.order {
				if k == key {
					e.order = append(e.order[:i], e.order[i+1:]...)
					break
				}
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			return false
		}
		return true
	}
	if segments[1] != "deployment-branch-policies" || env == nil ||
		env.policy == nil || env.policy["custom_branch_policies"] != true {
		return notFoundAnswer(w)
	}
	if len(segments) == 2 {
		switch r.Method {
		case http.MethodGet:
			entries := []string{}
			for _, p := range env.branchPolicies {
				entries = append(entries, branchPolicyJSON(p))
			}
			fmt.Fprintf(w, `{"total_count":%d,"branch_policies":[%s]}`, len(entries), strings.Join(entries, ","))
		case http.MethodPost:
			pattern, _ := body["name"].(string)
			kind, _ := body["type"].(string)
			if kind == "" {
				kind = "branch"
			}
			for _, p := range env.branchPolicies {
				if p.name == pattern && p.kind == kind {
					w.Header().Set("Location", "https://api.github.com/x")
					w.WriteHeader(http.StatusSeeOther)
					return true
				}
			}
			created := fakeBranchPolicy{id: e.nextID, name: pattern, kind: kind}
			e.nextID++
			env.branchPolicies = append(env.branchPolicies, created)
			fmt.Fprint(w, branchPolicyJSON(created))
		default:
			return false
		}
		return true
	}
	id, _ := strconv.ParseInt(segments[2], 10, 64)
	for i, p := range env.branchPolicies {
		if p.id != id {
			continue
		}
		switch r.Method {
		case http.MethodPut:
			env.branchPolicies[i].name, _ = body["name"].(string)
			fmt.Fprint(w, branchPolicyJSON(env.branchPolicies[i]))
		case http.MethodDelete:
			env.branchPolicies = append(env.branchPolicies[:i], env.branchPolicies[i+1:]...)
			w.WriteHeader(http.StatusNoContent)
		default:
			return false
		}
		return true
	}
	return notFoundAnswer(w)
}

// mutations counts the PUT, POST, and DELETE requests of the environment routes.
func (e *fakeEnvironments) mutations() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, request := range e.rawPaths {
		if !strings.HasPrefix(request, http.MethodGet+" ") {
			n++
		}
	}
	return n
}

var environmentToolIDs = []string{environmentsList.ID, environmentsGet.ID, environmentsSet.ID, environmentsDelete.ID,
	branchPoliciesList.ID, branchPoliciesCreate.ID, branchPoliciesUpdate.ID, branchPoliciesDelete.ID}

// environmentsConfig binds a connection with every effect and no tools list, one that lists every environment
// tool, one whose only target is another repository, and one that excludes the repository.
func environmentsConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: environmentToolIDs}
	cfg.Connections["foreign"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Target: "repos/octo-org/other", Effects: all, Tools: environmentToolIDs}
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: all,
		Tools: environmentToolIDs, Exclude: []string{repoTarget, "users/blocked"}}
	return cfg
}

const setProduction = `{"environment":"production","wait_timer":15,"prevent_self_review":true,` +
	`"reviewers":[{"type":"User","name":"octocat"},{"type":"Team","name":"release-managers"}],` +
	`"deployment_branch_policy":"custom"}`

// Every environment and deployment branch policy tool satisfies its output contract through the application
// core, a read answers every member GitHub documents, a name with a slash and a space stays one path segment,
// and the listed-only tools are sent exactly once each.
func TestEnvironmentToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, e, base := serveEnvironments(t)
	red := &redact.Redactor{}
	reg := registry(t)
	core := application.New(reg, environmentsConfig(base), resolver(red, nil), red)

	listed, err := invoke(t, core, environmentsList.ID, "listed", `{}`, false)
	var list EnvironmentList
	if err != nil || json.Unmarshal(listed, &list) != nil || len(list.Environments) != 2 || !list.HasMore ||
		list.TotalCount == nil || *list.TotalCount != 2 || !strings.Contains(string(listed), `"repository":"octo-org/example"`) {
		t.Fatalf("list = %s, %v", listed, err)
	}

	got, err := invoke(t, core, environmentsGet.ID, "listed", `{"environment":"production"}`, false)
	if err != nil {
		t.Fatalf("get = %v", err)
	}
	descriptor, _, _ := reg.Lookup(environmentsGet.ID)
	assertDocumentedMembers(t, got, descriptor.OutputSchema, []string{"name", "id", "node_id", "url", "created_at",
		"updated_at", "protection_rules", "type", "wait_timer", "prevent_self_review", "reviewers", "user", "team",
		"login", "avatar_url", "gravatar_id", "site_admin", "email", "starred_at", "user_view_type", "followers_url",
		"received_events_url", "slug", "description", "privacy", "notification_setting", "permission",
		"permissions", "members_url", "repositories_url", "organization_id", "enterprise_id", "access_source",
		"parent", "ldap_dn", "deployment_branch_policy", "protected_branches", "custom_branch_policies", "links"})

	spaced, err := invoke(t, core, environmentsGet.ID, "listed", `{"environment":"prod/eu west"}`, false)
	if err != nil || !strings.Contains(string(spaced), `"name":"prod/eu west"`) {
		t.Fatalf("get with a slash and a space = %s, %v", spaced, err)
	}
	if last := e.rawPaths[len(e.rawPaths)-1]; last != "GET "+environmentsRoute+"/prod%2Feu%20west" {
		t.Errorf("escaped route = %q, want the name as one segment", last)
	}

	// set resolves both reviewers, then sends one PUT with every setting and GitHub's identifiers.
	before := len(e.rawPaths)
	set, err := invoke(t, core, environmentsSet.ID, "listed",
		strings.Replace(setProduction, "production", "staging", 1), true)
	if err != nil || !strings.Contains(string(set), `"name":"staging"`) {
		t.Fatalf("set = %s, %v", set, err)
	}
	if paths := e.rawPaths[before:]; len(paths) != 3 || paths[0] != "GET /api/v3/users/octocat" ||
		paths[1] != "GET /api/v3/orgs/octo-org/teams/release-managers" || paths[2] != "PUT "+environmentsRoute+"/staging" {
		t.Errorf("set requests = %v, want two reviewer reads and one PUT", paths)
	}
	requests := f.recorded()
	body, _ := json.Marshal(requests[len(requests)-1].body)
	if want := `{"deployment_branch_policy":{"custom_branch_policies":true,"protected_branches":false},` +
		`"prevent_self_review":true,"reviewers":[{"id":1,"type":"User"},{"id":42,"type":"Team"}],"wait_timer":15}`; string(body) != want {
		t.Errorf("set body = %s, want %s", body, want)
	}
	cleared, err := invoke(t, core, environmentsSet.ID, "listed", `{"environment":"staging","wait_timer":0,`+
		`"prevent_self_review":false,"reviewers":[],"deployment_branch_policy":"all"}`, true)
	requests = f.recorded()
	body, _ = json.Marshal(requests[len(requests)-1].body)
	if err != nil || strings.Contains(string(cleared), "protection_rules") ||
		string(body) != `{"deployment_branch_policy":null,"prevent_self_review":false,"reviewers":[],"wait_timer":0}` {
		t.Errorf("clearing set = %s (body %s), %v", cleared, body, err)
	}

	policies, err := invoke(t, core, branchPoliciesList.ID, "listed", `{"environment":"production"}`, false)
	if err != nil || !strings.Contains(string(policies), `"name":"release/*"`) ||
		!strings.Contains(string(policies), `"type":"tag"`) || !strings.Contains(string(policies), `"total_count":2`) ||
		!strings.Contains(string(policies), `"environment":"production"`) {
		t.Fatalf("policy list = %s, %v", policies, err)
	}
	created, err := invoke(t, core, branchPoliciesCreate.ID, "listed",
		`{"environment":"production","name":"hotfix/*"}`, true)
	if err != nil || !strings.Contains(string(created), `"id":361471`) || !strings.Contains(string(created), `"type":"branch"`) {
		t.Fatalf("policy create = %s, %v", created, err)
	}
	// A pattern the environment already holds is answered 303, never followed, and refused once.
	before = e.mutations()
	if _, err := invoke(t, core, branchPoliciesCreate.ID, "listed", `{"environment":"production","name":"hotfix/*"}`,
		true); !isInvalidRequest(err) || !strings.Contains(err.Error(), "already holds") {
		t.Errorf("repeated policy create = %v, want an invalid request naming it", err)
	}
	if e.mutations() != before+1 {
		t.Errorf("repeated policy create sent %d requests, want one", e.mutations()-before)
	}
	updated, err := invoke(t, core, branchPoliciesUpdate.ID, "listed",
		`{"environment":"production","id":361471,"name":"hotfix/v*"}`, true)
	if err != nil || !strings.Contains(string(updated), `"name":"hotfix/v*"`) {
		t.Fatalf("policy update = %s, %v", updated, err)
	}
	deleted, err := invoke(t, core, branchPoliciesDelete.ID, "listed", `{"environment":"production","id":361471}`, true)
	if err != nil || string(deleted) != `{"deleted":true,"environment":"production","id":361471,"repository":"octo-org/example"}` {
		t.Fatalf("policy delete = %s, %v", deleted, err)
	}

	unconfirmed := &application.ConfirmationRequiredError{}
	before = len(f.recorded())
	if _, err := invoke(t, core, environmentsDelete.ID, "listed", `{"environment":"staging"}`, false); !errors.As(err, &unconfirmed) ||
		len(f.recorded()) != before {
		t.Errorf("delete without confirmation = %v, want confirmation-required before GitHub", err)
	}
	removed, err := invoke(t, core, environmentsDelete.ID, "listed", `{"environment":"staging"}`, true)
	if err != nil || string(removed) != `{"deleted":true,"environment":"staging","repository":"octo-org/example"}` {
		t.Fatalf("delete = %s, %v", removed, err)
	}
	if _, err := invoke(t, core, environmentsGet.ID, "listed", `{"environment":"staging"}`, false); classOf(err) != provider.ClassNotFound {
		t.Errorf("get of a deleted environment = %v, want not-found", err)
	}
}

// assertDocumentedMembers checks that the output carries every member name given and that the output schema
// describes every member it carries.
func assertDocumentedMembers(t *testing.T, output, schema json.RawMessage, want []string) {
	t.Helper()
	var value any
	if err := json.Unmarshal(output, &value); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	outputMemberNames(value, names)
	var missing []string
	for _, name := range want {
		if !names[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("output lacks %v: %s", missing, output)
	}
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatal(err)
	}
	checkSchemaCovers(t, "$", value, parsed)
}

// The bounds of github.environments.set hold before a credential is read, and a reviewer GitHub does not know
// as a user or as a team of the repository's organization ends the call after the reads, without a change.
func TestEnvironmentSetRefusesUnknownReviewersWithoutAChange(t *testing.T) {
	_, e, base := serveEnvironments(t)
	reads := 0
	red := &redact.Redactor{}
	core := application.New(registry(t), environmentsConfig(base), resolver(red, &reads), red)

	seven := make([]string, 7)
	for i := range seven {
		seven[i] = fmt.Sprintf(`{"type":"User","name":"user%d"}`, i)
	}
	set := func(reviewers string) string {
		return `{"environment":"production","wait_timer":0,"prevent_self_review":false,"reviewers":` + reviewers +
			`,"deployment_branch_policy":"all"}`
	}
	for name, arguments := range map[string]string{
		"seven reviewers":       set("[" + strings.Join(seven, ",") + "]"),
		"an unknown type":       set(`[{"type":"Bot","name":"octocat"}]`),
		"a numeric identifier":  set(`[{"type":"User","id":1}]`),
		"a repeated reviewer":   set(`[{"type":"User","name":"octocat"},{"type":"User","name":"OctoCat"}]`),
		"an excluded user":      `{"repository":"octo-org/allowed","wait_timer":0,"prevent_self_review":false,"reviewers":[{"type":"User","name":"blocked"}],"deployment_branch_policy":"all","environment":"x"}`,
		"no reviewers":          `{"environment":"production","wait_timer":0,"prevent_self_review":false,"deployment_branch_policy":"all"}`,
		"no wait timer":         `{"environment":"production","prevent_self_review":false,"reviewers":[],"deployment_branch_policy":"all"}`,
		"an unknown policy":     set(`[]`)[:len(set(`[]`))-len(`"all"}`)] + `"some"}`,
		"a wait timer too long": `{"environment":"production","wait_timer":43201,"prevent_self_review":false,"reviewers":[],"deployment_branch_policy":"all"}`,
		"a control character":   "{\"environment\":\"prod\\u0001\",\"wait_timer\":0,\"prevent_self_review\":false,\"reviewers\":[],\"deployment_branch_policy\":\"all\"}",
	} {
		connection := "listed"
		if name == "an excluded user" {
			connection = "ex"
		}
		reads, before := 0, len(e.rawPaths)
		if _, err := invoke(t, core, environmentsSet.ID, connection, arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s: set = %v, want an invalid request", name, err)
		}
		if reads != 0 || len(e.rawPaths) != before {
			t.Errorf("%s: set reached the credential or GitHub", name)
		}
	}

	for name, tt := range map[string]struct{ reviewers, want string }{
		"an unknown login":         {`[{"type":"User","name":"ghost"},{"type":"User","name":"octocat"}]`, "reviewer user ghost does not exist"},
		"an organization as user":  {`[{"type":"User","name":"octo-org"}]`, "is not a user account"},
		"an unknown team":          {`[{"type":"User","name":"octocat"},{"type":"Team","name":"nobody"}]`, "reviewer team nobody is not a team"},
		"a login as provider text": {`[{"type":"User","name":"ghost"}]`, "nothing was changed"},
	} {
		before := e.mutations()
		_, err := invoke(t, core, environmentsSet.ID, "listed", set(tt.reviewers), true)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), providerCanary) {
			t.Errorf("%s: set = %v, want an invalid request naming %q", name, err, tt.want)
		}
		if e.mutations() != before {
			t.Errorf("%s: set changed the environment", name)
		}
	}
}

// A refusal names what the request needs and the plan environments require, never GitHub's own text; a
// refused change says nothing was changed rather than that it may have been applied.
func TestEnvironmentRefusalsNameTheCauseWithoutProviderText(t *testing.T) {
	_, e, base := serveEnvironments(t)
	red := &redact.Redactor{}
	core := application.New(registry(t), environmentsConfig(base), resolver(red, nil), red)
	answer := func(status int) func(http.ResponseWriter, *http.Request) bool {
		return func(w http.ResponseWriter, r *http.Request) bool {
			if strings.HasPrefix(r.URL.Path, "/api/v3/users/") {
				return false
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			fmt.Fprint(w, `{"message":"`+providerCanary+`","documentation_url":"https://docs.github.com/rest"}`)
			return true
		}
	}
	setArguments := `{"environment":"production","wait_timer":0,"prevent_self_review":false,"reviewers":[],` +
		`"deployment_branch_policy":"protected"}`
	for _, tt := range []struct {
		name, id, arguments string
		status              int
		class               provider.Class
		want                []string
	}{
		{"list without a permission", environmentsList.ID, `{}`, http.StatusForbidden, provider.ClassPermission,
			[]string{"Actions: read", "GitHub Pro, Team, or Enterprise"}},
		{"list of a plan without environments", environmentsList.ID, `{}`, http.StatusNotFound, provider.ClassNotFound,
			[]string{"the environments of this repository", "GitHub Pro, Team, or Enterprise"}},
		{"get of a missing environment", environmentsGet.ID, `{"environment":"qa"}`, http.StatusNotFound,
			provider.ClassNotFound, []string{"environment qa", "GitHub Pro, Team, or Enterprise"}},
		{"set without a permission", environmentsSet.ID, setArguments, http.StatusForbidden, provider.ClassPermission,
			[]string{"Administration: read and write", "GitHub Pro, Team, or Enterprise"}},
		{"set rejected", environmentsSet.ID, setArguments, http.StatusUnprocessableEntity, provider.ClassProviderError,
			[]string{"the plan may not offer them", "public repository", "nothing was changed"}},
		{"set of a plan without environments", environmentsSet.ID, setArguments, http.StatusNotFound,
			provider.ClassNotFound, []string{"environment production", "GitHub Pro, Team, or Enterprise"}},
		{"delete without a permission", environmentsDelete.ID, `{"environment":"production"}`, http.StatusForbidden,
			provider.ClassPermission, []string{"Administration: read and write"}},
		{"policy list of a policy that is not custom", branchPoliciesList.ID, `{"environment":"production"}`,
			http.StatusNotFound, provider.ClassNotFound, []string{"deployment branch policies of environment production", "not custom"}},
		{"policy create rejected", branchPoliciesCreate.ID, `{"environment":"production","name":"["}`,
			http.StatusUnprocessableEntity, provider.ClassProviderError, []string{"pattern may be invalid"}},
		{"policy update of a missing policy", branchPoliciesUpdate.ID, `{"environment":"production","id":9,"name":"x"}`,
			http.StatusNotFound, provider.ClassNotFound, []string{"deployment branch policy 9 of environment production"}},
		{"policy delete without a permission", branchPoliciesDelete.ID, `{"environment":"production","id":9}`,
			http.StatusForbidden, provider.ClassPermission, []string{"Administration: read and write"}},
	} {
		e.fail = answer(tt.status)
		_, err := invoke(t, core, tt.id, "listed", tt.arguments, true)
		if classOf(err) != tt.class || strings.Contains(err.Error(), providerCanary) ||
			strings.Contains(err.Error(), "may have been applied") {
			t.Errorf("%s = %v, want class %s without provider text", tt.name, err, tt.class)
			continue
		}
		for _, want := range tt.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s = %v, want it to name %q", tt.name, err, want)
			}
		}
	}

	// A team reviewer the token may not read names the organization permission it needs.
	e.fail = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasPrefix(r.URL.Path, "/api/v3/orgs/") {
			return false
		}
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"`+providerCanary+`"}`)
		return true
	}
	before := e.mutations()
	_, err := invoke(t, core, environmentsSet.ID, "listed", setProduction, true)
	if classOf(err) != provider.ClassPermission || !strings.Contains(err.Error(), "read:org") ||
		strings.Contains(err.Error(), providerCanary) || e.mutations() != before {
		t.Errorf("set with a refused team read = %v, want a permission refusal naming read:org without a change", err)
	}
}

// After a server error, a gateway timeout, a dropped connection, or a client timeout, a change reports that it
// may have been applied, and nothing is sent again.
func TestEnvironmentChangesAreNeverRepeatedAfterAnUnclearOutcome(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"server error":    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"gateway timeout": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusGatewayTimeout) },
		"dropped connection": func(w http.ResponseWriter, _ *http.Request) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		},
		"client timeout": func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
	} {
		t.Run(name, func(t *testing.T) {
			_, e, base := serveEnvironments(t)
			e.fail = func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method == http.MethodGet {
					return false
				}
				e.mu.Unlock()
				defer e.mu.Lock()
				answer(w, r)
				return true
			}
			wait, timer, off := int64(0), false, "all"
			// Each call gets a client of its own, so the spacing after one change never delays the next.
			calls := map[string]func(context.Context, *Client) error{
				"set": func(ctx context.Context, c *Client) error {
					_, err := c.setEnvironment(ctx, &environmentsArguments{Environment: "production", WaitTimer: &wait,
						PreventSelfReview: &timer, Reviewers: []environmentReviewer{{Type: "User", Name: "octocat"}},
						DeploymentBranchPolicy: off})
					return err
				},
				"delete": func(ctx context.Context, c *Client) error {
					_, err := c.deleteEnvironment(ctx, "production")
					return err
				},
				"policy create": func(ctx context.Context, c *Client) error {
					_, err := c.createBranchPolicy(ctx, &environmentsArguments{Environment: "production", Name: "x/*"})
					return err
				},
				"policy update": func(ctx context.Context, c *Client) error {
					_, err := c.updateBranchPolicy(ctx, &environmentsArguments{Environment: "production", ID: 361470, Name: "y"})
					return err
				},
				"policy delete": func(ctx context.Context, c *Client) error {
					_, err := c.deleteBranchPolicy(ctx, &environmentsArguments{Environment: "production", ID: 361470})
					return err
				},
			}
			for call, run := range calls {
				before := e.mutations()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				if name == "client timeout" {
					ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
				}
				err := run(ctx, client(t, base, repoTarget))
				cancel()
				if err == nil || !strings.Contains(err.Error(), "may have been applied") {
					t.Errorf("%s = %v, want an unclear outcome", call, err)
				}
				if sent := e.mutations() - before; sent != 1 {
					t.Errorf("%s sent %d changes, want exactly one", call, sent)
				}
			}
		})
	}
}

// A repository outside the targets and an excluded repository are refused before a secret is read or GitHub
// is contacted, by every tool; without a tools list entry, the listed-only tools are not offered at all.
func TestEnvironmentToolsRefuseForeignAndExcludedRepositoriesBeforeIO(t *testing.T) {
	f, _, base := serveEnvironments(t)
	reads := 0
	red := &redact.Redactor{}
	reg := registry(t)
	core := application.New(reg, environmentsConfig(base), resolver(red, &reads), red)
	arguments := map[string]string{
		environmentsList.ID:     `{}`,
		environmentsGet.ID:      `{"environment":"production"}`,
		environmentsSet.ID:      `{"environment":"production","wait_timer":0,"prevent_self_review":false,"reviewers":[],"deployment_branch_policy":"all"}`,
		environmentsDelete.ID:   `{"environment":"production"}`,
		branchPoliciesList.ID:   `{"environment":"production"}`,
		branchPoliciesCreate.ID: `{"environment":"production","name":"x"}`,
		branchPoliciesUpdate.ID: `{"environment":"production","id":1,"name":"x"}`,
		branchPoliciesDelete.ID: `{"environment":"production","id":1}`,
	}
	for _, id := range environmentToolIDs {
		withRepository := `{"repository":"octo-org/example",` + strings.TrimPrefix(arguments[id], "{")
		withRepository = strings.Replace(withRepository, `",}`, `"}`, 1)
		if arguments[id] == `{}` {
			withRepository = `{"repository":"octo-org/example"}`
		}
		for _, connection := range []string{"foreign", "ex"} {
			reads = 0
			before := len(f.recorded())
			if _, err := invoke(t, core, id, connection, withRepository, true); err == nil {
				t.Errorf("%s on %s succeeded, want a refusal", id, connection)
			}
			if reads != 0 || len(f.recorded()) != before {
				t.Errorf("%s on %s reached the credential or GitHub", id, connection)
			}
		}
	}

	guarded := map[string]bool{environmentsSet.ID: true, environmentsDelete.ID: true, branchPoliciesDelete.ID: true}
	for _, id := range environmentToolIDs {
		descriptor, _, _ := reg.Lookup(id)
		if descriptor.RequiresToolAllowList != guarded[id] {
			t.Errorf("%s requires an allow-list = %t", id, descriptor.RequiresToolAllowList)
		}
		if !guarded[id] {
			continue
		}
		reads = 0
		before := len(f.recorded())
		var unsupported *capability.UnsupportedError
		if _, err := invoke(t, core, id, "repo", arguments[id], true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools list entry = %v, want unsupported-capability", id, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s without a tools list entry reached the credential or GitHub", id)
		}
	}
}

func TestEnvironmentsSubjectNamesOnlyCheckedNames(t *testing.T) {
	for path, want := range map[string]string{
		"environments":           "the environments of this repository",
		"environments/prod%2Feu": "environment prod/eu",
		"environments/prod/deployment-branch-policies":           "the deployment branch policies of environment prod",
		"environments/prod/deployment-branch-policies/7":         "deployment branch policy 7 of environment prod",
		"environments/prod/deployment-branch-policies/x":         "a deployment branch policy of environment prod",
		"environments/%01bad":                                    "an environment",
		"environments/prod/secrets":                              "a resource of environment prod",
		"labels":                                                 "",
		"environments/" + strings.Repeat("x", 256):               "an environment",
		"environments/prod/deployment-branch-policies/0":         "a deployment branch policy of environment prod",
		"environments/prod%20west/deployment-branch-policies/12": "deployment branch policy 12 of environment prod west",
	} {
		if got := environmentsSubject(path); got != want {
			t.Errorf("environmentsSubject(%q) = %q, want %q", path, got, want)
		}
	}
}
