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

const (
	ruleStream = "properties/200/dataStreams/2001"
	ruleOne    = ruleStream + "/eventCreateRules/3001"
)

var ruleTools = []string{eventRulesListID, eventRulesGetID, eventRulesCreateID, eventRulesUpdateID, eventRulesDeleteID}

// ruleEnv adds connections that offer the event create rule tools, with the bindings of the stream tests.
func ruleEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets,
			Tools: ruleTools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const ruleJSON = `{"name":"` + ruleOne + `","destinationEvent":"signup_done","eventConditions":[` +
	`{"field":"event_name","comparisonType":"EQUALS","value":"page_view"},` +
	`{"field":"page_location","comparisonType":"CONTAINS_CASE_INSENSITIVE","value":"/done","negated":true}],` +
	`"sourceCopyParameters":true,"parameterMutations":[{"parameter":"source","parameterValue":"[[page_location]]"}],"extra":1}`

type rulesOut struct {
	EventCreateRules []eventCreateRule `json:"eventCreateRules"`
	NextCursor       string            `json:"next_cursor"`
	HasMore          bool              `json:"has_more"`
}

func TestEventRulesListPagesThroughThreePages(t *testing.T) {
	rule := func(id string) string {
		return `{"name":"` + ruleStream + `/eventCreateRules/` + id + `","destinationEvent":"e` + id + `"}`
	}
	pages := []string{
		`{"eventCreateRules":[` + rule("1") + `],"nextPageToken":"t2"}`,
		`{"eventCreateRules":[` + rule("2") + `],"nextPageToken":"t3"}`,
		`{"eventCreateRules":[` + rule("3") + `]}`,
	}
	env := ruleEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"parent":"` + ruleStream + `","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(eventRulesListID, "sfree", args+`}`)
		var out rulesOut
		if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.EventCreateRules) != 1 ||
			out.HasMore != (i < 2) {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.EventCreateRules[0].DestinationEvent))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"e1", "e2", "e3"}) || len(requests) != 3 ||
		requests[0].path != alphaPrefix+ruleStream+"/eventCreateRules" || requests[0].rawQuery != "pageSize=1" ||
		!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	if _, err := env.call(eventRulesListID, "sfree", `{"parent":"properties/200/dataStreams/2002","cursor":"`+cursor+`"}`); err == nil && cursor != "" {
		t.Error("a cursor of another stream was accepted")
	}
}

func TestEventRulesGetReturnsEveryMemberAndIsBounded(t *testing.T) {
	env := ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, ruleJSON)})
	result, err := env.call(eventRulesGetID, "sfree", `{"name":"`+ruleOne+`"}`)
	requests, _ := env.fake.seen()
	var got, want map[string]any
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodGet ||
		requests[0].path != alphaPrefix+ruleOne || strings.Contains(result, "extra") ||
		json.Unmarshal([]byte(result), &got) != nil || json.Unmarshal([]byte(ruleJSON), &want) != nil {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
	delete(want, "extra")
	if !jsonEqual(got, want) {
		t.Errorf("result %s", result)
	}

	long := strings.Repeat("v", 500)
	conditions := strings.TrimSuffix(strings.Repeat(`{"field":"f","comparisonType":"EQUALS","value":"`+long+`"},`, 12), ",")
	env = ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"`+ruleOne+`","destinationEvent":"`+long+
		`","eventConditions":[`+conditions+`]}`)})
	result, err = env.call(eventRulesGetID, "sfree", `{"name":"`+ruleOne+`"}`)
	var out eventCreateRule
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.EventConditions) != maxRuleConditions ||
		len([]rune(string(out.DestinationEvent))) != maxTextRunes+1 ||
		len([]rune(string(out.EventConditions[0].Value))) != maxTextRunes+1 {
		t.Errorf("%v, %d bytes", err, len(result))
	}
}

