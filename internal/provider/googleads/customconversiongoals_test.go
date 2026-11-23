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

func goalPath(id string) string { return "/v26/customers/" + id + "/customConversionGoals:mutate" }

func goalName(id, goal string) string {
	return "customers/" + queryNumber(id) + "/customConversionGoals/" + goal
}

var goalToolIDs = []string{customConversionGoalsCreateID, customConversionGoalsUpdateID, customConversionGoalsRemoveID}

func goalAction(id, n string) string { return `"` + conversionName(id, n) + `"` }

func goalCalls() map[string]string {
	actions := goalAction(clientID, "5544332211") + "," + goalAction(clientID, "5544332212")
	return map[string]string{
		customConversionGoalsCreateID: `{"customer_id":"` + clientID + `","custom_conversion_goals":[{"name":"Leads",` +
			`"conversion_actions":[` + actions + `]}]}`,
		customConversionGoalsUpdateID: `{"customer_id":"` + clientID + `","custom_conversion_goals":[` +
			`{"custom_conversion_goal_id":"7788990011","name":"Qualified","conversion_actions":[` + actions + `]}]}`,
		customConversionGoalsRemoveID: `{"customer_id":"` + clientID + `","custom_conversion_goal_ids":["7788990011","7788990012"]}`,
	}
}

