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

const testTrigger = testWorkspace + "/triggers/5"

func TestTriggerToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{triggersListID, triggersGetID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	changes := map[string]capability.Effect{triggersCreateID: capability.EffectCreate,
		triggersUpdateID: capability.EffectUpdate, triggersDeleteID: capability.EffectDelete,
		triggersRevertID: capability.EffectUpdate}
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
		if d.RequiresToolAllowList != (id == triggersRevertID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
	}
	if toolOf(t, triggersCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("triggers.create must be non-idempotent")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{triggersListID, triggersGetID} {
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	for _, id := range []string{triggersCreateID, triggersUpdateID, triggersDeleteID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if slices.Contains(profiles["read"], triggersRevertID) || slices.Contains(profiles["write"], triggersRevertID) {
		t.Error("triggers.revert is in a profile")
	}
}

func TestConfirmationSummaryNamesTriggerTypeAndWorkspace(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(triggersCreateID)(json.RawMessage(
		`{"workspace":"` + testWorkspace + `","name":"Example trigger","type":"linkClick"}`))
	for _, want := range []string{"Example trigger", "linkClick", testWorkspace, "trigger"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if got := reg.ConfirmationSummary(triggersDeleteID)(json.RawMessage(`{"trigger":"` + testTrigger + `"}`)); !strings.Contains(got, testTrigger) {
		t.Errorf("delete summary %q lacks the trigger", got)
	}
}

// allMembersTrigger sets every member a caller can set, with several filter groups.
const allMembersTrigger = `{"name":"Example trigger","type":"linkClick","notes":"n","parentFolderId":"7",` +
	`"filter":[{"type":"matchRegex","parameter":[{"type":"template","key":"arg0","value":"{{Page URL}}"},` +
	`{"type":"template","key":"arg1","value":"^https://example\\.invalid/(a|b)+$"},` +
	`{"type":"boolean","key":"ignore_case","value":"true"}]},` +
	`{"type":"equals","parameter":[{"type":"template","key":"arg0","value":"{{Page Path}}"},` +
	`{"type":"template","key":"arg1","value":"/x"},{"type":"boolean","key":"negate","value":"true"}]}],` +
	`"autoEventFilter":[{"type":"cssSelector","parameter":[{"type":"template","key":"arg0","value":"{{Click Element}}"},` +
	`{"type":"template","key":"arg1","value":"a.button, #id > span"}]}],` +
	`"customEventFilter":[{"type":"equals","parameter":[{"type":"template","key":"arg0","value":"{{_event}}"},` +
	`{"type":"template","key":"arg1","value":"signup"}]}],` +
	`"eventName":{"type":"template","value":"tick"},` +
	`"waitForTags":{"type":"boolean","value":"true"},"checkValidation":{"type":"boolean","value":"false"},` +
	`"waitForTagsTimeout":{"type":"integer","value":"2000"},"uniqueTriggerId":{"type":"template","value":"u1"},` +
	`"intervalSeconds":{"type":"integer","value":"5"},"maxTimerLengthSeconds":{"type":"integer","value":"60"},` +
	`"limit":{"type":"integer","value":"3"},"interval":{"type":"integer","value":"1000"},` +
	`"verticalScrollPercentageList":{"type":"list","list":[{"type":"integer","value":"25"},{"type":"integer","value":"50"}]},` +
	`"horizontalScrollPercentageList":{"type":"list","list":[{"type":"integer","value":"10"}]},` +
	`"visibilitySelector":{"type":"template","value":"#hero"},"visiblePercentageMin":{"type":"integer","value":"20"},` +
	`"visiblePercentageMax":{"type":"integer","value":"100"},` +
	`"continuousTimeMinMilliseconds":{"type":"integer","value":"500"},` +
	`"totalTimeMinMilliseconds":{"type":"integer","value":"1500"},"selector":{"type":"template","value":"button.buy"},` +
	`"parameter":[{"type":"template","key":"k","value":"v"},{"type":"list","key":"l","list":[{"type":"template","value":"a"}]}]}`

func TestTriggersCreateSendsEveryMemberAndFilterGroup(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := `{"path":"` + testTrigger + `","accountId":"1000001","containerId":"2000002","workspaceId":"3",` +
		`"triggerId":"5","fingerprint":"1700000000001","tagManagerUrl":"https://tagmanager.google.com/x",` + allMembersTrigger[1:]
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, stored, nil }
	args := `{"workspace":"` + testWorkspace + `",` + allMembersTrigger[1:]
	if err := application.ValidateJSON(triggersCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid trigger: %v", err)
	}
	out, err := h.call(t, handleTriggersCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/triggers" ||
		sent[0].contentType != "application/json" || sent[0].query != "" {
		t.Fatalf("sent = %+v", sent)
	}
	var got, want map[string]any
	if err := json.Unmarshal([]byte(sent[0].body), &got); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(allMembersTrigger), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %s\nwant %s", sent[0].body, allMembersTrigger)
	}
	if len(got["filter"].([]any)) != 2 {
		t.Error("a filter group was lost")
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(triggersCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	var back map[string]any
	_ = json.Unmarshal([]byte(stored), &back)
	if !reflect.DeepEqual(out, back) {
		t.Errorf("result differs from the stored trigger: %s", encoded)
	}
}

func TestTriggersCreateNeverSendsOutputOnlyMembers(t *testing.T) {
	h := newHarness(t, "", testClientID)
	if _, err := h.call(t, handleTriggersCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"pageview"}`); err != nil {
		t.Fatal(err)
	}
	if got := h.sent(t)[0].body; got != `{"name":"n","type":"pageview"}` {
		t.Errorf("body = %s", got)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleTriggersCreate, `{"workspace":"`+testWorkspace+`","type":"pageview"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleTriggersCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`)
}

func TestTriggersUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"trigger":"` + testTrigger + `","name":"n","type":"pageview"}`,
		`{"trigger":"` + testTrigger + `","fingerprint":"","name":"n","type":"pageview"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleTriggersUpdate, args)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleTriggersRevert, `{"trigger":"`+testTrigger+`"}`)
	if err := application.ValidateJSON(triggersUpdate.InputSchema,
		json.RawMessage(`{"trigger":"`+testTrigger+`","name":"n","type":"pageview"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
	if err := application.ValidateJSON(triggersRevert.InputSchema, json.RawMessage(`{"trigger":"`+testTrigger+`"}`)); err == nil {
		t.Error("the schema accepts a revert without fingerprint")
	}
}

func TestTriggersUpdatePutsTheWholeTriggerWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testTrigger + `","fingerprint":"1700000000002","name":"n","type":"pageview"}`, nil
	}
	out, err := h.call(t, handleTriggersUpdate, `{"trigger":"`+testTrigger+`","fingerprint":"1700000000001","name":"n","type":"pageview","notes":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testTrigger ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].body != `{"name":"n","type":"pageview","notes":"x"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestStaleTriggerFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleTriggersUpdate, `{"trigger":"`+testTrigger+`","fingerprint":"1","name":"n","type":"pageview"}`)
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

func TestTriggersDeleteAndRevert(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "DELETE" {
			return 204, "", nil
		}
		return 200, `{"trigger":{"path":"` + testTrigger + `","name":"base","type":"pageview","fingerprint":"9"}}`, nil
	}
	out, err := h.call(t, handleTriggersDelete, `{"trigger":"`+testTrigger+`"}`)
	if err != nil || out["deleted"] != true || out["trigger"] != testTrigger {
		t.Fatalf("delete = %v, %v", out, err)
	}
	out, err = h.call(t, handleTriggersRevert, `{"trigger":"`+testTrigger+`","fingerprint":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if base, _ := out["trigger"].(map[string]any); base["name"] != "base" {
		t.Errorf("revert = %v", out)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if out, err = h.call(t, handleTriggersRevert, `{"trigger":"`+testTrigger+`","fingerprint":"5"}`); err != nil || len(out) != 0 {
		t.Errorf("revert of a trigger the base version lacks = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 3 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testTrigger || sent[0].body != "" ||
		sent[1].method != "POST" || sent[1].path != "/tagmanager/v2/"+testTrigger+":revert" ||
		sent[1].query != "fingerprint=5" || sent[1].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestTriggersOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
	}{
		{"accounts/1000001/containers/2000002", []string{"accounts/1000001/containers/2000009/workspaces/3/triggers/5",
			"accounts/1000009/containers/2000002/workspaces/3/triggers/5"}},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/workspaces/3/triggers/5"}},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			workspace, _, _ := strings.Cut(path, "/triggers/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleTriggersGet, `{"trigger":"`+path+`"}`)
			reachNothing(t, h, handleTriggersList, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleTriggersCreate, `{"workspace":"`+workspace+`","name":"n","type":"pageview"}`)
			reachNothing(t, h, handleTriggersUpdate, `{"trigger":"`+path+`","fingerprint":"1","name":"n","type":"pageview"}`)
			reachNothing(t, h, handleTriggersDelete, `{"trigger":"`+path+`"}`)
			reachNothing(t, h, handleTriggersRevert, `{"trigger":"`+path+`","fingerprint":"1"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleTriggersDelete, `{"trigger":"`+testTrigger+`"}`); err != nil {
			t.Errorf("%s: a trigger inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/workspaces/3/triggers/05", "accounts/1/containers/2/workspaces/3",
		"accounts/1/containers/2/workspaces/3/triggers/5/x", "accounts/1/containers/2/workspaces/3/triggers/5?a=b",
		"accounts/1/containers/2/workspaces/3/tags/5"} {
		reachNothing(t, newHarness(t, "", testClientID), handleTriggersGet, `{"trigger":"`+bad+`"}`)
	}
}

func TestTriggersListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"trigger":[{"triggerId":"1","name":"a","type":"pageview"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"trigger":[{"triggerId":"2","name":"b","type":"click"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleTriggersList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["triggers"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleTriggersList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["triggers"].([]any)[0].(map[string]any)["triggerId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleTriggersList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testWorkspace+"/triggers" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
}

func TestFilterValuesAreNeverCutOrChanged(t *testing.T) {
	long := strings.Repeat(`^(a|b)+\.é€   "q" \\ [x]$|`, 40000)
	stored := map[string]any{"path": testTrigger, "name": strings.Repeat("n", 5000), "notes": strings.Repeat("o", 70000),
		"type": "customEvent", "fingerprint": "1",
		"customEventFilter": []any{map[string]any{"type": "matchRegex", "parameter": []any{
			map[string]any{"type": "template", "key": "arg1", "value": long}}}}}
	raw, _ := json.Marshal(stored)
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	out, err := h.call(t, handleTriggersGet, `{"trigger":"`+testTrigger+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(out, want) {
		t.Fatal("the trigger read differs from the stored trigger")
	}
	// What a read returns goes back unchanged through an update.
	args := map[string]any{"trigger": testTrigger, "fingerprint": "1"}
	keys := []string{"name", "notes", "type", "customEventFilter"}
	for _, k := range keys {
		args[k] = out[k]
	}
	encoded, _ := json.Marshal(args)
	if err := application.ValidateJSON(triggersUpdate.InputSchema, encoded); err != nil {
		t.Fatalf("the schema refuses what a read returned: %v", err)
	}
	if _, err := h.call(t, handleTriggersUpdate, string(encoded)); err != nil {
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

func TestOversizedTriggerAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	big := `{"name":"n","filter":[{"parameter":[{"type":"template","value":"` + strings.Repeat("a", maxResponse) + `"}]}]}`
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, big, nil }
	if _, err := h.call(t, handleTriggersGet, `{"trigger":"`+testTrigger+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"filter":[{"parameter":[` + strings.Repeat(`{"type":"template"},`, maxParamNodes) + `{"type":"template"}]}]}`, nil
	}
	if _, err := h.call(t, handleTriggersGet, `{"trigger":"`+testTrigger+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response for too many entries", err)
	}
}

func TestTriggerInputBoundsAreRefusedBeforeAnyRequest(t *testing.T) {
	nest := `{"type":"template","value":"x"}`
	for i := 0; i < maxParamDepth; i++ {
		nest = `{"type":"list","list":[` + nest + `]}`
	}
	args := `{"workspace":"` + testWorkspace + `","name":"n","type":"pageview","filter":[{"type":"equals","parameter":[` + nest + `]}]}`
	reachNothing(t, newHarness(t, "", testClientID), handleTriggersCreate, args)
	prefix := `{"workspace":"` + testWorkspace + `","name":"n","type":"pageview",`
	for _, bad := range []string{
		prefix + `"filter":[{"type":"likes"}]}`,
		prefix + `"filter":[{"type":"equals","unknown":1}]}`,
		prefix + `"limit":{"type":"number"}}`,
		prefix + `"limit":"3"}`,
		`{"workspace":"` + testWorkspace + `","name":"n","type":"doNothing"}`,
		prefix + `"unknown":1}`,
	} {
		if err := application.ValidateJSON(triggersCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
}

func TestATriggerChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleTriggersCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"pageview"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeATrigger(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleTriggersDelete, `{"trigger":"`+testTrigger+`"}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing edit scope and no request", err, len(h.google.seen()))
	}
	if _, err := h.call(t, handleTriggersGet, `{"trigger":"`+testTrigger+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
}
