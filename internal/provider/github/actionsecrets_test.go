package github

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
	"golang.org/x/crypto/nacl/box"
)

var secretReadIDs = []string{actionSecretsList.ID, actionSecretsGet.ID, actionSecretRepositoriesList.ID,
	dependabotSecretsList.ID, dependabotSecretsGet.ID, dependabotSecretRepositoriesList.ID}

// secretValueCanary stands for a value or an encrypted value a server adds to an answer; it must never reach an
// output. secretProviderCanary stands for GitHub's own text of a refusal.
const (
	secretValueCanary    = "secret-value-canary-5e1f"
	secretProviderCanary = "secret-provider-canary-b7c4"
)

// fakeSecrets answers the Actions secret routes of repository octo-org/example, its environments, and
// organization octo-org. Every secret it holds carries a value and an encrypted value that GitHub never sends.
type fakeSecrets struct {
	mu    sync.Mutex
	f     *fakeGitHub
	store map[string]map[string]map[string]any
	order map[string][]string
	// status, when set, answers every secret route with this status and GitHub's own text.
	status int
	// paths are the escaped request paths of the secret routes.
	paths []string
	// The public key every level serves and its private half, for tests that open what a set sends.
	publicKey, privateKey *[32]byte
	// keyAnswer replaces the answer of the public key route when it is set.
	keyAnswer string
	// putStatus, when set, answers every write of a secret with this status and GitHub's own text; puts holds
	// the bodies of the writes that were accepted.
	putStatus int
	puts      []map[string]any
}

func serveSecrets(t *testing.T) (*fakeGitHub, *fakeSecrets, string) {
	t.Helper()
	s := &fakeSecrets{store: map[string]map[string]map[string]any{}, order: map[string][]string{}}
	s.publicKey, s.privateKey, _ = box.GenerateKey(rand.Reader)
	s.put("repos/octo-org/example", map[string]any{"name": "DEPLOY_TOKEN", "created_at": "2026-01-02T03:04:05Z",
		"updated_at": "2026-01-03T03:04:05Z"})
	s.put("env:production", map[string]any{"name": "PROD_KEY", "created_at": "2026-01-02T03:04:05Z",
		"updated_at": "2026-01-03T03:04:05Z"})
	s.put("orgs/octo-org", map[string]any{"name": "SHARED_TOKEN", "created_at": "2026-01-02T03:04:05Z",
		"updated_at": "2026-01-03T03:04:05Z", "visibility": "selected",
		"selected_repositories_url": "https://api.github.com/orgs/octo-org/actions/secrets/SHARED_TOKEN/repositories"})
	s.put("dependabot:repos/octo-org/example", map[string]any{"name": "REGISTRY_TOKEN",
		"created_at": "2026-02-02T03:04:05Z", "updated_at": "2026-02-03T03:04:05Z"})
	s.put("dependabot:orgs/octo-org", map[string]any{"name": "SHARED_REGISTRY", "created_at": "2026-02-02T03:04:05Z",
		"updated_at": "2026-02-03T03:04:05Z", "visibility": "selected",
		"selected_repositories_url": "https://api.github.com/orgs/octo-org/dependabot/secrets/SHARED_REGISTRY/repositories"})
	f := &fakeGitHub{}
	s.f = f
	f.failure = s.route
	return f, s, serve(t, f)
}

func (s *fakeSecrets) put(level string, secret map[string]any) {
	secret["value"] = secretValueCanary
	secret["encrypted_value"] = secretValueCanary
	if s.store[level] == nil {
		s.store[level] = map[string]map[string]any{}
	}
	key := strings.ToUpper(secret["name"].(string))
	if _, exists := s.store[level][key]; !exists {
		s.order[level] = append(s.order[level], key)
	}
	s.store[level][key] = secret
}

