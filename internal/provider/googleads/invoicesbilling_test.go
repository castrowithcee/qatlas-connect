package googleads

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	invBillingSetup = "4455667788"
	invPDFMarker    = "synthetic-pdf-address-marker-7c3d"
	invLinkMarker   = "synthetic-share-marker-4f81"
)

func invPath(customer string) string { return "/v26/customers/" + customer + "/invoices" }
func invAppLinksName(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/thirdPartyAppAnalyticsLinks/" + id
}

// invInvoice is an invoice of the account with the PDF address Google would add.
func invInvoice(customer, id string) string {
	return fmt.Sprintf(`{"resourceName":"customers/%[1]s/invoices/%[2]s","id":"%[2]s","type":"INVOICE",`+
		`"billingSetup":"customers/%[1]s/billingSetups/%[3]s","paymentsAccountId":"1111-2222-3333",`+
		`"paymentsProfileId":"4444-5555-6666","issueDate":"2026-04-01","dueDate":"2026-05-01",`+
		`"serviceDateRange":{"startDate":"2026-03-01","endDate":"2026-03-31"},"currencyCode":"EUR",`+
		`"subtotalAmountMicros":"1000000","taxAmountMicros":"190000","totalAmountMicros":"1190000",`+
		`"adjustmentsTotalAmountMicros":"-5000","regulatoryCostsTotalAmountMicros":"7000","exportChargeTotalAmountMicros":"0",`+
		`"correctedInvoice":"customers/%[1]s/invoices/corr-1","replacedInvoices":["customers/%[1]s/invoices/old-1"],`+
		`"pdfUrl":"https://invoices.example.invalid/%[4]s",`+
		`"accountBudgetSummaries":[{"customer":"customers/%[1]s","customerDescriptiveName":"Summary\u0007 name",`+
		`"accountBudget":"customers/%[1]s/accountBudgets/12","accountBudgetName":"Budget","purchaseOrderNumber":"PO-1",`+
		`"totalAmountMicros":"1190000","billableActivityDateRange":{"startDate":"2026-03-01","endDate":"2026-03-31"},`+
		`"invalidActivitySummaries":[{"originalMonthOfService":"FEBRUARY","originalYearOfService":"2026","amountMicros":"3"}],`+
		`"campaignSummaries":[{"campaignDescription":"Camp","quantity":"5","unitOfMeasure":"CLICKS","amountMicros":"9"}]}],`+
		`"accountSummaries":[{"customer":"customers/%[1]s","totalAmountMicros":"1190000",`+
		`"regulatoryCostSummaries":[{"regulatoryFeeType":"UNKNOWN","amountMicros":"7000"}],`+
		`"adjustmentSummaries":[{"adjustmentDescription":"Credit","amountMicros":"-5000"}]}]}`,
		customer, id, invBillingSetup, invPDFMarker)
}

const invArgs = `{"customer_id":"%s","billing_setup_id":"` + invBillingSetup + `","issue_year":2026,"issue_month":"MARCH"}`

func newInvoicesEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironment(t, targets, options)
}

var invoicesToolArguments = map[string]string{
	invoicesListID:                    invArgs,
	incentivesFetchID:                 `{"country_code":"DE"}`,
	thirdPartyAppAnalyticsLinksListID: `{"customer_id":"%s"}`,
}

func TestInvoicesToolsAreRegisteredAsReadOnlyAndWithholdAccessValues(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id := range invoicesToolArguments {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != capability.EffectRead || !d.Risk.OpenWorld || d.RequiresToolAllowList ||
			d.Risk.Confirmation != capability.ConfirmationNone || len(d.OutputSchema) == 0 {
			t.Fatalf("%s = %+v", id, d)
		}
		if !strings.Contains(strings.Join(metadata.Profiles[0].Tools, " "), id) {
			t.Errorf("%s is not in the read profile", id)
		}
		// The input schema and the names of the arguments hold no way to reach an access value or to steer a query.
		inputs := string(d.InputSchema)
		for _, a := range d.Arguments {
			inputs += a.Name
		}
		for _, forbidden := range []string{"shareable", "pdf", "login_customer_id", "billing_setup\"", "fields", "conditions",
			"query", "path", "validate_only"} {
			if strings.Contains(inputs, forbidden) {
				t.Errorf("%s: input names %q", id, forbidden)
			}
		}
		outputs := string(d.OutputSchema)
		for _, f := range d.Fields {
			outputs += f.Name
		}
		for _, forbidden := range []string{"shareable", "pdf_url", "pdfUrl"} {
			if strings.Contains(outputs, forbidden) {
				t.Errorf("%s: output names %q", id, forbidden)
			}
		}
	}
	// No tool applies an incentive or regenerates a link ID. recommendations.apply is another family.
	for _, tool := range metadata.Tools {
		for _, word := range []string{"regenerate", "shareable"} {
			if strings.Contains(tool.ID, word) {
				t.Errorf("tool %s must not exist", tool.ID)
			}
		}
		if strings.Contains(tool.ID, "apply") && (strings.Contains(tool.ID, "incentive") || tool.ID != recommendationsApplyID) {
			t.Errorf("tool %s must not exist", tool.ID)
		}
	}
	d, _, _ := reg.Lookup(invoicesListID)
	for _, word := range []string{"Explorer", "Basic", "2019", "monthly invoicing"} {
		if !strings.Contains(d.Description, word) {
			t.Errorf("invoices.list description lacks %q", word)
		}
	}
}

