package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// ApplyRecommendationOperation carries one optional parameter message out of a oneof. Qatlas Connect offers
// the variants whose parameters are numbers, enums and plain ad text, and checks every address in them against
// the rule for tracking addresses: https without user info. What it leaves out is listed in the provider
// documentation:
//
//   - tracking_url_template, final_url_suffix, url_custom_parameters, final_mobile_urls, final_app_urls,
//     url_collections and display_url of ads and assets, which send clicks or signals elsewhere;
//   - the lead form variant, whose assets can deliver leads to a webhook;
//   - text_ad, callout_extension, call_extension and sitelink_extension, the legacy variants (the reference
//     names no recommendation type for the last three, and expanded text ads are retired);
//   - the call tracking members of call assets.

type recApplyOperation struct {
	ResourceName string                     `json:"resource_name"`
	Parameters   map[string]json.RawMessage `json:"parameters"`
}

type applyArguments struct {
	CustomerID string              `json:"customer_id"`
	Operations []recApplyOperation `json:"operations"`
}

// recNumberField is one numeric member of a parameter message: integer micros (at least 1) or a float within
// bounds.
type recNumberField struct {
	name     string
	micros   bool
	lo, hi   float64
	loX, hiX bool
	required bool
}

// variant is one member of the oneof apply_parameters. fields describe a numeric message; build, when set,
// builds a message of text, enums and names instead. schema is the schema of the member.
type recVariant struct {
	name       string
	fields     []recNumberField
	atLeastOne bool
	schema     schema
	build      func(customerID string, raw json.RawMessage) (map[string]any, error)
}

