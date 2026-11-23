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
	recommendationSubscriptionsCreateID = Provider + ".recommendation-subscriptions.create"
	recommendationSubscriptionsUpdateID = Provider + ".recommendation-subscriptions.update"

	recommendationSubscriptionsService    = "/recommendationSubscriptions"
	recommendationSubscriptionsCollection = "recommendationSubscriptions"
	// recommendationSubscriptionsMax is Qatlas's own bound per call; the reference states none.
	recommendationSubscriptionsMax = maxListOperations
)

// The values of RecommendationType in v26 that name a recommendation: UNKNOWN and UNSPECIFIED are no values to
// send. Which of them a subscription accepts is Google's to decide.
var recommendationSubscriptionTypes = []string{"CALLOUT_ASSET", "CALL_ASSET", "CAMPAIGN_BUDGET",
	"CAMPAIGN_SPECIFIC_APP_GOAL", "CUSTOM_AUDIENCE_OPT_IN", "DISPLAY_EXPANSION_OPT_IN",
	"DYNAMIC_IMAGE_EXTENSION_OPT_IN", "ENHANCED_CPC_OPT_IN", "FORECASTING_CAMPAIGN_BUDGET",
	"FORECASTING_SET_TARGET_CPA", "FORECASTING_SET_TARGET_ROAS", "IMPROVE_DEMAND_GEN_AD_STRENGTH",
	"IMPROVE_GOOGLE_TAG_COVERAGE", "IMPROVE_PERFORMANCE_MAX_AD_STRENGTH", "KEYWORD", "KEYWORD_MATCH_TYPE",
	"LEAD_FORM_ASSET", "LOWER_TARGET_ROAS", "LOWER_TARGET_ROAS_PERFORMANCE_BID_TOO_LOW",
	"MARGINAL_ROI_CAMPAIGN_BUDGET", "MAXIMIZE_CLICKS_OPT_IN", "MAXIMIZE_CONVERSIONS_OPT_IN",
	"MAXIMIZE_CONVERSION_VALUE_OPT_IN", "MIGRATE_DYNAMIC_SEARCH_ADS_CAMPAIGN_TO_PERFORMANCE_MAX",
	"MOVE_UNUSED_BUDGET", "OPTIMIZE_AD_ROTATION", "PERFORMANCE_MAX_FINAL_URL_OPT_IN", "PERFORMANCE_MAX_OPT_IN",
	"RAISE_BUDGET_PERFORMANCE_TOO_LOW", "RAISE_TARGET_CPA", "RAISE_TARGET_CPA_BID_TOO_LOW",
	"RAISE_TARGET_CPA_PERFORMANCE_BID_TOO_LOW", "REFRESH_CUSTOMER_MATCH_LIST", "RESPONSIVE_SEARCH_AD",
	"RESPONSIVE_SEARCH_AD_ASSET", "RESPONSIVE_SEARCH_AD_IMPROVE_AD_STRENGTH", "SEARCH_PARTNERS_OPT_IN",
	"SET_TARGET_CPA", "SET_TARGET_ROAS", "SHOPPING_ADD_AGE_GROUP", "SHOPPING_ADD_COLOR", "SHOPPING_ADD_GENDER",
	"SHOPPING_ADD_GTIN", "SHOPPING_ADD_MORE_IDENTIFIERS", "SHOPPING_ADD_PRODUCTS_TO_CAMPAIGN",
	"SHOPPING_ADD_SIZE", "SHOPPING_FIX_DISAPPROVED_PRODUCTS",
	"SHOPPING_FIX_MERCHANT_CENTER_ACCOUNT_SUSPENSION_WARNING", "SHOPPING_FIX_SUSPENDED_MERCHANT_CENTER_ACCOUNT",
	"SHOPPING_MIGRATE_REGULAR_SHOPPING_CAMPAIGN_OFFERS_TO_PERFORMANCE_MAX", "SHOPPING_TARGET_ALL_OFFERS",
	"SITELINK_ASSET", "TARGET_CPA_OPT_IN", "TARGET_ROAS_OPT_IN", "TEXT_AD",
	"UPGRADE_LOCAL_CAMPAIGN_TO_PERFORMANCE_MAX", "UPGRADE_SMART_SHOPPING_CAMPAIGN_TO_PERFORMANCE_MAX",
	"USE_BROAD_MATCH_KEYWORD"}

// RecommendationSubscriptionStatus: a subscription is never deleted, only switched between the two.
var recommendationSubscriptionStatuses = []string{"ENABLED", "PAUSED"}

type (
	subscriptionEntry struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	subscriptionArguments struct {
		CustomerID          string              `json:"customer_id"`
		Subscriptions       []subscriptionEntry `json:"recommendation_subscriptions"`
		ValidateOnly        bool                `json:"validate_only"`
		ResponseContentType string              `json:"response_content_type"`
	}
)

func subscriptionName(customerID, kind string) string {
	return customerName(customerID) + "/" + recommendationSubscriptionsCollection + "/" + kind
}

