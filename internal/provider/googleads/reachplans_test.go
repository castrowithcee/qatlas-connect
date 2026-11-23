package googleads

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var reachToolIDs = []string{reachPlansListLocationsID, reachPlansListProductsID, reachPlansGenerateForecastID,
	reachPlansGenerateConversionRatesID, reachPlansListUserListsID, reachPlansListUserInterestsID}

const (
	reachRoot   = "/v26:"
	reachLists  = `"customers/` + clientID + `/userLists/7"`
	reachListsX = `"customers/` + otherID + `/userLists/7"`
)

func reachForecastPath(id string) string { return "/v26/customers/" + id + ":generateReachForecast" }

const reachForecastArgs = `{"customer_id":"` + clientID + `","campaign_duration":{"duration_in_days":28},` +
	`"planned_products":[{"plannable_product_code":"VIDEO_VIEW_CAMPAIGN","budget_micros":5000000000}]}`

// reachCalls is one valid call and the route of its answer per tool.
var reachCalls = map[string]struct{ arguments, route string }{
	reachPlansListLocationsID:           {`{}`, "POST " + reachRoot + "listPlannableLocations"},
	reachPlansListProductsID:            {`{"plannable_location_id":"2276"}`, "POST " + reachRoot + "listPlannableProducts"},
	reachPlansGenerateForecastID:        {reachForecastArgs, "POST " + reachForecastPath(clientID)},
	reachPlansGenerateConversionRatesID: {`{"customer_id":"` + clientID + `"}`, "POST " + reachRoot + "generateConversionRates"},
	reachPlansListUserListsID:           {`{"customer_id":"` + clientID + `"}`, "POST " + reachRoot + "listPlannableUserLists"},
	reachPlansListUserInterestsID:       {`{"customer_id":"` + clientID + `"}`, "POST " + reachRoot + "listPlannableUserInterests"},
}

func TestReachToolsAreReadOnlyAndInTheReadProfile(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range reachToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != capability.EffectRead || !d.Risk.OpenWorld ||
			d.Risk.Confirmation != capability.ConfirmationNone || d.RequiresToolAllowList || len(d.OutputSchema) == 0 {
			t.Fatalf("%s = %+v", id, d)
		}
		if !slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("%s is not in the profile read", id)
		}
		if !reflect.DeepEqual(metadata.Token.Scopes[id], []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, metadata.Token.Scopes[id])
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "validate_only", "partial_failure", "path", "url"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		_, hasCustomer := input.Properties["customer_id"]
		if hasCustomer != (id != reachPlansListLocationsID && id != reachPlansListProductsID) {
			t.Errorf("%s: customer_id offered = %v", id, hasCustomer)
		}
	}
}

