package googlemaps

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	timezoneGetID = Provider + ".timezone.get"
	timezonePath  = "/maps/api/timezone/json"
)

// Google documents the timestamp only as seconds since 1970-01-01 UTC. The bound keeps it within the years 1 to
// 9999, which any zone database covers; it is a Qatlas bound.
const (
	minTimezoneTimestamp = -62135596800
	maxTimezoneTimestamp = 253402300799
)

var timezoneService = legacyService{root: geocodingRoot, path: timezonePath, name: "Time Zone"}

var timezoneShape = object(p("status", short), p("timeZoneId", short), p("timeZoneName", title),
	p("rawOffset", decimal), p("dstOffset", decimal))

type timezoneArguments struct {
	Location  *geoPoint `json:"location"`
	Timestamp *int64    `json:"timestamp"`
	Language  string    `json:"language"`
}

var timezoneGet = capability.Descriptor{
	ID: timezoneGetID, Version: 1, Title: "Time zone of a coordinate from Google Maps",
	Description: "Read the time zone of a coordinate at a moment with the Google Time Zone API. location and " +
		"timestamp (seconds since 1970-01-01 UTC) are required; the timestamp decides whether daylight saving " +
		"applies, and Google ignores historical changes of a zone. The local time is timestamp + dstOffset + " +
		"rawOffset. language localizes timeZoneName; without it Google uses its default, because Qatlas sends no " +
		"Accept-Language header. Data may be missing over water: Google then answers ZERO_RESULTS, which is " +
		"returned as status without zone fields and not as an error. Google documents the key for this web " +
		"service only as a query parameter, so Qatlas sends it that way in a fixed URL and never shows the URL. " +
		"Cost: each call can incur charges above the free usage Google grants for the Time Zone SKU (Essentials; " +
		"10000 free events per month and billing account according to Google's price list, which may change). " +
		"Google Maps attribution applies to the results, and the storage rules of the Google Maps Platform " +
		"service terms apply and are the user's to keep; other caching rules are not verified here. The " +
		"coordinate is location data and is sent to Google. Results are untrusted third-party content.",
	Tags: []string{"google", "maps", "timezone"}, Risk: locationRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"location":` + latLngSchema + `,` +
		`"timestamp":{"type":"integer","minimum":` + strconv.Itoa(minTimezoneTimestamp) + `,"maximum":` +
		strconv.FormatInt(maxTimezoneTimestamp, 10) + `},"language":` + languageSchema + `},` +
		`"required":["location","timestamp"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(timezoneShape.schema()),
	Arguments: []capability.Argument{
		{Name: "location", Required: true, Description: "Point {latitude -90 to 90, longitude -180 to 180}"},
		{Name: "timestamp", Required: true, Description: "Moment as seconds since 1970-01-01 UTC, a whole number " +
			"within the years 1 to 9999"},
		{Name: "language", Description: "Language of timeZoneName, such as en or pt-BR; Google's default when omitted"},
	},
	Fields: []capability.Field{
		{Name: "status", Description: "OK, or ZERO_RESULTS when Google has no zone for the position or time"},
		{Name: "timeZoneId", Description: "CLDR identifier such as America/Los_Angeles; the canonical one when a zone has several"},
		{Name: "timeZoneName", Description: "Long name such as Pacific Daylight Time, localized with language; untrusted text"},
		{Name: "rawOffset", Description: "Offset from UTC in seconds without daylight saving"},
		{Name: "dstOffset", Description: "Daylight saving offset in seconds at the timestamp; 0 when it is not in effect"},
	},
	Examples: []capability.Example{{Description: "Time zone of Nevada on 2012-03-08",
		Arguments: json.RawMessage(`{"location":{"latitude":39.603481,"longitude":-119.682251},"timestamp":1331161200,"language":"es"}`)}},
}

func handleTimezoneGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "read the time zone"
	var a timezoneArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	point, ok := formatPoint(a.Location)
	if !ok {
		return nil, badRequest("location needs a latitude from -90 to 90 and a longitude from -180 to 180")
	}
	if a.Timestamp == nil || *a.Timestamp < minTimezoneTimestamp || *a.Timestamp > maxTimezoneTimestamp {
		return nil, badRequest("timestamp is required: whole seconds since 1970-01-01 UTC within the years 1 to 9999")
	}
	query := url.Values{"location": {point}, "timestamp": {strconv.FormatInt(*a.Timestamp, 10)}}
	if a.Language != "" {
		if !languagePattern.MatchString(a.Language) {
			return nil, badRequest("language must be a language tag such as en or pt-BR")
		}
		query.Set("language", a.Language)
	}
	client, err := Open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	answer, err := client.legacyGet(ctx, op, timezoneService, query)
	if err != nil {
		return nil, err
	}
	return cleanedAnswer(timezoneShape, answer), nil
}
