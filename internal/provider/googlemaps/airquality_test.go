package googlemaps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	airHost     = "airquality.googleapis.com"
	airLocation = `"location":{"latitude":37.422,"longitude":-122.0841}`
	pageToken   = "page-token-canary-93ab"
)

// airRewrite sends the Air Quality host to the test server and refuses every other host.
type airRewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (r airRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != airHost || req.URL.Scheme != "https" {
		return nil, errors.New("unexpected host")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set(originalHost, req.URL.Host)
	clone.URL.Scheme, clone.URL.Host, clone.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(clone)
}

func newAirEnvironment(t *testing.T, f *fakeGoogle) *environment {
	t.Helper()
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	previous := transport
	transport = airRewrite{target: target, base: server.Client().Transport}
	t.Cleanup(func() { transport = previous })
	t.Cleanup(limiters.Replace(keyValue, ratelimit.New(0, time.Now, func(context.Context, time.Duration) error { return nil })))
	reads := 0
	red := &redact.Redactor{}
	lookup := func(name string) string {
		reads++
		if name == keyEnv {
			return keyValue
		}
		return ""
	}
	return &environment{fake: f, core: application.New(registry(t), coreConfig(), secret.NewWith(lookup, nil, nil, red), red),
		red: red, reads: &reads, t: t}
}

const airIndexes = `"indexes":[{"code":"uaqi","displayName":"Universal AQI","aqi":71,"aqiDisplay":"71",` +
	`"color":{"red":0.6,"green":0.8,"alpha":1},"category":"Good air quality","dominantPollutant":"pm25","surprise":1}]`

const airCurrentAnswer = `{"dateTime":"2026-11-10T09:00:00Z","regionCode":"us",` + airIndexes + `,` +
	`"pollutants":[{"code":"pm25","displayName":"PM2.5","fullName":"Fine particulate matter (<2.5µm)",` +
	`"concentration":{"value":6.5,"units":"MICROGRAMS_PER_CUBIC_METER"},` +
	`"additionalInfo":{"sources":"Fires","effects":"Lungs"}}],` +
	`"healthRecommendations":{"generalPopulation":"Enjoy the outdoors.","elderly":"Fine.","lungDiseasePopulation":"a",` +
	`"heartDiseasePopulation":"b","athletes":"c","pregnantWomen":"d","children":"e","extra":"x"}}`

func airBodyOf(t *testing.T, raw string) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	return body
}