func secretLevel(escaped string) (level, rest string, ok bool) {
	if tail, found := strings.CutPrefix(escaped, "/api/v3/orgs/octo-org/dependabot/secrets"); found {
		return "dependabot:orgs/octo-org", tail, true
	}
	if tail, found := strings.CutPrefix(escaped, "/api/v3/repos/octo-org/example/dependabot/secrets"); found {
		return "dependabot:repos/octo-org/example", tail, true
	}
	if tail, found := strings.CutPrefix(escaped, "/api/v3/orgs/octo-org/actions/secrets"); found {
		return "orgs/octo-org", tail, true
	}
	if tail, found := strings.CutPrefix(escaped, "/api/v3/repos/octo-org/example/actions/secrets"); found {
		return "repos/octo-org/example", tail, true
	}
	if tail, found := strings.CutPrefix(escaped, "/api/v3/repos/octo-org/example/environments/"); found {
		segment, after, cut := strings.Cut(tail, "/")
		environment, err := url.PathUnescape(segment)
		if rest, isSecrets := strings.CutPrefix(after, "secrets"); cut && err == nil && isSecrets {
			return "env:" + environment, rest, true
		}
	}
	return "", "", false
}

// secretRepository is a minimal-repository entry with a token GitHub may add.
func secretRepository(id int, fullName string) string {
	owner, name, _ := strings.Cut(fullName, "/")
	return fmt.Sprintf(`{"id":%d,"node_id":"R_%d","name":%q,"full_name":%q,"private":true,"owner":{"login":%q,`+
		`"id":1,"type":"Organization"},"html_url":"https://github.com/%s","description":"d","fork":false,`+
		`"temp_clone_token":%q}`, id, id, name, fullName, owner, fullName, secretValueCanary)
}

