package googleanalytics

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Bounds of the filter clauses of an audience. Google documents no limits for the size of a filter, so these
// are Qatlas bounds: a request over them is refused before any I/O, and a result is cut to them. The grammar
// of the expressions follows the Discovery document analyticsadmin v1alpha (revision 20261115): the top level
// of a filter expression is an and group of or groups, an or group holds filters, event filters and negated
// filters, a negation holds one filter, and the parameter expression of an event filter has the same shape
// without event filters. The deepest legal expression has 7 levels.
const (
	maxAudFilterClauses  = 20
	maxAudSequenceSteps  = 20
	maxAudFilterNodes    = 200
	maxAudFilterDepth    = 8
	maxAudGroupItems     = 50
	maxAudInListValues   = 100
	maxAudFilterValue    = 300
	maxAudFilterNesting  = 24
	maxAudFilterBodySize = 64 << 10
)

var (
	audienceScopes   = []string{"AUDIENCE_FILTER_SCOPE_WITHIN_SAME_EVENT", "AUDIENCE_FILTER_SCOPE_WITHIN_SAME_SESSION", "AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS"}
	clauseTypes      = []string{"INCLUDE", "EXCLUDE"}
	stringMatchTypes = []string{"EXACT", "BEGINS_WITH", "ENDS_WITH", "CONTAINS", "FULL_REGEXP"}
	numericOps       = []string{"EQUAL", "LESS_THAN", "GREATER_THAN"}
	logConditions    = []string{"AUDIENCE_JOINED", "AUDIENCE_MEMBERSHIP_RENEWED"}
	exclusionModes   = []string{"EXCLUDE_TEMPORARILY", "EXCLUDE_PERMANENTLY"}

	filterFieldPattern = regexp.MustCompile(`^[A-Za-z0-9_:.-]{1,64}$`)
	filterEventPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,39}$`)
	int64Pattern       = regexp.MustCompile(`^-?[0-9]{1,18}$`)
	durationPattern    = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,9})?s$`)
)

// The types below are the filter structure of an Audience answer. Every text is bounded by its type.
type (
	audStringFilter struct {
		Value         text  `json:"value,omitempty"`
		MatchType     code  `json:"matchType,omitempty"`
		CaseSensitive *bool `json:"caseSensitive,omitempty"`
	}
	audInListFilter struct {
		CaseSensitive *bool  `json:"caseSensitive,omitempty"`
		Values        []text `json:"values,omitempty"`
	}
	audNumericValue struct {
		DoubleValue *float64 `json:"doubleValue,omitempty"`
		Int64Value  code     `json:"int64Value,omitempty"`
	}
	audNumericFilter struct {
		Operation code             `json:"operation,omitempty"`
		Value     *audNumericValue `json:"value,omitempty"`
	}
	audBetweenFilter struct {
		FromValue *audNumericValue `json:"fromValue,omitempty"`
		ToValue   *audNumericValue `json:"toValue,omitempty"`
	}
	audMetricFilter struct {
		StringFilter     *audStringFilter  `json:"stringFilter,omitempty"`
		InListFilter     *audInListFilter  `json:"inListFilter,omitempty"`
		FieldName        code              `json:"fieldName,omitempty"`
		NumericFilter    *audNumericFilter `json:"numericFilter,omitempty"`
		BetweenFilter    *audBetweenFilter `json:"betweenFilter,omitempty"`
		InAnyNDayPeriod  *int32            `json:"inAnyNDayPeriod,omitempty"`
		AtAnyPointInTime *bool             `json:"atAnyPointInTime,omitempty"`
	}
	audEventFilter struct {
		EventName                      code     `json:"eventName,omitempty"`
		EventParameterFilterExpression *audExpr `json:"eventParameterFilterExpression,omitempty"`
	}
	audExprList struct {
		FilterExpressions []audExpr `json:"filterExpressions,omitempty"`
	}
	audExpr struct {
		DimensionOrMetricFilter *audMetricFilter `json:"dimensionOrMetricFilter,omitempty"`
		EventFilter             *audEventFilter  `json:"eventFilter,omitempty"`
		OrGroup                 *audExprList     `json:"orGroup,omitempty"`
		NotExpression           *audExpr         `json:"notExpression,omitempty"`
		AndGroup                *audExprList     `json:"andGroup,omitempty"`
	}
	audSimpleFilter struct {
		FilterExpression *audExpr `json:"filterExpression,omitempty"`
		Scope            code     `json:"scope,omitempty"`
	}
	audSequenceStep struct {
		Scope              code     `json:"scope,omitempty"`
		ImmediatelyFollows *bool    `json:"immediatelyFollows,omitempty"`
		ConstraintDuration code     `json:"constraintDuration,omitempty"`
		FilterExpression   *audExpr `json:"filterExpression,omitempty"`
	}
	audSequenceFilter struct {
		SequenceMaximumDuration code              `json:"sequenceMaximumDuration,omitempty"`
		Scope                   code              `json:"scope,omitempty"`
		SequenceSteps           []audSequenceStep `json:"sequenceSteps,omitempty"`
	}
	audClause struct {
		ClauseType     code               `json:"clauseType,omitempty"`
		SimpleFilter   *audSimpleFilter   `json:"simpleFilter,omitempty"`
		SequenceFilter *audSequenceFilter `json:"sequenceFilter,omitempty"`
	}
)

