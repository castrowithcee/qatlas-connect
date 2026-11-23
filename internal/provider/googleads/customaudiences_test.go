package googleads

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func customAudiencePath(id string) string { return "/v26/customers/" + id + "/customAudiences:mutate" }

func customAudienceNames(ids ...string) string {
	var out []string
	for _, id := range ids {
		out = append(out, `{"resourceName":"customers/`+queryNumber(clientID)+`/customAudiences/`+id+`"}`)
	}
	return `{"results":[` + strings.Join(out, ",") + `]}`
}

const (
	customAudienceCreateArgs = `{"customer_id":"` + clientID + `","custom_audiences":[{"name":"Hiking","description":"Walkers",` +
		`"type":"SEARCH","members":[{"member_type":"KEYWORD","value":"trail shoes"},{"member_type":"URL","value":` +
		`"https://www.example.com/hiking"},{"member_type":"PLACE_CATEGORY","value":"88"},{"member_type":"APP",` +
		`"value":"com.example.hike"}]},{"name":"Cycling"}]}`
	customAudienceUpdateArgs = `{"customer_id":"` + clientID + `","custom_audiences":[{"custom_audience_id":"11","name":"Hiking 2",` +
		`"description":"","members":[{"member_type":"KEYWORD","value":"boots"}]},{"custom_audience_id":"12","type":"AUTO"}]}`
	customAudienceRemoveArgs = `{"customer_id":"` + clientID + `","custom_audience_ids":["11","12"]}`
)

// customAudienceCalls are valid calls of the three tools.
var customAudienceCalls = map[string]string{customAudiencesCreateID: customAudienceCreateArgs, customAudiencesUpdateID: customAudienceUpdateArgs,
	customAudiencesRemoveID: customAudienceRemoveArgs}

