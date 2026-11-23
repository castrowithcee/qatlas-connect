package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	adGroupCriteriaCreateID = Provider + ".ad-group-criteria.create"
	adGroupCriteriaUpdateID = Provider + ".ad-group-criteria.update"
	adGroupCriteriaRemoveID = Provider + ".ad-group-criteria.remove"

	adGroupCriterionService = "/adGroupCriteria"
)

const (
	adGroupCriterionSens = "criteria of ad groups of a Google Ads account: keywords, audiences, placements, bids, tracking templates"
)

var adGroupCriterionNameSchema = schema{"type": "string", "maxLength": 100, "pattern": criterionNamePattern,
	"description": "Ad group criterion of this account, customers/ID/adGroupCriteria/ADGROUPID~CRITERIONID"}

var exemptionKeysSchema = schema{"type": "array", "minItems": 1, "maxItems": maxExemptionKeys,
	"description": "Policy violations of this operation that Google may save the criterion despite: it is saved but does not " +
		"serve until the policy allows it. Each key names a policy; without violating_text the whole policy is exempted",
	"items": object(schema{
		"policy_name":    schema{"type": "string", "minLength": 1, "maxLength": maxNameArgument, "description": "ID of the violated policy"},
		"violating_text": schema{"type": "string", "minLength": 1, "maxLength": maxNameArgument, "description": "Text that violates the policy; left out exempts the whole policy"}},
		"policy_name")}

var criterionIDPattern = regexp.MustCompile(`^([0-9]{1,20})~([0-9]{1,20})$`)

// criterionParts splits a resource name customers/ID/adGroupCriteria/AdGroupID~CriterionID of exactly the
// account into the two IDs, both positive. A temporary ID, which is negative, never matches.
func criterionParts(name, customerID string) (adGroup, criterion string, ok bool) {
	rest, found := strings.CutPrefix(name, customerName(customerID)+"/"+criterionResourcePrefix+"/")
	if !found {
		return "", "", false
	}
	m := criterionIDPattern.FindStringSubmatch(rest)
	if m == nil || strings.Trim(m[1], "0") == "" || strings.Trim(m[2], "0") == "" {
		return "", "", false
	}
	return m[1], m[2], true
}

type adGroupCriterionArguments struct {
	CustomerID          string   `json:"customer_id"`
	Criteria            []any    `json:"ad_group_criteria"`
	ResourceNames       []string `json:"resource_names"`
	ValidateOnly        bool     `json:"validate_only"`
	ResponseContentType string   `json:"response_content_type"`
}

