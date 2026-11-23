package googleads

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func TestRegisterPublishesMetadata(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatalf("ValidateProfiles() = %v", err)
	}
	metadata, found := reg.ProviderMetadata(Provider)
	if !found || metadata.DefaultBaseURL != "https://googleads.googleapis.com" ||
		metadata.DocPath() != "google.ads/setup.md" || metadata.Token == nil {
		t.Fatalf("metadata = %+v", metadata)
	}
	if !reflect.DeepEqual(metadata.SecretRoles, config.TokenSecretRoles()) {
		t.Errorf("secret roles = %+v, want the token roles", metadata.SecretRoles)
	}
	want := []string{accountBudgetProposalsListID, accountBudgetsListID, accountLinksListID, accountLinksRemoveID,
		adGroupAdsCreateID, adGroupAdsRemoveID, adGroupAdCreatedAssetsRemoveID, adGroupAdsUpdateID,
		adGroupAssetSetsCreateID, adGroupAssetSetsRemoveID, adGroupAssetsCreateID, adGroupAssetsRemoveID,
		adGroupAssetsUpdateID, adGroupBidModifiersCreateID, adGroupBidModifiersRemoveID, adGroupBidModifiersUpdateID,
		adGroupCriteriaCreateID, adGroupCriteriaRemoveID, adGroupCriteriaUpdateID,
		adGroupsCreateID,
		adGroupsRemoveID, adGroupsUpdateID, adParametersCreateID, adParametersRemoveID, adParametersUpdateID, adsUpdateID,
		assetSetAssetsCreateID, assetSetAssetsRemoveID,
		assetSetsCreateID, assetSetsRemoveID, assetSetsUpdateID, assetsCreateID,
		assetsUpdateID, audiencesCreateID, audiencesUpdateID, biddingDataExclusionsCreateID, biddingDataExclusionsRemoveID,
		biddingDataExclusionsUpdateID, seasonalityCreateID, seasonalityRemoveID, seasonalityUpdateID,
		biddingStrategiesCreateID, biddingStrategiesRemoveID,
		biddingStrategiesUpdateID, billingSetupsListID,
		campaignAssetSetsCreateID, campaignAssetSetsRemoveID, campaignAssetsCreateID, campaignAssetsRemoveID,
		campaignAssetsUpdateID, campaignAutomaticallyCreatedAssetsRemoveID, campaignBidModifiersCreateID, campaignBidModifiersRemoveID, campaignBidModifiersUpdateID,
		campaignBudgetsCreateID, campaignBudgetsRemoveID,
		campaignBudgetsUpdateID, campaignCriteriaCreateID, campaignCriteriaRemoveID,
		campaignCriteriaUpdateID, campaignGroupsCreateID, campaignGroupsRemoveID,
		campaignGroupsUpdateID, campaignSharedSetsCreateID, campaignSharedSetsRemoveID, campaignsCreateID, campaignsEnablePMaxBrandGuidelinesID,
		campaignsRemoveID, campaignsUpdateID, conversionActionsCreateID, conversionActionsRemoveID,
		conversionActionsUpdateID, conversionCustomVariablesCreateID, conversionCustomVariablesUpdateID,
		customAudiencesCreateID, customAudiencesRemoveID, customAudiencesUpdateID, customInterestsCreateID,
		customInterestsUpdateID, customerAssetSetsCreateID, customerAssetSetsRemoveID, customerBudgetNetworkSettingsCreateID, customerBudgetNetworkSettingsUpdateID,
		customerClientLinksListID, customerClientLinksUpdateID, customerClientsListID, customerManagerLinksListID,
		customerManagerLinksUpdateID, customerUserAccessInvitationsListID, customerUserAccessInvitationsRemoveID,
		customerUserAccessesListID, customerUserAccessesRemoveID, customerUserAccessesUpdateID, customersGetID,
		customersListAccessibleID, customersUpdateID, dataLinksListID, dataLinksRemoveID, dataLinksUpdateID, fieldsGetID, fieldsSearchID,
		incentivesFetchID, invoicesListID, labelsCreateID, labelsRemoveID, labelsUpdateID,
		multiPartyAuthReviewsListID, multiPartyAuthReviewsResolveID, paymentsAccountsListID,
		productLinkInvitationsListID, productLinkInvitationsUpdateID, productLinksListID, productLinksRemoveID,
		recommendationsApplyID, recommendationsDismissID,
		recommendationsGenerateID, remarketingActionsCreateID, remarketingActionsUpdateID,
		reportsSearchID, sharedSetsCreateID, sharedSetsRemoveID, sharedSetsUpdateID, thirdPartyAppAnalyticsLinksListID, userListCustomerTypesCreateID,
		userListCustomerTypesRemoveID, userListsCreateID, userListsRemoveID, userListsUpdateID}
	if len(metadata.Tools) != len(want) {
		t.Fatalf("tools = %+v", metadata.Tools)
	}
	for i, tool := range metadata.Tools {
		// recommendations.apply is listed-only but tested in recommendations_test.go, not with changeToolIDs.
		listed := slices.Contains(changeToolIDs, tool.ID) || slices.Contains(assetToolIDs, tool.ID) ||
			tool.ID == recommendationsApplyID || tool.ID == campaignGroupsRemoveID || tool.ID == sharedSetsRemoveID ||
			tool.ID == campaignSharedSetsRemoveID
		changing := listed || slices.Contains(notListedOnly, tool.ID) || tool.ID == labelsCreateID ||
			tool.ID == labelsUpdateID || tool.ID == campaignGroupsCreateID || tool.ID == campaignGroupsUpdateID ||
			tool.ID == campaignAssetsCreateID || tool.ID == campaignAssetsUpdateID || tool.ID == campaignAssetsRemoveID ||
			tool.ID == assetSetsCreateID || tool.ID == assetSetsUpdateID ||
			tool.ID == adGroupAssetsCreateID || tool.ID == adGroupAssetsUpdateID || tool.ID == adGroupAssetsRemoveID ||
			slices.Contains(setLinkToolIDs, tool.ID) ||
			tool.ID == sharedSetsCreateID || tool.ID == sharedSetsUpdateID || tool.ID == campaignSharedSetsCreateID
		if tool.ID != want[i] || (tool.Effect == config.EffectRead) == changing || tool.RequiresToolAllowList != listed {
			t.Errorf("tool %d = %+v", i, tool)
		}
		if scopes := metadata.Token.Scopes[tool.ID]; !reflect.DeepEqual(scopes, []string{"https://www.googleapis.com/auth/adwords"}) {
			t.Errorf("scopes of %s = %v", tool.ID, scopes)
		}
	}
	if len(metadata.Profiles) != 1 || metadata.Profiles[0].ID != "read" || !metadata.Profiles[0].Recommended ||
		!reflect.DeepEqual(metadata.Profiles[0].Tools, readTools) {
		t.Errorf("profiles = %+v", metadata.Profiles)
	}
	if err := metadata.ValidateBaseURL("https://googleads.googleapis.com/"); err != nil {
		t.Errorf("official origin refused: %v", err)
	}
	for _, other := range []string{"https://example.invalid", "http://googleads.googleapis.com",
		"https://googleads.googleapis.com/v26", "https://googleads.googleapis.com:8443",
		"https://googleads.googleapis.com.example.invalid"} {
		if err := metadata.ValidateBaseURL(other); err == nil {
			t.Errorf("base URL %q accepted", other)
		}
	}
	kinds := metadata.Target.Kinds
	if len(kinds) != 2 || kinds[0].Name != kindManager || !kinds[0].Nested || kinds[1].Name != kindCustomer ||
		!kinds[1].Top || !reflect.DeepEqual(kinds[1].Parents, []string{kindManager}) {
		t.Errorf("target kinds = %+v", kinds)
	}
}

