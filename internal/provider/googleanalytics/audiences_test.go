package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var audienceTools = []string{audiencesListID, audiencesGetID, audiencesCreateID, audiencesUpdateID, audiencesArchiveID}

// audiencesEnv adds connections that list the five audience tools, with the bindings of the stream tests.
func audiencesEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: audienceTools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	audienceOneJSON = `{"name":"properties/200/audiences/7001","displayName":"Purchasers","description":"Users who bought",` +
		`"membershipDurationDays":30,"adsPersonalizationEnabled":true,"createTime":"2026-11-01T10:00:00Z",` +
		`"eventTrigger":{"eventName":"joined_purchasers","logCondition":"AUDIENCE_JOINED"},` +
		`"exclusionDurationMode":"EXCLUDE_TEMPORARILY","filterClauses":[` +
		`{"clauseType":"INCLUDE","simpleFilter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS","filterExpression":` +
		`{"andGroup":{"filterExpressions":[{"orGroup":{"filterExpressions":[` +
		`{"eventFilter":{"eventName":"purchase","eventParameterFilterExpression":{"andGroup":{"filterExpressions":[` +
		`{"orGroup":{"filterExpressions":[{"dimensionOrMetricFilter":{"fieldName":"currency","stringFilter":` +
		`{"value":"EUR","matchType":"EXACT","caseSensitive":true}}}]}}]}}}},` +
		`{"dimensionOrMetricFilter":{"fieldName":"country","inListFilter":{"values":["DE","AT"],"caseSensitive":false},` +
		`"inAnyNDayPeriod":30,"atAnyPointInTime":false}},` +
		`{"notExpression":{"dimensionOrMetricFilter":{"fieldName":"eventCount","numericFilter":{"operation":"LESS_THAN",` +
		`"value":{"int64Value":"5"}}}}},` +
		`{"dimensionOrMetricFilter":{"fieldName":"purchaseRevenue","betweenFilter":{"fromValue":{"doubleValue":1.5},` +
		`"toValue":{"int64Value":"100"}}}}]}}]}}}},` +
		`{"clauseType":"EXCLUDE","sequenceFilter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS",` +
		`"sequenceMaximumDuration":"3600s","sequenceSteps":[{"scope":"AUDIENCE_FILTER_SCOPE_WITHIN_SAME_SESSION",` +
		`"immediatelyFollows":true,"constraintDuration":"600s","filterExpression":{"andGroup":{"filterExpressions":[` +
		`{"orGroup":{"filterExpressions":[{"eventFilter":{"eventName":"refund"}}]}}]}}}]}}],"extra":1}`
	audienceTwoJSON = `{"name":"properties/200/audiences/7002","displayName":"Visitors","description":"All",` +
		`"membershipDurationDays":7}`
	audienceThreeJSON = `{"name":"properties/200/audiences/7003","displayName":"Others","description":"Rest",` +
		`"membershipDurationDays":1}`
)

type audiencesOut struct {
	Audiences  []audience `json:"audiences"`
	NextCursor string     `json:"next_cursor"`
	HasMore    bool       `json:"has_more"`
}

func TestAudiencesListPagesThroughThreePages(t *testing.T) {
	pages := []string{
		`{"audiences":[` + audienceOneJSON + `],"nextPageToken":"t2"}`,
		`{"audiences":[` + audienceTwoJSON + `],"nextPageToken":"t3"}`,
		`{"audiences":[` + audienceThreeJSON + `]}`,
	}
	env := audiencesEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(audiencesListID, "sfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		var out audiencesOut
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Audiences) != 1 ||
			out.HasMore != (i < 2) || strings.Contains(result, "extra") {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.Audiences[0].Name))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"properties/200/audiences/7001", "properties/200/audiences/7002",
		"properties/200/audiences/7003"}) || len(requests) != 3 || requests[0].path != "/v1alpha/properties/200/audiences" ||
		requests[0].rawQuery != "pageSize=1" || !strings.Contains(requests[1].rawQuery, "pageToken=t2") ||
		!strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	if _, err := env.call(audiencesListID, "sfree", `{"property":"properties/201","cursor":"`+cursor+`"}`); err == nil && cursor != "" {
		t.Error("a cursor of another property was accepted")
	}
}

