package googleanalytics

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var edsTools = []string{edsListID, edsGetID, edsCreateID, edsUpdateID, edsDeleteID}

// edsEnv adds connections that list the five expanded data set tools, with the bindings of the stream tests.
func edsEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: edsTools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	edsOneJSON = `{"name":"properties/200/expandedDataSets/3001","displayName":"Events","description":"Event data",` +
		`"dimensionNames":["eventName","country"],"metricNames":["eventCount"],"dimensionFilterExpression":` +
		`{"andGroup":{"filterExpressions":[{"filter":{"fieldName":"eventName","inListFilter":{"values":` +
		`["page_view","click"],"caseSensitive":true}}},{"notExpression":{"filter":{"fieldName":"country",` +
		`"stringFilter":{"matchType":"CONTAINS","value":"test","caseSensitive":false}}}}]}},` +
		`"dataCollectionStartTime":"2026-11-17T10:00:00Z","extra":1}`
	edsTwoJSON   = `{"name":"properties/200/expandedDataSets/3002","displayName":"Two","dimensionNames":["eventName"]}`
	edsThreeJSON = `{"name":"properties/200/expandedDataSets/3003","displayName":"Three"}`
)

type edsOut struct {
	ExpandedDataSets []expandedDataSet `json:"expandedDataSets"`
	NextCursor       string            `json:"next_cursor"`
	HasMore          bool              `json:"has_more"`
}

func TestExpandedDataSetsListPagesThroughThreePages(t *testing.T) {
	pages := []string{
		`{"expandedDataSets":[` + edsOneJSON + `],"nextPageToken":"t2"}`,
		`{"expandedDataSets":[` + edsTwoJSON + `],"nextPageToken":"t3"}`,
		`{"expandedDataSets":[` + edsThreeJSON + `]}`,
	}
	env := edsEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(edsListID, "sfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		var out edsOut
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.ExpandedDataSets) != 1 ||
			out.HasMore != (i < 2) || strings.Contains(result, "extra") {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.ExpandedDataSets[0].Name))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"properties/200/expandedDataSets/3001", "properties/200/expandedDataSets/3002",
		"properties/200/expandedDataSets/3003"}) || len(requests) != 3 ||
		requests[0].path != "/v1alpha/properties/200/expandedDataSets" || requests[0].rawQuery != "pageSize=1" ||
		!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
}

