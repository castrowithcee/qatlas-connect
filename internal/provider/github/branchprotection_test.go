package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	protectionPath = "/api/v3/repos/octo-org/example/branches/main/protection"
	rulesPath      = "/api/v3/repos/octo-org/example/rules/branches/main"
)

// protectedBranchJSON is GitHub's protected branch with every documented member.
const protectedBranchJSON = `{"url":"https://api.github.com/repos/octo-org/example/branches/main/protection",` +
	`"required_status_checks":{"url":"https://api.github.com/s","strict":true,"contexts":["ci/test"],` +
	`"contexts_url":"https://api.github.com/c","checks":[{"context":"ci/test","app_id":15368}]},` +
	`"enforce_admins":{"url":"https://api.github.com/e","enabled":true},` +
	`"required_pull_request_reviews":{"url":"https://api.github.com/r","dismiss_stale_reviews":true,` +
	`"require_code_owner_reviews":false,"required_approving_review_count":2,"require_last_push_approval":true,` +
	`"dismissal_restrictions":{"url":"https://api.github.com/d","users_url":"https://api.github.com/du",` +
	`"teams_url":"https://api.github.com/dt","users":[{"login":"octocat","id":1}],"teams":[],"apps":[]},` +
	`"bypass_pull_request_allowances":{"users":[],"teams":[{"slug":"justice-league","id":2,"name":"Justice"}],"apps":[]}},` +
	`"restrictions":{"url":"https://api.github.com/x","users_url":"https://api.github.com/xu",` +
	`"teams_url":"https://api.github.com/xt","apps_url":"https://api.github.com/xa","users":[],"teams":[],"apps":[]},` +
	`"required_signatures":{"url":"https://api.github.com/g","enabled":false},` +
	`"required_linear_history":{"enabled":true},"allow_force_pushes":{"enabled":false},` +
	`"allow_deletions":{"enabled":false},"block_creations":{"enabled":true},` +
	`"required_conversation_resolution":{"enabled":true},"lock_branch":{"enabled":false},` +
	`"allow_fork_syncing":{"enabled":false}}`

// fullProtection is a complete set of settings, for the set tool.
const fullProtection = `"required_status_checks":{"strict":true,"checks":[{"context":"ci/test","app_id":15368}]},` +
	`"enforce_admins":true,"required_pull_request_reviews":{"dismiss_stale_reviews":true,` +
	`"require_code_owner_reviews":false,"required_approving_review_count":2,"require_last_push_approval":true,` +
	`"dismissal_restrictions":{"users":["octocat"],"teams":[]}},"restrictions":null,` +
	`"required_linear_history":true,"allow_force_pushes":null,"allow_deletions":false,"block_creations":true,` +
	`"required_conversation_resolution":true,"lock_branch":false,"allow_fork_syncing":false`

func protectionConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["repo-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: []string{branchProtectionSet.ID, branchProtectionDelete.ID}}
	cfg.Connections["excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Exclude: []string{repoTarget}, Effects: all,
		Tools: []string{branchProtectionSet.ID, branchProtectionDelete.ID}}
	return cfg
}

func protectionCore(t *testing.T, handler func(http.ResponseWriter, *http.Request), reads *int) (*fakeGitHub, *application.Core) {
	t.Helper()
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/protection") || strings.Contains(r.URL.Path, "/rules/branches/") {
			handler(w, r)
			return true
		}
		return false
	}
	base := serve(t, f)
	red := &redact.Redactor{}
	return f, application.New(registry(t), protectionConfig(base), resolver(red, reads), red)
}

func answer(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
}

func count(f *fakeGitHub, method string) int {
	n := 0
	for _, r := range f.recorded() {
		if r.method == method && strings.Contains(r.path, "/protection") {
			n++
		}
	}
	return n
}

