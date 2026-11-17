package googleanalytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const taskDefinition = `{"dimensions":[{"name":"pagePath"}],"metrics":[{"name":"sessions"}],` +
	`"date_ranges":[{"start_date":"90daysAgo","end_date":"yesterday"}],"dimension_filter":{"filter":{"field_name":` +
	`"country","string_filter":{"value":"Switzerland"}}},"order_bys":[{"desc":true,"metric":{"metric_name":"sessions"}}],` +
	`"offset":10,"limit":100000,"metric_aggregations":["TOTAL"],"currency_code":"CHF","keep_empty_rows":true,` +
	`"sampling_level":"LOW"}`

const taskArguments = `{"property":"properties/200","report_definition":` + taskDefinition + `}`

const taskOperationJSON = `{"name":"properties/200/reportTasks/500","metadata":{"@type":"type.googleapis.com/` +
	`google.analytics.data.v1alpha.ReportTaskMetadata"}}`

// taskJSON is a task in the given state.
func taskJSON(id int, state string) string {
	meta := ""
	switch state {
	case "ACTIVE":
		meta = `,"taskRowCount":1200,"totalRowCount":5000,"creationQuotaTokensCharged":9`
	case "FAILED":
		meta = `,"errorMessage":"quota exhausted","creationQuotaTokensCharged":4`
	}
	return fmt.Sprintf(`{"name":"properties/200/reportTasks/%d","reportDefinition":{"dimensions":[{"name":"pagePath"}],`+
		`"metrics":[{"name":"sessions"}],"dateRanges":[{"startDate":"90daysAgo","endDate":"yesterday"}],"limit":"100000",`+
		`"samplingLevel":"LOW"},"reportMetadata":{"state":"%s","beginCreatingTime":"2026-11-16T10:00:00Z"%s}}`, id, state, meta)
}

func TestReportTasksCreateNeedsTheConfirmationAndSendsOneRequest(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, taskOperationJSON)})
	if _, err := env.call(reportTasksCreateID, "xwrite", taskArguments); err == nil {
		t.Fatal("create without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(reportTasksCreateID, "xwrite", taskArguments)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
	r := requests[0]
	if r.method != http.MethodPost || r.host != "analyticsdata.googleapis.com" ||
		r.path != "/v1alpha/properties/200/reportTasks" || r.rawQuery != "" || r.contentType != "application/json" ||
		r.auth != "Bearer "+accessPrefix+"1" {
		t.Fatalf("request = %+v", r)
	}
	var body map[string]map[string]any
	if json.Unmarshal([]byte(r.body), &body) != nil || len(body) != 1 {
		t.Fatalf("body = %s", r.body)
	}
	def := body["reportDefinition"]
	for key, want := range map[string]any{"offset": "10", "limit": "100000", "currencyCode": "CHF",
		"keepEmptyRows": true, "samplingLevel": "LOW"} {
		if def[key] != want {
			t.Errorf("%s = %v (%T), want %v", key, def[key], def[key], want)
		}
	}
	for _, key := range []string{"dimensions", "metrics", "dateRanges", "dimensionFilter", "orderBys", "metricAggregations"} {
		if def[key] == nil {
			t.Errorf("body lacks %s: %s", key, r.body)
		}
	}
	if result != `{"done":false,"name":"properties/200/reportTasks/500"}` {
		t.Errorf("result = %s", result)
	}
}

func TestReportTasksCreateReturnsAnOperationThatAlreadyHoldsAnOutcome(t *testing.T) {
	freshQuotas(t)
	answer := `{"name":"properties/200/reportTasks/500","done":true,"response":` + taskJSON(500, "CREATING") + `}`
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.confirmed(reportTasksCreateID, "xwrite", taskArguments)
	var out taskOperation
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || !out.Done || out.Response == nil ||
		out.Response.ReportMetadata == nil || out.Response.ReportMetadata.State != "CREATING" {
		t.Errorf("result = %s, %v", result, err)
	}
	failed := `{"name":"properties/200/reportTasks/500","done":true,"error":{"code":8,"message":"quota","details":[{"x":1}]}}`
	env = exportEnv(t, &fakeGoogle{api: always(http.StatusOK, failed)})
	result, err = env.confirmed(reportTasksCreateID, "xwrite", taskArguments)
	if err != nil || !strings.Contains(result, `"error":{"code":8,"message":"quota"}`) || strings.Contains(result, "details") {
		t.Errorf("result = %s, %v", result, err)
	}
}

func TestReportTasksCreateChecksTheArgumentsAndTargetsBeforeAnyIO(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, taskOperationJSON)})
	const dates = `"date_ranges":[{"start_date":"7daysAgo","end_date":"today"}]`
	for name, c := range map[string]struct{ connection, arguments string }{
		"foreign property":         {"xproperty", `{"property":"properties/201","report_definition":{` + dates + `}}`},
		"account without property": {"xaccount", `{"report_definition":{` + dates + `}}`},
		"no definition":            {"xwrite", `{"property":"properties/200"}`},
		"no dates":                 {"xwrite", `{"property":"properties/200","report_definition":{"metrics":[{"name":"sessions"}]}}`},
		"dates and cohort":         {"xwrite", `{"property":"properties/200","report_definition":{` + dates + `,"cohort_spec":{"cohorts":[{"dimension":"firstSessionDate","date_range":{"start_date":"7daysAgo","end_date":"today"}}],"cohorts_range":{"end_offset":2,"granularity":"DAILY"}}}}`},
		"unknown member":           {"xwrite", `{"property":"properties/200","report_definition":{` + dates + `,"comparisons":[{"name":"x","comparison":"comparisons/1"}]}}`},
		"bad sampling":             {"xwrite", `{"property":"properties/200","report_definition":{` + dates + `,"sampling_level":"HUGE"}}`},
		"bad filter":               {"xwrite", `{"property":"properties/200","report_definition":{` + dates + `,"dimension_filter":{}}}`},
		"limit zero":               {"xwrite", `{"property":"properties/200","report_definition":{` + dates + `,"limit":0}}`},
		"unknown argument":         {"xwrite", `{"property":"properties/200","report_definition":{` + dates + `},"webhook":{}}`},
	} {
		if _, err := env.confirmed(reportTasksCreateID, c.connection, c.arguments); !refusedInvalid(err) ||
			strings.Contains(err.Error(), "201") {
			t.Errorf("%s: %v", name, err)
		}
	}
	env.noIO(t)
	if _, err := env.confirmed(reportTasksCreateID, "xproperty", `{"report_definition":{`+dates+`}}`); err != nil {
		t.Errorf("bound default: %v", err)
	}
}

