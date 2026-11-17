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

const testFolder = testWorkspace + "/folders/7"

func TestFolderToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range []string{foldersListID, foldersGetID, foldersEntitiesID} {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{readonlyScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	changes := map[string]capability.Effect{foldersCreateID: capability.EffectCreate,
		foldersUpdateID: capability.EffectUpdate, foldersMoveID: capability.EffectUpdate,
		foldersDeleteID: capability.EffectDelete, foldersRevertID: capability.EffectUpdate}
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
		if d.RequiresToolAllowList != (id == foldersRevertID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
		if slices.Contains(profiles["read"], id) || slices.Contains(profiles["write"], id) != (id != foldersRevertID) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if toolOf(t, foldersCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("folders.create must be non-idempotent")
	}
	if len(folderDescriptors()) != 8 {
		t.Errorf("folder tools = %d, want 8", len(folderDescriptors()))
	}
}

func TestFolderConfirmationNamesFolderAndWorkspace(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(foldersCreateID)(json.RawMessage(
		`{"workspace":"` + testWorkspace + `","name":"Example folder","notes":"private-note-text"}`))
	for _, want := range []string{"Example folder", testWorkspace, "folder"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "private-note-text") {
		t.Errorf("summary %q shows the notes", got)
	}
	got = reg.ConfirmationSummary(foldersUpdateID)(json.RawMessage(
		`{"folder":"` + testFolder + `","fingerprint":"1","name":"Renamed folder"}`))
	if !strings.Contains(got, testFolder) || !strings.Contains(got, "Renamed folder") {
		t.Errorf("update summary %q", got)
	}
}

func TestFoldersCreateSendsNameAndNotesOnly(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := `{"path":"` + testFolder + `","accountId":"1000001","containerId":"2000002","workspaceId":"3",` +
		`"folderId":"7","name":"Example folder","fingerprint":"1700000000001",` +
		`"tagManagerUrl":"https://tagmanager.google.com/x","notes":"Example note"}`
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, stored, nil }
	args := `{"workspace":"` + testWorkspace + `","name":"Example folder","notes":"Example note"}`
	if err := application.ValidateJSON(foldersCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid folder: %v", err)
	}
	out, err := h.call(t, handleFoldersCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/folders" ||
		sent[0].contentType != "application/json" || sent[0].query != "" ||
		sent[0].body != `{"name":"Example folder","notes":"Example note"}` {
		t.Fatalf("sent = %+v", sent)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(foldersCreate.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	var back map[string]any
	_ = json.Unmarshal([]byte(stored), &back)
	if !reflect.DeepEqual(out, back) {
		t.Errorf("result differs from the stored folder: %s", encoded)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleFoldersCreate, `{"workspace":"`+testWorkspace+`"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleFoldersCreate, `{"workspace":"`+testWorkspace+`","name":" "}`)
}

func TestFolderInputIsClosedAndNeedsAName(t *testing.T) {
	for _, bad := range []string{
		`{"workspace":"` + testWorkspace + `","name":""}`,
		`{"workspace":"` + testWorkspace + `","name":"n","unknown":1}`,
		`{"workspace":"` + testWorkspace + `","name":"n","parentFolderId":"1"}`,
	} {
		if err := application.ValidateJSON(foldersCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
}

func TestFoldersUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"folder":"` + testFolder + `","name":"n"}`,
		`{"folder":"` + testFolder + `","fingerprint":"","name":"n"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleFoldersUpdate, args)
	}
	if err := application.ValidateJSON(foldersUpdate.InputSchema,
		json.RawMessage(`{"folder":"`+testFolder+`","name":"n"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
	reachNothing(t, newHarness(t, "", testClientID), handleFoldersUpdate,
		`{"folder":"`+testFolder+`","fingerprint":"1"}`)
}

func TestFoldersUpdatePutsTheWholeFolderWithFingerprint(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testFolder + `","fingerprint":"1700000000002","name":"n"}`, nil
	}
	out, err := h.call(t, handleFoldersUpdate, `{"folder":"`+testFolder+`","fingerprint":"1700000000001","name":"n","notes":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "PUT" || sent[0].path != "/tagmanager/v2/"+testFolder ||
		sent[0].query != "fingerprint=1700000000001" || sent[0].body != `{"name":"n","notes":"x"}` {
		t.Fatalf("sent = %+v", sent)
	}
	if out["fingerprint"] != "1700000000002" {
		t.Errorf("result = %v", out)
	}
}

func TestStaleFolderFingerprintIsNotGuessedAndNotRepeated(t *testing.T) {
	for _, code := range []int{409, 412} {
		h := newHarness(t, "", testClientID)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return code, googleBody(code, "FAILED_PRECONDITION", "STALE"), nil
		}
		_, err := h.call(t, handleFoldersUpdate, `{"folder":"`+testFolder+`","fingerprint":"1","name":"n"}`)
		if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), testEcho) {
			t.Fatalf("status %d: err = %v", code, err)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("status %d: requests = %d, want 1", code, got)
		}
	}
}

func TestFolderTextsAreNeverCutAndGoBackThroughUpdate(t *testing.T) {
	h := newHarness(t, "", testClientID)
	stored := map[string]any{"path": testFolder, "folderId": "7", "fingerprint": "5",
		"name": strings.Repeat("n", 500) + "é", "notes": strings.Repeat("o", 70000) + "\n\"q\"", "unknown": "dropped"}
	raw, _ := json.Marshal(stored)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, string(raw), nil }
	out, err := h.call(t, handleFoldersGet, `{"folder":"`+testFolder+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != stored["name"] || out["notes"] != stored["notes"] {
		t.Fatal("a folder text was cut or changed")
	}
	if _, ok := out["unknown"]; ok {
		t.Error("a member outside the schema was passed on")
	}
	args, _ := json.Marshal(map[string]any{"folder": testFolder, "fingerprint": "5", "name": out["name"], "notes": out["notes"]})
	if err := application.ValidateJSON(foldersUpdate.InputSchema, args); err != nil {
		t.Fatalf("the schema refuses what a read returned: %v", err)
	}
	if _, err := h.call(t, handleFoldersUpdate, string(args)); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(h.sent(t)[1].body), &sent)
	if sent["name"] != stored["name"] || sent["notes"] != stored["notes"] {
		t.Error("a folder text was changed on the way")
	}
}

func TestOversizedFolderAnswersAreErrorsNotShorterValues(t *testing.T) {
	h := newHarness(t, "", testClientID)
	big := `{"name":"n","notes":"` + strings.Repeat("a", maxResponse) + `"}`
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, big, nil }
	if _, err := h.call(t, handleFoldersGet, `{"folder":"`+testFolder+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
}

func TestFoldersListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"folder":[{"folderId":"1","name":"a"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"folder":[{"folderId":"2","name":"b"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleFoldersList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["folders"].([]any)) != 1 {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleFoldersList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["folders"].([]any)[0].(map[string]any)["folderId"] != "2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleFoldersList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testWorkspace+"/folders" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
	empty := newHarness(t, "", testClientID)
	out, err := empty.call(t, handleFoldersList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil || len(out["folders"].([]any)) != 0 {
		t.Errorf("an empty answer = %v, %v", out, err)
	}
}

func TestFolderEntitiesMixTypesAndPage(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"tag":[{"tagId":"1","name":"t","type":"html","parameter":[{"type":"template","key":"html","value":"<b>x</b>"}]}],` +
				`"variable":[{"variableId":"2","name":"v","type":"c"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"trigger":[{"triggerId":"3","name":"g","type":"pageview"}]}`, nil
		}
		return 400, `{}`, nil
	}
	args := `{"folder":"` + testFolder + `"}`
	first, err := h.call(t, handleFoldersEntities, args)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || len(first["tags"].([]any)) != 1 || len(first["variables"].([]any)) != 1 ||
		len(first["triggers"].([]any)) != 0 {
		t.Fatalf("first = %v", first)
	}
	encoded, _ := json.Marshal(first)
	if err := application.ValidateJSON(foldersEntities.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	second, err := h.call(t, handleFoldersEntities, `{"folder":"`+testFolder+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || len(second["triggers"].([]any)) != 1 ||
		len(second["tags"].([]any)) != 0 {
		t.Fatalf("second = %v, %v", second, err)
	}
	other := `{"folder":"` + testWorkspace + `/folders/8","cursor":"` + cursor + `"}`
	before := len(h.google.seen())
	if _, err := h.call(t, handleFoldersEntities, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another folder was taken: %v", err)
	}
	got := h.sent(t)
	if got[0].method != "POST" || got[0].path != "/tagmanager/v2/"+testFolder+":entities" || got[0].query != "" ||
		got[0].body != "" || got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
}

func TestFolderEntitiesAnswersAreBounded(t *testing.T) {
	h := newHarness(t, "", testClientID)
	many := strings.Repeat(`{"type":"template"},`, maxParamNodes) + `{"type":"template"}`
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"tag":[{"parameter":[` + many + `]}]}`, nil
	}
	if _, err := h.call(t, handleFoldersEntities, `{"folder":"`+testFolder+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response", err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		entry := strings.Repeat(`{"variableId":"1"},`, maxEntries)
		return 200, `{"variable":[` + entry + `{"variableId":"2"}]}`, nil
	}
	if _, err := h.call(t, handleFoldersEntities, `{"folder":"`+testFolder+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid-provider-response for too many entries", err)
	}
}

func TestFoldersOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		target string
		paths  []string
	}{
		{"accounts/1000001/containers/2000002", []string{"accounts/1000001/containers/2000009/workspaces/3/folders/7",
			"accounts/1000009/containers/2000002/workspaces/3/folders/7"}},
		{"accounts/1000001", []string{"accounts/1000009/containers/2000002/workspaces/3/folders/7"}},
	}
	for _, c := range cases {
		for _, path := range c.paths {
			workspace, _, _ := strings.Cut(path, "/folders/")
			h := newHarness(t, c.target, testClientID)
			reachNothing(t, h, handleFoldersGet, `{"folder":"`+path+`"}`)
			reachNothing(t, h, handleFoldersEntities, `{"folder":"`+path+`"}`)
			reachNothing(t, h, handleFoldersList, `{"workspace":"`+workspace+`"}`)
			reachNothing(t, h, handleFoldersCreate, `{"workspace":"`+workspace+`","name":"n"}`)
			reachNothing(t, h, handleFoldersUpdate, `{"folder":"`+path+`","fingerprint":"1","name":"n"}`)
		}
		h := newHarness(t, c.target, testClientID)
		if _, err := h.call(t, handleFoldersGet, `{"folder":"`+testFolder+`"}`); err != nil {
			t.Errorf("%s: a folder inside the target is refused: %v", c.target, err)
		}
	}
	for _, bad := range []string{"accounts/1/containers/2/workspaces/3/folders/07", "accounts/1/containers/2/workspaces/3",
		"accounts/1/containers/2/workspaces/3/folders/7/x", "accounts/1/containers/2/workspaces/3/folders/7?a=b",
		"accounts/1/containers/2/workspaces/3/tags/7"} {
		reachNothing(t, newHarness(t, "", testClientID), handleFoldersGet, `{"folder":"`+bad+`"}`)
	}
}

func TestAFolderChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleFoldersCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeAFolder(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleFoldersCreate, `{"workspace":"`+testWorkspace+`","name":"n"}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing edit scope and no request", err, len(h.google.seen()))
	}
	if _, err := h.call(t, handleFoldersGet, `{"folder":"`+testFolder+`"}`); err != nil {
		t.Errorf("a read with the readonly scope failed: %v", err)
	}
	if _, err := h.call(t, handleFoldersEntities, `{"folder":"`+testFolder+`"}`); err != nil {
		t.Errorf("an entities read with the readonly scope failed: %v", err)
	}
}

func TestFolderMoveNamesCountsNotIDs(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(foldersMoveID)(json.RawMessage(
		`{"folder":"` + testFolder + `","tagId":["11","12"],"variableId":["5"]}`))
	for _, want := range []string{testFolder, "2 tags", "0 triggers", "1 variables"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	for _, id := range []string{foldersDeleteID, foldersRevertID} {
		if got := reg.ConfirmationSummary(id)(json.RawMessage(`{"folder":"` + testFolder + `"}`)); !strings.Contains(got, testFolder) {
			t.Errorf("%s summary %q lacks the folder", id, got)
		}
	}
	d := foldersMove.Description
	for _, want := range []string{"Only the folder assignment", "published container version", "versions.publish"} {
		if !strings.Contains(d, want) {
			t.Errorf("move description lacks %q", want)
		}
	}
}

func TestFoldersMoveSendsTheIDListsAsQuery(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	args := `{"folder":"` + testFolder + `","tagId":["11","12"],"triggerId":[],"variableId":["5"]}`
	if err := application.ValidateJSON(foldersMove.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid move: %v", err)
	}
	out, err := h.call(t, handleFoldersMove, args)
	if err != nil || out["moved"] != true || out["folder"] != testFolder {
		t.Fatalf("move = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" ||
		sent[0].path != "/tagmanager/v2/"+testFolder+":move_entities_to_folder" ||
		sent[0].query != "tagId=11&tagId=12&variableId=5" || sent[0].body != `{}` {
		t.Fatalf("sent = %+v", sent)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(foldersMove.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
}

func TestFoldersMoveRefusesEmptyAndForeignIDsBeforeIO(t *testing.T) {
	for _, ids := range []string{``, `,"tagId":[]`, `,"tagId":[],"triggerId":[],"variableId":[]`,
		`,"tagId":["1/../x"]`, `,"triggerId":["1?a=b"]`, `,"variableId":["07"]`, `,"tagId":["1","a"]`,
		`,"tagId":["accounts/1/containers/2/workspaces/3/tags/4"]`, `,"variableId":["1&tagId=2"]`,
		`,"tagId":["` + strings.Repeat("1", 21) + `"]`,
		`,"tagId":[` + strings.TrimSuffix(strings.Repeat(`"1",`, maxMoveIDs+1), ",") + `]`} {
		args := `{"folder":"` + testFolder + `"` + ids + `}`
		reachNothing(t, newHarness(t, "", testClientID), handleFoldersMove, args)
		if ids != "" && !strings.Contains(ids, `"tagId":[],"triggerId":[]`) && ids != `,"tagId":[]` {
			if err := application.ValidateJSON(foldersMove.InputSchema, json.RawMessage(args)); err == nil {
				t.Errorf("the schema accepts %s", args)
			}
		}
	}
	reachNothing(t, newHarness(t, "", testClientID), handleFoldersMove, `{"tagId":["1"]}`)
}

func TestFoldersDeleteAndRevert(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "DELETE" {
			return 204, "", nil
		}
		return 200, `{"folder":{"path":"` + testFolder + `","name":"base","fingerprint":"9"}}`, nil
	}
	out, err := h.call(t, handleFoldersDelete, `{"folder":"`+testFolder+`"}`)
	if err != nil || out["deleted"] != true || out["folder"] != testFolder {
		t.Fatalf("delete = %v, %v", out, err)
	}
	out, err = h.call(t, handleFoldersRevert, `{"folder":"`+testFolder+`","fingerprint":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if base, _ := out["folder"].(map[string]any); base["name"] != "base" {
		t.Errorf("revert = %v", out)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(foldersRevert.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	if out, err = h.call(t, handleFoldersRevert, `{"folder":"`+testFolder+`","fingerprint":"5"}`); err != nil || len(out) != 0 {
		t.Errorf("revert of a folder the base version lacks = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 3 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testFolder || sent[0].body != "" ||
		sent[1].method != "POST" || sent[1].path != "/tagmanager/v2/"+testFolder+":revert" ||
		sent[1].query != "fingerprint=5" || sent[1].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestFoldersRevertWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	reachNothing(t, newHarness(t, "", testClientID), handleFoldersRevert, `{"folder":"`+testFolder+`"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleFoldersRevert, `{"folder":"`+testFolder+`","fingerprint":""}`)
	if err := application.ValidateJSON(foldersRevert.InputSchema, json.RawMessage(`{"folder":"`+testFolder+`"}`)); err == nil {
		t.Error("the schema accepts a revert without fingerprint")
	}
}

func TestFolderMutationsOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	h := newHarness(t, "accounts/1000001/containers/2000002", testClientID)
	other := "accounts/1000001/containers/2000009/workspaces/3/folders/7"
	reachNothing(t, h, handleFoldersMove, `{"folder":"`+other+`","tagId":["1"]}`)
	reachNothing(t, h, handleFoldersDelete, `{"folder":"`+other+`"}`)
	reachNothing(t, h, handleFoldersRevert, `{"folder":"`+other+`","fingerprint":"1"}`)
}

func TestFolderMutationsAreSentOnceAndSayWhenUnsure(t *testing.T) {
	for name, call := range map[string]func(*harness) error{
		"move": func(h *harness) error {
			_, err := h.call(t, handleFoldersMove, `{"folder":"`+testFolder+`","tagId":["1"]}`)
			return err
		},
		"delete": func(h *harness) error {
			_, err := h.call(t, handleFoldersDelete, `{"folder":"`+testFolder+`"}`)
			return err
		},
		"revert": func(h *harness) error {
			_, err := h.call(t, handleFoldersRevert, `{"folder":"`+testFolder+`","fingerprint":"1"}`)
			return err
		},
	} {
		h := newHarness(t, "", testClientID)
		h.google.api = status(500, `oops`)
		err := call(h)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
			t.Errorf("%s: err = %v, requests = %d", name, err, len(h.google.seen()))
		}
	}
}
