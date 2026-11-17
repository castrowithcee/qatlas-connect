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

const testGtagConfigPath = testWorkspace + "/gtag_config/5"

// storedGtagConfig is a configuration as Google answers it, with one destination ID and one other parameter.
const storedGtagConfig = `{"path":"` + testGtagConfigPath + `","accountId":"1000001","containerId":"2000002",` +
	`"workspaceId":"3","gtagConfigId":"5","fingerprint":"1700000000001","tagManagerUrl":"https://tagmanager.google.com/x",` +
	`"type":"googtag","parameter":[{"type":"template","key":"tagId","value":"G-EXAMPLE1"},` +
	`{"type":"boolean","key":"example-flag","value":"true"}]}`

const storedParameters = `[{"type":"template","key":"tagId","value":"G-EXAMPLE1"},` +
	`{"type":"boolean","key":"example-flag","value":"true"}]`

func TestGtagConfigToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{gtagConfigsListID, gtagConfigsGetID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	changes := map[string]capability.Effect{gtagConfigsCreateID: capability.EffectCreate,
		gtagConfigsUpdateID: capability.EffectUpdate, gtagConfigsDeleteID: capability.EffectDelete}
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
		if d.RequiresToolAllowList {
			t.Errorf("%s is listed-only", id)
		}
	}
	if toolOf(t, gtagConfigsCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("gtag-configs.create must be non-idempotent")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{gtagConfigsListID, gtagConfigsGetID} {
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	for _, id := range []string{gtagConfigsCreateID, gtagConfigsUpdateID, gtagConfigsDeleteID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
}

func TestNoToolLinksDestinationsAndNoGtagConfigRevertExists(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{gtagConfigsListID, gtagConfigsGetID, gtagConfigsCreateID, gtagConfigsUpdateID, gtagConfigsDeleteID}
	var got []string
	for _, d := range reg.Provider(Provider) {
		if strings.Contains(d.ID, "destination") && d.ID != destinationsListID && d.ID != destinationsGetID {
			t.Errorf("%s links a destination", d.ID)
		}
		if strings.Contains(d.ID, ".gtag-configs.") {
			got = append(got, d.ID)
		}
	}
	slices.Sort(got)
	slices.Sort(wantIDs)
	if !slices.Equal(got, wantIDs) {
		t.Errorf("gtag-config tools = %v, want %v", got, wantIDs)
	}
}

func TestGtagConfigDescriptionsNameTheEffectAndTheUpdateCheck(t *testing.T) {
	for _, id := range []string{gtagConfigsCreateID, gtagConfigsUpdateID, gtagConfigsDeleteID} {
		d := toolOf(t, id)
		for _, want := range []string{"measurement IDs", "destination of the measurement data", "workspace only",
			"published container version", "versions.publish"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", id, want)
			}
		}
	}
	update := toolOf(t, gtagConfigsUpdateID)
	for _, want := range []string{"previous_parameter", "ends in Id or ID", "refuses the call"} {
		if !strings.Contains(update.Description, want) {
			t.Errorf("update description lacks %q", want)
		}
	}
	found := false
	for _, a := range update.Arguments {
		if a.Name == "previous_parameter" && a.Required && strings.Contains(a.Description, "Id or ID") {
			found = true
		}
	}
	if !found {
		t.Error("previous_parameter is not a documented required argument that names the key rule")
	}
}

func TestConfirmationShowsOldAndNewDestinationID(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	update := reg.ConfirmationSummary(gtagConfigsUpdateID)
	args := func(previous, current string) json.RawMessage {
		return json.RawMessage(`{"gtag_config":"` + testGtagConfigPath + `","fingerprint":"1","type":"googtag",` +
			`"previous_parameter":` + previous + `,"parameter":` + current + `}`)
	}
	got := update(args(storedParameters, `[{"type":"template","key":"tagId","value":"G-EXAMPLE2"},`+
		`{"type":"boolean","key":"example-flag","value":"false"}]`))
	for _, want := range []string{`tagId: "G-EXAMPLE1" -> "G-EXAMPLE2"`, testGtagConfigPath, "googtag"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "example-flag") || strings.Contains(got, "false") {
		t.Errorf("summary %q shows another parameter", got)
	}

	// Unchanged IDs are said to be unchanged, and other values never show.
	got = update(args(storedParameters, `[{"type":"template","key":"tagId","value":"G-EXAMPLE1"},`+
		`{"type":"template","key":"other","value":"synthetic-other-value"}]`))
	if !strings.Contains(got, "no destination ID changes") || strings.Contains(got, "G-EXAMPLE1") ||
		strings.Contains(got, "synthetic-other-value") {
		t.Errorf("summary %q", got)
	}

	// Nested, added, and removed IDs; only template parameters whose key ends in Id or ID count.
	previous := `[{"type":"list","key":"destinations","list":[{"type":"map","map":[` +
		`{"type":"template","key":"accountID","value":"AW-EXAMPLE1"}]}]},` +
		`{"type":"template","key":"removedId","value":"R-1"},{"type":"integer","key":"countId","value":"1"},` +
		`{"type":"template","key":"label","value":"synthetic-label"}]`
	current := `[{"type":"list","key":"destinations","list":[{"type":"map","map":[` +
		`{"type":"template","key":"accountID","value":"AW-EXAMPLE2"}]}]},` +
		`{"type":"template","key":"addedId","value":"A-1"},{"type":"integer","key":"countId","value":"2"},` +
		`{"type":"template","key":"label","value":"other-label"}]`
	got = update(args(previous, current))
	for _, want := range []string{`destinations/[0]/accountID: "AW-EXAMPLE1" -> "AW-EXAMPLE2"`,
		`addedId: (none) -> "A-1"`, `removedId: "R-1" -> (none)`} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	for _, never := range []string{"countId", "label", "synthetic-label", "other-label"} {
		if strings.Contains(got, never) {
			t.Errorf("summary %q shows %q", got, never)
		}
	}

	create := reg.ConfirmationSummary(gtagConfigsCreateID)(json.RawMessage(`{"workspace":"` + testWorkspace +
		`","type":"googtag","parameter":[{"type":"template","key":"tagId","value":"G-EXAMPLE1"},` +
		`{"type":"template","key":"other","value":"synthetic-other-value"}]}`))
	if !strings.Contains(create, `tagId: "G-EXAMPLE1"`) || !strings.Contains(create, testWorkspace) ||
		strings.Contains(create, "synthetic-other-value") {
		t.Errorf("create summary %q", create)
	}
	del := reg.ConfirmationSummary(gtagConfigsDeleteID)(json.RawMessage(`{"gtag_config":"` + testGtagConfigPath + `"}`))
	if !strings.Contains(del, testGtagConfigPath) || !strings.HasPrefix(del, "Delete") {
		t.Errorf("delete summary %q", del)
	}
}

func TestGtagConfigsCreateSendsEveryMember(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, storedGtagConfig, nil }
	args := `{"workspace":"` + testWorkspace + `","type":"googtag","parameter":` + storedParameters + `}`
	if err := application.ValidateJSON(gtagConfigsCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid configuration: %v", err)
	}
	out, err := h.call(t, handleGtagConfigsCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/gtag_config" ||
		sent[0].contentType != "application/json" || sent[0].query != "" {
		t.Fatalf("sent = %+v", sent)
	}
	var got, want map[string]any
	_ = json.Unmarshal([]byte(sent[0].body), &got)
	_ = json.Unmarshal([]byte(`{"type":"googtag","parameter":`+storedParameters+`}`), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %s", sent[0].body)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(gtagConfigsCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	var back map[string]any
	_ = json.Unmarshal([]byte(storedGtagConfig), &back)
	if !reflect.DeepEqual(out, back) {
		t.Errorf("result differs from the stored configuration: %s", encoded)
	}
}

func TestGtagConfigsCreateNeedsATypeBeforeAnyRequest(t *testing.T) {
	reachNothing(t, newHarness(t, "", testClientID), handleGtagConfigsCreate, `{"workspace":"`+testWorkspace+`"}`)
	h := newHarness(t, "", testClientID)
	if _, err := h.call(t, handleGtagConfigsCreate, `{"workspace":"`+testWorkspace+`","type":"googtag"}`); err != nil {
		t.Fatal(err)
	}
	if got := h.sent(t)[0].body; got != `{"type":"googtag"}` {
		t.Errorf("body = %s", got)
	}
}

func TestGtagConfigsUpdateWithoutFingerprintOrPreviousParameterIsRefusedBeforeIO(t *testing.T) {
	base := `"gtag_config":"` + testGtagConfigPath + `","type":"googtag"`
	for _, args := range []string{
		`{` + base + `,"previous_parameter":[]}`,
		`{` + base + `,"fingerprint":"","previous_parameter":[]}`,
		`{` + base + `,"fingerprint":"1"}`,
		`{` + base + `,"fingerprint":"1","previous_parameter":null}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleGtagConfigsUpdate, args)
		if err := application.ValidateJSON(gtagConfigsUpdate.InputSchema, json.RawMessage(args)); err == nil {
			t.Errorf("the schema accepts %s", args)
		}
	}
	ok := `{` + base + `,"fingerprint":"1","previous_parameter":[]}`
	if err := application.ValidateJSON(gtagConfigsUpdate.InputSchema, json.RawMessage(ok)); err != nil {
		t.Errorf("the schema refuses an empty previous_parameter: %v", err)
	}
}

func TestGtagConfigsUpdateReadsFirstThenPutsTheWholeConfigurationWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "GET" {
			return 200, storedGtagConfig, nil
		}
		return 200, `{"path":"` + testGtagConfigPath + `","fingerprint":"1700000000002","type":"googtag"}`, nil
	}
	args := `{"gtag_config":"` + testGtagConfigPath + `","fingerprint":"1700000000001","type":"googtag",` +
		`"previous_parameter":` + storedParameters + `,"parameter":[{"type":"template","key":"tagId","value":"G-EXAMPLE2"}]}`
	if err := application.ValidateJSON(gtagConfigsUpdate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatal(err)
	}
	out, err := h.call(t, handleGtagConfigsUpdate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 2 || sent[0].method != "GET" || sent[0].path != "/tagmanager/v2/"+testGtagConfigPath ||
		sent[1].method != "PUT" || sent[1].path != "/tagmanager/v2/"+testGtagConfigPath ||
		sent[1].query != "fingerprint=1700000000001" ||
		sent[1].body != `{"type":"googtag","parameter":[{"type":"template","key":"tagId","value":"G-EXAMPLE2"}]}` {
		t.Fatalf("sent = %+v", sent)
	}
	if strings.Contains(sent[1].body, "previous") {
		t.Error("previous_parameter was sent to Google")
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestAPreviousParameterThatIsNotTheStoredListIsRefusedWithoutAWrite(t *testing.T) {
	for name, previous := range map[string]string{
		"other value":  `[{"type":"template","key":"tagId","value":"G-EXAMPLE9"},{"type":"boolean","key":"example-flag","value":"true"}]`,
		"other order":  `[{"type":"boolean","key":"example-flag","value":"true"},{"type":"template","key":"tagId","value":"G-EXAMPLE1"}]`,
		"missing item": `[{"type":"template","key":"tagId","value":"G-EXAMPLE1"}]`,
		"extra item":   `[{"type":"template","key":"tagId","value":"G-EXAMPLE1"},{"type":"boolean","key":"example-flag","value":"true"},{"type":"template","key":"x"}]`,
		"empty":        `[]`,
	} {
		h := newHarness(t, "", testClientID)
		h.google.api = status(200, storedGtagConfig)
		_, err := h.call(t, handleGtagConfigsUpdate, `{"gtag_config":"`+testGtagConfigPath+`","fingerprint":"1","type":"googtag",`+
			`"previous_parameter":`+previous+`,"parameter":[]}`)
		if !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want invalid-request", name, err)
		}
		for _, req := range h.sent(t) {
			if req.method != "GET" {
				t.Errorf("%s: a %s request was sent", name, req.method)
			}
		}
		if err != nil && (strings.Contains(err.Error(), "G-EXAMPLE") || strings.Contains(err.Error(), testEcho)) {
			t.Errorf("%s: the message quotes a value: %v", name, err)
		}
	}
}

func TestAPreviousParameterEqualsTheStoredListIgnoringEmptyNestedLists(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "GET" {
			return 200, `{"type":"googtag"}`, nil
		}
		return 200, `{}`, nil
	}
	// An absent list equals an empty one, and a stored empty nested list equals an absent one.
	if _, err := h.call(t, handleGtagConfigsUpdate, `{"gtag_config":"`+testGtagConfigPath+`","fingerprint":"1","type":"googtag","previous_parameter":[]}`); err != nil {
		t.Fatal(err)
	}
	h = newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "GET" {
			return 200, `{"type":"googtag","parameter":[{"type":"list","key":"l","list":[]}]}`, nil
		}
		return 200, `{}`, nil
	}
	if _, err := h.call(t, handleGtagConfigsUpdate, `{"gtag_config":"`+testGtagConfigPath+`","fingerprint":"1","type":"googtag",`+
		`"previous_parameter":[{"type":"list","key":"l"}]}`); err != nil {
		t.Fatal(err)
	}
}

func TestStaleGtagConfigFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(req *http.Request) (int, string, http.Header) {
			if req.Method == "GET" {
				return 200, storedGtagConfig, nil
			}
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleGtagConfigsUpdate, `{"gtag_config":"`+testGtagConfigPath+`","fingerprint":"1","type":"googtag",`+
			`"previous_parameter":`+storedParameters+`}`)
		if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
			t.Fatalf("status %d: err = %v", code, err)
		}
		if strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("status %d is a definite refusal: %v", code, err)
		}
		if got := len(h.google.seen()); got != 2 {
			t.Errorf("status %d: requests = %d, want the read and one write", code, got)
		}
	}
}

func TestAFailedReadBeforeAnUpdateSendsNoWrite(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(404, googleBody(404, "NOT_FOUND", "NOT_FOUND"))
	_, err := h.call(t, handleGtagConfigsUpdate, `{"gtag_config":"`+testGtagConfigPath+`","fingerprint":"1","type":"googtag","previous_parameter":[]}`)
	if err == nil || classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
		t.Fatalf("err = %v", err)
	}
	var perr *provider.Error
	if !errors.As(err, &perr) || perr.Original == nil {
		t.Errorf("the original of Google's answer is missing: %v", err)
	}
	if sent := h.sent(t); len(sent) != 1 || sent[0].method != "GET" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestGtagConfigsDeleteNeedsConfirmationAndSendsADelete(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(204, "")
	out, err := h.call(t, handleGtagConfigsDelete, `{"gtag_config":"`+testGtagConfigPath+`"}`)
	if err != nil || out["deleted"] != true || out["gtag_config"] != testGtagConfigPath {
		t.Fatalf("delete = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testGtagConfigPath || sent[0].body != "" {
		t.Errorf("sent = %+v", sent)
	}
	d := toolOf(t, gtagConfigsDeleteID)
	if d.Risk.Effect != capability.EffectDelete || d.Risk.Confirmation != capability.ConfirmationRequired {
		t.Errorf("delete risk = %+v", d.Risk)
	}
}

func TestGtagConfigsOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
	}{
		{"accounts/1000001/containers/2000002", []string{"accounts/1000001/containers/2000009/workspaces/3/gtag_config/5",
			"accounts/1000009/containers/2000002/workspaces/3/gtag_config/5"}},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/workspaces/3/gtag_config/5"}},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			workspace, _, _ := strings.Cut(path, "/gtag_config/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleGtagConfigsGet, `{"gtag_config":"`+path+`"}`)
			reachNothing(t, h, handleGtagConfigsList, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleGtagConfigsCreate, `{"workspace":"`+workspace+`","type":"t"}`)
			reachNothing(t, h, handleGtagConfigsUpdate, `{"gtag_config":"`+path+`","fingerprint":"1","type":"t","previous_parameter":[]}`)
			reachNothing(t, h, handleGtagConfigsDelete, `{"gtag_config":"`+path+`"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleGtagConfigsDelete, `{"gtag_config":"`+testGtagConfigPath+`"}`); err != nil {
			t.Errorf("%s: a configuration inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/workspaces/3/gtag_config/05", "accounts/1/containers/2/workspaces/3",
		"accounts/1/containers/2/workspaces/3/gtag_config/5/x", "accounts/1/containers/2/workspaces/3/gtag_config/5?a=b",
		"accounts/1/containers/2/workspaces/3/gtagConfig/5", "accounts/1/containers/2/workspaces/3/tags/5"} {
		reachNothing(t, newHarness(t, "", testClientID), handleGtagConfigsGet, `{"gtag_config":"`+bad+`"}`)
	}
}

func TestGtagConfigsListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"gtagConfig":[{"gtagConfigId":"1","type":"googtag"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"gtagConfig":[{"gtagConfigId":"2","type":"googtag"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleGtagConfigsList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["gtagConfigs"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleGtagConfigsList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["gtagConfigs"].([]any)[0].(map[string]any)["gtagConfigId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleGtagConfigsList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testWorkspace+"/gtag_config" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
	h.google.api = status(200, `{}`)
	empty, err := h.call(t, handleGtagConfigsList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil || empty["gtagConfigs"] == nil {
		t.Errorf("an empty answer must be an empty list: %v, %v", empty, err)
	}
}

func TestGtagConfigValuesAreNeverCutAndGoBackUnchangedThroughAnUpdate(t *testing.T) {
	value := strings.Repeat("synthetic-value-ÄÖ \"q\" \\ {{y}}\n", 20000)
	stored := map[string]any{"path": testGtagConfigPath, "type": "googtag", "fingerprint": "1",
		"parameter": []any{map[string]any{"type": "template", "key": "k", "value": value}}}
	raw, _ := json.Marshal(stored)
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	out, err := h.call(t, handleGtagConfigsGet, `{"gtag_config":"`+testGtagConfigPath+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(out, want) {
		t.Fatal("the configuration read differs from the stored one")
	}
	args := map[string]any{"gtag_config": testGtagConfigPath, "fingerprint": "1", "type": out["type"],
		"parameter": out["parameter"], "previous_parameter": out["parameter"]}
	encoded, _ := json.Marshal(args)
	if err := application.ValidateJSON(gtagConfigsUpdate.InputSchema, encoded); err != nil {
		t.Fatalf("the schema refuses what a read returned: %v", err)
	}
	if _, err := h.call(t, handleGtagConfigsUpdate, string(encoded)); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(h.sent(t)[2].body), &sent)
	if !reflect.DeepEqual(sent["parameter"], want["parameter"]) || sent["type"] != "googtag" {
		t.Error("the parameters were changed on the way")
	}
}

func TestOversizedGtagConfigAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	big := `{"type":"t","parameter":[{"type":"template","value":"` + strings.Repeat("a", maxResponse) + `"}]}`
	h.google.api = status(200, big)
	if _, err := h.call(t, handleGtagConfigsGet, `{"gtag_config":"`+testGtagConfigPath+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
	many := strings.Repeat(`{"type":"template"},`, maxParamNodes) + `{"type":"template"}`
	h.google.api = status(200, `{"parameter":[`+many+`]}`)
	if _, err := h.call(t, handleGtagConfigsGet, `{"gtag_config":"`+testGtagConfigPath+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response for too many entries", err)
	}
	h.google.api = status(200, `{"gtagConfig":[{"parameter":[`+many+`]}]}`)
	if _, err := h.call(t, handleGtagConfigsList, `{"workspace":"`+testWorkspace+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("list: err = %v, want invalid-provider-response for too many entries", err)
	}
}

func TestGtagConfigInputBoundsAreRefusedBeforeAnyRequest(t *testing.T) {
	nest := `{"type":"template","value":"x"}`
	for i := 0; i < maxParamDepth; i++ {
		nest = `{"type":"list","list":[` + nest + `]}`
	}
	reachNothing(t, newHarness(t, "", testClientID), handleGtagConfigsCreate,
		`{"workspace":"`+testWorkspace+`","type":"t","parameter":[`+nest+`]}`)
	reachNothing(t, newHarness(t, "", testClientID), handleGtagConfigsUpdate,
		`{"gtag_config":"`+testGtagConfigPath+`","fingerprint":"1","type":"t","previous_parameter":[`+nest+`]}`)
	prefix := `{"workspace":"` + testWorkspace + `","type":"t",`
	for _, bad := range []string{
		prefix + `"parameter":"x"}`,
		prefix + `"parameter":[{"type":"number"}]}`,
		prefix + `"name":"n"}`,
		prefix + `"notes":"n"}`,
		prefix + `"priority":1}`,
		`{"workspace":"` + testWorkspace + `","type":""}`,
	} {
		if err := application.ValidateJSON(gtagConfigsCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
}

func TestAGtagConfigChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleGtagConfigsCreate, `{"workspace":"`+testWorkspace+`","type":"t"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeAGtagConfig(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	for name, call := range map[string]func() error{
		"delete": func() error {
			_, err := h.call(t, handleGtagConfigsDelete, `{"gtag_config":"`+testGtagConfigPath+`"}`)
			return err
		},
		"update": func() error {
			_, err := h.call(t, handleGtagConfigsUpdate, `{"gtag_config":"`+testGtagConfigPath+`","fingerprint":"1","type":"t","previous_parameter":[]}`)
			return err
		},
	} {
		err := call()
		if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
			t.Errorf("%s: err = %v, requests = %d, want the missing edit scope and no request", name, err, len(h.google.seen()))
		}
	}
	if _, err := h.call(t, handleGtagConfigsGet, `{"gtag_config":"`+testGtagConfigPath+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
}
