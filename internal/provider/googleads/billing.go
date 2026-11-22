package googleads

import (
	"context"
	"encoding/json"
	"net/http"
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
	billingSetupsListID          = Provider + ".billing-setups.list"
	accountBudgetsListID         = Provider + ".account-budgets.list"
	accountBudgetProposalsListID = Provider + ".account-budget-proposals.list"
	paymentsAccountsListID       = Provider + ".payments-accounts.list"
)

// The billing tools only read. Billing setups, account budgets and their proposals come from GAQL searches
// with fixed field sets, the payments accounts from PaymentsAccountService.ListPaymentsAccounts. Nothing here
// creates, changes or removes billing, a budget proposal or a payments account, and no argument names a
// field, a query or an operation. Billing belongs to the paying account, so on a manager account every tool
// needs a manager target that names it (callEnv.settle with managerNeedsTarget).

// Values of the status and type enums of v26 that a filter may name; UNSPECIFIED and UNKNOWN are no values to
// ask for.
var (
	billingSetupStatuses    = []string{"PENDING", "APPROVED_HELD", "APPROVED", "CANCELLED"}
	accountBudgetStatuses   = []string{"PENDING", "APPROVED", "CANCELLED"}
	budgetProposalStatuses  = []string{"PENDING", "APPROVED_HELD", "APPROVED", "CANCELLED", "REJECTED"}
	budgetProposalTypes     = []string{"CREATE", "UPDATE", "END", "REMOVE"}
	paymentsAccountSegment  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	managerCustomerResource = regexp.MustCompile(`^customers/[0-9]{1,10}$`)
)

// ownName reports whether a resource name is name of the collection of the queried account with an ID of the
// given form.
func ownName(name, customerID, collection string, id *regexp.Regexp) bool {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+collection+"/")
	return ok && id.MatchString(rest)
}

// billingRisk is the risk of the four tools: they read, and the answer holds business data of the payer.
var billingRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "billing data of a Google Ads account: payments account and payments profile IDs and names, " +
		"billing setup status and dates, account budget amounts, notes and purchase order numbers; no card or " +
		"bank data and no data of other people",
}

const billingAccessNote = "customer_id must be one of the targets or lie below a manager target; on a connection " +
	"with targets, a manager account needs a manager target that names it. "

const billingExplorerNote = "Google's access level page restricts PaymentsAccountService, BillingSetupService, " +
	"AccountBudgetProposalService and InvoiceService to the Basic or Standard access level and blocks them at " +
	"Explorer. "

// textProperties is a list of output properties that are plain strings.
func textProperties(names ...string) schema {
	out := schema{}
	for _, name := range names {
		out[name] = stringSchema
	}
	return out
}

func mergeProperties(parts ...schema) schema {
	out := schema{}
	for _, part := range parts {
		for k, v := range part {
			out[k] = v
		}
	}
	return out
}

func idsProperty() schema {
	return schema{"type": "array", "minItems": 1, "maxItems": maxClientIDs, "uniqueItems": true,
		"items": schema{"type": "string", "pattern": idPatternText}}
}

func enumProperty(values []string) schema {
	return schema{"type": "array", "minItems": 1, "maxItems": len(values), "uniqueItems": true,
		"items": schema{"type": "string", "enum": values}}
}

// billingListArguments are the filters of the three GAQL lists. The proposal types belong to the proposals only;
// the schemas of the other tools do not offer them and the handlers refuse them as well.
type billingListArguments struct {
	CustomerID    string   `json:"customer_id"`
	IDs           []string `json:"ids"`
	Statuses      []string `json:"statuses"`
	ProposalTypes []string `json:"proposal_types"`
	PageSize      *int     `json:"page_size"`
	Cursor        string   `json:"cursor"`
}

// billingList is what differs between the three lists.
type billingList struct {
	tool, op, resource string
	fields             []string
	statuses           []string
	types              []string
}

func (b billingList) conditions(a billingListArguments) ([]string, error) {
	var out []string
	for _, c := range []struct {
		argument, field string
		values, allowed []string
	}{
		{"statuses", b.resource + ".status", a.Statuses, b.statuses},
		{"proposal_types", b.resource + ".proposal_type", a.ProposalTypes, b.types},
	} {
		if len(c.values) > 0 && len(c.allowed) == 0 {
			return nil, badRequest(c.argument + " is not an argument of this tool")
		}
		condition, err := enumList(c.argument, c.field, c.values, c.allowed)
		if err != nil {
			return nil, err
		}
		if condition != "" {
			out = append(out, condition)
		}
	}
	ids, err := idList("ids", b.resource+".id", a.IDs)
	if err != nil {
		return nil, err
	}
	if ids != "" {
		out = append(out, ids)
	}
	return out, nil
}

