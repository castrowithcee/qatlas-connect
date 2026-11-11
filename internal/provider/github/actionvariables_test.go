package github

import (
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

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// valueCanary stands for a variable value; it must never reach a message. variableProviderCanary stands for GitHub's
// own text of a refusal.
const (
	valueCanary            = "variable-value-canary-7c2d"
	variableProviderCanary = "provider-text-canary-91ab"
)

// fakeVariables answers the Actions variable routes of repository octo-org/example, its environments, and
// organization octo-org, and the repository reads that resolve a selected repository, through the failure
// hook of fakeGitHub. Variables are kept by level and by name without case, in insertion order.
type fakeVariables struct {
	mu      sync.Mutex
	f       *fakeGitHub
	store   map[string]map[string]map[string]any
	order   map[string][]string
	repoIDs map[string]int64
	// status, when set, answers every variable route with this status and GitHub's own text.
	status int
	// paths are the escaped request paths of the variable routes.
	paths []string
}

func serveVariables(t *testing.T) (*fakeGitHub, *fakeVariables, string) {
	t.Helper()
	v := &fakeVariables{store: map[string]map[string]map[string]any{}, order: map[string][]string{},
		repoIDs: map[string]int64{"octo-org/example": 1296269, "octo-org/tools": 1296270, "octo-org/secret": 1296271}}
	v.put("repos/octo-org/example", map[string]any{"name": "DEPLOY_REGION", "value": "eu-central-1",
		"created_at": "2026-01-02T03:04:05Z", "updated_at": "2026-01-03T03:04:05Z"})
	v.put("env:production", map[string]any{"name": "DEPLOY_REGION", "value": "eu-west-1",
		"created_at": "2026-01-02T03:04:05Z", "updated_at": "2026-01-03T03:04:05Z"})
	v.put("orgs/octo-org", map[string]any{"name": "SHARED_HOST", "value": "builds.example.com",
		"created_at": "2026-01-02T03:04:05Z", "updated_at": "2026-01-03T03:04:05Z", "visibility": "selected",
		"selected_repositories_url": "https://api.github.com/orgs/octo-org/actions/variables/SHARED_HOST/repositories"})
	f := &fakeGitHub{}
	v.f = f
	f.failure = v.route
	return f, v, serve(t, f)
}

func (v *fakeVariables) put(level string, variable map[string]any) {
	if v.store[level] == nil {
		v.store[level] = map[string]map[string]any{}
	}
	key := strings.ToUpper(variable["name"].(string))
	if _, exists := v.store[level][key]; !exists {
		v.order[level] = append(v.order[level], key)
	}
	v.store[level][key] = variable
}

// level reads the variable level and the escaped remainder of a variable route.
func variableLevel(escaped string) (level, rest string, ok bool) {
	if tail, found := strings.CutPrefix(escaped, "/api/v3/orgs/octo-org/actions/variables"); found {
		return "orgs/octo-org", tail, true
	}
	if tail, found := strings.CutPrefix(escaped, "/api/v3/repos/octo-org/example/actions/variables"); found {
		return "repos/octo-org/example", tail, true
	}
	if tail, found := strings.CutPrefix(escaped, "/api/v3/repos/octo-org/example/environments/"); found {
		segment, after, cut := strings.Cut(tail, "/")
		environment, err := url.PathUnescape(segment)
		if rest, isVariables := strings.CutPrefix(after, "variables"); cut && err == nil && isVariables {
			return "env:" + environment, rest, true
		}
	}
	return "", "", false
}

func (v *fakeVariables) route(w http.ResponseWriter, req *http.Request) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	escaped := req.URL.EscapedPath()
	w.Header().Set("Content-Type", "application/json")
	if repository, found := strings.CutPrefix(escaped, "/api/v3/repos/"); found && req.Method == http.MethodGet {
		canonical := strings.ToLower(repository)
		if id, known := v.repoIDs[canonical]; known {
			fmt.Fprintf(w, `{"id":%d,"full_name":%q}`, id, canonical)
			return true
		}
	}
	level, rest, ok := variableLevel(escaped)
	if !ok {
		return false
	}
	v.paths = append(v.paths, escaped)
	if v.status != 0 {
		w.WriteHeader(v.status)
		fmt.Fprintf(w, `{"message":%q}`, variableProviderCanary)
		return true
	}
	var body map[string]any
	if requests := v.f.recorded(); req.Method != http.MethodGet && len(requests) > 0 {
		body = requests[len(requests)-1].body
	}
	name, _ := url.PathUnescape(strings.TrimPrefix(rest, "/"))
	key := strings.ToUpper(name)
	notFound := func() bool {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
		return true
	}
	switch {
	case req.Method == http.MethodGet && rest == "":
		perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		keys := v.order[level]
		start := min((page-1)*perPage, len(keys))
		end := min(start+perPage, len(keys))
		entries := make([]string, 0, end-start)
		for _, k := range keys[start:end] {
			data, _ := json.Marshal(v.store[level][k])
			entries = append(entries, string(data))
		}
		fmt.Fprintf(w, `{"total_count":%d,"variables":[%s]}`, len(keys), strings.Join(entries, ","))
	case req.Method == http.MethodGet:
		variable, exists := v.store[level][key]
		if !exists {
			return notFound()
		}
		data, _ := json.Marshal(variable)
		w.Write(data)
	case req.Method == http.MethodPost && rest == "":
		created, _ := body["name"].(string)
		if _, exists := v.store[level][strings.ToUpper(created)]; exists {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprintf(w, `{"message":%q}`, variableProviderCanary)
			return true
		}
		variable := map[string]any{"name": created, "value": body["value"], "created_at": "2026-11-10T00:00:00Z",
			"updated_at": "2026-11-10T00:00:00Z"}
		if visibility, given := body["visibility"]; given {
			variable["visibility"] = visibility
		}
		v.put(level, variable)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{}`)
	case req.Method == http.MethodPatch:
		variable, exists := v.store[level][key]
		if !exists {
			return notFound()
		}
		for _, member := range []string{"name", "value", "visibility"} {
			if value, given := body[member]; given {
				variable[member] = value
			}
		}
		if renamed := strings.ToUpper(variable["name"].(string)); renamed != key {
			delete(v.store[level], key)
			for i, k := range v.order[level] {
				if k == key {
					v.order[level][i] = renamed
				}
			}
			v.store[level][renamed] = variable
		}
		w.WriteHeader(http.StatusNoContent)
	case req.Method == http.MethodDelete:
		if _, exists := v.store[level][key]; !exists {
			return notFound()
		}
		delete(v.store[level], key)
		v.order[level] = slicesWithout(v.order[level], key)
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

func slicesWithout(values []string, drop string) []string {
	out := values[:0]
	for _, value := range values {
		if value != drop {
			out = append(out, value)
		}
	}
	return out
}

var variableTools = []string{actionVariablesList.ID, actionVariablesGet.ID, actionVariablesCreate.ID,
	actionVariablesUpdate.ID, actionVariablesDelete.ID}

// variablesConfig binds the connection shapes the variable tools distinguish: a repository connection that
// selects by effects, one that lists every variable tool, an organization connection whose repository targets
// cover the organization with one exclusion, one with the organization alone, and one without targets that
// excludes the organization.
func variablesConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all}
	cfg.Connections["repo-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: variableTools}
	cfg.Connections["org-listed"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Exclude: []string{"repos/octo-org/secret"},
		Effects: all, Tools: variableTools}
	cfg.Connections["org-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: all, Tools: variableTools}
	cfg.Connections["open-excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: all, Tools: variableTools}
	return cfg
}

func variablesCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), variablesConfig(base), resolver(red, reads), red)
}

// Every variable tool satisfies its output contract through the application core at each of the three levels,
// sends every change exactly once to its fixed route, and resolves selected repositories before the change.
func TestActionVariablesToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, v, base := serveVariables(t)
	core := variablesCore(t, base, nil)

	for _, tt := range []struct {
		level, connection, target, located, existing, value, route string
	}{
		{"repository", "repo-listed", `"repository":"octo-org/example"`, `"repository":"octo-org/example"`,
			"DEPLOY_REGION", "eu-central-1", "/api/v3/repos/octo-org/example/actions/variables"},
		{"environment", "repo-listed", `"repository":"octo-org/example","environment":"production"`,
			`"environment":"production"`, "DEPLOY_REGION", "eu-west-1",
			"/api/v3/repos/octo-org/example/environments/production/variables"},
		{"organization", "org-listed", `"organization":"orgs/octo-org"`, `"organization":"orgs/octo-org"`,
			"SHARED_HOST", "builds.example.com", "/api/v3/orgs/octo-org/actions/variables"},
	} {
		listed, err := invoke(t, core, actionVariablesList.ID, tt.connection, `{`+tt.target+`}`, false)
		if err != nil || !strings.Contains(string(listed), `"value":"`+tt.value+`"`) ||
			!strings.Contains(string(listed), `"total_count":1`) || !strings.Contains(string(listed), tt.located) {
			t.Fatalf("%s list = %s, %v", tt.level, listed, err)
		}
		got, err := invoke(t, core, actionVariablesGet.ID, tt.connection, `{`+tt.target+`,"name":"`+tt.existing+`"}`, false)
		if err != nil || !strings.Contains(string(got), `"created_at":"2026-01-02T03:04:05Z"`) ||
			!strings.Contains(string(got), tt.located) {
			t.Fatalf("%s get = %s, %v", tt.level, got, err)
		}
		if tt.level == "organization" && (!strings.Contains(string(got), `"visibility":"selected"`) ||
			!strings.Contains(string(got), `"selected_repositories_url":"https://api.github.com/orgs/`)) {
			t.Errorf("organization get = %s, want visibility and selected_repositories_url", got)
		}

		create := `{` + tt.target + `,"name":"NEW_VAR","value":"` + valueCanary + `"`
		if tt.level == "organization" {
			create += `,"visibility":"selected","selected_repositories":["octo-org/example","Octo-Org/tools"]`
		}
		before := len(f.recorded())
		created, err := invoke(t, core, actionVariablesCreate.ID, tt.connection, create+`}`, true)
		if err != nil || !strings.Contains(string(created), `"created":true`) ||
			strings.Contains(string(created), valueCanary) || !strings.Contains(string(created), tt.located) {
			t.Fatalf("%s create = %s, %v", tt.level, created, err)
		}
		requests := f.recorded()[before:]
		changes := requests
		if tt.level == "organization" {
			if len(requests) != 3 || requests[0].method != http.MethodGet || requests[1].method != http.MethodGet ||
				requests[0].path != "/api/v3/repos/octo-org/example" || requests[1].path != "/api/v3/repos/Octo-Org/tools" {
				t.Fatalf("organization create requests = %+v, want two repository reads and one create", requests)
			}
			changes = requests[2:]
			ids, _ := json.Marshal(changes[0].body["selected_repository_ids"])
			if string(ids) != `[1296269,1296270]` || changes[0].body["visibility"] != "selected" {
				t.Errorf("organization create body = %+v", changes[0].body)
			}
			if !strings.Contains(string(created), `"selected_repositories":["octo-org/example","Octo-Org/tools"]`) {
				t.Errorf("organization create = %s, want the selected repositories it sent", created)
			}
		}
		if len(changes) != 1 || changes[0].method != http.MethodPost || changes[0].path != tt.route ||
			changes[0].body["value"] != valueCanary || changes[0].body["name"] != "NEW_VAR" {
			t.Fatalf("%s create requests = %+v, want one POST to %s", tt.level, changes, tt.route)
		}

		before = len(f.recorded())
		updated, err := invoke(t, core, actionVariablesUpdate.ID, tt.connection,
			`{`+tt.target+`,"name":"NEW_VAR","new_name":"RENAMED_VAR","value":"changed"}`, true)
		if err != nil || !strings.Contains(string(updated), `"updated":true`) ||
			!strings.Contains(string(updated), `"name":"RENAMED_VAR"`) {
			t.Fatalf("%s update = %s, %v", tt.level, updated, err)
		}
		if requests := f.recorded()[before:]; len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != tt.route+"/NEW_VAR" || requests[0].body["name"] != "RENAMED_VAR" {
			t.Fatalf("%s update requests = %+v, want one PATCH", tt.level, requests)
		}

		before = len(f.recorded())
		deleted, err := invoke(t, core, actionVariablesDelete.ID, tt.connection, `{`+tt.target+`,"name":"RENAMED_VAR"}`, true)
		if err != nil || !strings.Contains(string(deleted), `"deleted":true`) {
			t.Fatalf("%s delete = %s, %v", tt.level, deleted, err)
		}
		if requests := f.recorded()[before:]; len(requests) != 1 || requests[0].method != http.MethodDelete ||
			requests[0].path != tt.route+"/RENAMED_VAR" {
			t.Fatalf("%s delete requests = %+v, want one DELETE", tt.level, requests)
		}
	}

	// An organization update to visibility all sends no repository identifiers and reads no repository.
	before := len(f.recorded())
	if _, err := invoke(t, core, actionVariablesUpdate.ID, "org-listed",
		`{"organization":"orgs/octo-org","name":"SHARED_HOST","visibility":"all"}`, true); err != nil {
		t.Fatalf("organization update to all = %v", err)
	}
	if requests := f.recorded()[before:]; len(requests) != 1 || requests[0].body["visibility"] != "all" ||
		requests[0].body["selected_repository_ids"] != nil {
		t.Errorf("organization update to all = %+v", requests)
	}
	if v.store["orgs/octo-org"]["SHARED_HOST"]["visibility"] != "all" {
		t.Errorf("visibility = %v, want all", v.store["orgs/octo-org"]["SHARED_HOST"]["visibility"])
	}
}

