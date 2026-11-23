package googleads

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	sugGeoPath     = "/v26/geoTargetConstants:suggest"
	sugKeywordPath = "/v26/keywordThemeConstants:suggest"
)

func sugCustomerPath(id, action string) string { return "/v26/customers/" + id + action }

// sugCases are the tools of this group with a valid call and the route of its request. Account tools carry
// %s for the customer ID.
type sugCase struct {
	id, args, route string
	account         bool
}

func sugCases() []sugCase {
	return []sugCase{
		{contentCreatorInsightsGenerateCreatorID, `{"customer_id":"%s","customer_insights_group":"Spring",` +
			`"country_locations":["geoTargetConstants/2276"],"search_topics":{"entities":["/m/09l6s"]}}`,
			"POST " + sugCustomerPath(clientID, ":generateCreatorInsights"), true},
		{contentCreatorInsightsGenerateTrendingID, `{"customer_id":"%s","customer_insights_group":"Spring",` +
			`"country_location":"geoTargetConstants/2276","search_topics":{"entities":["/m/09l6s"]}}`,
			"POST " + sugCustomerPath(clientID, ":generateTrendingInsights"), true},
		{brandSuggestionsSuggestID, `{"customer_id":"%s","brand_prefix":"Qa"}`,
			"POST " + sugCustomerPath(clientID, ":suggestBrands"), true},
		{geoTargetConstantsSuggestID, `{"location_names":["Berlin"]}`, "POST " + sugGeoPath, false},
		{keywordThemeConstantsSuggestID, `{"query_text":"plumber"}`, "POST " + sugKeywordPath, false},
	}
}

func (c sugCase) arguments(customer string) string {
	if c.account {
		return fmt.Sprintf(c.args, customer)
	}
	return c.args
}

func TestSuggestionToolsAreReadOnlyAndTakeNoApplicationInfo(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, c := range sugCases() {
		d, _, ok := reg.Lookup(c.id)
		if !ok || d.Version != 1 || d.Risk.Effect != capability.EffectRead || !d.Risk.OpenWorld || d.RequiresToolAllowList ||
			d.Risk.Confirmation != capability.ConfirmationNone || len(d.OutputSchema) == 0 {
			t.Fatalf("%s = %+v", c.id, d)
		}
		if !strings.Contains(strings.Join(metadata.Profiles[0].Tools, " "), c.id) {
			t.Errorf("%s is not in the read profile", c.id)
		}
		var input struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(d.InputSchema, &input); err != nil || len(input.Properties) != len(d.Arguments) {
			t.Errorf("%s: %d schema properties but %d documented arguments", c.id, len(input.Properties), len(d.Arguments))
		}
		_, hasCustomer := input.Properties["customer_id"]
		if hasCustomer != c.account {
			t.Errorf("%s: customer_id presence = %v, want %v", c.id, hasCustomer, c.account)
		}
		for _, forbidden := range []string{"application", "login_customer_id", "validate_only", "path"} {
			if strings.Contains(string(d.InputSchema), forbidden) {
				t.Errorf("%s: input names %q", c.id, forbidden)
			}
		}
	}
	d, _, _ := reg.Lookup(geoTargetConstantsSuggestID)
	if !strings.Contains(d.Description, "acts on no account") {
		t.Error("geo description does not say that it needs no account")
	}
}

