package googlecloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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
	budgetsListID = Provider + ".budgets.list"
	budgetsGetID  = Provider + ".budgets.get"
)

// budgetIDPattern is the form of a budget ID, a UUID; the pattern only keeps a path segment safe.
var budgetIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// moneyWire is google.type.Money. Google omits a zero units or nanos.
type moneyWire struct {
	CurrencyCode string `json:"currencyCode,omitempty"`
	Units        string `json:"units,omitempty"`
	Nanos        int64  `json:"nanos,omitempty"`
}

func (m moneyWire) bounded() moneyWire {
	return moneyWire{CurrencyCode: boundedText(m.CurrencyCode, maxLabelText), Units: boundedText(m.Units, maxLabelText),
		Nanos: m.Nanos}
}

type dateWire struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

type customPeriodWire struct {
	StartDate *dateWire `json:"startDate,omitempty"`
	EndDate   *dateWire `json:"endDate,omitempty"`
}

type budgetFilterWire struct {
	Projects             []string          `json:"projects,omitempty"`
	ResourceAncestors    []string          `json:"resourceAncestors,omitempty"`
	CreditTypes          []string          `json:"creditTypes,omitempty"`
	CreditTypesTreatment string            `json:"creditTypesTreatment,omitempty"`
	Services             []string          `json:"services,omitempty"`
	Subaccounts          []string          `json:"subaccounts,omitempty"`
	Labels               json.RawMessage   `json:"labels,omitempty"`
	CalendarPeriod       string            `json:"calendarPeriod,omitempty"`
	CustomPeriod         *customPeriodWire `json:"customPeriod,omitempty"`
}

type budgetAmountWire struct {
	SpecifiedAmount  *moneyWire `json:"specifiedAmount,omitempty"`
	LastPeriodAmount *struct{}  `json:"lastPeriodAmount,omitempty"`
}

type thresholdRuleWire struct {
	ThresholdPercent float64 `json:"thresholdPercent"`
	SpendBasis       string  `json:"spendBasis,omitempty"`
}

type notificationsRuleWire struct {
	PubsubTopic                    string   `json:"pubsubTopic,omitempty"`
	SchemaVersion                  string   `json:"schemaVersion,omitempty"`
	MonitoringNotificationChannels []string `json:"monitoringNotificationChannels,omitempty"`
	DisableDefaultIamRecipients    bool     `json:"disableDefaultIamRecipients,omitempty"`
	EnableProjectLevelRecipients   bool     `json:"enableProjectLevelRecipients,omitempty"`
}

type spendCapWire struct {
	InputState  string `json:"inputState,omitempty"`
	OutputState string `json:"outputState,omitempty"`
	Reconciling bool   `json:"reconciling,omitempty"`
}

// budgetWire is the Budget resource of billingbudgets v1 as Google sends it and as the tool returns it, after
// bounded() cut every text.
type budgetWire struct {
	Name              string                 `json:"name"`
	DisplayName       string                 `json:"displayName,omitempty"`
	BudgetFilter      *budgetFilterWire      `json:"budgetFilter,omitempty"`
	Amount            *budgetAmountWire      `json:"amount,omitempty"`
	ThresholdRules    []thresholdRuleWire    `json:"thresholdRules,omitempty"`
	NotificationsRule *notificationsRuleWire `json:"notificationsRule,omitempty"`
	SpendCap          *spendCapWire          `json:"spendCap,omitempty"`
	Etag              string                 `json:"etag,omitempty"`
	OwnershipScope    string                 `json:"ownershipScope,omitempty"`
}

