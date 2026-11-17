package googleanalytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// realtimePage answers a realtime report with rows rows of one dimension and one metric, and the quota.
func realtimePage(rows, rowCount, hourLeft int) string {
	list := make([]string, rows)
	for i := range list {
		list[i] = fmt.Sprintf(`{"dimensionValues":[{"value":"CH"},{"value":"%d"}],"metricValues":[{"value":"%d"}]}`, i%2, i+1)
	}
	return fmt.Sprintf(`{"dimensionHeaders":[{"name":"country"},{"name":"dateRange"}],`+
		`"metricHeaders":[{"name":"activeUsers","type":"TYPE_INTEGER"}],"rows":[%s],`+
		`"totals":[{"dimensionValues":[{"value":"RESERVED_TOTAL"}],"metricValues":[{"value":"9"}]}],`+
		`"rowCount":%d,`+quotaJSON+`,"kind":"analyticsData#runRealtimeReport"}`, strings.Join(list, ","), rowCount, hourLeft)
}

const realtimeRequestJSON = `{"property":"properties/200","dimensions":[{"name":"country"}],` +
	`"metrics":[{"name":"activeUsers"},{"name":"perEvent","expression":"activeUsers/eventCount"}],` +
	`"minute_ranges":[{"start_minutes_ago":29,"end_minutes_ago":15,"name":"early"},{"start_minutes_ago":14}],` +
	`"dimension_filter":{"filter":{"field_name":"country","string_filter":{"value":"CH"}}},` +
	`"metric_aggregations":["TOTAL"],"order_bys":[{"desc":true,"metric":{"metric_name":"activeUsers"}}],"limit":50}`

func TestReportsRunRealtimeSendsMinuteRangesAndReturnsTheRealtimeQuota(t *testing.T) {
	freshQuotas(t)
	freshRealtimeQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, realtimePage(3, 3, 39000))})
	result, err := env.call(reportsRunRealtimeID, "read", realtimeRequestJSON)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].method != http.MethodPost || requests[0].host != "analyticsdata.googleapis.com" ||
		requests[0].path != "/v1beta/properties/200:runRealtimeReport" || requests[0].rawQuery != "" {
		t.Fatalf("requests = %+v", requests)
	}
	body := bodyOf(t, requests[0])
	for key, want := range map[string]string{
		"minuteRanges": `[{"endMinutesAgo":15,"name":"early","startMinutesAgo":29},{"startMinutesAgo":14}]`,
		"limit":        `"50"`, "returnPropertyQuota": `true`, "metricAggregations": `["TOTAL"]`,
		"orderBys":        `[{"desc":true,"metric":{"metricName":"activeUsers"}}]`,
		"dimensionFilter": `{"filter":{"fieldName":"country","stringFilter":{"value":"CH"}}}`,
		"metrics":         `[{"name":"activeUsers"},{"expression":"activeUsers/eventCount","name":"perEvent"}]`,
		"dimensions":      `[{"name":"country"}]`,
	} {
		got, _ := json.Marshal(body[key])
		if string(got) != want {
			t.Errorf("body[%s] = %s, want %s", key, got, want)
		}
	}
	if len(body) != 8 {
		t.Errorf("body = %s", requests[0].body)
	}
	var out realtimeResult
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Rows) != 3 || out.RowCount != 3 ||
		len(out.Totals) != 1 || out.Limit != 50 || out.LimitCapped || out.Kind != "analyticsData#runRealtimeReport" ||
		out.PropertyQuota == nil || *out.PropertyQuota.TokensPerHour.Remaining != 39000 ||
		strings.Contains(result, "next_cursor") || strings.Contains(result, `"offset"`) {
		t.Fatalf("result = %s, %v", result, err)
	}
}

