package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var changeToolIDs = []string{customersUpdateID, customerBudgetNetworkSettingsCreateID, customerBudgetNetworkSettingsUpdateID,
	customerUserAccessesUpdateID, customerUserAccessesRemoveID, customerUserAccessInvitationsRemoveID,
	multiPartyAuthReviewsResolveID, conversionActionsCreateID, conversionActionsUpdateID, conversionActionsRemoveID,
	customerManagerLinksUpdateID, customerClientLinksUpdateID,
	customAudiencesRemoveID, userListsRemoveID,
	campaignsCreateID, campaignsUpdateID, campaignsRemoveID, campaignsEnablePMaxBrandGuidelinesID, labelsRemoveID,
	accountLinksRemoveID, productLinksRemoveID, dataLinksRemoveID, adGroupsCreateID, adGroupsUpdateID, adGroupsRemoveID,
	campaignBudgetsCreateID, campaignBudgetsUpdateID, campaignBudgetsRemoveID, campaignGroupsRemoveID, assetSetsRemoveID,
	adGroupAdsCreateID, adGroupAdsUpdateID, adGroupAdsRemoveID, adGroupAdCreatedAssetsRemoveID,
	biddingStrategiesCreateID, biddingStrategiesUpdateID, biddingStrategiesRemoveID,
	adsUpdateID, adParametersRemoveID,
	biddingDataExclusionsCreateID, biddingDataExclusionsUpdateID, biddingDataExclusionsRemoveID,
	adGroupBidModifiersCreateID, adGroupBidModifiersUpdateID, adGroupBidModifiersRemoveID}

// notListedOnly are the changing tools that a connection with the matching effect offers without a tools list.
// They still need the confirmation of every call.
var notListedOnly = []string{customInterestsCreateID, customInterestsUpdateID, audiencesCreateID, audiencesUpdateID,
	conversionCustomVariablesCreateID, conversionCustomVariablesUpdateID, remarketingActionsCreateID, remarketingActionsUpdateID,
	customAudiencesCreateID, customAudiencesUpdateID,
	userListsCreateID, userListsUpdateID, userListCustomerTypesCreateID, userListCustomerTypesRemoveID,
	recommendationsDismissID, productLinkInvitationsUpdateID, dataLinksUpdateID, adParametersCreateID,
	adParametersUpdateID}

// noValidateOnly are the changing tools whose Mutate request has no validate_only in v26; they offer none.
var noValidateOnly = []string{customerUserAccessesUpdateID, customerUserAccessesRemoveID,
	customerUserAccessInvitationsRemoveID, multiPartyAuthReviewsResolveID, campaignsEnablePMaxBrandGuidelinesID,
	dataLinksRemoveID, adGroupAdCreatedAssetsRemoveID}

func mutatePath(id string) string { return "/v26/customers/" + id + ":mutate" }
func networkPath(id string) string {
	return "/v26/customers/" + id + "/customerBudgetNetworkSettings:mutate"
}

const networkArgs = `"budget_network_settings":[{"advertising_channel_type":"SEARCH","channel_priorities":[` +
	`{"priority_tier":"TIER1","network_channels":["OWNED_AND_OPERATED"]},` +
	`{"priority_tier":"TIER2","network_channels":["SEARCH_PARTNER"]}],` +
	`"budget_caps":[{"network_channel":"SEARCH_PARTNER","cap_percent_millis":20000}]}]`

// posts are the requests of the Mutate services that the fake saw.
func (e *environment) posts() []seen {
	var out []seen
	for _, s := range e.fake.api() {
		if s.method == http.MethodPost && (strings.HasSuffix(s.path, ":mutate") || strings.HasSuffix(s.path, ":resolve")) {
			out = append(out, s)
		}
	}
	return out
}

func bodyOf(t *testing.T, s seen) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(s.body), &body); err != nil {
		t.Fatalf("body %q: %v", s.body, err)
	}
	return body
}

func (e *environment) untouched(t *testing.T) {
	t.Helper()
	if e.lookups != 0 || len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 {
		t.Errorf("lookups=%d requests=%d tokens=%d, want none", e.lookups, len(e.fake.api()), e.fake.tokenRequests())
	}
}

