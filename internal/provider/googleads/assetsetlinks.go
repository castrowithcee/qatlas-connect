package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	assetSetAssetsCreateID    = Provider + ".asset-set-assets.create"
	assetSetAssetsRemoveID    = Provider + ".asset-set-assets.remove"
	campaignAssetSetsCreateID = Provider + ".campaign-asset-sets.create"
	campaignAssetSetsRemoveID = Provider + ".campaign-asset-sets.remove"
	adGroupAssetSetsCreateID  = Provider + ".ad-group-asset-sets.create"
	adGroupAssetSetsRemoveID  = Provider + ".ad-group-asset-sets.remove"
	customerAssetSetsCreateID = Provider + ".customer-asset-sets.create"
	customerAssetSetsRemoveID = Provider + ".customer-asset-sets.remove"
)

// A setLinkFamily is a resource that ties an asset set to another object: an asset in a set, a set attached to
// a campaign, an ad group, or the account. Such a link has no mutable member and no update. It is created from the resource names of the
// objects it ties and removed by its own resource name, customers/ID/<collection>/A~B, whose key is the IDs of
// the parts in order. The customer link has a single part, the asset set, since the account is the one of the call.
type setLinkFamily struct {
	// tool is the name segment of the tool IDs; argument is the list argument of the create tool.
	tool, argument string
	// service is the Mutate service path below the customer; collection the collection of the link and key
	// the member of a Mutate result that holds the link after the change.
	service, collection, key string
	// resultKey is the output member of a result that holds the link; noun names a link in texts.
	resultKey, noun string
	parts           []setLinkPart
	// account names a member of the answer that holds the account of the call; it is never an argument.
	account string
	// example is a create call and removeExample a remove call, each with the arguments after customer_id.
	example, removeExample string
	// rpc is the method that serves both tools; effect says in a few words what a link does, and sensitivity
	// is the data it exposes.
	rpc, effect, sensitivity string
}

// setLinkPart is one object a link ties: a member of the link that holds a resource name of this account.
type setLinkPart struct {
	name, collection, noun string
}

func (f setLinkFamily) id(verb string) string { return Provider + "." + f.tool + "." + verb }

// prefix is the start of every resource name of the link collection of the account.
func (f setLinkFamily) prefix(customerID string) string {
	return customerName(customerID) + "/" + f.collection + "/"
}

// idsOf returns the IDs of customers/ID/<collection>/A~B of exactly the account, one per part.
func (f setLinkFamily) idsOf(name, customerID string) ([]string, bool) {
	rest, ok := strings.CutPrefix(name, f.prefix(customerID))
	if !ok {
		return nil, false
	}
	ids := strings.Split(rest, "~")
	if len(ids) != len(f.parts) {
		return nil, false
	}
	for _, id := range ids {
		if !numericIDPattern.MatchString(id) {
			return nil, false
		}
	}
	return ids, true
}

func (f setLinkFamily) nameOf(customerID string, ids []string) string {
	return f.prefix(customerID) + strings.Join(ids, "~")
}

func (f setLinkFamily) partName(customerID string, i int, id string) string {
	return customerName(customerID) + "/" + f.parts[i].collection + "/" + id
}

func (f setLinkFamily) namePattern() string {
	id := `[1-9][0-9]{0,18}`
	return `^customers/[0-9]{1,10}/` + f.collection + `/` + id + strings.Repeat(`~`+id, len(f.parts)-1) + `$`
}

// outputTree is the link as Google answers with it: the parts, bound to the account, and the status, which
// Google fills itself and no request can set.
func (f setLinkFamily) outputTree() node {
	fields := []node{textNode("resource_name", "", maxNameArgument)}
	for _, p := range f.parts {
		fields = append(fields, resourceNode(p.name, "", p.collection))
	}
	if f.account != "" {
		fields = append(fields, textNode(f.account, "", maxNameArgument))
	}
	return objectNode("", "", append(fields, enumNode("status", "", nil))...)
}

