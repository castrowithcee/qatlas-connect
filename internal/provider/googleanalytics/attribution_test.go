package googleanalytics

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// attributionEnv adds connections that list the attribution tools, with the bindings of the property tests.
func attributionEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{attributionGetID, attributionUpdateID}
	for name, targets := range map[string][]string{"gfree": nil, "gaccount": {"accounts/100"},
		"gproperty": {"properties/200"}, "gmixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const attributionJSON = `{"name":"properties/200/attributionSettings",` +
	`"reportingAttributionModel":"PAID_AND_ORGANIC_CHANNELS_DATA_DRIVEN",` +
	`"acquisitionConversionEventLookbackWindow":"ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_30_DAYS",` +
	`"otherConversionEventLookbackWindow":"OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_90_DAYS",` +
	`"adsWebConversionDataExportScope":"NOT_SELECTED_YET","extra":1}`

func TestAttributionGetReadsEveryMember(t *testing.T) {
	env := attributionEnv(t, &fakeGoogle{api: always(http.StatusOK, attributionJSON)})
	result, err := env.call(attributionGetID, "gfree", `{"property":"properties/200"}`)
	var out attribution
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || out.Name != "properties/200/attributionSettings" ||
		out.ReportingAttributionModel != "PAID_AND_ORGANIC_CHANNELS_DATA_DRIVEN" ||
		out.AcquisitionConversionEventLookbackWindow != "ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_30_DAYS" ||
		out.OtherConversionEventLookbackWindow != "OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_90_DAYS" ||
		out.AdsWebConversionDataExportScope != "NOT_SELECTED_YET" || strings.Contains(result, "extra") {
		t.Fatalf("%s, %v", result, err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].host != "analyticsadmin.googleapis.com" ||
		requests[0].path != "/v1alpha/properties/200/attributionSettings" {
		t.Errorf("requests = %+v", requests)
	}
	env = attributionEnv(t, &fakeGoogle{api: always(http.StatusOK, attributionJSON)})
	if _, err := env.call(attributionGetID, "gfree", `{"property":"properties/201"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("settings of another property: %v", err)
	}
}

func TestAttributionUpdateMaskComesFromSetArguments(t *testing.T) {
	const (
		model   = `"reporting_attribution_model":"GOOGLE_PAID_CHANNELS_LAST_CLICK"`
		acquire = `"acquisition_conversion_event_lookback_window":"ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_7_DAYS"`
		other   = `"other_conversion_event_lookback_window":"OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_60_DAYS"`
		export  = `"ads_web_conversion_data_export_scope":"GOOGLE_PAID_CHANNELS"`
	)
	for _, c := range []struct{ args, mask, body string }{
		{model, "reporting_attribution_model", `{"reportingAttributionModel":"GOOGLE_PAID_CHANNELS_LAST_CLICK"}`},
		{acquire, "acquisition_conversion_event_lookback_window",
			`{"acquisitionConversionEventLookbackWindow":"ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_7_DAYS"}`},
		{other, "other_conversion_event_lookback_window",
			`{"otherConversionEventLookbackWindow":"OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_60_DAYS"}`},
		{export, "ads_web_conversion_data_export_scope", `{"adsWebConversionDataExportScope":"GOOGLE_PAID_CHANNELS"}`},
		{export + "," + other + "," + acquire + "," + model,
			"reporting_attribution_model,acquisition_conversion_event_lookback_window," +
				"other_conversion_event_lookback_window,ads_web_conversion_data_export_scope",
			`{"reportingAttributionModel":"GOOGLE_PAID_CHANNELS_LAST_CLICK",` +
				`"acquisitionConversionEventLookbackWindow":"ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_7_DAYS",` +
				`"otherConversionEventLookbackWindow":"OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_60_DAYS",` +
				`"adsWebConversionDataExportScope":"GOOGLE_PAID_CHANNELS"}`},
	} {
		env := attributionEnv(t, &fakeGoogle{api: always(http.StatusOK, attributionJSON)})
		args := `{"property":"properties/200",` + c.args + `}`
		if _, err := env.call(attributionUpdateID, "gfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		result, err := env.confirmed(attributionUpdateID, "gfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1alpha/properties/200/attributionSettings" ||
			requests[0].rawQuery != "updateMask="+strings.NewReplacer(",", "%2C").Replace(c.mask) ||
			requests[0].body != c.body || !strings.Contains(result, "PAID_AND_ORGANIC_CHANNELS_DATA_DRIVEN") {
			t.Fatalf("%v, %+v, %s", err, requests, result)
		}
	}
}

func TestAttributionUpdateRefusesBadInputBeforeAnyIO(t *testing.T) {
	env := attributionEnv(t, &fakeGoogle{api: always(http.StatusOK, attributionJSON)})
	for name, args := range map[string]string{
		"no field":            `{"property":"properties/200"}`,
		"unspecified model":   `{"property":"properties/200","reporting_attribution_model":"REPORTING_ATTRIBUTION_MODEL_UNSPECIFIED"}`,
		"old model name":      `{"property":"properties/200","reporting_attribution_model":"CROSS_CHANNEL_LAST_CLICK"}`,
		"acquisition 60 days": `{"property":"properties/200","acquisition_conversion_event_lookback_window":"OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_60_DAYS"}`,
		"other 7 days":        `{"property":"properties/200","other_conversion_event_lookback_window":"ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_7_DAYS"}`,
		"not selected yet":    `{"property":"properties/200","ads_web_conversion_data_export_scope":"NOT_SELECTED_YET"}`,
		"lower case":          `{"property":"properties/200","ads_web_conversion_data_export_scope":"google_paid_channels"}`,
		"bad property":        `{"property":"accounts/200","ads_web_conversion_data_export_scope":"GOOGLE_PAID_CHANNELS"}`,
		"unknown argument":    `{"property":"properties/200","ads_web_conversion_data_export_scope":"GOOGLE_PAID_CHANNELS","mask":"*"}`,
		"missing property":    `{"ads_web_conversion_data_export_scope":"GOOGLE_PAID_CHANNELS"}`,
	} {
		if _, err := env.confirmed(attributionUpdateID, "gfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
}

func TestAttributionFollowsTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{attributionGetID, `{"property":"PROP"}`, "GET"},
		{attributionUpdateID, `{"property":"PROP","ads_web_conversion_data_export_scope":"GOOGLE_PAID_CHANNELS"}`, "PATCH"},
	} {
		for _, c := range streamBindingCases {
			connection := strings.Replace(c.connection, "s", "g", 1)
			env := attributionEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body := map[string]string{
					"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
					"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
					"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
				}[r.URL.Path]
				if body == "" {
					body = `{"name":"` + strings.TrimPrefix(r.URL.Path, "/v1alpha/") + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			result, err := env.confirmed(tool.id, connection, strings.ReplaceAll(tool.arguments, "PROP", c.property))
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = tool.method
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, connection, c.property, err,
					methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, connection, c.property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestAttributionErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct {
		id, arguments string
		write         bool
	}{
		{attributionGetID, `{"property":"properties/200"}`, false},
		{attributionUpdateID, `{"property":"properties/200","reporting_attribution_model":"GOOGLE_PAID_CHANNELS_LAST_CLICK"}`, true},
	}
	for _, c := range []struct {
		name   string
		api    func(int, *http.Request) (*http.Response, error)
		class  provider.Class
		unsure bool
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission, false},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited, false},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, true},
	} {
		for _, call := range calls {
			t.Run(c.name+" "+call.id, func(t *testing.T) {
				env := attributionEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(call.id, "gfree", call.arguments)
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

func TestAttribution429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := attributionEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", attributionJSON), nil
	}})
	if _, err := env.call(attributionGetID, "gfree", `{"property":"properties/200"}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(attributionGetID, "gfree", `{"property":"properties/200"}`); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestAttributionUpdateDistrustsAnAnswerOfAnotherProperty(t *testing.T) {
	env := attributionEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/attributionSettings"}`)})
	_, err := env.confirmed(attributionUpdateID, "gfree",
		`{"property":"properties/200","reporting_attribution_model":"GOOGLE_PAID_CHANNELS_LAST_CLICK"}`)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error = %v", err)
	}
}

