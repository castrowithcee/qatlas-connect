package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// A conversion value rule set (ConversionValueRuleSetService.MutateConversionValueRuleSets) attaches conversion
// value rules to the account or to one campaign. Attached rules change the conversion value that Smart Bidding
// optimizes for, so all three tools are offered by the tools list only.

const (
	conversionValueRuleSetsCreateID = Provider + ".conversion-value-rule-sets.create"
	conversionValueRuleSetsUpdateID = Provider + ".conversion-value-rule-sets.update"
	conversionValueRuleSetsRemoveID = Provider + ".conversion-value-rule-sets.remove"

	valueRuleSetService    = "/conversionValueRuleSets"
	valueRuleSetCollection = "conversionValueRuleSets"
)

var (
	valueRuleSetAttachments = []string{"CUSTOMER", "CAMPAIGN"}
	valueRuleSetDimensions  = []string{"AUDIENCE", "DEVICE", "GEO_LOCATION", "ITINERARY", "NO_CONDITION"}
)

type (
	// valueRuleSetFields are the settings of a set. A nil or empty member was not given.
	valueRuleSetFields struct {
		AttachmentType string   `json:"attachment_type,omitempty"`
		Campaign       string   `json:"campaign,omitempty"`
		Categories     []string `json:"conversion_action_categories,omitempty"`
		Rules          []string `json:"conversion_value_rules,omitempty"`
		Dimensions     []string `json:"dimensions,omitempty"`
	}
	valueRuleSetEntry struct {
		valueRuleSetFields
		ID string `json:"conversion_value_rule_set_id,omitempty"`
	}
	valueRuleSetArguments struct {
		CustomerID          string              `json:"customer_id"`
		Sets                []valueRuleSetEntry `json:"conversion_value_rule_sets"`
		IDs                 []string            `json:"conversion_value_rule_set_ids"`
		ValidateOnly        bool                `json:"validate_only"`
		ResponseContentType string              `json:"response_content_type"`
	}
)

func valueRuleSetName(customerID, id string) string {
	return customerName(customerID) + "/" + valueRuleSetCollection + "/" + id
}

// check refuses what no request may start with. Which lists a set must carry and whether the rules fit the
// dimensions is left to Google.
func (f valueRuleSetFields) check(customerID string, create bool) error {
	owned := func(collection string) func(string) bool {
		return func(v string) bool { return ownedName(v, customerID, collection) }
	}
	if create {
		if !slices.Contains(valueRuleSetAttachments, f.AttachmentType) {
			return badRequest("attachment_type must be CUSTOMER or CAMPAIGN")
		}
		if (f.AttachmentType == "CAMPAIGN") != (f.Campaign != "") {
			return badRequest("campaign is needed with attachment_type CAMPAIGN and refused with CUSTOMER")
		}
		if err := checkRefs("conversion_action_categories", f.Categories, inValues(conversionCategories),
			"conversion action category"); err != nil {
			return err
		}
	}
	if f.Campaign != "" && !owned("campaigns")(f.Campaign) {
		return badRequest("campaign must be a campaign of customer_id, customers/ID/campaigns/N")
	}
	if err := checkRefs("conversion_value_rules", f.Rules, owned(valueRuleCollection),
		"conversion value rule of customer_id, customers/ID/conversionValueRules/N"); err != nil {
		return err
	}
	return checkRefs("dimensions", f.Dimensions, inValues(valueRuleSetDimensions), "dimension of "+
		strings.Join(valueRuleSetDimensions, ", "))
}

func (f valueRuleSetFields) wire() map[string]any {
	raw, _ := json.Marshal(f)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	return camelKeys(generic).(map[string]any)
}

// mask lists the members an update sets. A list given replaces the whole list.
func (f valueRuleSetFields) mask() []string {
	var mask []string
	if f.Campaign != "" {
		mask = append(mask, "campaign")
	}
	if len(f.Rules) > 0 {
		mask = append(mask, "conversionValueRules")
	}
	if len(f.Dimensions) > 0 {
		mask = append(mask, "dimensions")
	}
	return mask
}