// recCamelCase turns a name of the reference into the member name of the JSON mapping.
func recCamelCase(name string) string {
	parts := strings.Split(name, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func recMicrosField(name string, required bool) recNumberField {
	return recNumberField{name: name, micros: true, required: required}
}

// roas is a target ROAS: the reference gives 0.01 to 1000.0, inclusive.
func recRoasField(name string) recNumberField { return recNumberField{name: name, lo: 0.01, hi: 1000} }

// multiplier is a factor above zero; the reference bounds some of them further, which the caller passes in.
func recMultiplierField(name string, lo, hi float64, loX, hiX bool) recNumberField {
	return recNumberField{name: name, lo: lo, hi: hi, loX: loX, hiX: hiX, required: true}
}

func (v recVariant) numbers(raw json.RawMessage) (map[string]any, error) {
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil || members == nil {
		return nil, badRequest("parameters." + v.name + " must be an object")
	}
	known := map[string]recNumberField{}
	for _, f := range v.fields {
		known[f.name] = f
	}
	for name := range members {
		if _, ok := known[name]; !ok {
			return nil, badRequest("parameters." + v.name + "." + name + " is not a parameter of this variant")
		}
	}
	out := map[string]any{}
	for _, f := range v.fields {
		member, ok := members[f.name]
		if !ok {
			if f.required {
				return nil, badRequest("parameters." + v.name + "." + f.name + " is required")
			}
			continue
		}
		path := "parameters." + v.name + "." + f.name
		if f.micros {
			var n int64
			if json.Unmarshal(member, &n) != nil || n < 1 || n > recMaxMicros {
				return nil, badRequest(path + " must be a whole amount in micros, at least 1")
			}
			out[recCamelCase(f.name)] = n
			continue
		}
		var x float64
		if json.Unmarshal(member, &x) != nil {
			return nil, badRequest(path + " must be a number")
		}
		if err := recCheckFinite(path, x, f.lo, f.hi, f.loX, f.hiX); err != nil {
			return nil, err
		}
		out[recCamelCase(f.name)] = x
	}
	if v.atLeastOne && len(out) == 0 {
		return nil, badRequest("parameters." + v.name + " needs at least one of its parameters")
	}
	return out, nil
}

func recNumericVariant(name string, atLeastOne bool, fields ...recNumberField) recVariant {
	properties := schema{}
	var required []string
	for _, f := range fields {
		if f.micros {
			properties[f.name] = recMicrosSchema()
		} else {
			properties[f.name] = schema{"type": "number", "minimum": f.lo, "maximum": f.hi}
		}
		if f.required {
			required = append(required, f.name)
		}
	}
	v := recVariant{name: name, fields: fields, atLeastOne: atLeastOne, schema: object(properties, required...)}
	if atLeastOne {
		v.schema["minProperties"] = 1
	}
	return v
}

// recStrictDecode reads a JSON object into out and refuses a member out would not hold.
func recStrictDecode(raw []byte, out any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out) == nil
}

func recOwnedNames(argument, customerID, collection string, names []string) error {
	for _, name := range names {
		if !ownedName(name, customerID, collection) {
			return badRequest(argument + " must name objects of the account of customer_id, customers/CUSTOMER_ID/" +
				collection + "/ID")
		}
	}
	return nil
}

func recOwnedSchema(collection string) schema {
	return schema{"type": "string", "pattern": "^customers/[0-9]{1,10}/" + collection + "/[0-9]{1,20}$"}
}

// recFinalURLs checks the final URLs of an ad or asset: https without user info. They are ad content that Qatlas
// Connect never fetches.
func recFinalURLs(argument string, urls []string) error {
	if len(urls) > 10 {
		return badRequest(argument + " takes at most 10 addresses")
	}
	for _, u := range urls {
		if u == "" {
			return badRequest(argument + " must not hold an empty address")
		}
		if err := checkAddress(argument, u); err != nil {
			return err
		}
	}
	return nil
}

var recPhonePattern = regexp.MustCompile(`^[0-9+()./ -]{3,32}$`)

const recPhonePatternText = `^[0-9+()./ -]{3,32}$`

// --- asset variants ---

var recApplyScopes = []string{"CUSTOMER", "CAMPAIGN"}

// recAssetVariant builds a variant that attaches assets: the new assets of one kind and existing ones, at a scope.
// The member ad_asset_apply_parameters of the reference is flattened into the variant.
func recAssetVariant(name string, newAsset schema, buildAsset func(customerID string, raw json.RawMessage) (map[string]any, error)) recVariant {
	return recVariant{name: name,
		schema: object(schema{"new_assets": recListOf(newAsset, 20), "existing_assets": recListOf(recOwnedSchema("assets"), 20),
			"scope": recEnumSchema(recApplyScopes)}, "scope"),
		build: func(customerID string, raw json.RawMessage) (map[string]any, error) {
			var a struct {
				NewAssets      []json.RawMessage `json:"new_assets"`
				ExistingAssets []string          `json:"existing_assets"`
				Scope          string            `json:"scope"`
			}
			if !recStrictDecode(raw, &a) {
				return nil, badRequest("parameters." + name + " holds an unknown member or a wrong type")
			}
			if !slices.Contains(recApplyScopes, a.Scope) {
				return nil, badRequest("parameters." + name + ".scope must be CUSTOMER or CAMPAIGN")
			}
			if len(a.NewAssets)+len(a.ExistingAssets) == 0 || len(a.NewAssets) > 20 || len(a.ExistingAssets) > 20 {
				return nil, badRequest("parameters." + name + " needs 1 to 20 new_assets or existing_assets each")
			}
			if err := recOwnedNames("parameters."+name+".existing_assets", customerID, "assets", a.ExistingAssets); err != nil {
				return nil, err
			}
			params := map[string]any{"scope": a.Scope}
			if len(a.ExistingAssets) > 0 {
				params["existingAssets"] = a.ExistingAssets
			}
			if len(a.NewAssets) > 0 {
				assets := make([]any, 0, len(a.NewAssets))
				for _, raw := range a.NewAssets {
					asset, err := buildAsset(customerID, raw)
					if err != nil {
						return nil, err
					}
					assets = append(assets, asset)
				}
				params["newAssets"] = assets
			}
			return map[string]any{"adAssetApplyParameters": params}, nil
		}}
}

func recCalloutAsset(_ string, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		CalloutText string `json:"callout_text"`
	}
	if !recStrictDecode(raw, &a) {
		return nil, badRequest("a callout asset holds only callout_text")
	}
	if err := recCheckText("callout_text", a.CalloutText, recMaxText); err != nil {
		return nil, err
	}
	return map[string]any{"calloutAsset": map[string]any{"calloutText": a.CalloutText}}, nil
}

