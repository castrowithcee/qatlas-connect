package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	assetGroupsCreateID = Provider + ".asset-groups.create"
	assetGroupsUpdateID = Provider + ".asset-groups.update"
	assetGroupsRemoveID = Provider + ".asset-groups.remove"

	assetGroupsService    = "/assetGroups"
	assetGroupsCollection = "assetGroups"

	// maxAssetGroups is Qatlas's own bound per call; the reference of MutateAssetGroups states none.
	maxAssetGroups      = 10
	maxAssetGroupURLs   = 20
	maxAssetGroupParams = 50
	// assetGroupNameMax is the bound of AssetGroup.name in v26: 1 to 128 characters.
	assetGroupNameMax = 128
)

// The members of the AssetGroup resource of v26 that the tools offer. Not offered: the members Google fills
// itself (output only): id, primary_status and its reasons, ad_strength and asset_coverage, which
// reports.search reads. The resource has no deprecated member. MutateAssetGroups has no response_content_type
// and no partial_failure in the request, so the answer holds resource names only.

var assetGroupResourceName = resourceNode("resource_name", "Asset group of this account, customers/ID/assetGroups/N", assetGroupsCollection)

const assetGroupSensitivity = "asset groups of a Performance Max campaign: names, URLs, tracking templates"

func checkAssetGroupName(argument, value string) error {
	if value == "" || utf8.RuneCountInString(value) > assetGroupNameMax || hasControl(value) {
		return badRequest(argument + " must be a name of 1 to 128 characters without control characters")
	}
	return nil
}

func assetGroupMembers() []node {
	name := textNode("name", "Name of the asset group, 1 to 128 characters; unique within the campaign", assetGroupNameMax)
	name.check = checkAssetGroupName
	finalURLs := textNode("final_urls", "Final URL, an https address without user info; Performance Max may expand it", maxURLArgument)
	finalURLs.check = requireHTTPS
	finalMobileURLs := textNode("final_mobile_urls", "Final mobile URL, an https address without user info", maxURLArgument)
	finalMobileURLs.check = requireHTTPS
	return []node{
		name,
		enumNode("status", "ENABLED serves and spends within the budget of the campaign; PAUSED does not. An asset group is removed with asset-groups.remove",
			[]string{"ENABLED", "PAUSED"}),
		createOnly(resourceNode("campaign", "Performance Max campaign of this account the asset group belongs to, customers/ID/campaigns/N; fixed after creation", "campaigns")),
		listOf(finalURLs, maxAssetGroupURLs),
		listOf(finalMobileURLs, maxAssetGroupURLs),
		textNode("path1", "First path segment shown after the display URL", 64),
		textNode("path2", "Second path segment shown after the display URL; Google accepts it only when path1 is set", 64),
		addressNode("tracking_url_template", "Tracking template: an https address without user info, up to 2048 bytes, ValueTrack parameters such as {lpurl} after the host; empty on an update clears it", checkAddress),
		addressNode("final_url_suffix", "Parameters appended to final URLs, such as utm_source=x, no address, up to 2048 bytes; empty on an update clears it", checkSuffix),
		listOf(objectNode("url_custom_parameters", "Custom parameters for the tracking template and the URLs; the list replaces the current one",
			patternNode("key", "Name of the parameter tag", keyPattern, 64), textNode("value", "Value substituted for the tag", maxNameArgument)), maxAssetGroupParams),
		objectNode("google_local_services_info", "Local Services information of the asset group",
			createOnly(textNode("category_id", "Local Services category in XCAT form; fixed after creation", 128)),
			listOf(objectNode("callouts", "Callouts the advertiser selected; the list replaces the current one",
				textNode("callout_id", "ID of the callout; required", 128)), maxAssetGroupParams)),
	}
}

// assetGroupTree holds the members of an asset group that the tools offer.
var assetGroupTree = objectNode("asset_group", "", assetGroupMembers()...)

var assetGroupRequired = []string{"name", "campaign", "status"}

type assetGroupArguments struct {
	CustomerID    string   `json:"customer_id"`
	AssetGroups   []any    `json:"asset_groups"`
	ResourceNames []string `json:"resource_names"`
	ValidateOnly  bool     `json:"validate_only"`
}

