package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// confirmed invokes a changing tool with the confirmation of a person.
func (e *testEnv) confirmed(operation, connection, arguments string) (string, error) {
	response, err := e.core.Invoke(context.Background(), application.InvokeRequest{
		Operation: operation, Connection: connection, Arguments: json.RawMessage(arguments), Confirmed: true})
	return string(response.Result), err
}

const createdJSON = `{"name":"properties/300","propertyType":"PROPERTY_TYPE_ORDINARY","parent":"accounts/100",` +
	`"displayName":"Shop web","industryCategory":"SHOPPING","timeZone":"Europe/Zurich","currencyCode":"CHF",` +
	`"createTime":"2026-11-01T00:00:00Z","updateTime":"2026-11-01T00:00:00Z","account":"accounts/100"}`

const trashedJSON = `{"name":"properties/200","parent":"accounts/100","displayName":"Shop web",` +
	`"deleteTime":"2026-11-01T00:00:00Z","expireTime":"2026-12-06T00:00:00Z"}`

func (e *testEnv) noIO(t *testing.T) {
	t.Helper()
	if requests, grants := e.fake.seen(); len(requests) != 0 || grants != 0 || e.reads.Load() != 0 {
		t.Errorf("requests %d, grants %d, secret reads %d, want none", len(requests), grants, e.reads.Load())
	}
}

func TestExcludedMethodsHaveNoTool(t *testing.T) {
	reg := registry(t)
	var ids []string
	for _, d := range reg.Provider(Provider) {
		ids = append(ids, d.ID)
		id := strings.ToLower(d.ID)
		for _, excluded := range []string{"acknowledge", "rollup", "subproperty", "subproperties", "source-link"} {
			if strings.Contains(id, excluded) {
				t.Errorf("%s is a tool of an excluded method", d.ID)
			}
		}
	}
	slices.Sort(ids)
	if want := []string{accountsID, accountsGetID, accountsSharingID, accountsUpdateID, accountsDeleteID, summariesID,
		propertiesCreateID, propertiesDeleteID, propertiesGetID, propertiesListID,
		propertiesUpdateID, reportsRunID, metadataGetID, compatibilityID, dataStreamsListID, dataStreamsGetID,
		dataStreamsCreateID, dataStreamsUpdateID, funnelRunID, audienceCreateID, audienceGetID,
		audienceListID}; !slices.Equal(ids, sortedCopy(want)) {
		t.Errorf("tools = %v", ids)
	}
	// Only an ordinary property is created, so no tool can make a subproperty or a rollup.
	if !strings.Contains(string(propertiesCreate.InputSchema), `"enum":["PROPERTY_TYPE_ORDINARY"]`) {
		t.Error("properties.create does not pin the ordinary property type")
	}
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func TestWriteToolsAreConfirmedAndDeleteIsListedOnly(t *testing.T) {
	reg := registry(t)
	for id, c := range map[string]struct {
		effect string
		listed bool
	}{propertiesCreateID: {"create", false}, propertiesUpdateID: {"update", false}, propertiesDeleteID: {"delete", true}} {
		var found bool
		for _, d := range reg.Provider(Provider) {
			if d.ID != id {
				continue
			}
			found = true
			if string(d.Risk.Effect) != c.effect || d.Risk.Confirmation != "required" || !d.Risk.OpenWorld ||
				d.RequiresToolAllowList != c.listed {
				t.Errorf("%s = %+v, listed %t", id, d.Risk, d.RequiresToolAllowList)
			}
			if reg.ConfirmationSummary(id) == nil {
				t.Errorf("%s has no confirmation summary", id)
			}
		}
		if !found {
			t.Errorf("%s is not registered", id)
		}
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, propertiesDeleteID) {
			t.Errorf("profile %s holds properties.delete", profile.ID)
		}
	}
}