// partNode is the argument for one part of a link to create.
func (f setLinkFamily) partNode(i int) node {
	p := f.parts[i]
	return resourceNode(p.name, p.noun+" of this account, customers/ID/"+p.collection+"/N; a temporary negative ID is refused",
		p.collection)
}

func (f setLinkFamily) createItemSchema() schema {
	props := schema{}
	required := make([]string, 0, len(f.parts))
	for i, p := range f.parts {
		props[p.name] = f.partNode(i).inputSchema()
		required = append(required, p.name)
	}
	return object(props, required...)
}

func (f setLinkFamily) inputSchema(create bool) schema {
	if create {
		return object(schema{"customer_id": customerIDArgument,
			f.argument:              schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "items": f.createItemSchema()},
			"validate_only":         validateOnlySchema,
			"response_content_type": responseContentTypeSchema}, "customer_id", f.argument)
	}
	return object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "uniqueItems": true,
			"items": schema{"type": "string", "maxLength": 100, "pattern": f.namePattern()}},
		"validate_only": validateOnlySchema}, "customer_id", "resource_names")
}

func (f setLinkFamily) outputSchema() schema {
	return object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema, f.resultKey: f.outputTree().outputSchema()},
			"resource_name"))}, "customer_id", "validate_only", "results")
}

// setLinkArguments are the arguments of both tools. Entries is the list of the create tool under its own
// name, which differs per family.
type setLinkArguments struct {
	CustomerID          string
	Entries             []any
	ResourceNames       []string
	ValidateOnly        bool
	ResponseContentType string
}

func (f setLinkFamily) read(op string, raw json.RawMessage) (setLinkArguments, error) {
	var in struct {
		CustomerID          string   `json:"customer_id"`
		ResourceNames       []string `json:"resource_names"`
		ValidateOnly        bool     `json:"validate_only"`
		ResponseContentType string   `json:"response_content_type"`
	}
	a := setLinkArguments{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&in); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	if list, present := all[f.argument]; present {
		decoder := json.NewDecoder(bytes.NewReader(list))
		decoder.UseNumber()
		if err := decoder.Decode(&a.Entries); err != nil {
			return a, failure(op, "the validated arguments could not be read")
		}
	}
	a.CustomerID, a.ResourceNames, a.ValidateOnly, a.ResponseContentType =
		in.CustomerID, in.ResourceNames, in.ValidateOnly, in.ResponseContentType
	return a, nil
}

func setLinkCount(argument string, n int) error {
	if n == 0 || n > maxListOperations {
		return badRequest(argument + " must hold one to " + strconv.Itoa(maxListOperations) + " entries")
	}
	return nil
}

// createOperations checks every entry: it names exactly the parts, each a resource of the account of the call.
// The resource name Google must answer with follows from the IDs, so the answer is checked against it.
func (f setLinkFamily) createOperations(customerID string, items []any) ([]any, []string, error) {
	if err := setLinkCount(f.argument, len(items)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		at := f.argument + "[" + strconv.Itoa(i) + "]"
		in, ok := item.(map[string]any)
		if !ok || len(in) != len(f.parts) {
			return nil, nil, badRequest(at + " must be an object with exactly " + f.partList())
		}
		body := map[string]any{}
		ids := make([]string, 0, len(f.parts))
		for j, p := range f.parts {
			value, _ := in[p.name].(string)
			id, ok := numericIDOf(value, customerID, p.collection)
			if !ok {
				return nil, nil, badRequest(at + "." + p.name + " must be customers/" + queryNumber(customerID) + "/" +
					p.collection + "/N of the account of customer_id; a temporary ID is not accepted")
			}
			body[camel(f.parts[j].name)] = value
			ids = append(ids, id)
		}
		name := f.nameOf(customerID, ids)
		for _, seen := range names {
			if seen == name {
				return nil, nil, badRequest("a " + f.noun + " appears in " + f.argument + " only once")
			}
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"create": body})
	}
	return ops, names, nil
}

func (f setLinkFamily) partList() string {
	names := make([]string, len(f.parts))
	for i, p := range f.parts {
		names[i] = p.name
	}
	return strings.Join(names, " and ")
}

