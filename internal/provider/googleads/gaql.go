package googleads

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// The structured query of reports.search. Qatlas builds the GAQL string itself from arguments that were checked
// against the field catalog: every field must be known, fit the resource in FROM, and have the capability its
// role needs. Values are typed by the catalog and never pass through quoting or escaping, since a value with a
// quote, a backslash or a control character is refused.

const (
	maxReportFields    = 100
	maxConditions      = 50
	maxOrderings       = 10
	maxConditionValues = 1000
	maxValueRunes      = 1000
	maxFieldNameLength = 128
)

// fieldNamePattern is the form of a GAQL field or resource name in the catalog.
var fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,127}$`)

var (
	intPattern   = regexp.MustCompile(`^-?[0-9]{1,18}$`)
	floatPattern = regexp.MustCompile(`^-?[0-9]{1,18}(\.[0-9]{1,18})?$`)
	datePattern  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	dateTimeForm = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$`)
)

// operators are the operators of the GAQL grammar of a WHERE condition.
var operators = []string{"=", "!=", ">", ">=", "<", "<=", "IN", "NOT IN", "LIKE", "NOT LIKE", "REGEXP_MATCH",
	"NOT REGEXP_MATCH", "CONTAINS ANY", "CONTAINS ALL", "CONTAINS NONE", "IS NULL", "IS NOT NULL", "BETWEEN", "DURING"}

// dateFunctions are the date literals of the grammar that DURING takes.
var dateFunctions = []string{"LAST_14_DAYS", "LAST_30_DAYS", "LAST_7_DAYS", "LAST_BUSINESS_WEEK", "LAST_MONTH",
	"LAST_WEEK_MON_SUN", "LAST_WEEK_SUN_SAT", "THIS_MONTH", "THIS_WEEK_MON_TODAY", "THIS_WEEK_SUN_TODAY", "TODAY",
	"YESTERDAY"}

var directions = []string{"ASC", "DESC"}

// scalar is a JSON string, number or boolean of a condition value, kept as text.
type scalar struct {
	kind byte // 's' string, 'n' number, 'b' boolean
	text string
}

func (s *scalar) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) > 0 && raw[0] == '"':
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		*s = scalar{'s', text}
	case string(raw) == "true" || string(raw) == "false":
		*s = scalar{'b', string(raw)}
	case len(raw) > 0 && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') && json.Valid(raw):
		*s = scalar{'n', string(raw)}
	default:
		return fmt.Errorf("a value must be a string, a number or a boolean")
	}
	return nil
}

type reportCondition struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Value    *scalar  `json:"value"`
	Values   []scalar `json:"values"`
}

type reportOrdering struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

// withheldFields are the fields whose value is never returned and that therefore may not be selected, filtered
// or sorted either, since a filter would reveal the value: the account security of other people, credentials
// that give third parties access, and the webhook delivery of a lead form with its shared secret.
func withheld(name string) bool {
	switch name {
	case "customer_user_access.passkey_enabled", "third_party_app_analytics_link.shareable_link_id",
		"asset.lead_form_asset.delivery_methods":
		return true
	}
	if strings.HasPrefix(name, "asset.lead_form_asset.delivery_methods.") {
		return true
	}
	// The shared keys of the Android privacy resources have no documented kind and count as credentials.
	if strings.HasPrefix(name, "android_privacy_shared_key_google_") {
		_, field, _ := strings.Cut(name, ".")
		return strings.HasPrefix(field, "shared_") && strings.HasSuffix(field, "_key")
	}
	return false
}

// withheldKeys are keys of a message value (camel-case JSON names) that are dropped wherever they occur, for
// messages that hold a withheld field: the webhook of a lead form, the passkey flag of an access entry in a
// review, and the keys above.
var withheldKeys = map[string]bool{"passkeyEnabled": true, "shareableLinkId": true, "webhook": true,
	"googleSecret": true, "advertiserWebhookUrl": true, "sharedAdGroupKey": true, "sharedCampaignKey": true,
	"sharedNetworkTypeKey": true}

