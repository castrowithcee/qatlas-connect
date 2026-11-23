package googleads

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var keywordPlansToolIDs = []string{keywordPlansCreateID, keywordPlansUpdateID, keywordPlansRemoveID}

func keywordPlansPath(id string) string { return "/v26/customers/" + id + "/keywordPlans:mutate" }

func keywordPlanResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/keywordPlans/" + id
}

func newKeywordPlansEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: keywordPlansToolIDs})
}

func keywordPlansCalls(customer string) map[string]string {
	return map[string]string{
		keywordPlansCreateID: `{"customer_id":"` + customer + `","keyword_plans":[` +
			`{"name":"Spring","forecast_period":{"date_interval":"NEXT_MONTH"}},` +
			`{"name":"Summer","forecast_period":{"date_range":{"start_date":"2030-06-01","end_date":"2030-08-31"}}}]}`,
		keywordPlansUpdateID: `{"customer_id":"` + customer + `","keyword_plans":[` +
			`{"keyword_plan_id":"111","name":"Autumn"},` +
			`{"keyword_plan_id":"222","name":"Winter","forecast_period":{"date_interval":"NEXT_WEEK"}}]}`,
		keywordPlansRemoveID: `{"customer_id":"` + customer + `","keyword_plan_ids":["111","222"]}`,
	}
}

func keywordPlansAnswer(customer string) string {
	return `{"results":[{"resourceName":"` + keywordPlanResource(customer, "111") + `"},{"resourceName":"` +
		keywordPlanResource(customer, "222") + `"}]}`
}

func TestKeywordPlanToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	effects := map[string]capability.Effect{keywordPlansCreateID: capability.EffectCreate,
		keywordPlansUpdateID: capability.EffectUpdate, keywordPlansRemoveID: capability.EffectDelete}
	for _, id := range keywordPlansToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != effects[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || d.RequiresToolAllowList ||
			!strings.HasSuffix(d.Description, mutateListNote) || !strings.Contains(d.Description, "Explorer") {
			t.Fatalf("%s = %+v", id, d)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		if got := metadata.Token.Scopes[id]; !reflect.DeepEqual(got, []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, got)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "update_mask",
			"response_content_type", "resource_name"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers %s", id, forbidden)
			}
		}
	}
}

func TestKeywordPlanToolsAreReachedByTheirEffectOrByTheToolsList(t *testing.T) {
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+keywordPlansPath(clientID)] = okReply(keywordPlansAnswer(clientID))
		_, err := e.invokeConfirmed(id, keywordPlansCalls(clientID)[id])
		return err == nil
	}
	for _, c := range []struct {
		id     string
		effect config.Effect
	}{{keywordPlansCreateID, config.EffectCreate}, {keywordPlansUpdateID, config.EffectUpdate},
		{keywordPlansRemoveID, config.EffectDelete}} {
		if !reached(config.Connection{Effects: []config.Effect{c.effect}}, c.id) ||
			reached(config.Connection{Effects: []config.Effect{config.EffectRead}}, c.id) ||
			!reached(config.Connection{Tools: []string{c.id}}, c.id) {
			t.Errorf("%s is not reached as described", c.id)
		}
	}
}

func TestKeywordPlansNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, id := range keywordPlansToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlansEnvironment(t, nil, nil)
			_, err := e.invoke(id, keywordPlansCalls(clientID)[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)

			e = newKeywordPlansEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err = e.invokeConfirmed(id, keywordPlansCalls(otherID)[id])
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestKeywordPlanArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	plans := func(items ...string) string { return head + `"keyword_plans":[` + strings.Join(items, ",") + `]}` }
	ids := func(items ...string) string { return head + `"keyword_plan_ids":[` + strings.Join(items, ",") + `]}` }
	var manyPlans, manyIDs []string
	for i := 1; i <= 51; i++ {
		manyPlans = append(manyPlans, `{"name":"a"}`)
		manyIDs = append(manyIDs, `"`+strconv.Itoa(i)+`"`)
	}
	period := func(p string) string { return `{"name":"a","forecast_period":` + p + `}` }
	edit := func(rest string) string { return `{"keyword_plan_id":"1"` + rest + `}` }
	bad := map[string][2]string{
		"create none":            {keywordPlansCreateID, plans()},
		"create too many":        {keywordPlansCreateID, plans(manyPlans...)},
		"create no name":         {keywordPlansCreateID, plans(`{}`)},
		"create empty name":      {keywordPlansCreateID, plans(`{"name":""}`)},
		"create name 256":        {keywordPlansCreateID, plans(`{"name":"` + strings.Repeat("a", 256) + `"}`)},
		"create line feed":       {keywordPlansCreateID, plans(`{"name":"a\nb"}`)},
		"create id":              {keywordPlansCreateID, plans(`{"name":"a","keyword_plan_id":"1"}`)},
		"create resource name":   {keywordPlansCreateID, plans(`{"name":"a","resource_name":"customers/1/keywordPlans/1"}`)},
		"create content type":    {keywordPlansCreateID, strings.TrimSuffix(plans(`{"name":"a"}`), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
		"period empty":           {keywordPlansCreateID, plans(period(`{}`))},
		"period both":            {keywordPlansCreateID, plans(period(`{"date_interval":"NEXT_WEEK","date_range":{"start_date":"2030-01-01","end_date":"2030-01-02"}}`))},
		"period interval":        {keywordPlansCreateID, plans(period(`{"date_interval":"UNKNOWN"}`))},
		"period interval lower":  {keywordPlansCreateID, plans(period(`{"date_interval":"next_week"}`))},
		"period no end":          {keywordPlansCreateID, plans(period(`{"date_range":{"start_date":"2030-01-01"}}`))},
		"period date format":     {keywordPlansCreateID, plans(period(`{"date_range":{"start_date":"2030-1-1","end_date":"2030-01-02"}}`))},
		"period date impossible": {keywordPlansCreateID, plans(period(`{"date_range":{"start_date":"2030-02-30","end_date":"2030-03-02"}}`))},
		"period reversed":        {keywordPlansCreateID, plans(period(`{"date_range":{"start_date":"2030-02-02","end_date":"2030-02-01"}}`))},
		"update none":            {keywordPlansUpdateID, plans()},
		"update too many":        {keywordPlansUpdateID, plans(manyPlans...)},
		"update no id":           {keywordPlansUpdateID, plans(`{"name":"a"}`)},
		"update zero":            {keywordPlansUpdateID, plans(`{"keyword_plan_id":"0","name":"a"}`)},
		"update name as id":      {keywordPlansUpdateID, plans(`{"keyword_plan_id":"customers/1/keywordPlans/2","name":"a"}`)},
		"update nothing":         {keywordPlansUpdateID, plans(edit(``))},
		"update empty name":      {keywordPlansUpdateID, plans(edit(`,"name":""`))},
		"update twice":           {keywordPlansUpdateID, plans(edit(`,"name":"a"`), edit(`,"name":"b"`))},
		"update period":          {keywordPlansUpdateID, plans(edit(`,"forecast_period":{}`))},
		"remove none":            {keywordPlansRemoveID, ids()},
		"remove too many":        {keywordPlansRemoveID, ids(manyIDs...)},
		"remove name":            {keywordPlansRemoveID, ids(`"customers/1/keywordPlans/2"`)},
		"remove negative":        {keywordPlansRemoveID, ids(`"-5"`)},
		"remove twice":           {keywordPlansRemoveID, ids(`"1"`, `"1"`)},
		"bad customer":           {keywordPlansRemoveID, `{"customer_id":"12-34","keyword_plan_ids":["1"]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlansEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newKeywordPlansEnvironment(t, nil, nil)
		e.fake.routes["POST "+keywordPlansPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
			keywordPlanResource(clientID, "5") + `"}]}`)
		if _, err := e.invokeConfirmed(keywordPlansCreateID, plans(`{"name":"`+strings.Repeat("ä", 255)+`"}`)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestKeywordPlansSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		keywordPlansCreateID: `[{"create":{"name":"Spring","forecastPeriod":{"dateInterval":"NEXT_MONTH"}}},` +
			`{"create":{"name":"Summer","forecastPeriod":{"dateRange":{"startDate":"2030-06-01","endDate":"2030-08-31"}}}}]`,
		keywordPlansUpdateID: `[{"update":{"resourceName":"customers/1000000003/keywordPlans/111","name":"Autumn"},"updateMask":"name"},` +
			`{"update":{"resourceName":"customers/1000000003/keywordPlans/222","name":"Winter","forecastPeriod":{"dateInterval":"NEXT_WEEK"}},"updateMask":"name,forecast_period"}]`,
		keywordPlansRemoveID: `[{"remove":"customers/1000000003/keywordPlans/111"},{"remove":"customers/1000000003/keywordPlans/222"}]`,
	}
	for _, id := range keywordPlansToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlansEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+keywordPlansPath(clientID)] = okReply(keywordPlansAnswer(clientID))
			out, err := e.invokeConfirmed(id, keywordPlansCalls(clientID)[id])
			if err != nil {
				t.Fatal(err)
			}
			posts := e.posts()
			if len(posts) != 1 || posts[0].login != loginID {
				t.Fatalf("posts = %+v", posts)
			}
			body := bodyOf(t, posts[0])
			if len(body) != 1 || !reflect.DeepEqual(body["operations"], wantWire(t, want[id])) {
				t.Errorf("body = %s", posts[0].body)
			}
			got := decode(t, out)
			results := got["results"].([]any)
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
				results[0].(map[string]any)["keyword_plan_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != keywordPlanResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
		t.Run(id+" validate only", func(t *testing.T) {
			e := newKeywordPlansEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlansPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(keywordPlansCalls(clientID)[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); bodyOf(t, e.posts()[0])["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestKeywordPlanResultsAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(id string) string { return `{"resourceName":"` + keywordPlanResource(clientID, id) + `"}` }
	foreign := `{"resourceName":"` + keywordPlanResource(otherID, "111") + `"}`
	cases := map[string]struct{ id, body string }{
		"create foreign":    {keywordPlansCreateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"create none":       {keywordPlansCreateID, `{}`},
		"create one":        {keywordPlansCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":      {keywordPlansCreateID, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create not a plan": {keywordPlansCreateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) + `/labels/1"},` + own("222") + `]}`},
		"create partial": {keywordPlansCreateID, `{"results":[` + own("111") + `,` + own("222") +
			`],"partialFailureError":{"code":3}}`},
		"update other order": {keywordPlansUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":     {keywordPlansUpdateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"remove other order": {keywordPlansRemoveID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"remove foreign":     {keywordPlansRemoveID, `{"results":[` + foreign + `,` + own("222") + `]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlansEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlansPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, keywordPlansCalls(clientID)[c.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestKeywordPlanManagerAccountsNeedAManagerTarget(t *testing.T) {
	call := `{"customer_id":"` + managerID + `","keyword_plan_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + keywordPlanResource(managerID, "7") + `"}]}`)
	e := newKeywordPlansEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlansPath(managerID)] = answer
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	if _, err := e.invokeConfirmed(keywordPlansRemoveID, call); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
	e = newKeywordPlansEnvironment(t, []string{"managers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlansPath(managerID)] = answer
	if _, err := e.invokeConfirmed(keywordPlansRemoveID, call); err != nil {
		t.Fatal(err)
	}
}

func TestKeywordPlanErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"plan error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "keywordPlanError",
			"INVALID_NAME", "")}, provider.ClassProviderError,
			"rejected the change (keywordPlanError INVALID_NAME), nothing was changed"},
		{"limit", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "resourceCountLimitExceededError",
			"ACCOUNT_LIMIT", "")}, provider.ClassProviderError,
			"rejected the change (resourceCountLimitExceededError ACCOUNT_LIMIT), nothing was changed"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown"},
	}
	for _, k := range cases {
		for _, id := range keywordPlansToolIDs {
			t.Run(k.name+" "+id, func(t *testing.T) {
				e := newKeywordPlansEnvironment(t, nil, nil)
				e.fake.routes["POST "+keywordPlansPath(clientID)] = k.reply
				_, err := e.invokeConfirmed(id, keywordPlansCalls(clientID)[id])
				if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) || len(e.posts()) != 1 ||
					len(e.fake.api()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestKeywordPlanSummariesNameTheEffect(t *testing.T) {
	calls := keywordPlansCalls(clientID)
	create := keywordPlansCreateSummary(json.RawMessage(calls[keywordPlansCreateID]))
	update := keywordPlansUpdateSummary(json.RawMessage(calls[keywordPlansUpdateID]))
	remove := keywordPlansRemoveSummary(json.RawMessage(calls[keywordPlansRemoveID]))
	for _, word := range []string{clientID, "2 keyword plan", `"Spring" forecasting NEXT_MONTH`, "2030-06-01 to 2030-08-31"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	for _, word := range []string{clientID, `keyword plan 111 name "Autumn"`, "forecasting NEXT_WEEK"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	for _, word := range []string{clientID, "111, 222", "No tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	long := keywordPlansCreateSummary(json.RawMessage(`{"customer_id":"` + clientID + `","keyword_plans":[{"name":"` +
		strings.Repeat("a", 5000) + `\n"}]}`))
	if len(long) > 2000 || strings.Contains(long, "\n") {
		t.Errorf("summary is not cut: %d characters", len(long))
	}
	if text := keywordPlansCreateSummary(json.RawMessage(strings.TrimSuffix(calls[keywordPlansCreateID], "}") +
		`,"validate_only":true}`)); !strings.HasPrefix(text, "Only validate") {
		t.Errorf("summary %q", text)
	}
}
