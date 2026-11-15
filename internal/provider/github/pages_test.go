package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	pagesRoute  = actionsPrefix + "pages"
	pagesAPI    = "https://api.github.com/api/v3/repos/octo-org/example/pages"
	pagesCanary = "pages-address-canary"
)

// fakePages answers the Pages routes of octo-org/example. override replaces the answer of one route path.
type fakePages struct {
	mu       sync.Mutex
	override map[string]func(http.ResponseWriter)
	builds   int
}

const pagesSiteJSON = `{"url":"` + pagesAPI + `","status":"built","cname":"docs.example.test",` +
	`"protected_domain_state":"verified","pending_domain_unverified_at":null,"custom_404":true,` +
	`"html_url":"https://` + pagesCanary + `.example.test/","build_type":"workflow",` +
	`"source":{"branch":"main","path":"/docs"},"public":false,` +
	`"https_certificate":{"state":"approved","description":"Certificate ok","domains":["docs.example.test"],` +
	`"expires_at":"2027-01-01"},"https_enforced":true}`

func pagesBuildJSON(id string) string {
	return `{"url":"` + pagesAPI + `/builds/` + id + `","status":"built","error":{"message":null},` +
		`"pusher":{"login":"octocat","id":1,"node_id":"U_1","type":"User","site_admin":false,` +
		`"avatar_url":"https://x/a","gravatar_id":"","url":"https://api.github.com/users/octocat","html_url":"https://github.com/octocat"},` +
		`"commit":"abc123","duration":1234,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:01:00Z"}`
}

const pagesHealthJSON = `{"domain":{"host":"docs.example.test","uri":"http://docs.example.test/","nameservers":"ns1",` +
	`"dns_resolves":true,"is_proxied":null,"is_a_record":true,"is_valid_domain":true,"is_apex_domain":false,` +
	`"is_pages_domain":false,"is_valid":true,"reason":null,"responds_to_https":true,"enforces_https":true,` +
	`"https_error":null,"is_https_eligible":true,"caa_error":null},"alt_domain":null}`

func (p *fakePages) route(w http.ResponseWriter, r *http.Request) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, actionsPrefix)
	if !ok || rest != "pages" && !strings.HasPrefix(rest, "pages/") {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if answer := p.override[rest]; answer != nil {
		answer(w)
		return true
	}
	switch {
	case rest == "pages":
		fmt.Fprint(w, pagesSiteJSON)
	case rest == "pages/health":
		fmt.Fprint(w, pagesHealthJSON)
	case rest == "pages/builds":
		if r.URL.Query().Get("page") != "2" {
			w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
		}
		fmt.Fprint(w, "["+pagesBuildJSON("11")+"]")
	case rest == "pages/builds/latest":
		fmt.Fprint(w, pagesBuildJSON("latest"))
	case strings.HasPrefix(rest, "pages/builds/"):
		fmt.Fprint(w, pagesBuildJSON(strings.TrimPrefix(rest, "pages/builds/")))
	default:
		return false
	}
	return true
}

func pagesConfig(base string) *config.Config {
	cfg := coreConfig(base)
	cfg.Connections["foreign"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Target: "repos/octo-org/other", Effects: config.Effects()}
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: config.Effects(),
		Exclude: []string{repoTarget}}
	return cfg
}

func pagesCore(t *testing.T) (*fakeGitHub, *fakePages, *application.Core, *int) {
	t.Helper()
	p := &fakePages{override: map[string]func(http.ResponseWriter){}}
	f := &fakeGitHub{failure: p.route}
	base := serve(t, f)
	reads := 0
	red := &redact.Redactor{}
	return f, p, application.New(registry(t), pagesConfig(base), resolver(red, &reads), red), &reads
}

func TestPagesToolsReadTheSiteBuildsAndHealth(t *testing.T) {
	f, _, core, _ := pagesCore(t)
	for _, tt := range []struct {
		id, arguments string
		want          []string
	}{
		{pagesGet.ID, `{}`, []string{`"status":"built"`, `"cname":"docs.example.test"`, `"protected_domain_state":"verified"`,
			`"custom_404":true`, `"build_type":"workflow"`, `"source":{"branch":"main","path":"/docs"}`, `"public":false`,
			`"https_enforced":true`, `"state":"approved"`, `"domains":["docs.example.test"]`, `"html_url":"https://` + pagesCanary,
			`"repository":"octo-org/example"`}},
		{pagesBuildsList.ID, `{"limit":1}`, []string{`"builds":[`, `"commit":"abc123"`, `"duration":1234`, `"login":"octocat"`,
			`"has_more":true`}},
		{pagesBuildsGet.ID, `{"build_id":11}`, []string{`"status":"built"`, `"updated_at":"2026-01-01T00:01:00Z"`}},
		{pagesBuildsGet.ID, `{"build_id":"latest"}`, []string{`"commit":"abc123"`}},
		{pagesHealthGet.ID, `{}`, []string{`"ready":true`, `"host":"docs.example.test"`, `"dns_resolves":true`,
			`"responds_to_https":true`}},
	} {
		got, err := invoke(t, core, tt.id, "repo", tt.arguments, false)
		for _, want := range tt.want {
			if err != nil || !strings.Contains(string(got), want) {
				t.Errorf("%s = %s, %v; want %s", tt.id, got, err, want)
			}
		}
	}
	for _, r := range f.recorded() {
		if r.method != http.MethodGet || !strings.HasPrefix(r.path, pagesRoute) {
			t.Errorf("request %s %s, want only reads below the Pages routes", r.method, r.path)
		}
	}
}

