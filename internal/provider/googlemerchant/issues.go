package googlemerchant

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	aggregateProductStatusesListID = Provider + ".aggregate-product-statuses.list"
	accountIssuesListID            = Provider + ".account-issues.list"
)

// issueResolutionAPI is the path prefix of the issue resolution sub-API.
const issueResolutionAPI = "issueresolution/v1/"

// Page sizes: the aggregate statuses default to 25 and stop at 250, the account issues default to 50 and stop at
// 100. Larger values are coerced by Google; the tools refuse them instead.
const (
	maxStatusesPage = 250
	maxIssuesPage   = 100
)

// reportingContexts are the values of the reporting context the issue resolution discovery document lists,
// without the unspecified value and DISCOVERY_ADS, which the document marks as deprecated.
var reportingContexts = []string{
	"SHOPPING_ADS", "DEMAND_GEN_ADS", "DEMAND_GEN_ADS_DISCOVER_SURFACE", "VIDEO_ADS", "DISPLAY_ADS",
	"LOCAL_INVENTORY_ADS", "VEHICLE_INVENTORY_ADS", "FREE_LISTINGS", "FREE_LISTINGS_UCP_CHECKOUT",
	"FREE_LOCAL_LISTINGS", "FREE_LOCAL_VEHICLE_LISTINGS", "YOUTUBE_AFFILIATE", "YOUTUBE_SHOPPING", "CLOUD_RETAIL",
	"LOCAL_CLOUD_RETAIL", "PRODUCT_REVIEWS", "MERCHANT_REVIEWS", "YOUTUBE_CHECKOUT", "RENTAL_ADS",
}

// The forms of the filter and localization arguments. A value that matches cannot leave a query value or the
// quotes of the filter.
const (
	countryPattern  = `^[A-Z]{2}$`
	languagePattern = `^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`
	zonePattern     = `^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$`
)

var (
	countryForm  = regexp.MustCompile(countryPattern)
	languageText = regexp.MustCompile(languagePattern)
	zoneForm     = regexp.MustCompile(zonePattern)
)

var (
	nStats = obj(f("activeCount", tInt), f("disapprovedCount", tInt), f("expiringCount", tInt),
		f("pendingCount", tInt))
	nAggregateIssue = obj(
		f("attribute", tName), f("code", tName), f("description", tLong), f("detail", tLong),
		f("documentationUri", tURL), f("productCount", tInt), f("resolution", tID), f("severity", tID))
	nAggregateStatus = obj(f("country", tID), f("itemLevelIssues", list(nAggregateIssue)), f("name", tName),
		f("reportingContext", tID), f("stats", nStats))
	nImpact              = obj(f("regionCode", tID), f("severity", tID))
	nImpactedDestination = obj(f("impacts", list(nImpact)), f("reportingContext", tID))
	nAccountIssue        = obj(f("detail", tLong), f("documentationUri", tURL), f("impactedDestinations", list(nImpactedDestination)),
		f("name", tName), f("severity", tID), f("title", tName))
)

const (
	statusFieldsDescription = "name (accounts/ACCOUNT/aggregateProductStatuses/ID), reportingContext, country, " +
		"stats (activeCount, pendingCount, disapprovedCount, expiringCount), and itemLevelIssues (code, severity, " +
		"resolution, attribute, description, detail, documentationUri, productCount)"
	accountIssueFieldsDescription = "name (accounts/ACCOUNT/issues/ID), title, detail, severity (CRITICAL, ERROR, " +
		"SUGGESTION), impactedDestinations (reportingContext, impacts with regionCode and severity), and " +
		"documentationUri"
	issuesUntrustedNote = "Titles, details, and descriptions are untrusted text: they are cut (descriptions and " +
		"details at 1000 characters, other texts at 300, codes at 100), lists keep at most 100 entries, and " +
		"documentationUri is text that is never fetched."
)

