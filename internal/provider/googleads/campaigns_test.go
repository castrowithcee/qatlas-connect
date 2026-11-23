package googleads

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var campaignToolIDs = []string{campaignsCreateID, campaignsUpdateID, campaignsRemoveID, campaignsEnablePMaxBrandGuidelinesID}

func campaignsPath(id string) string { return "/v26/customers/" + id + "/campaigns:mutate" }
func brandPath(id string) string {
	return "/v26/customers/" + id + "/campaigns:enablePMaxBrandGuidelines"
}

var (
	own     = "customers/" + queryNumber(clientID)
	foreign = "customers/" + queryNumber(otherID)
)

const createdCampaign = `{"name":"Spring sale","status":"PAUSED","advertising_channel_type":"SEARCH"`

func campaignsCall(campaigns string, extra string) string {
	return `{"customer_id":"` + clientID + `","campaigns":` + campaigns + extra + `}`
}

func campaignCreateArgs(members string) string {
	return campaignsCall(`[`+createdCampaign+members+`}]`, "")
}

func campaignUpdateArgs(members string) string {
	return campaignsCall(`[{"resource_name":"`+own+`/campaigns/21"`+members+`}]`, "")
}

func TestCampaignToolsAreListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range campaignToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Fatalf("%s = %+v", id, d)
		}
		schema := string(d.InputSchema)
		for _, absent := range []string{"login_customer_id", "partial_failure", "third_party_integration_partners",
			"advertising_partner_ids", "fixed_cpm", "use_vehicle_inventory", "target_spend_micros", "phone_numbers",
			"reservation_ad_category_self_disclosure", "target_restriction_operations", "bidding_strategy_type",
			"google_ads_conversion_customer"} {
			if strings.Contains(schema, `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	update, _, _ := reg.Lookup(campaignsUpdateID)
	if strings.Contains(string(update.InputSchema), `"advertising_channel_type"`) ||
		strings.Contains(string(update.InputSchema), `"brand_guidelines_enabled"`) ||
		strings.Contains(string(update.InputSchema), `"app_id"`) {
		t.Error("campaigns.update offers a member that only creation accepts")
	}
	create, _, _ := reg.Lookup(campaignsCreateID)
	if !strings.Contains(string(create.InputSchema), `"advertising_channel_type"`) ||
		!strings.Contains(string(create.InputSchema), `"url_custom_parameters"`) {
		t.Error("campaigns.create lacks a member of the resource")
	}
	remove, _, _ := reg.Lookup(campaignsRemoveID)
	if strings.Contains(string(remove.InputSchema), "response_content_type") {
		t.Error("campaigns.remove offers a response content type")
	}
	enable, _, _ := reg.Lookup(campaignsEnablePMaxBrandGuidelinesID)
	if strings.Contains(string(enable.InputSchema), "validate_only") {
		t.Error("the brand guidelines call has no validate_only in Google")
	}
}

// Every scalar member that is not part of a repeated message is a field of the catalog of v26. The members of
// repeated messages are not fields of it, and the bid limits of the two maximize strategies are listed in the
// resource but not in the catalog.
func TestCampaignMembersAreFieldsOfTheCatalog(t *testing.T) {
	exempt := map[string]bool{
		"campaign.maximize_conversions.cpc_bid_ceiling_micros":      true,
		"campaign.maximize_conversions.cpc_bid_floor_micros":        true,
		"campaign.maximize_conversion_value.cpc_bid_ceiling_micros": true,
		"campaign.maximize_conversion_value.cpc_bid_floor_micros":   true,
	}
	var walk func(n node, path string)
	walk = func(n node, path string) {
		for _, f := range n.fields {
			full := path + "." + f.name
			if f.list {
				continue
			}
			_, ok := LookupField(full)
			if f.kind != kindObject && !ok && !exempt[full] {
				t.Errorf("%s is not in the catalog", full)
			}
			walk(f, full)
		}
	}
	walk(campaignTree, "campaign")
}

func TestCampaignChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		campaignsCreateID: campaignCreateArgs(``),
		campaignsUpdateID: campaignUpdateArgs(`,"status":"ENABLED"`),
		campaignsRemoveID: `{"customer_id":"` + clientID + `","resource_names":["` + own + `/campaigns/21"]}`,
		campaignsEnablePMaxBrandGuidelinesID: `{"customer_id":"` + clientID + `","campaign":"` + own +
			`/campaigns/21","auto_populate_brand_assets":true}`,
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
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
		t.Run(id+" outside the targets", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + otherID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignSummaryNamesTheSpendingEffect(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(campaignsCreateID, campaignsCall(`[{"name":"Spring sale","status":"ENABLED",`+
		`"advertising_channel_type":"SEARCH","campaign_budget":"`+own+`/campaignBudgets/11","target_cpa":`+
		`{"target_cpa_micros":2000000}}]`, `,"validate_only":true`))
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "Spring sale", "ENABLED", "SEARCH", "campaignBudgets/11", "target_cpa", "spends"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(campaignsRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+own+`/campaigns/21"]}`)
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "cannot be enabled again") {
		t.Errorf("summary = %v", err)
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestCampaignArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	// at keeps the user-info samples from reading as mail addresses.
	const at = "@"
	bad := map[string]struct{ id, arguments string }{
		"no status":   {campaignsCreateID, campaignsCall(`[{"name":"x","advertising_channel_type":"SEARCH"}]`, "")},
		"no name":     {campaignsCreateID, campaignsCall(`[{"status":"PAUSED","advertising_channel_type":"SEARCH"}]`, "")},
		"no channel":  {campaignsCreateID, campaignsCall(`[{"name":"x","status":"PAUSED"}]`, "")},
		"removed":     {campaignsCreateID, campaignCreateArgs(`,"status":"REMOVED"`)},
		"unknown":     {campaignsCreateID, campaignCreateArgs(`,"advertising_channel_type":"UNKNOWN"`)},
		"no campaign": {campaignsCreateID, campaignsCall(`[]`, "")},
		"eleven":      {campaignsCreateID, campaignsCall(`[`+strings.Repeat(createdCampaign+`},`, 10)+createdCampaign+`}]`, "")},
		"http template": {campaignsCreateID,
			campaignCreateArgs(`,"tracking_url_template":"http://t.example.com/?u={lpurl}"`)},
		"user info": {campaignsCreateID,
			campaignCreateArgs(`,"tracking_url_template":"https://user:pw` + at + `t.example.com/?u={lpurl}"`)},
		"template parameter first": {campaignsCreateID, campaignCreateArgs(`,"tracking_url_template":"{lpurl}?x=1"`)},
		"suffix address":           {campaignsCreateID, campaignCreateArgs(`,"final_url_suffix":"https://x.example.com/"`)},
		"suffix space":             {campaignsCreateID, campaignCreateArgs(`,"final_url_suffix":"a=1 b=2"`)},
		"foreign budget":           {campaignsCreateID, campaignCreateArgs(`,"campaign_budget":"` + foreign + `/campaignBudgets/11"`)},
		"temporary budget":         {campaignsCreateID, campaignCreateArgs(`,"campaign_budget":"` + own + `/campaignBudgets/-1"`)},
		"zero budget":              {campaignsCreateID, campaignCreateArgs(`,"campaign_budget":"` + own + `/campaignBudgets/0"`)},
		"budget collection":        {campaignsCreateID, campaignCreateArgs(`,"campaign_budget":"` + own + `/campaigns/11"`)},
		"foreign strategy":         {campaignsCreateID, campaignCreateArgs(`,"bidding_strategy":"` + foreign + `/biddingStrategies/3"`)},
		"temporary strategy":       {campaignsCreateID, campaignCreateArgs(`,"bidding_strategy":"` + own + `/biddingStrategies/-3"`)},
		"foreign group":            {campaignsCreateID, campaignCreateArgs(`,"campaign_group":"` + foreign + `/campaignGroups/3"`)},
		"foreign conversion action": {campaignsCreateID, campaignCreateArgs(`,"selective_optimization":{"conversion_actions":["` +
			foreign + `/conversionActions/3"]}`)},
		"two strategies":      {campaignsCreateID, campaignCreateArgs(`,"manual_cpm":{},"target_cpa":{"target_cpa_micros":1}`)},
		"strategy and shared": {campaignsCreateID, campaignCreateArgs(`,"bidding_strategy":"` + own + `/biddingStrategies/3","manual_cpc":{}`)},
		"negative micros":     {campaignsCreateID, campaignCreateArgs(`,"target_cpa":{"target_cpa_micros":-1}`)},
		"fraction micros":     {campaignsCreateID, campaignCreateArgs(`,"target_cpa":{"target_cpa_micros":1.5}`)},
		"roas range":          {campaignsCreateID, campaignCreateArgs(`,"target_roas":{"target_roas":2000}`)},
		"bad date":            {campaignsCreateID, campaignCreateArgs(`,"start_date_time":"2027-01-01"`)},
		"bad color":           {campaignsCreateID, campaignCreateArgs(`,"brand_guidelines":{"main_color":"green","accent_color":"#00ff00"}`)},
		"bad font":            {campaignsCreateID, campaignCreateArgs(`,"brand_guidelines":{"predefined_font_family":"Comic Sans"}`)},
		"both controls": {campaignsCreateID, campaignCreateArgs(`,"video_campaign_settings":{"video_ad_inventory_control":` +
			`{"allow_shorts":true},"video_ad_format_control":{"format_restriction":"NON_SKIPPABLE_IN_STREAM"}}`)},
		"crawl address": {campaignsCreateID, campaignCreateArgs(`,"asset_automation_settings":[{"asset_automation_type":"AUTOMATED_VIDEO_CRAWL",` +
			`"automated_video_crawl_setting":{"automated_video_crawl_infos":[{"url":"http://x.example.com/"}]}}]`)},
		"member of a message":  {campaignsCreateID, campaignCreateArgs(`,"network_settings":{"target_everything":true}`)},
		"empty message":        {campaignsCreateID, campaignCreateArgs(`,"network_settings":{}`)},
		"filled empty message": {campaignsCreateID, campaignCreateArgs(`,"manual_cpm":{"x":1}`)},
		"excluded member":      {campaignsCreateID, campaignCreateArgs(`,"third_party_integration_partners":{}`)},
		"excluded strategy":    {campaignsCreateID, campaignCreateArgs(`,"fixed_cpm":{"goal":"TARGET_FREQUENCY"}`)},
		"deprecated member":    {campaignsCreateID, campaignCreateArgs(`,"target_spend":{"target_spend_micros":5}`)},
		"output only member":   {campaignsCreateID, campaignCreateArgs(`,"bidding_strategy_type":"MANUAL_CPC"`)},
		"content type":         {campaignsCreateID, campaignsCall(`[`+createdCampaign+`}]`, `,"response_content_type":"ALL"`)},

		"update no member":  {campaignsUpdateID, campaignUpdateArgs(``)},
		"update channel":    {campaignsUpdateID, campaignUpdateArgs(`,"advertising_channel_type":"DISPLAY"`)},
		"update guidelines": {campaignsUpdateID, campaignUpdateArgs(`,"brand_guidelines_enabled":true`)},
		"update removed":    {campaignsUpdateID, campaignUpdateArgs(`,"status":"REMOVED"`)},
		"update foreign":    {campaignsUpdateID, campaignsCall(`[{"resource_name":"`+foreign+`/campaigns/21","status":"PAUSED"}]`, "")},
		"update temporary":  {campaignsUpdateID, campaignsCall(`[{"resource_name":"`+own+`/campaigns/-2","status":"PAUSED"}]`, "")},
		"update twice": {campaignsUpdateID, campaignsCall(`[{"resource_name":"`+own+`/campaigns/21","status":"PAUSED"},`+
			`{"resource_name":"`+own+`/campaigns/21","status":"ENABLED"}]`, "")},
		"update foreign budget": {campaignsUpdateID, campaignUpdateArgs(`,"campaign_budget":"` + foreign + `/campaignBudgets/1"`)},
		"update template":       {campaignsUpdateID, campaignUpdateArgs(`,"tracking_url_template":"https://evil.example` + at + `t.example.com/"`)},

		"remove foreign":   {campaignsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + foreign + `/campaigns/21"]}`},
		"remove temporary": {campaignsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/campaigns/-21"]}`},
		"remove twice": {campaignsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/campaigns/21","` +
			own + `/campaigns/21"]}`},
		"remove none":    {campaignsRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
		"remove content": {campaignsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/campaigns/21"],"response_content_type":"MUTABLE_RESOURCE"}`},

		"brand no assets": {campaignsEnablePMaxBrandGuidelinesID, `{"customer_id":"` + clientID + `","campaign":"` + own +
			`/campaigns/21","auto_populate_brand_assets":false}`},
		"brand foreign asset": {campaignsEnablePMaxBrandGuidelinesID, `{"customer_id":"` + clientID + `","campaign":"` + own +
			`/campaigns/21","auto_populate_brand_assets":false,"brand_assets":{"business_name_asset":"` + foreign +
			`/assets/1","logo_asset":["` + own + `/assets/2"]}}`},
		"brand temporary asset": {campaignsEnablePMaxBrandGuidelinesID, `{"customer_id":"` + clientID + `","campaign":"` + own +
			`/campaigns/21","auto_populate_brand_assets":false,"brand_assets":{"business_name_asset":"` + own +
			`/assets/-1","logo_asset":["` + own + `/assets/2"]}}`},
		"brand foreign campaign": {campaignsEnablePMaxBrandGuidelinesID, `{"customer_id":"` + clientID + `","campaign":"` + foreign +
			`/campaigns/21","auto_populate_brand_assets":true}`},
		"brand one color": {campaignsEnablePMaxBrandGuidelinesID, `{"customer_id":"` + clientID + `","campaign":"` + own +
			`/campaigns/21","auto_populate_brand_assets":true,"main_color":"#00ff00"}`},
		"brand no flag": {campaignsEnablePMaxBrandGuidelinesID, `{"customer_id":"` + clientID + `","campaign":"` + own + `/campaigns/21"}`},
		"brand domain":  {campaignsEnablePMaxBrandGuidelinesID, `{"customer_id":"` + clientID + `","campaign":"` + own + `/campaigns/21","auto_populate_brand_assets":true,"final_uri_domain":"https://x.example.com/p"}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c.id, c.arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func campaignResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

func TestCampaignsCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+campaignsPath(clientID)] = campaignResults(own+"/campaigns/21", own+"/campaigns/22")
	out, err := e.invokeConfirmed(campaignsCreateID, campaignsCall(`[{"name":"Spring sale","status":"PAUSED",`+
		`"advertising_channel_type":"SEARCH","campaign_budget":"`+own+`/campaignBudgets/11","target_cpa":`+
		`{"target_cpa_micros":2000000,"cpc_bid_ceiling_micros":900000},"network_settings":{"target_google_search":true,`+
		`"target_search_network":false},"start_date_time":"2027-03-01 00:00:00","end_date_time":"2027-03-31 23:59:59",`+
		`"tracking_url_template":"https://t.example.com/?u={lpurl}&c={campaignid}","final_url_suffix":"utm_source=g",`+
		`"url_custom_parameters":[{"key":"season","value":"spring"}],"frequency_caps":[{"key":{"level":"CAMPAIGN",`+
		`"event_type":"IMPRESSION","time_unit":"DAY","time_length":1},"cap":5}],"contains_eu_political_advertising":`+
		`"DOES_NOT_CONTAIN_EU_POLITICAL_ADVERTISING"},{"name":"Brand","status":"ENABLED","advertising_channel_type":`+
		`"PERFORMANCE_MAX","bidding_strategy":"`+own+`/biddingStrategies/3","campaign_budget":"`+own+
		`/campaignBudgets/12","brand_guidelines_enabled":true,"brand_guidelines":{"main_color":"#00ff00",`+
		`"accent_color":"#0000ff","predefined_font_family":"Open Sans"}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
		results[0].(map[string]any)["resource_name"] != own+"/campaigns/21" ||
		results[1].(map[string]any)["resource_name"] != own+"/campaigns/22" || results[0].(map[string]any)["campaign"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+campaignsPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	want := wantWire(t, `[{"create":{"name":"Spring sale","status":"PAUSED","advertisingChannelType":"SEARCH",`+
		`"campaignBudget":"`+own+`/campaignBudgets/11","targetCpa":{"targetCpaMicros":2000000,"cpcBidCeilingMicros":900000},`+
		`"networkSettings":{"targetGoogleSearch":true,"targetSearchNetwork":false},"startDateTime":"2027-03-01 00:00:00",`+
		`"endDateTime":"2027-03-31 23:59:59","trackingUrlTemplate":"https://t.example.com/?u={lpurl}&c={campaignid}",`+
		`"finalUrlSuffix":"utm_source=g","urlCustomParameters":[{"key":"season","value":"spring"}],"frequencyCaps":[`+
		`{"key":{"level":"CAMPAIGN","eventType":"IMPRESSION","timeUnit":"DAY","timeLength":1},"cap":5}],`+
		`"containsEuPoliticalAdvertising":"DOES_NOT_CONTAIN_EU_POLITICAL_ADVERTISING"}},{"create":{"name":"Brand",`+
		`"status":"ENABLED","advertisingChannelType":"PERFORMANCE_MAX","biddingStrategy":"`+own+`/biddingStrategies/3",`+
		`"campaignBudget":"`+own+`/campaignBudgets/12","brandGuidelinesEnabled":true,"brandGuidelines":{"mainColor":"#00ff00",`+
		`"accentColor":"#0000ff","predefinedFontFamily":"Open Sans"}}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

func TestCampaignsUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignsPath(clientID)] = campaignResults(own+"/campaigns/21", own+"/campaigns/22", own+"/campaigns/23")
	_, err := e.invokeConfirmed(campaignsUpdateID, campaignsCall(`[{"resource_name":"`+own+`/campaigns/21","status":"ENABLED",`+
		`"network_settings":{"target_search_network":true},"target_roas":{"target_roas":3.5,"cpc_bid_ceiling_micros":700000},`+
		`"url_custom_parameters":[{"key":"a","value":"b"}]},`+
		`{"resource_name":"`+own+`/campaigns/22","end_date_time":"","tracking_url_template":"","final_url_suffix":"x=1"},`+
		`{"resource_name":"`+own+`/campaigns/23","manual_cpm":{}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, second, third := ops[0].(map[string]any), ops[1].(map[string]any), ops[2].(map[string]any)
	if first["updateMask"] != "networkSettings.targetSearchNetwork,status,targetRoas.cpcBidCeilingMicros,"+
		"targetRoas.targetRoas,urlCustomParameters" {
		t.Errorf("first mask = %v", first["updateMask"])
	}
	if !reflect.DeepEqual(first["update"], wantWire(t, `{"resourceName":"`+own+`/campaigns/21","status":"ENABLED",`+
		`"networkSettings":{"targetSearchNetwork":true},"targetRoas":{"targetRoas":3.5,"cpcBidCeilingMicros":700000},`+
		`"urlCustomParameters":[{"key":"a","value":"b"}]}`)) {
		t.Errorf("first update = %v", first["update"])
	}
	// A text that is cleared is in the mask and has no value.
	if second["updateMask"] != "endDateTime,finalUrlSuffix,trackingUrlTemplate" ||
		!reflect.DeepEqual(second["update"], map[string]any{"resourceName": own + "/campaigns/22", "finalUrlSuffix": "x=1"}) {
		t.Errorf("second operation = %v", second)
	}
	// A message without members is set by its path alone.
	if third["updateMask"] != "manualCpm" || !reflect.DeepEqual(third["update"], map[string]any{
		"resourceName": own + "/campaigns/23", "manualCpm": map[string]any{}}) {
		t.Errorf("third operation = %v", third)
	}
}

func TestCampaignsRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignsPath(clientID)] = campaignResults(own+"/campaigns/21", own+"/campaigns/22")
	out, err := e.invokeConfirmed(campaignsRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+own+
		`/campaigns/21","`+own+`/campaigns/22"]}`)
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+own+`/campaigns/21"},{"remove":"`+own+
		`/campaigns/22"}]`)) || len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestCampaignValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		campaignsCreateID: campaignsCall(`[`+createdCampaign+`}]`, `,"validate_only":true`),
		campaignsUpdateID: campaignsCall(`[{"resource_name":"`+own+`/campaigns/21","status":"PAUSED"}]`, `,"validate_only":true`),
		campaignsRemoveID: `{"customer_id":"` + clientID + `","resource_names":["` + own + `/campaigns/21"],"validate_only":true}`,
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+campaignsPath(clientID)] = okReply(answer)
				out, err := e.invokeConfirmed(id, arguments)
				got := decode(t, out)
				if err != nil || got["validate_only"] != true || got["results"] != nil {
					t.Fatalf("out=%s err=%v", out, err)
				}
				if bodyOf(t, e.posts()[0])["validateOnly"] != true || len(e.posts()) != 1 {
					t.Errorf("posts = %+v", e.posts())
				}
			})
		}
	}
}

func TestCampaignResponseContentTypeShowsTheMutableCampaign(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + own + `/campaigns/21",` +
		`"campaign":{"resourceName":"` + own + `/campaigns/21","name":"Spring sale","status":"PAUSED",` +
		`"advertisingChannelType":"SEARCH","campaignBudget":"` + own + `/campaignBudgets/11","targetCpa":` +
		`{"targetCpaMicros":"2000000"},"networkSettings":{"targetGoogleSearch":true},"manualCpm":{},` +
		`"frequencyCaps":[{"key":{"level":"CAMPAIGN","timeLength":1},"cap":5}],"id":"21",` +
		`"biddingStrategyType":"TARGET_CPA","thirdPartyIntegrationPartners":{"x":1},"unknownMember":"never held"}}]}`)
	out, err := e.invokeConfirmed(campaignsUpdateID, campaignsCall(`[{"resource_name":"`+own+`/campaigns/21","status":"PAUSED"}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	campaign := decode(t, out)["results"].([]any)[0].(map[string]any)["campaign"].(map[string]any)
	want := map[string]any{"resource_name": own + "/campaigns/21", "name": "Spring sale", "status": "PAUSED",
		"advertising_channel_type": "SEARCH", "campaign_budget": own + "/campaignBudgets/11",
		"target_cpa":       map[string]any{"target_cpa_micros": float64(2000000)},
		"network_settings": map[string]any{"target_google_search": true},
		"frequency_caps":   []any{map[string]any{"key": map[string]any{"level": "CAMPAIGN", "time_length": float64(1)}, "cap": float64(5)}}}
	if !reflect.DeepEqual(campaign, want) {
		t.Errorf("campaign = %v", campaign)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that names another account, another campaign, or another number of results than asked for never
// passes. The change was made as far as Qatlas can tell, so the error says that the outcome is unknown.
func TestCampaignAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	update := campaignsCall(`[{"resource_name":"`+own+`/campaigns/21","status":"PAUSED"}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	create := campaignsCall(`[`+createdCampaign+`}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":  {campaignsCreateID, create, `{"results":[{"resourceName":"` + foreign + `/campaigns/21"}]}`},
		"other campaign":  {campaignsUpdateID, update, `{"results":[{"resourceName":"` + own + `/campaigns/99"}]}`},
		"no name":         {campaignsUpdateID, update, `{"results":[{}]}`},
		"no results":      {campaignsUpdateID, update, `{}`},
		"too many":        {campaignsCreateID, create, `{"results":[{"resourceName":"` + own + `/campaigns/1"},{"resourceName":"` + own + `/campaigns/2"}]}`},
		"partial failure": {campaignsUpdateID, update, `{"results":[{"resourceName":"` + own + `/campaigns/21"}],"partialFailureError":{"code":3}}`},
		"foreign budget": {campaignsUpdateID, update, `{"results":[{"resourceName":"` + own + `/campaigns/21","campaign":{"resourceName":"` +
			own + `/campaigns/21","campaignBudget":"` + foreign + `/campaignBudgets/1"}}]}`},
		"foreign strategy": {campaignsUpdateID, update, `{"results":[{"resourceName":"` + own + `/campaigns/21","campaign":{"resourceName":"` +
			own + `/campaigns/21","biddingStrategy":"` + foreign + `/biddingStrategies/1"}}]}`},
		"other campaign inside": {campaignsUpdateID, update, `{"results":[{"resourceName":"` + own + `/campaigns/21","campaign":{"resourceName":"` +
			own + `/campaigns/22"}}]}`},
		"member of the wrong kind": {campaignsUpdateID, update, `{"results":[{"resourceName":"` + own + `/campaigns/21","campaign":{"resourceName":"` +
			own + `/campaigns/21","networkSettings":"x"}}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestCampaignFailuresMapWithoutProviderText(t *testing.T) {
	arguments := campaignsCall(`[`+createdCampaign+`}]`, "")
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "campaignError", "DUPLICATE_CAMPAIGN_NAME", ""), "",
			provider.ClassProviderError},
		"permission": {403, adsFailure(403, "PERMISSION_DENIED", "authorizationError", "USER_PERMISSION_DENIED", ""), "",
			provider.ClassPermission},
		"quota": {429, adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED",
			`{"rateScope":"ACCOUNT","retryDelay":"3s"}`), "", provider.ClassRateLimited},
		"server": {503, googleError(503, "UNAVAILABLE", ""), "unknown", provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignsPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(campaignsCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+campaignsPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(campaignsCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

func TestEnablePMaxBrandGuidelinesSendsOneOperation(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+brandPath(clientID)] = okReply(`{"results":[{"campaign":"` + own + `/campaigns/21"}]}`)
	out, err := e.invokeConfirmed(campaignsEnablePMaxBrandGuidelinesID, `{"customer_id":"`+clientID+`","campaign":"`+own+
		`/campaigns/21","auto_populate_brand_assets":false,"brand_assets":{"business_name_asset":"`+own+`/assets/1",`+
		`"logo_asset":["`+own+`/assets/2"],"landscape_logo_asset":["`+own+`/assets/3"]},"final_uri_domain":"www.example.com",`+
		`"main_color":"#00ff00","accent_color":"#0000ff","font_family":"Roboto Slab"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if got["enabled"] != true || got["campaign"] != own+"/campaigns/21" || got["customer_id"] != clientID {
		t.Errorf("out = %s", out)
	}
	requests := e.fake.api()
	if len(requests) != 1 || requests[0].method != http.MethodPost || requests[0].login != loginID ||
		requests[0].path != brandPath(clientID) {
		t.Fatalf("requests = %+v", requests)
	}
	body := bodyOf(t, requests[0])
	want := wantWire(t, `[{"campaign":"`+own+`/campaigns/21","autoPopulateBrandAssets":false,"brandAssets":{"businessNameAsset":"`+
		own+`/assets/1","logoAsset":["`+own+`/assets/2"],"landscapeLogoAsset":["`+own+`/assets/3"]},"finalUriDomain":`+
		`"www.example.com","mainColor":"#00ff00","accentColor":"#0000ff","fontFamily":"Roboto Slab"}]`)
	if len(body) != 1 || !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("body = %s", requests[0].body)
	}
}

func TestEnablePMaxBrandGuidelinesAnswers(t *testing.T) {
	arguments := `{"customer_id":"` + clientID + `","campaign":"` + own + `/campaigns/21","auto_populate_brand_assets":true}`
	t.Run("a campaign Google could not enable is the error of the call", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+brandPath(clientID)] = okReply(`{"results":[{"campaign":"` + own + `/campaigns/21",` +
			`"enablementError":{"code":3,"message":"` + detailCanary + `","details":[{"@type":` +
			`"type.googleapis.com/google.ads.googleads.v26.errors.GoogleAdsFailure","errors":[{"errorCode":` +
			`{"brandGuidelinesMigrationError":"CAMPAIGN_NOT_ELIGIBLE"},"message":"` + detailCanary + `"}]}]}}]}`)
		_, err := e.invokeConfirmed(campaignsEnablePMaxBrandGuidelinesID, arguments)
		if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) ||
			!strings.Contains(err.Error(), "brandGuidelinesMigrationError CAMPAIGN_NOT_ELIGIBLE") {
			t.Fatalf("err = %v", err)
		}
	})
	for name, body := range map[string]string{
		"another campaign": `{"results":[{"campaign":"` + own + `/campaigns/99"}]}`,
		"foreign campaign": `{"results":[{"campaign":"` + foreign + `/campaigns/21"}]}`,
		"no result":        `{}`,
		"two results":      `{"results":[{"campaign":"` + own + `/campaigns/21"},{"campaign":"` + own + `/campaigns/21"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+brandPath(clientID)] = okReply(body)
			_, err := e.invokeConfirmed(campaignsEnablePMaxBrandGuidelinesID, arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	t.Run("a server error is not repeated", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+brandPath(clientID)] = reply{status: 500, body: googleError(500, "INTERNAL", "")}
		_, err := e.invokeConfirmed(campaignsEnablePMaxBrandGuidelinesID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.fake.api()) != 1 {
			t.Fatalf("err = %v, requests = %d", err, len(e.fake.api()))
		}
	})
}

// A manager account has no campaigns: a customer target that names one is refused before the change.
func TestCampaignChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(campaignsRemoveID, `{"customer_id":"`+managerID+`","resource_names":["customers/`+
		queryNumber(managerID)+`/campaigns/21"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