func TestChangeToolsAreListedOnlyConfirmedAndOutsideTheReadProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{customersUpdateID: capability.EffectUpdate,
		customerBudgetNetworkSettingsCreateID: capability.EffectCreate,
		customerBudgetNetworkSettingsUpdateID: capability.EffectUpdate,
		customerUserAccessesUpdateID:          capability.EffectUpdate, customerUserAccessesRemoveID: capability.EffectDelete,
		customerUserAccessInvitationsRemoveID: capability.EffectDelete, multiPartyAuthReviewsResolveID: capability.EffectUpdate,
		conversionActionsCreateID: capability.EffectCreate, conversionActionsUpdateID: capability.EffectUpdate,
		conversionActionsRemoveID:    capability.EffectDelete,
		customerManagerLinksUpdateID: capability.EffectUpdate, customerClientLinksUpdateID: capability.EffectUpdate,
		customAudiencesRemoveID: capability.EffectDelete,
		userListsRemoveID:       capability.EffectDelete,
		campaignsCreateID:       capability.EffectCreate, campaignsUpdateID: capability.EffectUpdate,
		campaignsRemoveID: capability.EffectDelete, campaignsEnablePMaxBrandGuidelinesID: capability.EffectUpdate,
		labelsRemoveID: capability.EffectDelete, accountLinksRemoveID: capability.EffectDelete,
		productLinksRemoveID: capability.EffectDelete, dataLinksRemoveID: capability.EffectDelete,
		adGroupsCreateID: capability.EffectCreate, adGroupsUpdateID: capability.EffectUpdate,
		adGroupsRemoveID:        capability.EffectDelete,
		campaignBudgetsCreateID: capability.EffectCreate, campaignBudgetsUpdateID: capability.EffectUpdate,
		campaignBudgetsRemoveID: capability.EffectDelete, campaignGroupsRemoveID: capability.EffectDelete,
		assetSetsRemoveID:  capability.EffectDelete,
		adGroupAdsCreateID: capability.EffectCreate, adGroupAdsUpdateID: capability.EffectUpdate,
		adGroupAdsRemoveID: capability.EffectDelete, adGroupAdCreatedAssetsRemoveID: capability.EffectDelete,
		biddingStrategiesCreateID: capability.EffectCreate, biddingStrategiesUpdateID: capability.EffectUpdate,
		biddingStrategiesRemoveID: capability.EffectDelete,
		adsUpdateID:               capability.EffectUpdate, adParametersRemoveID: capability.EffectDelete,
		biddingDataExclusionsCreateID: capability.EffectCreate,
		biddingDataExclusionsUpdateID: capability.EffectUpdate, biddingDataExclusionsRemoveID: capability.EffectDelete,
		adGroupBidModifiersCreateID: capability.EffectCreate, adGroupBidModifiersUpdateID: capability.EffectUpdate, adGroupBidModifiersRemoveID: capability.EffectDelete}
	for _, id := range changeToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Version != 1 || d.Risk.Effect != wantEffect[id] ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Errorf("%s = %+v", id, d)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		if got := metadata.Token.Scopes[id]; len(got) != 1 || got[0] != scopeAdwords {
			t.Errorf("scopes of %s = %v", id, got)
		}
		var properties map[string]json.RawMessage
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		properties = input.Properties
		for _, forbidden := range []string{"login_customer_id", "login-customer-id", "partial_failure", "operations",
			"google_ads_conversion_customer", "conversion_tracking_setting", "path", "url", "header"} {
			if _, found := properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := properties["validate_only"]; !found == !slices.Contains(noValidateOnly, id) {
			t.Errorf("%s: validate_only present = %v", id, found)
		}
		if strings.Contains(string(d.InputSchema), "google_ads_conversion_customer") {
			t.Errorf("%s offers google_ads_conversion_customer", id)
		}
		if !strings.HasSuffix(d.Description, mutateNote) {
			t.Errorf("%s: a tools-list-only tool must end on the note that says so", id)
		}
		if !strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "never repeats") {
			t.Errorf("%s: description does not name the unknown outcome: %s", id, d.Description)
		}
	}
	update, _, _ := reg.Lookup(customersUpdateID)
	for _, word := range []string{"tracking", "final URL", "click data", "https", "not offered"} {
		if !strings.Contains(strings.ToLower(update.Description), strings.ToLower(word)) {
			t.Errorf("customers.update: description lacks %q", word)
		}
	}
	network, _, _ := reg.Lookup(customerBudgetNetworkSettingsUpdateID)
	for _, word := range []string{"budget", "spread", "caps", "allowlisted", "whole"} {
		if !strings.Contains(strings.ToLower(network.Description), word) {
			t.Errorf("budget network settings: description lacks %q", word)
		}
	}
	for _, tool := range metadata.Tools {
		if slices.Contains(changeToolIDs, tool.ID) && (!tool.RequiresToolAllowList || tool.Effect == config.EffectRead) {
			t.Errorf("tool %+v is not a listed-only change tool", tool)
		}
	}
}

