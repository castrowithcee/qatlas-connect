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

const programRow = `{"name":"accounts/1000001/programs/free-listings","state":"ENABLED","activeRegionCodes":["US","DE"],` +
	`"unmetRequirements":[{"title":"Add a return policy","affectedRegionCodes":["DE","001"],` +
	`"documentationUri":"https://support.example.invalid/req"}],"documentationUri":"https://support.example.invalid/p",` +
	`"unknownMember":"never held"}`

// requests lists method, path, and query of the requests so far, and checks the origin and the token.
func requests(t *testing.T, h *harness) []string {
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

func TestProgramToolsAreDeclaredWithTheirRisk(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range []string{programsListID, programsGetID} {
		d, _, _ := reg.Lookup(id)
		if d.Risk != readRisk || d.RequiresToolAllowList || d.Version != 1 {
			t.Errorf("%s = %+v", id, d)
		}
	}
	for _, id := range []string{programsEnableID, programsDisableID} {
		d, handler, ok := reg.Lookup(id)
		if !ok || handler == nil || d.Version != 1 || !d.RequiresToolAllowList || d.Risk.Effect != capability.EffectUpdate ||
			d.Risk.Confirmation != capability.ConfirmationRequired || d.Risk.Idempotency != capability.IdempotencyIdempotent {
			t.Errorf("%s = %+v", id, d)
		}
		for _, want := range []string{"Admin", "Standard", "Google Shopping", "afterwards", "never repeated"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", id, want)
			}
		}
		for _, profile := range metadata.Profiles {
			if slices.Contains(profile.Tools, id) {
				t.Errorf("profile %s lists %s", profile.ID, id)
			}
		}
	}
	for verb, summary := range map[string]capability.SummaryFunc{"Enable": programSummary("Enable"), "Disable": programSummary("Disable")} {
		got := summary(json.RawMessage(`{"account":"1000001","program":"free-listings"}`))
		if !strings.HasPrefix(got, verb+` program "free-listings" of account 1000001`) || !strings.Contains(got, "Google Shopping") {
			t.Errorf("summary = %q", got)
		}
		if got := summary(json.RawMessage(`{"program":"checkout"}`)); !strings.Contains(got, "account of this connection") {
			t.Errorf("summary without account = %q", got)
		}
	}
}

func TestProgramsListSendsTheDocumentedParametersAndShowsEveryDocumentedMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "page-two" {
			return 200, `{"programs":[{"name":"accounts/1000001/programs/checkout","state":"NOT_ELIGIBLE"}]}`, nil
		}
		return 200, `{"programs":[` + programRow + `],"nextPageToken":"page-two"}`, nil
	}
	first, err := h.call(t, handleProgramsList, `{"account":"1000001"}`)
	if err != nil {
		t.Fatal(err)
	}
	row := first["programs"].([]any)[0].(map[string]any)
	req := row["unmetRequirements"].([]any)[0].(map[string]any)
	if row["name"] != "accounts/1000001/programs/free-listings" || row["state"] != "ENABLED" ||
		row["documentationUri"] != "https://support.example.invalid/p" || row["unknownMember"] != nil ||
		!slices.Equal(row["activeRegionCodes"].([]any), []any{"US", "DE"}) ||
		req["title"] != "Add a return policy" || req["documentationUri"] != "https://support.example.invalid/req" ||
		!slices.Equal(req["affectedRegionCodes"].([]any), []any{"DE", "001"}) || len(req) != 3 {
		t.Fatalf("row = %v", row)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "page-two" {
		t.Fatalf("next_cursor = %q", cursor)
	}
	second, err := h.call(t, handleProgramsList, `{"account":"1000001","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || len(second["programs"].([]any)) != 1 {
		t.Fatalf("second = %v, %v", second, err)
	}
	if _, err := h.call(t, handleProgramsList, `{"account":"1000001","page_size":5,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments: %v", err)
	}
	want := []string{"GET /accounts/v1/accounts/1000001/programs?pageSize=100",
		"GET /accounts/v1/accounts/1000001/programs?pageSize=100&pageToken=page-two"}
	if got := requests(t, h); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
	if _, err := h.call(t, handleProgramsList, `{"account":"1000001","page_size":1000}`); err != nil {
		t.Fatal(err)
	}
	if got := requests(t, h); !strings.HasSuffix(got[len(got)-1], "pageSize=1000") {
		t.Errorf("requests = %v", got)
	}
}

func TestProgramsListEmptyAnswerIsAnEmptyListAndUnmetRequirementsStayEmptyWhenAbsent(t *testing.T) {
	h := newHarness(t)
	out, err := h.call(t, handleProgramsList, `{"account":"1000001"}`)
	if err != nil || out["programs"] == nil || len(out["programs"].([]any)) != 0 {
		t.Fatalf("out = %v, %v", out, err)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/1000001/programs/loyalty","state":"ELIGIBLE","unmetRequirements":"nope","activeRegionCodes":7}`, nil
	}
	got, err := h.call(t, handleProgramsGet, `{"account":"1000001","program":"loyalty"}`)
	if err != nil || got["unmetRequirements"] != nil || got["activeRegionCodes"] != nil || got["state"] != "ELIGIBLE" {
		t.Fatalf("got = %v, %v", got, err)
	}
}

func TestProgramsGetReadsOneProgram(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, programRow, nil }
	out, err := h.call(t, handleProgramsGet, `{"account":"1000001","program":"free-listings"}`)
	if err != nil || out["state"] != "ENABLED" {
		t.Fatalf("out = %v, %v", out, err)
	}
	if got := requests(t, h); !slices.Equal(got, []string{"GET /accounts/v1/accounts/1000001/programs/free-listings"}) {
		t.Errorf("requests = %v", got)
	}
}