func TestExpandedDataSetsListRefusesACursorOfAnotherProperty(t *testing.T) {
	env := edsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"expandedDataSets":[`+edsOneJSON+`],"nextPageToken":"t2"}`)})
	result, err := env.call(edsListID, "sfree", `{"property":"properties/200","page_size":1}`)
	var out edsOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || out.NextCursor == "" {
		t.Fatalf("%v %s", err, result)
	}
	if _, err := env.call(edsListID, "sfree", `{"property":"properties/201","cursor":"`+out.NextCursor+`"}`); err == nil {
		t.Error("a cursor of another property was accepted")
	}
}

func TestExpandedDataSetsGetReturnsEveryDocumentedField(t *testing.T) {
	env := edsEnv(t, &fakeGoogle{api: always(http.StatusOK, edsOneJSON)})
	result, err := env.call(edsGetID, "sfree", `{"name":"properties/200/expandedDataSets/3001"}`)
	var got expandedDataSet
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || strings.Contains(result, "extra") {
		t.Fatalf("%v %s", err, result)
	}
	requests, _ := env.fake.seen()
	group := got.DimensionFilterExpression.AndGroup.FilterExpressions
	if got.DisplayName != "Events" || got.Description != "Event data" || !slices.Equal(got.DimensionNames, apiNames{"eventName", "country"}) ||
		!slices.Equal(got.MetricNames, apiNames{"eventCount"}) || got.DataCollectionStartTime != "2026-11-17T10:00:00Z" ||
		len(group) != 2 || group[0].Filter.FieldName != "eventName" ||
		!slices.Equal(group[0].Filter.InListFilter.Values, textList{"page_view", "click"}) ||
		!*group[0].Filter.InListFilter.CaseSensitive || group[1].NotExpression.Filter.StringFilter.MatchType != "CONTAINS" ||
		group[1].NotExpression.Filter.StringFilter.Value != "test" || *group[1].NotExpression.Filter.StringFilter.CaseSensitive ||
		len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/expandedDataSets/3001" {
		t.Errorf("data set = %+v, requests %+v", got, requests)
	}
}

func TestExpandedDataSetsAnswerIsBoundedAndChecked(t *testing.T) {
	names := make([]string, 300)
	for i := range names {
		names[i] = "d" + strings.Repeat("x", 3)
	}
	encodedNames, _ := json.Marshal(append(names, "bad name", "9lead"))
	env := edsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"expandedDataSets":[`+
		`{"name":"properties/200/expandedDataSets/a","displayName":"`+strings.Repeat("n", 400)+`","description":"`+
		strings.Repeat("d", 400)+`","dimensionNames":`+string(encodedNames)+`,"dataCollectionStartTime":"not a time"},`+
		`{"name":"properties/999/expandedDataSets/b"},{"name":"properties/200/expandedDataSets/bad name"},{"name":"nonsense"}]}`)})
	result, err := env.call(edsListID, "sfree", `{"property":"properties/200"}`)
	var out edsOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.ExpandedDataSets) != 1 {
		t.Fatalf("result %v, %v", len(result), err)
	}
	s := out.ExpandedDataSets[0]
	if len([]rune(string(s.DisplayName))) != maxTextRunes+1 || len([]rune(string(s.Description))) != maxTextRunes+1 ||
		len(s.DimensionNames) != maxEdsNamesShown || s.DataCollectionStartTime != "" {
		t.Errorf("data set = %d %d %d %q", len(s.DisplayName), len(s.Description), len(s.DimensionNames), s.DataCollectionStartTime)
	}
	env = edsEnv(t, &fakeGoogle{api: always(http.StatusOK, edsOneJSON)})
	if _, err := env.call(edsGetID, "sfree", `{"name":"properties/200/expandedDataSets/other"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

const edsCreateArgs = `{"property":"properties/200","display_name":"Events","description":"Event data",` +
	`"dimension_names":["eventName","country"],"metric_names":["eventCount"],"dimension_filter_expression":` +
	`{"and_group":{"filter_expressions":[{"filter":{"field_name":"eventName","in_list_filter":{"values":` +
	`["page_view","click"],"case_sensitive":true}}},{"not_expression":{"filter":{"field_name":"country",` +
	`"string_filter":{"match_type":"CONTAINS","value":"test","case_sensitive":false}}}}]}}}`

func TestExpandedDataSetsCreateSendsTheFilterAndNeedsConfirmation(t *testing.T) {
	env := edsEnv(t, &fakeGoogle{api: always(http.StatusOK, edsOneJSON)})
	if _, err := env.call(edsCreateID, "sfree", edsCreateArgs); err == nil {
		t.Fatal("create without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(edsCreateID, "sfree", edsCreateArgs)
	requests, _ := env.fake.seen()
	const wantBody = `{"displayName":"Events","description":"Event data","dimensionNames":["eventName","country"],` +
		`"metricNames":["eventCount"],"dimensionFilterExpression":{"andGroup":{"filterExpressions":[{"filter":` +
		`{"fieldName":"eventName","inListFilter":{"values":["page_view","click"],"caseSensitive":true}}},` +
		`{"notExpression":{"filter":{"fieldName":"country","stringFilter":{"matchType":"CONTAINS","value":"test",` +
		`"caseSensitive":false}}}}]}}}`
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != "/v1alpha/properties/200/expandedDataSets" || requests[0].rawQuery != "" ||
		requests[0].body != wantBody || !strings.Contains(result, `"displayName":"Events"`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func edsArgsWith(key string, value any) string {
	m := map[string]any{"property": "properties/200", "display_name": "E", "dimension_names": []string{"eventName"},
		"metric_names": []string{"eventCount"}}
	if value == nil {
		delete(m, key)
	} else {
		m[key] = value
	}
	out, _ := json.Marshal(m)
	return string(out)
}

func filterWith(leaf string) map[string]any {
	var group any
	_ = json.Unmarshal([]byte(`{"and_group":{"filter_expressions":[`+leaf+`]}}`), &group)
	return group.(map[string]any)
}

func TestExpandedDataSetsCreateRefusesBadInputBeforeAnyIO(t *testing.T) {
	env := edsEnv(t, &fakeGoogle{api: always(http.StatusOK, edsOneJSON)})
	many := func(n int, prefix string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = prefix + strings.Repeat("a", i%9) + string(rune('a'+i%26)) + strings.Repeat("b", i/26)
		}
		return out
	}
	big := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = strings.Repeat("v", 200)
		}
		return out
	}
	deep := func(n int) string {
		leaf := make([]string, n)
		for i := range leaf {
			leaf[i] = `{"filter":{"field_name":"eventName","string_filter":{"match_type":"EXACT","value":"` +
				strings.Repeat("v", 200) + `","case_sensitive":true}}}`
		}
		return strings.Join(leaf, ",")
	}
	cases := map[string]string{
		"no display name":      edsArgsWith("display_name", nil),
		"no property":          edsArgsWith("property", nil),
		"bad property":         edsArgsWith("property", "accounts/200"),
		"blank name":           edsArgsWith("display_name", "   "),
		"long name":            edsArgsWith("display_name", strings.Repeat("n", 201)),
		"control name":         edsArgsWith("display_name", "a\x01b"),
		"long description":     edsArgsWith("description", strings.Repeat("d", 51)),
		"no dimensions":        edsArgsWith("dimension_names", []string{}),
		"too many dimensions":  edsArgsWith("dimension_names", many(26, "dim")),
		"too many metrics":     edsArgsWith("metric_names", many(51, "met")),
		"dimension with blank": edsArgsWith("dimension_names", []string{"a b"}),
		"dimension with digit": edsArgsWith("dimension_names", []string{"1a"}),
		"dimension repeated":   edsArgsWith("dimension_names", []string{"a", "a"}),
		"metric with slash":    edsArgsWith("metric_names", []string{"a/b"}),
		"filter not and":       edsArgsWith("dimension_filter_expression", map[string]any{"filter": map[string]any{}}),
		"filter empty group":   edsArgsWith("dimension_filter_expression", filterWith("")),
		"filter nested and":    edsArgsWith("dimension_filter_expression", map[string]any{"and_group": map[string]any{"filter_expressions": []any{map[string]any{"and_group": map[string]any{}}}}}),
		"leaf with both":       edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","in_list_filter":{"values":["x"],"case_sensitive":true}},"not_expression":{"filter":{"field_name":"a","in_list_filter":{"values":["x"],"case_sensitive":true}}}}`)),
		"leaf with none":       edsArgsWith("dimension_filter_expression", filterWith(`{}`)),
		"not without filter":   edsArgsWith("dimension_filter_expression", filterWith(`{"not_expression":{}}`)),
		"filter without kind":  edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a"}}`)),
		"filter with both":     edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","in_list_filter":{"values":["x"],"case_sensitive":true},"string_filter":{"match_type":"CONTAINS","value":"x"}}}`)),
		"bad field name":       edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a b","in_list_filter":{"values":["x"],"case_sensitive":true}}}`)),
		"bad match type":       edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","string_filter":{"match_type":"REGEX","value":"x"}}}`)),
		"exact insensitive":    edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","string_filter":{"match_type":"EXACT","value":"x","case_sensitive":false}}}`)),
		"exact unset":          edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","string_filter":{"match_type":"EXACT","value":"x"}}}`)),
		"contains sensitive":   edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","string_filter":{"match_type":"CONTAINS","value":"x","case_sensitive":true}}}`)),
		"empty value":          edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","string_filter":{"match_type":"CONTAINS","value":""}}}`)),
		"long value":           edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","string_filter":{"match_type":"CONTAINS","value":"`+strings.Repeat("v", 201)+`"}}}`)),
		"empty list":           edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","in_list_filter":{"values":[],"case_sensitive":true}}}`)),
		"list insensitive":     edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","in_list_filter":{"values":["x"],"case_sensitive":false}}}`)),
		"list unset":           edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","in_list_filter":{"values":["x"]}}}`)),
		"list too long":        edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","in_list_filter":{"values":["`+strings.Join(many(101, "v"), `","`)+`"],"case_sensitive":true}}}`)),
		"too many expressions": edsArgsWith("dimension_filter_expression", filterWith(deep(21))),
		"too large":            edsArgsWith("dimension_filter_expression", filterWith(`{"filter":{"field_name":"a","in_list_filter":{"values":["`+strings.Join(big(100), `","`)+`"],"case_sensitive":true}}}`)),
	}
	for name, args := range cases {
		if _, err := env.confirmed(edsCreateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
	ok20 := filterWith(deep(1))
	for name, args := range map[string]string{
		"bare":        edsArgsWith("display_name", strings.Repeat("n", 200)),
		"description": edsArgsWith("description", strings.Repeat("d", 50)),
		"dimensions":  edsArgsWith("dimension_names", many(25, "dim")),
		"metrics":     edsArgsWith("metric_names", many(50, "met")),
		"filter":      edsArgsWith("dimension_filter_expression", ok20),
	} {
		if _, err := env.confirmed(edsCreateID, "sfree", args); refusedInvalid(err) {
			t.Errorf("%s: refused as %v", name, err)
		}
	}
}

func TestExpandedDataSetsUpdateMaskComesFromSetArguments(t *testing.T) {
	const name = `"name":"properties/200/expandedDataSets/3001"`
	for _, c := range []struct{ args, mask, body string }{
		{`,"display_name":"New"`, "display_name", `{"displayName":"New"}`},
		{`,"description":""`, "description", `{"description":""}`},
		{`,"description":"d","display_name":"N"`, "display_name%2Cdescription", `{"displayName":"N","description":"d"}`},
	} {
		env := edsEnv(t, &fakeGoogle{api: always(http.StatusOK, edsOneJSON)})
		args := `{` + name + c.args + `}`
		if _, err := env.call(edsUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(edsUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1alpha/properties/200/expandedDataSets/3001" ||
			requests[0].rawQuery != "updateMask="+c.mask || requests[0].body != c.body {
			t.Fatalf("%v, %+v", err, requests)
		}
	}
}

func TestExpandedDataSetsUpdateRefusesImmutableFieldsAndBadInputBeforeAnyIO(t *testing.T) {
	const name = `"name":"properties/200/expandedDataSets/3001"`
	env := edsEnv(t, &fakeGoogle{api: always(http.StatusOK, edsOneJSON)})
	for argName, args := range map[string]string{
		"no field":           `{` + name + `}`,
		"empty name":         `{` + name + `,"display_name":""}`,
		"long name":          `{` + name + `,"display_name":"` + strings.Repeat("n", 201) + `"}`,
		"long description":   `{` + name + `,"description":"` + strings.Repeat("d", 51) + `"}`,
		"bad name":           `{"name":"properties/200","display_name":"x"}`,
		"id with blank":      `{"name":"properties/200/expandedDataSets/a b","display_name":"x"}`,
		"immutable dims":     `{` + name + `,"dimension_names":["eventName"]}`,
		"immutable metrics":  `{` + name + `,"metric_names":["eventCount"]}`,
		"immutable filter":   `{` + name + `,"dimension_filter_expression":{"and_group":{"filter_expressions":[]}}}`,
		"immutable with set": `{` + name + `,"display_name":"x","dimension_names":["eventName"]}`,
	} {
		if _, err := env.confirmed(edsUpdateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", argName)
		}
	}
	env.noIO(t)
	// The handler refuses the immutable members as well when the schema layer is out of the way.
	_, _, ok := registry(t).Lookup(edsUpdateID)
	if !ok {
		t.Fatal("update tool is not registered")
	}
	if _, err := handleEdsUpdate(t.Context(), nil, nil, nil, json.RawMessage(
		`{`+name+`,"dimension_names":["eventName"]}`)); err == nil {
		t.Error("handler accepted an immutable member")
	}
}

func TestExpandedDataSetsDelete(t *testing.T) {
	env := edsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"properties/200/expandedDataSets/3001"}`
	if _, err := env.call(edsDeleteID, "sfree", args); err == nil {
		t.Fatal("delete without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(edsDeleteID, "sfree", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete ||
		requests[0].path != "/v1alpha/properties/200/expandedDataSets/3001" || requests[0].body != "" ||
		!strings.Contains(result, `"deleted":true`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func TestExpandedDataSetsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{edsListID, `{"property":"PROP"}`, "GET"},
		{edsGetID, `{"name":"PROP/expandedDataSets/3001"}`, "GET"},
		{edsCreateID, `{"property":"PROP","display_name":"S"}`, "POST"},
		{edsUpdateID, `{"name":"PROP/expandedDataSets/3001","display_name":"S"}`, "PATCH"},
		{edsDeleteID, `{"name":"PROP/expandedDataSets/3001"}`, "DELETE"},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			property := c.property
			env := edsEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimPrefix(r.URL.Path, "/v1alpha/")
					if r.Method == http.MethodPost {
						name += "/3001"
					}
					body = `{"expandedDataSets":[],"name":"` + name + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			args := strings.ReplaceAll(tool.arguments, "PROP", property)
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

var edsCalls = []struct {
	id, arguments string
	write         bool
}{
	{edsListID, `{"property":"properties/200"}`, false},
	{edsGetID, `{"name":"properties/200/expandedDataSets/3001"}`, false},
	{edsCreateID, `{"property":"properties/200","display_name":"S","dimension_names":["eventName"],"metric_names":["eventCount"]}`, true},
	{edsUpdateID, `{"name":"properties/200/expandedDataSets/3001","display_name":"S"}`, true},
	{edsDeleteID, `{"name":"properties/200/expandedDataSets/3001"}`, true},
}

func TestExpandedDataSetErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
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
		for _, call := range edsCalls {
			t.Run(c.name+" "+call.id, func(t *testing.T) {
				env := edsEnv(t, &fakeGoogle{api: c.api})
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

// A property without Analytics 360 is not verified live. The simulated answers below are the shapes Google
// uses for a failed precondition and for a refused feature; each must come back as an error without the
// provider text, after exactly one request and without a retry.
func TestExpandedDataSetsOnAPropertyWithout360AreProviderErrorsWithoutRetry(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden} {
		for _, call := range edsCalls {
			t.Run(http.StatusText(status)+" "+call.id, func(t *testing.T) {
				const secretText = "Expanded data sets need Analytics 360"
				body := strings.Replace(errorObject(status, "FAILED_PRECONDITION", ""), `"message":"`, `"message":"`+secretText, 1)
				env := edsEnv(t, &fakeGoogle{api: always(status, body)})
				_, err := env.confirmed(call.id, "sfree", call.arguments)
				if err == nil || leaks(err.Error()) != "" || strings.Contains(err.Error(), secretText) ||
					strings.Contains(err.Error(), "unknown") {
					t.Fatalf("error = %v", err)
				}
				if class := classOf(err); class == provider.ClassRateLimited || class == provider.ClassNotFound {
					t.Errorf("class = %v", class)
				}
				if requests, _ := env.fake.seen(); len(requests) != 1 {
					t.Errorf("requests = %d, want exactly one", len(requests))
				}
			})
		}
	}
	env := edsEnv(t, &fakeGoogle{api: always(http.StatusBadRequest, errorObject(400, "FAILED_PRECONDITION", ""))})
	if _, err := env.call(edsListID, "sfree", `{"property":"properties/200"}`); classOf(err) != provider.ClassProviderError {
		t.Errorf("400 class = %v", classOf(err))
	}
}

func TestExpandedDataSet429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := edsEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", edsOneJSON), nil
	}})
	const args = `{"name":"properties/200/expandedDataSets/3001"}`
	if _, err := env.call(edsGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(edsGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestExpandedDataSetWritesDistrustAnAnswerOfAnotherDataSet(t *testing.T) {
	env := edsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/expandedDataSets/1"}`)})
	if _, err := env.confirmed(edsUpdateID, "sfree", `{"name":"properties/200/expandedDataSets/1","display_name":"S"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	if _, err := env.confirmed(edsCreateID, "sfree", edsCreateArgs); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("create: %v", err)
	}
}

func TestExpandedDataSetToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for id, want := range map[string]string{edsListID: scopeReadOnly, edsGetID: scopeReadOnly,
		edsCreateID: scopeEdit, edsUpdateID: scopeEdit, edsDeleteID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
		descriptor, _, ok := reg.Lookup(id)
		deletion := id == edsDeleteID
		if !ok || descriptor.RequiresToolAllowList != deletion || slices.Contains(profiles["write"], id) == deletion ||
			slices.Contains(profiles["read"], id) != (want == scopeReadOnly) ||
			!strings.HasSuffix(descriptor.Description, alphaNote) && !strings.Contains(descriptor.Description, "ALPHA:") {
			t.Errorf("%s: descriptor %+v, profiles wrong", id, descriptor)
		}
	}
	create := reg.ConfirmationSummary(edsCreateID)(json.RawMessage(edsCreateArgs))
	update := reg.ConfirmationSummary(edsUpdateID)(json.RawMessage(
		`{"name":"properties/200/expandedDataSets/1","description":"a","display_name":"X"}`))
	del := reg.ConfirmationSummary(edsDeleteID)(json.RawMessage(`{"name":"properties/200/expandedDataSets/1"}`))
	for got, wants := range map[string][]string{
		create: {"Create", "Events", "properties/200", "2 dimensions", "1 metrics", "dimension filter"},
		update: {"display_name, description", "properties/200/expandedDataSets/1"},
		del:    {"Delete", "properties/200/expandedDataSets/1"}} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}
