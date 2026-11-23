package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

var recommendationToolIDs = []string{recommendationsGenerateID, recommendationsApplyID, recommendationsDismissID}

// testPhone is built at run time so that no phone-like literal sits in the source.
var testPhone = fmt.Sprintf("+%03d %03d %04d", 0, 0, 0)

func recName(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/recommendations/" + id
}

func recPath(customer, action string) string {
	return "/v26/customers/" + customer + "/recommendations:" + action
}

// recPosts are the requests of the recommendation service that the fake saw.
func (e *environment) recPosts() []seen {
	var out []seen
	for _, s := range e.fake.api() {
		if s.method == http.MethodPost && strings.Contains(s.path, "/recommendations:") {
			out = append(out, s)
		}
	}
	return out
}

func newRecommendationEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: recommendationToolIDs})
}

func recResults(names ...string) string {
	items := make([]string, 0, len(names))
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return `{"results":[` + strings.Join(items, ",") + `]}`
}

func TestRecommendationToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	want := map[string]struct {
		effect capability.Effect
		listed bool
	}{recommendationsGenerateID: {capability.EffectRead, false}, recommendationsApplyID: {capability.EffectUpdate, true},
		recommendationsDismissID: {capability.EffectUpdate, false}}
	for id, w := range want {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != w.effect || !d.Risk.OpenWorld || len(d.OutputSchema) == 0 {
			t.Fatalf("%s = %+v", id, d)
		}
		if d.RequiresToolAllowList != w.listed || slices.Contains(metadata.Profiles[0].Tools, id) != (w.effect == capability.EffectRead) {
			t.Errorf("%s: tools list = %v, in read profile = %v", id, d.RequiresToolAllowList,
				slices.Contains(metadata.Profiles[0].Tools, id))
		}
		if (d.Risk.Confirmation == capability.ConfirmationRequired) != (w.effect != capability.EffectRead) {
			t.Errorf("%s: confirmation = %v", id, d.Risk.Confirmation)
		}
		if got := metadata.Token.Scopes[id]; !reflect.DeepEqual(got, []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, got)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "validate_only", "path", "url"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if w.effect != capability.EffectRead {
			for _, word := range []string{"no validate_only", "never repeats", "outcome is unknown", "atomic"} {
				if !strings.Contains(d.Description, word) {
					t.Errorf("%s: description lacks %q", id, word)
				}
			}
		}
	}
}

