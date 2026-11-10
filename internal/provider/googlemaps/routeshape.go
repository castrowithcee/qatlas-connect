package googlemaps

import (
	"encoding/json"
	"strings"
)

// Bounds of a route answer. Google documents none of them except the 25 intermediates and the 625 elements of a
// matrix; the others are Qatlas bounds that no ordinary route reaches.
const (
	maxRoutes          = 10
	maxLegs            = maxIntermediates + 1
	maxSteps           = 1000
	maxSegments        = 200
	maxSpeedIntervals  = 2000
	maxDetailIntervals = 1000
	// A polyline is kept whole or dropped: a cut line would draw a wrong route. The bounds keep one line usable
	// for a client; the whole answer is bounded by maxResponseBytes anyway.
	maxPolylineBytes  = 256 << 10
	maxGeoJSONPoints  = 20000
	maxRouteTokenSize = 8192
)

// Shapes of the answers, following the reference of Routes API v2. Members that Google does not document are
// dropped. The details of a Status are left out: they are objects of any type that no shape can bound.
var (
	rtLatLng    = object(p("latitude", decimal), p("longitude", decimal))
	rtLocation  = object(p("latLng", rtLatLng), p("heading", integer))
	rtLocalized = object(p("text", short), p("languageCode", short))
	rtMoney     = object(p("currencyCode", short), p("units", short), p("nanos", integer))
	rtStatus    = object(p("code", integer), p("message", text))
	rtGeoLine   = wholeObject(p("type", short), p("coordinates", wholeList(maxGeoJSONPoints, wholeList(3, decimal))))
	rtPolyline  = object(p("encodedPolyline", encoded(maxPolylineBytes)), p("geoJsonLinestring", rtGeoLine))
	rtSpeed     = object(p("startPolylinePointIndex", integer), p("endPolylinePointIndex", integer), p("speed", short))
	rtSpeeds    = listOf(maxSpeedIntervals, rtSpeed)
	rtToll      = object(p("estimatedPrice", listOf(10, rtMoney)))
	rtFallback  = object(p("routingMode", short), p("reason", short))
	rtViewport  = object(p("low", rtLatLng), p("high", rtLatLng))
	rtNavigate  = object(p("maneuver", short), p("instructions", text))

	rtAdvisory = object(p("tollInfo", rtToll), p("speedReadingIntervals", rtSpeeds),
		p("fuelConsumptionMicroliters", short), p("routeRestrictionsPartiallyIgnored", boolean),
		p("transitFare", rtMoney))
	rtValues = object(p("distance", rtLocalized), p("duration", rtLocalized), p("staticDuration", rtLocalized),
		p("transitFare", rtLocalized))

	rtTime    = object(p("time", rtLocalized), p("timeZone", short))
	rtStop    = object(p("name", title), p("location", rtLocation))
	rtAgency  = object(p("name", title), p("phoneNumber", short), p("uri", addr))
	rtVehicle = object(p("name", rtLocalized), p("type", short), p("iconUri", addr), p("localIconUri", addr))
	rtLine    = object(p("agencies", listOf(10, rtAgency)), p("name", title), p("uri", addr), p("color", short),
		p("iconUri", addr), p("nameShort", short), p("textColor", short), p("vehicle", rtVehicle))
	rtTransit = object(
		p("stopDetails", object(p("arrivalStop", rtStop), p("arrivalTime", short), p("departureStop", rtStop),
			p("departureTime", short))),
		p("localizedValues", object(p("arrivalTime", rtTime), p("departureTime", rtTime))),
		p("headsign", title), p("headway", short), p("transitLine", rtLine), p("stopCount", integer),
		p("tripShortText", short))

	rtStep = object(p("distanceMeters", integer), p("staticDuration", short), p("polyline", rtPolyline),
		p("startLocation", rtLocation), p("endLocation", rtLocation), p("navigationInstruction", rtNavigate),
		p("travelAdvisory", object(p("speedReadingIntervals", rtSpeeds))),
		p("localizedValues", object(p("distance", rtLocalized), p("staticDuration", rtLocalized))),
		p("transitDetails", rtTransit), p("travelMode", short))
	rtSegment = object(p("navigationInstruction", rtNavigate), p("travelMode", short), p("stepStartIndex", integer),
		p("stepEndIndex", integer))
	rtLeg = object(p("distanceMeters", integer), p("duration", short), p("staticDuration", short),
		p("polyline", rtPolyline), p("startLocation", rtLocation), p("endLocation", rtLocation),
		p("steps", listOf(maxSteps, rtStep)),
		p("travelAdvisory", object(p("tollInfo", rtToll), p("speedReadingIntervals", rtSpeeds))),
		p("localizedValues", object(p("distance", rtLocalized), p("duration", rtLocalized),
			p("staticDuration", rtLocalized))),
		p("stepsOverview", object(p("multiModalSegments", listOf(maxSegments, rtSegment)))))

	rtIndex      = object(p("startIndex", integer), p("endIndex", integer))
	rtFeature    = func(presence string) *shape { return object(p(presence, short), p("polylinePointIndex", rtIndex)) }
	rtFeatures   = func(presence string) *shape { return listOf(maxDetailIntervals, rtFeature(presence)) }
	rtRestricted = object(p("restrictionPresence", short), p("polylinePointIndex", rtIndex),
		p("restrictedHeightMm", short), p("restrictedWeightKg", short), p("hazardousGoods", boolean),
		p("trucksProhibited", boolean))
	rtDetails = object(p("tunnelInfo", rtFeatures("tunnelPresence")),
		p("restrictionInfo", listOf(maxDetailIntervals, rtRestricted)),
		p("flyoverInfo", rtFeatures("flyoverPresence")), p("narrowRoadInfo", rtFeatures("narrowRoadPresence")),
		p("bridgeInfo", rtFeatures("bridgePresence")), p("skywayInfo", rtFeatures("skywayPresence")))

	rtRoute = object(p("routeLabels", listOf(10, short)), p("legs", listOf(maxLegs, rtLeg)),
		p("distanceMeters", integer), p("duration", short), p("staticDuration", short), p("polyline", rtPolyline),
		p("description", title), p("warnings", list(text)), p("viewport", rtViewport), p("travelAdvisory", rtAdvisory),
		p("optimizedIntermediateWaypointIndex", listOf(maxIntermediates, integer)), p("localizedValues", rtValues),
		p("routeToken", keepWhole(maxRouteTokenSize)), p("polylineDetails", rtDetails))
	rtGeocoded = object(p("geocoderStatus", rtStatus), p("type", list(short)), p("partialMatch", boolean),
		p("placeId", keepWhole(256)), p("intermediateWaypointRequestIndex", integer))

	routesAnswer = object(p("routes", listOf(maxRoutes, rtRoute)), p("fallbackInfo", rtFallback),
		p("geocodingResults", object(p("origin", rtGeocoded), p("destination", rtGeocoded),
			p("intermediates", listOf(maxIntermediates, rtGeocoded)))))
	matrixElement = object(p("status", rtStatus), p("condition", short), p("distanceMeters", integer),
		p("duration", short), p("staticDuration", short), p("travelAdvisory", rtAdvisory),
		p("fallbackInfo", rtFallback), p("localizedValues", rtValues), p("originIndex", integer),
		p("destinationIndex", integer))
)