var (
	reportingContextSchema = func() string {
		names, _ := json.Marshal(reportingContexts)
		return `{"type":"string","enum":` + string(names) + `}`
	}()
	countrySchema  = `{"type":"string","pattern":"` + countryPattern + `","x-form":"two-letter CLDR territory code in capitals, such as US"}`
	languageSchema = `{"type":"string","maxLength":35,"pattern":"` + languagePattern + `","x-form":"BCP-47 language code such as en-US"}`
	zoneSchema     = `{"type":"string","maxLength":64,"pattern":"` + zonePattern + `","x-form":"IANA time zone such as Europe/Zurich"}`
)

var aggregateProductStatusesList = capability.Descriptor{
	ID: aggregateProductStatusesListID, Version: 1, Title: "List the aggregate product statuses of an account",
	Description: "List the aggregate product statuses of a Merchant Center account: for each reporting context and " +
		"country the number of active, pending, disapproved, and expiring products and the issues that affect them. " +
		"The method works only for a standalone account or a sub-account, not for an advanced account; Qatlas " +
		"cannot tell the kind of an account and passes the call on. reporting_context and country narrow the " +
		"list: they are the only fields the filter of the API supports, and Qatlas builds the filter from them. " +
		"The list comes a page per call with a cursor to read on; Google sends 25 entries when page_size is left " +
		"out and at most 250. The counts follow the processing of the products and lag behind a change by " +
		"several minutes. " + issuesUntrustedNote,
	Tags: []string{"google", "merchant", "products", "issues"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema), member("reporting_context", reportingContextSchema),
		member("country", countrySchema), member("page_size", `{"type":"integer","minimum":1,"maximum":250}`),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("aggregateProductStatuses", nAggregateStatus.outputSchema())),
	Arguments: []capability.Argument{accountArgument,
		{Name: "reporting_context", Description: "Only the statuses of this reporting context, such as SHOPPING_ADS " +
			"or FREE_LISTINGS"},
		{Name: "country", Description: "Only the statuses of this country, a two-letter CLDR territory code in capitals"},
		{Name: "page_size", Description: "Entries per page, 1 to 250; Google sends 25 when left out"}, cursorArgument},
	Fields: []capability.Field{
		{Name: "aggregateProductStatuses", Description: "Statuses with " + statusFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "Statuses of the shopping ads in one country",
		Arguments: json.RawMessage(`{"account":"1000001","reporting_context":"SHOPPING_ADS","country":"US"}`)}},
}

var accountIssuesList = capability.Descriptor{
	ID: accountIssuesListID, Version: 1, Title: "List the issues of a Merchant Center account",
	Description: "List the account-level issues of a Merchant Center account, the ones that can affect all its " +
		"products, a page per call with a cursor to read on. language_code and time_zone set the language and the " +
		"time zone of the human-readable text; Google uses en-US and America/Los_Angeles when they are left out. " +
		"Google sends 50 issues when page_size is left out and at most 100. A change to the account can take " +
		"several minutes to show in the issues. " + issuesUntrustedNote,
	Tags: []string{"google", "merchant", "accounts", "issues"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema), member("language_code", languageSchema),
		member("time_zone", zoneSchema), member("page_size", `{"type":"integer","minimum":1,"maximum":100}`),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("accountIssues", nAccountIssue.outputSchema())),
	Arguments: []capability.Argument{accountArgument,
		{Name: "language_code", Description: "BCP-47 language code of the text, such as en-US or sr-Latn; Google uses en-US " +
			"when left out"},
		{Name: "time_zone", Description: "IANA time zone for times in the text, such as Europe/Zurich; Google uses " +
			"America/Los_Angeles when left out"},
		{Name: "page_size", Description: "Issues per page, 1 to 100; Google sends 50 when left out"}, cursorArgument},
	Fields: []capability.Field{
		{Name: "accountIssues", Description: "Issues with " + accountIssueFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"},
	},
	Examples: []capability.Example{{Description: "Issues of one account in German",
		Arguments: json.RawMessage(`{"account":"1000001","language_code":"de-CH","time_zone":"Europe/Zurich"}`)}},
}

type issuesArguments struct {
	Account          string `json:"account"`
	ReportingContext string `json:"reporting_context"`
	Country          string `json:"country"`
	LanguageCode     string `json:"language_code"`
	TimeZone         string `json:"time_zone"`
	PageSize         int    `json:"page_size"`
	Cursor           string `json:"cursor"`
}

