package googleads

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	goalsCreateID = Provider + ".goals.create"
	goalsUpdateID = Provider + ".goals.update"

	goalService    = "/goals"
	goalCollection = "goals"
)

// The three goal types of v26 that a request names; the type is set by the settings member that carries the
// value settings, so it cannot change after creation. UNSPECIFIED and UNKNOWN are no values to send.
var (
	goalTypes = []string{"CUSTOMER_RETENTION", "LOYALTY_RETENTION", "NEW_CUSTOMER_ACQUISITION"}

	goalSettingsMembers = map[string]string{"CUSTOMER_RETENTION": "retentionGoalSettings",
		"LOYALTY_RETENTION": "loyaltyRetentionGoalSettings", "NEW_CUSTOMER_ACQUISITION": "newCustomerAcquisitionGoalSettings"}
)

// lifecycleValueSettings is CustomerLifecycleOptimizationValueSettings: two oneofs, each with an incremental
// value or a multiplier. Google documents no range, so the bound of finite and not negative is Qatlas's own.
type lifecycleValueSettings struct {
	AdditionalValue             *float64 `json:"additional_value"`
	ValueMultiplier             *float64 `json:"value_multiplier"`
	AdditionalHighLifetimeValue *float64 `json:"additional_high_lifetime_value"`
	HighLifetimeValueMultiplier *float64 `json:"high_lifetime_value_multiplier"`
}

func checkLifecycleNumber(v *float64) bool {
	return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0)
}

// check refuses a value settings message that sets nothing, sets both members of a oneof, or a number outside
// the bound. That the high lifetime value exceeds the value is Google's to validate.
func (v *lifecycleValueSettings) check() error {
	if v.AdditionalValue == nil && v.ValueMultiplier == nil && v.AdditionalHighLifetimeValue == nil &&
		v.HighLifetimeValueMultiplier == nil {
		return badRequest("value settings name at least one of additional_value, value_multiplier, " +
			"additional_high_lifetime_value and high_lifetime_value_multiplier")
	}
	if v.AdditionalValue != nil && v.ValueMultiplier != nil {
		return badRequest("value settings take additional_value or value_multiplier, not both")
	}
	if v.AdditionalHighLifetimeValue != nil && v.HighLifetimeValueMultiplier != nil {
		return badRequest("value settings take additional_high_lifetime_value or high_lifetime_value_multiplier, not both")
	}
	for _, n := range []*float64{v.AdditionalValue, v.ValueMultiplier, v.AdditionalHighLifetimeValue, v.HighLifetimeValueMultiplier} {
		if !checkLifecycleNumber(n) {
			return badRequest("value settings numbers must be finite and 0 or more")
		}
	}
	return nil
}

func (v *lifecycleValueSettings) wire() map[string]any {
	out := map[string]any{}
	for member, n := range map[string]*float64{"additionalValue": v.AdditionalValue, "valueMultiplier": v.ValueMultiplier,
		"additionalHighLifetimeValue": v.AdditionalHighLifetimeValue, "highLifetimeValueMultiplier": v.HighLifetimeValueMultiplier} {
		if n != nil {
			out[member] = *n
		}
	}
	return out
}

var lifecycleValueSettingsSchema = func() schema {
	number := schema{"type": "number", "minimum": 0}
	s := object(schema{"additional_value": number, "value_multiplier": number,
		"additional_high_lifetime_value": number, "high_lifetime_value_multiplier": number})
	s["minProperties"] = 1
	return s
}()

const lifecycleValueSettingsDoc = "value settings with at least one of additional_value or value_multiplier " +
	"(conversion value adjustment for customers of the lifecycle) and additional_high_lifetime_value or " +
	"high_lifetime_value_multiplier (for customers identified as high lifetime value), numbers of 0 or more and " +
	"never both of a pair; Google requires the high lifetime value to exceed the value when both are set"

