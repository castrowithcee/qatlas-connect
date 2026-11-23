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
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

var keywordToolIDs = []string{keywordIdeasID, keywordMetricsID, keywordThemesID, keywordForecastID}

var keywordMethods = map[string]string{keywordIdeasID: "generateKeywordIdeas",
	keywordMetricsID: "generateKeywordHistoricalMetrics", keywordThemesID: "generateAdGroupThemes",
	keywordForecastID: "generateKeywordForecastMetrics"}

func keywordPath(customer, tool string) string {
	return "/v26/customers/" + customer + ":" + keywordMethods[tool]
}

func (e *environment) keywordPosts() []seen {
	var out []seen
	for _, s := range e.fake.api() {
		if s.method == http.MethodPost && strings.Contains(s.path, ":generate") {
			out = append(out, s)
		}
	}
	return out
}

const (
	ideasArgs    = `{"customer_id":"` + clientID + `","seed_keywords":["garden chairs"]}`
	metricsArgs  = `{"customer_id":"` + clientID + `","keywords":["garden chairs"]}`
	themesArgs   = `{"customer_id":"` + clientID + `","keywords":["garden chairs"],"ad_groups":["customers/` + clientID + `/adGroups/111"]}`
	forecastArgs = `{"customer_id":"` + clientID + `","campaign":{"bidding_strategy":{"manual_cpc_bidding_strategy":` +
		`{"max_cpc_bid_micros":1500000}},"ad_groups":[{"keywords":[{"text":"garden chairs","match_type":"PHRASE"}]}]}}`
)

var keywordArgs = map[string]string{keywordIdeasID: ideasArgs, keywordMetricsID: metricsArgs,
	keywordThemesID: themesArgs, keywordForecastID: forecastArgs}

const metricsFixture = `{"avgMonthlySearches":"1900","monthlySearchVolumes":[{"year":"2025","month":"JANUARY",` +
	`"monthlySearches":"1600"},{"year":"2025","month":"FEBRUARY"}],"competition":"MEDIUM","competitionIndex":"47",` +
	`"lowTopOfPageBidMicros":"300000","highTopOfPageBidMicros":"900000","averageCpcMicros":"500000"}`

// waitsOf installs a limiter for the customer account whose clock stands still and that records every wait
// instead of sleeping, so a test proves the spacing of the slots without waiting. The slots of a standing
// clock lie one spacing apart, so the waits grow by one spacing per request.
func waitsOf(t *testing.T, registry interface {
	Replace(string, *ratelimit.Limiter) func()
}, key string, interval time.Duration) *[]time.Duration {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	waits := &[]time.Duration{}
	t.Cleanup(registry.Replace(key, ratelimit.New(interval, func() time.Time { return now },
		func(_ context.Context, d time.Duration) error {
			*waits = append(*waits, d)
			return nil
		})))
	return waits
}

// newKeywordEnvironment is an environment whose accounts never wait for real.
func newKeywordEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	e := newEnvironment(t, targets, options)
	for _, id := range []string{clientID, otherID} {
		waitsOf(t, keywordLimiters, id, keywordSpacing)
	}
	return e
}

func TestKeywordToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range keywordToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != capability.EffectRead || !d.Risk.OpenWorld ||
			d.Risk.Confirmation != capability.ConfirmationNone || d.RequiresToolAllowList || len(d.OutputSchema) == 0 {
			t.Fatalf("%s = %+v", id, d)
		}
		if !slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("%s is not in the read profile", id)
		}
		if got := metadata.Token.Scopes[id]; !reflect.DeepEqual(got, []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, got)
		}
		for _, word := range []string{"Explorer", "Basic", "one request per second per customer ID", "untrusted",
			"permission error", "manager target"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "validate_only", "path", "url", "page_size_override"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
	}
	if keywordSpacing != time.Second {
		t.Errorf("keywordSpacing = %v, Google allows one request per second per customer ID", keywordSpacing)
	}
}

