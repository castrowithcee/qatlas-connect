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

// retentionEnv adds connections that list the four settings tools, with the bindings of the property tests, and
// one connection that holds the changing effects but lists nothing.
func retentionEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{dataRetentionGetID, dataRetentionUpdateID, googleSignalsGetID, googleSignalsUpdateID}
	for name, targets := range map[string][]string{"gfree": nil, "gaccount": {"accounts/100"},
		"gproperty": {"properties/200"}, "gmixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	retentionJSON = `{"name":"properties/200/dataRetentionSettings","eventDataRetention":"FOURTEEN_MONTHS",` +
		`"userDataRetention":"TWO_MONTHS","resetUserDataOnNewActivity":true,"extra":1}`
	signalsJSON = `{"name":"properties/200/googleSignalsSettings","state":"GOOGLE_SIGNALS_ENABLED",` +
		`"consent":"GOOGLE_SIGNALS_CONSENT_CONSENTED","extra":1}`
)

func TestDataRetentionGetReadsEveryDuration(t *testing.T) {
	for _, event := range []string{"TWO_MONTHS", "FOURTEEN_MONTHS", "TWENTY_SIX_MONTHS", "THIRTY_EIGHT_MONTHS", "FIFTY_MONTHS"} {
		for _, user := range []string{"TWO_MONTHS", "FOURTEEN_MONTHS"} {
			answer := `{"name":"properties/200/dataRetentionSettings","eventDataRetention":"` + event +
				`","userDataRetention":"` + user + `","resetUserDataOnNewActivity":true}`
			env := retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, answer)})
			result, err := env.call(dataRetentionGetID, "gfree", `{"property":"properties/200"}`)
			var out dataRetention
			if err != nil || json.Unmarshal([]byte(result), &out) != nil || string(out.EventDataRetention) != event ||
				string(out.UserDataRetention) != user || !out.ResetUserDataOnNewActivity ||
				out.Name != "properties/200/dataRetentionSettings" {
				t.Fatalf("%s %s: %s, %v", event, user, result, err)
			}
			requests, _ := env.fake.seen()
			if len(requests) != 1 || requests[0].method != http.MethodGet ||
				requests[0].path != "/v1beta/properties/200/dataRetentionSettings" {
				t.Errorf("requests = %+v", requests)
			}
		}
	}
	env := retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, retentionJSON)})
	if result, _ := env.call(dataRetentionGetID, "gfree", `{"property":"properties/200"}`); strings.Contains(result, "extra") {
		t.Errorf("an unknown member was passed on: %s", result)
	}
	env = retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, retentionJSON)})
	if _, err := env.call(dataRetentionGetID, "gfree", `{"property":"properties/201"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("settings of another property: %v", err)
	}
}

func TestDataRetentionUpdateIsListedOnly(t *testing.T) {
	env := retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, signalsJSON)})
	args := `{"property":"properties/200","event_data_retention":"FOURTEEN_MONTHS"}`
	// write holds every changing effect but does not list the tool; read does not either.
	for _, connection := range []string{"write", "read", "wfree"} {
		if _, err := env.confirmed(dataRetentionUpdateID, connection, args); err == nil {
			t.Errorf("%s: a tool that is not listed was reachable", connection)
		}
	}
	// The other tools of the group are not listed-only: google-signals.update is reachable by effect.
	if _, err := env.confirmed(googleSignalsUpdateID, "write",
		`{"property":"properties/200","state":"GOOGLE_SIGNALS_DISABLED"}`); err != nil {
		t.Errorf("google-signals.update on a write connection: %v", err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d, want only the signals change", len(requests))
	}
}

func TestDataRetentionUpdateMaskComesFromSetArguments(t *testing.T) {
	for _, c := range []struct{ args, mask, body string }{
		{`,"event_data_retention":"TWO_MONTHS"`, "event_data_retention", `{"eventDataRetention":"TWO_MONTHS"}`},
		{`,"user_data_retention":"FOURTEEN_MONTHS"`, "user_data_retention", `{"userDataRetention":"FOURTEEN_MONTHS"}`},
		{`,"reset_user_data_on_new_activity":false`, "reset_user_data_on_new_activity",
			`{"resetUserDataOnNewActivity":false}`},
		{`,"reset_user_data_on_new_activity":true,"user_data_retention":"TWO_MONTHS","event_data_retention":"FIFTY_MONTHS"`,
			"event_data_retention,user_data_retention,reset_user_data_on_new_activity",
			`{"eventDataRetention":"FIFTY_MONTHS","userDataRetention":"TWO_MONTHS","resetUserDataOnNewActivity":true}`},
	} {
		env := retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, retentionJSON)})
		args := `{"property":"properties/200"` + c.args + `}`
		if _, err := env.call(dataRetentionUpdateID, "gfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		result, err := env.confirmed(dataRetentionUpdateID, "gfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1beta/properties/200/dataRetentionSettings" ||
			requests[0].rawQuery != "updateMask="+strings.NewReplacer(",", "%2C").Replace(c.mask) ||
			requests[0].body != c.body || !strings.Contains(result, "FOURTEEN_MONTHS") {
			t.Fatalf("%v, %+v, %s", err, requests, result)
		}
	}
}

func TestDataRetentionUpdateRefusesBadInputBeforeAnyIO(t *testing.T) {
	env := retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, retentionJSON)})
	for name, args := range map[string]string{
		"no field":           `{"property":"properties/200"}`,
		"bad event value":    `{"property":"properties/200","event_data_retention":"SIXTY_MONTHS"}`,
		"unspecified":        `{"property":"properties/200","event_data_retention":"RETENTION_DURATION_UNSPECIFIED"}`,
		"lower case":         `{"property":"properties/200","event_data_retention":"two_months"}`,
		"user 26 months":     `{"property":"properties/200","user_data_retention":"TWENTY_SIX_MONTHS"}`,
		"user bad value":     `{"property":"properties/200","user_data_retention":"x"}`,
		"reset not boolean":  `{"property":"properties/200","reset_user_data_on_new_activity":"yes"}`,
		"bad property":       `{"property":"accounts/200","event_data_retention":"TWO_MONTHS"}`,
		"unknown argument":   `{"property":"properties/200","event_data_retention":"TWO_MONTHS","mask":"*"}`,
		"missing property":   `{"event_data_retention":"TWO_MONTHS"}`,
		"foreign (property)": `{"property":"properties/201","event_data_retention":"TWO_MONTHS"}`,
	} {
		connection := "gfree"
		if name == "foreign (property)" {
			connection = "gproperty"
		}
		if _, err := env.confirmed(dataRetentionUpdateID, connection, args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
}

func TestDataRetentionFollowsTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{dataRetentionGetID, `{"property":"PROP"}`, "GET"},
		{dataRetentionUpdateID, `{"property":"PROP","event_data_retention":"TWO_MONTHS"}`, "PATCH"},
		{googleSignalsGetID, `{"property":"PROP"}`, "GET"},
		{googleSignalsUpdateID, `{"property":"PROP","state":"GOOGLE_SIGNALS_DISABLED"}`, "PATCH"},
	} {
		for _, c := range streamBindingCases {
			connection := strings.Replace(c.connection, "s", "g", 1)
			env := retentionEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body := map[string]string{
					"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
					"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
					"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
				}[r.URL.Path]
				if body == "" {
					body = `{"name":"` + strings.NewReplacer("/v1beta/", "", "/v1alpha/", "").Replace(r.URL.Path) + `"}`
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

func TestGoogleSignalsReadAndStateChanges(t *testing.T) {
	env := retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, signalsJSON)})
	result, err := env.call(googleSignalsGetID, "gfree", `{"property":"properties/200"}`)
	var out googleSignals
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || out.State != "GOOGLE_SIGNALS_ENABLED" ||
		out.Consent != "GOOGLE_SIGNALS_CONSENT_CONSENTED" || strings.Contains(result, "extra") {
		t.Fatalf("%s, %v", result, err)
	}
	requests, _ := env.fake.seen()
	if len(requests) != 1 || requests[0].path != "/v1alpha/properties/200/googleSignalsSettings" {
		t.Errorf("requests = %+v", requests)
	}
	for _, state := range []string{"GOOGLE_SIGNALS_ENABLED", "GOOGLE_SIGNALS_DISABLED"} {
		env := retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, signalsJSON)})
		args := `{"property":"properties/200","state":"` + state + `"}`
		if _, err := env.call(googleSignalsUpdateID, "gfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		if _, err := env.confirmed(googleSignalsUpdateID, "gfree", args); err != nil {
			t.Fatal(err)
		}
		requests, _ := env.fake.seen()
		if len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1alpha/properties/200/googleSignalsSettings" ||
			requests[0].rawQuery != "updateMask=state" || requests[0].body != `{"state":"`+state+`"}` {
			t.Errorf("%s: requests = %+v", state, requests)
		}
	}
	env = retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, signalsJSON)})
	for name, args := range map[string]string{
		"no state":    `{"property":"properties/200"}`,
		"unspecified": `{"property":"properties/200","state":"GOOGLE_SIGNALS_STATE_UNSPECIFIED"}`,
		"other":       `{"property":"properties/200","state":"on"}`,
	} {
		if _, err := env.confirmed(googleSignalsUpdateID, "gfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
}

func TestRetentionAndSignalsErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct {
		id, arguments string
		write         bool
	}{
		{dataRetentionGetID, `{"property":"properties/200"}`, false},
		{dataRetentionUpdateID, `{"property":"properties/200","event_data_retention":"TWO_MONTHS"}`, true},
		{googleSignalsGetID, `{"property":"properties/200"}`, false},
		{googleSignalsUpdateID, `{"property":"properties/200","state":"GOOGLE_SIGNALS_ENABLED"}`, true},
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
				env := retentionEnv(t, &fakeGoogle{api: c.api})
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

func TestRetentionSignals429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := retentionEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", retentionJSON), nil
	}})
	if _, err := env.call(dataRetentionGetID, "gfree", `{"property":"properties/200"}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(dataRetentionGetID, "gfree", `{"property":"properties/200"}`); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestSettingsWritesDistrustAnAnswerOfAnotherProperty(t *testing.T) {
	env := retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/dataRetentionSettings"}`)})
	if _, err := env.confirmed(dataRetentionUpdateID, "gfree", `{"property":"properties/200","user_data_retention":"TWO_MONTHS"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("retention: %v", err)
	}
	env = retentionEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/googleSignalsSettings"}`)})
	if _, err := env.confirmed(googleSignalsUpdateID, "gfree", `{"property":"properties/200","state":"GOOGLE_SIGNALS_ENABLED"}`); classOf(err) != provider.ClassProviderError {
		t.Errorf("signals: %v", err)
	}
}