func recCallAsset(_ string, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		CountryCode string `json:"country_code"`
		PhoneNumber string `json:"phone_number"`
	}
	if !recStrictDecode(raw, &a) {
		return nil, badRequest("a call asset holds only country_code and phone_number")
	}
	if !recCountryCodePattern.MatchString(a.CountryCode) {
		return nil, badRequest("country_code must be two capital letters, such as DE")
	}
	if !recPhonePattern.MatchString(a.PhoneNumber) {
		return nil, badRequest("phone_number must be 3 to 32 digits, spaces and the characters + ( ) . / -")
	}
	return map[string]any{"callAsset": map[string]any{"countryCode": a.CountryCode, "phoneNumber": a.PhoneNumber}}, nil
}

func recSitelinkAsset(_ string, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		LinkText     string   `json:"link_text"`
		Description1 string   `json:"description1"`
		Description2 string   `json:"description2"`
		FinalURLs    []string `json:"final_urls"`
	}
	if !recStrictDecode(raw, &a) {
		return nil, badRequest("a sitelink asset holds only link_text, description1, description2 and final_urls")
	}
	if err := recCheckText("link_text", a.LinkText, recMaxText); err != nil {
		return nil, err
	}
	for name, text := range map[string]string{"description1": a.Description1, "description2": a.Description2} {
		if text != "" {
			if err := recCheckText(name, text, recMaxText); err != nil {
				return nil, err
			}
		}
	}
	if err := recFinalURLs("final_urls", a.FinalURLs); err != nil {
		return nil, err
	}
	link := map[string]any{"linkText": a.LinkText}
	if a.Description1 != "" {
		link["description1"] = a.Description1
	}
	if a.Description2 != "" {
		link["description2"] = a.Description2
	}
	asset := map[string]any{"sitelinkAsset": link}
	if len(a.FinalURLs) > 0 {
		asset["finalUrls"] = a.FinalURLs
	}
	return asset, nil
}

// --- responsive search ad variants ---

type recAdTextArgument struct {
	Text        string `json:"text"`
	PinnedField string `json:"pinned_field"`
}

type recAdArgument struct {
	ResourceName string              `json:"resource_name"`
	FinalURLs    []string            `json:"final_urls"`
	Headlines    []recAdTextArgument `json:"headlines"`
	Descriptions []recAdTextArgument `json:"descriptions"`
	Path1        string              `json:"path1"`
	Path2        string              `json:"path2"`
}

var (
	recHeadlinePins    = []string{"HEADLINE_1", "HEADLINE_2", "HEADLINE_3"}
	recDescriptionPins = []string{"DESCRIPTION_1", "DESCRIPTION_2"}
)

func recAdSchema(withName bool) schema {
	properties := schema{
		"final_urls": recListOf(recTextSchema(maxURLArgument), 10),
		"headlines": recListOf(object(schema{"text": recTextSchema(recMaxText), "pinned_field": recEnumSchema(recHeadlinePins)},
			"text"), 20),
		"descriptions": recListOf(object(schema{"text": recTextSchema(recMaxText), "pinned_field": recEnumSchema(recDescriptionPins)},
			"text"), 10),
		"path1": recTextSchema(recMaxText), "path2": recTextSchema(recMaxText),
	}
	if withName {
		properties["resource_name"] = recOwnedSchema("ads")
	}
	return object(properties)
}

