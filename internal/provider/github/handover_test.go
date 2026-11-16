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

// handoverRoute answers one method and path of the fake with a body; the routes below it are the writes of
// the visibility, transfer, Pages, and draft advisory tools.
type handoverRoute struct {
	method, path string
	answer       func(http.ResponseWriter, *http.Request)
}

const (
	handoverRepo     = "/api/v3/repos/octo-org/example"
	handoverAdvisory = handoverRepo + "/security-advisories/" + testGHSA
)

// handoverCore serves the routes and returns the fake, the credential read counter, and a core over
// connections that cover the four tools in different ways.
func handoverCore(t *testing.T, routes ...handoverRoute) (*fakeGitHub, *int, *application.Core) {
	t.Helper()
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		for _, route := range routes {
			if r.Method == route.method && r.URL.Path == route.path {
				w.Header().Set("Content-Type", "application/json")
				route.answer(w, r)
				return true
			}
		}
		return false
	}
	base := serve(t, f)
	cfg := coreConfig(base)
	all := config.Effects()
	tools := []string{repositoriesSetVisibility.ID, repositoriesTransfer.ID, pagesDelete.ID,
		repositoryAdvisoriesUpdate.ID}
	add := func(name string, targets, excludes []string, listed ...string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Targets: targets,
			Exclude: excludes, Effects: all, Tools: listed}
	}
	add("listed", []string{"repos/octo-org/example"}, nil, tools...)
	add("unlisted", []string{"repos/octo-org/example"}, nil)
	add("excluded", []string{"repos/octo-org/*"}, []string{"repos/octo-org/example"}, tools...)
	add("owner", []string{"repos/octo-org/example", "orgs/octo-archive", "users/octo-cat"}, nil, tools...)
	add("destination-excluded", []string{"repos/octo-org/example", "orgs/octo-archive"},
		[]string{"repos/octo-archive/example"}, tools...)
	add("implied", []string{"repos/octo-org/example", "repos/octo-archive/example"}, nil, tools...)
	add("same-owner", []string{"repos/octo-org/example", "orgs/octo-org"}, nil, tools...)
	reads := 0
	red := &redact.Redactor{}
	return f, &reads, application.New(registry(t), cfg, resolver(red, &reads), red)
}

func handoverWrites(f *fakeGitHub) int {
	n := 0
	for _, r := range f.recorded() {
		if r.method != http.MethodGet {
			n++
		}
	}
	return n
}

func status(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		fmt.Fprint(w, `{"message":"SECRETTEXT"}`)
	}
}

func visibilityRoute(answer func(http.ResponseWriter, *http.Request)) handoverRoute {
	return handoverRoute{http.MethodPatch, handoverRepo, answer}
}

func privateAnswer(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, `{"full_name":"octo-org/example","visibility":"private","private":true,"description":"SECRETTEXT"}`)
}

// refusalCases are the answers every write tool maps without provider text.
var refusalCases = []struct {
	name   string
	status int
	class  provider.Class
	want   string
}{
	{"forbidden", http.StatusForbidden, provider.ClassPermission, "needs"},
	{"not found", http.StatusNotFound, provider.ClassNotFound, ""},
	{"unprocessable", http.StatusUnprocessableEntity, "", ""},
}

func checkRefusal(t *testing.T, f *fakeGitHub, err error, status int, class provider.Class, want string) {
	t.Helper()
	if err == nil || strings.Contains(err.Error(), "SECRETTEXT") || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want %q without provider text", err, want)
	}
	if status == http.StatusUnprocessableEntity && !isInvalidRequest(err) {
		t.Errorf("422 = %v, want an invalid request", err)
	}
	if class != "" && classOf(err) != class {
		t.Errorf("class = %s, want %s", classOf(err), class)
	}
	if handoverWrites(f) != 1 {
		t.Errorf("writes = %d, want exactly 1 and no retry", handoverWrites(f))
	}
}