// Without the tool in the tools list no connection offers it, whatever its effects are; without the
// confirmation of the call nothing runs. Neither reads a secret or sends a request.
func TestChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		customersUpdateID:                     `{"customer_id":"` + clientID + `","auto_tagging_enabled":true}`,
		customerBudgetNetworkSettingsCreateID: `{"customer_id":"` + clientID + `",` + networkArgs + `}`,
		customerBudgetNetworkSettingsUpdateID: `{"customer_id":"` + clientID + `",` + networkArgs + `}`,
	}
	for id, arguments := range calls {
		t.Run(id+" without the tools list", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
				config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}})
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a connection with all effects but no tools list ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || needed.Summary == "" {
				t.Fatalf("err = %v, want a confirmation with a summary", err)
			}
			if !strings.Contains(needed.Summary, clientID) {
				t.Errorf("summary %q names no account", needed.Summary)
			}
			e.untouched(t)
		})
		t.Run(id+" over the read effect", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			e.untouched(t)
		})
	}
}

// The arguments a caller may not use are refused by the closed input schema before anything runs.
func TestChangesRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`,
		`"google_ads_conversion_customer":"customers/` + otherID + `"`, `"operations":[]`, `"path":"/x"`} {
		for id, arguments := range map[string]string{
			customersUpdateID:                     `{"customer_id":"` + clientID + `","auto_tagging_enabled":true,`,
			customerBudgetNetworkSettingsUpdateID: `{"customer_id":"` + clientID + `",` + networkArgs + `,`} {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments+extra+`}`); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestChangeTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	cases := map[string]string{
		customersUpdateID:                     `{"customer_id":"` + otherID + `","auto_tagging_enabled":true}`,
		customerBudgetNetworkSettingsCreateID: `{"customer_id":"` + otherID + `",` + networkArgs + `}`,
		customerBudgetNetworkSettingsUpdateID: `{"customer_id":"` + otherID + `",` + networkArgs + `}`,
	}
	for id, arguments := range cases {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestChangeArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	// at keeps the user-info samples from reading as mail addresses.
	const at = "@"
	bad := map[string]string{
		"http template":       `{"customer_id":"` + clientID + `","tracking_url_template":"http://t.example.com/?u={lpurl}"}`,
		"user info":           `{"customer_id":"` + clientID + `","tracking_url_template":"https://user:pw` + at + `t.example.com/?u={lpurl}"}`,
		"user info at":        `{"customer_id":"` + clientID + `","tracking_url_template":"https://evil.example` + at + `t.example.com/"}`,
		"parameter first":     `{"customer_id":"` + clientID + `","tracking_url_template":"{lpurl}?x=1"}`,
		"parameter as host":   `{"customer_id":"` + clientID + `","tracking_url_template":"https://{lpurl}/x"}`,
		"no host":             `{"customer_id":"` + clientID + `","tracking_url_template":"https:///x"}`,
		"other scheme":        `{"customer_id":"` + clientID + `","tracking_url_template":"ftp://t.example.com/"}`,
		"space":               `{"customer_id":"` + clientID + `","tracking_url_template":"https://t.example.com/a b"}`,
		"backslash":           `{"customer_id":"` + clientID + `","tracking_url_template":"https://t.example.com\\@x/"}`,
		"too long":            `{"customer_id":"` + clientID + `","tracking_url_template":"https://t.example.com/` + strings.Repeat("a", 2048) + `"}`,
		"suffix address":      `{"customer_id":"` + clientID + `","final_url_suffix":"https://x.example.com/"}`,
		"suffix scheme-rel":   `{"customer_id":"` + clientID + `","final_url_suffix":"//x.example.com"}`,
		"suffix space":        `{"customer_id":"` + clientID + `","final_url_suffix":"a=1 b=2"}`,
		"foreign action":      `{"customer_id":"` + clientID + `","call_conversion_action":"customers/` + queryNumber(otherID) + `/conversionActions/5"}`,
		"action form":         `{"customer_id":"` + clientID + `","call_conversion_action":"conversionActions/5"}`,
		"blank name":          `{"customer_id":"` + clientID + `","descriptive_name":"   "}`,
		"nothing to change":   `{"customer_id":"` + clientID + `"}`,
		"validate only alone": `{"customer_id":"` + clientID + `","validate_only":true}`,
	}
	for name, arguments := range bad {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(customersUpdateID, arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	network := map[string]string{
		"twice the type": `"budget_network_settings":[{"advertising_channel_type":"SEARCH"},{"advertising_channel_type":"SEARCH"}]`,
		"unknown type":   `"budget_network_settings":[{"advertising_channel_type":"UNKNOWN"}]`,
		"cap too high": `"budget_network_settings":[{"advertising_channel_type":"SEARCH","budget_caps":[` +
			`{"network_channel":"SEARCH_PARTNER","cap_percent_millis":100001}]}]`,
		"cap negative": `"budget_network_settings":[{"advertising_channel_type":"SEARCH","budget_caps":[` +
			`{"network_channel":"SEARCH_PARTNER","cap_percent_millis":-1}]}]`,
		"cap channel twice": `"budget_network_settings":[{"advertising_channel_type":"SEARCH","budget_caps":[` +
			`{"network_channel":"SEARCH_PARTNER"},{"network_channel":"SEARCH_PARTNER"}]}]`,
		"channel in two tiers": `"budget_network_settings":[{"advertising_channel_type":"SEARCH","channel_priorities":[` +
			`{"priority_tier":"TIER1","network_channels":["SEARCH_PARTNER"]},` +
			`{"priority_tier":"TIER2","network_channels":["SEARCH_PARTNER"]}]}]`,
		"tier twice": `"budget_network_settings":[{"advertising_channel_type":"SEARCH","channel_priorities":[` +
			`{"priority_tier":"TIER1"},{"priority_tier":"TIER1"}]}]`,
		"empty list":     `"budget_network_settings":[]`,
		"too many types": `"budget_network_settings":[` + strings.Repeat(`{"advertising_channel_type":"SEARCH"},`, 12) + `{"advertising_channel_type":"SEARCH"}]`,
	}
	for name, arguments := range network {
		for _, id := range []string{customerBudgetNetworkSettingsCreateID, customerBudgetNetworkSettingsUpdateID} {
			t.Run(name+" "+id, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				if _, err := e.invokeConfirmed(id, `{"customer_id":"`+clientID+`",`+arguments+`}`); err == nil {
					t.Fatal("accepted")
				}
				e.untouched(t)
			})
		}
	}
}

func TestCustomersUpdateSendsOneAtomicRequestWithMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+mutatePath(clientID)] = okReply(`{"result":{"resourceName":"customers/` + clientID + `"}}`)
	out, err := e.invokeConfirmed(customersUpdateID, `{"customer_id":"`+clientID+`","descriptive_name":"Shop",`+
		`"tracking_url_template":"https://t.example.com/?u={lpurl}&c={campaignid}","final_url_suffix":"utm_source=g",`+
		`"auto_tagging_enabled":false,"call_reporting_enabled":true,"call_conversion_reporting_enabled":false,`+
		`"call_conversion_action":"customers/`+queryNumber(clientID)+`/conversionActions/55"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if got["resource_name"] != "customers/"+clientID || got["validate_only"] != false || got["customer_id"] != clientID ||
		len(got["updated"].([]any)) != 7 || got["customer"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+mutatePath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 {
		t.Errorf("body members = %v, want the operation alone", body)
	}
	op := body["operation"].(map[string]any)
	update := op["update"].(map[string]any)
	if op["updateMask"] != "descriptiveName,trackingUrlTemplate,finalUrlSuffix,autoTaggingEnabled,"+
		"callReportingSetting.callReportingEnabled,callReportingSetting.callConversionReportingEnabled,"+
		"callReportingSetting.callConversionAction" ||
		update["resourceName"] != "customers/"+queryNumber(clientID) || update["descriptiveName"] != "Shop" ||
		update["trackingUrlTemplate"] != "https://t.example.com/?u={lpurl}&c={campaignid}" ||
		update["finalUrlSuffix"] != "utm_source=g" || update["autoTaggingEnabled"] != false {
		t.Errorf("operation = %v", op)
	}
	call := update["callReportingSetting"].(map[string]any)
	if call["callReportingEnabled"] != true || call["callConversionReportingEnabled"] != false ||
		call["callConversionAction"] != "customers/"+queryNumber(clientID)+"/conversionActions/55" {
		t.Errorf("call reporting = %v", call)
	}
	if strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") ||
		strings.Contains(p.body, "googleAdsConversionCustomer") {
		t.Errorf("body %s carries a member it must not", p.body)
	}
}

func TestCustomersUpdateClearsWithTheMaskAlone(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+mutatePath(clientID)] = okReply(`{"result":{"resourceName":"customers/` + clientID + `"}}`)
	if _, err := e.invokeConfirmed(customersUpdateID, `{"customer_id":"`+clientID+`","tracking_url_template":"",`+
		`"final_url_suffix":"","call_conversion_action":""}`); err != nil {
		t.Fatal(err)
	}
	op := bodyOf(t, e.posts()[0])["operation"].(map[string]any)
	update := op["update"].(map[string]any)
	if op["updateMask"] != "trackingUrlTemplate,finalUrlSuffix,callReportingSetting.callConversionAction" || len(update) != 1 {
		t.Errorf("operation = %v", op)
	}
}

func TestValidateOnlyValidatesWithoutAResult(t *testing.T) {
	cases := map[string]struct {
		id, arguments, path string
	}{
		"customers.update": {customersUpdateID, `{"customer_id":"` + clientID + `","auto_tagging_enabled":true,"validate_only":true}`,
			mutatePath(clientID)},
		"network create": {customerBudgetNetworkSettingsCreateID, `{"customer_id":"` + clientID + `",` + networkArgs + `,"validate_only":true}`,
			networkPath(clientID)},
		"network update": {customerBudgetNetworkSettingsUpdateID, `{"customer_id":"` + clientID + `",` + networkArgs + `,"validate_only":true}`,
			networkPath(clientID)},
	}
	for name, c := range cases {
		for _, answer := range []string{`{}`, `{"result":{}}`} {
			t.Run(name+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+c.path] = okReply(answer)
				out, err := e.invokeConfirmed(c.id, c.arguments)
				if err != nil {
					t.Fatal(err)
				}
				got := decode(t, out)
				if got["validate_only"] != true || got["resource_name"] != nil || got["customer"] != nil ||
					got["customer_budget_network_setting"] != nil {
					t.Errorf("out = %s", out)
				}
				posts := e.posts()
				if len(posts) != 1 || bodyOf(t, posts[0])["validateOnly"] != true {
					t.Errorf("posts = %+v", posts)
				}
			})
		}
	}
	t.Run("a validation error is the refusal of Google", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+networkPath(clientID)] = reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT",
			"budgetNetworkSettingError", "MISSING_PRIMARY_PRIORITY_CHANNEL", "")}
		_, err := e.invokeConfirmed(customerBudgetNetworkSettingsUpdateID, `{"customer_id":"`+clientID+`",`+networkArgs+
			`,"validate_only":true}`)
		if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "budgetNetworkSettingError "+
			"MISSING_PRIMARY_PRIORITY_CHANNEL") || !strings.Contains(err.Error(), "nothing was changed") ||
			strings.Contains(err.Error(), "outcome") || strings.Contains(err.Error(), detailCanary) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestResponseContentTypeShowsTheMutableFields(t *testing.T) {
	t.Run("customer", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+mutatePath(clientID)] = okReply(`{"result":{"resourceName":"customers/` + clientID + `",` +
			`"customer":{"resourceName":"customers/` + clientID + `","descriptiveName":"Shop","autoTaggingEnabled":false,` +
			`"trackingUrlTemplate":"https://t.example.com/","callReportingSetting":{"callReportingEnabled":true,` +
			`"callConversionAction":"customers/` + queryNumber(clientID) + `/conversionActions/55"},` +
			`"conversionTrackingSetting":{"googleAdsConversionCustomer":"customers/` + otherID + `"},` +
			`"videoBrandSafetySuitability":"EXPANDED_INVENTORY","unknownMember":"never held"}}}`)
		out, err := e.invokeConfirmed(customersUpdateID, `{"customer_id":"`+clientID+`","auto_tagging_enabled":false,`+
			`"response_content_type":"MUTABLE_RESOURCE"}`)
		if err != nil {
			t.Fatal(err)
		}
		customer, _ := decode(t, out)["customer"].(map[string]any)
		call, _ := customer["call_reporting_setting"].(map[string]any)
		if customer["descriptive_name"] != "Shop" || customer["auto_tagging_enabled"] != false ||
			customer["tracking_url_template"] != "https://t.example.com/" || call["call_reporting_enabled"] != true ||
			customer["google_ads_conversion_customer"] != "customers/"+otherID ||
			customer["video_brand_safety_suitability"] != "EXPANDED_INVENTORY" || customer["unknownMember"] != nil {
			t.Errorf("customer = %v", customer)
		}
		if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
			t.Errorf("body = %s", e.posts()[0].body)
		}
	})
	t.Run("budget network setting", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+networkPath(clientID)] = okReply(`{"result":{"resourceName":"customers/` + clientID +
			`/customerBudgetNetworkSettings","customerBudgetNetworkSetting":{"resourceName":"customers/` + clientID +
			`/customerBudgetNetworkSettings","budgetNetworkSettings":{"settings":[{"advertisingChannelType":"SEARCH",` +
			`"budgetCaps":[{"networkChannel":"SEARCH_PARTNER","capPercentMillis":"20000"}],"channelPriorities":[` +
			`{"priorityTier":"TIER1","networkChannels":["OWNED_AND_OPERATED"]},{"priorityTier":"TIER2",` +
			`"networkChannels":["SEARCH_PARTNER"]}]}]}}}}`)
		out, err := e.invokeConfirmed(customerBudgetNetworkSettingsUpdateID, `{"customer_id":"`+clientID+`",`+networkArgs+
			`,"response_content_type":"MUTABLE_RESOURCE"}`)
		if err != nil {
			t.Fatal(err)
		}
		setting := decode(t, out)["customer_budget_network_setting"].(map[string]any)
		entry := setting["budget_network_settings"].([]any)[0].(map[string]any)
		caps := entry["budget_caps"].([]any)[0].(map[string]any)
		if entry["advertising_channel_type"] != "SEARCH" || caps["cap_percent_millis"] != float64(20000) ||
			caps["network_channel"] != "SEARCH_PARTNER" || len(entry["channel_priorities"].([]any)) != 2 {
			t.Errorf("setting = %v", setting)
		}
	})
}

