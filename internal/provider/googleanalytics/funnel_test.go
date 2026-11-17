package googleanalytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const funnelQuotaJSON = `"propertyQuota":{"tokensPerDay":{"consumed":9,"remaining":199991},` +
	`"tokensPerHour":{"consumed":9,"remaining":%d},"concurrentRequests":{"consumed":1,"remaining":9},` +
	`"serverErrorsPerProjectPerHour":{"remaining":10},"potentiallyThresholdedRequestsPerHour":{"remaining":120},` +
	`"tokensPerProjectPerHour":{"consumed":9,"remaining":13991}}`

// funnelAnswerJSON answers a funnel report: a table with one row per step and a visualization.
func funnelAnswerJSON(steps, hourLeft int) string {
	rows := make([]string, steps)
	for i := range rows {
		rows[i] = fmt.Sprintf(`{"dimensionValues":[{"value":"%d. step-%d"}],"metricValues":[{"value":"%d"},{"value":"0.5"}]}`,
			i+1, i+1, 100-i*10)
	}
	list := strings.Join(rows, ",")
	return `{"funnelTable":{"dimensionHeaders":[{"name":"funnelStepName"}],"metricHeaders":[{"name":"activeUsers",` +
		`"type":"TYPE_INTEGER"},{"name":"funnelStepCompletionRate","type":"TYPE_FLOAT"}],"rows":[` + list + `],` +
		`"metadata":{"samplingMetadatas":[{"samplesReadCount":"1000","samplingSpaceSize":"5000"}]}},` +
		`"funnelVisualization":{"dimensionHeaders":[{"name":"funnelStepName"}],"metricHeaders":[{"name":"activeUsers",` +
		`"type":"TYPE_INTEGER"}],"rows":[` + list + `]},` + fmt.Sprintf(funnelQuotaJSON, hourLeft) +
		`,"kind":"analyticsData#runFunnelReport"}`
}

const threeSteps = `{"is_open_funnel":false,"steps":[` +
	`{"name":"View","filter_expression":{"funnel_event_filter":{"event_name":"page_view"}}},` +
	`{"name":"Cart","is_directly_followed_by":true,"within_duration_from_prior_step":"600s","filter_expression":` +
	`{"and_group":{"expressions":[{"funnel_event_filter":{"event_name":"add_to_cart","funnel_parameter_filter_expression":` +
	`{"funnel_parameter_filter":{"event_parameter_name":"value","numeric_filter":{"operation":"GREATER_THAN",` +
	`"value":{"int64_value":5}}}}}},{"funnel_field_filter":{"field_name":"country","in_list_filter":{"values":["CH","DE"]}}}]}}},` +
	`{"name":"Buy","filter_expression":{"not_expression":{"funnel_event_filter":{"event_name":"refund"}}}}]}`

const funnelBase = `{"property":"properties/200","date_ranges":[{"start_date":"28daysAgo","end_date":"yesterday"}],` +
	`"funnel":` + threeSteps

