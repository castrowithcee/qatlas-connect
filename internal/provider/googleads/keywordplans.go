package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	keywordPlansCreateID = Provider + ".keyword-plans.create"
	keywordPlansUpdateID = Provider + ".keyword-plans.update"
	keywordPlansRemoveID = Provider + ".keyword-plans.remove"

	keywordPlansService = "/keywordPlans"
)

// Bounds of the keyword plan fields as the reference of v26 documents them: KeywordPlan.name is required and
// not empty on create, with no length stated; forecast_period holds one of date_interval and date_range; the
// dates of date_range are yyyy-mm-dd, inclusive, in the future, and span at most a year, which Google checks
// when a forecast runs. MutateKeywordPlansRequest has no response_content_type and its result holds the
// resource name only.
const (
	// keywordPlanNameMax is Qatlas's own bound, since the reference states none.
	keywordPlanNameMax = 255
	// keywordPlansMaxOperations is Qatlas's own bound per call; Google accepts up to 10,000 operations per request.
	keywordPlansMaxOperations = 50
	keywordPlanDateLayout     = "2006-01-02"
	keywordPlanDatePattern    = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`
)

var keywordPlanIntervals = []string{"NEXT_WEEK", "NEXT_MONTH", "NEXT_QUARTER"}

// keywordPlanName is the resource name of a keyword plan, built from checked IDs alone.
func keywordPlanName(customerID, planID string) string {
	return customerName(customerID) + "/keywordPlans/" + planID
}

func keywordPlansCheckName(value string) error {
	n := utf8.RuneCountInString(value)
	if n < 1 || n > keywordPlanNameMax {
		return badRequest("name must be 1 to " + strconv.Itoa(keywordPlanNameMax) + " characters")
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return badRequest("name must not hold control characters")
		}
	}
	return nil
}

func keywordPlansCheckCount(argument string, n int) error {
	if n < 1 || n > keywordPlansMaxOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(keywordPlansMaxOperations) + " entries")
	}
	return nil
}

// --- arguments ---

type (
	keywordPlansRange struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	keywordPlansPeriod struct {
		DateInterval *string            `json:"date_interval"`
		DateRange    *keywordPlansRange `json:"date_range"`
	}
	keywordPlansNew struct {
		Name           string              `json:"name"`
		ForecastPeriod *keywordPlansPeriod `json:"forecast_period"`
	}
	keywordPlansEdit struct {
		KeywordPlanID  string              `json:"keyword_plan_id"`
		Name           *string             `json:"name"`
		ForecastPeriod *keywordPlansPeriod `json:"forecast_period"`
	}
	keywordPlansCreateArguments struct {
		CustomerID   string            `json:"customer_id"`
		KeywordPlans []keywordPlansNew `json:"keyword_plans"`
		ValidateOnly bool              `json:"validate_only"`
	}
	keywordPlansUpdateArguments struct {
		CustomerID   string             `json:"customer_id"`
		KeywordPlans []keywordPlansEdit `json:"keyword_plans"`
		ValidateOnly bool               `json:"validate_only"`
	}
	keywordPlansRemoveArguments struct {
		CustomerID     string   `json:"customer_id"`
		KeywordPlanIDs []string `json:"keyword_plan_ids"`
		ValidateOnly   bool     `json:"validate_only"`
	}
)

// wire checks the forecast period, which holds exactly one of its two members, and returns it as Google takes it.
func (p keywordPlansPeriod) wire() (map[string]any, error) {
	if (p.DateInterval == nil) == (p.DateRange == nil) {
		return nil, badRequest("forecast_period must hold exactly one of date_interval and date_range")
	}
	if p.DateInterval != nil {
		if !slices.Contains(keywordPlanIntervals, *p.DateInterval) {
			return nil, badRequest("date_interval must be one of " + strings.Join(keywordPlanIntervals, ", "))
		}
		return map[string]any{"dateInterval": *p.DateInterval}, nil
	}
	start, err1 := time.Parse(keywordPlanDateLayout, p.DateRange.StartDate)
	end, err2 := time.Parse(keywordPlanDateLayout, p.DateRange.EndDate)
	if err1 != nil || err2 != nil {
		return nil, badRequest("date_range holds start_date and end_date as dates in the form yyyy-mm-dd")
	}
	if end.Before(start) {
		return nil, badRequest("date_range must not end before it starts")
	}
	return map[string]any{"dateRange": map[string]any{"startDate": p.DateRange.StartDate,
		"endDate": p.DateRange.EndDate}}, nil
}

// operations checks the arguments and builds the operations of the request.
func (a keywordPlansCreateArguments) operations() ([]any, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plans", len(a.KeywordPlans)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlans))
	for _, p := range a.KeywordPlans {
		if err := keywordPlansCheckName(p.Name); err != nil {
			return nil, err
		}
		create := map[string]any{"name": p.Name}
		if p.ForecastPeriod != nil {
			period, err := p.ForecastPeriod.wire()
			if err != nil {
				return nil, err
			}
			create["forecastPeriod"] = period
		}
		ops = append(ops, map[string]any{"create": create})
	}
	return ops, nil
}

func (a keywordPlansUpdateArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plans", len(a.KeywordPlans)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlans))
	names := make([]string, 0, len(a.KeywordPlans))
	for _, p := range a.KeywordPlans {
		if !numericIDPattern.MatchString(p.KeywordPlanID) {
			return nil, nil, badRequest("keyword_plan_id must be the ID of an existing keyword plan, a positive number")
		}
		name := keywordPlanName(a.CustomerID, p.KeywordPlanID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plans names each keyword_plan_id at most once")
		}
		if p.Name == nil && p.ForecastPeriod == nil {
			return nil, nil, badRequest("each entry of keyword_plans needs at least one of name and forecast_period")
		}
		update := map[string]any{"resourceName": name}
		var mask []string
		if p.Name != nil {
			if err := keywordPlansCheckName(*p.Name); err != nil {
				return nil, nil, err
			}
			update["name"] = *p.Name
			mask = append(mask, "name")
		}
		if p.ForecastPeriod != nil {
			period, err := p.ForecastPeriod.wire()
			if err != nil {
				return nil, nil, err
			}
			update["forecastPeriod"] = period
			mask = append(mask, "forecast_period")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"update": update, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a keywordPlansRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := keywordPlansCheckCount("keyword_plan_ids", len(a.KeywordPlanIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.KeywordPlanIDs))
	names := make([]string, 0, len(a.KeywordPlanIDs))
	for _, id := range a.KeywordPlanIDs {
		if !numericIDPattern.MatchString(id) {
			return nil, nil, badRequest("keyword_plan_ids must be IDs of existing keyword plans, positive numbers")
		}
		name := keywordPlanName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("keyword_plan_ids names each keyword plan at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	keywordPlansMutated struct {
		KeywordPlanID string `json:"keyword_plan_id"`
		ResourceName  string `json:"resource_name"`
	}
	keywordPlansChanged struct {
		CustomerID   string                `json:"customer_id"`
		ValidateOnly bool                  `json:"validate_only"`
		Results      []keywordPlansMutated `json:"results"`
	}
)

// keywordPlansResults maps the results. mutateList has bound them to the account and, for an update and a
// remove, to the names asked for; a create must name distinct plans.
func keywordPlansResults(op string, results []listResult) ([]keywordPlansMutated, error) {
	out := []keywordPlansMutated{}
	for _, r := range results {
		for _, previous := range out {
			if previous.KeywordPlanID == r.ID {
				return nil, unsureResult(op, "answered with the same keyword plan twice")
			}
		}
		out = append(out, keywordPlansMutated{KeywordPlanID: r.ID, ResourceName: r.ResourceName})
	}
	return out, nil
}

// --- descriptors ---

var (
	keywordPlanNameSchema = schema{"type": "string", "minLength": 1, "maxLength": keywordPlanNameMax}
	keywordPlanIDSchema   = schema{"type": "string", "pattern": numericIDPattern.String()}
	keywordPlanDateSchema = schema{"type": "string", "pattern": keywordPlanDatePattern}
	keywordPlanPeriod     = object(schema{
		"date_interval": schema{"type": "string", "enum": keywordPlanIntervals},
		"date_range": object(schema{"start_date": keywordPlanDateSchema, "end_date": keywordPlanDateSchema},
			"start_date", "end_date"),
	})

	keywordPlansOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"keyword_plan_id": stringSchema, "resource_name": stringSchema},
			"keyword_plan_id", "resource_name"))},
		"customer_id", "validate_only", "results")
)

const keywordPlansSensitivity = "keyword plans of a Google Ads account: their names and forecast periods, " +
	"which the confirmation names; no data of other people"

const keywordPlansNote = " A keyword plan is a saved Keyword Planner plan; this tool sets only its name and " +
	"forecast period, adds no campaign, ad group, or keyword, runs no forecast, and does not read plans (read " +
	"them with reports.search on the keyword_plan resource). Google holds at most 10000 saved plans per " +
	"account and allows removing plans when the limit is reached. Google may block the keyword planning " +
	"services at the Explorer access level, so the Cloud project of the credential then needs Basic access. The " +
	"request goes to KeywordPlanService.MutateKeywordPlans with up to 50 operations, atomic and without " +
	"partial_failure: Google applies all of them or none."

var keywordPlansFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: keyword_plan_id and " +
		"resource_name (customers/ID/keywordPlans/PLAN_ID, as Google answered, always of the account of the " +
		"call). Google answers the resource name only, so there is no response_content_type. Empty after " +
		"validate_only"},
}

var keywordPlansArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

const keywordPlansPeriodDoc = "forecast_period holds exactly one of date_interval (NEXT_WEEK, NEXT_MONTH, or " +
	"NEXT_QUARTER) and date_range (start_date and end_date, both yyyy-mm-dd, inclusive; Google requires both " +
	"dates in the future and a span of at most a year and checks that when a forecast runs)"

var keywordPlansCreate = capability.Descriptor{
	ID: keywordPlansCreateID, Version: 1, Title: "Create keyword plans in a Google Ads account",
	Description: "Create one or more keyword plans in a Google Ads account: a name of 1 to " +
		strconv.Itoa(keywordPlanNameMax) + " characters without control characters and optionally a forecast " +
		"period." + keywordPlansNote + " Offered by a connection with the create effect or whose tools list " +
		"names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, keywordPlansSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plans": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": object(schema{"name": keywordPlanNameSchema, "forecast_period": keywordPlanPeriod}, "name")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plans")),
	OutputSchema: encoded(keywordPlansOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plans", Required: true, Description: "1 to 50 plans, each with name " +
			"(required, 1 to 255 characters, no control characters) and optionally " + keywordPlansPeriodDoc},
		validateOnlyArgument),
	Fields: keywordPlansFields,
	Examples: []capability.Example{{Description: "Create a keyword plan that forecasts the next month",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plans":[{"name":"Spring plan","forecast_period":{"date_interval":"NEXT_MONTH"}}]}`)}},
}

