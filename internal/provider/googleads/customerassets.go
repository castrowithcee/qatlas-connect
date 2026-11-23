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
	customerAssetsCreateID = Provider + ".customer-assets.create"
	customerAssetsUpdateID = Provider + ".customer-assets.update"
	customerAssetsRemoveID = Provider + ".customer-assets.remove"

	customerAssetsService = "/customerAssets"
)

// CustomerAssetService.MutateCustomerAssets takes operations[]. A link is named by asset and field type, which are
// immutable and make up its resource name; the account itself is the parent. Status is the only field an update
// changes.

var customerAssetKind = assetLinkKind{collection: "customerAssets", resourceKey: "customerAsset"}

type (
	customerAssetKey struct {
		AssetID   string `json:"asset_id"`
		FieldType string `json:"field_type"`
	}
	customerAssetNew struct {
		customerAssetKey
		Status *string `json:"status"`
	}
	customerAssetEdit struct {
		customerAssetKey
		Status string `json:"status"`
	}
	customerAssetsCreateArguments struct {
		CustomerID          string             `json:"customer_id"`
		CustomerAssets      []customerAssetNew `json:"customer_assets"`
		ValidateOnly        bool               `json:"validate_only"`
		ResponseContentType string             `json:"response_content_type"`
	}
	customerAssetsUpdateArguments struct {
		CustomerID          string              `json:"customer_id"`
		CustomerAssets      []customerAssetEdit `json:"customer_assets"`
		ValidateOnly        bool                `json:"validate_only"`
		ResponseContentType string              `json:"response_content_type"`
	}
	customerAssetsRemoveArguments struct {
		CustomerID     string             `json:"customer_id"`
		CustomerAssets []customerAssetKey `json:"customer_assets"`
		ValidateOnly   bool               `json:"validate_only"`
	}
)

func (k customerAssetKey) key() assetLinkKey {
	return assetLinkKey{AssetID: k.AssetID, FieldType: k.FieldType}
}

func customerAssetPath(i int) string { return "customer_assets[" + strconv.Itoa(i) + "]." }

// customerAssetKeys checks every key and refuses one that is named twice.
func customerAssetKeys(customerID string, keys []customerAssetKey) ([]assetLinkKey, error) {
	if !customerIDPattern.MatchString(customerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := assetLinkCheckCount("customer_assets", len(keys)); err != nil {
		return nil, err
	}
	out := make([]assetLinkKey, 0, len(keys))
	for i, k := range keys {
		if err := customerAssetKind.check(customerAssetPath(i), k.key()); err != nil {
			return nil, err
		}
		if slices.Contains(out, k.key()) {
			return nil, badRequest("customer_assets names each asset and field_type at most once")
		}
		out = append(out, k.key())
	}
	return out, nil
}

// operations checks the arguments and builds the operations of the request with the keys they name.
func (a customerAssetsCreateArguments) operations() ([]any, []assetLinkKey, error) {
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	keys := make([]customerAssetKey, 0, len(a.CustomerAssets))
	for _, c := range a.CustomerAssets {
		keys = append(keys, c.customerAssetKey)
	}
	checked, err := customerAssetKeys(a.CustomerID, keys)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(keys))
	for i, c := range a.CustomerAssets {
		create := map[string]any{
			"asset":     customerName(a.CustomerID) + "/assets/" + c.AssetID,
			"fieldType": c.FieldType,
		}
		if c.Status != nil {
			if err := assetLinkCheckStatus(customerAssetPath(i), *c.Status); err != nil {
				return nil, nil, err
			}
			create["status"] = *c.Status
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, checked, nil
}

func (a customerAssetsUpdateArguments) operations() ([]any, []assetLinkKey, error) {
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	keys := make([]customerAssetKey, 0, len(a.CustomerAssets))
	for _, c := range a.CustomerAssets {
		keys = append(keys, c.customerAssetKey)
	}
	checked, err := customerAssetKeys(a.CustomerID, keys)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(keys))
	for i, c := range a.CustomerAssets {
		if err := assetLinkCheckStatus(customerAssetPath(i), c.Status); err != nil {
			return nil, nil, err
		}
		ops = append(ops, map[string]any{
			"update":     map[string]any{"resourceName": customerAssetKind.name(a.CustomerID, checked[i]), "status": c.Status},
			"updateMask": "status"})
	}
	return ops, checked, nil
}

func (a customerAssetsRemoveArguments) operations() ([]any, []assetLinkKey, error) {
	checked, err := customerAssetKeys(a.CustomerID, a.CustomerAssets)
	if err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(checked))
	for _, k := range checked {
		ops = append(ops, map[string]any{"remove": customerAssetKind.name(a.CustomerID, k)})
	}
	return ops, checked, nil
}

// --- results ---

type (
	customerAssetObject struct {
		assetLinkObject
	}
	customerAssetMutated struct {
		AssetID       string               `json:"asset_id"`
		FieldType     string               `json:"field_type"`
		ResourceName  string               `json:"resource_name"`
		CustomerAsset *customerAssetObject `json:"customer_asset,omitempty"`
	}
	customerAssetsChanged struct {
		CustomerID   string                 `json:"customer_id"`
		ValidateOnly bool                   `json:"validate_only"`
		Results      []customerAssetMutated `json:"results"`
	}
)