func TestReachToolsSendOneReadAndMapTheAnswer(t *testing.T) {
	answers := map[string]string{
		reachPlansListLocationsID: `{"plannableLocations":[{"id":"2276","name":"Germany","locationType":"Country",` +
			`"countryCode":"DE"},{"id":"1003854","name":"Bavaria","parentCountryId":"2276"}]}`,
		reachPlansListProductsID: `{"productMetadata":[{"plannableProductCode":"VIDEO_VIEW_CAMPAIGN",` +
			`"plannableProductName":"Video view","plannableProductCoreAttributes":{"buyingMethod":"AUCTION"},` +
			`"plannableTargeting":{"networks":["YOUTUBE"],"devices":[{"type":"MOBILE"}]}}]}`,
		reachPlansGenerateForecastID: `{"onTargetAudienceMetrics":{"censusAudienceSize":"1000","youtubeAudienceSize":"900"},` +
			`"reachCurve":{"reachForecasts":[{"costMicros":"5000000000","forecast":{"onTargetReach":"123","totalImpressions":"456"},` +
			`"plannedProductReachForecasts":[{"plannableProductCode":"VIDEO_VIEW_CAMPAIGN","costMicros":"5000000000",` +
			`"plannedProductForecast":{"onTargetReach":"123","conversions":1.5}}]}]}}`,
		reachPlansGenerateConversionRatesID: `{"conversionRateSuggestions":[{"plannableProductCode":"DEMAND_GEN",` +
			`"conversionRate":0.02,"conversionRateModel":"INVENTORY_MEDIAN"}]}`,
		reachPlansListUserListsID: `{"plannableUserLists":[{"displayName":"Visitors","plannableStatus":"PLANNABLE",` +
			`"userListInfo":{"userList":` + reachLists + `},"userListType":"REMARKETING"}]}`,
		reachPlansListUserInterestsID: `{"plannableUserInterests":[{"userInterest":{"userInterestCategory":` +
			`"customers/` + clientID + `/userInterests/80"},"userInterestDisplayName":"Autos","userInterestType":"AFFINITY"}]}`,
	}
	wantMember := map[string]string{reachPlansListLocationsID: "plannable_locations", reachPlansListProductsID: "product_metadata",
		reachPlansGenerateForecastID: "reach_curve", reachPlansGenerateConversionRatesID: "conversion_rate_suggestions",
		reachPlansListUserListsID: "plannable_user_lists", reachPlansListUserInterestsID: "plannable_user_interests"}
	for _, id := range reachToolIDs {
		t.Run(id, func(t *testing.T) {
			call := reachCalls[id]
			e := newEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes[call.route] = okReply(answers[id])
			out, err := e.invoke(id, call.arguments)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if got[wantMember[id]] == nil || got["truncated"] != nil {
				t.Errorf("out = %s", out)
			}
			seen := e.fake.api()
			if len(seen) != 1 || seen[0].method != http.MethodPost || seen[0].contentType != "application/json" {
				t.Fatalf("requests = %+v", seen)
			}
			onAccount := id != reachPlansListLocationsID && id != reachPlansListProductsID
			if (seen[0].login == loginID) != onAccount || (got["customer_id"] == clientID) != onAccount {
				t.Errorf("login header = %q, customer_id = %v", seen[0].login, got["customer_id"])
			}
		})
	}
	t.Run("answers keep every documented member", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes[reachCalls[reachPlansGenerateForecastID].route] = okReply(answers[reachPlansGenerateForecastID])
		out, _ := e.invoke(reachPlansGenerateForecastID, reachForecastArgs)
		for _, member := range []string{"on_target_audience_metrics", "census_audience_size", "youtube_audience_size",
			"reach_forecasts", "cost_micros", "on_target_reach", "total_impressions", "planned_product_reach_forecasts",
			"planned_product_forecast", "conversions"} {
			if !strings.Contains(out, `"`+member+`"`) {
				t.Errorf("answer lacks %s: %s", member, out)
			}
		}
	})
}

func TestReachForecastMapsEveryArgumentIntoTheRequest(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+reachForecastPath(clientID)] = okReply(`{}`)
	arguments := `{"customer_id":"` + clientID + `","customer_reach_group":"Spring",` +
		`"campaign_duration":{"date_range":{"start_date":"2027-01-01","end_date":"2027-03-02"}},` +
		`"cookie_frequency_cap_setting":{"impressions":3,"time_unit":"WEEK"},"currency_code":"EUR",` +
		`"effective_frequency_limit":{"effective_frequency_breakdown_limit":4},` +
		`"forecast_metric_options":{"include_coview":true},` +
		`"planned_products":[{"plannable_product_code":"DEMAND_GEN","budget_micros":9007199254740991,"conversion_rate":0.02,` +
		`"advanced_product_targeting":{"surface_targeting_settings":{"surfaces":["GMAIL","SHORTS"]}}},` +
		`{"plannable_product_code":"TARGET_FREQUENCY","budget_micros":1000000,"advanced_product_targeting":` +
		`{"target_frequency_settings":{"target_frequency":2,"time_unit":"WEEKLY"}}}],` +
		`"reach_application_info":{"application_id":"app-1","application_instance":"PRODUCTION"},` +
		`"targeting":{"age_range":"AGE_RANGE_18_34","audience_targeting":{"user_interest":[{"user_interest_category":` +
		`"customers/` + clientID + `/userInterests/80"}],"user_lists":[{"user_list":` + reachLists + `}]},` +
		`"devices":[{"type":"MOBILE"}],"genders":[{"type":"FEMALE"}],"network":"YOUTUBE",` +
		`"parental_statuses":[{"type":"PARENT"}],"plannable_location_ids":["2276"]}}`
	if _, err := e.invoke(reachPlansGenerateForecastID, arguments); err != nil {
		t.Fatal(err)
	}
	want := `{"customerReachGroup":"Spring","campaignDuration":{"dateRange":{"startDate":"2027-01-01","endDate":"2027-03-02"}},` +
		`"cookieFrequencyCapSetting":{"impressions":3,"timeUnit":"WEEK"},"currencyCode":"EUR",` +
		`"effectiveFrequencyLimit":{"effectiveFrequencyBreakdownLimit":4},"forecastMetricOptions":{"includeCoview":true},` +
		`"plannedProducts":[{"plannableProductCode":"DEMAND_GEN","budgetMicros":9007199254740991,"conversionRate":0.02,` +
		`"advancedProductTargeting":{"surfaceTargetingSettings":{"surfaces":["GMAIL","SHORTS"]}}},` +
		`{"plannableProductCode":"TARGET_FREQUENCY","budgetMicros":1000000,"advancedProductTargeting":` +
		`{"targetFrequencySettings":{"targetFrequency":2,"timeUnit":"WEEKLY"}}}],` +
		`"reachApplicationInfo":{"applicationId":"app-1","applicationInstance":"PRODUCTION"},` +
		`"targeting":{"ageRange":"AGE_RANGE_18_34","audienceTargeting":{"userInterest":[{"userInterestCategory":` +
		`"customers/` + clientID + `/userInterests/80"}],"userLists":[{"userList":` + reachLists + `}]},` +
		`"devices":[{"type":"MOBILE"}],"genders":[{"type":"FEMALE"}],"network":"YOUTUBE",` +
		`"parentalStatuses":[{"type":"PARENT"}],"plannableLocationIds":["2276"]}}`
	var wantBody map[string]any
	_ = json.Unmarshal([]byte(want), &wantBody)
	seen := e.fake.api()
	if len(seen) != 1 || !reflect.DeepEqual(bodyOf(t, seen[0]), wantBody) {
		t.Fatalf("body = %s", seen[0].body)
	}
}

