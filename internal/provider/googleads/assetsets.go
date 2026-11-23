package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	assetSetsCreateID = Provider + ".asset-sets.create"
	assetSetsUpdateID = Provider + ".asset-sets.update"
	assetSetsRemoveID = Provider + ".asset-sets.remove"

	assetSetsService    = "/assetSets"
	assetSetsCollection = "assetSets"

	// assetSetNameMax is the bound of AssetSet.name in v26: 1 to 128 characters.
	assetSetNameMax = 128
	// The bounds of the lists and texts below are Qatlas's own; the reference of v26 names none.
	assetSetMaxList = 50
	assetSetMaxText = 256
)

// The members of the AssetSet resource of v26 that asset-sets.create and asset-sets.update offer. Not offered:
//   - the members Google fills itself (output only): id, status, hotel_property_data and travel_feed_data
//     are answered but never sent
//   - business_profile_location_set of location_set as an argument: it carries http_authorization_token, a
//     credential for the Business Profile of another party, which never passes through these tools and is
//     never answered. Google requires it, so a location sync from a Business Profile cannot be
//     set up here; chain and Maps location syncs, location groups and the other types can
//
// The members that Google accepts only when the set is created (type, location_group_parent_asset_set_id,
// location_ownership_type, relationship_type) are refused on an update.

// assetSetChains is the list of ChainFilter entries of a chain location set and a chain location group.
func assetSetChains(name, desc string) node {
	return listOf(objectNode(name, desc,
		wholeNode("chain_id", "ID of the chain", 1, math64),
		listOf(textNode("location_attributes", "Attributes a location must all match; empty or left out means no filtering",
			assetSetMaxText), assetSetMaxList)).with(func(n *node) { n.required, n.requiredAlways = []string{"chain_id"}, true }),
		assetSetMaxList)
}

func assetSetBusinessNameFilter() node {
	return objectNode("business_name_filter", "Keeps only listings with a matching business name",
		textNode("business_name", "Business name to match", assetSetMaxText),
		enumNode("filter_type", "How the name is matched", enumNamed("LocationStringFilterTypeEnum.LocationStringFilterType")))
}

func assetSetListingIDs(desc string) node {
	return listOf(wholeNode("listing_id_filters", desc, 1, math64), assetSetMaxList)
}

