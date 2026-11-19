package googlemeasurementprotocol

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const eventsSendID = Provider + ".events.send"

const dataSensitivity = "google-analytics-event-data"

// Limits of the Measurement Protocol reference. The body must be under 130 kB; 130000 bytes is the cautious
// reading of kB. The two value limits are the longest Google names: standard properties keep 100 characters
// and drop longer values silently, Analytics 360 properties keep 500.
const (
	maxEvents       = 25
	maxBodyBytes    = 130000
	maxParams       = 25
	maxUserProps    = 25
	maxNameLength   = 40
	maxParamValue   = 500
	maxPropNameLen  = 24
	maxPropValueLen = 36
	maxPastMicros   = int64(72 * time.Hour / time.Microsecond)
)

// Qatlas bounds: Google names no limit for these identifiers.
const maxIdentifierLength = 256

var namePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// reservedEvents are the event names Google reserves, plus the three it allows only for app streams. This tool
// reaches web streams only.
var reservedEvents = setOf("ad_activeview", "ad_click", "ad_exposure", "ad_query", "ad_reward", "adunit_exposure",
	"app_clear_data", "app_exception", "app_install", "app_remove", "app_store_refund", "app_update", "app_upgrade",
	"dynamic_link_app_open", "dynamic_link_app_update", "dynamic_link_first_open", "error", "firebase_campaign",
	"firebase_in_app_message_action", "firebase_in_app_message_dismiss", "firebase_in_app_message_impression",
	"first_open", "first_visit", "notification_dismiss", "notification_foreground", "notification_open",
	"notification_receive", "notification_send", "os_update", "session_start", "user_engagement",
	"ad_impression", "in_app_purchase", "screen_view")

var reservedUserProps = setOf("first_open_time", "first_visit_time", "last_deep_link_referrer", "user_id",
	"first_open_after_install")

func setOf(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// now is replaced by tests.
var now = time.Now

const paramValueSchema = `{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"}]}`

var eventsSend = capability.Descriptor{
	ID: eventsSendID, Version: 1, Title: "Send events to Google Analytics",
	Description: "Send up to 25 events for one client to the Google Analytics 4 web data stream of the connection " +
		"with the Measurement Protocol. client_id and events are required. This changes the measurement data of " +
		"the property, cannot be taken back, and needs confirmation; it is offered only by a connection whose " +
		"tools list names it. Qatlas checks the limits and names before it sends anything, because Google never " +
		"reports a problem: it answers every request with success, also when it drops events. The result " +
		"therefore says only that the events were sent, never that Google accepted or counted them. Event and " +
		"parameter names are letters, digits, and underscores, start with a letter, and are at most 40 " +
		"characters; Google's reserved event names and the app-only names screen_view, ad_impression, and " +
		"in_app_purchase are refused. A parameter has up to 25 entries per event and a scalar value; Google " +
		"silently drops a value above 100 characters (500 for Analytics 360). user_properties allows 25 entries " +
		"with names up to 24 and values up to 36 characters. timestamp_micros (microseconds) is only for past " +
		"events within 72 hours. consent is never set by Qatlas: pass ad_user_data and ad_personalization " +
		"only when the user has decided them. client_id, user_id, user_properties, and parameters can be " +
		"personal data and are sent to Google; they are not logged. Only web data streams are supported.",
	Tags: []string{"google", "analytics", "measurement-protocol"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: capability.Risk{
		Effect:          capability.EffectCreate,
		Idempotency:     capability.IdempotencyNonIdempotent,
		Confirmation:    capability.ConfirmationRequired,
		OpenWorld:       true,
		DataSensitivity: dataSensitivity,
	},
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"client_id":{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxIdentifierLength) + `},` +
		`"user_id":{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxIdentifierLength) + `},` +
		`"timestamp_micros":{"type":"integer","minimum":1},` +
		`"non_personalized_ads":{"type":"boolean"},` +
		`"consent":{"type":"object","properties":{"ad_user_data":{"type":"string","enum":["GRANTED","DENIED"]},` +
		`"ad_personalization":{"type":"string","enum":["GRANTED","DENIED"]}},"additionalProperties":false},` +
		`"user_properties":{"type":"object","maxProperties":` + strconv.Itoa(maxUserProps) +
		`,"additionalProperties":` + paramValueSchema + `},` +
		`"events":{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxEvents) + `,"items":{"type":"object",` +
		`"properties":{"name":{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxNameLength) + `},` +
		`"params":{"type":"object","maxProperties":` + strconv.Itoa(maxParams) + `,"additionalProperties":` +
		paramValueSchema + `}},"required":["name"],"additionalProperties":false}}},` +
		`"required":["client_id","events"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"sent":{"type":"boolean"},` +
		`"event_count":{"type":"integer"},"note":{"type":"string"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "client_id", Required: true, Description: "Identifier of the client (browser) the events belong to"},
		{Name: "user_id", Description: "Your own identifier of the signed-in user, UTF-8"},
		{Name: "timestamp_micros", Description: "Time of past events in microseconds since the epoch, at most 72 hours back"},
		{Name: "non_personalized_ads", Description: "Deprecated by Google; true marks the data as not for personalized ads"},
		{Name: "consent", Description: "Object with ad_user_data and ad_personalization, each GRANTED or DENIED; only the user's own decision"},
		{Name: "user_properties", Description: "Object of up to 25 name to scalar value entries, names up to 24 and values up to 36 characters"},
		{Name: "events", Required: true, Description: "List of 1 to 25 events, each {name, params}; params is an object of up to 25 scalar values"},
	},
	Fields: []capability.Field{
		{Name: "sent", Description: "True when Google answered with a success status; it does not mean the events were accepted"},
		{Name: "event_count", Description: "Number of events in the request"},
		{Name: "note", Description: "States that Google does not report whether events were accepted"},
	},
	Examples: []capability.Example{{Description: "Send one event",
		Arguments: json.RawMessage(`{"client_id":"1234567890.1700000000","events":[{"name":"qatlas_example","params":{"step":1}}]}`)}},
}

