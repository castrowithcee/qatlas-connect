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
	customerConversionGoalsUpdateID       = Provider + ".customer-conversion-goals.update"
	campaignConversionGoalsUpdateID       = Provider + ".campaign-conversion-goals.update"
	conversionGoalCampaignConfigsUpdateID = Provider + ".conversion-goal-campaign-configs.update"
	customerConversionGoalService         = "/customerConversionGoals"
	campaignConversionGoalService         = "/campaignConversionGoals"
	conversionGoalCampaignConfigService   = "/conversionGoalCampaignConfigs"
	customConversionGoalCollection        = "customConversionGoals"
	campaignCollection                    = "campaigns"
	// maxGoalOperations is Qatlas's own bound of the entries of one call; Google documents none for these services.
	maxGoalOperations = 20
)

// The values of the enums in v26 that a request may name. UNSPECIFIED and UNKNOWN are no values to send. The
// categories are those of the conversion actions.
var (
	conversionOrigins = []string{"WEBSITE", "GOOGLE_HOSTED", "APP", "CALL_FROM_ADS", "STORE", "YOUTUBE_HOSTED",
		"LOCAL_SERVICES_ADS"}
	goalConfigLevels = []string{"CUSTOMER", "CAMPAIGN"}
)

type (
	goalSettingItem struct {
		CampaignID string `json:"campaign_id"`
		Category   string `json:"category"`
		Origin     string `json:"origin"`
		Biddable   *bool  `json:"biddable"`
	}
	goalConfigItem struct {
		CampaignID             string `json:"campaign_id"`
		GoalConfigLevel        string `json:"goal_config_level"`
		CustomConversionGoalID string `json:"custom_conversion_goal_id"`
	}
	goalSettingsArguments struct {
		CustomerID          string          `json:"customer_id"`
		Goals               json.RawMessage `json:"goals"`
		Configs             json.RawMessage `json:"configs"`
		ValidateOnly        bool            `json:"validate_only"`
		ResponseContentType string          `json:"response_content_type"`
	}
)

func customerGoalName(customerID string, g goalSettingItem) string {
	return customerName(customerID) + customerConversionGoalService + "/" + g.Category + "~" + g.Origin
}

func campaignGoalName(customerID string, g goalSettingItem) string {
	return customerName(customerID) + campaignConversionGoalService + "/" + g.CampaignID + "~" + g.Category + "~" + g.Origin
}

func goalSettingConfigName(customerID, campaignID string) string {
	return customerName(customerID) + conversionGoalCampaignConfigService + "/" + campaignID
}

func checkNumericID(argument, value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !numericIDPattern.MatchString(value) {
		return badRequest(argument + " must be the numeric ID of an existing object of the account")
	}
	return nil
}

// check refuses what no request may start with. The category and the origin come from the enums of the API and
// build the resource name, so no free text reaches it.
func (g goalSettingItem) check(withCampaign bool) error {
	if withCampaign {
		if err := checkNumericID("campaign_id", g.CampaignID); err != nil {
			return err
		}
	}
	if !slices.Contains(conversionCategories, g.Category) {
		return badRequest("category must be a ConversionActionCategory of the API")
	}
	if !slices.Contains(conversionOrigins, g.Origin) {
		return badRequest("origin must be a ConversionOrigin of the API")
	}
	if g.Biddable == nil {
		return badRequest("every entry needs biddable")
	}
	return nil
}

