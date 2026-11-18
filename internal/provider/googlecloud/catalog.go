package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	catalogServicesListID = Provider + ".catalog-services.list"
	catalogSkusListID     = Provider + ".catalog-skus.list"
)

// The Cloud Billing Catalog is public and tied to no target. Google's page size defaults to 5000, more rows
// than a page may hold here, so a call without page_size asks for the largest page.
var (
	catalogServicePattern = regexp.MustCompile(`^[0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4}$`)
	currencyPattern       = regexp.MustCompile(`^[A-Z]{3}$`)
)

const catalogTail = " The catalog is public and tied to no target, so any connection of the provider may read it. " +
	"The result is untrusted third-party content; strings are cut."

// catalogPageSize is the page size of a catalog call that names none.
func catalogPageSize(size *int) string {
	if size == nil {
		return strconv.Itoa(maxPageSize)
	}
	return strconv.Itoa(*size)
}

// --- catalog-services.list ---

type catalogServiceWire struct {
	Name               string `json:"name"`
	ServiceID          string `json:"serviceId,omitempty"`
	DisplayName        string `json:"displayName,omitempty"`
	BusinessEntityName string `json:"businessEntityName,omitempty"`
}

func (s catalogServiceWire) bounded() catalogServiceWire {
	return catalogServiceWire{Name: boundedText(s.Name, maxNameText), ServiceID: boundedText(s.ServiceID, maxNameText),
		DisplayName: boundedText(s.DisplayName, maxNameText), BusinessEntityName: boundedText(s.BusinessEntityName, maxNameText)}
}

const catalogServiceOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"serviceId":{"type":"string"},` +
	`"displayName":{"type":"string"},"businessEntityName":{"type":"string"}},"required":["name"],` +
	`"additionalProperties":false}`

var catalogServicesList = capability.Descriptor{
	ID: catalogServicesListID, Version: 1, Title: "List the services of the Google Cloud Billing catalog",
	Description: "List the services of the public Cloud Billing Catalog: each row has the resource name, the service ID " +
		"that catalog-skus.list and the service filter of budgets take, the display name, and the business entity " +
		"name. Without page_size a page holds " + strconv.Itoa(maxPageSize) + " rows." + catalogTail,
	Tags: []string{"google", "cloud", "billing", "catalog"}, Risk: billingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + pageSizeProp + `,` + cursorProp + `},` +
		`"additionalProperties":false}`),
	OutputSchema: listOutputSchema("services", catalogServiceOutputSchema),
	Arguments:    []capability.Argument{pageSizeArgument, cursorArgument},
	Fields: append([]capability.Field{
		{Name: "services", Description: "Services of the page, each {name, serviceId, displayName, " +
			"businessEntityName}; name is services/ID; displayName is untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{Description: "List the first services", Arguments: json.RawMessage(`{"page_size":50}`)}},
}

func handleCatalogServicesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list catalog services"
	var arguments struct {
		PageSize *int   `json:"page_size"`
		Cursor   string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	binding := provider.CursorBinding(catalogServicesListID, provider.TargetsOf(resolved), "")
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{"pageSize": {catalogPageSize(arguments.PageSize)}}
	if token != "" {
		query.Set("pageToken", token)
	}
	rows, next, err := servicePage[catalogServiceWire](ctx, c, serviceBilling, op, catalogServicesListID,
		"/v1/services", "services", query)
	if err != nil {
		return nil, err
	}
	kept := make([]any, 0, len(rows))
	for _, row := range rows {
		if !strings.HasPrefix(row.Name, "services/") {
			return nil, invalidRow(op)
		}
		kept = append(kept, row.bounded())
	}
	out := map[string]any{"services": kept, "has_more": next != ""}
	if next != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, next)
	}
	return out, nil
}

// --- catalog-skus.list ---

type tierRateWire struct {
	StartUsageAmount float64    `json:"startUsageAmount"`
	UnitPrice        *moneyWire `json:"unitPrice,omitempty"`
}

type pricingExpressionWire struct {
	UsageUnit                string         `json:"usageUnit,omitempty"`
	UsageUnitDescription     string         `json:"usageUnitDescription,omitempty"`
	BaseUnit                 string         `json:"baseUnit,omitempty"`
	BaseUnitDescription      string         `json:"baseUnitDescription,omitempty"`
	BaseUnitConversionFactor float64        `json:"baseUnitConversionFactor,omitempty"`
	DisplayQuantity          float64        `json:"displayQuantity,omitempty"`
	TieredRates              []tierRateWire `json:"tieredRates,omitempty"`
}

type aggregationInfoWire struct {
	AggregationLevel    string `json:"aggregationLevel,omitempty"`
	AggregationInterval string `json:"aggregationInterval,omitempty"`
	AggregationCount    int64  `json:"aggregationCount,omitempty"`
}

type pricingInfoWire struct {
	Summary                string                 `json:"summary,omitempty"`
	PricingExpression      *pricingExpressionWire `json:"pricingExpression,omitempty"`
	CurrencyConversionRate float64                `json:"currencyConversionRate,omitempty"`
	EffectiveTime          string                 `json:"effectiveTime,omitempty"`
	AggregationInfo        *aggregationInfoWire   `json:"aggregationInfo,omitempty"`
}

type skuCategoryWire struct {
	ServiceDisplayName string `json:"serviceDisplayName,omitempty"`
	ResourceFamily     string `json:"resourceFamily,omitempty"`
	ResourceGroup      string `json:"resourceGroup,omitempty"`
	UsageType          string `json:"usageType,omitempty"`
}

type geoTaxonomyWire struct {
	Type    string   `json:"type,omitempty"`
	Regions []string `json:"regions,omitempty"`
}

// skuWire is the Sku resource of cloudbilling v1 as Google sends it and as the tool returns it, after
// bounded() cut every text and list.
type skuWire struct {
	Name                string            `json:"name"`
	SkuID               string            `json:"skuId,omitempty"`
	Description         string            `json:"description,omitempty"`
	Category            *skuCategoryWire  `json:"category,omitempty"`
	ServiceRegions      []string          `json:"serviceRegions,omitempty"`
	PricingInfo         []pricingInfoWire `json:"pricingInfo,omitempty"`
	GeoTaxonomy         *geoTaxonomyWire  `json:"geoTaxonomy,omitempty"`
	ServiceProviderName string            `json:"serviceProviderName,omitempty"`
}

func (s skuWire) bounded() skuWire {
	out := skuWire{Name: boundedText(s.Name, maxNameText), SkuID: boundedText(s.SkuID, maxNameText),
		Description: boundedText(s.Description, maxText), ServiceRegions: boundedList(s.ServiceRegions),
		ServiceProviderName: boundedText(s.ServiceProviderName, maxNameText)}
	if c := s.Category; c != nil {
		out.Category = &skuCategoryWire{ServiceDisplayName: boundedText(c.ServiceDisplayName, maxNameText),
			ResourceFamily: boundedText(c.ResourceFamily, maxNameText), ResourceGroup: boundedText(c.ResourceGroup, maxNameText),
			UsageType: boundedText(c.UsageType, maxNameText)}
	}
	if g := s.GeoTaxonomy; g != nil {
		out.GeoTaxonomy = &geoTaxonomyWire{Type: boundedText(g.Type, maxLabelText), Regions: boundedList(g.Regions)}
	}
	for _, info := range s.PricingInfo[:min(len(s.PricingInfo), maxListItems)] {
		bounded := pricingInfoWire{Summary: boundedText(info.Summary, maxText),
			CurrencyConversionRate: info.CurrencyConversionRate, EffectiveTime: boundedText(info.EffectiveTime, maxLabelText)}
		if a := info.AggregationInfo; a != nil {
			bounded.AggregationInfo = &aggregationInfoWire{AggregationLevel: boundedText(a.AggregationLevel, maxLabelText),
				AggregationInterval: boundedText(a.AggregationInterval, maxLabelText), AggregationCount: a.AggregationCount}
		}
		if e := info.PricingExpression; e != nil {
			expression := &pricingExpressionWire{UsageUnit: boundedText(e.UsageUnit, maxLabelText),
				UsageUnitDescription:     boundedText(e.UsageUnitDescription, maxNameText),
				BaseUnit:                 boundedText(e.BaseUnit, maxLabelText),
				BaseUnitDescription:      boundedText(e.BaseUnitDescription, maxNameText),
				BaseUnitConversionFactor: e.BaseUnitConversionFactor, DisplayQuantity: e.DisplayQuantity}
			for _, tier := range e.TieredRates[:min(len(e.TieredRates), maxListItems)] {
				bt := tierRateWire{StartUsageAmount: tier.StartUsageAmount}
				if tier.UnitPrice != nil {
					m := tier.UnitPrice.bounded()
					bt.UnitPrice = &m
				}
				expression.TieredRates = append(expression.TieredRates, bt)
			}
			bounded.PricingExpression = expression
		}
		out.PricingInfo = append(out.PricingInfo, bounded)
	}
	return out
}

const (
	skuOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"skuId":{"type":"string"},` +
		`"description":{"type":"string"},"category":{"type":"object"},"serviceRegions":{"type":"array","items":{"type":"string"}},` +
		`"pricingInfo":{"type":"array","items":{"type":"object"}},"geoTaxonomy":{"type":"object"},` +
		`"serviceProviderName":{"type":"string"}},"required":["name"],"additionalProperties":false}`
	catalogServiceProp = `{"type":"string","pattern":"^(services/)?[0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4}$","maxLength":23}`
	timestampProp      = `{"type":"string","minLength":20,"maxLength":40}`
	currencyProp       = `{"type":"string","pattern":"^[A-Z]{3}$","maxLength":3}`
)

