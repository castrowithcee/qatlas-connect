package googleads

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	customerNegativeCriteriaCreateID = Provider + ".customer-negative-criteria.create"
	customerNegativeCriteriaRemoveID = Provider + ".customer-negative-criteria.remove"
	sharedCriteriaCreateID           = Provider + ".shared-criteria.create"
	sharedCriteriaRemoveID           = Provider + ".shared-criteria.remove"

	customerNegativeCriteriaService    = "/customerNegativeCriteria"
	customerNegativeCriteriaCollection = "customerNegativeCriteria"
	sharedCriteriaService              = "/sharedCriteria"
	sharedCriteriaCollection           = "sharedCriteria"

	// maxExclusions is Qatlas's own bound per call; Google documents none for these services.
	maxExclusions = 50
)

const exclusionsSensitivity = "exclusions of a whole Google Ads account or of the shared sets its campaigns use: " +
	"keywords, placements, apps, channels, IP blocks; no data of other people"

// --- criterion types ---

// exclusionTypesNamed takes types of the criteria of a campaign by name. The enums of a type are the same
// enums of Google for every criterion resource, so the campaign prefix serves all of them.
func exclusionTypesNamed(names ...string) []criterionType {
	all := baseCriterionTypes("campaign_criterion")
	out := make([]criterionType, 0, len(names))
	for _, name := range names {
		t, ok := typeNamed(all, name)
		if !ok {
			panic("googleads: no criterion type " + name)
		}
		out = append(out, t)
	}
	return out
}

func sharedSetListType(name, desc string) criterionType {
	return criterionType{node: typeNode(name, desc,
		resourceNode("shared_set", "Shared set of this account, customers/ID/sharedSets/N", "sharedSets")),
		required: []string{"shared_set"}}
}

var (
	customerNegativeCriterionTypes = func() []criterionType {
		types := exclusionTypesNamed("content_label", "ip_block", "mobile_app_category", "mobile_application",
			"placement", "youtube_channel", "youtube_video")
		for i := range types {
			if types[i].node.name == "ip_block" {
				types[i].node.desc = "Address or block of IP addresses to exclude; Google allows up to 500 per account"
			}
		}
		return append(types,
			sharedSetListType("negative_keyword_list", "Shared set of negative keywords that applies to the whole account"),
			sharedSetListType("placement_list", "Shared set of negative placements that applies to the whole account"))
	}()

	sharedCriterionTypes = append(exclusionTypesNamed("keyword", "mobile_app_category", "mobile_application", "placement",
		"webpage", "youtube_channel", "youtube_video"), brandType(), retailFilterType(), verticalAdsItemGroupRuleType())
)

func brandType() criterionType {
	return criterionType{node: typeNode("brand", "Brand of a brand list",
		textNode("entity_id", "ID of the brand in the Commercial Knowledge Graph (MID)", 255)), required: []string{"entity_id"}}
}

const maxRetailTagValue = 50

// checkRetailTagValue holds a tag to what Google states: up to 50 characters, none of them a comma or a newline.
// Control characters are refused as well.
func checkRetailTagValue(argument, value string) error {
	if value == "" || utf8.RuneCountInString(value) > maxRetailTagValue || strings.ContainsRune(value, ',') || hasControl(value) {
		return badRequest(argument + " must be 1 to 50 characters without a comma, a line break or control characters")
	}
	return nil
}

const retailNamePattern = `^[a-z0-9_]{1,20}$`

func retailFilterType() criterionType {
	return criterionType{node: oneOf(typeNode("retail_filter", "Node of a retail filter; give exactly one of expression and tag",
		objectNode("expression", "Expression that groups tags with OR",
			patternNode("name", "Name of 1 to 20 characters: lowercase latin letters, digits and underscores; unique in the shared set",
				retailNamePattern, 20)),
		objectNode("tag", "Tag of the filter",
			patternNode("expression_name", "Name of the expression this tag belongs to; left out for a top-level condition",
				retailNamePattern, 20),
			node{name: "value", desc: "Value of the tag, up to 50 characters, no comma or line break", kind: kindText,
				maxLen: maxRetailTagValue * 4, check: checkRetailTagValue})), "expression", "tag"),
		rules: func(in map[string]any) error {
			expression, tag := in["expression"], in["tag"]
			if (expression == nil) == (tag == nil) {
				return badRequest("retail_filter needs exactly one of expression and tag")
			}
			if object, ok := expression.(map[string]any); ok {
				return requireMembers(object, "retail_filter.expression", "name")
			}
			if object, ok := tag.(map[string]any); ok {
				return requireMembers(object, "retail_filter.tag", "value")
			}
			return nil
		}}
}