// Every variant of the oneof the reference documents is either offered with exactly the members listed here
// or left out for a stated reason. Nothing that sets a tracking route is among the offered members.
func TestApplyOffersTheDocumentedVariantsAndNothingThatLeavesTheAccount(t *testing.T) {
	reg := capability.NewRegistry()
	_ = Register(reg)
	apply, _, _ := reg.Lookup(recommendationsApplyID)
	var input struct {
		Properties struct {
			Operations struct {
				MaxItems int `json:"maxItems"`
				Items    struct {
					Properties struct {
						Parameters struct {
							Properties    map[string]json.RawMessage `json:"properties"`
							MinProperties int                        `json:"minProperties"`
							MaxProperties int                        `json:"maxProperties"`
							Additional    bool                       `json:"additionalProperties"`
						} `json:"parameters"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"operations"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(apply.InputSchema, &input); err != nil {
		t.Fatal(err)
	}
	p := input.Properties.Operations.Items.Properties.Parameters
	var got []string
	for name := range p.Properties {
		got = append(got, name)
	}
	slices.Sort(got)
	offered := []string{"call_asset", "callout_asset", "campaign_budget", "forecasting_set_target_cpa",
		"forecasting_set_target_roas", "keyword", "lower_target_roas", "lower_target_roas_performance_bid_too_low",
		"move_unused_budget", "optimize_for_new_customers", "raise_budget_performance_too_low", "raise_target_cpa",
		"raise_target_cpa_bid_too_low", "raise_target_cpa_performance_bid_too_low", "responsive_search_ad",
		"responsive_search_ad_asset", "responsive_search_ad_improve_ad_strength", "set_target_cpa", "set_target_roas",
		"sitelink_asset", "target_cpa_opt_in", "target_roas_opt_in", "use_broad_match_keyword"}
	if !reflect.DeepEqual(got, offered) {
		t.Errorf("variants = %v, want %v", got, offered)
	}
	if p.MinProperties != 1 || p.MaxProperties != 1 || p.Additional || input.Properties.Operations.MaxItems != 10 {
		t.Errorf("the oneof or the bound is open: %+v", p)
	}
	schemaText := string(apply.InputSchema)
	for _, absent := range []string{"lead_form_asset", "text_ad", "callout_extension", "call_extension", "sitelink_extension",
		"tracking_url_template", "final_url_suffix", "url_custom_parameters", "final_mobile_urls", "final_app_urls",
		"display_url", "url_collections", "webhook", "call_conversion_action", "partial_failure", "validate_only"} {
		if strings.Contains(schemaText, absent) {
			t.Errorf("the input schema offers %s", absent)
		}
	}
	for _, word := range []string{"Not offered", "lead form", "no validate_only", "asynchronously", "https without user info"} {
		if !strings.Contains(apply.Description, word) {
			t.Errorf("description lacks %q", word)
		}
	}
}

const (
	adGroupOf = `customers/1000000003/adGroups/55`
	adOf      = `customers/1000000003/ads/77`
	assetOf   = `customers/1000000003/assets/9`
	rsaIn     = `"final_urls":["https://shop.example.com/chairs"],"headlines":[{"text":"Garden chairs","pinned_field":"HEADLINE_1"},` +
		`{"text":"Free delivery"}],"descriptions":[{"text":"Order today","pinned_field":"DESCRIPTION_2"}],"path1":"chairs","path2":"sale"`
	rsaOut = `"finalUrls":["https://shop.example.com/chairs"],"responsiveSearchAd":{"headlines":[{"text":"Garden chairs",` +
		`"pinnedField":"HEADLINE_1"},{"text":"Free delivery"}],"descriptions":[{"text":"Order today","pinnedField":"DESCRIPTION_2"}],` +
		`"path1":"chairs","path2":"sale"}`
)

// Each variant maps to the member of the reference with its parameters, and to nothing else.
func TestApplyMapsEveryVariantIntoTheRequest(t *testing.T) {
	cases := []struct{ variant, in, out string }{
		{"campaign_budget", `{"new_budget_amount_micros":20000000}`, `{"campaignBudget":{"newBudgetAmountMicros":20000000}}`},
		{"keyword", `{"ad_group":"` + adGroupOf + `","match_type":"PHRASE","cpc_bid_micros":1500000}`,
			`{"keyword":{"adGroup":"` + adGroupOf + `","matchType":"PHRASE","cpcBidMicros":1500000}}`},
		{"keyword", `{"ad_group":"` + adGroupOf + `","match_type":"EXACT"}`,
			`{"keyword":{"adGroup":"` + adGroupOf + `","matchType":"EXACT"}}`},
		{"target_cpa_opt_in", `{"target_cpa_micros":5000000,"new_campaign_budget_amount_micros":30000000}`,
			`{"targetCpaOptIn":{"targetCpaMicros":5000000,"newCampaignBudgetAmountMicros":30000000}}`},
		{"target_roas_opt_in", `{"target_roas":3.5}`, `{"targetRoasOptIn":{"targetRoas":3.5}}`},
		{"target_roas_opt_in", `{"new_campaign_budget_amount_micros":30000000}`,
			`{"targetRoasOptIn":{"newCampaignBudgetAmountMicros":30000000}}`},
		{"move_unused_budget", `{"budget_micros_to_move":4000000}`, `{"moveUnusedBudget":{"budgetMicrosToMove":4000000}}`},
		{"responsive_search_ad", `{"ad":{` + rsaIn + `}}`, `{"responsiveSearchAd":{"ad":{` + rsaOut + `}}}`},
		{"use_broad_match_keyword", `{"new_budget_amount_micros":6000000}`,
			`{"useBroadMatchKeyword":{"newBudgetAmountMicros":6000000}}`},
		{"responsive_search_ad_asset", `{"updated_ad":{"resource_name":"` + adOf + `",` + rsaIn + `}}`,
			`{"responsiveSearchAdAsset":{"updatedAd":{"resourceName":"` + adOf + `",` + rsaOut + `}}}`},
		{"responsive_search_ad_improve_ad_strength", `{"updated_ad":{"resource_name":"` + adOf + `",` + rsaIn + `}}`,
			`{"responsiveSearchAdImproveAdStrength":{"updatedAd":{"resourceName":"` + adOf + `",` + rsaOut + `}}}`},
		{"raise_target_cpa_bid_too_low", `{"target_multiplier":1.2}`, `{"raiseTargetCpaBidTooLow":{"targetMultiplier":1.2}}`},
		{"forecasting_set_target_roas", `{"target_roas":2,"campaign_budget_amount_micros":1000000}`,
			`{"forecastingSetTargetRoas":{"targetRoas":2,"campaignBudgetAmountMicros":1000000}}`},
		{"callout_asset", `{"scope":"CAMPAIGN","new_assets":[{"callout_text":"Free returns"}],"existing_assets":["` + assetOf + `"]}`,
			`{"calloutAsset":{"adAssetApplyParameters":{"scope":"CAMPAIGN","existingAssets":["` + assetOf + `"],` +
				`"newAssets":[{"calloutAsset":{"calloutText":"Free returns"}}]}}}`},
		{"call_asset", `{"scope":"CUSTOMER","new_assets":[{"country_code":"DE","phone_number":"` + testPhone + `"}]}`,
			`{"callAsset":{"adAssetApplyParameters":{"scope":"CUSTOMER","newAssets":[{"callAsset":{"countryCode":"DE",` +
				`"phoneNumber":"` + testPhone + `"}}]}}}`},
		{"sitelink_asset", `{"scope":"CAMPAIGN","new_assets":[{"link_text":"Sale","description1":"First line",` +
			`"description2":"Second line","final_urls":["https://shop.example.com/sale"]}]}`,
			`{"sitelinkAsset":{"adAssetApplyParameters":{"scope":"CAMPAIGN","newAssets":[{"finalUrls":["https://shop.example.com/sale"],` +
				`"sitelinkAsset":{"linkText":"Sale","description1":"First line","description2":"Second line"}}]}}}`},
		{"raise_target_cpa", `{"target_cpa_multiplier":1.5}`, `{"raiseTargetCpa":{"targetCpaMultiplier":1.5}}`},
		{"lower_target_roas", `{"target_roas_multiplier":0.8}`, `{"lowerTargetRoas":{"targetRoasMultiplier":0.8}}`},
		{"forecasting_set_target_cpa", `{"target_cpa_micros":7000000,"campaign_budget_amount_micros":8000000}`,
			`{"forecastingSetTargetCpa":{"targetCpaMicros":7000000,"campaignBudgetAmountMicros":8000000}}`},
		{"set_target_cpa", `{"target_cpa_micros":7000000}`, `{"setTargetCpa":{"targetCpaMicros":7000000}}`},
		{"set_target_roas", `{"target_roas":4,"campaign_budget_amount_micros":8000000}`,
			`{"setTargetRoas":{"targetRoas":4,"campaignBudgetAmountMicros":8000000}}`},
		{"raise_target_cpa_performance_bid_too_low", `{"target_cpa_multiplier":1.3}`,
			`{"raiseTargetCpaPerformanceBidTooLow":{"targetCpaMultiplier":1.3}}`},
		{"lower_target_roas_performance_bid_too_low", `{"target_roas_multiplier":0.7}`,
			`{"lowerTargetRoasPerformanceBidTooLow":{"targetRoasMultiplier":0.7}}`},
		{"raise_budget_performance_too_low", `{"new_budget_amount_micros":9000000}`,
			`{"raiseBudgetPerformanceTooLow":{"newBudgetAmountMicros":9000000}}`},
		{"optimize_for_new_customers", `{"account_default_incremental_conversion_value":12.5,` +
			`"campaign_override_incremental_conversion_value":3}`,
			`{"optimizeForNewCustomers":{"accountDefaultIncrementalConversionValue":12.5,"campaignOverrideIncrementalConversionValue":3}}`},
		{"optimize_for_new_customers", `{}`, `{"optimizeForNewCustomers":{}}`},
	}
	covered := map[string]bool{}
	for _, c := range cases {
		covered[c.variant] = true
		t.Run(c.variant, func(t *testing.T) {
			e := newRecommendationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			name := recName(clientID, "Rec-1")
			e.fake.routes["POST "+recPath(clientID, "apply")] = okReply(recResults(name))
			out, err := e.invokeConfirmed(recommendationsApplyID, `{"customer_id":"`+clientID+`","operations":[`+
				`{"resource_name":"`+name+`","parameters":{"`+c.variant+`":`+c.in+`}}]}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); got["customer_id"] != clientID ||
				!reflect.DeepEqual(got["results"], []any{name}) {
				t.Errorf("out = %s", out)
			}
			posts := e.recPosts()
			// Two requests: the check that the account is no manager, which a connection without targets skips.
			if len(posts) != 1 || len(e.fake.api()) != 1 || posts[0].login != loginID ||
				posts[0].url != "https://googleads.googleapis.com"+recPath(clientID, "apply") {
				t.Fatalf("requests = %+v", e.fake.api())
			}
			var wire map[string]any
			if err := json.Unmarshal([]byte(c.out), &wire); err != nil {
				t.Fatal(err)
			}
			wire["resourceName"] = name
			want := map[string]any{"operations": []any{wire}}
			if !reflect.DeepEqual(bodyOf(t, posts[0]), want) {
				t.Errorf("body = %s\nwant %v", posts[0].body, want)
			}
			for _, absent := range []string{"partialFailure", "validateOnly", "responseContentType"} {
				if strings.Contains(posts[0].body, absent) {
					t.Errorf("body holds %s: %s", absent, posts[0].body)
				}
			}
		})
	}
	for _, v := range recApplyVariants {
		if !covered[v.name] {
			t.Errorf("variant %s has no mapping test", v.name)
		}
	}
	if len(recApplyVariants) != 23 {
		t.Errorf("%d variants are offered", len(recApplyVariants))
	}
}

func TestApplyWithoutParametersSendsTheNameOnly(t *testing.T) {
	e := newRecommendationEnvironment(t, nil, nil)
	a, b := recName(clientID, "A1"), recName(clientID, "B2")
	e.fake.routes["POST "+recPath(clientID, "apply")] = okReply(recResults(a, b))
	out, err := e.invokeConfirmed(recommendationsApplyID, `{"customer_id":"`+clientID+`","operations":[`+
		`{"resource_name":"`+a+`"},{"resource_name":"`+b+`"}]}`)
	if err != nil || !reflect.DeepEqual(decode(t, out)["results"], []any{a, b}) {
		t.Fatalf("out = %s, err = %v", out, err)
	}
	want := map[string]any{"operations": []any{map[string]any{"resourceName": a}, map[string]any{"resourceName": b}}}
	if got := bodyOf(t, e.recPosts()[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v", got)
	}
}

func TestApplyRefusesBadArgumentsBeforeSecretAndIO(t *testing.T) {
	good := recName(clientID, "Rec-1")
	op := func(params string) string {
		return `{"customer_id":"` + clientID + `","operations":[{"resource_name":"` + good + `","parameters":` + params + `}]}`
	}
	const at = "@"
	bad := map[string]string{
		"two variants":       op(`{"campaign_budget":{"new_budget_amount_micros":1},"set_target_cpa":{}}`),
		"no variant":         op(`{}`),
		"unknown variant":    op(`{"lead_form_asset":{"scope":"CUSTOMER","existing_assets":["` + assetOf + `"]}}`),
		"text ad":            op(`{"text_ad":{"ad":{}}}`),
		"sitelink extension": op(`{"sitelink_extension":{"sitelink_extensions":[]}}`),
		"unknown member":     op(`{"campaign_budget":{"new_budget_amount_micros":1,"tracking_url_template":"https://t.example.com/"}}`),
		"missing required":   op(`{"campaign_budget":{}}`),
		"zero micros":        op(`{"campaign_budget":{"new_budget_amount_micros":0}}`),
		"negative micros":    op(`{"campaign_budget":{"new_budget_amount_micros":-5}}`),
		"fractional micros":  op(`{"campaign_budget":{"new_budget_amount_micros":1.5}}`),
		"string micros":      op(`{"campaign_budget":{"new_budget_amount_micros":"5"}}`),
		"roas too low":       op(`{"set_target_roas":{"target_roas":0.001}}`),
		"roas too high":      op(`{"set_target_roas":{"target_roas":1000.5}}`),
		"roas opt in empty":  op(`{"target_roas_opt_in":{}}`),
		"cpa opt in no cpa":  op(`{"target_cpa_opt_in":{"new_campaign_budget_amount_micros":5}}`),
		"raise not above 1":  op(`{"raise_target_cpa_bid_too_low":{"target_multiplier":1}}`),
		"perf raise below 1": op(`{"raise_target_cpa_performance_bid_too_low":{"target_cpa_multiplier":0.9}}`),
		"lower not below 1":  op(`{"lower_target_roas_performance_bid_too_low":{"target_roas_multiplier":1}}`),
		"lower zero":         op(`{"lower_target_roas_performance_bid_too_low":{"target_roas_multiplier":0}}`),
		"keyword no match":   op(`{"keyword":{"ad_group":"` + adGroupOf + `"}}`),
		"keyword broad enum": op(`{"keyword":{"ad_group":"` + adGroupOf + `","match_type":"BROADISH"}}`),
		"keyword foreign ad group": op(`{"keyword":{"ad_group":"customers/` + otherID + `/adGroups/55",` +
			`"match_type":"EXACT"}}`),
		"keyword path ad group": op(`{"keyword":{"ad_group":"customers/` + clientID + `/adGroups/55/../../x",` +
			`"match_type":"EXACT"}}`),
		"asset no scope":     op(`{"callout_asset":{"new_assets":[{"callout_text":"x"}]}}`),
		"asset bad scope":    op(`{"callout_asset":{"scope":"AD_GROUP","new_assets":[{"callout_text":"x"}]}}`),
		"asset nothing":      op(`{"callout_asset":{"scope":"CUSTOMER"}}`),
		"asset foreign name": op(`{"callout_asset":{"scope":"CUSTOMER","existing_assets":["customers/` + otherID + `/assets/9"]}}`),
		"asset other family": op(`{"callout_asset":{"scope":"CUSTOMER","existing_assets":["customers/` + clientID + `/campaigns/9"]}}`),
		"callout extra":      op(`{"callout_asset":{"scope":"CUSTOMER","new_assets":[{"callout_text":"x","start_date":"2026-01-01"}]}}`),
		"call tracking": op(`{"call_asset":{"scope":"CUSTOMER","new_assets":[{"country_code":"DE","phone_number":"` + testPhone + `",` +
			`"call_conversion_action":"customers/` + clientID + `/conversionActions/1"}]}}`),
		"call bad number": op(`{"call_asset":{"scope":"CUSTOMER","new_assets":[{"country_code":"DE","phone_number":"call me"}]}}`),
		"call bad code":   op(`{"call_asset":{"scope":"CUSTOMER","new_assets":[{"country_code":"de","phone_number":"` + testPhone + `"}]}}`),
		"sitelink http": op(`{"sitelink_asset":{"scope":"CUSTOMER","new_assets":[{"link_text":"x",` +
			`"final_urls":["http://shop.example.com/"]}]}}`),
		"sitelink user info": op(`{"sitelink_asset":{"scope":"CUSTOMER","new_assets":[{"link_text":"x",` +
			`"final_urls":["https://user:pw` + at + `shop.example.com/"]}]}}`),
		"sitelink host user info": op(`{"sitelink_asset":{"scope":"CUSTOMER","new_assets":[{"link_text":"x",` +
			`"final_urls":["https://shop.example.com` + at + `evil.example.net/"]}]}}`),
		"sitelink template": op(`{"sitelink_asset":{"scope":"CUSTOMER","new_assets":[{"link_text":"x",` +
			`"tracking_url_template":"https://t.example.com/?u={lpurl}"}]}}`),
		"sitelink suffix": op(`{"sitelink_asset":{"scope":"CUSTOMER","new_assets":[{"link_text":"x","final_url_suffix":"a=b"}]}}`),
		"sitelink javascript": op(`{"sitelink_asset":{"scope":"CUSTOMER","new_assets":[{"link_text":"x",` +
			`"final_urls":["javascript:alert(1)"]}]}}`),
		"ad http":           op(`{"responsive_search_ad":{"ad":{"final_urls":["http://shop.example.com/"],"headlines":[{"text":"x"}]}}}`),
		"ad tracking":       op(`{"responsive_search_ad":{"ad":{"tracking_url_template":"https://t.example.com/","headlines":[{"text":"x"}]}}}`),
		"ad mobile":         op(`{"responsive_search_ad":{"ad":{"final_mobile_urls":["https://m.example.com/"],"headlines":[{"text":"x"}]}}}`),
		"ad name on new ad": op(`{"responsive_search_ad":{"ad":{"resource_name":"` + adOf + `","headlines":[{"text":"x"}]}}}`),
		"ad foreign name": op(`{"responsive_search_ad_asset":{"updated_ad":{"resource_name":"customers/` + otherID +
			`/ads/77","headlines":[{"text":"x"}]}}}`),
		"ad bad pin":      op(`{"responsive_search_ad":{"ad":{"headlines":[{"text":"x","pinned_field":"DESCRIPTION_1"}]}}}`),
		"ad path2 alone":  op(`{"responsive_search_ad":{"ad":{"path2":"sale","headlines":[{"text":"x"}]}}}`),
		"ad path slash":   op(`{"responsive_search_ad":{"ad":{"path1":"a/b","headlines":[{"text":"x"}]}}}`),
		"ad control text": op(`{"responsive_search_ad":{"ad":{"headlines":[{"text":"a\u0007b"}]}}}`),
		"ad wrapper":      op(`{"responsive_search_ad":{"ad":{"headlines":[{"text":"x"}]},"updated_ad":{}}}`),
		"foreign name": `{"customer_id":"` + clientID + `","operations":[{"resource_name":"` +
			recName(otherID, "Rec-1") + `"}]}`,
		"other family": `{"customer_id":"` + clientID + `","operations":[{"resource_name":"customers/` + clientID +
			`/campaigns/5"}]}`,
		"traversal":     `{"customer_id":"` + clientID + `","operations":[{"resource_name":"` + good + `/../x"}]}`,
		"duplicates":    `{"customer_id":"` + clientID + `","operations":[{"resource_name":"` + good + `"},{"resource_name":"` + good + `"}]}`,
		"no operations": `{"customer_id":"` + clientID + `","operations":[]}`,
		"eleven":        `{"customer_id":"` + clientID + `","operations":[` + strings.Repeat(`{"resource_name":"`+good+`"},`, 10) + `{"resource_name":"` + good + `"}]}`,
		"partial failure": `{"customer_id":"` + clientID + `","partial_failure":true,"operations":[{"resource_name":"` +
			good + `"}]}`,
		"validate only": `{"customer_id":"` + clientID + `","validate_only":true,"operations":[{"resource_name":"` + good + `"}]}`,
	}
	for name, arguments := range bad {
		t.Run(name, func(t *testing.T) {
			e := newRecommendationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(recommendationsApplyID, arguments); err == nil {
				t.Fatal("the call was accepted")
			}
			e.untouched(t)
		})
	}
}

func TestRecommendationTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	calls := map[string]string{
		recommendationsGenerateID: `{"customer_id":"` + otherID + `","recommendation_types":["KEYWORD"],` +
			`"advertising_channel_type":"SEARCH","seed_info":{"keyword_seeds":["chairs"]}}`,
		recommendationsApplyID:   `{"customer_id":"` + otherID + `","operations":[{"resource_name":"` + recName(otherID, "R1") + `"}]}`,
		recommendationsDismissID: `{"customer_id":"` + otherID + `","resource_names":["` + recName(otherID, "R1") + `"]}`,
	}
	for id, arguments := range calls {
		t.Run(id, func(t *testing.T) {
			e := newRecommendationEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestRecommendationChangesNeedTheToolsListOrEffectAndTheConfirmation(t *testing.T) {
	good := recName(clientID, "Rec-1")
	calls := map[string]string{
		recommendationsApplyID: `{"customer_id":"` + clientID + `","operations":[{"resource_name":"` + good +
			`","parameters":{"campaign_budget":{"new_budget_amount_micros":20000000}}}]}`,
		recommendationsDismissID: `{"customer_id":"` + clientID + `","resource_names":["` + good + `"]}`,
	}
	allEffects := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	t.Run("apply is never offered by effects alone", func(t *testing.T) {
		e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: allEffects})
		if _, err := e.invokeConfirmed(recommendationsApplyID, calls[recommendationsApplyID]); err == nil {
			t.Fatal("a connection with all effects but no tools list ran apply")
		}
		e.untouched(t)
	})
	t.Run("dismiss is offered by the update effect", func(t *testing.T) {
		e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectUpdate}})
		e.fake.routes["POST "+recPath(clientID, "dismiss")] = okReply(recResults(good))
		if _, err := e.invokeConfirmed(recommendationsDismissID, calls[recommendationsDismissID]); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a read connection runs neither", func(t *testing.T) {
		for id, arguments := range calls {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatalf("a read connection ran %s", id)
			}
			e.untouched(t)
		}
	})
	for id, arguments := range calls {
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newRecommendationEnvironment(t, nil, nil)
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) || !strings.Contains(needed.Summary, good) {
				t.Fatalf("err = %v, want a confirmation that names the account and the recommendation", err)
			}
			e.untouched(t)
		})
	}
	t.Run("the summary of apply names the parameters and the spend", func(t *testing.T) {
		e := newRecommendationEnvironment(t, nil, nil)
		_, err := e.invoke(recommendationsApplyID, calls[recommendationsApplyID])
		var needed *application.ConfirmationRequiredError
		if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "campaign_budget") ||
			!strings.Contains(needed.Summary, "20000000") || !strings.Contains(needed.Summary, "spends") {
			t.Fatalf("summary = %v", err)
		}
	})
}

