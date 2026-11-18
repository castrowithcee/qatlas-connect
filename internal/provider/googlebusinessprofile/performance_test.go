package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// performanceHost lets the fake transport of this package answer the Performance API as well: it records
// the request like the other APIs and answers with the same answer function.
type performanceHost struct{ fake *fakeGoogle }

func (p performanceHost) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "businessprofileperformance.googleapis.com" {
		return p.fake.RoundTrip(r)
	}
	p.fake.mu.Lock()
	p.fake.api = append(p.fake.api, seen{host: r.URL.Host, path: r.URL.EscapedPath(), query: r.URL.RawQuery,
		authorization: r.Header.Get("Authorization"), full: r.URL.String()})
	n := len(p.fake.api)
	answer := p.fake.answer
	p.fake.mu.Unlock()
	if answer != nil {
		return answer(r, n)
	}
	return respond(r, http.StatusOK, "application/json", `{}`), nil
}

func newPerformanceEnvironment(t *testing.T) *environment {
	t.Helper()
	env := newEnvironment(t, nil)
	transport = performanceHost{env.fake}
	return env
}

const (
	locArg  = `"location":"` + boundLocation + `"`
	rangeQ1 = `"start_date":"2026-01-01","end_date":"2026-03-31"`
)

func TestGetDailyMetricReturnsTheSeries(t *testing.T) {
	env := newPerformanceEnvironment(t)
	env.fake.answer = reply(http.StatusOK, `{"timeSeries":{"datedValues":[`+
		`{"date":{"year":2026,"month":1,"day":1},"value":"7"},`+
		`{"date":{"year":2026,"month":1,"day":2}},`+
		`{"date":{"year":2026,"month":1,"day":3},"value":"12"}]},"undocumented":"`+detailCanary+`"}`)
	out, err := env.invoke(t, performanceGetDailyID, "all", `{`+locArg+`,"daily_metric":"WEBSITE_CLICKS",`+rangeQ1+
		`,"sub_entity_day_of_week":"MONDAY","sub_entity_time_of_day":{"hours":13,"minutes":34,"seconds":20,"nanos":0}}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"timeSeries": map[string]any{"datedValues": []any{
		map[string]any{"date": map[string]any{"year": 2026.0, "month": 1.0, "day": 1.0}, "value": 7.0},
		map[string]any{"date": map[string]any{"year": 2026.0, "month": 1.0, "day": 2.0}, "value": 0.0},
		map[string]any{"date": map[string]any{"year": 2026.0, "month": 1.0, "day": 3.0}, "value": 12.0}}}}
	if !reflect.DeepEqual(out, want) {
		got, _ := json.Marshal(out)
		t.Errorf("result = %s", got)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].host != "businessprofileperformance.googleapis.com" ||
		calls[0].path != "/v1/"+boundLocation+":getDailyMetricsTimeSeries" ||
		calls[0].authorization != "Bearer "+accessPrefix+"1" {
		t.Fatalf("requests = %+v", calls)
	}
	wantQuery := map[string]string{"dailyMetric": "WEBSITE_CLICKS",
		"dailyRange.startDate.year": "2026", "dailyRange.startDate.month": "1", "dailyRange.startDate.day": "1",
		"dailyRange.endDate.year": "2026", "dailyRange.endDate.month": "3", "dailyRange.endDate.day": "31",
		"dailySubEntityType.dayOfWeek": "MONDAY", "dailySubEntityType.timeOfDay.hours": "13",
		"dailySubEntityType.timeOfDay.minutes": "34", "dailySubEntityType.timeOfDay.seconds": "20",
		"dailySubEntityType.timeOfDay.nanos": "0"}
	values := calls[0].values()
	if len(values) != len(wantQuery) {
		t.Errorf("query = %v", values)
	}
	for key, value := range wantQuery {
		if values.Get(key) != value {
			t.Errorf("query %s = %q, want %q", key, values.Get(key), value)
		}
	}
}

func TestGetDailyMetricAnswersAnEmptySeries(t *testing.T) {
	env := newPerformanceEnvironment(t)
	for _, body := range []string{`{}`, `{"timeSeries":{}}`} {
		env.fake.answer = reply(http.StatusOK, body)
		out, err := env.invoke(t, performanceGetDailyID, "all", `{`+locArg+`,"daily_metric":"CALL_CLICKS",`+rangeQ1+`}`)
		if err != nil {
			t.Fatal(err)
		}
		// The broker leaves an empty list out of a result: missing means empty.
		if len(out) != 0 {
			t.Errorf("%s: result = %v, want an empty series that is not cut", body, out)
		}
	}
}

func TestFetchDailyMetricsReadsEveryMetricGroup(t *testing.T) {
	env := newPerformanceEnvironment(t)
	var groups []string
	for _, metric := range dailyMetrics {
		groups = append(groups, `{"dailyMetric":"`+metric+`","timeSeries":{"datedValues":[`+
			`{"date":{"year":2026,"month":2,"day":1},"value":"3"}]}}`)
	}
	env.fake.answer = reply(http.StatusOK, `{"multiDailyMetricTimeSeries":[{"dailyMetricTimeSeries":[`+
		strings.Join(groups, ",")+`,{"dailyMetric":"CALL_CLICKS","dailySubEntityType":{"dayOfWeek":"FRIDAY",`+
		`"timeOfDay":{"hours":9}},"timeSeries":{}}]}]}`)
	metrics, _ := json.Marshal(dailyMetrics)
	out, err := env.invoke(t, performanceFetchDailyID, "all", `{`+locArg+`,"daily_metrics":`+string(metrics)+
		`,"start_date":"2026-02-01","end_date":"2026-02-01"}`)
	if err != nil {
		t.Fatal(err)
	}
	series := out["multiDailyMetricTimeSeries"].([]any)[0].(map[string]any)["dailyMetricTimeSeries"].([]any)
	if len(series) != len(dailyMetrics)+1 || out["truncated"] != nil {
		t.Fatalf("result = %v", out)
	}
	for i, metric := range dailyMetrics {
		entry := series[i].(map[string]any)
		if entry["dailyMetric"] != metric || entry["timeSeries"].(map[string]any)["datedValues"].([]any)[0].(map[string]any)["value"] != 3.0 {
			t.Errorf("series %d = %v", i, entry)
		}
	}
	last := series[len(dailyMetrics)].(map[string]any)
	sub := last["dailySubEntityType"].(map[string]any)
	if sub["dayOfWeek"] != "FRIDAY" || sub["timeOfDay"].(map[string]any)["hours"] != 9.0 ||
		last["timeSeries"] != nil {
		t.Errorf("last series = %v", last)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 || calls[0].path != "/v1/"+boundLocation+":fetchMultiDailyMetricsTimeSeries" ||
		!reflect.DeepEqual(calls[0].values()["dailyMetrics"], dailyMetrics) ||
		calls[0].values().Get("dailyRange.endDate.day") != "1" {
		t.Errorf("request = %+v", calls)
	}
}

func TestLongSeriesAreCutAndSaySo(t *testing.T) {
	points := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `{"date":{"year":2026,"month":1,"day":` + strconv.Itoa(i%28+1) + `},"value":"1"}`
		}
		return strings.Join(parts, ",")
	}
	env := newPerformanceEnvironment(t)
	env.fake.answer = reply(http.StatusOK, `{"timeSeries":{"datedValues":[`+points(maxSeriesPoints+5)+`]}}`)
	out, err := env.invoke(t, performanceGetDailyID, "all", `{`+locArg+`,"daily_metric":"CALL_CLICKS",`+rangeQ1+`}`)
	if err != nil {
		t.Fatal(err)
	}
	shown := out["timeSeries"].(map[string]any)["datedValues"].([]any)
	if len(shown) != maxSeriesPoints || out["truncated"] != true || out["points_total"] != float64(maxSeriesPoints+5) ||
		out["note"] == nil {
		t.Errorf("result has %d points, truncated = %v, total = %v", len(shown), out["truncated"], out["points_total"])
	}

	// All series of one answer share a budget.
	var groups []string
	for range 6 {
		groups = append(groups, `{"dailyMetric":"CALL_CLICKS","timeSeries":{"datedValues":[`+points(maxSeriesPoints)+`]}}`)
	}
	env.fake.answer = reply(http.StatusOK, `{"multiDailyMetricTimeSeries":[{"dailyMetricTimeSeries":[`+
		strings.Join(groups, ",")+`]}]}`)
	out, err = env.invoke(t, performanceFetchDailyID, "all", `{`+locArg+`,"daily_metrics":["CALL_CLICKS"],`+rangeQ1+`}`)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, entry := range out["multiDailyMetricTimeSeries"].([]any)[0].(map[string]any)["dailyMetricTimeSeries"].([]any) {
		if one, ok := entry.(map[string]any)["timeSeries"].(map[string]any); ok {
			total += len(one["datedValues"].([]any))
		}
	}
	if total != maxTotalPoints || out["truncated"] != true || out["points_total"] != float64(6*maxSeriesPoints) {
		t.Errorf("result has %d points, truncated = %v, total = %v", total, out["truncated"], out["points_total"])
	}
}

func TestUnknownMetricsAndBadRangesAreRefusedBeforeAnyRequest(t *testing.T) {
	env := newPerformanceEnvironment(t)
	metric := `"daily_metric":"CALL_CLICKS"`
	for name, c := range map[string]struct{ tool, args string }{
		"unknown metric":          {performanceGetDailyID, `{` + locArg + `,"daily_metric":"PAGE_VIEWS",` + rangeQ1 + `}`},
		"unspecified metric":      {performanceGetDailyID, `{` + locArg + `,"daily_metric":"DAILY_METRIC_UNKNOWN",` + rangeQ1 + `}`},
		"deprecated metric":       {performanceGetDailyID, `{` + locArg + `,"daily_metric":"BUSINESS_CONVERSATIONS",` + rangeQ1 + `}`},
		"lower-case metric":       {performanceGetDailyID, `{` + locArg + `,"daily_metric":"call_clicks",` + rangeQ1 + `}`},
		"no metric":               {performanceGetDailyID, `{` + locArg + `,` + rangeQ1 + `}`},
		"unknown in list":         {performanceFetchDailyID, `{` + locArg + `,"daily_metrics":["CALL_CLICKS","FOO"],` + rangeQ1 + `}`},
		"deprecated in list":      {performanceFetchDailyID, `{` + locArg + `,"daily_metrics":["BUSINESS_FOOD_ORDERS"],` + rangeQ1 + `}`},
		"empty list":              {performanceFetchDailyID, `{` + locArg + `,"daily_metrics":[],` + rangeQ1 + `}`},
		"metric twice":            {performanceFetchDailyID, `{` + locArg + `,"daily_metrics":["CALL_CLICKS","CALL_CLICKS"],` + rangeQ1 + `}`},
		"end before start":        {performanceGetDailyID, `{` + locArg + `,` + metric + `,"start_date":"2026-03-31","end_date":"2026-03-30"}`},
		"end before start (list)": {performanceFetchDailyID, `{` + locArg + `,"daily_metrics":["CALL_CLICKS"],"start_date":"2026-03-01","end_date":"2025-12-31"}`},
		"impossible date":         {performanceGetDailyID, `{` + locArg + `,` + metric + `,"start_date":"2026-02-30","end_date":"2026-03-01"}`},
		"month 13":                {performanceGetDailyID, `{` + locArg + `,` + metric + `,"start_date":"2026-13-01","end_date":"2026-13-02"}`},
		"year 0":                  {performanceGetDailyID, `{` + locArg + `,` + metric + `,"start_date":"0000-01-01","end_date":"2026-01-01"}`},
		"wrong form":              {performanceGetDailyID, `{` + locArg + `,` + metric + `,"start_date":"2026-1-1","end_date":"2026-01-02"}`},
		"no range":                {performanceGetDailyID, `{` + locArg + `,` + metric + `}`},
		"hours 24":                {performanceGetDailyID, `{` + locArg + `,` + metric + `,` + rangeQ1 + `,"sub_entity_time_of_day":{"hours":24}}`},
		"unspecified weekday":     {performanceGetDailyID, `{` + locArg + `,` + metric + `,` + rangeQ1 + `,"sub_entity_day_of_week":"DAY_OF_WEEK_UNSPECIFIED"}`},
		"month end before start":  {performanceKeywordsID, `{` + locArg + `,"start_month":"2026-03","end_month":"2026-02"}`},
		"month with a day":        {performanceKeywordsID, `{` + locArg + `,"start_month":"2026-03-01","end_month":"2026-04"}`},
		"month 00":                {performanceKeywordsID, `{` + locArg + `,"start_month":"2026-00","end_month":"2026-04"}`},
		"no months":               {performanceKeywordsID, `{` + locArg + `}`},
		"page size 101":           {performanceKeywordsID, `{` + locArg + `,"start_month":"2026-01","end_month":"2026-02","page_size":101}`},
	} {
		if _, err := env.invoke(t, c.tool, "all", c.args); !isInvalidRequest(err) {
			t.Errorf("%s: error = %v, want invalid-request", name, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Errorf("%d requests and %d grants, want none", len(calls), grants)
	}
}

func keywordsPage(r *http.Request, _ int) (*http.Response, error) {
	switch r.URL.Query().Get("pageToken") {
	case "":
		return respond(r, http.StatusOK, "application/json", `{"searchKeywordsCounts":[`+
			`{"searchKeyword":"bakery near me","insightsValue":{"value":"120"}},`+
			`{"searchKeyword":"cake","insightsValue":{"threshold":"15"}}],"nextPageToken":"keywords-two"}`), nil
	case "keywords-two":
		return respond(r, http.StatusOK, "application/json", `{"searchKeywordsCounts":[`+
			`{"searchKeyword":"bread","insightsValue":{"value":"0"}}],"nextPageToken":"keywords-three"}`), nil
	case "keywords-three":
		return respond(r, http.StatusOK, "application/json", `{"searchKeywordsCounts":[`+
			`{"searchKeyword":"`+strings.Repeat("k", maxLineRunes+10)+`","insightsValue":{"threshold":"15"}}]}`), nil
	}
	return respond(r, http.StatusBadRequest, "application/json", errorObject(400, "INVALID_ARGUMENT", "")), nil
}

func TestSearchKeywordsReadThreePagesWithThresholds(t *testing.T) {
	env := newPerformanceEnvironment(t)
	env.fake.answer = keywordsPage
	args := `{` + locArg + `,"start_month":"2026-01","end_month":"2026-03","page_size":`
	var terms []map[string]any
	cursor := ""
	for page := 1; page <= 3; page++ {
		call := args + strconv.Itoa(100-page)
		if cursor != "" {
			call += `,"cursor":"` + cursor + `"`
		}
		out, err := env.invoke(t, performanceKeywordsID, "all", call+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, entry := range out["searchKeywordsCounts"].([]any) {
			terms = append(terms, entry.(map[string]any))
		}
		cursor, _ = out["next_cursor"].(string)
		if (page < 3) != (cursor != "") {
			t.Fatalf("page %d: next_cursor = %q", page, cursor)
		}
	}
	if len(terms) != 4 {
		t.Fatalf("terms = %v", terms)
	}
	if v := terms[0]["insightsValue"].(map[string]any); v["value"] != 120.0 || v["threshold"] != nil {
		t.Errorf("counted term = %v", terms[0])
	}
	// A threshold stays a threshold: it is never shown as a value.
	if v := terms[1]["insightsValue"].(map[string]any); v["threshold"] != 15.0 || v["value"] != nil {
		t.Errorf("term below the threshold = %v", terms[1])
	}
	if v := terms[2]["insightsValue"].(map[string]any); v["value"] != 0.0 {
		t.Errorf("term with a count of zero = %v", terms[2])
	}
	if long := terms[3]["searchKeyword"].(string); len([]rune(long)) != maxLineRunes+1 || !strings.HasSuffix(long, "…") {
		t.Errorf("a long search term has %d characters", len([]rune(long)))
	}
	calls, _ := env.fake.calls()
	for i, token := range []string{"", "keywords-two", "keywords-three"} {
		values := calls[i].values()
		if calls[i].path != "/v1/"+boundLocation+"/searchkeywords/impressions/monthly" ||
			values.Get("monthlyRange.startMonth.year") != "2026" || values.Get("monthlyRange.startMonth.month") != "1" ||
			values.Get("monthlyRange.endMonth.month") != "3" || values.Get("monthlyRange.endMonth.day") != "" ||
			values.Get("pageSize") != strconv.Itoa(99-i) || values.Get("pageToken") != token {
			t.Errorf("request %d = %+v", i+1, calls[i])
		}
	}
	// A cursor belongs to its location and its range.
	first, _ := env.invoke(t, performanceKeywordsID, "all", args+`50}`)
	for _, other := range []string{`{"location":"` + otherLocation + `","start_month":"2026-01","end_month":"2026-03"`,
		`{` + locArg + `,"start_month":"2026-01","end_month":"2026-04"`} {
		if _, err := env.invoke(t, performanceKeywordsID, "all", other+`,"cursor":"`+first["next_cursor"].(string)+`"}`); !isInvalidRequest(err) {
			t.Errorf("cursor of another call: error = %v", err)
		}
	}
}

func TestSearchKeywordsAnswerAnEmptyListAndRefuseTooMany(t *testing.T) {
	env := newPerformanceEnvironment(t)
	out, err := env.invoke(t, performanceKeywordsID, "all", `{`+locArg+`,"start_month":"2026-01","end_month":"2026-01"}`)
	if err != nil || len(out) != 0 {
		t.Errorf("result = %v, %v", out, err)
	}
	many := make([]string, maxKeywordsPage+1)
	for i := range many {
		many[i] = `{"searchKeyword":"k"}`
	}
	env.fake.answer = reply(http.StatusOK, `{"searchKeywordsCounts":[`+strings.Join(many, ",")+`]}`)
	_, err = env.invoke(t, performanceKeywordsID, "all", `{`+locArg+`,"start_month":"2026-01","end_month":"2026-01"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("more than a page: error = %v", err)
	}
}

func TestUnusableCountsAreAnInvalidResponse(t *testing.T) {
	env := newPerformanceEnvironment(t)
	env.fake.answer = reply(http.StatusOK, `{"timeSeries":{"datedValues":[{"value":"`+detailCanary+`"}]}}`)
	_, err := env.invoke(t, performanceGetDailyID, "all", `{`+locArg+`,"daily_metric":"CALL_CLICKS",`+rangeQ1+`}`)
	if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("error = %v", err)
	}
}

func TestPerformanceTargets(t *testing.T) {
	tools := map[string]string{
		performanceGetDailyID:   `,"daily_metric":"CALL_CLICKS",` + rangeQ1,
		performanceFetchDailyID: `,"daily_metrics":["CALL_CLICKS"],` + rangeQ1,
		performanceKeywordsID:   `,"start_month":"2026-01","end_month":"2026-02"`,
	}
	t.Run("location target reads its location only", func(t *testing.T) {
		env := newPerformanceEnvironment(t)
		for tool, rest := range tools {
			if _, err := env.invoke(t, tool, "location", `{`+rest[1:]+`}`); err != nil {
				t.Errorf("%s: %v", tool, err)
			}
			if _, err := env.invoke(t, tool, "location", `{"location":"`+otherLocation+`"`+rest+`}`); !isInvalidRequest(err) {
				t.Errorf("%s other location: error = %v", tool, err)
			}
		}
		calls, _ := env.fake.calls()
		if len(calls) != len(tools) {
			t.Fatalf("%d requests, want one per tool and none for a refused location", len(calls))
		}
		for _, call := range calls {
			if !strings.HasPrefix(call.path, "/v1/"+boundLocation) || strings.Contains(call.path, "accounts") {
				t.Errorf("request = %+v", call)
			}
		}
	})
	t.Run("account target proves the location", func(t *testing.T) {
		for tool, rest := range tools {
			env := newPerformanceEnvironment(t)
			env.fake.answer = func(r *http.Request, _ int) (*http.Response, error) {
				if r.URL.Host == "mybusinessbusinessinformation.googleapis.com" {
					return respond(r, http.StatusOK, "application/json", `{"locations":[{"name":"`+boundLocation+`"}]}`), nil
				}
				return respond(r, http.StatusOK, "application/json", `{}`), nil
			}
			if _, err := env.invoke(t, tool, "account", `{`+locArg+rest+`}`); err != nil {
				t.Fatalf("%s: %v", tool, err)
			}
			calls, _ := env.fake.calls()
			if len(calls) != 2 || calls[0].host != "mybusinessbusinessinformation.googleapis.com" ||
				calls[0].path != "/v1/"+boundAccount+"/locations" || calls[0].values().Get("readMask") != "name" ||
				calls[1].host != "businessprofileperformance.googleapis.com" {
				t.Fatalf("%s: requests = %+v, want the proof first and then the read", tool, calls)
			}
			// A location outside the account is refused after the proof, without being read or named.
			_, err := env.invoke(t, tool, "account", `{"location":"`+otherLocation+`"`+rest+`}`)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherLocation) {
				t.Errorf("%s outside the account: error = %v", tool, err)
			}
			after, _ := env.fake.calls()
			for _, call := range after[2:] {
				if strings.Contains(call.path, otherLocation) && call.host == "businessprofileperformance.googleapis.com" {
					t.Errorf("%s: a location outside the account was read: %+v", tool, call)
				}
			}
		}
	})
	t.Run("account target needs a location", func(t *testing.T) {
		env := newPerformanceEnvironment(t)
		for tool, rest := range tools {
			if _, err := env.invoke(t, tool, "account", `{`+rest[1:]+`}`); !isInvalidRequest(err) {
				t.Errorf("%s: error = %v, want invalid-request", tool, err)
			}
			for _, bad := range []string{"locations/-", "locations/02", boundAccount, "accounts/1/locations/2"} {
				if _, err := env.invoke(t, tool, "all", `{"location":"`+bad+`"`+rest+`}`); !isInvalidRequest(err) {
					t.Errorf("%s %s: error = %v, want invalid-request", tool, bad, err)
				}
			}
		}
		if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
			t.Errorf("%d requests and %d grants, want none", len(calls), grants)
		}
	})
}

