package googlemerchant

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

func TestAggregateProductStatusesBuildsTheFilterFromTheDocumentedFields(t *testing.T) {
	for _, c := range []struct{ args, query string }{
		{`{"account":"1000001"}`, ""},
		{`{"account":"1000001","reporting_context":"SHOPPING_ADS"}`, "filter=reporting_context+%3D+%22SHOPPING_ADS%22"},
		{`{"account":"1000001","country":"US","page_size":250}`, "filter=country+%3D+%22US%22&pageSize=250"},
		{`{"account":"1000001","reporting_context":"FREE_LISTINGS","country":"CH"}`,
			"filter=reporting_context+%3D+%22FREE_LISTINGS%22+AND+country+%3D+%22CH%22"},
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
		out, err := h.call(t, handleAggregateProductStatusesList, c.args)
		if err != nil || out["aggregateProductStatuses"] == nil {
			t.Fatalf("%s: %v, %v", c.args, out, err)
		}
		want := "/issueresolution/v1/accounts/1000001/aggregateProductStatuses"
		if c.query != "" {
			want += "?" + c.query
		}
		if paths := h.paths(t); !slices.Equal(paths, []string{want}) {
			t.Errorf("%s: paths = %v, want %s", c.args, paths, want)
		}
	}
}

func TestAggregateProductStatusesRefusesFilterValuesOutsideTheDocumentedFormsBeforeSecret(t *testing.T) {
	for _, args := range []string{
		`{"account":"1","country":"us"}`, `{"account":"1","country":"USA"}`, `{"account":"1","country":"US\" OR country = \"CH"}`,
		`{"account":"1","reporting_context":"SHOPPING_ADS\" AND country = \"US"}`, `{"account":"1","reporting_context":"DISCOVERY_ADS"}`,
		`{"account":"1","reporting_context":"REPORTING_CONTEXT_ENUM_UNSPECIFIED"}`, `{"account":"1","reporting_context":"shopping_ads"}`,
		`{"account":"1","page_size":251}`,
	} {
		h := newHarness(t)
		if _, err := h.call(t, handleAggregateProductStatusesList, args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v", args, err)
		}
		h.untouched(t)
	}
}

