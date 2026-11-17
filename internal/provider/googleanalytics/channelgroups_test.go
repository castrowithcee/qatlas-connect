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

// channelEnv adds connections that list the five channel group tools, with the bindings of the stream tests.
func channelEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{channelGroupsListID, channelGroupsGetID, channelGroupsCreateID, channelGroupsUpdateID,
		channelGroupsDeleteID}
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	groupOneJSON = `{"name":"properties/200/channelGroups/3001","displayName":"Partners","description":"Paid and partner traffic",` +
		`"groupingRule":[{"displayName":"Newsletter","expression":{"andGroup":{"filterExpressions":[{"orGroup":` +
		`{"filterExpressions":[{"filter":{"fieldName":"sessionSource","stringFilter":{"matchType":"EXACT","value":"newsletter"}}},` +
		`{"notExpression":{"filter":{"fieldName":"sessionMedium","inListFilter":{"values":["email","mail"]}}}}]}}]}}}],` +
		`"systemDefined":false,"primary":true,"extra":1}`
	groupTwoJSON   = `{"name":"properties/200/channelGroups/3002","displayName":"Second"}`
	groupThreeJSON = `{"name":"properties/200/channelGroups/3003","displayName":"Default Channel Group","systemDefined":true}`
)

type channelOut struct {
	ChannelGroups []channelGroup `json:"channelGroups"`
	NextCursor    string         `json:"next_cursor"`
	HasMore       bool           `json:"has_more"`
}

func TestChannelGroupsListPagesThroughThreePages(t *testing.T) {
	pages := []string{
		`{"channelGroups":[` + groupOneJSON + `],"nextPageToken":"t2"}`,
		`{"channelGroups":[` + groupTwoJSON + `],"nextPageToken":"t3"}`,
		`{"channelGroups":[` + groupThreeJSON + `]}`,
	}
	env := channelEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(channelGroupsListID, "sfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		var out channelOut
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.ChannelGroups) != 1 ||
			out.HasMore != (i < 2) || strings.Contains(result, "extra") {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.ChannelGroups[0].Name))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"properties/200/channelGroups/3001", "properties/200/channelGroups/3002",
		"properties/200/channelGroups/3003"}) || len(requests) != 3 ||
		requests[0].path != "/v1alpha/properties/200/channelGroups" || requests[0].rawQuery != "pageSize=1" ||
		!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	if _, err := env.call(channelGroupsListID, "sfree", `{"property":"properties/201","cursor":"`+cursor+`"}`); err == nil && cursor != "" {
		t.Error("a cursor of another property was accepted")
	}
}

func TestChannelGroupsGetReturnsEveryDocumentedField(t *testing.T) {
	env := channelEnv(t, &fakeGoogle{api: always(http.StatusOK, groupOneJSON)})
	result, err := env.call(channelGroupsGetID, "sfree", `{"name":"properties/200/channelGroups/3001"}`)
	var got channelGroup
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || strings.Contains(result, "extra") {
		t.Fatalf("%v %s", err, result)
	}
	requests, _ := env.fake.seen()
	if got.DisplayName != "Partners" || got.Description != "Paid and partner traffic" ||
		got.SystemDefined == nil || *got.SystemDefined || got.Primary == nil || !*got.Primary ||
		len(got.GroupingRule) != 1 || got.GroupingRule[0].DisplayName != "Newsletter" ||
		len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/channelGroups/3001" {
		t.Fatalf("group = %+v, requests %+v", got, requests)
	}
	or := got.GroupingRule[0].Expression.AndGroup.FilterExpressions[0].OrGroup.FilterExpressions
	if len(or) != 2 || or[0].Filter.FieldName != "sessionSource" || or[0].Filter.StringFilter.MatchType != "EXACT" ||
		or[0].Filter.StringFilter.Value != "newsletter" || or[1].NotExpression.Filter.InListFilter.Values[1] != "mail" {
		t.Errorf("expression = %+v", or)
	}
}

