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
	adGroupsCreateID = Provider + ".ad-groups.create"
	adGroupsUpdateID = Provider + ".ad-groups.update"
	adGroupsRemoveID = Provider + ".ad-groups.remove"

	adGroupService = "/adGroups"
)

var adGroupResourceName = resourceNode("resource_name", "Ad group of this account, customers/ID/adGroups/N", "adGroups")

const adGroupSensitivity = "ad groups of a Google Ads account: names, bids, targeting, tracking templates"

type adGroupArguments struct {
	CustomerID          string   `json:"customer_id"`
	AdGroups            []any    `json:"ad_groups"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readAdGroupArguments(op string, raw json.RawMessage) (adGroupArguments, error) {
	var a adGroupArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

var adGroupRequired = []string{"name", "status", "campaign"}

// adGroupCreateOperations checks every ad group to create and builds its operation. Qatlas requires, beyond
// the name Google requires, the status, so that no ad group starts serving by the default of Google, which is
// ENABLED, and the campaign it belongs to.
func adGroupCreateOperations(customerID string, items []any) ([]any, error) {
	if len(items) == 0 || len(items) > maxAdGroups {
		return nil, badRequest("ad_groups must hold one to 10 ad groups")
	}
	out := make([]any, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("ad_groups[%d]", i)}
		if !ok {
			return nil, badRequest(at.snake + " must be an object")
		}
		for _, required := range adGroupRequired {
			if _, found := in[required]; !found {
				return nil, badRequest(at.snake + "." + required + " is required")
			}
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(adGroupTree, in, at)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"create": body})
	}
	return out, nil
}

// adGroupUpdateOperations builds one update with a field mask per ad group. Each ad group is named once.
func adGroupUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAdGroups {
		return nil, nil, badRequest("ad_groups must hold one to 10 ad groups")
	}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("ad_groups[%d]", i)}
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		name, _ := in["resource_name"].(string)
		if !ownedResource(name, customerID, "adGroups") {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/adGroups/N of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("an ad group appears in ad_groups only once")
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
		body, err := b.object(adGroupTree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

func adGroupRemoveOperations(customerID string, resourceNames []string) ([]any, error) {
	if len(resourceNames) == 0 || len(resourceNames) > maxAdGroups {
		return nil, badRequest("resource_names must hold one to 10 ad groups")
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if !ownedResource(name, customerID, "adGroups") {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/adGroups/N of the account of "+
				"customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("an ad group appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	adGroupResult struct {
		ResourceName string         `json:"resource_name"`
		AdGroup      map[string]any `json:"ad_group,omitempty"`
	}
	adGroupsChanged struct {
		CustomerID   string          `json:"customer_id"`
		ValidateOnly bool            `json:"validate_only"`
		Results      []adGroupResult `json:"results"`
	}
)

// adGroupAnswer is the answer of MutateAdGroups.
type adGroupAnswer struct {
	Results []struct {
		ResourceName string          `json:"resourceName"`
		AdGroup      json.RawMessage `json:"adGroup"`
	} `json:"results"`
	PartialFailureError json.RawMessage `json:"partialFailureError"`
}

// adGroupsOf maps the results of a change. One result per operation, in the order sent, each an ad group of
// the account; for an update and a remove it is the ad group asked for. Anything else is an invalid answer
// with an unknown outcome, since the change may have been made.
func adGroupsOf(op string, answer adGroupAnswer, customerID string, validateOnly bool, expected []string, count int) (*adGroupsChanged, error) {
	out := &adGroupsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []adGroupResult{}}
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
		if !ownedResource(r.ResourceName, customerID, "adGroups") || (expected != nil && r.ResourceName != expected[i]) {
			return nil, invalid("a result for another ad group or account than the one asked for")
		}
		item := adGroupResult{ResourceName: r.ResourceName}
		if len(r.AdGroup) > 0 && string(r.AdGroup) != "null" {
			raw, ok := decodeObject(r.AdGroup)
			if ok {
				var members map[string]any
				if members, ok = outputMembers(adGroupOutputTree.fields, raw, customerID); ok {
					name, _ := members["resource_name"].(string)
					ok = name == "" || name == r.ResourceName
					item.AdGroup = members
				}
			}
			if !ok {
				return nil, invalid("an ad group that is not the one asked for")
			}
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptors ---

func adGroupInputSchema(create bool) schema {
	var item schema
	if create {
		item = adGroupTree.inputSchema()
		item["required"] = adGroupRequired
		delete(item, "description")
	} else {
		item = adGroupTree.mutable().inputSchema()
		props := item["properties"].(schema)
		props["resource_name"] = adGroupResourceName.inputSchema()
		item["required"] = []string{"resource_name"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"ad_groups":     schema{"type": "array", "minItems": 1, "maxItems": maxAdGroups, "items": item},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "ad_groups")
}

var adGroupOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "ad_group": adGroupOutputTree.outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results")

var adGroupFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per ad group, in the order given, with its resource_name and, with " +
		"MUTABLE_RESOURCE, the ad group after the change with the members the tool offers and the reservation price; " +
		"empty after validate_only"}}

var adGroupCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the " +
	"account without hyphens; a target of the connection or an account below a manager target"}

const adGroupSpendNote = "An ad group that is ENABLED serves in its campaign and bids within the budget of the campaign, and " +
	"its bids decide how much each click or view costs; the call is confirmed per call. Resource names in the " +
	"arguments (campaign) must belong to customer_id. The tracking template and the final URL suffix can pass click " +
	"data to a third party, so the template must be an https address without user info and the suffix must be " +
	"parameters, not an address. Up to 10 ad groups per call, one atomic request without partial failure."

var adGroupsCreate = capability.Descriptor{
	ID: adGroupsCreateID, Version: 1, Title: "Create Google Ads ad groups",
	Description: "Create up to 10 ad groups in a Google Ads account (AdGroupService.MutateAdGroups with create " +
		"operations): Google applies all of them or none. Each ad group needs name, campaign and status (ENABLED or " +
		"PAUSED: Qatlas has no default, so that no ad group starts serving unintended); type, bids, targeting " +
		"and the settings are as the AdGroup resource of v26 has them. " + adGroupSpendNote +
		" Not offered: members Google fills itself, the reservation price (fixed_cpm_micros), and the type " +
		"SHOPPING_SMART_ADS, which Google no longer creates." + mutateListListedNote,
	Tags: []string{"google", "ads", "ad-groups", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, adGroupSensitivity),
	InputSchema: encoded(adGroupInputSchema(true)), OutputSchema: encoded(adGroupOutputSchema),
	Arguments: []capability.Argument{adGroupCustomerArgument,
		{Name: "ad_groups", Required: true, Description: "One to 10 ad groups, each an object of the members of the AdGroup " +
			"resource: name, campaign, status (required) and the optional rest, nested as the resource nests them"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: adGroupFields,
	Examples: []capability.Example{{Description: "Validate a paused Search ad group with a CPC bid without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ad_groups":[{"name":"Shoes","status":"PAUSED",` +
			`"campaign":"customers/1234567890/campaigns/21","type":"SEARCH_STANDARD","cpc_bid_micros":750000}],"validate_only":true}`)}},
}