// query selects exactly the fields of the list and pages by the ID of the resource.
func (b billingList) query(conditions []string, after string, limit int) string {
	fields := make([]string, len(b.fields))
	for i, field := range b.fields {
		fields[i] = b.resource + "." + field
	}
	query := "SELECT " + strings.Join(fields, ", ") + " FROM " + b.resource
	if after != "" {
		conditions = append(append([]string(nil), conditions...), b.resource+".id > "+after)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query + " ORDER BY " + b.resource + ".id LIMIT " + strconv.Itoa(limit)
}

// runBillingList checks the arguments and runs the list. mapRow receives the row and the checked account and
// returns the item and its ID.
func runBillingList[T any](ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, spec billingList, mapRow func(json.RawMessage, string) (T, string, error)) (items []T, next string,
	more bool, err error) {
	var args billingListArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, "", false, failure(spec.op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, spec.op, spec.tool)
	if err != nil {
		return nil, "", false, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, "", false, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	conditions, err := spec.conditions(args)
	if err != nil {
		return nil, "", false, err
	}
	return listAccess(env, listSpec{customerID: args.CustomerID, tool: spec.tool, op: spec.op, conditions: conditions,
		pageSize: args.PageSize, cursor: args.Cursor, query: spec.query},
		func(raw json.RawMessage) (T, string, error) { return mapRow(raw, args.CustomerID) })
}

func billingInput(statuses, types []string) json.RawMessage {
	properties := schema{"customer_id": customerIDArgument, "ids": idsProperty(), "statuses": enumProperty(statuses),
		"page_size": pageProperties["page_size"], "cursor": pageProperties["cursor"]}
	if types != nil {
		properties["proposal_types"] = enumProperty(types)
	}
	return encoded(object(properties, "customer_id"))
}

func billingArguments(resource string, statuses, types []string, idsName string) []capability.Argument {
	args := []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target. A manager account needs a manager target " +
			"that names it"},
		{Name: "ids", Description: "Only these " + idsName + " IDs, at most 100"},
		{Name: "statuses", Description: "Only " + resource + " with one of these statuses: " + joinOr(statuses)},
	}
	if types != nil {
		args = append(args, capability.Argument{Name: "proposal_types", Description: "Only proposals of one of these " +
			"types: " + joinOr(types)})
	}
	return append(args, pageArguments...)
}

func billingOutput(key string, item schema) json.RawMessage {
	return encoded(object(schema{key: arrayOf(item), "next_cursor": stringSchema, "has_more": booleanSchema}, key, "has_more"))
}

