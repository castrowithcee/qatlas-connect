package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const subOne = "0a1b2c3d-4e5f-6789-abcd-ef0123456789"

// notificationRequest is one request as the fake Google saw it.
type notificationRequest struct{ method, path, query, body string }

// requests lists the requests so far and checks each stayed on the fixed origin with the token in the header.
// Nothing but the Merchant API is ever contacted, so a callback address is never fetched.
func (h *harness) requests(t *testing.T) []notificationRequest {
	t.Helper()
	var out []notificationRequest
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" {
			t.Errorf("request left the fixed origin: %s", req.URL.Redacted())
		}
		if req.Header.Get("Authorization") != "Bearer "+testAccess {
			t.Errorf("request %s carries no bearer token", req.URL.Path)
		}
		body := ""
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			body = string(raw)
		}
		out = append(out, notificationRequest{req.Method, req.URL.Path, req.URL.RawQuery, body})
	}
	return out
}

func subscriptionRow(account, id, extra string) string {
	return `{"name":"accounts/` + account + `/notificationsubscriptions/` + id + `","registeredEvent":"PRODUCT_STATUS_CHANGE",` +
		`"callBackUri":"https://hooks.example.invalid/` + id + `"` + extra + `}`
}

func TestNotificationToolsAreRegisteredWithTheRightEffectAndOfferedAsDocumented(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{notificationSubscriptionsListID, notificationSubscriptionsGetID} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.RequiresToolAllowList || d.Risk.Effect != capability.EffectRead ||
			d.Risk.Confirmation != capability.ConfirmationNone {
			t.Errorf("%s = %+v", id, d)
		}
		if !slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile lacks %s", id)
		}
	}
	d, _, ok := reg.Lookup(notificationSubscriptionsDeleteID)
	if !ok || !d.RequiresToolAllowList || d.Version != 1 || d.Risk.Effect != capability.EffectDelete ||
		d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
		t.Fatalf("delete = %+v", d)
	}
	if slices.Contains(metadata.Profiles[0].Tools, notificationSubscriptionsDeleteID) {
		t.Error("the read profile holds the delete")
	}
	var listed bool
	for _, tool := range metadata.Tools {
		if tool.ID == notificationSubscriptionsDeleteID {
			listed = tool.RequiresToolAllowList && tool.Effect == config.EffectDelete
		}
	}
	if !listed {
		t.Error("the delete is not a listed-only delete tool")
	}
	for _, c := range []struct {
		id    string
		words []string
	}{
		{notificationSubscriptionsDeleteID, []string{"no further events", "callback address", "Only the acceptance",
			"delay", "never repeated", "tools list"}},
		{notificationSubscriptionsListID, []string{"never fetched", "untrusted", "outside the targets", "API developer"}},
		{notificationSubscriptionsGetID, []string{"never fetched", "untrusted", "outside the targets"}},
	} {
		d, _, _ := reg.Lookup(c.id)
		for _, word := range c.words {
			if !strings.Contains(strings.ToLower(d.Description), strings.ToLower(word)) {
				t.Errorf("%s: description lacks %q", c.id, word)
			}
		}
	}
}

func TestDeleteConfirmationNamesTheEndOfTheEvents(t *testing.T) {
	text := notificationDeleteSummary(json.RawMessage(`{"account":"7","subscription_id":"` + subOne + `"}`))
	for _, want := range []string{subOne, "account 7", "no further events", "callback address", "cannot create"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary lacks %q: %s", want, text)
		}
	}
	// A value that is not an ID never reaches the text of the confirmation.
	text = notificationDeleteSummary(json.RawMessage(`{"account":"7 evil","subscription_id":"../x"}`))
	if strings.Contains(text, "evil") || strings.Contains(text, "../x") {
		t.Errorf("summary shows unchecked input: %s", text)
	}
}