func TestPagesBuildsPageByLinkHeader(t *testing.T) {
	_, _, core, _ := pagesCore(t)
	first, err := invoke(t, core, pagesBuildsList.ID, "repo", `{"limit":1}`, false)
	var page PagesBuildList
	if err != nil || json.Unmarshal(first, &page) != nil || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("first = %s, %v", first, err)
	}
	second, err := invoke(t, core, pagesBuildsList.ID, "repo", `{"cursor":"`+page.NextCursor+`"}`, false)
	if err != nil || json.Unmarshal(second, &page) != nil || page.HasMore {
		t.Fatalf("second = %s, %v", second, err)
	}
}

func TestPagesHealthAnswersNotReadyWhileGitHubRunsTheCheck(t *testing.T) {
	_, p, core, _ := pagesCore(t)
	p.override["pages/health"] = func(w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted) }
	got, err := invoke(t, core, pagesHealthGet.ID, "repo", `{}`, false)
	if err != nil || !strings.Contains(string(got), `"ready":false`) || strings.Contains(string(got), `"domain"`) {
		t.Errorf("accepted check = %s, %v, want ready false and no domain", got, err)
	}
}

func TestPagesToolsMapRefusalsWithoutProviderText(t *testing.T) {
	calls := []struct{ id, arguments, path string }{
		{pagesGet.ID, `{}`, "pages"}, {pagesBuildsList.ID, `{}`, "pages/builds"},
		{pagesBuildsGet.ID, `{"build_id":3}`, "pages/builds/3"}, {pagesHealthGet.ID, `{}`, "pages/health"},
	}
	for _, tt := range []struct {
		name   string
		status int
		class  provider.Class
		want   string
	}{
		{"forbidden", http.StatusForbidden, provider.ClassPermission, "Pages"},
		{"missing", http.StatusNotFound, provider.ClassNotFound, "does not hold"},
		{"rejected", http.StatusUnprocessableEntity, provider.ClassProviderError, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, p, core, _ := pagesCore(t)
			for _, call := range calls {
				p.override[call.path] = func(w http.ResponseWriter) {
					w.WriteHeader(tt.status)
					fmt.Fprint(w, `{"message":"refused SECRETTEXT"}`)
				}
				_, err := invoke(t, core, call.id, "repo", call.arguments, false)
				if err == nil || classOf(err) != tt.class || strings.Contains(err.Error(), "SECRETTEXT") ||
					!strings.Contains(err.Error(), tt.want) {
					t.Errorf("%s = %v, want class %s mentioning %q without provider text", call.id, err, tt.class, tt.want)
				}
			}
		})
	}
}