func TestSettingsToolsDeclareRiskScopeProfileAndSummary(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	wants := map[string]struct {
		scope    string
		risk     capability.Risk
		listed   bool
		inWrite  bool
		inRead   bool
		confirms []string
	}{
		dataRetentionGetID:    {scopeReadOnly, readRisk, false, true, true, nil},
		googleSignalsGetID:    {scopeReadOnly, readRisk, false, true, true, nil},
		dataRetentionUpdateID: {scopeEdit, writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent), true, false, false, []string{"event data TWO_MONTHS", "properties/200", "permanently deletes"}},
		googleSignalsUpdateID: {scopeEdit, writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent), false, true, false, []string{"GOOGLE_SIGNALS_DISABLED", "properties/200"}},
	}
	for id, want := range wants {
		var d capability.Descriptor
		for _, candidate := range reg.Provider(Provider) {
			if candidate.ID == id {
				d = candidate
			}
		}
		if d.ID == "" || d.Risk != want.risk || d.RequiresToolAllowList != want.listed ||
			!slices.Equal(metadata.Token.Scopes[id], []string{want.scope}) {
			t.Errorf("%s: %+v, listed-only %v, scopes %v", id, d.Risk, d.RequiresToolAllowList, metadata.Token.Scopes[id])
		}
		if slices.Contains(metadata.Profiles[1].Tools, id) != want.inWrite || slices.Contains(metadata.Profiles[0].Tools, id) != want.inRead {
			t.Errorf("%s: wrong profile membership", id)
		}
		if want.risk.Confirmation == capability.ConfirmationRequired {
			summary := reg.ConfirmationSummary(id)
			if summary == nil {
				t.Fatalf("%s has no confirmation summary", id)
			}
			text := summary(json.RawMessage(`{"property":"properties/200","event_data_retention":"TWO_MONTHS","state":"GOOGLE_SIGNALS_DISABLED"}`))
			for _, part := range want.confirms {
				if !strings.Contains(text, part) {
					t.Errorf("summary %q lacks %q", text, part)
				}
			}
		}
	}
}