func (s *fakeSecrets) route(w http.ResponseWriter, req *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	escaped := req.URL.EscapedPath()
	if repository, found := strings.CutPrefix(escaped, "/api/v3/repos/"); found && req.Method == http.MethodGet {
		if id, known := map[string]int{"octo-org/example": 1296269, "octo-org/tools": 1296270}[strings.ToLower(repository)]; known {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":%d,"full_name":%q}`, id, strings.ToLower(repository))
			return true
		}
	}
	level, rest, ok := secretLevel(escaped)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	s.paths = append(s.paths, escaped)
	if s.status != 0 {
		w.WriteHeader(s.status)
		fmt.Fprintf(w, `{"message":%q}`, secretProviderCanary)
		return true
	}
	name, tail, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
	name, _ = url.PathUnescape(name)
	key := strings.ToUpper(name)
	notFound := func() bool {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
		return true
	}
	switch {
	case req.Method == http.MethodGet && rest == "/public-key":
		if s.keyAnswer != "" {
			fmt.Fprint(w, s.keyAnswer)
			return true
		}
		fmt.Fprintf(w, `{"key_id":"012345678","key":%q}`, base64.StdEncoding.EncodeToString(s.publicKey[:]))
	case req.Method == http.MethodPut && tail == "":
		// The fake has read the request body already and recorded it.
		requests := s.f.recorded()
		body := requests[len(requests)-1].body
		if s.putStatus != 0 {
			w.WriteHeader(s.putStatus)
			fmt.Fprintf(w, `{"message":%q,"echo":%q}`, secretProviderCanary, body["encrypted_value"])
			return true
		}
		s.puts = append(s.puts, body)
		_, exists := s.store[level][key]
		s.put(level, map[string]any{"name": name})
		if exists {
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{}`)
	case req.Method == http.MethodGet && rest == "":
		perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		keys := s.order[level]
		start := min((page-1)*perPage, len(keys))
		end := min(start+perPage, len(keys))
		entries := make([]string, 0, end-start)
		for _, k := range keys[start:end] {
			data, _ := json.Marshal(s.store[level][k])
			entries = append(entries, string(data))
		}
		fmt.Fprintf(w, `{"total_count":%d,"secrets":[%s]}`, len(keys), strings.Join(entries, ","))
	case req.Method == http.MethodGet && tail == "repositories" && strings.HasSuffix(level, "orgs/octo-org"):
		if _, exists := s.store[level][key]; !exists {
			return notFound()
		}
		perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		all := []string{secretRepository(1, "octo-org/example"), secretRepository(2, "octo-org/secret"),
			secretRepository(3, "other-org/foreign")}
		start := min((page-1)*perPage, len(all))
		end := min(start+perPage, len(all))
		fmt.Fprintf(w, `{"total_count":3,"repositories":[%s]}`, strings.Join(all[start:end], ","))
	case req.Method == http.MethodGet && tail == "":
		secret, exists := s.store[level][key]
		if !exists {
			return notFound()
		}
		data, _ := json.Marshal(secret)
		w.Write(data)
	case req.Method == http.MethodDelete && tail == "":
		if _, exists := s.store[level][key]; !exists {
			return notFound()
		}
		delete(s.store[level], key)
		s.order[level] = slicesWithout(s.order[level], key)
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

var secretTools = []string{actionSecretsList.ID, actionSecretsGet.ID, actionSecretRepositoriesList.ID,
	actionSecretsDelete.ID, actionSecretsSet.ID, dependabotSecretsSet.ID, dependabotSecretsList.ID, dependabotSecretsGet.ID, dependabotSecretRepositoriesList.ID,
	dependabotSecretsDelete.ID}

// secretsConfig binds the connection shapes the secret tools distinguish, like variablesConfig.
func secretsConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all}
	cfg.Connections["repo-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: secretTools}
	cfg.Connections["org-listed"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Exclude: []string{"repos/octo-org/secret"},
		Effects: all, Tools: secretTools}
	cfg.Connections["org-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: all, Tools: secretTools}
	cfg.Connections["open-excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: all, Tools: secretTools}
	return cfg
}

func secretsCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), secretsConfig(base), resolver(red, reads), red)
}

// Every secret tool satisfies its contract at each level, deletes with exactly one request to a fixed route, and
// no output carries a value a server adds.
func TestActionSecretsToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, s, base := serveSecrets(t)
	core := secretsCore(t, base, nil)
	for _, tt := range []struct {
		level, connection, target, located, existing, route string
	}{
		{"repository", "repo-listed", `"repository":"octo-org/example"`, `"repository":"octo-org/example"`,
			"DEPLOY_TOKEN", "/api/v3/repos/octo-org/example/actions/secrets"},
		{"environment", "repo-listed", `"repository":"octo-org/example","environment":"production"`,
			`"environment":"production"`, "PROD_KEY", "/api/v3/repos/octo-org/example/environments/production/secrets"},
		{"organization", "org-listed", `"organization":"orgs/octo-org"`, `"organization":"orgs/octo-org"`,
			"SHARED_TOKEN", "/api/v3/orgs/octo-org/actions/secrets"},
	} {
		listed, err := invoke(t, core, actionSecretsList.ID, tt.connection, `{`+tt.target+`}`, false)
		if err != nil || !strings.Contains(string(listed), `"name":"`+tt.existing+`"`) ||
			!strings.Contains(string(listed), `"total_count":1`) || !strings.Contains(string(listed), tt.located) ||
			strings.Contains(string(listed), secretValueCanary) {
			t.Fatalf("%s list = %s, %v", tt.level, listed, err)
		}
		got, err := invoke(t, core, actionSecretsGet.ID, tt.connection, `{`+tt.target+`,"name":"`+tt.existing+`"}`, false)
		if err != nil || !strings.Contains(string(got), `"created_at":"2026-01-02T03:04:05Z"`) ||
			!strings.Contains(string(got), `"updated_at":"2026-01-03T03:04:05Z"`) || !strings.Contains(string(got), tt.located) ||
			strings.Contains(string(got), secretValueCanary) || strings.Contains(string(got), `"value"`) ||
			strings.Contains(string(got), "encrypted_value") {
			t.Fatalf("%s get = %s, %v", tt.level, got, err)
		}
		if tt.level == "organization" && (!strings.Contains(string(got), `"visibility":"selected"`) ||
			!strings.Contains(string(got), `"selected_repositories_url":"https://api.github.com/orgs/`)) {
			t.Errorf("organization get = %s, want visibility and selected_repositories_url", got)
		}
		before := len(f.recorded())
		deleted, err := invoke(t, core, actionSecretsDelete.ID, tt.connection, `{`+tt.target+`,"name":"`+tt.existing+`"}`, true)
		if err != nil || !strings.Contains(string(deleted), `"deleted":true`) || !strings.Contains(string(deleted), tt.located) ||
			strings.Contains(string(deleted), secretValueCanary) {
			t.Fatalf("%s delete = %s, %v", tt.level, deleted, err)
		}
		if requests := f.recorded()[before:]; len(requests) != 1 || requests[0].method != http.MethodDelete ||
			requests[0].path != tt.route+"/"+tt.existing {
			t.Fatalf("%s delete requests = %+v, want one DELETE to %s", tt.level, requests, tt.route)
		}
	}
	if len(s.store["orgs/octo-org"]) != 0 {
		t.Errorf("the organization secret was not deleted")
	}
}

