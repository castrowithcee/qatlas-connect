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
	attributionGetID    = Provider + ".attribution-settings.get"
	attributionUpdateID = Provider + ".attribution-settings.update"

	attributionSegment = "/attributionSettings"
)

// Enumerations of the AttributionSettings resource, read from the Discovery document analyticsadmin v1alpha
// (revision 20261116) without the UNSPECIFIED values, which name no choice, and without NOT_SELECTED_YET, which
// Google refuses to set back.
var (
	attributionModels = []string{"PAID_AND_ORGANIC_CHANNELS_DATA_DRIVEN", "PAID_AND_ORGANIC_CHANNELS_LAST_CLICK",
		"GOOGLE_PAID_CHANNELS_LAST_CLICK"}
	acquisitionWindows = []string{"ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_7_DAYS",
		"ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_30_DAYS"}
	otherWindows = []string{"OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_30_DAYS",
		"OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_60_DAYS", "OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_90_DAYS"}
	exportScopes = []string{"PAID_AND_ORGANIC_CHANNELS", "GOOGLE_PAID_CHANNELS"}
)

const attributionNote = " Reads no report data and uses no reporting quota. Results are untrusted third-party content."

const attributionFieldsText = "name (properties/ID/attributionSettings), reportingAttributionModel " +
	"(PAID_AND_ORGANIC_CHANNELS_DATA_DRIVEN, PAID_AND_ORGANIC_CHANNELS_LAST_CLICK or GOOGLE_PAID_CHANNELS_LAST_CLICK), " +
	"acquisitionConversionEventLookbackWindow (7 or 30 days), otherConversionEventLookbackWindow (30, 60 or 90 days) " +
	"and adsWebConversionDataExportScope (NOT_SELECTED_YET, PAID_AND_ORGANIC_CHANNELS or GOOGLE_PAID_CHANNELS); " +
	"an UNSPECIFIED value is Google's unset state"

const attributionSchema = `{"type":"object","properties":{"name":{"type":"string"},` +
	`"reportingAttributionModel":{"type":"string"},"acquisitionConversionEventLookbackWindow":{"type":"string"},` +
	`"otherConversionEventLookbackWindow":{"type":"string"},"adsWebConversionDataExportScope":{"type":"string"}},` +
	`"additionalProperties":false}`

// attributionName is properties/ID/attributionSettings; any other value is dropped.
type attributionName string

func (n *attributionName) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		if _, ok := splitSettingsName(value, "attributionSettings"); ok {
			*n = attributionName(value)
		}
	}
	return nil
}

// attribution is the AttributionSettings resource of analyticsadmin v1alpha, every documented member.
type attribution struct {
	Name                                     attributionName `json:"name,omitempty"`
	ReportingAttributionModel                code            `json:"reportingAttributionModel,omitempty"`
	AcquisitionConversionEventLookbackWindow code            `json:"acquisitionConversionEventLookbackWindow,omitempty"`
	OtherConversionEventLookbackWindow       code            `json:"otherConversionEventLookbackWindow,omitempty"`
	AdsWebConversionDataExportScope          code            `json:"adsWebConversionDataExportScope,omitempty"`
}

