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

const (
	areaHost    = "areainsights.googleapis.com"
	areaCircle1 = `"area":{"circle":{"center":{"latitude":37.7749,"longitude":-122.4194},"radius_meters":5000}}`
	areaTypes1  = `"included_types":["restaurant"]`
)

// areaRewrite sends the Places Aggregate host to the test server and refuses every other host.
type areaRewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (r areaRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != areaHost || req.URL.Scheme != "https" {
		return nil, errors.New("unexpected host")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set(originalHost, req.URL.Host)
	clone.URL.Scheme, clone.URL.Host, clone.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(clone)
}

func newAreaEnvironment(t *testing.T, f *fakeGoogle) *environment {
	t.Helper()
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	previous := transport
	transport = areaRewrite{target: target, base: server.Client().Transport}
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

func areaCall(env *environment, arguments string) (string, error) {
	return env.invokeTool(areaInsightsComputeID, "read", arguments)
}

func areaBodyOf(t *testing.T, r recorded) string {
	t.Helper()
	var body any
	if err := json.Unmarshal([]byte(r.body), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	out, _ := json.Marshal(body)
	return string(out)
}

func TestAreaInsightsCountAtACircle(t *testing.T) {
	env := newAreaEnvironment(t, &fakeGoogle{answer: reply(200, "application/json; charset=UTF-8",
		`{"count":"1234","surprise":1}`)})
	result, err := areaCall(env, `{"insights":["INSIGHT_COUNT"],`+areaCircle1+`,`+areaTypes1+`,"min_rating":4}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	requests := env.fake.recorded()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	r := requests[0]
	if r.method != http.MethodPost || r.path != "/v1:computeInsights" || r.rawQuery != "" ||
		r.header.Get(originalHost) != areaHost || r.header.Get("X-Goog-Api-Key") != keyValue ||
		r.header.Get("X-Goog-FieldMask") != "" || r.header.Get("Content-Type") != "application/json" {
		t.Errorf("request = %+v", r)
	}
	want := `{"filter":{"locationFilter":{"circle":{"latLng":{"latitude":37.7749,"longitude":-122.4194},"radius":5000}},` +
		`"ratingFilter":{"minRating":4},"typeFilter":{"includedTypes":["restaurant"]}},"insights":["INSIGHT_COUNT"]}`
	if got := areaBodyOf(t, r); got != want {
		t.Errorf("body = %s", got)
	}
	if result != `{"count":1234}` {
		t.Errorf("result = %s", result)
	}
}

func TestAreaInsightsPlaceIDsAtARegionAndAPointPlace(t *testing.T) {
	answer := `{"placeInsights":[{"place":"places/ChIJabc_-1"},{"place":"places/XYZ","extra":1}],"count":"2"}`
	env := newAreaEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", answer)})
	result, err := areaCall(env, `{"insights":["INSIGHT_PLACES","INSIGHT_COUNT"],`+
		`"area":{"region":{"place_id":"ChIJPV4oX_65j4ARVW8IJ6IJUYs"}},"included_primary_types":["cafe"],`+
		`"excluded_primary_types":["coffee_shop"]}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	want := `{"filter":{"locationFilter":{"region":{"place":"places/ChIJPV4oX_65j4ARVW8IJ6IJUYs"}},` +
		`"typeFilter":{"excludedPrimaryTypes":["coffee_shop"],"includedPrimaryTypes":["cafe"]}},` +
		`"insights":["INSIGHT_PLACES","INSIGHT_COUNT"]}`
	if got := areaBodyOf(t, env.fake.recorded()[0]); got != want {
		t.Errorf("body = %s", got)
	}
	if result != `{"count":2,"placeInsights":[{"place":"places/ChIJabc_-1"},{"place":"places/XYZ"}]}` {
		t.Errorf("result = %s", result)
	}
	// A place ID with the prefix, as Google writes it, is accepted for a circle center too.
	env = newAreaEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{"count":"0"}`)})
	if _, err := areaCall(env, `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center_place_id":"places/ChIJgUbEo8cfqokR5lP9_Wh_DaM",`+
		`"radius_meters":1000}},`+areaTypes1+`}`); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	want = `{"filter":{"locationFilter":{"circle":{"place":"places/ChIJgUbEo8cfqokR5lP9_Wh_DaM","radius":1000}},` +
		`"typeFilter":{"includedTypes":["restaurant"]}},"insights":["INSIGHT_COUNT"]}`
	if got := areaBodyOf(t, env.fake.recorded()[0]); got != want {
		t.Errorf("body = %s", got)
	}
}

func TestAreaInsightsPolygonAndEveryDocumentedFilter(t *testing.T) {
	env := newAreaEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{"count":"7"}`)})
	_, err := areaCall(env, `{"insights":["INSIGHT_COUNT"],"area":{"polygon":{"coordinates":[`+
		`{"latitude":37.776,"longitude":-122.666},{"latitude":37.130,"longitude":-121.898},`+
		`{"latitude":37.326,"longitude":-121.598},{"latitude":37.912,"longitude":-122.247},`+
		`{"latitude":37.776,"longitude":-122.666}]}},`+
		`"included_types":["restaurant","bar"],"excluded_types":["hotel"],`+
		`"included_primary_types":["restaurant"],"excluded_primary_types":["steak_house"],`+
		`"operating_status":["OPERATING_STATUS_OPERATIONAL","OPERATING_STATUS_TEMPORARILY_CLOSED","OPERATING_STATUS_PERMANENTLY_CLOSED"],`+
		`"price_levels":["PRICE_LEVEL_FREE","PRICE_LEVEL_INEXPENSIVE","PRICE_LEVEL_MODERATE","PRICE_LEVEL_EXPENSIVE","PRICE_LEVEL_VERY_EXPENSIVE"],`+
		`"min_rating":1,"max_rating":4.5}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	want := `{"filter":{"locationFilter":{"customArea":{"polygon":{"coordinates":[` +
		`{"latitude":37.776,"longitude":-122.666},{"latitude":37.13,"longitude":-121.898},` +
		`{"latitude":37.326,"longitude":-121.598},{"latitude":37.912,"longitude":-122.247},` +
		`{"latitude":37.776,"longitude":-122.666}]}}},` +
		`"operatingStatus":["OPERATING_STATUS_OPERATIONAL","OPERATING_STATUS_TEMPORARILY_CLOSED","OPERATING_STATUS_PERMANENTLY_CLOSED"],` +
		`"priceLevels":["PRICE_LEVEL_FREE","PRICE_LEVEL_INEXPENSIVE","PRICE_LEVEL_MODERATE","PRICE_LEVEL_EXPENSIVE","PRICE_LEVEL_VERY_EXPENSIVE"],` +
		`"ratingFilter":{"maxRating":4.5,"minRating":1},` +
		`"typeFilter":{"excludedPrimaryTypes":["steak_house"],"excludedTypes":["hotel"],` +
		`"includedPrimaryTypes":["restaurant"],"includedTypes":["restaurant","bar"]}},"insights":["INSIGHT_COUNT"]}`
	if got := areaBodyOf(t, env.fake.recorded()[0]); got != want {
		t.Errorf("body = %s", got)
	}
}

const (
	squareArea  = `{"latitude":0,"longitude":0},{"latitude":0,"longitude":1},{"latitude":1,"longitude":1},{"latitude":1,"longitude":0},{"latitude":0,"longitude":0}`
	validCircle = `"area":{"circle":{"center":{"latitude":1,"longitude":2},"radius_meters":100}}`
)

func TestAreaInsightsRefusesBadArgumentsBeforeKeyAndNetwork(t *testing.T) {
	for name, arguments := range map[string]string{
		"no insights":        `{"area":{"region":{"place_id":"abc"}},` + areaTypes1 + `}`,
		"empty insights":     `{"insights":[],"area":{"region":{"place_id":"abc"}},` + areaTypes1 + `}`,
		"unspecified":        `{"insights":["INSIGHT_UNSPECIFIED"],"area":{"region":{"place_id":"abc"}},` + areaTypes1 + `}`,
		"repeated insight":   `{"insights":["INSIGHT_COUNT","INSIGHT_COUNT"],"area":{"region":{"place_id":"abc"}},` + areaTypes1 + `}`,
		"no area":            `{"insights":["INSIGHT_COUNT"],` + areaTypes1 + `}`,
		"empty area":         `{"insights":["INSIGHT_COUNT"],"area":{},` + areaTypes1 + `}`,
		"two shapes":         `{"insights":["INSIGHT_COUNT"],"area":{"region":{"place_id":"abc"},` + validCircle[8:len(validCircle)-1] + `},` + areaTypes1 + `}`,
		"three shapes":       `{"insights":["INSIGHT_COUNT"],"area":{"region":{"place_id":"abc"},"circle":{"center_place_id":"x","radius_meters":100},"polygon":{"coordinates":[` + squareArea + `]}},` + areaTypes1 + `}`,
		"circle no radius":   `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center":{"latitude":1,"longitude":2}}},` + areaTypes1 + `}`,
		"radius too small":   `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center":{"latitude":1,"longitude":2},"radius_meters":22}},` + areaTypes1 + `}`,
		"radius too large":   `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center":{"latitude":1,"longitude":2},"radius_meters":797885}},` + areaTypes1 + `}`,
		"radius zero":        `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center":{"latitude":1,"longitude":2},"radius_meters":0}},` + areaTypes1 + `}`,
		"circle no center":   `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"radius_meters":100}},` + areaTypes1 + `}`,
		"circle two centers": `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center":{"latitude":1,"longitude":2},"center_place_id":"abc","radius_meters":100}},` + areaTypes1 + `}`,
		"bad latitude":       `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center":{"latitude":91,"longitude":2},"radius_meters":100}},` + areaTypes1 + `}`,
		"bad longitude":      `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center":{"latitude":1,"longitude":181},"radius_meters":100}},` + areaTypes1 + `}`,
		"place with slash":   `{"insights":["INSIGHT_COUNT"],"area":{"region":{"place_id":"a/b"}},` + areaTypes1 + `}`,
		"place with query":   `{"insights":["INSIGHT_COUNT"],"area":{"region":{"place_id":"a?key=1"}},` + areaTypes1 + `}`,
		"empty place":        `{"insights":["INSIGHT_COUNT"],"area":{"region":{"place_id":""}},` + areaTypes1 + `}`,
		"polygon open":       `{"insights":["INSIGHT_COUNT"],"area":{"polygon":{"coordinates":[{"latitude":0,"longitude":0},{"latitude":0,"longitude":1},{"latitude":1,"longitude":1},{"latitude":1,"longitude":0}]}},` + areaTypes1 + `}`,
		"polygon short":      `{"insights":["INSIGHT_COUNT"],"area":{"polygon":{"coordinates":[{"latitude":0,"longitude":0},{"latitude":0,"longitude":1},{"latitude":0,"longitude":0}]}},` + areaTypes1 + `}`,
		"polygon two points": `{"insights":["INSIGHT_COUNT"],"area":{"polygon":{"coordinates":[{"latitude":0,"longitude":0},{"latitude":0,"longitude":1},{"latitude":0,"longitude":1},{"latitude":0,"longitude":0}]}},` + areaTypes1 + `}`,
		"polygon repeats":    `{"insights":["INSIGHT_COUNT"],"area":{"polygon":{"coordinates":[{"latitude":0,"longitude":0},{"latitude":0,"longitude":1},{"latitude":1,"longitude":1},{"latitude":0,"longitude":1},{"latitude":0,"longitude":0}]}},` + areaTypes1 + `}`,
		"polygon range":      `{"insights":["INSIGHT_COUNT"],"area":{"polygon":{"coordinates":[{"latitude":0,"longitude":0},{"latitude":0,"longitude":1},{"latitude":91,"longitude":1},{"latitude":0,"longitude":0}]}},` + areaTypes1 + `}`,
		"polygon extra":      `{"insights":["INSIGHT_COUNT"],"area":{"polygon":{"coordinates":[` + squareArea + `],"holes":[]}},` + areaTypes1 + `}`,
		"no type":            `{"insights":["INSIGHT_COUNT"],` + validCircle + `}`,
		"only excluded":      `{"insights":["INSIGHT_COUNT"],` + validCircle + `,"excluded_types":["hotel"]}`,
		"empty type list":    `{"insights":["INSIGHT_COUNT"],` + validCircle + `,"included_types":[]}`,
		"conflict types":     `{"insights":["INSIGHT_COUNT"],` + validCircle + `,"included_types":["bar","hotel"],"excluded_types":["hotel"]}`,
		"conflict primary":   `{"insights":["INSIGHT_COUNT"],` + validCircle + `,"included_primary_types":["bar"],"excluded_primary_types":["bar"]}`,
		"repeated type":      `{"insights":["INSIGHT_COUNT"],` + validCircle + `,"included_types":["bar","bar"]}`,
		"bad type":           `{"insights":["INSIGHT_COUNT"],` + validCircle + `,"included_types":["Bar Room"]}`,
		"bad status":         `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"operating_status":["OPERATING_STATUS_UNSPECIFIED"]}`,
		"bad price":          `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"price_levels":["PRICE_LEVEL_UNSPECIFIED"]}`,
		"repeated price":     `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"price_levels":["PRICE_LEVEL_FREE","PRICE_LEVEL_FREE"]}`,
		"min above max":      `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"min_rating":4.5,"max_rating":4}`,
		"min low":            `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"min_rating":0.5}`,
		"max high":           `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"max_rating":5.5}`,
		"key argument":       `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"key":"x"}`,
		"body argument":      `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"filter":{}}`,
		"path argument":      `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"path":"/v1"}`,
	} {
		env := newAreaEnvironment(t, &fakeGoogle{})
		if _, err := areaCall(env, arguments); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
}

// The core checks the input schema first; the handler checks the same rules again on its own.
func TestAreaInsightsHandlerChecksArgumentsItself(t *testing.T) {
	resolved := &config.Resolved{BaseURL: apiRoot}
	for name, arguments := range map[string]string{
		"two shapes":     `{"insights":["INSIGHT_COUNT"],"area":{"region":{"place_id":"abc"},` + validCircle[8:len(validCircle)-1] + `},` + areaTypes1 + `}`,
		"no shape":       `{"insights":["INSIGHT_COUNT"],"area":{},` + areaTypes1 + `}`,
		"no area":        `{"insights":["INSIGHT_COUNT"],` + areaTypes1 + `}`,
		"no insights":    `{` + validCircle + `,` + areaTypes1 + `}`,
		"conflict":       `{"insights":["INSIGHT_COUNT"],` + validCircle + `,"included_types":["a"],"excluded_types":["a"]}`,
		"no included":    `{"insights":["INSIGHT_COUNT"],` + validCircle + `}`,
		"ratings":        `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"min_rating":5,"max_rating":1}`,
		"radius":         `{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center_place_id":"a","radius_meters":10}},` + areaTypes1 + `}`,
		"polygon open":   `{"insights":["INSIGHT_COUNT"],"area":{"polygon":{"coordinates":[{"latitude":0,"longitude":0},{"latitude":0,"longitude":1},{"latitude":1,"longitude":1},{"latitude":1,"longitude":0}]}},` + areaTypes1 + `}`,
		"status":         `{"insights":["INSIGHT_COUNT"],` + validCircle + `,` + areaTypes1 + `,"operating_status":["X"]}`,
		"insight value":  `{"insights":["INSIGHT_UNSPECIFIED"],` + validCircle + `,` + areaTypes1 + `}`,
		"bad region id":  `{"insights":["INSIGHT_COUNT"],"area":{"region":{"place_id":"a/b"}},` + areaTypes1 + `}`,
		"type list size": `{"insights":["INSIGHT_COUNT"],` + validCircle + `,"included_types":[` + strings.TrimSuffix(strings.Repeat(`"t",`, 51), ",") + `]}`,
	} {
		_, err := handleAreaInsightsCompute(context.Background(), resolved, nil, nil, json.RawMessage(arguments))
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an invalid request before any secret access", name, err)
		}
	}
	for _, bad := range []*config.Resolved{{BaseURL: areaInsightsRoot}, {BaseURL: apiRoot, Target: "x"}, nil} {
		if _, err := handleAreaInsightsCompute(context.Background(), bad, nil, nil,
			json.RawMessage(`{"insights":["INSIGHT_COUNT"],`+validCircle+`,`+areaTypes1+`}`)); err == nil {
			t.Errorf("connection %+v was accepted", bad)
		}
	}
}

func TestAreaInsightsBoundsOfPolygonAndCircle(t *testing.T) {
	polygon := func(n int) areaInsightsArguments {
		points := make([]geoPoint, 0, n)
		for i := 0; i < n-1; i++ {
			lat, lng := float64(i)/10000, float64(i%7)/1000+float64(i)/100000
			points = append(points, geoPoint{Latitude: &lat, Longitude: &lng})
		}
		first := points[0]
		points = append(points, first)
		a := areaInsightsArguments{Insights: []string{"INSIGHT_COUNT"}, IncludedTypes: []string{"restaurant"}}
		a.Area = &insightsAreaArgument{}
		a.Area.Polygon = &struct {
			Coordinates []geoPoint `json:"coordinates"`
		}{points}
		return a
	}
	if _, err := areaInsightsBody(polygon(7000)); err != nil {
		t.Errorf("7000 vertices: %v", err)
	}
	if _, err := areaInsightsBody(polygon(7001)); err == nil {
		t.Error("7001 vertices were accepted")
	}
	if minCircleRadius != 23 || maxCircleRadius != 797884 {
		t.Errorf("radius bounds = %d to %d", minCircleRadius, maxCircleRadius)
	}
	env := newAreaEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{"count":"1"}`)})
	for _, radius := range []int{23, 797884} {
		if _, err := areaCall(env, fmt.Sprintf(`{"insights":["INSIGHT_COUNT"],"area":{"circle":{"center_place_id":"abc",`+
			`"radius_meters":%d}},`+areaTypes1+`}`, radius)); err != nil {
			t.Errorf("radius %d: %v", radius, err)
		}
	}
}

func TestAreaInsightsAnswerShapes(t *testing.T) {
	ids := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"place":"places/P%d"}`, i)
		}
		return `{"placeInsights":[` + strings.Join(parts, ",") + `]}`
	}
	arguments := `{"insights":["INSIGHT_PLACES"],` + validCircle + `,` + areaTypes1 + `}`
	for name, tc := range map[string]struct{ answer, want string }{
		"empty object":      {`{}`, `{}`},
		"zero count":        {`{"count":"0"}`, `{"count":0}`},
		"numeric count":     {`{"count":12}`, `{"count":12}`},
		"empty list":        {`{"placeInsights":[]}`, `{}`}, // the core leaves empty members out
		"exactly the bound": {ids(100), ""},
		"unknown members":   {`{"surprise":1}`, `{}`},
		"count and places":  {`{"count":"1","placeInsights":[{"place":"places/A"}]}`, `{"count":1,"placeInsights":[{"place":"places/A"}]}`},
	} {
		env := newAreaEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", tc.answer)})
		result, err := areaCall(env, arguments)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if tc.want != "" && result != tc.want {
			t.Errorf("%s: result = %s", name, result)
		}
		if name == "exactly the bound" && strings.Count(result, `"place"`) != 100 {
			t.Errorf("%s: places = %d", name, strings.Count(result, `"place"`))
		}
	}
	for name, answer := range map[string]string{
		"more than 100 IDs": ids(101),
		"not json":          `not json`,
		"array":             `[]`,
		"negative count":    `{"count":"-1"}`,
		"text count":        `{"count":"many"}`,
		"fraction count":    `{"count":1.5}`,
		"object count":      `{"count":{}}`,
		"entry not object":  `{"placeInsights":["places/A"]}`,
		"empty place":       `{"placeInsights":[{"place":""}]}`,
		"missing prefix":    `{"placeInsights":[{"place":"ABC"}]}`,
		"unsafe place":      `{"placeInsights":[{"place":"places/a b"}]}`,
		"list not list":     `{"placeInsights":{"place":"places/A"}}`,
	} {
		contentType := "application/json"
		if name == "not json" {
			contentType = "text/plain"
		}
		env := newAreaEnvironment(t, &fakeGoogle{answer: reply(200, contentType, answer)})
		_, err := areaCall(env, arguments)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: class = %q, err = %v", name, classOf(err), err)
		}
		if len(env.fake.recorded()) != 1 {
			t.Errorf("%s: %d requests, want exactly one", name, len(env.fake.recorded()))
		}
	}
}

