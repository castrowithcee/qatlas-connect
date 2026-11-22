package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	adGroupBidModifiersCreateID = Provider + ".ad-group-bid-modifiers.create"
	adGroupBidModifiersUpdateID = Provider + ".ad-group-bid-modifiers.update"
	adGroupBidModifiersRemoveID = Provider + ".ad-group-bid-modifiers.remove"

	adGroupBidModifierService = "/adGroupBidModifiers"

	adGroupBidModifierCollection = "adGroupBidModifiers"
)

// adGroupBidModifierName is the form of a resource name: ad group ID and criterion ID joined by a tilde.
var adGroupBidModifierName = regexp.MustCompile(`^customers/[0-9]{1,10}/adGroupBidModifiers/([0-9]{1,20})~([0-9]{1,20})$`)

var adGroupBidModifierResourceName = schema{"type": "string", "maxLength": 100,
	"pattern":     adGroupBidModifierName.String(),
	"description": "Ad group bid modifier of this account, customers/ID/adGroupBidModifiers/AdGroupID~CriterionID"}

const adGroupBidModifierSensitivity = "bid modifiers of ad groups of a Google Ads account: how much more or less the " +
	"bids of an ad group are for a device or a booking window"

// adGroupBidModifierParts returns the ad group ID and the criterion ID of a resource name that is an ad group
// bid modifier of exactly the account, both positive. A temporary (negative) ID never matches.
func adGroupBidModifierParts(name, customerID string) (adGroup, criterion string, ok bool) {
	m := adGroupBidModifierName.FindStringSubmatch(name)
	if m == nil || !strings.HasPrefix(name, customerName(customerID)+"/"+adGroupBidModifierCollection+"/") ||
		strings.Trim(m[1], "0") == "" || strings.Trim(m[2], "0") == "" {
		return "", "", false
	}
	return m[1], m[2], true
}

// bidModifierAdGroupID returns the ID of customers/ID/adGroups/N when it belongs to the account.
func bidModifierAdGroupID(name, customerID string) (string, bool) {
	if !ownedResource(name, customerID, "adGroups") {
		return "", false
	}
	return name[strings.LastIndex(name, "/")+1:], true
}