func TestAudiencesGetReturnsEveryDocumentedField(t *testing.T) {
	env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, audienceOneJSON)})
	result, err := env.call(audiencesGetID, "sfree", `{"name":"properties/200/audiences/7001"}`)
	var got audience
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || strings.Contains(result, "extra") {
		t.Fatalf("%v %s", err, result)
	}
	requests, _ := env.fake.seen()
	if got.DisplayName != "Purchasers" || got.Description != "Users who bought" || got.MembershipDurationDays == nil ||
		*got.MembershipDurationDays != 30 || got.AdsPersonalizationEnabled == nil || !*got.AdsPersonalizationEnabled ||
		got.EventTrigger == nil || got.EventTrigger.EventName != "joined_purchasers" ||
		got.EventTrigger.LogCondition != "AUDIENCE_JOINED" || got.ExclusionDurationMode != "EXCLUDE_TEMPORARILY" ||
		got.CreateTime != "2026-11-01T10:00:00Z" || len(got.FilterClauses) != 2 ||
		len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/audiences/7001" {
		t.Errorf("audience = %+v, requests %+v", got, requests)
	}
	simple := got.FilterClauses[0].SimpleFilter
	or := simple.FilterExpression.AndGroup.FilterExpressions[0].OrGroup.FilterExpressions
	event := or[0].EventFilter
	param := event.EventParameterFilterExpression.AndGroup.FilterExpressions[0].OrGroup.FilterExpressions[0]
	numeric := or[2].NotExpression.DimensionOrMetricFilter.NumericFilter
	between := or[3].DimensionOrMetricFilter.BetweenFilter
	list := or[1].DimensionOrMetricFilter
	if got.FilterClauses[0].ClauseType != "INCLUDE" || simple.Scope != "AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS" ||
		event.EventName != "purchase" || param.DimensionOrMetricFilter.StringFilter.Value != "EUR" ||
		param.DimensionOrMetricFilter.StringFilter.MatchType != "EXACT" ||
		!*param.DimensionOrMetricFilter.StringFilter.CaseSensitive || len(list.InListFilter.Values) != 2 ||
		*list.InAnyNDayPeriod != 30 || *list.AtAnyPointInTime || numeric.Operation != "LESS_THAN" ||
		numeric.Value.Int64Value != "5" || *between.FromValue.DoubleValue != 1.5 || between.ToValue.Int64Value != "100" {
		t.Errorf("clause = %+v", got.FilterClauses[0])
	}
	seq := got.FilterClauses[1].SequenceFilter
	if got.FilterClauses[1].ClauseType != "EXCLUDE" || seq.SequenceMaximumDuration != "3600s" ||
		len(seq.SequenceSteps) != 1 || !*seq.SequenceSteps[0].ImmediatelyFollows ||
		seq.SequenceSteps[0].ConstraintDuration != "600s" ||
		seq.SequenceSteps[0].Scope != "AUDIENCE_FILTER_SCOPE_WITHIN_SAME_SESSION" {
		t.Errorf("sequence = %+v", seq)
	}
}

