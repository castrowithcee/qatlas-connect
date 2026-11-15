package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The billing tools read the usage and cost reports and the budgets of a user or an organization, and
// nothing else: no tool creates, changes, or deletes a budget, and no payment method is touched. Every tool
// names an owner target, checked against the targets and exclusions before a secret is read. The routes
// below /organizations/OWNER and /users/OWNER are fixed. Amounts and quantities are passed on as the number
// literal GitHub sent, never rounded. Product, SKU, model, repository, and cost names are untrusted data.
// GitHub answers a refused or unknown billing route with 403 or 404 for a missing right, an account without
// the enhanced billing platform, or an unknown budget alike, so both end as the state unavailable.

const billingSensitivity = "github-billing"

var billingRisk = capability.Risk{
	Effect:          capability.EffectRead,
	Idempotency:     capability.IdempotencySafe,
	Confirmation:    capability.ConfirmationNone,
	OpenWorld:       true,
	DataSensitivity: billingSensitivity,
}

// maxBillingItems bounds the line items one report answers; GitHub pages none of the report routes.
const maxBillingItems = 500

const (
	billingUnavailableState  = "unavailable"
	billingUnavailableReason = "GitHub did not give this billing data: the account may not use the enhanced " +
		"billing platform or this plan may not offer it, the token may lack the billing right the route needs, " +
		"the owner or budget may not exist, or GitHub may not show it to this token"
	billingReadPermission = "GitHub refused this token the billing data of this owner"
	billingTextLimit      = 100
)

// BillingUnavailable is the answer for a billing read GitHub refused with 403 or 404.
type BillingUnavailable struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// ---- Arguments

type billingArguments struct {
	Year       int    `json:"year"`
	Month      int    `json:"month"`
	Day        int    `json:"day"`
	Product    string `json:"product"`
	SKU        string `json:"sku"`
	Model      string `json:"model"`
	User       string `json:"user"`
	Repository string `json:"repository"`
	Scope      string `json:"scope"`
	BudgetID   string `json:"budget_id"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`

	query         url.Values
	page, perPage int
	binding       []byte
}

var billingBudgetScopeValues = []string{"enterprise", "organization", "repository", "cost_center",
	"multi_user_customer", "user"}

const (
	billingYearSchema   = `{"type":"integer","minimum":1000,"maximum":9999}`
	billingMonthSchema  = `{"type":"integer","minimum":1,"maximum":12}`
	billingDaySchema    = `{"type":"integer","minimum":1,"maximum":31}`
	billingTextSchema   = `{"type":"string","minLength":1,"maxLength":100}`
	billingLoginSchema  = `{"type":"string","minLength":1,"maxLength":100}`
	billingRepoSchema   = `{"type":"string","minLength":3,"maxLength":201}`
	billingBudgetSchema = `{"type":"string","minLength":1,"maxLength":100,"pattern":"^[A-Za-z0-9][A-Za-z0-9_-]*$"}`
	billingScopeSchema  = `{"type":"string","enum":["enterprise","organization","repository","cost_center",` +
		`"multi_user_customer","user"]}`
)

const billingDateKeys = `"year":` + billingYearSchema + `,"month":` + billingMonthSchema + `,"day":` + billingDaySchema

var billingDateArguments = []capability.Argument{
	{Name: "year", Description: "Only the usage of this year, four digits; the current year when omitted"},
	{Name: "month", Description: "Only the usage of this month, 1 through 12; GitHub's default when omitted"},
	{Name: "day", Description: "Only the usage of this day, 1 through 31; GitHub's default when omitted"},
}

func billingTextArgument(name, what string) capability.Argument {
	return capability.Argument{Name: name, Description: "Only the usage of this " + what + ", at most 100 characters; " +
		"GitHub compares it without case"}
}

// checkBillingText refuses a filter text that is too long or holds a control character.
func checkBillingText(name, value string) error {
	if value == "" {
		return nil
	}
	if utf8.RuneCountInString(value) > billingTextLimit || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return invalidRequest(name + " must hold 1 to 100 characters without control characters")
	}
	return nil
}

// dateQuery validates year, month, and day and adds the given ones to the query; GitHub decides which
// combinations and periods it serves.
func (a *billingArguments) dateQuery() error {
	if a.Year != 0 && (a.Year < 1000 || a.Year > 9999) {
		return invalidRequest("year must be four digits")
	}
	if a.Month != 0 && (a.Month < 1 || a.Month > 12) {
		return invalidRequest("month must be between 1 and 12")
	}
	if a.Day != 0 && (a.Day < 1 || a.Day > 31) {
		return invalidRequest("day must be between 1 and 31")
	}
	a.query = url.Values{}
	for _, entry := range []struct {
		name  string
		value int
	}{{"year", a.Year}, {"month", a.Month}, {"day", a.Day}} {
		if entry.value != 0 {
			a.query.Set(entry.name, strconv.Itoa(entry.value))
		}
	}
	return nil
}

func (a *billingArguments) textQuery(filters ...[2]string) error {
	for _, filter := range filters {
		if err := checkBillingText(filter[0], filter[1]); err != nil {
			return err
		}
		if filter[1] != "" {
			a.query.Set(filter[0], filter[1])
		}
	}
	return nil
}

// reportUser refuses the user filter of an owner that is not an organization: GitHub documents it for the
// organization routes only, and a user report is one user already.
func (a *billingArguments) reportUser(bound target) error {
	if a.User == "" {
		return nil
	}
	if bound.scope != "orgs" {
		return invalidRequest("user filters the report of an organization; the report of a user is one user already")
	}
	if !validLogin(a.User) {
		return invalidRequest("user must be a login of letters, digits, hyphens, and underscores")
	}
	a.query.Set("user", a.User)
	return nil
}

func checkBillingUsage(a *billingArguments, _ target, _ allowlist) error { return a.dateQuery() }

