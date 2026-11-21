package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const serviceRow = `{"name":"accounts/1000001/services/2000002","provider":"providers/777",` +
	`"providerDisplayName":"Acme Platform","externalAccountId":"shop-9","handshake":{"actor":"OTHER_PARTY",` +
	`"approvalState":"PENDING","extra":"never held"},"mutability":"MUTABLE","accountAggregation":{},` +
	`"productsManagement":{"unknown":1},"unknownMember":"never held"}`

// serviceRequests lists method, path, and query of the requests so far, and checks the origin and the token.
func serviceRequests(t *testing.T, h *harness) []string {
	t.Helper()
	var out []string
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" {
			t.Errorf("request left the fixed origin: %s", req.URL.Host)
		}
		if req.Header.Get("Authorization") != "Bearer "+testAccess || strings.Contains(req.URL.String(), testAccess) {
			t.Errorf("request %s has the token in the wrong place", req.URL.Path)
		}
		line := req.Method + " " + req.URL.Path
		if req.URL.RawQuery != "" {
			line += "?" + req.URL.RawQuery
		}
		out = append(out, line)
	}
	return out
}

var serviceHandlers = map[string]capability.Handler{"list": handleAccountServicesList, "get": handleAccountServicesGet,
	"reject": handleAccountServicesReject}

func TestServiceToolsAreDeclaredWithTheirRiskAndOmitProposeAndApprove(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{accountServicesListID, accountServicesGetID, accountRelationshipsListID} {
		d, handler, ok := reg.Lookup(id)
		if !ok || handler == nil || d.Risk != readRisk || d.RequiresToolAllowList || d.Version != 1 {
			t.Errorf("%s = %+v", id, d)
		}
	}
	d, handler, ok := reg.Lookup(accountServicesRejectID)
	if !ok || handler == nil || d.Version != 1 || !d.RequiresToolAllowList || d.Risk.Effect != capability.EffectUpdate ||
		d.Risk.Confirmation != capability.ConfirmationRequired {
		t.Fatalf("reject = %+v", d)
	}
	for _, want := range []string{"Admin", "loses that access", "afterwards", "never repeated", "tools", "IMMUTABLE"} {
		if !strings.Contains(d.Description, want) {
			t.Errorf("reject description lacks %q", want)
		}
	}
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, accountServicesRejectID) {
			t.Errorf("profile %s lists reject", profile.ID)
		}
	}
	for _, id := range []string{"propose", "approve"} {
		if _, _, found := reg.Lookup(Provider + ".account-services." + id); found {
			t.Errorf("account-services.%s is registered", id)
		}
		for _, registered := range reg.Provider(Provider) {
			if strings.Contains(registered.ID, id) {
				t.Errorf("%s is registered", registered.ID)
			}
		}
	}
	got := serviceRejectSummary(json.RawMessage(`{"account":"1000001","service":"2000002"}`))
	if !strings.HasPrefix(got, `Reject service "2000002" of account 1000001`) || !strings.Contains(got, "loses the access") {
		t.Errorf("summary = %q", got)
	}
	if got := serviceRejectSummary(json.RawMessage(`{"service":"s"}`)); !strings.Contains(got, "account of this connection") {
		t.Errorf("summary without account = %q", got)
	}
}

