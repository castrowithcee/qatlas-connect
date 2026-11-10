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
	"github.com/castrowithcee/qatlas-connect/internal/provider/ratelimit"
	"github.com/castrowithcee/qatlas-connect/internal/redact"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
)

const routesTestHost = "routes.googleapis.com"

// routesRewrite sends the Routes host to the test server and refuses every other host.
type routesRewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (r routesRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != routesTestHost || req.URL.Scheme != "https" {
		return nil, errors.New("unexpected host")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set(originalHost, req.URL.Host)
	clone.URL.Scheme, clone.URL.Host, clone.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(clone)
}

func newRoutesEnvironment(t *testing.T, f *fakeGoogle) *environment {
	t.Helper()
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	previous := transport
	transport = routesRewrite{target: target, base: server.Client().Transport}
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
	routeEnds    = `"origin":{"address":"Brandenburger Tor, Berlin"},"destination":{"location":{"latitude":52.5251,"longitude":13.3694}}`
	routeMaskArg = `"field_mask":["routes.duration","routes.distanceMeters"]`
	matrixMask   = `"field_mask":"duration,distanceMeters,condition"`
	routeAnswer  = `{"routes":[{"distanceMeters":3401,"duration":"612s","surprise":1,` +
		`"legs":[{"steps":[{"navigationInstruction":{"maneuver":"TURN_LEFT","instructions":"Turn left onto Unter den Linden"}}]}],` +
		`"polyline":{"encodedPolyline":"_p~iF~ps|U_ulLnnqC_mqNvxq` + "`" + `@"},"routeToken":"token-1"}],` +
		`"geocodingResults":{"origin":{"geocoderStatus":{},"type":["premise"],"placeId":"ChIJiQnyVcZRqEcRY0xnhE77uyY"}}}`
)

func routesBodyOf(t *testing.T, r recorded) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(r.body), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	return body
}

