package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Bounds of the ExpandedDataSet resource. Read from the Discovery document analyticsadmin v1alpha (revision
// 20261116): the display name has at most 200 characters, the description at most 50. Google documents no
// number of dimensions or metrics for a data set (the Analytics help limits the expanded report to 6
// dimensions and 12 metrics), so the lists, the filter and the values are bounded by Qatlas below. dimension
// names, metric names and the filter cannot be changed after creation.
const (
	maxEdsDisplayRunes  = 200
	maxEdsDescRunes     = 50
	maxEdsDimensions    = 25
	maxEdsMetrics       = 50
	maxEdsFilterGroup   = 20
	maxEdsInListValues  = 100
	maxEdsFilterRunes   = 200
	maxEdsFilterBytes   = 16 << 10
	edsMatchExact       = "EXACT"
	edsMatchContains    = "CONTAINS"
	edsFilterDepthLimit = "3 levels"
)

func checkEdsDisplayName(value string) error {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return badRequest("display_name must be non-blank text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest("display_name must not hold control characters")
		}
	}
	if utf8.RuneCountInString(value) > maxEdsDisplayRunes {
		return badRequest("display_name must be at most 200 characters")
	}
	return nil
}

// checkEdsDescription accepts empty text, which clears the description on update.
func checkEdsDescription(value string) error {
	if !utf8.ValidString(value) {
		return badRequest("description must be valid text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest("description must not hold control characters")
		}
	}
	if utf8.RuneCountInString(value) > maxEdsDescRunes {
		return badRequest("description must be at most 50 characters")
	}
	return nil
}

// checkAPINames checks a list of dimension or metric API names: 1 to limit distinct names of the usual form.
func checkAPINames(argument string, values []string, limit int) error {
	if len(values) < 1 || len(values) > limit {
		return badRequest(argument + " must hold 1 to " + strconv.Itoa(limit) + " names")
	}
	seen := map[string]bool{}
	for _, v := range values {
		if !apiNamePattern.MatchString(v) {
			return badRequest(argument + " must hold API names of 1 to 100 letters, digits, underscores or colons, " +
				"starting with a letter")
		}
		if seen[v] {
			return badRequest(argument + " must not repeat a name")
		}
		seen[v] = true
	}
	return nil
}

func checkFilterText(argument, value string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxEdsFilterRunes {
		return badRequest(argument + " must be 1 to 200 characters of valid text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest(argument + " must not hold control characters")
		}
	}
	return nil
}

// The arguments of dimension_filter_expression, the snake case form of the body types in expandeddatasets.go.
type (
	stringFilterArg struct {
		MatchType     string `json:"match_type"`
		Value         string `json:"value"`
		CaseSensitive *bool  `json:"case_sensitive"`
	}
	inListFilterArg struct {
		Values        []string `json:"values"`
		CaseSensitive *bool    `json:"case_sensitive"`
	}
	filterArg struct {
		FieldName    string           `json:"field_name"`
		StringFilter *stringFilterArg `json:"string_filter"`
		InListFilter *inListFilterArg `json:"in_list_filter"`
	}
	negatedArg struct {
		Filter *filterArg `json:"filter"`
	}
	leafArg struct {
		Filter        *filterArg  `json:"filter"`
		NotExpression *negatedArg `json:"not_expression"`
	}
	groupArg struct {
		FilterExpressions []leafArg `json:"filter_expressions"`
	}
	filterExpressionArg struct {
		AndGroup *groupArg `json:"and_group"`
	}
)

const filterExpressionRule = "dimension_filter_expression must be an and_group of 1 to 20 filter_expressions, each " +
	"with exactly one of filter and not_expression (which holds one filter)"

