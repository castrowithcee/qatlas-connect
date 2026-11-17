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

// calculatedEnv adds connections that list the five calculated metric tools, with the bindings of the stream tests.
func calculatedEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{calculatedListID, calculatedGetID, calculatedCreateID, calculatedUpdateID, calculatedDeleteID}
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
	metricOneJSON = `{"name":"properties/200/calculatedMetrics/revenue_per_user","displayName":"Revenue per user",` +
		`"description":"Revenue by users","calculatedMetricId":"revenue_per_user","metricUnit":"CURRENCY",` +
		`"restrictedMetricType":["REVENUE_DATA"],"formula":"totalRevenue / totalUsers","invalidMetricReference":false,"extra":1}`
	metricTwoJSON = `{"name":"properties/200/calculatedMetrics/ratio","displayName":"Ratio","calculatedMetricId":"ratio",` +
		`"metricUnit":"STANDARD","formula":"a + b"}`
	metricThreeJSON = `{"name":"properties/200/calculatedMetrics/third","displayName":"Third","metricUnit":"SECONDS",` +
		`"formula":"x","invalidMetricReference":true}`
)

type calculatedOut struct {
	CalculatedMetrics []calculatedMetric `json:"calculatedMetrics"`
	NextCursor        string             `json:"next_cursor"`
	HasMore           bool               `json:"has_more"`
}

func TestCalculatedMetricsListPagesThroughThreePages(t *testing.T) {
	pages := []string{
		`{"calculatedMetrics":[` + metricOneJSON + `],"nextPageToken":"t2"}`,
		`{"calculatedMetrics":[` + metricTwoJSON + `],"nextPageToken":"t3"}`,
		`{"calculatedMetrics":[` + metricThreeJSON + `]}`,
	}
	env := calculatedEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(calculatedListID, "sfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		var out calculatedOut
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.CalculatedMetrics) != 1 ||
			out.HasMore != (i < 2) || strings.Contains(result, "extra") {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.CalculatedMetrics[0].Name))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"properties/200/calculatedMetrics/revenue_per_user",
		"properties/200/calculatedMetrics/ratio", "properties/200/calculatedMetrics/third"}) ||
		len(requests) != 3 || requests[0].path != "/v1alpha/properties/200/calculatedMetrics" ||
		requests[0].rawQuery != "pageSize=1" || !strings.Contains(requests[1].rawQuery, "pageToken=t2") ||
		!strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	if _, err := env.call(calculatedListID, "sfree", `{"property":"properties/201","cursor":"`+cursor+`"}`); err == nil && cursor != "" {
		t.Error("a cursor of another property was accepted")
	}
}

func TestCalculatedMetricsGetReturnsEveryDocumentedField(t *testing.T) {
	env := calculatedEnv(t, &fakeGoogle{api: always(http.StatusOK, metricOneJSON)})
	result, err := env.call(calculatedGetID, "sfree", `{"name":"properties/200/calculatedMetrics/revenue_per_user"}`)
	var got calculatedMetric
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || strings.Contains(result, "extra") {
		t.Fatalf("%v %s", err, result)
	}
	requests, _ := env.fake.seen()
	if got.DisplayName != "Revenue per user" || got.Description != "Revenue by users" ||
		got.CalculatedMetricID != "revenue_per_user" || got.MetricUnit != "CURRENCY" ||
		len(got.RestrictedMetricType) != 1 || got.RestrictedMetricType[0] != "REVENUE_DATA" ||
		got.Formula != "totalRevenue / totalUsers" || got.InvalidMetricReference == nil || *got.InvalidMetricReference ||
		len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/calculatedMetrics/revenue_per_user" {
		t.Errorf("metric = %+v, requests %+v", got, requests)
	}
}

