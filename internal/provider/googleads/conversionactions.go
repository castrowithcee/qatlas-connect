package googleads

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	conversionActionsCreateID = Provider + ".conversion-actions.create"
	conversionActionsUpdateID = Provider + ".conversion-actions.update"
	conversionActionsRemoveID = Provider + ".conversion-actions.remove"

	conversionActionService = "/conversionActions"

	// maxConversionActionOperations is the fixed bound of operations in one request. Google allows far more; a
	// conversion action changes what campaigns optimize for, so a call stays small enough to review.
	maxConversionActionOperations = 20

	maxConversionNameLength = 200
	maxPhoneCallSeconds     = 10_000
	maxClickLookbackDays    = 90
	maxCallClickLookback    = 60
	maxViewLookbackDays     = 30
)

var conversionActionIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// The values of the enums in v26 that a request may name. UNSPECIFIED and UNKNOWN are no values to send.
//
// Types: only the ones that stand for no link to a third party. Firebase, third-party app analytics, Google Play
// and app conversions bind the account to an app or a foreign analytics platform, so they are not created here;
// the types Google creates itself (Google Analytics 4, Floodlight, lead form, store visits, smart campaign) are
// read-only in the API as well.
var (
	conversionActionCreateTypes = []string{"WEBPAGE", "UPLOAD_CLICKS", "UPLOAD_CALLS", "AD_CALL", "WEBSITE_CALL"}
	callConversionTypes         = []string{"AD_CALL", "WEBSITE_CALL"}
	// REMOVED is left to the remove tool, which carries the delete effect.
	conversionActionStatuses = []string{"ENABLED", "HIDDEN"}
	conversionCategories     = []string{"DEFAULT", "PAGE_VIEW", "PURCHASE", "SIGNUP", "DOWNLOAD", "ADD_TO_CART",
		"BEGIN_CHECKOUT", "SUBSCRIBE_PAID", "PHONE_CALL_LEAD", "IMPORTED_LEAD", "SUBMIT_LEAD_FORM", "BOOK_APPOINTMENT",
		"REQUEST_QUOTE", "GET_DIRECTIONS", "OUTBOUND_CLICK", "CONTACT", "ENGAGEMENT", "STORE_VISIT", "STORE_SALE",
		"QUALIFIED_LEAD", "CONVERTED_LEAD", "YOUTUBE_FOLLOW_ON_VIEWS", "MESSAGE_LEAD", "IN_APP_AD_REVENUE"}
	conversionCountingTypes = []string{"ONE_PER_CLICK", "MANY_PER_CLICK"}
	// The rule-based models are refused by Google (CANNOT_SET_RULE_BASED_ATTRIBUTION_MODELS) and EXTERNAL is
	// for imported attribution, so only the two models the reference names for new settings are offered.
	conversionAttributionModels = []string{"GOOGLE_ADS_LAST_CLICK", "GOOGLE_SEARCH_ATTRIBUTION_DATA_DRIVEN"}
)

type (
	conversionValueSettings struct {
		DefaultValue          *float64 `json:"default_value"`
		DefaultCurrencyCode   *string  `json:"default_currency_code"`
		AlwaysUseDefaultValue *bool    `json:"always_use_default_value"`
	}
	// conversionActionFields are the settings a create or an update may carry. A pointer that is nil was not given.
	conversionActionFields struct {
		Name                     *string                  `json:"name"`
		Status                   string                   `json:"status"`
		Category                 string                   `json:"category"`
		PrimaryForGoal           *bool                    `json:"primary_for_goal"`
		ClickThroughLookbackDays *int64                   `json:"click_through_lookback_window_days"`
		ViewThroughLookbackDays  *int64                   `json:"view_through_lookback_window_days"`
		ValueSettings            *conversionValueSettings `json:"value_settings"`
		CountingType             string                   `json:"counting_type"`
		AttributionModel         string                   `json:"attribution_model"`
		PhoneCallDurationSeconds *int64                   `json:"phone_call_duration_seconds"`
	}
	conversionActionCreateItem struct {
		conversionActionFields
		Type string `json:"type"`
	}
	conversionActionUpdateItem struct {
		conversionActionFields
		ConversionActionID string `json:"conversion_action_id"`
	}
	conversionActionsArguments struct {
		CustomerID          string          `json:"customer_id"`
		ConversionActions   json.RawMessage `json:"conversion_actions"`
		ConversionActionIDs []string        `json:"conversion_action_ids"`
		ValidateOnly        bool            `json:"validate_only"`
		ResponseContentType string          `json:"response_content_type"`
	}
)

func conversionActionName(customerID, id string) string {
	return customerName(customerID) + conversionActionService + "/" + id
}

