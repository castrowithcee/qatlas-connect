package github

import (
	"errors"
	"fmt"
	"net/http"
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

const (
	appTokenCanary   = "installation-token-address-canary-7c2a"
	appSecretCanary  = "app-secret-canary-91be"
	appHookCanary    = "webhook-configuration-canary-3d4f"
	appTextCanary    = "app-provider-text-canary-5a60"
	appRepositoryID  = 4242
	appInstallPrefix = "/api/v3/orgs/octo-org/installations"
)

// installationJSON is an installation as GitHub documents it, plus members that must never reach a result.
func installationJSON(id int64, slug, selection string) string {
	return fmt.Sprintf(`{"id":%d,"account":{"login":"octo-org","id":7,"node_id":"O_7","type":"Organization",`+
		`"site_admin":false,"avatar_url":"https://avatars.example/7","html_url":"https://github.com/octo-org",`+
		`"url":"https://api.github.com/users/octo-org","repos_url":"https://api.github.com/users/octo-org/repos"},`+
		`"repository_selection":%q,"access_tokens_url":"https://api.github.com/%s/access_tokens",`+
		`"repositories_url":"https://api.github.com/installation/repositories","html_url":"https://github.com/apps/%s",`+
		`"app_id":55,"client_id":"Iv1.abc","target_id":7,"target_type":"Organization",`+
		`"permissions":{"contents":"read","issues":"write"},"events":["push","issues"],`+
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-02-01T00:00:00Z","single_file_name":null,`+
		`"has_multiple_single_files":false,"single_file_paths":[],"app_slug":%q,`+
		`"suspended_by":{"login":"hubot","id":9,"type":"User","name":"Hu Bot"},"suspended_at":"2026-03-01T00:00:00Z",`+
		`"contact_email":"owner@example.com","webhook_config":{"secret":%q},"token":%q}`,
		id, selection, appTokenCanary, slug, slug, appHookCanary, appSecretCanary)
}

var appJSON = fmt.Sprintf(`{"id":55,"slug":"example-app","node_id":"A_55","client_id":"Iv1.abc","owner":{"login":"octo-org",`+
	`"id":7,"type":"Organization","slug":"ignored-slug","website_url":"https://example.com"},"name":"Example App",`+
	`"description":"Does things. app-provider-text-canary-5a60","external_url":"https://example.com/app",`+
	`"html_url":"https://github.com/apps/example-app","created_at":"2026-01-01T00:00:00Z",`+
	`"updated_at":"2026-02-01T00:00:00Z","permissions":{"contents":"read","metadata":"read"},`+
	`"events":["push"],"installations_count":12,"client_secret":%[1]q,"webhook_secret":%[1]q,"pem":%[1]q}`,
	appSecretCanary)

// fakeApps answers the app routes of octo-org through the failure hook.
type fakeApps struct {
	mu            sync.Mutex
	installations []string
	// total replaces total_count when set.
	total int
	// ownerType is the type of the owner of octo-org/example.
	ownerType string
	// repoName replaces the name the repository read answers with.
	repoName string
	// status answers every installation list read with this status.
	status int
	// repoStatus answers the repository read with this status.
	repoStatus int
	// deleteAnswer replaces the answer of the DELETE.
	deleteAnswer func(http.ResponseWriter)
	// appStatus answers the app read with this status.
	appStatus int
	// noLink leaves the Link header out of the installation list.
	noLink bool
}

func serveApps(t *testing.T) (*fakeGitHub, *fakeApps, string) {
	t.Helper()
	a := &fakeApps{ownerType: "Organization", repoName: "example"}
	a.installations = []string{installationJSON(100, "example-app", "selected"), installationJSON(101, "other-app", "all")}
	f := &fakeGitHub{}
	f.failure = a.route
	return f, a, serve(t, f)
}

func (a *fakeApps) route(w http.ResponseWriter, req *http.Request) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	path := req.URL.Path
	refuse := func(status int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"message":%q}`, appTextCanary)
	}
	switch {
	case path == "/api/v3/repos/octo-org/example" && req.Method == http.MethodGet:
		if a.repoStatus != 0 {
			refuse(a.repoStatus)
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":%d,"name":%q,"full_name":"octo-org/%s","owner":{"login":"octo-org","type":%q}}`,
			appRepositoryID, a.repoName, a.repoName, a.ownerType)
	case path == appInstallPrefix && req.Method == http.MethodGet:
		if a.status != 0 {
			refuse(a.status)
			return true
		}
		perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		start := min((page-1)*perPage, len(a.installations))
		end := min(start+perPage, len(a.installations))
		if end < len(a.installations) && !a.noLink {
			w.Header().Set("Link", `<https://api.github.com/x?page=`+strconv.Itoa(page+1)+`>; rel="next"`)
		}
		total := len(a.installations)
		if a.total != 0 {
			total = a.total
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"total_count":%d,"installations":[%s]}`, total, strings.Join(a.installations[start:end], ","))
	case strings.HasPrefix(path, "/api/v3/apps/") && req.Method == http.MethodGet:
		if a.appStatus != 0 {
			refuse(a.appStatus)
			return true
		}
		if path != "/api/v3/apps/example-app" {
			refuse(http.StatusNotFound)
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, appJSON)
	case strings.HasPrefix(path, "/api/v3/user/installations/") && req.Method == http.MethodDelete:
		if a.deleteAnswer != nil {
			a.deleteAnswer(w)
			return true
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

func appsConfig(base string) *config.Config {
	cfg := coreConfig(base)
	read := []config.Effect{config.EffectRead}
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	remove := []string{appInstallationRepositoriesRemove.ID}
	cfg.Connections["org"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Effects: read}
	cfg.Connections["global"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: read}
	cfg.Connections["owner-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: read}
	cfg.Connections["repo-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: read}
	cfg.Connections["user"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"users/octocat"}, Effects: read}
	cfg.Connections["excluded-owner"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: read}
	cfg.Connections["unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: all}
	cfg.Connections["listed"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: all, Tools: remove}
	cfg.Connections["foreign"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/other"}, Effects: all, Tools: remove}
	cfg.Connections["excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Exclude: []string{"repos/octo-org/example"},
		Effects: all, Tools: remove}
	return cfg
}

func appsCore(t *testing.T) (*fakeGitHub, *fakeApps, *application.Core, *int) {
	t.Helper()
	f, a, base := serveApps(t)
	reads := 0
	red := &redact.Redactor{}
	return f, a, application.New(registry(t), appsConfig(base), resolver(red, &reads), red), &reads
}

func TestAppToolsSatisfyTheirContractThroughTheApplicationCore(t *testing.T) {
	f, _, core, _ := appsCore(t)

	before := len(f.recorded())
	list, err := invoke(t, core, appInstallationsList.ID, "org", `{"owner":"orgs/octo-org","limit":10}`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"owner":"orgs/octo-org"`, `"total_count":2`, `"id":100`, `"app_slug":"example-app"`,
		`"repository_selection":"selected"`, `"repositories_url":"https://api.github.com/installation/repositories"`,
		`"html_url":"https://github.com/apps/example-app"`, `"app_id":55`, `"client_id":"Iv1.abc"`, `"target_id":7`,
		`"target_type":"Organization"`, `"permissions":{"contents":"read","issues":"write"}`, `"events":["push","issues"]`,
		`"created_at":"2026-01-01T00:00:00Z"`, `"updated_at":"2026-02-01T00:00:00Z"`,
		`"has_multiple_single_files":false`, `"suspended_at":"2026-03-01T00:00:00Z"`,
		`"contact_email":"owner@example.com"`, `"suspended_by":{`, `"login":"hubot"`, `"account":{`,
		`"login":"octo-org"`, `"has_more":false`} {
		if !strings.Contains(string(list), want) {
			t.Errorf("list lacks %s: %s", want, list)
		}
	}
	for _, unwanted := range []string{appTokenCanary, "access_tokens", appHookCanary, appSecretCanary, "webhook", `"token"`} {
		if strings.Contains(string(list), unwanted) {
			t.Errorf("list shows %q: %s", unwanted, list)
		}
	}
	requests := f.recorded()[before:]
	if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].path != appInstallPrefix ||
		requests[0].query != "page=1&per_page=10" {
		t.Errorf("list sent %+v, want one GET %s?page=1&per_page=10", requests, appInstallPrefix)
	}

	before = len(f.recorded())
	app, err := invoke(t, core, appsGet.ID, "global", `{"app_slug":"example-app"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id":55`, `"slug":"example-app"`, `"node_id":"A_55"`, `"client_id":"Iv1.abc"`,
		`"name":"Example App"`, `"description":"Does things.`, `"external_url":"https://example.com/app"`,
		`"html_url":"https://github.com/apps/example-app"`, `"permissions":{"contents":"read","metadata":"read"}`,
		`"events":["push"]`, `"installations_count":12`, `"owner":{`, `"website_url":"https://example.com"`} {
		if !strings.Contains(string(app), want) {
			t.Errorf("app lacks %s: %s", want, app)
		}
	}
	if strings.Contains(string(app), appSecretCanary) || strings.Contains(string(app), "pem") ||
		strings.Contains(string(app), "client_secret") || strings.Contains(string(app), `"repository"`) ||
		strings.Contains(string(app), `"owner":"`) {
		t.Errorf("app shows a secret or a target: %s", app)
	}
	if got := f.recorded()[before:]; len(got) != 1 || got[0].path != "/api/v3/apps/example-app" || got[0].method != http.MethodGet {
		t.Errorf("app read sent %+v", got)
	}

	before = len(f.recorded())
	removed, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "listed",
		`{"installation_id":100}`, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"removed":true`, `"installation_id":100`, `"repository_id":4242`,
		`"app_slug":"example-app"`, `"repository":"octo-org/example"`} {
		if !strings.Contains(string(removed), want) {
			t.Errorf("removal lacks %s: %s", want, removed)
		}
	}
	got := f.recorded()[before:]
	if len(got) != 3 || got[0].method != http.MethodGet || got[0].path != "/api/v3/repos/octo-org/example" ||
		got[1].method != http.MethodGet || got[1].path != appInstallPrefix || got[1].query != "page=1&per_page=100" ||
		got[2].method != http.MethodDelete || got[2].path != "/api/v3/user/installations/100/repositories/4242" {
		t.Errorf("removal sent %+v, want the repository read, the list read, then one DELETE", got)
	}
}

// Every refusal is settled before a credential is read or GitHub is contacted.
func TestAppRefusalsBeforeIO(t *testing.T) {
	f, _, core, reads := appsCore(t)
	for _, tt := range []struct {
		name, id, connection, arguments, message string
	}{
		{"owner outside the targets", appInstallationsList.ID, "org", `{"owner":"orgs/other"}`, "outside the targets"},
		{"excluded owner", appInstallationsList.ID, "excluded-owner", `{"owner":"orgs/octo-org"}`, "not available"},
		{"repository target only", appInstallationsList.ID, "repo-only", `{}`, ""},
		{"user owner", appInstallationsList.ID, "user", `{"owner":"users/octocat"}`, "organization"},
		{"malformed owner", appInstallationsList.ID, "org", `{"owner":"octo-org"}`, ""},
		{"limit above the bound", appInstallationsList.ID, "org", `{"owner":"orgs/octo-org","limit":101}`, ""},
		{"foreign cursor", appInstallationsList.ID, "org", `{"owner":"orgs/octo-org","cursor":"AAAA"}`, ""},
		{"app with a repository target", appsGet.ID, "repo-only", `{"app_slug":"example-app"}`, "repository or a project"},
		{"app with a repository target and owner", appsGet.ID, "unlisted", `{"app_slug":"example-app"}`, "repository or a project"},
		{"app slug with a path", appsGet.ID, "global", `{"app_slug":"a/../b"}`, ""},
		{"app slug with a query", appsGet.ID, "global", `{"app_slug":"a?x=1"}`, ""},
		{"missing app slug", appsGet.ID, "global", `{}`, ""},
		{"app with a repository argument", appsGet.ID, "global", `{"app_slug":"example-app","repository":"octo-org/example"}`, ""},
		{"installation id zero", appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":0}`, "installation_id"},
		{"negative installation id", appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":-5}`, "installation_id"},
		{"text installation id", appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":"100"}`, ""},
		{"missing installation id", appInstallationRepositoriesRemove.ID, "listed", `{}`, "installation_id"},
		{"repository id as argument", appInstallationRepositoriesRemove.ID, "listed",
			`{"installation_id":100,"repository_id":1}`, ""},
		{"repository outside the targets", appInstallationRepositoriesRemove.ID, "foreign",
			`{"repository":"octo-org/example","installation_id":100}`, "outside the targets"},
		{"excluded repository", appInstallationRepositoriesRemove.ID, "excluding",
			`{"repository":"octo-org/example","installation_id":100}`, "not available"},
		{"excluded repository in another spelling", appInstallationRepositoriesRemove.ID, "excluding",
			`{"repository":"OCTO-org/Example","installation_id":100}`, "not available"},
		{"malformed repository", appInstallationRepositoriesRemove.ID, "listed",
			`{"repository":"example","installation_id":100}`, ""},
		{"free route", appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":100,"path":"/x"}`, ""},
	} {
		*reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		var unsupported *capability.UnsupportedError
		if err == nil || errors.As(err, &unsupported) ||
			(tt.message != "" && !strings.Contains(err.Error(), tt.message)) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential (%d) or GitHub", tt.name, *reads)
		}
	}
}

