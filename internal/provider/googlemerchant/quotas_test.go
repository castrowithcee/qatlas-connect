package googlemerchant

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

func TestQuotasListReadsGroupsWithCursorAndTheDocumentedPageSize(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "groups-two" {
			return 200, `{"quotaGroups":[{"name":"accounts/7/quotas/b"}]}`, nil
		}
		return 200, `{"quotaGroups":[{"name":"accounts/7/quotas/a","quotaUsage":"3","quotaLimit":"1000","quotaMinuteLimit":"10",` +
			`"methodDetails":[{"version":"v1","method":"products.list","subapi":"products","path":"products/v1/products.list"}],` +
			`"unknownMember":"never held"}],"nextPageToken":"groups-two"}`, nil
	}
	first, err := h.call(t, handleQuotasList, `{"account":"7","page_size":1000}`)
	if err != nil {
		t.Fatal(err)
	}
	group := first["quotaGroups"].([]any)[0].(map[string]any)
	detail := group["methodDetails"].([]any)[0].(map[string]any)
	if group["name"] != "accounts/7/quotas/a" || group["quotaUsage"] != "3" || group["quotaLimit"] != "1000" ||
		group["quotaMinuteLimit"] != "10" || group["unknownMember"] != nil || detail["method"] != "products.list" ||
		detail["version"] != "v1" || detail["subapi"] != "products" || detail["path"] != "products/v1/products.list" {
		t.Fatalf("first = %v", first)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "groups-two" {
		t.Fatalf("next_cursor = %q", cursor)
	}
	second, err := h.call(t, handleQuotasList, `{"account":"7","page_size":1000,"cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || len(second["quotaGroups"].([]any)) != 1 {
		t.Fatalf("second = %v, %v", second, err)
	}
	if paths := h.paths(t); !slices.Equal(paths, []string{"/quota/v1/accounts/7/quotas?pageSize=1000",
		"/quota/v1/accounts/7/quotas?pageSize=1000&pageToken=groups-two"}) {
		t.Errorf("paths = %v", paths)
	}
}

func TestQuotasListBoundsTheValuesOfAGroup(t *testing.T) {
	h := newHarness(t)
	details := strings.Repeat(`{"method":"m"},`, 150)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"quotaGroups":[{"name":"` + strings.Repeat("n", 300) + `","quotaUsage":"12x","quotaLimit":7,` +
			`"methodDetails":[` + strings.TrimSuffix(details, ",") + `]}]}`, nil
	}
	out, err := h.call(t, handleQuotasList, `{"account":"7"}`)
	if err != nil {
		t.Fatal(err)
	}
	group := out["quotaGroups"].([]any)[0].(map[string]any)
	if len([]rune(group["name"].(string))) != maxIDRunes || group["quotaUsage"] != nil || group["quotaLimit"] != nil ||
		len(group["methodDetails"].([]any)) != maxMethodDetails {
		t.Errorf("group = %v", group)
	}
}

func TestLimitsListFiltersByTypeAndLimitsGetReadsOneLimit(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, "/limits") {
			return 200, `{"accountLimits":[{"name":"accounts/7/limits/products~ADS_NON_EEA","products":{"scope":"ADS_NON_EEA","limit":"1000000"}}],` +
				`"nextPageToken":"limits-two"}`, nil
		}
		return 200, `{"name":"accounts/7/limits/products~ADS_EEA","products":{"scope":"ADS_EEA","limit":"5"}}`, nil
	}
	list, err := h.call(t, handleLimitsList, `{"account":"7","type":"products","page_size":100}`)
	if err != nil {
		t.Fatal(err)
	}
	limit := list["accountLimits"].([]any)[0].(map[string]any)
	products := limit["products"].(map[string]any)
	if limit["name"] != "accounts/7/limits/products~ADS_NON_EEA" || products["scope"] != "ADS_NON_EEA" ||
		products["limit"] != "1000000" || list["next_cursor"] == nil {
		t.Fatalf("list = %v", list)
	}
	one, err := h.call(t, handleLimitsGet, `{"account":"7","limit":"products~ADS_EEA"}`)
	if err != nil || one["products"].(map[string]any)["limit"] != "5" {
		t.Fatalf("one = %v, %v", one, err)
	}
	want := []string{"/accounts/v1/accounts/7/limits?filter=type+%3D+%22products%22&pageSize=100",
		"/accounts/v1/accounts/7/limits/products~ADS_EEA"}
	if paths := h.paths(t); !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestQuotaAndLimitToolsCheckArgumentsBeforeSecretAndRequest(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		handler capability.Handler
		args    string
	}{
		"quotas, outside the targets": {[]string{"accounts/1"}, handleQuotasList, `{"account":"2"}`},
		"quotas, page size too large": {nil, handleQuotasList, `{"account":"2","page_size":1001}`},
		"limits, outside the targets": {[]string{"accounts/1"}, handleLimitsList, `{"account":"2","type":"products"}`},
		"limits, page size too large": {nil, handleLimitsList, `{"account":"2","type":"products","page_size":101}`},
		"limits, another type":        {nil, handleLimitsList, `{"account":"2","type":"products\" OR"}`},
		"limits get, outside":         {[]string{"accounts/1"}, handleLimitsGet, `{"account":"2","limit":"products~ADS_EEA"}`},
		"limits get, path":            {nil, handleLimitsGet, `{"account":"2","limit":"a/b"}`},
		"limits get, dot segment":     {nil, handleLimitsGet, `{"account":"2","limit":".."}`},
		"limits get, query":           {nil, handleLimitsGet, `{"account":"2","limit":"a?b=c"}`},
		"limits get, empty":           {nil, handleLimitsGet, `{"account":"2","limit":""}`},
		"limits get, no account":      {nil, handleLimitsGet, `{"limit":"products~ADS_EEA"}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, c.handler, c.args); !isInvalidRequest(err) {
				t.Fatalf("err = %v, want an invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestQuotaAndLimitToolsProveASubAccountOfABoundAdvancedAccount(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Path {
		case "/accounts/v1/accounts/10:listSubaccounts":
			return 200, `{"accounts":[` + accountRow("11", "Sub") + `]}`, nil
		}
		return 200, `{}`, nil
	}
	if _, err := h.call(t, handleQuotasList, `{"account":"11"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.call(t, handleLimitsGet, `{"account":"12","limit":"products~ADS_EEA"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v, want a refusal of an account that is no sub-account", err)
	}
	want := []string{"/accounts/v1/accounts/10:listSubaccounts?pageSize=500", "/quota/v1/accounts/11/quotas",
		"/accounts/v1/accounts/10:listSubaccounts?pageSize=500"}
	if paths := h.paths(t); !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}