type (
	goalItem struct {
		GoalID        string                  `json:"goal_id"`
		GoalType      string                  `json:"goal_type"`
		ValueSettings *lifecycleValueSettings `json:"value_settings"`
	}
	goalsArguments struct {
		CustomerID   string     `json:"customer_id"`
		Goals        []goalItem `json:"goals"`
		ValidateOnly bool       `json:"validate_only"`
	}
	goalResult struct {
		ResourceName string `json:"resource_name"`
		GoalID       string `json:"goal_id"`
	}
	goalsChanged struct {
		CustomerID   string       `json:"customer_id"`
		ValidateOnly bool         `json:"validate_only"`
		Results      []goalResult `json:"results"`
	}
)

// operations checks the entries and builds the operations; names holds the resource name an update names.
func (a goalsArguments) operations(create bool) ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if n := len(a.Goals); n == 0 || n > maxListOperations {
		return nil, nil, badRequest("goals must hold one to 10 entries")
	}
	var ops []any
	var names, seen []string
	for _, g := range a.Goals {
		if !slices.Contains(goalTypes, g.GoalType) {
			return nil, nil, badRequest("goal_type must be CUSTOMER_RETENTION, LOYALTY_RETENTION or NEW_CUSTOMER_ACQUISITION")
		}
		member := goalSettingsMembers[g.GoalType]
		settings := map[string]any{}
		if g.ValueSettings != nil {
			if err := g.ValueSettings.check(); err != nil {
				return nil, nil, err
			}
			settings["valueSettings"] = g.ValueSettings.wire()
		}
		if create {
			if g.GoalID != "" {
				return nil, nil, badRequest("a new goal takes no goal_id")
			}
			ops = append(ops, map[string]any{"create": map[string]any{member: settings}})
			continue
		}
		if !numericIDPattern.MatchString(g.GoalID) {
			return nil, nil, badRequest("every entry needs the goal_id of an existing goal, a positive number")
		}
		if g.ValueSettings == nil {
			return nil, nil, badRequest("every entry needs value_settings to change")
		}
		if slices.Contains(seen, g.GoalID) {
			return nil, nil, badRequest("a goal appears in one entry only")
		}
		seen = append(seen, g.GoalID)
		name := customerName(a.CustomerID) + "/" + goalCollection + "/" + g.GoalID
		names = append(names, name)
		ops = append(ops, map[string]any{"update": map[string]any{"resourceName": name, member: settings},
			"updateMask": member + ".valueSettings"})
	}
	return ops, names, nil
}

var (
	goalOutputSchema = encoded(object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"resource_name": stringSchema, "goal_id": stringSchema},
			"resource_name", "goal_id"))}, "customer_id", "validate_only", "results"))

	goalFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "results", Description: "One result per entry, in the order of the entries, empty after validate_only: " +
			"resource_name (customers/ID/goals/N, as Google answered, always of the account of the call) and goal_id " +
			"(its number). Google answers the resource name only: MutateGoalsRequest has no response_content_type"}}

	goalCustomerArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit customer " +
		"ID of the account of the goals, without hyphens; a target of the connection or an account below a manager " +
		"target. A manager account needs a manager target that names it"}
)

const goalNote = " A goal defines what a campaign can optimize for and bid on beyond plain conversions: the value " +
	"settings change the conversion value that bidding sees for customers of a lifecycle, so they change spend. " +
	"The goal type is fixed by its settings, and goal_type names it because the request needs it. Goals have no " +
	"remove method in the API. They are read with reports.search on goal. A goal takes effect on a campaign only " +
	"through a campaign goal config (campaign-goal-configs.create). The reference marks no field as required, " +
	"names no bound of the numbers, and does not say which accounts may use the lifecycle goals, so Google " +
	"validates that."

