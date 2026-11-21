package googleads

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountLinksListID           = Provider + ".account-links.list"
	productLinksListID           = Provider + ".product-links.list"
	productLinkInvitationsListID = Provider + ".product-link-invitations.list"
	dataLinksListID              = Provider + ".data-links.list"
)

// The four lists of this file read what is linked to an account: app analytics providers, products such as
// Merchant Center, invitations to link a product, and data such as YouTube videos. Each query selects one fixed
// set of fields that is defined here; no argument adds a field, a resource, or a condition other than the
// structured filters, and the fixed sets hold every field of the resource that v26 lets a query select.

// The values of the enums in v26 that a filter may name. UNSPECIFIED and UNKNOWN are no values to ask for.
var (
	accountLinkStatuses   = []string{"ENABLED", "PENDING_APPROVAL", "REJECTED", "REMOVED", "REQUESTED", "REVOKED"}
	accountLinkTypes      = []string{"THIRD_PARTY_APP_ANALYTICS"}
	appVendors            = []string{"APPLE_APP_STORE", "GOOGLE_APP_STORE"}
	productLinkTypes      = []string{"ADVERTISING_PARTNER", "DATA_PARTNER", "GOOGLE_ADS", "HOTEL_CENTER", "MERCHANT_CENTER"}
	productInviteStatuses = []string{"ACCEPTED", "EXPIRED", "PENDING_APPROVAL", "REJECTED", "REQUESTED", "REVOKED"}
	dataLinkStatuses      = []string{"DISABLED", "ENABLED", "PENDING_APPROVAL", "REJECTED", "REQUESTED", "REVOKED"}
	dataLinkTypes         = []string{"VIDEO"}
)

// The fixed field sets, relative to the resource.
var (
	accountLinkFields = []string{"resource_name", "account_link_id", "type", "status",
		"third_party_app_analytics.app_analytics_provider_id", "third_party_app_analytics.app_id",
		"third_party_app_analytics.app_vendor"}
	productLinkFields = []string{"resource_name", "product_link_id", "type", "data_partner.data_partner_id",
		"google_ads.customer", "merchant_center.merchant_center_id", "advertising_partner.customer",
		"advertising_partner_properties.allowed_domain"}
	productInviteFields = []string{"resource_name", "product_link_invitation_id", "type", "status",
		"merchant_center.merchant_center_id", "hotel_center.hotel_center_id", "advertising_partner.customer",
		"advertising_partner_properties.allowed_domain"}
	dataLinkFields = []string{"resource_name", "data_link_id", "product_link_id", "type", "status",
		"youtube_video.channel_id", "youtube_video.video_id"}
)

var (
	appIDPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)
	domainPattern    = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`)
	channelIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	videoIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	// customerRefPattern is a customer resource name an answer holds for the linked account.
	customerRefPattern = regexp.MustCompile(`^customers/[0-9]{1,10}$`)
	// dataLinkKeyPattern is the keyset of a data link: the product link ID and the data link ID.
	dataLinkKeyPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}~[1-9][0-9]{0,18}$`)
)

// conditionSet collects the GAQL conditions of the structured filters and keeps the first refusal.
type conditionSet struct {
	conditions []string
	err        error
}

func (c *conditionSet) add(condition string, err error) {
	if c.err == nil && err == nil && condition != "" {
		c.conditions = append(c.conditions, condition)
	} else if c.err == nil {
		c.err = err
	}
}

func (c *conditionSet) enum(argument, field string, values, allowed []string) {
	condition, err := enumList(argument, field, values, allowed)
	c.add(condition, err)
}

func (c *conditionSet) ids(argument, field string, values []string) {
	condition, err := idList(argument, field, values)
	c.add(condition, err)
}

// customer is an equality on a customer resource name built from a checked ten-digit customer ID.
func (c *conditionSet) customer(argument, field, value string) {
	if value == "" {
		return
	}
	if !customerIDPattern.MatchString(value) {
		c.add("", badRequest(argument+" must be a ten-digit customer ID without hyphens"))
		return
	}
	c.add(field+" = "+gaqlString(customerName(value)), nil)
}

// text is an equality on a string that matches the pattern, which excludes quotes, backslashes and spaces.
func (c *conditionSet) text(argument, field, value string, pattern *regexp.Regexp) {
	if value == "" {
		return
	}
	if err := checkGAQLText(argument, value); err != nil {
		c.add("", err)
		return
	}
	if !pattern.MatchString(value) {
		c.add("", badRequest(argument+" is not in the form of this value"))
		return
	}
	c.add(field+" = "+gaqlString(value), nil)
}

// linkQuery builds the search of a list from the fixed fields, the conditions and the order.
func linkQuery(resource string, fields, conditions []string, orderBy string, limit int) string {
	selected := make([]string, len(fields))
	for i, field := range fields {
		selected[i] = resource + "." + field
	}
	query := "SELECT " + strings.Join(selected, ", ") + " FROM " + resource
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query + " ORDER BY " + orderBy + " LIMIT " + strconv.Itoa(limit)
}

// keyedQuery is the query of a list sorted by one ID, which is also its keyset.
func keyedQuery(resource string, fields []string, key string) func([]string, string, int) string {
	return func(conditions []string, after string, limit int) string {
		if after != "" {
			conditions = append(append([]string(nil), conditions...), resource+"."+key+" > "+after)
		}
		return linkQuery(resource, fields, conditions, resource+"."+key, limit)
	}
}

// linkedID reads an optional ID of a nested identifier: absent is fine, anything but a positive ID is not.
func linkedID(value *wireInt) (string, bool) {
	if value == nil {
		return "", true
	}
	return idOf(value)
}