func TestAudiencesAnswerIsBoundedAndChecked(t *testing.T) {
	// A chain nested far beyond the bound, with a filter on every level so that the cut leaves no empty shell,
	// a long value list and a long text.
	chain := `{"dimensionOrMetricFilter":{"fieldName":"x"}}`
	for i := 0; i < 20; i++ {
		chain = `{"dimensionOrMetricFilter":{"fieldName":"x"},"notExpression":` + chain + `}`
	}
	values := strings.TrimSuffix(strings.Repeat(`"v",`, 150), ",")
	env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"audiences":[`+
		`{"name":"properties/200/audiences/1","displayName":"`+strings.Repeat("n", 400)+`","description":"`+
		strings.Repeat("d", 5000)+`","filterClauses":[{"clauseType":"INCLUDE","simpleFilter":{"filterExpression":`+chain+`}},`+
		`{"clauseType":"EXCLUDE","simpleFilter":{"filterExpression":{"dimensionOrMetricFilter":{"inListFilter":{"values":[`+
		values+`,"`+strings.Repeat("w", 400)+`"]}}}}}]},`+
		`{"name":"properties/999/audiences/2"},{"name":"properties/200/audiences/bad"},{"name":"nonsense"}]}`)})
	result, err := env.call(audiencesListID, "sfree", `{"property":"properties/200"}`)
	var out audiencesOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Audiences) != 1 {
		t.Fatalf("result %v, %v", len(result), err)
	}
	a := out.Audiences[0]
	if len([]rune(string(a.DisplayName))) != maxTextRunes+1 || len([]rune(string(a.Description))) != maxDescriptionRunes+1 {
		t.Errorf("texts = %d %d", len(a.DisplayName), len(a.Description))
	}
	depth, e := 0, a.FilterClauses[0].SimpleFilter.FilterExpression
	for ; e != nil; e = e.NotExpression {
		depth++
	}
	if depth != maxAudFilterDepth {
		t.Errorf("expression depth = %d, want %d", depth, maxAudFilterDepth)
	}
	if n := len(a.FilterClauses[1].SimpleFilter.FilterExpression.DimensionOrMetricFilter.InListFilter.Values); n != maxAudInListValues {
		t.Errorf("list values = %d, want %d", n, maxAudInListValues)
	}
	env = audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, audienceOneJSON)})
	if _, err := env.call(audiencesGetID, "sfree", `{"name":"properties/200/audiences/7009"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

const (
	simpleClause = `{"clause_type":"INCLUDE","simple_filter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS",` +
		`"filter_expression":{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"country",` +
		`"string_filter":{"value":"DE","match_type":"EXACT"}}}]}]}}}`
	sequenceClause = `{"clause_type":"INCLUDE","sequence_filter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS",` +
		`"sequence_maximum_duration":"3600s","sequence_steps":[` +
		`{"scope":"AUDIENCE_FILTER_SCOPE_WITHIN_SAME_SESSION","filter_expression":{"and_group":[{"or_group":[` +
		`{"event_filter":{"event_name":"view_item"}}]}]}},` +
		`{"scope":"AUDIENCE_FILTER_SCOPE_WITHIN_SAME_SESSION","immediately_follows":true,"constraint_duration":"600s",` +
		`"filter_expression":{"and_group":[{"or_group":[{"event_filter":{"event_name":"purchase",` +
		`"event_parameter_filter_expression":{"and_group":[{"or_group":[{"not_expression":{"dimension_or_metric_filter":` +
		`{"field_name":"currency","in_list_filter":{"values":["EUR","USD"]}}}}]}]}}}]}]}}]}}`
)

func audienceCreateArgs(clauses string) string {
	return `{"property":"properties/200","display_name":"Purchasers","description":"Users who bought",` +
		`"membership_duration_days":30,"filter_clauses":[` + clauses + `]}`
}

func TestAudiencesCreateWithASimpleClauseNeedsConfirmation(t *testing.T) {
	env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, audienceOneJSON)})
	args := audienceCreateArgs(simpleClause)
	if _, err := env.call(audiencesCreateID, "sfree", args); err == nil {
		t.Fatal("create without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(audiencesCreateID, "sfree", args)
	requests, _ := env.fake.seen()
	const want = `{"description":"Users who bought","displayName":"Purchasers","filterClauses":[{"clauseType":"INCLUDE",` +
		`"simpleFilter":{"filterExpression":{"andGroup":{"filterExpressions":[{"orGroup":{"filterExpressions":[` +
		`{"dimensionOrMetricFilter":{"fieldName":"country","stringFilter":{"matchType":"EXACT","value":"DE"}}}]}}]}},` +
		`"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS"}}],"membershipDurationDays":30}`
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != "/v1alpha/properties/200/audiences" || requests[0].rawQuery != "" ||
		requests[0].body != want || !strings.Contains(result, `"name":"properties/200/audiences/7001"`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func TestAudiencesCreateWithASequenceClauseAndOptionalMembers(t *testing.T) {
	env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, audienceOneJSON)})
	args := strings.Replace(audienceCreateArgs(sequenceClause), `"filter_clauses"`,
		`"event_trigger":{"event_name":"joined_buyers","log_condition":"AUDIENCE_JOINED"},`+
			`"exclusion_duration_mode":"EXCLUDE_PERMANENTLY","filter_clauses"`, 1)
	if _, err := env.confirmed(audiencesCreateID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	var body map[string]any
	if len(requests) != 1 || json.Unmarshal([]byte(requests[0].body), &body) != nil {
		t.Fatalf("requests %+v", requests)
	}
	trigger, _ := body["eventTrigger"].(map[string]any)
	clauses, _ := body["filterClauses"].([]any)
	if trigger["eventName"] != "joined_buyers" || trigger["logCondition"] != "AUDIENCE_JOINED" ||
		body["exclusionDurationMode"] != "EXCLUDE_PERMANENTLY" || len(clauses) != 1 {
		t.Fatalf("body = %s", requests[0].body)
	}
	for _, part := range []string{`"sequenceFilter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS",` +
		`"sequenceMaximumDuration":"3600s","sequenceSteps":[`, `"immediatelyFollows":true`, `"constraintDuration":"600s"`,
		`"eventParameterFilterExpression":{"andGroup":{"filterExpressions":[{"orGroup":{"filterExpressions":[` +
			`{"notExpression":{"dimensionOrMetricFilter":{"fieldName":"currency","inListFilter":{"values":["EUR","USD"]}}}}]}}]}}`} {
		if !strings.Contains(requests[0].body, part) {
			t.Errorf("body lacks %s: %s", part, requests[0].body)
		}
	}
}

// deepChain nests negations n levels deep.
func deepChain(n int) string {
	expr := `{"dimension_or_metric_filter":{"field_name":"x","string_filter":{"value":"a","match_type":"EXACT"}}}`
	for i := 0; i < n; i++ {
		expr = `{"not_expression":` + expr + `}`
	}
	return expr
}

func TestAudiencesCreateRefusesBadInputBeforeAnyIO(t *testing.T) {
	env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, audienceOneJSON)})
	base := map[string]any{"property": "properties/200", "display_name": "P", "description": "D",
		"membership_duration_days": 30, "filter_clauses": json.RawMessage(`[` + simpleClause + `]`)}
	with := func(key string, value any) string {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		if value == nil {
			delete(m, key)
		} else {
			m[key] = value
		}
		out, _ := json.Marshal(m)
		return string(out)
	}
	clause := func(expr string) json.RawMessage {
		return json.RawMessage(`[{"clause_type":"INCLUDE","simple_filter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS",` +
			`"filter_expression":` + expr + `}}]`)
	}
	leaf := `{"dimension_or_metric_filter":{"field_name":"x","string_filter":{"value":"a","match_type":"EXACT"}}}`
	many := make([]string, 51)
	for i := range many {
		many[i] = `{"or_group":[` + leaf + `]}`
	}
	values := make([]string, 101)
	for i := range values {
		values[i] = `"v"`
	}
	for name, args := range map[string]string{
		"no property":         with("property", nil),
		"no display name":     with("display_name", nil),
		"no description":      with("description", nil),
		"no duration":         with("membership_duration_days", nil),
		"no clauses":          with("filter_clauses", nil),
		"empty clauses":       with("filter_clauses", json.RawMessage(`[]`)),
		"foreign kind":        with("property", "accounts/200"),
		"blank name":          with("display_name", "   "),
		"long name":           with("display_name", strings.Repeat("n", 301)),
		"control name":        with("display_name", "a\x01b"),
		"blank description":   with("description", "  "),
		"long description":    with("description", strings.Repeat("d", 4097)),
		"duration zero":       with("membership_duration_days", 0),
		"duration too long":   with("membership_duration_days", 541),
		"unknown mode":        with("exclusion_duration_mode", "FOREVER"),
		"unknown field":       with("calculated", 1),
		"bad trigger event":   with("event_trigger", map[string]string{"event_name": "1x", "log_condition": "AUDIENCE_JOINED"}),
		"bad trigger cond":    with("event_trigger", map[string]string{"event_name": "x", "log_condition": "NEVER"}),
		"too deep (schema)":   with("filter_clauses", clause(`{"and_group":[{"or_group":[`+deepChain(12)+`]}]}`)),
		"clause type":         with("filter_clauses", json.RawMessage(`[{"clause_type":"MAYBE"}]`)),
		"both filters":        with("filter_clauses", json.RawMessage(`[{"clause_type":"INCLUDE","simple_filter":{},"sequence_filter":{}}]`)),
		"top is or group":     with("filter_clauses", clause(`{"or_group":[`+leaf+`]}`)),
		"and holds a leaf":    with("filter_clauses", clause(`{"and_group":[`+leaf+`]}`)),
		"not holds a not":     with("filter_clauses", clause(`{"and_group":[{"or_group":[`+deepChain(2)+`]}]}`)),
		"param holds event":   with("filter_clauses", clause(`{"and_group":[{"or_group":[{"event_filter":{"event_name":"e","event_parameter_filter_expression":{"and_group":[{"or_group":[{"event_filter":{"event_name":"f"}}]}]}}}]}]}`)),
		"two members":         with("filter_clauses", clause(`{"and_group":[{"or_group":[`+leaf+`]}],"or_group":[]}`)),
		"too many group":      with("filter_clauses", clause(`{"and_group":[`+strings.Join(many, ",")+`]}`)),
		"no scope":            with("filter_clauses", json.RawMessage(`[{"clause_type":"INCLUDE","simple_filter":{"filter_expression":{"and_group":[{"or_group":[`+leaf+`]}]}}}]`)),
		"bad scope":           with("filter_clauses", json.RawMessage(`[{"clause_type":"INCLUDE","simple_filter":{"scope":"ALL","filter_expression":{"and_group":[{"or_group":[`+leaf+`]}]}}}]`)),
		"two filter kinds":    with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"x","string_filter":{"value":"a","match_type":"EXACT"},"in_list_filter":{"values":["a"]}}}]}]}`)),
		"no filter kind":      with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"x"}}]}]}`)),
		"bad field":           with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"a b","string_filter":{"value":"a","match_type":"EXACT"}}}]}]}`)),
		"bad match":           with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"x","string_filter":{"value":"a","match_type":"LIKE"}}}]}]}`)),
		"long value":          with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"x","string_filter":{"value":"`+strings.Repeat("v", 301)+`","match_type":"EXACT"}}}]}]}`)),
		"too many list items": with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"x","in_list_filter":{"values":[`+strings.Join(values, ",")+`]}}}]}]}`)),
		"bad day period":      with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"x","in_any_n_day_period":61,"string_filter":{"value":"a","match_type":"EXACT"}}}]}]}`)),
		"bad numeric op":      with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"x","numeric_filter":{"operation":"NEAR","value":{"int64_value":"1"}}}}]}]}`)),
		"bad int64":           with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"x","numeric_filter":{"operation":"EQUAL","value":{"int64_value":"1.5"}}}}]}]}`)),
		"both numbers":        with("filter_clauses", clause(`{"and_group":[{"or_group":[{"dimension_or_metric_filter":{"field_name":"x","numeric_filter":{"operation":"EQUAL","value":{"int64_value":"1","double_value":1}}}}]}]}`)),
		"bad event name":      with("filter_clauses", clause(`{"and_group":[{"or_group":[{"event_filter":{"event_name":"a b"}}]}]}`)),
		"bad duration":        with("filter_clauses", json.RawMessage(`[{"clause_type":"INCLUDE","sequence_filter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS","sequence_maximum_duration":"1h","sequence_steps":[]}}]`)),
		"no steps":            with("filter_clauses", json.RawMessage(`[{"clause_type":"INCLUDE","sequence_filter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS","sequence_steps":[]}}]`)),
	} {
		if _, err := env.confirmed(audiencesCreateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
	for _, ok := range []string{with("membership_duration_days", 540), with("display_name", strings.Repeat("n", 300)),
		with("filter_clauses", json.RawMessage(`[`+sequenceClause+`,`+simpleClause+`]`))} {
		if _, err := env.confirmed(audiencesCreateID, "sfree", ok); refusedInvalid(err) {
			t.Errorf("%s: refused as %v", ok, err)
		}
	}
}

// TestAudienceFilterClausesAreBoundedWithoutTheSchema calls the handler directly, so the bounds hold for a
// caller that skips the schema.
func TestAudienceFilterClausesAreBoundedWithoutTheSchema(t *testing.T) {
	env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, audienceOneJSON)})
	resolved := env.resolved(t, "sfree")
	handle := func(clauses string) error {
		_, err := handleAudiencesCreate(context.Background(), resolved, env.secrets(), env.red,
			json.RawMessage(audienceCreateArgs(clauses)))
		return err
	}
	wrap := func(expr string) string {
		return `{"clause_type":"INCLUDE","simple_filter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS","filter_expression":` +
			`{"and_group":[{"or_group":[` + expr + `]}]}}}`
	}
	leaf := `{"dimension_or_metric_filter":{"field_name":"x","string_filter":{"value":"a","match_type":"EXACT"}}}`
	groups := make([]string, 50)
	for i := range groups {
		groups[i] = `{"or_group":[` + leaf + `]}`
	}
	// Each clause holds 101 expressions, so three of them exceed the bound of 200.
	big := `{"clause_type":"INCLUDE","simple_filter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS",` +
		`"filter_expression":{"and_group":[` + strings.Join(groups, ",") + `]}}}`
	for name, clauses := range map[string]string{
		"deeply nested":    wrap(deepChain(40)),
		"nested to 8":      wrap(deepChain(8)),
		"huge":             wrap(`{"dimension_or_metric_filter":{"field_name":"x","string_filter":{"value":"` + strings.Repeat("v", 70000) + `","match_type":"EXACT"}}}`),
		"too many nodes":   big + "," + big + "," + big,
		"too many clauses": strings.TrimSuffix(strings.Repeat(wrap(leaf)+",", 21), ","),
	} {
		if err := handle(clauses); !refusedInvalid(err) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if err := handle(big); refusedInvalid(err) {
		t.Errorf("101 expressions were refused: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d, want only the accepted call", len(requests))
	}
}