func TestAccountServicesListSendsTheDocumentedParametersAndShowsEveryDocumentedMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "page-two" {
			return 200, `{"accountServices":[{"name":"accounts/1000001/services/3","comparisonShopping":{}}]}`, nil
		}
		return 200, `{"accountServices":[` + serviceRow + `],"nextPageToken":"page-two"}`, nil
	}
	first, err := h.call(t, handleAccountServicesList, `{"account":"1000001"}`)
	if err != nil {
		t.Fatal(err)
	}
	row := first["accountServices"].([]any)[0].(map[string]any)
	handshake := row["handshake"].(map[string]any)
	if row["name"] != "accounts/1000001/services/2000002" || row["provider"] != "providers/777" ||
		row["providerDisplayName"] != "Acme Platform" || row["externalAccountId"] != "shop-9" ||
		row["mutability"] != "MUTABLE" || handshake["approvalState"] != "PENDING" || handshake["actor"] != "OTHER_PARTY" ||
		len(handshake) != 2 || row["unknownMember"] != nil || len(row["accountAggregation"].(map[string]any)) != 0 ||
		row["accountAggregation"] == nil || len(row["productsManagement"].(map[string]any)) != 0 ||
		row["campaignsManagement"] != nil || len(row) != 8 {
		t.Fatalf("row = %v", row)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "page-two" {
		t.Fatalf("next_cursor = %q", cursor)
	}
	second, err := h.call(t, handleAccountServicesList, `{"account":"1000001","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || len(second["accountServices"].([]any)) != 1 {
		t.Fatalf("second = %v, %v", second, err)
	}
	if _, err := h.call(t, handleAccountServicesList, `{"account":"1000001","page_size":5,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments: %v", err)
	}
	if _, err := h.call(t, handleAccountServicesList, `{"account":"1000001","page_size":1000}`); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /accounts/v1/accounts/1000001/services?pageSize=100",
		"GET /accounts/v1/accounts/1000001/services?pageSize=100&pageToken=page-two",
		"GET /accounts/v1/accounts/1000001/services?pageSize=1000"}
	if got := serviceRequests(t, h); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

func TestAnEmptyServiceAnswerIsAnEmptyListAndOddMembersAreDropped(t *testing.T) {
	h := newHarness(t)
	out, err := h.call(t, handleAccountServicesList, `{"account":"1"}`)
	if err != nil || out["accountServices"] == nil || len(out["accountServices"].([]any)) != 0 {
		t.Fatalf("out = %v, %v", out, err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/1/services/s","handshake":"x","accountAggregation":"yes","provider":5,"mutability":["a"]}`, nil
	}
	got, err := h.call(t, handleAccountServicesGet, `{"account":"1","service":"s"}`)
	if err != nil || got["handshake"] != nil || got["accountAggregation"] != nil || got["provider"] != nil ||
		got["mutability"] != nil || got["name"] != "accounts/1/services/s" {
		t.Fatalf("got = %v, %v", got, err)
	}
}

func TestAccountServicesGetReadsOneService(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, serviceRow, nil }
	out, err := h.call(t, handleAccountServicesGet, `{"account":"1000001","service":"2000002"}`)
	if err != nil || out["provider"] != "providers/777" {
		t.Fatalf("out = %v, %v", out, err)
	}
	if got := serviceRequests(t, h); !slices.Equal(got, []string{"GET /accounts/v1/accounts/1000001/services/2000002"}) {
		t.Errorf("requests = %v", got)
	}
}

func TestServiceTextsAndListsAreBounded(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"accountServices":[` + strings.Repeat(`{},`, maxEntries) + `{}]}`, nil
	}
	if _, err := h.call(t, handleAccountServicesList, `{"account":"1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"` + strings.Repeat("n", 500) + `","providerDisplayName":"` + strings.Repeat("d", 500) +
			`","externalAccountId":"` + strings.Repeat("e", 500) + `"}`, nil
	}
	out, err := h.call(t, handleAccountServicesGet, `{"account":"1","service":"s"}`)
	if err != nil || len([]rune(out["name"].(string))) != maxIDRunes ||
		len([]rune(out["providerDisplayName"].(string))) != maxNameRunes ||
		len([]rune(out["externalAccountId"].(string))) != maxIDRunes {
		t.Errorf("bounds not kept: %v, %v", out, err)
	}
}

func TestRejectSendsOneFixedRequestAndReportsAcceptance(t *testing.T) {
	h := newHarness(t)
	var body string
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
		if req.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", req.Header.Get("Content-Type"))
		}
		return 200, `{}`, nil
	}
	out, err := h.call(t, handleAccountServicesReject, `{"account":"1000001","service":"2000002"}`)
	if err != nil || out["accepted"] != true || out["name"] != "accounts/1000001/services/2000002" {
		t.Fatalf("out = %v, %v", out, err)
	}
	want := "POST /accounts/v1/accounts/1000001/services/2000002:reject"
	if got := serviceRequests(t, h); !slices.Equal(got, []string{want}) || body != `{}` {
		t.Errorf("requests = %v, body = %q", got, body)
	}
}