// conversionActionIDOf returns the numeric ID of a resource name of the account, or false.
func conversionActionIDOf(name, customerID string) (string, bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+conversionActionService+"/")
	if !ok || !conversionActionIDPattern.MatchString(rest) {
		return "", false
	}
	_, err := strconv.ParseInt(rest, 10, 64)
	return rest, err == nil
}

func checkConversionActionID(argument, value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !conversionActionIDPattern.MatchString(value) {
		return badRequest(argument + " must be the numeric ID of an existing conversion action")
	}
	return nil
}

func inRange(value *int64, low, high int64) bool {
	return value == nil || (*value >= low && *value <= high)
}

// check refuses what no request may start with: values outside the ranges the reference names and enum values
// that this tool does not offer. Every other rule, such as which model a type accepts, is Google's to validate.
func (f conversionActionFields) check(callType bool) error {
	if f.Name != nil {
		if n := utf8.RuneCountInString(*f.Name); n < 1 || n > maxConversionNameLength || strings.TrimSpace(*f.Name) == "" ||
			strings.IndexFunc(*f.Name, unicode.IsControl) >= 0 {
			return badRequest("name must be 1 to 200 characters without control characters")
		}
	}
	if f.Status != "" && !slices.Contains(conversionActionStatuses, f.Status) {
		return badRequest("status may be ENABLED or HIDDEN; a conversion action is removed with conversion-actions.remove")
	}
	if f.Category != "" && !slices.Contains(conversionCategories, f.Category) {
		return badRequest("category must be a ConversionActionCategory of the API")
	}
	if f.CountingType != "" && !slices.Contains(conversionCountingTypes, f.CountingType) {
		return badRequest("counting_type must be ONE_PER_CLICK or MANY_PER_CLICK")
	}
	if f.AttributionModel != "" && !slices.Contains(conversionAttributionModels, f.AttributionModel) {
		return badRequest("attribution_model must be GOOGLE_ADS_LAST_CLICK or GOOGLE_SEARCH_ATTRIBUTION_DATA_DRIVEN")
	}
	clickMax := int64(maxClickLookbackDays)
	if callType {
		clickMax = maxCallClickLookback
	}
	if !inRange(f.ClickThroughLookbackDays, 1, clickMax) {
		return badRequest("click_through_lookback_window_days must be 1 to 90, 1 to 60 for call types")
	}
	if !inRange(f.ViewThroughLookbackDays, 1, maxViewLookbackDays) {
		return badRequest("view_through_lookback_window_days must be 1 to 30")
	}
	if callType && f.ViewThroughLookbackDays != nil {
		return badRequest("view_through_lookback_window_days cannot be set on a call type")
	}
	if !inRange(f.PhoneCallDurationSeconds, 0, maxPhoneCallSeconds) {
		return badRequest("phone_call_duration_seconds must be 0 to 10000")
	}
	if v := f.ValueSettings; v != nil {
		if v.DefaultValue == nil && v.DefaultCurrencyCode == nil && v.AlwaysUseDefaultValue == nil {
			return badRequest("value_settings names at least one setting")
		}
		if v.DefaultValue != nil && (math.IsNaN(*v.DefaultValue) || math.IsInf(*v.DefaultValue, 0) || *v.DefaultValue < 0) {
			return badRequest("default_value must be a number of 0 or more")
		}
		if v.DefaultCurrencyCode != nil && !currencyPattern.MatchString(*v.DefaultCurrencyCode) {
			return badRequest("default_currency_code must be a three-letter ISO 4217 code in capitals")
		}
	}
	return nil
}

// wire builds the resource members and the mask paths of the given settings.
func (f conversionActionFields) wire(resource map[string]any) []string {
	var mask []string
	add := func(path, member string, value any) {
		resource[member] = value
		mask = append(mask, path)
	}
	if f.Name != nil {
		add("name", "name", *f.Name)
	}
	if f.Status != "" {
		add("status", "status", f.Status)
	}
	if f.Category != "" {
		add("category", "category", f.Category)
	}
	if f.PrimaryForGoal != nil {
		add("primaryForGoal", "primaryForGoal", *f.PrimaryForGoal)
	}
	if f.ClickThroughLookbackDays != nil {
		add("clickThroughLookbackWindowDays", "clickThroughLookbackWindowDays", *f.ClickThroughLookbackDays)
	}
	if f.ViewThroughLookbackDays != nil {
		add("viewThroughLookbackWindowDays", "viewThroughLookbackWindowDays", *f.ViewThroughLookbackDays)
	}
	if v := f.ValueSettings; v != nil {
		value := map[string]any{}
		resource["valueSettings"] = value
		if v.DefaultValue != nil {
			value["defaultValue"] = *v.DefaultValue
			mask = append(mask, "valueSettings.defaultValue")
		}
		if v.DefaultCurrencyCode != nil {
			value["defaultCurrencyCode"] = *v.DefaultCurrencyCode
			mask = append(mask, "valueSettings.defaultCurrencyCode")
		}
		if v.AlwaysUseDefaultValue != nil {
			value["alwaysUseDefaultValue"] = *v.AlwaysUseDefaultValue
			mask = append(mask, "valueSettings.alwaysUseDefaultValue")
		}
	}
	if f.CountingType != "" {
		add("countingType", "countingType", f.CountingType)
	}
	if f.AttributionModel != "" {
		resource["attributionModelSettings"] = map[string]any{"attributionModel": f.AttributionModel}
		mask = append(mask, "attributionModelSettings.attributionModel")
	}
	if f.PhoneCallDurationSeconds != nil {
		add("phoneCallDurationSeconds", "phoneCallDurationSeconds", *f.PhoneCallDurationSeconds)
	}
	return mask
}

