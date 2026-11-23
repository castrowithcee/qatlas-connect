package googleads

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func goalsPath(id string) string { return "/v26/customers/" + id + "/goals:mutate" }
func campaignGoalConfigsPath(id string) string {
	return "/v26/customers/" + id + "/campaignGoalConfigs:mutate"
}
func lifecycleGoalName(id, n string) string { return "customers/" + queryNumber(id) + "/goals/" + n }
func goalConfigName(id, campaign, goal string) string {
	return "customers/" + queryNumber(id) + "/campaignGoalConfigs/" + campaign + "~" + goal
}

var lifecycleGoalToolIDs = []string{goalsCreateID, goalsUpdateID, campaignGoalConfigsCreateID, campaignGoalConfigsUpdateID,
	campaignGoalConfigsRemoveID}

func lifecycleGoalCalls() map[string]string {
	c := `{"customer_id":"` + clientID + `",`
	return map[string]string{
		goalsCreateID: c + `"goals":[{"goal_type":"LOYALTY_RETENTION","value_settings":{"additional_value":2.5,` +
			`"high_lifetime_value_multiplier":3}},{"goal_type":"NEW_CUSTOMER_ACQUISITION"}]}`,
		goalsUpdateID: c + `"goals":[{"goal_id":"71","goal_type":"CUSTOMER_RETENTION","value_settings":{"value_multiplier":1.5}}]}`,
		campaignGoalConfigsCreateID: c + `"campaign_goal_configs":[{"campaign_id":"11","goal_id":"71",` +
			`"goal_type":"NEW_CUSTOMER_ACQUISITION","target_option":"TARGET_SPECIFIC","value_settings_override":{"value_multiplier":2}}]}`,
		campaignGoalConfigsUpdateID: c + `"campaign_goal_configs":[{"campaign_id":"11","goal_id":"72","goal_type":"LOYALTY_RETENTION",` +
			`"enable_bid_adjustments_for_loyalty_members":true,"show_targeted_loyalty_member_benefits_in_pla":false}]}`,
		campaignGoalConfigsRemoveID: c + `"campaign_goal_configs":[{"campaign_id":"11","goal_id":"71"},{"campaign_id":"12","goal_id":"71"}]}`,
	}
}

func goalPathOf(id string) string {
	if strings.Contains(id, "campaign-goal-configs") {
		return campaignGoalConfigsPath(clientID)
	}
	return goalsPath(clientID)
}

// goalAnswers is the successful answer for each call above.
func goalAnswers() map[string]string {
	r := func(names ...string) string {
		var items []string
		for _, n := range names {
			items = append(items, `{"resourceName":"`+n+`"}`)
		}
		return `{"results":[` + strings.Join(items, ",") + `]}`
	}
	return map[string]string{
		goalsCreateID:               r(lifecycleGoalName(clientID, "81"), lifecycleGoalName(clientID, "82")),
		goalsUpdateID:               r(lifecycleGoalName(clientID, "71")),
		campaignGoalConfigsCreateID: r(goalConfigName(clientID, "11", "71")),
		campaignGoalConfigsUpdateID: r(goalConfigName(clientID, "11", "72")),
		campaignGoalConfigsRemoveID: r(goalConfigName(clientID, "11", "71"), goalConfigName(clientID, "12", "71")),
	}
}