func TestTargets(t *testing.T) {
	for raw, kind := range map[string]string{"managers/" + managerID: kindManager, "customers/" + clientID: kindCustomer} {
		if got, _, err := parseTarget(raw); err != nil || got != kind {
			t.Errorf("%q = %q, %v", raw, got, err)
		}
	}
	for _, bad := range []string{"customers/123-456-7890", "customers/123456789", "customers/12345678901",
		"managers/", "accounts/" + managerID, "customers/" + clientID + "/x", "customers/*", clientID,
		"customers/ABCDEFGHIJ"} {
		if _, _, err := parseTarget(bad); err == nil {
			t.Errorf("%q accepted", bad)
		} else if strings.Contains(err.Error(), "ABCDEFGHIJ") || strings.Contains(err.Error(), "123-456") {
			t.Errorf("error %q quotes the value", err)
		}
	}
	for _, set := range [][]string{
		{"customers/" + clientID, "customers/" + clientID},
		{"managers/" + managerID, "customers/" + managerID},
	} {
		if _, err := parseBinding(set); err == nil {
			t.Errorf("%v accepted", set)
		}
	}
	many := make([]string, maxTargets+1)
	for i := range many {
		many[i] = fmt.Sprintf("customers/%d", 2000000000+i)
	}
	if _, err := parseBinding(many); err == nil {
		t.Error("101 targets accepted")
	}
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	cfg := config.New(reg)
	cfg.Services = map[string]config.Service{"ads": {Provider: Provider}}
	cfg.Credentials = map[string]config.Credential{credential: {Provider: Provider, Type: config.CredentialTypeKeyring}}
	cfg.Connections = map[string]config.Connection{"work": {Service: "ads", Credential: credential,
		Effects: []config.Effect{config.EffectRead}, Targets: []string{"accounts/1"}}}
	if err := cfg.Validate(); err == nil {
		t.Error("a connection with a foreign target form is valid")
	}
}

