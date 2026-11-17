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

const testZonePath = testWorkspace + "/zones/5"

func TestZoneToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{zonesListID, zonesGetID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	changes := map[string]capability.Effect{zonesCreateID: capability.EffectCreate,
		zonesUpdateID: capability.EffectUpdate, zonesDeleteID: capability.EffectDelete,
		zonesRevertID: capability.EffectUpdate}
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
		if d.RequiresToolAllowList != (id == zonesRevertID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
	}
	if toolOf(t, zonesCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("zones.create must be non-idempotent")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{zonesListID, zonesGetID} {
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	for _, id := range []string{zonesCreateID, zonesUpdateID, zonesDeleteID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if slices.Contains(profiles["read"], zonesRevertID) || slices.Contains(profiles["write"], zonesRevertID) {
		t.Error("zones.revert is in a profile")
	}
}

func TestZoneDescriptionsNameTheEffect(t *testing.T) {
	for _, id := range []string{zonesCreateID, zonesUpdateID, zonesDeleteID, zonesRevertID} {
		d := toolOf(t, id)
		for _, want := range []string{"childContainer", "configuration of that container", "workspace only",
			"published container version", "versions.publish"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", id, want)
			}
		}
	}
	for _, id := range []string{zonesCreateID, zonesUpdateID} {
		if !strings.Contains(toolOf(t, id).Description, "public ID of a container of that account") {
			t.Errorf("%s description lacks the binding rule", id)
		}
	}
}

func TestConfirmationSummaryNamesZoneWorkspaceAndChildContainers(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	args := `{"workspace":"` + testWorkspace + `","name":"Example zone","childContainer":[` +
		`{"publicId":"GTM-EXAMPLE1","nickname":"one"},{"publicId":"GTM-EXAMPLE2"}],` +
		`"boundary":{"condition":[{"type":"equals","parameter":[{"type":"template","key":"arg1","value":"synthetic-condition-value"}]}]}}`
	got := reg.ConfirmationSummary(zonesCreateID)(json.RawMessage(args))
	for _, want := range []string{"Example zone", testWorkspace, "GTM-EXAMPLE1", "GTM-EXAMPLE2", "zone"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "synthetic-condition-value") {
		t.Errorf("summary %q shows a condition value", got)
	}
	got = reg.ConfirmationSummary(zonesUpdateID)(json.RawMessage(
		`{"zone":"` + testZonePath + `","fingerprint":"1","name":"n","childContainer":[{"publicId":"GTM-EXAMPLE1"}]}`))
	for _, want := range []string{testZonePath, "GTM-EXAMPLE1"} {
		if !strings.Contains(got, want) {
			t.Errorf("update summary %q lacks %q", got, want)
		}
	}
	for _, id := range []string{zonesDeleteID, zonesRevertID} {
		if got := reg.ConfirmationSummary(id)(json.RawMessage(`{"zone":"` + testZonePath + `"}`)); !strings.Contains(got, testZonePath) {
			t.Errorf("%s summary %q lacks the zone", id, got)
		}
	}
}

// allMembersZone sets every member a caller can set.
const allMembersZone = `{"name":"Example zone","notes":"n",` +
	`"boundary":{"customEvaluationTriggerId":["7","8"],"condition":[{"type":"matchRegex","parameter":[` +
	`{"type":"template","key":"arg0","value":"{{Page URL}}"},{"type":"template","key":"arg1","value":"^/example"},` +
	`{"type":"boolean","key":"ignore_case","value":"true"}]}]},` +
	`"childContainer":[{"publicId":"GTM-EXAMPLE1","nickname":"one"}],` +
	`"typeRestriction":{"enable":true,"whitelistedTypeId":["example-type"]}}`

func TestZonesCreateSendsEveryMember(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := `{"path":"` + testZonePath + `","accountId":"1000001","containerId":"2000002","workspaceId":"3",` +
		`"zoneId":"5","fingerprint":"1700000000001","tagManagerUrl":"https://tagmanager.google.com/x",` + allMembersZone[1:]
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, stored, nil }
	args := `{"workspace":"` + testWorkspace + `",` + allMembersZone[1:]
	if err := application.ValidateJSON(zonesCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid zone: %v", err)
	}
	out, err := h.call(t, handleZonesCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/zones" ||
		sent[0].contentType != "application/json" || sent[0].query != "" {
		t.Fatalf("sent = %+v", sent)
	}
	var got, want map[string]any
	if err := json.Unmarshal([]byte(sent[0].body), &got); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(allMembersZone), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %s\nwant %s", sent[0].body, allMembersZone)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(zonesCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	var back map[string]any
	_ = json.Unmarshal([]byte(stored), &back)
	if !reflect.DeepEqual(out, back) {
		t.Errorf("result differs from the stored zone: %s", encoded)
	}
}

func TestZonesCreateNeverSendsOutputOnlyMembersAndNeedsAName(t *testing.T) {
	h := newHarness(t, "", testClientID)
	if _, err := h.call(t, handleZonesCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`); err != nil {
		t.Fatal(err)
	}
	if sent := h.sent(t); sent[0].body != `{"name":"n"}` {
		t.Errorf("body = %s", sent[0].body)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleZonesCreate, `{"workspace":"`+testWorkspace+`"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleZonesCreate, `{"workspace":"`+testWorkspace+`","name":" "}`)
}

func TestZonesUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"zone":"` + testZonePath + `","name":"n"}`,
		`{"zone":"` + testZonePath + `","fingerprint":"","name":"n"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleZonesUpdate, args)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleZonesRevert, `{"zone":"`+testZonePath+`"}`)
	if err := application.ValidateJSON(zonesUpdate.InputSchema, json.RawMessage(`{"zone":"`+testZonePath+`","name":"n"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
	if err := application.ValidateJSON(zonesRevert.InputSchema, json.RawMessage(`{"zone":"`+testZonePath+`"}`)); err == nil {
		t.Error("the schema accepts a revert without fingerprint")
	}
}

func TestZonesUpdatePutsTheWholeZoneWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testZonePath + `","fingerprint":"1700000000002","name":"n"}`, nil
	}
	out, err := h.call(t, handleZonesUpdate, `{"zone":"`+testZonePath+`","fingerprint":"1700000000001","name":"n","notes":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testZonePath ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].body != `{"name":"n","notes":"x"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestStaleZoneFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleZonesUpdate, `{"zone":"`+testZonePath+`","fingerprint":"1","name":"n"}`)
		if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
			t.Fatalf("status %d: err = %v", code, err)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("status %d: requests = %d, want 1", code, got)
		}
	}
}

