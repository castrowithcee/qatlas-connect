package googlemerchant

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// request is one API request as the fake Google saw it.
type seenRequest struct{ method, host, path, query, contentType, body string }

func (h *harness) renderRequests(t *testing.T) []seenRequest {
	t.Helper()
	var out []seenRequest
	for _, req := range h.google.seen() {
		if req.URL.Scheme != "https" || req.Header.Get("Authorization") != "Bearer "+testAccess {
			t.Errorf("request %s %s has the wrong scheme or no bearer token", req.Method, req.URL.Path)
		}
		body := ""
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			body = string(raw)
		}
		out = append(out, seenRequest{req.Method, req.URL.Host, req.URL.EscapedPath(), req.URL.RawQuery,
			req.Header.Get("Content-Type"), body})
	}
	return out
}

func TestRenderToolsAreReadsWithinTheReadProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{renderAccountIssuesID, renderProductIssuesID} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.RequiresToolAllowList || d.Risk.Effect != capability.EffectRead ||
			d.Risk.Confirmation != capability.ConfirmationNone || d.Risk.Idempotency != capability.IdempotencySafe {
			t.Errorf("%s = %+v", id, d)
		}
		found := false
		for _, tool := range metadata.Profiles[0].Tools {
			found = found || tool == id
		}
		if !found {
			t.Errorf("the read profile lacks %s", id)
		}
	}
}

func TestRenderDescriptionsNameTheFlowsAndTheTextOnlyAddresses(t *testing.T) {
	for _, d := range []capability.Descriptor{renderAccountIssues, renderProductIssues} {
		for _, word := range []string{"read", "POST", "triggeraction", "never fetched", "untrusted", "plain text",
			"BUILT_IN_USER_INPUT_ACTIONS", "actionContext"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", d.ID, word)
			}
		}
	}
}

const renderedFixture = `{"renderedIssues":[{
 "title":"Missing image",
 "impact":{"severity":"ERROR","message":"Disapproves 3 offers","breakdowns":[{"regions":[{"code":"CH","name":"Switzerland"}],"details":["Not shown in ads"]}],"x":1},
 "prerenderedContent":"<div class=\"issue-detail\"><a href=\"https://content.example.invalid/fix\">fix</a><script>alert(1)</script></div>",
 "prerenderedOutOfCourtDisputeSettlement":"<div class=\"ods-section\"><a href=\"https://ods.example.invalid/\">ods</a></div>",
 "actions":[
  {"buttonLabel":"Request review","isAvailable":false,
   "reasons":[{"message":"Review requested","detail":"wait","action":{"buttonLabel":"Open","externalAction":{"type":"REVIEW_PRODUCT_ISSUE_IN_MERCHANT_CENTER","uri":"https://redirect.example.invalid/mc"}}}],
   "externalAction":{"type":"LEGAL_APPEAL_IN_HELP_CENTER","uri":"https://help.example.invalid/appeal"}},
  {"buttonLabel":"Show more","builtinSimpleAction":{"type":"SHOW_ADDITIONAL_CONTENT","additionalContent":{"title":"More","paragraphs":["one","two"]}}},
  {"buttonLabel":"Disagree","isAvailable":true,"builtinUserInputAction":{"actionContext":"OPAQUE-CONTEXT","flows":[{"label":"I disagree","dialogTitle":"Before","dialogButtonLabel":"Send",
    "dialogMessage":{"simpleValue":"Check","simpleTooltipValue":"tip","tooltipIconStyle":"INFO"},
    "dialogCallout":{"fullMessage":{"simpleValue":"Once"},"styleHint":"WARNING"},
    "inputs":[{"id":"f1","required":true,"label":{"simpleValue":"Why"},"textInput":{"type":"GENERIC_LONG_TEXT","formatInfo":"no PII"}},
              {"id":"f2","checkboxInput":{}},
              {"id":"f3","choiceInput":{"options":[{"id":"o1","label":{"simpleValue":"A"},"additionalInput":{"id":"f4","textInput":{"type":"GENERIC_SHORT_TEXT"}}}]}}]}]}}
 ]}],"unknown":"never held"}`