func TestCustomAudienceDescriptors(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for id := range customAudienceCalls {
		d, _, ok := reg.Lookup(id)
		if !ok || d.RequiresToolAllowList != (id == customAudiencesRemoveID) || d.Version != 1 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || len(d.Fields) == 0 || len(d.OutputSchema) == 0 {
			t.Fatalf("%s = %+v", id, d)
		}
		var input struct {
			Properties map[string]struct {
				MaxItems *int `json:"maxItems"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(d.InputSchema, &input); err != nil {
			t.Fatal(err)
		}
		list := "custom_audiences"
		if id == customAudiencesRemoveID {
			list = "custom_audience_ids"
		}
		if got := input.Properties[list].MaxItems; got == nil || *got != maxCustomAudienceOperations {
			t.Errorf("%s: bound of %s = %v", id, list, got)
		}
		if _, ok := input.Properties["response_content_type"]; ok {
			t.Errorf("%s offers response_content_type, which the service does not take", id)
		}
	}
	if d, _, _ := reg.Lookup(customAudiencesRemoveID); d.Risk.Effect != capability.EffectDelete {
		t.Errorf("remove effect = %s", d.Risk.Effect)
	}
}

func TestCustomAudienceClassesOfOfferAndConfirmation(t *testing.T) {
	effects := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	for id, arguments := range customAudienceCalls {
		listedOnly := id == customAudiencesRemoveID
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
		t.Run(id+" over a read connection", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" with the effects and no tools list", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: effects})
			e.fake.routes["POST "+customAudiencePath(clientID)] = okReply(customAudienceNames("11", "12"))
			if id == customAudiencesCreateID {
				e.fake.routes["POST "+customAudiencePath(clientID)] = okReply(customAudienceNames("21", "22"))
			}
			// The confirmation is required whichever way the tool is offered.
			if _, err := e.invoke(id, arguments); err == nil {
				t.Fatal("ran without a confirmation")
			}
			_, err := e.invokeConfirmed(id, arguments)
			if listedOnly {
				if err == nil {
					t.Fatal("remove ran without a tools list")
				}
				e.untouched(t)
			} else if err != nil || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("the effect still decides for the tools that need no list", func(t *testing.T) {
		e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
			config.EffectCreate}})
		e.fake.routes["POST "+customAudiencePath(clientID)] = okReply(customAudienceNames("11", "12"))
		if _, err := e.invokeConfirmed(customAudiencesUpdateID, customAudienceUpdateArgs); err == nil {
			t.Fatal("update ran on a connection without the update effect")
		}
		e.untouched(t)
	})
}

func TestCustomAudienceTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for id, arguments := range customAudienceCalls {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + otherID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCustomAudienceArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	pre := `{"customer_id":"` + clientID + `",`
	create := func(audience string) string { return pre + `"custom_audiences":[` + audience + `]}` }
	update := func(audience string) string { return pre + `"custom_audiences":[` + audience + `]}` }
	many := func(n int, item string) string { return strings.TrimSuffix(strings.Repeat(item+",", n), ",") }
	member := func(kind, value string) string {
		return `{"name":"A","members":[{"member_type":"` + kind + `","value":"` + value + `"}]}`
	}
	bad := map[string]struct{ id, args string }{
		"foreign argument":       {customAudiencesCreateID, pre + `"custom_audiences":[{"name":"A"}],"partial_failure":true}`},
		"response content type":  {customAudiencesCreateID, pre + `"custom_audiences":[{"name":"A"}],"response_content_type":"MUTABLE_RESOURCE"}`},
		"resource name argument": {customAudiencesUpdateID, update(`{"custom_audience_id":"1","resource_name":"customers/9/customAudiences/1","name":"A"}`)},
		"no audiences":           {customAudiencesCreateID, pre + `"custom_audiences":[]}`},
		"eleven audiences":       {customAudiencesCreateID, create(many(11, `{"name":"A"}`))},
		"no name":                {customAudiencesCreateID, create(`{"description":"x"}`)},
		"blank name":             {customAudiencesCreateID, create(`{"name":"   "}`)},
		"control in name":        {customAudiencesCreateID, create(`{"name":"a\nb"}`)},
		"name twice":             {customAudiencesCreateID, create(`{"name":"A"},{"name":"A"}`)},
		"interest type":          {customAudiencesCreateID, create(`{"name":"A","type":"INTEREST"}`)},
		"purchase intent type":   {customAudiencesUpdateID, update(`{"custom_audience_id":"1","type":"PURCHASE_INTENT"}`)},
		"unknown member type":    {customAudiencesCreateID, create(member("EMAIL", "a"))},
		"keyword 11 words":       {customAudiencesCreateID, create(member("KEYWORD", "a b c d e f g h i j k"))},
		"keyword 81 characters":  {customAudiencesCreateID, create(member("KEYWORD", strings.Repeat("a", 81)))},
		"url without scheme":     {customAudiencesCreateID, create(member("URL", "www.example.com/x"))},
		"url other scheme":       {customAudiencesCreateID, create(member("URL", "ftp://example.com/x"))},
		"url without host":       {customAudiencesCreateID, create(member("URL", "https:///x"))},
		"url with space":         {customAudiencesCreateID, create(member("URL", "https://example.com/a b"))},
		"url too long":           {customAudiencesCreateID, create(member("URL", "https://example.com/"+strings.Repeat("a", 2040)))},
		"app with space":         {customAudiencesCreateID, create(member("APP", "com example"))},
		"app without dot":        {customAudiencesCreateID, create(member("APP", "example"))},
		"place category text":    {customAudiencesCreateID, create(member("PLACE_CATEGORY", "park"))},
		"place category zero":    {customAudiencesCreateID, create(member("PLACE_CATEGORY", "0"))},
		"101 members": {customAudiencesCreateID, create(`{"name":"A","members":[` +
			many(101, `{"member_type":"KEYWORD","value":"a"}`) + `]}`)},
		"update without id":      {customAudiencesUpdateID, update(`{"name":"A"}`)},
		"update id text":         {customAudiencesUpdateID, update(`{"custom_audience_id":"customers/1/customAudiences/2","name":"A"}`)},
		"update id zero":         {customAudiencesUpdateID, update(`{"custom_audience_id":"0","name":"A"}`)},
		"update id too large":    {customAudiencesUpdateID, update(`{"custom_audience_id":"9223372036854775808","name":"A"}`)},
		"update nothing":         {customAudiencesUpdateID, update(`{"custom_audience_id":"1"}`)},
		"update blank name":      {customAudiencesUpdateID, update(`{"custom_audience_id":"1","name":""}`)},
		"update id twice":        {customAudiencesUpdateID, update(`{"custom_audience_id":"1","name":"A"},{"custom_audience_id":"1","name":"B"}`)},
		"create with an id":      {customAudiencesCreateID, create(`{"custom_audience_id":"5","name":"A"}`)},
		"remove no ids":          {customAudiencesRemoveID, pre + `"custom_audience_ids":[]}`},
		"remove eleven ids":      {customAudiencesRemoveID, pre + `"custom_audience_ids":["1","2","3","4","5","6","7","8","9","10","11"]}`},
		"remove twice":           {customAudiencesRemoveID, pre + `"custom_audience_ids":["1","1"]}`},
		"remove resource name":   {customAudiencesRemoveID, pre + `"custom_audience_ids":["customers/` + otherID + `/customAudiences/1"]}`},
		"remove foreign account": {customAudiencesRemoveID, `{"customer_id":"12345","custom_audience_ids":["1"]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c.id, c.args); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestCustomAudienceCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+customAudiencePath(clientID)] = okReply(customAudienceNames("21", "22"))
	out, err := e.invokeConfirmed(customAudiencesCreateID, customAudienceCreateArgs)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	list := got["custom_audiences"].([]any)
	first := list[0].(map[string]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(list) != 2 ||
		first["resource_name"] != "customers/"+queryNumber(clientID)+"/customAudiences/21" || first["custom_audience_id"] != "21" {
		t.Errorf("out = %s", out)
	}
	if len(e.fake.api()) != 1 || len(e.posts()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := e.posts()[0]
	if p.url != "https://googleads.googleapis.com"+customAudiencePath(clientID) || p.login != loginID || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	want := map[string]any{"operations": wantWire(t, `[{"create":{"name":"Hiking","description":"Walkers","type":"SEARCH",`+
		`"members":[{"memberType":"KEYWORD","keyword":"trail shoes"},{"memberType":"URL","url":`+
		`"https://www.example.com/hiking"},{"memberType":"PLACE_CATEGORY","placeCategory":"88"},`+
		`{"memberType":"APP","app":"com.example.hike"}]}},{"create":{"name":"Cycling"}}]`)}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %s", p.body)
	}
	for _, member := range []string{"partialFailure", "validateOnly", "responseContentType", `"operation"`} {
		if strings.Contains(p.body, member) {
			t.Errorf("body %s carries %s", p.body, member)
		}
	}
}

