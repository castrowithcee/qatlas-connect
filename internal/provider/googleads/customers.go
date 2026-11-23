package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	customersListAccessibleID = Provider + ".customers.list-accessible"
	customersGetID            = Provider + ".customers.get"
)

// maxAccessible bounds the accounts customers.list-accessible returns.
const maxAccessible = 1000

var accessibleNamePattern = regexp.MustCompile(`^customers/([0-9]{1,20})$`)

// accessibleAccount is one account the credential reaches directly.
type accessibleAccount struct {
	ResourceName string `json:"resource_name"`
	ID           string `json:"id"`
}

// accessible reads the accounts the credential reaches directly. Google ignores login-customer-id for this
// call, so it is not sent. A resource name in another form makes the whole answer invalid.
func (c *client) accessible(ctx context.Context, op string) ([]accessibleAccount, error) {
	var answer struct {
		ResourceNames []string `json:"resourceNames"`
	}
	if err := c.read(ctx, request{op: op, tool: customersListAccessibleID, method: http.MethodGet,
		path: "/customers:listAccessibleCustomers"}, &answer); err != nil {
		return nil, err
	}
	accounts := make([]accessibleAccount, 0, len(answer.ResourceNames))
	for _, name := range answer.ResourceNames {
		match := accessibleNamePattern.FindStringSubmatch(name)
		if match == nil {
			return nil, provider.InvalidResponse(op, subject+" returned an account name in a form Qatlas Connect "+
				"does not know")
		}
		id, ok := canonicalID(wireInt(match[1]))
		if !ok {
			return nil, provider.InvalidResponse(op, subject+" returned an account ID Qatlas Connect cannot read")
		}
		accounts = append(accounts, accessibleAccount{ResourceName: name, ID: id})
	}
	return accounts, nil
}

var customersListAccessible = capability.Descriptor{
	ID: customersListAccessibleID, Version: 1, Title: "List directly accessible Google Ads accounts",
	Description: "List the Google Ads accounts the credential reaches directly (CustomerService." +
		"ListAccessibleCustomers): the resource name customers/ID and the ten-digit ID of each, without names. An " +
		"account reached only through a manager account is not listed; customer-clients.list on the manager lists " +
		"it. On a connection with targets only accounts that a target names are listed. The call costs one operation " +
		"of the daily quota and nothing else, needs no account, and is the connection test of this provider.",
	Tags: []string{"google", "ads", "customers", "accounts"}, Risk: readRisk, Provider: Provider,
	InputSchema:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	OutputSchema: encoded(object(schema{"customers": arrayOf(object(schema{"resource_name": stringSchema, "id": stringSchema})), "truncated": booleanSchema}, "customers")),
	Arguments:    []capability.Argument{},
	Fields: []capability.Field{
		{Name: "customers", Description: "Per account: resource_name (customers/ID) and id, the ten-digit customer " +
			"ID that customer_id of the other tools takes; at most 1,000"},
		{Name: "truncated", Description: "true when Google listed more than 1,000 accounts and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "List the accounts the credential reaches directly",
		Arguments: json.RawMessage(`{}`)}},
}

func handleCustomersListAccessible(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, _ json.RawMessage) (any, error) {
	const op = "list accessible customers"
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, conn, secrets, red)
	if err != nil {
		return nil, err
	}
	accounts, err := c.accessible(ctx, op)
	if err != nil {
		return nil, err
	}
	kept := []accessibleAccount{}
	for _, account := range accounts {
		if !conn.binding.bound() || conn.binding.names(account.ID) {
			kept = append(kept, account)
		}
	}
	result := struct {
		Customers []accessibleAccount `json:"customers"`
		Truncated bool                `json:"truncated"`
	}{Customers: kept}
	if len(kept) > maxAccessible {
		result.Customers, result.Truncated = kept[:maxAccessible], true
	}
	return result, nil
}