// operations checks the arguments of the tool and builds the operations, with the resource name each must be
// answered with.
func (a goalSettingsArguments) operations(tool string) (operations []any, names []string, err error) {
	if tool == conversionGoalCampaignConfigsUpdateID {
		var items []goalConfigItem
		if json.Unmarshal(a.Configs, &items) != nil || len(items) < 1 || len(items) > maxGoalOperations {
			return nil, nil, badRequest("configs holds one to 20 entries")
		}
		for _, item := range items {
			if err := checkNumericID("campaign_id", item.CampaignID); err != nil {
				return nil, nil, err
			}
			name := goalSettingConfigName(a.CustomerID, item.CampaignID)
			if slices.Contains(names, name) {
				return nil, nil, badRequest("configs names each campaign once")
			}
			resource := map[string]any{"resourceName": name}
			var mask []string
			if item.GoalConfigLevel != "" {
				if !slices.Contains(goalConfigLevels, item.GoalConfigLevel) {
					return nil, nil, badRequest("goal_config_level must be CUSTOMER or CAMPAIGN")
				}
				resource["goalConfigLevel"] = item.GoalConfigLevel
				mask = append(mask, "goalConfigLevel")
			}
			if item.CustomConversionGoalID != "" {
				if err := checkNumericID("custom_conversion_goal_id", item.CustomConversionGoalID); err != nil {
					return nil, nil, err
				}
				resource["customConversionGoal"] = customerName(a.CustomerID) + "/" + customConversionGoalCollection + "/" +
					item.CustomConversionGoalID
				mask = append(mask, "customConversionGoal")
			}
			if len(mask) == 0 {
				return nil, nil, badRequest("every entry of configs needs goal_config_level or custom_conversion_goal_id")
			}
			operations = append(operations, map[string]any{"update": resource, "updateMask": strings.Join(mask, ",")})
			names = append(names, name)
		}
		return operations, names, nil
	}
	campaign := tool == campaignConversionGoalsUpdateID
	var items []goalSettingItem
	if json.Unmarshal(a.Goals, &items) != nil || len(items) < 1 || len(items) > maxGoalOperations {
		return nil, nil, badRequest("goals holds one to 20 entries")
	}
	for _, item := range items {
		if err := item.check(campaign); err != nil {
			return nil, nil, err
		}
		name := customerGoalName(a.CustomerID, item)
		if campaign {
			name = campaignGoalName(a.CustomerID, item)
		}
		if slices.Contains(names, name) {
			return nil, nil, badRequest("goals names each category and origin (and campaign) once")
		}
		operations = append(operations, map[string]any{
			"update": map[string]any{"resourceName": name, "biddable": *item.Biddable}, "updateMask": "biddable"})
		names = append(names, name)
	}
	return operations, names, nil
}

type (
	goalSettingsResult struct {
		CampaignID   string              `json:"campaign_id,omitempty"`
		Category     string              `json:"category,omitempty"`
		Origin       string              `json:"origin,omitempty"`
		ResourceName string              `json:"resource_name"`
		Config       *goalConfigResource `json:"conversion_goal_campaign_config,omitempty"`
	}
	goalConfigResource struct {
		ResourceName         string `json:"resource_name"`
		Campaign             string `json:"campaign,omitempty"`
		GoalConfigLevel      string `json:"goal_config_level,omitempty"`
		CustomConversionGoal string `json:"custom_conversion_goal,omitempty"`
	}
	goalSettingsOutput struct {
		CustomerID   string               `json:"customer_id"`
		ValidateOnly bool                 `json:"validate_only"`
		Results      []goalSettingsResult `json:"results"`
	}
)

// goalConfigOf maps the config of a mutate answer. It must be the one of the result, and a goal it names must
// belong to the account of the call.
func goalConfigOf(raw json.RawMessage, customerID, name string) (*goalConfigResource, bool) {
	var w struct {
		ResourceName         string `json:"resourceName"`
		Campaign             string `json:"campaign"`
		GoalConfigLevel      string `json:"goalConfigLevel"`
		CustomConversionGoal string `json:"customConversionGoal"`
	}
	if json.Unmarshal(raw, &w) != nil || (w.ResourceName != "" && w.ResourceName != name) {
		return nil, false
	}
	campaignPrefix := customerName(customerID) + "/" + campaignCollection + "/"
	goalPrefix := customerName(customerID) + "/" + customConversionGoalCollection + "/"
	if w.Campaign != "" && w.Campaign != campaignPrefix+strings.TrimPrefix(name, customerName(customerID)+
		conversionGoalCampaignConfigService+"/") {
		return nil, false
	}
	if w.CustomConversionGoal != "" && !strings.HasPrefix(w.CustomConversionGoal, goalPrefix) {
		return nil, false
	}
	return &goalConfigResource{ResourceName: name, Campaign: boundedText(w.Campaign, maxText),
		GoalConfigLevel: code(w.GoalConfigLevel), CustomConversionGoal: boundedText(w.CustomConversionGoal, maxText)}, true
}

func goalSettingsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, tool string) (any, error) {
	op := map[string]string{customerConversionGoalsUpdateID: "update customer conversion goals",
		campaignConversionGoalsUpdateID:       "update campaign conversion goals",
		conversionGoalCampaignConfigsUpdateID: "update conversion goal campaign configs"}[tool]
	var args goalSettingsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkContentType(args.ResponseContentType); err != nil {
		return nil, err
	}
	operations, names, err := args.operations(tool)
	if err != nil {
		return nil, err
	}
	service, key := customerConversionGoalService, ""
	switch tool {
	case campaignConversionGoalsUpdateID:
		service = campaignConversionGoalService
	case conversionGoalCampaignConfigsUpdateID:
		service, key = conversionGoalCampaignConfigService, "conversionGoalCampaignConfig"
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(args.CustomerID, tool == customerConversionGoalsUpdateID)
	if err != nil {
		return nil, err
	}
	prefix := customerName(args.CustomerID) + service + "/"
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: args.CustomerID, service: service,
		resourceKey: key, operations: operations, names: names, validateOnly: args.ValidateOnly,
		content: args.ResponseContentType,
		idOf: func(name string) (string, bool) {
			rest, ok := strings.CutPrefix(name, prefix)
			return rest, ok && rest != ""
		}})
	if err != nil {
		return nil, err
	}
	out := goalSettingsOutput{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Results: []goalSettingsResult{}}
	// The arguments were decoded once by operations, so these decodings cannot fail and the lists have the length
	// of the results.
	var goals []goalSettingItem
	var configs []goalConfigItem
	_ = json.Unmarshal(args.Goals, &goals)
	_ = json.Unmarshal(args.Configs, &configs)
	for i, r := range results {
		item := goalSettingsResult{ResourceName: r.ResourceName}
		switch tool {
		case customerConversionGoalsUpdateID, campaignConversionGoalsUpdateID:
			item.Category, item.Origin = goals[i].Category, goals[i].Origin
			if tool == campaignConversionGoalsUpdateID {
				item.CampaignID = goals[i].CampaignID
			}
		default:
			item.CampaignID = configs[i].CampaignID
			if len(r.Resource) > 0 {
				config, ok := goalConfigOf(r.Resource, args.CustomerID, r.ResourceName)
				if !ok {
					return nil, unsureResult(op, "answered with a config that is not the one asked for")
				}
				item.Config = config
			}
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

func handleCustomerConversionGoalsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return goalSettingsChange(ctx, resolved, secrets, red, raw, customerConversionGoalsUpdateID)
}

func handleCampaignConversionGoalsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return goalSettingsChange(ctx, resolved, secrets, red, raw, campaignConversionGoalsUpdateID)
}

func handleConversionGoalCampaignConfigsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return goalSettingsChange(ctx, resolved, secrets, red, raw, conversionGoalCampaignConfigsUpdateID)
}

// goalSettingsSummary phrases the change for the confirmation, from the arguments alone.
func goalSettingsSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a goalSettingsArguments
		_ = json.Unmarshal(raw, &a)
		prefix := ""
		if a.ValidateOnly {
			prefix = "Only validate: "
		}
		where := " in Google Ads account " + a.CustomerID
		var parts []string
		if tool == conversionGoalCampaignConfigsUpdateID {
			var items []goalConfigItem
			_ = json.Unmarshal(a.Configs, &items)
			for _, item := range items {
				part := "campaign " + item.CampaignID
				if item.GoalConfigLevel != "" {
					part += " level=" + item.GoalConfigLevel
				}
				if item.CustomConversionGoalID != "" {
					part += " custom goal " + item.CustomConversionGoalID
				}
				parts = append(parts, part)
			}
			return prefix + "Change the conversion goal settings of " + strconv.Itoa(len(items)) + " campaign(s)" + where +
				" (" + strings.Join(parts, "; ") + "). This changes which conversion goals the campaigns optimize for."
		}
		var items []goalSettingItem
		_ = json.Unmarshal(a.Goals, &items)
		scope := "account"
		for _, item := range items {
			part := item.Category + "/" + item.Origin
			if tool == campaignConversionGoalsUpdateID {
				part = "campaign " + item.CampaignID + " " + part
				scope = "campaign"
			}
			if item.Biddable != nil {
				part += " biddable=" + strconv.FormatBool(*item.Biddable)
			}
			parts = append(parts, part)
		}
		return prefix + "Change the biddability of " + strconv.Itoa(len(items)) + " conversion goal(s) at " + scope +
			" level" + where + " (" + strings.Join(parts, "; ") + "). This changes which conversions campaigns bid for."
	}
}

