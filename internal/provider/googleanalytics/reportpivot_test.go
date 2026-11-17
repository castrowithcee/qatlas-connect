package googleanalytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// freshRealtimeQuotas gives a test its own realtime quota memory.
func freshRealtimeQuotas(t *testing.T) {
	t.Helper()
	previous := realtimeQuotas
	realtimeQuotas = &quotaBook{entries: map[string]quotaEntry{}, running: map[string]int{}}
	t.Cleanup(func() { realtimeQuotas = previous })
}

const pivotMembersJSON = `"dimensions":[{"name":"country"},{"name":"deviceCategory"}],"metrics":[{"name":"sessions"}],` +
	`"date_ranges":[{"start_date":"7daysAgo","end_date":"today"}],` +
	`"pivots":[{"field_names":["country"],"limit":10,"metric_aggregations":["TOTAL"],` +
	`"order_bys":[{"desc":true,"pivot":{"metric_name":"sessions","pivot_selections":` +
	`[{"dimension_name":"deviceCategory","dimension_value":"mobile"}]}},{"dimension":{"dimension_name":"country"}}]},` +
	`{"field_names":["deviceCategory"],"limit":3,"offset":1,"metric_aggregations":["TOTAL"]}]`

const pivotRequestJSON = `{"property":"properties/200",` + pivotMembersJSON + `}`

// pivotPage answers a pivot report of two pivots, with aggregates and the quota.
func pivotPage(hourLeft int) string {
	return fmt.Sprintf(`{"pivotHeaders":[{"pivotDimensionHeaders":[{"dimensionValues":[{"value":"CH"}]},`+
		`{"dimensionValues":[{"value":"DE"}]}],"rowCount":2},{"pivotDimensionHeaders":[{"dimensionValues":`+
		`[{"value":"mobile"}]},{"dimensionValues":[{"value":"desktop"}]}],"rowCount":2}],`+
		`"dimensionHeaders":[{"name":"country"},{"name":"deviceCategory"}],`+
		`"metricHeaders":[{"name":"sessions","type":"TYPE_INTEGER"}],`+
		`"rows":[{"dimensionValues":[{"value":"mobile"}],"metricValues":[{"value":"5"},{"value":"3"}]},`+
		`{"dimensionValues":[{"value":"desktop"}],"metricValues":[{"value":"2"},{"value":"1"}]}],`+
		`"aggregates":[{"dimensionValues":[{"value":"RESERVED_TOTAL"}],"metricValues":[{"value":"7"},{"value":"4"}]}],`+
		`"metadata":{"currencyCode":"USD","timeZone":"Europe/Zurich","subjectToThresholding":true},`+quotaJSON+
		`,"kind":"analyticsData#runPivotReport"}`, hourLeft)
}

func TestReportsRunPivotSendsTwoPivotsAndReturnsHeadersAndAggregates(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, pivotPage(39000))})
	result, err := env.call(reportsRunPivotID, "read", pivotRequestJSON)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].method != http.MethodPost || requests[0].host != "analyticsdata.googleapis.com" ||
		requests[0].path != "/v1beta/properties/200:runPivotReport" || requests[0].rawQuery != "" {
		t.Fatalf("requests = %+v", requests)
	}
	body := bodyOf(t, requests[0])
	pivots, _ := json.Marshal(body["pivots"])
	if string(pivots) != `[{"fieldNames":["country"],"limit":"10","metricAggregations":["TOTAL"],"orderBys":`+
		`[{"desc":true,"pivot":{"metricName":"sessions","pivotSelections":[{"dimensionName":"deviceCategory",`+
		`"dimensionValue":"mobile"}]}},{"dimension":{"dimensionName":"country"}}]},`+
		`{"fieldNames":["deviceCategory"],"limit":"3","metricAggregations":["TOTAL"],"offset":"1"}]` ||
		body["returnPropertyQuota"] != true || len(body) != 5 {
		t.Errorf("body = %s", requests[0].body)
	}
	var out pivotResult
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.PivotHeaders) != 2 ||
		out.PivotHeaders[0].RowCount != 2 || out.PivotHeaders[1].PivotDimensionHeaders[1].DimensionValues[0].Value != "desktop" ||
		len(out.Rows) != 2 || len(out.Rows[0].MetricValues) != 2 || len(out.Aggregates) != 1 ||
		out.Aggregates[0].DimensionValues[0].Value != "RESERVED_TOTAL" || !out.Metadata.SubjectToThresholding ||
		out.Kind != "analyticsData#runPivotReport" || out.PropertyQuota == nil || len(out.Truncated) != 0 {
		t.Fatalf("result = %s, %v", result, err)
	}
}