func TestAirQualityCurrentWithoutExtraComputations(t *testing.T) {
	env := newAirEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", airCurrentAnswer)})
	result, err := env.invokeTool(airQualityCurrentID, "read", `{`+airLocation+`}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	requests := env.fake.recorded()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	r := requests[0]
	if r.method != http.MethodPost || r.path != "/v1/currentConditions:lookup" || r.rawQuery != "" ||
		r.header.Get(originalHost) != airHost || r.header.Get("X-Goog-Api-Key") != keyValue ||
		r.header.Get("X-Goog-FieldMask") != "" || r.header.Get("Content-Type") != "application/json" {
		t.Errorf("request = %+v", r)
	}
	if r.body != `{"location":{"latitude":37.422,"longitude":-122.0841}}` {
		t.Errorf("body = %s", r.body)
	}
	for _, want := range []string{`"regionCode":"us"`, `"aqi":71`, `"aqiDisplay":"71"`, `"code":"pm25"`, `"units":"MICROGRAMS_PER_CUBIC_METER"`,
		`"generalPopulation":"Enjoy the outdoors."`, `"sources":"Fires"`, `"alpha":1`} {
		if !strings.Contains(result, want) {
			t.Errorf("result lacks %s: %s", want, result)
		}
	}
	for _, banned := range []string{"surprise", `"extra"`, keyValue, "next_cursor", "has_more"} {
		if strings.Contains(result, banned) {
			t.Errorf("result contains %q", banned)
		}
	}
}

func TestAirQualityCurrentSendsEveryDocumentedParameter(t *testing.T) {
	env := newAirEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", airCurrentAnswer)})
	_, err := env.invokeTool(airQualityCurrentID, "read", `{`+airLocation+`,`+
		`"extra_computations":["LOCAL_AQI","HEALTH_RECOMMENDATIONS","POLLUTANT_ADDITIONAL_INFO",`+
		`"DOMINANT_POLLUTANT_CONCENTRATION","POLLUTANT_CONCENTRATION"],"universal_aqi":false,`+
		`"uaqi_color_palette":"INDIGO_PERSIAN_DARK","language_code":"pt-BR",`+
		`"custom_local_aqis":[{"region_code":"ca","aqi":"usa_epa"}]}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	want := `{"customLocalAqis":[{"aqi":"usa_epa","regionCode":"CA"}],` +
		`"extraComputations":["LOCAL_AQI","HEALTH_RECOMMENDATIONS","POLLUTANT_ADDITIONAL_INFO",` +
		`"DOMINANT_POLLUTANT_CONCENTRATION","POLLUTANT_CONCENTRATION"],"languageCode":"pt-BR",` +
		`"location":{"latitude":37.422,"longitude":-122.0841},"uaqiColorPalette":"INDIGO_PERSIAN_DARK","universalAqi":false}`
	if got, _ := json.Marshal(airBodyOf(t, env.fake.recorded()[0].body)); string(got) != want {
		t.Errorf("body = %s", got)
	}
}

const forecastPage1 = `{"hourlyForecasts":[{"dateTime":"2026-11-11T10:00:00Z",` + airIndexes + `},` +
	`{"dateTime":"2026-11-11T11:00:00Z","pollutants":[{"code":"o3"}]}],"regionCode":"US","nextPageToken":"` + pageToken + `"}`

const forecastPage2 = `{"hourlyForecasts":[{"dateTime":"2026-11-11T12:00:00Z"}],"regionCode":"US"}`

func nextCursor(t *testing.T, result string) string {
	t.Helper()
	var page struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(result), &page); err != nil {
		t.Fatalf("result: %v", err)
	}
	if !page.HasMore || page.NextCursor == "" {
		t.Fatalf("no further page: %s", result)
	}
	if strings.Contains(result, pageToken) || strings.Contains(result, "nextPageToken") {
		t.Errorf("the Google token is shown: %s", result)
	}
	return page.NextCursor
}

func paged(first, second string) func(http.ResponseWriter, *http.Request) {
	calls := 0
	return func(w http.ResponseWriter, r *http.Request) {
		calls++
		answer := first
		if calls > 1 {
			answer = second
		}
		reply(200, "application/json", answer)(w, r)
	}
}