func customerAssetsOutcome(customerID string, validateOnly bool, done []assetLinkDone) customerAssetsChanged {
	out := customerAssetsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: []customerAssetMutated{}}
	for _, d := range done {
		item := customerAssetMutated{AssetID: d.Key.AssetID, FieldType: d.Key.FieldType,
			ResourceName: d.ResourceName}
		if d.Link != nil {
			item.CustomerAsset = &customerAssetObject{assetLinkObject: *d.Link}
		}
		out.Results = append(out.Results, item)
	}
	return out
}

// --- descriptors ---

var (
	customerAssetIDSchema        = schema{"type": "string", "pattern": numericIDPattern.String()}
	customerAssetFieldTypeSchema = schema{"type": "string", "enum": assetLinkFieldTypes}
	customerAssetStatusSchema    = schema{"type": "string", "enum": assetLinkStatuses}

	customerAssetKeyProperties = func() schema {
		return schema{"asset_id": customerAssetIDSchema,
			"field_type": customerAssetFieldTypeSchema}
	}

	customerAssetObjectSchema = object(schema{"resource_name": stringSchema,
		"asset": stringSchema, "field_type": stringSchema, "status": stringSchema, "source": stringSchema,
		"primary_status": stringSchema, "primary_status_reasons": arrayOf(stringSchema),
		"primary_status_details": arrayOf(object(schema{"status": stringSchema, "reason": stringSchema,
			"asset_disapproved": object(schema{"offline_evaluation_error_reasons": arrayOf(stringSchema)})}))},
		"resource_name")
	customerAssetsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"asset_id": stringSchema,
			"field_type": stringSchema, "resource_name": stringSchema, "customer_asset": customerAssetObjectSchema},
			"asset_id", "field_type", "resource_name"))},
		"customer_id", "validate_only", "results")
)

func customerAssetItems(extra schema, required ...string) schema {
	properties := customerAssetKeyProperties()
	for name, s := range extra {
		properties[name] = s
	}
	return schema{"type": "array", "minItems": 1, "maxItems": assetLinksMaxOperations,
		"items": object(properties, append([]string{"asset_id", "field_type"}, required...)...)}
}

const customerAssetsSensitivity = "which assets a Google Ads account uses account-wide and in which role, " +
	"which the confirmation names; no data of other people"

const customerAssetsNote = " A customer asset is the use of an existing asset by the whole account for one field type, " +
	"named by asset_id and field_type, from which the resource name " +
	"customers/ID/customerAssets/ASSET_ID~FIELD_TYPE is built; this tool neither creates assets " +
	"nor reads links (read them with reports.search on customer_asset). Which field type fits which asset and " +
	"account is up to Google. The request goes to CustomerAssetService.MutateCustomerAssets with up " +
	"to 50 operations, atomic and without partial_failure: Google applies all of them or none."

var customerAssetsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: asset_id, " +
		"field_type, resource_name (customers/ID/customerAssets/ASSET_ID~FIELD_TYPE, as Google " +
		"answered, always of the account and the link of the call), and with MUTABLE_RESOURCE customer_asset, " +
		"the link after the change as Google returns it: resource_name, asset, field_type, status and " +
		"the fields Google fills in (source, primary_status, primary_status_reasons, primary_status_details " +
		"with status, reason and the offline evaluation reasons of a disapproved asset). Empty after validate_only"},
}

var customerAssetsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

const customerAssetKeyDoc = "asset_id (ID of an asset of the " +
	"account), field_type (name of the asset field type, such as HEADLINE, SITELINK or CALLOUT); the IDs are " +
	"positive numbers as reports.search returns them, never a temporary ID"

var customerAssetsCreate = capability.Descriptor{
	ID: customerAssetsCreateID, Version: 1, Title: "Attach assets to a Google Ads account",
	Description: "Attach existing assets to a Google Ads account as a whole, each for one field type, " +
		"optionally with the status ENABLED or PAUSED." + customerAssetsNote +
		" Offered by a connection with the create effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "assets", "account", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, customerAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"customer_assets": customerAssetItems(schema{"status": customerAssetStatusSchema}),
		"validate_only":   validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "customer_assets")),
	OutputSchema: encoded(customerAssetsOutputSchema),
	Arguments: append(slices.Clone(customerAssetsArgumentDocs),
		capability.Argument{Name: "customer_assets", Required: true, Description: "1 to 50 entries, each with " +
			customerAssetKeyDoc + ", and optionally status (ENABLED or PAUSED; left out, Google decides). Each " +
			"asset and field type at most once"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: customerAssetsFields,
	Examples: []capability.Example{{Description: "Attach a sitelink asset to the account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","customer_assets":[{"asset_id":"33","field_type":"SITELINK"}]}`)}},
}

var customerAssetsUpdate = capability.Descriptor{
	ID: customerAssetsUpdateID, Version: 1, Title: "Change the status of customer assets of Google Ads",
	Description: "Pause or enable the use of an asset by the account (an update with the field mask status). " +
		"Asset and field type cannot be changed: they name the link. A link is ended with " +
		"customer-assets.remove, not with a status." + customerAssetsNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "assets", "account", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, customerAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"customer_assets": customerAssetItems(schema{"status": customerAssetStatusSchema}, "status"),
		"validate_only":   validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "customer_assets")),
	OutputSchema: encoded(customerAssetsOutputSchema),
	Arguments: append(slices.Clone(customerAssetsArgumentDocs),
		capability.Argument{Name: "customer_assets", Required: true, Description: "1 to 50 entries, each with " +
			customerAssetKeyDoc + ", and status (ENABLED or PAUSED). Each asset and field type at most once"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: customerAssetsFields,
	Examples: []capability.Example{{Description: "Pause a link",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","customer_assets":[{"asset_id":"33","field_type":"SITELINK","status":"PAUSED"}]}`)}},
}

