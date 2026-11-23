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
	assetGroupAssetsCreateID = Provider + ".asset-group-assets.create"
	assetGroupAssetsUpdateID = Provider + ".asset-group-assets.update"
	assetGroupAssetsRemoveID = Provider + ".asset-group-assets.remove"

	assetGroupAssetsService = "/assetGroupAssets"
)

// AssetGroupAssetService.MutateAssetGroupAssets takes operations[] and has no response_content_type: a result
// holds the resource name only. A link is named by asset group, asset, and field
// type, which are immutable and make up its resource name; status is the only field an update changes.

var assetGroupAssetKind = assetLinkKind{collection: "assetGroupAssets", parentCollection: "assetGroups", parentKey: "assetGroup",
	parentNoun: "asset group", parentArgument: "asset_group_id"}

type (
	assetGroupAssetKey struct {
		AssetGroupID string `json:"asset_group_id"`
		AssetID      string `json:"asset_id"`
		FieldType    string `json:"field_type"`
	}
	assetGroupAssetNew struct {
		assetGroupAssetKey
		Status *string `json:"status"`
	}
	assetGroupAssetEdit struct {
		assetGroupAssetKey
		Status string `json:"status"`
	}
	assetGroupAssetsCreateArguments struct {
		CustomerID       string               `json:"customer_id"`
		AssetGroupAssets []assetGroupAssetNew `json:"asset_group_assets"`
		ValidateOnly     bool                 `json:"validate_only"`
	}
	assetGroupAssetsUpdateArguments struct {
		CustomerID       string                `json:"customer_id"`
		AssetGroupAssets []assetGroupAssetEdit `json:"asset_group_assets"`
		ValidateOnly     bool                  `json:"validate_only"`
	}
	assetGroupAssetsRemoveArguments struct {
		CustomerID       string               `json:"customer_id"`
		AssetGroupAssets []assetGroupAssetKey `json:"asset_group_assets"`
		ValidateOnly     bool                 `json:"validate_only"`
	}
)

func (k assetGroupAssetKey) key() assetLinkKey {
	return assetLinkKey{ParentID: k.AssetGroupID, AssetID: k.AssetID, FieldType: k.FieldType}
}

func assetGroupAssetPath(i int) string { return "asset_group_assets[" + strconv.Itoa(i) + "]." }

// assetGroupAssetKeys checks every key and refuses one that is named twice.
func assetGroupAssetKeys(customerID string, keys []assetGroupAssetKey) ([]assetLinkKey, error) {
	if !customerIDPattern.MatchString(customerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := assetLinkCheckCount("asset_group_assets", len(keys)); err != nil {
		return nil, err
	}
	out := make([]assetLinkKey, 0, len(keys))
	for i, k := range keys {
		if err := assetGroupAssetKind.check(assetGroupAssetPath(i), k.key()); err != nil {
			return nil, err
		}
		if slices.Contains(out, k.key()) {
			return nil, badRequest("asset_group_assets names each asset group, asset, and field_type at most once")
		}
		out = append(out, k.key())
	}
	return out, nil
}

// operations checks the arguments and builds the operations of the request with the keys they name.
func (a assetGroupAssetsCreateArguments) operations() ([]any, []assetLinkKey, error) {
	keys := make([]assetGroupAssetKey, 0, len(a.AssetGroupAssets))
	for _, c := range a.AssetGroupAssets {
		keys = append(keys, c.assetGroupAssetKey)
	}
	checked, err := assetGroupAssetKeys(a.CustomerID, keys)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(keys))
	for i, c := range a.AssetGroupAssets {
		create := map[string]any{
			"assetGroup": assetGroupAssetKind.parentName(a.CustomerID, c.AssetGroupID),
			"asset":      customerName(a.CustomerID) + "/assets/" + c.AssetID,
			"fieldType":  c.FieldType,
		}
		if c.Status != nil {
			if err := assetLinkCheckStatus(assetGroupAssetPath(i), *c.Status); err != nil {
				return nil, nil, err
			}
			create["status"] = *c.Status
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, checked, nil
}

func (a assetGroupAssetsUpdateArguments) operations() ([]any, []assetLinkKey, error) {
	keys := make([]assetGroupAssetKey, 0, len(a.AssetGroupAssets))
	for _, c := range a.AssetGroupAssets {
		keys = append(keys, c.assetGroupAssetKey)
	}
	checked, err := assetGroupAssetKeys(a.CustomerID, keys)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(keys))
	for i, c := range a.AssetGroupAssets {
		if err := assetLinkCheckStatus(assetGroupAssetPath(i), c.Status); err != nil {
			return nil, nil, err
		}
		ops = append(ops, map[string]any{
			"update":     map[string]any{"resourceName": assetGroupAssetKind.name(a.CustomerID, checked[i]), "status": c.Status},
			"updateMask": "status"})
	}
	return ops, checked, nil
}

func (a assetGroupAssetsRemoveArguments) operations() ([]any, []assetLinkKey, error) {
	checked, err := assetGroupAssetKeys(a.CustomerID, a.AssetGroupAssets)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(checked))
	for _, k := range checked {
		ops = append(ops, map[string]any{"remove": assetGroupAssetKind.name(a.CustomerID, k)})
	}
	return ops, checked, nil
}