func TestReportTasksCreateProvesTheAccountOfAPropertyUnderABoundAccount(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if r.URL.Host == "analyticsadmin.googleapis.com" {
			return respond(r, http.StatusOK, "application/json", `{"name":"properties/200","parent":"accounts/100"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", taskOperationJSON), nil
	}})
	if _, err := env.confirmed(reportTasksCreateID, "xaccount", taskArguments); err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 2 || requests[0].host != "analyticsadmin.googleapis.com" || requests[1].method != http.MethodPost {
		t.Errorf("requests = %+v", requests)
	}
	env = exportEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/200","parent":"accounts/900"}`)})
	if _, err := env.confirmed(reportTasksCreateID, "xaccount", taskArguments); !refusedInvalid(err) {
		t.Errorf("foreign account: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d", len(requests))
	}
}

func TestReportTasksCreateIsCountedWithTheCoreCategory(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, ":runReport") {
			return respond(r, http.StatusOK, "application/json", reportPage(1, 1, minTokensLeft-1)), nil
		}
		return respond(r, http.StatusOK, "application/json", taskOperationJSON), nil
	}})
	if _, err := env.call(reportsRunID, "xwrite", `{"property":"properties/200",`+minimalReport[1:]); err != nil {
		t.Fatal(err)
	}
	before, _ := env.fake.seen()
	_, err := env.confirmed(reportTasksCreateID, "xwrite", taskArguments)
	after, _ := env.fake.seen()
	if classOf(err) != provider.ClassRateLimited || len(after) != len(before) {
		t.Fatalf("err = %v, requests %d -> %d", err, len(before), len(after))
	}
	if got := len(quotas.running); got != 0 {
		t.Errorf("slots still held: %d", got)
	}
	// get, list and query are no gated requests: they run although the remainder is low and the slots are taken.
	freshQuotas(t)
	env2 := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, taskJSON(500, "ACTIVE"))})
	for i := 0; i < maxConcurrentReports; i++ {
		if _, err := quotas.enter("test", "properties/200", env2.open(t, "read").identity); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := env2.call(reportTasksGetID, "read", `{"name":"properties/200/reportTasks/500"}`); err != nil {
		t.Errorf("get with all slots taken: %v", err)
	}
}