func TestSetVisibilitySendsOnlyTheVisibility(t *testing.T) {
	f, _, core := handoverCore(t, visibilityRoute(privateAnswer))
	for _, value := range []string{"private", "internal"} {
		result, err := invoke(t, core, repositoriesSetVisibility.ID, "listed",
			`{"repository":"octo-org/example","visibility":"`+value+`"}`, true)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if json.Unmarshal(result, &got) != nil || got["repository"] != "octo-org/example" ||
			got["visibility"] != "private" || got["private"] != true || len(got) != 3 {
			t.Errorf("result = %s, want repository, visibility, and private as GitHub reports", result)
		}
	}
	requests := f.recorded()
	if len(requests) != 2 || requests[0].method != http.MethodPatch || len(requests[0].body) != 1 ||
		requests[0].body["visibility"] != "private" || requests[1].body["visibility"] != "internal" {
		t.Errorf("requests = %+v, want two PATCH with only the visibility", requests)
	}
}

func TestSetVisibilityRefusesPublicAndOtherValuesBeforeAnyRequest(t *testing.T) {
	f, reads, core := handoverCore(t, visibilityRoute(privateAnswer))
	for _, arguments := range []string{
		`{"repository":"octo-org/example","visibility":"public"}`,
		`{"repository":"octo-org/example","visibility":"PUBLIC"}`,
		`{"repository":"octo-org/example","visibility":"Private"}`,
		`{"repository":"octo-org/example","visibility":""}`,
		`{"repository":"octo-org/example"}`,
		`{"repository":"octo-org/example","visibility":"private","archived":true}`,
	} {
		_, err := invoke(t, core, repositoriesSetVisibility.ID, "listed", arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", arguments, err)
		}
	}
	if *reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("a refused value reached the credential (%d) or GitHub (%d)", *reads, len(f.recorded()))
	}
	schema := string(repositoriesSetVisibility.InputSchema)
	if strings.Contains(schema, `"public"`) {
		t.Errorf("the input schema offers public: %s", schema)
	}
}

func TestSetVisibilityMapsRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range refusalCases {
		t.Run(tt.name, func(t *testing.T) {
			f, _, core := handoverCore(t, visibilityRoute(status(tt.status)))
			_, err := invoke(t, core, repositoriesSetVisibility.ID, "listed",
				`{"repository":"octo-org/example","visibility":"private"}`, true)
			checkRefusal(t, f, err, tt.status, tt.class, tt.want)
		})
	}
}

