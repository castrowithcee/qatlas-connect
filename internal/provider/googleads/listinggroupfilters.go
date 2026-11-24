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
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The listing group filters of an asset group form the tree that decides which products or pages a Performance
// Max asset group advertises (AssetGroupListingGroupFilterService.MutateAssetGroupListingGroupFilters).
// All three tools are offered by the tools list only, as the asset group tools are: a change moves what serves
// and spends.

const (
	listingGroupFiltersCreateID = Provider + ".asset-group-listing-group-filters.create"
	listingGroupFiltersUpdateID = Provider + ".asset-group-listing-group-filters.update"
	listingGroupFiltersRemoveID = Provider + ".asset-group-listing-group-filters.remove"

	listingGroupFiltersService    = "/assetGroupListingGroupFilters"
	listingGroupFiltersCollection = "assetGroupListingGroupFilters"

	// maxListingGroupFilters is Qatlas's own bound per call; the reference of v26 states none. A tree is sent
	// whole, so the bound is higher than for most list tools.
	maxListingGroupFilters = 100
	// maxFilterValue bounds the text of a dimension (brand, item ID, product type, custom attribute, URL
	// part); the reference states no length.
	maxFilterValue       = 1000
	maxWebpageConditions = 20
	// minTemporaryID is Qatlas's own lower bound of a temporary ID; Google asks for a negative, unique number.
	minTemporaryID = -2147483648
)

// The members of the AssetGroupListingGroupFilter resource of v26 that the tools offer. Not offered: id and
// path, which Google fills (output only), and nothing is deprecated. asset_group, type, listing_source and the
// parent are immutable in the reference, so an update offers case_value only. The parent and the temporary ID
// are no members of the tree: they are checked against the request as a whole.

const filterSensitivity = "listing group filters of a Performance Max asset group: which products or pages it advertises"

var (
	filterNameSchema = schema{"type": "string", "maxLength": 100,
		"pattern":     `^customers/[0-9]{1,10}/assetGroupListingGroupFilters/[0-9]{1,19}~[0-9]{1,19}$`,
		"description": "Listing group filter of this account, customers/ID/assetGroupListingGroupFilters/ASSET_GROUP_ID~FILTER_ID"}
	filterParentSchema = schema{"type": "string", "maxLength": 100,
		"pattern": `^customers/[0-9]{1,10}/assetGroupListingGroupFilters/[0-9]{1,19}~-?[0-9]{1,19}$`,
		"description": "The subdivision this node refines, customers/ID/assetGroupListingGroupFilters/ASSET_GROUP_ID~FILTER_ID " +
			"of this account. A negative FILTER_ID must be the resource name that another entry of the same request " +
			"gets through temp_id. Left out for the root node"}
	filterTempIDSchema = schema{"type": "integer", "minimum": minTemporaryID, "maximum": -1,
		"description": "Temporary ID of this node: a negative number, unique in the request. The node is created under " +
			"customers/ID/assetGroupListingGroupFilters/ASSET_GROUP_ID~TEMP_ID, so that parent_listing_group_filter of " +
			"other entries of the same request can name it; Google replaces it with the real ID"}
)

// filterParts splits customers/ID/assetGroupListingGroupFilters/G~F of exactly this account into the asset group
// ID and the filter ID. The filter ID is positive, or negative (temporary) when temporary is true.
func filterParts(name, customerID string, temporary bool) (group, id string, ok bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+listingGroupFiltersCollection+"/")
	if !ok {
		return "", "", false
	}
	group, id, ok = strings.Cut(rest, "~")
	if !ok || !numericIDPattern.MatchString(group) {
		return "", "", false
	}
	if temporary && strings.HasPrefix(id, "-") {
		return group, id, numericIDPattern.MatchString(id[1:])
	}
	return group, id, numericIDPattern.MatchString(id)
}