func TestReportsRunPivotChecksTheLimitsBeforeAnyIO(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{})
	rest := `"metrics":[{"name":"sessions"}],"date_ranges":[{"start_date":"today","end_date":"today"}]`
	pivot := func(fields string, limit int) string {
		return fmt.Sprintf(`{"field_names":[%s],"limit":%d}`, fields, limit)
	}
	elevenDimensions := make([]string, 11)
	elevenPivots := make([]string, 11)
	for i := range elevenPivots {
		elevenDimensions[i] = fmt.Sprintf(`{"name":"d%d"}`, i)
		elevenPivots[i] = pivot(fmt.Sprintf(`"d%d"`, i), 1)
	}
	for name, arguments := range map[string]string{
		"more pivots than allowed": `{"property":"properties/200","dimensions":[` + strings.Join(elevenDimensions, ",") +
			`],` + rest + `,"pivots":[` + strings.Join(elevenPivots, ",") + `]}`,
		"product above 250000": `{"property":"properties/200","dimensions":[{"name":"a"},{"name":"b"}],` + rest +
			`,"pivots":[` + pivot(`"a"`, 1000) + `,` + pivot(`"b"`, 251) + `]}`,
		"limit above 250000": `{"property":"properties/200","dimensions":[{"name":"a"}],` + rest + `,"pivots":[` + pivot(`"a"`, 250001) + `]}`,
		"limit missing":      `{"property":"properties/200","dimensions":[{"name":"a"}],` + rest + `,"pivots":[{"field_names":["a"]}]}`,
		"no pivot":           `{"property":"properties/200","dimensions":[{"name":"a"}],` + rest + `,"pivots":[]}`,
		"field is no dimension": `{"property":"properties/200","dimensions":[{"name":"a"}],` + rest + `,"pivots":[` +
			pivot(`"b"`, 5) + `]}`,
		"dimension shared by two pivots": `{"property":"properties/200","dimensions":[{"name":"a"}],` + rest +
			`,"pivots":[` + pivot(`"a"`, 5) + `,` + pivot(`"a"`, 5) + `]}`,
		"dimension order outside the pivot": `{"property":"properties/200","dimensions":[{"name":"a"},{"name":"b"}],` +
			rest + `,"pivots":[{"field_names":["a"],"limit":5,"order_bys":[{"dimension":{"dimension_name":"b"}}]}]}`,
		"order with two kinds": `{"property":"properties/200","dimensions":[{"name":"a"}],` + rest +
			`,"pivots":[{"field_names":["a"],"limit":5,"order_bys":[{"metric":{"metric_name":"sessions"},` +
			`"dimension":{"dimension_name":"a"}}]}]}`,
		"no date range": `{"property":"properties/200","dimensions":[{"name":"a"}],"metrics":[{"name":"sessions"}],` +
			`"pivots":[` + pivot(`"a"`, 5) + `]}`,
		"offset in the request": `{"property":"properties/200","offset":1,"dimensions":[{"name":"a"}],` + rest +
			`,"pivots":[` + pivot(`"a"`, 5) + `]}`,
	} {
		if _, err := env.call(reportsRunPivotID, "read", arguments); !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := env.call(reportsBatchRunPivotID, "read", `{"property":"properties/200","requests":[`+
			strings.Replace(arguments, `{"property":"properties/200",`, `{`, 1)+`]}`); !refusedInvalid(err) {
			t.Errorf("batch %s: %v", name, err)
		}
	}
	// The largest allowed product passes.
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, pivotPage(39000))})
	ok := `{"property":"properties/200","dimensions":[{"name":"a"},{"name":"b"}],` + rest + `,"pivots":[` +
		pivot(`"a"`, 1000) + `,` + pivot(`"b"`, 250) + `]}`
	if _, err := env.call(reportsRunPivotID, "read", ok); err != nil {
		t.Errorf("product 250000: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d", len(requests))
	}
}