// The list is paged in batches of at most 30, bound to its level, and continues where it ended.
func TestActionVariablesListPages(t *testing.T) {
	f, v, base := serveVariables(t)
	for i := 0; i < 4; i++ {
		v.put("repos/octo-org/example", map[string]any{"name": fmt.Sprintf("VAR_%d", i), "value": "x"})
	}
	core := variablesCore(t, base, nil)
	first, err := invoke(t, core, actionVariablesList.ID, "repo", `{"repository":"octo-org/example","limit":2}`, false)
	var page struct {
		Variables  []ActionVariable `json:"variables"`
		NextCursor string           `json:"next_cursor"`
		HasMore    bool             `json:"has_more"`
		TotalCount int              `json:"total_count"`
	}
	if err != nil || json.Unmarshal(first, &page) != nil || !page.HasMore || page.TotalCount != 5 || len(page.Variables) != 2 {
		t.Fatalf("first page = %s, %v", first, err)
	}
	second, err := invoke(t, core, actionVariablesList.ID, "repo",
		`{"repository":"octo-org/example","cursor":"`+page.NextCursor+`"}`, false)
	if err != nil || !strings.Contains(string(second), `"name":"VAR_1"`) {
		t.Fatalf("second page = %s, %v", second, err)
	}
	if requests := f.recorded(); !strings.Contains(requests[len(requests)-1].query, "page=2") ||
		!strings.Contains(requests[len(requests)-1].query, "per_page=2") {
		t.Errorf("continuation query = %q", requests[len(requests)-1].query)
	}
	// A cursor of the repository list does not continue the list of an environment.
	if _, err := invoke(t, core, actionVariablesList.ID, "repo",
		`{"repository":"octo-org/example","environment":"production","cursor":"`+page.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("foreign cursor = %v, want an invalid request", err)
	}
	if _, err := invoke(t, core, actionVariablesList.ID, "repo", `{"repository":"octo-org/example","limit":31}`, false); !isInvalidRequest(err) {
		t.Errorf("limit 31 = %v, want an invalid request", err)
	}
}

