package googlemerchant

import (
	"strconv"
	"strings"
)

// The field catalog of the Merchant API reports, fixed and versioned with the provider. Table and field
// names are the Merchant Center Query Language names: the snake case of the member names of the Available
// fields reference of the Merchant API v1. A query is built only from these names; a name that is not here is
// refused, so a report query never carries text of the agent into the query language.

// reportsCatalogVersion changes whenever a table or field of the catalog does.
const reportsCatalogVersion = 1

type fieldKind int

const (
	kindText      fieldKind = iota // free text of the merchant or of Google
	kindEnum                       // an enumeration, upper case with underscores
	kindInt                        // an int64, shown as a string by Google
	kindNumber                     // a double
	kindBool                       // a boolean, never filterable
	kindDate                       // a date: year, month, day
	kindTimestamp                  // a timestamp, never filterable
	kindPrice                      // a price: amountMicros, currencyCode
	kindTextList                   // a list of texts
	kindObjects                    // nested statuses or issues
)

// reportField is one field of a table and the rules the documentation gives for it.
type reportField struct {
	name string
	kind fieldKind
	// metric marks a metric of a performance table; the other fields of such a table are segments.
	metric bool
	// inSelect: the query always selects the field, and the tool adds it when the agent leaves it out.
	inSelect bool
	// inWhere: the query needs a condition on the field.
	inWhere bool
	// noFilter and noSort: the documentation excludes the field from WHERE or from ORDER BY.
	noFilter, noSort bool
	// whereOnly: the field can be filtered but not selected.
	whereOnly bool
	// noSelect: the field cannot be selected; its condition comes from date_range.
	noSelect bool
}

// reportTable is one view of the reports.
type reportTable struct {
	name string
	// key is the member of a result row that holds the view.
	key string
	// performance tables need at least one metric, a condition on date, and no segment without a metric.
	performance bool
	fields      []reportField
}