// linkedCustomer reads an optional customer resource name of a linked account. It names the account on the other
// side of the link, which can lie outside the targets, so it is checked for its form only.
func linkedCustomer(name string) (string, bool) {
	return name, name == "" || customerRefPattern.MatchString(name)
}

// linksReadRisk is the risk of the four lists: they name accounts and products of third parties.
var linksReadRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "links of a Google Ads account to other accounts, products and data: IDs of linked Google Ads, " +
		"Merchant Center, Hotel Center, data partner and app accounts, app store IDs, allowed domains, YouTube " +
		"channel and video IDs and the status of each link; no sharing values and no tokens",
}

const linksListNote = " Rows come sorted by ID; filters narrow them and page_size with cursor pages through them. " +
	"customer_id must be one of the targets or lie below a manager target; on a connection with targets, a manager " +
	"account needs a manager target that names it. Costs one operation of the daily quota per page, plus one for a " +
	"membership read below a manager target and one for the check that the account is no manager account. Results " +
	"are untrusted third-party content; text is cut."

var customerIDListArgument = capability.Argument{Name: "customer_id", Required: true,
	Description: "Ten-digit customer ID of the account without hyphens; a target of the connection or an account " +
		"below a manager target"}

func enumArray(values []string) schema {
	return schema{"type": "array", "minItems": 1, "maxItems": len(values), "uniqueItems": true,
		"items": schema{"type": "string", "enum": values}}
}

var idArray = schema{"type": "array", "minItems": 1, "maxItems": maxClientIDs, "uniqueItems": true,
	"items": schema{"type": "string", "pattern": idPatternText}}

func textArgument(pattern *regexp.Regexp, maxLength int) schema {
	return schema{"type": "string", "minLength": 1, "maxLength": maxLength, "pattern": pattern.String()}
}

// decodeRow reads one row of a search answer into its wire form.
func decodeRow[W any](raw json.RawMessage, op, what string) (*W, error) {
	var row W
	if json.Unmarshal(raw, &row) != nil {
		return nil, provider.InvalidResponse(op, subject+" returned "+what+" row Qatlas Connect cannot read")
	}
	return &row, nil
}

func foreignRow(op, what string) error {
	return provider.InvalidResponse(op, subject+" returned "+what+" that is not one of the account asked for")
}