// assetSetMembers are the members of an asset set. output adds what Google answers and no request can set.
func assetSetMembers(output bool) []node {
	locationSet := []node{
		createOnly(enumNode("location_ownership_type", "Whether the locations are owned or affiliate locations; fixed after creation",
			enumNamed("LocationOwnershipTypeEnum.LocationOwnershipType"))),
		objectNode("chain_location_set", "Locations of the chains named; sync source chain",
			createOnly(enumNode("relationship_type", "Relationship to the chains; fixed after creation",
				enumNamed("ChainRelationshipTypeEnum.ChainRelationshipType"))),
			assetSetChains("chains", "Chains whose locations are synced; the list replaces the current one")).
			with(func(n *node) { n.required = []string{"relationship_type", "chains"} }),
		objectNode("maps_location_set", "Selected Maps locations; sync source Maps",
			listOf(objectNode("maps_locations", "Maps locations to sync; the list replaces the current one",
				textNode("place_id", "Maps place ID", assetSetMaxText)), assetSetMaxList)).
			with(func(n *node) { n.required = []string{"maps_locations"} }),
	}
	if output {
		locationSet = append(locationSet, objectNode("business_profile_location_set",
			"Location set from a Business Profile; its credential is never returned",
			textNode("email_address", "", maxNameArgument),
			textNode("business_account_id", "", maxNameArgument),
			textNode("business_name_filter", "", assetSetMaxText),
			listOf(textNode("label_filters", "", assetSetMaxText), maxListItems),
			listOf(wholeNode("listing_id_filters", "", 1, math64), maxListItems)))
	}
	members := []node{
		textNode("name", "Name of the asset set, 1 to 128 characters", assetSetNameMax).
			with(func(n *node) { n.check = assetSetNameCheck }),
		createOnly(enumNode("type", "Type of the asset set; fixed after creation", enumNamed("AssetSetTypeEnum.AssetSetType"))),
		createOnly(wholeNode("location_group_parent_asset_set_id", "ID of the sync level asset set whose locations "+
			"this location group draws from; required for location group types, fixed after creation", 1, math64)),
		objectNode("merchant_center_feed", "Merchant Center account and feed of a Merchant Center feed set",
			wholeNode("merchant_id", "Merchant Center ID", 1, math64),
			textNode("feed_label", "Feed label of the Merchant Center feed", assetSetMaxText)).
			with(func(n *node) { n.required = []string{"merchant_id"} }),
		objectNode("location_set", "Location data of a LOCATION_SYNC set", locationSet...).with(func(n *node) {
			n.required = []string{"location_ownership_type"}
			n.exclusive = []string{"chain_location_set", "maps_location_set"}
		}),
		objectNode("business_profile_location_group", "Filter of a Business Profile location group",
			objectNode("dynamic_business_profile_location_group_filter", "Filter of the locations of the group",
				listOf(textNode("label_filters", "Labels of which a location needs one; empty or left out means no filtering",
					assetSetMaxText), assetSetMaxList),
				assetSetBusinessNameFilter(),
				assetSetListingIDs("Listing IDs of which a location needs one; empty or left out means no filtering"))),
		objectNode("chain_location_group", "Chains of a chain location group",
			assetSetChains("dynamic_chain_location_group_filters", "Chains whose locations the group holds; the list replaces the current one")),
	}
	if output {
		members = append(members,
			textNode("resource_name", "", maxNameArgument),
			wholeNode("id", "", 1, math64),
			enumNode("status", "", nil),
			objectNode("hotel_property_data", "",
				wholeNode("hotel_center_id", "", 1, math64), textNode("partner_name", "", maxText)),
			objectNode("travel_feed_data", "",
				wholeNode("subset_id", "", 1, math64), enumNode("travel_feed_vertical_type", "", nil),
				wholeNode("hotel_center_account_id", "", 1, math64), wholeNode("merchant_center_id", "", 1, math64),
				wholeNode("partner_center_id", "", 1, math64)))
	}
	return members
}

// assetSetTree is the asset set a request can carry; assetSetOutputTree the one Google answers with.
var (
	assetSetTree = objectNode("", "", assetSetMembers(false)...).with(func(n *node) {
		n.exclusive = []string{"location_set", "business_profile_location_group", "chain_location_group"}
		n.required = []string{"name", "type"}
	})
	assetSetOutputTree = objectNode("", "", assetSetMembers(true)...)
)

func assetSetNameCheck(argument, value string) error {
	return labelsText(argument, value, 1, assetSetNameMax)
}

// ownedAssetSet reports whether a name is customers/ID/assetSets/N of exactly the account.
func ownedAssetSet(name, customerID string) bool {
	return ownedResource(name, customerID, assetSetsCollection)
}

var assetSetResourceNode = resourceNode("resource_name", "Asset set of this account, customers/ID/assetSets/N", assetSetsCollection)