func TestPerformanceFailuresMapToClassesWithoutProviderText(t *testing.T) {
	for name, c := range map[string]struct {
		answer func(*http.Request, int) (*http.Response, error)
		class  provider.Class
	}{
		"403": {reply(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")), provider.ClassPermission},
		"404": {reply(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound},
		"429": {reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")),
			provider.ClassRateLimited},
		"429 with a quota of zero": {reply(http.StatusTooManyRequests, zeroQuotaObject()), ClassAccessNotApproved},
		"400":                      {reply(http.StatusBadRequest, errorObject(400, "INVALID_ARGUMENT", "")), provider.ClassInvalidResponse},
		"not JSON": {func(r *http.Request, _ int) (*http.Response, error) {
			return respond(r, http.StatusOK, "text/html", "<p>"+detailCanary+"</p>"), nil
		}, provider.ClassInvalidResponse},
		"timeout": {func(*http.Request, int) (*http.Response, error) {
			return nil, fmt.Errorf("%s: %w", detailCanary, context.DeadlineExceeded)
		}, provider.ClassTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			env := newPerformanceEnvironment(t)
			env.fake.answer = c.answer
			_, err := env.invoke(t, performanceGetDailyID, "all", `{`+locArg+`,"daily_metric":"CALL_CLICKS",`+rangeQ1+`}`)
			if classOf(err) != c.class {
				t.Fatalf("error = %v (class %q), want %q", err, classOf(err), c.class)
			}
			if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), accessPrefix) {
				t.Errorf("the message carries provider text or a token: %v", err)
			}
			var failed *provider.Error
			if errors.As(err, &failed) && failed.Class == provider.ClassNotFound &&
				!strings.Contains(failed.Message, "location") {
				t.Errorf("not-found message = %q", failed.Message)
			}
		})
	}
}

func TestPerformanceToolsNeedOnlyTheProfileScope(t *testing.T) {
	for _, id := range []string{performanceGetDailyID, performanceFetchDailyID, performanceKeywordsID} {
		if !reflect.DeepEqual(toolScopes[id], []string{scopeManage}) {
			t.Errorf("%s scopes = %v", id, toolScopes[id])
		}
	}
	if performanceRevision != "20261116" {
		t.Errorf("revision = %s", performanceRevision)
	}
}