var adGroupsUpdate = capability.Descriptor{
	ID: adGroupsUpdateID, Version: 1, Title: "Change Google Ads ad groups",
	Description: "Change up to 10 ad groups of a Google Ads account (AdGroupService.MutateAdGroups with update " +
		"operations and a field mask that names exactly the members given): Google applies all of them or none. " +
		"Each entry names the ad group by resource_name and the members to change, nested as the AdGroup " +
		"resource nests them; a list member replaces the whole list, and members that Google accepts only at " +
		"creation (the campaign and the type) are not offered. Setting status to ENABLED starts serving, and " +
		"setting a bid changes the spend. " + adGroupSpendNote + mutateListListedNote,
	Tags: []string{"google", "ads", "ad-groups", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, adGroupSensitivity),
	InputSchema: encoded(adGroupInputSchema(false)), OutputSchema: encoded(adGroupOutputSchema),
	Arguments: []capability.Argument{adGroupCustomerArgument,
		{Name: "ad_groups", Required: true, Description: "One to 10 entries: resource_name of the ad group and at least " +
			"one member to change"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: adGroupFields,
	Examples: []capability.Example{{Description: "Pause an ad group", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
		`"ad_groups":[{"resource_name":"customers/1234567890/adGroups/31","status":"PAUSED"}]}`)}},
}

var adGroupsRemove = capability.Descriptor{
	ID: adGroupsRemoveID, Version: 1, Title: "Remove Google Ads ad groups",
	Description: "Remove up to 10 ad groups of a Google Ads account (AdGroupService.MutateAdGroups with remove " +
		"operations): Google applies all of them or none. A removed ad group stops serving and cannot be enabled " +
		"again; its history stays readable with reports.search. Resource names must be ad groups of customer_id. " +
		"Offers no response_content_type: a removed ad group has no mutable resource to show." + mutateListListedNote,
	Tags: []string{"google", "ads", "ad-groups", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, adGroupSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxAdGroups,
			"items": adGroupResourceName.inputSchema()},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(adGroupOutputSchema),
	Arguments: []capability.Argument{adGroupCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 ad groups of this account, customers/ID/adGroups/N"},
		validateOnlyArgument},
	Fields: adGroupFields,
	Examples: []capability.Example{{Description: "Validate the removal of an ad group without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/adGroups/31"],"validate_only":true}`)}},
}

// --- handlers ---

func handleAdGroupsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupsChange(ctx, resolved, secrets, red, raw, "create ad groups", adGroupsCreateID)
}

func handleAdGroupsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupsChange(ctx, resolved, secrets, red, raw, "update ad groups", adGroupsUpdateID)
}

func handleAdGroupsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupsChange(ctx, resolved, secrets, red, raw, "remove ad groups", adGroupsRemoveID)
}

func adGroupsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readAdGroupArguments(op, raw)
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
	case adGroupsCreateID:
		ops, err = adGroupCreateOperations(a.CustomerID, a.AdGroups)
	case adGroupsUpdateID:
		ops, expected, err = adGroupUpdateOperations(a.CustomerID, a.AdGroups)
	default:
		ops, err = adGroupRemoveOperations(a.CustomerID, a.ResourceNames)
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
	var answer adGroupAnswer
	body := mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: adGroupService, body: body},
		&answer); err != nil {
		return nil, err
	}
	return adGroupsOf(op, answer, a.CustomerID, a.ValidateOnly, expected, len(ops))
}

// --- confirmation ---

// adGroupSummary phrases the change for the confirmation, from the arguments alone.
func adGroupSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readAdGroupArguments("", raw)
		verb := map[string]string{adGroupsCreateID: "Create", adGroupsUpdateID: "Change", adGroupsRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		var parts []string
		if tool == adGroupsRemoveID {
			parts = a.ResourceNames
		}
		for _, item := range a.AdGroups {
			in, _ := item.(map[string]any)
			part := stringOf(in["name"])
			if part == "" {
				part = stringOf(in["resource_name"])
			}
			var facts []string
			for _, key := range []string{"type", "status", "campaign"} {
				if v := stringOf(in[key]); v != "" {
					facts = append(facts, key+" "+v)
				}
			}
			var keys []string
			for key := range in {
				if key != "resource_name" && key != "name" {
					keys = append(keys, key)
				}
			}
			slices.Sort(keys)
			if tool == adGroupsUpdateID {
				facts = append(facts, "members "+strings.Join(keys, "+"))
			} else {
				for _, key := range keys {
					if strings.HasSuffix(key, "_bid_micros") || strings.HasPrefix(key, "target_") {
						facts = append(facts, key)
					}
				}
			}
			parts = append(parts, part+" ("+strings.Join(facts, ", ")+")")
		}
		summary := verb + " ad groups of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + "."
		if tool == adGroupsRemoveID {
			return summary + " A removed ad group cannot be enabled again."
		}
		return summary + " An ENABLED ad group serves and bids within the budget of its campaign; its bids decide how much it costs."
	}
}