var verticalDimensions = []string{"city_criterion_id", "country_criterion_id", "region_criterion_id",
	"event_participant_display_name", "hotel_class", "item_code", "user_rating", "venue"}

func verticalAdsItemGroupRuleType() criterionType {
	geo := func(name, desc string) node { return constantNode(name, desc, "geoTargetConstants") }
	return criterionType{node: oneOf(typeNode("vertical_ads_item_group_rule", "Single item group rule of Vertical Ads; give exactly one member",
		geo("city_criterion_id", "City, a constant of Google, geoTargetConstants/N"),
		geo("country_criterion_id", "Country, a constant of Google, geoTargetConstants/N"),
		textNode("event_participant_display_name", "Name of a participant of an event, such as the headliner of a concert", 255),
		wholeNode("hotel_class", "Class of a hotel from 1 to 5 stars", 1, 5),
		textNode("item_code", "ID of one Vertical Ads listing", 255),
		geo("region_criterion_id", "Region, a constant of Google, geoTargetConstants/N"),
		wholeNode("user_rating", "User rating of a listing from 1 to 5, 5 is best", 1, 5),
		textNode("venue", "Name of a location, such as the venue of a concert", 255)), verticalDimensions...),
		rules: func(in map[string]any) error {
			if len(in) != 1 {
				return badRequest("vertical_ads_item_group_rule needs exactly one member")
			}
			return nil
		}}
}

// --- trees ---

func exclusionTree(name string, extra []node, types []criterionType) node {
	members := slices.Clone(extra)
	for _, t := range types {
		members = append(members, t.node)
	}
	return oneOf(objectNode(name, "", members...), typeNames(types)...)
}

// withOutputMembers is the tree of an object Google answers with: it adds the members that only Google sets,
// to a type or to the object itself.
func withOutputMembers(tree node, typeName string, extra ...node) node {
	tree.fields = slices.Clone(tree.fields)
	if typeName == "" {
		tree.fields = append(tree.fields, extra...)
		return tree
	}
	for i, f := range tree.fields {
		if f.name == typeName {
			f.fields = append(slices.Clone(f.fields), extra...)
			tree.fields[i] = f
		}
	}
	return tree
}

var (
	customerNegativeCriterionTree = exclusionTree("customer_negative_criterion", nil, customerNegativeCriterionTypes)

	customerNegativeCriterionOutputTree = withOutputMembers(customerNegativeCriterionTree, "",
		textNode("resource_name", "", maxNameArgument), wholeNode("id", "", 0, math64), enumNode("type", "", nil))

	sharedCriterionTree = exclusionTree("shared_criterion", []node{
		createOnly(resourceNode("shared_set", "Shared set of this account that holds the criterion, customers/ID/sharedSets/N", "sharedSets")),
		createOnly(flagNode("negative", "true excludes what the criterion names; false or left out targets it")),
	}, sharedCriterionTypes)

	sharedCriterionOutputTree = withOutputMembers(withOutputMembers(withOutputMembers(sharedCriterionTree, "webpage",
		node{name: "coverage_percentage", kind: kindNumber},
		objectNode("sample", "", listOf(textNode("sample_urls", "", maxURLArgument), maxListItems))), "brand",
		textNode("display_name", "", maxNameArgument), textNode("primary_url", "", maxURLArgument),
		enumNode("rejection_reason", "", nil), enumNode("status", "", nil)), "",
		textNode("resource_name", "", maxNameArgument), wholeNode("criterion_id", "", 0, math64), enumNode("type", "", nil))
)

// --- operations ---

func exclusionCount(argument string, n int) error {
	if n < 1 || n > maxExclusions {
		return badRequest(fmt.Sprintf("%s must hold one to %d entries", argument, maxExclusions))
	}
	return nil
}