func TestAudiencesUpdateMaskComesFromSetArguments(t *testing.T) {
	const name = `"name":"properties/200/audiences/7001"`
	for _, c := range []struct{ args, mask, body string }{
		{`,"display_name":"New"`, "display_name", `{"displayName":"New"}`},
		{`,"description":"Fresh"`, "description", `{"description":"Fresh"}`},
		{`,"event_trigger":{"event_name":"joined","log_condition":"AUDIENCE_MEMBERSHIP_RENEWED"}`, "event_trigger",
			`{"eventTrigger":{"eventName":"joined","logCondition":"AUDIENCE_MEMBERSHIP_RENEWED"}}`},
		{`,"event_trigger":{"event_name":"joined","log_condition":"AUDIENCE_JOINED"},"description":"d","display_name":"N"`,
			"display_name,description,event_trigger",
			`{"description":"d","displayName":"N","eventTrigger":{"eventName":"joined","logCondition":"AUDIENCE_JOINED"}}`},
	} {
		env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, audienceOneJSON)})
		args := `{` + name + c.args + `}`
		if _, err := env.call(audiencesUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(audiencesUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1alpha/properties/200/audiences/7001" ||
			requests[0].rawQuery != "updateMask="+strings.NewReplacer(",", "%2C").Replace(c.mask) ||
			requests[0].body != c.body {
			t.Fatalf("%v, %+v", err, requests)
		}
	}
}

