package googleads

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Synthetic values of the billing tests: short labels, no number formats of real payment or contact data.
const (
	payAccount = "pa-4001"
	payProfile = "pp-5001"
	payManager = "customers/1000000001"
)

var billingToolIDs = []string{billingSetupsListID, accountBudgetsListID, accountBudgetProposalsListID, paymentsAccountsListID}

func billingEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironment(t, targets, options)
}

func billingPath(id string) string { return "/v26/customers/" + id + "/paymentsAccounts" }

func ownRef(customer, collection, id string) string {
	return "customers/" + queryNumber(customer) + "/" + collection + "/" + id
}

func billingSetupItem(customer, id string) string {
	return fmt.Sprintf(`{"billingSetup":{"resourceName":"%s","id":"%s","status":"APPROVED","paymentsAccount":"%s",`+
		`"paymentsAccountInfo":{"paymentsAccountId":"%s","paymentsAccountName":"Payer %s","paymentsProfileId":"%s",`+
		`"paymentsProfileName":"Profile %s","secondaryPaymentsProfileId":"pp-5002"},`+
		`"startDateTime":"2026-01-02 03:04:05","endDateTime":"2026-12-31 23:59:59","endTimeType":"FOREVER"}}`,
		ownRef(customer, "billingSetups", id), id, ownRef(customer, "paymentsAccounts", payAccount), payAccount, id,
		payProfile, id)
}

func accountBudgetItem(customer, id string) string {
	return fmt.Sprintf(`{"accountBudget":{"resourceName":"%s","id":"%s","billingSetup":"%s","status":"APPROVED",`+
		`"name":"Budget %s","proposedStartDateTime":"2026-01-01 00:00:00","approvedStartDateTime":"2026-01-02 00:00:00",`+
		`"proposedEndTimeType":"FOREVER","approvedEndDateTime":"2026-12-31 00:00:00","proposedSpendingLimitMicros":"5000000",`+
		`"approvedSpendingLimitType":"INFINITE","adjustedSpendingLimitMicros":"0","totalAdjustmentsMicros":"250000",`+
		`"amountServedMicros":"125000","purchaseOrderNumber":"po-%s","notes":"note %s",`+
		`"pendingProposal":{"accountBudgetProposal":"%s","proposalType":"UPDATE","name":"Next %s",`+
		`"startDateTime":"2027-01-01 00:00:00","endTimeType":"FOREVER","spendingLimitMicros":"9000000",`+
		`"purchaseOrderNumber":"po-next","notes":"next note","creationDateTime":"2026-11-01 10:00:00"}}}`,
		ownRef(customer, "accountBudgets", id), id, ownRef(customer, "billingSetups", "11"), id, id, id,
		ownRef(customer, "accountBudgetProposals", "21"), id)
}

func proposalItem(customer, id string) string {
	return fmt.Sprintf(`{"accountBudgetProposal":{"resourceName":"%s","id":"%s","billingSetup":"%s","accountBudget":"%s",`+
		`"proposalType":"UPDATE","status":"PENDING","proposedName":"Plan %s","proposedStartDateTime":"2026-02-01 00:00:00",`+
		`"approvedStartDateTime":"2026-02-02 00:00:00","proposedEndTimeType":"FOREVER","approvedEndDateTime":"2026-12-01 00:00:00",`+
		`"proposedSpendingLimitMicros":"7000000","approvedSpendingLimitType":"INFINITE","proposedPurchaseOrderNumber":"po-p",`+
		`"proposedNotes":"proposal note","creationDateTime":"2026-01-15 08:00:00","approvalDateTime":"2026-01-16 09:00:00"}}`,
		ownRef(customer, "accountBudgetProposals", id), id, ownRef(customer, "billingSetups", "11"),
		ownRef(customer, "accountBudgets", "31"), id)
}

func paymentsAccountJSON(customer, id string) string {
	return fmt.Sprintf(`{"resourceName":"%s","paymentsAccountId":"%s","name":"Payer %s","currencyCode":"EUR",`+
		`"paymentsProfileId":"%s","secondaryPaymentsProfileId":"pp-5002","payingManagerCustomer":"%s"}`,
		ownRef(customer, "paymentsAccounts", id), id, id, payProfile, payManager)
}