func TestAttributionToolsDeclareRiskScopeProfileAlphaAndSummary(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, want := range map[string]struct {
		scope   string
		risk    capability.Risk
		inRead  bool
		inWrite bool
	}{
		attributionGetID:    {scopeReadOnly, readRisk, true, true},
		attributionUpdateID: {scopeEdit, writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent), false, true},
	} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Risk != want.risk || d.RequiresToolAllowList || !strings.HasSuffix(d.Description, alphaNote) ||
			!slices.Equal(metadata.Token.Scopes[id], []string{want.scope}) {
			t.Errorf("%s: %+v, scopes %v", id, d.Risk, metadata.Token.Scopes[id])
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) != want.inRead || slices.Contains(metadata.Profiles[1].Tools, id) != want.inWrite {
			t.Errorf("%s: wrong profile membership", id)
		}
	}
	summary := reg.ConfirmationSummary(attributionUpdateID)
	if summary == nil {
		t.Fatal("no confirmation summary")
	}
	text := summary(json.RawMessage(`{"property":"properties/200","reporting_attribution_model":"GOOGLE_PAID_CHANNELS_LAST_CLICK",` +
		`"other_conversion_event_lookback_window":"OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_30_DAYS"}`))
	for _, part := range []string{"properties/200", "GOOGLE_PAID_CHANNELS_LAST_CLICK", "OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_30_DAYS", "historical"} {
		if !strings.Contains(text, part) {
			t.Errorf("summary %q lacks %q", text, part)
		}
	}
}