type adGroupBidModifierArguments struct {
	CustomerID          string   `json:"customer_id"`
	AdGroupBidModifiers []any    `json:"ad_group_bid_modifiers"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readAdGroupBidModifierArguments(op string, raw json.RawMessage) (adGroupBidModifierArguments, error) {
	var a adGroupBidModifierArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

var adGroupBidModifierRequired = []string{"ad_group", "bid_modifier"}

// checkBidModifierValue applies the range of the reference: 0.1 to 10.0, and 0 only to a device. On an update
// the criterion is not known, so a 0 passes and Google decides.
func checkBidModifierValue(at string, value any, device, create bool) error {
	number, ok := value.(json.Number)
	if !ok {
		return nil // the builder refuses what is no number
	}
	v, err := strconv.ParseFloat(string(number), 64)
	if err != nil {
		return nil
	}
	switch {
	case v == 0 && create && !device:
		return badRequest(at + " may be 0 only for the device criterion, which 0 opts out; otherwise it must be from 0.1 to 10.0")
	case v != 0 && v < minBidModifier:
		return badRequest(at + " must be from 0.1 to 10.0, or 0 for a device that is opted out")
	}
	return nil
}

// adGroupBidModifierCreateOperations checks every bid modifier to create and builds its operation. Exactly one
// criterion names what the modifier applies to. It also returns the ID of the ad group of each, for the check
// of the answer.
func adGroupBidModifierCreateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAdGroupBidModifiers {
		return nil, nil, badRequest("ad_group_bid_modifiers must hold one to 10 bid modifiers")
	}
	out := make([]any, 0, len(items))
	groups := make([]string, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("ad_group_bid_modifiers[%d]", i)}
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		for _, required := range adGroupBidModifierRequired {
			if _, found := in[required]; !found {
				return nil, nil, badRequest(at.snake + "." + required + " is required")
			}
		}
		criteria := 0
		for _, key := range adGroupBidModifierCriteria {
			if _, found := in[key]; found {
				criteria++
			}
		}
		if criteria != 1 {
			return nil, nil, badRequest(at.snake + " needs exactly one criterion of " +
				strings.Join(adGroupBidModifierCriteria, ", "))
		}
		_, device := in["device"]
		if err := checkBidModifierValue(at.snake+".bid_modifier", in["bid_modifier"], device, true); err != nil {
			return nil, nil, err
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(adGroupBidModifierTree, in, at)
		if err != nil {
			return nil, nil, err
		}
		group, _ := bidModifierAdGroupID(stringOf(in["ad_group"]), customerID)
		groups = append(groups, group)
		out = append(out, map[string]any{"create": body})
	}
	return out, groups, nil
}

// adGroupBidModifierUpdateOperations builds one update with a field mask per bid modifier. Only bid_modifier
// can change: the ad group and the criterion are immutable. Each bid modifier is named once.
func adGroupBidModifierUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAdGroupBidModifiers {
		return nil, nil, badRequest("ad_group_bid_modifiers must hold one to 10 bid modifiers")
	}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("ad_group_bid_modifiers[%d]", i)}
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		name, _ := in["resource_name"].(string)
		if _, _, owned := adGroupBidModifierParts(name, customerID); !owned {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/adGroupBidModifiers/AdGroupID~CriterionID of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("an ad group bid modifier appears in ad_group_bid_modifiers only once")
		}
		fields := map[string]any{}
		for key, value := range in {
			if key != "resource_name" {
				fields[key] = value
			}
		}
		if len(fields) == 0 {
			return nil, nil, badRequest(at.snake + " must give bid_modifier")
		}
		if err := checkBidModifierValue(at.snake+".bid_modifier", fields["bid_modifier"], false, false); err != nil {
			return nil, nil, err
		}
		b := builder{customerID: customerID}
		body, err := b.object(adGroupBidModifierTree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

func adGroupBidModifierRemoveOperations(customerID string, resourceNames []string) ([]any, error) {
	if len(resourceNames) == 0 || len(resourceNames) > maxAdGroupBidModifiers {
		return nil, badRequest("resource_names must hold one to 10 bid modifiers")
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if _, _, owned := adGroupBidModifierParts(name, customerID); !owned {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/adGroupBidModifiers/"+
				"AdGroupID~CriterionID of the account of customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("an ad group bid modifier appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	adGroupBidModifierResult struct {
		ResourceName       string         `json:"resource_name"`
		AdGroupBidModifier map[string]any `json:"ad_group_bid_modifier,omitempty"`
	}
	adGroupBidModifiersChanged struct {
		CustomerID   string                     `json:"customer_id"`
		ValidateOnly bool                       `json:"validate_only"`
		Results      []adGroupBidModifierResult `json:"results"`
	}
)

// adGroupBidModifierAnswer is the answer of MutateAdGroupBidModifiers.
type adGroupBidModifierAnswer struct {
	Results []struct {
		ResourceName       string          `json:"resourceName"`
		AdGroupBidModifier json.RawMessage `json:"adGroupBidModifier"`
	} `json:"results"`
	PartialFailureError json.RawMessage `json:"partialFailureError"`
}

// adGroupBidModifiersOf maps the results of a change. One result per operation, in the order sent, each a bid
// modifier of the account; for an update and a remove it is the one asked for, for a create it is one of the
// ad group asked for. Anything else is an invalid answer with an unknown outcome, since the change may have
// been made.
func adGroupBidModifiersOf(op string, answer adGroupBidModifierAnswer, customerID string, validateOnly bool,
	expected, groups []string, count int) (*adGroupBidModifiersChanged, error) {
	out := &adGroupBidModifiersChanged{CustomerID: customerID, ValidateOnly: validateOnly,
		Results: []adGroupBidModifierResult{}}
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
	for i, r := range answer.Results {
		group, _, owned := adGroupBidModifierParts(r.ResourceName, customerID)
		if !owned || (expected != nil && r.ResourceName != expected[i]) || (groups != nil && group != groups[i]) {
			return nil, invalid("a result for another bid modifier, ad group, or account than the one asked for")
		}
		item := adGroupBidModifierResult{ResourceName: r.ResourceName}
		if len(r.AdGroupBidModifier) > 0 && string(r.AdGroupBidModifier) != "null" {
			raw, ok := decodeObject(r.AdGroupBidModifier)
			if ok {
				var members map[string]any
				if members, ok = outputMembers(adGroupBidModifierOutputTree.fields, raw, customerID); ok {
					name, _ := members["resource_name"].(string)
					inner, _ := bidModifierAdGroupID(stringOf(members["ad_group"]), customerID)
					ok = (name == "" || name == r.ResourceName) &&
						(members["ad_group"] == nil || inner == group)
					item.AdGroupBidModifier = members
				}
			}
			if !ok {
				return nil, invalid("a bid modifier that is not the one asked for")
			}
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptors ---

func adGroupBidModifierInputSchema(create bool) schema {
	var item schema
	if create {
		item = adGroupBidModifierTree.inputSchema()
		item["required"] = adGroupBidModifierRequired
		delete(item, "description")
	} else {
		item = adGroupBidModifierTree.mutable().inputSchema()
		props := item["properties"].(schema)
		props["resource_name"] = adGroupBidModifierResourceName
		item["required"] = []string{"resource_name", "bid_modifier"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"ad_group_bid_modifiers": schema{"type": "array", "minItems": 1, "maxItems": maxAdGroupBidModifiers, "items": item},
		"validate_only":          validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "ad_group_bid_modifiers")
}

var adGroupBidModifierOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema,
		"ad_group_bid_modifier": adGroupBidModifierOutputTree.outputSchema()}, "resource_name"))},
	"customer_id", "validate_only", "results")

var adGroupBidModifierFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per bid modifier, in the order given, with its resource_name and, " +
		"with MUTABLE_RESOURCE, the bid modifier after the change: the members the tool offers and the ones " +
		"Google fills itself (criterion_id, base_ad_group, bid_modifier_source); empty after validate_only"}}

var adGroupBidModifierCustomerArgument = capability.Argument{Name: "customer_id", Required: true,
	Description: "Ten-digit customer ID of the account without hyphens; a target of the connection or an account " +
		"below a manager target"}

const adGroupBidModifierSpendNote = "A bid modifier changes the bids of an ad group where its criterion applies, so " +
	"it changes how much each click costs and how much the ad group spends; the call is confirmed per call. " +
	"bid_modifier is from 0.1 to 10.0, and 0 opts out of a device. Resource names in the arguments (ad_group, " +
	"resource_names) must belong to customer_id. Up to 10 bid modifiers per call, one atomic request without " +
	"partial failure."

var adGroupBidModifiersCreate = capability.Descriptor{
	ID: adGroupBidModifiersCreateID, Version: 1, Title: "Create Google Ads ad group bid modifiers",
	Description: "Create up to 10 bid modifiers of ad groups in a Google Ads account " +
		"(AdGroupBidModifierService.MutateAdGroupBidModifiers with create operations): Google applies all of them " +
		"or none. Each needs ad_group, bid_modifier, and exactly one criterion: device, advance_booking_window, " +
		"length_of_booking, start_date_range, start_day_of_week, or user_set_dates, as the AdGroupBidModifier " +
		"resource of v26 has them. " + adGroupBidModifierSpendNote +
		" Not offered: the members Google fills itself (criterion_id, base_ad_group, bid_modifier_source), " +
		"which an answer shows." + mutateNote,
	Tags: []string{"google", "ads", "ad-group-bid-modifiers", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, adGroupBidModifierSensitivity),
	InputSchema: encoded(adGroupBidModifierInputSchema(true)), OutputSchema: encoded(adGroupBidModifierOutputSchema),
	Arguments: []capability.Argument{adGroupBidModifierCustomerArgument,
		{Name: "ad_group_bid_modifiers", Required: true, Description: "One to 10 bid modifiers, each an object with " +
			"ad_group, bid_modifier, and exactly one criterion object"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: adGroupBidModifierFields,
	Examples: []capability.Example{{Description: "Validate a bid 20 percent higher on mobile devices without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ad_group_bid_modifiers":[{"ad_group":` +
			`"customers/1234567890/adGroups/31","bid_modifier":1.2,"device":{"type":"MOBILE"}}],"validate_only":true}`)}},
}