// exclusionCreateOperations checks every criterion to create and builds its operation. sets are the shared sets
// named, one per operation, for the check of the answer; nil without a shared set.
func exclusionCreateOperations(customerID string, items []any, tree node, types []criterionType,
	withSet bool) ([]any, []string, error) {
	if err := exclusionCount("criteria", len(items)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(items))
	var sets []string
	for i, item := range items {
		at := pathOfMember{snake: fmt.Sprintf("criteria[%d]", i)}
		in, ok := item.(map[string]any)
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		if _, found := in["shared_set"]; withSet && !found {
			return nil, nil, badRequest(at.snake + ".shared_set is required")
		}
		var given []criterionType
		for _, t := range types {
			if _, ok := in[t.node.name]; ok {
				given = append(given, t)
			}
		}
		if len(given) != 1 {
			return nil, nil, badRequest(at.snake + " must name exactly one criterion type: " + strings.Join(typeNames(types), ", "))
		}
		typeIn, ok := in[given[0].node.name].(map[string]any)
		if !ok {
			return nil, nil, badRequest(at.snake + "." + given[0].node.name + " must be an object")
		}
		if err := requireMembers(typeIn, at.snake+"."+given[0].node.name, given[0].required...); err != nil {
			return nil, nil, err
		}
		if given[0].rules != nil {
			if err := given[0].rules(typeIn); err != nil {
				return nil, nil, err
			}
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(tree, in, at)
		if err != nil {
			return nil, nil, err
		}
		if withSet {
			sets = append(sets, stringOf(in["shared_set"]))
		}
		ops = append(ops, map[string]any{"create": body})
	}
	return ops, sets, nil
}

func customerNegativeRemoveOperations(customerID string, names []string) ([]any, error) {
	if err := exclusionCount("resource_names", len(names)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(names))
	for i, name := range names {
		if _, ok := numericIDOf(name, customerID, customerNegativeCriteriaCollection); !ok {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/customerNegativeCriteria/N of the account "+
				"of customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(names[:i], name) {
			return nil, badRequest("a criterion appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

func sharedCriterionRemoveOperations(customerID string, names []string) ([]any, error) {
	if err := exclusionCount("resource_names", len(names)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(names))
	for i, name := range names {
		if _, ok := ownedCriterionName(name, customerID, sharedCriteriaCollection); !ok {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/sharedCriteria/SHARED_SET~CRITERION of the "+
				"account of customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(names[:i], name) {
			return nil, badRequest("a criterion appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- results ---

type (
	customerNegativeCriterionResult struct {
		ResourceName string         `json:"resource_name"`
		Criterion    map[string]any `json:"customer_negative_criterion,omitempty"`
	}
	customerNegativeCriteriaChanged struct {
		CustomerID   string                            `json:"customer_id"`
		ValidateOnly bool                              `json:"validate_only"`
		Results      []customerNegativeCriterionResult `json:"results"`
	}
	sharedCriterionResult struct {
		ResourceName    string         `json:"resource_name"`
		SharedCriterion map[string]any `json:"shared_criterion,omitempty"`
	}
	sharedCriteriaChanged struct {
		CustomerID   string                  `json:"customer_id"`
		ValidateOnly bool                    `json:"validate_only"`
		Results      []sharedCriterionResult `json:"results"`
	}
)

// exclusionObject maps the criterion Google returned with a result. The resource name inside must be the one of
// the result and every constant must have its fixed form; the caller checks the ID.
func exclusionObject(raw json.RawMessage, tree node, name, customerID string) (map[string]any, bool) {
	object, ok := decodeObject(raw)
	if !ok {
		return nil, false
	}
	members, ok := outputMembers(tree.fields, object, customerID)
	if !ok {
		return nil, false
	}
	inner, _ := members["resource_name"].(string)
	return members, (inner == "" || inner == name) && constantsBound(tree, members)
}

func idMatches(members map[string]any, key, id string) bool {
	value, present := members[key]
	if !present {
		return true
	}
	whole, ok := value.(int64)
	return ok && strconv.FormatInt(whole, 10) == id
}

func customerNegativeCriteriaOf(op, customerID string, results []listResult) ([]customerNegativeCriterionResult, error) {
	out := make([]customerNegativeCriterionResult, 0, len(results))
	for i, r := range results {
		for _, previous := range results[:i] {
			if previous.ResourceName == r.ResourceName {
				return nil, unsureResult(op, "answered with the same criterion twice")
			}
		}
		item := customerNegativeCriterionResult{ResourceName: r.ResourceName}
		if r.Resource != nil {
			members, ok := exclusionObject(r.Resource, customerNegativeCriterionOutputTree, r.ResourceName, customerID)
			if !ok || !idMatches(members, "id", r.ID) {
				return nil, unsureResult(op, "answered with a criterion that is not the one asked for")
			}
			item.Criterion = members
		}
		out = append(out, item)
	}
	return out, nil
}

type sharedCriteriaAnswer struct {
	Results             []map[string]json.RawMessage `json:"results"`
	PartialFailureError json.RawMessage              `json:"partialFailureError"`
}

// sharedCriteriaOf binds the answer to the account: one result per operation, in the order sent, each a
// criterion of the account. For a removal it is the criterion asked for, for a creation one in the shared set asked
// for. Anything else has an unknown outcome, since the change may have been made.
func sharedCriteriaOf(op string, answer sharedCriteriaAnswer, customerID string, validateOnly bool, want, sets []string,
	count int) (*sharedCriteriaChanged, error) {
	out := &sharedCriteriaChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []sharedCriterionResult{}}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure, which this request did not ask for")
	}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != count {
		return nil, unsureResult(op, "answered with another number of results than the entries asked for")
	}
	const other = "answered with a result for another criterion, shared set or account than the one asked for"
	for i, result := range answer.Results {
		var name string
		if json.Unmarshal(result["resourceName"], &name) != nil {
			return nil, unsureResult(op, "answered with a result without a resource name")
		}
		parent, ok := ownedCriterionName(name, customerID, sharedCriteriaCollection)
		if !ok || (want != nil && name != want[i]) || (sets != nil && customerName(customerID)+"/sharedSets/"+parent != sets[i]) {
			return nil, unsureResult(op, other)
		}
		for _, previous := range out.Results {
			if previous.ResourceName == name {
				return nil, unsureResult(op, "answered with the same criterion twice")
			}
		}
		item := sharedCriterionResult{ResourceName: name}
		if raw := result["sharedCriterion"]; len(raw) > 0 && string(raw) != "null" {
			members, ok := exclusionObject(raw, sharedCriterionOutputTree, name, customerID)
			_, id, _ := strings.Cut(name[strings.LastIndex(name, "/")+1:], "~")
			if set, present := members["shared_set"].(string); ok && present && set != customerName(customerID)+"/sharedSets/"+parent {
				ok = false
			}
			if !ok || !idMatches(members, "criterion_id", id) {
				return nil, unsureResult(op, "answered with a criterion that is not the one asked for")
			}
			item.SharedCriterion = members
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptors ---

func exclusionCreateSchema(tree node, required ...string) schema {
	item := tree.inputSchema()
	delete(item, "description")
	if len(required) > 0 {
		item["required"] = required
	}
	return object(schema{"customer_id": customerIDArgument,
		"criteria":      schema{"type": "array", "minItems": 1, "maxItems": maxExclusions, "items": item},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "criteria")
}

func exclusionRemoveSchema(name schema) schema {
	return object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxExclusions, "uniqueItems": true, "items": name},
		"validate_only":  validateOnlySchema}, "customer_id", "resource_names")
}

var customerNegativeNameSchema = schema{"type": "string", "maxLength": 100,
	"pattern": `^customers/[0-9]{1,10}/customerNegativeCriteria/[0-9]{1,19}$`}

var (
	customerNegativeOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema,
			"customer_negative_criterion": customerNegativeCriterionOutputTree.outputSchema()}, "resource_name"))},
		"customer_id", "validate_only", "results")
	sharedCriteriaOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema,
			"shared_criterion": sharedCriterionOutputTree.outputSchema()}, "resource_name"))},
		"customer_id", "validate_only", "results")
)

