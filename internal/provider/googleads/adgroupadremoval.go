package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	adGroupAdCreatedAssetsRemoveID = Provider + ".ad-group-ads.remove-automatically-created-assets"

	// maxAdAssetRemovals is Qatlas's own bound; Google documents none.
	maxAdAssetRemovals = 10
)

type (
	adAssetRemovalArguments struct {
		CustomerID          string           `json:"customer_id"`
		AdGroupAd           string           `json:"ad_group_ad"`
		AssetsWithFieldType []adAssetRemoval `json:"assets_with_field_type"`
	}
	adAssetRemoval struct {
		Asset     string `json:"asset"`
		FieldType string `json:"asset_field_type"`
	}
	adAssetRemovalDone struct {
		CustomerID string           `json:"customer_id"`
		AdGroupAd  string           `json:"ad_group_ad"`
		Removed    []adAssetRemoval `json:"removed"`
	}
)

var adAssetFieldTypes = enumNamed("AssetFieldTypeEnum.AssetFieldType")

// request checks the ad and every asset: both must belong to the account of the call, which also refuses
// the temporary negative IDs of a batch, and the field type must name an asset field type of the API. The
// ad is part of the path of the request, the assets are its body.
func (a adAssetRemovalArguments) request() (string, []any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return "", nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	group, ad, ok := adGroupAdIDs(a.AdGroupAd, a.CustomerID)
	if !ok {
		return "", nil, badRequest("ad_group_ad must be customers/" + queryNumber(a.CustomerID) +
			"/adGroupAds/AD_GROUP_ID~AD_ID of customer_id; temporary IDs are not accepted")
	}
	if n := len(a.AssetsWithFieldType); n == 0 || n > maxAdAssetRemovals {
		return "", nil, badRequest("assets_with_field_type must hold one to " + strconv.Itoa(maxAdAssetRemovals) + " entries")
	}
	entries := make([]any, 0, len(a.AssetsWithFieldType))
	for i, r := range a.AssetsWithFieldType {
		path := "assets_with_field_type[" + strconv.Itoa(i) + "]."
		switch {
		case !ownedResource(r.Asset, a.CustomerID, "assets"):
			return "", nil, badRequest(path + "asset must be an asset of customer_id, customers/ID/assets/N; " +
				"temporary IDs are not accepted")
		case !slices.Contains(adAssetFieldTypes, r.FieldType):
			return "", nil, badRequest(path + "asset_field_type must be the name of an asset field type of the API, " +
				"such as HEADLINE")
		}
		entries = append(entries, map[string]any{"asset": r.Asset, "assetFieldType": r.FieldType})
	}
	return "/adGroupAds/" + group + "~" + ad, entries, nil
}

var adGroupAdsRemoveAutomaticallyCreatedAssets = capability.Descriptor{
	ID: adGroupAdCreatedAssetsRemoveID, Version: 1,
	Title: "Remove automatically created assets from a Google Ads ad",
	Description: "Remove text customization that Google created automatically, such as generated headlines and " +
		"descriptions, from one ad, each named by asset and asset field type " +
		"(AdGroupAdService.RemoveAutomaticallyCreatedAssets). The asset itself stays in the account; only its use " +
		"by the ad for that field type ends. The service has no validate_only and no partial failure, so the tool " +
		"offers none; the request is one atomic transaction and answers nothing but success." + mutateListListedNote,
	Tags: []string{"google", "ads", "ad-group-ads", "assets", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent,
		"ad content of a Google Ads account"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "ad_group_ad": adGroupAdResourceName,
		"assets_with_field_type": schema{"type": "array", "minItems": 1, "maxItems": maxAdAssetRemovals, "items": object(schema{
			"asset":            schema{"type": "string", "pattern": assetNamePattern},
			"asset_field_type": schema{"type": "string", "enum": adAssetFieldTypes}},
			"asset", "asset_field_type")}},
		"customer_id", "ad_group_ad", "assets_with_field_type")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "ad_group_ad": stringSchema,
		"removed": arrayOf(object(schema{"asset": stringSchema, "asset_field_type": stringSchema}))},
		"customer_id", "ad_group_ad", "removed")),
	Arguments: []capability.Argument{adGroupAdCustomerArgument,
		{Name: "ad_group_ad", Required: true, Description: "The ad of this account to remove from, " +
			"customers/ID/adGroupAds/AD_GROUP_ID~AD_ID"},
		{Name: "assets_with_field_type", Required: true, Description: "One to 10 entries, each with asset " +
			"(customers/ID/assets/N, of customer_id) and asset_field_type, the name of the asset field type of the " +
			"customization, such as HEADLINE or DESCRIPTION"}},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "ad_group_ad", Description: "The ad of the call"},
		{Name: "removed", Description: "The removals Google carried out, as asked; Google answers nothing else"}},
	Examples: []capability.Example{{Description: "Remove a generated headline from an ad",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ad_group_ad":"customers/1234567890/adGroupAds/31~41",` +
			`"assets_with_field_type":[{"asset":"customers/1234567890/assets/33","asset_field_type":"HEADLINE"}]}`)}},
}

func handleAdGroupAdsRemoveAutomaticallyCreatedAssets(ctx context.Context, resolved *config.Resolved,
	secrets *secret.Resolver, red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove automatically created assets from an ad"
	var args adAssetRemovalArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, adGroupAdCreatedAssetsRemoveID)
	if err != nil {
		return nil, err
	}
	service, entries, err := args.request()
	if err != nil {
		return nil, err
	}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct{}
	if err := c.mutate(ctx, mutation{op: op, tool: env.tool, customerID: args.CustomerID, service: service,
		action: "removeAutomaticallyCreatedAssets", body: map[string]any{"assetsWithFieldType": entries}}, &answer); err != nil {
		return nil, err
	}
	return adAssetRemovalDone{CustomerID: args.CustomerID, AdGroupAd: args.AdGroupAd, Removed: args.AssetsWithFieldType}, nil
}

// adAssetRemovalSummary phrases the change for the confirmation, from the arguments alone.
func adAssetRemovalSummary(raw json.RawMessage) string {
	var a adAssetRemovalArguments
	_ = json.Unmarshal(raw, &a)
	parts := make([]string, 0, len(a.AssetsWithFieldType))
	for _, r := range a.AssetsWithFieldType {
		parts = append(parts, r.FieldType+" "+r.Asset)
	}
	return "Remove automatically created assets of Google Ads account " + a.CustomerID + " from ad " + a.AdGroupAd +
		": " + strings.Join(parts, "; ") + ". The ad stops using them for that field type."
}
