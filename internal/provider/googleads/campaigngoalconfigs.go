package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	campaignGoalConfigsCreateID = Provider + ".campaign-goal-configs.create"
	campaignGoalConfigsUpdateID = Provider + ".campaign-goal-configs.update"
	campaignGoalConfigsRemoveID = Provider + ".campaign-goal-configs.remove"

	campaignGoalConfigService    = "/campaignGoalConfigs"
	campaignGoalConfigCollection = "campaignGoalConfigs"
)

// The settings member of a campaign goal config follows the type of its goal. campaign and goal are immutable.
var (
	campaignGoalSettingsMembers = map[string]string{"CUSTOMER_RETENTION": "campaignRetentionSettings",
		"LOYALTY_RETENTION": "campaignLoyaltyRetentionSettings", "NEW_CUSTOMER_ACQUISITION": "campaignNewCustomerAcquisitionSettings"}
	targetOptions = []string{"TARGET_ALL", "TARGET_SPECIFIC"}
)

type (
	campaignGoalConfigItem struct {
		CampaignID string `json:"campaign_id"`
		GoalID     string `json:"goal_id"`
		GoalType   string `json:"goal_type"`
		// TargetOption is for the customer retention and new customer acquisition settings, the two booleans
		// for the loyalty retention settings.
		TargetOption                   string                  `json:"target_option"`
		EnableBidAdjustmentsForMembers *bool                   `json:"enable_bid_adjustments_for_loyalty_members"`
		ShowMemberBenefitsInPLA        *bool                   `json:"show_targeted_loyalty_member_benefits_in_pla"`
		ValueSettingsOverride          *lifecycleValueSettings `json:"value_settings_override"`
	}
	campaignGoalConfigsArguments struct {
		CustomerID          string                   `json:"customer_id"`
		CampaignGoalConfigs []campaignGoalConfigItem `json:"campaign_goal_configs"`
		ValidateOnly        bool                     `json:"validate_only"`
	}
)

func campaignGoalConfigName(customerID, campaignID, goalID string) string {
	return customerName(customerID) + "/" + campaignGoalConfigCollection + "/" + campaignID + "~" + goalID
}

// campaignGoalConfigIDOf returns campaign~goal of a resource name of exactly the checked account.
func campaignGoalConfigIDOf(customerID string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+campaignGoalConfigCollection+"/")
		campaign, goal, found := strings.Cut(rest, "~")
		return rest, ok && found && numericIDPattern.MatchString(campaign) && numericIDPattern.MatchString(goal)
	}
}

// settings builds the members of the settings message and their mask paths, relative to the message.
func (i campaignGoalConfigItem) settings() (map[string]any, []string, error) {
	out := map[string]any{}
	var mask []string
	loyalty := i.GoalType == "LOYALTY_RETENTION"
	if i.TargetOption != "" {
		if loyalty || !slices.Contains(targetOptions, i.TargetOption) {
			return nil, nil, badRequest("target_option is TARGET_ALL or TARGET_SPECIFIC and belongs to the goal types " +
				"CUSTOMER_RETENTION and NEW_CUSTOMER_ACQUISITION")
		}
		out["targetOption"] = i.TargetOption
		mask = append(mask, "targetOption")
	}
	if i.EnableBidAdjustmentsForMembers != nil || i.ShowMemberBenefitsInPLA != nil {
		if !loyalty {
			return nil, nil, badRequest("the loyalty member settings belong to the goal type LOYALTY_RETENTION")
		}
	}
	if i.EnableBidAdjustmentsForMembers != nil {
		out["enableBidAdjustmentsForLoyaltyMembers"] = *i.EnableBidAdjustmentsForMembers
		mask = append(mask, "enableBidAdjustmentsForLoyaltyMembers")
	}
	if i.ShowMemberBenefitsInPLA != nil {
		out["showTargetedLoyaltyMemberBenefitsInPla"] = *i.ShowMemberBenefitsInPLA
		mask = append(mask, "showTargetedLoyaltyMemberBenefitsInPla")
	}
	if i.ValueSettingsOverride != nil {
		if err := i.ValueSettingsOverride.check(); err != nil {
			return nil, nil, err
		}
		out["valueSettingsOverride"] = i.ValueSettingsOverride.wire()
		mask = append(mask, "valueSettingsOverride")
	}
	return out, mask, nil
}

