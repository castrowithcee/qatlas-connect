package googlesearchconsole

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const searchAnalyticsID = Provider + ".search-analytics.query"

// Bounds of a query. The row limits, the dimensions, the operators and the value sets are those of the
// discovery document (revision 20261115). It names no maximum for the number of filters, so the filter counts
// and the text lengths below are Qatlas' own bounds of what it builds into a request.
const (
	defaultRowLimit = 1000
	maxRowLimit     = 25000
	maxStartRow     = math.MaxInt32
	// maxResultRows and maxResultKeyBytes cut what one result shows. What is cut stays reachable: the cursor
	// continues at the first row not shown.
	maxResultRows     = 1000
	maxResultKeyBytes = 1 << 20
	maxKeys           = 8
	maxFilterGroups   = 16
	maxFilters        = 32
	maxFilterLists    = 2
	maxListValues     = 1000 // documented for a filter list
	maxExpression     = 2048
	maxCursorBytes    = 256
)

var (
	dimensionValues   = []string{"DATE", "QUERY", "PAGE", "COUNTRY", "DEVICE", "SEARCH_APPEARANCE", "HOUR", "BRANDED_CLASSIFICATION"}
	filterDimensions  = []string{"QUERY", "PAGE", "COUNTRY", "DEVICE", "SEARCH_APPEARANCE", "BRANDED_CLASSIFICATION"}
	listDimensions    = []string{"COUNTRY", "DEVICE"}
	operatorValues    = []string{"EQUALS", "NOT_EQUALS", "CONTAINS", "NOT_CONTAINS", "INCLUDING_REGEX", "EXCLUDING_REGEX"}
	searchTypeValues  = []string{"WEB", "IMAGE", "VIDEO", "NEWS", "DISCOVER", "GOOGLE_NEWS", "WEB_MULTIMODAL"}
	aggregationValues = []string{"AUTO", "BY_PROPERTY", "BY_PAGE", "BY_NEWS_SHOWCASE_PANEL"}
	// DATA_STATE_UNSPECIFIED is the default value and "should not be used", so it is not offered.
	dataStateValues = []string{"FINAL", "ALL", "HOURLY_ALL"}
)

// enumSchema writes a string enumeration of a closed value set.
func enumSchema(values []string) string {
	b, _ := json.Marshal(map[string]any{"type": "string", "enum": values})
	return string(b)
}

const dateSchema = `{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$"}`

