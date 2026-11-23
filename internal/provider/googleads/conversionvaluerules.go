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

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// A conversion value rule (ConversionValueRuleService.MutateConversionValueRules) adds to, multiplies, or sets
// the value of a conversion when its conditions hold. Smart Bidding optimizes for that value, so a rule moves
// spend; this is why all three tools are offered by the tools list only.

const (
	conversionValueRulesCreateID = Provider + ".conversion-value-rules.create"
	conversionValueRulesUpdateID = Provider + ".conversion-value-rules.update"
	conversionValueRulesRemoveID = Provider + ".conversion-value-rules.remove"

	valueRuleService    = "/conversionValueRules"
	valueRuleCollection = "conversionValueRules"

	// maxRuleReferences is Qatlas's own bound per list of a condition; Google documents none.
	maxRuleReferences = 100
)

var (
	valueRuleOperations = []string{"ADD", "MULTIPLY", "SET"}
	// REMOVED is left to the remove tool, which carries the delete effect.
	valueRuleStatuses     = []string{"ENABLED", "PAUSED"}
	valueRuleDeviceTypes  = []string{"MOBILE", "DESKTOP", "TABLET"}
	valueRuleGeoMatchType = []string{"ANY", "LOCATION_OF_PRESENCE"}

	valueRuleGeoPattern  = regexp.MustCompile(geoConstantPattern)
	valueRuleOwnerSchema = schema{"type": "string", "maxLength": 100}
)

type (
	valueRuleAction struct {
		Operation string   `json:"operation"`
		Value     *float64 `json:"value"`
	}
	valueRuleGeo struct {
		GeoTargetConstants         []string `json:"geo_target_constants,omitempty"`
		GeoMatchType               string   `json:"geo_match_type,omitempty"`
		ExcludedGeoTargetConstants []string `json:"excluded_geo_target_constants,omitempty"`
		ExcludedGeoMatchType       string   `json:"excluded_geo_match_type,omitempty"`
	}
	valueRuleDevice struct {
		DeviceTypes []string `json:"device_types,omitempty"`
	}
	valueRuleAudience struct {
		UserLists     []string `json:"user_lists,omitempty"`
		UserInterests []string `json:"user_interests,omitempty"`
	}
	valueRuleItinerary struct {
		AdvanceBookingWindow *struct {
			MinDays *int32 `json:"min_days,omitempty"`
			MaxDays *int32 `json:"max_days,omitempty"`
		} `json:"advance_booking_window,omitempty"`
		TravelLength *struct {
			MinNights *int32 `json:"min_nights,omitempty"`
			MaxNights *int32 `json:"max_nights,omitempty"`
		} `json:"travel_length,omitempty"`
		TravelStartDay *struct {
			Monday    bool `json:"monday,omitempty"`
			Tuesday   bool `json:"tuesday,omitempty"`
			Wednesday bool `json:"wednesday,omitempty"`
			Thursday  bool `json:"thursday,omitempty"`
			Friday    bool `json:"friday,omitempty"`
			Saturday  bool `json:"saturday,omitempty"`
			Sunday    bool `json:"sunday,omitempty"`
		} `json:"travel_start_day,omitempty"`
	}
	// valueRuleFields are the settings a create or an update may carry. A nil pointer was not given.
	valueRuleFields struct {
		Action    *valueRuleAction    `json:"action,omitempty"`
		Status    string              `json:"status,omitempty"`
		Geo       *valueRuleGeo       `json:"geo_location_condition,omitempty"`
		Device    *valueRuleDevice    `json:"device_condition,omitempty"`
		Audience  *valueRuleAudience  `json:"audience_condition,omitempty"`
		Itinerary *valueRuleItinerary `json:"itinerary_condition,omitempty"`
	}
	valueRuleEntry struct {
		valueRuleFields
		ID string `json:"conversion_value_rule_id,omitempty"`
	}
	valueRuleArguments struct {
		CustomerID          string           `json:"customer_id"`
		Rules               []valueRuleEntry `json:"conversion_value_rules"`
		IDs                 []string         `json:"conversion_value_rule_ids"`
		ValidateOnly        bool             `json:"validate_only"`
		ResponseContentType string           `json:"response_content_type"`
	}
)

