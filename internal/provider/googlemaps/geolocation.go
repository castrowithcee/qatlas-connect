package googlemaps

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	geolocationLocateID = Provider + ".geolocation.locate"
	geolocationPath     = "/geolocation/v1/geolocate"
)

// Google documents no maximum for the two lists, so the counts are Qatlas bounds: a device sees a handful of
// cells and, at most, a few dozen access points.
const (
	maxCellTowers    = 32
	maxWifiAccessPts = 100
	maxCarrierRunes  = 100
)

var geolocationRadioTypes = []string{"gsm", "cdma", "wcdma", "lte", "nr"}

var (
	macPattern = regexp.MustCompile(`^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$`)
	// Google tells callers to drop the broadcast address and the IANA-reserved range 00:00:5E:00:00:00 to
	// 00:00:5E:FF:FF:FF before sending.
	reservedMAC = regexp.MustCompile(`(?i)^(ff:ff:ff:ff:ff:ff|00:00:5e:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2})$`)
)

var geolocationShape = object(p("location", geoLatLng), p("accuracy", decimal))

type cellTowerArgument struct {
	CellID            *int64   `json:"cell_id"`
	NewRadioCellID    *int64   `json:"new_radio_cell_id"`
	LocationAreaCode  *int64   `json:"location_area_code"`
	MobileCountryCode *int64   `json:"mobile_country_code"`
	MobileNetworkCode *int64   `json:"mobile_network_code"`
	Age               *int64   `json:"age"`
	SignalStrength    *float64 `json:"signal_strength"`
	TimingAdvance     *float64 `json:"timing_advance"`
}

type wifiAccessPointArgument struct {
	MACAddress         string   `json:"mac_address"`
	SignalStrength     *float64 `json:"signal_strength"`
	Age                *int64   `json:"age"`
	Channel            *int64   `json:"channel"`
	SignalToNoiseRatio *float64 `json:"signal_to_noise_ratio"`
}

type geolocationArguments struct {
	HomeMobileCountryCode *int64                    `json:"home_mobile_country_code"`
	HomeMobileNetworkCode *int64                    `json:"home_mobile_network_code"`
	RadioType             string                    `json:"radio_type"`
	Carrier               string                    `json:"carrier"`
	ConsiderIP            *bool                     `json:"consider_ip"`
	CellTowers            []cellTowerArgument       `json:"cell_towers"`
	WifiAccessPoints      []wifiAccessPointArgument `json:"wifi_access_points"`
}

const (
	uint32Schema   = `{"type":"integer","minimum":0,"maximum":4294967295}`
	dbmSchema      = `{"type":"number","minimum":-1000,"maximum":1000}`
	geolocationCap = " Google documents no maximum for cell_towers and wifi_access_points; Qatlas allows 32 and 100."
)

