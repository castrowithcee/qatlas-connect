package googletagmanager

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	testEnvironment = testContainer + "/environments/4"
	// testPreviewKey stands for the key of a preview environment; it is synthetic.
	testPreviewKey = "synthetic-preview-key-one"
)

func storedEnvironment() string {
	return `{"path":"` + testEnvironment + `","accountId":"1000001","containerId":"2000002","environmentId":"4",` +
		`"type":"user","name":"Example staging","description":"Example text","url":"https://staging.example.com/",` +
		`"enableDebug":true,"containerVersionId":"9","authorizationCode":"` + testPreviewKey + `",` +
		`"authorizationTimestamp":"2026-11-01T10:00:00Z","fingerprint":"1700000000001",` +
		`"tagManagerUrl":"https://tagmanager.google.com/x"}`
}

func TestEnvironmentToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{environmentsListID, environmentsGetID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	changes := map[string]capability.Effect{environmentsCreateID: capability.EffectCreate,
		environmentsUpdateID: capability.EffectUpdate, environmentsDeleteID: capability.EffectDelete}
	for id, effect := range changes {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{editScope}) {
			t.Errorf("%s scopes = %v, want only %s", id, got, editScope)
		}
		if d.Risk.Effect != effect || d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
		if d.RequiresToolAllowList || slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if toolOf(t, environmentsCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("environments.create must be non-idempotent")
	}
}

func TestEnvironmentToolsAreExactlyFiveAndHaveNoReauthorize(t *testing.T) {
	var got []string
	for _, d := range descriptors() {
		if strings.Contains(d.ID, ".environments.") {
			got = append(got, d.ID)
		}
	}
	slices.Sort(got)
	want := []string{Provider + ".environments.create", Provider + ".environments.delete",
		Provider + ".environments.get", Provider + ".environments.list", Provider + ".environments.update"}
	if !slices.Equal(got, want) {
		t.Errorf("environment tools = %v, want %v", got, want)
	}
	for _, d := range descriptors() {
		if strings.Contains(d.ID, "reauthorize") {
			t.Errorf("tool %s exists", d.ID)
		}
	}
}

func TestEnvironmentConfirmationShowsTheURL(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(environmentsCreateID)(json.RawMessage(`{"container":"` + testContainer +
		`","name":"Example staging","url":"https://staging.example.com/","enableDebug":true,` +
		`"description":"private-note-text"}`))
	for _, want := range []string{"Example staging", testContainer, "https://staging.example.com/", "debug on"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "private-note-text") {
		t.Errorf("summary %q shows the description", got)
	}
	got = reg.ConfirmationSummary(environmentsUpdateID)(json.RawMessage(
		`{"environment":"` + testEnvironment + `","fingerprint":"1","name":"n","url":"https://other.example.com/"}`))
	if !strings.Contains(got, testEnvironment) || !strings.Contains(got, "https://other.example.com/") {
		t.Errorf("update summary %q", got)
	}
	if got := reg.ConfirmationSummary(environmentsDeleteID)(json.RawMessage(`{"environment":"` + testEnvironment +
		`"}`)); !strings.Contains(got, testEnvironment) {
		t.Errorf("delete summary %q lacks the environment", got)
	}
}

