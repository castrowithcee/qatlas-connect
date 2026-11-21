package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// checkoutRequest is one request the fake saw: method, path with query, and the JSON body.
type checkoutRequest struct{ method, path, body string }

func (h *harness) checkoutRequests(t *testing.T) []checkoutRequest {
	t.Helper()
	var out []checkoutRequest
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" {
			t.Errorf("request left the fixed origin: %s", req.URL.Host)
		}
		if req.Header.Get("Authorization") != "Bearer "+testAccess || strings.Contains(req.URL.String(), testAccess) {
			t.Errorf("token missing from the header or present in the URL of %s", req.URL.Path)
		}
		body := ""
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			body = string(b)
		}
		path := req.URL.Path
		if req.URL.RawQuery != "" {
			path += "?" + req.URL.RawQuery
		}
		out = append(out, checkoutRequest{req.Method, path, body})
	}
	return out
}

const testCheckoutAnswer = `{"name":"accounts/1000002/programs/checkout/checkoutSettings",` +
	`"eligibleDestinations":["SHOPPING_ADS","FREE_LISTINGS"],` +
	`"effectiveUriSettings":{"checkoutUriTemplate":"https://shop.example.invalid/checkout?item_id={id}"},` +
	`"uriSettings":{"checkoutUriTemplate":"https://shop.example.invalid/checkout?item_id={id}"},` +
	`"enrollmentState":"ENROLLED","reviewState":"IN_REVIEW","effectiveEnrollmentState":"ENROLLED",` +
	`"effectiveReviewState":"APPROVED","unknownMember":"dropped"}`

const checkoutTemplate = "https://shop.example.invalid/checkout?item_id={id}"

func TestCheckoutDescriptorsCarryRiskAndToolListRequirement(t *testing.T) {
	for _, c := range []struct {
		d           capability.Descriptor
		effect      capability.Effect
		idempotency capability.Idempotency
		listed      bool
	}{
		{checkoutSettingsGet, capability.EffectRead, capability.IdempotencySafe, false},
		{checkoutSettingsCreate, capability.EffectCreate, capability.IdempotencyNonIdempotent, true},
		{checkoutSettingsUpdate, capability.EffectUpdate, capability.IdempotencyIdempotent, true},
		{checkoutSettingsDelete, capability.EffectDelete, capability.IdempotencyUnknown, true},
	} {
		r := c.d.Risk
		changes := c.effect != capability.EffectRead
		if c.d.Version != 1 || r.Effect != c.effect || r.Idempotency != c.idempotency || !r.OpenWorld ||
			(r.Confirmation == capability.ConfirmationRequired) != changes || c.d.RequiresToolAllowList != c.listed {
			t.Errorf("%s: version %d, risk %+v, tools list %t", c.d.ID, c.d.Version, r, c.d.RequiresToolAllowList)
		}
		if changes && !strings.Contains(c.d.Description, "never repeated") {
			t.Errorf("%s does not say the call is never repeated", c.d.ID)
		}
		if changes && !strings.Contains(c.d.Description, "reviews the template") {
			t.Errorf("%s does not name the delayed processing", c.d.ID)
		}
		if len(c.d.OutputSchema) == 0 || len(c.d.InputSchema) == 0 {
			t.Errorf("%s has no schema", c.d.ID)
		}
	}
	// Only the documented, writable members are arguments: no output-only member, no mask, no resource name, no
	// program.
	for _, d := range []capability.Descriptor{checkoutSettingsCreate, checkoutSettingsUpdate} {
		for _, name := range []string{"name", "update_mask", "updateMask", "program", "enrollment_state", "review_state",
			"effective_uri_settings", "effectiveUriSettings", "enrollmentState", "reviewState", "eligibleDestinations",
			"uriSettings"} {
			if strings.Contains(string(d.InputSchema), `"`+name+`"`) {
				t.Errorf("%s takes %s", d.ID, name)
			}
		}
		for _, name := range []string{"eligible_destinations", "uri_settings", "checkout_uri_template", "cart_uri_template"} {
			if !strings.Contains(string(d.InputSchema), `"`+name+`"`) {
				t.Errorf("%s does not take %s", d.ID, name)
			}
		}
	}
	for _, d := range []capability.Descriptor{checkoutSettingsGet, checkoutSettingsDelete} {
		if strings.Contains(string(d.InputSchema), "program") {
			t.Errorf("%s takes a program", d.ID)
		}
	}
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	tools := metadata.Profiles[0].Tools
	if !slices.Contains(tools, checkoutSettingsGetID) || slices.Contains(tools, checkoutSettingsCreateID) ||
		slices.Contains(tools, checkoutSettingsUpdateID) || slices.Contains(tools, checkoutSettingsDeleteID) {
		t.Errorf("profile tools = %v", tools)
	}
	if got, _ := metadata.Target.Reach(checkoutSettingsDeleteID, []string{"accounts/2"}); !got {
		t.Error("an account target is refused the delete tool")
	}
}