var (
	filterSchema = `{"type":"object","properties":{"dimension":` + enumSchema(filterDimensions) + `,"operator":` +
		enumSchema(operatorValues) + `,"expression":{"type":"string","minLength":1,"maxLength":` +
		strconv.Itoa(maxExpression) + `}},"required":["dimension","operator","expression"],"additionalProperties":false}`
	filterListSchema = `{"type":"object","properties":{"dimension":` + enumSchema(listDimensions) +
		`,"expressions":{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxListValues) +
		`,"items":{"type":"string","minLength":1,"maxLength":100}}},"required":["dimension","expressions"],"additionalProperties":false}`
	groupSchema = `{"type":"object","properties":{"group_type":{"type":"string","enum":["AND"]},"filters":` +
		`{"type":"array","maxItems":` + strconv.Itoa(maxFilters) + `,"items":` + filterSchema + `},"filter_lists":` +
		`{"type":"array","maxItems":` + strconv.Itoa(maxFilterLists) + `,"items":` + filterListSchema +
		`}},"additionalProperties":false}`
	searchAnalyticsInput = `{"type":"object","properties":{` + siteURLSchema +
		`,"start_date":` + dateSchema + `,"end_date":` + dateSchema +
		`,"dimensions":{"type":"array","maxItems":` + strconv.Itoa(len(dimensionValues)) +
		`,"uniqueItems":true,"items":` + enumSchema(dimensionValues) + `},"type":` + enumSchema(searchTypeValues) +
		`,"search_type":` + enumSchema(searchTypeValues) + `,"aggregation_type":` + enumSchema(aggregationValues) +
		`,"data_state":` + enumSchema(dataStateValues) + `,"dimension_filter_groups":{"type":"array","maxItems":` +
		strconv.Itoa(maxFilterGroups) + `,"items":` + groupSchema + `},"row_limit":{"type":"integer","minimum":1,` +
		`"maximum":` + strconv.Itoa(maxRowLimit) + `},"start_row":{"type":"integer","minimum":0,"maximum":` +
		strconv.Itoa(maxStartRow) + `},"cursor":{"type":"string","minLength":1,"maxLength":` +
		strconv.Itoa(maxCursorBytes) + `}},"required":["start_date","end_date"],"additionalProperties":false}`
	searchAnalyticsOutput = `{"type":"object","properties":{"rows":{"type":"array","items":{"type":"object",` +
		`"properties":{"keys":{"type":"array","items":{"type":"string"}},"clicks":{"type":"number"},` +
		`"impressions":{"type":"number"},"ctr":{"type":"number"},"position":{"type":"number"}},` +
		`"required":["clicks","impressions","ctr","position"],"additionalProperties":false}},` +
		`"responseAggregationType":{"type":"string"},"metadata":{"type":"object","properties":{` +
		`"firstIncompleteDate":{"type":"string"},"firstIncompleteHour":{"type":"string"}},` +
		`"additionalProperties":false},"truncated":{"type":"boolean"},"next_cursor":{"type":"string"}},` +
		`"required":["rows"],"additionalProperties":false}`
)

// analyticsRisk is the risk of the query: a safe read although it is sent as POST. The search queries it
// returns are personal data of the people who typed them.
var analyticsRisk = capability.Risk{
	Effect:       capability.EffectRead,
	Idempotency:  capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone,
	OpenWorld:    true,
	DataSensitivity: "search queries of the site's visitors, which are personal data and can reveal interests, " +
		"health, or identity, together with the addresses and traffic figures of the property",
}

var searchAnalyticsQuery = capability.Descriptor{
	ID: searchAnalyticsID, Version: 1, Title: "Query the search traffic of a property",
	Description: "Query the Search Console search traffic of one property for a date range: clicks, impressions, " +
		"click-through rate and average position, grouped by dimensions such as date, query, page, country, " +
		"device or hour and narrowed by filters. A query is a read although it is sent as POST. It is heavy for " +
		"Google when it covers a long range, groups or filters by page or query, or asks for many rows: ask for " +
		"the fewest rows and the shortest range that answer the question. A result holds at most 1000 rows; " +
		"a full result carries next_cursor for the following rows, and the last one holds fewer rows than asked. " +
		"The data of the last days is preliminary: data_state FINAL leaves it out, ALL includes it and marks " +
		"where it begins in metadata, and HOURLY_ALL is the only state that serves the hour dimension. " +
		"Google keeps the data of a limited time only, and dates are Pacific time. Search queries are personal " +
		"data; handle them accordingly. " + untrustedNote,
	Tags: []string{"google", "search-console", "search-analytics"}, Risk: analyticsRisk, Provider: Provider,
	InputSchema:  json.RawMessage(searchAnalyticsInput),
	OutputSchema: json.RawMessage(searchAnalyticsOutput),
	Arguments: []capability.Argument{siteArgument,
		{Name: "start_date", Required: true, Description: "First day of the range, YYYY-MM-DD, Pacific time, included; not after end_date"},
		{Name: "end_date", Required: true, Description: "Last day of the range, YYYY-MM-DD, Pacific time, included"},
		{Name: "dimensions", Description: "Up to 8 different dimensions to group by, in the order of the keys of each " +
			"row: DATE, QUERY, PAGE, COUNTRY, DEVICE, SEARCH_APPEARANCE, HOUR, BRANDED_CLASSIFICATION; none gives one " +
			"total row. HOUR needs data_state HOURLY_ALL"},
		{Name: "type", Description: "Search type or report: WEB (default), IMAGE, VIDEO, NEWS, DISCOVER, GOOGLE_NEWS, " +
			"WEB_MULTIMODAL"},
		{Name: "search_type", Description: "The search type to filter for, same values as type; not together with type"},
		{Name: "aggregation_type", Description: "AUTO (default), BY_PROPERTY, BY_PAGE or BY_NEWS_SHOWCASE_PANEL. " +
			"BY_PROPERTY cannot be used when grouping or filtering by PAGE"},
		{Name: "data_state", Description: "FINAL (complete data only), ALL (also preliminary data) or HOURLY_ALL " +
			"(hourly data, also preliminary); Google's default applies when omitted"},
		{Name: "dimension_filter_groups", Description: "Up to 16 groups, all of which a row must pass. A group holds " +
			"filters (dimension QUERY, PAGE, COUNTRY, DEVICE, SEARCH_APPEARANCE or BRANDED_CLASSIFICATION, operator " +
			"EQUALS, NOT_EQUALS, CONTAINS, NOT_CONTAINS, INCLUDING_REGEX or EXCLUDING_REGEX, and expression) and " +
			"filter_lists (dimension COUNTRY or DEVICE with up to 1000 expressions of which a row matches any); " +
			"group_type is AND. Values are not case-sensitive; a regular expression uses RE2 syntax"},
		{Name: "row_limit", Description: "Rows per further step, 1 to 25000 (default 1000); a result shows at most " +
			"1000 of them"},
		{Name: "start_row", Description: "Zero-based index of the first row (default 0); not together with cursor"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous result of the same query; it only fits the " +
			"unchanged arguments, and start_row is then left out"}},
	Fields: []capability.Field{
		{Name: "rows", Description: "one entry per group: keys (the values of the requested dimensions, in order, " +
			"cut at 2048 characters), clicks, impressions, ctr (0 to 1) and position (average)"},
		{Name: "responseAggregationType", Description: "how Google aggregated the rows"},
		{Name: "metadata", Description: "firstIncompleteDate with data_state ALL and the DATE dimension, " +
			"firstIncompleteHour with HOURLY_ALL and the HOUR dimension: from there on the figures may still change"},
		{Name: "truncated", Description: "true when the result was cut short of the rows Google sent; " +
			"next_cursor continues at the first row not shown"},
		{Name: "next_cursor", Description: "Cursor of the following rows, absent at the end"}},
	Examples: []capability.Example{{Description: "Top queries of a month with their clicks",
		Arguments: json.RawMessage(`{"site_url":"sc-domain:example.com","start_date":"2026-10-01",` +
			`"end_date":"2026-10-31","dimensions":["QUERY"],"row_limit":100}`)}},
}

type (
	filterArgument struct {
		Dimension  string `json:"dimension"`
		Operator   string `json:"operator"`
		Expression string `json:"expression"`
	}
	filterListArgument struct {
		Dimension   string   `json:"dimension"`
		Expressions []string `json:"expressions"`
	}
	filterGroupArgument struct {
		GroupType   string               `json:"group_type"`
		Filters     []filterArgument     `json:"filters"`
		FilterLists []filterListArgument `json:"filter_lists"`
	}
	searchAnalyticsArguments struct {
		SiteURL      string                `json:"site_url"`
		StartDate    string                `json:"start_date"`
		EndDate      string                `json:"end_date"`
		Dimensions   []string              `json:"dimensions"`
		Type         string                `json:"type"`
		SearchType   string                `json:"search_type"`
		Aggregation  string                `json:"aggregation_type"`
		DataState    string                `json:"data_state"`
		FilterGroups []filterGroupArgument `json:"dimension_filter_groups"`
		RowLimit     *int                  `json:"row_limit"`
		StartRow     *int                  `json:"start_row"`
		Cursor       string                `json:"cursor"`
	}
)

// number is a figure that drops a value that is not a number instead of failing the whole answer.
type number float64

func (n *number) UnmarshalJSON(b []byte) error {
	var value float64
	if json.Unmarshal(b, &value) == nil {
		*n = number(value)
	}
	return nil
}

type (
	analyticsRow struct {
		Keys        []text `json:"keys"`
		Clicks      number `json:"clicks"`
		Impressions number `json:"impressions"`
		CTR         number `json:"ctr"`
		Position    number `json:"position"`
	}
	analyticsMetadata struct {
		FirstIncompleteDate short `json:"firstIncompleteDate,omitempty"`
		FirstIncompleteHour short `json:"firstIncompleteHour,omitempty"`
	}
	analyticsAnswer struct {
		Rows                    []analyticsRow    `json:"rows"`
		ResponseAggregationType short             `json:"responseAggregationType"`
		Metadata                analyticsMetadata `json:"metadata"`
	}
	analyticsResult struct {
		Rows                    []analyticsRow     `json:"rows"`
		ResponseAggregationType short              `json:"responseAggregationType,omitempty"`
		Metadata                *analyticsMetadata `json:"metadata,omitempty"`
		Truncated               bool               `json:"truncated,omitempty"`
		NextCursor              string             `json:"next_cursor,omitempty"`
	}
)

// checkDate checks a calendar day in the form Google takes.
func checkDate(name, value string) (time.Time, error) {
	day, err := time.Parse(time.DateOnly, value)
	if err != nil || len(value) != len(time.DateOnly) {
		return time.Time{}, badRequest(name + " must be a calendar date in the form YYYY-MM-DD")
	}
	return day, nil
}

func checkEnum(name, value string, allowed []string) error {
	if value != "" && !slices.Contains(allowed, value) {
		return badRequest(name + " must be one of " + strings.Join(allowed, ", "))
	}
	return nil
}

// searchAnalyticsBody checks every argument and builds the request body, without the paging members that
// the cursor sets. It runs before any secret is read.
func searchAnalyticsBody(a searchAnalyticsArguments) (map[string]any, error) {
	start, err := checkDate("start_date", a.StartDate)
	if err != nil {
		return nil, err
	}
	end, err := checkDate("end_date", a.EndDate)
	if err != nil {
		return nil, err
	}
	if start.After(end) {
		return nil, badRequest("start_date must not be after end_date")
	}
	if len(a.Dimensions) > len(dimensionValues) {
		return nil, badRequest("dimensions takes at most 8 entries")
	}
	byPage, hour := false, false
	for i, dimension := range a.Dimensions {
		if err := checkEnum("dimensions", dimension, dimensionValues); err != nil || dimension == "" {
			return nil, badRequest("dimensions must be of " + strings.Join(dimensionValues, ", "))
		}
		if slices.Contains(a.Dimensions[:i], dimension) {
			return nil, badRequest("dimensions must not repeat a dimension")
		}
		byPage = byPage || dimension == "PAGE"
		hour = hour || dimension == "HOUR"
	}
	if hour && a.DataState != "HOURLY_ALL" {
		return nil, badRequest("the HOUR dimension needs data_state HOURLY_ALL")
	}
	for name, check := range map[string]struct {
		value   string
		allowed []string
	}{"type": {a.Type, searchTypeValues}, "search_type": {a.SearchType, searchTypeValues},
		"aggregation_type": {a.Aggregation, aggregationValues}, "data_state": {a.DataState, dataStateValues}} {
		if err := checkEnum(name, check.value, check.allowed); err != nil {
			return nil, err
		}
	}
	if a.Type != "" && a.SearchType != "" {
		return nil, badRequest("type and search_type cannot be given together")
	}
	if len(a.FilterGroups) > maxFilterGroups {
		return nil, badRequest("dimension_filter_groups takes at most 16 groups")
	}
	groups := make([]map[string]any, 0, len(a.FilterGroups))
	for _, group := range a.FilterGroups {
		built, groupByPage, err := filterGroup(group)
		if err != nil {
			return nil, err
		}
		byPage = byPage || groupByPage
		groups = append(groups, built)
	}
	if a.Aggregation == "BY_PROPERTY" && byPage {
		return nil, badRequest("aggregation_type BY_PROPERTY cannot be used when grouping or filtering by PAGE")
	}
	body := map[string]any{"startDate": a.StartDate, "endDate": a.EndDate}
	set := func(key, value string) {
		if value != "" {
			body[key] = value
		}
	}
	if len(a.Dimensions) > 0 {
		body["dimensions"] = a.Dimensions
	}
	set("type", a.Type)
	set("searchType", a.SearchType)
	set("aggregationType", a.Aggregation)
	set("dataState", a.DataState)
	if len(groups) > 0 {
		body["dimensionFilterGroups"] = groups
	}
	return body, nil
}

// filterGroup checks one group and reports whether it filters by page.
func filterGroup(group filterGroupArgument) (map[string]any, bool, error) {
	if err := checkEnum("group_type", group.GroupType, []string{"AND"}); err != nil {
		return nil, false, err
	}
	if len(group.Filters) > maxFilters || len(group.FilterLists) > maxFilterLists {
		return nil, false, badRequest("a filter group takes at most 32 filters and 2 filter lists")
	}
	built := map[string]any{}
	if group.GroupType != "" {
		built["groupType"] = group.GroupType
	}
	byPage := false
	filters := make([]map[string]string, 0, len(group.Filters))
	for _, f := range group.Filters {
		if !slices.Contains(filterDimensions, f.Dimension) || !slices.Contains(operatorValues, f.Operator) {
			return nil, false, badRequest("a filter needs a dimension of " + strings.Join(filterDimensions, ", ") +
				" and an operator of " + strings.Join(operatorValues, ", "))
		}
		if f.Expression == "" || len(f.Expression) > maxExpression {
			return nil, false, badRequest("a filter expression must hold 1 to 2048 bytes")
		}
		if strings.HasSuffix(f.Operator, "_REGEX") {
			if _, err := regexp.Compile(f.Expression); err != nil {
				return nil, false, badRequest("a filter expression with a regex operator must be a valid RE2 regular expression")
			}
		}
		byPage = byPage || f.Dimension == "PAGE"
		filters = append(filters, map[string]string{"dimension": f.Dimension, "operator": f.Operator, "expression": f.Expression})
	}
	if len(filters) > 0 {
		built["filters"] = filters
	}
	lists := make([]map[string]any, 0, len(group.FilterLists))
	for _, l := range group.FilterLists {
		if !slices.Contains(listDimensions, l.Dimension) {
			return nil, false, badRequest("a filter list supports only the dimensions COUNTRY and DEVICE")
		}
		if len(l.Expressions) == 0 || len(l.Expressions) > maxListValues {
			return nil, false, badRequest("a filter list takes 1 to 1000 expressions")
		}
		for _, expression := range l.Expressions {
			if expression == "" || len(expression) > 100 {
				return nil, false, badRequest("a filter list expression must hold 1 to 100 bytes")
			}
		}
		lists = append(lists, map[string]any{"dimension": l.Dimension, "expressions": l.Expressions})
	}
	if len(lists) > 0 {
		built["filterLists"] = lists
	}
	return built, byPage, nil
}

// decodePosition reads the continuation a cursor carries: the first row to ask for and the row where the
// page being served ends.
func decodePosition(inner string) (next, end int, ok bool) {
	first, second, found := strings.Cut(inner, ":")
	if !found {
		return 0, 0, false
	}
	next, err1 := strconv.Atoi(first)
	end, err2 := strconv.Atoi(second)
	return next, end, err1 == nil && err2 == nil && next > 0 && end > next && end <= maxStartRow+maxRowLimit
}

func handleSearchAnalytics(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "search analytics query"
	var arguments searchAnalyticsArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	siteURL, err := siteOf(resolved, arguments.SiteURL)
	if err != nil {
		return nil, err
	}
	body, err := searchAnalyticsBody(arguments)
	if err != nil {
		return nil, err
	}
	limit := defaultRowLimit
	if arguments.RowLimit != nil {
		limit = *arguments.RowLimit
	}
	if limit < 1 || limit > maxRowLimit {
		return nil, badRequest("row_limit must be 1 to 25000")
	}
	start := 0
	if arguments.StartRow != nil {
		if start = *arguments.StartRow; start < 0 || start > maxStartRow {
			return nil, badRequest("start_row must be a non-negative 32-bit number")
		}
	}
	// The cursor only carries where to go on, so it binds the property, the whole query and the row limit.
	binding := provider.CursorBinding(searchAnalyticsID, siteURL, body, limit)
	end := start + limit
	if arguments.Cursor != "" {
		if arguments.StartRow != nil {
			return nil, badRequest("start_row and cursor cannot be given together")
		}
		inner, ok := provider.DecodeCursor(binding, arguments.Cursor, maxCursorBytes)
		if !ok {
			return nil, badRequest("cursor is not a next_cursor of this query; start the query again without cursor")
		}
		if start, end, ok = decodePosition(inner); !ok {
			return nil, badRequest("cursor is not a next_cursor of this query; start the query again without cursor")
		}
	}
	ask := min(maxResultRows, end-start)
	body["rowLimit"] = ask
	body["startRow"] = start

	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer analyticsAnswer
	if err := c.post(ctx, op, searchAnalyticsID, sitePath(siteURL)+"/searchAnalytics/query", body,
		c.queryLimiter(siteURL), notFoundSite, &answer); err != nil {
		return nil, err
	}

	result := analyticsResult{Rows: []analyticsRow{}, ResponseAggregationType: answer.ResponseAggregationType}
	if answer.Metadata != (analyticsMetadata{}) {
		result.Metadata = &answer.Metadata
	}
	received := min(len(answer.Rows), ask)
	size := 0
	for _, row := range answer.Rows[:received] {
		if len(row.Keys) > maxKeys {
			row.Keys = row.Keys[:maxKeys]
		}
		for _, key := range row.Keys {
			size += len(key)
		}
		if size > maxResultKeyBytes && len(result.Rows) > 0 {
			result.Truncated = true
			break
		}
		result.Rows = append(result.Rows, row)
	}
	// A result that was cut, or a full one, may have rows behind it; fewer rows than asked end the query.
	if len(result.Rows) == received && received < ask {
		return result, nil
	}
	next := start + len(result.Rows)
	if next > maxStartRow {
		return result, nil
	}
	if next >= end {
		end = next + limit
	}
	result.NextCursor = provider.EncodeCursor(binding, strconv.Itoa(next)+":"+strconv.Itoa(end))
	return result, nil
}