// The notes end the descriptions of the changing tools. Google counts the quota per operation of the list.
const (
	exclusionsChangeNote = mutateListNote
	exclusionsListedNote = mutateListListedNote
)

const exclusionsReferenceNote = " References in the arguments to shared sets must belong to customer_id, and a temporary " +
	"negative ID is refused; the constants of Google (mobile app categories, locations) are accepted only in their fixed form. " +
	"Addresses of placements are targets of the ads and are never called by Qatlas Connect. Whether a type fits the " +
	"object, such as a keyword in a shared set of placements, is Google's to decide: the request is sent once and a " +
	"refusal is shown as one (validate_only finds it without a change)."

const customerNegativeNote = " A customer negative criterion excludes something from every campaign of the account, " +
	"so the call is confirmed per call. This tool does not read criteria (read them with reports.search on the " +
	"customer_negative_criterion resource). The request goes to CustomerNegativeCriterionService.MutateCustomerNegativeCriteria " +
	"with up to 50 operations, atomic and without partial_failure: Google applies all of them or none." + exclusionsReferenceNote

const sharedCriterionNote = " A shared criterion is one entry of a shared set, such as a negative keyword, a placement, a " +
	"brand, or a retail filter; every campaign that uses the shared set is affected, so the call is confirmed per " +
	"call. The shared set is created with shared-sets.create and attached with campaign-shared-sets.create; this tool " +
	"does not read criteria (read them with reports.search on the shared_criterion resource). The request goes to " +
	"SharedCriterionService.MutateSharedCriteria with up to 50 operations, atomic and without partial_failure: Google " +
	"applies all of them or none." + exclusionsReferenceNote