// filterDimensionMembers are the dimensions of ListingGroupFilterDimension. Each is empty to say "everything
// else" of its subdivision, which the Google guide builds the same way.
func filterDimensionMembers() []node {
	value := func(desc string) node { return textNode("value", desc, maxFilterValue) }
	open := func(n node) node { n.emptyOK = true; return n }
	level := func(enum string) node {
		return enumNode("level", "Level of the taxonomy, from LEVEL1 at the top; all cases of one subdivision use the same level", enumNamed(enum))
	}
	return []node{
		open(objectNode("product_brand", "Brand of a product offer; empty is every other brand", value("The brand"))),
		open(objectNode("product_category", "Category of a product offer, up to five levels; empty is every other category",
			wholeNode("category_id", "ID of the google_product_category of Merchant Center", 0, math64),
			level("ListingGroupFilterProductCategoryLevelEnum.ListingGroupFilterProductCategoryLevel"))),
		open(objectNode("product_channel", "Locality of a product offer; empty is every other locality",
			enumNode("channel", "LOCAL or ONLINE", enumNamed("ListingGroupFilterProductChannelEnum.ListingGroupFilterProductChannel")))),
		open(objectNode("product_condition", "Condition of a product offer; empty is every other condition",
			enumNode("condition", "NEW, REFURBISHED or USED", enumNamed("ListingGroupFilterProductConditionEnum.ListingGroupFilterProductCondition")))),
		open(objectNode("product_custom_attribute", "Custom attribute of a product offer; empty is every other value",
			enumNode("index", "Which of the five custom attributes, INDEX0 to INDEX4",
				enumNamed("ListingGroupFilterCustomAttributeIndexEnum.ListingGroupFilterCustomAttributeIndex")),
			value("The value of the attribute"))),
		open(objectNode("product_item_id", "Item ID of a product offer; empty is every other item", value("The item ID"))),
		open(objectNode("product_type", "Product type of a product offer; empty is every other type",
			level("ListingGroupFilterProductTypeLevelEnum.ListingGroupFilterProductTypeLevel"), value("The product type"))),
		open(objectNode("retail_filter_bundle", "Retail product tags: the retail filter bundle shared set",
			resourceNode("shared_set", "Retail filter bundle shared set of this account, customers/ID/sharedSets/N", "sharedSets"))),
		open(objectNode("webpage", "URLs of a page feed and of the advertiser domain. Several root nodes with this dimension "+
			"are combined with OR; the conditions of one node are case-sensitive and combined with AND, and must be of one type",
			listOf(oneOf(objectNode("conditions", "One condition, exactly one of its members",
				textNode("custom_label", "Page feed URLs with this label, from an asset set of type PAGE_FEED linked to the campaign", maxFilterValue),
				textNode("url_contains", "Page feed URLs and URLs of the advertiser domain that contain this text", maxFilterValue)),
				"custom_label", "url_contains"), maxWebpageConditions))),
	}
}

func filterDimensionNames() []string {
	var names []string
	for _, n := range filterDimensionMembers() {
		names = append(names, n.name)
	}
	return names
}

// filterTree holds the members of a listing group filter that a tool offers.
var filterTree = objectNode("listing_group_filter", "",
	createOnly(resourceNode("asset_group", "Asset group of this account the node belongs to, customers/ID/assetGroups/N; fixed after creation", "assetGroups")),
	createOnly(enumNode("type", "SUBDIVISION has children and is the root; UNIT_INCLUDED serves the listings of the node, UNIT_EXCLUDED does not; fixed after creation",
		enumNamed("ListingGroupFilterTypeEnum.ListingGroupFilterType"))),
	createOnly(enumNode("listing_source", "Source of the listings, SHOPPING, RETAIL or WEBPAGE; fixed after creation",
		enumNamed("ListingGroupFilterListingSourceEnum.ListingGroupFilterListingSource"))),
	oneOf(objectNode("case_value", "The dimension this node uses to refine its parent, exactly one of the dimensions; left out for the root node. "+
		"All siblings use the same dimension", filterDimensionMembers()...), filterDimensionNames()...)).
	with(func(n *node) { n.required = []string{"asset_group"} })

// filterOutputTree is the node as Google answers it with MUTABLE_RESOURCE. The resource name and the parent are
// checked and added by hand, because they carry a composite key.
var filterOutputTree = objectNode("listing_group_filter", "",
	resourceNode("asset_group", "", "assetGroups"), wholeNode("id", "", 0, math64),
	enumNode("type", "", enumNamed("ListingGroupFilterTypeEnum.ListingGroupFilterType")),
	enumNode("listing_source", "", enumNamed("ListingGroupFilterListingSourceEnum.ListingGroupFilterListingSource")),
	objectNode("case_value", "", filterDimensionMembers()...),
	objectNode("path", "", listOf(objectNode("dimensions", "", filterDimensionMembers()...), maxListItems)))