// startList is the start of every handler of this file: it reads the connection and the arguments.
func startList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string, args any, customerID func() string) (callEnv, error) {
	if err := json.Unmarshal(raw, args); err != nil {
		return callEnv{}, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return callEnv{}, err
	}
	if !customerIDPattern.MatchString(customerID()) {
		return callEnv{}, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	return env, nil
}

// ---- account-links.list

type accountLinksArguments struct {
	CustomerID              string   `json:"customer_id"`
	Statuses                []string `json:"statuses"`
	Types                   []string `json:"types"`
	AccountLinkIDs          []string `json:"account_link_ids"`
	AppVendors              []string `json:"app_vendors"`
	AppID                   string   `json:"app_id"`
	AppAnalyticsProviderIDs []string `json:"app_analytics_provider_ids"`
	PageSize                *int     `json:"page_size"`
	Cursor                  string   `json:"cursor"`
}

func (a accountLinksArguments) conditions() ([]string, error) {
	var c conditionSet
	c.enum("statuses", "account_link.status", a.Statuses, accountLinkStatuses)
	c.enum("types", "account_link.type", a.Types, accountLinkTypes)
	c.ids("account_link_ids", "account_link.account_link_id", a.AccountLinkIDs)
	c.enum("app_vendors", "account_link.third_party_app_analytics.app_vendor", a.AppVendors, appVendors)
	c.text("app_id", "account_link.third_party_app_analytics.app_id", a.AppID, appIDPattern)
	c.ids("app_analytics_provider_ids", "account_link.third_party_app_analytics.app_analytics_provider_id",
		a.AppAnalyticsProviderIDs)
	return c.conditions, c.err
}

type (
	accountLinkWire struct {
		ResourceName  string   `json:"resourceName"`
		AccountLinkID *wireInt `json:"accountLinkId"`
		Type          string   `json:"type"`
		Status        string   `json:"status"`
		ThirdParty    *struct {
			ProviderID *wireInt `json:"appAnalyticsProviderId"`
			AppID      string   `json:"appId"`
			AppVendor  string   `json:"appVendor"`
		} `json:"thirdPartyAppAnalytics"`
	}
	accountLinkRow struct {
		AccountLink *accountLinkWire `json:"accountLink"`
	}
	appAnalyticsResult struct {
		AppAnalyticsProviderID string `json:"app_analytics_provider_id,omitempty"`
		AppID                  string `json:"app_id,omitempty"`
		AppVendor              string `json:"app_vendor,omitempty"`
	}
	accountLinkResult struct {
		ResourceName           string              `json:"resource_name"`
		AccountLinkID          string              `json:"account_link_id"`
		Type                   string              `json:"type,omitempty"`
		Status                 string              `json:"status,omitempty"`
		ThirdPartyAppAnalytics *appAnalyticsResult `json:"third_party_app_analytics,omitempty"`
	}
	accountLinksResult struct {
		AccountLinks []accountLinkResult `json:"account_links"`
		NextCursor   string              `json:"next_cursor,omitempty"`
		HasMore      bool                `json:"has_more"`
	}
)

func accountLinkName(customerID, id string) string {
	return customerName(customerID) + "/accountLinks/" + id
}

// accountLinkOf maps a row. Its resource name must be that of the account of the call and its own ID.
func accountLinkOf(w *accountLinkWire, customerID string) (accountLinkResult, bool) {
	id, ok := idOf(w.AccountLinkID)
	if !ok || (w.ResourceName != "" && w.ResourceName != accountLinkName(customerID, id)) {
		return accountLinkResult{}, false
	}
	out := accountLinkResult{ResourceName: accountLinkName(customerID, id), AccountLinkID: id, Type: code(w.Type),
		Status: code(w.Status)}
	if t := w.ThirdParty; t != nil {
		providerID, ok := linkedID(t.ProviderID)
		if !ok {
			return accountLinkResult{}, false
		}
		out.ThirdPartyAppAnalytics = &appAnalyticsResult{AppAnalyticsProviderID: providerID,
			AppID: boundedText(t.AppID, maxText), AppVendor: code(t.AppVendor)}
	}
	return out, true
}

var appAnalyticsSchema = object(schema{"app_analytics_provider_id": stringSchema, "app_id": stringSchema,
	"app_vendor": stringSchema})

var accountLinksList = capability.Descriptor{
	ID: accountLinksListID, Version: 1, Title: "List the account links of a Google Ads account",
	Description: "List the links of a Google Ads account to third-party app analytics providers (a GAQL search " +
		"FROM account_link of API version v26 with a fixed set of fields): ID, type, status (ENABLED, " +
		"PENDING_APPROVAL, REJECTED, REMOVED, REQUESTED, REVOKED) and the app analytics provider ID, app ID and app " +
		"store of the link. The sharing value of a third-party app analytics link belongs to another resource and " +
		"is not part of this answer." + linksListNote,
	Tags: []string{"google", "ads", "links", "account-links"}, Risk: linksReadRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id":                customerIDArgument,
		"statuses":                   enumArray(accountLinkStatuses),
		"types":                      enumArray(accountLinkTypes),
		"account_link_ids":           idArray,
		"app_vendors":                enumArray(appVendors),
		"app_id":                     textArgument(appIDPattern, 255),
		"app_analytics_provider_ids": idArray,
		"page_size":                  pageProperties["page_size"], "cursor": pageProperties["cursor"],
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"account_links": arrayOf(object(schema{"resource_name": stringSchema,
		"account_link_id": stringSchema, "type": stringSchema, "status": stringSchema,
		"third_party_app_analytics": appAnalyticsSchema}, "resource_name", "account_link_id")),
		"next_cursor": stringSchema, "has_more": booleanSchema}, "account_links", "has_more")),
	Arguments: append([]capability.Argument{customerIDListArgument,
		{Name: "statuses", Description: "Only links with one of these statuses: ENABLED, PENDING_APPROVAL, REJECTED, " +
			"REMOVED, REQUESTED, REVOKED"},
		{Name: "types", Description: "Only links of this type: THIRD_PARTY_APP_ANALYTICS"},
		{Name: "account_link_ids", Description: "Only these link IDs, at most 100"},
		{Name: "app_vendors", Description: "Only links to an app of this store: APPLE_APP_STORE, GOOGLE_APP_STORE"},
		{Name: "app_id", Description: "Only links to this app: the 9-digit App Store ID or the Android package name"},
		{Name: "app_analytics_provider_ids", Description: "Only links of these app analytics providers, at most 100"},
	}, pageArguments...),
	Fields: []capability.Field{
		{Name: "account_links", Description: "Per link: resource_name (customers/ID/accountLinks/LINK_ID), " +
			"account_link_id, type, status, and third_party_app_analytics with app_analytics_provider_id, app_id " +
			"and app_vendor"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the enabled app analytics links of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","statuses":["ENABLED"]}`)}},
}

func handleAccountLinksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list account links"
	var args accountLinksArguments
	env, err := startList(ctx, resolved, secrets, red, raw, op, accountLinksListID, &args,
		func() string { return args.CustomerID })
	if err != nil {
		return nil, err
	}
	conditions, err := args.conditions()
	if err != nil {
		return nil, err
	}
	items, next, more, err := listAccess(env, listSpec{customerID: args.CustomerID, tool: env.tool, op: op,
		conditions: conditions, pageSize: args.PageSize, cursor: args.Cursor,
		query: keyedQuery("account_link", accountLinkFields, "account_link_id")},
		func(raw json.RawMessage) (accountLinkResult, string, error) {
			row, err := decodeRow[accountLinkRow](raw, op, "an account link")
			if err != nil {
				return accountLinkResult{}, "", err
			}
			if row.AccountLink == nil {
				return accountLinkResult{}, "", foreignRow(op, "an account link")
			}
			item, ok := accountLinkOf(row.AccountLink, args.CustomerID)
			if !ok {
				return accountLinkResult{}, "", foreignRow(op, "an account link")
			}
			return item, item.AccountLinkID, nil
		})
	if err != nil {
		return nil, err
	}
	return accountLinksResult{AccountLinks: items, NextCursor: next, HasMore: more}, nil
}

// ---- product-links.list

type productLinksArguments struct {
	CustomerID                   string   `json:"customer_id"`
	Types                        []string `json:"types"`
	ProductLinkIDs               []string `json:"product_link_ids"`
	DataPartnerIDs               []string `json:"data_partner_ids"`
	MerchantCenterIDs            []string `json:"merchant_center_ids"`
	GoogleAdsCustomerID          string   `json:"google_ads_customer_id"`
	AdvertisingPartnerCustomerID string   `json:"advertising_partner_customer_id"`
	AllowedDomain                string   `json:"allowed_domain"`
	PageSize                     *int     `json:"page_size"`
	Cursor                       string   `json:"cursor"`
}