func (f setLinkFamily) removeOperations(customerID string, resourceNames []string) ([]any, error) {
	if err := setLinkCount("resource_names", len(resourceNames)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if _, ok := f.idsOf(name, customerID); !ok {
			return nil, badRequest("resource_names[" + strconv.Itoa(i) + "] must be customers/" + queryNumber(customerID) +
				"/" + f.collection + "/" + strings.Repeat("N~", len(f.parts)-1) + "N of the account of customer_id")
		}
		for _, seen := range resourceNames[:i] {
			if seen == name {
				return nil, badRequest("a " + f.noun + " appears in resource_names only once")
			}
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// result maps the answer. mutateList has bound every resource name to the account and to the link asked for;
// the object of MUTABLE_RESOURCE must be that very link, tying the parts its resource name names.
func (f setLinkFamily) result(op, customerID string, results []listResult, validateOnly bool) (map[string]any, error) {
	tree := f.outputTree()
	list := make([]map[string]any, 0, len(results))
	for _, r := range results {
		item := map[string]any{"resource_name": r.ResourceName}
		if len(r.Resource) > 0 {
			raw, ok := decodeObject(r.Resource)
			var members map[string]any
			if ok {
				members, ok = outputMembers(tree.fields, raw, customerID)
			}
			var ids []string
			if ok {
				ids, ok = f.idsOf(r.ResourceName, customerID)
			}
			if ok {
				name, _ := members["resource_name"].(string)
				ok = name == "" || name == r.ResourceName
				if got, has := members[f.account].(string); f.account != "" && has && got != customerName(customerID) {
					ok = false
				}
				for i, p := range f.parts {
					if got, has := members[p.name].(string); has && got != f.partName(customerID, i, ids[i]) {
						ok = false
					}
				}
			}
			if !ok {
				return nil, unsureResult(op, "answered with a "+f.noun+" that is not the one asked for")
			}
			item[f.resultKey] = members
		}
		list = append(list, item)
	}
	return map[string]any{"customer_id": customerID, "validate_only": validateOnly, "results": list}, nil
}

// --- descriptors ---

func (f setLinkFamily) fields() []capability.Field {
	return []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One entry per " + f.noun + ", in the order given, with its resource_name " +
			"(customers/ID/" + f.collection + "/" + strings.Repeat("N~", len(f.parts)-1) + "N, always of the account " +
			"of the call) and, with MUTABLE_RESOURCE, the " + f.noun + " after the change under " + f.resultKey + ": " +
			f.partList() + f.accountField() + ", resource_name and status as Google returns them. Empty after validate_only"},
	}
}

func (f setLinkFamily) accountField() string {
	if f.account == "" {
		return ""
	}
	return ", " + f.account + " (always the account of the call)"
}

func (f setLinkFamily) note() string {
	return " A " + f.noun + " " + f.effect + "; it has no member that can be changed, so there is no update tool. " +
		"The request goes to " + f.rpc + " with up to 10 operations, atomic and without partial_failure: Google " +
		"applies all of them or none. Resource names in the arguments must belong to customer_id, and an ID that is " +
		"negative, a temporary one, is refused. The tool does not read " + f.noun + "s (read them with " +
		"reports.search on " + f.resultKey + ")."
}

func (f setLinkFamily) descriptors() (capability.Descriptor, capability.Descriptor) {
	tags := func(verb string) []string {
		return []string{"google", "ads", "assets", "asset-sets", f.tool, verb}
	}
	createDescription := "Create one to 10 " + f.noun + "s in a Google Ads account (one create operation each), each " +
		"named by " + f.partList() + ", resources of the account of the call."
	create := capability.Descriptor{
		ID: f.id("create"), Version: 1, Title: "Create " + f.noun + "s in a Google Ads account",
		Description: createDescription + f.note() +
			" Offered by a connection with the create effect or whose tools list names it; no profile holds it." + mutateListNote,
		Tags: tags("create"), Provider: Provider,
		Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, f.sensitivity),
		InputSchema: encoded(f.inputSchema(true)), OutputSchema: encoded(f.outputSchema()),
		Arguments: []capability.Argument{assetSetCustomerArgument,
			{Name: f.argument, Required: true, Description: "One to 10 entries, each an object of " + f.partList() +
				" as customers/ID/<collection>/N of customer_id, each link once"},
			validateOnlyArgument, responseContentTypeArgument},
		Fields: f.fields(),
		Examples: []capability.Example{{Description: "Validate the creation without making it", Arguments: json.RawMessage(
			`{"customer_id":"1234567890","` + f.argument + `":[` + f.example + `],"validate_only":true}`)}},
	}
	remove := capability.Descriptor{
		ID: f.id("remove"), Version: 1, Title: "Remove " + f.noun + "s from a Google Ads account",
		Description: "Remove one to 10 " + f.noun + "s of a Google Ads account (one remove operation each), each named " +
			"by its resource name. Only the link ends, none of the objects it ties is removed; Google marks the link " +
			"REMOVED, and it can be created again. Offers no response_content_type: a removed link has no mutable " +
			"resource to show." + f.note() + " Offered by a connection with the delete effect or whose tools list names " +
			"it; no profile holds it." + mutateListNote,
		Tags: tags("remove"), Provider: Provider,
		Risk:        changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, f.sensitivity),
		InputSchema: encoded(f.inputSchema(false)), OutputSchema: encoded(f.outputSchema()),
		Arguments: []capability.Argument{assetSetCustomerArgument,
			{Name: "resource_names", Required: true, Description: "One to 10 " + f.noun + "s of this account, each once, " +
				"customers/ID/" + f.collection + "/" + strings.Repeat("N~", len(f.parts)-1) + "N as reports.search returns them"},
			validateOnlyArgument},
		Fields: f.fields(),
		Examples: []capability.Example{{Description: "Validate the removal without making it", Arguments: json.RawMessage(
			`{"customer_id":"1234567890","resource_names":["` + f.removeExample + `"],"validate_only":true}`)}},
	}
	return create, remove
}

