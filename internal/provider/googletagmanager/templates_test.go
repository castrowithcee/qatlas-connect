package googletagmanager

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const testTemplate = testWorkspace + "/templates/5"

func TestTemplateToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{templatesListID, templatesGetID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	changes := map[string]capability.Effect{templatesCreateID: capability.EffectCreate,
		templatesUpdateID: capability.EffectUpdate, templatesDeleteID: capability.EffectDelete,
		templatesRevertID: capability.EffectUpdate}
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
		if d.RequiresToolAllowList != (id == templatesRevertID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
	}
	if toolOf(t, templatesCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("templates.create must be non-idempotent")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{templatesListID, templatesGetID} {
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	for _, id := range []string{templatesCreateID, templatesUpdateID, templatesDeleteID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if slices.Contains(profiles["read"], templatesRevertID) || slices.Contains(profiles["write"], templatesRevertID) {
		t.Error("templates.revert is in a profile")
	}
}

func TestTemplateDescriptionsWarnAboutCode(t *testing.T) {
	for _, d := range templateDescriptors() {
		if d.ID == templatesDeleteID || d.ID == templatesRevertID {
			continue
		}
		for _, want := range []string{"code", "sensitive", "untrusted", "never run"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", d.ID, want)
			}
		}
	}
	for _, id := range []string{templatesCreateID, templatesUpdateID, templatesDeleteID, templatesRevertID} {
		d := toolOf(t, id)
		if !strings.Contains(d.Description, "runs on the pages") || !strings.Contains(d.Description, "permissions") {
			t.Errorf("%s description does not say that a published template runs code on the pages", id)
		}
	}
}

func TestConfirmationSummaryNamesTemplateAndWorkspaceNeverTheCode(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(templatesCreateID)(json.RawMessage(
		`{"workspace":"` + testWorkspace + `","name":"Example template","templateData":"synthetic-code-body"}`))
	for _, want := range []string{"Example template", testWorkspace, "template"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	got = reg.ConfirmationSummary(templatesUpdateID)(json.RawMessage(
		`{"template":"` + testTemplate + `","fingerprint":"1","name":"Example template","templateData":"synthetic-code-body"}`))
	if !strings.Contains(got, testTemplate) || strings.Contains(got, "synthetic-code-body") {
		t.Errorf("update summary %q", got)
	}
	for _, id := range []string{templatesDeleteID, templatesRevertID} {
		if got := reg.ConfirmationSummary(id)(json.RawMessage(`{"template":"` + testTemplate + `"}`)); !strings.Contains(got, testTemplate) {
			t.Errorf("%s summary %q lacks the template", id, got)
		}
	}
}

const templateText = "___INFO___\n{\"type\":\"TAG\",\"displayName\":\"Example\"}\n___SANDBOXED_JS_FOR_WEB_TEMPLATE___\n" +
	"const log = require('logToConsole');\nlog('é€ \\\\ {{x}} <script>');\ndata.gtmOnSuccess();\n"

func TestTemplatesCreateSendsNameAndDataAndReturnsEveryMember(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := map[string]any{"path": testTemplate, "accountId": "1000001", "containerId": "2000002",
		"workspaceId": "3", "templateId": "5", "fingerprint": "1700000000001",
		"tagManagerUrl": "https://tagmanager.google.com/x", "name": "Example template", "templateData": templateText,
		"galleryReference": map[string]any{"host": "github.com", "owner": "example-owner", "repository": "example-repo",
			"version": "abc", "isModified": true, "signature": "sig", "templateDeveloperId": "dev",
			"galleryTemplateId": "gal"}}
	raw, _ := json.Marshal(stored)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	argsMap := map[string]any{"workspace": testWorkspace, "name": "Example template", "templateData": templateText}
	args, _ := json.Marshal(argsMap)
	if err := application.ValidateJSON(templatesCreate.InputSchema, args); err != nil {
		t.Fatalf("schema refuses a valid template: %v", err)
	}
	out, err := h.call(t, handleTemplatesCreate, string(args))
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/templates" ||
		sent[0].contentType != "application/json" || sent[0].query != "" {
		t.Fatalf("sent = %+v", sent)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(sent[0].body), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]any{"name": "Example template", "templateData": templateText}) {
		t.Errorf("body = %s", sent[0].body)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(templatesCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	if !reflect.DeepEqual(out, stored) {
		t.Errorf("result differs from the stored template: %s", encoded)
	}
}

func TestTemplateInputRefusesAGalleryReferenceAndUnknownMembers(t *testing.T) {
	for _, bad := range []string{
		`{"workspace":"` + testWorkspace + `","name":"n","templateData":"d","galleryReference":{"host":"x"}}`,
		`{"workspace":"` + testWorkspace + `","name":"n","templateData":"d","fingerprint":"1"}`,
		`{"workspace":"` + testWorkspace + `","name":"","templateData":"d"}`,
		`{"workspace":"` + testWorkspace + `","name":"n","templateData":""}`,
		`{"workspace":"` + testWorkspace + `","name":"n"}`,
		`{"workspace":"` + testWorkspace + `","templateData":"d"}`,
	} {
		if err := application.ValidateJSON(templatesCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
	reachNothing(t, newHarness(t, "", testClientID), handleTemplatesCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleTemplatesCreate, `{"workspace":"`+testWorkspace+`","templateData":"d"}`)
}

func TestLongTemplateDataIsBoundedOnInputBeforeAnyRequest(t *testing.T) {
	atLimit := strings.Repeat("a", maxTemplateDataBytes)
	args := `{"workspace":"` + testWorkspace + `","name":"n","templateData":"` + atLimit + `"}`
	if err := application.ValidateJSON(templatesCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("the schema refuses templateData at the limit: %v", err)
	}
	h := newHarness(t, "", testClientID)
	if _, err := h.call(t, handleTemplatesCreate, args); err != nil {
		t.Fatalf("templateData at the limit was refused: %v", err)
	}
	tooLong := `{"workspace":"` + testWorkspace + `","name":"n","templateData":"` + atLimit + `a"}`
	if err := application.ValidateJSON(templatesCreate.InputSchema, json.RawMessage(tooLong)); err == nil {
		t.Error("the schema accepts templateData over the limit")
	}
	reachNothing(t, newHarness(t, "", testClientID), handleTemplatesCreate, tooLong)
	reachNothing(t, newHarness(t, "", testClientID), handleTemplatesUpdate,
		`{"template":"`+testTemplate+`","fingerprint":"1","name":"n","templateData":"`+atLimit+`a"}`)
	// Multibyte text counts in bytes in the handler even where the schema counts characters.
	multibyte := strings.Repeat("é", maxTemplateDataBytes/2+1)
	reachNothing(t, newHarness(t, "", testClientID), handleTemplatesCreate,
		`{"workspace":"`+testWorkspace+`","name":"n","templateData":"`+multibyte+`"}`)
}

func TestTemplatesUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"template":"` + testTemplate + `","name":"n","templateData":"d"}`,
		`{"template":"` + testTemplate + `","fingerprint":"","name":"n","templateData":"d"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleTemplatesUpdate, args)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleTemplatesRevert, `{"template":"`+testTemplate+`"}`)
	if err := application.ValidateJSON(templatesUpdate.InputSchema,
		json.RawMessage(`{"template":"`+testTemplate+`","name":"n","templateData":"d"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
	if err := application.ValidateJSON(templatesRevert.InputSchema, json.RawMessage(`{"template":"`+testTemplate+`"}`)); err == nil {
		t.Error("the schema accepts a revert without fingerprint")
	}
}

func TestTemplatesUpdatePutsTheWholeTemplateWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testTemplate + `","fingerprint":"1700000000002","name":"n","templateData":"d"}`, nil
	}
	out, err := h.call(t, handleTemplatesUpdate, `{"template":"`+testTemplate+`","fingerprint":"1700000000001","name":"n","templateData":"d"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testTemplate ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].body != `{"name":"n","templateData":"d"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestStaleTemplateFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleTemplatesUpdate, `{"template":"`+testTemplate+`","fingerprint":"1","name":"n","templateData":"d"}`)
		if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
			t.Fatalf("status %d: err = %v", code, err)
		}
		if strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("status %d is a definite refusal: %v", code, err)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("status %d: requests = %d, want 1", code, got)
		}
	}
}

func TestTemplatesDeleteAndRevert(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "DELETE" {
			return 204, "", nil
		}
		return 200, `{"template":{"path":"` + testTemplate + `","name":"base","templateData":"d","fingerprint":"9"}}`, nil
	}
	out, err := h.call(t, handleTemplatesDelete, `{"template":"`+testTemplate+`"}`)
	if err != nil || out["deleted"] != true || out["template"] != testTemplate {
		t.Fatalf("delete = %v, %v", out, err)
	}
	out, err = h.call(t, handleTemplatesRevert, `{"template":"`+testTemplate+`","fingerprint":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if base, _ := out["template"].(map[string]any); base["name"] != "base" {
		t.Errorf("revert = %v", out)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if out, err = h.call(t, handleTemplatesRevert, `{"template":"`+testTemplate+`","fingerprint":"5"}`); err != nil || len(out) != 0 {
		t.Errorf("revert of a template the base version lacks = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 3 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testTemplate || sent[0].body != "" ||
		sent[1].method != "POST" || sent[1].path != "/tagmanager/v2/"+testTemplate+":revert" ||
		sent[1].query != "fingerprint=5" || sent[1].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestTemplatesOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
	}{
		{"accounts/1000001/containers/2000002", []string{"accounts/1000001/containers/2000009/workspaces/3/templates/5",
			"accounts/1000009/containers/2000002/workspaces/3/templates/5"}},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/workspaces/3/templates/5"}},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			workspace, _, _ := strings.Cut(path, "/templates/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleTemplatesGet, `{"template":"`+path+`"}`)
			reachNothing(t, h, handleTemplatesList, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleTemplatesCreate, `{"workspace":"`+workspace+`","name":"n","templateData":"d"}`)
			reachNothing(t, h, handleTemplatesUpdate, `{"template":"`+path+`","fingerprint":"1","name":"n","templateData":"d"}`)
			reachNothing(t, h, handleTemplatesDelete, `{"template":"`+path+`"}`)
			reachNothing(t, h, handleTemplatesRevert, `{"template":"`+path+`","fingerprint":"1"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleTemplatesDelete, `{"template":"`+testTemplate+`"}`); err != nil {
			t.Errorf("%s: a template inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/workspaces/3/templates/05", "accounts/1/containers/2/workspaces/3",
		"accounts/1/containers/2/workspaces/3/templates/5/x", "accounts/1/containers/2/workspaces/3/templates/5?a=b",
		"accounts/1/containers/2/workspaces/3/variables/5"} {
		reachNothing(t, newHarness(t, "", testClientID), handleTemplatesGet, `{"template":"`+bad+`"}`)
	}
}

func TestTemplatesListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"template":[{"templateId":"1","name":"a"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"template":[{"templateId":"2","name":"b"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleTemplatesList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["templates"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleTemplatesList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["templates"].([]any)[0].(map[string]any)["templateId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleTemplatesList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testWorkspace+"/templates" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
}

func TestEmptyTemplateListIsAnEmptyResult(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	out, err := h.call(t, handleTemplatesList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if list, ok := out["templates"].([]any); !ok || len(list) != 0 {
		t.Errorf("templates = %v", out["templates"])
	}
}

func TestTemplateCodeIsNeverCutOrChangedAndGoesBackThroughUpdate(t *testing.T) {
	code := strings.Repeat(templateText, 5000)
	stored := map[string]any{"path": testTemplate, "name": strings.Repeat("n", 5000), "templateData": code,
		"fingerprint": "1"}
	raw, _ := json.Marshal(stored)
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	out, err := h.call(t, handleTemplatesGet, `{"template":"`+testTemplate+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(out, want) {
		t.Fatal("the template read differs from the stored template")
	}
	args := map[string]any{"template": testTemplate, "fingerprint": "1", "name": out["name"], "templateData": out["templateData"]}
	encoded, _ := json.Marshal(args)
	if err := application.ValidateJSON(templatesUpdate.InputSchema, encoded); err != nil {
		t.Fatalf("the schema refuses what a read returned: %v", err)
	}
	if _, err := h.call(t, handleTemplatesUpdate, string(encoded)); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(h.sent(t)[1].body), &sent)
	for _, k := range []string{"name", "templateData"} {
		if !reflect.DeepEqual(sent[k], want[k]) {
			t.Errorf("%s was changed on the way", k)
		}
	}
}

func TestOversizedTemplateAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	big := `{"name":"n","templateData":"` + strings.Repeat("a", maxResponse) + `"}`
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, big, nil }
	if _, err := h.call(t, handleTemplatesGet, `{"template":"`+testTemplate+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
}

func TestATemplateChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleTemplatesCreate, `{"workspace":"`+testWorkspace+`","name":"n","templateData":"d"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeATemplate(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleTemplatesDelete, `{"template":"`+testTemplate+`"}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing edit scope and no request", err, len(h.google.seen()))
	}
	if _, err := h.call(t, handleTemplatesGet, `{"template":"`+testTemplate+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
}