func TestAppRemovalIsOfferedOnlyWhereTheToolsListNamesIt(t *testing.T) {
	f, _, core, _ := appsCore(t)
	var unsupported *capability.UnsupportedError
	before := len(f.recorded())
	if _, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "unlisted", `{"installation_id":100}`, true); !errors.As(err, &unsupported) {
		t.Errorf("removal without a tools list entry = %v, want unsupported-capability", err)
	}
	unconfirmed := &application.ConfirmationRequiredError{}
	if _, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":100}`, false); !errors.As(err, &unconfirmed) {
		t.Errorf("removal without confirmation = %v, want confirmation-required", err)
	}
	if len(f.recorded()) != before {
		t.Error("a refused removal reached GitHub")
	}
}

// An installation id that is not in the list of the organization, a repository of a user account, and an
// installation that gives access to all repositories end without a write request.
func TestAppRemovalWithoutProofSendsNoWrite(t *testing.T) {
	for _, tt := range []struct {
		name    string
		prepare func(*fakeApps)
		id      string
		class   provider.Class
		message string
	}{
		{"installation not in the list", func(*fakeApps) {}, `999`, "", "not an installation of the organization"},
		{"repository of a user account", func(a *fakeApps) { a.ownerType = "User" }, `100`, "", "user account"},
		{"installation of all repositories", func(*fakeApps) {}, `101`, "", "all repositories"},
		{"list refused", func(a *fakeApps) { a.status = http.StatusForbidden }, `100`, provider.ClassPermission, "admin:read"},
		{"list missing", func(a *fakeApps) { a.status = http.StatusNotFound }, `100`, provider.ClassNotFound, ""},
		{"repository refused", func(a *fakeApps) { a.repoStatus = http.StatusForbidden }, `100`, provider.ClassPermission, ""},
		{"repository missing", func(a *fakeApps) { a.repoStatus = http.StatusNotFound }, `100`, provider.ClassNotFound, ""},
		{"repository of another name", func(a *fakeApps) { a.repoName = "renamed" }, `100`, provider.ClassInvalidResponse, ""},
		{"list shorter than its total", func(a *fakeApps) { a.total = 5 }, `999`, provider.ClassProviderError, "completely"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, a, core, _ := appsCore(t)
			tt.prepare(a)
			_, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":`+tt.id+`}`, true)
			if err == nil || (tt.class != "" && classOf(err) != tt.class) ||
				(tt.message != "" && !strings.Contains(err.Error(), tt.message)) || strings.Contains(err.Error(), appTextCanary) {
				t.Errorf("err = %v, want class %q containing %q without provider text", err, tt.class, tt.message)
			}
			if deletes(f) != 0 {
				t.Error("a write request was sent without proof")
			}
		})
	}
}

