package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
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

// Bounds of the ChannelGroup resource, read from the Discovery document analyticsadmin v1alpha (revision
// 20261116): the display name has at most 80 characters, the description at most 256 and a group at most 50
// rules. Google documents no bound for the rule names, the filter values and the size of an expression; the
// bounds below are Qatlas bounds, checked before any request. Which expression Google accepts stays with Google.
const (
	// maxExpressionDepth is how many levels an expression nests, counted from the top level expression: the
	// documented shape is and_group of or_group of a filter, which a not_expression can wrap, so four levels.
	maxExpressionDepth = 4
	// maxExpressionNodes bounds all expressions, filters and values of one call.
	maxExpressionNodes = 500
	// maxRuleInList bounds the values of one in-list filter.
	maxRuleInList = 200
	maxFieldRunes = 100
)

var channelMatchTypes = []string{"EXACT", "BEGINS_WITH", "ENDS_WITH", "CONTAINS", "FULL_REGEXP", "PARTIAL_REGEXP"}

// The input side of the expression, as the arguments name it. A node holds exactly one member.
type (
	stringFilterIn struct {
		MatchType string  `json:"match_type"`
		Value     *string `json:"value"`
	}
	inListFilterIn struct {
		Values []string `json:"values"`
	}
	filterIn struct {
		FieldName    string          `json:"field_name"`
		StringFilter *stringFilterIn `json:"string_filter"`
		InListFilter *inListFilterIn `json:"in_list_filter"`
	}
	groupIn struct {
		FilterExpressions []exprIn `json:"filter_expressions"`
	}
	exprIn struct {
		Filter        *filterIn `json:"filter"`
		AndGroup      *groupIn  `json:"and_group"`
		OrGroup       *groupIn  `json:"or_group"`
		NotExpression *exprIn   `json:"not_expression"`
	}
	groupingRuleIn struct {
		DisplayName string  `json:"display_name"`
		Expression  *exprIn `json:"expression"`
	}
)

// The request side, in the member names of Google. Only what the caller set is sent.
type (
	stringFilterBody struct {
		MatchType string `json:"matchType"`
		Value     string `json:"value"`
	}
	inListFilterBody struct {
		Values []string `json:"values"`
	}
	filterBody struct {
		FieldName    string            `json:"fieldName"`
		StringFilter *stringFilterBody `json:"stringFilter,omitempty"`
		InListFilter *inListFilterBody `json:"inListFilter,omitempty"`
	}
	groupBody struct {
		FilterExpressions []exprBody `json:"filterExpressions"`
	}
	exprBody struct {
		Filter        *filterBody `json:"filter,omitempty"`
		AndGroup      *groupBody  `json:"andGroup,omitempty"`
		OrGroup       *groupBody  `json:"orGroup,omitempty"`
		NotExpression *exprBody   `json:"notExpression,omitempty"`
	}
	groupingRuleBody struct {
		DisplayName string    `json:"displayName"`
		Expression  *exprBody `json:"expression"`
	}
)

func checkPlainText(argument, value string, limit int) error {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return badRequest(argument + " must be non-blank text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest(argument + " must not hold control characters")
		}
	}
	if utf8.RuneCountInString(value) > limit {
		return badRequest(argument + " must be at most " + strconv.Itoa(limit) + " characters")
	}
	return nil
}

func checkChannelGroupName80(value string) error {
	return checkPlainText("display_name", value, maxChannelNameRunes)
}

// checkChannelDescription accepts empty text, which clears the description on update, line breaks and tabs.
func checkChannelDescription(value string) error {
	if !utf8.ValidString(value) {
		return badRequest("description must be valid text")
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return badRequest("description must not hold control characters")
		}
	}
	if utf8.RuneCountInString(value) > maxChannelDescriptionRunes {
		return badRequest("description must be at most 256 characters")
	}
	return nil
}

// checkFilterValue accepts non-empty valid text without control characters; a regular expression or a
// separator stays as the caller wrote it.
func checkFilterValue(argument, value string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRuleValueRunes {
		return badRequest(argument + " must be 1 to 1000 characters of valid text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest(argument + " must not hold control characters")
		}
	}
	return nil
}

// ruleBudget counts what one call holds, so that the size is bounded before any request.
type ruleBudget struct{ nodes int }