// --- schemas and descriptors ---

var (
	goalResponseDoc = []capability.Argument{validateOnlyArgument}

	goalCustomerDoc = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of " +
		"the account whose goals change, without hyphens; a target of the connection or an account below a manager target"}

	goalBiddableSchema = booleanSchema
	goalAccountItem    = object(schema{"category": schema{"type": "string", "enum": conversionCategories},
		"origin": schema{"type": "string", "enum": conversionOrigins}, "biddable": goalBiddableSchema},
		"category", "origin", "biddable")
	goalCampaignItem = object(schema{"campaign_id": schema{"type": "string", "pattern": numericIDPattern.String()},
		"category": schema{"type": "string", "enum": conversionCategories},
		"origin":   schema{"type": "string", "enum": conversionOrigins}, "biddable": goalBiddableSchema},
		"campaign_id", "category", "origin", "biddable")
	goalConfigItemSchema = func() schema {
		s := object(schema{"campaign_id": schema{"type": "string", "pattern": numericIDPattern.String()},
			"goal_config_level":         schema{"type": "string", "enum": goalConfigLevels},
			"custom_conversion_goal_id": schema{"type": "string", "pattern": numericIDPattern.String()}}, "campaign_id")
		s["minProperties"] = 2
		return s
	}()

	goalSettingOutputSchema = encoded(object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"campaign_id": stringSchema, "category": stringSchema, "origin": stringSchema,
			"resource_name": stringSchema, "conversion_goal_campaign_config": object(schema{"resource_name": stringSchema,
				"campaign": stringSchema, "goal_config_level": stringSchema, "custom_conversion_goal": stringSchema})},
			"resource_name"))}, "customer_id", "validate_only", "results"))

	goalSettingFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the request: results is then empty"},
		{Name: "results", Description: "One entry per operation, in the order of the request: the campaign_id, " +
			"category and origin asked for (where the tool has them) and resource_name as Google answered, always " +
			"of the account of the call. The config tool adds, with MUTABLE_RESOURCE, the " +
			"conversion_goal_campaign_config: resource_name, campaign, goal_config_level, custom_conversion_goal"},
	}
)

func goalInputSchema(member string, items schema, content bool) json.RawMessage {
	properties := schema{"customer_id": customerIDArgument, "validate_only": validateOnlySchema,
		member: schema{"type": "array", "minItems": 1, "maxItems": maxGoalOperations, "items": items}}
	if content {
		properties["response_content_type"] = responseContentTypeSchema
	}
	return encoded(object(properties, "customer_id", member))
}

const goalSettingNote = " The change decides which conversions campaigns bid for, so it can move spend. Up to 20 entries go in " +
	"one request without partial_failure. The resource names are built from customer_id and the checked parts; no " +
	"argument takes a resource name. The settings are read with reports.search on the resource."

var goalUpdateRisk = changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
	"conversion goal settings of a Google Ads account")

