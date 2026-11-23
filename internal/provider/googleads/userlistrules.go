package googleads

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// The type-specific parts of a user list: what a request may carry and how it is checked and built. Customer
// Match lists can only be created as an empty shell: no tool uploads members. URL values in rules are data
// that Google evaluates; Qatlas Connect never fetches them.

// Qatlas's own bounds of the structure of a user list; Google documents none of them.
const (
	userListMaxRuleGroups      = 20
	userListMaxRuleItems       = 20
	userListMaxOperands        = 20
	userListMaxLogicalRules    = 10
	userListMaxLogicalOperands = 20
	userListMaxActions         = 20
	userListMaxSeedLists       = 20
	userListMaxCountryCodes    = 250
	userListMaxRuleText        = 2048
	userListMaxRuleName        = 128
	userListMaxAppID           = 256
)

// The values of the enums in v26 that a request may name. UNSPECIFIED and UNKNOWN are no values to send.
var (
	userListRuleTypes           = []string{"OR_OF_ANDS", "AND_OF_ORS"}
	userListNumberOperators     = []string{"EQUALS", "NOT_EQUALS", "GREATER_THAN", "GREATER_THAN_OR_EQUAL", "LESS_THAN", "LESS_THAN_OR_EQUAL"}
	userListStringOperators     = []string{"CONTAINS", "NOT_CONTAINS", "STARTS_WITH", "NOT_STARTS_WITH", "ENDS_WITH", "NOT_ENDS_WITH", "EQUALS", "NOT_EQUALS"}
	userListDateOperators       = []string{"AFTER", "BEFORE", "EQUALS", "NOT_EQUALS"}
	userListLogicalOperators    = []string{"ALL", "ANY", "NONE"}
	userListFlexibleOperators   = []string{"AND", "OR"}
	userListPrepopulationValues = []string{"REQUESTED"}
	userListUploadKeyTypes      = []string{"CONTACT_INFO", "CRM_ID", "MOBILE_ADVERTISING_ID"}
	userListCrmDataSources      = []string{"FIRST_PARTY", "THIRD_PARTY_CREDIT_BUREAU", "THIRD_PARTY_VOTER_FILE", "THIRD_PARTY_PARTNER_DATA"}
	userListExpansionLevels     = []string{"NARROW", "BALANCED", "BROAD"}
	userListMembershipStatuses  = []string{"OPEN", "CLOSED"}
	userListAccessStatuses      = []string{"ENABLED", "DISABLED"}
	userListClosingReasons      = []string{"UNUSED"}
)