func camel(snake string) string {
	parts := strings.Split(snake, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

// jsonName is the member name of a field in an answer.
func (f reportField) jsonName() string { return camel(f.name) }

func (t reportTable) field(name string) (reportField, bool) {
	for _, f := range t.fields {
		if f.name == name {
			return f, true
		}
	}
	return reportField{}, false
}

func text(name string) reportField { return reportField{name: name} }

func fld(name string, k fieldKind) reportField { return reportField{name: name, kind: k} }

func (f reportField) required(inSelect, inWhere bool) reportField {
	f.inSelect, f.inWhere = inSelect, inWhere
	return f
}

func (f reportField) plain() reportField { f.noFilter = true; return f }

func (f reportField) metricOf() reportField { f.metric = true; return f }

// levels is five numbered text fields, such as category_l1 to category_l5.
func levels(prefix string, first int) []reportField {
	var out []reportField
	for n := first; n < first+5; n++ {
		out = append(out, text(prefix+strconv.Itoa(n)))
	}
	return out
}

func join(parts ...any) []reportField {
	var out []reportField
	for _, p := range parts {
		switch v := p.(type) {
		case reportField:
			out = append(out, v)
		case []reportField:
			out = append(out, v...)
		}
	}
	return out
}

var traffic = fld("traffic_source", kindEnum)

var reportTables = []reportTable{
	{name: "product_performance_view", key: "productPerformanceView", performance: true, fields: join(
		fld("date", kindDate).required(false, true), fld("week", kindDate),
		fld("conversion_value", kindPrice).metricOf().plain(), fld("marketing_method", kindEnum),
		text("customer_country_code"), fld("store_type", kindEnum), fld("network", kindEnum),
		text("offer_id"), text("title"), text("brand"), levels("category_l", 1), levels("product_type_l", 1),
		levels("custom_label", 0),
		fld("clicks", kindInt).metricOf(), fld("impressions", kindInt).metricOf(),
		fld("click_through_rate", kindNumber).metricOf(), fld("conversions", kindNumber).metricOf(),
		fld("conversion_rate", kindNumber).metricOf())},
	{name: "non_product_performance_view", key: "nonProductPerformanceView", performance: true, fields: join(
		fld("date", kindDate).required(false, true), fld("week", kindDate),
		fld("clicks", kindInt).metricOf(), fld("impressions", kindInt).metricOf(),
		fld("click_through_rate", kindNumber).metricOf())},
	{name: "product_view", key: "productView", fields: join(
		fld("price", kindPrice).plain(), fld("gtin", kindTextList), fld("creation_time", kindTimestamp).plain(),
		fld("expiration_date", kindDate),
		reportField{name: "status_per_reporting_context", kind: kindObjects, noFilter: true, noSort: true},
		reportField{name: "item_issues", kind: kindObjects, noFilter: true, noSort: true},
		fld("click_potential", kindEnum), text("id").required(true, false), fld("channel", kindEnum),
		text("language_code"), text("feed_label"), text("offer_id"), text("title"), text("brand"),
		levels("category_l", 1), levels("product_type_l", 1), text("condition"), text("availability"),
		text("shipping_label"), text("item_group_id"), text("thumbnail_link"),
		fld("aggregated_reporting_context_status", kindEnum),
		reportField{name: "reporting_context", kind: kindEnum, whereOnly: true},
		reportField{name: itemSeverityPath, kind: kindEnum, whereOnly: true},
		fld("click_potential_rank", kindInt))},
	{name: "price_competitiveness_product_view", key: "priceCompetitivenessProductView", fields: join(
		fld("price", kindPrice).plain(), fld("benchmark_price", kindPrice).plain(),
		text("report_country_code").required(true, false), text("id").required(true, false),
		text("offer_id"), text("title"), text("brand"), levels("category_l", 1), levels("product_type_l", 1))},
	{name: "price_insights_product_view", key: "priceInsightsProductView", fields: join(
		fld("price", kindPrice).plain(), fld("suggested_price", kindPrice).plain(),
		fld("effectiveness", kindEnum), text("id").required(true, false), text("offer_id"), text("title"),
		text("brand"), levels("category_l", 1), levels("product_type_l", 1),
		fld("predicted_impressions_change_fraction", kindNumber),
		fld("predicted_clicks_change_fraction", kindNumber),
		fld("predicted_conversions_change_fraction", kindNumber))},
	{name: "best_sellers_product_cluster_view", key: "bestSellersProductClusterView", fields: join(
		fld("report_date", kindDate).required(true, false), fld("variant_gtins", kindTextList),
		fld("report_granularity", kindEnum).required(true, true),
		text("report_country_code").required(true, true), fld("report_category_id", kindInt).required(true, false),
		text("title"), text("brand"), levels("category_l", 1), fld("inventory_status", kindEnum),
		fld("brand_inventory_status", kindEnum), fld("rank", kindInt), fld("previous_rank", kindInt),
		fld("relative_demand", kindEnum), fld("previous_relative_demand", kindEnum),
		fld("relative_demand_change", kindEnum))},
	{name: "best_sellers_brand_view", key: "bestSellersBrandView", fields: join(
		fld("report_date", kindDate).required(true, false), fld("report_granularity", kindEnum).required(true, true),
		text("report_country_code").required(true, true), fld("report_category_id", kindInt).required(true, false),
		text("brand"), fld("rank", kindInt), fld("previous_rank", kindInt), fld("relative_demand", kindEnum),
		fld("previous_relative_demand", kindEnum), fld("relative_demand_change", kindEnum))},
	{name: "competitive_visibility_competitor_view", key: "competitiveVisibilityCompetitorView", fields: join(
		fld("date", kindDate).required(false, true), text("domain").required(true, false).plain(),
		fld("is_your_domain", kindBool).plain(), text("report_country_code").required(true, true),
		fld("report_category_id", kindInt).required(true, true), traffic.required(true, false),
		fld("rank", kindInt).plain(), fld("ads_organic_ratio", kindNumber).plain(),
		fld("page_overlap_rate", kindNumber).plain(), fld("higher_position_rate", kindNumber).plain(),
		fld("relative_visibility", kindNumber).plain())},
	{name: "competitive_visibility_top_merchant_view", key: "competitiveVisibilityTopMerchantView", fields: join(
		reportField{name: "date", kind: kindDate, inWhere: true, noSelect: true},
		text("domain").required(true, false).plain(), fld("is_your_domain", kindBool).plain(),
		text("report_country_code").required(true, true), fld("report_category_id", kindInt).required(true, true),
		traffic.required(true, false), fld("rank", kindInt).plain(), fld("ads_organic_ratio", kindNumber).plain(),
		fld("page_overlap_rate", kindNumber).plain(), fld("higher_position_rate", kindNumber).plain())},
	{name: "competitive_visibility_benchmark_view", key: "competitiveVisibilityBenchmarkView", fields: join(
		fld("date", kindDate).required(true, true), text("report_country_code").required(true, true),
		fld("report_category_id", kindInt).required(true, true), traffic.required(true, false),
		fld("your_domain_visibility_trend", kindNumber).plain(),
		fld("category_benchmark_visibility_trend", kindNumber).plain())},
}

// itemSeverityPath is the one nested member of a table the documentation names as filterable.
const itemSeverityPath = "item_issues.severity.aggregated_severity"

func reportTableNamed(name string) (reportTable, bool) {
	for _, t := range reportTables {
		if t.name == name {
			return t, true
		}
	}
	return reportTable{}, false
}

// selectable lists the names of the fields a query may select, for the schema of the tool.
func (t reportTable) selectable() []string {
	var out []string
	for _, f := range t.fields {
		if !f.whereOnly && !f.noSelect {
			out = append(out, f.name)
		}
	}
	return out
}

// allReportFieldNames is the union of the field names of every table, for the input schema.
func allReportFieldNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range reportTables {
		for _, f := range t.fields {
			if !seen[f.name] {
				seen[f.name] = true
				out = append(out, f.name)
			}
		}
	}
	return out
}

func reportTableNames() []string {
	var out []string
	for _, t := range reportTables {
		out = append(out, t.name)
	}
	return out
}