func TestServiceIDAndAccountAreCheckedBeforeSecretAndRequest(t *testing.T) {
	for _, bad := range []string{"", "a/b", "..", "a.b", "a b", "a:reject", "a?x=1", "a%2Fb", "a/../b", "a#", "é",
		strings.Repeat("a", 65), "accounts/1/services/2"} {
		for name, handler := range serviceHandlers {
			if name == "list" {
				continue
			}
			h := newHarness(t)
			encoded, _ := json.Marshal(map[string]string{"account": "1", "service": bad})
			if _, err := h.call(t, handler, string(encoded)); !isInvalidRequest(err) {
				t.Errorf("%s %q: err = %v", name, bad, err)
			}
			h.untouched(t)
		}
	}
	for _, args := range []string{`{"account":"1","service":"s","extra":1}`, `{"account":"01","service":"s"}`,
		`{"account":"1/2","service":"s"}`, `{"service":"s"}`, `{"account":"1","service":"s","cursor":"x"}`,
		`{"account":"1","service":"s","page_size":3}`} {
		for name, handler := range serviceHandlers {
			if name == "list" {
				continue
			}
			h := newHarness(t)
			if _, err := h.call(t, handler, args); !isInvalidRequest(err) {
				t.Errorf("%s %s: err = %v", name, args, err)
			}
			h.untouched(t)
		}
	}
	for _, args := range []string{`{"account":"1","service":"s"}`, `{"account":"x"}`} {
		for name, handler := range map[string]capability.Handler{"list": handleAccountServicesList,
			"relationships": handleAccountRelationshipsList} {
			h := newHarness(t)
			if _, err := h.call(t, handler, args); !isInvalidRequest(err) {
				t.Errorf("%s %s: err = %v", name, args, err)
			}
			h.untouched(t)
		}
	}
}

func TestServiceToolsNeedTheAdvancedAccountTargetAndRefuseOthersWithoutNamingThem(t *testing.T) {
	for _, targets := range [][]string{{"accounts/7"}, {"advanced-accounts/10"}} {
		for name, handler := range serviceHandlers {
			h := newHarness(t, targets...)
			// 7 is an account target and 4242 lies outside every target; neither is an advanced account of the binding.
			for _, account := range []string{"7", "4242", "11"} {
				_, err := h.call(t, handler, `{"account":"`+account+`","service":"s"}`)
				if name == "list" {
					_, err = h.call(t, handler, `{"account":"`+account+`"}`)
				}
				if !isInvalidRequest(err) || strings.Contains(err.Error(), account) {
					t.Errorf("%v %s %s: err = %v", targets, name, account, err)
				}
			}
			h.untouched(t)
		}
	}
	// With the advanced account bound, the account argument may be left out and is that account.
	h := newHarness(t, "advanced-accounts/10")
	if _, err := h.call(t, handleAccountServicesGet, `{"service":"s"}`); err != nil {
		t.Fatal(err)
	}
	if got := serviceRequests(t, h); !slices.Equal(got, []string{"GET /accounts/v1/accounts/10/services/s"}) {
		t.Errorf("requests = %v", got)
	}
}

func TestServiceToolsAreWithheldFromAConnectionWithoutTheAdvancedTarget(t *testing.T) {
	for _, id := range []string{accountServicesListID, accountServicesGetID, accountServicesRejectID} {
		if ok, want := reaches(id, []string{"accounts/7"}); ok || !slices.Equal(want, []string{formAdvanced}) {
			t.Errorf("%s reaches an account target: %v %v", id, ok, want)
		}
		if ok, _ := reaches(id, []string{"advanced-accounts/10", "accounts/7"}); !ok {
			t.Errorf("%s does not reach the advanced target", id)
		}
		if ok, _ := reaches(id, nil); !ok {
			t.Errorf("%s does not reach an unbound connection", id)
		}
	}
	if ok, _ := reaches(accountRelationshipsListID, []string{"accounts/7"}); !ok {
		t.Error("relationships.list is withheld from an account target")
	}
}

