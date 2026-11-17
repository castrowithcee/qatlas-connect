package googleanalytics

import (
	"fmt"
	"slices"
	"strings"
)

// Building blocks of the funnel report arguments (analyticsdata v1alpha, revision 20261114). The JSON schema
// subset has no references, so every expression is written out to a fixed depth: a leaf, and at most
// funnelExprDepth levels of and_group, or_group and not_expression above it.

const (
	funnelExprDepth   = 1
	funnelParamDepth  = 1
	segmentExprDepth  = 0
	segmentParamDepth = 0
	maxFunnelSteps    = 10
	maxSegments       = 4
	maxCriteria       = 10
)

const (
	paramNameSchema = `{"type":"string","minLength":1,"maxLength":100}`
	eventNameSchema = `{"type":"string","minLength":1,"maxLength":100}`
	// durationSchema is a protobuf Duration in seconds, such as 300s or 1.5s.
	durationSchema = `{"type":"string","pattern":"^[0-9]{1,10}(\\.[0-9]{1,9})?s$"}`
)

// treeSchema is an expression that is one leaf of leaves or, below the last level, a group of expressions.
// The leaves are written once per level by the caller-supplied function.
func treeSchema(depth int, leaves func() string) string {
	properties := leaves()
	if depth > 0 {
		inner := treeSchema(depth-1, leaves)
		group := objectSchema(`"expressions":`+arraySchema(inner, 1, 50), "expressions")
		properties += `,"and_group":` + group + `,"or_group":` + group + `,"not_expression":` + inner
	}
	return objectSchema(properties)
}

// parameterFilter is the filter on one parameter of an event, named by an event or an item parameter.
func parameterFilter(extra string) string {
	return objectSchema(`"event_parameter_name":` + paramNameSchema + `,"item_parameter_name":` + paramNameSchema +
		`,` + valueFilters + extra)
}

func funnelParameterExpression() string {
	return treeSchema(funnelParamDepth, func() string { return `"funnel_parameter_filter":` + parameterFilter(``) })
}

func funnelExpression() string {
	return treeSchema(funnelExprDepth, func() string {
		return `"funnel_field_filter":` + objectSchema(`"field_name":`+fieldNameSchema+`,`+valueFilters, "field_name") +
			`,"funnel_event_filter":` + objectSchema(`"event_name":`+eventNameSchema+
			`,"funnel_parameter_filter_expression":`+funnelParameterExpression(), "event_name")
	})
}

func segmentParameterExpression() string {
	return treeSchema(segmentParamDepth, func() string {
		return `"segment_parameter_filter":` + parameterFilter(`,"filter_scoping":`+
			objectSchema(`"in_any_n_day_period":{"type":"integer","minimum":1,"maximum":2147483647}`))
	})
}

func segmentExpression() string {
	return treeSchema(segmentExprDepth, func() string {
		return `"segment_filter":` + objectSchema(`"field_name":`+fieldNameSchema+`,`+valueFilters+
			`,"filter_scoping":`+objectSchema(`"at_any_point_in_time":{"type":"boolean"}`), "field_name") +
			`,"segment_event_filter":` + objectSchema(`"event_name":`+eventNameSchema+
			`,"segment_parameter_filter_expression":`+segmentParameterExpression(), "event_name")
	})
}

var userScopings = []string{"USER_CRITERIA_WITHIN_SAME_EVENT", "USER_CRITERIA_WITHIN_SAME_SESSION",
	"USER_CRITERIA_ACROSS_ALL_SESSIONS"}

// criteriaSchema is a SegmentCriteria: condition groups and, for a user segment, sequence groups.
func criteriaSchema(expression string, scopings []string, sequences bool) string {
	groups := `"and_condition_groups":` + arraySchema(objectSchema(`"condition_scoping":`+enumSchema(scopings...)+
		`,"segment_filter_expression":`+expression), 1, maxCriteria)
	if sequences {
		step := objectSchema(`"segment_filter_expression":` + expression + `,"is_directly_followed_by":{"type":"boolean"}` +
			`,"step_scoping":` + enumSchema(scopings...))
		groups += `,"and_sequence_groups":` + arraySchema(objectSchema(`"user_sequence_steps":`+
			arraySchema(step, 1, maxCriteria)+`,"sequence_maximum_duration":`+durationSchema+
			`,"sequence_scoping":`+enumSchema(scopings...), "user_sequence_steps"), 1, maxCriteria)
	}
	return objectSchema(groups)
}

// segmentKind is one of the three segment kinds with its documented member names.
func segmentKind(prefix string, scopings, durations []string, sequences bool) string {
	criteria := criteriaSchema(segmentExpression(), scopings, sequences)
	return objectSchema(`"` + prefix + `_inclusion_criteria":` + criteria + `,"exclusion":` +
		objectSchema(`"`+prefix+`_exclusion_duration":`+enumSchema(durations...)+`,"`+prefix+`_exclusion_criteria":`+criteria))
}