var goalsCreate = capability.Descriptor{
	ID: goalsCreateID, Version: 1, Title: "Create goals of a Google Ads account",
	Description: "Create one to 10 customer lifecycle goals in a Google Ads account (GoalService.MutateGoals with " +
		"create operations): each names a goal_type (CUSTOMER_RETENTION, LOYALTY_RETENTION or " +
		"NEW_CUSTOMER_ACQUISITION) and optionally " + lifecycleValueSettingsDoc + "." + goalNote + mutateListListedNote,
	Tags: []string{"google", "ads", "goals", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent,
		"optimization goals of a Google Ads account that steer campaign bidding"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"goals": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"goal_type": schema{"type": "string", "enum": goalTypes},
				"value_settings": lifecycleValueSettingsSchema}, "goal_type")},
		"validate_only": validateOnlySchema}, "customer_id", "goals")),
	OutputSchema: goalOutputSchema,
	Arguments: []capability.Argument{goalCustomerArgument,
		{Name: "goals", Required: true, Description: "One to 10 entries, each with goal_type and optionally " +
			lifecycleValueSettingsDoc}, validateOnlyArgument},
	Fields: goalFields,
	Examples: []capability.Example{{Description: "Validate a new customer acquisition goal without creating it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","goals":[{"goal_type":"NEW_CUSTOMER_ACQUISITION",` +
			`"value_settings":{"additional_value":10}}],"validate_only":true}`)}},
}

var goalsUpdate = capability.Descriptor{
	ID: goalsUpdateID, Version: 1, Title: "Change goals of a Google Ads account",
	Description: "Replace the value settings of one to 10 existing goals of a Google Ads account (GoalService." +
		"MutateGoals with update operations and the mask <settings>.value_settings): each entry names the goal_id, " +
		"its goal_type and the new " + lifecycleValueSettingsDoc + ". The value settings are replaced as a whole, " +
		"not merged, and cannot be cleared." + goalNote + mutateListListedNote,
	Tags: []string{"google", "ads", "goals", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"optimization goals of a Google Ads account that steer campaign bidding"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"goals": schema{"type": "array", "minItems": 1, "maxItems": maxListOperations,
			"items": object(schema{"goal_id": schema{"type": "string", "pattern": numericIDPattern.String()},
				"goal_type":      schema{"type": "string", "enum": goalTypes},
				"value_settings": lifecycleValueSettingsSchema}, "goal_id", "goal_type", "value_settings")},
		"validate_only": validateOnlySchema}, "customer_id", "goals")),
	OutputSchema: goalOutputSchema,
	Arguments: []capability.Argument{goalCustomerArgument,
		{Name: "goals", Required: true, Description: "One to 10 entries, each with goal_id (the number at the end of " +
			"the resource name that reports.search returns, each goal once), the goal_type of that goal and the new " +
			lifecycleValueSettingsDoc}, validateOnlyArgument},
	Fields: goalFields,
	Examples: []capability.Example{{Description: "Validate a new value multiplier for a goal",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","goals":[{"goal_id":"123","goal_type":"LOYALTY_RETENTION",` +
			`"value_settings":{"value_multiplier":1.2}}],"validate_only":true}`)}},
}

func handleGoalsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return goalsChange(ctx, resolved, secrets, red, raw, true)
}

func handleGoalsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return goalsChange(ctx, resolved, secrets, red, raw, false)
}

func goalsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, create bool) (any, error) {
	op, tool := "update goals", goalsUpdateID
	if create {
		op, tool = "create goals", goalsCreateID
	}
	var args goalsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	operations, names, err := args.operations(create)
	if err != nil {
		return nil, err
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: args.CustomerID,
		service: goalService, collection: goalCollection, operations: operations, names: names,
		validateOnly: args.ValidateOnly})
	if err != nil {
		return nil, err
	}
	out := goalsChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Results: []goalResult{}}
	for _, r := range results {
		out.Results = append(out.Results, goalResult{ResourceName: r.ResourceName, GoalID: r.ID})
	}
	return out, nil
}

// goalsSummary phrases the change for the confirmation, from the arguments alone.
func goalsSummary(create bool) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a goalsArguments
		_ = json.Unmarshal(raw, &a)
		var parts []string
		for _, g := range a.Goals {
			if create {
				parts = append(parts, "create "+boundedText(g.GoalType, 40))
			} else {
				parts = append(parts, "replace the value settings of goal "+boundedText(g.GoalID, 20))
			}
		}
		lead := "Goals of Google Ads account " + a.CustomerID + ": "
		if a.ValidateOnly {
			lead = "Only validate, goals of Google Ads account " + a.CustomerID + ": "
		}
		return lead + strings.Join(parts, "; ") + ". Goals change what campaigns optimize and bid for."
	}
}