func TestEventRulesAnswersAreChecked(t *testing.T) {
	env := ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"eventCreateRules":[`+
		`{"name":"`+ruleOne+`"},{"name":"properties/999/dataStreams/1/eventCreateRules/2"},{"name":"nonsense"},`+
		`{"destinationEvent":"unnamed"}]}`)})
	result, err := env.call(eventRulesListID, "sfree", `{"parent":"`+ruleStream+`"}`)
	var out rulesOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.EventCreateRules) != 1 {
		t.Errorf("result %s, %v", result, err)
	}
	env = ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, ruleJSON)})
	if _, err := env.call(eventRulesGetID, "sfree", `{"name":"`+ruleStream+`/eventCreateRules/9"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

func TestEventRulesCreateSendsEveryMember(t *testing.T) {
	args := `{"parent":"` + ruleStream + `","destination_event":"signup_done","event_conditions":[` +
		`{"field":"event_name","comparison_type":"EQUALS","value":"page_view"},` +
		`{"field":"page_location","comparison_type":"CONTAINS_CASE_INSENSITIVE","value":"/done","negated":true}],` +
		`"source_copy_parameters":true,"parameter_mutations":[{"parameter":"source","parameter_value":"[[page_location]]"},` +
		`{"parameter":"kind","parameter_value":"signup"}]}`
	env := ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, ruleJSON)})
	if _, err := env.call(eventRulesCreateID, "sfree", args); err == nil {
		t.Fatal("create without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(eventRulesCreateID, "sfree", args)
	requests, _ := env.fake.seen()
	want := `{"destinationEvent":"signup_done","eventConditions":[` +
		`{"field":"event_name","comparisonType":"EQUALS","value":"page_view"},` +
		`{"field":"page_location","comparisonType":"CONTAINS_CASE_INSENSITIVE","value":"/done","negated":true}],` +
		`"sourceCopyParameters":true,"parameterMutations":[{"parameter":"source","parameterValue":"[[page_location]]"},` +
		`{"parameter":"kind","parameterValue":"signup"}]}`
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != alphaPrefix+ruleStream+"/eventCreateRules" || requests[0].rawQuery != "" ||
		requests[0].body != want || !strings.Contains(result, `"destinationEvent":"signup_done"`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
	// Only the required members go out when nothing else is set.
	env = ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, ruleJSON)})
	_, err = env.confirmed(eventRulesCreateID, "sfree", `{"parent":"`+ruleStream+`","destination_event":"a",`+
		`"event_conditions":[{"field":"f","comparison_type":"EQUALS","value":"v"}]}`)
	requests, _ = env.fake.seen()
	if err != nil || requests[0].body != `{"destinationEvent":"a","eventConditions":[{"field":"f","comparisonType":"EQUALS","value":"v"}]}` {
		t.Errorf("%v, %+v", err, requests)
	}
}

func TestEventRulesUpdateMaskComesFromSetArguments(t *testing.T) {
	for _, c := range []struct{ args, mask, body string }{
		{`"destination_event":"b"`, "destination_event", `{"destinationEvent":"b"}`},
		{`"source_copy_parameters":false`, "source_copy_parameters", `{"sourceCopyParameters":false}`},
		{`"parameter_mutations":[]`, "parameter_mutations", `{"parameterMutations":[]}`},
		{`"parameter_mutations":[{"parameter":"p","parameter_value":"1"}],"event_conditions":` +
			`[{"field":"f","comparison_type":"EQUALS","value":"v","negated":false}],"destination_event":"b"`,
			"destination_event,event_conditions,parameter_mutations",
			`{"destinationEvent":"b","eventConditions":[{"field":"f","comparisonType":"EQUALS","value":"v","negated":false}],` +
				`"parameterMutations":[{"parameter":"p","parameterValue":"1"}]}`},
	} {
		env := ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, ruleJSON)})
		args := `{"name":"` + ruleOne + `",` + c.args + `}`
		if _, err := env.call(eventRulesUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(eventRulesUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != alphaPrefix+ruleOne ||
			requests[0].rawQuery != "updateMask="+strings.ReplaceAll(c.mask, ",", "%2C") || requests[0].body != c.body {
			t.Fatalf("%v, %+v", err, requests)
		}
	}
}