func TestGoalToolsAreDescribed(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	for _, id := range lifecycleGoalToolIDs {
		d, _, ok := e.reg.Lookup(id)
		if !ok {
			t.Fatalf("%s is not registered", id)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || !slices.Contains(input.Required, "customer_id") {
			t.Errorf("%s: input schema %s", id, d.InputSchema)
		}
		for _, banned := range []string{"response_content_type", "partial_failure", "resource_name", "owner_customer", "goal_type\":\"UNKNOWN"} {
			if _, found := input.Properties[banned]; found {
				t.Errorf("%s offers %s", id, banned)
			}
		}
		if _, found := input.Properties["validate_only"]; !found || len(d.OutputSchema) == 0 || !strings.Contains(d.Description, "10") {
			t.Errorf("%s: validate_only, output schema or bound missing", id)
		}
		for _, banned := range []string{"UNSPECIFIED", "UNKNOWN"} {
			if strings.Contains(string(d.InputSchema), banned) {
				t.Errorf("%s names %s", id, banned)
			}
		}
	}
}

func TestGoalChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	for id, arguments := range lifecycleGoalCalls() {
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
	goal := func(id, item string) string { return c + `"goals":[` + item + `]}` }
	cfg := func(item string) string { return c + `"campaign_goal_configs":[` + item + `]}` }
	const base = `"campaign_id":"11","goal_id":"71"`
	cases := []struct{ id, arguments string }{
		{goalsCreateID, goal(goalsCreateID, `{"goal_type":"UNKNOWN"}`)},
		{goalsCreateID, goal(goalsCreateID, `{}`)},
		{goalsCreateID, goal(goalsCreateID, `{"goal_type":"RETENTION"}`)},
		{goalsCreateID, goal(goalsCreateID, `{"goal_type":"LOYALTY_RETENTION","goal_id":"5"}`)},
		{goalsCreateID, goal(goalsCreateID, `{"goal_type":"LOYALTY_RETENTION","value_settings":{}}`)},
		{goalsCreateID, goal(goalsCreateID, `{"goal_type":"LOYALTY_RETENTION","value_settings":{"additional_value":1,"value_multiplier":1}}`)},
		{goalsCreateID, goal(goalsCreateID, `{"goal_type":"LOYALTY_RETENTION","value_settings":{"additional_high_lifetime_value":1,"high_lifetime_value_multiplier":1}}`)},
		{goalsCreateID, goal(goalsCreateID, `{"goal_type":"LOYALTY_RETENTION","value_settings":{"value_multiplier":-1}}`)},
		{goalsCreateID, goal(goalsCreateID, `{"goal_type":"LOYALTY_RETENTION","goal_type2":1}`)},
		{goalsCreateID, c + `"goals":[]}`},
		{goalsCreateID, c + `"goals":[` + strings.TrimSuffix(strings.Repeat(`{"goal_type":"LOYALTY_RETENTION"},`, 11), ",") + `]}`},
		{goalsCreateID, `{"customer_id":"123","goals":[{"goal_type":"LOYALTY_RETENTION"}]}`},
		{goalsCreateID, c + `"goals":[{"goal_type":"LOYALTY_RETENTION"}],"partial_failure":true}`},
		{goalsCreateID, c + `"goals":[{"goal_type":"LOYALTY_RETENTION"}],"response_content_type":"MUTABLE_RESOURCE"}`},
		{goalsUpdateID, goal(goalsUpdateID, `{"goal_id":"5","goal_type":"LOYALTY_RETENTION"}`)},
		{goalsUpdateID, goal(goalsUpdateID, `{"goal_type":"LOYALTY_RETENTION","value_settings":{"value_multiplier":1}}`)},
		{goalsUpdateID, goal(goalsUpdateID, `{"goal_id":"customers/`+otherID+`/goals/5","goal_type":"LOYALTY_RETENTION","value_settings":{"value_multiplier":1}}`)},
		{goalsUpdateID, goal(goalsUpdateID, `{"goal_id":"0","goal_type":"LOYALTY_RETENTION","value_settings":{"value_multiplier":1}}`)},
		{goalsUpdateID, goal(goalsUpdateID, `{"goal_id":"5","goal_type":"LOYALTY_RETENTION","value_settings":{"value_multiplier":1}},`+
			`{"goal_id":"5","goal_type":"LOYALTY_RETENTION","value_settings":{"value_multiplier":2}}`)},
		{campaignGoalConfigsCreateID, cfg(`{` + base + `}`)},
		{campaignGoalConfigsCreateID, cfg(`{` + base + `,"goal_type":"UNSPECIFIED"}`)},
		{campaignGoalConfigsCreateID, cfg(`{"campaign":"customers/` + queryNumber(otherID) + `/campaigns/11","goal_id":"71","goal_type":"LOYALTY_RETENTION"}`)},
		{campaignGoalConfigsCreateID, cfg(`{"campaign_id":"customers/` + queryNumber(otherID) + `/campaigns/11","goal_id":"71","goal_type":"LOYALTY_RETENTION"}`)},
		{campaignGoalConfigsCreateID, cfg(`{` + base + `,"goal_type":"LOYALTY_RETENTION","target_option":"TARGET_ALL"}`)},
		{campaignGoalConfigsCreateID, cfg(`{` + base + `,"goal_type":"NEW_CUSTOMER_ACQUISITION","enable_bid_adjustments_for_loyalty_members":true}`)},
		{campaignGoalConfigsCreateID, cfg(`{` + base + `,"goal_type":"NEW_CUSTOMER_ACQUISITION","target_option":"TARGET_NONE"}`)},
		{campaignGoalConfigsCreateID, cfg(`{` + base + `,"goal_type":"NEW_CUSTOMER_ACQUISITION","value_settings_override":{}}`)},
		{campaignGoalConfigsCreateID, cfg(`{` + base + `,"goal_type":"NEW_CUSTOMER_ACQUISITION"},{` + base + `,"goal_type":"NEW_CUSTOMER_ACQUISITION"}`)},
		{campaignGoalConfigsUpdateID, cfg(`{` + base + `,"goal_type":"NEW_CUSTOMER_ACQUISITION"}`)},
		{campaignGoalConfigsUpdateID, cfg(`{` + base + `,"goal_type":"LOYALTY_RETENTION","target_option":"TARGET_ALL"}`)},
		{campaignGoalConfigsRemoveID, cfg(`{"campaign_id":"11"}`)},
		{campaignGoalConfigsRemoveID, cfg(`{` + base + `,"goal_type":"NEW_CUSTOMER_ACQUISITION"}`)},
		{campaignGoalConfigsRemoveID, cfg(`{` + base + `},{` + base + `}`)},
		{campaignGoalConfigsRemoveID, cfg(`{"campaign_id":"11~71","goal_id":"1"}`)},
		{campaignGoalConfigsRemoveID, c + `"campaign_goal_configs":[]}`},
		{campaignGoalConfigsRemoveID, c + `"campaign_goal_configs":[{` + base + `}],"login_customer_id":"` + loginID + `"}`},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%d %s", i, c.id), func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c.id, c.arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func operationsOf(t *testing.T, e *environment) []map[string]any {
	t.Helper()
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	body := bodyOf(t, posts[0])
	if len(body) != 1 || strings.Contains(posts[0].body, "partialFailure") || strings.Contains(posts[0].body, "responseContentType") {
		t.Errorf("body = %s, want the operations alone", posts[0].body)
	}
	var out []map[string]any
	for _, o := range body["operations"].([]any) {
		out = append(out, o.(map[string]any))
	}
	return out
}

func TestGoalToolsSendOneAtomicRequestPerCall(t *testing.T) {
	for _, id := range lifecycleGoalToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+goalPathOf(id)] = okReply(goalAnswers()[id])
			out, err := e.invokeConfirmed(id, lifecycleGoalCalls()[id])
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			results := got["results"].([]any)
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != len(operationsOf(t, e)) {
				t.Errorf("out = %s", out)
			}
			p := e.posts()[0]
			if p.url != "https://googleads.googleapis.com"+goalPathOf(id) || p.auth != "Bearer synthetic-access-1" ||
				p.login != loginID || p.developerToken != "" {
				t.Errorf("request = %+v", p)
			}
		})
	}
}