func TestPagesHealthNamesItsTwoDocumentedRefusals(t *testing.T) {
	_, p, core, _ := pagesCore(t)
	for status, want := range map[int]string{http.StatusBadRequest: "custom domains", http.StatusUnprocessableEntity: "no custom domain"} {
		p.override["pages/health"] = func(w http.ResponseWriter) {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"message":"x"}`)
		}
		if _, err := invoke(t, core, pagesHealthGet.ID, "repo", `{}`, false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("health %d = %v, want a message naming %q", status, err, want)
		}
	}
}

func TestPagesToolsNeverRepeatATimedOutRead(t *testing.T) {
	f, p, core, _ := pagesCore(t)
	for _, path := range []string{"pages", "pages/builds", "pages/builds/3", "pages/health"} {
		p.override[path] = func(w http.ResponseWriter) { w.WriteHeader(http.StatusGatewayTimeout) }
	}
	for _, call := range []struct{ id, arguments string }{
		{pagesGet.ID, `{}`}, {pagesBuildsList.ID, `{}`}, {pagesBuildsGet.ID, `{"build_id":3}`}, {pagesHealthGet.ID, `{}`},
	} {
		before := len(f.recorded())
		if _, err := invoke(t, core, call.id, "repo", call.arguments, false); classOf(err) != provider.ClassTimeout {
			t.Errorf("%s = %v, want a timeout", call.id, err)
		}
		if sent := len(f.recorded()) - before; sent != 1 {
			t.Errorf("%s sent %d requests, want exactly one", call.id, sent)
		}
	}
}

func TestPagesToolsRefuseAnAnswerOfAnotherRepository(t *testing.T) {
	_, p, core, _ := pagesCore(t)
	other := strings.ReplaceAll(pagesSiteJSON, "/example/pages", "/other/pages")
	p.override["pages"] = func(w http.ResponseWriter) { fmt.Fprint(w, other) }
	p.override["pages/builds/latest"] = func(w http.ResponseWriter) {
		fmt.Fprint(w, strings.ReplaceAll(pagesBuildJSON("latest"), "/example/", "/other/"))
	}
	p.override["pages/builds/5"] = func(w http.ResponseWriter) { fmt.Fprint(w, pagesBuildJSON("6")) }
	for _, call := range []struct{ id, arguments string }{
		{pagesGet.ID, `{}`}, {pagesBuildsGet.ID, `{"build_id":"latest"}`}, {pagesBuildsGet.ID, `{"build_id":5}`},
	} {
		if _, err := invoke(t, core, call.id, "repo", call.arguments, false); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s = %v, want an invalid response", call.id, err)
		}
	}
}

func TestPagesToolsRefuseForeignAndExcludedTargetsAndBadArgumentsBeforeIO(t *testing.T) {
	f, _, core, reads := pagesCore(t)
	calls := map[string]string{pagesGet.ID: `{}`, pagesBuildsList.ID: `{}`, pagesBuildsGet.ID: `"build_id":"latest"`,
		pagesHealthGet.ID: `{}`}
	for id, arguments := range calls {
		withRepository := `{"repository":"octo-org/example"}`
		if arguments != `{}` {
			withRepository = `{"repository":"octo-org/example",` + arguments + `}`
		}
		for _, connection := range []string{"foreign", "ex"} {
			*reads = 0
			before := len(f.recorded())
			if _, err := invoke(t, core, id, connection, withRepository, false); err == nil {
				t.Errorf("%s on %s succeeded, want a refusal", id, connection)
			}
			if *reads != 0 || len(f.recorded()) != before {
				t.Errorf("%s on %s reached the credential or GitHub", id, connection)
			}
		}
	}
	for _, tt := range []struct{ id, arguments string }{
		{pagesBuildsGet.ID, `{}`}, {pagesBuildsGet.ID, `{"build_id":0}`}, {pagesBuildsGet.ID, `{"build_id":"newest"}`},
		{pagesBuildsGet.ID, `{"build_id":"../health"}`}, {pagesBuildsGet.ID, `{"build_id":-1}`},
		{pagesBuildsList.ID, `{"limit":1001}`}, {pagesBuildsList.ID, `{"cursor":"x"}`}, {pagesGet.ID, `{"path":"x"}`},
	} {
		*reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, "repo", tt.arguments, false); !isInvalidRequest(err) {
			t.Errorf("%s(%s) = %v, want an invalid request", tt.id, tt.arguments, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s(%s) reached the credential or GitHub", tt.id, tt.arguments)
		}
	}
}

func TestPagesViewsBoundTextsAndNeverShowUnsetMembers(t *testing.T) {
	_, p, core, _ := pagesCore(t)
	p.override["pages"] = func(w http.ResponseWriter) {
		fmt.Fprint(w, `{"url":"`+pagesAPI+`","status":null,"cname":null,"custom_404":false,"public":true}`)
	}
	got, err := invoke(t, core, pagesGet.ID, "repo", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{`"status"`, `"cname"`, `"https_certificate"`, `"source"`, `"https_enforced"`} {
		if strings.Contains(string(got), unwanted) {
			t.Errorf("site shows %s without GitHub naming it: %s", unwanted, got)
		}
	}
	if !strings.Contains(string(got), `"public":true`) || !strings.Contains(string(got), `"custom_404":false`) {
		t.Errorf("site = %s, want the booleans GitHub sent", got)
	}
}

func TestPagesSubjectNamesOnlyCheckedIDs(t *testing.T) {
	for path, want := range map[string]string{
		"pages": "the GitHub Pages site of this repository", "pages/health": "the GitHub Pages health check of this repository",
		"pages/builds": "the GitHub Pages builds of this repository", "pages/builds/9": "GitHub Pages build 9",
		"pages/builds/latest": "the latest GitHub Pages build of this repository",
		"pages/builds/x":      "the GitHub Pages builds of this repository", "labels": "",
	} {
		if got := pagesSubject(path); got != want {
			t.Errorf("pagesSubject(%q) = %q, want %q", path, got, want)
		}
	}
}
