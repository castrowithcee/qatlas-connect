package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const reportsSearchID = Provider + ".reports.search"

// reportsAPI is the path prefix of the reports sub-API.
const reportsAPI = "reports/v1/"

// Bounds of one report call. The API allows up to 100000 rows per page; a result of this tool stays far below.
const (
	defaultReportLimit = 100
	maxReportLimit     = 1000
	maxReportFields    = 40
	maxConditions      = 10
	maxConditionValues = 20
	maxOrderBy         = 5
	maxValueRunes      = 200
	maxPatternRunes    = 100
	maxNestingDepth    = 6
	maxObjectMembers   = 50
)

var dateWindows = []string{
	"LAST_7_DAYS", "LAST_14_DAYS", "LAST_30_DAYS", "LAST_BUSINESS_WEEK", "LAST_WEEK_MON_SUN", "LAST_WEEK_SUN_SAT",
	"LAST_MONTH", "THIS_MONTH", "THIS_WEEK_MON_TODAY", "THIS_WEEK_SUN_TODAY", "TODAY", "YESTERDAY",
}

var reportOperators = []string{
	"=", "!=", "<>", ">", ">=", "<", "<=", "IN", "NOT IN", "CONTAINS ANY", "CONTAINS ALL", "CONTAINS NONE", "LIKE",
	"NOT LIKE", "REGEXP_MATCH", "NOT REGEXP_MATCH", "BETWEEN",
}

// operatorsOf are the operators the documentation allows for the values of a kind of field. DURING belongs to
// date_range.
var operatorsOf = map[fieldKind][]string{
	kindText:     {"=", "!=", "<>", "IN", "NOT IN", "LIKE", "NOT LIKE", "REGEXP_MATCH", "NOT REGEXP_MATCH"},
	kindEnum:     {"=", "!=", "<>", "IN", "NOT IN"},
	kindInt:      {"=", "!=", "<>", ">", ">=", "<", "<=", "IN", "NOT IN", "BETWEEN"},
	kindNumber:   {"=", "!=", "<>", ">", ">=", "<", "<=", "IN", "NOT IN", "BETWEEN"},
	kindDate:     {"=", ">", ">=", "<", "<=", "BETWEEN"},
	kindTextList: {"CONTAINS ANY", "CONTAINS ALL", "CONTAINS NONE"},
}

var (
	enumValue   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	intValue    = regexp.MustCompile(`^-?[0-9]{1,18}$`)
	numberValue = regexp.MustCompile(`^-?[0-9]{1,15}(\.[0-9]{1,10})?$`)
)

type reportArguments struct {
	Table      string            `json:"table"`
	Fields     []string          `json:"fields"`
	Conditions []reportCondition `json:"conditions"`
	DateRange  *reportDateRange  `json:"date_range"`
	OrderBy    []reportOrder     `json:"order_by"`
	Limit      int               `json:"limit"`
	Cursor     string            `json:"cursor"`
}

type reportCondition struct {
	Field    string            `json:"field"`
	Operator string            `json:"operator"`
	Values   []json.RawMessage `json:"values"`
}

type reportDateRange struct {
	Window string `json:"window"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

type reportOrder struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

// builtReport is a checked query and the table it reads.
type builtReport struct {
	query string
	table reportTable
}

// isoDate checks a calendar date in the form YYYY-MM-DD.
func isoDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil && len(s) == len(time.DateOnly)
}

// quoteText checks a text value and quotes it. The query language documents no escape rule for a string, so
// a quote, a control character, or a backslash at the end, which would escape the closing quote, is refused.
func quoteText(value string, limit int) (string, error) {
	switch {
	case value == "" || utf8.RuneCountInString(value) > limit || !utf8.ValidString(value):
		return "", invalidArgument("a text value must hold 1 to " + strconv.Itoa(limit) + " characters")
	case strings.ContainsAny(value, `'"`) || strings.HasSuffix(value, `\`):
		return "", invalidArgument("a text value must not contain quotes or end with a backslash")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", invalidArgument("a text value must not contain control characters")
		}
	}
	return "'" + value + "'", nil
}