func compact(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestRoutesComputeSendsAddressCoordinateAndPlaceID(t *testing.T) {
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json; charset=UTF-8", routeAnswer)})
	result, err := env.invokeTool(routesComputeID, "read", `{"origin":{"address":"Brandenburger Tor, Berlin"},`+
		`"destination":{"location":{"latitude":52.5251,"longitude":13.3694,"heading":90},"side_of_road":true},`+
		`"intermediates":[{"place_id":"ChIJiQnyVcZRqEcRY0xnhE77uyY","via":true}],`+
		`"field_mask":"routes.duration, routes.legs.steps.navigationInstruction,routes.polyline,routes.routeToken,geocodingResults"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	requests := env.fake.recorded()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	r := requests[0]
	if r.method != http.MethodPost || r.path != "/directions/v2:computeRoutes" || r.rawQuery != "" ||
		r.header.Get(originalHost) != routesTestHost || r.header.Get("X-Goog-Api-Key") != keyValue ||
		r.header.Get("Content-Type") != "application/json" ||
		r.header.Get("X-Goog-FieldMask") != "routes.duration,routes.legs.steps.navigationInstruction,routes.polyline,routes.routeToken,geocodingResults" {
		t.Errorf("request = %+v", r)
	}
	want := `{"destination":{"location":{"heading":90,"latLng":{"latitude":52.5251,"longitude":13.3694}},"sideOfRoad":true},` +
		`"intermediates":[{"placeId":"ChIJiQnyVcZRqEcRY0xnhE77uyY","via":true}],` +
		`"origin":{"address":"Brandenburger Tor, Berlin"}}`
	if got := compact(t, routesBodyOf(t, r)); got != want {
		t.Errorf("body = %s", got)
	}
	for _, wanted := range []string{`"distanceMeters":3401`, `"duration":"612s"`, `"maneuver":"TURN_LEFT"`,
		`"instructions":"Turn left onto Unter den Linden"`, `"encodedPolyline":"_p~iF~ps|U_ulLnnqC_mqNvxq` + "`" + `@"`,
		`"routeToken":"token-1"`, `"placeId":"ChIJiQnyVcZRqEcRY0xnhE77uyY"`, `"type":["premise"]`} {
		if !strings.Contains(result, wanted) {
			t.Errorf("result lacks %s: %s", wanted, result)
		}
	}
	for _, banned := range []string{"surprise", keyValue} {
		if strings.Contains(result, banned) {
			t.Errorf("result contains %q", banned)
		}
	}
}

func TestRoutesComputeSendsEveryDocumentedParameter(t *testing.T) {
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{}`)})
	_, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,"intermediates":[`+
		`{"navigation_point_token":"np-token_1","vehicle_stopover":true},{"address":"Alexanderplatz"}],`+
		`"travel_mode":"DRIVE","routing_preference":"TRAFFIC_AWARE_OPTIMAL","polyline_quality":"HIGH_QUALITY",`+
		`"polyline_encoding":"GEO_JSON_LINESTRING","departure_time":"2026-11-11T08:00:00+01:00",`+
		`"compute_alternative_routes":false,"language_code":"de-DE","region_code":"de","units":"METRIC",`+
		`"optimize_waypoint_order":true,"requested_reference_routes":["FUEL_EFFICIENT"],`+
		`"extra_computations":["TOLLS","FUEL_CONSUMPTION","TRAFFIC_ON_POLYLINE","HTML_FORMATTED_NAVIGATION_INSTRUCTIONS",`+
		`"FLYOVER_INFO_ON_POLYLINE","NARROW_ROAD_INFO_ON_POLYLINE","TUNNEL_INFO_ON_POLYLINE","BRIDGE_INFO_ON_POLYLINE",`+
		`"SKYWAY_INFO_ON_POLYLINE"],"traffic_model":"PESSIMISTIC",`+
		`"route_modifiers":{"avoid_tolls":true,"avoid_highways":false,"avoid_ferries":true,"avoid_indoor":false,`+
		`"avoid_tunnels":true,"toll_passes":["US_CA_FASTRAK","JP_ETC2"],"vehicle_info":{"emission_type":"DIESEL",`+
		`"total_height_mm":3500,"total_length_mm":12000,"total_width_mm":2500,"total_weight_kg":18000,`+
		`"trailer_info":[{"length_mm":7000,"weight_kg":6000}],"hazardous_goods_types":["FLAMMABLE","GASES"]}},`+
		`"field_mask":["routes.optimizedIntermediateWaypointIndex","routes.routeLabels"]}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	want := `{"computeAlternativeRoutes":false,"departureTime":"2026-11-11T08:00:00+01:00",` +
		`"destination":{"location":{"latLng":{"latitude":52.5251,"longitude":13.3694}}},` +
		`"extraComputations":["TOLLS","FUEL_CONSUMPTION","TRAFFIC_ON_POLYLINE","HTML_FORMATTED_NAVIGATION_INSTRUCTIONS",` +
		`"FLYOVER_INFO_ON_POLYLINE","NARROW_ROAD_INFO_ON_POLYLINE","TUNNEL_INFO_ON_POLYLINE","BRIDGE_INFO_ON_POLYLINE",` +
		`"SKYWAY_INFO_ON_POLYLINE"],"intermediates":[{"navigationPointToken":"np-token_1","vehicleStopover":true},` +
		`{"address":"Alexanderplatz"}],"languageCode":"de-DE","optimizeWaypointOrder":true,` +
		`"origin":{"address":"Brandenburger Tor, Berlin"},"polylineEncoding":"GEO_JSON_LINESTRING",` +
		`"polylineQuality":"HIGH_QUALITY","regionCode":"de","requestedReferenceRoutes":["FUEL_EFFICIENT"],` +
		`"routeModifiers":{"avoidFerries":true,"avoidHighways":false,"avoidIndoor":false,"avoidTolls":true,"avoidTunnels":true,` +
		`"tollPasses":["US_CA_FASTRAK","JP_ETC2"],"vehicleInfo":{"emissionType":"DIESEL","hazardousGoodsTypes":["FLAMMABLE","GASES"],` +
		`"totalHeightMm":"3500","totalLengthMm":"12000","totalWeightKg":"18000","totalWidthMm":"2500",` +
		`"trailerInfo":[{"lengthMm":"7000","weightKg":"6000"}]}},"routingPreference":"TRAFFIC_AWARE_OPTIMAL",` +
		`"trafficModel":"PESSIMISTIC","travelMode":"DRIVE","units":"METRIC"}`
	if got := compact(t, routesBodyOf(t, env.fake.recorded()[0])); got != want {
		t.Errorf("body = %s", got)
	}
}

func TestRoutesComputeTransitAndTruck(t *testing.T) {
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{}`)})
	if _, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,`+routeMaskArg+`,"travel_mode":"TRANSIT",`+
		`"arrival_time":"2026-11-11T09:00:00Z","transit_preferences":{"allowed_travel_modes":["BUS","RAIL"],`+
		`"routing_preference":"LESS_WALKING"}}`); err != nil {
		t.Fatalf("transit: %v", err)
	}
	body := compact(t, routesBodyOf(t, env.fake.recorded()[0]))
	if !strings.Contains(body, `"arrivalTime":"2026-11-11T09:00:00Z"`) ||
		!strings.Contains(body, `"transitPreferences":{"allowedTravelModes":["BUS","RAIL"],"routingPreference":"LESS_WALKING"}`) {
		t.Errorf("body = %s", body)
	}
	if _, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,`+routeMaskArg+`,"travel_mode":"TRUCK",`+
		`"route_modifiers":{"vehicle_info":{"total_height_mm":4000,"total_length_mm":16500,"total_width_mm":2550,`+
		`"total_weight_kg":40000}}}`); err != nil {
		t.Fatalf("truck: %v", err)
	}
}

func intermediates(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = `{"location":{"latitude":52.5,"longitude":13.4}}`
	}
	return `"intermediates":[` + strings.Join(items, ",") + `]`
}