// customerFields are the selectable fields of the customer resource in v26 (fields reference of the API),
// every one of them. The query names them, since GAQL has no wildcard.
var customerFields = []string{
	"resource_name", "id", "descriptive_name", "currency_code", "time_zone", "tracking_url_template",
	"final_url_suffix", "auto_tagging_enabled", "has_partners_badge", "manager", "test_account", "status",
	"optimization_score", "optimization_score_weight", "pay_per_conversion_eligibility_failure_reasons",
	"call_reporting_setting.call_reporting_enabled", "call_reporting_setting.call_conversion_reporting_enabled",
	"call_reporting_setting.call_conversion_action",
	"conversion_tracking_setting.conversion_tracking_id",
	"conversion_tracking_setting.cross_account_conversion_tracking_id",
	"conversion_tracking_setting.accepted_customer_data_terms",
	"conversion_tracking_setting.conversion_tracking_status",
	"conversion_tracking_setting.enablement_setting", "conversion_tracking_setting.effective_enablement_setting",
	"conversion_tracking_setting.google_ads_conversion_customer",
	"remarketing_setting.google_global_site_tag", "customer_agreement_setting.accepted_lead_form_terms",
	"local_services_settings.granular_license_statuses", "local_services_settings.granular_insurance_statuses",
	"local_services_settings.redemption_provider_info.external_merchant_id.alphanumeric_id",
	"local_services_settings.redemption_provider_info.external_merchant_id.numeric_id",
	"location_asset_auto_migration_done", "location_asset_auto_migration_done_date_time",
	"image_asset_auto_migration_done", "image_asset_auto_migration_done_date_time",
	"video_brand_safety_suitability",
	"video_customer.third_party_integration_partners.brand_lift_integration_partners",
	"video_customer.third_party_integration_partners.brand_safety_integration_partners",
	"video_customer.third_party_integration_partners.conversion_attribution_integration_partners",
	"video_customer.third_party_integration_partners.reach_integration_partners",
	"video_customer.third_party_integration_partners.viewability_integration_partners",
	"contains_eu_political_advertising",
}

func customerQuery() string {
	fields := make([]string, len(customerFields))
	for i, field := range customerFields {
		fields[i] = "customer." + field
	}
	return "SELECT " + strings.Join(fields, ", ") + " FROM customer LIMIT 2"
}