var adGroupBidModifiersUpdate = capability.Descriptor{
	ID: adGroupBidModifiersUpdateID, Version: 1, Title: "Change Google Ads ad group bid modifiers",
	Description: "Change the bid_modifier of up to 10 ad group bid modifiers of a Google Ads account " +
		"(AdGroupBidModifierService.MutateAdGroupBidModifiers with update operations and a field mask that names " +
		"bid_modifier): Google applies all of them or none. Each entry names the bid modifier by resource_name. " +
		"The ad group and the criterion are fixed after creation, so they are not offered: remove the bid " +
		"modifier and create another instead. " + adGroupBidModifierSpendNote + mutateNote,
	Tags: []string{"google", "ads", "ad-group-bid-modifiers", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, adGroupBidModifierSensitivity),
	InputSchema: encoded(adGroupBidModifierInputSchema(false)), OutputSchema: encoded(adGroupBidModifierOutputSchema),
	Arguments: []capability.Argument{adGroupBidModifierCustomerArgument,
		{Name: "ad_group_bid_modifiers", Required: true, Description: "One to 10 entries: resource_name of the bid " +
			"modifier and its new bid_modifier"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: adGroupBidModifierFields,
	Examples: []capability.Example{{Description: "Lower a bid modifier to 0.8", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","ad_group_bid_modifiers":[{"resource_name":` +
			`"customers/1234567890/adGroupBidModifiers/31~30001","bid_modifier":0.8}]}`)}},
}

var adGroupBidModifiersRemove = capability.Descriptor{
	ID: adGroupBidModifiersRemoveID, Version: 1, Title: "Remove Google Ads ad group bid modifiers",
	Description: "Remove up to 10 ad group bid modifiers of a Google Ads account " +
		"(AdGroupBidModifierService.MutateAdGroupBidModifiers with remove operations): Google applies all of them " +
		"or none. A removed bid modifier no longer adjusts the bids of its ad group, which changes what the ad " +
		"group spends. Resource names must be bid modifiers of customer_id. Offers no response_content_type: a " +
		"removed bid modifier has no mutable resource to show." + mutateNote,
	Tags: []string{"google", "ads", "ad-group-bid-modifiers", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, adGroupBidModifierSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxAdGroupBidModifiers,
			"items": adGroupBidModifierResourceName},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(adGroupBidModifierOutputSchema),
	Arguments: []capability.Argument{adGroupBidModifierCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 bid modifiers of this account, " +
			"customers/ID/adGroupBidModifiers/AdGroupID~CriterionID"}, validateOnlyArgument},
	Fields: adGroupBidModifierFields,
	Examples: []capability.Example{{Description: "Validate the removal of a bid modifier without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/` +
			`adGroupBidModifiers/31~30001"],"validate_only":true}`)}},
}