// What is wrong with a connection ends before a secret is read and before any request.
func TestConnectionIsCheckedBeforeSecretAndIO(t *testing.T) {
	cases := []struct {
		name   string
		change func(*config.Resolved)
	}{
		{"foreign base URL", func(r *config.Resolved) { r.BaseURL = "https://example.invalid" }},
		{"unknown option", func(r *config.Resolved) { r.Options = map[string]string{"login-customer-id": loginID} }},
		{"login with hyphens", func(r *config.Resolved) { r.Options = map[string]string{optionLoginCustomerID: "100-000-0000"} }},
		{"login too short", func(r *config.Resolved) { r.Options = map[string]string{optionLoginCustomerID: "123"} }},
		{"exclusions", func(r *config.Resolved) { r.Exclude = []string{"customers/" + clientID} }},
		{"bad target", func(r *config.Resolved) { r.Targets = []string{"accounts/1"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			resolved := e.resolved()
			c.change(resolved)
			for _, handler := range []capability.Handler{handleCustomersListAccessible, handleCustomersGet,
				handleCustomerClientsList, handleReportsSearch, handleFieldsSearch, handleFieldsGet} {
				_, err := handler(context.Background(), resolved, e.secrets(), e.red,
					json.RawMessage(`{"customer_id":"`+clientID+`"}`))
				if classOf(err) != provider.ClassProviderError {
					t.Errorf("err = %v, want provider-error", err)
				}
				if err != nil && (strings.Contains(err.Error(), "example.invalid") || strings.Contains(err.Error(), "100-000")) {
					t.Errorf("error %q quotes a value", err)
				}
			}
			class, err := TestConnection(context.Background(), resolved, e.secrets(), e.red)
			if err != nil || class != provider.ClassProviderError {
				t.Errorf("connection test = %s, %v", class, err)
			}
			if e.lookups != 0 || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
				t.Errorf("lookups=%d requests=%d tokens=%d, want none", e.lookups, len(e.fake.api()), e.fake.tokenRequests())
			}
		})
	}
}