func TestNoNotificationToolKnowsTheCallbackAddressAsAnArgument(t *testing.T) {
	banned := func(name string) bool {
		n := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(name))
		return strings.Contains(n, "callback") || strings.Contains(n, "uri") || strings.Contains(n, "url") ||
			strings.Contains(n, "webhook") || strings.Contains(n, "endpoint") || strings.Contains(n, "address")
	}
	for _, d := range []capability.Descriptor{notificationSubscriptionsList, notificationSubscriptionsGet,
		notificationSubscriptionsDelete} {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Fatalf("%s: %v", d.ID, err)
		}
		for name := range schema.Properties {
			if banned(name) {
				t.Errorf("%s takes the argument %q", d.ID, name)
			}
		}
		for _, a := range d.Arguments {
			if banned(a.Name) {
				t.Errorf("%s documents the argument %q", d.ID, a.Name)
			}
		}
		if !strings.Contains(string(d.InputSchema), `"additionalProperties":false`) {
			t.Errorf("%s accepts further arguments", d.ID)
		}
	}
	// Even an argument that gets through to a handler changes nothing: no body is ever sent, and no address is.
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == http.MethodGet {
			return 200, subscriptionRow("7", subOne, ""), nil
		}
		return 200, `{}`, nil
	}
	args := `{"account":"7","subscription_id":"` + subOne + `","callBackUri":"https://attacker.example.invalid/x"}`
	if _, err := h.call(t, handleNotificationSubscriptionsGet, args); err != nil {
		t.Fatal(err)
	}
	if _, err := h.call(t, handleNotificationSubscriptionsDelete, args); err != nil {
		t.Fatal(err)
	}
	for _, r := range h.requests(t) {
		if r.body != "" || strings.Contains(r.path+r.query, "attacker") {
			t.Errorf("request carries the address: %+v", r)
		}
	}
}

