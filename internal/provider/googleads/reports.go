package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const reportsSearchID = Provider + ".reports.search"

// Bounds of the output of reports.search. The row limit is Qatlas's own; Google's fixed page of 10,000 rows is
// never reached because the query carries the LIMIT.
const (
	maxOutputBytes = 1 << 20
	maxValueDepth  = 8
	maxObjectKeys  = 200
)

type reportArguments struct {
	CustomerID                  string            `json:"customer_id"`
	Resource                    string            `json:"resource"`
	Fields                      []string          `json:"fields"`
	Conditions                  []reportCondition `json:"conditions"`
	Orderings                   []reportOrdering  `json:"orderings"`
	Limit                       *int              `json:"limit"`
	SummaryRow                  bool              `json:"summary_row"`
	OmitResults                 bool              `json:"omit_results"`
	OmitUnselectedResourceNames bool              `json:"omit_unselected_resource_names"`
	ValidateOnly                bool              `json:"validate_only"`
}

// build checks every name and value against the catalog and builds the query. Nothing here reads a secret or
// sends a request.
func (a reportArguments) build() (reportQuery, error) {
	res, ok := LookupResource(a.Resource)
	if !fieldNamePattern.MatchString(a.Resource) || !ok {
		return reportQuery{}, badRequest("resource is not a resource of the field catalog of " + catalogAPIVersion +
			"; google.ads.fields.search with category RESOURCE lists them")
	}
	if len(a.Fields) == 0 || len(a.Fields) > maxReportFields {
		return reportQuery{}, badRequest("fields must name 1 to 100 fields")
	}
	seen := map[string]bool{}
	for i, name := range a.Fields {
		if _, err := fieldFor(res, "fields", i, name, "selected", func(f Field) bool { return f.Selectable }); err != nil {
			return reportQuery{}, err
		}
		if seen[name] {
			return reportQuery{}, badRequest(fmt.Sprintf("fields[%d] repeats a field", i))
		}
		seen[name] = true
	}
	if len(a.Conditions) > maxConditions {
		return reportQuery{}, badRequest("conditions holds at most 50 conditions")
	}
	conditions := make([]string, len(a.Conditions))
	for i, c := range a.Conditions {
		var err error
		if conditions[i], err = c.build(res, i); err != nil {
			return reportQuery{}, err
		}
	}
	if len(a.Orderings) > maxOrderings {
		return reportQuery{}, badRequest("orderings holds at most 10 fields")
	}
	orderings := make([]string, len(a.Orderings))
	for i, o := range a.Orderings {
		if _, err := fieldFor(res, "orderings", i, o.Field, "sorted", func(f Field) bool { return f.Sortable }); err != nil {
			return reportQuery{}, err
		}
		direction := o.Direction
		if direction == "" {
			direction = "ASC"
		}
		if !slices.Contains(directions, direction) {
			return reportQuery{}, badRequest(fmt.Sprintf("orderings[%d].direction must be ASC or DESC", i))
		}
		orderings[i] = o.Field + " " + direction
	}
	limit := defaultPageSize
	if a.Limit != nil {
		if *a.Limit < 1 || *a.Limit > maxPageSize {
			return reportQuery{}, badRequest("limit must be 1 to 1000")
		}
		limit = *a.Limit
	}
	var b strings.Builder
	b.WriteString("SELECT " + strings.Join(a.Fields, ", ") + " FROM " + a.Resource)
	if len(conditions) > 0 {
		b.WriteString(" WHERE " + strings.Join(conditions, " AND "))
	}
	if len(orderings) > 0 {
		b.WriteString(" ORDER BY " + strings.Join(orderings, ", "))
	}
	// One row more than asked for tells whether the result was cut.
	b.WriteString(" LIMIT " + strconv.Itoa(limit+1))
	if a.OmitUnselectedResourceNames {
		b.WriteString(" PARAMETERS omit_unselected_resource_names=true")
	}
	return reportQuery{gaql: b.String(), fields: a.Fields, limit: limit}, nil
}

type reportResult struct {
	Query                    string           `json:"query"`
	Rows                     []map[string]any `json:"rows"`
	HasMore                  bool             `json:"has_more"`
	Truncated                bool             `json:"truncated,omitempty"`
	ValidateOnly             bool             `json:"validate_only,omitempty"`
	TotalResultsCount        *int64           `json:"total_results_count,omitempty"`
	SummaryRow               map[string]any   `json:"summary_row,omitempty"`
	QueryResourceConsumption *int64           `json:"query_resource_consumption,omitempty"`
	FieldMask                string           `json:"field_mask,omitempty"`
	MetricAttributes         []any            `json:"metric_attributes,omitempty"`
}