func TestEventRulesDelete(t *testing.T) {
	env := ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"` + ruleOne + `"}`
	if _, err := env.call(eventRulesDeleteID, "sfree", args); err == nil {
		t.Fatal("delete without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(eventRulesDeleteID, "sfree", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete ||
		requests[0].path != alphaPrefix+ruleOne || requests[0].rawQuery != "" || requests[0].body != "" ||
		!jsonEqualString(result, `{"name":"`+ruleOne+`","deleted":true}`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func jsonEqualString(a, b string) bool {
	var x, y any
	return json.Unmarshal([]byte(a), &x) == nil && json.Unmarshal([]byte(b), &y) == nil && jsonEqual(x, y)
}

func TestEventRulesRefuseBadArgumentsBeforeAnyIO(t *testing.T) {
	env := ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, ruleJSON)})
	cond := `{"field":"f","comparison_type":"EQUALS","value":"v"}`
	create := func(rest string) string { return `{"parent":"` + ruleStream + `",` + rest + `}` }
	update := func(rest string) string { return `{"name":"` + ruleOne + `",` + rest + `}` }
	ten := strings.TrimSuffix(strings.Repeat(cond+",", 10), ",")
	mutations := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `{"parameter":"p` + strings.Repeat("a", i%3) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `","parameter_value":"1"}`
		}
		return strings.Join(parts, ",")
	}
	for _, c := range []struct{ id, args string }{
		{eventRulesCreateID, create(`"event_conditions":[` + cond + `]`)},
		{eventRulesCreateID, create(`"destination_event":"a"`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[` + ten + `,` + cond + `]`)},
		{eventRulesCreateID, create(`"destination_event":"1a","event_conditions":[` + cond + `]`)},
		{eventRulesCreateID, create(`"destination_event":"_a","event_conditions":[` + cond + `]`)},
		{eventRulesCreateID, create(`"destination_event":"a b","event_conditions":[` + cond + `]`)},
		{eventRulesCreateID, create(`"destination_event":"a-b","event_conditions":[` + cond + `]`)},
		{eventRulesCreateID, create(`"destination_event":"` + strings.Repeat("a", 40) + `","event_conditions":[` + cond + `]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[{"field":"a b","comparison_type":"EQUALS","value":"v"}]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[{"field":"f","comparison_type":"NOPE","value":"v"}]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[{"field":"f","comparison_type":"COMPARISON_TYPE_UNSPECIFIED","value":"v"}]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[{"field":"f","comparison_type":"EQUALS","value":""}]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[{"field":"f","comparison_type":"EQUALS","value":"a\u0001"}]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[{"field":"f","comparison_type":"EQUALS"}]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[{"field":"f","comparison_type":"EQUALS","value":"v","other":1}]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[` + cond + `],"parameter_mutations":[` + mutations(21) + `]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[` + cond + `],"parameter_mutations":[` +
			`{"parameter":"p","parameter_value":"1"},{"parameter":"p","parameter_value":"2"}]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[` + cond + `],"parameter_mutations":[` +
			`{"parameter":"p","parameter_value":"` + strings.Repeat("v", 100) + `"}]`)},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[` + cond + `],"parameter_mutations":[` +
			`{"parameter":"p q","parameter_value":"1"}]`)},
		{eventRulesCreateID, `{"parent":"properties/200","destination_event":"a","event_conditions":[` + cond + `]}`},
		{eventRulesCreateID, create(`"destination_event":"a","event_conditions":[` + cond + `],"unknown":true`)},
		{eventRulesUpdateID, `{"name":"` + ruleOne + `"}`},
		{eventRulesUpdateID, update(`"event_conditions":[]`)},
		{eventRulesUpdateID, update(`"destination_event":""`)},
		{eventRulesUpdateID, `{"name":"` + ruleStream + `","destination_event":"a"}`},
		{eventRulesUpdateID, `{"name":"nonsense","destination_event":"a"}`},
		{eventRulesGetID, `{"name":"` + ruleStream + `"}`},
		{eventRulesDeleteID, `{"name":"` + ruleStream + `/eventCreateRules/0"}`},
		{eventRulesListID, `{"parent":"properties/200"}`},
	} {
		if _, err := env.confirmed(c.id, "sfree", c.args); err == nil {
			t.Errorf("%s %s: accepted", c.id, c.args)
		}
	}
	env.noIO(t)
}

var ruleBindingTools = []struct{ id, arguments, method string }{
	{eventRulesListID, `{"parent":"PROP/dataStreams/2001"}`, "GET"},
	{eventRulesGetID, `{"name":"PROP/dataStreams/2001/eventCreateRules/3001"}`, "GET"},
	{eventRulesCreateID, `{"parent":"PROP/dataStreams/2001","destination_event":"a","event_conditions":[` +
		`{"field":"f","comparison_type":"EQUALS","value":"v"}]}`, "POST"},
	{eventRulesUpdateID, `{"name":"PROP/dataStreams/2001/eventCreateRules/3001","destination_event":"b"}`, "PATCH"},
	{eventRulesDeleteID, `{"name":"PROP/dataStreams/2001/eventCreateRules/3001"}`, "DELETE"},
}

func TestEventRulesFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range ruleBindingTools {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := ruleEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimPrefix(r.URL.Path, alphaPrefix)
					if r.Method == http.MethodPost {
						name += "/9"
					}
					body = `{"eventCreateRules":[],"name":"` + name + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			result, err := env.confirmed(tool.id, c.connection, strings.ReplaceAll(tool.arguments, "PROP", c.property))
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

func TestEventRulesErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	for _, c := range []struct {
		name   string
		api    func(int, *http.Request) (*http.Response, error)
		unsure bool
		class  provider.Class
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			false, provider.ClassPermission},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), false, provider.ClassNotFound},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), false, provider.ClassRateLimited},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), true, provider.ClassProviderError},
	} {
		for _, tool := range ruleBindingTools {
			t.Run(c.name+" "+tool.id, func(t *testing.T) {
				env := ruleEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(tool.id, "sfree", strings.ReplaceAll(tool.arguments, "PROP", "properties/200"))
				if err == nil || leaks(err.Error()) != "" || strings.Contains(err.Error(), detailCanary) {
					t.Fatalf("error = %v", err)
				}
				if classOf(err) != c.class {
					t.Errorf("class = %s, want %s", classOf(err), c.class)
				}
				write := tool.method != "GET"
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != (c.unsure && write) {
					t.Errorf("unknown outcome reported = %t: %v", got, err)
				}
				if requests, _ := env.fake.seen(); len(requests) != 1 {
					t.Errorf("requests = %d, want exactly one", len(requests))
				}
			})
		}
	}
}

