package googlemaps

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"time"
)

// Bounds Google documents for one request: 25 intermediates for a route, and for a matrix 625 elements, 100 with
// TRAFFIC_AWARE_OPTIMAL or TRANSIT, and 50 origins and destinations given by address or place ID.
const (
	maxIntermediates      = 25
	maxMatrixElements     = 625
	maxMatrixLimited      = 100
	maxMatrixPlaceAddress = 50
)

// Qatlas bounds of arguments Google gives no limit for.
const (
	maxRouteAddressRunes = 1000
	maxNavigationToken   = 1024
	maxVehicleValue      = 1_000_000_000
	maxTrailers          = 10
)

// The enumerations of the request, from the reference of Routes API v2. The unspecified values are not offered,
// nor the deprecated toll pass US_MI_IQ_PROX_CARD.
var (
	routeTravelModes      = []string{"DRIVE", "BICYCLE", "WALK", "TWO_WHEELER", "TRUCK", "TRANSIT"}
	polylineQualities     = []string{"HIGH_QUALITY", "OVERVIEW"}
	polylineEncodings     = []string{"ENCODED_POLYLINE", "GEO_JSON_LINESTRING"}
	referenceRouteValues  = []string{"FUEL_EFFICIENT", "SHORTER_DISTANCE"}
	routeExtraValues      = []string{"TOLLS", "FUEL_CONSUMPTION", "TRAFFIC_ON_POLYLINE", "HTML_FORMATTED_NAVIGATION_INSTRUCTIONS", "FLYOVER_INFO_ON_POLYLINE", "NARROW_ROAD_INFO_ON_POLYLINE", "TUNNEL_INFO_ON_POLYLINE", "BRIDGE_INFO_ON_POLYLINE", "SKYWAY_INFO_ON_POLYLINE"}
	matrixExtraValues     = []string{"TOLLS"}
	unitValues            = []string{"METRIC", "IMPERIAL"}
	trafficModelValues    = []string{"BEST_GUESS", "PESSIMISTIC", "OPTIMISTIC"}
	transitModeValues     = []string{"BUS", "SUBWAY", "TRAIN", "LIGHT_RAIL", "RAIL"}
	transitRoutingValues  = []string{"LESS_WALKING", "FEWER_TRANSFERS"}
	emissionTypeValues    = []string{"GASOLINE", "ELECTRIC", "HYBRID", "DIESEL"}
	hazardousGoodsValues  = []string{"EXPLOSIVES", "GASES", "FLAMMABLE", "COMBUSTIBLE", "ORGANIC", "POISON", "RADIOACTIVE", "CORROSIVE", "ASPIRATION_HAZARD", "ENVIRONMENTAL_HAZARD", "OTHER"}
	shorterDistanceModes  = []string{"DRIVE", "BICYCLE", "TWO_WHEELER"}
	navigationTokenFormat = regexp.MustCompile(`^[!-~]+$`)
	tollPassValues        = []string{"AU_ETOLL_TAG", "AU_EWAY_TAG", "AU_LINKT", "AR_TELEPASE", "BR_AUTO_EXPRESO",
		"BR_CONECTCAR", "BR_MOVE_MAIS", "BR_PASSA_RAPIDO", "BR_SEM_PARAR", "BR_TAGGY", "BR_VELOE",
		"CA_US_AKWASASNE_SEAWAY_CORPORATE_CARD", "CA_US_AKWASASNE_SEAWAY_TRANSIT_CARD", "CA_US_BLUE_WATER_EDGE_PASS",
		"CA_US_CONNEXION", "CA_US_NEXUS_CARD", "ID_E_TOLL", "IN_FASTAG", "IN_LOCAL_HP_PLATE_EXEMPT", "JP_ETC",
		"JP_ETC2", "MX_IAVE", "MX_PASE", "MX_QUICKPASS", "MX_SISTEMA_TELEPEAJE_CHIHUAHUA", "MX_TAG_IAVE",
		"MX_TAG_TELEVIA", "MX_TELEVIA", "MX_VIAPASS", "US_AL_FREEDOM_PASS",
		"US_AK_ANTON_ANDERSON_TUNNEL_BOOK_OF_10_TICKETS", "US_CA_FASTRAK", "US_CA_FASTRAK_CAV_STICKER",
		"US_CO_EXPRESSTOLL", "US_CO_GO_PASS", "US_DE_EZPASSDE", "US_FL_BOB_SIKES_TOLL_BRIDGE_PASS",
		"US_FL_DUNES_COMMUNITY_DEVELOPMENT_DISTRICT_EXPRESSCARD", "US_FL_EPASS", "US_FL_GIBA_TOLL_PASS",
		"US_FL_LEEWAY", "US_FL_SUNPASS", "US_FL_SUNPASS_PRO", "US_IL_EZPASSIL", "US_IL_IPASS", "US_IN_EZPASSIN",
		"US_KS_BESTPASS_HORIZON", "US_KS_KTAG", "US_KS_NATIONALPASS", "US_KS_PREPASS_ELITEPASS", "US_KY_RIVERLINK",
		"US_LA_GEAUXPASS", "US_LA_TOLL_TAG", "US_MA_EZPASSMA", "US_MD_EZPASSMD", "US_ME_EZPASSME",
		"US_MI_AMBASSADOR_BRIDGE_PREMIER_COMMUTER_CARD", "US_MI_BCPASS", "US_MI_BREAKAWAY",
		"US_MI_GROSSE_ILE_TOLL_BRIDGE_PASS_TAG", "US_MI_IQ_TAG", "US_MI_MACKINAC_BRIDGE_MAC_PASS",
		"US_MI_NEXPRESS_TOLL", "US_MN_EZPASSMN", "US_NC_EZPASSNC", "US_NC_PEACH_PASS", "US_NC_QUICK_PASS",
		"US_NH_EZPASSNH", "US_NJ_DOWNBEACH_EXPRESS_PASS", "US_NJ_EZPASSNJ", "US_NY_EXPRESSPASS", "US_NY_EZPASSNY",
		"US_OH_EZPASSOH", "US_PA_EZPASSPA", "US_RI_EZPASSRI", "US_SC_PALPASS", "US_TX_AVI_TAG", "US_TX_BANCPASS",
		"US_TX_DEL_RIO_PASS", "US_TX_EFAST_PASS", "US_TX_EAGLE_PASS_EXPRESS_CARD", "US_TX_EPTOLL", "US_TX_EZ_CROSS",
		"US_TX_EZTAG", "US_TX_FUEGO_TAG", "US_TX_LAREDO_TRADE_TAG", "US_TX_PLUSPASS", "US_TX_TOLLTAG",
		"US_TX_TXTAG", "US_TX_XPRESS_CARD", "US_UT_ADAMS_AVE_PARKWAY_EXPRESSCARD", "US_VA_EZPASSVA",
		"US_WA_BREEZEBY", "US_WA_GOOD_TO_GO", "US_WV_EZPASSWV", "US_WV_MEMORIAL_BRIDGE_TICKETS", "US_WV_MOV_PASS",
		"US_WV_NEWELL_TOLL_BRIDGE_TICKET"}
)

