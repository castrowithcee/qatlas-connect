package googleads

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var goalToolIDs = []string{customerConversionGoalsUpdateID, campaignConversionGoalsUpdateID,
	conversionGoalCampaignConfigsUpdateID, customerSkAdNetworkSchemasUpdateID}

func goalPath(service string) string { return "/v26/customers/" + clientID + service + ":mutate" }

var goalServices = map[string]string{customerConversionGoalsUpdateID: customerConversionGoalService,
	campaignConversionGoalsUpdateID: campaignConversionGoalService, conversionGoalCampaignConfigsUpdateID: conversionGoalCampaignConfigService,
	customerSkAdNetworkSchemasUpdateID: skAdNetworkSchemaService}

func goalName(service, rest string) string {
	return "customers/" + queryNumber(clientID) + service + "/" + rest
}

func goalCalls() map[string]string {
	c := `{"customer_id":"` + clientID + `",`
	return map[string]string{
		customerConversionGoalsUpdateID: c + `"goals":[{"category":"PURCHASE","origin":"WEBSITE","biddable":false}]}`,
		campaignConversionGoalsUpdateID: c + `"goals":[{"campaign_id":"77","category":"PURCHASE","origin":"WEBSITE","biddable":true}]}`,
		conversionGoalCampaignConfigsUpdateID: c + `"configs":[{"campaign_id":"77","goal_config_level":"CAMPAIGN",` +
			`"custom_conversion_goal_id":"88"}]}`,
		customerSkAdNetworkSchemasUpdateID: c + `"account_link_id":"99","schema":{"app_id":"123456789",` +
			`"measurement_window_hours":72,"fine_grained_conversion_value_mappings":[{"fine_grained_conversion_value":5,` +
			`"conversion_value_mapping":{"mapped_events":[{"mapped_event_name":"purchase","event_revenue_range":` +
			`{"min":1,"max":5}}]}}],"postback_mappings":[{"postback_sequence_index":1,"lock_window_coarse_conversion_value":"HIGH"}]}}`,
	}
}

// goodAnswers are the answers Google gives to goalCalls.
func goodAnswers() map[string]string {
	return map[string]string{
		customerConversionGoalsUpdateID:       `{"results":[{"resourceName":"` + goalName(customerConversionGoalService, "PURCHASE~WEBSITE") + `"}]}`,
		campaignConversionGoalsUpdateID:       `{"results":[{"resourceName":"` + goalName(campaignConversionGoalService, "77~PURCHASE~WEBSITE") + `"}]}`,
		conversionGoalCampaignConfigsUpdateID: `{"results":[{"resourceName":"` + goalName(conversionGoalCampaignConfigService, "77") + `"}]}`,
		customerSkAdNetworkSchemasUpdateID:    `{"result":{"resourceName":"` + goalName(skAdNetworkSchemaService, "99") + `","appId":"123456789"}}`,
	}
}

