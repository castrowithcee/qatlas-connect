package googleads

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	campaignBudgetsCreateID = Provider + ".campaign-budgets.create"
	campaignBudgetsUpdateID = Provider + ".campaign-budgets.update"
	campaignBudgetsRemoveID = Provider + ".campaign-budgets.remove"

	campaignBudgetsService = "/campaignBudgets"

	// campaignBudgetNameMax is the bound of CampaignBudget.name in v26: 1 to 255 bytes of UTF-8, trimmed.
	campaignBudgetNameMax = 255
	// campaignBudgetsMaxOperations is Qatlas's own bound per call; the reference of MutateCampaignBudgets
	// states none.
	campaignBudgetsMaxOperations = 50
)

// The values of the enums in v26 that a request may name. UNSPECIFIED and UNKNOWN are no values to send.
var (
	campaignBudgetPeriods         = []string{"DAILY", "CUSTOM_PERIOD"}
	campaignBudgetDeliveryMethods = []string{"STANDARD", "ACCELERATED"}
	campaignBudgetTypes           = []string{"STANDARD", "FIXED_CPA", "SMART_CAMPAIGN", "LOCAL_SERVICES"}

	campaignBudgetIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	microsPattern           = regexp.MustCompile(`^[0-9]{1,19}$`)
)

// campaignBudgetName is the resource name of a budget, built from checked IDs alone.
func campaignBudgetName(customerID, budgetID string) string {
	return customerName(customerID) + campaignBudgetsService + "/" + budgetID
}

// campaignBudgetIDOfName returns the budget ID of a resource name that belongs to the account.
func campaignBudgetIDOfName(name, customerID string) (string, bool) {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+campaignBudgetsService+"/")
	if !ok || !campaignBudgetIDPattern.MatchString(rest) {
		return "", false
	}
	if _, err := strconv.ParseInt(rest, 10, 64); err != nil {
		return "", false
	}
	return rest, true
}

func campaignBudgetsCheckID(argument, value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !campaignBudgetIDPattern.MatchString(value) {
		return badRequest(argument + " must be the ID of an existing campaign budget, a positive number")
	}
	return nil
}

func campaignBudgetsCheckCount(argument string, n int) error {
	if n < 1 || n > campaignBudgetsMaxOperations {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(campaignBudgetsMaxOperations) + " entries")
	}
	return nil
}

func campaignBudgetsCheckName(value string) error {
	n := len(strings.TrimSpace(value))
	if n < 1 || n > campaignBudgetNameMax || !utf8.ValidString(value) {
		return badRequest("name must be 1 to 255 bytes of UTF-8 without the blanks around it")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest("name must not hold control characters")
		}
	}
	return nil
}