// literal renders one value of a condition for the kind of its field.
func literal(f reportField, op string, raw json.RawMessage) (string, error) {
	var text string
	var asString bool
	if json.Unmarshal(raw, &text) == nil {
		asString = true
	} else {
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return "", invalidArgument("a value of " + f.name + " must be a string or a number")
		}
		text = number.String()
	}
	switch f.kind {
	case kindInt:
		if !intValue.MatchString(text) {
			return "", invalidArgument("a value of " + f.name + " must be an integer")
		}
		return text, nil
	case kindNumber:
		if !numberValue.MatchString(text) {
			return "", invalidArgument("a value of " + f.name + " must be a decimal number")
		}
		return text, nil
	case kindDate:
		if !asString || !isoDate(text) {
			return "", invalidArgument("a value of " + f.name + " must be a date as YYYY-MM-DD")
		}
		return "'" + text + "'", nil
	case kindEnum:
		if !asString || !enumValue.MatchString(text) {
			return "", invalidArgument("a value of " + f.name + " must be an enumeration value in upper case")
		}
		return "'" + text + "'", nil
	}
	if !asString {
		return "", invalidArgument("a value of " + f.name + " must be a string")
	}
	if strings.Contains(op, "REGEXP_MATCH") {
		if _, err := regexp.Compile(text); err != nil {
			return "", invalidArgument("a value of " + f.name + " is not a valid RE2 pattern")
		}
		return quoteText(text, maxPatternRunes)
	}
	return quoteText(text, maxValueRunes)
}

func conditionText(f reportField, c reportCondition) (string, error) {
	if !slices.Contains(operatorsOf[f.kind], c.Operator) {
		return "", invalidArgument("operator " + c.Operator + " does not apply to " + f.name)
	}
	var count int
	switch c.Operator {
	case "BETWEEN":
		if len(c.Values) != 2 {
			return "", invalidArgument("BETWEEN takes exactly two values")
		}
		count = 2
	case "IN", "NOT IN", "CONTAINS ANY", "CONTAINS ALL", "CONTAINS NONE":
		if len(c.Values) < 1 || len(c.Values) > maxConditionValues {
			return "", invalidArgument(c.Operator + " takes 1 to " + strconv.Itoa(maxConditionValues) + " values")
		}
		count = len(c.Values)
	default:
		if len(c.Values) != 1 {
			return "", invalidArgument("operator " + c.Operator + " takes exactly one value")
		}
		count = 1
	}
	values := make([]string, 0, count)
	for _, raw := range c.Values {
		v, err := literal(f, c.Operator, raw)
		if err != nil {
			return "", err
		}
		values = append(values, v)
	}
	switch c.Operator {
	case "BETWEEN":
		return f.name + " BETWEEN " + values[0] + " AND " + values[1], nil
	case "IN", "NOT IN", "CONTAINS ANY", "CONTAINS ALL", "CONTAINS NONE":
		return f.name + " " + c.Operator + " (" + strings.Join(values, ", ") + ")", nil
	}
	return f.name + " " + c.Operator + " " + values[0], nil
}

func dateRangeText(r *reportDateRange) (string, error) {
	switch {
	case r.Window != "" && (r.Start != "" || r.End != ""):
		return "", invalidArgument("date_range takes either window or start and end")
	case r.Window != "":
		if !slices.Contains(dateWindows, r.Window) {
			return "", invalidArgument("date_range window is not one of the documented date functions")
		}
		return "date DURING " + r.Window, nil
	case !isoDate(r.Start) || !isoDate(r.End) || r.Start > r.End:
		return "", invalidArgument("date_range needs a window, or start and end as YYYY-MM-DD with start not after end")
	}
	return "date BETWEEN '" + r.Start + "' AND '" + r.End + "'", nil
}