func wantWire(t *testing.T, text string) any {
	t.Helper()
	var out any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBudgetNetworkSettingsBodies(t *testing.T) {
	wantSettings := `{"settings":[{"advertisingChannelType":"SEARCH","budgetCaps":[{"networkChannel":"SEARCH_PARTNER",` +
		`"capPercentMillis":20000}],"channelPriorities":[{"priorityTier":"TIER1","networkChannels":["OWNED_AND_OPERATED"]},` +
		`{"priorityTier":"TIER2","networkChannels":["SEARCH_PARTNER"]}]}]}`
	resource := okReply(`{"result":{"resourceName":"customers/` + clientID + `/customerBudgetNetworkSettings"}}`)
	t.Run("create", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+networkPath(clientID)] = resource
		out, err := e.invokeConfirmed(customerBudgetNetworkSettingsCreateID, `{"customer_id":"`+clientID+`",`+networkArgs+`}`)
		if err != nil || decode(t, out)["resource_name"] != "customers/"+clientID+"/customerBudgetNetworkSettings" {
			t.Fatalf("out=%s err=%v", out, err)
		}
		op := bodyOf(t, e.posts()[0])["operation"].(map[string]any)
		if len(op) != 1 || !reflect.DeepEqual(op["create"], map[string]any{"budgetNetworkSettings": wantWire(t, wantSettings)}) {
			t.Errorf("operation = %v", op)
		}
	})
	t.Run("update replaces the whole setting by its fixed name", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+networkPath(clientID)] = resource
		if _, err := e.invokeConfirmed(customerBudgetNetworkSettingsUpdateID, `{"customer_id":"`+clientID+`",`+networkArgs+`}`); err != nil {
			t.Fatal(err)
		}
		op := bodyOf(t, e.posts()[0])["operation"].(map[string]any)
		if op["updateMask"] != "budgetNetworkSettings" || !reflect.DeepEqual(op["update"], map[string]any{
			"budgetNetworkSettings": wantWire(t, wantSettings),
			"resourceName":          "customers/" + queryNumber(clientID) + "/customerBudgetNetworkSettings"}) {
			t.Errorf("operation = %v", op)
		}
	})
}