func TestAudiencesUpdateRefusesFieldsThatCannotBeChanged(t *testing.T) {
	const name = `"name":"properties/200/audiences/7001"`
	env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, audienceOneJSON)})
	for argName, args := range map[string]string{
		"filter clauses":    `{` + name + `,"display_name":"x","filter_clauses":[` + simpleClause + `]}`,
		"membership days":   `{` + name + `,"display_name":"x","membership_duration_days":10}`,
		"exclusion mode":    `{` + name + `,"display_name":"x","exclusion_duration_mode":"EXCLUDE_PERMANENTLY"}`,
		"only filter":       `{` + name + `,"filter_clauses":[` + simpleClause + `]}`,
		"no field":          `{` + name + `}`,
		"empty name":        `{` + name + `,"display_name":""}`,
		"blank description": `{` + name + `,"description":" "}`,
		"bad trigger":       `{` + name + `,"event_trigger":{"event_name":"x"}}`,
		"bad name":          `{"name":"properties/200","display_name":"x"}`,
		"export name":       `{"name":"properties/200/audienceExports/7","display_name":"x"}`,
		"long description":  `{` + name + `,"description":"` + strings.Repeat("d", 4097) + `"}`,
	} {
		if _, err := env.confirmed(audiencesUpdateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", argName)
		}
	}
	env.noIO(t)
	// The handler refuses them too when the schema is skipped.
	resolved := env.resolved(t, "sfree")
	for _, args := range []string{`{` + name + `,"display_name":"x","filter_clauses":[` + simpleClause + `]}`,
		`{` + name + `,"display_name":"x","membership_duration_days":10}`,
		`{` + name + `,"display_name":"x","exclusion_duration_mode":"EXCLUDE_PERMANENTLY"}`} {
		if _, err := handleAudiencesUpdate(context.Background(), resolved, env.secrets(), env.red, json.RawMessage(args)); !refusedInvalid(err) {
			t.Errorf("handler accepted %s: %v", args, err)
		}
	}
	env.noIO(t)
}

