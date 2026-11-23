package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// api is one request the fake saw: method, path with query, and the JSON body.
type api struct{ method, path, body string }

func (h *harness) api(t *testing.T) []api {
	t.Helper()
	var out []api
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
		out = append(out, api{req.Method, path, body})
	}
	return out
}

const testAccountAnswer = `{"name":"accounts/1000002","accountId":"1000002","accountName":"Example Store","testAccount":true,` +
	`"languageCode":"en-US","homePageUri":"https://shop.example.invalid/","timeZone":{"id":"Europe/Zurich"}}`

func TestAccountWriteDescriptorsCarryRiskAndToolListRequirement(t *testing.T) {
	for _, c := range []struct {
		d           capability.Descriptor
		effect      capability.Effect
		idempotency capability.Idempotency
		listed      bool
		confirm     bool
	}{
		{accountsListSubaccounts, capability.EffectRead, capability.IdempotencySafe, false, false},
		{accountsUpdate, capability.EffectUpdate, capability.IdempotencyIdempotent, false, true},
		{accountsDelete, capability.EffectDelete, capability.IdempotencyUnknown, true, true},
		{accountsCreateTestAccount, capability.EffectCreate, capability.IdempotencyNonIdempotent, true, true},
	} {
		r := c.d.Risk
		if c.d.Version != 1 || r.Effect != c.effect || r.Idempotency != c.idempotency || !r.OpenWorld ||
			(r.Confirmation == capability.ConfirmationRequired) != c.confirm || c.d.RequiresToolAllowList != c.listed {
			t.Errorf("%s: version %d, risk %+v, tools list %t", c.d.ID, c.d.Version, r, c.d.RequiresToolAllowList)
		}
		if c.effect != capability.EffectRead && !strings.Contains(c.d.Description, "several minutes") {
			t.Errorf("%s does not name the delayed processing", c.d.ID)
		}
	}
	// The read profile holds no tool that changes data.
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range metadata.Profiles[0].Tools {
		for _, d := range reg.Provider(Provider) {
			if d.ID == id && d.Risk.Effect != capability.EffectRead {
				t.Errorf("the read profile holds %s", id)
			}
		}
	}
	if slicesContain(metadata.Profiles[0].Tools, accountsUpdateID) || !slicesContain(metadata.Profiles[0].Tools, accountsListSubaccountsID) {
		t.Errorf("profile tools = %v", metadata.Profiles[0].Tools)
	}
	// Only the four documented, writable members are arguments; the output-only home page is none.
	for _, d := range []capability.Descriptor{accountsUpdate, accountsCreateTestAccount} {
		for _, name := range []string{"homePageUri", "home_page_uri", "update_mask", "updateMask", "name", "test_account"} {
			if strings.Contains(string(d.InputSchema), `"`+name+`"`) {
				t.Errorf("%s takes %s", d.ID, name)
			}
		}
	}
}