func readAssetGroupArguments(op string, raw json.RawMessage) (assetGroupArguments, error) {
	var a assetGroupArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

// assetGroupItems checks the number of entries and that each one is an object.
func assetGroupItems(items []any) ([]map[string]any, error) {
	if len(items) == 0 || len(items) > maxAssetGroups {
		return nil, badRequest("asset_groups must hold one to 10 asset groups")
	}
	out := make([]map[string]any, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		if !ok {
			return nil, badRequest(fmt.Sprintf("asset_groups[%d] must be an object", i))
		}
		out[i] = in
	}
	return out, nil
}

// assetGroupCreateOperations requires, beyond the name Google requires, the status, so that no asset group
// starts serving by the default of Google, and the campaign it belongs to.
func assetGroupCreateOperations(customerID string, items []any) ([]any, error) {
	entries, err := assetGroupItems(items)
	if err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(entries))
	for i, in := range entries {
		at := pathOfMember{snake: fmt.Sprintf("asset_groups[%d]", i)}
		for _, required := range assetGroupRequired {
			if _, found := in[required]; !found {
				return nil, badRequest(at.snake + "." + required + " is required")
			}
		}
		if _, has2 := in["path2"]; has2 {
			if _, has1 := in["path1"]; !has1 {
				return nil, badRequest(at.snake + ".path2 can only be set together with path1")
			}
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(assetGroupTree, in, at)
		if err != nil {
			return nil, err
		}
		ops = append(ops, map[string]any{"create": body})
	}
	return ops, nil
}

// assetGroupUpdateOperations builds one update with a field mask per asset group. Each is named once.
func assetGroupUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	entries, err := assetGroupItems(items)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(entries))
	names := make([]string, 0, len(entries))
	for i, in := range entries {
		at := pathOfMember{snake: fmt.Sprintf("asset_groups[%d]", i)}
		name, _ := in["resource_name"].(string)
		if !ownedResource(name, customerID, assetGroupsCollection) {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/assetGroups/N of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("an asset group appears in asset_groups only once")
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
		body, err := b.object(assetGroupTree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

func assetGroupRemoveOperations(customerID string, resourceNames []string) ([]any, error) {
	if len(resourceNames) == 0 || len(resourceNames) > maxAssetGroups {
		return nil, badRequest("resource_names must hold one to 10 asset groups")
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if !ownedResource(name, customerID, assetGroupsCollection) {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/assetGroups/N of the account of "+
				"customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("an asset group appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	assetGroupResult struct {
		ResourceName string `json:"resource_name"`
	}
	assetGroupsChanged struct {
		CustomerID   string             `json:"customer_id"`
		ValidateOnly bool               `json:"validate_only"`
		Results      []assetGroupResult `json:"results"`
	}
)

// --- descriptors ---

func assetGroupInputSchema(create bool) schema {
	var item schema
	if create {
		item = assetGroupTree.inputSchema()
		item["required"] = assetGroupRequired
		delete(item, "description")
	} else {
		item = assetGroupTree.mutable().inputSchema()
		props := item["properties"].(schema)
		props["resource_name"] = assetGroupResourceName.inputSchema()
		item["required"] = []string{"resource_name"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"asset_groups":  schema{"type": "array", "minItems": 1, "maxItems": maxAssetGroups, "items": item},
		"validate_only": validateOnlySchema}, "customer_id", "asset_groups")
}

var assetGroupOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema}, "resource_name"))},
	"customer_id", "validate_only", "results")

var assetGroupFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per asset group, in the order given, with its resource_name; empty after " +
		"validate_only. The service answers no resource, so the asset group is read with reports.search"}}

var assetGroupCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the " +
	"account without hyphens; a target of the connection or an account below a manager target"}

const assetGroupSpendNote = "An asset group that is ENABLED serves in its Performance Max campaign and spends within the " +
	"budget of the campaign; the call is confirmed per call. The campaign in the arguments must belong to " +
	"customer_id. Final URLs, the tracking template and the final URL suffix can pass click data to a third party, so " +
	"URLs and the template must be https addresses without user info and the suffix must be parameters, not an " +
	"address. Up to 10 asset groups per call, one atomic request without partial failure. Assets, signals and " +
	"listing group filters of the group have their own tools."

