package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	seasonalityCreateID = Provider + ".bidding-seasonality-adjustments.create"
	seasonalityUpdateID = Provider + ".bidding-seasonality-adjustments.update"
	seasonalityRemoveID = Provider + ".bidding-seasonality-adjustments.remove"

	seasonalityService    = "/biddingSeasonalityAdjustments"
	seasonalityCollection = "biddingSeasonalityAdjustments"
)

// Bounds of the BiddingSeasonalityAdjustment resource of v26: name at most 255 and description at most 2048
// characters, conversion_rate_modifier from 0.1 to 10.0, at most 2000 campaigns per adjustment, and a
// [start_date_time, end_date_time) interval within (0, 14 days]. The resource cannot be used in manager accounts.
const (
	seasonalityNameMax        = 255
	seasonalityDescriptionMax = 2048
	seasonalityCampaignsMax   = 2000
	seasonalityMaxLength      = 14 * 24 * time.Hour
	seasonalityTimeLayout     = "2006-01-02 15:04:05"
	// maxSeasonalityAdjustments is Qatlas's own bound per call; Google accepts up to 10,000 operations per request.
	maxSeasonalityAdjustments = 10
)

// seasonalityChannels are the channel types a seasonality adjustment supports.
var seasonalityChannels = []string{"DISPLAY", "SEARCH", "SHOPPING"}

func seasonalityText(limit int) func(argument, value string) error {
	return func(argument, value string) error {
		if n := utf8.RuneCountInString(value); n < 1 || n > limit || hasControl(value) || !utf8.ValidString(value) {
			return badRequest(argument + " must be text of 1 to " + strconv.Itoa(limit) + " characters without control characters")
		}
		return nil
	}
}

// seasonalityTree is the members of the resource that the tools offer. Not offered: the ID and the status, which
// Google fills, and the scope CUSTOMER, which Google refuses in a mutate request.
var seasonalityTree = func() node {
	name := textNode("name", "Name of the adjustment, 1 to 255 characters; an empty text clears it on a change", seasonalityNameMax)
	name.check, name.clears = seasonalityText(seasonalityNameMax), true
	description := textNode("description", "Description, 1 to 2048 characters; an empty text clears it on a change",
		seasonalityDescriptionMax)
	description.check, description.clears = seasonalityText(seasonalityDescriptionMax), true
	modifier := node{name: "conversion_rate_modifier", kind: kindNumber, bounded: true, low: 0.1, high: 10,
		desc: "Expected change of the conversion rate, 0.1 to 10.0; 1.0 or unset applies no adjustment"}
	scope := slices.DeleteFunc(enumOf("bidding_seasonality_adjustment.scope"), func(v string) bool { return v == "CUSTOMER" })
	return objectNode("", "",
		name, description,
		enumNode("scope", "CAMPAIGN applies to the campaigns listed, CHANNEL to the campaigns of the channel types listed", scope),
		dateTimeNode("start_date_time", "Inclusive start, yyyy-MM-dd HH:mm:ss in the time zone of the account; the event "+
			"should lie in the future", false),
		dateTimeNode("end_date_time", "Exclusive end, yyyy-MM-dd HH:mm:ss; the interval to the start is longer than 0 and at most 14 days", false),
		listOf(enumNode("devices", "Devices the adjustment applies to; none or an empty list on a change means all devices",
			enumOf("bidding_seasonality_adjustment.devices")), 10),
		modifier,
		listOf(resourceNode("campaigns", "Campaigns of this account, customers/ID/campaigns/N, for the scope CAMPAIGN; at most 2000, "+
			"never together with advertising_channel_types", "campaigns"), seasonalityCampaignsMax),
		listOf(enumNode("advertising_channel_types", "Channel types for the scope CHANNEL, never together with campaigns",
			seasonalityChannels), len(seasonalityChannels)),
	)
}()

// seasonalityOutputTree is the adjustment Google returns with MUTABLE_RESOURCE.
var seasonalityOutputTree = func() node {
	t := seasonalityTree
	t.fields = append(slices.Clone(t.fields), wholeNode("seasonality_adjustment_id", "", 1, 1<<62),
		enumNode("status", "", nil), resourceNode("resource_name", "", seasonalityCollection))
	return t
}()