func TestChannelGroupsAnswerIsBoundedAndChecked(t *testing.T) {
	deep := `{"filter":{"fieldName":"x"}}`
	for i := 0; i < 30; i++ {
		deep = `{"notExpression":` + deep + `}`
	}
	env := channelEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"channelGroups":[`+
		`{"name":"properties/200/channelGroups/a","displayName":"`+strings.Repeat("n", 400)+`","description":"`+
		strings.Repeat("d", 400)+`","groupingRule":[{"displayName":"r","expression":`+deep+`},`+
		`{"displayName":"v","expression":{"filter":{"fieldName":"f","stringFilter":{"matchType":"bad type","value":"`+
		strings.Repeat("v", 1500)+`"}}}}]},`+
		`{"name":"properties/999/channelGroups/b"},{"name":"properties/200/channelGroups/bad name"},{"name":"nonsense"}]}`)})
	result, err := env.call(channelGroupsListID, "sfree", `{"property":"properties/200"}`)
	var out channelOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.ChannelGroups) != 1 {
		t.Fatalf("result %v, %v", len(result), err)
	}
	g := out.ChannelGroups[0]
	if len([]rune(string(g.DisplayName))) != maxTextRunes+1 || len([]rune(string(g.Description))) != maxChannelDescriptionRunes+1 {
		t.Errorf("texts = %d %d", len(g.DisplayName), len(g.Description))
	}
	depth := 0
	for e := g.GroupingRule[0].Expression; e != nil && e.NotExpression != nil; e = e.NotExpression {
		depth++
	}
	value := g.GroupingRule[1].Expression.Filter.StringFilter
	if depth >= 30 || depth > maxResultDepth || value.MatchType != "" ||
		len([]rune(string(value.Value))) != maxRuleValueRunes+1 {
		t.Errorf("depth %d, filter %+v", depth, value)
	}
	env = channelEnv(t, &fakeGoogle{api: always(http.StatusOK, groupOneJSON)})
	if _, err := env.call(channelGroupsGetID, "sfree", `{"name":"properties/200/channelGroups/other"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

// rule builds a grouping rule of the documented shape with one string filter.
func channelRule(name, value string) string {
	return `{"display_name":"` + name + `","expression":{"and_group":{"filter_expressions":[{"or_group":` +
		`{"filter_expressions":[{"filter":{"field_name":"sessionSource","string_filter":` +
		`{"match_type":"EXACT","value":"` + value + `"}}}]}}]}}}`
}

const (
	channelOneRule = `[` + `{"display_name":"Newsletter","expression":{"and_group":{"filter_expressions":[{"or_group":` +
		`{"filter_expressions":[{"filter":{"field_name":"sessionSource","string_filter":{"match_type":"EXACT",` +
		`"value":"newsletter"}}}]}}]}}}]`
	channelCreateArgs = `{"property":"properties/200","display_name":"Partners","description":"Paid and partner traffic",` +
		`"grouping_rule":[` +
		`{"display_name":"Newsletter","expression":{"and_group":{"filter_expressions":[{"or_group":{"filter_expressions":` +
		`[{"filter":{"field_name":"sessionSource","string_filter":{"match_type":"EXACT","value":"newsletter"}}},` +
		`{"not_expression":{"filter":{"field_name":"sessionMedium","in_list_filter":{"values":["email","mail"]}}}}]}}]}}},` +
		`{"display_name":"Referral","expression":{"and_group":{"filter_expressions":[{"or_group":{"filter_expressions":` +
		`[{"filter":{"field_name":"sessionMedium","string_filter":{"match_type":"CONTAINS","value":"referral"}}}]}}]}}}]}`
	channelCreateBody = `{"displayName":"Partners","description":"Paid and partner traffic","groupingRule":[` +
		`{"displayName":"Newsletter","expression":{"andGroup":{"filterExpressions":[{"orGroup":{"filterExpressions":` +
		`[{"filter":{"fieldName":"sessionSource","stringFilter":{"matchType":"EXACT","value":"newsletter"}}},` +
		`{"notExpression":{"filter":{"fieldName":"sessionMedium","inListFilter":{"values":["email","mail"]}}}}]}}]}}},` +
		`{"displayName":"Referral","expression":{"andGroup":{"filterExpressions":[{"orGroup":{"filterExpressions":` +
		`[{"filter":{"fieldName":"sessionMedium","stringFilter":{"matchType":"CONTAINS","value":"referral"}}}]}}]}}}]}`
)

func TestChannelGroupsCreateSendsSeveralRulesAndNeedsConfirmation(t *testing.T) {
	env := channelEnv(t, &fakeGoogle{api: always(http.StatusOK, groupOneJSON)})
	if _, err := env.call(channelGroupsCreateID, "sfree", channelCreateArgs); err == nil {
		t.Fatal("create without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(channelGroupsCreateID, "sfree", channelCreateArgs)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != "/v1alpha/properties/200/channelGroups" || requests[0].rawQuery != "" ||
		requests[0].body != channelCreateBody || !strings.Contains(result, `"displayName":"Partners"`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func TestChannelGroupsCreateRefusesBadInputBeforeAnyIO(t *testing.T) {
	env := channelEnv(t, &fakeGoogle{api: always(http.StatusOK, groupOneJSON)})
	rules := func(n int) string {
		list := make([]string, n)
		for i := range list {
			list[i] = channelRule("c", "v")
		}
		return `[` + strings.Join(list, ",") + `]`
	}
	filter := func(f string) string {
		return `[{"display_name":"c","expression":{"and_group":{"filter_expressions":[{"or_group":{"filter_expressions":[` +
			f + `]}}]}}}]`
	}
	nest := func(levels int) string {
		inner := `{"filter":{"field_name":"f","string_filter":{"match_type":"EXACT","value":"v"}}}`
		for i := 1; i < levels; i++ {
			inner = `{"not_expression":` + inner + `}`
		}
		return `[{"display_name":"c","expression":{"and_group":{"filter_expressions":[` + inner + `]}}}]`
	}
	values := func(n int) string {
		list := make([]string, n)
		for i := range list {
			list[i] = `"v"`
		}
		return `{"filter":{"field_name":"f","in_list_filter":{"values":[` + strings.Join(list, ",") + `]}}}`
	}
	with := func(key, value string) string {
		base := map[string]json.RawMessage{"property": json.RawMessage(`"properties/200"`),
			"display_name": json.RawMessage(`"G"`), "grouping_rule": json.RawMessage(channelOneRule)}
		if value == "" {
			delete(base, key)
		} else {
			base[key] = json.RawMessage(value)
		}
		out, _ := json.Marshal(base)
		return string(out)
	}
	for name, args := range map[string]string{
		"no property":       with("property", ""),
		"no display name":   with("display_name", ""),
		"no rules":          with("grouping_rule", ""),
		"bad property":      with("property", `"accounts/200"`),
		"blank name":        with("display_name", `"   "`),
		"long name":         with("display_name", `"`+strings.Repeat("n", 81)+`"`),
		"control name":      with("display_name", `"a\u0001b"`),
		"long desc":         with("description", `"`+strings.Repeat("d", 257)+`"`),
		"empty rules":       with("grouping_rule", `[]`),
		"51 rules":          with("grouping_rule", rules(51)),
		"blank rule name":   with("grouping_rule", strings.Replace(channelOneRule, `"Newsletter"`, `"  "`, 1)),
		"no expression":     with("grouping_rule", `[{"display_name":"c"}]`),
		"top level filter":  with("grouping_rule", `[{"display_name":"c","expression":{"filter":{"field_name":"f","string_filter":{"match_type":"EXACT","value":"v"}}}}]`),
		"top level or":      with("grouping_rule", `[{"display_name":"c","expression":{"or_group":{"filter_expressions":[{"filter":{"field_name":"f","string_filter":{"match_type":"EXACT","value":"v"}}}]}}}]`),
		"empty node":        with("grouping_rule", filter(`{}`)),
		"two members":       with("grouping_rule", filter(`{"filter":{"field_name":"f","string_filter":{"match_type":"EXACT","value":"v"}},"or_group":{"filter_expressions":[]}}`)),
		"empty group":       with("grouping_rule", `[{"display_name":"c","expression":{"and_group":{"filter_expressions":[]}}}]`),
		"no field":          with("grouping_rule", filter(`{"filter":{"string_filter":{"match_type":"EXACT","value":"v"}}}`)),
		"filter without":    with("grouping_rule", filter(`{"filter":{"field_name":"f"}}`)),
		"both filters":      with("grouping_rule", filter(`{"filter":{"field_name":"f","string_filter":{"match_type":"EXACT","value":"v"},"in_list_filter":{"values":["a"]}}}`)),
		"match unspecified": with("grouping_rule", filter(`{"filter":{"field_name":"f","string_filter":{"match_type":"MATCH_TYPE_UNSPECIFIED","value":"v"}}}`)),
		"no match type":     with("grouping_rule", filter(`{"filter":{"field_name":"f","string_filter":{"value":"v"}}}`)),
		"no value":          with("grouping_rule", filter(`{"filter":{"field_name":"f","string_filter":{"match_type":"EXACT"}}}`)),
		"empty value":       with("grouping_rule", filter(`{"filter":{"field_name":"f","string_filter":{"match_type":"EXACT","value":""}}}`)),
		"long value":        with("grouping_rule", filter(`{"filter":{"field_name":"f","string_filter":{"match_type":"EXACT","value":"`+strings.Repeat("v", 1001)+`"}}}`)),
		"control value":     with("grouping_rule", filter(`{"filter":{"field_name":"f","string_filter":{"match_type":"EXACT","value":"a\u0000b"}}}`)),
		"empty list":        with("grouping_rule", filter(`{"filter":{"field_name":"f","in_list_filter":{"values":[]}}}`)),
		"201 values":        with("grouping_rule", filter(values(201))),
		"too deep":          with("grouping_rule", nest(4)),
		"too large":         with("grouping_rule", `[`+strings.Repeat(channelRule("c", "v")+",", 49)+channelRule("c", "v")+`]`+``),
		"unknown member":    with("grouping_rule", strings.Replace(channelOneRule, `"display_name"`, `"extra":1,"display_name"`, 1)),
		"primary on create": with("primary", `true`),
	} {
		if name == "too large" {
			// 50 rules of 6 nodes stay below the bound; 200 values in 3 filters exceed it.
			args = with("grouping_rule", filter(values(200)+","+values(200)+","+values(200)))
		}
		if _, err := env.confirmed(channelGroupsCreateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
	if _, err := env.confirmed(channelGroupsCreateID, "sfree", with("grouping_rule", nest(3))); refusedInvalid(err) {
		t.Errorf("four levels (and, two not, filter) refused: %v", err)
	}
	for _, ok := range []string{with("display_name", `"`+strings.Repeat("n", 80)+`"`), with("grouping_rule", rules(50)),
		with("grouping_rule", filter(values(200))), with("description", `""`)} {
		if _, err := env.confirmed(channelGroupsCreateID, "sfree", ok); refusedInvalid(err) {
			t.Errorf("%.80s: refused as %v", ok, err)
		}
	}
}

func TestChannelGroupsUpdateMaskComesFromSetArguments(t *testing.T) {
	const name = `"name":"properties/200/channelGroups/3001"`
	const ruleBodyJSON = `[{"displayName":"Newsletter","expression":{"andGroup":{"filterExpressions":[{"orGroup":` +
		`{"filterExpressions":[{"filter":{"fieldName":"sessionSource","stringFilter":{"matchType":"EXACT","value":"newsletter"}}}]}}]}}}]`
	for _, c := range []struct{ args, mask, body string }{
		{`,"display_name":"New"`, "display_name", `{"displayName":"New"}`},
		{`,"description":""`, "description", `{"description":""}`},
		{`,"primary":true`, "primary", `{"primary":true}`},
		{`,"primary":false`, "primary", `{"primary":false}`},
		{`,"grouping_rule":` + channelOneRule, "grouping_rule", `{"groupingRule":` + ruleBodyJSON + `}`},
		{`,"primary":true,"grouping_rule":` + channelOneRule + `,"description":"d","display_name":"N"`,
			"display_name,description,grouping_rule,primary",
			`{"displayName":"N","description":"d","groupingRule":` + ruleBodyJSON + `,"primary":true}`},
	} {
		env := channelEnv(t, &fakeGoogle{api: always(http.StatusOK, groupOneJSON)})
		args := `{` + name + c.args + `}`
		if _, err := env.call(channelGroupsUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(channelGroupsUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1alpha/properties/200/channelGroups/3001" ||
			requests[0].rawQuery != "updateMask="+strings.NewReplacer(",", "%2C").Replace(c.mask) ||
			requests[0].body != c.body {
			t.Fatalf("%v, %+v", err, requests)
		}
	}
	env := channelEnv(t, &fakeGoogle{api: always(http.StatusOK, groupOneJSON)})
	for argName, args := range map[string]string{
		"no field":    `{` + name + `}`,
		"empty name":  `{` + name + `,"display_name":""}`,
		"empty rules": `{` + name + `,"grouping_rule":[]}`,
		"bad rule":    `{` + name + `,"grouping_rule":[{"display_name":"c","expression":{}}]}`,
		"bad primary": `{` + name + `,"primary":"yes"}`,
		"bad name":    `{"name":"properties/200","display_name":"x"}`,
		"id in name":  `{"name":"properties/200/channelGroups/a b","display_name":"x"}`,
		"system flag": `{` + name + `,"system_defined":true}`,
		"long desc":   `{` + name + `,"description":"` + strings.Repeat("d", 257) + `"}`,
	} {
		if _, err := env.confirmed(channelGroupsUpdateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", argName)
		}
	}
	env.noIO(t)
}

func TestChannelGroupsDelete(t *testing.T) {
	env := channelEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"properties/200/channelGroups/3001"}`
	if _, err := env.call(channelGroupsDeleteID, "sfree", args); err == nil {
		t.Fatal("delete without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(channelGroupsDeleteID, "sfree", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete ||
		requests[0].path != "/v1alpha/properties/200/channelGroups/3001" || requests[0].body != "" ||
		!strings.Contains(result, `"deleted":true`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

// Google's refusal of a delete of the predefined group is simulated here: the status and the text are an
// assumption, not an observed answer. The tool sends one request, reads nothing before and returns the failure.
func TestChannelGroupsDeleteOfASystemGroupReturnsGoogleError(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		class  provider.Class
	}{
		{http.StatusBadRequest, errorObject(400, "INVALID_ARGUMENT", "SYSTEM_DEFINED_GROUP"), provider.ClassProviderError},
		{http.StatusConflict, errorObject(409, "FAILED_PRECONDITION", "SYSTEM_DEFINED_GROUP"), provider.ClassProviderError},
	} {
		env := channelEnv(t, &fakeGoogle{api: always(c.status, c.body)})
		_, err := env.confirmed(channelGroupsDeleteID, "sfree", `{"name":"properties/200/channelGroups/3003"}`)
		requests, _ := env.fake.seen()
		if err == nil || leaks(err.Error()) != "" || strings.Contains(err.Error(), "outcome of the change is unknown") ||
			len(requests) != 1 || requests[0].method != http.MethodDelete {
			t.Fatalf("status %d: error %v, requests %+v", c.status, err, requests)
		}
		if classOf(err) != c.class {
			t.Errorf("status %d: class %q: %v", c.status, classOf(err), err)
		}
	}
}

func TestChannelGroupsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{channelGroupsListID, `{"property":"PROP"}`, "GET"},
		{channelGroupsGetID, `{"name":"PROP/channelGroups/g1"}`, "GET"},
		{channelGroupsCreateID, `{"property":"PROP","display_name":"S","grouping_rule":` + channelOneRule + `}`, "POST"},
		{channelGroupsUpdateID, `{"name":"PROP/channelGroups/g1","display_name":"S"}`, "PATCH"},
		{channelGroupsDeleteID, `{"name":"PROP/channelGroups/g1"}`, "DELETE"},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := channelEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimPrefix(r.URL.Path, "/v1alpha/")
					if r.Method == http.MethodPost {
						name += "/g1"
					}
					body = `{"channelGroups":[],"name":"` + name + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			args := strings.ReplaceAll(tool.arguments, "PROP", c.property)
			result, err := env.confirmed(tool.id, c.connection, args)
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = tool.method
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, c.connection, c.property, err,
					methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, c.connection, c.property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestChannelGroupErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct {
		id, arguments string
		write         bool
	}{
		{channelGroupsListID, `{"property":"properties/200"}`, false},
		{channelGroupsGetID, `{"name":"properties/200/channelGroups/g1"}`, false},
		{channelGroupsCreateID, `{"property":"properties/200","display_name":"S","grouping_rule":` + channelOneRule + `}`, true},
		{channelGroupsUpdateID, `{"name":"properties/200/channelGroups/g1","display_name":"S"}`, true},
		{channelGroupsDeleteID, `{"name":"properties/200/channelGroups/g1"}`, true},
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
				env := channelEnv(t, &fakeGoogle{api: c.api})
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

func TestChannelGroup429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := channelEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", groupOneJSON), nil
	}})
	const args = `{"name":"properties/200/channelGroups/3001"}`
	if _, err := env.call(channelGroupsGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(channelGroupsGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestChannelGroupWritesDistrustAnAnswerOfAnotherGroup(t *testing.T) {
	env := channelEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/channelGroups/g1"}`)})
	if _, err := env.confirmed(channelGroupsUpdateID, "sfree", `{"name":"properties/200/channelGroups/g1","display_name":"S"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	if _, err := env.confirmed(channelGroupsCreateID, "sfree", channelCreateArgs); classOf(err) != provider.ClassProviderError {
		t.Errorf("create: %v", err)
	}
}

func TestChannelGroupToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for id, want := range map[string]string{channelGroupsListID: scopeReadOnly, channelGroupsGetID: scopeReadOnly,
		channelGroupsCreateID: scopeEdit, channelGroupsUpdateID: scopeEdit, channelGroupsDeleteID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
		descriptor, _, ok := reg.Lookup(id)
		if !ok || descriptor.RequiresToolAllowList || !slices.Contains(profiles["write"], id) ||
			slices.Contains(profiles["read"], id) != (want == scopeReadOnly) {
			t.Errorf("%s: descriptor %+v, profiles wrong", id, descriptor)
		}
	}
	create := reg.ConfirmationSummary(channelGroupsCreateID)(json.RawMessage(channelCreateArgs))
	update := reg.ConfirmationSummary(channelGroupsUpdateID)(json.RawMessage(
		`{"name":"properties/200/channelGroups/g1","primary":true,"display_name":"X"}`))
	del := reg.ConfirmationSummary(channelGroupsDeleteID)(json.RawMessage(`{"name":"properties/200/channelGroups/g1"}`))
	for got, wants := range map[string][]string{
		create: {"Create", "Partners", "properties/200", "2 grouping rules"},
		update: {"display_name, primary", "properties/200/channelGroups/g1", "primary true"},
		del:    {"Delete", "properties/200/channelGroups/g1"}} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}
