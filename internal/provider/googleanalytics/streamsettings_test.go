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

const settingsStream = "properties/200/dataStreams/2001"

var settingTools = []string{enhancedGetID, enhancedUpdateID, redactionGetID, redactionUpdateID, siteTagGetID}

// alphaTools are the tools on the Admin API v1alpha; a tool added there joins this list.
var alphaTools = settingTools

// settingsEnv adds connections that offer the settings tools, with the bindings of the stream tests.
func settingsEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets,
			Tools: settingTools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	enhancedAnswer = `{"name":"` + settingsStream + `/enhancedMeasurementSettings","streamEnabled":true,` +
		`"scrollsEnabled":false,"outboundClicksEnabled":true,"siteSearchEnabled":false,"videoEngagementEnabled":true,` +
		`"fileDownloadsEnabled":false,"pageChangesEnabled":true,"formInteractionsEnabled":false,` +
		`"searchQueryParameter":"q,s","uriQueryParameter":"utm","extra":1}`
	redactionAnswer = `{"name":"` + settingsStream + `/dataRedactionSettings","emailRedactionEnabled":true,` +
		`"queryParameterRedactionEnabled":false,"queryParameterKeys":["token","Email"],"extra":1}`
	siteTagAnswer = `{"name":"` + settingsStream + `/globalSiteTag","snippet":"<script>alert(1)</script>"}`
)