func TestGoalToolsAreDescribedWithoutPartialFailure(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	for _, id := range goalToolIDs {
		d, _, ok := e.reg.Lookup(id)
		if !ok {
			t.Fatalf("%s is not registered", id)
		}
		if !d.RequiresToolAllowList || len(d.OutputSchema) == 0 || d.Version != 1 {
			t.Errorf("%s: descriptor %+v", id, d)
		}
		for _, banned := range []string{"partial_failure", "resource_name", "login_customer_id", "operations"} {
			if strings.Contains(string(d.InputSchema), `"`+banned+`"`) {
				t.Errorf("%s offers %s", id, banned)
			}
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		_ = json.Unmarshal(d.InputSchema, &input)
		_, content := input.Properties["response_content_type"]
		if want := id == conversionGoalCampaignConfigsUpdateID; content != want {
			t.Errorf("%s: response_content_type offered = %v, want %v", id, content, want)
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s has no validate_only", id)
		}
	}
}

func TestGoalChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	for id, arguments := range goalCalls() {
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
				t.Fatalf("err = %v, want a confirmation naming the account", err)
			}
			e.untouched(t)
		})
		t.Run(id+" target", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invokeConfirmed(id, strings.Replace(arguments, clientID, otherID, 1))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestGoalArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	c := `{"customer_id":"` + clientID + `",`
	cust := func(item string) string { return c + `"goals":[` + item + `]}` }
	camp := func(item string) string { return c + `"goals":[` + item + `]}` }
	cfg := func(item string) string { return c + `"configs":[` + item + `]}` }
	sk := func(schema string) string { return c + `"account_link_id":"99","schema":` + schema + `}` }
	g := `{"category":"PURCHASE","origin":"WEBSITE","biddable":true}`
	cases := []struct{ id, arguments string }{
		{customerConversionGoalsUpdateID, cust(`{"category":"PURCHASE","origin":"WEBSITE"}`)},
		{customerConversionGoalsUpdateID, cust(`{"category":"UNKNOWN","origin":"WEBSITE","biddable":true}`)},
		{customerConversionGoalsUpdateID, cust(`{"category":"PURCHASE","origin":"WEBSITE~X","biddable":true}`)},
		{customerConversionGoalsUpdateID, cust(`{"category":"PURCHASE/../x","origin":"WEBSITE","biddable":true}`)},
		{customerConversionGoalsUpdateID, cust(g + `,` + g)},
		{customerConversionGoalsUpdateID, cust(`{"category":"PURCHASE","origin":"WEBSITE","biddable":true,"resource_name":"x"}`)},
		{customerConversionGoalsUpdateID, c + `"goals":[]}`},
		{customerConversionGoalsUpdateID, c + `"goals":[` + g + `],"partial_failure":true}`},
		{customerConversionGoalsUpdateID, c + `"goals":[` + g + `],"response_content_type":"MUTABLE_RESOURCE"}`},
		{customerConversionGoalsUpdateID, strings.Replace(cust(g), clientID, "123", 1)},
		{customerConversionGoalsUpdateID, c + `"goals":[` + strings.TrimSuffix(strings.Repeat(g+",", 21), ",") + `]}`},
		{campaignConversionGoalsUpdateID, camp(g)},
		{campaignConversionGoalsUpdateID, camp(`{"campaign_id":"customers/` + otherID + `/campaigns/7","category":"PURCHASE","origin":"WEBSITE","biddable":true}`)},
		{campaignConversionGoalsUpdateID, camp(`{"campaign_id":"7~PURCHASE","category":"PURCHASE","origin":"WEBSITE","biddable":true}`)},
		{campaignConversionGoalsUpdateID, camp(`{"campaign_id":"0","category":"PURCHASE","origin":"WEBSITE","biddable":true}`)},
		{conversionGoalCampaignConfigsUpdateID, cfg(`{"campaign_id":"7"}`)},
		{conversionGoalCampaignConfigsUpdateID, cfg(`{"campaign_id":"7","goal_config_level":"ALL"}`)},
		{conversionGoalCampaignConfigsUpdateID, cfg(`{"campaign_id":"7","custom_conversion_goal_id":"customers/` + otherID + `/customConversionGoals/5"}`)},
		{conversionGoalCampaignConfigsUpdateID, cfg(`{"campaign_id":"7","goal_config_level":"CAMPAIGN"},{"campaign_id":"7","goal_config_level":"CUSTOMER"}`)},
		{conversionGoalCampaignConfigsUpdateID, cfg(`{"campaign_id":"7","goal_config_level":"CAMPAIGN","campaign":"x"}`)},
		{customerSkAdNetworkSchemasUpdateID, c + `"account_link_id":"x","schema":{"app_id":"1"}}`},
		{customerSkAdNetworkSchemasUpdateID, c + `"account_link_id":"customers/` + otherID + `/x/1","schema":{"app_id":"1"}}`},
		{customerSkAdNetworkSchemasUpdateID, sk(`{}`)},
		{customerSkAdNetworkSchemasUpdateID, sk(`{"app_id":"1 2"}`)},
		{customerSkAdNetworkSchemasUpdateID, sk(`{"app_id":"1","fine_grained_conversion_value_mappings":[{"fine_grained_conversion_value":64}]}`)},
		{customerSkAdNetworkSchemasUpdateID, sk(`{"app_id":"1","fine_grained_conversion_value_mappings":[{"fine_grained_conversion_value":1},{"fine_grained_conversion_value":1}]}`)},
		{customerSkAdNetworkSchemasUpdateID, sk(`{"app_id":"1","postback_mappings":[{"postback_sequence_index":3}]}`)},
		{customerSkAdNetworkSchemasUpdateID, sk(`{"app_id":"1","postback_mappings":[{"postback_sequence_index":0,"lock_window_event":"a","lock_window_fine_conversion_value":1}]}`)},
		{customerSkAdNetworkSchemasUpdateID, sk(`{"app_id":"1","postback_mappings":[{"postback_sequence_index":0,"lock_window_coarse_conversion_value":"VERY_HIGH"}]}`)},
		{customerSkAdNetworkSchemasUpdateID, sk(`{"app_id":"1","fine_grained_conversion_value_mappings":[{"fine_grained_conversion_value":1,"conversion_value_mapping":{"mapped_events":[{"mapped_event_name":"a","event_counter":1,"event_occurrence_range":{"min":1}}]}}]}`)},
		{customerSkAdNetworkSchemasUpdateID, sk(`{"app_id":"1","fine_grained_conversion_value_mappings":[{"fine_grained_conversion_value":1,"conversion_value_mapping":{"mapped_events":[{"mapped_event_name":"a","event_revenue_range":{"min":5,"max":1}}]}}]}`)},
		{customerSkAdNetworkSchemasUpdateID, sk(`{"app_id":"1","fine_grained_conversion_value_mappings":[{"fine_grained_conversion_value":1,"conversion_value_mapping":{"mapped_events":[{"mapped_event_name":"a","currency_code":"eur"}]}}]}`)},
		{customerSkAdNetworkSchemasUpdateID, c + `"account_link_id":"9","schema":{"app_id":"1"},"partial_failure":true}`},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("%d %s", i, tc.id), func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(tc.id, tc.arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestGoalRequestsCarryOnlyWhatTheReferenceNames(t *testing.T) {
	want := map[string]func(t *testing.T, body map[string]any){
		customerConversionGoalsUpdateID: func(t *testing.T, body map[string]any) {
			op := body["operations"].([]any)[0].(map[string]any)
			u := op["update"].(map[string]any)
			if op["updateMask"] != "biddable" || u["resourceName"] != goalName(customerConversionGoalService, "PURCHASE~WEBSITE") ||
				u["biddable"] != false || len(u) != 2 || len(body) != 1 {
				t.Errorf("body = %v", body)
			}
		},
		campaignConversionGoalsUpdateID: func(t *testing.T, body map[string]any) {
			op := body["operations"].([]any)[0].(map[string]any)
			u := op["update"].(map[string]any)
			if op["updateMask"] != "biddable" || u["resourceName"] != goalName(campaignConversionGoalService, "77~PURCHASE~WEBSITE") ||
				u["biddable"] != true || len(u) != 2 || len(body) != 1 {
				t.Errorf("body = %v", body)
			}
		},
		conversionGoalCampaignConfigsUpdateID: func(t *testing.T, body map[string]any) {
			op := body["operations"].([]any)[0].(map[string]any)
			u := op["update"].(map[string]any)
			if op["updateMask"] != "goalConfigLevel,customConversionGoal" ||
				u["resourceName"] != goalName(conversionGoalCampaignConfigService, "77") ||
				u["goalConfigLevel"] != "CAMPAIGN" ||
				u["customConversionGoal"] != goalName("/customConversionGoals", "88") ||
				len(u) != 3 || len(body) != 1 {
				t.Errorf("body = %v", body)
			}
		},
		customerSkAdNetworkSchemasUpdateID: func(t *testing.T, body map[string]any) {
			u := body["operation"].(map[string]any)["update"].(map[string]any)
			s := u["schema"].(map[string]any)
			fine := s["fineGrainedConversionValueMappings"].([]any)[0].(map[string]any)
			event := fine["conversionValueMapping"].(map[string]any)["mappedEvents"].([]any)[0].(map[string]any)
			post := s["postbackMappings"].([]any)[0].(map[string]any)
			if u["resourceName"] != goalName(skAdNetworkSchemaService, "99") || s["appId"] != "123456789" ||
				s["measurementWindowHours"] != float64(72) || fine["fineGrainedConversionValue"] != float64(5) ||
				event["mappedEventName"] != "purchase" || event["eventRevenueRange"].(map[string]any)["maxEventRevenue"] != float64(5) ||
				post["postbackSequenceIndex"] != float64(1) || post["lockWindowCoarseConversionValue"] != "HIGH" || len(body) != 1 {
				t.Errorf("body = %v", body)
			}
		},
	}
	for id, arguments := range goalCalls() {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+goalPath(goalServices[id])] = okReply(goodAnswers()[id])
			out, err := e.invokeConfirmed(id, arguments)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if got["customer_id"] != clientID || got["validate_only"] != false {
				t.Errorf("out = %s", out)
			}
			posts := e.posts()
			if len(posts) != 1 || len(e.fake.api()) != 1 {
				t.Fatalf("requests = %+v", e.fake.api())
			}
			p := posts[0]
			if p.url != "https://googleads.googleapis.com"+goalPath(goalServices[id]) || p.auth != "Bearer synthetic-access-1" ||
				p.login != loginID || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") ||
				strings.Contains(p.body, "responseContentType") || strings.Contains(p.body, "enableWarnings") {
				t.Errorf("request = %+v", p)
			}
			want[id](t, bodyOf(t, p))
		})
	}
}