func TestRenderAccountIssuesSendsOnePostAndShowsEveryDocumentedMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, renderedFixture, nil }
	out, err := h.call(t, handleRenderAccountIssues, `{"account":"7","language_code":"de-CH","time_zone":"Europe/Zurich",`+
		`"content_option":"PRE_RENDERED_HTML","user_input_action_option":"BUILT_IN_USER_INPUT_ACTIONS"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := h.renderRequests(t)
	if len(got) != 1 || got[0].method != http.MethodPost || got[0].host != "merchantapi.googleapis.com" ||
		got[0].path != "/issueresolution/v1/accounts/7:renderaccountissues" ||
		got[0].query != "languageCode=de-CH&timeZone=Europe%2FZurich" || got[0].contentType != "application/json" ||
		got[0].body != `{"contentOption":"PRE_RENDERED_HTML","userInputActionOption":"BUILT_IN_USER_INPUT_ACTIONS"}` {
		t.Fatalf("requests = %+v", got)
	}
	if out["unknown"] != nil {
		t.Errorf("unknown member held: %v", out)
	}
	issue := out["renderedIssues"].([]any)[0].(map[string]any)
	impact := issue["impact"].(map[string]any)
	breakdown := impact["breakdowns"].([]any)[0].(map[string]any)
	if issue["title"] != "Missing image" || impact["severity"] != "ERROR" || impact["message"] != "Disapproves 3 offers" ||
		impact["x"] != nil || breakdown["details"].([]any)[0] != "Not shown in ads" ||
		breakdown["regions"].([]any)[0].(map[string]any)["code"] != "CH" {
		t.Errorf("issue = %v", issue)
	}
	actions := issue["actions"].([]any)
	if len(actions) != 3 {
		t.Fatalf("actions = %v", actions)
	}
	first := actions[0].(map[string]any)
	reason := first["reasons"].([]any)[0].(map[string]any)
	if first["isAvailable"] != false || first["externalAction"].(map[string]any)["uri"] != "https://help.example.invalid/appeal" ||
		reason["message"] != "Review requested" ||
		reason["action"].(map[string]any)["externalAction"].(map[string]any)["type"] != "REVIEW_PRODUCT_ISSUE_IN_MERCHANT_CENTER" {
		t.Errorf("first action = %v", first)
	}
	more := actions[1].(map[string]any)["builtinSimpleAction"].(map[string]any)["additionalContent"].(map[string]any)
	if more["title"] != "More" || len(more["paragraphs"].([]any)) != 2 {
		t.Errorf("additional content = %v", more)
	}
	flow := actions[2].(map[string]any)["builtinUserInputAction"].(map[string]any)["flows"].([]any)[0].(map[string]any)
	inputs := flow["inputs"].([]any)
	if flow["dialogTitle"] != "Before" || flow["dialogButtonLabel"] != "Send" || len(inputs) != 3 ||
		inputs[0].(map[string]any)["textInput"].(map[string]any)["formatInfo"] != "no PII" ||
		inputs[2].(map[string]any)["choiceInput"].(map[string]any)["options"].([]any)[0].(map[string]any)["additionalInput"] == nil ||
		flow["dialogCallout"].(map[string]any)["styleHint"] != "WARNING" {
		t.Errorf("flow = %v", flow)
	}
}

func TestRenderedAddressesAndHTMLAreTextAndNeverLeadToAnotherRequest(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, renderedFixture, nil }
	out, err := h.call(t, handleRenderAccountIssues, `{"account":"7","user_input_action_option":"BUILT_IN_USER_INPUT_ACTIONS"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range h.renderRequests(t) {
		if req.host != "merchantapi.googleapis.com" || !strings.HasPrefix(req.path, "/issueresolution/v1/") {
			t.Errorf("a request left the one render path: %+v", req)
		}
	}
	if n := len(h.google.seen()); n != 1 {
		t.Errorf("%d requests, want the one render POST", n)
	}
	issue := out["renderedIssues"].([]any)[0].(map[string]any)
	html, _ := issue["prerenderedContent"].(string)
	if !strings.Contains(html, `<a href="https://content.example.invalid/fix">`) || !strings.Contains(html, "<script>") {
		t.Errorf("pre-rendered HTML is not kept verbatim as text: %q", html)
	}
	if h.google.grants > 1 {
		t.Errorf("%d token grants, want at most the one of the call", h.google.grants)
	}
	if encoded := renderedJSON(t, out); strings.Contains(encoded, "OPAQUE-CONTEXT") || strings.Contains(encoded, "actionContext") {
		t.Error("the opaque action context was returned")
	}
}

func renderedJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRenderedTextsAreBoundedAndListsAreCut(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("x", maxRenderedText+5)
	html := strings.Repeat("h", maxHTMLRunes+5)
	var issues []string
	for i := 0; i < maxIssues+5; i++ {
		issues = append(issues, `{"title":"t"}`)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"renderedIssues":[{"title":"` + long + `","prerenderedContent":"` + html + `","impact":{"severity":3},` +
			`"actions":[{"externalAction":{"uri":"` + strings.Repeat("u", maxURLRunes+5) + `"}}]},` +
			strings.Join(issues, ",") + `]}`, nil
	}
	out, err := h.call(t, handleRenderAccountIssues, `{"account":"7"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["renderedIssues"].([]any)
	if len(rows) != maxIssues {
		t.Errorf("%d issues, want %d", len(rows), maxIssues)
	}
	first := rows[0].(map[string]any)
	if got := []rune(first["title"].(string)); len(got) != maxRenderedText || got[len(got)-1] != '…' {
		t.Errorf("title has %d runes", len(got))
	}
	if got := []rune(first["prerenderedContent"].(string)); len(got) != maxHTMLRunes {
		t.Errorf("html has %d runes", len(got))
	}
	uri := first["actions"].([]any)[0].(map[string]any)["externalAction"].(map[string]any)["uri"].(string)
	if len([]rune(uri)) != maxURLRunes {
		t.Errorf("uri has %d runes", len([]rune(uri)))
	}
	if first["impact"] != nil {
		if _, has := first["impact"].(map[string]any)["severity"]; has {
			t.Errorf("a number as severity = %v, want it dropped", first["impact"])
		}
	}
}

func TestRenderAnEmptyAnswerIsAnEmptyList(t *testing.T) {
	h := newHarness(t)
	out, err := h.call(t, handleRenderAccountIssues, `{"account":"7"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rows, ok := out["renderedIssues"].([]any); !ok || len(rows) != 0 {
		t.Errorf("out = %v", out)
	}
	if got := h.renderRequests(t); len(got) != 1 || got[0].body != `{}` || got[0].query != "" {
		t.Errorf("requests = %+v", got)
	}
}

func TestRenderProductIssuesBuildsTheEscapedPathFromTheCheckedSegment(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("en~US~sku/123"))
	for _, c := range []struct{ product, segment string }{
		{"en~US~sku123", "en~US~sku123"},
		{"local~en~US~sku.1_a-b", "local~en~US~sku.1_a-b"},
		{"de-CH~CH~A.B", "de-CH~CH~A.B"},
		{encoded, encoded},
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, renderedFixture, nil }
		if _, err := h.call(t, handleRenderProductIssues, `{"account":"7","product":"`+c.product+`"}`); err != nil {
			t.Fatalf("%s: %v", c.product, err)
		}
		got := h.renderRequests(t)
		want := "/issueresolution/v1/accounts/7/products/" + url.PathEscape(c.segment) + ":renderproductissues"
		if len(got) != 1 || got[0].method != http.MethodPost || got[0].path != want {
			t.Errorf("%s: requests = %+v, want path %s", c.product, got, want)
		}
	}
}

func TestRenderArgumentsAreCheckedBeforeSecretAndRequest(t *testing.T) {
	badEncoded := base64.RawURLEncoding.EncodeToString([]byte("no tildes here"))
	control := base64.RawURLEncoding.EncodeToString([]byte("en~US~a\nb"))
	empty := base64.RawURLEncoding.EncodeToString([]byte("en~US~"))
	for _, c := range []struct {
		name    string
		handler capability.Handler
		args    string
	}{
		{"language with a slash", handleRenderAccountIssues, `{"account":"7","language_code":"en/US"}`},
		{"long language", handleRenderAccountIssues, `{"account":"7","language_code":"` + strings.Repeat("a", 40) + `"}`},
		{"time zone traversal", handleRenderAccountIssues, `{"account":"7","time_zone":"../etc"}`},
		{"time zone with a space", handleRenderAccountIssues, `{"account":"7","time_zone":"Europe/Zu rich"}`},
		{"unknown content option", handleRenderAccountIssues, `{"account":"7","content_option":"RAW"}`},
		{"unknown input option", handleRenderAccountIssues, `{"account":"7","user_input_action_option":"TRIGGER"}`},
		{"bad account", handleRenderAccountIssues, `{"account":"07"}`},
		{"no account", handleRenderAccountIssues, `{}`},
		{"no product", handleRenderProductIssues, `{"account":"7"}`},
		{"product with a slash", handleRenderProductIssues, `{"account":"7","product":"en~US~a/b"}`},
		{"product with a percent", handleRenderProductIssues, `{"account":"7","product":"en~US~a%2Fb"}`},
		{"product with a query", handleRenderProductIssues, `{"account":"7","product":"en~US~a?x=1"}`},
		{"product with a second action", handleRenderProductIssues, `{"account":"7","product":"en~US~a:triggeraction"}`},
		{"product with a dot path", handleRenderProductIssues, `{"account":"7","product":".."}`},
		{"product with a tilde in the offer", handleRenderProductIssues, `{"account":"7","product":"en~US~a~b"}`},
		{"product of two parts", handleRenderProductIssues, `{"account":"7","product":"en~US"}`},
		{"encoded without structure", handleRenderProductIssues, `{"account":"7","product":"` + badEncoded + `"}`},
		{"encoded with a control character", handleRenderProductIssues, `{"account":"7","product":"` + control + `"}`},
		{"encoded with an empty offer", handleRenderProductIssues, `{"account":"7","product":"` + empty + `"}`},
		{"encoded with padding", handleRenderProductIssues, `{"account":"7","product":"ZW5-VVN-c2t1LzEyMw=="}`},
		{"too long", handleRenderProductIssues, `{"account":"7","product":"` + strings.Repeat("a", 600) + `"}`},
		{"outside the targets", handleRenderAccountIssues, `{"account":"8"}`},
	} {
		h := newHarness(t, "accounts/7", "accounts/9")
		_, err := h.call(t, c.handler, c.args)
		if !isInvalidRequest(err) {
			t.Errorf("%s: err = %v, want an invalid request", c.name, err)
		}
		h.untouched(t)
	}
}

func TestRenderOfASubAccountIsProvenBeforeThePost(t *testing.T) {
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("5", "Sub") + `]}`, nil
		}
		return 200, renderedFixture, nil
	}
	if _, err := h.call(t, handleRenderAccountIssues, `{"account":"5"}`); err != nil {
		t.Fatal(err)
	}
	got := h.renderRequests(t)
	if len(got) != 2 || got[0].method != http.MethodGet || !strings.HasSuffix(got[0].path, ":listSubaccounts") ||
		got[1].method != http.MethodPost || got[1].path != "/issueresolution/v1/accounts/5:renderaccountissues" {
		t.Errorf("requests = %+v", got)
	}
	h = newHarness(t, "advanced-accounts/1")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{"accounts":[]}`, nil }
	if _, err := h.call(t, handleRenderProductIssues, `{"account":"9","product":"en~US~a"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v, want a refusal", err)
	}
	for _, req := range h.renderRequests(t) {
		if req.method == http.MethodPost {
			t.Error("a POST followed a failed proof")
		}
	}
}

func TestRenderIsRepeatedOnlyAfterARenewedTokenAndFailuresAreMapped(t *testing.T) {
	for _, c := range []struct {
		status int
		class  provider.Class
	}{
		{401, provider.ClassAuth}, {403, provider.ClassPermission}, {404, provider.ClassNotFound},
		{429, provider.ClassRateLimited}, {500, provider.ClassProviderError}, {503, provider.ClassUnreachable},
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return c.status, `{"error":{"status":"X","message":"https://leak.example.invalid"}}`, nil
		}
		_, err := h.call(t, handleRenderAccountIssues, `{"account":"7"}`)
		if classOf(err) != c.class {
			t.Errorf("status %d: class = %q, want %q (%v)", c.status, classOf(err), c.class, err)
		}
		// A read renews the token once after 401 and is sent again; no other answer repeats it.
		want := 1
		if c.status == 401 {
			want = 2
		}
		if n := len(h.google.seen()); n != want {
			t.Errorf("status %d: %d requests, want %d", c.status, n, want)
		}
	}
}

func TestRenderRefusesAnAnswerThatIsNotJSON(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `<html></html>`, http.Header{"Content-Type": {"text/html"}}
	}
	_, err := h.call(t, handleRenderAccountIssues, `{"account":"7"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}
