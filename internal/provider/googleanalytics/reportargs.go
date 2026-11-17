package googleanalytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Building blocks of the arguments of the Data API tools. Every argument is a structured member; the handler
// renames the checked members to the documented ones and sends nothing else. The JSON schema subset has no
// references, so a nested filter expression is written out to a fixed depth.

const (
	// maxFilterDepth is how many levels of and_group, or_group and not_expression a filter expression nests.
	maxFilterDepth = 2
	// maxFilterText bounds one filter value; maxInListValues the values of one in-list filter.
	maxFilterText   = 1000
	maxInListValues = 200
)

// fieldName is the API name of a dimension or metric; a custom one has a scope prefix such as customEvent:.
const fieldNameSchema = `{"type":"string","minLength":1,"maxLength":100,"pattern":"^[A-Za-z0-9_:]+$"}`

const numericValueSchema = `{"type":"object","properties":{"int64_value":{"type":"integer"},` +
	`"double_value":{"type":"number"}},"additionalProperties":false}`

func enumSchema(values ...string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = `"` + v + `"`
	}
	return `{"type":"string","enum":[` + strings.Join(quoted, ",") + `]}`
}

func objectSchema(properties string, required ...string) string {
	out := `{"type":"object","properties":{` + properties + `}`
	if len(required) > 0 {
		out += `,"required":["` + strings.Join(required, `","`) + `"]`
	}
	return out + `,"additionalProperties":false}`
}

func arraySchema(items string, min, max int) string {
	return fmt.Sprintf(`{"type":"array","minItems":%d,"maxItems":%d,"items":%s}`, min, max, items)
}

// filterSchema is the Filter of analyticsdata v1beta: a field name and exactly one of five filters.
var filterSchema = objectSchema(
	`"field_name":`+fieldNameSchema+`,`+
		`"string_filter":`+objectSchema(`"value":{"type":"string","maxLength":1000},"match_type":`+
		enumSchema("EXACT", "BEGINS_WITH", "ENDS_WITH", "CONTAINS", "FULL_REGEXP", "PARTIAL_REGEXP")+
		`,"case_sensitive":{"type":"boolean"}`, "value")+`,`+
		`"in_list_filter":`+objectSchema(`"values":`+arraySchema(`{"type":"string","maxLength":1000}`, 1, maxInListValues)+
		`,"case_sensitive":{"type":"boolean"}`, "values")+`,`+
		`"numeric_filter":`+objectSchema(`"operation":`+enumSchema("EQUAL", "LESS_THAN", "LESS_THAN_OR_EQUAL",
		"GREATER_THAN", "GREATER_THAN_OR_EQUAL")+`,"value":`+numericValueSchema, "operation", "value")+`,`+
		`"between_filter":`+objectSchema(`"from_value":`+numericValueSchema+`,"to_value":`+numericValueSchema,
		"from_value", "to_value")+`,`+
		`"empty_filter":`+objectSchema(``),
	"field_name")

// filterExpressionSchema is the FilterExpression to the given depth: a filter, and below the last level also
// and_group, or_group and not_expression.
func filterExpressionSchema(depth int) string {
	properties := `"filter":` + filterSchema
	if depth > 0 {
		inner := filterExpressionSchema(depth - 1)
		group := objectSchema(`"expressions":`+arraySchema(inner, 1, 50), "expressions")
		properties += `,"and_group":` + group + `,"or_group":` + group + `,"not_expression":` + inner
	}
	return objectSchema(properties)
}

var (
	dimensionsSchema = arraySchema(objectSchema(`"name":`+fieldNameSchema+`,"dimension_expression":`+
		objectSchema(`"lower_case":`+objectSchema(`"dimension_name":`+fieldNameSchema, "dimension_name")+
			`,"upper_case":`+objectSchema(`"dimension_name":`+fieldNameSchema, "dimension_name")+
			`,"concatenate":`+objectSchema(`"dimension_names":`+arraySchema(fieldNameSchema, 1, 9)+
			`,"delimiter":{"type":"string","maxLength":20}`, "dimension_names")), "name"), 1, 9)
	metricsSchema = arraySchema(objectSchema(`"name":`+fieldNameSchema+`,"expression":{"type":"string",`+
		`"minLength":1,"maxLength":1000},"invisible":{"type":"boolean"}`, "name"), 1, 10)
)