func paymentsReply(items ...string) reply {
	return okReply(`{"paymentsAccounts":[` + strings.Join(items, ",") + `]}`)
}

// billingRoute answers the tool the way Google would, for the account clientID.
func billingRoute(e *environment, id string, r reply) {
	if id == paymentsAccountsListID {
		e.fake.routes["GET "+billingPath(clientID)] = r
		return
	}
	e.fake.routes["POST "+searchPath(clientID)] = r
}

func emptyBillingReply(id string) reply {
	if id == paymentsAccountsListID {
		return paymentsReply()
	}
	return rows()
}

func billingCall(id, customer string) string { return `{"customer_id":"` + customer + `"}` }

func TestBillingToolsAreReadOnlyAndDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range billingToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.RequiresToolAllowList || d.Risk.Effect != capability.EffectRead || d.Version != 1 ||
			d.Risk.Confirmation != capability.ConfirmationNone || !slices.Contains(metadata.Profiles[0].Tools, id) ||
			len(d.InputSchema) == 0 || len(d.OutputSchema) == 0 {
			t.Fatalf("%s = %+v", id, d)
		}
		for _, word := range []string{"Explorer", "Basic", "manager target", "only reads", "untrusted"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
		if id != paymentsAccountsListID && !strings.Contains(d.Description, "not verified") {
			t.Errorf("%s: the Explorer limit of the GAQL read is not marked as not verified", id)
		}
		if got := metadata.Token.Scopes[id]; len(got) != 1 || got[0] != scopeAdwords {
			t.Errorf("%s: scopes = %v", id, got)
		}
	}
}

// No tool of the provider creates, changes or removes billing, a budget proposal or a payments account, and no
// tool of this group takes an argument that would: the group is reading only, in name, effect and arguments.
func TestBillingHasNoWriteForm(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	families := []string{".billing-setups.", ".account-budgets.", ".account-budget-proposals.", ".payments-accounts.",
		".invoices.", ".incentives.", ".reservations."}
	for _, tool := range metadata.Tools {
		for _, family := range families {
			readName := strings.HasSuffix(tool.ID, ".list") || strings.HasSuffix(tool.ID, ".fetch")
			if strings.Contains(tool.ID, family) && (!readName || tool.Effect != config.EffectRead ||
				tool.RequiresToolAllowList) {
				t.Errorf("%s is not a plain read tool", tool.ID)
			}
		}
		for _, word := range []string{"billing-setup", "budget", "payments-account"} {
			if strings.Contains(tool.ID, word) && tool.Effect != config.EffectRead && !strings.Contains(tool.ID, "budget-network") &&
				!strings.Contains(tool.ID, ".campaign-budgets.") {
				t.Errorf("%s changes a billing resource", tool.ID)
			}
		}
	}
	forbidden := []string{"operation", "operations", "validate_only", "partial_failure", "response_content_type", "create",
		"update", "remove", "delete", "proposed_spending_limit_micros", "spending_limit",
		"payments_account", "payments_profile_id", "query", "fields", "login_customer_id", "resource_name", "update_mask"}
	for _, id := range billingToolIDs {
		d, _, _ := reg.Lookup(id)
		var in struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(d.InputSchema, &in); err != nil {
			t.Fatal(err)
		}
		for name := range in.Properties {
			if slices.Contains(forbidden, name) {
				t.Errorf("%s offers the argument %s", id, name)
			}
		}
		for _, argument := range d.Arguments {
			if slices.Contains(forbidden, argument.Name) {
				t.Errorf("%s documents the argument %s", id, argument.Name)
			}
		}
	}
	// Whatever the tools send is a search or the read of the payments accounts, never a mutation.
	for _, id := range billingToolIDs {
		e := billingEnvironment(t, nil, nil)
		billingRoute(e, id, emptyBillingReply(id))
		if _, err := e.invoke(id, billingCall(id, clientID)); err != nil {
			t.Fatal(err)
		}
		for _, s := range e.fake.api() {
			readSearch := s.method == http.MethodPost && strings.HasSuffix(s.path, ":search") && strings.HasPrefix(s.query, "SELECT ")
			readPayments := s.method == http.MethodGet && strings.HasSuffix(s.path, "/paymentsAccounts")
			if !readSearch && !readPayments || strings.Contains(s.path, "mutate") || strings.Contains(s.path, "BillingSetup") {
				t.Errorf("%s sent %+v", id, s)
			}
		}
		if len(e.posts()) != 0 {
			t.Errorf("%s mutated", id)
		}
	}
}