func TestCustomConversionGoalToolsAreDescribed(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	for _, id := range goalToolIDs {
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
		for _, banned := range []string{"partial_failure", "status", "resource_name", "login_customer_id"} {
			if strings.Contains(string(d.InputSchema), banned) {
				t.Errorf("%s offers %s", id, banned)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s has no validate_only", id)
		}
		_, content := input.Properties["response_content_type"]
		if content == (id == customConversionGoalsRemoveID) {
			t.Errorf("%s: response_content_type offered = %v", id, content)
		}
		if len(d.OutputSchema) == 0 || !d.RequiresToolAllowList || !strings.Contains(d.Description, "10") {
			t.Errorf("%s: output schema, tools list or bound missing", id)
		}
	}
}

func TestCustomConversionGoalChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
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
		t.Run(id+" over the read effect", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" target", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
			foreign := strings.ReplaceAll(arguments, clientID, otherID)
			_, err := e.invokeConfirmed(id, foreign)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCustomConversionGoalArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	own := goalAction(clientID, "5")
	create := func(item string) string {
		return `{"customer_id":"` + clientID + `","custom_conversion_goals":[` + item + `]}`
	}
	cases := []struct{ id, arguments string }{
		{customConversionGoalsCreateID, create(`{"conversion_actions":[` + own + `]}`)},
		{customConversionGoalsCreateID, create(`{"name":"A"}`)},
		{customConversionGoalsCreateID, create(`{"name":"A","conversion_actions":[]}`)},
		{customConversionGoalsCreateID, create(`{"name":"  ","conversion_actions":[` + own + `]}`)},
		{customConversionGoalsCreateID, create(`{"name":"` + strings.Repeat("n", 201) + `","conversion_actions":[` + own + `]}`)},
		{customConversionGoalsCreateID, create(`{"name":"A","conversion_actions":[` + goalAction(otherID, "5") + `]}`)},
		{customConversionGoalsCreateID, create(`{"name":"A","conversion_actions":["conversionActions/5"]}`)},
		{customConversionGoalsCreateID, create(`{"name":"A","conversion_actions":["customers/` + queryNumber(clientID) + `/remarketingActions/5"]}`)},
		{customConversionGoalsCreateID, create(`{"name":"A","conversion_actions":[` + own + `,` + own + `]}`)},
		{customConversionGoalsCreateID, create(`{"name":"A","conversion_actions":[` + own + `],"status":"ENABLED"}`)},
		{customConversionGoalsCreateID, create(`{"name":"A","conversion_actions":[` + own + `],"custom_conversion_goal_id":"1"}`)},
		{customConversionGoalsCreateID, strings.TrimSuffix(create(`{"name":"A","conversion_actions":[`+own+`]}`), "}") +
			`,"response_content_type":"ALL"}`},
		{customConversionGoalsCreateID, strings.TrimSuffix(create(`{"name":"A","conversion_actions":[`+own+`]}`), "}") +
			`,"partial_failure":true}`},
		{customConversionGoalsCreateID, create(strings.TrimSuffix(strings.Repeat(`{"name":"A","conversion_actions":[`+own+`]},`, 11), ","))},
		{customConversionGoalsCreateID, `{"customer_id":"123","custom_conversion_goals":[{"name":"A","conversion_actions":[` + own + `]}]}`},
		{customConversionGoalsUpdateID, create(`{"custom_conversion_goal_id":"5"}`)},
		{customConversionGoalsUpdateID, create(`{"name":"B"}`)},
		{customConversionGoalsUpdateID, create(`{"custom_conversion_goal_id":"abc","name":"B"}`)},
		{customConversionGoalsUpdateID, create(`{"custom_conversion_goal_id":"5","conversion_actions":[]}`)},
		{customConversionGoalsUpdateID, create(`{"custom_conversion_goal_id":"5","name":"B"},{"custom_conversion_goal_id":"5","name":"C"}`)},
		{customConversionGoalsUpdateID, create(`{"custom_conversion_goal_id":"5","status":"REMOVED"}`)},
		{customConversionGoalsRemoveID, `{"customer_id":"` + clientID + `","custom_conversion_goal_ids":["5","5"]}`},
		{customConversionGoalsRemoveID, `{"customer_id":"` + clientID + `","custom_conversion_goal_ids":["customers/` + otherID + `/customConversionGoals/5"]}`},
		{customConversionGoalsRemoveID, `{"customer_id":"` + clientID + `","custom_conversion_goal_ids":[]}`},
		{customConversionGoalsRemoveID, `{"customer_id":"` + clientID + `","custom_conversion_goal_ids":["5"],"response_content_type":"MUTABLE_RESOURCE"}`},
		{customConversionGoalsRemoveID, `{"customer_id":"` + clientID + `","custom_conversion_goal_ids":["5"],"partial_failure":true}`},
		{customConversionGoalsRemoveID, `{"customer_id":"` + clientID + `","custom_conversion_goal_ids":["5"],"login_customer_id":"` + loginID + `"}`},
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

func TestCustomConversionGoalCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+goalPath(clientID)] = okReply(`{"results":[{"resourceName":"` + goalName(clientID, "777") + `"}]}`)
	out, err := e.invokeConfirmed(customConversionGoalsCreateID, goalCalls()[customConversionGoalsCreateID])
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	first := got["results"].([]any)[0].(map[string]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || first["custom_conversion_goal_id"] != "777" ||
		first["resource_name"] != goalName(clientID, "777") || first["custom_conversion_goal"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+goalPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body = %s, want the operations alone", p.body)
	}
	op := body["operations"].([]any)[0].(map[string]any)
	create := op["create"].(map[string]any)
	actions := create["conversionActions"].([]any)
	if create["name"] != "Leads" || len(actions) != 2 || actions[0] != conversionName(clientID, "5544332211") ||
		len(create) != 2 || op["updateMask"] != nil {
		t.Errorf("operation = %v", op)
	}
}

func TestCustomConversionGoalUpdateSendsTheMaskOfTheGivenSettings(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+goalPath(clientID)] = okReply(`{"results":[{"resourceName":"` + goalName(clientID, "7788990011") + `"}]}`)
	if _, err := e.invokeConfirmed(customConversionGoalsUpdateID, goalCalls()[customConversionGoalsUpdateID]); err != nil {
		t.Fatal(err)
	}
	op := bodyOf(t, e.posts()[0])["operations"].([]any)[0].(map[string]any)
	update := op["update"].(map[string]any)
	if op["updateMask"] != "name,conversionActions" || update["resourceName"] != goalName(clientID, "7788990011") ||
		update["name"] != "Qualified" || len(update) != 3 {
		t.Errorf("operation = %v", op)
	}
	name := `{"customer_id":"` + clientID + `","custom_conversion_goals":[{"custom_conversion_goal_id":"7788990011","name":"Only"}]}`
	if _, err := e.invokeConfirmed(customConversionGoalsUpdateID, name); err != nil {
		t.Fatal(err)
	}
	if mask := bodyOf(t, e.posts()[1])["operations"].([]any)[0].(map[string]any)["updateMask"]; mask != "name" {
		t.Errorf("mask = %v", mask)
	}
}

func TestCustomConversionGoalRemoveSendsOneRemovePerID(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+goalPath(clientID)] = okReply(`{"results":[{"resourceName":"` + goalName(clientID, "7788990011") +
		`"},{"resourceName":"` + goalName(clientID, "7788990012") + `"}]}`)
	out, err := e.invokeConfirmed(customConversionGoalsRemoveID, goalCalls()[customConversionGoalsRemoveID])
	if err != nil {
		t.Fatal(err)
	}
	if len(decode(t, out)["results"].([]any)) != 2 {
		t.Errorf("out = %s", out)
	}
	operations := bodyOf(t, e.posts()[0])["operations"].([]any)
	if len(operations) != 2 || operations[0].(map[string]any)["remove"] != goalName(clientID, "7788990011") ||
		operations[1].(map[string]any)["remove"] != goalName(clientID, "7788990012") {
		t.Errorf("operations = %v", operations)
	}
}

