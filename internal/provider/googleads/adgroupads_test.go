package googleads

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var adGroupAdToolIDs = []string{adGroupAdsCreateID, adGroupAdsUpdateID, adGroupAdsRemoveID, adGroupAdCreatedAssetsRemoveID}

func adGroupAdsPath(id string) string { return "/v26/customers/" + id + "/adGroupAds:mutate" }

func adGroupAdRemovalPath(id, ad string) string {
	return "/v26/customers/" + id + "/adGroupAds/" + ad + ":removeAutomaticallyCreatedAssets"
}

var (
	adGroupAdGroup = `"ad_group":"` + own + `/adGroups/31"`
	searchAd       = `"ad":{"final_urls":["https://www.example.com/shoes"],"responsive_search_ad":{"headlines":` +
		`[{"text":"Shoes","pinned_field":"HEADLINE_1"},{"text":"Free delivery"},{"text":"New season"}],"descriptions":` +
		`[{"text":"Find your pair."},{"text":"Order today."}],"path1":"shoes"}}`
	adName = own + "/adGroupAds/31~41"
)

var createdAdGroupAd = `{` + adGroupAdGroup + `,"status":"PAUSED",` + searchAd

func adGroupAdsCall(items, extra string) string {
	return `{"customer_id":"` + clientID + `","ad_group_ads":` + items + extra + `}`
}

func adGroupAdCreateArgs(members string) string {
	return adGroupAdsCall(`[`+createdAdGroupAd+members+`}]`, "")
}

func adGroupAdUpdateArgs(members string) string {
	return adGroupAdsCall(`[{"resource_name":"`+adName+`"`+members+`}]`, "")
}

func adGroupAdRemoveArgs(extra string) string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + adName + `"]` + extra + `}`
}

func adGroupAdRemovalArgs(ad, assets string) string {
	return `{"customer_id":"` + clientID + `","ad_group_ad":"` + ad + `","assets_with_field_type":` + assets + `}`
}

var removalAssets = `[{"asset":"` + own + `/assets/33","asset_field_type":"HEADLINE"}]`

func adGroupAdResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

func TestAdGroupAdToolsAreListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range adGroupAdToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Fatalf("%s = %+v", id, d)
		}
		schema := string(d.InputSchema)
		for _, absent := range []string{"login_customer_id", "partial_failure", "expanded_text_ad",
			"legacy_responsive_display_ad", "legacy_app_install_ad", "shopping_smart_ad", "call_ad",
			"policy_summary", "ad_strength", "labels", "primary_status", "data",
			"ad_id_to_copy_image_from", "image_url"} {
			if strings.Contains(schema, `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	update, _, _ := reg.Lookup(adGroupAdsUpdateID)
	for _, absent := range []string{`"ad"`, `"ad_group"`, `"responsive_search_ad"`, `"final_urls"`} {
		if strings.Contains(string(update.InputSchema), absent) {
			t.Errorf("ad-group-ads.update offers %s, which Google marks immutable", absent)
		}
	}
	create, _, _ := reg.Lookup(adGroupAdsCreateID)
	for _, present := range []string{`"responsive_search_ad"`, `"final_url_suffix"`, `"policy_validation_parameter"`,
		`"ignorable_policy_topics"`, `"exempt_policy_violation_keys"`, `"demand_gen_carousel_ad"`, `"url_collections"`,
		`"final_app_urls"`, `"display_url"`, `"content_base64"`} {
		if !strings.Contains(string(create.InputSchema), present) {
			t.Errorf("ad-group-ads.create lacks %s", present)
		}
	}
	for _, id := range []string{adGroupAdsRemoveID, adGroupAdCreatedAssetsRemoveID} {
		d, _, _ := reg.Lookup(id)
		if strings.Contains(string(d.InputSchema), "response_content_type") {
			t.Errorf("%s offers a response content type", id)
		}
	}
	if create.LocalFiles != "" || strings.Contains(string(create.InputSchema), `"local_path"`) {
		t.Error("ad-group-ads.create reads no local file")
	}
	removal, _, _ := reg.Lookup(adGroupAdCreatedAssetsRemoveID)
	if strings.Contains(string(removal.InputSchema), "validate_only") {
		t.Error("the removal of automatically created assets offers validate_only, which its service lacks")
	}
}

// Every member of the ad group ad spec outside repeated messages is a field of the catalog of v26 or the parent
// of one: the catalog lists the leaves of a message. It lists the video asset info and the synthetic content info
// of an ad not at all.
func TestAdGroupAdMembersAreFieldsOfTheCatalog(t *testing.T) {
	listed := func(name string) bool {
		for _, f := range catalogFields {
			if f.name == name || strings.HasPrefix(f.name, name+".") {
				return true
			}
		}
		return false
	}
	var walk func(s *assetSpec, path string)
	walk = func(s *assetSpec, path string) {
		for _, f := range s.fields {
			full := path + "." + f.name
			if f.out || f.kind == kBytes || strings.HasSuffix(full, ".ad_video_asset_info") || strings.HasSuffix(full, ".synthetic_content_info") {
				continue
			}
			if !listed(full) && !(f.kind == kObject && len(f.sub.fields) == 0) {
				t.Errorf("%s is not in the catalog", full)
			}
			if f.kind == kObject && listed(full+"."+firstName(f.sub)) {
				walk(f.sub, full)
			}
		}
	}
	walk(adGroupAdSpec(), "ad_group_ad")
}

func firstName(s *assetSpec) string {
	if len(s.fields) == 0 {
		return "-"
	}
	return s.fields[0].name
}

func TestAdGroupAdChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		adGroupAdsCreateID:             adGroupAdCreateArgs(``),
		adGroupAdsUpdateID:             adGroupAdUpdateArgs(`,"status":"ENABLED"`),
		adGroupAdsRemoveID:             adGroupAdRemoveArgs(""),
		adGroupAdCreatedAssetsRemoveID: adGroupAdRemovalArgs(adName, removalAssets),
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

func TestAdGroupAdSummaryNamesTheEffects(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"ENABLED",`+searchAd+
		`,"policy_validation_parameter":{"ignorable_policy_topics":["ALCOHOL"]}}]`, `,"validate_only":true`))
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "adGroups/31", "ENABLED", "responsive_search_ad", "send clicks",
		"policy exemption", "cannot serve"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(adGroupAdsRemoveID, adGroupAdRemoveArgs(""))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "cannot be enabled again") {
		t.Errorf("summary = %v", err)
	}
	_, err = e.invoke(adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(adName, removalAssets))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "HEADLINE") || !strings.Contains(needed.Summary, "31~41") {
		t.Errorf("summary = %v", err)
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestAdGroupAdArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	const at = "@"
	rsa := func(extra string) string { // a search ad with the given members besides the headlines
		return `{` + adGroupAdGroup + `,"status":"PAUSED","ad":{"final_urls":["https://www.example.com/"],` + extra + `}}`
	}
	headlines := `{"text":"a"},{"text":"b"},{"text":"c"}`
	descriptions := `{"text":"d"},{"text":"e"}`
	good := `"responsive_search_ad":{"headlines":[` + headlines + `],"descriptions":[` + descriptions + `]}`
	bad := map[string]struct{ id, arguments string }{
		"no status":   {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,`+searchAd+`}]`, "")},
		"no ad group": {adGroupAdsCreateID, adGroupAdsCall(`[{"status":"PAUSED",`+searchAd+`}]`, "")},
		"no ad":       {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED"}]`, "")},
		"no type":     {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"name":"x"`)+`]`, "")},
		"two types":   {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(good+`,"text_ad":{"headline":"x"}`)+`]`, "")},
		"removed":     {adGroupAdsCreateID, adGroupAdCreateArgs(`,"status":"REMOVED"`)},
		"none":        {adGroupAdsCreateID, adGroupAdsCall(`[]`, "")},
		"eleven":      {adGroupAdsCreateID, adGroupAdsCall(`[`+strings.Repeat(createdAdGroupAd+`},`, 10)+createdAdGroupAd+`}]`, "")},
		"two headlines": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"responsive_search_ad":{"headlines":[{"text":"a"},{"text":"b"}],`+
			`"descriptions":[`+descriptions+`]}`)+`]`, "")},
		"sixteen headlines": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"responsive_search_ad":{"headlines":[`+
			strings.Repeat(`{"text":"a"},`, 15)+`{"text":"b"}],"descriptions":[`+descriptions+`]}`)+`]`, "")},
		"one description": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"responsive_search_ad":{"headlines":[`+headlines+
			`],"descriptions":[{"text":"d"}]}`)+`]`, "")},
		"five descriptions": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"responsive_search_ad":{"headlines":[`+headlines+
			`],"descriptions":[`+strings.Repeat(`{"text":"d"},`, 4)+`{"text":"e"}]}`)+`]`, "")},
		"pin to a sitelink slot": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"responsive_search_ad":{"headlines":[`+
			`{"text":"a","pinned_field":"TEXT_DISCLAIMER"},{"text":"b"},{"text":"c"}],"descriptions":[`+descriptions+`]}`)+`]`, "")},
		"path2 alone": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"responsive_search_ad":{"headlines":[`+headlines+
			`],"descriptions":[`+descriptions+`],"path2":"x"}`)+`]`, "")},
		"no final url": {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{`+good+`}}]`, "")},
		"http final url": {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"final_urls":`+
			`["http://www.example.com/"],`+good+`}}]`, "")},
		"user info": {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"final_urls":`+
			`["https://user:pw`+at+`www.example.com/"],`+good+`}}]`, "")},
		"mobile final url": {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"final_urls":`+
			`["https://www.example.com/"],"final_mobile_urls":["javascript:x"],`+good+`}}]`, "")},
		"http template": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(good+`,"tracking_url_template":"http://t.example.com/?u={lpurl}"`)+`]`, "")},
		"template user info": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(good+`,"tracking_url_template":`+
			`"https://u:p`+at+`t.example.com/?u={lpurl}"`)+`]`, "")},
		"suffix address":    {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(good+`,"final_url_suffix":"https://x.example.com/"`)+`]`, "")},
		"bad parameter key": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(good+`,"url_custom_parameters":[{"key":"a b","value":"c"}]`)+`]`, "")},
		"device":            {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(good+`,"device_preference":"DESKTOP"`)+`]`, "")},
		"foreign ad group":  {adGroupAdsCreateID, adGroupAdsCall(`[{"ad_group":"`+foreign+`/adGroups/31","status":"PAUSED",`+searchAd+`}]`, "")},
		"temp ad group":     {adGroupAdsCreateID, adGroupAdsCall(`[{"ad_group":"`+own+`/adGroups/-1","status":"PAUSED",`+searchAd+`}]`, "")},
		"ad group kind":     {adGroupAdsCreateID, adGroupAdsCall(`[{"ad_group":"`+own+`/campaigns/31","status":"PAUSED",`+searchAd+`}]`, "")},
		"foreign image": {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"final_urls":`+
			`["https://www.example.com/"],"image_ad":{"image_asset":{"asset":"`+foreign+`/assets/5"}}}}]`, "")},
		"temp image": {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"final_urls":`+
			`["https://www.example.com/"],"image_ad":{"image_asset":{"asset":"`+own+`/assets/-5"}}}}]`, "")},
		"image bytes": {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"final_urls":`+
			`["https://www.example.com/"],"image_ad":{"data":"AAAA"}}}]`, "")},
		"image copy": {adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"final_urls":`+
			`["https://www.example.com/"],"image_ad":{"ad_id_to_copy_image_from":5}}}]`, "")},
		"expanded text":  {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"expanded_text_ad":{"headline_part1":"x"}`)+`]`, "")},
		"legacy display": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"legacy_responsive_display_ad":{"short_headline":"x"}`)+`]`, "")},
		"smart shopping": {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"shopping_smart_ad":{}`)+`]`, "")},
		"call only":      {adGroupAdsCreateID, adGroupAdsCall(`[`+rsa(`"call_ad":{"phone_number":"1"}`)+`]`, "")},
		"output only":    {adGroupAdsCreateID, adGroupAdCreateArgs(`,"ad_strength":"GOOD"`)},
		"policy summary": {adGroupAdsCreateID, adGroupAdCreateArgs(`,"policy_summary":{}`)},
		"bad date":       {adGroupAdsCreateID, adGroupAdCreateArgs(`,"start_date_time":"2027-01-01"`)},
		"bad automation": {adGroupAdsCreateID, adGroupAdCreateArgs(`,"ad_group_ad_asset_automation_settings":[{"asset_automation_type":"X"}]`)},
		"content type":   {adGroupAdsCreateID, adGroupAdsCall(`[`+createdAdGroupAd+`}]`, `,"response_content_type":"ALL"`)},
		"policy empty":   {adGroupAdsCreateID, adGroupAdCreateArgs(`,"policy_validation_parameter":{}`)},
		"policy both": {adGroupAdsCreateID, adGroupAdCreateArgs(`,"policy_validation_parameter":{"ignorable_policy_topics":["A"],` +
			`"exempt_policy_violation_keys":[{"policy_name":"B"}]}`)},
		"policy topic":    {adGroupAdsCreateID, adGroupAdCreateArgs(`,"policy_validation_parameter":{"ignorable_policy_topics":["a b"]}`)},
		"policy no topic": {adGroupAdsCreateID, adGroupAdCreateArgs(`,"policy_validation_parameter":{"ignorable_policy_topics":[]}`)},
		"policy key name": {adGroupAdsCreateID, adGroupAdCreateArgs(`,"policy_validation_parameter":{"exempt_policy_violation_keys":[{"policy_name":"a b"}]}`)},
		"policy key member": {adGroupAdsCreateID, adGroupAdCreateArgs(`,"policy_validation_parameter":{"exempt_policy_violation_keys":` +
			`[{"policy_name":"A","extra":"x"}]}`)},
		"policy unknown": {adGroupAdsCreateID, adGroupAdCreateArgs(`,"policy_validation_parameter":{"ignore_all":true}`)},

		"update no member": {adGroupAdsUpdateID, adGroupAdUpdateArgs(``)},
		"update ad":        {adGroupAdsUpdateID, adGroupAdUpdateArgs(`,"ad":{"name":"x"}`)},
		"update ad group":  {adGroupAdsUpdateID, adGroupAdUpdateArgs(`,"ad_group":"` + own + `/adGroups/32"`)},
		"update removed":   {adGroupAdsUpdateID, adGroupAdUpdateArgs(`,"status":"REMOVED"`)},
		"update foreign":   {adGroupAdsUpdateID, adGroupAdsCall(`[{"resource_name":"`+foreign+`/adGroupAds/31~41","status":"PAUSED"}]`, "")},
		"update temporary": {adGroupAdsUpdateID, adGroupAdsCall(`[{"resource_name":"`+own+`/adGroupAds/31~-41","status":"PAUSED"}]`, "")},
		"update kind":      {adGroupAdsUpdateID, adGroupAdsCall(`[{"resource_name":"`+own+`/adGroups/31","status":"PAUSED"}]`, "")},
		"update no ad id":  {adGroupAdsUpdateID, adGroupAdsCall(`[{"resource_name":"`+own+`/adGroupAds/31","status":"PAUSED"}]`, "")},
		"update twice": {adGroupAdsUpdateID, adGroupAdsCall(`[{"resource_name":"`+adName+`","status":"PAUSED"},`+
			`{"resource_name":"`+adName+`","status":"ENABLED"}]`, "")},

		"remove foreign":   {adGroupAdsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + foreign + `/adGroupAds/31~41"]}`},
		"remove temporary": {adGroupAdsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroupAds/-31~41"]}`},
		"remove twice": {adGroupAdsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + adName + `","` +
			adName + `"]}`},
		"remove none":    {adGroupAdsRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
		"remove content": {adGroupAdsRemoveID, adGroupAdRemoveArgs(`,"response_content_type":"MUTABLE_RESOURCE"`)},

		"removal foreign ad":   {adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(foreign+"/adGroupAds/31~41", removalAssets)},
		"removal temporary ad": {adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(own+"/adGroupAds/31~-41", removalAssets)},
		"removal ad kind":      {adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(own+"/adGroups/31", removalAssets)},
		"removal none":         {adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(adName, `[]`)},
		"removal eleven": {adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(adName, `[`+
			strings.Repeat(`{"asset":"`+own+`/assets/33","asset_field_type":"HEADLINE"},`, 10)+
			`{"asset":"`+own+`/assets/33","asset_field_type":"HEADLINE"}]`)},
		"removal foreign asset": {adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(adName,
			`[{"asset":"`+foreign+`/assets/33","asset_field_type":"HEADLINE"}]`)},
		"removal temporary asset": {adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(adName,
			`[{"asset":"`+own+`/assets/-33","asset_field_type":"HEADLINE"}]`)},
		"removal field type": {adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(adName,
			`[{"asset":"`+own+`/assets/33","asset_field_type":"NOT_A_FIELD"}]`)},
		"removal validate only": {adGroupAdCreatedAssetsRemoveID, `{"customer_id":"` + clientID + `","ad_group_ad":"` + adName +
			`","assets_with_field_type":` + removalAssets + `,"validate_only":true}`},
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

// Every ad type that is offered builds a request from the minimum its reference states.
func TestEveryOfferedAdTypeBuildsItsAd(t *testing.T) {
	asset := func(kind string) string { return `{"asset":"` + own + `/assets/` + kind + `"}` }
	text := func(s string) string { return `{"text":"` + s + `"}` }
	cases := map[string]string{
		"responsive_search_ad": `{"headlines":[` + text("a") + `,` + text("b") + `,` + text("c") + `],"descriptions":[` +
			text("d") + `,` + text("e") + `]}`,
		"expanded_dynamic_search_ad":     `{"description":"a","description2":"b"}`,
		"text_ad":                        `{"headline":"a","description1":"b","description2":"c"}`,
		"hotel_ad":                       `{}`,
		"travel_ad":                      `{}`,
		"shopping_product_ad":            `{}`,
		"shopping_comparison_listing_ad": `{"headline":"A headline that is long enough"}`,
		"image_ad":                       `{"image_asset":` + asset("5") + `}`,
		"video_ad": `{"video":` + asset("6") + `,"in_stream":{"action_button_label":"Buy","companion_banner":` + asset("5") +
			`}}`,
		"video_responsive_ad": `{"videos":[` + asset("6") + `],"business_name":` + text("Shoes") + `,"logo_images":[` + asset("7") + `]}`,
		"responsive_display_ad": `{"marketing_images":[` + asset("5") + `],"square_marketing_images":[` + asset("8") +
			`],"headlines":[` + text("a") + `],"long_headline":` + text("long") + `,"descriptions":[` + text("d") +
			`],"main_color":"#112233","accent_color":"#445566","format_setting":"NATIVE"}`,
		"app_ad":                  `{"mandatory_ad_text":` + text("x") + `,"headlines":[` + text("a") + `],"images":[` + asset("5") + `]}`,
		"app_engagement_ad":       `{"headlines":[` + text("a") + `],"videos":[` + asset("6") + `]}`,
		"app_pre_registration_ad": `{"headlines":[` + text("a") + `],"youtube_videos":[` + asset("6") + `]}`,
		"local_ad": `{"headlines":[` + text("a") + `],"descriptions":[` + text("d") + `],"call_to_actions":[` + text("c") +
			`],"marketing_images":[` + asset("5") + `],"logo_images":[` + asset("7") + `],"videos":[` + asset("6") + `]}`,
		"display_upload_ad": `{"display_upload_product_type":"HTML5_UPLOAD_AD","media_bundle":` + asset("9") + `}`,
		"smart_campaign_ad": `{"headlines":[` + text("a") + `,` + text("b") + `,` + text("c") + `],"descriptions":[` +
			text("d") + `,` + text("e") + `]}`,
		"demand_gen_multi_asset_ad": `{"marketing_images":[` + asset("5") + `],"logo_images":[` + asset("7") + `],"business_name":"Shoes"}`,
		"demand_gen_carousel_ad": `{"business_name":"Shoes","logo_image":` + asset("7") + `,"headline":` + text("a") + `,"description":` +
			text("d") + `,"carousel_cards":[` + asset("10") + `,` + asset("11") + `]}`,
		"demand_gen_video_responsive_ad": `{"videos":[` + asset("6") + `],"logo_images":[` + asset("7") + `],"business_name":` +
			text("Shoes") + `,"call_to_actions":[` + asset("12") + `]}`,
		"demand_gen_product_ad": `{"headline":` + text("a") + `,"description":` + text("d") + `,"logo_image":` + asset("7") +
			`,"business_name":` + text("Shoes") + `,"call_to_action":` + asset("12") + `}`,
	}
	if len(cases) != len(adTypes()) {
		t.Fatalf("%d cases for %d offered ad types", len(cases), len(adTypes()))
	}
	for kind, body := range cases {
		t.Run(kind, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adGroupAdsPath(clientID)] = adGroupAdResults(adName)
			_, err := e.invokeConfirmed(adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":`+
				`{"final_urls":["https://www.example.com/"],"`+kind+`":`+body+`}}]`, ""))
			if err != nil {
				t.Fatal(err)
			}
			ops := bodyOf(t, e.posts()[0])["operations"].([]any)
			ad := ops[0].(map[string]any)["create"].(map[string]any)["ad"].(map[string]any)
			if _, ok := ad[camel(kind)]; !ok || len(ops) != 1 {
				t.Errorf("ad = %v", ad)
			}
		})
	}
}