// The repositories of an organization secret lie inside the targets or are counted as withheld; GitHub's
// temporary clone token and any value never appear.
func TestActionSecretRepositoriesWithholdForeignRepositories(t *testing.T) {
	f, _, base := serveSecrets(t)
	core := secretsCore(t, base, nil)
	listed, err := invoke(t, core, actionSecretRepositoriesList.ID, "org-listed",
		`{"organization":"orgs/octo-org","name":"shared_token"}`, false)
	if err != nil {
		t.Fatalf("list = %v", err)
	}
	var page ActionSecretRepositoryList
	if json.Unmarshal(listed, &page) != nil || len(page.Repositories) != 1 || page.Withheld != 2 ||
		page.TotalCount != 3 || *page.Repositories[0].FullName != "octo-org/example" {
		t.Fatalf("list = %s, want example listed and two withheld", listed)
	}
	for _, leak := range []string{secretValueCanary, "octo-org/secret", "other-org", "temp_clone_token"} {
		if strings.Contains(string(listed), leak) {
			t.Errorf("list leaks %q: %s", leak, listed)
		}
	}
	if requests := f.recorded(); requests[len(requests)-1].path != "/api/v3/orgs/octo-org/actions/secrets/shared_token/repositories" {
		t.Errorf("path = %s", requests[len(requests)-1].path)
	}
	// An owner target alone admits every repository of the organization that is not excluded.
	listed, err = invoke(t, core, actionSecretRepositoriesList.ID, "org-only",
		`{"organization":"orgs/octo-org","name":"SHARED_TOKEN"}`, false)
	if err != nil || json.Unmarshal(listed, &page) != nil || len(page.Repositories) != 2 || page.Withheld != 1 {
		t.Fatalf("org-only list = %s, %v", listed, err)
	}
	// A secret GitHub does not hold is not found, and a repository level is not offered.
	if _, err := invoke(t, core, actionSecretRepositoriesList.ID, "org-listed",
		`{"organization":"orgs/octo-org","name":"MISSING"}`, false); classOf(err) != provider.ClassNotFound {
		t.Errorf("missing secret = %v, want not-found", err)
	}
	if _, err := invoke(t, core, actionSecretRepositoriesList.ID, "org-listed",
		`{"repository":"octo-org/example","name":"SHARED_TOKEN"}`, false); !isInvalidRequest(err) {
		t.Errorf("repository level = %v, want an invalid request", err)
	}
	first, err := invoke(t, core, actionSecretRepositoriesList.ID, "org-only",
		`{"organization":"orgs/octo-org","name":"SHARED_TOKEN","limit":1}`, false)
	var paged struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	if err != nil || json.Unmarshal(first, &paged) != nil || !paged.HasMore || paged.NextCursor == "" {
		t.Fatalf("first page = %s, %v", first, err)
	}
	second, err := invoke(t, core, actionSecretRepositoriesList.ID, "org-only",
		`{"organization":"orgs/octo-org","name":"SHARED_TOKEN","cursor":"`+paged.NextCursor+`"}`, false)
	if err != nil || !strings.Contains(string(second), "octo-org/secret") {
		t.Fatalf("second page = %s, %v", second, err)
	}
	// A cursor of another secret does not continue this list.
	if _, err := invoke(t, core, actionSecretRepositoriesList.ID, "org-only",
		`{"organization":"orgs/octo-org","name":"OTHER","cursor":"`+paged.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("foreign cursor = %v, want an invalid request", err)
	}
}

