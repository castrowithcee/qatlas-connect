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

const testContainer = "accounts/1000001/containers/2000002"

func TestWorkspaceToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	if got := metadata.Token.Scopes[workspacesStatusID]; !slices.Equal(got, []string{readonlyScope}) {
		t.Errorf("status scopes = %v", got)
	}
	if d := toolOf(t, workspacesStatusID); d.Risk.Effect != capability.EffectRead || d.RequiresToolAllowList {
		t.Errorf("status = %+v", d.Risk)
	}
	if !slices.Contains(profiles["read"], workspacesStatusID) || !slices.Contains(profiles["write"], workspacesStatusID) {
		t.Error("status is missing from read or write")
	}
	changes := map[string]struct {
		effect capability.Effect
		scope  string
	}{
		workspacesCreateID: {capability.EffectCreate, editScope}, workspacesUpdateID: {capability.EffectUpdate, editScope},
		workspacesDeleteID: {capability.EffectDelete, deleteScope},
	}
	for id, want := range changes {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{want.scope}) {
			t.Errorf("%s scopes = %v, want only %s", id, got, want.scope)
		}
		if d.Risk.Effect != want.effect || d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
		if d.RequiresToolAllowList != (id == workspacesDeleteID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
		if slices.Contains(profiles["read"], id) {
			t.Errorf("%s is in the read profile", id)
		}
		if inWrite := slices.Contains(profiles["write"], id); inWrite == (id == workspacesDeleteID) {
			t.Errorf("%s in write = %v", id, inWrite)
		}
	}
	if toolOf(t, workspacesCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("workspaces.create must be non-idempotent")
	}
	if d := toolOf(t, workspacesDeleteID).Description; !strings.Contains(d, "not part of a published") ||
		!strings.Contains(d, "lost") {
		t.Errorf("delete description does not warn about lost changes: %s", d)
	}
}

func TestWorkspaceConfirmationsNameTheWorkspace(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(workspacesCreateID)(json.RawMessage(
		`{"container":"` + testContainer + `","name":"Example workspace","description":"private-note"}`))
	for _, want := range []string{"Create", "Example workspace", testContainer} {
		if !strings.Contains(got, want) {
			t.Errorf("create summary %q lacks %q", got, want)
		}
	}
	got = reg.ConfirmationSummary(workspacesUpdateID)(json.RawMessage(
		`{"workspace":"` + testWorkspace + `","fingerprint":"1","name":"Renamed"}`))
	for _, want := range []string{"Renamed", testWorkspace} {
		if !strings.Contains(got, want) {
			t.Errorf("update summary %q lacks %q", got, want)
		}
	}
	got = reg.ConfirmationSummary(workspacesDeleteID)(json.RawMessage(`{"workspace":"` + testWorkspace + `"}`))
	for _, want := range []string{"Delete", testWorkspace, "discard"} {
		if !strings.Contains(got, want) {
			t.Errorf("delete summary %q lacks %q", got, want)
		}
	}
}

func TestWorkspacesCreatePostsNameAndDescription(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testWorkspace + `","accountId":"1000001","containerId":"2000002","workspaceId":"3",` +
			`"name":"Example workspace","description":"d","fingerprint":"1700000000001",` +
			`"tagManagerUrl":"https://tagmanager.google.com/x"}`, nil
	}
	args := `{"container":"` + testContainer + `","name":"Example workspace","description":"d"}`
	if err := application.ValidateJSON(workspacesCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatal(err)
	}
	out, err := h.call(t, handleWorkspacesCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testContainer+"/workspaces" ||
		sent[0].contentType != "application/json" || sent[0].query != "" ||
		sent[0].body != `{"name":"Example workspace","description":"d"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["workspaceId"] != "3" || out["fingerprint"] != "1700000000001" {
		t.Errorf("result = %v", out)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(workspacesCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleWorkspacesCreate, `{"container":"`+testContainer+`"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleWorkspacesCreate, `{"name":"n"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleWorkspacesCreate,
		`{"container":"`+testContainer+`","name":"  "}`)
	if err := application.ValidateJSON(workspacesCreate.InputSchema,
		json.RawMessage(`{"container":"`+testContainer+`","name":"n","unknown":1}`)); err == nil {
		t.Error("the schema accepts an unknown member")
	}
}

func TestWorkspacesCreateUsesTheContainerTarget(t *testing.T) {
	h := newHarness(t, testContainer, testClientID)
	if _, err := h.call(t, handleWorkspacesCreate, `{"name":"n"}`); err != nil {
		t.Fatal(err)
	}
	if got := h.sent(t); len(got) != 1 || got[0].path != "/tagmanager/v2/"+testContainer+"/workspaces" {
		t.Errorf("sent = %+v", got)
	}
}

func TestWorkspacesUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"workspace":"` + testWorkspace + `","name":"n"}`,
		`{"workspace":"` + testWorkspace + `","fingerprint":"","name":"n"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleWorkspacesUpdate, args)
	}
	if err := application.ValidateJSON(workspacesUpdate.InputSchema,
		json.RawMessage(`{"workspace":"`+testWorkspace+`","name":"n"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
}

func TestWorkspacesUpdatePutsTheWorkspaceWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testWorkspace + `","name":"n","fingerprint":"1700000000002"}`, nil
	}
	out, err := h.call(t, handleWorkspacesUpdate,
		`{"workspace":"`+testWorkspace+`","fingerprint":"1700000000001","name":"n","description":"d"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testWorkspace ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].body != `{"name":"n","description":"d"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestStaleWorkspaceFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleWorkspacesUpdate, `{"workspace":"`+testWorkspace+`","fingerprint":"1","name":"n"}`)
		if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
			t.Fatalf("status %d: err = %v", code, err)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("status %d: requests = %d, want 1", code, got)
		}
	}
}