func TestReportsRunPivotRemembersTheQuotaAndStopsWhenLow(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, pivotPage(minTokensLeft-1))})
	if _, err := env.call(reportsRunPivotID, "read", pivotRequestJSON); err != nil {
		t.Fatal(err)
	}
	before, _ := env.fake.seen()
	_, err := env.call(reportsRunPivotID, "read", pivotRequestJSON)
	after, _ := env.fake.seen()
	if classOf(err) != provider.ClassRateLimited || len(after) != len(before) {
		t.Errorf("err = %v, requests %d -> %d", err, len(before), len(after))
	}
	// The core quota memory is shared with reports.run, the realtime one is not.
	if _, err := env.call(reportsRunID, "read", `{"property":"properties/200",`+minimalReport[1:]); classOf(err) != provider.ClassRateLimited {
		t.Errorf("reports.run: %v", err)
	}
}

const batchQuotaless = `{"rows":[],"rowCount":0}`

func batchRunRequests(n int) string {
	list := make([]string, n)
	for i := range list {
		list[i] = fmt.Sprintf(`{"dimensions":[{"name":"pagePath"}],"metrics":[{"name":"sessions"}],`+
			`"date_ranges":[{"start_date":"7daysAgo","end_date":"today"}],"limit":2,"offset":%d}`, i)
	}
	return `{"property":"properties/200","requests":[` + strings.Join(list, ",") + `]}`
}

func batchPage(n int) string {
	list := make([]string, n)
	for i := range list {
		list[i] = reportPage(2, 10, 39000-i)
	}
	return `{"reports":[` + strings.Join(list, ",") + `],"kind":"analyticsData#batchRunReports"}`
}

func TestReportsBatchRunSendsFiveReportsInOrderAndContinuesByOffset(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, batchPage(5))})
	result, err := env.call(reportsBatchRunID, "read", batchRunRequests(5))
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].path != "/v1beta/properties/200:batchRunReports" ||
		requests[0].host != "analyticsdata.googleapis.com" {
		t.Fatalf("requests = %+v", requests)
	}
	var sent struct {
		Requests []map[string]any `json:"requests"`
	}
	if err := json.Unmarshal([]byte(requests[0].body), &sent); err != nil || len(sent.Requests) != 5 {
		t.Fatalf("body = %s, %v", requests[0].body, err)
	}
	for i, r := range sent.Requests {
		offset, hasOffset := r["offset"]
		if r["limit"] != "2" || r["returnPropertyQuota"] != true || (i == 0) == hasOffset ||
			(hasOffset && offset != fmt.Sprint(i)) || r["property"] != nil {
			t.Errorf("request %d = %v", i, r)
		}
	}
	var out struct {
		Kind    string `json:"kind"`
		Reports []struct {
			reportResult
			NextOffset *int64 `json:"next_offset"`
		} `json:"reports"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Reports) != 5 ||
		out.Kind != "analyticsData#batchRunReports" || strings.Contains(result, "next_cursor") {
		t.Fatalf("result = %s, %v", result, err)
	}
	for i, r := range out.Reports {
		if len(r.Rows) != 2 || r.Offset != int64(i) || r.Limit != 2 || !r.HasMore || r.NextOffset == nil ||
			*r.NextOffset != int64(i)+2 || r.PropertyQuota == nil || *r.PropertyQuota.TokensPerHour.Remaining != int32(39000-i) {
			t.Errorf("report %d = %+v", i, r)
		}
	}
	// The remainder of the last report is the one the memory keeps.
	if got := quotas.entries["properties/200\x00login\x00"].left.hour; got != 38996 {
		t.Errorf("remembered hour = %d", got)
	}
}

func TestReportsBatchRunRefusesSixReportsAndBadReportsBeforeAnyIO(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{})
	good := `{"metrics":[{"name":"sessions"}],"date_ranges":[{"start_date":"today","end_date":"today"}]}`
	for name, arguments := range map[string]string{
		"six reports":              batchRunRequests(6),
		"no reports":               `{"property":"properties/200","requests":[]}`,
		"cursor":                   `{"property":"properties/200","requests":[{"cursor":"abc",` + good[1:] + `]}`,
		"property inside":          `{"property":"properties/200","requests":[{"property":"properties/200",` + good[1:] + `]}`,
		"second has no date range": `{"property":"properties/200","requests":[` + good + `,{"metrics":[{"name":"sessions"}]}]}`,
		"second has two filter kinds": `{"property":"properties/200","requests":[` + good + `,{"metrics":[{"name":"sessions"}],` +
			`"date_ranges":[{"start_date":"today","end_date":"today"}],"dimension_filter":{"filter":{"field_name":"a"}}}]}`,
		"no requests member": `{"property":"properties/200"}`,
	} {
		if _, err := env.call(reportsBatchRunID, "read", arguments); !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if requests, _ := env.fake.seen(); len(requests) != 0 || env.reads.Load() != 0 {
		t.Errorf("a refused batch was sent or read a secret: %d requests", len(requests))
	}
}

func TestReportsBatchRunFailsAsAWholeAndRefusesAMisalignedAnswer(t *testing.T) {
	freshQuotas(t)
	// Google reports one error for the batch; none of its reports is returned and nothing is repeated.
	env := newEnv(t, &fakeGoogle{api: always(http.StatusBadRequest, errorObject(400, "INVALID_ARGUMENT", "X"))})
	result, err := env.call(reportsBatchRunID, "read", batchRunRequests(3))
	requests, _ := env.fake.seen()
	if err == nil || leaks(err.Error()) != "" || strings.Contains(result, "rows") || len(requests) != 1 {
		t.Errorf("400: %v, requests %d", err, len(requests))
	}
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, batchPage(2))})
	if _, err := env.call(reportsBatchRunID, "read", batchRunRequests(3)); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("two answers for three requests: %v", err)
	}
	// An empty report does not discard the others.
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"reports":[`+batchQuotaless+`,`+reportPage(1, 1, 39000)+`]}`)})
	result, err = env.call(reportsBatchRunID, "read", batchRunRequests(2))
	var out struct {
		Reports []reportResult `json:"reports"`
	}
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Reports) != 2 ||
		len(out.Reports[0].Rows) != 0 || len(out.Reports[1].Rows) != 1 {
		t.Errorf("result = %s, %v", result, err)
	}
}