type routeLocationArgument struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Heading   *int     `json:"heading"`
}

type waypointArgument struct {
	Address              string                 `json:"address"`
	Location             *routeLocationArgument `json:"location"`
	PlaceID              string                 `json:"place_id"`
	NavigationPointToken string                 `json:"navigation_point_token"`
	Via                  *bool                  `json:"via"`
	VehicleStopover      *bool                  `json:"vehicle_stopover"`
	SideOfRoad           *bool                  `json:"side_of_road"`
}

type trailerArgument struct {
	HeightMm *int64 `json:"height_mm"`
	LengthMm *int64 `json:"length_mm"`
	WidthMm  *int64 `json:"width_mm"`
	WeightKg *int64 `json:"weight_kg"`
}

type vehicleArgument struct {
	EmissionType        string            `json:"emission_type"`
	TotalHeightMm       *int64            `json:"total_height_mm"`
	TotalLengthMm       *int64            `json:"total_length_mm"`
	TotalWidthMm        *int64            `json:"total_width_mm"`
	TotalWeightKg       *int64            `json:"total_weight_kg"`
	TrailerInfo         []trailerArgument `json:"trailer_info"`
	HazardousGoodsTypes []string          `json:"hazardous_goods_types"`
}

type modifiersArgument struct {
	AvoidTolls    *bool            `json:"avoid_tolls"`
	AvoidHighways *bool            `json:"avoid_highways"`
	AvoidFerries  *bool            `json:"avoid_ferries"`
	AvoidIndoor   *bool            `json:"avoid_indoor"`
	AvoidTunnels  *bool            `json:"avoid_tunnels"`
	VehicleInfo   *vehicleArgument `json:"vehicle_info"`
	TollPasses    []string         `json:"toll_passes"`
}

