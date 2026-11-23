package googlemerchant

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Synthetic names: no value here has the shape of an address of a person.
const gbpRows = `{"gbpAccounts":[` +
	`{"name":"accounts/10/gbpAccount/1","gbpAccountId":"1","type":"BUSINESS_ACCOUNT","listingCount":"12",` +
	`"gbpAccountName":"Example Business","unknownMember":"never held"},` +
	`{"name":"accounts/10/gbpAccount/2","gbpAccountId":"2","type":"USER","listingCount":3,"gbpAccountName":"Example Owner"},` +
	`{"name":"accounts/10/gbpAccount/3","gbpAccountId":"3","type":"USER","listingCount":"x1"}` +
	`],"nextPageToken":"page-two"}`

func TestGbpAccountsListShowsEveryDocumentedMemberOnAnUnboundConnectionAndPages(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "page-two" {
			return 200, `{"gbpAccounts":[]}`, nil
		}
		return 200, gbpRows, nil
	}
	out, err := h.call(t, handleGbpAccountsList, `{"account":"10"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["gbpAccounts"].([]any)
	first := rows[0].(map[string]any)
	if len(rows) != 3 || len(first) != 5 || first["name"] != "accounts/10/gbpAccount/1" || first["gbpAccountId"] != "1" ||
		first["type"] != "BUSINESS_ACCOUNT" || first["listingCount"] != "12" || first["gbpAccountName"] != "Example Business" {
		t.Fatalf("first = %v", first)
	}
	if second := rows[1].(map[string]any); second["listingCount"] != "3" || second["gbpAccountName"] != "Example Owner" {
		t.Errorf("second = %v", second)
	}
	if third := rows[2].(map[string]any); third["listingCount"] != nil {
		t.Errorf("a bad count was kept: %v", third)
	}
	cursor, _ := out["next_cursor"].(string)
	if _, err := h.call(t, handleGbpAccountsList, `{"account":"10","page_size":20,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other arguments: %v", err)
	}
	next, err := h.call(t, handleGbpAccountsList, `{"account":"10","cursor":"`+cursor+`"}`)
	if err != nil || next["next_cursor"] != nil || len(next["gbpAccounts"].([]any)) != 0 {
		t.Fatalf("next = %v, %v", next, err)
	}
	want := []string{"GET /accounts/v1/accounts/10/gbpAccounts?pageSize=50",
		"GET /accounts/v1/accounts/10/gbpAccounts?pageSize=50&pageToken=page-two"}
	if got := serviceRequests(t, h); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

func TestGbpAccountNamesAreWithheldOnABoundConnection(t *testing.T) {
	for _, targets := range [][]string{{"accounts/10"}, {"advanced-accounts/10"}} {
		h := newHarness(t, targets...)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, gbpRows, nil }
		out, err := h.call(t, handleGbpAccountsList, `{"account":"10","page_size":1000}`)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range out["gbpAccounts"].([]any) {
			m := row.(map[string]any)
			if m["gbpAccountName"] != nil || m["gbpAccountId"] == nil || m["name"] == nil || m["type"] == nil {
				t.Errorf("%v row = %v", targets, m)
			}
		}
		if got := serviceRequests(t, h); !slices.Equal(got, []string{"GET /accounts/v1/accounts/10/gbpAccounts?pageSize=1000"}) {
			t.Errorf("requests = %v", got)
		}
	}
}

