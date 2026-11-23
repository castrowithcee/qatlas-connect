package googleads

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/localfile"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	adGroupAdsCreateID = Provider + ".ad-group-ads.create"
	adGroupAdsUpdateID = Provider + ".ad-group-ads.update"
	adGroupAdsRemoveID = Provider + ".ad-group-ads.remove"

	adGroupAdService = "/adGroupAds"
)

const adGroupAdSensitivity = "ads of a Google Ads account: texts, images, videos, final URLs and tracking templates " +
	"that become public once the ad serves"

const (
	adGroupAdNamePattern = `^customers/[0-9]{1,10}/adGroupAds/[0-9]{1,20}~[0-9]{1,20}$`
	policyParameterKey   = "policy_validation_parameter"
)

// adGroupAdIDs splits customers/ID/adGroupAds/G~A of exactly this account into the ad group ID and the ad ID.
// A zero or a temporary (negative) ID never matches.
func adGroupAdIDs(name, customerID string) (group, ad string, ok bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/adGroupAds/")
	if !ok {
		return "", "", false
	}
	group, ad, ok = strings.Cut(rest, "~")
	ok = ok && numberPattern.MatchString(group) && numberPattern.MatchString(ad) && strings.Trim(group, "0") != "" &&
		strings.Trim(ad, "0") != ""
	return group, ad, ok
}

var adGroupAdResourceName = schema{"type": "string", "maxLength": 100, "pattern": adGroupAdNamePattern,
	"description": "Ad of this account, customers/ID/adGroupAds/AD_GROUP_ID~AD_ID"}

type adGroupAdArguments struct {
	CustomerID          string            `json:"customer_id"`
	AdGroupAds          []json.RawMessage `json:"ad_group_ads"`
	ResourceNames       []string          `json:"resource_names"`
	ValidateOnly        bool              `json:"validate_only"`
	ResponseContentType string            `json:"response_content_type"`
}

func readAdGroupAdArguments(op string, raw json.RawMessage) (adGroupAdArguments, error) {
	var a adGroupAdArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

// --- policy validation parameter ---

var adPolicyParameterSchema = schema{"type": "object", "additionalProperties": false, "minProperties": 1,
	"description": "How Google treats policy findings of this operation (PolicyValidationParameter): " +
		"ignorable_policy_topics saves the ad although it has a finding of these topics, exempt_policy_violation_keys " +
		"does so for violation keys; at most one of the two. An exempted ad is saved but cannot serve",
	"properties": schema{
		"ignorable_policy_topics": schema{"type": "array", "minItems": 1, "maxItems": maxAdPolicyTopics,
			"items": schema{"type": "string", "pattern": codePattern.String()},
			"description": "Policy topics, as the PolicyTopicEntry of a rejected change names them, such as " +
				"TRADEMARKS_IN_AD_TEXT; Google documents this for ad policy exemptions"},
		"exempt_policy_violation_keys": schema{"type": "array", "minItems": 1, "maxItems": maxAdPolicyKeys,
			"items": object(schema{
				"policy_name":    schema{"type": "string", "pattern": policyNamePattern.String()},
				"violating_text": schema{"type": "string", "minLength": 1, "maxLength": maxAssetText}}, "policy_name"),
			"description": "Violation keys of a PolicyViolationError, as its details name them; Google documents this " +
				"for keyword policy exemptions and refuses a violation that is not exemptible"}}}

// policyParameter takes policy_validation_parameter out of an item and builds the wire form of the operation.
func policyParameter(in map[string]any, path string) (map[string]any, error) {
	raw, given := in[policyParameterKey]
	delete(in, policyParameterKey)
	if !given {
		return nil, nil
	}
	at := path + policyParameterKey
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, badRequest(at + " must be an object with ignorable_policy_topics or exempt_policy_violation_keys")
	}
	topics, haveTopics := m["ignorable_policy_topics"]
	keys, haveKeys := m["exempt_policy_violation_keys"]
	switch {
	case len(m) > 1 || haveTopics == haveKeys:
		return nil, badRequest(at + " takes exactly one of ignorable_policy_topics and exempt_policy_violation_keys")
	case haveTopics:
		list, ok := topics.([]any)
		if !ok || len(list) == 0 || len(list) > maxAdPolicyTopics {
			return nil, badRequest(fmt.Sprintf("%s.ignorable_policy_topics must hold 1 to %d topics", at, maxAdPolicyTopics))
		}
		out := make([]any, 0, len(list))
		for i, item := range list {
			topic, ok := item.(string)
			if !ok || !codePattern.MatchString(topic) {
				return nil, badRequest(fmt.Sprintf("%s.ignorable_policy_topics[%d] must be a policy topic name such as ALCOHOL", at, i))
			}
			out = append(out, topic)
		}
		return map[string]any{"ignorablePolicyTopics": out}, nil
	}
	list, ok := keys.([]any)
	if !ok || len(list) == 0 || len(list) > maxAdPolicyKeys {
		return nil, badRequest(fmt.Sprintf("%s.exempt_policy_violation_keys must hold 1 to %d keys", at, maxAdPolicyKeys))
	}
	out := make([]any, 0, len(list))
	for i, item := range list {
		here := fmt.Sprintf("%s.exempt_policy_violation_keys[%d]", at, i)
		key, ok := item.(map[string]any)
		name, _ := key["policy_name"].(string)
		if !ok || !policyNamePattern.MatchString(name) {
			return nil, badRequest(here + ".policy_name must be the name of a violated policy")
		}
		wire := map[string]any{"policyName": name}
		for member := range key {
			if member != "policy_name" && member != "violating_text" {
				return nil, badRequest(here + "." + member + " is not an argument here")
			}
		}
		if v, given := key["violating_text"]; given {
			text, ok := v.(string)
			if !ok || text == "" {
				return nil, badRequest(here + ".violating_text must be text")
			}
			if err := plainText(here+".violating_text", text, maxAssetText); err != nil {
				return nil, err
			}
			wire["violatingText"] = text
		}
		out = append(out, wire)
	}
	return map[string]any{"exemptPolicyViolationKeys": out}, nil
}