type listingGroupFilterArguments struct {
	CustomerID          string   `json:"customer_id"`
	Filters             []any    `json:"listing_group_filters"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readListingGroupFilterArguments(op string, raw json.RawMessage) (listingGroupFilterArguments, error) {
	var a listingGroupFilterArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

func listingGroupFilterItems(items []any) ([]map[string]any, error) {
	if len(items) == 0 || len(items) > maxListingGroupFilters {
		return nil, badRequest("listing_group_filters must hold one to " + strconv.Itoa(maxListingGroupFilters) + " nodes")
	}
	out := make([]map[string]any, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		if !ok {
			return nil, badRequest(fmt.Sprintf("listing_group_filters[%d] must be an object", i))
		}
		out[i] = in
	}
	return out, nil
}

// listingGroupFilterCreateOperations builds the creations. A temporary ID is allowed exactly for the nodes this
// request creates: each temp_id becomes the resource name of its node, and a negative parent must be one of them.
// groups holds the asset group ID of each entry, to bind the answer to it.
func listingGroupFilterCreateOperations(customerID string, items []any) (ops []any, groups []string, err error) {
	entries, err := listingGroupFilterItems(items)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, len(entries))
	groups = make([]string, len(entries))
	for i, in := range entries {
		at := fmt.Sprintf("listing_group_filters[%d]", i)
		group, ok := numericIDOf(stringOf(in["asset_group"]), customerID, "assetGroups")
		if !ok {
			return nil, nil, badRequest(at + ".asset_group must be customers/" + queryNumber(customerID) +
				"/assetGroups/N of the account of customer_id")
		}
		groups[i] = group
		raw, has := in["temp_id"]
		if !has {
			continue
		}
		number, isNumber := raw.(json.Number)
		temp, parseErr := strconv.ParseInt(string(number), 10, 64)
		if !isNumber || parseErr != nil || temp >= 0 || temp < minTemporaryID {
			return nil, nil, badRequest(at + ".temp_id must be a negative whole number of at least " + strconv.Itoa(minTemporaryID))
		}
		names[i] = customerName(customerID) + "/" + listingGroupFiltersCollection + "/" + group + "~" + strconv.FormatInt(temp, 10)
		if slices.Contains(names[:i], names[i]) {
			return nil, nil, badRequest("a temp_id appears once per asset group in listing_group_filters")
		}
	}
	ops = make([]any, 0, len(entries))
	for i, in := range entries {
		at := pathOfMember{snake: fmt.Sprintf("listing_group_filters[%d]", i)}
		rest := map[string]any{}
		for key, value := range in {
			if key != "temp_id" && key != "parent_listing_group_filter" {
				rest[key] = value
			}
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(filterTree, rest, at)
		if err != nil {
			return nil, nil, err
		}
		if names[i] != "" {
			body["resourceName"] = names[i]
		}
		if raw, has := in["parent_listing_group_filter"]; has {
			parent, _ := raw.(string)
			_, id, ok := filterParts(parent, customerID, true)
			if ok && strings.HasPrefix(id, "-") {
				ok = slices.Contains(names, parent)
			}
			if !ok {
				return nil, nil, badRequest(at.snake + ".parent_listing_group_filter must be a filter of the account of customer_id; " +
					"a negative ID only when another entry of this request creates it with temp_id")
			}
			body["parentListingGroupFilter"] = parent
		}
		ops = append(ops, map[string]any{"create": body})
	}
	return ops, groups, nil
}

// listingGroupFilterUpdateOperations builds one update with a field mask per node. A node is named once.
func listingGroupFilterUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	entries, err := listingGroupFilterItems(items)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(entries))
	names := make([]string, 0, len(entries))
	for i, in := range entries {
		at := pathOfMember{snake: fmt.Sprintf("listing_group_filters[%d]", i)}
		name, _ := in["resource_name"].(string)
		if _, _, ok := filterParts(name, customerID, false); !ok {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/assetGroupListingGroupFilters/ASSET_GROUP_ID~FILTER_ID of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("a listing group filter appears in listing_group_filters only once")
		}
		fields := map[string]any{}
		for key, value := range in {
			if key != "resource_name" {
				fields[key] = value
			}
		}
		if len(fields) == 0 {
			return nil, nil, badRequest(at.snake + " must give case_value, the member that can change")
		}
		b := builder{customerID: customerID}
		body, err := b.object(filterTree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")})
	}
	return ops, names, nil
}

// listingGroupFilterRemoveOperations keeps the order given: Google removes the children before the parent only
// when the list is in that order.
func listingGroupFilterRemoveOperations(customerID string, resourceNames []string) ([]any, error) {
	if len(resourceNames) == 0 || len(resourceNames) > maxListingGroupFilters {
		return nil, badRequest("resource_names must hold one to " + strconv.Itoa(maxListingGroupFilters) + " nodes")
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if _, _, ok := filterParts(name, customerID, false); !ok {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/assetGroupListingGroupFilters/"+
				"ASSET_GROUP_ID~FILTER_ID of the account of customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("a listing group filter appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	listingGroupFilterResult struct {
		ResourceName       string         `json:"resource_name"`
		ListingGroupFilter map[string]any `json:"listing_group_filter,omitempty"`
	}
	listingGroupFiltersChanged struct {
		CustomerID   string                     `json:"customer_id"`
		ValidateOnly bool                       `json:"validate_only"`
		Results      []listingGroupFilterResult `json:"results"`
	}
)

// listingGroupFilterOf maps the node of a result. It must be the one of the result, in the account of the call, and
// its parent and asset group must belong to that account and to the asset group of the result.
func listingGroupFilterOf(raw json.RawMessage, r listResult, customerID string) (map[string]any, bool) {
	in, ok := decodeObject(raw)
	if !ok {
		return nil, false
	}
	out, ok := outputMembers(filterOutputTree.fields, in, customerID)
	if !ok {
		return nil, false
	}
	group, id, _ := filterParts(r.ResourceName, customerID, false)
	if name := stringOf(in["resourceName"]); name != "" && name != r.ResourceName {
		return nil, false
	}
	if g, has := out["asset_group"]; has && g != customerName(customerID)+"/assetGroups/"+group {
		return nil, false
	}
	if v, has := out["id"]; has && strconv.FormatInt(v.(int64), 10) != id {
		return nil, false
	}
	if parent, has := in["parentListingGroupFilter"]; has && parent != nil {
		p, _ := parent.(string)
		pg, _, ok := filterParts(p, customerID, false)
		if !ok || pg != group {
			return nil, false
		}
		out["parent_listing_group_filter"] = p
	}
	out["resource_name"] = r.ResourceName
	return out, true
}

// --- descriptors ---

func listingGroupFilterInputSchema(create bool) schema {
	var item schema
	if create {
		item = filterTree.inputSchema()
		item["properties"].(schema)["parent_listing_group_filter"] = filterParentSchema
		item["properties"].(schema)["temp_id"] = filterTempIDSchema
		item["required"] = []string{"asset_group"}
		delete(item, "description")
	} else {
		item = filterTree.mutable().inputSchema()
		item["properties"].(schema)["resource_name"] = filterNameSchema
		item["required"] = []string{"resource_name", "case_value"}
	}
	props := schema{"customer_id": customerIDArgument,
		"listing_group_filters": schema{"type": "array", "minItems": 1, "maxItems": maxListingGroupFilters, "items": item},
		"validate_only":         validateOnlySchema, "response_content_type": responseContentTypeSchema}
	return object(props, "customer_id", "listing_group_filters")
}

var listingGroupFilterOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "listing_group_filter": func() schema {
		s := filterOutputTree.outputSchema()
		props := s["properties"].(schema)
		props["resource_name"], props["parent_listing_group_filter"] = stringSchema, stringSchema
		return s
	}()}, "resource_name"))}, "customer_id", "validate_only", "results")

var listingGroupFilterFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per node, in the order given, with its resource_name (the real ID, " +
		"not a temporary one); empty after validate_only. With response_content_type MUTABLE_RESOURCE also " +
		"listing_group_filter: resource_name, asset_group, id, type, listing_source, case_value, " +
		"parent_listing_group_filter and path, as far as Google returns them"}}

var listingGroupFilterCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit " +
	"customer ID of the account without hyphens; a target of the connection or an account below a manager target"}

const listingGroupFilterNote = " The listing group tree decides which products or pages the asset group advertises: " +
	"a node of type UNIT_INCLUDED serves within the budget of the Performance Max campaign, UNIT_EXCLUDED does " +
	"not, and the call is confirmed per call. Resource names in the arguments must belong to customer_id. Up to " +
	"100 nodes per call, one atomic request without partial failure, because Google validates the tree as a " +
	"whole."

var listingGroupFiltersCreate = capability.Descriptor{
	ID: listingGroupFiltersCreateID, Version: 1, Title: "Create Google Ads listing group filters",
	Description: "Create up to 100 listing group filter nodes of Performance Max asset groups in a Google Ads account " +
		"(AssetGroupListingGroupFilterService.MutateAssetGroupListingGroupFilters with create operations): Google " +
		"applies all of them or none. A whole tree is created in one call: give each node that others refine a " +
		"negative temp_id and name it as parent_listing_group_filter of its children, parents before children " +
		"as Google's own sample orders them; the root has no parent and no case_value and is a SUBDIVISION. Each " +
		"node needs asset_group; type, listing_source and case_value are as the resource of v26 has them, and " +
		"Google decides whether the tree is valid (one root, siblings of one dimension, an empty case for " +
		"everything else)." + listingGroupFilterNote + mutateListListedNote,
	Tags: []string{"google", "ads", "asset-group-listing-group-filters", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, filterSensitivity),
	InputSchema: encoded(listingGroupFilterInputSchema(true)), OutputSchema: encoded(listingGroupFilterOutputSchema),
	Arguments: []capability.Argument{listingGroupFilterCustomerArgument,
		{Name: "listing_group_filters", Required: true, Description: "One to 100 nodes, each an object with asset_group " +
			"(required), type, listing_source, case_value, parent_listing_group_filter and temp_id"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: listingGroupFilterFields,
	Examples: []capability.Example{{Description: "Validate a tree: a root that splits by condition into new products and everything else",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","listing_group_filters":[{"asset_group":"customers/1234567890/assetGroups/31",` +
			`"temp_id":-1,"type":"SUBDIVISION","listing_source":"SHOPPING"},{"asset_group":"customers/1234567890/assetGroups/31",` +
			`"type":"UNIT_INCLUDED","listing_source":"SHOPPING","parent_listing_group_filter":` +
			`"customers/1234567890/assetGroupListingGroupFilters/31~-1","case_value":{"product_condition":{"condition":"NEW"}}},` +
			`{"asset_group":"customers/1234567890/assetGroups/31","type":"UNIT_EXCLUDED","listing_source":"SHOPPING",` +
			`"parent_listing_group_filter":"customers/1234567890/assetGroupListingGroupFilters/31~-1",` +
			`"case_value":{"product_condition":{}}}],"validate_only":true}`)}},
}