// The field paths a mask may name: every member of the answer shapes, nested ones as dot paths. A whole
// geometry is one field of Google's message, so its own members are no paths.
var (
	routesMaskPaths = maskPaths(routesAnswer)
	matrixMaskPaths = maskPaths(matrixElement)
)

func maskPaths(s *shape) map[string]bool {
	out := map[string]bool{}
	var walk func(s *shape, prefix string)
	walk = func(s *shape, prefix string) {
		for _, property := range s.props {
			path := prefix + property.name
			out[path] = true
			child := property.shape
			if child.kind == kArr {
				child = child.elem
			}
			if child.kind == kObj && !child.whole {
				walk(child, path+".")
			}
		}
	}
	walk(s, "")
	return out
}

// Bounds of a route field mask. The longest documented path has 72 characters.
const (
	maxRouteMaskPaths = 200
	maxRouteMaskPath  = 128
	maxRouteMaskBytes = 8192
)

// matrixAlways are the paths every matrix mask gets: without the indexes an element cannot be placed, and
// without status Google shows every element as successful. Google bills a matrix per element, not per field.
var matrixAlways = []string{"originIndex", "destinationIndex", "status"}

// routeMask is a checked field mask: its header value and the paths it names.
type routeMask struct {
	header string
	paths  map[string]bool
}

// parseRouteMask checks the field_mask of a route tool, a list of paths or one comma-separated string, against
// the documented paths before any secret is read or any request is built. There is no default and no wildcard,
// and the bare top-level routes, which Google documents as selecting every field, is refused as well. A
// message names the rule and the position of an entry, never its value.
func parseRouteMask(raw json.RawMessage, matrix bool) (routeMask, error) {
	var entries []string
	var text string
	switch {
	case len(raw) == 0 || string(raw) == "null":
		return routeMask{}, badRequest("field_mask is required: name the fields to return; there is no default")
	case json.Unmarshal(raw, &text) == nil:
		if len(text) > maxRouteMaskBytes {
			return routeMask{}, badRequest("field_mask is too long")
		}
		entries = strings.Split(text, ",")
	case json.Unmarshal(raw, &entries) == nil:
	default:
		return routeMask{}, badRequest("field_mask must be a list of field paths or one comma-separated string")
	}
	if len(entries) == 0 {
		return routeMask{}, badRequest("field_mask is required: name the fields to return; there is no default")
	}
	if len(entries) > maxRouteMaskPaths {
		return routeMask{}, badRequest("field_mask names too many fields")
	}
	known := routesMaskPaths
	if matrix {
		known = matrixMaskPaths
	}
	mask := routeMask{paths: map[string]bool{}}
	var ordered []string
	add := func(path string) {
		if !mask.paths[path] {
			mask.paths[path] = true
			ordered = append(ordered, path)
		}
	}
	for i, entry := range entries {
		path := strings.TrimSpace(entry)
		position := itoa(i + 1)
		switch {
		case path == "":
			return routeMask{}, badRequest("field_mask entry " + position + " is empty")
		case strings.Contains(path, "*"):
			return routeMask{}, badRequest("field_mask entry " + position +
				" is a wildcard; name the fields to return, a wildcard is never accepted")
		case len(path) > maxRouteMaskPath:
			return routeMask{}, badRequest("field_mask entry " + position + " is too long")
		case !matrix && path == "routes":
			return routeMask{}, badRequest("field_mask entry " + position + " selects every field of a route; " +
				"name the fields below routes, such as routes.duration")
		case !known[path]:
			return routeMask{}, badRequest("field_mask entry " + position + " is not a documented field path " +
				"of this answer; use Google's camelCase names with dots, such as " + maskExample(matrix))
		}
		add(path)
	}
	if matrix {
		for _, path := range matrixAlways {
			add(path)
		}
	}
	mask.header = strings.Join(ordered, ",")
	return mask, nil
}

func maskExample(matrix bool) string {
	if matrix {
		return "duration or localizedValues.distance.text"
	}
	return "routes.duration or routes.legs.steps.navigationInstruction"
}
