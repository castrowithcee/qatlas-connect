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
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// ads.update changes the content of existing ads (AdService.MutateAds, which has update operations only). It
// reuses the ad spec of ad-group-ads.create with the members Google marks immutable left out: the resource
// name (it names the ad), name, and the types text_ad, expanded_dynamic_search_ad and image_ad. Google also
// states that MutateAds cannot update TextAd, ExpandedDynamicSearchAd, GmailAd and ImageAd.

const (
	adsUpdateID = Provider + ".ads.update"

	adService = "/ads"
	maxAds    = 10
)

const adSensitivity = "ads of a Google Ads account: texts, images, videos, final URLs and tracking templates " +
	"that become public once the ad serves"

// immutableAdMembers are the members of the ad that no update may name.
var immutableAdMembers = []string{"name", "text_ad", "expanded_dynamic_search_ad", "image_ad"}

// adUpdateSpec is what an update of an ad may name. A type member is masked by the paths of its members, so
// that a change of the headlines leaves the descriptions as they are.
func adUpdateSpec() *assetSpec {
	out := &assetSpec{}
	for _, f := range updatable(adSpec()).fields {
		if slices.Contains(immutableAdMembers, f.name) {
			continue
		}
		if f.group == "ad_data" {
			f.typeMember = true
		}
		out.fields = append(out.fields, f)
	}
	return out
}

var adResourceName = schema{"type": "string", "maxLength": 100, "pattern": `^customers/[0-9]{1,10}/ads/[0-9]{1,20}$`,
	"description": "Ad of this account, customers/ID/ads/AD_ID"}

type adArguments struct {
	CustomerID          string            `json:"customer_id"`
	Ads                 []json.RawMessage `json:"ads"`
	ValidateOnly        bool              `json:"validate_only"`
	ResponseContentType string            `json:"response_content_type"`
}

func readAdArguments(op string, raw json.RawMessage) (adArguments, error) {
	var a adArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, failure(op, "the validated arguments could not be read")
	}
	return a, nil
}

