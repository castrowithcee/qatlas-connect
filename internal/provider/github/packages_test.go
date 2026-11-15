package github

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const packageProviderCanary = "package-provider-text-canary-5e1f"

// fakePackages answers the package routes of octo-org and octocat through the failure hook of fakeGitHub and
// keeps the escaped path of every request, which fakeGitHub's own record decodes.
type fakePackages struct {
	mu      sync.Mutex
	status  int
	escaped []string
}

const packageOwnerJSON = `{"login":"octo-org","id":9,"node_id":"O_kgDO","type":"Organization",` +
	`"url":"https://api.github.com/users/octo-org","html_url":"https://github.com/octo-org"}`

// The linked repository lies inside the targets of the connections below; packageForeignRepositoryJSON does not.
const packageRepositoryJSON = `{"id":1,"node_id":"R_1","name":"example","full_name":"octo-org/example",` +
	`"private":true,"html_url":"https://github.com/octo-org/example","description":"inside",` +
	`"url":"https://api.github.com/repos/octo-org/example"}`

const packageForeignRepositoryJSON = `{"id":2,"node_id":"R_2","name":"elsewhere","full_name":"other/elsewhere",` +
	`"private":true,"html_url":"https://github.com/other/elsewhere","description":"` + packageProviderCanary + `"}`

func packageJSON(name, repository string) string {
	return `{"id":11,"name":` + fmt.Sprintf("%q", name) + `,"package_type":"container","visibility":"private",` +
		`"url":"https://api.github.com/orgs/octo-org/packages/container/app",` +
		`"html_url":"https://github.com/orgs/octo-org/packages/container/package/app","version_count":3,` +
		`"owner":` + packageOwnerJSON + `,"repository":` + repository + `,` +
		`"created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-03T03:04:05Z"}`
}

const packageVersionJSON = `{"id":786068,"name":"sha256:abc","url":"https://api.github.com/orgs/octo-org/packages/container/app/versions/786068",` +
	`"package_html_url":"https://github.com/orgs/octo-org/packages/container/package/app",` +
	`"html_url":"https://github.com/orgs/octo-org/packages/container/app/786068","license":"MIT",` +
	`"description":"a version","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-03T03:04:05Z",` +
	`"deleted_at":"2026-02-03T03:04:05Z","metadata":{"package_type":"container","container":{"tags":["latest","v1"]},` +
	`"docker":{"tag":["old"]}}}`

func (p *fakePackages) route(w http.ResponseWriter, req *http.Request) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	path := strings.TrimPrefix(req.URL.EscapedPath(), "/api/v3")
	var rest string
	switch {
	case strings.HasPrefix(path, "/orgs/octo-org/packages"):
		rest = strings.TrimPrefix(path, "/orgs/octo-org/packages")
	case strings.HasPrefix(path, "/users/octocat/packages"):
		rest = strings.TrimPrefix(path, "/users/octocat/packages")
	default:
		return false
	}
	p.escaped = append(p.escaped, path)
	w.Header().Set("Content-Type", "application/json")
	if p.status != 0 {
		w.WriteHeader(p.status)
		fmt.Fprintf(w, `{"message":%q}`, packageProviderCanary)
		return true
	}
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	switch {
	case rest == "":
		w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
		fmt.Fprint(w, "["+packageJSON("app", packageRepositoryJSON)+","+
			strings.Replace(packageJSON("lib", packageForeignRepositoryJSON), `"id":11`, `"id":12`, 1)+"]")
	case strings.HasSuffix(rest, "/versions"):
		w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
		fmt.Fprint(w, "["+packageVersionJSON+"]")
	case strings.Contains(rest, "/versions/"):
		fmt.Fprint(w, packageVersionJSON)
	default:
		fmt.Fprint(w, packageJSON("app", packageRepositoryJSON))
	}
	return true
}

