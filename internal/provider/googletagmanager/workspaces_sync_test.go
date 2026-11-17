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

func TestWorkspaceSyncToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	tools := map[string]struct {
		scope      string
		idempotent capability.Idempotency
	}{
		workspacesSyncID:            {editScope, capability.IdempotencyIdempotent},
		workspacesResolveConflictID: {editScope, capability.IdempotencyIdempotent},
		workspacesQuickPreviewID:    {previewScope, capability.IdempotencyNonIdempotent},
	}
	for id, want := range tools {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{want.scope}) {
			t.Errorf("%s scopes = %v, want only %s", id, got, want.scope)
		}
		if d.Risk.Effect != capability.EffectUpdate || d.Risk.Idempotency != want.idempotent ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld || d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s: wrong profiles", id)
		}
	}
	if d := toolOf(t, workspacesSyncID).Description; !strings.Contains(d, "conflict") ||
		!strings.Contains(d, "cannot make a container version") {
		t.Errorf("sync description does not warn about conflicts: %s", d)
	}
}

func TestWorkspaceSyncConfirmationsNameTheWorkspace(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for id, args := range map[string]string{
		workspacesSyncID:         `{"workspace":"` + testWorkspace + `"}`,
		workspacesQuickPreviewID: `{"workspace":"` + testWorkspace + `"}`,
		workspacesResolveConflictID: `{"workspace":"` + testWorkspace + `","fingerprint":"1","entity":{"tag":{"path":"` +
			testTag + `","name":"n","type":"html"}}}`,
	} {
		got := reg.ConfirmationSummary(id)(json.RawMessage(args))
		if !strings.Contains(got, testWorkspace) {
			t.Errorf("%s summary %q lacks the workspace", id, got)
		}
		if id == workspacesResolveConflictID && !strings.Contains(got, testTag) {
			t.Errorf("resolve summary %q lacks the entity", got)
		}
	}
}