func TestCustomAudienceUpdateMasksOnlyWhatIsGiven(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+customAudiencePath(clientID)] = okReply(customAudienceNames("12", "11"))
	out, err := e.invokeConfirmed(customAudiencesUpdateID, customAudienceUpdateArgs)
	if err != nil {
		t.Fatal(err)
	}
	if list := decode(t, out)["custom_audiences"].([]any); len(list) != 2 {
		t.Errorf("out = %s", out)
	}
	name := func(id string) string { return "customers/" + queryNumber(clientID) + "/customAudiences/" + id }
	want := []any{
		map[string]any{"updateMask": "name,description,members", "update": map[string]any{"resourceName": name("11"),
			"name": "Hiking 2", "description": "", "members": wantWire(t, `[{"memberType":"KEYWORD","keyword":"boots"}]`)}},
		map[string]any{"updateMask": "type", "update": map[string]any{"resourceName": name("12"), "type": "AUTO"}},
	}
	if got := bodyOf(t, e.posts()[0])["operations"]; !reflect.DeepEqual(got, want) {
		t.Errorf("operations = %v", got)
	}
}

func TestCustomAudienceRemoveSendsTheBuiltNames(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+customAudiencePath(clientID)] = okReply(customAudienceNames("11", "12"))
	if _, err := e.invokeConfirmed(customAudiencesRemoveID, customAudienceRemoveArgs); err != nil {
		t.Fatal(err)
	}
	prefix := "customers/" + queryNumber(clientID) + "/customAudiences/"
	want := []any{map[string]any{"remove": prefix + "11"}, map[string]any{"remove": prefix + "12"}}
	if got := bodyOf(t, e.posts()[0])["operations"]; !reflect.DeepEqual(got, want) {
		t.Errorf("operations = %v", got)
	}
}

