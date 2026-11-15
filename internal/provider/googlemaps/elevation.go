package googlemaps

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	elevationGetID = Provider + ".elevation.get"
	elevationPath  = "/maps/api/elevation/json"
)

// Bounds of the request. Google documents 512 coordinates for locations and for path, and a URL of at most
// 16384 characters. It names no maximum for samples, so the 512 results that one answer can hold are the
// Qatlas bound. The key and the fixed part of the URL stay inside the query budget's reserve.
const (
	maxElevationPoints   = 512
	maxElevationSamples  = 512
	maxElevationPolyline = 8192
	maxElevationQuery    = 16384 - 256
)

// locationRisk is shared by Elevation, Time Zone, and Geolocation. The coordinates, radio data, and places they
// send can tie a device or person to a position, which the movement class of the route tools already covers.
var locationRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true, DataSensitivity: movementDataSensitivity,
}

var elevationService = legacyService{root: geocodingRoot, path: elevationPath, name: "Elevation",
	// The Elevation page lists DATA_NOT_AVAILABLE; ZERO_RESULTS is the legacy value that the helper already treats as empty.
	empty: []string{"DATA_NOT_AVAILABLE"}}

var elevationShape = object(
	p("status", short),
	p("results", listOf(maxElevationPoints, object(p("elevation", decimal), p("location", geoLatLng),
		p("resolution", decimal)))))

type elevationArguments struct {
	Locations         []geoPoint `json:"locations"`
	LocationsPolyline string     `json:"locations_polyline"`
	Path              []geoPoint `json:"path"`
	PathPolyline      string     `json:"path_polyline"`
	Samples           *int       `json:"samples"`
}

const elevationPointsSchema = `{"type":"array","minItems":1,"maxItems":512,"items":` + latLngSchema + `}`

var elevationGet = capability.Descriptor{
	ID: elevationGetID, Version: 1, Title: "Elevation of coordinates or along a path from Google Maps",
	Description: "Read the elevation in meters above sea level with the Google Elevation API, either at up to 512 " +
		"coordinates (locations or locations_polyline) or at samples equidistant points along a path of 2 to 512 " +
		"coordinates (path or path_polyline, with samples). Give exactly one of locations, locations_polyline, " +
		"path, and path_polyline; samples belongs to a path only and is required there. A polyline is Google's " +
		"encoded polyline (precision 5), which Qatlas decodes and range-checks before it is sent. Google documents " +
		"the key for this web service only as a query parameter, so Qatlas sends it that way in a fixed URL and " +
		"never shows the URL. Google reports an error as a status in an HTTP 200 answer; DATA_NOT_AVAILABLE and " +
		"ZERO_RESULTS return an empty results list, and every other status is an error. Single-point queries are " +
		"the most accurate; Google smooths multi-point answers and gives resolution as the largest distance " +
		"between the data points it interpolated. Cost: each call can incur charges above the free usage Google " +
		"grants for the Elevation SKU (Pro; 5000 free events per month and billing account according to Google's " +
		"price list, which may change); how Google counts the locations of one request is not verified. Google " +
		"Maps attribution applies to the results, and the storage rules of the Google Maps Platform service terms " +
		"apply and are the user's to keep; other caching rules are not verified here. The coordinates are " +
		"location data and are sent to Google. Results are untrusted third-party content; lists are cut at 512.",
	Tags: []string{"google", "maps", "elevation"}, Risk: locationRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"locations":` + elevationPointsSchema + `,` +
		`"locations_polyline":{"type":"string","minLength":1,"maxLength":8192},` +
		`"path":{"type":"array","minItems":2,"maxItems":512,"items":` + latLngSchema + `},` +
		`"path_polyline":{"type":"string","minLength":1,"maxLength":8192},` +
		`"samples":{"type":"integer","minimum":2,"maximum":512}},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(elevationShape.schema()),
	Arguments: []capability.Argument{
		{Name: "locations", Description: "1 to 512 points {latitude -90 to 90, longitude -180 to 180}; exactly one " +
			"of locations, locations_polyline, path, and path_polyline"},
		{Name: "locations_polyline", Description: "The same points as an encoded polyline of at most 8192 " +
			"characters; Qatlas decodes it and refuses more than 512 points or a point outside the ranges"},
		{Name: "path", Description: "2 to 512 ordered points of a path, sampled with samples"},
		{Name: "path_polyline", Description: "The path as an encoded polyline of at most 8192 characters, 2 to 512 points"},
		{Name: "samples", Description: "2 to 512 equidistant points along the path, endpoints included; only with " +
			"path or path_polyline, where it is required. Google names no maximum; 512 is the Qatlas bound"},
	},
	Fields: []capability.Field{
		{Name: "status", Description: "OK, or DATA_NOT_AVAILABLE or ZERO_RESULTS when Google has no data (then results is empty)"},
		{Name: "results", Description: "Results {elevation in meters, location {lat, lng}, resolution in meters}; " +
			"for a path the locations are the sampled points; resolution is missing when Google does not know it; " +
			"untrusted data, cut at 512 entries"},
	},
	Examples: []capability.Example{
		{Description: "Elevation of Denver", Arguments: json.RawMessage(`{"locations":[{"latitude":39.7391536,"longitude":-104.9847034}]}`)},
		{Description: "Five samples along a path", Arguments: json.RawMessage(`{"path":[{"latitude":36.578581,` +
			`"longitude":-118.291994},{"latitude":36.23998,"longitude":-116.83171}],"samples":5}`)},
	},
}