func TestConnectionTest(t *testing.T) {
	accessible := okReply(`{"resourceNames":["customers/` + managerID + `","customers/` + clientID + `"]}`)
	t.Run("lists the accessible accounts once, without login-customer-id or developer token", func(t *testing.T) {
		e := newEnvironment(t, []string{"managers/" + managerID}, map[string]string{optionLoginCustomerID: loginID})
		e.fake.routes["GET "+accessiblePath] = accessible
		class, err := e.testConnection()
		if err != nil || class != provider.ClassOK {
			t.Fatalf("class=%s err=%v", class, err)
		}
		requests := e.fake.api()
		if len(requests) != 1 || requests[0].method != http.MethodGet ||
			requests[0].url != "https://googleads.googleapis.com/v26/customers:listAccessibleCustomers" ||
			requests[0].login != "" || requests[0].developerToken != "" || requests[0].auth != "Bearer synthetic-access-1" {
			t.Errorf("requests = %+v", requests)
		}
	})
	failures := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"test access only", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION", "")}, provider.ClassPermission},
		{"not an ads user", reply{status: 401, body: adsFailure(401, "UNAUTHENTICATED", "authenticationError",
			"NOT_ADS_USER", "")}, provider.ClassAuth},
		{"api disabled", reply{status: 403, body: googleError(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission},
		{"scope", reply{status: 403, body: googleError(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")}, provider.ClassScopeMissing},
		{"rate", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_EXHAUSTED", "")}, provider.ClassRateLimited},
		{"unknown name form", okReply(`{"resourceNames":["accounts/1"]}`), provider.ClassInvalidResponse},
		{"not json", reply{status: 200, body: "<html>", header: http.Header{"Content-Type": {"text/html"}}}, provider.ClassInvalidResponse},
	}
	for _, c := range failures {
		t.Run(c.name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["GET "+accessiblePath] = c.reply
			class, err := e.testConnection()
			if err != nil || class != c.want {
				t.Fatalf("class=%s err=%v, want %s", class, err, c.want)
			}
		})
	}
	t.Run("no consent yet", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		delete(e.roles, config.RoleRefreshToken)
		if class, err := e.testConnection(); err != nil || class != provider.ClassConsentRequired || len(e.fake.api()) != 0 {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("expired consent", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.tokenReply = &reply{status: 400, body: `{"error":"invalid_grant"}`}
		if class, err := e.testConnection(); err != nil || class != provider.ClassConsentExpired {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
	t.Run("missing scope in the consent", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.tokenReply = &reply{status: 200, body: `{"access_token":"synthetic-access-x","token_type":"Bearer",` +
			`"expires_in":3600,"scope":"openid"}`}
		if class, err := e.testConnection(); err != nil || class != provider.ClassScopeMissing || len(e.fake.api()) != 0 {
			t.Fatalf("class=%s err=%v", class, err)
		}
	})
}

func TestCustomersListAccessible(t *testing.T) {
	answer := okReply(`{"resourceNames":["customers/` + managerID + `","customers/` + clientID + `","customers/` +
		otherID + `"]}`)
	t.Run("without targets every account", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["GET "+accessiblePath] = answer
		out, err := e.invoke(customersListAccessibleID, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		customers, _ := decode(t, out)["customers"].([]any)
		if len(customers) != 3 || customers[0].(map[string]any)["id"] != managerID ||
			customers[0].(map[string]any)["resource_name"] != "customers/"+managerID {
			t.Errorf("result = %s", out)
		}
	})
	t.Run("with targets only named accounts", func(t *testing.T) {
		e := newEnvironment(t, []string{"managers/" + managerID, "customers/" + clientID}, nil)
		e.fake.routes["GET "+accessiblePath] = answer
		out, err := e.invoke(customersListAccessibleID, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, otherID) || !strings.Contains(out, managerID) || !strings.Contains(out, clientID) {
			t.Errorf("result = %s", out)
		}
	})
	t.Run("an undeclared argument is refused before any request", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		if _, err := e.invoke(customersListAccessibleID, `{"login_customer_id":"`+loginID+`"}`); err == nil ||
			len(e.fake.api()) != 0 || e.lookups != 0 {
			t.Errorf("err=%v requests=%d lookups=%d", err, len(e.fake.api()), e.lookups)
		}
	})
}

// customerAnswer is a full customer row with every field of the resource.
const customerAnswer = `{"results":[{"customer":{"resourceName":"customers/` + clientID + `","id":"` + clientID + `",` +
	`"descriptiveName":"Shop\u0007 Berlin","currencyCode":"EUR","timeZone":"Europe/Berlin",` +
	`"trackingUrlTemplate":"https://track.example.com/?u={lpurl}","finalUrlSuffix":"src=ads","autoTaggingEnabled":true,` +
	`"hasPartnersBadge":false,"manager":false,"testAccount":true,"status":"ENABLED","optimizationScore":0.82,` +
	`"optimizationScoreWeight":12.5,"payPerConversionEligibilityFailureReasons":["NOT_ENOUGH_CONVERSIONS"],` +
	`"callReportingSetting":{"callReportingEnabled":true,"callConversionAction":"customers/` + clientID + `/conversionActions/4"},` +
	`"conversionTrackingSetting":{"conversionTrackingId":"55","crossAccountConversionTrackingId":66,` +
	`"acceptedCustomerDataTerms":true,"conversionTrackingStatus":"CONVERSION_TRACKING_MANAGED_BY_SELF",` +
	`"enablementSetting":"ON","effectiveEnablementSetting":"ON","googleAdsConversionCustomer":"customers/` + managerID + `"},` +
	`"remarketingSetting":{"googleGlobalSiteTag":"<script>tag</script>"},` +
	`"customerAgreementSetting":{"acceptedLeadFormTerms":true},` +
	`"localServicesSettings":{"granularLicenseStatuses":[{"geoCriterionId":"2276","categoryId":"xcat:plumber",` +
	`"verificationStatus":"PASSED"}],"granularInsuranceStatuses":[{"geoCriterionId":"2276","verificationStatus":"PENDING"}],` +
	`"redemptionProviderInfo":{"externalMerchantId":{"numericId":"77"}}},` +
	`"locationAssetAutoMigrationDone":true,"locationAssetAutoMigrationDoneDateTime":"2026-01-02 03:04:05",` +
	`"imageAssetAutoMigrationDone":true,"imageAssetAutoMigrationDoneDateTime":"2026-01-02 03:04:06",` +
	`"videoBrandSafetySuitability":"STANDARD_INVENTORY","videoCustomer":{"thirdPartyIntegrationPartners":{` +
	`"brandLiftIntegrationPartners":[{"brandLiftIntegrationPartner":"KANTAR_MILLWARD_BROWN","allowShareCost":true}],` +
	`"brandSafetyIntegrationPartners":[{"brandSafetyIntegrationPartner":"DOUBLE_VERIFY"}],` +
	`"conversionAttributionIntegrationPartners":[{"conversionAttributionIntegrationPartner":"KANTAR"}],` +
	`"reachIntegrationPartners":[{"reachIntegrationPartner":"NIELSEN","allowShareCost":true}],` +
	`"viewabilityIntegrationPartners":[{"viewabilityIntegrationPartner":"MOAT"}]}},` +
	`"containsEuPoliticalAdvertising":"DOES_NOT_CONTAIN_EU_POLITICAL_ADVERTISING"}}],"fieldMask":"customer.id"}`

func TestCustomersGet(t *testing.T) {
	get := `{"customer_id":"` + clientID + `"}`
	t.Run("a customer target is read directly with every field", func(t *testing.T) {
		e := newEnvironment(t, []string{"customers/" + clientID}, map[string]string{optionLoginCustomerID: loginID})
		e.fake.routes["POST "+searchPath(clientID)] = okReply(customerAnswer)
		out, err := e.invoke(customersGetID, get)
		if err != nil {
			t.Fatal(err)
		}
		requests := e.fake.api()
		if len(requests) != 1 || requests[0].method != http.MethodPost || requests[0].login != loginID ||
			requests[0].developerToken != "" || !strings.HasSuffix(requests[0].query, " FROM customer LIMIT 2") {
			t.Fatalf("requests = %+v", requests)
		}
		for _, field := range customerFields {
			if !strings.Contains(requests[0].query, "customer."+field) {
				t.Errorf("the query lacks customer.%s", field)
			}
		}
		got := decode(t, out)
		ct, _ := got["conversion_tracking_setting"].(map[string]any)
		ls, _ := got["local_services_settings"].(map[string]any)
		video, _ := got["video_customer"].(map[string]any)
		if got["id"] != clientID || got["descriptive_name"] != "Shop  Berlin" || got["test_account"] != true ||
			got["optimization_score"] != 0.82 || got["tracking_url_template"] != "https://track.example.com/?u={lpurl}" ||
			ct["cross_account_conversion_tracking_id"] != "66" || ct["google_ads_conversion_customer"] != "customers/"+managerID ||
			ls["external_merchant_numeric_id"] != "77" || len(ls["granular_license_statuses"].([]any)) != 1 ||
			len(video["reach_integration_partners"].([]any)) != 1 ||
			got["contains_eu_political_advertising"] != "DOES_NOT_CONTAIN_EU_POLITICAL_ADVERTISING" {
			t.Errorf("result = %s", out)
		}
	})
	t.Run("without login_customer_id no header is sent", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = okReply(customerAnswer)
		if _, err := e.invoke(customersGetID, get); err != nil {
			t.Fatal(err)
		}
		if requests := e.fake.api(); len(requests) != 1 || requests[0].login != "" {
			t.Errorf("requests = %+v", requests)
		}
	})
	t.Run("another account in the answer is invalid", func(t *testing.T) {
		for _, body := range []string{strings.ReplaceAll(customerAnswer, `"id":"`+clientID, `"id":"`+otherID),
			`{"results":[]}`, `{"results":[{"customer":{"id":"x"}}]}`} {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = okReply(body)
			if _, err := e.invoke(customersGetID, get); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("body %.40s: err = %v", body, err)
			}
		}
	})
	t.Run("an account below a manager target is proved first", func(t *testing.T) {
		e := newEnvironment(t, []string{"managers/" + otherID, "managers/" + managerID}, map[string]string{optionLoginCustomerID: loginID})
		e.fake.routes["POST "+searchPath(managerID)] = rows()
		e.fake.routes["POST "+searchPath(otherID)] = rows(proofItem(clientID))
		e.fake.routes["POST "+searchPath(clientID)] = okReply(customerAnswer)
		if _, err := e.invoke(customersGetID, get); err != nil {
			t.Fatal(err)
		}
		requests := e.fake.api()
		if len(requests) != 3 || requests[0].path != searchPath(managerID) || requests[1].path != searchPath(otherID) ||
			requests[2].path != searchPath(clientID) {
			t.Fatalf("requests = %+v", requests)
		}
		want := "SELECT customer_client.id FROM customer_client WHERE customer_client.id = " + clientID + " LIMIT 2"
		if requests[0].query != want || requests[0].login != loginID {
			t.Errorf("membership read = %+v, want query %q under the login header", requests[0], want)
		}
	})
}

// A target check refuses before the secret and before any request when the targets alone decide, and refuses
// without reading the account when the membership read does not prove it.
func TestTargetCheckIsFailClosed(t *testing.T) {
	for _, tool := range []string{customersGetID, customerClientsListID} {
		t.Run(tool+" outside customer targets", func(t *testing.T) {
			e := newEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invoke(tool, `{"customer_id":"`+otherID+`"}`)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "not available to this connection") ||
				strings.Contains(err.Error(), otherID) {
				t.Errorf("err = %v", err)
			}
			if e.lookups != 0 || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
				t.Errorf("lookups=%d requests=%d tokens=%d, want none", e.lookups, len(e.fake.api()), e.fake.tokenRequests())
			}
		})
		t.Run(tool+" malformed customer_id", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			for _, id := range []string{"123-456-7890", "123456789", "12345678901", "", "1234567890 OR 1=1"} {
				if _, err := e.invoke(tool, `{"customer_id":"`+id+`"}`); err == nil {
					t.Errorf("customer_id %q accepted", id)
				}
			}
			if e.lookups != 0 || len(e.fake.api()) != 0 {
				t.Errorf("lookups=%d requests=%d", e.lookups, len(e.fake.api()))
			}
		})
	}
	membership := []struct {
		name   string
		answer reply
		class  provider.Class
	}{
		{"no row", rows(), ""},
		{"another account", rows(proofItem(otherID)), provider.ClassInvalidResponse},
		{"two rows", rows(proofItem(clientID), proofItem(clientID)), provider.ClassInvalidResponse},
		{"unreadable row", rows(`{"customerClient":{"id":"abc"}}`), provider.ClassInvalidResponse},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"server error", reply{status: 500, body: googleError(500, "INTERNAL", "")}, provider.ClassProviderError},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout},
	}
	for _, c := range membership {
		t.Run("membership "+c.name, func(t *testing.T) {
			e := newEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.routes["POST "+searchPath(managerID)] = c.answer
			e.fake.routes["POST "+searchPath(clientID)] = okReply(customerAnswer)
			_, err := e.invoke(customersGetID, `{"customer_id":"`+clientID+`"}`)
			switch {
			case err == nil:
				t.Fatal("the call was accepted")
			case c.class == "" && !isInvalidRequest(err):
				t.Errorf("err = %v, want the refusal outside the targets", err)
			case c.class != "" && (classOf(err) != c.class || !strings.Contains(err.Error(), "membership")):
				t.Errorf("err = %v, want %s naming the membership check", err, c.class)
			}
			for _, request := range e.fake.api() {
				if request.path == searchPath(clientID) {
					t.Error("the account was read without a proved membership")
				}
			}
			if strings.Contains(err.Error(), detailCanary) {
				t.Errorf("error %q carries provider text", err)
			}
		})
	}
}