var assetGroupsCreate = capability.Descriptor{
	ID: assetGroupsCreateID, Version: 1, Title: "Create Google Ads asset groups",
	Description: "Create up to 10 asset groups of Performance Max campaigns in a Google Ads account " +
		"(AssetGroupService.MutateAssetGroups with create operations): Google applies all of them or none. Each " +
		"needs name, campaign and status (ENABLED or PAUSED: Qatlas has no default, so that no asset group starts " +
		"serving unintended); the other members are as the AssetGroup resource of v26 has them. " +
		assetGroupSpendNote + " Not offered: the members Google fills itself." + mutateListListedNote,
	Tags: []string{"google", "ads", "asset-groups", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, assetGroupSensitivity),
	InputSchema: encoded(assetGroupInputSchema(true)), OutputSchema: encoded(assetGroupOutputSchema),
	Arguments: []capability.Argument{assetGroupCustomerArgument,
		{Name: "asset_groups", Required: true, Description: "One to 10 asset groups, each an object of the members of the " +
			"AssetGroup resource: name, campaign, status (required) and the optional rest, nested as the resource nests them"},
		validateOnlyArgument},
	Fields: assetGroupFields,
	Examples: []capability.Example{{Description: "Validate a paused asset group without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","asset_groups":[{"name":"Shoes","status":"PAUSED",` +
			`"campaign":"customers/1234567890/campaigns/21","final_urls":["https://www.example.com/shoes"]}],"validate_only":true}`)}},
}

var assetGroupsUpdate = capability.Descriptor{
	ID: assetGroupsUpdateID, Version: 1, Title: "Change Google Ads asset groups",
	Description: "Change up to 10 asset groups of a Google Ads account (AssetGroupService.MutateAssetGroups with " +
		"update operations and a field mask that names exactly the members given): Google applies all of them or " +
		"none. Each entry names the asset group by resource_name and the members to change, nested as the " +
		"AssetGroup resource nests them; a list member replaces the whole list, and members that Google accepts " +
		"only at creation (the campaign and the Local Services category) are not offered. Setting status to " +
		"ENABLED starts serving. " + assetGroupSpendNote + mutateListListedNote,
	Tags: []string{"google", "ads", "asset-groups", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, assetGroupSensitivity),
	InputSchema: encoded(assetGroupInputSchema(false)), OutputSchema: encoded(assetGroupOutputSchema),
	Arguments: []capability.Argument{assetGroupCustomerArgument,
		{Name: "asset_groups", Required: true, Description: "One to 10 entries: resource_name of the asset group and at " +
			"least one member to change"}, validateOnlyArgument},
	Fields: assetGroupFields,
	Examples: []capability.Example{{Description: "Pause an asset group", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
		`"asset_groups":[{"resource_name":"customers/1234567890/assetGroups/31","status":"PAUSED"}]}`)}},
}

var assetGroupsRemove = capability.Descriptor{
	ID: assetGroupsRemoveID, Version: 1, Title: "Remove Google Ads asset groups",
	Description: "Remove up to 10 asset groups of a Google Ads account (AssetGroupService.MutateAssetGroups with " +
		"remove operations): Google applies all of them or none. A removed asset group stops serving and cannot be " +
		"enabled again; its history stays readable with reports.search. Resource names must be asset groups of " +
		"customer_id." + mutateListListedNote,
	Tags: []string{"google", "ads", "asset-groups", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, assetGroupSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxAssetGroups,
			"items": assetGroupResourceName.inputSchema()},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(assetGroupOutputSchema),
	Arguments: []capability.Argument{assetGroupCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 asset groups of this account, customers/ID/assetGroups/N"},
		validateOnlyArgument},
	Fields: assetGroupFields,
	Examples: []capability.Example{{Description: "Validate the removal of an asset group without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/assetGroups/31"],"validate_only":true}`)}},
}

// --- handlers ---

func handleAssetGroupsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetGroupsChange(ctx, resolved, secrets, red, raw, "create asset groups", assetGroupsCreateID)
}

func handleAssetGroupsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetGroupsChange(ctx, resolved, secrets, red, raw, "update asset groups", assetGroupsUpdateID)
}

func handleAssetGroupsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetGroupsChange(ctx, resolved, secrets, red, raw, "remove asset groups", assetGroupsRemoveID)
}

func assetGroupsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readAssetGroupArguments(op, raw)
	if err != nil {
		return nil, err
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	var ops []any
	var expected []string
	switch tool {
	case assetGroupsCreateID:
		ops, err = assetGroupCreateOperations(a.CustomerID, a.AssetGroups)
	case assetGroupsUpdateID:
		ops, expected, err = assetGroupUpdateOperations(a.CustomerID, a.AssetGroups)
	default:
		ops, err = assetGroupRemoveOperations(a.CustomerID, a.ResourceNames)
		expected = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: a.CustomerID,
		service: assetGroupsService, collection: assetGroupsCollection, resourceKey: "assetGroup", operations: ops,
		names: expected, validateOnly: a.ValidateOnly})
	if err != nil {
		return nil, err
	}
	out := assetGroupsChanged{CustomerID: a.CustomerID, ValidateOnly: a.ValidateOnly, Results: []assetGroupResult{}}
	for _, r := range results {
		out.Results = append(out.Results, assetGroupResult{ResourceName: r.ResourceName})
	}
	return out, nil
}

// --- confirmation ---

// assetGroupSummary phrases the change for the confirmation, from the arguments alone.
func assetGroupSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readAssetGroupArguments("", raw)
		verb := map[string]string{assetGroupsCreateID: "Create", assetGroupsUpdateID: "Change", assetGroupsRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		var parts []string
		if tool == assetGroupsRemoveID {
			parts = a.ResourceNames
		}
		for _, item := range a.AssetGroups {
			in, _ := item.(map[string]any)
			part := stringOf(in["name"])
			if part == "" {
				part = stringOf(in["resource_name"])
			}
			var facts []string
			for _, key := range []string{"status", "campaign"} {
				if v := stringOf(in[key]); v != "" {
					facts = append(facts, key+" "+v)
				}
			}
			if tool == assetGroupsUpdateID {
				var keys []string
				for key := range in {
					if key != "resource_name" && key != "name" {
						keys = append(keys, key)
					}
				}
				slices.Sort(keys)
				facts = append(facts, "members "+strings.Join(keys, "+"))
			}
			parts = append(parts, part+" ("+strings.Join(facts, ", ")+")")
		}
		summary := verb + " asset groups of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + "."
		if tool == assetGroupsRemoveID {
			return summary + " A removed asset group cannot be enabled again."
		}
		return summary + " An ENABLED asset group serves and spends within the budget of its Performance Max campaign."
	}
}