// checkRefs checks one list of resource names of a condition.
func checkRefs(argument string, values []string, valid func(string) bool, what string) error {
	if len(values) > maxRuleReferences {
		return badRequest(argument + " holds at most " + strconv.Itoa(maxRuleReferences) + " entries")
	}
	for i, v := range values {
		if !valid(v) || slices.Contains(values[:i], v) {
			return badRequest(argument + " must hold each " + what + " once")
		}
	}
	return nil
}

func inValues(allowed []string) func(string) bool {
	return func(v string) bool { return slices.Contains(allowed, v) }
}

func checkDays(v *int32) bool { return v == nil || *v >= 0 }

// check refuses what no request may start with. Rules that Google decides, such as which conditions a rule must
// carry or which accounts may use the itinerary condition, are left to Google.
func (f valueRuleFields) check(customerID string) error {
	if a := f.Action; a != nil {
		if !slices.Contains(valueRuleOperations, a.Operation) || a.Value == nil || math.IsNaN(*a.Value) || math.IsInf(*a.Value, 0) {
			return badRequest("action needs an operation of ADD, MULTIPLY or SET and a finite number as value")
		}
	}
	if f.Status != "" && !slices.Contains(valueRuleStatuses, f.Status) {
		return badRequest("status may be ENABLED or PAUSED; a rule is removed with conversion-value-rules.remove")
	}
	if g := f.Geo; g != nil {
		if err := checkRefs("geo_target_constants", g.GeoTargetConstants, valueRuleGeoPattern.MatchString,
			"geo target constant of the form geoTargetConstants/N"); err != nil {
			return err
		}
		if err := checkRefs("excluded_geo_target_constants", g.ExcludedGeoTargetConstants, valueRuleGeoPattern.MatchString,
			"geo target constant of the form geoTargetConstants/N"); err != nil {
			return err
		}
		for _, m := range []string{g.GeoMatchType, g.ExcludedGeoMatchType} {
			if m != "" && !slices.Contains(valueRuleGeoMatchType, m) {
				return badRequest("a geo match type must be ANY or LOCATION_OF_PRESENCE")
			}
		}
	}
	if d := f.Device; d != nil {
		if err := checkRefs("device_types", d.DeviceTypes, inValues(valueRuleDeviceTypes), "device type of MOBILE, DESKTOP, TABLET"); err != nil {
			return err
		}
	}
	if a := f.Audience; a != nil {
		owned := func(collection string) func(string) bool {
			return func(v string) bool { return ownedName(v, customerID, collection) }
		}
		if err := checkRefs("user_lists", a.UserLists, owned("userLists"), "user list of customer_id, customers/ID/userLists/N"); err != nil {
			return err
		}
		if err := checkRefs("user_interests", a.UserInterests, owned("userInterests"),
			"user interest of customer_id, customers/ID/userInterests/N"); err != nil {
			return err
		}
	}
	if i := f.Itinerary; i != nil {
		if w := i.AdvanceBookingWindow; w != nil && (!checkDays(w.MinDays) || !checkDays(w.MaxDays)) ||
			i.TravelLength != nil && (!checkDays(i.TravelLength.MinNights) || !checkDays(i.TravelLength.MaxNights)) {
			return badRequest("days and nights must be 0 or more")
		}
	}
	for name, condition := range map[string]any{"geo_location_condition": f.Geo, "device_condition": f.Device,
		"audience_condition": f.Audience, "itinerary_condition": f.Itinerary} {
		if isEmptyObject(condition) {
			return badRequest(name + " must name at least one setting; conditions are set, not cleared")
		}
	}
	return nil
}

// isEmptyObject reports whether a non-nil condition marshals to {}.
func isEmptyObject(v any) bool {
	raw, err := json.Marshal(v)
	return err == nil && string(raw) == "{}"
}

// camelKeys turns the snake_case member names of a decoded object into the camelCase of Google's JSON, and
// snakeKeys does the reverse for an answer.
func camelKeys(v any) any {
	return renameKeys(v, func(r rune) string { return strings.ToUpper(string(r)) }, '_')
}
func snakeKeys(v any) any {
	return renameKeys(v, func(r rune) string { return "_" + strings.ToLower(string(r)) }, 0)
}