func TestGoalValidateOnlyAnswersWithoutResults(t *testing.T) {
	for id, arguments := range goalCalls() {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+goalPath(goalServices[id])] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); got["validate_only"] != true || got["resource_name"] != nil {
				t.Errorf("out = %s", out)
			}
			if body := bodyOf(t, e.posts()[0]); body["validateOnly"] != true {
				t.Errorf("body = %v", body)
			}
		})
	}
}

func TestGoalConfigMutableResourceAndWarningsAreMappedAndBound(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	name := goalName(conversionGoalCampaignConfigService, "77")
	e.fake.routes["POST "+goalPath(conversionGoalCampaignConfigService)] = okReply(`{"results":[{"resourceName":"` + name +
		`","conversionGoalCampaignConfig":{"resourceName":"` + name + `","campaign":"customers/` + queryNumber(clientID) +
		`/campaigns/77","goalConfigLevel":"CAMPAIGN","customConversionGoal":"customers/` + queryNumber(clientID) +
		`/customConversionGoals/88\u0007"}}]}`)
	out, err := e.invokeConfirmed(conversionGoalCampaignConfigsUpdateID, strings.TrimSuffix(
		goalCalls()[conversionGoalCampaignConfigsUpdateID], "}")+`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	r := decode(t, out)["results"].([]any)[0].(map[string]any)
	cfg := r["conversion_goal_campaign_config"].(map[string]any)
	if r["campaign_id"] != "77" || cfg["goal_config_level"] != "CAMPAIGN" ||
		cfg["custom_conversion_goal"] != "customers/"+queryNumber(clientID)+"/customConversionGoals/88 " {
		t.Errorf("out = %s", out)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}

	e = newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+goalPath(skAdNetworkSchemaService)] = okReply(goodAnswers()[customerSkAdNetworkSchemasUpdateID][:len(
		goodAnswers()[customerSkAdNetworkSchemasUpdateID])-1] + `,"warning":{"code":3,"message":"check\u0007me"}}`)
	out, err = e.invokeConfirmed(customerSkAdNetworkSchemasUpdateID, strings.TrimSuffix(
		goalCalls()[customerSkAdNetworkSchemasUpdateID], "}")+`,"enable_warnings":true}`)
	if err != nil {
		t.Fatal(err)
	}
	w := decode(t, out)["warning"].(map[string]any)
	if w["code"] != float64(3) || w["message"] != "check me" || bodyOf(t, e.posts()[0])["enableWarnings"] != true {
		t.Errorf("out = %s, body = %s", out, e.posts()[0].body)
	}
}

// An answer that does not fit the request is never taken for the change.
func TestGoalAnswersAreBoundToTheCheckedAccountAndRequest(t *testing.T) {
	foreign := func(service, rest string) string { return "customers/" + queryNumber(otherID) + service + "/" + rest }
	cases := map[string]struct{ id, body string }{
		"customer foreign": {customerConversionGoalsUpdateID, `{"results":[{"resourceName":"` + foreign(customerConversionGoalService, "PURCHASE~WEBSITE") + `"}]}`},
		"customer other":   {customerConversionGoalsUpdateID, `{"results":[{"resourceName":"` + goalName(customerConversionGoalService, "SIGNUP~WEBSITE") + `"}]}`},
		"customer none":    {customerConversionGoalsUpdateID, `{}`},
		"customer partial": {customerConversionGoalsUpdateID, `{"results":[{"resourceName":"` + goalName(customerConversionGoalService, "PURCHASE~WEBSITE") + `"}],"partialFailureError":{"code":3}}`},
		"campaign other":   {campaignConversionGoalsUpdateID, `{"results":[{"resourceName":"` + goalName(campaignConversionGoalService, "78~PURCHASE~WEBSITE") + `"}]}`},
		"config foreign":   {conversionGoalCampaignConfigsUpdateID, `{"results":[{"resourceName":"` + foreign(conversionGoalCampaignConfigService, "77") + `"}]}`},
		"config foreign goal": {conversionGoalCampaignConfigsUpdateID, `{"results":[{"resourceName":"` + goalName(conversionGoalCampaignConfigService, "77") +
			`","conversionGoalCampaignConfig":{"customConversionGoal":"` + foreign("/customConversionGoals", "88") + `"}}]}`},
		"config other campaign": {conversionGoalCampaignConfigsUpdateID, `{"results":[{"resourceName":"` + goalName(conversionGoalCampaignConfigService, "77") +
			`","conversionGoalCampaignConfig":{"campaign":"customers/` + queryNumber(clientID) + `/campaigns/78"}}]}`},
		"schema foreign": {customerSkAdNetworkSchemasUpdateID, `{"result":{"resourceName":"` + foreign(skAdNetworkSchemaService, "99") + `"}}`},
		"schema other":   {customerSkAdNetworkSchemasUpdateID, `{"result":{"resourceName":"` + goalName(skAdNetworkSchemaService, "98") + `"}}`},
		"schema none":    {customerSkAdNetworkSchemasUpdateID, `{}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+goalPath(goalServices[c.id])] = okReply(c.body)
			arguments := goalCalls()[c.id]
			if c.id == conversionGoalCampaignConfigsUpdateID {
				arguments = strings.TrimSuffix(arguments, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
			}
			_, err := e.invokeConfirmed(c.id, arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestGoalErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		unknown bool
	}{
		{"mutate error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "mutateError", "RESOURCE_NOT_FOUND", "")},
			provider.ClassProviderError, false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, true},
		{"timeout", reply{err: errors.New("i/o timeout")}, provider.ClassUnreachable, true},
	}
	for _, c := range cases {
		for id, arguments := range goalCalls() {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+goalPath(goalServices[id])] = c.reply
				_, err := e.invokeConfirmed(id, arguments)
				if classOf(err) != c.class {
					t.Fatalf("err = %v, want %s", err, c.class)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != c.unknown {
					t.Errorf("unknown outcome reported = %v, want %v: %v", got, c.unknown, err)
				}
				for _, leaked := range []string{detailCanary, triggerCanary, "synthetic-access"} {
					if strings.Contains(err.Error(), leaked) {
						t.Errorf("error %q carries %q", err, leaked)
					}
				}
				if len(e.posts()) != 1 || len(e.fake.api()) != 1 {
					t.Errorf("mutate requests = %d of %d, want one and no repeat", len(e.posts()), len(e.fake.api()))
				}
			})
		}
	}
}

func TestGoalChangeOnManagerAccountNeedsAManagerTargetForAccountGoals(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
	e.fake.routes["GET "+searchPath(clientID)] = rows()
	e.fake.routes["POST "+searchPath(clientID)] = rows(`{"customer":{"id":"` + clientID + `","manager":true}}`)
	_, err := e.invokeConfirmed(customerConversionGoalsUpdateID, goalCalls()[customerConversionGoalsUpdateID])
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d, want invalid-request and no change", err, len(e.posts()))
	}
}

func TestGoalSummariesNameTheEffect(t *testing.T) {
	for id, arguments := range goalCalls() {
		var s string
		if id == customerSkAdNetworkSchemasUpdateID {
			s = skSummary(json.RawMessage(arguments))
		} else {
			s = goalSettingsSummary(id)(json.RawMessage(arguments))
		}
		if !strings.Contains(s, clientID) || !strings.Contains(s, "bid") && !strings.Contains(s, "optimize") &&
			!strings.Contains(s, "valued") {
			t.Errorf("%s: summary %q", id, s)
		}
	}
	if s := goalSettingsSummary(customerConversionGoalsUpdateID)(json.RawMessage(strings.TrimSuffix(
		goalCalls()[customerConversionGoalsUpdateID], "}") + `,"validate_only":true}`)); !strings.HasPrefix(s, "Only validate") ||
		!strings.Contains(s, "biddable=false") {
		t.Errorf("summary = %q", s)
	}
}