// --- schemas ---

var (
	conversionFieldProperties = func() schema {
		return schema{
			"name":                               schema{"type": "string", "minLength": 1, "maxLength": maxConversionNameLength},
			"status":                             schema{"type": "string", "enum": conversionActionStatuses},
			"category":                           schema{"type": "string", "enum": conversionCategories},
			"primary_for_goal":                   booleanSchema,
			"click_through_lookback_window_days": schema{"type": "integer", "minimum": 1, "maximum": maxClickLookbackDays},
			"view_through_lookback_window_days":  schema{"type": "integer", "minimum": 1, "maximum": maxViewLookbackDays},
			"value_settings": object(schema{"default_value": schema{"type": "number", "minimum": 0},
				"default_currency_code":    schema{"type": "string", "pattern": currencyPattern.String()},
				"always_use_default_value": booleanSchema}),
			"counting_type":               schema{"type": "string", "enum": conversionCountingTypes},
			"attribution_model":           schema{"type": "string", "enum": conversionAttributionModels},
			"phone_call_duration_seconds": schema{"type": "integer", "minimum": 0, "maximum": maxPhoneCallSeconds},
		}
	}

	conversionCreateItemSchema = func() schema {
		p := conversionFieldProperties()
		p["type"] = schema{"type": "string", "enum": conversionActionCreateTypes}
		return object(p, "name", "type")
	}()
	conversionUpdateItemSchema = func() schema {
		p := conversionFieldProperties()
		p["conversion_action_id"] = schema{"type": "string", "pattern": conversionActionIDPattern.String()}
		s := object(p, "conversion_action_id")
		s["minProperties"] = 2
		return s
	}()

	tagSnippetSchema = object(schema{"type": stringSchema, "page_format": stringSchema, "global_site_tag": stringSchema,
		"event_snippet": stringSchema})

	conversionActionResourceSchema = object(schema{
		"resource_name": stringSchema, "id": stringSchema, "name": stringSchema, "status": stringSchema,
		"type": stringSchema, "origin": stringSchema, "primary_for_goal": booleanSchema, "category": stringSchema,
		"owner_customer": stringSchema, "include_in_conversions_metric": booleanSchema,
		"click_through_lookback_window_days": integerSchema, "view_through_lookback_window_days": integerSchema,
		"value_settings": object(schema{"default_value": numberSchema, "default_currency_code": stringSchema,
			"always_use_default_value": booleanSchema}),
		"counting_type": stringSchema,
		"attribution_model_settings": object(schema{"attribution_model": stringSchema,
			"data_driven_model_status": stringSchema}),
		"tag_snippets":                arrayOf(tagSnippetSchema),
		"phone_call_duration_seconds": integerSchema, "app_id": stringSchema, "mobile_app_vendor": stringSchema,
		"firebase_settings": object(schema{"event_name": stringSchema, "project_id": stringSchema,
			"property_id": stringSchema, "property_name": stringSchema}),
		"third_party_app_analytics_settings": object(schema{"event_name": stringSchema, "provider_name": stringSchema}),
		"google_analytics_4_settings": object(schema{"event_name": stringSchema, "property_name": stringSchema,
			"property_id": stringSchema}),
	})

	conversionOutputSchema = encoded(object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"conversion_action_id": stringSchema, "resource_name": stringSchema,
			"conversion_action": conversionActionResourceSchema}, "conversion_action_id", "resource_name"))},
		"customer_id", "validate_only", "results"))
)

func conversionInputSchema(items schema, idList bool) json.RawMessage {
	properties := schema{"customer_id": customerIDArgument, "validate_only": validateOnlySchema,
		"response_content_type": responseContentTypeSchema}
	required := []string{"customer_id"}
	if idList {
		properties["conversion_action_ids"] = schema{"type": "array", "minItems": 1,
			"maxItems": maxConversionActionOperations, "uniqueItems": true,
			"items": schema{"type": "string", "pattern": conversionActionIDPattern.String()}}
		delete(properties, "response_content_type")
		required = append(required, "conversion_action_ids")
	} else {
		properties["conversion_actions"] = schema{"type": "array", "minItems": 1,
			"maxItems": maxConversionActionOperations, "items": items}
		required = append(required, "conversion_actions")
	}
	return encoded(object(properties, required...))
}