func TestBillingToolsAreOfferedThroughAToolsList(t *testing.T) {
	for _, id := range billingToolIDs {
		e := newEnvironmentWith(t, nil, nil, config.Connection{Tools: []string{id}})
		billingRoute(e, id, emptyBillingReply(id))
		if _, err := e.invoke(id, billingCall(id, clientID)); err != nil {
			t.Errorf("%s: a tools list should offer the read tool: %v", id, err)
		}
	}
}

func TestBillingTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range billingToolIDs {
		t.Run(id, func(t *testing.T) {
			e := billingEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invoke(id, billingCall(id, otherID))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
		t.Run(id+" malformed customer_id", func(t *testing.T) {
			e := billingEnvironment(t, nil, nil)
			for _, bad := range []string{"123-456-7890", "123456789", "", "1234567890 OR 1=1"} {
				if _, err := e.invoke(id, billingCall(id, bad)); err == nil {
					t.Errorf("customer_id %q accepted", bad)
				}
			}
			e.untouched(t)
		})
	}
}

func TestBillingListFiltersAreCheckedBeforeSecretAndIO(t *testing.T) {
	common := []string{`"ids":["0"]`, `"ids":["1 OR 1=1"]`, `"ids":["1","1"]`, `"page_size":0`, `"page_size":1001`,
		`"cursor":"not-a-cursor"`, `"statuses":["UNKNOWN"]`, `"statuses":["PENDING","PENDING"]`, `"query":"x"`,
		`"fields":["id"]`, `"validate_only":true`}
	bad := map[string][]string{
		billingSetupsListID:          append([]string{`"statuses":["REJECTED"]`, `"proposal_types":["CREATE"]`}, common...),
		accountBudgetsListID:         append([]string{`"statuses":["APPROVED_HELD"]`, `"proposal_types":["CREATE"]`}, common...),
		accountBudgetProposalsListID: append([]string{`"proposal_types":["APPROVE"]`, `"proposal_types":["CREATE","CREATE"]`}, common...),
	}
	for id, list := range bad {
		for _, filter := range list {
			e := billingEnvironment(t, nil, nil)
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`",`+filter+`}`); err == nil {
				t.Errorf("%s accepted %s", id, filter)
			}
			e.untouched(t)
		}
	}
	// The handlers refuse a proposal type for the other tools when the schema is bypassed.
	e := billingEnvironment(t, nil, nil)
	for _, handler := range []func() (any, error){
		func() (any, error) {
			return handleBillingSetupsList(context.Background(), e.resolved(), e.secrets(), e.red,
				json.RawMessage(`{"customer_id":"`+clientID+`","proposal_types":["CREATE"]}`))
		},
		func() (any, error) {
			return handleAccountBudgetsList(context.Background(), e.resolved(), e.secrets(), e.red,
				json.RawMessage(`{"customer_id":"`+clientID+`","statuses":["PENDING'"]}`))
		},
	} {
		if _, err := handler(); !isInvalidRequest(err) {
			t.Errorf("handler accepted a bypassed argument: %v", err)
		}
	}
	e.untouched(t)
}