type transitArgument struct {
	AllowedTravelModes []string `json:"allowed_travel_modes"`
	RoutingPreference  string   `json:"routing_preference"`
}

// routeCommonArguments are the members both tools share.
type routeCommonArguments struct {
	FieldMask          json.RawMessage  `json:"field_mask"`
	TravelMode         string           `json:"travel_mode"`
	RoutingPreference  string           `json:"routing_preference"`
	DepartureTime      string           `json:"departure_time"`
	ArrivalTime        string           `json:"arrival_time"`
	LanguageCode       string           `json:"language_code"`
	RegionCode         string           `json:"region_code"`
	Units              string           `json:"units"`
	ExtraComputations  []string         `json:"extra_computations"`
	TrafficModel       string           `json:"traffic_model"`
	TransitPreferences *transitArgument `json:"transit_preferences"`
}

type routesComputeArguments struct {
	routeCommonArguments
	Origin                   *waypointArgument  `json:"origin"`
	Destination              *waypointArgument  `json:"destination"`
	Intermediates            []waypointArgument `json:"intermediates"`
	PolylineQuality          string             `json:"polyline_quality"`
	PolylineEncoding         string             `json:"polyline_encoding"`
	ComputeAlternativeRoutes *bool              `json:"compute_alternative_routes"`
	RouteModifiers           *modifiersArgument `json:"route_modifiers"`
	OptimizeWaypointOrder    *bool              `json:"optimize_waypoint_order"`
	RequestedReferenceRoutes []string           `json:"requested_reference_routes"`
}

type matrixOriginArgument struct {
	Waypoint       *waypointArgument  `json:"waypoint"`
	RouteModifiers *modifiersArgument `json:"route_modifiers"`
}

type matrixDestinationArgument struct {
	Waypoint *waypointArgument `json:"waypoint"`
}

type routesMatrixArguments struct {
	routeCommonArguments
	Origins      []matrixOriginArgument      `json:"origins"`
	Destinations []matrixDestinationArgument `json:"destinations"`
}

// routeSettings are the checked settings later checks depend on. mode is the effective travel mode: Google
// routes by car when none is given.
type routeSettings struct {
	mode, preference string
	matrix           bool
}

func (s routeSettings) motorized() bool { return s.mode == "DRIVE" || s.mode == "TWO_WHEELER" }

// routeEnums checks a list of enumeration values, each at most once.
func routeEnums(name string, values, allowed []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if !inSet(value, allowed) || seen[value] {
			return badRequest(name + " must list documented values, each at most once")
		}
		seen[value] = true
	}
	return nil
}

func routeTime(name, value string) error {
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return badRequest(name + " must be an RFC 3339 timestamp such as 2026-11-11T08:00:00Z or " +
			"2026-11-11T09:00:00+01:00")
	}
	return nil
}