var exclusionCustomerArgument = campaignCustomerArgument

var customerNegativeFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per criterion, in the order given, with its resource_name and, with " +
		"MUTABLE_RESOURCE (create only), the criterion after the change with the members the tool offers and its id and " +
		"type; empty after validate_only"}}

var sharedCriteriaFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per criterion, in the order given, with its resource_name and, with " +
		"MUTABLE_RESOURCE (create only), the criterion after the change with the members the tool offers, its shared_set, " +
		"criterion_id and type; empty after validate_only"}}

var customerNegativeCriteriaCreate = capability.Descriptor{
	ID: customerNegativeCriteriaCreateID, Version: 1, Title: "Create Google Ads customer negative criteria",
	Description: "Create up to 50 customer negative criteria in a Google Ads account " +
		"(CustomerNegativeCriterionService.MutateCustomerNegativeCriteria with create operations): exclusions that apply " +
		"to the whole account. Each entry names exactly one criterion type (content_label, ip_block, " +
		"mobile_app_category, mobile_application, placement, youtube_channel, youtube_video, negative_keyword_list, " +
		"placement_list) with the members Google documents for it. Not offered: the output-only id and type." +
		customerNegativeNote + " Offered by a connection with the create effect or whose tools list names it; no " +
		"profile holds it." + exclusionsChangeNote,
	Tags: []string{"google", "ads", "customer-negative-criteria", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, exclusionsSensitivity),
	InputSchema: encoded(exclusionCreateSchema(customerNegativeCriterionTree)), OutputSchema: encoded(customerNegativeOutputSchema),
	Arguments: []capability.Argument{exclusionCustomerArgument,
		{Name: "criteria", Required: true, Description: "One to 50 criteria, each an object with exactly one criterion type object"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: customerNegativeFields,
	Examples: []capability.Example{{Description: "Validate the exclusion of a website without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","criteria":[{"placement":{"url":"www.example.com"}}],"validate_only":true}`)}},
}

var customerNegativeCriteriaRemove = capability.Descriptor{
	ID: customerNegativeCriteriaRemoveID, Version: 1, Title: "Remove Google Ads customer negative criteria",
	Description: "Remove up to 50 customer negative criteria of a Google Ads account " +
		"(CustomerNegativeCriterionService.MutateCustomerNegativeCriteria with remove operations): what the criterion " +
		"excluded is shown to the account's campaigns again. A removed criterion cannot be restored, only created " +
		"again. Resource names must be criteria of customer_id. Offers no response_content_type: a removed criterion has " +
		"no mutable resource to show." + customerNegativeNote + exclusionsListedNote,
	Tags: []string{"google", "ads", "customer-negative-criteria", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, exclusionsSensitivity),
	InputSchema: encoded(exclusionRemoveSchema(customerNegativeNameSchema)), OutputSchema: encoded(customerNegativeOutputSchema),
	Arguments: []capability.Argument{exclusionCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 50 criteria of this account, each once, " +
			"customers/ID/customerNegativeCriteria/N"}, validateOnlyArgument},
	Fields: customerNegativeFields,
	Examples: []capability.Example{{Description: "Validate the removal of a criterion without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/customerNegativeCriteria/7"],"validate_only":true}`)}},
}

