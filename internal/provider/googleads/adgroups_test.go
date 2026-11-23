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

var adGroupToolIDs = []string{adGroupsCreateID, adGroupsUpdateID, adGroupsRemoveID}

func adGroupsPath(id string) string { return "/v26/customers/" + id + "/adGroups:mutate" }

var adGroupCampaign = `"campaign":"` + own + `/campaigns/21"`

var createdAdGroup = `{"name":"Shoes","status":"PAUSED",` + adGroupCampaign

func adGroupsCall(items, extra string) string {
	return `{"customer_id":"` + clientID + `","ad_groups":` + items + extra + `}`
}

func adGroupCreateArgs(members string) string {
	return adGroupsCall(`[`+createdAdGroup+members+`}]`, "")
}

func adGroupUpdateArgs(members string) string {
	return adGroupsCall(`[{"resource_name":"`+own+`/adGroups/31"`+members+`}]`, "")
}

func adGroupRemoveArgs(extra string) string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroups/31"]` + extra + `}`
}

func adGroupResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

func TestAdGroupToolsAreListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range adGroupToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Fatalf("%s = %+v", id, d)
		}
		schema := string(d.InputSchema)
		for _, absent := range []string{"login_customer_id", "partial_failure", "fixed_cpm_micros", "base_ad_group",
			"effective_cpc_bid_micros", "primary_status", "labels", "SHOPPING_SMART_ADS",
			"google_ads_conversion_customer"} {
			if strings.Contains(schema, `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	update, _, _ := reg.Lookup(adGroupsUpdateID)
	if strings.Contains(string(update.InputSchema), `"campaign"`) || strings.Contains(string(update.InputSchema), `"SEARCH_STANDARD"`) ||
		strings.Contains(string(update.InputSchema), `"audience_setting"`) {
		t.Error("ad-groups.update offers a member that only creation accepts")
	}
	create, _, _ := reg.Lookup(adGroupsCreateID)
	if !strings.Contains(string(create.InputSchema), `"campaign"`) || !strings.Contains(string(create.InputSchema), `"url_custom_parameters"`) ||
		!strings.Contains(string(create.InputSchema), `"SEARCH_STANDARD"`) {
		t.Error("ad-groups.create lacks a member of the resource")
	}
	remove, _, _ := reg.Lookup(adGroupsRemoveID)
	if strings.Contains(string(remove.InputSchema), "response_content_type") {
		t.Error("ad-groups.remove offers a response content type")
	}
}

// Every scalar member that is not part of a repeated message is a field of the catalog of v26.
func TestAdGroupMembersAreFieldsOfTheCatalog(t *testing.T) {
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
	walk(adGroupTree, "ad_group")
}

func TestAdGroupChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		adGroupsCreateID: adGroupCreateArgs(``),
		adGroupsUpdateID: adGroupUpdateArgs(`,"status":"ENABLED"`),
		adGroupsRemoveID: adGroupRemoveArgs(""),
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

func TestAdGroupSummaryNamesTheSpendingEffect(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(adGroupsCreateID, adGroupsCall(`[{"name":"Shoes","status":"ENABLED",`+adGroupCampaign+
		`,"type":"SEARCH_STANDARD","cpc_bid_micros":750000}]`, `,"validate_only":true`))
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "Shoes", "ENABLED", "SEARCH_STANDARD", "campaigns/21", "cpc_bid_micros", "bids"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(adGroupsRemoveID, adGroupRemoveArgs(""))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "cannot be enabled again") {
		t.Errorf("summary = %v", err)
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestAdGroupArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	// at keeps the user-info samples from reading as mail addresses.
	const at = "@"
	bad := map[string]struct{ id, arguments string }{
		"no status":      {adGroupsCreateID, adGroupsCall(`[{"name":"x",`+adGroupCampaign+`}]`, "")},
		"no name":        {adGroupsCreateID, adGroupsCall(`[{"status":"PAUSED",`+adGroupCampaign+`}]`, "")},
		"no campaign":    {adGroupsCreateID, adGroupsCall(`[{"name":"x","status":"PAUSED"}]`, "")},
		"removed":        {adGroupsCreateID, adGroupCreateArgs(`,"status":"REMOVED"`)},
		"none":           {adGroupsCreateID, adGroupsCall(`[]`, "")},
		"eleven":         {adGroupsCreateID, adGroupsCall(`[`+strings.Repeat(createdAdGroup+`},`, 10)+createdAdGroup+`}]`, "")},
		"empty name":     {adGroupsCreateID, adGroupsCall(`[{"name":"","status":"PAUSED",`+adGroupCampaign+`}]`, "")},
		"line feed":      {adGroupsCreateID, adGroupsCall(`[{"name":"a\nb","status":"PAUSED",`+adGroupCampaign+`}]`, "")},
		"long name":      {adGroupsCreateID, adGroupsCall(`[{"name":"`+strings.Repeat("a", 255)+`","status":"PAUSED",`+adGroupCampaign+`}]`, "")},
		"wide name":      {adGroupsCreateID, adGroupsCall(`[{"name":"`+strings.Repeat("あ", 128)+`","status":"PAUSED",`+adGroupCampaign+`}]`, "")},
		"unknown type":   {adGroupsCreateID, adGroupCreateArgs(`,"type":"UNKNOWN"`)},
		"smart shopping": {adGroupsCreateID, adGroupCreateArgs(`,"type":"SHOPPING_SMART_ADS"`)},
		"http template": {adGroupsCreateID,
			adGroupCreateArgs(`,"tracking_url_template":"http://t.example.com/?u={lpurl}"`)},
		"user info": {adGroupsCreateID,
			adGroupCreateArgs(`,"tracking_url_template":"https://user:pw` + at + `t.example.com/?u={lpurl}"`)},
		"suffix address":   {adGroupsCreateID, adGroupCreateArgs(`,"final_url_suffix":"https://x.example.com/"`)},
		"foreign campaign": {adGroupsCreateID, adGroupsCall(`[{"name":"x","status":"PAUSED","campaign":"`+foreign+`/campaigns/21"}]`, "")},
		"temp campaign":    {adGroupsCreateID, adGroupsCall(`[{"name":"x","status":"PAUSED","campaign":"`+own+`/campaigns/-1"}]`, "")},
		"campaign kind":    {adGroupsCreateID, adGroupsCall(`[{"name":"x","status":"PAUSED","campaign":"`+own+`/adGroups/21"}]`, "")},
		"negative bid":     {adGroupsCreateID, adGroupCreateArgs(`,"cpc_bid_micros":-1`)},
		"fraction bid":     {adGroupsCreateID, adGroupCreateArgs(`,"cpc_bid_micros":1.5`)},
		"percent too high": {adGroupsCreateID, adGroupCreateArgs(`,"percent_cpc_bid_micros":1000000`)},
		"bad rotation":     {adGroupsCreateID, adGroupCreateArgs(`,"ad_rotation_mode":"SOMETIMES"`)},
		"bad asset set":    {adGroupsCreateID, adGroupCreateArgs(`,"excluded_parent_asset_set_types":["PAGE_FEED"]`)},
		"output only":      {adGroupsCreateID, adGroupCreateArgs(`,"primary_status":"ELIGIBLE"`)},
		"labels":           {adGroupsCreateID, adGroupCreateArgs(`,"labels":["` + own + `/labels/1"]`)},
		"reservation":      {adGroupsCreateID, adGroupCreateArgs(`,"fixed_cpm_micros":1000`)},
		"empty message":    {adGroupsCreateID, adGroupCreateArgs(`,"ai_max_ad_group_setting":{}`)},
		"message member":   {adGroupsCreateID, adGroupCreateArgs(`,"ai_max_ad_group_setting":{"x":true}`)},
		"content type":     {adGroupsCreateID, adGroupsCall(`[`+createdAdGroup+`}]`, `,"response_content_type":"ALL"`)},

		"update no member": {adGroupsUpdateID, adGroupUpdateArgs(``)},
		"update campaign":  {adGroupsUpdateID, adGroupUpdateArgs(`,"campaign":"` + own + `/campaigns/22"`)},
		"update type":      {adGroupsUpdateID, adGroupUpdateArgs(`,"type":"DISPLAY_STANDARD"`)},
		"update audience":  {adGroupsUpdateID, adGroupUpdateArgs(`,"audience_setting":{"use_audience_grouped":true}`)},
		"update removed":   {adGroupsUpdateID, adGroupUpdateArgs(`,"status":"REMOVED"`)},
		"update foreign":   {adGroupsUpdateID, adGroupsCall(`[{"resource_name":"`+foreign+`/adGroups/31","status":"PAUSED"}]`, "")},
		"update temporary": {adGroupsUpdateID, adGroupsCall(`[{"resource_name":"`+own+`/adGroups/-2","status":"PAUSED"}]`, "")},
		"update kind":      {adGroupsUpdateID, adGroupsCall(`[{"resource_name":"`+own+`/campaigns/31","status":"PAUSED"}]`, "")},
		"update twice": {adGroupsUpdateID, adGroupsCall(`[{"resource_name":"`+own+`/adGroups/31","status":"PAUSED"},`+
			`{"resource_name":"`+own+`/adGroups/31","status":"ENABLED"}]`, "")},
		"update template": {adGroupsUpdateID, adGroupUpdateArgs(`,"tracking_url_template":"https://evil.example` + at + `t.example.com/"`)},

		"remove foreign":   {adGroupsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + foreign + `/adGroups/31"]}`},
		"remove temporary": {adGroupsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroups/-31"]}`},
		"remove twice": {adGroupsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroups/31","` +
			own + `/adGroups/31"]}`},
		"remove none":    {adGroupsRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
		"remove content": {adGroupsRemoveID, adGroupRemoveArgs(`,"response_content_type":"MUTABLE_RESOURCE"`)},
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

func TestAdGroupsCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+adGroupsPath(clientID)] = adGroupResults(own+"/adGroups/31", own+"/adGroups/32")
	out, err := e.invokeConfirmed(adGroupsCreateID, adGroupsCall(`[{"name":"Shoes","status":"PAUSED",`+adGroupCampaign+
		`,"type":"SEARCH_STANDARD","ad_rotation_mode":"OPTIMIZE","cpc_bid_micros":750000,"percent_cpc_bid_micros":999999,`+
		`"target_roas":3.5,"optimized_targeting_enabled":true,"tracking_url_template":"https://t.example.com/?u={lpurl}",`+
		`"final_url_suffix":"utm_source=g","url_custom_parameters":[{"key":"season","value":"spring"}],`+
		`"targeting_setting":{"target_restrictions":[{"targeting_dimension":"AUDIENCE","bid_only":true}]},`+
		`"audience_setting":{"use_audience_grouped":true},"ai_max_ad_group_setting":{"disable_search_term_matching":true}},`+
		`{"name":"Demand","status":"ENABLED",`+adGroupCampaign+`,"type":"DISPLAY_STANDARD","demand_gen_ad_group_settings":`+
		`{"channel_controls":{"channel_config":"SELECTED_CHANNELS","selected_channels":{"youtube_shorts":true,"gmail":false}}}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
		results[0].(map[string]any)["resource_name"] != own+"/adGroups/31" ||
		results[1].(map[string]any)["resource_name"] != own+"/adGroups/32" || results[0].(map[string]any)["ad_group"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+adGroupsPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	want := wantWire(t, `[{"create":{"name":"Shoes","status":"PAUSED","campaign":"`+own+`/campaigns/21","type":"SEARCH_STANDARD",`+
		`"adRotationMode":"OPTIMIZE","cpcBidMicros":750000,"percentCpcBidMicros":999999,"targetRoas":3.5,`+
		`"optimizedTargetingEnabled":true,"trackingUrlTemplate":"https://t.example.com/?u={lpurl}","finalUrlSuffix":"utm_source=g",`+
		`"urlCustomParameters":[{"key":"season","value":"spring"}],"targetingSetting":{"targetRestrictions":[`+
		`{"targetingDimension":"AUDIENCE","bidOnly":true}]},"audienceSetting":{"useAudienceGrouped":true},`+
		`"aiMaxAdGroupSetting":{"disableSearchTermMatching":true}}},{"create":{"name":"Demand","status":"ENABLED","campaign":"`+
		own+`/campaigns/21","type":"DISPLAY_STANDARD","demandGenAdGroupSettings":{"channelControls":{"channelConfig":`+
		`"SELECTED_CHANNELS","selectedChannels":{"youtubeShorts":true,"gmail":false}}}}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

func TestAdGroupsUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupsPath(clientID)] = adGroupResults(own+"/adGroups/31", own+"/adGroups/32")
	_, err := e.invokeConfirmed(adGroupsUpdateID, adGroupsCall(`[{"resource_name":"`+own+`/adGroups/31","status":"ENABLED",`+
		`"vertical_ads_format_setting":{"enable_booking_links":true},"cpc_bid_micros":900000,`+
		`"url_custom_parameters":[{"key":"a","value":"b"}]},`+
		`{"resource_name":"`+own+`/adGroups/32","tracking_url_template":"","final_url_suffix":"x=1"}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, second := ops[0].(map[string]any), ops[1].(map[string]any)
	if first["updateMask"] != "cpcBidMicros,status,urlCustomParameters,verticalAdsFormatSetting.enableBookingLinks" {
		t.Errorf("first mask = %v", first["updateMask"])
	}
	if !reflect.DeepEqual(first["update"], wantWire(t, `{"resourceName":"`+own+`/adGroups/31","status":"ENABLED",`+
		`"verticalAdsFormatSetting":{"enableBookingLinks":true},"cpcBidMicros":900000,`+
		`"urlCustomParameters":[{"key":"a","value":"b"}]}`)) {
		t.Errorf("first update = %v", first["update"])
	}
	// A text that is cleared is in the mask and has no value.
	if second["updateMask"] != "finalUrlSuffix,trackingUrlTemplate" ||
		!reflect.DeepEqual(second["update"], map[string]any{"resourceName": own + "/adGroups/32", "finalUrlSuffix": "x=1"}) {
		t.Errorf("second operation = %v", second)
	}
}

func TestAdGroupsRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupsPath(clientID)] = adGroupResults(own+"/adGroups/31", own+"/adGroups/32")
	out, err := e.invokeConfirmed(adGroupsRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+own+
		`/adGroups/31","`+own+`/adGroups/32"]}`)
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+own+`/adGroups/31"},{"remove":"`+own+
		`/adGroups/32"}]`)) || len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestAdGroupValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		adGroupsCreateID: adGroupsCall(`[`+createdAdGroup+`}]`, `,"validate_only":true`),
		adGroupsUpdateID: adGroupsCall(`[{"resource_name":"`+own+`/adGroups/31","status":"PAUSED"}]`, `,"validate_only":true`),
		adGroupsRemoveID: adGroupRemoveArgs(`,"validate_only":true`),
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+adGroupsPath(clientID)] = okReply(answer)
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

func TestAdGroupResponseContentTypeShowsTheMutableAdGroup(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + own + `/adGroups/31",` +
		`"adGroup":{"resourceName":"` + own + `/adGroups/31","name":"Shoes","status":"PAUSED","campaign":"` + own +
		`/campaigns/21","type":"SEARCH_STANDARD","cpcBidMicros":"750000","fixedCpmMicros":"1000","targetingSetting":` +
		`{"targetRestrictions":[{"targetingDimension":"AUDIENCE","bidOnly":true}]},"urlCustomParameters":` +
		`[{"key":"a","value":"b"}],"id":"31","primaryStatus":"ELIGIBLE","labels":["` + own + `/labels/1"],"unknownMember":"never held"}}]}`)
	out, err := e.invokeConfirmed(adGroupsUpdateID, adGroupsCall(`[{"resource_name":"`+own+`/adGroups/31","status":"PAUSED"}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	adGroup := decode(t, out)["results"].([]any)[0].(map[string]any)["ad_group"].(map[string]any)
	want := map[string]any{"resource_name": own + "/adGroups/31", "name": "Shoes", "status": "PAUSED",
		"campaign": own + "/campaigns/21", "type": "SEARCH_STANDARD", "cpc_bid_micros": float64(750000),
		"fixed_cpm_micros": float64(1000),
		"targeting_setting": map[string]any{"target_restrictions": []any{map[string]any{
			"targeting_dimension": "AUDIENCE", "bid_only": true}}},
		"url_custom_parameters": []any{map[string]any{"key": "a", "value": "b"}}}
	if !reflect.DeepEqual(adGroup, want) {
		t.Errorf("ad group = %v", adGroup)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that names another account, another ad group, or another number of results than asked for never
// passes. The change was made as far as Qatlas can tell, so the error says that the outcome is unknown.
func TestAdGroupAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	update := adGroupsCall(`[{"resource_name":"`+own+`/adGroups/31","status":"PAUSED"}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	create := adGroupsCall(`[`+createdAdGroup+`}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":  {adGroupsCreateID, create, `{"results":[{"resourceName":"` + foreign + `/adGroups/31"}]}`},
		"other ad group":  {adGroupsUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroups/99"}]}`},
		"campaign result": {adGroupsCreateID, create, `{"results":[{"resourceName":"` + own + `/campaigns/31"}]}`},
		"no name":         {adGroupsUpdateID, update, `{"results":[{}]}`},
		"no results":      {adGroupsUpdateID, update, `{}`},
		"too many":        {adGroupsCreateID, create, `{"results":[{"resourceName":"` + own + `/adGroups/1"},{"resourceName":"` + own + `/adGroups/2"}]}`},
		"partial failure": {adGroupsUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroups/31"}],"partialFailureError":{"code":3}}`},
		"foreign campaign": {adGroupsUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroups/31","adGroup":{"resourceName":"` +
			own + `/adGroups/31","campaign":"` + foreign + `/campaigns/1"}}]}`},
		"other ad group inside": {adGroupsUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroups/31","adGroup":{"resourceName":"` +
			own + `/adGroups/32"}}]}`},
		"member of the wrong kind": {adGroupsUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroups/31","adGroup":{"resourceName":"` +
			own + `/adGroups/31","targetingSetting":"x"}}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adGroupsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAdGroupFailuresMapWithoutProviderText(t *testing.T) {
	arguments := adGroupsCall(`[`+createdAdGroup+`}]`, "")
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "adGroupError", "DUPLICATE_ADGROUP_NAME", ""), "",
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
			e.fake.routes["POST "+adGroupsPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(adGroupsCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+adGroupsPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(adGroupsCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A manager account has no ad groups: a customer target that names one is refused before the change.
func TestAdGroupChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(adGroupsRemoveID, `{"customer_id":"`+managerID+`","resource_names":["customers/`+
		queryNumber(managerID)+`/adGroups/31"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