var seasonalityResourceName = resourceNode("resource_name", "Adjustment of this account, customers/ID/biddingSeasonalityAdjustments/N",
	seasonalityCollection)

const seasonalitySensitivity = "seasonality adjustments of a Google Ads account: names, descriptions, periods, " +
	"conversion rate modifiers and the campaigns they apply to"

type seasonalityArguments struct {
	CustomerID          string   `json:"customer_id"`
	Adjustments         []any    `json:"adjustments"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readSeasonalityArguments(op string, raw json.RawMessage) (seasonalityArguments, error) {
	var a seasonalityArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

// nonEmptyList reports whether the member is a list with entries.
func nonEmptyList(in map[string]any, key string) bool {
	items, _ := in[key].([]any)
	return len(items) > 0
}

// seasonalityRules checks what the tree cannot say: the dates are real, the interval is within (0, 14 days], the
// scope matches the list given, and campaigns and channel types never come together.
func seasonalityRules(in map[string]any, at string) error {
	var start, end time.Time
	var haveStart, haveEnd bool
	for _, key := range []string{"start_date_time", "end_date_time"} {
		text, given := in[key].(string)
		if !given {
			continue
		}
		parsed, err := time.Parse(seasonalityTimeLayout, text)
		if err != nil {
			return badRequest(at + "." + key + " must be a real date and time as yyyy-MM-dd HH:mm:ss")
		}
		if key == "start_date_time" {
			start, haveStart = parsed, true
		} else {
			end, haveEnd = parsed, true
		}
	}
	if haveStart && haveEnd && (!end.After(start) || end.Sub(start) > seasonalityMaxLength) {
		return badRequest(at + ": the interval from start_date_time to end_date_time must be longer than 0 and at most 14 days")
	}
	if nonEmptyList(in, "campaigns") && nonEmptyList(in, "advertising_channel_types") {
		return badRequest(at + ": campaigns and advertising_channel_types cannot be given together")
	}
	if scope, _ := in["scope"].(string); scope == "CAMPAIGN" && nonEmptyList(in, "advertising_channel_types") {
		return badRequest(at + ": the scope CAMPAIGN takes campaigns, not advertising_channel_types")
	} else if scope == "CHANNEL" && nonEmptyList(in, "campaigns") {
		return badRequest(at + ": the scope CHANNEL takes advertising_channel_types, not campaigns")
	}
	return nil
}

func seasonalityCreateOperations(customerID string, items []any) ([]any, error) {
	if len(items) == 0 || len(items) > maxSeasonalityAdjustments {
		return nil, badRequest("adjustments must hold one to " + strconv.Itoa(maxSeasonalityAdjustments) + " adjustments")
	}
	out := make([]any, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("adjustments[%d]", i)}
		if !ok {
			return nil, badRequest(at.snake + " must be an object")
		}
		for _, required := range []string{"scope", "start_date_time", "end_date_time"} {
			if _, found := in[required]; !found {
				return nil, badRequest(at.snake + "." + required + " is required")
			}
		}
		if err := seasonalityRules(in, at.snake); err != nil {
			return nil, err
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(seasonalityTree, in, at)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"create": body})
	}
	return out, nil
}

func seasonalityUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxSeasonalityAdjustments {
		return nil, nil, badRequest("adjustments must hold one to " + strconv.Itoa(maxSeasonalityAdjustments) + " adjustments")
	}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("adjustments[%d]", i)}
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		name, _ := in["resource_name"].(string)
		if !ownedResource(name, customerID, seasonalityCollection) {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) + "/" +
				seasonalityCollection + "/N of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("an adjustment appears in adjustments only once")
		}
		fields := map[string]any{}
		for key, value := range in {
			if key != "resource_name" {
				fields[key] = value
			}
		}
		if len(fields) == 0 {
			return nil, nil, badRequest(at.snake + " must give at least one member to change")
		}
		if err := seasonalityRules(fields, at.snake); err != nil {
			return nil, nil, err
		}
		b := builder{customerID: customerID}
		body, err := b.object(seasonalityTree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

func seasonalityRemoveOperations(customerID string, resourceNames []string) ([]any, error) {
	if len(resourceNames) == 0 || len(resourceNames) > maxSeasonalityAdjustments {
		return nil, badRequest("resource_names must hold one to " + strconv.Itoa(maxSeasonalityAdjustments) + " adjustments")
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if !ownedResource(name, customerID, seasonalityCollection) {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/%s/N of the account of customer_id",
				i, queryNumber(customerID), seasonalityCollection))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("an adjustment appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	seasonalityResult struct {
		ResourceName string         `json:"resource_name"`
		Adjustment   map[string]any `json:"adjustment,omitempty"`
	}
	seasonalityChanged struct {
		CustomerID   string              `json:"customer_id"`
		ValidateOnly bool                `json:"validate_only"`
		Results      []seasonalityResult `json:"results"`
	}
	seasonalityAnswer struct {
		Results []struct {
			ResourceName string          `json:"resourceName"`
			Adjustment   json.RawMessage `json:"biddingSeasonalityAdjustment"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
)