// The customer resource as REST returns it. A bool or number Google leaves out is false or zero; the
// optimization score is absent for a manager and an account without a score.
type (
	customerRow struct {
		Customer *customerWire `json:"customer"`
	}
	customerWire struct {
		ResourceName            string   `json:"resourceName"`
		ID                      *wireInt `json:"id"`
		DescriptiveName         string   `json:"descriptiveName"`
		CurrencyCode            string   `json:"currencyCode"`
		TimeZone                string   `json:"timeZone"`
		TrackingURLTemplate     string   `json:"trackingUrlTemplate"`
		FinalURLSuffix          string   `json:"finalUrlSuffix"`
		AutoTaggingEnabled      bool     `json:"autoTaggingEnabled"`
		HasPartnersBadge        bool     `json:"hasPartnersBadge"`
		Manager                 bool     `json:"manager"`
		TestAccount             bool     `json:"testAccount"`
		Status                  string   `json:"status"`
		OptimizationScore       *float64 `json:"optimizationScore"`
		OptimizationScoreWeight *float64 `json:"optimizationScoreWeight"`
		PayPerConversionReasons []string `json:"payPerConversionEligibilityFailureReasons"`
		CallReportingSetting    struct {
			CallReportingEnabled           bool   `json:"callReportingEnabled"`
			CallConversionReportingEnabled bool   `json:"callConversionReportingEnabled"`
			CallConversionAction           string `json:"callConversionAction"`
		} `json:"callReportingSetting"`
		ConversionTrackingSetting struct {
			ConversionTrackingID             *wireInt `json:"conversionTrackingId"`
			CrossAccountConversionTrackingID *wireInt `json:"crossAccountConversionTrackingId"`
			AcceptedCustomerDataTerms        bool     `json:"acceptedCustomerDataTerms"`
			ConversionTrackingStatus         string   `json:"conversionTrackingStatus"`
			EnablementSetting                string   `json:"enablementSetting"`
			EffectiveEnablementSetting       string   `json:"effectiveEnablementSetting"`
			GoogleAdsConversionCustomer      string   `json:"googleAdsConversionCustomer"`
		} `json:"conversionTrackingSetting"`
		RemarketingSetting struct {
			GoogleGlobalSiteTag string `json:"googleGlobalSiteTag"`
		} `json:"remarketingSetting"`
		CustomerAgreementSetting struct {
			AcceptedLeadFormTerms bool `json:"acceptedLeadFormTerms"`
		} `json:"customerAgreementSetting"`
		LocalServicesSettings struct {
			GranularLicenseStatuses   []verificationWire `json:"granularLicenseStatuses"`
			GranularInsuranceStatuses []verificationWire `json:"granularInsuranceStatuses"`
			RedemptionProviderInfo    struct {
				ExternalMerchantID struct {
					AlphanumericID string   `json:"alphanumericId"`
					NumericID      *wireInt `json:"numericId"`
				} `json:"externalMerchantId"`
			} `json:"redemptionProviderInfo"`
		} `json:"localServicesSettings"`
		LocationAssetAutoMigrationDone         bool   `json:"locationAssetAutoMigrationDone"`
		LocationAssetAutoMigrationDoneDateTime string `json:"locationAssetAutoMigrationDoneDateTime"`
		ImageAssetAutoMigrationDone            bool   `json:"imageAssetAutoMigrationDone"`
		ImageAssetAutoMigrationDoneDateTime    string `json:"imageAssetAutoMigrationDoneDateTime"`
		VideoBrandSafetySuitability            string `json:"videoBrandSafetySuitability"`
		VideoCustomer                          struct {
			Partners struct {
				BrandLift             []partnerWire `json:"brandLiftIntegrationPartners"`
				BrandSafety           []partnerWire `json:"brandSafetyIntegrationPartners"`
				ConversionAttribution []partnerWire `json:"conversionAttributionIntegrationPartners"`
				Reach                 []partnerWire `json:"reachIntegrationPartners"`
				Viewability           []partnerWire `json:"viewabilityIntegrationPartners"`
			} `json:"thirdPartyIntegrationPartners"`
		} `json:"videoCustomer"`
		ContainsEUPoliticalAdvertising string `json:"containsEuPoliticalAdvertising"`
	}
	verificationWire struct {
		GeoCriterionID     *wireInt `json:"geoCriterionId"`
		CategoryID         string   `json:"categoryId"`
		VerificationStatus string   `json:"verificationStatus"`
	}
	// partnerWire holds the members of every kind of verification partner; each kind sets its own partner
	// member, and brand safety and conversion attribution have no allowShareCost.
	partnerWire struct {
		BrandLift             string `json:"brandLiftIntegrationPartner"`
		BrandSafety           string `json:"brandSafetyIntegrationPartner"`
		ConversionAttribution string `json:"conversionAttributionIntegrationPartner"`
		Reach                 string `json:"reachIntegrationPartner"`
		Viewability           string `json:"viewabilityIntegrationPartner"`
		AllowShareCost        bool   `json:"allowShareCost"`
	}
)