func TestCheckoutToolsSettleTheAccountBeforeSecretAndRequest(t *testing.T) {
	for _, tool := range []struct {
		name    string
		handler capability.Handler
		extra   string
	}{
		{"get", handleCheckoutGet, ``},
		{"create", handleCheckoutCreate, `,"eligible_destinations":["FREE_LISTINGS"]`},
		{"update", handleCheckoutUpdate, `,"eligible_destinations":["FREE_LISTINGS"]`},
		{"delete", handleCheckoutDelete, ``},
	} {
		for name, c := range map[string]struct {
			targets []string
			account string
		}{
			"outside the accounts":       {[]string{"accounts/1"}, `"account":"2"`},
			"not an ID":                  {nil, `"account":"1/users"`},
			"leading zero":               {nil, `"account":"01"`},
			"two targets, no argument":   {[]string{"accounts/1", "accounts/2"}, ``},
			"no target and no argument":  {nil, ``},
			"advanced target, other one": {[]string{"advanced-accounts/1", "accounts/2"}, `"account":"3x"`},
		} {
			t.Run(tool.name+" "+name, func(t *testing.T) {
				h := newHarness(t, c.targets...)
				args := `{` + c.account + tool.extra + `}`
				if c.account == "" {
					args = `{` + strings.TrimPrefix(tool.extra, ",") + `}`
				}
				if _, err := tool.handler(t.Context(), h.resolved, h.secrets, h.red, json.RawMessage(args)); !isInvalidRequest(err) {
					t.Errorf("err = %v, want invalid request", err)
				}
				h.untouched(t)
			})
		}
	}
}