func renameKeys(v any, upper func(rune) string, drop rune) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			var b strings.Builder
			next := false
			for _, r := range k {
				switch {
				case drop != 0 && r == drop:
					next = true
				case next:
					b.WriteString(upper(r))
					next = false
				case drop == 0 && unicode.IsUpper(r):
					b.WriteString(upper(r))
				default:
					b.WriteRune(r)
				}
			}
			out[b.String()] = renameKeys(item, upper, drop)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = renameKeys(item, upper, drop)
		}
		return out
	}
	return v
}

// wire is the resource body of Google for the given settings.
func (f valueRuleFields) wire() map[string]any {
	raw, _ := json.Marshal(f)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	return camelKeys(generic).(map[string]any)
}

// mask lists the top-level members the update sets. A condition given replaces the whole condition.
func (f valueRuleFields) mask() []string {
	var mask []string
	for _, m := range []struct {
		name string
		set  bool
	}{{"action", f.Action != nil}, {"status", f.Status != ""}, {"geoLocationCondition", f.Geo != nil},
		{"deviceCondition", f.Device != nil}, {"audienceCondition", f.Audience != nil},
		{"itineraryCondition", f.Itinerary != nil}} {
		if m.set {
			mask = append(mask, m.name)
		}
	}
	return mask
}

func valueRuleName(customerID, id string) string {
	return customerName(customerID) + "/" + valueRuleCollection + "/" + id
}