func TestReportTasksGetReturnsEveryStateAndMember(t *testing.T) {
	for state, wants := range map[string][]string{
		"CREATING": {`"state":"CREATING"`},
		"ACTIVE": {`"state":"ACTIVE"`, `"taskRowCount":1200`, `"totalRowCount":5000`,
			`"creationQuotaTokensCharged":9`},
		"FAILED": {`"state":"FAILED"`, `"errorMessage":"quota exhausted"`, `"creationQuotaTokensCharged":4`},
	} {
		env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, taskJSON(500, state))})
		result, err := env.call(reportTasksGetID, "read", `{"name":"properties/200/reportTasks/500"}`)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range append(wants, `"name":"properties/200/reportTasks/500"`,
			`"beginCreatingTime":"2026-11-16T10:00:00Z"`, `"dimensions":[{"name":"pagePath"}]`,
			`"samplingLevel":"LOW"`, `"limit":"100000"`) {
			if !strings.Contains(result, want) {
				t.Errorf("%s: result lacks %s: %s", state, want, result)
			}
		}
		requests, _ := env.fake.seen()
		if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].host != "analyticsdata.googleapis.com" ||
			requests[0].path != "/v1alpha/properties/200/reportTasks/500" || requests[0].body != "" {
			t.Errorf("%s: requests = %+v", state, requests)
		}
	}
}

func TestReportTasksGetAndQueryCheckTheNameBeforeAnyIO(t *testing.T) {
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, taskJSON(500, "ACTIVE"))})
	for _, tool := range []string{reportTasksGetID, reportTasksQueryID} {
		for name, c := range map[string]struct{ connection, name string }{
			"foreign property": {"property", "properties/999/reportTasks/500"},
			"bad form":         {"read", "properties/200/audienceExports/500"},
			"no id":            {"read", "properties/200/reportTasks/"},
			"traversal":        {"read", "properties/200/reportTasks/../../accounts/1"},
			"query string":     {"read", "properties/200/reportTasks/5?alt=media"},
		} {
			if _, err := env.call(tool, c.connection, `{"name":"`+c.name+`"}`); !refusedInvalid(err) ||
				strings.Contains(err.Error(), "999") {
				t.Errorf("%s %s: %v", tool, name, err)
			}
		}
	}
	env.noIO(t)
}

func TestReportTasksGetRefusesAnAnswerForAnotherTask(t *testing.T) {
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, taskJSON(501, "ACTIVE"))})
	_, err := env.call(reportTasksGetID, "read", `{"name":"properties/200/reportTasks/500"}`)
	if classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func tasksPageJSON(token string, ids ...int) string {
	items := make([]string, len(ids))
	for i, id := range ids {
		items[i] = taskJSON(id, "ACTIVE")
	}
	next := ""
	if token != "" {
		next = `,"nextPageToken":"` + token + `"`
	}
	return `{"reportTasks":[` + strings.Join(items, ",") + `]` + next + `}`
}