func TestEventRules429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := ruleEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", ruleJSON), nil
	}})
	args := `{"name":"` + ruleOne + `"}`
	if _, err := env.call(eventRulesGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(eventRulesGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestEventRuleChangesDistrustAnAnswerForAnotherRule(t *testing.T) {
	env := ruleEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/dataStreams/1/eventCreateRules/1"}`)})
	for _, c := range []struct{ id, args string }{
		{eventRulesUpdateID, `{"name":"` + ruleOne + `","destination_event":"b"}`},
		{eventRulesCreateID, ruleBindingTools[2].arguments},
	} {
		_, err := env.confirmed(c.id, "sfree", strings.ReplaceAll(c.args, "PROP", "properties/200"))
		if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "unknown") {
			t.Errorf("%s: error = %v", c.id, err)
		}
	}
}

func TestEventRuleToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range ruleTools {
		descriptor, _, ok := reg.Lookup(id)
		if !ok {
			t.Fatalf("%s is not registered", id)
		}
		read := effectOf(id) == config.EffectRead
		wantScope := scopeEdit
		if read {
			wantScope = scopeReadOnly
		}
		if !slices.Equal(metadata.Token.Scopes[id], []string{wantScope}) ||
			slices.Contains(profiles["read"], id) != read || !slices.Contains(profiles["write"], id) ||
			descriptor.RequiresToolAllowList {
			t.Errorf("%s: scopes %v, read profile %t, listed-only %t", id, metadata.Token.Scopes[id],
				slices.Contains(profiles["read"], id), descriptor.RequiresToolAllowList)
		}
		if !read && (reg.ConfirmationSummary(id) == nil || !strings.Contains(descriptor.Description, "changes the events")) {
			t.Errorf("%s lacks a summary or the statement of its effect", id)
		}
	}
	for _, id := range []string{eventRulesCreateID, eventRulesUpdateID} {
		descriptor, _, _ := reg.Lookup(id)
		if !strings.Contains(descriptor.Description, "data collected from then on") {
			t.Errorf("%s does not say that it acts on future data", id)
		}
	}
	summary := reg.ConfirmationSummary(eventRulesUpdateID)(json.RawMessage(
		`{"name":"` + ruleOne + `","parameter_mutations":[],"destination_event":"b"}`))
	for _, want := range []string{"destination_event, parameter_mutations", ruleOne} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q lacks %q", summary, want)
		}
	}
}