func TestReportsRunRealtimeRefusesNamesOfTheCoreReportsBeforeAnyIO(t *testing.T) {
	freshQuotas(t)
	freshRealtimeQuotas(t)
	env := newEnv(t, &fakeGoogle{})
	for name, arguments := range map[string]string{
		"core metric":     `{"property":"properties/200","metrics":[{"name":"sessions"}]}`,
		"core dimension":  `{"property":"properties/200","dimensions":[{"name":"pagePath"}],"metrics":[{"name":"activeUsers"}]}`,
		"event custom":    `{"property":"properties/200","dimensions":[{"name":"customEvent:level"}],"metrics":[{"name":"activeUsers"}]}`,
		"expression name": `{"property":"properties/200","metrics":[{"name":"perSession","expression":"activeUsers/sessions"}]}`,
		"expression of a dimension": `{"property":"properties/200","dimensions":[{"name":"x","dimension_expression":` +
			`{"lower_case":{"dimension_name":"pagePath"}}}],"metrics":[{"name":"activeUsers"}]}`,
		"no metric":        `{"property":"properties/200","dimensions":[{"name":"country"}]}`,
		"date ranges":      `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"date_ranges":[{"start_date":"today","end_date":"today"}]}`,
		"offset":           `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"offset":5}`,
		"cursor":           `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"cursor":"abc"}`,
		"three ranges":     `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"minute_ranges":[{},{},{}]}`,
		"range above 59":   `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"minute_ranges":[{"start_minutes_ago":60}]}`,
		"start after end":  `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"minute_ranges":[{"start_minutes_ago":3,"end_minutes_ago":5}]}`,
		"default start":    `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"minute_ranges":[{"end_minutes_ago":30}]}`,
		"reserved name":    `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"minute_ranges":[{"name":"RESERVED_x"}]}`,
		"generated name":   `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"minute_ranges":[{"name":"date_range_1"}]}`,
		"bad property":     `{"property":"accounts/1","metrics":[{"name":"activeUsers"}]}`,
		"two filter kinds": `{"property":"properties/200","metrics":[{"name":"activeUsers"}],"dimension_filter":{"filter":{"field_name":"country"}}}`,
	} {
		if _, err := env.call(reportsRunRealtimeID, "read", arguments); !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if requests, _ := env.fake.seen(); len(requests) != 0 || env.reads.Load() != 0 {
		t.Errorf("a refused report was sent or read a secret: %d requests", len(requests))
	}
	// The realtime names, a user-scoped custom dimension and a number in an expression pass.
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, realtimePage(1, 1, 39000))})
	if _, err := env.call(reportsRunRealtimeID, "read", `{"property":"properties/200","dimensions":[{"name":"customUser:level"},`+
		`{"name":"unifiedScreenName"},{"name":"minutesAgo"}],"metrics":[{"name":"keyEvents"},{"name":"screenPageViews"},`+
		`{"name":"scaled","expression":"eventCount*1e3"}]}`); err != nil {
		t.Errorf("realtime names: %v", err)
	}
}

func TestReportsRunRealtimeCapsTheLimitAndBoundsTheRows(t *testing.T) {
	freshQuotas(t)
	freshRealtimeQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, realtimePage(3, 3, 39000))})
	result, err := env.call(reportsRunRealtimeID, "read", `{"property":"properties/200","limit":250000,`+
		`"metrics":[{"name":"activeUsers"}]}`)
	requests, _ := env.fake.seen()
	var out realtimeResult
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || out.Limit != maxReportRows || !out.LimitCapped ||
		bodyOf(t, requests[0])["limit"] != "10000" {
		t.Errorf("result = %s, %v", result, err)
	}
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, realtimePage(5, 20, 39000))})
	result, err = env.call(reportsRunRealtimeID, "read", `{"property":"properties/200","limit":2,`+
		`"metrics":[{"name":"activeUsers"}]}`)
	out = realtimeResult{}
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Rows) != 2 || out.RowCount != 20 ||
		len(out.Truncated) != 1 || out.Truncated[0] != "rows" {
		t.Errorf("cut rows: %s, %v", result, err)
	}
}