func TestGoalCreateAndUpdateBodies(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+goalsPath(clientID)] = okReply(goalAnswers()[goalsCreateID])
	out, err := e.invokeConfirmed(goalsCreateID, lifecycleGoalCalls()[goalsCreateID])
	if err != nil {
		t.Fatal(err)
	}
	first := decode(t, out)["results"].([]any)[0].(map[string]any)
	if first["goal_id"] != "81" || first["resource_name"] != lifecycleGoalName(clientID, "81") {
		t.Errorf("result = %v", first)
	}
	ops := operationsOf(t, e)
	loyalty := ops[0]["create"].(map[string]any)["loyaltyRetentionGoalSettings"].(map[string]any)["valueSettings"].(map[string]any)
	acquisition := ops[1]["create"].(map[string]any)["newCustomerAcquisitionGoalSettings"].(map[string]any)
	if loyalty["additionalValue"] != 2.5 || loyalty["highLifetimeValueMultiplier"] != float64(3) || len(loyalty) != 2 ||
		len(acquisition) != 0 || ops[0]["updateMask"] != nil {
		t.Errorf("operations = %v", ops)
	}

	e = newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+goalsPath(clientID)] = okReply(goalAnswers()[goalsUpdateID])
	if _, err := e.invokeConfirmed(goalsUpdateID, lifecycleGoalCalls()[goalsUpdateID]); err != nil {
		t.Fatal(err)
	}
	op := operationsOf(t, e)[0]
	update := op["update"].(map[string]any)
	if op["updateMask"] != "retentionGoalSettings.valueSettings" || update["resourceName"] != lifecycleGoalName(clientID, "71") ||
		update["retentionGoalSettings"].(map[string]any)["valueSettings"].(map[string]any)["valueMultiplier"] != 1.5 {
		t.Errorf("operation = %v", op)
	}
}