// boundClauses cuts the clauses of an answer: the number of clauses and steps, the number and depth of the
// expressions and the values of a list filter.
func boundClauses(clauses []audClause) []audClause {
	clauses = head(clauses, maxAudFilterClauses)
	budget := maxAudFilterNodes
	for i := range clauses {
		if s := clauses[i].SimpleFilter; s != nil {
			s.FilterExpression = limitExpr(s.FilterExpression, 1, &budget)
		}
		if q := clauses[i].SequenceFilter; q != nil {
			q.SequenceSteps = head(q.SequenceSteps, maxAudSequenceSteps)
			for j := range q.SequenceSteps {
				q.SequenceSteps[j].FilterExpression = limitExpr(q.SequenceSteps[j].FilterExpression, 1, &budget)
			}
		}
	}
	return clauses
}

func limitExpr(e *audExpr, depth int, budget *int) *audExpr {
	if e == nil || depth > maxAudFilterDepth || *budget <= 0 {
		return nil
	}
	*budget--
	e.NotExpression = limitExpr(e.NotExpression, depth+1, budget)
	e.AndGroup = limitGroup(e.AndGroup, depth+1, budget)
	e.OrGroup = limitGroup(e.OrGroup, depth+1, budget)
	if f := e.EventFilter; f != nil {
		f.EventParameterFilterExpression = limitExpr(f.EventParameterFilterExpression, depth+1, budget)
	}
	if f := e.DimensionOrMetricFilter; f != nil && f.InListFilter != nil {
		f.InListFilter.Values = head(f.InListFilter.Values, maxAudInListValues)
	}
	return e
}

func limitGroup(g *audExprList, depth int, budget *int) *audExprList {
	if g == nil {
		return nil
	}
	items := head(g.FilterExpressions, maxAudGroupItems)
	kept := make([]audExpr, 0, len(items))
	for i := range items {
		if e := limitExpr(&items[i], depth, budget); e != nil {
			kept = append(kept, *e)
		}
	}
	g.FilterExpressions = kept
	return g
}

// The request side works on the decoded argument tree, which the schema has already shaped. Qatlas checks it
// again before any I/O, because the bounds and the grammar must hold for every caller.

type exprKind int

const (
	kindTop exprKind = iota
	kindParamTop
	kindAndItem
	kindParamAndItem
	kindOrItem
	kindParamOrItem
	kindNot
)

// allowedKeys are the members an expression of each kind may hold; exactly one is set.
var allowedKeys = map[exprKind][]string{
	kindTop:          {"and_group"},
	kindParamTop:     {"and_group"},
	kindAndItem:      {"or_group"},
	kindParamAndItem: {"or_group"},
	kindOrItem:       {"dimension_or_metric_filter", "event_filter", "not_expression"},
	kindParamOrItem:  {"dimension_or_metric_filter", "not_expression"},
	kindNot:          {"dimension_or_metric_filter"},
}