func TestReportTasksListPagesThreeTimesThroughTheBoundCursor(t *testing.T) {
	pages := map[string]string{"": tasksPageJSON("tok-2", 1, 2), "tok-2": tasksPageJSON("tok-3", 3, 4),
		"tok-3": tasksPageJSON("", 5)}
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[r.URL.Query().Get("pageToken")]), nil
	}})
	type listed struct {
		ReportTasks []reportTask `json:"reportTasks"`
		NextCursor  string       `json:"next_cursor"`
		HasMore     bool         `json:"has_more"`
	}
	page := func(cursor string) listed {
		t.Helper()
		arguments := `{"property":"properties/200","page_size":2`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(reportTasksListID, "read", arguments+`}`)
		var out listed
		if err != nil || json.Unmarshal([]byte(result), &out) != nil {
			t.Fatalf("result = %s, %v", result, err)
		}
		return out
	}
	one := page("")
	two := page(one.NextCursor)
	three := page(two.NextCursor)
	if len(one.ReportTasks) != 2 || !one.HasMore || len(two.ReportTasks) != 2 || !two.HasMore ||
		len(three.ReportTasks) != 1 || three.HasMore || three.NextCursor != "" || strings.Contains(one.NextCursor, "tok-2") {
		t.Fatalf("pages = %+v %+v %+v", one, two, three)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 3 || requests[0].rawQuery != "pageSize=2" || requests[1].rawQuery != "pageSize=2&pageToken=tok-2" ||
		requests[2].rawQuery != "pageSize=2&pageToken=tok-3" || requests[0].path != "/v1alpha/properties/200/reportTasks" ||
		requests[0].method != http.MethodGet {
		t.Errorf("requests = %+v", requests)
	}
	for name, c := range map[string]struct{ connection, property, cursor string }{
		"altered":        {"read", "properties/200", flipLast(one.NextCursor)},
		"garbage":        {"read", "properties/200", "not-a-cursor"},
		"other property": {"read", "properties/201", one.NextCursor},
		"other tool":     {"read", "properties/200", mustCursorOf(t, env, audienceListID)},
	} {
		if _, err := env.call(reportTasksListID, c.connection, `{"property":"`+c.property+`","cursor":"`+c.cursor+`"}`); !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := env.call(reportTasksListID, "property", `{"cursor":"`+one.NextCursor+`"}`); !refusedInvalid(err) {
		t.Errorf("other connection: %v", err)
	}
	if after, _ := env.fake.seen(); len(after) != 3 {
		t.Errorf("requests = %d", len(after))
	}
	if _, err := env.call(reportTasksListID, "read", `{"property":"properties/200","page_size":1001}`); !refusedInvalid(err) {
		t.Errorf("page size 1001: %v", err)
	}
}

func TestReportTasksListDropsEntriesOfAnotherPropertyAndBoundsDefinitions(t *testing.T) {
	long := strings.Repeat("q", 500)
	hostile := `{"name":"properties/200/reportTasks/7","reportDefinition":{"dimensionFilter":{"filter":{"fieldName":"x",` +
		`"stringFilter":{"value":"` + long + `"}}},"bad key!":1,"dimensions":[` +
		strings.TrimSuffix(strings.Repeat(`{"name":"d"},`, 250), ",") + `]}}`
	answer := `{"reportTasks":[` + taskJSON(1, "ACTIVE") + `,{"name":"properties/999/reportTasks/5"},` +
		`{"name":"accounts/1"},{"state":"ACTIVE"},` + hostile + `]}`
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.call(reportTasksListID, "read", `{"property":"properties/200"}`)
	var out struct {
		ReportTasks []map[string]any `json:"reportTasks"`
		Truncated   []string         `json:"truncated"`
	}
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.ReportTasks) != 2 ||
		strings.Join(out.Truncated, ",") != "reportTasks,reportTasks.reportDefinition" || strings.Contains(result, "999") ||
		strings.Contains(result, "bad key") || strings.Contains(result, long) {
		t.Errorf("result = %.600s, %v", result, err)
	}
	def, _ := out.ReportTasks[1]["reportDefinition"].(map[string]any)
	if dims, _ := def["dimensions"].([]any); len(dims) != maxDefinitionList {
		t.Errorf("dimensions = %d", len(dims))
	}
}

func TestReportTasksGetMarksACutDefinition(t *testing.T) {
	long := strings.Repeat("q", 500)
	answer := `{"name":"properties/200/reportTasks/7","reportDefinition":{"dimensionFilter":{"filter":{"fieldName":"x",` +
		`"stringFilter":{"value":"` + long + `"}}}}}`
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.call(reportTasksGetID, "read", `{"name":"properties/200/reportTasks/7"}`)
	if err != nil || !strings.Contains(result, `"truncated":["reportDefinition"]`) || strings.Contains(result, long) {
		t.Errorf("result = %.300s, %v", result, err)
	}
}

func taskQueryPage(rows, rowCount int) string {
	list := make([]string, rows)
	for i := range list {
		list[i] = fmt.Sprintf(`{"dimensionValues":[{"value":"page-%d"}],"metricValues":[{"value":"%d"}]}`, i, i+1)
	}
	return fmt.Sprintf(`{"dimensionHeaders":[{"name":"pagePath"}],"metricHeaders":[{"name":"sessions","type":"TYPE_INTEGER"}],`+
		`"rows":[%s],"rowCount":%d,"metadata":{"currencyCode":"CHF","timeZone":"Europe/Zurich","section":"SECTION_REPORT"}}`,
		strings.Join(list, ","), rowCount)
}

