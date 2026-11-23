package googleads

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	campaignAutomaticallyCreatedAssetsRemoveID = Provider + ".campaign-automatically-created-assets.remove"

	// maxRemovalsPerCall is Qatlas's own bound; Google documents none.
	maxRemovalsPerCall = 10
)

type (
	removalArguments struct {
		CustomerID string    `json:"customer_id"`
		Removals   []removal `json:"removals"`
	}
	removal struct {
		Campaign  string `json:"campaign"`
		Asset     string `json:"asset"`
		FieldType string `json:"field_type"`
	}
	removalDone struct {
		CustomerID string    `json:"customer_id"`
		Removed    []removal `json:"removed"`
	}
)

// operations checks every removal: the campaign and the asset must belong to the account of the call, which
// also refuses the temporary negative IDs of a batch, and the field type must name an enum value.
func (a removalArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if n := len(a.Removals); n == 0 || n > maxRemovalsPerCall {
		return nil, badRequest("removals must hold one to " + strconv.Itoa(maxRemovalsPerCall) + " entries")
	}
	operations := make([]any, 0, len(a.Removals))
	for i, r := range a.Removals {
		path := "removals[" + strconv.Itoa(i) + "]."
		switch {
		case !ownedName(r.Campaign, a.CustomerID, "campaigns"):
			return nil, badRequest(path + "campaign must be a campaign of customer_id, customers/ID/campaigns/N; " +
				"temporary IDs are not accepted")
		case !ownedName(r.Asset, a.CustomerID, "assets"):
			return nil, badRequest(path + "asset must be an asset of customer_id, customers/ID/assets/N; " +
				"temporary IDs are not accepted")
		case !enumPattern.MatchString(r.FieldType) || strings.HasPrefix(r.FieldType, "UNSPECIFIED") ||
			strings.HasPrefix(r.FieldType, "UNKNOWN"):
			return nil, badRequest(path + "field_type must be the name of an asset field type of the API, such as HEADLINE")
		}
		operations = append(operations, map[string]any{"campaign": r.Campaign, "asset": r.Asset, "fieldType": r.FieldType})
	}
	return operations, nil
}

var campaignAutomaticallyCreatedAssetsRemove = capability.Descriptor{
	ID: campaignAutomaticallyCreatedAssetsRemoveID, Version: 1,
	Title: "Remove automatically created assets from Google Ads campaigns",
	Description: "Remove text customizations that Google created automatically, such as generated headlines and " +
		"descriptions, from campaigns, each named by campaign, asset and field type " +
		"(AutomaticallyCreatedAssetRemovalService.RemoveCampaignAutomaticallyCreatedAsset). The asset itself stays " +
		"in the account; only its use by the campaign for that field type ends. The service has no validate_only, " +
		"so the tool offers none; the request is one atomic transaction without partial_failure." + mutateListListedNote,
	Tags: []string{"google", "ads", "assets", "campaigns", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent,
		"ad content of campaigns of a Google Ads account"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"removals": schema{"type": "array", "minItems": 1, "maxItems": maxRemovalsPerCall, "items": object(schema{
			"campaign":   schema{"type": "string", "pattern": "^customers/[0-9]{1,10}/campaigns/[0-9]{1,20}$"},
			"asset":      schema{"type": "string", "pattern": assetNamePattern},
			"field_type": schema{"type": "string", "pattern": enumPattern.String()}},
			"campaign", "asset", "field_type")}},
		"customer_id", "removals")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "removed": arrayOf(object(schema{
		"campaign": stringSchema, "asset": stringSchema, "field_type": stringSchema}))}, "customer_id", "removed")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "removals", Required: true, Description: "One to 10 entries, each with campaign " +
			"(customers/ID/campaigns/N), asset (customers/ID/assets/N), both of customer_id, and field_type, the " +
			"name of the asset field type of the customization, such as HEADLINE or DESCRIPTION"}},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "removed", Description: "The removals Google carried out, as asked; Google answers nothing else"}},
	Examples: []capability.Example{{Description: "Remove a generated headline from a campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","removals":[{"campaign":"customers/1234567890/campaigns/22",` +
			`"asset":"customers/1234567890/assets/33","field_type":"HEADLINE"}]}`)}},
}

func handleCampaignAutomaticallyCreatedAssetsRemove(ctx context.Context, resolved *config.Resolved,
	secrets *secret.Resolver, red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove automatically created assets"
	var args removalArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, campaignAutomaticallyCreatedAssetsRemoveID)
	if err != nil {
		return nil, err
	}
	operations, err := args.operations()
	if err != nil {
		return nil, err
	}
	c, err := env.settle(args.CustomerID, false)
	if err != nil {
		return nil, err
	}
	var answer struct {
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: env.tool, customerID: args.CustomerID,
		action: "removeCampaignAutomaticallyCreatedAsset",
		body:   map[string]any{"operations": operations, "partialFailure": false}}, &answer); err != nil {
		return nil, err
	}
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		body, _ := json.Marshal(map[string]json.RawMessage{"error": answer.PartialFailureError})
		failed, _ := adsError(op, 400, body, true)
		if failed.Class == provider.ClassInvalidResponse {
			failed.Class = provider.ClassProviderError
			failed.Message = subject + " refused the removal; nothing was changed"
		}
		return nil, failed
	}
	return removalDone{CustomerID: args.CustomerID, Removed: args.Removals}, nil
}

// removalSummary phrases the change for the confirmation, from the arguments alone.
func removalSummary(raw json.RawMessage) string {
	var a removalArguments
	_ = json.Unmarshal(raw, &a)
	parts := make([]string, 0, len(a.Removals))
	for _, r := range a.Removals {
		parts = append(parts, r.FieldType+" "+r.Asset+" from "+r.Campaign)
	}
	return "Remove automatically created assets of Google Ads account " + a.CustomerID + ": " +
		strings.Join(parts, "; ") + ". The campaigns stop using them for that field type."
}