var (
	userListRuleNamePattern = regexp.MustCompile(`^[A-Za-z_\x{80}-\x{10FFFF}][^\x00-\x1f\x7f]*$`)
	userListRuleDatePattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$`)
	userListCountryPattern  = regexp.MustCompile(`^[A-Z]{2}$`)
	// listIDPattern is an int64 of the API in decimal form: positive, written as a string.
	userListListIDPattern = userIDPattern
)

type (
	userListNumberRule struct {
		Operator string   `json:"operator"`
		Value    *float64 `json:"value"`
	}
	userListStringRule struct {
		Operator string  `json:"operator"`
		Value    *string `json:"value"`
	}
	userListDateRule struct {
		Operator     string  `json:"operator"`
		Value        *string `json:"value"`
		OffsetInDays *int64  `json:"offset_in_days"`
	}
	userListRuleItem struct {
		Name           string              `json:"name"`
		NumberRuleItem *userListNumberRule `json:"number_rule_item"`
		StringRuleItem *userListStringRule `json:"string_rule_item"`
		DateRuleItem   *userListDateRule   `json:"date_rule_item"`
	}
	userListRuleItemGroup struct {
		RuleItems []userListRuleItem `json:"rule_items"`
	}
	userListRuleInfo struct {
		RuleType       string                  `json:"rule_type"`
		RuleItemGroups []userListRuleItemGroup `json:"rule_item_groups"`
	}
	userListRuleOperand struct {
		Rule               *userListRuleInfo `json:"rule"`
		LookbackWindowDays *int64            `json:"lookback_window_days"`
	}
	userListFlexibleRule struct {
		InclusiveRuleOperator string                `json:"inclusive_rule_operator"`
		InclusiveOperands     []userListRuleOperand `json:"inclusive_operands"`
		ExclusiveOperands     []userListRuleOperand `json:"exclusive_operands"`
	}
	userListRuleBasedList struct {
		PrepopulationStatus  string                `json:"prepopulation_status"`
		FlexibleRuleUserList *userListFlexibleRule `json:"flexible_rule_user_list"`
	}
	userListLogicalOperand struct {
		UserList string `json:"user_list"`
	}
	userListLogicalRule struct {
		Operator     string                   `json:"operator"`
		RuleOperands []userListLogicalOperand `json:"rule_operands"`
	}
	userListLogicalList struct {
		Rules []userListLogicalRule `json:"rules"`
	}
	userListListAction struct {
		ConversionAction  string `json:"conversion_action"`
		RemarketingAction string `json:"remarketing_action"`
	}
	userListBasicList struct {
		Actions []userListListAction `json:"actions"`
	}
	userListCrmList struct {
		AppID          string `json:"app_id"`
		UploadKeyType  string `json:"upload_key_type"`
		DataSourceType string `json:"data_source_type"`
	}
	userListLookalikeList struct {
		SeedUserListIDs []string `json:"seed_user_list_ids"`
		ExpansionLevel  string   `json:"expansion_level"`
		CountryCodes    []string `json:"country_codes"`
	}
)

// plainText checks a text of 1 to limit bytes of valid UTF-8 that is not blank and holds no control character.
func userListPlainText(argument, value string, limit int) error {
	if strings.TrimSpace(value) == "" || len(value) > limit || !utf8.ValidString(value) ||
		strings.ContainsFunc(value, unicode.IsControl) {
		return badRequest(argument + " must be text of 1 to " + strconv.Itoa(limit) + " bytes without control characters")
	}
	return nil
}

func (r userListNumberRule) wire() (map[string]any, error) {
	if !slices.Contains(userListNumberOperators, r.Operator) || r.Value == nil {
		return nil, badRequest("number_rule_item needs an operator of the API and a value")
	}
	return map[string]any{"operator": r.Operator, "value": *r.Value}, nil
}

// wire checks a string rule. For the built-in URL variables Google refuses newlines, quotes, tabs and
// parentheses in the value, which is checked here so that the request is not sent for nothing.
func (r userListStringRule) wire(name string) (map[string]any, error) {
	if !slices.Contains(userListStringOperators, r.Operator) || r.Value == nil {
		return nil, badRequest("string_rule_item needs an operator of the API and a value")
	}
	if err := userListPlainText("string_rule_item value", *r.Value, userListMaxRuleText); err != nil {
		return nil, err
	}
	if (name == "url__" || name == "ref_url__") && strings.ContainsAny(*r.Value, "\"'()") {
		return nil, badRequest("the value of a rule on url__ or ref_url__ must not hold quotes or parentheses")
	}
	return map[string]any{"operator": r.Operator, "value": *r.Value}, nil
}

func (r userListDateRule) wire() (map[string]any, error) {
	if !slices.Contains(userListDateOperators, r.Operator) || (r.Value == nil) == (r.OffsetInDays == nil) {
		return nil, badRequest("date_rule_item needs an operator of the API and exactly one of value and offset_in_days")
	}
	out := map[string]any{"operator": r.Operator}
	if r.Value != nil {
		if !userListRuleDatePattern.MatchString(*r.Value) {
			return nil, badRequest("the value of date_rule_item must be a date as YYYY-MM-DD")
		}
		out["value"] = *r.Value
	} else {
		out["offsetInDays"] = *r.OffsetInDays
	}
	return out, nil
}

func (i userListRuleItem) wire() (map[string]any, error) {
	if !userListRuleNamePattern.MatchString(i.Name) || len(i.Name) > userListMaxRuleName || !utf8.ValidString(i.Name) {
		return nil, badRequest("a rule item needs a name that starts with a letter, an underscore or a non-ASCII " +
			"character, up to 128 bytes")
	}
	kinds := 0
	out := map[string]any{"name": i.Name}
	if i.NumberRuleItem != nil {
		kinds++
		v, err := i.NumberRuleItem.wire()
		if err != nil {
			return nil, err
		}
		out["numberRuleItem"] = v
	}
	if i.StringRuleItem != nil {
		kinds++
		v, err := i.StringRuleItem.wire(i.Name)
		if err != nil {
			return nil, err
		}
		out["stringRuleItem"] = v
	}
	if i.DateRuleItem != nil {
		kinds++
		v, err := i.DateRuleItem.wire()
		if err != nil {
			return nil, err
		}
		out["dateRuleItem"] = v
	}
	if kinds != 1 {
		return nil, badRequest("a rule item needs exactly one of number_rule_item, string_rule_item and date_rule_item")
	}
	return out, nil
}

// wire checks a rule. A flexible rule list supports OR of ANDs only, which flexible says.
func (r userListRuleInfo) wire(flexible bool) (map[string]any, error) {
	if r.RuleType != "" && !slices.Contains(userListRuleTypes, r.RuleType) || flexible && r.RuleType == "AND_OF_ORS" {
		return nil, badRequest("rule_type must be OR_OF_ANDS (AND_OF_ORS is not supported in a rule based list)")
	}
	if n := len(r.RuleItemGroups); n == 0 || n > userListMaxRuleGroups {
		return nil, badRequest("rule_item_groups must hold 1 to 20 groups")
	}
	groups := []any{}
	for _, g := range r.RuleItemGroups {
		if n := len(g.RuleItems); n == 0 || n > userListMaxRuleItems {
			return nil, badRequest("a rule item group must hold 1 to 20 rule items")
		}
		items := []any{}
		for _, item := range g.RuleItems {
			v, err := item.wire()
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		groups = append(groups, map[string]any{"ruleItems": items})
	}
	out := map[string]any{"ruleItemGroups": groups}
	if r.RuleType != "" {
		out["ruleType"] = r.RuleType
	}
	return out, nil
}

func userListOperandsWire(argument string, operands []userListRuleOperand, atLeastOne bool) ([]any, error) {
	if len(operands) > userListMaxOperands || atLeastOne && len(operands) == 0 {
		return nil, badRequest(argument + " must hold at most 20 operands, and inclusive_operands at least one")
	}
	out := []any{}
	for _, o := range operands {
		if o.Rule == nil {
			return nil, badRequest("every operand of " + argument + " needs a rule")
		}
		rule, err := o.Rule.wire(true)
		if err != nil {
			return nil, err
		}
		item := map[string]any{"rule": rule}
		if o.LookbackWindowDays != nil {
			if *o.LookbackWindowDays < 0 {
				return nil, badRequest("lookback_window_days must not be negative")
			}
			item["lookbackWindowDays"] = *o.LookbackWindowDays
		}
		out = append(out, item)
	}
	return out, nil
}

func (b userListRuleBasedList) wire() (map[string]any, error) {
	f := b.FlexibleRuleUserList
	if f == nil || !slices.Contains(userListFlexibleOperators, f.InclusiveRuleOperator) {
		return nil, badRequest("rule_based_user_list needs flexible_rule_user_list with an inclusive_rule_operator " +
			"AND or OR")
	}
	if b.PrepopulationStatus != "" && !slices.Contains(userListPrepopulationValues, b.PrepopulationStatus) {
		return nil, badRequest("prepopulation_status may only be REQUESTED; leave it out for none")
	}
	inclusive, err := userListOperandsWire("inclusive_operands", f.InclusiveOperands, true)
	if err != nil {
		return nil, err
	}
	exclusive, err := userListOperandsWire("exclusive_operands", f.ExclusiveOperands, false)
	if err != nil {
		return nil, err
	}
	flex := map[string]any{"inclusiveRuleOperator": f.InclusiveRuleOperator, "inclusiveOperands": inclusive}
	if len(exclusive) > 0 {
		flex["exclusiveOperands"] = exclusive
	}
	out := map[string]any{"flexibleRuleUserList": flex}
	if b.PrepopulationStatus != "" {
		out["prepopulationStatus"] = b.PrepopulationStatus
	}
	return out, nil
}

// wire checks a logical list. An operand is a user list of the account of the call: Google documents no
// operand from another account, so none is accepted.
func (l userListLogicalList) wire(customerID string) (map[string]any, error) {
	if n := len(l.Rules); n == 0 || n > userListMaxLogicalRules {
		return nil, badRequest("logical_user_list must hold 1 to 10 rules")
	}
	rules := []any{}
	for _, r := range l.Rules {
		if !slices.Contains(userListLogicalOperators, r.Operator) {
			return nil, badRequest("the operator of a logical rule must be ALL, ANY or NONE")
		}
		if n := len(r.RuleOperands); n == 0 || n > userListMaxLogicalOperands {
			return nil, badRequest("a logical rule must hold 1 to 20 rule_operands")
		}
		operands := []any{}
		for _, o := range r.RuleOperands {
			if !ownedName(o.UserList, customerID, "userLists") {
				return nil, badRequest("every operand must be a user list of customer_id, such as customers/" +
					queryNumber(customerID) + "/userLists/123")
			}
			operands = append(operands, map[string]any{"userList": o.UserList})
		}
		rules = append(rules, map[string]any{"operator": r.Operator, "ruleOperands": operands})
	}
	return map[string]any{"rules": rules}, nil
}

// wire checks a basic list: every action must be an action of the account of the call.
func (b userListBasicList) wire(customerID string) (map[string]any, error) {
	if n := len(b.Actions); n == 0 || n > userListMaxActions {
		return nil, badRequest("basic_user_list must hold 1 to 20 actions")
	}
	actions := []any{}
	for _, a := range b.Actions {
		if (a.ConversionAction == "") == (a.RemarketingAction == "") {
			return nil, badRequest("an action names exactly one of conversion_action and remarketing_action")
		}
		if a.ConversionAction != "" {
			if !ownedName(a.ConversionAction, customerID, "conversionActions") {
				return nil, badRequest("conversion_action must be a conversion action of customer_id, such as " +
					"customers/" + queryNumber(customerID) + "/conversionActions/123")
			}
			actions = append(actions, map[string]any{"conversionAction": a.ConversionAction})
			continue
		}
		if !ownedName(a.RemarketingAction, customerID, "remarketingActions") {
			return nil, badRequest("remarketing_action must be a remarketing action of customer_id, such as " +
				"customers/" + queryNumber(customerID) + "/remarketingActions/123")
		}
		actions = append(actions, map[string]any{"remarketingAction": a.RemarketingAction})
	}
	return map[string]any{"actions": actions}, nil
}

// wire checks the shell of a CRM based list. It carries no member: no tool uploads any.
func (c userListCrmList) wire() (map[string]any, error) {
	out := map[string]any{}
	if c.UploadKeyType != "" {
		if !slices.Contains(userListUploadKeyTypes, c.UploadKeyType) {
			return nil, badRequest("upload_key_type must be CONTACT_INFO, CRM_ID or MOBILE_ADVERTISING_ID")
		}
		out["uploadKeyType"] = c.UploadKeyType
	}
	if c.DataSourceType != "" {
		if !slices.Contains(userListCrmDataSources, c.DataSourceType) {
			return nil, badRequest("data_source_type must be FIRST_PARTY or one of the THIRD_PARTY values")
		}
		out["dataSourceType"] = c.DataSourceType
	}
	if c.AppID != "" {
		if err := userListPlainText("app_id", c.AppID, userListMaxAppID); err != nil {
			return nil, err
		}
		out["appId"] = c.AppID
	}
	return out, nil
}

func (l userListLookalikeList) wire() (map[string]any, error) {
	if n := len(l.SeedUserListIDs); n == 0 || n > userListMaxSeedLists {
		return nil, badRequest("seed_user_list_ids must hold 1 to 20 user list IDs")
	}
	for i, id := range l.SeedUserListIDs {
		if err := userListCheckID("seed_user_list_ids", id); err != nil || slices.Contains(l.SeedUserListIDs[:i], id) {
			return nil, badRequest("seed_user_list_ids must hold distinct user list IDs of the account")
		}
	}
	if !slices.Contains(userListExpansionLevels, l.ExpansionLevel) {
		return nil, badRequest("expansion_level must be NARROW, BALANCED or BROAD")
	}
	if n := len(l.CountryCodes); n == 0 || n > userListMaxCountryCodes {
		return nil, badRequest("country_codes must hold 1 to 250 two-letter country codes")
	}
	for i, cc := range l.CountryCodes {
		if !userListCountryPattern.MatchString(cc) || slices.Contains(l.CountryCodes[:i], cc) {
			return nil, badRequest("country_codes must hold distinct two-letter ISO-3166 codes in capitals")
		}
	}
	return map[string]any{"seedUserListIds": l.SeedUserListIDs, "expansionLevel": l.ExpansionLevel,
		"countryCodes": l.CountryCodes}, nil
}

// --- schemas ---

func userListEnumSchema(values []string) schema { return schema{"type": "string", "enum": values} }

func userListBoundedArray(items schema, minItems, maxItems int) schema {
	return schema{"type": "array", "minItems": minItems, "maxItems": maxItems, "items": items}
}

var (
	userListRuleNameSchema = schema{"type": "string", "minLength": 1, "maxLength": userListMaxRuleName}
	userListRuleTextSchema = schema{"type": "string", "minLength": 1, "maxLength": userListMaxRuleText}
	userListIdStringSchema = schema{"type": "string", "pattern": userListListIDPattern.String()}

	userListRuleItemSchema = object(schema{
		"name": userListRuleNameSchema,
		"number_rule_item": object(schema{"operator": userListEnumSchema(userListNumberOperators), "value": numberSchema},
			"operator", "value"),
		"string_rule_item": object(schema{"operator": userListEnumSchema(userListStringOperators), "value": userListRuleTextSchema},
			"operator", "value"),
		"date_rule_item": object(schema{"operator": userListEnumSchema(userListDateOperators),
			"value": schema{"type": "string", "pattern": userListRuleDatePattern.String()}, "offset_in_days": integerSchema},
			"operator"),
	}, "name")
	userListRuleSchema = object(schema{"rule_type": userListEnumSchema(userListRuleTypes),
		"rule_item_groups": userListBoundedArray(object(schema{"rule_items": userListBoundedArray(userListRuleItemSchema, 1, userListMaxRuleItems)},
			"rule_items"), 1, userListMaxRuleGroups)}, "rule_item_groups")
	userListOperandSchema = object(schema{"rule": userListRuleSchema,
		"lookback_window_days": schema{"type": "integer", "minimum": 0}}, "rule")

	userListRuleBasedSchema = object(schema{"prepopulation_status": userListEnumSchema(userListPrepopulationValues),
		"flexible_rule_user_list": object(schema{"inclusive_rule_operator": userListEnumSchema(userListFlexibleOperators),
			"inclusive_operands": userListBoundedArray(userListOperandSchema, 1, userListMaxOperands),
			"exclusive_operands": userListBoundedArray(userListOperandSchema, 0, userListMaxOperands)},
			"inclusive_rule_operator", "inclusive_operands")}, "flexible_rule_user_list")
	userListLogicalSchema = object(schema{"rules": userListBoundedArray(object(schema{"operator": userListEnumSchema(userListLogicalOperators),
		"rule_operands": userListBoundedArray(object(schema{"user_list": schema{"type": "string",
			"pattern": `^customers/[0-9]{1,10}/userLists/[0-9]{1,20}$`}}, "user_list"), 1, userListMaxLogicalOperands)},
		"operator", "rule_operands"), 1, userListMaxLogicalRules)}, "rules")
	userListBasicSchema = object(schema{"actions": userListBoundedArray(object(schema{
		"conversion_action":  schema{"type": "string", "pattern": conversionActionPattern},
		"remarketing_action": schema{"type": "string", "pattern": `^customers/[0-9]{1,10}/remarketingActions/[0-9]{1,20}$`}}),
		1, userListMaxActions)}, "actions")
	userListCrmSchema = object(schema{"app_id": schema{"type": "string", "minLength": 1, "maxLength": userListMaxAppID},
		"upload_key_type": userListEnumSchema(userListUploadKeyTypes), "data_source_type": userListEnumSchema(userListCrmDataSources)})
	userListLookalikeSchema = object(schema{"seed_user_list_ids": schema{"type": "array", "minItems": 1,
		"maxItems": userListMaxSeedLists, "uniqueItems": true, "items": userListIdStringSchema},
		"expansion_level": userListEnumSchema(userListExpansionLevels),
		"country_codes": schema{"type": "array", "minItems": 1, "maxItems": userListMaxCountryCodes, "uniqueItems": true,
			"items": schema{"type": "string", "pattern": userListCountryPattern.String()}}},
		"seed_user_list_ids", "expansion_level", "country_codes")
)

// userListTypeArguments documents the members that pick the type of a list.
var userListTypeArguments = []capability.Argument{
	{Name: "basic_user_list", Description: "A list of visitors that did a conversion or remarketing action: " +
		"actions, one to 20, each with exactly one of conversion_action (customers/ID/conversionActions/N) and " +
		"remarketing_action (customers/ID/remarketingActions/N) of customer_id"},
	{Name: "rule_based_user_list", Description: "A list that a rule generates: flexible_rule_user_list with " +
		"inclusive_rule_operator (AND or OR), inclusive_operands (1 to 20) and exclusive_operands (up to 20, " +
		"combined with OR and subtracted). Each operand has a rule (rule_item_groups of 1 to 20 groups of 1 to 20 " +
		"rule_items, rule_type OR_OF_ANDS only) and lookback_window_days. A rule item has a name (the key of the " +
		"tag variable, such as url__ or ref_url__) and exactly one of number_rule_item, string_rule_item " +
		"(operator and value) and date_rule_item (operator and one of value as YYYY-MM-DD and offset_in_days). " +
		"prepopulation_status REQUESTED adds past visitors of the last 30 days on the Display Network. Rule values " +
		"that look like addresses are only compared, never fetched"},
	{Name: "logical_user_list", Description: "A combination of other lists: rules, one to 10, each with an " +
		"operator (ALL, ANY, NONE) and rule_operands (1 to 20), each naming a user list of customer_id as " +
		"customers/ID/userLists/N in user_list. All rules apply together"},
	{Name: "crm_based_user_list", Description: "The empty shell of a Customer Match list: upload_key_type " +
		"(CONTACT_INFO, CRM_ID, MOBILE_ADVERTISING_ID), data_source_type (FIRST_PARTY by default; Google allows " +
		"the THIRD_PARTY values only for allowlisted accounts) and app_id for mobile advertising IDs. No tool " +
		"uploads members, so the list stays empty"},
	{Name: "lookalike_user_list", Description: "A lookalike list (create only, immutable): seed_user_list_ids " +
		"(one to 20 IDs of lists of customer_id), expansion_level (NARROW, BALANCED, BROAD) and country_codes " +
		"(two-letter ISO-3166 codes)"},
}

var userListTypeProperties = schema{
	"basic_user_list": userListBasicSchema, "rule_based_user_list": userListRuleBasedSchema, "logical_user_list": userListLogicalSchema,
	"crm_based_user_list": userListCrmSchema,
}