func TestCustomAudienceValidateOnlyAnswersWithoutResults(t *testing.T) {
	for id, arguments := range customAudienceCalls {
		for _, answer := range []string{`{}`, `{"results":[]}`, `{"results":[{},{}]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+customAudiencePath(clientID)] = okReply(answer)
				out, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+`,"validate_only":true}`)
				if err != nil {
					t.Fatal(err)
				}
				got := decode(t, out)
				if got["validate_only"] != true || got["custom_audiences"] != nil {
					t.Errorf("out = %s", out)
				}
				if posts := e.posts(); len(posts) != 1 || bodyOf(t, posts[0])["validateOnly"] != true {
					t.Errorf("posts = %+v", posts)
				}
			})
		}
	}
}

// An answer for another account, another audience, or another number of audiences than asked for never
// passes. The request was sent, so the error says that the outcome is unclear.
func TestCustomAudienceResultsAreBoundToTheRequest(t *testing.T) {
	foreign := `{"results":[{"resourceName":"customers/` + queryNumber(otherID) + `/customAudiences/21"},` +
		`{"resourceName":"customers/` + queryNumber(clientID) + `/customAudiences/22"}]}`
	own := func(names ...string) string { return customAudienceNames(names...) }
	cases := map[string]struct{ id, args, body string }{
		"create foreign account":   {customAudiencesCreateID, customAudienceCreateArgs, foreign},
		"create too few":           {customAudiencesCreateID, customAudienceCreateArgs, own("21")},
		"create too many":          {customAudiencesCreateID, customAudienceCreateArgs, own("21", "22", "23")},
		"create twice the same":    {customAudiencesCreateID, customAudienceCreateArgs, own("21", "21")},
		"create not an audience":   {customAudiencesCreateID, customAudienceCreateArgs, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) + `/campaigns/1"},{"resourceName":"x"}]}`},
		"create no answer":         {customAudiencesCreateID, customAudienceCreateArgs, `{}`},
		"create empty names":       {customAudiencesCreateID, customAudienceCreateArgs, `{"results":[{},{}]}`},
		"update another audience":  {customAudiencesUpdateID, customAudienceUpdateArgs, own("11", "13")},
		"update foreign account":   {customAudiencesUpdateID, customAudienceUpdateArgs, foreign},
		"update too few":           {customAudiencesUpdateID, customAudienceUpdateArgs, own("11")},
		"update twice the same":    {customAudiencesUpdateID, customAudienceUpdateArgs, own("11", "11")},
		"remove another audience":  {customAudiencesRemoveID, customAudienceRemoveArgs, own("11", "99")},
		"remove foreign account":   {customAudiencesRemoveID, customAudienceRemoveArgs, foreign},
		"remove no answer":         {customAudiencesRemoveID, customAudienceRemoveArgs, `{}`},
		"validate only with names": {customAudiencesRemoveID, strings.TrimSuffix(customAudienceRemoveArgs, "}") + `,"validate_only":true}`, own("11", "99")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+customAudiencePath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.args)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestCustomAudienceErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"name already used", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "customAudienceError",
			"NAME_ALREADY_USED", "")}, provider.ClassProviderError, "customAudienceError NAME_ALREADY_USED), nothing was changed", false},
		{"validation of a member", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "fieldError",
			"INVALID_VALUE_WITH_DURATION", "")}, provider.ClassProviderError, "nothing was changed", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"cut connection", reply{err: errors.New("connection reset by peer")}, provider.ClassUnreachable,
			"outcome of the change is unknown", true},
		{"not json", reply{status: 200, body: "<html>", header: http.Header{"Content-Type": {"text/html"}}},
			provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+customAudiencePath(clientID)] = c.reply
			_, err := e.invokeConfirmed(customAudiencesCreateID, customAudienceCreateArgs)
			if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) {
				t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
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

func TestCustomAudienceMemberDataIsNeverFetched(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+customAudiencePath(clientID)] = okReply(customAudienceNames("21", "22"))
	if _, err := e.invokeConfirmed(customAudiencesCreateID, customAudienceCreateArgs); err != nil {
		t.Fatal(err)
	}
	for _, s := range e.fake.api() {
		if !strings.HasPrefix(s.url, "https://googleads.googleapis.com/") || strings.Contains(s.url, "example.com") {
			t.Errorf("request %s leaves the API origin", s.url)
		}
	}
}

