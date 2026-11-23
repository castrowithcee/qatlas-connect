package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	campaignsCreateID = Provider + ".campaigns.create"
	campaignsUpdateID = Provider + ".campaigns.update"
	campaignsRemoveID = Provider + ".campaigns.remove"

	campaignService = "/campaigns"
)

// campaignOutputTree is the campaign Google returns with MUTABLE_RESOURCE: the members the tools offer and
// the resource name.
var campaignOutputTree = func() node {
	t := campaignTree
	t.fields = append(slices.Clone(t.fields), textNode("resource_name", "", maxNameArgument))
	return t
}()

var campaignResourceName = resourceNode("resource_name", "Campaign of this account, customers/ID/campaigns/N", "campaigns")

const campaignSensitivity = "campaigns of a Google Ads account: names, budgets, bidding, targeting, tracking templates"

type campaignArguments struct {
	CustomerID          string   `json:"customer_id"`
	Campaigns           []any    `json:"campaigns"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readCampaignArguments(op string, raw json.RawMessage) (campaignArguments, error) {
	var a campaignArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

// createOperations checks every campaign to create and builds its operation. A campaign names, as Google
// requires, its name and its channel type; Qatlas also requires the status, so that no campaign starts
// serving by the default of Google, which is ENABLED.
func createOperations(customerID string, items []any) ([]any, error) {
	if len(items) == 0 || len(items) > maxCampaigns {
		return nil, badRequest("campaigns must hold one to 10 campaigns")
	}
	out := make([]any, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("campaigns[%d]", i)}
		if !ok {
			return nil, badRequest(at.snake + " must be an object")
		}
		for _, required := range []string{"name", "status", "advertising_channel_type"} {
			if _, found := in[required]; !found {
				return nil, badRequest(at.snake + "." + required + " is required")
			}
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(campaignTree, in, at)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"create": body})
	}
	return out, nil
}

// updateOperations builds one update with a field mask per campaign. Each campaign is named once.
func updateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxCampaigns {
		return nil, nil, badRequest("campaigns must hold one to 10 campaigns")
	}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("campaigns[%d]", i)}
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		name, _ := in["resource_name"].(string)
		if !ownedResource(name, customerID, "campaigns") {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/campaigns/N of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("a campaign appears in campaigns only once")
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
		b := builder{customerID: customerID}
		body, err := b.object(campaignTree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

func removeOperations(customerID string, resourceNames []string) ([]any, error) {
	if len(resourceNames) == 0 || len(resourceNames) > maxCampaigns {
		return nil, badRequest("resource_names must hold one to 10 campaigns")
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if !ownedResource(name, customerID, "campaigns") {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/campaigns/N of the account of "+
				"customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("a campaign appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	campaignResult struct {
		ResourceName string         `json:"resource_name"`
		Campaign     map[string]any `json:"campaign,omitempty"`
	}
	campaignsChanged struct {
		CustomerID   string           `json:"customer_id"`
		ValidateOnly bool             `json:"validate_only"`
		Results      []campaignResult `json:"results"`
	}
)

// campaignAnswer is the answer of MutateCampaigns.
type campaignAnswer struct {
	Results []struct {
		ResourceName string          `json:"resourceName"`
		Campaign     json.RawMessage `json:"campaign"`
	} `json:"results"`
	PartialFailureError json.RawMessage `json:"partialFailureError"`
}

// campaignsOf maps the results of a change. One result per operation, in the order sent, each a campaign of the
// account; for an update and a remove it is the campaign asked for. Anything else is an invalid answer with
// an unknown outcome, since the change may have been made.
func campaignsOf(op string, answer campaignAnswer, customerID string, validateOnly bool, expected []string, count int) (*campaignsChanged, error) {
	out := &campaignsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []campaignResult{}}
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
		if !ownedResource(r.ResourceName, customerID, "campaigns") || (expected != nil && r.ResourceName != expected[i]) {
			return nil, invalid("a result for another campaign or account than the one asked for")
		}
		item := campaignResult{ResourceName: r.ResourceName}
		if len(r.Campaign) > 0 && string(r.Campaign) != "null" {
			raw, ok := decodeObject(r.Campaign)
			if ok {
				var members map[string]any
				if members, ok = outputMembers(campaignOutputTree.fields, raw, customerID); ok {
					name, _ := members["resource_name"].(string)
					ok = name == "" || name == r.ResourceName
					item.Campaign = members
				}
			}
			if !ok {
				return nil, invalid("a campaign that is not the one asked for")
			}
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptors ---

var campaignRequired = []string{"name", "status", "advertising_channel_type"}

func campaignInputSchema(create bool) schema {
	var item schema
	if create {
		item = campaignTree.inputSchema()
		item["required"] = campaignRequired
		delete(item, "description")
	} else {
		item = campaignTree.mutable().inputSchema()
		props := item["properties"].(schema)
		props["resource_name"] = campaignResourceName.inputSchema()
		item["required"] = []string{"resource_name"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"campaigns":     schema{"type": "array", "minItems": 1, "maxItems": maxCampaigns, "items": item},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "campaigns")
}

var campaignOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "campaign": campaignOutputTree.outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results")

var campaignFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per campaign, in the order given, with its resource_name and, with " +
		"MUTABLE_RESOURCE, the campaign after the change with the members the tool offers; empty after validate_only"}}

var campaignCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the " +
	"account without hyphens; a target of the connection or an account below a manager target"}

const campaignSpendNote = "A campaign that is ENABLED serves and spends money within its budget, and bidding, budget and " +
	"networks decide how much; the call is confirmed per call. Resource names in the arguments (budget, bidding " +
	"strategy, group, assets) must belong to customer_id, and an ID that is negative, a temporary one, is refused: " +
	"create what a campaign refers to first, then name it. The tracking template and the final URL suffix can pass " +
	"click data to a third party, so the template must be an https address without user info and the suffix must be " +
	"parameters, not an address. Up to 10 campaigns per call, one atomic request without partial failure."

var campaignsCreate = capability.Descriptor{
	ID: campaignsCreateID, Version: 1, Title: "Create Google Ads campaigns",
	Description: "Create up to 10 campaigns in a Google Ads account (CampaignService.MutateCampaigns with create " +
		"operations): Google applies all of them or none. Each campaign needs name, status (ENABLED or PAUSED: " +
		"Qatlas has no default, so that no campaign starts serving unintended) and advertising_channel_type; " +
		"budget, bidding and the settings of the channel are as the Campaign resource of v26 has them, with at " +
		"most one of the bidding members (a portfolio bidding_strategy or one embedded strategy). " +
		campaignSpendNote + " Not offered: members Google fills itself, deprecated members, reservation bidding " +
		"(fixed_cpm), third-party integration partners and advertising partners, and phone numbers." + mutateListListedNote,
	Tags: []string{"google", "ads", "campaigns", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, campaignSensitivity),
	InputSchema: encoded(campaignInputSchema(true)), OutputSchema: encoded(campaignOutputSchema),
	Arguments: []capability.Argument{campaignCustomerArgument,
		{Name: "campaigns", Required: true, Description: "One to 10 campaigns, each an object of the members of the Campaign " +
			"resource: name, status, advertising_channel_type (required) and the optional rest, nested as the " +
			"resource nests them"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: campaignFields,
	Examples: []capability.Example{{Description: "Validate a paused Search campaign with manual bidding without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaigns":[{"name":"Spring sale","status":"PAUSED",` +
			`"advertising_channel_type":"SEARCH","campaign_budget":"customers/1234567890/campaignBudgets/11",` +
			`"manual_cpc":{"enhanced_cpc_enabled":false},"network_settings":{"target_google_search":true}}],"validate_only":true}`)}},
}

