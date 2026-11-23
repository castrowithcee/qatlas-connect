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
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	assetGroupSignalsCreateID = Provider + ".asset-group-signals.create"
	assetGroupSignalsRemoveID = Provider + ".asset-group-signals.remove"

	assetGroupSignalsService    = "/assetGroupSignals"
	assetGroupSignalsCollection = "assetGroupSignals"

	// maxAssetGroupSignals is Qatlas's own bound per call; MutateAssetGroupSignals states none.
	maxAssetGroupSignals = 10
	// maxSearchThemeBytes is Qatlas's own loose bound: v26 documents no length or word count for SearchThemeInfo.text
	// (the errors TOO_LONG and TOO_MANY_WORDS exist), so Google decides the exact limits.
	maxSearchThemeBytes = 255
)

// The members of the AssetGroupSignal resource of v26 that the tools offer: the four members of the signal union.
// Not offered: the members Google fills itself (approval_status, disapproval_reasons), which the answer shows.
// The resource has no deprecated member and, unlike AssetGroup, no primary_status.

// signalKinds are the members of the signal union with the one member each has in v26: AudienceInfo.audience,
// SearchThemeInfo.text, LocalServiceIdInfo.service_id and VerticalAdsItemGroupRuleListInfo.shared_set. collection
// is set for a resource name of the account.
var signalKinds = []struct{ name, member, collection string }{
	{"audience", "audience", "audiences"},
	{"search_theme", "text", ""},
	{"local_services_id", "service_id", ""},
	{"vertical_ads_item_group_rule_list", "shared_set", "sharedSets"},
}

// maxSignalTextBytes is Qatlas's own bound of service_id: the reference of v26 states none.
const maxSignalTextBytes = 128

// signalNamePattern is customers/ID/assetGroupSignals/ASSETGROUPID~CRITERIONID.
const signalNamePattern = `^customers/[0-9]{1,10}/assetGroupSignals/[1-9][0-9]{0,18}~[1-9][0-9]{0,18}$`

// signalIDs splits customers/ID/assetGroupSignals/A~C of exactly the account into the two positive IDs.
func signalIDs(name, customerID string) (group, criterion string, ok bool) {
	rest, found := strings.CutPrefix(name, customerName(customerID)+"/"+assetGroupSignalsCollection+"/")
	if !found {
		return "", "", false
	}
	group, criterion, found = strings.Cut(rest, "~")
	return group, criterion, found && numericIDPattern.MatchString(group) && numericIDPattern.MatchString(criterion)
}

func signalIDOf(customerID string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		group, criterion, ok := signalIDs(name, customerID)
		return group + "~" + criterion, ok
	}
}

// assetGroupSignalOutput is the signal as Google answers with it. Every member is bound to the account of the
// call when it is read.
var assetGroupSignalOutput = objectNode("asset_group_signal", "",
	textNode("resource_name", "", maxNameArgument),
	resourceNode("asset_group", "", assetGroupsCollection),
	objectNode("audience", "", resourceNode("audience", "", "audiences")),
	objectNode("search_theme", "", textNode("text", "", maxSearchThemeBytes)),
	objectNode("local_services_id", "", textNode("service_id", "", maxNameArgument)),
	objectNode("vertical_ads_item_group_rule_list", "", resourceNode("shared_set", "", "sharedSets")),
	enumNode("approval_status", "", nil),
	listOf(textNode("disapproval_reasons", "", maxNameArgument), maxListItems))