// seasonalityOf maps the results of a change: one per operation in the order sent, each an adjustment of the
// account, and for an update and a remove exactly the one asked for. Anything else leaves the outcome open,
// because the request was sent.
func seasonalityOf(op string, answer seasonalityAnswer, customerID string, validateOnly bool, expected []string,
	count int) (*seasonalityChanged, error) {
	out := &seasonalityChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []seasonalityResult{}}
	invalid := func(what string) error {
		return unsure(provider.InvalidResponse(op, subject+" answered with "+what), true)
	}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, invalid("a partial failure although none was asked for")
	}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != count {
		return nil, invalid("another number of results than operations")
	}
	seen := map[string]bool{}
	for i, r := range answer.Results {
		if !ownedResource(r.ResourceName, customerID, seasonalityCollection) ||
			(expected != nil && r.ResourceName != expected[i]) || seen[r.ResourceName] {
			return nil, invalid("a result for another adjustment or account than the one asked for")
		}
		seen[r.ResourceName] = true
		item := seasonalityResult{ResourceName: r.ResourceName}
		if len(r.Adjustment) > 0 && string(r.Adjustment) != "null" {
			raw, ok := decodeObject(r.Adjustment)
			if ok {
				var members map[string]any
				if members, ok = outputMembers(seasonalityOutputTree.fields, raw, customerID); ok {
					name, _ := members["resource_name"].(string)
					ok = name == "" || name == r.ResourceName
					item.Adjustment = members
				}
			}
			if !ok {
				return nil, invalid("an adjustment that is not the one asked for")
			}
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptors ---

func seasonalityInputSchema(create bool) schema {
	var item schema
	if create {
		item = seasonalityTree.inputSchema()
		item["required"] = []string{"scope", "start_date_time", "end_date_time"}
		delete(item, "description")
	} else {
		item = seasonalityTree.inputSchema()
		delete(item, "description")
		item["properties"].(schema)["resource_name"] = seasonalityResourceName.inputSchema()
		item["required"] = []string{"resource_name"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"adjustments":   schema{"type": "array", "minItems": 1, "maxItems": maxSeasonalityAdjustments, "items": item},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "adjustments")
}

var seasonalityOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "adjustment": seasonalityOutputTree.outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results")

var seasonalityFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per adjustment, in the order given, with its resource_name " +
		"(customers/ID/biddingSeasonalityAdjustments/N, always of the account of the call) and, with MUTABLE_RESOURCE, " +
		"the adjustment after the change: the members the tool offers plus seasonality_adjustment_id and status as " +
		"Google returns them; empty after validate_only"}}

var seasonalityCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit " +
	"customer ID of the account without hyphens; a target of the connection or an account below a manager target. " +
	"Seasonality adjustments cannot be used in manager accounts"}

const seasonalityNote = " A seasonality adjustment tells Smart Bidding that the conversion rate changes for a short, " +
	"known event: during the period the bids of the campaigns it covers change with conversion_rate_modifier, so " +
	"the spend changes with it. It is forward looking and lasts more than 0 and at most 14 days. It applies to the " +
	"campaigns listed (scope CAMPAIGN, at most 2000) or to all campaigns of the channel types listed (scope CHANNEL: " +
	"DISPLAY, SEARCH, SHOPPING), never both. Resource names in the arguments must belong to customer_id. Up to 10 " +
	"adjustments per call, one atomic request to BiddingSeasonalityAdjustmentService without partial failure. The " +
	"adjustments are read with reports.search on bidding_seasonality_adjustment."