// --- operations ---

// adRules checks what Google documents about the ad types and the spec cannot say: a responsive search ad
// needs a final URL, and path2 needs path1.
func adRules(ad map[string]any, path string) error {
	for _, kind := range []string{"responsive_search_ad", "local_ad"} {
		m, _ := ad[kind].(map[string]any)
		if m == nil {
			continue
		}
		if _, has := m["path2"]; has && stringOf(m["path1"]) == "" {
			return badRequest(path + kind + ".path2 can be set only when path1 is set")
		}
	}
	if m, ok := ad["image_ad"].(map[string]any); ok {
		_, asset := m["image_asset"]
		_, local := m[localfile.LocalPathArgument]
		_, content := m[localfile.ContentArgument]
		if local {
			return badRequest(path + "image_ad takes no local_path: give content_base64, or an image_asset made by assets.create")
		}
		if asset == content {
			return badRequest(path + "image_ad takes exactly one of image_asset and content_base64")
		}
	}
	if _, rsa := ad["responsive_search_ad"]; rsa {
		if urls, _ := ad["final_urls"].([]any); len(urls) == 0 {
			return badRequest(path + "final_urls needs at least one address for a responsive search ad")
		}
	}
	return nil
}

func adGroupAdItem(items []json.RawMessage, i int) (map[string]any, string, error) {
	path := fmt.Sprintf("ad_group_ads[%d].", i)
	var in map[string]any
	if decodeLoose(items[i], &in) != nil || in == nil {
		return nil, "", badRequest(fmt.Sprintf("ad_group_ads[%d] must be an object", i))
	}
	return in, path, nil
}

// adGroupAdCreateOperations checks every ad to create and builds its operation. Qatlas requires, beyond what
// Google does, the status, so that no ad starts serving by the default of Google, which is ENABLED. The ad
// groups it names are returned for the check of the answer.
func adGroupAdCreateOperations(ctx context.Context, resolved *config.Resolved, customerID string,
	items []json.RawMessage) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAdGroupAds {
		return nil, nil, badRequest("ad_group_ads must hold one to " + strconv.Itoa(maxAdGroupAds) + " ads")
	}
	spec := adGroupAdSpec()
	loader := &mediaLoader{ctx: ctx, resolved: resolved}
	w := &assetCall{customerID: customerID, load: loader.load}
	ops := make([]any, 0, len(items))
	groups := make([]string, 0, len(items))
	for i := range items {
		in, path, err := adGroupAdItem(items, i)
		if err != nil {
			return nil, nil, err
		}
		policy, err := policyParameter(in, path)
		if err != nil {
			return nil, nil, err
		}
		if ad, ok := in["ad"].(map[string]any); ok {
			if err := adRules(ad, path+"ad."); err != nil {
				return nil, nil, err
			}
		}
		wire, _, err := w.object(spec, in, path)
		if err != nil {
			return nil, nil, err
		}
		op := map[string]any{"create": wire}
		if policy != nil {
			op["policyValidationParameter"] = policy
		}
		ops = append(ops, op)
		groups = append(groups, stringOf(wire["adGroup"]))
	}
	return ops, groups, nil
}