func (a productLinksArguments) conditions() ([]string, error) {
	var c conditionSet
	c.enum("types", "product_link.type", a.Types, productLinkTypes)
	c.ids("product_link_ids", "product_link.product_link_id", a.ProductLinkIDs)
	c.ids("data_partner_ids", "product_link.data_partner.data_partner_id", a.DataPartnerIDs)
	c.ids("merchant_center_ids", "product_link.merchant_center.merchant_center_id", a.MerchantCenterIDs)
	c.customer("google_ads_customer_id", "product_link.google_ads.customer", a.GoogleAdsCustomerID)
	c.customer("advertising_partner_customer_id", "product_link.advertising_partner.customer",
		a.AdvertisingPartnerCustomerID)
	c.text("allowed_domain", "product_link.advertising_partner_properties.allowed_domain", a.AllowedDomain, domainPattern)
	return c.conditions, c.err
}

type (
	linkedCustomerWire struct {
		Customer string `json:"customer"`
	}
	allowedDomainWire struct {
		AllowedDomain string `json:"allowedDomain"`
	}
	productLinkWire struct {
		ResourceName  string   `json:"resourceName"`
		ProductLinkID *wireInt `json:"productLinkId"`
		Type          string   `json:"type"`
		DataPartner   *struct {
			DataPartnerID *wireInt `json:"dataPartnerId"`
		} `json:"dataPartner"`
		GoogleAds      *linkedCustomerWire `json:"googleAds"`
		MerchantCenter *struct {
			MerchantCenterID *wireInt `json:"merchantCenterId"`
		} `json:"merchantCenter"`
		AdvertisingPartner           *linkedCustomerWire `json:"advertisingPartner"`
		AdvertisingPartnerProperties *allowedDomainWire  `json:"advertisingPartnerProperties"`
	}
	productLinkRow struct {
		ProductLink *productLinkWire `json:"productLink"`
	}
	linkedCustomerResult struct {
		Customer string `json:"customer"`
	}
	allowedDomainResult struct {
		AllowedDomain string `json:"allowed_domain"`
	}
	productLinkResult struct {
		ResourceName                 string                `json:"resource_name"`
		ProductLinkID                string                `json:"product_link_id"`
		Type                         string                `json:"type,omitempty"`
		DataPartnerID                string                `json:"data_partner_id,omitempty"`
		GoogleAds                    *linkedCustomerResult `json:"google_ads,omitempty"`
		MerchantCenterID             string                `json:"merchant_center_id,omitempty"`
		AdvertisingPartner           *linkedCustomerResult `json:"advertising_partner,omitempty"`
		AdvertisingPartnerProperties *allowedDomainResult  `json:"advertising_partner_properties,omitempty"`
	}
	productLinksResult struct {
		ProductLinks []productLinkResult `json:"product_links"`
		NextCursor   string              `json:"next_cursor,omitempty"`
		HasMore      bool                `json:"has_more"`
	}
)

func productLinkName(customerID, id string) string {
	return customerName(customerID) + "/productLinks/" + id
}

// linkedCustomerOf maps the identifier of a linked account.
func linkedCustomerOf(w *linkedCustomerWire) (*linkedCustomerResult, bool) {
	if w == nil {
		return nil, true
	}
	name, ok := linkedCustomer(w.Customer)
	return &linkedCustomerResult{Customer: name}, ok
}

func allowedDomainOf(w *allowedDomainWire) *allowedDomainResult {
	if w == nil {
		return nil
	}
	return &allowedDomainResult{AllowedDomain: boundedText(w.AllowedDomain, maxText)}
}

func productLinkOf(w *productLinkWire, customerID string) (productLinkResult, bool) {
	id, ok := idOf(w.ProductLinkID)
	if !ok || (w.ResourceName != "" && w.ResourceName != productLinkName(customerID, id)) {
		return productLinkResult{}, false
	}
	out := productLinkResult{ResourceName: productLinkName(customerID, id), ProductLinkID: id, Type: code(w.Type),
		AdvertisingPartnerProperties: allowedDomainOf(w.AdvertisingPartnerProperties)}
	if w.DataPartner != nil {
		if out.DataPartnerID, ok = linkedID(w.DataPartner.DataPartnerID); !ok {
			return productLinkResult{}, false
		}
	}
	if w.MerchantCenter != nil {
		if out.MerchantCenterID, ok = linkedID(w.MerchantCenter.MerchantCenterID); !ok {
			return productLinkResult{}, false
		}
	}
	if out.GoogleAds, ok = linkedCustomerOf(w.GoogleAds); !ok {
		return productLinkResult{}, false
	}
	if out.AdvertisingPartner, ok = linkedCustomerOf(w.AdvertisingPartner); !ok {
		return productLinkResult{}, false
	}
	return out, true
}

var (
	linkedCustomerSchema = object(schema{"customer": stringSchema})
	allowedDomainSchema  = object(schema{"allowed_domain": stringSchema})
)