func TestAirQualityForecastPagesThroughTheCursor(t *testing.T) {
	env := newAirEnvironment(t, &fakeGoogle{answer: paged(forecastPage1, forecastPage2)})
	arguments := `{` + airLocation + `,"period":{"start_time":"2026-11-11T10:00:00Z","end_time":"2026-11-11T13:00:00Z"},` +
		`"page_size":2,"extra_computations":["LOCAL_AQI"]`
	first, err := env.invokeTool(airQualityForecastID, "read", arguments+`}`)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	cursor := nextCursor(t, first)
	if !strings.Contains(first, `"hourlyForecasts"`) || !strings.Contains(first, `"code":"o3"`) {
		t.Errorf("first page = %s", first)
	}
	second, err := env.invokeTool(airQualityForecastID, "read", arguments+`,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if !strings.Contains(second, `"has_more":false`) || strings.Contains(second, "next_cursor") {
		t.Errorf("second page = %s", second)
	}
	requests := env.fake.recorded()
	if len(requests) != 2 || requests[0].path != "/v1/forecast:lookup" {
		t.Fatalf("requests = %+v", requests)
	}
	one, two := airBodyOf(t, requests[0].body), airBodyOf(t, requests[1].body)
	if _, ok := one["pageToken"]; ok {
		t.Errorf("first body has a page token: %v", one)
	}
	if two["pageToken"] != pageToken || two["pageSize"] != float64(2) {
		t.Errorf("second body = %v", two)
	}
	delete(two, "pageToken")
	if a, b := mustJSON(t, one), mustJSON(t, two); a != b {
		t.Errorf("the second body changed besides the token: %s vs %s", a, b)
	}
	if strings.Contains(requests[1].body, cursor) {
		t.Error("the Qatlas cursor was sent to Google")
	}
}

func TestAirQualityHistoryPagesAndSendsHours(t *testing.T) {
	answer := strings.Replace(strings.Replace(forecastPage1, "hourlyForecasts", "hoursInfo", 1), "2026-11-11", "2026-11-09", -1)
	env := newAirEnvironment(t, &fakeGoogle{answer: paged(answer, `{"hoursInfo":[{"dateTime":"2026-11-09T12:00:00Z"}]}`)})
	arguments := `{` + airLocation + `,"hours":48,"page_size":24,"universal_aqi":true`
	first, err := env.invokeTool(airQualityHistoryID, "read", arguments+`}`)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if !strings.Contains(first, `"hoursInfo"`) || !strings.Contains(first, `"regionCode":"US"`) {
		t.Errorf("first page = %s", first)
	}
	if _, err := env.invokeTool(airQualityHistoryID, "read", arguments+`,"cursor":"`+nextCursor(t, first)+`"}`); err != nil {
		t.Fatalf("second page: %v", err)
	}
	requests := env.fake.recorded()
	if requests[0].path != "/v1/history:lookup" {
		t.Errorf("path = %s", requests[0].path)
	}
	if body := airBodyOf(t, requests[1].body); body["pageToken"] != pageToken || body["hours"] != float64(48) ||
		body["pageSize"] != float64(24) || body["universalAqi"] != true {
		t.Errorf("second body = %v", body)
	}
}

func TestAirQualityHistoryAndForecastTimeSelectors(t *testing.T) {
	env := newAirEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{"hoursInfo":[{"dateTime":"2026-11-09T12:00:00Z"}]}`)})
	if _, err := env.invokeTool(airQualityHistoryID, "read", `{`+airLocation+`,"date_time":"2026-11-09T12:05:49.5Z"}`); err != nil {
		t.Fatalf("date_time: %v", err)
	}
	if body := env.fake.recorded()[0].body; !strings.Contains(body, `"dateTime":"2026-11-09T12:05:49.5Z"`) {
		t.Errorf("body = %s", body)
	}
}

