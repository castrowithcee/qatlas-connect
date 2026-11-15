package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// triageFake answers the alert update routes and the instance, analysis, and location reads of the bound
// repository through the failure hook of fakeGitHub. Code scanning alert 42, Dependabot alert 7, and secret
// scanning alert 3 exist; code scanning alert 43 reads as another alert (it answers number 44); every other
// number is unknown, which is how GitHub answers a number of another repository. Analysis 201 exists, and 202
// answers another ID. Every secret scanning answer carries the canary in every value-bearing member.
type triageFake struct {
	f *fakeGitHub
	// patch answers the update routes; nil answers the alert in the requested state.
	patch func(w http.ResponseWriter, req *http.Request)
	// instances, locations, and analysis replace the answers of the reads.
	instances, locations, analysis string
}

var triageKnown = map[string]int{"code-scanning": 42, "dependabot": 7, "secret-scanning": 3}

func (a *triageFake) route(w http.ResponseWriter, req *http.Request) bool {
	rest, ok := strings.CutPrefix(req.URL.Path, actionsPrefix)
	if !ok {
		return false
	}
	kind, tail, _ := strings.Cut(rest, "/")
	if _, known := triageKnown[kind]; !known {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	notFound := func() bool {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
		return true
	}
	if id, isAnalysis := strings.CutPrefix(tail, "analyses"); isAnalysis && kind == "code-scanning" {
		id = strings.Trim(id, "/")
		switch {
		case id == "":
			if req.URL.Query().Get("page") == "1" && req.URL.Query().Get("per_page") == "1" {
				w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
			}
			fmt.Fprint(w, "["+a.analysisJSON(201)+"]")
		case id == "201":
			fmt.Fprint(w, a.analysisJSON(201))
		case id == "202":
			fmt.Fprint(w, a.analysisJSON(999))
		default:
			return notFound()
		}
		return true
	}
	number, after, _ := strings.Cut(strings.TrimPrefix(tail, "alerts/"), "/")
	if !strings.HasPrefix(tail, "alerts/") {
		return false
	}
	n, err := strconv.Atoi(number)
	if err != nil || (n != triageKnown[kind] && !(kind == "code-scanning" && n == 43)) {
		return notFound()
	}
	if after == "instances" || after == "locations" {
		if req.URL.Query().Get("page") == "1" && req.URL.Query().Get("per_page") == "1" {
			w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
		}
	}
	switch {
	case after == "instances" && req.Method == http.MethodGet:
		if a.instances != "" {
			fmt.Fprint(w, a.instances)
		} else {
			fmt.Fprint(w, `[{"ref":"refs/heads/main","analysis_key":"codeql:build","environment":"{}","category":"codeql:build",`+
				`"state":"open","commit_sha":"39406e4","message":{"text":"depends on a user-provided value"},`+
				`"location":{"path":"lib/a.js","start_line":917,"end_line":918,"start_column":7,"end_column":18},`+
				`"html_url":"https://github.com/octo-org/example/blob/39406e4/lib/a.js#L917","classifications":["library"],`+
				`"snippet":"`+secretCanary+`"}]`)
		}
	case after == "locations" && req.Method == http.MethodGet:
		if a.locations != "" {
			fmt.Fprint(w, a.locations)
		} else {
			fmt.Fprint(w, triageLocations)
		}
	case after == "" && req.Method == http.MethodGet:
		shown := n
		if n == 43 {
			shown = 44
		}
		fmt.Fprintf(w, `{"number":%d,"state":"open","secret":%q,"snippet":%q}`, shown, secretCanary, secretCanary)
	case after == "" && req.Method == http.MethodPatch:
		if a.patch != nil {
			a.patch(w, req)
			return true
		}
		// The fake has already read the body for its record.
		requests := a.f.recorded()
		fmt.Fprint(w, defaultPatchAnswer(kind, n, requests[len(requests)-1].body))
	default:
		return false
	}
	return true
}

// defaultPatchAnswer is the alert in the state a change asked for. A secret scanning answer carries the
// canary like GitHub's own answer carries the secret.
func defaultPatchAnswer(kind string, number int, body map[string]any) string {
	state, _ := body["state"].(string)
	text := func(key string) string {
		value, _ := body[key].(string)
		return value
	}
	switch kind {
	case "code-scanning":
		return fmt.Sprintf(`{"number":%d,"state":%q,"html_url":"https://x/%d","dismissed_reason":%q,"dismissed_comment":%q,`+
			`"rule":{"id":"go/sql-injection"},"tool":{"name":"CodeQL"},"most_recent_instance":{"ref":"refs/heads/main"}}`,
			number, state, number, text("dismissed_reason"), text("dismissed_comment"))
	case "dependabot":
		return fmt.Sprintf(`{"number":%d,"state":%q,"html_url":"https://x/%d","dismissed_reason":%q,"dismissed_comment":%q,`+
			`"dependency":{"package":{"ecosystem":"npm","name":"lodash"}}}`,
			number, state, number, text("dismissed_reason"), text("dismissed_comment"))
	}
	return fmt.Sprintf(`{"number":%d,"state":%q,"html_url":"https://x/%d","resolution":%q,"secret_type":"github_personal_access_token",`+
		`"secret":%q,"secret_value":%q,"resolution_comment":%q,"first_location_detected":{"path":"src/a.go","start_line":3,`+
		`"snippet":%q,"secret":%q},"locations":[{"secret":%q}],"resolved_by":null}`,
		number, state, number, text("resolution"), secretCanary, secretCanary, secretCanary, secretCanary, secretCanary,
		secretCanary)
}

func (a *triageFake) analysisJSON(id int) string {
	if a.analysis != "" {
		return a.analysis
	}
	return fmt.Sprintf(`{"ref":"refs/heads/main","commit_sha":"c18c691","analysis_key":".github/workflows/codeql.yml:analyze",`+
		`"environment":"{\"language\":\"javascript\"}","error":%q,"category":".github/workflows/codeql.yml:analyze/language:javascript",`+
		`"created_at":"2021-01-13T11:55:49Z","results_count":3,"rules_count":67,"id":%d,`+
		`"url":"https://api.github.com/repos/octo-org/example/code-scanning/analyses/%d",`+
		`"sarif_id":"6c81cd8e-b078-4ac3-a3be-1dad7dbd0b53","tool":{"name":"CodeQL","guid":null,"version":"2.4.0"},`+
		`"deletable":true,"warning":"1 file was not analyzed"}`, strings.Repeat("e", 600), id, id)
}

const triageLocations = `[{"type":"commit","details":{"path":"/example/secrets.txt","start_line":1,"end_line":1,"start_column":1,` +
	`"end_column":64,"blob_sha":"af5626b","blob_url":"https://api.github.com/repos/octo-org/example/git/blobs/af5626b",` +
	`"commit_sha":"f14d7de","commit_url":"https://api.github.com/repos/octo-org/example/git/commits/f14d7de",` +
	`"secret":"` + secretCanary + `","snippet":"` + secretCanary + `","content":"` + secretCanary + `"}},` +
	`{"type":"wiki_commit","details":{"path":"/example/Home.md","start_line":2,"end_line":2,"blob_sha":"b1",` +
	`"page_url":"https://github.com/octo-org/example/wiki/Home","commit_sha":"c1","secret":"` + secretCanary + `"}},` +
	`{"type":"issue_title","details":{"issue_title_url":"https://api.github.com/repos/octo-org/example/issues/1347"}},` +
	`{"type":"pull_request_review_comment","details":{"pull_request_review_comment_url":"https://api.github.com/repos/octo-org/example/pulls/comments/1"}},` +
	`{"type":7,"details":"` + secretCanary + `"}]`

func triageConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	listed := []string{codeScanningAlertsUpdate.ID, dependabotAlertsUpdate.ID, secretScanningAlertsUpdate.ID}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["repo-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: append(append([]string(nil), listed...), codeScanningAlertInstancesList.ID,
			codeScanningAnalysesList.ID, codeScanningAnalysesGet.ID, secretScanningLocationsList.ID)}
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: all,
		Tools: append(append([]string(nil), listed...), codeScanningAnalysesList.ID), Exclude: []string{"repos/octo-org/example"}}
	return cfg
}

