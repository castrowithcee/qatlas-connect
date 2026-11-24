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

const (
	keywordPlanAdGroupsCreateID = Provider + ".keyword-plan-ad-groups.create"
	keywordPlanAdGroupsUpdateID = Provider + ".keyword-plan-ad-groups.update"
	keywordPlanAdGroupsRemoveID = Provider + ".keyword-plan-ad-groups.remove"

	keywordPlanAdGroupsService = "/keywordPlanAdGroups"
)

// KeywordPlanAdGroup of v26 requires only name on create, cpc_bid_micros is optional and inherited from the plan
// campaign. MutateKeywordPlanAdGroupsRequest has no response_content_type and its result holds the resource name
// only. A plan holds at most 200 ad groups, which Google checks.

// keywordPlanAdGroupName is the resource name of a plan ad group, built from checked IDs alone.
func keywordPlanAdGroupName(customerID, id string) string {
	return customerName(customerID) + "/keywordPlanAdGroups/" + id
}

// --- arguments ---

type (
	keywordPlanAdGroupNew struct {
		KeywordPlanCampaignID string       `json:"keyword_plan_campaign_id"`
		Name                  string       `json:"name"`
		CPCBidMicros          *json.Number `json:"cpc_bid_micros"`
	}
	keywordPlanAdGroupEdit struct {
		KeywordPlanAdGroupID string       `json:"keyword_plan_ad_group_id"`
		Name                 *string      `json:"name"`
		CPCBidMicros         *json.Number `json:"cpc_bid_micros"`
	}
	keywordPlanAdGroupsCreateArguments struct {
		CustomerID          string                  `json:"customer_id"`
		KeywordPlanAdGroups []keywordPlanAdGroupNew `json:"keyword_plan_ad_groups"`
		ValidateOnly        bool                    `json:"validate_only"`
	}
	keywordPlanAdGroupsUpdateArguments struct {
		CustomerID          string                   `json:"customer_id"`
		KeywordPlanAdGroups []keywordPlanAdGroupEdit `json:"keyword_plan_ad_groups"`
		ValidateOnly        bool                     `json:"validate_only"`
	}
	keywordPlanAdGroupsRemoveArguments struct {
		CustomerID            string   `json:"customer_id"`
		KeywordPlanAdGroupIDs []string `json:"keyword_plan_ad_group_ids"`
		ValidateOnly          bool     `json:"validate_only"`
	}
)

func (a keywordPlanAdGroupsCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_ad_groups", len(a.KeywordPlanAdGroups)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanAdGroups))
	for _, g := range a.KeywordPlanAdGroups {
		if !numericIDPattern.MatchString(g.KeywordPlanCampaignID) {
			return nil, badRequest("keyword_plan_campaign_id must be the ID of an existing keyword plan campaign, a positive number")
		}
		if err := keywordPlansCheckName(g.Name); err != nil {
			return nil, err
		}
		create := map[string]any{"keywordPlanCampaign": keywordPlanCampaignName(a.CustomerID, g.KeywordPlanCampaignID),
			"name": g.Name}
		if g.CPCBidMicros != nil {
			bid, err := campaignBudgetsMicros("cpc_bid_micros", g.CPCBidMicros)
			if err != nil {
				return nil, err
			}
			create["cpcBidMicros"] = bid
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a keywordPlanAdGroupsUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_ad_groups", len(a.KeywordPlanAdGroups)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanAdGroups))
	names := make([]string, 0, len(a.KeywordPlanAdGroups))
	for _, g := range a.KeywordPlanAdGroups {
		if !numericIDPattern.MatchString(g.KeywordPlanAdGroupID) {
			return nil, nil, badRequest("keyword_plan_ad_group_id must be the ID of an existing keyword plan ad group, a positive number")
		}
		name := keywordPlanAdGroupName(a.CustomerID, g.KeywordPlanAdGroupID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plan_ad_groups names each keyword_plan_ad_group_id at most once")
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		if g.Name != nil {
			if err := keywordPlansCheckName(*g.Name); err != nil {
				return nil, nil, err
			}
			update["name"] = *g.Name
			mask = append(mask, "name")
		}
		if g.CPCBidMicros != nil {
			bid, err := campaignBudgetsMicros("cpc_bid_micros", g.CPCBidMicros)
			if err != nil {
				return nil, nil, err
			}
			update["cpcBidMicros"] = bid
			mask = append(mask, "cpc_bid_micros")
		}
		if len(mask) == 0 {
			return nil, nil, badRequest("each entry of keyword_plan_ad_groups needs at least one field to change")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a keywordPlanAdGroupsRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_ad_group_ids", len(a.KeywordPlanAdGroupIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanAdGroupIDs))
	names := make([]string, 0, len(a.KeywordPlanAdGroupIDs))
	for _, id := range a.KeywordPlanAdGroupIDs {
		if !numericIDPattern.MatchString(id) {
			return nil, nil, badRequest("keyword_plan_ad_group_ids must be IDs of existing keyword plan ad groups, positive numbers")
		}
		name := keywordPlanAdGroupName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plan_ad_group_ids names each keyword plan ad group at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	keywordPlanAdGroupsMutated struct {
		KeywordPlanAdGroupID string `json:"keyword_plan_ad_group_id"`
		ResourceName         string `json:"resource_name"`
	}
	keywordPlanAdGroupsChanged struct {
		CustomerID   string                       `json:"customer_id"`
		ValidateOnly bool                         `json:"validate_only"`
		Results      []keywordPlanAdGroupsMutated `json:"results"`
	}
)

// keywordPlanAdGroupsResults maps the results. mutateList has bound them to the account and, for an update and a
// remove, to the names asked for; a create must name distinct ad groups.
func keywordPlanAdGroupsResults(op string, results []listResult) ([]keywordPlanAdGroupsMutated, error) {
	out := []keywordPlanAdGroupsMutated{}
	for _, r := range results {
		for _, previous := range out {
			if previous.KeywordPlanAdGroupID == r.ID {
				return nil, unsureResult(op, "answered with the same keyword plan ad group twice")
			}
		}
		out = append(out, keywordPlanAdGroupsMutated{KeywordPlanAdGroupID: r.ID, ResourceName: r.ResourceName})
	}
	return out, nil
}

// --- descriptors ---

var (
	keywordPlanAdGroupIDSchema = schema{"type": "string", "pattern": numericIDPattern.String()}

	keywordPlanAdGroupsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"keyword_plan_ad_group_id": stringSchema, "resource_name": stringSchema},
			"keyword_plan_ad_group_id", "resource_name"))},
		"customer_id", "validate_only", "results")
)

const keywordPlanAdGroupsSensitivity = "ad groups of the keyword plans of a Google Ads account: their names and " +
	"bids, which the confirmation names; no data of other people"

const keywordPlanAdGroupsNote = " A keyword plan ad group is an ad group of a saved Keyword Planner plan campaign, " +
	"and a plan holds at most 200; this tool sets its name and default max CPC bid, adds no keyword, runs no " +
	"forecast, and does not read plan ad groups (read them with reports.search on the keyword_plan_ad_group " +
	"resource). Google may block the keyword planning services at the Explorer access level, so the Cloud " +
	"project of the credential then needs Basic access. The request goes to " +
	"KeywordPlanAdGroupService.MutateKeywordPlanAdGroups with up to 50 operations, atomic and without " +
	"partial_failure: Google applies all of them or none."

var keywordPlanAdGroupsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: keyword_plan_ad_group_id and " +
		"resource_name (customers/ID/keywordPlanAdGroups/AD_GROUP_ID, as Google answered, always of the account " +
		"of the call). Google answers the resource name only, so there is no response_content_type. Empty after " +
		"validate_only"},
}

const keywordPlanAdGroupsFieldDoc = "cpc_bid_micros (whole micros of the account currency, the default max CPC " +
	"bid of the ad group; an ad group without one inherits the bid of its plan campaign)"

