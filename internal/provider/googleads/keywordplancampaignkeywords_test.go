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

var keywordPlanCampaignKeywordsToolIDs = []string{keywordPlanCampaignKeywordsCreateID,
	keywordPlanCampaignKeywordsUpdateID, keywordPlanCampaignKeywordsRemoveID}

func keywordPlanCampaignKeywordsPath(id string) string {
	return "/v26/customers/" + id + "/keywordPlanCampaignKeywords:mutate"
}

func keywordPlanCampaignKeywordResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/keywordPlanCampaignKeywords/" + id
}

func newKeywordPlanCampaignKeywordsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: keywordPlanCampaignKeywordsToolIDs})
}

func keywordPlanCampaignKeywordsCalls(customer string) map[string]string {
	return map[string]string{
		keywordPlanCampaignKeywordsCreateID: `{"customer_id":"` + customer + `","keyword_plan_campaign_keywords":[` +
			`{"keyword_plan_campaign_id":"11","text":"free trial","match_type":"PHRASE"},` +
			`{"keyword_plan_campaign_id":"22","text":"käse","match_type":"EXACT"}]}`,
		keywordPlanCampaignKeywordsUpdateID: `{"customer_id":"` + customer + `","keyword_plan_campaign_keywords":[` +
			`{"keyword_plan_campaign_keyword_id":"111","text":"free"},` +
			`{"keyword_plan_campaign_keyword_id":"222","text":"cheap","match_type":"BROAD"}]}`,
		keywordPlanCampaignKeywordsRemoveID: `{"customer_id":"` + customer + `","keyword_plan_campaign_keyword_ids":["111","222"]}`,
	}
}

func keywordPlanCampaignKeywordsAnswer(customer string) string {
	return `{"results":[{"resourceName":"` + keywordPlanCampaignKeywordResource(customer, "111") + `"},{"resourceName":"` +
		keywordPlanCampaignKeywordResource(customer, "222") + `"}]}`
}

func TestKeywordPlanCampaignKeywordToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	effects := map[string]capability.Effect{keywordPlanCampaignKeywordsCreateID: capability.EffectCreate,
		keywordPlanCampaignKeywordsUpdateID: capability.EffectUpdate, keywordPlanCampaignKeywordsRemoveID: capability.EffectDelete}
	for _, id := range keywordPlanCampaignKeywordsToolIDs {
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
			"response_content_type", "resource_name", "keyword_plan_campaign", "negative"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers %s", id, forbidden)
			}
		}
	}
}

func TestKeywordPlanCampaignKeywordToolsAreReachedByTheirEffectOrByTheToolsList(t *testing.T) {
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+keywordPlanCampaignKeywordsPath(clientID)] = okReply(keywordPlanCampaignKeywordsAnswer(clientID))
		_, err := e.invokeConfirmed(id, keywordPlanCampaignKeywordsCalls(clientID)[id])
		return err == nil
	}
	for _, c := range []struct {
		id     string
		effect config.Effect
	}{{keywordPlanCampaignKeywordsCreateID, config.EffectCreate}, {keywordPlanCampaignKeywordsUpdateID, config.EffectUpdate},
		{keywordPlanCampaignKeywordsRemoveID, config.EffectDelete}} {
		if !reached(config.Connection{Effects: []config.Effect{c.effect}}, c.id) ||
			reached(config.Connection{Effects: []config.Effect{config.EffectRead}}, c.id) ||
			!reached(config.Connection{Tools: []string{c.id}}, c.id) {
			t.Errorf("%s is not reached as described", c.id)
		}
	}
}

func TestKeywordPlanCampaignKeywordsNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, id := range keywordPlanCampaignKeywordsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlanCampaignKeywordsEnvironment(t, nil, nil)
			_, err := e.invoke(id, keywordPlanCampaignKeywordsCalls(clientID)[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
			e = newKeywordPlanCampaignKeywordsEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err = e.invokeConfirmed(id, keywordPlanCampaignKeywordsCalls(otherID)[id])
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestKeywordPlanCampaignKeywordArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	list := func(items ...string) string {
		return head + `"keyword_plan_campaign_keywords":[` + strings.Join(items, ",") + `]}`
	}
	ids := func(items ...string) string {
		return head + `"keyword_plan_campaign_keyword_ids":[` + strings.Join(items, ",") + `]}`
	}
	var manyNew, manyIDs []string
	for i := 1; i <= 51; i++ {
		manyNew = append(manyNew, `{"keyword_plan_campaign_id":"1","text":"a","match_type":"EXACT"}`)
		manyIDs = append(manyIDs, `"`+strconv.Itoa(i)+`"`)
	}
	create := func(rest string) string {
		return `{"keyword_plan_campaign_id":"1","text":"a","match_type":"EXACT"` + rest + `}`
	}
	edit := func(rest string) string { return `{"keyword_plan_campaign_keyword_id":"1"` + rest + `}` }
	bad := map[string][2]string{
		"create none":          {keywordPlanCampaignKeywordsCreateID, list()},
		"create too many":      {keywordPlanCampaignKeywordsCreateID, list(manyNew...)},
		"create no campaign":   {keywordPlanCampaignKeywordsCreateID, list(`{"text":"a","match_type":"EXACT"}`)},
		"create campaign name": {keywordPlanCampaignKeywordsCreateID, list(create(`,"keyword_plan_campaign_id":"customers/1/keywordPlanCampaigns/2"`))},
		"create campaign zero": {keywordPlanCampaignKeywordsCreateID, list(create(`,"keyword_plan_campaign_id":"0"`))},
		"create no text":       {keywordPlanCampaignKeywordsCreateID, list(`{"keyword_plan_campaign_id":"1","match_type":"EXACT"}`)},
		"create empty text":    {keywordPlanCampaignKeywordsCreateID, list(create(`,"text":""`))},
		"create blank text":    {keywordPlanCampaignKeywordsCreateID, list(create(`,"text":"   "`))},
		"create line feed":     {keywordPlanCampaignKeywordsCreateID, list(create(`,"text":"a\nb"`))},
		"create long text":     {keywordPlanCampaignKeywordsCreateID, list(create(`,"text":"` + strings.Repeat("a", 81) + `"`))},
		"create no match":      {keywordPlanCampaignKeywordsCreateID, list(`{"keyword_plan_campaign_id":"1","text":"a"}`)},
		"create match":         {keywordPlanCampaignKeywordsCreateID, list(create(`,"match_type":"UNKNOWN"`))},
		"create match low":     {keywordPlanCampaignKeywordsCreateID, list(create(`,"match_type":"exact"`))},
		"create negative":      {keywordPlanCampaignKeywordsCreateID, list(create(`,"negative":false`))},
		"create resource":      {keywordPlanCampaignKeywordsCreateID, list(create(`,"resource_name":"customers/1/keywordPlanCampaignKeywords/1"`))},
		"create content type":  {keywordPlanCampaignKeywordsCreateID, strings.TrimSuffix(list(create(``)), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
		"update none":          {keywordPlanCampaignKeywordsUpdateID, list()},
		"update too many":      {keywordPlanCampaignKeywordsUpdateID, list(manyNew...)},
		"update no id":         {keywordPlanCampaignKeywordsUpdateID, list(`{"text":"a"}`)},
		"update name as id":    {keywordPlanCampaignKeywordsUpdateID, list(`{"keyword_plan_campaign_keyword_id":"customers/1/keywordPlanCampaignKeywords/2","text":"a"}`)},
		"update nothing":       {keywordPlanCampaignKeywordsUpdateID, list(edit(``))},
		"update empty text":    {keywordPlanCampaignKeywordsUpdateID, list(edit(`,"text":""`))},
		"update match":         {keywordPlanCampaignKeywordsUpdateID, list(edit(`,"match_type":"UNSPECIFIED"`))},
		"update campaign":      {keywordPlanCampaignKeywordsUpdateID, list(edit(`,"keyword_plan_campaign_id":"2"`))},
		"update negative":      {keywordPlanCampaignKeywordsUpdateID, list(edit(`,"negative":true`))},
		"update twice":         {keywordPlanCampaignKeywordsUpdateID, list(edit(`,"text":"a"`), edit(`,"text":"b"`))},
		"remove none":          {keywordPlanCampaignKeywordsRemoveID, ids()},
		"remove too many":      {keywordPlanCampaignKeywordsRemoveID, ids(manyIDs...)},
		"remove name":          {keywordPlanCampaignKeywordsRemoveID, ids(`"customers/1/keywordPlanCampaignKeywords/2"`)},
		"remove negative":      {keywordPlanCampaignKeywordsRemoveID, ids(`"-5"`)},
		"remove twice":         {keywordPlanCampaignKeywordsRemoveID, ids(`"1"`, `"1"`)},
		"bad customer":         {keywordPlanCampaignKeywordsRemoveID, `{"customer_id":"12-34","keyword_plan_campaign_keyword_ids":["1"]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlanCampaignKeywordsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newKeywordPlanCampaignKeywordsEnvironment(t, nil, nil)
		e.fake.routes["POST "+keywordPlanCampaignKeywordsPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
			keywordPlanCampaignKeywordResource(clientID, "5") + `"}]}`)
		call := list(create(`,"text":"` + strings.Repeat("ä", 80) + `"`))
		if _, err := e.invokeConfirmed(keywordPlanCampaignKeywordsCreateID, call); err != nil {
			t.Fatal(err)
		}
	})
}

func TestKeywordPlanCampaignKeywordsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		keywordPlanCampaignKeywordsCreateID: `[{"create":{"keywordPlanCampaign":"customers/1000000003/keywordPlanCampaigns/11",` +
			`"text":"free trial","matchType":"PHRASE","negative":true}},` +
			`{"create":{"keywordPlanCampaign":"customers/1000000003/keywordPlanCampaigns/22",` +
			`"text":"käse","matchType":"EXACT","negative":true}}]`,
		keywordPlanCampaignKeywordsUpdateID: `[{"update":{"resourceName":"customers/1000000003/keywordPlanCampaignKeywords/111","text":"free"},"updateMask":"text"},` +
			`{"update":{"resourceName":"customers/1000000003/keywordPlanCampaignKeywords/222","text":"cheap","matchType":"BROAD"},` +
			`"updateMask":"text,match_type"}]`,
		keywordPlanCampaignKeywordsRemoveID: `[{"remove":"customers/1000000003/keywordPlanCampaignKeywords/111"},{"remove":"customers/1000000003/keywordPlanCampaignKeywords/222"}]`,
	}
	for _, id := range keywordPlanCampaignKeywordsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlanCampaignKeywordsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+keywordPlanCampaignKeywordsPath(clientID)] = okReply(keywordPlanCampaignKeywordsAnswer(clientID))
			out, err := e.invokeConfirmed(id, keywordPlanCampaignKeywordsCalls(clientID)[id])
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
				results[0].(map[string]any)["keyword_plan_campaign_keyword_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != keywordPlanCampaignKeywordResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
		t.Run(id+" validate only", func(t *testing.T) {
			e := newKeywordPlanCampaignKeywordsEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlanCampaignKeywordsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(keywordPlanCampaignKeywordsCalls(clientID)[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); bodyOf(t, e.posts()[0])["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestKeywordPlanCampaignKeywordResultsAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(id string) string {
		return `{"resourceName":"` + keywordPlanCampaignKeywordResource(clientID, id) + `"}`
	}
	foreign := `{"resourceName":"` + keywordPlanCampaignKeywordResource(otherID, "111") + `"}`
	cases := map[string]struct{ id, body string }{
		"create foreign": {keywordPlanCampaignKeywordsCreateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"create none":    {keywordPlanCampaignKeywordsCreateID, `{}`},
		"create one":     {keywordPlanCampaignKeywordsCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":   {keywordPlanCampaignKeywordsCreateID, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create not a keyword": {keywordPlanCampaignKeywordsCreateID, `{"results":[{"resourceName":"customers/` +
			queryNumber(clientID) + `/keywordPlanCampaigns/1"},` + own("222") + `]}`},
		"create partial": {keywordPlanCampaignKeywordsCreateID, `{"results":[` + own("111") + `,` + own("222") +
			`],"partialFailureError":{"code":3}}`},
		"update other order": {keywordPlanCampaignKeywordsUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":     {keywordPlanCampaignKeywordsUpdateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"remove other order": {keywordPlanCampaignKeywordsRemoveID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"remove foreign":     {keywordPlanCampaignKeywordsRemoveID, `{"results":[` + foreign + `,` + own("222") + `]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlanCampaignKeywordsEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlanCampaignKeywordsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, keywordPlanCampaignKeywordsCalls(clientID)[c.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestKeywordPlanCampaignKeywordManagerAccountsNeedAManagerTarget(t *testing.T) {
	call := `{"customer_id":"` + managerID + `","keyword_plan_campaign_keyword_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + keywordPlanCampaignKeywordResource(managerID, "7") + `"}]}`)
	e := newKeywordPlanCampaignKeywordsEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlanCampaignKeywordsPath(managerID)] = answer
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	if _, err := e.invokeConfirmed(keywordPlanCampaignKeywordsRemoveID, call); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
	e = newKeywordPlanCampaignKeywordsEnvironment(t, []string{"managers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlanCampaignKeywordsPath(managerID)] = answer
	if _, err := e.invokeConfirmed(keywordPlanCampaignKeywordsRemoveID, call); err != nil {
		t.Fatal(err)
	}
}

func TestKeywordPlanCampaignKeywordErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"keyword error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "keywordPlanCampaignKeywordError",
			"CAMPAIGN_KEYWORD_IS_POSITIVE", "")}, provider.ClassProviderError,
			"rejected the change (keywordPlanCampaignKeywordError CAMPAIGN_KEYWORD_IS_POSITIVE), nothing was changed"},
		{"limit", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "resourceCountLimitExceededError",
			"ACCOUNT_LIMIT", "")}, provider.ClassProviderError,
			"rejected the change (resourceCountLimitExceededError ACCOUNT_LIMIT), nothing was changed"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown"},
	}
	for _, k := range cases {
		for _, id := range keywordPlanCampaignKeywordsToolIDs {
			t.Run(k.name+" "+id, func(t *testing.T) {
				e := newKeywordPlanCampaignKeywordsEnvironment(t, nil, nil)
				e.fake.routes["POST "+keywordPlanCampaignKeywordsPath(clientID)] = k.reply
				_, err := e.invokeConfirmed(id, keywordPlanCampaignKeywordsCalls(clientID)[id])
				if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) || len(e.posts()) != 1 ||
					len(e.fake.api()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestKeywordPlanCampaignKeywordSummariesNameTheEffect(t *testing.T) {
	calls := keywordPlanCampaignKeywordsCalls(clientID)
	create := keywordPlanCampaignKeywordsCreateSummary(json.RawMessage(calls[keywordPlanCampaignKeywordsCreateID]))
	update := keywordPlanCampaignKeywordsUpdateSummary(json.RawMessage(calls[keywordPlanCampaignKeywordsUpdateID]))
	remove := keywordPlanCampaignKeywordsRemoveSummary(json.RawMessage(calls[keywordPlanCampaignKeywordsRemoveID]))
	for _, word := range []string{clientID, "2 negative keyword", `"free trial" PHRASE on keyword plan campaign 11`} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	for _, word := range []string{clientID, `keyword 111 text "free"`, `keyword 222 text "cheap" match type BROAD`} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	for _, word := range []string{clientID, "111, 222", "No tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	long := keywordPlanCampaignKeywordsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID +
		`","keyword_plan_campaign_keywords":[{"text":"` + strings.Repeat("a", 5000) +
		`\n","keyword_plan_campaign_id":"1","match_type":"EXACT"}]}`))
	if len(long) > 2000 || strings.Contains(long, "\n") {
		t.Errorf("summary is not cut: %d characters", len(long))
	}
	if text := keywordPlanCampaignKeywordsRemoveSummary(json.RawMessage(strings.TrimSuffix(calls[keywordPlanCampaignKeywordsRemoveID], "}") +
		`,"validate_only":true}`)); !strings.HasPrefix(text, "Only validate") {
		t.Errorf("summary %q", text)
	}
}