func TestAdGroupAdsCarryUrlCollectionsAppUrlsDisplayUrlAndImageBytes(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupAdsPath(clientID)] = adGroupAdResults(adName, own+"/adGroupAds/31~42")
	_, err := e.invokeConfirmed(adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"final_urls":`+
		`["https://www.example.com/"],"display_url":"example.com/shoes","url_collections":[{"url_collection_id":"sale",`+
		`"final_urls":["https://www.example.com/sale"],"final_mobile_urls":["https://m.example.com/sale"],`+
		`"tracking_url_template":"https://t.example.com/?u={lpurl}"}],"final_app_urls":[{"os_type":"IOS","url":"shoes://sale"},`+
		`{"os_type":"ANDROID","url":"shoes://sale"}],"hotel_ad":{}}},{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"image_ad":`+
		`{"content_base64":"QUJD"}}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	want := wantWire(t, `[{"create":{"adGroup":"`+own+`/adGroups/31","status":"PAUSED","ad":{"finalUrls":["https://www.example.com/"],`+
		`"displayUrl":"example.com/shoes","urlCollections":[{"urlCollectionId":"sale","finalUrls":["https://www.example.com/sale"],`+
		`"finalMobileUrls":["https://m.example.com/sale"],"trackingUrlTemplate":"https://t.example.com/?u={lpurl}"}],`+
		`"finalAppUrls":[{"osType":"IOS","url":"shoes://sale"},{"osType":"ANDROID","url":"shoes://sale"}],"hotelAd":{}}}},`+
		`{"create":{"adGroup":"`+own+`/adGroups/31","status":"PAUSED","ad":{"imageAd":{"data":"QUJD"}}}}]`)
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("operations = %s", e.posts()[0].body)
	}
}