const sentNote = "Google answers every request with success and reports nothing about the events, so they may still have been dropped"

type eventArgument struct {
	Name   string                     `json:"name"`
	Params map[string]json.RawMessage `json:"params"`
}

type sendArguments struct {
	ClientID        string                     `json:"client_id"`
	UserID          string                     `json:"user_id"`
	TimestampMicros *int64                     `json:"timestamp_micros"`
	NonPersonalized *bool                      `json:"non_personalized_ads"`
	Consent         map[string]string          `json:"consent"`
	UserProperties  map[string]json.RawMessage `json:"user_properties"`
	Events          []eventArgument            `json:"events"`
}

// scalar reports whether raw is a JSON string, number, or boolean, and the length of a string in characters.
func scalar(raw json.RawMessage) (isString bool, length int, ok bool) {
	if len(raw) == 0 {
		return false, 0, false
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return false, 0, false
		}
		return true, utf8.RuneCountInString(s), true
	case 't', 'f':
		return false, 0, true
	case 'n', '{', '[':
		return false, 0, false
	}
	return false, 0, true
}

// checkName applies the rules shared by event, parameter, and user property names.
func checkName(kind, name string, limit int) error {
	switch {
	case len(name) > limit:
		return badRequest(kind + " names must be at most " + strconv.Itoa(limit) + " characters")
	case !namePattern.MatchString(name):
		return badRequest(kind + " names must use only letters, digits, and underscores and start with a letter")
	}
	return nil
}