func (b *ruleBudget) take() error {
	if b.nodes++; b.nodes > maxExpressionNodes {
		return badRequest("grouping_rule is too large: at most 500 expressions, filters and values in all")
	}
	return nil
}

func groupBodyOf(g *groupIn, depth int, budget *ruleBudget) (*groupBody, error) {
	if len(g.FilterExpressions) == 0 {
		return nil, badRequest("a filter group needs at least one entry in filter_expressions")
	}
	out := &groupBody{FilterExpressions: make([]exprBody, 0, len(g.FilterExpressions))}
	for i := range g.FilterExpressions {
		expr, err := exprBodyOf(&g.FilterExpressions[i], depth+1, budget)
		if err != nil {
			return nil, err
		}
		out.FilterExpressions = append(out.FilterExpressions, *expr)
	}
	return out, nil
}

func filterBodyOf(f *filterIn, budget *ruleBudget) (*filterBody, error) {
	if err := checkPlainText("field_name", f.FieldName, maxFieldRunes); err != nil {
		return nil, err
	}
	out := &filterBody{FieldName: f.FieldName}
	switch {
	case f.StringFilter != nil && f.InListFilter == nil:
		sf := f.StringFilter
		if !slices.Contains(channelMatchTypes, sf.MatchType) {
			return nil, badRequest("match_type must be one of " + strings.Join(channelMatchTypes, ", "))
		}
		if sf.Value == nil {
			return nil, badRequest("string_filter needs a value")
		}
		if err := checkFilterValue("value", *sf.Value); err != nil {
			return nil, err
		}
		out.StringFilter = &stringFilterBody{MatchType: sf.MatchType, Value: *sf.Value}
	case f.InListFilter != nil && f.StringFilter == nil:
		values := f.InListFilter.Values
		if len(values) == 0 || len(values) > maxRuleInList {
			return nil, badRequest("in_list_filter needs 1 to 200 values")
		}
		out.InListFilter = &inListFilterBody{Values: make([]string, 0, len(values))}
		for _, v := range values {
			if err := budget.take(); err != nil {
				return nil, err
			}
			if err := checkFilterValue("values", v); err != nil {
				return nil, err
			}
			out.InListFilter.Values = append(out.InListFilter.Values, v)
		}
	default:
		return nil, badRequest("a filter needs exactly one of string_filter and in_list_filter")
	}
	return out, nil
}