var customerConversionGoalsUpdate = capability.Descriptor{
	ID: customerConversionGoalsUpdateID, Version: 1, Title: "Change the biddability of conversion goals of a Google Ads account",
	Description: "Turn conversion goals on or off for the bidding of a whole Google Ads account " +
		"(CustomerConversionGoalService.MutateCustomerConversionGoals with update operations and the mask biddable). " +
		"A goal is the pair of a conversion category and a conversion origin; biddable decides whether the conversion " +
		"actions of that pair count for the bidding of the account's campaigns by default." + goalSettingNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "goals", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: goalUpdateRisk, InputSchema: goalInputSchema("goals", goalAccountItem, false), OutputSchema: goalSettingOutputSchema,
	Arguments: append([]capability.Argument{goalCustomerDoc,
		{Name: "goals", Required: true, Description: "One to 20 entries, each with category (a ConversionActionCategory " +
			"such as PURCHASE), origin (WEBSITE, GOOGLE_HOSTED, APP, CALL_FROM_ADS, STORE, YOUTUBE_HOSTED or " +
			"LOCAL_SERVICES_ADS) and biddable (true or false); each pair at most once"}}, goalResponseDoc...),
	Fields: goalSettingFields,
	Examples: []capability.Example{{Description: "Take a goal out of bidding, validated only",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","goals":[{"category":"PAGE_VIEW","origin":"WEBSITE",` +
			`"biddable":false}],"validate_only":true}`)}},
}

var campaignConversionGoalsUpdate = capability.Descriptor{
	ID: campaignConversionGoalsUpdateID, Version: 1, Title: "Change the biddability of conversion goals of campaigns",
	Description: "Turn conversion goals on or off for the bidding of single campaigns " +
		"(CampaignConversionGoalService.MutateCampaignConversionGoals with update operations and the mask biddable). " +
		"The goal is the pair of a conversion category and a conversion origin. A campaign that was never changed " +
		"inherits the account default; to keep a campaign from bidding on a goal, set biddable to false for it " +
		"explicitly." + goalSettingNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "goals", "campaigns", "update"}, Provider: Provider,
	RequiresToolAllowList: true, Risk: goalUpdateRisk,
	InputSchema: goalInputSchema("goals", goalCampaignItem, false), OutputSchema: goalSettingOutputSchema,
	Arguments: append([]capability.Argument{goalCustomerDoc,
		{Name: "goals", Required: true, Description: "One to 20 entries, each with campaign_id (numeric, as " +
			"reports.search returns it), category, origin (as for customer-conversion-goals.update) and biddable " +
			"(true or false); each campaign, category and origin at most once"}}, goalResponseDoc...),
	Fields: goalSettingFields,
	Examples: []capability.Example{{Description: "Keep one campaign from bidding on a goal, validated only",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","goals":[{"campaign_id":"5544332211","category":"PURCHASE",` +
			`"origin":"WEBSITE","biddable":false}],"validate_only":true}`)}},
}

var conversionGoalCampaignConfigsUpdate = capability.Descriptor{
	ID: conversionGoalCampaignConfigsUpdateID, Version: 1, Title: "Change the conversion goal configuration of campaigns",
	Description: "Set which conversion goals single campaigns optimize for " +
		"(ConversionGoalCampaignConfigService.MutateConversionGoalCampaignConfigs with update operations and a mask " +
		"of exactly the given settings): the goal config level (CUSTOMER uses the goals of the account, CAMPAIGN the " +
		"goals of the campaign) and the custom conversion goal of the campaign. The campaign itself cannot be " +
		"changed. A setting is set, never cleared." + goalSettingNote + mutateListListedNote,
	Tags: []string{"google", "ads", "conversions", "goals", "campaigns", "update"}, Provider: Provider,
	RequiresToolAllowList: true, Risk: goalUpdateRisk,
	InputSchema: goalInputSchema("configs", goalConfigItemSchema, true), OutputSchema: goalSettingOutputSchema,
	Arguments: append([]capability.Argument{goalCustomerDoc,
		{Name: "configs", Required: true, Description: "One to 20 entries, each with campaign_id (numeric) and at " +
			"least one of goal_config_level (CUSTOMER or CAMPAIGN) and custom_conversion_goal_id (numeric ID of a " +
			"custom conversion goal of the same account); each campaign at most once"}},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: goalSettingFields,
	Examples: []capability.Example{{Description: "Let a campaign use the goals of its own level, validated only",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","configs":[{"campaign_id":"5544332211",` +
			`"goal_config_level":"CAMPAIGN"}],"validate_only":true}`)}},
}