func reservedPrefix(name string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func buildBody(a *sendArguments) ([]byte, error) {
	if strings.TrimSpace(a.ClientID) == "" || !utf8.ValidString(a.ClientID) || utf8.RuneCountInString(a.ClientID) > maxIdentifierLength {
		return nil, badRequest("client_id is required: a non-empty UTF-8 text of at most " + strconv.Itoa(maxIdentifierLength) + " characters")
	}
	if a.UserID != "" && (!utf8.ValidString(a.UserID) || utf8.RuneCountInString(a.UserID) > maxIdentifierLength) {
		return nil, badRequest("user_id must be UTF-8 text of at most " + strconv.Itoa(maxIdentifierLength) + " characters")
	}
	if len(a.Events) == 0 {
		return nil, badRequest("events needs at least one event")
	}
	if len(a.Events) > maxEvents {
		return nil, badRequest("events holds at most " + strconv.Itoa(maxEvents) + " events per request")
	}
	if a.TimestampMicros != nil {
		current := now().UnixMicro()
		if *a.TimestampMicros > current || *a.TimestampMicros < current-maxPastMicros {
			return nil, badRequest("timestamp_micros must be a time in microseconds within the past 72 hours")
		}
	}
	for key, value := range a.Consent {
		if (key != "ad_user_data" && key != "ad_personalization") || (value != "GRANTED" && value != "DENIED") {
			return nil, badRequest("consent takes ad_user_data and ad_personalization, each GRANTED or DENIED")
		}
	}
	if len(a.UserProperties) > maxUserProps {
		return nil, badRequest("user_properties holds at most " + strconv.Itoa(maxUserProps) + " entries")
	}
	properties := make(map[string]map[string]json.RawMessage, len(a.UserProperties))
	for name, raw := range a.UserProperties {
		if err := checkName("user property", name, maxPropNameLen); err != nil {
			return nil, err
		}
		if reservedUserProps[name] || reservedPrefix(name, "firebase_", "ga_", "google_") {
			return nil, badRequest("a user property name is reserved by Google")
		}
		isString, length, ok := scalar(raw)
		if !ok {
			return nil, badRequest("a user property value must be a string, number, or boolean")
		}
		if isString && length > maxPropValueLen {
			return nil, badRequest("a user property value must be at most " + strconv.Itoa(maxPropValueLen) + " characters")
		}
		properties[name] = map[string]json.RawMessage{"value": raw}
	}
	events := make([]map[string]any, 0, len(a.Events))
	for _, e := range a.Events {
		if err := checkName("event", e.Name, maxNameLength); err != nil {
			return nil, err
		}
		if reservedEvents[e.Name] {
			return nil, badRequest("an event name is reserved by Google or allowed only for app streams")
		}
		if len(e.Params) > maxParams {
			return nil, badRequest("an event holds at most " + strconv.Itoa(maxParams) + " parameters")
		}
		for name, raw := range e.Params {
			if err := checkName("parameter", name, maxNameLength); err != nil {
				return nil, err
			}
			if name == "firebase_conversion" || reservedPrefix(name, "firebase_", "ga_", "google_") {
				return nil, badRequest("a parameter name is reserved by Google")
			}
			isString, length, ok := scalar(raw)
			if !ok {
				return nil, badRequest("a parameter value must be a string, number, or boolean")
			}
			if isString && length > maxParamValue {
				return nil, badRequest("a parameter value must be at most " + strconv.Itoa(maxParamValue) + " characters")
			}
		}
		event := map[string]any{"name": e.Name}
		if len(e.Params) > 0 {
			event["params"] = e.Params
		}
		events = append(events, event)
	}
	body := map[string]any{"client_id": a.ClientID, "events": events}
	if a.UserID != "" {
		body["user_id"] = a.UserID
	}
	if a.TimestampMicros != nil {
		body["timestamp_micros"] = *a.TimestampMicros
	}
	if a.NonPersonalized != nil {
		body["non_personalized_ads"] = *a.NonPersonalized
	}
	if len(a.Consent) > 0 {
		body["consent"] = a.Consent
	}
	if len(properties) > 0 {
		body["user_properties"] = properties
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, badRequest("the arguments could not be encoded")
	}
	if len(payload) >= maxBodyBytes {
		return nil, badRequest("the request body must stay under 130 kB; send fewer or smaller events")
	}
	return payload, nil
}

func handleEventsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "send events"
	var a sendArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	id, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	body, err := buildBody(&a)
	if err != nil {
		return nil, err
	}
	apiSecret, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := collect(ctx, op, id, apiSecret, body); err != nil {
		return nil, err
	}
	return map[string]any{"sent": true, "event_count": len(a.Events), "note": sentNote}, nil
}