func TestCustomConversionGoalValidateOnlyAnswersWithoutResults(t *testing.T) {
	for id, arguments := range goalCalls() {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+goalPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if results, _ := got["results"].([]any); got["validate_only"] != true || len(results) != 0 {
				t.Errorf("out = %s", out)
			}
			if body := bodyOf(t, e.posts()[0]); body["validateOnly"] != true {
				t.Errorf("body = %v", body)
			}
		})
	}
}

func TestCustomConversionGoalMutableResourceIsMappedAndBound(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	name := goalName(clientID, "777")
	e.fake.routes["POST "+goalPath(clientID)] = okReply(`{"results":[{"resourceName":"` + name + `","customConversionGoal":{` +
		`"resourceName":"` + name + `","id":"777","name":"Leads\u0007","status":"ENABLED","conversionActions":["` +
		conversionName(clientID, "5") + `"]}}]}`)
	out, err := e.invokeConfirmed(customConversionGoalsCreateID, strings.TrimSuffix(goalCalls()[customConversionGoalsCreateID], "}")+
		`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	goal := decode(t, out)["results"].([]any)[0].(map[string]any)["custom_conversion_goal"].(map[string]any)
	if goal["id"] != "777" || goal["name"] != "Leads " || goal["status"] != "ENABLED" ||
		len(goal["conversion_actions"].([]any)) != 1 {
		t.Errorf("goal = %v", goal)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that does not fit the request is never taken for the change: the request was sent, so the error
// says that the outcome is unknown.
func TestCustomConversionGoalAnswersAreBoundToTheCheckedAccountAndRequest(t *testing.T) {
	good := goalName(clientID, "7788990011")
	foreign := goalName(otherID, "7788990011")
	cases := map[string]struct{ id, body string }{
		"foreign account": {customConversionGoalsUpdateID, `{"results":[{"resourceName":"` + foreign + `"}]}`},
		"other goal":      {customConversionGoalsUpdateID, `{"results":[{"resourceName":"` + goalName(clientID, "9") + `"}]}`},
		"no results":      {customConversionGoalsUpdateID, `{}`},
		"too many":        {customConversionGoalsUpdateID, `{"results":[{"resourceName":"` + good + `"},{"resourceName":"` + good + `"}]}`},
		"create foreign":  {customConversionGoalsCreateID, `{"results":[{"resourceName":"` + foreign + `"}]}`},
		"create no id":    {customConversionGoalsCreateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) + `/customConversionGoals/x"}]}`},
		"remove twice":    {customConversionGoalsRemoveID, `{"results":[{"resourceName":"` + good + `"},{"resourceName":"` + good + `"}]}`},
		"resource of another": {customConversionGoalsUpdateID, `{"results":[{"resourceName":"` + good +
			`","customConversionGoal":{"resourceName":"` + foreign + `"}}]}`},
		"id of another": {customConversionGoalsUpdateID, `{"results":[{"resourceName":"` + good + `","customConversionGoal":{"id":"1"}}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+goalPath(clientID)] = okReply(c.body)
			arguments := goalCalls()[c.id]
			if c.id == customConversionGoalsUpdateID {
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

func TestCustomConversionGoalErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		unknown bool
	}{
		{"goal error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "customConversionGoalError",
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
		for id, arguments := range goalCalls() {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+goalPath(clientID)] = c.reply
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

func TestCustomConversionGoalOnManagerAccountsNeedsAManagerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
	e.fake.routes["GET "+searchPath(clientID)] = rows()
	e.fake.routes["POST "+searchPath(clientID)] = rows(`{"customer":{"id":"` + clientID + `","manager":true}}`)
	_, err := e.invokeConfirmed(customConversionGoalsRemoveID, goalCalls()[customConversionGoalsRemoveID])
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d, want invalid-request and no change", err, len(e.posts()))
	}
}

func TestCustomConversionGoalSummariesNameTheEffect(t *testing.T) {
	for id, arguments := range goalCalls() {
		summary := customGoalsSummary(id)(json.RawMessage(arguments))
		if !strings.Contains(summary, clientID) || !strings.Contains(summary, "custom conversion goal") {
			t.Errorf("%s: summary %q", id, summary)
		}
	}
	if s := customGoalsSummary(customConversionGoalsRemoveID)(json.RawMessage(`{"customer_id":"` + clientID +
		`","custom_conversion_goal_ids":["5"],"validate_only":true}`)); !strings.HasPrefix(s, "Only validate") ||
		!strings.Contains(s, "bidding signal") {
		t.Errorf("summary = %q", s)
	}
}
