package googleads

import (
	"context"
	"encoding/json"
	"math"
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
	customerSkAdNetworkSchemasUpdateID = Provider + ".customer-sk-ad-network-conversion-value-schemas.update"
	skAdNetworkSchemaService           = "/customerSkAdNetworkConversionValueSchemas"

	maxFineGrainedValue    = 63
	maxPostbacks           = 3
	maxMappedEvents        = 20
	maxSkAdNetworkNameText = 100
)

var coarseConversionValues = []string{"UNAVAILABLE", "LOW", "MEDIUM", "HIGH", "NONE"}

type (
	skRange struct {
		Min *float64 `json:"min"`
		Max *float64 `json:"max"`
	}
	skEvent struct {
		MappedEventName      string   `json:"mapped_event_name"`
		CurrencyCode         string   `json:"currency_code"`
		EventRevenueRange    *skRange `json:"event_revenue_range"`
		EventRevenueValue    *float64 `json:"event_revenue_value"`
		EventOccurrenceRange *skRange `json:"event_occurrence_range"`
		EventCounter         *int64   `json:"event_counter"`
	}
	skMapping struct {
		MinTimePostInstallHours *int64    `json:"min_time_post_install_hours"`
		MaxTimePostInstallHours *int64    `json:"max_time_post_install_hours"`
		MappedEvents            []skEvent `json:"mapped_events"`
	}
	skFine struct {
		FineGrainedConversionValue *int64     `json:"fine_grained_conversion_value"`
		ConversionValueMapping     *skMapping `json:"conversion_value_mapping"`
	}
	skCoarse struct {
		Low    *skMapping `json:"low_conversion_value_mapping"`
		Medium *skMapping `json:"medium_conversion_value_mapping"`
		High   *skMapping `json:"high_conversion_value_mapping"`
	}
	skPostback struct {
		PostbackSequenceIndex           *int64    `json:"postback_sequence_index"`
		CoarseGrainedMappings           *skCoarse `json:"coarse_grained_conversion_value_mappings"`
		LockWindowCoarseConversionValue string    `json:"lock_window_coarse_conversion_value"`
		LockWindowFineConversionValue   *int64    `json:"lock_window_fine_conversion_value"`
		LockWindowEvent                 string    `json:"lock_window_event"`
	}
	skSchema struct {
		AppID                  string       `json:"app_id"`
		MeasurementWindowHours *int64       `json:"measurement_window_hours"`
		FineGrainedMappings    []skFine     `json:"fine_grained_conversion_value_mappings"`
		PostbackMappings       []skPostback `json:"postback_mappings"`
	}
	skArguments struct {
		CustomerID     string   `json:"customer_id"`
		AccountLinkID  string   `json:"account_link_id"`
		Schema         skSchema `json:"schema"`
		ValidateOnly   bool     `json:"validate_only"`
		EnableWarnings bool     `json:"enable_warnings"`
	}
)

func skName(customerID, linkID string) string {
	return customerName(customerID) + skAdNetworkSchemaService + "/" + linkID
}

func nonNegative(v *float64) bool {
	return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0)
}