func TestSetVisibilityNeverRepeatsAnUnclearChange(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"server error": status(http.StatusBadGateway),
		"unreadable":   func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "x") },
		"other repository": func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"full_name":"octo-org/other","visibility":"private"}`)
		},
		"no visibility": func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"full_name":"octo-org/example"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, core := handoverCore(t, visibilityRoute(answer))
			_, err := invoke(t, core, repositoriesSetVisibility.ID, "listed",
				`{"repository":"octo-org/example","visibility":"private"}`, true)
			if err == nil || !strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("err = %v, want an unclear outcome", err)
			}
			if handoverWrites(f) != 1 {
				t.Errorf("writes = %d, want exactly 1", handoverWrites(f))
			}
		})
	}
}

func TestWriteToolsReportATimeoutOnceAndUncertain(t *testing.T) {
	stall := newChangeStall(t)
	slow := stall.answer
	for _, tt := range []struct {
		tool, method, path, arguments string
	}{
		{repositoriesSetVisibility.ID, http.MethodPatch, handoverRepo,
			`{"repository":"octo-org/example","visibility":"private"}`},
		{repositoriesTransfer.ID, http.MethodPost, handoverRepo + "/transfer",
			`{"repository":"octo-org/example","confirm_name":"octo-org/example","new_owner":"orgs/octo-archive"}`},
		{pagesDelete.ID, http.MethodDelete, handoverRepo + "/pages", `{"repository":"octo-org/example"}`},
	} {
		t.Run(tt.tool, func(t *testing.T) {
			f, _, core := handoverCore(t, handoverRoute{tt.method, tt.path, slow})
			ctx, cancel := stall.context()
			defer cancel()
			_, err := core.Invoke(ctx, application.InvokeRequest{Operation: tt.tool, Connection: "owner",
				Arguments: json.RawMessage(tt.arguments), Confirmed: true})
			if classOf(err) != provider.ClassTimeout || !strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("err = %v, want a timeout with an unclear outcome", err)
			}
			if handoverWrites(f) != 1 {
				t.Errorf("writes = %d, want exactly 1", handoverWrites(f))
			}
		})
	}
}

func transferRoute(answer func(http.ResponseWriter, *http.Request)) handoverRoute {
	return handoverRoute{http.MethodPost, handoverRepo + "/transfer", answer}
}

func transferred(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, `{"full_name":"octo-org/example","html_url":"https://github.com/octo-org/example",`+
		`"description":"SECRETTEXT"}`)
}

func TestTransferSendsOneRequestToAnOwnerTheConnectionNames(t *testing.T) {
	f, _, core := handoverCore(t, transferRoute(transferred))
	result, err := invoke(t, core, repositoriesTransfer.ID, "owner",
		`{"repository":"octo-org/example","confirm_name":"octo-org/example","new_owner":"orgs/octo-archive",`+
			`"new_name":"example-kept","team_ids":[7,9]}`, true)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if json.Unmarshal(result, &got) != nil || got["repository"] != "octo-org/example" ||
		got["new_owner"] != "orgs/octo-archive" || got["new_name"] != "example-kept" || got["accepted"] != true ||
		len(got) != 4 || strings.Contains(string(result), "SECRETTEXT") {
		t.Errorf("result = %s, want only what was asked and accepted", result)
	}
	requests := f.recorded()
	if len(requests) != 1 || requests[0].method != http.MethodPost || len(requests[0].body) != 3 ||
		requests[0].body["new_owner"] != "octo-archive" || requests[0].body["new_name"] != "example-kept" {
		t.Errorf("requests = %+v, want one POST with new_owner, new_name, and team_ids", requests)
	}
	// A user account as new owner takes no teams and sends only the owner.
	f2, _, core2 := handoverCore(t, transferRoute(transferred))
	if _, err := invoke(t, core2, repositoriesTransfer.ID, "owner",
		`{"repository":"octo-org/example","confirm_name":"octo-org/example","new_owner":"users/octo-cat"}`,
		true); err != nil {
		t.Fatal(err)
	}
	if r := f2.recorded(); len(r) != 1 || len(r[0].body) != 1 || r[0].body["new_owner"] != "octo-cat" {
		t.Errorf("user transfer = %+v, want only new_owner", r)
	}
}

func TestTransferChecksEverythingBeforeAnySecretOrRequest(t *testing.T) {
	f, reads, core := handoverCore(t, transferRoute(transferred))
	good := func(rest string) string {
		return `{"repository":"octo-org/example","confirm_name":"octo-org/example",` + rest + `}`
	}
	for name, tt := range map[string]struct{ connection, arguments string }{
		"owner outside the targets":       {"owner", good(`"new_owner":"orgs/other-org"`)},
		"owner of another kind":           {"owner", good(`"new_owner":"users/octo-archive"`)},
		"owner only implied by repo":      {"implied", good(`"new_owner":"orgs/octo-archive"`)},
		"owner not an owner form":         {"owner", good(`"new_owner":"octo-archive"`)},
		"owner a pattern":                 {"owner", good(`"new_owner":"orgs/*"`)},
		"owner padded":                    {"owner", good(`"new_owner":" orgs/octo-archive"`)},
		"destination repository excluded": {"destination-excluded", good(`"new_owner":"orgs/octo-archive"`)},
		"same owner":                      {"same-owner", good(`"new_owner":"orgs/octo-org"`)},
		"no owner":                        {"owner", good(`"new_name":"x"`)},
		"empty owner":                     {"owner", good(`"new_owner":""`)},
		"wrong confirm name":              {"owner", `{"repository":"octo-org/example","confirm_name":"octo-org/Example","new_owner":"orgs/octo-archive"}`},
		"confirm name without owner":      {"owner", `{"repository":"octo-org/example","confirm_name":"example","new_owner":"orgs/octo-archive"}`},
		"no confirm name":                 {"owner", `{"repository":"octo-org/example","new_owner":"orgs/octo-archive"}`},
		"teams for a user":                {"owner", good(`"new_owner":"users/octo-cat","team_ids":[1]`)},
		"team id zero":                    {"owner", good(`"new_owner":"orgs/octo-archive","team_ids":[0]`)},
		"too many teams":                  {"owner", good(`"new_owner":"orgs/octo-archive","team_ids":[` + strings.TrimSuffix(strings.Repeat("1,", 51), ",") + `]`)},
		"bad new name":                    {"owner", good(`"new_owner":"orgs/octo-archive","new_name":"a/b"`)},
		"git suffix":                      {"owner", good(`"new_owner":"orgs/octo-archive","new_name":"x.git"`)},
		"unknown argument":                {"owner", good(`"new_owner":"orgs/octo-archive","visibility":"public"`)},
		"repository outside the targets":  {"owner", `{"repository":"other-org/example","confirm_name":"other-org/example","new_owner":"orgs/octo-archive"}`},
	} {
		*reads = 0
		_, err := invoke(t, core, repositoriesTransfer.ID, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", name, err)
			continue
		}
		for _, quoted := range []string{"other-org", "octo-archive", "octo-cat"} {
			if strings.Contains(err.Error(), quoted) {
				t.Errorf("%s quotes a foreign name: %v", name, err)
			}
		}
		if *reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("%s reached the credential or GitHub", name)
		}
	}
}

func TestTransferMapsRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range refusalCases {
		t.Run(tt.name, func(t *testing.T) {
			f, _, core := handoverCore(t, transferRoute(status(tt.status)))
			_, err := invoke(t, core, repositoriesTransfer.ID, "owner",
				`{"repository":"octo-org/example","confirm_name":"octo-org/example","new_owner":"orgs/octo-archive"}`, true)
			checkRefusal(t, f, err, tt.status, tt.class, tt.want)
		})
	}
}

func TestTransferNeverRepeatsAnUnclearRequest(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"server error": status(http.StatusBadGateway),
		"unreadable":   func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "x") },
		"no name":      func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{}`) },
	} {
		t.Run(name, func(t *testing.T) {
			f, _, core := handoverCore(t, transferRoute(answer))
			_, err := invoke(t, core, repositoriesTransfer.ID, "owner",
				`{"repository":"octo-org/example","confirm_name":"octo-org/example","new_owner":"orgs/octo-archive"}`, true)
			if err == nil || !strings.Contains(err.Error(), "may have been applied") || handoverWrites(f) != 1 {
				t.Errorf("err = %v, writes %d, want an unclear outcome after one request", err, handoverWrites(f))
			}
		})
	}
}

