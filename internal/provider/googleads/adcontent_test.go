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

func adsPath(id string) string { return "/v26/customers/" + id + "/ads:mutate" }

var adContentName = own + "/ads/41"

func adsCall(items, extra string) string {
	return `{"customer_id":"` + clientID + `","ads":` + items + extra + `}`
}

func adContentArgs(members string) string {
	return adsCall(`[{"resource_name":"`+adContentName+`"`+members+`}]`, "")
}

func adResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

func TestAdsUpdateIsListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	d, _, ok := reg.Lookup(adsUpdateID)
	if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired ||
		d.Risk.Effect != capability.EffectUpdate {
		t.Fatalf("descriptor = %+v", d)
	}
	schema := string(d.InputSchema)
	for _, absent := range []string{"login_customer_id", "partial_failure", "operations", `"name"`, `"text_ad"`,
		`"expanded_dynamic_search_ad"`, `"image_ad"`, `"expanded_text_ad"`, `"ad_group"`, "local_path",
		"content_base64", "policy_summary", "ad_strength"} {
		if strings.Contains(schema, absent) {
			t.Errorf("ads.update offers %s", absent)
		}
	}
	for _, present := range []string{`"responsive_search_ad"`, `"final_urls"`, `"final_url_suffix"`, `"tracking_url_template"`,
		`"url_collections"`, `"policy_validation_parameter"`, `"demand_gen_carousel_ad"`, `"synthetic_content_info"`} {
		if !strings.Contains(schema, present) {
			t.Errorf("ads.update lacks %s", present)
		}
	}
	var input struct {
		Properties struct {
			Ads struct {
				Items struct {
					Properties map[string]any `json:"properties"`
				} `json:"items"`
			} `json:"ads"`
		} `json:"properties"`
	}
	if json.Unmarshal(d.InputSchema, &input) != nil {
		t.Fatal("schema does not decode")
	}
	for _, member := range []string{"status", "type", "id"} {
		if _, found := input.Properties.Ads.Items.Properties[member]; found {
			t.Errorf("ads.update offers %s of an ad", member)
		}
	}
	for _, kind := range []string{"text_ad", "expanded_dynamic_search_ad", "image_ad"} {
		for _, f := range adUpdateSpec().fields {
			if f.name == kind {
				t.Errorf("update spec holds the immutable type %s", kind)
			}
		}
	}
}

func TestAdsUpdateNeedsTheToolsListAndTheConfirmation(t *testing.T) {
	arguments := adContentArgs(`,"final_urls":["https://www.example.com/sale"]`)
	t.Run("without the tools list", func(t *testing.T) {
		e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
			config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}})
		if _, err := e.invokeConfirmed(adsUpdateID, arguments); err == nil {
			t.Fatal("a connection with all effects but no tools list ran the tool")
		}
		e.untouched(t)
	})
	t.Run("without confirmation", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		_, err := e.invoke(adsUpdateID, arguments)
		var needed *application.ConfirmationRequiredError
		if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
			t.Fatalf("err = %v, want a confirmation that names the account", err)
		}
		e.untouched(t)
	})
	t.Run("outside the targets", func(t *testing.T) {
		e := newMutationEnvironment(t, []string{"customers/" + otherID}, nil)
		_, err := e.invokeConfirmed(adsUpdateID, arguments)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
			t.Fatalf("err = %v, want invalid-request without the ID", err)
		}
		e.untouched(t)
	})
}

