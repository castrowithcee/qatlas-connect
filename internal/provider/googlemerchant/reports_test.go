package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func report(t *testing.T, args string) (string, error) {
	t.Helper()
	var a reportArguments
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		t.Fatal(err)
	}
	built, err := buildReport(a)
	return built.query, err
}

func TestTheCatalogHoldsTheTenTablesWithTheDocumentedFieldCounts(t *testing.T) {
	want := map[string]int{
		"product_view": 32, "product_performance_view": 30, "non_product_performance_view": 5,
		"price_insights_product_view": 20, "price_competitiveness_product_view": 17,
		"best_sellers_product_cluster_view": 19, "best_sellers_brand_view": 10,
		"competitive_visibility_benchmark_view": 6, "competitive_visibility_competitor_view": 11,
		"competitive_visibility_top_merchant_view": 10,
	}
	if len(reportTables) != len(want) {
		t.Fatalf("%d tables", len(reportTables))
	}
	for _, table := range reportTables {
		members := map[string]bool{}
		count := 0
		for _, f := range table.fields {
			if f.name == itemSeverityPath {
				continue
			}
			count++
			if members[f.jsonName()] {
				t.Errorf("%s: %s twice", table.name, f.name)
			}
			members[f.jsonName()] = true
		}
		if count != want[table.name] || table.key != camel(table.name) {
			t.Errorf("%s has %d fields under %q, want %d", table.name, count, table.key, want[table.name])
		}
	}
	for _, name := range []string{"custom_label0", "custom_label4", "category_l5", "product_type_l1"} {
		table, _ := reportTableNamed("product_performance_view")
		if _, ok := table.field(name); !ok {
			t.Errorf("missing %s", name)
		}
	}
	table, _ := reportTableNamed("product_performance_view")
	if f, _ := table.field("custom_label0"); f.jsonName() != "customLabel0" {
		t.Errorf("custom_label0 member = %s", f.jsonName())
	}
}

func TestReportQueriesAreComposedFromCatalogNames(t *testing.T) {
	for name, c := range map[string]struct{ args, want string }{
		"performance with a window": {
			`{"table":"product_performance_view","fields":["offer_id","clicks"],"date_range":{"window":"LAST_30_DAYS"},` +
				`"order_by":[{"field":"clicks","direction":"DESC"}]}`,
			"SELECT offer_id, clicks FROM product_performance_view WHERE date DURING LAST_30_DAYS ORDER BY clicks DESC"},
		"absolute dates and conditions": {
			`{"table":"product_performance_view","fields":["impressions","clicks","brand"],"date_range":{"start":"2026-01-01","end":"2026-01-31"},` +
				`"conditions":[{"field":"clicks","operator":">","values":[100]},{"field":"brand","operator":"IN","values":["Alpha","Beta"]}]}`,
			"SELECT impressions, clicks, brand FROM product_performance_view WHERE date BETWEEN '2026-01-01' AND '2026-01-31' " +
				"AND clicks > 100 AND brand IN ('Alpha', 'Beta')"},
		"required fields are added": {
			`{"table":"product_view","fields":["title"],"conditions":[{"field":"aggregated_reporting_context_status",` +
				`"operator":"=","values":["ELIGIBLE_LIMITED"]},{"field":"item_issues.severity.aggregated_severity","operator":"IN",` +
				`"values":["DISAPPROVED","DEMOTED"]}]}`,
			"SELECT id, title FROM product_view WHERE aggregated_reporting_context_status = 'ELIGIBLE_LIMITED' AND " +
				"item_issues.severity.aggregated_severity IN ('DISAPPROVED', 'DEMOTED')"},
		"best sellers": {
			`{"table":"best_sellers_brand_view","fields":["brand","rank"],"conditions":[{"field":"report_granularity","operator":"=",` +
				`"values":["WEEKLY"]},{"field":"report_country_code","operator":"=","values":["CH"]},` +
				`{"field":"rank","operator":"BETWEEN","values":[1,20]}]}`,
			"SELECT report_date, report_granularity, report_country_code, report_category_id, brand, rank FROM " +
				"best_sellers_brand_view WHERE report_granularity = 'WEEKLY' AND report_country_code = 'CH' AND rank BETWEEN 1 AND 20"},
		"competitive visibility without the date in the select": {
			`{"table":"competitive_visibility_top_merchant_view","fields":["rank"],"date_range":{"window":"LAST_7_DAYS"},` +
				`"conditions":[{"field":"report_country_code","operator":"=","values":["DE"]},{"field":"report_category_id","operator":"=","values":["166"]}]}`,
			"SELECT domain, report_country_code, report_category_id, traffic_source, rank FROM " +
				"competitive_visibility_top_merchant_view WHERE date DURING LAST_7_DAYS AND report_country_code = 'DE' AND " +
				"report_category_id = 166"},
		"pattern and list": {
			`{"table":"product_view","fields":["gtin","title"],"conditions":[{"field":"title","operator":"REGEXP_MATCH","values":["(?i)^shoe.*"]},` +
				`{"field":"gtin","operator":"CONTAINS ANY","values":["0000000000000"]}]}`,
			"SELECT id, gtin, title FROM product_view WHERE title REGEXP_MATCH '(?i)^shoe.*' AND gtin CONTAINS ANY ('0000000000000')"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := report(t, c.args)
			if err != nil || got != c.want {
				t.Errorf("query = %q, %v\nwant %q", got, err, c.want)
			}
		})
	}
}

