package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	dataRetentionGetID    = Provider + ".data-retention.get"
	dataRetentionUpdateID = Provider + ".data-retention.update"

	retentionSegment = "/dataRetentionSettings"
)

// Enumerations of the DataRetentionSettings resource, read from the Discovery document analyticsadmin v1beta
// (revision 20261115) without RETENTION_DURATION_UNSPECIFIED, which names no duration. Google documents the
// durations of 26, 38 and 50 months for event data of Analytics 360 properties only, and user data as two or
// fourteen months.
var (
	eventRetentions = []string{"TWO_MONTHS", "FOURTEEN_MONTHS", "TWENTY_SIX_MONTHS", "THIRTY_EIGHT_MONTHS",
		"FIFTY_MONTHS"}
	userRetentions = []string{"TWO_MONTHS", "FOURTEEN_MONTHS"}
)

const retentionNote = " Reads no report data and uses no reporting quota. Results are untrusted third-party content."

const retentionFieldsText = "name (properties/ID/dataRetentionSettings), eventDataRetention and userDataRetention " +
	"(TWO_MONTHS, FOURTEEN_MONTHS and, for event data of Analytics 360 properties, TWENTY_SIX_MONTHS, " +
	"THIRTY_EIGHT_MONTHS or FIFTY_MONTHS) and resetUserDataOnNewActivity (false when Google leaves it out)"

const retentionSchema = `{"type":"object","properties":{"name":{"type":"string"},"eventDataRetention":` +
	`{"type":"string"},"userDataRetention":{"type":"string"},"resetUserDataOnNewActivity":{"type":"boolean"}},` +
	`"additionalProperties":false}`

// retentionName is properties/ID/dataRetentionSettings; any other value is dropped.
type retentionName string

func (n *retentionName) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		if _, ok := splitSettingsName(value, "dataRetentionSettings"); ok {
			*n = retentionName(value)
		}
	}
	return nil
}

// splitSettingsName returns the property of a name of the form properties/ID/<leaf>.
func splitSettingsName(value, leaf string) (property string, ok bool) {
	parts := strings.Split(value, "/")
	if len(parts) != 3 || parts[0] != "properties" || parts[2] != leaf || !validID(parts[1]) {
		return "", false
	}
	return propertyPrefix + parts[1], true
}

// dataRetention is the DataRetentionSettings resource of analyticsadmin v1beta, every documented member.
type dataRetention struct {
	Name                       retentionName `json:"name,omitempty"`
	EventDataRetention         code          `json:"eventDataRetention,omitempty"`
	UserDataRetention          code          `json:"userDataRetention,omitempty"`
	ResetUserDataOnNewActivity bool          `json:"resetUserDataOnNewActivity"`
}