// operations checks the entries and builds the operations. The resource names of campaign and goal are built
// from the checked customer_id and the numeric IDs, so no entry can name an object of another account.
func (a campaignGoalConfigsArguments) operations(tool string) ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if n := len(a.CampaignGoalConfigs); n == 0 || n > maxListOperations {
		return nil, nil, badRequest("campaign_goal_configs must hold one to 10 entries")
	}
	var ops []any
	var names []string
	for _, i := range a.CampaignGoalConfigs {
		if !numericIDPattern.MatchString(i.CampaignID) || !numericIDPattern.MatchString(i.GoalID) {
			return nil, nil, badRequest("every entry needs campaign_id and goal_id, positive numbers")
		}
		name := campaignGoalConfigName(a.CustomerID, i.CampaignID, i.GoalID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("a campaign and goal pair appears in one entry only")
		}
		if tool == campaignGoalConfigsRemoveID {
			if i.GoalType != "" || i.TargetOption != "" || i.EnableBidAdjustmentsForMembers != nil ||
				i.ShowMemberBenefitsInPLA != nil || i.ValueSettingsOverride != nil {
				return nil, nil, badRequest("a removal names campaign_id and goal_id only")
			}
			names = append(names, name)
			ops = append(ops, map[string]any{"remove": name})
			continue
		}
		member := campaignGoalSettingsMembers[i.GoalType]
		if member == "" {
			return nil, nil, badRequest("goal_type must be CUSTOMER_RETENTION, LOYALTY_RETENTION or NEW_CUSTOMER_ACQUISITION")
		}
		settings, mask, err := i.settings()
		if err != nil {
			return nil, nil, err
		}
		if tool == campaignGoalConfigsCreateID {
			names = append(names, name)
			ops = append(ops, map[string]any{"create": map[string]any{
				"campaign": customerName(a.CustomerID) + "/campaigns/" + i.CampaignID,
				"goal":     customerName(a.CustomerID) + "/" + goalCollection + "/" + i.GoalID, member: settings}})
			continue
		}
		if len(mask) == 0 {
			return nil, nil, badRequest("every entry needs at least one setting to change")
		}
		for k := range mask {
			mask[k] = member + "." + mask[k]
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": map[string]any{"resourceName": name, member: settings},
			"updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

type (
	campaignGoalConfigResult struct {
		ResourceName string `json:"resource_name"`
		CampaignID   string `json:"campaign_id"`
		GoalID       string `json:"goal_id"`
	}
	campaignGoalConfigsChanged struct {
		CustomerID   string                     `json:"customer_id"`
		ValidateOnly bool                       `json:"validate_only"`
		Results      []campaignGoalConfigResult `json:"results"`
	}
)

var (
	campaignGoalConfigProperties = func() schema {
		return schema{"campaign_id": numericIDSchema, "goal_id": numericIDSchema,
			"goal_type":     schema{"type": "string", "enum": goalTypes},
			"target_option": schema{"type": "string", "enum": targetOptions},
			"enable_bid_adjustments_for_loyalty_members":   booleanSchema,
			"show_targeted_loyalty_member_benefits_in_pla": booleanSchema,
			"value_settings_override":                      lifecycleValueSettingsSchema}
	}

	campaignGoalConfigOutputSchema = encoded(object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema, "campaign_id": stringSchema, "goal_id": stringSchema},
			"resource_name", "campaign_id", "goal_id"))}, "customer_id", "validate_only", "results"))

	campaignGoalConfigFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One result per entry, in the order of the entries, empty after validate_only: " +
			"resource_name (customers/ID/campaignGoalConfigs/CAMPAIGN~GOAL, as Google answered, always of the account " +
			"of the call), campaign_id and goal_id. Google answers the resource name only: " +
			"MutateCampaignGoalConfigsRequest has no response_content_type"}}

	campaignGoalConfigCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit " +
		"customer ID of the account of the campaigns and goals, without hyphens; a target of the connection or an " +
		"account below a manager target. A manager account needs a manager target that names it"}
)

