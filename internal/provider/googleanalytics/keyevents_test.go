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

// keyEventEnv adds connections that list the five key event tools, with the bindings of the stream tests.
func keyEventEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{keyEventsListID, keyEventsGetID, keyEventsCreateID, keyEventsUpdateID, keyEventsDeleteID}
	for name, targets := range map[string][]string{"kfree": nil, "kaccount": {"accounts/100"},
		"kproperty": {"properties/200"}, "kmixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	purchaseJSON = `{"name":"properties/200/keyEvents/3001","eventName":"purchase","createTime":"2026-11-01T00:00:00Z",` +
		`"deletable":true,"custom":false,"countingMethod":"ONCE_PER_EVENT","defaultValue":` +
		`{"numericValue":10.5,"currencyCode":"EUR"},"extra":1}`
	signupJSON = `{"name":"properties/200/keyEvents/3002","eventName":"sign_up","createTime":"2026-11-02T00:00:00Z",` +
		`"deletable":false,"custom":true,"countingMethod":"ONCE_PER_SESSION"}`
)

type keyEventsOut struct {
	KeyEvents  []keyEvent `json:"keyEvents"`
	NextCursor string     `json:"next_cursor"`
	HasMore    bool       `json:"has_more"`
}

func TestKeyEventsListPagesThroughThreePages(t *testing.T) {
	pages := []string{
		`{"keyEvents":[` + purchaseJSON + `],"nextPageToken":"t2"}`,
		`{"keyEvents":[` + signupJSON + `],"nextPageToken":"t3"}`,
		`{"keyEvents":[{"name":"properties/200/keyEvents/3003","eventName":"login"}]}`,
	}
	env := keyEventEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(keyEventsListID, "kfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		var out keyEventsOut
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.KeyEvents) != 1 || out.HasMore != (i < 2) {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.KeyEvents[0].EventName))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"purchase", "sign_up", "login"}) || len(requests) != 3 ||
		requests[0].path != "/v1beta/properties/200/keyEvents" || requests[0].rawQuery != "pageSize=1" ||
		!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	if _, err := env.call(keyEventsListID, "kfree", `{"property":"properties/201","cursor":"`+cursor+`"}`); err == nil && cursor != "" {
		t.Error("a cursor of another property was accepted")
	}
}

func TestKeyEventsGetReturnsEveryMember(t *testing.T) {
	answers := map[string]string{"3001": purchaseJSON, "3002": signupJSON}
	env := keyEventEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", answers[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]), nil
	}})
	result, err := env.call(keyEventsGetID, "kfree", `{"name":"properties/200/keyEvents/3001"}`)
	var got keyEvent
	if err != nil || json.Unmarshal([]byte(result), &got) != nil || strings.Contains(result, "extra") {
		t.Fatalf("%v %s", err, result)
	}
	if got.EventName != "purchase" || got.CreateTime == "" || got.Deletable == nil || !*got.Deletable ||
		got.Custom == nil || *got.Custom || got.CountingMethod != "ONCE_PER_EVENT" || got.DefaultValue == nil ||
		*got.DefaultValue.NumericValue != 10.5 || got.DefaultValue.CurrencyCode != "EUR" {
		t.Errorf("key event = %+v", got)
	}
	// A key event that cannot be deleted shows deletable=false.
	result, err = env.call(keyEventsGetID, "kfree", `{"name":"properties/200/keyEvents/3002"}`)
	if err != nil || !strings.Contains(result, `"deletable":false`) || strings.Contains(result, "defaultValue") {
		t.Errorf("%v %s", err, result)
	}
	// An answer for another key event is not trusted.
	if _, err := env.call(keyEventsGetID, "kfree", `{"name":"properties/200/keyEvents/9"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

func TestKeyEventsListIsBoundedAndChecked(t *testing.T) {
	env := keyEventEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"keyEvents":[`+
		`{"name":"properties/200/keyEvents/1","eventName":"`+strings.Repeat("n", 400)+`"},`+
		`{"name":"properties/999/keyEvents/2"},{"name":"nonsense"},{"eventName":"unnamed"}]}`)})
	result, err := env.call(keyEventsListID, "kfree", `{"property":"properties/200"}`)
	var out keyEventsOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.KeyEvents) != 1 ||
		len([]rune(string(out.KeyEvents[0].EventName))) != maxTextRunes+1 {
		t.Errorf("result %v, %v", result, err)
	}
}