// exprBodyOf checks one expression and returns its request form. The shape rules of Google (and_group only
// holds or_group, a not_expression only a filter) stay with Google; Qatlas checks that a node holds exactly
// one member, the depth and the size.
func exprBodyOf(e *exprIn, depth int, budget *ruleBudget) (*exprBody, error) {
	if depth > maxExpressionDepth {
		return nil, badRequest("an expression must not nest deeper than 4 levels")
	}
	if err := budget.take(); err != nil {
		return nil, err
	}
	set := 0
	for _, present := range []bool{e.Filter != nil, e.AndGroup != nil, e.OrGroup != nil, e.NotExpression != nil} {
		if present {
			set++
		}
	}
	if set != 1 {
		return nil, badRequest("an expression needs exactly one of filter, and_group, or_group and not_expression")
	}
	var (
		out = &exprBody{}
		err error
	)
	switch {
	case e.Filter != nil:
		out.Filter, err = filterBodyOf(e.Filter, budget)
	case e.AndGroup != nil:
		out.AndGroup, err = groupBodyOf(e.AndGroup, depth, budget)
	case e.OrGroup != nil:
		out.OrGroup, err = groupBodyOf(e.OrGroup, depth, budget)
	default:
		out.NotExpression, err = exprBodyOf(e.NotExpression, depth+1, budget)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// groupingBody checks the rules and returns their request form. The top level expression of a rule must be an
// and_group, as Google documents.
func groupingBody(rules []groupingRuleIn) ([]groupingRuleBody, error) {
	if len(rules) == 0 || len(rules) > maxChannelRules {
		return nil, badRequest("grouping_rule needs 1 to 50 rules")
	}
	budget := &ruleBudget{}
	out := make([]groupingRuleBody, 0, len(rules))
	for i := range rules {
		rule := &rules[i]
		if err := checkPlainText("grouping_rule display_name", rule.DisplayName, maxTextRunes); err != nil {
			return nil, err
		}
		if rule.Expression == nil || rule.Expression.AndGroup == nil {
			return nil, badRequest("the expression of a rule must be an and_group at the top level")
		}
		expr, err := exprBodyOf(rule.Expression, 1, budget)
		if err != nil {
			return nil, err
		}
		out = append(out, groupingRuleBody{DisplayName: rule.DisplayName, Expression: expr})
	}
	return out, nil
}

// channelExprSchema is an expression to the given number of levels, written out because the schema subset has
// no references: a filter, and below the last level also and_group, or_group and not_expression.
func channelExprSchema(levels int) string {
	filter := objectSchema(`"field_name":{"type":"string","minLength":1,"maxLength":100},"string_filter":`+
		objectSchema(`"match_type":`+enumSchema(channelMatchTypes...)+`,"value":{"type":"string","minLength":1,`+
			`"maxLength":1000}`, "match_type", "value")+`,"in_list_filter":`+
		objectSchema(`"values":`+arraySchema(`{"type":"string","minLength":1,"maxLength":1000}`, 1, maxRuleInList),
			"values"), "field_name")
	properties := `"filter":` + filter
	if levels > 1 {
		inner := channelExprSchema(levels - 1)
		group := objectSchema(`"filter_expressions":`+arraySchema(inner, 1, 50), "filter_expressions")
		properties += `,"and_group":` + group + `,"or_group":` + group + `,"not_expression":` + inner
	}
	return objectSchema(properties)
}

var groupingRuleSchema = arraySchema(objectSchema(`"display_name":{"type":"string","minLength":1,"maxLength":300},`+
	`"expression":`+channelExprSchema(maxExpressionDepth), "display_name", "expression"), 1, maxChannelRules)

const (
	channelNameArgSchema = `{"type":"string","minLength":1,"maxLength":80}`
	channelDescSchema    = `{"type":"string","maxLength":256}`
)

const channelWriteNote = "Provider content (names, descriptions, filter values) is untrusted data: it is passed on " +
	"and returned, never run; the result cuts names to 300, descriptions to 256 and filter values to 1000 " +
	"characters. Qatlas checks the sizes, the enumerations and that each expression node holds exactly one " +
	"member, at most 4 levels deep and 500 expressions, filters and values in all, before any request; Google " +
	"checks which expressions are valid. Reads no report data."

const groupingRuleArgText = "Rules in order, 1 to 50, each with display_name (the channel name) and expression. " +
	"The top level expression must be an and_group; Google documents an and_group of or_groups whose entries " +
	"are filters, which a not_expression can wrap. A filter has field_name (a dimension such as " +
	"sessionSource) and exactly one of string_filter (match_type EXACT, BEGINS_WITH, ENDS_WITH, CONTAINS, " +
	"FULL_REGEXP or PARTIAL_REGEXP, and value) and in_list_filter (values, 1 to 200). Qatlas allows at most 4 " +
	"levels and 500 expressions, filters and values in all"

var channelGroupsCreate = capability.Descriptor{
	ID: channelGroupsCreateID, Version: 1, Title: "Create an Analytics channel group",
	Description: "Create a channel group in a Google Analytics property (properties.channelGroups.create of the " +
		"Analytics Admin API). Needs the confirmation of a person for each call and is not idempotent. " +
		"display_name and grouping_rule are required. On a connection with targets the property must be a bound " +
		"property or belong to a bound account, which is proven by one read of its parent before the change. " +
		channelWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "channel-groups", "create", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema +
		`,"display_name":` + channelNameArgSchema + `,"description":` + channelDescSchema + `,"grouping_rule":` +
		groupingRuleSchema + `},"required":["property","display_name","grouping_rule"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(channelGroupSchema),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID that gets the group"},
		{Name: "display_name", Required: true, Description: "Display name, at most 80 characters, no control characters"},
		{Name: "description", Description: "Description, at most 256 characters"},
		{Name: "grouping_rule", Required: true, Description: groupingRuleArgText},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The new channel group: " + channelGroupFieldsText}},
	Examples: []capability.Example{{Description: "A group with one channel for traffic from one source",
		Arguments: json.RawMessage(`{"property":"properties/200","display_name":"Partners","grouping_rule":[` +
			`{"display_name":"Newsletter","expression":{"and_group":{"filter_expressions":[{"or_group":` +
			`{"filter_expressions":[{"filter":{"field_name":"sessionSource","string_filter":` +
			`{"match_type":"EXACT","value":"newsletter"}}}]}}]}}}]}`)}},
}

var channelGroupsUpdate = capability.Descriptor{
	ID: channelGroupsUpdateID, Version: 1, Title: "Update an Analytics channel group",
	Description: "Change a channel group of a Google Analytics property (properties.channelGroups.patch of the " +
		"Analytics Admin API). Only the arguments that are set are changed, and at least one is required; Qatlas " +
		"sends exactly them as the update mask. grouping_rule replaces all rules of the group. An empty " +
		"description clears it. primary true makes this group the default group of reports and unsets the " +
		"previous one. Google does not accept a new display name or new rules for the Default Channel Group that " +
		"it predefines; its refusal is returned as the error. Needs the confirmation of a person for each call. " +
		"On a connection with targets the property of the group must be a bound property or belong to a bound " +
		"account, which is proven by one read of its parent before the change. " + channelWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "channel-groups", "update", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + channelGroupNameSchema + `,"display_name":` +
		channelNameArgSchema + `,"description":` + channelDescSchema + `,"grouping_rule":` + groupingRuleSchema +
		`,"primary":{"type":"boolean"}},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(channelGroupSchema),
	Arguments: []capability.Argument{
		{Name: "name", Required: true, Description: "Channel group resource name " +
			"properties/ID/channelGroups/ID, from channel-groups.list"},
		{Name: "display_name", Description: "New display name, at most 80 characters, no control characters"},
		{Name: "description", Description: "New description, at most 256 characters; empty clears it"},
		{Name: "grouping_rule", Description: "New rules, which replace all rules of the group. " + groupingRuleArgText},
		{Name: "primary", Description: "true makes the group the default group of reports; false is sent as given"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The channel group after the change: " +
		channelGroupFieldsText}},
	Examples: []capability.Example{{Description: "Make a group the default group of reports",
		Arguments: json.RawMessage(`{"name":"properties/200/channelGroups/3001","primary":true}`)}},
}

var channelGroupsDelete = capability.Descriptor{
	ID: channelGroupsDeleteID, Version: 1, Title: "Delete an Analytics channel group",
	Description: "Delete a channel group of a Google Analytics property (properties.channelGroups.delete of the " +
		"Analytics Admin API). Reports that use the group lose it, and no tool restores it. Google does not " +
		"delete the Default Channel Group that it predefines; the tool sends the request as it is and returns " +
		"Google's refusal as the error. Needs the confirmation of a person for each call. On a connection with " +
		"targets the property of the group must be a bound property or belong to a bound account, which is " +
		"proven by one read of its parent before the change. Google's answer is empty and, like all provider " +
		"content, is untrusted data. Reads no report data." + alphaNote,
	Tags: []string{"google", "analytics", "channel-groups", "delete", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + channelGroupNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"deleted":` +
		`{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Channel group resource " +
		"name properties/ID/channelGroups/ID, from channel-groups.list"}},
	Fields: []capability.Field{
		{Name: "name", Description: "The resource name that was deleted, as asked"},
		{Name: "deleted", Description: "true when Google confirmed the deletion; Google's answer is empty"},
	},
	Examples: []capability.Example{{Description: "Delete one channel group",
		Arguments: json.RawMessage(`{"name":"properties/200/channelGroups/3001"}`)}},
}

type channelGroupWriteArguments struct {
	Property     string            `json:"property"`
	Name         string            `json:"name"`
	DisplayName  *string           `json:"display_name"`
	Description  *string           `json:"description"`
	GroupingRule *[]groupingRuleIn `json:"grouping_rule"`
	Primary      *bool             `json:"primary"`
}

// channelGroupBody is the ChannelGroup of create and patch: only what the caller set. A description that is
// set but empty is sent, which clears it on update.
type channelGroupBody struct {
	DisplayName  *string            `json:"displayName,omitempty"`
	Description  *string            `json:"description,omitempty"`
	GroupingRule []groupingRuleBody `json:"groupingRule,omitempty"`
	Primary      *bool              `json:"primary,omitempty"`
}

// mask is the update mask of the members that are set, in a fixed order.
func (a channelGroupWriteArguments) mask() string {
	var paths maskOf
	paths.add(a.DisplayName != nil, "display_name")
	paths.add(a.Description != nil, "description")
	paths.add(a.GroupingRule != nil, "grouping_rule")
	paths.add(a.Primary != nil, "primary")
	return paths.String()
}

// body checks the members that are set and returns their request form.
func (a channelGroupWriteArguments) body() (channelGroupBody, error) {
	out := channelGroupBody{DisplayName: a.DisplayName, Description: a.Description, Primary: a.Primary}
	if a.DisplayName != nil {
		if err := checkChannelGroupName80(*a.DisplayName); err != nil {
			return out, err
		}
	}
	if a.Description != nil {
		if err := checkChannelDescription(*a.Description); err != nil {
			return out, err
		}
	}
	if a.GroupingRule != nil {
		rules, err := groupingBody(*a.GroupingRule)
		if err != nil {
			return out, err
		}
		out.GroupingRule = rules
	}
	return out, nil
}

func handleChannelGroupsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create channel group"
	var arguments channelGroupWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	if arguments.DisplayName == nil || arguments.GroupingRule == nil {
		return nil, badRequest("property, display_name and grouping_rule are required")
	}
	body, err := arguments.body()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the channel group could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, channelGroupsCreateID, property, proof); err != nil {
		return nil, err
	}
	var out channelGroup
	if err := c.change(ctx, changeCall{op: op, tool: channelGroupsCreateID, method: http.MethodPost,
		path: alphaPath(property + channelGroupSegment), body: encoded, limit: maxChannelGroupBytes,
		notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if !inPropertyChannelGroup(out.Name, property) {
		return nil, unsure(failure(op, subject+" answered without the name of the channel group that was created"))
	}
	out.GroupingRule = head(out.GroupingRule, maxChannelRules)
	return out, nil
}

func handleChannelGroupsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update channel group"
	var arguments channelGroupWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkChannelGroupName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	mask := arguments.mask()
	if mask == "" {
		return nil, badRequest("set at least one of display_name, description, grouping_rule and primary")
	}
	body, err := arguments.body()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the channel group could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, channelGroupsUpdateID, property, proof); err != nil {
		return nil, err
	}
	var out channelGroup
	if err := c.change(ctx, changeCall{op: op, tool: channelGroupsUpdateID, method: http.MethodPatch,
		path: alphaPath(arguments.Name), query: url.Values{"updateMask": {mask}}, body: encoded,
		limit: maxChannelGroupBytes, notFound: notFoundChannelGroup}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, unsure(failure(op, subject+" answered with another channel group than the one asked for"))
	}
	out.GroupingRule = head(out.GroupingRule, maxChannelRules)
	return out, nil
}