// The installation id is found on a later page, and an id is foreign only after the whole list was read.
func TestAppRemovalReadsEveryPageBeforeAnIDIsForeign(t *testing.T) {
	f, a, core, _ := appsCore(t)
	a.installations = nil
	for id := int64(1); id <= 150; id++ {
		a.installations = append(a.installations, installationJSON(id, "app-"+strconv.FormatInt(id, 10), "selected"))
	}
	before := len(f.recorded())
	if _, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":999}`, true); err == nil ||
		!strings.Contains(err.Error(), "not an installation") {
		t.Fatalf("unknown id = %v", err)
	}
	got := f.recorded()[before:]
	if len(got) != 3 || got[1].query != "page=1&per_page=100" || got[2].query != "page=2&per_page=100" || deletes(f) != 0 {
		t.Errorf("unknown id sent %+v, want the repository read and both list pages, no write", got)
	}
	before = len(f.recorded())
	removed, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":120}`, true)
	if err != nil || !strings.Contains(string(removed), `"app_slug":"app-120"`) {
		t.Fatalf("removal = %s, %v", removed, err)
	}
	got = f.recorded()[before:]
	if len(got) != 4 || got[3].method != http.MethodDelete || got[3].path != "/api/v3/user/installations/120/repositories/4242" {
		t.Errorf("removal sent %+v, want two list pages then one DELETE", got)
	}
}