func slicesContain(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestAdvancedOnlyToolsAreWithheldFromAConnectionWithoutTheAdvancedTarget(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, c := range []struct {
		tool    string
		targets []string
		want    bool
	}{
		{accountsListSubaccountsID, []string{"accounts/2"}, false},
		{accountsDeleteID, []string{"accounts/2"}, false},
		{accountsCreateTestAccountID, []string{"accounts/2"}, false},
		{accountsListSubaccountsID, []string{"accounts/2", "advanced-accounts/1"}, true},
		{accountsDeleteID, nil, true},
		{accountsUpdateID, []string{"accounts/2"}, true},
	} {
		if got, _ := metadata.Target.Reach(c.tool, c.targets); got != c.want {
			t.Errorf("Reach(%s, %v) = %t", c.tool, c.targets, got)
		}
	}
}

func TestListSubaccountsNeedsTheAdvancedTargetBeforeSecretAndRequest(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"account target":              {[]string{"accounts/1"}, `{"account":"1"}`},
		"sub-account of the advanced": {[]string{"advanced-accounts/1"}, `{"account":"2"}`},
		"other advanced account":      {[]string{"advanced-accounts/1"}, `{"account":"3"}`},
		"no argument, account target": {[]string{"accounts/1"}, `{}`},
		"no argument, two advanced":   {[]string{"advanced-accounts/1", "advanced-accounts/2"}, `{}`},
		"not an ID":                   {nil, `{"account":"1/users"}`},
		"no argument, no target":      {nil, `{}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, handleAccountsListSubaccounts, c.args); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestListSubaccountsReadsOnePageWithCursor(t *testing.T) {
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "page-two" {
			return 200, `{"accounts":[` + accountRow("4", "Four") + `]}`, nil
		}
		return 200, `{"accounts":[` + accountRow("2", "Two") + `,` + accountRow("3", "Three") + `],"nextPageToken":"page-two"}`, nil
	}
	first, err := h.call(t, handleAccountsListSubaccounts, `{"page_size":2}`)
	if err != nil || len(first["accounts"].([]any)) != 2 {
		t.Fatalf("first = %v, %v", first, err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "page-two" {
		t.Fatalf("next_cursor = %q", cursor)
	}
	second, err := h.call(t, handleAccountsListSubaccounts, `{"account":"1","page_size":2,"cursor":"`+cursor+`"}`)
	if err != nil || len(second["accounts"].([]any)) != 1 || second["next_cursor"] != nil {
		t.Fatalf("second = %v, %v", second, err)
	}
	if _, err := h.call(t, handleAccountsListSubaccounts, `{"page_size":3,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments: err = %v", err)
	}
	got := h.api(t)
	if len(got) != 2 || got[0] != (api{"GET", "/accounts/v1/accounts/1:listSubaccounts?pageSize=2", ""}) ||
		got[1] != (api{"GET", "/accounts/v1/accounts/1:listSubaccounts?pageSize=2&pageToken=page-two", ""}) {
		t.Errorf("requests = %v", got)
	}
}

func TestDeleteActsOnlyOnABoundAdvancedAccountAndNamesItExplicitly(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"account target":             {[]string{"accounts/1"}, `{"account":"1"}`},
		"sub-account":                {[]string{"advanced-accounts/1"}, `{"account":"2"}`},
		"other advanced account":     {[]string{"advanced-accounts/1"}, `{"account":"3"}`},
		"no argument, one advanced":  {[]string{"advanced-accounts/1"}, `{}`},
		"no argument, no target":     {nil, `{}`},
		"not an ID":                  {nil, `{"account":"1:force"}`},
		"advanced and account named": {[]string{"advanced-accounts/1", "accounts/2"}, `{"account":"2"}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, handleAccountsDelete, c.args); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestDeleteSendsOneDeleteWithForceOnlyWhenAsked(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		args   string
		path   string
	}{
		"plain":        {200, `{}`, `{"account":"1"}`, "/accounts/v1/accounts/1"},
		"no content":   {204, ``, `{"account":"1"}`, "/accounts/v1/accounts/1"},
		"force":        {200, `{}`, `{"account":"1","force":true}`, "/accounts/v1/accounts/1?force=true"},
		"force false":  {200, `{}`, `{"account":"1","force":false}`, "/accounts/v1/accounts/1"},
		"bound target": {200, `{}`, `{}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			targets := []string(nil)
			if name == "bound target" {
				targets = []string{"advanced-accounts/1"}
				c.args = `{"account":"1"}`
				c.path = "/accounts/v1/accounts/1"
			}
			h := newHarness(t, targets...)
			h.google.api = func(*http.Request) (int, string, http.Header) { return c.status, c.body, nil }
			out, err := h.call(t, handleAccountsDelete, c.args)
			if err != nil || out["account"] != "1" || out["deleted"] != true {
				t.Fatalf("out = %v, err = %v", out, err)
			}
			if got := h.api(t); len(got) != 1 || got[0] != (api{"DELETE", c.path, ""}) {
				t.Errorf("requests = %v", got)
			}
		})
	}
}