// repository and organization are exclusive and environment belongs to a repository; every refusal, a
// target outside the connection, and an excluded target end before a credential is resolved.
func TestActionVariablesTargetIsExclusiveAndCheckedBeforeIO(t *testing.T) {
	f, _, base := serveVariables(t)
	reads := 0
	core := variablesCore(t, base, &reads)
	for _, tt := range []struct {
		name, id, connection, arguments string
	}{
		{"neither", actionVariablesList.ID, "repo", `{}`},
		{"both", actionVariablesList.ID, "org-listed", `{"repository":"octo-org/example","organization":"orgs/octo-org"}`},
		{"environment without repository", actionVariablesList.ID, "repo", `{"environment":"production"}`},
		{"environment with organization", actionVariablesGet.ID, "org-listed",
			`{"organization":"orgs/octo-org","environment":"production","name":"X"}`},
		{"an organization outside the targets", actionVariablesList.ID, "repo", `{"organization":"orgs/octo-org"}`},
		{"a user owner", actionVariablesList.ID, "open-excluding", `{"organization":"users/octocat"}`},
		{"a repository outside the targets", actionVariablesCreate.ID, "repo",
			`{"repository":"other-org/example","name":"X","value":"` + valueCanary + `"}`},
		{"an excluded repository", actionVariablesList.ID, "org-listed", `{"repository":"octo-org/secret"}`},
		{"an excluded organization", actionVariablesDelete.ID, "open-excluding", `{"organization":"orgs/octo-org","name":"X"}`},
		{"a repository of an excluded organization", actionVariablesUpdate.ID, "open-excluding",
			`{"repository":"octo-org/example","name":"X","value":"` + valueCanary + `"}`},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if err != nil && strings.Contains(err.Error(), valueCanary) {
			t.Errorf("%s quotes the value: %v", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// visibility and selected repositories belong to an organization variable, and every selected repository must
// belong to the organization and lie inside the connection's repository targets, before any request.
func TestActionVariablesSelectedRepositoriesAreBoundBeforeIO(t *testing.T) {
	f, _, base := serveVariables(t)
	reads := 0
	core := variablesCore(t, base, &reads)
	org := `"organization":"orgs/octo-org","name":"X","value":"` + valueCanary + `"`
	for _, tt := range []struct {
		name, id, connection, arguments string
	}{
		{"a repository outside the targets", actionVariablesCreate.ID, "org-only",
			`{` + org + `,"visibility":"selected","selected_repositories":["octo-org/example"]}`},
		{"an excluded repository", actionVariablesCreate.ID, "org-listed",
			`{` + org + `,"visibility":"selected","selected_repositories":["octo-org/example","octo-org/secret"]}`},
		{"a repository of another organization", actionVariablesUpdate.ID, "org-listed",
			`{` + org + `,"visibility":"selected","selected_repositories":["other-org/example"]}`},
		{"a repeated repository", actionVariablesCreate.ID, "org-listed",
			`{` + org + `,"visibility":"selected","selected_repositories":["octo-org/example","Octo-Org/Example"]}`},
		{"a repository pattern", actionVariablesCreate.ID, "org-listed",
			`{` + org + `,"visibility":"selected","selected_repositories":["octo-org/*"]}`},
		{"selected repositories without visibility selected", actionVariablesUpdate.ID, "org-listed",
			`{` + org + `,"selected_repositories":["octo-org/example"]}`},
		{"selected repositories with visibility all", actionVariablesCreate.ID, "org-listed",
			`{` + org + `,"visibility":"all","selected_repositories":["octo-org/example"]}`},
		{"visibility selected without repositories", actionVariablesUpdate.ID, "org-listed",
			`{` + org + `,"visibility":"selected"}`},
		{"an organization variable without visibility", actionVariablesCreate.ID, "org-listed", `{` + org + `}`},
		{"visibility on a repository variable", actionVariablesCreate.ID, "repo-listed",
			`{"repository":"octo-org/example","name":"X","value":"` + valueCanary + `","visibility":"all"}`},
		{"selected repositories on an environment variable", actionVariablesUpdate.ID, "repo-listed",
			`{"repository":"octo-org/example","environment":"production","name":"X","selected_repositories":[]}`},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if err != nil && strings.Contains(err.Error(), valueCanary) {
			t.Errorf("%s quotes the value: %v", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// Names follow GitHub's rules, values its bounds, and an environment stays one plain path segment; every
// refusal ends before a credential is resolved and never quotes the value.
func TestActionVariablesArgumentsAreValidatedBeforeIO(t *testing.T) {
	f, _, base := serveVariables(t)
	reads := 0
	core := variablesCore(t, base, &reads)
	repo := `"repository":"octo-org/example"`
	for _, tt := range []struct {
		name, id, arguments string
	}{
		{"a name starting with a digit", actionVariablesGet.ID, `{` + repo + `,"name":"1ABC"}`},
		{"a name with a hyphen", actionVariablesGet.ID, `{` + repo + `,"name":"A-B"}`},
		{"a name with a slash", actionVariablesDelete.ID, `{` + repo + `,"name":"A/../B"}`},
		{"the GITHUB_ prefix", actionVariablesCreate.ID, `{` + repo + `,"name":"GITHUB_TOKEN","value":"` + valueCanary + `"}`},
		{"the GITHUB_ prefix in lower case", actionVariablesCreate.ID, `{` + repo + `,"name":"github_x","value":"` + valueCanary + `"}`},
		{"a new name with the GITHUB_ prefix", actionVariablesUpdate.ID, `{` + repo + `,"name":"A","new_name":"Github_A"}`},
		{"an empty name", actionVariablesGet.ID, `{` + repo + `,"name":""}`},
		{"an empty value", actionVariablesCreate.ID, `{` + repo + `,"name":"A","value":""}`},
		{"a missing value", actionVariablesCreate.ID, `{` + repo + `,"name":"A"}`},
		{"a value with a control character", actionVariablesCreate.ID, `{` + repo + `,"name":"A","value":"` + valueCanary + `\u0007"}`},
		{"a value over 48 KB", actionVariablesUpdate.ID, `{` + repo + `,"name":"A","value":"` + valueCanary +
			strings.Repeat("ä", 25000) + `"}`},
		{"an update without a change", actionVariablesUpdate.ID, `{` + repo + `,"name":"A"}`},
		{"the environment ..", actionVariablesList.ID, `{` + repo + `,"environment":".."}`},
		{"an environment with a control character", actionVariablesList.ID, `{` + repo + `,"environment":"a\nb"}`},
		{"an environment over 255 characters", actionVariablesList.ID, `{` + repo + `,"environment":"` + strings.Repeat("e", 256) + `"}`},
		{"an empty environment", actionVariablesList.ID, `{` + repo + `,"environment":""}`},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, "repo-listed", tt.arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if err != nil && strings.Contains(err.Error(), valueCanary) {
			t.Errorf("%s quotes the value: %v", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// An environment name of any plain text travels as exactly one escaped path segment, and a refusal names it.
func TestActionVariablesEnvironmentIsOnePathSegment(t *testing.T) {
	_, v, base := serveVariables(t)
	core := variablesCore(t, base, nil)
	_, err := invoke(t, core, actionVariablesGet.ID, "repo",
		`{"repository":"octo-org/example","environment":"prod/eu west ü?#%","name":"DEPLOY_REGION"}`, false)
	if classOf(err) != provider.ClassNotFound ||
		!strings.Contains(err.Error(), "variable DEPLOY_REGION of environment prod/eu west ü?#% in repository octo-org/example") {
		t.Fatalf("get in a missing environment = %v, want not-found naming the environment", err)
	}
	want := "/api/v3/repos/octo-org/example/environments/prod%2Feu%20west%20%C3%BC%3F%23%25/variables/DEPLOY_REGION"
	if got := v.paths[len(v.paths)-1]; got != want {
		t.Errorf("path = %s, want %s", got, want)
	}
}

// A refusal names what the request needs in Qatlas' own words: the token right by level, a conflicting name, a
// rejected variable, or an outcome that may have been applied. The value and GitHub's text never reach the
// message, and a change is sent once.
func TestActionVariablesFailuresAreNamed(t *testing.T) {
	f, v, base := serveVariables(t)
	core := variablesCore(t, base, nil)
	levels := map[string]struct{ connection, target, right string }{
		"repository":   {"repo-listed", `"repository":"octo-org/example"`, "Variables: read and write"},
		"environment":  {"repo-listed", `"repository":"octo-org/example","environment":"production"`, "Environments: read and write"},
		"organization": {"org-listed", `"organization":"orgs/octo-org"`, "admin:org"},
	}
	names := make([]string, 0, len(levels))
	for name := range levels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		level := levels[name]
		create := `{` + level.target + `,"name":"FRESH","value":"` + valueCanary + `"`
		if name == "organization" {
			create += `,"visibility":"private"`
		}
		create += `}`
		for _, tt := range []struct {
			status    int
			class     provider.Class
			fragment  string
			uncertain bool
		}{
			{http.StatusForbidden, provider.ClassPermission, level.right, false},
			{http.StatusNotFound, provider.ClassNotFound, "does not hold", false},
			{http.StatusConflict, provider.ClassProviderError, "already holds a variable named FRESH", false},
			{http.StatusUnprocessableEntity, provider.ClassProviderError, "rejected the variable as invalid", false},
			{http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true},
		} {
			v.status = tt.status
			before := len(f.recorded())
			_, err := invoke(t, core, actionVariablesCreate.ID, level.connection, create, true)
			if classOf(err) != tt.class || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain {
				t.Errorf("%s create answered %d = %v, want %s naming %q", name, tt.status, err, tt.class, tt.fragment)
			}
			if err != nil && (strings.Contains(err.Error(), valueCanary) || strings.Contains(err.Error(), variableProviderCanary)) {
				t.Errorf("%s create answered %d quotes the value or GitHub's text: %v", name, tt.status, err)
			}
			if requests := f.recorded()[before:]; len(requests) != 1 {
				t.Errorf("%s create answered %d sent %d requests, want exactly one", name, tt.status, len(requests))
			}
		}
		// Reads name the read right, and a timeout of an update or a delete is never repeated.
		v.status = http.StatusForbidden
		readRight := strings.Replace(level.right, "read and write", "read", 1)
		if _, err := invoke(t, core, actionVariablesList.ID, level.connection, `{`+level.target+`}`, false); classOf(err) != provider.ClassPermission ||
			!strings.Contains(err.Error(), readRight) || strings.Contains(err.Error(), "write") {
			t.Errorf("%s list refused = %v, want the read right %q", name, err, readRight)
		}
		v.status = http.StatusGatewayTimeout
		for _, id := range []string{actionVariablesUpdate.ID, actionVariablesDelete.ID} {
			arguments := `{` + level.target + `,"name":"FRESH"`
			if id == actionVariablesUpdate.ID {
				arguments += `,"value":"` + valueCanary + `"`
			}
			before := len(f.recorded())
			_, err := invoke(t, core, id, level.connection, arguments+`}`, true)
			if classOf(err) != provider.ClassTimeout || !strings.Contains(err.Error(), uncertain) ||
				strings.Contains(err.Error(), valueCanary) {
				t.Errorf("%s %s timed out = %v, want an uncertain timeout", name, id, err)
			}
			if requests := f.recorded()[before:]; len(requests) != 1 {
				t.Errorf("%s %s timed out and sent %d requests, want exactly one", name, id, len(requests))
			}
		}
		v.status = 0
	}

	// A selected repository GitHub does not show stops the change before it is sent.
	delete(v.repoIDs, "octo-org/tools")
	before := len(f.recorded())
	_, err := invoke(t, core, actionVariablesCreate.ID, "org-listed", `{"organization":"orgs/octo-org","name":"FRESH",`+
		`"value":"`+valueCanary+`","visibility":"selected","selected_repositories":["octo-org/example","octo-org/tools"]}`, true)
	if classOf(err) != provider.ClassNotFound || strings.Contains(err.Error(), uncertain) {
		t.Errorf("an unresolvable selected repository = %v, want not-found without uncertainty", err)
	}
	for _, request := range f.recorded()[before:] {
		if request.method != http.MethodGet {
			t.Errorf("an unresolvable selected repository sent %s %s", request.method, request.path)
		}
	}
}

// A long value is cut with value_truncated: at 1000 characters in a list and at 20000 in a single read, with
// control characters other than line breaks and tabs replaced.
func TestActionVariablesValuesAreCut(t *testing.T) {
	_, v, base := serveVariables(t)
	long := strings.Repeat("ab\n", 9000) + "\x1b"
	v.put("repos/octo-org/example", map[string]any{"name": "LONG", "value": long})
	core := variablesCore(t, base, nil)

	got, err := invoke(t, core, actionVariablesGet.ID, "repo", `{"repository":"octo-org/example","name":"LONG"}`, false)
	var variable ActionVariable
	if err != nil || json.Unmarshal(got, &variable) != nil || !variable.ValueTruncated ||
		len([]rune(variable.Value)) != variableValueLimit || !strings.HasPrefix(variable.Value, "ab\nab\n") {
		t.Fatalf("get = %d runes, truncated %t, %v", len([]rune(variable.Value)), variable.ValueTruncated, err)
	}
	listed, err := invoke(t, core, actionVariablesList.ID, "repo", `{"repository":"octo-org/example"}`, false)
	var list ActionVariableList
	if err != nil || json.Unmarshal(listed, &list) != nil {
		t.Fatalf("list = %s, %v", listed, err)
	}
	for _, entry := range list.Variables {
		if entry.Name == "LONG" && (!entry.ValueTruncated || len([]rune(entry.Value)) != variableListValueLimit) {
			t.Errorf("listed long value = %d runes, truncated %t", len([]rune(entry.Value)), entry.ValueTruncated)
		}
		if entry.Name == "DEPLOY_REGION" && entry.ValueTruncated {
			t.Errorf("a short value is marked truncated")
		}
	}
	v.put("repos/octo-org/example", map[string]any{"name": "ESCAPE", "value": "a\x1b[31mb\tc"})
	escaped, err := invoke(t, core, actionVariablesGet.ID, "repo", `{"repository":"octo-org/example","name":"ESCAPE"}`, false)
	if err != nil || !strings.Contains(string(escaped), `"value":"a [31mb\tc"`) {
		t.Errorf("control characters = %s, %v", escaped, err)
	}
}

// The delete is offered only where a connection's tools list names it; the changes need confirmation; both
// refusals end before a secret is read.
func TestActionVariablesDeleteIsListedOnlyAndChangesNeedConfirmation(t *testing.T) {
	f, _, base := serveVariables(t)
	reads := 0
	core := variablesCore(t, base, &reads)
	var unsupported *capability.UnsupportedError
	if _, err := core.Describe(application.DescribeRequest{Operation: actionVariablesDelete.ID, Connection: "repo"}); !errors.As(err, &unsupported) {
		t.Errorf("describe delete on repo = %v, want unsupported", err)
	}
	if _, err := invoke(t, core, actionVariablesDelete.ID, "repo", `{"repository":"octo-org/example","name":"DEPLOY_REGION"}`, true); !errors.As(err, &unsupported) {
		t.Errorf("invoke delete on repo = %v, want unsupported", err)
	}
	for _, id := range []string{actionVariablesCreate.ID, actionVariablesUpdate.ID} {
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "repo"}); err != nil {
			t.Errorf("describe %s on repo = %v, want it offered by effects", id, err)
		}
	}
	unconfirmed := &application.ConfirmationRequiredError{}
	for _, tt := range []struct{ id, arguments string }{
		{actionVariablesCreate.ID, `{"repository":"octo-org/example","name":"A","value":"` + valueCanary + `"}`},
		{actionVariablesUpdate.ID, `{"repository":"octo-org/example","name":"A","value":"` + valueCanary + `"}`},
		{actionVariablesDelete.ID, `{"repository":"octo-org/example","name":"A"}`},
	} {
		if _, err := invoke(t, core, tt.id, "repo-listed", tt.arguments, false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v, want confirmation-required", tt.id, err)
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}

// A refusal of a variable route names the variable by its checked name, below a repository, an environment,
// or an organization.
func TestVariableSubjectNamesOnlyCheckedNames(t *testing.T) {
	for path, want := range map[string]string{
		"actions/variables":                   "the Actions variables",
		"actions/variables/DEPLOY":            "Actions variable DEPLOY",
		"actions/variables/bad%2Fname":        "a variable",
		"actions/variablesX":                  "",
		"environments/prod%2Feu/variables":    "the variables of environment prod/eu",
		"environments/prod/variables/DEPLOY":  "variable DEPLOY of environment prod",
		"environments/a%0Ab/variables/DEPLOY": "variable DEPLOY of an environment",
		"environments/prod/secrets/DEPLOY":    "",
		"actions/runs/1":                      "",
	} {
		if got := variableSubject(path); got != want {
			t.Errorf("variableSubject(%q) = %q, want %q", path, got, want)
		}
	}
	c := &Client{endpoints: endpoints{rest: "https://api.github.com"}}
	request, _ := http.NewRequest(http.MethodGet, "https://api.github.com/orgs/octo-org/actions/variables/SHARED", nil)
	if got := c.restSubject(request).String(); got != "Actions variable SHARED of orgs/octo-org" {
		t.Errorf("organization subject = %q", got)
	}
}