// edsFilterBody checks one filter and returns its body form.
func (f *filterArg) body() (*edsFilterBody, error) {
	if f == nil {
		return nil, badRequest(filterExpressionRule)
	}
	if !apiNamePattern.MatchString(f.FieldName) {
		return nil, badRequest("field_name must be a dimension API name of 1 to 100 letters, digits, underscores or " +
			"colons, starting with a letter")
	}
	if (f.StringFilter == nil) == (f.InListFilter == nil) {
		return nil, badRequest("a filter needs exactly one of string_filter and in_list_filter")
	}
	out := &edsFilterBody{FieldName: apiName(f.FieldName)}
	if s := f.StringFilter; s != nil {
		if s.MatchType != edsMatchExact && s.MatchType != edsMatchContains {
			return nil, badRequest("match_type must be EXACT or CONTAINS")
		}
		if err := checkFilterText("string_filter value", s.Value); err != nil {
			return nil, err
		}
		exact := s.MatchType == edsMatchExact
		if exact && (s.CaseSensitive == nil || !*s.CaseSensitive) {
			return nil, badRequest("case_sensitive must be true when match_type is EXACT")
		}
		if !exact && s.CaseSensitive != nil && *s.CaseSensitive {
			return nil, badRequest("case_sensitive must be false when match_type is CONTAINS")
		}
		out.StringFilter = &edsStringFilterBody{MatchType: code(s.MatchType), Value: text(s.Value), CaseSensitive: s.CaseSensitive}
		return out, nil
	}
	in := f.InListFilter
	if len(in.Values) < 1 || len(in.Values) > maxEdsInListValues {
		return nil, badRequest("in_list_filter values must hold 1 to 100 values")
	}
	if in.CaseSensitive == nil || !*in.CaseSensitive {
		return nil, badRequest("case_sensitive must be true for in_list_filter")
	}
	values := make(textList, 0, len(in.Values))
	for _, v := range in.Values {
		if err := checkFilterText("in_list_filter values", v); err != nil {
			return nil, err
		}
		values = append(values, text(v))
	}
	out.InListFilter = &edsInListFilterBody{Values: values, CaseSensitive: in.CaseSensitive}
	return out, nil
}

// body checks the whole expression against the fixed shape and the size bound and returns its body form.
func (e *filterExpressionArg) body() (*filterExpressionBody, error) {
	if e == nil || e.AndGroup == nil || len(e.AndGroup.FilterExpressions) < 1 ||
		len(e.AndGroup.FilterExpressions) > maxEdsFilterGroup {
		return nil, badRequest(filterExpressionRule)
	}
	group := &edsGroupBody{}
	for _, leaf := range e.AndGroup.FilterExpressions {
		if (leaf.Filter == nil) == (leaf.NotExpression == nil) {
			return nil, badRequest(filterExpressionRule)
		}
		var item leafBody
		var err error
		if leaf.Filter != nil {
			item.Filter, err = leaf.Filter.body()
		} else {
			item.NotExpression = &negatedBody{}
			item.NotExpression.Filter, err = leaf.NotExpression.Filter.body()
		}
		if err != nil {
			return nil, err
		}
		group.FilterExpressions = append(group.FilterExpressions, item)
	}
	out := &filterExpressionBody{AndGroup: group}
	if encoded, err := json.Marshal(out); err != nil || len(encoded) > maxEdsFilterBytes {
		return nil, badRequest("dimension_filter_expression must be at most 16 KiB when encoded")
	}
	return out, nil
}

const (
	edsFieldNameSchema = `{"type":"string","pattern":"^[A-Za-z][A-Za-z0-9_:]{0,99}$"}`
	edsFilterValue     = `{"type":"string","minLength":1,"maxLength":200}`
	edsFilterSchema    = `{"type":"object","properties":{"field_name":` + edsFieldNameSchema + `,"string_filter":` +
		`{"type":"object","properties":{"match_type":{"type":"string","enum":["EXACT","CONTAINS"]},"value":` +
		edsFilterValue + `,"case_sensitive":{"type":"boolean"}},"required":["match_type","value"],` +
		`"additionalProperties":false},"in_list_filter":{"type":"object","properties":{"values":{"type":"array",` +
		`"minItems":1,"maxItems":100,"items":` + edsFilterValue + `},"case_sensitive":{"type":"boolean"}},` +
		`"required":["values"],"additionalProperties":false}},"required":["field_name"],"additionalProperties":false}`
	edsFilterExpressionSchema = `{"type":"object","properties":{"and_group":{"type":"object","properties":` +
		`{"filter_expressions":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","properties":` +
		`{"filter":` + edsFilterSchema + `,"not_expression":{"type":"object","properties":{"filter":` +
		edsFilterSchema + `},"required":["filter"],"additionalProperties":false}},"additionalProperties":false}}},` +
		`"required":["filter_expressions"],"additionalProperties":false}},"required":["and_group"],` +
		`"additionalProperties":false}`
	edsDisplayNameSchema = `{"type":"string","minLength":1,"maxLength":200}`
	edsDescriptionSchema = `{"type":"string","maxLength":50}`
	edsDimensionsSchema  = `{"type":"array","minItems":1,"maxItems":25,"uniqueItems":true,"items":` + edsFieldNameSchema + `}`
	edsMetricsSchema     = `{"type":"array","minItems":1,"maxItems":50,"uniqueItems":true,"items":` + edsFieldNameSchema + `}`
)