// operations checks the entries and builds the operations. The type of a subscription is its key and cannot
// change, so an update names it and sets the status alone.
func (a subscriptionArguments) operations(create bool) ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	if n := len(a.Subscriptions); n == 0 || n > recommendationSubscriptionsMax {
		return nil, nil, badRequest("recommendation_subscriptions must hold 1 to " +
			strconv.Itoa(recommendationSubscriptionsMax) + " entries")
	}
	ops := make([]any, 0, len(a.Subscriptions))
	names := make([]string, 0, len(a.Subscriptions))
	for _, e := range a.Subscriptions {
		if !slices.Contains(recommendationSubscriptionTypes, e.Type) {
			return nil, nil, badRequest("every entry needs a type that names a recommendation type of the API")
		}
		if !slices.Contains(recommendationSubscriptionStatuses, e.Status) {
			return nil, nil, badRequest("status must be ENABLED or PAUSED")
		}
		name := subscriptionName(a.CustomerID, e.Type)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("recommendation_subscriptions names each type at most once")
		}
		names = append(names, name)
		if create {
			ops = append(ops, map[string]any{"create": map[string]any{"type": e.Type, "status": e.Status}})
			continue
		}
		ops = append(ops, map[string]any{"update": map[string]any{"resourceName": name, "status": e.Status},
			"updateMask": "status"})
	}
	return ops, names, nil
}

type (
	subscriptionResource struct {
		ResourceName   string `json:"resource_name"`
		Type           string `json:"type"`
		Status         string `json:"status"`
		CreateDateTime string `json:"create_date_time,omitempty"`
		ModifyDateTime string `json:"modify_date_time,omitempty"`
	}
	subscriptionResult struct {
		Type                       string                `json:"type"`
		ResourceName               string                `json:"resource_name"`
		RecommendationSubscription *subscriptionResource `json:"recommendation_subscription,omitempty"`
	}
	subscriptionChanged struct {
		CustomerID   string               `json:"customer_id"`
		ValidateOnly bool                 `json:"validate_only"`
		Results      []subscriptionResult `json:"results"`
	}
)

// subscriptionOf maps the subscription of an answer; it must be the one the result names.
func subscriptionOf(raw json.RawMessage, name, kind string) (*subscriptionResource, bool) {
	var w struct {
		ResourceName   string `json:"resourceName"`
		Type           string `json:"type"`
		Status         string `json:"status"`
		CreateDateTime string `json:"createDateTime"`
		ModifyDateTime string `json:"modifyDateTime"`
	}
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != name || (w.Type != "" && w.Type != kind) {
		return nil, false
	}
	return &subscriptionResource{ResourceName: name, Type: kind, Status: code(w.Status),
		CreateDateTime: code(w.CreateDateTime), ModifyDateTime: code(w.ModifyDateTime)}, true
}

var (
	subscriptionTypeSchema   = schema{"type": "string", "enum": recommendationSubscriptionTypes}
	subscriptionStatusSchema = schema{"type": "string", "enum": recommendationSubscriptionStatuses}

	subscriptionOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"type": stringSchema, "resource_name": stringSchema,
			"recommendation_subscription": object(schema{"resource_name": stringSchema, "type": stringSchema,
				"status": stringSchema, "create_date_time": stringSchema, "modify_date_time": stringSchema})},
			"type", "resource_name"))}, "customer_id", "validate_only", "results")

	subscriptionInputSchema = encoded(object(schema{"customer_id": customerIDArgument,
		"recommendation_subscriptions": schema{"type": "array", "minItems": 1, "maxItems": recommendationSubscriptionsMax,
			"items": object(schema{"type": subscriptionTypeSchema, "status": subscriptionStatusSchema}, "type", "status")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema},
		"customer_id", "recommendation_subscriptions"))

	subscriptionArgumentDocs = []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target. A manager account needs a manager " +
			"target that names it"},
		{Name: "recommendation_subscriptions", Required: true, Description: "1 to 10 entries, each recommendation " +
			"type once, with type (a RecommendationType of the API such as KEYWORD or CAMPAIGN_BUDGET) and status " +
			"(ENABLED applies the recommendations of that type automatically, PAUSED does not)"},
		validateOnlyArgument, responseContentTypeArgument}

	subscriptionFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "Per entry, in the order of the arguments: type, resource_name " +
			"(customers/ID/recommendationSubscriptions/TYPE, as Google answered, always of the account of the call), " +
			"and with MUTABLE_RESOURCE the subscription after the change (type, status, create_date_time, " +
			"modify_date_time, as Google returns them). Empty after validate_only"}}
)

const subscriptionEffectNote = " A subscription makes Google apply the recommendations of one type to the account " +
	"automatically and from then on, without a further call or a confirmation: depending on the type that changes " +
	"budgets, bids, targets, keywords, ads, assets and the networks of campaigns, and so the spending of the " +
	"account, until the subscription is paused. The subscription carries no parameters, so Google chooses the " +
	"values, and Qatlas Connect cannot see or limit what Google applies later. A subscription is never deleted, " +
	"only switched between ENABLED and PAUSED. The request goes to " +
	"RecommendationSubscriptionService.MutateRecommendationSubscription with up to 10 operations, atomic and " +
	"without partial_failure. Read subscriptions with reports.search on recommendation_subscription."