var productLinksList = capability.Descriptor{
	ID: productLinksListID, Version: 1, Title: "List the product links of a Google Ads account",
	Description: "List the links of a Google Ads account to products (a GAQL search FROM product_link of API " +
		"version v26 with a fixed set of fields): ID, type (ADVERTISING_PARTNER, DATA_PARTNER, GOOGLE_ADS, " +
		"HOTEL_CENTER, MERCHANT_CENTER), and the identifier of the linked product: data partner ID, Google Ads " +
		"account (customers/ID), Merchant Center ID, advertising partner account and the domain an advertising " +
		"partner may advertise on. A link has no status; a link that exists is active." + linksListNote,
	Tags: []string{"google", "ads", "links", "product-links"}, Risk: linksReadRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id":                     customerIDArgument,
		"types":                           enumArray(productLinkTypes),
		"product_link_ids":                idArray,
		"data_partner_ids":                idArray,
		"merchant_center_ids":             idArray,
		"google_ads_customer_id":          customerIDArgument,
		"advertising_partner_customer_id": customerIDArgument,
		"allowed_domain":                  textArgument(domainPattern, 253),
		"page_size":                       pageProperties["page_size"], "cursor": pageProperties["cursor"],
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"product_links": arrayOf(object(schema{"resource_name": stringSchema,
		"product_link_id": stringSchema, "type": stringSchema, "data_partner_id": stringSchema,
		"google_ads": linkedCustomerSchema, "merchant_center_id": stringSchema,
		"advertising_partner": linkedCustomerSchema, "advertising_partner_properties": allowedDomainSchema},
		"resource_name", "product_link_id")), "next_cursor": stringSchema, "has_more": booleanSchema},
		"product_links", "has_more")),
	Arguments: append([]capability.Argument{customerIDListArgument,
		{Name: "types", Description: "Only links of these types: ADVERTISING_PARTNER, DATA_PARTNER, GOOGLE_ADS, " +
			"HOTEL_CENTER, MERCHANT_CENTER"},
		{Name: "product_link_ids", Description: "Only these link IDs, at most 100"},
		{Name: "data_partner_ids", Description: "Only links to these data partners, at most 100"},
		{Name: "merchant_center_ids", Description: "Only links to these Merchant Center accounts, at most 100"},
		{Name: "google_ads_customer_id", Description: "Only links to this Google Ads account, ten digits without hyphens"},
		{Name: "advertising_partner_customer_id", Description: "Only links to this advertising partner account, " +
			"ten digits without hyphens"},
		{Name: "allowed_domain", Description: "Only links whose advertising partner may advertise on exactly this domain"},
	}, pageArguments...),
	Fields: []capability.Field{
		{Name: "product_links", Description: "Per link: resource_name (customers/ID/productLinks/LINK_ID), " +
			"product_link_id, type, and the identifier of the linked product that applies: data_partner_id, " +
			"google_ads.customer, merchant_center_id, advertising_partner.customer, " +
			"advertising_partner_properties.allowed_domain. The customer of a linked Google Ads or advertising " +
			"partner account can lie outside the targets and is only an identifier"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the Merchant Center links of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","types":["MERCHANT_CENTER"]}`)}},
}

func handleProductLinksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list product links"
	var args productLinksArguments
	env, err := startList(ctx, resolved, secrets, red, raw, op, productLinksListID, &args,
		func() string { return args.CustomerID })
	if err != nil {
		return nil, err
	}
	conditions, err := args.conditions()
	if err != nil {
		return nil, err
	}
	items, next, more, err := listAccess(env, listSpec{customerID: args.CustomerID, tool: env.tool, op: op,
		conditions: conditions, pageSize: args.PageSize, cursor: args.Cursor,
		query: keyedQuery("product_link", productLinkFields, "product_link_id")},
		func(raw json.RawMessage) (productLinkResult, string, error) {
			row, err := decodeRow[productLinkRow](raw, op, "a product link")
			if err != nil {
				return productLinkResult{}, "", err
			}
			if row.ProductLink == nil {
				return productLinkResult{}, "", foreignRow(op, "a product link")
			}
			item, ok := productLinkOf(row.ProductLink, args.CustomerID)
			if !ok {
				return productLinkResult{}, "", foreignRow(op, "a product link")
			}
			return item, item.ProductLinkID, nil
		})
	if err != nil {
		return nil, err
	}
	return productLinksResult{ProductLinks: items, NextCursor: next, HasMore: more}, nil
}

// ---- product-link-invitations.list

type productInvitationsArguments struct {
	CustomerID                   string   `json:"customer_id"`
	Statuses                     []string `json:"statuses"`
	Types                        []string `json:"types"`
	InvitationIDs                []string `json:"product_link_invitation_ids"`
	MerchantCenterIDs            []string `json:"merchant_center_ids"`
	HotelCenterIDs               []string `json:"hotel_center_ids"`
	AdvertisingPartnerCustomerID string   `json:"advertising_partner_customer_id"`
	AllowedDomain                string   `json:"allowed_domain"`
	PageSize                     *int     `json:"page_size"`
	Cursor                       string   `json:"cursor"`
}

func (a productInvitationsArguments) conditions() ([]string, error) {
	var c conditionSet
	c.enum("statuses", "product_link_invitation.status", a.Statuses, productInviteStatuses)
	c.enum("types", "product_link_invitation.type", a.Types, productLinkTypes)
	c.ids("product_link_invitation_ids", "product_link_invitation.product_link_invitation_id", a.InvitationIDs)
	c.ids("merchant_center_ids", "product_link_invitation.merchant_center.merchant_center_id", a.MerchantCenterIDs)
	c.ids("hotel_center_ids", "product_link_invitation.hotel_center.hotel_center_id", a.HotelCenterIDs)
	c.customer("advertising_partner_customer_id", "product_link_invitation.advertising_partner.customer",
		a.AdvertisingPartnerCustomerID)
	c.text("allowed_domain", "product_link_invitation.advertising_partner_properties.allowed_domain", a.AllowedDomain,
		domainPattern)
	return c.conditions, c.err
}

