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

// dimensionEnv adds connections that list the five custom dimension tools, with the bindings of the stream
// tests (streamBindingCases).
func dimensionEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{customDimensionsListID, customDimensionsGetID, customDimensionsCreateID,
		customDimensionsUpdateID, customDimensionsArchiveID}
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
	eventDimensionJSON = `{"name":"properties/200/customDimensions/3001","parameterName":"plan_tier",` +
		`"displayName":"Plan tier","description":"Plan of the account","scope":"EVENT","extra":1}`
	userDimensionJSON = `{"name":"properties/200/customDimensions/3002","parameterName":"member_level",` +
		`"displayName":"Member level","scope":"USER","disallowAdsPersonalization":true}`
	itemDimensionJSON = `{"name":"properties/200/customDimensions/3003","parameterName":"item_color",` +
		`"displayName":"Item color","scope":"ITEM"}`
)

type dimensionsOut struct {
	CustomDimensions []customDimension `json:"customDimensions"`
	NextCursor       string            `json:"next_cursor"`
	HasMore          bool              `json:"has_more"`
}

func TestCustomDimensionsListPagesThroughThreePages(t *testing.T) {
	pages := []string{
		`{"customDimensions":[` + eventDimensionJSON + `],"nextPageToken":"t2"}`,
		`{"customDimensions":[` + userDimensionJSON + `],"nextPageToken":"t3"}`,
		`{"customDimensions":[` + itemDimensionJSON + `]}`,
	}
	env := dimensionEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(customDimensionsListID, "sfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		var out dimensionsOut
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.CustomDimensions) != 1 ||
			out.HasMore != (i < 2) {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.CustomDimensions[0].Scope))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"EVENT", "USER", "ITEM"}) || len(requests) != 3 ||
		requests[0].path != "/v1beta/properties/200/customDimensions" || requests[0].rawQuery != "pageSize=1" ||
		!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	if _, err := env.call(customDimensionsListID, "sfree", `{"property":"properties/201","cursor":"`+cursor+`"}`); err == nil && cursor != "" {
		t.Error("a cursor of another property was accepted")
	}
}

func TestCustomDimensionsReadAllScopesAndMembers(t *testing.T) {
	answers := map[string]string{"3001": eventDimensionJSON, "3002": userDimensionJSON, "3003": itemDimensionJSON}
	env := dimensionEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", answers[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]), nil
	}})
	var got [3]customDimension
	for i, id := range []string{"3001", "3002", "3003"} {
		result, err := env.call(customDimensionsGetID, "sfree", `{"name":"properties/200/customDimensions/`+id+`"}`)
		if err != nil || json.Unmarshal([]byte(result), &got[i]) != nil || strings.Contains(result, "extra") {
			t.Fatalf("%s: %v %s", id, err, result)
		}
	}
	event, user, item := got[0], got[1], got[2]
	if event.Scope != "EVENT" || event.ParameterName != "plan_tier" || event.DisplayName != "Plan tier" ||
		event.Description != "Plan of the account" || event.DisallowAdsPersonalization != nil ||
		user.Scope != "USER" || user.DisallowAdsPersonalization == nil || !*user.DisallowAdsPersonalization ||
		item.Scope != "ITEM" || item.Description != "" {
		t.Errorf("dimensions = %+v %+v %+v", event, user, item)
	}
}