// adGroupAdUpdateOperations builds one update with a field mask per ad. Each ad is named once. Google marks
// the ad and its ad group immutable, so only the status, the dates, and the asset automation settings change.
func adGroupAdUpdateOperations(customerID string, items []json.RawMessage) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAdGroupAds {
		return nil, nil, badRequest("ad_group_ads must hold one to " + strconv.Itoa(maxAdGroupAds) + " ads")
	}
	spec := adGroupAdUpdateSpec()
	w := &assetCall{customerID: customerID, update: true}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i := range items {
		in, path, err := adGroupAdItem(items, i)
		if err != nil {
			return nil, nil, err
		}
		name, _ := in["resource_name"].(string)
		if _, _, ok := adGroupAdIDs(name, customerID); !ok {
			return nil, nil, badRequest(path + "resource_name must be customers/" + queryNumber(customerID) +
				"/adGroupAds/AD_GROUP_ID~AD_ID of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("an ad appears in ad_group_ads only once")
		}
		delete(in, "resource_name")
		policy, err := policyParameter(in, path)
		if err != nil {
			return nil, nil, err
		}
		wire, mask, err := w.object(spec, in, path)
		if err != nil {
			return nil, nil, err
		}
		if len(mask) == 0 {
			return nil, nil, badRequest(path + "give at least one member to change")
		}
		wire["resourceName"] = name
		names = append(names, name)
		op := map[string]any{"update": wire, "updateMask": strings.Join(mask, ",")}
		if policy != nil {
			op["policyValidationParameter"] = policy
		}
		ops = append(ops, op)
	}
	return ops, names, nil
}