func packagesConfig(base string) *config.Config {
	cfg := coreConfig(base)
	read := []config.Effect{config.EffectRead}
	cfg.Connections["pkg"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "users/octocat"}, Effects: read}
	cfg.Connections["pkg-org-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "repos/octo-org/*"}, Effects: read}
	cfg.Connections["pkg-repo-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: read}
	cfg.Connections["pkg-excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: read}
	cfg.Connections["pkg-write-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: []config.Effect{config.EffectUpdate}}
	all := []config.Effect{config.EffectRead, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["pkg-listed"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "users/octocat"}, Effects: all, Tools: packageToolIDs}
	cfg.Connections["pkg-unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org", "users/octocat"}, Effects: all}
	cfg.Connections["pkg-listed-repo-only"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/example"}, Effects: all, Tools: packageToolIDs}
	cfg.Connections["pkg-listed-excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: all, Tools: packageToolIDs}
	return cfg
}

func servePackages(t *testing.T, reads *int) (*fakeGitHub, *fakePackages, *application.Core) {
	t.Helper()
	p := &fakePackages{}
	f := &fakeGitHub{}
	f.failure = p.route
	red := &redact.Redactor{}
	return f, p, application.New(registry(t), packagesConfig(serve(t, f)), resolver(red, reads), red)
}

var packageCalls = []struct {
	id, arguments, suffix, query string
	want                         []string
}{
	{packagesList.ID, `"package_type":"container","visibility":"private","limit":10`, "", "package_type=container&page=1&per_page=10&visibility=private",
		[]string{`"name":"app"`, `"version_count":3`, `"has_more":true`, `"next_cursor"`,
			`"links":{"url":"https://api.github.com/orgs/octo-org/packages/container/app"}`,
			`"url":"https://github.com/orgs/octo-org/packages/container/package/app"`, `"owner_detail":{`,
			`"repository":"octo-org/example"`, `"repository_detail":{`}},
	{packagesGet.ID, `"package_type":"container","package_name":"app"`, "/container/app", "",
		[]string{`"name":"app"`, `"package_type":"container"`, `"visibility":"private"`, `"version_count":3`,
			`"repository":"octo-org/example"`}},
	{packageVersionsList.ID, `"package_type":"container","package_name":"app","state":"deleted","limit":5`,
		"/container/app/versions", "page=1&per_page=5&state=deleted",
		[]string{`"name":"sha256:abc"`, `"license":"MIT"`, `"deleted_at":"2026-02-03T03:04:05Z"`,
			`"tags":["latest","v1"]`, `"docker":{"tag":["old"]}`, `"has_more":true`,
			`"package_html_url":"https://github.com/orgs/octo-org/packages/container/package/app"`}},
	{packageVersionsGet.ID, `"package_type":"container","package_name":"app","package_version_id":786068`,
		"/container/app/versions/786068", "",
		[]string{`"id":786068`, `"description":"a version"`, `"url":"https://github.com/orgs/octo-org/packages/container/app/786068"`,
			`"links":{`}},
}

// Both kinds of owner are read through their own route.
func TestPackageToolsSatisfyTheirContractForUsersAndOrganizations(t *testing.T) {
	f, p, core := servePackages(t, nil)
	for _, owner := range []struct{ target, prefix string }{
		{"orgs/octo-org", "/orgs/octo-org/packages"}, {"users/octocat", "/users/octocat/packages"}} {
		for _, call := range packageCalls {
			before := len(f.recorded())
			result, err := invoke(t, core, call.id, "pkg", `{"owner":"`+owner.target+`",`+call.arguments+`}`, false)
			if err != nil {
				t.Fatalf("%s on %s = %v", call.id, owner.target, err)
			}
			for _, want := range call.want {
				if !strings.Contains(string(result), want) {
					t.Errorf("%s on %s = %s, want %s", call.id, owner.target, result, want)
				}
			}
			if !strings.Contains(string(result), `"owner":"`+owner.target+`"`) {
				t.Errorf("%s on %s names no owner: %s", call.id, owner.target, result)
			}
			requests := f.recorded()[before:]
			wantPath := "/api/v3" + owner.prefix + call.suffix
			if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].path != wantPath ||
				requests[0].query != call.query {
				t.Errorf("%s on %s sent %+v, want one GET %s?%s", call.id, owner.target, requests, wantPath, call.query)
			}
		}
	}
	_ = p
}

// The detail objects stay out of a default package read and a repository outside the targets keeps only the
// members GitHub's own text cannot widen, while its full name stays visible as text.
func TestPackageReadsNarrowByDefaultAndKeepForeignRepositoriesAsText(t *testing.T) {
	_, _, core := servePackages(t, nil)
	response, err := core.Invoke(t.Context(), application.InvokeRequest{Operation: packagesGet.ID, Connection: "pkg",
		Arguments: []byte(`{"owner":"orgs/octo-org","package_type":"container","package_name":"app"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if text := string(response.Result); strings.Contains(text, "owner_detail") ||
		strings.Contains(text, "repository_detail") || !strings.Contains(text, `"owner":"orgs/octo-org"`) {
		t.Errorf("default package read = %s", text)
	}
	result, err := invoke(t, core, packagesList.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"container"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(result)
	if !strings.Contains(text, `"repository":"other/elsewhere"`) || strings.Contains(text, packageProviderCanary) ||
		strings.Contains(text, "elsewhere/") {
		t.Errorf("foreign repository = %s", text)
	}
}

// A name travels as one escaped segment whatever characters it holds.
func TestPackageNamesAreOneEscapedSegment(t *testing.T) {
	f, p, core := servePackages(t, nil)
	for _, name := range []string{"a/b", "@scope/pkg", "100%", "a?b=c", "a#b", "a b", `a\b`, "a%2Fb", "é"} {
		before := len(f.recorded())
		for _, id := range []string{packagesGet.ID, packageVersionsList.ID} {
			_, err := invoke(t, core, id, "pkg", fmt.Sprintf(`{"owner":"orgs/octo-org","package_type":"npm","package_name":%q}`, name), false)
			if err != nil {
				t.Fatalf("%s with %q = %v", id, name, err)
			}
		}
		requests := f.recorded()[before:]
		if len(requests) != 2 {
			t.Fatalf("%q sent %d requests", name, len(requests))
		}
		escaped := p.escaped[len(p.escaped)-2]
		segment := strings.TrimPrefix(escaped, "/orgs/octo-org/packages/npm/")
		if strings.Contains(segment, "/") || strings.ContainsAny(segment, "?#") || requests[0].query != "" {
			t.Errorf("%q became %s, want one segment", name, escaped)
		}
		if name == "a/b" && segment != "a%2Fb" || name == "@scope/pkg" && segment != "@scope%2Fpkg" ||
			name == "100%" && segment != "100%25" || name == "a%2Fb" && segment != "a%252Fb" {
			t.Errorf("%q became %s", name, escaped)
		}
	}
}

// The owner and the arguments are settled before a credential is read or GitHub is contacted.
func TestPackageRefusalsBeforeIO(t *testing.T) {
	reads := 0
	f, _, core := servePackages(t, &reads)
	valid := `"package_type":"npm","package_name":"app"`
	for _, tt := range []struct {
		name, id, connection, arguments, message string
	}{
		{"foreign owner", packagesGet.ID, "pkg", `{"owner":"orgs/other",` + valid + `}`, "outside the targets"},
		{"foreign user", packagesList.ID, "pkg", `{"owner":"users/other","package_type":"npm"}`, "outside the targets"},
		{"excluded owner", packagesList.ID, "pkg-excluded", `{"owner":"orgs/octo-org","package_type":"npm"}`, "not available"},
		{"owner through a repository target", packagesList.ID, "pkg-repo-only", `{"owner":"orgs/octo-org","package_type":"npm"}`, ""},
		{"no owner target", packageVersionsList.ID, "pkg-repo-only", `{` + valid + `}`, ""},
		{"owner as a repository", packagesList.ID, "pkg", `{"owner":"octo-org/example","package_type":"npm"}`, "users/LOGIN or orgs/LOGIN"},
		{"unknown package type", packagesList.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"pypi"}`, ""},
		{"package type in another case", packagesGet.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"NPM","package_name":"a"}`, ""},
		{"package type as a path", packagesGet.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm/x","package_name":"a"}`, ""},
		{"missing package type", packagesList.ID, "pkg", `{"owner":"orgs/octo-org"}`, ""},
		{"empty name", packagesGet.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm","package_name":""}`, ""},
		{"dot name", packagesGet.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm","package_name":".."}`, "dot segment"},
		{"single dot name", packageVersionsGet.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm","package_name":".","package_version_id":1}`, "dot segment"},
		{"control character name", packagesGet.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm","package_name":"a\u0000b"}`, ""},
		{"name too long", packagesGet.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm","package_name":"` + strings.Repeat("a", 256) + `"}`, ""},
		{"zero version id", packageVersionsGet.ID, "pkg", `{"owner":"orgs/octo-org",` + valid + `,"package_version_id":0}`, ""},
		{"negative version id", packageVersionsGet.ID, "pkg", `{"owner":"orgs/octo-org",` + valid + `,"package_version_id":-4}`, ""},
		{"text version id", packageVersionsGet.ID, "pkg", `{"owner":"orgs/octo-org",` + valid + `,"package_version_id":"1/../2"}`, ""},
		{"fractional version id", packageVersionsGet.ID, "pkg", `{"owner":"orgs/octo-org",` + valid + `,"package_version_id":1.5}`, ""},
		{"unknown visibility", packagesList.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm","visibility":"secret"}`, ""},
		{"unknown state", packageVersionsList.ID, "pkg", `{"owner":"orgs/octo-org",` + valid + `,"state":"gone"}`, ""},
		{"token argument", packagesList.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm","token":"x"}`, ""},
		{"foreign cursor", packagesList.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm","cursor":"bogus"}`, ""},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, false)
		var unsupported *capability.UnsupportedError
		if err == nil || errors.As(err, &unsupported) || (tt.message != "" && !strings.Contains(err.Error(), tt.message)) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential (%d) or GitHub", tt.name, reads)
		}
	}
}

// A cursor leads to the next page of the same list only.
func TestPackageListsPageByCursor(t *testing.T) {
	f, _, core := servePackages(t, nil)
	first, err := invoke(t, core, packagesList.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm","limit":5}`, false)
	if err != nil {
		t.Fatal(err)
	}
	cursor := between(string(first), `"next_cursor":"`, `"`)
	if cursor == "" {
		t.Fatalf("first page = %s", first)
	}
	before := len(f.recorded())
	if _, err := invoke(t, core, packagesList.ID, "pkg",
		`{"owner":"orgs/octo-org","package_type":"npm","cursor":"`+cursor+`"}`, false); err != nil {
		t.Fatal(err)
	}
	if got := f.recorded()[before:]; len(got) != 1 || got[0].query != "package_type=npm&page=2&per_page=5" {
		t.Errorf("second page sent %+v", got)
	}
	before = len(f.recorded())
	for _, arguments := range []string{
		`{"owner":"orgs/octo-org","package_type":"maven","cursor":"` + cursor + `"}`,
		`{"owner":"users/octocat","package_type":"npm","cursor":"` + cursor + `"}`,
		`{"owner":"orgs/octo-org","package_type":"npm","visibility":"public","cursor":"` + cursor + `"}`,
	} {
		if _, err := invoke(t, core, packagesList.ID, "pkg", arguments, false); !isInvalidRequest(err) {
			t.Errorf("foreign cursor with %s = %v", arguments, err)
		}
	}
	if len(f.recorded()) != before {
		t.Errorf("a foreign cursor reached GitHub")
	}
}

// Every tool names GitHub's failures without quoting its text and without a retry.
func TestPackageFailuresAreNamed(t *testing.T) {
	f, p, core := servePackages(t, nil)
	for _, owner := range []string{"orgs/octo-org", "users/octocat"} {
		for _, call := range packageCalls {
			for _, tt := range []struct {
				status int
				class  provider.Class
			}{
				{http.StatusForbidden, provider.ClassPermission},
				{http.StatusNotFound, provider.ClassNotFound},
				{http.StatusUnprocessableEntity, provider.ClassProviderError},
				{http.StatusGatewayTimeout, provider.ClassTimeout},
			} {
				p.status = tt.status
				before := len(f.recorded())
				_, err := invoke(t, core, call.id, "pkg", `{"owner":"`+owner+`",`+call.arguments+`}`, false)
				if classOf(err) != tt.class || strings.Contains(err.Error(), packageProviderCanary) ||
					strings.Contains(err.Error(), uncertain) || strings.Contains(err.Error(), `"app"`) {
					t.Errorf("%s answered %d = %v, want %s without GitHub's text", call.id, tt.status, err, tt.class)
				}
				if got := len(f.recorded()[before:]); got != 1 {
					t.Errorf("%s answered %d sent %d requests, want 1", call.id, tt.status, got)
				}
				if tt.status == http.StatusForbidden && !strings.Contains(err.Error(), "read:packages") {
					t.Errorf("%s forbidden = %v, want the token right named", call.id, err)
				}
			}
			p.status = 0
		}
	}
}

// A connection without the read effect does not offer the tools, and a package write tool is not offered.
func TestPackageToolsNeedTheReadEffectAndOfferNoWrite(t *testing.T) {
	reads := 0
	f, _, core := servePackages(t, &reads)
	for _, call := range packageCalls {
		_, err := invoke(t, core, call.id, "pkg-write-only", `{"owner":"orgs/octo-org",`+call.arguments+`}`, false)
		var unsupported *capability.UnsupportedError
		if !errors.As(err, &unsupported) {
			t.Errorf("%s without the read effect = %v, want unsupported", call.id, err)
		}
	}
	for _, id := range []string{"github.packages.delete", "github.packages.restore",
		"github.package-versions.delete", "github.package-versions.restore"} {
		if _, err := invoke(t, core, id, "pkg", `{"owner":"orgs/octo-org"}`, true); err == nil {
			t.Errorf("%s is offered", id)
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused calls read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
	reg := registry(t)
	for _, call := range packageCalls {
		descriptor, _, _ := reg.Lookup(call.id)
		if strings.Contains(string(descriptor.InputSchema), `"token"`) {
			t.Errorf("%s offers a token argument", call.id)
		}
		if descriptor.Group != "packages" {
			t.Errorf("%s group = %q", call.id, descriptor.Group)
		}
	}
}

// Undocumented members and hostile text never widen the answer, and an address is never fetched.
func TestPackageReadsTreatProviderTextAsData(t *testing.T) {
	f := &fakeGitHub{}
	hostile := `[{"id":5,"name":"app\u0007x","package_type":"npm","secret_member":"` + packageProviderCanary + `",` +
		`"html_url":"https://evil.invalid/x","url":"https://evil.invalid/api","repository":{"id":3,"full_name":"other/r",` +
		`"html_url":"https://evil.invalid/r","description":"` + packageProviderCanary + `"}}]`
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.URL.Path, "/packages") {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, hostile)
		return true
	}
	red := &redact.Redactor{}
	core := application.New(registry(t), packagesConfig(serve(t, f)), resolver(red, nil), red)
	result, err := invoke(t, core, packagesList.ID, "pkg", `{"owner":"orgs/octo-org","package_type":"npm"}`, false)
	if err != nil || strings.Contains(string(result), packageProviderCanary) || strings.Contains(string(result), "\u0007") {
		t.Fatalf("result = %s, %v", result, err)
	}
	for _, request := range f.recorded() {
		if strings.Contains(request.path, "evil") {
			t.Errorf("an address was fetched: %s", request.path)
		}
	}
}

var packageChangeCalls = []struct {
	id, arguments, suffix, method string
	want                          []string
}{
	{packagesDelete.ID, `"package_type":"npm","package_name":"app"`, "/npm/app", http.MethodDelete,
		[]string{`"deleted":true`, `"package_type":"npm"`, `"package_name":"app"`}},
	{packagesRestore.ID, `"package_type":"npm","package_name":"app"`, "/npm/app/restore", http.MethodPost,
		[]string{`"restored":true`, `"package_type":"npm"`, `"package_name":"app"`}},
	{packageVersionsDelete.ID, `"package_type":"container","package_name":"app","package_version_id":786068`,
		"/container/app/versions/786068", http.MethodDelete,
		[]string{`"deleted":true`, `"package_version_id":786068`}},
	{packageVersionsRestore.ID, `"package_type":"container","package_name":"app","package_version_id":786068`,
		"/container/app/versions/786068/restore", http.MethodPost,
		[]string{`"restored":true`, `"package_version_id":786068`}},
}

var packageOwners = []struct{ target, prefix string }{
	{"orgs/octo-org", "/orgs/octo-org/packages"}, {"users/octocat", "/users/octocat/packages"}}

// Every change goes to its own route for both kinds of owner as exactly one request.
func TestPackageChangesSendOneRequestForUsersAndOrganizations(t *testing.T) {
	f, _, core := servePackages(t, nil)
	for _, owner := range packageOwners {
		for _, call := range packageChangeCalls {
			before := len(f.recorded())
			result, err := invoke(t, core, call.id, "pkg-listed", `{"owner":"`+owner.target+`",`+call.arguments+`}`, true)
			if err != nil {
				t.Fatalf("%s on %s = %v", call.id, owner.target, err)
			}
			for _, want := range append(call.want, `"owner":"`+owner.target+`"`) {
				if !strings.Contains(string(result), want) {
					t.Errorf("%s on %s = %s, want %s", call.id, owner.target, result, want)
				}
			}
			requests := f.recorded()[before:]
			wantPath := "/api/v3" + owner.prefix + call.suffix
			if len(requests) != 1 || requests[0].method != call.method || requests[0].path != wantPath ||
				requests[0].query != "" {
				t.Errorf("%s on %s sent %+v, want one %s %s", call.id, owner.target, requests, call.method, wantPath)
			}
		}
	}
}

// GitHub's refusals and an unclear outcome are named without its text; a change is never repeated.
func TestPackageChangeFailuresAreNamed(t *testing.T) {
	f, p, core := servePackages(t, nil)
	for _, owner := range packageOwners {
		for _, call := range packageChangeCalls {
			restore := strings.HasSuffix(call.id, ".restore")
			for _, tt := range []struct {
				status    int
				class     provider.Class
				uncertain bool
				contains  string
			}{
				{http.StatusForbidden, provider.ClassPermission, false, "read:packages"},
				{http.StatusNotFound, provider.ClassNotFound, false, ""},
				{http.StatusUnprocessableEntity, provider.ClassProviderError, false, "nothing was"},
				{http.StatusBadRequest, provider.ClassProviderError, false, "nothing was"},
				{http.StatusInternalServerError, provider.ClassProviderError, true, ""},
				{http.StatusGatewayTimeout, provider.ClassTimeout, true, ""},
			} {
				p.status = tt.status
				before := len(f.recorded())
				_, err := invoke(t, core, call.id, "pkg-listed", `{"owner":"`+owner.target+`",`+call.arguments+`}`, true)
				if err == nil || classOf(err) != tt.class || strings.Contains(err.Error(), packageProviderCanary) ||
					strings.Contains(err.Error(), uncertain) != tt.uncertain ||
					!strings.Contains(err.Error(), tt.contains) {
					t.Errorf("%s on %s answered %d = %v, want %s, uncertain = %t", call.id, owner.target,
						tt.status, err, tt.class, tt.uncertain)
				}
				if got := len(f.recorded()[before:]); got != 1 {
					t.Errorf("%s answered %d sent %d requests, want 1", call.id, tt.status, got)
				}
				if tt.status == http.StatusForbidden && restore != strings.Contains(err.Error(), "token query argument") {
					t.Errorf("%s forbidden = %v, want the token argument named for a restore only", call.id, err)
				}
			}
			p.status = 0
		}
	}
}

// A change is refused before a credential is read or GitHub is contacted when its owner, its arguments, or
// its confirmation is wrong, and a name travels as one escaped segment.
func TestPackageChangeRefusalsBeforeIO(t *testing.T) {
	reads := 0
	f, _, core := servePackages(t, &reads)
	for _, call := range packageChangeCalls {
		for _, tt := range []struct {
			name, connection, arguments string
			confirmed                   bool
		}{
			{"foreign owner", "pkg-listed", `{"owner":"orgs/other",` + call.arguments + `}`, true},
			{"foreign user", "pkg-listed", `{"owner":"users/other",` + call.arguments + `}`, true},
			{"owner through a repository target", "pkg-listed-repo-only", `{"owner":"orgs/octo-org",` + call.arguments + `}`, true},
			{"excluded owner", "pkg-listed-excluded", `{"owner":"orgs/octo-org",` + call.arguments + `}`, true},
			{"unlisted", "pkg-unlisted", `{"owner":"orgs/octo-org",` + call.arguments + `}`, true},
			{"unconfirmed", "pkg-listed", `{"owner":"orgs/octo-org",` + call.arguments + `}`, false},
			{"token argument", "pkg-listed", `{"owner":"orgs/octo-org","token":"x",` + call.arguments + `}`, true},
			{"unknown package type", "pkg-listed", `{"owner":"orgs/octo-org","package_type":"pypi",` +
				packageNameAndVersion(call.id, "app"), true},
			{"dot name", "pkg-listed", `{"owner":"orgs/octo-org","package_type":"npm",` +
				packageNameAndVersion(call.id, ".."), true},
		} {
			reads = 0
			before := len(f.recorded())
			_, err := invoke(t, core, call.id, tt.connection, tt.arguments, tt.confirmed)
			if err == nil {
				t.Errorf("%s: %s was not refused", call.id, tt.name)
			}
			if reads != 0 || len(f.recorded()) != before {
				t.Errorf("%s: %s reached the credential (%d) or GitHub", call.id, tt.name, reads)
			}
		}
	}
	unconfirmed := &application.ConfirmationRequiredError{}
	for _, call := range packageChangeCalls {
		_, err := invoke(t, core, call.id, "pkg-listed", `{"owner":"orgs/octo-org",`+call.arguments+`}`, false)
		if !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v, want confirmation-required", call.id, err)
		}
	}
}

// A change names a package as one escaped segment whatever characters it holds, and its descriptor offers
// neither a token argument nor a use without the tools list.
func TestPackageChangeNamesAreOneSegmentAndDescriptorsOfferNoToken(t *testing.T) {
	f, p, core := servePackages(t, nil)
	for _, name := range []string{"a/b", "@scope/pkg", "100%", "a?b=c", "a#b", "a b", "a%2Fb"} {
		for _, call := range packageChangeCalls {
			before := len(f.recorded())
			arguments := `{"owner":"orgs/octo-org","package_type":"npm",` + packageNameAndVersion(call.id, name)
			if _, err := invoke(t, core, call.id, "pkg-listed", arguments, true); err != nil {
				t.Fatalf("%s with %q = %v", call.id, name, err)
			}
			requests := f.recorded()[before:]
			escaped := p.escaped[len(p.escaped)-1]
			segment := strings.Split(strings.TrimPrefix(escaped, "/orgs/octo-org/packages/npm/"), "/")[0]
			if len(requests) != 1 || requests[0].query != "" || segment != url.PathEscape(name) {
				t.Errorf("%s with %q sent %+v as %s, want one segment %s", call.id, name, requests, escaped, url.PathEscape(name))
			}
		}
	}
	reg := registry(t)
	for _, call := range packageChangeCalls {
		descriptor, _, _ := reg.Lookup(call.id)
		if strings.Contains(string(descriptor.InputSchema), `"token"`) {
			t.Errorf("%s offers a token argument", call.id)
		}
		for _, argument := range descriptor.Arguments {
			if argument.Name == "token" {
				t.Errorf("%s describes a token argument", call.id)
			}
		}
		if descriptor.Group != "packages" || !descriptor.RequiresToolAllowList {
			t.Errorf("%s group = %q, allow-list = %t", call.id, descriptor.Group, descriptor.RequiresToolAllowList)
		}
	}
}

// packageNameAndVersion is the rest of the arguments of a package change tool: its name and, for a version
// tool, an id, closing the object.
func packageNameAndVersion(id, name string) string {
	rest := fmt.Sprintf(`"package_name":%q`, name)
	if strings.Contains(id, ".package-versions.") {
		rest += `,"package_version_id":5`
	}
	return rest + `}`
}