type (
	productInvitationWire struct {
		ResourceName string   `json:"resourceName"`
		InvitationID *wireInt `json:"productLinkInvitationId"`
		Type         string   `json:"type"`
		Status       string   `json:"status"`
		Merchant     *struct {
			MerchantCenterID *wireInt `json:"merchantCenterId"`
		} `json:"merchantCenter"`
		Hotel *struct {
			HotelCenterID *wireInt `json:"hotelCenterId"`
		} `json:"hotelCenter"`
		AdvertisingPartner           *linkedCustomerWire `json:"advertisingPartner"`
		AdvertisingPartnerProperties *allowedDomainWire  `json:"advertisingPartnerProperties"`
	}
	productInvitationRow struct {
		Invitation *productInvitationWire `json:"productLinkInvitation"`
	}
	productInvitationResult struct {
		ResourceName                 string                `json:"resource_name"`
		InvitationID                 string                `json:"product_link_invitation_id"`
		Type                         string                `json:"type,omitempty"`
		Status                       string                `json:"status,omitempty"`
		MerchantCenterID             string                `json:"merchant_center_id,omitempty"`
		HotelCenterID                string                `json:"hotel_center_id,omitempty"`
		AdvertisingPartner           *linkedCustomerResult `json:"advertising_partner,omitempty"`
		AdvertisingPartnerProperties *allowedDomainResult  `json:"advertising_partner_properties,omitempty"`
	}
	productInvitationsResult struct {
		Invitations []productInvitationResult `json:"product_link_invitations"`
		NextCursor  string                    `json:"next_cursor,omitempty"`
		HasMore     bool                      `json:"has_more"`
	}
)

func productInvitationName(customerID, id string) string {
	return customerName(customerID) + "/productLinkInvitations/" + id
}

func productInvitationOf(w *productInvitationWire, customerID string) (productInvitationResult, bool) {
	id, ok := idOf(w.InvitationID)
	if !ok || (w.ResourceName != "" && w.ResourceName != productInvitationName(customerID, id)) {
		return productInvitationResult{}, false
	}
	out := productInvitationResult{ResourceName: productInvitationName(customerID, id), InvitationID: id,
		Type: code(w.Type), Status: code(w.Status), AdvertisingPartnerProperties: allowedDomainOf(w.AdvertisingPartnerProperties)}
	if w.Merchant != nil {
		if out.MerchantCenterID, ok = linkedID(w.Merchant.MerchantCenterID); !ok {
			return productInvitationResult{}, false
		}
	}
	if w.Hotel != nil {
		if out.HotelCenterID, ok = linkedID(w.Hotel.HotelCenterID); !ok {
			return productInvitationResult{}, false
		}
	}
	if out.AdvertisingPartner, ok = linkedCustomerOf(w.AdvertisingPartner); !ok {
		return productInvitationResult{}, false
	}
	return out, true
}

var productLinkInvitationsList = capability.Descriptor{
	ID: productLinkInvitationsListID, Version: 1, Title: "List the product link invitations of a Google Ads account",
	Description: "List the invitations to link a product to a Google Ads account (a GAQL search FROM " +
		"product_link_invitation of API version v26 with a fixed set of fields): ID, type (ADVERTISING_PARTNER, " +
		"DATA_PARTNER, GOOGLE_ADS, HOTEL_CENTER, MERCHANT_CENTER), status (ACCEPTED, EXPIRED, PENDING_APPROVAL, " +
		"REJECTED, REQUESTED, REVOKED) and the identifier of the invited account: Merchant Center ID, Hotel Center " +
		"ID, advertising partner account and the domain the partner may advertise on." + linksListNote,
	Tags: []string{"google", "ads", "links", "product-link-invitations"}, Risk: linksReadRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id":                     customerIDArgument,
		"statuses":                        enumArray(productInviteStatuses),
		"types":                           enumArray(productLinkTypes),
		"product_link_invitation_ids":     idArray,
		"merchant_center_ids":             idArray,
		"hotel_center_ids":                idArray,
		"advertising_partner_customer_id": customerIDArgument,
		"allowed_domain":                  textArgument(domainPattern, 253),
		"page_size":                       pageProperties["page_size"], "cursor": pageProperties["cursor"],
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"product_link_invitations": arrayOf(object(schema{"resource_name": stringSchema,
		"product_link_invitation_id": stringSchema, "type": stringSchema, "status": stringSchema,
		"merchant_center_id": stringSchema, "hotel_center_id": stringSchema,
		"advertising_partner": linkedCustomerSchema, "advertising_partner_properties": allowedDomainSchema},
		"resource_name", "product_link_invitation_id")), "next_cursor": stringSchema, "has_more": booleanSchema},
		"product_link_invitations", "has_more")),
	Arguments: append([]capability.Argument{customerIDListArgument,
		{Name: "statuses", Description: "Only invitations with one of these statuses: ACCEPTED, EXPIRED, " +
			"PENDING_APPROVAL, REJECTED, REQUESTED, REVOKED"},
		{Name: "types", Description: "Only invitations of these types: ADVERTISING_PARTNER, DATA_PARTNER, GOOGLE_ADS, " +
			"HOTEL_CENTER, MERCHANT_CENTER"},
		{Name: "product_link_invitation_ids", Description: "Only these invitation IDs, at most 100"},
		{Name: "merchant_center_ids", Description: "Only invitations to these Merchant Center accounts, at most 100"},
		{Name: "hotel_center_ids", Description: "Only invitations to these Hotel Center accounts, at most 100"},
		{Name: "advertising_partner_customer_id", Description: "Only invitations to this advertising partner account, " +
			"ten digits without hyphens"},
		{Name: "allowed_domain", Description: "Only invitations whose advertising partner may advertise on exactly this domain"},
	}, pageArguments...),
	Fields: []capability.Field{
		{Name: "product_link_invitations", Description: "Per invitation: resource_name (customers/ID/" +
			"productLinkInvitations/INVITATION_ID), product_link_invitation_id, type, status, and the identifier of " +
			"the invited account that applies: merchant_center_id, hotel_center_id, advertising_partner.customer, " +
			"advertising_partner_properties.allowed_domain. The customer of an advertising partner can lie outside " +
			"the targets and is only an identifier"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the pending invitations of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","statuses":["PENDING_APPROVAL","REQUESTED"]}`)}},
}

func handleProductLinkInvitationsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list product link invitations"
	var args productInvitationsArguments
	env, err := startList(ctx, resolved, secrets, red, raw, op, productLinkInvitationsListID, &args,
		func() string { return args.CustomerID })
	if err != nil {
		return nil, err
	}
	conditions, err := args.conditions()
	if err != nil {
		return nil, err
	}
	items, next, more, err := listAccess(env, listSpec{customerID: args.CustomerID, tool: env.tool, op: op,
		conditions: conditions, pageSize: args.PageSize, cursor: args.Cursor,
		query: keyedQuery("product_link_invitation", productInviteFields, "product_link_invitation_id")},
		func(raw json.RawMessage) (productInvitationResult, string, error) {
			row, err := decodeRow[productInvitationRow](raw, op, "a product link invitation")
			if err != nil {
				return productInvitationResult{}, "", err
			}
			if row.Invitation == nil {
				return productInvitationResult{}, "", foreignRow(op, "a product link invitation")
			}
			item, ok := productInvitationOf(row.Invitation, args.CustomerID)
			if !ok {
				return productInvitationResult{}, "", foreignRow(op, "a product link invitation")
			}
			return item, item.InvitationID, nil
		})
	if err != nil {
		return nil, err
	}
	return productInvitationsResult{Invitations: items, NextCursor: next, HasMore: more}, nil
}

