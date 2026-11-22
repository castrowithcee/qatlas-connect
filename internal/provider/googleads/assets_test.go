package googleads

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var assetToolIDs = []string{assetsCreateID, assetsUpdateID, campaignAutomaticallyCreatedAssetsRemoveID}

func assetsPath(id string) string { return "/v26/customers/" + id + "/assets:mutate" }
func removalPath(id string) string {
	return "/v26/customers/" + id + ":removeCampaignAutomaticallyCreatedAsset"
}

func assetName(id string, n int) string { return "customers/" + queryNumber(id) + "/assets/" + itoa(n) }

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// jsonText quotes a value for an argument document, so that a Windows path keeps its backslashes.
func jsonText(s string) string { b, _ := json.Marshal(s); return string(b) }

// assetEnvironment is a connection whose tools list names the asset tools and that releases dir for reading.
func assetEnvironment(t *testing.T, targets []string, dir string) *environment {
	t.Helper()
	conn := config.Connection{Tools: assetToolIDs}
	if dir != "" {
		conn.Files = config.Files{Read: []string{dir}}
	}
	return newEnvironmentWith(t, targets, nil, conn)
}

const sitelinkArgs = `{"customer_id":"` + clientID + `","assets":[{"name":"Pricing link",` +
	`"final_urls":["https://www.example.com/pricing"],"sitelink_asset":{"link_text":"Pricing",` +
	`"description1":"Plans for teams","ad_schedule_targets":[{"day_of_week":"MONDAY","start_hour":8,` +
	`"start_minute":"ZERO","end_hour":17,"end_minute":"ZERO"}]}}]}`

func TestAssetToolsAreListedOnlyConfirmedAndGuardedByFiles(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range assetToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Version != 1 || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Errorf("%s = %+v", id, d)
		}
		wantFiles := config.LocalFiles("")
		if id == assetsCreateID {
			wantFiles = config.LocalFilesRead
		}
		if d.LocalFiles != wantFiles {
			t.Errorf("%s: LocalFiles = %q, want %q", id, d.LocalFiles, wantFiles)
		}
		if got := tokenMetadata.Scopes[id]; len(got) != 1 || got[0] != scopeAdwords {
			t.Errorf("scopes of %s = %v", id, got)
		}
		schemas := string(d.InputSchema) + string(d.OutputSchema)
		for _, forbidden := range []string{"delivery_methods", "webhook", "google_secret", "login_customer_id",
			"partial_failure", "advertiser_webhook_url", `"operations"`} {
			if strings.Contains(schemas, forbidden) {
				t.Errorf("%s offers %s", id, forbidden)
			}
		}
		hasValidateOnly := strings.Contains(string(d.InputSchema), `"validate_only"`)
		if hasValidateOnly == (id == campaignAutomaticallyCreatedAssetsRemoveID) {
			t.Errorf("%s: validate_only offered = %v", id, hasValidateOnly)
		}
	}
	// The data of the immutable types and the output-only location asset are never an update argument.
	update, _, _ := reg.Lookup(assetsUpdateID)
	for _, immutable := range []string{"text_asset", "image_asset", "media_bundle_asset", "youtube_video_asset",
		"location_asset", "data"} {
		if strings.Contains(string(update.InputSchema), `"`+immutable+`"`) {
			t.Errorf("update offers %s", immutable)
		}
	}
	create, _, _ := reg.Lookup(assetsCreateID)
	if strings.Contains(string(create.InputSchema), "location_asset") {
		t.Error("create offers the output-only location asset")
	}
}

