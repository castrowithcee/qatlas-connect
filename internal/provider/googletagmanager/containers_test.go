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

const storedContainerJSON = `{"path":"` + testContainer + `","accountId":"1000001","containerId":"2000002",` +
	`"name":"Example site","publicId":"GTM-EXAMPLE1","usageContext":["web","amp"],"domainName":["example.org"],` +
	`"notes":"stored notes","taggingServerUrls":["https://old.example.org"],"fingerprint":"1700000000000"}`

func TestContainerToolsFollowScopesProfilesAndRisks(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	if got := metadata.Token.Scopes[containersLookupID]; !slices.Equal(got, []string{readonlyScope}) {
		t.Errorf("lookup scopes = %v", got)
	}
	if d := toolOf(t, containersLookupID); d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
		t.Errorf("lookup risk = %+v", d.Risk)
	}
	changes := map[string]struct {
		effect capability.Effect
		scope  string
	}{
		containersCreateID: {capability.EffectCreate, editScope}, containersUpdateID: {capability.EffectUpdate, editScope},
		containersDeleteID: {capability.EffectDelete, deleteScope},
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
		if d.RequiresToolAllowList != (id == containersDeleteID) {
			t.Errorf("%s listed-only = %v", id, d.RequiresToolAllowList)
		}
	}
	if toolOf(t, containersCreateID).Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Error("containers.create must be non-idempotent")
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	if !slices.Contains(profiles["read"], containersLookupID) || !slices.Contains(profiles["write"], containersLookupID) {
		t.Error("containers.lookup is missing from read or write")
	}
	for _, id := range []string{containersCreateID, containersUpdateID} {
		if slices.Contains(profiles["read"], id) || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
	}
	if slices.Contains(profiles["read"], containersDeleteID) || slices.Contains(profiles["write"], containersDeleteID) {
		t.Error("containers.delete is in a profile")
	}
	if d := toolOf(t, containersDeleteID).Description; !strings.Contains(d, "irreversible") ||
		!strings.Contains(d, "Offered only where") {
		t.Errorf("delete description = %q", d)
	}
}

func TestContainersCreateSendsSeveralUsageContexts(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, storedContainerJSON)
	args := `{"account":"accounts/1000001","name":"Example site","usageContext":["web","amp"],` +
		`"domainName":["example.org","example.net"],"notes":"n","taggingServerUrls":["https://tag.example.org"]}`
	if err := application.ValidateJSON(containersCreate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatalf("schema refuses a valid container: %v", err)
	}
	out, err := h.call(t, handleContainersCreate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 1 || sent[0].method != "POST" || sent[0].path != "/tagmanager/v2/accounts/1000001/containers" ||
		sent[0].query != "" || sent[0].contentType != "application/json" {
		t.Fatalf("sent = %+v", sent)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(sent[0].body), &body)
	want := map[string]any{"name": "Example site", "usageContext": []any{"web", "amp"},
		"domainName": []any{"example.org", "example.net"}, "notes": "n",
		"taggingServerUrls": []any{"https://tag.example.org"}}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %s", sent[0].body)
	}
	encoded, _ := json.Marshal(out)
	if err := application.ValidateJSON(containersCreate.OutputSchema, encoded); err != nil || out["fingerprint"] != "1700000000000" {
		t.Errorf("result = %s, %v", encoded, err)
	}
}