// campaignBudgetsMicros checks an amount before any request: a whole, positive number of micros that fits an
// int64, in the digits of a JSON integer. Google documents neither a minimum nor a multiple, so the smallest
// unit of the currency is Google's to validate.
func campaignBudgetsMicros(argument string, value *json.Number) (string, error) {
	text := string(*value)
	if !microsPattern.MatchString(text) {
		return "", badRequest(argument + " must be a whole number of micros, such as 5000000 for 5 units of the currency")
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 1 {
		return "", badRequest(argument + " must be a positive number of micros that fits 64 bits")
	}
	return strconv.FormatInt(n, 10), nil
}

func campaignBudgetsCheckEnum(argument, value string, allowed []string) error {
	if !slices.Contains(allowed, value) {
		return badRequest(argument + " must be one of " + strings.Join(allowed, ", "))
	}
	return nil
}

// --- arguments ---

type (
	campaignBudgetsNew struct {
		Name                     *string          `json:"name"`
		AmountMicros             *json.Number     `json:"amount_micros"`
		TotalAmountMicros        *json.Number     `json:"total_amount_micros"`
		Period                   string           `json:"period"`
		DeliveryMethod           string           `json:"delivery_method"`
		ExplicitlyShared         *bool            `json:"explicitly_shared"`
		Type                     string           `json:"type"`
		AlignedBiddingStrategyID string           `json:"aligned_bidding_strategy_id"`
		BudgetNetworkSettings    []networkSetting `json:"budget_network_settings"`
	}
	campaignBudgetsEdit struct {
		BudgetID                 string           `json:"budget_id"`
		Name                     *string          `json:"name"`
		AmountMicros             *json.Number     `json:"amount_micros"`
		TotalAmountMicros        *json.Number     `json:"total_amount_micros"`
		DeliveryMethod           string           `json:"delivery_method"`
		ExplicitlyShared         *bool            `json:"explicitly_shared"`
		AlignedBiddingStrategyID string           `json:"aligned_bidding_strategy_id"`
		BudgetNetworkSettings    []networkSetting `json:"budget_network_settings"`
	}
	campaignBudgetsCreateArguments struct {
		CustomerID          string               `json:"customer_id"`
		CampaignBudgets     []campaignBudgetsNew `json:"campaign_budgets"`
		ValidateOnly        bool                 `json:"validate_only"`
		ResponseContentType string               `json:"response_content_type"`
	}
	campaignBudgetsUpdateArguments struct {
		CustomerID          string                `json:"customer_id"`
		CampaignBudgets     []campaignBudgetsEdit `json:"campaign_budgets"`
		ValidateOnly        bool                  `json:"validate_only"`
		ResponseContentType string                `json:"response_content_type"`
	}
	campaignBudgetsRemoveArguments struct {
		CustomerID   string   `json:"customer_id"`
		BudgetIDs    []string `json:"budget_ids"`
		ValidateOnly bool     `json:"validate_only"`
	}
)

// fields checks the members that create and update share and puts them on the wire form of the budget. The
// mask lists the members in the order of the wire form.
func (b campaignBudgetsEdit) fields() (map[string]any, []string, error) {
	wire := map[string]any{}
	var mask []string
	if b.Name != nil {
		if err := campaignBudgetsCheckName(*b.Name); err != nil {
			return nil, nil, err
		}
		wire["name"] = *b.Name
		mask = append(mask, "name")
	}
	if b.AmountMicros != nil {
		amount, err := campaignBudgetsMicros("amount_micros", b.AmountMicros)
		if err != nil {
			return nil, nil, err
		}
		wire["amountMicros"] = amount
		mask = append(mask, "amountMicros")
	}
	if b.TotalAmountMicros != nil {
		amount, err := campaignBudgetsMicros("total_amount_micros", b.TotalAmountMicros)
		if err != nil {
			return nil, nil, err
		}
		wire["totalAmountMicros"] = amount
		mask = append(mask, "totalAmountMicros")
	}
	if b.DeliveryMethod != "" {
		if err := campaignBudgetsCheckEnum("delivery_method", b.DeliveryMethod, campaignBudgetDeliveryMethods); err != nil {
			return nil, nil, err
		}
		wire["deliveryMethod"] = b.DeliveryMethod
		mask = append(mask, "deliveryMethod")
	}
	if b.ExplicitlyShared != nil {
		wire["explicitlyShared"] = *b.ExplicitlyShared
		mask = append(mask, "explicitlyShared")
	}
	if b.AlignedBiddingStrategyID != "" {
		if err := campaignBudgetsCheckID("aligned_bidding_strategy_id", b.AlignedBiddingStrategyID); err != nil {
			return nil, nil, err
		}
		wire["alignedBiddingStrategyId"] = b.AlignedBiddingStrategyID
		mask = append(mask, "alignedBiddingStrategyId")
	}
	if b.BudgetNetworkSettings != nil {
		settings, err := networkSettingsBody(b.BudgetNetworkSettings)
		if err != nil {
			return nil, nil, err
		}
		wire["budgetNetworkSettings"] = settings
		mask = append(mask, "budgetNetworkSettings")
	}
	if b.AmountMicros != nil && b.TotalAmountMicros != nil {
		return nil, nil, badRequest("amount_micros and total_amount_micros exclude each other")
	}
	return wire, mask, nil
}

func campaignBudgetsCheckHead(customerID, content string) error {
	if !customerIDPattern.MatchString(customerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	return checkContentType(content)
}

// operations checks the arguments and builds the operations of the request.
func (a campaignBudgetsCreateArguments) operations() ([]any, error) {
	if err := campaignBudgetsCheckHead(a.CustomerID, a.ResponseContentType); err != nil {
		return nil, err
	}
	if err := campaignBudgetsCheckCount("campaign_budgets", len(a.CampaignBudgets)); err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(a.CampaignBudgets))
	for _, b := range a.CampaignBudgets {
		// Google names a shared budget by its name and defaults explicitly_shared to true.
		shared := b.ExplicitlyShared == nil || *b.ExplicitlyShared
		if shared && b.Name == nil {
			return nil, badRequest("a campaign budget that is explicitly shared, which is the default, needs a name")
		}
		period := b.Period
		if period == "" {
			period = "DAILY"
		}
		if err := campaignBudgetsCheckEnum("period", period, campaignBudgetPeriods); err != nil {
			return nil, err
		}
		if period == "DAILY" && (b.AmountMicros == nil || b.TotalAmountMicros != nil) {
			return nil, badRequest("a DAILY budget takes amount_micros and no total_amount_micros")
		}
		if period == "CUSTOM_PERIOD" && (b.TotalAmountMicros == nil || b.AmountMicros != nil) {
			return nil, badRequest("a CUSTOM_PERIOD budget takes total_amount_micros and no amount_micros")
		}
		if b.Type != "" {
			if err := campaignBudgetsCheckEnum("type", b.Type, campaignBudgetTypes); err != nil {
				return nil, err
			}
		}
		wire, _, err := campaignBudgetsEdit{Name: b.Name, AmountMicros: b.AmountMicros, TotalAmountMicros: b.TotalAmountMicros,
			DeliveryMethod: b.DeliveryMethod, ExplicitlyShared: b.ExplicitlyShared,
			AlignedBiddingStrategyID: b.AlignedBiddingStrategyID, BudgetNetworkSettings: b.BudgetNetworkSettings}.fields()
		if err != nil {
			return nil, err
		}
		if b.Period != "" {
			wire["period"] = b.Period
		}
		if b.Type != "" {
			wire["type"] = b.Type
		}
		ops = append(ops, map[string]any{"create": wire})
	}
	return ops, nil
}

func (a campaignBudgetsUpdateArguments) operations() ([]any, []string, error) {
	if err := campaignBudgetsCheckHead(a.CustomerID, a.ResponseContentType); err != nil {
		return nil, nil, err
	}
	if err := campaignBudgetsCheckCount("campaign_budgets", len(a.CampaignBudgets)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.CampaignBudgets))
	names := make([]string, 0, len(a.CampaignBudgets))
	for _, b := range a.CampaignBudgets {
		if err := campaignBudgetsCheckID("budget_id", b.BudgetID); err != nil {
			return nil, nil, err
		}
		name := campaignBudgetName(a.CustomerID, b.BudgetID)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("campaign_budgets names each budget_id at most once")
		}
		wire, mask, err := b.fields()
		if err != nil {
			return nil, nil, err
		}
		if len(mask) == 0 {
			return nil, nil, badRequest("each entry of campaign_budgets needs at least one member to change")
		}
		// A shared budget never becomes non-shared, and one that becomes shared needs its name in the same operation.
		if b.ExplicitlyShared != nil && (!*b.ExplicitlyShared || b.Name == nil) {
			return nil, nil, badRequest("explicitly_shared can only be set to true on an update, together with a name")
		}
		wire["resourceName"] = name
		names = append(names, name)
		ops = append(ops, map[string]any{"update": wire, "updateMask": strings.Join(mask, ",")})
	}
	return ops, names, nil
}