func TestAssetsNeedTheToolsListTheConfirmationAndTheFilesRelease(t *testing.T) {
	// Without the tools list, whatever the effects.
	e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
		config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute},
		Files: config.Files{Read: []string{t.TempDir()}}})
	if _, err := e.invokeConfirmed(assetsCreateID, sitelinkArgs); err == nil {
		t.Fatal("a connection without the tools list ran the tool")
	}
	e.untouched(t)
	// Without the confirmation of the call.
	e = assetEnvironment(t, nil, t.TempDir())
	_, err := e.invoke(assetsCreateID, sitelinkArgs)
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) || !strings.Contains(needed.Summary, "sitelink") {
		t.Fatalf("err = %v, want a confirmation that names the account and the asset", err)
	}
	e.untouched(t)
	// Without a directory released for reading, create is not offered; update and removal are.
	e = assetEnvironment(t, nil, "")
	if _, err := e.invokeConfirmed(assetsCreateID, sitelinkArgs); err == nil {
		t.Fatal("create ran on a connection without a files release")
	}
	e.untouched(t)
	e = assetEnvironment(t, nil, "")
	e.fake.routes["POST "+assetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + assetName(clientID, 9) + `"}]}`)
	if _, err := e.invokeConfirmed(assetsUpdateID, `{"customer_id":"`+clientID+`","assets":[{"resource_name":"`+
		assetName(clientID, 9)+`","name":"x"}]}`); err != nil {
		t.Fatalf("update on a connection without files: %v", err)
	}
}

