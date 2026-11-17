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

const testVersionWorkspace = testContainer + "/workspaces/3"

func storedVersion() string {
	return `{"path":"` + testVersionPath + `","accountId":"1000001","containerId":"2000002","containerVersionId":"7",` +
		`"name":"Example release","description":"Example notes","fingerprint":"1700000000007",` +
		`"tag":[{"tagId":"1","name":"example tag","type":"html"}],"unknownMember":"x"}`
}

func TestVersionWriteToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	effects := map[string]capability.Effect{versionsCreateID: capability.EffectCreate,
		versionsUpdateID: capability.EffectUpdate, versionsSetLatestID: capability.EffectUpdate,
		versionsDeleteID: capability.EffectDelete, versionsUndeleteID: capability.EffectUpdate}
	for id, effect := range effects {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{previewScope}) {
			t.Errorf("%s scopes = %v, want only %s", id, got, previewScope)
		}
		if d.Risk.Effect != effect || d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
		if slices.Contains(profiles["read"], id) {
			t.Errorf("%s is in the read profile", id)
		}
		listedOnly := id == versionsDeleteID
		if d.RequiresToolAllowList != listedOnly || slices.Contains(profiles["write"], id) == listedOnly {
			t.Errorf("%s: listed-only = %v, in write = %v", id, d.RequiresToolAllowList,
				slices.Contains(profiles["write"], id))
		}
		if !strings.HasPrefix(id, "google.tag-manager.versions.") {
			t.Errorf("%s has the wrong prefix", id)
		}
	}
	if toolOf(t, versionsCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("versions.create must be non-idempotent")
	}
	for _, d := range descriptors() {
		if strings.Contains(d.ID, "publish") && d.ID != versionsPublishID {
			t.Errorf("tool %s exists in this group", d.ID)
		}
	}
}

func TestVersionWriteConfirmationSummariesNameTheTarget(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(versionsCreateID)(json.RawMessage(`{"workspace":"` + testVersionWorkspace +
		`","name":"Example release","notes":"private-note-text"}`))
	if !strings.Contains(got, testVersionWorkspace) || !strings.Contains(got, "Example release") ||
		strings.Contains(got, "private-note-text") {
		t.Errorf("create summary %q", got)
	}
	for _, id := range []string{versionsUpdateID, versionsSetLatestID, versionsDeleteID, versionsUndeleteID} {
		got := reg.ConfirmationSummary(id)(json.RawMessage(`{"version":"` + testVersionPath +
			`","fingerprint":"1","name":"n"}`))
		if !strings.Contains(got, testVersionPath) {
			t.Errorf("%s summary %q lacks the version", id, got)
		}
	}
}