// The secret list is paged and bound to its level.
func TestActionSecretsListPages(t *testing.T) {
	f, s, base := serveSecrets(t)
	for i := 0; i < 4; i++ {
		s.put("repos/octo-org/example", map[string]any{"name": fmt.Sprintf("SECRET_%d", i)})
	}
	core := secretsCore(t, base, nil)
	first, err := invoke(t, core, actionSecretsList.ID, "repo", `{"repository":"octo-org/example","limit":2}`, false)
	var page struct {
		Secrets    []ActionSecret `json:"secrets"`
		NextCursor string         `json:"next_cursor"`
		HasMore    bool           `json:"has_more"`
		TotalCount int            `json:"total_count"`
	}
	if err != nil || json.Unmarshal(first, &page) != nil || !page.HasMore || page.TotalCount != 5 || len(page.Secrets) != 2 {
		t.Fatalf("first page = %s, %v", first, err)
	}
	second, err := invoke(t, core, actionSecretsList.ID, "repo",
		`{"repository":"octo-org/example","cursor":"`+page.NextCursor+`"}`, false)
	if err != nil || !strings.Contains(string(second), `"name":"SECRET_1"`) {
		t.Fatalf("second page = %s, %v", second, err)
	}
	if requests := f.recorded(); !strings.Contains(requests[len(requests)-1].query, "page=2") ||
		!strings.Contains(requests[len(requests)-1].query, "per_page=2") {
		t.Errorf("continuation query = %q", requests[len(requests)-1].query)
	}
	if _, err := invoke(t, core, actionSecretsList.ID, "repo",
		`{"repository":"octo-org/example","environment":"production","cursor":"`+page.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("foreign cursor = %v, want an invalid request", err)
	}
	if _, err := invoke(t, core, actionSecretsList.ID, "repo", `{"repository":"octo-org/example","limit":101}`, false); !isInvalidRequest(err) {
		t.Errorf("limit 101 = %v, want an invalid request", err)
	}
}