func TestAssetsCreateSendsOneAtomicRequestAndMapsTheAnswer(t *testing.T) {
	e := assetEnvironment(t, nil, t.TempDir())
	e.fake.routes["POST "+assetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + assetName(clientID, 41) +
		`","asset":{"resourceName":"` + assetName(clientID, 41) + `","id":"41","name":"Pricing link","type":"SITELINK",` +
		`"finalUrls":["https://www.example.com/pricing"],"sitelinkAsset":{"linkText":"Pricing","description1":"` +
		"line\\u0007break" + `"},"leadFormAsset":{"deliveryMethods":[{"webhook":{"googleSecret":"` + detailCanary +
		`","advertiserWebhookUrl":"https://hook.example.com/"}}]},"imageAsset":{"data":"QUJD","fileSize":"3",` +
		`"mimeType":"IMAGE_PNG"},"unknownMember":"x"}}]}`)
	out, err := e.invokeConfirmed(assetsCreateID, strings.TrimSuffix(sitelinkArgs, "}")+`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{detailCanary, "hook.example.com", "QUJD", "unknownMember", "deliveryMethods", "delivery_methods"} {
		if strings.Contains(out, leak) {
			t.Errorf("result %s leaks %s", out, leak)
		}
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 1 {
		t.Fatalf("out = %s", out)
	}
	item := results[0].(map[string]any)
	asset := item["asset"].(map[string]any)
	link := asset["sitelink_asset"].(map[string]any)
	if item["resource_name"] != assetName(clientID, 41) || asset["id"] != float64(41) || asset["type"] != "SITELINK" ||
		link["link_text"] != "Pricing" || link["description1"] != "line break" {
		t.Errorf("item = %v", item)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+assetsPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if body["responseContentType"] != "MUTABLE_RESOURCE" || body["partialFailure"] != nil || body["validateOnly"] != nil {
		t.Errorf("body = %s", p.body)
	}
	ops := body["operations"].([]any)
	create := ops[0].(map[string]any)["create"].(map[string]any)
	sitelink := create["sitelinkAsset"].(map[string]any)
	schedule := sitelink["adScheduleTargets"].([]any)[0].(map[string]any)
	if len(ops) != 1 || create["name"] != "Pricing link" || sitelink["linkText"] != "Pricing" ||
		create["finalUrls"].([]any)[0] != "https://www.example.com/pricing" || schedule["dayOfWeek"] != "MONDAY" ||
		schedule["startHour"] != float64(8) {
		t.Errorf("operation = %v", ops)
	}
}

func TestAssetsValidateOnlyAnswersWithoutResults(t *testing.T) {
	e := assetEnvironment(t, nil, t.TempDir())
	e.fake.routes["POST "+assetsPath(clientID)] = okReply(`{}`)
	out, err := e.invokeConfirmed(assetsCreateID, strings.TrimSuffix(sitelinkArgs, "}")+`,"validate_only":true}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if got["validate_only"] != true || got["results"] != nil {
		t.Errorf("out = %s", out)
	}
	if bodyOf(t, e.posts()[0])["validateOnly"] != true {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestAssetsCreateMixedTypesInOneRequest(t *testing.T) {
	e := assetEnvironment(t, nil, t.TempDir())
	e.fake.routes["POST "+assetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + assetName(clientID, 1) +
		`"},{"resourceName":"` + assetName(clientID, 2) + `"},{"resourceName":"` + assetName(clientID, 3) + `"}]}`)
	_, err := e.invokeConfirmed(assetsCreateID, `{"customer_id":"`+clientID+`","assets":[`+
		`{"callout_asset":{"callout_text":"Free shipping"}},`+
		`{"structured_snippet_asset":{"header":"Brands","values":["A","B","C"]}},`+
		`{"promotion_asset":{"promotion_target":"Shoes","percent_off":100000,"promotion_code":"SPRING",`+
		`"redemption_start_date":"2026-12-01"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	promotion := ops[2].(map[string]any)["create"].(map[string]any)["promotionAsset"].(map[string]any)
	if len(ops) != 3 || promotion["percentOff"] != float64(100000) || promotion["promotionCode"] != "SPRING" {
		t.Errorf("operations = %v", ops)
	}
}

func TestAssetsImageBytesFollowTheFileContract(t *testing.T) {
	dir := t.TempDir()
	image := []byte("synthetic image bytes")
	if err := os.WriteFile(filepath.Join(dir, "banner.png"), image, 0o600); err != nil {
		t.Fatal(err)
	}
	e := assetEnvironment(t, nil, dir)
	e.fake.routes["POST "+assetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + assetName(clientID, 7) + `"},` +
		`{"resourceName":"` + assetName(clientID, 8) + `"}]}`)
	inline := base64.StdEncoding.EncodeToString([]byte("inline bytes"))
	out, err := e.invokeConfirmed(assetsCreateID, `{"customer_id":"`+clientID+`","assets":[`+
		`{"name":"From file","image_asset":{"local_path":`+jsonText(filepath.Join(dir, "banner.png"))+`}},`+
		`{"name":"Inline","image_asset":{"content_base64":"`+inline+`"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, inline) || strings.Contains(out, base64.StdEncoding.EncodeToString(image)) {
		t.Errorf("result carries image bytes: %s", out)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first := ops[0].(map[string]any)["create"].(map[string]any)["imageAsset"].(map[string]any)
	second := ops[1].(map[string]any)["create"].(map[string]any)["imageAsset"].(map[string]any)
	if first["data"] != base64.StdEncoding.EncodeToString(image) || second["data"] != inline ||
		len(first) != 1 || len(second) != 1 {
		t.Errorf("image assets = %v %v", first, second)
	}
}

// A path outside the released directory, both sources, no source, bad base64 and too much data end before a
// secret is read and before any request.
func TestAssetsImageInputsAreRefusedBeforeSecretAndIO(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "x.png"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.png"), make([]byte, maxMediaBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.png"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.png"), make([]byte, 6<<20/2), 0o600); err != nil {
		t.Fatal(err)
	}
	inline := base64.StdEncoding.EncodeToString([]byte("x"))
	cases := map[string]string{
		"outside":     `{"image_asset":{"local_path":` + jsonText(filepath.Join(outside, "x.png")) + `}}`,
		"relative":    `{"image_asset":{"local_path":"x.png"}}`,
		"missing":     `{"image_asset":{"local_path":` + jsonText(filepath.Join(dir, "none.png")) + `}}`,
		"both":        `{"image_asset":{"local_path":` + jsonText(filepath.Join(dir, "ok.png")) + `,"content_base64":"` + inline + `"}}`,
		"neither":     `{"image_asset":{}}`,
		"bad base64":  `{"image_asset":{"content_base64":"***"}}`,
		"empty":       `{"image_asset":{"local_path":` + jsonText(filepath.Join(dir, "empty.png")) + `}}`,
		"too large":   `{"image_asset":{"local_path":` + jsonText(filepath.Join(dir, "big.png")) + `}}`,
		"bundle path": `{"media_bundle_asset":{"local_path":` + jsonText(filepath.Join(outside, "x.png")) + `}}`,
		"inline big":  `{"image_asset":{"content_base64":"` + strings.Repeat("A", maxInlineBase64+4) + `"}}`,
		"on update":   ``,
	}
	for name, asset := range cases {
		t.Run(name, func(t *testing.T) {
			e := assetEnvironment(t, nil, dir)
			if asset == "" {
				if _, err := e.invokeConfirmed(assetsUpdateID, `{"customer_id":"`+clientID+`","assets":[{"resource_name":"`+
					assetName(clientID, 3)+`","image_asset":{"local_path":`+jsonText(filepath.Join(dir, "ok.png"))+`}}]}`); err == nil {
					t.Fatal("update accepted image data")
				}
				e.untouched(t)
				return
			}
			_, err := e.invokeConfirmed(assetsCreateID, `{"customer_id":"`+clientID+`","assets":[`+asset+`]}`)
			if !isInvalidRequest(err) && !strings.Contains(errString(err), "invalid") {
				t.Fatalf("err = %v, want invalid-request", err)
			}
			e.untouched(t)
		})
	}
	t.Run("more than 10 MiB in one call", func(t *testing.T) {
		e := assetEnvironment(t, nil, dir)
		one := `{"image_asset":{"local_path":` + jsonText(filepath.Join(dir, "ok.png")) + `}}`
		_, err := e.invokeConfirmed(assetsCreateID, `{"customer_id":"`+clientID+`","assets":[`+
			strings.Join([]string{one, one, one, one}, ",")+`]}`)
		if err == nil {
			t.Fatal("accepted 12 MiB of image data")
		}
		e.untouched(t)
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestAssetsRefuseWhatThePolicyExcludesBeforeSecretAndIO(t *testing.T) {
	const at = "@"
	foreign := "customers/" + queryNumber(otherID) + "/assets/5"
	bad := map[string]string{
		"webhook delivery": `{"lead_form_asset":{"business_name":"B","call_to_action_type":"LEARN_MORE",` +
			`"call_to_action_description":"d","headline":"h","description":"d","privacy_policy_url":"https://www.example.com/p",` +
			`"delivery_methods":[{"webhook":{"advertiser_webhook_url":"https://hook.example.com/"}}]}}`,
		"location asset":        `{"location_asset":{"place_id":"p"}}`,
		"two types":             `{"callout_asset":{"callout_text":"a"},"sitelink_asset":{"link_text":"b"}}`,
		"no type":               `{"name":"only a name"}`,
		"missing required":      `{"sitelink_asset":{"description1":"x"}}`,
		"http final url":        `{"final_urls":["http://www.example.com/"],"sitelink_asset":{"link_text":"a"}}`,
		"final url user info":   `{"final_urls":["https://user:pw` + at + `www.example.com/"],"sitelink_asset":{"link_text":"a"}}`,
		"http template":         `{"tracking_url_template":"http://t.example.com/?u={lpurl}","sitelink_asset":{"link_text":"a"}}`,
		"template user info":    `{"tracking_url_template":"https://u:p` + at + `t.example.com/","sitelink_asset":{"link_text":"a"}}`,
		"suffix as address":     `{"final_url_suffix":"https://x.example.com/","sitelink_asset":{"link_text":"a"}}`,
		"page url http":         `{"page_feed_asset":{"page_url":"http://www.example.com/"}}`,
		"foreign background":    `{"lead_form_asset":{"business_name":"B","call_to_action_type":"LEARN_MORE","call_to_action_description":"d","headline":"h","description":"d","privacy_policy_url":"https://www.example.com/p","background_image_asset":"` + foreign + `"}}`,
		"temporary asset id":    `{"demand_gen_carousel_card_asset":{"headline":"h","marketing_image_asset":"customers/` + queryNumber(clientID) + `/assets/-1"}}`,
		"foreign action":        `{"call_asset":{"country_code":"DE","phone_number":"030 0000000","call_conversion_action":"customers/` + queryNumber(otherID) + `/conversionActions/5"}}`,
		"both discounts":        `{"promotion_asset":{"promotion_target":"s","percent_off":5,"money_amount_off":{"currency_code":"EUR","amount_micros":5}}}`,
		"no discount":           `{"promotion_asset":{"promotion_target":"s"}}`,
		"two answer forms":      `{"lead_form_asset":{"business_name":"B","call_to_action_type":"LEARN_MORE","call_to_action_description":"d","headline":"h","description":"d","privacy_policy_url":"https://www.example.com/p","fields":[{"input_type":"FULL_NAME","has_location_answer":true,"single_choice_answers":{"answers":["a","b"]}}]}}`,
		"too few snippet items": `{"structured_snippet_asset":{"header":"h","values":["a","b"]}}`,
		"bad date":              `{"callout_asset":{"callout_text":"a","start_date":"1.12.2026"}}`,
		"control character":     `{"callout_asset":{"callout_text":"a\u0000b"}}`,
		"enum value in text":    `{"mobile_app_asset":{"app_id":"a","app_store":"google play","link_text":"x"}}`,
	}
	for name, asset := range bad {
		t.Run(name, func(t *testing.T) {
			e := assetEnvironment(t, nil, t.TempDir())
			if _, err := e.invokeConfirmed(assetsCreateID, `{"customer_id":"`+clientID+`","assets":[`+asset+`]}`); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("too many assets", func(t *testing.T) {
		e := assetEnvironment(t, nil, t.TempDir())
		one := `{"callout_asset":{"callout_text":"a"}}`
		if _, err := e.invokeConfirmed(assetsCreateID, `{"customer_id":"`+clientID+`","assets":[`+
			strings.Repeat(one+",", 10)+one+`]}`); err == nil {
			t.Fatal("accepted 11 assets")
		}
		e.untouched(t)
	})
	t.Run("foreign arguments", func(t *testing.T) {
		for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`,
			`"operations":[]`, `"path":"/x"`} {
			e := assetEnvironment(t, nil, t.TempDir())
			if _, err := e.invokeConfirmed(assetsCreateID, strings.TrimSuffix(sitelinkArgs, "}")+","+extra+"}"); err == nil {
				t.Errorf("accepted %s", extra)
			}
			e.untouched(t)
		}
	})
}

func TestAssetsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	cases := map[string]string{
		assetsCreateID: `{"customer_id":"` + otherID + `","assets":[{"callout_asset":{"callout_text":"a"}}]}`,
		assetsUpdateID: `{"customer_id":"` + otherID + `","assets":[{"resource_name":"` + assetName(otherID, 1) + `","name":"x"}]}`,
		campaignAutomaticallyCreatedAssetsRemoveID: `{"customer_id":"` + otherID + `","removals":[{"campaign":"customers/` +
			queryNumber(otherID) + `/campaigns/2","asset":"` + assetName(otherID, 1) + `","field_type":"HEADLINE"}]}`,
	}
	// A file that does not exist would be a file error if it were opened first.
	cases[assetsCreateID+" with a local file"] = `{"customer_id":"` + otherID + `","assets":[{"image_asset":` +
		`{"local_path":` + jsonText(filepath.Join(t.TempDir(), "missing.png")) + `}}]}`
	for id, arguments := range cases {
		id = strings.TrimSuffix(id, " with a local file")
		t.Run(id, func(t *testing.T) {
			e := assetEnvironment(t, []string{"customers/" + clientID}, t.TempDir())
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) || !strings.Contains(err.Error(), outsideTargets) {
				t.Fatalf("err = %v, want the target refusal without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestAssetsUpdateSendsTheMaskOfTheGivenMembers(t *testing.T) {
	e := assetEnvironment(t, nil, "")
	name := assetName(clientID, 9)
	e.fake.routes["POST "+assetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + name + `"}]}`)
	_, err := e.invokeConfirmed(assetsUpdateID, `{"customer_id":"`+clientID+`","assets":[{"resource_name":"`+name+`",`+
		`"name":"Plans","final_url_suffix":"","sitelink_asset":{"link_text":"Plans","description2":""}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	op := bodyOf(t, e.posts()[0])["operations"].([]any)[0].(map[string]any)
	update := op["update"].(map[string]any)
	if op["updateMask"] != "name,finalUrlSuffix,sitelinkAsset.linkText,sitelinkAsset.description2" ||
		update["resourceName"] != name || update["name"] != "Plans" || update["finalUrlSuffix"] != nil ||
		update["sitelinkAsset"].(map[string]any)["linkText"] != "Plans" || len(update["sitelinkAsset"].(map[string]any)) != 1 {
		t.Errorf("operation = %v", op)
	}
}

func TestAssetsUpdateRefusesBeforeSecretAndIO(t *testing.T) {
	name := assetName(clientID, 9)
	bad := map[string]string{
		"no member":        `{"resource_name":"` + name + `"}`,
		"immutable type":   `{"resource_name":"` + name + `","text_asset":{"text":"x"}}`,
		"created only":     `{"resource_name":"` + name + `","promotion_asset":{"external_redeem_info":{"code_type":"A","code_source":"B"}}}`,
		"foreign asset":    `{"resource_name":"customers/` + queryNumber(otherID) + `/assets/9","name":"x"}`,
		"temporary asset":  `{"resource_name":"customers/` + queryNumber(clientID) + `/assets/-3","name":"x"}`,
		"resource missing": `{"name":"x"}`,
		"two types":        `{"resource_name":"` + name + `","callout_asset":{"callout_text":"a"},"sitelink_asset":{"link_text":"b"}}`,
		"http template":    `{"resource_name":"` + name + `","tracking_url_template":"http://t.example.com/"}`,
	}
	for label, asset := range bad {
		t.Run(label, func(t *testing.T) {
			e := assetEnvironment(t, nil, "")
			if _, err := e.invokeConfirmed(assetsUpdateID, `{"customer_id":"`+clientID+`","assets":[`+asset+`]}`); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestAssetAnswersAreCheckedAgainstTheAccount(t *testing.T) {
	foreign := assetName(otherID, 5)
	own := assetName(clientID, 5)
	cases := map[string]string{
		"result for another account": `{"results":[{"resourceName":"` + foreign + `"}]}`,
		"asset for another account":  `{"results":[{"resourceName":"` + own + `","asset":{"resourceName":"` + foreign + `"}}]}`,
		"reference to another account": `{"results":[{"resourceName":"` + own + `","asset":{"leadFormAsset":{` +
			`"backgroundImageAsset":"` + foreign + `"}}}]}`,
		"conversion action of another account": `{"results":[{"resourceName":"` + own + `","asset":{"callAsset":{` +
			`"callConversionAction":"customers/` + queryNumber(otherID) + `/conversionActions/1"}}}]}`,
		"another number of results": `{"results":[]}`,
		"partial failure":           `{"results":[{"resourceName":"` + own + `"}],"partialFailureError":{"code":3}}`,
		"number that is none":       `{"results":[{"resourceName":"` + own + `","asset":{"id":"x1"}}]}`,
	}
	for label, answer := range cases {
		t.Run(label, func(t *testing.T) {
			e := assetEnvironment(t, nil, t.TempDir())
			e.fake.routes["POST "+assetsPath(clientID)] = okReply(answer)
			_, err := e.invokeConfirmed(assetsCreateID, `{"customer_id":"`+clientID+`","assets":[{"callout_asset":{"callout_text":"a"}}]}`)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "unknown") {
				t.Fatalf("err = %v, want invalid-provider-response with the unknown outcome", err)
			}
		})
	}
}

func TestAssetErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := map[string]struct {
		reply   reply
		class   provider.Class
		unknown bool
	}{
		"policy refusal": {reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "assetError", "INVALID_URL", "")},
			provider.ClassProviderError, false},
		"permission": {reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, false},
		"rate": {reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"rateScope":"ACCOUNT","retryDelay":"3s"}`)}, provider.ClassRateLimited, false},
		"server error": {reply{status: 503, body: googleError(503, "UNAVAILABLE", "")}, "", true},
		"cut":          {reply{err: errors.New("connection reset")}, "", true},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			e := assetEnvironment(t, nil, t.TempDir())
			e.fake.routes["POST "+assetsPath(clientID)] = c.reply
			_, err := e.invokeConfirmed(assetsCreateID, `{"customer_id":"`+clientID+`","assets":[{"callout_asset":{"callout_text":"a"}}]}`)
			if err == nil || (c.class != "" && classOf(err) != c.class) || strings.Contains(err.Error(), detailCanary) ||
				strings.Contains(err.Error(), triggerCanary) || strings.Contains(err.Error(), "unknown") != c.unknown {
				t.Fatalf("err = %v", err)
			}
			if len(e.posts()) != 1 {
				t.Errorf("requests = %d, want one without a repeat", len(e.posts()))
			}
		})
	}
}

func TestCampaignAutomaticallyCreatedAssetsRemove(t *testing.T) {
	e := assetEnvironment(t, nil, "")
	e.fake.routes["POST "+removalPath(clientID)] = okReply(`{}`)
	campaign := "customers/" + queryNumber(clientID) + "/campaigns/22"
	arguments := `{"customer_id":"` + clientID + `","removals":[{"campaign":"` + campaign + `","asset":"` +
		assetName(clientID, 33) + `","field_type":"HEADLINE"}]}`
	out, err := e.invokeConfirmed(campaignAutomaticallyCreatedAssetsRemoveID, arguments)
	if err != nil {
		t.Fatal(err)
	}
	removed := decode(t, out)["removed"].([]any)[0].(map[string]any)
	if removed["campaign"] != campaign || removed["asset"] != assetName(clientID, 33) || removed["field_type"] != "HEADLINE" {
		t.Errorf("out = %s", out)
	}
	posts := e.fake.api()
	if len(posts) != 1 || posts[0].url != "https://googleads.googleapis.com"+removalPath(clientID) {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	body := bodyOf(t, posts[0])
	op := body["operations"].([]any)[0].(map[string]any)
	if body["partialFailure"] != false || body["validateOnly"] != nil || op["campaign"] != campaign ||
		op["asset"] != assetName(clientID, 33) || op["fieldType"] != "HEADLINE" {
		t.Errorf("body = %s", posts[0].body)
	}
	// The answer is the summary of the confirmation too.
	e = assetEnvironment(t, nil, "")
	_, err = e.invoke(campaignAutomaticallyCreatedAssetsRemoveID, arguments)
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, campaign) {
		t.Fatalf("err = %v", err)
	}
	e.untouched(t)
}

func TestCampaignAutomaticallyCreatedAssetsRemoveChecksTheObjectsBeforeSecretAndIO(t *testing.T) {
	own := "customers/" + queryNumber(clientID)
	foreign := "customers/" + queryNumber(otherID)
	bad := map[string]string{
		"foreign campaign":   `{"campaign":"` + foreign + `/campaigns/2","asset":"` + own + `/assets/3","field_type":"HEADLINE"}`,
		"foreign asset":      `{"campaign":"` + own + `/campaigns/2","asset":"` + foreign + `/assets/3","field_type":"HEADLINE"}`,
		"temporary campaign": `{"campaign":"` + own + `/campaigns/-1","asset":"` + own + `/assets/3","field_type":"HEADLINE"}`,
		"temporary asset":    `{"campaign":"` + own + `/campaigns/2","asset":"` + own + `/assets/-2","field_type":"HEADLINE"}`,
		"asset as campaign":  `{"campaign":"` + own + `/assets/2","asset":"` + own + `/assets/3","field_type":"HEADLINE"}`,
		"unspecified":        `{"campaign":"` + own + `/campaigns/2","asset":"` + own + `/assets/3","field_type":"UNSPECIFIED"}`,
		"lower case":         `{"campaign":"` + own + `/campaigns/2","asset":"` + own + `/assets/3","field_type":"headline"}`,
	}
	for label, removal := range bad {
		t.Run(label, func(t *testing.T) {
			e := assetEnvironment(t, nil, "")
			if _, err := e.invokeConfirmed(campaignAutomaticallyCreatedAssetsRemoveID,
				`{"customer_id":"`+clientID+`","removals":[`+removal+`]}`); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("validate_only is no argument", func(t *testing.T) {
		e := assetEnvironment(t, nil, "")
		if _, err := e.invokeConfirmed(campaignAutomaticallyCreatedAssetsRemoveID, `{"customer_id":"`+clientID+
			`","validate_only":true,"removals":[{"campaign":"`+own+`/campaigns/2","asset":"`+own+
			`/assets/3","field_type":"HEADLINE"}]}`); err == nil {
			t.Fatal("accepted")
		}
		e.untouched(t)
	})
	t.Run("a partial failure of the answer", func(t *testing.T) {
		e := assetEnvironment(t, nil, "")
		e.fake.routes["POST "+removalPath(clientID)] = okReply(`{"partialFailureError":{"code":3,"message":"` + detailCanary + `"}}`)
		_, err := e.invokeConfirmed(campaignAutomaticallyCreatedAssetsRemoveID, `{"customer_id":"`+clientID+
			`","removals":[{"campaign":"`+own+`/campaigns/2","asset":"`+own+`/assets/3","field_type":"HEADLINE"}]}`)
		if err == nil || strings.Contains(err.Error(), detailCanary) {
			t.Fatalf("err = %v", err)
		}
	})
}