var subscriptionSensitivity = "automatic recommendation settings of a Google Ads account, which change its spending"

var recommendationSubscriptionsCreate = capability.Descriptor{
	ID: recommendationSubscriptionsCreateID, Version: 1, Title: "Subscribe a Google Ads account to automatic recommendations",
	Description: "Create subscriptions that make Google apply the recommendations of the named types to a Google " +
		"Ads account automatically (an ongoing, spending-relevant effect, not a one-time change). Each entry names " +
		"the type and the status: ENABLED starts the automatic application at once." + subscriptionEffectNote +
		" Google refuses a type the account already subscribed to; change it with update." + mutateListListedNote,
	Tags: []string{"google", "ads", "recommendations", "subscriptions", "create"}, Provider: Provider,
	RequiresToolAllowList: true,
	Risk:                  changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, subscriptionSensitivity),
	InputSchema:           subscriptionInputSchema, OutputSchema: encoded(subscriptionOutputSchema),
	Arguments: subscriptionArgumentDocs, Fields: subscriptionFields,
	Examples: []capability.Example{{Description: "Validate a paused subscription without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","recommendation_subscriptions":[{"type":"KEYWORD","status":"PAUSED"}],"validate_only":true}`)}},
}

var recommendationSubscriptionsUpdate = capability.Descriptor{
	ID: recommendationSubscriptionsUpdateID, Version: 1, Title: "Enable or pause automatic recommendations of a Google Ads account",
	Description: "Switch existing subscriptions of a Google Ads account between ENABLED and PAUSED (an update with " +
		"the mask status): ENABLED makes Google apply the recommendations of that type automatically from then on " +
		"(an ongoing, spending-relevant effect), PAUSED stops it. The type cannot change." +
		subscriptionEffectNote + mutateListListedNote,
	Tags: []string{"google", "ads", "recommendations", "subscriptions", "update"}, Provider: Provider,
	RequiresToolAllowList: true,
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, subscriptionSensitivity),
	InputSchema:           subscriptionInputSchema, OutputSchema: encoded(subscriptionOutputSchema),
	Arguments: subscriptionArgumentDocs, Fields: subscriptionFields,
	Examples: []capability.Example{{Description: "Pause the automatic keyword recommendations",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","recommendation_subscriptions":[{"type":"KEYWORD","status":"PAUSED"}]}`)}},
}

func handleRecommendationSubscriptionsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return recommendationSubscriptionsChange(ctx, resolved, secrets, red, raw, true)
}

func handleRecommendationSubscriptionsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return recommendationSubscriptionsChange(ctx, resolved, secrets, red, raw, false)
}

func recommendationSubscriptionsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, create bool) (any, error) {
	op, tool := "update recommendation subscriptions", recommendationSubscriptionsUpdateID
	if create {
		op, tool = "create recommendation subscriptions", recommendationSubscriptionsCreateID
	}
	var args subscriptionArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	ops, names, err := args.operations(create)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: args.CustomerID,
		service: recommendationSubscriptionsService, collection: recommendationSubscriptionsCollection,
		resourceKey: "recommendationSubscription", operations: ops, names: names, validateOnly: args.ValidateOnly,
		content: args.ResponseContentType,
		idOf: func(name string) (string, bool) {
			kind, ok := strings.CutPrefix(name, customerName(args.CustomerID)+"/"+recommendationSubscriptionsCollection+"/")
			return kind, ok && slices.Contains(recommendationSubscriptionTypes, kind)
		}})
	if err != nil {
		return nil, err
	}
	out := subscriptionChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Results: []subscriptionResult{}}
	for _, r := range results {
		item := subscriptionResult{Type: r.ID, ResourceName: r.ResourceName}
		if len(r.Resource) > 0 {
			s, ok := subscriptionOf(r.Resource, r.ResourceName, r.ID)
			if !ok {
				return nil, unsureResult(op, "answered with a subscription that is not the one asked for")
			}
			item.RecommendationSubscription = s
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// recommendationSubscriptionsSummary phrases the change for the confirmation, from the arguments alone.
func recommendationSubscriptionsSummary(create bool) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a subscriptionArguments
		_ = json.Unmarshal(raw, &a)
		var parts []string
		for _, e := range a.Subscriptions[:min(len(a.Subscriptions), 10)] {
			parts = append(parts, boundedText(e.Type, maxCodeText)+" "+boundedText(e.Status, maxCodeText))
		}
		verb := "Change"
		if create {
			verb = "Create"
		}
		lead := ""
		if a.ValidateOnly {
			lead = "Only validate: "
		}
		return lead + verb + " automatic recommendation subscription(s) of Google Ads account " + a.CustomerID + ": " +
			strings.Join(parts, "; ") + ". An ENABLED subscription makes Google apply recommendations of that type " +
			"to the account automatically and from then on, which can change budgets, bids and ads."
	}
}