var customerAssetsRemove = capability.Descriptor{
	ID: customerAssetsRemoveID, Version: 1, Title: "Remove customer assets from a Google Ads account",
	Description: "End the use of assets by a Google Ads account (a remove of the resource name " +
		"built from asset_id and field_type). The asset itself stays in the account. Google marks " +
		"a removed link REMOVED; no tool of this provider brings it back, but the same asset can be attached " +
		"again." + customerAssetsNote +
		" Offered by a connection with the delete effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "assets", "account", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, customerAssetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id":     customerIDArgument,
		"customer_assets": customerAssetItems(nil),
		"validate_only":   validateOnlySchema,
	}, "customer_id", "customer_assets")),
	OutputSchema: encoded(customerAssetsOutputSchema),
	Arguments: append(slices.Clone(customerAssetsArgumentDocs),
		capability.Argument{Name: "customer_assets", Required: true, Description: "1 to 50 entries, each with " +
			customerAssetKeyDoc + ". Each asset and field type at most once"},
		validateOnlyArgument),
	Fields: customerAssetsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a link without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","customer_assets":[{"asset_id":"33","field_type":"SITELINK"}],"validate_only":true}`)}},
}

// --- handlers ---

func handleCustomerAssetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "attach customer assets"
	var args customerAssetsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return customerAssetsSend(ctx, resolved, secrets, red, op, customerAssetsCreateID, args.CustomerID, ops, keys,
		args.ValidateOnly, args.ResponseContentType)
}

func handleCustomerAssetsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update customer assets"
	var args customerAssetsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return customerAssetsSend(ctx, resolved, secrets, red, op, customerAssetsUpdateID, args.CustomerID, ops, keys,
		args.ValidateOnly, args.ResponseContentType)
}

func handleCustomerAssetsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove customer assets"
	var args customerAssetsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, keys, err := args.operations()
	if err != nil {
		return nil, err
	}
	return customerAssetsSend(ctx, resolved, secrets, red, op, customerAssetsRemoveID, args.CustomerID, ops, keys,
		args.ValidateOnly, "")
}

func customerAssetsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
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
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: customerAssetsService,
		body: mutateOperationsBody(ops, validateOnly, content)}, &answer); err != nil {
		return nil, err
	}
	done, err := customerAssetKind.results(op, customerID, answer.Results, answer.PartialFailureError, keys, validateOnly)
	if err != nil {
		return nil, err
	}
	return customerAssetsOutcome(customerID, validateOnly, done), nil
}

// --- confirmation ---

func customerAssetsKeyText(k customerAssetKey) string {
	return boundedText(k.FieldType, maxCodeText) + " asset " + boundedText(k.AssetID, maxCodeText)
}

func customerAssetsCreateSummary(raw json.RawMessage) string {
	var a customerAssetsCreateArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.CustomerAssets), func(i int) string {
		c := a.CustomerAssets[i]
		text := customerAssetsKeyText(c.customerAssetKey)
		if c.Status != nil {
			text += " " + boundedText(*c.Status, maxCodeText)
		}
		return text
	})
	return assetLinksLead(a.ValidateOnly) + "Attach " + strconv.Itoa(len(a.CustomerAssets)) +
		" asset(s) to Google Ads account " + a.CustomerID + ": " + list + "."
}

func customerAssetsUpdateSummary(raw json.RawMessage) string {
	var a customerAssetsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.CustomerAssets), func(i int) string {
		c := a.CustomerAssets[i]
		return customerAssetsKeyText(c.customerAssetKey) + " to " + boundedText(c.Status, maxCodeText)
	})
	return assetLinksLead(a.ValidateOnly) + "Set the status of " + strconv.Itoa(len(a.CustomerAssets)) +
		" customer asset(s) of Google Ads account " + a.CustomerID + ": " + list + "."
}

func customerAssetsRemoveSummary(raw json.RawMessage) string {
	var a customerAssetsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	list := assetLinksList(len(a.CustomerAssets), func(i int) string {
		return customerAssetsKeyText(a.CustomerAssets[i])
	})
	return assetLinksLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.CustomerAssets)) +
		" asset(s) from Google Ads account " + a.CustomerID + ": " + list +
		". Google marks the links removed; the assets stay in the account."
}