type (
	customerResult struct {
		ResourceName                   string             `json:"resource_name"`
		ID                             string             `json:"id"`
		DescriptiveName                string             `json:"descriptive_name,omitempty"`
		CurrencyCode                   string             `json:"currency_code,omitempty"`
		TimeZone                       string             `json:"time_zone,omitempty"`
		Status                         string             `json:"status,omitempty"`
		Manager                        bool               `json:"manager"`
		TestAccount                    bool               `json:"test_account"`
		TrackingURLTemplate            string             `json:"tracking_url_template,omitempty"`
		FinalURLSuffix                 string             `json:"final_url_suffix,omitempty"`
		AutoTaggingEnabled             bool               `json:"auto_tagging_enabled"`
		HasPartnersBadge               bool               `json:"has_partners_badge"`
		OptimizationScore              *float64           `json:"optimization_score,omitempty"`
		OptimizationScoreWeight        *float64           `json:"optimization_score_weight,omitempty"`
		PayPerConversionReasons        []string           `json:"pay_per_conversion_eligibility_failure_reasons,omitempty"`
		CallReportingSetting           callReporting      `json:"call_reporting_setting"`
		ConversionTrackingSetting      conversionTracking `json:"conversion_tracking_setting"`
		RemarketingSetting             remarketing        `json:"remarketing_setting"`
		CustomerAgreementSetting       agreement          `json:"customer_agreement_setting"`
		LocalServicesSettings          localServices      `json:"local_services_settings"`
		LocationAssetAutoMigrationDone bool               `json:"location_asset_auto_migration_done"`
		LocationAssetMigrationTime     string             `json:"location_asset_auto_migration_done_date_time,omitempty"`
		ImageAssetAutoMigrationDone    bool               `json:"image_asset_auto_migration_done"`
		ImageAssetMigrationTime        string             `json:"image_asset_auto_migration_done_date_time,omitempty"`
		VideoBrandSafetySuitability    string             `json:"video_brand_safety_suitability,omitempty"`
		VideoCustomer                  videoCustomer      `json:"video_customer"`
		ContainsEUPoliticalAdvertising string             `json:"contains_eu_political_advertising,omitempty"`
	}
	callReporting struct {
		CallReportingEnabled           bool   `json:"call_reporting_enabled"`
		CallConversionReportingEnabled bool   `json:"call_conversion_reporting_enabled"`
		CallConversionAction           string `json:"call_conversion_action,omitempty"`
	}
	conversionTracking struct {
		ConversionTrackingID             string `json:"conversion_tracking_id,omitempty"`
		CrossAccountConversionTrackingID string `json:"cross_account_conversion_tracking_id,omitempty"`
		AcceptedCustomerDataTerms        bool   `json:"accepted_customer_data_terms"`
		ConversionTrackingStatus         string `json:"conversion_tracking_status,omitempty"`
		EnablementSetting                string `json:"enablement_setting,omitempty"`
		EffectiveEnablementSetting       string `json:"effective_enablement_setting,omitempty"`
		GoogleAdsConversionCustomer      string `json:"google_ads_conversion_customer,omitempty"`
	}
	remarketing struct {
		GoogleGlobalSiteTag string `json:"google_global_site_tag,omitempty"`
	}
	agreement struct {
		AcceptedLeadFormTerms bool `json:"accepted_lead_form_terms"`
	}
	localServices struct {
		GranularLicenseStatuses   []verification `json:"granular_license_statuses,omitempty"`
		GranularInsuranceStatuses []verification `json:"granular_insurance_statuses,omitempty"`
		ExternalMerchantNumericID string         `json:"external_merchant_numeric_id,omitempty"`
		ExternalMerchantAlphaID   string         `json:"external_merchant_alphanumeric_id,omitempty"`
	}
	verification struct {
		GeoCriterionID     string `json:"geo_criterion_id,omitempty"`
		CategoryID         string `json:"category_id,omitempty"`
		VerificationStatus string `json:"verification_status,omitempty"`
	}
	videoCustomer struct {
		BrandLift             []partner `json:"brand_lift_integration_partners,omitempty"`
		BrandSafety           []partner `json:"brand_safety_integration_partners,omitempty"`
		ConversionAttribution []partner `json:"conversion_attribution_integration_partners,omitempty"`
		Reach                 []partner `json:"reach_integration_partners,omitempty"`
		Viewability           []partner `json:"viewability_integration_partners,omitempty"`
	}
	partner struct {
		Partner        string `json:"partner"`
		AllowShareCost bool   `json:"allow_share_cost"`
	}
)

