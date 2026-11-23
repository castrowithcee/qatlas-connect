package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	invoicesListID = Provider + ".invoices.list"

	invoiceCollection        = "invoices"
	invoiceBillingCollection = "billingSetups"
	// invoiceMinYear is the first year Google serves invoices for (InvoiceError.YEAR_MONTH_TOO_OLD before it).
	invoiceMinYear = 2019
	invoiceMaxYear = 9999
)

// invoiceMonths are the calendar months of the MonthOfYear enum in v26; UNSPECIFIED and UNKNOWN are no months.
var invoiceMonths = []string{"JANUARY", "FEBRUARY", "MARCH", "APRIL", "MAY", "JUNE", "JULY", "AUGUST", "SEPTEMBER",
	"OCTOBER", "NOVEMBER", "DECEMBER"}

// The shapes of references inside an invoice. An invoice ID is no documented number, so it may hold letters.
var (
	invoiceCustomerRef = regexp.MustCompile(`^customers/[0-9]{1,20}$`)
	invoiceBudgetRef   = regexp.MustCompile(`^customers/[0-9]{1,20}/accountBudgets/[0-9]{1,20}$`)
	invoiceInvoiceRef  = regexp.MustCompile(`^customers/[0-9]{1,20}/invoices/[A-Za-z0-9_-]{1,64}$`)
	invoiceIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

type invoicesArguments struct {
	CustomerID      string `json:"customer_id"`
	BillingSetupID  string `json:"billing_setup_id"`
	IssueYear       int    `json:"issue_year"`
	IssueMonth      string `json:"issue_month"`
	GranularDetails bool   `json:"include_granular_level_invoice_details"`
}

// invoicesBillingName is the resource name of the billing setup, built from the checked customer and ID.
func invoicesBillingName(customerID, id string) string {
	return customerName(customerID) + "/" + invoiceBillingCollection + "/" + id
}

type (
	invoiceRangeWire struct {
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
	}
	invoiceInvalidWire struct {
		OriginalMonthOfService      string   `json:"originalMonthOfService"`
		OriginalYearOfService       string   `json:"originalYearOfService"`
		OriginalInvoiceID           string   `json:"originalInvoiceId"`
		OriginalAccountBudgetName   string   `json:"originalAccountBudgetName"`
		OriginalPurchaseOrderNumber string   `json:"originalPurchaseOrderNumber"`
		AmountMicros                *wireInt `json:"amountMicros"`
	}
	invoiceCampaignWire struct {
		CampaignDescription string   `json:"campaignDescription"`
		Quantity            *wireInt `json:"quantity"`
		UnitOfMeasure       string   `json:"unitOfMeasure"`
		AmountMicros        *wireInt `json:"amountMicros"`
	}
	invoiceBudgetWire struct {
		Customer                    string                `json:"customer"`
		CustomerDescriptiveName     string                `json:"customerDescriptiveName"`
		AccountBudget               string                `json:"accountBudget"`
		AccountBudgetName           string                `json:"accountBudgetName"`
		PurchaseOrderNumber         string                `json:"purchaseOrderNumber"`
		SubtotalAmountMicros        *wireInt              `json:"subtotalAmountMicros"`
		TaxAmountMicros             *wireInt              `json:"taxAmountMicros"`
		TotalAmountMicros           *wireInt              `json:"totalAmountMicros"`
		BillableActivityDateRange   *invoiceRangeWire     `json:"billableActivityDateRange"`
		ServedAmountMicros          *wireInt              `json:"servedAmountMicros"`
		BilledAmountMicros          *wireInt              `json:"billedAmountMicros"`
		OverdeliveryAmountMicros    *wireInt              `json:"overdeliveryAmountMicros"`
		InvalidActivityAmountMicros *wireInt              `json:"invalidActivityAmountMicros"`
		InvalidActivitySummaries    []invoiceInvalidWire  `json:"invalidActivitySummaries"`
		CampaignSummaries           []invoiceCampaignWire `json:"campaignSummaries"`
	}
	invoiceCostWire struct {
		RegulatoryFeeType string   `json:"regulatoryFeeType"`
		AmountMicros      *wireInt `json:"amountMicros"`
	}
	invoiceAdjustWire struct {
		AdjustmentDescription string   `json:"adjustmentDescription"`
		AmountMicros          *wireInt `json:"amountMicros"`
	}
	invoiceSummaryWire struct {
		Customer                                   string              `json:"customer"`
		BillingCorrectionSubtotalAmountMicros      *wireInt            `json:"billingCorrectionSubtotalAmountMicros"`
		BillingCorrectionTaxAmountMicros           *wireInt            `json:"billingCorrectionTaxAmountMicros"`
		BillingCorrectionTotalAmountMicros         *wireInt            `json:"billingCorrectionTotalAmountMicros"`
		CouponAdjustmentSubtotalAmountMicros       *wireInt            `json:"couponAdjustmentSubtotalAmountMicros"`
		CouponAdjustmentTaxAmountMicros            *wireInt            `json:"couponAdjustmentTaxAmountMicros"`
		CouponAdjustmentTotalAmountMicros          *wireInt            `json:"couponAdjustmentTotalAmountMicros"`
		ExcessCreditAdjustmentSubtotalAmountMicros *wireInt            `json:"excessCreditAdjustmentSubtotalAmountMicros"`
		ExcessCreditAdjustmentTaxAmountMicros      *wireInt            `json:"excessCreditAdjustmentTaxAmountMicros"`
		ExcessCreditAdjustmentTotalAmountMicros    *wireInt            `json:"excessCreditAdjustmentTotalAmountMicros"`
		RegulatoryCostsSubtotalAmountMicros        *wireInt            `json:"regulatoryCostsSubtotalAmountMicros"`
		RegulatoryCostsTaxAmountMicros             *wireInt            `json:"regulatoryCostsTaxAmountMicros"`
		RegulatoryCostsTotalAmountMicros           *wireInt            `json:"regulatoryCostsTotalAmountMicros"`
		SubtotalAmountMicros                       *wireInt            `json:"subtotalAmountMicros"`
		TaxAmountMicros                            *wireInt            `json:"taxAmountMicros"`
		TotalAmountMicros                          *wireInt            `json:"totalAmountMicros"`
		ExportChargeSubtotalAmountMicros           *wireInt            `json:"exportChargeSubtotalAmountMicros"`
		ExportChargeTaxAmountMicros                *wireInt            `json:"exportChargeTaxAmountMicros"`
		ExportChargeTotalAmountMicros              *wireInt            `json:"exportChargeTotalAmountMicros"`
		RegulatoryCostSummaries                    []invoiceCostWire   `json:"regulatoryCostSummaries"`
		AdjustmentSummaries                        []invoiceAdjustWire `json:"adjustmentSummaries"`
	}
	// invoiceWire has no member for pdf_url: the link is never copied into a result.
	invoiceWire struct {
		ResourceName                        string               `json:"resourceName"`
		ID                                  string               `json:"id"`
		Type                                string               `json:"type"`
		BillingSetup                        string               `json:"billingSetup"`
		PaymentsAccountID                   string               `json:"paymentsAccountId"`
		PaymentsProfileID                   string               `json:"paymentsProfileId"`
		IssueDate                           string               `json:"issueDate"`
		DueDate                             string               `json:"dueDate"`
		ServiceDateRange                    *invoiceRangeWire    `json:"serviceDateRange"`
		CurrencyCode                        string               `json:"currencyCode"`
		AdjustmentsSubtotalAmountMicros     *wireInt             `json:"adjustmentsSubtotalAmountMicros"`
		AdjustmentsTaxAmountMicros          *wireInt             `json:"adjustmentsTaxAmountMicros"`
		AdjustmentsTotalAmountMicros        *wireInt             `json:"adjustmentsTotalAmountMicros"`
		RegulatoryCostsSubtotalAmountMicros *wireInt             `json:"regulatoryCostsSubtotalAmountMicros"`
		RegulatoryCostsTaxAmountMicros      *wireInt             `json:"regulatoryCostsTaxAmountMicros"`
		RegulatoryCostsTotalAmountMicros    *wireInt             `json:"regulatoryCostsTotalAmountMicros"`
		ExportChargeSubtotalAmountMicros    *wireInt             `json:"exportChargeSubtotalAmountMicros"`
		ExportChargeTaxAmountMicros         *wireInt             `json:"exportChargeTaxAmountMicros"`
		ExportChargeTotalAmountMicros       *wireInt             `json:"exportChargeTotalAmountMicros"`
		SubtotalAmountMicros                *wireInt             `json:"subtotalAmountMicros"`
		TaxAmountMicros                     *wireInt             `json:"taxAmountMicros"`
		TotalAmountMicros                   *wireInt             `json:"totalAmountMicros"`
		CorrectedInvoice                    string               `json:"correctedInvoice"`
		ReplacedInvoices                    []string             `json:"replacedInvoices"`
		AccountBudgetSummaries              []invoiceBudgetWire  `json:"accountBudgetSummaries"`
		AccountSummaries                    []invoiceSummaryWire `json:"accountSummaries"`
	}
	invoicesAnswer struct {
		Invoices []invoiceWire `json:"invoices"`
	}
)

// invoiceHasPDF reads only whether the answer carries a pdf_url, and nothing else of it. The link is no tool output: Google documents
// that it is fetched with the account's token, and nothing shows that the address holds no access value.
type invoiceHasPDF struct {
	Invoices []struct {
		PDFURL string `json:"pdfUrl"`
	} `json:"invoices"`
}

type (
	invoiceRange struct {
		StartDate string `json:"start_date,omitempty"`
		EndDate   string `json:"end_date,omitempty"`
	}
	invoiceInvalid struct {
		OriginalMonthOfService      string `json:"original_month_of_service,omitempty"`
		OriginalYearOfService       string `json:"original_year_of_service,omitempty"`
		OriginalInvoiceID           string `json:"original_invoice_id,omitempty"`
		OriginalAccountBudgetName   string `json:"original_account_budget_name,omitempty"`
		OriginalPurchaseOrderNumber string `json:"original_purchase_order_number,omitempty"`
		AmountMicros                string `json:"amount_micros,omitempty"`
	}
	invoiceCampaign struct {
		CampaignDescription string `json:"campaign_description,omitempty"`
		Quantity            string `json:"quantity,omitempty"`
		UnitOfMeasure       string `json:"unit_of_measure,omitempty"`
		AmountMicros        string `json:"amount_micros,omitempty"`
	}
	invoiceBudget struct {
		Customer                    string            `json:"customer,omitempty"`
		CustomerDescriptiveName     string            `json:"customer_descriptive_name,omitempty"`
		AccountBudget               string            `json:"account_budget,omitempty"`
		AccountBudgetName           string            `json:"account_budget_name,omitempty"`
		PurchaseOrderNumber         string            `json:"purchase_order_number,omitempty"`
		SubtotalAmountMicros        string            `json:"subtotal_amount_micros,omitempty"`
		TaxAmountMicros             string            `json:"tax_amount_micros,omitempty"`
		TotalAmountMicros           string            `json:"total_amount_micros,omitempty"`
		BillableActivityDateRange   *invoiceRange     `json:"billable_activity_date_range,omitempty"`
		ServedAmountMicros          string            `json:"served_amount_micros,omitempty"`
		BilledAmountMicros          string            `json:"billed_amount_micros,omitempty"`
		OverdeliveryAmountMicros    string            `json:"overdelivery_amount_micros,omitempty"`
		InvalidActivityAmountMicros string            `json:"invalid_activity_amount_micros,omitempty"`
		InvalidActivitySummaries    []invoiceInvalid  `json:"invalid_activity_summaries"`
		CampaignSummaries           []invoiceCampaign `json:"campaign_summaries"`
	}
	invoiceCost struct {
		RegulatoryFeeType string `json:"regulatory_fee_type,omitempty"`
		AmountMicros      string `json:"amount_micros,omitempty"`
	}
	invoiceAdjust struct {
		AdjustmentDescription string `json:"adjustment_description,omitempty"`
		AmountMicros          string `json:"amount_micros,omitempty"`
	}
	invoiceSummary struct {
		Customer                                   string          `json:"customer,omitempty"`
		BillingCorrectionSubtotalAmountMicros      string          `json:"billing_correction_subtotal_amount_micros,omitempty"`
		BillingCorrectionTaxAmountMicros           string          `json:"billing_correction_tax_amount_micros,omitempty"`
		BillingCorrectionTotalAmountMicros         string          `json:"billing_correction_total_amount_micros,omitempty"`
		CouponAdjustmentSubtotalAmountMicros       string          `json:"coupon_adjustment_subtotal_amount_micros,omitempty"`
		CouponAdjustmentTaxAmountMicros            string          `json:"coupon_adjustment_tax_amount_micros,omitempty"`
		CouponAdjustmentTotalAmountMicros          string          `json:"coupon_adjustment_total_amount_micros,omitempty"`
		ExcessCreditAdjustmentSubtotalAmountMicros string          `json:"excess_credit_adjustment_subtotal_amount_micros,omitempty"`
		ExcessCreditAdjustmentTaxAmountMicros      string          `json:"excess_credit_adjustment_tax_amount_micros,omitempty"`
		ExcessCreditAdjustmentTotalAmountMicros    string          `json:"excess_credit_adjustment_total_amount_micros,omitempty"`
		RegulatoryCostsSubtotalAmountMicros        string          `json:"regulatory_costs_subtotal_amount_micros,omitempty"`
		RegulatoryCostsTaxAmountMicros             string          `json:"regulatory_costs_tax_amount_micros,omitempty"`
		RegulatoryCostsTotalAmountMicros           string          `json:"regulatory_costs_total_amount_micros,omitempty"`
		SubtotalAmountMicros                       string          `json:"subtotal_amount_micros,omitempty"`
		TaxAmountMicros                            string          `json:"tax_amount_micros,omitempty"`
		TotalAmountMicros                          string          `json:"total_amount_micros,omitempty"`
		ExportChargeSubtotalAmountMicros           string          `json:"export_charge_subtotal_amount_micros,omitempty"`
		ExportChargeTaxAmountMicros                string          `json:"export_charge_tax_amount_micros,omitempty"`
		ExportChargeTotalAmountMicros              string          `json:"export_charge_total_amount_micros,omitempty"`
		RegulatoryCostSummaries                    []invoiceCost   `json:"regulatory_cost_summaries"`
		AdjustmentSummaries                        []invoiceAdjust `json:"adjustment_summaries"`
	}
	invoiceResult struct {
		ResourceName                        string           `json:"resource_name"`
		ID                                  string           `json:"id,omitempty"`
		Type                                string           `json:"type,omitempty"`
		BillingSetup                        string           `json:"billing_setup,omitempty"`
		PaymentsAccountID                   string           `json:"payments_account_id,omitempty"`
		PaymentsProfileID                   string           `json:"payments_profile_id,omitempty"`
		IssueDate                           string           `json:"issue_date,omitempty"`
		DueDate                             string           `json:"due_date,omitempty"`
		ServiceDateRange                    *invoiceRange    `json:"service_date_range,omitempty"`
		CurrencyCode                        string           `json:"currency_code,omitempty"`
		AdjustmentsSubtotalAmountMicros     string           `json:"adjustments_subtotal_amount_micros,omitempty"`
		AdjustmentsTaxAmountMicros          string           `json:"adjustments_tax_amount_micros,omitempty"`
		AdjustmentsTotalAmountMicros        string           `json:"adjustments_total_amount_micros,omitempty"`
		RegulatoryCostsSubtotalAmountMicros string           `json:"regulatory_costs_subtotal_amount_micros,omitempty"`
		RegulatoryCostsTaxAmountMicros      string           `json:"regulatory_costs_tax_amount_micros,omitempty"`
		RegulatoryCostsTotalAmountMicros    string           `json:"regulatory_costs_total_amount_micros,omitempty"`
		ExportChargeSubtotalAmountMicros    string           `json:"export_charge_subtotal_amount_micros,omitempty"`
		ExportChargeTaxAmountMicros         string           `json:"export_charge_tax_amount_micros,omitempty"`
		ExportChargeTotalAmountMicros       string           `json:"export_charge_total_amount_micros,omitempty"`
		SubtotalAmountMicros                string           `json:"subtotal_amount_micros,omitempty"`
		TaxAmountMicros                     string           `json:"tax_amount_micros,omitempty"`
		TotalAmountMicros                   string           `json:"total_amount_micros,omitempty"`
		CorrectedInvoice                    string           `json:"corrected_invoice,omitempty"`
		ReplacedInvoices                    []string         `json:"replaced_invoices"`
		HasPDF                              bool             `json:"has_pdf"`
		AccountBudgetSummaries              []invoiceBudget  `json:"account_budget_summaries"`
		AccountSummaries                    []invoiceSummary `json:"account_summaries"`
	}
	invoicesResult struct {
		Invoices  []invoiceResult `json:"invoices"`
		Truncated bool            `json:"truncated"`
	}
)

// invoiceCut bounds a list of the answer and records that it did.
func invoiceCut[T any](list []T, cut *bool) []T {
	if len(list) > maxListItems {
		*cut = true
		return list[:maxListItems]
	}
	return list
}

func invoiceText(value string) string { return boundedText(value, maxText) }

func invoiceRangeOf(w *invoiceRangeWire) *invoiceRange {
	if w == nil {
		return nil
	}
	return &invoiceRange{StartDate: code(w.StartDate), EndDate: code(w.EndDate)}
}

// invoiceRef keeps a reference of an invoice only in the form of its kind; anything else is no answer to read.
func invoiceRef(value string, pattern *regexp.Regexp) (string, bool) {
	if value == "" || pattern.MatchString(value) {
		return value, true
	}
	return "", false
}

// invoiceOf maps one invoice. It must belong to the account and the billing setup of the call: its resource name
// is that of the account, and its billing setup is the one asked for.
func invoiceOf(w invoiceWire, customerID, billingName string, pdf bool, cut *bool) (invoiceResult, bool) {
	rest, ok := strings.CutPrefix(w.ResourceName, customerName(customerID)+"/"+invoiceCollection+"/")
	if !ok || !invoiceIDPattern.MatchString(rest) || (w.BillingSetup != "" && w.BillingSetup != billingName) {
		return invoiceResult{}, false
	}
	corrected, ok1 := invoiceRef(w.CorrectedInvoice, invoiceInvoiceRef)
	r := invoiceResult{ResourceName: w.ResourceName, ID: code(w.ID), Type: code(w.Type), BillingSetup: w.BillingSetup,
		PaymentsAccountID: code(w.PaymentsAccountID), PaymentsProfileID: code(w.PaymentsProfileID),
		IssueDate: code(w.IssueDate), DueDate: code(w.DueDate), ServiceDateRange: invoiceRangeOf(w.ServiceDateRange),
		CurrencyCode:                        code(w.CurrencyCode),
		AdjustmentsSubtotalAmountMicros:     intText(w.AdjustmentsSubtotalAmountMicros),
		AdjustmentsTaxAmountMicros:          intText(w.AdjustmentsTaxAmountMicros),
		AdjustmentsTotalAmountMicros:        intText(w.AdjustmentsTotalAmountMicros),
		RegulatoryCostsSubtotalAmountMicros: intText(w.RegulatoryCostsSubtotalAmountMicros),
		RegulatoryCostsTaxAmountMicros:      intText(w.RegulatoryCostsTaxAmountMicros),
		RegulatoryCostsTotalAmountMicros:    intText(w.RegulatoryCostsTotalAmountMicros),
		ExportChargeSubtotalAmountMicros:    intText(w.ExportChargeSubtotalAmountMicros),
		ExportChargeTaxAmountMicros:         intText(w.ExportChargeTaxAmountMicros),
		ExportChargeTotalAmountMicros:       intText(w.ExportChargeTotalAmountMicros),
		SubtotalAmountMicros:                intText(w.SubtotalAmountMicros), TaxAmountMicros: intText(w.TaxAmountMicros),
		TotalAmountMicros: intText(w.TotalAmountMicros), CorrectedInvoice: corrected, HasPDF: pdf,
		ReplacedInvoices: []string{}, AccountBudgetSummaries: []invoiceBudget{}, AccountSummaries: []invoiceSummary{}}
	if !ok1 {
		return invoiceResult{}, false
	}
	for _, ref := range invoiceCut(w.ReplacedInvoices, cut) {
		if _, ok := invoiceRef(ref, invoiceInvoiceRef); !ok || ref == "" {
			return invoiceResult{}, false
		}
		r.ReplacedInvoices = append(r.ReplacedInvoices, ref)
	}
	for _, b := range invoiceCut(w.AccountBudgetSummaries, cut) {
		item, ok := invoiceBudgetOf(b, cut)
		if !ok {
			return invoiceResult{}, false
		}
		r.AccountBudgetSummaries = append(r.AccountBudgetSummaries, item)
	}
	for _, s := range invoiceCut(w.AccountSummaries, cut) {
		item, ok := invoiceSummaryOf(s, cut)
		if !ok {
			return invoiceResult{}, false
		}
		r.AccountSummaries = append(r.AccountSummaries, item)
	}
	return r, true
}

func invoiceBudgetOf(b invoiceBudgetWire, cut *bool) (invoiceBudget, bool) {
	customer, ok1 := invoiceRef(b.Customer, invoiceCustomerRef)
	budget, ok2 := invoiceRef(b.AccountBudget, invoiceBudgetRef)
	if !ok1 || !ok2 {
		return invoiceBudget{}, false
	}
	out := invoiceBudget{Customer: customer, CustomerDescriptiveName: invoiceText(b.CustomerDescriptiveName),
		AccountBudget: budget, AccountBudgetName: invoiceText(b.AccountBudgetName),
		PurchaseOrderNumber: invoiceText(b.PurchaseOrderNumber), SubtotalAmountMicros: intText(b.SubtotalAmountMicros),
		TaxAmountMicros: intText(b.TaxAmountMicros), TotalAmountMicros: intText(b.TotalAmountMicros),
		BillableActivityDateRange: invoiceRangeOf(b.BillableActivityDateRange),
		ServedAmountMicros:        intText(b.ServedAmountMicros), BilledAmountMicros: intText(b.BilledAmountMicros),
		OverdeliveryAmountMicros:    intText(b.OverdeliveryAmountMicros),
		InvalidActivityAmountMicros: intText(b.InvalidActivityAmountMicros),
		InvalidActivitySummaries:    []invoiceInvalid{}, CampaignSummaries: []invoiceCampaign{}}
	for _, s := range invoiceCut(b.InvalidActivitySummaries, cut) {
		out.InvalidActivitySummaries = append(out.InvalidActivitySummaries, invoiceInvalid{
			OriginalMonthOfService: code(s.OriginalMonthOfService), OriginalYearOfService: code(s.OriginalYearOfService),
			OriginalInvoiceID: code(s.OriginalInvoiceID), OriginalAccountBudgetName: invoiceText(s.OriginalAccountBudgetName),
			OriginalPurchaseOrderNumber: invoiceText(s.OriginalPurchaseOrderNumber), AmountMicros: intText(s.AmountMicros)})
	}
	for _, s := range invoiceCut(b.CampaignSummaries, cut) {
		out.CampaignSummaries = append(out.CampaignSummaries, invoiceCampaign{
			CampaignDescription: invoiceText(s.CampaignDescription), Quantity: intText(s.Quantity),
			UnitOfMeasure: code(s.UnitOfMeasure), AmountMicros: intText(s.AmountMicros)})
	}
	return out, true
}

func invoiceSummaryOf(s invoiceSummaryWire, cut *bool) (invoiceSummary, bool) {
	customer, ok := invoiceRef(s.Customer, invoiceCustomerRef)
	if !ok {
		return invoiceSummary{}, false
	}
	out := invoiceSummary{Customer: customer,
		BillingCorrectionSubtotalAmountMicros:      intText(s.BillingCorrectionSubtotalAmountMicros),
		BillingCorrectionTaxAmountMicros:           intText(s.BillingCorrectionTaxAmountMicros),
		BillingCorrectionTotalAmountMicros:         intText(s.BillingCorrectionTotalAmountMicros),
		CouponAdjustmentSubtotalAmountMicros:       intText(s.CouponAdjustmentSubtotalAmountMicros),
		CouponAdjustmentTaxAmountMicros:            intText(s.CouponAdjustmentTaxAmountMicros),
		CouponAdjustmentTotalAmountMicros:          intText(s.CouponAdjustmentTotalAmountMicros),
		ExcessCreditAdjustmentSubtotalAmountMicros: intText(s.ExcessCreditAdjustmentSubtotalAmountMicros),
		ExcessCreditAdjustmentTaxAmountMicros:      intText(s.ExcessCreditAdjustmentTaxAmountMicros),
		ExcessCreditAdjustmentTotalAmountMicros:    intText(s.ExcessCreditAdjustmentTotalAmountMicros),
		RegulatoryCostsSubtotalAmountMicros:        intText(s.RegulatoryCostsSubtotalAmountMicros),
		RegulatoryCostsTaxAmountMicros:             intText(s.RegulatoryCostsTaxAmountMicros),
		RegulatoryCostsTotalAmountMicros:           intText(s.RegulatoryCostsTotalAmountMicros),
		SubtotalAmountMicros:                       intText(s.SubtotalAmountMicros),
		TaxAmountMicros:                            intText(s.TaxAmountMicros),
		TotalAmountMicros:                          intText(s.TotalAmountMicros),
		ExportChargeSubtotalAmountMicros:           intText(s.ExportChargeSubtotalAmountMicros),
		ExportChargeTaxAmountMicros:                intText(s.ExportChargeTaxAmountMicros),
		ExportChargeTotalAmountMicros:              intText(s.ExportChargeTotalAmountMicros),
		RegulatoryCostSummaries:                    []invoiceCost{}, AdjustmentSummaries: []invoiceAdjust{}}
	for _, c := range invoiceCut(s.RegulatoryCostSummaries, cut) {
		out.RegulatoryCostSummaries = append(out.RegulatoryCostSummaries, invoiceCost{
			RegulatoryFeeType: code(c.RegulatoryFeeType), AmountMicros: intText(c.AmountMicros)})
	}
	for _, a := range invoiceCut(s.AdjustmentSummaries, cut) {
		out.AdjustmentSummaries = append(out.AdjustmentSummaries, invoiceAdjust{
			AdjustmentDescription: invoiceText(a.AdjustmentDescription), AmountMicros: intText(a.AmountMicros)})
	}
	return out, true
}

var invoiceRangeSchema = object(schema{"start_date": stringSchema, "end_date": stringSchema})

func invoiceAmountProperties(names ...string) schema {
	out := schema{}
	for _, n := range names {
		out[n] = stringSchema
	}
	return out
}

func invoiceMerge(parts ...schema) schema {
	out := schema{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

var invoiceResultSchema = object(invoiceMerge(invoiceAmountProperties("resource_name", "id", "type", "billing_setup",
	"payments_account_id", "payments_profile_id", "issue_date", "due_date", "currency_code", "corrected_invoice",
	"adjustments_subtotal_amount_micros", "adjustments_tax_amount_micros", "adjustments_total_amount_micros",
	"regulatory_costs_subtotal_amount_micros", "regulatory_costs_tax_amount_micros",
	"regulatory_costs_total_amount_micros", "export_charge_subtotal_amount_micros", "export_charge_tax_amount_micros",
	"export_charge_total_amount_micros", "subtotal_amount_micros", "tax_amount_micros", "total_amount_micros"),
	schema{
		"service_date_range": invoiceRangeSchema, "replaced_invoices": arrayOf(stringSchema), "has_pdf": booleanSchema,
		"account_budget_summaries": arrayOf(object(invoiceMerge(invoiceAmountProperties("customer",
			"customer_descriptive_name", "account_budget", "account_budget_name", "purchase_order_number",
			"subtotal_amount_micros", "tax_amount_micros", "total_amount_micros", "served_amount_micros",
			"billed_amount_micros", "overdelivery_amount_micros", "invalid_activity_amount_micros"), schema{
			"billable_activity_date_range": invoiceRangeSchema,
			"invalid_activity_summaries": arrayOf(object(invoiceAmountProperties("original_month_of_service",
				"original_year_of_service", "original_invoice_id", "original_account_budget_name",
				"original_purchase_order_number", "amount_micros"))),
			"campaign_summaries": arrayOf(object(invoiceAmountProperties("campaign_description", "quantity",
				"unit_of_measure", "amount_micros"))),
		}))),
		"account_summaries": arrayOf(object(invoiceMerge(invoiceAmountProperties("customer",
			"billing_correction_subtotal_amount_micros", "billing_correction_tax_amount_micros",
			"billing_correction_total_amount_micros", "coupon_adjustment_subtotal_amount_micros",
			"coupon_adjustment_tax_amount_micros", "coupon_adjustment_total_amount_micros",
			"excess_credit_adjustment_subtotal_amount_micros", "excess_credit_adjustment_tax_amount_micros",
			"excess_credit_adjustment_total_amount_micros", "regulatory_costs_subtotal_amount_micros",
			"regulatory_costs_tax_amount_micros", "regulatory_costs_total_amount_micros", "subtotal_amount_micros",
			"tax_amount_micros", "total_amount_micros", "export_charge_subtotal_amount_micros",
			"export_charge_tax_amount_micros", "export_charge_total_amount_micros"), schema{
			"regulatory_cost_summaries": arrayOf(object(invoiceAmountProperties("regulatory_fee_type", "amount_micros"))),
			"adjustment_summaries":      arrayOf(object(invoiceAmountProperties("adjustment_description", "amount_micros"))),
		}))),
	}), "resource_name", "has_pdf")

// invoicesRisk is the risk of the invoice list: it reads financial data of an account.
var invoicesRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "financial data of a Google Ads account: invoice amounts and taxes, payments account and profile " +
		"IDs, purchase order numbers, and the names of accounts, budgets and campaigns on the invoice; the link to the " +
		"invoice PDF is withheld",
}

var invoicesList = capability.Descriptor{
	ID: invoicesListID, Version: 1, Title: "List the invoices of a Google Ads billing setup",
	Description: "List the invoices Google issued for one billing setup of an account in one month " +
		"(InvoiceService.ListInvoices of API version v26, GET customers/ID/invoices): type, issue and due date, service " +
		"period, currency, subtotal, tax and total as micros (millionths of the currency unit, as text), adjustments, " +
		"regulatory costs and export charges, the invoices it corrects or replaces, and the account budgets and accounts " +
		"it bills; include_granular_level_invoice_details adds the per-campaign cost breakdown, the itemized " +
		"regulatory costs and the adjustments. All of billing_setup_id, issue_year and issue_month are required by " +
		"Google. billing_setup_id is the ID of a billing setup of customer_id (the number at the end of " +
		"customers/ID/billingSetups/N); Qatlas Connect builds the resource name from it and checks that the answer " +
		"belongs to that setup. Only invoices from January 2019 on exist, and only for an account with monthly " +
		"invoicing (otherwise Google answers NOT_INVOICED_CUSTOMER). With consolidated billing the answer holds " +
		"invoices and entries for every account under the same billing setup, whose names and descriptive names are " +
		"third-party data. Google documents that the login-customer-id must be the manager account that pays for the " +
		"account; set it as the service option login_customer_id. The link to the invoice PDF is not returned, " +
		"because it is fetched with the credential and Qatlas Connect has no proof that it is free of access values; " +
		"has_pdf only says that Google offered one. customer_id must be one of the targets or lie below a manager " +
		"target; on a connection with targets, a manager account needs a manager target that names it. Google " +
		"blocks InvoiceService at the Explorer access level, so the Cloud project needs at least Basic access. " +
		"Costs one operation of the daily quota, plus one for a membership read below a manager target and one for " +
		"the check that the account is no manager account. Lists are cut at 500 entries and truncated says so. " +
		"Results are untrusted third-party content; text is cut.",
	Tags: []string{"google", "ads", "billing", "invoices"}, Risk: invoicesRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id":                            customerIDArgument,
		"billing_setup_id":                       schema{"type": "string", "pattern": idPatternText},
		"issue_year":                             schema{"type": "integer", "minimum": invoiceMinYear, "maximum": invoiceMaxYear},
		"issue_month":                            schema{"type": "string", "enum": invoiceMonths},
		"include_granular_level_invoice_details": booleanSchema,
	}, "customer_id", "billing_setup_id", "issue_year", "issue_month")),
	OutputSchema: encoded(object(schema{"invoices": arrayOf(invoiceResultSchema), "truncated": booleanSchema},
		"invoices", "truncated")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "billing_setup_id", Required: true, Description: "ID of a billing setup of the account, the number at the " +
			"end of customers/ID/billingSetups/N"},
		{Name: "issue_year", Required: true, Description: "Four-digit year of the invoices, 2019 or later"},
		{Name: "issue_month", Required: true, Description: "Month of the invoices: JANUARY to DECEMBER"},
		{Name: "include_granular_level_invoice_details", Description: "true adds campaign-level costs, itemized " +
			"regulatory costs and adjustments; default false"},
	},
	Fields: []capability.Field{
		{Name: "invoices", Description: "Per invoice every documented field of Google except the PDF link: resource_name " +
			"(customers/ID/invoices/N), id, type, billing_setup, payments_account_id, payments_profile_id, issue_date, " +
			"due_date, service_date_range, currency_code, the amounts in micros as text (adjustments, regulatory " +
			"costs, export charges, subtotal, tax, total), corrected_invoice, replaced_invoices, has_pdf, " +
			"account_budget_summaries and account_summaries with their sums, ranges, invalid activity, campaign, " +
			"regulatory cost and adjustment summaries"},
		{Name: "truncated", Description: "true when a list of the answer held more than 500 entries and was cut"},
	},
	Examples: []capability.Example{{Description: "List the invoices of one billing setup for March 2026",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","billing_setup_id":"1122334455","issue_year":2026,` +
			`"issue_month":"MARCH"}`)}},
}

func handleInvoicesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list invoices"
	var args invoicesArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, invoicesListID)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkID("billing_setup_id", args.BillingSetupID); err != nil {
		return nil, err
	}
	if args.IssueYear < invoiceMinYear || args.IssueYear > invoiceMaxYear {
		return nil, badRequest("issue_year must be a four-digit year from 2019 on")
	}
	if !slices.Contains(invoiceMonths, args.IssueMonth) {
		return nil, badRequest("issue_month must be JANUARY to DECEMBER")
	}
	billingName := invoicesBillingName(args.CustomerID, args.BillingSetupID)
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	query := url.Values{"billingSetup": {billingName}, "issueYear": {strconv.Itoa(args.IssueYear)},
		"issueMonth": {args.IssueMonth}}
	if args.GranularDetails {
		query.Set("includeGranularLevelInvoiceDetails", "true")
	}
	var body json.RawMessage
	if err := c.read(ctx, request{op: op, tool: invoicesListID, method: http.MethodGet,
		path: "/" + customerName(args.CustomerID) + "/invoices?" + query.Encode(), onAccount: true}, &body); err != nil {
		return nil, err
	}
	var answer invoicesAnswer
	var pdfs invoiceHasPDF
	if json.Unmarshal(body, &answer) != nil || json.Unmarshal(body, &pdfs) != nil ||
		len(pdfs.Invoices) != len(answer.Invoices) {
		return nil, provider.InvalidResponse(op, subject+" returned an invoice answer Qatlas Connect cannot read")
	}
	result := invoicesResult{Invoices: []invoiceResult{}}
	for i, w := range answer.Invoices {
		if i == maxListItems {
			result.Truncated = true
			break
		}
		pdf := pdfs.Invoices[i].PDFURL != ""
		item, ok := invoiceOf(w, args.CustomerID, billingName, pdf, &result.Truncated)
		if !ok {
			return nil, provider.InvalidResponse(op, subject+" returned an invoice that is not one of the account and "+
				"billing setup asked for, or holds a reference in an unknown form")
		}
		result.Invoices = append(result.Invoices, item)
	}
	return result, nil
}