func TestKeyEventsCreateWithBothCountingMethods(t *testing.T) {
	for _, c := range []struct{ args, body string }{
		{`{"property":"properties/200","event_name":"purchase","counting_method":"ONCE_PER_EVENT","default_value":{"numeric_value":10.5,"currency_code":"EUR"}}`,
			`{"eventName":"purchase","countingMethod":"ONCE_PER_EVENT","defaultValue":{"numericValue":10.5,"currencyCode":"EUR"}}`},
		{`{"property":"properties/200","event_name":"sign_up","counting_method":"ONCE_PER_SESSION"}`,
			`{"eventName":"sign_up","countingMethod":"ONCE_PER_SESSION"}`},
	} {
		env := keyEventEnv(t, &fakeGoogle{api: always(http.StatusOK, purchaseJSON)})
		if _, err := env.call(keyEventsCreateID, "kfree", c.args); err == nil {
			t.Fatal("create without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(keyEventsCreateID, "kfree", c.args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
			requests[0].path != "/v1beta/properties/200/keyEvents" || requests[0].rawQuery != "" ||
			requests[0].body != c.body || requests[0].contentType != "application/json" {
			t.Fatalf("%v, %+v", err, requests)
		}
	}
}

func TestKeyEventsCreateRefusesBadArguments(t *testing.T) {
	env := keyEventEnv(t, &fakeGoogle{api: always(http.StatusOK, purchaseJSON)})
	base := `"property":"properties/200"`
	for name, args := range map[string]string{
		"no event name":   `{` + base + `,"counting_method":"ONCE_PER_EVENT"}`,
		"empty event":     `{` + base + `,"event_name":"","counting_method":"ONCE_PER_EVENT"}`,
		"bad event":       `{` + base + `,"event_name":"1 bad","counting_method":"ONCE_PER_EVENT"}`,
		"long event":      `{` + base + `,"event_name":"` + strings.Repeat("a", 41) + `","counting_method":"ONCE_PER_EVENT"}`,
		"no method":       `{` + base + `,"event_name":"purchase"}`,
		"unspecified":     `{` + base + `,"event_name":"purchase","counting_method":"COUNTING_METHOD_UNSPECIFIED"}`,
		"value no cur":    `{` + base + `,"event_name":"purchase","counting_method":"ONCE_PER_EVENT","default_value":{"numeric_value":1}}`,
		"value no number": `{` + base + `,"event_name":"purchase","counting_method":"ONCE_PER_EVENT","default_value":{"currency_code":"EUR"}}`,
		"bad currency":    `{` + base + `,"event_name":"purchase","counting_method":"ONCE_PER_EVENT","default_value":{"numeric_value":1,"currency_code":"eur"}}`,
		"bad property":    `{"property":"accounts/100","event_name":"purchase","counting_method":"ONCE_PER_EVENT"}`,
	} {
		if _, err := env.confirmed(keyEventsCreateID, "kfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
}

func TestKeyEventsUpdateMaskComesFromSetArguments(t *testing.T) {
	const name = `"name":"properties/200/keyEvents/3001"`
	for _, c := range []struct{ args, mask, body string }{
		{`"counting_method":"ONCE_PER_SESSION"`, "counting_method", `{"countingMethod":"ONCE_PER_SESSION"}`},
		{`"default_value":{"numeric_value":2,"currency_code":"USD"}`, "default_value",
			`{"defaultValue":{"numericValue":2,"currencyCode":"USD"}}`},
		{`"default_value":{"numeric_value":0,"currency_code":"USD"},"counting_method":"ONCE_PER_EVENT"`,
			"counting_method,default_value", `{"countingMethod":"ONCE_PER_EVENT","defaultValue":{"numericValue":0,"currencyCode":"USD"}}`},
	} {
		env := keyEventEnv(t, &fakeGoogle{api: always(http.StatusOK, purchaseJSON)})
		args := `{` + name + `,` + c.args + `}`
		if _, err := env.call(keyEventsUpdateID, "kfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(keyEventsUpdateID, "kfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1beta/properties/200/keyEvents/3001" ||
			requests[0].rawQuery != "updateMask="+strings.ReplaceAll(c.mask, ",", "%2C") || requests[0].body != c.body {
			t.Fatalf("%v, %+v", err, requests)
		}
	}
	env := keyEventEnv(t, &fakeGoogle{api: always(http.StatusOK, purchaseJSON)})
	for label, args := range map[string]string{
		"no field":   `{` + name + `}`,
		"event name": `{` + name + `,"event_name":"x"}`,
		"bad method": `{` + name + `,"counting_method":"NONE"}`,
		"bad value":  `{` + name + `,"default_value":{"numeric_value":1}}`,
		"bad name":   `{"name":"properties/200","counting_method":"ONCE_PER_EVENT"}`,
	} {
		if _, err := env.confirmed(keyEventsUpdateID, "kfree", args); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
	env.noIO(t)
}

func TestKeyEventsDelete(t *testing.T) {
	env := keyEventEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"properties/200/keyEvents/3001"}`
	if _, err := env.call(keyEventsDeleteID, "kfree", args); err == nil {
		t.Fatal("delete without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(keyEventsDeleteID, "kfree", args)
	requests, _ := env.fake.seen()
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete ||
		requests[0].path != "/v1beta/properties/200/keyEvents/3001" || requests[0].body != "" ||
		!strings.Contains(result, `"deleted":true`) || !strings.Contains(result, "properties/200/keyEvents/3001") {
		t.Fatalf("%v, %+v, %s", err, requests, result)
	}
}

func TestKeyEventDeleteOfANonDeletableKeyEventIsRefusedByGoogle(t *testing.T) {
	env := keyEventEnv(t, &fakeGoogle{api: always(http.StatusBadRequest,
		errorObject(400, "FAILED_PRECONDITION", "KEY_EVENT_NOT_DELETABLE"))})
	_, err := env.confirmed(keyEventsDeleteID, "kfree", `{"name":"properties/200/keyEvents/3002"}`)
	if err == nil || classOf(err) != provider.ClassProviderError || leaks(err.Error()) != "" ||
		strings.Contains(err.Error(), "outcome of the change is unknown") {
		t.Errorf("error = %v (class %s)", err, classOf(err))
	}
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d, want exactly one", len(requests))
	}
}

func TestKeyEventsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{keyEventsListID, `{"property":"PROP"}`, "GET"},
		{keyEventsGetID, `{"name":"PROP/keyEvents/3001"}`, "GET"},
		{keyEventsCreateID, `{"property":"PROP","event_name":"purchase","counting_method":"ONCE_PER_EVENT"}`, "POST"},
		{keyEventsUpdateID, `{"name":"PROP/keyEvents/3001","counting_method":"ONCE_PER_SESSION"}`, "PATCH"},
		{keyEventsDeleteID, `{"name":"PROP/keyEvents/3001"}`, "DELETE"},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			property := c.property
			env := keyEventEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimPrefix(r.URL.Path, "/v1beta/")
					if r.Method == http.MethodPost {
						name += "/9"
					}
					body = `{"keyEvents":[],"name":"` + name + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			connection := "k" + strings.TrimPrefix(c.connection, "s")
			args := strings.ReplaceAll(tool.arguments, "PROP", property)
			result, err := env.confirmed(tool.id, connection, args)
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = tool.method
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, connection, property, err, methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, connection, property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestKeyEventErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct {
		id, arguments string
		write         bool
	}{
		{keyEventsListID, `{"property":"properties/200"}`, false},
		{keyEventsGetID, `{"name":"properties/200/keyEvents/3001"}`, false},
		{keyEventsCreateID, `{"property":"properties/200","event_name":"purchase","counting_method":"ONCE_PER_EVENT"}`, true},
		{keyEventsUpdateID, `{"name":"properties/200/keyEvents/3001","counting_method":"ONCE_PER_SESSION"}`, true},
		{keyEventsDeleteID, `{"name":"properties/200/keyEvents/3001"}`, true},
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
				env := keyEventEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(call.id, "kfree", call.arguments)
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

func TestKeyEvent429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := keyEventEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", purchaseJSON), nil
	}})
	args := `{"name":"properties/200/keyEvents/3001"}`
	if _, err := env.call(keyEventsGetID, "kfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(keyEventsGetID, "kfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestKeyEventWritesDistrustAnAnswerOfAnotherKeyEvent(t *testing.T) {
	env := keyEventEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/keyEvents/1"}`)})
	if _, err := env.confirmed(keyEventsUpdateID, "kfree", `{"name":"properties/200/keyEvents/3001","counting_method":"ONCE_PER_EVENT"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	if _, err := env.confirmed(keyEventsCreateID, "kfree", `{"property":"properties/200","event_name":"purchase","counting_method":"ONCE_PER_EVENT"}`); classOf(err) != provider.ClassProviderError {
		t.Errorf("create: %v", err)
	}
}

func TestKeyEventToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, want := range map[string]string{keyEventsListID: scopeReadOnly, keyEventsGetID: scopeReadOnly,
		keyEventsCreateID: scopeEdit, keyEventsUpdateID: scopeEdit, keyEventsDeleteID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
	}
	for _, id := range []string{keyEventsListID, keyEventsGetID} {
		if !slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("%s is not in the read profile", id)
		}
	}
	for _, id := range []string{keyEventsCreateID, keyEventsUpdateID, keyEventsDeleteID} {
		if slices.Contains(metadata.Profiles[0].Tools, id) || !slices.Contains(metadata.Profiles[1].Tools, id) {
			t.Errorf("%s has the wrong profiles", id)
		}
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
	for got, wants := range map[string][]string{
		reg.ConfirmationSummary(keyEventsCreateID)(json.RawMessage(`{"property":"properties/200","event_name":"purchase","counting_method":"ONCE_PER_EVENT"}`)):                                                {"Create", "purchase", "properties/200", "ONCE_PER_EVENT"},
		reg.ConfirmationSummary(keyEventsUpdateID)(json.RawMessage(`{"name":"properties/200/keyEvents/3001","counting_method":"ONCE_PER_SESSION","default_value":{"numeric_value":1,"currency_code":"EUR"}}`)): {"counting_method ONCE_PER_SESSION", "default_value", "properties/200/keyEvents/3001"},
		reg.ConfirmationSummary(keyEventsDeleteID)(json.RawMessage(`{"name":"properties/200/keyEvents/3001"}`)):                                                                                                {"Delete", "properties/200/keyEvents/3001"},
	} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}