// managerOnly are the resources that list accounts other than the queried one. They are read only on a
// connection without targets or on a manager target, since a row does not say under which manager an account sits.
var managerOnly = map[string]bool{"customer_client": true, "customer_manager_link": true, "customer_client_link": true}

// fieldFor checks one field name of an argument: it must be in the catalog, fit the resource, not be withheld,
// and allow the role. The messages name the position and never the value.
func fieldFor(res Resource, argument string, i int, name, role string, allows func(Field) bool) (Field, error) {
	label := fmt.Sprintf("%s[%d]", argument, i)
	if !fieldNamePattern.MatchString(name) {
		return Field{}, badRequest(label + " is not a field name of the form resource.field")
	}
	f, ok := LookupField(name)
	if !ok {
		return Field{}, badRequest(label + " is not a field of the field catalog of " + catalogAPIVersion +
			"; google.ads.fields.search finds fields")
	}
	if res.Compat(name) != CompatOK {
		return Field{}, badRequest(label + " cannot be used together with the resource " + res.Name() +
			"; google.ads.fields.get on the resource names the fields that fit")
	}
	if withheld(name) {
		return Field{}, badRequest(label + " is a field whose value this provider never returns")
	}
	if !allows(f) {
		return Field{}, badRequest(label + " cannot be " + role)
	}
	return f, nil
}

// typeGroup is how the catalog data type of a field decides the literals and operators of a condition.
func typeGroup(f Field) string {
	switch f.DataType {
	case "ENUM":
		return "enum"
	case "STRING", "RESOURCE_NAME":
		return "text"
	case "DATE":
		return "date"
	case "INT32", "INT64":
		return "int"
	case "DOUBLE", "FLOAT":
		return "float"
	case "BOOLEAN":
		return "bool"
	}
	return ""
}

// operatorFits tells whether an operator may be used on a field.
func operatorFits(op string, f Field) bool {
	g := typeGroup(f)
	switch op {
	case "IS NULL", "IS NOT NULL":
		return true
	case "CONTAINS ANY", "CONTAINS ALL", "CONTAINS NONE":
		return f.Repeated && (g == "enum" || g == "text" || g == "int")
	}
	if f.Repeated {
		return false
	}
	switch op {
	case "=", "!=":
		return g != ""
	case "IN", "NOT IN":
		return g != "" && g != "bool"
	case ">", ">=", "<", "<=", "BETWEEN":
		return g == "int" || g == "float" || g == "date"
	case "LIKE", "NOT LIKE", "REGEXP_MATCH", "NOT REGEXP_MATCH":
		return f.DataType == "STRING"
	case "DURING":
		return g == "date"
	}
	return false
}

// literal turns one value into the GAQL literal that the catalog type of the field takes. Enum values must be
// listed by the catalog and never UNSPECIFIED or UNKNOWN, which are placeholders of the API.
func literal(label string, f Field, op string, s scalar) (string, error) {
	switch typeGroup(f) {
	case "enum":
		if s.kind != 's' || s.text == "UNSPECIFIED" || s.text == "UNKNOWN" || !slices.Contains(f.EnumValues, s.text) {
			return "", badRequest(label + " is not a value of the enum " + f.EnumType +
				" that a condition may name; google.ads.fields.get lists enum_values, UNSPECIFIED and UNKNOWN never apply")
		}
		return gaqlString(s.text), nil
	case "text":
		if s.kind != 's' {
			return "", badRequest(label + " must be a string")
		}
		if utf8.RuneCountInString(s.text) > maxValueRunes {
			return "", badRequest(label + " is longer than 1000 characters")
		}
		if err := checkGAQLText(label, s.text); err != nil {
			return "", err
		}
		if strings.HasSuffix(op, "REGEXP_MATCH") {
			if _, err := regexp.Compile(s.text); err != nil {
				return "", badRequest(label + " is not a valid RE2 regular expression")
			}
		}
		return gaqlString(s.text), nil
	case "date":
		if s.kind != 's' {
			return "", badRequest(label + " must be a date YYYY-MM-DD or a date and time YYYY-MM-DD HH:MM:SS")
		}
		layout := "2006-01-02"
		switch {
		case datePattern.MatchString(s.text):
		case dateTimeForm.MatchString(s.text):
			layout = "2006-01-02 15:04:05"
		default:
			return "", badRequest(label + " must be a date YYYY-MM-DD or a date and time YYYY-MM-DD HH:MM:SS")
		}
		if _, err := time.Parse(layout, s.text); err != nil {
			return "", badRequest(label + " is not a real date")
		}
		return gaqlString(s.text), nil
	case "int", "float":
		pattern := intPattern
		if typeGroup(f) == "float" {
			pattern = floatPattern
		}
		if s.kind == 'b' || !pattern.MatchString(s.text) {
			return "", badRequest(label + " must be a plain number without exponent")
		}
		return s.text, nil
	case "bool":
		if s.kind != 'b' {
			return "", badRequest(label + " must be true or false")
		}
		return strings.ToUpper(s.text), nil
	}
	return "", badRequest(label + " has a type that no condition can compare")
}