func checkBillingSummary(a *billingArguments, bound target, allowed allowlist) error {
	if err := a.dateQuery(); err != nil {
		return err
	}
	if a.Repository != "" {
		repository, err := parseArgument(kindRepository, a.Repository)
		if err != nil {
			return invalidRequest("repository must be OWNER/REPO")
		}
		if !strings.EqualFold(repository.owner, bound.owner) {
			return invalidRequest("repository must belong to the owner of the call")
		}
		if allowed.excluded(repository) {
			return invalidRequest("repository is not available on this connection; pass another one")
		}
		a.query.Set("repository", repository.owner+"/"+repository.repo)
	}
	return a.textQuery([2]string{"product", a.Product}, [2]string{"sku", a.SKU})
}

func checkBillingModelReport(a *billingArguments, bound target, _ allowlist) error {
	if err := a.dateQuery(); err != nil {
		return err
	}
	if err := a.reportUser(bound); err != nil {
		return err
	}
	return a.textQuery([2]string{"model", a.Model}, [2]string{"product", a.Product})
}

func checkBillingBudgetsList(a *billingArguments, bound target, _ allowlist) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	filters := []any{}
	if a.Scope != "" {
		if !containsFold(billingBudgetScopeValues, a.Scope) {
			return invalidRequest("scope must be " + strings.Join(billingBudgetScopeValues, ", "))
		}
		a.Scope = strings.ToLower(a.Scope)
		filters = append(filters, "scope", a.Scope)
	}
	if a.User != "" {
		if !validLogin(a.User) {
			return invalidRequest("user must be a login of letters, digits, hyphens, and underscores")
		}
		filters = append(filters, "user", strings.ToLower(a.User))
	}
	a.binding = fingerprint(append([]any{"billingbudgets", strings.ToLower(bound.String())}, filters...)...)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkBillingBudgetsGet(a *billingArguments, _ target, _ allowlist) error {
	if a.BudgetID == "" || len(a.BudgetID) > 100 || !validBudgetID(a.BudgetID) {
		return invalidRequest("budget_id must be the identifier of a budget, letters, digits, hyphens, and underscores")
	}
	return nil
}

func validBudgetID(value string) bool {
	for i, r := range value {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alnum && (i == 0 || (r != '-' && r != '_')) {
			return false
		}
	}
	return true
}

// ---- Handler

