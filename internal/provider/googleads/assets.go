package googleads

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/localfile"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	assetsCreateID = Provider + ".assets.create"
	assetsUpdateID = Provider + ".assets.update"

	assetService = "/assets"
)

const assetSensitivity = "ad content of a Google Ads account that becomes public once an ad uses it: texts, links, " +
	"images, phone numbers, promotion codes and lead form questions"

type assetArguments struct {
	CustomerID          string            `json:"customer_id"`
	Assets              []json.RawMessage `json:"assets"`
	ValidateOnly        bool              `json:"validate_only"`
	ResponseContentType string            `json:"response_content_type"`
}

type (
	assetResult struct {
		ResourceName string         `json:"resource_name"`
		Asset        map[string]any `json:"asset,omitempty"`
	}
	assetsChanged struct {
		CustomerID   string        `json:"customer_id"`
		ValidateOnly bool          `json:"validate_only"`
		Results      []assetResult `json:"results"`
	}
)

// mediaLoader reads the bytes of images and bundles. A local file is opened here, before any secret is read,
// so a path outside the released directories is refused without secret access or a request to Google. The
// bytes of one call are counted together.
type mediaLoader struct {
	ctx      context.Context
	resolved *config.Resolved
	total    int64
}

func (m *mediaLoader) load(local, content *string, path string) ([]byte, error) {
	kind, err := localfile.UploadSource(local, content)
	if err != nil {
		return nil, err
	}
	var data []byte
	if kind == localfile.SourceLocalPath {
		up, err := localfile.OpenForUpload(m.ctx, m.resolved, *local)
		if err != nil {
			return nil, err
		}
		defer up.Close()
		if up.Size == 0 || up.Size > maxMediaBytes {
			return nil, badRequest(path + "the file must have 1 byte to 5120 KiB")
		}
		data, err = io.ReadAll(up)
		if err != nil || int64(len(data)) != up.Size {
			return nil, badRequest(path + "the local file could not be read completely or changed while it was read")
		}
	} else {
		if len(*content) > maxInlineBase64 {
			return nil, badRequest(path + "inline content is limited to 4 MiB, use local_path")
		}
		data, err = base64.StdEncoding.DecodeString(*content)
		if err != nil || len(data) == 0 {
			return nil, badRequest(path + "content_base64 must be non-empty valid base64")
		}
	}
	m.total += int64(len(data))
	if m.total > maxMediaPerCall {
		return nil, badRequest("one call carries at most 10 MiB of image data")
	}
	return data, nil
}

// assetOperations checks every asset of the call and builds the operations. The update takes the resource
// name of the asset and a mask of the members that were given.
func assetOperations(ctx context.Context, resolved *config.Resolved, a assetArguments, update bool) ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, err
	}
	if n := len(a.Assets); n == 0 || n > maxAssetsPerCall {
		return nil, badRequest("assets must hold one to " + strconv.Itoa(maxAssetsPerCall) + " entries")
	}
	loader := &mediaLoader{ctx: ctx, resolved: resolved}
	w := &assetCall{customerID: a.CustomerID, update: update, load: loader.load}
	s := assetOperationSpec()
	if update {
		s = updatableSpec()
	}
	var operations []any
	for i, raw := range a.Assets {
		path := "assets[" + strconv.Itoa(i) + "]."
		var in map[string]any
		if decodeLoose(raw, &in) != nil || in == nil {
			return nil, badRequest("assets[" + strconv.Itoa(i) + "] must be an object")
		}
		name := ""
		if update {
			var ok bool
			name, ok = in["resource_name"].(string)
			if !ok || !ownedName(name, a.CustomerID, "assets") {
				return nil, badRequest(path + "resource_name must be an asset of customer_id, customers/ID/assets/N; " +
					"temporary IDs are not accepted")
			}
			delete(in, "resource_name")
		}
		wire, mask, err := w.object(s, in, path)
		if err != nil {
			return nil, err
		}
		if !update {
			operations = append(operations, map[string]any{"create": wire})
			continue
		}
		if len(mask) == 0 {
			return nil, badRequest(path + "give at least one member to change")
		}
		wire["resourceName"] = name
		operations = append(operations, map[string]any{"update": wire, "updateMask": strings.Join(mask, ",")})
	}
	return operations, nil
}

func handleAssetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetsChange(ctx, resolved, secrets, red, raw, false)
}

func handleAssetsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return assetsChange(ctx, resolved, secrets, red, raw, true)
}

func assetsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, update bool) (any, error) {
	op, tool := "create assets", assetsCreateID
	if update {
		op, tool = "update assets", assetsUpdateID
	}
	var args assetArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	// The account is checked against the targets before any local file is opened.
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if _, err := reachOf(env.conn, args.CustomerID); err != nil {
		return nil, err
	}
	operations, err := assetOperations(ctx, resolved, args, update)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(args.CustomerID, false)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"operations": operations}
	if args.ValidateOnly {
		body["validateOnly"] = true
	}
	if args.ResponseContentType != "" {
		body["responseContentType"] = args.ResponseContentType
	}
	var answer struct {
		Results []struct {
			ResourceName string          `json:"resourceName"`
			Asset        json.RawMessage `json:"asset"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: args.CustomerID, service: assetService,
		body: body}, &answer); err != nil {
		return nil, err
	}
	out := assetsChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Results: []assetResult{}}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure, which the request did not ask for")
	}
	if args.ValidateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != len(operations) {
		return nil, unsureResult(op, "answered with another number of results than the number of assets asked for")
	}
	for i, r := range answer.Results {
		if !ownedName(r.ResourceName, args.CustomerID, "assets") {
			return nil, unsureResult(op, "answered with a result for another account than the one asked for")
		}
		if update {
			var want string
			if m, ok := operations[i].(map[string]any)["update"].(map[string]any); ok {
				want, _ = m["resourceName"].(string)
			}
			if r.ResourceName != want {
				return nil, unsureResult(op, "answered with a result for another asset than the one asked for")
			}
		}
		item := assetResult{ResourceName: r.ResourceName}
		if len(r.Asset) > 0 && string(r.Asset) != "null" {
			var m map[string]any
			if decodeLoose(r.Asset, &m) != nil {
				return nil, unsureResult(op, "answered with an asset that cannot be read")
			}
			if name, ok := m["resourceName"].(string); ok && name != r.ResourceName {
				return nil, unsureResult(op, "answered with an asset that is not the one of the result")
			}
			mapped, ok := outObject(assetOperationSpec(), m, args.CustomerID)
			if !ok {
				return nil, unsureResult(op, "answered with an asset that is not the one asked for")
			}
			item.Asset = mapped
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// assetSummary phrases the change for the confirmation, from the arguments alone.
func assetSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a assetArguments
		_ = json.Unmarshal(raw, &a)
		var parts []string
		clicks := false
		for _, item := range a.Assets {
			var in map[string]any
			if decodeLoose(item, &in) != nil {
				continue
			}
			parts = append(parts, assetLabel(in))
			for _, key := range []string{"tracking_url_template", "final_url_suffix", "final_urls", "final_mobile_urls"} {
				if in[key] != nil {
					clicks = true
				}
			}
		}
		lead := verb
		if a.ValidateOnly {
			lead = "Only validate: " + verb
		}
		summary := lead + " " + strconv.Itoa(len(a.Assets)) + " asset(s) of Google Ads account " + a.CustomerID + ": " +
			strings.Join(parts, "; ") + ". Assets become public once an ad uses them."
		if clicks {
			summary += " Clicks on ads that use them pass through the addresses given."
		}
		return summary
	}
}

// assetLabel names an asset by its type and the first texts it holds.
func assetLabel(in map[string]any) string {
	label := ""
	if name, ok := in["resource_name"].(string); ok {
		label = name + " "
	}
	for _, t := range assetTypes {
		body, ok := in[t.key].(map[string]any)
		if !ok {
			continue
		}
		label += strings.TrimSuffix(t.key, "_asset")
		keys := make([]string, 0, len(body))
		for key := range body {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		shown := 0
		for _, key := range keys {
			if s, ok := body[key].(string); ok && shown < 3 && key != localfile.ContentArgument {
				label += " " + key + "=" + shortText(s)
				shown++
			}
		}
		if _, ok := body[localfile.ContentArgument]; ok {
			label += " (inline image data)"
		}
	}
	return label
}

func shortText(s string) string {
	runes := []rune(s)
	if len(runes) > 60 {
		return string(runes[:60]) + "..."
	}
	return s
}

// typeList names the asset types of a create or an update for the description.
func typeList(update bool) string {
	var keys []string
	for _, t := range assetTypes {
		if !update || !t.immutable {
			keys = append(keys, t.key)
		}
	}
	return strings.Join(keys, ", ")
}

func assetItemSchema(update bool) schema {
	if !update {
		return assetOperationSpec().inputSchema(false)
	}
	item := updatableSpec().inputSchema(true)
	item["properties"].(schema)["resource_name"] = schema{"type": "string", "pattern": assetNamePattern}
	item["required"] = []string{"resource_name"}
	return item
}

func assetInputSchema(update bool) json.RawMessage {
	return encoded(object(schema{"customer_id": customerIDArgument,
		"assets":        schema{"type": "array", "minItems": 1, "maxItems": maxAssetsPerCall, "items": assetItemSchema(update)},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "assets"))
}

var assetOutputSchema = encoded(object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"results": arrayOf(object(schema{"resource_name": stringSchema, "asset": assetOperationSpec().outputSchema()},
		"resource_name"))}, "customer_id", "validate_only", "results"))

var assetFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "One entry per asset, in the order of the call, each with the resource name " +
		"customers/ID/assets/N as Google answered; empty after validate_only"},
	{Name: "results[].asset", Description: "With MUTABLE_RESOURCE: the mutable members of the asset after the change, " +
		"as Google returns them. Image bytes and the webhook settings of a lead form are never returned"},
}

const assetCommonNote = " name, final_urls, final_mobile_urls, tracking_url_template, url_custom_parameters, " +
	"final_url_suffix and synthetic_content_info are members of every asset; final_urls, final_mobile_urls and " +
	"tracking_url_template must be https addresses without user info, final_url_suffix must be parameters, not an " +
	"address, and tracking_url_template and final_url_suffix pass click data to the address they name. Texts are " +
	"public once an ad uses the asset. Which characters, lengths and combinations Google accepts is Google's to " +
	"validate; use validate_only to see its verdict. A member is the member of the Google Ads API in snake case, " +
	"and a value of an enum is its name."

const assetAssetsDescription = "One to 10 assets. "

var assetsCreate = capability.Descriptor{
	ID: assetsCreateID, Version: 1, Title: "Create assets in a Google Ads account",
	Description: "Create one to 10 assets in one Google Ads account (AssetService.MutateAssets with create operations). " +
		"Each asset has exactly one type member: " + typeList(false) + ". An image or media bundle carries its " +
		"bytes as local_path, a file of a directory the connection releases for reading, or as content_base64 up " +
		"to 4 MiB, never both; a file is up to 5120 KiB and one call carries up to 10 MiB of them. The delivery " +
		"of lead form data to a webhook (delivery_methods) is not offered, and location assets are output only." +
		assetCommonNote + " An asset cannot be removed: to stop it from serving, remove it from what uses it." +
		mutateListListedNote,
	Tags: []string{"google", "ads", "assets", "create"}, Provider: Provider, RequiresToolAllowList: true,
	LocalFiles:  config.LocalFilesRead,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, assetSensitivity),
	InputSchema: assetInputSchema(false), OutputSchema: assetOutputSchema,
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "assets", Required: true, Description: assetAssetsDescription + "Each entry has the common members and " +
			"exactly one type member, such as sitelink_asset; image_asset and media_bundle_asset take local_path or " +
			"content_base64. A reference to another asset, such as background_image_asset, must be an asset of " +
			"customer_id, customers/ID/assets/N"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: assetFields,
	Examples: []capability.Example{
		{Description: "Validate a sitelink without creating it", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
			`"assets":[{"final_urls":["https://www.example.com/pricing"],"sitelink_asset":{"link_text":"Pricing",` +
			`"description1":"Plans for teams"}}],"validate_only":true}`)},
		{Description: "Create an image from a local file", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
			`"assets":[{"name":"Spring banner","image_asset":{"local_path":"~/uploads/banner.png"}}]}`)}},
}

var assetsUpdate = capability.Descriptor{
	ID: assetsUpdateID, Version: 1, Title: "Change assets of a Google Ads account",
	Description: "Change one to 10 assets of one Google Ads account (AssetService.MutateAssets with update operations " +
		"and a field mask that names exactly the members given). An asset is named by its resource name; give the " +
		"members to change, an empty text or list clears a member. Only these types can change: " + typeList(true) +
		"; text, image, media bundle, YouTube video and the other types that Google marks immutable cannot, and a " +
		"member set at creation, such as external_redeem_info of a promotion, stays. The type of an asset does not " +
		"change." + assetCommonNote + mutateListListedNote,
	Tags: []string{"google", "ads", "assets", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, assetSensitivity),
	InputSchema: assetInputSchema(true), OutputSchema: assetOutputSchema,
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "assets", Required: true, Description: assetAssetsDescription + "Each entry has resource_name, " +
			"customers/ID/assets/N of an asset of customer_id, and the members to change"},
		validateOnlyArgument, responseContentTypeArgument},
	Fields: assetFields,
	Examples: []capability.Example{
		{Description: "Change the text of a sitelink", Arguments: json.RawMessage(`{"customer_id":"1234567890",` +
			`"assets":[{"resource_name":"customers/1234567890/assets/111","sitelink_asset":{"link_text":"Plans"}}]}`)}},
}