// --- results ---

type (
	assetGroupAssetMutated struct {
		AssetGroupID string `json:"asset_group_id"`
		AssetID      string `json:"asset_id"`
		FieldType    string `json:"field_type"`
		ResourceName string `json:"resource_name"`
	}
	assetGroupAssetsChanged struct {
		CustomerID   string                   `json:"customer_id"`
		ValidateOnly bool                     `json:"validate_only"`
		Results      []assetGroupAssetMutated `json:"results"`
	}
)

func assetGroupAssetsOutcome(customerID string, validateOnly bool, done []assetLinkDone) assetGroupAssetsChanged {
	out := assetGroupAssetsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []assetGroupAssetMutated{}}
	for _, d := range done {
		item := assetGroupAssetMutated{AssetGroupID: d.Key.ParentID, AssetID: d.Key.AssetID, FieldType: d.Key.FieldType,
			ResourceName: d.ResourceName}
		out.Results = append(out.Results, item)
	}
	return out
}

// --- descriptors ---

var (
	assetGroupAssetIDSchema        = schema{"type": "string", "pattern": numericIDPattern.String()}
	assetGroupAssetFieldTypeSchema = schema{"type": "string", "enum": assetLinkFieldTypes}
	assetGroupAssetStatusSchema    = schema{"type": "string", "enum": assetLinkStatuses}

	assetGroupAssetKeyProperties = func() schema {
		return schema{"asset_group_id": assetGroupAssetIDSchema, "asset_id": assetGroupAssetIDSchema,
			"field_type": assetGroupAssetFieldTypeSchema}
	}

	assetGroupAssetsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"asset_group_id": stringSchema, "asset_id": stringSchema,
			"field_type": stringSchema, "resource_name": stringSchema},
			"asset_group_id", "asset_id", "field_type", "resource_name"))},
		"customer_id", "validate_only", "results")
)

func assetGroupAssetItems(extra schema, required ...string) schema {
	properties := assetGroupAssetKeyProperties()
	for name, s := range extra {
		properties[name] = s
	}
	return schema{"type": "array", "minItems": 1, "maxItems": assetLinksMaxOperations,
		"items": object(properties, append([]string{"asset_group_id", "asset_id", "field_type"}, required...)...)}
}

const assetGroupAssetsSensitivity = "which assets an asset group of a Google Ads account uses and in which role, " +
	"which the confirmation names; no data of other people"

const assetGroupAssetsNote = " An asset group asset is the use of an existing asset by an asset group for one field type, " +
	"named by asset_group_id, asset_id, and field_type, from which the resource name " +
	"customers/ID/assetGroupAssets/ASSET_GROUP_ID~ASSET_ID~FIELD_TYPE is built; this tool neither creates assets " +
	"nor reads links (read them with reports.search on asset_group_asset, which also shows the policy summary). Which field type fits which asset and " +
	"which asset group type is up to Google. The request goes to AssetGroupAssetService.MutateAssetGroupAssets with up " +
	"to 50 operations, atomic and without partial_failure: Google applies all of them or none."

var assetGroupAssetsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: asset_group_id, asset_id, " +
		"field_type, resource_name (customers/ID/assetGroupAssets/ASSET_GROUP_ID~ASSET_ID~FIELD_TYPE, as Google " +
		"answered, always of the account and the link of the call). Google returns nothing else per result, so the " +
		"link itself is read with reports.search. Empty after validate_only"},
}

var assetGroupAssetsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

const assetGroupAssetKeyDoc = "asset_group_id (ID of an asset group of the account), asset_id (ID of an asset of the " +
	"account), field_type (name of the asset field type, such as HEADLINE, SITELINK or CALLOUT); the IDs are " +
	"positive numbers as reports.search returns them, never a temporary ID"

var assetGroupAssetsCreate = capability.Descriptor{
	ID: assetGroupAssetsCreateID, Version: 1, Title: "Attach assets to Google Ads asset groups",
	Description: "Attach existing assets to asset groups of a Google Ads account, each for one field type, " +
		"optionally with the status ENABLED or PAUSED." + assetGroupAssetsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "assets", "assetGroups", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, assetGroupAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":        customerIDArgument,
		"asset_group_assets": assetGroupAssetItems(schema{"status": assetGroupAssetStatusSchema}),
		"validate_only":      validateOnlySchema,
	}, "customer_id", "asset_group_assets")),
	OutputSchema: encoded(assetGroupAssetsOutputSchema),
	Arguments: append(slices.Clone(assetGroupAssetsArgumentDocs),
		capability.Argument{Name: "asset_group_assets", Required: true, Description: "1 to 50 entries, each with " +
			assetGroupAssetKeyDoc + ", and optionally status (ENABLED or PAUSED; left out, Google decides). Each " +
			"asset group, asset, and field type at most once"},
		validateOnlyArgument),
	Fields: assetGroupAssetsFields,
	Examples: []capability.Example{{Description: "Attach a sitelink asset to an asset group",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","asset_group_assets":[{"asset_group_id":"22","asset_id":"33","field_type":"SITELINK"}]}`)}},
}

var assetGroupAssetsUpdate = capability.Descriptor{
	ID: assetGroupAssetsUpdateID, Version: 1, Title: "Change the status of asset group assets of Google Ads",
	Description: "Pause or enable the use of an asset by an asset group (an update with the field mask status). " +
		"Asset group, asset, and field type cannot be changed: they name the link. A link is ended with " +
		"asset-group-assets.remove, not with a status." + assetGroupAssetsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "assets", "assetGroups", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, assetGroupAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":        customerIDArgument,
		"asset_group_assets": assetGroupAssetItems(schema{"status": assetGroupAssetStatusSchema}, "status"),
		"validate_only":      validateOnlySchema,
	}, "customer_id", "asset_group_assets")),
	OutputSchema: encoded(assetGroupAssetsOutputSchema),
	Arguments: append(slices.Clone(assetGroupAssetsArgumentDocs),
		capability.Argument{Name: "asset_group_assets", Required: true, Description: "1 to 50 entries, each with " +
			assetGroupAssetKeyDoc + ", and status (ENABLED or PAUSED). Each asset group, asset, and field type at most once"},
		validateOnlyArgument),
	Fields: assetGroupAssetsFields,
	Examples: []capability.Example{{Description: "Pause a link",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","asset_group_assets":[{"asset_group_id":"22","asset_id":"33","field_type":"SITELINK","status":"PAUSED"}]}`)}},
}

var assetGroupAssetsRemove = capability.Descriptor{
	ID: assetGroupAssetsRemoveID, Version: 1, Title: "Remove assets from Google Ads asset groups",
	Description: "End the use of assets by asset groups of a Google Ads account (a remove of the resource name " +
		"built from asset_group_id, asset_id, and field_type). The asset itself stays in the account. Google marks " +
		"a removed link REMOVED; no tool of this provider brings it back, but the same asset can be attached " +
		"again." + assetGroupAssetsNote +
		" Offered by a connection with the delete effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "assets", "assetGroups", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, assetGroupAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":        customerIDArgument,
		"asset_group_assets": assetGroupAssetItems(nil),
		"validate_only":      validateOnlySchema,
	}, "customer_id", "asset_group_assets")),
	OutputSchema: encoded(assetGroupAssetsOutputSchema),
	Arguments: append(slices.Clone(assetGroupAssetsArgumentDocs),
		capability.Argument{Name: "asset_group_assets", Required: true, Description: "1 to 50 entries, each with " +
			assetGroupAssetKeyDoc + ". Each asset group, asset, and field type at most once"},
		validateOnlyArgument),
	Fields: assetGroupAssetsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a link without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","asset_group_assets":[{"asset_group_id":"22","asset_id":"33","field_type":"SITELINK"}],"validate_only":true}`)}},
}