// routeCommonBody checks the members both tools share and returns them as Google's body members together with the
// settings the other checks need.
func routeCommonBody(a routeCommonArguments, matrix bool) (map[string]any, routeSettings, error) {
	body := map[string]any{}
	set := routeSettings{mode: "DRIVE", matrix: matrix}
	if a.TravelMode != "" {
		if !inSet(a.TravelMode, routeTravelModes) {
			return nil, set, badRequest("travel_mode must be DRIVE, BICYCLE, WALK, TWO_WHEELER, TRUCK, or TRANSIT")
		}
		set.mode = a.TravelMode
		body["travelMode"] = a.TravelMode
	}
	if a.RoutingPreference != "" {
		if !inSet(a.RoutingPreference, routingPreferenceValues) {
			return nil, set, badRequest("routing_preference must be TRAFFIC_UNAWARE, TRAFFIC_AWARE, or TRAFFIC_AWARE_OPTIMAL")
		}
		if !set.motorized() {
			return nil, set, badRequest("routing_preference applies only to travel_mode DRIVE or TWO_WHEELER; " +
				"Google fails the request otherwise")
		}
		set.preference = a.RoutingPreference
		body["routingPreference"] = a.RoutingPreference
	}
	if a.DepartureTime != "" && a.ArrivalTime != "" {
		return nil, set, badRequest("give departure_time or arrival_time, not both")
	}
	if a.DepartureTime != "" {
		if err := routeTime("departure_time", a.DepartureTime); err != nil {
			return nil, set, err
		}
		body["departureTime"] = a.DepartureTime
	}
	if a.ArrivalTime != "" {
		if set.mode != "TRANSIT" {
			return nil, set, badRequest("arrival_time applies only to travel_mode TRANSIT")
		}
		if err := routeTime("arrival_time", a.ArrivalTime); err != nil {
			return nil, set, err
		}
		body["arrivalTime"] = a.ArrivalTime
	}
	if a.LanguageCode != "" {
		if !languagePattern.MatchString(a.LanguageCode) {
			return nil, set, badRequest("language_code must be a language tag such as en-US or sr-Latn")
		}
		body["languageCode"] = a.LanguageCode
	}
	if a.RegionCode != "" {
		if !regionPattern.MatchString(a.RegionCode) {
			return nil, set, badRequest("region_code must be a two-letter country code top-level domain such as de")
		}
		body["regionCode"] = a.RegionCode
	}
	if a.Units != "" {
		if !inSet(a.Units, unitValues) {
			return nil, set, badRequest("units must be METRIC or IMPERIAL")
		}
		body["units"] = a.Units
	}
	if len(a.ExtraComputations) > 0 {
		allowed := routeExtraValues
		if matrix {
			allowed = matrixExtraValues
		}
		if err := routeEnums("extra_computations", a.ExtraComputations, allowed); err != nil {
			return nil, set, err
		}
		body["extraComputations"] = a.ExtraComputations
	}
	if a.TrafficModel != "" {
		if !inSet(a.TrafficModel, trafficModelValues) {
			return nil, set, badRequest("traffic_model must be BEST_GUESS, PESSIMISTIC, or OPTIMISTIC")
		}
		if set.mode != "DRIVE" || set.preference != "TRAFFIC_AWARE_OPTIMAL" {
			return nil, set, badRequest("traffic_model needs travel_mode DRIVE and routing_preference TRAFFIC_AWARE_OPTIMAL")
		}
		body["trafficModel"] = a.TrafficModel
	}
	if a.TransitPreferences != nil {
		if set.mode != "TRANSIT" {
			return nil, set, badRequest("transit_preferences applies only to travel_mode TRANSIT")
		}
		transit := map[string]any{}
		if len(a.TransitPreferences.AllowedTravelModes) > 0 {
			if err := routeEnums("transit_preferences.allowed_travel_modes", a.TransitPreferences.AllowedTravelModes,
				transitModeValues); err != nil {
				return nil, set, err
			}
			transit["allowedTravelModes"] = a.TransitPreferences.AllowedTravelModes
		}
		if value := a.TransitPreferences.RoutingPreference; value != "" {
			if !inSet(value, transitRoutingValues) {
				return nil, set, badRequest("transit_preferences.routing_preference must be LESS_WALKING or FEWER_TRANSFERS")
			}
			transit["routingPreference"] = value
		}
		if len(transit) == 0 {
			return nil, set, badRequest("transit_preferences needs allowed_travel_modes or routing_preference")
		}
		body["transitPreferences"] = transit
	}
	return body, set, nil
}