func TestAdGroupAdImageBytesAreInlineOnlyAndNeedNoFilesRelease(t *testing.T) {
	args := adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"image_ad":{"content_base64":"QUJD"}}}]`, "")
	// A connection without a files release reaches create.
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupAdsPath(clientID)] = adGroupAdResults(adName)
	if _, err := e.invokeConfirmed(adGroupAdsCreateID, args); err != nil {
		t.Fatal(err)
	}
	ad := bodyOf(t, e.posts()[0])["operations"].([]any)[0].(map[string]any)["create"].(map[string]any)["ad"]
	if !reflect.DeepEqual(ad, wantWire(t, `{"imageAd":{"data":"QUJD"}}`)) {
		t.Errorf("ad = %v", ad)
	}
	// A local file is no argument of an ad, even on a connection that releases the directory.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "banner.png"), []byte("ABC"), 0o600); err != nil {
		t.Fatal(err)
	}
	e = newEnvironmentWith(t, nil, nil, config.Connection{Tools: changeToolIDs, Files: config.Files{Read: []string{dir}}})
	_, err := e.invokeConfirmed(adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"image_ad":`+
		`{"local_path":"`+filepath.Join(dir, "banner.png")+`"}}}]`, ""))
	if err == nil {
		t.Fatal("accepted a local path")
	}
	e.untouched(t)
	// More than 4 MiB inline is refused.
	e = newMutationEnvironment(t, nil, nil)
	big := strings.Repeat("A", maxInlineBase64+4)
	if _, err := e.invokeConfirmed(adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED","ad":{"image_ad":`+
		`{"content_base64":"`+big+`"}}}]`, "")); err == nil {
		t.Fatal("accepted more than 4 MiB")
	}
	e.untouched(t)
}

func TestAdGroupAdsCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+adGroupAdsPath(clientID)] = adGroupAdResults(own+"/adGroupAds/31~41", own+"/adGroupAds/32~42")
	out, err := e.invokeConfirmed(adGroupAdsCreateID, adGroupAdsCall(`[{`+adGroupAdGroup+`,"status":"PAUSED",`+
		`"start_date_time":"2027-01-01 08:00:00","ad_group_ad_asset_automation_settings":[{"asset_automation_type":`+
		`"TEXT_ASSET_AUTOMATION","asset_automation_status":"OPTED_OUT"}],"policy_validation_parameter":`+
		`{"ignorable_policy_topics":["TRADEMARKS_IN_AD_TEXT"]},"ad":{"final_urls":["https://www.example.com/shoes"],`+
		`"final_mobile_urls":["https://m.example.com/shoes"],"tracking_url_template":"https://t.example.com/?u={lpurl}",`+
		`"final_url_suffix":"utm_source=g","url_custom_parameters":[{"key":"season","value":"spring"}],"name":"RSA 1",`+
		`"device_preference":"MOBILE","responsive_search_ad":{"headlines":[{"text":"Shoes","pinned_field":"HEADLINE_1"},`+
		`{"text":"Free delivery"},{"text":"New season"}],"descriptions":[{"text":"Find your pair."},{"text":"Order today."}],`+
		`"path1":"shoes","path2":"new"}}},{"ad_group":"`+own+`/adGroups/32","status":"ENABLED","ad":{"final_urls":`+
		`["https://www.example.com/"],"image_ad":{"image_asset":{"asset":"`+own+`/assets/5"}}},`+
		`"policy_validation_parameter":{"exempt_policy_violation_keys":[{"policy_name":"P_ONE","violating_text":"free"}]}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
		results[0].(map[string]any)["resource_name"] != own+"/adGroupAds/31~41" ||
		results[1].(map[string]any)["resource_name"] != own+"/adGroupAds/32~42" || results[0].(map[string]any)["ad_group_ad"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+adGroupAdsPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	want := wantWire(t, `[{"create":{"adGroup":"`+own+`/adGroups/31","status":"PAUSED","startDateTime":"2027-01-01 08:00:00",`+
		`"adGroupAdAssetAutomationSettings":[{"assetAutomationType":"TEXT_ASSET_AUTOMATION","assetAutomationStatus":"OPTED_OUT"}],`+
		`"ad":{"finalUrls":["https://www.example.com/shoes"],"finalMobileUrls":["https://m.example.com/shoes"],`+
		`"trackingUrlTemplate":"https://t.example.com/?u={lpurl}","finalUrlSuffix":"utm_source=g","urlCustomParameters":`+
		`[{"key":"season","value":"spring"}],"name":"RSA 1","devicePreference":"MOBILE","responsiveSearchAd":{"headlines":`+
		`[{"text":"Shoes","pinnedField":"HEADLINE_1"},{"text":"Free delivery"},{"text":"New season"}],"descriptions":`+
		`[{"text":"Find your pair."},{"text":"Order today."}],"path1":"shoes","path2":"new"}}},"policyValidationParameter":`+
		`{"ignorablePolicyTopics":["TRADEMARKS_IN_AD_TEXT"]}},{"create":{"adGroup":"`+own+`/adGroups/32","status":"ENABLED",`+
		`"ad":{"finalUrls":["https://www.example.com/"],"imageAd":{"imageAsset":{"asset":"`+own+`/assets/5"}}}},`+
		`"policyValidationParameter":{"exemptPolicyViolationKeys":[{"policyName":"P_ONE","violatingText":"free"}]}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

func TestAdGroupAdsUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupAdsPath(clientID)] = adGroupAdResults(own+"/adGroupAds/31~41", own+"/adGroupAds/32~42")
	_, err := e.invokeConfirmed(adGroupAdsUpdateID, adGroupAdsCall(`[{"resource_name":"`+adName+`","status":"ENABLED",`+
		`"start_date_time":"2027-01-01 08:00:00","ad_group_ad_asset_automation_settings":[{"asset_automation_type":`+
		`"TEXT_ASSET_AUTOMATION","asset_automation_status":"OPTED_IN"}],"policy_validation_parameter":`+
		`{"ignorable_policy_topics":["ALCOHOL"]}},{"resource_name":"`+own+`/adGroupAds/32~42","end_date_time":""}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, second := ops[0].(map[string]any), ops[1].(map[string]any)
	if first["updateMask"] != "status,startDateTime,adGroupAdAssetAutomationSettings" {
		t.Errorf("first mask = %v", first["updateMask"])
	}
	if !reflect.DeepEqual(first["update"], wantWire(t, `{"resourceName":"`+adName+`","status":"ENABLED","startDateTime":`+
		`"2027-01-01 08:00:00","adGroupAdAssetAutomationSettings":[{"assetAutomationType":"TEXT_ASSET_AUTOMATION",`+
		`"assetAutomationStatus":"OPTED_IN"}]}`)) ||
		!reflect.DeepEqual(first["policyValidationParameter"], wantWire(t, `{"ignorablePolicyTopics":["ALCOHOL"]}`)) {
		t.Errorf("first operation = %v", first)
	}
	// A text that is cleared is in the mask and has no value.
	if second["updateMask"] != "endDateTime" ||
		!reflect.DeepEqual(second["update"], map[string]any{"resourceName": own + "/adGroupAds/32~42"}) {
		t.Errorf("second operation = %v", second)
	}
}

func TestAdGroupAdsRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupAdsPath(clientID)] = adGroupAdResults(adName, own+"/adGroupAds/32~42")
	out, err := e.invokeConfirmed(adGroupAdsRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+adName+`","`+
		own+`/adGroupAds/32~42"]}`)
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+adName+`"},{"remove":"`+own+`/adGroupAds/32~42"}]`)) ||
		len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestAdGroupAdValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		adGroupAdsCreateID: adGroupAdsCall(`[`+createdAdGroupAd+`}]`, `,"validate_only":true`),
		adGroupAdsUpdateID: adGroupAdsCall(`[{"resource_name":"`+adName+`","status":"PAUSED"}]`, `,"validate_only":true`),
		adGroupAdsRemoveID: adGroupAdRemoveArgs(`,"validate_only":true`),
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+adGroupAdsPath(clientID)] = okReply(answer)
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

func TestAdGroupAdResponseContentTypeShowsTheMutableAd(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupAdsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + adName + `","adGroupAd":{` +
		`"resourceName":"` + adName + `","status":"PAUSED","adGroup":"` + own + `/adGroups/31","startDateTime":"2027-01-01 08:00:00",` +
		`"ad":{"resourceName":"` + own + `/ads/41","finalUrls":["https://www.example.com/"],"responsiveSearchAd":{"headlines":` +
		`[{"text":"Shoes","pinnedField":"HEADLINE_1"}],"path1":"shoes"},"id":"41","unknownMember":"never held"},` +
		`"policySummary":{"approvalStatus":"APPROVED"},"adStrength":"GOOD","labels":["` + own + `/adGroupAdLabels/31~41~1"]}}]}`)
	out, err := e.invokeConfirmed(adGroupAdsUpdateID, adGroupAdsCall(`[{"resource_name":"`+adName+`","status":"PAUSED"}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	ad := decode(t, out)["results"].([]any)[0].(map[string]any)["ad_group_ad"].(map[string]any)
	want := map[string]any{"status": "PAUSED", "ad_group": own + "/adGroups/31", "start_date_time": "2027-01-01 08:00:00",
		"ad": map[string]any{"resource_name": own + "/ads/41", "final_urls": []any{"https://www.example.com/"},
			"responsive_search_ad": map[string]any{"headlines": []any{map[string]any{"text": "Shoes", "pinned_field": "HEADLINE_1"}},
				"path1": "shoes"}}}
	if !reflect.DeepEqual(ad, want) {
		t.Errorf("ad group ad = %v", ad)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that names another account, another ad, another ad group, or another number of results than asked
// for never passes. The change was made as far as Qatlas can tell, so the error says that the outcome is unknown.
func TestAdGroupAdAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	update := adGroupAdsCall(`[{"resource_name":"`+adName+`","status":"PAUSED"}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	create := adGroupAdsCall(`[`+createdAdGroupAd+`}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":  {adGroupAdsCreateID, create, `{"results":[{"resourceName":"` + foreign + `/adGroupAds/31~41"}]}`},
		"other ad":        {adGroupAdsUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroupAds/31~99"}]}`},
		"other ad group":  {adGroupAdsCreateID, create, `{"results":[{"resourceName":"` + own + `/adGroupAds/99~41"}]}`},
		"ad group kind":   {adGroupAdsCreateID, create, `{"results":[{"resourceName":"` + own + `/adGroups/31"}]}`},
		"temporary":       {adGroupAdsCreateID, create, `{"results":[{"resourceName":"` + own + `/adGroupAds/31~-41"}]}`},
		"no name":         {adGroupAdsUpdateID, update, `{"results":[{}]}`},
		"no results":      {adGroupAdsUpdateID, update, `{}`},
		"too many":        {adGroupAdsCreateID, create, `{"results":[{"resourceName":"` + adName + `"},{"resourceName":"` + adName + `"}]}`},
		"partial failure": {adGroupAdsUpdateID, update, `{"results":[{"resourceName":"` + adName + `"}],"partialFailureError":{"code":3}}`},
		"foreign ad group in": {adGroupAdsUpdateID, update, `{"results":[{"resourceName":"` + adName + `","adGroupAd":{"adGroup":"` +
			foreign + `/adGroups/31"}}]}`},
		"other ad group in": {adGroupAdsCreateID, create, `{"results":[{"resourceName":"` + adName + `","adGroupAd":{"adGroup":"` +
			own + `/adGroups/32"}}]}`},
		"other name in": {adGroupAdsUpdateID, update, `{"results":[{"resourceName":"` + adName + `","adGroupAd":{"resourceName":"` +
			own + `/adGroupAds/31~42"}}]}`},
		"other ad in": {adGroupAdsUpdateID, update, `{"results":[{"resourceName":"` + adName + `","adGroupAd":{"ad":{"resourceName":"` +
			own + `/ads/42"}}}]}`},
		"foreign asset in": {adGroupAdsUpdateID, update, `{"results":[{"resourceName":"` + adName + `","adGroupAd":{"ad":{"imageAd":` +
			`{"imageAsset":{"asset":"` + foreign + `/assets/5"}}}}}]}`},
		"member of the wrong kind": {adGroupAdsUpdateID, update, `{"results":[{"resourceName":"` + adName + `","adGroupAd":{"ad":"x"}}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adGroupAdsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAdGroupAdFailuresMapWithoutProviderText(t *testing.T) {
	arguments := adGroupAdsCall(`[`+createdAdGroupAd+`}]`, "")
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "adError", "CANNOT_SET_PATH2_WITHOUT_PATH1", ""), "",
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
			e.fake.routes["POST "+adGroupAdsPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(adGroupAdsCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+adGroupAdsPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(adGroupAdsCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// policyRejection is the answer of Google to a change whose ad has policy findings: a topic entry of a
// PolicyFindingError and a violation of a PolicyViolationError. Every text of Google echoes the canary.
func policyRejection(topic, entryType, policy string) string {
	return `{"error":{"code":400,"message":"` + detailCanary + `","status":"INVALID_ARGUMENT","details":[{"@type":` +
		`"type.googleapis.com/google.ads.googleads.v26.errors.GoogleAdsFailure","errors":[` +
		`{"errorCode":{"policyFindingError":"POLICY_FINDING"},"message":"` + detailCanary + `","trigger":{"stringValue":"` +
		triggerCanary + `"},"location":{"fieldPathElements":[{"fieldName":"operations","index":0},{"fieldName":"create"},` +
		`{"fieldName":"ad"}]},"details":{"policyFindingDetails":{"policyTopicEntries":[{"topic":"` + topic + `","type":"` +
		entryType + `","evidences":[{"textList":{"texts":["` + detailCanary + `"]}}]}]}}},` +
		`{"errorCode":{"policyViolationError":"POLICY_ERROR"},"message":"` + detailCanary + `","location":{"fieldPathElements":` +
		`[{"fieldName":"operations","index":0},{"fieldName":"create"},{"fieldName":"ad"},{"fieldName":"headlines","index":1}]},` +
		`"details":{"policyViolationDetails":{"externalPolicyDescription":"` + detailCanary + `","key":{"policyName":"` +
		policy + `","violatingText":"` + detailCanary + `"},"externalPolicyName":"` + detailCanary + `","isExemptible":true}}}],` +
		`"requestId":"synthetic-request-1"}]}}`
}

// A validate_only call reports the policy findings of Google as topics and policy names in the message,
// without any text of Google; the raw texts stay in the original.
func TestAdGroupAdPolicyFindingsAreReportedByValidateOnly(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupAdsPath(clientID)] = reply{status: 400, body: policyRejection("TRADEMARKS_IN_AD_TEXT", "PROHIBITED", "P_ONE")}
	_, err := e.invokeConfirmed(adGroupAdsCreateID, adGroupAdsCall(`[`+createdAdGroupAd+`}]`, `,"validate_only":true`))
	var failed *provider.Error
	if !errors.As(err, &failed) || failed.Class != provider.ClassProviderError {
		t.Fatalf("err = %v", err)
	}
	for _, word := range []string{"policyFindingError POLICY_FINDING", "topic TRADEMARKS_IN_AD_TEXT (PROHIBITED)",
		"operations[0].create.ad", "policy P_ONE (exemptible: true)", "operations[0].create.ad.headlines[1]",
		"ignorable_policy_topics", "nothing was changed"} {
		if !strings.Contains(err.Error(), word) {
			t.Errorf("error %q lacks %q", err.Error(), word)
		}
	}
	if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) {
		t.Errorf("error carries text of Google: %v", err)
	}
	var types []string
	var carried bool
	for _, entry := range failed.Original.Entries {
		types = append(types, entry.Type+"="+entry.Code)
		carried = carried || strings.Contains(entry.Message, detailCanary)
	}
	if !reflect.DeepEqual(types, []string{"=INVALID_ARGUMENT", "policyFindingError=POLICY_FINDING", "policyViolationError=POLICY_ERROR",
		"policyTopic=TRADEMARKS_IN_AD_TEXT", "policyViolation=P_ONE", "requestId=synthetic-request-1"}) || !carried {
		t.Errorf("original entries = %v (raw text carried: %v)", types, carried)
	}
	if bodyOf(t, e.posts()[0])["validateOnly"] != true || len(e.posts()) != 1 {
		t.Errorf("posts = %+v", e.posts())
	}
}

// A topic or policy name that is not of the documented form is provider text and stays out of the message.
func TestAdGroupAdPolicyFindingsWithOddNamesStayOutOfTheMessage(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adGroupAdsPath(clientID)] = reply{status: 400, body: policyRejection(detailCanary, "NOT A TYPE", "bad name "+detailCanary)}
	_, err := e.invokeConfirmed(adGroupAdsCreateID, adGroupAdsCall(`[`+createdAdGroupAd+`}]`, `,"validate_only":true`))
	if err == nil || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), "policy findings") {
		t.Fatalf("err = %v", err)
	}
}

func TestAdGroupAdChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(adGroupAdsRemoveID, `{"customer_id":"`+managerID+`","resource_names":["customers/`+
		queryNumber(managerID)+`/adGroupAds/31~41"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}

func TestRemoveAutomaticallyCreatedAssetsSendsTheAdInThePath(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+adGroupAdRemovalPath(clientID, "31~41")] = okReply(`{}`)
	out, err := e.invokeConfirmed(adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(adName,
		`[{"asset":"`+own+`/assets/33","asset_field_type":"HEADLINE"},{"asset":"`+own+`/assets/34","asset_field_type":"DESCRIPTION"}]`))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if got["customer_id"] != clientID || got["ad_group_ad"] != adName || len(got["removed"].([]any)) != 2 {
		t.Errorf("out = %s", out)
	}
	var sent []seen
	for _, s := range e.fake.api() {
		if s.method == "POST" {
			sent = append(sent, s)
		}
	}
	if len(sent) != 1 || sent[0].url != "https://googleads.googleapis.com"+adGroupAdRemovalPath(clientID, "31~41") ||
		sent[0].login != loginID || sent[0].developerToken != "" {
		t.Fatalf("requests = %+v", sent)
	}
	want := wantWire(t, `{"assetsWithFieldType":[{"asset":"`+own+`/assets/33","assetFieldType":"HEADLINE"},{"asset":"`+own+
		`/assets/34","assetFieldType":"DESCRIPTION"}]}`)
	if !reflect.DeepEqual(bodyOf(t, sent[0]), want) {
		t.Errorf("body = %s", sent[0].body)
	}
}

func TestRemoveAutomaticallyCreatedAssetsFailuresMapWithoutProviderText(t *testing.T) {
	cases := map[string]struct {
		status  int
		body    string
		class   provider.Class
		unknown bool
	}{
		"refused": {400, adsFailure(400, "INVALID_ARGUMENT", "automaticallyCreatedAssetRemovalError", "ASSET_NOT_FOUND", ""),
			provider.ClassProviderError, false},
		"quota": {429, adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED", ""),
			provider.ClassRateLimited, false},
		"server": {503, googleError(503, "UNAVAILABLE", ""), provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adGroupAdRemovalPath(clientID, "31~41")] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(adGroupAdCreatedAssetsRemoveID, adGroupAdRemovalArgs(adName, removalAssets))
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != c.unknown {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