func adGroupAdRemoveOperations(customerID string, names []string) ([]any, error) {
	if len(names) == 0 || len(names) > maxAdGroupAds {
		return nil, badRequest("resource_names must hold one to " + strconv.Itoa(maxAdGroupAds) + " ads")
	}
	ops := make([]any, 0, len(names))
	for i, name := range names {
		if _, _, ok := adGroupAdIDs(name, customerID); !ok {
			return nil, badRequest(fmt.Sprintf("resource_names[%d] must be customers/%s/adGroupAds/AD_GROUP_ID~AD_ID of "+
				"the account of customer_id", i, queryNumber(customerID)))
		}
		if slices.Contains(names[:i], name) {
			return nil, badRequest("an ad appears in resource_names only once")
		}
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, nil
}

// --- result ---

type (
	adGroupAdResult struct {
		ResourceName string         `json:"resource_name"`
		AdGroupAd    map[string]any `json:"ad_group_ad,omitempty"`
	}
	adGroupAdsChanged struct {
		CustomerID   string            `json:"customer_id"`
		ValidateOnly bool              `json:"validate_only"`
		Results      []adGroupAdResult `json:"results"`
	}
)

// adGroupAdAnswer is the answer of MutateAdGroupAds.
type adGroupAdAnswer struct {
	Results []struct {
		ResourceName string          `json:"resourceName"`
		AdGroupAd    json.RawMessage `json:"adGroupAd"`
	} `json:"results"`
	PartialFailureError json.RawMessage `json:"partialFailureError"`
}

// adGroupAdsOf maps the results of a change. One result per operation, in the order sent, each an ad of the
// account; for an update and a remove it is the ad asked for, for a create it is an ad of the ad group asked
// for. Anything else is an invalid answer with an unknown outcome, since the change may have been made.
func adGroupAdsOf(op string, answer adGroupAdAnswer, customerID string, validateOnly bool, expected, groups []string,
	count int) (*adGroupAdsChanged, error) {
	out := &adGroupAdsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []adGroupAdResult{}}
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
		group, ad, ok := adGroupAdIDs(r.ResourceName, customerID)
		if ok && expected != nil {
			ok = r.ResourceName == expected[i]
		}
		if ok && groups != nil {
			ok = groups[i] == customerName(customerID)+"/adGroups/"+group
		}
		if !ok {
			return nil, invalid("a result for another ad, ad group or account than the one asked for")
		}
		item := adGroupAdResult{ResourceName: r.ResourceName}
		if len(r.AdGroupAd) > 0 && string(r.AdGroupAd) != "null" {
			m, ok := decodeObject(r.AdGroupAd)
			var mapped map[string]any
			if ok {
				mapped, ok = outObject(adGroupAdSpec(), m, customerID)
			}
			if ok {
				ok = stringOf(m["resourceName"]) == "" || stringOf(m["resourceName"]) == r.ResourceName
			}
			if ok && groups != nil && mapped["ad_group"] != nil {
				ok = mapped["ad_group"] == groups[i]
			}
			if ok {
				if inner, has := mapped["ad"].(map[string]any); has && inner["resource_name"] != nil {
					ok = inner["resource_name"] == customerName(customerID)+"/ads/"+ad
				}
			}
			if !ok {
				return nil, invalid("an ad that is not the one asked for")
			}
			item.AdGroupAd = mapped
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptors ---

func adGroupAdInputSchema(create bool) schema {
	var item schema
	if create {
		item = adGroupAdSpec().inputSchema(false)
		item["required"] = []string{"status", "ad_group", "ad"}
		props := item["properties"].(schema)
		props[policyParameterKey] = adPolicyParameterSchema
		// An ad takes image bytes inline only; a local file goes through assets.create.
		image := props["ad"].(schema)["properties"].(schema)["image_ad"].(schema)["properties"].(schema)
		delete(image, localfile.LocalPathArgument)
	} else {
		item = adGroupAdUpdateSpec().inputSchema(true)
		props := item["properties"].(schema)
		props["resource_name"] = adGroupAdResourceName
		props[policyParameterKey] = adPolicyParameterSchema
		item["required"] = []string{"resource_name"}
		item["minProperties"] = 2
	}
	return object(schema{"customer_id": customerIDArgument,
		"ad_group_ads":  schema{"type": "array", "minItems": 1, "maxItems": maxAdGroupAds, "items": item},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "ad_group_ads")
}

var adGroupAdOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "ad_group_ad": adGroupAdSpec().outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results")

var adGroupAdFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per ad, in the order given, with its resource_name and, with " +
		"MUTABLE_RESOURCE, the ad after the change with the members the tool offers; empty after validate_only"}}

var adGroupAdCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit " +
	"customer ID of the account without hyphens; a target of the connection or an account below a manager target"}

const adGroupAdSpendNote = "An ad that is ENABLED serves in its ad group and costs per click or view within the budget " +
	"of the campaign; the call is confirmed per call. Resource names in the arguments (ad group, assets) must belong " +
	"to customer_id. Final URLs and tracking templates send clicks to an address: each must be an https address " +
	"without user info, and the final URL suffix must be parameters, not an address. Up to 10 ads per call, one " +
	"atomic request without partial failure."

const adPolicyNote = " Google checks the ad policy while it validates: with validate_only the answer is the rejection " +
	"with its policy findings, which the error names as topics (and as exemptible policies of a violation), so " +
	"the content can be changed before the ad is created. policy_validation_parameter per ad names the topics " +
	"that Google may save anyway; such an ad cannot serve."