func TestWorkspacesDeleteSendsOneDelete(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 204, "", nil }
	out, err := h.call(t, handleWorkspacesDelete, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil || out["deleted"] != true || out["workspace"] != testWorkspace {
		t.Fatalf("delete = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testWorkspace || sent[0].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestAWorkspaceChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleWorkspacesDelete, `{"workspace":"`+testWorkspace+`"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestWorkspaceToolsNeedTheirOwnScope(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleWorkspacesUpdate, `{"workspace":"`+testWorkspace+`","fingerprint":"1","name":"n"}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("update: err = %v, requests = %d", err, len(h.google.seen()))
	}
	h.resolved.TokenScopes = []string{readonlyScope, editScope}
	_, err = h.call(t, handleWorkspacesDelete, `{"workspace":"`+testWorkspace+`"}`)
	if err == nil || !strings.Contains(err.Error(), deleteScope) || len(h.google.seen()) != 0 {
		t.Errorf("delete: err = %v, requests = %d", err, len(h.google.seen()))
	}
	if _, err := h.call(t, handleWorkspacesStatus, `{"workspace":"`+testWorkspace+`"}`); err != nil {
		t.Errorf("a status read with the readonly scope failed: %v", err)
	}
}

func TestWorkspacesOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	for _, target := range []string{testContainer, "accounts/1000001"} {
		for _, workspace := range []string{"accounts/1000001/containers/2000009/workspaces/3",
			"accounts/1000009/containers/2000002/workspaces/3"} {
			if target == "accounts/1000001" && strings.HasPrefix(workspace, "accounts/1000001/") {
				continue
			}
			container, _, _ := strings.Cut(workspace, "/workspaces/")
			h := newHarness(t, target, testClientID)
			reachNothing(t, h, handleWorkspacesCreate, `{"container":"`+container+`","name":"n"}`)
			reachNothing(t, h, handleWorkspacesUpdate, `{"workspace":"`+workspace+`","fingerprint":"1","name":"n"}`)
			reachNothing(t, h, handleWorkspacesDelete, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleWorkspacesStatus, `{"workspace":"`+workspace+`"}`)
		}
	}
	h := newHarness(t, "", testClientID)
	reachNothing(t, h, handleWorkspacesDelete, `{"workspace":"accounts/1/containers/2/workspaces/03"}`)
	reachNothing(t, h, handleWorkspacesStatus, `{"workspace":"accounts/1/containers/2"}`)
}

// statusBody has changed and conflicting entities of several kinds, each with a value that must not reach the result.
const statusBody = `{"workspaceChange":[` +
	`{"changeStatus":"updated","tag":{"path":"` + testWorkspace + `/tags/4","name":"Example tag","type":"html",` +
	`"fingerprint":"11","tagManagerUrl":"https://tagmanager.google.com/t","parameter":[{"type":"template","key":"html","value":"synthetic-tag-code"}]}},` +
	`{"changeStatus":"added","variable":{"path":"` + testWorkspace + `/variables/5","name":"Example variable","type":"c",` +
	`"parameter":[{"type":"template","key":"value","value":"synthetic-constant"}]}},` +
	`{"changeStatus":"deleted","folder":{"path":"` + testWorkspace + `/folders/6","name":"Example folder"}},` +
	`{"changeStatus":"updated","customTemplate":{"path":"` + testWorkspace + `/templates/7","name":"Example template","templateData":"synthetic-template-code"}}],` +
	`"mergeConflict":[{"entityInWorkspace":{"changeStatus":"updated","trigger":{"path":"` + testWorkspace + `/triggers/8","name":"Example trigger","type":"pageview"}},` +
	`"entityInBaseVersion":{"changeStatus":"none","trigger":{"path":"` + testWorkspace + `/triggers/8","name":"Base trigger","type":"pageview"}}},` +
	`{"entityInWorkspace":{"changeStatus":"deleted","tag":{"path":"` + testWorkspace + `/tags/9","name":"Gone tag"}}}]}`

