package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func TestEmailPreferencesUpdateIsConfirmedOutsideTheReadProfileAndNotListedOnly(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	d, _, ok := reg.Lookup(emailPreferencesUpdateID)
	if !ok || d.RequiresToolAllowList || d.Version != 1 || d.Risk.Effect != capability.EffectUpdate ||
		d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
		t.Fatalf("update = %+v", d)
	}
	for _, id := range metadata.Profiles[0].Tools {
		if id == emailPreferencesUpdateID {
			t.Error("the read profile holds the update")
		}
	}
	for _, tool := range metadata.Tools {
		if tool.ID == emailPreferencesUpdateID && (tool.RequiresToolAllowList || tool.Effect != config.EffectUpdate) {
			t.Errorf("tool = %+v", tool)
		}
	}
	for _, word := range []string{"mail settings", "service account", "no other user", "Only the acceptance", "delay", "untrusted"} {
		if !strings.Contains(d.Description, word) {
			t.Errorf("description lacks %q", word)
		}
	}
}

func TestEmailPreferencesGetReadsTheAliasOfTheAuthenticatedUserAndDropsTheAddress(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/7/users/person@example.com/emailPreferences","newsAndTips":"OPTED_IN","extra":1}`, nil
	}
	out, err := h.call(t, handleEmailPreferencesGet, `{"account":"7"}`)
	if err != nil || out["newsAndTips"] != "OPTED_IN" || out["name"] != "accounts/7/users/me/emailPreferences" ||
		out["extra"] != nil || strings.Contains(mailJSON(t, out), "person@example.com") {
		t.Fatalf("out = %v, %v", out, err)
	}
	if got := h.mailRequests(t); len(got) != 1 || got[0].method != http.MethodGet ||
		got[0].path != "/accounts/v1/accounts/7/users/me/emailPreferences" {
		t.Errorf("requests = %+v", got)
	}
}

func TestEmailPreferencesUpdateSendsOnePatchWithTheMaskAndReportsAcceptance(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/7/users/person@example.com/emailPreferences","newsAndTips":"UNCONFIRMED"}`, nil
	}
	out, err := h.call(t, handleEmailPreferencesUpdate, `{"account":"7","news_and_tips":"OPTED_IN"}`)
	if err != nil || out["accepted"] != true || out["name"] != "accounts/7/users/me/emailPreferences" ||
		strings.Contains(mailJSON(t, out), "person@example.com") || len(out["updated"].([]any)) != 1 ||
		out["updated"].([]any)[0] != "newsAndTips" {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.mailRequests(t)
	if len(got) != 1 || got[0].method != http.MethodPatch || got[0].path != "/accounts/v1/accounts/7/users/me/emailPreferences" ||
		got[0].query != "updateMask=newsAndTips" || got[0].contentType != "application/json" ||
		got[0].body != `{"newsAndTips":"OPTED_IN"}` {
		t.Errorf("requests = %+v", got)
	}
}

func TestEmailPreferencesUpdateRefusesBeforeSecretAndRequest(t *testing.T) {
	for _, args := range []string{
		`{"account":"7"}`, `{"account":"7","news_and_tips":"UNCONFIRMED"}`,
		`{"account":"7","news_and_tips":"OPT_IN_STATE_UNSPECIFIED"}`, `{"account":"7","news_and_tips":"opted_in"}`,
		`{"account":"7","news_and_tips":true}`, `{"account":"07","news_and_tips":"OPTED_OUT"}`,
		`{"account":"8","news_and_tips":"OPTED_OUT"}`, `{"news_and_tips":"OPTED_OUT"}`,
	} {
		h := newHarness(t, "accounts/7", "accounts/9")
		if _, err := h.call(t, handleEmailPreferencesUpdate, args); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want an invalid request", args, err)
		}
		h.untouched(t)
	}
}

func TestEmailPreferencesGetRefusesAnAccountOutsideTheTargets(t *testing.T) {
	h := newHarness(t, "accounts/7")
	if _, err := h.call(t, handleEmailPreferencesGet, `{"account":"8"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	h.untouched(t)
}

func TestEmailPreferencesUpdateOfASubAccountIsProvenBeforeThePatch(t *testing.T) {
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("5", "Sub") + `]}`, nil
		}
		return 200, `{}`, nil
	}
	if _, err := h.call(t, handleEmailPreferencesUpdate, `{"account":"5","news_and_tips":"OPTED_OUT"}`); err != nil {
		t.Fatal(err)
	}
	got := h.mailRequests(t)
	if len(got) != 2 || got[0].method != http.MethodGet || got[1].method != http.MethodPatch {
		t.Errorf("requests = %+v", got)
	}
	h = newHarness(t, "advanced-accounts/1")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{"accounts":[]}`, nil }
	if _, err := h.call(t, handleEmailPreferencesUpdate, `{"account":"9","news_and_tips":"OPTED_OUT"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	for _, req := range h.mailRequests(t) {
		if req.method == http.MethodPatch {
			t.Error("a PATCH followed a failed proof")
		}
	}
}

func TestEmailPreferencesUpdateIsNeverRepeatedAndFailuresAreMapped(t *testing.T) {
	for _, c := range []struct {
		status int
		class  provider.Class
	}{
		{401, provider.ClassAuth}, {403, provider.ClassPermission}, {429, provider.ClassRateLimited},
		{500, provider.ClassProviderError}, {503, provider.ClassUnreachable},
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return c.status, `{"error":{"status":"X"}}`, nil }
		_, err := h.call(t, handleEmailPreferencesUpdate, `{"account":"7","news_and_tips":"OPTED_OUT"}`)
		if classOf(err) != c.class {
			t.Errorf("status %d: class = %q, want %q", c.status, classOf(err), c.class)
		}
		if n := len(h.google.seen()); n != 1 {
			t.Errorf("status %d: %d requests, want one", c.status, n)
		}
	}
}

func TestEmailPreferencesSummaryNamesTheUserOfTheCredential(t *testing.T) {
	in := emailPreferencesSummary([]byte(`{"account":"7","news_and_tips":"OPTED_IN"}`))
	out := emailPreferencesSummary([]byte(`{"news_and_tips":"OPTED_OUT"}`))
	if !strings.Contains(in, "in to") || !strings.Contains(in, "7") || !strings.Contains(in, "service account") ||
		!strings.Contains(out, "out of") || !strings.Contains(out, "of the connection") {
		t.Errorf("summaries = %q, %q", in, out)
	}
}

// mailRequest is one API request as the fake Google saw it.
type mailRequest struct{ method, host, path, query, contentType, body string }

func (h *harness) mailRequests(t *testing.T) []mailRequest {
	t.Helper()
	var out []mailRequest
	for _, req := range h.google.seen() {
		body := ""
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			body = string(raw)
		}
		out = append(out, mailRequest{req.Method, req.URL.Host, req.URL.EscapedPath(), req.URL.RawQuery,
			req.Header.Get("Content-Type"), body})
	}
	return out
}

func mailJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