func TestInvoicesToolsCheckTargetsAndArgumentsBeforeSecretAndIO(t *testing.T) {
	for _, id := range []string{invoicesListID, thirdPartyAppAnalyticsLinksListID} {
		t.Run(id+" outside the targets", func(t *testing.T) {
			e := newInvoicesEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invoke(id, fmt.Sprintf(invoicesToolArguments[id], otherID))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
		t.Run(id+" malformed customer_id", func(t *testing.T) {
			e := newInvoicesEnvironment(t, nil, nil)
			for _, bad := range []string{"123-456-7890", "123456789", "", "1234567890 OR 1=1"} {
				if _, err := e.invoke(id, fmt.Sprintf(invoicesToolArguments[id], bad)); err == nil {
					t.Errorf("customer_id %q accepted", bad)
				}
			}
			e.untouched(t)
		})
	}
	e := newInvoicesEnvironment(t, nil, nil)
	for name, bad := range map[string]string{
		"billing setup as name":  `"billing_setup_id":"customers/` + clientID + `/billingSetups/1","issue_year":2026,"issue_month":"MARCH"`,
		"billing setup zero":     `"billing_setup_id":"0","issue_year":2026,"issue_month":"MARCH"`,
		"billing setup injected": `"billing_setup_id":"1&issueYear=2020","issue_year":2026,"issue_month":"MARCH"`,
		"year too old":           `"billing_setup_id":"1","issue_year":2018,"issue_month":"MARCH"`,
		"year five digits":       `"billing_setup_id":"1","issue_year":20260,"issue_month":"MARCH"`,
		"year as text":           `"billing_setup_id":"1","issue_year":"2026","issue_month":"MARCH"`,
		"month unknown":          `"billing_setup_id":"1","issue_year":2026,"issue_month":"UNKNOWN"`,
		"month number":           `"billing_setup_id":"1","issue_year":2026,"issue_month":"3"`,
		"month missing":          `"billing_setup_id":"1","issue_year":2026`,
		"year missing":           `"billing_setup_id":"1","issue_month":"MARCH"`,
		"setup missing":          `"issue_year":2026,"issue_month":"MARCH"`,
		"free resource name":     `"billing_setup":"customers/` + clientID + `/billingSetups/1","billing_setup_id":"1","issue_year":2026,"issue_month":"MARCH"`,
		"login header":           `"login_customer_id":"` + loginID + `","billing_setup_id":"1","issue_year":2026,"issue_month":"MARCH"`,
	} {
		if _, err := e.invoke(invoicesListID, `{"customer_id":"`+clientID+`",`+bad+`}`); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for name, bad := range map[string]string{
		`{"language_code":"english-US-x"}`: "language", `{"language_code":"en US"}`: "language",
		`{"country_code":"DEU"}`: "country", `{"country_code":"D1"}`: "country",
		`{"email":"a b@c"}`: "email", `{"email":"a'b@c"}`: "email",
		`{"incentive_type":"UNKNOWN"}`: "type", `{"incentive_type":"CYO"}`: "type",
		`{"customer_id":"` + clientID + `"}`: "customer_id", `{"selected_incentive_id":"1"}`: "apply",
		`{"login_customer_id":"` + loginID + `"}`: "login",
	} {
		if _, err := e.invoke(incentivesFetchID, name); err == nil {
			t.Errorf("incentives.fetch accepted %s (%s)", name, bad)
		}
	}
	for _, extra := range []string{`"shareable_link_id":"x"`, `"fields":["third_party_app_analytics_link.shareable_link_id"]`,
		`"conditions":[]`, `"regenerate":true`, `"resource_name":"customers/` + clientID + `/thirdPartyAppAnalyticsLinks/1"`} {
		if _, err := e.invoke(thirdPartyAppAnalyticsLinksListID, `{"customer_id":"`+clientID+`",`+extra+`}`); err == nil {
			t.Errorf("app analytics list accepted %s", extra)
		}
	}
	e.untouched(t)
}

func TestInvoicesListBuildsTheRequestFromCheckedValuesAndMapsEveryField(t *testing.T) {
	e := newInvoicesEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["GET "+invPath(clientID)] = okReply(`{"invoices":[` + invInvoice(clientID, "inv-9") + `]}`)
	out, err := e.invoke(invoicesListID, fmt.Sprintf(invArgs, clientID)[:len(fmt.Sprintf(invArgs, clientID))-1]+
		`,"include_granular_level_invoice_details":true}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].method != "GET" || api[0].login != loginID || api[0].auth != "Bearer synthetic-access-1" ||
		api[0].developerToken != "" || api[0].body != "" {
		t.Fatalf("requests = %+v", api)
	}
	u, _ := url.Parse(api[0].url)
	if u.Path != invPath(clientID) || u.Query().Get("billingSetup") != "customers/"+clientID+"/billingSetups/"+invBillingSetup ||
		u.Query().Get("issueYear") != "2026" || u.Query().Get("issueMonth") != "MARCH" ||
		u.Query().Get("includeGranularLevelInvoiceDetails") != "true" || len(u.Query()) != 4 {
		t.Errorf("url = %s", api[0].url)
	}
	if strings.Contains(out, invPDFMarker) || strings.Contains(out, "pdf_url") || strings.Contains(out, "https://") {
		t.Errorf("the PDF address is in the result: %s", out)
	}
	got := decode(t, out)
	list, _ := got["invoices"].([]any)
	if len(list) != 1 || got["truncated"] != false {
		t.Fatalf("out = %s", out)
	}
	inv := list[0].(map[string]any)
	if inv["resource_name"] != "customers/"+clientID+"/invoices/inv-9" || inv["has_pdf"] != true ||
		inv["total_amount_micros"] != "1190000" || inv["adjustments_total_amount_micros"] != "-5000" ||
		inv["corrected_invoice"] != "customers/"+clientID+"/invoices/corr-1" || inv["currency_code"] != "EUR" ||
		inv["payments_profile_id"] != "4444-5555-6666" || inv["type"] != "INVOICE" {
		t.Errorf("invoice = %v", inv)
	}
	budgets := inv["account_budget_summaries"].([]any)
	budget := budgets[0].(map[string]any)
	if budget["customer_descriptive_name"] != "Summary  name" || budget["purchase_order_number"] != "PO-1" ||
		len(budget["invalid_activity_summaries"].([]any)) != 1 || len(budget["campaign_summaries"].([]any)) != 1 {
		t.Errorf("budget summary = %v", budget)
	}
	summary := inv["account_summaries"].([]any)[0].(map[string]any)
	if len(summary["regulatory_cost_summaries"].([]any)) != 1 || len(summary["adjustment_summaries"].([]any)) != 1 {
		t.Errorf("account summary = %v", summary)
	}
	// Without the optional flag the parameter is not sent.
	e = newInvoicesEnvironment(t, nil, nil)
	e.fake.routes["GET "+invPath(clientID)] = okReply(`{}`)
	out, err = e.invoke(invoicesListID, fmt.Sprintf(invArgs, clientID))
	if err != nil || strings.Contains(out, "resource_name") || strings.Contains(e.fake.api()[0].url, "includeGranular") {
		t.Errorf("out = %s, err = %v, url = %s", out, err, e.fake.api()[0].url)
	}
}

func TestInvoicesListRefusesForeignInvoices(t *testing.T) {
	mine := invInvoice(clientID, "inv-1")
	for name, body := range map[string]string{
		"another account":  strings.ReplaceAll(mine, "customers/"+clientID+"/invoices/inv-1", "customers/"+otherID+"/invoices/inv-1"),
		"another setup":    strings.ReplaceAll(mine, "billingSetups/"+invBillingSetup, "billingSetups/1"),
		"odd invoice id":   strings.ReplaceAll(mine, "invoices/inv-1\"", "invoices/inv-1/../x\""),
		"foreign summary":  strings.ReplaceAll(mine, `"customer":"customers/`+clientID+`","customerDescriptiveName"`, `"customer":"http://other.invalid","customerDescriptiveName"`),
		"odd budget name":  strings.ReplaceAll(mine, "accountBudgets/12", "accountBudgets/x"),
		"odd replaced ref": strings.ReplaceAll(mine, "invoices/old-1", "x/old-1"),
	} {
		t.Run(name, func(t *testing.T) {
			e := newInvoicesEnvironment(t, nil, nil)
			e.fake.routes["GET "+invPath(clientID)] = okReply(`{"invoices":[` + body + `]}`)
			out, err := e.invoke(invoicesListID, fmt.Sprintf(invArgs, clientID))
			if classOf(err) != provider.ClassInvalidResponse || out != "" {
				t.Errorf("out = %s, err = %v", out, err)
			}
		})
	}
}

func TestInvoicesListBoundsLongLists(t *testing.T) {
	items := make([]string, maxListItems+3)
	for i := range items {
		items[i] = `"customers/` + clientID + `/invoices/r` + fmt.Sprint(i) + `"`
	}
	inv := strings.Replace(invInvoice(clientID, "inv-1"), `"replacedInvoices":["customers/`+clientID+`/invoices/old-1"]`,
		`"replacedInvoices":[`+strings.Join(items, ",")+`]`, 1)
	e := newInvoicesEnvironment(t, nil, nil)
	e.fake.routes["GET "+invPath(clientID)] = okReply(`{"invoices":[` + inv + `]}`)
	out, err := e.invoke(invoicesListID, fmt.Sprintf(invArgs, clientID))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	replaced := got["invoices"].([]any)[0].(map[string]any)["replaced_invoices"].([]any)
	if got["truncated"] != true || len(replaced) != maxListItems {
		t.Errorf("truncated = %v, replaced = %d", got["truncated"], len(replaced))
	}
}

func TestIncentivesFetchSendsOnlyTheNamedParametersAndMapsTheOffer(t *testing.T) {
	e := newInvoicesEnvironment(t, []string{"customers/" + clientID}, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["GET /v26/incentives:fetchIncentive"] = okReply(`{"incentiveOffer":{` +
		`"consolidatedTermsAndConditionsUrl":"https://terms.example.invalid/all","offerType":"CYO_INCENTIVE",` +
		`"cyoIncentives":{"lowOffer":{"incentiveId":"11","requirement":{"spend":{"awardAmount":{"currencyCode":"EUR",` +
		`"units":"50"},"requiredAmount":{"currencyCode":"EUR","units":"500","nanos":250000000}}},` +
		`"incentiveTermsAndConditionsUrl":"https://terms.example.invalid/low","incentiveType":"ACQUISITION"},` +
		`"mediumOffer":{"incentiveId":"12","incentiveTermsAndConditionsUrl":"http://terms.example.invalid/medium"},` +
		`"highOffer":{"incentiveId":"13","incentiveTermsAndConditionsUrl":"https://visitor` + string(rune(64)) + `terms.example.invalid/high"}}}}`)
	out, err := e.invoke(incentivesFetchID, `{"language_code":"de-DE","country_code":"DE","incentive_type":"ACQUISITION"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].login != "" || api[0].method != "GET" {
		t.Fatalf("requests = %+v, want one GET without login-customer-id", api)
	}
	u, _ := url.Parse(api[0].url)
	if u.Path != "/v26/incentives:fetchIncentive" || u.Query().Get("languageCode") != "de-DE" ||
		u.Query().Get("countryCode") != "DE" || u.Query().Get("incentiveType") != "ACQUISITION" || len(u.Query()) != 3 {
		t.Errorf("url = %s", api[0].url)
	}
	offer := decode(t, out)["incentive_offer"].(map[string]any)
	cyo := offer["cyo_incentives"].(map[string]any)
	low := cyo["low_offer"].(map[string]any)
	spend := low["spend"].(map[string]any)
	if offer["offer_type"] != "CYO_INCENTIVE" || offer["consolidated_terms_and_conditions_url"] != "https://terms.example.invalid/all" ||
		low["incentive_id"] != "11" || spend["award_amount"].(map[string]any)["units"] != "50" ||
		spend["required_amount"].(map[string]any)["nanos"] != float64(250000000) {
		t.Errorf("offer = %s", out)
	}
	if _, found := cyo["medium_offer"].(map[string]any)["incentive_terms_and_conditions_url"]; found {
		t.Errorf("an http address was returned: %s", out)
	}
	if _, found := cyo["high_offer"].(map[string]any)["incentive_terms_and_conditions_url"]; found || strings.Contains(out, "visitor") {
		t.Errorf("an address with user information was returned: %s", out)
	}
	// No parameter, no query string; an offer without incentive.
	e = newInvoicesEnvironment(t, nil, nil)
	e.fake.routes["GET /v26/incentives:fetchIncentive"] = okReply(`{"incentiveOffer":{"offerType":"NO_INCENTIVE"}}`)
	out, err = e.invoke(incentivesFetchID, `{}`)
	if err != nil || e.fake.api()[0].url != "https://googleads.googleapis.com/v26/incentives:fetchIncentive" ||
		decode(t, out)["incentive_offer"].(map[string]any)["offer_type"] != "NO_INCENTIVE" {
		t.Errorf("out = %s, err = %v, requests = %+v", out, err, e.fake.api())
	}
	// An answer without the offer is no answer.
	e = newInvoicesEnvironment(t, nil, nil)
	e.fake.routes["GET /v26/incentives:fetchIncentive"] = okReply(`{}`)
	if _, err := e.invoke(incentivesFetchID, `{}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestAppAnalyticsLinksNeverSelectFilterOrReturnTheShareableLinkID(t *testing.T) {
	e := newInvoicesEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	// Google is made to answer the value although it was not asked for.
	e.fake.routes["POST "+searchPath(clientID)] = rows(
		`{"thirdPartyAppAnalyticsLink":{"resourceName":"`+invAppLinksName(clientID, "20")+`","shareableLinkId":"`+invLinkMarker+`"}}`,
		`{"thirdPartyAppAnalyticsLink":{"resourceName":"`+invAppLinksName(clientID, "3")+`","shareableLinkId":"`+invLinkMarker+`"}}`)
	out, err := e.invoke(thirdPartyAppAnalyticsLinksListID, `{"customer_id":"`+clientID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, invLinkMarker) || strings.Contains(strings.ToLower(out), "shareable") {
		t.Errorf("the link ID is in the result: %s", out)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].login != loginID {
		t.Fatalf("requests = %+v", api)
	}
	if strings.Contains(strings.ToLower(api[0].body), "shareable") ||
		api[0].query != "SELECT third_party_app_analytics_link.resource_name FROM third_party_app_analytics_link LIMIT 501" {
		t.Errorf("query = %q", api[0].query)
	}
	links := decode(t, out)["links"].([]any)
	if len(links) != 2 || links[0].(map[string]any)["id"] != "3" || links[1].(map[string]any)["id"] != "20" ||
		links[0].(map[string]any)["resource_name"] != invAppLinksName(clientID, "3") {
		t.Errorf("links = %s", out)
	}
	// The value stays out of an error as well: a failing answer that echoes it is mapped without it.
	e = newInvoicesEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = reply{status: 400, body: strings.ReplaceAll(adsFailure(400, "INVALID_ARGUMENT",
		"queryError", "UNRECOGNIZED_FIELD", ""), detailCanary, invLinkMarker)}
	_, err = e.invoke(thirdPartyAppAnalyticsLinksListID, `{"customer_id":"`+clientID+`"}`)
	if err == nil || strings.Contains(err.Error(), invLinkMarker) {
		t.Errorf("err = %v", err)
	}
	// The query builder takes no field, so no argument reaches the value.
	if strings.Contains(appAnalyticsLinkQuery, "shareable") {
		t.Error("the fixed query names the link ID")
	}
}

func TestAppAnalyticsLinksRefuseForeignRowsAndCutLongLists(t *testing.T) {
	for name, item := range map[string]string{
		"another account": `{"thirdPartyAppAnalyticsLink":{"resourceName":"` + invAppLinksName(otherID, "5") + `"}}`,
		"another kind":    `{"thirdPartyAppAnalyticsLink":{"resourceName":"customers/` + clientID + `/accountLinks/5"}}`,
		"no id":           `{"thirdPartyAppAnalyticsLink":{"resourceName":"` + invAppLinksName(clientID, "x") + `"}}`,
		"no link":         `{"customer":{"id":"1"}}`,
	} {
		e := newInvoicesEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = rows(item)
		out, err := e.invoke(thirdPartyAppAnalyticsLinksListID, `{"customer_id":"`+clientID+`"}`)
		if classOf(err) != provider.ClassInvalidResponse || out != "" {
			t.Errorf("%s: out = %s, err = %v", name, out, err)
		}
	}
	items := make([]string, maxListItems+1)
	for i := range items {
		items[i] = `{"thirdPartyAppAnalyticsLink":{"resourceName":"` + invAppLinksName(clientID, fmt.Sprint(i+1)) + `"}}`
	}
	e := newInvoicesEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = rows(items...)
	out, err := e.invoke(thirdPartyAppAnalyticsLinksListID, `{"customer_id":"`+clientID+`"}`)
	got := decode(t, out)
	if err != nil || got["truncated"] != true || len(got["links"].([]any)) != maxListItems {
		t.Errorf("err = %v, truncated = %v", err, got["truncated"])
	}
}

// invManagerAnswer answers the check whether the account is a manager and, for the tool itself, an empty list.
func invManagerAnswer(manager bool) func(s seen) *reply {
	return func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			return &reply{status: 200, body: fmt.Sprintf(`{"results":[{"customer":{"id":"%s","manager":%t}}]}`, managerID, manager)}
		}
		if strings.Contains(s.query, "FROM third_party_app_analytics_link") {
			return &reply{status: 200, body: `{"results":[]}`}
		}
		return nil
	}
}

func TestInvoicesAndLinksNeedAManagerTargetOnManagerAccounts(t *testing.T) {
	for _, id := range []string{invoicesListID, thirdPartyAppAnalyticsLinksListID} {
		args := fmt.Sprintf(invoicesToolArguments[id], managerID)
		t.Run(id+" a customer target that names a manager is refused", func(t *testing.T) {
			e := newInvoicesEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = invManagerAnswer(true)
			e.fake.routes["GET "+invPath(managerID)] = okReply(`{"invoices":[]}`)
			_, err := e.invoke(id, args)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") {
				t.Fatalf("err = %v", err)
			}
			for _, s := range e.fake.api() {
				if strings.Contains(s.path, "/invoices") || strings.Contains(s.query, "FROM third_party") {
					t.Errorf("the tool read the manager's data: %+v", s)
				}
			}
		})
		t.Run(id+" a manager target that names it passes without the check", func(t *testing.T) {
			e := newInvoicesEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.answer = invManagerAnswer(true)
			e.fake.routes["GET "+invPath(managerID)] = okReply(`{"invoices":[]}`)
			if _, err := e.invoke(id, args); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 {
				t.Errorf("requests = %+v, want the call alone", got)
			}
		})
		t.Run(id+" a client account passes after one check", func(t *testing.T) {
			e := newInvoicesEnvironment(t, []string{"customers/" + clientID}, nil)
			e.fake.answer = func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + clientID + `"}}]}`}
				}
				return nil
			}
			e.fake.routes["GET "+invPath(clientID)] = okReply(`{"invoices":[]}`)
			e.fake.routes["POST "+searchPath(clientID)] = rows()
			if _, err := e.invoke(id, fmt.Sprintf(invoicesToolArguments[id], clientID)); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 2 || !strings.Contains(got[0].query, "customer.manager") {
				t.Errorf("requests = %+v", got)
			}
		})
	}
}