func TestReportTasksQueryPagesThroughTheBoundCursor(t *testing.T) {
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, taskQueryPage(2, 5))})
	type queriedTask struct {
		Rows       []json.RawMessage `json:"rows"`
		RowCount   int               `json:"rowCount"`
		Offset     int               `json:"offset"`
		NextCursor string            `json:"next_cursor"`
		HasMore    bool              `json:"has_more"`
		Metadata   struct {
			Section      string `json:"section"`
			CurrencyCode string `json:"currencyCode"`
		} `json:"metadata"`
	}
	const name = "properties/200/reportTasks/500"
	page := func(cursor string) queriedTask {
		t.Helper()
		arguments := `{"name":"` + name + `","limit":2`
		if cursor != "" {
			arguments += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(reportTasksQueryID, "read", arguments+`}`)
		var out queriedTask
		if err != nil || json.Unmarshal([]byte(result), &out) != nil {
			t.Fatalf("result = %s, %v", result, err)
		}
		return out
	}
	one := page("")
	two := page(one.NextCursor)
	three := page(two.NextCursor)
	if !one.HasMore || one.Offset != 0 || len(one.Rows) != 2 || !two.HasMore || two.Offset != 2 || three.HasMore ||
		three.NextCursor != "" || three.Offset != 4 || one.RowCount != 5 || one.Metadata.Section != "SECTION_REPORT" ||
		one.Metadata.CurrencyCode != "CHF" || one.NextCursor == "2" {
		t.Fatalf("pages = %+v %+v %+v", one, two, three)
	}
	requests, _ := env.fake.seen()
	for i, want := range []string{"", "2", "4"} {
		body := bodyOf(t, requests[i])
		got, _ := body["offset"].(string)
		r := requests[i]
		if got != want || body["limit"] != "2" || r.method != http.MethodPost || r.path != "/v1alpha/"+name+":query" {
			t.Errorf("request %d = %+v", i, r)
		}
	}
	// A manipulated or foreign cursor never reaches Google.
	for label, c := range map[string]struct{ connection, name, cursor string }{
		"altered":          {"read", name, flipLast(one.NextCursor)},
		"garbage":          {"read", name, "not-a-cursor"},
		"other task":       {"read", "properties/200/reportTasks/501", one.NextCursor},
		"other property":   {"read", "properties/201/reportTasks/500", one.NextCursor},
		"other connection": {"property", name, one.NextCursor},
	} {
		if _, err := env.call(reportTasksQueryID, c.connection, `{"name":"`+c.name+`","cursor":"`+c.cursor+`"}`); !refusedInvalid(err) {
			t.Errorf("%s: %v", label, err)
		}
	}
	if after, _ := env.fake.seen(); len(after) != 3 {
		t.Errorf("requests = %d", len(after))
	}
}

func TestReportTasksQueryOfATaskThatIsNotActiveIsRefused(t *testing.T) {
	env := exportEnv(t, &fakeGoogle{api: always(http.StatusBadRequest, errorObject(400, "FAILED_PRECONDITION", ""))})
	_, err := env.call(reportTasksQueryID, "read", `{"name":"properties/200/reportTasks/500"}`)
	if classOf(err) != provider.ClassProviderError || leaks(err.Error()) != "" {
		t.Errorf("err = %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d, want exactly one", len(requests))
	}
}