type assetGroupSignalArguments struct {
	CustomerID          string   `json:"customer_id"`
	Signals             []any    `json:"asset_group_signals"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readAssetGroupSignalArguments(op string, raw json.RawMessage) (assetGroupSignalArguments, error) {
	var a assetGroupSignalArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

// assetGroupSignalOperation checks one entry and builds its operation. It returns the ID of the asset group,
// which the resource name of the answer must carry, and the identity of the signal, to refuse a signal given twice.
func assetGroupSignalOperation(customerID string, item any, at string) (op map[string]any, group, identity string, err error) {
	in, ok := item.(map[string]any)
	if !ok {
		return nil, "", "", badRequest(at + " must be an object")
	}
	exempt, in, err := exemptionKeys(in, at)
	if err != nil {
		return nil, "", "", err
	}
	name, _ := in["asset_group"].(string)
	if !ownedResource(name, customerID, assetGroupsCollection) {
		return nil, "", "", badRequest(at + ".asset_group must be customers/" + queryNumber(customerID) +
			"/assetGroups/N of the account of customer_id; a temporary negative ID is not accepted")
	}
	body := map[string]any{"assetGroup": name}
	given := 0
	for key, value := range in {
		if key == "asset_group" {
			continue
		}
		i := slices.IndexFunc(signalKinds, func(k struct{ name, member, collection string }) bool { return k.name == key })
		if i < 0 {
			return nil, "", "", badRequest(at + "." + key + " is not an argument of this tool")
		}
		kind := signalKinds[i]
		member, _ := value.(map[string]any)
		text, _ := member[kind.member].(string)
		if len(member) != 1 || text == "" {
			return nil, "", "", badRequest(at + "." + key + " must be an object with exactly " + kind.member)
		}
		path := at + "." + key + "." + kind.member
		switch {
		case kind.collection != "":
			if !ownedResource(text, customerID, kind.collection) {
				return nil, "", "", badRequest(path + " must be customers/" + queryNumber(customerID) + "/" +
					kind.collection + "/N of the account of customer_id; a temporary negative ID is not accepted")
			}
		case strings.TrimSpace(text) == "":
			return nil, "", "", badRequest(path + " must not be blank")
		default:
			limit := maxSignalTextBytes
			if key == "search_theme" {
				limit = maxSearchThemeBytes
			}
			if err := plainText(path, text, limit); err != nil {
				return nil, "", "", err
			}
		}
		body[camel(key)] = map[string]any{camel(kind.member): text}
		identity = name + "|" + key + "|" + text
		given++
	}
	if given != 1 {
		return nil, "", "", badRequest(at + " must give exactly one of audience, search_theme, local_services_id and " +
			"vertical_ads_item_group_rule_list")
	}
	op = map[string]any{"create": body}
	if exempt != nil {
		op["exemptPolicyViolationKeys"] = exempt
	}
	return op, name[strings.LastIndex(name, "/")+1:], identity, nil
}

func assetGroupSignalCreateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAssetGroupSignals {
		return nil, nil, badRequest("asset_group_signals must hold one to 10 signals")
	}
	ops := make([]any, 0, len(items))
	groups := make([]string, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		op, group, identity, err := assetGroupSignalOperation(customerID, item, fmt.Sprintf("asset_group_signals[%d]", i))
		if err != nil {
			return nil, nil, err
		}
		if seen[identity] {
			return nil, nil, badRequest("a signal appears in asset_group_signals only once")
		}
		seen[identity] = true
		ops = append(ops, op)
		groups = append(groups, group)
	}
	return ops, groups, nil
}

func assetGroupSignalRemoveOperations(customerID string, names []string) ([]any, error) {
	if len(names) == 0 || len(names) > maxAssetGroupSignals {
		return nil, badRequest("resource_names must hold one to 10 signals")
	}
	ops := make([]any, 0, len(names))
	for i, name := range names {
		if _, _, ok := signalIDs(name, customerID); !ok {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/assetGroupSignals/ASSETGROUPID~CRITERIONID "+
				"of the account of customer_id", i, queryNumber(customerID)))
		}
		for _, seen := range names[:i] {
			if seen == name {
				return nil, badRequest("a signal appears in resource_names only once")
			}
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// assetGroupSignalResult maps the answer. mutateList has bound every resource name to the account; a created
// signal must belong to the asset group asked for, and the object shown must be the very signal its name names.
func assetGroupSignalResult(op, customerID string, results []listResult, groups []string, validateOnly bool) (map[string]any, error) {
	list := make([]map[string]any, 0, len(results))
	for i, r := range results {
		group, _, _ := signalIDs(r.ResourceName, customerID)
		if groups != nil && group != groups[i] {
			return nil, unsureResult(op, "answered with a signal of another asset group than the one asked for")
		}
		item := map[string]any{"resource_name": r.ResourceName}
		if len(r.Resource) > 0 {
			raw, ok := decodeObject(r.Resource)
			var members map[string]any
			if ok {
				members, ok = outputMembers(assetGroupSignalOutput.fields, raw, customerID)
			}
			if ok {
				name, _ := members["resource_name"].(string)
				shown, _ := members["asset_group"].(string)
				ok = (name == "" || name == r.ResourceName) &&
					(shown == "" || shown == customerName(customerID)+"/assetGroups/"+group)
			}
			if !ok {
				return nil, unsureResult(op, "answered with a signal that is not the one asked for")
			}
			item["asset_group_signal"] = members
		}
		list = append(list, item)
	}
	return map[string]any{"customer_id": customerID, "validate_only": validateOnly, "results": list}, nil
}

// --- descriptors ---

const assetGroupSignalSensitivity = "signals of a Performance Max asset group: which audiences, search topics, services and feed rules steer it"

func assetGroupSignalInputSchema(create bool) schema {
	if !create {
		return object(schema{"customer_id": customerIDArgument,
			"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxAssetGroupSignals, "uniqueItems": true,
				"items": schema{"type": "string", "maxLength": 100, "pattern": signalNamePattern}},
			"validate_only": validateOnlySchema}, "customer_id", "resource_names")
	}
	only := func(n node, member string) schema {
		n.required = []string{member}
		return n.inputSchema()
	}
	item := object(schema{
		"asset_group": resourceNode("asset_group", "Asset group of this account, customers/ID/assetGroups/N; fixed", assetGroupsCollection).inputSchema(),
		"audience": only(objectNode("audience", "Audience signal, the AudienceInfo of v26",
			resourceNode("audience", "Audience of this account, customers/ID/audiences/N", "audiences")), "audience"),
		"search_theme": only(objectNode("search_theme", "Search theme signal, the SearchThemeInfo of v26",
			textNode("text", "Search theme, a keyword-like text; Google checks length, words and characters", maxSearchThemeBytes)), "text"),
		"local_services_id": only(objectNode("local_services_id", "Local Services signal, the LocalServiceIdInfo of v26",
			textNode("service_id", "A Local Services Ads service ID, a service type such as install_faucet", maxSignalTextBytes)), "service_id"),
		"vertical_ads_item_group_rule_list": only(objectNode("vertical_ads_item_group_rule_list",
			"Vertical ads signal, the VerticalAdsItemGroupRuleListInfo of v26: unlike the other signals it decides which items of the attached travel feed may serve",
			resourceNode("shared_set", "Shared set of this account, customers/ID/sharedSets/N, of type VERTICAL_ADS_ITEM_GROUP_RULE_LIST", "sharedSets")), "shared_set"),
		"exempt_policy_violation_keys": exemptionKeysSchema}, "asset_group")
	item["minProperties"] = 2
	return object(schema{"customer_id": customerIDArgument,
		"asset_group_signals":   schema{"type": "array", "minItems": 1, "maxItems": maxAssetGroupSignals, "items": item},
		"validate_only":         validateOnlySchema,
		"response_content_type": responseContentTypeSchema}, "customer_id", "asset_group_signals")
}

var assetGroupSignalOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema,
		"asset_group_signal": assetGroupSignalOutput.outputSchema()}, "resource_name"))},
	"customer_id", "validate_only", "results")

var assetGroupSignalFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per signal, in the order given, with its resource_name " +
		"(customers/ID/assetGroupSignals/ASSETGROUPID~CRITERIONID, always of the account of the call) and, with " +
		"MUTABLE_RESOURCE, the signal under asset_group_signal: resource_name, asset_group, audience, search_theme " +
		"(text), local_services_id, vertical_ads_item_group_rule_list and, where Google fills them, approval_status " +
		"and disapproval_reasons (the policy review of a search theme; absent for an audience). Empty after validate_only"},
}

const assetGroupSignalNote = " A signal steers the Performance Max campaign toward an audience or a search theme; it " +
	"guides the optimization and sets no targeting limit, except the vertical ads rule list, which selects items. It has no member that can be changed, so there is no update " +
	"tool. The request goes to AssetGroupSignalService.MutateAssetGroupSignals with up to 10 operations, atomic and " +
	"without partial_failure: Google applies all of them or none. Resource names in the arguments must belong to " +
	"customer_id, and a temporary negative ID is refused. The tool does not read signals (read them with " +
	"reports.search on asset_group_signal)."

var assetGroupSignalsCreate = capability.Descriptor{
	ID: assetGroupSignalsCreateID, Version: 1, Title: "Create signals of Google Ads asset groups",
	Description: "Create up to 10 signals of Performance Max asset groups in a Google Ads " +
		"account (one create operation each). Each entry names its asset_group and exactly one of audience (an audience of " +
		"this account), search_theme (a text), local_services_id (a Local Services service ID) and " +
		"vertical_ads_item_group_rule_list (a shared set of this account that decides which items of the attached " +
		"travel feed may serve, unlike the other signals, which only guide). An entry may carry " +
		"exempt_policy_violation_keys: Google then saves the signal despite those policy violations, and it does not " +
		"serve until the policy allows it; Google decides which violations it exempts, and the confirmation names " +
		"the exemption. A search theme is checked by the policy review of Google, which the answer shows as " +
		"approval_status and disapproval_reasons." + assetGroupSignalNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "asset-groups", "asset-group-signals", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, assetGroupSignalSensitivity),
	InputSchema: encoded(assetGroupSignalInputSchema(true)), OutputSchema: encoded(assetGroupSignalOutputSchema),
	Arguments: []capability.Argument{assetGroupCustomerArgument,
		{Name: "asset_group_signals", Required: true, Description: "One to 10 entries, each an object with asset_group " +
			"(customers/ID/assetGroups/N of this account) and exactly one of audience ({audience: customers/ID/audiences/N}), " +
			"search_theme ({text}), local_services_id ({service_id}) and vertical_ads_item_group_rule_list " +
			"({shared_set: customers/ID/sharedSets/N}); optionally exempt_policy_violation_keys"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: assetGroupSignalFields,
	Examples: []capability.Example{{Description: "Validate a search theme signal without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","asset_group_signals":[{"asset_group":` +
			`"customers/1234567890/assetGroups/31","search_theme":{"text":"running shoes"}}],"validate_only":true}`)}},
}