// operations checks the arguments and builds the operations and the resource names each result must carry.
func (a valueRuleArguments) operations(tool string) ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	var ops []any
	var names []string
	if tool == conversionValueRulesRemoveID {
		if n := len(a.IDs); n < 1 || n > maxListOperations {
			return nil, nil, badRequest("conversion_value_rule_ids must hold one to 10 IDs")
		}
		for i, id := range a.IDs {
			if !numericIDPattern.MatchString(id) || slices.Contains(a.IDs[:i], id) {
				return nil, nil, badRequest("conversion_value_rule_ids must hold each rule once as a positive number")
			}
			names = append(names, valueRuleName(a.CustomerID, id))
			ops = append(ops, map[string]any{"remove": names[i]})
		}
		return ops, names, nil
	}
	if n := len(a.Rules); n < 1 || n > maxListOperations {
		return nil, nil, badRequest("conversion_value_rules must hold one to 10 entries")
	}
	create := tool == conversionValueRulesCreateID
	var seen []string
	for _, r := range a.Rules {
		if err := r.check(a.CustomerID); err != nil {
			return nil, nil, err
		}
		if create {
			if r.ID != "" || r.Action == nil {
				return nil, nil, badRequest("a new rule takes no conversion_value_rule_id and needs an action")
			}
			ops = append(ops, map[string]any{"create": r.wire()})
			continue
		}
		mask := r.mask()
		if !numericIDPattern.MatchString(r.ID) || len(mask) == 0 || slices.Contains(seen, r.ID) {
			return nil, nil, badRequest("every entry needs the conversion_value_rule_id of an existing rule, once, " +
				"and at least one setting to change")
		}
		seen = append(seen, r.ID)
		name := valueRuleName(a.CustomerID, r.ID)
		update := r.wire()
		update["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

// The rule as the tools answer it. valueRuleResource is read from the answer of Google after its member names
// are turned into snake_case, so the settings keep the form of the arguments.
type (
	valueRuleResource struct {
		ResourceName  string   `json:"resource_name"`
		ID            *wireInt `json:"id,omitempty"`
		OwnerCustomer string   `json:"owner_customer,omitempty"`
		valueRuleFields
	}
	valueRuleResult struct {
		ResourceName          string             `json:"resource_name"`
		ConversionValueRuleID string             `json:"conversion_value_rule_id"`
		ConversionValueRule   *valueRuleResource `json:"conversion_value_rule,omitempty"`
	}
	valueRulesChanged struct {
		CustomerID   string            `json:"customer_id"`
		ValidateOnly bool              `json:"validate_only"`
		Results      []valueRuleResult `json:"results"`
	}
)

// valueRuleOf maps the rule of a result. It must be the one of the result, and every resource name in it must
// belong to the account of the call.
func valueRuleOf(raw json.RawMessage, name, id, customerID string) (*valueRuleResource, bool) {
	var generic map[string]any
	if json.Unmarshal(raw, &generic) != nil {
		return nil, false
	}
	var w valueRuleResource
	re, err := json.Marshal(snakeKeys(generic))
	if err != nil || json.Unmarshal(re, &w) != nil || (w.ResourceName != "" && w.ResourceName != name) ||
		(w.ID != nil && string(*w.ID) != id) ||
		(w.OwnerCustomer != "" && w.OwnerCustomer != customerName(customerID)) {
		return nil, false
	}
	w.ResourceName = name
	w.Status = code(w.Status)
	if a := w.Action; a != nil {
		a.Operation = code(a.Operation)
	}
	if g := w.Geo; g != nil {
		g.GeoMatchType, g.ExcludedGeoMatchType = code(g.GeoMatchType), code(g.ExcludedGeoMatchType)
		if !allMatch(g.GeoTargetConstants, valueRuleGeoPattern.MatchString) ||
			!allMatch(g.ExcludedGeoTargetConstants, valueRuleGeoPattern.MatchString) {
			return nil, false
		}
	}
	if d := w.Device; d != nil {
		for i := range d.DeviceTypes {
			d.DeviceTypes[i] = code(d.DeviceTypes[i])
		}
	}
	if au := w.Audience; au != nil &&
		(!allMatch(au.UserLists, func(v string) bool { return ownedName(v, customerID, "userLists") }) ||
			!allMatch(au.UserInterests, func(v string) bool { return ownedName(v, customerID, "userInterests") })) {
		return nil, false
	}
	return &w, true
}

func allMatch(values []string, valid func(string) bool) bool {
	return len(values) <= 10*maxRuleReferences && !slices.ContainsFunc(values, func(v string) bool { return !valid(v) })
}

// --- schemas ---

func minOne(s schema) schema {
	s["minProperties"] = 1
	return s
}

func refList(pattern, description string) schema {
	return schema{"type": "array", "maxItems": maxRuleReferences, "uniqueItems": true, "description": description,
		"items": schema{"type": "string", "maxLength": 100, "pattern": pattern}}
}

var (
	ruleDayNumber = schema{"type": "integer", "minimum": 0, "maximum": math.MaxInt32}
	ruleWeekday   = booleanSchema

	ruleFieldSchemas = schema{
		"action": object(schema{"operation": schema{"type": "string", "enum": valueRuleOperations},
			"value": numberSchema}, "operation", "value"),
		"status": schema{"type": "string", "enum": valueRuleStatuses},
		"geo_location_condition": minOne(object(schema{
			"geo_target_constants":          refList(geoConstantPattern, "Geo locations that count, geoTargetConstants/N"),
			"geo_match_type":                schema{"type": "string", "enum": valueRuleGeoMatchType},
			"excluded_geo_target_constants": refList(geoConstantPattern, "Geo locations that are excluded, geoTargetConstants/N"),
			"excluded_geo_match_type":       schema{"type": "string", "enum": valueRuleGeoMatchType}})),
		"device_condition": minOne(object(schema{"device_types": schema{"type": "array", "maxItems": len(valueRuleDeviceTypes),
			"uniqueItems": true, "items": schema{"type": "string", "enum": valueRuleDeviceTypes}}})),
		"audience_condition": minOne(object(schema{
			"user_lists":     refList(`^customers/[0-9]{1,10}/userLists/[0-9]{1,20}$`, "User lists of this account, customers/ID/userLists/N"),
			"user_interests": refList(`^customers/[0-9]{1,10}/userInterests/[0-9]{1,20}$`, "User interests of this account, customers/ID/userInterests/N")})),
		"itinerary_condition": minOne(object(schema{
			"advance_booking_window": minOne(object(schema{"min_days": ruleDayNumber, "max_days": ruleDayNumber})),
			"travel_length":          minOne(object(schema{"min_nights": ruleDayNumber, "max_nights": ruleDayNumber})),
			"travel_start_day": minOne(object(schema{"monday": ruleWeekday, "tuesday": ruleWeekday, "wednesday": ruleWeekday,
				"thursday": ruleWeekday, "friday": ruleWeekday, "saturday": ruleWeekday, "sunday": ruleWeekday}))})),
	}

	ruleItemSchema = func(update bool) schema {
		props := schema{}
		for k, v := range ruleFieldSchemas {
			props[k] = v
		}
		if update {
			props["conversion_value_rule_id"] = schema{"type": "string", "pattern": numericIDPattern.String()}
			return object(props, "conversion_value_rule_id")
		}
		return object(props, "action")
	}

	ruleObjectSchema = func() schema {
		props := schema{"resource_name": stringSchema, "id": stringSchema, "owner_customer": valueRuleOwnerSchema}
		for k := range ruleFieldSchemas {
			props[k] = schema{} // the nested shapes are bounded by the mapping, not by the output schema
		}
		return object(props, "resource_name")
	}()
	ruleOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema, "conversion_value_rule_id": stringSchema,
			"conversion_value_rule": ruleObjectSchema}, "resource_name", "conversion_value_rule_id"))},
		"customer_id", "validate_only", "results")

	ruleCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of " +
		"the account without hyphens; a target of the connection or an account below a manager target"}

	ruleFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One result per entry, in the order of the entries, empty after validate_only: " +
			"resource_name (customers/ID/conversionValueRules/N), conversion_value_rule_id (its number), and with " +
			"MUTABLE_RESOURCE the rule after the change (resource_name, id, owner_customer, action, status and the " +
			"four conditions, as far as Google returns them)"}}
)