// adUpdateOperations builds one update with a field mask per ad. Each ad is named once and must be an ad of the
// account.
func adUpdateOperations(customerID string, items []json.RawMessage) ([]any, []string, error) {
	if len(items) == 0 || len(items) > maxAds {
		return nil, nil, badRequest("ads must hold one to " + strconv.Itoa(maxAds) + " ads")
	}
	spec := adUpdateSpec()
	w := &assetCall{customerID: customerID, update: true}
	ops := make([]any, 0, len(items))
	names := make([]string, 0, len(items))
	for i := range items {
		path := fmt.Sprintf("ads[%d].", i)
		var in map[string]any
		if decodeLoose(items[i], &in) != nil || in == nil {
			return nil, nil, badRequest(fmt.Sprintf("ads[%d] must be an object", i))
		}
		name, _ := in["resource_name"].(string)
		if !ownedResource(name, customerID, "ads") {
			return nil, nil, badRequest(path + "resource_name must be customers/" + queryNumber(customerID) +
				"/ads/AD_ID of the account of customer_id")
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("an ad appears in ads only once")
		}
		delete(in, "resource_name")
		policy, err := policyParameter(in, path)
		if err != nil {
			return nil, nil, err
		}
		for _, kind := range adTypeMembers(spec) {
			if m, given := in[kind]; given {
				if fields, ok := m.(map[string]any); ok && len(fields) == 0 {
					return nil, nil, badRequest(path + kind + " must name at least one member to change")
				}
			}
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

func adTypeMembers(s *assetSpec) []string {
	var out []string
	for _, f := range s.fields {
		if f.group == "ad_data" {
			out = append(out, f.name)
		}
	}
	return out
}

// --- result ---

type (
	adResult struct {
		ResourceName string         `json:"resource_name"`
		Ad           map[string]any `json:"ad,omitempty"`
	}
	adsChanged struct {
		CustomerID   string     `json:"customer_id"`
		ValidateOnly bool       `json:"validate_only"`
		Results      []adResult `json:"results"`
	}
)

// adAnswer is the answer of MutateAds.
type adAnswer struct {
	Results []struct {
		ResourceName string          `json:"resourceName"`
		Ad           json.RawMessage `json:"ad"`
	} `json:"results"`
	PartialFailureError json.RawMessage `json:"partialFailureError"`
}

// adsOf maps the results of an update. One result per operation, in the order sent, each the ad asked for.
// Anything else is an invalid answer with an unknown outcome, since the change may have been made.
func adsOf(op string, answer adAnswer, customerID string, validateOnly bool, expected []string) (*adsChanged, error) {
	out := &adsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []adResult{}}
	invalid := func(what string) error {
		return unsure(provider.InvalidResponse(op, subject+" answered with "+what), true)
	}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, invalid("a partial failure although none was asked for")
	}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != len(expected) {
		return nil, invalid("another number of results than operations")
	}
	for i, r := range answer.Results {
		if !ownedResource(r.ResourceName, customerID, "ads") || r.ResourceName != expected[i] {
			return nil, invalid("a result for another ad or account than the one asked for")
		}
		item := adResult{ResourceName: r.ResourceName}
		if len(r.Ad) > 0 && string(r.Ad) != "null" {
			m, ok := decodeObject(r.Ad)
			var mapped map[string]any
			if ok {
				mapped, ok = outObject(adSpec(), m, customerID)
			}
			if ok {
				ok = mapped["resource_name"] == nil || mapped["resource_name"] == r.ResourceName
			}
			if !ok {
				return nil, invalid("an ad that is not the one asked for")
			}
			item.Ad = mapped
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- descriptor ---

func adInputSchema() schema {
	item := adUpdateSpec().inputSchema(true)
	props := item["properties"].(schema)
	props["resource_name"] = adResourceName
	props[policyParameterKey] = adPolicyParameterSchema
	item["required"] = []string{"resource_name"}
	item["minProperties"] = 2
	return object(schema{"customer_id": customerIDArgument,
		"ads":           schema{"type": "array", "minItems": 1, "maxItems": maxAds, "items": item},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "ads")
}

var adOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "ad": adSpec().outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results")

var adsUpdate = capability.Descriptor{
	ID: adsUpdateID, Version: 1, Title: "Change the content of Google Ads ads",
	Description: "Change the content of up to 10 existing ads of a Google Ads account (AdService.MutateAds with " +
		"update operations and a field mask that names exactly the members given): Google applies all of them or " +
		"none. Each entry names the ad by resource_name (customers/ID/ads/AD_ID) and the members to change: " +
		"final_urls, final_mobile_urls, tracking_url_template, final_url_suffix, url_custom_parameters, " +
		"url_collections, final_app_urls, display_url, device_preference, synthetic_content_info, or the members of " +
		"the one ad type the ad has, nested as the Ad resource of v26 nests them; a list member replaces the whole " +
		"list. Not offered: the name of the ad and the types text_ad, expanded_dynamic_search_ad and image_ad, " +
		"which Google marks immutable (it refuses to update text, expanded dynamic search, Gmail and image ads), the " +
		"deprecated and legacy types, the image bytes, and the members Google fills itself; status and dates belong " +
		"to the ad group ad, see ad-group-ads.update. A changed ad goes through the policy review again and serves " +
		"with the new content once it passes: the content of an ad that serves is public at once. Resource names in " +
		"the arguments (assets) must belong to customer_id. Final URLs and tracking templates send clicks to an " +
		"address: each must be an https address without user info, and the final URL suffix must be parameters, not " +
		"an address. Up to 10 ads per call, one atomic request without partial failure." + adUpdatePolicyNote + mutateNote,
	Tags: []string{"google", "ads", "ad-content", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, adSensitivity),
	InputSchema: encoded(adInputSchema()), OutputSchema: encoded(adOutputSchema),
	Arguments: []capability.Argument{adGroupAdCustomerArgument,
		{Name: "ads", Required: true, Description: "One to 10 entries: resource_name of the ad and at least one " +
			"member to change, and optional policy_validation_parameter"}, validateOnlyArgument, responseContentTypeArgument},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One entry per ad, in the order given, with its resource_name and, with " +
			"MUTABLE_RESOURCE, the ad after the change with the members the tool offers; empty after validate_only"}},
	Examples: []capability.Example{{Description: "Validate a new final URL for an ad, which also runs the policy check, without changing it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ads":[{"resource_name":"customers/1234567890/ads/41",` +
			`"final_urls":["https://www.example.com/sale"]}],"validate_only":true}`)}},
}

const adUpdatePolicyNote = " Google checks the ad policy while it validates: with validate_only the answer is the " +
	"rejection with its policy findings, which the error names as topics (and as exemptible policies of a " +
	"violation), so the content can be changed before the ad is. policy_validation_parameter per ad names the " +
	"topics that Google may save anyway; such an ad cannot serve."

// --- handler ---

func handleAdsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update ads"
	a, err := readAdArguments(op, raw)
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
	ops, expected, err := adUpdateOperations(a.CustomerID, a.Ads)
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: adsUpdateID}
	c, err := env.settle(a.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer adAnswer
	body := mutateOperationsBody(ops, a.ValidateOnly, a.ResponseContentType)
	if err := c.mutate(ctx, mutation{op: op, tool: adsUpdateID, customerID: a.CustomerID, service: adService, body: body},
		&answer); err != nil {
		return nil, err
	}
	return adsOf(op, answer, a.CustomerID, a.ValidateOnly, expected)
}

// --- confirmation ---

// adUpdateSummary phrases the change for the confirmation, from the arguments alone.
func adUpdateSummary(raw json.RawMessage) string {
	a, _ := readAdArguments("", raw)
	verb := "Change"
	if a.ValidateOnly {
		verb = "Only validate: change"
	}
	var parts []string
	clicks, exempt := false, false
	for _, item := range a.Ads {
		var in map[string]any
		if decodeLoose(item, &in) != nil {
			continue
		}
		var keys []string
		for key := range in {
			if key != "resource_name" && key != policyParameterKey {
				keys = append(keys, key)
			}
			switch key {
			case "final_urls", "final_mobile_urls", "tracking_url_template", "final_url_suffix", "url_collections":
				clicks = true
			}
		}
		slices.Sort(keys)
		facts := []string{"members " + strings.Join(keys, "+")}
		if in[policyParameterKey] != nil {
			exempt = true
			facts = append(facts, "policy exemption")
		}
		parts = append(parts, stringOf(in["resource_name"])+" ("+strings.Join(facts, ", ")+")")
	}
	summary := verb + " the content of ads of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		". A changed ad goes through the policy review again and serves with the new content once it passes."
	if clicks {
		summary += " Its final URLs and tracking send clicks to the addresses given."
	}
	if exempt {
		summary += " A policy exemption saves the ad although Google found a policy topic; it cannot serve then."
	}
	return summary
}
