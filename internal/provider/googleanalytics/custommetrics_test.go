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

var metricTools = []string{customMetricsListID, customMetricsGetID, customMetricsCreateID, customMetricsUpdateID,
	customMetricsArchiveID}

// metricEnv adds connections that list the five metric tools, with the bindings of the property tests.
func metricEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: metricTools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	eventMetricJSON = `{"name":"properties/200/customMetrics/3001","parameterName":"load_time","displayName":"Load time",` +
		`"description":"Page load","measurementUnit":"MILLISECONDS","scope":"EVENT","extra":1}`
	currencyMetricJSON = `{"name":"properties/200/customMetrics/3002","parameterName":"margin","displayName":"Margin",` +
		`"measurementUnit":"CURRENCY","scope":"EVENT","restrictedMetricType":["COST_DATA","REVENUE_DATA"]}`
)

type metricsOut struct {
	CustomMetrics []customMetric `json:"customMetrics"`
	NextCursor    string         `json:"next_cursor"`
	HasMore       bool           `json:"has_more"`
}

func TestCustomMetricsListPagesThroughThreePages(t *testing.T) {
	pages := []string{
		`{"customMetrics":[` + eventMetricJSON + `],"nextPageToken":"t2"}`,
		`{"customMetrics":[` + currencyMetricJSON + `],"nextPageToken":"t3"}`,
		`{"customMetrics":[{"name":"properties/200/customMetrics/3003","parameterName":"p3"}]}`,
	}
	env := metricEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(customMetricsListID, "sfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		var out metricsOut
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.CustomMetrics) != 1 ||
			out.HasMore != (i < 2) || strings.Contains(result, "extra") {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.CustomMetrics[0].ParameterName))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"load_time", "margin", "p3"}) || len(requests) != 3 ||
		requests[0].path != "/v1beta/properties/200/customMetrics" || requests[0].rawQuery != "pageSize=1" ||
		!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	if _, err := env.call(customMetricsListID, "sfree", `{"property":"properties/201","cursor":"x"}`); err == nil {
		t.Error("a foreign cursor was accepted")
	}
}

func TestCustomMetricsGetReturnsEveryMemberAndChecksTheName(t *testing.T) {
	env := metricEnv(t, &fakeGoogle{api: always(http.StatusOK, currencyMetricJSON)})
	result, err := env.call(customMetricsGetID, "sfree", `{"name":"properties/200/customMetrics/3002"}`)
	var got customMetric
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || got.ParameterName != "margin" ||
		got.DisplayName != "Margin" || got.MeasurementUnit != "CURRENCY" || got.Scope != "EVENT" ||
		!slices.Equal(got.RestrictedMetricType, []code{"COST_DATA", "REVENUE_DATA"}) {
		t.Fatalf("%v %s", err, result)
	}
	if _, err := env.call(customMetricsGetID, "sfree", `{"name":"properties/200/customMetrics/9"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("another metric: %v", err)
	}
	long := strings.Repeat("n", 400)
	env = metricEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"customMetrics":[{"name":"properties/200/customMetrics/1","displayName":"`+
		long+`"},{"name":"properties/999/customMetrics/2"},{"name":"junk"}]}`)})
	result, err = env.call(customMetricsListID, "sfree", `{"property":"properties/200"}`)
	var out metricsOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.CustomMetrics) != 1 ||
		len([]rune(string(out.CustomMetrics[0].DisplayName))) != maxTextRunes+1 {
		t.Errorf("bounded list: %v %s", err, result)
	}
}

