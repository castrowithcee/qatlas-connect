package googleads

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"encoding/json"
	"fmt"
	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func conversionPath(id string) string { return "/v26/customers/" + id + "/conversionActions:mutate" }

func conversionName(id, action string) string {
	return "customers/" + queryNumber(id) + "/conversionActions/" + action
}

var conversionToolIDs = []string{conversionActionsCreateID, conversionActionsUpdateID, conversionActionsRemoveID}

const (
	createArgs = `"conversion_actions":[{"name":"Checkout","type":"WEBPAGE","category":"PURCHASE","counting_type":"ONE_PER_CLICK",` +
		`"click_through_lookback_window_days":30,"view_through_lookback_window_days":1,"primary_for_goal":true,` +
		`"value_settings":{"default_value":12.5,"default_currency_code":"EUR","always_use_default_value":false},` +
		`"attribution_model":"GOOGLE_ADS_LAST_CLICK","phone_call_duration_seconds":60}]`
	updateArgs = `"conversion_actions":[{"conversion_action_id":"5544332211","primary_for_goal":false,"status":"HIDDEN",` +
		`"value_settings":{"default_value":3}}]`
	removeArgs = `"conversion_action_ids":["5544332211","5544332212"]`
)

func conversionCalls() map[string]string {
	return map[string]string{
		conversionActionsCreateID: `{"customer_id":"` + clientID + `",` + createArgs + `}`,
		conversionActionsUpdateID: `{"customer_id":"` + clientID + `",` + updateArgs + `}`,
		conversionActionsRemoveID: `{"customer_id":"` + clientID + `",` + removeArgs + `}`,
	}
}