// waypointBody checks one waypoint. intermediate says whether via is allowed; Google fails a request with via
// on a terminal waypoint, and a matrix has none other.
func waypointBody(name string, w *waypointArgument, set routeSettings, intermediate bool) (map[string]any, error) {
	if w == nil {
		return nil, badRequest(name + " is required")
	}
	body := map[string]any{}
	forms := 0
	if w.Address != "" {
		forms++
		if !plainText(w.Address, maxRouteAddressRunes) {
			return nil, badRequest(name + ".address must be 1 to 1000 characters of text without control characters")
		}
		body["address"] = w.Address
	}
	if w.Location != nil {
		forms++
		l := w.Location
		if l.Latitude == nil || l.Longitude == nil || math.IsNaN(*l.Latitude) || math.IsNaN(*l.Longitude) ||
			math.IsInf(*l.Latitude, 0) || math.IsInf(*l.Longitude, 0) || *l.Latitude < -90 || *l.Latitude > 90 ||
			*l.Longitude < -180 || *l.Longitude > 180 {
			return nil, badRequest(name + ".location needs a latitude from -90 to 90 and a longitude from -180 to 180")
		}
		location := map[string]any{"latLng": map[string]any{"latitude": *l.Latitude, "longitude": *l.Longitude}}
		if l.Heading != nil {
			if *l.Heading < 0 || *l.Heading > 360 {
				return nil, badRequest(name + ".location.heading must be 0 to 360")
			}
			if !set.motorized() {
				return nil, badRequest(name + ".location.heading applies only to travel_mode DRIVE or TWO_WHEELER")
			}
			location["heading"] = *l.Heading
		}
		body["location"] = location
	}
	if w.PlaceID != "" {
		forms++
		if !placeIDPattern.MatchString(w.PlaceID) {
			return nil, badRequest(name + ".place_id must be 1 to 256 letters, digits, - or _")
		}
		body["placeId"] = w.PlaceID
	}
	if w.NavigationPointToken != "" {
		forms++
		if len(w.NavigationPointToken) > maxNavigationToken || !navigationTokenFormat.MatchString(w.NavigationPointToken) {
			return nil, badRequest(name + ".navigation_point_token must be 1 to 1024 visible ASCII characters")
		}
		body["navigationPointToken"] = w.NavigationPointToken
	}
	if forms != 1 {
		return nil, badRequest(name + " needs exactly one of address, location, place_id, and navigation_point_token")
	}
	if w.Via != nil {
		if !intermediate {
			return nil, badRequest(name + ".via is allowed on intermediates only; Google fails the request otherwise")
		}
		body["via"] = *w.Via
	}
	if w.VehicleStopover != nil {
		body["vehicleStopover"] = *w.VehicleStopover
	}
	if w.SideOfRoad != nil {
		body["sideOfRoad"] = *w.SideOfRoad
	}
	return body, nil
}

func vehicleValue(name string, value *int64, out map[string]any, member string) error {
	if value == nil {
		return nil
	}
	if *value < 1 || *value > maxVehicleValue {
		return badRequest(name + " must be 1 to " + strconv.Itoa(maxVehicleValue))
	}
	// Google takes these int64 members as strings.
	out[member] = strconv.FormatInt(*value, 10)
	return nil
}