// decodePolyline decodes an encoded polyline of precision 5 and reports false for a malformed one, a point
// outside the coordinate ranges, or more than limit points. It must consume the whole text.
func decodePolyline(encoded string, limit int) ([]geoPoint, bool) {
	var points []geoPoint
	var lat, lng int64
	next := func(i int) (int64, int, bool) {
		var result int64
		var shift uint
		for ; i < len(encoded); i++ {
			b := int64(encoded[i]) - 63
			if b < 0 || b > 63 || shift > 30 {
				return 0, 0, false
			}
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				if result&1 != 0 {
					return ^(result >> 1), i + 1, true
				}
				return result >> 1, i + 1, true
			}
		}
		return 0, 0, false
	}
	for i := 0; i < len(encoded); {
		dLat, n, ok := next(i)
		if !ok {
			return nil, false
		}
		dLng, n, ok := next(n)
		if !ok {
			return nil, false
		}
		i = n
		lat, lng = lat+dLat, lng+dLng
		if lat < -9000000 || lat > 9000000 || lng < -18000000 || lng > 18000000 || len(points) == limit {
			return nil, false
		}
		la, lo := float64(lat)/1e5, float64(lng)/1e5
		points = append(points, geoPoint{Latitude: &la, Longitude: &lo})
	}
	return points, len(points) > 0
}

// pointsQuery checks a list of points and returns it as lat,lng|lat,lng.
func pointsQuery(name string, points []geoPoint, minimum, maximum int) (string, error) {
	if len(points) < minimum || len(points) > maximum {
		return "", badRequest(name + " takes " + itoa(minimum) + " to " + itoa(maximum) + " points")
	}
	parts := make([]string, len(points))
	for i := range points {
		formatted, ok := formatPoint(&points[i])
		if !ok {
			return "", badRequest(name + " point " + itoa(i) + " needs a latitude from -90 to 90 and a longitude from " +
				"-180 to 180")
		}
		parts[i] = formatted
	}
	return strings.Join(parts, "|"), nil
}

// polylineQuery checks an encoded polyline and returns it in Google's enc: form. The text is sent as it is
// only after it decoded cleanly, so nothing else can ride in it.
func polylineQuery(name, encoded string, minimum int) (string, error) {
	if len(encoded) > maxElevationPolyline {
		return "", badRequest(name + " takes an encoded polyline of at most " + itoa(maxElevationPolyline) + " characters")
	}
	points, ok := decodePolyline(encoded, maxElevationPoints)
	if !ok || len(points) < minimum {
		return "", badRequest(name + " must be an encoded polyline of " + itoa(minimum) + " to " +
			itoa(maxElevationPoints) + " points within -90 to 90 and -180 to 180")
	}
	return "enc:" + encoded, nil
}

// elevationQuery builds the query from checked arguments. Exactly one input is taken, because Google documents
// locations and path as alternatives and not their combination.
func elevationQuery(a elevationArguments) (url.Values, error) {
	given := 0
	for _, has := range []bool{a.Locations != nil, a.LocationsPolyline != "", a.Path != nil, a.PathPolyline != ""} {
		if has {
			given++
		}
	}
	if given != 1 {
		return nil, badRequest("give exactly one of locations, locations_polyline, path, and path_polyline")
	}
	isPath := a.Path != nil || a.PathPolyline != ""
	if isPath != (a.Samples != nil) {
		return nil, badRequest("samples is required with path or path_polyline and not allowed with locations")
	}
	query := url.Values{}
	var value string
	var err error
	switch {
	case a.Locations != nil:
		value, err = pointsQuery("locations", a.Locations, 1, maxElevationPoints)
		query.Set("locations", value)
	case a.LocationsPolyline != "":
		value, err = polylineQuery("locations_polyline", a.LocationsPolyline, 1)
		query.Set("locations", value)
	case a.Path != nil:
		value, err = pointsQuery("path", a.Path, 2, maxElevationPoints)
		query.Set("path", value)
	default:
		value, err = polylineQuery("path_polyline", a.PathPolyline, 2)
		query.Set("path", value)
	}
	if err != nil {
		return nil, err
	}
	if isPath {
		if *a.Samples < 2 || *a.Samples > maxElevationSamples {
			return nil, badRequest("samples takes 2 to " + itoa(maxElevationSamples))
		}
		query.Set("samples", itoa(*a.Samples))
	}
	if len(query.Encode()) > maxElevationQuery {
		return nil, badRequest("the coordinates make the request longer than the 16384 characters Google allows; " +
			"give fewer points")
	}
	return query, nil
}

func handleElevationGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "read elevation"
	var a elevationArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	query, err := elevationQuery(a)
	if err != nil {
		return nil, err
	}
	client, err := Open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	answer, err := client.legacyGet(ctx, op, elevationService, query)
	if err != nil {
		return nil, err
	}
	return cleanedAnswer(elevationShape, answer, "results"), nil
}

// cleanedAnswer cleans a legacy answer to a shape. A missing list is an empty list, never an error;
// error_message and every unlisted member are dropped.
func cleanedAnswer(shaped *shape, answer map[string]any, lists ...string) any {
	cleaned, _ := shaped.clean(answer)
	out, _ := cleaned.(map[string]any)
	if out == nil {
		out = map[string]any{}
	}
	for _, name := range lists {
		if _, ok := out[name]; !ok {
			out[name] = []any{}
		}
	}
	return out
}