var seasonalityCreate = capability.Descriptor{
	ID: seasonalityCreateID, Version: 1, Title: "Create bidding seasonality adjustments",
	Description: "Create up to 10 bidding seasonality adjustments in a Google Ads account " +
		"(BiddingSeasonalityAdjustmentService.MutateBiddingSeasonalityAdjustments with create operations): Google " +
		"applies all of them or none. Each needs scope (CAMPAIGN or CHANNEL, not CUSTOMER, which Google refuses), " +
		"start_date_time and end_date_time; name, description, devices, conversion_rate_modifier and the campaigns or " +
		"channel types are optional." + seasonalityNote + mutateNote,
	Tags: []string{"google", "ads", "bidding", "seasonality", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, seasonalitySensitivity),
	InputSchema: encoded(seasonalityInputSchema(true)), OutputSchema: encoded(seasonalityOutputSchema),
	Arguments: []capability.Argument{seasonalityCustomerArgument,
		{Name: "adjustments", Required: true, Description: "One to 10 adjustments, each an object of the members of the " +
			"BiddingSeasonalityAdjustment resource: scope, start_date_time and end_date_time (required) and the optional rest"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: seasonalityFields,
	Examples: []capability.Example{{Description: "Validate a three-day adjustment of one campaign without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","adjustments":[{"name":"Spring sale","scope":"CAMPAIGN",` +
			`"start_date_time":"2027-03-01 00:00:00","end_date_time":"2027-03-04 00:00:00","conversion_rate_modifier":1.5,` +
			`"campaigns":["customers/1234567890/campaigns/21"]}],"validate_only":true}`)}},
}

var seasonalityUpdate = capability.Descriptor{
	ID: seasonalityUpdateID, Version: 1, Title: "Change bidding seasonality adjustments",
	Description: "Change up to 10 bidding seasonality adjustments of a Google Ads account " +
		"(BiddingSeasonalityAdjustmentService.MutateBiddingSeasonalityAdjustments with update operations and a field mask " +
		"that names exactly the members given): Google applies all of them or none. Each entry names the adjustment by " +
		"resource_name and the members to change; a list member replaces the whole list, and an empty list or empty " +
		"text clears it. Google decides which members of an adjustment it accepts to change." + seasonalityNote + mutateNote,
	Tags: []string{"google", "ads", "bidding", "seasonality", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, seasonalitySensitivity),
	InputSchema: encoded(seasonalityInputSchema(false)), OutputSchema: encoded(seasonalityOutputSchema),
	Arguments: []capability.Argument{seasonalityCustomerArgument,
		{Name: "adjustments", Required: true, Description: "One to 10 entries: resource_name of the adjustment and at least " +
			"one member to change"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: seasonalityFields,
	Examples: []capability.Example{{Description: "Raise the modifier of an adjustment", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","adjustments":[{"resource_name":"customers/1234567890/biddingSeasonalityAdjustments/5",` +
			`"conversion_rate_modifier":2}]}`)}},
}