// modifiersBody checks route modifiers. A matrix does not take avoid_tunnels, and with DRIVE not the vehicle
// dimensions or trailers; the reference documents these as not supported there.
func modifiersBody(name string, m *modifiersArgument, set routeSettings) (map[string]any, error) {
	body := map[string]any{}
	if m == nil {
		if set.mode == "TRUCK" {
			return nil, badRequest(name + ".vehicle_info with its total height, length, width, and weight is " +
				"required for travel_mode TRUCK")
		}
		return body, nil
	}
	for _, flag := range []struct {
		value  *bool
		member string
	}{{m.AvoidTolls, "avoidTolls"}, {m.AvoidHighways, "avoidHighways"}, {m.AvoidFerries, "avoidFerries"},
		{m.AvoidIndoor, "avoidIndoor"}} {
		if flag.value != nil {
			body[flag.member] = *flag.value
		}
	}
	if m.AvoidTunnels != nil {
		if set.matrix {
			return nil, badRequest(name + ".avoid_tunnels is not supported by the route matrix")
		}
		if set.preference != "TRAFFIC_AWARE_OPTIMAL" {
			return nil, badRequest(name + ".avoid_tunnels needs routing_preference TRAFFIC_AWARE_OPTIMAL")
		}
		body["avoidTunnels"] = *m.AvoidTunnels
	}
	if len(m.TollPasses) > 0 {
		if err := routeEnums(name+".toll_passes", m.TollPasses, tollPassValues); err != nil {
			return nil, err
		}
		body["tollPasses"] = m.TollPasses
	}
	v := m.VehicleInfo
	if v == nil {
		if set.mode == "TRUCK" {
			return nil, badRequest(name + ".vehicle_info with its total height, length, width, and weight is " +
				"required for travel_mode TRUCK")
		}
		return body, nil
	}
	vehicle := map[string]any{}
	if v.EmissionType != "" {
		if !inSet(v.EmissionType, emissionTypeValues) {
			return nil, badRequest(name + ".vehicle_info.emission_type must be GASOLINE, ELECTRIC, HYBRID, or DIESEL")
		}
		vehicle["emissionType"] = v.EmissionType
	}
	dimensions := v.TotalHeightMm != nil || v.TotalLengthMm != nil || v.TotalWidthMm != nil || v.TotalWeightKg != nil ||
		len(v.TrailerInfo) > 0
	if set.matrix && set.mode == "DRIVE" && dimensions {
		return nil, badRequest(name + ".vehicle_info dimensions and trailer_info are not supported by the route " +
			"matrix with travel_mode DRIVE")
	}
	if set.mode == "TRUCK" && (v.TotalHeightMm == nil || v.TotalLengthMm == nil || v.TotalWidthMm == nil ||
		v.TotalWeightKg == nil) {
		return nil, badRequest(name + ".vehicle_info needs total_height_mm, total_length_mm, total_width_mm, and " +
			"total_weight_kg for travel_mode TRUCK")
	}
	prefix := name + ".vehicle_info."
	for _, total := range []struct {
		value       *int64
		arg, member string
	}{{v.TotalHeightMm, "total_height_mm", "totalHeightMm"}, {v.TotalLengthMm, "total_length_mm", "totalLengthMm"},
		{v.TotalWidthMm, "total_width_mm", "totalWidthMm"}, {v.TotalWeightKg, "total_weight_kg", "totalWeightKg"}} {
		if err := vehicleValue(prefix+total.arg, total.value, vehicle, total.member); err != nil {
			return nil, err
		}
	}
	if len(v.TrailerInfo) > 0 {
		if len(v.TrailerInfo) > maxTrailers {
			return nil, badRequest(prefix + "trailer_info takes at most " + itoa(maxTrailers) + " trailers")
		}
		if set.preference != "TRAFFIC_AWARE_OPTIMAL" {
			return nil, badRequest(prefix + "trailer_info needs routing_preference TRAFFIC_AWARE_OPTIMAL")
		}
		trailers := make([]map[string]any, 0, len(v.TrailerInfo))
		for i, trailer := range v.TrailerInfo {
			entry := map[string]any{}
			at := prefix + "trailer_info[" + itoa(i) + "]."
			for _, member := range []struct {
				value       *int64
				arg, member string
			}{{trailer.HeightMm, "height_mm", "heightMm"}, {trailer.LengthMm, "length_mm", "lengthMm"},
				{trailer.WidthMm, "width_mm", "widthMm"}, {trailer.WeightKg, "weight_kg", "weightKg"}} {
				if err := vehicleValue(at+member.arg, member.value, entry, member.member); err != nil {
					return nil, err
				}
			}
			if len(entry) == 0 {
				return nil, badRequest(at[:len(at)-1] + " needs at least one dimension")
			}
			trailers = append(trailers, entry)
		}
		vehicle["trailerInfo"] = trailers
	}
	if len(v.HazardousGoodsTypes) > 0 {
		if err := routeEnums(prefix+"hazardous_goods_types", v.HazardousGoodsTypes, hazardousGoodsValues); err != nil {
			return nil, err
		}
		vehicle["hazardousGoodsTypes"] = v.HazardousGoodsTypes
	}
	if len(vehicle) > 0 {
		body["vehicleInfo"] = vehicle
	}
	return body, nil
}