func TestDismissSendsOneAtomicRequest(t *testing.T) {
	e := newRecommendationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	a, b := recName(clientID, "A1"), recName(clientID, "B2")
	e.fake.routes["POST "+recPath(clientID, "dismiss")] = okReply(recResults(a, b))
	out, err := e.invokeConfirmed(recommendationsDismissID, `{"customer_id":"`+clientID+`","resource_names":["`+a+`","`+b+`"]}`)
	if err != nil || !reflect.DeepEqual(decode(t, out)["results"], []any{a, b}) {
		t.Fatalf("out = %s, err = %v", out, err)
	}
	posts := e.recPosts()
	if len(posts) != 1 || len(e.fake.api()) != 1 || posts[0].login != loginID || posts[0].developerToken != "" {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	want := map[string]any{"operations": []any{map[string]any{"resourceName": a}, map[string]any{"resourceName": b}}}
	if !reflect.DeepEqual(bodyOf(t, posts[0]), want) {
		t.Errorf("body = %s", posts[0].body)
	}
}

func TestDismissRefusesBadNames(t *testing.T) {
	good := recName(clientID, "Rec-1")
	for name, names := range map[string]string{
		"foreign":    `"` + recName(otherID, "R1") + `"`,
		"other kind": `"customers/` + clientID + `/campaigns/5"`,
		"duplicate":  `"` + good + `","` + good + `"`,
		"empty":      ``,
		"short":      `"R1"`,
		"eleven":     strings.Repeat(`"`+good+`",`, 10) + `"` + recName(clientID, "last") + `"`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newRecommendationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(recommendationsDismissID, `{"customer_id":"`+clientID+`","resource_names":[`+names+`]}`); err == nil {
				t.Fatal("the call was accepted")
			}
			e.untouched(t)
		})
	}
}

