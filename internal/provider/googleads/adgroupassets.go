package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	adGroupAssetsCreateID = Provider + ".ad-group-assets.create"
	adGroupAssetsUpdateID = Provider + ".ad-group-assets.update"
	adGroupAssetsRemoveID = Provider + ".ad-group-assets.remove"

	adGroupAssetsService = "/adGroupAssets"
)

// AdGroupAssetService.MutateAdGroupAssets takes operations[]. A link is named by ad group, asset, and field
// type, which are immutable and make up its resource name; status is the only field an update changes.

var adGroupAssetKind = assetLinkKind{collection: "adGroupAssets", parentCollection: "adGroups", parentKey: "adGroup",
	parentNoun: "ad group", parentArgument: "ad_group_id", resourceKey: "adGroupAsset"}

type (
	adGroupAssetKey struct {
		AdGroupID string `json:"ad_group_id"`
		AssetID   string `json:"asset_id"`
		FieldType string `json:"field_type"`
	}
	adGroupAssetNew struct {
		adGroupAssetKey
		Status *string `json:"status"`
	}
	adGroupAssetEdit struct {
		adGroupAssetKey
		Status string `json:"status"`
	}
	adGroupAssetsCreateArguments struct {
		CustomerID          string            `json:"customer_id"`
		AdGroupAssets       []adGroupAssetNew `json:"ad_group_assets"`
		ValidateOnly        bool              `json:"validate_only"`
		ResponseContentType string            `json:"response_content_type"`
	}
	adGroupAssetsUpdateArguments struct {
		CustomerID          string             `json:"customer_id"`
		AdGroupAssets       []adGroupAssetEdit `json:"ad_group_assets"`
		ValidateOnly        bool               `json:"validate_only"`
		ResponseContentType string             `json:"response_content_type"`
	}
	adGroupAssetsRemoveArguments struct {
		CustomerID    string            `json:"customer_id"`
		AdGroupAssets []adGroupAssetKey `json:"ad_group_assets"`
		ValidateOnly  bool              `json:"validate_only"`
	}
)

func (k adGroupAssetKey) key() assetLinkKey {
	return assetLinkKey{ParentID: k.AdGroupID, AssetID: k.AssetID, FieldType: k.FieldType}
}

func adGroupAssetPath(i int) string { return "ad_group_assets[" + strconv.Itoa(i) + "]." }

// adGroupAssetKeys checks every key and refuses one that is named twice.
func adGroupAssetKeys(customerID string, keys []adGroupAssetKey) ([]assetLinkKey, error) {
	if !customerIDPattern.MatchString(customerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := assetLinkCheckCount("ad_group_assets", len(keys)); err != nil {
		return nil, err
	}
	out := make([]assetLinkKey, 0, len(keys))
	for i, k := range keys {
		if err := adGroupAssetKind.check(adGroupAssetPath(i), k.key()); err != nil {
			return nil, err
		}
		if slices.Contains(out, k.key()) {
			return nil, badRequest("ad_group_assets names each ad group, asset, and field_type at most once")
		}
		out = append(out, k.key())
	}
	return out, nil
}

// operations checks the arguments and builds the operations of the request with the keys they name.
func (a adGroupAssetsCreateArguments) operations() ([]any, []assetLinkKey, error) {
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	keys := make([]adGroupAssetKey, 0, len(a.AdGroupAssets))
	for _, c := range a.AdGroupAssets {
		keys = append(keys, c.adGroupAssetKey)
	}
	checked, err := adGroupAssetKeys(a.CustomerID, keys)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(keys))
	for i, c := range a.AdGroupAssets {
		create := map[string]any{
			"adGroup":   adGroupAssetKind.parentName(a.CustomerID, c.AdGroupID),
			"asset":     customerName(a.CustomerID) + "/assets/" + c.AssetID,
			"fieldType": c.FieldType,
		}
		if c.Status != nil {
			if err := assetLinkCheckStatus(adGroupAssetPath(i), *c.Status); err != nil {
				return nil, nil, err
			}
			create["status"] = *c.Status
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, checked, nil
}

func (a adGroupAssetsUpdateArguments) operations() ([]any, []assetLinkKey, error) {
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	keys := make([]adGroupAssetKey, 0, len(a.AdGroupAssets))
	for _, c := range a.AdGroupAssets {
		keys = append(keys, c.adGroupAssetKey)
	}
	checked, err := adGroupAssetKeys(a.CustomerID, keys)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(keys))
	for i, c := range a.AdGroupAssets {
		if err := assetLinkCheckStatus(adGroupAssetPath(i), c.Status); err != nil {
			return nil, nil, err
		}
		ops = append(ops, map[string]any{
			"update":     map[string]any{"resourceName": adGroupAssetKind.name(a.CustomerID, checked[i]), "status": c.Status},
			"updateMask": "status"})
	}
	return ops, checked, nil
}

func (a adGroupAssetsRemoveArguments) operations() ([]any, []assetLinkKey, error) {
	checked, err := adGroupAssetKeys(a.CustomerID, a.AdGroupAssets)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(checked))
	for _, k := range checked {
		ops = append(ops, map[string]any{"remove": adGroupAssetKind.name(a.CustomerID, k)})
	}
	return ops, checked, nil
}