// channelGroupDeleted is the result of a delete: Google answers with an empty object.
type channelGroupDeleted struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

func handleChannelGroupsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete channel group"
	var arguments channelGroupWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkChannelGroupName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, channelGroupsDeleteID, property, proof); err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: channelGroupsDeleteID, method: http.MethodDelete,
		path: alphaPath(arguments.Name), notFound: notFoundChannelGroup}, nil); err != nil {
		return nil, err
	}
	return channelGroupDeleted{Name: arguments.Name, Deleted: true}, nil
}

// channelGroupSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func channelGroupSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a channelGroupWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		switch tool {
		case channelGroupsCreateID:
			out := "Create Analytics channel group"
			if a.DisplayName != nil {
				out += " " + strconv.Quote(*a.DisplayName)
			}
			out += " in " + a.Property
			if a.GroupingRule != nil {
				out += " with " + strconv.Itoa(len(*a.GroupingRule)) + " grouping rules"
			}
			return out
		case channelGroupsUpdateID:
			out := "Change " + strings.ReplaceAll(a.mask(), ",", ", ") + " of Analytics channel group " + a.Name
			if a.Primary != nil {
				out += ", primary " + strconv.FormatBool(*a.Primary)
			}
			return out
		}
		return "Delete Analytics channel group " + a.Name + " (reports that use it lose it; no tool restores it)"
	}
}