func TestCustomAudienceSummariesNameEveryObject(t *testing.T) {
	create := customAudienceSummary(false)(json.RawMessage(customAudienceCreateArgs))
	for _, word := range []string{clientID, "Hiking", "Cycling", "SEARCH", "1 keyword", "1 url", "1 place_category", "1 app"} {
		if !strings.Contains(create, word) {
			t.Errorf("create summary %q lacks %q", create, word)
		}
	}
	update := customAudienceSummary(true)(json.RawMessage(customAudienceUpdateArgs))
	for _, word := range []string{clientID, "audience 11", "audience 12", "Hiking 2", "description", "replaced", "type AUTO"} {
		if !strings.Contains(update, word) {
			t.Errorf("update summary %q lacks %q", update, word)
		}
	}
	remove := customAudienceRemoveSummary(json.RawMessage(strings.TrimSuffix(customAudienceRemoveArgs, "}") + `,"validate_only":true}`))
	for _, word := range []string{clientID, "11", "12", "Only validate", "no longer"} {
		if !strings.Contains(remove, word) {
			t.Errorf("remove summary %q lacks %q", remove, word)
		}
	}
	if again := customAudienceRemoveSummary(json.RawMessage(customAudienceRemoveArgs)); strings.Contains(again, "validate") {
		t.Errorf("summary %q kept state", again)
	}
	if !slices.Contains(changeToolIDs, customAudiencesRemoveID) {
		t.Error("remove is not a change tool")
	}
}

// A manager account has custom audiences of its own: on a connection with targets it needs a manager target
// that names it. A customer target that names a manager is refused after one read and before any change.
func TestCustomAudiencesOnManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerRow := func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	swap := func(args string) string { return strings.Replace(args, clientID, managerID, 1) }
	for id, arguments := range customAudienceCalls {
		t.Run(id+" customer target naming a manager", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = managerRow
			_, err := e.invokeConfirmed(id, swap(arguments))
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
			if got := e.fake.api(); len(got) != 1 || !strings.Contains(got[0].query, "customer.manager") {
				t.Errorf("requests = %+v", got)
			}
		})
		t.Run(id+" manager target naming it", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"managers/" + managerID}, nil)
			name := func(n string) string {
				return `{"resourceName":"customers/` + queryNumber(managerID) + `/customAudiences/` + n + `"}`
			}
			e.fake.routes["POST "+customAudiencePath(managerID)] = okReply(`{"results":[` + name("11") + `,` + name("12") + `]}`)
			if _, err := e.invokeConfirmed(id, swap(arguments)); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 || len(e.posts()) != 1 {
				t.Errorf("requests = %+v, want the one change and no read", got)
			}
		})
	}
}