func readAdGroupCriterionArguments(op string, raw json.RawMessage) (adGroupCriterionArguments, error) {
	var a adGroupCriterionArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

// criterionTypeOf returns the one criterion type an object holds, or "" with the count of those it holds.
func criterionTypeOf(in map[string]any) (string, int) {
	found, count := "", 0
	for _, t := range criterionTypes {
		if _, ok := in[t]; ok {
			found = t
			count++
		}
	}
	return found, count
}

// checkCriterionCreate holds the rules of a new criterion that the tree cannot state: exactly one criterion type
// with its required members, a status for a criterion that targets, and nothing a negative criterion cannot take.
func checkCriterionCreate(customerID string, in map[string]any, at string) error {
	if _, found := in["ad_group"]; !found {
		return badRequest(at + ".ad_group is required")
	}
	kind, count := criterionTypeOf(in)
	if count != 1 {
		return badRequest(at + " must hold exactly one criterion type: " + strings.Join(criterionTypes, ", "))
	}
	if object, ok := in[kind].(map[string]any); ok {
		for _, required := range criterionRequired[kind] {
			if _, found := object[required]; !found {
				return badRequest(at + "." + kind + "." + required + " is required")
			}
		}
		if conditions, ok := object["conditions"].([]any); ok {
			for i, c := range conditions {
				condition, _ := c.(map[string]any)
				for _, required := range webpageConditionRequired {
					if _, found := condition[required]; !found {
						return badRequest(fmt.Sprintf("%s.%s.conditions[%d].%s is required", at, kind, i, required))
					}
				}
			}
		}
	}
	if err := checkListingParent(customerID, in, at); err != nil {
		return err
	}
	negative, _ := in["negative"].(bool)
	status, hasStatus := in["status"]
	switch {
	case !negative && !hasStatus:
		return badRequest(at + ".status is required: Qatlas starts no criterion by the default of Google, which is ENABLED")
	case negative && hasStatus && status != "ENABLED":
		return badRequest(at + ".status must be ENABLED or left out for a negative criterion")
	}
	if negative {
		for _, key := range negativeRefused {
			if _, found := in[key]; found {
				return badRequest(at + "." + key + " is not taken by a negative criterion")
			}
		}
	}
	return nil
}

// checkListingParent binds the parent subdivision of a listing group to the account and to the ad group of the
// same entry: it is a criterion of that ad group, with a positive ID, never a temporary one.
func checkListingParent(customerID string, in map[string]any, at string) error {
	group, _ := in["listing_group"].(map[string]any)
	parent, given := group["parent_ad_group_criterion"]
	if !given {
		return nil
	}
	name, _ := parent.(string)
	adGroup, _ := in["ad_group"].(string)
	parentGroup, _, owned := criterionParts(name, customerID)
	if !owned || adGroup[strings.LastIndex(adGroup, "/")+1:] != parentGroup {
		return badRequest(at + ".listing_group.parent_ad_group_criterion must be customers/" + queryNumber(customerID) +
			"/adGroupCriteria/ADGROUPID~CRITERIONID of the ad group of this entry in the account of customer_id; a temporary negative ID is not accepted")
	}
	return nil
}

// exemptionKeys takes the policy exemption keys off one entry, checks them and returns them as the operation
// holds them, with the entry without them. Omitting violating_text exempts the whole policy.
func exemptionKeys(in map[string]any, at string) ([]any, map[string]any, error) {
	raw, given := in["exempt_policy_violation_keys"]
	if !given {
		return nil, in, nil
	}
	rest := make(map[string]any, len(in))
	for key, value := range in {
		if key != "exempt_policy_violation_keys" {
			rest[key] = value
		}
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 || len(list) > maxExemptionKeys {
		return nil, nil, badRequest(at + ".exempt_policy_violation_keys must be a list of 1 to 20 keys")
	}
	keys := make([]any, 0, len(list))
	for i, item := range list {
		path := fmt.Sprintf("%s.exempt_policy_violation_keys[%d]", at, i)
		key, ok := item.(map[string]any)
		if !ok {
			return nil, nil, badRequest(path + " must be an object")
		}
		out := map[string]any{}
		for member, value := range key {
			text, isText := value.(string)
			if (member != "policy_name" && member != "violating_text") || !isText || text == "" || len(text) > maxNameArgument ||
				hasControl(text) {
				return nil, nil, badRequest(path + " takes policy_name and optionally violating_text, each text of 1 to 1024 bytes without control characters")
			}
			out[camel(member)] = text
		}
		if _, found := out["policyName"]; !found {
			return nil, nil, badRequest(path + ".policy_name is required")
		}
		keys = append(keys, out)
	}
	return keys, rest, nil
}

// adGroupCriterionCreateOperations checks every criterion to create and builds its operation. prefixes are the
// ad group IDs the results must belong to.
func adGroupCriterionCreateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAdGroupCriteria {
		return nil, nil, badRequest("ad_group_criteria must hold one to 50 criteria")
	}
	ops := make([]any, 0, len(items))
	adGroups := make([]string, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("ad_group_criteria[%d]", i)}
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		exempt, in, err := exemptionKeys(in, at.snake)
		if err != nil {
			return nil, nil, err
		}
		if err := checkCriterionCreate(customerID, in, at.snake); err != nil {
			return nil, nil, err
		}
		b := builder{customerID: customerID, create: true}
		body, err := b.object(adGroupCriterionTree, in, at)
		if err != nil {
			return nil, nil, err
		}
		adGroup, _ := in["ad_group"].(string)
		adGroups = append(adGroups, adGroup[strings.LastIndex(adGroup, "/")+1:])
		op := map[string]any{"create": body}
		if exempt != nil {
			op["exemptPolicyViolationKeys"] = exempt
		}
		ops = append(ops, op)
	}
	return ops, adGroups, nil
}

