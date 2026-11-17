package googletagmanager

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

const testAccount = "accounts/1000001"

func TestStructureToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	wantScopes := map[string]string{accountsUpdateID: manageAccountsScope, containersSnippetID: readonlyScope,
		containersCombineID: editScope, destinationsListID: readonlyScope, destinationsGetID: readonlyScope}
	for id, scope := range wantScopes {
		if got := metadata.Token.Scopes[id]; !slices.Equal(got, []string{scope}) {
			t.Errorf("%s scopes = %v, want %s", id, got, scope)
		}
	}
	for _, id := range []string{containersSnippetID, destinationsListID, destinationsGetID} {
		d := toolOf(t, id)
		if d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone || d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
		if !slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s is missing from read or write", id)
		}
	}
	update := toolOf(t, accountsUpdateID)
	if update.Risk.Effect != capability.EffectUpdate || update.Risk.Confirmation != capability.ConfirmationRequired ||
		update.RequiresToolAllowList || reg.ConfirmationSummary(accountsUpdateID) == nil {
		t.Errorf("accounts.update = %+v", update)
	}
	if slices.Contains(profiles["read"], accountsUpdateID) || !slices.Contains(profiles["write"], accountsUpdateID) {
		t.Error("accounts.update must be in write and not in read")
	}
	combine := toolOf(t, containersCombineID)
	if !combine.RequiresToolAllowList || combine.Risk.Confirmation != capability.ConfirmationRequired ||
		combine.Risk.Idempotency != capability.IdempotencyNonIdempotent || reg.ConfirmationSummary(containersCombineID) == nil {
		t.Errorf("containers.combine = %+v", combine)
	}
	for name, tools := range profiles {
		if slices.Contains(tools, containersCombineID) {
			t.Errorf("containers.combine is in profile %s", name)
		}
	}
	if !slices.Contains(allToolIDs(), containersCombineID) {
		t.Error("containers.combine is not in the tools list of the test connection")
	}
}

func TestNoToolForDestinationLinkOrOtherUnlistedMethods(t *testing.T) {
	structural := []string{accountsUpdateID, containersSnippetID, containersCombineID, destinationsListID, destinationsGetID}
	var got []string
	for _, d := range descriptors() {
		if strings.Contains(d.ID, "destination") || strings.Contains(d.ID, "move-tag") || strings.Contains(d.ID, "link") ||
			strings.Contains(d.ID, "lookup") {
			got = append(got, d.ID)
		}
	}
	want := []string{destinationsListID, destinationsGetID, containersLookupID}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("destination, move, link, and lookup tools = %v, want %v", got, want)
	}
	ids := map[string]bool{}
	for _, d := range descriptors() {
		ids[d.ID] = true
	}
	for _, id := range structural {
		if !ids[id] {
			t.Errorf("%s is not registered", id)
		}
	}
	for _, id := range []string{Provider + ".destinations.link", Provider + ".containers.move-tag-id",
		Provider + ".containers.move_tag_id", Provider + ".accounts.create", Provider + ".accounts.delete"} {
		if ids[id] {
			t.Errorf("%s must not exist", id)
		}
	}
}