func TestAggregateProductStatusesReturnsEveryDocumentedMember(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "more" {
			return 200, `{"aggregateProductStatuses":[]}`, nil
		}
		return 200, `{"aggregateProductStatuses":[{"name":"accounts/1000001/aggregateProductStatuses/SHOPPING_ADS~US",` +
			`"country":"US","reportingContext":"SHOPPING_ADS","stats":{"activeCount":"10","pendingCount":"2",` +
			`"disapprovedCount":"1","expiringCount":"0"},"itemLevelIssues":[{"code":"missing_gtin","severity":"DEMOTED",` +
			`"resolution":"MERCHANT_ACTION","attribute":"gtin","description":"` + strings.Repeat("d", 2000) + `",` +
			`"detail":"Detail","documentationUri":"https://doc.example.invalid/gtin","productCount":"3","unknown":"x"}],` +
			`"unknown":"x"}],"nextPageToken":"more"}`, nil
	}
	out, err := h.call(t, handleAggregateProductStatusesList, `{"account":"1000001"}`)
	if err != nil {
		t.Fatal(err)
	}
	row := out["aggregateProductStatuses"].([]any)[0].(map[string]any)
	stats := row["stats"].(map[string]any)
	issue := row["itemLevelIssues"].([]any)[0].(map[string]any)
	if row["country"] != "US" || row["reportingContext"] != "SHOPPING_ADS" || row["name"] == nil || row["unknown"] != nil ||
		stats["activeCount"] != "10" || stats["pendingCount"] != "2" || stats["disapprovedCount"] != "1" ||
		stats["expiringCount"] != "0" || len(issue) != 8 || issue["unknown"] != nil || issue["productCount"] != "3" ||
		len([]rune(issue["description"].(string))) != maxTextRunes {
		t.Fatalf("row = %v", row)
	}
	cursor, _ := out["next_cursor"].(string)
	if cursor == "" || cursor == "more" {
		t.Fatalf("next_cursor = %q", cursor)
	}
	if _, err := h.call(t, handleAggregateProductStatusesList, `{"account":"1000001","country":"US","cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of another filter gave %v", err)
	}
	if _, err := h.call(t, handleAggregateProductStatusesList, `{"account":"1000001","cursor":"`+cursor+`"}`); err != nil {
		t.Fatal(err)
	}
	if paths := h.paths(t); len(paths) != 2 || !strings.HasSuffix(paths[1], "aggregateProductStatuses?pageToken=more") {
		t.Errorf("paths = %v", paths)
	}
}

func TestAccountIssuesListSendsLanguageTimeZoneAndPaging(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		if req.URL.Query().Get("pageToken") == "more" {
			return 200, `{"accountIssues":[{"name":"accounts/1000001/issues/two"}]}`, nil
		}
		return 200, `{"accountIssues":[{"name":"accounts/1000001/issues/one","title":"Synthetic title",` +
			`"detail":"Synthetic detail","severity":"CRITICAL","documentationUri":"https://doc.example.invalid/one",` +
			`"impactedDestinations":[{"reportingContext":"SHOPPING_ADS","impacts":[{"regionCode":"US","severity":"ERROR"}]}],` +
			`"unknown":"x"},{}],"nextPageToken":"more"}`, nil
	}
	out, err := h.call(t, handleAccountIssuesList, `{"account":"1000001","language_code":"de-CH","time_zone":"Europe/Zurich","page_size":100}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["accountIssues"].([]any)
	issue := rows[0].(map[string]any)
	destination := issue["impactedDestinations"].([]any)[0].(map[string]any)
	impact := destination["impacts"].([]any)[0].(map[string]any)
	if len(rows) != 1 || issue["title"] != "Synthetic title" || issue["detail"] != "Synthetic detail" || issue["severity"] != "CRITICAL" ||
		issue["documentationUri"] != "https://doc.example.invalid/one" || issue["name"] == nil || issue["unknown"] != nil ||
		destination["reportingContext"] != "SHOPPING_ADS" || impact["regionCode"] != "US" || impact["severity"] != "ERROR" {
		t.Fatalf("issue = %v", issue)
	}
	cursor, _ := out["next_cursor"].(string)
	args := `{"account":"1000001","language_code":"de-CH","time_zone":"Europe/Zurich","page_size":100,"cursor":"` + cursor + `"}`
	if _, err := h.call(t, handleAccountIssuesList, args); err != nil {
		t.Fatal(err)
	}
	if _, err := h.call(t, handleAccountIssuesList, `{"account":"1000001","time_zone":"Europe/Zurich","page_size":100,"cursor":"`+cursor+`"}`); !isInvalidRequest(err) {
		t.Errorf("a cursor of other localization gave %v", err)
	}
	want := []string{
		"/accounts/v1/accounts/1000001/issues?languageCode=de-CH&pageSize=100&timeZone=Europe%2FZurich",
		"/accounts/v1/accounts/1000001/issues?languageCode=de-CH&pageSize=100&pageToken=more&timeZone=Europe%2FZurich",
	}
	if paths := h.paths(t); !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestAccountIssuesListRefusesBadLocalizationBeforeSecretAndRequest(t *testing.T) {
	for _, args := range []string{
		`{"account":"1","language_code":"en_US"}`, `{"account":"1","language_code":"en-US&x=1"}`, `{"account":"1","language_code":"e"}`,
		`{"account":"1","language_code":"en-US-aaaaaaaaa"}`, `{"account":"1","time_zone":"Europe/Zurich?x"}`,
		`{"account":"1","time_zone":"../etc"}`, `{"account":"1","time_zone":"a/b/c/d"}`, `{"account":"1","page_size":101}`,
	} {
		h := newHarness(t)
		if _, err := h.call(t, handleAccountIssuesList, args); !isInvalidRequest(err) {
			t.Errorf("%s gave %v", args, err)
		}
		h.untouched(t)
	}
}

func TestIssueToolsWithoutAnythingToListReturnEmptyLists(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct {
		handler capability.Handler
		member  string
	}{{handleAccountIssuesList, "accountIssues"}, {handleAggregateProductStatusesList, "aggregateProductStatuses"},
		{handleProductsList, "products"}} {
		out, err := h.call(t, c.handler, `{"account":"1"}`)
		if err != nil {
			t.Fatal(err)
		}
		if rows, ok := out[c.member].([]any); !ok || len(rows) != 0 {
			t.Errorf("%s = %v", c.member, out[c.member])
		}
	}
}

func TestReportingContextsAreTheDocumentedOnesWithoutTheDeprecatedValue(t *testing.T) {
	if len(reportingContexts) != 19 || slices.Contains(reportingContexts, "DISCOVERY_ADS") {
		t.Errorf("reportingContexts = %v", reportingContexts)
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(aggregateProductStatusesList.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(schema.Properties["reporting_context"].Enum, reportingContexts) {
		t.Error("the schema does not list the reporting contexts")
	}
}
