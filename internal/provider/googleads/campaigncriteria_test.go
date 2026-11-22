package googleads

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var campaignCriteriaToolIDs = []string{campaignCriteriaCreateID, campaignCriteriaUpdateID, campaignCriteriaRemoveID}

func criteriaPath(id string) string { return "/v26/customers/" + id + "/campaignCriteria:mutate" }

var (
	ownCampaign  = own + "/campaigns/21"
	ownCriterion = own + "/campaignCriteria/21~7"
)

func criteriaCall(criteria, extra string) string {
	return `{"customer_id":"` + clientID + `","criteria":` + criteria + extra + `}`
}

func criterionCreateArgs(members string) string {
	return criteriaCall(`[{"campaign":"`+ownCampaign+`"`+members+`}]`, "")
}

func criterionUpdateArgs(members string) string {
	return criteriaCall(`[{"resource_name":"`+ownCriterion+`"`+members+`}]`, "")
}

func criterionRemoveArgs(names ...string) string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + strings.Join(names, `","`) + `"]}`
}

func criteriaResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

func TestCampaignCriteriaToolsAreListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range campaignCriteriaToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired || d.Version != 1 {
			t.Fatalf("%s = %+v", id, d)
		}
		schema := string(d.InputSchema)
		for _, absent := range []string{"login_customer_id", "partial_failure", "webpage_list", "feed_item_sets", "criterion_id",
			"display_name", "require_eu_political_advertising_declaration", "coverage_percentage", "sample_urls",
			"product_grouping", "product_labels", "product_legacy_condition", "product_type_full"} {
			if strings.Contains(schema, `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	update, _, _ := reg.Lookup(campaignCriteriaUpdateID)
	for _, absent := range []string{`"campaign"`, `"negative"`, `"keyword"`, `"location"`, `"language"`} {
		if strings.Contains(string(update.InputSchema), absent) {
			t.Errorf("campaign-criteria.update offers %s, which Google accepts only at creation", absent)
		}
	}
	create, _, _ := reg.Lookup(campaignCriteriaCreateID)
	for _, present := range []string{`"language"`, `"negative"`, `"bid_modifier"`, `"ad_schedule"`, `"ip_block"`, `"placement"`,
		`"webpage"`, `"keyword_theme"`, `"local_service_id"`, `"video_lineup"`, `"listing_scope"`, `"path"`,
		`"unknown_listing_dimension"`} {
		if !strings.Contains(string(create.InputSchema), present) {
			t.Errorf("campaign-criteria.create lacks %s", present)
		}
	}
	remove, _, _ := reg.Lookup(campaignCriteriaRemoveID)
	if strings.Contains(string(remove.InputSchema), "response_content_type") {
		t.Error("campaign-criteria.remove offers a response content type")
	}
}

// Every scalar member of a type is a field of the catalog of v26, except the few that Google accepts but does
// not make selectable.
func TestCampaignCriterionMembersAreFieldsOfTheCatalog(t *testing.T) {
	exempt := map[string]bool{
		"campaign_criterion.location_group.radius":                    true,
		"campaign_criterion.location_group.radius_units":              true,
		"campaign_criterion.location_group.location_group_asset_sets": true,
		"campaign_criterion.proximity.address.street_address2":        true,
		"campaign_criterion.webpage.conditions.operand":               true,
	}
	var walk func(n node, path string)
	walk = func(n node, path string) {
		for _, f := range n.fields {
			full := path + "." + f.name
			if f.list && f.kind == kindObject {
				continue
			}
			_, ok := LookupField(full)
			if f.kind != kindObject && !ok && !exempt[full] {
				t.Errorf("%s is not in the catalog", full)
			}
			walk(f, full)
		}
	}
	walk(campaignCriterionTree, "campaign_criterion")
}

func TestCampaignCriteriaChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		campaignCriteriaCreateID: criterionCreateArgs(`,"language":{"language_constant":"languageConstants/1001"}`),
		campaignCriteriaUpdateID: criterionUpdateArgs(`,"bid_modifier":1.5`),
		campaignCriteriaRemoveID: criterionRemoveArgs(ownCriterion),
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

func TestCampaignCriteriaSummaryNamesTheEffect(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	var needed *application.ConfirmationRequiredError
	_, err := e.invoke(campaignCriteriaCreateID, criteriaCall(`[{"campaign":"`+ownCampaign+`","location":`+
		`{"geo_target_constant":"geoTargetConstants/2276"},"bid_modifier":1.2,"status":"PAUSED"},{"campaign":"`+ownCampaign+
		`","negative":true,"keyword":{"text":"free","match_type":"EXACT"}}]`, `,"validate_only":true`))
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "location", "bid_modifier 1.2", "status PAUSED", "excluded", "keyword", "campaigns/21", "costs"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(campaignCriteriaRemoveID, criterionRemoveArgs(ownCriterion))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "cannot be restored") || !strings.Contains(needed.Summary, "21~7") {
		t.Errorf("summary = %v", err)
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestCampaignCriteriaArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	create := func(members string) struct{ id, arguments string } {
		return struct{ id, arguments string }{campaignCriteriaCreateID, criterionCreateArgs(members)}
	}
	// at keeps the user-info samples from reading as mail addresses.
	const at = "@"
	const location = `,"location":{"geo_target_constant":"geoTargetConstants/2276"}`
	bad := map[string]struct{ id, arguments string }{
		"no campaign": {campaignCriteriaCreateID, criteriaCall(`[{"location":{"geo_target_constant":"geoTargetConstants/2276"}}]`, "")},
		"no type":     create(`,"bid_modifier":1.5`),
		"two types": create(`,"location":{"geo_target_constant":"geoTargetConstants/2276"},"language":` +
			`{"language_constant":"languageConstants/1001"}`),
		"none": {campaignCriteriaCreateID, criteriaCall(`[]`, "")},
		"fifty one": {campaignCriteriaCreateID, criteriaCall(`[`+strings.Repeat(`{"campaign":"`+ownCampaign+`"`+location+`},`, 50)+
			`{"campaign":"`+ownCampaign+`"`+location+`}]`, "")},
		"foreign campaign":     {campaignCriteriaCreateID, criteriaCall(`[{"campaign":"`+foreign+`/campaigns/21"`+location+`}]`, "")},
		"temporary campaign":   {campaignCriteriaCreateID, criteriaCall(`[{"campaign":"`+own+`/campaigns/-1"`+location+`}]`, "")},
		"campaign collection":  {campaignCriteriaCreateID, criteriaCall(`[{"campaign":"`+own+`/adGroups/21"`+location+`}]`, "")},
		"excluded with bid":    create(`,"negative":true,"bid_modifier":1.5` + location),
		"excluded with status": create(`,"negative":true,"status":"PAUSED"` + location),
		"bid too low":          create(`,"bid_modifier":0.05` + location),
		"bid too high":         create(`,"bid_modifier":10.5` + location),
		"bid zero off device":  create(`,"bid_modifier":0` + location),
		"removed status":       create(`,"status":"REMOVED"` + location),
		"output only member":   create(`,"criterion_id":"7"` + location),
		"type member":          create(`,"type":"LOCATION"` + location),
		"unknown member":       create(`,"location":{"geo_target_constant":"geoTargetConstants/2276","radius":3}`),
		"empty type":           create(`,"location":{}`),
		"type not object":      create(`,"location":"geoTargetConstants/2276"`),
		// constants of Google are accepted in their fixed form only
		"foreign constant":     create(`,"location":{"geo_target_constant":"customers/` + clientID + `/geoTargetConstants/2276"}`),
		"zero constant":        create(`,"location":{"geo_target_constant":"geoTargetConstants/0"}`),
		"constant collection":  create(`,"language":{"language_constant":"geoTargetConstants/2276"}`),
		"constant with suffix": create(`,"language":{"language_constant":"languageConstants/1001/x"}`),
		"constant by name":     create(`,"language":{"language_constant":"languageConstants/german"}`),
		"carrier collection":   create(`,"carrier":{"carrier_constant":"languageConstants/1"}`),
		"topic constant":       create(`,"topic":{"topic_constant":"topicConstants/-5"}`),
		// references to objects of the account must belong to it
		"foreign user list":     create(`,"user_list":{"user_list":"` + foreign + `/userLists/3"}`),
		"temporary user list":   create(`,"user_list":{"user_list":"` + own + `/userLists/-3"}`),
		"foreign audience":      create(`,"custom_audience":{"custom_audience":"` + foreign + `/customAudiences/3"}`),
		"foreign combined":      create(`,"combined_audience":{"combined_audience":"` + foreign + `/combinedAudiences/3"}`),
		"foreign affinity":      create(`,"custom_affinity":{"custom_affinity":"` + foreign + `/customInterests/3"}`),
		"foreign interest":      create(`,"user_interest":{"user_interest_category":"` + foreign + `/userInterests/3"}`),
		"foreign brand list":    create(`,"brand_list":{"shared_set":"` + foreign + `/sharedSets/3"}`),
		"brand list collection": create(`,"brand_list":{"shared_set":"` + own + `/assets/3"}`),
		"foreign asset set":     create(`,"location_group":{"radius":5,"radius_units":"MILES","location_group_asset_sets":["` + foreign + `/assetSets/3"]}`),
		// rules of the types
		"keyword no match":     create(`,"keyword":{"text":"summer shoes"}`),
		"keyword no text":      create(`,"keyword":{"match_type":"BROAD"}`),
		"keyword long":         create(`,"keyword":{"text":"` + strings.Repeat("a", 81) + `","match_type":"BROAD"}`),
		"keyword words":        create(`,"keyword":{"text":"a b c d e f g h i j k","match_type":"BROAD"}`),
		"keyword control":      create(`,"keyword":{"text":"a\u0000b","match_type":"BROAD"}`),
		"keyword match":        create(`,"keyword":{"text":"x","match_type":"FUZZY"}`),
		"app no id":            create(`,"mobile_application":{"name":"Example"}`),
		"app platform":         create(`,"mobile_application":{"app_id":"3-com.example.app"}`),
		"label no type":        create(`,"content_label":{}`),
		"label unknown":        create(`,"content_label":{"type":"UNKNOWN"}`),
		"device unknown":       create(`,"device":{"type":"WATCH"}`),
		"schedule partial":     create(`,"ad_schedule":{"day_of_week":"MONDAY","start_hour":8,"end_hour":17}`),
		"schedule hour":        create(`,"ad_schedule":{"day_of_week":"MONDAY","start_hour":24,"start_minute":"ZERO","end_hour":24,"end_minute":"ZERO"}`),
		"schedule end hour":    create(`,"ad_schedule":{"day_of_week":"MONDAY","start_hour":8,"start_minute":"ZERO","end_hour":25,"end_minute":"ZERO"}`),
		"proximity nothing":    create(`,"proximity":{"radius":5}`),
		"proximity no radius":  create(`,"proximity":{"geo_point":{"longitude_in_micro_degrees":1,"latitude_in_micro_degrees":1}}`),
		"proximity half point": create(`,"proximity":{"radius":5,"geo_point":{"latitude_in_micro_degrees":1}}`),
		"proximity latitude":   create(`,"proximity":{"radius":5,"geo_point":{"longitude_in_micro_degrees":1,"latitude_in_micro_degrees":91000000}}`),
		"proximity units":      create(`,"proximity":{"radius":5,"radius_units":"METERS","address":{"city_name":"x"}}`),
		"group no radius":      create(`,"location_group":{"geo_target_constants":["geoTargetConstants/2276"]}`),
		"group two":            create(`,"location_group":{"radius":5,"radius_units":"MILES","geo_target_constants":["geoTargetConstants/1","geoTargetConstants/2"]}`),
		"group geo and sets": create(`,"location_group":{"radius":5,"radius_units":"MILES","geo_target_constants":["geoTargetConstants/1"],` +
			`"location_group_asset_sets":["` + own + `/assetSets/3"]}`),
		"group enable and sets": create(`,"location_group":{"radius":5,"radius_units":"MILES","enable_customer_level_location_asset_set":true,` +
			`"location_group_asset_sets":["` + own + `/assetSets/3"]}`),
		"group feed sets":       create(`,"location_group":{"radius":5,"radius_units":"MILES","feed_item_sets":["customers/` + clientID + `/feedItemSets/1"]}`),
		"ip text":               create(`,"ip_block":{"ip_address":"not an address"}`),
		"ip range":              create(`,"ip_block":{"ip_address":"2001:db8::1-2001:db8::9"}`),
		"ip zone":               create(`,"ip_block":{"ip_address":"fe80::1%eth0"}`),
		"video id":              create(`,"youtube_video":{"video_id":"short"}`),
		"channel id":            create(`,"youtube_channel":{"channel_id":"a b"}`),
		"life event":            create(`,"life_event":{"life_event_id":0}`),
		"demographic":           create(`,"extended_demographic":{"extended_demographic_id":-1}`),
		"age unknown":           create(`,"age_range":{"type":"AGE_RANGE_UNKNOWN"}`),
		"placement empty":       create(`,"placement":{"url":""}`),
		"placement scheme":      create(`,"placement":{"url":"ftp://www.example.com"}`),
		"placement user":        create(`,"placement":{"url":"https://user:pw` + at + `www.example.com"}`),
		"placement bare user":   create(`,"placement":{"url":"user` + at + `www.example.com"}`),
		"placement space":       create(`,"placement":{"url":"www.example.com/a b"}`),
		"placement no host":     create(`,"placement":{"url":"https:///page"}`),
		"placement long":        create(`,"placement":{"url":"` + strings.Repeat("a", 1025) + `"}`),
		"webpage no name":       create(`,"webpage":{"conditions":[]}`),
		"webpage no conditions": create(`,"webpage":{"criterion_name":"All"}`),
		"webpage operand":       create(`,"webpage":{"criterion_name":"x","conditions":[{"operand":"HOST","operator":"EQUALS","argument":"a"}]}`),
		"webpage operator":      create(`,"webpage":{"criterion_name":"x","conditions":[{"operand":"URL","operator":"STARTS","argument":"a"}]}`),
		"webpage no argument":   create(`,"webpage":{"criterion_name":"x","conditions":[{"operand":"URL","operator":"EQUALS"}]}`),
		"webpage read only":     create(`,"webpage":{"criterion_name":"x","conditions":[],"coverage_percentage":5}`),
		"webpage conditions":    create(`,"webpage":{"criterion_name":"x","conditions":"all"}`),
		"theme none":            create(`,"keyword_theme":{}`),
		"theme both":            create(`,"keyword_theme":{"keyword_theme_constant":"keywordThemeConstants/1~2","free_form_keyword_theme":"x"}`),
		"theme constant":        create(`,"keyword_theme":{"keyword_theme_constant":"keywordThemeConstants/1"}`),
		"service id":            create(`,"local_service_id":{"service_id":"a b"}`),
		"lineup":                create(`,"video_lineup":{"video_lineup_id":0}`),
		"topic nothing":         create(`,"topic":{}`),
		"topic path type":       create(`,"topic":{"path":"Pets"}`),
		"scope none":            create(`,"listing_scope":{}`),
		"scope empty":           create(`,"listing_scope":{"dimensions":[]}`),
		"scope two":             create(`,"listing_scope":{"dimensions":[{"hotel_id":{"value":"h1"},"hotel_class":{"value":3}}]}`),
		"scope class":           create(`,"listing_scope":{"dimensions":[{"hotel_class":{"value":6}}]}`),
		"scope rating":          create(`,"listing_scope":{"dimensions":[{"activity_rating":{"value":0}}]}`),
		"scope no level":        create(`,"listing_scope":{"dimensions":[{"product_category":{"category_id":166}}]}`),
		"scope foreign city":    create(`,"listing_scope":{"dimensions":[{"hotel_city":{"city_criterion":"customers/` + clientID + `/geoTargetConstants/1"}}]}`),
		"scope deprecated":      create(`,"listing_scope":{"dimensions":[{"product_grouping":{"value":"x"}}]}`),
		"scope level":           create(`,"listing_scope":{"dimensions":[{"product_type":{"value":"x","level":"LEVEL9"}}]}`),
		"content type":          {campaignCriteriaCreateID, criteriaCall(`[{"campaign":"`+ownCampaign+`"`+location+`}]`, `,"response_content_type":"ALL"`)},

		"update no member":      {campaignCriteriaUpdateID, criterionUpdateArgs(``)},
		"update type":           {campaignCriteriaUpdateID, criterionUpdateArgs(`,"language":{"language_constant":"languageConstants/1001"}`)},
		"update negative":       {campaignCriteriaUpdateID, criterionUpdateArgs(`,"negative":true`)},
		"update campaign":       {campaignCriteriaUpdateID, criterionUpdateArgs(`,"campaign":"` + ownCampaign + `"`)},
		"update removed":        {campaignCriteriaUpdateID, criterionUpdateArgs(`,"status":"REMOVED"`)},
		"update bid":            {campaignCriteriaUpdateID, criterionUpdateArgs(`,"bid_modifier":11`)},
		"update foreign":        {campaignCriteriaUpdateID, criteriaCall(`[{"resource_name":"`+foreign+`/campaignCriteria/21~7","bid_modifier":1.5}]`, "")},
		"update temporary":      {campaignCriteriaUpdateID, criteriaCall(`[{"resource_name":"`+own+`/campaignCriteria/-21~7","bid_modifier":1.5}]`, "")},
		"update single id":      {campaignCriteriaUpdateID, criteriaCall(`[{"resource_name":"`+own+`/campaignCriteria/21","bid_modifier":1.5}]`, "")},
		"update collection":     {campaignCriteriaUpdateID, criteriaCall(`[{"resource_name":"`+own+`/campaigns/21~7","bid_modifier":1.5}]`, "")},
		"update zero criterion": {campaignCriteriaUpdateID, criteriaCall(`[{"resource_name":"`+own+`/campaignCriteria/21~0","bid_modifier":1.5}]`, "")},
		"update twice": {campaignCriteriaUpdateID, criteriaCall(`[{"resource_name":"`+ownCriterion+`","bid_modifier":1.5},`+
			`{"resource_name":"`+ownCriterion+`","status":"PAUSED"}]`, "")},

		"remove foreign":   {campaignCriteriaRemoveID, criterionRemoveArgs(foreign + "/campaignCriteria/21~7")},
		"remove temporary": {campaignCriteriaRemoveID, criterionRemoveArgs(own + "/campaignCriteria/21~-7")},
		"remove campaign":  {campaignCriteriaRemoveID, criterionRemoveArgs(ownCampaign)},
		"remove twice":     {campaignCriteriaRemoveID, criterionRemoveArgs(ownCriterion, ownCriterion)},
		"remove none":      {campaignCriteriaRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
		"remove content": {campaignCriteriaRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + ownCriterion +
			`"],"response_content_type":"MUTABLE_RESOURCE"}`},
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

// Every type is sent in the form Google documents for it, with its members in the names of the wire.
func TestCampaignCriteriaCreateSendsEveryTypeAsOneAtomicRequest(t *testing.T) {
	cases := []struct{ name, members, wire string }{
		{"keyword", `"keyword":{"text":"summer shoes","match_type":"PHRASE"},"negative":true`,
			`"keyword":{"text":"summer shoes","matchType":"PHRASE"},"negative":true`},
		{"mobile_app_category", `"mobile_app_category":{"mobile_app_category_constant":"mobileAppCategoryConstants/60000"}`,
			`"mobileAppCategory":{"mobileAppCategoryConstant":"mobileAppCategoryConstants/60000"}`},
		{"mobile_application", `"mobile_application":{"app_id":"2-com.example.app","name":"Example"}`,
			`"mobileApplication":{"appId":"2-com.example.app","name":"Example"}`},
		{"location", `"location":{"geo_target_constant":"geoTargetConstants/2276"},"bid_modifier":1.2`,
			`"location":{"geoTargetConstant":"geoTargetConstants/2276"},"bidModifier":1.2`},
		{"device", `"device":{"type":"MOBILE"},"bid_modifier":0`, `"device":{"type":"MOBILE"},"bidModifier":0`},
		{"proximity point", `"proximity":{"geo_point":{"longitude_in_micro_degrees":13400000,"latitude_in_micro_degrees":52500000},` +
			`"radius":12.5,"radius_units":"KILOMETERS"}`,
			`"proximity":{"geoPoint":{"longitudeInMicroDegrees":13400000,"latitudeInMicroDegrees":52500000},"radius":12.5,"radiusUnits":"KILOMETERS"}`},
		{"proximity address", `"proximity":{"address":{"city_name":"Springfield","country_code":"US"},"radius":5}`,
			`"proximity":{"address":{"cityName":"Springfield","countryCode":"US"},"radius":5}`},
		{"language", `"language":{"language_constant":"languageConstants/1001"}`, `"language":{"languageConstant":"languageConstants/1001"}`},
		{"ip_block", `"ip_block":{"ip_address":"2001:db8::/32"},"negative":true`, `"ipBlock":{"ipAddress":"2001:db8::/32"},"negative":true`},
		{"content_label", `"content_label":{"type":"PARKED_DOMAIN"},"negative":true`, `"contentLabel":{"type":"PARKED_DOMAIN"},"negative":true`},
		{"carrier", `"carrier":{"carrier_constant":"carrierConstants/70092"}`, `"carrier":{"carrierConstant":"carrierConstants/70092"}`},
		{"user_interest", `"user_interest":{"user_interest_category":"` + own + `/userInterests/80001"}`,
			`"userInterest":{"userInterestCategory":"` + own + `/userInterests/80001"}`},
		{"operating_system_version", `"operating_system_version":{"operating_system_version_constant":"operatingSystemVersionConstants/630160"}`,
			`"operatingSystemVersion":{"operatingSystemVersionConstant":"operatingSystemVersionConstants/630160"}`},
		{"mobile_device", `"mobile_device":{"mobile_device_constant":"mobileDeviceConstants/1000"}`,
			`"mobileDevice":{"mobileDeviceConstant":"mobileDeviceConstants/1000"}`},
		{"location_group constants", `"location_group":{"geo_target_constants":["geoTargetConstants/2276"],"radius":10,"radius_units":"MILES"}`,
			`"locationGroup":{"geoTargetConstants":["geoTargetConstants/2276"],"radius":10,"radiusUnits":"MILES"}`},
		{"location_group asset sets", `"location_group":{"location_group_asset_sets":["` + own + `/assetSets/5"],"radius":1000,"radius_units":"METERS"}`,
			`"locationGroup":{"locationGroupAssetSets":["` + own + `/assetSets/5"],"radius":1000,"radiusUnits":"METERS"}`},
		{"custom_affinity", `"custom_affinity":{"custom_affinity":"` + own + `/customInterests/3"}`,
			`"customAffinity":{"customAffinity":"` + own + `/customInterests/3"}`},
		{"custom_audience", `"custom_audience":{"custom_audience":"` + own + `/customAudiences/3"}`,
			`"customAudience":{"customAudience":"` + own + `/customAudiences/3"}`},
		{"combined_audience", `"combined_audience":{"combined_audience":"` + own + `/combinedAudiences/3"}`,
			`"combinedAudience":{"combinedAudience":"` + own + `/combinedAudiences/3"}`},
		{"age_range", `"age_range":{"type":"AGE_RANGE_25_34"}`, `"ageRange":{"type":"AGE_RANGE_25_34"}`},
		{"gender", `"gender":{"type":"FEMALE"}`, `"gender":{"type":"FEMALE"}`},
		{"income_range", `"income_range":{"type":"INCOME_RANGE_90_UP"}`, `"incomeRange":{"type":"INCOME_RANGE_90_UP"}`},
		{"parental_status", `"parental_status":{"type":"PARENT"}`, `"parentalStatus":{"type":"PARENT"}`},
		{"user_list", `"user_list":{"user_list":"` + own + `/userLists/3"},"status":"PAUSED"`,
			`"userList":{"userList":"` + own + `/userLists/3"},"status":"PAUSED"`},
		{"youtube_video", `"youtube_video":{"video_id":"abcdefghijk"}`, `"youtubeVideo":{"videoId":"abcdefghijk"}`},
		{"youtube_channel", `"youtube_channel":{"channel_id":"UCabcdefghijklmnopqrstuv"}`, `"youtubeChannel":{"channelId":"UCabcdefghijklmnopqrstuv"}`},
		{"topic", `"topic":{"topic_constant":"topicConstants/3"}`, `"topic":{"topicConstant":"topicConstants/3"}`},
		{"ad_schedule", `"ad_schedule":{"day_of_week":"MONDAY","start_hour":8,"start_minute":"ZERO","end_hour":24,"end_minute":"ZERO"}`,
			`"adSchedule":{"dayOfWeek":"MONDAY","startHour":8,"startMinute":"ZERO","endHour":24,"endMinute":"ZERO"}`},
		{"life_event", `"life_event":{"life_event_id":10017}`, `"lifeEvent":{"lifeEventId":10017}`},
		{"extended_demographic", `"extended_demographic":{"extended_demographic_id":4}`, `"extendedDemographic":{"extendedDemographicId":4}`},
		{"placement", `"placement":{"url":"www.example.com"},"negative":true`, `"placement":{"url":"www.example.com"},"negative":true`},
		{"placement http", `"placement":{"url":"http://www.example.com/page?x=1"}`, `"placement":{"url":"http://www.example.com/page?x=1"}`},
		{"placement https", `"placement":{"url":"https://www.example.com:8443/"}`, `"placement":{"url":"https://www.example.com:8443/"}`},
		{"webpage", `"webpage":{"criterion_name":"Shoes","conditions":[{"operand":"URL","operator":"CONTAINS","argument":"/shoes"},` +
			`{"operand":"CUSTOM_LABEL","operator":"EQUALS","argument":"sale"}]}`,
			`"webpage":{"criterionName":"Shoes","conditions":[{"operand":"URL","operator":"CONTAINS","argument":"/shoes"},` +
				`{"operand":"CUSTOM_LABEL","operator":"EQUALS","argument":"sale"}]}`},
		{"webpage all pages", `"webpage":{"criterion_name":"All","conditions":[]}`, `"webpage":{"criterionName":"All","conditions":[]}`},
		{"keyword_theme constant", `"keyword_theme":{"keyword_theme_constant":"keywordThemeConstants/12~34"}`,
			`"keywordTheme":{"keywordThemeConstant":"keywordThemeConstants/12~34"}`},
		{"keyword_theme free form", `"keyword_theme":{"free_form_keyword_theme":"garden tools"}`, `"keywordTheme":{"freeFormKeywordTheme":"garden tools"}`},
		{"local_service_id", `"local_service_id":{"service_id":"install_faucet"}`, `"localServiceId":{"serviceId":"install_faucet"}`},
		{"video_lineup", `"video_lineup":{"video_lineup_id":12}`, `"videoLineup":{"videoLineupId":12}`},
		{"topic path", `"topic":{"path":["Pets & Animals","Pets","Dogs"]}`, `"topic":{"path":["Pets & Animals","Pets","Dogs"]}`},
		{"topic both", `"topic":{"topic_constant":"topicConstants/3","path":["Pets"]}`, `"topic":{"topicConstant":"topicConstants/3","path":["Pets"]}`},
		{"listing_scope", `"listing_scope":{"dimensions":[{"hotel_class":{"value":4}},{"hotel_city":{"city_criterion":"geoTargetConstants/1"}},` +
			`{"product_category":{"category_id":166,"level":"LEVEL2"}},{"product_brand":{"value":"Acme"}},` +
			`{"product_channel":{"channel":"ONLINE"}},{"product_custom_attribute":{"value":"x","index":"INDEX1"}},` +
			`{"activity_rating":{"value":5}},{"activity_country":{"value":"geoTargetConstants/2276"}},{"unknown_listing_dimension":{}}]}`,
			`"listingScope":{"dimensions":[{"hotelClass":{"value":4}},{"hotelCity":{"cityCriterion":"geoTargetConstants/1"}},` +
				`{"productCategory":{"categoryId":166,"level":"LEVEL2"}},{"productBrand":{"value":"Acme"}},` +
				`{"productChannel":{"channel":"ONLINE"}},{"productCustomAttribute":{"value":"x","index":"INDEX1"}},` +
				`{"activityRating":{"value":5}},{"activityCountry":{"value":"geoTargetConstants/2276"}},{"unknownListingDimension":{}}]}`},
		{"brand_list", `"brand_list":{"shared_set":"` + own + `/sharedSets/9"}`, `"brandList":{"sharedSet":"` + own + `/sharedSets/9"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+criteriaPath(clientID)] = criteriaResults(ownCriterion)
			out, err := e.invokeConfirmed(campaignCriteriaCreateID, criterionCreateArgs(","+c.members))
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if results := got["results"].([]any); got["customer_id"] != clientID || got["validate_only"] != false ||
				len(results) != 1 || results[0].(map[string]any)["resource_name"] != ownCriterion {
				t.Errorf("out = %s", out)
			}
			posts := e.posts()
			if len(posts) != 1 || len(e.fake.api()) != 1 {
				t.Fatalf("requests = %+v", e.fake.api())
			}
			p := posts[0]
			if p.url != "https://googleads.googleapis.com"+criteriaPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
				p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
				t.Errorf("request = %+v", p)
			}
			body := bodyOf(t, p)
			if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
				t.Errorf("body members = %v, want the operations alone", body)
			}
			want := wantWire(t, `[{"create":{"campaign":"`+ownCampaign+`",`+c.wire+`}}]`)
			if !reflect.DeepEqual(body["operations"], want) {
				t.Errorf("operations = %s", p.body)
			}
		})
	}
}

func TestCampaignCriteriaCreateTakesSeveralCriteriaInOneRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	other := own + "/campaignCriteria/22~8"
	e.fake.routes["POST "+criteriaPath(clientID)] = criteriaResults(ownCriterion, other)
	_, err := e.invokeConfirmed(campaignCriteriaCreateID, criteriaCall(`[{"campaign":"`+ownCampaign+`","language":`+
		`{"language_constant":"languageConstants/1001"}},{"campaign":"`+own+`/campaigns/22","gender":{"type":"MALE"}}]`, ""))
	if err != nil || len(e.posts()) != 1 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	if len(ops) != 2 {
		t.Errorf("operations = %v", ops)
	}
}

func TestCampaignCriteriaUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	second := own + "/campaignCriteria/22~8"
	e.fake.routes["POST "+criteriaPath(clientID)] = criteriaResults(ownCriterion, second)
	_, err := e.invokeConfirmed(campaignCriteriaUpdateID, criteriaCall(`[{"resource_name":"`+ownCriterion+`","bid_modifier":1.5,`+
		`"status":"PAUSED"},{"resource_name":"`+second+`","bid_modifier":0}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, next := ops[0].(map[string]any), ops[1].(map[string]any)
	if first["updateMask"] != "bidModifier,status" || !reflect.DeepEqual(first["update"], wantWire(t,
		`{"resourceName":"`+ownCriterion+`","bidModifier":1.5,"status":"PAUSED"}`)) {
		t.Errorf("first operation = %v", first)
	}
	if next["updateMask"] != "bidModifier" || !reflect.DeepEqual(next["update"], wantWire(t,
		`{"resourceName":"`+second+`","bidModifier":0}`)) {
		t.Errorf("second operation = %v", next)
	}
}

func TestCampaignCriteriaRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	second := own + "/campaignCriteria/22~8"
	e.fake.routes["POST "+criteriaPath(clientID)] = criteriaResults(ownCriterion, second)
	out, err := e.invokeConfirmed(campaignCriteriaRemoveID, criterionRemoveArgs(ownCriterion, second))
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+ownCriterion+`"},{"remove":"`+second+`"}]`)) || len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestCampaignCriteriaValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		campaignCriteriaCreateID: criteriaCall(`[{"campaign":"`+ownCampaign+`","language":{"language_constant":"languageConstants/1001"}}]`,
			`,"validate_only":true`),
		campaignCriteriaUpdateID: criteriaCall(`[{"resource_name":"`+ownCriterion+`","bid_modifier":2}]`, `,"validate_only":true`),
		campaignCriteriaRemoveID: `{"customer_id":"` + clientID + `","resource_names":["` + ownCriterion + `"],"validate_only":true}`,
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+criteriaPath(clientID)] = okReply(answer)
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

func TestCampaignCriteriaResponseContentTypeShowsTheMutableCriterion(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+criteriaPath(clientID)] = okReply(`{"results":[{"resourceName":"` + ownCriterion + `",` +
		`"campaignCriterion":{"resourceName":"` + ownCriterion + `","campaign":"` + ownCampaign + `","criterionId":"7",` +
		`"type":"LOCATION","displayName":"Germany","bidModifier":1.5,"status":"ENABLED","negative":false,` +
		`"location":{"geoTargetConstant":"geoTargetConstants/2276"},"webpage":{"criterionName":"x","conditions":[],` +
		`"coveragePercentage":0.25,"sample":{"sampleUrls":["https://www.example.com/a"]}},"unknownMember":"never held"}}]}`)
	out, err := e.invokeConfirmed(campaignCriteriaUpdateID, criteriaCall(`[{"resource_name":"`+ownCriterion+`","bid_modifier":1.5}]`, `,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	criterion := decode(t, out)["results"].([]any)[0].(map[string]any)["campaign_criterion"].(map[string]any)
	want := map[string]any{"resource_name": ownCriterion, "campaign": ownCampaign, "criterion_id": float64(7), "type": "LOCATION",
		"display_name": "Germany", "bid_modifier": 1.5, "status": "ENABLED", "negative": false,
		"location": map[string]any{"geo_target_constant": "geoTargetConstants/2276"},
		"webpage": map[string]any{"criterion_name": "x", "coverage_percentage": 0.25,
			"sample": map[string]any{"sample_urls": []any{"https://www.example.com/a"}}}}
	if !reflect.DeepEqual(criterion, want) {
		t.Errorf("criterion = %v", criterion)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that names another account, another criterion or campaign, or another number of results than asked
// for never passes. The change was made as far as Qatlas can tell, so the error says that the outcome is unknown.
func TestCampaignCriteriaAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	update := criteriaCall(`[{"resource_name":"`+ownCriterion+`","bid_modifier":1.5}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	create := criteriaCall(`[{"campaign":"`+ownCampaign+`","language":{"language_constant":"languageConstants/1001"}}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`)
	inside := func(member string) string {
		return `{"results":[{"resourceName":"` + ownCriterion + `","campaignCriterion":{"resourceName":"` + ownCriterion + `"` + member + `}}]}`
	}
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":     {campaignCriteriaCreateID, create, `{"results":[{"resourceName":"` + foreign + `/campaignCriteria/21~7"}]}`},
		"other campaign":     {campaignCriteriaCreateID, create, `{"results":[{"resourceName":"` + own + `/campaignCriteria/99~7"}]}`},
		"other criterion":    {campaignCriteriaUpdateID, update, `{"results":[{"resourceName":"` + own + `/campaignCriteria/21~8"}]}`},
		"single id":          {campaignCriteriaUpdateID, update, `{"results":[{"resourceName":"` + own + `/campaignCriteria/21"}]}`},
		"campaign name":      {campaignCriteriaUpdateID, update, `{"results":[{"resourceName":"` + ownCampaign + `"}]}`},
		"no name":            {campaignCriteriaUpdateID, update, `{"results":[{}]}`},
		"no results":         {campaignCriteriaUpdateID, update, `{}`},
		"too many":           {campaignCriteriaCreateID, create, `{"results":[{"resourceName":"` + ownCriterion + `"},{"resourceName":"` + ownCriterion + `"}]}`},
		"partial failure":    {campaignCriteriaUpdateID, update, `{"results":[{"resourceName":"` + ownCriterion + `"}],"partialFailureError":{"code":3}}`},
		"foreign campaign":   {campaignCriteriaUpdateID, update, inside(`,"campaign":"` + foreign + `/campaigns/21"`)},
		"foreign user list":  {campaignCriteriaUpdateID, update, inside(`,"userList":{"userList":"` + foreign + `/userLists/1"}`)},
		"foreign brand list": {campaignCriteriaUpdateID, update, inside(`,"brandList":{"sharedSet":"` + foreign + `/sharedSets/1"}`)},
		"other name inside": {campaignCriteriaUpdateID, update, `{"results":[{"resourceName":"` + ownCriterion +
			`","campaignCriterion":{"resourceName":"` + own + `/campaignCriteria/21~8"}}]}`},
		"wrong kind":        {campaignCriteriaUpdateID, update, inside(`,"location":"x"`)},
		"foreign dimension": {campaignCriteriaUpdateID, update, inside(`,"listingScope":{"dimensions":[{"hotelState":{"stateCriterion":"customers/` + clientID + `/geoTargetConstants/1"}}]}`)},
		"foreign theme":     {campaignCriteriaUpdateID, update, inside(`,"keywordTheme":{"keywordThemeConstant":"keywordThemeConstants/1"}`)},
		"foreign constant":  {campaignCriteriaUpdateID, update, inside(`,"location":{"geoTargetConstant":"customers/` + clientID + `/geoTargetConstants/1"}`)},
		"constant by name":  {campaignCriteriaUpdateID, update, inside(`,"language":{"languageConstant":"https://example.invalid/x"}`)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+criteriaPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestCampaignCriteriaFailuresMapWithoutProviderText(t *testing.T) {
	arguments := criterionCreateArgs(`,"language":{"language_constant":"languageConstants/1001"}`)
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "campaignCriterionError", "CANNOT_TARGET_AND_EXCLUDE", ""), "",
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
			e.fake.routes["POST "+criteriaPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(campaignCriteriaCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+criteriaPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(campaignCriteriaCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A language criterion on a Search campaign is the limit: Qatlas cannot tell the channel of a campaign without a
// read, so it sends the request once and shows Google's refusal as a refusal of the change, never as an unknown
// outcome and never repeated. Removing a language criterion stays possible.
func TestLanguageCriterionOnASearchCampaignIsGoogleRefusal(t *testing.T) {
	language := `,"language":{"language_constant":"languageConstants/1001"}`
	refusal := reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "contextError", "OPERATION_NOT_PERMITTED_FOR_CONTEXT", "")}
	for name, arguments := range map[string]string{
		"change":        criterionCreateArgs(language),
		"validate only": criteriaCall(`[{"campaign":"`+ownCampaign+`"`+language+`}]`, `,"validate_only":true`),
	} {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+criteriaPath(clientID)] = refusal
			_, err := e.invokeConfirmed(campaignCriteriaCreateID, arguments)
			if classOf(err) != provider.ClassProviderError || len(e.posts()) != 1 ||
				!strings.Contains(err.Error(), "contextError OPERATION_NOT_PERMITTED_FOR_CONTEXT") ||
				!strings.Contains(err.Error(), "nothing was changed") || strings.Contains(err.Error(), "unknown") ||
				strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a language criterion is removed", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+criteriaPath(clientID)] = criteriaResults(ownCriterion)
		if _, err := e.invokeConfirmed(campaignCriteriaRemoveID, criterionRemoveArgs(ownCriterion)); err != nil || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A manager account has no campaign criteria: a customer target that names one is refused before the change.
func TestCampaignCriteriaChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(campaignCriteriaRemoveID, `{"customer_id":"`+managerID+`","resource_names":["customers/`+
		queryNumber(managerID)+`/campaignCriteria/21~7"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