var keywordPlanAdGroupProperties = schema{"name": keywordPlanNameSchema, "cpc_bid_micros": campaignBudgetMicrosSchema}

func keywordPlanAdGroupItem(extra schema, required ...string) schema {
	properties := schema{}
	for k, v := range keywordPlanAdGroupProperties {
		properties[k] = v
	}
	for k, v := range extra {
		properties[k] = v
	}
	return object(properties, required...)
}

var keywordPlanAdGroupsCreate = capability.Descriptor{
	ID: keywordPlanAdGroupsCreateID, Version: 1, Title: "Create keyword plan ad groups in a Google Ads account",
	Description: "Create ad groups in the campaigns of keyword plans of a Google Ads account: the " +
		"keyword_plan_campaign_id of the plan campaign (its resource name is built from customer_id and " +
		"keyword_plan_campaign_id) and a name, and optionally a default max CPC bid." + keywordPlanAdGroupsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-ad-groups", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, keywordPlanAdGroupsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_ad_groups": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": keywordPlanAdGroupItem(schema{"keyword_plan_campaign_id": keywordPlanCampaignIDSchema},
				"keyword_plan_campaign_id", "name")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_ad_groups")),
	OutputSchema: encoded(keywordPlanAdGroupsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_ad_groups", Required: true, Description: "1 to 50 ad groups, each " +
			"for a keyword_plan_campaign_id (the id of an existing plan campaign as reports.search returns it), " +
			"with name (required, 1 to 255 characters, no control characters) and optionally " +
			keywordPlanAdGroupsFieldDoc},
		validateOnlyArgument),
	Fields: keywordPlanAdGroupsFields,
	Examples: []capability.Example{{Description: "Create an ad group in a keyword plan campaign with its own bid",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_ad_groups":[{"keyword_plan_campaign_id":"445566",` +
			`"name":"Running shoes","cpc_bid_micros":800000}]}`)}},
}