var geolocationLocate = capability.Descriptor{
	ID: geolocationLocateID, Version: 1, Title: "Locate a device from radio data with Google Maps",
	Description: "Estimate a position and its accuracy radius in meters from the radio environment of a device with " +
		"the Google Geolocation API. With no arguments, or with consider_ip alone, Google locates the " +
		"connection by its IP address (consider_ip defaults to true at Google; false forbids that fallback). " +
		"cell_towers (cell identity and signal data) and wifi_access_points (at least two, with MAC address) give " +
		"a better position; radio_type names the cell network and defaults to gsm at Google when omitted, which " +
		"gives wrong or empty results if the network is another one, so always give it when known. The call is a " +
		"POST that changes nothing and is safe to repeat. A position that Google cannot determine, HTTP 404 " +
		"notFound, is not an error of the key and is reported as not-found. Google documents the key for this " +
		"API only as a query parameter, so Qatlas sends it that way in a fixed URL on the shared Google APIs " +
		"host and never shows the URL; other credentials are not used." + geolocationCap + " Cost: each call can " +
		"incur charges above the free usage Google grants for the Geolocation SKU (Essentials; 10000 free events " +
		"per month and billing account according to Google's price list, which may change). Google Maps " +
		"attribution applies to the results, and the storage rules of the Google Maps Platform service terms " +
		"apply and are the user's to keep; other caching rules are not verified here. Wi-Fi MAC addresses, cell " +
		"towers, and the position are personal location data and are sent to Google, which compares them with " +
		"its own database. The result is untrusted third-party content.",
	Tags: []string{"google", "maps", "geolocation"}, Risk: locationRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"home_mobile_country_code":{"type":"integer","minimum":0,"maximum":999},` +
		`"home_mobile_network_code":{"type":"integer","minimum":0,"maximum":32767},` +
		`"radio_type":` + enumSchema(geolocationRadioTypes) + `,` +
		`"carrier":{"type":"string","minLength":1,"maxLength":100},"consider_ip":{"type":"boolean"},` +
		`"cell_towers":{"type":"array","minItems":1,"maxItems":32,"items":{"type":"object","properties":{` +
		`"cell_id":{"type":"integer","minimum":0,"maximum":268435455},` +
		`"new_radio_cell_id":{"type":"integer","minimum":0,"maximum":68719476735},` +
		`"location_area_code":{"type":"integer","minimum":0,"maximum":16777215},` +
		`"mobile_country_code":{"type":"integer","minimum":0,"maximum":999},` +
		`"mobile_network_code":{"type":"integer","minimum":0,"maximum":32767},"age":` + uint32Schema + `,` +
		`"signal_strength":` + dbmSchema + `,"timing_advance":{"type":"number","minimum":0,"maximum":1000000}},` +
		`"required":["mobile_network_code"],"additionalProperties":false}},` +
		`"wifi_access_points":{"type":"array","minItems":2,"maxItems":100,"items":{"type":"object","properties":{` +
		`"mac_address":{"type":"string","minLength":17,"maxLength":17},` +
		`"signal_strength":{"type":"number","minimum":-128,"maximum":-10},"age":` + uint32Schema + `,` +
		`"channel":` + uint32Schema + `,"signal_to_noise_ratio":` + dbmSchema + `},` +
		`"required":["mac_address"],"additionalProperties":false}}},"additionalProperties":false}`),
	OutputSchema: json.RawMessage(geolocationShape.schema()),
	Arguments: []capability.Argument{
		{Name: "home_mobile_country_code", Description: "Home mobile country code, 0 to 999; not used for cdma"},
		{Name: "home_mobile_network_code", Description: "Home mobile network code, 0 to 999; for cdma the system " +
			"identification number (SID), 0 to 32767"},
		{Name: "radio_type", Description: "gsm (Google's default), cdma, wcdma, lte, or nr; give it when known"},
		{Name: "carrier", Description: "Carrier name, at most 100 characters"},
		{Name: "consider_ip", Description: "false forbids the fallback to the IP address when radio data does not " +
			"locate the device; Google's default is true"},
		{Name: "cell_towers", Description: "1 to 32 cell towers, each {cell_id, new_radio_cell_id, " +
			"location_area_code, mobile_country_code, mobile_network_code (required), age in ms, signal_strength in " +
			"dBm, timing_advance}. cell_id (0 to 65535 for gsm and cdma, 0 to 268435455 for wcdma and lte) is " +
			"required for gsm, cdma, wcdma, lte and refused for nr; new_radio_cell_id (0 to 68719476735) is " +
			"required for nr and refused otherwise; location_area_code (0 to 65535, nr up to 16777215) is required " +
			"for gsm and cdma; mobile_country_code (0 to 999) is required except for cdma, where it is refused; " +
			"mobile_network_code is 0 to 999 (cdma: SID up to 32767). The rules follow radio_type, which is gsm when omitted"},
		{Name: "wifi_access_points", Description: "2 to 100 Wi-Fi access points, each {mac_address (required, " +
			"colon-separated hex), signal_strength in dBm (-128 to -10), age in ms, channel, signal_to_noise_ratio " +
			"in dB}. Google needs two or more distinct, stationary access points, ignores locally administered " +
			"addresses silently, and tells callers to drop FF:FF:FF:FF:FF:FF and 00:00:5E:00:00:00 to " +
			"00:00:5E:FF:FF:FF, which Qatlas refuses"},
	},
	Fields: []capability.Field{
		{Name: "location", Description: "Estimated position {lat, lng} in degrees"},
		{Name: "accuracy", Description: "Accuracy radius in meters; typically about 20 m with Wi-Fi, hundreds of " +
			"meters with macro cells, thousands with an IP address"},
	},
	Examples: []capability.Example{
		{Description: "Locate by IP address", Arguments: json.RawMessage(`{}`)},
		{Description: "Locate by two access points", Arguments: json.RawMessage(`{"consider_ip":false,` +
			`"wifi_access_points":[{"mac_address":"00:25:9c:cf:1c:ac","signal_strength":-43,"channel":11},` +
			`{"mac_address":"00:25:9c:cf:1c:ad","signal_strength":-55,"channel":11}]}`)},
	},
}

// geolocationFields maps the error object of the Geolocation API, whose entries carry a reason and a message.
var geolocationFields = provider.OriginalFields{
	Code: "error.code", Message: "error.message", List: "error.errors",
	Item: &provider.OriginalFields{Code: "reason", Message: "message"},
}

// geolocationReasons are the reasons of the error descriptions. Only these may shape a message; any other
// value is provider text.
var geolocationReasons = []string{"dailyLimitExceeded", "keyInvalid", "userRateLimitExceeded", "notFound", "parseError"}

func geolocationReason(body []byte) string {
	var answer struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	for _, entry := range answer.Error.Errors {
		if slices.Contains(geolocationReasons, entry.Reason) {
			return entry.Reason
		}
	}
	return ""
}

// geolocationError names the Geolocation API in the messages of a failed answer. Google documents 400
// keyInvalid and parseError, 403 dailyLimitExceeded and userRateLimitExceeded, and 404 notFound; the reason
// decides within a status, and the HTTP status alone decides without a known reason. The body only goes along
// as the Original.
func geolocationError(failed *provider.Error, status int, body []byte) *provider.Error {
	reason := geolocationReason(body)
	switch {
	case status == http.StatusNotFound:
		failed.Class = provider.ClassNotFound
		failed.Message = "Google could not determine a position from this data (HTTP 404 notFound); the request was " +
			"valid but returned no result, so give more or better radio data or allow consider_ip"
	case status == http.StatusBadRequest && reason == "keyInvalid":
		failed.Class = provider.ClassAuth
		failed.Message = "Google did not accept the key for the Geolocation API; check that the API is enabled in the " +
			"key's project, that the project has a billing account, and the key's restrictions"
	case status == http.StatusBadRequest:
		failed.Class = provider.ClassInvalidResponse
		failed.Message = "Google rejected the geolocation request as invalid (HTTP 400) although Qatlas Connect built " +
			"it from checked arguments; the API may have changed"
	case status == http.StatusForbidden && (reason == "dailyLimitExceeded" || reason == "userRateLimitExceeded"):
		failed.Class = provider.ClassRateLimited
		failed.Message = "Google reports the usage quota or a request cap of the key's project as reached (" + reason + ")"
	case failed.Class == provider.ClassPermission:
		failed.Message = "Google refused the key for the Geolocation API; check that the API is enabled in the key's " +
			"project, that the project has a billing account, and the key's restrictions"
	}
	failed.Original = provider.ErrorOriginal(status, body, geolocationFields)
	return failed
}

func checkRange(value *int64, name string, maximum int64) error {
	if value != nil && (*value < 0 || *value > maximum) {
		return badRequest(name + " must be a whole number from 0 to " + itoa(int(maximum)))
	}
	return nil
}

func finite(value *float64, name string, low, high float64) error {
	if value != nil && (math.IsNaN(*value) || *value < low || *value > high) {
		return badRequest(name + " must be a number from " + itoa(int(low)) + " to " + itoa(int(high)))
	}
	return nil
}

// geolocationBody checks every argument and returns the JSON body in Google's names. A missing optional member
// stays missing, so Google's defaults apply.
func geolocationBody(a geolocationArguments) (map[string]any, error) {
	body := map[string]any{}
	radio := a.RadioType
	if radio != "" && !slices.Contains(geolocationRadioTypes, radio) {
		return nil, badRequest("radio_type must be one of " + strings.Join(geolocationRadioTypes, ", "))
	}
	if radio == "" {
		radio = "gsm"
	} else {
		body["radioType"] = radio
	}
	cdma := radio == "cdma"
	networkMax := int64(999)
	if cdma {
		networkMax = 32767
	}
	for name, field := range map[string]struct {
		value   *int64
		maximum int64
		key     string
	}{
		"home_mobile_country_code": {a.HomeMobileCountryCode, 999, "homeMobileCountryCode"},
		"home_mobile_network_code": {a.HomeMobileNetworkCode, networkMax, "homeMobileNetworkCode"},
	} {
		if err := checkRange(field.value, name, field.maximum); err != nil {
			return nil, err
		}
		if field.value != nil {
			body[field.key] = *field.value
		}
	}
	if cdma && a.HomeMobileCountryCode != nil {
		return nil, badRequest("home_mobile_country_code is not used for radio_type cdma")
	}
	if a.Carrier != "" {
		if !plainText(a.Carrier, maxCarrierRunes) {
			return nil, badRequest("carrier must be 1 to 100 characters of text without control characters")
		}
		body["carrier"] = a.Carrier
	}
	if a.ConsiderIP != nil {
		body["considerIp"] = *a.ConsiderIP
	}
	if a.CellTowers != nil {
		if len(a.CellTowers) < 1 || len(a.CellTowers) > maxCellTowers {
			return nil, badRequest("cell_towers takes 1 to " + itoa(maxCellTowers) + " towers")
		}
		towers := make([]map[string]any, len(a.CellTowers))
		for i, tower := range a.CellTowers {
			built, err := cellTowerBody(tower, radio)
			if err != nil {
				return nil, err
			}
			towers[i] = built
		}
		body["cellTowers"] = towers
	}
	if a.WifiAccessPoints != nil {
		if len(a.WifiAccessPoints) < 2 || len(a.WifiAccessPoints) > maxWifiAccessPts {
			return nil, badRequest("wifi_access_points takes 2 to " + itoa(maxWifiAccessPts) + " access points")
		}
		points := make([]map[string]any, len(a.WifiAccessPoints))
		for i, point := range a.WifiAccessPoints {
			built, err := wifiBody(point)
			if err != nil {
				return nil, err
			}
			points[i] = built
		}
		body["wifiAccessPoints"] = points
	}
	return body, nil
}

func cellTowerBody(t cellTowerArgument, radio string) (map[string]any, error) {
	nr, cdma := radio == "nr", radio == "cdma"
	cellMax := int64(268435455)
	if radio == "gsm" || cdma {
		cellMax = 65535
	}
	areaMax := int64(65535)
	if nr {
		areaMax = 16777215
	}
	networkMax := int64(999)
	if cdma {
		networkMax = 32767
	}
	switch {
	case nr && t.CellID != nil:
		return nil, badRequest("cell_id is refused for radio_type nr; give new_radio_cell_id")
	case nr && t.NewRadioCellID == nil:
		return nil, badRequest("new_radio_cell_id is required for radio_type nr")
	case !nr && t.NewRadioCellID != nil:
		return nil, badRequest("new_radio_cell_id is only for radio_type nr")
	case !nr && t.CellID == nil:
		return nil, badRequest("cell_id is required for radio_type " + radio)
	case (radio == "gsm" || cdma) && t.LocationAreaCode == nil:
		return nil, badRequest("location_area_code is required for radio_type " + radio)
	case cdma && t.MobileCountryCode != nil:
		return nil, badRequest("mobile_country_code is not used for radio_type cdma")
	case !cdma && t.MobileCountryCode == nil:
		return nil, badRequest("mobile_country_code is required for radio_type " + radio)
	case t.MobileNetworkCode == nil:
		return nil, badRequest("mobile_network_code is required for a cell tower")
	}
	body := map[string]any{}
	for _, field := range []struct {
		value   *int64
		name    string
		key     string
		maximum int64
	}{
		{t.CellID, "cell_id", "cellId", cellMax},
		{t.NewRadioCellID, "new_radio_cell_id", "newRadioCellId", 68719476735},
		{t.LocationAreaCode, "location_area_code", "locationAreaCode", areaMax},
		{t.MobileCountryCode, "mobile_country_code", "mobileCountryCode", 999},
		{t.MobileNetworkCode, "mobile_network_code", "mobileNetworkCode", networkMax},
		{t.Age, "age", "age", 4294967295},
	} {
		if err := checkRange(field.value, field.name, field.maximum); err != nil {
			return nil, err
		}
		if field.value != nil {
			body[field.key] = *field.value
		}
	}
	if err := finite(t.SignalStrength, "signal_strength", -1000, 1000); err != nil {
		return nil, err
	}
	if err := finite(t.TimingAdvance, "timing_advance", 0, 1000000); err != nil {
		return nil, err
	}
	if t.SignalStrength != nil {
		body["signalStrength"] = *t.SignalStrength
	}
	if t.TimingAdvance != nil {
		body["timingAdvance"] = *t.TimingAdvance
	}
	return body, nil
}

func wifiBody(w wifiAccessPointArgument) (map[string]any, error) {
	if !macPattern.MatchString(w.MACAddress) {
		return nil, badRequest("mac_address must be six colon-separated hex pairs such as 00:25:9c:cf:1c:ac")
	}
	if reservedMAC.MatchString(w.MACAddress) {
		return nil, badRequest("mac_address may not be the broadcast address or in the IANA-reserved range 00:00:5E")
	}
	if err := finite(w.SignalStrength, "signal_strength", -128, -10); err != nil {
		return nil, err
	}
	if err := finite(w.SignalToNoiseRatio, "signal_to_noise_ratio", -1000, 1000); err != nil {
		return nil, err
	}
	body := map[string]any{"macAddress": w.MACAddress}
	for _, field := range []struct {
		value *int64
		name  string
		key   string
	}{{w.Age, "age", "age"}, {w.Channel, "channel", "channel"}} {
		if err := checkRange(field.value, field.name, 4294967295); err != nil {
			return nil, err
		}
		if field.value != nil {
			body[field.key] = *field.value
		}
	}
	if w.SignalStrength != nil {
		body["signalStrength"] = *w.SignalStrength
	}
	if w.SignalToNoiseRatio != nil {
		body["signalToNoiseRatio"] = *w.SignalToNoiseRatio
	}
	return body, nil
}

func handleGeolocationLocate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "locate a device"
	var a geolocationArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	body, err := geolocationBody(a)
	if err != nil {
		return nil, err
	}
	client, err := Open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	answer, err := client.postKeyedTo(ctx, geolocationRoot, op, geolocationPath, body)
	if err != nil {
		return nil, err
	}
	return cleanedAnswer(geolocationShape, answer), nil
}