func TestProgramTextsAndListsAreBounded(t *testing.T) {
	h := newHarness(t)
	var reqs []string
	for i := 0; i < maxListItems+20; i++ {
		reqs = append(reqs, `{"title":"`+strings.Repeat("t", 500)+`","documentationUri":"https://x.invalid/`+
			strings.Repeat("u", 3000)+`","affectedRegionCodes":["`+strings.Repeat("R", 500)+`"]}`)
	}
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/1/programs/checkout","unmetRequirements":[` + strings.Join(reqs, ",") + `]}`, nil
	}
	out, err := h.call(t, handleProgramsGet, `{"account":"1","program":"checkout"}`)
	if err != nil {
		t.Fatal(err)
	}
	list := out["unmetRequirements"].([]any)
	first := list[0].(map[string]any)
	if len(list) != maxListItems || len([]rune(first["title"].(string))) != maxNameRunes ||
		len([]rune(first["documentationUri"].(string))) != maxURLRunes ||
		len([]rune(first["affectedRegionCodes"].([]any)[0].(string))) != maxIDRunes {
		t.Errorf("bounds not kept: %d entries", len(list))
	}
}

func TestProgramsListRefusesMoreRowsThanTheBound(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"programs":[` + strings.Repeat(`{},`, maxEntries) + `{}]}`, nil
	}
	if _, err := h.call(t, handleProgramsList, `{"account":"1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestEnableAndDisableSendOneFixedRequestAndReportAcceptance(t *testing.T) {
	for _, c := range []struct {
		handler capability.Handler
		action  string
	}{{handleProgramsEnable, "enable"}, {handleProgramsDisable, "disable"}} {
		h := newHarness(t)
		var body string
		h.google.api = func(req *http.Request) (int, string, http.Header) {
			b, _ := io.ReadAll(req.Body)
			body = string(b)
			if req.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q", req.Header.Get("Content-Type"))
			}
			return 200, programRow, nil
		}
		out, err := h.call(t, c.handler, `{"account":"1000001","program":"shopping-ads"}`)
		if err != nil {
			t.Fatal(err)
		}
		if out["accepted"] != true || out["program"].(map[string]any)["state"] != "ENABLED" {
			t.Errorf("out = %v", out)
		}
		want := "POST /accounts/v1/accounts/1000001/programs/shopping-ads:" + c.action
		if got := requests(t, h); !slices.Equal(got, []string{want}) || body != `{}` {
			t.Errorf("%s: requests = %v, body = %q", c.action, got, body)
		}
	}
}

func TestProgramIDAndAccountAreCheckedBeforeSecretAndRequest(t *testing.T) {
	for _, bad := range []string{"", "Free-Listings", "free listings", "free/listings", "..", "free.listings", "free_listings",
		"-free", "1free", "free-listings:enable", "free?x=1", "free%2Flistings", "a/../b", strings.Repeat("a", 41), "frée"} {
		for name, handler := range map[string]capability.Handler{"get": handleProgramsGet, "enable": handleProgramsEnable,
			"disable": handleProgramsDisable} {
			h := newHarness(t)
			encoded, _ := json.Marshal(map[string]string{"account": "1", "program": bad})
			if _, err := h.call(t, handler, string(encoded)); !isInvalidRequest(err) {
				t.Errorf("%s %q: err = %v", name, bad, err)
			}
			h.untouched(t)
		}
	}
	for _, args := range []string{`{"account":"1","program":"checkout","extra":1}`, `{"account":"01","program":"checkout"}`,
		`{"account":"1/2","program":"checkout"}`, `{"program":"checkout"}`, `{"account":"1","program":"checkout","cursor":"x"}`} {
		for _, handler := range []capability.Handler{handleProgramsGet, handleProgramsEnable, handleProgramsDisable} {
			h := newHarness(t)
			if _, err := h.call(t, handler, args); !isInvalidRequest(err) {
				t.Errorf("%s: err = %v", args, err)
			}
			h.untouched(t)
		}
	}
	h := newHarness(t)
	if _, err := h.call(t, handleProgramsList, `{"account":"1","program":"checkout"}`); !isInvalidRequest(err) {
		t.Errorf("list with program: %v", err)
	}
	h.untouched(t)
}