func TestReportsBatchCountsEveryReportAgainstSlotsAndReserve(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, batchPage(5))})
	// Six running reports leave room for four more: a batch of five is refused before any I/O, one of four runs.
	quotas.running["properties/200"] = 6
	_, err := env.call(reportsBatchRunID, "read", batchRunRequests(5))
	if classOf(err) != provider.ClassRateLimited || !strings.Contains(err.Error(), "6 reports") {
		t.Errorf("batch over the slots: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 0 {
		t.Errorf("requests = %d", len(requests))
	}
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, batchPage(4))})
	if _, err := env.call(reportsBatchRunID, "read", batchRunRequests(4)); err != nil {
		t.Errorf("batch of four: %v", err)
	}
	if quotas.running["properties/200"] != 6 {
		t.Errorf("slots after the batch = %d", quotas.running["properties/200"])
	}
	// The reserve grows with the number of reports: 499 tokens fit one report, but not five.
	freshQuotas(t)
	quotas.remember("properties/200", "login\x00", &propertyQuota{TokensPerHour: &quotaStatus{Remaining: ptr(int32(499))}})
	if _, err := env.call(reportsBatchRunID, "read", batchRunRequests(5)); classOf(err) != provider.ClassRateLimited ||
		!strings.Contains(err.Error(), "reserve 500") {
		t.Errorf("batch over the reserve: %v", err)
	}
	if _, err := env.call(reportsBatchRunID, "read", batchRunRequests(4)); err != nil {
		t.Errorf("batch within the reserve: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestReportsBatchRunPivotSendsReportsInOrderAndBoundsCells(t *testing.T) {
	freshQuotas(t)
	cells := make([]string, maxPivotCells+5)
	for i := range cells {
		cells[i] = `{"value":"1"}`
	}
	wide := `{"pivotHeaders":[],"rows":[{"dimensionValues":[{"value":"x"}],"metricValues":[` + strings.Join(cells, ",") + `]}]}`
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"pivotReports":[`+pivotPage(39000)+`,`+wide+`],"kind":"analyticsData#batchRunPivotReports"}`)})
	request := `{"property":"properties/200","requests":[{` + pivotMembersJSON + `},{` + pivotMembersJSON + `,"return_property_quota":false}]}`
	result, err := env.call(reportsBatchRunPivotID, "read", request)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	var sent struct {
		Requests []map[string]any `json:"requests"`
	}
	if len(requests) != 1 || requests[0].path != "/v1beta/properties/200:batchRunPivotReports" ||
		json.Unmarshal([]byte(requests[0].body), &sent) != nil || len(sent.Requests) != 2 ||
		sent.Requests[0]["returnPropertyQuota"] != true || sent.Requests[1]["returnPropertyQuota"] != false {
		t.Fatalf("requests = %+v", requests)
	}
	var out pivotBatchResult
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.PivotReports) != 2 ||
		len(out.PivotReports[0].PivotHeaders) != 2 || len(out.PivotReports[1].Rows[0].MetricValues) != maxPivotCells ||
		len(out.PivotReports[1].Truncated) != 1 || out.PivotReports[1].Truncated[0] != "cells" ||
		out.PivotReports[0].PropertyQuota == nil {
		t.Fatalf("result = %s, %v", result, err)
	}
	// Six pivot reports are refused, a misaligned answer too.
	if _, err := env.call(reportsBatchRunPivotID, "read", `{"property":"properties/200","requests":[`+
		strings.Repeat(`{`+pivotMembersJSON+`},`, 5)+`{`+pivotMembersJSON+`}]}`); !refusedInvalid(err) {
		t.Errorf("six: %v", err)
	}
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"pivotReports":[`+pivotPage(39000)+`]}`)})
	if _, err := env.call(reportsBatchRunPivotID, "read", request); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("misaligned: %v", err)
	}
	// One slot per report and the grown reserve apply here too.
	quotas.running["properties/200"] = 9
	if _, err := env.call(reportsBatchRunPivotID, "read", request); classOf(err) != provider.ClassRateLimited {
		t.Errorf("batch over the slots: %v", err)
	}
}