func TestRoutesComputeTakesAtMost25Intermediates(t *testing.T) {
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{}`)})
	if _, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,`+routeMaskArg+`,`+intermediates(25)+`}`); err != nil {
		t.Fatalf("25 intermediates: %v", err)
	}
	if got := routesBodyOf(t, env.fake.recorded()[0])["intermediates"].([]any); len(got) != 25 {
		t.Errorf("intermediates sent = %d", len(got))
	}
	env = newRoutesEnvironment(t, &fakeGoogle{})
	if _, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,`+routeMaskArg+`,`+intermediates(26)+`}`); err == nil {
		t.Error("26 intermediates were accepted")
	}
	if len(env.fake.recorded()) != 0 || *env.reads != 0 {
		t.Errorf("26 intermediates: %d requests, %d secret reads", len(env.fake.recorded()), *env.reads)
	}
	var arguments routesComputeArguments
	_ = json.Unmarshal([]byte(`{`+routeEnds+`,`+intermediates(26)+`}`), &arguments)
	if _, err := computeBody(arguments, routeMask26()); err == nil || !strings.Contains(err.Error(), "at most 25") {
		t.Errorf("handler check = %v", err)
	}
}

func routeMask26() routeMask {
	mask, _ := parseRouteMask(json.RawMessage(`"routes.duration"`), false)
	return mask
}

func TestRoutesRefuseBeforeAnyIO(t *testing.T) {
	refusals := map[string]string{
		"no mask":            `{` + routeEnds + `}`,
		"empty mask":         `{` + routeEnds + `,"field_mask":""}`,
		"wildcard":           `{` + routeEnds + `,"field_mask":"*"}`,
		"partial wildcard":   `{` + routeEnds + `,"field_mask":"routes.legs.*"}`,
		"wildcard in list":   `{` + routeEnds + `,"field_mask":["routes.duration","routes.*"]}`,
		"top level routes":   `{` + routeEnds + `,"field_mask":"routes"}`,
		"unknown path":       `{` + routeEnds + `,"field_mask":"routes.nope"}`,
		"geojson inner path": `{` + routeEnds + `,"field_mask":"routes.polyline.geoJsonLinestring.coordinates"}`,
		"status details":     `{` + routeEnds + `,"field_mask":"geocodingResults.origin.geocoderStatus.details"}`,
		"matrix path":        `{` + routeEnds + `,"field_mask":"originIndex"}`,
		"bad travel mode":    `{` + routeEnds + `,` + routeMaskArg + `,"travel_mode":"FLY"}`,
		"departure and arrival": `{` + routeEnds + `,` + routeMaskArg + `,"travel_mode":"TRANSIT",` +
			`"departure_time":"2026-11-11T08:00:00Z","arrival_time":"2026-11-11T09:00:00Z"}`,
		"arrival without transit":   `{` + routeEnds + `,` + routeMaskArg + `,"arrival_time":"2026-11-11T09:00:00Z"}`,
		"bad time":                  `{` + routeEnds + `,` + routeMaskArg + `,"departure_time":"tomorrow at eight"}`,
		"time without zone":         `{` + routeEnds + `,` + routeMaskArg + `,"departure_time":"2026-11-11 08:00:00 local"}`,
		"preference for walking":    `{` + routeEnds + `,` + routeMaskArg + `,"travel_mode":"WALK","routing_preference":"TRAFFIC_AWARE"}`,
		"traffic model unoptimal":   `{` + routeEnds + `,` + routeMaskArg + `,"routing_preference":"TRAFFIC_AWARE","traffic_model":"OPTIMISTIC"}`,
		"transit prefs for driving": `{` + routeEnds + `,` + routeMaskArg + `,"transit_preferences":{"routing_preference":"LESS_WALKING"}}`,
		"via on origin":             `{"origin":{"address":"A","via":true},"destination":{"address":"B"},` + routeMaskArg + `}`,
		"two forms":                 `{"origin":{"address":"A","place_id":"ChIJiQnyVcZRqEcRY0xnhE77uyY"},"destination":{"address":"B"},` + routeMaskArg + `}`,
		"no form":                   `{"origin":{"side_of_road":true},"destination":{"address":"B"},` + routeMaskArg + `}`,
		"no destination":            `{"origin":{"address":"A"},` + routeMaskArg + `}`,
		"heading for walking": `{"origin":{"location":{"latitude":1,"longitude":2,"heading":10}},"destination":{"address":"B"},` +
			routeMaskArg + `,"travel_mode":"WALK"}`,
		"bad latitude":     `{"origin":{"location":{"latitude":91,"longitude":2}},"destination":{"address":"B"},` + routeMaskArg + `}`,
		"bad place id":     `{"origin":{"place_id":"places/x"},"destination":{"address":"B"},` + routeMaskArg + `}`,
		"control in addr":  `{"origin":{"address":"A\u0007"},"destination":{"address":"B"},` + routeMaskArg + `}`,
		"optimize no path": `{` + routeEnds + `,` + routeMaskArg + `,"optimize_waypoint_order":true}`,
		"optimize via": `{` + routeEnds + `,"intermediates":[{"address":"C","via":true}],"optimize_waypoint_order":true,` +
			`"field_mask":"routes.optimizedIntermediateWaypointIndex"}`,
		"shorter for transit": `{` + routeEnds + `,` + routeMaskArg + `,"travel_mode":"TRANSIT","requested_reference_routes":["SHORTER_DISTANCE"]}`,
		"tunnels unoptimal":   `{` + routeEnds + `,` + routeMaskArg + `,"route_modifiers":{"avoid_tunnels":true}}`,
		"trailer unoptimal": `{` + routeEnds + `,` + routeMaskArg + `,"route_modifiers":{"vehicle_info":` +
			`{"trailer_info":[{"length_mm":1}]}}}`,
		"truck without vehicle": `{` + routeEnds + `,` + routeMaskArg + `,"travel_mode":"TRUCK"}`,
		"truck partial vehicle": `{` + routeEnds + `,` + routeMaskArg + `,"travel_mode":"TRUCK","route_modifiers":` +
			`{"vehicle_info":{"total_height_mm":4000}}}`,
		"unknown toll pass":   `{` + routeEnds + `,` + routeMaskArg + `,"route_modifiers":{"toll_passes":["US_MI_IQ_PROX_CARD"]}}`,
		"unspecified extra":   `{` + routeEnds + `,` + routeMaskArg + `,"extra_computations":["EXTRA_COMPUTATION_UNSPECIFIED"]}`,
		"key argument":        `{` + routeEnds + `,` + routeMaskArg + `,"key":"x"}`,
		"region of three":     `{` + routeEnds + `,` + routeMaskArg + `,"region_code":"deu"}`,
		"zero vehicle height": `{` + routeEnds + `,` + routeMaskArg + `,"route_modifiers":{"vehicle_info":{"total_height_mm":0}}}`,
	}
	for name, arguments := range refusals {
		env := newRoutesEnvironment(t, &fakeGoogle{})
		_, err := env.invokeTool(routesComputeID, "read", arguments)
		if err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: %d requests, %d secret reads", name, len(env.fake.recorded()), *env.reads)
		}
	}
}

// The core checks the input schema first; the handlers check the same rules again on their own.
func TestRoutesHandlersCheckArgumentsThemselves(t *testing.T) {
	resolved := &config.Resolved{BaseURL: apiRoot}
	for _, arguments := range []string{
		`{` + routeEnds + `}`, `{` + routeEnds + `,"field_mask":"*"}`, `{` + routeEnds + `,"field_mask":"routes"}`,
		`{` + routeEnds + `,` + routeMaskArg + `,"travel_mode":"FLY"}`, `{` + routeEnds + `,` + routeMaskArg + `,` + intermediates(26) + `}`,
		`{` + routeEnds + `,` + routeMaskArg + `,"travel_mode":"TRANSIT","departure_time":"2026-11-11T08:00:00Z",` +
			`"arrival_time":"2026-11-11T09:00:00Z"}`,
		`{"origin":{"address":"A","via":true},"destination":{"address":"B"},` + routeMaskArg + `}`,
	} {
		_, err := handleRoutesCompute(context.Background(), resolved, nil, nil, json.RawMessage(arguments))
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an invalid request before any secret access", arguments, err)
		}
	}
	for _, arguments := range []string{
		`{"origins":[{"waypoint":{"address":"A"}}],"destinations":[{"waypoint":{"address":"B"}}]}`,
		`{"origins":` + matrixPoints(2) + `,"destinations":` + matrixPoints(313) + `,` + matrixMask + `}`,
		`{"origins":[{"waypoint":{"address":"A","via":true}}],"destinations":[{"waypoint":{"address":"B"}}],` + matrixMask + `}`,
	} {
		_, err := handleRoutesMatrix(context.Background(), resolved, nil, nil, json.RawMessage(arguments))
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an invalid request before any secret access", arguments[:40], err)
		}
	}
}

func TestRoutesFieldMaskRefusalsCarryNoValue(t *testing.T) {
	for _, raw := range []string{`"routes.secretCanaryField"`, `["routes.duration","secretCanaryField.*"]`,
		`"` + strings.Repeat("a", 200) + `"`} {
		_, err := parseRouteMask(json.RawMessage(raw), false)
		if err == nil || strings.Contains(err.Error(), "secretCanaryField") || strings.Contains(err.Error(), "aaaa") {
			t.Errorf("%s: err = %v", raw, err)
		}
	}
	for _, path := range []string{"routes.legs.steps.transitDetails.localizedValues.departureTime.time.text",
		"routes.travelAdvisory.tollInfo.estimatedPrice.units", "routes.polylineDetails.restrictionInfo.trucksProhibited",
		"geocodingResults.intermediates.intermediateWaypointRequestIndex", "fallbackInfo.reason",
		"routes.polyline.geoJsonLinestring", "routes.legs.stepsOverview.multiModalSegments.stepEndIndex"} {
		if !routesMaskPaths[path] {
			t.Errorf("%s is not a known path", path)
		}
	}
	for _, path := range []string{"travelAdvisory.tollInfo.estimatedPrice.currencyCode", "localizedValues.transitFare.text",
		"fallbackInfo.routingMode", "condition", "status.message"} {
		if !matrixMaskPaths[path] {
			t.Errorf("matrix %s is not a known path", path)
		}
	}
}

func matrixPoints(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = `{"waypoint":{"location":{"latitude":52.5,"longitude":13.4}}}`
	}
	return `[` + strings.Join(items, ",") + `]`
}

func matrixElements(origins, destinations int) string {
	var items []string
	for o := 0; o < origins; o++ {
		for d := 0; d < destinations; d++ {
			items = append(items, `{"originIndex":`+itoa(o)+`,"destinationIndex":`+itoa(d)+`,"status":{},`+
				`"condition":"ROUTE_EXISTS","distanceMeters":1200,"duration":"300s"}`)
		}
	}
	return `[` + strings.Join(items, ",") + `]`
}

func TestRoutesMatrixTakesAtMost625Elements(t *testing.T) {
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", matrixElements(25, 25))})
	result, err := env.invokeTool(routesMatrixID, "read", `{"origins":`+matrixPoints(25)+`,"destinations":`+
		matrixPoints(25)+`,`+matrixMask+`}`)
	if err != nil {
		t.Fatalf("625 elements: %v", err)
	}
	var out struct {
		Elements []map[string]any `json:"elements"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Elements) != 625 {
		t.Fatalf("elements = %d, %v", len(out.Elements), err)
	}
	r := env.fake.recorded()[0]
	if r.path != "/distanceMatrix/v2:computeRouteMatrix" ||
		r.header.Get("X-Goog-FieldMask") != "duration,distanceMeters,condition,originIndex,destinationIndex,status" {
		t.Errorf("request = %+v", r)
	}
	for name, shape := range map[string][2]int{"626 origins": {626, 1}, "2 by 313": {2, 313}, "1 by 626": {1, 626}} {
		env := newRoutesEnvironment(t, &fakeGoogle{})
		_, err := env.invokeTool(routesMatrixID, "read", `{"origins":`+matrixPoints(shape[0])+`,"destinations":`+
			matrixPoints(shape[1])+`,`+matrixMask+`}`)
		if err == nil || len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: err = %v, %d requests, %d secret reads", name, err, len(env.fake.recorded()), *env.reads)
		}
	}
}