// recAdTexts checks a list of ad texts and maps it to the wire form.
func recAdTexts(argument string, texts []recAdTextArgument, pins []string, limit int) ([]any, error) {
	if len(texts) > limit {
		return nil, badRequest(argument + " takes at most " + recItoa(limit) + " texts")
	}
	out := make([]any, 0, len(texts))
	for _, t := range texts {
		if err := recCheckText(argument+".text", t.Text, recMaxText); err != nil {
			return nil, err
		}
		item := map[string]any{"text": t.Text}
		if t.PinnedField != "" {
			if !slices.Contains(pins, t.PinnedField) {
				return nil, badRequest(argument + ".pinned_field must be " + joinOr(pins))
			}
			item["pinnedField"] = t.PinnedField
		}
		out = append(out, item)
	}
	return out, nil
}

// recBuildAd checks the ad of a responsive search ad variant. Only the text, the paths and the final URLs are
// offered; the other URL members of an ad (tracking template, URL suffix, custom parameters, mobile and app
// URLs, display URL, collections) are not.
func recBuildAd(customerID, argument string, raw json.RawMessage, withName bool) (map[string]any, error) {
	var a recAdArgument
	if !recStrictDecode(raw, &a) {
		return nil, badRequest(argument + " holds an unknown member or a wrong type")
	}
	if a.ResourceName != "" && (!withName || !ownedName(a.ResourceName, customerID, "ads")) {
		return nil, badRequest(argument + ".resource_name must be an ad of the account of customer_id, " +
			"customers/CUSTOMER_ID/ads/ID, and only an updated ad takes it")
	}
	if err := recFinalURLs(argument+".final_urls", a.FinalURLs); err != nil {
		return nil, err
	}
	headlines, err := recAdTexts(argument+".headlines", a.Headlines, recHeadlinePins, 20)
	if err != nil {
		return nil, err
	}
	descriptions, err := recAdTexts(argument+".descriptions", a.Descriptions, recDescriptionPins, 10)
	if err != nil {
		return nil, err
	}
	for name, path := range map[string]string{"path1": a.Path1, "path2": a.Path2} {
		if path != "" {
			if err := recCheckText(argument+"."+name, path, recMaxText); err != nil {
				return nil, err
			}
			for _, r := range path {
				if unicode.IsSpace(r) || r == '/' || r == '?' || r == '#' {
					return nil, badRequest(argument + "." + name + " must be one path segment without spaces or slashes")
				}
			}
		}
	}
	if a.Path2 != "" && a.Path1 == "" {
		return nil, badRequest(argument + ".path2 can only be set when path1 is set")
	}
	info := map[string]any{}
	if len(headlines) > 0 {
		info["headlines"] = headlines
	}
	if len(descriptions) > 0 {
		info["descriptions"] = descriptions
	}
	if a.Path1 != "" {
		info["path1"] = a.Path1
	}
	if a.Path2 != "" {
		info["path2"] = a.Path2
	}
	ad := map[string]any{"responsiveSearchAd": info}
	if len(a.FinalURLs) > 0 {
		ad["finalUrls"] = a.FinalURLs
	}
	if a.ResourceName != "" {
		ad["resourceName"] = a.ResourceName
	}
	return ad, nil
}

func recAdVariant(name, member string, withName bool) recVariant {
	return recVariant{name: name, schema: object(schema{member: recAdSchema(withName)}, member),
		build: func(customerID string, raw json.RawMessage) (map[string]any, error) {
			var wrapper map[string]json.RawMessage
			if json.Unmarshal(raw, &wrapper) != nil || len(wrapper) != 1 || wrapper[member] == nil {
				return nil, badRequest("parameters." + name + " holds only " + member)
			}
			ad, err := recBuildAd(customerID, "parameters."+name+"."+member, wrapper[member], withName)
			if err != nil {
				return nil, err
			}
			return map[string]any{recCamelCase(member): ad}, nil
		}}
}