func TestChangesAreSentOnceAndAnUnclearOutcomeIsReported(t *testing.T) {
	calls := map[string]struct {
		handler capability.Handler
		args    string
	}{
		"delete": {handleAccountsDelete, `{"account":"1"}`},
		"update": {handleAccountsUpdate, `{"account":"1","account_name":"Example"}`},
		"create": {handleAccountsCreateTestAccount, `{"account":"1","account_name":"Example","language_code":"en","time_zone":{"id":"UTC"}}`},
	}
	for name, c := range calls {
		for status, want := range map[int]provider.Class{
			401: provider.ClassAuth, 403: provider.ClassPermission, 404: provider.ClassNotFound,
			429: provider.ClassRateLimited, 500: provider.ClassProviderError, 503: provider.ClassUnreachable,
		} {
			t.Run(name+"/"+http.StatusText(status), func(t *testing.T) {
				h := newHarness(t)
				h.google.api = func(*http.Request) (int, string, http.Header) {
					return status, `{"error":{"code":` + http.StatusText(status) + `,"status":"X"}}`, nil
				}
				_, err := h.call(t, c.handler, c.args)
				if classOf(err) != want {
					t.Fatalf("class = %q, want %q (%v)", classOf(err), want, err)
				}
				unknown := strings.Contains(err.Error(), "outcome of the change is unknown")
				if unknown != (status >= 500) {
					t.Errorf("status %d: unknown outcome reported = %t", status, unknown)
				}
				if n := len(h.google.seen()); n != 1 {
					t.Errorf("%d requests, want exactly one", n)
				}
			})
		}
	}
	t.Run("transport failure", func(t *testing.T) {
		h := newHarness(t)
		h.google.fail = io.ErrUnexpectedEOF
		_, err := h.call(t, handleAccountsDelete, calls["delete"].args)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
			t.Errorf("err = %v, %d requests", err, len(h.google.seen()))
		}
	})
	t.Run("answer that is not JSON", func(t *testing.T) {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) {
			return 200, `<html>`, http.Header{"Content-Type": {"text/html"}}
		}
		_, err := h.call(t, handleAccountsUpdate, calls["update"].args)
		if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestUpdateSendsOnlyThePassedMembersAndTheirMask(t *testing.T) {
	h := newHarness(t, "accounts/1000001")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testAccountAnswer, nil }
	out, err := h.call(t, handleAccountsUpdate, `{"account":"1000001","account_name":"Example Store","adult_content":false,`+
		`"language_code":"en-US","time_zone":{"id":"Europe/Zurich","version":"2025a"}}`)
	if err != nil || out["accountId"] != "1000002" || out["homePageUri"] != "https://shop.example.invalid/" {
		t.Fatalf("out = %v, err = %v", out, err)
	}
	got := h.api(t)
	if len(got) != 1 || got[0].method != "PATCH" ||
		got[0].path != "/accounts/v1/accounts/1000001?updateMask=accountName%2CadultContent%2ClanguageCode%2CtimeZone" {
		t.Fatalf("requests = %v", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got[0].body), &body); err != nil || len(body) != 4 || body["accountName"] != "Example Store" ||
		body["adultContent"] != false || body["languageCode"] != "en-US" ||
		body["timeZone"].(map[string]any)["id"] != "Europe/Zurich" || body["timeZone"].(map[string]any)["version"] != "2025a" {
		t.Errorf("body = %s", got[0].body)
	}

	h = newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testAccountAnswer, nil }
	if _, err := h.call(t, handleAccountsUpdate, `{"account":"7","time_zone":{"id":"UTC"}}`); err != nil {
		t.Fatal(err)
	}
	got = h.api(t)
	if got[0].path != "/accounts/v1/accounts/7?updateMask=timeZone" || got[0].body != `{"timeZone":{"id":"UTC"}}` {
		t.Errorf("request = %v", got[0])
	}
}