var listingGroupFiltersUpdate = capability.Descriptor{
	ID: listingGroupFiltersUpdateID, Version: 1, Title: "Change Google Ads listing group filters",
	Description: "Change the case_value of up to 100 listing group filter nodes of a Google Ads account " +
		"(AssetGroupListingGroupFilterService.MutateAssetGroupListingGroupFilters with update operations and a " +
		"field mask that names exactly the members given): Google applies all of them or none. Each entry names " +
		"the node by resource_name. The type, listing source, asset group, and parent are immutable in the " +
		"reference, so they are not offered: to change them remove the node and create it again. A temporary ID " +
		"is not accepted." + listingGroupFilterNote + mutateListListedNote,
	Tags: []string{"google", "ads", "asset-group-listing-group-filters", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, filterSensitivity),
	InputSchema: encoded(listingGroupFilterInputSchema(false)), OutputSchema: encoded(listingGroupFilterOutputSchema),
	Arguments: []capability.Argument{listingGroupFilterCustomerArgument,
		{Name: "listing_group_filters", Required: true, Description: "One to 100 entries: resource_name of the node and its " +
			"new case_value"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: listingGroupFilterFields,
	Examples: []capability.Example{{Description: "Change the brand a node selects", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
		`"listing_group_filters":[{"resource_name":"customers/1234567890/assetGroupListingGroupFilters/31~41",` +
		`"case_value":{"product_brand":{"value":"Acme"}}}]}`)}},
}

var listingGroupFiltersRemove = capability.Descriptor{
	ID: listingGroupFiltersRemoveID, Version: 1, Title: "Remove Google Ads listing group filters",
	Description: "Remove up to 100 listing group filter nodes of a Google Ads account " +
		"(AssetGroupListingGroupFilterService.MutateAssetGroupListingGroupFilters with remove operations): Google " +
		"applies all of them or none. A node can be removed only when no other node has it as its parent, and " +
		"Google needs the order: list the children before their parent, and Qatlas keeps the order given. " +
		"Resource names must be nodes of customer_id." + listingGroupFilterNote + mutateListListedNote,
	Tags: []string{"google", "ads", "asset-group-listing-group-filters", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, filterSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxListingGroupFilters, "items": filterNameSchema},
		"validate_only":  validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(listingGroupFilterOutputSchema),
	Arguments: []capability.Argument{listingGroupFilterCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 100 nodes of this account, children before their " +
			"parent, customers/ID/assetGroupListingGroupFilters/ASSET_GROUP_ID~FILTER_ID"}, validateOnlyArgument},
	Fields: listingGroupFilterFields,
	Examples: []capability.Example{{Description: "Validate the removal of a leaf and then its parent without removing them",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/assetGroupListingGroupFilters/31~42",` +
			`"customers/1234567890/assetGroupListingGroupFilters/31~41"],"validate_only":true}`)}},
}