func TestZonesDeleteAndRevert(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "DELETE" {
			return 204, "", nil
		}
		return 200, `{"zone":{"path":"` + testZonePath + `","name":"base","fingerprint":"9"}}`, nil
	}
	out, err := h.call(t, handleZonesDelete, `{"zone":"`+testZonePath+`"}`)
	if err != nil || out["deleted"] != true || out["zone"] != testZonePath {
		t.Fatalf("delete = %v, %v", out, err)
	}
	out, err = h.call(t, handleZonesRevert, `{"zone":"`+testZonePath+`","fingerprint":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if base, _ := out["zone"].(map[string]any); base["name"] != "base" {
		t.Errorf("revert = %v", out)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if out, err = h.call(t, handleZonesRevert, `{"zone":"`+testZonePath+`","fingerprint":"5"}`); err != nil || len(out) != 0 {
		t.Errorf("revert of a zone the base version lacks = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 3 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testZonePath || sent[0].body != "" ||
		sent[1].method != "POST" || sent[1].path != "/tagmanager/v2/"+testZonePath+":revert" ||
		sent[1].query != "fingerprint=5" || sent[1].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestZonesOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
	}{
		{"accounts/1000001/containers/2000002", []string{"accounts/1000001/containers/2000009/workspaces/3/zones/5",
			"accounts/1000009/containers/2000002/workspaces/3/zones/5"}},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/workspaces/3/zones/5"}},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			workspace, _, _ := strings.Cut(path, "/zones/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleZonesGet, `{"zone":"`+path+`"}`)
			reachNothing(t, h, handleZonesList, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleZonesCreate, `{"workspace":"`+workspace+`","name":"n"}`)
			reachNothing(t, h, handleZonesUpdate, `{"zone":"`+path+`","fingerprint":"1","name":"n"}`)
			reachNothing(t, h, handleZonesDelete, `{"zone":"`+path+`"}`)
			reachNothing(t, h, handleZonesRevert, `{"zone":"`+path+`","fingerprint":"1"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleZonesDelete, `{"zone":"`+testZonePath+`"}`); err != nil {
			t.Errorf("%s: a zone inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/workspaces/3/zones/05", "accounts/1/containers/2/workspaces/3",
		"accounts/1/containers/2/workspaces/3/zones/5/x", "accounts/1/containers/2/workspaces/3/zones/5?a=b",
		"accounts/1/containers/2/workspaces/3/tags/5"} {
		reachNothing(t, newHarness(t, "", testClientID), handleZonesGet, `{"zone":"`+bad+`"}`)
	}
}

func TestZonesListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"zone":[{"zoneId":"1","name":"a"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"zone":[{"zoneId":"2","name":"b"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleZonesList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["zones"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleZonesList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["zones"].([]any)[0].(map[string]any)["zoneId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleZonesList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testWorkspace+"/zones" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
}

func TestZoneValuesAreNeverCutAndGoBackUnchangedThroughAnUpdate(t *testing.T) {
	value := strings.Repeat("synthetic-value-ÄÖ \"q\" \\ {{y}}\n", 20000)
	stored := map[string]any{"path": testZonePath, "name": strings.Repeat("n", 5000), "notes": strings.Repeat("o", 70000),
		"fingerprint": "1",
		"boundary": map[string]any{"customEvaluationTriggerId": []any{"7"}, "condition": []any{map[string]any{
			"type": "contains", "parameter": []any{map[string]any{"type": "template", "key": "arg1", "value": value}}}}},
		"childContainer":  []any{map[string]any{"publicId": "GTM-EXAMPLE1", "nickname": strings.Repeat("k", 5000)}},
		"typeRestriction": map[string]any{"enable": true, "whitelistedTypeId": []any{"example-type"}}}
	raw, _ := json.Marshal(stored)
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	out, err := h.call(t, handleZonesGet, `{"zone":"`+testZonePath+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(out, want) {
		t.Fatal("the zone read differs from the stored zone")
	}
	args := map[string]any{"zone": testZonePath, "fingerprint": "1"}
	keys := []string{"name", "notes", "boundary", "childContainer", "typeRestriction"}
	for _, k := range keys {
		args[k] = out[k]
	}
	encoded, _ := json.Marshal(args)
	if err := application.ValidateJSON(zonesUpdate.InputSchema, encoded); err != nil {
		t.Fatalf("the schema refuses what a read returned: %v", err)
	}
	if _, err := h.call(t, handleZonesUpdate, string(encoded)); err != nil {
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

func TestOversizedZoneAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	big := `{"name":"n","boundary":{"condition":[{"parameter":[{"type":"template","value":"` +
		strings.Repeat("a", maxResponse) + `"}]}]}}`
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, big, nil }
	if _, err := h.call(t, handleZonesGet, `{"zone":"`+testZonePath+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
	many := strings.Repeat(`{"type":"template"},`, maxParamNodes) + `{"type":"template"}`
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"boundary":{"condition":[{"parameter":[` + many + `]}]}}`, nil
	}
	if _, err := h.call(t, handleZonesGet, `{"zone":"`+testZonePath+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response for too many entries", err)
	}
}

func TestZoneInputBoundsAreRefusedBeforeAnyRequest(t *testing.T) {
	nest := `{"type":"template","value":"x"}`
	for i := 0; i < triggerParamDepth; i++ {
		nest = `{"type":"list","list":[` + nest + `]}`
	}
	prefix := `{"workspace":"` + testWorkspace + `","name":"n",`
	for _, bad := range []string{
		prefix + `"boundary":{"condition":[{"parameter":[` + nest + `]}]}}`,
		prefix + `"boundary":{"condition":[{"type":"likes"}]}}`,
		prefix + `"boundary":{"unknown":1}}`,
		prefix + `"boundary":"x"}`,
		prefix + `"childContainer":"x"}`,
		prefix + `"childContainer":[{"publicId":7}]}`,
		prefix + `"typeRestriction":{"enable":"yes"}}`,
		prefix + `"parameter":[]}`,
		prefix + `"type":"t"}`,
		`{"workspace":"` + testWorkspace + `","name":""}`,
	} {
		if err := application.ValidateJSON(zonesCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
	deep := `{"type":"template","value":"x"}`
	for i := 0; i < maxParamDepth; i++ {
		deep = `{"type":"list","list":[` + deep + `]}`
	}
	reachNothing(t, newHarness(t, "", testClientID), handleZonesCreate,
		prefix+`"boundary":{"condition":[{"parameter":[`+deep+`]}]}}`)
}

func TestAZoneChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleZonesCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeAZone(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleZonesDelete, `{"zone":"`+testZonePath+`"}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing edit scope and no request", err, len(h.google.seen()))
	}
	if _, err := h.call(t, handleZonesGet, `{"zone":"`+testZonePath+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
}

// accountContainers answers the container list of an account in two pages and any zone write with a zone.
func accountContainers(req *http.Request) (int, string, http.Header) {
	if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/containers") {
		if req.URL.Query().Get("pageToken") == "" {
			return 200, `{"container":[{"containerId":"2000002","publicId":"GTM-OWNONE"}],"nextPageToken":"more"}`, nil
		}
		return 200, `{"container":[{"containerId":"2000003","publicId":"GTM-OWNTWO"},{"containerId":"2000004"}]}`, nil
	}
	return 200, `{"path":"` + testZonePath + `","name":"n","fingerprint":"2"}`, nil
}

func writesOf(sent []sentRequest) int {
	n := 0
	for _, r := range sent {
		if r.method != http.MethodGet {
			n++
		}
	}
	return n
}

func TestChildContainerOfTheBoundAccountIsAllowedAndFetchedBeforeTheWrite(t *testing.T) {
	create := `{"workspace":"` + testWorkspace + `","name":"n","childContainer":[{"publicId":"GTM-OWNONE"},` +
		`{"publicId":"GTM-OWNTWO","nickname":"two"}]}`
	update := `{"zone":"` + testZonePath + `","fingerprint":"1","name":"n","childContainer":[{"publicId":"GTM-OWNTWO"}]}`
	for name, run := range map[string]func(h *harness) error{
		"create": func(h *harness) error { _, err := h.call(t, handleZonesCreate, create); return err },
		"update": func(h *harness) error { _, err := h.call(t, handleZonesUpdate, update); return err },
	} {
		h := newHarness(t, "accounts/1000001", testClientID)
		h.google.api = accountContainers
		if err := run(h); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sent := h.sent(t)
		if len(sent) != 3 || sent[0].method != "GET" || sent[0].path != "/tagmanager/v2/accounts/1000001/containers" ||
			sent[0].query != "" || sent[1].query != "pageToken=more" || sent[2].method == "GET" {
			t.Errorf("%s: sent = %+v", name, sent)
		}
		if !strings.Contains(sent[2].body, "GTM-OWNTWO") {
			t.Errorf("%s: body = %s", name, sent[2].body)
		}
	}
}

func TestChildContainerOutsideTheBoundAccountIsRefusedWithoutAWrite(t *testing.T) {
	for _, public := range []string{"GTM-FOREIGN", "", "gtm-ownone", "GTM-OWNONE ", "GTM-OWNONE,GTM-OWNTWO"} {
		child, _ := json.Marshal([]map[string]string{{"publicId": "GTM-OWNONE"}, {"publicId": public}})
		for name, call := range map[string]struct {
			handler func(*harness) error
		}{
			"create": {func(h *harness) error {
				_, err := h.call(t, handleZonesCreate, `{"workspace":"`+testWorkspace+`","name":"n","childContainer":`+string(child)+`}`)
				return err
			}},
			"update": {func(h *harness) error {
				_, err := h.call(t, handleZonesUpdate, `{"zone":"`+testZonePath+`","fingerprint":"1","name":"n","childContainer":`+string(child)+`}`)
				return err
			}},
		} {
			h := newHarness(t, "accounts/1000001", testClientID)
			h.google.api = accountContainers
			err := call.handler(h)
			if !isInvalidRequest(err) {
				t.Errorf("%s %q: err = %v, want invalid-request", name, public, err)
			}
			if got := writesOf(h.sent(t)); got != 0 {
				t.Errorf("%s %q: %d write requests, want none", name, public, got)
			}
		}
	}
}

func TestAFailedContainerLookupSendsNoWrite(t *testing.T) {
	h := newHarness(t, "accounts/1000001", testClientID)
	h.google.api = status(403, googleBody(403, "PERMISSION_DENIED", "X"))
	_, err := h.call(t, handleZonesCreate, `{"workspace":"`+testWorkspace+`","name":"n","childContainer":[{"publicId":"GTM-OWNONE"}]}`)
	if classOf(err) != provider.ClassPermission {
		t.Errorf("err = %v, want a permission error", err)
	}
	if got := writesOf(h.sent(t)); got != 0 {
		t.Errorf("%d write requests, want none", got)
	}
}

func TestContainerBoundConnectionRefusesEveryChildContainerWithoutRequest(t *testing.T) {
	h := newHarness(t, "accounts/1000001/containers/2000002", testClientID)
	h.google.api = accountContainers
	reachNothing(t, h, handleZonesCreate,
		`{"workspace":"`+testWorkspace+`","name":"n","childContainer":[{"publicId":"GTM-OWNONE"}]}`)
	reachNothing(t, h, handleZonesUpdate,
		`{"zone":"`+testZonePath+`","fingerprint":"1","name":"n","childContainer":[{"publicId":"GTM-OWNONE"}]}`)
	for _, args := range []string{
		`{"workspace":"` + testWorkspace + `","name":"n"}`,
		`{"workspace":"` + testWorkspace + `","name":"n","childContainer":[]}`,
	} {
		h := newHarness(t, "accounts/1000001/containers/2000002", testClientID)
		if _, err := h.call(t, handleZonesCreate, args); err != nil {
			t.Errorf("%s: %v", args, err)
		}
		if got := h.sent(t); len(got) != 1 || got[0].method != "POST" {
			t.Errorf("%s: sent = %+v", args, got)
		}
	}
}

func TestUnboundConnectionTakesAnyChildContainerWithoutLookup(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = accountContainers
	if _, err := h.call(t, handleZonesCreate,
		`{"workspace":"`+testWorkspace+`","name":"n","childContainer":[{"publicId":"GTM-ANYTHING"}]}`); err != nil {
		t.Fatal(err)
	}
	if got := h.sent(t); len(got) != 1 || got[0].method != "POST" {
		t.Errorf("sent = %+v", got)
	}
}

func TestEndlessContainerPagesAreRefusedWithoutAWrite(t *testing.T) {
	h := newHarness(t, "accounts/1000001", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"container":[],"nextPageToken":"again"}`, nil
	}
	_, err := h.call(t, handleZonesCreate, `{"workspace":"`+testWorkspace+`","name":"n","childContainer":[{"publicId":"GTM-OWNONE"}]}`)
	var failed *provider.Error
	if !errors.As(err, &failed) || writesOf(h.sent(t)) != 0 || len(h.google.seen()) > maxLookupPages {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}
