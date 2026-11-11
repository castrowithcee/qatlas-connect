package github

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const (
	setupRoute  = "GET code-scanning/default-setup"
	setupChange = "PATCH code-scanning/default-setup"
	setupAnswer = `{"state":"configured","languages":["go","python","swift"],"runner_type":"labeled",` +
		`"runner_label":"scan-pool","query_suite":"extended","threat_model":"remote_and_local",` +
		`"updated_at":"2026-11-01T10:00:00Z","schedule":"weekly"}`
)

func TestCodeScanningSetupGetReadsEveryDocumentedField(t *testing.T) {
	f, core, _ := serveSecurity(t, map[string]answerOf{setupRoute: {http.StatusOK, setupAnswer}})
	result, err := invoke(t, core, codeScanningSetupGet.ID, "sec", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"state": "configured", "languages": []any{"go", "python", "swift"},
		"runner_type": "labeled", "runner_label": "scan-pool", "query_suite": "extended",
		"threat_model": "remote_and_local", "updated_at": "2026-11-01T10:00:00Z", "schedule": "weekly"}
	if got := decodeResult(t, result); !reflect.DeepEqual(got, want) {
		t.Errorf("result = %v, want %v", got, want)
	}
	if requests := f.recorded(); len(requests) != 1 || requests[0].method != http.MethodGet {
		t.Errorf("requests = %+v, want one read", requests)
	}
}

func TestCodeScanningSetupGetAnswersNullsAsAbsentMembers(t *testing.T) {
	_, core, _ := serveSecurity(t, map[string]answerOf{setupRoute: {http.StatusOK,
		`{"state":"not-configured","languages":[],"runner_type":null,"runner_label":null,"query_suite":"default",` +
			`"threat_model":"remote","updated_at":null,"schedule":null}`}})
	result, err := invoke(t, core, codeScanningSetupGet.ID, "sec", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"state": "not-configured", "query_suite": "default", "threat_model": "remote"}
	if got := decodeResult(t, result); !reflect.DeepEqual(got, want) {
		t.Errorf("result = %v, want %v", got, want)
	}
}

func TestCodeScanningSetupGetReportsALicenseOrServiceLimitAsUnavailable(t *testing.T) {
	for name, status := range map[string]int{"403": http.StatusForbidden, "503": http.StatusServiceUnavailable} {
		_, core, _ := serveSecurity(t, map[string]answerOf{setupRoute: {status, `{"message":"provider-text-canary"}`}})
		result, err := invoke(t, core, codeScanningSetupGet.ID, "sec", `{}`, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := decodeResult(t, result); !reflect.DeepEqual(got, map[string]any{"state": "unavailable"}) {
			t.Errorf("%s: result = %v, want state unavailable alone", name, got)
		}
	}
}

func TestCodeScanningSetupGetFailsOnAHiddenRepositoryAndAnInvalidAnswer(t *testing.T) {
	for name, tt := range map[string]struct {
		answer answerOf
		want   string
	}{
		"a hidden repository": {answerOf{http.StatusNotFound, `{"message":"provider-text-canary"}`}, "Administration: read"},
		"another state":       {answerOf{http.StatusOK, `{"state":"provider-text-canary"}`}, "invalid response"},
		"another language":    {answerOf{http.StatusOK, `{"state":"configured","languages":["Go Lang"]}`}, "invalid response"},
		"an unreadable reply": {answerOf{http.StatusOK, `not json`}, "invalid response"},
	} {
		_, core, _ := serveSecurity(t, map[string]answerOf{setupRoute: tt.answer})
		_, err := invoke(t, core, codeScanningSetupGet.ID, "sec", `{}`, false)
		if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "canary") {
			t.Errorf("%s: err = %v, want %q and no provider text", name, err, tt.want)
		}
	}
}