var sharedCriteriaCreate = capability.Descriptor{
	ID: sharedCriteriaCreateID, Version: 1, Title: "Create Google Ads shared criteria",
	Description: "Create up to 50 shared criteria in the shared sets of a Google Ads account " +
		"(SharedCriterionService.MutateSharedCriteria with create operations). Each entry names the shared_set, exactly " +
		"one criterion type (keyword, mobile_app_category, mobile_application, placement, webpage, youtube_channel, " +
		"youtube_video, brand, retail_filter, vertical_ads_item_group_rule) with the members Google documents for it, and " +
		"optionally negative. Not offered: the output-only criterion_id and type." + sharedCriterionNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		exclusionsChangeNote,
	Tags: []string{"google", "ads", "shared-criteria", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, exclusionsSensitivity),
	InputSchema: encoded(exclusionCreateSchema(sharedCriterionTree, "shared_set")), OutputSchema: encoded(sharedCriteriaOutputSchema),
	Arguments: []capability.Argument{exclusionCustomerArgument,
		{Name: "criteria", Required: true, Description: "One to 50 criteria, each an object with shared_set, exactly one " +
			"criterion type object and optionally negative"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: sharedCriteriaFields,
	Examples: []capability.Example{{Description: "Validate a negative keyword without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","criteria":[{"shared_set":"customers/1234567890/sharedSets/112233",` +
			`"keyword":{"text":"free trial","match_type":"PHRASE"}}],"validate_only":true}`)}},
}

var sharedCriteriaRemove = capability.Descriptor{
	ID: sharedCriteriaRemoveID, Version: 1, Title: "Remove Google Ads shared criteria",
	Description: "Remove up to 50 shared criteria of a Google Ads account (SharedCriterionService.MutateSharedCriteria " +
		"with remove operations): the entry leaves its shared set for every campaign that uses it. A removed criterion " +
		"cannot be restored, only created again. Resource names must be criteria of customer_id. Offers no " +
		"response_content_type: a removed criterion has no mutable resource to show." + sharedCriterionNote + exclusionsListedNote,
	Tags: []string{"google", "ads", "shared-criteria", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, exclusionsSensitivity),
	InputSchema:  encoded(exclusionRemoveSchema(criterionNameSchema(sharedCriteriaCollection))),
	OutputSchema: encoded(sharedCriteriaOutputSchema),
	Arguments: []capability.Argument{exclusionCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 50 criteria of this account, each once, " +
			"customers/ID/sharedCriteria/SHARED_SET~CRITERION"}, validateOnlyArgument},
	Fields: sharedCriteriaFields,
	Examples: []capability.Example{{Description: "Validate the removal of a criterion without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/sharedCriteria/112233~7"],"validate_only":true}`)}},
}

// --- handlers ---

func handleCustomerNegativeCriteriaCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return customerNegativeCriteriaChange(ctx, resolved, secrets, red, raw, "create customer negative criteria",
		customerNegativeCriteriaCreateID)
}

func handleCustomerNegativeCriteriaRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return customerNegativeCriteriaChange(ctx, resolved, secrets, red, raw, "remove customer negative criteria",
		customerNegativeCriteriaRemoveID)
}

func handleSharedCriteriaCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return sharedCriteriaChange(ctx, resolved, secrets, red, raw, "create shared criteria", sharedCriteriaCreateID)
}

func handleSharedCriteriaRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return sharedCriteriaChange(ctx, resolved, secrets, red, raw, "remove shared criteria", sharedCriteriaRemoveID)
}