func TestBranchProtectionGetReadsEveryField(t *testing.T) {
	f, core := protectionCore(t, answer(200, protectedBranchJSON), nil)
	out, err := invoke(t, core, branchProtectionGet.ID, "repo", `{"branch":"main"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Branch     string         `json:"branch"`
		State      string         `json:"state"`
		Repository string         `json:"repository"`
		Protection map[string]any `json:"protection"`
	}
	if json.Unmarshal(out, &got) != nil || got.State != "protected" || got.Branch != "main" ||
		got.Repository != "octo-org/example" {
		t.Fatalf("get = %s", out)
	}
	for _, field := range []string{"required_status_checks", "enforce_admins", "required_pull_request_reviews",
		"restrictions", "required_signatures", "required_linear_history", "allow_force_pushes", "allow_deletions",
		"block_creations", "required_conversation_resolution", "lock_branch", "allow_fork_syncing", "links"} {
		if _, ok := got.Protection[field]; !ok {
			t.Errorf("field %s missing in %s", field, out)
		}
	}
	reviews, _ := got.Protection["required_pull_request_reviews"].(map[string]any)
	for _, field := range []string{"dismissal_restrictions", "bypass_pull_request_allowances", "dismiss_stale_reviews",
		"require_code_owner_reviews", "required_approving_review_count", "require_last_push_approval", "links"} {
		if _, ok := reviews[field]; !ok {
			t.Errorf("review field %s missing in %s", field, out)
		}
	}
	if r := f.recorded(); len(r) != 1 || r[0].method != http.MethodGet || r[0].path != protectionPath {
		t.Errorf("requests = %+v", r)
	}
}

func TestBranchProtectionGetStates(t *testing.T) {
	_, core := protectionCore(t, answer(404, `{"message":"Branch not protected"}`), nil)
	out, err := invoke(t, core, branchProtectionGet.ID, "repo", `{"branch":"main"}`, false)
	if err != nil || !strings.Contains(string(out), `"state":"unprotected"`) || strings.Contains(string(out), `"protection"`) {
		t.Errorf("unprotected = %s, %v", out, err)
	}
	for name, tt := range map[string]struct {
		status int
		body   string
		class  provider.Class
	}{
		"missing branch": {404, `{"message":"Branch not found SECRETTEXT"}`, provider.ClassNotFound},
		"forbidden":      {403, `{"message":"Resource not accessible SECRETTEXT"}`, provider.ClassPermission},
		"unreadable":     {200, `x`, provider.ClassInvalidResponse},
		"null":           {200, `null`, provider.ClassInvalidResponse},
		"empty":          {200, `{}`, provider.ClassInvalidResponse},
	} {
		_, core := protectionCore(t, answer(tt.status, tt.body), nil)
		_, err := invoke(t, core, branchProtectionGet.ID, "repo", `{"branch":"main"}`, false)
		if classOf(err) != tt.class || strings.Contains(err.Error(), "SECRETTEXT") {
			t.Errorf("%s = %v, want %s without provider text", name, err, tt.class)
		}
	}
}

func TestBranchProtectionSetSendsTheWholeProtectionOnce(t *testing.T) {
	f, core := protectionCore(t, answer(200, protectedBranchJSON), nil)
	out, err := invoke(t, core, branchProtectionSet.ID, "repo-listed", `{"branch":"main",`+fullProtection+`}`, true)
	if err != nil || !strings.Contains(string(out), `"state":"protected"`) ||
		!strings.Contains(string(out), `"required_approving_review_count":2`) {
		t.Fatalf("set = %s, %v", out, err)
	}
	requests := f.recorded()
	if len(requests) != 1 || requests[0].method != http.MethodPut || requests[0].path != protectionPath {
		t.Fatalf("requests = %+v, want one PUT", requests)
	}
	body := requests[0].body
	for _, key := range []string{"required_status_checks", "enforce_admins", "required_pull_request_reviews",
		"restrictions", "required_linear_history", "allow_force_pushes", "allow_deletions", "block_creations",
		"required_conversation_resolution", "lock_branch", "allow_fork_syncing"} {
		if _, ok := body[key]; !ok {
			t.Errorf("body lacks %s: %v", key, body)
		}
	}
	if body["restrictions"] != nil || body["allow_force_pushes"] != nil || body["enforce_admins"] != true {
		t.Errorf("body = %v, want explicit nulls kept", body)
	}
	if _, has := body["contexts"]; has {
		t.Error("deprecated contexts was sent")
	}
}

func TestBranchProtectionSetRefusesEverySettingLeftOut(t *testing.T) {
	reads := 0
	f, core := protectionCore(t, answer(200, protectedBranchJSON), &reads)
	var full map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{"branch":"main",`+fullProtection+`}`), &full); err != nil {
		t.Fatal(err)
	}
	for name := range full {
		if name == "branch" {
			continue
		}
		arguments := map[string]json.RawMessage{}
		for key, value := range full {
			if key != name {
				arguments[key] = value
			}
		}
		encoded, _ := json.Marshal(arguments)
		reads = 0
		if _, err := invoke(t, core, branchProtectionSet.ID, "repo-listed", string(encoded), true); !isInvalidRequest(err) {
			t.Errorf("without %s = %v, want an invalid request", name, err)
		}
		if reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("without %s reached the credential or GitHub", name)
		}
	}
}