func TestCodeScanningSetupGetKeepsLanguagesGitHubMayAddLater(t *testing.T) {
	_, core, _ := serveSecurity(t, map[string]answerOf{setupRoute: {http.StatusOK,
		`{"state":"configured","languages":["rust","go"]}`}})
	result, err := invoke(t, core, codeScanningSetupGet.ID, "sec", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeResult(t, result)["languages"]; !reflect.DeepEqual(got, []any{"rust", "go"}) {
		t.Errorf("languages = %v", got)
	}
}

func TestCodeScanningSetupUpdateSendsOnlyTheGivenValuesOnce(t *testing.T) {
	for name, tt := range map[string]struct {
		arguments string
		want      map[string]any
	}{
		"state": {`{"state":"configured"}`, map[string]any{"state": "configured"}},
		"off":   {`{"state":"not-configured"}`, map[string]any{"state": "not-configured"}},
		"all": {`{"state":"configured","runner_type":"labeled","runner_label":"scan-pool","query_suite":"extended",` +
			`"threat_model":"remote_and_local","languages":["go","actions"]}`,
			map[string]any{"state": "configured", "runner_type": "labeled", "runner_label": "scan-pool",
				"query_suite": "extended", "threat_model": "remote_and_local", "languages": []any{"go", "actions"}}},
		"standard runner": {`{"runner_type":"standard"}`, map[string]any{"runner_type": "standard"}},
		"suite":           {`{"query_suite":"default"}`, map[string]any{"query_suite": "default"}},
		"threat model":    {`{"threat_model":"remote"}`, map[string]any{"threat_model": "remote"}},
		"languages":       {`{"languages":["java-kotlin"]}`, map[string]any{"languages": []any{"java-kotlin"}}},
	} {
		t.Run(name, func(t *testing.T) {
			f, core, _ := serveSecurity(t, map[string]answerOf{setupChange: {http.StatusOK, `{}`}})
			result, err := invoke(t, core, codeScanningSetupUpdate.ID, "sec", tt.arguments, true)
			if err != nil {
				t.Fatal(err)
			}
			requests := f.recorded()
			if len(requests) != 1 || requests[0].method != http.MethodPatch ||
				requests[0].path != repositoryRoute+"/code-scanning/default-setup" ||
				!reflect.DeepEqual(requests[0].body, tt.want) {
				t.Fatalf("requests = %+v, want one PATCH with %v", requests, tt.want)
			}
			if got := decodeResult(t, result); !reflect.DeepEqual(got, map[string]any{"started": false}) {
				t.Errorf("result = %v, want started false", got)
			}
		})
	}
}

func TestCodeScanningSetupUpdateAcceptsAnEmptyAnswerAndNamesAStartedRun(t *testing.T) {
	_, core, _ := serveSecurity(t, map[string]answerOf{setupChange: {http.StatusOK, ""}})
	result, err := invoke(t, core, codeScanningSetupUpdate.ID, "sec", `{"state":"configured"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeResult(t, result); !reflect.DeepEqual(got, map[string]any{"started": false}) {
		t.Errorf("result = %v", got)
	}
	_, core, _ = serveSecurity(t, map[string]answerOf{setupChange: {http.StatusAccepted,
		`{"run_id":42,"run_url":"https://api.example.org/runs/42"}`}})
	result, err = invoke(t, core, codeScanningSetupUpdate.ID, "sec", `{"state":"configured"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"started": true, "run_id": float64(42), "run_url": "https://api.example.org/runs/42"}
	if got := decodeResult(t, result); !reflect.DeepEqual(got, want) {
		t.Errorf("result = %v, want %v", got, want)
	}
	_, core, _ = serveSecurity(t, map[string]answerOf{setupChange: {http.StatusAccepted, ""}})
	result, err = invoke(t, core, codeScanningSetupUpdate.ID, "sec", `{"state":"configured"}`, true)
	if err != nil || !reflect.DeepEqual(decodeResult(t, result), map[string]any{"started": true}) {
		t.Errorf("a 202 without a body = %s, %v", result, err)
	}
}

func TestCodeScanningSetupUpdateNamesRefusalsWithoutProviderText(t *testing.T) {
	for name, tt := range map[string]struct {
		status int
		want   string
	}{
		"a refusal":         {http.StatusForbidden, "Administration: write"},
		"a hidden route":    {http.StatusNotFound, "Administration: write"},
		"a running run":     {http.StatusConflict, "already in progress"},
		"a wrong state":     {http.StatusUnprocessableEntity, "not in the state it needs"},
		"service refusal":   {http.StatusServiceUnavailable, uncertain},
		"a server failure":  {http.StatusBadGateway, uncertain},
		"a gateway timeout": {http.StatusGatewayTimeout, uncertain},
	} {
		t.Run(name, func(t *testing.T) {
			f, core, _ := serveSecurity(t, map[string]answerOf{setupChange: {tt.status, `{"message":"provider-text-canary"}`}})
			_, err := invoke(t, core, codeScanningSetupUpdate.ID, "sec", `{"state":"configured"}`, true)
			if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "canary") {
				t.Errorf("err = %v, want %q and no provider text", err, tt.want)
			}
			if got := len(f.recorded()); got != 1 {
				t.Errorf("requests = %d, want exactly one", got)
			}
		})
	}
}

func TestCodeScanningSetupUpdateIsNeverRepeatedAfterAnUnreadableAnswer(t *testing.T) {
	f, core, _ := serveSecurity(t, map[string]answerOf{setupChange: {http.StatusAccepted, `not json`}})
	_, err := invoke(t, core, codeScanningSetupUpdate.ID, "sec", `{"state":"configured"}`, true)
	if err == nil || !strings.Contains(err.Error(), uncertain) {
		t.Errorf("err = %v, want the uncertainty", err)
	}
	if got := len(f.recorded()); got != 1 {
		t.Errorf("requests = %d, want exactly one", got)
	}
}

func TestCodeScanningSetupUpdateIsBoundedBeforeIO(t *testing.T) {
	f, core, reads := serveSecurity(t, nil)
	for name, arguments := range map[string]string{
		"empty":                   `{}`,
		"an unknown state":        `{"state":"enabled"}`,
		"an unknown runner":       `{"runner_type":"hosted"}`,
		"a label without labeled": `{"runner_label":"pool"}`,
		"a label on standard":     `{"runner_type":"standard","runner_label":"pool"}`,
		"labeled without a label": `{"runner_type":"labeled"}`,
		"an empty label":          `{"runner_type":"labeled","runner_label":""}`,
		"a label with a newline":  `{"runner_type":"labeled","runner_label":"a\nb"}`,
		"a long label":            `{"runner_type":"labeled","runner_label":"` + strings.Repeat("a", 257) + `"}`,
		"an unknown suite":        `{"query_suite":"security"}`,
		"an unknown threat model": `{"threat_model":"local"}`,
		"no languages":            `{"languages":[]}`,
		"an unknown language":     `{"languages":["rust"]}`,
		"the removed javascript":  `{"languages":["javascript"]}`,
		"a repeated language":     `{"languages":["go","go"]}`,
		"a schedule":              `{"schedule":"weekly"}`,
		"a wrong type":            `{"languages":"go"}`,
	} {
		*reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, codeScanningSetupUpdate.ID, "sec", arguments, true); err == nil {
			t.Errorf("%s was accepted", name)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s: secret reads = %d, requests = %d; want none", name, *reads, len(f.recorded())-before)
		}
	}
	// The handler applies the same checks when the schema is not in front of it.
	_, handler, _ := registry(t).Lookup(codeScanningSetupUpdate.ID)
	for _, arguments := range []string{`{}`, `{"runner_label":"pool"}`, `{"languages":["go","go"]}`, `{"state":"x"}`} {
		*reads = 0
		if _, err := handler(t.Context(), resolvedConnection("sec", "http://127.0.0.1:1", repoTarget), nil, nil,
			[]byte(arguments)); !isInvalidRequest(err) || *reads != 0 {
			t.Errorf("handler %s = %v", arguments, err)
		}
	}
}