// operations checks the arguments and builds the operations and the resource names each result must carry.
func (a valueRuleSetArguments) operations(tool string) ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	var ops []any
	var names []string
	if tool == conversionValueRuleSetsRemoveID {
		if n := len(a.IDs); n < 1 || n > maxListOperations {
			return nil, nil, badRequest("conversion_value_rule_set_ids must hold one to 10 IDs")
		}
		for i, id := range a.IDs {
			if !numericIDPattern.MatchString(id) || slices.Contains(a.IDs[:i], id) {
				return nil, nil, badRequest("conversion_value_rule_set_ids must hold each set once as a positive number")
			}
			names = append(names, valueRuleSetName(a.CustomerID, id))
			ops = append(ops, map[string]any{"remove": names[i]})
		}
		return ops, names, nil
	}
	if n := len(a.Sets); n < 1 || n > maxListOperations {
		return nil, nil, badRequest("conversion_value_rule_sets must hold one to 10 entries")
	}
	create := tool == conversionValueRuleSetsCreateID
	var seen []string
	for _, s := range a.Sets {
		if err := s.check(a.CustomerID, create); err != nil {
			return nil, nil, err
		}
		if create {
			if s.ID != "" {
				return nil, nil, badRequest("a new set takes no conversion_value_rule_set_id")
			}
			ops = append(ops, map[string]any{"create": s.wire()})
			continue
		}
		mask := s.mask()
		if !numericIDPattern.MatchString(s.ID) || len(mask) == 0 || slices.Contains(seen, s.ID) {
			return nil, nil, badRequest("every entry needs the conversion_value_rule_set_id of an existing set, once, " +
				"and at least one setting to change")
		}
		seen = append(seen, s.ID)
		name := valueRuleSetName(a.CustomerID, s.ID)
		update := s.wire()
		update["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

// The set as the tools answer it, read from the answer of Google after its member names are turned into snake_case.
type (
	valueRuleSetResource struct {
		ResourceName  string   `json:"resource_name"`
		ID            *wireInt `json:"id,omitempty"`
		OwnerCustomer string   `json:"owner_customer,omitempty"`
		Status        string   `json:"status,omitempty"`
		valueRuleSetFields
	}
	valueRuleSetResult struct {
		ResourceName             string                `json:"resource_name"`
		ConversionValueRuleSet   *valueRuleSetResource `json:"conversion_value_rule_set,omitempty"`
		ConversionValueRuleSetID string                `json:"conversion_value_rule_set_id"`
	}
	valueRuleSetsChanged struct {
		CustomerID   string               `json:"customer_id"`
		ValidateOnly bool                 `json:"validate_only"`
		Results      []valueRuleSetResult `json:"results"`
	}
)

// valueRuleSetOf maps the set of a result. It must be the one of the result, and every resource name in it must
// belong to the account of the call.
func valueRuleSetOf(raw json.RawMessage, name, id, customerID string) (*valueRuleSetResource, bool) {
	var generic map[string]any
	if json.Unmarshal(raw, &generic) != nil {
		return nil, false
	}
	var w valueRuleSetResource
	re, err := json.Marshal(snakeKeys(generic))
	if err != nil || json.Unmarshal(re, &w) != nil || (w.ResourceName != "" && w.ResourceName != name) ||
		(w.ID != nil && string(*w.ID) != id) ||
		(w.OwnerCustomer != "" && w.OwnerCustomer != customerName(customerID)) ||
		(w.Campaign != "" && !ownedName(w.Campaign, customerID, "campaigns")) ||
		!allMatch(w.Rules, func(v string) bool { return ownedName(v, customerID, valueRuleCollection) }) ||
		len(w.Categories) > 10*maxRuleReferences || len(w.Dimensions) > 10*maxRuleReferences {
		return nil, false
	}
	w.ResourceName = name
	w.Status, w.AttachmentType = code(w.Status), code(w.AttachmentType)
	for i := range w.Categories {
		w.Categories[i] = code(w.Categories[i])
	}
	for i := range w.Dimensions {
		w.Dimensions[i] = code(w.Dimensions[i])
	}
	return &w, true
}

// --- schemas ---

var (
	ruleSetFieldSchemas = schema{
		"campaign": schema{"type": "string", "maxLength": 100, "pattern": `^customers/[0-9]{1,10}/campaigns/[0-9]{1,20}$`,
			"description": "Campaign of this account, customers/ID/campaigns/N"},
		"conversion_value_rules": refList(`^customers/[0-9]{1,10}/conversionValueRules/[0-9]{1,20}$`,
			"Conversion value rules of this account, customers/ID/conversionValueRules/N"),
		"dimensions": schema{"type": "array", "maxItems": len(valueRuleSetDimensions), "uniqueItems": true,
			"description": "Dimensions of the conditions of the rules; the first is the primary one",
			"items":       schema{"type": "string", "enum": valueRuleSetDimensions}},
	}

	ruleSetItemSchema = func(update bool) schema {
		props := schema{}
		for k, v := range ruleSetFieldSchemas {
			props[k] = v
		}
		if update {
			props["conversion_value_rule_set_id"] = schema{"type": "string", "pattern": numericIDPattern.String()}
			return object(props, "conversion_value_rule_set_id")
		}
		props["attachment_type"] = schema{"type": "string", "enum": valueRuleSetAttachments}
		props["conversion_action_categories"] = schema{"type": "array", "maxItems": len(conversionCategories),
			"uniqueItems": true, "items": schema{"type": "string", "enum": conversionCategories}}
		return object(props, "attachment_type")
	}

	ruleSetObjectSchema = object(schema{"resource_name": stringSchema, "id": stringSchema,
		"owner_customer": valueRuleOwnerSchema, "status": stringSchema, "attachment_type": stringSchema,
		"campaign": stringSchema, "conversion_action_categories": arrayOf(stringSchema),
		"conversion_value_rules": arrayOf(stringSchema), "dimensions": arrayOf(stringSchema)}, "resource_name")
	ruleSetOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema, "conversion_value_rule_set_id": stringSchema,
			"conversion_value_rule_set": ruleSetObjectSchema}, "resource_name", "conversion_value_rule_set_id"))},
		"customer_id", "validate_only", "results")

	ruleSetFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One result per entry, in the order of the entries, empty after validate_only: " +
			"resource_name (customers/ID/conversionValueRuleSets/N), conversion_value_rule_set_id (its number), and " +
			"with MUTABLE_RESOURCE the set after the change (resource_name, id, owner_customer, status, " +
			"attachment_type, campaign, conversion_action_categories, conversion_value_rules, dimensions, as far " +
			"as Google returns them)"}}
)