func TestBranchProtectionSetChecksSubObjectsBeforeIO(t *testing.T) {
	reads := 0
	f, core := protectionCore(t, answer(200, protectedBranchJSON), &reads)
	base := func(replace string) string {
		return strings.NewReplacer(`"restrictions":null`, replace).Replace(`{"branch":"main",` + fullProtection + `}`)
	}
	for name, arguments := range map[string]string{
		"restrictions without teams":   base(`"restrictions":{"users":[]}`),
		"restriction with a bad login": base(`"restrictions":{"users":["a b"],"teams":[]}`),
		"restriction url":              base(`"restrictions":{"users":["https://evil.example/x"],"teams":[]}`),
		"unknown restriction member":   base(`"restrictions":{"users":[],"teams":[],"x":1}`),
		"checks left out":              strings.Replace(base(`"restrictions":null`), `"checks":[{"context":"ci/test","app_id":15368}]`, `"x":1`, 1),
		"strict null":                  strings.Replace(base(`"restrictions":null`), `"strict":true`, `"strict":null`, 1),
		"review count too high":        strings.Replace(base(`"restrictions":null`), `"required_approving_review_count":2`, `"required_approving_review_count":9`, 1),
		"review count left out":        strings.Replace(base(`"restrictions":null`), `"required_approving_review_count":2,`, ``, 1),
		"null bool":                    strings.Replace(base(`"restrictions":null`), `"lock_branch":false`, `"lock_branch":null`, 1),
		"string bool":                  strings.Replace(base(`"restrictions":null`), `"enforce_admins":true`, `"enforce_admins":"yes"`, 1),
		"ref prefix":                   strings.Replace(base(`"restrictions":null`), `"branch":"main"`, `"branch":"refs/heads/main"`, 1),
		"bad branch":                   strings.Replace(base(`"restrictions":null`), `"branch":"main"`, `"branch":"a..b"`, 1),
		"unknown argument":             strings.Replace(base(`"restrictions":null`), `"branch":"main"`, `"branch":"main","force":true`, 1),
	} {
		reads = 0
		if _, err := invoke(t, core, branchProtectionSet.ID, "repo-listed", arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", name, err)
		}
		if reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("%s reached the credential or GitHub", name)
		}
	}
}