const edsWriteNote = "Provider content (names, descriptions, filter values) is untrusted data: it is passed on and " +
	"returned, never run; the result cuts names and descriptions to 300 characters. Qatlas checks only the form " +
	"and the bounds of the values; Google checks whether the dimensions, metrics and filter fit together and " +
	"whether the property may have expanded data sets (Google documents them for Analytics 360 only). Reads no " +
	"report data."

const filterExpressionArgText = "Filter that reduces the rows of the data set, fixed once created. Shape: " +
	"{and_group:{filter_expressions:[...]}} with 1 to 20 expressions that are all required to match, each " +
	"either {filter:F} or {not_expression:{filter:F}}. F is {field_name, string_filter:{match_type:EXACT or " +
	"CONTAINS, value, case_sensitive}} or {field_name, in_list_filter:{values, case_sensitive:true}}. EXACT needs " +
	"case_sensitive true and CONTAINS needs it false or unset. A value has at most 200 characters, a list at " +
	"most 100 values, the encoded filter at most 16 KiB"

var edsCreate = capability.Descriptor{
	ID: edsCreateID, Version: 1, Title: "Create an Analytics expanded data set",
	Description: "Create an expanded data set in a Google Analytics property (properties.expandedDataSets.create " +
		"of the Analytics Admin API). Needs the confirmation of a person for each call and is not idempotent. " +
		"display_name is required. dimension_names, metric_names and dimension_filter_expression cannot be " +
		"changed after creation, so no update tool changes them. Google assigns the ID of the data set. On a " +
		"connection with targets the property must be a bound property or belong to a bound account, which is " +
		"proven by one read of its parent before the change. " + edsWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "expanded-data-sets", "create", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema +
		`,"display_name":` + edsDisplayNameSchema + `,"description":` + edsDescriptionSchema +
		`,"dimension_names":` + edsDimensionsSchema + `,"metric_names":` + edsMetricsSchema +
		`,"dimension_filter_expression":` + edsFilterExpressionSchema + `},"required":["property","display_name"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(edsSchema),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID that gets the data set"},
		{Name: "display_name", Required: true, Description: "Display name, at most 200 characters, no control characters"},
		{Name: "description", Description: "Description, at most 50 characters"},
		{Name: "dimension_names", Description: "API names of the dimensions, 1 to 25 distinct names (a Qatlas " +
			"bound), for example eventName; fixed once created"},
		{Name: "metric_names", Description: "API names of the metrics, 1 to 50 distinct names (a Qatlas bound), " +
			"for example eventCount; fixed once created"},
		{Name: "dimension_filter_expression", Description: filterExpressionArgText},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The new expanded data set: " + edsFieldsText}},
	Examples: []capability.Example{{Description: "A data set of event names and counts for two events",
		Arguments: json.RawMessage(`{"property":"properties/200","display_name":"Events","dimension_names":` +
			`["eventName"],"metric_names":["eventCount"],"dimension_filter_expression":{"and_group":` +
			`{"filter_expressions":[{"filter":{"field_name":"eventName","in_list_filter":{"values":` +
			`["page_view","click"],"case_sensitive":true}}}]}}}`)}},
}

var edsUpdate = capability.Descriptor{
	ID: edsUpdateID, Version: 1, Title: "Update an Analytics expanded data set",
	Description: "Change an expanded data set of a Google Analytics property (properties.expandedDataSets.patch of " +
		"the Analytics Admin API). Only display_name and description can be changed: Google documents " +
		"dimension_names, metric_names and dimension_filter_expression as immutable, so this tool has no " +
		"argument for them and a call that names one is refused before any request. Only the arguments that " +
		"are set are changed, and at least one is required; Qatlas sends exactly them as the update mask. An " +
		"empty description clears it. Needs the confirmation of a person for each call. On a connection with " +
		"targets the property of the data set must be a bound property or belong to a bound account, which is " +
		"proven by one read of its parent before the change. " + edsWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "expanded-data-sets", "update", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + edsNameSchema + `,"display_name":` +
		edsDisplayNameSchema + `,"description":` + edsDescriptionSchema + `},"required":["name"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(edsSchema),
	Arguments: []capability.Argument{
		{Name: "name", Required: true, Description: "Expanded data set resource name " +
			"properties/ID/expandedDataSets/ID, from expanded-data-sets.list"},
		{Name: "display_name", Description: "New display name, at most 200 characters, no control characters"},
		{Name: "description", Description: "New description, at most 50 characters; empty clears it"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The expanded data set after the change: " + edsFieldsText}},
	Examples: []capability.Example{{Description: "Rename a data set",
		Arguments: json.RawMessage(`{"name":"properties/200/expandedDataSets/3001","display_name":"Events 2"}`)}},
}

var edsDelete = capability.Descriptor{
	ID: edsDeleteID, Version: 1, Title: "Delete an Analytics expanded data set",
	Description: "Delete an expanded data set of a Google Analytics property (properties.expandedDataSets.delete " +
		"of the Analytics Admin API). No tool restores it. Needs the confirmation of a person for each call. On a " +
		"connection with targets the property of the data set must be a bound property or belong to a bound " +
		"account, which is proven by one read of its parent before the change. Google's answer is empty and, " +
		"like all provider content, is untrusted data. Reads no report data." + alphaNote,
	Tags: []string{"google", "analytics", "expanded-data-sets", "delete", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + edsNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"deleted":` +
		`{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Expanded data set resource " +
		"name properties/ID/expandedDataSets/ID, from expanded-data-sets.list"}},
	Fields: []capability.Field{
		{Name: "name", Description: "The resource name that was deleted, as asked"},
		{Name: "deleted", Description: "true when Google confirmed the deletion; Google's answer is empty"},
	},
	Examples: []capability.Example{{Description: "Delete one expanded data set",
		Arguments: json.RawMessage(`{"name":"properties/200/expandedDataSets/3001"}`)}},
}

type edsWriteArguments struct {
	Property                  string               `json:"property"`
	Name                      string               `json:"name"`
	DisplayName               *string              `json:"display_name"`
	Description               *string              `json:"description"`
	DimensionNames            *[]string            `json:"dimension_names"`
	MetricNames               *[]string            `json:"metric_names"`
	DimensionFilterExpression *filterExpressionArg `json:"dimension_filter_expression"`
}

// edsRequest is the ExpandedDataSet of create and patch: only what the caller set. A description that is set
// but empty is sent, which clears it on update.
type edsRequest struct {
	DisplayName               *string               `json:"displayName,omitempty"`
	Description               *string               `json:"description,omitempty"`
	DimensionNames            []string              `json:"dimensionNames,omitempty"`
	MetricNames               []string              `json:"metricNames,omitempty"`
	DimensionFilterExpression *filterExpressionBody `json:"dimensionFilterExpression,omitempty"`
}

// updateMask holds the members of an update that are set, in a fixed order.
func (a edsWriteArguments) updateMask() string {
	var paths maskOf
	paths.add(a.DisplayName != nil, "display_name")
	paths.add(a.Description != nil, "description")
	return paths.String()
}

func (a edsWriteArguments) checkText() error {
	if a.DisplayName != nil {
		if err := checkEdsDisplayName(*a.DisplayName); err != nil {
			return err
		}
	}
	if a.Description != nil {
		return checkEdsDescription(*a.Description)
	}
	return nil
}

func handleEdsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create expanded data set"
	var arguments edsWriteArguments
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
	if arguments.DisplayName == nil {
		return nil, badRequest("property and display_name are required")
	}
	if err := arguments.checkText(); err != nil {
		return nil, err
	}
	request := edsRequest{DisplayName: arguments.DisplayName, Description: arguments.Description}
	if arguments.DimensionNames != nil {
		if err := checkAPINames("dimension_names", *arguments.DimensionNames, maxEdsDimensions); err != nil {
			return nil, err
		}
		request.DimensionNames = *arguments.DimensionNames
	}
	if arguments.MetricNames != nil {
		if err := checkAPINames("metric_names", *arguments.MetricNames, maxEdsMetrics); err != nil {
			return nil, err
		}
		request.MetricNames = *arguments.MetricNames
	}
	if arguments.DimensionFilterExpression != nil {
		if request.DimensionFilterExpression, err = arguments.DimensionFilterExpression.body(); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, failure(op, "the expanded data set could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, edsCreateID, property, proof); err != nil {
		return nil, err
	}
	var out expandedDataSet
	if err := c.change(ctx, changeCall{op: op, tool: edsCreateID, method: http.MethodPost,
		path: alphaPath(property + edsSegment), body: encoded, limit: maxEdsBytes, notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if !inPropertyEds(out.Name, property) {
		return nil, unsure(failure(op, subject+" answered without the name of an expanded data set of the property"))
	}
	return out, nil
}

func handleEdsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update expanded data set"
	var arguments edsWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkEdsName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	// The members Google documents as immutable are refused here as well, in case a caller passes them.
	if arguments.DimensionNames != nil || arguments.MetricNames != nil || arguments.DimensionFilterExpression != nil {
		return nil, badRequest("dimension_names, metric_names and dimension_filter_expression cannot be changed " +
			"after creation; create a new expanded data set instead")
	}
	mask := arguments.updateMask()
	if mask == "" {
		return nil, badRequest("set at least one of display_name and description")
	}
	if err := arguments.checkText(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(edsRequest{DisplayName: arguments.DisplayName, Description: arguments.Description})
	if err != nil {
		return nil, failure(op, "the expanded data set could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, edsUpdateID, property, proof); err != nil {
		return nil, err
	}
	var out expandedDataSet
	if err := c.change(ctx, changeCall{op: op, tool: edsUpdateID, method: http.MethodPatch,
		path: alphaPath(arguments.Name), query: map[string][]string{"updateMask": {mask}}, body: encoded,
		limit: maxEdsBytes, notFound: notFoundEds}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, unsure(failure(op, subject+" answered with another expanded data set than the one asked for"))
	}
	return out, nil
}

// edsDeleted is the result of a delete: Google answers with an empty object.
type edsDeleted struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

func handleEdsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete expanded data set"
	var arguments edsWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkEdsName("name", arguments.Name)
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
	if err := c.proveProperty(ctx, op, edsDeleteID, property, proof); err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: edsDeleteID, method: http.MethodDelete,
		path: alphaPath(arguments.Name), notFound: notFoundEds}, nil); err != nil {
		return nil, err
	}
	return edsDeleted{Name: arguments.Name, Deleted: true}, nil
}

// edsWriteSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func edsWriteSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a edsWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		switch tool {
		case edsCreateID:
			out := "Create Analytics expanded data set"
			if a.DisplayName != nil {
				out += " " + strconv.Quote(*a.DisplayName)
			}
			out += " in " + a.Property
			if a.DimensionNames != nil {
				out += ", " + strconv.Itoa(len(*a.DimensionNames)) + " dimensions"
			}
			if a.MetricNames != nil {
				out += ", " + strconv.Itoa(len(*a.MetricNames)) + " metrics"
			}
			if a.DimensionFilterExpression != nil {
				out += ", with a dimension filter"
			}
			return out + " (dimensions, metrics and filter cannot be changed later)"
		case edsUpdateID:
			return "Change " + strings.ReplaceAll(a.updateMask(), ",", ", ") + " of Analytics expanded data set " + a.Name
		}
		return "Delete Analytics expanded data set " + a.Name + " (no tool restores it)"
	}
}
