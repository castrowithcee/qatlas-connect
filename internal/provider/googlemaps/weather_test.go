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
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/ratelimit"
	"github.com/castrowithcee/qatlas-connect/internal/redact"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
)

// weatherRewrite sends the Weather host to the test server and refuses every other host.
type weatherRewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (r weatherRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "weather.googleapis.com" || req.URL.Scheme != "https" {
		return nil, errors.New("unexpected host")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set(originalHost, req.URL.Host)
	clone.URL.Scheme, clone.URL.Host, clone.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(clone)
}

func newWeatherEnvironment(t *testing.T, f *fakeGoogle) *environment {
	t.Helper()
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	previous := transport
	transport = weatherRewrite{target: target, base: server.Client().Transport}
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

const weatherPoint = `{"latitude":37.422,"longitude":-122.0841}`

const currentAnswer = `{"currentTime":"2026-11-10T12:00:00Z","timeZone":{"id":"America/Los_Angeles","version":"2026a"},` +
	`"isDaytime":true,"weatherCondition":{"iconBaseUri":"https://maps.gstatic.com/weather/v1/sunny",` +
	`"description":{"text":"Sunny","languageCode":"en"},"type":"CLEAR"},"temperature":{"degrees":21.5,"unit":"CELSIUS"},` +
	`"relativeHumidity":40,"uvIndex":3,"airPressure":{"meanSeaLevelMillibars":1015.2},` +
	`"wind":{"direction":{"degrees":270,"cardinal":"WEST"},"speed":{"value":10,"unit":"KILOMETERS_PER_HOUR"}},` +
	`"precipitation":{"probability":{"percent":5,"type":"RAIN"},"qpf":{"quantity":0,"unit":"MILLIMETERS"}},` +
	`"currentConditionsHistory":{"maxTemperature":{"degrees":23,"unit":"CELSIUS"}},"surprise":1}`

func TestWeatherCurrentSendsQueryAndShapesResult(t *testing.T) {
	env := newWeatherEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", currentAnswer)})
	result, err := env.invokeTool(weatherCurrentID, "read", `{"location":`+weatherPoint+`,"units_system":"IMPERIAL","language_code":"pt-BR"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	requests := env.fake.recorded()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	r := requests[0]
	query, _ := url.ParseQuery(r.rawQuery)
	if r.method != http.MethodGet || r.path != "/v1/currentConditions:lookup" ||
		r.header.Get(originalHost) != "weather.googleapis.com" || r.header.Get("X-Goog-Api-Key") != keyValue ||
		r.header.Get("X-Goog-FieldMask") != "" || r.body != "" ||
		query.Get("location.latitude") != "37.422" || query.Get("location.longitude") != "-122.0841" ||
		query.Get("unitsSystem") != "IMPERIAL" || query.Get("languageCode") != "pt-BR" || len(query) != 4 {
		t.Errorf("request = %+v", r)
	}
	for _, text := range []string{r.path, r.rawQuery, result} {
		if strings.Contains(text, keyValue) {
			t.Error("the key reached the URL or the result")
		}
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(result), &got)
	condition, _ := got["weatherCondition"].(map[string]any)
	if got["isDaytime"] != true || got["surprise"] != nil || got["relativeHumidity"] != float64(40) ||
		got["temperature"].(map[string]any)["degrees"] != 21.5 || condition["type"] != "CLEAR" ||
		condition["iconBaseUri"] != "https://maps.gstatic.com/weather/v1/sunny" ||
		got["timeZone"].(map[string]any)["id"] != "America/Los_Angeles" ||
		got["airPressure"].(map[string]any)["meanSeaLevelMillibars"] != 1015.2 ||
		got["wind"].(map[string]any)["direction"].(map[string]any)["cardinal"] != "WEST" ||
		got["currentConditionsHistory"].(map[string]any)["maxTemperature"] == nil {
		t.Errorf("result = %s", result)
	}
}

func TestWeatherUnsafeIconAddressIsDropped(t *testing.T) {
	env := newWeatherEnvironment(t, &fakeGoogle{answer: reply(200, "application/json",
		`{"weatherCondition":{"iconBaseUri":"javascript:alert(1)","type":"CLEAR"}}`)})
	result, err := env.invokeTool(weatherCurrentID, "read", `{"location":`+weatherPoint+`}`)
	if err != nil || strings.Contains(result, "javascript") || !strings.Contains(result, "CLEAR") {
		t.Errorf("result = %s, err = %v", result, err)
	}
}

const hoursPage1 = `{"forecastHours":[{"interval":{"startTime":"2026-11-10T12:00:00Z","endTime":"2026-11-10T13:00:00Z"},` +
	`"displayDateTime":{"year":2026,"month":11,"day":10,"hours":4,"utcOffset":"-28800s"},"isDaytime":true,` +
	`"temperature":{"degrees":12,"unit":"CELSIUS"},"visibility":{"distance":16,"unit":"KILOMETERS"},` +
	`"airPressure":{"meanSeaLevelMillibars":1010},"wetBulbTemperature":{"degrees":9,"unit":"CELSIUS"},"extra":1}],` +
	`"timeZone":{"id":"America/Los_Angeles"},"nextPageToken":"hours-token-2"}`
const hoursPage2 = `{"forecastHours":[{"isDaytime":false}],"timeZone":{"id":"America/Los_Angeles"}}`

func TestWeatherHoursPagesThroughTheCursor(t *testing.T) {
	calls := 0
	env := newWeatherEnvironment(t, &fakeGoogle{answer: func(w http.ResponseWriter, r *http.Request) {
		calls++
		body := hoursPage1
		if r.URL.Query().Get("pageToken") != "" {
			body = hoursPage2
		}
		reply(200, "application/json", body)(w, r)
	}})
	arguments := `{"location":` + weatherPoint + `,"hours":48,"page_size":1,"units_system":"METRIC"}`
	first, err := env.invokeTool(weatherHoursID, "read", arguments)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	var page map[string]any
	_ = json.Unmarshal([]byte(first), &page)
	cursor, _ := page["next_cursor"].(string)
	hours := page["forecastHours"].([]any)
	if page["has_more"] != true || cursor == "" || strings.Contains(cursor, "hours-token-2") && strings.Contains(first, `"nextPageToken"`) ||
		len(hours) != 1 || hours[0].(map[string]any)["extra"] != nil ||
		hours[0].(map[string]any)["wetBulbTemperature"] == nil || page["nextPageToken"] != nil {
		t.Fatalf("first page = %s", first)
	}
	q, _ := url.ParseQuery(env.fake.recorded()[0].rawQuery)
	if q.Get("hours") != "48" || q.Get("pageSize") != "1" || q.Get("unitsSystem") != "METRIC" || q.Get("pageToken") != "" {
		t.Errorf("first query = %v", q)
	}
	second, err := env.invokeTool(weatherHoursID, "read", `{"location":`+weatherPoint+`,"hours":48,"page_size":1,`+
		`"units_system":"METRIC","cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !strings.Contains(second, `"has_more":false`) || strings.Contains(second, "next_cursor") {
		t.Errorf("second page = %s", second)
	}
	if q, _ := url.ParseQuery(env.fake.recorded()[1].rawQuery); q.Get("pageToken") != "hours-token-2" ||
		q.Get("hours") != "48" || q.Get("pageSize") != "1" {
		t.Errorf("second query = %v", q)
	}
	// A cursor of another request, a changed request, and an altered cursor are refused without any request.
	before := len(env.fake.recorded())
	for name, arguments := range map[string]string{
		"other location": `{"location":{"latitude":1,"longitude":2},"hours":48,"page_size":1,"units_system":"METRIC","cursor":"` + cursor + `"}`,
		"other hours":    `{"location":` + weatherPoint + `,"hours":24,"page_size":1,"units_system":"METRIC","cursor":"` + cursor + `"}`,
		"other units":    `{"location":` + weatherPoint + `,"hours":48,"page_size":1,"units_system":"IMPERIAL","cursor":"` + cursor + `"}`,
		"other size":     `{"location":` + weatherPoint + `,"hours":48,"page_size":2,"units_system":"METRIC","cursor":"` + cursor + `"}`,
		"altered":        `{"location":` + weatherPoint + `,"hours":48,"page_size":1,"units_system":"METRIC","cursor":"` + cursor[:len(cursor)-2] + `AA"}`,
		"invented":       `{"location":` + weatherPoint + `,"hours":48,"page_size":1,"units_system":"METRIC","cursor":"hours-token-2"}`,
	} {
		if _, err := env.invokeTool(weatherHoursID, "read", arguments); err == nil {
			t.Errorf("%s: cursor accepted", name)
		}
	}
	if len(env.fake.recorded()) != before {
		t.Error("a refused cursor reached Google")
	}
	// The cursor of the hourly forecast is not valid for the daily forecast.
	if _, err := env.invokeTool(weatherDaysID, "read", `{"location":`+weatherPoint+`,"cursor":"`+cursor+`"}`); err == nil {
		t.Error("the hourly cursor was accepted by the daily tool")
	}
}

const daysAnswer = `{"forecastDays":[{"interval":{"startTime":"2026-11-10T08:00:00Z","endTime":"2026-11-11T08:00:00Z"},` +
	`"displayDate":{"year":2026,"month":11,"day":10},"maxTemperature":{"degrees":18,"unit":"FAHRENHEIT"},` +
	`"minTemperature":{"degrees":9,"unit":"FAHRENHEIT"},"maxHeatIndex":{"degrees":19,"unit":"FAHRENHEIT"},` +
	`"sunEvents":{"sunriseTime":"2026-11-10T14:30:00Z","sunsetTime":"2026-11-11T00:50:00Z"},` +
	`"moonEvents":{"moonPhase":"FULL_MOON","moonriseTimes":["a","b"],"moonsetTimes":["c"]},` +
	`"daytimeForecast":{"relativeHumidity":50,"uvIndex":4,"weatherCondition":{"type":"RAIN"},` +
	`"precipitation":{"snowQpf":{"quantity":1,"unit":"INCHES"}},"iceThickness":{"thickness":2,"unit":"INCHES"}},` +
	`"nighttimeForecast":{"cloudCover":80},"fullDayForecast":{"thunderstormProbability":10}}],` +
	`"timeZone":{"id":"America/New_York"},"nextPageToken":"days-token-2"}`

func TestWeatherDaysSendsDaysAndShapesResult(t *testing.T) {
	env := newWeatherEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", daysAnswer)})
	result, err := env.invokeTool(weatherDaysID, "read", `{"location":`+weatherPoint+`,"days":3,"page_size":2,"units_system":"IMPERIAL"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r := env.fake.recorded()[0]
	q, _ := url.ParseQuery(r.rawQuery)
	if r.path != "/v1/forecast/days:lookup" || q.Get("days") != "3" || q.Get("pageSize") != "2" ||
		q.Get("unitsSystem") != "IMPERIAL" || q.Get("hours") != "" {
		t.Errorf("request = %+v", r)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(result), &got)
	day := got["forecastDays"].([]any)[0].(map[string]any)
	moon := day["moonEvents"].(map[string]any)
	if got["has_more"] != true || got["next_cursor"] == "" || day["maxTemperature"].(map[string]any)["unit"] != "FAHRENHEIT" ||
		moon["moonPhase"] != "FULL_MOON" || len(moon["moonriseTimes"].([]any)) != 2 ||
		day["daytimeForecast"].(map[string]any)["iceThickness"] == nil ||
		day["nighttimeForecast"].(map[string]any)["cloudCover"] != float64(80) ||
		day["sunEvents"].(map[string]any)["sunriseTime"] == nil || day["displayDate"].(map[string]any)["day"] != float64(10) {
		t.Errorf("result = %s", result)
	}
}

func TestWeatherMinutes(t *testing.T) {
	env := newWeatherEnvironment(t, &fakeGoogle{answer: reply(200, "application/json",
		`{"timeZone":{"id":"Europe/Paris"},"overallPredictionTimeframe":{"startTime":"a","endTime":"b"},`+
			`"segments":[{"timeFrame":{"startTime":"a","endTime":"b"},"probability":60,"type":"RAIN","intensity":"LIGHT",`+
			`"qpf":{"quantity":0.2,"unit":"MILLIMETERS"},"snowfallAmount":{"quantity":0,"unit":"MILLIMETERS"}}]}`)})
	result, err := env.invokeTool(weatherMinutesID, "read", `{"location":`+weatherPoint+`,"page_size":60,"units_system":"METRIC"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r := env.fake.recorded()[0]
	q, _ := url.ParseQuery(r.rawQuery)
	if r.path != "/v1/forecast/minutes:lookup" || q.Get("pageSize") != "60" || q.Has("languageCode") || q.Has("minutesCount") {
		t.Errorf("request = %+v", r)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(result), &got)
	segment := got["segments"].([]any)[0].(map[string]any)
	if got["has_more"] != false || segment["intensity"] != "LIGHT" || segment["probability"] != float64(60) ||
		segment["qpf"].(map[string]any)["quantity"] != 0.2 {
		t.Errorf("result = %s", result)
	}
	if _, err := env.invokeTool(weatherMinutesID, "read", `{"location":`+weatherPoint+`,"language_code":"en"}`); err == nil {
		t.Error("minutes accepted language_code")
	}
}

func TestWeatherListsAreCut(t *testing.T) {
	var items []string
	for i := 0; i < 40; i++ {
		items = append(items, `{"isDaytime":true}`)
	}
	env := newWeatherEnvironment(t, &fakeGoogle{answer: reply(200, "application/json",
		`{"forecastHours":[`+strings.Join(items, ",")+`]}`)})
	result, err := env.invokeTool(weatherHoursID, "read", `{"location":`+weatherPoint+`}`)
	var got map[string]any
	_ = json.Unmarshal([]byte(result), &got)
	if err != nil || len(got["forecastHours"].([]any)) != maxHoursPageSize {
		t.Errorf("result = %s, err = %v", result, err)
	}
}

func TestWeatherRefusesBeforeAnyIO(t *testing.T) {
	for name, tc := range map[string]struct{ tool, arguments string }{
		"no location":        {weatherCurrentID, `{}`},
		"no longitude":       {weatherCurrentID, `{"location":{"latitude":1}}`},
		"latitude over":      {weatherCurrentID, `{"location":{"latitude":90.1,"longitude":0}}`},
		"latitude under":     {weatherCurrentID, `{"location":{"latitude":-91,"longitude":0}}`},
		"longitude over":     {weatherCurrentID, `{"location":{"latitude":0,"longitude":180.5}}`},
		"longitude under":    {weatherCurrentID, `{"location":{"latitude":0,"longitude":-181}}`},
		"text coordinate":    {weatherCurrentID, `{"location":{"latitude":"1","longitude":"2"}}`},
		"address":            {weatherCurrentID, `{"location":` + weatherPoint + `,"address":"Paris"}`},
		"bad units":          {weatherCurrentID, `{"location":` + weatherPoint + `,"units_system":"KELVIN"}`},
		"bad language":       {weatherCurrentID, `{"location":` + weatherPoint + `,"language_code":"en us"}`},
		"key":                {weatherCurrentID, `{"location":` + weatherPoint + `,"key":"x"}`},
		"current page size":  {weatherCurrentID, `{"location":` + weatherPoint + `,"page_size":1}`},
		"hours zero":         {weatherHoursID, `{"location":` + weatherPoint + `,"hours":0}`},
		"hours over":         {weatherHoursID, `{"location":` + weatherPoint + `,"hours":241}`},
		"hours page over":    {weatherHoursID, `{"location":` + weatherPoint + `,"page_size":25}`},
		"hours days":         {weatherHoursID, `{"location":` + weatherPoint + `,"days":2}`},
		"days over":          {weatherDaysID, `{"location":` + weatherPoint + `,"days":11}`},
		"days page over":     {weatherDaysID, `{"location":` + weatherPoint + `,"page_size":11}`},
		"days hours":         {weatherDaysID, `{"location":` + weatherPoint + `,"hours":2}`},
		"minutes page over":  {weatherMinutesID, `{"location":` + weatherPoint + `,"page_size":361}`},
		"minutes count":      {weatherMinutesID, `{"location":` + weatherPoint + `,"minutes_count":5}`},
		"minutes hours":      {weatherMinutesID, `{"location":` + weatherPoint + `,"hours":1}`},
		"current cursor":     {weatherCurrentID, `{"location":` + weatherPoint + `,"cursor":"x"}`},
		"empty cursor token": {weatherHoursID, `{"location":` + weatherPoint + `,"cursor":""}`},
	} {
		env := newWeatherEnvironment(t, &fakeGoogle{})
		if _, err := env.invokeTool(tc.tool, "read", tc.arguments); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
	// A connection with a target or another base URL is refused before the key is read.
	for _, resolved := range []*config.Resolved{{BaseURL: "https://weather.googleapis.com"}, {BaseURL: apiRoot, Target: "x"}} {
		if _, err := handleWeatherCurrent(context.Background(), resolved, nil, nil, json.RawMessage(`{"location":`+weatherPoint+`}`)); err == nil {
			t.Errorf("resolved %+v was accepted", resolved)
		}
	}
	// The handler checks the coordinate itself, before any secret access.
	var invalid *provider.InvalidRequestError
	_, err := handleWeatherHours(context.Background(), &config.Resolved{BaseURL: apiRoot}, nil, nil,
		json.RawMessage(`{"location":{"latitude":95,"longitude":0}}`))
	if !errors.As(err, &invalid) {
		t.Errorf("err = %v, want an invalid request", err)
	}
}

func TestWeatherErrorFormsKeepProviderTextApart(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		class  provider.Class
		text   string
	}{
		{401, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), provider.ClassAuth, ""},
		{403, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"), provider.ClassPermission, "Weather API"},
		{429, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), provider.ClassRateLimited, ""},
		{404, errorObject(404, "NOT_FOUND", "X"), provider.ClassNotFound, "no weather data"},
		{400, errorObject(400, "INVALID_ARGUMENT", "X"), provider.ClassInvalidResponse, "coverage"},
		{500, errorObject(500, "INTERNAL", "X"), provider.ClassProviderError, ""},
	} {
		for _, tool := range []string{weatherCurrentID, weatherMinutesID, weatherHoursID, weatherDaysID} {
			env := newWeatherEnvironment(t, &fakeGoogle{answer: reply(tc.status, "application/json", tc.body)})
			_, err := env.invokeTool(tool, "read", `{"location":`+weatherPoint+`}`)
			if classOf(err) != tc.class {
				t.Errorf("%s %d: class = %q, err = %v", tool, tc.status, classOf(err), err)
				continue
			}
			var failed *provider.Error
			_ = errors.As(err, &failed)
			if strings.Contains(failed.Message, detailCanary) || strings.Contains(err.Error(), keyValue) ||
				strings.Contains(failed.Message, "Places") || !strings.Contains(failed.Message, tc.text) {
				t.Errorf("%s %d: message = %q", tool, tc.status, failed.Message)
			}
			if len(env.fake.recorded()) != 1 {
				t.Errorf("%s %d: %d requests, want one", tool, tc.status, len(env.fake.recorded()))
			}
		}
	}
}

func TestWeatherUnusableAnswers(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"not json":     reply(200, "text/plain", "x"),
		"empty":        reply(200, "application/json", `{}`),
		"only unknown": reply(200, "application/json", `{"surprise":1}`),
		"array":        reply(200, "application/json", `[]`),
		"null":         reply(200, "application/json", `null`),
		"redirect":     reply(302, "", ""),
	} {
		env := newWeatherEnvironment(t, &fakeGoogle{answer: answer})
		_, err := env.invokeTool(weatherCurrentID, "read", `{"location":`+weatherPoint+`}`)
		if classOf(err) != provider.ClassInvalidResponse && classOf(err) != provider.ClassProviderError {
			t.Errorf("%s: err = %v", name, err)
		}
		if n := len(env.fake.recorded()); n != 1 {
			t.Errorf("%s: %d requests", name, n)
		}
	}
	env := newWeatherEnvironment(t, &fakeGoogle{})
	transport = failingTransport{err: context.DeadlineExceeded}
	if _, err := env.invokeTool(weatherCurrentID, "read", `{"location":`+weatherPoint+`}`); classOf(err) != provider.ClassTimeout &&
		classOf(err) != provider.ClassProviderError {
		t.Errorf("timeout err = %v", err)
	}
}

func TestWeatherToolsRiskAndDescriptions(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	inProfile := map[string]bool{}
	for _, id := range metadata.Profiles[0].Tools {
		inProfile[id] = true
	}
	found := 0
	for _, d := range reg.Provider(Provider) {
		if !strings.HasPrefix(d.ID, Provider+".weather.") {
			continue
		}
		found++
		if d.Risk != readRisk || !inProfile[d.ID] {
			t.Errorf("%s risk = %+v, in profile %v", d.ID, d.Risk, inProfile[d.ID])
		}
		for _, want := range []string{"attribution", "storage rules", "charges above the free usage", "untrusted",
			"google.maps.geocoding.geocode", "Weather API"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", d.ID, want)
			}
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(d.InputSchema, &schema) != nil || schema.Properties["location"] == nil {
			t.Errorf("%s input schema = %s", d.ID, d.InputSchema)
		}
		for _, banned := range []string{"key", "target", "url"} {
			if schema.Properties[banned] != nil {
				t.Errorf("%s takes %s", d.ID, banned)
			}
		}
		for name := range schema.Properties {
			documented := false
			for _, argument := range d.Arguments {
				documented = documented || argument.Name == name
			}
			if !documented {
				t.Errorf("%s argument %s is undocumented", d.ID, name)
			}
		}
	}
	if found != 4 {
		t.Errorf("weather tools = %d", found)
	}
	if weatherRoot != "https://weather.googleapis.com" || metadata.ValidateBaseURL(weatherRoot) == nil {
		t.Error("the Weather origin is not fixed or may be a base URL")
	}
}