func TestBranchProtectionChangesMapRefusalsAndNeverRepeat(t *testing.T) {
	set := `{"branch":"main",` + fullProtection + `}`
	for _, tool := range []struct{ id, arguments, method string }{
		{branchProtectionSet.ID, set, http.MethodPut},
		{branchProtectionDelete.ID, `{"branch":"main"}`, http.MethodDelete},
	} {
		for name, tt := range map[string]struct {
			handler func(http.ResponseWriter, *http.Request)
			class   provider.Class
			unclear bool
		}{
			"forbidden":  {answer(403, `{"message":"SECRETTEXT"}`), provider.ClassPermission, false},
			"not found":  {answer(404, `{"message":"Branch not found SECRETTEXT"}`), provider.ClassNotFound, false},
			"rejected":   {answer(422, `{"message":"Only organization repositories SECRETTEXT"}`), provider.ClassProviderError, false},
			"server":     {answer(502, ``), provider.ClassProviderError, true},
			"unreadable": {answer(200, `x`), provider.ClassInvalidResponse, true},
			"dropped": {func(w http.ResponseWriter, _ *http.Request) {
				if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
					connection.Close()
				}
			}, "", true},
		} {
			if tool.id == branchProtectionDelete.ID && name == "unreadable" {
				continue
			}
			t.Run(tool.id+" "+name, func(t *testing.T) {
				f, core := protectionCore(t, tt.handler, nil)
				_, err := invoke(t, core, tool.id, "repo-listed", tool.arguments, true)
				if err == nil || strings.Contains(err.Error(), "SECRETTEXT") {
					t.Fatalf("err = %v", err)
				}
				if tt.class != "" && classOf(err) != tt.class {
					t.Errorf("class = %s, want %s", classOf(err), tt.class)
				}
				if got := strings.Contains(err.Error(), "may have been applied"); got != tt.unclear {
					t.Errorf("uncertainty = %v in %v, want %v", got, err, tt.unclear)
				}
				if count(f, tool.method) != 1 {
					t.Errorf("%s requests = %d, want exactly 1", tool.method, count(f, tool.method))
				}
			})
		}
	}
}

func TestBranchProtectionDeleteRemovesTheProtection(t *testing.T) {
	f, core := protectionCore(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }, nil)
	out, err := invoke(t, core, branchProtectionDelete.ID, "repo-listed", `{"branch":"main"}`, true)
	if err != nil || !strings.Contains(string(out), `"deleted":true`) || !strings.Contains(string(out), `"branch":"main"`) {
		t.Fatalf("delete = %s, %v", out, err)
	}
	if r := f.recorded(); len(r) != 1 || r[0].method != http.MethodDelete || r[0].path != protectionPath {
		t.Errorf("requests = %+v", r)
	}
}

func TestBranchRulesGetReadsEveryFieldAndPages(t *testing.T) {
	body := `[{"type":"pull_request","parameters":{"required_approving_review_count":1},` +
		`"ruleset_source_type":"Repository","ruleset_source":"octo-org/example","ruleset_id":42},` +
		`{"type":"deletion","ruleset_source_type":"Organization","ruleset_source":"octo-org","ruleset_id":7}]`
	f, core := protectionCore(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "" || r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", `<https://x/?page=2>; rel="next"`)
		}
		fmt.Fprint(w, body)
	}, nil)
	out, err := invoke(t, core, branchRulesGet.ID, "repo", `{"branch":"main","limit":2}`, false)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Branch     string `json:"branch"`
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
		Rules      []struct {
			Type              string         `json:"type"`
			Parameters        map[string]any `json:"parameters"`
			RulesetSourceType string         `json:"ruleset_source_type"`
			RulesetSource     string         `json:"ruleset_source"`
			RulesetID         int64          `json:"ruleset_id"`
		} `json:"rules"`
	}
	if json.Unmarshal(out, &got) != nil || len(got.Rules) != 2 || got.Rules[0].Type != "pull_request" ||
		got.Rules[0].Parameters["required_approving_review_count"] != float64(1) || got.Rules[0].RulesetID != 42 ||
		got.Rules[1].RulesetSourceType != "Organization" || !got.HasMore || got.NextCursor == "" {
		t.Fatalf("rules = %s", out)
	}
	if r := f.recorded(); len(r) != 1 || r[0].path != rulesPath || r[0].query != "page=1&per_page=2" {
		t.Errorf("requests = %+v", r)
	}
	if _, err := invoke(t, core, branchRulesGet.ID, "repo", `{"branch":"main","cursor":"`+got.NextCursor+`"}`, false); err != nil {
		t.Errorf("continuation = %v", err)
	}
	if _, err := invoke(t, core, branchRulesGet.ID, "repo", `{"branch":"other","cursor":"`+got.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("cursor of another branch = %v, want an invalid request", err)
	}
	for name, status := range map[string]int{"forbidden": 403, "not found": 404, "unprocessable": 422} {
		_, core := protectionCore(t, answer(status, `{"message":"SECRETTEXT"}`), nil)
		if _, err := invoke(t, core, branchRulesGet.ID, "repo", `{"branch":"main"}`, false); err == nil ||
			strings.Contains(err.Error(), "SECRETTEXT") {
			t.Errorf("%s = %v", name, err)
		}
	}
	_, core = protectionCore(t, answer(200, `[{"parameters":{}}]`), nil)
	if _, err := invoke(t, core, branchRulesGet.ID, "repo", `{"branch":"main"}`, false); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("rule without a type = %v", err)
	}
}

