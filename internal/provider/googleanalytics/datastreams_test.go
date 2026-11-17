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

// streamEnv adds connections that list the four stream tools, with the bindings of the property tests.
func streamEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{dataStreamsListID, dataStreamsGetID, dataStreamsCreateID, dataStreamsUpdateID}
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
	webStreamJSON = `{"name":"properties/200/dataStreams/2001","type":"WEB_DATA_STREAM","displayName":"Shop web",` +
		`"createTime":"2026-11-01T00:00:00Z","updateTime":"2026-11-02T00:00:00Z","webStreamData":` +
		`{"measurementId":"G-SAMPLE0001","firebaseAppId":"1:100:web:abc","defaultUri":"https://shop.example.com"},"extra":1}`
	androidStreamJSON = `{"name":"properties/200/dataStreams/2002","type":"ANDROID_APP_DATA_STREAM",` +
		`"displayName":"Shop android","androidAppStreamData":{"packageName":"com.example.shop","firebaseAppId":"1:100:android:abc"}}`
	iosStreamJSON = `{"name":"properties/200/dataStreams/2003","type":"IOS_APP_DATA_STREAM",` +
		`"displayName":"Shop ios","iosAppStreamData":{"bundleId":"com.example.shop","firebaseAppId":"1:100:ios:abc"}}`
)

type streamsOut struct {
	DataStreams []dataStream `json:"dataStreams"`
	NextCursor  string       `json:"next_cursor"`
	HasMore     bool         `json:"has_more"`
}

func TestDataStreamsListPagesThroughThreePages(t *testing.T) {
	pages := []string{
		`{"dataStreams":[` + webStreamJSON + `],"nextPageToken":"t2"}`,
		`{"dataStreams":[` + androidStreamJSON + `],"nextPageToken":"t3"}`,
		`{"dataStreams":[` + iosStreamJSON + `]}`,
	}
	env := streamEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	cursor, seen := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(dataStreamsListID, "sfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		var out streamsOut
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.DataStreams) != 1 ||
			out.HasMore != (i < 2) {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		seen = append(seen, string(out.DataStreams[0].Type))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(seen, []string{"WEB_DATA_STREAM", "ANDROID_APP_DATA_STREAM", "IOS_APP_DATA_STREAM"}) ||
		len(requests) != 3 || requests[0].path != "/v1beta/properties/200/dataStreams" ||
		requests[0].rawQuery != "pageSize=1" || !strings.Contains(requests[1].rawQuery, "pageToken=t2") ||
		!strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("seen %v, requests %+v", seen, requests)
	}
	if _, err := env.call(dataStreamsListID, "sfree", `{"property":"properties/201","cursor":"`+cursor+`"}`); err == nil && cursor != "" {
		t.Error("a cursor of another property was accepted")
	}
}

func TestDataStreamsReadAllThreeTypes(t *testing.T) {
	answers := map[string]string{"2001": webStreamJSON, "2002": androidStreamJSON, "2003": iosStreamJSON}
	env := streamEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", answers[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]), nil
	}})
	var got [3]dataStream
	for i, id := range []string{"2001", "2002", "2003"} {
		result, err := env.call(dataStreamsGetID, "sfree", `{"name":"properties/200/dataStreams/`+id+`"}`)
		if err != nil || json.Unmarshal([]byte(result), &got[i]) != nil || strings.Contains(result, "extra") {
			t.Fatalf("%s: %v %s", id, err, result)
		}
	}
	web, android, ios := got[0], got[1], got[2]
	if web.Type != "WEB_DATA_STREAM" || web.DisplayName != "Shop web" || web.CreateTime == "" || web.UpdateTime == "" ||
		web.WebStreamData == nil || web.WebStreamData.MeasurementID != "G-SAMPLE0001" ||
		web.WebStreamData.FirebaseAppID != "1:100:web:abc" || web.WebStreamData.DefaultURI != "https://shop.example.com" ||
		android.AndroidAppStreamData == nil || android.AndroidAppStreamData.PackageName != "com.example.shop" ||
		android.AndroidAppStreamData.FirebaseAppID == "" || android.WebStreamData != nil ||
		ios.IosAppStreamData == nil || ios.IosAppStreamData.BundleID != "com.example.shop" ||
		ios.IosAppStreamData.FirebaseAppID == "" {
		t.Errorf("streams = %+v %+v %+v", web, android, ios)
	}
}