// buildReport checks the arguments against the field catalog and composes the query from catalog names and
// quoted values. Nothing the agent wrote reaches the query unchecked.
func buildReport(a reportArguments) (builtReport, error) {
	table, ok := reportTableNamed(a.Table)
	if !ok {
		return builtReport{}, invalidArgument("table is not a table of the field catalog")
	}
	if len(a.Fields) == 0 || len(a.Fields) > maxReportFields || len(a.Conditions) > maxConditions ||
		len(a.OrderBy) > maxOrderBy {
		return builtReport{}, invalidArgument("fields, conditions, or order_by exceed the bounds of the tool")
	}
	var selected []string
	for _, name := range a.Fields {
		f, ok := table.field(name)
		if !ok || f.whereOnly || f.noSelect || slices.Contains(selected, name) {
			return builtReport{}, invalidArgument("field " + strconv.Quote(name) + " is not a selectable field of " +
				table.name + " or is listed twice")
		}
		selected = append(selected, name)
	}
	var required []string
	for _, f := range table.fields {
		if f.inSelect && !slices.Contains(selected, f.name) {
			required = append(required, f.name)
		}
	}
	selected = append(required, selected...)
	if table.performance {
		metric := false
		for _, name := range selected {
			if f, _ := table.field(name); f.metric {
				metric = true
			}
		}
		if !metric {
			return builtReport{}, invalidArgument(table.name + " needs at least one metric among the fields")
		}
	}

	var where []string
	filtered := map[string]bool{}
	if a.DateRange != nil {
		if _, ok := table.field("date"); !ok {
			return builtReport{}, invalidArgument(table.name + " has no date field, so date_range does not apply")
		}
		part, err := dateRangeText(a.DateRange)
		if err != nil {
			return builtReport{}, err
		}
		where, filtered["date"] = append(where, part), true
	}
	for _, c := range a.Conditions {
		f, ok := table.field(c.Field)
		switch {
		case !ok:
			return builtReport{}, invalidArgument("condition field " + strconv.Quote(c.Field) + " is not a field of " + table.name)
		case c.Field == "date":
			return builtReport{}, invalidArgument("filter the date with date_range")
		case f.noFilter:
			return builtReport{}, invalidArgument("the documentation does not allow a condition on " + f.name)
		case filtered[c.Field]:
			return builtReport{}, invalidArgument("at most one condition per field: " + f.name)
		case f.metric && !slices.Contains(selected, f.name):
			return builtReport{}, invalidArgument("a condition on the metric " + f.name + " needs it among the fields")
		}
		part, err := conditionText(f, c)
		if err != nil {
			return builtReport{}, err
		}
		where, filtered[c.Field] = append(where, part), true
	}
	for _, f := range table.fields {
		if f.inWhere && !filtered[f.name] {
			if f.name == "date" {
				return builtReport{}, invalidArgument(table.name + " needs date_range")
			}
			return builtReport{}, invalidArgument(table.name + " needs a condition on " + f.name)
		}
	}

	query := "SELECT " + strings.Join(selected, ", ") + " FROM " + table.name
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	var order []string
	for _, o := range a.OrderBy {
		f, ok := table.field(o.Field)
		switch {
		case !ok || !slices.Contains(selected, o.Field) || slices.ContainsFunc(order, func(s string) bool {
			return strings.HasPrefix(s, o.Field+" ")
		}):
			return builtReport{}, invalidArgument("order_by field " + strconv.Quote(o.Field) + " must be one of the selected fields, once")
		case f.noSort || f.kind == kindPrice || f.kind == kindTextList || f.kind == kindObjects:
			return builtReport{}, invalidArgument("the documentation does not allow ordering by " + f.name)
		case o.Direction != "" && o.Direction != "ASC" && o.Direction != "DESC":
			return builtReport{}, invalidArgument("direction is ASC or DESC")
		}
		direction := o.Direction
		if direction == "" {
			direction = "ASC"
		}
		order = append(order, f.name+" "+direction)
	}
	if len(order) > 0 {
		query += " ORDER BY " + strings.Join(order, ", ")
	}
	return builtReport{query: query, table: table}, nil
}

type reportBody struct {
	Query     string `json:"query"`
	PageSize  int    `json:"pageSize"`
	PageToken string `json:"pageToken,omitempty"`
}

type reportsAnswer struct {
	Results       []map[string]json.RawMessage `json:"results"`
	NextPageToken string                       `json:"nextPageToken"`
}

