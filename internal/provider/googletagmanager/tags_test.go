package googletagmanager

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	testWorkspace = "accounts/1000001/containers/2000002/workspaces/3"
	testTag       = testWorkspace + "/tags/4"
)

// sentRequest is one request of a test with its body read.
type sentRequest struct {
	method, path, query, contentType, body string
}

func (h *harness) sent(t *testing.T) []sentRequest {
	t.Helper()
	var out []sentRequest
	for _, req := range h.google.seen() {
		if req.URL.Host != "tagmanager.googleapis.com" || req.Header.Get("Authorization") != "Bearer "+testAccess {
			t.Errorf("request %s %s left the origin or carries no bearer token", req.Method, req.URL.Host)
		}
		body := ""
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			body = string(raw)
		}
		out = append(out, sentRequest{req.Method, req.URL.Path, req.URL.RawQuery, req.Header.Get("Content-Type"), body})
	}
	return out
}

func reachNothing(t *testing.T, h *harness, handler capability.Handler, args string) {
	t.Helper()
	if _, err := h.call(t, handler, args); !isInvalidRequest(err) {
		t.Errorf("%s: err = %v, want invalid-request", args, err)
	}
	if got := len(h.google.seen()); got != 0 || h.google.grants != 0 {
		t.Errorf("%s: %d requests and %d grants, want none", args, got, h.google.grants)
	}
}