func TestReachCustomerToolsMapTheirArgumentsIntoTheRequest(t *testing.T) {
	cases := map[string]struct{ arguments, body string }{
		reachPlansListLocationsID: {`{"reach_application_info":{"application_instance":"DEVELOPMENT_AND_TESTING"}}`,
			`{"reachApplicationInfo":{"applicationInstance":"DEVELOPMENT_AND_TESTING"}}`},
		reachPlansListProductsID: {`{"plannable_location_id":"2276"}`, `{"plannableLocationId":"2276"}`},
		reachPlansGenerateConversionRatesID: {`{"customer_id":"` + clientID + `","customer_reach_group":"Spring"}`,
			`{"customerId":"` + clientID + `","customerReachGroup":"Spring"}`},
		reachPlansListUserListsID: {`{"customer_id":"` + clientID + `"}`, `{"customerId":"` + clientID + `"}`},
		reachPlansListUserInterestsID: {`{"customer_id":"` + clientID + `","name_query":"auto","path_query":"/Autos",` +
			`"user_interest_taxonomy_types":["AFFINITY","IN_MARKET"]}`,
			`{"customerId":"` + clientID + `","nameQuery":"auto","pathQuery":"/Autos",` +
				`"userInterestTaxonomyTypes":["AFFINITY","IN_MARKET"]}`},
	}
	for id, c := range cases {
		t.Run(id, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes[reachCalls[id].route] = okReply(`{}`)
			if _, err := e.invoke(id, c.arguments); err != nil {
				t.Fatal(err)
			}
			var want map[string]any
			_ = json.Unmarshal([]byte(c.body), &want)
			if seen := e.fake.api(); len(seen) != 1 || !reflect.DeepEqual(bodyOf(t, seen[0]), want) {
				t.Fatalf("requests = %+v", seen)
			}
		})
	}
}

