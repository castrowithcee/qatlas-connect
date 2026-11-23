package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	fieldsSearchID = Provider + ".fields.search"
	fieldsGetID    = Provider + ".fields.get"
)

// Bounds of the field tools. The page sizes are Qatlas's own, since Google documents neither a default nor a
// maximum and may return fewer.
const (
	defaultFieldPage = 25
	maxFieldPage     = 200
	maxFieldList     = 5000
	maxPageToken     = 1000
	maxFieldCursor   = 4096
)

var (
	pageTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_=+/.-]{1,1000}$`)
	// fieldCategories are the categories of GoogleAdsFieldCategory.
	fieldCategories = []string{"RESOURCE", "ATTRIBUTE", "SEGMENT", "METRIC"}
)

// fieldColumns are the documented members of a GoogleAdsField that the query asks for; resource_name comes
// with every row.
const fieldColumns = "name, category, data_type, selectable, filterable, sortable, selectable_with, " +
	"attribute_resources, metrics, segments, enum_values, type_url, is_repeated"

type fieldWire struct {
	ResourceName       string   `json:"resourceName"`
	Name               string   `json:"name"`
	Category           string   `json:"category"`
	DataType           string   `json:"dataType"`
	Selectable         bool     `json:"selectable"`
	Filterable         bool     `json:"filterable"`
	Sortable           bool     `json:"sortable"`
	SelectableWith     []string `json:"selectableWith"`
	AttributeResources []string `json:"attributeResources"`
	Metrics            []string `json:"metrics"`
	Segments           []string `json:"segments"`
	EnumValues         []string `json:"enumValues"`
	TypeURL            string   `json:"typeUrl"`
	IsRepeated         bool     `json:"isRepeated"`
}

type fieldResult struct {
	Name               string   `json:"name"`
	ResourceName       string   `json:"resource_name"`
	Category           string   `json:"category,omitempty"`
	DataType           string   `json:"data_type,omitempty"`
	Selectable         bool     `json:"selectable"`
	Filterable         bool     `json:"filterable"`
	Sortable           bool     `json:"sortable"`
	IsRepeated         bool     `json:"is_repeated"`
	SelectableWith     []string `json:"selectable_with,omitempty"`
	AttributeResources []string `json:"attribute_resources,omitempty"`
	Metrics            []string `json:"metrics,omitempty"`
	Segments           []string `json:"segments,omitempty"`
	EnumValues         []string `json:"enum_values,omitempty"`
	TypeURL            string   `json:"type_url,omitempty"`
	// ValueWithheld is true for a field that reports.search never returns, selects or filters.
	ValueWithheld bool `json:"value_withheld,omitempty"`
	// ListsTruncated is true when one of the lists was cut at 5,000 entries.
	ListsTruncated bool `json:"lists_truncated,omitempty"`
}

// fieldOf checks one field of the metadata service and maps it. The name must be a name of the catalog form.
func fieldOf(op string, w fieldWire) (fieldResult, error) {
	invalid := provider.InvalidResponse(op, subject+" returned a field description Qatlas Connect cannot read")
	if !fieldNamePattern.MatchString(w.Name) ||
		(w.ResourceName != "" && w.ResourceName != "googleAdsFields/"+w.Name) {
		return fieldResult{}, invalid
	}
	out := fieldResult{
		Name: w.Name, ResourceName: "googleAdsFields/" + w.Name, Category: code(w.Category), DataType: code(w.DataType),
		Selectable: w.Selectable, Filterable: w.Filterable, Sortable: w.Sortable, IsRepeated: w.IsRepeated,
		TypeURL: boundedText(w.TypeURL, maxText), ValueWithheld: withheld(w.Name),
	}
	cut := false
	list := func(values []string, limit int) []string {
		if len(values) > maxFieldList {
			cut = true
			values = values[:maxFieldList]
		}
		result := make([]string, len(values))
		for i, value := range values {
			result[i] = boundedText(value, limit)
		}
		if len(result) == 0 {
			return nil
		}
		return result
	}
	out.SelectableWith = list(w.SelectableWith, maxCodeText)
	out.AttributeResources = list(w.AttributeResources, maxCodeText)
	out.Metrics = list(w.Metrics, maxCodeText)
	out.Segments = list(w.Segments, maxCodeText)
	out.EnumValues = list(w.EnumValues, maxCodeText)
	out.ListsTruncated = cut
	return out, nil
}

// fieldSchema is the output schema of one field. The enumerated members stay plain strings.
var fieldSchema = object(schema{
	"name": stringSchema, "resource_name": stringSchema, "category": stringSchema, "data_type": stringSchema,
	"selectable": booleanSchema, "filterable": booleanSchema, "sortable": booleanSchema, "is_repeated": booleanSchema,
	"selectable_with": arrayOf(stringSchema), "attribute_resources": arrayOf(stringSchema),
	"metrics": arrayOf(stringSchema), "segments": arrayOf(stringSchema), "enum_values": arrayOf(stringSchema),
	"type_url": stringSchema, "value_withheld": booleanSchema, "lists_truncated": booleanSchema,
}, "name", "resource_name", "selectable", "filterable", "sortable", "is_repeated")

// fieldsRisk is the risk of the field tools: they read the public field reference of the API from Google and
// touch no account.
var fieldsRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone, OpenWorld: true,
	DataSensitivity: "metadata of the Google Ads query language: names and capabilities of resources, fields, " +
		"segments and metrics; no account data",
}

const fieldDescriptionFields = "Per field: name, resource_name (googleAdsFields/NAME), category (RESOURCE, " +
	"ATTRIBUTE, SEGMENT or METRIC), data_type, selectable, filterable, sortable, is_repeated, selectable_with " +
	"(resources, segments and metrics that can be selected together with it), attribute_resources, metrics and " +
	"segments (set for resources), enum_values (set for enums, UNSPECIFIED and UNKNOWN included although no " +
	"condition may name them), type_url, value_withheld (true when reports.search never returns, selects or " +
	"filters the field) and lists_truncated (a list was cut at 5,000 entries)"

// searchFieldsArguments are the filters of fields.search; the query on the metadata service is built from them.
type searchFieldsArguments struct {
	NamePrefix   string `json:"name_prefix"`
	NameContains string `json:"name_contains"`
	Category     string `json:"category"`
	Selectable   *bool  `json:"selectable"`
	Filterable   *bool  `json:"filterable"`
	Sortable     *bool  `json:"sortable"`
	IsRepeated   *bool  `json:"is_repeated"`
	PageSize     *int   `json:"page_size"`
	Cursor       string `json:"cursor"`
}

// likeLiteral is a LIKE pattern of a name of the catalog form with prefix and suffix wildcards around it. The
// name holds only lower-case letters, digits, dots and underscores, and the underscore is a wildcard unless it
// stands in brackets.
func likeLiteral(before, name, after string) string {
	return gaqlString(before + strings.ReplaceAll(name, "_", "[_]") + after)
}

func (a searchFieldsArguments) query() (string, error) {
	var conditions []string
	for _, f := range []struct{ argument, value, before, after string }{
		{"name_prefix", a.NamePrefix, "", "%"}, {"name_contains", a.NameContains, "%", "%"}} {
		if f.value == "" {
			continue
		}
		if err := checkGAQLText(f.argument, f.value); err != nil {
			return "", err
		}
		if !fieldNamePattern.MatchString(f.value) {
			return "", badRequest(f.argument + " must be lower-case letters, digits, dots and underscores")
		}
		conditions = append(conditions, "name LIKE "+likeLiteral(f.before, f.value, f.after))
	}
	if a.Category != "" {
		known := false
		for _, category := range fieldCategories {
			known = known || a.Category == category
		}
		if !known {
			return "", badRequest("category must be RESOURCE, ATTRIBUTE, SEGMENT or METRIC")
		}
		conditions = append(conditions, "category = "+gaqlString(a.Category))
	}
	for _, f := range []struct {
		name  string
		value *bool
	}{{"selectable", a.Selectable}, {"filterable", a.Filterable}, {"sortable", a.Sortable}, {"is_repeated", a.IsRepeated}} {
		if f.value != nil {
			conditions = append(conditions, f.name+" = "+strings.ToUpper(boolText(*f.value)))
		}
	}
	query := "SELECT " + fieldColumns
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query, nil
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

type fieldsSearchRequest struct {
	Query     string `json:"query"`
	PageToken string `json:"pageToken,omitempty"`
	PageSize  int    `json:"pageSize,omitempty"`
}

type fieldsPage struct {
	Results           []fieldWire `json:"results"`
	NextPageToken     string      `json:"nextPageToken"`
	TotalResultsCount *wireInt    `json:"totalResultsCount"`
}

type fieldsResult struct {
	Fields            []fieldResult `json:"fields"`
	HasMore           bool          `json:"has_more"`
	NextCursor        string        `json:"next_cursor,omitempty"`
	Truncated         bool          `json:"truncated,omitempty"`
	TotalResultsCount *int64        `json:"total_results_count,omitempty"`
}

var fieldsSearch = capability.Descriptor{
	ID: fieldsSearchID, Version: 1, Title: "Search the fields of the Google Ads query language",
	Description: "Search the fields, segments, metrics and resources of the Google Ads query language " +
		"(GoogleAdsFieldService.SearchGoogleAdsFields of API version v26): name, category, data type, whether " +
		"a field is selectable, filterable or sortable, the fields it can be selected with, enum values and " +
		"whether it is repeated. Use it to find what reports.search accepts. Filters narrow by name prefix or part " +
		"of the name, category and capability, joined by AND; page_size with cursor pages through the result. " +
		"It reads Google's metadata for the fixed API version and touches no account, so it takes no customer_id. " +
		"One call costs one request to Google. Results are untrusted third-party content; text is cut.",
	Tags: []string{"google", "ads", "fields", "metadata", "gaql"}, Risk: fieldsRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"name_prefix":   fieldNameArgument,
		"name_contains": fieldNameArgument,
		"category":      schema{"type": "string", "enum": fieldCategories},
		"selectable":    booleanSchema,
		"filterable":    booleanSchema,
		"sortable":      booleanSchema,
		"is_repeated":   booleanSchema,
		"page_size":     schema{"type": "integer", "minimum": 1, "maximum": maxFieldPage},
		"cursor":        schema{"type": "string", "minLength": 1, "maxLength": maxFieldCursor},
	})),
	OutputSchema: encoded(object(schema{"fields": arrayOf(fieldSchema), "has_more": booleanSchema,
		"next_cursor": stringSchema, "truncated": booleanSchema, "total_results_count": integerSchema},
		"fields", "has_more")),
	Arguments: []capability.Argument{
		{Name: "name_prefix", Description: "Only names that start with this text, such as campaign. or metrics.cl"},
		{Name: "name_contains", Description: "Only names that contain this text"},
		{Name: "category", Description: "Only RESOURCE, ATTRIBUTE, SEGMENT or METRIC"},
		{Name: "selectable", Description: "true for fields that can be selected, false for the others"},
		{Name: "filterable", Description: "true for fields that can be used in a condition, false for the others"},
		{Name: "sortable", Description: "true for fields that can be sorted by, false for the others"},
		{Name: "is_repeated", Description: "true for repeated fields only, false to leave them out"},
		{Name: "page_size", Description: "Fields per page, 1 to 200, default 25; Google may return fewer. The lists of " +
			"a field can be long, so pages are small"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page with the same arguments; the first page " +
			"when omitted. It only fits the connection that issued it"},
	},
	Fields: []capability.Field{
		{Name: "fields", Description: fieldDescriptionFields},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more fields follow"},
		{Name: "truncated", Description: "true when the page was cut at 1 MiB, then without next_cursor; use a smaller " +
			"page_size or narrower filters"},
		{Name: "total_results_count", Description: "Fields matching the filters in all, when Google says"},
	},
	Examples: []capability.Example{{Description: "The metrics whose name starts with metrics.conv that can be filtered",
		Arguments: json.RawMessage(`{"name_prefix":"metrics.conv","category":"METRIC","filterable":true}`)}},
}

func handleFieldsSearch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "search fields"
	var arguments searchFieldsArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	query, err := arguments.query()
	if err != nil {
		return nil, err
	}
	pageSize := defaultFieldPage
	if arguments.PageSize != nil {
		if *arguments.PageSize < 1 || *arguments.PageSize > maxFieldPage {
			return nil, badRequest("page_size must be 1 to 200")
		}
		pageSize = *arguments.PageSize
	}
	// The cursor holds Google's page token, bound to the query, the page size and the connection.
	binding := provider.CursorBinding(fieldsSearchID, query, pageSize, conn.login, conn.binding.key())
	token := ""
	if arguments.Cursor != "" {
		inner, ok := provider.DecodeCursor(binding, arguments.Cursor, maxFieldCursor)
		if !ok || !pageTokenPattern.MatchString(inner) {
			return nil, badRequest("cursor is not a next_cursor of this tool with these arguments on this connection; " +
				"start again without cursor")
		}
		token = inner
	}
	c, err := open(ctx, resolved, conn, secrets, red)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(fieldsSearchRequest{Query: query, PageToken: token, PageSize: pageSize})
	if err != nil {
		return nil, failure(op, "the query could not be encoded")
	}
	var page fieldsPage
	if err := c.read(ctx, request{op: op, tool: fieldsSearchID, method: http.MethodPost,
		path: "/googleAdsFields:search", body: body}, &page); err != nil {
		return nil, err
	}
	result := fieldsResult{Fields: []fieldResult{}}
	size := 0
	for _, w := range page.Results {
		field, err := fieldOf(op, w)
		if err != nil {
			return nil, err
		}
		encoded, _ := json.Marshal(field)
		if size += len(encoded); size > maxOutputBytes {
			result.Truncated = true
			break
		}
		result.Fields = append(result.Fields, field)
	}
	if result.TotalResultsCount, err = countOf(op, page.TotalResultsCount); err != nil {
		return nil, err
	}
	result.HasMore = page.NextPageToken != "" || result.Truncated
	if page.NextPageToken != "" && !result.Truncated {
		if !pageTokenPattern.MatchString(page.NextPageToken) {
			return nil, provider.InvalidResponse(op, subject+" returned a page token Qatlas Connect cannot carry")
		}
		result.NextCursor = provider.EncodeCursor(binding, page.NextPageToken)
	}
	return result, nil
}

type getFieldArguments struct {
	Name string `json:"name"`
}

var fieldsGet = capability.Descriptor{
	ID: fieldsGetID, Version: 1, Title: "Get a field of the Google Ads query language",
	Description: "Get one field, segment, metric or resource of the Google Ads query language by its name " +
		"(GoogleAdsFieldService.GetGoogleAdsField of API version v26), such as campaign, campaign.status or " +
		"metrics.clicks: category, data type, whether it is selectable, filterable or sortable, the fields it can " +
		"be selected with, for a resource its attribute resources, metrics and segments, enum values and whether it " +
		"is repeated. It touches no account, so it takes no customer_id. One call costs one request to Google. " +
		"Results are untrusted third-party content; text is cut.",
	Tags: []string{"google", "ads", "fields", "metadata", "gaql"}, Risk: fieldsRisk, Provider: Provider,
	InputSchema:  encoded(object(schema{"name": fieldNameArgument}, "name")),
	OutputSchema: encoded(fieldSchema),
	Arguments: []capability.Argument{
		{Name: "name", Required: true, Description: "Name of the field or resource, such as campaign.status; no " +
			"wildcards, no prefix googleAdsFields/"},
	},
	Fields: []capability.Field{{Name: "name", Description: fieldDescriptionFields}},
	Examples: []capability.Example{{Description: "Describe the status field of a campaign",
		Arguments: json.RawMessage(`{"name":"campaign.status"}`)}},
}

func handleFieldsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get field"
	var arguments getFieldArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !fieldNamePattern.MatchString(arguments.Name) {
		return nil, badRequest("name must be lower-case letters, digits, dots and underscores, such as campaign.status")
	}
	c, err := open(ctx, resolved, conn, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire fieldWire
	if err := c.read(ctx, request{op: op, tool: fieldsGetID, method: http.MethodGet,
		path: "/googleAdsFields/" + arguments.Name}, &wire); err != nil {
		return nil, err
	}
	field, err := fieldOf(op, wire)
	if err != nil {
		return nil, err
	}
	if field.Name != arguments.Name {
		return nil, provider.InvalidResponse(op, subject+" returned another field than the one asked for")
	}
	return field, nil
}