func TestEnvironmentsCreateSendsACustomEnvironment(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, storedEnvironment())
	args := `{"container":"` + testContainer + `","name":"Example staging","description":"Example text",` +
		`"url":"https://staging.example.com/","enableDebug":true,"containerVersionId":"9"}`
	if err := application.ValidateJSON(environmentsCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid environment: %v", err)
	}
	out, err := h.call(t, handleEnvironmentsCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	want := `{"name":"Example staging","type":"user","description":"Example text",` +
		`"url":"https://staging.example.com/","enableDebug":true,"containerVersionId":"9"}`
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testContainer+"/environments" ||
		sent[0].contentType != "application/json" || sent[0].query != "" || sent[0].body != want {
		t.Fatalf("sent = %+v", sent)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(environmentsCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	for _, key := range []string{"path", "accountId", "containerId", "environmentId", "type", "name", "description",
		"url", "enableDebug", "containerVersionId", "authorizationTimestamp", "fingerprint", "tagManagerUrl"} {
		if _, ok := out[key]; !ok {
			t.Errorf("result lacks %s", key)
		}
	}
	reachNothing(t, newHarness(t, "", testClientID), handleEnvironmentsCreate, `{"container":"`+testContainer+`"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleEnvironmentsCreate,
		`{"container":"`+testContainer+`","name":" "}`)
	reachNothing(t, newHarness(t, "", testClientID), handleEnvironmentsCreate,
		`{"container":"`+testContainer+`","name":"n","type":"live"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleEnvironmentsCreate,
		`{"container":"`+testContainer+`","name":"n","url":"javascript:alert(1)"}`)
}

func TestEnvironmentInputIsClosedAndTypeIsUserOnly(t *testing.T) {
	for _, bad := range []string{
		`{"container":"` + testContainer + `","name":""}`,
		`{"container":"` + testContainer + `","name":"n","unknown":1}`,
		`{"container":"` + testContainer + `","name":"n","authorizationCode":"x"}`,
		`{"container":"` + testContainer + `","name":"n","type":"live"}`,
		`{"container":"` + testContainer + `","name":"n","url":"ftp://example.com/"}`,
		`{"container":"` + testContainer + `","name":"n","containerVersionId":"v1"}`,
	} {
		if err := application.ValidateJSON(environmentsCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
}

func TestAuthorizationCodeNeverReachesAResultAndGoesToTheRedactor(t *testing.T) {
	list := `{"environment":[` + storedEnvironment() + `]}`
	cases := []struct {
		name    string
		handler capability.Handler
		args    string
		body    string
	}{
		{"get", handleEnvironmentsGet, `{"environment":"` + testEnvironment + `"}`, storedEnvironment()},
		{"list", handleEnvironmentsList, `{"container":"` + testContainer + `"}`, list},
		{"create", handleEnvironmentsCreate, `{"container":"` + testContainer + `","name":"n"}`, storedEnvironment()},
		{"update", handleEnvironmentsUpdate, `{"environment":"` + testEnvironment + `","fingerprint":"1","name":"n"}`,
			storedEnvironment()},
	}
	for _, c := range cases {
		h := newHarness(t, "", testClientID)
		h.google.api = status(200, c.body)
		out, err := h.call(t, c.handler, c.args)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		encoded, _ := json.Marshal(out)
		if strings.Contains(string(encoded), testPreviewKey) || strings.Contains(string(encoded), "authorizationCode") {
			t.Errorf("%s: result shows the authorization code: %s", c.name, encoded)
		}
		if got := h.red.Apply("echo " + testPreviewKey); strings.Contains(got, testPreviewKey) {
			t.Errorf("%s: the redactor does not know the code: %s", c.name, got)
		}
	}
	for _, d := range environmentDescriptors() {
		for _, f := range d.Fields {
			if f.Name == "authorizationCode" {
				t.Errorf("%s declares authorizationCode", d.ID)
			}
		}
		if strings.Contains(string(d.OutputSchema), "authorizationCode") {
			t.Errorf("%s output schema has authorizationCode", d.ID)
		}
	}
}

func TestEnvironmentsUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"environment":"` + testEnvironment + `","name":"n"}`,
		`{"environment":"` + testEnvironment + `","fingerprint":"","name":"n"}`,
		`{"environment":"` + testEnvironment + `","fingerprint":"1"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleEnvironmentsUpdate, args)
	}
	if err := application.ValidateJSON(environmentsUpdate.InputSchema,
		json.RawMessage(`{"environment":"`+testEnvironment+`","name":"n"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
}

func TestEnvironmentsUpdatePutsTheWholeEnvironmentWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{"path":"`+testEnvironment+`","fingerprint":"1700000000002","name":"n"}`)
	out, err := h.call(t, handleEnvironmentsUpdate, `{"environment":"`+testEnvironment+
		`","fingerprint":"1700000000001","name":"n","url":"https://staging.example.com/"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testEnvironment ||
		sent[0].query != "fingerprint=1700000000001" ||
		sent[0].body != `{"name":"n","type":"user","url":"https://staging.example.com/"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestStaleEnvironmentFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleEnvironmentsUpdate, `{"environment":"`+testEnvironment+`","fingerprint":"1","name":"n"}`)
		if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
			t.Fatalf("status %d: err = %v", code, err)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("status %d: requests = %d, want 1", code, got)
		}
	}
}

func TestEnvironmentsDeleteSendsOneDeleteAndAnswersPath(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(204, ``)
	out, err := h.call(t, handleEnvironmentsDelete, `{"environment":"`+testEnvironment+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testEnvironment ||
		sent[0].query != "" || sent[0].body != "" {
		t.Fatalf("sent = %+v", sent)
	}
	if out["environment"] != testEnvironment || out["deleted"] != true {
		t.Errorf("result = %v", out)
	}
}

func TestDeletingASystemEnvironmentIsAnErrorWithoutLeakingGoogleText(t *testing.T) {
	for _, tc := range []struct {
		code  int
		class provider.Class
	}{{400, provider.ClassInvalidResponse}, {403, provider.ClassPermission}} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return tc.code, googleBody(tc.code, "FAILED_PRECONDITION", "SYSTEM"), nil
		}
		_, err := h.call(t, handleEnvironmentsDelete, `{"environment":"`+testContainer+`/environments/1"}`)
		if classOf(err) != tc.class || strings.Contains(err.Error(), testEcho) {
			t.Errorf("status %d: err = %v, want class %s", tc.code, err, tc.class)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("status %d: requests = %d, want 1", tc.code, got)
		}
	}
}

func TestEnvironmentTextsAreNeverCutAndGoBackThroughUpdate(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := map[string]any{"path": testEnvironment, "environmentId": "4", "fingerprint": "5",
		"name": strings.Repeat("n", 500) + "é", "description": strings.Repeat("o", 70000) + "\n\"q\"",
		"url": "https://example.com/" + strings.Repeat("p", 3000), "unknown": "dropped"}
	raw, _ := json.Marshal(stored)
	h.google.api = status(200, string(raw))
	out, err := h.call(t, handleEnvironmentsGet, `{"environment":"`+testEnvironment+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != stored["name"] || out["description"] != stored["description"] {
		t.Fatal("an environment text was cut or changed")
	}
	if out["url"] != stored["url"] {
		t.Fatal("the url was cut or changed")
	}
	if _, ok := out["unknown"]; ok {
		t.Error("a member outside the schema was passed on")
	}
}

func TestOversizedEnvironmentAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{"name":"n","description":"`+strings.Repeat("a", maxResponse)+`"}`)
	if _, err := h.call(t, handleEnvironmentsGet, `{"environment":"`+testEnvironment+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
}

func TestEnvironmentsListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"environment":[{"environmentId":"1","name":"a"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"environment":[{"environmentId":"2","name":"b"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleEnvironmentsList, `{"container":"`+testContainer+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["environments"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleEnvironmentsList, `{"container":"`+testContainer+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["environments"].([]any)[0].(map[string]any)["environmentId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	before := len(h.google.seen())
	other := `{"container":"accounts/1000001/containers/2000003","cursor":"` + cursor + `"}`
	if _, err := h.call(t, handleEnvironmentsList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another container was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testContainer+"/environments" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
	out, err := newHarness(t, "", testClientID).call(t, handleEnvironmentsList, `{"container":"`+testContainer+`"}`)
	if err != nil || len(out["environments"].([]any)) != 0 {
		t.Errorf("an empty answer = %v, %v", out, err)
	}
}

func TestEnvironmentsOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
	}{
		{testContainer, []string{"accounts/1000001/containers/2000009/environments/4",
			"accounts/1000009/containers/2000002/environments/4"}},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/environments/4"}},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			container, _, _ := strings.Cut(path, "/environments/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleEnvironmentsGet, `{"environment":"`+path+`"}`)
			reachNothing(t, h, handleEnvironmentsList, `{"container":"`+container+`"}`)
			reachNothing(t, h, handleEnvironmentsCreate, `{"container":"`+container+`","name":"n"}`)
			reachNothing(t, h, handleEnvironmentsUpdate, `{"environment":"`+path+`","fingerprint":"1","name":"n"}`)
			reachNothing(t, h, handleEnvironmentsDelete, `{"environment":"`+path+`"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleEnvironmentsGet, `{"environment":"`+testEnvironment+`"}`); err != nil {
			t.Errorf("%s: an environment inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/environments/04", "accounts/1/containers/2",
		"accounts/1/containers/2/environments/4/x", "accounts/1/containers/2/environments/4?a=b",
		"accounts/1/containers/2/workspaces/3/environments/4", "accounts/1/containers/2/versions/4"} {
		reachNothing(t, newHarness(t, "", testClientID), handleEnvironmentsGet, `{"environment":"`+bad+`"}`)
		reachNothing(t, newHarness(t, "", testClientID), handleEnvironmentsDelete, `{"environment":"`+bad+`"}`)
	}
}

func TestAnEnvironmentChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleEnvironmentsCreate, `{"container":"`+testContainer+`","name":"n"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeAnEnvironment(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	for _, call := range []struct {
		handler capability.Handler
		args    string
	}{
		{handleEnvironmentsCreate, `{"container":"` + testContainer + `","name":"n"}`},
		{handleEnvironmentsDelete, `{"environment":"` + testEnvironment + `"}`},
	} {
		_, err := h.call(t, call.handler, call.args)
		if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
			t.Errorf("err = %v, requests = %d, want the missing edit scope and no request", err, len(h.google.seen()))
		}
	}
	if _, err := h.call(t, handleEnvironmentsGet, `{"environment":"`+testEnvironment+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
}