func skText(value string) bool {
	n := utf8.RuneCountInString(value)
	return n >= 1 && n <= maxSkAdNetworkNameText && strings.TrimSpace(value) != "" &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func (r *skRange) wire(minKey, maxKey string) (map[string]any, error) {
	if !nonNegative(r.Min) || !nonNegative(r.Max) || (r.Min != nil && r.Max != nil && *r.Min > *r.Max) {
		return nil, badRequest("a range needs a minimum and a maximum of 0 or more, the minimum not above the maximum")
	}
	out := map[string]any{}
	if r.Min != nil {
		out[minKey] = *r.Min
	}
	if r.Max != nil {
		out[maxKey] = *r.Max
	}
	return out, nil
}

func (e skEvent) wire() (map[string]any, error) {
	if !skText(e.MappedEventName) {
		return nil, badRequest("mapped_event_name must be 1 to 100 characters without control characters")
	}
	out := map[string]any{"mappedEventName": e.MappedEventName}
	if e.CurrencyCode != "" {
		if !currencyPattern.MatchString(e.CurrencyCode) {
			return nil, badRequest("currency_code must be a three-letter ISO 4217 code in capitals")
		}
		out["currencyCode"] = e.CurrencyCode
	}
	if e.EventRevenueRange != nil && e.EventRevenueValue != nil || e.EventOccurrenceRange != nil && e.EventCounter != nil {
		return nil, badRequest("an event names a revenue range or a revenue value, and an occurrence range or a counter, " +
			"never both")
	}
	if e.EventRevenueRange != nil {
		r, err := e.EventRevenueRange.wire("minEventRevenue", "maxEventRevenue")
		if err != nil {
			return nil, err
		}
		out["eventRevenueRange"] = r
	}
	if e.EventRevenueValue != nil {
		if !nonNegative(e.EventRevenueValue) {
			return nil, badRequest("event_revenue_value must be a number of 0 or more")
		}
		out["eventRevenueValue"] = *e.EventRevenueValue
	}
	if e.EventOccurrenceRange != nil {
		r, err := e.EventOccurrenceRange.wire("minEventCount", "maxEventCount")
		if err != nil {
			return nil, err
		}
		out["eventOccurrenceRange"] = r
	}
	if e.EventCounter != nil {
		if *e.EventCounter < 0 {
			return nil, badRequest("event_counter must be 0 or more")
		}
		out["eventCounter"] = *e.EventCounter
	}
	return out, nil
}

func (m *skMapping) wire() (map[string]any, error) {
	if len(m.MappedEvents) > maxMappedEvents {
		return nil, badRequest("a conversion value mapping holds at most 20 mapped events")
	}
	out := map[string]any{}
	for key, v := range map[string]*int64{"minTimePostInstallHours": m.MinTimePostInstallHours,
		"maxTimePostInstallHours": m.MaxTimePostInstallHours} {
		if v != nil {
			if *v < 0 {
				return nil, badRequest("the post-install hours must be 0 or more")
			}
			out[key] = *v
		}
	}
	if m.MinTimePostInstallHours != nil && m.MaxTimePostInstallHours != nil &&
		*m.MinTimePostInstallHours > *m.MaxTimePostInstallHours {
		return nil, badRequest("min_time_post_install_hours must not be above max_time_post_install_hours")
	}
	events := []any{}
	for _, e := range m.MappedEvents {
		w, err := e.wire()
		if err != nil {
			return nil, err
		}
		events = append(events, w)
	}
	if len(events) > 0 {
		out["mappedEvents"] = events
	}
	return out, nil
}

func inValueRange(v *int64) bool { return v != nil && *v >= 0 && *v <= maxFineGrainedValue }

// schemaBody checks the schema as far as the reference states limits (fine grained values 0 to 63, postback
// index 0 to 2) and builds the wire form. Every other rule is Google's to validate.
func (s skSchema) wire() (map[string]any, error) {
	if !skText(s.AppID) || strings.ContainsAny(s.AppID, " \t") {
		return nil, badRequest("app_id must be the Apple App Store app ID, 1 to 100 characters without spaces")
	}
	out := map[string]any{"appId": s.AppID}
	if s.MeasurementWindowHours != nil {
		if *s.MeasurementWindowHours < 0 || *s.MeasurementWindowHours > math.MaxInt32 {
			return nil, badRequest("measurement_window_hours must be 0 or more")
		}
		out["measurementWindowHours"] = *s.MeasurementWindowHours
	}
	if len(s.FineGrainedMappings) > maxFineGrainedValue+1 || len(s.PostbackMappings) > maxPostbacks {
		return nil, badRequest("a schema holds at most 64 fine grained mappings and 3 postback mappings")
	}
	var fines []any
	var seen []int64
	for _, f := range s.FineGrainedMappings {
		if !inValueRange(f.FineGrainedConversionValue) || slices.Contains(seen, *f.FineGrainedConversionValue) {
			return nil, badRequest("fine_grained_conversion_value must be 0 to 63, each value at most once")
		}
		seen = append(seen, *f.FineGrainedConversionValue)
		entry := map[string]any{"fineGrainedConversionValue": *f.FineGrainedConversionValue}
		if f.ConversionValueMapping != nil {
			m, err := f.ConversionValueMapping.wire()
			if err != nil {
				return nil, err
			}
			entry["conversionValueMapping"] = m
		}
		fines = append(fines, entry)
	}
	if fines != nil {
		out["fineGrainedConversionValueMappings"] = fines
	}
	var posts []any
	var indexes []int64
	for _, p := range s.PostbackMappings {
		if p.PostbackSequenceIndex == nil || *p.PostbackSequenceIndex < 0 || *p.PostbackSequenceIndex >= maxPostbacks ||
			slices.Contains(indexes, *p.PostbackSequenceIndex) {
			return nil, badRequest("postback_sequence_index must be 0 to 2, each index at most once")
		}
		indexes = append(indexes, *p.PostbackSequenceIndex)
		entry := map[string]any{"postbackSequenceIndex": *p.PostbackSequenceIndex}
		if p.CoarseGrainedMappings != nil {
			coarse := map[string]any{}
			for key, m := range map[string]*skMapping{"lowConversionValueMapping": p.CoarseGrainedMappings.Low,
				"mediumConversionValueMapping": p.CoarseGrainedMappings.Medium,
				"highConversionValueMapping":   p.CoarseGrainedMappings.High} {
				if m != nil {
					w, err := m.wire()
					if err != nil {
						return nil, err
					}
					coarse[key] = w
				}
			}
			entry["coarseGrainedConversionValueMappings"] = coarse
		}
		triggers := 0
		if p.LockWindowCoarseConversionValue != "" {
			if !slices.Contains(coarseConversionValues, p.LockWindowCoarseConversionValue) {
				return nil, badRequest("lock_window_coarse_conversion_value must be UNAVAILABLE, LOW, MEDIUM, HIGH or NONE")
			}
			entry["lockWindowCoarseConversionValue"] = p.LockWindowCoarseConversionValue
			triggers++
		}
		if p.LockWindowFineConversionValue != nil {
			if !inValueRange(p.LockWindowFineConversionValue) {
				return nil, badRequest("lock_window_fine_conversion_value must be 0 to 63")
			}
			entry["lockWindowFineConversionValue"] = *p.LockWindowFineConversionValue
			triggers++
		}
		if p.LockWindowEvent != "" {
			if !skText(p.LockWindowEvent) {
				return nil, badRequest("lock_window_event must be 1 to 100 characters without control characters")
			}
			entry["lockWindowEvent"] = p.LockWindowEvent
			triggers++
		}
		if triggers > 1 {
			return nil, badRequest("a postback names at most one lock window trigger")
		}
		posts = append(posts, entry)
	}
	if posts != nil {
		out["postbackMappings"] = posts
	}
	return out, nil
}

type skChanged struct {
	CustomerID    string     `json:"customer_id"`
	ValidateOnly  bool       `json:"validate_only"`
	ResourceName  string     `json:"resource_name,omitempty"`
	AccountLinkID string     `json:"account_link_id,omitempty"`
	AppID         string     `json:"app_id,omitempty"`
	Warning       *skWarning `json:"warning,omitempty"`
}

type skWarning struct {
	Code    *int64 `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func handleSkAdNetworkSchemaUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update SKAdNetwork conversion value schema"
	var args skArguments
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
	if err := checkNumericID("account_link_id", args.AccountLinkID); err != nil {
		return nil, err
	}
	schemaWire, err := args.Schema.wire()
	if err != nil {
		return nil, err
	}
	name := skName(args.CustomerID, args.AccountLinkID)
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op,
		tool: customerSkAdNetworkSchemasUpdateID}
	c, err := env.settle(args.CustomerID, false)
	if err != nil {
		return nil, err
	}
	body := mutateBody(map[string]any{"update": map[string]any{"resourceName": name, "schema": schemaWire}},
		args.ValidateOnly, "")
	if args.EnableWarnings {
		body["enableWarnings"] = true
	}
	var answer struct {
		Result *struct {
			ResourceName string `json:"resourceName"`
			AppID        string `json:"appId"`
		} `json:"result"`
		Warning *struct {
			Code    *wireInt `json:"code"`
			Message string   `json:"message"`
		} `json:"warning"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: customerSkAdNetworkSchemasUpdateID, customerID: args.CustomerID,
		service: skAdNetworkSchemaService, body: body}, &answer); err != nil {
		return nil, err
	}
	out := skChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly}
	if answer.Warning != nil {
		out.Warning = &skWarning{Message: boundedText(answer.Warning.Message, maxText)}
		if answer.Warning.Code != nil {
			if n, err := strconv.ParseInt(string(*answer.Warning.Code), 10, 64); err == nil {
				out.Warning.Code = &n
			}
		}
	}
	if answer.Result == nil || answer.Result.ResourceName == "" {
		if args.ValidateOnly {
			return out, nil
		}
		return nil, unsureResult(op, "answered without a result")
	}
	if answer.Result.ResourceName != name {
		return nil, unsure(provider.InvalidResponse(op, subject+" answered with a result for another object than the "+
			"one asked for"), true)
	}
	out.ResourceName, out.AccountLinkID, out.AppID = name, args.AccountLinkID, boundedText(answer.Result.AppID, maxText)
	return out, nil
}