func TestWorkspacesStatusListsChangesAndConflictsWithoutValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, statusBody, nil }
	out, err := h.call(t, handleWorkspacesStatus, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.paths(t); len(got) != 1 || got[0] != "/tagmanager/v2/"+testWorkspace+"/status" {
		t.Errorf("requests = %v", got)
	}
	encoded, _ := json.Marshal(out)
	for _, secret := range []string{"synthetic-tag-code", "synthetic-constant", "synthetic-template-code", "parameter", "templateData"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("the result carries %q: %s", secret, encoded)
		}
	}
	if err := application.ValidateJSON(workspacesStatus.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	changes := out["workspaceChange"].([]any)
	if len(changes) != 4 {
		t.Fatalf("changes = %v", changes)
	}
	first := changes[0].(map[string]any)
	want := map[string]any{"entityType": "tag", "changeStatus": "updated", "path": testWorkspace + "/tags/4",
		"name": "Example tag", "type": "html", "fingerprint": "11", "tagManagerUrl": "https://tagmanager.google.com/t"}
	for k, v := range want {
		if first[k] != v {
			t.Errorf("first change %s = %v, want %v", k, first[k], v)
		}
	}
	if kinds := []any{changes[1].(map[string]any)["entityType"], changes[2].(map[string]any)["entityType"],
		changes[3].(map[string]any)["entityType"]}; !slices.Equal(kinds, []any{"variable", "folder", "customTemplate"}) {
		t.Errorf("kinds = %v", kinds)
	}
	conflicts := out["mergeConflict"].([]any)
	if len(conflicts) != 2 {
		t.Fatalf("conflicts = %v", conflicts)
	}
	both := conflicts[0].(map[string]any)
	if both["entityInWorkspace"].(map[string]any)["name"] != "Example trigger" ||
		both["entityInBaseVersion"].(map[string]any)["name"] != "Base trigger" {
		t.Errorf("conflict = %v", both)
	}
	if _, present := conflicts[1].(map[string]any)["entityInBaseVersion"]; present {
		t.Errorf("a conflict with an entity the base version lacks shows one: %v", conflicts[1])
	}
}

func TestWorkspacesStatusOfAnUnchangedWorkspaceHasEmptyLists(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	out, err := h.call(t, handleWorkspacesStatus, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(out)
	if string(encoded) != `{"mergeConflict":[],"workspaceChange":[]}` {
		t.Errorf("result = %s", encoded)
	}
}

func TestWorkspacesStatusCutsTextAndTheList(t *testing.T) {
	var entries []string
	for i := 0; i < maxEntries+1; i++ {
		entries = append(entries, `{"changeStatus":"updated","tag":{"name":"`+strings.Repeat("n", maxNameRunes+100)+`"}}`)
	}
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"workspaceChange":[` + strings.Join(entries, ",") + `]}`, nil
	}
	out, err := h.call(t, handleWorkspacesStatus, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	changes := out["workspaceChange"].([]any)
	name := changes[0].(map[string]any)["name"].(string)
	if len(changes) != maxEntries || out["truncated"] != true || len([]rune(name)) != maxNameRunes ||
		!strings.HasSuffix(name, "…") {
		t.Errorf("changes = %d, truncated = %v, name runes = %d", len(changes), out["truncated"], len([]rune(name)))
	}
}

func TestWorkspacesStatusFailureHasNoGoogleText(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 403, googleBody(403, "PERMISSION_DENIED", "FORBIDDEN"), nil
	}
	_, err := h.call(t, handleWorkspacesStatus, `{"workspace":"`+testWorkspace+`"}`)
	if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), testEcho) {
		t.Errorf("err = %v", err)
	}
}

func TestWorkspaceTextsAreNeverCut(t *testing.T) {
	name, description := strings.Repeat("n", 5000), strings.Repeat("é", 70000)
	stored, _ := json.Marshal(map[string]string{"path": testWorkspace, "name": name, "description": description,
		"fingerprint": "1"})
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(stored), nil }
	for _, c := range []struct {
		handler capability.Handler
		args    string
	}{
		{handleWorkspacesGet, `{"workspace":"` + testWorkspace + `"}`},
		{handleWorkspacesUpdate, `{"workspace":"` + testWorkspace + `","fingerprint":"1","name":"` + name +
			`","description":"` + description + `"}`},
	} {
		out, err := h.call(t, c.handler, c.args)
		if err != nil || out["name"] != name || out["description"] != description {
			t.Fatalf("a workspace text was cut or refused: %v", err)
		}
	}
	list, _ := json.Marshal(map[string]any{"workspace": []json.RawMessage{stored}})
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(list), nil }
	out, err := h.call(t, handleWorkspacesList, `{"container":"`+testContainer+`"}`)
	if err != nil || out["workspaces"].([]any)[0].(map[string]any)["description"] != description {
		t.Fatalf("list cut a workspace text: %v", err)
	}
	if got := h.sent(t)[1]; !strings.Contains(got.body, description) {
		t.Error("update did not send the description unchanged")
	}
}