func TestInvoicesToolsMapGoogleErrors(t *testing.T) {
	for id, args := range invoicesToolArguments {
		args = strings.ReplaceAll(args, "%s", clientID)
		route := "GET " + invPath(clientID)
		switch id {
		case incentivesFetchID:
			route = "GET /v26/incentives:fetchIncentive"
		case thirdPartyAppAnalyticsLinksListID:
			route = "POST " + searchPath(clientID)
		}
		for name, c := range map[string]struct {
			r     reply
			class provider.Class
		}{
			"permission": {reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
				"USER_PERMISSION_DENIED", "")}, provider.ClassPermission},
			"quota": {reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
				"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"5s"}`)}, provider.ClassRateLimited},
		} {
			e := newInvoicesEnvironment(t, nil, nil)
			e.fake.routes[route] = c.r
			_, err := e.invoke(id, args)
			if classOf(err) != c.class || err == nil || strings.Contains(err.Error(), detailCanary) ||
				strings.Contains(err.Error(), triggerCanary) {
				t.Errorf("%s %s: err = %v", id, name, err)
			}
		}
	}
	// A not invoiced account is mapped as well and never repeated.
	e := newInvoicesEnvironment(t, nil, nil)
	e.fake.routes["GET "+invPath(clientID)] = reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "invoiceError",
		"NOT_INVOICED_CUSTOMER", "")}
	_, err := e.invoke(invoicesListID, fmt.Sprintf(invArgs, clientID))
	if err == nil || strings.Contains(err.Error(), detailCanary) || len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
}