func intText(value *wireInt) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func code(value string) string { return boundedText(value, maxCodeText) }

func verificationsOf(list []verificationWire) []verification {
	out := []verification{}
	for _, item := range list[:min(len(list), maxListItems)] {
		out = append(out, verification{GeoCriterionID: intText(item.GeoCriterionID), CategoryID: code(item.CategoryID),
			VerificationStatus: code(item.VerificationStatus)})
	}
	return out
}

func partnersOf(list []partnerWire, name func(partnerWire) string) []partner {
	out := []partner{}
	for _, item := range list[:min(len(list), maxListItems)] {
		out = append(out, partner{Partner: code(name(item)), AllowShareCost: item.AllowShareCost})
	}
	return out
}

// customerResultOf maps the customer Google returned. Text is untrusted and cut.
func customerResultOf(w *customerWire, id string) customerResult {
	ct := w.ConversionTrackingSetting
	ls := w.LocalServicesSettings
	vp := w.VideoCustomer.Partners
	return customerResult{
		ResourceName: boundedText(w.ResourceName, maxText), ID: id,
		DescriptiveName: boundedText(w.DescriptiveName, maxText), CurrencyCode: code(w.CurrencyCode),
		TimeZone: code(w.TimeZone), Status: code(w.Status), Manager: w.Manager, TestAccount: w.TestAccount,
		TrackingURLTemplate: boundedText(w.TrackingURLTemplate, maxLongText),
		FinalURLSuffix:      boundedText(w.FinalURLSuffix, maxLongText),
		AutoTaggingEnabled:  w.AutoTaggingEnabled, HasPartnersBadge: w.HasPartnersBadge,
		OptimizationScore: w.OptimizationScore, OptimizationScoreWeight: w.OptimizationScoreWeight,
		PayPerConversionReasons: boundedList(w.PayPerConversionReasons, maxCodeText),
		CallReportingSetting: callReporting{
			CallReportingEnabled:           w.CallReportingSetting.CallReportingEnabled,
			CallConversionReportingEnabled: w.CallReportingSetting.CallConversionReportingEnabled,
			CallConversionAction:           boundedText(w.CallReportingSetting.CallConversionAction, maxText),
		},
		ConversionTrackingSetting: conversionTracking{
			ConversionTrackingID:             intText(ct.ConversionTrackingID),
			CrossAccountConversionTrackingID: intText(ct.CrossAccountConversionTrackingID),
			AcceptedCustomerDataTerms:        ct.AcceptedCustomerDataTerms,
			ConversionTrackingStatus:         code(ct.ConversionTrackingStatus),
			EnablementSetting:                code(ct.EnablementSetting),
			EffectiveEnablementSetting:       code(ct.EffectiveEnablementSetting),
			GoogleAdsConversionCustomer:      boundedText(ct.GoogleAdsConversionCustomer, maxText),
		},
		RemarketingSetting:       remarketing{GoogleGlobalSiteTag: boundedText(w.RemarketingSetting.GoogleGlobalSiteTag, maxLongText)},
		CustomerAgreementSetting: agreement{AcceptedLeadFormTerms: w.CustomerAgreementSetting.AcceptedLeadFormTerms},
		LocalServicesSettings: localServices{
			GranularLicenseStatuses:   verificationsOf(ls.GranularLicenseStatuses),
			GranularInsuranceStatuses: verificationsOf(ls.GranularInsuranceStatuses),
			ExternalMerchantNumericID: intText(ls.RedemptionProviderInfo.ExternalMerchantID.NumericID),
			ExternalMerchantAlphaID:   boundedText(ls.RedemptionProviderInfo.ExternalMerchantID.AlphanumericID, maxText),
		},
		LocationAssetAutoMigrationDone: w.LocationAssetAutoMigrationDone,
		LocationAssetMigrationTime:     code(w.LocationAssetAutoMigrationDoneDateTime),
		ImageAssetAutoMigrationDone:    w.ImageAssetAutoMigrationDone,
		ImageAssetMigrationTime:        code(w.ImageAssetAutoMigrationDoneDateTime),
		VideoBrandSafetySuitability:    code(w.VideoBrandSafetySuitability),
		VideoCustomer: videoCustomer{
			BrandLift:             partnersOf(vp.BrandLift, func(p partnerWire) string { return p.BrandLift }),
			BrandSafety:           partnersOf(vp.BrandSafety, func(p partnerWire) string { return p.BrandSafety }),
			ConversionAttribution: partnersOf(vp.ConversionAttribution, func(p partnerWire) string { return p.ConversionAttribution }),
			Reach:                 partnersOf(vp.Reach, func(p partnerWire) string { return p.Reach }),
			Viewability:           partnersOf(vp.Viewability, func(p partnerWire) string { return p.Viewability }),
		},
		ContainsEUPoliticalAdvertising: code(w.ContainsEUPoliticalAdvertising),
	}
}