func TestWorkspacesSyncWithoutConflicts(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{"syncStatus":{}}`)
	out, err := h.call(t, handleWorkspacesSync, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+":sync" ||
		sent[0].query != "" || sent[0].body != "" {
		t.Fatalf("sent = %+v", sent)
	}
	encoded, _ := json.Marshal(out)
	if string(encoded) != `{"mergeConflict":[],"syncStatus":{"mergeConflict":false,"syncError":false}}` {
		t.Errorf("result = %s", encoded)
	}
	if err := application.ValidateJSON(workspacesSync.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
}

func TestWorkspacesSyncWithConflictsShowsIdentitiesOnly(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{"syncStatus":{"mergeConflict":true},"mergeConflict":[`+
		`{"entityInWorkspace":{"changeStatus":"updated","tag":{"path":"`+testTag+`","name":"Example tag","type":"html",`+
		`"fingerprint":"11","parameter":[{"type":"template","key":"html","value":"synthetic-tag-code"}]}},`+
		`"entityInBaseVersion":{"changeStatus":"none","tag":{"path":"`+testTag+`","name":"Base tag","type":"html",`+
		`"parameter":[{"type":"template","key":"html","value":"synthetic-base-code"}]}}},`+
		`{"entityInWorkspace":{"changeStatus":"deleted","variable":{"path":"`+testWorkspace+`/variables/5",`+
		`"name":"Gone variable"}}}]}`)
	out, err := h.call(t, handleWorkspacesSync, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(out)
	for _, leaked := range []string{"synthetic-tag-code", "synthetic-base-code", "parameter"} {
		if strings.Contains(string(encoded), leaked) {
			t.Errorf("the result carries %q: %s", leaked, encoded)
		}
	}
	if err := application.ValidateJSON(workspacesSync.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	if out["syncStatus"].(map[string]any)["mergeConflict"] != true {
		t.Errorf("syncStatus = %v", out["syncStatus"])
	}
	conflicts := out["mergeConflict"].([]any)
	if len(conflicts) != 2 {
		t.Fatalf("conflicts = %v", conflicts)
	}
	first := conflicts[0].(map[string]any)
	inWorkspace := first["entityInWorkspace"].(map[string]any)
	if inWorkspace["path"] != testTag || inWorkspace["entityType"] != "tag" || inWorkspace["fingerprint"] != "11" ||
		first["entityInBaseVersion"].(map[string]any)["name"] != "Base tag" {
		t.Errorf("conflict = %v", first)
	}
	if _, present := conflicts[1].(map[string]any)["entityInBaseVersion"]; present {
		t.Errorf("a conflict the base version lacks shows one: %v", conflicts[1])
	}
}

func TestWorkspacesSyncCutsTheConflictList(t *testing.T) {
	var entries []string
	for i := 0; i < maxEntries+1; i++ {
		entries = append(entries, `{"entityInWorkspace":{"tag":{"name":"`+strings.Repeat("n", maxNameRunes+100)+`"}}}`)
	}
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{"mergeConflict":[`+strings.Join(entries, ",")+`]}`)
	out, err := h.call(t, handleWorkspacesSync, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	name := out["mergeConflict"].([]any)[0].(map[string]any)["entityInWorkspace"].(map[string]any)["name"].(string)
	if len(out["mergeConflict"].([]any)) != maxEntries || out["truncated"] != true ||
		len([]rune(name)) != maxNameRunes {
		t.Errorf("not cut: %d conflicts, truncated = %v, name runes = %d", len(out["mergeConflict"].([]any)),
			out["truncated"], len([]rune(name)))
	}
}

func TestWorkspaceSyncToolsNeedTheirOwnScope(t *testing.T) {
	for _, c := range []struct {
		handler capability.Handler
		args    string
		have    []string
		want    string
	}{
		{handleWorkspacesSync, `{"workspace":"` + testWorkspace + `"}`, []string{readonlyScope}, editScope},
		{handleWorkspacesSync, `{"workspace":"` + testWorkspace + `"}`, []string{previewScope}, editScope},
		{handleWorkspacesQuickPreview, `{"workspace":"` + testWorkspace + `"}`, []string{readonlyScope, editScope},
			previewScope},
		{handleWorkspacesResolveConflict, resolveArgs(testTag, "1"), []string{readonlyScope, previewScope}, editScope},
	} {
		h := newHarness(t, "", testClientID)
		h.resolved.TokenScopes = c.have
		_, err := h.call(t, c.handler, c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) || len(h.google.seen()) != 0 {
			t.Errorf("have %v: err = %v, requests = %d", c.have, err, len(h.google.seen()))
		}
	}
}

func resolveArgs(path, fingerprint string) string {
	return `{"workspace":"` + testWorkspace + `","fingerprint":"` + fingerprint + `","entity":{"tag":{"path":"` +
		path + `","name":"Example tag","type":"html","paused":true,"parameter":[{"type":"template","key":"html",` +
		`"value":"synthetic html"}]}}}`
}

func TestWorkspacesResolveConflictPostsTheEntityWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{}`)
	args := resolveArgs(testTag, "1700000000001")
	if err := application.ValidateJSON(workspacesResolveConflict.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatal(err)
	}
	out, err := h.call(t, handleWorkspacesResolveConflict, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	wantBody := `{"tag":{"path":"` + testTag + `","name":"Example tag","type":"html","parameter":[{"type":"template",` +
		`"key":"html","value":"synthetic html"}],"paused":true}}`
	if len(sent) != 1 || sent[0].method != "POST" ||
		sent[0].path != "/tagmanager/v2/"+testWorkspace+":resolve_conflict" ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].contentType != "application/json" ||
		sent[0].body != wantBody {
		t.Fatalf("sent = %+v", sent)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(workspacesResolveConflict.OutputSchema, encoded); err != nil ||
		out["resolved"] != true || out["entity"] != testTag || out["workspace"] != testWorkspace {
		t.Errorf("result = %s, %v", encoded, err)
	}
}

func TestWorkspacesResolveConflictOtherKinds(t *testing.T) {
	for kind, member := range map[string]string{
		"trigger":        `"name":"n","type":"pageview"`,
		"variable":       `"name":"n","type":"c"`,
		"folder":         `"name":"n"`,
		"client":         `"name":"n","type":"gtm"`,
		"transformation": `"name":"n","type":"tf"`,
	} {
		h := newHarness(t, "", testClientID)
		h.google.api = status(200, `{}`)
		path := testWorkspace + "/" + kind + "s/9"
		args := `{"workspace":"` + testWorkspace + `","fingerprint":"1","entity":{"` + kind + `":{"path":"` + path +
			`",` + member + `}}}`
		if err := application.ValidateJSON(workspacesResolveConflict.InputSchema, json.RawMessage(args)); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if _, err := h.call(t, handleWorkspacesResolveConflict, args); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if sent := h.sent(t); len(sent) != 1 || !strings.HasPrefix(sent[0].body, `{"`+kind+`":{"path":"`+path+`"`) {
			t.Errorf("%s: sent = %+v", kind, sent)
		}
	}
}

func TestWorkspacesResolveConflictWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	without := `{"workspace":"` + testWorkspace + `","entity":{"tag":{"path":"` + testTag + `","name":"n","type":"html"}}}`
	for _, args := range []string{without, resolveArgs(testTag, "")} {
		reachNothing(t, newHarness(t, "", testClientID), handleWorkspacesResolveConflict, args)
	}
	if err := application.ValidateJSON(workspacesResolveConflict.InputSchema, json.RawMessage(without)); err == nil {
		t.Error("the schema accepts a resolution without fingerprint")
	}
}

func TestWorkspacesResolveConflictRefusesEntitiesOutsideTheBinding(t *testing.T) {
	other := "accounts/1000001/containers/2000002/workspaces/9"
	for _, path := range []string{
		other + "/tags/4",
		"accounts/1000001/containers/2000009/workspaces/3/tags/4",
		"accounts/1000009/containers/2000002/workspaces/3/tags/4",
		testWorkspace + "/triggers/4",
		testWorkspace + "/tags/04",
		testWorkspace + "/tags/4/../5",
		testWorkspace + "/tags/4?x=1",
		"",
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleWorkspacesResolveConflict, resolveArgs(path, "1"))
	}
	// A foreign path is refused even where the connection reaches both workspaces.
	reachNothing(t, newHarness(t, testContainer, testClientID), handleWorkspacesResolveConflict,
		resolveArgs(other+"/tags/4", "1"))
	for _, target := range []string{testContainer, "accounts/1000001"} {
		h := newHarness(t, target, testClientID)
		foreign := "accounts/1000001/containers/2000009/workspaces/3"
		if target == "accounts/1000001" {
			foreign = "accounts/1000009/containers/2000002/workspaces/3"
		}
		reachNothing(t, h, handleWorkspacesResolveConflict, `{"workspace":"`+foreign+`","fingerprint":"1",`+
			`"entity":{"tag":{"path":"`+foreign+`/tags/4","name":"n","type":"html"}}}`)
	}
}

func TestWorkspacesResolveConflictRefusesMalformedEntities(t *testing.T) {
	head := `{"workspace":"` + testWorkspace + `","fingerprint":"1","entity":`
	for _, entity := range []string{
		`{}`,
		`{"customTemplate":{"path":"` + testWorkspace + `/templates/4","name":"n"}}`,
		`{"tag":{"path":"` + testTag + `","name":"n","type":"html"},"folder":{"path":"` + testWorkspace +
			`/folders/6","name":"n"}}`,
		`{"tag":{"path":"` + testTag + `","type":"html"}}`,
		`{"tag":{"path":"` + testTag + `","name":"n"}}`,
		`{"tag":{"name":"n","type":"html"}}`,
		`{"tag":"x"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleWorkspacesResolveConflict, head+entity+`}`)
		if err := application.ValidateJSON(workspacesResolveConflict.InputSchema, json.RawMessage(head+entity+`}`)); err == nil {
			t.Errorf("the schema accepts %s", entity)
		}
	}
	member := `{"tag":{"path":"` + testTag + `","name":"n","type":"html","accountId":"1000009"}}`
	if err := application.ValidateJSON(workspacesResolveConflict.InputSchema, json.RawMessage(head+member+`}`)); err == nil {
		t.Error("the schema accepts an account ID inside an entity")
	}
}

func TestWorkspacesResolveConflictStaleFingerprintIsNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleWorkspacesResolveConflict, resolveArgs(testTag, "1"))
		if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) ||
			len(h.google.seen()) != 1 {
			t.Errorf("status %d: err = %v, requests = %d", code, err, len(h.google.seen()))
		}
	}
}

// previewBody is a preview version with entities whose values must not reach the result.
const previewBody = `{"compilerError":true,"syncStatus":{"syncError":false,"mergeConflict":true},` +
	`"authorizationCode":"synthetic-auth-code","previewUrl":"https://example.invalid/synthetic-preview",` +
	`"containerVersion":{"path":"accounts/1000001/containers/2000002/versions/0","accountId":"1000001",` +
	`"containerId":"2000002","containerVersionId":"0","name":"Preview","description":"Example preview",` +
	`"fingerprint":"77","tagManagerUrl":"https://example.invalid/synthetic-ui-link","authorizationCode":"synthetic-nested-code",` +
	`"tag":[{"name":"t1","parameter":[{"type":"template","key":"html","value":"synthetic-tag-code"}]},{"name":"t2"}],` +
	`"variable":[{"name":"v1","parameter":[{"type":"template","key":"value","value":"synthetic-constant"}]}],` +
	`"customTemplate":[{"name":"c1","templateData":"synthetic-template-code"}],` +
	`"client":[{"name":"k1"}],"container":{"name":"Example container","notes":"synthetic-container-note"}}}`

func TestWorkspacesQuickPreviewShowsIdentifiersAndCountsOnly(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, previewBody)
	out, err := h.call(t, handleWorkspacesQuickPreview, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+":quick_preview" ||
		sent[0].query != "" || sent[0].body != "" {
		t.Fatalf("sent = %+v", sent)
	}
	encoded, _ := json.Marshal(out)
	for _, leaked := range []string{"synthetic-", "example.invalid", "authorizationCode", "Url", "http", "parameter",
		"templateData", "notes"} {
		if strings.Contains(string(encoded), leaked) {
			t.Errorf("the result carries %q: %s", leaked, encoded)
		}
	}
	if err := application.ValidateJSON(workspacesQuickPreview.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	version := out["containerVersion"].(map[string]any)
	counts := version["entityCounts"].(map[string]any)
	if out["compilerError"] != true || out["syncStatus"].(map[string]any)["mergeConflict"] != true ||
		version["containerVersionId"] != "0" || version["name"] != "Preview" || version["fingerprint"] != "77" ||
		counts["tag"] != float64(2) || counts["variable"] != float64(1) || counts["customTemplate"] != float64(1) ||
		counts["client"] != float64(1) || counts["trigger"] != float64(0) {
		t.Errorf("result = %s", encoded)
	}
}

func TestWorkspacesQuickPreviewWithoutVersion(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, `{"syncStatus":{"syncError":true}}`)
	out, err := h.call(t, handleWorkspacesQuickPreview, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(out)
	if string(encoded) != `{"compilerError":false,"syncStatus":{"mergeConflict":false,"syncError":true}}` {
		t.Errorf("result = %s", encoded)
	}
}

func TestWorkspaceSyncToolsRefuseWorkspacesOutsideTheBindingBeforeAnyRequest(t *testing.T) {
	for _, bad := range []struct{ target, workspace string }{
		{testContainer, "accounts/1000001/containers/2000009/workspaces/3"},
		{"accounts/1000001", "accounts/1000009/containers/2000002/workspaces/3"},
		{"", "accounts/1/containers/2/workspaces/03"},
		{"", "accounts/1/containers/2"},
	} {
		h := newHarness(t, bad.target, testClientID)
		reachNothing(t, h, handleWorkspacesSync, `{"workspace":"`+bad.workspace+`"}`)
		reachNothing(t, h, handleWorkspacesQuickPreview, `{"workspace":"`+bad.workspace+`"}`)
	}
}

func TestWorkspaceSyncChangesAreSentOnceAndSayWhenTheOutcomeIsUnknown(t *testing.T) {
	for id, call := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		workspacesSyncID:            {handleWorkspacesSync, `{"workspace":"` + testWorkspace + `"}`},
		workspacesQuickPreviewID:    {handleWorkspacesQuickPreview, `{"workspace":"` + testWorkspace + `"}`},
		workspacesResolveConflictID: {handleWorkspacesResolveConflict, resolveArgs(testTag, "1")},
	} {
		h := newHarness(t, "", testClientID)
		h.google.api = status(500, `oops`)
		_, err := h.call(t, call.handler, call.args)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
			t.Errorf("%s: err = %v, requests = %d", id, err, len(h.google.seen()))
		}
	}
}

func TestWorkspaceSyncFailureHasNoGoogleText(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(403, googleBody(403, "PERMISSION_DENIED", "FORBIDDEN"))
	_, err := h.call(t, handleWorkspacesSync, `{"workspace":"`+testWorkspace+`"}`)
	if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), testEcho) {
		t.Errorf("err = %v", err)
	}
}