func TestSuggestionToolsCheckTargetsAndArgumentsBeforeSecretAndIO(t *testing.T) {
	for _, c := range sugCases() {
		if !c.account {
			continue
		}
		t.Run(c.id+" outside the targets", func(t *testing.T) {
			e := newEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invoke(c.id, c.arguments(otherID))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
		t.Run(c.id+" malformed customer_id", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			for _, bad := range []string{"123-456-7890", "123456789", "", "1234567890 OR 1=1"} {
				if _, err := e.invoke(c.id, c.arguments(bad)); err == nil {
					t.Errorf("customer_id %q accepted", bad)
				}
			}
			e.untouched(t)
		})
	}
	bad := map[string][]string{
		contentCreatorInsightsGenerateCreatorID: {
			`"country_locations":["geoTargetConstants/2276"]`,
			`"country_locations":["geoTargetConstants/2276"],"search_topics":{"entities":["/m/09l6s"]},"search_channels":{"youtube_channel_ids":["UC1"]}`,
			`"country_locations":["geoTargetConstants/1","geoTargetConstants/2"],"search_topics":{"entities":["/m/09l6s"]}`,
			`"country_locations":["2276"],"search_topics":{"entities":["/m/09l6s"]}`,
			`"country_locations":["geoTargetConstants/1"],"search_topics":{"entities":["/m/09l6s"]},"sub_country_locations":["geoTargetConstants/5"]`,
			`"country_locations":["geoTargetConstants/1"],"search_attributes":{"creator_entities":["/m/1"]},"sub_country_locations":["geoTargetConstants/5"],"supplemental_data":["LOCAL_CREATOR_DATA"]`,
			`"country_locations":["geoTargetConstants/1"],"search_attributes":{}`,
			`"country_locations":["geoTargetConstants/1"],"search_attributes":{"audience_attributes":[{"age_range":"AGE_RANGE_18_24","gender":"MALE"}]}`,
			`"country_locations":["geoTargetConstants/1"],"search_attributes":{"audience_attributes":[{"location":"geoTargetConstants/1"}]}`,
			`"country_locations":["geoTargetConstants/1"],"search_attributes":{"audience_attributes":[{"user_interest":"customers/` + otherID + `/userInterests/5"}]}`,
			`"country_locations":["geoTargetConstants/1"],"search_attributes":{"audience_attributes":[{"entity":"/m/1"}],"audience_combinations":[{"attributes":[{"entity":"/m/2"}]}]}`,
			`"country_locations":["geoTargetConstants/1"],"search_attributes":{"audience_combinations":[{"attributes":[{"gender":"MALE"}]}]}`,
			`"country_locations":["geoTargetConstants/1"],"search_channels":{"youtube_channel_handles":["nohandle"]}`,
			`"country_locations":["geoTargetConstants/1"],"search_channels":{}`,
			`"country_locations":["geoTargetConstants/1"],"search_topics":{"entities":["/m/09l6s"]},"insights_application_info":{"application_id":"x"}`,
			`"country_locations":["geoTargetConstants/1"],"search_topics":{"entities":["/m/09l6s"]},"supplemental_data":["BRAND_SENTIMENT_DATA"]`,
		},
		contentCreatorInsightsGenerateTrendingID: {
			`"country_location":"geoTargetConstants/2276"`,
			`"country_location":"geoTargetConstants/2276","search_audience":{"audience_attributes":[{"device":"MOBILE"}]}`,
			`"country_location":"geoTargetConstants/2276","search_audience":{}`,
			`"country_location":"geoTargetConstants/2276","search_topics":{"entities":["/m/09l6s"]},"sub_country_locations":["geoTargetConstants/5"]`,
			`"country_location":"geoTargetConstants/2276","search_audience":{"audience_attributes":[{"gender":"MALE"}]},"sub_country_locations":["geoTargetConstants/5"],"supplemental_data":["LOCAL_CREATOR_DATA"]`,
			`"country_location":["geoTargetConstants/1"],"search_topics":{"entities":["/m/09l6s"]}`,
			`"country_location":"geoTargetConstants/2276","search_topics":{"entities":[]}`,
		},
		brandSuggestionsSuggestID: {
			`"brand_prefix":""`, `"brand_prefix":"a\u0007b"`, `"brand_prefix":"Qa","selected_brands":["Brand Name"]`,
			`"brand_prefix":"Qa","selected_brands":[]`, `"brand_prefix":"Qa","login_customer_id":"` + loginID + `"`,
		},
	}
	for id, list := range bad {
		for _, extra := range list {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`","customer_insights_group":"g",`+extra+`}`); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
	for id, list := range map[string][]string{
		geoTargetConstantsSuggestID: {`{}`, `{"location_names":["A"],"geo_target_constants":["geoTargetConstants/1"]}`,
			`{"location_names":[]}`, `{"location_names":["A"],"country_code":"de"}`, `{"location_names":["A"],"locale":"d e"}`,
			`{"geo_target_constants":["Berlin"]}`, `{"location_names":["A"],"customer_id":"` + clientID + `"}`,
			`{"location_names":["` + strings.Repeat("a", 256) + `"]}`},
		keywordThemeConstantsSuggestID: {`{}`, `{"query_text":""}`, `{"query_text":"x","country_code":"DEU"}`,
			`{"query_text":"x","language_code":"eng"}`, `{"query_text":"x","customer_id":"` + clientID + `"}`},
	} {
		e := newEnvironment(t, nil, nil)
		for _, args := range list {
			if _, err := e.invoke(id, args); err == nil {
				t.Errorf("%s accepted %s", id, args)
			}
		}
		e.untouched(t)
	}
	tooMany := `["` + strings.Repeat(`a","`, 25) + `a"]`
	e := newEnvironment(t, nil, nil)
	if _, err := e.invoke(geoTargetConstantsSuggestID, `{"location_names":`+tooMany+`}`); err == nil {
		t.Error("26 location names accepted")
	}
	e.untouched(t)
}

func TestSuggestionToolsSendTheDocumentedRequests(t *testing.T) {
	e := newEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	for _, route := range []string{
		"POST " + sugCustomerPath(clientID, ":generateCreatorInsights"),
		"POST " + sugCustomerPath(clientID, ":generateTrendingInsights"),
		"POST " + sugCustomerPath(clientID, ":suggestBrands"), "POST " + sugGeoPath, "POST " + sugKeywordPath} {
		e.fake.routes[route] = okReply(`{}`)
	}
	call := func(id, args string) seen {
		t.Helper()
		before := len(e.fake.api())
		if _, err := e.invoke(id, args); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		api := e.fake.api()
		if len(api) != before+1 {
			t.Fatalf("%s sent %d requests", id, len(api)-before)
		}
		s := api[before]
		if s.method != "POST" || s.contentType != "application/json" || s.developerToken != "" || !strings.HasPrefix(s.auth, "Bearer ") {
			t.Errorf("%s: request = %+v", id, s)
		}
		return s
	}
	body := func(s seen) map[string]any {
		var out map[string]any
		if err := json.Unmarshal([]byte(s.body), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	s := call(contentCreatorInsightsGenerateCreatorID, fmt.Sprintf(`{"customer_id":"%s","customer_insights_group":"Spring",`+
		`"country_locations":["geoTargetConstants/2276"],"sub_country_locations":["geoTargetConstants/9"],`+
		`"search_attributes":{"audience_attributes":[{"age_range":"AGE_RANGE_25_34"},{"device":"MOBILE"},`+
		`{"parental_status":"PARENT"},{"income_range":"INCOME_RANGE_90_UP"},{"gender":"FEMALE"}],"creator_entities":["/m/0abc"]}}`,
		clientID))
	b := body(s)
	attrs := b["searchAttributes"].(map[string]any)
	if s.path != sugCustomerPath(clientID, ":generateCreatorInsights") || s.login != loginID ||
		b["customerInsightsGroup"] != "Spring" || len(b) != 4 || b["customerId"] != nil ||
		fmt.Sprint(b["countryLocations"]) != "[map[geoTargetConstant:geoTargetConstants/2276]]" ||
		fmt.Sprint(b["subCountryLocations"]) != "[map[geoTargetConstant:geoTargetConstants/9]]" ||
		fmt.Sprint(attrs["audienceAttributes"]) != "[map[ageRange:map[type:AGE_RANGE_25_34]] map[device:map[type:MOBILE]] "+
			"map[parentalStatus:map[type:PARENT]] map[incomeRange:map[type:INCOME_RANGE_90_UP]] map[gender:map[type:FEMALE]]]" ||
		fmt.Sprint(attrs["creatorAttributes"]) != "[map[entity:map[knowledgeGraphMachineId:/m/0abc]]]" {
		t.Errorf("creator request = %s", s.body)
	}

	s = call(contentCreatorInsightsGenerateCreatorID, fmt.Sprintf(`{"customer_id":"%s","customer_insights_group":"g",`+
		`"country_locations":["geoTargetConstants/1"],"supplemental_data":["LOCAL_CREATOR_DATA"],"search_channels":`+
		`{"youtube_channel_ids":["UC_x-1"],"youtube_channel_handles":["@creators"]}}`, clientID))
	if got := body(s); fmt.Sprint(got["searchChannels"]) != "map[youtubeChannelHandles:[@creators] youtubeChannels:[map[channelId:UC_x-1]]]" ||
		fmt.Sprint(got["supplementalData"]) != "[LOCAL_CREATOR_DATA]" {
		t.Errorf("channels request = %s", s.body)
	}

	s = call(contentCreatorInsightsGenerateTrendingID, fmt.Sprintf(`{"customer_id":"%s","customer_insights_group":"g",`+
		`"country_location":"geoTargetConstants/2276","supplemental_data":["BRAND_SENTIMENT_DATA"],"search_audience":`+
		`{"audience_attributes":[{"gender":"MALE"}],"audience_combinations":[{"attributes":[{"entity":"/m/1"},`+
		`{"category":"88"}]},{"attributes":[{"user_interest":"customers/%s/userInterests/7"}]}]}}`, clientID, clientID))
	b = body(s)
	if s.path != sugCustomerPath(clientID, ":generateTrendingInsights") || s.login != loginID ||
		fmt.Sprint(b["countryLocation"]) != "map[geoTargetConstant:geoTargetConstants/2276]" ||
		fmt.Sprint(b["searchAudience"]) != "map[audienceAttributes:[map[gender:map[type:MALE]]] audienceCombinations:"+
			"[map[attributes:[map[entity:map[knowledgeGraphMachineId:/m/1]] map[category:map[categoryId:88]]]] "+
			"map[attributes:[map[userInterest:map[userInterestCategory:customers/"+clientID+"/userInterests/7]]]]]]" {
		t.Errorf("trending request = %s", s.body)
	}

	s = call(brandSuggestionsSuggestID, fmt.Sprintf(`{"customer_id":"%s","brand_prefix":"Qa","selected_brands":["/g/11abc"]}`, clientID))
	if got := body(s); s.path != sugCustomerPath(clientID, ":suggestBrands") || s.login != loginID ||
		fmt.Sprint(got) != "map[brandPrefix:Qa selectedBrands:[/g/11abc]]" {
		t.Errorf("brand request = %s", s.body)
	}

	// The two tools without an account send neither the account nor the manager header.
	s = call(geoTargetConstantsSuggestID, `{"geo_target_constants":["geoTargetConstants/1003854"],"country_code":"DE","locale":"de-DE"}`)
	if got := body(s); s.path != sugGeoPath || s.login != "" ||
		fmt.Sprint(got) != "map[countryCode:DE geoTargets:map[geoTargetConstants:[geoTargetConstants/1003854]] locale:de-DE]" {
		t.Errorf("geo request = %s login=%q", s.body, s.login)
	}
	s = call(geoTargetConstantsSuggestID, `{"location_names":["Berlin","Hamburg"]}`)
	if got := body(s); fmt.Sprint(got) != "map[locationNames:map[names:[Berlin Hamburg]]]" {
		t.Errorf("geo names request = %s", s.body)
	}
	s = call(keywordThemeConstantsSuggestID, `{"query_text":"plumber","country_code":"DE","language_code":"de"}`)
	if got := body(s); s.path != sugKeywordPath || s.login != "" ||
		fmt.Sprint(got) != "map[countryCode:DE languageCode:de queryText:plumber]" {
		t.Errorf("keyword request = %s login=%q", s.body, s.login)
	}
}

func TestSuggestionToolsWithoutAnAccountIgnoreTheTargets(t *testing.T) {
	e := newEnvironment(t, []string{"customers/" + clientID}, nil)
	e.fake.routes["POST "+sugGeoPath] = okReply(`{"geoTargetConstantSuggestions":[]}`)
	e.fake.routes["POST "+sugKeywordPath] = okReply(`{}`)
	if _, err := e.invoke(geoTargetConstantsSuggestID, `{"location_names":["Berlin"]}`); err != nil {
		t.Fatal(err)
	}
	out, err := e.invoke(keywordThemeConstantsSuggestID, `{"query_text":"plumber"}`)
	if err != nil || len(e.fake.api()) != 2 {
		t.Fatalf("err = %v, requests = %+v", err, e.fake.api())
	}
	if got := decode(t, out); got["customer_id"] != nil || got["keyword_theme_constants"] != nil {
		t.Errorf("out = %s", out)
	}
}

func TestGeoSuggestionsAreMappedWithEveryDocumentedField(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+sugGeoPath] = okReply(`{"geoTargetConstantSuggestions":[{"locale":"de","reach":"3600000",` +
		`"searchTerm":"Ber\u0007lin","geoTargetConstant":{"resourceName":"geoTargetConstants/1003854","id":"1003854",` +
		`"name":"Berlin","countryCode":"DE","targetType":"City","status":"ENABLED","canonicalName":"Berlin,Berlin,Germany",` +
		`"parentGeoTarget":"geoTargetConstants/20228","requireEuPoliticalAdvertisingDeclaration":true},` +
		`"geoTargetConstantParents":[{"resourceName":"geoTargetConstants/20228","name":"Berlin","targetType":"State"}]}]}`)
	out, err := e.invoke(geoTargetConstantsSuggestID, `{"location_names":["Berlin"],"country_code":"DE"}`)
	if err != nil {
		t.Fatal(err)
	}
	list := decode(t, out)["geo_target_constant_suggestions"].([]any)
	item := list[0].(map[string]any)
	constant := item["geo_target_constant"].(map[string]any)
	if len(list) != 1 || item["search_term"] != "Ber lin" || item["reach"] != "3600000" || item["locale"] != "de" ||
		constant["canonical_name"] != "Berlin,Berlin,Germany" || constant["parent_geo_target"] != "geoTargetConstants/20228" ||
		constant["require_eu_political_advertising_declaration"] != true || constant["target_type"] != "City" ||
		len(item["geo_target_constant_parents"].([]any)) != 1 {
		t.Errorf("out = %s", out)
	}
	// A list over the bound is cut and says so; an answer that names an account object is refused.
	var many []string
	for i := 0; i < maxListItems+1; i++ {
		many = append(many, `{"searchTerm":"x"}`)
	}
	e.fake.routes["POST "+sugGeoPath] = okReply(`{"geoTargetConstantSuggestions":[` + strings.Join(many, ",") + `]}`)
	out, err = e.invoke(geoTargetConstantsSuggestID, `{"location_names":["x"]}`)
	got := decode(t, out)
	if err != nil || len(got["geo_target_constant_suggestions"].([]any)) != maxListItems || got["truncated"] != true {
		t.Errorf("err = %v, truncated = %v", err, got["truncated"])
	}
	e.fake.routes["POST "+sugGeoPath] = okReply(`{"geoTargetConstantSuggestions":[{"searchTerm":"customers/` + otherID + `/x"}]}`)
	if _, err := e.invoke(geoTargetConstantsSuggestID, `{"location_names":["x"]}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid response", err)
	}
}

func TestKeywordThemeAndBrandAnswersAreMapped(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+sugKeywordPath] = okReply(`{"keywordThemeConstants":[{"resourceName":"keywordThemeConstants/12~34",` +
		`"countryCode":"DE","languageCode":"de","displayName":"Klempner"}]}`)
	out, err := e.invoke(keywordThemeConstantsSuggestID, `{"query_text":"plumber"}`)
	theme := decode(t, out)["keyword_theme_constants"].([]any)[0].(map[string]any)
	if err != nil || theme["resource_name"] != "keywordThemeConstants/12~34" || theme["display_name"] != "Klempner" ||
		theme["country_code"] != "DE" || theme["language_code"] != "de" {
		t.Errorf("err = %v, out = %s", err, out)
	}
	e.fake.routes["POST "+sugCustomerPath(clientID, ":suggestBrands")] = okReply(`{"brands":[{"id":"/g/11abc","name":"Qatl\u0000as",` +
		`"urls":["qatlas.example"],"state":"ENABLED"}]}`)
	out, err = e.invoke(brandSuggestionsSuggestID, fmt.Sprintf(`{"customer_id":"%s","brand_prefix":"Qa"}`, clientID))
	got := decode(t, out)
	brand := got["brands"].([]any)[0].(map[string]any)
	if err != nil || got["customer_id"] != clientID || brand["id"] != "/g/11abc" || brand["name"] != "Qatl as" ||
		fmt.Sprint(brand["urls"]) != "[qatlas.example]" || brand["state"] != "ENABLED" {
		t.Errorf("err = %v, out = %s", err, out)
	}
	e.fake.routes["POST "+sugCustomerPath(clientID, ":suggestBrands")] = okReply(`{}`)
	out, _ = e.invoke(brandSuggestionsSuggestID, fmt.Sprintf(`{"customer_id":"%s","brand_prefix":"Qa"}`, clientID))
	if decode(t, out)["brands"] != nil {
		t.Errorf("an empty answer must give no entries: %s", out)
	}
}

func TestInsightAnswersAreMappedAndBoundToTheAccount(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	creator := `{"creatorName":"Creator\u0007 One","creatorChannels":[{"displayName":"Chan","youtubeChannel":{"channelId":"UC1"},` +
		`"channelUrl":"https://www.youtube.com/channel/UC1","handle":"@chan","channelMetrics":{"subscriberCount":"1200",` +
		`"engagementRate":0.12,"isActiveShortsCreator":true,"partnershipOpportunities":["BRAND_CONNECT"]},` +
		`"dataSharingConsentGiven":true,"channelAudienceAttributes":[{"dimension":"AGE_RANGE","attribute":{"ageRange":` +
		`{"type":"AGE_RANGE_25_34"}},"displayName":"25-34","subscriberShare":0.4}],"topVideos":[{"dimension":"YOUTUBE_VIDEO",` +
		`"youtubeVideoMetadata":{"videoUrl":"https://www.youtube.com/watch?v=x","viewsCount":"5"}}],` +
		`"languageDistributions":[{"languageCode":"de","proportion":0.9}],"relevanceScore":0.5}]}`
	e.fake.routes["POST "+sugCustomerPath(clientID, ":generateCreatorInsights")] = okReply(`{"creatorInsights":[` + creator + `]}`)
	out, err := e.invoke(contentCreatorInsightsGenerateCreatorID, fmt.Sprintf(sugCases()[0].args, clientID))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	first := got["creator_insights"].([]any)[0].(map[string]any)
	channel := first["creator_channels"].([]any)[0].(map[string]any)
	metrics := channel["channel_metrics"].(map[string]any)
	if got["customer_id"] != clientID || first["creator_name"] != "Creator  One" ||
		got["local_creator_insights"] != nil || channel["handle"] != "@chan" ||
		metrics["subscriber_count"] != "1200" || metrics["engagement_rate"] != 0.12 ||
		channel["data_sharing_consent_given"] != true || channel["relevance_score"] != 0.5 ||
		fmt.Sprint(channel["language_distributions"]) != "[map[language_code:de proportion:0.9]]" ||
		len(channel["channel_audience_attributes"].([]any)) != 1 || len(channel["top_videos"].([]any)) != 1 {
		t.Errorf("out = %s", out)
	}

	e.fake.routes["POST "+sugCustomerPath(clientID, ":generateTrendingInsights")] = okReply(`{"trendInsights":[{"trendAttribute":` +
		`{"dimension":"KNOWLEDGE_GRAPH","displayName":"Cats","knowledgeGraphAttributeMetadata":{"entityCapabilities":["BRAND"],` +
		`"relatedCategories":[{"displayName":"Pets","attribute":{"category":{"categoryId":"88"}}}]}},` +
		`"trendMetrics":{"viewsCount":"90","trendChangePercent":0.3},"trend":"EMERGING","trendDataPoints":[{"month":"2026-10",` +
		`"trendMetrics":{"viewsIndexedValue":"50"}}],"brandSentimentInsights":[{"month":"2026-10","sentimentDistributions":` +
		`[{"sentiment":"POSITIVE","sentimentShare":0.5}],"sentimentSummaries":[{"sentiment":"POSITIVE","summary":"Good"}]}],` +
		`"relatedCreators":[` + creator + `]}]}`)
	out, err = e.invoke(contentCreatorInsightsGenerateTrendingID, fmt.Sprintf(sugCases()[1].args, clientID))
	if err != nil {
		t.Fatal(err)
	}
	trend := decode(t, out)["trend_insights"].([]any)[0].(map[string]any)
	if trend["trend"] != "EMERGING" || trend["trend_metrics"].(map[string]any)["trend_change_percent"] != 0.3 ||
		len(trend["trend_data_points"].([]any)) != 1 || len(trend["brand_sentiment_insights"].([]any)) != 1 ||
		len(trend["related_creators"].([]any)) != 1 {
		t.Errorf("out = %s", out)
	}

	// An answer that names an object of another account is refused.
	e.fake.routes["POST "+sugCustomerPath(clientID, ":generateTrendingInsights")] = okReply(`{"trendInsights":[{"trendAttribute":` +
		`{"attribute":{"userInterest":{"userInterestCategory":"customers/` + otherID + `/userInterests/5"}}}}]}`)
	if _, err := e.invoke(contentCreatorInsightsGenerateTrendingID, fmt.Sprintf(sugCases()[1].args, clientID)); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v, want invalid response", err)
	}
	// An interest of the account itself passes.
	e.fake.routes["POST "+sugCustomerPath(clientID, ":generateTrendingInsights")] = okReply(`{"trendInsights":[{"trendAttribute":` +
		`{"attribute":{"userInterest":{"userInterestCategory":"customers/` + clientID + `/userInterests/5"}}}}]}`)
	if _, err := e.invoke(contentCreatorInsightsGenerateTrendingID, fmt.Sprintf(sugCases()[1].args, clientID)); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestSuggestionToolsMapGoogleErrors(t *testing.T) {
	for _, c := range sugCases() {
		for name, f := range map[string]struct {
			r     reply
			class provider.Class
		}{
			"permission": {reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
				"USER_PERMISSION_DENIED", "")}, provider.ClassPermission},
			"allowlist": {reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "notAllowlistedError",
				"CUSTOMER_NOT_ALLOWLISTED_FOR_THIS_FEATURE", "")}, provider.ClassPermission},
			"quota": {reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
				"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"5s"}`)}, provider.ClassRateLimited},
		} {
			e := newEnvironment(t, nil, nil)
			e.fake.routes[c.route] = f.r
			_, err := e.invoke(c.id, c.arguments(clientID))
			if classOf(err) != f.class || err == nil || strings.Contains(err.Error(), detailCanary) ||
				strings.Contains(err.Error(), triggerCanary) || len(e.fake.api()) != 1 {
				t.Errorf("%s %s: err = %v, requests = %d", c.id, name, err, len(e.fake.api()))
			}
		}
		// A refusal of the input is Google's, with its code, and the request is not repeated.
		e := newEnvironment(t, nil, nil)
		e.fake.routes[c.route] = reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "requestError", "INVALID_INPUT", "")}
		if _, err := e.invoke(c.id, c.arguments(clientID)); err == nil || strings.Contains(err.Error(), detailCanary) ||
			len(e.fake.api()) != 1 {
			t.Errorf("%s: err = %v, requests = %d", c.id, err, len(e.fake.api()))
		}
		e = newEnvironment(t, nil, nil)
		e.fake.routes[c.route] = okReply(`[]`)
		if _, err := e.invoke(c.id, c.arguments(clientID)); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: a non-object answer gave %v", c.id, err)
		}
	}
}