const ruleSensitivity = "conversion value rules of a Google Ads account: how the value of conversions is adjusted, " +
	"which Smart Bidding optimizes for"

const ruleEffectNote = " A conversion value rule adds a value to, multiplies, or sets the value of a conversion when " +
	"its conditions hold (geo location, device, audience, itinerary); Smart Bidding optimizes for that value, so a " +
	"rule moves spend. The action is an operation (ADD, MULTIPLY, SET) with a number. Geo constants are " +
	"geoTargetConstants/N; user lists and user interests must belong to customer_id. The rules are read with " +
	"reports.search on conversion_value_rule."

const ruleSettingsDoc = "action (operation and value), status (ENABLED or PAUSED), geo_location_condition, " +
	"device_condition, audience_condition, itinerary_condition"

var conversionValueRulesCreate = capability.Descriptor{
	ID: conversionValueRulesCreateID, Version: 1, Title: "Create conversion value rules of a Google Ads account",
	Description: "Create one to 10 conversion value rules in a Google Ads account (ConversionValueRuleService." +
		"MutateConversionValueRules with create operations). Each needs an action; optional: status and the four " +
		"conditions." + ruleEffectNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "value-rules", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, ruleSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"conversion_value_rules": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "items": ruleItemSchema(false)},
		"validate_only":          validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "conversion_value_rules")),
	OutputSchema: encoded(ruleOutputSchema),
	Arguments: []capability.Argument{ruleCustomerArgument,
		{Name: "conversion_value_rules", Required: true, Description: "One to 10 rules, each with action and optionally " +
			"status, geo_location_condition, device_condition, audience_condition, itinerary_condition"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: ruleFields,
	Examples: []capability.Example{{Description: "Validate a rule that multiplies the value on mobile devices",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","conversion_value_rules":[{"action":{"operation":"MULTIPLY",` +
			`"value":1.2},"device_condition":{"device_types":["MOBILE"]}}],"validate_only":true}`)}},
}