func TestRoutesMatrixFurtherDocumentedLimits(t *testing.T) {
	addresses := func(n int) string {
		items := make([]string, n)
		for i := range items {
			items[i] = `{"waypoint":{"address":"Street ` + itoa(i) + `"}}`
		}
		return `[` + strings.Join(items, ",") + `]`
	}
	for name, arguments := range map[string]string{
		"optimal over 100": `{"origins":` + matrixPoints(101) + `,"destinations":` + matrixPoints(1) + `,` + matrixMask +
			`,"routing_preference":"TRAFFIC_AWARE_OPTIMAL"}`,
		"transit over 100": `{"origins":` + matrixPoints(11) + `,"destinations":` + matrixPoints(10) + `,` + matrixMask +
			`,"travel_mode":"TRANSIT"}`,
		"51 named":       `{"origins":` + addresses(50) + `,"destinations":` + addresses(1) + `,` + matrixMask + `}`,
		"tunnels":        `{"origins":[{"waypoint":{"address":"A"},"route_modifiers":{"avoid_tunnels":true}}],"destinations":[{"waypoint":{"address":"B"}}],` + matrixMask + `}`,
		"drive vehicle":  `{"origins":[{"waypoint":{"address":"A"},"route_modifiers":{"vehicle_info":{"total_height_mm":3000}}}],"destinations":[{"waypoint":{"address":"B"}}],` + matrixMask + `}`,
		"route extra":    `{"origins":` + matrixPoints(1) + `,"destinations":` + matrixPoints(1) + `,` + matrixMask + `,"extra_computations":["FUEL_CONSUMPTION"]}`,
		"via":            `{"origins":[{"waypoint":{"address":"A","via":true}}],"destinations":[{"waypoint":{"address":"B"}}],` + matrixMask + `}`,
		"route mask":     `{"origins":` + matrixPoints(1) + `,"destinations":` + matrixPoints(1) + `,"field_mask":"routes.duration"}`,
		"no mask":        `{"origins":` + matrixPoints(1) + `,"destinations":` + matrixPoints(1) + `}`,
		"wildcard":       `{"origins":` + matrixPoints(1) + `,"destinations":` + matrixPoints(1) + `,"field_mask":"*"}`,
		"no destination": `{"origins":` + matrixPoints(1) + `,"destinations":[],` + matrixMask + `}`,
	} {
		env := newRoutesEnvironment(t, &fakeGoogle{})
		_, err := env.invokeTool(routesMatrixID, "read", arguments)
		if err == nil || len(env.fake.recorded()) != 0 || *env.reads != 0 {
			t.Errorf("%s: err = %v, %d requests, %d secret reads", name, err, len(env.fake.recorded()), *env.reads)
		}
	}
	allowed := map[string]string{
		"optimal 100": `{"origins":` + matrixPoints(10) + `,"destinations":` + matrixPoints(10) + `,` + matrixMask +
			`,"routing_preference":"TRAFFIC_AWARE_OPTIMAL"}`,
		"50 named": `{"origins":` + addresses(25) + `,"destinations":` + addresses(25) + `,` + matrixMask + `}`,
	}
	for name, arguments := range allowed {
		var a routesMatrixArguments
		if err := json.Unmarshal([]byte(arguments), &a); err != nil {
			t.Fatal(err)
		}
		if _, _, err := matrixBody(a); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRoutesMatrixSendsEveryDocumentedParameter(t *testing.T) {
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", matrixElements(1, 2))})
	_, err := env.invokeTool(routesMatrixID, "read", `{"origins":[{"waypoint":{"place_id":"ChIJiQnyVcZRqEcRY0xnhE77uyY",`+
		`"vehicle_stopover":true},"route_modifiers":{"avoid_tolls":true,"avoid_highways":true,"avoid_ferries":false,`+
		`"avoid_indoor":true,"toll_passes":["IN_FASTAG"],"vehicle_info":{"emission_type":"ELECTRIC"}}}],`+
		`"destinations":[{"waypoint":{"address":"Alexanderplatz"}},{"waypoint":{"navigation_point_token":"tok"}}],`+
		`"travel_mode":"DRIVE","routing_preference":"TRAFFIC_AWARE_OPTIMAL","departure_time":"2026-11-11T08:00:00Z",`+
		`"language_code":"en","region_code":"us","units":"IMPERIAL","extra_computations":["TOLLS"],`+
		`"traffic_model":"BEST_GUESS",`+matrixMask+`}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	want := `{"departureTime":"2026-11-11T08:00:00Z","destinations":[{"waypoint":{"address":"Alexanderplatz"}},` +
		`{"waypoint":{"navigationPointToken":"tok"}}],"extraComputations":["TOLLS"],"languageCode":"en",` +
		`"origins":[{"routeModifiers":{"avoidFerries":false,"avoidHighways":true,"avoidIndoor":true,"avoidTolls":true,` +
		`"tollPasses":["IN_FASTAG"],"vehicleInfo":{"emissionType":"ELECTRIC"}},"waypoint":{"placeId":` +
		`"ChIJiQnyVcZRqEcRY0xnhE77uyY","vehicleStopover":true}}],"regionCode":"us","routingPreference":"TRAFFIC_AWARE_OPTIMAL",` +
		`"trafficModel":"BEST_GUESS","travelMode":"DRIVE","units":"IMPERIAL"}`
	if got := compact(t, routesBodyOf(t, env.fake.recorded()[0])); got != want {
		t.Errorf("body = %s", got)
	}
	env = newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", matrixElements(1, 1))})
	if _, err := env.invokeTool(routesMatrixID, "read", `{"origins":[{"waypoint":{"address":"A"},"route_modifiers":`+
		`{"vehicle_info":{"total_height_mm":4000,"total_length_mm":16500,"total_width_mm":2550,"total_weight_kg":40000,`+
		`"hazardous_goods_types":["OTHER"]}}}],"destinations":[{"waypoint":{"address":"B"}}],"travel_mode":"TRUCK",`+
		matrixMask+`}`); err != nil {
		t.Errorf("truck matrix: %v", err)
	}
}

func TestRoutesMatrixReportsElementErrorsPerElement(t *testing.T) {
	answer := `[{"destinationIndex":1,"status":{"code":5,"message":"Origin not found ` + detailCanary + `",` +
		`"details":[{"@type":"x","echo":"` + detailCanary + `"}]},"condition":"ROUTE_MATRIX_ELEMENT_CONDITION_UNSPECIFIED"},` +
		`{"originIndex":0,"destinationIndex":0,"status":{},"condition":"ROUTE_EXISTS","distanceMeters":822,"duration":"160s",` +
		`"localizedValues":{"distance":{"text":"0.8 km"}},"surprise":true}]`
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", answer)})
	result, err := env.invokeTool(routesMatrixID, "read", `{"origins":`+matrixPoints(1)+`,"destinations":`+
		matrixPoints(2)+`,`+matrixMask+`}`)
	if err != nil {
		t.Fatalf("a failed element failed the call: %v", err)
	}
	var out struct {
		Elements []map[string]any `json:"elements"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Elements) != 2 {
		t.Fatalf("result = %s", result)
	}
	failed, ok := out.Elements[0]["status"].(map[string]any)
	if !ok || failed["code"] != float64(5) || out.Elements[0]["originIndex"] != float64(0) ||
		out.Elements[0]["destinationIndex"] != float64(1) {
		t.Errorf("failed element = %v", out.Elements[0])
	}
	if _, present := out.Elements[1]["status"]; present || out.Elements[1]["distanceMeters"] != float64(822) {
		t.Errorf("successful element = %v", out.Elements[1])
	}
	if strings.Contains(result, "surprise") || strings.Contains(result, `"details"`) || strings.Contains(result, "echo") {
		t.Errorf("undocumented members kept: %s", result)
	}
}

func TestRoutesMatrixUnusableStreams(t *testing.T) {
	full := matrixElements(1, 2)
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"cut stream":    reply(200, "application/json", full[:len(full)-20]),
		"unclosed":      reply(200, "application/json", strings.TrimSuffix(full, "]")),
		"too few":       reply(200, "application/json", matrixElements(1, 1)),
		"too many":      reply(200, "application/json", matrixElements(2, 2)),
		"object":        reply(200, "application/json", `{"elements":[]}`),
		"null":          reply(200, "application/json", `null`),
		"not an object": reply(200, "application/json", `[1,2]`),
		"not json":      reply(200, "text/html", `<html>`),
		"oversized":     reply(200, "application/json", `[`+strings.Repeat(` `, maxResponseBytes)+`]`),
	} {
		env := newRoutesEnvironment(t, &fakeGoogle{answer: answer})
		_, err := env.invokeTool(routesMatrixID, "read", `{"origins":`+matrixPoints(1)+`,"destinations":`+
			matrixPoints(2)+`,`+matrixMask+`}`)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestRoutesErrorFormsKeepProviderTextApart(t *testing.T) {
	cases := []struct {
		status        int
		state, reason string
		class         provider.Class
		want          string
	}{
		{403, "PERMISSION_DENIED", "API_KEY_SERVICE_BLOCKED", provider.ClassPermission, "Routes API"},
		{404, "NOT_FOUND", "", provider.ClassNotFound, "route request"},
		{400, "INVALID_ARGUMENT", "", provider.ClassInvalidResponse, "route request as invalid"},
		{429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED", provider.ClassRateLimited, "rate-limited"},
		{401, "UNAUTHENTICATED", "", provider.ClassAuth, "credential"},
	}
	for _, tool := range []struct{ id, arguments string }{
		{routesComputeID, `{` + routeEnds + `,` + routeMaskArg + `}`},
		{routesMatrixID, `{"origins":` + matrixPoints(1) + `,"destinations":` + matrixPoints(1) + `,` + matrixMask + `}`},
	} {
		for _, tc := range cases {
			env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(tc.status, "application/json",
				errorObject(tc.status, tc.state, tc.reason))})
			_, err := env.invokeTool(tool.id, "read", tool.arguments)
			var failed *provider.Error
			if !errors.As(err, &failed) || failed.Class != tc.class {
				t.Errorf("%s %d: err = %v", tool.id, tc.status, err)
				continue
			}
			if strings.Contains(failed.Message, detailCanary) || strings.Contains(err.Error(), keyValue) ||
				strings.Contains(failed.Message, "Places") || !strings.Contains(failed.Message, tc.want) {
				t.Errorf("%s %d: message = %q", tool.id, tc.status, failed.Message)
			}
			if failed.Original == nil || failed.Original.Status != tc.status ||
				!strings.Contains(compact(t, failed.Original), detailCanary) {
				t.Errorf("%s %d: original = %+v", tool.id, tc.status, failed.Original)
			}
			if len(env.fake.recorded()) != 1 {
				t.Errorf("%s %d: %d requests, want exactly one", tool.id, tc.status, len(env.fake.recorded()))
			}
		}
	}
}

func TestRoutesCutTextsListsAndPolylines(t *testing.T) {
	long := strings.Repeat("x", 2000)
	steps := strings.TrimSuffix(strings.Repeat(`{"distanceMeters":1},`, maxSteps+5), ",")
	big := strings.Repeat("?", maxPolylineBytes+1)
	points := strings.TrimSuffix(strings.Repeat(`[13.4,52.5],`, maxGeoJSONPoints+1), ",")
	answer := `{"routes":[` +
		`{"description":"` + long + `","legs":[{"steps":[{"navigationInstruction":{"instructions":"` + long + `"}},` + steps + `]}],` +
		`"polyline":{"encodedPolyline":"` + big + `"}},` +
		`{"polyline":{"encodedPolyline":"abc def"}},` +
		`{"polyline":{"geoJsonLinestring":{"type":"LineString","coordinates":[` + points + `]}}},` +
		`{"polyline":{"geoJsonLinestring":{"type":"LineString","coordinates":[[13.4,52.5],[13.5,"x"]]}}},` +
		`{"polyline":{"geoJsonLinestring":{"type":"LineString","coordinates":[[13.4,52.5],[13.5,52.6]]}}}]}`
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", answer)})
	result, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,"field_mask":"routes.description,`+
		`routes.legs.steps,routes.polyline"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var out struct {
		Routes []struct {
			Description string `json:"description"`
			Legs        []struct {
				Steps []any `json:"steps"`
			} `json:"legs"`
			Polyline map[string]any `json:"polyline"`
		} `json:"routes"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Routes) != 5 {
		t.Fatalf("result: %v", err)
	}
	if len(out.Routes[0].Legs[0].Steps) != maxSteps || strings.Contains(result, long) ||
		!strings.HasSuffix(out.Routes[0].Description, "…") {
		t.Errorf("not cut: %d steps", len(out.Routes[0].Legs[0].Steps))
	}
	for i := 0; i < 4; i++ {
		if len(out.Routes[i].Polyline) != 0 {
			t.Errorf("route %d keeps an unusable polyline", i)
		}
	}
	if line, _ := out.Routes[4].Polyline["geoJsonLinestring"].(map[string]any); line == nil || line["type"] != "LineString" {
		t.Errorf("a valid GeoJSON line was dropped: %v", out.Routes[4].Polyline)
	}
}

func TestRoutesEmptyAnswerIsNoRoute(t *testing.T) {
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", `{}`)})
	result, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,`+routeMaskArg+`}`)
	if err != nil || strings.Contains(result, "routes\":[{") {
		t.Errorf("result = %s, err = %v", result, err)
	}
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"array":    reply(200, "application/json", `[]`),
		"cut":      reply(200, "application/json", `{"routes":[{"duration":"1s"`),
		"not json": reply(200, "text/plain", `x`),
	} {
		env := newRoutesEnvironment(t, &fakeGoogle{answer: answer})
		if _, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,`+routeMaskArg+`}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestRoutesToolsRiskAndDescriptions(t *testing.T) {
	reg := registry(t)
	seen := 0
	for _, d := range reg.Provider(Provider) {
		if !strings.HasPrefix(d.ID, Provider+".routes.") {
			continue
		}
		seen++
		if d.Risk != routesRisk || d.Risk.Effect != capability.EffectRead || d.Risk.DataSensitivity != "google-maps-movement-data" {
			t.Errorf("%s risk = %+v", d.ID, d.Risk)
		}
		for _, want := range []string{"SKU", "Essentials", "Pro", "Enterprise", "TRAFFIC_AWARE", "charges above the free usage",
			"attribution", "storage rules", "service terms", "untrusted", "sent to Google", "field_mask is required",
			"no wildcard", "beta"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s description lacks %q", d.ID, want)
			}
		}
		var schema map[string]any
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Errorf("%s input schema: %v", d.ID, err)
		}
		props, _ := schema["properties"].(map[string]any)
		if len(props) != len(d.Arguments) {
			t.Errorf("%s: %d schema properties, %d documented arguments", d.ID, len(props), len(d.Arguments))
		}
		for _, banned := range []string{"key", "api_key", "url", "host"} {
			if _, ok := props[banned]; ok {
				t.Errorf("%s takes %s", d.ID, banned)
			}
		}
		for _, example := range d.Examples {
			if err := application.ValidateJSON(d.InputSchema, example.Arguments); err != nil {
				t.Errorf("%s example: %v", d.ID, err)
			}
		}
	}
	if seen != 2 {
		t.Errorf("route tools = %d", seen)
	}
}

func TestRoutesOriginIsFixedAndTheKeyStaysInTheHeader(t *testing.T) {
	env := newRoutesEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", routeAnswer)})
	result, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,`+routeMaskArg+`}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r := env.fake.recorded()[0]
	if r.header.Get(originalHost) != routesTestHost || r.header.Get("X-Goog-Api-Key") != keyValue ||
		strings.Contains(r.path+r.rawQuery+r.body+result, keyValue) {
		t.Errorf("request = %+v", r)
	}
	if routesRoot != "https://routes.googleapis.com" {
		t.Errorf("root = %q", routesRoot)
	}
	metadata, _ := registry(t).ProviderMetadata(Provider)
	if metadata.ValidateBaseURL("https://routes.googleapis.com") == nil {
		t.Error("a connection may not name the Routes host as its base URL")
	}
	if _, err := env.invokeTool(routesComputeID, "bound", `{`+routeEnds+`,`+routeMaskArg+`}`); err == nil ||
		len(env.fake.recorded()) != 1 {
		t.Errorf("a connection with a target was used: %v", err)
	}
	if got := env.red.Apply(keyValue); strings.Contains(got, keyValue) {
		t.Errorf("redactor leaves the key: %q", got)
	}
}

func TestRoutesRateLimitHoldsTheKey(t *testing.T) {
	var waits []time.Duration
	limiter := ratelimit.New(0, time.Now, func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil })
	env := newRoutesEnvironment(t, &fakeGoogle{answer: func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		reply(429, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"))(w, r)
	}})
	t.Cleanup(limiters.Replace(keyValue, limiter))
	if _, err := env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,`+routeMaskArg+`}`); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("err = %v", err)
	}
	if len(env.fake.recorded()) != 1 {
		t.Errorf("requests = %d, the failed request was repeated", len(env.fake.recorded()))
	}
	_, _ = env.invokeTool(routesComputeID, "read", `{`+routeEnds+`,`+routeMaskArg+`}`)
	held := false
	for _, wait := range waits {
		held = held || wait >= 6*time.Second
	}
	if !held {
		t.Errorf("waits = %v, want the Retry-After hold", waits)
	}
}