var dataRetentionGet = capability.Descriptor{
	ID: dataRetentionGetID, Version: 1, Title: "Get the data retention settings of an Analytics property",
	Description: "Read the data retention settings of a Google Analytics property " +
		"(properties.getDataRetentionSettings of the Analytics Admin API): how long event-level data and " +
		"user-level data are kept, and whether the retention period of a user is reset by each new event of that " +
		"user. On a connection with targets the property must be a bound property or belong to a bound account, " +
		"which is proven by one read of its parent before the read." + retentionNote,
	Tags: []string{"google", "analytics", "data-retention", "get"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema +
		`},"required":["property"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(retentionSchema),
	Arguments: []capability.Argument{{Name: "property", Required: true, Description: "Property resource name " +
		"properties/ID, from accounts.list-summaries or properties.list"}},
	Fields:   []capability.Field{{Name: "(result)", Description: "The settings: " + retentionFieldsText}},
	Examples: []capability.Example{{Description: "Read the retention of property 200", Arguments: json.RawMessage(`{"property":"properties/200"}`)}},
}

var dataRetentionUpdate = capability.Descriptor{
	ID: dataRetentionUpdateID, Version: 1, Title: "Update the data retention settings of an Analytics property",
	Description: "Change the data retention settings of a Google Analytics property " +
		"(properties.updateDataRetentionSettings of the Analytics Admin API). Only the arguments that are set are " +
		"changed, and at least one is required; Qatlas sends exactly them as the update mask. DESTRUCTIVE: " +
		"shortening a retention period deletes the data older than the new period permanently, and the tool cannot " +
		"tell beforehand whether a value shortens it, so every call is treated as destructive. Needs the " +
		"confirmation of a person for each call and is offered only where a connection's tools list names it; it " +
		"is in no profile. Event data of 26, 38 and 50 months is available to Analytics 360 properties only. On a " +
		"connection with targets the property must be a bound property or belong to a bound account, which is " +
		"proven by one read of its parent before the change. The credential needs the scope analytics.edit and the " +
		"Editor role on the property." + retentionNote,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "analytics", "data-retention", "update"}, Provider: Provider,
	Risk: writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema +
		`,"event_data_retention":{"type":"string","enum":` + quoted(eventRetentions) +
		`},"user_data_retention":{"type":"string","enum":` + quoted(userRetentions) +
		`},"reset_user_data_on_new_activity":{"type":"boolean"}},"required":["property"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(retentionSchema),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID"},
		{Name: "event_data_retention", Description: "One of " + strings.Join(eventRetentions, ", ") +
			"; the last three need an Analytics 360 property"},
		{Name: "user_data_retention", Description: "One of " + strings.Join(userRetentions, ", ")},
		{Name: "reset_user_data_on_new_activity", Description: "true resets the retention period of a user with " +
			"every event of that user"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The settings after the change: " + retentionFieldsText}},
	Examples: []capability.Example{{Description: "Keep event data for 14 months",
		Arguments: json.RawMessage(`{"property":"properties/200","event_data_retention":"FOURTEEN_MONTHS"}`)}},
}

type retentionArguments struct {
	Property                   string  `json:"property"`
	EventDataRetention         *string `json:"event_data_retention"`
	UserDataRetention          *string `json:"user_data_retention"`
	ResetUserDataOnNewActivity *bool   `json:"reset_user_data_on_new_activity"`
}

// retentionBody holds the members that are set.
type retentionBody struct {
	EventDataRetention         *string `json:"eventDataRetention,omitempty"`
	UserDataRetention          *string `json:"userDataRetention,omitempty"`
	ResetUserDataOnNewActivity *bool   `json:"resetUserDataOnNewActivity,omitempty"`
}

// updateBody holds the members that are set and the update mask of exactly them, in a fixed order.
func (a retentionArguments) updateBody() (body retentionBody, mask string) {
	var paths []string
	if a.EventDataRetention != nil {
		body.EventDataRetention = a.EventDataRetention
		paths = append(paths, "event_data_retention")
	}
	if a.UserDataRetention != nil {
		body.UserDataRetention = a.UserDataRetention
		paths = append(paths, "user_data_retention")
	}
	if a.ResetUserDataOnNewActivity != nil {
		body.ResetUserDataOnNewActivity = a.ResetUserDataOnNewActivity
		paths = append(paths, "reset_user_data_on_new_activity")
	}
	return body, strings.Join(paths, ",")
}

func (a retentionArguments) check() error {
	if a.EventDataRetention != nil && !slices.Contains(eventRetentions, *a.EventDataRetention) {
		return badRequest("event_data_retention must be one of " + strings.Join(eventRetentions, ", "))
	}
	if a.UserDataRetention != nil && !slices.Contains(userRetentions, *a.UserDataRetention) {
		return badRequest("user_data_retention must be one of " + strings.Join(userRetentions, ", "))
	}
	return nil
}

func handleDataRetentionGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get data retention settings"
	var arguments retentionArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, dataRetentionGetID, property, proof); err != nil {
		return nil, err
	}
	var out dataRetention
	if err := c.get(ctx, apiCall{op: op, tool: dataRetentionGetID, path: "/v1beta/" + property + retentionSegment,
		limit: maxSettingsBytes, notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != property+retentionSegment {
		return nil, provider.InvalidResponse(op, subject+" answered with the settings of another property than the one asked for")
	}
	return out, nil
}

func handleDataRetentionUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update data retention settings"
	var arguments retentionArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	body, mask := arguments.updateBody()
	if mask == "" {
		return nil, badRequest("set at least one of event_data_retention, user_data_retention and " +
			"reset_user_data_on_new_activity")
	}
	if err := arguments.check(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the settings could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, dataRetentionUpdateID, property, proof); err != nil {
		return nil, err
	}
	var out dataRetention
	if err := c.change(ctx, changeCall{op: op, tool: dataRetentionUpdateID, method: http.MethodPatch,
		path: "/v1beta/" + property + retentionSegment, query: url.Values{"updateMask": {mask}}, body: encoded,
		limit: maxSettingsBytes, notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != property+retentionSegment {
		return nil, unsure(failure(op, subject+" answered with the settings of another property than the one asked for"))
	}
	return out, nil
}

// retentionSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func retentionSummary(raw json.RawMessage) string {
	var a retentionArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	var parts []string
	if a.EventDataRetention != nil {
		parts = append(parts, "event data "+*a.EventDataRetention)
	}
	if a.UserDataRetention != nil {
		parts = append(parts, "user data "+*a.UserDataRetention)
	}
	if a.ResetUserDataOnNewActivity != nil {
		parts = append(parts, "reset on new activity "+map[bool]string{true: "on", false: "off"}[*a.ResetUserDataOnNewActivity])
	}
	return "Set data retention of Analytics property " + a.Property + " to " + strings.Join(parts, ", ") +
		"; a shorter period permanently deletes older data"
}