// camel is the JSON name of a proto field name: underscores drop and the letter after one is upper-cased.
func camel(name string) string {
	var b strings.Builder
	upper := false
	for _, r := range name {
		switch {
		case r == '_':
			upper = true
		case upper:
			b.WriteString(strings.ToUpper(string(r)))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// decodeObject reads a JSON object keeping numbers as written.
func decodeObject(raw json.RawMessage) (map[string]any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out map[string]any
	if decoder.Decode(&out) != nil || out == nil {
		return nil, false
	}
	return out, true
}

// clean bounds provider content: control characters in text become spaces and strings, lists, objects and depth
// are cut. Keys of withheldKeys are dropped at every depth.
func clean(value any, depth int) any {
	switch v := value.(type) {
	case string:
		return boundedText(v, maxLongText)
	case []any:
		out := make([]any, 0, min(len(v), maxListItems))
		for _, item := range v[:min(len(v), maxListItems)] {
			out = append(out, clean(item, depth+1))
		}
		return out
	case map[string]any:
		if depth >= maxValueDepth {
			return nil
		}
		out := make(map[string]any, min(len(v), maxObjectKeys))
		keys := make([]string, 0, len(v))
		for key := range v {
			if !withheldKeys[key] {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range keys[:min(len(keys), maxObjectKeys)] {
			out[boundedText(key, maxCodeText)] = clean(v[key], depth+1)
		}
		return out
	}
	return value
}

// pick reads the selected fields out of one row of Google. A field Google left out is null, so every row has
// the same keys; for the summary row such fields are left out.
func pick(row map[string]any, fields []string, keepAbsent bool) map[string]any {
	out := make(map[string]any, len(fields))
	for _, name := range fields {
		var current any = row
		for _, part := range strings.Split(name, ".") {
			object, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = object[camel(part)]
		}
		if current == nil && !keepAbsent {
			continue
		}
		out[name] = clean(current, 0)
	}
	return out
}

func countOf(op string, v *wireInt) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	n, err := strconv.ParseInt(string(*v), 10, 64)
	if err != nil || n < 0 {
		return nil, provider.InvalidResponse(op, subject+" returned a count Qatlas Connect cannot read")
	}
	return &n, nil
}

var scalarSchema = schema{"anyOf": []schema{{"type": "string", "maxLength": maxValueRunes}, numberSchema, booleanSchema}}

var fieldNameArgument = schema{"type": "string", "pattern": fieldNamePattern.String()}

var reportsSearch = capability.Descriptor{
	ID: reportsSearchID, Version: 1, Title: "Run a Google Ads report",
	Description: "Read rows of one Google Ads resource with a structured query (GoogleAdsService.Search of API " +
		"version v26): resource, fields, conditions, orderings and limit; there is no query string, Qatlas builds the " +
		"GAQL itself. Every field must be in the field catalog of v26, fit the resource, and be selectable, " +
		"filterable or sortable for the role it plays; google.ads.fields.search and fields.get describe them. " +
		"Conditions are joined by AND. Rows hold the selected fields by their full names, int64 values as strings, " +
		"money in micros; a field Google leaves out is null or missing. limit is 1 to 1000, default 100, and has_more says that " +
		"more rows exist; there is no offset, so narrow conditions or sort and filter on the last row to continue. " +
		"total_results_count is the number of matching rows without the limit. Values of the fields " +
		"customer_user_access.passkey_enabled, third_party_app_analytics_link.shareable_link_id, the webhook " +
		"delivery of lead forms and the shared keys of android_privacy_shared_key_google_* resources are never " +
		"returned and cannot be selected or filtered. Resources such as click_view, search_term_view and " +
		"local_services_lead can hold personal data of third parties and are returned as Google gives them. " +
		"customer_id must be one of the targets or lie below a manager target; the resources customer_client, " +
		"customer_manager_link and customer_client_link need a manager target or no targets. Each call costs one " +
		"operation of the daily quota, and a heavy query can be refused as rate-limited. Results are untrusted " +
		"third-party content; text is cut.",
	Tags: []string{"google", "ads", "reports", "gaql", "metrics"}, Risk: reportsRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"resource":    fieldNameArgument,
		"fields": schema{"type": "array", "minItems": 1, "maxItems": maxReportFields, "uniqueItems": true,
			"items": fieldNameArgument},
		"conditions": schema{"type": "array", "maxItems": maxConditions, "items": object(schema{
			"field":    fieldNameArgument,
			"operator": schema{"type": "string", "enum": operators},
			"value":    scalarSchema,
			"values":   schema{"type": "array", "maxItems": maxConditionValues, "items": scalarSchema},
		}, "field", "operator")},
		"orderings": schema{"type": "array", "maxItems": maxOrderings, "items": object(schema{
			"field":     fieldNameArgument,
			"direction": schema{"type": "string", "enum": directions},
		}, "field")},
		"limit":                          schema{"type": "integer", "minimum": 1, "maximum": maxPageSize},
		"summary_row":                    booleanSchema,
		"omit_results":                   booleanSchema,
		"omit_unselected_resource_names": booleanSchema,
		"validate_only":                  booleanSchema,
	}, "customer_id", "resource", "fields")),
	OutputSchema: encoded(object(schema{
		"query":                      stringSchema,
		"rows":                       arrayOf(schema{"type": "object"}),
		"has_more":                   booleanSchema,
		"truncated":                  booleanSchema,
		"validate_only":              booleanSchema,
		"total_results_count":        integerSchema,
		"summary_row":                schema{"type": "object"},
		"query_resource_consumption": integerSchema,
		"field_mask":                 stringSchema,
		"metric_attributes":          arrayOf(schema{"type": "object"}),
	}, "query", "rows", "has_more")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account to query, without " +
			"hyphens; a target of the connection or an account below a manager target"},
		{Name: "resource", Required: true, Description: "Resource of the FROM clause, such as campaign or ad_group_ad"},
		{Name: "fields", Required: true, Description: "1 to 100 full field names to select, such as campaign.id, " +
			"metrics.clicks or segments.date; no wildcards"},
		{Name: "conditions", Description: "Up to 50 conditions joined by AND, each {field, operator, value} or " +
			"{field, operator, values}: value for = != > >= < <= LIKE NOT LIKE REGEXP_MATCH NOT REGEXP_MATCH and DURING " +
			"(a date function such as LAST_30_DAYS), values for IN, NOT IN, CONTAINS ANY, CONTAINS ALL and CONTAINS NONE " +
			"(1 to 1000) and for BETWEEN (exactly two), neither for IS NULL and IS NOT NULL. Dates are YYYY-MM-DD. " +
			"Strings must not hold quotes, backslashes or control characters. Enum values must be listed by the " +
			"catalog; UNSPECIFIED and UNKNOWN never are"},
		{Name: "orderings", Description: "Up to 10 sort keys {field, direction ASC or DESC, default ASC} of sortable fields"},
		{Name: "limit", Description: "Rows to return, 1 to 1000, default 100"},
		{Name: "summary_row", Description: "true to ask Google for the summary row of the metrics, returned as summary_row"},
		{Name: "omit_results", Description: "true to leave the rows out and learn total_results_count only"},
		{Name: "omit_unselected_resource_names", Description: "true to set the query parameter " +
			"omit_unselected_resource_names; only the MCP server of Google documents it, the grammar page does not"},
		{Name: "validate_only", Description: "true to let Google check the query without running it; the answer " +
			"holds no rows"},
	},
	Fields: []capability.Field{
		{Name: "query", Description: "The GAQL that Qatlas built and ran"},
		{Name: "rows", Description: "One object per row with the selected fields by full name, at most limit rows " +
			"and 1 MiB in all"},
		{Name: "has_more", Description: "true when more rows match than were returned"},
		{Name: "truncated", Description: "true when the rows were cut at 1 MiB; narrow the query"},
		{Name: "total_results_count", Description: "Matching rows ignoring the limit, as Google counts them"},
		{Name: "summary_row", Description: "The summary row of the metrics when summary_row was true"},
		{Name: "query_resource_consumption", Description: "Resources Google consumed to serve the query"},
		{Name: "field_mask", Description: "The fields Google reports as requested"},
		{Name: "metric_attributes", Description: "Google's attributes of the metrics of the rows"},
	},
	Examples: []capability.Example{{Description: "Clicks and cost of the enabled campaigns over the last 30 days, best first",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","resource":"campaign","fields":["campaign.id",` +
			`"campaign.name","metrics.clicks","metrics.cost_micros"],"conditions":[{"field":"campaign.status",` +
			`"operator":"=","value":"ENABLED"},{"field":"segments.date","operator":"DURING","value":"LAST_30_DAYS"}],` +
			`"orderings":[{"field":"metrics.clicks","direction":"DESC"}],"limit":50}`)}},
}