func TestCampaignGoalConfigBodiesAreBuiltFromTheCheckedAccount(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignGoalConfigsPath(clientID)] = okReply(goalAnswers()[campaignGoalConfigsCreateID])
	out, err := e.invokeConfirmed(campaignGoalConfigsCreateID, lifecycleGoalCalls()[campaignGoalConfigsCreateID])
	if err != nil {
		t.Fatal(err)
	}
	result := decode(t, out)["results"].([]any)[0].(map[string]any)
	if result["campaign_id"] != "11" || result["goal_id"] != "71" || result["resource_name"] != goalConfigName(clientID, "11", "71") {
		t.Errorf("result = %v", result)
	}
	create := operationsOf(t, e)[0]["create"].(map[string]any)
	settings := create["campaignNewCustomerAcquisitionSettings"].(map[string]any)
	if create["campaign"] != "customers/"+queryNumber(clientID)+"/campaigns/11" ||
		create["goal"] != lifecycleGoalName(clientID, "71") || settings["targetOption"] != "TARGET_SPECIFIC" ||
		settings["valueSettingsOverride"].(map[string]any)["valueMultiplier"] != float64(2) {
		t.Errorf("create = %v", create)
	}

	e = newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignGoalConfigsPath(clientID)] = okReply(goalAnswers()[campaignGoalConfigsUpdateID])
	if _, err := e.invokeConfirmed(campaignGoalConfigsUpdateID, lifecycleGoalCalls()[campaignGoalConfigsUpdateID]); err != nil {
		t.Fatal(err)
	}
	op := operationsOf(t, e)[0]
	update := op["update"].(map[string]any)
	loyalty := update["campaignLoyaltyRetentionSettings"].(map[string]any)
	if op["updateMask"] != "campaignLoyaltyRetentionSettings.enableBidAdjustmentsForLoyaltyMembers,"+
		"campaignLoyaltyRetentionSettings.showTargetedLoyaltyMemberBenefitsInPla" ||
		update["resourceName"] != goalConfigName(clientID, "11", "72") || loyalty["enableBidAdjustmentsForLoyaltyMembers"] != true ||
		loyalty["showTargetedLoyaltyMemberBenefitsInPla"] != false || len(update) != 2 {
		t.Errorf("operation = %v", op)
	}

	e = newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignGoalConfigsPath(clientID)] = okReply(goalAnswers()[campaignGoalConfigsRemoveID])
	if _, err := e.invokeConfirmed(campaignGoalConfigsRemoveID, lifecycleGoalCalls()[campaignGoalConfigsRemoveID]); err != nil {
		t.Fatal(err)
	}
	ops := operationsOf(t, e)
	if len(ops) != 2 || ops[0]["remove"] != goalConfigName(clientID, "11", "71") ||
		ops[1]["remove"] != goalConfigName(clientID, "12", "71") {
		t.Errorf("operations = %v", ops)
	}
}