func serveTriage(t *testing.T) (*triageFake, *application.Core, *int) {
	t.Helper()
	a := &triageFake{f: &fakeGitHub{}}
	a.f.failure = a.route
	base := serve(t, a.f)
	reads := 0
	red := &redact.Redactor{}
	return a, application.New(registry(t), triageConfig(base), resolver(red, &reads), red), &reads
}

// changes lists the requests that are not reads.
func (a *triageFake) changes() []recorded {
	var out []recorded
	for _, r := range a.f.recorded() {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

// Every update reads its alert below the bound repository first, sends exactly one PATCH with only the fields
// its state needs, and answers with the alert as the get tool reads it.
func TestAlertUpdatesReadTheAlertThenSendOnePatch(t *testing.T) {
	a, core, _ := serveTriage(t)
	for _, tt := range []struct {
		id, args, path, want string
		body                 map[string]any
		query                string
	}{
		{codeScanningAlertsUpdate.ID, `{"alert_number":42,"state":"dismissed","dismissed_reason":"won't fix","dismissed_comment":"later","create_request":true}`,
			"code-scanning/alerts/42", `"dismissed_reason":"won't fix"`,
			map[string]any{"state": "dismissed", "dismissed_reason": "won't fix", "dismissed_comment": "later", "create_request": true}, ""},
		{codeScanningAlertsUpdate.ID, `{"alert_number":42,"state":"open"}`, "code-scanning/alerts/42", `"state":"open"`,
			map[string]any{"state": "open"}, ""},
		{dependabotAlertsUpdate.ID, `{"alert_number":7,"state":"dismissed","dismissed_reason":"tolerable_risk"}`,
			"dependabot/alerts/7", `"dismissed_reason":"tolerable_risk"`,
			map[string]any{"state": "dismissed", "dismissed_reason": "tolerable_risk"}, ""},
		{dependabotAlertsUpdate.ID, `{"alert_number":7,"state":"open"}`, "dependabot/alerts/7", `"state":"open"`,
			map[string]any{"state": "open"}, ""},
		{secretScanningAlertsUpdate.ID, `{"alert_number":3,"state":"resolved","resolution":"revoked","resolution_comment":"rotated"}`,
			"secret-scanning/alerts/3", `"resolution":"revoked"`,
			map[string]any{"state": "resolved", "resolution": "revoked", "resolution_comment": "rotated"}, "hide_secret=true"},
		{secretScanningAlertsUpdate.ID, `{"alert_number":3,"state":"open"}`, "secret-scanning/alerts/3", `"state":"open"`,
			map[string]any{"state": "open"}, "hide_secret=true"},
	} {
		before := len(a.f.recorded())
		got, err := invoke(t, core, tt.id, "repo-listed", tt.args, true)
		if err != nil || !strings.Contains(string(got), tt.want) || strings.Contains(string(got), "CANARY") ||
			!strings.Contains(string(got), `"repository":"octo-org/example"`) {
			t.Fatalf("%s %s = %s, %v", tt.id, tt.args, got, err)
		}
		sent := a.f.recorded()[before:]
		if len(sent) != 2 || sent[0].method != http.MethodGet || sent[1].method != http.MethodPatch ||
			sent[0].path != actionsPrefix+tt.path || sent[1].path != actionsPrefix+tt.path ||
			sent[0].query != tt.query || sent[1].query != "" {
			t.Fatalf("%s sent %+v, want a read and one PATCH of %s", tt.id, sent, tt.path)
		}
		if fmt.Sprint(sent[1].body) != fmt.Sprint(tt.body) {
			t.Errorf("%s sent %v, want %v", tt.id, sent[1].body, tt.body)
		}
	}
}

// The updates are offered only where a connection's tools list names them, need confirmation, and are in no
// profile.
func TestAlertUpdatesAreListedOnlyAndNeedConfirmation(t *testing.T) {
	a, core, _ := serveTriage(t)
	args := map[string]string{codeScanningAlertsUpdate.ID: `{"alert_number":42,"state":"open"}`,
		dependabotAlertsUpdate.ID: `{"alert_number":7,"state":"open"}`, secretScanningAlertsUpdate.ID: `{"alert_number":3,"state":"open"}`}
	var unsupported *capability.UnsupportedError
	for id, arguments := range args {
		if _, err := invoke(t, core, id, "repo", arguments, true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools list = %v, want unsupported-capability", id, err)
		}
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := invoke(t, core, id, "repo-listed", arguments, false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v", id, err)
		}
	}
	if n := len(a.f.recorded()); n != 0 {
		t.Errorf("refused calls sent %d requests", n)
	}
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, descriptor := range []capability.Descriptor{codeScanningAlertsUpdate, dependabotAlertsUpdate, secretScanningAlertsUpdate} {
		if descriptor.Risk.Confirmation != capability.ConfirmationRequired || descriptor.Risk.Effect != capability.EffectUpdate ||
			!descriptor.RequiresToolAllowList {
			t.Errorf("%s risk = %+v, listed-only = %t", descriptor.ID, descriptor.Risk, descriptor.RequiresToolAllowList)
		}
		for _, profile := range metadata.Profiles {
			for _, id := range profile.Tools {
				if id == descriptor.ID {
					t.Errorf("profile %s selects %s", profile.ID, id)
				}
			}
		}
	}
}

// Missing fields per state, values GitHub does not document, a comment above 280 characters, fields of
// another tool, the repository, and the exclusion are refused before a credential is resolved or a request is sent.
func TestAlertUpdatesAreCheckedBeforeSecretAccess(t *testing.T) {
	a, core, reads := serveTriage(t)
	long := strings.Repeat("c", 281)
	for _, tt := range []struct{ name, id, connection, args string }{
		{"zero number", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":0,"state":"open"}`},
		{"no state", dependabotAlertsUpdate.ID, "repo-listed", `{"alert_number":7}`},
		{"unknown state", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"fixed"}`},
		{"capitalized state", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"Open"}`},
		{"secret state dismissed", secretScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":3,"state":"dismissed","resolution":"revoked"}`},
		{"code scanning state resolved", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"resolved"}`},
		{"code scanning dismissal without reason", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"dismissed"}`},
		{"code scanning unknown reason", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"dismissed","dismissed_reason":"tolerable_risk"}`},
		{"code scanning reason on reopen", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"open","dismissed_reason":"false positive"}`},
		{"code scanning comment on reopen", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"open","dismissed_comment":"x"}`},
		{"code scanning request on reopen", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"open","create_request":false}`},
		{"code scanning long comment", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"dismissed","dismissed_reason":"false positive","dismissed_comment":"` + long + `"}`},
		{"code scanning control character", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"dismissed","dismissed_reason":"false positive","dismissed_comment":"a\u0007b"}`},
		{"code scanning assignees", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"open","assignees":["octocat"]}`},
		{"dependabot dismissal without reason", dependabotAlertsUpdate.ID, "repo-listed", `{"alert_number":7,"state":"dismissed"}`},
		{"dependabot unknown reason", dependabotAlertsUpdate.ID, "repo-listed", `{"alert_number":7,"state":"dismissed","dismissed_reason":"false positive"}`},
		{"dependabot reason on reopen", dependabotAlertsUpdate.ID, "repo-listed", `{"alert_number":7,"state":"open","dismissed_reason":"not_used"}`},
		{"dependabot create request", dependabotAlertsUpdate.ID, "repo-listed", `{"alert_number":7,"state":"dismissed","dismissed_reason":"not_used","create_request":true}`},
		{"dependabot long comment", dependabotAlertsUpdate.ID, "repo-listed", `{"alert_number":7,"state":"dismissed","dismissed_reason":"not_used","dismissed_comment":"` + long + `"}`},
		{"secret resolution missing", secretScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":3,"state":"resolved"}`},
		{"secret unknown resolution", secretScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":3,"state":"resolved","resolution":"fixed"}`},
		{"secret filter-only resolution", secretScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":3,"state":"resolved","resolution":"pattern_edited"}`},
		{"secret resolution on reopen", secretScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":3,"state":"open","resolution":"revoked"}`},
		{"secret over-long comment", secretScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":3,"state":"resolved","resolution":"revoked","resolution_comment":"` + strings.Repeat("c", 65537) + `"}`},
		{"secret control character", secretScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":3,"state":"resolved","resolution":"revoked","resolution_comment":"a\u0007b"}`},
		{"secret dismissal fields", secretScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":3,"state":"resolved","resolution":"revoked","dismissed_reason":"x"}`},
		{"foreign repository", codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":42,"state":"open","repository":"octo-org/other"}`},
		{"foreign repository dependabot", dependabotAlertsUpdate.ID, "repo-listed", `{"alert_number":7,"state":"open","repository":"octo-org/other"}`},
		{"foreign repository secret", secretScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":3,"state":"open","repository":"octo-org/other"}`},
		{"excluded repository", secretScanningAlertsUpdate.ID, "ex", `{"alert_number":3,"state":"open","repository":"octo-org/example"}`},
	} {
		if _, err := invoke(t, core, tt.id, tt.connection, tt.args, true); !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
	}
	if *reads != 0 || len(a.f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", *reads, len(a.f.recorded()))
	}
	// Comments of exactly 280 characters and every documented value pass.
	exact := strings.Repeat("é", 280)
	for _, reason := range codeScanningDismissReasons {
		if _, err := invoke(t, core, codeScanningAlertsUpdate.ID, "repo-listed",
			`{"alert_number":42,"state":"dismissed","dismissed_reason":"`+reason+`","dismissed_comment":"`+exact+`"}`, true); err != nil {
			t.Errorf("code scanning reason %q = %v", reason, err)
		}
	}
	for _, reason := range dependabotDismissReasons {
		if _, err := invoke(t, core, dependabotAlertsUpdate.ID, "repo-listed",
			`{"alert_number":7,"state":"dismissed","dismissed_reason":"`+reason+`"}`, true); err != nil {
			t.Errorf("Dependabot reason %q = %v", reason, err)
		}
	}
	if _, err := invoke(t, core, secretScanningAlertsUpdate.ID, "repo-listed",
		`{"alert_number":3,"state":"resolved","resolution":"revoked","resolution_comment":"`+long+`"}`, true); err != nil {
		t.Errorf("resolution comment above 280 characters = %v", err)
	}
	for _, reason := range secretResolveReasons {
		if _, err := invoke(t, core, secretScanningAlertsUpdate.ID, "repo-listed",
			`{"alert_number":3,"state":"resolved","resolution":"`+reason+`"}`, true); err != nil {
			t.Errorf("secret resolution %q = %v", reason, err)
		}
	}
}

// A number the repository does not hold is refused by the read, and so is an answer of another alert; neither
// sends a PATCH. An answer to the change that names another alert is reported as uncertain.
func TestAlertUpdatesAreBoundToTheRepositoryByTheRead(t *testing.T) {
	a, core, _ := serveTriage(t)
	for _, tt := range []struct{ id, args string }{
		{codeScanningAlertsUpdate.ID, `{"alert_number":99,"state":"open"}`},
		{dependabotAlertsUpdate.ID, `{"alert_number":99,"state":"open"}`},
		{secretScanningAlertsUpdate.ID, `{"alert_number":99,"state":"open"}`},
	} {
		if _, err := invoke(t, core, tt.id, "repo-listed", tt.args, true); classOf(err) != provider.ClassNotFound {
			t.Errorf("%s on an unknown number = %v, want not-found", tt.id, err)
		}
	}
	if _, err := invoke(t, core, codeScanningAlertsUpdate.ID, "repo-listed", `{"alert_number":43,"state":"open"}`, true); classOf(err) != provider.ClassInvalidResponse ||
		strings.Contains(err.Error(), uncertain) {
		t.Errorf("an answer of another alert = %v, want an invalid response of a read", err)
	}
	if changes := a.changes(); len(changes) != 0 {
		t.Errorf("unbound alerts were changed: %+v", changes)
	}
	a.patch = func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"number":1000,"state":"open"}`) }
	for _, tt := range []struct{ id, args string }{
		{codeScanningAlertsUpdate.ID, `{"alert_number":42,"state":"open"}`},
		{dependabotAlertsUpdate.ID, `{"alert_number":7,"state":"open"}`},
		{secretScanningAlertsUpdate.ID, `{"alert_number":3,"state":"open"}`},
	} {
		if _, err := invoke(t, core, tt.id, "repo-listed", tt.args, true); classOf(err) != provider.ClassInvalidResponse ||
			!strings.Contains(err.Error(), uncertain) {
			t.Errorf("%s answered another alert = %v, want uncertain", tt.id, err)
		}
	}
}

// Refusals of the change name no provider text; an unclear outcome is sent once and reported as uncertain.
func TestAlertUpdateFailures(t *testing.T) {
	status := func(code int) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			fmt.Fprint(w, `{"message":"canary `+secretCanary+`"}`)
		}
	}
	for name, tt := range map[string]struct {
		answer    func(http.ResponseWriter, *http.Request)
		class     provider.Class
		uncertain bool
	}{
		"forbidden":       {status(http.StatusForbidden), provider.ClassPermission, false},
		"not found":       {status(http.StatusNotFound), provider.ClassNotFound, false},
		"conflict":        {status(http.StatusConflict), provider.ClassProviderError, false},
		"gone":            {status(http.StatusGone), provider.ClassProviderError, false},
		"unprocessable":   {status(http.StatusUnprocessableEntity), provider.ClassProviderError, false},
		"gateway timeout": {status(http.StatusGatewayTimeout), provider.ClassTimeout, true},
		"server error":    {status(http.StatusBadGateway), provider.ClassProviderError, true},
		"unreadable": {func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"number":3,"secret": "%s" broken`, secretCanary)
		}, provider.ClassInvalidResponse, true},
		"dropped connection": {func(w http.ResponseWriter, _ *http.Request) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		}, "", true},
	} {
		t.Run(name, func(t *testing.T) {
			a, core, _ := serveTriage(t)
			a.patch = tt.answer
			for _, call := range []struct{ id, args string }{
				{codeScanningAlertsUpdate.ID, `{"alert_number":42,"state":"dismissed","dismissed_reason":"false positive"}`},
				{dependabotAlertsUpdate.ID, `{"alert_number":7,"state":"dismissed","dismissed_reason":"not_used"}`},
				{secretScanningAlertsUpdate.ID, `{"alert_number":3,"state":"resolved","resolution":"revoked"}`},
			} {
				before := len(a.changes())
				_, err := invoke(t, core, call.id, "repo-listed", call.args, true)
				if err == nil || strings.Contains(err.Error(), "canary") || strings.Contains(err.Error(), "CANARY") ||
					strings.Contains(err.Error(), uncertain) != tt.uncertain ||
					(tt.class != "" && classOf(err) != tt.class) {
					t.Errorf("%s = %v, want class %q, uncertain %v", call.id, err, tt.class, tt.uncertain)
				}
				if n := len(a.changes()) - before; n != 1 {
					t.Errorf("%s sent %d changes, want exactly one", call.id, n)
				}
			}
		})
	}
}