const campaignGoalConfigSettingsDoc = "goal_type (CUSTOMER_RETENTION, LOYALTY_RETENTION or NEW_CUSTOMER_ACQUISITION, " +
	"the type of the goal), target_option (TARGET_ALL or TARGET_SPECIFIC, for CUSTOMER_RETENTION and " +
	"NEW_CUSTOMER_ACQUISITION; Google lets only allowlisted accounts set it for CUSTOMER_RETENTION), " +
	"enable_bid_adjustments_for_loyalty_members and show_targeted_loyalty_member_benefits_in_pla (booleans, for " +
	"LOYALTY_RETENTION) and value_settings_override (" + lifecycleValueSettingsDoc + ")"

const campaignGoalConfigNote = " A campaign goal config attaches a goal to a campaign and decides what that campaign " +
	"optimizes and bids for, so it changes spend. campaign_id and goal_id are numbers of the account of customer_id: " +
	"the tool builds the resource names from it, so a campaign or goal of another account, including a goal that " +
	"a manager account owns, cannot be named. The campaign and the goal cannot change after creation. Read the " +
	"configs with reports.search on campaign_goal_config. The reference marks no field as required and says " +
	"nothing about which campaign types take which goal, so Google validates that."

func campaignGoalConfigRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return changeRisk(effect, idempotency, "optimization goal assignments of Google Ads campaigns that steer bidding")
}

func campaignGoalConfigInput(items schema) json.RawMessage {
	return encoded(object(schema{"customer_id": customerIDArgument,
		"campaign_goal_configs": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations, "items": items},
		"validate_only":         validateOnlySchema}, "customer_id", "campaign_goal_configs"))
}

var campaignGoalConfigsCreate = capability.Descriptor{
	ID: campaignGoalConfigsCreateID, Version: 1, Title: "Attach goals to campaigns of a Google Ads account",
	Description: "Create one to 10 campaign goal configs in a Google Ads account " +
		"(CampaignGoalConfigService.MutateCampaignGoalConfigs with create operations): each attaches the goal " +
		"goal_id to the campaign campaign_id, names the goal_type and optionally sets " + campaignGoalConfigSettingsDoc +
		"." + campaignGoalConfigNote + mutateListListedNote,
	Tags: []string{"google", "ads", "goals", "campaigns", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: campaignGoalConfigRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: campaignGoalConfigInput(object(campaignGoalConfigProperties(),
		"campaign_id", "goal_id", "goal_type")),
	OutputSchema: campaignGoalConfigOutputSchema,
	Arguments: []capability.Argument{campaignGoalConfigCustomerArgument,
		{Name: "campaign_goal_configs", Required: true, Description: "One to 10 entries, each with campaign_id, goal_id " +
			"and goal_type, optionally " + campaignGoalConfigSettingsDoc}, validateOnlyArgument},
	Fields: campaignGoalConfigFields,
	Examples: []capability.Example{{Description: "Validate attaching a goal to a campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_goal_configs":[{"campaign_id":"111",` +
			`"goal_id":"222","goal_type":"NEW_CUSTOMER_ACQUISITION","target_option":"TARGET_SPECIFIC"}],"validate_only":true}`)}},
}

var campaignGoalConfigsUpdate = capability.Descriptor{
	ID: campaignGoalConfigsUpdateID, Version: 1, Title: "Change goal settings of campaigns of a Google Ads account",
	Description: "Change the settings of one to 10 existing campaign goal configs of a Google Ads account " +
		"(CampaignGoalConfigService.MutateCampaignGoalConfigs with update operations and a mask of exactly the given " +
		"settings): each entry names campaign_id and goal_id and goal_type and at least one of " +
		campaignGoalConfigSettingsDoc + ". Settings are set, not cleared; value_settings_override is replaced as a " +
		"whole." + campaignGoalConfigNote + mutateListListedNote,
	Tags: []string{"google", "ads", "goals", "campaigns", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: campaignGoalConfigRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: campaignGoalConfigInput(func() schema {
		s := object(campaignGoalConfigProperties(), "campaign_id", "goal_id", "goal_type")
		s["minProperties"] = 4
		return s
	}()),
	OutputSchema: campaignGoalConfigOutputSchema,
	Arguments: []capability.Argument{campaignGoalConfigCustomerArgument,
		{Name: "campaign_goal_configs", Required: true, Description: "One to 10 entries, each with campaign_id, goal_id, " +
			"goal_type and at least one setting of: " + campaignGoalConfigSettingsDoc}, validateOnlyArgument},
	Fields: campaignGoalConfigFields,
	Examples: []capability.Example{{Description: "Validate switching a campaign to all customers",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_goal_configs":[{"campaign_id":"111",` +
			`"goal_id":"222","goal_type":"NEW_CUSTOMER_ACQUISITION","target_option":"TARGET_ALL"}],"validate_only":true}`)}},
}