var billingPageFields = []capability.Field{
	{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
	{Name: "has_more", Description: "true when more rows follow"},
}

// --- billing setups ---

var billingSetupFields = []string{"resource_name", "id", "status", "payments_account",
	"payments_account_info.payments_account_id", "payments_account_info.payments_account_name",
	"payments_account_info.payments_profile_id", "payments_account_info.payments_profile_name",
	"payments_account_info.secondary_payments_profile_id", "start_date_time", "end_date_time", "end_time_type"}

type (
	paymentsAccountInfoWire struct {
		PaymentsAccountID          string `json:"paymentsAccountId"`
		PaymentsAccountName        string `json:"paymentsAccountName"`
		PaymentsProfileID          string `json:"paymentsProfileId"`
		PaymentsProfileName        string `json:"paymentsProfileName"`
		SecondaryPaymentsProfileID string `json:"secondaryPaymentsProfileId"`
	}
	billingSetupRow struct {
		BillingSetup *struct {
			ResourceName        string                   `json:"resourceName"`
			ID                  *wireInt                 `json:"id"`
			Status              string                   `json:"status"`
			PaymentsAccount     string                   `json:"paymentsAccount"`
			PaymentsAccountInfo *paymentsAccountInfoWire `json:"paymentsAccountInfo"`
			StartDateTime       string                   `json:"startDateTime"`
			EndDateTime         string                   `json:"endDateTime"`
			EndTimeType         string                   `json:"endTimeType"`
		} `json:"billingSetup"`
	}
	paymentsAccountInfo struct {
		PaymentsAccountID          string `json:"payments_account_id,omitempty"`
		PaymentsAccountName        string `json:"payments_account_name,omitempty"`
		PaymentsProfileID          string `json:"payments_profile_id,omitempty"`
		PaymentsProfileName        string `json:"payments_profile_name,omitempty"`
		SecondaryPaymentsProfileID string `json:"secondary_payments_profile_id,omitempty"`
	}
	billingSetupResult struct {
		ResourceName        string               `json:"resource_name"`
		ID                  string               `json:"id"`
		Status              string               `json:"status,omitempty"`
		PaymentsAccount     string               `json:"payments_account,omitempty"`
		PaymentsAccountInfo *paymentsAccountInfo `json:"payments_account_info,omitempty"`
		StartDateTime       string               `json:"start_date_time,omitempty"`
		EndDateTime         string               `json:"end_date_time,omitempty"`
		EndTimeType         string               `json:"end_time_type,omitempty"`
	}
	billingSetupsResult struct {
		BillingSetups []billingSetupResult `json:"billing_setups"`
		NextCursor    string               `json:"next_cursor,omitempty"`
		HasMore       bool                 `json:"has_more"`
	}
)

func billingSetupName(customerID, id string) string {
	return customerName(customerID) + "/billingSetups/" + id
}

// billingSetupOf checks a row against the queried account and maps it. Both resource names must belong to it.
func billingSetupOf(op string, raw json.RawMessage, customerID string) (billingSetupResult, string, error) {
	invalid := provider.InvalidResponse(op, subject+" returned a billing setup row Qatlas Connect cannot read")
	var row billingSetupRow
	if json.Unmarshal(raw, &row) != nil || row.BillingSetup == nil {
		return billingSetupResult{}, "", invalid
	}
	w := row.BillingSetup
	id, ok := idOf(w.ID)
	if !ok || (w.ResourceName != "" && w.ResourceName != billingSetupName(customerID, id)) ||
		(w.PaymentsAccount != "" && !ownName(w.PaymentsAccount, customerID, "paymentsAccounts", paymentsAccountSegment)) {
		return billingSetupResult{}, "", invalid
	}
	out := billingSetupResult{ResourceName: billingSetupName(customerID, id), ID: id, Status: code(w.Status),
		PaymentsAccount: w.PaymentsAccount, StartDateTime: code(w.StartDateTime), EndDateTime: code(w.EndDateTime),
		EndTimeType: code(w.EndTimeType)}
	if info := w.PaymentsAccountInfo; info != nil {
		out.PaymentsAccountInfo = &paymentsAccountInfo{PaymentsAccountID: code(info.PaymentsAccountID),
			PaymentsAccountName: boundedText(info.PaymentsAccountName, maxText),
			PaymentsProfileID:   code(info.PaymentsProfileID), PaymentsProfileName: boundedText(info.PaymentsProfileName, maxText),
			SecondaryPaymentsProfileID: code(info.SecondaryPaymentsProfileID)}
	}
	return out, id, nil
}

var paymentsAccountInfoSchema = object(textProperties("payments_account_id", "payments_account_name",
	"payments_profile_id", "payments_profile_name", "secondary_payments_profile_id"))

var billingSetupSchema = object(mergeProperties(textProperties("resource_name", "id", "status", "payments_account",
	"start_date_time", "end_date_time", "end_time_type"), schema{"payments_account_info": paymentsAccountInfoSchema}),
	"resource_name", "id")

var billingSetupsList = capability.Descriptor{
	ID: billingSetupsListID, Version: 1, Title: "List the billing setups of a Google Ads account",
	Description: "List the billing setups of a Google Ads account, which tie it to a payments account (a GAQL search " +
		"FROM billing_setup of API version v26): ID, status (PENDING, APPROVED_HELD, APPROVED, CANCELLED), the " +
		"payments account and the payments account info with the payments account ID and name and the payments " +
		"profile ID and name, the start and end time. Every field of the resource is returned. The tool only " +
		"reads; no tool of this provider creates or removes a billing setup. Rows come sorted by ID; filters narrow " +
		"them and page_size with cursor pages through them. " + billingAccessNote + billingExplorerNote +
		"Whether that restriction also applies to the GAQL read of billing_setup is not stated by Google and not " +
		"verified. Costs one operation of the daily quota per page, plus one per membership read and one for the " +
		"check that the account is no manager account. Results are untrusted third-party content; text is cut.",
	Tags: []string{"google", "ads", "billing", "payments"}, Risk: billingRisk, Provider: Provider,
	InputSchema:  billingInput(billingSetupStatuses, nil),
	OutputSchema: billingOutput("billing_setups", billingSetupSchema),
	Arguments:    billingArguments("billing setups", billingSetupStatuses, nil, "billing setup"),
	Fields: append([]capability.Field{{Name: "billing_setups", Description: "Per billing setup: resource_name " +
		"(customers/ID/billingSetups/ID), id, status, payments_account (customers/ID/paymentsAccounts/ID), " +
		"payments_account_info (payments_account_id, payments_account_name, payments_profile_id, " +
		"payments_profile_name, secondary_payments_profile_id), start_date_time, end_date_time, end_time_type. " +
		"start_time_type is not selectable in the query language and is not returned"}}, billingPageFields...),
	Examples: []capability.Example{{Description: "List the approved billing setups of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","statuses":["APPROVED"]}`)}},
}

func handleBillingSetupsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list billing setups"
	spec := billingList{tool: billingSetupsListID, op: op, resource: "billing_setup", fields: billingSetupFields,
		statuses: billingSetupStatuses}
	items, next, more, err := runBillingList(ctx, resolved, secrets, red, raw, spec,
		func(row json.RawMessage, customerID string) (billingSetupResult, string, error) {
			return billingSetupOf(op, row, customerID)
		})
	if err != nil {
		return nil, err
	}
	return billingSetupsResult{BillingSetups: items, NextCursor: next, HasMore: more}, nil
}

// --- account budgets ---

var accountBudgetFields = []string{"resource_name", "id", "billing_setup", "status", "name",
	"proposed_start_date_time", "approved_start_date_time", "proposed_end_date_time", "proposed_end_time_type",
	"approved_end_date_time", "approved_end_time_type", "proposed_spending_limit_micros",
	"proposed_spending_limit_type", "approved_spending_limit_micros", "approved_spending_limit_type",
	"adjusted_spending_limit_micros", "adjusted_spending_limit_type", "total_adjustments_micros",
	"amount_served_micros", "purchase_order_number", "notes", "pending_proposal.account_budget_proposal",
	"pending_proposal.proposal_type", "pending_proposal.name", "pending_proposal.start_date_time",
	"pending_proposal.end_date_time", "pending_proposal.end_time_type", "pending_proposal.spending_limit_micros",
	"pending_proposal.spending_limit_type", "pending_proposal.purchase_order_number", "pending_proposal.notes",
	"pending_proposal.creation_date_time"}

type (
	pendingProposalWire struct {
		AccountBudgetProposal string   `json:"accountBudgetProposal"`
		ProposalType          string   `json:"proposalType"`
		Name                  string   `json:"name"`
		StartDateTime         string   `json:"startDateTime"`
		EndDateTime           string   `json:"endDateTime"`
		EndTimeType           string   `json:"endTimeType"`
		SpendingLimitMicros   *wireInt `json:"spendingLimitMicros"`
		SpendingLimitType     string   `json:"spendingLimitType"`
		PurchaseOrderNumber   string   `json:"purchaseOrderNumber"`
		Notes                 string   `json:"notes"`
		CreationDateTime      string   `json:"creationDateTime"`
	}
	accountBudgetRow struct {
		AccountBudget *struct {
			ResourceName                string               `json:"resourceName"`
			ID                          *wireInt             `json:"id"`
			BillingSetup                string               `json:"billingSetup"`
			Status                      string               `json:"status"`
			Name                        string               `json:"name"`
			ProposedStartDateTime       string               `json:"proposedStartDateTime"`
			ApprovedStartDateTime       string               `json:"approvedStartDateTime"`
			ProposedEndDateTime         string               `json:"proposedEndDateTime"`
			ProposedEndTimeType         string               `json:"proposedEndTimeType"`
			ApprovedEndDateTime         string               `json:"approvedEndDateTime"`
			ApprovedEndTimeType         string               `json:"approvedEndTimeType"`
			ProposedSpendingLimitMicros *wireInt             `json:"proposedSpendingLimitMicros"`
			ProposedSpendingLimitType   string               `json:"proposedSpendingLimitType"`
			ApprovedSpendingLimitMicros *wireInt             `json:"approvedSpendingLimitMicros"`
			ApprovedSpendingLimitType   string               `json:"approvedSpendingLimitType"`
			AdjustedSpendingLimitMicros *wireInt             `json:"adjustedSpendingLimitMicros"`
			AdjustedSpendingLimitType   string               `json:"adjustedSpendingLimitType"`
			TotalAdjustmentsMicros      *wireInt             `json:"totalAdjustmentsMicros"`
			AmountServedMicros          *wireInt             `json:"amountServedMicros"`
			PurchaseOrderNumber         string               `json:"purchaseOrderNumber"`
			Notes                       string               `json:"notes"`
			PendingProposal             *pendingProposalWire `json:"pendingProposal"`
		} `json:"accountBudget"`
	}
	pendingProposalResult struct {
		AccountBudgetProposal string `json:"account_budget_proposal,omitempty"`
		ProposalType          string `json:"proposal_type,omitempty"`
		Name                  string `json:"name,omitempty"`
		StartDateTime         string `json:"start_date_time,omitempty"`
		EndDateTime           string `json:"end_date_time,omitempty"`
		EndTimeType           string `json:"end_time_type,omitempty"`
		SpendingLimitMicros   string `json:"spending_limit_micros,omitempty"`
		SpendingLimitType     string `json:"spending_limit_type,omitempty"`
		PurchaseOrderNumber   string `json:"purchase_order_number,omitempty"`
		Notes                 string `json:"notes,omitempty"`
		CreationDateTime      string `json:"creation_date_time,omitempty"`
	}
	accountBudgetResult struct {
		ResourceName                string                 `json:"resource_name"`
		ID                          string                 `json:"id"`
		BillingSetup                string                 `json:"billing_setup,omitempty"`
		Status                      string                 `json:"status,omitempty"`
		Name                        string                 `json:"name,omitempty"`
		ProposedStartDateTime       string                 `json:"proposed_start_date_time,omitempty"`
		ApprovedStartDateTime       string                 `json:"approved_start_date_time,omitempty"`
		ProposedEndDateTime         string                 `json:"proposed_end_date_time,omitempty"`
		ProposedEndTimeType         string                 `json:"proposed_end_time_type,omitempty"`
		ApprovedEndDateTime         string                 `json:"approved_end_date_time,omitempty"`
		ApprovedEndTimeType         string                 `json:"approved_end_time_type,omitempty"`
		ProposedSpendingLimitMicros string                 `json:"proposed_spending_limit_micros,omitempty"`
		ProposedSpendingLimitType   string                 `json:"proposed_spending_limit_type,omitempty"`
		ApprovedSpendingLimitMicros string                 `json:"approved_spending_limit_micros,omitempty"`
		ApprovedSpendingLimitType   string                 `json:"approved_spending_limit_type,omitempty"`
		AdjustedSpendingLimitMicros string                 `json:"adjusted_spending_limit_micros,omitempty"`
		AdjustedSpendingLimitType   string                 `json:"adjusted_spending_limit_type,omitempty"`
		TotalAdjustmentsMicros      string                 `json:"total_adjustments_micros,omitempty"`
		AmountServedMicros          string                 `json:"amount_served_micros,omitempty"`
		PurchaseOrderNumber         string                 `json:"purchase_order_number,omitempty"`
		Notes                       string                 `json:"notes,omitempty"`
		PendingProposal             *pendingProposalResult `json:"pending_proposal,omitempty"`
	}
	accountBudgetsResult struct {
		AccountBudgets []accountBudgetResult `json:"account_budgets"`
		NextCursor     string                `json:"next_cursor,omitempty"`
		HasMore        bool                  `json:"has_more"`
	}
)

func accountBudgetName(customerID, id string) string {
	return customerName(customerID) + "/accountBudgets/" + id
}

func accountBudgetOf(op string, raw json.RawMessage, customerID string) (accountBudgetResult, string, error) {
	invalid := provider.InvalidResponse(op, subject+" returned an account budget row Qatlas Connect cannot read")
	var row accountBudgetRow
	if json.Unmarshal(raw, &row) != nil || row.AccountBudget == nil {
		return accountBudgetResult{}, "", invalid
	}
	w := row.AccountBudget
	id, ok := idOf(w.ID)
	if !ok || (w.ResourceName != "" && w.ResourceName != accountBudgetName(customerID, id)) ||
		(w.BillingSetup != "" && !ownName(w.BillingSetup, customerID, "billingSetups", idPattern)) {
		return accountBudgetResult{}, "", invalid
	}
	out := accountBudgetResult{ResourceName: accountBudgetName(customerID, id), ID: id, BillingSetup: w.BillingSetup,
		Status: code(w.Status), Name: boundedText(w.Name, maxText),
		ProposedStartDateTime: code(w.ProposedStartDateTime), ApprovedStartDateTime: code(w.ApprovedStartDateTime),
		ProposedEndDateTime: code(w.ProposedEndDateTime), ProposedEndTimeType: code(w.ProposedEndTimeType),
		ApprovedEndDateTime: code(w.ApprovedEndDateTime), ApprovedEndTimeType: code(w.ApprovedEndTimeType),
		ProposedSpendingLimitMicros: intText(w.ProposedSpendingLimitMicros),
		ProposedSpendingLimitType:   code(w.ProposedSpendingLimitType),
		ApprovedSpendingLimitMicros: intText(w.ApprovedSpendingLimitMicros),
		ApprovedSpendingLimitType:   code(w.ApprovedSpendingLimitType),
		AdjustedSpendingLimitMicros: intText(w.AdjustedSpendingLimitMicros),
		AdjustedSpendingLimitType:   code(w.AdjustedSpendingLimitType),
		TotalAdjustmentsMicros:      intText(w.TotalAdjustmentsMicros), AmountServedMicros: intText(w.AmountServedMicros),
		PurchaseOrderNumber: boundedText(w.PurchaseOrderNumber, maxText), Notes: boundedText(w.Notes, maxLongText)}
	if p := w.PendingProposal; p != nil {
		if p.AccountBudgetProposal != "" && !ownName(p.AccountBudgetProposal, customerID, "accountBudgetProposals", idPattern) {
			return accountBudgetResult{}, "", invalid
		}
		out.PendingProposal = &pendingProposalResult{AccountBudgetProposal: p.AccountBudgetProposal,
			ProposalType: code(p.ProposalType), Name: boundedText(p.Name, maxText), StartDateTime: code(p.StartDateTime),
			EndDateTime: code(p.EndDateTime), EndTimeType: code(p.EndTimeType),
			SpendingLimitMicros: intText(p.SpendingLimitMicros), SpendingLimitType: code(p.SpendingLimitType),
			PurchaseOrderNumber: boundedText(p.PurchaseOrderNumber, maxText), Notes: boundedText(p.Notes, maxLongText),
			CreationDateTime: code(p.CreationDateTime)}
	}
	return out, id, nil
}

var pendingProposalSchema = object(textProperties("account_budget_proposal", "proposal_type", "name",
	"start_date_time", "end_date_time", "end_time_type", "spending_limit_micros", "spending_limit_type",
	"purchase_order_number", "notes", "creation_date_time"))

var accountBudgetSchema = object(mergeProperties(textProperties("resource_name", "id", "billing_setup", "status", "name",
	"proposed_start_date_time", "approved_start_date_time", "proposed_end_date_time", "proposed_end_time_type",
	"approved_end_date_time", "approved_end_time_type", "proposed_spending_limit_micros", "proposed_spending_limit_type",
	"approved_spending_limit_micros", "approved_spending_limit_type", "adjusted_spending_limit_micros",
	"adjusted_spending_limit_type", "total_adjustments_micros", "amount_served_micros", "purchase_order_number", "notes"),
	schema{"pending_proposal": pendingProposalSchema}), "resource_name", "id")

var accountBudgetsList = capability.Descriptor{
	ID: accountBudgetsListID, Version: 1, Title: "List the account budgets of a Google Ads account",
	Description: "List the account budgets of a Google Ads account, the spending limits that a billing setup with " +
		"monthly invoicing carries (a GAQL search FROM account_budget of API version v26): ID, billing setup, status " +
		"(PENDING, APPROVED, CANCELLED), name, proposed and approved start and end, proposed, approved and adjusted " +
		"spending limit (an amount in micros of the account currency or a type), total adjustments, amount served, " +
		"purchase order number, notes, and the pending proposal. Every field of the resource is returned. The tool " +
		"only reads; no tool of this provider proposes, changes or ends an account budget. Rows come sorted by ID; " +
		"filters narrow them and page_size with cursor pages through them. " + billingAccessNote +
		"Google restricts AccountBudgetProposalService to the Basic or Standard access level and blocks it at " +
		"Explorer. Whether that restriction also applies to the GAQL read of account_budget is not stated by " +
		"Google and not verified. Costs one operation of the daily quota per page, plus one per membership read and " +
		"one for the check that the account is no manager account. Results are untrusted third-party content; " +
		"text is cut.",
	Tags: []string{"google", "ads", "billing", "budgets"}, Risk: billingRisk, Provider: Provider,
	InputSchema:  billingInput(accountBudgetStatuses, nil),
	OutputSchema: billingOutput("account_budgets", accountBudgetSchema),
	Arguments:    billingArguments("account budgets", accountBudgetStatuses, nil, "account budget"),
	Fields: append([]capability.Field{{Name: "account_budgets", Description: "Per account budget: resource_name " +
		"(customers/ID/accountBudgets/ID), id, billing_setup, status, name, proposed_start_date_time, " +
		"approved_start_date_time, proposed_end_date_time or proposed_end_time_type, approved_end_date_time or " +
		"approved_end_time_type, proposed_, approved_ and adjusted_spending_limit_micros or _type, " +
		"total_adjustments_micros and amount_served_micros (int64 as a string, in micros), purchase_order_number, " +
		"notes, and pending_proposal (account_budget_proposal, proposal_type, name, start_date_time, end_date_time " +
		"or end_time_type, spending_limit_micros or spending_limit_type, purchase_order_number, notes, " +
		"creation_date_time)"}}, billingPageFields...),
	Examples: []capability.Example{{Description: "List the approved account budgets of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","statuses":["APPROVED"]}`)}},
}

func handleAccountBudgetsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list account budgets"
	spec := billingList{tool: accountBudgetsListID, op: op, resource: "account_budget", fields: accountBudgetFields,
		statuses: accountBudgetStatuses}
	items, next, more, err := runBillingList(ctx, resolved, secrets, red, raw, spec,
		func(row json.RawMessage, customerID string) (accountBudgetResult, string, error) {
			return accountBudgetOf(op, row, customerID)
		})
	if err != nil {
		return nil, err
	}
	return accountBudgetsResult{AccountBudgets: items, NextCursor: next, HasMore: more}, nil
}

// --- account budget proposals ---

var accountBudgetProposalFields = []string{"resource_name", "id", "billing_setup", "account_budget", "proposal_type",
	"status", "proposed_name", "proposed_start_date_time", "approved_start_date_time", "proposed_end_date_time",
	"proposed_end_time_type", "approved_end_date_time", "approved_end_time_type", "proposed_spending_limit_micros",
	"proposed_spending_limit_type", "approved_spending_limit_micros", "approved_spending_limit_type",
	"proposed_purchase_order_number", "proposed_notes", "creation_date_time", "approval_date_time"}

type (
	accountBudgetProposalRow struct {
		AccountBudgetProposal *struct {
			ResourceName                string   `json:"resourceName"`
			ID                          *wireInt `json:"id"`
			BillingSetup                string   `json:"billingSetup"`
			AccountBudget               string   `json:"accountBudget"`
			ProposalType                string   `json:"proposalType"`
			Status                      string   `json:"status"`
			ProposedName                string   `json:"proposedName"`
			ProposedStartDateTime       string   `json:"proposedStartDateTime"`
			ApprovedStartDateTime       string   `json:"approvedStartDateTime"`
			ProposedEndDateTime         string   `json:"proposedEndDateTime"`
			ProposedEndTimeType         string   `json:"proposedEndTimeType"`
			ApprovedEndDateTime         string   `json:"approvedEndDateTime"`
			ApprovedEndTimeType         string   `json:"approvedEndTimeType"`
			ProposedSpendingLimitMicros *wireInt `json:"proposedSpendingLimitMicros"`
			ProposedSpendingLimitType   string   `json:"proposedSpendingLimitType"`
			ApprovedSpendingLimitMicros *wireInt `json:"approvedSpendingLimitMicros"`
			ApprovedSpendingLimitType   string   `json:"approvedSpendingLimitType"`
			ProposedPurchaseOrderNumber string   `json:"proposedPurchaseOrderNumber"`
			ProposedNotes               string   `json:"proposedNotes"`
			CreationDateTime            string   `json:"creationDateTime"`
			ApprovalDateTime            string   `json:"approvalDateTime"`
		} `json:"accountBudgetProposal"`
	}
	accountBudgetProposalResult struct {
		ResourceName                string `json:"resource_name"`
		ID                          string `json:"id"`
		BillingSetup                string `json:"billing_setup,omitempty"`
		AccountBudget               string `json:"account_budget,omitempty"`
		ProposalType                string `json:"proposal_type,omitempty"`
		Status                      string `json:"status,omitempty"`
		ProposedName                string `json:"proposed_name,omitempty"`
		ProposedStartDateTime       string `json:"proposed_start_date_time,omitempty"`
		ApprovedStartDateTime       string `json:"approved_start_date_time,omitempty"`
		ProposedEndDateTime         string `json:"proposed_end_date_time,omitempty"`
		ProposedEndTimeType         string `json:"proposed_end_time_type,omitempty"`
		ApprovedEndDateTime         string `json:"approved_end_date_time,omitempty"`
		ApprovedEndTimeType         string `json:"approved_end_time_type,omitempty"`
		ProposedSpendingLimitMicros string `json:"proposed_spending_limit_micros,omitempty"`
		ProposedSpendingLimitType   string `json:"proposed_spending_limit_type,omitempty"`
		ApprovedSpendingLimitMicros string `json:"approved_spending_limit_micros,omitempty"`
		ApprovedSpendingLimitType   string `json:"approved_spending_limit_type,omitempty"`
		ProposedPurchaseOrderNumber string `json:"proposed_purchase_order_number,omitempty"`
		ProposedNotes               string `json:"proposed_notes,omitempty"`
		CreationDateTime            string `json:"creation_date_time,omitempty"`
		ApprovalDateTime            string `json:"approval_date_time,omitempty"`
	}
	accountBudgetProposalsResult struct {
		AccountBudgetProposals []accountBudgetProposalResult `json:"account_budget_proposals"`
		NextCursor             string                        `json:"next_cursor,omitempty"`
		HasMore                bool                          `json:"has_more"`
	}
)