func TestStreamSettingsReadEveryMemberOnV1alpha(t *testing.T) {
	for _, c := range []struct{ id, suffix, answer string }{
		{enhancedGetID, "/enhancedMeasurementSettings", enhancedAnswer},
		{redactionGetID, "/dataRedactionSettings", redactionAnswer},
		{siteTagGetID, "/globalSiteTag", siteTagAnswer},
	} {
		env := settingsEnv(t, &fakeGoogle{api: always(http.StatusOK, c.answer)})
		result, err := env.call(c.id, "sfree", `{"name":"`+settingsStream+`"}`)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodGet ||
			requests[0].path != alphaPrefix+settingsStream+c.suffix || requests[0].rawQuery != "" ||
			strings.Contains(result, "extra") {
			t.Fatalf("%s: %v, %+v, %s", c.id, err, requests, result)
		}
		var got, want map[string]any
		if json.Unmarshal([]byte(result), &got) != nil || json.Unmarshal([]byte(c.answer), &want) != nil {
			t.Fatalf("%s: result %s", c.id, result)
		}
		delete(want, "extra")
		if !jsonEqual(got, want) {
			t.Errorf("%s: result %s, want %s", c.id, result, c.answer)
		}
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestSiteTagSnippetIsReturnedAsDataAndBounded(t *testing.T) {
	long := strings.Repeat("a", maxSnippetRunes+50)
	env := settingsEnv(t, &fakeGoogle{api: always(http.StatusOK,
		`{"name":"`+settingsStream+`/globalSiteTag","snippet":"`+long+`"}`)})
	result, err := env.call(siteTagGetID, "sfree", `{"name":"`+settingsStream+`"}`)
	var out siteTag
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len([]rune(string(out.Snippet))) != maxSnippetRunes+1 {
		t.Fatalf("%v, %d", err, len(result))
	}
	// Only the stream request goes out: the snippet is never followed.
	if requests, _ := env.fake.seen(); len(requests) != 1 {
		t.Errorf("requests = %d", len(requests))
	}
}

func TestSettingsReadsRefuseAnswersForOtherSettings(t *testing.T) {
	for _, id := range []string{enhancedGetID, redactionGetID, siteTagGetID} {
		env := settingsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/dataStreams/1/globalSiteTag"}`)})
		if _, err := env.call(id, "sfree", `{"name":"`+settingsStream+`"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestRedactionKeysAreBounded(t *testing.T) {
	keys := make([]string, maxRedactionKeys+5)
	for i := range keys {
		keys[i] = `"k"`
	}
	env := settingsEnv(t, &fakeGoogle{api: always(http.StatusOK,
		`{"name":"`+settingsStream+`/dataRedactionSettings","queryParameterKeys":[`+strings.Join(keys, ",")+`]}`)})
	result, err := env.call(redactionGetID, "sfree", `{"name":"`+settingsStream+`"}`)
	var out dataRedaction
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.QueryParameterKeys) != maxRedactionKeys {
		t.Errorf("%v, %d keys", err, len(out.QueryParameterKeys))
	}
}

func TestEnhancedUpdateMaskComesFromSetArguments(t *testing.T) {
	for _, c := range []struct{ args, mask, body string }{
		{`"scrolls_enabled":false`, "scrolls_enabled", `{"scrollsEnabled":false}`},
		{`"stream_enabled":true,"file_downloads_enabled":true`, "stream_enabled,file_downloads_enabled",
			`{"streamEnabled":true,"fileDownloadsEnabled":true}`},
		{`"uri_query_parameter":""`, "uri_query_parameter", `{"uriQueryParameter":""}`},
		{`"search_query_parameter":"q","site_search_enabled":true,"outbound_clicks_enabled":false,` +
			`"video_engagement_enabled":false,"page_changes_enabled":true,"form_interactions_enabled":true`,
			"outbound_clicks_enabled,site_search_enabled,video_engagement_enabled,page_changes_enabled," +
				"form_interactions_enabled,search_query_parameter",
			`{"outboundClicksEnabled":false,"siteSearchEnabled":true,"videoEngagementEnabled":false,` +
				`"pageChangesEnabled":true,"formInteractionsEnabled":true,"searchQueryParameter":"q"}`},
	} {
		env := settingsEnv(t, &fakeGoogle{api: always(http.StatusOK, enhancedAnswer)})
		args := `{"name":"` + settingsStream + `",` + c.args + `}`
		if _, err := env.call(enhancedUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		result, err := env.confirmed(enhancedUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != alphaPrefix+settingsStream+"/enhancedMeasurementSettings" ||
			requests[0].rawQuery != "updateMask="+strings.ReplaceAll(c.mask, ",", "%2C") ||
			requests[0].body != c.body || !strings.Contains(result, `"streamEnabled":true`) {
			t.Fatalf("%v, %+v, %s", err, requests, result)
		}
	}
}

func TestRedactionUpdateMaskComesFromSetArguments(t *testing.T) {
	for _, c := range []struct{ args, mask, body string }{
		{`"email_redaction_enabled":true`, "email_redaction_enabled", `{"emailRedactionEnabled":true}`},
		{`"query_parameter_keys":["a","B"],"query_parameter_redaction_enabled":true`,
			"query_parameter_redaction_enabled,query_parameter_keys",
			`{"queryParameterRedactionEnabled":true,"queryParameterKeys":["a","B"]}`},
		{`"query_parameter_keys":[]`, "query_parameter_keys", `{"queryParameterKeys":[]}`},
	} {
		env := settingsEnv(t, &fakeGoogle{api: always(http.StatusOK, redactionAnswer)})
		args := `{"name":"` + settingsStream + `",` + c.args + `}`
		if _, err := env.call(redactionUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		_, err := env.confirmed(redactionUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != alphaPrefix+settingsStream+"/dataRedactionSettings" ||
			requests[0].rawQuery != "updateMask="+strings.ReplaceAll(c.mask, ",", "%2C") || requests[0].body != c.body {
			t.Fatalf("%v, %+v", err, requests)
		}
	}
}

func TestSettingsUpdatesRefuseBadArgumentsBeforeAnyIO(t *testing.T) {
	env := settingsEnv(t, &fakeGoogle{api: always(http.StatusOK, enhancedAnswer)})
	n := `{"name":"` + settingsStream + `"`
	for _, c := range []struct{ id, args string }{
		{enhancedUpdateID, n + `}`},
		{enhancedUpdateID, `{"name":"properties/200","scrolls_enabled":true}`},
		{enhancedUpdateID, n + `,"search_query_parameter":""}`},
		{enhancedUpdateID, n + `,"search_query_parameter":"  "}`},
		{enhancedUpdateID, n + `,"search_query_parameter":"` + strings.Repeat("q", 1025) + `"}`},
		{enhancedUpdateID, n + `,"uri_query_parameter":"a\u0001"}`},
		{enhancedUpdateID, n + `,"scrolls_enabled":"yes"}`},
		{enhancedUpdateID, n + `,"unknown":true}`},
		{redactionUpdateID, n + `}`},
		{redactionUpdateID, n + `,"query_parameter_keys":["a,b"]}`},
		{redactionUpdateID, n + `,"query_parameter_keys":[" "]}`},
		{redactionUpdateID, n + `,"query_parameter_keys":["a\nb"]}`},
		{redactionUpdateID, n + `,"query_parameter_keys":["` + strings.Repeat("k", 257) + `"]}`},
		{redactionUpdateID, n + `,"query_parameter_keys":[` + strings.TrimSuffix(strings.Repeat(`"k",`, 101), ",") + `]}`},
		{redactionUpdateID, `{"name":"nonsense","email_redaction_enabled":true}`},
	} {
		if _, err := env.confirmed(c.id, "sfree", c.args); err == nil {
			t.Errorf("%s %s: accepted", c.id, c.args)
		}
	}
	env.noIO(t)
}

var settingsBindingTools = []struct{ id, arguments, method string }{
	{enhancedGetID, `{"name":"PROP/dataStreams/2001"}`, "GET"},
	{enhancedUpdateID, `{"name":"PROP/dataStreams/2001","scrolls_enabled":true}`, "PATCH"},
	{redactionGetID, `{"name":"PROP/dataStreams/2001"}`, "GET"},
	{redactionUpdateID, `{"name":"PROP/dataStreams/2001","email_redaction_enabled":true}`, "PATCH"},
	{siteTagGetID, `{"name":"PROP/dataStreams/2001"}`, "GET"},
}

func TestSettingsToolsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range settingsBindingTools {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := settingsEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					body = `{"name":"` + strings.TrimPrefix(r.URL.Path, alphaPrefix) + `"}`
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

func TestSettingsErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	appStream := `{"error":{"code":400,"message":"` + detailCanary + `","status":"INVALID_ARGUMENT"}}`
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
		{"app stream", always(http.StatusBadRequest, appStream), false, ""},
	} {
		for _, tool := range settingsBindingTools {
			t.Run(c.name+" "+tool.id, func(t *testing.T) {
				env := settingsEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(tool.id, "sfree", strings.ReplaceAll(tool.arguments, "PROP", "properties/200"))
				if err == nil || leaks(err.Error()) != "" || strings.Contains(err.Error(), detailCanary) {
					t.Fatalf("error = %v", err)
				}
				if c.class != "" && classOf(err) != c.class {
					t.Errorf("class = %s, want %s", classOf(err), c.class)
				}
				if c.name == "app stream" && classOf(err) == provider.ClassOK {
					t.Errorf("an app stream error was not reported: %v", err)
				}
				write := tool.method == "PATCH"
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

func TestSettings429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := settingsEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", enhancedAnswer), nil
	}})
	args := `{"name":"` + settingsStream + `"}`
	if _, err := env.call(enhancedGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(enhancedGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestSettingsUpdatesDistrustAnAnswerForOtherSettings(t *testing.T) {
	env := settingsEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/dataStreams/1/dataRedactionSettings"}`)})
	_, err := env.confirmed(redactionUpdateID, "sfree", `{"name":"`+settingsStream+`","email_redaction_enabled":true}`)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error = %v", err)
	}
}