func skSummary(raw json.RawMessage) string {
	var a skArguments
	_ = json.Unmarshal(raw, &a)
	prefix := ""
	if a.ValidateOnly {
		prefix = "Only validate: "
	}
	return prefix + "Replace the SKAdNetwork conversion value schema of app " + boundedText(a.Schema.AppID, 100) +
		" (account link " + a.AccountLinkID + ") in Google Ads account " + a.CustomerID + ". This changes how " +
		"iOS app conversions are valued for bidding."
}

// --- schemas and descriptor ---

var (
	skMappingSchema = object(schema{
		"min_time_post_install_hours": schema{"type": "integer", "minimum": 0},
		"max_time_post_install_hours": schema{"type": "integer", "minimum": 0},
		"mapped_events": schema{"type": "array", "maxItems": maxMappedEvents, "items": object(schema{
			"mapped_event_name":      schema{"type": "string", "minLength": 1, "maxLength": maxSkAdNetworkNameText},
			"currency_code":          schema{"type": "string", "pattern": currencyPattern.String()},
			"event_revenue_range":    skRangeSchema(),
			"event_revenue_value":    schema{"type": "number", "minimum": 0},
			"event_occurrence_range": skRangeSchema(),
			"event_counter":          schema{"type": "integer", "minimum": 0}}, "mapped_event_name")}})

	fineValueSchema = schema{"type": "integer", "minimum": 0, "maximum": maxFineGrainedValue}

	skSchemaSchema = object(schema{
		"app_id":                   schema{"type": "string", "minLength": 1, "maxLength": maxSkAdNetworkNameText},
		"measurement_window_hours": schema{"type": "integer", "minimum": 0},
		"fine_grained_conversion_value_mappings": schema{"type": "array", "maxItems": maxFineGrainedValue + 1,
			"items": object(schema{"fine_grained_conversion_value": fineValueSchema,
				"conversion_value_mapping": skMappingSchema}, "fine_grained_conversion_value")},
		"postback_mappings": schema{"type": "array", "maxItems": maxPostbacks, "items": object(schema{
			"postback_sequence_index": schema{"type": "integer", "minimum": 0, "maximum": maxPostbacks - 1},
			"coarse_grained_conversion_value_mappings": object(schema{"low_conversion_value_mapping": skMappingSchema,
				"medium_conversion_value_mapping": skMappingSchema, "high_conversion_value_mapping": skMappingSchema}),
			"lock_window_coarse_conversion_value": schema{"type": "string", "enum": coarseConversionValues},
			"lock_window_fine_conversion_value":   fineValueSchema,
			"lock_window_event":                   schema{"type": "string", "minLength": 1, "maxLength": maxSkAdNetworkNameText}},
			"postback_sequence_index")}}, "app_id")

	skInputSchema = encoded(object(schema{"customer_id": customerIDArgument,
		"account_link_id": schema{"type": "string", "pattern": numericIDPattern.String()},
		"schema":          skSchemaSchema, "validate_only": validateOnlySchema, "enable_warnings": booleanSchema},
		"customer_id", "account_link_id", "schema"))

	skOutputSchema = encoded(object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"resource_name": stringSchema, "account_link_id": stringSchema, "app_id": stringSchema,
		"warning": object(schema{"code": integerSchema, "message": stringSchema})}, "customer_id", "validate_only"))
)