func TestCheckoutWritesNameTheirAccountExplicitly(t *testing.T) {
	// One target would be enough for a read, but a change must say which account it changes.
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"create": {handleCheckoutCreate, `{"eligible_destinations":["FREE_LISTINGS"]}`},
		"update": {handleCheckoutUpdate, `{"eligible_destinations":["FREE_LISTINGS"]}`},
		"delete": {handleCheckoutDelete, `{}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1")
			if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestCheckoutTemplatesMustBePlainHTTPSBeforeSecretAndRequest(t *testing.T) {
	bad := []string{
		"http://shop.example.invalid/checkout?item_id={id}",
		"https://user" + at + "shop.example.invalid/checkout?item_id={id}",
		"https://user:word" + at + "shop.example.invalid/checkout",
		"https://shop.example.invalid\\@other.example.invalid/checkout",
		"https://shop.example.invalid/check out?item_id={id}",
		"https://shop.example.invalid/checkout\n?item_id={id}",
		"javascript:alert(1)",
		"//shop.example.invalid/checkout",
		"shop.example.invalid/checkout?item_id={id}",
		"https:///checkout?item_id={id}",
		"",
		"https://shop.example.invalid/" + strings.Repeat("a", 2048),
	}
	for _, kind := range []string{"checkout_uri_template", "cart_uri_template"} {
		for _, uri := range bad {
			for tool, handler := range map[string]capability.Handler{"create": handleCheckoutCreate,
				"update": handleCheckoutUpdate} {
				h := newHarness(t, "accounts/1")
				args := `{"account":"1","eligible_destinations":["FREE_LISTINGS"],"uri_settings":{"` + kind + `":` +
					checkoutQuoted(uri) + `}}`
				if _, err := h.call(t, handler, args); !isInvalidRequest(err) {
					t.Errorf("%s %s %.40q: err = %v, want invalid request", tool, kind, uri, err)
				}
				h.untouched(t)
			}
		}
	}
}

func checkoutQuoted(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestCheckoutArgumentsAreCheckedBeforeSecretAndRequest(t *testing.T) {
	both := `"uri_settings":{"checkout_uri_template":"` + checkoutTemplate + `","cart_uri_template":"` + checkoutTemplate + `"}`
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"create: no destinations":       {handleCheckoutCreate, `{"account":"1"}`},
		"create: empty destinations":    {handleCheckoutCreate, `{"account":"1","eligible_destinations":[]}`},
		"create: unknown destination":   {handleCheckoutCreate, `{"account":"1","eligible_destinations":["DESTINATION_UNSPECIFIED"]}`},
		"create: lower case":            {handleCheckoutCreate, `{"account":"1","eligible_destinations":["free_listings"]}`},
		"create: repeated destination":  {handleCheckoutCreate, `{"account":"1","eligible_destinations":["FREE_LISTINGS","FREE_LISTINGS"]}`},
		"create: three destinations":    {handleCheckoutCreate, `{"account":"1","eligible_destinations":["FREE_LISTINGS","SHOPPING_ADS","FREE_LISTINGS"]}`},
		"create: both templates":        {handleCheckoutCreate, `{"account":"1","eligible_destinations":["FREE_LISTINGS"],` + both + `}`},
		"create: empty settings":        {handleCheckoutCreate, `{"account":"1","eligible_destinations":["FREE_LISTINGS"],"uri_settings":{}}`},
		"create: template not a string": {handleCheckoutCreate, `{"account":"1","eligible_destinations":["FREE_LISTINGS"],"uri_settings":{"cart_uri_template":3}}`},
		"update: no member":             {handleCheckoutUpdate, `{"account":"1"}`},
		"update: empty destinations":    {handleCheckoutUpdate, `{"account":"1","eligible_destinations":[]}`},
		"update: both templates":        {handleCheckoutUpdate, `{"account":"1",` + both + `}`},
		"update: empty settings":        {handleCheckoutUpdate, `{"account":"1","uri_settings":{}}`},
		"update: arguments not JSON":    {handleCheckoutUpdate, `{"account":"1","uri_settings":"` + checkoutTemplate + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1")
			if _, err := h.call(t, c.handler, c.args); err == nil {
				t.Error("no error")
			}
			h.untouched(t)
		})
	}
}

func TestCheckoutTemplatesWithTheDocumentedPlaceholderPass(t *testing.T) {
	for _, template := range []string{
		"https://www.shop.example.invalid/checkout?item_id={id}",
		"https://shop.example.invalid/add_to_cart?sku={id}",
		"https://shop.example.invalid/cart/{id}:1",
		"https://shop.example.invalid/checkout?item_id={ID}",
	} {
		for _, kind := range []string{"checkout_uri_template", "cart_uri_template"} {
			h := newHarness(t, "accounts/1")
			h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testCheckoutAnswer, nil }
			if _, err := h.call(t, handleCheckoutCreate, `{"account":"1","eligible_destinations":["SHOPPING_ADS"],`+
				`"uri_settings":{"`+kind+`":`+checkoutQuoted(template)+`}}`); err != nil {
				t.Errorf("%s %q refused: %v", kind, template, err)
			}
			got := h.checkoutRequests(t)
			if len(got) != 1 || !strings.Contains(got[0].body, checkoutQuoted(template)) {
				t.Errorf("%s %q: requests = %v", kind, template, got)
			}
		}
	}
}