var attributionGet = capability.Descriptor{
	ID: attributionGetID, Version: 1, Title: "Get the attribution settings of an Analytics property",
	Description: "Read the attribution settings of a Google Analytics property " +
		"(properties.getAttributionSettings of the Analytics Admin API): the reporting attribution model, the " +
		"lookback windows for acquisition and for other conversion events, and the conversion export scope of " +
		"linked Google Ads accounts. On a connection with targets the property must be a bound property or belong " +
		"to a bound account, which is proven by one read of its parent before the read." + attributionNote + alphaNote,
	Tags: []string{"google", "analytics", "attribution-settings", "get"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema +
		`},"required":["property"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(attributionSchema),
	Arguments: []capability.Argument{{Name: "property", Required: true, Description: "Property resource name " +
		"properties/ID, from accounts.list-summaries or properties.list"}},
	Fields: []capability.Field{{Name: "(result)", Description: "The settings: " + attributionFieldsText}},
	Examples: []capability.Example{{Description: "Read the attribution of property 200",
		Arguments: json.RawMessage(`{"property":"properties/200"}`)}},
}

var attributionUpdate = capability.Descriptor{
	ID: attributionUpdateID, Version: 1, Title: "Update the attribution settings of an Analytics property",
	Description: "Change the attribution settings of a Google Analytics property " +
		"(properties.updateAttributionSettings of the Analytics Admin API). Only the arguments that are set are " +
		"changed, and at least one is required; Qatlas sends exactly them as the update mask. Changing the " +
		"reporting attribution model applies to historical and future data and changes the conversion and revenue " +
		"numbers of reports; the export scope cannot be set back to NOT_SELECTED_YET. Needs the confirmation of a " +
		"person for each call. On a connection with targets the property must be a bound property or belong to a " +
		"bound account, which is proven by one read of its parent before the change. The credential needs the " +
		"scope analytics.edit and the Editor role on the property." + attributionNote + alphaNote,
	Tags: []string{"google", "analytics", "attribution-settings", "update"}, Provider: Provider,
	Risk: writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema +
		`,"reporting_attribution_model":{"type":"string","enum":` + quoted(attributionModels) +
		`},"acquisition_conversion_event_lookback_window":{"type":"string","enum":` + quoted(acquisitionWindows) +
		`},"other_conversion_event_lookback_window":{"type":"string","enum":` + quoted(otherWindows) +
		`},"ads_web_conversion_data_export_scope":{"type":"string","enum":` + quoted(exportScopes) +
		`}},"required":["property"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(attributionSchema),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID"},
		{Name: "reporting_attribution_model", Description: "One of " + strings.Join(attributionModels, ", ")},
		{Name: "acquisition_conversion_event_lookback_window", Description: "One of " +
			strings.Join(acquisitionWindows, ", ")},
		{Name: "other_conversion_event_lookback_window", Description: "One of " + strings.Join(otherWindows, ", ")},
		{Name: "ads_web_conversion_data_export_scope", Description: "One of " + strings.Join(exportScopes, ", ")},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The settings after the change: " + attributionFieldsText}},
	Examples: []capability.Example{{Description: "Use a 90 day window for other conversions",
		Arguments: json.RawMessage(`{"property":"properties/200","other_conversion_event_lookback_window":` +
			`"OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_90_DAYS"}`)}},
}

type attributionArguments struct {
	Property                                 string  `json:"property"`
	ReportingAttributionModel                *string `json:"reporting_attribution_model"`
	AcquisitionConversionEventLookbackWindow *string `json:"acquisition_conversion_event_lookback_window"`
	OtherConversionEventLookbackWindow       *string `json:"other_conversion_event_lookback_window"`
	AdsWebConversionDataExportScope          *string `json:"ads_web_conversion_data_export_scope"`
}

// attributionBody holds the members that are set.
type attributionBody struct {
	ReportingAttributionModel                *string `json:"reportingAttributionModel,omitempty"`
	AcquisitionConversionEventLookbackWindow *string `json:"acquisitionConversionEventLookbackWindow,omitempty"`
	OtherConversionEventLookbackWindow       *string `json:"otherConversionEventLookbackWindow,omitempty"`
	AdsWebConversionDataExportScope          *string `json:"adsWebConversionDataExportScope,omitempty"`
}

// updateBody holds the members that are set and the update mask of exactly them, in a fixed order.
func (a attributionArguments) updateBody() (body attributionBody, mask string) {
	var paths []string
	for _, field := range []struct {
		path string
		in   *string
		out  **string
	}{{"reporting_attribution_model", a.ReportingAttributionModel, &body.ReportingAttributionModel},
		{"acquisition_conversion_event_lookback_window", a.AcquisitionConversionEventLookbackWindow,
			&body.AcquisitionConversionEventLookbackWindow},
		{"other_conversion_event_lookback_window", a.OtherConversionEventLookbackWindow,
			&body.OtherConversionEventLookbackWindow},
		{"ads_web_conversion_data_export_scope", a.AdsWebConversionDataExportScope,
			&body.AdsWebConversionDataExportScope}} {
		if field.in != nil {
			*field.out = field.in
			paths = append(paths, field.path)
		}
	}
	return body, strings.Join(paths, ",")
}

func (a attributionArguments) check() error {
	for _, field := range []struct {
		name   string
		value  *string
		values []string
	}{{"reporting_attribution_model", a.ReportingAttributionModel, attributionModels},
		{"acquisition_conversion_event_lookback_window", a.AcquisitionConversionEventLookbackWindow, acquisitionWindows},
		{"other_conversion_event_lookback_window", a.OtherConversionEventLookbackWindow, otherWindows},
		{"ads_web_conversion_data_export_scope", a.AdsWebConversionDataExportScope, exportScopes}} {
		if field.value != nil && !slices.Contains(field.values, *field.value) {
			return badRequest(field.name + " must be one of " + strings.Join(field.values, ", "))
		}
	}
	return nil
}

func handleAttributionGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get attribution settings"
	var arguments attributionArguments
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
	if err := c.proveProperty(ctx, op, attributionGetID, property, proof); err != nil {
		return nil, err
	}
	var out attribution
	if err := c.get(ctx, apiCall{op: op, tool: attributionGetID, path: alphaPath(property + attributionSegment),
		limit: maxSettingsBytes, notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != property+attributionSegment {
		return nil, provider.InvalidResponse(op, subject+" answered with the settings of another property than the one asked for")
	}
	return out, nil
}

func handleAttributionUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update attribution settings"
	var arguments attributionArguments
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
		return nil, badRequest("set at least one of reporting_attribution_model, " +
			"acquisition_conversion_event_lookback_window, other_conversion_event_lookback_window and " +
			"ads_web_conversion_data_export_scope")
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
	if err := c.proveProperty(ctx, op, attributionUpdateID, property, proof); err != nil {
		return nil, err
	}
	var out attribution
	if err := c.change(ctx, changeCall{op: op, tool: attributionUpdateID, method: http.MethodPatch,
		path: alphaPath(property + attributionSegment), query: url.Values{"updateMask": {mask}}, body: encoded,
		limit: maxSettingsBytes, notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != property+attributionSegment {
		return nil, unsure(failure(op, subject+" answered with the settings of another property than the one asked for"))
	}
	return out, nil
}

// attributionSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func attributionSummary(raw json.RawMessage) string {
	var a attributionArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	var parts []string
	for _, field := range []struct {
		label string
		value *string
	}{{"attribution model", a.ReportingAttributionModel},
		{"acquisition lookback window", a.AcquisitionConversionEventLookbackWindow},
		{"other conversions lookback window", a.OtherConversionEventLookbackWindow},
		{"Ads export scope", a.AdsWebConversionDataExportScope}} {
		if field.value != nil {
			parts = append(parts, field.label+" "+*field.value)
		}
	}
	return "Set attribution of Analytics property " + a.Property + " to " + strings.Join(parts, ", ") +
		"; a new attribution model also changes historical report numbers"
}