func recKeywordVariant() recVariant {
	return recVariant{name: "keyword",
		schema: object(schema{"ad_group": recOwnedSchema("adGroups"), "match_type": recEnumSchema(recKeywordMatchTypes),
			"cpc_bid_micros": recMicrosSchema()}, "ad_group", "match_type"),
		build: func(customerID string, raw json.RawMessage) (map[string]any, error) {
			var a struct {
				AdGroup     string `json:"ad_group"`
				MatchType   string `json:"match_type"`
				CPCBidMicro *int64 `json:"cpc_bid_micros"`
			}
			if !recStrictDecode(raw, &a) {
				return nil, badRequest("parameters.keyword holds an unknown member or a wrong type")
			}
			if !ownedName(a.AdGroup, customerID, "adGroups") {
				return nil, badRequest("parameters.keyword.ad_group must be an ad group of the account of customer_id, " +
					"customers/CUSTOMER_ID/adGroups/ID")
			}
			if !slices.Contains(recKeywordMatchTypes, a.MatchType) {
				return nil, badRequest("parameters.keyword.match_type must be EXACT, PHRASE or BROAD")
			}
			out := map[string]any{"adGroup": a.AdGroup, "matchType": a.MatchType}
			if a.CPCBidMicro != nil {
				if *a.CPCBidMicro < 1 || *a.CPCBidMicro > recMaxMicros {
					return nil, badRequest("parameters.keyword.cpc_bid_micros must be a whole amount in micros, at least 1")
				}
				out["cpcBidMicros"] = *a.CPCBidMicro
			}
			return out, nil
		}}
}

// recApplyVariants are the members of the oneof apply_parameters that Qatlas Connect offers, in the order of the
// reference.
var recApplyVariants = []recVariant{
	recNumericVariant("campaign_budget", false, recMicrosField("new_budget_amount_micros", true)),
	recKeywordVariant(),
	recNumericVariant("target_cpa_opt_in", false, recMicrosField("target_cpa_micros", true),
		recMicrosField("new_campaign_budget_amount_micros", false)),
	recNumericVariant("target_roas_opt_in", true, recRoasField("target_roas"), recMicrosField("new_campaign_budget_amount_micros", false)),
	recNumericVariant("move_unused_budget", false, recMicrosField("budget_micros_to_move", true)),
	recAdVariant("responsive_search_ad", "ad", false),
	recNumericVariant("use_broad_match_keyword", false, recMicrosField("new_budget_amount_micros", false)),
	recAdVariant("responsive_search_ad_asset", "updated_ad", true),
	recAdVariant("responsive_search_ad_improve_ad_strength", "updated_ad", true),
	// The reference says the factor is above 1.0 for the two "too low" raises.
	recNumericVariant("raise_target_cpa_bid_too_low", false, recMultiplierField("target_multiplier", 1, 1000, true, false)),
	recNumericVariant("forecasting_set_target_roas", false, recRoasField("target_roas"), recMicrosField("campaign_budget_amount_micros", false)),
	recAssetVariant("callout_asset", object(schema{"callout_text": recTextSchema(recMaxText)}, "callout_text"), recCalloutAsset),
	recAssetVariant("call_asset", object(schema{"country_code": schema{"type": "string", "pattern": "^[A-Z]{2}$"},
		"phone_number": schema{"type": "string", "pattern": recPhonePatternText}}, "country_code", "phone_number"), recCallAsset),
	recAssetVariant("sitelink_asset", object(schema{"link_text": recTextSchema(recMaxText),
		"description1": recTextSchema(recMaxText), "description2": recTextSchema(recMaxText),
		"final_urls": recListOf(recTextSchema(maxURLArgument), 10)}, "link_text"), recSitelinkAsset),
	recNumericVariant("raise_target_cpa", false, recMultiplierField("target_cpa_multiplier", 0, 1000, true, false)),
	recNumericVariant("lower_target_roas", false, recMultiplierField("target_roas_multiplier", 0, 1000, true, false)),
	recNumericVariant("forecasting_set_target_cpa", false, recMicrosField("target_cpa_micros", false),
		recMicrosField("campaign_budget_amount_micros", false)),
	recNumericVariant("set_target_cpa", false, recMicrosField("target_cpa_micros", false), recMicrosField("campaign_budget_amount_micros", false)),
	recNumericVariant("set_target_roas", false, recRoasField("target_roas"), recMicrosField("campaign_budget_amount_micros", false)),
	recNumericVariant("raise_target_cpa_performance_bid_too_low", false,
		recMultiplierField("target_cpa_multiplier", 1, 1000, true, false)),
	recNumericVariant("lower_target_roas_performance_bid_too_low", false,
		recMultiplierField("target_roas_multiplier", 0, 1, true, true)),
	recNumericVariant("raise_budget_performance_too_low", false, recMicrosField("new_budget_amount_micros", true)),
	recNumericVariant("optimize_for_new_customers", false,
		recNumberField{name: "account_default_incremental_conversion_value", lo: 0, hi: 1e12},
		recNumberField{name: "campaign_override_incremental_conversion_value", lo: 0, hi: 1e12}),
}