func TestCheckoutGetReadsTheFixedProgramAndShowsEveryMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testCheckoutAnswer, nil }
	out, err := h.call(t, handleCheckoutGet, `{"account":"1000002"}`)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"name": "accounts/1000002/programs/checkout/checkoutSettings",
		"enrollmentState": "ENROLLED", "reviewState": "IN_REVIEW", "effectiveEnrollmentState": "ENROLLED",
		"effectiveReviewState": "APPROVED"} {
		if out[key] != want {
			t.Errorf("%s = %v, want %s", key, out[key], want)
		}
	}
	for _, key := range []string{"uriSettings", "effectiveUriSettings"} {
		settings := out[key].(map[string]any)
		if settings["checkoutUriTemplate"] != checkoutTemplate || settings["cartUriTemplate"] != nil {
			t.Errorf("%s = %v", key, settings)
		}
	}
	destinations := out["eligibleDestinations"].([]any)
	if len(destinations) != 2 || destinations[0] != "SHOPPING_ADS" || destinations[1] != "FREE_LISTINGS" ||
		out["unknownMember"] != nil {
		t.Errorf("out = %v", out)
	}
	if got := h.checkoutRequests(t); len(got) != 1 ||
		got[0] != (checkoutRequest{"GET", "/accounts/v1/accounts/1000002/programs/checkout/checkoutSettings", ""}) {
		t.Errorf("requests = %v", got)
	}
	// An account without argument is the only bound one, and a template longer than the bound is cut.
	h = newHarness(t, "accounts/7")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"uriSettings":{"cartUriTemplate":"https://shop.example.invalid/` + strings.Repeat("a", 3000) + `"}}`, nil
	}
	out, err = h.call(t, handleCheckoutGet, `{}`)
	cart, _ := out["uriSettings"].(map[string]any)["cartUriTemplate"].(string)
	if err != nil || len([]rune(cart)) != maxURLRunes {
		t.Errorf("cart template of %d runes, %v", len([]rune(cart)), err)
	}
	if got := h.checkoutRequests(t); got[0].path != "/accounts/v1/accounts/7/programs/checkout/checkoutSettings" {
		t.Errorf("requests = %v", got)
	}
}

func TestCheckoutReadsAndWritesProveASubAccountOfABoundAdvancedAccount(t *testing.T) {
	proof := func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("2", "Two") + `]}`, nil
		}
		return 200, testCheckoutAnswer, nil
	}
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = proof
	if _, err := h.call(t, handleCheckoutGet, `{"account":"2"}`); err != nil {
		t.Fatal(err)
	}
	if got := h.checkoutRequests(t); len(got) != 2 || !strings.HasSuffix(got[0].path, "/accounts/1:listSubaccounts?pageSize=500") ||
		got[1].path != "/accounts/v1/accounts/2/programs/checkout/checkoutSettings" {
		t.Errorf("requests = %v", got)
	}
	// A proven account is changed, an unproven one is refused after the proof read and never changed.
	for name, handler := range map[string]capability.Handler{"create": handleCheckoutCreate,
		"update": handleCheckoutUpdate, "delete": handleCheckoutDelete} {
		args := `,"eligible_destinations":["FREE_LISTINGS"]`
		if name == "delete" {
			args = ``
		}
		h = newHarness(t, "advanced-accounts/1")
		h.google.api = proof
		if _, err := h.call(t, handler, `{"account":"2"`+args+`}`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		h = newHarness(t, "advanced-accounts/1")
		h.google.api = proof
		if _, err := h.call(t, handler, `{"account":"3"`+args+`}`); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
		for _, r := range h.checkoutRequests(t) {
			if r.method != "GET" {
				t.Errorf("%s: request %v", name, r)
			}
		}
	}
}

func TestCheckoutCreateSendsOnlyThePassedMembersToTheFixedProgram(t *testing.T) {
	h := newHarness(t, "accounts/1000002")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testCheckoutAnswer, nil }
	out, err := h.call(t, handleCheckoutCreate, `{"account":"1000002","eligible_destinations":["FREE_LISTINGS","SHOPPING_ADS"],`+
		`"uri_settings":{"checkout_uri_template":`+checkoutQuoted(checkoutTemplate)+`}}`)
	if err != nil || out["reviewState"] != "IN_REVIEW" || out["enrollmentState"] != "ENROLLED" {
		t.Fatalf("out = %v, %v", out, err)
	}
	got := h.checkoutRequests(t)
	want := `{"eligibleDestinations":["FREE_LISTINGS","SHOPPING_ADS"],"uriSettings":{"checkoutUriTemplate":` +
		checkoutQuoted(checkoutTemplate) + `}}`
	if len(got) != 1 || got[0].method != "POST" ||
		got[0].path != "/accounts/v1/accounts/1000002/programs/checkout/checkoutSettings" || got[0].body != want {
		t.Fatalf("requests = %v\nwant body %s", got, want)
	}
	// Destinations alone are enough, for products that carry their own checkout links.
	h = newHarness(t, "accounts/1000002")
	if _, err := h.call(t, handleCheckoutCreate, `{"account":"1000002","eligible_destinations":["SHOPPING_ADS"]}`); err != nil {
		t.Fatal(err)
	}
	if got := h.checkoutRequests(t); got[0].body != `{"eligibleDestinations":["SHOPPING_ADS"]}` {
		t.Errorf("body = %s", got[0].body)
	}
}

func TestCheckoutUpdateMasksOnlyThePassedMembersInLowerCamelCase(t *testing.T) {
	for name, c := range map[string]struct{ args, mask, body string }{
		"destinations": {`"eligible_destinations":["FREE_LISTINGS"]`, "eligibleDestinations",
			`{"eligibleDestinations":["FREE_LISTINGS"]}`},
		"checkout": {`"uri_settings":{"checkout_uri_template":"` + checkoutTemplate + `"}`, "uriSettings",
			`{"uriSettings":{"checkoutUriTemplate":"` + checkoutTemplate + `"}}`},
		"cart": {`"uri_settings":{"cart_uri_template":"https://shop.example.invalid/cart?sku={id}"}`, "uriSettings",
			`{"uriSettings":{"cartUriTemplate":"https://shop.example.invalid/cart?sku={id}"}}`},
		"both": {`"eligible_destinations":["SHOPPING_ADS"],"uri_settings":{"cart_uri_template":"https://shop.example.invalid/c"}`,
			"eligibleDestinations,uriSettings",
			`{"eligibleDestinations":["SHOPPING_ADS"],"uriSettings":{"cartUriTemplate":"https://shop.example.invalid/c"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1000002")
			h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testCheckoutAnswer, nil }
			if _, err := h.call(t, handleCheckoutUpdate, `{"account":"1000002",`+c.args+`}`); err != nil {
				t.Fatal(err)
			}
			got := h.checkoutRequests(t)
			wantPath := "/accounts/v1/accounts/1000002/programs/checkout/checkoutSettings?updateMask=" +
				strings.ReplaceAll(c.mask, ",", "%2C")
			if len(got) != 1 || got[0].method != "PATCH" || got[0].path != wantPath || got[0].body != c.body {
				t.Errorf("requests = %v\nwant %s %s", got, wantPath, c.body)
			}
		})
	}
}

