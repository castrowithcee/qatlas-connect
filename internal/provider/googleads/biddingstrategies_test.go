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

var biddingStrategyToolIDs = []string{biddingStrategiesCreateID, biddingStrategiesUpdateID, biddingStrategiesRemoveID}

func biddingStrategiesPath(id string) string {
	return "/v26/customers/" + id + "/biddingStrategies:mutate"
}

func strategiesCall(strategies, extra string) string {
	return `{"customer_id":"` + clientID + `","bidding_strategies":` + strategies + extra + `}`
}

func strategyCreateArgs(members string) string {
	return strategiesCall(`[{"name":"Portfolio CPA"`+members+`}]`, "")
}

func strategyUpdateArgs(members string) string {
	return strategiesCall(`[{"resource_name":"`+own+`/biddingStrategies/31"`+members+`}]`, "")
}

func strategyResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

func TestBiddingStrategyToolsAreListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range biddingStrategyToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Fatalf("%s = %+v", id, d)
		}
		schema := string(d.InputSchema)
		for _, absent := range []string{"login_customer_id", "partial_failure", "enhanced_cpc", "target_spend_micros",
			"effective_currency_code", "campaign_count"} {
			if strings.Contains(schema, `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	create, _, _ := reg.Lookup(biddingStrategiesCreateID)
	for _, scheme := range biddingStrategySchemes {
		if !strings.Contains(string(create.InputSchema), `"`+scheme+`"`) {
			t.Errorf("create lacks scheme %s", scheme)
		}
	}
	if !strings.Contains(string(create.InputSchema), `"currency_code"`) {
		t.Error("create lacks currency_code")
	}
	update, _, _ := reg.Lookup(biddingStrategiesUpdateID)
	if strings.Contains(string(update.InputSchema), `"currency_code"`) {
		t.Error("update offers the immutable currency_code")
	}
	remove, _, _ := reg.Lookup(biddingStrategiesRemoveID)
	if strings.Contains(string(remove.InputSchema), "response_content_type") {
		t.Error("remove offers a response content type")
	}
}

func TestBiddingStrategyChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		biddingStrategiesCreateID: strategyCreateArgs(`,"target_cpa":{"target_cpa_micros":2000000}`),
		biddingStrategiesUpdateID: strategyUpdateArgs(`,"target_cpa":{"target_cpa_micros":2500000}`),
		biddingStrategiesRemoveID: `{"customer_id":"` + clientID + `","resource_names":["` + own + `/biddingStrategies/31"]}`,
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

func TestBiddingStrategySummaryNamesSchemeAndAmounts(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(biddingStrategiesCreateID, strategiesCall(`[{"name":"Portfolio CPA","target_cpa":`+
		`{"target_cpa_micros":2000000,"cpc_bid_ceiling_micros":900000}}]`, `,"validate_only":true`))
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "Portfolio CPA", "target_cpa", "target_cpa_micros 2000000", "spend"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(biddingStrategiesRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+own+`/biddingStrategies/31"]}`)
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "biddingStrategies/31") ||
		!strings.Contains(needed.Summary, "cannot be used again") {
		t.Errorf("summary = %v", err)
	}
}

func TestBiddingStrategyArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	roas := `,"target_roas":{"target_roas":3}`
	bad := map[string]struct{ id, arguments string }{
		"no name":         {biddingStrategiesCreateID, strategiesCall(`[{"target_cpa":{"target_cpa_micros":1}}]`, "")},
		"no scheme":       {biddingStrategiesCreateID, strategyCreateArgs(``)},
		"two schemes":     {biddingStrategiesCreateID, strategyCreateArgs(`,"target_cpa":{},"target_spend":{}`)},
		"enhanced cpc":    {biddingStrategiesCreateID, strategyCreateArgs(`,"enhanced_cpc":{}`)},
		"manual cpc":      {biddingStrategiesCreateID, strategyCreateArgs(`,"manual_cpc":{}`)},
		"deprecated":      {biddingStrategiesCreateID, strategyCreateArgs(`,"target_spend":{"target_spend_micros":5}`)},
		"output only":     {biddingStrategiesCreateID, strategyCreateArgs(roas + `,"status":"ENABLED"`)},
		"blank name":      {biddingStrategiesCreateID, strategiesCall(`[{"name":"  ","target_spend":{}}]`, "")},
		"long name":       {biddingStrategiesCreateID, strategiesCall(`[{"name":"`+strings.Repeat("x", 256)+`","target_spend":{}}]`, "")},
		"control name":    {biddingStrategiesCreateID, strategiesCall(`[{"name":"a\u0007b","target_spend":{}}]`, "")},
		"negative micros": {biddingStrategiesCreateID, strategyCreateArgs(`,"target_cpa":{"target_cpa_micros":-1}`)},
		"fraction micros": {biddingStrategiesCreateID, strategyCreateArgs(`,"target_cpa":{"target_cpa_micros":1.5}`)},
		"floor micros":    {biddingStrategiesCreateID, strategyCreateArgs(`,"target_spend":{"cpc_bid_floor_micros":1}`)},
		"roas range":      {biddingStrategiesCreateID, strategyCreateArgs(`,"target_roas":{"target_roas":1001}`)},
		"roas low":        {biddingStrategiesCreateID, strategyCreateArgs(`,"target_roas":{"target_roas":0.001}`)},
		"roas missing":    {biddingStrategiesCreateID, strategyCreateArgs(`,"target_roas":{"cpc_bid_ceiling_micros":5}`)},
		"roas empty":      {biddingStrategiesCreateID, strategyCreateArgs(`,"target_roas":{}`)},
		"share no ceiling": {biddingStrategiesCreateID, strategyCreateArgs(`,"target_impression_share":` +
			`{"location":"ANYWHERE_ON_PAGE","location_fraction_micros":500000}`)},
		"share location": {biddingStrategiesCreateID, strategyCreateArgs(`,"target_impression_share":` +
			`{"location":"NOWHERE","cpc_bid_ceiling_micros":5}`)},
		"share fraction": {biddingStrategiesCreateID, strategyCreateArgs(`,"target_impression_share":` +
			`{"cpc_bid_ceiling_micros":5,"location_fraction_micros":1000001}`)},
		"unknown member": {biddingStrategiesCreateID, strategyCreateArgs(`,"target_cpa":{"x":1}`)},
		"currency":       {biddingStrategiesCreateID, strategyCreateArgs(`,"target_spend":{},"currency_code":"eur"`)},
		"budget id":      {biddingStrategiesCreateID, strategyCreateArgs(`,"target_spend":{},"aligned_campaign_budget_id":0`)},
		"none":           {biddingStrategiesCreateID, strategiesCall(`[]`, "")},
		"eleven": {biddingStrategiesCreateID, strategiesCall(`[`+strings.Repeat(`{"name":"x","target_spend":{}},`, 10)+
			`{"name":"x","target_spend":{}}]`, "")},
		"content type": {biddingStrategiesCreateID, strategyCreateArgs(`,"target_spend":{}`)[:len(strategyCreateArgs(`,"target_spend":{}`))-1] +
			`,"response_content_type":"ALL"}`},

		"update no member":  {biddingStrategiesUpdateID, strategyUpdateArgs(``)},
		"update currency":   {biddingStrategiesUpdateID, strategyUpdateArgs(`,"currency_code":"EUR"`)},
		"update foreign":    {biddingStrategiesUpdateID, strategiesCall(`[{"resource_name":"`+foreign+`/biddingStrategies/31","name":"x"}]`, "")},
		"update temporary":  {biddingStrategiesUpdateID, strategiesCall(`[{"resource_name":"`+own+`/biddingStrategies/-3","name":"x"}]`, "")},
		"update collection": {biddingStrategiesUpdateID, strategiesCall(`[{"resource_name":"`+own+`/campaigns/31","name":"x"}]`, "")},
		"update twice": {biddingStrategiesUpdateID, strategiesCall(`[{"resource_name":"`+own+`/biddingStrategies/31","name":"x"},`+
			`{"resource_name":"`+own+`/biddingStrategies/31","name":"y"}]`, "")},
		"update two schemes": {biddingStrategiesUpdateID, strategyUpdateArgs(`,"target_cpa":{"target_cpa_micros":1},"target_spend":{}`)},

		"remove foreign":   {biddingStrategiesRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + foreign + `/biddingStrategies/31"]}`},
		"remove temporary": {biddingStrategiesRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/biddingStrategies/-31"]}`},
		"remove twice": {biddingStrategiesRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own +
			`/biddingStrategies/31","` + own + `/biddingStrategies/31"]}`},
		"remove none":    {biddingStrategiesRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
		"remove content": {biddingStrategiesRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/biddingStrategies/31"],"response_content_type":"MUTABLE_RESOURCE"}`},
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

func TestBiddingStrategiesCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+biddingStrategiesPath(clientID)] = strategyResults(own+"/biddingStrategies/31", own+"/biddingStrategies/32",
		own+"/biddingStrategies/33", own+"/biddingStrategies/34", own+"/biddingStrategies/35", own+"/biddingStrategies/36")
	out, err := e.invokeConfirmed(biddingStrategiesCreateID, strategiesCall(`[`+
		`{"name":"CPA","aligned_campaign_budget_id":11,"target_cpa":{"target_cpa_micros":2000000,"cpc_bid_ceiling_micros":900000,"cpc_bid_floor_micros":100000}},`+
		`{"name":"ROAS","target_roas":{"target_roas":3.5,"cpc_bid_ceiling_micros":700000,"target_roas_tolerance_percent_millis":10000}},`+
		`{"name":"Conversions","currency_code":"EUR","maximize_conversions":{"target_cpa_micros":1500000}},`+
		`{"name":"Value","maximize_conversion_value":{}},`+
		`{"name":"Share","target_impression_share":{"location":"ABSOLUTE_TOP_OF_PAGE","location_fraction_micros":500000,"cpc_bid_ceiling_micros":800000}},`+
		`{"name":"Clicks","target_spend":{"cpc_bid_ceiling_micros":600000}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 6 ||
		results[0].(map[string]any)["resource_name"] != own+"/biddingStrategies/31" ||
		results[0].(map[string]any)["bidding_strategy"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+biddingStrategiesPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	want := wantWire(t, `[{"create":{"name":"CPA","alignedCampaignBudgetId":11,"targetCpa":{"targetCpaMicros":2000000,`+
		`"cpcBidCeilingMicros":900000,"cpcBidFloorMicros":100000}}},`+
		`{"create":{"name":"ROAS","targetRoas":{"targetRoas":3.5,"cpcBidCeilingMicros":700000,"targetRoasTolerancePercentMillis":10000}}},`+
		`{"create":{"name":"Conversions","currencyCode":"EUR","maximizeConversions":{"targetCpaMicros":1500000}}},`+
		`{"create":{"name":"Value","maximizeConversionValue":{}}},`+
		`{"create":{"name":"Share","targetImpressionShare":{"location":"ABSOLUTE_TOP_OF_PAGE","locationFractionMicros":500000,"cpcBidCeilingMicros":800000}}},`+
		`{"create":{"name":"Clicks","targetSpend":{"cpcBidCeilingMicros":600000}}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

func TestBiddingStrategiesUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+biddingStrategiesPath(clientID)] = strategyResults(own+"/biddingStrategies/31", own+"/biddingStrategies/32")
	_, err := e.invokeConfirmed(biddingStrategiesUpdateID, strategiesCall(`[`+
		`{"resource_name":"`+own+`/biddingStrategies/31","name":"Renamed","target_roas":{"target_roas":4,"cpc_bid_ceiling_micros":700000}},`+
		`{"resource_name":"`+own+`/biddingStrategies/32","maximize_conversions":{}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, second := ops[0].(map[string]any), ops[1].(map[string]any)
	if first["updateMask"] != "name,targetRoas.cpcBidCeilingMicros,targetRoas.targetRoas" ||
		!reflect.DeepEqual(first["update"], wantWire(t, `{"resourceName":"`+own+`/biddingStrategies/31","name":"Renamed",`+
			`"targetRoas":{"targetRoas":4,"cpcBidCeilingMicros":700000}}`)) {
		t.Errorf("first operation = %v", first)
	}
	if second["updateMask"] != "maximizeConversions" || !reflect.DeepEqual(second["update"], map[string]any{
		"resourceName": own + "/biddingStrategies/32", "maximizeConversions": map[string]any{}}) {
		t.Errorf("second operation = %v", second)
	}
}

func TestBiddingStrategiesRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+biddingStrategiesPath(clientID)] = strategyResults(own+"/biddingStrategies/31", own+"/biddingStrategies/32")
	out, err := e.invokeConfirmed(biddingStrategiesRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+own+
		`/biddingStrategies/31","`+own+`/biddingStrategies/32"]}`)
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+own+`/biddingStrategies/31"},{"remove":"`+own+
		`/biddingStrategies/32"}]`)) || len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestBiddingStrategyValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		biddingStrategiesCreateID: strategiesCall(`[{"name":"x","target_spend":{}}]`, `,"validate_only":true`),
		biddingStrategiesUpdateID: strategiesCall(`[{"resource_name":"`+own+`/biddingStrategies/31","name":"x"}]`, `,"validate_only":true`),
		biddingStrategiesRemoveID: `{"customer_id":"` + clientID + `","resource_names":["` + own + `/biddingStrategies/31"],"validate_only":true}`,
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+biddingStrategiesPath(clientID)] = okReply(answer)
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

func TestBiddingStrategyResponseContentTypeShowsTheMutableStrategy(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+biddingStrategiesPath(clientID)] = okReply(`{"results":[{"resourceName":"` + own + `/biddingStrategies/31",` +
		`"biddingStrategy":{"resourceName":"` + own + `/biddingStrategies/31","id":"31","name":"CPA","status":"ENABLED",` +
		`"type":"TARGET_CPA","effectiveCurrencyCode":"EUR","campaignCount":"2","nonRemovedCampaignCount":"1",` +
		`"alignedCampaignBudgetId":"11","targetCpa":{"targetCpaMicros":"2000000"},"enhancedCpc":{},"unknownMember":"never held"}}]}`)
	out, err := e.invokeConfirmed(biddingStrategiesUpdateID, strategiesCall(`[{"resource_name":"`+own+`/biddingStrategies/31","name":"CPA"}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)["results"].([]any)[0].(map[string]any)["bidding_strategy"].(map[string]any)
	want := map[string]any{"resource_name": own + "/biddingStrategies/31", "id": float64(31), "name": "CPA", "status": "ENABLED",
		"type": "TARGET_CPA", "effective_currency_code": "EUR", "campaign_count": float64(2), "non_removed_campaign_count": float64(1),
		"aligned_campaign_budget_id": float64(11), "target_cpa": map[string]any{"target_cpa_micros": float64(2000000)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("strategy = %v", got)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestBiddingStrategyAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	update := strategiesCall(`[{"resource_name":"`+own+`/biddingStrategies/31","name":"x"}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	create := strategiesCall(`[{"name":"x","target_spend":{}}]`, ``)
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":   {biddingStrategiesCreateID, create, `{"results":[{"resourceName":"` + foreign + `/biddingStrategies/31"}]}`},
		"other strategy":   {biddingStrategiesUpdateID, update, `{"results":[{"resourceName":"` + own + `/biddingStrategies/99"}]}`},
		"other collection": {biddingStrategiesCreateID, create, `{"results":[{"resourceName":"` + own + `/campaigns/31"}]}`},
		"no results":       {biddingStrategiesUpdateID, update, `{}`},
		"too many":         {biddingStrategiesCreateID, create, `{"results":[{"resourceName":"` + own + `/biddingStrategies/1"},{"resourceName":"` + own + `/biddingStrategies/2"}]}`},
		"partial failure":  {biddingStrategiesUpdateID, update, `{"results":[{"resourceName":"` + own + `/biddingStrategies/31"}],"partialFailureError":{"code":3}}`},
		"other inside": {biddingStrategiesUpdateID, update, `{"results":[{"resourceName":"` + own + `/biddingStrategies/31","biddingStrategy":` +
			`{"resourceName":"` + own + `/biddingStrategies/32"}}]}`},
		"wrong kind": {biddingStrategiesUpdateID, update, `{"results":[{"resourceName":"` + own + `/biddingStrategies/31","biddingStrategy":` +
			`{"resourceName":"` + own + `/biddingStrategies/31","targetCpa":"x"}}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+biddingStrategiesPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestBiddingStrategyFailuresMapWithoutProviderText(t *testing.T) {
	arguments := strategiesCall(`[{"name":"x","target_spend":{}}]`, "")
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "biddingStrategyError", "DUPLICATE_NAME", ""), "",
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
			e.fake.routes["POST "+biddingStrategiesPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(biddingStrategiesCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+biddingStrategiesPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(biddingStrategiesCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A manager account takes strategies of its own, but only through a manager target that names it.
func TestBiddingStrategyChangesOnAManagerNeedAManagerTarget(t *testing.T) {
	arguments := `{"customer_id":"` + managerID + `","resource_names":["customers/` + queryNumber(managerID) + `/biddingStrategies/31"]}`
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	if _, err := e.invokeConfirmed(biddingStrategiesRemoveID, arguments); !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