func TestAccountRelationshipsListShowsEveryDocumentedMemberAndPages(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "page-two" {
			return 200, `{"accountRelationships":[{"name":"accounts/1/relationships/2","provider":"providers/GOOGLE_ADS",` +
				`"providerDisplayName":"Google Ads"}]}`, nil
		}
		return 200, `{"accountRelationships":[{"name":"accounts/1/relationships/1","provider":"providers/55",` +
			`"providerDisplayName":"Shop","accountIdAlias":"my-alias","unknownMember":1}],"nextPageToken":"page-two"}`, nil
	}
	first, err := h.call(t, handleAccountRelationshipsList, `{"account":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	row := first["accountRelationships"].([]any)[0].(map[string]any)
	if row["name"] != "accounts/1/relationships/1" || row["provider"] != "providers/55" ||
		row["providerDisplayName"] != "Shop" || row["accountIdAlias"] != "my-alias" || len(row) != 4 {
		t.Fatalf("row = %v", row)
	}
	cursor, _ := first["next_cursor"].(string)
	second, err := h.call(t, handleAccountRelationshipsList, `{"account":"1","cursor":"`+cursor+`","page_size":100}`)
	if !isInvalidRequest(err) {
		t.Fatalf("a cursor of other arguments: %v, %v", second, err)
	}
	second, err = h.call(t, handleAccountRelationshipsList, `{"account":"1","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || len(second["accountRelationships"].([]any)) != 1 {
		t.Fatalf("second = %v, %v", second, err)
	}
	want := []string{"GET /accounts/v1/accounts/1/relationships?pageSize=100",
		"GET /accounts/v1/accounts/1/relationships?pageSize=100&pageToken=page-two"}
	if got := serviceRequests(t, h); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

func TestProviderDisplayNamesOutsideTheBindingAreMasked(t *testing.T) {
	rows := `{"accountRelationships":[` +
		`{"name":"accounts/10/relationships/1","provider":"providers/10","providerDisplayName":"Bound"},` +
		`{"name":"accounts/10/relationships/2","provider":"providers/999","providerDisplayName":"Foreign"},` +
		`{"name":"accounts/10/relationships/3","provider":"providers/GOOGLE_ADS","providerDisplayName":"Ads"},` +
		`{"name":"accounts/10/relationships/4","provider":"other","providerDisplayName":"Odd","accountIdAlias":"a"}]}`
	services := `{"accountServices":[` +
		`{"name":"accounts/10/services/1","provider":"providers/10","providerDisplayName":"Bound"},` +
		`{"name":"accounts/10/services/2","provider":"providers/999","providerDisplayName":"Foreign"},` +
		`{"name":"accounts/10/services/3","provider":"providers/GOOGLE_BUSINESS_PROFILE","providerDisplayName":"Gbp"}]}`
	names := func(out map[string]any, list string) []any {
		var got []any
		for _, row := range out[list].([]any) {
			got = append(got, row.(map[string]any)["providerDisplayName"])
		}
		return got
	}
	for _, targets := range [][]string{{"advanced-accounts/10"}, {"accounts/10"}} {
		h := newHarness(t, targets...)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, rows, nil }
		out, err := h.call(t, handleAccountRelationshipsList, `{"account":"10"}`)
		if err != nil || !slices.Equal(names(out, "accountRelationships"), []any{"Bound", nil, "Ads", nil}) {
			t.Fatalf("%v relationships = %v, %v", targets, out, err)
		}
		if row := out["accountRelationships"].([]any)[1].(map[string]any); row["provider"] != "providers/999" {
			t.Errorf("the provider identifier is gone: %v", row)
		}
		if targets[0] != "advanced-accounts/10" {
			continue
		}
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, services, nil }
		out, err = h.call(t, handleAccountServicesList, `{}`)
		if err != nil || !slices.Equal(names(out, "accountServices"), []any{"Bound", nil, "Gbp"}) {
			t.Fatalf("services = %v, %v", out, err)
		}
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return 200, `{"name":"accounts/10/services/s","provider":"providers/10","providerDisplayName":"Bound"}`, nil
		}
		got, err := h.call(t, handleAccountServicesGet, `{"service":"s"}`)
		if err != nil || got["providerDisplayName"] != "Bound" {
			t.Fatalf("get = %v, %v", got, err)
		}
	}
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, rows, nil }
	out, err := h.call(t, handleAccountRelationshipsList, `{"account":"10"}`)
	if err != nil || !slices.Equal(names(out, "accountRelationships"), []any{"Bound", "Foreign", "Ads", "Odd"}) {
		t.Fatalf("unbound relationships = %v, %v", out, err)
	}
}