// --- handlers ---

func handleAssetGroupAssetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "attach asset group assets"
	var args assetGroupAssetsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return assetGroupAssetsSend(ctx, resolved, secrets, red, op, assetGroupAssetsCreateID, args.CustomerID, ops, keys,
		args.ValidateOnly)
}

func handleAssetGroupAssetsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update asset group assets"
	var args assetGroupAssetsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return assetGroupAssetsSend(ctx, resolved, secrets, red, op, assetGroupAssetsUpdateID, args.CustomerID, ops, keys,
		args.ValidateOnly)
}

func handleAssetGroupAssetsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove asset group assets"
	var args assetGroupAssetsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return assetGroupAssetsSend(ctx, resolved, secrets, red, op, assetGroupAssetsRemoveID, args.CustomerID, ops, keys,
		args.ValidateOnly)
}

func assetGroupAssetsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID string, ops []any, keys []assetLinkKey, validateOnly bool) (any, error) {
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
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: assetGroupAssetsService,
		body: mutateOperationsBody(ops, validateOnly, "")}, &answer); err != nil {
		return nil, err
	}
	done, err := assetGroupAssetKind.results(op, customerID, answer.Results, answer.PartialFailureError, keys, validateOnly)
	if err != nil {
		return nil, err
	}
	return assetGroupAssetsOutcome(customerID, validateOnly, done), nil
}

// --- confirmation ---

func assetGroupAssetsKeyText(k assetGroupAssetKey) string {
	return boundedText(k.FieldType, maxCodeText) + " asset " + boundedText(k.AssetID, maxCodeText) + " on asset group " +
		boundedText(k.AssetGroupID, maxCodeText)
}

func assetGroupAssetsCreateSummary(raw json.RawMessage) string {
	var a assetGroupAssetsCreateArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.AssetGroupAssets), func(i int) string {
		c := a.AssetGroupAssets[i]
		text := assetGroupAssetsKeyText(c.assetGroupAssetKey)
		if c.Status != nil {
			text += " " + boundedText(*c.Status, maxCodeText)
		}
		return text
	})
	return assetLinksLead(a.ValidateOnly) + "Attach " + strconv.Itoa(len(a.AssetGroupAssets)) +
		" asset(s) to asset groups of Google Ads account " + a.CustomerID + ": " + list + "."
}

func assetGroupAssetsUpdateSummary(raw json.RawMessage) string {
	var a assetGroupAssetsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.AssetGroupAssets), func(i int) string {
		c := a.AssetGroupAssets[i]
		return assetGroupAssetsKeyText(c.assetGroupAssetKey) + " to " + boundedText(c.Status, maxCodeText)
	})
	return assetLinksLead(a.ValidateOnly) + "Set the status of " + strconv.Itoa(len(a.AssetGroupAssets)) +
		" asset group asset(s) of Google Ads account " + a.CustomerID + ": " + list + "."
}

func assetGroupAssetsRemoveSummary(raw json.RawMessage) string {
	var a assetGroupAssetsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.AssetGroupAssets), func(i int) string {
		return assetGroupAssetsKeyText(a.AssetGroupAssets[i])
	})
	return assetLinksLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.AssetGroupAssets)) +
		" asset(s) from asset groups of Google Ads account " + a.CustomerID + ": " + list +
		". Google marks the links removed; the assets stay in the account."
}
