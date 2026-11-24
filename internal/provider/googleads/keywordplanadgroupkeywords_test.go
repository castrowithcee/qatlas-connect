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

var keywordPlanAdGroupKeywordsToolIDs = []string{keywordPlanAdGroupKeywordsCreateID, keywordPlanAdGroupKeywordsUpdateID,
	keywordPlanAdGroupKeywordsRemoveID}

func keywordPlanAdGroupKeywordsPath(id string) string {
	return "/v26/customers/" + id + "/keywordPlanAdGroupKeywords:mutate"
}

func keywordPlanAdGroupKeywordResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/keywordPlanAdGroupKeywords/" + id
}

func newKeywordPlanAdGroupKeywordsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: keywordPlanAdGroupKeywordsToolIDs})
}

func keywordPlanAdGroupKeywordsCalls(customer string) map[string]string {
	return map[string]string{
		keywordPlanAdGroupKeywordsCreateID: `{"customer_id":"` + customer + `","keyword_plan_ad_group_keywords":[` +
			`{"keyword_plan_ad_group_id":"11","text":"running shoes","match_type":"EXACT","cpc_bid_micros":1500000},` +
			`{"keyword_plan_ad_group_id":"11","text":"free","match_type":"BROAD","negative":true}]}`,
		keywordPlanAdGroupKeywordsUpdateID: `{"customer_id":"` + customer + `","keyword_plan_ad_group_keywords":[` +
			`{"keyword_plan_ad_group_keyword_id":"111","text":"trail shoes"},` +
			`{"keyword_plan_ad_group_keyword_id":"222","match_type":"PHRASE","cpc_bid_micros":9007199254740993}]}`,
		keywordPlanAdGroupKeywordsRemoveID: `{"customer_id":"` + customer + `","keyword_plan_ad_group_keyword_ids":["111","222"]}`,
	}
}

func keywordPlanAdGroupKeywordsAnswer(customer string) string {
	return `{"results":[{"resourceName":"` + keywordPlanAdGroupKeywordResource(customer, "111") + `"},{"resourceName":"` +
		keywordPlanAdGroupKeywordResource(customer, "222") + `"}]}`
}

func TestKeywordPlanAdGroupKeywordToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	effects := map[string]capability.Effect{keywordPlanAdGroupKeywordsCreateID: capability.EffectCreate,
		keywordPlanAdGroupKeywordsUpdateID: capability.EffectUpdate, keywordPlanAdGroupKeywordsRemoveID: capability.EffectDelete}
	for _, id := range keywordPlanAdGroupKeywordsToolIDs {
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
			"response_content_type", "resource_name", "keyword_plan_ad_group"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers %s", id, forbidden)
			}
		}
	}
}

func TestKeywordPlanAdGroupKeywordToolsAreReachedByTheirEffectOrByTheToolsList(t *testing.T) {
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+keywordPlanAdGroupKeywordsPath(clientID)] = okReply(keywordPlanAdGroupKeywordsAnswer(clientID))
		_, err := e.invokeConfirmed(id, keywordPlanAdGroupKeywordsCalls(clientID)[id])
		return err == nil
	}
	for _, c := range []struct {
		id     string
		effect config.Effect
	}{{keywordPlanAdGroupKeywordsCreateID, config.EffectCreate}, {keywordPlanAdGroupKeywordsUpdateID, config.EffectUpdate},
		{keywordPlanAdGroupKeywordsRemoveID, config.EffectDelete}} {
		if !reached(config.Connection{Effects: []config.Effect{c.effect}}, c.id) ||
			reached(config.Connection{Effects: []config.Effect{config.EffectRead}}, c.id) ||
			!reached(config.Connection{Tools: []string{c.id}}, c.id) {
			t.Errorf("%s is not reached as described", c.id)
		}
	}
}

func TestKeywordPlanAdGroupKeywordsNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, id := range keywordPlanAdGroupKeywordsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlanAdGroupKeywordsEnvironment(t, nil, nil)
			_, err := e.invoke(id, keywordPlanAdGroupKeywordsCalls(clientID)[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)

			e = newKeywordPlanAdGroupKeywordsEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err = e.invokeConfirmed(id, keywordPlanAdGroupKeywordsCalls(otherID)[id])
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestKeywordPlanAdGroupKeywordArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	list := func(items ...string) string {
		return head + `"keyword_plan_ad_group_keywords":[` + strings.Join(items, ",") + `]}`
	}
	ids := func(items ...string) string {
		return head + `"keyword_plan_ad_group_keyword_ids":[` + strings.Join(items, ",") + `]}`
	}
	var manyNew, manyIDs []string
	for i := 1; i <= 51; i++ {
		manyNew = append(manyNew, `{"keyword_plan_ad_group_id":"`+strconv.Itoa(i)+`","text":"a","match_type":"EXACT"}`)
		manyIDs = append(manyIDs, `"`+strconv.Itoa(i)+`"`)
	}
	create := func(rest string) string {
		return `{"keyword_plan_ad_group_id":"1","text":"a","match_type":"EXACT"` + rest + `}`
	}
	edit := func(rest string) string { return `{"keyword_plan_ad_group_keyword_id":"1"` + rest + `}` }
	c, u, r := keywordPlanAdGroupKeywordsCreateID, keywordPlanAdGroupKeywordsUpdateID, keywordPlanAdGroupKeywordsRemoveID
	bad := map[string][2]string{
		"create none":     {c, list()},
		"create too many": {c, list(manyNew...)},
		"create no group": {c, list(`{"text":"a","match_type":"EXACT"}`)},
		"create group name": {c, list(`{"keyword_plan_ad_group_id":"customers/1/keywordPlanAdGroups/2","text":"a",` +
			`"match_type":"EXACT"}`)},
		"create group zero": {c, list(`{"keyword_plan_ad_group_id":"0","text":"a","match_type":"EXACT"}`)},
		"create no text":    {c, list(`{"keyword_plan_ad_group_id":"1","match_type":"EXACT"}`)},
		"create no match":   {c, list(`{"keyword_plan_ad_group_id":"1","text":"a"}`)},
		"create empty text": {c, list(`{"keyword_plan_ad_group_id":"1","text":"","match_type":"EXACT"}`)},
		"create blank text": {c, list(`{"keyword_plan_ad_group_id":"1","text":"   ","match_type":"EXACT"}`)},
		"create long text": {c, list(`{"keyword_plan_ad_group_id":"1","text":"` + strings.Repeat("a", 81) +
			`","match_type":"EXACT"}`)},
		"create line feed": {c, list(`{"keyword_plan_ad_group_id":"1","text":"a\nb","match_type":"EXACT"}`)},
		"create match":     {c, list(`{"keyword_plan_ad_group_id":"1","text":"a","match_type":"exact"}`)},
		"create unknown":   {c, list(`{"keyword_plan_ad_group_id":"1","text":"a","match_type":"UNSPECIFIED"}`)},
		"create negative bid": {c, list(`{"keyword_plan_ad_group_id":"1","text":"a","match_type":"EXACT",` +
			`"negative":true,"cpc_bid_micros":100}`)},
		"create negative text": {c, list(create(`,"negative":"yes"`))},
		"create zero bid":      {c, list(create(`,"cpc_bid_micros":0`))},
		"create fractional":    {c, list(create(`,"cpc_bid_micros":1.5`))},
		"create huge bid":      {c, list(create(`,"cpc_bid_micros":99999999999999999999`))},
		"create resource":      {c, list(create(`,"resource_name":"customers/1/keywordPlanAdGroupKeywords/1"`))},
		"create content type":  {c, strings.TrimSuffix(list(create(``)), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
		"update none":          {u, list()},
		"update too many":      {u, list(manyNew...)},
		"update no id":         {u, list(`{"text":"a"}`)},
		"update name as id":    {u, list(`{"keyword_plan_ad_group_keyword_id":"customers/1/keywordPlanAdGroupKeywords/2","text":"a"}`)},
		"update nothing":       {u, list(edit(``))},
		"update empty text":    {u, list(edit(`,"text":""`))},
		"update long text":     {u, list(edit(`,"text":"` + strings.Repeat("a", 81) + `"`))},
		"update match":         {u, list(edit(`,"match_type":"BAD"`))},
		"update bid":           {u, list(edit(`,"cpc_bid_micros":0`))},
		"update negative":      {u, list(edit(`,"negative":true`))},
		"update group":         {u, list(edit(`,"keyword_plan_ad_group_id":"2"`))},
		"update twice":         {u, list(edit(`,"text":"a"`), edit(`,"text":"b"`))},
		"remove none":          {r, ids()},
		"remove too many":      {r, ids(manyIDs...)},
		"remove name":          {r, ids(`"customers/1/keywordPlanAdGroupKeywords/2"`)},
		"remove negative id":   {r, ids(`"-5"`)},
		"remove twice":         {r, ids(`"1"`, `"1"`)},
		"bad customer":         {r, `{"customer_id":"12-34","keyword_plan_ad_group_keyword_ids":["1"]}`},
	}
	for name, k := range bad {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlanAdGroupKeywordsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(k[0], k[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newKeywordPlanAdGroupKeywordsEnvironment(t, nil, nil)
		e.fake.routes["POST "+keywordPlanAdGroupKeywordsPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
			keywordPlanAdGroupKeywordResource(clientID, "5") + `"}]}`)
		call := list(`{"keyword_plan_ad_group_id":"1","text":"` + strings.Repeat("ä", 80) + `","match_type":"PHRASE"}`)
		if _, err := e.invokeConfirmed(c, call); err != nil {
			t.Fatal(err)
		}
	})
}

func TestKeywordPlanAdGroupKeywordsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		keywordPlanAdGroupKeywordsCreateID: `[{"create":{"keywordPlanAdGroup":"customers/1000000003/keywordPlanAdGroups/11",` +
			`"text":"running shoes","matchType":"EXACT","negative":false,"cpcBidMicros":"1500000"}},` +
			`{"create":{"keywordPlanAdGroup":"customers/1000000003/keywordPlanAdGroups/11","text":"free",` +
			`"matchType":"BROAD","negative":true}}]`,
		keywordPlanAdGroupKeywordsUpdateID: `[{"update":{"resourceName":"customers/1000000003/keywordPlanAdGroupKeywords/111","text":"trail shoes"},"updateMask":"text"},` +
			`{"update":{"resourceName":"customers/1000000003/keywordPlanAdGroupKeywords/222","matchType":"PHRASE","cpcBidMicros":"9007199254740993"},` +
			`"updateMask":"match_type,cpc_bid_micros"}]`,
		keywordPlanAdGroupKeywordsRemoveID: `[{"remove":"customers/1000000003/keywordPlanAdGroupKeywords/111"},{"remove":"customers/1000000003/keywordPlanAdGroupKeywords/222"}]`,
	}
	for _, id := range keywordPlanAdGroupKeywordsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlanAdGroupKeywordsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+keywordPlanAdGroupKeywordsPath(clientID)] = okReply(keywordPlanAdGroupKeywordsAnswer(clientID))
			out, err := e.invokeConfirmed(id, keywordPlanAdGroupKeywordsCalls(clientID)[id])
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
				results[0].(map[string]any)["keyword_plan_ad_group_keyword_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != keywordPlanAdGroupKeywordResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
		t.Run(id+" validate only", func(t *testing.T) {
			e := newKeywordPlanAdGroupKeywordsEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlanAdGroupKeywordsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(keywordPlanAdGroupKeywordsCalls(clientID)[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); bodyOf(t, e.posts()[0])["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestKeywordPlanAdGroupKeywordResultsAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(id string) string {
		return `{"resourceName":"` + keywordPlanAdGroupKeywordResource(clientID, id) + `"}`
	}
	foreign := `{"resourceName":"` + keywordPlanAdGroupKeywordResource(otherID, "111") + `"}`
	c, u, r := keywordPlanAdGroupKeywordsCreateID, keywordPlanAdGroupKeywordsUpdateID, keywordPlanAdGroupKeywordsRemoveID
	cases := map[string]struct{ id, body string }{
		"create foreign": {c, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"create none":    {c, `{}`},
		"create one":     {c, `{"results":[` + own("111") + `]}`},
		"create twice":   {c, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create not a keyword": {c, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) +
			`/keywordPlanAdGroups/1"},` + own("222") + `]}`},
		"create partial":     {c, `{"results":[` + own("111") + `,` + own("222") + `],"partialFailureError":{"code":3}}`},
		"update other order": {u, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":     {u, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"remove other order": {r, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"remove foreign":     {r, `{"results":[` + foreign + `,` + own("222") + `]}`},
	}
	for name, k := range cases {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlanAdGroupKeywordsEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlanAdGroupKeywordsPath(clientID)] = okReply(k.body)
			_, err := e.invokeConfirmed(k.id, keywordPlanAdGroupKeywordsCalls(clientID)[k.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestKeywordPlanAdGroupKeywordManagerAccountsNeedAManagerTarget(t *testing.T) {
	call := `{"customer_id":"` + managerID + `","keyword_plan_ad_group_keyword_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + keywordPlanAdGroupKeywordResource(managerID, "7") + `"}]}`)
	e := newKeywordPlanAdGroupKeywordsEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlanAdGroupKeywordsPath(managerID)] = answer
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	if _, err := e.invokeConfirmed(keywordPlanAdGroupKeywordsRemoveID, call); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
	e = newKeywordPlanAdGroupKeywordsEnvironment(t, []string{"managers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlanAdGroupKeywordsPath(managerID)] = answer
	if _, err := e.invokeConfirmed(keywordPlanAdGroupKeywordsRemoveID, call); err != nil {
		t.Fatal(err)
	}
}

func TestKeywordPlanAdGroupKeywordErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"keyword error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "keywordPlanAdGroupKeywordError",
			"INVALID_KEYWORD_MATCH_TYPE", "")}, provider.ClassProviderError,
			"rejected the change (keywordPlanAdGroupKeywordError INVALID_KEYWORD_MATCH_TYPE), nothing was changed"},
		{"limit", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "resourceCountLimitExceededError",
			"ACCOUNT_LIMIT", "")}, provider.ClassProviderError,
			"rejected the change (resourceCountLimitExceededError ACCOUNT_LIMIT), nothing was changed"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown"},
	}
	for _, k := range cases {
		for _, id := range keywordPlanAdGroupKeywordsToolIDs {
			t.Run(k.name+" "+id, func(t *testing.T) {
				e := newKeywordPlanAdGroupKeywordsEnvironment(t, nil, nil)
				e.fake.routes["POST "+keywordPlanAdGroupKeywordsPath(clientID)] = k.reply
				_, err := e.invokeConfirmed(id, keywordPlanAdGroupKeywordsCalls(clientID)[id])
				if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) || len(e.posts()) != 1 ||
					len(e.fake.api()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestKeywordPlanAdGroupKeywordSummariesNameTheEffect(t *testing.T) {
	calls := keywordPlanAdGroupKeywordsCalls(clientID)
	create := keywordPlanAdGroupKeywordsCreateSummary(json.RawMessage(calls[keywordPlanAdGroupKeywordsCreateID]))
	update := keywordPlanAdGroupKeywordsUpdateSummary(json.RawMessage(calls[keywordPlanAdGroupKeywordsUpdateID]))
	remove := keywordPlanAdGroupKeywordsRemoveSummary(json.RawMessage(calls[keywordPlanAdGroupKeywordsRemoveID]))
	for _, word := range []string{clientID, "2 keyword(s)", `positive "running shoes" EXACT in keyword plan ad group 11 bid 1500000 micros`,
		`negative "free" BROAD in keyword plan ad group 11`} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	for _, word := range []string{clientID, `keyword 111 text "trail shoes"`, "keyword 222 match type PHRASE bid 9007199254740993 micros"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	for _, word := range []string{clientID, "111, 222", "No tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	long := keywordPlanAdGroupKeywordsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID +
		`","keyword_plan_ad_group_keywords":[{"text":"` + strings.Repeat("a", 5000) +
		`\n","keyword_plan_ad_group_id":"1","match_type":"EXACT"}]}`))
	if len(long) > 2000 || strings.Contains(long, "\n") {
		t.Errorf("summary is not cut: %d characters", len(long))
	}
	if text := keywordPlanAdGroupKeywordsRemoveSummary(json.RawMessage(strings.TrimSuffix(calls[keywordPlanAdGroupKeywordsRemoveID], "}") +
		`,"validate_only":true}`)); !strings.HasPrefix(text, "Only validate") {
		t.Errorf("summary %q", text)
	}
}