func TestRelationshipsOfASubAccountAreProvenFirstAndOthersAreRefused(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		}
		return 200, `{"accountRelationships":[]}`, nil
	}
	if _, err := h.call(t, handleAccountRelationshipsList, `{"account":"11"}`); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /accounts/v1/accounts/10:listSubaccounts?pageSize=500",
		"GET /accounts/v1/accounts/11/relationships?pageSize=100"}
	if got := serviceRequests(t, h); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
	h = newHarness(t, "advanced-accounts/10")
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
	}
	_, err := h.call(t, handleAccountRelationshipsList, `{"account":"4242"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "4242") {
		t.Fatalf("err = %v", err)
	}
	if got := serviceRequests(t, h); len(got) != 1 || !strings.Contains(got[0], ":listSubaccounts") {
		t.Errorf("requests = %v, want only the proof read", got)
	}
	h = newHarness(t, "accounts/7")
	if _, err := h.call(t, handleAccountRelationshipsList, `{"account":"4242"}`); !isInvalidRequest(err) {
		t.Errorf("outside: %v", err)
	}
	h.untouched(t)
}

func TestAFailedRejectIsNeverRepeatedAndSaysWhenTheOutcomeIsOpen(t *testing.T) {
	type answer struct {
		status int
		body   string
		header http.Header
	}
	for name, c := range map[string]struct {
		answer  answer
		fail    error
		class   provider.Class
		unknown bool
	}{
		"401":      {answer: answer{401, `{}`, nil}, class: provider.ClassAuth},
		"403":      {answer: answer{403, googleErrorBody, nil}, class: provider.ClassPermission},
		"404":      {answer: answer{404, `{}`, nil}, class: provider.ClassNotFound},
		"429":      {answer: answer{429, quotaBody(reasonDayQuota), http.Header{"Retry-After": {"30"}}}, class: provider.ClassRateLimited},
		"500":      {answer: answer{500, `{}`, nil}, class: provider.ClassProviderError, unknown: true},
		"not JSON": {answer: answer{200, `<html>`, http.Header{"Content-Type": {"text/html"}}}, class: provider.ClassInvalidResponse, unknown: true},
		"broken":   {answer: answer{200, `{"name":`, nil}, class: provider.ClassInvalidResponse, unknown: true},
		"timeout":  {fail: http.ErrHandlerTimeout, unknown: true},
	} {
		h := newHarness(t)
		h.google.fail = c.fail
		h.google.api = func(*http.Request) (int, string, http.Header) { return c.answer.status, c.answer.body, c.answer.header }
		_, err := h.call(t, handleAccountServicesReject, `{"account":"1","service":"s"}`)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		if got := classOf(err); c.class != "" && got != c.class {
			t.Errorf("%s: class = %q, want %q (%v)", name, got, c.class, err)
		}
		if got := len(h.google.seen()); got != 1 {
			t.Errorf("%s: %d requests, want exactly one", name, got)
		}
		if open := strings.Contains(err.Error(), "outcome of the change is unknown"); open != c.unknown {
			t.Errorf("%s: outcome note = %t, want %t (%v)", name, open, c.unknown, err)
		}
		if strings.Contains(err.Error(), "merchantapi.googleapis.com") {
			t.Errorf("%s: error quotes the URL: %v", name, err)
		}
	}
}

func TestARateLimitedRejectHoldsTheNextRequest(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 429, quotaBody(reasonMinuteQuota), http.Header{"Retry-After": {"30"}}
	}
	if _, err := h.call(t, handleAccountServicesReject, `{"account":"1","service":"s"}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("err = %v", err)
	}
	h.google.api = nil
	if _, err := h.call(t, handleAccountServicesGet, `{"account":"1","service":"s"}`); err != nil {
		t.Fatal(err)
	}
	if waits := h.spacing.recorded(); len(waits) != 1 || waits[0] < 30*time.Second || waits[0] > maxHold {
		t.Errorf("waits = %v, want the Retry-After hold", waits)
	}
}

func TestAServiceReadFailureMapsWithoutRepeatingAnything(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 403, googleErrorBody, nil }
	if _, err := h.call(t, handleAccountServicesGet, `{"account":"1","service":"s"}`); classOf(err) != provider.ClassPermission {
		t.Fatalf("err = %v", err)
	}
	if got := len(h.google.seen()); got != 1 {
		t.Errorf("requests = %d", got)
	}
}