var keywordPlanAdGroupsUpdate = capability.Descriptor{
	ID: keywordPlanAdGroupsUpdateID, Version: 1, Title: "Change keyword plan ad groups of a Google Ads account",
	Description: "Change existing keyword plan ad groups of a Google Ads account (an update with a field mask " +
		"that names exactly the fields given). Only the keyword_plan_ad_group_id selects an ad group: its " +
		"resource name is built from customer_id and keyword_plan_ad_group_id. The plan campaign of an ad " +
		"group is not changed." + keywordPlanAdGroupsNote + " Offered by a connection with the update effect or " +
		"whose tools list names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-ad-groups", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, keywordPlanAdGroupsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_ad_groups": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": keywordPlanAdGroupItem(schema{"keyword_plan_ad_group_id": keywordPlanAdGroupIDSchema},
				"keyword_plan_ad_group_id")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_ad_groups")),
	OutputSchema: encoded(keywordPlanAdGroupsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_ad_groups", Required: true, Description: "1 to 50 entries, each " +
			"keyword_plan_ad_group_id once (the id of the ad group as reports.search returns it), with at least " +
			"one of name (1 to 255 characters) and " + keywordPlanAdGroupsFieldDoc},
		validateOnlyArgument),
	Fields: keywordPlanAdGroupsFields,
	Examples: []capability.Example{{Description: "Rename a keyword plan ad group",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_ad_groups":[{"keyword_plan_ad_group_id":"778899","name":"Trail shoes"}]}`)}},
}

var keywordPlanAdGroupsRemove = capability.Descriptor{
	ID: keywordPlanAdGroupsRemoveID, Version: 1, Title: "Remove keyword plan ad groups from a Google Ads account",
	Description: "Remove existing keyword plan ad groups of a Google Ads account (a remove of " +
		"customers/ID/keywordPlanAdGroups/AD_GROUP_ID, built from customer_id and keyword_plan_ad_group_id). A " +
		"removed ad group is gone from the account; no tool of this provider brings it back." +
		keywordPlanAdGroupsNote + " Offered by a connection with the delete effect or whose tools list names " +
		"it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-ad-groups", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, keywordPlanAdGroupsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_ad_group_ids": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"uniqueItems": true, "items": keywordPlanAdGroupIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_ad_group_ids")),
	OutputSchema: encoded(keywordPlanAdGroupsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_ad_group_ids", Required: true, Description: "1 to 50 IDs of " +
			"existing keyword plan ad groups, each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: keywordPlanAdGroupsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a keyword plan ad group without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_ad_group_ids":["778899"],"validate_only":true}`)}},
}

// --- handlers ---

func handleKeywordPlanAdGroupsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create keyword plan ad groups"
	var args keywordPlanAdGroupsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanAdGroupsSend(ctx, resolved, secrets, red, op, keywordPlanAdGroupsCreateID, args.CustomerID,
		ops, nil, args.ValidateOnly)
}

func handleKeywordPlanAdGroupsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update keyword plan ad groups"
	var args keywordPlanAdGroupsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanAdGroupsSend(ctx, resolved, secrets, red, op, keywordPlanAdGroupsUpdateID, args.CustomerID,
		ops, names, args.ValidateOnly)
}

func handleKeywordPlanAdGroupsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove keyword plan ad groups"
	var args keywordPlanAdGroupsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanAdGroupsSend(ctx, resolved, secrets, red, op, keywordPlanAdGroupsRemoveID, args.CustomerID,
		ops, names, args.ValidateOnly)
}

func keywordPlanAdGroupsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, op, tool, customerID string, ops []any, want []string, validateOnly bool) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: customerID,
		service: keywordPlanAdGroupsService, collection: "keywordPlanAdGroups", operations: ops, names: want,
		validateOnly: validateOnly})
	if err != nil {
		return nil, err
	}
	out, err := keywordPlanAdGroupsResults(op, results)
	if err != nil {
		return nil, err
	}
	return keywordPlanAdGroupsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: out}, nil
}

// --- confirmation ---

func keywordPlanAdGroupsBidText(bid *json.Number) string {
	if bid == nil {
		return ""
	}
	return " bid " + boundedText(string(*bid), maxCodeText) + " micros"
}

func keywordPlanAdGroupsCreateSummary(raw json.RawMessage) string {
	var a keywordPlanAdGroupsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, g := range a.KeywordPlanAdGroups[:min(len(a.KeywordPlanAdGroups), 10)] {
		parts = append(parts, keywordPlansQuote(g.Name)+" in keyword plan campaign "+
			boundedText(g.KeywordPlanCampaignID, maxCodeText)+keywordPlanAdGroupsBidText(g.CPCBidMicros))
	}
	return keywordPlansLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.KeywordPlanAdGroups)) +
		" keyword plan ad group(s) in Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		keywordPlansMore(len(a.KeywordPlanAdGroups), len(parts)) + "."
}

func keywordPlanAdGroupsUpdateSummary(raw json.RawMessage) string {
	var a keywordPlanAdGroupsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, g := range a.KeywordPlanAdGroups[:min(len(a.KeywordPlanAdGroups), 10)] {
		part := "keyword plan ad group " + boundedText(g.KeywordPlanAdGroupID, maxCodeText)
		if g.Name != nil {
			part += " name " + keywordPlansQuote(*g.Name)
		}
		parts = append(parts, part+keywordPlanAdGroupsBidText(g.CPCBidMicros))
	}
	return keywordPlansLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.KeywordPlanAdGroups)) +
		" keyword plan ad group(s) of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		keywordPlansMore(len(a.KeywordPlanAdGroups), len(parts)) + "."
}

func keywordPlanAdGroupsRemoveSummary(raw json.RawMessage) string {
	var a keywordPlanAdGroupsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.KeywordPlanAdGroupIDs[:min(len(a.KeywordPlanAdGroupIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return keywordPlansLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.KeywordPlanAdGroupIDs)) +
		" keyword plan ad group(s) from Google Ads account " + a.CustomerID + ": " + strings.Join(shown, ", ") +
		keywordPlansMore(len(a.KeywordPlanAdGroupIDs), len(shown)) + ". No tool brings them back."
}