// billingHandler decodes and checks the arguments and the owner before a credential is resolved, so a refused
// request never becomes a provider call. Budgets belong to an organization, so a user owner is refused for them.
func billingHandler(id string, organizationOnly bool, check func(*billingArguments, target, allowlist) error,
	call func(context.Context, *Client, *billingArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments billingArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOwner(resolved, kindOwner, raw)
		if err != nil {
			return nil, err
		}
		if organizationOnly && bound.scope != "orgs" {
			return nil, invalidRequest("owner must be an organization, as orgs/LOGIN; budgets belong to an " +
				"organization, not a user")
		}
		allowed, err := allowlistOf(resolved)
		if err != nil {
			return nil, providerError("open", err.Error())
		}
		if err := check(&arguments, bound, allowed); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

// ---- Descriptors

var billingOwnerArgument = capability.Argument{Name: "owner", Description: "Owner as users/LOGIN for a user or " +
	"orgs/LOGIN for an organization; optional when the connection's targets name exactly one owner; must lie " +
	"inside the targets when the connection lists any, as an owner target itself, since a project or a " +
	"repository pattern of the owner is not enough; the budget tools take an organization only"}

var billingOwnerField = capability.Field{Name: "owner", Description: "Owner the tool read, as users/LOGIN or " +
	"orgs/LOGIN; the one a default chose when the argument was left out"}

const billingUnavailableProperties = `"state":{"type":"string"},"reason":{"type":"string"}`

var billingUnavailableFields = []capability.Field{
	{Name: "state", Description: "unavailable when GitHub refused the read with 403 or 404; then no report is " +
		"given; absent otherwise"},
	{Name: "reason", Description: "Fixed reason that accompanies state unavailable; it names the possible causes " +
		"and never quotes GitHub"},
}

const billingUntrusted = "; untrusted data"

func billingTool(id, title, description string, tags []string, input json.RawMessage, output string,
	arguments []capability.Argument, fields []capability.Field, example capability.Example) capability.Descriptor {
	return capability.Descriptor{ID: Provider + "." + id, Version: 1, Title: title, Description: description,
		Tags: append([]string{"github", "billing", "organization-admin"}, tags...), Risk: billingRisk, Provider: Provider,
		InputSchema: input, OutputSchema: json.RawMessage(output), Arguments: arguments,
		Fields:   append(append([]capability.Field(nil), fields...), billingUnavailableFields...),
		Examples: []capability.Example{example}}
}

// billingReportOutput is the schema of a report: the given members, the line items, truncated, and the
// unavailable state.
func billingReportOutput(properties, itemProperties string) string {
	return `{"type":"object","properties":{` + properties + `,"usage_items":{"type":"array","items":{"type":"object",` +
		`"properties":{` + itemProperties + `},"additionalProperties":false}},"truncated":{"type":"boolean"},` +
		billingUnavailableProperties + `},"additionalProperties":false}`
}

const (
	billingNumber = `{"type":"number"}`
	billingString = `{"type":"string"}`

	billingTimePeriodSchema = `{"type":"object","properties":{"year":{"type":"integer"},"month":{"type":"integer"},` +
		`"day":{"type":"integer"}},"additionalProperties":false}`
)

const billingUsageItemProperties = `"date":` + billingString + `,"product":` + billingString + `,"sku":` +
	billingString + `,"quantity":` + billingNumber + `,"unit_type":` + billingString + `,"price_per_unit":` +
	billingNumber + `,"gross_amount":` + billingNumber + `,"discount_amount":` + billingNumber + `,"net_amount":` +
	billingNumber + `,"organization_name":` + billingString + `,"repository_name":` + billingString

const billingQuantityItemProperties = `"product":` + billingString + `,"sku":` + billingString + `,"unit_type":` +
	billingString + `,"price_per_unit":` + billingNumber + `,"gross_quantity":` + billingNumber + `,"gross_amount":` +
	billingNumber + `,"discount_quantity":` + billingNumber + `,"discount_amount":` + billingNumber +
	`,"net_quantity":` + billingNumber + `,"net_amount":` + billingNumber

const billingModelItemProperties = `"product":` + billingString + `,"sku":` + billingString + `,"model":` +
	billingString + `,"unit_type":` + billingString + `,"price_per_unit":` + billingNumber + `,"gross_quantity":` +
	billingNumber + `,"gross_amount":` + billingNumber + `,"discount_quantity":` + billingNumber +
	`,"discount_amount":` + billingNumber + `,"net_quantity":` + billingNumber + `,"net_amount":` + billingNumber

var billingTruncatedField = capability.Field{Name: "truncated", Description: "True when the report held more than " +
	"500 line items and only the first 500 are given; absent for state unavailable"}

var billingTimePeriodField = capability.Field{Name: "time_period", Description: "year, month, and day the " +
	"report covers, as GitHub names them; month and day only when GitHub gives them"}

// billingFieldsOf appends the members of a list's entries to its top-level fields; a name a top-level field
// already holds, such as the product a summary was narrowed to, is described once.
func billingFieldsOf(top []capability.Field, entries [][2]string) []capability.Field {
	fields := append([]capability.Field(nil), top...)
	named := map[string]bool{}
	for _, field := range top {
		named[field.Name] = true
	}
	for _, entry := range entries {
		if !named[entry[0]] {
			fields = append(fields, capability.Field{Name: entry[0], Description: entry[1]})
		}
	}
	return fields
}

var billingUsageFields = billingFieldsOf([]capability.Field{
	{Name: "usage_items", Description: "Line items of the report in GitHub's order; every amount and quantity " +
		"is the number GitHub sent, product, SKU, unit, and repository names are untrusted data; items of a " +
		"repository the connection excludes are left out and counted in withheld"},
	{Name: "withheld", Description: "Count of line items left out because their repository is excluded on this " +
		"connection, absent when none"},
	billingTruncatedField,
}, [][2]string{
	{"date", "Date of the line item" + billingUntrusted},
	{"product", "Product name" + billingUntrusted},
	{"sku", "SKU name" + billingUntrusted},
	{"quantity", "Quantity of the line item"},
	{"unit_type", "Unit type of the line item" + billingUntrusted},
	{"price_per_unit", "Price per unit"},
	{"gross_amount", "Gross amount"},
	{"discount_amount", "Discount amount"},
	{"net_amount", "Net amount"},
	{"organization_name", "Organization of the line item, only in the report of an organization" + billingUntrusted},
	{"repository_name", "Repository of the line item, when GitHub gives one" + billingUntrusted},
})

var billingUsageGet = billingTool("billing-usage.get", "Get the GitHub billing usage report of an owner",
	"Read the usage report of one user or organization an explicit connection allows, as the line items GitHub "+
		"reports: date, product, SKU, quantity, unit, price, and gross, discount, and net amount, for the "+
		"current year or the year, month, or day asked for; GitHub serves it only to accounts on the enhanced "+
		"billing platform, otherwise the state is unavailable; at most 500 line items are given",
	[]string{"usage", "get"}, inputSchema(billingDateKeys),
	`{"type":"object","properties":{"usage_items":{"type":"array","items":{"type":"object","properties":{`+
		billingUsageItemProperties+`},"additionalProperties":false}},"withheld":{"type":"integer"},`+
		`"truncated":{"type":"boolean"},`+billingUnavailableProperties+`},"additionalProperties":false}`,
	billingDateArguments, billingUsageFields,
	capability.Example{Description: "Read the usage of March 2026", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","year":2026,"month":3}`)})

var billingSummaryFields = billingFieldsOf([]capability.Field{
	billingTimePeriodField,
	{Name: "organization", Description: "Organization the report belongs to, only in the report of an organization" + billingUntrusted},
	{Name: "user", Description: "User the report belongs to, only in the report of a user" + billingUntrusted},
	{Name: "repository", Description: "Repository the report was narrowed to, when given" + billingUntrusted},
	{Name: "product", Description: "Product the report was narrowed to, when given" + billingUntrusted},
	{Name: "sku", Description: "SKU the report was narrowed to, when given" + billingUntrusted},
	{Name: "usage_items", Description: "Summed line items per product and SKU; every amount and quantity is the " +
		"number GitHub sent, names are untrusted data. GitHub sums the whole owner: an excluded repository's " +
		"usage is part of the sums and cannot be told apart"},
	billingTruncatedField,
}, [][2]string{
	{"product", "Product name" + billingUntrusted},
	{"sku", "SKU name" + billingUntrusted},
	{"unit_type", "Unit type" + billingUntrusted},
	{"price_per_unit", "Price per unit"},
	{"gross_quantity", "Gross quantity"},
	{"gross_amount", "Gross amount"},
	{"discount_quantity", "Discount quantity"},
	{"discount_amount", "Discount amount"},
	{"net_quantity", "Net quantity"},
	{"net_amount", "Net amount"},
})

var billingSummaryGet = billingTool("billing-usage-summary.get", "Get the GitHub billing usage summary of an owner",
	"Read the summarized usage of one user or organization an explicit connection allows per product and SKU, "+
		"with gross, discount, and net quantity and amount, for the current month or the year, month, or day "+
		"asked for, optionally narrowed to a repository of the owner, a product, or a SKU; GitHub documents "+
		"this report as a public preview and keeps data of the past 24 months; GitHub sums the whole owner, "+
		"including repositories this connection excludes",
	[]string{"usage", "summary", "get"},
	inputSchema(billingDateKeys+`,"repository":`+billingRepoSchema+`,"product":`+billingTextSchema+
		`,"sku":`+billingTextSchema),
	billingReportOutput(`"time_period":`+billingTimePeriodSchema+`,"organization":`+billingString+`,"user":`+
		billingString+`,"repository":`+billingString+`,"product":`+billingString+`,"sku":`+billingString,
		billingQuantityItemProperties),
	append(append([]capability.Argument(nil), billingDateArguments...),
		capability.Argument{Name: "repository", Description: "Only the usage of this repository as OWNER/REPO; " +
			"the owner must be the owner of the call and the repository must not be excluded"},
		billingTextArgument("product", "product"), billingTextArgument("sku", "SKU")),
	billingSummaryFields,
	capability.Example{Description: "Read the usage summary of one repository", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","repository":"octo-org/example","month":3}`)})

func billingModelFields(what string) []capability.Field {
	return billingFieldsOf([]capability.Field{
		billingTimePeriodField,
		{Name: "organization", Description: "Organization the report belongs to, only in the report of an organization" + billingUntrusted},
		{Name: "user", Description: "User the report belongs to or was narrowed to" + billingUntrusted},
		{Name: "product", Description: "Product the report was narrowed to, when given" + billingUntrusted},
		{Name: "model", Description: "Model the report was narrowed to, when given" + billingUntrusted},
		{Name: "usage_items", Description: "Summed " + what + " line items per product, SKU, and model; every " +
			"amount and quantity is the number GitHub sent, names are untrusted data"},
		billingTruncatedField,
	}, [][2]string{
		{"product", "Product name" + billingUntrusted},
		{"sku", "SKU name" + billingUntrusted},
		{"model", "Model name" + billingUntrusted},
		{"unit_type", "Unit type" + billingUntrusted},
		{"price_per_unit", "Price per unit"},
		{"gross_quantity", "Gross quantity"},
		{"gross_amount", "Gross amount"},
		{"discount_quantity", "Discount quantity"},
		{"discount_amount", "Discount amount"},
		{"net_quantity", "Net quantity"},
		{"net_amount", "Net amount"},
	})
}

func billingModelTool(id, title, what string) capability.Descriptor {
	return billingTool(id, title,
		"Read the "+what+" usage of one user or organization an explicit connection allows per product, SKU, and "+
			"model, with gross, discount, and net quantity and amount, for the current month or the year, month, "+
			"or day asked for, optionally narrowed to a model or product, and for an organization to a user; "+
			"GitHub keeps data of the past 24 months",
		[]string{what, "usage", "get"},
		inputSchema(billingDateKeys+`,"user":`+billingLoginSchema+`,"model":`+billingTextSchema+`,"product":`+
			billingTextSchema),
		billingReportOutput(`"time_period":`+billingTimePeriodSchema+`,"organization":`+billingString+`,"user":`+
			billingString+`,"product":`+billingString+`,"model":`+billingString, billingModelItemProperties),
		append(append([]capability.Argument(nil), billingDateArguments...),
			capability.Argument{Name: "user", Description: "Only the usage of this user login; for an organization " +
				"owner only, since the report of a user owner is one user already"},
			billingTextArgument("model", "model"), billingTextArgument("product", "product")),
		billingModelFields(what),
		capability.Example{Description: "Read the " + what + " usage of a month", Arguments: json.RawMessage(
			`{"owner":"orgs/octo-org","year":2026,"month":3}`)})
}

var billingPremiumRequestsGet = billingModelTool("billing-premium-requests.get",
	"Get the GitHub premium request usage of an owner", "premium request")

var billingAICreditsGet = billingModelTool("billing-ai-credits.get",
	"Get the GitHub AI credit usage of an owner", "AI credit")

const billingAlertingSchema = `{"type":"object","properties":{"will_alert":{"type":"boolean"},` +
	`"alert_recipients":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`

const billingBudgetProperties = `"id":` + billingString + `,"budget_type":` + billingString + `,"budget_amount":` +
	billingNumber + `,"prevent_further_usage":{"type":"boolean"},"budget_scope":` + billingString +
	`,"budget_entity_name":` + billingString + `,"user":` + billingString + `,"consumed_amount":` + billingNumber +
	`,"budget_product_sku":` + billingString + `,"budget_alerting":` + billingAlertingSchema +
	`,"alert_recipients_truncated":{"type":"boolean"}`

const billingEffectiveBudgetSchema = `{"type":"object","properties":{"id":` + billingString + `,"budget_amount":` +
	billingNumber + `,"consumed_amount":` + billingNumber + `},"additionalProperties":false}`

var billingBudgetEntries = [][2]string{
	{"id", "Identifier of the budget, the budget_id github.billing-budgets.get takes"},
	{"budget_type", "Type of pricing, ProductPricing or SkuPricing as GitHub documents it" + billingUntrusted},
	{"budget_amount", "Budget limit in whole dollars; for a license-based product the number of licenses"},
	{"prevent_further_usage", "Whether GitHub stops further spending once the budget is exceeded"},
	{"budget_scope", "Scope of the budget: enterprise, organization, repository, cost_center, " +
		"multi_user_customer, multi_user_cost_center, or user" + billingUntrusted},
	{"budget_entity_name", "Name of the entity the budget applies to, for a repository as OWNER/REPO" + billingUntrusted},
	{"user", "User login of a budget with scope user" + billingUntrusted},
	{"consumed_amount", "Amount consumed; for a list only for a user budget or when the list is narrowed to a user"},
	{"budget_product_sku", "The single product or SKU the budget applies to" + billingUntrusted},
	{"budget_alerting", "will_alert and alert_recipients, the user logins GitHub alerts, at most 100" +
		billingUntrusted},
	{"alert_recipients_truncated", "True when GitHub named more than 100 alert recipients and only the first 100 " +
		"are given; absent otherwise"},
}

var billingBudgetFields = billingFieldsOf(nil, billingBudgetEntries)

var billingBudgetsList = billingTool("billing-budgets.list", "List the GitHub billing budgets of an organization",
	"List one bounded batch of the budgets of an organization an explicit connection allows, in the order "+
		"GitHub returns them, with scope, entity, product or SKU, limit, enforcement, and alerting, optionally "+
		"narrowed to a scope or to the consumed amount of a user; user owners have no budgets here; budgets "+
		"of an excluded repository are left out and counted in withheld",
	[]string{"budgets", "list"},
	inputSchema(`"scope":`+billingScopeSchema+`,"user":`+billingLoginSchema+`,`+pagingKeys),
	`{"type":"object","properties":{"budgets":{"type":"array","items":{"type":"object","properties":{`+
		billingBudgetProperties+`,"expires_at":`+billingString+`},"additionalProperties":false}},"user":`+
		billingString+`,"effective_budget":`+billingEffectiveBudgetSchema+`,"total_count":{"type":"integer"},`+
		`"withheld":{"type":"integer"},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"},`+
		billingUnavailableProperties+`},"additionalProperties":false}`,
	append([]capability.Argument{
		{Name: "scope", Description: "Only budgets of this scope: " + strings.Join(billingBudgetScopeValues, ", ")},
		{Name: "user", Description: "User login whose consumed amount the budgets show, with the effective budget " +
			"of that user"},
	}, pagingArguments...),
	append(billingFieldsOf([]capability.Field{
		{Name: "budgets", Description: "Budgets of the batch" + billingUntrusted},
		{Name: "user", Description: "User login the answer was narrowed to, when given" + billingUntrusted},
		{Name: "effective_budget", Description: "id, budget_amount, and consumed_amount of the effective budget of " +
			"the user the list was narrowed to; absent otherwise"},
		{Name: "total_count", Description: "Number of budgets GitHub reports for the query"},
		{Name: "withheld", Description: "Count of budgets of this batch left out because their repository is " +
			"excluded on this connection, absent when none"},
	}, append(append([][2]string(nil), billingBudgetEntries...),
		[2]string{"expires_at", "Date the budget expires, YYYY-MM-DD, when GitHub gives one"})),
		pagingFields...),
	capability.Example{Description: "List the repository budgets", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","scope":"repository"}`)})

var billingBudgetsGet = billingTool("billing-budgets.get", "Get a GitHub billing budget of an organization",
	"Read one budget of an organization an explicit connection allows by its budget_id; an unknown budget is "+
		"reported as unavailable like any other 404, and a budget of an excluded repository is not found",
	[]string{"budgets", "get"},
	inputSchema(`"budget_id":`+billingBudgetSchema, "budget_id"),
	`{"type":"object","properties":{`+billingBudgetProperties+`,`+billingUnavailableProperties+
		`},"additionalProperties":false}`,
	[]capability.Argument{{Name: "budget_id", Description: "Identifier of the budget, from github.billing-budgets.list",
		Required: true}},
	billingBudgetFields,
	capability.Example{Description: "Read one budget", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","budget_id":"2066deda-923f-43f9-88d2-62395a28c0cdd"}`)})

// billingToolIDs are the billing tools; each names an owner as its target.
var billingToolIDs = []string{billingUsageGet.ID, billingSummaryGet.ID, billingPremiumRequestsGet.ID,
	billingAICreditsGet.ID, billingBudgetsList.ID, billingBudgetsGet.ID}

// ---- Results

// BillingUsageItem is one line item of the usage report.
type BillingUsageItem struct {
	Date             *string      `json:"date,omitempty"`
	Product          *string      `json:"product,omitempty"`
	SKU              *string      `json:"sku,omitempty"`
	Quantity         *json.Number `json:"quantity,omitempty"`
	UnitType         *string      `json:"unit_type,omitempty"`
	PricePerUnit     *json.Number `json:"price_per_unit,omitempty"`
	GrossAmount      *json.Number `json:"gross_amount,omitempty"`
	DiscountAmount   *json.Number `json:"discount_amount,omitempty"`
	NetAmount        *json.Number `json:"net_amount,omitempty"`
	OrganizationName *string      `json:"organization_name,omitempty"`
	RepositoryName   *string      `json:"repository_name,omitempty"`
}

// BillingUsage is the usage report of an owner.
type BillingUsage struct {
	UsageItems []BillingUsageItem `json:"usage_items"`
	Withheld   int                `json:"withheld,omitempty"`
	Truncated  bool               `json:"truncated"`
}

// BillingTimePeriod is the period a report covers.
type BillingTimePeriod struct {
	Year  *int64 `json:"year,omitempty"`
	Month *int64 `json:"month,omitempty"`
	Day   *int64 `json:"day,omitempty"`
}

// BillingQuantityItem is one summed line item of the usage summary.
type BillingQuantityItem struct {
	Product          *string      `json:"product,omitempty"`
	SKU              *string      `json:"sku,omitempty"`
	UnitType         *string      `json:"unit_type,omitempty"`
	PricePerUnit     *json.Number `json:"price_per_unit,omitempty"`
	GrossQuantity    *json.Number `json:"gross_quantity,omitempty"`
	GrossAmount      *json.Number `json:"gross_amount,omitempty"`
	DiscountQuantity *json.Number `json:"discount_quantity,omitempty"`
	DiscountAmount   *json.Number `json:"discount_amount,omitempty"`
	NetQuantity      *json.Number `json:"net_quantity,omitempty"`
	NetAmount        *json.Number `json:"net_amount,omitempty"`
}

// BillingSummary is the usage summary of an owner.
type BillingSummary struct {
	TimePeriod   *BillingTimePeriod    `json:"time_period,omitempty"`
	Organization *string               `json:"organization,omitempty"`
	User         *string               `json:"user,omitempty"`
	Repository   *string               `json:"repository,omitempty"`
	Product      *string               `json:"product,omitempty"`
	SKU          *string               `json:"sku,omitempty"`
	UsageItems   []BillingQuantityItem `json:"usage_items"`
	Truncated    bool                  `json:"truncated"`
}

// BillingModelItem is one summed line item of the premium request or AI credit report.
type BillingModelItem struct {
	Product          *string      `json:"product,omitempty"`
	SKU              *string      `json:"sku,omitempty"`
	Model            *string      `json:"model,omitempty"`
	UnitType         *string      `json:"unit_type,omitempty"`
	PricePerUnit     *json.Number `json:"price_per_unit,omitempty"`
	GrossQuantity    *json.Number `json:"gross_quantity,omitempty"`
	GrossAmount      *json.Number `json:"gross_amount,omitempty"`
	DiscountQuantity *json.Number `json:"discount_quantity,omitempty"`
	DiscountAmount   *json.Number `json:"discount_amount,omitempty"`
	NetQuantity      *json.Number `json:"net_quantity,omitempty"`
	NetAmount        *json.Number `json:"net_amount,omitempty"`
}

// BillingModelReport is the premium request or AI credit report of an owner.
type BillingModelReport struct {
	TimePeriod   *BillingTimePeriod `json:"time_period,omitempty"`
	Organization *string            `json:"organization,omitempty"`
	User         *string            `json:"user,omitempty"`
	Product      *string            `json:"product,omitempty"`
	Model        *string            `json:"model,omitempty"`
	UsageItems   []BillingModelItem `json:"usage_items"`
	Truncated    bool               `json:"truncated"`
}

// BillingAlerting is the alerting of a budget.
type BillingAlerting struct {
	WillAlert       *bool    `json:"will_alert,omitempty"`
	AlertRecipients []string `json:"alert_recipients,omitempty"`
}

// BillingBudget is one budget of an organization.
type BillingBudget struct {
	ID                       string           `json:"id"`
	BudgetType               *string          `json:"budget_type,omitempty"`
	BudgetAmount             *json.Number     `json:"budget_amount,omitempty"`
	PreventFurtherUsage      *bool            `json:"prevent_further_usage,omitempty"`
	BudgetScope              *string          `json:"budget_scope,omitempty"`
	BudgetEntityName         *string          `json:"budget_entity_name,omitempty"`
	User                     *string          `json:"user,omitempty"`
	ConsumedAmount           *json.Number     `json:"consumed_amount,omitempty"`
	BudgetProductSKU         *string          `json:"budget_product_sku,omitempty"`
	BudgetAlerting           *BillingAlerting `json:"budget_alerting,omitempty"`
	AlertRecipientsTruncated bool             `json:"alert_recipients_truncated,omitempty"`
	ExpiresAt                *string          `json:"expires_at,omitempty"`
}

// BillingEffectiveBudget is the effective budget of the user a budget list was narrowed to.
type BillingEffectiveBudget struct {
	ID             *string      `json:"id,omitempty"`
	BudgetAmount   *json.Number `json:"budget_amount,omitempty"`
	ConsumedAmount *json.Number `json:"consumed_amount,omitempty"`
}

// BillingBudgetList is one batch of the budgets of an organization.
type BillingBudgetList struct {
	Budgets         []BillingBudget         `json:"budgets"`
	User            *string                 `json:"user,omitempty"`
	EffectiveBudget *BillingEffectiveBudget `json:"effective_budget,omitempty"`
	TotalCount      *int64                  `json:"total_count,omitempty"`
	Withheld        int                     `json:"withheld,omitempty"`
	NextCursor      string                  `json:"next_cursor,omitempty"`
	HasMore         bool                    `json:"has_more"`
}

// ---- Raw answers

type billingUsageItemRaw struct {
	Date             *string      `json:"date"`
	Product          *string      `json:"product"`
	SKU              *string      `json:"sku"`
	Quantity         *json.Number `json:"quantity"`
	UnitType         *string      `json:"unitType"`
	PricePerUnit     *json.Number `json:"pricePerUnit"`
	GrossAmount      *json.Number `json:"grossAmount"`
	DiscountAmount   *json.Number `json:"discountAmount"`
	NetAmount        *json.Number `json:"netAmount"`
	OrganizationName *string      `json:"organizationName"`
	RepositoryName   *string      `json:"repositoryName"`
}

type billingQuantityItemRaw struct {
	Product          *string      `json:"product"`
	SKU              *string      `json:"sku"`
	Model            *string      `json:"model"`
	UnitType         *string      `json:"unitType"`
	PricePerUnit     *json.Number `json:"pricePerUnit"`
	GrossQuantity    *json.Number `json:"grossQuantity"`
	GrossAmount      *json.Number `json:"grossAmount"`
	DiscountQuantity *json.Number `json:"discountQuantity"`
	DiscountAmount   *json.Number `json:"discountAmount"`
	NetQuantity      *json.Number `json:"netQuantity"`
	NetAmount        *json.Number `json:"netAmount"`
}

type billingTimePeriodRaw struct {
	Year  *int64 `json:"year"`
	Month *int64 `json:"month"`
	Day   *int64 `json:"day"`
}

func (r *billingTimePeriodRaw) project() *BillingTimePeriod {
	if r == nil || (r.Year == nil && r.Month == nil && r.Day == nil) {
		return nil
	}
	return &BillingTimePeriod{Year: r.Year, Month: r.Month, Day: r.Day}
}

type billingReportRaw struct {
	TimePeriod   *billingTimePeriodRaw    `json:"timePeriod"`
	Organization *string                  `json:"organization"`
	User         *string                  `json:"user"`
	Repository   *string                  `json:"repository"`
	Product      *string                  `json:"product"`
	SKU          *string                  `json:"sku"`
	Model        *string                  `json:"model"`
	UsageItems   []billingQuantityItemRaw `json:"usageItems"`
}

type billingBudgetRaw struct {
	ID                  *string      `json:"id"`
	BudgetType          *string      `json:"budget_type"`
	BudgetAmount        *json.Number `json:"budget_amount"`
	PreventFurtherUsage *bool        `json:"prevent_further_usage"`
	BudgetScope         *string      `json:"budget_scope"`
	BudgetEntityName    *string      `json:"budget_entity_name"`
	User                *string      `json:"user"`
	ConsumedAmount      *json.Number `json:"consumed_amount"`
	BudgetProductSKU    *string      `json:"budget_product_sku"`
	BudgetAlerting      *struct {
		WillAlert       *bool    `json:"will_alert"`
		AlertRecipients []string `json:"alert_recipients"`
	} `json:"budget_alerting"`
	ExpiresAt *string `json:"expires_at"`
}

// ---- Requests

// billingPath is the route of one billing report below the bound owner. The organization routes start with
// /organizations/, not /orgs/.
func (c *Client) billingPath(rest string) string {
	root := "/users/"
	if c.target.scope == "orgs" {
		root = "/organizations/"
	}
	return root + url.PathEscape(c.target.owner) + "/settings/billing/" + rest
}

func (c *Client) billingRead(ctx context.Context, op, rest string, query url.Values, out any) error {
	path := c.billingPath(rest)
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return c.rest(ctx, op, path, out)
}

// billingState turns a read failure: a refusal with 403 or 404 into the unavailable state; every other failure stays an error.
func billingState(err error) (any, error) {
	var failure *provider.Error
	if errors.As(err, &failure) && (failure.Class == provider.ClassPermission || failure.Class == provider.ClassNotFound) {
		return &BillingUnavailable{State: billingUnavailableState, Reason: billingUnavailableReason}, nil
	}
	return nil, err
}

func billingText(p *string) *string { return shortText(p) }

func (c *Client) billingUsage(ctx context.Context, a *billingArguments) (any, error) {
	const op = "get billing usage"
	var raw struct {
		UsageItems []billingUsageItemRaw `json:"usageItems"`
	}
	if err := c.billingRead(ctx, op, "usage", a.query, &raw); err != nil {
		return billingState(err)
	}
	result := &BillingUsage{UsageItems: []BillingUsageItem{}}
	for _, item := range raw.UsageItems {
		// An excluded repository takes its line items out of the result, not only out of the request.
		if item.RepositoryName != nil && *item.RepositoryName != "" && !c.allowed.shows(c.target,
			target{kind: kindRepository, owner: c.target.owner, repo: *item.RepositoryName}) {
			result.Withheld++
			continue
		}
		if len(result.UsageItems) == maxBillingItems {
			result.Truncated = true
			continue
		}
		result.UsageItems = append(result.UsageItems, BillingUsageItem{Date: billingText(item.Date),
			Product: billingText(item.Product), SKU: billingText(item.SKU), Quantity: item.Quantity,
			UnitType: billingText(item.UnitType), PricePerUnit: item.PricePerUnit, GrossAmount: item.GrossAmount,
			DiscountAmount: item.DiscountAmount, NetAmount: item.NetAmount,
			OrganizationName: billingText(item.OrganizationName), RepositoryName: billingText(item.RepositoryName)})
	}
	return result, nil
}

// billingReport reads one of the three summed reports. GitHub pages none of them, so a report over the bound
// is cut and the caller marks it truncated.
func (c *Client) billingReport(ctx context.Context, op, rest string, a *billingArguments) (*billingReportRaw, bool, error) {
	var raw billingReportRaw
	if err := c.billingRead(ctx, op, rest, a.query, &raw); err != nil {
		return nil, false, err
	}
	cut := len(raw.UsageItems) > maxBillingItems
	if cut {
		raw.UsageItems = raw.UsageItems[:maxBillingItems]
	}
	return &raw, cut, nil
}

func (c *Client) billingSummary(ctx context.Context, a *billingArguments) (any, error) {
	raw, cut, err := c.billingReport(ctx, "get billing usage summary", "usage/summary", a)
	if err != nil {
		return billingState(err)
	}
	result := &BillingSummary{TimePeriod: raw.TimePeriod.project(), Organization: billingText(raw.Organization),
		User: billingText(raw.User), Repository: billingText(raw.Repository), Product: billingText(raw.Product),
		SKU: billingText(raw.SKU), UsageItems: make([]BillingQuantityItem, 0, len(raw.UsageItems)), Truncated: cut}
	for _, item := range raw.UsageItems {
		result.UsageItems = append(result.UsageItems, BillingQuantityItem{Product: billingText(item.Product),
			SKU: billingText(item.SKU), UnitType: billingText(item.UnitType), PricePerUnit: item.PricePerUnit,
			GrossQuantity: item.GrossQuantity, GrossAmount: item.GrossAmount, DiscountQuantity: item.DiscountQuantity,
			DiscountAmount: item.DiscountAmount, NetQuantity: item.NetQuantity, NetAmount: item.NetAmount})
	}
	return result, nil
}

func (c *Client) billingModelReport(ctx context.Context, op, rest string, a *billingArguments) (any, error) {
	raw, cut, err := c.billingReport(ctx, op, rest, a)
	if err != nil {
		return billingState(err)
	}
	result := &BillingModelReport{TimePeriod: raw.TimePeriod.project(), Organization: billingText(raw.Organization),
		User: billingText(raw.User), Product: billingText(raw.Product), Model: billingText(raw.Model),
		UsageItems: make([]BillingModelItem, 0, len(raw.UsageItems)), Truncated: cut}
	for _, item := range raw.UsageItems {
		result.UsageItems = append(result.UsageItems, BillingModelItem{Product: billingText(item.Product),
			SKU: billingText(item.SKU), Model: billingText(item.Model), UnitType: billingText(item.UnitType),
			PricePerUnit: item.PricePerUnit, GrossQuantity: item.GrossQuantity, GrossAmount: item.GrossAmount,
			DiscountQuantity: item.DiscountQuantity, DiscountAmount: item.DiscountAmount,
			NetQuantity: item.NetQuantity, NetAmount: item.NetAmount})
	}
	return result, nil
}

// withheldBudget reports whether the budget belongs to a repository this connection excludes.
func (c *Client) withheldBudget(r *billingBudgetRaw) bool {
	if r.BudgetScope == nil || *r.BudgetScope != "repository" || r.BudgetEntityName == nil {
		return false
	}
	owner, name, found := strings.Cut(*r.BudgetEntityName, "/")
	if !found {
		owner, name = c.target.owner, *r.BudgetEntityName
	}
	return !c.allowed.shows(c.target, target{kind: kindRepository, owner: owner, repo: name})
}

func budgetView(op string, r *billingBudgetRaw, expiry bool) (*BillingBudget, error) {
	if r.ID == nil || *r.ID == "" {
		return nil, invalidEntry(op, "a budget")
	}
	budget := &BillingBudget{ID: *nameText(r.ID), BudgetType: billingText(r.BudgetType), BudgetAmount: r.BudgetAmount,
		PreventFurtherUsage: r.PreventFurtherUsage, BudgetScope: billingText(r.BudgetScope),
		BudgetEntityName: billingText(r.BudgetEntityName), User: billingText(r.User), ConsumedAmount: r.ConsumedAmount,
		BudgetProductSKU: billingText(r.BudgetProductSKU)}
	if expiry {
		budget.ExpiresAt = billingText(r.ExpiresAt)
	}
	if alerting := r.BudgetAlerting; alerting != nil {
		view := &BillingAlerting{WillAlert: alerting.WillAlert}
		recipients, cut := capped(alerting.AlertRecipients)
		for _, login := range recipients {
			if text := shortText(&login); text != nil && *text != "" {
				view.AlertRecipients = append(view.AlertRecipients, *text)
			}
		}
		budget.AlertRecipientsTruncated = cut
		budget.BudgetAlerting = view
	}
	return budget, nil
}

func (c *Client) billingBudgetsList(ctx context.Context, a *billingArguments) (any, error) {
	const op = "list billing budgets"
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if a.Scope != "" {
		query.Set("scope", a.Scope)
	}
	if a.User != "" {
		query.Set("user", a.User)
	}
	var raw struct {
		Budgets         []billingBudgetRaw `json:"budgets"`
		User            *string            `json:"user"`
		EffectiveBudget *struct {
			ID             *string      `json:"id"`
			BudgetAmount   *json.Number `json:"budget_amount"`
			ConsumedAmount *json.Number `json:"consumed_amount"`
		} `json:"effective_budget"`
		HasNextPage *bool  `json:"has_next_page"`
		TotalCount  *int64 `json:"total_count"`
	}
	if err := c.billingRead(ctx, op, "budgets", query, &raw); err != nil {
		return billingState(err)
	}
	result := &BillingBudgetList{Budgets: make([]BillingBudget, 0, len(raw.Budgets)), User: billingText(raw.User),
		TotalCount: raw.TotalCount}
	if e := raw.EffectiveBudget; e != nil {
		result.EffectiveBudget = &BillingEffectiveBudget{ID: billingText(e.ID), BudgetAmount: e.BudgetAmount,
			ConsumedAmount: e.ConsumedAmount}
	}
	for i := range raw.Budgets {
		if c.withheldBudget(&raw.Budgets[i]) {
			result.Withheld++
			continue
		}
		budget, err := budgetView(op, &raw.Budgets[i], true)
		if err != nil {
			return nil, err
		}
		result.Budgets = append(result.Budgets, *budget)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, raw.HasNextPage != nil && *raw.HasNextPage)
	return result, nil
}

func (c *Client) billingBudgetsGet(ctx context.Context, a *billingArguments) (any, error) {
	const op = "get billing budget"
	var raw billingBudgetRaw
	if err := c.billingRead(ctx, op, "budgets/"+url.PathEscape(a.BudgetID), nil, &raw); err != nil {
		return billingState(err)
	}
	// A budget of an excluded repository is not shown, as if it did not exist.
	if c.withheldBudget(&raw) {
		return nil, notFound(op, subject{in: c.target, what: "this budget"})
	}
	return budgetView(op, &raw, false)
}

func billingOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: billingUsageGet, Handler: billingHandler(billingUsageGet.ID, false, checkBillingUsage,
			func(ctx context.Context, c *Client, a *billingArguments) (any, error) { return c.billingUsage(ctx, a) })},
		{Descriptor: billingSummaryGet, Handler: billingHandler(billingSummaryGet.ID, false, checkBillingSummary,
			func(ctx context.Context, c *Client, a *billingArguments) (any, error) {
				return c.billingSummary(ctx, a)
			})},
		{Descriptor: billingPremiumRequestsGet, Handler: billingHandler(billingPremiumRequestsGet.ID, false,
			checkBillingModelReport, func(ctx context.Context, c *Client, a *billingArguments) (any, error) {
				return c.billingModelReport(ctx, "get billing premium request usage", "premium_request/usage", a)
			})},
		{Descriptor: billingAICreditsGet, Handler: billingHandler(billingAICreditsGet.ID, false,
			checkBillingModelReport, func(ctx context.Context, c *Client, a *billingArguments) (any, error) {
				return c.billingModelReport(ctx, "get billing AI credit usage", "ai_credit/usage", a)
			})},
		{Descriptor: billingBudgetsList, Handler: billingHandler(billingBudgetsList.ID, true, checkBillingBudgetsList,
			func(ctx context.Context, c *Client, a *billingArguments) (any, error) {
				return c.billingBudgetsList(ctx, a)
			})},
		{Descriptor: billingBudgetsGet, Handler: billingHandler(billingBudgetsGet.ID, true, checkBillingBudgetsGet,
			func(ctx context.Context, c *Client, a *billingArguments) (any, error) {
				return c.billingBudgetsGet(ctx, a)
			})},
	}
}