func TestReportQueriesRefuseWhatTheCatalogAndTheDocumentationExclude(t *testing.T) {
	const perf = `"table":"product_performance_view","date_range":{"window":"LAST_7_DAYS"}`
	for name, args := range map[string]string{
		"unknown table":            `{"table":"orders","fields":["x"]}`,
		"unknown field":            `{` + perf + `,"fields":["clicks","offer_id; DROP"]}`,
		"field of another table":   `{` + perf + `,"fields":["clicks","rank"]}`,
		"no metric":                `{` + perf + `,"fields":["offer_id"]}`,
		"no date range":            `{"table":"product_performance_view","fields":["clicks"]}`,
		"date range without date":  `{"table":"product_view","fields":["title"],"date_range":{"window":"TODAY"}}`,
		"unknown window":           `{"table":"product_performance_view","fields":["clicks"],"date_range":{"window":"LAST_YEAR"}}`,
		"window and start":         `{"table":"product_performance_view","fields":["clicks"],"date_range":{"window":"TODAY","start":"2026-01-01"}}`,
		"start after end":          `{"table":"product_performance_view","fields":["clicks"],"date_range":{"start":"2026-02-01","end":"2026-01-01"}}`,
		"bad date":                 `{"table":"product_performance_view","fields":["clicks"],"date_range":{"start":"2026-02-30","end":"2026-03-01"}}`,
		"date as condition":        `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"date","operator":"=","values":["2026-01-01"]}]}`,
		"quote in a value":         `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"brand","operator":"=","values":["a' OR 1=1 --"]}]}`,
		"double quote":             `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"brand","operator":"=","values":["a\" OR"]}]}`,
		"trailing backslash":       `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"brand","operator":"LIKE","values":["a\\"]}]}`,
		"control character":        `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"brand","operator":"=","values":["a\nb"]}]}`,
		"bad pattern":              `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"brand","operator":"REGEXP_MATCH","values":["(["]}]}`,
		"operator of another kind": `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"clicks","operator":"LIKE","values":["1"]}]}`,
		"text for a number":        `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"clicks","operator":">","values":["1 OR 1"]}]}`,
		"lower case enum":          `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"marketing_method","operator":"=","values":["ads' --"]}]}`,
		"between with one value":   `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"clicks","operator":"BETWEEN","values":[1]}]}`,
		"two values for equals":    `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"brand","operator":"=","values":["a","b"]}]}`,
		"no filter on a price":     `{"table":"product_view","fields":["price"],"conditions":[{"field":"price","operator":"=","values":["1"]}]}`,
		"no filter on rank":        `{"table":"competitive_visibility_benchmark_view","fields":["your_domain_visibility_trend"],"conditions":[{"field":"your_domain_visibility_trend","operator":">","values":[1]}]}`,
		"metric not selected":      `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"impressions","operator":">","values":[1]}]}`,
		"two conditions on a field": `{` + perf + `,"fields":["clicks"],"conditions":[{"field":"clicks","operator":">","values":[1]},` +
			`{"field":"clicks","operator":"<","values":[9]}]}`,
		"reporting context in select":  `{"table":"product_view","fields":["reporting_context"]}`,
		"best sellers without a where": `{"table":"best_sellers_brand_view","fields":["brand"]}`,
		"competitor without country":   `{"table":"competitive_visibility_competitor_view","fields":["rank"],"date_range":{"window":"TODAY"}}`,
		"date selected in top merchant": `{"table":"competitive_visibility_top_merchant_view","fields":["date"],"date_range":{"window":"TODAY"},` +
			`"conditions":[{"field":"report_country_code","operator":"=","values":["DE"]},{"field":"report_category_id","operator":"=","values":[1]}]}`,
		"order by unselected":  `{` + perf + `,"fields":["clicks"],"order_by":[{"field":"impressions"}]}`,
		"order by a price":     `{"table":"product_view","fields":["price"],"order_by":[{"field":"price"}]}`,
		"order by item issues": `{"table":"product_view","fields":["item_issues"],"order_by":[{"field":"item_issues"}]}`,
		"order by twice":       `{` + perf + `,"fields":["clicks"],"order_by":[{"field":"clicks"},{"field":"clicks","direction":"DESC"}]}`,
		"bad direction":        `{` + perf + `,"fields":["clicks"],"order_by":[{"field":"clicks","direction":"DOWN"}]}`,
		"duplicate fields":     `{` + perf + `,"fields":["clicks","clicks"]}`,
		"too many conditions":  `{` + perf + `,"fields":["clicks"],"conditions":[` + strings.Repeat(`{"field":"brand","operator":"=","values":["a"]},`, 10) + `{"field":"title","operator":"=","values":["a"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if q, err := report(t, args); !isInvalidRequest(err) {
				t.Errorf("query = %q, err = %v, want an invalid request", q, err)
			}
		})
	}
}

// reportRequest records the body of every API request.
type reportRequest struct{ method, path, body, content string }

func recordReports(h *harness, reply func(n int, req *http.Request) (int, string, http.Header)) *[]reportRequest {
	var seen []reportRequest
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
		}
		seen = append(seen, reportRequest{req.Method, req.URL.RequestURI(), string(body), req.Header.Get("Content-Type")})
		return reply(len(seen), req)
	}
	return &seen
}

const performanceArgs = `{"account":"7","table":"product_performance_view","fields":["offer_id","clicks"],` +
	`"date_range":{"window":"LAST_30_DAYS"}`

func TestReportsSearchPostsTheBuiltQueryAndShowsRowsWithHasMore(t *testing.T) {
	h := newHarness(t)
	seen := recordReports(h, func(n int, req *http.Request) (int, string, http.Header) {
		if n == 1 {
			return 200, `{"results":[{"productPerformanceView":{"offerId":"sku-1","clicks":"12","impressions":"9","clickThroughRate":0.5,` +
				`"title":"` + strings.Repeat("t", 400) + `","unknownMember":"never held","brand":42}}],"nextPageToken":"next-one"}`, nil
		}
		return 200, `{"results":[{"productPerformanceView":{"offerId":"sku-2","clicks":"3"}}]}`, nil
	})
	first, err := h.call(t, handleReportsSearch, performanceArgs+`}`)
	if err != nil {
		t.Fatal(err)
	}
	row := first["rows"].([]any)[0].(map[string]any)
	if first["table"] != "product_performance_view" || first["has_more"] != true || row["offerId"] != "sku-1" ||
		row["clicks"] != "12" || row["clickThroughRate"] != 0.5 || row["unknownMember"] != nil || row["brand"] != nil ||
		len([]rune(row["title"].(string))) != maxNameRunes {
		t.Fatalf("first = %v", first)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "next-one" {
		t.Fatalf("next_cursor = %q", cursor)
	}
	second, err := h.call(t, handleReportsSearch, performanceArgs+`,"cursor":"`+cursor+`"}`)
	if err != nil || second["has_more"] != false || second["next_cursor"] != nil {
		t.Fatalf("second = %v, %v", second, err)
	}
	if len(*seen) != 2 {
		t.Fatalf("%d requests", len(*seen))
	}
	for i, r := range *seen {
		if r.method != http.MethodPost || r.path != "/reports/v1/accounts/7/reports:search" || r.content != "application/json" {
			t.Errorf("request %d = %+v", i, r)
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte((*seen)[0].body), &body); err != nil || len(body) != 2 || body["pageSize"] != float64(100) ||
		body["query"] != "SELECT offer_id, clicks FROM product_performance_view WHERE date DURING LAST_30_DAYS" {
		t.Errorf("first body = %v, %v", (*seen)[0].body, err)
	}
	if !strings.Contains((*seen)[1].body, `"pageToken":"next-one"`) {
		t.Errorf("second body = %s", (*seen)[1].body)
	}
}

func TestReportsSearchLimitIsBoundedAtOneThousand(t *testing.T) {
	h := newHarness(t)
	seen := recordReports(h, func(int, *http.Request) (int, string, http.Header) { return 200, `{}`, nil })
	if out, err := h.call(t, handleReportsSearch, performanceArgs+`,"limit":1000}`); err != nil || out["has_more"] != false ||
		len(out["rows"].([]any)) != 0 || !strings.Contains((*seen)[0].body, `"pageSize":1000`) {
		t.Fatalf("out = %v, %v, %v", out, err, *seen)
	}
	for _, limit := range []string{"1001", "-1", "100000"} {
		h := newHarness(t)
		if _, err := h.call(t, handleReportsSearch, performanceArgs+`,"limit":`+limit+`}`); !isInvalidRequest(err) {
			t.Errorf("limit %s: err = %v", limit, err)
		}
		h.untouched(t)
	}
}

func TestReportsSearchRefusesACursorOfAnotherQueryOrAccount(t *testing.T) {
	h := newHarness(t)
	recordReports(h, func(int, *http.Request) (int, string, http.Header) { return 200, `{"nextPageToken":"p"}`, nil })
	first, err := h.call(t, handleReportsSearch, performanceArgs+`}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor := first["next_cursor"].(string)
	other := strings.Replace(performanceArgs, "LAST_30_DAYS", "LAST_7_DAYS", 1)
	for _, args := range []string{other + `,"cursor":"` + cursor + `"}`,
		strings.Replace(performanceArgs, `"7"`, `"8"`, 1) + `,"cursor":"` + cursor + `"}`,
		performanceArgs + `,"cursor":"forged"}`} {
		before := len(h.google.seen())
		if _, err := h.call(t, handleReportsSearch, args); !isInvalidRequest(err) || len(h.google.seen()) != before {
			t.Errorf("args %s: err = %v", args, err)
		}
	}
}

