package googlemaps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// locationRewrite sends the hosts of Elevation, Time Zone (maps.googleapis.com) and Geolocation
// (www.googleapis.com) to the test server and refuses every other host.
type locationRewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (r locationRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if (req.URL.Host != "maps.googleapis.com" && req.URL.Host != "www.googleapis.com") || req.URL.Scheme != "https" {
		return nil, errors.New("unexpected host")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set(originalHost, req.URL.Host)
	clone.URL.Scheme, clone.URL.Host, clone.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(clone)
}

func newLocationEnvironment(t *testing.T, answer func(http.ResponseWriter, *http.Request)) *environment {
	t.Helper()
	f := &fakeGoogle{answer: answer}
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	previous := transport
	transport = locationRewrite{target: target, base: server.Client().Transport}
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

func jsonOK(body string) func(http.ResponseWriter, *http.Request) {
	return reply(200, "application/json; charset=UTF-8", body)
}

// legacyRequest checks a GET to the legacy host with the key in the query once and nowhere else.
func legacyRequest(t *testing.T, env *environment, path string) (recorded, url.Values) {
	t.Helper()
	r, query := lastQuery(t, env)
	if r.method != http.MethodGet || r.path != path || r.header.Get(originalHost) != "maps.googleapis.com" ||
		r.header.Get("X-Goog-Api-Key") != "" || r.header.Get("Authorization") != "" {
		t.Errorf("request = %+v", r)
	}
	if query.Get("key") != keyValue || len(query["key"]) != 1 || strings.Count(r.rawQuery, keyValue) != 1 {
		t.Errorf("the key is not in the query exactly once: %s", r.rawQuery)
	}
	return r, query
}

const elevationBody = `{"results":[{"elevation":1608.637939453125,"location":{"lat":39.7391536,"lng":-104.9847034},` +
	`"resolution":4.771975994110107,"surprise":1},{"elevation":-50.5,"location":{"lat":1,"lng":2}}],` +
	`"status":"OK","error_message":"` + detailCanary + `","unknownFuture":1}`

func TestElevationLocationsAndPath(t *testing.T) {
	env := newLocationEnvironment(t, jsonOK(elevationBody))
	result, err := env.invokeTool(elevationGetID, "read",
		`{"locations":[{"latitude":39.7391536,"longitude":-104.9847034},{"latitude":-90,"longitude":180}]}`)
	if err != nil {
		t.Fatal(err)
	}
	_, query := legacyRequest(t, env, "/maps/api/elevation/json")
	if query.Get("locations") != "39.7391536,-104.9847034|-90,180" || query.Has("path") || query.Has("samples") {
		t.Errorf("query = %v", query)
	}
	got := decodeResult(t, result)
	results, _ := got["results"].([]any)
	first, _ := results[0].(map[string]any)
	if got["status"] != "OK" || len(results) != 2 || first["elevation"] != 1608.637939453125 ||
		first["resolution"] != 4.771975994110107 || first["location"].(map[string]any)["lng"] != -104.9847034 ||
		first["surprise"] != nil || got["unknownFuture"] != nil || got["error_message"] != nil ||
		strings.Contains(result, detailCanary) || results[1].(map[string]any)["resolution"] != nil {
		t.Errorf("result = %s", result)
	}

	env = newLocationEnvironment(t, jsonOK(elevationBody))
	if _, err := env.invokeTool(elevationGetID, "read",
		`{"path":[{"latitude":36.578581,"longitude":-118.291994},{"latitude":36.23998,"longitude":-116.83171}],"samples":3}`); err != nil {
		t.Fatal(err)
	}
	_, query = legacyRequest(t, env, "/maps/api/elevation/json")
	if query.Get("path") != "36.578581,-118.291994|36.23998,-116.83171" || query.Get("samples") != "3" || query.Has("locations") {
		t.Errorf("query = %v", query)
	}
}

func TestDecodePolylineAndElevationPolylines(t *testing.T) {
	const google = "_p~iF~ps|U_ulLnnqC_mqNvxq`@"
	points, ok := decodePolyline(google, 512)
	if !ok || len(points) != 3 || math.Abs(*points[0].Latitude-38.5) > 1e-9 || math.Abs(*points[0].Longitude+120.2) > 1e-9 ||
		math.Abs(*points[2].Latitude-43.252) > 1e-9 || math.Abs(*points[2].Longitude+126.453) > 1e-9 {
		t.Fatalf("points = %v, %v", points, ok)
	}
	for _, bad := range []string{"", "_p~iF~ps|U_", "_p~iF~ps|U\x7f", "é", "~~~~~~~~~~~~~~~~~~~~"} {
		if _, ok := decodePolyline(bad, 512); ok {
			t.Errorf("%q decoded", bad)
		}
	}
	if _, ok := decodePolyline(google, 2); ok {
		t.Error("a polyline over the point limit decoded")
	}

	env := newLocationEnvironment(t, jsonOK(elevationBody))
	if _, err := env.invokeTool(elevationGetID, "read", `{"locations_polyline":"`+google+`"}`); err != nil {
		t.Fatal(err)
	}
	if _, query := legacyRequest(t, env, "/maps/api/elevation/json"); query.Get("locations") != "enc:"+google {
		t.Errorf("query = %v", query)
	}
	env = newLocationEnvironment(t, jsonOK(elevationBody))
	if _, err := env.invokeTool(elevationGetID, "read", `{"path_polyline":"`+google+`","samples":10}`); err != nil {
		t.Fatal(err)
	}
	if _, query := legacyRequest(t, env, "/maps/api/elevation/json"); query.Get("path") != "enc:"+google ||
		query.Get("samples") != "10" {
		t.Errorf("query = %v", query)
	}
}

func TestElevationArgumentRefusals(t *testing.T) {
	one := `[{"latitude":1,"longitude":2}]`
	two := `[{"latitude":1,"longitude":2},{"latitude":3,"longitude":4}]`
	many := func(n int) string {
		points := make([]string, n)
		for i := range points {
			points[i] = fmt.Sprintf(`{"latitude":%d.123456789012345,"longitude":%d.123456789012345}`, i%90, i%180)
		}
		return "[" + strings.Join(points, ",") + "]"
	}
	for name, arguments := range map[string]string{
		"nothing":                `{}`,
		"locations and path":     `{"locations":` + one + `,"path":` + two + `,"samples":2}`,
		"locations and polyline": `{"locations":` + one + `,"locations_polyline":"_p~iF~ps|U"}`,
		"path and path polyline": `{"path":` + two + `,"path_polyline":"_p~iF~ps|U_ulLnnqC","samples":2}`,
		"locations with samples": `{"locations":` + one + `,"samples":2}`,
		"polyline with samples":  `{"locations_polyline":"_p~iF~ps|U","samples":2}`,
		"path without samples":   `{"path":` + two + `}`,
		"samples 1":              `{"path":` + two + `,"samples":1}`,
		"samples 513":            `{"path":` + two + `,"samples":513}`,
		"path of one":            `{"path":` + one + `,"samples":2}`,
		"path polyline of one":   `{"path_polyline":"_p~iF~ps|U","samples":2}`,
		"empty locations":        `{"locations":[]}`,
		"513 locations":          `{"locations":` + many(513) + `}`,
		"latitude 91":            `{"locations":[{"latitude":91,"longitude":0}]}`,
		"longitude -181":         `{"locations":[{"latitude":0,"longitude":-181}]}`,
		"no longitude":           `{"locations":[{"latitude":0}]}`,
		"string location":        `{"locations":["1,2"]}`,
		"string pipe":            `{"locations":"1,2|3,4"}`,
		"bad polyline":           `{"locations_polyline":"not a polyline"}`,
		"polyline injection":     `{"locations_polyline":"a&key=evil"}`,
		"long polyline":          `{"locations_polyline":"` + strings.Repeat("?", 8193) + `"}`,
		"extra":                  `{"locations":` + one + `,"key":"x"}`,
		"output":                 `{"locations":` + one + `,"output":"xml"}`,
		// 512 points with full-precision coordinates exceed the URL length Google allows.
		"url too long": `{"locations":` + many(512) + `}`,
	} {
		env := newLocationEnvironment(t, nil)
		if _, err := env.invokeTool(elevationGetID, "read", arguments); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
	// The handler checks what the schema checks first, before any secret access.
	resolved := &config.Resolved{BaseURL: apiRoot}
	for name, arguments := range map[string]string{
		"both": `{"locations":` + one + `,"path":` + two + `,"samples":2}`, "none": `{}`,
		"samples": `{"path":` + two + `,"samples":0}`, "range": `{"locations":[{"latitude":95,"longitude":0}]}`,
		"count": `{"locations":` + many(513) + `}`,
	} {
		_, err := handleElevationGet(context.Background(), resolved, nil, nil, json.RawMessage(arguments))
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	env := newLocationEnvironment(t, jsonOK(elevationBody))
	if _, err := env.invokeTool(elevationGetID, "read", `{"locations":[{"latitude":90,"longitude":-180}]}`); err != nil {
		t.Errorf("border values: %v", err)
	}
}

func TestElevationEmptyAnswers(t *testing.T) {
	for _, status := range []string{"ZERO_RESULTS", "DATA_NOT_AVAILABLE"} {
		env := newLocationEnvironment(t, jsonOK(`{"status":"`+status+`","results":[],"error_message":"`+detailCanary+`"}`))
		result, err := env.invokeTool(elevationGetID, "read", `{"locations":[{"latitude":1,"longitude":2}]}`)
		got := decodeResult(t, result)
		// The core leaves an empty list out; the handler's own answer holds it.
		if list, _ := got["results"].([]any); err != nil || len(list) != 0 || got["status"] != status ||
			strings.Contains(result, detailCanary) {
			t.Errorf("%s: result = %s, err = %v", status, result, err)
		}
	}
	newLocationEnvironment(t, jsonOK(`{"status":"OK"}`))
	resolver := secret.NewWith(func(string) string { return keyValue }, nil, nil, &redact.Redactor{})
	resolved := &config.Resolved{BaseURL: apiRoot, Credential: "maps-key", Secrets: coreConfig().Credentials["maps-key"]}
	got, err := handleElevationGet(context.Background(), resolved, resolver, &redact.Redactor{},
		json.RawMessage(`{"locations":[{"latitude":1,"longitude":2}]}`))
	if list, ok := got.(map[string]any)["results"].([]any); err != nil || !ok || len(list) != 0 {
		t.Errorf("result = %v, err = %v", got, err)
	}
}

func TestLegacyStatusErrorsOfElevationAndTimezone(t *testing.T) {
	calls := map[string][2]string{
		elevationGetID: {`{"locations":[{"latitude":1,"longitude":2}]}`, "Elevation"},
		timezoneGetID:  {`{"location":{"latitude":1,"longitude":2},"timestamp":1331161200}`, "Time Zone"},
	}
	for _, tc := range []struct {
		status string
		class  provider.Class
	}{
		{"OVER_QUERY_LIMIT", provider.ClassRateLimited}, {"REQUEST_DENIED", provider.ClassAuth},
		{"OVER_DAILY_LIMIT", provider.ClassPermission}, {"INVALID_REQUEST", provider.ClassInvalidResponse},
		{"UNKNOWN_ERROR", provider.ClassProviderError}, {"SOMETHING_NEW", provider.ClassInvalidResponse},
	} {
		for id, call := range calls {
			env := newLocationEnvironment(t, jsonOK(`{"status":"`+tc.status+`","results":[],"error_message":"`+
				detailCanary+` `+keyValue+`","errorMessage":"`+detailCanary+`"}`))
			_, err := env.invokeTool(id, "read", call[0])
			var failed *provider.Error
			if !errors.As(err, &failed) || failed.Class != tc.class {
				t.Errorf("%s %s: err = %v", id, tc.status, err)
				continue
			}
			if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), keyValue) ||
				strings.Contains(failed.Message, "Geocoding") {
				t.Errorf("%s %s: message = %q", id, tc.status, failed.Message)
			}
			if tc.status != "SOMETHING_NEW" && (failed.Original == nil || len(failed.Original.Entries) == 0 ||
				failed.Original.Entries[0].Message != detailCanary+" "+keyValue) {
				t.Errorf("%s %s: original = %+v", id, tc.status, failed.Original)
			}
			if got := env.red.Apply(fmt.Sprintf("%+v", failed.Original)); strings.Contains(got, keyValue) {
				t.Errorf("%s %s: the redacted original keeps the key", id, tc.status)
			}
			if len(env.fake.recorded()) != 1 {
				t.Errorf("%s %s: %d requests", id, tc.status, len(env.fake.recorded()))
			}
		}
	}
	// HTTP failures name the service.
	env := newLocationEnvironment(t, reply(403, "application/json", `{}`))
	_, err := env.invokeTool(timezoneGetID, "read", calls[timezoneGetID][0])
	if classOf(err) != provider.ClassPermission || !strings.Contains(err.Error(), "Time Zone API") {
		t.Errorf("err = %v", err)
	}
}

func TestTimezoneWithTimestampAndLanguage(t *testing.T) {
	env := newLocationEnvironment(t, jsonOK(`{"dstOffset":0,"rawOffset":-28800,"status":"OK","timeZoneId":"America/Los_Angeles",`+
		`"timeZoneName":"hora estándar del Pacífico","errorMessage":"`+detailCanary+`","other":1}`))
	result, err := env.invokeTool(timezoneGetID, "read",
		`{"location":{"latitude":39.603481,"longitude":-119.682251},"timestamp":1331161200,"language":"es"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, query := legacyRequest(t, env, "/maps/api/timezone/json")
	if query.Get("location") != "39.603481,-119.682251" || query.Get("timestamp") != "1331161200" ||
		query.Get("language") != "es" {
		t.Errorf("query = %v", query)
	}
	got := decodeResult(t, result)
	if got["status"] != "OK" || got["timeZoneId"] != "America/Los_Angeles" || got["rawOffset"] != float64(-28800) ||
		got["dstOffset"] != float64(0) || got["timeZoneName"] != "hora estándar del Pacífico" || got["other"] != nil ||
		got["errorMessage"] != nil {
		t.Errorf("result = %s", result)
	}

	env = newLocationEnvironment(t, jsonOK(`{"status":"ZERO_RESULTS","errorMessage":"`+detailCanary+`"}`))
	result, err = env.invokeTool(timezoneGetID, "read", `{"location":{"latitude":0,"longitude":0},"timestamp":0}`)
	if _, query := legacyRequest(t, env, "/maps/api/timezone/json"); err != nil || query.Get("timestamp") != "0" ||
		query.Has("language") || result != `{"status":"ZERO_RESULTS"}` {
		t.Errorf("result = %s, err = %v, query = %v", result, err, query)
	}
}

func TestTimezoneRefusals(t *testing.T) {
	point := `"location":{"latitude":1,"longitude":2}`
	for name, arguments := range map[string]string{
		"no timestamp":    `{` + point + `}`,
		"null timestamp":  `{` + point + `,"timestamp":null}`,
		"string time":     `{` + point + `,"timestamp":"1331161200"}`,
		"fractional time": `{` + point + `,"timestamp":1.5}`,
		"huge time":       `{` + point + `,"timestamp":253402300800}`,
		"no location":     `{"timestamp":1}`,
		"bad latitude":    `{"location":{"latitude":91,"longitude":2},"timestamp":1}`,
		"bad language":    `{` + point + `,"timestamp":1,"language":"en us"}`,
		"extra":           `{` + point + `,"timestamp":1,"key":"x"}`,
	} {
		env := newLocationEnvironment(t, nil)
		if _, err := env.invokeTool(timezoneGetID, "read", arguments); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
	resolved := &config.Resolved{BaseURL: apiRoot}
	for _, arguments := range []string{`{` + point + `}`, `{"timestamp":1}`, `{` + point + `,"timestamp":1,"language":"x y"}`} {
		_, err := handleTimezoneGet(context.Background(), resolved, nil, nil, json.RawMessage(arguments))
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v", arguments, err)
		}
	}
}

// geolocationRequest checks a POST to the shared Google APIs host with the key in the query and nowhere else,
// and returns the decoded body.
func geolocationRequest(t *testing.T, env *environment) map[string]any {
	t.Helper()
	r, query := lastQuery(t, env)
	if r.method != http.MethodPost || r.path != "/geolocation/v1/geolocate" || r.header.Get(originalHost) != "www.googleapis.com" ||
		r.header.Get("X-Goog-Api-Key") != "" || r.header.Get("Authorization") != "" ||
		r.header.Get("Content-Type") != "application/json" || len(query) != 1 {
		t.Errorf("request = %+v", r)
	}
	if query.Get("key") != keyValue || strings.Contains(r.body, keyValue) {
		t.Errorf("the key is not only in the query: %s", r.rawQuery)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(r.body), &body); err != nil {
		t.Fatalf("body = %q", r.body)
	}
	return body
}

const geolocationOK = `{"location":{"lat":51.0,"lng":-0.1},"accuracy":1200.4,"extra":1}`

func TestGeolocationConsiderIPAlone(t *testing.T) {
	env := newLocationEnvironment(t, jsonOK(geolocationOK))
	result, err := env.invokeTool(geolocationLocateID, "read", `{"consider_ip":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if body := geolocationRequest(t, env); len(body) != 1 || body["considerIp"] != false {
		t.Errorf("body = %v", body)
	}
	got := decodeResult(t, result)
	if got["accuracy"] != 1200.4 || got["location"].(map[string]any)["lat"] != 51.0 || got["extra"] != nil {
		t.Errorf("result = %s", result)
	}
	env = newLocationEnvironment(t, jsonOK(geolocationOK))
	if _, err := env.invokeTool(geolocationLocateID, "read", `{}`); err != nil {
		t.Fatal(err)
	}
	if body := geolocationRequest(t, env); len(body) != 0 {
		t.Errorf("an empty request sent %v", body)
	}
}

func TestGeolocationWifiAndCells(t *testing.T) {
	env := newLocationEnvironment(t, jsonOK(geolocationOK))
	if _, err := env.invokeTool(geolocationLocateID, "read", `{"wifi_access_points":[`+
		`{"mac_address":"01:23:45:67:89:AB","signal_strength":-65,"age":2000,"channel":11,"signal_to_noise_ratio":40},`+
		`{"mac_address":"01:23:45:67:89:AC"}],"carrier":"Example Carrier"}`); err != nil {
		t.Fatal(err)
	}
	want := `{"carrier":"Example Carrier","wifiAccessPoints":[{"age":2000,"channel":11,"macAddress":"01:23:45:67:89:AB",` +
		`"signalStrength":-65,"signalToNoiseRatio":40},{"macAddress":"01:23:45:67:89:AC"}]}`
	if body, _ := json.Marshal(geolocationRequest(t, env)); string(body) != want {
		t.Errorf("body = %s", body)
	}

	env = newLocationEnvironment(t, jsonOK(geolocationOK))
	if _, err := env.invokeTool(geolocationLocateID, "read", `{"home_mobile_country_code":310,"home_mobile_network_code":410,`+
		`"radio_type":"lte","carrier":"Vodafone","consider_ip":true,"cell_towers":[{"cell_id":42,"mobile_country_code":310,`+
		`"mobile_network_code":410,"location_area_code":7,"age":0,"signal_strength":-60,"timing_advance":15}]}`); err != nil {
		t.Fatal(err)
	}
	want = `{"carrier":"Vodafone","cellTowers":[{"age":0,"cellId":42,"locationAreaCode":7,"mobileCountryCode":310,` +
		`"mobileNetworkCode":410,"signalStrength":-60,"timingAdvance":15}],"considerIp":true,"homeMobileCountryCode":310,` +
		`"homeMobileNetworkCode":410,"radioType":"lte"}`
	if body, _ := json.Marshal(geolocationRequest(t, env)); string(body) != want {
		t.Errorf("body = %s", body)
	}

	for name, tc := range map[string]struct{ arguments, want string }{
		"nr": {`{"radio_type":"nr","cell_towers":[{"new_radio_cell_id":68719476735,"mobile_country_code":1,"mobile_network_code":2,"location_area_code":16777215}]}`,
			`{"cellTowers":[{"locationAreaCode":16777215,"mobileCountryCode":1,"mobileNetworkCode":2,"newRadioCellId":68719476735}],"radioType":"nr"}`},
		"cdma": {`{"radio_type":"cdma","home_mobile_network_code":32767,"cell_towers":[{"cell_id":65535,"location_area_code":1,"mobile_network_code":32767}]}`,
			`{"cellTowers":[{"cellId":65535,"locationAreaCode":1,"mobileNetworkCode":32767}],"homeMobileNetworkCode":32767,"radioType":"cdma"}`},
		"default gsm": {`{"cell_towers":[{"cell_id":1,"location_area_code":2,"mobile_country_code":3,"mobile_network_code":4}]}`,
			`{"cellTowers":[{"cellId":1,"locationAreaCode":2,"mobileCountryCode":3,"mobileNetworkCode":4}]}`},
	} {
		env := newLocationEnvironment(t, jsonOK(geolocationOK))
		if _, err := env.invokeTool(geolocationLocateID, "read", tc.arguments); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if body, _ := json.Marshal(geolocationRequest(t, env)); string(body) != tc.want {
			t.Errorf("%s: body = %s", name, body)
		}
	}
}

func TestGeolocationRefusals(t *testing.T) {
	ap := func(mac string) string { return `{"mac_address":"` + mac + `"}` }
	repeat := func(item string, n int) string {
		return "[" + strings.TrimSuffix(strings.Repeat(item+",", n), ",") + "]"
	}
	cell := `{"cell_id":1,"location_area_code":2,"mobile_country_code":3,"mobile_network_code":4}`
	for name, arguments := range map[string]string{
		"one access point":        `{"wifi_access_points":[` + ap("01:23:45:67:89:AB") + `]}`,
		"no access points":        `{"wifi_access_points":[]}`,
		"101 access points":       `{"wifi_access_points":` + repeat(ap("01:23:45:67:89:AB"), 101) + `}`,
		"bad mac":                 `{"wifi_access_points":[` + ap("0123456789AB") + `,` + ap("01:23:45:67:89:AC") + `]}`,
		"mac injection":           `{"wifi_access_points":[` + ap("01:23:45:67:89:A\\n") + `,` + ap("01:23:45:67:89:AC") + `]}`,
		"broadcast":               `{"wifi_access_points":[` + ap("ff:ff:ff:ff:ff:ff") + `,` + ap("01:23:45:67:89:AC") + `]}`,
		"iana range":              `{"wifi_access_points":[` + ap("00:00:5E:12:34:56") + `,` + ap("01:23:45:67:89:AC") + `]}`,
		"signal above -10":        `{"wifi_access_points":[{"mac_address":"01:23:45:67:89:AB","signal_strength":-9},` + ap("01:23:45:67:89:AC") + `]}`,
		"signal below -128":       `{"wifi_access_points":[{"mac_address":"01:23:45:67:89:AB","signal_strength":-129},` + ap("01:23:45:67:89:AC") + `]}`,
		"negative age":            `{"wifi_access_points":[{"mac_address":"01:23:45:67:89:AB","age":-1},` + ap("01:23:45:67:89:AC") + `]}`,
		"unknown wifi member":     `{"wifi_access_points":[{"mac_address":"01:23:45:67:89:AB","ssid":"x"},` + ap("01:23:45:67:89:AC") + `]}`,
		"33 cells":                `{"cell_towers":` + repeat(cell, 33) + `}`,
		"no cells":                `{"cell_towers":[]}`,
		"unknown radio":           `{"radio_type":"5g"}`,
		"uppercase radio":         `{"radio_type":"LTE"}`,
		"mcc 1000":                `{"home_mobile_country_code":1000}`,
		"mnc 1000 gsm":            `{"home_mobile_network_code":1000}`,
		"mnc sid with lte":        `{"radio_type":"lte","home_mobile_network_code":32767}`,
		"home mcc cdma":           `{"radio_type":"cdma","home_mobile_country_code":1}`,
		"carrier control":         `{"carrier":"a\u0000b"}`,
		"carrier long":            `{"carrier":"` + strings.Repeat("a", 101) + `"}`,
		"consider ip string":      `{"consider_ip":"false"}`,
		"cell without id":         `{"cell_towers":[{"location_area_code":2,"mobile_country_code":3,"mobile_network_code":4}]}`,
		"cell without network":    `{"cell_towers":[{"cell_id":1,"location_area_code":2,"mobile_country_code":3}]}`,
		"gsm cell id 65536":       `{"cell_towers":[{"cell_id":65536,"location_area_code":2,"mobile_country_code":3,"mobile_network_code":4}]}`,
		"lte cell id too big":     `{"radio_type":"lte","cell_towers":[{"cell_id":268435456,"mobile_country_code":3,"mobile_network_code":4}]}`,
		"gsm without area":        `{"cell_towers":[{"cell_id":1,"mobile_country_code":3,"mobile_network_code":4}]}`,
		"gsm without mcc":         `{"cell_towers":[{"cell_id":1,"location_area_code":2,"mobile_network_code":4}]}`,
		"cdma with mcc":           `{"radio_type":"cdma","cell_towers":[{"cell_id":1,"location_area_code":2,"mobile_country_code":3,"mobile_network_code":4}]}`,
		"nr with cell id":         `{"radio_type":"nr","cell_towers":[{"cell_id":1,"new_radio_cell_id":2,"mobile_country_code":3,"mobile_network_code":4}]}`,
		"nr without nr id":        `{"radio_type":"nr","cell_towers":[{"mobile_country_code":3,"mobile_network_code":4}]}`,
		"nr id on lte":            `{"radio_type":"lte","cell_towers":[{"cell_id":1,"new_radio_cell_id":2,"mobile_country_code":3,"mobile_network_code":4}]}`,
		"nr id too big":           `{"radio_type":"nr","cell_towers":[{"new_radio_cell_id":68719476736,"mobile_country_code":3,"mobile_network_code":4}]}`,
		"area code 65536 on lte":  `{"radio_type":"lte","cell_towers":[{"cell_id":1,"location_area_code":65536,"mobile_country_code":3,"mobile_network_code":4}]}`,
		"unknown cell member":     `{"cell_towers":[{"cell_id":1,"location_area_code":2,"mobile_country_code":3,"mobile_network_code":4,"psc":1}]}`,
		"negative timing advance": `{"cell_towers":[{"cell_id":1,"location_area_code":2,"mobile_country_code":3,"mobile_network_code":4,"timing_advance":-1}]}`,
		"key argument":            `{"key":"x"}`,
		"url argument":            `{"url":"https://evil.example"}`,
		"body argument":           `{"body":{"considerIp":true}}`,
	} {
		env := newLocationEnvironment(t, nil)
		if _, err := env.invokeTool(geolocationLocateID, "read", arguments); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
	// The handler checks what the schema checks first, before any secret access.
	resolved := &config.Resolved{BaseURL: apiRoot}
	for name, arguments := range map[string]string{
		"one ap": `{"wifi_access_points":[` + ap("01:23:45:67:89:AB") + `]}`, "radio": `{"radio_type":"x"}`,
		"nr":  `{"radio_type":"nr","cell_towers":[{"cell_id":1,"mobile_network_code":1}]}`,
		"mac": `{"wifi_access_points":[` + ap("zz") + `,` + ap("01:23:45:67:89:AC") + `]}`,
	} {
		_, err := handleGeolocationLocate(context.Background(), resolved, nil, nil, json.RawMessage(arguments))
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// 32 cells, 100 access points, and the border signal values are valid.
	env := newLocationEnvironment(t, jsonOK(geolocationOK))
	items := make([]string, 100)
	for i := range items {
		items[i] = fmt.Sprintf(`{"mac_address":"01:23:45:67:89:%02X","signal_strength":-128}`, i)
	}
	if _, err := env.invokeTool(geolocationLocateID, "read", `{"cell_towers":`+repeat(cell, 32)+`,"wifi_access_points":[`+
		strings.Join(items, ",")+`]}`); err != nil {
		t.Errorf("limits: %v", err)
	}
}

func geolocationErrorBody(code int, domain, reason string) string {
	return fmt.Sprintf(`{"error":{"errors":[{"domain":"%s","reason":"%s","message":"%s %s"}],"code":%d,"message":"%s %s"}}`,
		domain, reason, detailCanary, keyValue, code, detailCanary, keyValue)
}

func TestGeolocationHTTPErrorForms(t *testing.T) {
	for _, tc := range []struct {
		status         int
		domain, reason string
		class          provider.Class
		text           string
	}{
		{400, "usageLimits", "keyInvalid", provider.ClassAuth, "did not accept the key"},
		{400, "global", "parseError", provider.ClassInvalidResponse, "HTTP 400"},
		{403, "usageLimits", "dailyLimitExceeded", provider.ClassRateLimited, "dailyLimitExceeded"},
		{403, "usageLimits", "userRateLimitExceeded", provider.ClassRateLimited, "userRateLimitExceeded"},
		{403, "usageLimits", "somethingNew", provider.ClassPermission, "Geolocation API"},
		{404, "geolocation", "notFound", provider.ClassNotFound, "could not determine a position"},
		{429, "usageLimits", "rateLimitExceeded", provider.ClassRateLimited, "rate-limited"},
		{500, "global", "backendError", provider.ClassProviderError, ""},
	} {
		env := newLocationEnvironment(t, reply(tc.status, "application/json", geolocationErrorBody(tc.status, tc.domain, tc.reason)))
		_, err := env.invokeTool(geolocationLocateID, "read", `{"consider_ip":false}`)
		var failed *provider.Error
		if !errors.As(err, &failed) || failed.Class != tc.class {
			t.Errorf("%d %s: err = %v", tc.status, tc.reason, err)
			continue
		}
		if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), keyValue) ||
			strings.Contains(failed.Message, "Places") || strings.Contains(failed.Message, "somethingNew") ||
			!strings.Contains(failed.Message, tc.text) {
			t.Errorf("%d %s: message = %q", tc.status, tc.reason, failed.Message)
		}
		if failed.Original == nil || failed.Original.Status != tc.status || len(failed.Original.Entries) == 0 ||
			failed.Original.Entries[0].Message != detailCanary+" "+keyValue {
			t.Errorf("%d %s: original = %+v", tc.status, tc.reason, failed.Original)
		}
		found := false
		for _, entry := range failed.Original.Entries {
			found = found || entry.Code == tc.reason
		}
		if !found {
			t.Errorf("%d %s: original lacks the reason: %+v", tc.status, tc.reason, failed.Original)
		}
		if got := env.red.Apply(fmt.Sprintf("%+v", failed.Original)); strings.Contains(got, keyValue) {
			t.Errorf("%d %s: the redacted original keeps the key", tc.status, tc.reason)
		}
		if len(env.fake.recorded()) != 1 {
			t.Errorf("%d %s: %d requests, a request is sent once", tc.status, tc.reason, len(env.fake.recorded()))
		}
	}
	// A 404 without the Google error form is still "no position", and not an error of the key.
	env := newLocationEnvironment(t, reply(404, "text/html", "<html>"+detailCanary+"</html>"))
	if _, err := env.invokeTool(geolocationLocateID, "read", `{}`); classOf(err) != provider.ClassNotFound ||
		strings.Contains(err.Error(), detailCanary) {
		t.Errorf("err = %v", err)
	}
}

func TestLocationServicesUnusableAnswers(t *testing.T) {
	calls := map[string]string{
		elevationGetID:      `{"locations":[{"latitude":1,"longitude":2}]}`,
		timezoneGetID:       `{"location":{"latitude":1,"longitude":2},"timestamp":1}`,
		geolocationLocateID: `{}`,
	}
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"not json": reply(200, "text/plain", "x"),
		"html":     reply(200, "text/html", "<html>"+keyValue+"</html>"),
		"cut":      reply(200, "application/json", `{"results":[`),
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/?"+r.URL.RawQuery, http.StatusFound)
		},
		"too large": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"OK","a":"`+strings.Repeat("x", maxResponseBytes)+`"}`)
		},
	} {
		for id, arguments := range calls {
			env := newLocationEnvironment(t, answer)
			_, err := env.invokeTool(id, "read", arguments)
			if err == nil {
				t.Errorf("%s %s: no error", id, name)
			}
			if err != nil && strings.Contains(fmt.Sprintf("%+v", err), keyValue) {
				t.Errorf("%s %s: the error carries the key: %v", id, name, err)
			}
			if n := len(env.fake.recorded()); n != 1 {
				t.Errorf("%s %s: %d requests", id, name, n)
			}
		}
	}
}

func TestLegacyAnswerWithoutStatusIsInvalid(t *testing.T) {
	for id, arguments := range map[string]string{
		elevationGetID: `{"locations":[{"latitude":1,"longitude":2}]}`,
		timezoneGetID:  `{"location":{"latitude":1,"longitude":2},"timestamp":1}`,
	} {
		env := newLocationEnvironment(t, jsonOK(`{"results":[]}`))
		if _, err := env.invokeTool(id, "read", arguments); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}

func TestLocationServicesKeyNeverLeaks(t *testing.T) {
	for id, tc := range map[string]struct{ arguments, url string }{
		elevationGetID:      {`{"locations":[{"latitude":1,"longitude":2}]}`, geocodingRoot + elevationPath},
		timezoneGetID:       {`{"location":{"latitude":1,"longitude":2},"timestamp":1}`, geocodingRoot + timezonePath},
		geolocationLocateID: {`{}`, geolocationRoot + geolocationPath},
	} {
		env := newLocationEnvironment(t, jsonOK(`{}`))
		// A transport error quotes the whole URL, as Go's url.Error does.
		for _, cause := range []error{
			fmt.Errorf("Get %q: dial tcp: lookup failed", tc.url+"?key="+keyValue),
			&url.Error{Op: "Post", URL: tc.url + "?key=" + keyValue, Err: errors.New("boom " + keyValue)},
			context.DeadlineExceeded,
		} {
			transport = failingTransport{err: cause}
			_, err := env.invokeTool(id, "read", tc.arguments)
			if err == nil || strings.Contains(err.Error(), keyValue) || strings.Contains(fmt.Sprintf("%+v", err), keyValue) {
				t.Errorf("%s: err = %v", id, err)
			}
			var failed *provider.Error
			if errors.As(err, &failed) && failed.Original != nil {
				t.Errorf("%s: original = %+v", id, failed.Original)
			}
		}
		if got := env.red.Apply("a " + keyValue + " b"); strings.Contains(got, keyValue) {
			t.Errorf("%s: the redactor leaves the key: %q", id, got)
		}
	}
}

func TestLocationServicesHostsPathsAndConnection(t *testing.T) {
	if geocodingRoot != "https://maps.googleapis.com" || elevationPath != "/maps/api/elevation/json" ||
		timezonePath != "/maps/api/timezone/json" || geolocationRoot != "https://www.googleapis.com" ||
		geolocationPath != "/geolocation/v1/geolocate" || geolocationHost != "www.googleapis.com" {
		t.Fatal("endpoint changed")
	}
	for id, arguments := range map[string]string{
		elevationGetID:      `{"locations":[{"latitude":1,"longitude":2}]}`,
		timezoneGetID:       `{"location":{"latitude":1,"longitude":2},"timestamp":1}`,
		geolocationLocateID: `{}`,
	} {
		env := newLocationEnvironment(t, jsonOK(`{"status":"OK"}`))
		if _, err := env.invokeTool(id, "bound", arguments); err == nil || len(env.fake.recorded()) != 0 {
			t.Errorf("%s: a connection with a target was served: %v", id, err)
		}
		_, err := map[string]func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor,
			json.RawMessage) (any, error){elevationGetID: handleElevationGet, timezoneGetID: handleTimezoneGet,
			geolocationLocateID: handleGeolocationLocate}[id](context.Background(),
			&config.Resolved{BaseURL: "https://evil.example"}, nil, nil, json.RawMessage(arguments))
		if classOf(err) != provider.ClassProviderError {
			t.Errorf("%s: foreign base URL: err = %v", id, err)
		}
	}
	metadata, _ := registry(t).ProviderMetadata(Provider)
	if metadata.ValidateBaseURL(geolocationRoot) == nil {
		t.Error("the Geolocation origin may be a base URL")
	}
}

func lookupDescriptor(all []capability.Descriptor, id string) capability.Descriptor {
	for _, d := range all {
		if d.ID == id {
			return d
		}
	}
	return capability.Descriptor{}
}

func TestLocationServicesRiskDescriptionAndSchemas(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	inProfile := map[string]bool{}
	for _, id := range metadata.Profiles[0].Tools {
		inProfile[id] = true
	}
	for _, d := range []struct {
		id, description string
		input           json.RawMessage
		arguments       []string
		sku             []string
	}{
		{elevationGetID, elevationGet.Description, elevationGet.InputSchema, argumentNames(elevationGet.Arguments),
			[]string{"Pro", "5000"}},
		{timezoneGetID, timezoneGet.Description, timezoneGet.InputSchema, argumentNames(timezoneGet.Arguments),
			[]string{"Essentials", "10000"}},
		{geolocationLocateID, geolocationLocate.Description, geolocationLocate.InputSchema,
			argumentNames(geolocationLocate.Arguments), []string{"Essentials", "10000", "MAC"}},
	} {
		descriptor := lookupDescriptor(reg.Provider(Provider), d.id)
		if descriptor.Risk != locationRisk || locationRisk.DataSensitivity != "google-maps-movement-data" ||
			locationRisk.Effect != capability.EffectRead || locationRisk.Idempotency != capability.IdempotencySafe ||
			locationRisk.Confirmation != capability.ConfirmationNone ||
			!locationRisk.OpenWorld || !inProfile[d.id] {
			t.Errorf("%s: risk = %+v, in profile %v", d.id, descriptor.Risk, inProfile[d.id])
		}
		for _, want := range append([]string{"charges above the free usage", "attribution", "storage rules", "not verified",
			"untrusted", "query parameter", "never shows the URL"}, d.sku...) {
			if !strings.Contains(d.description, want) {
				t.Errorf("%s: description lacks %q", d.id, want)
			}
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(d.input, &schema) != nil {
			t.Fatalf("%s: input schema = %s", d.id, d.input)
		}
		if len(schema.Properties) != len(d.arguments) {
			t.Errorf("%s: %d properties, %d documented arguments", d.id, len(schema.Properties), len(d.arguments))
		}
		for name := range schema.Properties {
			found := false
			for _, argument := range d.arguments {
				found = found || argument == name
			}
			if !found {
				t.Errorf("%s: argument %s is undocumented", d.id, name)
			}
		}
		for _, banned := range []string{"key", "target", "url", "host", "path_template", "body", "header"} {
			if schema.Properties[banned] != nil {
				t.Errorf("%s: tool takes %s", d.id, banned)
			}
		}
	}
}
