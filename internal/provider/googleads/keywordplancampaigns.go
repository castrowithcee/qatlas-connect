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
	keywordPlanCampaignsCreateID = Provider + ".keyword-plan-campaigns.create"
	keywordPlanCampaignsUpdateID = Provider + ".keyword-plan-campaigns.update"
	keywordPlanCampaignsRemoveID = Provider + ".keyword-plan-campaigns.remove"

	keywordPlanCampaignsService = "/keywordPlanCampaigns"
)

// Bounds of KeywordPlanCampaign as the v26 reference documents them: name, keyword_plan_network and
// cpc_bid_micros are required on create, at most one language constant and 20 geo targets, one campaign per plan.
// MutateKeywordPlanCampaignsRequest has no response_content_type and its result holds the resource name only.
const (
	keywordPlanCampaignLanguageLimit = 1
	keywordPlanCampaignGeoLimit      = 20
)

// keywordPlanCampaignName is the resource name of a plan campaign, built from checked IDs alone.
func keywordPlanCampaignName(customerID, id string) string {
	return customerName(customerID) + "/keywordPlanCampaigns/" + id
}

// --- arguments ---

type (
	keywordPlanCampaignNew struct {
		KeywordPlanID      string       `json:"keyword_plan_id"`
		Name               string       `json:"name"`
		KeywordPlanNetwork string       `json:"keyword_plan_network"`
		CPCBidMicros       *json.Number `json:"cpc_bid_micros"`
		LanguageConstants  []string     `json:"language_constants"`
		GeoTargetConstants []string     `json:"geo_target_constants"`
	}
	keywordPlanCampaignEdit struct {
		KeywordPlanCampaignID string       `json:"keyword_plan_campaign_id"`
		Name                  *string      `json:"name"`
		KeywordPlanNetwork    *string      `json:"keyword_plan_network"`
		CPCBidMicros          *json.Number `json:"cpc_bid_micros"`
		LanguageConstants     []string     `json:"language_constants"`
		GeoTargetConstants    []string     `json:"geo_target_constants"`
	}
	keywordPlanCampaignsCreateArguments struct {
		CustomerID           string                   `json:"customer_id"`
		KeywordPlanCampaigns []keywordPlanCampaignNew `json:"keyword_plan_campaigns"`
		ValidateOnly         bool                     `json:"validate_only"`
	}
	keywordPlanCampaignsUpdateArguments struct {
		CustomerID           string                    `json:"customer_id"`
		KeywordPlanCampaigns []keywordPlanCampaignEdit `json:"keyword_plan_campaigns"`
		ValidateOnly         bool                      `json:"validate_only"`
	}
	keywordPlanCampaignsRemoveArguments struct {
		CustomerID             string   `json:"customer_id"`
		KeywordPlanCampaignIDs []string `json:"keyword_plan_campaign_ids"`
		ValidateOnly           bool     `json:"validate_only"`
	}
)

func keywordPlanCampaignsCheckNetwork(value string) error {
	if !slices.Contains(keywordNetworks, value) {
		return badRequest("keyword_plan_network must be GOOGLE_SEARCH or GOOGLE_SEARCH_AND_PARTNERS")
	}
	return nil
}

// keywordPlanCampaignsTargets checks the language and location constants and sets them in the resource. The
// returned mask names the fields set. A list given is never empty: an empty list would clear the field, which
// the reference does not document.
func keywordPlanCampaignsTargets(languages, geos []string, resource map[string]any) ([]string, error) {
	var mask []string
	if languages != nil {
		if len(languages) < 1 || len(languages) > keywordPlanCampaignLanguageLimit ||
			!keywordLanguageRE.MatchString(languages[0]) {
			return nil, badRequest("language_constants must hold one language constant, languageConstants/ID")
		}
		resource["languageConstants"] = languages
		mask = append(mask, "language_constants")
	}
	if geos != nil {
		if len(geos) < 1 || len(geos) > keywordPlanCampaignGeoLimit {
			return nil, badRequest("geo_target_constants must hold 1 to " + strconv.Itoa(keywordPlanCampaignGeoLimit) +
				" location constants")
		}
		targets := make([]any, 0, len(geos))
		for i, g := range geos {
			if !keywordGeoRE.MatchString(g) || slices.Contains(geos[:i], g) {
				return nil, badRequest("geo_target_constants must be location constants, geoTargetConstants/ID, each once")
			}
			targets = append(targets, map[string]any{"geoTargetConstant": g})
		}
		resource["geoTargets"] = targets
		mask = append(mask, "geo_targets")
	}
	return mask, nil
}