// ---- data-links.list

type dataLinksArguments struct {
	CustomerID     string   `json:"customer_id"`
	Statuses       []string `json:"statuses"`
	Types          []string `json:"types"`
	DataLinkIDs    []string `json:"data_link_ids"`
	ProductLinkIDs []string `json:"product_link_ids"`
	ChannelID      string   `json:"channel_id"`
	VideoID        string   `json:"video_id"`
	PageSize       *int     `json:"page_size"`
	Cursor         string   `json:"cursor"`
}

func (a dataLinksArguments) conditions() ([]string, error) {
	var c conditionSet
	c.enum("statuses", "data_link.status", a.Statuses, dataLinkStatuses)
	c.enum("types", "data_link.type", a.Types, dataLinkTypes)
	c.ids("data_link_ids", "data_link.data_link_id", a.DataLinkIDs)
	c.ids("product_link_ids", "data_link.product_link_id", a.ProductLinkIDs)
	c.text("channel_id", "data_link.youtube_video.channel_id", a.ChannelID, channelIDPattern)
	c.text("video_id", "data_link.youtube_video.video_id", a.VideoID, videoIDPattern)
	return c.conditions, c.err
}

type (
	dataLinkWire struct {
		ResourceName  string   `json:"resourceName"`
		DataLinkID    *wireInt `json:"dataLinkId"`
		ProductLinkID *wireInt `json:"productLinkId"`
		Type          string   `json:"type"`
		Status        string   `json:"status"`
		YoutubeVideo  *struct {
			ChannelID string `json:"channelId"`
			VideoID   string `json:"videoId"`
		} `json:"youtubeVideo"`
	}
	dataLinkRow struct {
		DataLink *dataLinkWire `json:"dataLink"`
	}
	youtubeVideoResult struct {
		ChannelID string `json:"channel_id,omitempty"`
		VideoID   string `json:"video_id,omitempty"`
	}
	dataLinkResult struct {
		ResourceName  string              `json:"resource_name"`
		DataLinkID    string              `json:"data_link_id"`
		ProductLinkID string              `json:"product_link_id"`
		Type          string              `json:"type,omitempty"`
		Status        string              `json:"status,omitempty"`
		YoutubeVideo  *youtubeVideoResult `json:"youtube_video,omitempty"`
	}
	dataLinksResult struct {
		DataLinks  []dataLinkResult `json:"data_links"`
		NextCursor string           `json:"next_cursor,omitempty"`
		HasMore    bool             `json:"has_more"`
	}
)

func dataLinkName(customerID, productLinkID, id string) string {
	return customerName(customerID) + "/dataLinks/" + productLinkID + "~" + id
}

// dataLinkOf maps a row. A data link is named by its product link and its own ID, so both belong to the key.
func dataLinkOf(w *dataLinkWire, customerID string) (dataLinkResult, bool) {
	id, okID := idOf(w.DataLinkID)
	productID, okProduct := idOf(w.ProductLinkID)
	if !okID || !okProduct || (w.ResourceName != "" && w.ResourceName != dataLinkName(customerID, productID, id)) {
		return dataLinkResult{}, false
	}
	out := dataLinkResult{ResourceName: dataLinkName(customerID, productID, id), DataLinkID: id,
		ProductLinkID: productID, Type: code(w.Type), Status: code(w.Status)}
	if v := w.YoutubeVideo; v != nil {
		out.YoutubeVideo = &youtubeVideoResult{ChannelID: boundedText(v.ChannelID, maxText),
			VideoID: boundedText(v.VideoID, maxText)}
	}
	return out, true
}

// dataLinkKey is the keyset of a row: the product link ID and the data link ID, in the order of the query.
func dataLinkKey(item dataLinkResult) string { return item.ProductLinkID + "~" + item.DataLinkID }

// dataLinkAfter reports whether the key a lies after the key b.
func dataLinkAfter(a, b string) bool {
	ap, ad, _ := strings.Cut(a, "~")
	bp, bd, _ := strings.Cut(b, "~")
	if ap != bp {
		return numberAfter(ap, bp)
	}
	return numberAfter(ad, bd)
}