type assetSetArguments struct {
	CustomerID          string   `json:"customer_id"`
	AssetSets           []any    `json:"asset_sets"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readAssetSetArguments(op string, raw json.RawMessage) (assetSetArguments, error) {
	var a assetSetArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

func assetSetCount(argument string, n int) error {
	if n == 0 || n > maxListOperations {
		return badRequest(argument + " must hold one to " + strconv.Itoa(maxListOperations) + " entries")
	}
	return nil
}

func assetSetCreateOperations(customerID string, items []any) ([]any, error) {
	if err := assetSetCount("asset_sets", len(items)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("asset_sets[%d]", i)}
		if !ok {
			return nil, badRequest(at.snake + " must be an object")
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(assetSetTree, in, at)
		if err != nil {
			return nil, err
		}
		ops = append(ops, map[string]any{"create": body})
	}
	return ops, nil
}

func assetSetUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	if err := assetSetCount("asset_sets", len(items)); err != nil {
		return nil, nil, err
	}
	tree := assetSetTree.mutable()
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("asset_sets[%d]", i)}
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		name, _ := in["resource_name"].(string)
		if !ownedAssetSet(name, customerID) {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/assetSets/N of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("an asset set appears in asset_sets only once")
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
		body, err := b.object(tree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

func assetSetRemoveOperations(customerID string, resourceNames []string) ([]any, error) {
	if err := assetSetCount("resource_names", len(resourceNames)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if !ownedAssetSet(name, customerID) {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/assetSets/N of the account of "+
				"customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("an asset set appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	assetSetResult struct {
		ResourceName string         `json:"resource_name"`
		AssetSet     map[string]any `json:"asset_set,omitempty"`
	}
	assetSetsChanged struct {
		CustomerID   string           `json:"customer_id"`
		ValidateOnly bool             `json:"validate_only"`
		Results      []assetSetResult `json:"results"`
	}
)

// assetSetsOf maps the answer. mutateList has bound every resource name to the account and, for an update and
// a remove, to the asset set asked for; the object of MUTABLE_RESOURCE must be that very asset set.
func assetSetsOf(op, customerID string, results []listResult, validateOnly bool) (*assetSetsChanged, error) {
	out := &assetSetsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []assetSetResult{}}
	for _, r := range results {
		item := assetSetResult{ResourceName: r.ResourceName}
		if len(r.Resource) > 0 {
			raw, ok := decodeObject(r.Resource)
			var members map[string]any
			if ok {
				members, ok = outputMembers(assetSetOutputTree.fields, raw, customerID)
			}
			if ok {
				name, _ := members["resource_name"].(string)
				id, hasID := members["id"].(int64)
				ok = (name == "" || name == r.ResourceName) && (!hasID || strconv.FormatInt(id, 10) == r.ID)
			}
			if !ok {
				return nil, unsureResult(op, "answered with an asset set that is not the one asked for")
			}
			item.AssetSet = members
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptors ---

const assetSetSensitivity = "asset sets of a Google Ads account: names, types, Merchant Center IDs and the filters " +
	"of location groups and chains"

var assetSetOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "asset_set": assetSetOutputTree.outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results")

var assetSetFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per asset set, in the order given, with its resource_name " +
		"(customers/ID/assetSets/N, always of the account of the call) and, with MUTABLE_RESOURCE, the asset set " +
		"after the change: name, type, status, id, merchant_center_feed, location_set (without the http_authorization_token " +
		"of a Business Profile), business_profile_location_group, chain_location_group, " +
		"location_group_parent_asset_set_id, hotel_property_data and travel_feed_data as Google returns them. " +
		"Empty after validate_only"},
}

var assetSetCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit " +
	"customer ID of the account without hyphens; a target of the connection or an account below a manager target. " +
	"A manager account needs a manager target that names it"}

const assetSetNote = " An asset set is a named group of assets (AssetSet resource of v26); this tool does not put " +
	"an asset into a set or attach a set to a campaign, an ad group or the account, and does not read sets " +
	"(read them with reports.search on asset_set). The request goes to AssetSetService.MutateAssetSets with up to " +
	"10 operations, atomic and without partial_failure: Google applies all of them or none. Resource names in " +
	"the arguments must belong to customer_id, and an ID that is negative, a temporary one, is refused. Not " +
	"offered: the members Google fills itself (answered, never sent), and the Business Profile location set " +
	"business_profile_location_set as an argument, whose credential does not pass through the tool, so a " +
	"location sync from a Business Profile cannot be set up here; its http_authorization_token is never answered."

var assetSetsCreate = capability.Descriptor{
	ID: assetSetsCreateID, Version: 1, Title: "Create asset sets in a Google Ads account",
	Description: "Create one to 10 asset sets in a Google Ads account (one create operation each): name (1 to 128 " +
		"characters) and type (one of the types of AssetSetType) are required, and the members for the type " +
		"follow: merchant_center_feed (merchant_id required) for a Merchant Center feed set, location_set " +
		"(location_ownership_type and one chain or Maps source) for a LOCATION_SYNC set, " +
		"location_group_parent_asset_set_id with a Business Profile or chain location group for a location group. " +
		"At most one of location_set, business_profile_location_group and chain_location_group. Which type takes " +
		"which member is Google's to validate; use validate_only to see its verdict. A Merchant Center feed " +
		"links data of a Merchant Center account to the account of the call." + assetSetNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "assets", "asset-sets", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, assetSetSensitivity),
	InputSchema: encoded(assetSetInputSchema(true)), OutputSchema: encoded(assetSetOutputSchema),
	Arguments: []capability.Argument{assetSetCustomerArgument,
		{Name: "asset_sets", Required: true, Description: "One to 10 asset sets, each an object of name and type (required) " +
			"and the optional members of the AssetSet resource, nested as the resource nests them"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: assetSetFields,
	Examples: []capability.Example{
		{Description: "Validate a page feed set without creating it", Arguments: json.RawMessage(
			`{"customer_id":"1234567890","asset_sets":[{"name":"Spring pages","type":"PAGE_FEED"}],"validate_only":true}`)},
		{Description: "Create a Merchant Center feed set", Arguments: json.RawMessage(
			`{"customer_id":"1234567890","asset_sets":[{"name":"Shop feed","type":"MERCHANT_CENTER_FEED",` +
				`"merchant_center_feed":{"merchant_id":123456,"feed_label":"DE"}}]}`)}},
}

var assetSetsUpdate = capability.Descriptor{
	ID: assetSetsUpdateID, Version: 1, Title: "Change asset sets of a Google Ads account",
	Description: "Change one to 10 asset sets of a Google Ads account (one update operation each, with a field mask " +
		"that names exactly the members given). Each entry names the set by resource_name and the members to " +
		"change, nested as the AssetSet resource nests them; a list member replaces the whole list. The type, " +
		"location_group_parent_asset_set_id, location_ownership_type and the relationship_type of a chain set are " +
		"fixed at creation and not offered." + assetSetNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "assets", "asset-sets", "update"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, assetSetSensitivity),
	InputSchema: encoded(assetSetInputSchema(false)), OutputSchema: encoded(assetSetOutputSchema),
	Arguments: []capability.Argument{assetSetCustomerArgument,
		{Name: "asset_sets", Required: true, Description: "One to 10 entries, each resource_name of an asset set of " +
			"customer_id once and at least one member to change"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: assetSetFields,
	Examples: []capability.Example{{Description: "Rename an asset set", Arguments: json.RawMessage(
		`{"customer_id":"1234567890","asset_sets":[{"resource_name":"customers/1234567890/assetSets/31","name":"Summer pages"}]}`)}},
}

var assetSetsRemove = capability.Descriptor{
	ID: assetSetsRemoveID, Version: 1, Title: "Remove asset sets from a Google Ads account",
	Description: "Remove one to 10 asset sets of a Google Ads account (one remove operation each). Google marks a " +
		"removed asset set REMOVED; no tool of this provider brings it back. Offers no response_content_type: a " +
		"removed asset set has no mutable resource to show." + assetSetNote + mutateListListedNote,
	Tags: []string{"google", "ads", "assets", "asset-sets", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, assetSetSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "uniqueItems": true,
			"items": assetSetResourceNode.inputSchema()},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(assetSetOutputSchema),
	Arguments: []capability.Argument{assetSetCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 asset sets of this account, each once, " +
			"customers/ID/assetSets/N as reports.search returns them"}, validateOnlyArgument},
	Fields: assetSetFields,
	Examples: []capability.Example{{Description: "Validate the removal of an asset set without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/assetSets/31"],"validate_only":true}`)}},
}

