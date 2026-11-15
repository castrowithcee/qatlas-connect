package googlemaps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// roadsRewrite sends the Roads host to the test server and refuses every other host.
type roadsRewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (r roadsRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "roads.googleapis.com" || req.URL.Scheme != "https" {
		return nil, errors.New("unexpected host")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set(originalHost, req.URL.Host)
	clone.URL.Scheme, clone.URL.Host, clone.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(clone)
}

func newRoadsEnvironment(t *testing.T, f *fakeGoogle) *environment {
	t.Helper()
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	previous := transport
	transport = roadsRewrite{target: target, base: server.Client().Transport}
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

const (
	roadsPathArgument = `[{"latitude":-35.27801,"longitude":149.12958},{"latitude":-35.28032,"longitude":149.12907}]`
	roadsSnapBody     = `{"snappedPoints":[{"location":{"latitude":-35.278,"longitude":149.1295},"originalIndex":0,` +
		`"placeId":"ChIJr_xl0GdNFmsRsUtUbW7qABM","surprise":1},{"location":{"latitude":-35.2784,"longitude":149.1294},` +
		`"placeId":"ChIJ6aXGemhNFmsRZE_zHqhmrG4"}],"warningMessage":"Input path is too sparse.","unknownFuture":1}`
	roadsSpeedBody = `{"speedLimits":[{"placeId":"ChIJX12duJAwGQ0Ra0d4Oi4jOGE","speedLimit":105,"units":"KPH","x":1},` +
		`{"placeId":"ChIJLQcticc0GQ0RoiNZJVa5GxU","speedLimit":70.5,"units":"KPH"}],` +
		`"snappedPoints":[{"location":{"latitude":38.758,"longitude":-9.037},"originalIndex":0,"placeId":"ChIJX12duJAwGQ0Ra0d4Oi4jOGE"}],` +
		`"warningMessage":"Input path is too sparse."}`
)

func roadsOK(body string) *fakeGoogle {
	return &fakeGoogle{answer: reply(200, "application/json; charset=UTF-8", body)}
}

func roadsRequest(t *testing.T, env *environment) (recorded, url.Values) {
	t.Helper()
	r, query := lastQuery(t, env)
	if r.method != http.MethodGet || r.header.Get(originalHost) != "roads.googleapis.com" {
		t.Errorf("request = %+v", r)
	}
	if r.header.Get("X-Goog-Api-Key") != "" || r.header.Get("X-Goog-FieldMask") != "" {
		t.Error("the request carries a key or mask header; Roads takes the key in the query")
	}
	if query.Get("key") != keyValue || len(query["key"]) != 1 {
		t.Errorf("the key is not in the query exactly once: %v", query)
	}
	return r, query
}

func decodeResult(t *testing.T, result string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("result = %s", result)
	}
	return got
}

func TestRoadsSnapWithAndWithoutInterpolate(t *testing.T) {
	env := newRoadsEnvironment(t, roadsOK(roadsSnapBody))
	result, err := env.invokeTool(roadsSnapID, "read", `{"path":`+roadsPathArgument+`,"interpolate":true,`+
		`"travel_mode":"DRIVING","asset_id":"truck 7 & co"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r, query := roadsRequest(t, env)
	if r.path != "/v1/snapToRoads" {
		t.Errorf("path = %s", r.path)
	}
	for name, want := range map[string]string{"path": "-35.27801,149.12958|-35.28032,149.12907", "interpolate": "true",
		"travelMode": "DRIVING", "assetId": "truck 7 & co"} {
		if query.Get(name) != want {
			t.Errorf("query %s = %q, want %q", name, query.Get(name), want)
		}
	}
	got := decodeResult(t, result)
	points, _ := got["snappedPoints"].([]any)
	if len(points) != 2 || got["warningMessage"] != "Input path is too sparse." {
		t.Fatalf("result = %s", result)
	}
	first := points[0].(map[string]any)
	if first["originalIndex"] != float64(0) || first["placeId"] != "ChIJr_xl0GdNFmsRsUtUbW7qABM" ||
		first["location"].(map[string]any)["latitude"] != -35.278 || first["surprise"] != nil {
		t.Errorf("first point = %v", first)
	}
	if _, ok := points[1].(map[string]any)["originalIndex"]; ok {
		t.Error("an interpolated point has an originalIndex")
	}
	if got["unknownFuture"] != nil || strings.Contains(result, keyValue) {
		t.Errorf("result = %s", result)
	}

	env = newRoadsEnvironment(t, roadsOK(roadsSnapBody))
	if _, err := env.invokeTool(roadsSnapID, "read", `{"path":`+roadsPathArgument+`,"interpolate":false}`); err != nil {
		t.Fatal(err)
	}
	if _, query := roadsRequest(t, env); query.Get("interpolate") != "false" || query.Has("travelMode") || query.Has("assetId") {
		t.Errorf("query = %v", query)
	}
	env = newRoadsEnvironment(t, roadsOK(roadsSnapBody))
	if _, err := env.invokeTool(roadsSnapID, "read", `{"path":`+roadsPathArgument+`}`); err != nil {
		t.Fatal(err)
	}
	if _, query := roadsRequest(t, env); query.Has("interpolate") {
		t.Errorf("an omitted interpolate was sent: %v", query)
	}
}

func roadsPoints(n int) string {
	points := make([]string, n)
	for i := range points {
		points[i] = fmt.Sprintf(`{"latitude":%d.5,"longitude":%d.25}`, i%90, i%180)
	}
	return "[" + strings.Join(points, ",") + "]"
}

func TestRoadsPointLimitsAndInvalidCoordinates(t *testing.T) {
	for _, tool := range []struct{ id, argument, rest string }{
		{roadsSnapID, "path", ""}, {roadsNearestID, "points", ""}, {roadsSpeedLimitsID, "path", ""},
	} {
		env := newRoadsEnvironment(t, roadsOK(`{}`))
		if _, err := env.invokeTool(tool.id, "read", `{"`+tool.argument+`":`+roadsPoints(100)+`}`); err != nil {
			t.Errorf("%s: 100 points: %v", tool.id, err)
		}
		if _, query := roadsRequest(t, env); strings.Count(query.Get(tool.argument), "|") != 99 {
			t.Errorf("%s: query = %v", tool.id, query.Get(tool.argument))
		}
		for name, points := range map[string]string{
			"101 points":     roadsPoints(101),
			"no points":      `[]`,
			"latitude high":  `[{"latitude":90.1,"longitude":0}]`,
			"latitude low":   `[{"latitude":-91,"longitude":0}]`,
			"longitude high": `[{"latitude":0,"longitude":180.5}]`,
			"longitude low":  `[{"latitude":0,"longitude":-181}]`,
			"no longitude":   `[{"latitude":0}]`,
			"as a string":    `"1,2|3,4"`,
			"pipe injection": `["1,2|3,4"]`,
		} {
			env := newRoadsEnvironment(t, &fakeGoogle{})
			if _, err := env.invokeTool(tool.id, "read", `{"`+tool.argument+`":`+points+`}`); err == nil {
				t.Errorf("%s %s: no error", tool.id, name)
			}
			if len(env.fake.recorded()) != 0 || *env.reads != 0 {
				t.Errorf("%s %s: %d requests, %d secret reads", tool.id, name, len(env.fake.recorded()), *env.reads)
			}
		}
	}
	// The handlers check on their own what the schema checks first, before any secret access.
	resolved := &config.Resolved{BaseURL: apiRoot}
	for name, call := range map[string]func() (any, error){
		"snap": func() (any, error) {
			return handleRoadsSnap(context.Background(), resolved, nil, nil, json.RawMessage(`{"path":[{"latitude":95,"longitude":0}]}`))
		},
		"snap too many": func() (any, error) {
			return handleRoadsSnap(context.Background(), resolved, nil, nil, json.RawMessage(`{"path":`+roadsPoints(101)+`}`))
		},
		"nearest": func() (any, error) {
			return handleRoadsNearest(context.Background(), resolved, nil, nil, json.RawMessage(`{"points":[{"latitude":0}]}`))
		},
		"speed both": func() (any, error) {
			return handleRoadsSpeedLimits(context.Background(), resolved, nil, nil,
				json.RawMessage(`{"path":`+roadsPathArgument+`,"place_ids":["a"]}`))
		},
		"speed none": func() (any, error) {
			return handleRoadsSpeedLimits(context.Background(), resolved, nil, nil, json.RawMessage(`{}`))
		},
		"travel mode": func() (any, error) {
			return handleRoadsSnap(context.Background(), resolved, nil, nil,
				json.RawMessage(`{"path":`+roadsPathArgument+`,"travel_mode":"FLYING"}`))
		},
		"asset id": func() (any, error) {
			return handleRoadsSnap(context.Background(), resolved, nil, nil,
				json.RawMessage(`{"path":`+roadsPathArgument+`,"asset_id":"a\nb"}`))
		},
	} {
		_, err := call()
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an invalid request before any secret access", name, err)
		}
		if strings.Contains(fmt.Sprint(err), "95") && name == "snap" {
			t.Errorf("%s: message quotes the value: %v", name, err)
		}
	}
	env := newRoadsEnvironment(t, roadsOK(`{}`))
	if _, err := env.invokeTool(roadsNearestID, "read", `{"points":[{"latitude":90,"longitude":-180},{"latitude":-90,"longitude":180}]}`); err != nil {
		t.Errorf("the border values are valid: %v", err)
	}
}

func TestRoadsEmptyAnswerIsAnEmptyList(t *testing.T) {
	resolver := secret.NewWith(func(string) string { return keyValue }, nil, nil, &redact.Redactor{})
	resolved := &config.Resolved{BaseURL: apiRoot, Credential: "maps-key", Secrets: coreConfig().Credentials["maps-key"]}
	for _, tc := range []struct {
		name, argument, member string
		call                   func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, json.RawMessage) (any, error)
		arguments              string
	}{
		{"snap", roadsSnapID, "snappedPoints", handleRoadsSnap, `{"path":` + roadsPathArgument + `}`},
		{"nearest", roadsNearestID, "snappedPoints", handleRoadsNearest, `{"points":` + roadsPathArgument + `}`},
		{"speed", roadsSpeedLimitsID, "speedLimits", handleRoadsSpeedLimits, `{"place_ids":["abc"]}`},
	} {
		for _, body := range []string{`{}`, `{"snappedPoints":[]}`, `{"snappedPoints":"x","speedLimits":null}`} {
			newRoadsEnvironment(t, roadsOK(body))
			got, err := tc.call(context.Background(), resolved, resolver, &redact.Redactor{}, json.RawMessage(tc.arguments))
			answer, _ := got.(map[string]any)
			list, ok := answer[tc.member].([]any)
			if err != nil || !ok || len(list) != 0 {
				t.Errorf("%s %s: result = %v, err = %v", tc.name, body, got, err)
			}
		}
	}
	env := newRoadsEnvironment(t, roadsOK(`{}`))
	if _, err := env.invokeTool(roadsSnapID, "read", `{"path":`+roadsPathArgument+`}`); err != nil {
		t.Errorf("an empty answer is no error: %v", err)
	}
}

func TestRoadsNearest(t *testing.T) {
	env := newRoadsEnvironment(t, roadsOK(`{"snappedPoints":[{"location":{"latitude":60.17,"longitude":24.94},"originalIndex":0,`+
		`"placeId":"ChIJfVFHrM0LkkYRBzUQos_jR5w"},{"location":{"latitude":60.18,"longitude":24.95},"originalIndex":0,`+
		`"placeId":"ChIJother"}],"warningMessage":"not documented here"}`))
	result, err := env.invokeTool(roadsNearestID, "read", `{"points":[{"latitude":60.17088,"longitude":24.942795},`+
		`{"latitude":60.170877,"longitude":24.942796}],"travel_mode":"WALKING"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r, query := roadsRequest(t, env)
	if r.path != "/v1/nearestRoads" || query.Get("points") != "60.17088,24.942795|60.170877,24.942796" ||
		query.Get("travelMode") != "WALKING" || query.Has("path") {
		t.Errorf("request = %+v, query = %v", r, query)
	}
	got := decodeResult(t, result)
	if points, _ := got["snappedPoints"].([]any); len(points) != 2 || got["warningMessage"] != nil {
		t.Errorf("result = %s", result)
	}
}

func TestRoadsSpeedLimitsWithPlaceIDsAndWithPath(t *testing.T) {
	env := newRoadsEnvironment(t, roadsOK(`{"speedLimits":[{"placeId":"a","speedLimit":55,"units":"MPH"}]}`))
	result, err := env.invokeTool(roadsSpeedLimitsID, "read", `{"place_ids":["ChIJX12duJAwGQ0Ra0d4Oi4jOGE","ChIJLQcticc0GQ0RoiNZJVa5GxU"],"units":"MPH"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r, query := roadsRequest(t, env)
	if r.path != "/v1/speedLimits" || strings.Join(query["placeId"], ",") != "ChIJX12duJAwGQ0Ra0d4Oi4jOGE,ChIJLQcticc0GQ0RoiNZJVa5GxU" ||
		query.Get("units") != "MPH" || query.Has("path") {
		t.Errorf("request = %+v, query = %v", r, query)
	}
	if !strings.Contains(result, `"speedLimit":55`) || !strings.Contains(result, `"units":"MPH"`) || strings.Contains(result, "snappedPoints") {
		t.Errorf("result = %s", result)
	}

	env = newRoadsEnvironment(t, roadsOK(roadsSpeedBody))
	result, err = env.invokeTool(roadsSpeedLimitsID, "read", `{"path":`+roadsPathArgument+`}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	_, query = roadsRequest(t, env)
	if query.Get("path") != "-35.27801,149.12958|-35.28032,149.12907" || query.Has("placeId") || query.Has("units") {
		t.Errorf("query = %v", query)
	}
	got := decodeResult(t, result)
	limits, _ := got["speedLimits"].([]any)
	snapped, _ := got["snappedPoints"].([]any)
	if len(limits) != 2 || len(snapped) != 1 || got["warningMessage"] != "Input path is too sparse." {
		t.Fatalf("result = %s", result)
	}
	if first := limits[1].(map[string]any); first["speedLimit"] != 70.5 || first["units"] != "KPH" ||
		first["placeId"] != "ChIJLQcticc0GQ0RoiNZJVa5GxU" || limits[0].(map[string]any)["x"] != nil {
		t.Errorf("limits = %v", limits)
	}

	env = newRoadsEnvironment(t, roadsOK(`{}`))
	if _, err := env.invokeTool(roadsSpeedLimitsID, "read", `{"place_ids":[`+strings.TrimSuffix(strings.Repeat(`"abc",`, 100), ",")+`]}`); err != nil {
		t.Errorf("100 place IDs: %v", err)
	}
}

func TestRoadsSpeedLimitsRefusals(t *testing.T) {
	ids := strings.TrimSuffix(strings.Repeat(`"abc",`, 101), ",")
	for name, arguments := range map[string]string{
		"nothing":            `{}`,
		"both":               `{"path":` + roadsPathArgument + `,"place_ids":["abc"]}`,
		"only units":         `{"units":"KPH"}`,
		"empty ids":          `{"place_ids":[]}`,
		"101 ids":            `{"place_ids":[` + ids + `]}`,
		"id with slash":      `{"place_ids":["a/b"]}`,
		"id with ampersand":  `{"place_ids":["a&key=x"]}`,
		"empty id":           `{"place_ids":[""]}`,
		"id of 257":          `{"place_ids":["` + strings.Repeat("a", 257) + `"]}`,
		"unspecified units":  `{"place_ids":["a"],"units":"SPEED_UNITS_UNSPECIFIED"}`,
		"lowercase units":    `{"place_ids":["a"],"units":"mph"}`,
		"unknown argument":   `{"place_ids":["a"],"key":"x"}`,
		"traffic type":       `{"place_ids":["a"],"type":"CURRENT"}`,
		"travel mode":        `{"place_ids":["a"],"travel_mode":"DRIVING"}`,
		"path of strings":    `{"path":"1,2"}`,
		"place ids a string": `{"place_ids":"abc"}`,
	} {
		env := newRoadsEnvironment(t, &fakeGoogle{})
		if _, err := env.invokeTool(roadsSpeedLimitsID, "read", arguments); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
}

func TestRoadsOtherArgumentRefusals(t *testing.T) {
	path := `"path":` + roadsPathArgument
	for name, call := range map[string][2]string{
		"snap no path":         {roadsSnapID, `{}`},
		"snap unspecified":     {roadsSnapID, `{` + path + `,"travel_mode":"TRAVEL_MODE_UNSPECIFIED"}`},
		"snap lowercase":       {roadsSnapID, `{` + path + `,"travel_mode":"driving"}`},
		"snap interpolate str": {roadsSnapID, `{` + path + `,"interpolate":"true"}`},
		"snap blank asset":     {roadsSnapID, `{` + path + `,"asset_id":""}`},
		"snap long asset":      {roadsSnapID, `{` + path + `,"asset_id":"` + strings.Repeat("a", 129) + `"}`},
		"snap control asset":   {roadsSnapID, `{` + path + `,"asset_id":"a\u0000b"}`},
		"snap key":             {roadsSnapID, `{` + path + `,"key":"x"}`},
		"snap host":            {roadsSnapID, `{` + path + `,"host":"evil.example"}`},
		"nearest no points":    {roadsNearestID, `{}`},
		"nearest interpolate":  {roadsNearestID, `{"points":` + roadsPathArgument + `,"interpolate":true}`},
		"nearest path":         {roadsNearestID, `{"path":` + roadsPathArgument + `}`},
	} {
		env := newRoadsEnvironment(t, &fakeGoogle{})
		if _, err := env.invokeTool(call[0], "read", call[1]); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
}

func TestRoadsEncodesValuesInTheQuery(t *testing.T) {
	env := newRoadsEnvironment(t, roadsOK(`{}`))
	if _, err := env.invokeTool(roadsSnapID, "read", `{"path":`+roadsPathArgument+`,"asset_id":"a&key=evil#x|y"}`); err != nil {
		t.Fatal(err)
	}
	r, query := roadsRequest(t, env)
	if query.Get("assetId") != "a&key=evil#x|y" || len(query["key"]) != 1 || strings.Contains(r.path, keyValue) {
		t.Errorf("query = %v (%s)", query, r.rawQuery)
	}
}

func TestRoadsErrorFormsKeepProviderTextApart(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		class  provider.Class
		text   string
	}{
		{401, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), provider.ClassAuth, ""},
		{403, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"), provider.ClassPermission, "Roads API"},
		{429, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), provider.ClassRateLimited, ""},
		{404, errorObject(404, "NOT_FOUND", "X"), provider.ClassNotFound, "HTTP 404"},
		{400, errorObject(400, "INVALID_ARGUMENT", "X"), provider.ClassInvalidResponse, "road network"},
		{500, errorObject(500, "INTERNAL", "X"), provider.ClassProviderError, ""},
	} {
		for _, tool := range []struct{ id, arguments string }{
			{roadsSnapID, `{"path":` + roadsPathArgument + `}`},
			{roadsNearestID, `{"points":` + roadsPathArgument + `}`},
			{roadsSpeedLimitsID, `{"place_ids":["abc"]}`},
		} {
			env := newRoadsEnvironment(t, &fakeGoogle{answer: reply(tc.status, "application/json", tc.body)})
			_, err := env.invokeTool(tool.id, "read", tool.arguments)
			if classOf(err) != tc.class {
				t.Errorf("%s %d: class = %q, err = %v", tool.id, tc.status, classOf(err), err)
				continue
			}
			var failed *provider.Error
			_ = errors.As(err, &failed)
			if strings.Contains(failed.Message, detailCanary) || strings.Contains(err.Error(), keyValue) ||
				strings.Contains(failed.Message, "Places") || strings.Contains(failed.Message, "Routes") ||
				strings.Contains(failed.Message, "roads.googleapis.com") || !strings.Contains(failed.Message, tc.text) {
				t.Errorf("%s %d: message = %q", tool.id, tc.status, failed.Message)
			}
			if len(env.fake.recorded()) != 1 {
				t.Errorf("%s %d: %d requests, want one", tool.id, tc.status, len(env.fake.recorded()))
			}
		}
	}
}

func TestRoadsUnusableAnswers(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"not json": reply(200, "text/plain", "x"),
		"html":     reply(200, "text/html", "<html>"+keyValue+"</html>"),
		"cut":      reply(200, "application/json", `{"snappedPoints":[`),
		"array":    reply(200, "application/json", `[]`),
		"null":     reply(200, "application/json", `null`),
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/?"+r.URL.RawQuery, http.StatusFound)
		},
		"too large": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"warningMessage":"`+strings.Repeat("x", maxResponseBytes)+`"}`)
		},
	} {
		env := newRoadsEnvironment(t, &fakeGoogle{answer: answer})
		_, err := env.invokeTool(roadsSnapID, "read", `{"path":`+roadsPathArgument+`}`)
		if classOf(err) != provider.ClassInvalidResponse && classOf(err) != provider.ClassProviderError {
			t.Errorf("%s: err = %v", name, err)
		}
		if err != nil && strings.Contains(fmt.Sprintf("%+v", err), keyValue) {
			t.Errorf("%s: the error carries the key: %v", name, err)
		}
		if n := len(env.fake.recorded()); n != 1 {
			t.Errorf("%s: %d requests", name, n)
		}
	}
}

func TestRoadsKeyNeverLeaks(t *testing.T) {
	env := newRoadsEnvironment(t, roadsOK(roadsSnapBody))
	arguments := `{"path":` + roadsPathArgument + `}`
	// A transport error quotes the whole URL, as Go's url.Error does.
	for _, cause := range []error{
		fmt.Errorf("Get %q: dial tcp: lookup failed", roadsRoot+roadsSnapPath+"?key="+keyValue),
		&url.Error{Op: "Get", URL: roadsRoot + roadsSnapPath + "?key=" + keyValue, Err: errors.New("boom " + keyValue)},
		context.DeadlineExceeded,
	} {
		transport = failingTransport{err: cause}
		_, err := env.invokeTool(roadsSnapID, "read", arguments)
		if err == nil || strings.Contains(err.Error(), keyValue) || strings.Contains(fmt.Sprintf("%+v", err), keyValue) {
			t.Errorf("err = %v", err)
		}
		var failed *provider.Error
		if errors.As(err, &failed) && failed.Original != nil {
			t.Errorf("original = %+v", failed.Original)
		}
	}
	// The key that Google echoes in an error body is masked in the original.
	env = newRoadsEnvironment(t, &fakeGoogle{answer: reply(403, "application/json", errorObject(403, "PERMISSION_DENIED", "X"))})
	_, err := env.invokeTool(roadsSnapID, "read", arguments)
	var failed *provider.Error
	if !errors.As(err, &failed) || failed.Original == nil {
		t.Fatalf("err = %v", err)
	}
	if got := env.red.Apply(fmt.Sprintf("%+v", failed.Original)); strings.Contains(got, keyValue) {
		t.Errorf("redacted original still has the key: %s", got)
	}
	if got := env.red.Apply("a " + keyValue + " b"); strings.Contains(got, keyValue) {
		t.Errorf("redactor leaves the key: %q", got)
	}
}

func TestRoadsHostAndPathAreFixed(t *testing.T) {
	if roadsRoot != "https://roads.googleapis.com" || roadsSnapPath != "/v1/snapToRoads" ||
		roadsNearestPath != "/v1/nearestRoads" || roadsSpeedPath != "/v1/speedLimits" {
		t.Fatal("endpoint changed")
	}
	if _, err := handleRoadsSnap(context.Background(), &config.Resolved{BaseURL: "https://evil.example"}, nil, nil,
		json.RawMessage(`{"path":`+roadsPathArgument+`}`)); classOf(err) != provider.ClassProviderError {
		t.Errorf("foreign base URL: err = %v", err)
	}
	env := newRoadsEnvironment(t, roadsOK(`{}`))
	if _, err := env.invokeTool(roadsSnapID, "bound", `{"path":`+roadsPathArgument+`}`); err == nil || len(env.fake.recorded()) != 0 {
		t.Errorf("a connection with a target was served: %v", err)
	}
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	if metadata.ValidateBaseURL(roadsRoot) == nil {
		t.Error("the Roads origin may be a base URL")
	}
}

func TestRoadsAnswersAreBounded(t *testing.T) {
	points := make([]string, maxRoadsSnappedPoint+50)
	for i := range points {
		points[i] = `{"location":{"latitude":1,"longitude":2},"originalIndex":1,"placeId":"p"}`
	}
	env := newRoadsEnvironment(t, roadsOK(`{"snappedPoints":[`+strings.Join(points, ",")+`],"warningMessage":"`+
		strings.Repeat("w", maxTextRunes+100)+`"}`))
	result, err := env.invokeTool(roadsSnapID, "read", `{"path":`+roadsPathArgument+`}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, result)
	if list, _ := got["snappedPoints"].([]any); len(list) != maxRoadsSnappedPoint {
		t.Errorf("points = %d", len(list))
	}
	if warning, _ := got["warningMessage"].(string); len([]rune(warning)) != maxTextRunes+1 || !strings.HasSuffix(warning, "…") {
		t.Errorf("warning has %d runes", len([]rune(warning)))
	}
	// A place ID that does not fit is left out, never cut.
	env = newRoadsEnvironment(t, roadsOK(`{"snappedPoints":[{"location":{"latitude":1,"longitude":2},"placeId":"`+
		strings.Repeat("a", maxRoadsPlaceIDBytes+1)+`"}]}`))
	result, err = env.invokeTool(roadsSnapID, "read", `{"path":`+roadsPathArgument+`}`)
	if err != nil || strings.Contains(result, "placeId") {
		t.Errorf("result = %s, err = %v", result, err)
	}
}

func TestRoadsToolsRiskDescriptionAndSchemas(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	inProfile := map[string]bool{}
	for _, id := range metadata.Profiles[0].Tools {
		inProfile[id] = true
	}
	for _, d := range []struct {
		id          string
		description string
		input       json.RawMessage
		arguments   []string
		risk        capability.Risk
	}{
		{roadsSnapID, roadsSnap.Description, roadsSnap.InputSchema, argumentNames(roadsSnap.Arguments), roadsSnap.Risk},
		{roadsNearestID, roadsNearest.Description, roadsNearest.InputSchema, argumentNames(roadsNearest.Arguments), roadsNearest.Risk},
		{roadsSpeedLimitsID, roadsSpeedLimits.Description, roadsSpeedLimits.InputSchema, argumentNames(roadsSpeedLimits.Arguments),
			roadsSpeedLimits.Risk},
	} {
		if d.risk != roadsRisk || roadsRisk.DataSensitivity != "google-maps-movement-data" || roadsRisk.Effect != capability.EffectRead ||
			roadsRisk.Idempotency != capability.IdempotencySafe || roadsRisk.Confirmation != capability.ConfirmationNone || !roadsRisk.OpenWorld || !inProfile[d.id] {
			t.Errorf("%s: risk = %+v, in profile %v", d.id, d.risk, inProfile[d.id])
		}
		for _, want := range []string{"charges above the free usage", "Pro", "attribution", "Place IDs may be stored",
			"not verified", "untrusted", "movement data", "query parameter", "never shows the URL"} {
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
		for name := range schema.Properties {
			found := false
			for _, argument := range d.arguments {
				found = found || argument == name
			}
			if !found {
				t.Errorf("%s: argument %s is undocumented", d.id, name)
			}
		}
		for _, banned := range []string{"key", "target", "url", "host", "path_template"} {
			if schema.Properties[banned] != nil {
				t.Errorf("%s: tool takes %s", d.id, banned)
			}
		}
	}
	if !strings.Contains(roadsSpeedLimits.Description, "Asset Tracking") || !strings.Contains(roadsSpeedLimits.Description, "limited access") {
		t.Error("the speed limit description lacks the access note")
	}
}

func argumentNames(arguments []capability.Argument) []string {
	names := make([]string, len(arguments))
	for i, argument := range arguments {
		names[i] = argument.Name
	}
	return names
}