func TestAccountsUpdateTakesOnlyNameAndRequiresFingerprint(t *testing.T) {
	for _, bad := range []string{
		`{"account":"` + testAccount + `","name":"n"}`,
		`{"account":"` + testAccount + `","fingerprint":"1"}`,
		`{"account":"` + testAccount + `","fingerprint":"1","name":""}`,
		`{"account":"` + testAccount + `","fingerprint":"1","name":"n","shareData":true}`,
		`{"account":"` + testAccount + `","fingerprint":"1","name":"n","shareData":false}`,
		`{"account":"` + testAccount + `","fingerprint":"1","name":"` + strings.Repeat("n", 301) + `"}`,
	} {
		if err := application.ValidateJSON(accountsUpdate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
	reachNothing(t, newHarness(t, "", testClientID), handleAccountsUpdate, `{"account":"`+testAccount+`","name":"n"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleAccountsUpdate,
		`{"account":"`+testAccount+`","fingerprint":"","name":"n"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleAccountsUpdate,
		`{"account":"`+testAccount+`","fingerprint":"1","name":"  "}`)
	reachNothing(t, newHarness(t, "", testClientID), handleAccountsUpdate, `{"fingerprint":"1","name":"n"}`)
}

// accountAPI answers the read of the account with its stored state and the update with the stored name.
func accountAPI(shareData bool, fingerprint string) func(*http.Request) (int, string, http.Header) {
	stored := `{"path":"` + testAccount + `","accountId":"1000001","name":"Old","shareData":` +
		strconv.FormatBool(shareData) + `,"fingerprint":"` + fingerprint + `"}`
	return func(req *http.Request) (int, string, http.Header) {
		if req.Method == "PUT" {
			return 200, `{"path":"` + testAccount + `","accountId":"1000001","name":"Example account","shareData":` +
				strconv.FormatBool(shareData) + `,"fingerprint":"1700000000002","features":{"supportMultipleContainers":true}}`, nil
		}
		return 200, stored, nil
	}
}

func TestAccountsUpdateReadsFirstAndSendsTheStoredShareDataUnchanged(t *testing.T) {
	for _, shareData := range []bool{true, false} {
		h := newHarness(t, "", testClientID)
		h.google.api = accountAPI(shareData, "1700000000001")
		args := `{"account":"` + testAccount + `","fingerprint":"1700000000001","name":"Example account"}`
		if err := application.ValidateJSON(accountsUpdate.InputSchema, json.RawMessage(args)); err != nil {
			t.Fatal(err)
		}
		out, err := h.call(t, handleAccountsUpdate, args)
		if err != nil {
			t.Fatal(err)
		}
		sent := h.sent(t)
		want := `{"name":"Example account","shareData":` + strconv.FormatBool(shareData) + `}`
		if len(sent) != 2 || sent[0].method != "GET" || sent[0].path != "/tagmanager/v2/"+testAccount ||
			sent[1].method != "PUT" || sent[1].path != "/tagmanager/v2/"+testAccount ||
			sent[1].query != "fingerprint=1700000000001" || sent[1].body != want ||
			sent[1].contentType != "application/json" {
			t.Fatalf("shareData %v: sent = %+v", shareData, sent)
		}
		if got, _ := out["shareData"].(bool); got != shareData || out["fingerprint"] != "1700000000002" {
			t.Errorf("result = %v", out)
		}
		encoded, _ := json.Marshal(out)
		if err := application.ValidateJSON(accountsUpdate.OutputSchema, encoded); err != nil {
			t.Errorf("result violates the output schema: %v", err)
		}
	}
}

func TestAccountsUpdateWritesNothingWithoutASuccessfulMatchingRead(t *testing.T) {
	args := `{"account":"` + testAccount + `","fingerprint":"1","name":"n"}`
	for _, code := range []int{401, 403, 429, 500} {
		h := newHarness(t, "", testClientID)
		h.google.api = status(code, `{}`)
		if _, err := h.call(t, handleAccountsUpdate, args); err == nil {
			t.Errorf("status %d: no error", code)
		}
		for _, r := range h.sent(t) {
			if r.method != "GET" {
				t.Errorf("status %d: %s was sent after a failed read", code, r.method)
			}
		}
	}
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{"shareData":"yes"}`, nil }
	if _, err := h.call(t, handleAccountsUpdate, args); err == nil {
		t.Error("an unreadable shareData was accepted")
	}
	h = newHarness(t, "", testClientID)
	h.google.api = accountAPI(true, "2")
	if _, err := h.call(t, handleAccountsUpdate, args); !isInvalidRequest(err) {
		t.Errorf("a stale fingerprint: err = %v", err)
	}
	for _, r := range h.sent(t) {
		if r.method != "GET" {
			t.Errorf("%s was sent after a fingerprint mismatch", r.method)
		}
	}
}

func TestAccountsUpdateBindingRules(t *testing.T) {
	args := `{"account":"` + testAccount + `","fingerprint":"1","name":"n"}`
	reachNothing(t, newHarness(t, testContainer, testClientID), handleAccountsUpdate, args)
	reachNothing(t, newHarness(t, testContainer, testClientID), handleAccountsUpdate, `{"fingerprint":"1","name":"n"}`)
	reachNothing(t, newHarness(t, testAccount, testClientID), handleAccountsUpdate,
		`{"account":"accounts/1000009","fingerprint":"1","name":"n"}`)
	for _, target := range []string{testAccount, ""} {
		h := newHarness(t, target, testClientID)
		h.google.api = accountAPI(false, "1")
		if _, err := h.call(t, handleAccountsUpdate, args); err != nil {
			t.Errorf("target %q: %v", target, err)
		}
	}
	h := newHarness(t, testAccount, testClientID)
	h.google.api = accountAPI(false, "1")
	if _, err := h.call(t, handleAccountsUpdate, `{"fingerprint":"1","name":"n"}`); err != nil ||
		h.sent(t)[0].path != "/tagmanager/v2/"+testAccount {
		t.Errorf("an account binding supplies the account: %v", err)
	}
}

func TestAReadOnlyConsentCannotRenameAnAccount(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope, editScope}
	_, err := h.call(t, handleAccountsUpdate, `{"account":"`+testAccount+`","fingerprint":"1","name":"n"}`)
	if err == nil || !strings.Contains(err.Error(), manageAccountsScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing manage scope and no request", err, len(h.google.seen()))
	}
}

func TestSnippetIsReturnedAsTextAndCut(t *testing.T) {
	h := newHarness(t, "", testClientID)
	const snippet = "<script>(function(w,d,s,l,i){w[l]=w[l]||[];})(window,document,'script','dataLayer','GTM-EXAMPLE');</script>"
	h.google.api = func(*http.Request) (int, string, http.Header) {
		raw, _ := json.Marshal(map[string]string{"snippet": snippet, "containerConfig": "synthetic-config"})
		return 200, string(raw), nil
	}
	out, err := h.call(t, handleContainersSnippet, `{"container":"`+testContainer+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["snippet"] != snippet || out["containerConfig"] != "synthetic-config" || out["truncated"] != nil {
		t.Errorf("result = %v", out)
	}
	if got := h.sent(t); len(got) != 1 || got[0].method != "GET" || got[0].path != "/tagmanager/v2/"+testContainer+":snippet" {
		t.Errorf("sent = %+v", got)
	}

	long := strings.Repeat("é", maxSnippetRunes+10)
	h = newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		raw, _ := json.Marshal(map[string]string{"snippet": long, "containerConfig": strings.Repeat("c", maxContainerConfigRunes+1)})
		return 200, string(raw), nil
	}
	out, err = h.call(t, handleContainersSnippet, `{"container":"`+testContainer+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := []rune(out["snippet"].(string)); len(got) != maxSnippetRunes || out["truncated"] != true ||
		len([]rune(out["containerConfig"].(string))) != maxContainerConfigRunes {
		t.Errorf("snippet of %d runes, truncated = %v", len(got), out["truncated"])
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(containersSnippet.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
}

func TestSnippetIsNeverFetchedOrRun(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"snippet":"<script src=\"https://example.org/x.js\"></script>"}`, nil
	}
	if _, err := h.call(t, handleContainersSnippet, `{"container":"`+testContainer+`"}`); err != nil {
		t.Fatal(err)
	}
	for _, req := range h.google.seen() {
		if req.URL.Host != "tagmanager.googleapis.com" {
			t.Errorf("request to %s", req.URL.Host)
		}
	}
	if got := len(h.google.seen()); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}

func TestSnippetOutsideTheBindingIsRefused(t *testing.T) {
	reachNothing(t, newHarness(t, testContainer, testClientID), handleContainersSnippet,
		`{"container":"accounts/1000001/containers/2000009"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleContainersSnippet, `{}`)
	h := newHarness(t, testContainer, testClientID)
	if _, err := h.call(t, handleContainersSnippet, `{}`); err != nil {
		t.Errorf("the bound container is refused: %v", err)
	}
}

const testSourceID = "2000003"

func TestCombineSendsBothContainersAndNoUserPermissionOption(t *testing.T) {
	h := newHarness(t, testAccount, testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testContainer + `","containerId":"2000002","name":"Example"}`, nil
	}
	args := `{"container":"` + testContainer + `","source_container_id":"` + testSourceID + `","setting_source":"other"}`
	if err := application.ValidateJSON(containersCombine.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatal(err)
	}
	out, err := h.call(t, handleContainersCombine, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/"+testContainer+":combine" ||
		sent[0].query != "containerId=2000003&settingSource=other" || sent[0].body != "" {
		t.Fatalf("sent = %+v", sent)
	}
	if strings.Contains(sent[0].query, "allowUserPermissionFeatureUpdate") || out["path"] != testContainer {
		t.Errorf("query = %s, result = %v", sent[0].query, out)
	}
	for _, bad := range []string{
		`{"container":"` + testContainer + `","source_container_id":"` + testSourceID + `","allowUserPermissionFeatureUpdate":true}`,
		`{"container":"` + testContainer + `","source_container_id":"` + testSourceID + `","setting_source":"settingSourceUnspecified"}`,
		`{"container":"` + testContainer + `"}`,
		`{"source_container_id":"` + testSourceID + `"}`,
		`{"container":"` + testContainer + `","source_container_id":"x"}`,
	} {
		if err := application.ValidateJSON(containersCombine.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
	// setting_source stays out of the request when it is left out.
	h = newHarness(t, "", testClientID)
	if _, err := h.call(t, handleContainersCombine,
		`{"container":"`+testContainer+`","source_container_id":"`+testSourceID+`"}`); err != nil ||
		h.sent(t)[0].query != "containerId=2000003" {
		t.Errorf("err = %v", err)
	}
}

func TestCombineRefusesContainersOutsideTheBindingBeforeAnyRequest(t *testing.T) {
	both := `{"container":"` + testContainer + `","source_container_id":"` + testSourceID + `"}`
	// A container binding holds one container, so a merge of two can never lie inside it.
	reachNothing(t, newHarness(t, testContainer, testClientID), handleContainersCombine, both)
	reachNothing(t, newHarness(t, "accounts/1000001/containers/"+testSourceID, testClientID), handleContainersCombine, both)
	// An account binding refuses a target in another account.
	reachNothing(t, newHarness(t, "accounts/1000009", testClientID), handleContainersCombine, both)
	// The source is read in the account of the target, so it cannot leave the binding through another account.
	reachNothing(t, newHarness(t, testAccount, testClientID), handleContainersCombine,
		`{"container":"accounts/1000009/containers/2000002","source_container_id":"`+testSourceID+`"}`)
	// The target must be given, also where a container binding could supply it.
	reachNothing(t, newHarness(t, testContainer, testClientID), handleContainersCombine,
		`{"source_container_id":"`+testSourceID+`"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleContainersCombine,
		`{"container":"`+testContainer+`","source_container_id":"2000002"}`)
	for _, bad := range []string{"0", "02", "-1", "2000003/x", "", "1.5", "99999999999999999999999"} {
		reachNothing(t, newHarness(t, "", testClientID), handleContainersCombine,
			`{"container":"`+testContainer+`","source_container_id":"`+bad+`"}`)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleContainersCombine,
		`{"container":"`+testContainer+`","source_container_id":"`+testSourceID+`","setting_source":"x"}`)
}

func TestConfirmationsNameBothContainersAndNeverShareData(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(containersCombineID)(json.RawMessage(
		`{"container":"` + testContainer + `","source_container_id":"` + testSourceID + `","setting_source":"other"}`))
	for _, want := range []string{testContainer, "accounts/1000001/containers/" + testSourceID, "source", "target"} {
		if !strings.Contains(got, want) {
			t.Errorf("combine summary %q lacks %q", got, want)
		}
	}
	got = reg.ConfirmationSummary(accountsUpdateID)(json.RawMessage(
		`{"account":"` + testAccount + `","fingerprint":"1","name":"Example account"}`))
	if !strings.Contains(got, testAccount) || !strings.Contains(got, "Example account") {
		t.Errorf("account summary %q", got)
	}
}

func TestStructureChangesWithUnclearOutcomeSaySo(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleContainersCombine, `{"container":"`+testContainer+`","source_container_id":"`+testSourceID+`"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
}