var assetGroupSignalsRemove = capability.Descriptor{
	ID: assetGroupSignalsRemoveID, Version: 1, Title: "Remove signals of Google Ads asset groups",
	Description: "Remove up to 10 signals of Performance Max asset groups of a Google Ads account (one remove " +
		"operation each), each named by its resource name. Only the signal ends; the asset group and the audience stay, " +
		"and the signal can be created again. Offers no response_content_type: a removed signal has no mutable resource " +
		"to show." + assetGroupSignalNote + " Offered by a connection with the delete effect or whose tools list names " +
		"it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "asset-groups", "asset-group-signals", "remove"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, assetGroupSignalSensitivity),
	InputSchema: encoded(assetGroupSignalInputSchema(false)), OutputSchema: encoded(assetGroupSignalOutputSchema),
	Arguments: []capability.Argument{assetGroupCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 signals of this account, each once, " +
			"customers/ID/assetGroupSignals/ASSETGROUPID~CRITERIONID as reports.search returns them"},
		validateOnlyArgument},
	Fields: assetGroupSignalFields,
	Examples: []capability.Example{{Description: "Validate the removal of a signal without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/assetGroupSignals/31~41"],"validate_only":true}`)}},
}

// --- handlers ---

func handleAssetGroupSignalsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetGroupSignalsChange(ctx, resolved, secrets, red, raw, "create asset group signals", assetGroupSignalsCreateID)
}

func handleAssetGroupSignalsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetGroupSignalsChange(ctx, resolved, secrets, red, raw, "remove asset group signals", assetGroupSignalsRemoveID)
}

func assetGroupSignalsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readAssetGroupSignalArguments(op, raw)
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
	var groups, names []string
	if tool == assetGroupSignalsCreateID {
		ops, groups, err = assetGroupSignalCreateOperations(a.CustomerID, a.Signals)
	} else {
		if a.ResponseContentType != "" {
			return nil, badRequest("response_content_type is not offered by " + tool)
		}
		ops, err = assetGroupSignalRemoveOperations(a.CustomerID, a.ResourceNames)
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
		service: assetGroupSignalsService, collection: assetGroupSignalsCollection, resourceKey: "assetGroupSignal",
		operations: ops, names: names, idOf: signalIDOf(a.CustomerID), validateOnly: a.ValidateOnly,
		content: a.ResponseContentType})
	if err != nil {
		return nil, err
	}
	return assetGroupSignalResult(op, a.CustomerID, results, groups, a.ValidateOnly)
}

// --- confirmation ---

// assetGroupSignalSummary phrases the change for the confirmation, from the arguments alone.
func assetGroupSignalSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readAssetGroupSignalArguments("", raw)
		verb := "Create"
		if tool == assetGroupSignalsRemoveID {
			verb = "Remove"
		}
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		var parts []string
		for _, name := range a.ResourceNames[:min(len(a.ResourceNames), 10)] {
			parts = append(parts, boundedText(name, maxCodeText))
		}
		exempt := false
		for _, item := range a.Signals[:min(len(a.Signals), 10)] {
			in, _ := item.(map[string]any)
			part := "asset_group " + boundedText(stringOf(in["asset_group"]), maxCodeText)
			if m, _ := in["audience"].(map[string]any); m != nil {
				part += " audience " + boundedText(stringOf(m["audience"]), maxCodeText)
			}
			if m, _ := in["search_theme"].(map[string]any); m != nil {
				part += " search theme \"" + boundedText(stringOf(m["text"]), maxCodeText) + "\""
			}
			if m, _ := in["local_services_id"].(map[string]any); m != nil {
				part += " local services ID " + boundedText(stringOf(m["service_id"]), maxCodeText)
			}
			if m, _ := in["vertical_ads_item_group_rule_list"].(map[string]any); m != nil {
				part += " vertical ads rule list " + boundedText(stringOf(m["shared_set"]), maxCodeText) + " (selects which feed items may serve)"
			}
			if keys, _ := in["exempt_policy_violation_keys"].([]any); len(keys) > 0 {
				exempt = true
				part += " (policy exemption)"
			}
			parts = append(parts, part)
		}
		total := len(a.Signals) + len(a.ResourceNames)
		more := ""
		if total > len(parts) {
			more = fmt.Sprintf(" and %d more", total-len(parts))
		}
		summary := fmt.Sprintf("%s %d signal(s) of Google Ads account %s: %s%s.", verb, total, a.CustomerID,
			strings.Join(parts, "; "), more)
		if tool == assetGroupSignalsRemoveID {
			return summary + " Only the signal ends; the asset group and the audience stay."
		}
		if exempt {
			summary += " A policy exemption saves the signal although Google found a policy violation; it cannot serve then."
		}
		return summary
	}
}