// --- handlers ---

func (f setLinkFamily) handler(verb string) capability.Handler {
	op := verb + " " + f.noun + "s"
	tool := f.id(verb)
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		a, err := f.read(op, raw)
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
		if verb == "create" {
			ops, names, err = f.createOperations(a.CustomerID, a.Entries)
		} else {
			if a.ResponseContentType != "" {
				return nil, badRequest("response_content_type is not offered by " + tool)
			}
			ops, err = f.removeOperations(a.CustomerID, a.ResourceNames)
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
			service: f.service, collection: f.collection, resourceKey: f.key, operations: ops, names: names,
			idOf: func(name string) (string, bool) {
				ids, ok := f.idsOf(name, a.CustomerID)
				return strings.Join(ids, "~"), ok
			},
			validateOnly: a.ValidateOnly, content: a.ResponseContentType})
		if err != nil {
			return nil, err
		}
		return f.result(op, a.CustomerID, results, a.ValidateOnly)
	}
}

// summary phrases the change for the confirmation, from the arguments alone.
func (f setLinkFamily) summary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := f.read("", raw)
		text := map[string]string{"create": "Create", "remove": "Remove"}[verb]
		if a.ValidateOnly {
			text = "Only validate: " + strings.ToLower(text[:1]) + text[1:]
		}
		var parts []string
		for _, name := range a.ResourceNames[:min(len(a.ResourceNames), 10)] {
			parts = append(parts, boundedText(name, maxCodeText))
		}
		for _, item := range a.Entries[:min(len(a.Entries), 10)] {
			in, _ := item.(map[string]any)
			var pair []string
			for _, p := range f.parts {
				pair = append(pair, p.name+" "+boundedText(stringOf(in[p.name]), maxCodeText))
			}
			parts = append(parts, strings.Join(pair, " in "))
		}
		total := len(a.Entries) + len(a.ResourceNames)
		more := ""
		if total > len(parts) {
			more = " and " + strconv.Itoa(total-len(parts)) + " more"
		}
		summary := text + " " + strconv.Itoa(total) + " " + f.noun + "(s) of Google Ads account " + a.CustomerID + ": " +
			strings.Join(parts, "; ") + more + "."
		if verb == "remove" {
			summary += " Only the link ends; the objects it ties stay."
		}
		return summary
	}
}