func recVariantByName(name string) (recVariant, bool) {
	for _, v := range recApplyVariants {
		if v.name == name {
			return v, true
		}
	}
	return recVariant{}, false
}

func (v recVariant) wireParameters(customerID string, raw json.RawMessage) (map[string]any, error) {
	if v.build != nil {
		return v.build(customerID, raw)
	}
	return v.numbers(raw)
}

// operationParts checks one operation and returns its wire form.
func (a applyArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if len(a.Operations) == 0 || len(a.Operations) > maxRecommendationOperations {
		return nil, nil, badRequest("operations must hold 1 to 10 operations")
	}
	wire := make([]any, 0, len(a.Operations))
	names := make([]string, 0, len(a.Operations))
	for _, op := range a.Operations {
		if err := checkRecommendationName("operations.resource_name", a.CustomerID, op.ResourceName); err != nil {
			return nil, nil, err
		}
		if slices.Contains(names, op.ResourceName) {
			return nil, nil, badRequest("operations names a recommendation more than once")
		}
		names = append(names, op.ResourceName)
		entry := map[string]any{"resourceName": op.ResourceName}
		if len(op.Parameters) > 1 {
			return nil, nil, badRequest("parameters holds exactly one variant, one member of the oneof of the reference")
		}
		for name, raw := range op.Parameters {
			v, ok := recVariantByName(name)
			if !ok {
				return nil, nil, badRequest("parameters." + name + " is not an offered variant")
			}
			params, err := v.wireParameters(a.CustomerID, raw)
			if err != nil {
				return nil, nil, err
			}
			entry[recCamelCase(name)] = params
		}
		wire = append(wire, entry)
	}
	return wire, names, nil
}

func recApplyOperationSchema() schema {
	variants := schema{}
	for _, v := range recApplyVariants {
		variants[v.name] = v.schema
	}
	return object(schema{
		"resource_name": schema{"type": "string", "pattern": recommendationNamePatternText},
		"parameters": schema{"type": "object", "properties": variants, "additionalProperties": false,
			"minProperties": 1, "maxProperties": 1},
	}, "resource_name")
}

func recVariantNames() string {
	names := make([]string, 0, len(recApplyVariants))
	for _, v := range recApplyVariants {
		names = append(names, v.name)
	}
	return strings.Join(names, ", ")
}