func assetSetInputSchema(create bool) schema {
	var item schema
	if create {
		item = assetSetTree.inputSchema()
	} else {
		item = assetSetTree.mutable().inputSchema()
		item["properties"].(schema)["resource_name"] = assetSetResourceNode.inputSchema()
		item["required"] = []string{"resource_name"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"asset_sets":    schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "items": item},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "asset_sets")
}

// --- handlers ---

func handleAssetSetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetSetsChange(ctx, resolved, secrets, red, raw, "create asset sets", assetSetsCreateID)
}

func handleAssetSetsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetSetsChange(ctx, resolved, secrets, red, raw, "update asset sets", assetSetsUpdateID)
}

func handleAssetSetsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetSetsChange(ctx, resolved, secrets, red, raw, "remove asset sets", assetSetsRemoveID)
}

func assetSetsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readAssetSetArguments(op, raw)
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
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	var ops []any
	var names []string
	switch tool {
	case assetSetsCreateID:
		ops, err = assetSetCreateOperations(a.CustomerID, a.AssetSets)
	case assetSetsUpdateID:
		ops, names, err = assetSetUpdateOperations(a.CustomerID, a.AssetSets)
	default:
		ops, err = assetSetRemoveOperations(a.CustomerID, a.ResourceNames)
		names = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: a.CustomerID,
		service: assetSetsService, collection: assetSetsCollection, resourceKey: "assetSet", operations: ops,
		names: names, validateOnly: a.ValidateOnly, content: a.ResponseContentType})
	if err != nil {
		return nil, err
	}
	return assetSetsOf(op, a.CustomerID, results, a.ValidateOnly)
}