// adGroupCriterionUpdateOperations builds one update with a field mask per criterion. Each criterion is named once.
func adGroupCriterionUpdateOperations(customerID string, items []any) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAdGroupCriteria {
		return nil, nil, badRequest("ad_group_criteria must hold one to 50 criteria")
	}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i, item := range items {
		in, ok := item.(map[string]any)
		at := pathOfMember{snake: fmt.Sprintf("ad_group_criteria[%d]", i)}
		if !ok {
			return nil, nil, badRequest(at.snake + " must be an object")
		}
		exempt, in, err := exemptionKeys(in, at.snake)
		if err != nil {
			return nil, nil, err
		}
		name, _ := in["resource_name"].(string)
		if _, _, owned := criterionParts(name, customerID); !owned {
			return nil, nil, badRequest(at.snake + ".resource_name must be customers/" + queryNumber(customerID) +
				"/adGroupCriteria/ADGROUPID~CRITERIONID of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("a criterion appears in ad_group_criteria only once")
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
		body, err := b.object(adGroupCriterionTree, fields, at)
		if err != nil {
			return nil, nil, err
		}
		body["resourceName"] = name
		names = append(names, name)
		op := map[string]any{"update": body, "updateMask": strings.Join(b.mask, ",")}
		if exempt != nil {
			op["exemptPolicyViolationKeys"] = exempt
		}
		ops = append(ops, op)
	}
	return ops, names, nil
}

func adGroupCriterionRemoveOperations(customerID string, resourceNames []string) ([]any, error) {
	if len(resourceNames) == 0 || len(resourceNames) > maxAdGroupCriteria {
		return nil, badRequest("resource_names must hold one to 50 criteria")
	}
	ops := make([]any, 0, len(resourceNames))
	for i, name := range resourceNames {
		if _, _, owned := criterionParts(name, customerID); !owned {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/adGroupCriteria/ADGROUPID~CRITERIONID "+
				"of the account of customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(resourceNames[:i], name) {
			return nil, badRequest("a criterion appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	adGroupCriterionResult struct {
		ResourceName     string         `json:"resource_name"`
		AdGroupCriterion map[string]any `json:"ad_group_criterion,omitempty"`
	}
	adGroupCriteriaChanged struct {
		CustomerID   string                   `json:"customer_id"`
		ValidateOnly bool                     `json:"validate_only"`
		Results      []adGroupCriterionResult `json:"results"`
	}
)

// adGroupCriterionAnswer is the answer of MutateAdGroupCriteria.
type adGroupCriterionAnswer struct {
	Results []struct {
		ResourceName     string          `json:"resourceName"`
		AdGroupCriterion json.RawMessage `json:"adGroupCriterion"`
	} `json:"results"`
	PartialFailureError json.RawMessage `json:"partialFailureError"`
}

// expectation says which criteria the results must be: the names asked for, or for a creation the ad group of
// each criterion.
type expectation struct {
	names, adGroups []string
}

// adGroupCriteriaOf maps the results of a change. One result per operation, in the order sent, each a criterion
// of the account; for an update and a remove it is the criterion asked for, for a create one of the ad group
// asked for. Anything else is an invalid answer with an unknown outcome, since the change may have been made.
func adGroupCriteriaOf(op string, answer adGroupCriterionAnswer, customerID string, validateOnly bool, want expectation,
	count int) (*adGroupCriteriaChanged, error) {
	out := &adGroupCriteriaChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []adGroupCriterionResult{}}
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
		adGroup, _, owned := criterionParts(r.ResourceName, customerID)
		if !owned || (want.names != nil && r.ResourceName != want.names[i]) ||
			(want.adGroups != nil && adGroup != want.adGroups[i]) {
			return nil, invalid("a result for another criterion, ad group or account than the one asked for")
		}
		item := adGroupCriterionResult{ResourceName: r.ResourceName}
		if len(r.AdGroupCriterion) > 0 && string(r.AdGroupCriterion) != "null" {
			raw, ok := decodeObject(r.AdGroupCriterion)
			if ok {
				var members map[string]any
				if members, ok = outputMembers(adGroupCriterionOutputTree.fields, raw, customerID); ok {
					name, _ := members["resource_name"].(string)
					ok = (name == "" || name == r.ResourceName) && listingParentOwned(members, customerID)
					item.AdGroupCriterion = members
				}
			}
			if !ok {
				return nil, invalid("a criterion that is not the one asked for")
			}
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// listingParentOwned reports whether the parent of a returned listing group, if any, is a criterion of the account.
func listingParentOwned(members map[string]any, customerID string) bool {
	group, _ := members["listing_group"].(map[string]any)
	parent, given := group["parent_ad_group_criterion"].(string)
	if !given || parent == "" {
		return true
	}
	_, _, owned := criterionParts(parent, customerID)
	return owned
}

// --- descriptors ---

func adGroupCriterionInputSchema(create bool) schema {
	var item schema
	if create {
		item = adGroupCriterionTree.inputSchema()
		item["required"] = []string{"ad_group"}
		delete(item, "description")
		props := item["properties"].(schema)
		for kind, members := range criterionRequired {
			props[kind].(schema)["required"] = members
		}
		conditions := props["webpage"].(schema)["properties"].(schema)["conditions"].(schema)
		conditions["items"].(schema)["required"] = webpageConditionRequired
		props["exempt_policy_violation_keys"] = exemptionKeysSchema
	} else {
		item = adGroupCriterionTree.mutable().inputSchema()
		props := item["properties"].(schema)
		props["resource_name"] = adGroupCriterionNameSchema
		props["exempt_policy_violation_keys"] = exemptionKeysSchema
		item["required"] = []string{"resource_name"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"ad_group_criteria": schema{"type": "array", "minItems": 1, "maxItems": maxAdGroupCriteria, "items": item},
		"validate_only":     validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "ad_group_criteria")
}

var adGroupCriterionOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "ad_group_criterion": adGroupCriterionOutputTree.outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results")

var adGroupCriterionFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per criterion, in the order given, with its resource_name and, with " +
		"MUTABLE_RESOURCE, the criterion after the change with the members the tool offers; empty after validate_only"}}

const adGroupCriterionSpendNote = "An entry may carry exempt_policy_violation_keys (up to 20): Google then saves the criterion despite those " +
	"policy violations, and it does not serve until the policy allows it; the confirmation names the exemption. A criterion that targets and is ENABLED serves in its ad group, and its bids and bid modifier decide " +
	"how much each click or view costs; the call is confirmed per call. Resource names in the arguments (ad_group, user_list, " +
	"audience and the others of this account) must belong to customer_id; the constants of Google (location, language, topic, " +
	"mobile app category) are checked for their form. The tracking template and the final URL suffix can pass click data to a " +
	"third party, so the template and the final URLs must be https addresses without user info and the suffix must be " +
	"parameters, not an address. Up to 50 criteria per call, one atomic request without partial failure."

var adGroupCriterionCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the " +
	"account without hyphens; a target of the connection or an account below a manager target"}

var adGroupCriteriaCreate = capability.Descriptor{
	ID: adGroupCriteriaCreateID, Version: 1, Title: "Create Google Ads ad group criteria",
	Description: "Create up to 50 criteria in the ad groups of a Google Ads account (AdGroupCriterionService." +
		"MutateAdGroupCriteria with create operations): Google applies all of them or none. Each criterion names its ad group, " +
		"exactly one criterion type (keyword, placement, audience, location, age range and the others of the " +
		"AdGroupCriterion resource of v26), and a status, ENABLED or PAUSED (Qatlas has no default, so that no criterion " +
		"starts serving unintended); a negative criterion (negative true) excludes and takes no status, bid or URL. " +
		adGroupCriterionSpendNote + " Not offered: members Google fills itself, and " +
		"a new tree with a parent and its children in one request." + mutateNote,
	Tags: []string{"google", "ads", "ad-group-criteria", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, adGroupCriterionSens),
	InputSchema: encoded(adGroupCriterionInputSchema(true)), OutputSchema: encoded(adGroupCriterionOutputSchema),
	Arguments: []capability.Argument{adGroupCriterionCustomerArgument,
		{Name: "ad_group_criteria", Required: true, Description: "One to 50 criteria, each an object of the members of the " +
			"AdGroupCriterion resource: ad_group, one criterion type, status (not for a negative criterion) and the optional rest"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: adGroupCriterionFields,
	Examples: []capability.Example{{Description: "Validate a paused exact keyword with a CPC bid without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ad_group_criteria":[{"ad_group":"customers/1234567890/adGroups/31",` +
			`"status":"PAUSED","keyword":{"text":"running shoes","match_type":"EXACT"},"cpc_bid_micros":750000}],"validate_only":true}`)}},
}

var adGroupCriteriaUpdate = capability.Descriptor{
	ID: adGroupCriteriaUpdateID, Version: 1, Title: "Change Google Ads ad group criteria",
	Description: "Change up to 50 criteria of the ad groups of a Google Ads account (AdGroupCriterionService." +
		"MutateAdGroupCriteria with update operations and a field mask that names exactly the members given): Google " +
		"applies all of them or none. Each entry names the criterion by resource_name and the members to change; a list " +
		"member replaces the whole list. The ad group, the sign (negative) and the criterion itself are fixed at creation " +
		"and are not offered, and Google does not update a negative criterion. Setting status to ENABLED starts " +
		"serving, and setting a bid or bid modifier changes the spend. " + adGroupCriterionSpendNote + mutateNote,
	Tags: []string{"google", "ads", "ad-group-criteria", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, adGroupCriterionSens),
	InputSchema: encoded(adGroupCriterionInputSchema(false)), OutputSchema: encoded(adGroupCriterionOutputSchema),
	Arguments: []capability.Argument{adGroupCriterionCustomerArgument,
		{Name: "ad_group_criteria", Required: true, Description: "One to 50 entries: resource_name of the criterion and at " +
			"least one member to change"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: adGroupCriterionFields,
	Examples: []capability.Example{{Description: "Pause a criterion", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
		`"ad_group_criteria":[{"resource_name":"customers/1234567890/adGroupCriteria/31~41","status":"PAUSED"}]}`)}},
}

var adGroupCriteriaRemove = capability.Descriptor{
	ID: adGroupCriteriaRemoveID, Version: 1, Title: "Remove Google Ads ad group criteria",
	Description: "Remove up to 50 criteria of the ad groups of a Google Ads account (AdGroupCriterionService." +
		"MutateAdGroupCriteria with remove operations): Google applies all of them or none. A removed criterion stops " +
		"serving, or, for a negative criterion, excluding, and is not enabled again: it is created anew. Its history stays " +
		"readable with reports.search. Resource names must be criteria of customer_id. Offers no response_content_type: " +
		"a removed criterion has no mutable resource to show." + mutateNote,
	Tags: []string{"google", "ads", "ad-group-criteria", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, adGroupCriterionSens),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxAdGroupCriteria, "items": adGroupCriterionNameSchema},
		"validate_only":  validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(adGroupCriterionOutputSchema),
	Arguments: []capability.Argument{adGroupCriterionCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 50 criteria of this account, " +
			"customers/ID/adGroupCriteria/ADGROUPID~CRITERIONID"}, validateOnlyArgument},
	Fields: adGroupCriterionFields,
	Examples: []capability.Example{{Description: "Validate the removal of a criterion without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/adGroupCriteria/31~41"],"validate_only":true}`)}},
}

// --- handlers ---

func handleAdGroupCriteriaCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupCriteriaChange(ctx, resolved, secrets, red, raw, "create ad group criteria", adGroupCriteriaCreateID)
}

func handleAdGroupCriteriaUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupCriteriaChange(ctx, resolved, secrets, red, raw, "update ad group criteria", adGroupCriteriaUpdateID)
}

func handleAdGroupCriteriaRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupCriteriaChange(ctx, resolved, secrets, red, raw, "remove ad group criteria", adGroupCriteriaRemoveID)
}

func adGroupCriteriaChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readAdGroupCriterionArguments(op, raw)
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
	var want expectation
	switch tool {
	case adGroupCriteriaCreateID:
		ops, want.adGroups, err = adGroupCriterionCreateOperations(a.CustomerID, a.Criteria)
	case adGroupCriteriaUpdateID:
		ops, want.names, err = adGroupCriterionUpdateOperations(a.CustomerID, a.Criteria)
	default:
		ops, err = adGroupCriterionRemoveOperations(a.CustomerID, a.ResourceNames)
		want.names = a.ResourceNames
	}
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer adGroupCriterionAnswer
	body := mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: adGroupCriterionService, body: body},
		&answer); err != nil {
		return nil, err
	}
	return adGroupCriteriaOf(op, answer, a.CustomerID, a.ValidateOnly, want, len(ops))
}

// --- confirmation ---

// adGroupCriterionSummary phrases the change for the confirmation, from the arguments alone.
func adGroupCriterionSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readAdGroupCriterionArguments("", raw)
		verb := map[string]string{adGroupCriteriaCreateID: "Create", adGroupCriteriaUpdateID: "Change",
			adGroupCriteriaRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		var parts []string
		if tool == adGroupCriteriaRemoveID {
			parts = a.ResourceNames
		}
		for _, item := range a.Criteria {
			in, _ := item.(map[string]any)
			part := stringOf(in["resource_name"])
			kind, _ := criterionTypeOf(in)
			if kind != "" {
				part = kind
				if negative, _ := in["negative"].(bool); negative {
					part = "negative " + kind
				}
			}
			var facts []string
			for _, key := range []string{"status", "ad_group"} {
				if v := stringOf(in[key]); v != "" {
					facts = append(facts, key+" "+v)
				}
			}
			var keys []string
			for key := range in {
				if strings.HasSuffix(key, "_bid_micros") || key == "bid_modifier" {
					keys = append(keys, key)
				}
			}
			slices.Sort(keys)
			facts = append(facts, keys...)
			if keys, _ := in["exempt_policy_violation_keys"].([]any); len(keys) > 0 {
				var names []string
				for _, k := range keys {
					key, _ := k.(map[string]any)
					names = append(names, stringOf(key["policy_name"]))
				}
				facts = append(facts, "POLICY EXEMPTION requested for "+strings.Join(names, "+"))
			}
			if tool == adGroupCriteriaUpdateID {
				var changed []string
				for key := range in {
					if key != "resource_name" && key != "exempt_policy_violation_keys" {
						changed = append(changed, key)
					}
				}
				slices.Sort(changed)
				facts = append(facts, "members "+strings.Join(changed, "+"))
			}
			parts = append(parts, part+" ("+strings.Join(facts, ", ")+")")
		}
		summary := verb + " ad group criteria of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + "."
		if tool == adGroupCriteriaRemoveID {
			return summary + " A removed criterion is not enabled again."
		}
		return summary + " An ENABLED criterion serves and bids within the budget of its campaign; its bids and bid modifier decide how much it costs."
	}
}