func TestAudiencesArchive(t *testing.T) {
	env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"properties/200/audiences/7001"}`
	if _, err := env.call(audiencesArchiveID, "sfree", args); err == nil {
		t.Fatal("archive without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(audiencesArchiveID, "sfree", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != "/v1alpha/properties/200/audiences/7001:archive" || requests[0].body != `{}` ||
		!strings.Contains(result, `"archived":true`) || !strings.Contains(result, `"name":"properties/200/audiences/7001"`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func TestAudiencesFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{audiencesListID, `{"property":"PROP"}`, "GET"},
		{audiencesGetID, `{"name":"PROP/audiences/7001"}`, "GET"},
		{audiencesCreateID, audienceCreateArgs(simpleClause), "POST"},
		{audiencesUpdateID, `{"name":"PROP/audiences/7001","display_name":"S"}`, "PATCH"},
		{audiencesArchiveID, `{"name":"PROP/audiences/7001"}`, "POST"},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			property := c.property
			env := audiencesEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1alpha/"), ":archive")
					if r.Method == http.MethodPost && !strings.Contains(name, "/audiences/") {
						name += "/7001"
					}
					body = `{"audiences":[],"name":"` + name + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			args := strings.ReplaceAll(tool.arguments, "PROP", property)
			if tool.id == audiencesCreateID {
				args = strings.ReplaceAll(args, "properties/200", property)
			}
			result, err := env.confirmed(tool.id, c.connection, args)
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = tool.method
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, c.connection, property, err,
					methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, c.connection, property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestAudienceErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct {
		id, arguments string
		write         bool
	}{
		{audiencesListID, `{"property":"properties/200"}`, false},
		{audiencesGetID, `{"name":"properties/200/audiences/7001"}`, false},
		{audiencesCreateID, audienceCreateArgs(simpleClause), true},
		{audiencesUpdateID, `{"name":"properties/200/audiences/7001","display_name":"S"}`, true},
		{audiencesArchiveID, `{"name":"properties/200/audiences/7001"}`, true},
	}
	for _, c := range []struct {
		name   string
		api    func(int, *http.Request) (*http.Response, error)
		class  provider.Class
		unsure bool
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission, false},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, false},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited, false},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, true},
	} {
		for _, call := range calls {
			t.Run(c.name+" "+call.id, func(t *testing.T) {
				env := audiencesEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(call.id, "sfree", call.arguments)
				if classOf(err) != c.class || leaks(err.Error()) != "" {
					t.Fatalf("error = %v", err)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != (c.unsure && call.write) {
					t.Errorf("unknown outcome reported = %t: %v", got, err)
				}
				if requests, _ := env.fake.seen(); len(requests) != 1 {
					t.Errorf("requests = %d, want exactly one", len(requests))
				}
			})
		}
	}
}