func pagesDeleteRoute(answer func(http.ResponseWriter, *http.Request)) handoverRoute {
	return handoverRoute{http.MethodDelete, handoverRepo + "/pages", answer}
}

func TestPagesDeleteTurnsTheSiteOffWithOneRequest(t *testing.T) {
	f, _, core := handoverCore(t, pagesDeleteRoute(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	result, err := invoke(t, core, pagesDelete.ID, "listed", `{"repository":"octo-org/example"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if json.Unmarshal(result, &got) != nil || got["deleted"] != true || got["repository"] != "octo-org/example" {
		t.Errorf("result = %s", result)
	}
	if requests := f.recorded(); len(requests) != 1 || requests[0].method != http.MethodDelete {
		t.Errorf("requests = %+v, want one DELETE", requests)
	}
}

func TestPagesDeleteMapsRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range append(refusalCases, struct {
		name   string
		status int
		class  provider.Class
		want   string
	}{"conflict", http.StatusConflict, provider.ClassProviderError, "current state"}) {
		t.Run(tt.name, func(t *testing.T) {
			f, _, core := handoverCore(t, pagesDeleteRoute(status(tt.status)))
			_, err := invoke(t, core, pagesDelete.ID, "listed", `{"repository":"octo-org/example"}`, true)
			checkRefusal(t, f, err, tt.status, tt.class, tt.want)
		})
	}
}

func TestPagesDeleteNeverRepeatsAServerError(t *testing.T) {
	f, _, core := handoverCore(t, pagesDeleteRoute(status(http.StatusBadGateway)))
	_, err := invoke(t, core, pagesDelete.ID, "listed", `{"repository":"octo-org/example"}`, true)
	if err == nil || !strings.Contains(err.Error(), "may have been applied") || handoverWrites(f) != 1 {
		t.Errorf("err = %v, writes %d", err, handoverWrites(f))
	}
}

func advisoryDraft(state string) string {
	return `{"ghsa_id":"` + testGHSA + `","cve_id":null,"html_url":"https://x/a","state":"` + state +
		`","severity":"high","summary":"SECRETTEXT","description":"details","cwe_ids":["CWE-79"],` +
		`"vulnerabilities":[{"package":{"ecosystem":"npm","name":"lodash"},"vulnerable_version_range":"<1",` +
		`"patched_versions":"1"}],"credits":[],"collaborating_users":[],"collaborating_teams":[]}`
}

func advisoryRoutes(state string, patch func(http.ResponseWriter, *http.Request)) []handoverRoute {
	read := func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, advisoryDraft(state)) }
	return []handoverRoute{{http.MethodGet, handoverAdvisory, read}, {http.MethodPatch, handoverAdvisory, patch}}
}

func advisoryPatched(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, advisoryDraft("draft")) }

func handoverPatches(f *fakeGitHub) []recorded {
	var patches []recorded
	for _, r := range f.recorded() {
		if r.method == http.MethodPatch {
			patches = append(patches, r)
		}
	}
	return patches
}

func TestAdvisoryUpdateReadsFirstAndSendsOnlyTheGivenMembers(t *testing.T) {
	f, _, core := handoverCore(t, advisoryRoutes("draft", advisoryPatched)...)
	result, err := invoke(t, core, repositoryAdvisoriesUpdate.ID, "listed",
		`{"ghsa_id":"`+testGHSA+`","summary":"New summary","severity":"low","cwe_ids":["CWE-89"],`+
			`"vulnerabilities":[{"package":{"ecosystem":"npm","name":"lodash"},"patched_versions":"2"}]}`, true)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if json.Unmarshal(result, &got) != nil || got["ghsa_id"] != testGHSA || got["state"] != "draft" {
		t.Errorf("result = %s, want the advisory as GitHub reports it", result)
	}
	requests := f.recorded()
	if len(requests) != 2 || requests[0].method != http.MethodGet || requests[1].method != http.MethodPatch {
		t.Fatalf("requests = %+v, want the read first, then one PATCH", requests)
	}
	body := requests[1].body
	if len(body) != 4 || body["summary"] != "New summary" || body["severity"] != "low" || body["cwe_ids"] == nil ||
		body["vulnerabilities"] == nil {
		t.Errorf("body = %v, want only the four given members", body)
	}
	for _, forbidden := range []string{"state", "credits", "collaborating_users", "collaborating_teams", "cve_id"} {
		if _, ok := body[forbidden]; ok {
			t.Errorf("body carries %s", forbidden)
		}
	}
}

func TestAdvisoryUpdateRefusesAnythingButADraftWithoutAWrite(t *testing.T) {
	for _, state := range []string{"triage", "published", "closed", "withdrawn", ""} {
		t.Run("state "+state, func(t *testing.T) {
			f, _, core := handoverCore(t, advisoryRoutes(state, advisoryPatched)...)
			_, err := invoke(t, core, repositoryAdvisoriesUpdate.ID, "listed",
				`{"ghsa_id":"`+testGHSA+`","summary":"x"}`, true)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), "SECRETTEXT") {
				t.Errorf("err = %v, want an invalid request", err)
			}
			if len(handoverPatches(f)) != 0 || handoverWrites(f) != 0 {
				t.Errorf("a non-draft advisory reached a write: %+v", f.recorded())
			}
		})
	}
}