// reportsResult is the result of reports.search. A result is never null, and has_more says whether Google
// named a next page: a full page alone proves nothing.
type reportsResult struct {
	Table      string           `json:"table"`
	Rows       []map[string]any `json:"rows"`
	HasMore    bool             `json:"has_more"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func handleReportsSearch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("search report", reportsSearchID, resolved, raw)
	if err != nil {
		return nil, err
	}
	id, needsProof, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	var a reportArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if a.Limit == 0 {
		a.Limit = defaultReportLimit
	}
	if a.Limit < 1 || a.Limit > maxReportLimit {
		return nil, invalidArgument("limit must be 1 to " + strconv.Itoa(maxReportLimit))
	}
	built, err := buildReport(a)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(c.tool, id, built.query, c.scope.key())
	body := reportBody{Query: built.query, PageSize: a.Limit}
	if a.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, a.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		body.PageToken = token
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(c.op, "the request could not be built")
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if needsProof {
		if err := cl.prove(ctx, c.op, c.tool, id); err != nil {
			return nil, err
		}
	}
	var answer reportsAnswer
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPost,
		path: reportsAPI + "accounts/" + id + "/reports:search", body: encoded}, &answer); err != nil {
		return nil, err
	}
	if len(answer.Results) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := reportsResult{Table: built.table.name, Rows: make([]map[string]any, 0, len(answer.Results)),
		HasMore: answer.NextPageToken != ""}
	for _, row := range answer.Results {
		out.Rows = append(out.Rows, built.table.row(row[built.table.key]))
	}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

// row keeps the members of a view that the catalog lists, each cut and typed like its field. What is not in
// the catalog or has another type is dropped.
func (t reportTable) row(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return out
	}
	for _, f := range t.fields {
		if f.whereOnly {
			continue
		}
		if value, ok := f.value(members[f.jsonName()]); ok {
			out[f.jsonName()] = value
		}
	}
	return out
}

func (f reportField) value(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	switch f.kind {
	case kindText:
		limit := maxNameRunes
		if f.name == "thumbnail_link" {
			limit = maxURLRunes
		}
		return textOf(raw, limit)
	case kindEnum, kindTimestamp:
		return textOf(raw, maxIDRunes)
	case kindInt:
		text, ok := textOf(raw, 24)
		if s, _ := text.(string); !ok || !intValue.MatchString(s) {
			return nil, false
		}
		return text, true
	case kindNumber:
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return nil, false
		}
		return number, true
	case kindBool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return nil, false
		}
		return b, true
	}
	return tree(raw, 0)
}

func textOf(raw json.RawMessage, limit int) (any, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil, false
	}
	return clipJSON(raw, limit), true
}

// tree keeps a nested value of an answer with bounded depth, members, and entries; every text is cut.
func tree(raw json.RawMessage, depth int) (any, bool) {
	if depth > maxNestingDepth {
		return nil, false
	}
	var parsed any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if d.Decode(&parsed) != nil {
		return nil, false
	}
	switch v := parsed.(type) {
	case string:
		return clipJSON(raw, maxNameRunes), true
	case json.Number, bool:
		return v, true
	case []any:
		var items []json.RawMessage
		_ = json.Unmarshal(raw, &items)
		out := []any{}
		for _, item := range items {
			if len(out) == maxListItems {
				break
			}
			if kept, ok := tree(item, depth+1); ok {
				out = append(out, kept)
			}
		}
		return out, true
	case map[string]any:
		var members map[string]json.RawMessage
		_ = json.Unmarshal(raw, &members)
		out := map[string]any{}
		for _, name := range sortedKeys(members) {
			if len(out) == maxObjectMembers {
				break
			}
			if kept, ok := tree(members[name], depth+1); ok {
				key, _ := json.Marshal(name)
				out[clipJSON(key, maxIDRunes)] = kept
			}
		}
		return out, true
	}
	return nil, false
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

var reportsSearch = capability.Descriptor{
	ID: reportsSearchID, Version: 1, Title: "Search a Merchant Center report",
	Description: "Read one report of an account with a structured query. Qatlas builds the Merchant Center Query " +
		"Language query itself from table, fields, conditions, date_range, and order_by, all checked against a " +
		"fixed field catalog of the ten report tables; a name or operator outside the catalog is refused, and " +
		"there is no free query text. The tool adds the fields the table needs in every query (such as id) and " +
		"refuses a query that lacks what the table requires: a performance table needs a metric and date_range, " +
		"the best sellers tables a report_granularity and a report_country_code condition, the competitive " +
		"visibility tables date_range and conditions on report_country_code and report_category_id. At most one " +
		"condition per field. limit is 1 to 1000 rows per call, 100 when left out; has_more and next_cursor say " +
		"that Google named a next page, and a full page alone does not prove more rows. Rows follow the schema " +
		"of the table, and the account needs the Merchant Center role Performance and insights or Admin; Google " +
		"does not offer every table to every account, and refuses with permission otherwise. Every call counts " +
		"against the quota of the account. Domains of the competitive tables are market data of third parties. " +
		untrustedReportNote,
	Tags: []string{"google", "merchant", "reports"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"table", "fields"}, member("account", accountArgSchema),
		member("table", enumSchema(reportTableNames())),
		member("fields", `{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxReportFields)+
			`,"uniqueItems":true,"items":`+enumSchema(allReportFieldNames())+`}`),
		member("conditions", `{"type":"array","maxItems":`+strconv.Itoa(maxConditions)+`,"items":{"type":"object",`+
			`"properties":{"field":`+enumSchema(allReportFieldNames())+`,"operator":`+enumSchema(reportOperators)+
			`,"values":{"type":"array","minItems":1,"maxItems":`+strconv.Itoa(maxConditionValues)+
			`,"items":{"anyOf":[{"type":"string","minLength":1,"maxLength":`+strconv.Itoa(maxValueRunes)+
			`},{"type":"number"}]}}},"required":["field","operator","values"],"additionalProperties":false}}`),
		member("date_range", `{"type":"object","properties":{"window":`+enumSchema(dateWindows)+
			`,"start":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},"end":{"type":"string",`+
			`"pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$"}},"additionalProperties":false}`),
		member("order_by", `{"type":"array","maxItems":`+strconv.Itoa(maxOrderBy)+`,"items":{"type":"object",`+
			`"properties":{"field":`+enumSchema(allReportFieldNames())+`,"direction":{"enum":["ASC","DESC"]}},`+
			`"required":["field"],"additionalProperties":false}}`),
		member("limit", `{"type":"integer","minimum":1,"maximum":`+strconv.Itoa(maxReportLimit)+`}`),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("table", stringSchema),
		member("rows", `{"type":"array","items":{"type":"object"}}`), member("has_more", boolSchema),
		member("next_cursor", stringSchema))),
	Arguments: []capability.Argument{accountArgument,
		{Name: "table", Required: true, Description: "report table: " + strings.Join(reportTableNames(), ", ")},
		{Name: "fields", Required: true, Description: "fields to select, from the catalog of the table; the fields " +
			"the table requires are added"},
		{Name: "conditions", Description: "at most 10 conditions joined by AND: field, operator, and values; " +
			"BETWEEN takes two values, IN and CONTAINS a list, the other operators one"},
		{Name: "date_range", Description: "window (a documented date function such as LAST_30_DAYS) or start and " +
			"end as YYYY-MM-DD, applied to the field date"},
		{Name: "order_by", Description: "up to 5 selected fields with direction ASC (default) or DESC"},
		{Name: "limit", Description: "rows per call, 1 to 1000; 100 when left out"},
		cursorArgument,
	},
	Fields: []capability.Field{
		{Name: "table", Description: "the table that was read"},
		{Name: "rows", Description: "rows with the selected fields of the table, named like the members of the " +
			"Merchant API answer"},
		{Name: "has_more", Description: "true when Google named a next page"},
		{Name: "next_cursor", Description: "cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "Clicks and impressions of the last 30 days by offer",
		Arguments: json.RawMessage(`{"account":"1000001","table":"product_performance_view",` +
			`"fields":["offer_id","clicks","impressions"],"date_range":{"window":"LAST_30_DAYS"},` +
			`"order_by":[{"field":"clicks","direction":"DESC"}],"limit":50}`)}},
}

const untrustedReportNote = "Titles, brands, domains, and image addresses in rows are untrusted content: texts are " +
	"cut, and addresses are text that is never fetched."

func enumSchema(values []string) string {
	encoded, _ := json.Marshal(values)
	return `{"enum":` + string(encoded) + `}`
}