var adGroupAdsCreate = capability.Descriptor{
	ID: adGroupAdsCreateID, Version: 1, Title: "Create Google Ads ads",
	Description: "Create up to 10 ads in ad groups of a Google Ads account (AdGroupAdService.MutateAdGroupAds " +
		"with create operations): Google applies all of them or none. Each entry needs ad_group, status (ENABLED or " +
		"PAUSED: Qatlas has no default, so that no ad starts serving unintended), and ad with exactly one ad type " +
		"member, such as responsive_search_ad, responsive_display_ad, video_ad, app_ad, or a Demand Gen type, nested as " +
		"the Ad resource of v26 nests them; images and videos are assets of the account, named by resource name. " +
		adGroupAdSpendNote + adPolicyNote + " An image ad takes image_asset, or the bytes of an image as content_base64 up " +
		"to 4 MiB, never both; a larger image or a local file goes through assets.create and image_asset. Not offered: members Google fills itself, the deprecated or no longer " +
		"creatable types (expanded_text_ad, legacy_responsive_display_ad, legacy_app_install_ad, shopping_smart_ad; " +
		"call-only ads no longer exist in v26), and the copy source of an image ad." + mutateListListedNote,
	Tags: []string{"google", "ads", "ad-group-ads", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, adGroupAdSensitivity),
	InputSchema: encoded(adGroupAdInputSchema(true)), OutputSchema: encoded(adGroupAdOutputSchema),
	Arguments: []capability.Argument{adGroupAdCustomerArgument,
		{Name: "ad_group_ads", Required: true, Description: "One to 10 ads, each an object with ad_group " +
			"(customers/ID/adGroups/N), status, ad (final_urls, tracking and exactly one ad type member), and optional " +
			"start_date_time, end_date_time, ad_group_ad_asset_automation_settings and policy_validation_parameter"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: adGroupAdFields,
	Examples: []capability.Example{{Description: "Validate a paused responsive search ad, which also runs the policy check, without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ad_group_ads":[{"ad_group":"customers/1234567890/adGroups/31",` +
			`"status":"PAUSED","ad":{"final_urls":["https://www.example.com/shoes"],"responsive_search_ad":{"headlines":` +
			`[{"text":"Shoes"},{"text":"Free delivery"},{"text":"New season"}],"descriptions":[{"text":"Find your pair."},` +
			`{"text":"Order today."}]}}}],"validate_only":true}`)}},
}

var adGroupAdsUpdate = capability.Descriptor{
	ID: adGroupAdsUpdateID, Version: 1, Title: "Change Google Ads ads",
	Description: "Change up to 10 ads of a Google Ads account (AdGroupAdService.MutateAdGroupAds with update " +
		"operations and a field mask that names exactly the members given): Google applies all of them or none. " +
		"Each entry names the ad by resource_name and the members to change: status (ENABLED or PAUSED), " +
		"start_date_time and end_date_time (an empty text clears), and ad_group_ad_asset_automation_settings (the list " +
		"replaces the current one). The ad itself and its ad group are immutable in this call; the content of an ad " +
		"is not changed here. Setting status to ENABLED starts serving. " + adGroupAdSpendNote + adPolicyNote + mutateListListedNote,
	Tags: []string{"google", "ads", "ad-group-ads", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, adGroupAdSensitivity),
	InputSchema: encoded(adGroupAdInputSchema(false)), OutputSchema: encoded(adGroupAdOutputSchema),
	Arguments: []capability.Argument{adGroupAdCustomerArgument,
		{Name: "ad_group_ads", Required: true, Description: "One to 10 entries: resource_name of the ad and at least " +
			"one member to change"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: adGroupAdFields,
	Examples: []capability.Example{{Description: "Pause an ad", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
		`"ad_group_ads":[{"resource_name":"customers/1234567890/adGroupAds/31~41","status":"PAUSED"}]}`)}},
}

var adGroupAdsRemove = capability.Descriptor{
	ID: adGroupAdsRemoveID, Version: 1, Title: "Remove Google Ads ads",
	Description: "Remove up to 10 ads of a Google Ads account (AdGroupAdService.MutateAdGroupAds with remove " +
		"operations): Google applies all of them or none. A removed ad stops serving and cannot be enabled again; " +
		"its history stays readable with reports.search. Resource names must be ads of customer_id. Offers no " +
		"response_content_type: a removed ad has no mutable resource to show." + mutateListListedNote,
	Tags: []string{"google", "ads", "ad-group-ads", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, adGroupAdSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"resource_names": schema{"type": "array", "minItems": 1, "maxItems": maxAdGroupAds, "items": adGroupAdResourceName},
		"validate_only":  validateOnlySchema}, "customer_id", "resource_names")),
	OutputSchema: encoded(adGroupAdOutputSchema),
	Arguments: []capability.Argument{adGroupAdCustomerArgument,
		{Name: "resource_names", Required: true, Description: "One to 10 ads of this account, " +
			"customers/ID/adGroupAds/AD_GROUP_ID~AD_ID"}, validateOnlyArgument},
	Fields: adGroupAdFields,
	Examples: []capability.Example{{Description: "Validate the removal of an ad without removing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource_names":["customers/1234567890/adGroupAds/31~41"],"validate_only":true}`)}},
}