var seasonalityRemove = capability.Descriptor{
	ID: seasonalityRemoveID, Version: 1, Title: "Remove bidding seasonality adjustments",
	Description: "Remove up to 10 bidding seasonality adjustments of a Google Ads account " +
		"(BiddingSeasonalityAdjustmentService.MutateBiddingSeasonalityAdjustments with remove operations): Google applies " +
		"all of them or none. A removed adjustment no longer changes bids; no tool of this provider brings it back. " +
		"Resource names must be adjustments of customer_id. Offers no response_content_type: a removed adjustment has no " +
		"mutable resource to show." + seasonalityNote + mutateNote,
	Tags: []string{"google", "ads", "bidding", "seasonality", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, seasonalitySensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxSeasonalityAdjustments, "uniqueItems": true,
			"items": seasonalityResourceName.inputSchema()},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(seasonalityOutputSchema),
	Arguments: []capability.Argument{seasonalityCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 adjustments of this account, " +
			"customers/ID/biddingSeasonalityAdjustments/N, each once"}, validateOnlyArgument},
	Fields: seasonalityFields,
	Examples: []capability.Example{{Description: "Validate the removal of an adjustment without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":` +
			`["customers/1234567890/biddingSeasonalityAdjustments/5"],"validate_only":true}`)}},
}

// --- handlers ---

func handleSeasonalityCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return seasonalityChange(ctx, resolved, secrets, red, raw, "create seasonality adjustments", seasonalityCreateID)
}

func handleSeasonalityUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return seasonalityChange(ctx, resolved, secrets, red, raw, "update seasonality adjustments", seasonalityUpdateID)
}

func handleSeasonalityRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return seasonalityChange(ctx, resolved, secrets, red, raw, "remove seasonality adjustments", seasonalityRemoveID)
}

func seasonalityChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readSeasonalityArguments(op, raw)
	if err != nil {
		return nil, err
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	var ops []any
	var expected []string
	switch tool {
	case seasonalityCreateID:
		ops, err = seasonalityCreateOperations(a.CustomerID, a.Adjustments)
	case seasonalityUpdateID:
		ops, expected, err = seasonalityUpdateOperations(a.CustomerID, a.Adjustments)
	default:
		ops, err = seasonalityRemoveOperations(a.CustomerID, a.ResourceNames)
		expected = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer seasonalityAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: seasonalityService,
		body: mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)}, &answer); err != nil {
		return nil, err
	}
	return seasonalityOf(op, answer, a.CustomerID, a.ValidateOnly, expected, len(ops))
}

// --- confirmation ---

// seasonalitySummary phrases the change for the confirmation, from the arguments alone.
func seasonalitySummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readSeasonalityArguments("", raw)
		verb := map[string]string{seasonalityCreateID: "Create", seasonalityUpdateID: "Change", seasonalityRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		var parts []string
		for _, name := range a.ResourceNames[:min(len(a.ResourceNames), 10)] {
			parts = append(parts, boundedText(name, maxCodeText))
		}
		for _, item := range a.Adjustments[:min(len(a.Adjustments), 10)] {
			in, _ := item.(map[string]any)
			part := stringOf(in["name"])
			if part != "" {
				part = strconv.Quote(boundedText(part, seasonalityNameMax*4))
			} else {
				part = boundedText(stringOf(in["resource_name"]), maxCodeText)
			}
			var facts []string
			for _, key := range []string{"scope", "start_date_time", "end_date_time"} {
				if v := stringOf(in[key]); v != "" {
					facts = append(facts, key+" "+boundedText(v, maxCodeText))
				}
			}
			if v, ok := in["conversion_rate_modifier"].(json.Number); ok {
				facts = append(facts, "conversion_rate_modifier "+boundedText(v.String(), maxCodeText))
			}
			if list, ok := in["campaigns"].([]any); ok {
				facts = append(facts, strconv.Itoa(len(list))+" campaign(s)")
			}
			if list, ok := in["advertising_channel_types"].([]any); ok {
				var types []string
				for _, t := range list {
					types = append(types, boundedText(stringOf(t), maxCodeText))
				}
				facts = append(facts, "channels "+strings.Join(types, "+"))
			}
			if tool == seasonalityUpdateID {
				var keys []string
				for key := range in {
					if key != "resource_name" {
						keys = append(keys, key)
					}
				}
				slices.Sort(keys)
				facts = append(facts, "members "+strings.Join(keys, "+"))
			}
			if len(facts) > 0 {
				part += " (" + strings.Join(facts, ", ") + ")"
			}
			parts = append(parts, part)
		}
		more := ""
		if n := len(a.Adjustments) + len(a.ResourceNames); n > len(parts) {
			more = " and " + strconv.Itoa(n-len(parts)) + " more"
		}
		summary := verb + " seasonality adjustments of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + more + "."
		if tool == seasonalityRemoveID {
			return summary + " A removed adjustment no longer changes bids; no tool brings it back."
		}
		return summary + " During its period Smart Bidding changes the bids of the campaigns it covers, which changes spend."
	}
}