// build checks one condition against the catalog and the resource and returns its GAQL.
func (c reportCondition) build(res Resource, i int) (string, error) {
	f, err := fieldFor(res, "conditions", i, c.Field, "filtered", func(f Field) bool { return f.Filterable })
	if err != nil {
		return "", err
	}
	label := fmt.Sprintf("conditions[%d]", i)
	if !slices.Contains(operators, c.Operator) {
		return "", badRequest(label + ".operator is not an operator of the GAQL grammar")
	}
	if !operatorFits(c.Operator, f) {
		return "", badRequest(label + ".operator does not fit the type of the field " +
			"(google.ads.fields.get names data_type, is_repeated and enum_values)")
	}
	head := c.Field + " " + c.Operator
	switch c.Operator {
	case "IS NULL", "IS NOT NULL":
		if c.Value != nil || len(c.Values) > 0 {
			return "", badRequest(label + " takes no value with " + c.Operator)
		}
		return head, nil
	case "BETWEEN":
		if c.Value != nil || len(c.Values) != 2 {
			return "", badRequest(label + ".values must hold exactly two values with BETWEEN")
		}
		a, err := literal(label+".values[0]", f, c.Operator, c.Values[0])
		if err != nil {
			return "", err
		}
		b, err := literal(label+".values[1]", f, c.Operator, c.Values[1])
		if err != nil {
			return "", err
		}
		return head + " " + a + " AND " + b, nil
	case "IN", "NOT IN", "CONTAINS ANY", "CONTAINS ALL", "CONTAINS NONE":
		if c.Value != nil || len(c.Values) == 0 || len(c.Values) > maxConditionValues {
			return "", badRequest(label + ".values must hold 1 to 1000 values with " + c.Operator)
		}
		items := make([]string, len(c.Values))
		for k, v := range c.Values {
			if items[k], err = literal(fmt.Sprintf("%s.values[%d]", label, k), f, c.Operator, v); err != nil {
				return "", err
			}
		}
		return head + " (" + strings.Join(items, ", ") + ")", nil
	case "DURING":
		if c.Value == nil || len(c.Values) > 0 || c.Value.kind != 's' || !slices.Contains(dateFunctions, c.Value.text) {
			return "", badRequest(label + ".value must be one date function of the grammar, such as LAST_30_DAYS, with DURING")
		}
		return head + " " + c.Value.text, nil
	}
	if c.Value == nil || len(c.Values) > 0 {
		return "", badRequest(label + ".value must hold one value with " + c.Operator)
	}
	v, err := literal(label+".value", f, c.Operator, *c.Value)
	if err != nil {
		return "", err
	}
	return head + " " + v, nil
}

// reportQuery is a checked query: the GAQL string and the selected fields in the order asked for.
type reportQuery struct {
	gaql   string
	fields []string
	limit  int
}