func TestCheckoutDeleteSendsOneBodylessDeleteAndReportsAcceptance(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
	}{"empty object": {200, `{}`}, "no content": {204, ``}} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1000002")
			h.google.api = func(*http.Request) (int, string, http.Header) { return c.status, c.body, nil }
			out, err := h.call(t, handleCheckoutDelete, `{"account":"1000002"}`)
			if err != nil || out["account"] != "1000002" || out["deleted"] != true || len(out) != 2 {
				t.Fatalf("out = %v, %v", out, err)
			}
			if got := h.checkoutRequests(t); len(got) != 1 || got[0] != (checkoutRequest{"DELETE",
				"/accounts/v1/accounts/1000002/programs/checkout/checkoutSettings", ""}) {
				t.Errorf("requests = %v", got)
			}
		})
	}
}

func TestCheckoutChangeIsSentOnceAndAnUnclearOutcomeIsSaid(t *testing.T) {
	for name, c := range map[string]struct {
		status      int
		unclearText bool
		class       provider.Class
	}{
		"server error":  {500, true, provider.ClassProviderError},
		"unauthorized":  {401, false, provider.ClassAuth},
		"rate limited":  {429, false, provider.ClassRateLimited},
		"bad request":   {400, false, ""},
		"not found":     {404, false, provider.ClassNotFound},
		"unavailable":   {503, true, provider.ClassUnreachable},
		"gateway error": {502, true, provider.ClassProviderError},
	} {
		for tool, call := range map[string]struct {
			handler capability.Handler
			args    string
		}{
			"create": {handleCheckoutCreate, `{"account":"1","eligible_destinations":["FREE_LISTINGS"]}`},
			"update": {handleCheckoutUpdate, `{"account":"1","eligible_destinations":["FREE_LISTINGS"]}`},
			"delete": {handleCheckoutDelete, `{"account":"1"}`},
		} {
			t.Run(tool+" "+name, func(t *testing.T) {
				h := newHarness(t, "accounts/1")
				h.google.api = func(*http.Request) (int, string, http.Header) {
					return c.status, `{"error":{"code":0,"message":"untrusted text","status":"X"}}`, nil
				}
				_, err := h.call(t, call.handler, call.args)
				if err == nil {
					t.Fatal("no error")
				}
				if c.class != "" && classOf(err) != c.class {
					t.Errorf("class = %s, want %s", classOf(err), c.class)
				}
				if unclear := strings.Contains(err.Error(), "outcome of the change is unknown"); unclear != c.unclearText {
					t.Errorf("unclear outcome said = %t: %v", unclear, err)
				}
				if strings.Contains(err.Error(), "untrusted text") {
					t.Errorf("the message carries Google's text: %v", err)
				}
				if got := h.checkoutRequests(t); len(got) != 1 {
					t.Errorf("%d requests, want exactly one", len(got))
				}
			})
		}
	}
}