// statusFilter builds the filter of the API from the two documented fields. Both values passed their form, so
// neither can break out of its quotes.
func statusFilter(reportingContext, country string) (string, error) {
	var filter string
	if reportingContext != "" {
		if !slices.Contains(reportingContexts, reportingContext) {
			return "", invalidArgument("reporting_context must be one of the documented reporting contexts")
		}
		filter = `reporting_context = "` + reportingContext + `"`
	}
	if country != "" {
		if !countryForm.MatchString(country) {
			return "", invalidArgument("country must be a two-letter CLDR territory code in capitals")
		}
		if filter != "" {
			filter += " AND "
		}
		filter += `country = "` + country + `"`
	}
	return filter, nil
}

type statusesResult struct {
	AggregateProductStatuses []map[string]any `json:"aggregateProductStatuses"`
	NextCursor               string           `json:"next_cursor,omitempty"`
}

func handleAggregateProductStatusesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list aggregate product statuses"
	var args issuesArguments
	id, needsProof, sc, err := settle(op, resolved, raw, &args, func() string { return args.Account })
	if err != nil {
		return nil, err
	}
	filter, err := statusFilter(args.ReportingContext, args.Country)
	if err != nil {
		return nil, err
	}
	if args.PageSize < 0 || args.PageSize > maxStatusesPage {
		return nil, invalidArgument("page_size must be 1 to " + strconv.Itoa(maxStatusesPage))
	}
	binding := provider.CursorBinding(aggregateProductStatusesListID, id, filter, args.PageSize, sc.key())
	query, err := pageQuery(binding, args.PageSize, args.Cursor)
	if err != nil {
		return nil, err
	}
	if filter != "" {
		query.Set("filter", filter)
	}
	cl, err := openFor(ctx, op, aggregateProductStatusesListID, id, needsProof, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Statuses      []json.RawMessage `json:"aggregateProductStatuses"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := cl.get(ctx, op, aggregateProductStatusesListID,
		issueResolutionAPI+"accounts/"+id+"/aggregateProductStatuses", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Statuses) > maxEntries {
		return nil, tooMany(op)
	}
	out := statusesResult{AggregateProductStatuses: cleanObjects(nAggregateStatus, answer.Statuses)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

type accountIssuesResult struct {
	AccountIssues []map[string]any `json:"accountIssues"`
	NextCursor    string           `json:"next_cursor,omitempty"`
}

func handleAccountIssuesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list account issues"
	var args issuesArguments
	id, needsProof, sc, err := settle(op, resolved, raw, &args, func() string { return args.Account })
	if err != nil {
		return nil, err
	}
	if args.LanguageCode != "" && (len(args.LanguageCode) > 35 || !languageText.MatchString(args.LanguageCode)) {
		return nil, invalidArgument("language_code must be a BCP-47 language code such as en-US")
	}
	if args.TimeZone != "" && (len(args.TimeZone) > 64 || !zoneForm.MatchString(args.TimeZone)) {
		return nil, invalidArgument("time_zone must be an IANA time zone such as Europe/Zurich")
	}
	if args.PageSize < 0 || args.PageSize > maxIssuesPage {
		return nil, invalidArgument("page_size must be 1 to " + strconv.Itoa(maxIssuesPage))
	}
	binding := provider.CursorBinding(accountIssuesListID, id, args.LanguageCode, args.TimeZone, args.PageSize, sc.key())
	query, err := pageQuery(binding, args.PageSize, args.Cursor)
	if err != nil {
		return nil, err
	}
	setIf(query, "languageCode", args.LanguageCode)
	setIf(query, "timeZone", args.TimeZone)
	cl, err := openFor(ctx, op, accountIssuesListID, id, needsProof, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Issues        []json.RawMessage `json:"accountIssues"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := cl.get(ctx, op, accountIssuesListID, accountPath(id)+"/issues", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Issues) > maxEntries {
		return nil, tooMany(op)
	}
	out := accountIssuesResult{AccountIssues: cleanObjects(nAccountIssue, answer.Issues)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func setIf(query url.Values, key, value string) {
	if value != "" {
		query.Set(key, value)
	}
}