var dataLinksList = capability.Descriptor{
	ID: dataLinksListID, Version: 1, Title: "List the data links of a Google Ads account",
	Description: "List the links of a Google Ads account to data of another owner, today YouTube videos (a GAQL " +
		"search FROM data_link of API version v26 with a fixed set of fields): the ID of the link and of its product " +
		"link, type (VIDEO), status (DISABLED, ENABLED, PENDING_APPROVAL, REJECTED, REQUESTED, REVOKED) and the " +
		"YouTube channel and video ID. Rows come sorted by product link ID and data link ID; filters narrow them and " +
		"page_size with cursor pages through them (a page after the first costs a second operation when the first " +
		"read ends inside a product link). customer_id must be one of the targets or lie below a manager target; on a " +
		"connection with targets, a manager account needs a manager target that names it. Costs one operation of " +
		"the daily quota per page, plus one for a membership read below a manager target and one for the check " +
		"that the account is no manager account. Results are untrusted third-party content; text is cut.",
	Tags: []string{"google", "ads", "links", "data-links"}, Risk: linksReadRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id":      customerIDArgument,
		"statuses":         enumArray(dataLinkStatuses),
		"types":            enumArray(dataLinkTypes),
		"data_link_ids":    idArray,
		"product_link_ids": idArray,
		"channel_id":       textArgument(channelIDPattern, 64),
		"video_id":         textArgument(videoIDPattern, 11),
		"page_size":        pageProperties["page_size"], "cursor": pageProperties["cursor"],
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"data_links": arrayOf(object(schema{"resource_name": stringSchema,
		"data_link_id": stringSchema, "product_link_id": stringSchema, "type": stringSchema, "status": stringSchema,
		"youtube_video": object(schema{"channel_id": stringSchema, "video_id": stringSchema})},
		"resource_name", "data_link_id", "product_link_id")), "next_cursor": stringSchema, "has_more": booleanSchema},
		"data_links", "has_more")),
	Arguments: append([]capability.Argument{customerIDListArgument,
		{Name: "statuses", Description: "Only links with one of these statuses: DISABLED, ENABLED, PENDING_APPROVAL, " +
			"REJECTED, REQUESTED, REVOKED"},
		{Name: "types", Description: "Only links of this type: VIDEO"},
		{Name: "data_link_ids", Description: "Only these data link IDs, at most 100"},
		{Name: "product_link_ids", Description: "Only links of these product links, at most 100"},
		{Name: "channel_id", Description: "Only links to videos of this YouTube channel, as the channel ID"},
		{Name: "video_id", Description: "Only links to this YouTube video, as the 11-character video ID"},
	}, pageArguments...),
	Fields: []capability.Field{
		{Name: "data_links", Description: "Per link: resource_name (customers/ID/dataLinks/PRODUCT_LINK_ID~" +
			"DATA_LINK_ID), data_link_id, product_link_id, type, status, and youtube_video with channel_id and video_id"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the enabled data links of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","statuses":["ENABLED"]}`)}},
}

func handleDataLinksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list data links"
	var args dataLinksArguments
	env, err := startList(ctx, resolved, secrets, red, raw, op, dataLinksListID, &args,
		func() string { return args.CustomerID })
	if err != nil {
		return nil, err
	}
	conditions, err := args.conditions()
	if err != nil {
		return nil, err
	}
	size := defaultPageSize
	if args.PageSize != nil {
		if *args.PageSize < 1 || *args.PageSize > maxPageSize {
			return nil, badRequest("page_size must be 1 to 1000")
		}
		size = *args.PageSize
	}
	binding := provider.CursorBinding(env.tool, args.CustomerID, conditions, env.conn.login, env.conn.binding.key())
	after := ""
	if args.Cursor != "" {
		inner, ok := provider.DecodeCursor(binding, args.Cursor, maxCursorBytes)
		if !ok || !dataLinkKeyPattern.MatchString(inner) {
			return nil, badRequest("cursor is not a next_cursor of this tool with these arguments on this " +
				"connection; start again without cursor")
		}
		after = inner
	}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	// GAQL has no OR, so the keyset after (product link, data link) is read in two steps: the rest of that
	// product link, then the product links that follow.
	const both = "data_link.product_link_id, data_link.data_link_id"
	var rows []json.RawMessage
	read := func(extra []string, orderBy string, limit int) error {
		query := linkQuery("data_link", dataLinkFields, append(append([]string(nil), conditions...), extra...), orderBy, limit)
		page, err := c.search(env.ctx, env.op, env.tool, args.CustomerID, query)
		rows = append(rows, page...)
		return err
	}
	limit := size + 1
	if after == "" {
		err = read(nil, both, limit)
	} else {
		productID, dataID, _ := strings.Cut(after, "~")
		err = read([]string{"data_link.product_link_id = " + productID, "data_link.data_link_id > " + dataID},
			"data_link.data_link_id", limit)
		if err == nil && len(rows) < limit {
			err = read([]string{"data_link.product_link_id > " + productID}, both, limit-len(rows))
		}
	}
	if err != nil {
		return nil, err
	}
	hasMore := false
	if len(rows) > size {
		rows, hasMore = rows[:size], true
	}
	items := []dataLinkResult{}
	last := after
	for _, raw := range rows {
		row, err := decodeRow[dataLinkRow](raw, env.op, "a data link")
		if err != nil {
			return nil, err
		}
		if row.DataLink == nil {
			return nil, foreignRow(env.op, "a data link")
		}
		item, ok := dataLinkOf(row.DataLink, args.CustomerID)
		if !ok {
			return nil, foreignRow(env.op, "a data link")
		}
		if key := dataLinkKey(item); last != "" && !dataLinkAfter(key, last) {
			return nil, provider.InvalidResponse(env.op, subject+" returned the rows out of the requested order")
		}
		last = dataLinkKey(item)
		items = append(items, item)
	}
	next := ""
	if hasMore {
		next = provider.EncodeCursor(binding, last)
	}
	return dataLinksResult{DataLinks: items, NextCursor: next, HasMore: hasMore}, nil
}