func TestVersionsCreateSendsNameAndNotesAndShowsTheVersion(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{"containerVersion":`+storedVersion()+`,"compilerError":false,`+
		`"syncStatus":{"syncError":false,"mergeConflict":false},"newWorkspacePath":"`+testContainer+`/workspaces/4"}`)
	args := `{"workspace":"` + testVersionWorkspace + `","name":"Example release","notes":"Example notes"}`
	if err := application.ValidateJSON(versionsCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses valid arguments: %v", err)
	}
	out, err := h.call(t, handleVersionsCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" ||
		sent[0].path != "/tagmanager/v2/"+testVersionWorkspace+":create_version" || sent[0].query != "" ||
		sent[0].contentType != "application/json" || sent[0].body != `{"name":"Example release","notes":"Example notes"}` {
		t.Fatalf("sent = %+v", sent)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(versionsCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	version, _ := out["containerVersion"].(map[string]any)
	if version["containerVersionId"] != "7" || version["fingerprint"] != "1700000000007" ||
		version["tag"] == nil || version["unknownMember"] != nil {
		t.Errorf("containerVersion = %v", version)
	}
	if out["compilerError"] != false || out["newWorkspacePath"] != testContainer+"/workspaces/4" || out["syncStatus"] == nil {
		t.Errorf("out = %v", out)
	}
}

func TestVersionsCreateWithoutNotesSendsAnEmptyObject(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{"containerVersion":`+storedVersion()+`}`)
	if _, err := h.call(t, handleVersionsCreate, `{"workspace":"`+testVersionWorkspace+`"}`); err != nil {
		t.Fatal(err)
	}
	if sent := h.sent(t); len(sent) != 1 || sent[0].body != `{}` {
		t.Fatalf("sent = %+v", sent)
	}
	h = newHarness(t, "", testClientID)
	h.google.api = status(200, `{"containerVersion":`+storedVersion()+`}`)
	if _, err := h.call(t, handleVersionsCreate, `{"workspace":"`+testVersionWorkspace+`","name":"Only a name"}`); err != nil {
		t.Fatal(err)
	}
	if sent := h.sent(t); len(sent) != 1 || sent[0].body != `{"name":"Only a name"}` {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestVersionsCreateReportsACompilerErrorAsSuch(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{"compilerError":true,"syncStatus":{"syncError":true,"mergeConflict":false}}`)
	out, err := h.call(t, handleVersionsCreate, `{"workspace":"`+testVersionWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	status, _ := out["syncStatus"].(map[string]any)
	if out["compilerError"] != true || out["containerVersion"] != nil || status["syncError"] != true {
		t.Errorf("out = %v", out)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(versionsCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
}

func TestVersionsCreateBoundsAnOversizedAnswerWithTheVersionForm(t *testing.T) {
	h := newHarness(t, "", testClientID)
	long := strings.Repeat("a", maxVersionTextRunes+10)
	h.google.api = status(200, `{"containerVersion":{"containerVersionId":"7","tag":[{"tagId":"1","name":"t","notes":"`+
		long+`"}]}}`)
	out, err := h.call(t, handleVersionsCreate, `{"workspace":"`+testVersionWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	version, _ := out["containerVersion"].(map[string]any)
	if version["truncated"] != true {
		t.Errorf("an oversized version is not marked as truncated: %v", version)
	}
}

func TestVersionsCreateAfterAnUnclearOutcomeIsNeverRepeated(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `{"error":{"message":"`+testEcho+`"}}`)
	_, err := h.call(t, handleVersionsCreate, `{"workspace":"`+testVersionWorkspace+`"}`)
	if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
		strings.Contains(err.Error(), testEcho) {
		t.Errorf("err = %v", err)
	}
	if len(h.google.seen()) != 1 {
		t.Errorf("%d requests, want exactly one", len(h.google.seen()))
	}
	h = newHarness(t, "", testClientID)
	h.google.fail = http.ErrHandlerTimeout
	if _, err := h.call(t, handleVersionsCreate, `{"workspace":"`+testVersionWorkspace+`"}`); err == nil ||
		!strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestVersionsUpdateRequiresTheFingerprintAndSendsTheDescription(t *testing.T) {
	h := newHarness(t, "", testClientID)
	reachNothing(t, h, handleVersionsUpdate, `{"version":"`+testVersionPath+`","name":"n"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleVersionsUpdate,
		`{"version":"`+testVersionPath+`","fingerprint":"","name":"n"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleVersionsUpdate,
		`{"version":"`+testVersionPath+`","fingerprint":"1"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleVersionsUpdate,
		`{"version":"`+testVersionPath+`","fingerprint":"1","name":"   "}`)
	if err := application.ValidateJSON(versionsUpdate.InputSchema,
		json.RawMessage(`{"version":"`+testVersionPath+`","name":"n"}`)); err == nil {
		t.Error("the schema accepts an update without a fingerprint")
	}

	h = newHarness(t, "", testClientID)
	h.google.api = status(200, storedVersion())
	args := `{"version":"` + testVersionPath + `","fingerprint":"1700000000007","name":"Renamed","notes":"New notes"}`
	if err := application.ValidateJSON(versionsUpdate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses valid arguments: %v", err)
	}
	out, err := h.call(t, handleVersionsUpdate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testVersionPath ||
		sent[0].query != "fingerprint=1700000000007" ||
		sent[0].body != `{"description":"New notes","name":"Renamed"}` {
		t.Fatalf("sent = %+v", sent)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(versionsUpdate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	if out["containerVersionId"] != "7" || out["tag"] == nil {
		t.Errorf("out = %v", out)
	}

	// Emptied notes are sent, notes that are left out are not.
	h = newHarness(t, "", testClientID)
	h.google.api = status(200, storedVersion())
	if _, err := h.call(t, handleVersionsUpdate, `{"version":"`+testVersionPath+`","fingerprint":"1","notes":""}`); err != nil {
		t.Fatal(err)
	}
	if sent := h.sent(t); len(sent) != 1 || sent[0].body != `{"description":""}` {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestVersionsSetLatestAndUndeletePostWithoutABodyAndShowTheVersion(t *testing.T) {
	for _, c := range []struct {
		handler capability.Handler
		suffix  string
		tool    capability.Descriptor
	}{{handleVersionsSetLatest, ":set_latest", versionsSetLatest}, {handleVersionsUndelete, ":undelete", versionsUndelete}} {
		h := newHarness(t, "", testClientID)
		h.google.api = status(200, storedVersion())
		out, err := h.call(t, c.handler, `{"version":"`+testVersionPath+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		sent := h.sent(t)
		if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testVersionPath+c.suffix ||
			sent[0].query != "" || sent[0].body != "" {
			t.Fatalf("%s: sent = %+v", c.suffix, sent)
		}
		encoded, _ := json.Marshal(out)
		if err := application.ValidateJSON(c.tool.OutputSchema, encoded); err != nil {
			t.Errorf("%s: result violates the output schema: %v", c.suffix, err)
		}
		if out["containerVersionId"] != "7" || out["fingerprint"] != "1700000000007" {
			t.Errorf("%s: out = %v", c.suffix, out)
		}
	}
}

func TestVersionsDeleteSendsDeleteAndAnswersWithThePath(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(204, ``)
	out, err := h.call(t, handleVersionsDelete, `{"version":"`+testVersionPath+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testVersionPath {
		t.Fatalf("sent = %+v", sent)
	}
	if out["version"] != testVersionPath || out["deleted"] != true {
		t.Errorf("out = %v", out)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(versionsDelete.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
}

func TestVersionsDeleteOfTheLiveVersionIsAnError(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(400, `{"error":{"code":400,"message":"`+testEcho+`","status":"FAILED_PRECONDITION"}}`)
	_, err := h.call(t, handleVersionsDelete, `{"version":"`+testVersionPath+`"}`)
	if err == nil || classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
		t.Errorf("err = %v", err)
	}
	if len(h.google.seen()) != 1 {
		t.Errorf("%d requests, want exactly one", len(h.google.seen()))
	}
}

func TestVersionWriteToolsRefuseAPathOutsideTheBindingBeforeAnyRequest(t *testing.T) {
	other := "accounts/1000001/containers/2000009"
	for _, target := range []string{"accounts/1000001/containers/2000002", "accounts/1000002"} {
		reachNothing(t, newHarness(t, target, testClientID), handleVersionsCreate,
			`{"workspace":"`+other+`/workspaces/3"}`)
		for _, handler := range []capability.Handler{handleVersionsUpdate, handleVersionsSetLatest, handleVersionsDelete,
			handleVersionsUndelete} {
			reachNothing(t, newHarness(t, target, testClientID), handler,
				`{"version":"`+other+`/versions/7","fingerprint":"1","name":"n"}`)
		}
	}
	// A version path is no workspace path and the other way round.
	reachNothing(t, newHarness(t, "", testClientID), handleVersionsCreate, `{"workspace":"`+testVersionPath+`"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleVersionsDelete, `{"version":"`+testVersionWorkspace+`"}`)
	for _, bad := range []string{testContainer + "/versions/07", testContainer + "/versions/7/x",
		testContainer + "/versions/7?a=b", testContainer + "/versions/"} {
		reachNothing(t, newHarness(t, "", testClientID), handleVersionsDelete, `{"version":"`+bad+`"}`)
	}
	// Inside the binding the call goes through.
	h := newHarness(t, "accounts/1000001/containers/2000002", testClientID)
	h.google.api = status(200, storedVersion())
	if _, err := h.call(t, handleVersionsSetLatest, `{"version":"`+testVersionPath+`"}`); err != nil {
		t.Errorf("a version inside the target is refused: %v", err)
	}
}

func TestVersionWriteInputIsClosedAndBounded(t *testing.T) {
	for _, c := range []struct {
		schema json.RawMessage
		args   string
	}{
		{versionsCreate.InputSchema, `{"workspace":"` + testVersionWorkspace + `","name":""}`},
		{versionsCreate.InputSchema, `{"workspace":"` + testVersionWorkspace + `","name":"` + strings.Repeat("a", 301) + `"}`},
		{versionsCreate.InputSchema, `{"workspace":"` + testVersionWorkspace + `","notes":"` + strings.Repeat("a", 2001) + `"}`},
		{versionsCreate.InputSchema, `{"workspace":"` + testVersionWorkspace + `","publish":true}`},
		{versionsCreate.InputSchema, `{"name":"n"}`},
		{versionsUpdate.InputSchema, `{"version":"` + testVersionPath + `","fingerprint":"1","deleted":true}`},
		{versionsDelete.InputSchema, `{"version":"` + testVersionPath + `","force":true}`},
		{versionsSetLatest.InputSchema, `{}`},
		{versionsUndelete.InputSchema, `{"version":"` + testVersionWorkspace + `"}`},
	} {
		if err := application.ValidateJSON(c.schema, json.RawMessage(c.args)); err == nil {
			t.Errorf("the schema accepts %s", c.args)
		}
	}
}

func TestVersionWriteToolsMapTheErrorFormsOfTheProvider(t *testing.T) {
	cases := map[string]capability.Handler{
		`{"workspace":"` + testVersionWorkspace + `"}`:                       handleVersionsCreate,
		`{"version":"` + testVersionPath + `","fingerprint":"1","name":"n"}`: handleVersionsUpdate,
		`{"version":"` + testVersionPath + `"}`:                              handleVersionsSetLatest,
	}
	for _, c := range []struct {
		status int
		class  provider.Class
	}{{401, provider.ClassAuth}, {403, provider.ClassPermission}, {429, provider.ClassRateLimited},
		{500, provider.ClassInvalidResponse}} {
		for args, handler := range cases {
			h := newHarness(t, "", testClientID)
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return c.status, `{"error":{"message":"` + testEcho + `"}}`, nil
			}
			_, err := h.call(t, handler, args)
			if classOf(err) != c.class || strings.Contains(err.Error(), testEcho) {
				t.Errorf("status %d: err = %v", c.status, err)
			}
			if len(h.google.seen()) != 1 {
				t.Errorf("status %d: %d requests, want exactly one", c.status, len(h.google.seen()))
			}
		}
	}
}

func TestAReadOnlyConsentCannotChangeAVersion(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleVersionsDelete, `{"version":"`+testVersionPath+`"}`)
	if err == nil || !strings.Contains(err.Error(), previewScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing scope and no request", err, len(h.google.seen()))
	}
}