func (a keywordPlanCampaignsCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_campaigns", len(a.KeywordPlanCampaigns)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanCampaigns))
	plans := make([]string, 0, len(a.KeywordPlanCampaigns))
	for _, c := range a.KeywordPlanCampaigns {
		if !numericIDPattern.MatchString(c.KeywordPlanID) {
			return nil, badRequest("keyword_plan_id must be the ID of an existing keyword plan, a positive number")
		}
		// Google allows one campaign per plan.
		if slices.Contains(plans, c.KeywordPlanID) {
			return nil, badRequest("keyword_plan_campaigns names each keyword_plan_id at most once: a plan holds one campaign")
		}
		plans = append(plans, c.KeywordPlanID)
		if err := keywordPlansCheckName(c.Name); err != nil {
			return nil, err
		}
		if err := keywordPlanCampaignsCheckNetwork(c.KeywordPlanNetwork); err != nil {
			return nil, err
		}
		if c.CPCBidMicros == nil {
			return nil, badRequest("cpc_bid_micros is required")
		}
		bid, err := campaignBudgetsMicros("cpc_bid_micros", c.CPCBidMicros)
		if err != nil {
			return nil, err
		}
		create := map[string]any{"keywordPlan": keywordPlanName(a.CustomerID, c.KeywordPlanID), "name": c.Name,
			"keywordPlanNetwork": c.KeywordPlanNetwork, "cpcBidMicros": bid}
		if _, err := keywordPlanCampaignsTargets(c.LanguageConstants, c.GeoTargetConstants, create); err != nil {
			return nil, err
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a keywordPlanCampaignsUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_campaigns", len(a.KeywordPlanCampaigns)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanCampaigns))
	names := make([]string, 0, len(a.KeywordPlanCampaigns))
	for _, c := range a.KeywordPlanCampaigns {
		if !numericIDPattern.MatchString(c.KeywordPlanCampaignID) {
			return nil, nil, badRequest("keyword_plan_campaign_id must be the ID of an existing keyword plan campaign, a positive number")
		}
		name := keywordPlanCampaignName(a.CustomerID, c.KeywordPlanCampaignID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plan_campaigns names each keyword_plan_campaign_id at most once")
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		if c.Name != nil {
			if err := keywordPlansCheckName(*c.Name); err != nil {
				return nil, nil, err
			}
			update["name"] = *c.Name
			mask = append(mask, "name")
		}
		if c.KeywordPlanNetwork != nil {
			if err := keywordPlanCampaignsCheckNetwork(*c.KeywordPlanNetwork); err != nil {
				return nil, nil, err
			}
			update["keywordPlanNetwork"] = *c.KeywordPlanNetwork
			mask = append(mask, "keyword_plan_network")
		}
		if c.CPCBidMicros != nil {
			bid, err := campaignBudgetsMicros("cpc_bid_micros", c.CPCBidMicros)
			if err != nil {
				return nil, nil, err
			}
			update["cpcBidMicros"] = bid
			mask = append(mask, "cpc_bid_micros")
		}
		targets, err := keywordPlanCampaignsTargets(c.LanguageConstants, c.GeoTargetConstants, update)
		if err != nil {
			return nil, nil, err
		}
		mask = append(mask, targets...)
		if len(mask) == 0 {
			return nil, nil, badRequest("each entry of keyword_plan_campaigns needs at least one field to change")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a keywordPlanCampaignsRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_campaign_ids", len(a.KeywordPlanCampaignIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanCampaignIDs))
	names := make([]string, 0, len(a.KeywordPlanCampaignIDs))
	for _, id := range a.KeywordPlanCampaignIDs {
		if !numericIDPattern.MatchString(id) {
			return nil, nil, badRequest("keyword_plan_campaign_ids must be IDs of existing keyword plan campaigns, positive numbers")
		}
		name := keywordPlanCampaignName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plan_campaign_ids names each keyword plan campaign at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	keywordPlanCampaignsMutated struct {
		KeywordPlanCampaignID string `json:"keyword_plan_campaign_id"`
		ResourceName          string `json:"resource_name"`
	}
	keywordPlanCampaignsChanged struct {
		CustomerID   string                        `json:"customer_id"`
		ValidateOnly bool                          `json:"validate_only"`
		Results      []keywordPlanCampaignsMutated `json:"results"`
	}
)

// keywordPlanCampaignsResults maps the results. mutateList has bound them to the account and, for an update and
// a remove, to the names asked for; a create must name distinct campaigns.
func keywordPlanCampaignsResults(op string, results []listResult) ([]keywordPlanCampaignsMutated, error) {
	out := []keywordPlanCampaignsMutated{}
	for _, r := range results {
		for _, previous := range out {
			if previous.KeywordPlanCampaignID == r.ID {
				return nil, unsureResult(op, "answered with the same keyword plan campaign twice")
			}
		}
		out = append(out, keywordPlanCampaignsMutated{KeywordPlanCampaignID: r.ID, ResourceName: r.ResourceName})
	}
	return out, nil
}

// --- descriptors ---

var (
	keywordPlanCampaignIDSchema = schema{"type": "string", "pattern": numericIDPattern.String()}
	keywordPlanLanguageSchema   = schema{"type": "array", "minItems": 1, "maxItems": keywordPlanCampaignLanguageLimit,
		"items": schema{"type": "string", "pattern": languageConstantPat}}
	keywordPlanGeoSchema = schema{"type": "array", "minItems": 1, "maxItems": keywordPlanCampaignGeoLimit,
		"uniqueItems": true, "items": schema{"type": "string", "pattern": geoConstantPattern}}

	keywordPlanCampaignsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"keyword_plan_campaign_id": stringSchema, "resource_name": stringSchema},
			"keyword_plan_campaign_id", "resource_name"))},
		"customer_id", "validate_only", "results")
)

const keywordPlanCampaignsSensitivity = "campaigns of the keyword plans of a Google Ads account: their names, " +
	"networks, bids, languages, and locations, which the confirmation names; no data of other people"

const keywordPlanCampaignsNote = " A keyword plan campaign is the campaign of a saved Keyword Planner plan, and " +
	"a plan holds at most one; this tool sets its name, network, default max CPC bid, language, and locations, " +
	"adds no ad group or keyword, runs no forecast, and does not read plan campaigns (read them with " +
	"reports.search on the keyword_plan_campaign resource). Google may block the keyword planning services at the " +
	"Explorer access level, so the Cloud project of the credential then needs Basic access. The request goes to " +
	"KeywordPlanCampaignService.MutateKeywordPlanCampaigns with up to 50 operations, atomic and without " +
	"partial_failure: Google applies all of them or none."

var keywordPlanCampaignsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: keyword_plan_campaign_id and " +
		"resource_name (customers/ID/keywordPlanCampaigns/CAMPAIGN_ID, as Google answered, always of the account " +
		"of the call). Google answers the resource name only, so there is no response_content_type. Empty after " +
		"validate_only"},
}