func TestCustomDimensionsAnswerIsBoundedAndChecked(t *testing.T) {
	env := dimensionEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"customDimensions":[`+
		`{"name":"properties/200/customDimensions/1","displayName":"`+strings.Repeat("n", 400)+`"},`+
		`{"name":"properties/999/customDimensions/2"},{"name":"nonsense"},{"displayName":"unnamed"}]}`)})
	result, err := env.call(customDimensionsListID, "sfree", `{"property":"properties/200"}`)
	var out dimensionsOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.CustomDimensions) != 1 ||
		len([]rune(string(out.CustomDimensions[0].DisplayName))) != maxTextRunes+1 {
		t.Errorf("result %v, %v", len(result), err)
	}
	env = dimensionEnv(t, &fakeGoogle{api: always(http.StatusOK, eventDimensionJSON)})
	if _, err := env.call(customDimensionsGetID, "sfree", `{"name":"properties/200/customDimensions/9"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

func TestCustomDimensionsCreateForEachScope(t *testing.T) {
	for _, c := range []struct{ args, answer, body string }{
		{`{"property":"properties/200","parameter_name":"plan_tier","display_name":"Plan tier","description":"Plan","scope":"EVENT"}`,
			eventDimensionJSON, `{"parameterName":"plan_tier","displayName":"Plan tier","description":"Plan","scope":"EVENT"}`},
		{`{"property":"properties/200","parameter_name":"member_level","display_name":"Member level","scope":"USER","disallow_ads_personalization":true}`,
			userDimensionJSON, `{"parameterName":"member_level","displayName":"Member level","scope":"USER","disallowAdsPersonalization":true}`},
		{`{"property":"properties/200","parameter_name":"item_color","display_name":"Item color","scope":"ITEM","disallow_ads_personalization":false}`,
			itemDimensionJSON, `{"parameterName":"item_color","displayName":"Item color","scope":"ITEM","disallowAdsPersonalization":false}`},
	} {
		env := dimensionEnv(t, &fakeGoogle{api: always(http.StatusOK, c.answer)})
		if _, err := env.call(customDimensionsCreateID, "sfree", c.args); err == nil {
			t.Fatal("create without confirm was accepted")
		}
		env.noIO(t)
		result, err := env.confirmed(customDimensionsCreateID, "sfree", c.args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
			requests[0].path != "/v1beta/properties/200/customDimensions" || requests[0].rawQuery != "" ||
			requests[0].body != c.body || requests[0].contentType != "application/json" ||
			!strings.Contains(result, "parameterName") {
			t.Fatalf("%v, %+v, %s", err, requests, result)
		}
	}
}

func TestCustomDimensionsCreateRefusesMissingOrInvalidValues(t *testing.T) {
	env := dimensionEnv(t, &fakeGoogle{api: always(http.StatusOK, eventDimensionJSON)})
	base := `{"property":"properties/200","parameter_name":"p","display_name":"D","scope":"EVENT"`
	for name, args := range map[string]string{
		"no parameter name":     `{"property":"properties/200","display_name":"D","scope":"EVENT"}`,
		"no display name":       `{"property":"properties/200","parameter_name":"p","scope":"EVENT"}`,
		"no scope":              `{"property":"properties/200","parameter_name":"p","display_name":"D"}`,
		"no property":           `{"parameter_name":"p","display_name":"D","scope":"EVENT"}`,
		"unspecified scope":     `{"property":"properties/200","parameter_name":"p","display_name":"D","scope":"DIMENSION_SCOPE_UNSPECIFIED"}`,
		"bad property":          `{"property":"accounts/100","parameter_name":"p","display_name":"D","scope":"EVENT"}`,
		"parameter with dash":   `{"property":"properties/200","parameter_name":"a-b","display_name":"D","scope":"EVENT"}`,
		"parameter with digit":  `{"property":"properties/200","parameter_name":"1a","display_name":"D","scope":"EVENT"}`,
		"user parameter long":   `{"property":"properties/200","parameter_name":"` + strings.Repeat("a", 25) + `","display_name":"D","scope":"USER"}`,
		"event parameter long":  `{"property":"properties/200","parameter_name":"` + strings.Repeat("a", 41) + `","display_name":"D","scope":"EVENT"}`,
		"blank display name":    `{"property":"properties/200","parameter_name":"p","display_name":"  ","scope":"EVENT"}`,
		"long display name":     `{"property":"properties/200","parameter_name":"p","display_name":"` + strings.Repeat("n", 83) + `","scope":"EVENT"}`,
		"long description":      base + `,"description":"` + strings.Repeat("d", 151) + `"}`,
		"control in desc":       base + `,"description":"a\u0007b"}`,
		"ads on event scope":    base + `,"disallow_ads_personalization":true}`,
		"unknown argument":      base + `,"type":"x"}`,
		"description not text":  base + `,"description":5}`,
		"display name too long": strings.Replace(base, `"D"`, `"`+strings.Repeat("n", 83)+`"`, 1) + `}`,
	} {
		if _, err := env.confirmed(customDimensionsCreateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
	for _, args := range []string{base + `}`,
		`{"property":"properties/200","parameter_name":"` + strings.Repeat("a", 24) + `","display_name":"D","scope":"USER"}`,
		`{"property":"properties/200","parameter_name":"` + strings.Repeat("a", 40) + `","display_name":"D","scope":"ITEM"}`} {
		if _, err := env.confirmed(customDimensionsCreateID, "sfree", args); err != nil {
			t.Errorf("valid create refused: %v", err)
		}
	}
}

func TestCustomDimensionsUpdateMaskComesFromSetArguments(t *testing.T) {
	for _, c := range []struct{ args, mask, body string }{
		{`,"display_name":"New"`, "display_name", `{"displayName":"New"}`},
		{`,"description":"Text"`, "description", `{"description":"Text"}`},
		{`,"description":""`, "description", `{"description":""}`},
		{`,"disallow_ads_personalization":false`, "disallow_ads_personalization", `{"disallowAdsPersonalization":false}`},
		{`,"disallow_ads_personalization":true,"display_name":"New","description":"Text"`,
			"display_name,description,disallow_ads_personalization",
			`{"displayName":"New","description":"Text","disallowAdsPersonalization":true}`},
	} {
		env := dimensionEnv(t, &fakeGoogle{api: always(http.StatusOK, eventDimensionJSON)})
		args := `{"name":"properties/200/customDimensions/3001"` + c.args + `}`
		if _, err := env.call(customDimensionsUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(customDimensionsUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1beta/properties/200/customDimensions/3001" ||
			requests[0].rawQuery != "updateMask="+strings.NewReplacer(",", "%2C").Replace(c.mask) ||
			requests[0].body != c.body {
			t.Fatalf("%v, %+v", err, requests)
		}
	}
	env := dimensionEnv(t, &fakeGoogle{api: always(http.StatusOK, eventDimensionJSON)})
	name := `{"name":"properties/200/customDimensions/3001"`
	for label, args := range map[string]string{
		"no field":       name + `}`,
		"empty display":  name + `,"display_name":""}`,
		"parameter name": name + `,"parameter_name":"other"}`,
		"scope":          name + `,"scope":"USER"}`,
		"bad name":       `{"name":"properties/200","display_name":"x"}`,
		"stream name":    `{"name":"properties/200/dataStreams/1","display_name":"x"}`,
		"long display":   name + `,"display_name":"` + strings.Repeat("n", 83) + `"}`,
		"long desc":      name + `,"description":"` + strings.Repeat("d", 151) + `"}`,
	} {
		if _, err := env.confirmed(customDimensionsUpdateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
	env.noIO(t)
}

func TestCustomDimensionsArchive(t *testing.T) {
	env := dimensionEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"properties/200/customDimensions/3001"}`
	if _, err := env.call(customDimensionsArchiveID, "sfree", args); err == nil {
		t.Fatal("archive without confirm was accepted")
	}
	env.noIO(t)
	for _, bad := range []string{`{}`, `{"name":"properties/200"}`, `{"name":"properties/200/customDimensions/0"}`,
		`{"name":"properties/200/customDimensions/3001","extra":1}`} {
		if _, err := env.confirmed(customDimensionsArchiveID, "sfree", bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	env.noIO(t)
	result, err := env.confirmed(customDimensionsArchiveID, "sfree", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != "/v1beta/properties/200/customDimensions/3001:archive" || requests[0].rawQuery != "" ||
		requests[0].body != `{}` || result != `{"archived":true,"name":"properties/200/customDimensions/3001"}` {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func TestCustomDimensionsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{customDimensionsListID, `{"property":"PROP"}`, "GET"},
		{customDimensionsGetID, `{"name":"PROP/customDimensions/3001"}`, "GET"},
		{customDimensionsCreateID, `{"property":"PROP","parameter_name":"p","display_name":"D","scope":"EVENT"}`, "POST"},
		{customDimensionsUpdateID, `{"name":"PROP/customDimensions/3001","display_name":"S"}`, "PATCH"},
		{customDimensionsArchiveID, `{"name":"PROP/customDimensions/3001"}`, "POST"},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := dimensionEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1beta/"), ":archive")
					if r.Method == http.MethodPost && !strings.HasSuffix(r.URL.Path, ":archive") {
						name += "/9"
					}
					body = `{"customDimensions":[],"name":"` + name + `"}`
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

func TestCustomDimensionErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct {
		id, arguments string
		write         bool
	}{
		{customDimensionsListID, `{"property":"properties/200"}`, false},
		{customDimensionsGetID, `{"name":"properties/200/customDimensions/3001"}`, false},
		{customDimensionsCreateID, `{"property":"properties/200","parameter_name":"p","display_name":"D","scope":"EVENT"}`, true},
		{customDimensionsUpdateID, `{"name":"properties/200/customDimensions/3001","display_name":"S"}`, true},
		{customDimensionsArchiveID, `{"name":"properties/200/customDimensions/3001"}`, true},
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
				env := dimensionEnv(t, &fakeGoogle{api: c.api})
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

func TestCustomDimension429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := dimensionEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", eventDimensionJSON), nil
	}})
	args := `{"name":"properties/200/customDimensions/3001"}`
	if _, err := env.call(customDimensionsGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(customDimensionsGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestCustomDimensionWritesDistrustAnAnswerOfAnotherDimension(t *testing.T) {
	env := dimensionEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/customDimensions/1"}`)})
	if _, err := env.confirmed(customDimensionsUpdateID, "sfree", `{"name":"properties/200/customDimensions/3001","display_name":"S"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	if _, err := env.confirmed(customDimensionsCreateID, "sfree", `{"property":"properties/200","parameter_name":"p","display_name":"D","scope":"EVENT"}`); classOf(err) != provider.ClassProviderError {
		t.Errorf("create: %v", err)
	}
}

func TestCustomDimensionToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, want := range map[string]string{customDimensionsListID: scopeReadOnly, customDimensionsGetID: scopeReadOnly,
		customDimensionsCreateID: scopeEdit, customDimensionsUpdateID: scopeEdit, customDimensionsArchiveID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
	}
	profile := func(id string) []string {
		for _, p := range metadata.Profiles {
			if p.ID == id {
				return p.Tools
			}
		}
		return nil
	}
	for _, id := range []string{customDimensionsListID, customDimensionsGetID} {
		if !slices.Contains(profile("read"), id) || !slices.Contains(profile("write"), id) {
			t.Errorf("%s is not in read and write", id)
		}
	}
	for _, id := range []string{customDimensionsCreateID, customDimensionsUpdateID, customDimensionsArchiveID} {
		if slices.Contains(profile("read"), id) ||
			slices.Contains(profile("write"), id) == (id == customDimensionsArchiveID) {
			t.Errorf("%s profile placement is wrong", id)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
	summary := func(id, args string) string { return reg.ConfirmationSummary(id)(json.RawMessage(args)) }
	for got, wants := range map[string][]string{
		summary(customDimensionsCreateID, `{"property":"properties/200","parameter_name":"p","display_name":"Shop","scope":"USER"}`): {"Create", "USER", "Shop", "properties/200", "parameter p"},
		summary(customDimensionsUpdateID, `{"name":"properties/200/customDimensions/3001","description":"x","display_name":"X"}`):    {"display_name, description", "properties/200/customDimensions/3001"},
		summary(customDimensionsArchiveID, `{"name":"properties/200/customDimensions/3001"}`):                                        {"Archive", "properties/200/customDimensions/3001"},
	} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}