// A list that never ends within the bound is not a proof.
func TestAppRemovalAbortsWhenTheListCannotBeReadCompletely(t *testing.T) {
	f, a, core, _ := appsCore(t)
	a.installations = nil
	for id := int64(1); id <= int64(maxInstallationPages*installationPageSize+1); id++ {
		a.installations = append(a.installations, `{"id":`+strconv.FormatInt(id, 10)+`,"app_slug":"a"}`)
	}
	_, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":999999}`, true)
	if err == nil || classOf(err) != provider.ClassProviderError || deletes(f) != 0 {
		t.Errorf("err = %v, deletes = %d, want an abort without a write", err, deletes(f))
	}
}

func TestAppToolsMapRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		class  provider.Class
	}{
		{"forbidden", http.StatusForbidden, provider.ClassPermission},
		{"missing", http.StatusNotFound, provider.ClassNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, a, core, _ := appsCore(t)
			a.status, a.appStatus = tt.status, tt.status
			for _, call := range []struct{ id, connection, arguments string }{
				{appInstallationsList.ID, "org", `{"owner":"orgs/octo-org"}`},
				{appsGet.ID, "global", `{"app_slug":"example-app"}`},
			} {
				_, err := invoke(t, core, call.id, call.connection, call.arguments, false)
				if err == nil || classOf(err) != tt.class || strings.Contains(err.Error(), appTextCanary) {
					t.Errorf("%s = %v, want class %s without provider text", call.id, err, tt.class)
				}
			}
			a.status = 0
			a.deleteAnswer = func(w http.ResponseWriter) {
				w.WriteHeader(tt.status)
				fmt.Fprintf(w, `{"message":%q}`, appTextCanary)
			}
			_, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":100}`, true)
			if err == nil || classOf(err) != tt.class || strings.Contains(err.Error(), appTextCanary) {
				t.Errorf("removal = %v, want class %s without provider text", err, tt.class)
			}
			if tt.class == provider.ClassPermission && !strings.Contains(err.Error(), "repo") {
				t.Errorf("removal message = %v, want the needed rights", err)
			}
		})
	}
}