// A resource name of another account never passes, however the answer looks otherwise. The change was made
// as far as Qatlas can tell, so the error says that the outcome is unclear.
func TestResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := `customers/` + queryNumber(otherID)
	cases := map[string]struct{ id, arguments, path, body string }{
		"update result": {customersUpdateID, `{"customer_id":"` + clientID + `","auto_tagging_enabled":true}`, mutatePath(clientID),
			`{"result":{"resourceName":"` + foreign + `"}}`},
		"update without a name": {customersUpdateID, `{"customer_id":"` + clientID + `","auto_tagging_enabled":true}`, mutatePath(clientID),
			`{}`},
		"update customer": {customersUpdateID, `{"customer_id":"` + clientID + `","auto_tagging_enabled":true,"response_content_type":"MUTABLE_RESOURCE"}`,
			mutatePath(clientID), `{"result":{"resourceName":"customers/` + queryNumber(clientID) + `","customer":{"resourceName":"` + foreign + `"}}}`},
		"update conversion action": {customersUpdateID, `{"customer_id":"` + clientID + `","auto_tagging_enabled":true,"response_content_type":"MUTABLE_RESOURCE"}`,
			mutatePath(clientID), `{"result":{"resourceName":"customers/` + queryNumber(clientID) + `","customer":{"resourceName":"customers/` +
				queryNumber(clientID) + `","callReportingSetting":{"callConversionAction":"` + foreign + `/conversionActions/5"}}}}`},
		"network result": {customerBudgetNetworkSettingsUpdateID, `{"customer_id":"` + clientID + `",` + networkArgs + `}`, networkPath(clientID),
			`{"result":{"resourceName":"` + foreign + `/customerBudgetNetworkSettings"}}`},
		"network setting": {customerBudgetNetworkSettingsUpdateID, `{"customer_id":"` + clientID + `",` + networkArgs +
			`,"response_content_type":"MUTABLE_RESOURCE"}`, networkPath(clientID),
			`{"result":{"resourceName":"customers/` + queryNumber(clientID) + `/customerBudgetNetworkSettings","customerBudgetNetworkSetting":` +
				`{"resourceName":"` + foreign + `/customerBudgetNetworkSettings"}}}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+c.path] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

// A connection with targets needs a manager target that names a manager account. A customer target and an
// account below a manager target reach only what is no manager.
func TestManagerAccountsNeedAManagerTarget(t *testing.T) {
	mutated := okReply(`{"result":{"resourceName":"customers/` + managerID + `"}}`)
	args := `{"customer_id":"` + managerID + `","auto_tagging_enabled":true}`
	managerRow := func(manager bool) *reply {
		r := rows(`{"customer":{"id":"` + managerID + `","manager":` + map[bool]string{true: "true", false: "false"}[manager] + `}}`)
		return &r
	}
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+mutatePath(managerID)] = mutated
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				return managerRow(true)
			}
			return nil
		}
		_, err := e.invokeConfirmed(customersUpdateID, args)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes without the check", func(t *testing.T) {
		e := newMutationEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+mutatePath(managerID)] = mutated
		if _, err := e.invokeConfirmed(customersUpdateID, args); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
	t.Run("a customer target of a client account passes after one read", func(t *testing.T) {
		e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
		e.fake.routes["POST "+mutatePath(clientID)] = okReply(`{"result":{"resourceName":"customers/` + clientID + `"}}`)
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + clientID + `"}}]}`}
			}
			return nil
		}
		if _, err := e.invokeConfirmed(customersUpdateID, `{"customer_id":"`+clientID+`","auto_tagging_enabled":true}`); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 2 || got[0].method != http.MethodPost || !strings.HasSuffix(got[0].path, "googleAds:search") {
			t.Errorf("requests = %+v", got)
		}
	})
	t.Run("an account below a manager target is proved, then checked, then changed", func(t *testing.T) {
		e := newMutationEnvironment(t, []string{"managers/" + managerID}, map[string]string{optionLoginCustomerID: managerID})
		e.fake.routes["POST "+mutatePath(clientID)] = okReply(`{"result":{"resourceName":"customers/` + clientID + `"}}`)
		e.fake.answer = func(s seen) *reply {
			switch {
			case strings.HasPrefix(s.query, "SELECT customer_client.id"):
				r := rows(proofItem(clientID))
				return &r
			case strings.HasPrefix(s.query, "SELECT customer.id, customer.manager"):
				return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + clientID + `"}}]}`}
			}
			return nil
		}
		if _, err := e.invokeConfirmed(customersUpdateID, `{"customer_id":"`+clientID+`","auto_tagging_enabled":true}`); err != nil {
			t.Fatal(err)
		}
		got := e.fake.api()
		if len(got) != 3 || !strings.Contains(got[0].query, "customer_client") || !strings.Contains(got[1].query, "customer.manager") ||
			!strings.HasSuffix(got[2].path, ":mutate") {
			t.Errorf("requests = %+v", got)
		}
	})
	t.Run("an account below the manager that is a manager itself is refused", func(t *testing.T) {
		e := newMutationEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.answer = func(s seen) *reply {
			switch {
			case strings.HasPrefix(s.query, "SELECT customer_client.id"):
				r := rows(proofItem(subManager))
				return &r
			case strings.HasPrefix(s.query, "SELECT customer.id, customer.manager"):
				return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + subManager + `","manager":true}}]}`}
			}
			return nil
		}
		_, err := e.invokeConfirmed(customersUpdateID, `{"customer_id":"`+subManager+`","auto_tagging_enabled":true}`)
		if !isInvalidRequest(err) || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("an unreadable check never counts as no manager", func(t *testing.T) {
		e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows()
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(customersUpdateID, `{"customer_id":"`+clientID+`","auto_tagging_enabled":true}`)
		if classOf(err) != provider.ClassInvalidResponse || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

func TestChangeErrorsAreMappedAndNeverRepeated(t *testing.T) {
	args := `{"customer_id":"` + clientID + `","auto_tagging_enabled":true}`
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"not allowlisted for budget network settings", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED",
			"budgetNetworkSettingError", "CUSTOMER_NOT_ALLOWLISTED", "")}, provider.ClassPermission, "allowlisted", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"field error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "fieldError", "REQUIRED",
			"")}, provider.ClassProviderError, "rejected the change (fieldError REQUIRED), nothing was changed", false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"token refused", reply{status: 401, body: adsFailure(401, "UNAUTHENTICATED", "authenticationError",
			"OAUTH_TOKEN_EXPIRED", "")}, provider.ClassAuth, "access token", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"internal error", reply{status: 500, body: adsFailure(500, "INTERNAL", "internalError", "INTERNAL_ERROR", "")},
			provider.ClassifyStatus("op", 500, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"cut connection", reply{err: errors.New("connection reset by peer")}, provider.ClassUnreachable,
			"outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"not json", reply{status: 200, body: "<html>", header: http.Header{"Content-Type": {"text/html"}}},
			provider.ClassInvalidResponse, "outcome of the change is unknown", true},
		{"unreadable json", okReply(`{"result":`), provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+mutatePath(clientID)] = c.reply
			_, err := e.invokeConfirmed(customersUpdateID, args)
			if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) {
				t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
			}
			if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != c.unknown {
				t.Errorf("unknown outcome reported = %v, want %v: %v", got, c.unknown, err)
			}
			for _, leaked := range []string{detailCanary, triggerCanary, "synthetic-access"} {
				if strings.Contains(err.Error(), leaked) {
					t.Errorf("error %q carries %q", err, leaked)
				}
			}
			if got := len(e.posts()); got != 1 || len(e.fake.api()) != 1 {
				t.Errorf("mutate requests = %d of %d, want one and no repeat", got, len(e.fake.api()))
			}
			if e.fake.tokenRequests() != 1 {
				t.Errorf("token requests = %d, a refused token is not renewed within the call", e.fake.tokenRequests())
			}
		})
	}
	t.Run("the original carries the code, the field and the request ID, never the trigger", func(t *testing.T) {
		failed, _ := adsError("op", 400, []byte(adsFailure(400, "INVALID_ARGUMENT", "fieldError", "REQUIRED", "")), true)
		if failed.Original == nil || failed.Original.Status != 400 {
			t.Fatalf("failed = %+v", failed)
		}
		var found bool
		for _, entry := range failed.Original.Entries {
			if entry.Type == "fieldError" && entry.Code == "REQUIRED" && entry.Path == "query" {
				found = true
			}
			if strings.Contains(entry.Message+entry.Code+entry.Path, triggerCanary) {
				t.Errorf("entry %+v carries the trigger", entry)
			}
		}
		if !found {
			t.Errorf("entries = %+v", failed.Original.Entries)
		}
	})
	t.Run("a read keeps its mapping of an unknown 400", func(t *testing.T) {
		failed, _ := adsError("op", 400, []byte(adsFailure(400, "INVALID_ARGUMENT", "fieldError", "REQUIRED", "")), false)
		if failed.Class != provider.ClassInvalidResponse {
			t.Errorf("class = %s", failed.Class)
		}
	})
}

