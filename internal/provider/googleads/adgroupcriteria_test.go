package googleads

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var adGroupCriterionToolIDs = []string{adGroupCriteriaCreateID, adGroupCriteriaUpdateID, adGroupCriteriaRemoveID}

func adGroupCriteriaPath(id string) string { return "/v26/customers/" + id + "/adGroupCriteria:mutate" }

var criterionAdGroup = `"ad_group":"` + own + `/adGroups/31"`

var createdCriterion = `{` + criterionAdGroup + `,"status":"PAUSED","keyword":{"text":"running shoes","match_type":"EXACT"}`

func adGroupCriteriaCall(items, extra string) string {
	return `{"customer_id":"` + clientID + `","ad_group_criteria":` + items + extra + `}`
}

func adGroupCriterionCreateArgs(members string) string {
	return adGroupCriteriaCall(`[`+createdCriterion+members+`}]`, "")
}

func adGroupCriterionUpdateArgs(members string) string {
	return adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~41"`+members+`}]`, "")
}

func adGroupCriterionRemoveArgs(extra string) string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroupCriteria/31~41"]` + extra + `}`
}

func criterionResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

func TestAdGroupCriterionToolsAreListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range adGroupCriterionToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Fatalf("%s = %+v", id, d)
		}
		schema := string(d.InputSchema)
		for _, absent := range []string{"login_customer_id", "partial_failure", "effective_cpc_bid_micros",
			"primary_status", "labels", "criterion_id", "quality_info", "device", "ad_schedule", "product_grouping",
			"product_labels", "product_legacy_condition", "product_type_full",
			"google_ads_conversion_customer"} {
			if strings.Contains(schema, `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	update, _, _ := reg.Lookup(adGroupCriteriaUpdateID)
	for _, absent := range []string{`"ad_group"`, `"negative"`, `"keyword"`, `"user_list"`, `"webpage"`} {
		if strings.Contains(string(update.InputSchema), absent) {
			t.Errorf("ad-group-criteria.update offers %s, which only creation accepts", absent)
		}
	}
	create, _, _ := reg.Lookup(adGroupCriteriaCreateID)
	for _, kind := range criterionTypes {
		if !strings.Contains(string(create.InputSchema), `"`+kind+`"`) {
			t.Errorf("ad-group-criteria.create lacks the criterion type %s", kind)
		}
	}
	remove, _, _ := reg.Lookup(adGroupCriteriaRemoveID)
	if strings.Contains(string(remove.InputSchema), "response_content_type") {
		t.Error("ad-group-criteria.remove offers a response content type")
	}
}

// The criterion types offered are the union of the resource in the catalog of v26.
func TestCriterionTypesAreTheUnionOfTheResource(t *testing.T) {
	inCatalog := map[string]bool{}
	for _, f := range catalogFields {
		rest, ok := strings.CutPrefix(f.name, "ad_group_criterion.")
		if head, _, nested := strings.Cut(rest, "."); ok && nested {
			inCatalog[head] = true
		}
	}
	for _, notAUnionMember := range []string{"position_estimates", "quality_info"} {
		delete(inCatalog, notAUnionMember)
	}
	offered := map[string]bool{}
	for _, kind := range criterionTypes {
		offered[kind] = true
		if _, ok := adGroupCriterionTree.member(kind); !ok {
			t.Errorf("%s is no member of the tree", kind)
		}
	}
	if !reflect.DeepEqual(inCatalog, offered) || !reflect.DeepEqual(adGroupCriterionTree.exclusive, criterionTypes) {
		t.Errorf("catalog union = %v, offered = %v", inCatalog, offered)
	}
}

// Every scalar member that is not part of a repeated message is a field of the catalog of v26.
func TestAdGroupCriterionMembersAreFieldsOfTheCatalog(t *testing.T) {
	var walk func(n node, path string)
	walk = func(n node, path string) {
		for _, f := range n.fields {
			full := path + "." + f.name
			if f.list {
				continue
			}
			if _, ok := LookupField(full); f.kind != kindObject && !ok {
				t.Errorf("%s is not in the catalog", full)
			}
			walk(f, full)
		}
	}
	walk(adGroupCriterionTree, "ad_group_criterion")
}

func TestAdGroupCriterionChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		adGroupCriteriaCreateID: adGroupCriterionCreateArgs(``),
		adGroupCriteriaUpdateID: adGroupCriterionUpdateArgs(`,"status":"ENABLED"`),
		adGroupCriteriaRemoveID: adGroupCriterionRemoveArgs(""),
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

func TestAdGroupCriterionSummaryNamesTheSpendingEffect(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(adGroupCriteriaCreateID, adGroupCriteriaCall(`[{`+criterionAdGroup+`,"status":"ENABLED",`+
		`"keyword":{"text":"shoes","match_type":"BROAD"},"cpc_bid_micros":750000,"bid_modifier":1.5}]`, `,"validate_only":true`))
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "keyword", "ENABLED", "adGroups/31", "cpc_bid_micros", "bid_modifier", "bids"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(adGroupCriteriaRemoveID, adGroupCriterionRemoveArgs(""))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "not enabled again") {
		t.Errorf("summary = %v", err)
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestAdGroupCriterionArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	// at keeps the user-info samples from reading as mail addresses.
	const at = "@"
	item := func(members string) string { return adGroupCriteriaCall(`[{`+criterionAdGroup+members+`}]`, "") }
	bad := map[string]struct{ id, arguments string }{
		"no status":       {adGroupCriteriaCreateID, item(`,"keyword":{"text":"x","match_type":"EXACT"}`)},
		"no ad group":     {adGroupCriteriaCreateID, adGroupCriteriaCall(`[{"status":"PAUSED","keyword":{"text":"x","match_type":"EXACT"}}]`, "")},
		"no type":         {adGroupCriteriaCreateID, item(`,"status":"PAUSED"`)},
		"two types":       {adGroupCriteriaCreateID, item(`,"status":"PAUSED","keyword":{"text":"x","match_type":"EXACT"},"gender":{"type":"MALE"}`)},
		"removed":         {adGroupCriteriaCreateID, item(`,"status":"REMOVED","gender":{"type":"MALE"}`)},
		"none":            {adGroupCriteriaCreateID, adGroupCriteriaCall(`[]`, "")},
		"fifty one":       {adGroupCriteriaCreateID, adGroupCriteriaCall(`[`+strings.Repeat(createdCriterion+`},`, 50)+createdCriterion+`}]`, "")},
		"no match type":   {adGroupCriteriaCreateID, item(`,"status":"PAUSED","keyword":{"text":"x"}`)},
		"no keyword text": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","keyword":{"match_type":"EXACT"}`)},
		"empty keyword":   {adGroupCriteriaCreateID, item(`,"status":"PAUSED","keyword":{"text":"","match_type":"EXACT"}`)},
		"long keyword": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","keyword":{"text":"` + strings.Repeat("a", 81) +
			`","match_type":"EXACT"}`)},
		"byte keyword": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","keyword":{"text":"` + strings.Repeat("😀", 70) +
			`","match_type":"EXACT"}`)},
		"word keyword": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","keyword":{"text":"` + strings.Repeat("a ", 11) +
			`","match_type":"EXACT"}`)},
		"line feed keyword":  {adGroupCriteriaCreateID, item(`,"status":"PAUSED","keyword":{"text":"a\nb","match_type":"EXACT"}`)},
		"match type":         {adGroupCriteriaCreateID, item(`,"status":"PAUSED","keyword":{"text":"x","match_type":"LOOSE"}`)},
		"long placement":     {adGroupCriteriaCreateID, item(`,"status":"PAUSED","placement":{"url":"` + strings.Repeat("a", 251) + `"}`)},
		"empty type":         {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{}`)},
		"gender value":       {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"OTHER"}`)},
		"foreign ad group":   {adGroupCriteriaCreateID, adGroupCriteriaCall(`[{"ad_group":"`+foreign+`/adGroups/31","status":"PAUSED","gender":{"type":"MALE"}}]`, "")},
		"temp ad group":      {adGroupCriteriaCreateID, adGroupCriteriaCall(`[{"ad_group":"`+own+`/adGroups/-1","status":"PAUSED","gender":{"type":"MALE"}}]`, "")},
		"ad group kind":      {adGroupCriteriaCreateID, adGroupCriteriaCall(`[{"ad_group":"`+own+`/campaigns/31","status":"PAUSED","gender":{"type":"MALE"}}]`, "")},
		"foreign user list":  {adGroupCriteriaCreateID, item(`,"status":"PAUSED","user_list":{"user_list":"` + foreign + `/userLists/5"}`)},
		"temp user list":     {adGroupCriteriaCreateID, item(`,"status":"PAUSED","user_list":{"user_list":"` + own + `/userLists/-5"}`)},
		"user list kind":     {adGroupCriteriaCreateID, item(`,"status":"PAUSED","user_list":{"user_list":"` + own + `/audiences/5"}`)},
		"foreign custom":     {adGroupCriteriaCreateID, item(`,"status":"PAUSED","custom_audience":{"custom_audience":"` + foreign + `/customAudiences/5"}`)},
		"foreign combined":   {adGroupCriteriaCreateID, item(`,"status":"PAUSED","combined_audience":{"combined_audience":"` + foreign + `/combinedAudiences/5"}`)},
		"foreign interest":   {adGroupCriteriaCreateID, item(`,"status":"PAUSED","user_interest":{"user_interest_category":"` + foreign + `/userInterests/5"}`)},
		"foreign audience":   {adGroupCriteriaCreateID, item(`,"status":"PAUSED","audience":{"audience":"` + foreign + `/audiences/5"}`)},
		"foreign affinity":   {adGroupCriteriaCreateID, item(`,"status":"PAUSED","custom_affinity":{"custom_affinity":"` + foreign + `/customInterests/5"}`)},
		"foreign shared set": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","brand_list":{"shared_set":"` + foreign + `/sharedSets/5"}`)},
		"location of account": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","location":{"geo_target_constant":"` + own +
			`/geoTargetConstants/2840"}`)},
		"location form":    {adGroupCriteriaCreateID, item(`,"status":"PAUSED","location":{"geo_target_constant":"geoTargetConstants/-1"}`)},
		"language form":    {adGroupCriteriaCreateID, item(`,"status":"PAUSED","language":{"language_constant":"languageConstants/x"}`)},
		"topic form":       {adGroupCriteriaCreateID, item(`,"status":"PAUSED","topic":{"topic_constant":"topicConstants/0"}`)},
		"category form":    {adGroupCriteriaCreateID, item(`,"status":"PAUSED","mobile_app_category":{"mobile_app_category_constant":"x/1"}`)},
		"app id":           {adGroupCriteriaCreateID, item(`,"status":"PAUSED","mobile_application":{"app_id":"3-abc"}`)},
		"no app id":        {adGroupCriteriaCreateID, item(`,"status":"PAUSED","mobile_application":{"name":"x"}`)},
		"video id":         {adGroupCriteriaCreateID, item(`,"status":"PAUSED","youtube_video":{"video_id":"short"}`)},
		"no webpage name":  {adGroupCriteriaCreateID, item(`,"status":"PAUSED","webpage":{"conditions":[{"operand":"URL","operator":"CONTAINS","argument":"a"}]}`)},
		"no conditions":    {adGroupCriteriaCreateID, item(`,"status":"PAUSED","webpage":{"criterion_name":"x"}`)},
		"empty conditions": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","webpage":{"criterion_name":"x","conditions":[]}`)},
		"condition member": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","webpage":{"criterion_name":"x","conditions":[{"operand":"URL","operator":"CONTAINS"}]}`)},
		"condition value":  {adGroupCriteriaCreateID, item(`,"status":"PAUSED","webpage":{"criterion_name":"x","conditions":[{"operand":"HOST","operator":"CONTAINS","argument":"a"}]}`)},
		"listing no type":  {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"case_value":{"product_brand":{"value":"x"}}}`)},
		"listing parent foreign account": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"type":"UNIT","parent_ad_group_criterion":"` +
			foreign + `/adGroupCriteria/31~7"}`)},
		"listing parent other ad group": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"type":"UNIT","parent_ad_group_criterion":"` +
			own + `/adGroupCriteria/99~7"}`)},
		"listing parent temporary": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"type":"UNIT","parent_ad_group_criterion":"` +
			own + `/adGroupCriteria/31~-1"}`)},
		"listing parent kind": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"type":"UNIT","parent_ad_group_criterion":"` +
			own + `/adGroups/31"}`)},
		"listing two dimensions": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"type":"UNIT","case_value":` +
			`{"product_brand":{"value":"x"},"product_item_id":{"value":"y"}}}`)},
		"listing deprecated dimension": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"type":"UNIT","case_value":` +
			`{"product_grouping":{"value":"x"}}}`)},
		"listing rating": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"type":"UNIT","case_value":` +
			`{"hotel_class":{"value":6}}}`)},
		"listing geo form": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"type":"UNIT","case_value":` +
			`{"hotel_city":{"city_criterion":"cities/1"}}}`)},
		"listing path empty": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","listing_group":{"type":"UNIT","path":{"dimensions":[]}}`)},
		"exempt none":        {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"exempt_policy_violation_keys":[]`)},
		"exempt many": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"exempt_policy_violation_keys":[` +
			strings.Repeat(`{"policy_name":"P"},`, 20) + `{"policy_name":"P"}]`)},
		"exempt no name":      {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"exempt_policy_violation_keys":[{"violating_text":"x"}]`)},
		"exempt member":       {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"exempt_policy_violation_keys":[{"policy_name":"P","x":"y"}]`)},
		"exempt empty":        {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"exempt_policy_violation_keys":[{"policy_name":""}]`)},
		"exempt control":      {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"exempt_policy_violation_keys":[{"policy_name":"a\nb"}]`)},
		"exempt type":         {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"exempt_policy_violation_keys":"P"`)},
		"update exempt alone": {adGroupCriteriaUpdateID, adGroupCriterionUpdateArgs(`,"exempt_policy_violation_keys":[{"policy_name":"P"}]`)},
		"update exempt bad":   {adGroupCriteriaUpdateID, adGroupCriterionUpdateArgs(`,"status":"PAUSED","exempt_policy_violation_keys":[{"policy_name":7}]`)},
		"device":              {adGroupCriteriaCreateID, item(`,"status":"PAUSED","device":{"type":"MOBILE"}`)},
		"http final url":      {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"final_urls":["http://x.example.com/"]`)},
		"final url info": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"final_urls":["https://u:p` + at +
			`x.example.com/"]`)},
		"empty final url": {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"final_urls":[""]`)},
		"http template": {adGroupCriteriaCreateID,
			item(`,"status":"PAUSED","gender":{"type":"MALE"},"tracking_url_template":"http://t.example.com/?u={lpurl}"`)},
		"user info template": {adGroupCriteriaCreateID,
			item(`,"status":"PAUSED","gender":{"type":"MALE"},"tracking_url_template":"https://user:pw` + at + `t.example.com/?u={lpurl}"`)},
		"suffix address":    {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"final_url_suffix":"https://x.example.com/"`)},
		"bid below zero":    {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"cpc_bid_micros":-1`)},
		"fraction bid":      {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"cpc_bid_micros":1.5`)},
		"percent too high":  {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"percent_cpc_bid_micros":1000000`)},
		"modifier low":      {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"bid_modifier":0.05`)},
		"modifier high":     {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"bid_modifier":10.5`)},
		"negative paused":   {adGroupCriteriaCreateID, item(`,"negative":true,"status":"PAUSED","keyword":{"text":"x","match_type":"EXACT"}`)},
		"negative bid":      {adGroupCriteriaCreateID, item(`,"negative":true,"keyword":{"text":"x","match_type":"EXACT"},"cpc_bid_micros":1`)},
		"negative modifier": {adGroupCriteriaCreateID, item(`,"negative":true,"keyword":{"text":"x","match_type":"EXACT"},"bid_modifier":2`)},
		"negative url":      {adGroupCriteriaCreateID, item(`,"negative":true,"keyword":{"text":"x","match_type":"EXACT"},"final_urls":["https://x.example.com/"]`)},
		"negative text":     {adGroupCriteriaCreateID, item(`,"negative":"yes","keyword":{"text":"x","match_type":"EXACT"}`)},
		"output only":       {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"approval_status":"APPROVED"`)},
		"labels":            {adGroupCriteriaCreateID, item(`,"status":"PAUSED","gender":{"type":"MALE"},"labels":["` + own + `/labels/1"]`)},
		"content type":      {adGroupCriteriaCreateID, adGroupCriteriaCall(`[`+createdCriterion+`}]`, `,"response_content_type":"ALL"`)},

		"update no member": {adGroupCriteriaUpdateID, adGroupCriterionUpdateArgs(``)},
		"update ad group":  {adGroupCriteriaUpdateID, adGroupCriterionUpdateArgs(`,"ad_group":"` + own + `/adGroups/32"`)},
		"update negative":  {adGroupCriteriaUpdateID, adGroupCriterionUpdateArgs(`,"negative":true`)},
		"update keyword":   {adGroupCriteriaUpdateID, adGroupCriterionUpdateArgs(`,"keyword":{"text":"y","match_type":"EXACT"}`)},
		"update removed":   {adGroupCriteriaUpdateID, adGroupCriterionUpdateArgs(`,"status":"REMOVED"`)},
		"update foreign":   {adGroupCriteriaUpdateID, adGroupCriteriaCall(`[{"resource_name":"`+foreign+`/adGroupCriteria/31~41","status":"PAUSED"}]`, "")},
		"update temporary": {adGroupCriteriaUpdateID, adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~-2","status":"PAUSED"}]`, "")},
		"update no tilde":  {adGroupCriteriaUpdateID, adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/41","status":"PAUSED"}]`, "")},
		"update kind":      {adGroupCriteriaUpdateID, adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroups/31","status":"PAUSED"}]`, "")},
		"update twice": {adGroupCriteriaUpdateID, adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~41","status":"PAUSED"},`+
			`{"resource_name":"`+own+`/adGroupCriteria/31~41","status":"ENABLED"}]`, "")},
		"update template": {adGroupCriteriaUpdateID, adGroupCriterionUpdateArgs(`,"tracking_url_template":"https://evil.example` + at + `t.example.com/"`)},
		"update final":    {adGroupCriteriaUpdateID, adGroupCriterionUpdateArgs(`,"final_urls":["http://x.example.com/"]`)},

		"remove foreign":   {adGroupCriteriaRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + foreign + `/adGroupCriteria/31~41"]}`},
		"remove temporary": {adGroupCriteriaRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroupCriteria/31~-41"]}`},
		"remove zero":      {adGroupCriteriaRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroupCriteria/0~41"]}`},
		"remove twice": {adGroupCriteriaRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroupCriteria/31~41","` +
			own + `/adGroupCriteria/31~41"]}`},
		"remove none":    {adGroupCriteriaRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
		"remove content": {adGroupCriteriaRemoveID, adGroupCriterionRemoveArgs(`,"response_content_type":"MUTABLE_RESOURCE"`)},
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

func TestAdGroupCriteriaCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = criterionResults(own+"/adGroupCriteria/31~41", own+"/adGroupCriteria/31~42",
		own+"/adGroupCriteria/31~43")
	out, err := e.invokeConfirmed(adGroupCriteriaCreateID, adGroupCriteriaCall(`[{`+criterionAdGroup+`,"status":"PAUSED",`+
		`"keyword":{"text":"running shoes","match_type":"PHRASE"},"cpc_bid_micros":750000,"bid_modifier":1.5,`+
		`"final_urls":["https://shop.example.com/shoes"],"tracking_url_template":"https://t.example.com/?u={lpurl}",`+
		`"final_url_suffix":"utm_source=g","url_custom_parameters":[{"key":"season","value":"spring"}]},`+
		`{`+criterionAdGroup+`,"negative":true,"keyword":{"text":"free","match_type":"BROAD"}},`+
		`{`+criterionAdGroup+`,"status":"ENABLED","user_list":{"user_list":"`+own+`/userLists/5"},"percent_cpc_bid_micros":999999}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 3 ||
		results[0].(map[string]any)["resource_name"] != own+"/adGroupCriteria/31~41" ||
		results[0].(map[string]any)["ad_group_criterion"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+adGroupCriteriaPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	want := wantWire(t, `[{"create":{"adGroup":"`+own+`/adGroups/31","status":"PAUSED","keyword":{"text":"running shoes",`+
		`"matchType":"PHRASE"},"cpcBidMicros":750000,"bidModifier":1.5,"finalUrls":["https://shop.example.com/shoes"],`+
		`"trackingUrlTemplate":"https://t.example.com/?u={lpurl}","finalUrlSuffix":"utm_source=g",`+
		`"urlCustomParameters":[{"key":"season","value":"spring"}]}},`+
		`{"create":{"adGroup":"`+own+`/adGroups/31","negative":true,"keyword":{"text":"free","matchType":"BROAD"}}},`+
		`{"create":{"adGroup":"`+own+`/adGroups/31","status":"ENABLED","userList":{"userList":"`+own+`/userLists/5"},`+
		`"percentCpcBidMicros":999999}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

// The constants of Google belong to no account and pass by their form.
func TestAdGroupCriteriaCreateSendsTheOtherCriterionTypes(t *testing.T) {
	cases := map[string][2]string{
		"location": {`"location":{"geo_target_constant":"geoTargetConstants/2840"}`,
			`"location":{"geoTargetConstant":"geoTargetConstants/2840"}`},
		"language": {`"language":{"language_constant":"languageConstants/1000"}`,
			`"language":{"languageConstant":"languageConstants/1000"}`},
		"topic": {`"topic":{"topic_constant":"topicConstants/3"}`, `"topic":{"topicConstant":"topicConstants/3"}`},
		"category": {`"mobile_app_category":{"mobile_app_category_constant":"mobileAppCategoryConstants/60000"}`,
			`"mobileAppCategory":{"mobileAppCategoryConstant":"mobileAppCategoryConstants/60000"}`},
		"app": {`"mobile_application":{"app_id":"2-com.example.app","name":"App"}`,
			`"mobileApplication":{"appId":"2-com.example.app","name":"App"}`},
		"placement": {`"placement":{"url":"www.example.com"}`, `"placement":{"url":"www.example.com"}`},
		"video":     {`"youtube_video":{"video_id":"dQw4w9WgXcQ"}`, `"youtubeVideo":{"videoId":"dQw4w9WgXcQ"}`},
		"channel":   {`"youtube_channel":{"channel_id":"UCabc_123"}`, `"youtubeChannel":{"channelId":"UCabc_123"}`},
		"webpage": {`"webpage":{"criterion_name":"sale","conditions":[{"operand":"URL","operator":"CONTAINS","argument":"/sale"}]}`,
			`"webpage":{"criterionName":"sale","conditions":[{"operand":"URL","operator":"CONTAINS","argument":"/sale"}]}`},
		"affinity": {`"custom_affinity":{"custom_affinity":"` + own + `/customInterests/5"}`,
			`"customAffinity":{"customAffinity":"` + own + `/customInterests/5"}`},
		"audience":   {`"audience":{"audience":"` + own + `/audiences/5"}`, `"audience":{"audience":"` + own + `/audiences/5"}`},
		"life event": {`"life_event":{"life_event_id":7}`, `"lifeEvent":{"lifeEventId":7}`},
		"age":        {`"age_range":{"type":"AGE_RANGE_18_24"}`, `"ageRange":{"type":"AGE_RANGE_18_24"}`},
		"brand list": {`"brand_list":{"shared_set":"` + own + `/sharedSets/5"}`, `"brandList":{"sharedSet":"` + own + `/sharedSets/5"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = criterionResults(own + "/adGroupCriteria/31~41")
			_, err := e.invokeConfirmed(adGroupCriteriaCreateID, adGroupCriteriaCall(`[{`+criterionAdGroup+`,"status":"PAUSED",`+c[0]+`}]`, ""))
			if err != nil {
				t.Fatal(err)
			}
			ops := bodyOf(t, e.posts()[0])["operations"]
			if !reflect.DeepEqual(ops, wantWire(t, `[{"create":{"adGroup":"`+own+`/adGroups/31","status":"PAUSED",`+c[1]+`}}]`)) {
				t.Errorf("operations = %s", e.posts()[0].body)
			}
		})
	}
}

func TestAdGroupCriteriaUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = criterionResults(own+"/adGroupCriteria/31~41", own+"/adGroupCriteria/31~42")
	_, err := e.invokeConfirmed(adGroupCriteriaUpdateID, adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~41","status":"ENABLED",`+
		`"cpc_bid_micros":900000,"bid_modifier":2,"final_urls":["https://a.example.com/"],`+
		`"url_custom_parameters":[{"key":"a","value":"b"}]},`+
		`{"resource_name":"`+own+`/adGroupCriteria/31~42","tracking_url_template":"","final_url_suffix":"x=1","final_urls":[]}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, second := ops[0].(map[string]any), ops[1].(map[string]any)
	if first["updateMask"] != "bidModifier,cpcBidMicros,finalUrls,status,urlCustomParameters" {
		t.Errorf("first mask = %v", first["updateMask"])
	}
	if !reflect.DeepEqual(first["update"], wantWire(t, `{"resourceName":"`+own+`/adGroupCriteria/31~41","status":"ENABLED",`+
		`"cpcBidMicros":900000,"bidModifier":2,"finalUrls":["https://a.example.com/"],`+
		`"urlCustomParameters":[{"key":"a","value":"b"}]}`)) {
		t.Errorf("first update = %v", first["update"])
	}
	// A text that is cleared is in the mask and has no value; an empty list clears a list.
	if second["updateMask"] != "finalUrlSuffix,finalUrls,trackingUrlTemplate" ||
		!reflect.DeepEqual(second["update"], map[string]any{"resourceName": own + "/adGroupCriteria/31~42", "finalUrlSuffix": "x=1",
			"finalUrls": []any{}}) {
		t.Errorf("second operation = %v", second)
	}
}

func TestAdGroupCriteriaRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = criterionResults(own+"/adGroupCriteria/31~41", own+"/adGroupCriteria/32~42")
	out, err := e.invokeConfirmed(adGroupCriteriaRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+own+
		`/adGroupCriteria/31~41","`+own+`/adGroupCriteria/32~42"]}`)
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+own+`/adGroupCriteria/31~41"},{"remove":"`+own+
		`/adGroupCriteria/32~42"}]`)) || len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestAdGroupCriterionValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		adGroupCriteriaCreateID: adGroupCriteriaCall(`[`+createdCriterion+`}]`, `,"validate_only":true`),
		adGroupCriteriaUpdateID: adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~41","status":"PAUSED"}]`, `,"validate_only":true`),
		adGroupCriteriaRemoveID: adGroupCriterionRemoveArgs(`,"validate_only":true`),
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = okReply(answer)
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

func TestAdGroupCriterionResponseContentTypeShowsTheMutableCriterion(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = okReply(`{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~41",` +
		`"adGroupCriterion":{"resourceName":"` + own + `/adGroupCriteria/31~41","adGroup":"` + own + `/adGroups/31","status":"PAUSED",` +
		`"keyword":{"text":"shoes","matchType":"EXACT"},"cpcBidMicros":"750000","bidModifier":1.5,"finalUrls":["https://a.example.com/"],` +
		`"urlCustomParameters":[{"key":"a","value":"b"}],"criterionId":"41","type":"KEYWORD","approvalStatus":"APPROVED",` +
		`"labels":["` + own + `/labels/1"],"unknownMember":"never held"}}]}`)
	out, err := e.invokeConfirmed(adGroupCriteriaUpdateID, adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~41","status":"PAUSED"}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)["results"].([]any)[0].(map[string]any)["ad_group_criterion"].(map[string]any)
	want := map[string]any{"resource_name": own + "/adGroupCriteria/31~41", "ad_group": own + "/adGroups/31", "status": "PAUSED",
		"keyword": map[string]any{"text": "shoes", "match_type": "EXACT"}, "cpc_bid_micros": float64(750000), "bid_modifier": 1.5,
		"final_urls":            []any{"https://a.example.com/"},
		"url_custom_parameters": []any{map[string]any{"key": "a", "value": "b"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("criterion = %v", got)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that names another account, another criterion, another ad group, or another number of results than
// asked for never passes. The change was made as far as Qatlas can tell, so the error says that the outcome is unknown.
func TestAdGroupCriterionAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	update := adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~41","status":"PAUSED"}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	create := adGroupCriteriaCall(`[`+createdCriterion+`}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":     {adGroupCriteriaCreateID, create, `{"results":[{"resourceName":"` + foreign + `/adGroupCriteria/31~41"}]}`},
		"other ad group":     {adGroupCriteriaCreateID, create, `{"results":[{"resourceName":"` + own + `/adGroupCriteria/99~41"}]}`},
		"other criterion":    {adGroupCriteriaUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~99"}]}`},
		"ad group as result": {adGroupCriteriaCreateID, create, `{"results":[{"resourceName":"` + own + `/adGroups/31"}]}`},
		"no name":            {adGroupCriteriaUpdateID, update, `{"results":[{}]}`},
		"no results":         {adGroupCriteriaUpdateID, update, `{}`},
		"too many": {adGroupCriteriaCreateID, create, `{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~1"},` +
			`{"resourceName":"` + own + `/adGroupCriteria/31~2"}]}`},
		"partial failure": {adGroupCriteriaUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~41"}],"partialFailureError":{"code":3}}`},
		"foreign ad group inside": {adGroupCriteriaUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~41","adGroupCriterion":{"resourceName":"` +
			own + `/adGroupCriteria/31~41","adGroup":"` + foreign + `/adGroups/31"}}]}`},
		"foreign user list inside": {adGroupCriteriaUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~41","adGroupCriterion":{"resourceName":"` +
			own + `/adGroupCriteria/31~41","userList":{"userList":"` + foreign + `/userLists/5"}}}]}`},
		"other criterion inside": {adGroupCriteriaUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~41","adGroupCriterion":{"resourceName":"` +
			own + `/adGroupCriteria/31~42"}}]}`},
		"member of the wrong kind": {adGroupCriteriaUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~41","adGroupCriterion":{"resourceName":"` +
			own + `/adGroupCriteria/31~41","keyword":"x"}}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAdGroupCriterionFailuresMapWithoutProviderText(t *testing.T) {
	arguments := adGroupCriteriaCall(`[`+createdCriterion+`}]`, "")
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "adGroupCriterionError", "CANNOT_ADD_CRITERIA_TYPE", ""), "",
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
			e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(adGroupCriteriaCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(adGroupCriteriaCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A manager account has no ad group criteria: a customer target that names one is refused before the change.
func TestAdGroupCriterionChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(adGroupCriteriaRemoveID, `{"customer_id":"`+managerID+`","resource_names":["customers/`+
		queryNumber(managerID)+`/adGroupCriteria/31~41"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}

func TestAdGroupCriteriaCreateSendsAListingGroupUnderAnExistingSubdivision(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = criterionResults(own+"/adGroupCriteria/31~41", own+"/adGroupCriteria/31~42")
	_, err := e.invokeConfirmed(adGroupCriteriaCreateID, adGroupCriteriaCall(`[{`+criterionAdGroup+`,"status":"ENABLED","cpc_bid_micros":500000,`+
		`"listing_group":{"type":"UNIT","parent_ad_group_criterion":"`+own+`/adGroupCriteria/31~7","case_value":`+
		`{"product_category":{"category_id":166,"level":"LEVEL1"}},"path":{"dimensions":[{"product_brand":{"value":"Acme"}},`+
		`{"unknown_listing_dimension":{}}]}}},`+
		`{`+criterionAdGroup+`,"status":"PAUSED","listing_group":{"type":"SUBDIVISION","case_value":{"hotel_city":`+
		`{"city_criterion":"geoTargetConstants/1023191"}}}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := wantWire(t, `[{"create":{"adGroup":"`+own+`/adGroups/31","status":"ENABLED","cpcBidMicros":500000,"listingGroup":{"type":"UNIT",`+
		`"parentAdGroupCriterion":"`+own+`/adGroupCriteria/31~7","caseValue":{"productCategory":{"categoryId":166,"level":"LEVEL1"}},`+
		`"path":{"dimensions":[{"productBrand":{"value":"Acme"}},{"unknownListingDimension":{}}]}}}},`+
		`{"create":{"adGroup":"`+own+`/adGroups/31","status":"PAUSED","listingGroup":{"type":"SUBDIVISION","caseValue":{"hotelCity":`+
		`{"cityCriterion":"geoTargetConstants/1023191"}}}}}]`)
	if !reflect.DeepEqual(bodyOf(t, e.posts()[0])["operations"], want) {
		t.Errorf("operations = %s", e.posts()[0].body)
	}
}

func TestAdGroupCriteriaSendPolicyExemptionKeysAndConfirmThem(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = criterionResults(own + "/adGroupCriteria/31~41")
	keys := `"exempt_policy_violation_keys":[{"policy_name":"TRADEMARKS_IN_AD_TEXT","violating_text":"Acme"},{"policy_name":"OTHER"}]`
	args := adGroupCriteriaCall(`[{`+criterionAdGroup+`,"status":"PAUSED","keyword":{"text":"acme","match_type":"EXACT"},`+keys+`}]`, "")
	var needed *application.ConfirmationRequiredError
	if _, err := e.invoke(adGroupCriteriaCreateID, args); !errors.As(err, &needed) ||
		!strings.Contains(needed.Summary, "POLICY EXEMPTION") || !strings.Contains(needed.Summary, "TRADEMARKS_IN_AD_TEXT") {
		t.Fatalf("summary = %v", err)
	}
	if _, err := e.invokeConfirmed(adGroupCriteriaCreateID, args); err != nil {
		t.Fatal(err)
	}
	e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = criterionResults(own + "/adGroupCriteria/31~42")
	if _, err := e.invokeConfirmed(adGroupCriteriaUpdateID, adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~42","status":"ENABLED",`+
		keys+`}]`, "")); err != nil {
		t.Fatal(err)
	}
	wantKeys := wantWire(t, `[{"policyName":"TRADEMARKS_IN_AD_TEXT","violatingText":"Acme"},{"policyName":"OTHER"}]`)
	posts := e.posts()
	create := bodyOf(t, posts[0])["operations"].([]any)[0].(map[string]any)
	update := bodyOf(t, posts[1])["operations"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(create["exemptPolicyViolationKeys"], wantKeys) || !reflect.DeepEqual(update["exemptPolicyViolationKeys"], wantKeys) ||
		strings.Contains(string(mustJSON(t, create["create"])), "exempt") || update["updateMask"] != "status" {
		t.Errorf("create = %v, update = %v", create, update)
	}
}

func TestAdGroupCriterionAnswerWithAForeignListingParentNeverPasses(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupCriteriaPath(clientID)] = okReply(`{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~41",` +
		`"adGroupCriterion":{"resourceName":"` + own + `/adGroupCriteria/31~41","listingGroup":{"type":"UNIT",` +
		`"parentAdGroupCriterion":"` + foreign + `/adGroupCriteria/31~7"}}}]}`)
	_, err := e.invokeConfirmed(adGroupCriteriaUpdateID, adGroupCriteriaCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~41","cpc_bid_micros":1}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`))
	if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") {
		t.Fatalf("err = %v", err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