func TestAppRemovalRejectedByGitHubIsInvalidWithoutProviderText(t *testing.T) {
	_, a, core, _ := appsCore(t)
	a.deleteAnswer = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprintf(w, `{"message":%q}`, appTextCanary)
	}
	_, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":100}`, true)
	if err == nil || strings.Contains(err.Error(), appTextCanary) || !strings.Contains(err.Error(), "rejected") {
		t.Errorf("422 = %v, want Qatlas' own rejection text", err)
	}
}

func TestAppRemovalNeverRepeatsAnUnclearOutcome(t *testing.T) {
	answers := map[string]func(http.ResponseWriter){
		"server error":    func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) },
		"gateway timeout": func(w http.ResponseWriter) { w.WriteHeader(http.StatusGatewayTimeout) },
		"dropped connection": func(w http.ResponseWriter) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		},
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			f, a, core, _ := appsCore(t)
			a.deleteAnswer = answer
			_, err := invoke(t, core, appInstallationRepositoriesRemove.ID, "listed", `{"installation_id":100}`, true)
			if err == nil || !strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("err = %v, want an unclear outcome", err)
			}
			if deletes(f) != 1 {
				t.Errorf("sent %d deletes, want exactly one", deletes(f))
			}
		})
	}
}

func TestAppInstallationListPagesByCursor(t *testing.T) {
	f, a, core, _ := appsCore(t)
	for id := int64(1); id <= 5; id++ {
		a.installations = append(a.installations, installationJSON(id, "extra", "selected"))
	}
	first, err := invoke(t, core, appInstallationsList.ID, "org", `{"owner":"orgs/octo-org","limit":3}`, false)
	if err != nil {
		t.Fatal(err)
	}
	cursor := between(string(first), `"next_cursor":"`, `"`)
	if cursor == "" || !strings.Contains(string(first), `"has_more":true`) {
		t.Fatalf("first page = %s, want a cursor", first)
	}
	before := len(f.recorded())
	if _, err := invoke(t, core, appInstallationsList.ID, "org",
		`{"owner":"orgs/octo-org","limit":3,"cursor":"`+cursor+`"}`, false); err != nil {
		t.Fatal(err)
	}
	if got := f.recorded()[before:]; len(got) != 1 || got[0].query != "page=2&per_page=3" {
		t.Errorf("second page sent %+v, want page=2&per_page=3", got)
	}
	if _, err := invoke(t, core, appInstallationsList.ID, "owner-only",
		`{"owner":"orgs/octo-org","cursor":"`+cursor+`"}`, false); err != nil {
		t.Errorf("a cursor of the same organization = %v, want it accepted", err)
	}
}