func TestAdvisoryUpdateRefusesForeignAnswersAndBadArgumentsBeforeSecrets(t *testing.T) {
	f, reads, core := handoverCore(t, advisoryRoutes("draft", advisoryPatched)...)
	for name, arguments := range map[string]string{
		"state":              `{"ghsa_id":"` + testGHSA + `","state":"published"}`,
		"credits":            `{"ghsa_id":"` + testGHSA + `","credits":[{"login":"a","type":"finder"}]}`,
		"collaborating user": `{"ghsa_id":"` + testGHSA + `","collaborating_users":["a"]}`,
		"collaborating team": `{"ghsa_id":"` + testGHSA + `","collaborating_teams":["a"]}`,
		"cve":                `{"ghsa_id":"` + testGHSA + `","cve_id":"CVE-2026-1"}`,
		"nothing to change":  `{"ghsa_id":"` + testGHSA + `"}`,
		"bad id":             `{"ghsa_id":"GHSA-nope","summary":"x"}`,
		"both severities":    `{"ghsa_id":"` + testGHSA + `","severity":"low","cvss_vector_string":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}`,
		"bad severity":       `{"ghsa_id":"` + testGHSA + `","severity":"unknown"}`,
		"bad vector":         `{"ghsa_id":"` + testGHSA + `","cvss_vector_string":"high"}`,
		"bad cwe":            `{"ghsa_id":"` + testGHSA + `","cwe_ids":["79"]}`,
		"empty summary":      `{"ghsa_id":"` + testGHSA + `","summary":""}`,
		"long summary":       `{"ghsa_id":"` + testGHSA + `","summary":"` + strings.Repeat("a", 1025) + `"}`,
		"bad ecosystem":      `{"ghsa_id":"` + testGHSA + `","vulnerabilities":[{"package":{"ecosystem":"cobol"}}]}`,
		"no package":         `{"ghsa_id":"` + testGHSA + `","vulnerabilities":[{"patched_versions":"1"}]}`,
		"repository outside": `{"repository":"other-org/example","ghsa_id":"` + testGHSA + `","summary":"x"}`,
	} {
		*reads = 0
		_, err := invoke(t, core, repositoryAdvisoriesUpdate.ID, "listed", arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", name, err)
		}
		if *reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("%s reached the credential or GitHub", name)
		}
	}
	// A direct caller that skips the schema still cannot carry a member the tool does not offer.
	var arguments advisoryUpdateArguments
	if err := json.Unmarshal([]byte(`{"ghsa_id":"`+testGHSA+`","state":"published"}`), &arguments); !isInvalidRequest(err) {
		t.Errorf("state member = %v, want an invalid request", err)
	}
}

func TestAdvisoryUpdateMapsRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range refusalCases {
		t.Run(tt.name, func(t *testing.T) {
			f, _, core := handoverCore(t, advisoryRoutes("draft", status(tt.status))...)
			_, err := invoke(t, core, repositoryAdvisoriesUpdate.ID, "listed",
				`{"ghsa_id":"`+testGHSA+`","summary":"x"}`, true)
			if err == nil || strings.Contains(err.Error(), "SECRETTEXT") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q without provider text", err, tt.want)
			}
			if tt.status == http.StatusUnprocessableEntity && !isInvalidRequest(err) {
				t.Errorf("422 = %v, want an invalid request", err)
			}
			if tt.class != "" && classOf(err) != tt.class {
				t.Errorf("class = %s, want %s", classOf(err), tt.class)
			}
			if len(handoverPatches(f)) != 1 {
				t.Errorf("patches = %d, want exactly 1", len(handoverPatches(f)))
			}
		})
	}
}

func TestAdvisoryUpdateReadFailureSendsNoWrite(t *testing.T) {
	f, _, core := handoverCore(t,
		handoverRoute{http.MethodGet, handoverAdvisory, status(http.StatusNotFound)})
	_, err := invoke(t, core, repositoryAdvisoriesUpdate.ID, "listed", `{"ghsa_id":"`+testGHSA+`","summary":"x"}`, true)
	if classOf(err) != provider.ClassNotFound || len(handoverPatches(f)) != 0 {
		t.Errorf("err = %v, patches %d, want not-found and no write", err, len(handoverPatches(f)))
	}
}

func TestAdvisoryUpdateNeverRepeatsAnUnclearChange(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"server error": status(http.StatusBadGateway),
		"unreadable":   func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "x") },
		"other advisory": func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, strings.Replace(advisoryDraft("draft"), testGHSA, "GHSA-2222-2222-2222", 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, core := handoverCore(t, advisoryRoutes("draft", answer)...)
			_, err := invoke(t, core, repositoryAdvisoriesUpdate.ID, "listed", `{"ghsa_id":"`+testGHSA+`","summary":"x"}`, true)
			if err == nil || !strings.Contains(err.Error(), "may have been applied") || len(handoverPatches(f)) != 1 {
				t.Errorf("err = %v, patches %d, want an unclear outcome after one PATCH", err, len(handoverPatches(f)))
			}
		})
	}
	t.Run("timeout", func(t *testing.T) {
		stall := newChangeStall(t)
		f, _, core := handoverCore(t, advisoryRoutes("draft", stall.answer)...)
		ctx, cancel := stall.context()
		defer cancel()
		_, err := core.Invoke(ctx, application.InvokeRequest{Operation: repositoryAdvisoriesUpdate.ID,
			Connection: "listed", Arguments: json.RawMessage(`{"ghsa_id":"` + testGHSA + `","summary":"x"}`), Confirmed: true})
		if classOf(err) != provider.ClassTimeout || !strings.Contains(err.Error(), "may have been applied") ||
			len(handoverPatches(f)) != 1 {
			t.Errorf("err = %v, patches %d", err, len(handoverPatches(f)))
		}
	})
}