func toolOf(t *testing.T, id string) capability.Descriptor {
	t.Helper()
	for _, d := range descriptors() {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("no tool %s", id)
	return capability.Descriptor{}
}

func TestTagToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{tagsListID, tagsGetID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	changes := map[string]capability.Effect{tagsCreateID: capability.EffectCreate, tagsUpdateID: capability.EffectUpdate,
		tagsDeleteID: capability.EffectDelete, tagsRevertID: capability.EffectUpdate}
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
		if listed := d.RequiresToolAllowList; listed != (id == tagsRevertID) {
			t.Errorf("%s listed-only = %v", id, listed)
		}
	}
	if toolOf(t, tagsCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("tags.create must be non-idempotent")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{tagsListID, tagsGetID, accountsGetID, workspacesListID} {
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	for _, id := range []string{tagsCreateID, tagsUpdateID, tagsDeleteID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	for _, id := range []string{tagsRevertID} {
		if slices.Contains(profiles["read"], id) || slices.Contains(profiles["write"], id) {
			t.Errorf("destructive tool %s is in a profile", id)
		}
	}
}

func TestConfirmationSummaryNamesTagTypeAndWorkspace(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(tagsCreateID)(json.RawMessage(
		`{"workspace":"` + testWorkspace + `","name":"Example tag","type":"html"}`))
	for _, want := range []string{"Example tag", "html", testWorkspace} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if got := reg.ConfirmationSummary(tagsDeleteID)(json.RawMessage(`{"tag":"` + testTag + `"}`)); !strings.Contains(got, testTag) {
		t.Errorf("delete summary %q lacks the tag", got)
	}
}

const allTypesTag = `{"name":"Example tag","type":"html","paused":true,"liveOnly":true,"notes":"n",` +
	`"firingTriggerId":["10","11"],"blockingTriggerId":["12"],` +
	`"setupTag":[{"tagName":"setup","stopOnSetupFailure":true}],` +
	`"teardownTag":[{"tagName":"teardown","stopTeardownOnFailure":true}],` +
	`"priority":{"type":"integer","value":"5"},"scheduleStartMs":"1700000000000","scheduleEndMs":"1800000000000",` +
	`"parentFolderId":"7","tagFiringOption":"oncePerLoad","monitoringMetadataTagNameKey":"tagName",` +
	`"monitoringMetadata":{"type":"map","map":[{"type":"template","key":"k","value":"v"}]},` +
	`"consentSettings":{"consentStatus":"needed","consentType":{"type":"list","list":[` +
	`{"type":"template","value":"ad_storage"}]}},` +
	`"parameter":[{"type":"template","key":"html","value":"<!-- example -->"},` +
	`{"type":"integer","key":"n","value":"42"},{"type":"boolean","key":"b","value":"true"},` +
	`{"type":"triggerReference","key":"t","value":"10"},{"type":"tagReference","key":"r","value":"setup"},` +
	`{"type":"list","key":"l","list":[{"type":"template","value":"a"},{"type":"map","map":[` +
	`{"type":"template","key":"x","value":"y"},{"type":"boolean","key":"z","value":"false"}]}]},` +
	`{"type":"map","key":"m","map":[{"type":"template","key":"p","value":"q"}]}]}`

func TestTagsCreateSendsEveryParameterType(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := `{"path":"` + testTag + `","accountId":"1000001","containerId":"2000002","workspaceId":"3",` +
		`"tagId":"4","fingerprint":"1700000000001","tagManagerUrl":"https://tagmanager.google.com/x",` + allTypesTag[1:]
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, stored, nil }
	args := `{"workspace":"` + testWorkspace + `",` + allTypesTag[1:]
	if err := application.ValidateJSON(tagsCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid tag: %v", err)
	}
	out, err := h.call(t, handleTagsCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/tags" ||
		sent[0].contentType != "application/json" || sent[0].query != "" {
		t.Fatalf("sent = %+v", sent)
	}
	var got, want map[string]any
	if err := json.Unmarshal([]byte(sent[0].body), &got); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(allTypesTag), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %s\nwant %s", sent[0].body, allTypesTag)
	}
	if out["fingerprint"] != "1700000000001" || out["tagId"] != "4" {
		t.Errorf("result = %v", out)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(tagsCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	var back map[string]any
	_ = json.Unmarshal([]byte(stored), &back)
	if !reflect.DeepEqual(out, back) {
		t.Errorf("result differs from the stored tag: %s", encoded)
	}
}

func TestTagsCreateNeverSendsOutputOnlyMembers(t *testing.T) {
	h := newHarness(t, "", testClientID)
	args := `{"workspace":"` + testWorkspace + `","name":"n","type":"img"}`
	if _, err := h.call(t, handleTagsCreate, args); err != nil {
		t.Fatal(err)
	}
	if got := h.sent(t)[0].body; got != `{"name":"n","type":"img"}` {
		t.Errorf("body = %s", got)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleTagsCreate, `{"workspace":"`+testWorkspace+`","type":"img"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleTagsCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`)
}

func TestTagsUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"tag":"` + testTag + `","name":"n","type":"html"}`,
		`{"tag":"` + testTag + `","fingerprint":"","name":"n","type":"html"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleTagsUpdate, args)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleTagsRevert, `{"tag":"`+testTag+`"}`)
	if err := application.ValidateJSON(tagsUpdate.InputSchema,
		json.RawMessage(`{"tag":"`+testTag+`","name":"n","type":"html"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
	if err := application.ValidateJSON(tagsRevert.InputSchema, json.RawMessage(`{"tag":"`+testTag+`"}`)); err == nil {
		t.Error("the schema accepts a revert without fingerprint")
	}
}

func TestTagsUpdatePutsTheWholeTagWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testTag + `","fingerprint":"1700000000002","name":"n","type":"html"}`, nil
	}
	out, err := h.call(t, handleTagsUpdate, `{"tag":"`+testTag+`","fingerprint":"1700000000001","name":"n","type":"html","paused":true}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testTag ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].body != `{"name":"n","type":"html","paused":true}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestStaleFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, status := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return status, googleBody(status, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleTagsUpdate, `{"tag":"`+testTag+`","fingerprint":"1","name":"n","type":"html"}`)
		if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
			t.Fatalf("status %d: err = %v", status, err)
		}
		if strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("status %d is a definite refusal: %v", status, err)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("status %d: requests = %d, want 1", status, got)
		}
	}
}