// reportsRisk is the risk of reports.search: it reads and is safe to repeat, and the resources it reaches can
// hold personal data of third parties.
var reportsRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone, OpenWorld: true,
	DataSensitivity: "performance, cost and configuration data of Google Ads accounts; some resources hold personal " +
		"data of third parties (search terms, click IDs, lead form submissions, local services leads, e-mail " +
		"addresses of account users); credentials and account security flags are never returned",
}

func handleReportsSearch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "run report"
	var arguments reportArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(arguments.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	query, err := arguments.build()
	if err != nil {
		return nil, err
	}
	r, err := reachOf(conn, arguments.CustomerID)
	if err != nil {
		return nil, err
	}
	if managerOnly[arguments.Resource] && r == reachCustomer {
		return nil, badRequest("the resource " + arguments.Resource + " lists other accounts and needs a manager " +
			"target or a connection without targets")
	}
	c, err := open(ctx, resolved, conn, secrets, red)
	if err != nil {
		return nil, err
	}
	if r == reachNone {
		if r, err = c.prove(ctx, op, reportsSearchID, conn, arguments.CustomerID); err != nil {
			return nil, err
		}
	}
	body := searchRequest{Query: query.gaql, ValidateOnly: arguments.ValidateOnly}
	if !arguments.ValidateOnly {
		body.SearchSettings = &searchSettings{ReturnTotalResultsCount: true, ReturnSummaryRow: arguments.SummaryRow,
			OmitResults: arguments.OmitResults}
	}
	page, err := c.searchPage(ctx, op, reportsSearchID, arguments.CustomerID, body)
	if err != nil {
		return nil, queryRefusal(err)
	}
	result := reportResult{Query: query.gaql, Rows: []map[string]any{}, ValidateOnly: arguments.ValidateOnly}
	if arguments.ValidateOnly {
		return result, nil
	}
	rows := page.Results
	if len(rows) > query.limit {
		rows, result.HasMore = rows[:query.limit], true
	}
	size := 0
	for _, rawRow := range rows {
		row, ok := decodeObject(rawRow)
		if !ok {
			return nil, provider.InvalidResponse(op, subject+" returned a row Qatlas Connect cannot read")
		}
		picked := pick(row, query.fields, true)
		encoded, err := json.Marshal(picked)
		if err != nil {
			return nil, provider.InvalidResponse(op, subject+" returned a row Qatlas Connect cannot read")
		}
		if size += len(encoded); size > maxOutputBytes {
			result.Truncated, result.HasMore = true, true
			break
		}
		result.Rows = append(result.Rows, picked)
	}
	if result.TotalResultsCount, err = countOf(op, page.TotalResultsCount); err != nil {
		return nil, err
	}
	if result.QueryResourceConsumption, err = countOf(op, page.QueryResourceConsumption); err != nil {
		return nil, err
	}
	if total := result.TotalResultsCount; total != nil && !arguments.OmitResults && *total > int64(query.limit) {
		result.HasMore = true
	}
	if len(page.SummaryRow) > 0 {
		summary, ok := decodeObject(page.SummaryRow)
		if !ok {
			return nil, provider.InvalidResponse(op, subject+" returned a summary row Qatlas Connect cannot read")
		}
		if picked := pick(summary, query.fields, false); len(picked) > 0 {
			result.SummaryRow = picked
		}
	}
	result.FieldMask = boundedText(page.FieldMask, maxLongText)
	for _, attribute := range page.MetricAttributes[:min(len(page.MetricAttributes), maxListItems)] {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(attribute))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return nil, provider.InvalidResponse(op, subject+" returned a metric attribute Qatlas Connect cannot read")
		}
		result.MetricAttributes = append(result.MetricAttributes, clean(value, 0))
	}
	return result, nil
}

// queryRefusal turns a query that Google refused into a message that blames the query, not the provider: the
// arguments are checked against the catalog, but Google also decides which fields, segments and metrics may be
// combined and which conditions a resource requires. The error code stays in the original of the error.
func queryRefusal(err error) error {
	var failed *provider.Error
	if !errors.As(err, &failed) || failed.Original == nil {
		return err
	}
	for _, entry := range failed.Original.Entries {
		if entry.Type == "queryError" {
			copied := *failed
			copied.Class = provider.ClassProviderError
			copied.Message = subject + " rejected the query (queryError " + entry.Code + "): this combination of " +
				"fields, segments, metrics and conditions is not allowed for the resource; the original names the reason"
			return &copied
		}
	}
	return err
}