func TestCheckoutConfirmationSummariesNameTheChange(t *testing.T) {
	summaries := checkoutSummaries()
	if len(summaries) != 3 || summaries[checkoutSettingsGetID] != nil {
		t.Fatalf("summaries = %v", summaries)
	}
	create := summaries[checkoutSettingsCreateID](json.RawMessage(`{"account":"1","eligible_destinations":["FREE_LISTINGS"],` +
		`"uri_settings":{"checkout_uri_template":` + checkoutQuoted(checkoutTemplate) + `}}`))
	if !strings.Contains(create, "account 1") || !strings.Contains(create, "FREE_LISTINGS") ||
		!strings.Contains(create, "checkout template "+checkoutQuoted(checkoutTemplate)) {
		t.Errorf("create summary = %q", create)
	}
	update := summaries[checkoutSettingsUpdateID](json.RawMessage(`{"account":"1","eligible_destinations":["SHOPPING_ADS"],` +
		`"uri_settings":{"cart_uri_template":"https://shop.example.invalid/c"}}`))
	if !strings.Contains(update, "set eligibleDestinations, uriSettings") ||
		!strings.Contains(update, `cart template "https://shop.example.invalid/c"`) {
		t.Errorf("update summary = %q", update)
	}
	if del := summaries[checkoutSettingsDeleteID](json.RawMessage(`{"account":"1"}`)); !strings.Contains(del, "account 1") ||
		!strings.Contains(del, "unenroll") {
		t.Errorf("delete summary = %q", del)
	}
	for id, summary := range summaries {
		if summary(json.RawMessage(`[`)) != "" {
			t.Errorf("%s: a summary of unreadable arguments is not empty", id)
		}
	}
}