// computeBody builds the body of computeRoutes from checked arguments only. It never holds the key.
func computeBody(a routesComputeArguments, mask routeMask) (map[string]any, error) {
	body, set, err := routeCommonBody(a.routeCommonArguments, false)
	if err != nil {
		return nil, err
	}
	if body["origin"], err = waypointBody("origin", a.Origin, set, false); err != nil {
		return nil, err
	}
	if body["destination"], err = waypointBody("destination", a.Destination, set, false); err != nil {
		return nil, err
	}
	if len(a.Intermediates) > maxIntermediates {
		return nil, badRequest("intermediates takes at most " + itoa(maxIntermediates) + " waypoints")
	}
	via := false
	if len(a.Intermediates) > 0 {
		intermediates := make([]map[string]any, 0, len(a.Intermediates))
		for i := range a.Intermediates {
			waypoint, err := waypointBody("intermediates["+itoa(i)+"]", &a.Intermediates[i], set, true)
			if err != nil {
				return nil, err
			}
			via = via || (a.Intermediates[i].Via != nil && *a.Intermediates[i].Via)
			intermediates = append(intermediates, waypoint)
		}
		body["intermediates"] = intermediates
	}
	if a.PolylineQuality != "" {
		if !inSet(a.PolylineQuality, polylineQualities) {
			return nil, badRequest("polyline_quality must be HIGH_QUALITY or OVERVIEW")
		}
		body["polylineQuality"] = a.PolylineQuality
	}
	if a.PolylineEncoding != "" {
		if !inSet(a.PolylineEncoding, polylineEncodings) {
			return nil, badRequest("polyline_encoding must be ENCODED_POLYLINE or GEO_JSON_LINESTRING")
		}
		body["polylineEncoding"] = a.PolylineEncoding
	}
	if a.ComputeAlternativeRoutes != nil {
		body["computeAlternativeRoutes"] = *a.ComputeAlternativeRoutes
	}
	modifiers, err := modifiersBody("route_modifiers", a.RouteModifiers, set)
	if err != nil {
		return nil, err
	}
	if len(modifiers) > 0 {
		body["routeModifiers"] = modifiers
	}
	optimize := a.OptimizeWaypointOrder != nil && *a.OptimizeWaypointOrder
	if a.OptimizeWaypointOrder != nil {
		if optimize && via {
			return nil, badRequest("optimize_waypoint_order cannot be used with a via intermediate; Google fails the request")
		}
		if optimize && !mask.paths["routes.optimizedIntermediateWaypointIndex"] {
			return nil, badRequest("optimize_waypoint_order needs routes.optimizedIntermediateWaypointIndex in " +
				"field_mask; Google fails the request otherwise")
		}
		body["optimizeWaypointOrder"] = *a.OptimizeWaypointOrder
	}
	if len(a.RequestedReferenceRoutes) > 0 {
		if err := routeEnums("requested_reference_routes", a.RequestedReferenceRoutes, referenceRouteValues); err != nil {
			return nil, err
		}
		if inSet("SHORTER_DISTANCE", a.RequestedReferenceRoutes) &&
			(!inSet(set.mode, shorterDistanceModes) || via || optimize) {
			return nil, badRequest("requested_reference_routes SHORTER_DISTANCE needs travel_mode DRIVE, BICYCLE, or " +
				"TWO_WHEELER and works with neither a via intermediate nor optimize_waypoint_order")
		}
		body["requestedReferenceRoutes"] = a.RequestedReferenceRoutes
	}
	return body, nil
}