func TestGoalValidateOnlyAnswersWithoutResults(t *testing.T) {
	for id, arguments := range lifecycleGoalCalls() {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+goalPathOf(id)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if results, _ := decode(t, out)["results"].([]any); decode(t, out)["validate_only"] != true || len(results) != 0 {
				t.Errorf("out = %s", out)
			}
			if bodyOf(t, e.posts()[0])["validateOnly"] != true {
				t.Errorf("body = %s", e.posts()[0].body)
			}
		})
	}
}

// An answer that does not fit the request is never taken for the change: the request was sent, so the error
// says that the outcome is unknown.
func TestGoalAnswersAreBoundToTheCheckedAccountAndRequest(t *testing.T) {
	foreignGoal, foreignConfig := lifecycleGoalName(otherID, "71"), goalConfigName(otherID, "11", "71")
	cases := map[string]struct{ id, body string }{
		"goal foreign account":   {goalsUpdateID, `{"results":[{"resourceName":"` + foreignGoal + `"}]}`},
		"goal other object":      {goalsUpdateID, `{"results":[{"resourceName":"` + lifecycleGoalName(clientID, "9") + `"}]}`},
		"goal no results":        {goalsUpdateID, `{}`},
		"goal partial failure":   {goalsUpdateID, `{"results":[{"resourceName":"` + lifecycleGoalName(clientID, "71") + `"}],"partialFailureError":{"code":3}}`},
		"goal create foreign":    {goalsCreateID, `{"results":[{"resourceName":"` + foreignGoal + `"},{"resourceName":"` + foreignGoal + `"}]}`},
		"goal create count":      {goalsCreateID, `{"results":[{"resourceName":"` + lifecycleGoalName(clientID, "81") + `"}]}`},
		"config foreign account": {campaignGoalConfigsCreateID, `{"results":[{"resourceName":"` + foreignConfig + `"}]}`},
		"config other pair":      {campaignGoalConfigsCreateID, `{"results":[{"resourceName":"` + goalConfigName(clientID, "11", "9") + `"}]}`},
		"config no tilde":        {campaignGoalConfigsUpdateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) + `/campaignGoalConfigs/11"}]}`},
		"config remove foreign":  {campaignGoalConfigsRemoveID, `{"results":[{"resourceName":"` + foreignConfig + `"},{"resourceName":"` + foreignConfig + `"}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+goalPathOf(c.id)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, lifecycleGoalCalls()[c.id])
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
		{"goal error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "goalError", "INVALID_GOAL", "")},
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
		for id, arguments := range lifecycleGoalCalls() {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+goalPathOf(id)] = c.reply
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

func TestGoalChangeOnManagerAccountNeedsAManagerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
	e.fake.routes["GET "+searchPath(clientID)] = rows()
	e.fake.routes["POST "+searchPath(clientID)] = rows(`{"customer":{"id":"` + clientID + `","manager":true}}`)
	_, err := e.invokeConfirmed(campaignGoalConfigsRemoveID, lifecycleGoalCalls()[campaignGoalConfigsRemoveID])
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d, want invalid-request and no change", err, len(e.posts()))
	}
}

func TestGoalSummariesNameTheEffect(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	for id, arguments := range lifecycleGoalCalls() {
		_, err := e.invoke(id, arguments)
		var needed *application.ConfirmationRequiredError
		if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) ||
			!strings.Contains(needed.Summary, "optimize") {
			t.Errorf("%s: summary %v", id, err)
		}
	}
	if s := campaignGoalConfigsSummary(campaignGoalConfigsRemoveID)(json.RawMessage(lifecycleGoalCalls()[campaignGoalConfigsRemoveID])); !strings.Contains(s, "detach") ||
		!strings.Contains(s, "no longer optimize") {
		t.Errorf("summary = %q", s)
	}
}