func TestLfpProvidersFindUsesTheRegionOfTheSettingAndKeepsTheCursorToIt(t *testing.T) {
	h := newHarness(t, "accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		return 200, `{"lfpProviders":[{"name":"accounts/10/omnichannelSettings/CH/lfpProviders/7","regionCode":"CH",` +
			`"displayName":"Example Provider","unknownMember":"never held"}],"nextPageToken":"page-two"}`, nil
	}
	out, err := h.call(t, handleLfpProvidersFind, `{"account":"10","region_code":"CH"}`)
	if err != nil {
		t.Fatal(err)
	}
	row := out["lfpProviders"].([]any)[0].(map[string]any)
	if len(row) != 3 || row["name"] != "accounts/10/omnichannelSettings/CH/lfpProviders/7" || row["regionCode"] != "CH" ||
		row["displayName"] != "Example Provider" {
		t.Fatalf("row = %v", row)
	}
	cursor, _ := out["next_cursor"].(string)
	if _, err := h.call(t, handleLfpProvidersFind, `{"account":"10","region_code":"DE","cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of another region: %v", err)
	}
	if _, err := h.call(t, handleLfpProvidersFind, `{"account":"10","region_code":"CH","cursor":"`+cursor+`"}`); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /accounts/v1/accounts/10/omnichannelSettings/CH/lfpProviders:find?pageSize=50",
		"GET /accounts/v1/accounts/10/omnichannelSettings/CH/lfpProviders:find?pageSize=50&pageToken=page-two"}
	if got := serviceRequests(t, h); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

func TestLinkReadsRefuseBadArgumentsBeforeSecretAndRequest(t *testing.T) {
	type call struct {
		handler capability.Handler
		args    string
		targets []string
	}
	cases := map[string]call{
		"gbp region":         {handleGbpAccountsList, `{"account":"10","region_code":"CH"}`, nil},
		"gbp outside":        {handleGbpAccountsList, `{"account":"2"}`, []string{"accounts/1"}},
		"gbp no account":     {handleGbpAccountsList, `{}`, nil},
		"gbp not an ID":      {handleGbpAccountsList, `{"account":"1/x"}`, nil},
		"gbp unknown member": {handleGbpAccountsList, `{"account":"10","email":"x"}`, nil},
		"gbp bad cursor":     {handleGbpAccountsList, `{"account":"10","cursor":"zzz"}`, nil},
		"lfp no region":      {handleLfpProvidersFind, `{"account":"10"}`, nil},
		"lfp lower case":     {handleLfpProvidersFind, `{"account":"10","region_code":"ch"}`, nil},
		"lfp three letters":  {handleLfpProvidersFind, `{"account":"10","region_code":"CHE"}`, nil},
		"lfp path":           {handleLfpProvidersFind, `{"account":"10","region_code":"C/"}`, nil},
		"lfp traversal":      {handleLfpProvidersFind, `{"account":"10","region_code":".."}`, nil},
		"lfp digits":         {handleLfpProvidersFind, `{"account":"10","region_code":"1A"}`, nil},
		"lfp outside":        {handleLfpProvidersFind, `{"account":"2","region_code":"CH"}`, []string{"accounts/1"}},
		"lfp bad cursor":     {handleLfpProvidersFind, `{"account":"10","region_code":"CH","cursor":"zzz"}`, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestLinkReadsProveASubAccountFirstAndMapGoogleErrors(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.Contains(req.URL.Path, "listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		}
		return 200, `{}`, nil
	}
	for _, c := range []struct {
		handler capability.Handler
		args    string
		path    string
	}{
		{handleGbpAccountsList, `{"account":"11"}`, "/accounts/v1/accounts/11/gbpAccounts"},
		{handleLfpProvidersFind, `{"account":"11","region_code":"CH"}`,
			"/accounts/v1/accounts/11/omnichannelSettings/CH/lfpProviders:find"},
	} {
		out, err := h.call(t, c.handler, c.args)
		if err != nil {
			t.Fatal(err)
		}
		for _, list := range []string{"gbpAccounts", "lfpProviders"} {
			if v, ok := out[list]; ok && len(v.([]any)) != 0 {
				t.Errorf("%s = %v, want a list that is never null", list, v)
			}
		}
		got := serviceRequests(t, h)
		if !strings.HasPrefix(got[len(got)-2], "GET /accounts/v1/accounts/10:listSubaccounts") ||
			!strings.HasPrefix(got[len(got)-1], "GET "+c.path+"?pageSize=50") {
			t.Errorf("requests = %v", got)
		}
	}
	for status, want := range map[int]provider.Class{
		401: provider.ClassAuth, 403: provider.ClassPermission, 404: provider.ClassNotFound, 429: provider.ClassRateLimited,
	} {
		for _, c := range []struct {
			handler capability.Handler
			args    string
		}{{handleGbpAccountsList, `{"account":"10"}`}, {handleLfpProvidersFind, `{"account":"10","region_code":"CH"}`}} {
			h := newHarness(t)
			h.google.api = func(*http.Request) (int, string, http.Header) { return status, `{"error":{"status":"X"}}`, nil }
			if _, err := h.call(t, c.handler, c.args); classOf(err) != want {
				t.Errorf("status %d: class = %q, want %q", status, classOf(err), want)
			}
		}
	}
}