func TestAdsUpdateSummaryNamesTheEffects(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(adsUpdateID, adsCall(`[{"resource_name":"`+adContentName+`","final_urls":["https://www.example.com/"],`+
		`"policy_validation_parameter":{"ignorable_policy_topics":["ALCOHOL"]}}]`, `,"validate_only":true`))
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "ads/41", "final_urls", "policy review again", "serves with the new content",
		"send clicks", "policy exemption", "cannot serve"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestAdsUpdateArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	const at = "@"
	headlines := `{"text":"a"},{"text":"b"},{"text":"c"}`
	bad := map[string]string{
		"no member":          adContentArgs(``),
		"none":               adsCall(`[]`, ""),
		"eleven":             adsCall(`[`+strings.Repeat(`{"resource_name":"`+adContentName+`","display_url":"x"},`, 10)+`{"resource_name":"`+adContentName+`","display_url":"x"}]`, ""),
		"name":               adContentArgs(`,"name":"x"`),
		"text ad":            adContentArgs(`,"text_ad":{"headline":"x"}`),
		"dynamic search ad":  adContentArgs(`,"expanded_dynamic_search_ad":{"description":"x"}`),
		"image ad":           adContentArgs(`,"image_ad":{"image_asset":{"asset":"` + own + `/assets/5"}}`),
		"image bytes":        adContentArgs(`,"image_ad":{"content_base64":"QUJD"}`),
		"output only":        adContentArgs(`,"ad_strength":"GOOD"`),
		"status":             adContentArgs(`,"status":"PAUSED"`),
		"empty type":         adContentArgs(`,"responsive_search_ad":{}`),
		"two types":          adContentArgs(`,"responsive_search_ad":{"path1":"a"},"hotel_ad":{}`),
		"two headlines":      adContentArgs(`,"responsive_search_ad":{"headlines":[{"text":"a"},{"text":"b"}]}`),
		"sixteen headlines":  adContentArgs(`,"responsive_search_ad":{"headlines":[` + strings.Repeat(`{"text":"a"},`, 15) + `{"text":"b"}]}`),
		"pin":                adContentArgs(`,"responsive_search_ad":{"headlines":[` + headlines + `,{"text":"d","pinned_field":"TEXT_DISCLAIMER"}]}`),
		"http final url":     adContentArgs(`,"final_urls":["http://www.example.com/"]`),
		"user info":          adContentArgs(`,"final_urls":["https://user:pw` + at + `www.example.com/"]`),
		"http template":      adContentArgs(`,"tracking_url_template":"http://t.example.com/?u={lpurl}"`),
		"template user info": adContentArgs(`,"tracking_url_template":"https://u:p` + at + `t.example.com/?u={lpurl}"`),
		"suffix address":     adContentArgs(`,"final_url_suffix":"https://x.example.com/"`),
		"collection url":     adContentArgs(`,"url_collections":[{"url_collection_id":"a","final_urls":["http://x.example.com/"]}]`),
		"bad parameter key":  adContentArgs(`,"url_custom_parameters":[{"key":"a b","value":"c"}]`),
		"device":             adContentArgs(`,"device_preference":"DESKTOP"`),
		"foreign asset":      adContentArgs(`,"app_ad":{"images":[{"asset":"` + foreign + `/assets/5"}]}`),
		"temporary asset":    adContentArgs(`,"app_ad":{"images":[{"asset":"` + own + `/assets/-5"}]}`),
		"content type":       adsCall(`[{"resource_name":"`+adContentName+`","display_url":"x"}]`, `,"response_content_type":"ALL"`),
		"foreign ad":         adsCall(`[{"resource_name":"`+foreign+`/ads/41","display_url":"x"}]`, ""),
		"temporary ad":       adsCall(`[{"resource_name":"`+own+`/ads/-41","display_url":"x"}]`, ""),
		"zero ad":            adsCall(`[{"resource_name":"`+own+`/ads/0","display_url":"x"}]`, ""),
		"ad group ad":        adsCall(`[{"resource_name":"`+own+`/adGroupAds/31~41","display_url":"x"}]`, ""),
		"no resource name":   adsCall(`[{"display_url":"x"}]`, ""),
		"twice": adsCall(`[{"resource_name":"`+adContentName+`","display_url":"x"},{"resource_name":"`+adContentName+
			`","display_url":"y"}]`, ""),
		"policy empty": adContentArgs(`,"display_url":"x","policy_validation_parameter":{}`),
		"policy both": adContentArgs(`,"display_url":"x","policy_validation_parameter":{"ignorable_policy_topics":["A"],` +
			`"exempt_policy_violation_keys":[{"policy_name":"B"}]}`),
		"policy topic": adContentArgs(`,"display_url":"x","policy_validation_parameter":{"ignorable_policy_topics":["a b"]}`),
	}
	for name, arguments := range bad {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(adsUpdateID, arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestAdsUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+adsPath(clientID)] = adResults(adContentName, own+"/ads/42")
	out, err := e.invokeConfirmed(adsUpdateID, adsCall(`[{"resource_name":"`+adContentName+`","final_urls":`+
		`["https://www.example.com/sale"],"tracking_url_template":"https://t.example.com/?u={lpurl}","final_url_suffix":"utm_source=g",`+
		`"responsive_search_ad":{"headlines":[{"text":"Sale","pinned_field":"HEADLINE_1"},{"text":"Shoes"},{"text":"Now"}],"path1":"sale"},`+
		`"policy_validation_parameter":{"ignorable_policy_topics":["ALCOHOL"]}},{"resource_name":"`+own+`/ads/42",`+
		`"display_url":"","final_mobile_urls":[],"app_ad":{"images":[{"asset":"`+own+`/assets/5"}]}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(got["results"].([]any)) != 2 ||
		got["results"].([]any)[0].(map[string]any)["resource_name"] != adContentName {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+adsPath(clientID) || p.login != loginID || p.developerToken != "" ||
		p.auth != "Bearer synthetic-access-1" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	ops := body["operations"].([]any)
	first, second := ops[0].(map[string]any), ops[1].(map[string]any)
	if first["updateMask"] != "finalUrls,trackingUrlTemplate,finalUrlSuffix,responsiveSearchAd.headlines,responsiveSearchAd.path1" {
		t.Errorf("first mask = %v", first["updateMask"])
	}
	if !reflect.DeepEqual(first["update"], wantWire(t, `{"resourceName":"`+adContentName+`","finalUrls":["https://www.example.com/sale"],`+
		`"trackingUrlTemplate":"https://t.example.com/?u={lpurl}","finalUrlSuffix":"utm_source=g","responsiveSearchAd":{"headlines":`+
		`[{"text":"Sale","pinnedField":"HEADLINE_1"},{"text":"Shoes"},{"text":"Now"}],"path1":"sale"}}`)) ||
		!reflect.DeepEqual(first["policyValidationParameter"], wantWire(t, `{"ignorablePolicyTopics":["ALCOHOL"]}`)) {
		t.Errorf("first operation = %v", first)
	}
	// A member that is cleared is in the mask and has no value.
	if second["updateMask"] != "finalMobileUrls,displayUrl,appAd.images" ||
		!reflect.DeepEqual(second["update"], wantWire(t, `{"resourceName":"`+own+`/ads/42","appAd":{"images":[{"asset":"`+own+`/assets/5"}]}}`)) ||
		second["policyValidationParameter"] != nil {
		t.Errorf("second operation = %v", second)
	}
}

func TestAdsUpdateValidateOnlyAnswersNoResult(t *testing.T) {
	for _, answer := range []string{`{}`, `{"results":[]}`} {
		t.Run(answer, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adsPath(clientID)] = okReply(answer)
			out, err := e.invokeConfirmed(adsUpdateID, adsCall(`[{"resource_name":"`+adContentName+`","display_url":"x"}]`,
				`,"validate_only":true`))
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

func TestAdsUpdateResponseContentTypeShowsTheMutableAd(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + adContentName + `","ad":{"resourceName":"` +
		adContentName + `","finalUrls":["https://www.example.com/"],"responsiveSearchAd":{"headlines":[{"text":"Shoes",` +
		`"pinnedField":"HEADLINE_1"}],"path1":"shoes"},"id":"41","adStrength":"GOOD","unknownMember":"never held"}}]}`)
	out, err := e.invokeConfirmed(adsUpdateID, adsCall(`[{"resource_name":"`+adContentName+`","display_url":"x"}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	ad := decode(t, out)["results"].([]any)[0].(map[string]any)["ad"].(map[string]any)
	want := map[string]any{"resource_name": adContentName, "final_urls": []any{"https://www.example.com/"},
		"responsive_search_ad": map[string]any{"headlines": []any{map[string]any{"text": "Shoes", "pinned_field": "HEADLINE_1"}},
			"path1": "shoes"}}
	if !reflect.DeepEqual(ad, want) {
		t.Errorf("ad = %v", ad)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that names another account, another ad, or another number of results than asked for never passes.
// The change was made as far as Qatlas can tell, so the error says that the outcome is unknown.
func TestAdsUpdateAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	arguments := adsCall(`[{"resource_name":"`+adContentName+`","display_url":"x"}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	cases := map[string]string{
		"foreign result":  `{"results":[{"resourceName":"` + foreign + `/ads/41"}]}`,
		"other ad":        `{"results":[{"resourceName":"` + own + `/ads/99"}]}`,
		"temporary":       `{"results":[{"resourceName":"` + own + `/ads/-41"}]}`,
		"kind":            `{"results":[{"resourceName":"` + own + `/adGroupAds/31~41"}]}`,
		"no name":         `{"results":[{}]}`,
		"no results":      `{}`,
		"too many":        `{"results":[{"resourceName":"` + adContentName + `"},{"resourceName":"` + adContentName + `"}]}`,
		"partial failure": `{"results":[{"resourceName":"` + adContentName + `"}],"partialFailureError":{"code":3}}`,
		"other name in":   `{"results":[{"resourceName":"` + adContentName + `","ad":{"resourceName":"` + own + `/ads/42"}}]}`,
		"foreign asset in": `{"results":[{"resourceName":"` + adContentName + `","ad":{"appAd":{"images":[{"asset":"` + foreign +
			`/assets/5"}]}}}]}`,
		"member of the wrong kind": `{"results":[{"resourceName":"` + adContentName + `","ad":"x"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adsPath(clientID)] = okReply(body)
			_, err := e.invokeConfirmed(adsUpdateID, arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAdsUpdateFailuresMapWithoutProviderText(t *testing.T) {
	arguments := adContentArgs(`,"display_url":"x"`)
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "adError", "AD_CANNOT_BE_UPDATED", ""), "", provider.ClassProviderError},
		"permission": {403, adsFailure(403, "PERMISSION_DENIED", "authorizationError", "USER_PERMISSION_DENIED", ""), "",
			provider.ClassPermission},
		"quota": {429, adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED",
			`{"rateScope":"ACCOUNT","retryDelay":"3s"}`), "", provider.ClassRateLimited},
		"server": {503, googleError(503, "UNAVAILABLE", ""), "unknown", provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adsPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(adsUpdateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+adsPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(adsUpdateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A validate_only call reports the policy findings of Google as topics, without any text of Google.
func TestAdsUpdatePolicyFindingsAreReportedByValidateOnly(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adsPath(clientID)] = reply{status: 400, body: policyRejection("TRADEMARKS_IN_AD_TEXT", "PROHIBITED", "P_ONE")}
	_, err := e.invokeConfirmed(adsUpdateID, adsCall(`[{"resource_name":"`+adContentName+`","display_url":"x"}]`, `,"validate_only":true`))
	if err == nil || classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "topic TRADEMARKS_IN_AD_TEXT (PROHIBITED)") ||
		strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) {
		t.Fatalf("err = %v", err)
	}
}

func TestAdsUpdateRefusesAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(adsUpdateID, `{"customer_id":"`+managerID+`","ads":[{"resource_name":"customers/`+
		queryNumber(managerID)+`/ads/41","display_url":"x"}]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
