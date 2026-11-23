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
	campaignAssetsCreateID = Provider + ".campaign-assets.create"
	campaignAssetsUpdateID = Provider + ".campaign-assets.update"
	campaignAssetsRemoveID = Provider + ".campaign-assets.remove"

	campaignAssetsService = "/campaignAssets"
)

// CampaignAssetService.MutateCampaignAssets takes operations[]. A link is named by campaign, asset, and field
// type, which are immutable and make up its resource name; status is the only field an update changes.

var campaignAssetKind = assetLinkKind{collection: "campaignAssets", parentCollection: "campaigns", parentKey: "campaign",
	parentNoun: "campaign", parentArgument: "campaign_id", resourceKey: "campaignAsset"}

type (
	campaignAssetKey struct {
		CampaignID string `json:"campaign_id"`
		AssetID    string `json:"asset_id"`
		FieldType  string `json:"field_type"`
	}
	campaignAssetNew struct {
		campaignAssetKey
		Status *string `json:"status"`
	}
	campaignAssetEdit struct {
		campaignAssetKey
		Status string `json:"status"`
	}
	campaignAssetsCreateArguments struct {
		CustomerID          string             `json:"customer_id"`
		CampaignAssets      []campaignAssetNew `json:"campaign_assets"`
		ValidateOnly        bool               `json:"validate_only"`
		ResponseContentType string             `json:"response_content_type"`
	}
	campaignAssetsUpdateArguments struct {
		CustomerID          string              `json:"customer_id"`
		CampaignAssets      []campaignAssetEdit `json:"campaign_assets"`
		ValidateOnly        bool                `json:"validate_only"`
		ResponseContentType string              `json:"response_content_type"`
	}
	campaignAssetsRemoveArguments struct {
		CustomerID     string             `json:"customer_id"`
		CampaignAssets []campaignAssetKey `json:"campaign_assets"`
		ValidateOnly   bool               `json:"validate_only"`
	}
)

func (k campaignAssetKey) key() assetLinkKey {
	return assetLinkKey{ParentID: k.CampaignID, AssetID: k.AssetID, FieldType: k.FieldType}
}

func campaignAssetPath(i int) string { return "campaign_assets[" + strconv.Itoa(i) + "]." }

// campaignAssetKeys checks every key and refuses one that is named twice.
func campaignAssetKeys(customerID string, keys []campaignAssetKey) ([]assetLinkKey, error) {
	if !customerIDPattern.MatchString(customerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := assetLinkCheckCount("campaign_assets", len(keys)); err != nil {
		return nil, err
	}
	out := make([]assetLinkKey, 0, len(keys))
	for i, k := range keys {
		if err := campaignAssetKind.check(campaignAssetPath(i), k.key()); err != nil {
			return nil, err
		}
		if slices.Contains(out, k.key()) {
			return nil, badRequest("campaign_assets names each campaign, asset, and field_type at most once")
		}
		out = append(out, k.key())
	}
	return out, nil
}

// operations checks the arguments and builds the operations of the request with the keys they name.
func (a campaignAssetsCreateArguments) operations() ([]any, []assetLinkKey, error) {
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	keys := make([]campaignAssetKey, 0, len(a.CampaignAssets))
	for _, c := range a.CampaignAssets {
		keys = append(keys, c.campaignAssetKey)
	}
	checked, err := campaignAssetKeys(a.CustomerID, keys)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(keys))
	for i, c := range a.CampaignAssets {
		create := map[string]any{
			"campaign":  campaignAssetKind.parentName(a.CustomerID, c.CampaignID),
			"asset":     customerName(a.CustomerID) + "/assets/" + c.AssetID,
			"fieldType": c.FieldType,
		}
		if c.Status != nil {
			if err := assetLinkCheckStatus(campaignAssetPath(i), *c.Status); err != nil {
				return nil, nil, err
			}
			create["status"] = *c.Status
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, checked, nil
}

func (a campaignAssetsUpdateArguments) operations() ([]any, []assetLinkKey, error) {
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	keys := make([]campaignAssetKey, 0, len(a.CampaignAssets))
	for _, c := range a.CampaignAssets {
		keys = append(keys, c.campaignAssetKey)
	}
	checked, err := campaignAssetKeys(a.CustomerID, keys)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(keys))
	for i, c := range a.CampaignAssets {
		if err := assetLinkCheckStatus(campaignAssetPath(i), c.Status); err != nil {
			return nil, nil, err
		}
		ops = append(ops, map[string]any{
			"update":     map[string]any{"resourceName": campaignAssetKind.name(a.CustomerID, checked[i]), "status": c.Status},
			"updateMask": "status"})
	}
	return ops, checked, nil
}

func (a campaignAssetsRemoveArguments) operations() ([]any, []assetLinkKey, error) {
	checked, err := campaignAssetKeys(a.CustomerID, a.CampaignAssets)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(checked))
	for _, k := range checked {
		ops = append(ops, map[string]any{"remove": campaignAssetKind.name(a.CustomerID, k)})
	}
	return ops, checked, nil
}