func TestAnAccountOutsideTheTargetsIsRefusedWithoutNamingItForEveryTool(t *testing.T) {
	for name, handler := range map[string]capability.Handler{"list": handleProgramsList, "get": handleProgramsGet,
		"enable": handleProgramsEnable, "disable": handleProgramsDisable} {
		h := newHarness(t, "accounts/7")
		_, err := h.call(t, handler, `{"account":"4242","program":"checkout"}`)
		if name == "list" {
			_, err = h.call(t, handler, `{"account":"4242"}`)
		}
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "4242") {
			t.Errorf("%s: err = %v", name, err)
		}
		h.untouched(t)
	}
}

func TestASubAccountIsProvenBeforeTheReadOrTheChange(t *testing.T) {
	for name, c := range map[string]struct {
		handler capability.Handler
		args    string
		last    string
	}{
		"list":    {handleProgramsList, `{"account":"11"}`, "GET /accounts/v1/accounts/11/programs?pageSize=100"},
		"get":     {handleProgramsGet, `{"account":"11","program":"checkout"}`, "GET /accounts/v1/accounts/11/programs/checkout"},
		"enable":  {handleProgramsEnable, `{"account":"11","program":"checkout"}`, "POST /accounts/v1/accounts/11/programs/checkout:enable"},
		"disable": {handleProgramsDisable, `{"account":"11","program":"checkout"}`, "POST /accounts/v1/accounts/11/programs/checkout:disable"},
	} {
		h := newHarness(t, "advanced-accounts/10")
		h.google.api = func(req *http.Request) (int, string, http.Header) {
			if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
				return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
			}
			return 200, `{"programs":[]}`, nil
		}
		if _, err := h.call(t, c.handler, c.args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := []string{"GET /accounts/v1/accounts/10:listSubaccounts?pageSize=500", c.last}
		if got := requests(t, h); !slices.Equal(got, want) {
			t.Errorf("%s: requests = %v, want %v", name, got, want)
		}
	}
}

func TestAnAccountThatIsNoSubAccountSendsNoChangeAndIsNotNamed(t *testing.T) {
	for args, handler := range map[string]capability.Handler{`{"account":"4242"}`: handleProgramsList,
		`{"account":"4242","program":"checkout"}`: handleProgramsGet, `{"account":"4242","program":"x"}`: handleProgramsEnable,
		`{"account":"4242","program":"y"}`: handleProgramsDisable} {
		h := newHarness(t, "advanced-accounts/10")
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		}
		_, err := h.call(t, handler, args)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "4242") {
			t.Fatalf("err = %v", err)
		}
		if got := requests(t, h); len(got) != 1 || !strings.Contains(got[0], ":listSubaccounts") {
			t.Errorf("requests = %v, want only the proof read", got)
		}
	}
}

func TestAFailedChangeIsNeverRepeatedAndSaysWhenTheOutcomeIsOpen(t *testing.T) {
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
		for _, handler := range []capability.Handler{handleProgramsEnable, handleProgramsDisable} {
			h := newHarness(t)
			h.google.fail = c.fail
			h.google.api = func(*http.Request) (int, string, http.Header) { return c.answer.status, c.answer.body, c.answer.header }
			_, err := h.call(t, handler, `{"account":"1","program":"checkout"}`)
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
}

func TestARateLimitedChangeHoldsTheNextRequest(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 429, quotaBody(reasonMinuteQuota), http.Header{"Retry-After": {"30"}}
	}
	if _, err := h.call(t, handleProgramsEnable, `{"account":"1","program":"checkout"}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("err = %v", err)
	}
	h.google.api = nil
	if _, err := h.call(t, handleProgramsGet, `{"account":"1","program":"checkout"}`); err != nil {
		t.Fatal(err)
	}
	if waits := h.spacing.recorded(); len(waits) != 1 || waits[0] < 30*time.Second || waits[0] > maxHold {
		t.Errorf("waits = %v, want the Retry-After hold", waits)
	}
}

func TestAProgramReadFailureMapsWithoutRepeatingAnythingButTheTokenRenewal(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 403, googleErrorBody, nil }
	if _, err := h.call(t, handleProgramsGet, `{"account":"1","program":"checkout"}`); classOf(err) != provider.ClassPermission {
		t.Fatalf("err = %v", err)
	}
	if got := len(h.google.seen()); got != 1 {
		t.Errorf("requests = %d", got)
	}
}