var campaignsUpdate = capability.Descriptor{
	ID: campaignsUpdateID, Version: 1, Title: "Change Google Ads campaigns",
	Description: "Change up to 10 campaigns of a Google Ads account (CampaignService.MutateCampaigns with update " +
		"operations and a field mask that names exactly the members given): Google applies all of them or none. " +
		"Each entry names the campaign by resource_name and the members to change, nested as the Campaign " +
		"resource nests them; a list member replaces the whole list, and members that Google accepts only at " +
		"creation (the channel type, for one) are not offered. Setting status to ENABLED starts serving, and " +
		"setting a budget, bidding or networks changes the spend. " + campaignSpendNote + mutateListListedNote,
	Tags: []string{"google", "ads", "campaigns", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, campaignSensitivity),
	InputSchema: encoded(campaignInputSchema(false)), OutputSchema: encoded(campaignOutputSchema),
	Arguments: []capability.Argument{campaignCustomerArgument,
		{Name: "campaigns", Required: true, Description: "One to 10 entries: resource_name of the campaign and at least " +
			"one member to change"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: campaignFields,
	Examples: []capability.Example{{Description: "Pause a campaign", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
		`"campaigns":[{"resource_name":"customers/1234567890/campaigns/21","status":"PAUSED"}]}`)}},
}

var campaignsRemove = capability.Descriptor{
	ID: campaignsRemoveID, Version: 1, Title: "Remove Google Ads campaigns",
	Description: "Remove up to 10 campaigns of a Google Ads account (CampaignService.MutateCampaigns with remove " +
		"operations): Google applies all of them or none. A removed campaign stops serving and cannot be enabled " +
		"again; its history stays readable with reports.search. Resource names must be campaigns of customer_id. " +
		"Offers no response_content_type: a removed campaign has no mutable resource to show." + mutateListListedNote,
	Tags: []string{"google", "ads", "campaigns", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, campaignSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxCampaigns,
			"items": campaignResourceName.inputSchema()},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(campaignOutputSchema),
	Arguments: []capability.Argument{campaignCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 campaigns of this account, customers/ID/campaigns/N"},
		validateOnlyArgument},
	Fields: campaignFields,
	Examples: []capability.Example{{Description: "Validate the removal of a campaign without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/campaigns/21"],"validate_only":true}`)}},
}

// --- handlers ---

func handleCampaignsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return campaignsChange(ctx, resolved, secrets, red, raw, "create campaigns", campaignsCreateID)
}

func handleCampaignsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return campaignsChange(ctx, resolved, secrets, red, raw, "update campaigns", campaignsUpdateID)
}

func handleCampaignsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return campaignsChange(ctx, resolved, secrets, red, raw, "remove campaigns", campaignsRemoveID)
}

func campaignsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readCampaignArguments(op, raw)
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
	case campaignsCreateID:
		ops, err = createOperations(a.CustomerID, a.Campaigns)
	case campaignsUpdateID:
		ops, expected, err = updateOperations(a.CustomerID, a.Campaigns)
	default:
		ops, err = removeOperations(a.CustomerID, a.ResourceNames)
		expected = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	body := map[string]any{"operations": ops}
	if a.ValidateOnly {
		body["validateOnly"] = true
	}
	if a.ResponseContentType != "" {
		body["responseContentType"] = a.ResponseContentType
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer campaignAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: campaignService, body: body},
		&answer); err != nil {
		return nil, err
	}
	return campaignsOf(op, answer, a.CustomerID, a.ValidateOnly, expected, len(ops))
}

// --- confirmation ---

// campaignSummary phrases the change for the confirmation, from the arguments alone.
func campaignSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readCampaignArguments("", raw)
		verb := map[string]string{campaignsCreateID: "Create", campaignsUpdateID: "Change", campaignsRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		var parts []string
		if tool == campaignsRemoveID {
			parts = a.ResourceNames
		}
		for _, item := range a.Campaigns {
			in, _ := item.(map[string]any)
			part := stringOf(in["name"])
			if part == "" {
				part = stringOf(in["resource_name"])
			}
			var facts []string
			for _, key := range []string{"advertising_channel_type", "status", "campaign_budget", "bidding_strategy"} {
				if v := stringOf(in[key]); v != "" {
					facts = append(facts, key+" "+v)
				}
			}
			for _, key := range biddingNames() {
				if _, ok := in[key]; ok && key != "bidding_strategy" {
					facts = append(facts, "bidding "+key)
				}
			}
			if tool == campaignsUpdateID {
				var keys []string
				for key := range in {
					if key != "resource_name" {
						keys = append(keys, key)
					}
				}
				slices.Sort(keys)
				facts = append(facts, "members "+strings.Join(keys, "+"))
			}
			parts = append(parts, part+" ("+strings.Join(facts, ", ")+")")
		}
		summary := verb + " campaigns of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + "."
		switch tool {
		case campaignsRemoveID:
			return summary + " A removed campaign cannot be enabled again."
		default:
			return summary + " An ENABLED campaign serves and spends within its budget; budget, bidding and networks decide how much."
		}
	}
}

func stringOf(value any) string {
	s, _ := value.(string)
	return s
}