func TestHandoverToolsAreListedOnlyConfirmedAndInNoProfile(t *testing.T) {
	ids := []string{repositoriesSetVisibility.ID, repositoriesTransfer.ID, pagesDelete.ID,
		repositoryAdvisoriesUpdate.ID}
	arguments := map[string]string{
		repositoriesSetVisibility.ID: `{"repository":"octo-org/example","visibility":"private"}`,
		repositoriesTransfer.ID: `{"repository":"octo-org/example","confirm_name":"octo-org/example",` +
			`"new_owner":"orgs/octo-archive"}`,
		pagesDelete.ID:                `{"repository":"octo-org/example"}`,
		repositoryAdvisoriesUpdate.ID: `{"ghsa_id":"` + testGHSA + `","summary":"x"}`,
	}
	f, reads, core := handoverCore(t)
	for _, id := range ids {
		var unsupported *capability.UnsupportedError
		if _, err := invoke(t, core, id, "unlisted", arguments[id], true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools entry = %v, want unsupported-capability", id, err)
		}
		*reads = 0
		if _, err := invoke(t, core, id, "owner", arguments[id], false); err == nil || *reads != 0 {
			t.Errorf("%s unconfirmed = %v, reads %d", id, err, *reads)
		}
		if _, err := invoke(t, core, id, "excluded", arguments[id], true); err == nil || *reads != 0 {
			t.Errorf("%s on an excluded repository = %v, reads %d", id, err, *reads)
		}
	}
	if len(f.recorded()) != 0 {
		t.Errorf("a refused call reached GitHub: %+v", f.recorded())
	}
	reg := registry(t)
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, tool := range profile.Tools {
			if contains(ids, tool) {
				t.Errorf("profile %s contains %s", profile.ID, tool)
			}
		}
	}
	for _, d := range reg.All() {
		if !contains(ids, d.ID) {
			continue
		}
		if !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired ||
			d.Risk.Effect == capability.EffectRead {
			t.Errorf("%s risk = %+v, listed-only %v", d.ID, d.Risk, d.RequiresToolAllowList)
		}
	}
}

// TestCatalogOmitsTheOperationsThatReachThirdParties pins the deliberate limit: nothing creates or changes a
// Pages site, publishes an advisory, or requests a CVE, and no argument of any tool offers public visibility,
// a state, credits, collaborators, or a CVE ID.
func TestCatalogOmitsTheOperationsThatReachThirdParties(t *testing.T) {
	reg := registry(t)
	for _, forbidden := range []string{Provider + ".pages.set", Provider + ".pages.create", Provider + ".pages.update",
		Provider + ".repository-advisories.publish", Provider + ".repository-advisories.request-cve",
		Provider + ".repository-advisories.create", Provider + ".repository-advisories.close",
		Provider + ".pages-builds.create", Provider + ".pages-builds.request"} {
		if _, _, ok := reg.Lookup(forbidden); ok {
			t.Errorf("%s exists", forbidden)
		}
	}
	for _, d := range reg.All() {
		if strings.Contains(d.ID, "request-cve") || strings.HasPrefix(d.ID, Provider+".pages.") &&
			d.ID != pagesGet.ID && d.ID != pagesDelete.ID {
			t.Errorf("unexpected tool %s", d.ID)
		}
		if !strings.HasPrefix(d.ID, Provider+".repository-advisories.") || d.Risk.Effect == capability.EffectRead {
			continue
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"state", "credits", "collaborating_users", "collaborating_teams", "cve_id"} {
			if _, ok := schema.Properties[name]; ok {
				t.Errorf("%s offers %s", d.ID, name)
			}
		}
		for _, argument := range d.Arguments {
			if strings.HasPrefix(argument.Name, "collaborating") || argument.Name == "state" ||
				argument.Name == "credits" || argument.Name == "cve_id" {
				t.Errorf("%s documents %s", d.ID, argument.Name)
			}
		}
	}
	var advisoryWrites int
	for _, d := range reg.All() {
		if strings.HasPrefix(d.ID, Provider+".repository-advisories.") && d.Risk.Effect != capability.EffectRead {
			advisoryWrites++
		}
	}
	if advisoryWrites != 1 {
		t.Errorf("repository advisory writes = %d, want only update", advisoryWrites)
	}
}