func TestBillingSetupsListMapsEveryFieldAndPages(t *testing.T) {
	e := billingEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+searchPath(clientID)] = rows(billingSetupItem(clientID, "11"), billingSetupItem(clientID, "12"),
		billingSetupItem(clientID, "13"))
	out, err := e.invoke(billingSetupsListID, `{"customer_id":"`+clientID+`","page_size":2,"statuses":["APPROVED","PENDING"],`+
		`"ids":["11","12"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	list := got["billing_setups"].([]any)
	first := list[0].(map[string]any)
	info := first["payments_account_info"].(map[string]any)
	want := map[string]any{"resource_name": ownRef(clientID, "billingSetups", "11"), "id": "11", "status": "APPROVED",
		"payments_account": ownRef(clientID, "paymentsAccounts", payAccount), "start_date_time": "2026-01-02 03:04:05",
		"end_date_time": "2026-12-31 23:59:59", "end_time_type": "FOREVER"}
	for k, v := range want {
		if first[k] != v {
			t.Errorf("%s = %v, want %v", k, first[k], v)
		}
	}
	if len(list) != 2 || got["has_more"] != true || got["next_cursor"] == "" || info["payments_account_id"] != payAccount ||
		info["payments_account_name"] != "Payer 11" || info["payments_profile_id"] != payProfile ||
		info["payments_profile_name"] != "Profile 11" || info["secondary_payments_profile_id"] != "pp-5002" {
		t.Errorf("out = %s", out)
	}
	api := e.fake.api()
	wantQuery := "SELECT billing_setup.resource_name, billing_setup.id, billing_setup.status, billing_setup.payments_account, " +
		"billing_setup.payments_account_info.payments_account_id, billing_setup.payments_account_info.payments_account_name, " +
		"billing_setup.payments_account_info.payments_profile_id, billing_setup.payments_account_info.payments_profile_name, " +
		"billing_setup.payments_account_info.secondary_payments_profile_id, billing_setup.start_date_time, " +
		"billing_setup.end_date_time, billing_setup.end_time_type FROM billing_setup " +
		"WHERE billing_setup.status IN (APPROVED, PENDING) AND billing_setup.id IN (11, 12) ORDER BY billing_setup.id LIMIT 3"
	if len(api) != 1 || api[0].query != wantQuery || api[0].login != loginID || api[0].method != http.MethodPost {
		t.Errorf("request = %+v\nwant %s", api, wantQuery)
	}
	cursor := got["next_cursor"].(string)
	e.fake.routes["POST "+searchPath(clientID)] = rows(billingSetupItem(clientID, "13"))
	out, err = e.invoke(billingSetupsListID, `{"customer_id":"`+clientID+`","page_size":2,"statuses":["APPROVED","PENDING"],`+
		`"ids":["11","12"],"cursor":"`+cursor+`"}`)
	if err != nil || decode(t, out)["has_more"] != false || !strings.Contains(e.fake.api()[1].query, "billing_setup.id > 12 ORDER BY") {
		t.Fatalf("out = %s, err = %v", out, err)
	}
	if _, err := e.invoke(billingSetupsListID, `{"customer_id":"`+clientID+`","cursor":"`+cursor+`"}`); err == nil {
		t.Error("a cursor of other arguments was accepted")
	}
}

func TestAccountBudgetsListMapsEveryField(t *testing.T) {
	e := billingEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = rows(accountBudgetItem(clientID, "31"))
	out, err := e.invoke(accountBudgetsListID, `{"customer_id":"`+clientID+`","statuses":["APPROVED"]}`)
	if err != nil {
		t.Fatal(err)
	}
	item := decode(t, out)["account_budgets"].([]any)[0].(map[string]any)
	pending := item["pending_proposal"].(map[string]any)
	for k, v := range map[string]string{"resource_name": ownRef(clientID, "accountBudgets", "31"), "id": "31",
		"billing_setup": ownRef(clientID, "billingSetups", "11"), "status": "APPROVED", "name": "Budget 31",
		"proposed_start_date_time": "2026-01-01 00:00:00", "approved_start_date_time": "2026-01-02 00:00:00",
		"proposed_end_time_type": "FOREVER", "approved_end_date_time": "2026-12-31 00:00:00",
		"proposed_spending_limit_micros": "5000000", "approved_spending_limit_type": "INFINITE",
		"adjusted_spending_limit_micros": "0", "total_adjustments_micros": "250000", "amount_served_micros": "125000",
		"purchase_order_number": "po-31", "notes": "note 31"} {
		if item[k] != v {
			t.Errorf("%s = %v, want %s", k, item[k], v)
		}
	}
	for k, v := range map[string]string{"account_budget_proposal": ownRef(clientID, "accountBudgetProposals", "21"),
		"proposal_type": "UPDATE", "name": "Next 31", "start_date_time": "2027-01-01 00:00:00", "end_time_type": "FOREVER",
		"spending_limit_micros": "9000000", "purchase_order_number": "po-next", "notes": "next note",
		"creation_date_time": "2026-11-01 10:00:00"} {
		if pending[k] != v {
			t.Errorf("pending %s = %v, want %s", k, pending[k], v)
		}
	}
	query := e.fake.api()[0].query
	for _, field := range accountBudgetFields {
		if !strings.Contains(query, "account_budget."+field) {
			t.Errorf("query lacks %s", field)
		}
	}
	if !strings.HasSuffix(query, "FROM account_budget WHERE account_budget.status IN (APPROVED) ORDER BY account_budget.id LIMIT 101") {
		t.Errorf("query = %s", query)
	}
}

func TestAccountBudgetProposalsListMapsEveryField(t *testing.T) {
	e := billingEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = rows(proposalItem(clientID, "21"))
	out, err := e.invoke(accountBudgetProposalsListID, `{"customer_id":"`+clientID+`","statuses":["PENDING","REJECTED"],`+
		`"proposal_types":["UPDATE","END"],"ids":["21"]}`)
	if err != nil {
		t.Fatal(err)
	}
	item := decode(t, out)["account_budget_proposals"].([]any)[0].(map[string]any)
	for k, v := range map[string]string{"resource_name": ownRef(clientID, "accountBudgetProposals", "21"), "id": "21",
		"billing_setup": ownRef(clientID, "billingSetups", "11"), "account_budget": ownRef(clientID, "accountBudgets", "31"),
		"proposal_type": "UPDATE", "status": "PENDING", "proposed_name": "Plan 21",
		"proposed_start_date_time": "2026-02-01 00:00:00", "approved_start_date_time": "2026-02-02 00:00:00",
		"proposed_end_time_type": "FOREVER", "approved_end_date_time": "2026-12-01 00:00:00",
		"proposed_spending_limit_micros": "7000000", "approved_spending_limit_type": "INFINITE",
		"proposed_purchase_order_number": "po-p", "proposed_notes": "proposal note",
		"creation_date_time": "2026-01-15 08:00:00", "approval_date_time": "2026-01-16 09:00:00"} {
		if item[k] != v {
			t.Errorf("%s = %v, want %s", k, item[k], v)
		}
	}
	query := e.fake.api()[0].query
	for _, field := range accountBudgetProposalFields {
		if !strings.Contains(query, "account_budget_proposal."+field) {
			t.Errorf("query lacks %s", field)
		}
	}
	if !strings.Contains(query, "WHERE account_budget_proposal.status IN (PENDING, REJECTED) AND "+
		"account_budget_proposal.proposal_type IN (UPDATE, END) AND account_budget_proposal.id IN (21) ORDER BY") {
		t.Errorf("query = %s", query)
	}
}

func TestPaymentsAccountsListReadsWithGetAndMapsEveryField(t *testing.T) {
	e := billingEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["GET "+billingPath(clientID)] = paymentsReply(paymentsAccountJSON(clientID, payAccount))
	out, err := e.invoke(paymentsAccountsListID, billingCall(paymentsAccountsListID, clientID))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	item := got["payments_accounts"].([]any)[0].(map[string]any)
	for k, v := range map[string]string{"resource_name": ownRef(clientID, "paymentsAccounts", payAccount),
		"payments_account_id": payAccount, "name": "Payer " + payAccount, "currency_code": "EUR",
		"payments_profile_id": payProfile, "secondary_payments_profile_id": "pp-5002", "paying_manager_customer": payManager} {
		if item[k] != v {
			t.Errorf("%s = %v, want %s", k, item[k], v)
		}
	}
	api := e.fake.api()
	if got["truncated"] != false || len(api) != 1 || api[0].method != http.MethodGet || api[0].path != billingPath(clientID) ||
		api[0].login != loginID || api[0].body != "" || api[0].auth != "Bearer synthetic-access-1" {
		t.Errorf("out = %s, request = %+v", out, api)
	}
}

func TestPaymentsAccountsListCutsLongAnswers(t *testing.T) {
	e := billingEnvironment(t, nil, nil)
	items := make([]string, maxListItems+3)
	for i := range items {
		items[i] = paymentsAccountJSON(clientID, fmt.Sprintf("pa-%d", 100+i))
	}
	e.fake.routes["GET "+billingPath(clientID)] = paymentsReply(items...)
	out, err := e.invoke(paymentsAccountsListID, billingCall(paymentsAccountsListID, clientID))
	got := decode(t, out)
	if err != nil || got["truncated"] != true || len(got["payments_accounts"].([]any)) != maxListItems {
		t.Fatalf("err = %v, truncated = %v", err, got["truncated"])
	}
}

// A resource name of another account never passes, nor does a row that does not fit the one asked for. Text of
// Google is cut and cleaned.
func TestBillingAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	replace := func(item, old, new string) string { return strings.Replace(item, old, new, 1) }
	foreign := func(collection, id string) string { return ownRef(otherID, collection, id) }
	cases := map[string]map[string][]string{
		billingSetupsListID: {"rows": {
			billingSetupItem(otherID, "11"),
			replace(billingSetupItem(clientID, "11"), `"id":"11"`, `"id":"12"`),
			replace(billingSetupItem(clientID, "11"), ownRef(clientID, "paymentsAccounts", payAccount), foreign("paymentsAccounts", payAccount)),
			replace(billingSetupItem(clientID, "11"), ownRef(clientID, "paymentsAccounts", payAccount), ownRef(clientID, "paymentsAccounts", "../x")),
			`{"billingSetup":{"id":"-4"}}`, `{"billingSetup":{"resourceName":"x"}}`, `{}`}},
		accountBudgetsListID: {"rows": {
			accountBudgetItem(otherID, "31"),
			replace(accountBudgetItem(clientID, "31"), ownRef(clientID, "billingSetups", "11"), foreign("billingSetups", "11")),
			replace(accountBudgetItem(clientID, "31"), ownRef(clientID, "accountBudgetProposals", "21"), foreign("accountBudgetProposals", "21")),
			replace(accountBudgetItem(clientID, "31"), ownRef(clientID, "accountBudgetProposals", "21"), ownRef(clientID, "accountBudgetProposals", "x")),
			`{}`}},
		accountBudgetProposalsListID: {"rows": {
			proposalItem(otherID, "21"),
			replace(proposalItem(clientID, "21"), ownRef(clientID, "billingSetups", "11"), foreign("billingSetups", "11")),
			replace(proposalItem(clientID, "21"), ownRef(clientID, "accountBudgets", "31"), foreign("accountBudgets", "31")),
			replace(proposalItem(clientID, "21"), `"id":"21"`, `"id":"22"`), `{}`}},
	}
	for id, byKind := range cases {
		for i, row := range byKind["rows"] {
			e := billingEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = rows(row)
			if _, err := e.invoke(id, billingCall(id, clientID)); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("%s row %d: err = %v, want invalid-provider-response", id, i, err)
			}
		}
	}
	for name, item := range map[string]string{
		"other account": paymentsAccountJSON(otherID, payAccount),
		"id differs": strings.Replace(paymentsAccountJSON(clientID, payAccount), `"paymentsAccountId":"`+payAccount+`"`,
			`"paymentsAccountId":"pa-other"`, 1),
		"name path": strings.Replace(paymentsAccountJSON(clientID, payAccount), "/paymentsAccounts/"+payAccount,
			"/paymentsAccounts/"+payAccount+"/../x", 1),
		"no name": `{"paymentsAccountId":"` + payAccount + `"}`,
		"manager": strings.Replace(paymentsAccountJSON(clientID, payAccount), payManager, "customers/../x", 1),
	} {
		e := billingEnvironment(t, nil, nil)
		e.fake.routes["GET "+billingPath(clientID)] = paymentsReply(item)
		if _, err := e.invoke(paymentsAccountsListID, billingCall(paymentsAccountsListID, clientID)); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("payments account %s: err = %v", name, err)
		}
	}
	t.Run("rows out of the requested order", func(t *testing.T) {
		e := billingEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = rows(billingSetupItem(clientID, "12"), billingSetupItem(clientID, "11"))
		if _, err := e.invoke(billingSetupsListID, billingCall(billingSetupsListID, clientID)); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("text is cleaned and cut", func(t *testing.T) {
		e := billingEnvironment(t, nil, nil)
		long := strings.Repeat("n", 5000)
		item := strings.Replace(accountBudgetItem(clientID, "31"), `"notes":"note 31"`, `"notes":"a\u0007b `+long+`"`, 1)
		e.fake.routes["POST "+searchPath(clientID)] = rows(item)
		out, err := e.invoke(accountBudgetsListID, billingCall(accountBudgetsListID, clientID))
		if err != nil {
			t.Fatal(err)
		}
		notes := decode(t, out)["account_budgets"].([]any)[0].(map[string]any)["notes"].(string)
		if strings.ContainsRune(notes, '\a') || len(notes) > maxLongText || !strings.HasSuffix(notes, "…") {
			t.Errorf("notes = %q", notes[:20])
		}
	})
}

func TestBillingToolsNeedAManagerTargetOnManagerAccounts(t *testing.T) {
	managerAnswer := func(manager bool) func(s seen) *reply {
		return func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				return &reply{status: 200, body: fmt.Sprintf(`{"results":[{"customer":{"id":"%s","manager":%t}}]}`, managerID, manager)}
			}
			return nil
		}
	}
	for _, id := range billingToolIDs {
		t.Run(id+" a customer target that names a manager is refused", func(t *testing.T) {
			e := billingEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = managerAnswer(true)
			_, err := e.invoke(id, billingCall(id, managerID))
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") {
				t.Fatalf("err = %v", err)
			}
			for _, s := range e.fake.api() {
				if !strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					t.Errorf("the tool read the manager's billing: %+v", s)
				}
			}
		})
		t.Run(id+" a manager target that names it passes without the check", func(t *testing.T) {
			e := billingEnvironment(t, []string{"managers/" + managerID}, nil)
			if id == paymentsAccountsListID {
				e.fake.routes["GET "+billingPath(managerID)] = paymentsReply()
			} else {
				e.fake.routes["POST "+searchPath(managerID)] = rows()
			}
			if _, err := e.invoke(id, billingCall(id, managerID)); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 {
				t.Errorf("requests = %+v, want the call alone", got)
			}
		})
		t.Run(id+" a client account passes after one check", func(t *testing.T) {
			e := billingEnvironment(t, []string{"customers/" + clientID}, nil)
			e.fake.answer = func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + clientID + `"}}]}`}
				}
				return nil
			}
			billingRoute(e, id, emptyBillingReply(id))
			if _, err := e.invoke(id, billingCall(id, clientID)); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 2 || !strings.Contains(got[0].query, "customer.manager") {
				t.Errorf("requests = %+v", got)
			}
		})
		t.Run(id+" a connection without targets makes no check", func(t *testing.T) {
			e := billingEnvironment(t, nil, nil)
			if id == paymentsAccountsListID {
				e.fake.routes["GET "+billingPath(managerID)] = paymentsReply()
			} else {
				e.fake.routes["POST "+searchPath(managerID)] = rows()
			}
			if _, err := e.invoke(id, billingCall(id, managerID)); err != nil || len(e.fake.api()) != 1 {
				t.Errorf("err = %v, requests = %+v", err, e.fake.api())
			}
		})
	}
}

func TestBillingErrorsAreMapped(t *testing.T) {
	for _, id := range billingToolIDs {
		for name, c := range map[string]struct {
			reply reply
			class provider.Class
		}{
			"permission": {reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
				"USER_PERMISSION_DENIED", "")}, provider.ClassPermission},
			"quota": {reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
				"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"5s"}`)}, provider.ClassRateLimited},
			"not json": {okReply(`{"results":`), provider.ClassInvalidResponse},
		} {
			e := billingEnvironment(t, nil, nil)
			billingRoute(e, id, c.reply)
			_, err := e.invoke(id, billingCall(id, clientID))
			if classOf(err) != c.class || err == nil || strings.Contains(err.Error(), detailCanary) ||
				strings.Contains(err.Error(), triggerCanary) {
				t.Errorf("%s %s: err = %v, want %s", id, name, err, c.class)
			}
			if len(e.fake.api()) != 1 {
				t.Errorf("%s %s: requests = %+v, want one and no repeat", id, name, e.fake.api())
			}
		}
	}
}