func TestDataStreamsAnswerIsBoundedAndChecked(t *testing.T) {
	long := strings.Repeat("a", 5000)
	env := streamEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"dataStreams":[`+
		`{"name":"properties/200/dataStreams/1","displayName":"`+strings.Repeat("n", 400)+`","webStreamData":{"defaultUri":"https://`+long+`"}},`+
		`{"name":"properties/999/dataStreams/2"},{"name":"nonsense"},{"displayName":"unnamed"}]}`)})
	result, err := env.call(dataStreamsListID, "sfree", `{"property":"properties/200"}`)
	var out streamsOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.DataStreams) != 1 ||
		len([]rune(string(out.DataStreams[0].DisplayName))) != maxTextRunes+1 ||
		len([]rune(string(out.DataStreams[0].WebStreamData.DefaultURI))) != maxAddressRunes+1 {
		t.Errorf("result %v, %v", len(result), err)
	}
	// A get that answers with another stream is not trusted.
	env = streamEnv(t, &fakeGoogle{api: always(http.StatusOK, webStreamJSON)})
	if _, err := env.call(dataStreamsGetID, "sfree", `{"name":"properties/200/dataStreams/9"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("error = %v", err)
	}
}

func TestDataStreamsCreateForEachType(t *testing.T) {
	for _, c := range []struct{ args, answer, body string }{
		{`{"property":"properties/200","type":"WEB_DATA_STREAM","display_name":"Shop web","default_uri":"https://shop.example.com"}`,
			webStreamJSON, `{"type":"WEB_DATA_STREAM","displayName":"Shop web","webStreamData":{"defaultUri":"https://shop.example.com"}}`},
		{`{"property":"properties/200","type":"ANDROID_APP_DATA_STREAM","display_name":"Shop android","package_name":"com.example.shop"}`,
			androidStreamJSON, `{"type":"ANDROID_APP_DATA_STREAM","displayName":"Shop android","androidAppStreamData":{"packageName":"com.example.shop"}}`},
		{`{"property":"properties/200","type":"IOS_APP_DATA_STREAM","display_name":"Shop ios","bundle_id":"com.example.shop"}`,
			iosStreamJSON, `{"type":"IOS_APP_DATA_STREAM","displayName":"Shop ios","iosAppStreamData":{"bundleId":"com.example.shop"}}`},
	} {
		env := streamEnv(t, &fakeGoogle{api: always(http.StatusOK, c.answer)})
		if _, err := env.call(dataStreamsCreateID, "sfree", c.args); err == nil {
			t.Fatal("create without confirm was accepted")
		}
		env.noIO(t)
		result, err := env.confirmed(dataStreamsCreateID, "sfree", c.args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
			requests[0].path != "/v1beta/properties/200/dataStreams" || requests[0].rawQuery != "" ||
			requests[0].body != c.body || requests[0].contentType != "application/json" {
			t.Fatalf("%v, %+v", err, requests)
		}
		if strings.Contains(result, "measurementId") || strings.Contains(result, "G-SAMPLE") {
			t.Errorf("a changing tool returned the measurement ID: %s", result)
		}
	}
}

func TestDataStreamsCreateRefusesMissingOrWrongTypeData(t *testing.T) {
	env := streamEnv(t, &fakeGoogle{api: always(http.StatusOK, webStreamJSON)})
	base := `{"property":"properties/200","display_name":"S",`
	for name, args := range map[string]string{
		"web without data":     base + `"type":"WEB_DATA_STREAM"}`,
		"android without data": base + `"type":"ANDROID_APP_DATA_STREAM"}`,
		"ios without data":     base + `"type":"IOS_APP_DATA_STREAM"}`,
		"web with package":     base + `"type":"WEB_DATA_STREAM","default_uri":"https://a.example","package_name":"com.example.a"}`,
		"android with uri":     base + `"type":"ANDROID_APP_DATA_STREAM","package_name":"com.example.a","default_uri":"https://a.example"}`,
		"unspecified type":     base + `"type":"DATA_STREAM_TYPE_UNSPECIFIED","default_uri":"https://a.example"}`,
		"bad uri scheme":       base + `"type":"WEB_DATA_STREAM","default_uri":"javascript:alert(1)"}`,
		"uri with user info":   base + `"type":"WEB_DATA_STREAM","default_uri":"https://user` + "@" + `a.example"}`,
		"uri with blank":       base + `"type":"WEB_DATA_STREAM","default_uri":"https://a.example/ x"}`,
		"bad package":          base + `"type":"ANDROID_APP_DATA_STREAM","package_name":"nodots"}`,
		"bad bundle":           base + `"type":"IOS_APP_DATA_STREAM","bundle_id":"-x"}`,
		"blank name":           `{"property":"properties/200","display_name":"  ","type":"WEB_DATA_STREAM","default_uri":"https://a.example"}`,
		"long name":            `{"property":"properties/200","display_name":"` + strings.Repeat("n", 256) + `","type":"WEB_DATA_STREAM","default_uri":"https://a.example"}`,
		"no name":              `{"property":"properties/200","type":"WEB_DATA_STREAM","default_uri":"https://a.example"}`,
		"bad property":         `{"property":"accounts/100","display_name":"S","type":"WEB_DATA_STREAM","default_uri":"https://a.example"}`,
	} {
		if _, err := env.confirmed(dataStreamsCreateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
	if _, err := env.confirmed(dataStreamsCreateID, "sfree",
		base+`"type":"WEB_DATA_STREAM","default_uri":"https://a.example"}`); err != nil {
		t.Errorf("valid create refused: %v", err)
	}
}

func TestDataStreamsUpdateMaskComesFromSetArguments(t *testing.T) {
	for _, c := range []struct{ args, mask, body string }{
		{`,"display_name":"New"`, "display_name", `{"displayName":"New"}`},
		{`,"default_uri":"https://new.example.com"`, "web_stream_data.default_uri",
			`{"webStreamData":{"defaultUri":"https://new.example.com"}}`},
		{`,"default_uri":"https://new.example.com","display_name":"New"`, "display_name,web_stream_data.default_uri",
			`{"displayName":"New","webStreamData":{"defaultUri":"https://new.example.com"}}`},
	} {
		env := streamEnv(t, &fakeGoogle{api: always(http.StatusOK, webStreamJSON)})
		args := `{"name":"properties/200/dataStreams/2001"` + c.args + `}`
		if _, err := env.call(dataStreamsUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		result, err := env.confirmed(dataStreamsUpdateID, "sfree", args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != "/v1beta/properties/200/dataStreams/2001" ||
			requests[0].rawQuery != "updateMask="+strings.NewReplacer(",", "%2C").Replace(c.mask) ||
			requests[0].body != c.body || strings.Contains(result, "G-SAMPLE") {
			t.Fatalf("%v, %+v, %s", err, requests, result)
		}
	}
	env := streamEnv(t, &fakeGoogle{api: always(http.StatusOK, webStreamJSON)})
	for name, args := range map[string]string{
		"no field": `{"name":"properties/200/dataStreams/2001"}`,
		"empty":    `{"name":"properties/200/dataStreams/2001","display_name":""}`,
		"type":     `{"name":"properties/200/dataStreams/2001","type":"WEB_DATA_STREAM"}`,
		"package":  `{"name":"properties/200/dataStreams/2001","package_name":"com.example.a"}`,
		"bundle":   `{"name":"properties/200/dataStreams/2001","bundle_id":"com.example.a"}`,
		"bad name": `{"name":"properties/200","display_name":"x"}`,
		"bad uri":  `{"name":"properties/200/dataStreams/2001","default_uri":"ftp://a.example"}`,
	} {
		if _, err := env.confirmed(dataStreamsUpdateID, "sfree", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.noIO(t)
}

var streamBindingCases = []struct {
	connection, property string
	methods              []string
	refused              bool
}{
	{"sfree", "properties/200", []string{"X"}, false},
	{"sproperty", "properties/200", []string{"X"}, false},
	{"sproperty", "properties/201", nil, true},
	{"sproperty", "properties/901", nil, true},
	{"saccount", "properties/201", []string{"GET", "X"}, false},
	{"saccount", "properties/901", []string{"GET"}, true},
	{"smixed", "properties/901", []string{"X"}, false},
	{"smixed", "properties/200", []string{"GET", "X"}, false},
}

func TestDataStreamsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{dataStreamsListID, `{"property":"PROP"}`, "GET"},
		{dataStreamsGetID, `{"name":"PROP/dataStreams/2001"}`, "GET"},
		{dataStreamsCreateID, `{"property":"PROP","type":"WEB_DATA_STREAM","display_name":"S","default_uri":"https://a.example"}`, "POST"},
		{dataStreamsUpdateID, `{"name":"PROP/dataStreams/2001","display_name":"S"}`, "PATCH"},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			property := c.property
			env := streamEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimPrefix(r.URL.Path, "/v1beta/")
					if r.Method == http.MethodPost {
						name += "/9"
					}
					body = `{"dataStreams":[],"name":"` + name + `"}`
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

func TestDataStreamErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct{ id, arguments string }{
		{dataStreamsListID, `{"property":"properties/200"}`},
		{dataStreamsGetID, `{"name":"properties/200/dataStreams/2001"}`},
		{dataStreamsCreateID, `{"property":"properties/200","type":"IOS_APP_DATA_STREAM","display_name":"S","bundle_id":"com.example.a"}`},
		{dataStreamsUpdateID, `{"name":"properties/200/dataStreams/2001","display_name":"S"}`},
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
				env := streamEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(call.id, "sfree", call.arguments)
				if classOf(err) != c.class || leaks(err.Error()) != "" {
					t.Fatalf("error = %v", err)
				}
				write := call.id == dataStreamsCreateID || call.id == dataStreamsUpdateID
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

func TestDataStream429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := streamEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", webStreamJSON), nil
	}})
	if _, err := env.call(dataStreamsGetID, "sfree", `{"name":"properties/200/dataStreams/2001"}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(dataStreamsGetID, "sfree", `{"name":"properties/200/dataStreams/2001"}`); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestDataStreamWritesDistrustAnAnswerOfAnotherStream(t *testing.T) {
	env := streamEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/dataStreams/1"}`)})
	if _, err := env.confirmed(dataStreamsUpdateID, "sfree", `{"name":"properties/200/dataStreams/2001","display_name":"S"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	if _, err := env.confirmed(dataStreamsCreateID, "sfree", `{"property":"properties/200","type":"WEB_DATA_STREAM","display_name":"S","default_uri":"https://a.example"}`); classOf(err) != provider.ClassProviderError {
		t.Errorf("create: %v", err)
	}
}

func TestDataStreamToolsAreRegisteredWithScopesProfilesAndSummaries(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, want := range map[string]string{dataStreamsListID: scopeReadOnly, dataStreamsGetID: scopeReadOnly,
		dataStreamsCreateID: scopeEdit, dataStreamsUpdateID: scopeEdit} {
		if !slices.Equal(metadata.Token.Scopes[id], []string{want}) {
			t.Errorf("%s scopes = %v", id, metadata.Token.Scopes[id])
		}
	}
	for _, id := range []string{dataStreamsCreateID, dataStreamsUpdateID} {
		if reg.ConfirmationSummary(id) == nil {
			t.Errorf("%s has no confirmation summary", id)
		}
	}
	create := reg.ConfirmationSummary(dataStreamsCreateID)(json.RawMessage(
		`{"property":"properties/200","type":"WEB_DATA_STREAM","display_name":"Shop","default_uri":"https://a.example"}`))
	update := reg.ConfirmationSummary(dataStreamsUpdateID)(json.RawMessage(
		`{"name":"properties/200/dataStreams/2001","default_uri":"https://a.example","display_name":"X"}`))
	for got, wants := range map[string][]string{create: {"Create", "WEB_DATA_STREAM", "Shop", "properties/200", "https://a.example"},
		update: {"display_name, web_stream_data.default_uri", "properties/200/dataStreams/2001"}} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}