func TestAudience429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := audiencesEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", audienceOneJSON), nil
	}})
	const args = `{"name":"properties/200/audiences/7001"}`
	if _, err := env.call(audiencesGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(audiencesGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestAudienceWritesDistrustAnAnswerOfAnotherAudience(t *testing.T) {
	env := audiencesEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/audiences/7001"}`)})
	if _, err := env.confirmed(audiencesUpdateID, "sfree", `{"name":"properties/200/audiences/7001","display_name":"S"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	if _, err := env.confirmed(audiencesCreateID, "sfree", audienceCreateArgs(simpleClause)); classOf(err) != provider.ClassProviderError {
		t.Errorf("create: %v", err)
	}
}

func TestAudienceToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for id, want := range map[string]string{audiencesListID: scopeReadOnly, audiencesGetID: scopeReadOnly,
		audiencesCreateID: scopeEdit, audiencesUpdateID: scopeEdit, audiencesArchiveID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
		descriptor, _, ok := reg.Lookup(id)
		if !ok || descriptor.RequiresToolAllowList || !slices.Contains(profiles["write"], id) ||
			slices.Contains(profiles["read"], id) != (want == scopeReadOnly) ||
			!strings.HasSuffix(descriptor.Description, alphaNote) {
			t.Errorf("%s: descriptor %+v, profiles wrong", id, descriptor)
		}
	}
	create := reg.ConfirmationSummary(audiencesCreateID)(json.RawMessage(audienceCreateArgs(simpleClause)))
	update := reg.ConfirmationSummary(audiencesUpdateID)(json.RawMessage(
		`{"name":"properties/200/audiences/7001","description":"a","display_name":"X"}`))
	archive := reg.ConfirmationSummary(audiencesArchiveID)(json.RawMessage(`{"name":"properties/200/audiences/7001"}`))
	for got, wants := range map[string][]string{
		create:  {"Create", "Purchasers", "properties/200", "30 days", "1 filter clauses"},
		update:  {"display_name, description", "properties/200/audiences/7001"},
		archive: {"Archive", "properties/200/audiences/7001"}} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}