// repository and organization are exclusive and environment belongs to a repository; every refusal, a target
// outside the connection, and an excluded target end before a credential is resolved.
func TestActionSecretsTargetIsExclusiveAndCheckedBeforeIO(t *testing.T) {
	f, _, base := serveSecrets(t)
	reads := 0
	core := secretsCore(t, base, &reads)
	for _, tt := range []struct {
		name, id, connection, arguments string
	}{
		{"neither", actionSecretsList.ID, "repo", `{}`},
		{"both", actionSecretsList.ID, "org-listed", `{"repository":"octo-org/example","organization":"orgs/octo-org"}`},
		{"environment without repository", actionSecretsList.ID, "repo", `{"environment":"production"}`},
		{"environment with organization", actionSecretsGet.ID, "org-listed",
			`{"organization":"orgs/octo-org","environment":"production","name":"X"}`},
		{"an organization outside the targets", actionSecretsList.ID, "repo", `{"organization":"orgs/octo-org"}`},
		{"a user owner", actionSecretsList.ID, "open-excluding", `{"organization":"users/octocat"}`},
		{"a repository outside the targets", actionSecretsGet.ID, "repo", `{"repository":"other-org/example","name":"X"}`},
		{"an excluded repository", actionSecretsList.ID, "org-listed", `{"repository":"octo-org/secret"}`},
		{"an excluded organization", actionSecretsDelete.ID, "open-excluding", `{"organization":"orgs/octo-org","name":"X"}`},
		{"a repository of an excluded organization", actionSecretsDelete.ID, "open-excluding",
			`{"repository":"octo-org/example","name":"X"}`},
		{"an organization outside the targets for its repositories", actionSecretRepositoriesList.ID, "repo",
			`{"organization":"orgs/octo-org","name":"X"}`},
		{"an excluded organization for its repositories", actionSecretRepositoriesList.ID, "open-excluding",
			`{"organization":"orgs/octo-org","name":"X"}`},
	} {
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// Names follow GitHub's rules and an environment stays one plain path segment; every refusal ends before a
// credential is resolved.
func TestActionSecretsArgumentsAreValidatedBeforeIO(t *testing.T) {
	f, _, base := serveSecrets(t)
	reads := 0
	core := secretsCore(t, base, &reads)
	repo := `"repository":"octo-org/example"`
	for _, tt := range []struct {
		name, id, arguments string
	}{
		{"a name starting with a digit", actionSecretsGet.ID, `{` + repo + `,"name":"1ABC"}`},
		{"a name with a hyphen", actionSecretsGet.ID, `{` + repo + `,"name":"A-B"}`},
		{"a name with a slash", actionSecretsDelete.ID, `{` + repo + `,"name":"A/../B"}`},
		{"a name with a dot segment", actionSecretsDelete.ID, `{` + repo + `,"name":".."}`},
		{"the GITHUB_ prefix", actionSecretsGet.ID, `{` + repo + `,"name":"github_x"}`},
		{"an empty name", actionSecretsGet.ID, `{` + repo + `,"name":""}`},
		{"the environment ..", actionSecretsList.ID, `{` + repo + `,"environment":".."}`},
		{"an environment with a control character", actionSecretsList.ID, `{` + repo + `,"environment":"a\nb"}`},
		{"an environment over 255 characters", actionSecretsList.ID, `{` + repo + `,"environment":"` + strings.Repeat("e", 256) + `"}`},
		{"an empty environment", actionSecretsList.ID, `{` + repo + `,"environment":""}`},
		{"a value argument", actionSecretsGet.ID, `{` + repo + `,"name":"A","value":"` + secretValueCanary + `"}`},
		{"a name for the list", actionSecretsList.ID, `{` + repo + `,"name":"A"}`},
		{"a repository name for the repositories", actionSecretRepositoriesList.ID,
			`{"organization":"orgs/octo-org","name":"A/B"}`},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, "repo-listed", tt.arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if err != nil && strings.Contains(err.Error(), secretValueCanary) {
			t.Errorf("%s quotes the value: %v", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// An environment name and a secret name of plain text travel as exactly one escaped path segment, and a
// refusal names them.
func TestActionSecretsEnvironmentIsOnePathSegment(t *testing.T) {
	_, s, base := serveSecrets(t)
	core := secretsCore(t, base, nil)
	_, err := invoke(t, core, actionSecretsGet.ID, "repo",
		`{"repository":"octo-org/example","environment":"prod/eu west ü?#%","name":"PROD_KEY"}`, false)
	if classOf(err) != provider.ClassNotFound ||
		!strings.Contains(err.Error(), "secret PROD_KEY of environment prod/eu west ü?#% in repository octo-org/example") {
		t.Fatalf("get in a missing environment = %v, want not-found naming the environment", err)
	}
	want := "/api/v3/repos/octo-org/example/environments/prod%2Feu%20west%20%C3%BC%3F%23%25/secrets/PROD_KEY"
	if got := s.paths[len(s.paths)-1]; got != want {
		t.Errorf("path = %s, want %s", got, want)
	}
}

// A refusal names what the request needs in Qatlas' own words; GitHub's text never reaches the message, and a
// delete is sent once, also after a timeout.
func TestActionSecretsFailuresAreNamed(t *testing.T) {
	f, s, base := serveSecrets(t)
	core := secretsCore(t, base, nil)
	for _, level := range []struct{ name, connection, target, right string }{
		{"repository", "repo-listed", `"repository":"octo-org/example"`, "Secrets: read"},
		{"environment", "repo-listed", `"repository":"octo-org/example","environment":"production"`, "Secrets: read"},
		{"organization", "org-listed", `"organization":"orgs/octo-org"`, "admin:org"},
	} {
		for _, tool := range []struct{ id, arguments string }{
			{actionSecretsList.ID, `{` + level.target + `}`},
			{actionSecretsGet.ID, `{` + level.target + `,"name":"FRESH"}`},
			{actionSecretsDelete.ID, `{` + level.target + `,"name":"FRESH"}`},
		} {
			confirmed := tool.id == actionSecretsDelete.ID
			for _, tt := range []struct {
				status    int
				class     provider.Class
				fragment  string
				uncertain bool
			}{
				{http.StatusForbidden, provider.ClassPermission, level.right, false},
				{http.StatusNotFound, provider.ClassNotFound, "does not hold", false},
				{http.StatusUnprocessableEntity, "", "", false},
				{http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", confirmed},
			} {
				s.status = tt.status
				before := len(f.recorded())
				_, err := invoke(t, core, tool.id, level.connection, tool.arguments, confirmed)
				if err == nil || (tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.fragment) ||
					strings.Contains(err.Error(), uncertain) != tt.uncertain {
					t.Errorf("%s %s answered %d = %v, want %s naming %q", level.name, tool.id, tt.status, err, tt.class, tt.fragment)
				}
				if err != nil && strings.Contains(err.Error(), secretProviderCanary) {
					t.Errorf("%s %s answered %d quotes GitHub's text: %v", level.name, tool.id, tt.status, err)
				}
				if requests := f.recorded()[before:]; len(requests) != 1 {
					t.Errorf("%s %s answered %d sent %d requests, want exactly one", level.name, tool.id, tt.status, len(requests))
				}
			}
			s.status = http.StatusForbidden
			_, err := invoke(t, core, tool.id, level.connection, tool.arguments, confirmed)
			if (tool.id == actionSecretsDelete.ID) != strings.Contains(err.Error(), "write") {
				t.Errorf("%s %s refused = %v, want write named for the delete only", level.name, tool.id, err)
			}
			s.status = 0
		}
	}
}

// The delete is offered only where a connection's tools list names it, and needs confirmation; both refusals
// end before a secret is read.
func TestActionSecretsDeleteIsListedOnlyAndNeedsConfirmation(t *testing.T) {
	f, _, base := serveSecrets(t)
	reads := 0
	core := secretsCore(t, base, &reads)
	var unsupported *capability.UnsupportedError
	if _, err := core.Describe(application.DescribeRequest{Operation: actionSecretsDelete.ID, Connection: "repo"}); !errors.As(err, &unsupported) {
		t.Errorf("describe delete on repo = %v, want unsupported", err)
	}
	if _, err := invoke(t, core, actionSecretsDelete.ID, "repo", `{"repository":"octo-org/example","name":"DEPLOY_TOKEN"}`, true); !errors.As(err, &unsupported) {
		t.Errorf("invoke delete on repo = %v, want unsupported", err)
	}
	for _, id := range secretReadIDs {
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "repo"}); err != nil {
			t.Errorf("describe %s on repo = %v, want it offered by effects", id, err)
		}
	}
	unconfirmed := &application.ConfirmationRequiredError{}
	if _, err := invoke(t, core, actionSecretsDelete.ID, "repo-listed", `{"repository":"octo-org/example","name":"A"}`, false); !errors.As(err, &unconfirmed) {
		t.Errorf("delete without confirmation = %v, want confirmation-required", err)
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}

// A secret the server answers with a value, an encrypted value, or any other member the schema lacks never
// shows that member in an output.
func TestActionSecretsDropAnyValueAMemberAdds(t *testing.T) {
	_, s, base := serveSecrets(t)
	s.put("repos/octo-org/example", map[string]any{"name": "EXTRA", "key_id": "012345", "plaintext": secretValueCanary})
	core := secretsCore(t, base, nil)
	listed, err := invoke(t, core, actionSecretsList.ID, "repo", `{"repository":"octo-org/example"}`, false)
	got, err2 := invoke(t, core, actionSecretsGet.ID, "repo", `{"repository":"octo-org/example","name":"EXTRA"}`, false)
	if err != nil || err2 != nil {
		t.Fatalf("list = %v, get = %v", err, err2)
	}
	for _, output := range []string{string(listed), string(got)} {
		for _, leak := range []string{secretValueCanary, "encrypted_value", "key_id", "plaintext", `"value"`} {
			if strings.Contains(output, leak) {
				t.Errorf("output leaks %q: %s", leak, output)
			}
		}
	}
}

// A refusal of a secret route names the secret by its checked name, below a repository, an environment, or an
// organization.
func TestSecretSubjectNamesOnlyCheckedNames(t *testing.T) {
	for path, want := range map[string]string{
		"actions/secrets":                     "the Actions secrets",
		"actions/secrets/DEPLOY":              "Actions secret DEPLOY",
		"actions/secrets/bad%2Fname":          "a secret",
		"actions/secrets/DEPLOY/repositories": "the repositories of Actions secret DEPLOY",
		"actions/secrets/a%0Ab/repositories":  "the repositories of a secret",
		"actions/secretsX":                    "",
		"environments/prod%2Feu/secrets":      "the secrets of environment prod/eu",
		"environments/prod/secrets/DEPLOY":    "secret DEPLOY of environment prod",
		"environments/a%0Ab/secrets/DEPLOY":   "secret DEPLOY of an environment",
		"environments/prod/variables/DEPLOY":  "",
		"actions/variables/DEPLOY":            "",
		"actions/runs/1":                      "",
	} {
		if got := actionSecretFamily.subject(path); got != want {
			t.Errorf("subject(%q) = %q, want %q", path, got, want)
		}
	}
	c := &Client{endpoints: endpoints{rest: "https://api.github.com"}}
	request, _ := http.NewRequest(http.MethodGet, "https://api.github.com/orgs/octo-org/actions/secrets/SHARED/repositories", nil)
	if got := c.restSubject(request).String(); got != "the repositories of Actions secret SHARED of orgs/octo-org" {
		t.Errorf("organization subject = %q", got)
	}
}