// --- results ---

type (
	adGroupAssetObject struct {
		AdGroup string `json:"ad_group,omitempty"`
		assetLinkObject
	}
	adGroupAssetMutated struct {
		AdGroupID    string              `json:"ad_group_id"`
		AssetID      string              `json:"asset_id"`
		FieldType    string              `json:"field_type"`
		ResourceName string              `json:"resource_name"`
		AdGroupAsset *adGroupAssetObject `json:"ad_group_asset,omitempty"`
	}
	adGroupAssetsChanged struct {
		CustomerID   string                `json:"customer_id"`
		ValidateOnly bool                  `json:"validate_only"`
		Results      []adGroupAssetMutated `json:"results"`
	}
)

func adGroupAssetsOutcome(customerID string, validateOnly bool, done []assetLinkDone) adGroupAssetsChanged {
	out := adGroupAssetsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []adGroupAssetMutated{}}
	for _, d := range done {
		item := adGroupAssetMutated{AdGroupID: d.Key.ParentID, AssetID: d.Key.AssetID, FieldType: d.Key.FieldType,
			ResourceName: d.ResourceName}
		if d.Link != nil {
			item.AdGroupAsset = &adGroupAssetObject{AdGroup: d.Parent, assetLinkObject: *d.Link}
		}
		out.Results = append(out.Results, item)
	}
	return out
}

// --- descriptors ---

var (
	adGroupAssetIDSchema        = schema{"type": "string", "pattern": numericIDPattern.String()}
	adGroupAssetFieldTypeSchema = schema{"type": "string", "enum": assetLinkFieldTypes}
	adGroupAssetStatusSchema    = schema{"type": "string", "enum": assetLinkStatuses}

	adGroupAssetKeyProperties = func() schema {
		return schema{"ad_group_id": adGroupAssetIDSchema, "asset_id": adGroupAssetIDSchema,
			"field_type": adGroupAssetFieldTypeSchema}
	}

	adGroupAssetObjectSchema = object(schema{"ad_group": stringSchema, "resource_name": stringSchema,
		"asset": stringSchema, "field_type": stringSchema, "status": stringSchema, "source": stringSchema,
		"primary_status": stringSchema, "primary_status_reasons": arrayOf(stringSchema),
		"primary_status_details": arrayOf(object(schema{"status": stringSchema, "reason": stringSchema,
			"asset_disapproved": object(schema{"offline_evaluation_error_reasons": arrayOf(stringSchema)})}))},
		"resource_name")
	adGroupAssetsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"ad_group_id": stringSchema, "asset_id": stringSchema,
			"field_type": stringSchema, "resource_name": stringSchema, "ad_group_asset": adGroupAssetObjectSchema},
			"ad_group_id", "asset_id", "field_type", "resource_name"))},
		"customer_id", "validate_only", "results")
)

func adGroupAssetItems(extra schema, required ...string) schema {
	properties := adGroupAssetKeyProperties()
	for name, s := range extra {
		properties[name] = s
	}
	return schema{"type": "array", "minItems": 1, "maxItems": assetLinksMaxOperations,
		"items": object(properties, append([]string{"ad_group_id", "asset_id", "field_type"}, required...)...)}
}

const adGroupAssetsSensitivity = "which assets an ad group of a Google Ads account uses and in which role, " +
	"which the confirmation names; no data of other people"