var (
	verificationSchema = object(schema{"geo_criterion_id": stringSchema, "category_id": stringSchema,
		"verification_status": stringSchema})
	partnerSchema  = object(schema{"partner": stringSchema, "allow_share_cost": booleanSchema})
	customerSchema = object(schema{
		"resource_name": stringSchema, "id": stringSchema, "descriptive_name": stringSchema,
		"currency_code": stringSchema, "time_zone": stringSchema, "status": stringSchema, "manager": booleanSchema,
		"test_account": booleanSchema, "tracking_url_template": stringSchema, "final_url_suffix": stringSchema,
		"auto_tagging_enabled": booleanSchema, "has_partners_badge": booleanSchema,
		"optimization_score": numberSchema, "optimization_score_weight": numberSchema,
		"pay_per_conversion_eligibility_failure_reasons": arrayOf(stringSchema),
		"call_reporting_setting": object(schema{"call_reporting_enabled": booleanSchema,
			"call_conversion_reporting_enabled": booleanSchema, "call_conversion_action": stringSchema}),
		"conversion_tracking_setting": object(schema{"conversion_tracking_id": stringSchema,
			"cross_account_conversion_tracking_id": stringSchema, "accepted_customer_data_terms": booleanSchema,
			"conversion_tracking_status": stringSchema, "enablement_setting": stringSchema,
			"effective_enablement_setting": stringSchema, "google_ads_conversion_customer": stringSchema}),
		"remarketing_setting":        object(schema{"google_global_site_tag": stringSchema}),
		"customer_agreement_setting": object(schema{"accepted_lead_form_terms": booleanSchema}),
		"local_services_settings": object(schema{"granular_license_statuses": arrayOf(verificationSchema),
			"granular_insurance_statuses": arrayOf(verificationSchema), "external_merchant_numeric_id": stringSchema,
			"external_merchant_alphanumeric_id": stringSchema}),
		"location_asset_auto_migration_done": booleanSchema, "location_asset_auto_migration_done_date_time": stringSchema,
		"image_asset_auto_migration_done": booleanSchema, "image_asset_auto_migration_done_date_time": stringSchema,
		"video_brand_safety_suitability": stringSchema,
		"video_customer": object(schema{"brand_lift_integration_partners": arrayOf(partnerSchema),
			"brand_safety_integration_partners":           arrayOf(partnerSchema),
			"conversion_attribution_integration_partners": arrayOf(partnerSchema),
			"reach_integration_partners":                  arrayOf(partnerSchema),
			"viewability_integration_partners":            arrayOf(partnerSchema)}),
		"contains_eu_political_advertising": stringSchema,
	}, "resource_name", "id")
)