// --- handlers ---

func handleAdGroupBidModifiersCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupBidModifiersChange(ctx, resolved, secrets, red, raw, "create ad group bid modifiers",
		adGroupBidModifiersCreateID)
}

func handleAdGroupBidModifiersUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupBidModifiersChange(ctx, resolved, secrets, red, raw, "update ad group bid modifiers",
		adGroupBidModifiersUpdateID)
}

func handleAdGroupBidModifiersRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupBidModifiersChange(ctx, resolved, secrets, red, raw, "remove ad group bid modifiers",
		adGroupBidModifiersRemoveID)
}

func adGroupBidModifiersChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, op, tool string) (any, error) {
	a, err := readAdGroupBidModifierArguments(op, raw)
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
	var expected, groups []string
	switch tool {
	case adGroupBidModifiersCreateID:
		ops, groups, err = adGroupBidModifierCreateOperations(a.CustomerID, a.AdGroupBidModifiers)
	case adGroupBidModifiersUpdateID:
		ops, expected, err = adGroupBidModifierUpdateOperations(a.CustomerID, a.AdGroupBidModifiers)
	default:
		ops, err = adGroupBidModifierRemoveOperations(a.CustomerID, a.ResourceNames)
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
	var answer adGroupBidModifierAnswer
	body := mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: adGroupBidModifierService,
		body: body}, &answer); err != nil {
		return nil, err
	}
	return adGroupBidModifiersOf(op, answer, a.CustomerID, a.ValidateOnly, expected, groups, len(ops))
}

// --- confirmation ---

// adGroupBidModifierSummary phrases the change for the confirmation, from the arguments alone.
func adGroupBidModifierSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readAdGroupBidModifierArguments("", raw)
		verb := map[string]string{adGroupBidModifiersCreateID: "Create", adGroupBidModifiersUpdateID: "Change",
			adGroupBidModifiersRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		parts := slices.Clone(a.ResourceNames)
		for _, item := range a.AdGroupBidModifiers {
			in, _ := item.(map[string]any)
			part := stringOf(in["resource_name"])
			if part == "" {
				part = "ad group " + stringOf(in["ad_group"])
			}
			var facts []string
			if v, ok := in["bid_modifier"]; ok {
				facts = append(facts, "bid_modifier "+fmt.Sprint(v))
			}
			for _, key := range adGroupBidModifierCriteria {
				if _, ok := in[key]; ok {
					facts = append(facts, "criterion "+key+" "+adGroupBidModifierCriterionText(in[key]))
				}
			}
			parts = append(parts, part+" ("+strings.Join(facts, ", ")+")")
		}
		summary := verb + " ad group bid modifiers of Google Ads account " + a.CustomerID + ": " +
			strings.Join(parts, "; ") + "."
		if tool == adGroupBidModifiersRemoveID {
			return summary + " The ad group is no longer bid up or down where they applied, which changes its spend."
		}
		return summary + " A bid modifier raises or lowers the bids of the ad group, so it changes how much it costs."
	}
}

// adGroupBidModifierCriterionText renders a criterion object on one line for the confirmation.
func adGroupBidModifierCriterionText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
