package github

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// answerOf is what the fake GitHub answers one route with.
type answerOf struct {
	status int
	body   string
}

// securityTools are the four tools of the security switches and the code scanning default setup.
var securityTools = []string{securitySettingsGet.ID, securitySettingsUpdate.ID, codeScanningSetupGet.ID,
	codeScanningSetupUpdate.ID}

func securityConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectUpdate}
	cfg.Connections["sec"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: securityTools}
	cfg.Connections["unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all}
	cfg.Connections["excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Exclude: []string{repoTarget}, Effects: all, Tools: securityTools}
	return cfg
}

// serveSecurity answers the routes below the repository by "METHOD path-below-the-repository"; the repository
// itself is "GET ". A route that is not listed answers 404.
func serveSecurity(t *testing.T, answers map[string]answerOf) (*fakeGitHub, *application.Core, *int) {
	t.Helper()
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		below, ok := strings.CutPrefix(r.URL.Path, repositoryRoute)
		if !ok {
			return false
		}
		answer, ok := answers[r.Method+" "+strings.TrimPrefix(below, "/")]
		if !ok {
			answer = answerOf{status: http.StatusNotFound, body: `{"message":"provider-text-canary"}`}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.status)
		_, _ = w.Write([]byte(answer.body))
		return true
	}
	base := serve(t, f)
	reads := 0
	red := &redact.Redactor{}
	return f, application.New(registry(t), securityConfig(base), resolver(red, &reads), red), &reads
}

const (
	analysisAllOn = `{"security_and_analysis":{"advanced_security":{"status":"enabled"},` +
		`"code_security":{"status":"disabled"},"dependabot_security_updates":{"status":"enabled"},` +
		`"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"disabled"},` +
		`"secret_scanning_non_provider_patterns":{"status":"enabled"},"secret_scanning_ai_detection":{"status":"disabled"},` +
		`"secret_scanning_delegated_alert_dismissal":{"status":"enabled"},"secret_scanning_delegated_bypass":{"status":"enabled"},` +
		`"secret_scanning_delegated_bypass_options":{"reviewers":[{"reviewer_id":7,"reviewer_type":"TEAM","mode":"ALWAYS"},` +
		`{"reviewer_id":5,"reviewer_type":"ROLE","mode":"EXEMPT"}]}}}`
	changeOK = `{"id":1}`
)

func decodeResult(t *testing.T, result json.RawMessage) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	delete(got, "repository")
	return got
}

func TestSecuritySettingsGetBundlesTheReadsAndKeepsEveryField(t *testing.T) {
	f, core, _ := serveSecurity(t, map[string]answerOf{
		"GET ":                                {http.StatusOK, analysisAllOn},
		"GET vulnerability-alerts":            {http.StatusNoContent, ""},
		"GET automated-security-fixes":        {http.StatusOK, `{"enabled":true,"paused":true}`},
		"GET private-vulnerability-reporting": {http.StatusOK, `{"enabled":false}`},
		"GET immutable-releases":              {http.StatusOK, `{"enabled":true,"enforced_by_owner":true}`},
	})
	result, err := invoke(t, core, securitySettingsGet.ID, "sec", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"dependabot_alerts":                         map[string]any{"state": "enabled"},
		"automated_security_fixes":                  map[string]any{"state": "enabled", "paused": true},
		"private_vulnerability_reporting":           map[string]any{"state": "disabled"},
		"immutable_releases":                        map[string]any{"state": "enabled", "enforced_by_owner": true},
		"advanced_security":                         map[string]any{"state": "enabled"},
		"code_security":                             map[string]any{"state": "disabled"},
		"dependabot_security_updates":               map[string]any{"state": "enabled"},
		"secret_scanning":                           map[string]any{"state": "enabled"},
		"secret_scanning_push_protection":           map[string]any{"state": "disabled"},
		"secret_scanning_non_provider_patterns":     map[string]any{"state": "enabled"},
		"secret_scanning_ai_detection":              map[string]any{"state": "disabled"},
		"secret_scanning_delegated_alert_dismissal": map[string]any{"state": "enabled"},
		"secret_scanning_delegated_bypass":          map[string]any{"state": "enabled"},
		"secret_scanning_delegated_bypass_reviewers": []any{
			map[string]any{"reviewer_id": float64(7), "reviewer_type": "TEAM", "mode": "ALWAYS"},
			map[string]any{"reviewer_id": float64(5), "reviewer_type": "ROLE", "mode": "EXEMPT"}},
	}
	if got := decodeResult(t, result); !reflect.DeepEqual(got, want) {
		t.Errorf("result = %v, want %v", got, want)
	}
	requests := f.recorded()
	if len(requests) != 5 {
		t.Fatalf("requests = %+v, want the repository and four routes", requests)
	}
	for _, request := range requests {
		if request.method != http.MethodGet {
			t.Errorf("request %+v is not a read", request)
		}
	}
}