// A rate limit holds the next request of the credential, also after a change, and never repeats the change.
func TestChangeRateLimitHoldsTheNextRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+mutatePath(clientID)] = reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED",
		"quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}
	args := `{"customer_id":"` + clientID + `","auto_tagging_enabled":true}`
	if _, err := e.invokeConfirmed(customersUpdateID, args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("err = %v", err)
	}
	_, _ = e.invokeConfirmed(customersUpdateID, args)
	if len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] < 29_000_000_000 || len(e.posts()) != 2 {
		t.Errorf("sleeps = %v, posts = %d", e.sleeps, len(e.posts()))
	}
}

func TestChangeSummariesNameTheEffect(t *testing.T) {
	summary := customerUpdateSummary(json.RawMessage(`{"customer_id":"` + clientID + `","tracking_url_template":"https://t.example.com/",` +
		`"final_url_suffix":"","validate_only":true}`))
	for _, word := range []string{clientID, "https://t.example.com/", "clear the final URL suffix", "Click data", "Only validate"} {
		if !strings.Contains(summary, word) {
			t.Errorf("summary %q lacks %q", summary, word)
		}
	}
	network := budgetNetworkSummary("Replace")
	text := network(json.RawMessage(`{"customer_id":"` + clientID + `",` + networkArgs + `}`))
	for _, word := range []string{clientID, "SEARCH", "TIER2=SEARCH_PARTNER", "20000", "spread"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
	// The same function serves every call: a validate-only call does not change what the next one says.
	_ = network(json.RawMessage(`{"customer_id":"` + clientID + `",` + networkArgs + `,"validate_only":true}`))
	if again := network(json.RawMessage(`{"customer_id":"` + clientID + `",` + networkArgs + `}`)); strings.Contains(again, "validate") {
		t.Errorf("summary %q kept state", again)
	}
}
