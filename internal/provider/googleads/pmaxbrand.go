package googleads

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const campaignsEnablePMaxBrandGuidelinesID = Provider + ".campaigns.enable-pmax-brand-guidelines"

const maxBrandLogos = 20

// brandGuidelinesTree holds the members of one operation of EnablePMaxBrandGuidelines. The call has no
// validate_only and no partial failure, and Google reports a failed campaign inside an answer that is
// otherwise successful, so the tool takes one campaign per call.
var brandGuidelinesTree = objectNode("", "",
	resourceNode("campaign", "Performance Max campaign of this account, customers/ID/campaigns/N", "campaigns"),
	flagNode("auto_populate_brand_assets", "true lets Google fill the brand assets from the best performing ones of the campaign"),
	objectNode("brand_assets", "Brand assets of this account; required when auto_populate_brand_assets is false",
		resourceNode("business_name_asset", "Text asset with the business name, customers/ID/assets/N", "assets"),
		listOf(resourceNode("logo_asset", "Square logo assets, customers/ID/assets/N", "assets"), maxBrandLogos),
		listOf(resourceNode("landscape_logo_asset", "Landscape logo assets, customers/ID/assets/N", "assets"), maxBrandLogos)),
	patternNode("final_uri_domain", "Domain of the final URL, such as www.example.com, without scheme and path", hostPattern, 253),
	patternNode("main_color", "Hex color such as #00ff00; needs accent_color", colorPattern, 7),
	patternNode("accent_color", "Hex color such as #00ff00; needs main_color", colorPattern, 7),
	enumNode("font_family", "Font family, case-sensitive", brandFonts))

// brandGuidelinesOperation checks the arguments and builds the one operation.
func brandGuidelinesOperation(customerID string, in map[string]any) (map[string]any, error) {
	for _, required := range []string{"campaign", "auto_populate_brand_assets"} {
		if _, ok := in[required]; !ok {
			return nil, badRequest(required + " is required")
		}
	}
	_, main := in["main_color"]
	_, accent := in["accent_color"]
	if main != accent {
		return nil, badRequest("main_color and accent_color are given together or not at all")
	}
	assets, hasAssets := in["brand_assets"].(map[string]any)
	if auto, _ := in["auto_populate_brand_assets"].(bool); !auto {
		_, name := assets["business_name_asset"]
		logos, _ := assets["logo_asset"].([]any)
		if !hasAssets || !name || len(logos) == 0 {
			return nil, badRequest("brand_assets with business_name_asset and logo_asset is required when " +
				"auto_populate_brand_assets is false")
		}
	}
	b := builder{customerID: customerID, create: true}
	return b.object(brandGuidelinesTree, in, pathOfMember{})
}

type pmaxBrandChanged struct {
	CustomerID string `json:"customer_id"`
	Campaign   string `json:"campaign"`
	Enabled    bool   `json:"enabled"`
}

var pmaxBrandInput = func() schema {
	s := brandGuidelinesTree.inputSchema()
	props := s["properties"].(schema)
	props["customer_id"] = customerIDArgument
	s["required"] = []string{"customer_id", "campaign", "auto_populate_brand_assets"}
	delete(s, "minProperties")
	return s
}()