func TestReachToolsRefuseBadArgumentsBeforeSecretAndIO(t *testing.T) {
	forecast := func(patch ...string) string {
		s := reachForecastArgs
		for i := 0; i+1 < len(patch); i += 2 {
			s = strings.Replace(s, patch[i], patch[i+1], 1)
		}
		return s
	}
	bad := map[string][]string{
		reachPlansListLocationsID: {`{"customer_id":"` + clientID + `"}`, `{"reach_application_info":{"application_id":"a b"}}`,
			`{"reach_application_info":{"application_instance":"STAGING"}}`},
		reachPlansListProductsID: {`{}`, `{"plannable_location_id":"germany"}`, `{"plannable_location_id":"2276","customer_id":"` + clientID + `"}`},
		reachPlansListUserListsID: {`{}`, `{"customer_id":"123"}`, `{"customer_id":"` + clientID + `","customer_reach_group":""}`,
			`{"customer_id":"` + clientID + `","customer_reach_group":"a\u0007b"}`},
		reachPlansGenerateConversionRatesID: {`{"customer_id":"` + clientID + `","customer_reach_group":"` + strings.Repeat("x", 201) + `"}`},
		reachPlansListUserInterestsID: {`{"customer_id":"` + clientID + `","user_interest_taxonomy_types":["AFFINITY","AFFINITY"]}`,
			`{"customer_id":"` + clientID + `","user_interest_taxonomy_types":["VERTICAL_GEO"]}`,
			`{"customer_id":"` + clientID + `","name_query":"` + strings.Repeat("x", 201) + `"}`,
			`{"customer_id":"` + clientID + `","name_query":"a\u0007b"}`},
		reachPlansGenerateForecastID: {
			forecast(`{"customer_id"`, `{"validate_only":true,"customer_id"`),
			forecast(`"campaign_duration":{"duration_in_days":28}`, `"campaign_duration":{}`),
			forecast(`{"duration_in_days":28}`, `{"duration_in_days":28,"date_range":{"start_date":"2027-01-01","end_date":"2027-01-02"}}`),
			forecast(`{"duration_in_days":28}`, `{"duration_in_days":0}`),
			forecast(`{"duration_in_days":28}`, `{"date_range":{"start_date":"2027-01-01","end_date":"2027-04-03"}}`),
			forecast(`{"duration_in_days":28}`, `{"date_range":{"start_date":"2027-01-05","end_date":"2027-01-01"}}`),
			forecast(`{"duration_in_days":28}`, `{"date_range":{"start_date":"2027-02-30","end_date":"2027-03-01"}}`),
			forecast(`"planned_products":[{"plannable_product_code":"VIDEO_VIEW_CAMPAIGN","budget_micros":5000000000}]`, `"planned_products":[]`),
			forecast(`"budget_micros":5000000000`, `"budget_micros":0`),
			forecast(`"budget_micros":5000000000`, `"budget_micros":1.5`),
			forecast(`"budget_micros":5000000000`, `"budget_micros":5000000000,"conversion_rate":1`),
			forecast(`"budget_micros":5000000000`, `"budget_micros":5000000000,"conversion_rate":0`),
			forecast(`"budget_micros":5000000000`, `"budget_micros":5000000000,"advanced_product_targeting":{}`),
			forecast(`"budget_micros":5000000000`, `"budget_micros":5000000000,"advanced_product_targeting":{"youtube_select_settings":{"lineup_id":1},"surface_targeting_settings":{"surfaces":["GMAIL"]}}`),
			forecast(`"budget_micros":5000000000`, `"budget_micros":5000000000,"advanced_product_targeting":{"surface_targeting_settings":{"surfaces":["TV"]}}`),
			forecast(`"budget_micros":5000000000`, `"budget_micros":5000000000,"advanced_product_targeting":{"target_frequency_settings":{"target_frequency":2,"time_unit":"DAILY"}}`),
			forecast(`"planned_products"`, `"currency_code":"eur","planned_products"`),
			forecast(`"planned_products"`, `"min_effective_frequency":11,"planned_products"`),
			forecast(`"planned_products"`, `"min_effective_frequency":2,"effective_frequency_limit":{"effective_frequency_breakdown_limit":3},"planned_products"`),
			forecast(`"planned_products"`, `"effective_frequency_limit":{"effective_frequency_breakdown_limit":11},"planned_products"`),
			forecast(`"planned_products"`, `"cookie_frequency_cap_setting":{"impressions":0,"time_unit":"DAY"},"planned_products"`),
			forecast(`"planned_products"`, `"cookie_frequency_cap_setting":{"impressions":1,"time_unit":"HOUR"},"planned_products"`),
			forecast(`}]}`, `}],"targeting":{"age_range":"AGE_RANGE_1_2"}}`),
			forecast(`}]}`, `}],"targeting":{"network":"TV"}}`),
			forecast(`}]}`, `}],"targeting":{"devices":[{"type":"WATCH"}]}}`),
			forecast(`}]}`, `}],"targeting":{"genders":[{"type":"MALE"},{"type":"FEMALE"},{"type":"UNDETERMINED"},{"type":"MALE"}]}}`),
			forecast(`}]}`, `}],"targeting":{"plannable_location_ids":["de"]}}`),
			forecast(`}]}`, `}],"targeting":{"audience_targeting":{"user_lists":[{"user_list":`+reachListsX+`}]}}}`),
			forecast(`}]}`, `}],"targeting":{"audience_targeting":{"user_lists":[{"user_list":"customers/`+clientID+`/campaigns/7"}]}}}`),
			forecast(`}]}`, `}],"targeting":{"audience_targeting":{"user_interest":[{"user_interest_category":"customers/`+otherID+`/userInterests/1"}]}}}`),
		},
	}
	for id, list := range bad {
		for i, arguments := range list {
			t.Run(id+"/"+string(rune('a'+i)), func(t *testing.T) {
				e := newEnvironment(t, nil, nil)
				if _, err := e.invoke(id, arguments); err == nil {
					t.Fatal("the call was accepted")
				}
				e.untouched(t)
			})
		}
	}
}

func TestReachTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range reachToolIDs {
		call := reachCalls[id]
		if id == reachPlansListLocationsID || id == reachPlansListProductsID {
			continue
		}
		t.Run(id, func(t *testing.T) {
			e := newEnvironment(t, []string{"customers/" + otherID}, nil)
			_, err := e.invoke(id, call.arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
	t.Run("an account below a manager target is proved with one read", func(t *testing.T) {
		e := newEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer_client.id FROM customer_client") {
				return &reply{status: 200, body: `{"results":[` + proofItem(clientID) + `]}`}
			}
			return nil
		}
		e.fake.routes[reachCalls[reachPlansListUserListsID].route] = okReply(`{}`)
		if _, err := e.invoke(reachPlansListUserListsID, reachCalls[reachPlansListUserListsID].arguments); err != nil {
			t.Fatalf("err = %v, requests = %+v", err, e.fake.api())
		}
		if len(e.fake.api()) != 2 {
			t.Errorf("requests = %d, want the proof and the call", len(e.fake.api()))
		}
	})
	t.Run("tools without an account touch no target and send no header", func(t *testing.T) {
		for _, id := range []string{reachPlansListLocationsID, reachPlansListProductsID} {
			e := newEnvironment(t, []string{"customers/" + otherID}, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes[reachCalls[id].route] = okReply(`{}`)
			if _, err := e.invoke(id, reachCalls[id].arguments); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if seen := e.fake.api(); len(seen) != 1 || seen[0].login != "" {
				t.Errorf("%s: requests = %+v", id, seen)
			}
		}
	})
}

func TestReachAnswersAreBoundedUntrustedData(t *testing.T) {
	t.Run("text and lists are cut", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		items := strings.TrimSuffix(strings.Repeat(`{"id":"1"},`, maxListItems+5), ",")
		e.fake.routes[reachCalls[reachPlansListLocationsID].route] = okReply(`{"plannableLocations":[` + items +
			`,{"name":"Bad\u0007 ` + strings.Repeat("x", 3000) + `"}]}`)
		out, err := e.invoke(reachPlansListLocationsID, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		if list, _ := got["plannable_locations"].([]any); len(list) != maxListItems || got["truncated"] != true {
			t.Errorf("list = %d, truncated = %v", len(list), got["truncated"])
		}
		e.fake.routes[reachCalls[reachPlansListLocationsID].route] = okReply(`{"plannableLocations":[{"name":"Bad\u0007 ` +
			strings.Repeat("x", 3000) + `"}]}`)
		out, _ = e.invoke(reachPlansListLocationsID, `{}`)
		name := decode(t, out)["plannable_locations"].([]any)[0].(map[string]any)["name"].(string)
		if len(name) > maxText || !strings.HasSuffix(name, "…") || strings.ContainsRune(name, 7) {
			t.Errorf("name is not bounded: %d bytes", len(name))
		}
	})
	t.Run("a name of another account is refused", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes[reachCalls[reachPlansListUserListsID].route] = okReply(`{"plannableUserLists":[{"userListInfo":{"userList":` +
			reachListsX + `}}]}`)
		out, err := e.invoke(reachPlansListUserListsID, reachCalls[reachPlansListUserListsID].arguments)
		if classOf(err) != provider.ClassInvalidResponse || strings.Contains(out, otherID) || strings.Contains(err.Error(), otherID) {
			t.Fatalf("out = %s, err = %v", out, err)
		}
	})
	t.Run("an answer that is not an object is refused", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes[reachCalls[reachPlansListLocationsID].route] = okReply(`[]`)
		if _, err := e.invoke(reachPlansListLocationsID, `{}`); classOf(err) != provider.ClassInvalidResponse {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestReachErrorsAreMappedAndNeverRepeated(t *testing.T) {
	for name, c := range map[string]struct {
		reply   reply
		class   provider.Class
		message string
	}{
		"permission": {reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, "may not access"},
		"input": {reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "reachPlanError", "INVALID_PRODUCT", "")},
			provider.ClassProviderError, "rejected the request as invalid input"},
		"quota": {reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes[reachCalls[reachPlansGenerateForecastID].route] = c.reply
			_, err := e.invoke(reachPlansGenerateForecastID, reachForecastArgs)
			if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) ||
				strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), "outcome of the change") {
				t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
			}
		})
	}
}
