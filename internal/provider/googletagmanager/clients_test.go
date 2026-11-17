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

const testClientPath = testWorkspace + "/clients/5"

func TestClientToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{clientsListID, clientsGetID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	changes := map[string]capability.Effect{clientsCreateID: capability.EffectCreate,
		clientsUpdateID: capability.EffectUpdate, clientsDeleteID: capability.EffectDelete,
		clientsRevertID: capability.EffectUpdate}
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
		if d.RequiresToolAllowList != (id == clientsRevertID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
	}
	if toolOf(t, clientsCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("clients.create must be non-idempotent")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{clientsListID, clientsGetID} {
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	for _, id := range []string{clientsCreateID, clientsUpdateID, clientsDeleteID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if slices.Contains(profiles["read"], clientsRevertID) || slices.Contains(profiles["write"], clientsRevertID) {
		t.Error("clients.revert is in a profile")
	}
}

func TestClientDescriptionsNameTheEffect(t *testing.T) {
	for _, id := range []string{clientsCreateID, clientsUpdateID, clientsDeleteID, clientsRevertID} {
		d := toolOf(t, id)
		for _, want := range []string{"server container", "published container version", "no tool of this provider"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", id, want)
			}
		}
	}
}

func TestConfirmationSummaryNamesClientTypeAndWorkspace(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(clientsCreateID)(json.RawMessage(
		`{"workspace":"` + testWorkspace + `","name":"Example client","type":"example-type","priority":10,` +
			`"parameter":[{"type":"template","key":"k","value":"synthetic-parameter-value"}]}`))
	for _, want := range []string{"Example client", "example-type", testWorkspace, "client"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "synthetic-parameter-value") {
		t.Errorf("summary %q shows a parameter value", got)
	}
	for _, id := range []string{clientsDeleteID, clientsRevertID} {
		if got := reg.ConfirmationSummary(id)(json.RawMessage(`{"client":"` + testClientPath + `"}`)); !strings.Contains(got, testClientPath) {
			t.Errorf("%s summary %q lacks the client", id, got)
		}
	}
}

// allMembersClient sets every member a caller can set.
const allMembersClient = `{"name":"Example client","type":"example-type","notes":"n","parentFolderId":"7","priority":10,` +
	`"parameter":[{"type":"boolean","key":"example-key","value":"true"},` +
	`{"type":"list","key":"l","list":[{"type":"template","value":"a"}]}]}`

func TestClientsCreateSendsEveryMember(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := `{"path":"` + testClientPath + `","accountId":"1000001","containerId":"2000002","workspaceId":"3",` +
		`"clientId":"5","fingerprint":"1700000000001","tagManagerUrl":"https://tagmanager.google.com/x",` + allMembersClient[1:]
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, stored, nil }
	args := `{"workspace":"` + testWorkspace + `",` + allMembersClient[1:]
	if err := application.ValidateJSON(clientsCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid client: %v", err)
	}
	out, err := h.call(t, handleClientsCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/clients" ||
		sent[0].contentType != "application/json" || sent[0].query != "" {
		t.Fatalf("sent = %+v", sent)
	}
	var got, want map[string]any
	if err := json.Unmarshal([]byte(sent[0].body), &got); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(allMembersClient), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %s\nwant %s", sent[0].body, allMembersClient)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(clientsCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	var back map[string]any
	_ = json.Unmarshal([]byte(stored), &back)
	if !reflect.DeepEqual(out, back) {
		t.Errorf("result differs from the stored client: %s", encoded)
	}
}

func TestClientsCreateNeverSendsOutputOnlyMembersAndKeepsAZeroPriority(t *testing.T) {
	h := newHarness(t, "", testClientID)
	if _, err := h.call(t, handleClientsCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"t"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.call(t, handleClientsCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"t","priority":0}`); err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if sent[0].body != `{"name":"n","type":"t"}` || sent[1].body != `{"name":"n","type":"t","priority":0}` {
		t.Errorf("bodies = %s, %s", sent[0].body, sent[1].body)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleClientsCreate, `{"workspace":"`+testWorkspace+`","type":"t"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleClientsCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`)
}

func TestClientPriorityIsAnIntegerInTheSchema(t *testing.T) {
	prefix := `{"workspace":"` + testWorkspace + `","name":"n","type":"t","priority":`
	for _, ok := range []string{`-5}`, `0}`, `2147483647}`} {
		if err := application.ValidateJSON(clientsCreate.InputSchema, json.RawMessage(prefix+ok)); err != nil {
			t.Errorf("schema refuses priority %s: %v", ok, err)
		}
	}
	for _, bad := range []string{`"5"}`, `1.5}`, `2147483648}`, `{"type":"integer","value":"5"}}`} {
		if err := application.ValidateJSON(clientsCreate.InputSchema, json.RawMessage(prefix+bad)); err == nil {
			t.Errorf("schema accepts priority %s", bad)
		}
	}
}

func TestClientsUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"client":"` + testClientPath + `","name":"n","type":"t"}`,
		`{"client":"` + testClientPath + `","fingerprint":"","name":"n","type":"t"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleClientsUpdate, args)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleClientsRevert, `{"client":"`+testClientPath+`"}`)
	if err := application.ValidateJSON(clientsUpdate.InputSchema,
		json.RawMessage(`{"client":"`+testClientPath+`","name":"n","type":"t"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
	if err := application.ValidateJSON(clientsRevert.InputSchema, json.RawMessage(`{"client":"`+testClientPath+`"}`)); err == nil {
		t.Error("the schema accepts a revert without fingerprint")
	}
}

func TestClientsUpdatePutsTheWholeClientWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testClientPath + `","fingerprint":"1700000000002","name":"n","type":"t","priority":3}`, nil
	}
	out, err := h.call(t, handleClientsUpdate, `{"client":"`+testClientPath+`","fingerprint":"1700000000001","name":"n","type":"t","priority":3,"notes":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testClientPath ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].body != `{"name":"n","type":"t","priority":3,"notes":"x"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" || out["priority"] != float64(3) {
		t.Errorf("result = %v", out)
	}
}

func TestStaleClientFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleClientsUpdate, `{"client":"`+testClientPath+`","fingerprint":"1","name":"n","type":"t"}`)
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

// The answer of Google for a client call on a web container is not verified; it arrives in the established
// form of an unmapped status.
func TestAWebContainerErrorArrivesAsAnUnmappedAnswerWithTheOriginal(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 400, googleBody(400, "INVALID_ARGUMENT", "WEB_CONTAINER"), nil
	}
	for name, call := range map[string]func() error{
		"list": func() error {
			_, err := h.call(t, handleClientsList, `{"workspace":"`+testWorkspace+`"}`)
			return err
		},
		"create": func() error {
			_, err := h.call(t, handleClientsCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"t"}`)
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

func TestClientsDeleteAndRevert(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "DELETE" {
			return 204, "", nil
		}
		return 200, `{"client":{"path":"` + testClientPath + `","name":"base","type":"t","fingerprint":"9"}}`, nil
	}
	out, err := h.call(t, handleClientsDelete, `{"client":"`+testClientPath+`"}`)
	if err != nil || out["deleted"] != true || out["client"] != testClientPath {
		t.Fatalf("delete = %v, %v", out, err)
	}
	out, err = h.call(t, handleClientsRevert, `{"client":"`+testClientPath+`","fingerprint":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if base, _ := out["client"].(map[string]any); base["name"] != "base" {
		t.Errorf("revert = %v", out)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if out, err = h.call(t, handleClientsRevert, `{"client":"`+testClientPath+`","fingerprint":"5"}`); err != nil || len(out) != 0 {
		t.Errorf("revert of a client the base version lacks = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 3 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testClientPath || sent[0].body != "" ||
		sent[1].method != "POST" || sent[1].path != "/tagmanager/v2/"+testClientPath+":revert" ||
		sent[1].query != "fingerprint=5" || sent[1].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestClientsOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
	}{
		{"accounts/1000001/containers/2000002", []string{"accounts/1000001/containers/2000009/workspaces/3/clients/5",
			"accounts/1000009/containers/2000002/workspaces/3/clients/5"}},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/workspaces/3/clients/5"}},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			workspace, _, _ := strings.Cut(path, "/clients/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleClientsGet, `{"client":"`+path+`"}`)
			reachNothing(t, h, handleClientsList, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleClientsCreate, `{"workspace":"`+workspace+`","name":"n","type":"t"}`)
			reachNothing(t, h, handleClientsUpdate, `{"client":"`+path+`","fingerprint":"1","name":"n","type":"t"}`)
			reachNothing(t, h, handleClientsDelete, `{"client":"`+path+`"}`)
			reachNothing(t, h, handleClientsRevert, `{"client":"`+path+`","fingerprint":"1"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleClientsDelete, `{"client":"`+testClientPath+`"}`); err != nil {
			t.Errorf("%s: a client inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/workspaces/3/clients/05", "accounts/1/containers/2/workspaces/3",
		"accounts/1/containers/2/workspaces/3/clients/5/x", "accounts/1/containers/2/workspaces/3/clients/5?a=b",
		"accounts/1/containers/2/workspaces/3/tags/5"} {
		reachNothing(t, newHarness(t, "", testClientID), handleClientsGet, `{"client":"`+bad+`"}`)
	}
}

func TestClientsListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"client":[{"clientId":"1","name":"a","type":"t","priority":1}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"client":[{"clientId":"2","name":"b","type":"t"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleClientsList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["clients"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleClientsList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["clients"].([]any)[0].(map[string]any)["clientId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleClientsList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testWorkspace+"/clients" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
}

func TestClientValuesAreNeverCutAndGoBackUnchangedThroughAnUpdate(t *testing.T) {
	value := strings.Repeat("synthetic-value-ÄÖ \"q\" \\ {{y}}\n", 20000)
	stored := map[string]any{"path": testClientPath, "name": strings.Repeat("n", 5000), "notes": strings.Repeat("o", 70000),
		"type": "example-type", "fingerprint": "1", "priority": float64(7),
		"parameter": []any{map[string]any{"type": "template", "key": "k", "value": value}}}
	raw, _ := json.Marshal(stored)
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	out, err := h.call(t, handleClientsGet, `{"client":"`+testClientPath+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(out, want) {
		t.Fatal("the client read differs from the stored client")
	}
	args := map[string]any{"client": testClientPath, "fingerprint": "1"}
	keys := []string{"name", "notes", "type", "parameter", "priority"}
	for _, k := range keys {
		args[k] = out[k]
	}
	encoded, _ := json.Marshal(args)
	if err := application.ValidateJSON(clientsUpdate.InputSchema, encoded); err != nil {
		t.Fatalf("the schema refuses what a read returned: %v", err)
	}
	if _, err := h.call(t, handleClientsUpdate, string(encoded)); err != nil {
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

func TestOversizedClientAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	big := `{"name":"n","parameter":[{"type":"template","value":"` + strings.Repeat("a", maxResponse) + `"}]}`
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, big, nil }
	if _, err := h.call(t, handleClientsGet, `{"client":"`+testClientPath+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
	many := strings.Repeat(`{"type":"template"},`, maxParamNodes) + `{"type":"template"}`
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"parameter":[` + many + `]}`, nil
	}
	if _, err := h.call(t, handleClientsGet, `{"client":"`+testClientPath+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response for too many entries", err)
	}
}

func TestClientInputBoundsAreRefusedBeforeAnyRequest(t *testing.T) {
	nest := `{"type":"template","value":"x"}`
	for i := 0; i < maxParamDepth; i++ {
		nest = `{"type":"list","list":[` + nest + `]}`
	}
	args := `{"workspace":"` + testWorkspace + `","name":"n","type":"t","parameter":[` + nest + `]}`
	reachNothing(t, newHarness(t, "", testClientID), handleClientsCreate, args)
	prefix := `{"workspace":"` + testWorkspace + `","name":"n","type":"t",`
	for _, bad := range []string{
		prefix + `"parameter":"x"}`,
		prefix + `"parameter":[{"type":"number"}]}`,
		prefix + `"parentFolderId":7}`,
		`{"workspace":"` + testWorkspace + `","name":"","type":"t"}`,
		prefix + `"unknown":1}`,
	} {
		if err := application.ValidateJSON(clientsCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
}

func TestAClientChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleClientsCreate, `{"workspace":"`+testWorkspace+`","name":"n","type":"t"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeAClient(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleClientsDelete, `{"client":"`+testClientPath+`"}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing edit scope and no request", err, len(h.google.seen()))
	}
	if _, err := h.call(t, handleClientsGet, `{"client":"`+testClientPath+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
}