func TestFunnelRunSendsThreeStepsWithBreakdownToTheAlphaPath(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, funnelAnswerJSON(3, 39000))})
	arguments := funnelBase + `,"funnel_breakdown":{"breakdown_dimension":{"name":"deviceCategory"},"limit":3},` +
		`"funnel_next_action":{"next_action_dimension":{"name":"eventName"},"limit":2},` +
		`"funnel_visualization_type":"TRENDED_FUNNEL","dimension_filter":{"filter":{"field_name":"country",` +
		`"string_filter":{"value":"CH","match_type":"EXACT"}}},"segments":[{"name":"Buyers","user_segment":{` +
		`"user_inclusion_criteria":{"and_condition_groups":[{"condition_scoping":"USER_CRITERIA_ACROSS_ALL_SESSIONS",` +
		`"segment_filter_expression":{"segment_event_filter":{"event_name":"purchase","segment_parameter_filter_expression":` +
		`{"segment_parameter_filter":{"event_parameter_name":"event_count","numeric_filter":{"operation":"GREATER_THAN",` +
		`"value":{"int64_value":1}},"filter_scoping":{"in_any_n_day_period":3}}}}}}]}}}],"limit":50}`
	result, err := env.call(funnelRunID, "read", arguments)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
	r := requests[0]
	if r.method != http.MethodPost || r.host != "analyticsdata.googleapis.com" ||
		r.path != "/v1alpha/properties/200:runFunnelReport" || r.rawQuery != "" || r.contentType != "application/json" {
		t.Fatalf("request = %+v", r)
	}
	body := bodyOf(t, r)
	for key, want := range map[string]string{"limit": `"50"`, "returnPropertyQuota": `true`,
		"funnelVisualizationType": `"TRENDED_FUNNEL"`,
		"dateRanges":              `[{"endDate":"yesterday","startDate":"28daysAgo"}]`,
		"funnelBreakdown":         `{"breakdownDimension":{"name":"deviceCategory"},"limit":"3"}`,
		"funnelNextAction":        `{"limit":"2","nextActionDimension":{"name":"eventName"}}`,
		"dimensionFilter":         `{"filter":{"fieldName":"country","stringFilter":{"matchType":"EXACT","value":"CH"}}}`,
		"funnel": `{"isOpenFunnel":false,"steps":[{"filterExpression":{"funnelEventFilter":{"eventName":"page_view"}},"name":"View"},` +
			`{"filterExpression":{"andGroup":{"expressions":[{"funnelEventFilter":{"eventName":"add_to_cart",` +
			`"funnelParameterFilterExpression":{"funnelParameterFilter":{"eventParameterName":"value","numericFilter":` +
			`{"operation":"GREATER_THAN","value":{"int64Value":"5"}}}}}},{"funnelFieldFilter":{"fieldName":"country",` +
			`"inListFilter":{"values":["CH","DE"]}}}]}},"isDirectlyFollowedBy":true,"name":"Cart",` +
			`"withinDurationFromPriorStep":"600s"},{"filterExpression":{"notExpression":{"funnelEventFilter":` +
			`{"eventName":"refund"}}},"name":"Buy"}]}`,
		"segments": `[{"name":"Buyers","userSegment":{"userInclusionCriteria":{"andConditionGroups":[{"conditionScoping":` +
			`"USER_CRITERIA_ACROSS_ALL_SESSIONS","segmentFilterExpression":{"segmentEventFilter":{"eventName":"purchase",` +
			`"segmentParameterFilterExpression":{"segmentParameterFilter":{"eventParameterName":"event_count",` +
			`"filterScoping":{"inAnyNDayPeriod":"3"},"numericFilter":{"operation":"GREATER_THAN","value":{"int64Value":"1"}}}}}}}]}}}]`,
	} {
		got, _ := json.Marshal(body[key])
		if string(got) != want {
			t.Errorf("body[%s] = %s, want %s", key, got, want)
		}
	}
	if len(body) != 9 {
		t.Errorf("body = %s", r.body)
	}
	var out struct {
		FunnelTable         funnelSubReport `json:"funnelTable"`
		FunnelVisualization funnelSubReport `json:"funnelVisualization"`
		PropertyQuota       map[string]any  `json:"propertyQuota"`
		Kind                string          `json:"kind"`
		Limit               int             `json:"limit"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.FunnelTable.Rows) != 3 ||
		len(out.FunnelVisualization.Rows) != 3 || len(out.PropertyQuota) != 6 || out.Kind != "analyticsData#runFunnelReport" ||
		out.FunnelTable.Metadata == nil || len(out.FunnelTable.Metadata.SamplingMetadatas) != 1 || out.Limit != 50 ||
		out.FunnelTable.MetricHeaders[1].Name != "funnelStepCompletionRate" {
		t.Fatalf("result = %s, %v", result, err)
	}
}

func TestFunnelRunSendsOnlyTheFunnelByDefault(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, funnelAnswerJSON(1, 39000))})
	if _, err := env.call(funnelRunID, "read", `{"property":"properties/200","funnel":{"steps":[{"filter_expression":`+
		`{"funnel_event_filter":{"event_name":"purchase"}}}]}}`); err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if got := requests[0].body; got != `{"funnel":{"steps":[{"filterExpression":{"funnelEventFilter":{"eventName":"purchase"}}}]},`+
		`"limit":"10000","returnPropertyQuota":true}` {
		t.Errorf("body = %s", got)
	}
}

func TestFunnelRunRefusesBadArgumentsBeforeAnyIO(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{})
	step := `{"filter_expression":{"funnel_event_filter":{"event_name":"purchase"}}}`
	event := `{"funnel_event_filter":{"event_name":"purchase"}}`
	field := `{"funnel_field_filter":{"field_name":"country","empty_filter":{}}}`
	segment := func(kind string) string {
		return `"segments":[{` + kind + `}]`
	}
	for name, arguments := range map[string]string{
		"no funnel":             `{"property":"properties/200"}`,
		"funnel without steps":  `{"property":"properties/200","funnel":{}}`,
		"empty steps":           `{"property":"properties/200","funnel":{"steps":[]}}`,
		"eleven steps":          `{"property":"properties/200","funnel":{"steps":[` + strings.TrimSuffix(strings.Repeat(step+",", 11), ",") + `]}}`,
		"step without a filter": `{"property":"properties/200","funnel":{"steps":[{"name":"Only a name"}]}}`,
		"two alternatives":      `{"property":"properties/200","funnel":{"steps":[{"filter_expression":{"funnel_event_filter":{"event_name":"a"},"funnel_field_filter":{"field_name":"b","in_list_filter":{"values":["c"]}}}}]}}`,
		"empty expression":      `{"property":"properties/200","funnel":{"steps":[{"filter_expression":{}}]}}`,
		"field filter kind":     `{"property":"properties/200","funnel":{"steps":[{"filter_expression":` + field + `}]}}`,
		"parameter name":        `{"property":"properties/200","funnel":{"steps":[{"filter_expression":{"funnel_event_filter":{"event_name":"a","funnel_parameter_filter_expression":{"funnel_parameter_filter":{"string_filter":{"value":"x"}}}}}}]}}`,
		"both parameter names":  `{"property":"properties/200","funnel":{"steps":[{"filter_expression":{"funnel_event_filter":{"event_name":"a","funnel_parameter_filter_expression":{"funnel_parameter_filter":{"event_parameter_name":"p","item_parameter_name":"q","string_filter":{"value":"x"}}}}}}]}}`,
		"too deep":              `{"property":"properties/200","funnel":{"steps":[{"filter_expression":{"not_expression":{"not_expression":` + event + `}}}]}}`,
		"bad duration":          `{"property":"properties/200","funnel":{"steps":[{"within_duration_from_prior_step":"5 minutes","filter_expression":` + event + `}]}}`,
		"unknown member":        `{"property":"properties/200","funnel":{"steps":[` + step + `],"path":"/x"}}`,
		"breakdown limit":       `{"property":"properties/200","funnel":{"steps":[` + step + `]},"funnel_breakdown":{"breakdown_dimension":{"name":"d"},"limit":16}}`,
		"next action limit":     `{"property":"properties/200","funnel":{"steps":[` + step + `]},"funnel_next_action":{"next_action_dimension":{"name":"eventName"},"limit":6}}`,
		"breakdown no name":     `{"property":"properties/200","funnel":{"steps":[` + step + `]},"funnel_breakdown":{"limit":3}}`,
		"visualization type":    `{"property":"properties/200","funnel":{"steps":[` + step + `]},"funnel_visualization_type":"FUNNEL_VISUALIZATION_TYPE_UNSPECIFIED"}`,
		"no segment kind":       `{"property":"properties/200","funnel":{"steps":[` + step + `]},` + segment(`"name":"x"`) + `}`,
		"two segment kinds":     `{"property":"properties/200","funnel":{"steps":[` + step + `]},` + segment(`"session_segment":{},"event_segment":{}`) + `}`,
		"five segments":         `{"property":"properties/200","funnel":{"steps":[` + step + `]},"segments":[` + strings.TrimSuffix(strings.Repeat(`{"event_segment":{}},`, 5), ",") + `]}`,
		"segment expression":    `{"property":"properties/200","funnel":{"steps":[` + step + `]},` + segment(`"event_segment":{"event_inclusion_criteria":{"and_condition_groups":[{"segment_filter_expression":{}}]}}`) + `}`,
		"segment two leaves":    `{"property":"properties/200","funnel":{"steps":[` + step + `]},` + segment(`"event_segment":{"event_inclusion_criteria":{"and_condition_groups":[{"segment_filter_expression":{"segment_filter":{"field_name":"a","in_list_filter":{"values":["b"]}},"segment_event_filter":{"event_name":"c"}}}]}}`) + `}`,
		"segment scoping":       `{"property":"properties/200","funnel":{"steps":[` + step + `]},` + segment(`"event_segment":{"event_inclusion_criteria":{"and_condition_groups":[{"condition_scoping":"USER_CRITERIA_ACROSS_ALL_SESSIONS","segment_filter_expression":{"segment_event_filter":{"event_name":"c"}}}]}}`) + `}`,
		"limit zero":            `{"property":"properties/200","funnel":{"steps":[` + step + `]},"limit":0}`,
		"bad property":          `{"property":"accounts/1","funnel":{"steps":[` + step + `]}}`,
		"dimension filter kind": `{"property":"properties/200","funnel":{"steps":[` + step + `]},"dimension_filter":{"filter":{"field_name":"country"}}}`,
	} {
		if _, err := env.call(funnelRunID, "read", arguments); !refusedInvalid(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if requests, grants := env.fake.seen(); len(requests) != 0 || grants != 0 || env.reads.Load() != 0 {
		t.Errorf("requests %d, grants %d, secret reads %d", len(requests), grants, env.reads.Load())
	}
}

func TestFunnelRunHasItsOwnQuotaRules(t *testing.T) {
	clock := freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "runFunnelReport") {
			return respond(r, http.StatusOK, "application/json", funnelAnswerJSON(1, minTokensLeft-1)), nil
		}
		return respond(r, http.StatusOK, "application/json", reportPage(1, 1, 39000)), nil
	}})
	arguments := funnelBase + `}`
	if _, err := env.call(funnelRunID, "read", arguments); err != nil {
		t.Fatal(err)
	}
	before, _ := env.fake.seen()
	_, err := env.call(funnelRunID, "read", arguments)
	after, _ := env.fake.seen()
	if classOf(err) != provider.ClassRateLimited || !strings.Contains(err.Error(), "tokens per hour left: 99") ||
		len(after) != len(before) {
		t.Fatalf("err = %v, requests %d -> %d", err, len(before), len(after))
	}
	// The Core category is separate: a core report still runs, and a low Core remainder leaves funnels alone.
	if _, err := env.call(reportsRunID, "read", `{"property":"properties/200",`+minimalReport[1:]); err != nil {
		t.Errorf("core report after a low funnel quota: %v", err)
	}
	if _, err := env.call(funnelRunID, "read", `{"property":"properties/201","funnel":`+threeSteps+`}`); err != nil {
		t.Errorf("other property: %v", err)
	}
	*clock = clock.Add(quotaMemory)
	if _, err := env.call(funnelRunID, "read", arguments); err != nil {
		t.Errorf("after the memory expired: %v", err)
	}
	freshQuotas(t)
	env = newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "runFunnelReport") {
			return respond(r, http.StatusOK, "application/json", funnelAnswerJSON(1, 39000)), nil
		}
		return respond(r, http.StatusOK, "application/json", reportPage(1, 1, minTokensLeft-1)), nil
	}})
	if _, err := env.call(reportsRunID, "read", `{"property":"properties/200",`+minimalReport[1:]); err != nil {
		t.Fatal(err)
	}
	if _, err := env.call(funnelRunID, "read", arguments); err != nil {
		t.Errorf("funnel after a low Core remainder: %v", err)
	}
}

func TestFunnelRunLimitsParallelFunnelsPerProperty(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, funnelAnswerJSON(1, 39000))})
	identity := env.open(t, "read").identity
	for i := 0; i < maxConcurrentReports; i++ {
		if _, err := funnelQuotas.enter("test", "properties/201", identity); err != nil {
			t.Fatal(err)
		}
	}
	_, err := env.call(funnelRunID, "read", `{"property":"properties/201","funnel":`+threeSteps+`}`)
	if classOf(err) != provider.ClassRateLimited || !strings.Contains(err.Error(), "10 reports") {
		t.Errorf("eleventh funnel: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 0 {
		t.Errorf("requests = %+v", requests)
	}
	// A Core report of the same property is not held back by funnel slots.
	if _, err := env.call(reportsRunID, "read", `{"property":"properties/201",`+minimalReport[1:]); err != nil {
		t.Errorf("core report: %v", err)
	}
}

func TestFunnelRunBoundsRowsAndUntrustedValues(t *testing.T) {
	freshQuotas(t)
	long := strings.Repeat("x", 500)
	rows := make([]string, 5)
	for i := range rows {
		rows[i] = `{"dimensionValues":[{"value":"` + long + `"}],"metricValues":[{"value":"1"}]}`
	}
	answer := `{"funnelTable":{"dimensionHeaders":[{"name":"funnelStepName"}],"metricHeaders":[{"name":"activeUsers"}],"rows":[` +
		strings.Join(rows, ",") + `]},"kind":"analyticsData#runFunnelReport"}`
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
	result, err := env.call(funnelRunID, "read", `{"property":"properties/200","limit":2,"funnel":`+threeSteps+`}`)
	var out struct {
		FunnelTable funnelSubReport `json:"funnelTable"`
		Truncated   []string        `json:"truncated"`
	}
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.FunnelTable.Rows) != 2 ||
		len(out.Truncated) != 1 || out.Truncated[0] != "funnelTable.rows" ||
		len([]rune(string(out.FunnelTable.Rows[0].DimensionValues[0].Value))) != maxTextRunes+1 ||
		strings.Contains(result, `"funnelVisualization"`) || strings.Contains(result, "next_cursor") {
		t.Errorf("result = %.300s, %v", result, err)
	}
	requests, _ := env.fake.seen()
	if bodyOf(t, requests[0])["limit"] != "2" {
		t.Errorf("body = %s", requests[0].body)
	}
	// A limit above a page is capped and says so.
	result, err = env.call(funnelRunID, "read", `{"property":"properties/200","limit":250000,"funnel":`+threeSteps+`}`)
	var capped struct {
		Limit       int  `json:"limit"`
		LimitCapped bool `json:"limit_capped"`
	}
	requests, _ = env.fake.seen()
	if err != nil || json.Unmarshal([]byte(result), &capped) != nil || capped.Limit != 10000 || !capped.LimitCapped ||
		bodyOf(t, requests[1])["limit"] != "10000" {
		t.Errorf("result = %.200s, %v", result, err)
	}
}

func TestFunnelRunReturnsThePropertyQuotaUnlessSwitchedOffExplicitly(t *testing.T) {
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, funnelAnswerJSON(1, 39000))})
	if _, err := env.call(funnelRunID, "read", `{"property":"properties/200","return_property_quota":false,"funnel":`+
		threeSteps+`}`); err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if bodyOf(t, requests[0])["returnPropertyQuota"] != false {
		t.Errorf("body = %s", requests[0].body)
	}
}

func TestFunnelRunBindingChecksTheTargetBeforeAnyIO(t *testing.T) {
	freshQuotas(t)
	fake := &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if r.URL.Host == "analyticsadmin.googleapis.com" {
			parent := "accounts/100"
			if strings.Contains(r.URL.Path, "901") {
				parent = "accounts/900"
			}
			return respond(r, http.StatusOK, "application/json",
				`{"name":"`+strings.TrimPrefix(r.URL.Path, "/v1beta/")+`","parent":"`+parent+`"}`), nil
		}
		return respond(r, http.StatusOK, "application/json", funnelAnswerJSON(1, 39000)), nil
	}}
	for _, c := range []struct {
		connection, property string
		requests             int
		refused              bool
	}{
		{"property", "properties/200", 1, false},
		{"property", "", 1, false},
		{"property", "properties/201", 0, true},
		{"account", "properties/201", 2, false},
		{"account", "properties/901", 1, true},
		{"account", "", 0, true},
		{"read", "properties/777", 1, false},
	} {
		env := newEnv(t, fake)
		before, _ := fake.seen()
		arguments := `{"funnel":` + threeSteps + `}`
		if c.property != "" {
			arguments = `{"property":"` + c.property + `","funnel":` + threeSteps + `}`
		}
		result, err := env.call(funnelRunID, c.connection, arguments)
		after, _ := fake.seen()
		if len(after)-len(before) != c.requests || (err != nil) != c.refused ||
			(c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901"))) {
			t.Errorf("%s %q: requests %d, error %v", c.connection, c.property, len(after)-len(before), err)
		}
	}
}

func TestFunnelRunMapsFailuresWithoutProviderText(t *testing.T) {
	arguments := `{"property":"properties/200","funnel":` + threeSteps + `}`
	for name, c := range map[string]struct {
		status int
		answer string
		class  provider.Class
	}{
		"403": {http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "X"), provider.ClassPermission},
		"404": {http.StatusNotFound, errorObject(404, "NOT_FOUND", "X"), provider.ClassNotFound},
		"429": {http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "X"), provider.ClassRateLimited},
	} {
		freshQuotas(t)
		env := newEnv(t, &fakeGoogle{api: always(c.status, c.answer)})
		_, err := env.call(funnelRunID, "read", arguments)
		requests, _ := env.fake.seen()
		if classOf(err) != c.class || leaks(err.Error()) != "" || len(requests) != 1 {
			t.Errorf("%s: %v, requests %d", name, err, len(requests))
		}
	}
	freshQuotas(t)
	env := newEnv(t, &fakeGoogle{api: always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"))})
	if _, err := env.call(funnelRunID, "read", arguments); classOf(err) != provider.ClassPermission ||
		!strings.Contains(err.Error(), "Data API is not enabled") {
		t.Errorf("service disabled: %v", err)
	}
}

func TestAlphaNoteOnlyOnTheFunnelAndUntrustedNoteOnAll(t *testing.T) {
	if !strings.Contains(funnelRun.Description, "ALPHA") {
		t.Error("the funnel description lacks the alpha note")
	}
	for _, d := range []string{funnelRun.Description, audienceCreate.Description, audienceGet.Description,
		audienceList.Description} {
		if !strings.Contains(d, "untrusted") {
			t.Errorf("description lacks the untrusted note: %.80s", d)
		}
	}
	for _, d := range []string{audienceCreate.Description, audienceGet.Description, audienceList.Description} {
		if strings.Contains(d, "ALPHA") || strings.Contains(d, "v1alpha") {
			t.Errorf("an audience export description names the alpha version: %.80s", d)
		}
	}
	if len(funnelInput) > 100<<10 {
		t.Errorf("funnel input schema is %d bytes", len(funnelInput))
	}
}