func TestReportsSearchChecksQueryAndAccountBeforeSecretAndRequest(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"account outside the targets": {[]string{"accounts/1"}, performanceArgs + `}`},
		"no account, two targets":     {[]string{"accounts/1", "accounts/2"}, strings.Replace(performanceArgs, `"account":"7",`, "", 1) + `}`},
		"path injection":              {nil, strings.Replace(performanceArgs, `"7"`, `"7/reports"`, 1) + `}`},
		"unknown field":               {nil, `{"account":"7","table":"product_view","fields":["x"]}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, c.targets...)
			if _, err := h.call(t, handleReportsSearch, c.args); !isInvalidRequest(err) {
				t.Fatalf("err = %v", err)
			}
			h.untouched(t)
		})
	}
}

func TestReportsSearchProvesASubAccountWithOneReadOfTheSubAccounts(t *testing.T) {
	h := newHarness(t, "advanced-accounts/10")
	seen := recordReports(h, func(_ int, req *http.Request) (int, string, http.Header) {
		if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
			return 200, `{"accounts":[` + accountRow("7", "Sub") + `]}`, nil
		}
		return 200, `{"results":[]}`, nil
	})
	if _, err := h.call(t, handleReportsSearch, performanceArgs+`}`); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 || (*seen)[0].method != http.MethodGet || (*seen)[1].method != http.MethodPost {
		t.Errorf("requests = %+v", *seen)
	}
}

func TestReportsSearchSendsAfterARenewedTokenOnceAndNeverAfterAnotherFailure(t *testing.T) {
	h := newHarness(t)
	seen := recordReports(h, func(n int, _ *http.Request) (int, string, http.Header) {
		if n == 1 {
			return 401, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, nil
		}
		return 200, `{"results":[]}`, nil
	})
	if _, err := h.call(t, handleReportsSearch, performanceArgs+`}`); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 || (*seen)[0].body != (*seen)[1].body || h.google.grants != 2 {
		t.Errorf("%d requests, %d grants, bodies %q", len(*seen), h.google.grants, *seen)
	}
	for status, class := range map[int]provider.Class{403: provider.ClassPermission, 429: provider.ClassRateLimited,
		400: provider.ClassProviderError, 503: provider.ClassUnreachable} {
		h := newHarness(t)
		seen := recordReports(h, func(int, *http.Request) (int, string, http.Header) {
			return status, quotaBody(reasonDayQuota), nil
		})
		if _, err := h.call(t, handleReportsSearch, performanceArgs+`}`); classOf(err) != class || len(*seen) != 1 {
			t.Errorf("status %d: err = %v, %d requests", status, err, len(*seen))
		}
	}
}

func TestReportsSearchKeepsNestedValuesBoundedAndTyped(t *testing.T) {
	h := newHarness(t)
	issues := strings.Repeat(`{"type":{"code":"c"},"severity":{"aggregatedSeverity":"DEMOTED"}},`, 150)
	recordReports(h, func(int, *http.Request) (int, string, http.Header) {
		return 200, `{"results":[{"productView":{"id":"online~en~US~sku-1","price":{"amountMicros":"1000000","currencyCode":"EUR"},` +
			`"itemIssues":[` + strings.TrimSuffix(issues, ",") + `],"gtin":["0000000000000"],"clickPotentialRank":"abc",` +
			`"thumbnailLink":"https://img.example.invalid/a.png","reportingContext":"FREE_LISTINGS"}}]}`, nil
	})
	out, err := h.call(t, handleReportsSearch, `{"account":"7","table":"product_view","fields":["price","item_issues","gtin"]}`)
	if err != nil {
		t.Fatal(err)
	}
	row := out["rows"].([]any)[0].(map[string]any)
	price := row["price"].(map[string]any)
	if row["id"] != "online~en~US~sku-1" || price["amountMicros"] != "1000000" || price["currencyCode"] != "EUR" ||
		len(row["itemIssues"].([]any)) != maxListItems || len(row["gtin"].([]any)) != 1 ||
		row["clickPotentialRank"] != nil || row["reportingContext"] != nil || row["thumbnailLink"] != "https://img.example.invalid/a.png" {
		t.Errorf("row = %v", row)
	}
}

func TestReportsSearchRefusesMoreRowsThanTheBound(t *testing.T) {
	h := newHarness(t)
	rows := strings.Repeat(`{"productPerformanceView":{}},`, maxEntries+1)
	recordReports(h, func(int, *http.Request) (int, string, http.Header) {
		return 200, `{"results":[` + strings.TrimSuffix(rows, ",") + `]}`, nil
	})
	if _, err := h.call(t, handleReportsSearch, performanceArgs+`}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestReportDescriptorNamesAllTablesAndCatalogFields(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(reportsSearch.InputSchema, &schema); err != nil || !slices.Equal(schema.Required, []string{"table", "fields"}) {
		t.Fatalf("schema = %v, %v", schema, err)
	}
	for _, table := range reportTableNames() {
		if !strings.Contains(string(schema.Properties["table"]), `"`+table+`"`) {
			t.Errorf("table schema lacks %s", table)
		}
	}
	if !strings.Contains(string(schema.Properties["limit"]), `"maximum":1000`) {
		t.Errorf("limit schema = %s", schema.Properties["limit"])
	}
}