// --- handlers ---

func handleAdGroupAdsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupAdsChange(ctx, resolved, secrets, red, raw, "create ads", adGroupAdsCreateID)
}

func handleAdGroupAdsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupAdsChange(ctx, resolved, secrets, red, raw, "update ads", adGroupAdsUpdateID)
}

func handleAdGroupAdsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return adGroupAdsChange(ctx, resolved, secrets, red, raw, "remove ads", adGroupAdsRemoveID)
}

func adGroupAdsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string) (any, error) {
	a, err := readAdGroupAdArguments(op, raw)
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
	var expected, groups []string
	switch tool {
	case adGroupAdsCreateID:
		ops, groups, err = adGroupAdCreateOperations(ctx, resolved, a.CustomerID, a.AdGroupAds)
	case adGroupAdsUpdateID:
		ops, expected, err = adGroupAdUpdateOperations(a.CustomerID, a.AdGroupAds)
	default:
		ops, err = adGroupAdRemoveOperations(a.CustomerID, a.ResourceNames)
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
	var answer adGroupAdAnswer
	body := mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: a.CustomerID, service: adGroupAdService, body: body},
		&answer); err != nil {
		return nil, err
	}
	return adGroupAdsOf(op, answer, a.CustomerID, a.ValidateOnly, expected, groups, len(ops))
}

// --- confirmation ---

// adGroupAdSummary phrases the change for the confirmation, from the arguments alone.
func adGroupAdSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		a, _ := readAdGroupAdArguments("", raw)
		verb := map[string]string{adGroupAdsCreateID: "Create", adGroupAdsUpdateID: "Change", adGroupAdsRemoveID: "Remove"}[tool]
		if a.ValidateOnly {
			verb = "Only validate: " + strings.ToLower(verb[:1]) + verb[1:]
		}
		parts := slices.Clone(a.ResourceNames)
		clicks, exempt := false, false
		for _, item := range a.AdGroupAds {
			var in map[string]any
			if decodeLoose(item, &in) != nil {
				continue
			}
			part := stringOf(in["resource_name"])
			var facts []string
			if v := stringOf(in["ad_group"]); v != "" {
				facts = append(facts, "ad group "+v)
			}
			if v := stringOf(in["status"]); v != "" {
				facts = append(facts, "status "+v)
			}
			if ad, ok := in["ad"].(map[string]any); ok {
				var kinds []string
				for key := range ad {
					if key != "final_urls" && key != "final_mobile_urls" && key != "tracking_url_template" &&
						key != "final_url_suffix" && key != "url_custom_parameters" && key != "name" &&
						key != "device_preference" && key != "synthetic_content_info" && key != "display_url" &&
						key != "url_collections" && key != "final_app_urls" {
						kinds = append(kinds, key)
					}
				}
				slices.Sort(kinds)
				if len(kinds) > 0 {
					facts = append(facts, "type "+strings.Join(kinds, "+"))
				}
				for _, key := range []string{"final_urls", "final_mobile_urls", "tracking_url_template", "final_url_suffix", "url_collections"} {
					clicks = clicks || ad[key] != nil
				}
			}
			if tool == adGroupAdsUpdateID {
				var keys []string
				for key := range in {
					if key != "resource_name" && key != policyParameterKey {
						keys = append(keys, key)
					}
				}
				slices.Sort(keys)
				facts = append(facts, "members "+strings.Join(keys, "+"))
			}
			if in[policyParameterKey] != nil {
				exempt = true
				facts = append(facts, "policy exemption")
			}
			parts = append(parts, strings.TrimSpace(part+" ("+strings.Join(facts, ", ")+")"))
		}
		summary := verb + " ads of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + "."
		if tool == adGroupAdsRemoveID {
			return summary + " A removed ad cannot be enabled again."
		}
		summary += " An ENABLED ad serves and costs per click or view within the budget of its campaign."
		if clicks {
			summary += " Its final URLs and tracking send clicks to the addresses given."
		}
		if exempt {
			summary += " A policy exemption saves the ad although Google found a policy topic; it cannot serve then."
		}
		return summary
	}
}