func (a campaignBudgetsRemoveArguments) operations() ([]any, []string, error) {
	if err := campaignBudgetsCheckHead(a.CustomerID, ""); err != nil {
		return nil, nil, err
	}
	if err := campaignBudgetsCheckCount("budget_ids", len(a.BudgetIDs)); err != nil {
		return nil, nil, err
	}
	ops := make([]any, 0, len(a.BudgetIDs))
	names := make([]string, 0, len(a.BudgetIDs))
	for _, id := range a.BudgetIDs {
		if err := campaignBudgetsCheckID("budget_ids", id); err != nil {
			return nil, nil, err
		}
		name := campaignBudgetName(a.CustomerID, id)
		if slices.Contains(names, name) {
			return nil, nil, badRequest("budget_ids names each campaign budget at most once")
		}
		names = append(names, name)
		ops = append(ops, map[string]any{"remove": name})
	}
	return ops, names, nil
}

// --- results ---

type (
	campaignBudgetsBudgetResult struct {
		ResourceName                  string                `json:"resource_name"`
		ID                            string                `json:"id,omitempty"`
		Name                          string                `json:"name,omitempty"`
		Status                        string                `json:"status,omitempty"`
		Period                        string                `json:"period,omitempty"`
		Type                          string                `json:"type,omitempty"`
		AmountMicros                  *int64                `json:"amount_micros,omitempty"`
		TotalAmountMicros             *int64                `json:"total_amount_micros,omitempty"`
		DeliveryMethod                string                `json:"delivery_method,omitempty"`
		ExplicitlyShared              *bool                 `json:"explicitly_shared,omitempty"`
		ReferenceCount                *int64                `json:"reference_count,omitempty"`
		AlignedBiddingStrategyID      string                `json:"aligned_bidding_strategy_id,omitempty"`
		HasRecommendedBudget          *bool                 `json:"has_recommended_budget,omitempty"`
		RecommendedBudgetAmountMicros *int64                `json:"recommended_budget_amount_micros,omitempty"`
		BudgetNetworkSettings         []networkSettingEntry `json:"budget_network_settings,omitempty"`
	}
	campaignBudgetsMutated struct {
		BudgetID       string                       `json:"budget_id"`
		ResourceName   string                       `json:"resource_name"`
		CampaignBudget *campaignBudgetsBudgetResult `json:"campaign_budget,omitempty"`
	}
	campaignBudgetsChanged struct {
		CustomerID   string                   `json:"customer_id"`
		ValidateOnly bool                     `json:"validate_only"`
		Results      []campaignBudgetsMutated `json:"results"`
	}
	campaignBudgetsAnswer struct {
		Results []struct {
			ResourceName   string          `json:"resourceName"`
			CampaignBudget json.RawMessage `json:"campaignBudget"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
	campaignBudgetsWire struct {
		ResourceName                  string              `json:"resourceName"`
		ID                            *wireInt            `json:"id"`
		Name                          string              `json:"name"`
		Status                        string              `json:"status"`
		Period                        string              `json:"period"`
		Type                          string              `json:"type"`
		AmountMicros                  *wireInt            `json:"amountMicros"`
		TotalAmountMicros             *wireInt            `json:"totalAmountMicros"`
		DeliveryMethod                string              `json:"deliveryMethod"`
		ExplicitlyShared              *bool               `json:"explicitlyShared"`
		ReferenceCount                *wireInt            `json:"referenceCount"`
		AlignedBiddingStrategyID      *wireInt            `json:"alignedBiddingStrategyId"`
		HasRecommendedBudget          *bool               `json:"hasRecommendedBudget"`
		RecommendedBudgetAmountMicros *wireInt            `json:"recommendedBudgetAmountMicros"`
		BudgetNetworkSettings         networkSettingsWire `json:"budgetNetworkSettings"`
	}
)

func campaignBudgetsNumber(value *wireInt) (*int64, bool) {
	if value == nil {
		return nil, true
	}
	n, err := strconv.ParseInt(string(*value), 10, 64)
	return &n, err == nil
}

func campaignBudgetsBudgetOf(raw json.RawMessage, name, budgetID string) (*campaignBudgetsBudgetResult, bool) {
	var w campaignBudgetsWire
	if json.Unmarshal(raw, &w) != nil || w.ResourceName != name {
		return nil, false
	}
	out := &campaignBudgetsBudgetResult{ResourceName: name, Name: boundedText(w.Name, maxText), Status: code(w.Status),
		Period: code(w.Period), Type: code(w.Type), DeliveryMethod: code(w.DeliveryMethod),
		ExplicitlyShared: w.ExplicitlyShared, HasRecommendedBudget: w.HasRecommendedBudget}
	if w.ID != nil {
		if string(*w.ID) != budgetID {
			return nil, false
		}
		out.ID = budgetID
	}
	var ok bool
	for _, n := range []struct {
		from *wireInt
		to   **int64
	}{{w.AmountMicros, &out.AmountMicros}, {w.TotalAmountMicros, &out.TotalAmountMicros},
		{w.ReferenceCount, &out.ReferenceCount}, {w.RecommendedBudgetAmountMicros, &out.RecommendedBudgetAmountMicros}} {
		if *n.to, ok = campaignBudgetsNumber(n.from); !ok {
			return nil, false
		}
	}
	if w.AlignedBiddingStrategyID != nil {
		out.AlignedBiddingStrategyID = code(string(*w.AlignedBiddingStrategyID))
	}
	if len(w.BudgetNetworkSettings.Settings) > 0 {
		if out.BudgetNetworkSettings, ok = networkEntriesOf(w.BudgetNetworkSettings); !ok {
			return nil, false
		}
	}
	return out, true
}

// campaignBudgetsResults binds the answer to the checked account. With want names (update, remove) the results
// must be exactly these in order; a create names the new budgets, which must belong to the account and be
// distinct. Everything else leaves the outcome open, since the request was sent.
func campaignBudgetsResults(op, customerID string, answer campaignBudgetsAnswer, count int, want []string,
	validateOnly bool) ([]campaignBudgetsMutated, error) {
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(op, "answered with a partial failure although the request asked for none")
	}
	out := []campaignBudgetsMutated{}
	if validateOnly && len(answer.Results) == 0 {
		return out, nil
	}
	if len(answer.Results) != count {
		return nil, unsureResult(op, "answered with another number of results than the operations asked for")
	}
	for i, r := range answer.Results {
		id, ok := campaignBudgetIDOfName(r.ResourceName, customerID)
		if want != nil && r.ResourceName != want[i] || !ok {
			return nil, unsureResult(op, "answered with a result for another campaign budget than the one asked for")
		}
		for _, previous := range out {
			if previous.BudgetID == id {
				return nil, unsureResult(op, "answered with the same campaign budget twice")
			}
		}
		item := campaignBudgetsMutated{BudgetID: id, ResourceName: r.ResourceName}
		if len(r.CampaignBudget) > 0 && string(r.CampaignBudget) != "null" {
			budget, ok := campaignBudgetsBudgetOf(r.CampaignBudget, r.ResourceName, id)
			if !ok {
				return nil, unsureResult(op, "answered with a campaign budget that is not the one asked for")
			}
			item.CampaignBudget = budget
		}
		out = append(out, item)
	}
	return out, nil
}

// --- descriptors ---

var (
	campaignBudgetNameSchema   = schema{"type": "string", "minLength": 1, "maxLength": campaignBudgetNameMax}
	campaignBudgetIDSchema     = schema{"type": "string", "pattern": campaignBudgetIDPattern.String()}
	campaignBudgetMicrosSchema = schema{"type": "integer", "minimum": 1}

	campaignBudgetsResultSchema = object(schema{"resource_name": stringSchema, "id": stringSchema, "name": stringSchema,
		"status": stringSchema, "period": stringSchema, "type": stringSchema, "amount_micros": integerSchema,
		"total_amount_micros": integerSchema, "delivery_method": stringSchema, "explicitly_shared": booleanSchema,
		"reference_count": integerSchema, "aligned_bidding_strategy_id": stringSchema,
		"has_recommended_budget": booleanSchema, "recommended_budget_amount_micros": integerSchema,
		"budget_network_settings": networkEntriesSchema}, "resource_name")
	campaignBudgetsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"results": arrayOf(object(schema{"budget_id": stringSchema, "resource_name": stringSchema,
			"campaign_budget": campaignBudgetsResultSchema}, "budget_id", "resource_name"))},
		"customer_id", "validate_only", "results")

	campaignBudgetNetworkSettingsSchema = schema{"type": "array", "minItems": 1, "maxItems": len(advertisingChannelTypes),
		"items": settingSchema}
)

const campaignBudgetsSensitivity = "campaign budgets of a Google Ads account: their names, amounts, delivery, and the " +
	"settings that decide how much campaigns may spend; no data of other people"

const campaignBudgetsNote = " A campaign budget is the amount a campaign, or several campaigns that share it, may " +
	"spend; the call is confirmed per call. The request goes to CampaignBudgetService.MutateCampaignBudgets with " +
	"up to 50 operations, atomic and without partial_failure: Google applies all of them or none. Amounts are in " +
	"micros of the currency of the account (1,000,000 micros are one unit); Google validates the smallest unit " +
	"the currency allows. This tool does not touch account budgets, which are the spending limits of a billing " +
	"setup and are only read (account-budgets.list), and it does not read campaign budgets (read them with " +
	"reports.search on campaign_budget)."

var campaignBudgetsFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "results", Description: "Per operation, in the order of the arguments: budget_id, resource_name " +
		"(customers/ID/campaignBudgets/BUDGET_ID, as Google answered, always of the account of the call), and with " +
		"MUTABLE_RESOURCE the budget after the change as Google returns it, which holds the mutable members: name, " +
		"period, type, amount_micros, total_amount_micros, delivery_method, explicitly_shared, " +
		"aligned_bidding_strategy_id, budget_network_settings, and when Google adds them id, status, " +
		"reference_count, has_recommended_budget, recommended_budget_amount_micros. Empty after validate_only"},
}

var campaignBudgetsArgumentDocs = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager " +
		"target that names it"},
}

const campaignBudgetsMicrosText = "a positive whole number of micros"

var campaignBudgetsCreate = capability.Descriptor{
	ID: campaignBudgetsCreateID, Version: 1, Title: "Create campaign budgets in a Google Ads account",
	Description: "Create one or more campaign budgets in a Google Ads account. A DAILY budget (the default period) " +
		"takes amount_micros, the average daily amount; a CUSTOM_PERIOD budget takes total_amount_micros, the " +
		"amount over its whole duration, and no amount_micros. A budget is explicitly shared unless " +
		"explicitly_shared is false, and a shared budget needs a name of 1 to 255 bytes. delivery_method is " +
		"STANDARD (the default) or ACCELERATED. period and type cannot be changed afterwards." +
		campaignBudgetsNote + mutateListListedNote,
	Tags: []string{"google", "ads", "budgets", "campaigns", "create"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, campaignBudgetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_budgets": schema{"type": "array", "minItems": 1, "maxItems": campaignBudgetsMaxOperations,
			"items": object(schema{"name": campaignBudgetNameSchema, "amount_micros": campaignBudgetMicrosSchema,
				"total_amount_micros":         campaignBudgetMicrosSchema,
				"period":                      schema{"type": "string", "enum": campaignBudgetPeriods},
				"delivery_method":             schema{"type": "string", "enum": campaignBudgetDeliveryMethods},
				"explicitly_shared":           booleanSchema,
				"type":                        schema{"type": "string", "enum": campaignBudgetTypes},
				"aligned_bidding_strategy_id": campaignBudgetIDSchema,
				"budget_network_settings":     campaignBudgetNetworkSettingsSchema})},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "campaign_budgets")),
	OutputSchema: encoded(campaignBudgetsOutputSchema),
	Arguments: append(slices.Clone(campaignBudgetsArgumentDocs),
		capability.Argument{Name: "campaign_budgets", Required: true, Description: "1 to 50 budgets, each with: name " +
			"(1 to 255 bytes of UTF-8, required unless explicitly_shared is false, no control characters), " +
			"amount_micros (" + campaignBudgetsMicrosText + ", for DAILY) or total_amount_micros (" +
			campaignBudgetsMicrosText + ", for CUSTOM_PERIOD), period (DAILY or CUSTOM_PERIOD), delivery_method " +
			"(STANDARD or ACCELERATED), explicitly_shared, type (STANDARD, FIXED_CPA, SMART_CAMPAIGN or " +
			"LOCAL_SERVICES; the last three only for the campaigns Google names for them), " +
			"aligned_bidding_strategy_id (the ID of the portfolio bidding strategy a shared budget is aligned " +
			"with) and budget_network_settings (as customer-budget-network-settings.create names them: one entry " +
			"per advertising channel type with budget_caps and channel_priorities)"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: campaignBudgetsFields,
	Examples: []capability.Example{{Description: "Create a shared daily budget of 5 units of the currency",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_budgets":[{"name":"Spring sale","amount_micros":5000000}]}`)}},
}