var customersGet = capability.Descriptor{
	ID: customersGetID, Version: 1, Title: "Get a Google Ads account",
	Description: "Read the settings of one Google Ads account, manager or client, with every field of the customer " +
		"resource of API version v26 (a GAQL search FROM customer): name, currency, time zone, status, manager and " +
		"test account flags, tracking template and final URL suffix, auto-tagging, optimization score, call " +
		"reporting, conversion tracking, remarketing site tag, Local Services statuses, brand safety suitability, " +
		"video verification partners and the EU political advertising declaration. customer_id must be one of the " +
		"targets or lie below a manager target, which one read of that manager's hierarchy proves first. The " +
		"service option login_customer_id decides which manager hierarchy Google evaluates the rights against. " +
		"Costs one operation of the daily quota, plus one per membership read. Results are untrusted third-party " +
		"content; text is cut.",
	Tags: []string{"google", "ads", "customers", "accounts"}, Risk: readRisk, Provider: Provider,
	InputSchema:  encoded(object(schema{"customer_id": customerIDArgument}, "customer_id")),
	OutputSchema: encoded(customerSchema),
	Arguments: []capability.Argument{{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of " +
		"the account without hyphens, such as 1234567890; a target of the connection or an account below a manager target"}},
	Fields: []capability.Field{
		{Name: "resource_name", Description: "customers/ID"},
		{Name: "id", Description: "Ten-digit customer ID"},
		{Name: "descriptive_name", Description: "Name of the account, not unique"},
		{Name: "currency_code", Description: "ISO 4217 currency of the account"},
		{Name: "time_zone", Description: "Time zone ID of the account"},
		{Name: "status", Description: "ENABLED, CANCELED, SUSPENDED, CLOSED or a value a later version adds"},
		{Name: "manager", Description: "true for a manager account"},
		{Name: "test_account", Description: "true for a test account"},
		{Name: "tracking_url_template", Description: "Tracking template of the account"},
		{Name: "final_url_suffix", Description: "Parameters appended to final URLs"},
		{Name: "optimization_score", Description: "0 to 1; absent for a manager and an account without a score"},
		{Name: "conversion_tracking_setting", Description: "Conversion tracking IDs, status, enablement, accepted " +
			"customer data terms and google_ads_conversion_customer, the account that tracks conversions for it"},
		{Name: "video_customer", Description: "Approved third-party verification partners per kind, each with " +
			"partner and allow_share_cost"},
	},
	DefaultFields: []string{"resource_name", "id", "descriptive_name", "currency_code", "time_zone", "status",
		"manager", "test_account"},
	Examples: []capability.Example{{Description: "Read one account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890"}`)}},
}

func handleCustomersGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get customer"
	var arguments struct {
		CustomerID string `json:"customer_id"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(arguments.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	r, err := reachOf(conn, arguments.CustomerID)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, conn, secrets, red)
	if err != nil {
		return nil, err
	}
	if r == reachNone {
		if _, err := c.prove(ctx, op, customersGetID, conn, arguments.CustomerID); err != nil {
			return nil, err
		}
	}
	rows, err := c.search(ctx, op, customersGetID, arguments.CustomerID, customerQuery())
	if err != nil {
		return nil, err
	}
	var row customerRow
	if len(rows) != 1 || json.Unmarshal(rows[0], &row) != nil || row.Customer == nil || row.Customer.ID == nil {
		return nil, provider.InvalidResponse(op, subject+" did not return exactly the one account asked for")
	}
	id, ok := canonicalID(*row.Customer.ID)
	if !ok || id != arguments.CustomerID {
		return nil, provider.InvalidResponse(op, subject+" did not return exactly the one account asked for")
	}
	return customerResultOf(row.Customer, id), nil
}