const ruleSetSensitivity = "conversion value rule sets of a Google Ads account: which conversion value rules apply to " +
	"the account or a campaign, which Smart Bidding optimizes for"

const ruleSetEffectNote = " A set attaches conversion value rules to the account (attachment_type CUSTOMER) or to one " +
	"campaign (CAMPAIGN with campaign); Smart Bidding optimizes for the adjusted value, so a set moves spend. " +
	"dimensions name the condition types of the rules, the first is the primary one. Rules and campaign must " +
	"belong to customer_id. The status of a set is set by Google and cannot be given. The sets are read with " +
	"reports.search on conversion_value_rule_set."

var conversionValueRuleSetsCreate = capability.Descriptor{
	ID: conversionValueRuleSetsCreateID, Version: 1, Title: "Create conversion value rule sets of a Google Ads account",
	Description: "Create one to 10 conversion value rule sets in a Google Ads account (ConversionValueRuleSetService." +
		"MutateConversionValueRuleSets with create operations). Each needs attachment_type; optional: campaign " +
		"(needed with CAMPAIGN), conversion_action_categories, conversion_value_rules, dimensions. The attachment " +
		"type and the categories cannot be changed afterwards." + ruleSetEffectNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "value-rules", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, ruleSetSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"conversion_value_rule_sets": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "items": ruleSetItemSchema(false)},
		"validate_only":              validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "conversion_value_rule_sets")),
	OutputSchema: encoded(ruleSetOutputSchema),
	Arguments: []capability.Argument{ruleCustomerArgument,
		{Name: "conversion_value_rule_sets", Required: true, Description: "One to 10 sets, each with attachment_type " +
			"and optionally campaign, conversion_action_categories, conversion_value_rules, dimensions"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: ruleSetFields,
	Examples: []capability.Example{{Description: "Validate a set that attaches a rule to one campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","conversion_value_rule_sets":[{"attachment_type":"CAMPAIGN",` +
			`"campaign":"customers/1234567890/campaigns/55","dimensions":["DEVICE"],"conversion_value_rules":` +
			`["customers/1234567890/conversionValueRules/11"]}],"validate_only":true}`)}},
}

var conversionValueRuleSetsUpdate = capability.Descriptor{
	ID: conversionValueRuleSetsUpdateID, Version: 1, Title: "Change conversion value rule sets of a Google Ads account",
	Description: "Change one to 10 existing conversion value rule sets of a Google Ads account (ConversionValueRuleSetService." +
		"MutateConversionValueRuleSets with update operations and a field mask of exactly the given settings). Each " +
		"entry names the conversion_value_rule_set_id and at least one of: campaign, conversion_value_rules, " +
		"dimensions. A list that is given replaces the whole list; settings are set, not cleared. attachment_type " +
		"and conversion_action_categories are immutable and not offered." + ruleSetEffectNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "value-rules", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, ruleSetSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"conversion_value_rule_sets": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "items": ruleSetItemSchema(true)},
		"validate_only":              validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "conversion_value_rule_sets")),
	OutputSchema: encoded(ruleSetOutputSchema),
	Arguments: []capability.Argument{ruleCustomerArgument,
		{Name: "conversion_value_rule_sets", Required: true, Description: "One to 10 entries, each with " +
			"conversion_value_rule_set_id (the number of the set, each once) and at least one of: campaign, " +
			"conversion_value_rules, dimensions"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: ruleSetFields,
	Examples: []capability.Example{{Description: "Replace the rules of a set", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","conversion_value_rule_sets":[{"conversion_value_rule_set_id":"7",` +
			`"conversion_value_rules":["customers/1234567890/conversionValueRules/12"]}]}`)}},
}