var keywordPlansUpdate = capability.Descriptor{
	ID: keywordPlansUpdateID, Version: 1, Title: "Change keyword plans of a Google Ads account",
	Description: "Change the name or the forecast period of existing keyword plans of a Google Ads account (an " +
		"update with a field mask that names exactly the fields given). Only the keyword_plan_id selects a plan: " +
		"its resource name is built from customer_id and keyword_plan_id. A forecast_period replaces the whole " +
		"period of the plan." + keywordPlansNote + " Offered by a connection with the update effect or whose " +
		"tools list names it; no profile holds it." + mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, keywordPlansSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plans": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"items": object(schema{"keyword_plan_id": keywordPlanIDSchema, "name": keywordPlanNameSchema,
				"forecast_period": keywordPlanPeriod}, "keyword_plan_id")},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plans")),
	OutputSchema: encoded(keywordPlansOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plans", Required: true, Description: "1 to 50 entries, each " +
			"keyword_plan_id once (the id of the plan as reports.search returns it), with at least one of name " +
			"(1 to 255 characters) and " + keywordPlansPeriodDoc},
		validateOnlyArgument),
	Fields: keywordPlansFields,
	Examples: []capability.Example{{Description: "Rename a keyword plan",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plans":[{"keyword_plan_id":"112233","name":"Autumn plan"}]}`)}},
}

var keywordPlansRemove = capability.Descriptor{
	ID: keywordPlansRemoveID, Version: 1, Title: "Remove keyword plans from a Google Ads account",
	Description: "Remove existing keyword plans of a Google Ads account (a remove of " +
		"customers/ID/keywordPlans/PLAN_ID, built from customer_id and keyword_plan_id). A removed plan is gone " +
		"from the account; no tool of this provider brings it back." + keywordPlansNote +
		" Offered by a connection with the delete effect or whose tools list names it; no profile holds it." +
		mutateListNote,
	Tags: []string{"google", "ads", "keyword-plans", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, keywordPlansSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"keyword_plan_ids": schema{"type": "array", "minItems": 1, "maxItems": keywordPlansMaxOperations,
			"uniqueItems": true, "items": keywordPlanIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "keyword_plan_ids")),
	OutputSchema: encoded(keywordPlansOutputSchema),
	Arguments: append(slices.Clone(keywordPlansArgumentDocs),
		capability.Argument{Name: "keyword_plan_ids", Required: true, Description: "1 to 50 IDs of existing " +
			"keyword plans, each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: keywordPlansFields,
	Examples: []capability.Example{{Description: "Validate the removal of a keyword plan without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keyword_plan_ids":["112233"],"validate_only":true}`)}},
}

// --- handlers ---

func handleKeywordPlansCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create keyword plans"
	var args keywordPlansCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlansSend(ctx, resolved, secrets, red, op, keywordPlansCreateID, args.CustomerID, ops, nil,
		args.ValidateOnly)
}

func handleKeywordPlansUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update keyword plans"
	var args keywordPlansUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlansSend(ctx, resolved, secrets, red, op, keywordPlansUpdateID, args.CustomerID, ops, names,
		args.ValidateOnly)
}

func handleKeywordPlansRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove keyword plans"
	var args keywordPlansRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return keywordPlansSend(ctx, resolved, secrets, red, op, keywordPlansRemoveID, args.CustomerID, ops, names,
		args.ValidateOnly)
}

func keywordPlansSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID string, ops []any, want []string, validateOnly bool) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	results, err := c.mutateList(ctx, listMutation{op: op, tool: tool, customerID: customerID,
		service: keywordPlansService, collection: "keywordPlans", operations: ops, names: want,
		validateOnly: validateOnly})
	if err != nil {
		return nil, err
	}
	out, err := keywordPlansResults(op, results)
	if err != nil {
		return nil, err
	}
	return keywordPlansChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: out}, nil
}

// --- confirmation ---

func keywordPlansLead(validateOnly bool) string {
	if validateOnly {
		return "Only validate: "
	}
	return ""
}

func keywordPlansMore(total, shown int) string {
	if total > shown {
		return " and " + strconv.Itoa(total-shown) + " more"
	}
	return ""
}

// keywordPlansQuote shows a name in the confirmation, cut and without control characters.
func keywordPlansQuote(value string) string {
	return strconv.Quote(boundedText(value, keywordPlanNameMax*4))
}

// keywordPlansPeriodText names a forecast period in the confirmation, cut like every text of the arguments.
func keywordPlansPeriodText(p *keywordPlansPeriod) string {
	switch {
	case p.DateInterval != nil:
		return boundedText(*p.DateInterval, maxCodeText)
	case p.DateRange != nil:
		return boundedText(p.DateRange.StartDate, maxCodeText) + " to " + boundedText(p.DateRange.EndDate, maxCodeText)
	}
	return ""
}

func keywordPlansCreateSummary(raw json.RawMessage) string {
	var a keywordPlansCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, p := range a.KeywordPlans[:min(len(a.KeywordPlans), 10)] {
		part := keywordPlansQuote(p.Name)
		if p.ForecastPeriod != nil {
			part += " forecasting " + keywordPlansPeriodText(p.ForecastPeriod)
		}
		parts = append(parts, part)
	}
	return keywordPlansLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.KeywordPlans)) +
		" keyword plan(s) in Google Ads account " + a.CustomerID + ": " + strings.Join(parts, ", ") +
		keywordPlansMore(len(a.KeywordPlans), len(parts)) + "."
}

func keywordPlansUpdateSummary(raw json.RawMessage) string {
	var a keywordPlansUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, p := range a.KeywordPlans[:min(len(a.KeywordPlans), 10)] {
		part := "keyword plan " + boundedText(p.KeywordPlanID, maxCodeText)
		if p.Name != nil {
			part += " name " + keywordPlansQuote(*p.Name)
		}
		if p.ForecastPeriod != nil {
			part += " forecasting " + keywordPlansPeriodText(p.ForecastPeriod)
		}
		parts = append(parts, part)
	}
	return keywordPlansLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.KeywordPlans)) +
		" keyword plan(s) of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		keywordPlansMore(len(a.KeywordPlans), len(parts)) + "."
}

func keywordPlansRemoveSummary(raw json.RawMessage) string {
	var a keywordPlansRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.KeywordPlanIDs[:min(len(a.KeywordPlanIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return keywordPlansLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.KeywordPlanIDs)) +
		" keyword plan(s) from Google Ads account " + a.CustomerID + ": " + strings.Join(shown, ", ") +
		keywordPlansMore(len(a.KeywordPlanIDs), len(shown)) + ". No tool brings them back."
}