func accountBudgetProposalName(customerID, id string) string {
	return customerName(customerID) + "/accountBudgetProposals/" + id
}

func accountBudgetProposalOf(op string, raw json.RawMessage, customerID string) (accountBudgetProposalResult, string, error) {
	invalid := provider.InvalidResponse(op, subject+" returned an account budget proposal row Qatlas Connect cannot read")
	var row accountBudgetProposalRow
	if json.Unmarshal(raw, &row) != nil || row.AccountBudgetProposal == nil {
		return accountBudgetProposalResult{}, "", invalid
	}
	w := row.AccountBudgetProposal
	id, ok := idOf(w.ID)
	if !ok || (w.ResourceName != "" && w.ResourceName != accountBudgetProposalName(customerID, id)) ||
		(w.BillingSetup != "" && !ownName(w.BillingSetup, customerID, "billingSetups", idPattern)) ||
		(w.AccountBudget != "" && !ownName(w.AccountBudget, customerID, "accountBudgets", idPattern)) {
		return accountBudgetProposalResult{}, "", invalid
	}
	return accountBudgetProposalResult{ResourceName: accountBudgetProposalName(customerID, id), ID: id,
		BillingSetup: w.BillingSetup, AccountBudget: w.AccountBudget, ProposalType: code(w.ProposalType),
		Status: code(w.Status), ProposedName: boundedText(w.ProposedName, maxText),
		ProposedStartDateTime: code(w.ProposedStartDateTime), ApprovedStartDateTime: code(w.ApprovedStartDateTime),
		ProposedEndDateTime: code(w.ProposedEndDateTime), ProposedEndTimeType: code(w.ProposedEndTimeType),
		ApprovedEndDateTime: code(w.ApprovedEndDateTime), ApprovedEndTimeType: code(w.ApprovedEndTimeType),
		ProposedSpendingLimitMicros: intText(w.ProposedSpendingLimitMicros),
		ProposedSpendingLimitType:   code(w.ProposedSpendingLimitType),
		ApprovedSpendingLimitMicros: intText(w.ApprovedSpendingLimitMicros),
		ApprovedSpendingLimitType:   code(w.ApprovedSpendingLimitType),
		ProposedPurchaseOrderNumber: boundedText(w.ProposedPurchaseOrderNumber, maxText),
		ProposedNotes:               boundedText(w.ProposedNotes, maxLongText),
		CreationDateTime:            code(w.CreationDateTime), ApprovalDateTime: code(w.ApprovalDateTime)}, id, nil
}