// --- handlers ---

func handleListingGroupFiltersCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return listingGroupFiltersChange(ctx, resolved, secrets, red, raw, "create listing group filters", listingGroupFiltersCreateID)
}

func handleListingGroupFiltersUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return listingGroupFiltersChange(ctx, resolved, secrets, red, raw, "update listing group filters", listingGroupFiltersUpdateID)
}

func handleListingGroupFiltersRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return listingGroupFiltersChange(ctx, resolved, secrets, red, raw, "remove listing group filters", listingGroupFiltersRemoveID)
}

func listingGroupFiltersChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readListingGroupFilterArguments(op, raw)
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
	var expected, groups []string
	switch tool {
	case listingGroupFiltersCreateID:
		ops, groups, err = listingGroupFilterCreateOperations(a.CustomerID, a.Filters)
	case listingGroupFiltersUpdateID:
		ops, expected, err = listingGroupFilterUpdateOperations(a.CustomerID, a.Filters)
	default:
		ops, err = listingGroupFilterRemoveOperations(a.CustomerID, a.ResourceNames)
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
		service: listingGroupFiltersService, collection: listingGroupFiltersCollection, resourceKey: "assetGroupListingGroupFilter",
		operations: ops, names: expected, validateOnly: a.ValidateOnly, content: a.ResponseContentType,
		idOf: func(name string) (string, bool) {
			_, id, ok := filterParts(name, a.CustomerID, false)
			return id, ok
		}})
	if err != nil {
		return nil, err
	}
	out := listingGroupFiltersChanged{CustomerID: a.CustomerID, ValidateOnly: a.ValidateOnly, Results: []listingGroupFilterResult{}}
	for i, r := range results {
		if groups != nil {
			if group, _, _ := filterParts(r.ResourceName, a.CustomerID, false); group != groups[i] {
				return nil, unsureResult(op, "answered with a result for another asset group than the one asked for")
			}
		}
		item := listingGroupFilterResult{ResourceName: r.ResourceName}
		if len(r.Resource) > 0 {
			mapped, ok := listingGroupFilterOf(r.Resource, r, a.CustomerID)
			if !ok {
				return nil, unsure(provider.InvalidResponse(op, subject+" answered with a node that is not the one of its result"), true)
			}
			item.ListingGroupFilter = mapped
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- confirmation ---

// listingGroupFilterSummary phrases the change for the confirmation, from the arguments alone.
func listingGroupFilterSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readListingGroupFilterArguments("", raw)
		verb := map[string]string{listingGroupFiltersCreateID: "Create", listingGroupFiltersUpdateID: "Change", listingGroupFiltersRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		parts := slices.Clone(a.ResourceNames)
		for _, item := range a.Filters {
			in, _ := item.(map[string]any)
			part := stringOf(in["resource_name"])
			var facts []string
			for _, key := range []string{"type", "listing_source", "asset_group"} {
				if v := stringOf(in[key]); v != "" {
					facts = append(facts, key+" "+v)
				}
			}
			if in["case_value"] != nil {
				var kinds []string
				if cv, ok := in["case_value"].(map[string]any); ok {
					for key := range cv {
						kinds = append(kinds, key)
					}
				}
				slices.Sort(kinds)
				facts = append(facts, "dimension "+strings.Join(kinds, "+"))
			}
			parts = append(parts, strings.TrimSpace(part+" ("+strings.Join(facts, ", ")+")"))
		}
		summary := verb + " " + strconv.Itoa(len(parts)) + " listing group filter node(s) of Google Ads account " + a.CustomerID +
			": " + strings.Join(parts, "; ") + "."
		return summary + " The tree decides which products or pages the asset group advertises within the budget of its Performance Max campaign."
	}
}