func TestBranchProtectionToolsRefuseForeignAndExcludedTargetsBeforeIO(t *testing.T) {
	reads := 0
	f, core := protectionCore(t, answer(200, protectedBranchJSON), &reads)
	set := `{"branch":"main",` + fullProtection + `,`
	for _, tt := range []struct{ id, connection, arguments string }{
		{branchProtectionGet.ID, "repo", `{"branch":"main","repository":"other-org/other"}`},
		{branchProtectionSet.ID, "repo-listed", set + `"repository":"other-org/other"}`},
		{branchProtectionDelete.ID, "repo-listed", `{"branch":"main","repository":"other-org/other"}`},
		{branchRulesGet.ID, "repo", `{"branch":"main","repository":"other-org/other"}`},
		{branchProtectionGet.ID, "excluded", `{"branch":"main","repository":"octo-org/example"}`},
		{branchProtectionSet.ID, "excluded", set + `"repository":"octo-org/example"}`},
		{branchProtectionDelete.ID, "excluded", `{"branch":"main","repository":"octo-org/example"}`},
		{branchRulesGet.ID, "excluded", `{"branch":"main","repository":"octo-org/example"}`},
	} {
		reads = 0
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true); err == nil ||
			strings.Contains(err.Error(), "other-org") {
			t.Errorf("%s on %s = %v, want a refusal without the foreign name", tt.id, tt.connection, err)
		}
		if reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("%s on %s reached the credential or GitHub", tt.id, tt.connection)
		}
	}
}

func TestBranchProtectionChangesAreListedOnlyAndConfirmed(t *testing.T) {
	reads := 0
	f, core := protectionCore(t, answer(200, protectedBranchJSON), &reads)
	var unsupported *capability.UnsupportedError
	for _, tt := range []struct{ id, arguments string }{
		{branchProtectionSet.ID, `{"branch":"main",` + fullProtection + `}`},
		{branchProtectionDelete.ID, `{"branch":"main"}`},
	} {
		if _, err := core.Describe(application.DescribeRequest{Operation: tt.id, Connection: "repo"}); !errors.As(err, &unsupported) {
			t.Errorf("describe %s without a tools entry = %v, want unsupported", tt.id, err)
		}
		reads = 0
		if _, err := invoke(t, core, tt.id, "repo", tt.arguments, true); !errors.As(err, &unsupported) {
			t.Errorf("invoke %s without a tools entry = %v, want unsupported", tt.id, err)
		}
		if reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("%s without a tools entry reached the credential or GitHub", tt.id)
		}
		var needed *application.ConfirmationRequiredError
		if _, err := invoke(t, core, tt.id, "repo-listed", tt.arguments, false); !errors.As(err, &needed) {
			t.Errorf("%s unconfirmed = %v, want confirmation-required", tt.id, err)
		}
	}
	for _, id := range []string{branchProtectionGet.ID, branchRulesGet.ID} {
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "repo"}); err != nil {
			t.Errorf("describe %s = %v, want offered without a tools entry", id, err)
		}
	}
}