// --- confirmation ---

// assetSetSummary phrases the change for the confirmation, from the arguments alone.
func assetSetSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readAssetSetArguments("", raw)
		verb := map[string]string{assetSetsCreateID: "Create", assetSetsUpdateID: "Change", assetSetsRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		var parts []string
		for _, name := range a.ResourceNames[:min(len(a.ResourceNames), 10)] {
			parts = append(parts, boundedText(name, maxCodeText))
		}
		for _, item := range a.AssetSets[:min(len(a.AssetSets), 10)] {
			in, _ := item.(map[string]any)
			part := strconv.Quote(boundedText(stringOf(in["name"]), assetSetNameMax*4))
			if name := stringOf(in["resource_name"]); name != "" {
				part = boundedText(name, maxCodeText) + " " + part
			}
			var facts []string
			if t := stringOf(in["type"]); t != "" {
				facts = append(facts, "type "+boundedText(t, maxCodeText))
			}
			var keys []string
			for key := range in {
				if key != "name" && key != "type" && key != "resource_name" {
					keys = append(keys, key)
				}
			}
			slices.Sort(keys)
			if len(keys) > 0 {
				facts = append(facts, "members "+strings.Join(keys, "+"))
			}
			if len(facts) > 0 {
				part += " (" + strings.Join(facts, ", ") + ")"
			}
			parts = append(parts, part)
		}
		total := len(a.AssetSets) + len(a.ResourceNames)
		more := ""
		if total > len(parts) {
			more = " and " + strconv.Itoa(total-len(parts)) + " more"
		}
		summary := verb + " " + strconv.Itoa(total) + " asset set(s) of Google Ads account " + a.CustomerID + ": " +
			strings.Join(parts, "; ") + more + "."
		if tool == assetSetsRemoveID {
			summary += " Google marks them removed; no tool brings them back."
		}
		return summary
	}
}