func TestIndustryCategoriesAreTheDocumentedEnumeration(t *testing.T) {
	var schema struct {
		Properties struct {
			Industry struct {
				Enum []string `json:"enum"`
			} `json:"industry_category"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(propertiesCreate.InputSchema, &schema); err != nil ||
		!reflect.DeepEqual(schema.Properties.Industry.Enum, industryCategories) || len(industryCategories) != 26 ||
		slices.Contains(industryCategories, "INDUSTRY_CATEGORY_UNSPECIFIED") {
		t.Errorf("schema enum %v, %v", schema.Properties.Industry.Enum, err)
	}
}

func TestFieldChecks(t *testing.T) {
	for _, zone := range []string{"Europe/Zurich", "UTC", "America/Argentina/Buenos_Aires", "Asia/Kolkata", "Etc/GMT+1"} {
		if err := checkTimeZone(zone); err != nil {
			t.Errorf("checkTimeZone(%q) = %v", zone, err)
		}
	}
	for _, zone := range []string{"", "Local", "Mars/Olympus", "europe/zurich", "europe/Zurich", "Europe/zurich",
		"Europe/Zurich/../x", "Europe Zurich", "../etc/passwd", "Pacific/", strings.Repeat("A", 65), "CET\n"} {
		if err := checkTimeZone(zone); !refusedInvalid(err) || (zone != "" && strings.Contains(err.Error(), zone)) {
			t.Errorf("checkTimeZone(%q) = %v", zone, err)
		}
	}
	for _, currency := range []string{"CHF", "USD", "JPY"} {
		if err := checkCurrency(currency); err != nil {
			t.Errorf("checkCurrency(%q) = %v", currency, err)
		}
	}
	for _, currency := range []string{"", "chf", "CH", "CHFF", "C1F", "CHF "} {
		if err := checkCurrency(currency); !refusedInvalid(err) {
			t.Errorf("checkCurrency(%q) = %v", currency, err)
		}
	}
	for _, name := range []string{"Shop", strings.Repeat("a", 100), strings.Repeat("ä", 100)} {
		if err := checkDisplayName(name); err != nil {
			t.Errorf("checkDisplayName(%d runes) = %v", len([]rune(name)), err)
		}
	}
	for _, name := range []string{"", "   ", strings.Repeat("a", 101), "a\x00b", "line\nbreak", "bad\xffutf",
		strings.Repeat("😀", 51)} {
		if err := checkDisplayName(name); !refusedInvalid(err) {
			t.Errorf("checkDisplayName(%q) = %v", name, err)
		}
	}
	for _, category := range []string{"", "NOPE", "shopping", "INDUSTRY_CATEGORY_UNSPECIFIED"} {
		if err := checkIndustry(category); !refusedInvalid(err) {
			t.Errorf("checkIndustry(%q) = %v", category, err)
		}
	}
}

func TestPropertiesCreateSendsEveryFieldOnceWithTheConfirmation(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, createdJSON)})
	args := `{"parent":"accounts/100","display_name":"Shop web","time_zone":"Europe/Zurich","currency_code":"CHF",` +
		`"industry_category":"SHOPPING","property_type":"PROPERTY_TYPE_ORDINARY"}`
	if _, err := env.call(propertiesCreateID, "write", args); err == nil {
		t.Fatal("create without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(propertiesCreateID, "write", args)
	if err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].method != http.MethodPost || requests[0].path != "/v1beta/properties" ||
		requests[0].rawQuery != "" || requests[0].contentType != "application/json" ||
		requests[0].auth != "Bearer "+accessPrefix+"1" {
		t.Fatalf("requests = %+v", requests)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(requests[0].body), &body); err != nil || !reflect.DeepEqual(body, map[string]string{
		"parent": "accounts/100", "displayName": "Shop web", "timeZone": "Europe/Zurich", "currencyCode": "CHF",
		"industryCategory": "SHOPPING", "propertyType": "PROPERTY_TYPE_ORDINARY"}) {
		t.Errorf("body = %s, %v", requests[0].body, err)
	}
	var got, want property
	_ = json.Unmarshal([]byte(createdJSON), &want)
	if json.Unmarshal([]byte(result), &got) != nil || got != want {
		t.Errorf("result = %s", result)
	}
	// Only the required members: the type defaults to the ordinary property.
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, createdJSON)})
	if _, err := env.confirmed(propertiesCreateID, "write",
		`{"parent":"accounts/100","display_name":"Shop web","time_zone":"UTC"}`); err != nil {
		t.Fatal(err)
	}
	requests, _ = env.fake.seen()
	if requests[0].body != `{"parent":"accounts/100","displayName":"Shop web","timeZone":"UTC",`+
		`"propertyType":"PROPERTY_TYPE_ORDINARY"}` {
		t.Errorf("body = %s", requests[0].body)
	}
}

func TestPropertiesCreateRefusesBadArgumentsBeforeAnyIO(t *testing.T) {
	ok := func(extra string) string {
		return `{"parent":"accounts/100","display_name":"Shop","time_zone":"UTC"` + extra + `}`
	}
	env := newEnv(t, &fakeGoogle{})
	for name, args := range map[string]string{
		"no parent":        `{"display_name":"Shop","time_zone":"UTC"}`,
		"no display name":  `{"parent":"accounts/100","time_zone":"UTC"}`,
		"no time zone":     `{"parent":"accounts/100","display_name":"Shop"}`,
		"property parent":  `{"parent":"properties/100","display_name":"Shop","time_zone":"UTC"}`,
		"bad parent":       `{"parent":"accounts/01","display_name":"Shop","time_zone":"UTC"}`,
		"unknown zone":     ok(`,"time_zone":"Mars/Olympus"`),
		"local zone":       ok(`,"time_zone":"Local"`),
		"lower currency":   ok(`,"currency_code":"chf"`),
		"long currency":    ok(`,"currency_code":"CHFF"`),
		"unknown industry": ok(`,"industry_category":"MINING"`),
		"unspecified":      ok(`,"industry_category":"INDUSTRY_CATEGORY_UNSPECIFIED"`),
		"subproperty":      ok(`,"property_type":"PROPERTY_TYPE_SUBPROPERTY"`),
		"rollup":           ok(`,"property_type":"PROPERTY_TYPE_ROLLUP"`),
		"long name":        ok(`,"display_name":"` + strings.Repeat("x", 101) + `"`),
		"extra member":     ok(`,"service_level":"GOOGLE_ANALYTICS_360"`),
	} {
		if _, err := env.confirmed(propertiesCreateID, "write", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The handler checks the same rules itself.
	for name, args := range map[string]string{
		"unknown zone": ok(`,"time_zone":"Mars/Olympus"`), "currency": ok(`,"currency_code":"chf"`),
		"industry": ok(`,"industry_category":"MINING"`), "type": ok(`,"property_type":"PROPERTY_TYPE_ROLLUP"`),
		"no zone": `{"parent":"accounts/100","display_name":"Shop"}`,
	} {
		_, err := handlePropertiesCreate(context.Background(), env.resolved(t, "write"), env.secrets(), env.red,
			json.RawMessage(args))
		if !refusedInvalid(err) || strings.Contains(err.Error(), "Mars") || strings.Contains(err.Error(), "MINING") {
			t.Errorf("%s: %v", name, err)
		}
	}
	env.noIO(t)
}

func TestPropertiesCreateFollowsTheAccountBinding(t *testing.T) {
	for _, c := range []struct {
		connection, parent string
		refused            bool
	}{
		{"wfree", "accounts/900", false}, {"waccount", "accounts/100", false}, {"waccount", "accounts/900", true},
		{"wproperty", "accounts/100", true}, {"wmixed", "accounts/100", false}, {"wmixed", "accounts/900", true},
	} {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, createdJSON)})
		_, err := env.confirmed(propertiesCreateID, c.connection,
			`{"parent":"`+c.parent+`","display_name":"Shop","time_zone":"UTC"}`)
		requests, _ := env.fake.seen()
		if (err != nil) != c.refused || len(requests) != map[bool]int{true: 0, false: 1}[c.refused] ||
			(c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "900") || env.reads.Load() != 0)) {
			t.Errorf("%s %s: %v, requests %d", c.connection, c.parent, err, len(requests))
		}
	}
}

func TestPropertiesUpdateSendsTheMaskOfTheSetArgumentsOnly(t *testing.T) {
	for _, c := range []struct{ args, mask, body string }{
		{`{"property":"properties/200","display_name":"New"}`, "display_name", `{"displayName":"New"}`},
		{`{"property":"properties/200","currency_code":"EUR"}`, "currency_code", `{"currencyCode":"EUR"}`},
		{`{"property":"properties/200","time_zone":"Asia/Tokyo","industry_category":"NEWS"}`,
			"industry_category,time_zone", `{"industryCategory":"NEWS","timeZone":"Asia/Tokyo"}`},
		{`{"property":"properties/200","display_name":"N","industry_category":"GAMES","time_zone":"UTC",` +
			`"currency_code":"CHF"}`, "display_name,industry_category,time_zone,currency_code",
			`{"currencyCode":"CHF","displayName":"N","industryCategory":"GAMES","timeZone":"UTC"}`},
	} {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, propertyJSON)})
		if _, err := env.call(propertiesUpdateID, "wfree", c.args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		result, err := env.confirmed(propertiesUpdateID, "wfree", c.args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1beta/properties/200" || requests[0].rawQuery != "updateMask="+strings.ReplaceAll(c.mask, ",", "%2C") ||
			requests[0].contentType != "application/json" || requests[0].body != c.body {
			t.Errorf("%s: %v, %+v", c.args, err, requests)
		}
		var got, want property
		_ = json.Unmarshal([]byte(propertyJSON), &want)
		if json.Unmarshal([]byte(result), &got) != nil || got != want {
			t.Errorf("result = %s", result)
		}
	}
}

func TestPropertiesUpdateRefusesBeforeAnyIO(t *testing.T) {
	env := newEnv(t, &fakeGoogle{})
	for name, args := range map[string]string{
		"no field":       `{"property":"properties/200"}`,
		"no property":    `{"display_name":"New"}`,
		"account":        `{"property":"accounts/100","display_name":"New"}`,
		"empty name":     `{"property":"properties/200","display_name":""}`,
		"bad zone":       `{"property":"properties/200","time_zone":"Mars/Olympus"}`,
		"bad currency":   `{"property":"properties/200","currency_code":"eur"}`,
		"bad industry":   `{"property":"properties/200","industry_category":"MINING"}`,
		"property type":  `{"property":"properties/200","property_type":"PROPERTY_TYPE_ORDINARY","display_name":"N"}`,
		"unbound parent": `{"property":"properties/200","parent":"accounts/1","display_name":"N"}`,
	} {
		if _, err := env.confirmed(propertiesUpdateID, "wfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, args := range map[string]string{
		"no field": `{"property":"properties/200"}`, "empty": `{"property":"properties/200","display_name":""}`,
		"zone": `{"property":"properties/200","time_zone":"Mars/Olympus"}`,
	} {
		_, err := handlePropertiesUpdate(context.Background(), env.resolved(t, "wfree"), env.secrets(), env.red,
			json.RawMessage(args))
		if !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	env.noIO(t)
}

// bindingCase is one call on a bound connection: the requests it may make and whether it is refused.
type bindingCase struct {
	connection, property string
	methods              []string
	refused              bool
}

var bindingCases = []bindingCase{
	{"wfree", "properties/200", []string{"PATCH"}, false},
	{"wproperty", "properties/200", []string{"PATCH"}, false},
	{"wproperty", "properties/201", nil, true},
	{"wproperty", "properties/901", nil, true},
	{"waccount", "properties/201", []string{"GET", "PATCH"}, false},
	{"waccount", "properties/901", []string{"GET"}, true},
	{"wmixed", "properties/901", []string{"PATCH"}, false},
	{"wmixed", "properties/200", []string{"GET", "PATCH"}, false},
}

func bindingFake() *fakeGoogle {
	answers := map[string]string{
		"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
		"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
		"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
	}
	return &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", answers[r.URL.Path]), nil
	}}
}

func methodsOf(requests []recorded) []string {
	var out []string
	for _, r := range requests {
		out = append(out, r.method)
	}
	return out
}

func TestPropertiesUpdateAndDeleteFollowTheBindingWithAProofRead(t *testing.T) {
	for _, tool := range []struct{ id, arguments, change string }{
		{propertiesUpdateID, `,"display_name":"New"`, "PATCH"}, {propertiesDeleteID, ``, "DELETE"},
	} {
		for _, c := range bindingCases {
			env := newEnv(t, bindingFake())
			result, err := env.confirmed(tool.id, c.connection, `{"property":"`+c.property+`"`+tool.arguments+`}`)
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "PATCH"); i >= 0 {
				want[i] = tool.change
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, c.connection, c.property, err,
					methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, c.connection, c.property, err)
			}
		}
	}
}

func TestPropertiesDeleteIsUnreachableWithoutAToolsEntryAndNeedsConfirm(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, trashedJSON)})
	// The connection offers every effect, yet a listed-only tool needs its entry in tools.
	if _, err := env.confirmed(propertiesDeleteID, "write", `{"property":"properties/200"}`); err == nil {
		t.Fatal("delete was reachable through effects")
	}
	env.noIO(t)
	if _, err := env.call(propertiesDeleteID, "wfree", `{"property":"properties/200"}`); err == nil {
		t.Fatal("delete without confirm was accepted")
	}
	env.noIO(t)
	for _, args := range []string{`{}`, `{"property":"accounts/100"}`, `{"property":"properties/0"}`,
		`{"property":"properties/200","force":true}`} {
		if _, err := env.confirmed(propertiesDeleteID, "wfree", args); err == nil {
			t.Errorf("%s accepted", args)
		}
	}
	env.noIO(t)
	result, err := env.confirmed(propertiesDeleteID, "wfree", `{"property":"properties/200"}`)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete ||
		requests[0].path != "/v1beta/properties/200" || requests[0].rawQuery != "" || requests[0].body != "" ||
		requests[0].contentType != "" {
		t.Fatalf("%v, %+v", err, requests)
	}
	var got property
	if json.Unmarshal([]byte(result), &got) != nil || got.DeleteTime != "2026-11-01T00:00:00Z" ||
		got.ExpireTime != "2026-12-06T00:00:00Z" {
		t.Errorf("result = %s", result)
	}
}

func TestPropertyChangesClassifyFailuresWithoutProviderTextAndNeverRepeat(t *testing.T) {
	tools := []struct{ id, arguments string }{
		{propertiesCreateID, `{"parent":"accounts/100","display_name":"Shop","time_zone":"UTC"}`},
		{propertiesUpdateID, `{"property":"properties/200","display_name":"New"}`},
		{propertiesDeleteID, `{"property":"properties/200"}`},
	}
	for _, c := range []struct {
		name   string
		api    func(int, *http.Request) (*http.Response, error)
		class  provider.Class
		unsure bool
	}{
		{"401", always(http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED", "")), provider.ClassAuth, false},
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission, false},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, false},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")),
			provider.ClassRateLimited, false},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, true},
		{"503", always(http.StatusServiceUnavailable, errorObject(503, "UNAVAILABLE", "")), provider.ClassUnreachable, true},
		{"not json", always(http.StatusOK, `{"name":`), provider.ClassInvalidResponse, true},
		{"html", func(_ int, r *http.Request) (*http.Response, error) {
			return respond(r, http.StatusOK, "text/html", "<html>"+detailCanary+"</html>"), nil
		}, provider.ClassInvalidResponse, true},
		{"unreadable body", func(_ int, r *http.Request) (*http.Response, error) {
			resp := respond(r, http.StatusOK, "application/json", "")
			resp.Body = failingBody{}
			return resp, nil
		}, provider.ClassInvalidResponse, true},
		{"timeout", func(int, *http.Request) (*http.Response, error) { return nil, timeoutError{} },
			provider.ClassTimeout, true},
	} {
		for _, tool := range tools {
			t.Run(c.name+" "+tool.id, func(t *testing.T) {
				env := newEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(tool.id, "wfree", tool.arguments)
				if classOf(err) != c.class || leaks(err.Error()) != "" {
					t.Fatalf("error = %v", err)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != c.unsure {
					t.Errorf("unknown outcome reported = %t, want %t: %v", got, c.unsure, err)
				}
				requests, _ := env.fake.seen()
				changes := 0
				for _, r := range requests {
					if r.method != http.MethodGet {
						changes++
					}
				}
				if changes != 1 {
					t.Errorf("changing requests = %d, want exactly one", changes)
				}
			})
		}
	}
}

func TestRateLimitedChangeHoldsTheNextRequestWithoutRetryAfter(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", trashedJSON), nil
	}})
	if _, err := env.confirmed(propertiesDeleteID, "wfree", `{"property":"properties/200"}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.confirmed(propertiesDeleteID, "wfree", `{"property":"properties/200"}`); err != nil {
		t.Fatal(err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900*time.Millisecond {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestAnAnswerWithAnotherPropertyIsNotTrusted(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999"}`)})
	_, err := env.confirmed(propertiesUpdateID, "wfree", `{"property":"properties/200","display_name":"New"}`)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	_, err = env.confirmed(propertiesDeleteID, "wfree", `{"property":"properties/200"}`)
	if classOf(err) != provider.ClassProviderError {
		t.Errorf("delete: %v", err)
	}
	env = newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"displayName":"S"}`)})
	_, err = env.confirmed(propertiesCreateID, "wfree", `{"parent":"accounts/100","display_name":"S","time_zone":"UTC"}`)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("create: %v", err)
	}
}

func TestConfirmationSummariesNameTheEffect(t *testing.T) {
	reg := registry(t)
	create := reg.ConfirmationSummary(propertiesCreateID)(json.RawMessage(
		`{"parent":"accounts/100","display_name":"Shop","time_zone":"UTC","currency_code":"CHF"}`))
	update := reg.ConfirmationSummary(propertiesUpdateID)(json.RawMessage(
		`{"property":"properties/200","time_zone":"UTC","display_name":"X"}`))
	del := reg.ConfirmationSummary(propertiesDeleteID)(json.RawMessage(`{"property":"properties/200"}`))
	for got, wants := range map[string][]string{
		create: {"Create", "Shop", "accounts/100", "UTC", "CHF"},
		update: {"display_name, time_zone", "properties/200"},
		del:    {"trash", "properties/200", "35 days"},
	} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}