func TestCustomerClientsList(t *testing.T) {
	t.Run("filters become GAQL conditions", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(managerID)] = rows()
		_, err := e.invoke(customerClientsListID, `{"customer_id":"`+managerID+`","min_level":1,"max_level":2,`+
			`"statuses":["ENABLED","SUSPENDED"],"manager":false,"hidden":true,"test_account":false,`+
			`"client_ids":["`+clientID+`"],"currency_code":"EUR","time_zone":"Europe/Berlin",`+
			`"name_contains":"50%_[x]","applied_labels":["customers/`+managerID+`/labels/7"],"page_size":10}`)
		if err != nil {
			t.Fatal(err)
		}
		want := "SELECT customer_client.resource_name, customer_client.client_customer, customer_client.id, " +
			"customer_client.descriptive_name, customer_client.currency_code, customer_client.time_zone, " +
			"customer_client.level, customer_client.manager, customer_client.hidden, customer_client.test_account, " +
			"customer_client.status, customer_client.applied_labels FROM customer_client WHERE " +
			"customer_client.level >= 1 AND customer_client.level <= 2 AND customer_client.status IN ('ENABLED', " +
			"'SUSPENDED') AND customer_client.manager = FALSE AND customer_client.hidden = TRUE AND " +
			"customer_client.test_account = FALSE AND customer_client.id IN (" + clientID + ") AND " +
			"customer_client.currency_code = 'EUR' AND customer_client.time_zone = 'Europe/Berlin' AND " +
			"customer_client.descriptive_name LIKE '%50[%][_][[]x[]]%' AND customer_client.applied_labels " +
			"CONTAINS ANY ('customers/" + managerID + "/labels/7') ORDER BY customer_client.id LIMIT 11"
		if requests := e.fake.api(); len(requests) != 1 || requests[0].query != want {
			t.Errorf("query = %q\nwant    %q", requests[0].query, want)
		}
	})
	t.Run("quotes and backslashes in a string are refused before secret and request", func(t *testing.T) {
		for _, value := range []string{`O'Brien`, `say "hi"`, `back\slash`} {
			e := newEnvironment(t, nil, nil)
			arguments, _ := json.Marshal(map[string]any{"customer_id": managerID, "name_contains": value})
			if _, err := e.invoke(customerClientsListID, string(arguments)); err == nil {
				t.Errorf("name_contains %q accepted", value)
			}
			// The rule of the package also holds without the input schema in front of it.
			for _, argument := range []string{"name_contains", "time_zone", "currency_code"} {
				raw, _ := json.Marshal(map[string]any{"customer_id": managerID, argument: value})
				_, err := handleCustomerClientsList(context.Background(), e.resolved(), e.secrets(), e.red, raw)
				if !isInvalidRequest(err) || strings.Contains(err.Error(), value) {
					t.Errorf("%s %q: err = %v, want invalid-request without the value", argument, value, err)
				}
			}
			raw, _ := json.Marshal(map[string]any{"customer_id": managerID, "applied_labels": []string{value}})
			if _, err := handleCustomerClientsList(context.Background(), e.resolved(), e.secrets(), e.red, raw); !isInvalidRequest(err) {
				t.Errorf("applied_labels %q: err = %v", value, err)
			}
			if e.lookups != 0 || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
				t.Errorf("lookups=%d requests=%d tokens=%d, want none", e.lookups, len(e.fake.api()), e.fake.tokenRequests())
			}
		}
	})
	t.Run("LIKE wildcards are matched literally", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(managerID)] = rows()
		if _, err := e.invoke(customerClientsListID, `{"customer_id":"`+managerID+`","name_contains":"a%b_c[d]"}`); err != nil {
			t.Fatal(err)
		}
		if requests := e.fake.api(); len(requests) != 1 ||
			!strings.Contains(requests[0].query, `descriptive_name LIKE '%a[%]b[_]c[[]d[]]%'`) {
			t.Errorf("requests = %+v", requests)
		}
	})
	t.Run("pages with a cursor bound to the arguments", func(t *testing.T) {
		e := newEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.answer = func(s seen) *reply {
			if strings.Contains(s.query, "customer_client.id > "+subManager) {
				r := rows(clientItem(managerID, clientID, 2, false))
				return &r
			}
			r := rows(clientItem(managerID, managerID, 0, true), clientItem(managerID, subManager, 1, true),
				clientItem(managerID, clientID, 2, false))
			return &r
		}
		out, err := e.invoke(customerClientsListID, `{"customer_id":"`+managerID+`","page_size":2}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		clients, _ := got["customer_clients"].([]any)
		cursor, _ := got["next_cursor"].(string)
		if len(clients) != 2 || got["has_more"] != true || cursor == "" {
			t.Fatalf("result = %s", out)
		}
		first := clients[0].(map[string]any)
		if first["id"] != managerID || first["resource_name"] != "customers/"+managerID+"/customerClients/"+managerID ||
			first["manager"] != true || first["applied_labels"] == nil || first["currency_code"] != "EUR" {
			t.Errorf("first row = %v", first)
		}
		out, err = e.invoke(customerClientsListID, `{"customer_id":"`+managerID+`","page_size":2,"cursor":"`+cursor+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		got = decode(t, out)
		if clients, _ := got["customer_clients"].([]any); len(clients) != 1 || got["has_more"] == true {
			t.Errorf("second page = %s", out)
		}
		requests := e.fake.api()
		if len(requests) != 2 || !strings.Contains(requests[1].query, "AND customer_client.id > "+subManager+" ORDER BY") &&
			!strings.Contains(requests[1].query, "WHERE customer_client.id > "+subManager+" ORDER BY") {
			t.Errorf("requests = %+v", requests)
		}
		// The cursor does not fit other arguments; that ends before any request.
		before := len(e.fake.api())
		if _, err := e.invoke(customerClientsListID, `{"customer_id":"`+managerID+`","max_level":1,"cursor":"`+cursor+`"}`); err == nil {
			t.Error("a cursor of other arguments was accepted")
		}
		if len(e.fake.api()) != before {
			t.Error("a foreign cursor reached Google")
		}
	})
	t.Run("a customer target releases only named rows", func(t *testing.T) {
		e := newEnvironment(t, []string{"customers/" + managerID, "customers/" + clientID}, nil)
		e.fake.routes["POST "+searchPath(managerID)] = rows(clientItem(managerID, managerID, 0, true),
			clientItem(managerID, subManager, 1, true), clientItem(managerID, clientID, 2, false))
		out, err := e.invoke(customerClientsListID, `{"customer_id":"`+managerID+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, subManager) || !strings.Contains(out, `"id":"`+clientID) {
			t.Errorf("result = %s", out)
		}
	})
	t.Run("below a manager target every row after the proof", func(t *testing.T) {
		e := newEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.answer = func(s seen) *reply {
			if s.path == searchPath(managerID) {
				r := rows(proofItem(subManager))
				return &r
			}
			r := rows(clientItem(subManager, subManager, 0, true), clientItem(subManager, clientID, 1, false))
			return &r
		}
		out, err := e.invoke(customerClientsListID, `{"customer_id":"`+subManager+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if clients, _ := decode(t, out)["customer_clients"].([]any); len(clients) != 2 {
			t.Errorf("result = %s", out)
		}
	})
	t.Run("rows that do not fit the queried account are invalid", func(t *testing.T) {
		for _, item := range []string{clientItem(otherID, clientID, 1, false),
			strings.Replace(clientItem(managerID, clientID, 1, false), `"clientCustomer":"customers/`+clientID, `"clientCustomer":"customers/`+otherID, 1),
			strings.Replace(clientItem(managerID, clientID, 1, false), `"level":"1"`, `"level":"-1"`, 1),
			`{"customerClient":{"descriptiveName":"no id"}}`} {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(managerID)] = rows(item)
			if _, err := e.invoke(customerClientsListID, `{"customer_id":"`+managerID+`"}`); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("row %.60s: err = %v", item, err)
			}
		}
	})
	t.Run("rows out of order are invalid", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(managerID)] = rows(clientItem(managerID, clientID, 1, false),
			clientItem(managerID, subManager, 1, false))
		if _, err := e.invoke(customerClientsListID, `{"customer_id":"`+managerID+`"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("invalid filters are refused before any request", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		for _, args := range []string{`"statuses":["REMOVED"]`, `"max_level":101`, `"min_level":-1`,
			`"currency_code":"eur"`, `"time_zone":"Europe/Berlin'"`, `"name_contains":"a\nb"`, `"name_contains":""`,
			`"applied_labels":["labels/7"]`, `"client_ids":["123"]`, `"page_size":1001`, `"cursor":"forged"`,
			`"query":"SELECT customer.id FROM customer"`} {
			if _, err := e.invoke(customerClientsListID, `{"customer_id":"`+managerID+`",`+args+`}`); err == nil {
				t.Errorf("%s accepted", args)
			}
		}
		if len(e.fake.api()) != 0 || e.lookups != 0 {
			t.Errorf("requests=%d lookups=%d", len(e.fake.api()), e.lookups)
		}
	})
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"test access only", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION", "")}, provider.ClassPermission, "test access"},
		{"user permission denied", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID},
		{"login and account do not fit", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"INVALID_LOGIN_CUSTOMER_ID_SERVING_CUSTOMER_ID_COMBINATION", "")}, provider.ClassPermission, optionLoginCustomerID},
		{"other authorization code", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"ACTION_NOT_PERMITTED", "")}, provider.ClassPermission, "authorizationError ACTION_NOT_PERMITTED"},
		{"customer not found", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "authenticationError",
			"CUSTOMER_NOT_FOUND", "")}, provider.ClassNotFound, "no account"},
		{"expired token", reply{status: 401, body: adsFailure(401, "UNAUTHENTICATED", "authenticationError",
			"OAUTH_TOKEN_EXPIRED", "")}, provider.ClassAuth, "access token"},
		{"bad login header", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "headerError",
			"INVALID_LOGIN_CUSTOMER_ID", "")}, provider.ClassProviderError, optionLoginCustomerID},
		{"not allowlisted", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "notAllowlistedError",
			"CUSTOMER_NOT_ALLOWLISTED_FOR_THIS_FEATURE", "")}, provider.ClassPermission, "allowlisted"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"rateScope":"ACCOUNT","rateName":"x","retryDelay":"30s"}`)},
			provider.ClassRateLimited, "too many requests"},
		{"query consumption", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"EXCESSIVE_SHORT_TERM_QUERY_RESOURCE_CONSUMPTION", "")}, provider.ClassRateLimited, "resource-intensive"},
		{"query error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "queryError",
			"UNRECOGNIZED_FIELD", "")}, provider.ClassInvalidResponse, "API may have changed"},
		{"too large without failure", reply{status: 429, body: googleError(429, "RESOURCE_EXHAUSTED", "")},
			provider.ClassRateLimited, "rate-limiting"},
		{"disabled API", reply{status: 403, body: googleError(403, "PERMISSION_DENIED", "SERVICE_DISABLED")},
			provider.ClassPermission, "googleads.googleapis.com"},
		{"scope", reply{status: 403, body: googleError(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")},
			provider.ClassScopeMissing, "scope"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, ""},
		{"unknown 4xx", reply{status: 409, body: `{}`}, provider.ClassInvalidResponse, "does not know"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = c.reply
			_, err := e.invoke(customersGetID, `{"customer_id":"`+clientID+`"}`)
			if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) {
				t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
			}
			if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "synthetic-access") {
				t.Errorf("error %q carries provider text or a token", err)
			}
			// Only a 401 renews the token and repeats the read, once.
			want := 1
			if c.reply.status == http.StatusUnauthorized {
				want = 2
			}
			if got := len(e.fake.api()); got != want {
				t.Errorf("requests = %d, want %d", got, want)
			}
		})
	}
	t.Run("the original carries the codes and the request ID, never the trigger", func(t *testing.T) {
		failed, hold := adsError("op", 429, []byte(adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"12.5s"}`)), false)
		if hold != 12500*time.Millisecond || failed.Original == nil || failed.Original.Status != 429 {
			t.Fatalf("hold=%v failed=%+v", hold, failed)
		}
		entries := failed.Original.Entries
		if len(entries) != 3 || entries[0].Code != "RESOURCE_EXHAUSTED" || entries[1].Type != "quotaError" ||
			entries[1].Code != "RESOURCE_TEMPORARILY_EXHAUSTED" || entries[1].Path != "query" ||
			entries[1].Message != detailCanary || entries[2].Type != "requestId" || entries[2].Code != "synthetic-request-1" {
			t.Errorf("entries = %+v", entries)
		}
		for _, entry := range entries {
			if strings.Contains(entry.Message+entry.Code+entry.Path, triggerCanary) {
				t.Errorf("entry %+v carries the trigger", entry)
			}
		}
	})
}

func TestRateLimitHoldsTheNextRequestAndNeverRepeats(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED",
		"quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}
	_, err := e.invoke(customersGetID, `{"customer_id":"`+clientID+`"}`)
	if classOf(err) != provider.ClassRateLimited || len(e.fake.api()) != 1 {
		t.Fatalf("err = %v, requests = %d", err, len(e.fake.api()))
	}
	_, _ = e.invoke(customersGetID, `{"customer_id":"`+clientID+`"}`)
	if len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] < 29*time.Second {
		t.Errorf("sleeps = %v, want the retry delay Google asked for", e.sleeps)
	}
	e = newEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = reply{status: 429, body: googleError(429, "RESOURCE_EXHAUSTED", "")}
	_, _ = e.invoke(customersGetID, `{"customer_id":"`+clientID+`"}`)
	_, _ = e.invoke(customersGetID, `{"customer_id":"`+clientID+`"}`)
	if len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] < time.Second {
		t.Errorf("sleeps = %v, want at least a second", e.sleeps)
	}
}

func TestTokenTravelsInTheHeaderOnly(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = okReply(customerAnswer)
	out, err := e.invoke(customersGetID, `{"customer_id":"`+clientID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range e.fake.api() {
		if request.auth != "Bearer synthetic-access-1" || strings.Contains(request.url, "synthetic") ||
			strings.Contains(request.query, "synthetic") {
			t.Errorf("request = %+v", request)
		}
	}
	if strings.Contains(out, "synthetic-access") || strings.Contains(out, refreshValue) {
		t.Errorf("result %q carries a secret", out)
	}
}

func TestUnauthorizedRenewsTheTokenOnce(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = reply{status: 401, body: adsFailure(401, "UNAUTHENTICATED",
		"authenticationError", "OAUTH_TOKEN_INVALID", "")}
	_, err := e.invoke(customersGetID, `{"customer_id":"`+clientID+`"}`)
	if classOf(err) != provider.ClassAuth || len(e.fake.api()) != 2 || e.fake.tokenRequests() != 2 {
		t.Errorf("err=%v requests=%d tokens=%d, want one renewal and one repeat", err, len(e.fake.api()), e.fake.tokenRequests())
	}
}

func TestMissingConsentEndsBeforeAnyRequest(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	delete(e.roles, config.RoleRefreshToken)
	_, err := e.invoke(customersGetID, `{"customer_id":"`+clientID+`"}`)
	if classOf(err) != provider.ClassConsentRequired || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestBoundedText(t *testing.T) {
	long := strings.Repeat("ä", 2000)
	if got := boundedText(long, maxText); len(got) > maxText || !strings.HasSuffix(got, "…") {
		t.Errorf("cut text = %d bytes", len(got))
	}
	if got := boundedText("a\x00b\nc", maxText); got != "a b c" {
		t.Errorf("control characters = %q", got)
	}
	var v wireInt
	for _, raw := range []string{`"12"`, `12`, `"-3"`} {
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{`"1e3"`, `1.5`, `"x"`, `true`} {
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			t.Errorf("%s accepted", raw)
		}
	}
	if id, ok := canonicalID("123456789"); !ok || id != "0123456789" {
		t.Errorf("canonicalID = %q, %v", id, ok)
	}
	if _, ok := canonicalID("12345678901"); ok {
		t.Error("an eleven-digit ID was accepted")
	}
}