type filterChecker struct{ nodes int }

func refuseFilter(path, rule string) error { return badRequest(path + " " + rule) }

// object returns the members of an object that holds only the listed ones.
func object(value any, path string, allowed ...string) (map[string]any, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, refuseFilter(path, "must be an object")
	}
	for key := range m {
		if !slices.Contains(allowed, key) {
			return nil, refuseFilter(path+"."+key, "is not allowed here")
		}
	}
	return m, nil
}

func enumValue(m map[string]any, key, path string, values []string, required bool) error {
	v, set := m[key]
	if !set {
		if required {
			return refuseFilter(path+"."+key, "is required")
		}
		return nil
	}
	s, ok := v.(string)
	if !ok || !slices.Contains(values, s) {
		return refuseFilter(path+"."+key, "must be one of "+strings.Join(values, ", "))
	}
	return nil
}

func boolValue(m map[string]any, key, path string) error {
	if v, set := m[key]; set {
		if _, ok := v.(bool); !ok {
			return refuseFilter(path+"."+key, "must be true or false")
		}
	}
	return nil
}

// filterText checks a value of a filter: valid text without control characters, 1 to maxAudFilterValue characters.
func filterText(value any, path string) error {
	s, ok := value.(string)
	if !ok || s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxAudFilterValue {
		return refuseFilter(path, "must be text of 1 to 300 characters")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return refuseFilter(path, "must not hold control characters")
		}
	}
	return nil
}

func patternValue(m map[string]any, key, path string, pattern *regexp.Regexp, rule string, required bool) error {
	v, set := m[key]
	if !set {
		if required {
			return refuseFilter(path+"."+key, "is required")
		}
		return nil
	}
	if s, ok := v.(string); !ok || !pattern.MatchString(s) {
		return refuseFilter(path+"."+key, rule)
	}
	return nil
}

func (f *filterChecker) numeric(value any, path string) error {
	m, err := object(value, path, "int64_value", "double_value")
	if err != nil {
		return err
	}
	if len(m) != 1 {
		return refuseFilter(path, "must set exactly one of int64_value and double_value")
	}
	if err := patternValue(m, "int64_value", path, int64Pattern, "must be a whole number of at most 18 digits as text", false); err != nil {
		return err
	}
	if v, set := m["double_value"]; set {
		n, ok := v.(json.Number)
		if !ok {
			return refuseFilter(path+".double_value", "must be a number")
		}
		if x, err := strconv.ParseFloat(string(n), 64); err != nil || math.IsInf(x, 0) || math.IsNaN(x) {
			return refuseFilter(path+".double_value", "must be a finite number")
		}
	}
	return nil
}