// A refused read before the change names no provider text either, and no PATCH follows.
func TestAlertUpdateReadFailureSendsNoChange(t *testing.T) {
	a, core, _ := serveTriage(t)
	a.f.failure = func(w http.ResponseWriter, req *http.Request) bool {
		if req.Method == http.MethodGet {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"no `+secretCanary+`"}`)
			return true
		}
		return a.route(w, req)
	}
	for id, args := range map[string]string{codeScanningAlertsUpdate.ID: `{"alert_number":42,"state":"open"}`,
		secretScanningAlertsUpdate.ID: `{"alert_number":3,"state":"open"}`} {
		_, err := invoke(t, core, id, "repo-listed", args, true)
		if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), "CANARY") ||
			!strings.Contains(err.Error(), "read and write") {
			t.Errorf("%s = %v, want the permission message", id, err)
		}
	}
	if changes := a.changes(); len(changes) != 0 {
		t.Errorf("changes after a refused read: %+v", changes)
	}
}

// No answer path carries the secret: not the answer of an update, the list of locations, nor an error.
func TestAlertTriageNeverExposesTheSecret(t *testing.T) {
	a, core, _ := serveTriage(t)
	calls := []struct{ id, args string }{
		{secretScanningAlertsUpdate.ID, `{"alert_number":3,"state":"resolved","resolution":"revoked","resolution_comment":"rotated"}`},
		{secretScanningAlertsUpdate.ID, `{"alert_number":3,"state":"open"}`},
		{secretScanningLocationsList.ID, `{"alert_number":3}`},
		{codeScanningAlertInstancesList.ID, `{"alert_number":42}`},
	}
	for _, mode := range []string{"", "bad-json", "status", "huge"} {
		switch mode {
		case "":
			a.locations, a.instances = "", ""
		case "bad-json":
			a.patch = func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, `{"secret": "%s" broken`, secretCanary) }
			a.locations = `[{"type":"commit","details":{"secret":"` + secretCanary + `"` + ` broken`
		case "status":
			a.patch = func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"message":"boom `+secretCanary+`"}`)
			}
			a.locations = ""
		case "huge":
			a.patch = func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"number":3,"secret": "%s"}`, secretCanary+strings.Repeat("x", maxResponseBytes))
			}
			a.locations = `[{"type":"commit","details":{"secret":"` + secretCanary + strings.Repeat("x", maxResponseBytes) + `"}}]`
		}
		for _, call := range calls {
			got, err := invoke(t, core, call.id, "repo-listed", call.args, true)
			text := string(got)
			if err != nil {
				text += err.Error()
			}
			if strings.Contains(text, secretCanary) || strings.Contains(text, "CANARY") {
				t.Errorf("mode %q %s leaked the secret: %s", mode, call.id, text)
			}
			if mode == "" && err != nil {
				t.Errorf("mode %q %s = %v", mode, call.id, err)
			}
			failing := strings.HasSuffix(call.id, "alerts.update") || (call.id == secretScanningLocationsList.ID &&
				(mode == "bad-json" || mode == "huge"))
			if mode != "" && failing && err == nil {
				t.Errorf("mode %q %s = %s, want an error", mode, call.id, got)
			}
		}
	}
}

func TestAlertInstancesListReportsEveryDocumentedMemberAndPages(t *testing.T) {
	a, core, reads := serveTriage(t)
	got, err := invoke(t, core, codeScanningAlertInstancesList.ID, "repo", `{"alert_number":42,"ref":"refs/heads/main","pr":7,"limit":1}`, false)
	if err != nil {
		t.Fatal(err)
	}
	value := decodeAlert(t, got)
	for path, want := range map[string]any{"ref": "refs/heads/main", "analysis_key": "codeql:build", "environment": "{}",
		"category": "codeql:build", "state": "open", "commit_sha": "39406e4", "message.text": "depends on a user-provided value",
		"location.path": "lib/a.js", "location.start_line": float64(917), "location.end_line": float64(918),
		"location.start_column": float64(7), "location.end_column": float64(18),
		"url": "https://github.com/octo-org/example/blob/39406e4/lib/a.js#L917"} {
		steps := []any{"instances", 0}
		for _, step := range strings.Split(path, ".") {
			steps = append(steps, step)
		}
		if g := at(t, value, steps...); g != want {
			t.Errorf("instances[0].%s = %v, want %v", path, g, want)
		}
	}
	if g := at(t, value, "instances", 0, "classifications", 0); g != "library" {
		t.Errorf("classification = %v", g)
	}
	if strings.Contains(string(got), "CANARY") {
		t.Errorf("an unnamed member reached the result: %s", got)
	}
	last := a.f.recorded()[len(a.f.recorded())-1]
	for _, want := range []string{"ref=refs%2Fheads%2Fmain", "pr=7", "per_page=1", "page=1"} {
		if !strings.Contains(last.query, want) {
			t.Errorf("query %q lacks %s", last.query, want)
		}
	}
	if last.path != actionsPrefix+"code-scanning/alerts/42/instances" || *reads == 0 {
		t.Errorf("path = %s", last.path)
	}
	var page CodeScanningInstanceList
	if json.Unmarshal(got, &page) != nil || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("page = %s", got)
	}
	if _, err := invoke(t, core, codeScanningAlertInstancesList.ID, "repo", `{"alert_number":43,"cursor":"`+page.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("cursor of another alert = %v, want invalid request", err)
	}
	if _, err := invoke(t, core, codeScanningAlertInstancesList.ID, "repo", `{"alert_number":42,"ref":"refs/heads/main","pr":7,"cursor":"`+page.NextCursor+`"}`, false); err != nil {
		t.Errorf("continuation = %v", err)
	}
	if _, err := invoke(t, core, codeScanningAlertInstancesList.ID, "repo", `{"alert_number":99}`, false); classOf(err) != provider.ClassNotFound {
		t.Errorf("alert of another repository = %v, want not-found", err)
	}
}

func TestCodeScanningAnalysesReportEveryDocumentedMember(t *testing.T) {
	a, core, _ := serveTriage(t)
	got, err := invoke(t, core, codeScanningAnalysesList.ID, "repo",
		`{"tool_name":"CodeQL","ref":"refs/heads/main","pr":9,"sarif_id":"6c81","sort":"created","direction":"asc","limit":1}`, false)
	if err != nil {
		t.Fatal(err)
	}
	single, err := invoke(t, core, codeScanningAnalysesGet.ID, "repo", `{"analysis_id":201}`, false)
	if err != nil {
		t.Fatal(err)
	}
	for prefix, value := range map[string]any{"analyses.0.": decodeAlert(t, got), "": decodeAlert(t, single)} {
		path := func(rest ...any) []any {
			var steps []any
			if prefix != "" {
				steps = append(steps, "analyses", 0)
			}
			return append(steps, rest...)
		}
		for member, want := range map[string]any{"id": float64(201), "ref": "refs/heads/main", "commit_sha": "c18c691",
			"analysis_key": ".github/workflows/codeql.yml:analyze", "environment": `{"language":"javascript"}`,
			"category":   ".github/workflows/codeql.yml:analyze/language:javascript",
			"created_at": "2021-01-13T11:55:49Z", "results_count": float64(3), "rules_count": float64(67),
			"sarif_id": "6c81cd8e-b078-4ac3-a3be-1dad7dbd0b53", "deletable": true, "warning": "1 file was not analyzed",
			"truncated": true} {
			if g := at(t, value, path(member)...); g != want {
				t.Errorf("%s%s = %v, want %v", prefix, member, g, want)
			}
		}
		if g := at(t, value, path("tool", "name")...); g != "CodeQL" {
			t.Errorf("tool = %v", g)
		}
		if g := at(t, value, path("tool", "version")...); g != "2.4.0" {
			t.Errorf("tool version = %v", g)
		}
		if g := at(t, value, path("links", "url")...); g != "https://api.github.com/repos/octo-org/example/code-scanning/analyses/201" {
			t.Errorf("links.url = %v", g)
		}
		if e, _ := at(t, value, path("error")...).(string); len([]rune(e)) > alertTextLimit {
			t.Errorf("error is %d characters long, want it cut", len([]rune(e)))
		}
	}
	query := ""
	for _, r := range a.f.recorded() {
		if strings.HasSuffix(r.path, "/analyses") {
			query = r.query
		}
	}
	for _, want := range []string{"tool_name=CodeQL", "ref=refs%2Fheads%2Fmain", "pr=9", "sarif_id=6c81", "sort=created",
		"direction=asc", "per_page=1", "page=1"} {
		if !strings.Contains(query, want) {
			t.Errorf("analyses query %q lacks %s", query, want)
		}
	}
	var page CodeScanningAnalysisList
	if json.Unmarshal(got, &page) != nil || !page.HasMore || page.NextCursor == "" {
		t.Errorf("page = %s", got)
	}
}

// An analysis ID is bound to the repository by the read: another repository's ID is not found, and an answer
// of another ID is refused.
func TestCodeScanningAnalysisIsBoundToTheRepository(t *testing.T) {
	_, core, _ := serveTriage(t)
	if _, err := invoke(t, core, codeScanningAnalysesGet.ID, "repo", `{"analysis_id":777}`, false); classOf(err) != provider.ClassNotFound ||
		!strings.Contains(err.Error(), "code scanning analysis 777") {
		t.Errorf("unknown analysis = %v, want not-found", err)
	}
	if _, err := invoke(t, core, codeScanningAnalysesGet.ID, "repo", `{"analysis_id":202}`, false); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("answer of another analysis = %v, want an invalid response", err)
	}
}

func TestSecretScanningLocationsNameWhereNeverWhat(t *testing.T) {
	_, core, _ := serveTriage(t)
	got, err := invoke(t, core, secretScanningLocationsList.ID, "repo", `{"alert_number":3}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "CANARY") || strings.Contains(string(got), secretCanary) {
		t.Fatalf("a location carries the secret: %s", got)
	}
	value := decodeAlert(t, got)
	for _, tt := range []struct {
		path []any
		want any
	}{
		{[]any{"locations", 0, "type"}, "commit"},
		{[]any{"locations", 0, "path"}, "/example/secrets.txt"},
		{[]any{"locations", 0, "start_line"}, float64(1)},
		{[]any{"locations", 0, "end_line"}, float64(1)},
		{[]any{"locations", 0, "start_column"}, float64(1)},
		{[]any{"locations", 0, "end_column"}, float64(64)},
		{[]any{"locations", 0, "blob_sha"}, "af5626b"},
		{[]any{"locations", 0, "commit_sha"}, "f14d7de"},
		{[]any{"locations", 0, "links", "blob_url"}, "https://api.github.com/repos/octo-org/example/git/blobs/af5626b"},
		{[]any{"locations", 0, "links", "commit_url"}, "https://api.github.com/repos/octo-org/example/git/commits/f14d7de"},
		{[]any{"locations", 1, "type"}, "wiki_commit"},
		{[]any{"locations", 1, "links", "page_url"}, "https://github.com/octo-org/example/wiki/Home"},
		{[]any{"locations", 2, "type"}, "issue_title"},
		{[]any{"locations", 2, "links", "issue_title_url"}, "https://api.github.com/repos/octo-org/example/issues/1347"},
		{[]any{"locations", 3, "type"}, "pull_request_review_comment"},
		{[]any{"locations", 3, "links", "pull_request_review_comment_url"}, "https://api.github.com/repos/octo-org/example/pulls/comments/1"},
	} {
		if g := at(t, value, tt.path...); g != tt.want {
			t.Errorf("%v = %v, want %v", tt.path, g, tt.want)
		}
	}
	if n := len(value["locations"].([]any)); n != 5 {
		t.Errorf("locations = %d, want one entry for every location GitHub sent", n)
	}
	if _, err := invoke(t, core, secretScanningLocationsList.ID, "repo", `{"alert_number":99}`, false); classOf(err) != provider.ClassNotFound {
		t.Errorf("alert of another repository = %v, want not-found", err)
	}
}

// The reads check arguments, the repository, and the exclusion before a credential is resolved.
func TestAlertDetailReadsAreCheckedBeforeSecretAccess(t *testing.T) {
	a, core, reads := serveTriage(t)
	for _, tt := range []struct{ id, connection, args string }{
		{codeScanningAlertInstancesList.ID, "repo", `{"alert_number":0}`},
		{codeScanningAlertInstancesList.ID, "repo", `{"alert_number":42,"pr":0}`},
		{codeScanningAlertInstancesList.ID, "repo", `{"alert_number":42,"ref":"a b"}`},
		{codeScanningAlertInstancesList.ID, "repo", `{"alert_number":42,"repository":"octo-org/other"}`},
		{codeScanningAnalysesList.ID, "repo", `{"tool_name":"CodeQL","tool_guid":"abc"}`},
		{codeScanningAnalysesList.ID, "repo", `{"tool_guid":"` + strings.Repeat("g", 257) + `"}`},
		{codeScanningAnalysesList.ID, "repo", `{"pr":0}`},
		{codeScanningAnalysesList.ID, "repo", `{"sort":"updated"}`},
		{codeScanningAnalysesList.ID, "repo", `{"direction":"up"}`},
		{codeScanningAnalysesList.ID, "repo", `{"sarif_id":"a\u0007b"}`},
		{codeScanningAnalysesList.ID, "repo", `{"repository":"octo-org/other"}`},
		{codeScanningAnalysesGet.ID, "repo", `{"analysis_id":0}`},
		{codeScanningAnalysesGet.ID, "repo", `{"analysis_id":201,"repository":"octo-org/other"}`},
		{secretScanningLocationsList.ID, "repo", `{"alert_number":0}`},
		{secretScanningLocationsList.ID, "repo", `{"alert_number":3,"repository":"octo-org/other"}`},
		{secretScanningLocationsList.ID, "planning", `{"alert_number":3}`},
		{codeScanningAnalysesList.ID, "ex", `{"repository":"octo-org/example"}`},
	} {
		if _, err := invoke(t, core, tt.id, tt.connection, tt.args, false); !isInvalidRequest(err) {
			t.Errorf("%s(%s) on %s = %v, want an invalid request", tt.id, tt.args, tt.connection, err)
		}
	}
	if *reads != 0 || len(a.f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", *reads, len(a.f.recorded()))
	}
}

func TestSecurityProfileHoldsTheReadsButNoUpdate(t *testing.T) {
	metadata, _ := registry(t).ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		if profile.ID != "security" {
			continue
		}
		have := map[string]bool{}
		for _, id := range profile.Tools {
			have[id] = true
		}
		for _, id := range []string{codeScanningAlertInstancesList.ID, codeScanningAnalysesList.ID, codeScanningAnalysesGet.ID,
			secretScanningLocationsList.ID} {
			if !have[id] {
				t.Errorf("security profile lacks %s", id)
			}
		}
		for _, id := range []string{codeScanningAlertsUpdate.ID, dependabotAlertsUpdate.ID, secretScanningAlertsUpdate.ID} {
			if have[id] {
				t.Errorf("security profile selects %s", id)
			}
		}
		if got := metadata.ProfileEffects(profile); len(got) != 1 || got[0] != config.EffectRead {
			t.Errorf("security permissions = %v, want read only", got)
		}
	}
}