func TestSecuritySettingsGetReportsWhatGitHubDoesNotOfferAsUnavailable(t *testing.T) {
	_, core, _ := serveSecurity(t, map[string]answerOf{
		"GET ":                                {http.StatusOK, `{"permissions":{"admin":true}}`},
		"GET vulnerability-alerts":            {http.StatusNotFound, `{"message":"provider-text-canary"}`},
		"GET automated-security-fixes":        {http.StatusNotFound, `{"message":"provider-text-canary"}`},
		"GET private-vulnerability-reporting": {http.StatusUnprocessableEntity, `{"message":"provider-text-canary"}`},
		"GET immutable-releases":              {http.StatusNotFound, `{"message":"provider-text-canary"}`},
	})
	result, err := invoke(t, core, securitySettingsGet.ID, "sec", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, result)
	for name, want := range map[string]string{"dependabot_alerts": "disabled", "automated_security_fixes": "disabled",
		"private_vulnerability_reporting": "unavailable", "immutable_releases": "disabled",
		"advanced_security": "unavailable", "code_security": "unavailable", "secret_scanning": "unavailable",
		"secret_scanning_push_protection": "unavailable", "secret_scanning_delegated_bypass": "unavailable",
		"dependabot_security_updates": "unavailable"} {
		entry, _ := got[name].(map[string]any)
		if entry["state"] != want || len(entry) != 1 {
			t.Errorf("%s = %v, want state %s alone", name, got[name], want)
		}
	}
	if _, ok := got["secret_scanning_delegated_bypass_reviewers"]; ok {
		t.Errorf("reviewers of a missing feature: %v", got)
	}
	if strings.Contains(string(result), "canary") {
		t.Errorf("result carries provider text: %s", result)
	}
}

func TestSecuritySettingsGetNeedsShownAdminRightsToReadA404AsDisabled(t *testing.T) {
	for _, route := range []string{"vulnerability-alerts", "automated-security-fixes", "immutable-releases"} {
		answers := map[string]answerOf{
			"GET ":                                {http.StatusOK, `{"permissions":{"admin":false}}`},
			"GET vulnerability-alerts":            {http.StatusNoContent, ""},
			"GET automated-security-fixes":        {http.StatusOK, `{"enabled":true,"paused":false}`},
			"GET private-vulnerability-reporting": {http.StatusOK, `{"enabled":true}`},
			"GET immutable-releases":              {http.StatusOK, `{"enabled":true,"enforced_by_owner":false}`},
		}
		answers["GET "+route] = answerOf{http.StatusNotFound, `{"message":"provider-text-canary"}`}
		_, core, _ := serveSecurity(t, answers)
		_, err := invoke(t, core, securitySettingsGet.ID, "sec", `{}`, false)
		if err == nil || !strings.Contains(err.Error(), "administrator rights") || strings.Contains(err.Error(), "canary") {
			t.Errorf("%s: err = %v, want the missing proof of administrator rights", route, err)
		}
		// Without the permissions member the proof is missing as well.
		answers["GET "] = answerOf{http.StatusOK, `{}`}
		_, core, _ = serveSecurity(t, answers)
		if _, err := invoke(t, core, securitySettingsGet.ID, "sec", `{}`, false); err == nil {
			t.Errorf("%s: a repository without permissions proved administrator rights", route)
		}
	}
}

func TestSecuritySettingsGetFailsOnARefusedRouteWithoutProviderText(t *testing.T) {
	for name, tt := range map[string]struct {
		answers map[string]answerOf
		want    string
	}{
		"a refused route": {map[string]answerOf{"GET ": {http.StatusOK, `{}`},
			"GET vulnerability-alerts": {http.StatusForbidden, `{"message":"provider-text-canary"}`}}, "Administration: read"},
		"a hidden repository": {map[string]answerOf{"GET ": {http.StatusNotFound, `{"message":"provider-text-canary"}`}},
			"Administration: read"},
		"an unknown status": {map[string]answerOf{"GET ": {http.StatusOK,
			`{"security_and_analysis":{"secret_scanning":{"status":"provider-text-canary"}}}`}}, "invalid response"},
		"a route without its answer": {map[string]answerOf{"GET ": {http.StatusOK, `{}`},
			"GET vulnerability-alerts":     {http.StatusNoContent, ""},
			"GET automated-security-fixes": {http.StatusOK, `{}`}}, "invalid response"},
	} {
		t.Run(name, func(t *testing.T) {
			_, core, _ := serveSecurity(t, tt.answers)
			_, err := invoke(t, core, securitySettingsGet.ID, "sec", `{}`, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "canary") {
				t.Errorf("err = %v, want %q and no provider text", err, tt.want)
			}
		})
	}
}

