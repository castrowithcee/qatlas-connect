package googletagmanager

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const testTransformationPath = testWorkspace + "/transformations/5"

func TestTransformationToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{transformationsListID, transformationsGetID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	changes := map[string]capability.Effect{transformationsCreateID: capability.EffectCreate,
		transformationsUpdateID: capability.EffectUpdate, transformationsDeleteID: capability.EffectDelete,
		transformationsRevertID: capability.EffectUpdate}
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
		if d.RequiresToolAllowList != (id == transformationsRevertID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
	}
	if toolOf(t, transformationsCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("transformations.create must be non-idempotent")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{transformationsListID, transformationsGetID} {
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	for _, id := range []string{transformationsCreateID, transformationsUpdateID, transformationsDeleteID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if slices.Contains(profiles["read"], transformationsRevertID) || slices.Contains(profiles["write"], transformationsRevertID) {
		t.Error("transformations.revert is in a profile")
	}
}

func TestTransformationDescriptionsNameTheEffect(t *testing.T) {
	for _, id := range []string{transformationsCreateID, transformationsUpdateID, transformationsDeleteID, transformationsRevertID} {
		d := toolOf(t, id)
		for _, want := range []string{"server containers", "event data", "published container version", "versions.publish"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", id, want)
			}
		}
	}
}

func TestConfirmationSummaryNamesTransformationTypeAndWorkspace(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(transformationsCreateID)(json.RawMessage(
		`{"workspace":"` + testWorkspace + `","name":"Example transformation","type":"example-type",` +
			`"parameter":[{"type":"template","key":"k","value":"synthetic-parameter-value"}]}`))
	for _, want := range []string{"Example transformation", "example-type", testWorkspace, "transformation"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "synthetic-parameter-value") {
		t.Errorf("summary %q shows a parameter value", got)
	}
	for _, id := range []string{transformationsDeleteID, transformationsRevertID} {
		if got := reg.ConfirmationSummary(id)(json.RawMessage(`{"transformation":"` + testTransformationPath + `"}`)); !strings.Contains(got, testTransformationPath) {
			t.Errorf("%s summary %q lacks the transformation", id, got)
		}
	}
}

// allMembersTransformation sets every member a caller can set.
const allMembersTransformation = `{"name":"Example transformation","type":"example-type","notes":"n","parentFolderId":"7",` +
	`"parameter":[{"type":"boolean","key":"example-key","value":"true"},` +
	`{"type":"list","key":"l","list":[{"type":"template","value":"a"}]}]}`

func TestTransformationsCreateSendsEveryMember(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := `{"path":"` + testTransformationPath + `","accountId":"1000001","containerId":"2000002","workspaceId":"3",` +
		`"transformationId":"5","fingerprint":"1700000000001","tagManagerUrl":"https://tagmanager.google.com/x",` + allMembersTransformation[1:]
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, stored, nil }
	args := `{"workspace":"` + testWorkspace + `",` + allMembersTransformation[1:]
	if err := application.ValidateJSON(transformationsCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid transformation: %v", err)
	}
	out, err := h.call(t, handleTransformationsCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/transformations" ||
		sent[0].contentType != "application/json" || sent[0].query != "" {
		t.Fatalf("sent = %+v", sent)
	}
	var got, want map[string]any
	if err := json.Unmarshal([]byte(sent[0].body), &got); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(allMembersTransformation), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %s\nwant %s", sent[0].body, allMembersTransformation)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(transformationsCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	var back map[string]any
	_ = json.Unmarshal([]byte(stored), &back)
	if !reflect.DeepEqual(out, back) {
		t.Errorf("result differs from the stored transformation: %s", encoded)
	}
}

func TestTransformationsCreateNeverSendsOutputOnlyMembersAndRequiresNameAndType(t *testing.T) {
	h := newHarness(t, "", testClientID)
	if _, err := h.call(t, handleTransformationsCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"t"}`); err != nil {
		t.Fatal(err)
	}
	if sent := h.sent(t); sent[0].body != `{"name":"n","type":"t"}` {
		t.Errorf("body = %s", sent[0].body)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleTransformationsCreate, `{"workspace":"`+testWorkspace+`","type":"t"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleTransformationsCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`)
	// A transformation has no priority, so the schema refuses one.
	args := `{"workspace":"` + testWorkspace + `","name":"n","type":"t","priority":1}`
	if err := application.ValidateJSON(transformationsCreate.InputSchema, json.RawMessage(args)); err == nil {
		t.Error("the schema accepts a priority")
	}
}

func TestTransformationsUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"transformation":"` + testTransformationPath + `","name":"n","type":"t"}`,
		`{"transformation":"` + testTransformationPath + `","fingerprint":"","name":"n","type":"t"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleTransformationsUpdate, args)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleTransformationsRevert, `{"transformation":"`+testTransformationPath+`"}`)
	if err := application.ValidateJSON(transformationsUpdate.InputSchema,
		json.RawMessage(`{"transformation":"`+testTransformationPath+`","name":"n","type":"t"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
	if err := application.ValidateJSON(transformationsRevert.InputSchema, json.RawMessage(`{"transformation":"`+testTransformationPath+`"}`)); err == nil {
		t.Error("the schema accepts a revert without fingerprint")
	}
}

func TestTransformationsUpdatePutsTheWholeTransformationWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testTransformationPath + `","fingerprint":"1700000000002","name":"n","type":"t"}`, nil
	}
	out, err := h.call(t, handleTransformationsUpdate, `{"transformation":"`+testTransformationPath+`","fingerprint":"1700000000001","name":"n","type":"t","notes":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testTransformationPath ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].body != `{"name":"n","type":"t","notes":"x"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestStaleTransformationFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleTransformationsUpdate, `{"transformation":"`+testTransformationPath+`","fingerprint":"1","name":"n","type":"t"}`)
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

// The answer of Google for a transformation call on a web container is not verified; it arrives in the established
// form of an unmapped status.
func TestAWebContainerErrorOfATransformationArrivesAsAnUnmappedAnswer(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 400, googleBody(400, "INVALID_ARGUMENT", "WEB_CONTAINER"), nil
	}
	for name, call := range map[string]func() error{
		"list": func() error {
			_, err := h.call(t, handleTransformationsList, `{"workspace":"`+testWorkspace+`"}`)
			return err
		},
		"create": func() error {
			_, err := h.call(t, handleTransformationsCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"t"}`)
			return err
		},
	} {
		err := call()
		if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "HTTP 400") {
			t.Fatalf("%s: err = %v", name, err)
		}
		var perr *provider.Error
		if !errors.As(err, &perr) || perr.Original == nil {
			t.Errorf("%s: the original of Google's answer is missing: %v", name, err)
		}
		if strings.Contains(err.Error(), testEcho) {
			t.Errorf("%s: message echoes provider content: %v", name, err)
		}
	}
}

func TestTransformationsDeleteAndRevert(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "DELETE" {
			return 204, "", nil
		}
		return 200, `{"transformation":{"path":"` + testTransformationPath + `","name":"base","type":"t","fingerprint":"9"}}`, nil
	}
	out, err := h.call(t, handleTransformationsDelete, `{"transformation":"`+testTransformationPath+`"}`)
	if err != nil || out["deleted"] != true || out["transformation"] != testTransformationPath {
		t.Fatalf("delete = %v, %v", out, err)
	}
	out, err = h.call(t, handleTransformationsRevert, `{"transformation":"`+testTransformationPath+`","fingerprint":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if base, _ := out["transformation"].(map[string]any); base["name"] != "base" {
		t.Errorf("revert = %v", out)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if out, err = h.call(t, handleTransformationsRevert, `{"transformation":"`+testTransformationPath+`","fingerprint":"5"}`); err != nil || len(out) != 0 {
		t.Errorf("revert of a transformation the base version lacks = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 3 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testTransformationPath || sent[0].body != "" ||
		sent[1].method != "POST" || sent[1].path != "/tagmanager/v2/"+testTransformationPath+":revert" ||
		sent[1].query != "fingerprint=5" || sent[1].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestTransformationsOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
	}{
		{"accounts/1000001/containers/2000002", []string{"accounts/1000001/containers/2000009/workspaces/3/transformations/5",
			"accounts/1000009/containers/2000002/workspaces/3/transformations/5"}},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/workspaces/3/transformations/5"}},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			workspace, _, _ := strings.Cut(path, "/transformations/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleTransformationsGet, `{"transformation":"`+path+`"}`)
			reachNothing(t, h, handleTransformationsList, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleTransformationsCreate, `{"workspace":"`+workspace+`","name":"n","type":"t"}`)
			reachNothing(t, h, handleTransformationsUpdate, `{"transformation":"`+path+`","fingerprint":"1","name":"n","type":"t"}`)
			reachNothing(t, h, handleTransformationsDelete, `{"transformation":"`+path+`"}`)
			reachNothing(t, h, handleTransformationsRevert, `{"transformation":"`+path+`","fingerprint":"1"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleTransformationsDelete, `{"transformation":"`+testTransformationPath+`"}`); err != nil {
			t.Errorf("%s: a transformation inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/workspaces/3/transformations/05", "accounts/1/containers/2/workspaces/3",
		"accounts/1/containers/2/workspaces/3/transformations/5/x", "accounts/1/containers/2/workspaces/3/transformations/5?a=b",
		"accounts/1/containers/2/workspaces/3/tags/5"} {
		reachNothing(t, newHarness(t, "", testClientID), handleTransformationsGet, `{"transformation":"`+bad+`"}`)
	}
}

func TestTransformationsListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"transformation":[{"transformationId":"1","name":"a","type":"t"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"transformation":[{"transformationId":"2","name":"b","type":"t"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleTransformationsList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["transformations"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleTransformationsList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["transformations"].([]any)[0].(map[string]any)["transformationId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleTransformationsList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testWorkspace+"/transformations" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
}

func TestTransformationValuesAreNeverCutAndGoBackUnchangedThroughAnUpdate(t *testing.T) {
	value := strings.Repeat("synthetic-value-ÄÖ \"q\" \\ {{y}}\n", 20000)
	stored := map[string]any{"path": testTransformationPath, "name": strings.Repeat("n", 5000), "notes": strings.Repeat("o", 70000),
		"type": "example-type", "fingerprint": "1",
		"parameter": []any{map[string]any{"type": "template", "key": "k", "value": value}}}
	raw, _ := json.Marshal(stored)
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	out, err := h.call(t, handleTransformationsGet, `{"transformation":"`+testTransformationPath+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(out, want) {
		t.Fatal("the transformation read differs from the stored transformation")
	}
	args := map[string]any{"transformation": testTransformationPath, "fingerprint": "1"}
	keys := []string{"name", "notes", "type", "parameter"}
	for _, k := range keys {
		args[k] = out[k]
	}
	encoded, _ := json.Marshal(args)
	if err := application.ValidateJSON(transformationsUpdate.InputSchema, encoded); err != nil {
		t.Fatalf("the schema refuses what a read returned: %v", err)
	}
	if _, err := h.call(t, handleTransformationsUpdate, string(encoded)); err != nil {
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

func TestOversizedTransformationAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	big := `{"name":"n","parameter":[{"type":"template","value":"` + strings.Repeat("a", maxResponse) + `"}]}`
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, big, nil }
	if _, err := h.call(t, handleTransformationsGet, `{"transformation":"`+testTransformationPath+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
	many := strings.Repeat(`{"type":"template"},`, maxParamNodes) + `{"type":"template"}`
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"parameter":[` + many + `]}`, nil
	}
	if _, err := h.call(t, handleTransformationsGet, `{"transformation":"`+testTransformationPath+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response for too many entries", err)
	}
}

func TestTransformationInputBoundsAreRefusedBeforeAnyRequest(t *testing.T) {
	nest := `{"type":"template","value":"x"}`
	for i := 0; i < maxParamDepth; i++ {
		nest = `{"type":"list","list":[` + nest + `]}`
	}
	args := `{"workspace":"` + testWorkspace + `","name":"n","type":"t","parameter":[` + nest + `]}`
	reachNothing(t, newHarness(t, "", testClientID), handleTransformationsCreate, args)
	prefix := `{"workspace":"` + testWorkspace + `","name":"n","type":"t",`
	for _, bad := range []string{
		prefix + `"parameter":"x"}`,
		prefix + `"parameter":[{"type":"number"}]}`,
		prefix + `"parentFolderId":7}`,
		`{"workspace":"` + testWorkspace + `","name":"","type":"t"}`,
		prefix + `"unknown":1}`,
	} {
		if err := application.ValidateJSON(transformationsCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
}

func TestATransformationChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleTransformationsCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"t"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeATransformation(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleTransformationsDelete, `{"transformation":"`+testTransformationPath+`"}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing edit scope and no request", err, len(h.google.seen()))
	}
	if _, err := h.call(t, handleTransformationsGet, `{"transformation":"`+testTransformationPath+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
}