func TestConversionActionToolsAreDescribed(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	for _, id := range conversionToolIDs {
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
		for _, banned := range []string{"app_id", "include_in_conversions_metric", "firebase", "third_party", "owner_customer",
			"tag_snippets", "resource_name", "partial_failure"} {
			if strings.Contains(string(d.InputSchema), banned) {
				t.Errorf("%s offers %s", id, banned)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s has no validate_only", id)
		}
		if len(d.OutputSchema) == 0 || !strings.Contains(d.Description, "20") {
			t.Errorf("%s: output schema or bound missing", id)
		}
	}
	for _, banned := range []string{"FIREBASE", "THIRD_PARTY", "GOOGLE_PLAY", "SALESFORCE", "REMOVED"} {
		d, _, _ := e.reg.Lookup(conversionActionsCreateID)
		u, _, _ := e.reg.Lookup(conversionActionsUpdateID)
		if strings.Contains(string(d.InputSchema)+string(u.InputSchema), banned) {
			t.Errorf("an input schema names %s", banned)
		}
	}
}

func TestConversionActionChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	for id, arguments := range conversionCalls() {
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
		t.Run(id+" over the read effect", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" target", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
			foreign := strings.Replace(arguments, clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, foreign)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestConversionActionArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	item := func(extra string) string {
		return `{"customer_id":"` + clientID + `","conversion_actions":[{"name":"A","type":"WEBPAGE"` + extra + `}]}`
	}
	upd := func(extra string) string {
		return `{"customer_id":"` + clientID + `","conversion_actions":[{"conversion_action_id":"5"` + extra + `}]}`
	}
	cases := []struct{ id, arguments string }{
		{conversionActionsCreateID, item(`,"type":"FIREBASE_ANDROID_CUSTOM"`)},
		{conversionActionsCreateID, strings.Replace(item(``), `"WEBPAGE"`, `"FIREBASE_ANDROID_CUSTOM"`, 1)},
		{conversionActionsCreateID, strings.Replace(item(``), `"WEBPAGE"`, `"THIRD_PARTY_APP_ANALYTICS_IOS_CUSTOM"`, 1)},
		{conversionActionsCreateID, strings.Replace(item(``), `"WEBPAGE"`, `"GOOGLE_PLAY_DOWNLOAD"`, 1)},
		{conversionActionsCreateID, `{"customer_id":"` + clientID + `","conversion_actions":[{"type":"WEBPAGE"}]}`},
		{conversionActionsCreateID, `{"customer_id":"` + clientID + `","conversion_actions":[{"name":"A"}]}`},
		{conversionActionsCreateID, item(`,"app_id":"x"`)},
		{conversionActionsCreateID, item(`,"include_in_conversions_metric":true`)},
		{conversionActionsCreateID, item(`,"status":"REMOVED"`)},
		{conversionActionsCreateID, item(`,"attribution_model":"GOOGLE_SEARCH_ATTRIBUTION_LINEAR"`)},
		{conversionActionsCreateID, item(`,"counting_type":"UNKNOWN"`)},
		{conversionActionsCreateID, item(`,"category":"UNSPECIFIED"`)},
		{conversionActionsCreateID, item(`,"click_through_lookback_window_days":91`)},
		{conversionActionsCreateID, item(`,"click_through_lookback_window_days":0`)},
		{conversionActionsCreateID, item(`,"view_through_lookback_window_days":31`)},
		{conversionActionsCreateID, item(`,"phone_call_duration_seconds":10001`)},
		{conversionActionsCreateID, item(`,"phone_call_duration_seconds":-1`)},
		{conversionActionsCreateID, item(`,"value_settings":{}`)},
		{conversionActionsCreateID, item(`,"value_settings":{"default_value":-1}`)},
		{conversionActionsCreateID, item(`,"value_settings":{"default_currency_code":"eur"}`)},
		{conversionActionsCreateID, strings.Replace(item(`,"click_through_lookback_window_days":61`), `"WEBPAGE"`, `"AD_CALL"`, 1)},
		{conversionActionsCreateID, strings.Replace(item(`,"view_through_lookback_window_days":5`), `"WEBPAGE"`, `"WEBSITE_CALL"`, 1)},
		{conversionActionsCreateID, `{"customer_id":"` + clientID + `","conversion_actions":[{"name":"  ","type":"WEBPAGE"}]}`},
		{conversionActionsCreateID, `{"customer_id":"` + clientID + `","conversion_actions":[]}`},
		{conversionActionsCreateID, `{"customer_id":"` + clientID + `","conversion_actions":[` +
			strings.TrimSuffix(strings.Repeat(`{"name":"A","type":"WEBPAGE"},`, 21), ",") + `]}`},
		{conversionActionsCreateID, `{"customer_id":"123","conversion_actions":[{"name":"A","type":"WEBPAGE"}]}`},
		{conversionActionsCreateID, strings.Replace(item(``), `}`, `,"response_content_type":"ALL"}`, 1)},
		{conversionActionsUpdateID, upd(``)},
		{conversionActionsUpdateID, upd(`,"type":"WEBPAGE"`)},
		{conversionActionsUpdateID, upd(`,"status":"REMOVED"`)},
		{conversionActionsUpdateID, upd(`,"app_id":"x"`)},
		{conversionActionsUpdateID, upd(`,"resource_name":"customers/` + otherID + `/conversionActions/5"`)},
		{conversionActionsUpdateID, strings.Replace(upd(`,"name":"B"`), `"5"`, `"customers/`+otherID+`/conversionActions/5"`, 1)},
		{conversionActionsUpdateID, strings.Replace(upd(`,"name":"B"`), `"5"`, `"0"`, 1)},
		{conversionActionsUpdateID, strings.Replace(upd(`,"name":"B"`), `"5"`, `"5/../6"`, 1)},
		{conversionActionsUpdateID, `{"customer_id":"` + clientID + `","conversion_actions":[` +
			`{"conversion_action_id":"5","name":"B"},{"conversion_action_id":"5","name":"C"}]}`},
		{conversionActionsRemoveID, `{"customer_id":"` + clientID + `","conversion_action_ids":["5","5"]}`},
		{conversionActionsRemoveID, `{"customer_id":"` + clientID + `","conversion_action_ids":["abc"]}`},
		{conversionActionsRemoveID, `{"customer_id":"` + clientID + `","conversion_action_ids":["customers/` + otherID + `/conversionActions/5"]}`},
		{conversionActionsRemoveID, `{"customer_id":"` + clientID + `","conversion_action_ids":[]}`},
		{conversionActionsRemoveID, `{"customer_id":"` + clientID + `","conversion_action_ids":["1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21"]}`},
		{conversionActionsRemoveID, `{"customer_id":"` + clientID + `","conversion_action_ids":["5"],"partial_failure":true}`},
		{conversionActionsRemoveID, `{"customer_id":"` + clientID + `","conversion_action_ids":["5"],"login_customer_id":"` + loginID + `"}`},
		{conversionActionsRemoveID, `{"customer_id":"` + clientID + `","conversion_action_ids":["5"],"operations":[]}`},
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

func TestConversionActionCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+conversionPath(clientID)] = okReply(`{"results":[{"resourceName":"` + conversionName(clientID, "777") + `"}]}`)
	out, err := e.invokeConfirmed(conversionActionsCreateID, conversionCalls()[conversionActionsCreateID])
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	first := results[0].(map[string]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 1 ||
		first["conversion_action_id"] != "777" || first["resource_name"] != conversionName(clientID, "777") ||
		first["conversion_action"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+conversionPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body = %s, want the operations alone", p.body)
	}
	create := body["operations"].([]any)[0].(map[string]any)["create"].(map[string]any)
	value := create["valueSettings"].(map[string]any)
	if create["name"] != "Checkout" || create["type"] != "WEBPAGE" || create["category"] != "PURCHASE" ||
		create["countingType"] != "ONE_PER_CLICK" || create["clickThroughLookbackWindowDays"] != float64(30) ||
		create["viewThroughLookbackWindowDays"] != float64(1) || create["primaryForGoal"] != true ||
		create["phoneCallDurationSeconds"] != float64(60) || value["defaultValue"] != 12.5 ||
		value["defaultCurrencyCode"] != "EUR" || value["alwaysUseDefaultValue"] != false ||
		create["attributionModelSettings"].(map[string]any)["attributionModel"] != "GOOGLE_ADS_LAST_CLICK" {
		t.Errorf("create = %v", create)
	}
	for _, member := range []string{"resourceName", "appId", "includeInConversionsMetric", "firebaseSettings", "status"} {
		if _, found := create[member]; found {
			t.Errorf("create carries %s", member)
		}
	}
	if _, found := body["operations"].([]any)[0].(map[string]any)["updateMask"]; found {
		t.Error("a create carries an update mask")
	}
}

func TestConversionActionUpdateSendsTheMaskOfTheGivenSettings(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+conversionPath(clientID)] = okReply(`{"results":[{"resourceName":"` + conversionName(clientID, "5544332211") + `"}]}`)
	if _, err := e.invokeConfirmed(conversionActionsUpdateID, conversionCalls()[conversionActionsUpdateID]); err != nil {
		t.Fatal(err)
	}
	op := bodyOf(t, e.posts()[0])["operations"].([]any)[0].(map[string]any)
	update := op["update"].(map[string]any)
	if op["updateMask"] != "status,primaryForGoal,valueSettings.defaultValue" ||
		update["resourceName"] != conversionName(clientID, "5544332211") || update["primaryForGoal"] != false ||
		update["status"] != "HIDDEN" || update["valueSettings"].(map[string]any)["defaultValue"] != float64(3) ||
		len(update) != 4 {
		t.Errorf("operation = %v", op)
	}
}

func TestConversionActionRemoveSendsOneRemovePerID(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+conversionPath(clientID)] = okReply(`{"results":[{"resourceName":"` + conversionName(clientID, "5544332211") +
		`"},{"resourceName":"` + conversionName(clientID, "5544332212") + `"}]}`)
	out, err := e.invokeConfirmed(conversionActionsRemoveID, conversionCalls()[conversionActionsRemoveID])
	if err != nil {
		t.Fatal(err)
	}
	if len(decode(t, out)["results"].([]any)) != 2 {
		t.Errorf("out = %s", out)
	}
	operations := bodyOf(t, e.posts()[0])["operations"].([]any)
	if len(operations) != 2 || operations[0].(map[string]any)["remove"] != conversionName(clientID, "5544332211") ||
		operations[1].(map[string]any)["remove"] != conversionName(clientID, "5544332212") {
		t.Errorf("operations = %v", operations)
	}
}

func TestConversionActionValidateOnlyAnswersWithoutResults(t *testing.T) {
	for id, arguments := range conversionCalls() {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+conversionPath(clientID)] = okReply(`{}`)
			arguments = strings.TrimSuffix(arguments, "}") + `,"validate_only":true}`
			out, err := e.invokeConfirmed(id, arguments)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			// An empty member is left out of the result.
			if results, _ := got["results"].([]any); got["validate_only"] != true || len(results) != 0 {
				t.Errorf("out = %s", out)
			}
			if body := bodyOf(t, e.posts()[0]); body["validateOnly"] != true {
				t.Errorf("body = %v", body)
			}
		})
	}
}