const adGroupAssetsNote = " An ad group asset is the use of an existing asset by an ad group for one field type, " +
	"named by ad_group_id, asset_id, and field_type, from which the resource name " +
	"customers/ID/adGroupAssets/AD_GROUP_ID~ASSET_ID~FIELD_TYPE is built; this tool neither creates assets " +
	"nor reads links (read them with reports.search on ad_group_asset). Which field type fits which asset and " +
	"which ad group type is up to Google. The request goes to AdGroupAssetService.MutateAdGroupAssets with up " +
	"to 50 operations, atomic and without partial_failure: Google applies all of them or none."

var adGroupAssetsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: ad_group_id, asset_id, " +
		"field_type, resource_name (customers/ID/adGroupAssets/AD_GROUP_ID~ASSET_ID~FIELD_TYPE, as Google " +
		"answered, always of the account and the link of the call), and with MUTABLE_RESOURCE ad_group_asset, " +
		"the link after the change as Google returns it: ad_group, resource_name, asset, field_type, status and " +
		"the fields Google fills in (source, primary_status, primary_status_reasons, primary_status_details " +
		"with status, reason and the offline evaluation reasons of a disapproved asset). Empty after validate_only"},
}

var adGroupAssetsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

const adGroupAssetKeyDoc = "ad_group_id (ID of an ad group of the account), asset_id (ID of an asset of the " +
	"account), field_type (name of the asset field type, such as HEADLINE, SITELINK or CALLOUT); the IDs are " +
	"positive numbers as reports.search returns them, never a temporary ID"