var (
	customerArgumentDoc = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer ID " +
		"of the account that owns the conversion actions, without hyphens; a target of the connection or an account " +
		"below a manager target. A manager account needs a manager target that names it"}

	settingsDoc = "name (1 to 200 characters), status (ENABLED or HIDDEN), category (a ConversionActionCategory such " +
		"as PURCHASE or SIGNUP), primary_for_goal (false makes the action non-biddable for the goals of every " +
		"campaign), click_through_lookback_window_days (1 to 90, 1 to 60 for AD_CALL and WEBSITE_CALL), " +
		"view_through_lookback_window_days (1 to 30, not for call types), value_settings (default_value of 0 or " +
		"more, default_currency_code as three capital letters, always_use_default_value), counting_type " +
		"(ONE_PER_CLICK or MANY_PER_CLICK), attribution_model (GOOGLE_ADS_LAST_CLICK or " +
		"GOOGLE_SEARCH_ATTRIBUTION_DATA_DRIVEN) and phone_call_duration_seconds (0 to 10000)"

	conversionResponseDoc = []capability.Argument{validateOnlyArgument, responseContentTypeArgument}

	conversionFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the request: results is then empty"},
		{Name: "results", Description: "One entry per operation, in the order of the request: conversion_action_id, " +
			"resource_name (customers/ID/conversionActions/ID, as Google answered, always of the account of the call), " +
			"and with MUTABLE_RESOURCE the conversion_action as Google returns it: id, name, status, type, origin, " +
			"primary_for_goal, category, owner_customer, include_in_conversions_metric, the lookback windows, " +
			"value_settings, counting_type, attribution_model_settings, tag_snippets, phone_call_duration_seconds, " +
			"app_id, mobile_app_vendor and the Firebase, third-party app analytics and Google Analytics 4 settings " +
			"when Google sends them (they are read-only)"},
	}
)

const conversionNote = " The settings change what campaigns optimize for: a conversion action that is primary for its " +
	"goal feeds the bidding of every campaign that uses the goal. Up to 20 operations go in one atomic request " +
	"without partial_failure: Google applies all of them or none. The tool offers no Firebase, third-party app " +
	"analytics, Google Play or app conversion types and no app_id, because they bind the account to an app or a " +
	"foreign analytics platform; include_in_conversions_metric is read-only in the API and not offered either. " +
	"Conversion actions are read with reports.search on conversion_action."

// --- descriptors ---

var conversionActionsCreate = capability.Descriptor{
	ID: conversionActionsCreateID, Version: 1, Title: "Create conversion actions of a Google Ads account",
	Description: "Create one to 20 conversion actions in a Google Ads account (ConversionActionService." +
		"MutateConversionActions with create operations). Each needs a name and a type: WEBPAGE, UPLOAD_CLICKS, " +
		"UPLOAD_CALLS, AD_CALL or WEBSITE_CALL; the type cannot be changed later. Optional: " + settingsDoc + "." +
		conversionNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent,
		"conversion measurement settings of a Google Ads account"),
	InputSchema: conversionInputSchema(conversionCreateItemSchema, false), OutputSchema: conversionOutputSchema,
	Arguments: append([]capability.Argument{customerArgumentDoc,
		{Name: "conversion_actions", Required: true, Description: "One to 20 conversion actions to create, each with " +
			"name and type (WEBPAGE, UPLOAD_CLICKS, UPLOAD_CALLS, AD_CALL, WEBSITE_CALL) and optionally " + settingsDoc}},
		conversionResponseDoc...),
	Fields: conversionFields,
	Examples: []capability.Example{{Description: "Validate a web conversion action without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","conversion_actions":[{"name":"Purchase","type":"WEBPAGE",` +
			`"category":"PURCHASE","value_settings":{"default_value":10,"default_currency_code":"EUR"}}],"validate_only":true}`)}},
}

var conversionActionsUpdate = capability.Descriptor{
	ID: conversionActionsUpdateID, Version: 1, Title: "Change conversion actions of a Google Ads account",
	Description: "Change the settings of one to 20 existing conversion actions of a Google Ads account " +
		"(ConversionActionService.MutateConversionActions with update operations and a mask of exactly the given " +
		"settings). Each entry names the conversion_action_id and at least one of: " + settingsDoc + ". The type " +
		"cannot be changed, and settings are set, not cleared." + conversionNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"conversion measurement settings of a Google Ads account"),
	InputSchema: conversionInputSchema(conversionUpdateItemSchema, false), OutputSchema: conversionOutputSchema,
	Arguments: append([]capability.Argument{customerArgumentDoc,
		{Name: "conversion_actions", Required: true, Description: "One to 20 entries, each with conversion_action_id " +
			"(as reports.search returns it) and at least one setting of: " + settingsDoc}}, conversionResponseDoc...),
	Fields: conversionFields,
	Examples: []capability.Example{{Description: "Make an action non-biddable, validated only",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","conversion_actions":[{"conversion_action_id":"5544332211",` +
			`"primary_for_goal":false}],"validate_only":true}`)}},
}