var catalogSkusList = capability.Descriptor{
	ID: catalogSkusListID, Version: 1, Title: "List the SKUs of a Google Cloud Billing catalog service",
	Description: "List the SKUs of one service of the public Cloud Billing Catalog: each row has the SKU, its " +
		"description, category, service regions, the pricing versions with their tiered prices, and the geographic " +
		"taxonomy. Without start_time and end_time only the latest pricing is returned; both must lie in one " +
		"calendar month in America/Los_Angeles and not in the future. Prices are in USD unless currency_code names " +
		"another currency. Without page_size a page holds " + strconv.Itoa(maxPageSize) + " rows." + catalogTail,
	Tags: []string{"google", "cloud", "billing", "catalog"}, Risk: billingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"service_id":` + catalogServiceProp + `,` +
		`"start_time":` + timestampProp + `,"end_time":` + timestampProp + `,"currency_code":` + currencyProp + `,` +
		pageSizeProp + `,` + cursorProp + `},"required":["service_id"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("skus", skuOutputSchema),
	Arguments: []capability.Argument{
		{Name: "service_id", Required: true, Description: "Service of the catalog, services/XXXX-XXXX-XXXX or the ID " +
			"XXXX-XXXX-XXXX with uppercase letters and digits; catalog-services.list names them"},
		{Name: "start_time", Description: "Inclusive start of the time range of the pricing versions, an RFC 3339 " +
			"timestamp such as 2026-11-01T00:00:00Z"},
		{Name: "end_time", Description: "Exclusive end of the time range of the pricing versions, an RFC 3339 timestamp"},
		{Name: "currency_code", Description: "ISO 4217 code of the currency of the prices, three uppercase letters " +
			"such as EUR; the rate is that of start_time"},
		pageSizeArgument, cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "skus", Description: "SKUs of the page, each {name, skuId, description, category {serviceDisplayName, " +
			"resourceFamily, resourceGroup, usageType}, serviceRegions, pricingInfo [{summary, pricingExpression " +
			"{usageUnit, usageUnitDescription, baseUnit, baseUnitDescription, baseUnitConversionFactor, " +
			"displayQuantity, tieredRates [{startUsageAmount, unitPrice {currencyCode, units, nanos}}]}, " +
			"currencyConversionRate, effectiveTime, aggregationInfo}], geoTaxonomy {type, regions}, " +
			"serviceProviderName}; Google leaves out a zero units or nanos; texts are untrusted"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the SKUs of a service in euros",
		Arguments:   json.RawMessage(`{"service_id":"services/6F81-5844-456A","currency_code":"EUR","page_size":50}`),
	}},
}

// checkTimestamp validates an RFC 3339 timestamp argument before any request.
func checkTimestamp(name, value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return badRequest(name + " must be an RFC 3339 timestamp such as 2026-11-01T00:00:00Z")
	}
	return nil
}

func handleCatalogSkusList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list catalog SKUs"
	var arguments struct {
		ServiceID    string `json:"service_id"`
		StartTime    string `json:"start_time"`
		EndTime      string `json:"end_time"`
		CurrencyCode string `json:"currency_code"`
		PageSize     *int   `json:"page_size"`
		Cursor       string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	service := strings.TrimPrefix(arguments.ServiceID, "services/")
	if !catalogServicePattern.MatchString(service) {
		return nil, badRequest("service_id must be services/XXXX-XXXX-XXXX or XXXX-XXXX-XXXX, never a path")
	}
	for name, value := range map[string]string{"start_time": arguments.StartTime, "end_time": arguments.EndTime} {
		if err := checkTimestamp(name, value); err != nil {
			return nil, err
		}
	}
	if arguments.CurrencyCode != "" && !currencyPattern.MatchString(arguments.CurrencyCode) {
		return nil, badRequest("currency_code must be an ISO 4217 code of three uppercase letters")
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	binding := provider.CursorBinding(catalogSkusListID, provider.TargetsOf(resolved),
		strings.Join([]string{service, arguments.StartTime, arguments.EndTime, arguments.CurrencyCode}, "|"))
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{"pageSize": {catalogPageSize(arguments.PageSize)}}
	for key, value := range map[string]string{"startTime": arguments.StartTime, "endTime": arguments.EndTime,
		"currencyCode": arguments.CurrencyCode, "pageToken": token} {
		if value != "" {
			query.Set(key, value)
		}
	}
	rows, next, err := servicePage[skuWire](ctx, c, serviceBilling, op, catalogSkusListID,
		"/v1/services/"+url.PathEscape(service)+"/skus", "skus", query)
	if err != nil {
		return nil, err
	}
	kept := make([]any, 0, len(rows))
	for _, row := range rows {
		if !strings.HasPrefix(row.Name, "services/"+service+"/skus/") {
			return nil, invalidRow(op)
		}
		kept = append(kept, row.bounded())
	}
	out := map[string]any{"skus": kept, "has_more": next != ""}
	if next != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, next)
	}
	return out, nil
}