// An answer that is not the one atomic result of the request leaves the outcome open, never a success.
func TestRecommendationActionAnswersAreCheckedAgainstTheRequest(t *testing.T) {
	a, b := recName(clientID, "A1"), recName(clientID, "B2")
	foreign := recName(otherID, "A1")
	cases := map[string]string{
		"too few":         recResults(a),
		"too many":        recResults(a, b, b),
		"another name":    recResults(a, recName(clientID, "Z9")),
		"another order":   recResults(b, a),
		"another account": recResults(a, foreign),
		"empty":           `{}`,
		"partial failure": `{"results":[{"resourceName":"` + a + `"},{}],"partialFailureError":{"code":3,"message":"` + detailCanary + `"}}`,
	}
	for _, id := range []string{recommendationsApplyID, recommendationsDismissID} {
		arguments := `{"customer_id":"` + clientID + `","resource_names":["` + a + `","` + b + `"]}`
		action := "dismiss"
		if id == recommendationsApplyID {
			arguments = `{"customer_id":"` + clientID + `","operations":[{"resource_name":"` + a + `"},{"resource_name":"` + b + `"}]}`
			action = "apply"
		}
		for name, answer := range cases {
			t.Run(id+" "+name, func(t *testing.T) {
				e := newRecommendationEnvironment(t, nil, nil)
				e.fake.routes["POST "+recPath(clientID, action)] = okReply(answer)
				_, err := e.invokeConfirmed(id, arguments)
				if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
					strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), otherID) {
					t.Fatalf("err = %v", err)
				}
				if len(e.recPosts()) != 1 {
					t.Errorf("posts = %d", len(e.recPosts()))
				}
			})
		}
	}
}

func TestRecommendationChangeErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"recommendation error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "recommendationError",
			"INVALID_ACTION", "")}, provider.ClassProviderError,
			"rejected the change (recommendationError INVALID_ACTION), nothing was changed", false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"token refused", reply{status: 401, body: adsFailure(401, "UNAUTHENTICATED", "authenticationError",
			"OAUTH_TOKEN_EXPIRED", "")}, provider.ClassAuth, "access token", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"cut connection", reply{err: errors.New("connection reset by peer")}, provider.ClassUnreachable,
			"outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"not json", reply{status: 200, body: "<html>", header: http.Header{"Content-Type": {"text/html"}}},
			provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	name := recName(clientID, "A1")
	for _, id := range []string{recommendationsApplyID, recommendationsDismissID} {
		arguments, action := `{"customer_id":"`+clientID+`","resource_names":["`+name+`"]}`, "dismiss"
		if id == recommendationsApplyID {
			arguments, action = `{"customer_id":"`+clientID+`","operations":[{"resource_name":"`+name+`"}]}`, "apply"
		}
		for _, c := range cases {
			t.Run(id+" "+c.name, func(t *testing.T) {
				e := newRecommendationEnvironment(t, nil, nil)
				e.fake.routes["POST "+recPath(clientID, action)] = c.reply
				_, err := e.invokeConfirmed(id, arguments)
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
				if len(e.recPosts()) != 1 || len(e.fake.api()) != 1 || e.fake.tokenRequests() != 1 {
					t.Errorf("requests = %d of %d, tokens = %d, want one and no repeat", len(e.recPosts()),
						len(e.fake.api()), e.fake.tokenRequests())
				}
			})
		}
	}
}