func skRangeSchema() schema {
	return object(schema{"min": schema{"type": "number", "minimum": 0}, "max": schema{"type": "number", "minimum": 0}})
}

var customerSkAdNetworkConversionValueSchemasUpdate = capability.Descriptor{
	ID: customerSkAdNetworkSchemasUpdateID, Version: 1,
	Title: "Replace the SKAdNetwork conversion value schema of an app of a Google Ads account",
	Description: "Replace the SKAdNetwork conversion value schema that maps iOS conversion values to conversion " +
		"events for one app of a Google Ads account " +
		"(CustomerSkAdNetworkConversionValueSchemaService.MutateCustomerSkAdNetworkConversionValueSchema with one " +
		"update operation). The schema is named by the numeric account_link_id of the app link, which must exist " +
		"already: the tool creates and changes no link. The reference of v26 marks every field of the schema as " +
		"output only, so Google may refuse a change; use validate_only first. The schema decides how iOS app " +
		"conversions are valued for bidding." + mutateNote,
	Tags: []string{"google", "ads", "conversions", "ios", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"iOS conversion value schema of a Google Ads account"),
	InputSchema: skInputSchema, OutputSchema: skOutputSchema,
	Arguments: []capability.Argument{goalCustomerDoc,
		{Name: "account_link_id", Required: true, Description: "Numeric ID of the existing account link of the app, as " +
			"the resource name customers/ID/customerSkAdNetworkConversionValueSchemas/ID of reports.search shows it"},
		{Name: "schema", Required: true, Description: "The whole schema: app_id (Apple App Store app ID), " +
			"measurement_window_hours, fine_grained_conversion_value_mappings (up to 64, each with " +
			"fine_grained_conversion_value 0 to 63 and a conversion_value_mapping) and postback_mappings (up to 3, " +
			"each with postback_sequence_index 0 to 2, coarse_grained_conversion_value_mappings with a low, medium " +
			"and high mapping, and at most one lock window trigger: lock_window_coarse_conversion_value " +
			"(UNAVAILABLE, LOW, MEDIUM, HIGH or NONE), lock_window_fine_conversion_value 0 to 63 or " +
			"lock_window_event). A conversion_value_mapping holds the post-install hours and up to 20 mapped_events " +
			"with mapped_event_name, currency_code, event_revenue_range (min, max) or event_revenue_value, " +
			"event_occurrence_range (min, max) or event_counter"},
		validateOnlyArgument,
		{Name: "enable_warnings", Description: "true lets Google return schema validation problems as a warning " +
			"without blocking the change"}},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "resource_name", Description: "customers/ID/customerSkAdNetworkConversionValueSchemas/ID, as Google " +
			"answered; absent after validate_only"},
		{Name: "account_link_id", Description: "The account link of the schema"},
		{Name: "app_id", Description: "The app ID of the schema Google changed"},
		{Name: "warning", Description: "With enable_warnings: code and message of the non-blocking problem Google found"}},
	Examples: []capability.Example{{Description: "Validate a schema without changing anything",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","account_link_id":"5544332211","schema":{"app_id":` +
			`"1234567890","measurement_window_hours":72,"fine_grained_conversion_value_mappings":[{` +
			`"fine_grained_conversion_value":1,"conversion_value_mapping":{"mapped_events":[{"mapped_event_name":` +
			`"purchase","event_counter":1}]}}]},"validate_only":true}`)}},
}