func TestUpdateRefusesBadArgumentsAndAccountsOutsideTheTargetsBeforeSecretAndRequest(t *testing.T) {
	long := strings.Repeat("a", maxAccountNameRunes+1)
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"no account":              {nil, `{"account_name":"Example"}`},
		"no member":               {nil, `{"account":"1"}`},
		"outside the target":      {[]string{"accounts/1"}, `{"account":"2","account_name":"Example"}`},
		"empty name":              {nil, `{"account":"1","account_name":" "}`},
		"long name":               {nil, `{"account":"1","account_name":"` + long + `"}`},
		"control character":       {nil, `{"account":"1","account_name":"a\nb"}`},
		"language code":           {nil, `{"account":"1","language_code":"en_US"}`},
		"language code injection": {nil, `{"account":"1","language_code":"en&x=y"}`},
		"time zone path":          {nil, `{"account":"1","time_zone":{"id":"../x"}}`},
		"time zone empty":         {nil, `{"account":"1","time_zone":{"id":""}}`},
		"time zone version":       {nil, `{"account":"1","time_zone":{"id":"UTC","version":"a b"}}`},
		"not an ID":               {nil, `{"account":"1:x","account_name":"Example"}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, handleAccountsUpdate, c.args); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestUpdateProvesASubAccountBeforeTheChangeAndRefusesOthersWithoutAChange(t *testing.T) {
	h := newHarness(t, "advanced-accounts/1")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.Method == http.MethodPatch {
			return 200, testAccountAnswer, nil
		}
		return 200, `{"accounts":[` + accountRow("2", "Two") + `]}`, nil
	}
	if _, err := h.call(t, handleAccountsUpdate, `{"account":"2","account_name":"Example"}`); err != nil {
		t.Fatal(err)
	}
	got := h.api(t)
	if len(got) != 2 || got[0].method != "GET" || !strings.HasPrefix(got[0].path, "/accounts/v1/accounts/1:listSubaccounts") ||
		got[1].method != "PATCH" || !strings.HasPrefix(got[1].path, "/accounts/v1/accounts/2?") {
		t.Fatalf("requests = %v", got)
	}
	_, err := h.call(t, handleAccountsUpdate, `{"account":"9","account_name":"Example"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "9") {
		t.Errorf("err = %v", err)
	}
	for _, r := range h.api(t)[2:] {
		if r.method != "GET" {
			t.Errorf("an account outside the targets was changed: %v", r)
		}
	}
}

func TestCreateTestAccountSendsTheRequiredMembersToTheAdvancedAccount(t *testing.T) {
	h := newHarness(t, "advanced-accounts/1000001")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testAccountAnswer, nil }
	out, err := h.call(t, handleAccountsCreateTestAccount, `{"account_name":"Example Test","language_code":"en-US",`+
		`"time_zone":{"id":"Europe/Zurich"},"adult_content":true}`)
	if err != nil || out["accountId"] != "1000002" || out["testAccount"] != true {
		t.Fatalf("out = %v, err = %v", out, err)
	}
	got := h.api(t)
	if len(got) != 1 || got[0].method != "POST" || got[0].path != "/accounts/v1/accounts/1000001:createTestAccount" {
		t.Fatalf("requests = %v", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got[0].body), &body); err != nil || len(body) != 4 || body["accountName"] != "Example Test" ||
		body["languageCode"] != "en-US" || body["adultContent"] != true || body["timeZone"].(map[string]any)["id"] != "Europe/Zurich" {
		t.Errorf("body = %s", got[0].body)
	}
}

func TestCreateTestAccountRefusesWithoutTheAdvancedTargetOrRequiredMembersBeforeSecretAndRequest(t *testing.T) {
	const members = `"account_name":"Example","language_code":"en","time_zone":{"id":"UTC"}`
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"account target":       {[]string{"accounts/1"}, `{"account":"1",` + members + `}`},
		"sub-account":          {[]string{"advanced-accounts/1"}, `{"account":"2",` + members + `}`},
		"no argument, account": {[]string{"accounts/1"}, `{` + members + `}`},
		"no name":              {nil, `{"account":"1","language_code":"en","time_zone":{"id":"UTC"}}`},
		"no language":          {nil, `{"account":"1","account_name":"Example","time_zone":{"id":"UTC"}}`},
		"no time zone":         {nil, `{"account":"1","account_name":"Example","language_code":"en"}`},
		"bad language":         {nil, `{"account":"1","account_name":"Example","language_code":"1","time_zone":{"id":"UTC"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, handleAccountsCreateTestAccount, c.args); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestAccountSummariesNameWhatTheConfirmationApproves(t *testing.T) {
	s := accountSummaries()
	for _, c := range []struct {
		id, args string
		want     []string
	}{
		{accountsUpdateID, `{"account":"1","account_name":"Example","time_zone":{"id":"UTC"}}`,
			[]string{"account 1", "accountName, timeZone", `"Example"`}},
		{accountsDeleteID, `{"account":"1","force":true}`, []string{"account 1", "sub-accounts", "Google Cloud project", "force"}},
		{accountsCreateTestAccountID, `{"account":"1","account_name":"Example"}`, []string{"test account", `"Example"`, "account 1"}},
	} {
		got := s[c.id](json.RawMessage(c.args))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s summary %q lacks %q", c.id, got, w)
			}
		}
	}
}