func TestContainersCreateInputIsClosedAndNeedsNameAndUsageContext(t *testing.T) {
	for _, bad := range []string{
		`{"name":"n"}`, `{"usageContext":["web"]}`, `{"name":"n","usageContext":[]}`,
		`{"name":"n","usageContext":["usageContextUnspecified"]}`, `{"name":"n","usageContext":["desktop"]}`,
		`{"name":"","usageContext":["web"]}`, `{"name":"n","usageContext":["web"],"fingerprint":"1"}`,
	} {
		if err := application.ValidateJSON(containersCreate.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
	reachNothing(t, newHarness(t, "accounts/1000001", testClientID), handleContainersCreate, `{"name":" ","usageContext":["web"]}`)
}

func TestContainersCreateIsRefusedUnderAContainerBindingAndOutsideAnAccountBinding(t *testing.T) {
	good := `"name":"n","usageContext":["web"]}`
	for _, target := range []string{testContainer, "accounts/1000001/containers/2000009"} {
		h := newHarness(t, target, testClientID)
		reachNothing(t, h, handleContainersCreate, `{"account":"accounts/1000001",`+good)
		reachNothing(t, h, handleContainersCreate, `{`+good)
	}
	h := newHarness(t, "accounts/1000001", testClientID)
	reachNothing(t, h, handleContainersCreate, `{"account":"accounts/1000009",`+good)
	if _, err := h.call(t, handleContainersCreate, `{`+good); err != nil {
		t.Errorf("create with the bound account was refused: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/accounts/1000001/containers" {
		t.Errorf("sent = %+v", got)
	}
	reachNothing(t, newHarness(t, "", testClientID), handleContainersCreate, `{`+good)
}

func TestContainersUpdateWithoutFingerprintIsRefusedBeforeIO(t *testing.T) {
	for _, args := range []string{
		`{"container":"` + testContainer + `","name":"n"}`,
		`{"container":"` + testContainer + `","fingerprint":"","name":"n"}`,
	} {
		reachNothing(t, newHarness(t, "", testClientID), handleContainersUpdate, args)
	}
	if err := application.ValidateJSON(containersUpdate.InputSchema,
		json.RawMessage(`{"container":"`+testContainer+`","name":"n"}`)); err == nil {
		t.Error("the schema accepts an update without fingerprint")
	}
	if err := application.ValidateJSON(containersUpdate.InputSchema,
		json.RawMessage(`{"container":"`+testContainer+`","fingerprint":"1","name":"n","usageContext":["web"]}`)); err == nil {
		t.Error("the schema accepts a usageContext change")
	}
}

func updateHarness(t *testing.T, target string) *harness {
	h := newHarness(t, target, testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == "PUT" {
			return 200, strings.Replace(storedContainerJSON, "1700000000000", "1700000000001", 1), nil
		}
		return 200, storedContainerJSON, nil
	}
	return h
}

func TestContainersUpdateReadsFirstThenPutsTheWholeContainerKeepingUsageContext(t *testing.T) {
	h := updateHarness(t, "")
	args := `{"container":"` + testContainer + `","fingerprint":"1700000000000","name":"Renamed","domainName":["example.org"],` +
		`"notes":"stored notes","taggingServerUrls":["https://old.example.org"],` +
		`"previous_tagging_server_urls":["https://old.example.org"]}`
	if err := application.ValidateJSON(containersUpdate.InputSchema, json.RawMessage(args)); err != nil {
		t.Fatal(err)
	}
	out, err := h.call(t, handleContainersUpdate, args)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 2 || sent[0].method != "GET" || sent[0].path != "/tagmanager/v2/"+testContainer ||
		sent[1].method != "PUT" || sent[1].path != "/tagmanager/v2/"+testContainer ||
		sent[1].query != "fingerprint=1700000000000" {
		t.Fatalf("sent = %+v", sent)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(sent[1].body), &body)
	want := map[string]any{"name": "Renamed", "usageContext": []any{"web", "amp"}, "domainName": []any{"example.org"},
		"notes": "stored notes", "taggingServerUrls": []any{"https://old.example.org"}}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %s", sent[1].body)
	}
	if out["fingerprint"] != "1700000000001" {
		t.Errorf("result = %v", out)
	}
}

func TestContainersUpdateWritesNothingWhenTheReadFails(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleContainersUpdate, `{"container":"`+testContainer+`","fingerprint":"1","name":"n",`+
		`"previous_tagging_server_urls":[]}`)
	if err == nil {
		t.Fatal("update succeeded without a read")
	}
	if sent := h.sent(t); len(sent) != 1 || sent[0].method != "GET" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestUpdateNeedsThePreviousTaggingServerUrlsAndTheyMustMatchTheStoredList(t *testing.T) {
	base := `{"container":"` + testContainer + `","fingerprint":"1","name":"Example site","domainName":["example.org"],` +
		`"notes":"stored notes",`
	for name, args := range map[string]string{
		"no previous":               base + `"taggingServerUrls":["https://old.example.org"]}`,
		"wrong previous":            base + `"taggingServerUrls":["https://new.example.org"],"previous_tagging_server_urls":["https://x.example.org"]}`,
		"removal":                   base + `"previous_tagging_server_urls":[]}`,
		"unchanged, wrong previous": base + `"taggingServerUrls":["https://old.example.org"],"previous_tagging_server_urls":[]}`,
	} {
		h := updateHarness(t, "")
		_, err := h.call(t, handleContainersUpdate, args)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "old.example.org") {
			t.Errorf("%s: err = %v", name, err)
		}
		if sent := h.sent(t); len(sent) != 1 || sent[0].method != "GET" {
			t.Errorf("%s: sent = %+v, want the read only", name, sent)
		}
	}
	if err := application.ValidateJSON(containersUpdate.InputSchema,
		json.RawMessage(base+`"taggingServerUrls":["https://old.example.org"]}`)); err == nil {
		t.Error("the schema accepts an update without previous_tagging_server_urls")
	}
	h := updateHarness(t, "")
	_, err := h.call(t, handleContainersUpdate, base+
		`"taggingServerUrls":["https://new.example.org"],"previous_tagging_server_urls":["https://old.example.org"]}`)
	if err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 2 || !strings.Contains(sent[1].body, `"taggingServerUrls":["https://new.example.org"]`) {
		t.Errorf("sent = %+v", sent)
	}
}

func TestContainerConfirmationShowsOldAndNewTaggingServerUrls(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	got := reg.ConfirmationSummary(containersUpdateID)(json.RawMessage(`{"container":"` + testContainer +
		`","fingerprint":"1","name":"Example site","taggingServerUrls":["https://new.example.org"],` +
		`"previous_tagging_server_urls":["https://old.example.org"]}`))
	for _, want := range []string{"Example site", testContainer, "https://old.example.org", "https://new.example.org", "from"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	got = reg.ConfirmationSummary(containersCreateID)(json.RawMessage(`{"account":"accounts/1000001","name":"Example site",` +
		`"usageContext":["web","server"],"taggingServerUrls":["https://tag.example.org"]}`))
	for _, want := range []string{"Create", "accounts/1000001", "web, server", "https://tag.example.org"} {
		if !strings.Contains(got, want) {
			t.Errorf("create summary %q lacks %q", got, want)
		}
	}
	got = reg.ConfirmationSummary(containersDeleteID)(json.RawMessage(`{"container":"` + testContainer + `"}`))
	if !strings.Contains(got, "Delete") || !strings.Contains(got, testContainer) || !strings.Contains(got, "irreversible") {
		t.Errorf("delete summary %q", got)
	}
}

func TestContainersUpdateRefusesCutValuesInsteadOfWritingThemBack(t *testing.T) {
	longNotes := strings.Repeat("n", maxNoteRunes+50)
	stored := `{"path":"` + testContainer + `","name":"Example site","usageContext":["web"],"notes":"` + longNotes + `"}`
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, stored)
	read, err := h.call(t, handleContainersGet, `{"container":"`+testContainer+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cut, _ := json.Marshal(map[string]any{"container": testContainer, "fingerprint": "1", "name": "Example site",
		"notes": read["notes"], "previous_tagging_server_urls": []string{}})
	_, err = h.call(t, handleContainersUpdate, string(cut))
	if !isInvalidRequest(err) || !strings.Contains(err.Error(), "cut form") {
		t.Fatalf("err = %v", err)
	}
	for _, s := range h.sent(t) {
		if s.method == "PUT" {
			t.Fatal("a cut value was written")
		}
	}
	full, _ := json.Marshal(map[string]any{"container": testContainer, "fingerprint": "1", "name": "Example site",
		"notes": longNotes, "previous_tagging_server_urls": []string{}})
	if _, err := h.call(t, handleContainersUpdate, string(full)); err != nil {
		t.Errorf("the complete value was refused: %v", err)
	}
}

func TestContainersLookupSendsOneIDAndReturnsTheContainer(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(200, storedContainerJSON)
	out, err := h.call(t, handleContainersLookup, `{"tag_id":"GTM-EXAMPLE1"}`)
	if err != nil || out["publicId"] != "GTM-EXAMPLE1" {
		t.Fatalf("out = %v, err = %v", out, err)
	}
	if _, err := h.call(t, handleContainersLookup, `{"destination_id":"AW-123456789"}`); err != nil {
		t.Fatal(err)
	}
	sent := h.sent(t)
	if len(sent) != 2 || sent[0].method != "GET" || sent[0].path != "/tagmanager/v2/accounts/containers:lookup" ||
		sent[0].query != "tagId=GTM-EXAMPLE1" || sent[1].query != "destinationId=AW-123456789" {
		t.Errorf("sent = %+v", sent)
	}
	for _, args := range []string{`{}`, `{"tag_id":"GTM-EXAMPLE1","destination_id":"AW-1"}`} {
		reachNothing(t, newHarness(t, "", testClientID), handleContainersLookup, args)
	}
	for _, bad := range []string{`{"tag_id":"x"}`, `{"tag_id":"GTM-1&x=y"}`, `{"tag_id":"GTM-1/../2"}`} {
		if err := application.ValidateJSON(containersLookup.InputSchema, json.RawMessage(bad)); err == nil {
			t.Errorf("the schema accepts %s", bad)
		}
	}
}

func TestContainersLookupOutsideTheBindingIsNotFoundLikeAMissingID(t *testing.T) {
	var outcomes []string
	for _, target := range []string{"accounts/1000009", "accounts/1000001/containers/2000009", "accounts/1000001"} {
		h := newHarness(t, target, testClientID)
		h.google.api = status(200, storedContainerJSON)
		out, err := h.call(t, handleContainersLookup, `{"tag_id":"GTM-EXAMPLE1"}`)
		if target == "accounts/1000001" {
			if err != nil || out["path"] != testContainer {
				t.Errorf("%s: a container inside the target was refused: %v", target, err)
			}
			continue
		}
		if classOf(err) != provider.ClassNotFound || out != nil {
			t.Fatalf("%s: out = %v, err = %v, want not-found", target, out, err)
		}
		if strings.Contains(err.Error(), "2000002") || strings.Contains(err.Error(), "GTM-EXAMPLE1") {
			t.Errorf("%s: the error names the container: %v", target, err)
		}
		outcomes = append(outcomes, err.Error())
	}
	h := newHarness(t, "accounts/1000009", testClientID)
	h.google.api = status(404, googleBody(404, "NOT_FOUND", "x"))
	_, err := h.call(t, handleContainersLookup, `{"tag_id":"GTM-NOSUCH1"}`)
	if classOf(err) != provider.ClassNotFound || err.Error() != outcomes[0] {
		t.Errorf("a missing ID answers %v, want the same as one outside the binding", err)
	}
	if strings.Contains(err.Error(), testEcho) {
		t.Errorf("Google's answer leaked: %v", err)
	}
}

func TestContainersDeleteSendsOneDeleteAndFollowsTheBinding(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(204, "")
	out, err := h.call(t, handleContainersDelete, `{"container":"`+testContainer+`"}`)
	if err != nil || out["deleted"] != true || out["container"] != testContainer {
		t.Fatalf("delete = %v, %v", out, err)
	}
	if sent := h.sent(t); len(sent) != 1 || sent[0].method != "DELETE" || sent[0].path != "/tagmanager/v2/"+testContainer ||
		sent[0].body != "" {
		t.Errorf("sent = %+v", sent)
	}
	// An account binding reaches every container of the account, a container binding only its own.
	h = newHarness(t, "accounts/1000001", testClientID)
	h.google.api = status(204, "")
	if _, err := h.call(t, handleContainersDelete, `{"container":"accounts/1000001/containers/2000009"}`); err != nil {
		t.Errorf("a container of the bound account was refused: %v", err)
	}
	reachNothing(t, newHarness(t, "accounts/1000001", testClientID), handleContainersDelete,
		`{"container":"accounts/1000009/containers/2000002"}`)
	h = newHarness(t, testContainer, testClientID)
	h.google.api = status(204, "")
	if _, err := h.call(t, handleContainersDelete, `{}`); err != nil {
		t.Errorf("the bound container is refused: %v", err)
	}
	reachNothing(t, newHarness(t, testContainer, testClientID), handleContainersDelete,
		`{"container":"accounts/1000001/containers/2000009"}`)
	reachNothing(t, newHarness(t, "", testClientID), handleContainersDelete, `{}`)
	reachNothing(t, newHarness(t, "", testClientID), handleContainersDelete, `{"container":"accounts/1/containers/02"}`)
}

func TestAContainerChangeWithUnclearOutcomeSaysSoAndAReadOnlyConsentCannotChange(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = status(500, `oops`)
	_, err := h.call(t, handleContainersCreate, `{"account":"accounts/1","name":"n","usageContext":["web"]}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(h.google.seen()))
	}
	h = newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope}
	if _, err := h.call(t, handleContainersDelete, `{"container":"`+testContainer+`"}`); err == nil ||
		!strings.Contains(err.Error(), deleteScope) || len(h.google.seen()) != 0 {
		t.Errorf("err = %v, requests = %d, want the missing delete scope and no request", err, len(h.google.seen()))
	}
	h = newHarness(t, "", testClientID)
	h.resolved.TokenScopes = []string{readonlyScope, editScope}
	if _, err := h.call(t, handleContainersDelete, `{"container":"`+testContainer+`"}`); err == nil ||
		!strings.Contains(err.Error(), deleteScope) || len(h.google.seen()) != 0 {
		t.Errorf("an edit consent deleted a container: %v", err)
	}
}