func TestCalculatedMetricsAnswerIsBoundedAndChecked(t *testing.T) {
	env := calculatedEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"calculatedMetrics":[`+
		`{"name":"properties/200/calculatedMetrics/a","displayName":"`+strings.Repeat("n", 400)+`","description":"`+
		strings.Repeat("d", 5000)+`","formula":"`+strings.Repeat("f", 5000)+`","calculatedMetricId":"bad id"},`+
		`{"name":"properties/999/calculatedMetrics/b"},{"name":"properties/200/calculatedMetrics/bad name"},{"name":"nonsense"}]}`)})
	result, err := env.call(calculatedListID, "sfree", `{"property":"properties/200"}`)
	var out calculatedOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.CalculatedMetrics) != 1 {
		t.Fatalf("result %v, %v", len(result), err)
	}
	m := out.CalculatedMetrics[0]
	if len([]rune(string(m.DisplayName))) != maxTextRunes+1 || len([]rune(string(m.Description))) != maxDescriptionRunes+1 ||
		len([]rune(string(m.Formula))) != maxFormulaRunes+1 || m.CalculatedMetricID != "" {
		t.Errorf("metric = %d %d %d %q", len(m.DisplayName), len(m.Description), len(m.Formula), m.CalculatedMetricID)
	}
	env = calculatedEnv(t, &fakeGoogle{api: always(http.StatusOK, metricOneJSON)})
	if _, err := env.call(calculatedGetID, "sfree", `{"name":"properties/200/calculatedMetrics/other"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

const createArgs = `{"property":"properties/200","calculated_metric_id":"revenue_per_user","display_name":"Revenue per user",` +
	`"description":"Revenue by users","metric_unit":"CURRENCY","formula":"totalRevenue / totalUsers"}`

func TestCalculatedMetricsCreateSendsTheFormulaAndNeedsConfirmation(t *testing.T) {
	env := calculatedEnv(t, &fakeGoogle{api: always(http.StatusOK, metricOneJSON)})
	if _, err := env.call(calculatedCreateID, "sfree", createArgs); err == nil {
		t.Fatal("create without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(calculatedCreateID, "sfree", createArgs)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
		requests[0].path != "/v1alpha/properties/200/calculatedMetrics" ||
		requests[0].rawQuery != "calculatedMetricId=revenue_per_user" ||
		requests[0].body != `{"displayName":"Revenue per user","description":"Revenue by users","metricUnit":"CURRENCY",`+
			`"formula":"totalRevenue / totalUsers"}` || !strings.Contains(result, `"formula":"totalRevenue / totalUsers"`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func TestCalculatedMetricsCreateRefusesBadInputBeforeAnyIO(t *testing.T) {
	env := calculatedEnv(t, &fakeGoogle{api: always(http.StatusOK, metricOneJSON)})
	base := map[string]any{"property": "properties/200", "calculated_metric_id": "m1", "display_name": "M",
		"metric_unit": "STANDARD", "formula": "a + b"}
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
	for name, args := range map[string]string{
		"no id":            with("calculated_metric_id", nil),
		"no display name":  with("display_name", nil),
		"no unit":          with("metric_unit", nil),
		"no formula":       with("formula", nil),
		"no property":      with("property", nil),
		"id with blank":    with("calculated_metric_id", "a b"),
		"id with slash":    with("calculated_metric_id", "a/b"),
		"id with dash":     with("calculated_metric_id", "a-b"),
		"id too long":      with("calculated_metric_id", strings.Repeat("a", 81)),
		"unit unspecified": with("metric_unit", "METRIC_UNIT_UNSPECIFIED"),
		"unit unknown":     with("metric_unit", "PARSECS"),
		"blank name":       with("display_name", "   "),
		"long name":        with("display_name", strings.Repeat("n", 83)),
		"control name":     with("display_name", "a\x01b"),
		"long desc":        with("description", strings.Repeat("d", 4097)),
		"empty formula":    with("formula", ""),
		"blank formula":    with("formula", "   "),
		"long formula":     with("formula", strings.Repeat("a", 4097)),
		"formula quote":    with("formula", `a + "b"`),
		"formula semi":     with("formula", "a; b"),
		"formula newline":  with("formula", "a +\nb"),
		"bad property":     with("property", "accounts/200"),
	} {
		if _, err := env.confirmed(calculatedCreateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
	for _, ok := range []string{with("calculated_metric_id", strings.Repeat("A", 80)), with("display_name", strings.Repeat("n", 82)),
		with("formula", "( customEvent:parameter_name + cartPurchaseQuantity ) / 2.0 - -1.5e3 * 2")} {
		if _, err := env.confirmed(calculatedCreateID, "sfree", ok); refusedInvalid(err) {
			t.Errorf("%s: refused as %v", ok, err)
		}
	}
}

func TestCalculatedMetricsUpdateMaskComesFromSetArguments(t *testing.T) {
	const name = `"name":"properties/200/calculatedMetrics/revenue_per_user"`
	for _, c := range []struct{ args, mask, body string }{
		{`,"display_name":"New"`, "display_name", `{"displayName":"New"}`},
		{`,"formula":"a * 2"`, "formula", `{"formula":"a * 2"}`},
		{`,"description":""`, "description", `{"description":""}`},
		{`,"metric_unit":"HOURS"`, "metric_unit", `{"metricUnit":"HOURS"}`},
		{`,"formula":"a","metric_unit":"HOURS","description":"d","display_name":"N"`,
			"display_name,description,metric_unit,formula",
			`{"displayName":"N","description":"d","metricUnit":"HOURS","formula":"a"}`},
	} {
		env := calculatedEnv(t, &fakeGoogle{api: always(http.StatusOK, metricOneJSON)})
		args := `{` + name + c.args + `}`
		if _, err := env.call(calculatedUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(calculatedUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1alpha/properties/200/calculatedMetrics/revenue_per_user" ||
			requests[0].rawQuery != "updateMask="+strings.NewReplacer(",", "%2C").Replace(c.mask) ||
			requests[0].body != c.body {
			t.Fatalf("%v, %+v", err, requests)
		}
	}
	env := calculatedEnv(t, &fakeGoogle{api: always(http.StatusOK, metricOneJSON)})
	for argName, args := range map[string]string{
		"no field":      `{` + name + `}`,
		"empty name":    `{` + name + `,"display_name":""}`,
		"empty formula": `{` + name + `,"formula":""}`,
		"bad unit":      `{` + name + `,"metric_unit":"METRIC_UNIT_UNSPECIFIED"}`,
		"bad formula":   `{` + name + `,"formula":"a ^ b"}`,
		"bad name":      `{"name":"properties/200","display_name":"x"}`,
		"id in name":    `{"name":"properties/200/calculatedMetrics/a b","display_name":"x"}`,
		"immutable id":  `{` + name + `,"calculated_metric_id":"other"}`,
		"too long desc": `{` + name + `,"description":"` + strings.Repeat("d", 4097) + `"}`,
	} {
		if _, err := env.confirmed(calculatedUpdateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", argName)
		}
	}
	env.noIO(t)
}

func TestCalculatedMetricsDelete(t *testing.T) {
	env := calculatedEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"properties/200/calculatedMetrics/revenue_per_user"}`
	if _, err := env.call(calculatedDeleteID, "sfree", args); err == nil {
		t.Fatal("delete without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(calculatedDeleteID, "sfree", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete ||
		requests[0].path != "/v1alpha/properties/200/calculatedMetrics/revenue_per_user" || requests[0].body != "" ||
		!strings.Contains(result, `"deleted":true`) {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

var calculatedBindingCases = streamBindingCases

func TestCalculatedMetricsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{calculatedListID, `{"property":"PROP"}`, "GET"},
		{calculatedGetID, `{"name":"PROP/calculatedMetrics/m1"}`, "GET"},
		{calculatedCreateID, `{"property":"PROP","calculated_metric_id":"m1","display_name":"S","metric_unit":"STANDARD","formula":"a"}`, "POST"},
		{calculatedUpdateID, `{"name":"PROP/calculatedMetrics/m1","display_name":"S"}`, "PATCH"},
		{calculatedDeleteID, `{"name":"PROP/calculatedMetrics/m1"}`, "DELETE"},
	} {
		for _, c := range calculatedBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			property := c.property
			env := calculatedEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimPrefix(r.URL.Path, "/v1alpha/")
					if r.Method == http.MethodPost {
						name += "/m1"
					}
					body = `{"calculatedMetrics":[],"name":"` + name + `"}`
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

func TestCalculatedMetricErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct {
		id, arguments string
		write         bool
	}{
		{calculatedListID, `{"property":"properties/200"}`, false},
		{calculatedGetID, `{"name":"properties/200/calculatedMetrics/m1"}`, false},
		{calculatedCreateID, `{"property":"properties/200","calculated_metric_id":"m1","display_name":"S","metric_unit":"STANDARD","formula":"a"}`, true},
		{calculatedUpdateID, `{"name":"properties/200/calculatedMetrics/m1","display_name":"S"}`, true},
		{calculatedDeleteID, `{"name":"properties/200/calculatedMetrics/m1"}`, true},
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
				env := calculatedEnv(t, &fakeGoogle{api: c.api})
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

func TestCalculatedMetric429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := calculatedEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", metricOneJSON), nil
	}})
	const args = `{"name":"properties/200/calculatedMetrics/revenue_per_user"}`
	if _, err := env.call(calculatedGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(calculatedGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestCalculatedMetricWritesDistrustAnAnswerOfAnotherMetric(t *testing.T) {
	env := calculatedEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/calculatedMetrics/m1"}`)})
	if _, err := env.confirmed(calculatedUpdateID, "sfree", `{"name":"properties/200/calculatedMetrics/m1","display_name":"S"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	if _, err := env.confirmed(calculatedCreateID, "sfree", createArgs); classOf(err) != provider.ClassProviderError {
		t.Errorf("create: %v", err)
	}
}

func TestCalculatedMetricToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for id, want := range map[string]string{calculatedListID: scopeReadOnly, calculatedGetID: scopeReadOnly,
		calculatedCreateID: scopeEdit, calculatedUpdateID: scopeEdit, calculatedDeleteID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
		descriptor, _, ok := reg.Lookup(id)
		if !ok || descriptor.RequiresToolAllowList || !slices.Contains(profiles["write"], id) ||
			slices.Contains(profiles["read"], id) != (want == scopeReadOnly) {
			t.Errorf("%s: descriptor %+v, profiles wrong", id, descriptor)
		}
	}
	create := reg.ConfirmationSummary(calculatedCreateID)(json.RawMessage(createArgs))
	update := reg.ConfirmationSummary(calculatedUpdateID)(json.RawMessage(
		`{"name":"properties/200/calculatedMetrics/m1","formula":"a","display_name":"X"}`))
	del := reg.ConfirmationSummary(calculatedDeleteID)(json.RawMessage(`{"name":"properties/200/calculatedMetrics/m1"}`))
	for got, wants := range map[string][]string{
		create: {"Create", "Revenue per user", "revenue_per_user", "properties/200", "CURRENCY", "totalRevenue / totalUsers"},
		update: {"display_name, formula", "properties/200/calculatedMetrics/m1"},
		del:    {"Delete", "properties/200/calculatedMetrics/m1"}} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}