var campaignsEnablePMaxBrandGuidelines = capability.Descriptor{
	ID: campaignsEnablePMaxBrandGuidelinesID, Version: 1, Title: "Enable brand guidelines of a Google Ads Performance Max campaign",
	Description: "Enable brand guidelines for one Performance Max campaign of a Google Ads account " +
		"(CampaignService.EnablePMaxBrandGuidelines): the brand assets, colors and font of the campaign then come " +
		"from the guidelines, and the campaign serves with them. Brand assets are either filled by Google from the " +
		"best performing assets of the campaign (auto_populate_brand_assets) or named: a business name asset and " +
		"square logos, optionally landscape logos, all assets of customer_id. The colors are given together. " +
		"Google offers no validate_only for this call and answers a campaign that it could not enable inside a " +
		"successful answer, so the tool takes one campaign and a failure is the error of the call. The campaign " +
		"and the assets must belong to customer_id; a temporary negative ID is refused." + mutateNote,
	Tags: []string{"google", "ads", "campaigns", "performance-max", "brand"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent, campaignSensitivity),
	InputSchema: encoded(pmaxBrandInput),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "campaign": stringSchema, "enabled": booleanSchema},
		"customer_id", "campaign", "enabled")),
	Arguments: []capability.Argument{campaignCustomerArgument,
		{Name: "campaign", Required: true, Description: "Performance Max campaign of this account, customers/ID/campaigns/N"},
		{Name: "auto_populate_brand_assets", Required: true, Description: "true: Google fills the brand assets; false: brand_assets is required"},
		{Name: "brand_assets", Description: "business_name_asset, logo_asset and optionally landscape_logo_asset, assets of this account"},
		{Name: "final_uri_domain", Description: "Domain of the final URL"},
		{Name: "main_color", Description: "Hex color; needs accent_color"},
		{Name: "accent_color", Description: "Hex color; needs main_color"},
		{Name: "font_family", Description: "One of Open Sans, Roboto, Roboto Slab, Montserrat, Poppins, Lato, Oswald, Playfair Display"}},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "campaign", Description: "The campaign Google enabled the guidelines for"},
		{Name: "enabled", Description: "true when Google enabled the guidelines; a failure is an error instead"}},
	Examples: []capability.Example{{Description: "Enable brand guidelines and let Google fill the brand assets",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign":"customers/1234567890/campaigns/21","auto_populate_brand_assets":true}`)}},
}

func handleCampaignsEnablePMaxBrandGuidelines(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "enable Performance Max brand guidelines"
	in, ok := decodeObject(raw)
	if !ok {
		return nil, failure(op, "the validated arguments could not be read")
	}
	customerID, _ := in["customer_id"].(string)
	delete(in, "customer_id")
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(customerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	operation, err := brandGuidelinesOperation(customerID, in)
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: campaignsEnablePMaxBrandGuidelinesID}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Results []struct {
			Campaign        string          `json:"campaign"`
			EnablementError json.RawMessage `json:"enablementError"`
		} `json:"results"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: campaignsEnablePMaxBrandGuidelinesID, customerID: customerID,
		service: campaignService, action: "enablePMaxBrandGuidelines",
		body: map[string]any{"operations": []any{operation}}}, &answer); err != nil {
		return nil, err
	}
	want := stringOf(in["campaign"])
	if len(answer.Results) != 1 || answer.Results[0].Campaign != want {
		return nil, unsure(provider.InvalidResponse(op, subject+" answered with another result than the one campaign asked for"), true)
	}
	if e := answer.Results[0].EnablementError; len(e) > 0 && string(e) != "null" {
		// Google's own error object of the campaign, mapped like the error of any failed call.
		failed, _ := adsError(op, 400, []byte(`{"error":`+string(e)+`}`), true)
		return nil, failed
	}
	return pmaxBrandChanged{CustomerID: customerID, Campaign: want, Enabled: true}, nil
}

// brandGuidelinesSummary phrases the change for the confirmation, from the arguments alone.
func brandGuidelinesSummary(raw json.RawMessage) string {
	in, _ := decodeObject(raw)
	how := "with the brand assets named in the call"
	if auto, _ := in["auto_populate_brand_assets"].(bool); auto {
		how = "with brand assets that Google fills from the best performing assets of the campaign"
	}
	var set []string
	for _, key := range []string{"final_uri_domain", "main_color", "accent_color", "font_family"} {
		if v := stringOf(in[key]); v != "" {
			set = append(set, key+" "+v)
		}
	}
	summary := "Enable brand guidelines of Performance Max campaign " + stringOf(in["campaign"]) + " of Google Ads account " +
		stringOf(in["customer_id"]) + " " + how
	if len(set) > 0 {
		summary += " (" + strings.Join(set, ", ") + ")"
	}
	return summary + ". The campaign then serves with these guidelines."
}