var campaignGoalConfigsRemove = capability.Descriptor{
	ID: campaignGoalConfigsRemoveID, Version: 1, Title: "Detach goals from campaigns of a Google Ads account",
	Description: "Remove one to 10 campaign goal configs of a Google Ads account " +
		"(CampaignGoalConfigService.MutateCampaignGoalConfigs with remove operations of " +
		"customers/ID/campaignGoalConfigs/CAMPAIGN~GOAL): the campaign no longer optimizes for the goal. The goal " +
		"itself stays. The tool builds the names from customer_id, campaign_id and goal_id and takes no settings. " +
		"The service answers the resource names only." + mutateListListedNote,
	Tags: []string{"google", "ads", "goals", "campaigns", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: campaignGoalConfigRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: campaignGoalConfigInput(object(schema{"campaign_id": numericIDSchema, "goal_id": numericIDSchema},
		"campaign_id", "goal_id")),
	OutputSchema: campaignGoalConfigOutputSchema,
	Arguments: []capability.Argument{campaignGoalConfigCustomerArgument,
		{Name: "campaign_goal_configs", Required: true, Description: "One to 10 entries, each with campaign_id and " +
			"goal_id, each pair once"}, validateOnlyArgument},
	Fields: campaignGoalConfigFields,
	Examples: []capability.Example{{Description: "Validate detaching a goal from a campaign",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_goal_configs":[{"campaign_id":"111",` +
			`"goal_id":"222"}],"validate_only":true}`)}},
}

func handleCampaignGoalConfigsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return campaignGoalConfigsChange(ctx, resolved, secrets, red, raw, campaignGoalConfigsCreateID)
}

func handleCampaignGoalConfigsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return campaignGoalConfigsChange(ctx, resolved, secrets, red, raw, campaignGoalConfigsUpdateID)
}

func handleCampaignGoalConfigsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return campaignGoalConfigsChange(ctx, resolved, secrets, red, raw, campaignGoalConfigsRemoveID)
}

func campaignGoalConfigsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, tool string) (any, error) {
	op := map[string]string{campaignGoalConfigsCreateID: "create campaign goal configs",
		campaignGoalConfigsUpdateID: "update campaign goal configs", campaignGoalConfigsRemoveID: "remove campaign goal configs"}[tool]
	var args campaignGoalConfigsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	operations, names, err := args.operations(tool)
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: args.CustomerID,
		service: campaignGoalConfigService, collection: campaignGoalConfigCollection, operations: operations,
		names: names, idOf: campaignGoalConfigIDOf(args.CustomerID), validateOnly: args.ValidateOnly})
	if err != nil {
		return nil, err
	}
	out := campaignGoalConfigsChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly,
		Results: []campaignGoalConfigResult{}}
	for _, r := range results {
		campaign, goal, _ := strings.Cut(r.ID, "~")
		out.Results = append(out.Results, campaignGoalConfigResult{ResourceName: r.ResourceName, CampaignID: campaign, GoalID: goal})
	}
	return out, nil
}

// campaignGoalConfigsSummary phrases the change for the confirmation, from the arguments alone.
func campaignGoalConfigsSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a campaignGoalConfigsArguments
		_ = json.Unmarshal(raw, &a)
		verb, tail := "attach", "The campaigns then optimize and bid for these goals."
		switch tool {
		case campaignGoalConfigsUpdateID:
			verb, tail = "change the settings of", "This can change what the campaigns optimize and bid for."
		case campaignGoalConfigsRemoveID:
			verb, tail = "detach", "The campaigns no longer optimize for these goals."
		}
		var parts []string
		for _, i := range a.CampaignGoalConfigs {
			parts = append(parts, "goal "+boundedText(i.GoalID, 20)+" on campaign "+boundedText(i.CampaignID, 20))
		}
		lead := "Google Ads account " + a.CustomerID + ": " + verb + " "
		if a.ValidateOnly {
			lead = "Only validate, Google Ads account " + a.CustomerID + ": " + verb + " "
		}
		return lead + strings.Join(parts, "; ") + ". " + tail
	}
}