// exclusionArguments reads and checks the arguments both families share before anything else happens.
func exclusionArguments(op string, raw json.RawMessage) (criteriaArguments, error) {
	a, err := readCriteriaArguments(op, raw)
	if err != nil {
		return a, err
	}
	if !customerIDPattern.MatchString(a.CustomerID) {
		return a, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	return a, checkContentType(a.ResponseContentType)
}

func customerNegativeCriteriaChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, op, tool string) (any, error) {
	a, err := exclusionArguments(op, raw)
	if err != nil {
		return nil, err
	}
	var ops []any
	var want []string
	if tool == customerNegativeCriteriaCreateID {
		ops, _, err = exclusionCreateOperations(a.CustomerID, a.Criteria, customerNegativeCriterionTree,
			customerNegativeCriterionTypes, false)
	} else {
		ops, err = customerNegativeRemoveOperations(a.CustomerID, a.ResourceNames)
		want = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: a.CustomerID,
		service: customerNegativeCriteriaService, collection: customerNegativeCriteriaCollection,
		resourceKey: "customerNegativeCriterion", operations: ops, names: want, validateOnly: a.ValidateOnly,
		content: a.ResponseContentType})
	if err != nil {
		return nil, err
	}
	items, err := customerNegativeCriteriaOf(op, a.CustomerID, results)
	if err != nil {
		return nil, err
	}
	return customerNegativeCriteriaChanged{CustomerID: a.CustomerID, ValidateOnly: a.ValidateOnly, Results: items}, nil
}

func sharedCriteriaChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, op, tool string) (any, error) {
	a, err := exclusionArguments(op, raw)
	if err != nil {
		return nil, err
	}
	var ops []any
	var want, sets []string
	if tool == sharedCriteriaCreateID {
		ops, sets, err = exclusionCreateOperations(a.CustomerID, a.Criteria, sharedCriterionTree, sharedCriterionTypes, true)
	} else {
		ops, err = sharedCriterionRemoveOperations(a.CustomerID, a.ResourceNames)
		want = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer sharedCriteriaAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: sharedCriteriaService,
		body: mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)}, &answer); err != nil {
		return nil, err
	}
	return sharedCriteriaOf(op, answer, a.CustomerID, a.ValidateOnly, want, sets, len(ops))
}

// --- confirmation ---

// exclusionSummary phrases the change for the confirmation, from the arguments alone: the account, the type of
// each criterion with its first text member, texts cut and without control characters.
func exclusionSummary(tool string, types []criterionType, noun string) capability.SummaryFunc {
	removing := tool == customerNegativeCriteriaRemoveID || tool == sharedCriteriaRemoveID
	return func(raw json.RawMessage) string {
		a, _ := readCriteriaArguments("", raw)
		var parts []string
		for _, name := range a.ResourceNames[:min(len(a.ResourceNames), 10)] {
			parts = append(parts, boundedText(name, 120))
		}
		total := len(a.ResourceNames)
		if !removing {
			total = len(a.Criteria)
			for _, item := range a.Criteria[:min(len(a.Criteria), 10)] {
				in, _ := item.(map[string]any)
				parts = append(parts, exclusionPart(in, types))
			}
		}
		verb := "Create"
		if removing {
			verb = "Remove"
		}
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		more := ""
		if total > len(parts) {
			more = " and " + strconv.Itoa(total-len(parts)) + " more"
		}
		summary := verb + " " + strconv.Itoa(total) + " " + noun + " of Google Ads account " + a.CustomerID + ": " +
			strings.Join(parts, "; ") + more + "."
		if removing {
			return summary + " A removed criterion cannot be restored."
		}
		return summary + " An exclusion changes who sees the ads."
	}
}

// summaryKeys are the members that name what a criterion excludes, in the order the confirmation prefers.
var summaryKeys = []string{"text", "url", "ip_address", "video_id", "channel_id", "entity_id", "app_id", "criterion_name",
	"item_code", "venue", "shared_set"}

func exclusionPart(in map[string]any, types []criterionType) string {
	part := ""
	for _, t := range types {
		object, ok := in[t.node.name].(map[string]any)
		if !ok {
			continue
		}
		part = t.node.name
		for _, key := range summaryKeys {
			if text, ok := object[key].(string); ok {
				part += " " + strconv.Quote(boundedText(text, 100))
				break
			}
		}
	}
	var facts []string
	if set := stringOf(in["shared_set"]); set != "" {
		facts = append(facts, "shared set "+boundedText(set, 120))
	}
	if negative, _ := in["negative"].(bool); negative {
		facts = append(facts, "excluded")
	}
	if len(facts) > 0 {
		part += " (" + strings.Join(facts, ", ") + ")"
	}
	return part
}