func TestCustomMetricsCreateWithEveryUnitAndRestrictedType(t *testing.T) {
	for _, unit := range measurementUnits {
		args := `{"property":"properties/200","parameter_name":"p_1","display_name":"Metric 1","measurement_unit":"` +
			unit + `","scope":"EVENT"`
		want := `{"parameterName":"p_1","displayName":"Metric 1","measurementUnit":"` + unit + `","scope":"EVENT"}`
		if unit == metricCurrency {
			args += `,"restricted_metric_type":["COST_DATA","REVENUE_DATA"]`
			want = `{"parameterName":"p_1","displayName":"Metric 1","measurementUnit":"CURRENCY","scope":"EVENT",` +
				`"restrictedMetricType":["COST_DATA","REVENUE_DATA"]}`
		}
		args += `,"description":"d"}`
		want = strings.Replace(want, `"measurementUnit"`, `"description":"d","measurementUnit"`, 1)
		env := metricEnv(t, &fakeGoogle{api: always(http.StatusOK, eventMetricJSON)})
		if _, err := env.call(customMetricsCreateID, "sfree", args); err == nil {
			t.Fatal("create without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(customMetricsCreateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
			requests[0].path != "/v1beta/properties/200/customMetrics" || requests[0].rawQuery != "" ||
			requests[0].body != want || requests[0].contentType != "application/json" {
			t.Fatalf("%s: %v, %+v", unit, err, requests)
		}
	}
	for _, kind := range []string{`["COST_DATA"]`, `["REVENUE_DATA"]`} {
		env := metricEnv(t, &fakeGoogle{api: always(http.StatusOK, currencyMetricJSON)})
		if _, err := env.confirmed(customMetricsCreateID, "sfree", `{"property":"properties/200","parameter_name":"m",`+
			`"display_name":"M","measurement_unit":"CURRENCY","scope":"EVENT","restricted_metric_type":`+kind+`}`); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}

func TestCustomMetricsCreateRefusesBadInputBeforeAnyIO(t *testing.T) {
	env := metricEnv(t, &fakeGoogle{api: always(http.StatusOK, eventMetricJSON)})
	base := `"property":"properties/200","parameter_name":"p","display_name":"D","measurement_unit":"STANDARD","scope":"EVENT"`
	for name, args := range map[string]string{
		"no parameter":          `{"property":"properties/200","display_name":"D","measurement_unit":"STANDARD","scope":"EVENT"}`,
		"no display":            `{"property":"properties/200","parameter_name":"p","measurement_unit":"STANDARD","scope":"EVENT"}`,
		"no unit":               `{"property":"properties/200","parameter_name":"p","display_name":"D","scope":"EVENT"}`,
		"no scope":              `{"property":"properties/200","parameter_name":"p","display_name":"D","measurement_unit":"STANDARD"}`,
		"no property":           `{"parameter_name":"p","display_name":"D","measurement_unit":"STANDARD","scope":"EVENT"}`,
		"bad property":          `{"property":"accounts/1","parameter_name":"p","display_name":"D","measurement_unit":"STANDARD","scope":"EVENT"}`,
		"unspecified unit":      `{"property":"properties/200","parameter_name":"p","display_name":"D","measurement_unit":"MEASUREMENT_UNIT_UNSPECIFIED","scope":"EVENT"}`,
		"unspecified scope":     `{"property":"properties/200","parameter_name":"p","display_name":"D","measurement_unit":"STANDARD","scope":"METRIC_SCOPE_UNSPECIFIED"}`,
		"currency without type": `{"property":"properties/200","parameter_name":"p","display_name":"D","measurement_unit":"CURRENCY","scope":"EVENT"}`,
		"type without currency": `{` + base + `,"restricted_metric_type":["COST_DATA"]}`,
		"unspecified type":      `{"property":"properties/200","parameter_name":"p","display_name":"D","measurement_unit":"CURRENCY","scope":"EVENT","restricted_metric_type":["RESTRICTED_METRIC_TYPE_UNSPECIFIED"]}`,
		"parameter digit":       `{"property":"properties/200","parameter_name":"1p","display_name":"D","measurement_unit":"STANDARD","scope":"EVENT"}`,
		"parameter long":        `{"property":"properties/200","parameter_name":"` + strings.Repeat("p", 41) + `","display_name":"D","measurement_unit":"STANDARD","scope":"EVENT"}`,
		"display symbol":        `{"property":"properties/200","parameter_name":"p","display_name":"D[1]","measurement_unit":"STANDARD","scope":"EVENT"}`,
		"display blank":         `{"property":"properties/200","parameter_name":"p","display_name":"  ","measurement_unit":"STANDARD","scope":"EVENT"}`,
		"display long":          `{"property":"properties/200","parameter_name":"p","display_name":"` + strings.Repeat("d", 83) + `","measurement_unit":"STANDARD","scope":"EVENT"}`,
		"description long":      `{` + base + `,"description":"` + strings.Repeat("d", 151) + `"}`,
	} {
		if _, err := env.confirmed(customMetricsCreateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
	if _, err := env.confirmed(customMetricsCreateID, "sfree", `{`+base+`}`); err != nil {
		t.Errorf("valid create refused: %v", err)
	}
}

func TestCustomMetricsUpdateMaskComesFromSetArguments(t *testing.T) {
	for _, c := range []struct{ args, mask, body string }{
		{`,"display_name":"New"`, "display_name", `{"displayName":"New"}`},
		{`,"description":"d"`, "description", `{"description":"d"}`},
		{`,"description":""`, "description", `{"description":""}`},
		{`,"measurement_unit":"SECONDS"`, "measurement_unit", `{"measurementUnit":"SECONDS"}`},
		{`,"restricted_metric_type":[]`, "restricted_metric_type", `{}`},
		{`,"restricted_metric_type":["COST_DATA"],"measurement_unit":"CURRENCY","description":"x","display_name":"N"`,
			"display_name,description,measurement_unit,restricted_metric_type",
			`{"displayName":"N","description":"x","measurementUnit":"CURRENCY","restrictedMetricType":["COST_DATA"]}`},
	} {
		env := metricEnv(t, &fakeGoogle{api: always(http.StatusOK, eventMetricJSON)})
		args := `{"name":"properties/200/customMetrics/3001"` + c.args + `}`
		if _, err := env.call(customMetricsUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(customMetricsUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1beta/properties/200/customMetrics/3001" ||
			requests[0].rawQuery != "updateMask="+strings.ReplaceAll(c.mask, ",", "%2C") || requests[0].body != c.body {
			t.Fatalf("%s: %v, %+v", c.args, err, requests)
		}
	}
	env := metricEnv(t, &fakeGoogle{api: always(http.StatusOK, eventMetricJSON)})
	for name, args := range map[string]string{
		"no field":    `{"name":"properties/200/customMetrics/3001"}`,
		"empty name":  `{"name":"properties/200/customMetrics/3001","display_name":""}`,
		"parameter":   `{"name":"properties/200/customMetrics/3001","parameter_name":"x"}`,
		"scope":       `{"name":"properties/200/customMetrics/3001","scope":"EVENT"}`,
		"bad name":    `{"name":"properties/200","display_name":"x"}`,
		"bad unit":    `{"name":"properties/200/customMetrics/3001","measurement_unit":"PARSECS"}`,
		"bad type":    `{"name":"properties/200/customMetrics/3001","restricted_metric_type":["X"]}`,
		"duplicate":   `{"name":"properties/200/customMetrics/3001","restricted_metric_type":["COST_DATA","COST_DATA"]}`,
		"long descr.": `{"name":"properties/200/customMetrics/3001","description":"` + strings.Repeat("d", 151) + `"}`,
	} {
		if _, err := env.confirmed(customMetricsUpdateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
}

func TestCustomMetricsArchive(t *testing.T) {
	env := metricEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"properties/200/customMetrics/3001"}`
	if _, err := env.call(customMetricsArchiveID, "sfree", args); err == nil {
		t.Fatal("archive without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(customMetricsArchiveID, "sfree", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != "/v1beta/properties/200/customMetrics/3001:archive" || requests[0].rawQuery != "" ||
		requests[0].body != `{}` || !strings.Contains(result, `"archived":true`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
	if _, err := env.confirmed(customMetricsArchiveID, "sfree", `{"name":"properties/200"}`); err == nil {
		t.Error("bad name accepted")
	}
}

func TestCustomMetricsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{customMetricsListID, `{"property":"PROP"}`, "GET"},
		{customMetricsGetID, `{"name":"PROP/customMetrics/3001"}`, "GET"},
		{customMetricsCreateID, `{"property":"PROP","parameter_name":"p","display_name":"D","measurement_unit":"STANDARD","scope":"EVENT"}`, "POST"},
		{customMetricsUpdateID, `{"name":"PROP/customMetrics/3001","display_name":"S"}`, "PATCH"},
		{customMetricsArchiveID, `{"name":"PROP/customMetrics/3001"}`, "POST"},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			property := c.property
			env := metricEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1beta/"), ":archive")
					if r.Method == http.MethodPost && strings.HasSuffix(name, "customMetrics") {
						name += "/9"
					}
					body = `{"customMetrics":[],"name":"` + name + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			result, err := env.confirmed(tool.id, c.connection, strings.ReplaceAll(tool.arguments, "PROP", property))
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = tool.method
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, c.connection, property, err, methodsOf(requests), want)
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

func TestCustomMetricErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct {
		id, arguments string
		write         bool
	}{
		{customMetricsListID, `{"property":"properties/200"}`, false},
		{customMetricsGetID, `{"name":"properties/200/customMetrics/3001"}`, false},
		{customMetricsCreateID, `{"property":"properties/200","parameter_name":"p","display_name":"D","measurement_unit":"STANDARD","scope":"EVENT"}`, true},
		{customMetricsUpdateID, `{"name":"properties/200/customMetrics/3001","display_name":"S"}`, true},
		{customMetricsArchiveID, `{"name":"properties/200/customMetrics/3001"}`, true},
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
				env := metricEnv(t, &fakeGoogle{api: c.api})
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

func TestCustomMetric429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := metricEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", eventMetricJSON), nil
	}})
	args := `{"name":"properties/200/customMetrics/3001"}`
	if _, err := env.call(customMetricsGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(customMetricsGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestCustomMetricWritesDistrustAnAnswerOfAnotherMetric(t *testing.T) {
	env := metricEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/customMetrics/1"}`)})
	if _, err := env.confirmed(customMetricsUpdateID, "sfree", `{"name":"properties/200/customMetrics/3001","display_name":"S"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	if _, err := env.confirmed(customMetricsCreateID, "sfree", `{"property":"properties/200","parameter_name":"p","display_name":"D","measurement_unit":"STANDARD","scope":"EVENT"}`); classOf(err) != provider.ClassProviderError {
		t.Errorf("create: %v", err)
	}
}

func TestCustomMetricToolsHaveScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, want := range map[string]string{customMetricsListID: scopeReadOnly, customMetricsGetID: scopeReadOnly,
		customMetricsCreateID: scopeEdit, customMetricsUpdateID: scopeEdit, customMetricsArchiveID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
	}
	for _, id := range metricTools[2:] {
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) ||
			slices.Contains(metadata.Profiles[1].Tools, id) == (id == customMetricsArchiveID) {
			t.Errorf("%s is in the wrong profile", id)
		}
	}
	for _, id := range metricTools[:2] {
		if !slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("%s is not in read", id)
		}
	}
	for got, wants := range map[string][]string{
		reg.ConfirmationSummary(customMetricsCreateID)(json.RawMessage(`{"property":"properties/200","parameter_name":"p","display_name":"Shop","measurement_unit":"HOURS","scope":"EVENT"}`)): {"Create", "Shop", "properties/200", "HOURS"},
		reg.ConfirmationSummary(customMetricsUpdateID)(json.RawMessage(`{"name":"properties/200/customMetrics/1","description":"x","display_name":"X"}`)):                                      {"display_name, description", "properties/200/customMetrics/1"},
		reg.ConfirmationSummary(customMetricsArchiveID)(json.RawMessage(`{"name":"properties/200/customMetrics/1"}`)):                                                                          {"Archive", "properties/200/customMetrics/1"},
	} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}