var conversionActionsRemove = capability.Descriptor{
	ID: conversionActionsRemoveID, Version: 1, Title: "Remove conversion actions of a Google Ads account",
	Description: "Remove one to 20 conversion actions of a Google Ads account (ConversionActionService." +
		"MutateConversionActions with remove operations of customers/ID/conversionActions/ID). A removed conversion " +
		"action records no conversions any more and cannot be enabled again; its history stays in reports. Campaigns " +
		"and goals that used it lose it as a bidding signal. The tool builds the names from customer_id and the " +
		"numeric IDs and takes no other settings. Up to 20 operations go in one atomic request without " +
		"partial_failure. The service answers the resource names only, so there is no response_content_type." +
		mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent,
		"conversion measurement settings of a Google Ads account"),
	InputSchema: conversionInputSchema(nil, true), OutputSchema: conversionOutputSchema,
	Arguments: []capability.Argument{customerArgumentDoc,
		{Name: "conversion_action_ids", Required: true, Description: "One to 20 distinct numeric IDs of conversion " +
			"actions of the account, as reports.search returns them"}, validateOnlyArgument},
	Fields: conversionFields,
	Examples: []capability.Example{{Description: "Validate the removal of a conversion action",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","conversion_action_ids":["5544332211"],"validate_only":true}`)}},
}

// --- handlers ---

func handleConversionActionsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionActionsChange(ctx, resolved, secrets, red, raw, conversionActionsCreateID)
}

func handleConversionActionsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionActionsChange(ctx, resolved, secrets, red, raw, conversionActionsUpdateID)
}

func handleConversionActionsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionActionsChange(ctx, resolved, secrets, red, raw, conversionActionsRemoveID)
}

type (
	conversionActionsResult struct {
		CustomerID   string                   `json:"customer_id"`
		ValidateOnly bool                     `json:"validate_only"`
		Results      []conversionActionResult `json:"results"`
	}
	conversionActionResult struct {
		ConversionActionID string                    `json:"conversion_action_id"`
		ResourceName       string                    `json:"resource_name"`
		ConversionAction   *conversionActionResource `json:"conversion_action,omitempty"`
	}
)

func conversionActionsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, tool string) (any, error) {
	op := map[string]string{conversionActionsCreateID: "create conversion actions",
		conversionActionsUpdateID: "update conversion actions", conversionActionsRemoveID: "remove conversion actions"}[tool]
	var args conversionActionsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(args.ResponseContentType); err != nil {
		return nil, err
	}
	operations, names, err := args.operations(tool)
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"operations": operations}
	if args.ValidateOnly {
		body["validateOnly"] = true
	}
	if args.ResponseContentType != "" {
		body["responseContentType"] = args.ResponseContentType
	}
	var answer struct {
		Results []struct {
			ResourceName     string          `json:"resourceName"`
			ConversionAction json.RawMessage `json:"conversionAction"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: args.CustomerID, service: conversionActionService,
		body: body}, &answer); err != nil {
		return nil, err
	}
	// The request was sent: an answer that does not fit it leaves the outcome open.
	invalid := func(what string) error {
		return unsure(provider.InvalidResponse(op, subject+" answered with "+what), !args.ValidateOnly)
	}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, invalid("a partial failure although none was asked for")
	}
	out := conversionActionsResult{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly,
		Results: []conversionActionResult{}}
	if args.ValidateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != len(operations) {
		return nil, invalid("a number of results that is not the number of operations")
	}
	seen := map[string]bool{}
	for i, r := range answer.Results {
		id, ok := conversionActionIDOf(r.ResourceName, args.CustomerID)
		if !ok || seen[id] || (names[i] != "" && r.ResourceName != names[i]) {
			return nil, invalid("a result for another conversion action or account than the one asked for")
		}
		seen[id] = true
		item := conversionActionResult{ConversionActionID: id, ResourceName: r.ResourceName}
		if len(r.ConversionAction) > 0 && string(r.ConversionAction) != "null" {
			resource, ok := conversionActionOf(r.ConversionAction, r.ResourceName, id)
			if !ok {
				return nil, invalid("a conversion action that is not the one asked for")
			}
			item.ConversionAction = resource
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// operations checks the arguments of the tool and builds the operations. names holds the resource name each
// operation must be answered with, empty for a create, whose ID Google assigns.
func (a conversionActionsArguments) operations(tool string) (operations []any, names []string, err error) {
	switch tool {
	case conversionActionsRemoveID:
		if n := len(a.ConversionActionIDs); n < 1 || n > maxConversionActionOperations {
			return nil, nil, badRequest("conversion_action_ids names one to 20 conversion actions")
		}
		for i, id := range a.ConversionActionIDs {
			if err := checkConversionActionID("conversion_action_ids", id); err != nil {
				return nil, nil, err
			}
			if slices.Contains(a.ConversionActionIDs[:i], id) {
				return nil, nil, badRequest("conversion_action_ids names each conversion action once")
			}
			name := conversionActionName(a.CustomerID, id)
			operations = append(operations, map[string]any{"remove": name})
			names = append(names, name)
		}
		return operations, names, nil
	case conversionActionsCreateID:
		var items []conversionActionCreateItem
		if json.Unmarshal(a.ConversionActions, &items) != nil || len(items) < 1 || len(items) > maxConversionActionOperations {
			return nil, nil, badRequest("conversion_actions holds one to 20 conversion actions")
		}
		for _, item := range items {
			if item.Name == nil || !slices.Contains(conversionActionCreateTypes, item.Type) {
				return nil, nil, badRequest("every conversion action needs a name and a type of WEBPAGE, UPLOAD_CLICKS, " +
					"UPLOAD_CALLS, AD_CALL or WEBSITE_CALL")
			}
			if err := item.check(slices.Contains(callConversionTypes, item.Type)); err != nil {
				return nil, nil, err
			}
			resource := map[string]any{"type": item.Type}
			item.wire(resource)
			operations = append(operations, map[string]any{"create": resource})
			names = append(names, "")
		}
		return operations, names, nil
	}
	var items []conversionActionUpdateItem
	if json.Unmarshal(a.ConversionActions, &items) != nil || len(items) < 1 || len(items) > maxConversionActionOperations {
		return nil, nil, badRequest("conversion_actions holds one to 20 conversion actions")
	}
	for i, item := range items {
		if err := checkConversionActionID("conversion_action_id", item.ConversionActionID); err != nil {
			return nil, nil, err
		}
		for _, earlier := range items[:i] {
			if earlier.ConversionActionID == item.ConversionActionID {
				return nil, nil, badRequest("conversion_actions names each conversion action once")
			}
		}
		// Whether the type is a call type is not known here; Google validates the lookback windows of it.
		if err := item.check(false); err != nil {
			return nil, nil, err
		}
		name := conversionActionName(a.CustomerID, item.ConversionActionID)
		resource := map[string]any{"resourceName": name}
		mask := item.wire(resource)
		if len(mask) == 0 {
			return nil, nil, badRequest("every entry of conversion_actions needs at least one setting to change")
		}
		operations = append(operations, map[string]any{"update": resource, "updateMask": strings.Join(mask, ",")})
		names = append(names, name)
	}
	return operations, names, nil
}

// --- the conversion action as Google returns it ---

type (
	conversionActionWire struct {
		ResourceName               string   `json:"resourceName"`
		ID                         *wireInt `json:"id"`
		Name                       string   `json:"name"`
		Status                     string   `json:"status"`
		Type                       string   `json:"type"`
		Origin                     string   `json:"origin"`
		PrimaryForGoal             *bool    `json:"primaryForGoal"`
		Category                   string   `json:"category"`
		OwnerCustomer              string   `json:"ownerCustomer"`
		IncludeInConversionsMetric *bool    `json:"includeInConversionsMetric"`
		ClickThroughLookbackWindow *wireInt `json:"clickThroughLookbackWindowDays"`
		ViewThroughLookbackWindow  *wireInt `json:"viewThroughLookbackWindowDays"`
		ValueSettings              *struct {
			DefaultValue          *float64 `json:"defaultValue"`
			DefaultCurrencyCode   string   `json:"defaultCurrencyCode"`
			AlwaysUseDefaultValue *bool    `json:"alwaysUseDefaultValue"`
		} `json:"valueSettings"`
		CountingType             string `json:"countingType"`
		AttributionModelSettings *struct {
			AttributionModel     string `json:"attributionModel"`
			DataDrivenModelState string `json:"dataDrivenModelStatus"`
		} `json:"attributionModelSettings"`
		TagSnippets []struct {
			Type          string `json:"type"`
			PageFormat    string `json:"pageFormat"`
			GlobalSiteTag string `json:"globalSiteTag"`
			EventSnippet  string `json:"eventSnippet"`
		} `json:"tagSnippets"`
		PhoneCallDurationSeconds *wireInt `json:"phoneCallDurationSeconds"`
		AppID                    string   `json:"appId"`
		MobileAppVendor          string   `json:"mobileAppVendor"`
		FirebaseSettings         *struct {
			EventName    string   `json:"eventName"`
			ProjectID    string   `json:"projectId"`
			PropertyID   *wireInt `json:"propertyId"`
			PropertyName string   `json:"propertyName"`
		} `json:"firebaseSettings"`
		ThirdPartyAppAnalyticsSettings *struct {
			EventName    string `json:"eventName"`
			ProviderName string `json:"providerName"`
		} `json:"thirdPartyAppAnalyticsSettings"`
		GoogleAnalytics4Settings *struct {
			EventName    string   `json:"eventName"`
			PropertyName string   `json:"propertyName"`
			PropertyID   *wireInt `json:"propertyId"`
		} `json:"googleAnalytics4Settings"`
	}

	conversionActionResource struct {
		ResourceName               string                 `json:"resource_name"`
		ID                         string                 `json:"id,omitempty"`
		Name                       string                 `json:"name,omitempty"`
		Status                     string                 `json:"status,omitempty"`
		Type                       string                 `json:"type,omitempty"`
		Origin                     string                 `json:"origin,omitempty"`
		PrimaryForGoal             *bool                  `json:"primary_for_goal,omitempty"`
		Category                   string                 `json:"category,omitempty"`
		OwnerCustomer              string                 `json:"owner_customer,omitempty"`
		IncludeInConversionsMetric *bool                  `json:"include_in_conversions_metric,omitempty"`
		ClickThroughLookbackDays   *int64                 `json:"click_through_lookback_window_days,omitempty"`
		ViewThroughLookbackDays    *int64                 `json:"view_through_lookback_window_days,omitempty"`
		ValueSettings              *conversionValueOutput `json:"value_settings,omitempty"`
		CountingType               string                 `json:"counting_type,omitempty"`
		AttributionModelSettings   *conversionAttribution `json:"attribution_model_settings,omitempty"`
		TagSnippets                []conversionTagSnippet `json:"tag_snippets,omitempty"`
		PhoneCallDurationSeconds   *int64                 `json:"phone_call_duration_seconds,omitempty"`
		AppID                      string                 `json:"app_id,omitempty"`
		MobileAppVendor            string                 `json:"mobile_app_vendor,omitempty"`
		FirebaseSettings           *conversionFirebase    `json:"firebase_settings,omitempty"`
		ThirdPartyAppAnalytics     *conversionThirdParty  `json:"third_party_app_analytics_settings,omitempty"`
		GoogleAnalytics4Settings   *conversionAnalytics4  `json:"google_analytics_4_settings,omitempty"`
	}
	conversionValueOutput struct {
		DefaultValue          *float64 `json:"default_value,omitempty"`
		DefaultCurrencyCode   string   `json:"default_currency_code,omitempty"`
		AlwaysUseDefaultValue *bool    `json:"always_use_default_value,omitempty"`
	}
	conversionAttribution struct {
		AttributionModel     string `json:"attribution_model,omitempty"`
		DataDrivenModelState string `json:"data_driven_model_status,omitempty"`
	}
	conversionTagSnippet struct {
		Type          string `json:"type,omitempty"`
		PageFormat    string `json:"page_format,omitempty"`
		GlobalSiteTag string `json:"global_site_tag,omitempty"`
		EventSnippet  string `json:"event_snippet,omitempty"`
	}
	conversionFirebase struct {
		EventName    string `json:"event_name,omitempty"`
		ProjectID    string `json:"project_id,omitempty"`
		PropertyID   string `json:"property_id,omitempty"`
		PropertyName string `json:"property_name,omitempty"`
	}
	conversionThirdParty struct {
		EventName    string `json:"event_name,omitempty"`
		ProviderName string `json:"provider_name,omitempty"`
	}
	conversionAnalytics4 struct {
		EventName    string `json:"event_name,omitempty"`
		PropertyName string `json:"property_name,omitempty"`
		PropertyID   string `json:"property_id,omitempty"`
	}
)

func wireNumber(v *wireInt) (*int64, bool) {
	if v == nil {
		return nil, true
	}
	n, err := strconv.ParseInt(string(*v), 10, 64)
	return &n, err == nil
}

func wireText(v *wireInt) string {
	if v == nil {
		return ""
	}
	return boundedText(string(*v), maxCodeText)
}

// conversionActionOf maps the conversion action of a mutate answer. It must be the one of the result.
func conversionActionOf(raw json.RawMessage, resourceName, id string) (*conversionActionResource, bool) {
	var w conversionActionWire
	if json.Unmarshal(raw, &w) != nil || (w.ResourceName != "" && w.ResourceName != resourceName) {
		return nil, false
	}
	out := &conversionActionResource{ResourceName: resourceName, Name: boundedText(w.Name, maxText),
		Status: code(w.Status), Type: code(w.Type), Origin: code(w.Origin), PrimaryForGoal: w.PrimaryForGoal,
		Category: code(w.Category), OwnerCustomer: boundedText(w.OwnerCustomer, maxText),
		IncludeInConversionsMetric: w.IncludeInConversionsMetric, CountingType: code(w.CountingType),
		AppID: boundedText(w.AppID, maxText), MobileAppVendor: code(w.MobileAppVendor)}
	if w.ID != nil {
		if string(*w.ID) != id {
			return nil, false
		}
		out.ID = id
	}
	var ok bool
	if out.ClickThroughLookbackDays, ok = wireNumber(w.ClickThroughLookbackWindow); !ok {
		return nil, false
	}
	if out.ViewThroughLookbackDays, ok = wireNumber(w.ViewThroughLookbackWindow); !ok {
		return nil, false
	}
	if out.PhoneCallDurationSeconds, ok = wireNumber(w.PhoneCallDurationSeconds); !ok {
		return nil, false
	}
	if v := w.ValueSettings; v != nil {
		out.ValueSettings = &conversionValueOutput{DefaultValue: v.DefaultValue,
			DefaultCurrencyCode: code(v.DefaultCurrencyCode), AlwaysUseDefaultValue: v.AlwaysUseDefaultValue}
	}
	if a := w.AttributionModelSettings; a != nil {
		out.AttributionModelSettings = &conversionAttribution{AttributionModel: code(a.AttributionModel),
			DataDrivenModelState: code(a.DataDrivenModelState)}
	}
	for _, s := range w.TagSnippets[:min(len(w.TagSnippets), maxListItems)] {
		out.TagSnippets = append(out.TagSnippets, conversionTagSnippet{Type: code(s.Type), PageFormat: code(s.PageFormat),
			GlobalSiteTag: boundedText(s.GlobalSiteTag, maxLongText), EventSnippet: boundedText(s.EventSnippet, maxLongText)})
	}
	if f := w.FirebaseSettings; f != nil {
		out.FirebaseSettings = &conversionFirebase{EventName: boundedText(f.EventName, maxText),
			ProjectID: boundedText(f.ProjectID, maxText), PropertyID: wireText(f.PropertyID),
			PropertyName: boundedText(f.PropertyName, maxText)}
	}
	if t := w.ThirdPartyAppAnalyticsSettings; t != nil {
		out.ThirdPartyAppAnalytics = &conversionThirdParty{EventName: boundedText(t.EventName, maxText),
			ProviderName: boundedText(t.ProviderName, maxText)}
	}
	if g := w.GoogleAnalytics4Settings; g != nil {
		out.GoogleAnalytics4Settings = &conversionAnalytics4{EventName: boundedText(g.EventName, maxText),
			PropertyName: boundedText(g.PropertyName, maxText), PropertyID: wireText(g.PropertyID)}
	}
	return out, true
}

// conversionActionsSummary phrases the change for the confirmation, from the arguments alone.
func conversionActionsSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a conversionActionsArguments
		_ = json.Unmarshal(raw, &a)
		where := " in Google Ads account " + a.CustomerID
		prefix := ""
		if a.ValidateOnly {
			prefix = "Only validate: "
		}
		switch tool {
		case conversionActionsRemoveID:
			return prefix + "Remove " + strconv.Itoa(len(a.ConversionActionIDs)) + " conversion action(s) (" +
				strings.Join(a.ConversionActionIDs, ", ") + ")" + where + ": they record no conversions any more " +
				"and cannot be enabled again."
		case conversionActionsCreateID:
			var items []conversionActionCreateItem
			_ = json.Unmarshal(a.ConversionActions, &items)
			var parts []string
			for _, item := range items {
				name := ""
				if item.Name != nil {
					name = boundedText(*item.Name, 80)
				}
				parts = append(parts, name+" ("+item.Type+")")
			}
			return prefix + "Create " + strconv.Itoa(len(items)) + " conversion action(s)" + where + ": " +
				strings.Join(parts, ", ") + ". A primary conversion action feeds the bidding of campaigns that use its goal."
		}
		var items []conversionActionUpdateItem
		_ = json.Unmarshal(a.ConversionActions, &items)
		var parts []string
		for _, item := range items {
			part := item.ConversionActionID
			if item.PrimaryForGoal != nil {
				part += " primary_for_goal=" + strconv.FormatBool(*item.PrimaryForGoal)
			}
			if item.Status != "" {
				part += " status=" + item.Status
			}
			parts = append(parts, part)
		}
		return prefix + "Change " + strconv.Itoa(len(items)) + " conversion action(s)" + where + " (" +
			strings.Join(parts, "; ") + "). This can change what campaigns optimize and bid for."
	}
}