// --- the families of this package ---

var assetSetAssetsFamily = setLinkFamily{
	tool: "asset-set-assets", argument: "asset_set_assets",
	service: "/assetSetAssets", collection: "assetSetAssets", key: "assetSetAsset", resultKey: "asset_set_asset",
	noun: "asset set asset",
	parts: []setLinkPart{{name: "asset_set", collection: "assetSets", noun: "Asset set"},
		{name: "asset", collection: "assets", noun: "Asset"}},
	example:       `{"asset_set":"customers/1234567890/assetSets/31","asset":"customers/1234567890/assets/41"}`,
	removeExample: "customers/1234567890/assetSetAssets/31~41",
	rpc:           "AssetSetAssetService.MutateAssetSetAssets",
	effect:        "puts an asset into an asset set",
	sensitivity:   "which assets an asset set of a Google Ads account holds",
}

var campaignAssetSetsFamily = setLinkFamily{
	tool: "campaign-asset-sets", argument: "campaign_asset_sets",
	service: "/campaignAssetSets", collection: "campaignAssetSets", key: "campaignAssetSet", resultKey: "campaign_asset_set",
	noun: "campaign asset set",
	parts: []setLinkPart{{name: "campaign", collection: "campaigns", noun: "Campaign"},
		{name: "asset_set", collection: "assetSets", noun: "Asset set"}},
	example:       `{"campaign":"customers/1234567890/campaigns/21","asset_set":"customers/1234567890/assetSets/31"}`,
	removeExample: "customers/1234567890/campaignAssetSets/21~31",
	rpc:           "CampaignAssetSetService.MutateCampaignAssetSets",
	effect:        "attaches an asset set to a campaign",
	sensitivity:   "which asset sets a campaign of a Google Ads account uses",
}

var adGroupAssetSetsFamily = setLinkFamily{
	tool: "ad-group-asset-sets", argument: "ad_group_asset_sets",
	service: "/adGroupAssetSets", collection: "adGroupAssetSets", key: "adGroupAssetSet", resultKey: "ad_group_asset_set",
	noun: "ad group asset set",
	parts: []setLinkPart{{name: "ad_group", collection: "adGroups", noun: "Ad group"},
		{name: "asset_set", collection: "assetSets", noun: "Asset set"}},
	example:       `{"ad_group":"customers/1234567890/adGroups/51","asset_set":"customers/1234567890/assetSets/31"}`,
	removeExample: "customers/1234567890/adGroupAssetSets/51~31",
	rpc:           "AdGroupAssetSetService.MutateAdGroupAssetSets",
	effect:        "attaches an asset set to an ad group",
	sensitivity:   "which asset sets an ad group of a Google Ads account uses",
}

var customerAssetSetsFamily = setLinkFamily{
	tool: "customer-asset-sets", argument: "customer_asset_sets",
	service: "/customerAssetSets", collection: "customerAssetSets", key: "customerAssetSet", resultKey: "customer_asset_set",
	noun:          "customer asset set",
	parts:         []setLinkPart{{name: "asset_set", collection: "assetSets", noun: "Asset set"}},
	account:       "customer",
	example:       `{"asset_set":"customers/1234567890/assetSets/31"}`,
	removeExample: "customers/1234567890/customerAssetSets/31",
	rpc:           "CustomerAssetSetService.MutateCustomerAssetSets",
	effect:        "attaches an asset set to the account itself",
	sensitivity:   "which asset sets a Google Ads account uses for itself",
}

var assetSetAssetsCreate, assetSetAssetsRemove = assetSetAssetsFamily.descriptors()
var campaignAssetSetsCreate, campaignAssetSetsRemove = campaignAssetSetsFamily.descriptors()
var adGroupAssetSetsCreate, adGroupAssetSetsRemove = adGroupAssetSetsFamily.descriptors()
var customerAssetSetsCreate, customerAssetSetsRemove = customerAssetSetsFamily.descriptors()