// --- results ---

type (
	campaignAssetObject struct {
		Campaign string `json:"campaign,omitempty"`
		assetLinkObject
	}
	campaignAssetMutated struct {
		CampaignID    string               `json:"campaign_id"`
		AssetID       string               `json:"asset_id"`
		FieldType     string               `json:"field_type"`
		ResourceName  string               `json:"resource_name"`
		CampaignAsset *campaignAssetObject `json:"campaign_asset,omitempty"`
	}
	campaignAssetsChanged struct {
		CustomerID   string                 `json:"customer_id"`
		ValidateOnly bool                   `json:"validate_only"`
		Results      []campaignAssetMutated `json:"results"`
	}
)

func campaignAssetsOutcome(customerID string, validateOnly bool, done []assetLinkDone) campaignAssetsChanged {
	out := campaignAssetsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []campaignAssetMutated{}}
	for _, d := range done {
		item := campaignAssetMutated{CampaignID: d.Key.ParentID, AssetID: d.Key.AssetID, FieldType: d.Key.FieldType,
			ResourceName: d.ResourceName}
		if d.Link != nil {
			item.CampaignAsset = &campaignAssetObject{Campaign: d.Parent, assetLinkObject: *d.Link}
		}
		out.Results = append(out.Results, item)
	}
	return out
}

// --- descriptors ---

var (
	campaignAssetIDSchema        = schema{"type": "string", "pattern": numericIDPattern.String()}
	campaignAssetFieldTypeSchema = schema{"type": "string", "enum": assetLinkFieldTypes}
	campaignAssetStatusSchema    = schema{"type": "string", "enum": assetLinkStatuses}

	campaignAssetKeyProperties = func() schema {
		return schema{"campaign_id": campaignAssetIDSchema, "asset_id": campaignAssetIDSchema,
			"field_type": campaignAssetFieldTypeSchema}
	}

	campaignAssetObjectSchema = object(schema{"campaign": stringSchema, "resource_name": stringSchema,
		"asset": stringSchema, "field_type": stringSchema, "status": stringSchema, "source": stringSchema,
		"primary_status": stringSchema, "primary_status_reasons": arrayOf(stringSchema),
		"primary_status_details": arrayOf(object(schema{"status": stringSchema, "reason": stringSchema,
			"asset_disapproved": object(schema{"offline_evaluation_error_reasons": arrayOf(stringSchema)})}))},
		"resource_name")
	campaignAssetsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"campaign_id": stringSchema, "asset_id": stringSchema,
			"field_type": stringSchema, "resource_name": stringSchema, "campaign_asset": campaignAssetObjectSchema},
			"campaign_id", "asset_id", "field_type", "resource_name"))},
		"customer_id", "validate_only", "results")
)

func campaignAssetItems(extra schema, required ...string) schema {
	properties := campaignAssetKeyProperties()
	for name, s := range extra {
		properties[name] = s
	}
	return schema{"type": "array", "minItems": 1, "maxItems": assetLinksMaxOperations,
		"items": object(properties, append([]string{"campaign_id", "asset_id", "field_type"}, required...)...)}
}

const campaignAssetsSensitivity = "which assets a campaign of a Google Ads account uses and in which role, " +
	"which the confirmation names; no data of other people"