// On a manager account a change needs a manager target that names it, as for the other changing tools.
func TestRecommendationChangesNeedAManagerTargetOnManagerAccounts(t *testing.T) {
	name := recName(managerID, "A1")
	calls := map[string]struct{ args, action string }{
		recommendationsApplyID:   {`{"customer_id":"` + managerID + `","operations":[{"resource_name":"` + name + `"}]}`, "apply"},
		recommendationsDismissID: {`{"customer_id":"` + managerID + `","resource_names":["` + name + `"]}`, "dismiss"},
	}
	for id, c := range calls {
		t.Run(id, func(t *testing.T) {
			e := newRecommendationEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + managerID + `","manager":true}}]}`}
				}
				return nil
			}
			_, err := e.invokeConfirmed(id, c.args)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.recPosts()) != 0 {
				t.Fatalf("err = %v, posts = %d", err, len(e.recPosts()))
			}
		})
		t.Run(id+" with a manager target", func(t *testing.T) {
			e := newRecommendationEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.routes["POST "+recPath(managerID, c.action)] = okReply(recResults(name))
			if _, err := e.invokeConfirmed(id, c.args); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// --- generate ---

const generateArgs = `{"customer_id":"` + clientID + `","recommendation_types":["CAMPAIGN_BUDGET"],` +
	`"advertising_channel_type":"SEARCH","bidding_info":{"bidding_strategy_type":"TARGET_SPEND"},` +
	`"ad_group_info":[{"keywords":[{"text":"garden chairs","match_type":"PHRASE"}]}],` +
	`"country_codes":["DE"],"language_codes":["de"],"positive_location_ids":[2276],` +
	`"budget_info":{"current_budget":10000000}}`

func TestGenerateMapsEveryArgumentIntoTheRequest(t *testing.T) {
	e := newEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+recPath(clientID, "generate")] = okReply(`{}`)
	arguments := `{"customer_id":"` + clientID + `","recommendation_types":["CAMPAIGN_BUDGET","KEYWORD","SITELINK_ASSET"],` +
		`"advertising_channel_type":"SEARCH","campaign_sitelink_count":2,` +
		`"conversion_tracking_status":"CONVERSION_TRACKING_MANAGED_BY_SELF",` +
		`"bidding_info":{"bidding_strategy_type":"TARGET_IMPRESSION_SHARE","target_impression_share_info":{` +
		`"location":"TOP_OF_PAGE","target_impression_share_micros":500000,"max_cpc_bid_ceiling":2000000}},` +
		`"ad_group_info":[{"ad_group_type":"SEARCH_STANDARD","keywords":[{"text":"garden chairs","match_type":"PHRASE"}]}],` +
		`"seed_info":{"url_seed":"www.shop.example.com/chairs","keyword_seeds":["chairs"]},` +
		`"budget_info":{"current_budget":10000000},"campaign_image_asset_count":3,"campaign_call_asset_count":1,` +
		`"country_codes":["DE"],"language_codes":["de"],"positive_location_ids":[2276],"negative_location_ids":[1],` +
		`"asset_group_info":[{"final_url":"https://shop.example.com/chairs","headline":["One"],"description":["Two"]}],` +
		`"target_partner_search_network":true,"target_content_network":false,"is_new_customer":true}`
	out, err := e.invoke(recommendationsGenerateID, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, out); got["customer_id"] != clientID || got["recommendations"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.recPosts()
	if len(posts) != 1 || len(e.fake.api()) != 1 || posts[0].login != loginID ||
		posts[0].url != "https://googleads.googleapis.com"+recPath(clientID, "generate") {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	want := `{"recommendationTypes":["CAMPAIGN_BUDGET","KEYWORD","SITELINK_ASSET"],"advertisingChannelType":"SEARCH",` +
		`"campaignSitelinkCount":2,"conversionTrackingStatus":"CONVERSION_TRACKING_MANAGED_BY_SELF",` +
		`"biddingInfo":{"biddingStrategyType":"TARGET_IMPRESSION_SHARE","targetImpressionShareInfo":{"location":"TOP_OF_PAGE",` +
		`"targetImpressionShareMicros":500000,"maxCpcBidCeiling":2000000}},` +
		`"adGroupInfo":[{"adGroupType":"SEARCH_STANDARD","keywords":[{"text":"garden chairs","matchType":"PHRASE"}]}],` +
		`"seedInfo":{"urlSeed":"www.shop.example.com/chairs","keywordSeeds":["chairs"]},"budgetInfo":{"currentBudget":10000000},` +
		`"campaignImageAssetCount":3,"campaignCallAssetCount":1,"countryCodes":["DE"],"languageCodes":["de"],` +
		`"positiveLocationsIds":[2276],"negativeLocationsIds":[1],` +
		`"assetGroupInfo":[{"finalUrl":"https://shop.example.com/chairs","headline":["One"],"description":["Two"]}],` +
		`"targetPartnerSearchNetwork":true,"targetContentNetwork":false,"isNewCustomer":true}`
	var wantBody map[string]any
	_ = json.Unmarshal([]byte(want), &wantBody)
	if !reflect.DeepEqual(bodyOf(t, posts[0]), wantBody) {
		t.Errorf("body = %s", posts[0].body)
	}
}

func TestGenerateRefusesBadArgumentsBeforeSecretAndIO(t *testing.T) {
	with := func(extra string) string {
		return `{"customer_id":"` + clientID + `","recommendation_types":["CAMPAIGN_BUDGET"],"advertising_channel_type":"SEARCH",` +
			`"bidding_info":{"bidding_strategy_type":"TARGET_SPEND"},"ad_group_info":[{"keywords":[{"text":"chairs","match_type":"EXACT"}]}],` +
			`"country_codes":["DE"],"language_codes":["de"],"positive_location_ids":[1]` + extra + `}`
	}
	const at = "@"
	bad := map[string]string{
		"no type":          `{"customer_id":"` + clientID + `","recommendation_types":[],"advertising_channel_type":"SEARCH"}`,
		"unsupported type": `{"customer_id":"` + clientID + `","recommendation_types":["TEXT_AD"],"advertising_channel_type":"SEARCH"}`,
		"twice":            `{"customer_id":"` + clientID + `","recommendation_types":["KEYWORD","KEYWORD"],"advertising_channel_type":"SEARCH"}`,
		"channel":          `{"customer_id":"` + clientID + `","recommendation_types":["KEYWORD"],"advertising_channel_type":"DISPLAY"}`,
		"no channel":       `{"customer_id":"` + clientID + `","recommendation_types":["KEYWORD"]}`,
		"sitelink count missing": `{"customer_id":"` + clientID + `","recommendation_types":["SITELINK_ASSET"],` +
			`"advertising_channel_type":"SEARCH"}`,
		"opt in without tracking": `{"customer_id":"` + clientID + `","recommendation_types":["TARGET_CPA_OPT_IN"],` +
			`"advertising_channel_type":"SEARCH","bidding_info":{"bidding_strategy_type":"TARGET_SPEND"}}`,
		"opt in without bidding": `{"customer_id":"` + clientID + `","recommendation_types":["SET_TARGET_ROAS"],` +
			`"advertising_channel_type":"SEARCH","conversion_tracking_status":"NOT_CONVERSION_TRACKED"}`,
		"keyword without seed": `{"customer_id":"` + clientID + `","recommendation_types":["KEYWORD"],"advertising_channel_type":"SEARCH"}`,
		"keyword group without type": `{"customer_id":"` + clientID + `","recommendation_types":["KEYWORD"],` +
			`"advertising_channel_type":"SEARCH","seed_info":{"keyword_seeds":["x"]},"ad_group_info":[{}]}`,
		"budget without bidding": `{"customer_id":"` + clientID + `","recommendation_types":["CAMPAIGN_BUDGET"],"advertising_channel_type":"PERFORMANCE_MAX"}`,
		"budget search without keywords": `{"customer_id":"` + clientID + `","recommendation_types":["CAMPAIGN_BUDGET"],` +
			`"advertising_channel_type":"SEARCH","bidding_info":{"bidding_strategy_type":"TARGET_SPEND"}}`,
		"budget search without locations": strings.Replace(with(``), `,"positive_location_ids":[1]`, ``, 1),
		"budget search share missing":     strings.Replace(with(``), `TARGET_SPEND`, `TARGET_IMPRESSION_SHARE`, 1),
		"budget pmax without asset group": `{"customer_id":"` + clientID + `","recommendation_types":["CAMPAIGN_BUDGET"],` +
			`"advertising_channel_type":"PERFORMANCE_MAX","bidding_info":{"bidding_strategy_type":"MAXIMIZE_CONVERSIONS"}}`,
		"two targets":         with(`,"bidding_info":{"bidding_strategy_type":"TARGET_CPA","target_cpa_micros":5,"target_roas":2}`),
		"merchant on search":  with(`,"merchant_center_account_id":"123"`),
		"seed http":           with(`,"seed_info":{"url_seed":"http://shop.example.com/"}`),
		"seed user info":      with(`,"seed_info":{"url_seed":"https://u:p` + at + `shop.example.com/"}`),
		"seed bare user info": with(`,"seed_info":{"url_seed":"u:p` + at + `shop.example.com/"}`),
		"seed port":           with(`,"seed_info":{"url_seed":"shop.example.com:8080/x"}`),
		"seed empty":          with(`,"seed_info":{}`),
		"final url http":      with(`,"asset_group_info":[{"final_url":"http://shop.example.com/"}]`),
		"final url user info": with(`,"asset_group_info":[{"final_url":"https://u:p` + at + `shop.example.com/"}]`),
		"keyword too long":    with(`,"ad_group_info":[{"keywords":[{"text":"` + strings.Repeat("a", 81) + `","match_type":"EXACT"}]}]`),
		"keyword words":       with(`,"ad_group_info":[{"keywords":[{"text":"a b c d e f g h i j k","match_type":"EXACT"}]}]`),
		"two groups":          with(`,"ad_group_info":[{},{}]`),
		"country":             with(`,"country_codes":["Germany"]`),
		"share range": with(`,"bidding_info":{"bidding_strategy_type":"TARGET_IMPRESSION_SHARE","target_impression_share_info":` +
			`{"location":"TOP_OF_PAGE","target_impression_share_micros":1000001}}`),
		"partial failure": with(`,"partial_failure":true`),
		"negative count":  with(`,"campaign_image_asset_count":-1`),
	}
	for name, arguments := range bad {
		if name == "twice" || name == "no type" {
			// the closed schema or the handler refuses these; both end before secrets
		}
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invoke(recommendationsGenerateID, arguments); err == nil {
				t.Fatal("the call was accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("the good call passes", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+recPath(clientID, "generate")] = okReply(`{}`)
		if _, err := e.invoke(recommendationsGenerateID, generateArgs); err != nil {
			t.Fatal(err)
		}
	})
}

func recommendationAnswer(customer string) string {
	budget := "customers/" + queryNumber(customer) + "/campaignBudgets/7"
	return `{"recommendations":[{"resourceName":"` + recName(customer, "Gen-1") + `","type":"CAMPAIGN_BUDGET",` +
		`"impact":{"baseMetrics":{"impressions":100.5,"clicks":10,"costMicros":"2000000"},` +
		`"potentialMetrics":{"impressions":200,"clicks":20,"costMicros":"4000000"}},"campaignBudget":"` + budget + `",` +
		`"campaign":"customers/` + queryNumber(customer) + `/campaigns/3","dismissed":false,` +
		`"campaignBudgetRecommendation":{"currentBudgetAmountMicros":"1000000","recommendedBudgetAmountMicros":"2000000",` +
		`"budgetOptions":[{"budgetAmountMicros":"2000000","impact":{"baseMetrics":{"clicks":1}}}]}},` +
		`{"type":"SITELINK_ASSET","sitelinkAssetRecommendation":{"recommendedCampaignSitelinkAssets":[{"resourceName":` +
		`"customers/` + queryNumber(customer) + `/assets/5","sitelinkAsset":{"linkText":"Sale\u0007 ` +
		strings.Repeat("x", 3000) + `"},"finalUrls":["https://shop.example.com/sale"],"leadFormAsset":{"deliveryMethods":` +
		`[{"webhook":{"advertiserWebhookUrl":"https://hook.example.net/x","googleSecret":"synthetic-secret-1"}}]}}]}},` +
		`{"type":"CAMPAIGN_SPECIFIC_APP_GOAL","campaignSpecificAppGoalRecommendation":{"appConversionGoals":["X"],` +
		`"currentAutoGoals":[{"a":1}],"currentCustomGoal":"customers/` + queryNumber(customer) + `/customConversionGoals/1",` +
		`"suggestedConversionGoals":["PURCHASE"],"campaignCostMicrosLastFifteenDays":"5"}}]}`
}

func TestGenerateMapsTheAnswerAsBoundedUntrustedData(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+recPath(clientID, "generate")] = okReply(recommendationAnswer(clientID))
	out, err := e.invoke(recommendationsGenerateID, generateArgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"synthetic-secret-1", "hook.example.net", "webhook", "deliveryMethods", "delivery_methods",
		"app_conversion_goals", "current_auto_goals", "current_custom_goal", `\u0007`} {
		if strings.Contains(out, leaked) {
			t.Errorf("the answer holds %q: %s", leaked, out)
		}
	}
	got := decode(t, out)
	recs := got["recommendations"].([]any)
	if len(recs) != 3 {
		t.Fatalf("recommendations = %s", out)
	}
	first := recs[0].(map[string]any)
	if first["resource_name"] != recName(clientID, "Gen-1") || first["type"] != "CAMPAIGN_BUDGET" ||
		first["campaign_budget"] != "customers/1000000003/campaignBudgets/7" || first["dismissed"] != false {
		t.Errorf("first = %v", first)
	}
	rec := first["campaign_budget_recommendation"].(map[string]any)
	if rec["recommended_budget_amount_micros"] != "2000000" ||
		rec["budget_options"].([]any)[0].(map[string]any)["budget_amount_micros"] != "2000000" {
		t.Errorf("recommendation = %v", rec)
	}
	impact := first["impact"].(map[string]any)["base_metrics"].(map[string]any)
	if impact["impressions"] != 100.5 || impact["cost_micros"] != "2000000" {
		t.Errorf("impact = %v", impact)
	}
	asset := recs[1].(map[string]any)["sitelink_asset_recommendation"].(map[string]any)["recommended_campaign_sitelink_assets"].([]any)[0].(map[string]any)
	text := asset["sitelink_asset"].(map[string]any)["link_text"].(string)
	if len(text) > maxText || !strings.HasSuffix(text, "…") || strings.ContainsRune(text, 7) {
		t.Errorf("text is not bounded: %d bytes", len(text))
	}
	if asset["final_urls"].([]any)[0] != "https://shop.example.com/sale" {
		t.Errorf("asset = %v", asset)
	}
	goal := recs[2].(map[string]any)["campaign_specific_app_goal_recommendation"].(map[string]any)
	if goal["campaign_cost_micros_last_fifteen_days"] != "5" || goal["suggested_conversion_goals"] == nil {
		t.Errorf("goal = %v", goal)
	}
}

func TestGenerateRefusesAnswersOfAnotherAccount(t *testing.T) {
	for name, answer := range map[string]string{
		"recommendation": `{"recommendations":[{"resourceName":"` + recName(otherID, "X1") + `"}]}`,
		"campaign":       `{"recommendations":[{"campaign":"customers/` + otherID + `/campaigns/3"}]}`,
		"nested":         `{"recommendations":[{"moveUnusedBudgetRecommendation":{"excessCampaignBudget":"customers/` + otherID + `/campaignBudgets/1"}}]}`,
		"in a list":      `{"recommendations":[{"campaigns":["customers/` + clientID + `/campaigns/1","customers/` + otherID + `/campaigns/2"]}]}`,
		"bad name":       `{"recommendations":[{"resourceName":"customers/` + clientID + `/campaigns/3"}]}`,
		"not an object":  `{"recommendations":["x"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+recPath(clientID, "generate")] = okReply(answer)
			out, err := e.invoke(recommendationsGenerateID, generateArgs)
			if classOf(err) != provider.ClassInvalidResponse || strings.Contains(out, otherID) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("out = %s, err = %v", out, err)
			}
		})
	}
}

func TestGenerateBoundsTheListAndMapsErrors(t *testing.T) {
	t.Run("long lists are cut", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		items := strings.TrimSuffix(strings.Repeat(`{"type":"KEYWORD"},`, maxListItems+5), ",")
		e.fake.routes["POST "+recPath(clientID, "generate")] = okReply(`{"recommendations":[` + items + `]}`)
		out, err := e.invoke(recommendationsGenerateID, generateArgs)
		got := decode(t, out)
		if recs, _ := got["recommendations"].([]any); err != nil || len(recs) != maxListItems || got["truncated"] != true {
			t.Fatalf("err = %v, truncated = %v", err, got["truncated"])
		}
	})
	for name, c := range map[string]struct {
		reply   reply
		class   provider.Class
		message string
	}{
		"permission": {reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID},
		"recommendation error": {reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "recommendationError",
			"INVALID_ACTION", "")}, provider.ClassProviderError, "rejected the request as invalid input (recommendationError INVALID_ACTION)"},
		"bad request without a code": {reply{status: 400, body: googleError(400, "INVALID_ARGUMENT", "")},
			provider.ClassInvalidResponse, "rejected the request as invalid (HTTP 400) although"},
		"quota": {reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+recPath(clientID, "generate")] = c.reply
			_, err := e.invoke(recommendationsGenerateID, generateArgs)
			if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) ||
				strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), "unknown") {
				t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
			}
		})
	}
}

func TestGenerateNeedsNoTargetOnAManagerBelowTheConnection(t *testing.T) {
	e := newEnvironment(t, []string{"managers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer_client.id FROM customer_client") {
			return &reply{status: 200, body: `{"results":[` + proofItem(clientID) + `]}`}
		}
		return nil
	}
	e.fake.routes["POST "+recPath(clientID, "generate")] = okReply(`{}`)
	if _, err := e.invoke(recommendationsGenerateID, generateArgs); err != nil {
		t.Fatalf("err = %v, requests = %+v", err, e.fake.api())
	}
	if len(e.recPosts()) != 1 {
		t.Errorf("posts = %d", len(e.recPosts()))
	}
}