const dateSchema = `{"type":"string","minLength":1,"maxLength":20,` +
	`"pattern":"^([0-9]{4}-[0-9]{2}-[0-9]{2}|today|yesterday|[0-9]{1,5}daysAgo)$"}`

var dateRangeSchema = objectSchema(`"start_date":`+dateSchema+`,"end_date":`+dateSchema+
	`,"name":{"type":"string","minLength":1,"maxLength":100}`, "start_date", "end_date")

// objectOf is a JSON object as the handlers read it.
type objectOf = map[string]any

// decodeObject reads the validated arguments with numbers kept as written.
func decodeObject(raw json.RawMessage) (objectOf, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out objectOf
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// presentOf counts the named members of an object.
func presentOf(m objectOf, keys ...string) int {
	n := 0
	for _, key := range keys {
		if _, ok := m[key]; ok {
			n++
		}
	}
	return n
}

// exactlyOne refuses an object that sets none or several of keys. The message names the members, never a value.
func exactlyOne(path string, m objectOf, keys ...string) error {
	if presentOf(m, keys...) != 1 {
		return badRequest(path + " must set exactly one of " + strings.Join(keys, ", "))
	}
	return nil
}

func asObject(v any) objectOf {
	m, _ := v.(map[string]any)
	return m
}

// checkFilterExpression checks what the schema cannot say: each expression and each filter sets exactly one
// of its alternatives, and a numeric value is either an integer or a double.
func checkFilterExpression(path string, v any) error {
	m := asObject(v)
	if err := exactlyOne(path, m, "filter", "and_group", "or_group", "not_expression"); err != nil {
		return err
	}
	switch {
	case m["filter"] != nil:
		filter := asObject(m["filter"])
		if err := exactlyOne(path+".filter", filter, "string_filter", "in_list_filter", "numeric_filter",
			"between_filter", "empty_filter"); err != nil {
			return err
		}
		for _, key := range []string{"numeric_filter.value", "between_filter.from_value", "between_filter.to_value"} {
			outer, inner, _ := strings.Cut(key, ".")
			if value := asObject(asObject(filter[outer])[inner]); value != nil {
				if err := exactlyOne(path+".filter."+key, value, "int64_value", "double_value"); err != nil {
					return err
				}
			}
		}
	case m["not_expression"] != nil:
		return checkFilterExpression(path+".not_expression", m["not_expression"])
	default:
		key := "and_group"
		if m["or_group"] != nil {
			key = "or_group"
		}
		list, _ := asObject(m[key])["expressions"].([]any)
		for i, item := range list {
			if err := checkFilterExpression(fmt.Sprintf("%s.%s.expressions[%d]", path, key, i), item); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkDimensions checks that a dimension sets at most one expression.
func checkDimensions(path string, v any) error {
	list, _ := v.([]any)
	for i, item := range list {
		if expression := asObject(asObject(item)["dimension_expression"]); expression != nil {
			if err := exactlyOne(fmt.Sprintf("%s[%d].dimension_expression", path, i), expression,
				"lower_case", "upper_case", "concatenate"); err != nil {
				return err
			}
		}
	}
	return nil
}

// camelKey turns a documented-name argument such as start_date into the member startDate.
func camelKey(key string) string {
	parts := strings.Split(key, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// camelize renames every member of a checked argument value to its documented name. A 64-bit integer goes
// as a string, as the API documents it. Values are never changed otherwise.
func camelize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, value := range x {
			name := camelKey(key)
			if number, ok := value.(json.Number); ok && name == "int64Value" {
				out[name] = number.String()
				continue
			}
			out[name] = camelize(value)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = camelize(value)
		}
		return out
	}
	return v
}