func TestKeywordIdeasMapsEveryArgumentAndTheAnswer(t *testing.T) {
	seeds := map[string]struct{ extra, key, want string }{
		"keywords":     {`,"seed_keywords":["garden chairs","patio"]`, "keywordSeed", `{"keywords":["garden chairs","patio"]}`},
		"url":          {`,"seed_url":"www.shop.example.com/chairs"`, "urlSeed", `{"url":"www.shop.example.com/chairs"}`},
		"keywords+url": {`,"seed_keywords":["chairs"],"seed_url":"https://shop.example.com/x"`, "keywordAndUrlSeed", `{"keywords":["chairs"],"url":"https://shop.example.com/x"}`},
		"site":         {`,"seed_site":"www.shop.example.com"`, "siteSeed", `{"site":"www.shop.example.com"}`},
	}
	for name, s := range seeds {
		t.Run(name, func(t *testing.T) {
			e := newKeywordEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+keywordPath(clientID, keywordIdeasID)] = okReply(`{}`)
			if _, err := e.invoke(keywordIdeasID, `{"customer_id":"`+clientID+`"`+s.extra+`}`); err != nil {
				t.Fatal(err)
			}
			posts := e.keywordPosts()
			if len(posts) != 1 || posts[0].login != loginID || posts[0].developerToken != "" {
				t.Fatalf("requests = %+v", e.fake.api())
			}
			var seed any
			_ = json.Unmarshal([]byte(s.want), &seed)
			want := map[string]any{"pageSize": float64(keywordPageDefault), s.key: seed}
			if !reflect.DeepEqual(bodyOf(t, posts[0]), want) {
				t.Errorf("body = %s", posts[0].body)
			}
		})
	}
	t.Run("all options and the answer", func(t *testing.T) {
		e := newKeywordEnvironment(t, nil, nil)
		e.fake.routes["POST "+keywordPath(clientID, keywordIdeasID)] = okReply(`{"results":[` +
			`{"text":"garden chairs","keywordIdeaMetrics":` + metricsFixture + `,"keywordAnnotations":{"concepts":[` +
			`{"name":"chairs","conceptGroup":{"name":"Furniture","type":"NON_BRAND"}}]},"closeVariants":["garden chair"]},` +
			`{"text":"line\u0000break ` + strings.Repeat("x", 2000) + `"}],` +
			`"aggregateMetricResults":{"deviceSearches":[{"device":"MOBILE","searchCount":"1200"}]},` +
			`"nextPageToken":"page-two","totalSize":"250"}`)
		out, err := e.invoke(keywordIdeasID, `{"customer_id":"`+clientID+`","seed_keywords":["garden chairs"],`+
			`"seed_url":"www.shop.example.com","language":"languageConstants/1001",`+
			`"geo_target_constants":["geoTargetConstants/2276","geoTargetConstants/2840"],"include_adult_keywords":true,`+
			`"keyword_plan_network":"GOOGLE_SEARCH","keyword_annotations":["KEYWORD_CONCEPT"],`+
			`"aggregate_metric_types":["DEVICE"],"historical_metrics_options":{"year_month_range":{"start":`+
			`{"year":2025,"month":"JANUARY"},"end":{"year":2025,"month":"JUNE"}},"include_average_cpc":true},`+
			`"page_token":"page-one","page_size":50}`)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"pageSize":50,"pageToken":"page-one","keywordAndUrlSeed":{"url":"www.shop.example.com","keywords":["garden chairs"]},` +
			`"language":"languageConstants/1001","geoTargetConstants":["geoTargetConstants/2276","geoTargetConstants/2840"],` +
			`"includeAdultKeywords":true,"keywordPlanNetwork":"GOOGLE_SEARCH","keywordAnnotation":["KEYWORD_CONCEPT"],` +
			`"aggregateMetrics":{"aggregateMetricTypes":["DEVICE"]},"historicalMetricsOptions":{"yearMonthRange":{` +
			`"start":{"year":2025,"month":"JANUARY"},"end":{"year":2025,"month":"JUNE"}},"includeAverageCpc":true}}`
		var wantBody map[string]any
		_ = json.Unmarshal([]byte(want), &wantBody)
		if got := bodyOf(t, e.keywordPosts()[0]); !reflect.DeepEqual(got, wantBody) {
			t.Errorf("body = %v", got)
		}
		got := decode(t, out)
		results := got["results"].([]any)
		first := results[0].(map[string]any)
		metrics := first["keyword_idea_metrics"].(map[string]any)
		volumes := metrics["monthly_search_volumes"].([]any)
		if got["customer_id"] != clientID || got["next_page_token"] != "page-two" || got["total_size"] != "250" ||
			first["text"] != "garden chairs" || metrics["avg_monthly_searches"] != "1900" || metrics["competition"] != "MEDIUM" ||
			metrics["competition_index"] != "47" || metrics["low_top_of_page_bid_micros"] != "300000" ||
			metrics["high_top_of_page_bid_micros"] != "900000" || metrics["average_cpc_micros"] != "500000" ||
			len(volumes) != 2 || volumes[0].(map[string]any)["monthly_searches"] != "1600" ||
			volumes[1].(map[string]any)["monthly_searches"] != nil {
			t.Errorf("out = %s", out)
		}
		concept := first["keyword_annotations"].([]any)[0].(map[string]any)
		if concept["name"] != "chairs" || concept["concept_group_name"] != "Furniture" || concept["concept_group_type"] != "NON_BRAND" ||
			first["close_variants"].([]any)[0] != "garden chair" {
			t.Errorf("annotations = %v", first)
		}
		device := got["aggregate_metric_results"].(map[string]any)["device_searches"].([]any)[0].(map[string]any)
		if device["device"] != "MOBILE" || device["search_count"] != "1200" {
			t.Errorf("aggregate = %v", got["aggregate_metric_results"])
		}
		second := results[1].(map[string]any)["text"].(string)
		if len(second) > maxText || strings.ContainsRune(second, 0) || !strings.HasSuffix(second, "…") {
			t.Errorf("text of an idea is not bounded: %d bytes", len(second))
		}
	})
	t.Run("rows beyond the bound are cut and said", func(t *testing.T) {
		e := newKeywordEnvironment(t, nil, nil)
		rows := strings.Repeat(`{"text":"k"},`, maxListItems) + `{"text":"k"}`
		e.fake.routes["POST "+keywordPath(clientID, keywordIdeasID)] = okReply(`{"results":[` + rows + `]}`)
		out, err := e.invoke(keywordIdeasID, ideasArgs)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		if len(got["results"].([]any)) != maxListItems || got["truncated"] != true {
			t.Errorf("results = %d, truncated = %v", len(got["results"].([]any)), got["truncated"])
		}
	})
}

func TestKeywordMetricsMapsTheRequestAndTheAnswer(t *testing.T) {
	e := newKeywordEnvironment(t, nil, nil)
	e.fake.routes["POST "+keywordPath(clientID, keywordMetricsID)] = okReply(`{"results":[{"text":"garden chairs",` +
		`"closeVariants":["garden chair"],"keywordMetrics":` + metricsFixture + `}],` +
		`"aggregateMetricResults":{"deviceSearches":[{"device":"DESKTOP","searchCount":"700"}]}}`)
	out, err := e.invoke(keywordMetricsID, `{"customer_id":"`+clientID+`","keywords":["garden chairs","garden chair"],`+
		`"language":"languageConstants/1001","geo_target_constants":["geoTargetConstants/2276"],"include_adult_keywords":true,`+
		`"keyword_plan_network":"GOOGLE_SEARCH_AND_PARTNERS","aggregate_metric_types":["DEVICE"],`+
		`"historical_metrics_options":{"year_month_range":{"start":{"year":2024,"month":"MARCH"},`+
		`"end":{"year":2025,"month":"MARCH"}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	var wantBody map[string]any
	_ = json.Unmarshal([]byte(`{"keywords":["garden chairs","garden chair"],"language":"languageConstants/1001",`+
		`"geoTargetConstants":["geoTargetConstants/2276"],"includeAdultKeywords":true,`+
		`"keywordPlanNetwork":"GOOGLE_SEARCH_AND_PARTNERS","aggregateMetrics":{"aggregateMetricTypes":["DEVICE"]},`+
		`"historicalMetricsOptions":{"yearMonthRange":{"start":{"year":2024,"month":"MARCH"},`+
		`"end":{"year":2025,"month":"MARCH"}}}}`), &wantBody)
	if got := bodyOf(t, e.keywordPosts()[0]); !reflect.DeepEqual(got, wantBody) {
		t.Errorf("body = %v", got)
	}
	got := decode(t, out)
	item := got["results"].([]any)[0].(map[string]any)
	metrics := item["keyword_metrics"].(map[string]any)
	if item["text"] != "garden chairs" || item["close_variants"].([]any)[0] != "garden chair" ||
		metrics["avg_monthly_searches"] != "1900" || metrics["average_cpc_micros"] != "500000" ||
		got["aggregate_metric_results"].(map[string]any)["device_searches"].([]any)[0].(map[string]any)["search_count"] != "700" {
		t.Errorf("out = %s", out)
	}
}

func TestKeywordThemesBindsAdGroupsToTheAccountAndRefusesForeignAnswers(t *testing.T) {
	good := `{"adGroupKeywordSuggestions":[{"keywordText":"garden chairs","suggestedKeywordText":"garden chairs",` +
		`"suggestedMatchType":"PHRASE","suggestedAdGroup":"customers/` + clientID + `/adGroups/111",` +
		`"suggestedCampaign":"customers/` + clientID + `/campaigns/222"}],` +
		`"unusableAdGroups":[{"adGroup":"customers/` + clientID + `/adGroups/333","campaign":"customers/` + clientID + `/campaigns/444"}]}`
	t.Run("the answer", func(t *testing.T) {
		e := newKeywordEnvironment(t, nil, nil)
		e.fake.routes["POST "+keywordPath(clientID, keywordThemesID)] = okReply(good)
		out, err := e.invoke(keywordThemesID, themesArgs)
		if err != nil {
			t.Fatal(err)
		}
		var wantBody map[string]any
		_ = json.Unmarshal([]byte(`{"keywords":["garden chairs"],"adGroups":["customers/`+clientID+`/adGroups/111"]}`), &wantBody)
		if got := bodyOf(t, e.keywordPosts()[0]); !reflect.DeepEqual(got, wantBody) {
			t.Errorf("body = %v", got)
		}
		got := decode(t, out)
		s := got["ad_group_keyword_suggestions"].([]any)[0].(map[string]any)
		u := got["unusable_ad_groups"].([]any)[0].(map[string]any)
		if s["suggested_match_type"] != "PHRASE" || s["suggested_campaign"] != "customers/"+clientID+"/campaigns/222" ||
			u["ad_group"] != "customers/"+clientID+"/adGroups/333" {
			t.Errorf("out = %s", out)
		}
	})
	for name, foreign := range map[string]string{
		"suggested ad group": strings.Replace(good, "customers/"+clientID+"/adGroups/111", "customers/"+otherID+"/adGroups/111", 1),
		"suggested campaign": strings.Replace(good, "customers/"+clientID+"/campaigns/222", "customers/"+otherID+"/campaigns/222", 1),
		"unusable ad group":  strings.Replace(good, "customers/"+clientID+"/adGroups/333", "customers/"+otherID+"/adGroups/333", 1),
		"wrong collection":   strings.Replace(good, "/campaigns/222", "/labels/222", 1),
	} {
		t.Run("foreign "+name, func(t *testing.T) {
			e := newKeywordEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPath(clientID, keywordThemesID)] = okReply(foreign)
			_, err := e.invoke(keywordThemesID, themesArgs)
			if classOf(err) != provider.ClassInvalidResponse || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestKeywordForecastMapsEveryStrategyAndTheAnswer(t *testing.T) {
	period := `"forecast_period":{"start_date":"2027-01-03","end_date":"2027-01-09"},"currency_code":"EUR",`
	campaign := func(strategy string) string {
		return `{"customer_id":"` + clientID + `",` + period + `"campaign":{"language_constants":["languageConstants/1001"],` +
			`"geo_target_constants":["geoTargetConstants/2276"],"bidding_strategy":` + strategy + `,` +
			`"ad_groups":[{"keywords":[{"text":"garden chairs","match_type":"EXACT"},{"text":"chairs","match_type":"BROAD"}]}]}}`
	}
	cases := map[string]struct{ strategy, wantStrategy, answer, wantOut string }{
		"manual cpc": {`{"manual_cpc_bidding_strategy":{"daily_budget_micros":20000000,"max_cpc_bid_micros":1500000}}`,
			`{"manualCpcBiddingStrategy":{"dailyBudgetMicros":20000000,"maxCpcBidMicros":1500000}}`,
			`{"campaignForecastMetrics":{"averageCpcMicros":"900000","clicks":12.5,"costMicros":"11250000"}}`,
			`{"average_cpc_micros":"900000","clicks":12.5,"cost_micros":"11250000"}`},
		"maximize clicks": {`{"maximize_clicks_bidding_strategy":{"daily_target_spend_micros":5000000,"max_cpc_bid_ceiling_micros":2000000}}`,
			`{"maximizeClicksBiddingStrategy":{"dailyTargetSpendMicros":5000000,"maxCpcBidCeilingMicros":2000000}}`,
			`{"campaignForecastMetrics":{"clicks":3,"costMicros":"5000000"}}`, `{"clicks":3,"cost_micros":"5000000"}`},
		"maximize conversions": {`{"maximize_conversions_bidding_strategy":{"daily_target_spend_micros":5000000}}`,
			`{"maximizeConversionsBiddingStrategy":{"dailyTargetSpendMicros":5000000}}`,
			`{"campaignForecastMetrics":{"conversions":0.5,"costMicros":"5000000","averageCpaMicros":"10000000"}}`,
			`{"conversions":0.5,"cost_micros":"5000000","average_cpa_micros":"10000000"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newKeywordEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPath(clientID, keywordForecastID)] = okReply(c.answer)
			out, err := e.invoke(keywordForecastID, campaign(c.strategy))
			if err != nil {
				t.Fatal(err)
			}
			var wantStrategy any
			_ = json.Unmarshal([]byte(c.wantStrategy), &wantStrategy)
			want := map[string]any{"currencyCode": "EUR", "forecastPeriod": map[string]any{"startDate": "2027-01-03", "endDate": "2027-01-09"},
				"campaign": map[string]any{"languageConstants": []any{"languageConstants/1001"},
					"geoTargetConstants": []any{"geoTargetConstants/2276"}, "biddingStrategy": wantStrategy,
					"adGroups": []any{map[string]any{"keywords": []any{map[string]any{"text": "garden chairs", "matchType": "EXACT"},
						map[string]any{"text": "chairs", "matchType": "BROAD"}}}}}}
			if got := bodyOf(t, e.keywordPosts()[0]); !reflect.DeepEqual(got, want) {
				t.Errorf("body = %s", e.keywordPosts()[0].body)
			}
			var wantOut, gotOut any
			_ = json.Unmarshal([]byte(c.wantOut), &wantOut)
			_ = json.Unmarshal([]byte(out), &gotOut)
			if !reflect.DeepEqual(gotOut.(map[string]any)["campaign_forecast_metrics"], wantOut) {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestKeywordToolsRefuseBadArgumentsBeforeSecretAndIO(t *testing.T) {
	const at = "@"
	ideas := func(extra string) string { return `{"customer_id":"` + clientID + `"` + extra + `}` }
	keywords := func(n int) string {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf(`"word %d"`, i)
		}
		return strings.Join(items, ",")
	}
	forecast := func(campaign string) string { return `{"customer_id":"` + clientID + `","campaign":` + campaign + `}` }
	manual := `{"manual_cpc_bidding_strategy":{"max_cpc_bid_micros":1}}`
	group := `"ad_groups":[{"keywords":[{"text":"chairs","match_type":"EXACT"}]}]`
	bad := map[string]map[string]string{
		keywordIdeasID: {
			"no seed":            ideas(``),
			"site with keywords": ideas(`,"seed_site":"www.example.com","seed_keywords":["a"]`),
			"site with url":      ideas(`,"seed_site":"www.example.com","seed_url":"www.example.com"`),
			"21 keywords":        ideas(`,"seed_keywords":[` + keywords(21) + `]`),
			"empty keywords":     ideas(`,"seed_keywords":[]`),
			"keyword words":      ideas(`,"seed_keywords":["a b c d e f g h i j k"]`),
			"keyword length":     ideas(`,"seed_keywords":["` + strings.Repeat("a", 81) + `"]`),
			"http url":           ideas(`,"seed_url":"http://shop.example.com/"`),
			"url user info":      ideas(`,"seed_url":"https://u:p` + at + `shop.example.com/"`),
			"bare user info":     ideas(`,"seed_url":"u:p` + at + `shop.example.com/"`),
			"url port":           ideas(`,"seed_url":"shop.example.com:8080/x"`),
			"site with path":     ideas(`,"seed_site":"www.example.com/cars"`),
			"site with scheme":   ideas(`,"seed_site":"https://www.example.com"`),
			"site with user":     ideas(`,"seed_site":"u` + at + `example.com"`),
			"11 locations": ideas(`,"seed_keywords":["a"],"geo_target_constants":[` + strings.Join(func() []string {
				out := make([]string, 11)
				for i := range out {
					out[i] = fmt.Sprintf(`"geoTargetConstants/%d"`, i+1)
				}
				return out
			}(), ",") + `]`),
			"location twice":   ideas(`,"seed_keywords":["a"],"geo_target_constants":["geoTargetConstants/1","geoTargetConstants/1"]`),
			"location form":    ideas(`,"seed_keywords":["a"],"geo_target_constants":["2276"]`),
			"language form":    ideas(`,"seed_keywords":["a"],"language":"en"`),
			"network":          ideas(`,"seed_keywords":["a"],"keyword_plan_network":"GOOGLE_DISPLAY"`),
			"aggregate":        ideas(`,"seed_keywords":["a"],"aggregate_metric_types":["CAMPAIGN"]`),
			"annotation":       ideas(`,"seed_keywords":["a"],"keyword_annotations":["OTHER"]`),
			"page size big":    ideas(`,"seed_keywords":["a"],"page_size":501`),
			"page size zero":   ideas(`,"seed_keywords":["a"],"page_size":0`),
			"token with space": ideas(`,"seed_keywords":["a"],"page_token":"a b"`),
			"range reversed": ideas(`,"seed_keywords":["a"],"historical_metrics_options":{"year_month_range":` +
				`{"start":{"year":2025,"month":"JUNE"},"end":{"year":2025,"month":"MAY"}}}`),
			"range month": ideas(`,"seed_keywords":["a"],"historical_metrics_options":{"year_month_range":` +
				`{"start":{"year":2025,"month":"JUNI"},"end":{"year":2025,"month":"MAY"}}}`),
			"partial failure": ideas(`,"seed_keywords":["a"],"partial_failure":true`),
			"login header":    ideas(`,"seed_keywords":["a"],"login_customer_id":"` + loginID + `"`),
			"customer form":   `{"customer_id":"123-456-7890","seed_keywords":["a"]}`,
		},
		keywordMetricsID: {
			"no keywords":    ideas(``),
			"501 keywords":   ideas(`,"keywords":[` + keywords(501) + `]`),
			"keyword words":  ideas(`,"keywords":["a b c d e f g h i j k"]`),
			"control char":   ideas(`,"keywords":["a\u0007b"]`),
			"location form":  ideas(`,"keywords":["a"],"geo_target_constants":["x"]`),
			"unknown member": ideas(`,"keywords":["a"],"seed_url":"www.example.com"`),
		},
		keywordThemesID: {
			"no keywords":      ideas(`,"ad_groups":["customers/` + clientID + `/adGroups/1"]`),
			"no ad groups":     ideas(`,"keywords":["a"]`),
			"foreign ad group": ideas(`,"keywords":["a"],"ad_groups":["customers/` + otherID + `/adGroups/1"]`),
			"wrong collection": ideas(`,"keywords":["a"],"ad_groups":["customers/` + clientID + `/campaigns/1"]`),
			"ad group twice":   ideas(`,"keywords":["a"],"ad_groups":["customers/` + clientID + `/adGroups/1","customers/` + clientID + `/adGroups/1"]`),
			"ad group suffix":  ideas(`,"keywords":["a"],"ad_groups":["customers/` + clientID + `/adGroups/1/x"]`),
			"ad group not ID":  ideas(`,"keywords":["a"],"ad_groups":["customers/` + clientID + `/adGroups/x"]`),
			"201 keywords":     ideas(`,"keywords":[` + keywords(201) + `],"ad_groups":["customers/` + clientID + `/adGroups/1"]`),
		},
		keywordForecastID: {
			"no campaign":            ideas(``),
			"no strategy":            forecast(`{` + group + `}`),
			"two strategies":         forecast(`{"bidding_strategy":{"manual_cpc_bidding_strategy":{"max_cpc_bid_micros":1},"maximize_conversions_bidding_strategy":{"daily_target_spend_micros":1}},` + group + `}`),
			"empty strategy":         forecast(`{"bidding_strategy":{},` + group + `}`),
			"zero micros":            forecast(`{"bidding_strategy":{"manual_cpc_bidding_strategy":{"max_cpc_bid_micros":0}},` + group + `}`),
			"missing required":       forecast(`{"bidding_strategy":{"maximize_clicks_bidding_strategy":{}},` + group + `}`),
			"group without keywords": forecast(`{"bidding_strategy":` + manual + `,"ad_groups":[{"keywords":[]}]}`),
			"match type":             forecast(`{"bidding_strategy":` + manual + `,"ad_groups":[{"keywords":[{"text":"a","match_type":"LOOSE"}]}]}`),
			"keyword words":          forecast(`{"bidding_strategy":` + manual + `,"ad_groups":[{"keywords":[{"text":"a b c d e f g h i j k","match_type":"EXACT"}]}]}`),
			"geo twice":              forecast(`{"bidding_strategy":` + manual + `,"geo_target_constants":["geoTargetConstants/1","geoTargetConstants/1"]}`),
			"language form":          forecast(`{"bidding_strategy":` + manual + `,"language_constants":["de"]}`),
			"campaign by name":       forecast(`{"bidding_strategy":` + manual + `,"campaign":"customers/` + clientID + `/campaigns/1"}`),
			"dates reversed": `{"customer_id":"` + clientID + `","forecast_period":{"start_date":"2027-02-01","end_date":"2027-01-01"},` +
				`"campaign":{"bidding_strategy":` + manual + `}}`,
			"date form": `{"customer_id":"` + clientID + `","forecast_period":{"start_date":"2027-1-1","end_date":"2027-01-09"},` +
				`"campaign":{"bidding_strategy":` + manual + `}}`,
			"impossible date": `{"customer_id":"` + clientID + `","forecast_period":{"start_date":"2027-02-30","end_date":"2027-03-09"},` +
				`"campaign":{"bidding_strategy":` + manual + `}}`,
			"currency": `{"customer_id":"` + clientID + `","currency_code":"eur","campaign":{"bidding_strategy":` + manual + `}}`,
		},
	}
	for id, cases := range bad {
		for name, arguments := range cases {
			t.Run(id+" "+name, func(t *testing.T) {
				e := newKeywordEnvironment(t, nil, nil)
				if _, err := e.invoke(id, arguments); err == nil {
					t.Fatal("the call was accepted")
				}
				e.untouched(t)
			})
		}
	}
	for _, id := range keywordToolIDs {
		t.Run(id+" the good call passes", func(t *testing.T) {
			e := newKeywordEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPath(clientID, id)] = okReply(`{}`)
			if _, err := e.invoke(id, keywordArgs[id]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKeywordTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range keywordToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(keywordArgs[id], clientID, otherID, 1)
			if id == keywordThemesID {
				arguments = strings.ReplaceAll(keywordArgs[id], clientID, otherID)
			}
			_, err := e.invoke(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want a refusal that does not name the account", err)
			}
			e.untouched(t)
		})
	}
	t.Run("an account below a manager target is proved first", func(t *testing.T) {
		e := newKeywordEnvironment(t, []string{"managers/" + managerID}, map[string]string{optionLoginCustomerID: managerID})
		e.fake.routes["POST "+searchPath(managerID)] = rows(proofItem(clientID))
		e.fake.routes["POST "+keywordPath(clientID, keywordMetricsID)] = okReply(`{}`)
		if _, err := e.invoke(keywordMetricsID, metricsArgs); err != nil {
			t.Fatal(err)
		}
		api := e.fake.api()
		if len(api) != 2 || !strings.Contains(api[0].path, "googleAds:search") || len(e.keywordPosts()) != 1 {
			t.Errorf("requests = %+v", api)
		}
	})
}

// Simulated limiters prove the spacing without waiting: the credential keeps no interval here, so every wait
// is the one of the customer account.
func TestKeywordRequestsAreSpacedPerCustomerIDAcrossTheToolsOfTheService(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	credentialWaits := waitsOf(t, limiters, credential, 0)
	accountWaits := map[string]*[]time.Duration{}
	for _, id := range []string{clientID, otherID} {
		accountWaits[id] = waitsOf(t, keywordLimiters, id, keywordSpacing)
		for _, tool := range keywordToolIDs {
			e.fake.routes["POST "+keywordPath(id, tool)] = okReply(`{}`)
		}
	}
	invoke := func(tool, customer string) {
		t.Helper()
		if _, err := e.invoke(tool, strings.ReplaceAll(keywordArgs[tool], clientID, customer)); err != nil {
			t.Fatal(err)
		}
	}
	invoke(keywordIdeasID, clientID)
	invoke(keywordIdeasID, otherID)
	if len(*accountWaits[clientID]) != 0 || len(*accountWaits[otherID]) != 0 {
		t.Fatalf("the first request of an account waited: %v %v", *accountWaits[clientID], *accountWaits[otherID])
	}
	invoke(keywordMetricsID, clientID)
	invoke(keywordThemesID, clientID)
	invoke(keywordForecastID, clientID)
	invoke(keywordForecastID, otherID)
	if want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}; !reflect.DeepEqual(*accountWaits[clientID], want) {
		t.Errorf("waits of %s = %v, want %v", clientID, *accountWaits[clientID], want)
	}
	if want := []time.Duration{time.Second}; !reflect.DeepEqual(*accountWaits[otherID], want) {
		t.Errorf("waits of %s = %v, want %v", otherID, *accountWaits[otherID], want)
	}
	if len(*credentialWaits) != 0 || len(e.keywordPosts()) != 6 {
		t.Errorf("credential waits = %v, posts = %d", *credentialWaits, len(e.keywordPosts()))
	}
}

func TestKeywordErrorsAreMappedNeverRepeatedAndHoldTheAccount(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"explorer access level", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"SERVICE_ACCESS_DENIED", "")}, provider.ClassPermission, "refused the request (authorizationError SERVICE_ACCESS_DENIED)"},
		{"access prohibited", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "quotaError",
			"ACCESS_PROHIBITED", "")}, provider.ClassPermission, "access to the resource is not permitted"},
		{"test project", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION", "")}, provider.ClassPermission, "Explorer, Basic or Standard"},
		{"one request per second", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_EXHAUSTED", `{"rateScope":"ACCOUNT","retryDelay":"3s"}`)}, provider.ClassRateLimited, "wait and try again"},
		{"rate", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"3s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"invalid input", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "keywordPlanIdeaError",
			"URL_CRAWL_ERROR", "")}, provider.ClassProviderError, "rejected the request as invalid input (keywordPlanIdeaError URL_CRAWL_ERROR)"},
		{"token refused", reply{status: 401, body: adsFailure(401, "UNAUTHENTICATED", "authenticationError",
			"OAUTH_TOKEN_EXPIRED", "")}, provider.ClassAuth, "access token"},
	}
	for _, id := range keywordToolIDs {
		for _, c := range cases {
			t.Run(id+" "+c.name, func(t *testing.T) {
				e := newKeywordEnvironment(t, nil, nil)
				e.fake.routes["POST "+keywordPath(clientID, id)] = c.reply
				_, err := e.invoke(id, keywordArgs[id])
				if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) {
					t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
				}
				for _, leaked := range []string{detailCanary, triggerCanary, "synthetic-access"} {
					if strings.Contains(err.Error(), leaked) {
						t.Errorf("error %q carries %q", err, leaked)
					}
				}
				// A refused token is renewed and the request is sent once more; nothing else is repeated.
				if want := map[bool]int{true: 2, false: 1}[c.class == provider.ClassAuth]; len(e.keywordPosts()) != want {
					t.Errorf("posts = %d, want %d", len(e.keywordPosts()), want)
				}
			})
		}
	}
	t.Run("a rate refusal holds the next request of the account", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		waitsOf(t, limiters, credential, 0)
		waits := waitsOf(t, keywordLimiters, clientID, keywordSpacing)
		e.fake.routes["POST "+keywordPath(clientID, keywordIdeasID)] = cases[3].reply
		_, _ = e.invoke(keywordIdeasID, ideasArgs)
		_, _ = e.invoke(keywordIdeasID, ideasArgs)
		if want := []time.Duration{3 * time.Second}; !reflect.DeepEqual(*waits, want) {
			t.Errorf("waits = %v, want %v", *waits, want)
		}
	})
	t.Run("a server error is no uncertain change", func(t *testing.T) {
		e := newKeywordEnvironment(t, nil, nil)
		e.fake.routes["POST "+keywordPath(clientID, keywordIdeasID)] = reply{status: 503, body: googleError(503, "UNAVAILABLE", "")}
		_, err := e.invoke(keywordIdeasID, ideasArgs)
		if err == nil || strings.Contains(err.Error(), "outcome of the change") || len(e.keywordPosts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.keywordPosts()))
		}
	})
}

func TestKeywordAnswersAreRefusedWhenTheyAreNotTheShapeAsked(t *testing.T) {
	for name, route := range map[string]reply{
		"not json":        {status: 200, body: "<html>", header: http.Header{"Content-Type": {"text/html"}}},
		"unreadable json": okReply(`{"results":`),
		"bad number":      okReply(`{"results":[{"text":"a","keywordIdeaMetrics":{"avgMonthlySearches":"12x"}}]}`),
		"long token":      okReply(`{"nextPageToken":"` + strings.Repeat("a", keywordPageToken+1) + `"}`),
	} {
		t.Run(name, func(t *testing.T) {
			e := newKeywordEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPath(clientID, keywordIdeasID)] = route
			_, err := e.invoke(keywordIdeasID, ideasArgs)
			if classOf(err) != provider.ClassInvalidResponse {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