const keywordPlanCampaignsFieldDoc = "cpc_bid_micros (whole micros of the account currency, the default max CPC " +
	"bid of the campaign), keyword_plan_network (GOOGLE_SEARCH or GOOGLE_SEARCH_AND_PARTNERS), " +
	"language_constants (one languageConstants/ID), and geo_target_constants (1 to 20 distinct " +
	"geoTargetConstants/ID)"

var keywordPlanCampaignProperties = schema{
	"name": keywordPlanNameSchema, "keyword_plan_network": recEnumSchema(keywordNetworks),
	"cpc_bid_micros": campaignBudgetMicrosSchema, "language_constants": keywordPlanLanguageSchema,
	"geo_target_constants": keywordPlanGeoSchema,
}

func keywordPlanCampaignItem(extra schema, required ...string) schema {
	properties := schema{}
	for k, v := range keywordPlanCampaignProperties {
		properties[k] = v
	}
	for k, v := range extra {
		properties[k] = v
	}
	return object(properties, required...)
}

var keywordPlanCampaignsCreate = capability.Descriptor{
	ID: keywordPlanCampaignsCreateID, Version: 1, Title: "Create keyword plan campaigns in a Google Ads account",
	Description: "Create the campaign of one or more keyword plans of a Google Ads account: the keyword_plan_id " +
		"of the plan (its resource name is built from customer_id and keyword_plan_id), a name, the network, " +
		"and the default max CPC bid, and optionally a language and locations." + keywordPlanCampaignsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-campaigns", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, keywordPlanCampaignsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_campaigns": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": keywordPlanCampaignItem(schema{"keyword_plan_id": keywordPlanIDSchema},
				"keyword_plan_id", "name", "keyword_plan_network", "cpc_bid_micros")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_campaigns")),
	OutputSchema: encoded(keywordPlanCampaignsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_campaigns", Required: true, Description: "1 to 50 campaigns, each " +
			"for another keyword_plan_id (the id of an existing plan as reports.search returns it, which holds no " +
			"campaign yet), with name (required, 1 to 255 characters, no control characters), " +
			"keyword_plan_network, cpc_bid_micros (both required) and optionally language_constants and " +
			"geo_target_constants. " + keywordPlanCampaignsFieldDoc},
		validateOnlyArgument),
	Fields: keywordPlanCampaignsFields,
	Examples: []capability.Example{{Description: "Create the campaign of a keyword plan for Germany in German",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_campaigns":[{"keyword_plan_id":"112233",` +
			`"name":"Spring campaign","keyword_plan_network":"GOOGLE_SEARCH","cpc_bid_micros":1000000,` +
			`"language_constants":["languageConstants/1001"],"geo_target_constants":["geoTargetConstants/2276"]}]}`)}},
}