func TestTagsDeleteAndRevert(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "DELETE" {
			return 204, "", nil
		}
		return 200, `{"tag":{"path":"` + testTag + `","name":"base","type":"html","fingerprint":"9"}}`, nil
	}
	out, err := h.call(t, handleTagsDelete, `{"tag":"`+testTag+`"}`)
	if err != nil || out["deleted"] != true || out["tag"] != testTag {
		t.Fatalf("delete = %v, %v", out, err)
	}
	out, err = h.call(t, handleTagsRevert, `{"tag":"`+testTag+`","fingerprint":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if base, _ := out["tag"].(map[string]any); base["name"] != "base" {
		t.Errorf("revert = %v", out)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if out, err = h.call(t, handleTagsRevert, `{"tag":"`+testTag+`","fingerprint":"5"}`); err != nil || len(out) != 0 {
		t.Errorf("revert of a tag the base version lacks = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 3 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testTag || sent[0].body != "" ||
		sent[1].method != "POST" || sent[1].path != "/tagmanager/v2/"+testTag+":revert" || sent[1].query != "fingerprint=5" ||
		sent[1].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestTagsOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
		inside string
	}{
		{"accounts/1000001/containers/2000002",
			[]string{"accounts/1000001/containers/2000009/workspaces/3/tags/4", "accounts/1000009/containers/2000002/workspaces/3/tags/4"},
			testTag},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/workspaces/3/tags/4"}, testTag},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			workspace, _, _ := strings.Cut(path, "/tags/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleTagsGet, `{"tag":"`+path+`"}`)
			reachNothing(t, h, handleTagsList, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleTagsCreate, `{"workspace":"`+workspace+`","name":"n","type":"html"}`)
			reachNothing(t, h, handleTagsUpdate, `{"tag":"`+path+`","fingerprint":"1","name":"n","type":"html"}`)
			reachNothing(t, h, handleTagsDelete, `{"tag":"`+path+`"}`)
			reachNothing(t, h, handleTagsRevert, `{"tag":"`+path+`","fingerprint":"1"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleTagsDelete, `{"tag":"`+c.inside+`"}`); err != nil {
			t.Errorf("%s: a tag inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/workspaces/3/tags/04", "accounts/1/containers/2/workspaces/3",
		"accounts/1/containers/2/workspaces/3/tags/4/x", "accounts/1/containers/2/workspaces/3/tags/4?a=b"} {
		reachNothing(t, newHarness(t, "", testClientID), handleTagsGet, `{"tag":"`+bad+`"}`)
	}
}

func TestTagsListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"tag":[{"tagId":"1","name":"a","type":"html"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"tag":[{"tagId":"2","name":"b","type":"img"}]}`, nil
		}
		return 400, `{}`, nil
	}
	args := `{"workspace":"` + testWorkspace + `"}`
	first, err := h.call(t, handleTagsList, args)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["tags"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleTagsList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["tags"].([]any)[0].(map[string]any)["tagId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleTagsList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if paths := h.sent(t); paths[0].query != "" || paths[1].query != "pageToken=two" {
		t.Errorf("queries = %+v", paths)
	}
}

func TestParameterValuesAreNeverCutOrChanged(t *testing.T) {
	long := strings.Repeat("<b>é€   \"quoted\" \\ </b>", 40000) // far beyond every text bound of the other tools
	stored := map[string]any{"path": testTag, "name": strings.Repeat("n", 5000), "notes": strings.Repeat("o", 70000),
		"type": "html", "fingerprint": "1", "firingTriggerId": idsOf(300),
		"parameter": []any{map[string]any{"type": "template", "key": "html", "value": long}}}
	raw, _ := json.Marshal(stored)
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	out, err := h.call(t, handleTagsGet, `{"tag":"`+testTag+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(out, want) {
		t.Fatal("the tag read differs from the stored tag")
	}
	// What a read returns goes back unchanged through an update.
	args := map[string]any{"tag": testTag, "fingerprint": "1"}
	for _, k := range []string{"name", "notes", "type", "firingTriggerId", "parameter"} {
		args[k] = out[k]
	}
	encoded, _ := json.Marshal(args)
	if err := application.ValidateJSON(tagsUpdate.InputSchema, encoded); err != nil {
		t.Fatalf("the schema refuses what a read returned: %v", err)
	}
	if _, err := h.call(t, handleTagsUpdate, string(encoded)); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(h.sent(t)[1].body), &sent)
	for _, k := range []string{"name", "notes", "type", "firingTriggerId", "parameter"} {
		if !reflect.DeepEqual(sent[k], want[k]) {
			t.Errorf("%s was changed on the way", k)
		}
	}
}

func idsOf(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = "9" + string(rune('0'+i%10)) + strings.Repeat("0", i%7)
	}
	return out
}

func TestOversizedAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	big := `{"name":"n","parameter":[{"type":"template","value":"` + strings.Repeat("a", maxResponse) + `"}]}`
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, big, nil }
	if _, err := h.call(t, handleTagsGet, `{"tag":"`+testTag+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
	// A string where a list belongs is not a value to shorten either.
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{"firingTriggerId":"10"}`, nil }
	if _, err := h.call(t, handleTagsGet, `{"tag":"`+testTag+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
}

func TestTagInputBoundsAreRefusedBeforeAnyRequest(t *testing.T) {
	nest := `{"type":"template","value":"x"}`
	for i := 0; i < maxParamDepth; i++ {
		nest = `{"type":"list","list":[` + nest + `]}`
	}
	args := `{"workspace":"` + testWorkspace + `","name":"n","type":"html","parameter":[` + nest + `]}`
	reachNothing(t, newHarness(t, "", testClientID), handleTagsCreate, args)
	if err := application.ValidateJSON(tagsCreate.InputSchema, json.RawMessage(args)); err == nil {
		t.Error("the schema accepts parameters nested deeper than the bound")
	}
	for _, bad := range []string{
		`{"workspace":"` + testWorkspace + `","name":"n","type":"html","tagFiringOption":"sometimes"}`,
		`{"workspace":"` + testWorkspace + `","name":"n","type":"html","parameter":[{"type":"text"}]}`,
		`{"workspace":"` + testWorkspace + `","name":"n","type":"html","unknown":1}`,
		`{"workspace":"` + testWorkspace + `","name":"n","type":"html","scheduleStartMs":"soon"}`,
	} {
		if err := application.ValidateJSON(tagsCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
}

func TestAChangeIsSentOnceAndWaitsForSpacingOnce(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 401, googleBody(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), nil
	}
	if _, err := h.call(t, handleTagsDelete, `{"tag":"`+testTag+`"}`); classOf(err) != provider.ClassAuth {
		t.Fatalf("err = %v", err)
	}
	if got := len(h.google.seen()); got != 1 {
		t.Errorf("requests = %d, want 1: a change is never sent twice", got)
	}
	if waits := h.spacing.recorded(); len(waits) != 0 {
		t.Errorf("waits = %v, want none: one request waits once, and the first needs no wait", waits)
	}
	// The rejected token is dropped, so the next call asks for a new one and waits for the spacing once.
	h.google.api = nil
	if _, err := h.call(t, handleTagsDelete, `{"tag":"`+testTag+`"}`); err != nil {
		t.Fatal(err)
	}
	if h.google.grants != 2 {
		t.Errorf("grants = %d, want 2", h.google.grants)
	}
	if waits := h.spacing.recorded(); len(waits) != 1 || waits[0] != minInterval {
		t.Errorf("waits = %v, want one wait of %s", waits, minInterval)
	}
}

func TestAChangeWithUnclearOutcomeSaysSoAndIsNotRepeated(t *testing.T) {
	cases := map[string]func(*harness){
		"timeout":  func(h *harness) { h.google.fail = context.DeadlineExceeded },
		"500":      func(h *harness) { h.google.api = status(500, `oops`) },
		"unusable": func(h *harness) { h.google.api = status(200, `{"path":`) },
	}
	for name, setup := range cases {
		h := newHarness(t, "", testClientID)
		setup(h)
		_, err := h.call(t, handleTagsCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"html"}`)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("%s: err = %v, want the unknown outcome", name, err)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("%s: requests = %d, want 1", name, got)
		}
	}
	h := newHarness(t, "", testClientID)
	h.google.api = status(429, googleBody(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"))
	_, err := h.call(t, handleTagsCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"html"}`)
	if classOf(err) != provider.ClassRateLimited || strings.Contains(err.Error(), "unknown") {
		t.Errorf("429: err = %v, want a plain rate limit", err)
	}
}

func status(code int, body string) func(*http.Request) (int, string, http.Header) {
	return func(*http.Request) (int, string, http.Header) { return code, body, nil }
}

func TestAReadOnlyConsentCannotChangeATag(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleTagsDelete, `{"tag":"`+testTag+`"}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing edit scope and no request", err, len(h.google.seen()))
	}
	if _, err := h.call(t, handleTagsGet, `{"tag":"`+testTag+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
}