func TestAirQualityCursorIsBoundAndForgeriesAreRefusedWithoutRequest(t *testing.T) {
	env := newAirEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", forecastPage1)})
	base := `{` + airLocation + `,"page_size":2`
	first, err := env.invokeTool(airQualityForecastID, "read", base+`}`)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	cursor := nextCursor(t, first)
	history, err := env.invokeTool(airQualityHistoryID, "read", base+`}`)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	foreign := nextCursor(t, strings.Replace(history, "hoursInfo", "x", 1))
	sent := len(env.fake.recorded())
	reads := *env.reads
	for name, arguments := range map[string]string{
		"altered":           base + `,"cursor":"` + cursor[:len(cursor)-2] + `AA"}`,
		"made up":           base + `,"cursor":"abcdefghijklmnopqrstuvwxyz0123456789"}`,
		"raw google token":  base + `,"cursor":"` + pageToken + `"}`,
		"other page size":   `{` + airLocation + `,"page_size":3,"cursor":"` + cursor + `"}`,
		"other location":    `{"location":{"latitude":1,"longitude":2},"page_size":2,"cursor":"` + cursor + `"}`,
		"other computation": base + `,"extra_computations":["LOCAL_AQI"],"cursor":"` + cursor + `"}`,
		"other tool":        base + `,"cursor":"` + foreign + `"}`,
	} {
		if _, err := env.invokeTool(airQualityForecastID, "read", arguments); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The cursor of the forecast is also refused by the history.
	if _, err := env.invokeTool(airQualityHistoryID, "read", base+`,"cursor":"`+cursor+`"}`); err == nil {
		t.Error("a forecast cursor continued the history")
	}
	if len(env.fake.recorded()) != sent || *env.reads != reads {
		t.Errorf("a refused cursor caused %d requests and %d secret reads", len(env.fake.recorded())-sent, *env.reads-reads)
	}
}

func TestAirQualityRefusesBeforeAnyIO(t *testing.T) {
	loc := airLocation
	for name, tc := range map[string]struct{ tool, arguments string }{
		"no location":          {airQualityCurrentID, `{}`},
		"latitude too big":     {airQualityCurrentID, `{"location":{"latitude":91,"longitude":0}}`},
		"longitude too small":  {airQualityCurrentID, `{"location":{"latitude":0,"longitude":-181}}`},
		"no longitude":         {airQualityCurrentID, `{"location":{"latitude":0}}`},
		"string coordinate":    {airQualityCurrentID, `{"location":{"latitude":"1","longitude":2}}`},
		"address":              {airQualityCurrentID, `{"location":"Mountain View"}`},
		"key":                  {airQualityCurrentID, `{` + loc + `,"key":"x"}`},
		"unspecified compute":  {airQualityCurrentID, `{` + loc + `,"extra_computations":["EXTRA_COMPUTATION_UNSPECIFIED"]}`},
		"unknown compute":      {airQualityCurrentID, `{` + loc + `,"extra_computations":["ALL"]}`},
		"repeated compute":     {airQualityCurrentID, `{` + loc + `,"extra_computations":["LOCAL_AQI","LOCAL_AQI"]}`},
		"empty compute":        {airQualityCurrentID, `{` + loc + `,"extra_computations":[]}`},
		"bad palette":          {airQualityCurrentID, `{` + loc + `,"uaqi_color_palette":"BLUE"}`},
		"bad language":         {airQualityCurrentID, `{` + loc + `,"language_code":"en us"}`},
		"bad region":           {airQualityCurrentID, `{` + loc + `,"custom_local_aqis":[{"region_code":"USA","aqi":"usa_epa"}]}`},
		"bad index code":       {airQualityCurrentID, `{` + loc + `,"custom_local_aqis":[{"region_code":"US","aqi":"USA EPA"}]}`},
		"duplicate region":     {airQualityCurrentID, `{` + loc + `,"custom_local_aqis":[{"region_code":"US","aqi":"a"},{"region_code":"us","aqi":"b"}]}`},
		"too many pairs":       {airQualityCurrentID, `{` + loc + `,"custom_local_aqis":[` + strings.TrimSuffix(strings.Repeat(`{"region_code":"US","aqi":"a"},`, 21), ",") + `]}`},
		"current date_time":    {airQualityCurrentID, `{` + loc + `,"date_time":"2026-11-10T09:00:00Z"}`},
		"current page size":    {airQualityCurrentID, `{` + loc + `,"page_size":5}`},
		"forecast hours":       {airQualityForecastID, `{` + loc + `,"hours":5}`},
		"time without Z":       {airQualityForecastID, `{` + loc + `,"date_time":"2026-11-10T09:00:00+01:00"}`},
		"not a time":           {airQualityForecastID, `{` + loc + `,"date_time":"tomorrow at 9 oclock please"}`},
		"date_time and period": {airQualityForecastID, `{` + loc + `,"date_time":"2026-11-10T09:00:00Z","period":{"start_time":"2026-11-10T09:00:00Z"}}`},
		"empty period":         {airQualityForecastID, `{` + loc + `,"period":{}}`},
		"reversed period":      {airQualityForecastID, `{` + loc + `,"period":{"start_time":"2026-11-11T09:00:00Z","end_time":"2026-11-10T09:00:00Z"}}`},
		"period extra":         {airQualityForecastID, `{` + loc + `,"period":{"start_time":"2026-11-10T09:00:00Z","x":1}}`},
		"page size 0":          {airQualityForecastID, `{` + loc + `,"page_size":0}`},
		"page size 169":        {airQualityHistoryID, `{` + loc + `,"page_size":169}`},
		"hours 0":              {airQualityHistoryID, `{` + loc + `,"hours":0}`},
		"hours 721":            {airQualityHistoryID, `{` + loc + `,"hours":721}`},
		"hours and date_time":  {airQualityHistoryID, `{` + loc + `,"hours":5,"date_time":"2026-11-10T09:00:00Z"}`},
		"hours and period":     {airQualityHistoryID, `{` + loc + `,"hours":5,"period":{"end_time":"2026-11-10T09:00:00Z"}}`},
		"empty cursor":         {airQualityHistoryID, `{` + loc + `,"cursor":""}`},
		"page token":           {airQualityHistoryID, `{` + loc + `,"page_token":"x"}`},
	} {
		env := newAirEnvironment(t, &fakeGoogle{})
		if _, err := env.invokeTool(tc.tool, "read", tc.arguments); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
}

func TestAirQualityHandlersCheckArgumentsThemselves(t *testing.T) {
	resolved := &config.Resolved{BaseURL: apiRoot}
	for name, call := range map[string]struct {
		handler capability.Handler
		args    string
	}{
		"latitude":      {handleAirQualityCurrent, `{"location":{"latitude":91,"longitude":0}}`},
		"no location":   {handleAirQualityCurrent, `{}`},
		"period order":  {handleAirQualityForecast, `{` + airLocation + `,"period":{"start_time":"2026-11-11T09:00:00Z","end_time":"2026-11-10T09:00:00Z"}}`},
		"two selectors": {handleAirQualityHistory, `{` + airLocation + `,"hours":5,"date_time":"2026-11-10T09:00:00Z"}`},
		"cursor":        {handleAirQualityForecast, `{` + airLocation + `,"cursor":"abc"}`},
		"current page":  {handleAirQualityCurrent, `{` + airLocation + `,"page_size":5}`},
	} {
		_, err := call.handler(context.Background(), resolved, nil, nil, json.RawMessage(call.args))
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an invalid request before any secret access", name, err)
		}
	}
	for _, bad := range []*config.Resolved{{BaseURL: "https://airquality.googleapis.com"}, {BaseURL: apiRoot, Target: "x"}, nil} {
		if _, err := handleAirQualityCurrent(context.Background(), bad, nil, nil, json.RawMessage(`{`+airLocation+`}`)); err == nil {
			t.Errorf("connection %+v was accepted", bad)
		}
	}
}

func TestAirQualityLocationWithoutCoverageEndsInAnErrorClass(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		class  provider.Class
		want   string
	}{
		{404, errorObject(404, "NOT_FOUND", "X"), provider.ClassNotFound, "not be covered"},
		{400, errorObject(400, "INVALID_ARGUMENT", "X"), provider.ClassInvalidResponse, "not be covered"},
		{401, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), provider.ClassAuth, ""},
		{403, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"), provider.ClassPermission, "Air Quality API"},
		{429, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), provider.ClassRateLimited, ""},
		{500, errorObject(500, "INTERNAL", "X"), provider.ClassProviderError, ""},
	} {
		for _, tool := range []string{airQualityCurrentID, airQualityForecastID, airQualityHistoryID} {
			env := newAirEnvironment(t, &fakeGoogle{answer: reply(tc.status, "application/json", tc.body)})
			_, err := env.invokeTool(tool, "read", `{`+airLocation+`}`)
			if classOf(err) != tc.class {
				t.Errorf("%s %d: class = %q, err = %v", tool, tc.status, classOf(err), err)
				continue
			}
			var failed *provider.Error
			_ = errors.As(err, &failed)
			if strings.Contains(failed.Message, detailCanary) || strings.Contains(failed.Message, keyValue) ||
				strings.Contains(err.Error(), keyValue) || strings.Contains(failed.Message, "Places") ||
				!strings.Contains(failed.Message, tc.want) {
				t.Errorf("%s %d: message = %q", tool, tc.status, failed.Message)
			}
			if len(env.fake.recorded()) != 1 {
				t.Errorf("%s %d: %d requests, want exactly one", tool, tc.status, len(env.fake.recorded()))
			}
		}
	}
}