var conversionValueRuleSetsRemove = capability.Descriptor{
	ID: conversionValueRuleSetsRemoveID, Version: 1, Title: "Remove conversion value rule sets of a Google Ads account",
	Description: "Remove one to 10 conversion value rule sets of a Google Ads account (ConversionValueRuleSetService." +
		"MutateConversionValueRuleSets with remove operations of customers/ID/conversionValueRuleSets/ID). A removed " +
		"set is permanently deleted and its rules no longer apply through it; conversions are valued without it from " +
		"then on. A set is removed only this way. The tool builds the names from customer_id and the numeric IDs. " +
		"The service answers the resource names only, so there is no response_content_type." + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "value-rules", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, ruleSetSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"conversion_value_rule_set_ids": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "uniqueItems": true,
			"items": schema{"type": "string", "pattern": numericIDPattern.String()}},
		"validate_only": validateOnlySchema}, "customer_id", "conversion_value_rule_set_ids")),
	OutputSchema: encoded(ruleSetOutputSchema),
	Arguments: []capability.Argument{ruleCustomerArgument,
		{Name: "conversion_value_rule_set_ids", Required: true, Description: "One to 10 distinct numeric IDs of sets of " +
			"the account, as reports.search returns them"}, validateOnlyArgument},
	Fields: ruleSetFields,
	Examples: []capability.Example{{Description: "Validate the removal of a set", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","conversion_value_rule_set_ids":["7"],"validate_only":true}`)}},
}

// --- handlers ---

func handleConversionValueRuleSetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionValueRuleSetsChange(ctx, resolved, secrets, red, raw, conversionValueRuleSetsCreateID)
}

func handleConversionValueRuleSetsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionValueRuleSetsChange(ctx, resolved, secrets, red, raw, conversionValueRuleSetsUpdateID)
}

func handleConversionValueRuleSetsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return conversionValueRuleSetsChange(ctx, resolved, secrets, red, raw, conversionValueRuleSetsRemoveID)
}

func conversionValueRuleSetsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, tool string) (any, error) {
	op := map[string]string{conversionValueRuleSetsCreateID: "create conversion value rule sets",
		conversionValueRuleSetsUpdateID: "update conversion value rule sets",
		conversionValueRuleSetsRemoveID: "remove conversion value rule sets"}[tool]
	var args valueRuleSetArguments
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
		service: valueRuleSetService, collection: valueRuleSetCollection, resourceKey: "conversionValueRuleSet",
		operations: operations, names: names, validateOnly: args.ValidateOnly, content: args.ResponseContentType})
	if err != nil {
		return nil, err
	}
	out := valueRuleSetsChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Results: []valueRuleSetResult{}}
	for _, r := range results {
		item := valueRuleSetResult{ResourceName: r.ResourceName, ConversionValueRuleSetID: r.ID}
		if len(r.Resource) > 0 {
			set, ok := valueRuleSetOf(r.Resource, r.ResourceName, r.ID, args.CustomerID)
			if !ok {
				return nil, unsureResult(op, "answered with a set that is not the one of its result")
			}
			item.ConversionValueRuleSet = set
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// conversionValueRuleSetsSummary phrases the change for the confirmation, from the arguments alone.
func conversionValueRuleSetsSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a valueRuleSetArguments
		_ = json.Unmarshal(raw, &a)
		lead := ""
		if a.ValidateOnly {
			lead = "Only validate: "
		}
		where := " of Google Ads account " + a.CustomerID
		if tool == conversionValueRuleSetsRemoveID {
			return lead + "Remove " + strconv.Itoa(len(a.IDs)) + " conversion value rule set(s) (" + strings.Join(a.IDs, ", ") +
				")" + where + ": they are deleted for good and conversions are valued without them."
		}
		verb := "Change"
		if tool == conversionValueRuleSetsCreateID {
			verb = "Create"
		}
		var parts []string
		for _, s := range a.Sets {
			part := s.ID + " " + s.AttachmentType + " " + s.Campaign
			if n := len(s.Rules); n > 0 {
				part += " " + strconv.Itoa(n) + " rule(s)"
			}
			parts = append(parts, strings.Join(strings.Fields(part), " "))
		}
		return lead + verb + " " + strconv.Itoa(len(a.Sets)) + " conversion value rule set(s)" + where + " (" +
			strings.Join(parts, "; ") + "). This changes the conversion value that Smart Bidding optimizes for."
	}
}