var campaignBudgetsUpdate = capability.Descriptor{
	ID: campaignBudgetsUpdateID, Version: 1, Title: "Change campaign budgets of a Google Ads account",
	Description: "Change existing campaign budgets of a Google Ads account (an update with a field mask that names " +
		"exactly the members given): name, amount_micros or total_amount_micros (not both), delivery_method, " +
		"aligned_bidding_strategy_id, and budget_network_settings, which replaces the whole message. Only " +
		"budget_id selects a budget: its resource name is built from customer_id and budget_id. A shared " +
		"budget never becomes non-shared; explicitly_shared can be set to true on an update, with a name, to share " +
		"a budget of one campaign. Raising an amount raises what the campaigns using the budget may spend. " +
		"The period and the type cannot be changed." + campaignBudgetsNote + mutateListListedNote,
	Tags: []string{"google", "ads", "budgets", "campaigns", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, campaignBudgetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"campaign_budgets": schema{"type": "array", "minItems": 1, "maxItems": campaignBudgetsMaxOperations,
			"items": object(schema{"budget_id": campaignBudgetIDSchema, "name": campaignBudgetNameSchema,
				"amount_micros": campaignBudgetMicrosSchema, "total_amount_micros": campaignBudgetMicrosSchema,
				"delivery_method":             schema{"type": "string", "enum": campaignBudgetDeliveryMethods},
				"explicitly_shared":           schema{"type": "boolean", "enum": []bool{true}},
				"aligned_bidding_strategy_id": campaignBudgetIDSchema,
				"budget_network_settings":     campaignBudgetNetworkSettingsSchema}, "budget_id")},
		"validate_only": validateOnlySchema, "response_content_type": responseContentTypeSchema,
	}, "customer_id", "campaign_budgets")),
	OutputSchema: encoded(campaignBudgetsOutputSchema),
	Arguments: append(slices.Clone(campaignBudgetsArgumentDocs),
		capability.Argument{Name: "campaign_budgets", Required: true, Description: "1 to 50 entries, each budget_id once, " +
			"with at least one of name (1 to 255 bytes of UTF-8), amount_micros or total_amount_micros (" +
			campaignBudgetsMicrosText + ", not both), delivery_method (STANDARD or ACCELERATED), explicitly_shared " +
			"(only true, with a name), aligned_bidding_strategy_id and budget_network_settings. budget_id is the id " +
			"of the budget as reports.search returns it"},
		validateOnlyArgument, responseContentTypeArgument),
	Fields: campaignBudgetsFields,
	Examples: []capability.Example{{Description: "Raise a daily budget to 7.5 units of the currency",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","campaign_budgets":[{"budget_id":"112233","amount_micros":7500000}]}`)}},
}

var campaignBudgetsRemove = capability.Descriptor{
	ID: campaignBudgetsRemoveID, Version: 1, Title: "Remove campaign budgets from a Google Ads account",
	Description: "Remove existing campaign budgets of a Google Ads account (a remove of " +
		"customers/ID/campaignBudgets/BUDGET_ID, built from customer_id and budget_id). Google marks a removed " +
		"budget REMOVED; no tool of this provider brings it back. Whether Google refuses to remove a budget that " +
		"campaigns still use is Google's to decide; validate_only shows it." + campaignBudgetsNote + mutateListListedNote,
	Tags: []string{"google", "ads", "budgets", "campaigns", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, campaignBudgetsSensitivity),
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"budget_ids": schema{"type": "array", "minItems": 1, "maxItems": campaignBudgetsMaxOperations,
			"uniqueItems": true, "items": campaignBudgetIDSchema},
		"validate_only": validateOnlySchema,
	}, "customer_id", "budget_ids")),
	OutputSchema: encoded(campaignBudgetsOutputSchema),
	Arguments: append(slices.Clone(campaignBudgetsArgumentDocs),
		capability.Argument{Name: "budget_ids", Required: true, Description: "1 to 50 IDs of existing campaign budgets, " +
			"each once, as reports.search returns them"},
		validateOnlyArgument),
	Fields: campaignBudgetsFields,
	Examples: []capability.Example{{Description: "Validate the removal of a budget without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","budget_ids":["112233"],"validate_only":true}`)}},
}

// --- handlers ---

func handleCampaignBudgetsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create campaign budgets"
	var args campaignBudgetsCreateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignBudgetsSend(ctx, resolved, secrets, red, op, campaignBudgetsCreateID, args.CustomerID, ops, nil,
		args.ValidateOnly, args.ResponseContentType)
}

func handleCampaignBudgetsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update campaign budgets"
	var args campaignBudgetsUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignBudgetsSend(ctx, resolved, secrets, red, op, campaignBudgetsUpdateID, args.CustomerID, ops, names,
		args.ValidateOnly, args.ResponseContentType)
}

func handleCampaignBudgetsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove campaign budgets"
	var args campaignBudgetsRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	ops, names, err := args.operations()
	if err != nil {
		return nil, err
	}
	return campaignBudgetsSend(ctx, resolved, secrets, red, op, campaignBudgetsRemoveID, args.CustomerID, ops, names,
		args.ValidateOnly, "")
}

func campaignBudgetsSend(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	op, tool, customerID string, ops []any, want []string, validateOnly bool, content string) (any, error) {
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	var answer campaignBudgetsAnswer
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: customerID, service: campaignBudgetsService,
		body: mutateOperationsBody(ops, validateOnly, content)}, &answer); err != nil {
		return nil, err
	}
	results, err := campaignBudgetsResults(op, customerID, answer, len(ops), want, validateOnly)
	if err != nil {
		return nil, err
	}
	return campaignBudgetsChanged{CustomerID: customerID, ValidateOnly: validateOnly, Results: results}, nil
}

// --- confirmation ---

// campaignBudgetsQuote shows a text in the confirmation, cut and without control characters.
func campaignBudgetsQuote(value string) string {
	return strconv.Quote(boundedText(value, campaignBudgetNameMax*2))
}

func campaignBudgetsMoney(argument string, value *json.Number) string {
	if value == nil {
		return ""
	}
	return " " + argument + " " + boundedText(string(*value), maxCodeText)
}

func campaignBudgetsMore(total, shown int) string {
	if total > shown {
		return " and " + strconv.Itoa(total-shown) + " more"
	}
	return ""
}

func campaignBudgetsLead(validateOnly bool) string {
	if validateOnly {
		return "Only validate: "
	}
	return ""
}

const campaignBudgetsSpendWarning = " The budgets decide how much the campaigns that use them may spend."

func campaignBudgetsCreateSummary(raw json.RawMessage) string {
	var a campaignBudgetsCreateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, b := range a.CampaignBudgets[:min(len(a.CampaignBudgets), 10)] {
		part := "unnamed"
		if b.Name != nil {
			part = campaignBudgetsQuote(*b.Name)
		}
		part += campaignBudgetsMoney("amount_micros", b.AmountMicros) +
			campaignBudgetsMoney("total_amount_micros", b.TotalAmountMicros)
		if b.Period != "" {
			part += " " + boundedText(b.Period, maxCodeText)
		}
		parts = append(parts, part)
	}
	return campaignBudgetsLead(a.ValidateOnly) + "Create " + strconv.Itoa(len(a.CampaignBudgets)) +
		" campaign budget(s) in Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		campaignBudgetsMore(len(a.CampaignBudgets), len(parts)) + "." + campaignBudgetsSpendWarning
}

func campaignBudgetsUpdateSummary(raw json.RawMessage) string {
	var a campaignBudgetsUpdateArguments
	_ = json.Unmarshal(raw, &a)
	var parts []string
	for _, b := range a.CampaignBudgets[:min(len(a.CampaignBudgets), 10)] {
		part := "budget " + boundedText(b.BudgetID, maxCodeText)
		if b.Name != nil {
			part += " name " + campaignBudgetsQuote(*b.Name)
		}
		part += campaignBudgetsMoney("amount_micros", b.AmountMicros) +
			campaignBudgetsMoney("total_amount_micros", b.TotalAmountMicros)
		if b.DeliveryMethod != "" {
			part += " delivery_method " + boundedText(b.DeliveryMethod, maxCodeText)
		}
		if b.ExplicitlyShared != nil {
			part += " explicitly_shared"
		}
		if b.AlignedBiddingStrategyID != "" {
			part += " aligned_bidding_strategy_id " + boundedText(b.AlignedBiddingStrategyID, maxCodeText)
		}
		if b.BudgetNetworkSettings != nil {
			part += " budget_network_settings"
		}
		parts = append(parts, part)
	}
	return campaignBudgetsLead(a.ValidateOnly) + "Change " + strconv.Itoa(len(a.CampaignBudgets)) +
		" campaign budget(s) of Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") +
		campaignBudgetsMore(len(a.CampaignBudgets), len(parts)) + "." + campaignBudgetsSpendWarning
}

func campaignBudgetsRemoveSummary(raw json.RawMessage) string {
	var a campaignBudgetsRemoveArguments
	_ = json.Unmarshal(raw, &a)
	shown := make([]string, 0, 10)
	for _, id := range a.BudgetIDs[:min(len(a.BudgetIDs), 10)] {
		shown = append(shown, boundedText(id, maxCodeText))
	}
	return campaignBudgetsLead(a.ValidateOnly) + "Remove " + strconv.Itoa(len(a.BudgetIDs)) +
		" campaign budget(s) from Google Ads account " + a.CustomerID + ": " + strings.Join(shown, ", ") +
		campaignBudgetsMore(len(a.BudgetIDs), len(shown)) + ". Google marks them removed; no tool brings them back."
}