var conversionValueRulesUpdate = capability.Descriptor{
	ID: conversionValueRulesUpdateID, Version: 1, Title: "Change conversion value rules of a Google Ads account",
	Description: "Change one to 10 existing conversion value rules of a Google Ads account (ConversionValueRuleService." +
		"MutateConversionValueRules with update operations and a field mask of exactly the given settings). Each " +
		"entry names the conversion_value_rule_id and at least one of: " + ruleSettingsDoc + ". A condition or an " +
		"action that is given replaces the whole one; settings are set, not cleared." + ruleEffectNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "value-rules", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, ruleSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"conversion_value_rules": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "items": ruleItemSchema(true)},
		"validate_only":          validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "conversion_value_rules")),
	OutputSchema: encoded(ruleOutputSchema),
	Arguments: []capability.Argument{ruleCustomerArgument,
		{Name: "conversion_value_rules", Required: true, Description: "One to 10 entries, each with " +
			"conversion_value_rule_id (the number of the rule, each once) and at least one of: " + ruleSettingsDoc},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: ruleFields,
	Examples: []capability.Example{{Description: "Pause a rule", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","conversion_value_rules":[{"conversion_value_rule_id":"123","status":"PAUSED"}]}`)}},
}

var conversionValueRulesRemove = capability.Descriptor{
	ID: conversionValueRulesRemoveID, Version: 1, Title: "Remove conversion value rules of a Google Ads account",
	Description: "Remove one to 10 conversion value rules of a Google Ads account (ConversionValueRuleService." +
		"MutateConversionValueRules with remove operations of customers/ID/conversionValueRules/ID). A removed rule is " +
		"permanently deleted, can no longer be applied and cannot be enabled again; conversions are valued without it " +
		"from then on. The tool builds the names from customer_id and the numeric IDs. The service answers the " +
		"resource names only, so there is no response_content_type." + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "value-rules", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, ruleSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"conversion_value_rule_ids": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "uniqueItems": true,
			"items": schema{"type": "string", "pattern": numericIDPattern.String()}},
		"validate_only": validateOnlySchema}, "customer_id", "conversion_value_rule_ids")),
	OutputSchema: encoded(ruleOutputSchema),
	Arguments: []capability.Argument{ruleCustomerArgument,
		{Name: "conversion_value_rule_ids", Required: true, Description: "One to 10 distinct numeric IDs of rules of the " +
			"account, as reports.search returns them"}, validateOnlyArgument},
	Fields: ruleFields,
	Examples: []capability.Example{{Description: "Validate the removal of a rule", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","conversion_value_rule_ids":["123"],"validate_only":true}`)}},
}

// --- handlers ---

func handleConversionValueRulesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionValueRulesChange(ctx, resolved, secrets, red, raw, conversionValueRulesCreateID)
}

func handleConversionValueRulesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionValueRulesChange(ctx, resolved, secrets, red, raw, conversionValueRulesUpdateID)
}

func handleConversionValueRulesRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionValueRulesChange(ctx, resolved, secrets, red, raw, conversionValueRulesRemoveID)
}

func conversionValueRulesChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, tool string) (any, error) {
	op := map[string]string{conversionValueRulesCreateID: "create conversion value rules",
		conversionValueRulesUpdateID: "update conversion value rules", conversionValueRulesRemoveID: "remove conversion value rules"}[tool]
	var args valueRuleArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
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
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: args.CustomerID,
		service: valueRuleService, collection: valueRuleCollection, resourceKey: "conversionValueRule",
		operations: operations, names: names, validateOnly: args.ValidateOnly, content: args.ResponseContentType})
	if err != nil {
		return nil, err
	}
	out := valueRulesChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Results: []valueRuleResult{}}
	for _, r := range results {
		item := valueRuleResult{ResourceName: r.ResourceName, ConversionValueRuleID: r.ID}
		if len(r.Resource) > 0 {
			rule, ok := valueRuleOf(r.Resource, r.ResourceName, r.ID, args.CustomerID)
			if !ok {
				return nil, unsureResult(op, "answered with a rule that is not the one of its result")
			}
			item.ConversionValueRule = rule
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// conversionValueRulesSummary phrases the change for the confirmation, from the arguments alone.
func conversionValueRulesSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a valueRuleArguments
		_ = json.Unmarshal(raw, &a)
		lead := ""
		if a.ValidateOnly {
			lead = "Only validate: "
		}
		where := " of Google Ads account " + a.CustomerID
		if tool == conversionValueRulesRemoveID {
			return lead + "Remove " + strconv.Itoa(len(a.IDs)) + " conversion value rule(s) (" + strings.Join(a.IDs, ", ") +
				")" + where + ": they are deleted for good and conversions are valued without them."
		}
		verb := "Change"
		if tool == conversionValueRulesCreateID {
			verb = "Create"
		}
		var parts []string
		for _, r := range a.Rules {
			part := r.ID
			if r.Action != nil && r.Action.Value != nil {
				part += " " + r.Action.Operation + " " + strconv.FormatFloat(*r.Action.Value, 'g', -1, 64)
			}
			if r.Status != "" {
				part += " status=" + r.Status
			}
			parts = append(parts, strings.TrimSpace(part))
		}
		return lead + verb + " " + strconv.Itoa(len(a.Rules)) + " conversion value rule(s)" + where + " (" +
			strings.Join(parts, "; ") + "). This changes the conversion value that Smart Bidding optimizes for."
	}
}