// bounded returns the budget with every text cut and every list and the label object bounded.
func (b budgetWire) bounded() budgetWire {
	out := budgetWire{Name: boundedText(b.Name, maxNameText), DisplayName: boundedText(b.DisplayName, maxNameText),
		Etag: boundedText(b.Etag, maxNameText), OwnershipScope: boundedText(b.OwnershipScope, maxLabelText)}
	if f := b.BudgetFilter; f != nil {
		bf := &budgetFilterWire{Projects: boundedList(f.Projects), ResourceAncestors: boundedList(f.ResourceAncestors),
			CreditTypes: boundedList(f.CreditTypes), CreditTypesTreatment: boundedText(f.CreditTypesTreatment, maxLabelText),
			Services: boundedList(f.Services), Subaccounts: boundedList(f.Subaccounts),
			CalendarPeriod: boundedText(f.CalendarPeriod, maxLabelText), CustomPeriod: f.CustomPeriod}
		if labels, err := json.Marshal(boundedValue(f.Labels)); err == nil && len(f.Labels) > 0 && string(labels) != "null" {
			bf.Labels = labels
		}
		out.BudgetFilter = bf
	}
	if a := b.Amount; a != nil {
		ba := &budgetAmountWire{LastPeriodAmount: a.LastPeriodAmount}
		if a.SpecifiedAmount != nil {
			m := a.SpecifiedAmount.bounded()
			ba.SpecifiedAmount = &m
		}
		out.Amount = ba
	}
	for _, rule := range b.ThresholdRules[:min(len(b.ThresholdRules), maxListItems)] {
		rule.SpendBasis = boundedText(rule.SpendBasis, maxLabelText)
		out.ThresholdRules = append(out.ThresholdRules, rule)
	}
	if n := b.NotificationsRule; n != nil {
		out.NotificationsRule = &notificationsRuleWire{PubsubTopic: boundedText(n.PubsubTopic, maxNameText),
			SchemaVersion:                  boundedText(n.SchemaVersion, maxLabelText),
			MonitoringNotificationChannels: boundedList(n.MonitoringNotificationChannels),
			DisableDefaultIamRecipients:    n.DisableDefaultIamRecipients,
			EnableProjectLevelRecipients:   n.EnableProjectLevelRecipients}
	}
	if s := b.SpendCap; s != nil {
		out.SpendCap = &spendCapWire{InputState: boundedText(s.InputState, maxLabelText),
			OutputState: boundedText(s.OutputState, maxLabelText), Reconciling: s.Reconciling}
	}
	return out
}