func TestSettingsToolsAreRegisteredAsAlphaWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, want := range map[string]string{enhancedGetID: scopeReadOnly, redactionGetID: scopeReadOnly,
		siteTagGetID: scopeReadOnly, enhancedUpdateID: scopeEdit, redactionUpdateID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
	}
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range alphaTools {
		descriptor, _, ok := reg.Lookup(id)
		if !ok || !strings.HasSuffix(descriptor.Description, alphaNote) {
			t.Errorf("%s lacks the alpha note", id)
		}
		write := id == enhancedUpdateID || id == redactionUpdateID
		if slices.Contains(profiles["read"], id) == write || !slices.Contains(profiles["write"], id) {
			t.Errorf("%s profiles are wrong", id)
		}
		if write && reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
	for _, id := range []string{enhancedUpdateID, redactionUpdateID} {
		descriptor, _, _ := reg.Lookup(id)
		if !strings.Contains(descriptor.Description, "which data Google collects or discards") {
			t.Errorf("%s does not name its effect", id)
		}
	}
	if descriptor, _, _ := reg.Lookup(redactionUpdateID); !strings.Contains(descriptor.Description, "from then on") {
		t.Error("update-data-redaction does not say that it acts on future data")
	}
	summary := reg.ConfirmationSummary(enhancedUpdateID)(json.RawMessage(
		`{"name":"` + settingsStream + `","scrolls_enabled":false,"stream_enabled":true}`))
	for _, want := range []string{"stream_enabled, scrolls_enabled", settingsStream} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q lacks %q", summary, want)
		}
	}
}
