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

const testVariable = testWorkspace + "/variables/5"

func TestVariableToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{variablesListID, variablesGetID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	changes := map[string]capability.Effect{variablesCreateID: capability.EffectCreate,
		variablesUpdateID: capability.EffectUpdate, variablesDeleteID: capability.EffectDelete,
		variablesRevertID: capability.EffectUpdate}
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
		if d.RequiresToolAllowList != (id == variablesRevertID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
	}
	if toolOf(t, variablesCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("variables.create must be non-idempotent")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{variablesListID, variablesGetID} {
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	for _, id := range []string{variablesCreateID, variablesUpdateID, variablesDeleteID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if slices.Contains(profiles["read"], variablesRevertID) || slices.Contains(profiles["write"], variablesRevertID) {
		t.Error("variables.revert is in a profile")
	}
}

func TestVariableDescriptionsWarnAboutCodeAndConstants(t *testing.T) {
	for _, d := range variableDescriptors() {
		if d.ID == variablesDeleteID || d.ID == variablesRevertID {
			continue
		}
		for _, want := range []string{"jsm", "sensitive", "untrusted", "never run"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", d.ID, want)
			}
		}
	}
}

func TestConfirmationSummaryNamesVariableTypeAndWorkspace(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(variablesCreateID)(json.RawMessage(
		`{"workspace":"` + testWorkspace + `","name":"Example variable","type":"jsm","parameter":[{"type":"template","key":"javascript","value":"secret-body"}]}`))
	for _, want := range []string{"Example variable", "jsm", testWorkspace, "variable"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "secret-body") {
		t.Errorf("summary %q shows a parameter value", got)
	}
	for _, id := range []string{variablesDeleteID, variablesRevertID} {
		if got := reg.ConfirmationSummary(id)(json.RawMessage(`{"variable":"` + testVariable + `"}`)); !strings.Contains(got, testVariable) {
			t.Errorf("%s summary %q lacks the variable", id, got)
		}
	}
}

// allMembersVariable sets every member a caller can set, with a complete format value.
const allMembersVariable = `{"name":"Example variable","type":"v","notes":"n","parentFolderId":"7",` +
	`"scheduleStartMs":"1700000000000","scheduleEndMs":"1800000000000",` +
	`"disablingTriggerId":["11","12"],"enablingTriggerId":["13"],` +
	`"parameter":[{"type":"template","key":"name","value":"example.field"},` +
	`{"type":"list","key":"l","list":[{"type":"template","value":"a"}]}],` +
	`"formatValue":{"caseConversionType":"uppercase","convertToNumber":"comma","convertToBoolean":true,` +
	`"convertNullToValue":{"type":"template","value":"is-null"},` +
	`"convertUndefinedToValue":{"type":"template","value":"is-undefined"},` +
	`"convertTrueToValue":{"type":"template","value":"yes"},` +
	`"convertFalseToValue":{"type":"template","value":"no"}}}`

func TestVariablesCreateSendsEveryMemberAndFormatValue(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := `{"path":"` + testVariable + `","accountId":"1000001","containerId":"2000002","workspaceId":"3",` +
		`"variableId":"5","fingerprint":"1700000000001","tagManagerUrl":"https://tagmanager.google.com/x",` + allMembersVariable[1:]
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, stored, nil }
	args := `{"workspace":"` + testWorkspace + `",` + allMembersVariable[1:]
	if err := application.ValidateJSON(variablesCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid variable: %v", err)
	}
	out, err := h.call(t, handleVariablesCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/variables" ||
		sent[0].contentType != "application/json" || sent[0].query != "" {
		t.Fatalf("sent = %+v", sent)
	}
	var got, want map[string]any
	if err := json.Unmarshal([]byte(sent[0].body), &got); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(allMembersVariable), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %s\nwant %s", sent[0].body, allMembersVariable)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(variablesCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	var back map[string]any
	_ = json.Unmarshal([]byte(stored), &back)
	if !reflect.DeepEqual(out, back) {
		t.Errorf("result differs from the stored variable: %s", encoded)
	}
}

func TestVariablesCreateNeverSendsOutputOnlyMembers(t *testing.T) {
	h := newHarness(t, "", testClientID)
	if _, err := h.call(t, handleVariablesCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"c"}`); err != nil {
		t.Fatal(err)
	}
	if got := h.sent(t)[0].body; got != `{"name":"n","type":"c"}` {
		t.Errorf("body = %s", got)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleVariablesCreate, `{"workspace":"`+testWorkspace+`","type":"c"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleVariablesCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`)
}

func TestVariablesUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"variable":"` + testVariable + `","name":"n","type":"c"}`,
		`{"variable":"` + testVariable + `","fingerprint":"","name":"n","type":"c"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleVariablesUpdate, args)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleVariablesRevert, `{"variable":"`+testVariable+`"}`)
	if err := application.ValidateJSON(variablesUpdate.InputSchema,
		json.RawMessage(`{"variable":"`+testVariable+`","name":"n","type":"c"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
	if err := application.ValidateJSON(variablesRevert.InputSchema, json.RawMessage(`{"variable":"`+testVariable+`"}`)); err == nil {
		t.Error("the schema accepts a revert without fingerprint")
	}
}

func TestVariablesUpdatePutsTheWholeVariableWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testVariable + `","fingerprint":"1700000000002","name":"n","type":"c"}`, nil
	}
	out, err := h.call(t, handleVariablesUpdate, `{"variable":"`+testVariable+`","fingerprint":"1700000000001","name":"n","type":"c","notes":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testVariable ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].body != `{"name":"n","type":"c","notes":"x"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestStaleVariableFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleVariablesUpdate, `{"variable":"`+testVariable+`","fingerprint":"1","name":"n","type":"c"}`)
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

func TestVariablesDeleteAndRevert(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "DELETE" {
			return 204, "", nil
		}
		return 200, `{"variable":{"path":"` + testVariable + `","name":"base","type":"c","fingerprint":"9"}}`, nil
	}
	out, err := h.call(t, handleVariablesDelete, `{"variable":"`+testVariable+`"}`)
	if err != nil || out["deleted"] != true || out["variable"] != testVariable {
		t.Fatalf("delete = %v, %v", out, err)
	}
	out, err = h.call(t, handleVariablesRevert, `{"variable":"`+testVariable+`","fingerprint":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if base, _ := out["variable"].(map[string]any); base["name"] != "base" {
		t.Errorf("revert = %v", out)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if out, err = h.call(t, handleVariablesRevert, `{"variable":"`+testVariable+`","fingerprint":"5"}`); err != nil || len(out) != 0 {
		t.Errorf("revert of a variable the base version lacks = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 3 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testVariable || sent[0].body != "" ||
		sent[1].method != "POST" || sent[1].path != "/tagmanager/v2/"+testVariable+":revert" ||
		sent[1].query != "fingerprint=5" || sent[1].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestVariablesOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
	}{
		{"accounts/1000001/containers/2000002", []string{"accounts/1000001/containers/2000009/workspaces/3/variables/5",
			"accounts/1000009/containers/2000002/workspaces/3/variables/5"}},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/workspaces/3/variables/5"}},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			workspace, _, _ := strings.Cut(path, "/variables/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleVariablesGet, `{"variable":"`+path+`"}`)
			reachNothing(t, h, handleVariablesList, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleVariablesCreate, `{"workspace":"`+workspace+`","name":"n","type":"c"}`)
			reachNothing(t, h, handleVariablesUpdate, `{"variable":"`+path+`","fingerprint":"1","name":"n","type":"c"}`)
			reachNothing(t, h, handleVariablesDelete, `{"variable":"`+path+`"}`)
			reachNothing(t, h, handleVariablesRevert, `{"variable":"`+path+`","fingerprint":"1"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleVariablesDelete, `{"variable":"`+testVariable+`"}`); err != nil {
			t.Errorf("%s: a variable inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/workspaces/3/variables/05", "accounts/1/containers/2/workspaces/3",
		"accounts/1/containers/2/workspaces/3/variables/5/x", "accounts/1/containers/2/workspaces/3/variables/5?a=b",
		"accounts/1/containers/2/workspaces/3/triggers/5"} {
		reachNothing(t, newHarness(t, "", testClientID), handleVariablesGet, `{"variable":"`+bad+`"}`)
	}
}

func TestVariablesListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"variable":[{"variableId":"1","name":"a","type":"c"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"variable":[{"variableId":"2","name":"b","type":"v"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleVariablesList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["variables"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleVariablesList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["variables"].([]any)[0].(map[string]any)["variableId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleVariablesList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testWorkspace+"/variables" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
}

func TestCodeAndConstantsAreNeverCutOrChanged(t *testing.T) {
	code := strings.Repeat("function(){return \"é€ \\\\ {{x}} <script>\";}\n", 20000)
	constant := "synthetic-constant-value-ÄÖ  \"q\" \\ {{y}}"
	stored := map[string]any{"path": testVariable, "name": strings.Repeat("n", 5000), "notes": strings.Repeat("o", 70000),
		"type": "jsm", "fingerprint": "1",
		"parameter": []any{map[string]any{"type": "template", "key": "javascript", "value": code},
			map[string]any{"type": "template", "key": "value", "value": constant}}}
	raw, _ := json.Marshal(stored)
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	out, err := h.call(t, handleVariablesGet, `{"variable":"`+testVariable+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(out, want) {
		t.Fatal("the variable read differs from the stored variable")
	}
	// What a read returns goes back unchanged through an update.
	args := map[string]any{"variable": testVariable, "fingerprint": "1"}
	keys := []string{"name", "notes", "type", "parameter"}
	for _, k := range keys {
		args[k] = out[k]
	}
	encoded, _ := json.Marshal(args)
	if err := application.ValidateJSON(variablesUpdate.InputSchema, encoded); err != nil {
		t.Fatalf("the schema refuses what a read returned: %v", err)
	}
	if _, err := h.call(t, handleVariablesUpdate, string(encoded)); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(h.sent(t)[1].body), &sent)
	for _, k := range keys {
		if !reflect.DeepEqual(sent[k], want[k]) {
			t.Errorf("%s was changed on the way", k)
		}
	}
}

func TestOversizedVariableAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	big := `{"name":"n","parameter":[{"type":"template","value":"` + strings.Repeat("a", maxResponse) + `"}]}`
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, big, nil }
	if _, err := h.call(t, handleVariablesGet, `{"variable":"`+testVariable+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
	many := strings.Repeat(`{"type":"template"},`, maxParamNodes) + `{"type":"template"}`
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"formatValue":{"convertNullToValue":{"type":"list","list":[` + many + `]}}}`, nil
	}
	if _, err := h.call(t, handleVariablesGet, `{"variable":"`+testVariable+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response for too many entries", err)
	}
}

func TestVariableInputBoundsAreRefusedBeforeAnyRequest(t *testing.T) {
	nest := `{"type":"template","value":"x"}`
	for i := 0; i < maxParamDepth; i++ {
		nest = `{"type":"list","list":[` + nest + `]}`
	}
	args := `{"workspace":"` + testWorkspace + `","name":"n","type":"v","parameter":[` + nest + `]}`
	reachNothing(t, newHarness(t, "", testClientID), handleVariablesCreate, args)
	prefix := `{"workspace":"` + testWorkspace + `","name":"n","type":"v",`
	for _, bad := range []string{
		prefix + `"formatValue":{"caseConversionType":"title"}}`,
		prefix + `"formatValue":{"convertToNumber":"dot"}}`,
		prefix + `"formatValue":{"convertToBoolean":"yes"}}`,
		prefix + `"formatValue":{"convertNullToValue":{"type":"number"}}}`,
		prefix + `"formatValue":{"unknown":1}}`,
		prefix + `"scheduleStartMs":"soon"}`,
		prefix + `"enablingTriggerId":"1"}`,
		`{"workspace":"` + testWorkspace + `","name":"","type":"v"}`,
		prefix + `"unknown":1}`,
	} {
		if err := application.ValidateJSON(variablesCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
}

func TestAVariableChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleVariablesCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"c"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeAVariable(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleVariablesDelete, `{"variable":"`+testVariable+`"}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing edit scope and no request", err, len(h.google.seen()))
	}
	if _, err := h.call(t, handleVariablesGet, `{"variable":"`+testVariable+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
}