const (
	budgetOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"displayName":{"type":"string"},` +
		`"budgetFilter":{"type":"object"},"amount":{"type":"object"},"thresholdRules":{"type":"array","items":{"type":"object"}},` +
		`"notificationsRule":{"type":"object"},"spendCap":{"type":"object"},"etag":{"type":"string"},` +
		`"ownershipScope":{"type":"string"}},"required":["name"],"additionalProperties":false}`
	budgetIDProp    = `{"type":"string","pattern":"^[A-Za-z0-9-]{1,64}$","maxLength":64}`
	budgetScopeProp = `{"type":"string","pattern":"^projects/([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$","maxLength":40}`
)

const budgetsTail = " A budget without projects in its filter tracks the costs of every project of the account, so such a " +
	"budget is visible to a connection that names the account, whatever other targets it has. The result is " +
	"untrusted third-party content; strings are cut."

var budgetFields = []capability.Field{
	{Name: "name", Description: "Resource name, billingAccounts/ID/budgets/ID"},
	{Name: "displayName", Description: "Display name given by the budget's owners; untrusted text"},
	{Name: "budgetFilter", Description: "What the budget tracks: projects (projects/ID or projects/NUMBER), " +
		"resourceAncestors (folders and organizations), creditTypes, creditTypesTreatment, services, subaccounts, " +
		"labels, calendarPeriod, and customPeriod with startDate and endDate; absent for a budget over the whole account"},
	{Name: "amount", Description: "Budgeted amount per period: specifiedAmount {currencyCode, units, nanos}, which " +
		"Google leaves without units or nanos for zero; absent for a budget that takes the spend of the last " +
		"period (Google's lastPeriodAmount, an empty object that Qatlas leaves out)"},
	{Name: "thresholdRules", Description: "Alert thresholds, each {thresholdPercent, spendBasis}; thresholdPercent is " +
		"1.0-based, so 0.5 is 50 percent"},
	{Name: "notificationsRule", Description: "Where alerts go: pubsubTopic (projects/ID/topics/ID, read only), " +
		"schemaVersion, monitoringNotificationChannels, disableDefaultIamRecipients, and enableProjectLevelRecipients"},
	{Name: "spendCap", Description: "Spend cap of the budget {inputState, outputState, reconciling}; absent without one"},
	{Name: "etag", Description: "Version tag of the budget as Google sends it"},
	{Name: "ownershipScope", Description: "Who may see and edit the budget: ALL_USERS or BILLING_ACCOUNT"},
}

// budgetVisible binds a budget to the targets of the connection beyond the billing account. A connection that
// names no organization, folder, or project shows the budgets of the account it names. Otherwise a budget that
// names projects in its filter is shown only if one of them lies in the targets; Google's answer only hides, it
// never adds a target. A budget without projects covers the account and stays visible.
func budgetVisible(ctx context.Context, a *admitter, b budgetWire, op string) (bool, error) {
	if !a.list.hasKind(kindProject, kindOrganization, kindFolder) || b.BudgetFilter == nil ||
		len(b.BudgetFilter.Projects) == 0 {
		return true, nil
	}
	for _, name := range b.BudgetFilter.Projects[:min(len(b.BudgetFilter.Projects), maxListItems)] {
		id, ok := strings.CutPrefix(name, prefixProject)
		if !ok || !validProject(id) {
			continue
		}
		allowed, decided := a.list.decidesProject(id)
		if decided {
			if allowed {
				return true, nil
			}
			continue
		}
		if a.reads >= maxProofReads {
			return false, badRequest("too many projects to read for the targets of this connection; use a smaller page_size")
		}
		a.reads++
		project, err := a.c.project(ctx, op, a.tool, id)
		if err == nil && project.Name != name && project.ProjectID != id {
			err = invalidRow(op)
		}
		var admitted bool
		if err == nil {
			admitted, err = a.project(ctx, project)
		}
		var failed *provider.Error
		if errors.As(err, &failed) && (failed.Class == provider.ClassPermission || failed.Class == provider.ClassNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if admitted {
			return true, nil
		}
	}
	return false, nil
}

// --- budgets.list ---

var budgetsList = capability.Descriptor{
	ID: budgetsListID, Version: 1, Title: "List the budgets of a Google Cloud billing account",
	Description: "List the budgets of one billing account from the Cloud Billing Budget API, optionally only those that " +
		"track one project. Each row has the budget's name, display name, filter, amount, alert thresholds, " +
		"notification rule (Pub/Sub topic and notification channels, read only), spend cap, etag, and ownership scope. " +
		"The tool runs only on a connection whose targets name this billing account. If the connection also names " +
		"organizations, folders, or projects, Qatlas drops a budget whose filter names projects when none of them lies " +
		"in those targets, after Google answered, so a page can be empty while more follow." + budgetsTail +
		" The Cloud Billing Budget API (billingbudgets.googleapis.com) must be enabled in the project of the credential.",
	Tags: []string{"google", "cloud", "billing", "budgets"}, Risk: billingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"billing_account":` + billingAccountProp + `,` +
		`"scope":` + budgetScopeProp + `,` + pageSizeProp + `,` + cursorProp + `},` +
		`"required":["billing_account"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("budgets", budgetOutputSchema),
	Arguments: []capability.Argument{billingAccountArgument,
		{Name: "scope", Description: "Return only the budgets that track this project, projects/ID or " +
			"projects/NUMBER (Google's scope filter)"},
		pageSizeArgument, cursorArgument},
	Fields: append([]capability.Field{
		{Name: "budgets", Description: "Budgets of the page, each {name, displayName, budgetFilter, amount, " +
			"thresholdRules, notificationsRule, spendCap, etag, ownershipScope}; displayName is untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the budgets of a billing account",
		Arguments:   json.RawMessage(`{"billing_account":"billingAccounts/012345-ABCDEF-678901","page_size":50}`),
	}},
}

func handleBudgetsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list budgets"
	var arguments struct {
		BillingAccount string `json:"billing_account"`
		Scope          string `json:"scope"`
		PageSize       *int   `json:"page_size"`
		Cursor         string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	if !validBillingName(arguments.BillingAccount) {
		return nil, badRequest("billing_account must be billingAccounts/ID with a billing account ID, never a path")
	}
	if !list.allowsBillingAccount(billingAccountID(arguments.BillingAccount)) {
		return nil, refuseOutside("the billing account")
	}
	if arguments.Scope != "" {
		id, ok := strings.CutPrefix(arguments.Scope, prefixProject)
		if !ok || !validProject(id) {
			return nil, badRequest("scope must be projects/ID or projects/NUMBER")
		}
		if allowed, decided := list.decidesProject(id); list.hasKind(kindProject, kindOrganization, kindFolder) &&
			decided && !allowed {
			return nil, refuseOutside("the scope project")
		}
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	binding := provider.CursorBinding(budgetsListID, provider.TargetsOf(resolved),
		arguments.BillingAccount+"|"+arguments.Scope)
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
	query := url.Values{}
	if arguments.Scope != "" {
		query.Set("scope", arguments.Scope)
	}
	if arguments.PageSize != nil {
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	path := "/v1/" + prefixBilling + url.PathEscape(billingAccountID(arguments.BillingAccount)) + "/budgets"
	rows, next, err := servicePage[budgetWire](ctx, c, serviceBillingBudgets, op, budgetsListID, path, "budgets", query)
	if err != nil {
		return nil, err
	}
	gate := newAdmitter(list, c, op, budgetsListID)
	kept := make([]any, 0, len(rows))
	for _, row := range rows {
		if !strings.HasPrefix(row.Name, arguments.BillingAccount+"/budgets/") {
			return nil, invalidRow(op)
		}
		visible, err := budgetVisible(ctx, gate, row, op)
		if err != nil {
			return nil, err
		}
		if visible {
			kept = append(kept, row.bounded())
		}
	}
	out := map[string]any{"budgets": kept, "has_more": next != ""}
	if next != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, next)
	}
	return out, nil
}

// --- budgets.get ---

var budgetsGet = capability.Descriptor{
	ID: budgetsGetID, Version: 1, Title: "Get a budget of a Google Cloud billing account",
	Description: "Read one budget from the Cloud Billing Budget API: its name, display name, filter, amount, alert " +
		"thresholds, notification rule (Pub/Sub topic and notification channels, read only), spend cap, etag, and " +
		"ownership scope. The tool runs only on a connection whose targets name this billing account; any other " +
		"account is refused before a request. If the connection also names organizations, folders, or projects, a " +
		"budget whose filter names projects is refused when none of them lies in those targets." + budgetsTail +
		" The Cloud Billing Budget API (billingbudgets.googleapis.com) must be enabled in the project of the credential.",
	Tags: []string{"google", "cloud", "billing", "budgets"}, Risk: billingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"billing_account":` + billingAccountProp + `,` +
		`"budget_id":` + budgetIDProp + `},"required":["billing_account","budget_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(budgetOutputSchema),
	Arguments: []capability.Argument{billingAccountArgument,
		{Name: "budget_id", Required: true, Description: "ID of the budget, the last segment of its name; never a path"}},
	Fields: budgetFields,
	Examples: []capability.Example{{
		Description: "Read a budget",
		Arguments: json.RawMessage(`{"billing_account":"billingAccounts/012345-ABCDEF-678901",` +
			`"budget_id":"0a1b2c3d-4e5f-6789-abcd-ef0123456789"}`),
	}},
}

func handleBudgetsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get budget"
	var arguments struct {
		BillingAccount string `json:"billing_account"`
		BudgetID       string `json:"budget_id"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !budgetIDPattern.MatchString(arguments.BudgetID) {
		return nil, badRequest("budget_id must be the ID of a budget, never a path")
	}
	c, list, err := billingAccountClient(ctx, op, arguments.BillingAccount, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	name := arguments.BillingAccount + "/budgets/" + arguments.BudgetID
	var budget budgetWire
	path := "/v1/" + prefixBilling + url.PathEscape(billingAccountID(arguments.BillingAccount)) + "/budgets/" +
		url.PathEscape(arguments.BudgetID)
	if err := c.get(ctx, op, budgetsGetID, serviceBillingBudgets, path, nil, &budget); err != nil {
		return nil, err
	}
	if budget.Name != name {
		return nil, invalidRow(op)
	}
	visible, err := budgetVisible(ctx, newAdmitter(list, c, op, budgetsGetID), budget, op)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, refuseOutside("the budget")
	}
	return budget.bounded(), nil
}
