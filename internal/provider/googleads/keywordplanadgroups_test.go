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

var keywordPlanAdGroupsToolIDs = []string{keywordPlanAdGroupsCreateID, keywordPlanAdGroupsUpdateID,
	keywordPlanAdGroupsRemoveID}

func keywordPlanAdGroupsPath(id string) string {
	return "/v26/customers/" + id + "/keywordPlanAdGroups:mutate"
}

func keywordPlanAdGroupResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/keywordPlanAdGroups/" + id
}

func newKeywordPlanAdGroupsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: keywordPlanAdGroupsToolIDs})
}

func keywordPlanAdGroupsCalls(customer string) map[string]string {
	return map[string]string{
		keywordPlanAdGroupsCreateID: `{"customer_id":"` + customer + `","keyword_plan_ad_groups":[` +
			`{"keyword_plan_campaign_id":"11","name":"Shoes","cpc_bid_micros":1500000},` +
			`{"keyword_plan_campaign_id":"11","name":"Boots"}]}`,
		keywordPlanAdGroupsUpdateID: `{"customer_id":"` + customer + `","keyword_plan_ad_groups":[` +
			`{"keyword_plan_ad_group_id":"111","name":"Sandals"},` +
			`{"keyword_plan_ad_group_id":"222","name":"Socks","cpc_bid_micros":9007199254740993}]}`,
		keywordPlanAdGroupsRemoveID: `{"customer_id":"` + customer + `","keyword_plan_ad_group_ids":["111","222"]}`,
	}
}

func keywordPlanAdGroupsAnswer(customer string) string {
	return `{"results":[{"resourceName":"` + keywordPlanAdGroupResource(customer, "111") + `"},{"resourceName":"` +
		keywordPlanAdGroupResource(customer, "222") + `"}]}`
}

func TestKeywordPlanAdGroupToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	effects := map[string]capability.Effect{keywordPlanAdGroupsCreateID: capability.EffectCreate,
		keywordPlanAdGroupsUpdateID: capability.EffectUpdate, keywordPlanAdGroupsRemoveID: capability.EffectDelete}
	for _, id := range keywordPlanAdGroupsToolIDs {
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
			"response_content_type", "resource_name", "keyword_plan_campaign"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers %s", id, forbidden)
			}
		}
	}
}

func TestKeywordPlanAdGroupToolsAreReachedByTheirEffectOrByTheToolsList(t *testing.T) {
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+keywordPlanAdGroupsPath(clientID)] = okReply(keywordPlanAdGroupsAnswer(clientID))
		_, err := e.invokeConfirmed(id, keywordPlanAdGroupsCalls(clientID)[id])
		return err == nil
	}
	for _, c := range []struct {
		id     string
		effect config.Effect
	}{{keywordPlanAdGroupsCreateID, config.EffectCreate}, {keywordPlanAdGroupsUpdateID, config.EffectUpdate},
		{keywordPlanAdGroupsRemoveID, config.EffectDelete}} {
		if !reached(config.Connection{Effects: []config.Effect{c.effect}}, c.id) ||
			reached(config.Connection{Effects: []config.Effect{config.EffectRead}}, c.id) ||
			!reached(config.Connection{Tools: []string{c.id}}, c.id) {
			t.Errorf("%s is not reached as described", c.id)
		}
	}
}

func TestKeywordPlanAdGroupsNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, id := range keywordPlanAdGroupsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlanAdGroupsEnvironment(t, nil, nil)
			_, err := e.invoke(id, keywordPlanAdGroupsCalls(clientID)[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)

			e = newKeywordPlanAdGroupsEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err = e.invokeConfirmed(id, keywordPlanAdGroupsCalls(otherID)[id])
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestKeywordPlanAdGroupArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	list := func(items ...string) string {
		return head + `"keyword_plan_ad_groups":[` + strings.Join(items, ",") + `]}`
	}
	ids := func(items ...string) string {
		return head + `"keyword_plan_ad_group_ids":[` + strings.Join(items, ",") + `]}`
	}
	var manyNew, manyIDs []string
	for i := 1; i <= 51; i++ {
		manyNew = append(manyNew, `{"keyword_plan_campaign_id":"`+strconv.Itoa(i)+`","name":"a"}`)
		manyIDs = append(manyIDs, `"`+strconv.Itoa(i)+`"`)
	}
	create := func(rest string) string { return `{"keyword_plan_campaign_id":"1","name":"a"` + rest + `}` }
	edit := func(rest string) string { return `{"keyword_plan_ad_group_id":"1"` + rest + `}` }
	bad := map[string][2]string{
		"create none":        {keywordPlanAdGroupsCreateID, list()},
		"create too many":    {keywordPlanAdGroupsCreateID, list(manyNew...)},
		"create no campaign": {keywordPlanAdGroupsCreateID, list(`{"name":"a"}`)},
		"create campaign name": {keywordPlanAdGroupsCreateID,
			list(`{"keyword_plan_campaign_id":"customers/1/keywordPlanCampaigns/2","name":"a"}`)},
		"create campaign zero": {keywordPlanAdGroupsCreateID, list(`{"keyword_plan_campaign_id":"0","name":"a"}`)},
		"create no name":       {keywordPlanAdGroupsCreateID, list(`{"keyword_plan_campaign_id":"1"}`)},
		"create empty name":    {keywordPlanAdGroupsCreateID, list(create(`,"name":""`))},
		"create line feed":     {keywordPlanAdGroupsCreateID, list(create(`,"name":"a\nb"`))},
		"create zero bid":      {keywordPlanAdGroupsCreateID, list(create(`,"cpc_bid_micros":0`))},
		"create fractional":    {keywordPlanAdGroupsCreateID, list(create(`,"cpc_bid_micros":1.5`))},
		"create negative":      {keywordPlanAdGroupsCreateID, list(create(`,"cpc_bid_micros":-1`))},
		"create text bid":      {keywordPlanAdGroupsCreateID, list(create(`,"cpc_bid_micros":"5"`))},
		"create huge bid":      {keywordPlanAdGroupsCreateID, list(create(`,"cpc_bid_micros":99999999999999999999`))},
		"create resource":      {keywordPlanAdGroupsCreateID, list(create(`,"resource_name":"customers/1/keywordPlanAdGroups/1"`))},
		"create content type": {keywordPlanAdGroupsCreateID,
			strings.TrimSuffix(list(create(``)), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
		"update none":       {keywordPlanAdGroupsUpdateID, list()},
		"update too many":   {keywordPlanAdGroupsUpdateID, list(manyNew...)},
		"update no id":      {keywordPlanAdGroupsUpdateID, list(`{"name":"a"}`)},
		"update name as id": {keywordPlanAdGroupsUpdateID, list(`{"keyword_plan_ad_group_id":"customers/1/keywordPlanAdGroups/2","name":"a"}`)},
		"update nothing":    {keywordPlanAdGroupsUpdateID, list(edit(``))},
		"update empty name": {keywordPlanAdGroupsUpdateID, list(edit(`,"name":""`))},
		"update bid":        {keywordPlanAdGroupsUpdateID, list(edit(`,"cpc_bid_micros":0`))},
		"update campaign":   {keywordPlanAdGroupsUpdateID, list(edit(`,"keyword_plan_campaign_id":"2"`))},
		"update twice":      {keywordPlanAdGroupsUpdateID, list(edit(`,"name":"a"`), edit(`,"name":"b"`))},
		"remove none":       {keywordPlanAdGroupsRemoveID, ids()},
		"remove too many":   {keywordPlanAdGroupsRemoveID, ids(manyIDs...)},
		"remove name":       {keywordPlanAdGroupsRemoveID, ids(`"customers/1/keywordPlanAdGroups/2"`)},
		"remove negative":   {keywordPlanAdGroupsRemoveID, ids(`"-5"`)},
		"remove twice":      {keywordPlanAdGroupsRemoveID, ids(`"1"`, `"1"`)},
		"bad customer":      {keywordPlanAdGroupsRemoveID, `{"customer_id":"12-34","keyword_plan_ad_group_ids":["1"]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlanAdGroupsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newKeywordPlanAdGroupsEnvironment(t, nil, nil)
		e.fake.routes["POST "+keywordPlanAdGroupsPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
			keywordPlanAdGroupResource(clientID, "5") + `"}]}`)
		call := list(create(`,"name":"` + strings.Repeat("ä", 255) + `"`))
		if _, err := e.invokeConfirmed(keywordPlanAdGroupsCreateID, call); err != nil {
			t.Fatal(err)
		}
	})
}

func TestKeywordPlanAdGroupsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		keywordPlanAdGroupsCreateID: `[{"create":{"keywordPlanCampaign":"customers/1000000003/keywordPlanCampaigns/11",` +
			`"name":"Shoes","cpcBidMicros":"1500000"}},` +
			`{"create":{"keywordPlanCampaign":"customers/1000000003/keywordPlanCampaigns/11","name":"Boots"}}]`,
		keywordPlanAdGroupsUpdateID: `[{"update":{"resourceName":"customers/1000000003/keywordPlanAdGroups/111","name":"Sandals"},"updateMask":"name"},` +
			`{"update":{"resourceName":"customers/1000000003/keywordPlanAdGroups/222","name":"Socks","cpcBidMicros":"9007199254740993"},` +
			`"updateMask":"name,cpc_bid_micros"}]`,
		keywordPlanAdGroupsRemoveID: `[{"remove":"customers/1000000003/keywordPlanAdGroups/111"},{"remove":"customers/1000000003/keywordPlanAdGroups/222"}]`,
	}
	for _, id := range keywordPlanAdGroupsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlanAdGroupsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+keywordPlanAdGroupsPath(clientID)] = okReply(keywordPlanAdGroupsAnswer(clientID))
			out, err := e.invokeConfirmed(id, keywordPlanAdGroupsCalls(clientID)[id])
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
				results[0].(map[string]any)["keyword_plan_ad_group_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != keywordPlanAdGroupResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
		t.Run(id+" validate only", func(t *testing.T) {
			e := newKeywordPlanAdGroupsEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlanAdGroupsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(keywordPlanAdGroupsCalls(clientID)[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); bodyOf(t, e.posts()[0])["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestKeywordPlanAdGroupResultsAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(id string) string { return `{"resourceName":"` + keywordPlanAdGroupResource(clientID, id) + `"}` }
	foreign := `{"resourceName":"` + keywordPlanAdGroupResource(otherID, "111") + `"}`
	cases := map[string]struct{ id, body string }{
		"create foreign": {keywordPlanAdGroupsCreateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"create none":    {keywordPlanAdGroupsCreateID, `{}`},
		"create one":     {keywordPlanAdGroupsCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":   {keywordPlanAdGroupsCreateID, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create not an ad group": {keywordPlanAdGroupsCreateID, `{"results":[{"resourceName":"customers/` +
			queryNumber(clientID) + `/keywordPlanCampaigns/1"},` + own("222") + `]}`},
		"create partial": {keywordPlanAdGroupsCreateID, `{"results":[` + own("111") + `,` + own("222") +
			`],"partialFailureError":{"code":3}}`},
		"update other order": {keywordPlanAdGroupsUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":     {keywordPlanAdGroupsUpdateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"remove other order": {keywordPlanAdGroupsRemoveID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"remove foreign":     {keywordPlanAdGroupsRemoveID, `{"results":[` + foreign + `,` + own("222") + `]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlanAdGroupsEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlanAdGroupsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, keywordPlanAdGroupsCalls(clientID)[c.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestKeywordPlanAdGroupManagerAccountsNeedAManagerTarget(t *testing.T) {
	call := `{"customer_id":"` + managerID + `","keyword_plan_ad_group_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + keywordPlanAdGroupResource(managerID, "7") + `"}]}`)
	e := newKeywordPlanAdGroupsEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlanAdGroupsPath(managerID)] = answer
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	if _, err := e.invokeConfirmed(keywordPlanAdGroupsRemoveID, call); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
	e = newKeywordPlanAdGroupsEnvironment(t, []string{"managers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlanAdGroupsPath(managerID)] = answer
	if _, err := e.invokeConfirmed(keywordPlanAdGroupsRemoveID, call); err != nil {
		t.Fatal(err)
	}
}

func TestKeywordPlanAdGroupErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"ad group error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "keywordPlanAdGroupError",
			"INVALID_ADGROUP_NAME", "")}, provider.ClassProviderError,
			"rejected the change (keywordPlanAdGroupError INVALID_ADGROUP_NAME), nothing was changed"},
		{"limit", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "resourceCountLimitExceededError",
			"ACCOUNT_LIMIT", "")}, provider.ClassProviderError,
			"rejected the change (resourceCountLimitExceededError ACCOUNT_LIMIT), nothing was changed"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown"},
	}
	for _, k := range cases {
		for _, id := range keywordPlanAdGroupsToolIDs {
			t.Run(k.name+" "+id, func(t *testing.T) {
				e := newKeywordPlanAdGroupsEnvironment(t, nil, nil)
				e.fake.routes["POST "+keywordPlanAdGroupsPath(clientID)] = k.reply
				_, err := e.invokeConfirmed(id, keywordPlanAdGroupsCalls(clientID)[id])
				if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) || len(e.posts()) != 1 ||
					len(e.fake.api()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestKeywordPlanAdGroupSummariesNameTheEffect(t *testing.T) {
	calls := keywordPlanAdGroupsCalls(clientID)
	create := keywordPlanAdGroupsCreateSummary(json.RawMessage(calls[keywordPlanAdGroupsCreateID]))
	update := keywordPlanAdGroupsUpdateSummary(json.RawMessage(calls[keywordPlanAdGroupsUpdateID]))
	remove := keywordPlanAdGroupsRemoveSummary(json.RawMessage(calls[keywordPlanAdGroupsRemoveID]))
	for _, word := range []string{clientID, "2 keyword plan ad group", `"Shoes" in keyword plan campaign 11 bid 1500000 micros`,
		`"Boots" in keyword plan campaign 11`} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	for _, word := range []string{clientID, `keyword plan ad group 111 name "Sandals"`, "bid 9007199254740993 micros"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	for _, word := range []string{clientID, "111, 222", "No tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	long := keywordPlanAdGroupsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID + `","keyword_plan_ad_groups":[{"name":"` +
		strings.Repeat("a", 5000) + `\n","keyword_plan_campaign_id":"1"}]}`))
	if len(long) > 2000 || strings.Contains(long, "\n") {
		t.Errorf("summary is not cut: %d characters", len(long))
	}
	if text := keywordPlanAdGroupsRemoveSummary(json.RawMessage(strings.TrimSuffix(calls[keywordPlanAdGroupsRemoveID], "}") +
		`,"validate_only":true}`)); !strings.HasPrefix(text, "Only validate") {
		t.Errorf("summary %q", text)
	}
}