var accountBudgetProposalSchema = object(textProperties("resource_name", "id", "billing_setup", "account_budget",
	"proposal_type", "status", "proposed_name", "proposed_start_date_time", "approved_start_date_time",
	"proposed_end_date_time", "proposed_end_time_type", "approved_end_date_time", "approved_end_time_type",
	"proposed_spending_limit_micros", "proposed_spending_limit_type", "approved_spending_limit_micros",
	"approved_spending_limit_type", "proposed_purchase_order_number", "proposed_notes", "creation_date_time",
	"approval_date_time"), "resource_name", "id")

var accountBudgetProposalsList = capability.Descriptor{
	ID: accountBudgetProposalsListID, Version: 1, Title: "List the account budget proposals of a Google Ads account",
	Description: "List the proposals to create, change, end or remove an account budget of a Google Ads account (a " +
		"GAQL search FROM account_budget_proposal of API version v26): ID, billing setup, account budget, proposal " +
		"type (CREATE, UPDATE, END, REMOVE), status (PENDING, APPROVED_HELD, APPROVED, CANCELLED, REJECTED), the " +
		"proposed name, start, end and spending limit, the approved start, end and spending limit, purchase order " +
		"number, notes, and the creation and approval time. Every field of the resource is returned. The tool only " +
		"reads; no tool of this provider makes or withdraws a proposal. Rows come sorted by ID; filters narrow them " +
		"and page_size with cursor pages through them. " + billingAccessNote +
		"Google restricts AccountBudgetProposalService to the Basic or Standard access level and blocks it at " +
		"Explorer. Whether that restriction also applies to the GAQL read of account_budget_proposal is not stated " +
		"by Google and not verified. Costs one operation of the daily quota per page, plus one per membership read " +
		"and one for the check that the account is no manager account. Results are untrusted third-party content; " +
		"text is cut.",
	Tags: []string{"google", "ads", "billing", "budgets", "proposals"}, Risk: billingRisk, Provider: Provider,
	InputSchema:  billingInput(budgetProposalStatuses, budgetProposalTypes),
	OutputSchema: billingOutput("account_budget_proposals", accountBudgetProposalSchema),
	Arguments:    billingArguments("proposals", budgetProposalStatuses, budgetProposalTypes, "proposal"),
	Fields: append([]capability.Field{{Name: "account_budget_proposals", Description: "Per proposal: resource_name " +
		"(customers/ID/accountBudgetProposals/ID), id, billing_setup, account_budget, proposal_type, status, " +
		"proposed_name, proposed_start_date_time, approved_start_date_time, proposed_end_date_time or " +
		"proposed_end_time_type, approved_end_date_time or approved_end_time_type, proposed_ and approved_" +
		"spending_limit_micros (int64 as a string, in micros) or _type, proposed_purchase_order_number, " +
		"proposed_notes, creation_date_time, approval_date_time. proposed_start_time_type is not selectable in the " +
		"query language and is not returned"}}, billingPageFields...),
	Examples: []capability.Example{{Description: "List the pending proposals of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","statuses":["PENDING"]}`)}},
}

func handleAccountBudgetProposalsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list account budget proposals"
	spec := billingList{tool: accountBudgetProposalsListID, op: op, resource: "account_budget_proposal",
		fields: accountBudgetProposalFields, statuses: budgetProposalStatuses, types: budgetProposalTypes}
	items, next, more, err := runBillingList(ctx, resolved, secrets, red, raw, spec,
		func(row json.RawMessage, customerID string) (accountBudgetProposalResult, string, error) {
			return accountBudgetProposalOf(op, row, customerID)
		})
	if err != nil {
		return nil, err
	}
	return accountBudgetProposalsResult{AccountBudgetProposals: items, NextCursor: next, HasMore: more}, nil
}

// --- payments accounts ---

type (
	paymentsAccountWire struct {
		ResourceName               string `json:"resourceName"`
		PaymentsAccountID          string `json:"paymentsAccountId"`
		Name                       string `json:"name"`
		CurrencyCode               string `json:"currencyCode"`
		PaymentsProfileID          string `json:"paymentsProfileId"`
		SecondaryPaymentsProfileID string `json:"secondaryPaymentsProfileId"`
		PayingManagerCustomer      string `json:"payingManagerCustomer"`
	}
	paymentsAccountResult struct {
		ResourceName               string `json:"resource_name"`
		PaymentsAccountID          string `json:"payments_account_id,omitempty"`
		Name                       string `json:"name,omitempty"`
		CurrencyCode               string `json:"currency_code,omitempty"`
		PaymentsProfileID          string `json:"payments_profile_id,omitempty"`
		SecondaryPaymentsProfileID string `json:"secondary_payments_profile_id,omitempty"`
		PayingManagerCustomer      string `json:"paying_manager_customer,omitempty"`
	}
	paymentsAccountsResult struct {
		PaymentsAccounts []paymentsAccountResult `json:"payments_accounts"`
		Truncated        bool                    `json:"truncated"`
	}
)

// paymentsAccountOf checks one payments account against the queried account and maps it. The payments account
// ID is a part of the resource name, so both must agree. The paying manager is an identifier of another account
// and is only checked for its form.
func paymentsAccountOf(op string, w paymentsAccountWire, customerID string) (paymentsAccountResult, error) {
	invalid := provider.InvalidResponse(op, subject+" returned a payments account Qatlas Connect cannot read")
	rest, ok := strings.CutPrefix(w.ResourceName, customerName(customerID)+"/paymentsAccounts/")
	if !ok || !paymentsAccountSegment.MatchString(rest) || (w.PaymentsAccountID != "" && w.PaymentsAccountID != rest) ||
		(w.PayingManagerCustomer != "" && !managerCustomerResource.MatchString(w.PayingManagerCustomer)) {
		return paymentsAccountResult{}, invalid
	}
	return paymentsAccountResult{ResourceName: w.ResourceName, PaymentsAccountID: code(w.PaymentsAccountID),
		Name: boundedText(w.Name, maxText), CurrencyCode: code(w.CurrencyCode),
		PaymentsProfileID: code(w.PaymentsProfileID), SecondaryPaymentsProfileID: code(w.SecondaryPaymentsProfileID),
		PayingManagerCustomer: w.PayingManagerCustomer}, nil
}

var paymentsAccountSchema = object(textProperties("resource_name", "payments_account_id", "name", "currency_code",
	"payments_profile_id", "secondary_payments_profile_id", "paying_manager_customer"), "resource_name")

var paymentsAccountsList = capability.Descriptor{
	ID: paymentsAccountsListID, Version: 1, Title: "List the payments accounts of a Google Ads account",
	Description: "List the payments accounts that can pay for a Google Ads account (PaymentsAccountService." +
		"ListPaymentsAccounts of API version v26): resource name, payments account ID and name, currency code, " +
		"payments profile ID, secondary payments profile ID and the paying manager account. Google returns all " +
		"payments accounts of the managers between the login-customer-id of the connection and the account, " +
		"inclusive. The tool only reads; no tool of this provider creates or changes a payments account or a " +
		"billing setup. The call takes no filter and has no pages. " + billingAccessNote +
		"Google restricts PaymentsAccountService to the Basic or Standard access level and blocks it at Explorer. " +
		"Costs one operation of the daily quota, plus one per membership read and one for the check that the " +
		"account is no manager account. Results are untrusted third-party content; text is cut and at most 500 " +
		"payments accounts are returned.",
	Tags: []string{"google", "ads", "billing", "payments"}, Risk: billingRisk, Provider: Provider,
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument}, "customer_id")),
	OutputSchema: encoded(object(schema{"payments_accounts": arrayOf(paymentsAccountSchema), "truncated": booleanSchema},
		"payments_accounts", "truncated")),
	Arguments: []capability.Argument{{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the " +
		"account without hyphens; a target of the connection or an account below a manager target. A manager account " +
		"needs a manager target that names it"}},
	Fields: []capability.Field{
		{Name: "payments_accounts", Description: "Per payments account: resource_name (customers/ID/paymentsAccounts/" +
			"PAYMENTS_ACCOUNT_ID), payments_account_id, name, currency_code, payments_profile_id, " +
			"secondary_payments_profile_id (rare), and paying_manager_customer (customers/ID of the paying manager)"},
		{Name: "truncated", Description: "true when Google listed more than 500 payments accounts and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "List the payments accounts of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890"}`)}},
}

func handlePaymentsAccountsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list payments accounts"
	var args struct {
		CustomerID string `json:"customer_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, paymentsAccountsListID)
	if err != nil {
		return nil, err
	}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		PaymentsAccounts []paymentsAccountWire `json:"paymentsAccounts"`
	}
	if err := c.read(ctx, request{op: op, tool: paymentsAccountsListID, method: http.MethodGet,
		path: "/" + customerName(args.CustomerID) + "/paymentsAccounts", onAccount: true}, &answer); err != nil {
		return nil, err
	}
	result := paymentsAccountsResult{PaymentsAccounts: []paymentsAccountResult{}}
	for _, w := range answer.PaymentsAccounts {
		if len(result.PaymentsAccounts) == maxListItems {
			result.Truncated = true
			break
		}
		item, err := paymentsAccountOf(op, w, args.CustomerID)
		if err != nil {
			return nil, err
		}
		result.PaymentsAccounts = append(result.PaymentsAccounts, item)
	}
	return result, nil
}