func (f *filterChecker) metricFilter(value any, path string) error {
	m, err := object(value, path, "field_name", "string_filter", "in_list_filter", "numeric_filter",
		"between_filter", "in_any_n_day_period", "at_any_point_in_time")
	if err != nil {
		return err
	}
	if err := patternValue(m, "field_name", path, filterFieldPattern,
		"must be 1 to 64 letters, digits, underscores, colons, points or hyphens", true); err != nil {
		return err
	}
	kinds := 0
	for _, key := range []string{"string_filter", "in_list_filter", "numeric_filter", "between_filter"} {
		if _, set := m[key]; set {
			kinds++
		}
	}
	if kinds != 1 {
		return refuseFilter(path, "must set exactly one of string_filter, in_list_filter, numeric_filter and between_filter")
	}
	if err := boolValue(m, "at_any_point_in_time", path); err != nil {
		return err
	}
	if v, set := m["in_any_n_day_period"]; set {
		n, ok := v.(json.Number)
		days, err := strconv.Atoi(string(n))
		if !ok || err != nil || days < 1 || days > 60 {
			return refuseFilter(path+".in_any_n_day_period", "must be a whole number from 1 to 60")
		}
	}
	if v, set := m["string_filter"]; set {
		s, err := object(v, path+".string_filter", "value", "match_type", "case_sensitive")
		if err != nil {
			return err
		}
		if _, ok := s["value"]; !ok {
			return refuseFilter(path+".string_filter.value", "is required")
		}
		if err := filterText(s["value"], path+".string_filter.value"); err != nil {
			return err
		}
		if err := enumValue(s, "match_type", path+".string_filter", stringMatchTypes, true); err != nil {
			return err
		}
		if err := boolValue(s, "case_sensitive", path+".string_filter"); err != nil {
			return err
		}
	}
	if v, set := m["in_list_filter"]; set {
		s, err := object(v, path+".in_list_filter", "values", "case_sensitive")
		if err != nil {
			return err
		}
		values, ok := s["values"].([]any)
		if !ok || len(values) < 1 || len(values) > maxAudInListValues {
			return refuseFilter(path+".in_list_filter.values", "must hold 1 to 100 values")
		}
		for i, item := range values {
			if err := filterText(item, path+".in_list_filter.values["+strconv.Itoa(i)+"]"); err != nil {
				return err
			}
		}
		if err := boolValue(s, "case_sensitive", path+".in_list_filter"); err != nil {
			return err
		}
	}
	if v, set := m["numeric_filter"]; set {
		s, err := object(v, path+".numeric_filter", "operation", "value")
		if err != nil {
			return err
		}
		if err := enumValue(s, "operation", path+".numeric_filter", numericOps, true); err != nil {
			return err
		}
		if err := f.numeric(s["value"], path+".numeric_filter.value"); err != nil {
			return err
		}
	}
	if v, set := m["between_filter"]; set {
		s, err := object(v, path+".between_filter", "from_value", "to_value")
		if err != nil {
			return err
		}
		for _, key := range []string{"from_value", "to_value"} {
			if err := f.numeric(s[key], path+".between_filter."+key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *filterChecker) group(value any, path string, item exprKind, depth int) error {
	items, ok := value.([]any)
	if !ok || len(items) < 1 || len(items) > maxAudGroupItems {
		return refuseFilter(path, "must hold 1 to 50 expressions")
	}
	for i, e := range items {
		if err := f.expr(e, path+"["+strconv.Itoa(i)+"]", item, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// expr checks one filter expression of the given kind.
func (f *filterChecker) expr(value any, path string, kind exprKind, depth int) error {
	if depth > maxAudFilterDepth {
		return refuseFilter(path, "is nested deeper than 8 levels")
	}
	if f.nodes++; f.nodes > maxAudFilterNodes {
		return refuseFilter(path, "exceeds the limit of 200 filter expressions in one audience")
	}
	m, err := object(value, path, allowedKeys[kind]...)
	if err != nil {
		return err
	}
	if len(m) != 1 {
		return refuseFilter(path, "must set exactly one of "+strings.Join(allowedKeys[kind], ", "))
	}
	param := kind == kindParamTop || kind == kindParamAndItem || kind == kindParamOrItem
	for key, v := range m {
		child := path + "." + key
		switch key {
		case "and_group":
			item := kindAndItem
			if param {
				item = kindParamAndItem
			}
			return f.group(v, child, item, depth)
		case "or_group":
			item := kindOrItem
			if param {
				item = kindParamOrItem
			}
			return f.group(v, child, item, depth)
		case "not_expression":
			return f.expr(v, child, kindNot, depth+1)
		case "dimension_or_metric_filter":
			return f.metricFilter(v, child)
		case "event_filter":
			e, err := object(v, child, "event_name", "event_parameter_filter_expression")
			if err != nil {
				return err
			}
			if err := patternValue(e, "event_name", child, filterEventPattern,
				"must be 1 to 40 letters, digits or underscores and start with a letter", true); err != nil {
				return err
			}
			if p, set := e["event_parameter_filter_expression"]; set {
				return f.expr(p, child+".event_parameter_filter_expression", kindParamTop, depth+1)
			}
		}
	}
	return nil
}

func (f *filterChecker) simple(value any, path string) error {
	m, err := object(value, path, "scope", "filter_expression")
	if err != nil {
		return err
	}
	if err := enumValue(m, "scope", path, audienceScopes, true); err != nil {
		return err
	}
	if _, ok := m["filter_expression"]; !ok {
		return refuseFilter(path+".filter_expression", "is required")
	}
	return f.expr(m["filter_expression"], path+".filter_expression", kindTop, 1)
}

func (f *filterChecker) sequence(value any, path string) error {
	m, err := object(value, path, "scope", "sequence_maximum_duration", "sequence_steps")
	if err != nil {
		return err
	}
	if err := enumValue(m, "scope", path, audienceScopes, true); err != nil {
		return err
	}
	if err := patternValue(m, "sequence_maximum_duration", path, durationPattern,
		"must be seconds as text such as 3600s", false); err != nil {
		return err
	}
	steps, ok := m["sequence_steps"].([]any)
	if !ok || len(steps) < 1 || len(steps) > maxAudSequenceSteps {
		return refuseFilter(path+".sequence_steps", "must hold 1 to 20 steps")
	}
	for i, s := range steps {
		sp := path + ".sequence_steps[" + strconv.Itoa(i) + "]"
		step, err := object(s, sp, "scope", "immediately_follows", "constraint_duration", "filter_expression")
		if err != nil {
			return err
		}
		if err := enumValue(step, "scope", sp, audienceScopes, true); err != nil {
			return err
		}
		if err := boolValue(step, "immediately_follows", sp); err != nil {
			return err
		}
		if err := patternValue(step, "constraint_duration", sp, durationPattern,
			"must be seconds as text such as 600s", false); err != nil {
			return err
		}
		if _, ok := step["filter_expression"]; !ok {
			return refuseFilter(sp+".filter_expression", "is required")
		}
		if err := f.expr(step["filter_expression"], sp+".filter_expression", kindTop, 1); err != nil {
			return err
		}
	}
	return nil
}

// nesting returns the number of object and array levels of a decoded value.
func nesting(value any) int {
	deepest := 0
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			deepest = max(deepest, nesting(child))
		}
		return deepest + 1
	case []any:
		for _, child := range v {
			deepest = max(deepest, nesting(child))
		}
		return deepest + 1
	}
	return 0
}

// checkFilterClauses decodes the filter_clauses argument and checks its size, nesting and grammar. It returns
// the clauses in the form Google takes.
func checkFilterClauses(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 {
		return nil, badRequest("filter_clauses is required")
	}
	if len(raw) > maxAudFilterBodySize {
		return nil, badRequest("filter_clauses must be at most 65536 bytes")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, badRequest("filter_clauses must be valid JSON")
	}
	clauses, ok := value.([]any)
	if !ok || len(clauses) < 1 || len(clauses) > maxAudFilterClauses {
		return nil, badRequest("filter_clauses must hold 1 to 20 clauses")
	}
	if nesting(value) > maxAudFilterNesting {
		return nil, badRequest("filter_clauses is nested deeper than the limit of 24 levels")
	}
	var f filterChecker
	for i, c := range clauses {
		path := "filter_clauses[" + strconv.Itoa(i) + "]"
		m, err := object(c, path, "clause_type", "simple_filter", "sequence_filter")
		if err != nil {
			return nil, err
		}
		if err := enumValue(m, "clause_type", path, clauseTypes, true); err != nil {
			return nil, err
		}
		_, simple := m["simple_filter"]
		_, sequence := m["sequence_filter"]
		if simple == sequence {
			return nil, refuseFilter(path, "must set exactly one of simple_filter and sequence_filter")
		}
		if simple {
			err = f.simple(m["simple_filter"], path+".simple_filter")
		} else {
			err = f.sequence(m["sequence_filter"], path+".sequence_filter")
		}
		if err != nil {
			return nil, err
		}
	}
	return toGoogle(clauses).([]any), nil
}

// toGoogle renames the validated members from snake case to the camel case of the API and wraps the groups
// into the list message Google expects.
func toGoogle(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = toGoogle(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			converted := toGoogle(child)
			if key == "and_group" || key == "or_group" {
				converted = map[string]any{"filterExpressions": converted}
			}
			out[camel(key)] = converted
		}
		return out
	}
	return value
}

func camel(snake string) string {
	parts := strings.Split(snake, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}