func TestReportTasksMapFailuresWithoutProviderTextAndNeverRepeat(t *testing.T) {
	tools := []struct {
		id, connection, arguments string
		change                    bool
	}{
		{reportTasksCreateID, "xwrite", taskArguments, true},
		{reportTasksGetID, "read", `{"name":"properties/200/reportTasks/500"}`, false},
		{reportTasksListID, "read", `{"property":"properties/200"}`, false},
		{reportTasksQueryID, "read", `{"name":"properties/200/reportTasks/500"}`, false},
	}
	for _, c := range []struct {
		name   string
		api    func(int, *http.Request) (*http.Response, error)
		class  provider.Class
		unsure bool
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "X")), provider.ClassPermission, false},
		{"service disabled", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")),
			provider.ClassPermission, false},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, false},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited, false},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, true},
		{"503", always(http.StatusServiceUnavailable, errorObject(503, "UNAVAILABLE", "")), provider.ClassUnreachable, true},
		{"not json", always(http.StatusOK, `{"name":`), provider.ClassInvalidResponse, true},
		{"unreadable body", func(_ int, r *http.Request) (*http.Response, error) {
			resp := respond(r, http.StatusOK, "application/json", "")
			resp.Body = failingBody{}
			return resp, nil
		}, provider.ClassInvalidResponse, true},
		{"timeout", func(int, *http.Request) (*http.Response, error) { return nil, timeoutError{} }, provider.ClassTimeout, true},
	} {
		for _, tool := range tools {
			t.Run(c.name+" "+tool.id, func(t *testing.T) {
				freshQuotas(t)
				env := exportEnv(t, &fakeGoogle{api: c.api})
				var err error
				if tool.change {
					_, err = env.confirmed(tool.id, tool.connection, tool.arguments)
				} else {
					_, err = env.call(tool.id, tool.connection, tool.arguments)
				}
				if classOf(err) != c.class || leaks(err.Error()) != "" {
					t.Fatalf("error = %v", err)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != (c.unsure && tool.change) {
					t.Errorf("unknown outcome reported = %t: %v", got, err)
				}
				if requests, _ := env.fake.seen(); len(requests) != 1 {
					t.Errorf("requests = %d, want exactly one", len(requests))
				}
			})
		}
	}
	for _, answer := range []string{`{}`, `{"name":"properties/201/reportTasks/4"}`, `{"name":"operations/9"}`} {
		env := exportEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
		_, err := env.confirmed(reportTasksCreateID, "xwrite", taskArguments)
		if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("answer %s: %v", answer, err)
		}
	}
}

func TestReportTasksRateLimitHoldsTheNextRequestWithoutRetryAfter(t *testing.T) {
	freshQuotas(t)
	env := exportEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", taskOperationJSON), nil
	}})
	if _, err := env.confirmed(reportTasksCreateID, "xwrite", taskArguments); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("429: %v", err)
	}
	if _, err := env.confirmed(reportTasksCreateID, "xwrite", taskArguments); err != nil {
		t.Fatal(err)
	}
	if len(*env.waits) == 0 {
		t.Error("the next request did not wait")
	}
}

func TestReportTasksToolsDeclareRiskScopeAndProfile(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	ids := []string{reportTasksCreateID, reportTasksGetID, reportTasksListID, reportTasksQueryID}
	for _, id := range ids {
		if got := metadata.Token.Scopes[id]; len(got) != 1 || got[0] != scopeReadOnly {
			t.Errorf("%s scopes = %v", id, got)
		}
	}
	for _, d := range reg.Provider(Provider) {
		switch d.ID {
		case reportTasksCreateID:
			if string(d.Risk.Effect) != "create" || d.Risk.Confirmation != "required" || !d.Risk.OpenWorld ||
				d.Risk.Idempotency != "non_idempotent" || d.RequiresToolAllowList || reg.ConfirmationSummary(d.ID) == nil {
				t.Errorf("%s = %+v", d.ID, d.Risk)
			}
		case reportTasksGetID, reportTasksListID, reportTasksQueryID:
			if d.Risk != reportsRisk {
				t.Errorf("%s = %+v", d.ID, d.Risk)
			}
		}
		if strings.HasPrefix(d.ID, Provider+".report-tasks.") && !strings.Contains(d.Description, "ALPHA") {
			t.Errorf("%s lacks the alpha note", d.ID)
		}
		if strings.HasPrefix(d.ID, Provider+".audience-exports.") && strings.Contains(d.Description, "ALPHA") {
			t.Errorf("%s carries the alpha note of another API version", d.ID)
		}
	}
	for _, profile := range metadata.Profiles {
		for _, id := range ids {
			holds := false
			for _, tool := range profile.Tools {
				holds = holds || tool == id
			}
			if want := id != reportTasksCreateID || profile.ID == "write"; holds != want {
				t.Errorf("profile %s holds %s: %t", profile.ID, id, holds)
			}
		}
	}
	summary := reg.ConfirmationSummary(reportTasksCreateID)(json.RawMessage(taskArguments))
	for _, want := range []string{"report task", "properties/200", "1 dimension", "1 metric", "Core quota"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q lacks %q", summary, want)
		}
	}
}