var recommendationsApply = capability.Descriptor{
	ID: recommendationsApplyID, Version: 1, Title: "Apply recommendations of a Google Ads account",
	Description: "Apply recommendations of a Google Ads account " +
		"(RecommendationService.ApplyRecommendation with 1 to 10 operations). What an application changes follows " +
		"the type of the recommendation and can be a budget, a bid, a bidding strategy, a keyword, an ad or the " +
		"assets of a campaign or the account, so it can change what the account spends. Each operation names the " +
		"recommendation and may carry parameters of exactly one variant (parameters holds one member): the " +
		"variant has to fit the type of the recommendation, otherwise Google refuses it; without parameters Google " +
		"applies the recommendation as it is. The offered variants are " + recVariantNames() + ". Numbers are micros " +
		"of the account's currency, target ROAS values 0.01 to 1000, the multipliers of the two \"too low\" raises " +
		"above 1 and of the lowering of ROAS below 1. The asset variants flatten the member " +
		"ad_asset_apply_parameters: new_assets, existing_assets and the scope CUSTOMER or CAMPAIGN. Ad and asset " +
		"addresses (final_urls) must be https without user info and are never fetched. Not offered: tracking " +
		"templates, URL suffixes, custom parameters, mobile and app URLs and display URLs of ads and assets, " +
		"lead form assets with their delivery routes, call tracking of call assets, and the legacy variants " +
		"text_ad, callout_extension, call_extension and sitelink_extension. The request is atomic, without " +
		"partial_failure: Google applies all operations or none, and the reference asks for recommendations of " +
		"one type per request. The service has no validate_only, so this tool has none either: the call " +
		"applies. A raise of a target CPA that is too low is applied by Google asynchronously and can take " +
		"minutes. Recommendations are named by customers/CUSTOMER_ID/recommendations/ID of the account of the " +
		"call." + mutateListListedNote,
	Tags: []string{"google", "ads", "recommendations", "apply"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent,
		"spend and content of a Google Ads account: budgets, bids, bidding strategies, keywords, ads and assets "+
			"change as the recommendation says"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"operations": schema{"type": "array", "minItems": 1, "maxItems": maxRecommendationOperations,
			"items": recApplyOperationSchema()}}, "customer_id", "operations")),
	OutputSchema: encoded(recActionOutputSchema),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "operations", Required: true, Description: "1 to 10 operations, each with resource_name " +
			"(customers/CUSTOMER_ID/recommendations/ID, the customer being the one of customer_id, each once) and " +
			"optional parameters with exactly one of the variants " + recVariantNames() + ", each with the parameters " +
			"of the reference as named there"},
	},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "results", Description: "The resource names of the applied recommendations, in the order asked for"},
	},
	Examples: []capability.Example{
		{Description: "Apply a budget recommendation with a new budget of 20 currency units",
			Arguments: json.RawMessage(`{"customer_id":"1234567890","operations":[{"resource_name":` +
				`"customers/1234567890/recommendations/abc123","parameters":{"campaign_budget":` +
				`{"new_budget_amount_micros":20000000}}}]}`)},
		{Description: "Apply a recommendation as Google made it",
			Arguments: json.RawMessage(`{"customer_id":"1234567890","operations":[{"resource_name":` +
				`"customers/1234567890/recommendations/abc123"}]}`)}},
}

func handleRecommendationsApply(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "apply recommendations"
	var args applyArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, recommendationsApplyID)
	if err != nil {
		return nil, err
	}
	operations, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer recActionAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: env.tool, customerID: args.CustomerID, service: recommendationService,
		action: "apply", body: map[string]any{"operations": operations}}, &answer); err != nil {
		return nil, err
	}
	results, err := recSettledNames(op, answer, names)
	if err != nil {
		return nil, err
	}
	return recActionResult{CustomerID: args.CustomerID, Results: results}, nil
}

// recommendationApplySummary phrases the change for the confirmation, from the arguments alone, with the
// parameters of every operation.
func recommendationApplySummary(raw json.RawMessage) string {
	var a applyArguments
	_ = json.Unmarshal(raw, &a)
	parts := make([]string, 0, len(a.Operations))
	for _, op := range a.Operations {
		part := boundedText(op.ResourceName, 200)
		for name, params := range op.Parameters {
			var compact bytes.Buffer
			if json.Compact(&compact, params) != nil {
				compact.Reset()
			}
			part += " with " + boundedText(name, 64) + " " + boundedText(compact.String(), 300)
		}
		parts = append(parts, part)
	}
	return "Apply " + recItoa(len(parts)) + " recommendation(s) of Google Ads account " + a.CustomerID + ": " +
		strings.Join(parts, "; ") + ". An application can change budgets, bids, keywords, ads or assets, " +
		"and with them what the account spends."
}
