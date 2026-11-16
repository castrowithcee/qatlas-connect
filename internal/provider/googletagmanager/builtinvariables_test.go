package googletagmanager

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

func TestBuiltInVariableToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	if got := metadata.Token.Scopes[builtInVariablesListID]; !slices.Equal(got, []string{readonlyScope}) {
		t.Errorf("list scopes = %v", got)
	}
	if d := toolOf(t, builtInVariablesListID); d.Risk.Effect != capability.EffectRead ||
		d.Risk.Confirmation != capability.ConfirmationNone {
		t.Errorf("list risk = %+v", d.Risk)
	}
	changes := map[string]capability.Effect{builtInVariablesCreateID: capability.EffectCreate,
		builtInVariablesDeleteID: capability.EffectDelete, builtInVariablesRevertID: capability.EffectUpdate}
	for id, effect := range changes {
		d := toolOf(t, id)
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{editScope}) {
			t.Errorf("%s scopes = %v", id, got)
		}
		if d.Risk.Effect != effect || d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
		if d.RequiresToolAllowList != (id == builtInVariablesRevertID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
	}
	if !strings.Contains(toolOf(t, builtInVariablesDeleteID).Description, "breaks them") {
		t.Error("delete description does not warn about broken references")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	if !slices.Contains(profiles["read"], builtInVariablesListID) || !slices.Contains(profiles["write"], builtInVariablesListID) {
		t.Error("list is missing from read or write")
	}
	for _, id := range []string{builtInVariablesCreateID, builtInVariablesDeleteID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if slices.Contains(profiles["read"], builtInVariablesRevertID) || slices.Contains(profiles["write"], builtInVariablesRevertID) {
		t.Error("built-in-variables.revert is in a profile")
	}
}

func TestBuiltInVariableSummariesNameTypesAndWorkspace(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(builtInVariablesDeleteID)(json.RawMessage(
		`{"workspace":"` + testWorkspace + `","types":["pageUrl","clickElement"]}`))
	for _, want := range []string{"Disable", "pageUrl", "clickElement", testWorkspace} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
}

func TestBuiltInVariableTypesComeFromTheDiscoveryEnum(t *testing.T) {
	if len(builtInVariableTypes) != 116 || slices.Contains(builtInVariableTypes, "builtInVariableTypeUnspecified") ||
		!slices.Contains(builtInVariableTypes, "analyticsSessionNumber") || !slices.Contains(builtInVariableTypes, "pageUrl") {
		t.Errorf("types = %d", len(builtInVariableTypes))
	}
	bad := `{"workspace":"` + testWorkspace + `","types":["notAType"]}`
	if err := application.ValidateJSON(builtInVariablesCreate.InputSchema, json.RawMessage(bad)); err == nil {
		t.Error("the schema accepts an unknown type")
	}
}

func TestBuiltInVariablesListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"builtInVariable":[{"type":"pageUrl","name":"Page URL","path":"` + testWorkspace +
				`/built_in_variables/pageUrl"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"builtInVariable":[{"type":"event","name":"Event"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleBuiltInVariablesList, `{"workspace":"`+testWorkspace+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	vars, _ := first["builtInVariables"].([]any)
	if cursor == "" || len(vars) != 1 || vars[0].(map[string]any)["name"] != "Page URL" {
		t.Fatalf("first = %v", first)
	}
	second, err := h.call(t, handleBuiltInVariablesList, `{"workspace":"`+testWorkspace+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["builtInVariables"].([]any)[0].(map[string]any)["type"] != "event" {
		t.Fatalf("second = %v, %v", second, err)
	}
	before := len(h.google.seen())
	other := `{"workspace":"accounts/1000001/containers/2000002/workspaces/4","cursor":"` + cursor + `"}`
	if _, err := h.call(t, handleBuiltInVariablesList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another workspace was taken: %v", err)
	}
	if got := h.sent(t); got[0].method != "GET" || got[0].path != "/tagmanager/v2/"+testWorkspace+"/built_in_variables" ||
		got[0].query != "" || got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
}

func TestBuiltInVariablesCreateSendsAllTypesAsQuery(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"builtInVariable":[{"type":"pageUrl","name":"Page URL"},{"type":"clickElement","name":"Click Element"}]}`, nil
	}
	out, err := h.call(t, handleBuiltInVariablesCreate,
		`{"workspace":"`+testWorkspace+`","types":["pageUrl","clickElement"]}`)
	if err != nil || len(out["builtInVariables"].([]any)) != 2 {
		t.Fatalf("create = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testWorkspace+"/built_in_variables" ||
		sent[0].query != "type=pageUrl&type=clickElement" || sent[0].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestBuiltInVariablesUnknownTypeIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"workspace":"` + testWorkspace + `","types":["pageUrl","bogus"]}`,
		`{"workspace":"` + testWorkspace + `","types":["builtInVariableTypeUnspecified"]}`,
		`{"workspace":"` + testWorkspace + `","types":["pageUrl","pageUrl"]}`,
		`{"workspace":"` + testWorkspace + `","types":[]}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleBuiltInVariablesCreate, args)
		reachNothing(t, newHarness(t, "", testClientID), handleBuiltInVariablesDelete, args)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleBuiltInVariablesRevert,
		`{"workspace":"`+testWorkspace+`","type":"bogus"}`)
}

func TestBuiltInVariablesDeleteAndRevert(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "DELETE" {
			return 204, "", nil
		}
		return 200, `{"enabled":true}`, nil
	}
	out, err := h.call(t, handleBuiltInVariablesDelete, `{"workspace":"`+testWorkspace+`","types":["pageUrl","event"]}`)
	if err != nil || out["deleted"] != true || out["workspace"] != testWorkspace || len(out["types"].([]any)) != 2 {
		t.Fatalf("delete = %v, %v", out, err)
	}
	out, err = h.call(t, handleBuiltInVariablesRevert, `{"workspace":"`+testWorkspace+`","type":"pageUrl"}`)
	if err != nil || out["enabled"] != true || out["type"] != "pageUrl" {
		t.Fatalf("revert = %v, %v", out, err)
	}
	sent := h.sent(t)
	if len(sent) != 2 || sent[0].method != "DELETE" || sent[0].query != "type=pageUrl&type=event" || sent[0].body != "" ||
		sent[0].path != "/tagmanager/v2/"+testWorkspace+"/built_in_variables" ||
		sent[1].method != "POST" || sent[1].path != "/tagmanager/v2/"+testWorkspace+"/built_in_variables:revert" ||
		sent[1].query != "type=pageUrl" || sent[1].body != "" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestBuiltInVariablesOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct{ target, workspace string }{
		{"accounts/1000001/containers/2000002", "accounts/1000001/containers/2000009/workspaces/3"},
		{"accounts/1000001/containers/2000002", "accounts/1000009/containers/2000002/workspaces/3"},
		{"accounts/1000001", "accounts/1000009/containers/2000002/workspaces/3"},
	}
	for _, c := range cases {
		w := `"workspace":"` + c.workspace + `"`
		reachNothing(t, newHarness(t, c.target, testClientID), handleBuiltInVariablesList, `{`+w+`}`)
		reachNothing(t, newHarness(t, c.target, testClientID), handleBuiltInVariablesCreate, `{`+w+`,"types":["pageUrl"]}`)
		reachNothing(t, newHarness(t, c.target, testClientID), handleBuiltInVariablesDelete, `{`+w+`,"types":["pageUrl"]}`)
		reachNothing(t, newHarness(t, c.target, testClientID), handleBuiltInVariablesRevert, `{`+w+`,"type":"pageUrl"}`)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleBuiltInVariablesList, `{"workspace":"accounts/1/containers/2"}`)
}

func TestABuiltInVariableChangeWithUnclearOutcomeSaysSo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleBuiltInVariablesCreate, `{"workspace":"`+testWorkspace+`","types":["pageUrl"]}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}

func TestAReadOnlyConsentCannotChangeBuiltInVariables(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	_, err := h.call(t, handleBuiltInVariablesDelete, `{"workspace":"`+testWorkspace+`","types":["pageUrl"]}`)
	if err == nil || !strings.Contains(err.Error(), editScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}