const campaignAssetsNote = " A campaign asset is the use of an existing asset by a campaign for one field type, " +
	"named by campaign_id, asset_id, and field_type, from which the resource name " +
	"customers/ID/campaignAssets/CAMPAIGN_ID~ASSET_ID~FIELD_TYPE is built; this tool neither creates assets " +
	"nor reads links (read them with reports.search on campaign_asset). Which field type fits which asset and " +
	"which campaign type is up to Google. The request goes to CampaignAssetService.MutateCampaignAssets with up " +
	"to 50 operations, atomic and without partial_failure: Google applies all of them or none."

var campaignAssetsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: campaign_id, asset_id, " +
		"field_type, resource_name (customers/ID/campaignAssets/CAMPAIGN_ID~ASSET_ID~FIELD_TYPE, as Google " +
		"answered, always of the account and the link of the call), and with MUTABLE_RESOURCE campaign_asset, " +
		"the link after the change as Google returns it: campaign, resource_name, asset, field_type, status and " +
		"the fields Google fills in (source, primary_status, primary_status_reasons, primary_status_details " +
		"with status, reason and the offline evaluation reasons of a disapproved asset). Empty after validate_only"},
}

var campaignAssetsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

const campaignAssetKeyDoc = "campaign_id (ID of a campaign of the account), asset_id (ID of an asset of the " +
	"account), field_type (name of the asset field type, such as HEADLINE, SITELINK or CALLOUT); the IDs are " +
	"positive numbers as reports.search returns them, never a temporary ID"

var campaignAssetsCreate = capability.Descriptor{
	ID: campaignAssetsCreateID, Version: 1, Title: "Attach assets to Google Ads campaigns",
	Description: "Attach existing assets to campaigns of a Google Ads account, each for one field type, " +
		"optionally with the status ENABLED or PAUSED." + campaignAssetsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "assets", "campaigns", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, campaignAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"campaign_assets": campaignAssetItems(schema{"status": campaignAssetStatusSchema}),
		"validate_only":   validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "campaign_assets")),
	OutputSchema: encoded(campaignAssetsOutputSchema),
	Arguments: append(slices.Clone(campaignAssetsArgumentDocs),
		capability.Argument{Name: "campaign_assets", Required: true, Description: "1 to 50 entries, each with " +
			campaignAssetKeyDoc + ", and optionally status (ENABLED or PAUSED; left out, Google decides). Each " +
			"campaign, asset, and field type at most once"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: campaignAssetsFields,
	Examples: []capability.Example{{Description: "Attach a sitelink asset to a campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_assets":[{"campaign_id":"22","asset_id":"33","field_type":"SITELINK"}]}`)}},
}

var campaignAssetsUpdate = capability.Descriptor{
	ID: campaignAssetsUpdateID, Version: 1, Title: "Change the status of campaign assets of Google Ads",
	Description: "Pause or enable the use of an asset by a campaign (an update with the field mask status). " +
		"Campaign, asset, and field type cannot be changed: they name the link. A link is ended with " +
		"campaign-assets.remove, not with a status." + campaignAssetsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "assets", "campaigns", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, campaignAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"campaign_assets": campaignAssetItems(schema{"status": campaignAssetStatusSchema}, "status"),
		"validate_only":   validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "campaign_assets")),
	OutputSchema: encoded(campaignAssetsOutputSchema),
	Arguments: append(slices.Clone(campaignAssetsArgumentDocs),
		capability.Argument{Name: "campaign_assets", Required: true, Description: "1 to 50 entries, each with " +
			campaignAssetKeyDoc + ", and status (ENABLED or PAUSED). Each campaign, asset, and field type at most once"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: campaignAssetsFields,
	Examples: []capability.Example{{Description: "Pause a link",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_assets":[{"campaign_id":"22","asset_id":"33","field_type":"SITELINK","status":"PAUSED"}]}`)}},
}