func TestAirQualityUnusableAnswers(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"not json":   reply(200, "text/plain", "x"),
		"empty":      reply(200, "application/json", `{}`),
		"array":      reply(200, "application/json", `[]`),
		"only junk":  reply(200, "application/json", `{"surprise":1}`),
		"huge token": reply(200, "application/json", `{"hoursInfo":[],"nextPageToken":"`+strings.Repeat("a", 4000)+`"}`),
	} {
		env := newAirEnvironment(t, &fakeGoogle{answer: answer})
		_, err := env.invokeTool(airQualityHistoryID, "read", `{`+airLocation+`}`)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestAirQualityCutsTextsAndLists(t *testing.T) {
	long := strings.Repeat("x", 2000)
	hours := strings.TrimSuffix(strings.Repeat(`{"dateTime":"2026-11-11T10:00:00Z"},`, 200), ",")
	env := newAirEnvironment(t, &fakeGoogle{answer: reply(200, "application/json",
		`{"hourlyForecasts":[`+hours+`],"healthRecommendations":{"generalPopulation":"`+long+`"}}`)})
	result, err := env.invokeTool(airQualityForecastID, "read", `{`+airLocation+`}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var out struct {
		Hours []any `json:"hourlyForecasts"`
	}
	_ = json.Unmarshal([]byte(result), &out)
	if len(out.Hours) != 168 {
		t.Errorf("hours = %d, want the cut at 168", len(out.Hours))
	}
	env = newAirEnvironment(t, &fakeGoogle{answer: reply(200, "application/json",
		`{"healthRecommendations":{"generalPopulation":"`+long+`"}}`)})
	result, err = env.invokeTool(airQualityCurrentID, "read", `{`+airLocation+`}`)
	if err != nil || strings.Contains(result, long) || !strings.Contains(result, "…") {
		t.Errorf("text not cut: %v", err)
	}
}

func TestAirQualityToolsRiskAndDescriptions(t *testing.T) {
	reg := registry(t)
	seen := 0
	for _, d := range reg.Provider(Provider) {
		if !strings.HasPrefix(d.ID, Provider+".airquality.") {
			continue
		}
		seen++
		if d.Risk != readRisk || d.Risk.Effect != capability.EffectRead {
			t.Errorf("%s risk = %+v", d.ID, d.Risk)
		}
		for _, want := range []string{"SKU", "attribution", "untrusted", "charges above the free usage",
			"google.maps.geocoding.geocode", "storage rules", "coordinate is sent"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", d.ID, want)
			}
		}
		var schema map[string]any
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Errorf("%s input schema: %v", d.ID, err)
		}
		props, _ := schema["properties"].(map[string]any)
		for _, arg := range d.Arguments {
			if _, ok := props[arg.Name]; !ok {
				t.Errorf("%s documents argument %s that the schema lacks", d.ID, arg.Name)
			}
		}
		if len(props) != len(d.Arguments) {
			t.Errorf("%s: %d schema properties, %d documented arguments", d.ID, len(props), len(d.Arguments))
		}
		for _, banned := range []string{"key", "page_token", "pageToken"} {
			if _, ok := props[banned]; ok {
				t.Errorf("%s takes %s", d.ID, banned)
			}
		}
	}
	if seen != 3 {
		t.Errorf("air quality tools = %d", seen)
	}
}

func TestAirQualityOriginIsFixedAndTheKeyStaysInTheHeader(t *testing.T) {
	env := newAirEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", airCurrentAnswer)})
	result, err := env.invokeTool(airQualityCurrentID, "read", `{`+airLocation+`}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r := env.fake.recorded()[0]
	if r.header.Get(originalHost) != airHost || r.header.Get("X-Goog-Api-Key") != keyValue ||
		strings.Contains(r.path+r.rawQuery+r.body+result, keyValue) {
		t.Errorf("request = %+v", r)
	}
	if airQualityRoot != "https://airquality.googleapis.com" {
		t.Errorf("root = %q", airQualityRoot)
	}
	metadata, _ := registry(t).ProviderMetadata(Provider)
	if metadata.ValidateBaseURL("https://airquality.googleapis.com") == nil {
		t.Error("a connection may not name the Air Quality host as its base URL")
	}
}