func TestConversionActionMutableResourceIsMappedAndBound(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	name := conversionName(clientID, "777")
	e.fake.routes["POST "+conversionPath(clientID)] = okReply(`{"results":[{"resourceName":"` + name + `","conversionAction":{` +
		`"resourceName":"` + name + `","id":"777","name":"Checkout\u0007","status":"ENABLED","type":"WEBPAGE","primaryForGoal":true,` +
		`"category":"PURCHASE","clickThroughLookbackWindowDays":"30","valueSettings":{"defaultValue":12.5,"defaultCurrencyCode":"EUR"},` +
		`"attributionModelSettings":{"attributionModel":"GOOGLE_ADS_LAST_CLICK","dataDrivenModelStatus":"UNKNOWN"},` +
		`"tagSnippets":[{"type":"WEBPAGE","pageFormat":"HTML","eventSnippet":"` + strings.Repeat("x", 6000) + `"}],` +
		`"phoneCallDurationSeconds":"60"}}]}`)
	out, err := e.invokeConfirmed(conversionActionsCreateID, strings.TrimSuffix(conversionCalls()[conversionActionsCreateID], "}")+
		`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	action := decode(t, out)["results"].([]any)[0].(map[string]any)["conversion_action"].(map[string]any)
	snippet := action["tag_snippets"].([]any)[0].(map[string]any)["event_snippet"].(string)
	if action["id"] != "777" || action["name"] != "Checkout " || action["click_through_lookback_window_days"] != float64(30) ||
		action["value_settings"].(map[string]any)["default_value"] != 12.5 || action["phone_call_duration_seconds"] != float64(60) ||
		len(snippet) > maxLongText {
		t.Errorf("action = %v", action)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that does not fit the request is never taken for the change: the request was sent, so the error
// says that the outcome is unknown.
func TestConversionActionAnswersAreBoundToTheCheckedAccountAndRequest(t *testing.T) {
	good := conversionName(clientID, "5544332211")
	foreign := conversionName(otherID, "5544332211")
	cases := map[string]struct{ id, body string }{
		"foreign account": {conversionActionsUpdateID, `{"results":[{"resourceName":"` + foreign + `"}]}`},
		"other action":    {conversionActionsUpdateID, `{"results":[{"resourceName":"` + conversionName(clientID, "9") + `"}]}`},
		"no results":      {conversionActionsUpdateID, `{}`},
		"too many":        {conversionActionsUpdateID, `{"results":[{"resourceName":"` + good + `"},{"resourceName":"` + good + `"}]}`},
		"create foreign":  {conversionActionsCreateID, `{"results":[{"resourceName":"` + foreign + `"}]}`},
		"create no name":  {conversionActionsCreateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) + `/conversionActions/x"}]}`},
		"remove twice":    {conversionActionsRemoveID, `{"results":[{"resourceName":"` + good + `"},{"resourceName":"` + good + `"}]}`},
		"partial failure": {conversionActionsUpdateID, `{"results":[{"resourceName":"` + good + `"}],"partialFailureError":{"code":3}}`},
		"resource of another": {conversionActionsUpdateID, `{"results":[{"resourceName":"` + good + `","conversionAction":{"resourceName":"` +
			foreign + `"}}]}`},
		"id of another": {conversionActionsUpdateID, `{"results":[{"resourceName":"` + good + `","conversionAction":{"id":"1"}}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+conversionPath(clientID)] = okReply(c.body)
			arguments := conversionCalls()[c.id]
			if c.id == conversionActionsRemoveID {
				arguments = `{"customer_id":"` + clientID + `",` + removeArgs + `}`
			}
			if c.id == conversionActionsUpdateID {
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

func TestConversionActionErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		unknown bool
	}{
		{"conversion action error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "conversionActionError",
			"DUPLICATE_NAME", "")}, provider.ClassProviderError, false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, true},
		{"timeout", reply{err: errors.New("i/o timeout")}, provider.ClassUnreachable, true},
	}
	for _, c := range cases {
		for id, arguments := range conversionCalls() {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+conversionPath(clientID)] = c.reply
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

func TestConversionActionOnManagerAccountsNeedsAManagerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
	e.fake.routes["GET "+searchPath(clientID)] = rows()
	e.fake.routes["POST "+searchPath(clientID)] = rows(`{"customer":{"id":"` + clientID + `","manager":true}}`)
	_, err := e.invokeConfirmed(conversionActionsRemoveID, conversionCalls()[conversionActionsRemoveID])
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d, want invalid-request and no change", err, len(e.posts()))
	}
}

func TestConversionActionSummariesNameTheEffect(t *testing.T) {
	for id, arguments := range conversionCalls() {
		summary := conversionActionsSummary(id)(json.RawMessage(arguments))
		if !strings.Contains(summary, clientID) || !strings.Contains(summary, "conversion action") {
			t.Errorf("%s: summary %q", id, summary)
		}
	}
	if s := conversionActionsSummary(conversionActionsRemoveID)(json.RawMessage(`{"customer_id":"` + clientID +
		`","conversion_action_ids":["5"],"validate_only":true}`)); !strings.HasPrefix(s, "Only validate") ||
		!strings.Contains(s, "cannot be enabled again") {
		t.Errorf("summary = %q", s)
	}
}
