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

var keywordPlanCampaignsToolIDs = []string{keywordPlanCampaignsCreateID, keywordPlanCampaignsUpdateID,
	keywordPlanCampaignsRemoveID}

func keywordPlanCampaignsPath(id string) string {
	return "/v26/customers/" + id + "/keywordPlanCampaigns:mutate"
}

func keywordPlanCampaignResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/keywordPlanCampaigns/" + id
}

func newKeywordPlanCampaignsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: keywordPlanCampaignsToolIDs})
}

func keywordPlanCampaignsCalls(customer string) map[string]string {
	return map[string]string{
		keywordPlanCampaignsCreateID: `{"customer_id":"` + customer + `","keyword_plan_campaigns":[` +
			`{"keyword_plan_id":"11","name":"Spring","keyword_plan_network":"GOOGLE_SEARCH","cpc_bid_micros":1500000,` +
			`"language_constants":["languageConstants/1001"],"geo_target_constants":["geoTargetConstants/2276","geoTargetConstants/2250"]},` +
			`{"keyword_plan_id":"22","name":"Summer","keyword_plan_network":"GOOGLE_SEARCH_AND_PARTNERS","cpc_bid_micros":9007199254740993}]}`,
		keywordPlanCampaignsUpdateID: `{"customer_id":"` + customer + `","keyword_plan_campaigns":[` +
			`{"keyword_plan_campaign_id":"111","name":"Autumn"},` +
			`{"keyword_plan_campaign_id":"222","keyword_plan_network":"GOOGLE_SEARCH","cpc_bid_micros":2000000,` +
			`"language_constants":["languageConstants/1000"],"geo_target_constants":["geoTargetConstants/2276"]}]}`,
		keywordPlanCampaignsRemoveID: `{"customer_id":"` + customer + `","keyword_plan_campaign_ids":["111","222"]}`,
	}
}

func keywordPlanCampaignsAnswer(customer string) string {
	return `{"results":[{"resourceName":"` + keywordPlanCampaignResource(customer, "111") + `"},{"resourceName":"` +
		keywordPlanCampaignResource(customer, "222") + `"}]}`
}

func TestKeywordPlanCampaignToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	effects := map[string]capability.Effect{keywordPlanCampaignsCreateID: capability.EffectCreate,
		keywordPlanCampaignsUpdateID: capability.EffectUpdate, keywordPlanCampaignsRemoveID: capability.EffectDelete}
	for _, id := range keywordPlanCampaignsToolIDs {
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
			"response_content_type", "resource_name", "keyword_plan"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers %s", id, forbidden)
			}
		}
	}
}

func TestKeywordPlanCampaignToolsAreReachedByTheirEffectOrByTheToolsList(t *testing.T) {
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+keywordPlanCampaignsPath(clientID)] = okReply(keywordPlanCampaignsAnswer(clientID))
		_, err := e.invokeConfirmed(id, keywordPlanCampaignsCalls(clientID)[id])
		return err == nil
	}
	for _, c := range []struct {
		id     string
		effect config.Effect
	}{{keywordPlanCampaignsCreateID, config.EffectCreate}, {keywordPlanCampaignsUpdateID, config.EffectUpdate},
		{keywordPlanCampaignsRemoveID, config.EffectDelete}} {
		if !reached(config.Connection{Effects: []config.Effect{c.effect}}, c.id) ||
			reached(config.Connection{Effects: []config.Effect{config.EffectRead}}, c.id) ||
			!reached(config.Connection{Tools: []string{c.id}}, c.id) {
			t.Errorf("%s is not reached as described", c.id)
		}
	}
}

func TestKeywordPlanCampaignsNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, id := range keywordPlanCampaignsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlanCampaignsEnvironment(t, nil, nil)
			_, err := e.invoke(id, keywordPlanCampaignsCalls(clientID)[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)

			e = newKeywordPlanCampaignsEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err = e.invokeConfirmed(id, keywordPlanCampaignsCalls(otherID)[id])
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestKeywordPlanCampaignArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	list := func(items ...string) string {
		return head + `"keyword_plan_campaigns":[` + strings.Join(items, ",") + `]}`
	}
	ids := func(items ...string) string {
		return head + `"keyword_plan_campaign_ids":[` + strings.Join(items, ",") + `]}`
	}
	var manyNew, manyIDs []string
	for i := 1; i <= 51; i++ {
		manyNew = append(manyNew, `{"keyword_plan_id":"`+strconv.Itoa(i)+`","name":"a","keyword_plan_network":"GOOGLE_SEARCH","cpc_bid_micros":1}`)
		manyIDs = append(manyIDs, `"`+strconv.Itoa(i)+`"`)
	}
	var geos21 []string
	for i := 1; i <= 21; i++ {
		geos21 = append(geos21, `"geoTargetConstants/`+strconv.Itoa(i)+`"`)
	}
	create := func(rest string) string {
		return `{"keyword_plan_id":"1","name":"a","keyword_plan_network":"GOOGLE_SEARCH","cpc_bid_micros":1` + rest + `}`
	}
	edit := func(rest string) string { return `{"keyword_plan_campaign_id":"1"` + rest + `}` }
	bad := map[string][2]string{
		"create none":         {keywordPlanCampaignsCreateID, list()},
		"create too many":     {keywordPlanCampaignsCreateID, list(manyNew...)},
		"create no plan":      {keywordPlanCampaignsCreateID, list(`{"name":"a","keyword_plan_network":"GOOGLE_SEARCH","cpc_bid_micros":1}`)},
		"create plan name":    {keywordPlanCampaignsCreateID, list(`{"keyword_plan_id":"customers/1/keywordPlans/2","name":"a","keyword_plan_network":"GOOGLE_SEARCH","cpc_bid_micros":1}`)},
		"create plan zero":    {keywordPlanCampaignsCreateID, list(`{"keyword_plan_id":"0","name":"a","keyword_plan_network":"GOOGLE_SEARCH","cpc_bid_micros":1}`)},
		"create same plan":    {keywordPlanCampaignsCreateID, list(create(``), create(`,"name":"b"`))},
		"create no name":      {keywordPlanCampaignsCreateID, list(`{"keyword_plan_id":"1","keyword_plan_network":"GOOGLE_SEARCH","cpc_bid_micros":1}`)},
		"create empty name":   {keywordPlanCampaignsCreateID, list(create(`,"name":""`))},
		"create line feed":    {keywordPlanCampaignsCreateID, list(create(`,"name":"a\nb"`))},
		"create no network":   {keywordPlanCampaignsCreateID, list(`{"keyword_plan_id":"1","name":"a","cpc_bid_micros":1}`)},
		"create network":      {keywordPlanCampaignsCreateID, list(create(`,"keyword_plan_network":"UNKNOWN"`))},
		"create network low":  {keywordPlanCampaignsCreateID, list(create(`,"keyword_plan_network":"google_search"`))},
		"create no bid":       {keywordPlanCampaignsCreateID, list(`{"keyword_plan_id":"1","name":"a","keyword_plan_network":"GOOGLE_SEARCH"}`)},
		"create zero bid":     {keywordPlanCampaignsCreateID, list(create(`,"cpc_bid_micros":0`))},
		"create fractional":   {keywordPlanCampaignsCreateID, list(create(`,"cpc_bid_micros":1.5`))},
		"create negative":     {keywordPlanCampaignsCreateID, list(create(`,"cpc_bid_micros":-1`))},
		"create text bid":     {keywordPlanCampaignsCreateID, list(create(`,"cpc_bid_micros":"5"`))},
		"create huge bid":     {keywordPlanCampaignsCreateID, list(create(`,"cpc_bid_micros":99999999999999999999`))},
		"create two langs":    {keywordPlanCampaignsCreateID, list(create(`,"language_constants":["languageConstants/1","languageConstants/2"]`))},
		"create no lang":      {keywordPlanCampaignsCreateID, list(create(`,"language_constants":[]`))},
		"create lang name":    {keywordPlanCampaignsCreateID, list(create(`,"language_constants":["en"]`))},
		"create 21 geos":      {keywordPlanCampaignsCreateID, list(create(`,"geo_target_constants":[` + strings.Join(geos21, ",") + `]`))},
		"create geo name":     {keywordPlanCampaignsCreateID, list(create(`,"geo_target_constants":["Germany"]`))},
		"create geo twice":    {keywordPlanCampaignsCreateID, list(create(`,"geo_target_constants":["geoTargetConstants/1","geoTargetConstants/1"]`))},
		"create no geos":      {keywordPlanCampaignsCreateID, list(create(`,"geo_target_constants":[]`))},
		"create resource":     {keywordPlanCampaignsCreateID, list(create(`,"resource_name":"customers/1/keywordPlanCampaigns/1"`))},
		"create keyword plan": {keywordPlanCampaignsCreateID, list(create(`,"keyword_plan":"customers/1/keywordPlans/1"`))},
		"create content type": {keywordPlanCampaignsCreateID, strings.TrimSuffix(list(create(``)), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
		"update none":         {keywordPlanCampaignsUpdateID, list()},
		"update too many":     {keywordPlanCampaignsUpdateID, list(manyNew...)},
		"update no id":        {keywordPlanCampaignsUpdateID, list(`{"name":"a"}`)},
		"update name as id":   {keywordPlanCampaignsUpdateID, list(`{"keyword_plan_campaign_id":"customers/1/keywordPlanCampaigns/2","name":"a"}`)},
		"update nothing":      {keywordPlanCampaignsUpdateID, list(edit(``))},
		"update empty name":   {keywordPlanCampaignsUpdateID, list(edit(`,"name":""`))},
		"update network":      {keywordPlanCampaignsUpdateID, list(edit(`,"keyword_plan_network":"UNSPECIFIED"`))},
		"update bid":          {keywordPlanCampaignsUpdateID, list(edit(`,"cpc_bid_micros":0`))},
		"update plan":         {keywordPlanCampaignsUpdateID, list(edit(`,"keyword_plan_id":"2"`))},
		"update empty langs":  {keywordPlanCampaignsUpdateID, list(edit(`,"language_constants":[]`))},
		"update empty geos":   {keywordPlanCampaignsUpdateID, list(edit(`,"geo_target_constants":[]`))},
		"update twice":        {keywordPlanCampaignsUpdateID, list(edit(`,"name":"a"`), edit(`,"name":"b"`))},
		"remove none":         {keywordPlanCampaignsRemoveID, ids()},
		"remove too many":     {keywordPlanCampaignsRemoveID, ids(manyIDs...)},
		"remove name":         {keywordPlanCampaignsRemoveID, ids(`"customers/1/keywordPlanCampaigns/2"`)},
		"remove negative":     {keywordPlanCampaignsRemoveID, ids(`"-5"`)},
		"remove twice":        {keywordPlanCampaignsRemoveID, ids(`"1"`, `"1"`)},
		"bad customer":        {keywordPlanCampaignsRemoveID, `{"customer_id":"12-34","keyword_plan_campaign_ids":["1"]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlanCampaignsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		var geos20 []string
		for i := 1; i <= 20; i++ {
			geos20 = append(geos20, `"geoTargetConstants/`+strconv.Itoa(i)+`"`)
		}
		e := newKeywordPlanCampaignsEnvironment(t, nil, nil)
		e.fake.routes["POST "+keywordPlanCampaignsPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
			keywordPlanCampaignResource(clientID, "5") + `"}]}`)
		call := list(create(`,"name":"` + strings.Repeat("ä", 255) + `","geo_target_constants":[` + strings.Join(geos20, ",") + `]`))
		if _, err := e.invokeConfirmed(keywordPlanCampaignsCreateID, call); err != nil {
			t.Fatal(err)
		}
	})
}

func TestKeywordPlanCampaignsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		keywordPlanCampaignsCreateID: `[{"create":{"keywordPlan":"customers/1000000003/keywordPlans/11","name":"Spring",` +
			`"keywordPlanNetwork":"GOOGLE_SEARCH","cpcBidMicros":"1500000","languageConstants":["languageConstants/1001"],` +
			`"geoTargets":[{"geoTargetConstant":"geoTargetConstants/2276"},{"geoTargetConstant":"geoTargetConstants/2250"}]}},` +
			`{"create":{"keywordPlan":"customers/1000000003/keywordPlans/22","name":"Summer",` +
			`"keywordPlanNetwork":"GOOGLE_SEARCH_AND_PARTNERS","cpcBidMicros":"9007199254740993"}}]`,
		keywordPlanCampaignsUpdateID: `[{"update":{"resourceName":"customers/1000000003/keywordPlanCampaigns/111","name":"Autumn"},"updateMask":"name"},` +
			`{"update":{"resourceName":"customers/1000000003/keywordPlanCampaigns/222","keywordPlanNetwork":"GOOGLE_SEARCH",` +
			`"cpcBidMicros":"2000000","languageConstants":["languageConstants/1000"],` +
			`"geoTargets":[{"geoTargetConstant":"geoTargetConstants/2276"}]},` +
			`"updateMask":"keyword_plan_network,cpc_bid_micros,language_constants,geo_targets"}]`,
		keywordPlanCampaignsRemoveID: `[{"remove":"customers/1000000003/keywordPlanCampaigns/111"},{"remove":"customers/1000000003/keywordPlanCampaigns/222"}]`,
	}
	for _, id := range keywordPlanCampaignsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newKeywordPlanCampaignsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+keywordPlanCampaignsPath(clientID)] = okReply(keywordPlanCampaignsAnswer(clientID))
			out, err := e.invokeConfirmed(id, keywordPlanCampaignsCalls(clientID)[id])
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
				results[0].(map[string]any)["keyword_plan_campaign_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != keywordPlanCampaignResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
		t.Run(id+" validate only", func(t *testing.T) {
			e := newKeywordPlanCampaignsEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlanCampaignsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(keywordPlanCampaignsCalls(clientID)[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); bodyOf(t, e.posts()[0])["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestKeywordPlanCampaignResultsAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(id string) string { return `{"resourceName":"` + keywordPlanCampaignResource(clientID, id) + `"}` }
	foreign := `{"resourceName":"` + keywordPlanCampaignResource(otherID, "111") + `"}`
	cases := map[string]struct{ id, body string }{
		"create foreign": {keywordPlanCampaignsCreateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"create none":    {keywordPlanCampaignsCreateID, `{}`},
		"create one":     {keywordPlanCampaignsCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":   {keywordPlanCampaignsCreateID, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create not a campaign": {keywordPlanCampaignsCreateID, `{"results":[{"resourceName":"customers/` +
			queryNumber(clientID) + `/keywordPlans/1"},` + own("222") + `]}`},
		"create partial": {keywordPlanCampaignsCreateID, `{"results":[` + own("111") + `,` + own("222") +
			`],"partialFailureError":{"code":3}}`},
		"update other order": {keywordPlanCampaignsUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":     {keywordPlanCampaignsUpdateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"remove other order": {keywordPlanCampaignsRemoveID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"remove foreign":     {keywordPlanCampaignsRemoveID, `{"results":[` + foreign + `,` + own("222") + `]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newKeywordPlanCampaignsEnvironment(t, nil, nil)
			e.fake.routes["POST "+keywordPlanCampaignsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, keywordPlanCampaignsCalls(clientID)[c.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestKeywordPlanCampaignManagerAccountsNeedAManagerTarget(t *testing.T) {
	call := `{"customer_id":"` + managerID + `","keyword_plan_campaign_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + keywordPlanCampaignResource(managerID, "7") + `"}]}`)
	e := newKeywordPlanCampaignsEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlanCampaignsPath(managerID)] = answer
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	if _, err := e.invokeConfirmed(keywordPlanCampaignsRemoveID, call); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
	e = newKeywordPlanCampaignsEnvironment(t, []string{"managers/" + managerID}, nil)
	e.fake.routes["POST "+keywordPlanCampaignsPath(managerID)] = answer
	if _, err := e.invokeConfirmed(keywordPlanCampaignsRemoveID, call); err != nil {
		t.Fatal(err)
	}
}

func TestKeywordPlanCampaignErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"campaign error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "keywordPlanCampaignError",
			"INVALID_LANGUAGE_CODE", "")}, provider.ClassProviderError,
			"rejected the change (keywordPlanCampaignError INVALID_LANGUAGE_CODE), nothing was changed"},
		{"limit", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "resourceCountLimitExceededError",
			"ACCOUNT_LIMIT", "")}, provider.ClassProviderError,
			"rejected the change (resourceCountLimitExceededError ACCOUNT_LIMIT), nothing was changed"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown"},
	}
	for _, k := range cases {
		for _, id := range keywordPlanCampaignsToolIDs {
			t.Run(k.name+" "+id, func(t *testing.T) {
				e := newKeywordPlanCampaignsEnvironment(t, nil, nil)
				e.fake.routes["POST "+keywordPlanCampaignsPath(clientID)] = k.reply
				_, err := e.invokeConfirmed(id, keywordPlanCampaignsCalls(clientID)[id])
				if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) || len(e.posts()) != 1 ||
					len(e.fake.api()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestKeywordPlanCampaignSummariesNameTheEffect(t *testing.T) {
	calls := keywordPlanCampaignsCalls(clientID)
	create := keywordPlanCampaignsCreateSummary(json.RawMessage(calls[keywordPlanCampaignsCreateID]))
	update := keywordPlanCampaignsUpdateSummary(json.RawMessage(calls[keywordPlanCampaignsUpdateID]))
	remove := keywordPlanCampaignsRemoveSummary(json.RawMessage(calls[keywordPlanCampaignsRemoveID]))
	for _, word := range []string{clientID, "2 keyword plan campaign", `"Spring" for keyword plan 11`, "network GOOGLE_SEARCH",
		"bid 1500000 micros", "languages languageConstants/1001", "locations geoTargetConstants/2276,geoTargetConstants/2250"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	for _, word := range []string{clientID, `keyword plan campaign 111 name "Autumn"`, "bid 2000000 micros"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	for _, word := range []string{clientID, "111, 222", "No tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	long := keywordPlanCampaignsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID + `","keyword_plan_campaigns":[{"name":"` +
		strings.Repeat("a", 5000) + `\n","keyword_plan_id":"1"}]}`))
	if len(long) > 2000 || strings.Contains(long, "\n") {
		t.Errorf("summary is not cut: %d characters", len(long))
	}
	if text := keywordPlanCampaignsRemoveSummary(json.RawMessage(strings.TrimSuffix(calls[keywordPlanCampaignsRemoveID], "}") +
		`,"validate_only":true}`)); !strings.HasPrefix(text, "Only validate") {
		t.Errorf("summary %q", text)
	}
}