func TestReportsRunRealtimeKeepsItsOwnQuotaApartFromTheCoreReports(t *testing.T) {
	freshQuotas(t)
	freshRealtimeQuotas(t)
	core := `{"property":"properties/200",` + minimalReport[1:]
	realtime := `{"property":"properties/200","metrics":[{"name":"activeUsers"}]}`
	// A low realtime remainder stops realtime reports and leaves the core reports alone.
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, realtimePage(1, 1, minTokensLeft-1))})
	if _, err := env.call(reportsRunRealtimeID, "read", realtime); err != nil {
		t.Fatal(err)
	}
	before, _ := env.fake.seen()
	_, err := env.call(reportsRunRealtimeID, "read", realtime)
	after, _ := env.fake.seen()
	if classOf(err) != provider.ClassRateLimited || len(after) != len(before) {
		t.Errorf("realtime after a low remainder: %v, requests %d -> %d", err, len(before), len(after))
	}
	if _, err := env.call(reportsRunID, "read", core); err != nil {
		t.Errorf("core report after a low realtime remainder: %v", err)
	}
	// A low core remainder does not stop a realtime report.
	freshQuotas(t)
	freshRealtimeQuotas(t)
	env = newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, ":runRealtimeReport") {
			return respond(r, http.StatusOK, "application/json", realtimePage(1, 1, 39000)), nil
		}
		return respond(r, http.StatusOK, "application/json", reportPage(1, 1, minTokensLeft-1)), nil
	}})
	if _, err := env.call(reportsRunID, "read", core); err != nil {
		t.Fatal(err)
	}
	if _, err := env.call(reportsRunID, "read", core); classOf(err) != provider.ClassRateLimited {
		t.Errorf("core after a low core remainder: %v", err)
	}
	if _, err := env.call(reportsRunRealtimeID, "read", realtime); err != nil {
		t.Errorf("realtime after a low core remainder: %v", err)
	}
	// Without return_property_quota the answer teaches nothing.
	freshRealtimeQuotas(t)
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"rows":[],"rowCount":0}`)})
	result, err := env.call(reportsRunRealtimeID, "read", `{"property":"properties/200","return_property_quota":false,`+
		`"metrics":[{"name":"activeUsers"}]}`)
	requests, _ := env.fake.seen()
	if err != nil || strings.Contains(result, "propertyQuota") || bodyOf(t, requests[0])["returnPropertyQuota"] != false {
		t.Errorf("result = %s, %v", result, err)
	}
}

func TestReportsRunRealtimeLimitsParallelReportsPerProperty(t *testing.T) {
	freshQuotas(t)
	freshRealtimeQuotas(t)
	release := make(chan struct{})
	arrived := make(chan struct{}, 32)
	env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		arrived <- struct{}{}
		<-release
		return respond(r, http.StatusOK, "application/json", realtimePage(1, 1, 39000)), nil
	}})
	arguments := `{"property":"properties/200","metrics":[{"name":"activeUsers"}]}`
	var wg sync.WaitGroup
	for i := 0; i < maxConcurrentReports; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := env.call(reportsRunRealtimeID, "read", arguments); err != nil {
				t.Error(err)
			}
		}()
	}
	for i := 0; i < maxConcurrentReports; i++ {
		<-arrived
	}
	if _, err := env.call(reportsRunRealtimeID, "read", arguments); classOf(err) != provider.ClassRateLimited {
		t.Errorf("eleventh realtime report: %v", err)
	}
	// The slots of the core reports are a separate pool.
	other := make(chan error, 1)
	go func() {
		_, err := env.call(reportsRunID, "read", `{"property":"properties/200",`+minimalReport[1:])
		other <- err
	}()
	<-arrived
	close(release)
	wg.Wait()
	if err := <-other; err != nil {
		t.Errorf("core report beside ten realtime reports: %v", err)
	}
}

func TestReportsRunRealtimeFollowsTheBindingAndMapsFailures(t *testing.T) {
	freshQuotas(t)
	freshRealtimeQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, realtimePage(1, 1, 39000))})
	arguments := `{"property":"properties/201","metrics":[{"name":"activeUsers"}]}`
	_, err := env.call(reportsRunRealtimeID, "property", arguments)
	if requests, _ := env.fake.seen(); !refusedInvalid(err) || len(requests) != 0 || env.reads.Load() != 0 ||
		strings.Contains(err.Error(), "201") {
		t.Errorf("foreign property: %v, requests %d", err, len(requests))
	}
	if _, err := env.call(reportsRunRealtimeID, "property", `{"metrics":[{"name":"activeUsers"}]}`); err != nil {
		t.Errorf("the one bound property is the default: %v", err)
	}
	for status, class := range map[int]provider.Class{http.StatusForbidden: provider.ClassPermission,
		http.StatusTooManyRequests: provider.ClassRateLimited, http.StatusNotFound: provider.ClassNotFound} {
		env = newEnv(t, &fakeGoogle{api: always(status, errorObject(status, "X", "X"))})
		_, err = env.call(reportsRunRealtimeID, "read", arguments)
		requests, _ := env.fake.seen()
		if classOf(err) != class || leaks(err.Error()) != "" || len(requests) != 1 {
			t.Errorf("%d: %v, requests %d", status, err, len(requests))
		}
	}
}