func TestListShowsEveryDocumentedMemberCapsTheAddressAndNeverFetchesIt(t *testing.T) {
	h := newHarness(t)
	long := "https://hooks.example.invalid/" + strings.Repeat("a", 5000)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "batch-two" {
			return 200, `{"notificationSubscriptions":[` + subscriptionRow("7", "two", "") + `]}`, nil
		}
		return 200, `{"notificationSubscriptions":[` +
			`{"name":"accounts/7/notificationsubscriptions/one","registeredEvent":"ACCOUNT_SERVICE_CHANGE",` +
			`"callBackUri":"` + long + `","targetAccount":"accounts/7","allManagedAccounts":false,"extra":"never held"},` +
			`{"name":"accounts/7/notificationsubscriptions/all","registeredEvent":"PRODUCT_STATUS_CHANGE",` +
			`"callBackUri":"https://hooks.example.invalid/all","allManagedAccounts":true}],"nextPageToken":"batch-two"}`, nil
	}
	first, err := h.call(t, handleNotificationSubscriptionsList, `{"account":"7","page_size":200}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := first["notificationSubscriptions"].([]any)
	one, all := rows[0].(map[string]any), rows[1].(map[string]any)
	uri, _ := one["callBackUri"].(string)
	if len(rows) != 2 || one["name"] != "accounts/7/notificationsubscriptions/one" ||
		one["registeredEvent"] != "ACCOUNT_SERVICE_CHANGE" || one["targetAccount"] != "accounts/7" ||
		one["allManagedAccounts"] != false || one["extra"] != nil ||
		len([]rune(uri)) != maxURLRunes || !strings.HasSuffix(uri, "…") ||
		all["allManagedAccounts"] != true || all["targetAccount"] != nil {
		t.Fatalf("first batch = %v", first)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "batch-two" {
		t.Fatalf("next_cursor = %q, want a wrapped cursor", cursor)
	}
	second, err := h.call(t, handleNotificationSubscriptionsList, `{"account":"7","page_size":200,"cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || len(second["notificationSubscriptions"].([]any)) != 1 {
		t.Fatalf("second batch = %v, %v", second, err)
	}
	got := h.requests(t)
	if len(got) != 2 || got[0].method != "GET" || got[0].path != "/notifications/v1/accounts/7/notificationsubscriptions" ||
		got[0].query != "pageSize=200" || got[1].query != "pageSize=200&pageToken=batch-two" {
		t.Errorf("requests = %+v", got)
	}
	// A cursor belongs to the account and the page size it came from.
	if _, err := h.call(t, handleNotificationSubscriptionsList, `{"account":"7","page_size":100,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("another page size accepted the cursor: %v", err)
	}
	if _, err := h.call(t, handleNotificationSubscriptionsList, `{"account":"8","page_size":200,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("another account accepted the cursor: %v", err)
	}
	if n := len(h.google.seen()); n != 2 {
		t.Errorf("a refused cursor still sent requests: %d", n)
	}
}

func TestListWithoutPageSizeSendsNoneAndRefusesMoreThanTheMaximum(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	out, err := h.call(t, handleNotificationSubscriptionsList, `{"account":"7"}`)
	if err != nil || out["notificationSubscriptions"] == nil || len(out["notificationSubscriptions"].([]any)) != 0 {
		t.Fatalf("out = %v, %v", out, err)
	}
	if got := h.requests(t); len(got) != 1 || got[0].query != "" {
		t.Errorf("requests = %+v", got)
	}
	for _, args := range []string{`{"account":"7","page_size":201}`, `{"account":"7","page_size":-1}`} {
		if _, err := h.call(t, handleNotificationSubscriptionsList, args); !isInvalidRequest(err) {
			t.Errorf("%s = %v", args, err)
		}
	}
}

func TestListDropsSubscriptionsWhoseTargetAccountLiesOutsideTheTargets(t *testing.T) {
	h := newHarness(t, "accounts/7")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"notificationSubscriptions":[` +
			subscriptionRow("7", "own", "") + `,` +
			subscriptionRow("7", "self", `,"targetAccount":"accounts/7"`) + `,` +
			subscriptionRow("7", "foreign", `,"targetAccount":"accounts/8"`) + `,` +
			subscriptionRow("7", "garbled", `,"targetAccount":"accounts/8/x"`) + `,` +
			subscriptionRow("7", "unspelled", `,"targetAccount":"8"`) + `,` +
			subscriptionRow("7", "managed", `,"allManagedAccounts":true`) + `,` +
			subscriptionRow("9", "other", "") + `,{"callBackUri":"https://hooks.example.invalid/nameless"}]}`, nil
	}
	out, err := h.call(t, handleNotificationSubscriptionsList, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, row := range out["notificationSubscriptions"].([]any) {
		names = append(names, strings.TrimPrefix(row.(map[string]any)["name"].(string), "accounts/7/notificationsubscriptions/"))
	}
	if !slices.Equal(names, []string{"own", "self", "managed"}) {
		t.Errorf("kept = %v", names)
	}
	if strings.Contains(mustJSON(t, out), "accounts/8") {
		t.Error("the answer names an account outside the targets")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSubscriptionsOfAnAdvancedAccountKeepTargetsProvenAsSubaccounts(t *testing.T) {
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("2", "Sub") + `]}`, nil
		}
		return 200, `{"notificationSubscriptions":[` +
			subscriptionRow("1", "sub", `,"targetAccount":"accounts/2"`) + `,` +
			subscriptionRow("1", "foreign", `,"targetAccount":"accounts/3"`) + `]}`, nil
	}
	out, err := h.call(t, handleNotificationSubscriptionsList, `{"account":"1"}`)
	rows := out["notificationSubscriptions"].([]any)
	if err != nil || len(rows) != 1 || rows[0].(map[string]any)["name"] != "accounts/1/notificationsubscriptions/sub" {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.requests(t)
	if len(got) != 2 || !strings.HasSuffix(got[1].path, "/accounts/1:listSubaccounts") {
		t.Errorf("requests = %+v", got)
	}
}

func TestAnAccountOutsideTheTargetsIsRefusedBeforeSecretAndRequest(t *testing.T) {
	for name, handler := range map[string]capability.Handler{
		"list": handleNotificationSubscriptionsList, "get": handleNotificationSubscriptionsGet,
		"delete": handleNotificationSubscriptionsDelete,
	} {
		h := newHarness(t, "accounts/7")
		for _, args := range []string{`{"account":"8","subscription_id":"` + subOne + `"}`,
			`{"account":"x","subscription_id":"` + subOne + `"}`} {
			if _, err := handler(t.Context(), h.resolved, h.secrets, h.red, json.RawMessage(args)); !isInvalidRequest(err) {
				t.Errorf("%s %s = %v", name, args, err)
			}
		}
		h.untouched(t)
	}
}

func TestAnAccountOfAnAdvancedAccountNeedsTheProofForEveryTool(t *testing.T) {
	for name, handler := range map[string]capability.Handler{
		"list": handleNotificationSubscriptionsList, "get": handleNotificationSubscriptionsGet,
		"delete": handleNotificationSubscriptionsDelete,
	} {
		h := newHarness(t, "advanced-accounts/1")
		h.google.api = func(req *http.Request) (int, string, http.Header) {
			if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
				return 200, `{"accounts":[` + accountRow("2", "Sub") + `]}`, nil
			}
			t.Errorf("%s: a request followed a failed proof: %s %s", name, req.Method, req.URL.Path)
			return 200, `{}`, nil
		}
		_, err := handler(t.Context(), h.resolved, h.secrets, h.red,
			json.RawMessage(`{"account":"3","subscription_id":"`+subOne+`"}`))
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "3") && strings.Contains(err.Error(), "accounts/3") {
			t.Errorf("%s = %v", name, err)
		}
	}
}

func TestGetReadsOneSubscriptionFromTheFixedPathAndRefusesBadIDsBeforeSecret(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, subscriptionRow("7", subOne, `,"allManagedAccounts":true,"targetAccount":"accounts/7","x":1`), nil
	}
	out, err := h.call(t, handleNotificationSubscriptionsGet, `{"account":"7","subscription_id":"`+subOne+`"}`)
	if err != nil || out["name"] != "accounts/7/notificationsubscriptions/"+subOne || out["allManagedAccounts"] != true ||
		out["callBackUri"] != "https://hooks.example.invalid/"+subOne || out["x"] != nil {
		t.Fatalf("out = %v, %v", out, err)
	}
	if got := h.requests(t); len(got) != 1 || got[0].method != "GET" || got[0].path != "/notifications/v1/accounts/7/notificationsubscriptions/"+subOne {
		t.Errorf("requests = %+v", got)
	}
	bad := newHarness(t)
	for _, id := range []string{"", "../accounts/8", "a/b", "a b", "a?x=1", "a%2fb", "a:b", "-x", "_x", ".", "..",
		strings.Repeat("a", 65), "é", "a\nb"} {
		args, _ := json.Marshal(map[string]string{"account": "7", "subscription_id": id})
		for _, handler := range []capability.Handler{handleNotificationSubscriptionsGet, handleNotificationSubscriptionsDelete} {
			if _, err := handler(t.Context(), bad.resolved, bad.secrets, bad.red, args); !isInvalidRequest(err) {
				t.Errorf("subscription_id %q = %v", id, err)
			}
		}
	}
	bad.untouched(t)
}

func TestGetRefusesAnotherSubscriptionAndATargetOutsideTheTargets(t *testing.T) {
	h := newHarness(t, "accounts/7")
	answers := map[string]string{
		"ok":       subscriptionRow("7", "ok", ""),
		"foreign":  subscriptionRow("7", "foreign", `,"targetAccount":"accounts/8"`),
		"swapped":  subscriptionRow("8", "swapped", ""),
		"nameless": `{"callBackUri":"https://hooks.example.invalid/n"}`,
	}
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		return 200, answers[req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]], nil
	}
	if _, err := h.call(t, handleNotificationSubscriptionsGet, `{"subscription_id":"ok"}`); err != nil {
		t.Errorf("ok = %v", err)
	}
	_, err := h.call(t, handleNotificationSubscriptionsGet, `{"subscription_id":"foreign"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "accounts/8") {
		t.Errorf("foreign = %v", err)
	}
	for _, id := range []string{"swapped", "nameless"} {
		if _, err := h.call(t, handleNotificationSubscriptionsGet, `{"subscription_id":"`+id+`"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s = %v", id, err)
		}
	}
}

func TestDeleteReadsThenDeletesOnceAndReportsOnlyTheAcceptance(t *testing.T) {
	h := newHarness(t, "accounts/7")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == http.MethodGet {
			return 200, subscriptionRow("7", subOne, `,"allManagedAccounts":true`), nil
		}
		return 200, `{}`, nil
	}
	out, err := h.call(t, handleNotificationSubscriptionsDelete, `{"subscription_id":"`+subOne+`"}`)
	if err != nil || out["accepted"] != true || out["name"] != "accounts/7/notificationsubscriptions/"+subOne || len(out) != 2 {
		t.Fatalf("out = %v, %v", out, err)
	}
	path := "/notifications/v1/accounts/7/notificationsubscriptions/" + subOne
	got := h.requests(t)
	if len(got) != 2 || got[0] != (notificationRequest{"GET", path, "", ""}) || got[1] != (notificationRequest{"DELETE", path, "", ""}) {
		t.Errorf("requests = %+v", got)
	}
}

func TestDeleteSendsNothingForASubscriptionWithATargetOutsideTheTargets(t *testing.T) {
	h := newHarness(t, "accounts/7")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method != http.MethodGet {
			t.Errorf("a %s followed the refusal", req.Method)
		}
		return 200, subscriptionRow("7", subOne, `,"targetAccount":"accounts/8"`), nil
	}
	if _, err := h.call(t, handleNotificationSubscriptionsDelete, `{"subscription_id":"`+subOne+`"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
}

func TestDeleteIsNeverRepeatedAndMapsGoogleErrors(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		class  provider.Class
	}{
		{401, `{"error":{"code":401,"status":"UNAUTHENTICATED","message":"expired"}}`, provider.ClassAuth},
		{403, `{"error":{"code":403,"status":"PERMISSION_DENIED","message":"role"}}`, provider.ClassPermission},
		{404, `{"error":{"code":404,"status":"NOT_FOUND","message":"gone"}}`, provider.ClassNotFound},
		{429, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota","details":[{"reason":"QUOTA_TOO_MANY_REQUESTS"}]}}`,
			provider.ClassRateLimited},
		{500, `{"error":{"code":500,"status":"INTERNAL","message":"boom"}}`, provider.ClassProviderError},
		{503, `{}`, provider.ClassUnreachable},
	} {
		h := newHarness(t)
		h.google.api = func(req *http.Request) (int, string, http.Header) {
			if req.Method == http.MethodGet {
				return 200, subscriptionRow("7", subOne, ""), nil
			}
			return c.status, c.body, nil
		}
		_, err := h.call(t, handleNotificationSubscriptionsDelete, `{"account":"7","subscription_id":"`+subOne+`"}`)
		if classOf(err) != c.class {
			t.Errorf("status %d: class = %q (%v), want %q", c.status, classOf(err), err, c.class)
		}
		deletes := 0
		for _, r := range h.requests(t) {
			if r.method == http.MethodDelete {
				deletes++
			}
		}
		if deletes != 1 {
			t.Errorf("status %d: %d DELETE requests, want exactly one", c.status, deletes)
		}
	}
}

func TestListAndGetMapGoogleErrors(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 403, `{"error":{"code":403,"status":"PERMISSION_DENIED","message":"not registered"}}`, nil
	}
	if _, err := h.call(t, handleNotificationSubscriptionsList, `{"account":"7"}`); classOf(err) != provider.ClassPermission {
		t.Errorf("list = %v", err)
	}
	if _, err := h.call(t, handleNotificationSubscriptionsGet, `{"account":"7","subscription_id":"x"}`); classOf(err) != provider.ClassPermission {
		t.Errorf("get = %v", err)
	}
}