func TestPivotAndBatchToolsFollowTheBindingAndMapFailures(t *testing.T) {
	freshQuotas(t)
	pivotBatch := `{"property":"properties/201","requests":[{` + pivotMembersJSON + `}]}`
	runBatch := strings.Replace(batchRunRequests(1), "properties/200", "properties/201", 1)
	for name, c := range map[string]struct{ id, arguments string }{
		"pivot":       {reportsRunPivotID, strings.Replace(pivotRequestJSON, "properties/200", "properties/201", 1)},
		"batch":       {reportsBatchRunID, runBatch},
		"batch pivot": {reportsBatchRunPivotID, pivotBatch},
	} {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
		// A property outside the binding is refused before any secret or request, without naming it.
		_, err := env.call(c.id, "property", c.arguments)
		if requests, _ := env.fake.seen(); !refusedInvalid(err) || len(requests) != 0 || env.reads.Load() != 0 ||
			strings.Contains(err.Error(), "201") {
			t.Errorf("%s foreign property: %v, requests %d", name, err, len(requests))
		}
		for status, class := range map[int]provider.Class{http.StatusForbidden: provider.ClassPermission,
			http.StatusTooManyRequests: provider.ClassRateLimited, http.StatusNotFound: provider.ClassNotFound} {
			env = newEnv(t, &fakeGoogle{api: always(status, errorObject(status, "X", "X"))})
			_, err = env.call(c.id, "read", c.arguments)
			requests, _ := env.fake.seen()
			if classOf(err) != class || leaks(err.Error()) != "" || len(requests) != 1 {
				t.Errorf("%s %d: %v, requests %d", name, status, err, len(requests))
			}
		}
	}
	// A property of a bound account is proven by one read before the report.
	fake := &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if r.URL.Host == "analyticsadmin.googleapis.com" {
			return respond(r, http.StatusOK, "application/json", `{"name":"properties/201","parent":"accounts/100"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", pivotPage(39000)), nil
	}}
	env := newEnv(t, fake)
	if _, err := env.call(reportsRunPivotID, "account", strings.Replace(pivotRequestJSON, "properties/200", "properties/201", 1)); err != nil {
		t.Fatal(err)
	}
	if requests, _ := fake.seen(); len(requests) != 2 || requests[0].host != "analyticsadmin.googleapis.com" {
		t.Errorf("requests = %+v", requests)
	}
}