// No tool of the app tools adds a repository, suspends or deletes an installation, or creates a token.
func TestAppToolsNeverAddSuspendOrCreateATokenAndOnlyTheRemovalWrites(t *testing.T) {
	reg := registry(t)
	var found []string
	for _, descriptor := range reg.Provider(Provider) {
		segment := strings.Split(descriptor.ID, ".")[1]
		if segment != "apps" && segment != "app-installations" && segment != "app-installation-repositories" {
			continue
		}
		found = append(found, descriptor.ID)
		if descriptor.Group != "organization-admin" {
			t.Errorf("%s is in group %q, want organization-admin", descriptor.ID, descriptor.Group)
		}
		if descriptor.Risk.Effect != capability.EffectRead && descriptor.ID != appInstallationRepositoriesRemove.ID {
			t.Errorf("%s has the effect %s, want read", descriptor.ID, descriptor.Risk.Effect)
		}
		for _, word := range []string{"add", "suspend", "token", "create", "delete", "update", "set"} {
			if strings.Contains(strings.Split(descriptor.ID, ".")[2], word) {
				t.Errorf("%s names the action %q", descriptor.ID, word)
			}
		}
	}
	if len(found) != 3 {
		t.Errorf("app tools = %v, want exactly the list, the read, and the removal", found)
	}
	remove, _, _ := reg.Lookup(appInstallationRepositoriesRemove.ID)
	if !remove.RequiresToolAllowList || remove.Risk.Effect != capability.EffectDelete ||
		remove.Risk.Confirmation != capability.ConfirmationRequired || strings.Contains(string(remove.InputSchema), "repository_id") {
		t.Errorf("removal contract = %+v, want a confirmed, listed-only delete without repository_id", remove.Risk)
	}
}