var keywordPlanCampaignsUpdate = capability.Descriptor{
	ID: keywordPlanCampaignsUpdateID, Version: 1, Title: "Change keyword plan campaigns of a Google Ads account",
	Description: "Change existing keyword plan campaigns of a Google Ads account (an update with a field mask " +
		"that names exactly the fields given). Only the keyword_plan_campaign_id selects a campaign: its resource " +
		"name is built from customer_id and keyword_plan_campaign_id. The plan of a campaign is not changed. " +
		"A language_constants or geo_target_constants given replaces the whole list." +
		keywordPlanCampaignsNote + " Offered by a connection with the update effect or whose tools list names " +
		"it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-campaigns", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, keywordPlanCampaignsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_campaigns": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": keywordPlanCampaignItem(schema{"keyword_plan_campaign_id": keywordPlanCampaignIDSchema},
				"keyword_plan_campaign_id")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_campaigns")),
	OutputSchema: encoded(keywordPlanCampaignsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_campaigns", Required: true, Description: "1 to 50 entries, each " +
			"keyword_plan_campaign_id once (the id of the campaign as reports.search returns it), with at least " +
			"one of name (1 to 255 characters), " + keywordPlanCampaignsFieldDoc},
		validateOnlyArgument),
	Fields: keywordPlanCampaignsFields,
	Examples: []capability.Example{{Description: "Raise the default bid of a keyword plan campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_campaigns":[{"keyword_plan_campaign_id":"445566","cpc_bid_micros":1500000}]}`)}},
}

var keywordPlanCampaignsRemove = capability.Descriptor{
	ID: keywordPlanCampaignsRemoveID, Version: 1, Title: "Remove keyword plan campaigns from a Google Ads account",
	Description: "Remove existing keyword plan campaigns of a Google Ads account (a remove of " +
		"customers/ID/keywordPlanCampaigns/CAMPAIGN_ID, built from customer_id and keyword_plan_campaign_id). A " +
		"removed campaign is gone from the account; no tool of this provider brings it back." +
		keywordPlanCampaignsNote + " Offered by a connection with the delete effect or whose tools list names " +
		"it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "keyword-plan-campaigns", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, keywordPlanCampaignsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_campaign_ids": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"uniqueItems": true, "items": keywordPlanCampaignIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_campaign_ids")),
	OutputSchema: encoded(keywordPlanCampaignsOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_campaign_ids", Required: true, Description: "1 to 50 IDs of " +
			"existing keyword plan campaigns, each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: keywordPlanCampaignsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a keyword plan campaign without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_campaign_ids":["445566"],"validate_only":true}`)}},
}

// --- handlers ---

func handleKeywordPlanCampaignsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create keyword plan campaigns"
	var args keywordPlanCampaignsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanCampaignsSend(ctx, resolved, secrets, red, op, keywordPlanCampaignsCreateID, args.CustomerID,
		ops, nil, args.ValidateOnly)
}

func handleKeywordPlanCampaignsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update keyword plan campaigns"
	var args keywordPlanCampaignsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanCampaignsSend(ctx, resolved, secrets, red, op, keywordPlanCampaignsUpdateID, args.CustomerID,
		ops, names, args.ValidateOnly)
}

func handleKeywordPlanCampaignsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove keyword plan campaigns"
	var args keywordPlanCampaignsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlanCampaignsSend(ctx, resolved, secrets, red, op, keywordPlanCampaignsRemoveID, args.CustomerID,
		ops, names, args.ValidateOnly)
}

func keywordPlanCampaignsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
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
		service: keywordPlanCampaignsService, collection: "keywordPlanCampaigns", operations: ops, names: want,
		validateOnly: validateOnly})
	if err != nil {
		return nil, err
	}
	out, err := keywordPlanCampaignsResults(op, results)
	if err != nil {
		return nil, err
	}
	return keywordPlanCampaignsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: out}, nil
}

// --- confirmation ---

// keywordPlanCampaignsTargetText names the optional fields of an entry in the confirmation, cut like every text
// of the arguments.
func keywordPlanCampaignsTargetText(network *string, bid *json.Number, languages, geos []string) string {
	var part string
	if network != nil {
		part += " network " + boundedText(*network, maxCodeText)
	}
	if bid != nil {
		part += " bid " + boundedText(string(*bid), maxCodeText) + " micros"
	}
	if len(languages) > 0 {
		part += " languages " + boundedText(strings.Join(languages[:min(len(languages), 1)], ","), maxCodeText)
	}
	if len(geos) > 0 {
		shown := make([]string, 0, 3)
		for _, g := range geos[:min(len(geos), 3)] {
			shown = append(shown, boundedText(g, maxCodeText))
		}
		part += " locations " + strings.Join(shown, ",") + keywordPlansMore(len(geos), len(shown))
	}
	return part
}

func keywordPlanCampaignsCreateSummary(raw json.RawMessage) string {
	var a keywordPlanCampaignsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, c := range a.KeywordPlanCampaigns[:min(len(a.KeywordPlanCampaigns), 10)] {
		network := c.KeywordPlanNetwork
		parts = append(parts, keywordPlansQuote(c.Name)+" for keyword plan "+boundedText(c.KeywordPlanID, maxCodeText)+
			keywordPlanCampaignsTargetText(&network, c.CPCBidMicros, c.LanguageConstants, c.GeoTargetConstants))
	}
	return keywordPlansLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.KeywordPlanCampaigns)) +
		" keyword plan campaign(s) in Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		keywordPlansMore(len(a.KeywordPlanCampaigns), len(parts)) + "."
}

func keywordPlanCampaignsUpdateSummary(raw json.RawMessage) string {
	var a keywordPlanCampaignsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, c := range a.KeywordPlanCampaigns[:min(len(a.KeywordPlanCampaigns), 10)] {
		part := "keyword plan campaign " + boundedText(c.KeywordPlanCampaignID, maxCodeText)
		if c.Name != nil {
			part += " name " + keywordPlansQuote(*c.Name)
		}
		parts = append(parts, part+keywordPlanCampaignsTargetText(c.KeywordPlanNetwork, c.CPCBidMicros,
			c.LanguageConstants, c.GeoTargetConstants))
	}
	return keywordPlansLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.KeywordPlanCampaigns)) +
		" keyword plan campaign(s) of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		keywordPlansMore(len(a.KeywordPlanCampaigns), len(parts)) + "."
}

func keywordPlanCampaignsRemoveSummary(raw json.RawMessage) string {
	var a keywordPlanCampaignsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.KeywordPlanCampaignIDs[:min(len(a.KeywordPlanCampaignIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return keywordPlansLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.KeywordPlanCampaignIDs)) +
		" keyword plan campaign(s) from Google Ads account " + a.CustomerID + ": " + strings.Join(shown, ", ") +
		keywordPlansMore(len(a.KeywordPlanCampaignIDs), len(shown)) + ". No tool brings them back."
}