var adGroupAssetsCreate = capability.Descriptor{
	ID: adGroupAssetsCreateID, Version: 1, Title: "Attach assets to Google Ads ad groups",
	Description: "Attach existing assets to ad groups of a Google Ads account, each for one field type, " +
		"optionally with the status ENABLED or PAUSED." + adGroupAssetsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		changeNote,
	Tags: []string{"google", "ads", "assets", "adGroups", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, adGroupAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"ad_group_assets": adGroupAssetItems(schema{"status": adGroupAssetStatusSchema}),
		"validate_only":   validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "ad_group_assets")),
	OutputSchema: encoded(adGroupAssetsOutputSchema),
	Arguments: append(slices.Clone(adGroupAssetsArgumentDocs),
		capability.Argument{Name: "ad_group_assets", Required: true, Description: "1 to 50 entries, each with " +
			adGroupAssetKeyDoc + ", and optionally status (ENABLED or PAUSED; left out, Google decides). Each " +
			"ad group, asset, and field type at most once"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: adGroupAssetsFields,
	Examples: []capability.Example{{Description: "Attach a sitelink asset to an ad group",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ad_group_assets":[{"ad_group_id":"22","asset_id":"33","field_type":"SITELINK"}]}`)}},
}

var adGroupAssetsUpdate = capability.Descriptor{
	ID: adGroupAssetsUpdateID, Version: 1, Title: "Change the status of ad group assets of Google Ads",
	Description: "Pause or enable the use of an asset by an ad group (an update with the field mask status). " +
		"Ad group, asset, and field type cannot be changed: they name the link. A link is ended with " +
		"ad-group-assets.remove, not with a status." + adGroupAssetsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		changeNote,
	Tags: []string{"google", "ads", "assets", "adGroups", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, adGroupAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"ad_group_assets": adGroupAssetItems(schema{"status": adGroupAssetStatusSchema}, "status"),
		"validate_only":   validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "ad_group_assets")),
	OutputSchema: encoded(adGroupAssetsOutputSchema),
	Arguments: append(slices.Clone(adGroupAssetsArgumentDocs),
		capability.Argument{Name: "ad_group_assets", Required: true, Description: "1 to 50 entries, each with " +
			adGroupAssetKeyDoc + ", and status (ENABLED or PAUSED). Each ad group, asset, and field type at most once"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: adGroupAssetsFields,
	Examples: []capability.Example{{Description: "Pause a link",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ad_group_assets":[{"ad_group_id":"22","asset_id":"33","field_type":"SITELINK","status":"PAUSED"}]}`)}},
}

var adGroupAssetsRemove = capability.Descriptor{
	ID: adGroupAssetsRemoveID, Version: 1, Title: "Remove assets from Google Ads ad groups",
	Description: "End the use of assets by ad groups of a Google Ads account (a remove of the resource name " +
		"built from ad_group_id, asset_id, and field_type). The asset itself stays in the account. Google marks " +
		"a removed link REMOVED; no tool of this provider brings it back, but the same asset can be attached " +
		"again." + adGroupAssetsNote +
		" Offered by a connection with the delete effect or whose tools list names it; no profile holds it." +
		changeNote,
	Tags: []string{"google", "ads", "assets", "adGroups", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, adGroupAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"ad_group_assets": adGroupAssetItems(nil),
		"validate_only":   validateOnlySchema,
	}, "customer_id", "ad_group_assets")),
	OutputSchema: encoded(adGroupAssetsOutputSchema),
	Arguments: append(slices.Clone(adGroupAssetsArgumentDocs),
		capability.Argument{Name: "ad_group_assets", Required: true, Description: "1 to 50 entries, each with " +
			adGroupAssetKeyDoc + ". Each ad group, asset, and field type at most once"},
		validateOnlyArgument),
	Fields: adGroupAssetsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a link without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","ad_group_assets":[{"ad_group_id":"22","asset_id":"33","field_type":"SITELINK"}],"validate_only":true}`)}},
}

// --- handlers ---

func handleAdGroupAssetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "attach ad group assets"
	var args adGroupAssetsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return adGroupAssetsSend(ctx, resolved, secrets, red, op, adGroupAssetsCreateID, args.CustomerID, ops, keys,
		args.ValidateOnly, args.ResponseContentType)
}

func handleAdGroupAssetsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update ad group assets"
	var args adGroupAssetsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return adGroupAssetsSend(ctx, resolved, secrets, red, op, adGroupAssetsUpdateID, args.CustomerID, ops, keys,
		args.ValidateOnly, args.ResponseContentType)
}

func handleAdGroupAssetsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove ad group assets"
	var args adGroupAssetsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return adGroupAssetsSend(ctx, resolved, secrets, red, op, adGroupAssetsRemoveID, args.CustomerID, ops, keys,
		args.ValidateOnly, "")
}

func adGroupAssetsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID string, ops []any, keys []assetLinkKey, validateOnly bool, content string) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Results             []map[string]json.RawMessage `json:"results"`
		PartialFailureError json.RawMessage              `json:"partialFailureError"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: adGroupAssetsService,
		body: mutateOperationsBody(ops, validateOnly, content)}, &answer); err != nil {
		return nil, err
	}
	done, err := adGroupAssetKind.results(op, customerID, answer.Results, answer.PartialFailureError, keys, validateOnly)
	if err != nil {
		return nil, err
	}
	return adGroupAssetsOutcome(customerID, validateOnly, done), nil
}

// --- confirmation ---

func adGroupAssetsKeyText(k adGroupAssetKey) string {
	return boundedText(k.FieldType, maxCodeText) + " asset " + boundedText(k.AssetID, maxCodeText) + " on ad group " +
		boundedText(k.AdGroupID, maxCodeText)
}

func adGroupAssetsCreateSummary(raw json.RawMessage) string {
	var a adGroupAssetsCreateArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.AdGroupAssets), func(i int) string {
		c := a.AdGroupAssets[i]
		text := adGroupAssetsKeyText(c.adGroupAssetKey)
		if c.Status != nil {
			text += " " + boundedText(*c.Status, maxCodeText)
		}
		return text
	})
	return assetLinksLead(a.ValidateOnly) + "Attach " + strconv.Itoa(len(a.AdGroupAssets)) +
		" asset(s) to ad groups of Google Ads account " + a.CustomerID + ": " + list + "."
}

func adGroupAssetsUpdateSummary(raw json.RawMessage) string {
	var a adGroupAssetsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.AdGroupAssets), func(i int) string {
		c := a.AdGroupAssets[i]
		return adGroupAssetsKeyText(c.adGroupAssetKey) + " to " + boundedText(c.Status, maxCodeText)
	})
	return assetLinksLead(a.ValidateOnly) + "Set the status of " + strconv.Itoa(len(a.AdGroupAssets)) +
		" ad group asset(s) of Google Ads account " + a.CustomerID + ": " + list + "."
}

func adGroupAssetsRemoveSummary(raw json.RawMessage) string {
	var a adGroupAssetsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.AdGroupAssets), func(i int) string {
		return adGroupAssetsKeyText(a.AdGroupAssets[i])
	})
	return assetLinksLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.AdGroupAssets)) +
		" asset(s) from ad groups of Google Ads account " + a.CustomerID + ": " + list +
		". Google marks the links removed; the assets stay in the account."
}