func TestSecuritySettingsUpdateSendsEachRouteSwitchOnceWithoutABody(t *testing.T) {
	for _, tt := range []struct {
		argument, route string
		enable          bool
	}{
		{"dependabot_alerts", "vulnerability-alerts", true},
		{"dependabot_alerts", "vulnerability-alerts", false},
		{"automated_security_fixes", "automated-security-fixes", true},
		{"automated_security_fixes", "automated-security-fixes", false},
		{"private_vulnerability_reporting", "private-vulnerability-reporting", true},
		{"private_vulnerability_reporting", "private-vulnerability-reporting", false},
		{"immutable_releases", "immutable-releases", true},
		{"immutable_releases", "immutable-releases", false},
	} {
		method := http.MethodDelete
		requested := "disabled"
		if tt.enable {
			method, requested = http.MethodPut, "enabled"
		}
		t.Run(method+" "+tt.route, func(t *testing.T) {
			f, core, _ := serveSecurity(t, map[string]answerOf{method + " " + tt.route: {http.StatusNoContent, ""}})
			arguments, _ := json.Marshal(map[string]bool{tt.argument: tt.enable})
			result, err := invoke(t, core, securitySettingsUpdate.ID, "sec", string(arguments), true)
			if err != nil {
				t.Fatal(err)
			}
			requests := f.recorded()
			if len(requests) != 1 || requests[0].method != method || requests[0].path != repositoryRoute+"/"+tt.route ||
				requests[0].body != nil {
				t.Fatalf("requests = %+v, want one %s of the route without a body", requests, method)
			}
			want := map[string]any{"results": []any{map[string]any{"switch": tt.argument, "requested": requested,
				"outcome": "set"}}}
			if got := decodeResult(t, result); !reflect.DeepEqual(got, want) {
				t.Errorf("result = %v, want %v", got, want)
			}
		})
	}
}

func TestSecuritySettingsUpdateSendsEachAnalysisSwitchAloneThroughThePatch(t *testing.T) {
	for _, name := range []string{"secret_scanning", "secret_scanning_push_protection",
		"secret_scanning_non_provider_patterns", "secret_scanning_ai_detection",
		"secret_scanning_delegated_alert_dismissal", "secret_scanning_delegated_bypass"} {
		t.Run(name, func(t *testing.T) {
			reply := `{"security_and_analysis":{"` + name + `":{"status":"enabled"}}}`
			f, core, _ := serveSecurity(t, map[string]answerOf{"PATCH ": {http.StatusOK, reply}})
			result, err := invoke(t, core, securitySettingsUpdate.ID, "sec", `{"`+name+`":true}`, true)
			if err != nil {
				t.Fatal(err)
			}
			requests := f.recorded()
			want := map[string]any{"security_and_analysis": map[string]any{name: map[string]any{"status": "enabled"}}}
			if len(requests) != 1 || requests[0].method != http.MethodPatch || requests[0].path != repositoryRoute ||
				!reflect.DeepEqual(requests[0].body, want) {
				t.Fatalf("requests = %+v, want one PATCH with %v", requests, want)
			}
			wantResult := map[string]any{"results": []any{map[string]any{"switch": name, "requested": "enabled",
				"outcome": "set", "reported": "enabled"}}}
			if got := decodeResult(t, result); !reflect.DeepEqual(got, wantResult) {
				t.Errorf("result = %v, want %v", got, wantResult)
			}
		})
	}
}