var campaignAssetsRemove = capability.Descriptor{
	ID: campaignAssetsRemoveID, Version: 1, Title: "Remove assets from Google Ads campaigns",
	Description: "End the use of assets by campaigns of a Google Ads account (a remove of the resource name " +
		"built from campaign_id, asset_id, and field_type). The asset itself stays in the account. Google marks " +
		"a removed link REMOVED; no tool of this provider brings it back, but the same asset can be attached " +
		"again." + campaignAssetsNote +
		" Offered by a connection with the delete effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "assets", "campaigns", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, campaignAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"campaign_assets": campaignAssetItems(nil),
		"validate_only":   validateOnlySchema,
	}, "customer_id", "campaign_assets")),
	OutputSchema: encoded(campaignAssetsOutputSchema),
	Arguments: append(slices.Clone(campaignAssetsArgumentDocs),
		capability.Argument{Name: "campaign_assets", Required: true, Description: "1 to 50 entries, each with " +
			campaignAssetKeyDoc + ". Each campaign, asset, and field type at most once"},
		validateOnlyArgument),
	Fields: campaignAssetsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a link without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_assets":[{"campaign_id":"22","asset_id":"33","field_type":"SITELINK"}],"validate_only":true}`)}},
}

// --- handlers ---

func handleCampaignAssetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "attach campaign assets"
	var args campaignAssetsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignAssetsSend(ctx, resolved, secrets, red, op, campaignAssetsCreateID, args.CustomerID, ops, keys,
		args.ValidateOnly, args.ResponseContentType)
}

func handleCampaignAssetsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update campaign assets"
	var args campaignAssetsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignAssetsSend(ctx, resolved, secrets, red, op, campaignAssetsUpdateID, args.CustomerID, ops, keys,
		args.ValidateOnly, args.ResponseContentType)
}

func handleCampaignAssetsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove campaign assets"
	var args campaignAssetsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignAssetsSend(ctx, resolved, secrets, red, op, campaignAssetsRemoveID, args.CustomerID, ops, keys,
		args.ValidateOnly, "")
}

func campaignAssetsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
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
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: campaignAssetsService,
		body: mutateOperationsBody(ops, validateOnly, content)}, &answer); err != nil {
		return nil, err
	}
	done, err := campaignAssetKind.results(op, customerID, answer.Results, answer.PartialFailureError, keys, validateOnly)
	if err != nil {
		return nil, err
	}
	return campaignAssetsOutcome(customerID, validateOnly, done), nil
}

// --- confirmation ---

func campaignAssetsKeyText(k campaignAssetKey) string {
	return boundedText(k.FieldType, maxCodeText) + " asset " + boundedText(k.AssetID, maxCodeText) + " on campaign " +
		boundedText(k.CampaignID, maxCodeText)
}

func campaignAssetsCreateSummary(raw json.RawMessage) string {
	var a campaignAssetsCreateArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.CampaignAssets), func(i int) string {
		c := a.CampaignAssets[i]
		text := campaignAssetsKeyText(c.campaignAssetKey)
		if c.Status != nil {
			text += " " + boundedText(*c.Status, maxCodeText)
		}
		return text
	})
	return assetLinksLead(a.ValidateOnly) + "Attach " + strconv.Itoa(len(a.CampaignAssets)) +
		" asset(s) to campaigns of Google Ads account " + a.CustomerID + ": " + list + "."
}

func campaignAssetsUpdateSummary(raw json.RawMessage) string {
	var a campaignAssetsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.CampaignAssets), func(i int) string {
		c := a.CampaignAssets[i]
		return campaignAssetsKeyText(c.campaignAssetKey) + " to " + boundedText(c.Status, maxCodeText)
	})
	return assetLinksLead(a.ValidateOnly) + "Set the status of " + strconv.Itoa(len(a.CampaignAssets)) +
		" campaign asset(s) of Google Ads account " + a.CustomerID + ": " + list + "."
}

func campaignAssetsRemoveSummary(raw json.RawMessage) string {
	var a campaignAssetsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.CampaignAssets), func(i int) string {
		return campaignAssetsKeyText(a.CampaignAssets[i])
	})
	return assetLinksLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.CampaignAssets)) +
		" asset(s) from campaigns of Google Ads account " + a.CustomerID + ": " + list +
		". Google marks the links removed; the assets stay in the account."
}