func segmentsSchema() string {
	return arraySchema(objectSchema(`"name":{"type":"string","minLength":1,"maxLength":100},`+
		`"session_segment":`+segmentKind("session", []string{"SESSION_CRITERIA_WITHIN_SAME_EVENT",
		"SESSION_CRITERIA_WITHIN_SAME_SESSION"}, []string{"SESSION_EXCLUSION_TEMPORARY", "SESSION_EXCLUSION_PERMANENT"}, false)+
		`,"event_segment":`+segmentKind("event", []string{"EVENT_CRITERIA_WITHIN_SAME_EVENT"},
		[]string{"EVENT_EXCLUSION_PERMANENT"}, false)+
		`,"user_segment":`+segmentKind("user", userScopings,
		[]string{"USER_EXCLUSION_TEMPORARY", "USER_EXCLUSION_PERMANENT"}, true)), 1, maxSegments)
}

func funnelSchema() string {
	step := objectSchema(`"name":{"type":"string","minLength":1,"maxLength":100},"filter_expression":`+
		funnelExpression()+`,"is_directly_followed_by":{"type":"boolean"},"within_duration_from_prior_step":`+
		durationSchema, "filter_expression")
	return objectSchema(`"is_open_funnel":{"type":"boolean"},"steps":`+arraySchema(step, 1, maxFunnelSteps), "steps")
}

// breakdownSchema is a FunnelBreakdown or FunnelNextAction: one dimension and a bound of its values.
func breakdownSchema(dimension string, max int) string {
	return objectSchema(`"`+dimension+`":`+dimensionSchema+fmt.Sprintf(`,"limit":{"type":"integer","minimum":1,"maximum":%d}`, max),
		dimension)
}

// checkValueFilters checks that a filter sets exactly one value filter and that a numeric value is either an
// integer or a double. The messages name members, never values.
func checkValueFilters(path string, m objectOf) error {
	if err := exactlyOne(path, m, "string_filter", "in_list_filter", "numeric_filter", "between_filter"); err != nil {
		return err
	}
	for _, key := range []string{"numeric_filter.value", "between_filter.from_value", "between_filter.to_value"} {
		outer, inner, _ := strings.Cut(key, ".")
		if value := asObject(asObject(m[outer])[inner]); value != nil {
			if err := exactlyOne(path+"."+key, value, "int64_value", "double_value"); err != nil {
				return err
			}
		}
	}
	return nil
}

type leafCheck func(path string, v any) error

// checkTree checks that an expression sets exactly one of its leaves, and_group, or_group or not_expression,
// and the same of every expression below it.
func checkTree(path string, v any, leaves map[string]leafCheck) error {
	m := asObject(v)
	keys := []string{"and_group", "or_group", "not_expression"}
	for key := range leaves {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if err := exactlyOne(path, m, keys...); err != nil {
		return err
	}
	switch {
	case m["not_expression"] != nil:
		return checkTree(path+".not_expression", m["not_expression"], leaves)
	case m["and_group"] != nil || m["or_group"] != nil:
		key := "and_group"
		if m["or_group"] != nil {
			key = "or_group"
		}
		list, _ := asObject(m[key])["expressions"].([]any)
		for i, item := range list {
			if err := checkTree(fmt.Sprintf("%s.%s.expressions[%d]", path, key, i), item, leaves); err != nil {
				return err
			}
		}
		return nil
	}
	for key, check := range leaves {
		if m[key] != nil {
			return check(path+"."+key, m[key])
		}
	}
	return nil
}

func checkParameterFilter(path string, v any) error {
	m := asObject(v)
	if err := exactlyOne(path, m, "event_parameter_name", "item_parameter_name"); err != nil {
		return err
	}
	return checkValueFilters(path, m)
}

func checkFieldFilter(path string, v any) error { return checkValueFilters(path, asObject(v)) }

// eventFilterCheck checks an event filter whose parameter expression is member.
func eventFilterCheck(member, leaf string) leafCheck {
	return func(path string, v any) error {
		expression := asObject(v)[member]
		if expression == nil {
			return nil
		}
		return checkTree(path+"."+member, expression, map[string]leafCheck{leaf: checkParameterFilter})
	}
}

var (
	funnelLeaves = map[string]leafCheck{"funnel_field_filter": checkFieldFilter,
		"funnel_event_filter": eventFilterCheck("funnel_parameter_filter_expression", "funnel_parameter_filter")}
	segmentLeaves = map[string]leafCheck{"segment_filter": checkFieldFilter,
		"segment_event_filter": eventFilterCheck("segment_parameter_filter_expression", "segment_parameter_filter")}
)

// checkExpressionsAt checks every expression held under the member key anywhere in v, which is a checked
// argument value. An expression is not searched further once it has been checked.
func checkExpressionsAt(path string, v any, key string, leaves map[string]leafCheck) error {
	switch x := v.(type) {
	case map[string]any:
		names := make([]string, 0, len(x))
		for name := range x {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			if name == key {
				if err := checkTree(path+"."+name, x[name], leaves); err != nil {
					return err
				}
				continue
			}
			if err := checkExpressionsAt(path+"."+name, x[name], key, leaves); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range x {
			if err := checkExpressionsAt(fmt.Sprintf("%s[%d]", path, i), item, key, leaves); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkSegments checks that every segment sets exactly one kind and that its expressions are well formed.
func checkSegments(v any) error {
	list, _ := v.([]any)
	for i, item := range list {
		path := fmt.Sprintf("segments[%d]", i)
		if err := exactlyOne(path, asObject(item), "session_segment", "event_segment", "user_segment"); err != nil {
			return err
		}
	}
	return checkExpressionsAt("segments", v, "segment_filter_expression", segmentLeaves)
}