func TestAreaInsightsErrorFormsKeepProviderTextApart(t *testing.T) {
	for _, tc := range []struct {
		status        int
		state, reason string
		class         provider.Class
		want          string
	}{
		{401, "UNAUTHENTICATED", "CREDENTIALS_MISSING", provider.ClassAuth, ""},
		{403, "PERMISSION_DENIED", "SERVICE_DISABLED", provider.ClassPermission, "Places Aggregate API"},
		{429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED", provider.ClassRateLimited, ""},
		{400, "INVALID_ARGUMENT", "X", provider.ClassInvalidResponse, "disputed area"},
		{404, "NOT_FOUND", "X", provider.ClassNotFound, "place ID may be unknown"},
		{500, "INTERNAL", "X", provider.ClassProviderError, ""},
	} {
		env := newAreaEnvironment(t, &fakeGoogle{answer: reply(tc.status, "application/json", errorObject(tc.status, tc.state, tc.reason))})
		_, err := areaCall(env, `{"insights":["INSIGHT_COUNT"],`+validCircle+`,`+areaTypes1+`}`)
		if classOf(err) != tc.class {
			t.Errorf("%d: class = %q, err = %v", tc.status, classOf(err), err)
			continue
		}
		var failed *provider.Error
		_ = errors.As(err, &failed)
		if strings.Contains(failed.Message, detailCanary) || strings.Contains(failed.Message, keyValue) ||
			strings.Contains(err.Error(), keyValue) || strings.Contains(failed.Message, "Places API (New)") ||
			!strings.Contains(failed.Message, tc.want) {
			t.Errorf("%d: message = %q", tc.status, failed.Message)
		}
		if strings.Contains(env.red.Apply(fmt.Sprint(failed.Original)), keyValue) {
			t.Errorf("%d: the original carries the key", tc.status)
		}
		if len(env.fake.recorded()) != 1 {
			t.Errorf("%d: %d requests, want exactly one", tc.status, len(env.fake.recorded()))
		}
	}
}

func TestAreaInsightsKeyStaysInTheHeaderAndOriginIsFixed(t *testing.T) {
	env := newAreaEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{"count":"3"}`)})
	result, err := areaCall(env, `{"insights":["INSIGHT_COUNT"],`+validCircle+`,`+areaTypes1+`}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r := env.fake.recorded()[0]
	if r.header.Get(originalHost) != areaHost || r.header.Get("X-Goog-Api-Key") != keyValue ||
		strings.Contains(r.path+r.rawQuery+r.body+result, keyValue) {
		t.Errorf("request = %+v", r)
	}
	if areaInsightsRoot != "https://areainsights.googleapis.com" {
		t.Errorf("root = %q", areaInsightsRoot)
	}
	if strings.Contains(env.red.Apply("x "+keyValue), keyValue) {
		t.Error("the key is not registered with the redactor")
	}
	metadata, _ := registry(t).ProviderMetadata(Provider)
	if metadata.ValidateBaseURL(areaInsightsRoot) == nil {
		t.Error("a connection may not name the Places Aggregate host as its base URL")
	}
}

func TestAreaInsightsToolRiskAndDescription(t *testing.T) {
	var d capability.Descriptor
	for _, candidate := range registry(t).Provider(Provider) {
		if candidate.ID == areaInsightsComputeID {
			d = candidate
		}
	}
	if d.ID == "" {
		t.Fatal("tool is not registered")
	}
	if d.Risk != readRisk || d.Risk.Effect != capability.EffectRead || d.Risk.Idempotency != capability.IdempotencySafe ||
		d.Risk.Confirmation != capability.ConfirmationNone || !d.Risk.OpenWorld {
		t.Errorf("risk = %+v", d.Risk)
	}
	for _, want := range []string{"SKU", "attribution", "untrusted", "charges above the free usage", "does not verify",
		"quota", "storage rules", "place IDs may be stored without limit", "google.maps.places.get", "not OAuth",
		"more than 100"} {
		if !strings.Contains(d.Description, want) {
			t.Errorf("description lacks %q", want)
		}
	}
	var schema map[string]any
	if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
		t.Fatalf("input schema: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)
	if len(props) != len(d.Arguments) {
		t.Errorf("%d schema properties, %d documented arguments", len(props), len(d.Arguments))
	}
	for _, arg := range d.Arguments {
		if _, ok := props[arg.Name]; !ok {
			t.Errorf("documented argument %s is not in the schema", arg.Name)
		}
	}
	for _, banned := range []string{"key", "api_key", "body", "filter", "path", "url", "header"} {
		if _, ok := props[banned]; ok {
			t.Errorf("tool takes %s", banned)
		}
	}
}