func outcomes(t *testing.T, result json.RawMessage) []string {
	t.Helper()
	var got struct {
		Results []SwitchResult `json:"results"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	var list []string
	for _, entry := range got.Results {
		list = append(list, entry.Switch+"="+entry.Outcome+":"+entry.Reason)
	}
	return list
}

func TestSecuritySettingsUpdateReportsAPartialSuccessPerSwitch(t *testing.T) {
	f, core, _ := serveSecurity(t, map[string]answerOf{
		"PUT vulnerability-alerts":     {http.StatusNoContent, ""},
		"PUT automated-security-fixes": {http.StatusForbidden, `{"message":"provider-text-canary"}`},
		"PUT immutable-releases":       {http.StatusConflict, `{"message":"provider-text-canary"}`},
		"PATCH ":                       {http.StatusUnprocessableEntity, `{"message":"provider-text-canary"}`},
	})
	result, err := invoke(t, core, securitySettingsUpdate.ID, "sec",
		`{"dependabot_alerts":true,"automated_security_fixes":true,"immutable_releases":true,"secret_scanning":true,`+
			`"private_vulnerability_reporting":true}`, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dependabot_alerts=set:", "automated_security_fixes=rejected:permission",
		"private_vulnerability_reporting=rejected:not-found", "immutable_releases=rejected:conflict",
		"secret_scanning=rejected:invalid"}
	if got := outcomes(t, result); !reflect.DeepEqual(got, want) {
		t.Errorf("outcomes = %v, want %v", got, want)
	}
	if got := len(f.recorded()); got != 5 {
		t.Errorf("requests = %d, want one per switch", got)
	}
	if strings.Contains(string(result), "canary") {
		t.Errorf("result carries provider text: %s", result)
	}
}

func TestSecuritySettingsUpdateSendsNothingAfterAnUnclearOutcome(t *testing.T) {
	for name, tt := range map[string]struct {
		status int
		body   string
	}{
		"a server error":      {http.StatusBadGateway, `{"message":"provider-text-canary"}`},
		"a gateway timeout":   {http.StatusGatewayTimeout, `{"message":"provider-text-canary"}`},
		"an unreadable reply": {http.StatusOK, `not json`},
		"another state":       {http.StatusOK, `{"security_and_analysis":{"secret_scanning":{"status":"disabled"}}}`},
	} {
		t.Run(name, func(t *testing.T) {
			f, core, _ := serveSecurity(t, map[string]answerOf{
				"PUT vulnerability-alerts": {http.StatusNoContent, ""},
				"PATCH ":                   {tt.status, tt.body},
				"PUT immutable-releases":   {http.StatusNoContent, ""},
			})
			result, err := invoke(t, core, securitySettingsUpdate.ID, "sec",
				`{"dependabot_alerts":true,"secret_scanning":true,"secret_scanning_push_protection":true,`+
					`"immutable_releases":true}`, true)
			if err != nil {
				t.Fatal(err)
			}
			// The switches go in a fixed order: the routes first, then the analysis members.
			want := []string{"dependabot_alerts=set:", "immutable_releases=set:", "secret_scanning=unknown:",
				"secret_scanning_push_protection=not-sent:"}
			if got := outcomes(t, result); !reflect.DeepEqual(got, want) {
				t.Errorf("outcomes = %v, want %v", got, want)
			}
			if got := len(f.recorded()); got != 3 {
				t.Errorf("requests = %d, want the two that came before the unclear one and it", got)
			}
			if strings.Contains(string(result), "canary") {
				t.Errorf("result carries provider text: %s", result)
			}
		})
	}
}

func TestSecuritySettingsUpdateFailsWhenNoSwitchWasSet(t *testing.T) {
	for name, tt := range map[string]struct {
		status    int
		want      string
		uncertain bool
	}{
		"a refusal":      {http.StatusForbidden, "Administration: write", false},
		"a hidden route": {http.StatusNotFound, "Administration: write", false},
		"a server error": {http.StatusBadGateway, "unknown", true},
		"a timeout":      {http.StatusGatewayTimeout, "unknown", true},
	} {
		t.Run(name, func(t *testing.T) {
			f, core, _ := serveSecurity(t, map[string]answerOf{
				"PUT vulnerability-alerts": {tt.status, `{"message":"provider-text-canary"}`},
				"PUT immutable-releases":   {tt.status, `{"message":"provider-text-canary"}`},
			})
			_, err := invoke(t, core, securitySettingsUpdate.ID, "sec",
				`{"dependabot_alerts":true,"immutable_releases":true}`, true)
			if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "canary") ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain {
				t.Fatalf("err = %v, want %q, no provider text, uncertain = %t", err, tt.want, tt.uncertain)
			}
			wantRequests := 1
			if tt.status == http.StatusForbidden || tt.status == http.StatusNotFound {
				wantRequests = 2
			}
			if got := len(f.recorded()); got != wantRequests {
				t.Errorf("requests = %d, want %d", got, wantRequests)
			}
		})
	}
}

func TestSecuritySettingsUpdateStopsOnARateLimit(t *testing.T) {
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	}
	base := serve(t, f)
	red := &redact.Redactor{}
	core := application.New(registry(t), securityConfig(base), resolver(red, nil), red)
	_, err := invoke(t, core, securitySettingsUpdate.ID, "sec", `{"dependabot_alerts":true,"immutable_releases":true}`, true)
	if err == nil || !strings.Contains(err.Error(), "rate-limited") {
		t.Errorf("err = %v, want the rate limit", err)
	}
	if got := len(f.recorded()); got != 1 {
		t.Errorf("requests = %d, want one", got)
	}
}

func TestSecuritySettingsUpdateIsBoundedBeforeIO(t *testing.T) {
	f, core, reads := serveSecurity(t, nil)
	for name, arguments := range map[string]string{
		"empty":                `{}`,
		"billing":              `{"advanced_security":true}`,
		"code security":        `{"code_security":true}`,
		"dependabot via patch": `{"dependabot_security_updates":true}`,
		"bypass reviewers":     `{"secret_scanning_delegated_bypass_options":{"reviewers":[]}}`,
		"a wrong type":         `{"secret_scanning":"enabled"}`,
		"a status":             `{"secret_scanning":{"status":"enabled"}}`,
	} {
		*reads = 0
		if _, err := invoke(t, core, securitySettingsUpdate.ID, "sec", arguments, true); err == nil {
			t.Errorf("%s was accepted", name)
		}
		if *reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("%s: secret reads = %d, requests = %d; want none", name, *reads, len(f.recorded()))
		}
	}
	// The handler applies the same checks when the schema is not in front of it.
	_, handler, _ := registry(t).Lookup(securitySettingsUpdate.ID)
	for _, arguments := range []string{`{}`, `{"secret_scanning":"yes"}`} {
		*reads = 0
		if _, err := handler(t.Context(), resolvedConnection("sec", "http://127.0.0.1:1", repoTarget), nil, nil,
			[]byte(arguments)); err == nil || *reads != 0 {
			t.Errorf("handler %s = %v", arguments, err)
		}
	}
}

func TestSecurityToolsRefuseAForeignAndAnExcludedRepositoryBeforeIO(t *testing.T) {
	f, core, reads := serveSecurity(t, nil)
	for _, id := range securityTools {
		for connection, repository := range map[string]string{"sec": "other/secret-repo", "excluded": "octo-org/example"} {
			arguments := `{"repository":"` + repository + `"}`
			switch id {
			case securitySettingsUpdate.ID:
				arguments = `{"repository":"` + repository + `","secret_scanning":true}`
			case codeScanningSetupUpdate.ID:
				arguments = `{"repository":"` + repository + `","state":"configured"}`
			}
			_, err := invoke(t, core, id, connection, arguments, true)
			if err == nil {
				t.Errorf("%s accepted %s on %s", id, repository, connection)
				continue
			}
			if strings.Contains(err.Error(), "secret-repo") {
				t.Errorf("%s names the foreign target: %v", id, err)
			}
		}
	}
	if *reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("secret reads = %d, requests = %d; want none", *reads, len(f.recorded()))
	}
}

func TestSecurityChangesAreReachedOnlyThroughTheToolsList(t *testing.T) {
	f, core, reads := serveSecurity(t, nil)
	for _, id := range []string{securitySettingsUpdate.ID, codeScanningSetupUpdate.ID} {
		arguments := `{"secret_scanning":true}`
		if id == codeScanningSetupUpdate.ID {
			arguments = `{"state":"configured"}`
		}
		if _, err := invoke(t, core, id, "unlisted", arguments, true); err == nil {
			t.Errorf("%s ran on a connection without a tools list", id)
		}
	}
	if *reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("secret reads = %d, requests = %d; want none", *reads, len(f.recorded()))
	}
	// The reads are offered without a tools list.
	reg := registry(t)
	for _, id := range []string{securitySettingsGet.ID, codeScanningSetupGet.ID} {
		descriptor, _, ok := reg.Lookup(id)
		if !ok || descriptor.RequiresToolAllowList {
			t.Errorf("%s requires an allow-list", id)
		}
	}
	for _, id := range []string{securitySettingsUpdate.ID, codeScanningSetupUpdate.ID} {
		if descriptor, _, ok := reg.Lookup(id); !ok || !descriptor.RequiresToolAllowList {
			t.Errorf("%s does not require an allow-list", id)
		}
	}
}

func TestSecurityChangesNeedConfirmation(t *testing.T) {
	f, core, _ := serveSecurity(t, nil)
	if _, err := invoke(t, core, securitySettingsUpdate.ID, "sec", `{"secret_scanning":true}`, false); err == nil {
		t.Error("an unconfirmed change was accepted")
	}
	if _, err := invoke(t, core, codeScanningSetupUpdate.ID, "sec", `{"state":"configured"}`, false); err == nil {
		t.Error("an unconfirmed change was accepted")
	}
	if len(f.recorded()) != 0 {
		t.Errorf("requests = %+v, want none", f.recorded())
	}
}