// matrixBody builds the body of computeRouteMatrix from checked arguments only and returns the number of
// elements Google answers with.
func matrixBody(a routesMatrixArguments) (map[string]any, int, error) {
	body, set, err := routeCommonBody(a.routeCommonArguments, true)
	if err != nil {
		return nil, 0, err
	}
	if len(a.Origins) == 0 || len(a.Destinations) == 0 {
		return nil, 0, badRequest("origins and destinations each need at least one waypoint")
	}
	elements := len(a.Origins) * len(a.Destinations)
	switch {
	case elements > maxMatrixElements:
		return nil, 0, badRequest("origins times destinations must not exceed " + itoa(maxMatrixElements) + " elements")
	case elements > maxMatrixLimited && set.preference == "TRAFFIC_AWARE_OPTIMAL":
		return nil, 0, badRequest("origins times destinations must not exceed " + itoa(maxMatrixLimited) +
			" elements with routing_preference TRAFFIC_AWARE_OPTIMAL")
	case elements > maxMatrixLimited && set.mode == "TRANSIT":
		return nil, 0, badRequest("origins times destinations must not exceed " + itoa(maxMatrixLimited) +
			" elements with travel_mode TRANSIT")
	}
	named := 0
	count := func(w *waypointArgument) {
		if w != nil && (w.Address != "" || w.PlaceID != "") {
			named++
		}
	}
	origins := make([]map[string]any, 0, len(a.Origins))
	for i, origin := range a.Origins {
		name := "origins[" + itoa(i) + "]"
		waypoint, err := waypointBody(name+".waypoint", origin.Waypoint, set, false)
		if err != nil {
			return nil, 0, err
		}
		count(origin.Waypoint)
		entry := map[string]any{"waypoint": waypoint}
		modifiers, err := modifiersBody(name+".route_modifiers", origin.RouteModifiers, set)
		if err != nil {
			return nil, 0, err
		}
		if len(modifiers) > 0 {
			entry["routeModifiers"] = modifiers
		}
		origins = append(origins, entry)
	}
	destinations := make([]map[string]any, 0, len(a.Destinations))
	for i, destination := range a.Destinations {
		waypoint, err := waypointBody("destinations["+itoa(i)+"].waypoint", destination.Waypoint, set, false)
		if err != nil {
			return nil, 0, err
		}
		count(destination.Waypoint)
		destinations = append(destinations, map[string]any{"waypoint": waypoint})
	}
	if named > maxMatrixPlaceAddress {
		return nil, 0, badRequest("origins and destinations may name at most " + itoa(maxMatrixPlaceAddress) +
			" waypoints by address or place_id together")
	}
	body["origins"], body["destinations"] = origins, destinations
	return body, elements, nil
}
