package googleads

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var bidModifierToolIDs = []string{adGroupBidModifiersCreateID, adGroupBidModifiersUpdateID, adGroupBidModifiersRemoveID}

func bidModifiersPath(id string) string {
	return "/v26/customers/" + id + "/adGroupBidModifiers:mutate"
}

var bidModifierGroup = `"ad_group":"` + own + `/adGroups/31"`

func bidModifierCall(items, extra string) string {
	return `{"customer_id":"` + clientID + `","ad_group_bid_modifiers":` + items + extra + `}`
}

func bidModifierCreateArgs(members string) string {
	return bidModifierCall(`[{`+bidModifierGroup+members+`}]`, "")
}

func bidModifierUpdateArgs(members string) string {
	return bidModifierCall(`[{"resource_name":"`+own+`/adGroupBidModifiers/31~30001"`+members+`}]`, "")
}

func bidModifierRemoveArgs(extra string) string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroupBidModifiers/31~30001"]` + extra + `}`
}

func bidModifierResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

const mobile = `,"bid_modifier":1.2,"device":{"type":"MOBILE"}`

func TestAdGroupBidModifierToolsAreListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range bidModifierToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Fatalf("%s = %+v", id, d)
		}
		schema := string(d.InputSchema)
		for _, absent := range []string{"login_customer_id", "partial_failure", "base_ad_group", "criterion_id",
			"bid_modifier_source", "hotel_date_selection_type"} {
			if strings.Contains(schema, `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
		if !strings.Contains(d.Description, "spend") {
			t.Errorf("%s does not name the effect on spend", id)
		}
	}
	update, _, _ := reg.Lookup(adGroupBidModifiersUpdateID)
	for _, absent := range []string{`"ad_group"`, `"device"`, `"start_date_range"`} {
		if strings.Contains(string(update.InputSchema), absent) {
			t.Errorf("update offers %s, which only creation accepts", absent)
		}
	}
	create, _, _ := reg.Lookup(adGroupBidModifiersCreateID)
	for _, c := range adGroupBidModifierCriteria {
		if !strings.Contains(string(create.InputSchema), `"`+c+`"`) {
			t.Errorf("create lacks criterion %s", c)
		}
	}
	remove, _, _ := reg.Lookup(adGroupBidModifiersRemoveID)
	if strings.Contains(string(remove.InputSchema), "response_content_type") {
		t.Error("remove offers a response content type")
	}
}

func TestAdGroupBidModifierMembersAreFieldsOfTheCatalog(t *testing.T) {
	var walk func(n node, path string)
	walk = func(n node, path string) {
		for _, f := range n.fields {
			full := path + "." + f.name
			if _, ok := LookupField(full); f.kind != kindObject && !ok {
				t.Errorf("%s is not in the catalog", full)
			}
			walk(f, full)
		}
	}
	walk(adGroupBidModifierOutputTree, "ad_group_bid_modifier")
}

func TestAdGroupBidModifierChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		adGroupBidModifiersCreateID: bidModifierCreateArgs(mobile),
		adGroupBidModifiersUpdateID: bidModifierUpdateArgs(`,"bid_modifier":1.5`),
		adGroupBidModifiersRemoveID: bidModifierRemoveArgs(""),
	}
	for id, arguments := range calls {
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
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
		t.Run(id+" outside the targets", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + otherID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestAdGroupBidModifierSummaryNamesTheSpendingEffect(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(adGroupBidModifiersCreateID, bidModifierCall(`[{`+bidModifierGroup+mobile+`}]`, `,"validate_only":true`))
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "adGroups/31", "bid_modifier 1.2", "device", "MOBILE", "how much it costs"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(adGroupBidModifiersRemoveID, bidModifierRemoveArgs(""))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "spend") {
		t.Errorf("summary = %v", err)
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestAdGroupBidModifierArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	c, u, r := adGroupBidModifiersCreateID, adGroupBidModifiersUpdateID, adGroupBidModifiersRemoveID
	bad := map[string]struct{ id, arguments string }{
		"no modifier":    {c, bidModifierCreateArgs(`,"device":{"type":"MOBILE"}`)},
		"no criterion":   {c, bidModifierCreateArgs(`,"bid_modifier":1.2`)},
		"no ad group":    {c, bidModifierCall(`[{"bid_modifier":1.2,"device":{"type":"MOBILE"}}]`, "")},
		"two criteria":   {c, bidModifierCreateArgs(`,"bid_modifier":1.2,"device":{"type":"MOBILE"},"user_set_dates":{"type":"USER_SELECTED"}`)},
		"too low":        {c, bidModifierCreateArgs(`,"bid_modifier":0.09,"device":{"type":"MOBILE"}`)},
		"too high":       {c, bidModifierCreateArgs(`,"bid_modifier":10.01,"device":{"type":"MOBILE"}`)},
		"negative":       {c, bidModifierCreateArgs(`,"bid_modifier":-1,"device":{"type":"MOBILE"}`)},
		"zero booking":   {c, bidModifierCreateArgs(`,"bid_modifier":0,"length_of_booking":{"min_days":1}`)},
		"not a number":   {c, bidModifierCreateArgs(`,"bid_modifier":"1.2","device":{"type":"MOBILE"}`)},
		"unknown device": {c, bidModifierCreateArgs(`,"bid_modifier":1.2,"device":{"type":"WATCH"}`)},
		"empty device":   {c, bidModifierCreateArgs(`,"bid_modifier":1.2,"device":{}`)},
		"bad date":       {c, bidModifierCreateArgs(`,"bid_modifier":1.2,"start_date_range":{"start_date":"2026-02-30"}`)},
		"date form":      {c, bidModifierCreateArgs(`,"bid_modifier":1.2,"start_date_range":{"start_date":"2026-1-5"}`)},
		"negative days":  {c, bidModifierCreateArgs(`,"bid_modifier":1.2,"advance_booking_window":{"min_days":-1}`)},
		"old hotel name": {c, bidModifierCreateArgs(`,"bid_modifier":1.2,"hotel_length_of_stay":{"min_nights":1}`)},
		"output only":    {c, bidModifierCreateArgs(mobile + `,"criterion_id":30001`)},
		"base ad group":  {c, bidModifierCreateArgs(mobile + `,"base_ad_group":"` + own + `/adGroups/31"`)},
		"source":         {c, bidModifierCreateArgs(mobile + `,"bid_modifier_source":"CAMPAIGN"`)},
		"none":           {c, bidModifierCall(`[]`, "")},
		"eleven":         {c, bidModifierCall(`[`+strings.Repeat(`{`+bidModifierGroup+mobile+`},`, 10)+`{`+bidModifierGroup+mobile+`}]`, "")},
		"foreign group":  {c, bidModifierCall(`[{"ad_group":"`+foreign+`/adGroups/31"`+mobile+`}]`, "")},
		"temp group":     {c, bidModifierCall(`[{"ad_group":"`+own+`/adGroups/-1"`+mobile+`}]`, "")},
		"group kind":     {c, bidModifierCall(`[{"ad_group":"`+own+`/campaigns/31"`+mobile+`}]`, "")},
		"content type":   {c, bidModifierCall(`[{`+bidModifierGroup+mobile+`}]`, `,"response_content_type":"ALL"`)},

		"update no member": {u, bidModifierUpdateArgs(``)},
		"update low":       {u, bidModifierUpdateArgs(`,"bid_modifier":0.05`)},
		"update high":      {u, bidModifierUpdateArgs(`,"bid_modifier":11`)},
		"update group":     {u, bidModifierUpdateArgs(`,"ad_group":"` + own + `/adGroups/32"`)},
		"update criterion": {u, bidModifierUpdateArgs(`,"bid_modifier":1.1,"device":{"type":"TABLET"}`)},
		"update foreign":   {u, bidModifierCall(`[{"resource_name":"`+foreign+`/adGroupBidModifiers/31~30001","bid_modifier":1.1}]`, "")},
		"update temporary": {u, bidModifierCall(`[{"resource_name":"`+own+`/adGroupBidModifiers/-31~30001","bid_modifier":1.1}]`, "")},
		"update temp crit": {u, bidModifierCall(`[{"resource_name":"`+own+`/adGroupBidModifiers/31~-30001","bid_modifier":1.1}]`, "")},
		"update no tilde":  {u, bidModifierCall(`[{"resource_name":"`+own+`/adGroupBidModifiers/31","bid_modifier":1.1}]`, "")},
		"update kind":      {u, bidModifierCall(`[{"resource_name":"`+own+`/adGroups/31","bid_modifier":1.1}]`, "")},
		"update twice": {u, bidModifierCall(`[{"resource_name":"`+own+`/adGroupBidModifiers/31~30001","bid_modifier":1.1},`+
			`{"resource_name":"`+own+`/adGroupBidModifiers/31~30001","bid_modifier":1.3}]`, "")},

		"remove foreign":   {r, `{"customer_id":"` + clientID + `","resource_names":["` + foreign + `/adGroupBidModifiers/31~30001"]}`},
		"remove temporary": {r, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroupBidModifiers/31~-1"]}`},
		"remove twice": {r, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adGroupBidModifiers/31~30001","` +
			own + `/adGroupBidModifiers/31~30001"]}`},
		"remove none":    {r, `{"customer_id":"` + clientID + `","resource_names":[]}`},
		"remove content": {r, bidModifierRemoveArgs(`,"response_content_type":"MUTABLE_RESOURCE"`)},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c.id, c.arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestAdGroupBidModifiersCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+bidModifiersPath(clientID)] = bidModifierResults(own+"/adGroupBidModifiers/31~30001",
		own+"/adGroupBidModifiers/32~30000", own+"/adGroupBidModifiers/31~30002", own+"/adGroupBidModifiers/31~30003",
		own+"/adGroupBidModifiers/31~30004", own+"/adGroupBidModifiers/31~30005")
	out, err := e.invokeConfirmed(adGroupBidModifiersCreateID, bidModifierCall(`[`+
		`{`+bidModifierGroup+`,"bid_modifier":1.2,"device":{"type":"MOBILE"}},`+
		`{"ad_group":"`+own+`/adGroups/32","bid_modifier":0,"device":{"type":"DESKTOP"}},`+
		`{`+bidModifierGroup+`,"bid_modifier":10,"advance_booking_window":{"min_days":0,"max_days":7}},`+
		`{`+bidModifierGroup+`,"bid_modifier":0.1,"length_of_booking":{"min_days":2}},`+
		`{`+bidModifierGroup+`,"bid_modifier":1.5,"start_date_range":{"start_date":"2026-12-01","end_date":"2026-12-31"}},`+
		`{`+bidModifierGroup+`,"bid_modifier":2,"start_day_of_week":{"day_of_week":"FRIDAY"}}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 6 ||
		results[0].(map[string]any)["resource_name"] != own+"/adGroupBidModifiers/31~30001" ||
		results[0].(map[string]any)["ad_group_bid_modifier"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+bidModifiersPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	want := wantWire(t, `[{"create":{"adGroup":"`+own+`/adGroups/31","bidModifier":1.2,"device":{"type":"MOBILE"}}},`+
		`{"create":{"adGroup":"`+own+`/adGroups/32","bidModifier":0,"device":{"type":"DESKTOP"}}},`+
		`{"create":{"adGroup":"`+own+`/adGroups/31","bidModifier":10,"advanceBookingWindow":{"minDays":0,"maxDays":7}}},`+
		`{"create":{"adGroup":"`+own+`/adGroups/31","bidModifier":0.1,"lengthOfBooking":{"minDays":2}}},`+
		`{"create":{"adGroup":"`+own+`/adGroups/31","bidModifier":1.5,"startDateRange":{"startDate":"2026-12-01","endDate":"2026-12-31"}}},`+
		`{"create":{"adGroup":"`+own+`/adGroups/31","bidModifier":2,"startDayOfWeek":{"dayOfWeek":"FRIDAY"}}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

func TestAdGroupBidModifiersUpdateNamesOnlyTheBidModifierInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+bidModifiersPath(clientID)] = bidModifierResults(own+"/adGroupBidModifiers/31~30001",
		own+"/adGroupBidModifiers/32~30001")
	_, err := e.invokeConfirmed(adGroupBidModifiersUpdateID, bidModifierCall(`[{"resource_name":"`+own+
		`/adGroupBidModifiers/31~30001","bid_modifier":1.5},{"resource_name":"`+own+`/adGroupBidModifiers/32~30001","bid_modifier":0}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, second := ops[0].(map[string]any), ops[1].(map[string]any)
	if first["updateMask"] != "bidModifier" || !reflect.DeepEqual(first["update"],
		wantWire(t, `{"resourceName":"`+own+`/adGroupBidModifiers/31~30001","bidModifier":1.5}`)) {
		t.Errorf("first operation = %v", first)
	}
	if second["updateMask"] != "bidModifier" || !reflect.DeepEqual(second["update"],
		wantWire(t, `{"resourceName":"`+own+`/adGroupBidModifiers/32~30001","bidModifier":0}`)) {
		t.Errorf("second operation = %v", second)
	}
}

func TestAdGroupBidModifiersRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+bidModifiersPath(clientID)] = bidModifierResults(own+"/adGroupBidModifiers/31~30001",
		own+"/adGroupBidModifiers/32~30002")
	out, err := e.invokeConfirmed(adGroupBidModifiersRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+own+
		`/adGroupBidModifiers/31~30001","`+own+`/adGroupBidModifiers/32~30002"]}`)
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+own+`/adGroupBidModifiers/31~30001"},{"remove":"`+own+
		`/adGroupBidModifiers/32~30002"}]`)) || len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestAdGroupBidModifierValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		adGroupBidModifiersCreateID: bidModifierCall(`[{`+bidModifierGroup+mobile+`}]`, `,"validate_only":true`),
		adGroupBidModifiersUpdateID: bidModifierCall(`[{"resource_name":"`+own+`/adGroupBidModifiers/31~30001","bid_modifier":1.1}]`,
			`,"validate_only":true`),
		adGroupBidModifiersRemoveID: bidModifierRemoveArgs(`,"validate_only":true`),
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+bidModifiersPath(clientID)] = okReply(answer)
				out, err := e.invokeConfirmed(id, arguments)
				got := decode(t, out)
				if err != nil || got["validate_only"] != true || got["results"] != nil {
					t.Fatalf("out=%s err=%v", out, err)
				}
				if bodyOf(t, e.posts()[0])["validateOnly"] != true || len(e.posts()) != 1 {
					t.Errorf("posts = %+v", e.posts())
				}
			})
		}
	}
}

func TestAdGroupBidModifierResponseContentTypeShowsTheMutableResource(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+bidModifiersPath(clientID)] = okReply(`{"results":[{"resourceName":"` + own +
		`/adGroupBidModifiers/31~30001","adGroupBidModifier":{"resourceName":"` + own + `/adGroupBidModifiers/31~30001",` +
		`"adGroup":"` + own + `/adGroups/31","baseAdGroup":"` + own + `/adGroups/31","criterionId":"30001",` +
		`"bidModifier":1.2,"bidModifierSource":"AD_GROUP","device":{"type":"MOBILE"},"unknownMember":"never held"}}]}`)
	out, err := e.invokeConfirmed(adGroupBidModifiersUpdateID, bidModifierCall(`[{"resource_name":"`+own+
		`/adGroupBidModifiers/31~30001","bid_modifier":1.2}]`, `,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)["results"].([]any)[0].(map[string]any)["ad_group_bid_modifier"].(map[string]any)
	want := map[string]any{"resource_name": own + "/adGroupBidModifiers/31~30001", "ad_group": own + "/adGroups/31",
		"base_ad_group": own + "/adGroups/31", "criterion_id": float64(30001), "bid_modifier": 1.2,
		"bid_modifier_source": "AD_GROUP", "device": map[string]any{"type": "MOBILE"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bid modifier = %v", got)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that names another account, another ad group, another bid modifier, or another number of results
// than asked for never passes. The change was made as far as Qatlas can tell, so the error says that the
// outcome is unknown.
func TestAdGroupBidModifierAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	update := bidModifierUpdateArgs(`,"bid_modifier":1.2`)
	update = strings.TrimSuffix(update, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
	create := bidModifierCall(`[{`+bidModifierGroup+mobile+`}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	good := own + "/adGroupBidModifiers/31~30001"
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":     {adGroupBidModifiersCreateID, create, `{"results":[{"resourceName":"` + foreign + `/adGroupBidModifiers/31~30001"}]}`},
		"other ad group":     {adGroupBidModifiersCreateID, create, `{"results":[{"resourceName":"` + own + `/adGroupBidModifiers/99~30001"}]}`},
		"other modifier":     {adGroupBidModifiersUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroupBidModifiers/31~30002"}]}`},
		"campaign modifier":  {adGroupBidModifiersCreateID, create, `{"results":[{"resourceName":"` + own + `/campaignBidModifiers/31~30001"}]}`},
		"temporary":          {adGroupBidModifiersCreateID, create, `{"results":[{"resourceName":"` + own + `/adGroupBidModifiers/31~-1"}]}`},
		"no name":            {adGroupBidModifiersUpdateID, update, `{"results":[{}]}`},
		"no results":         {adGroupBidModifiersUpdateID, update, `{}`},
		"too many":           {adGroupBidModifiersCreateID, create, `{"results":[{"resourceName":"` + good + `"},{"resourceName":"` + good + `"}]}`},
		"partial failure":    {adGroupBidModifiersUpdateID, update, `{"results":[{"resourceName":"` + good + `"}],"partialFailureError":{"code":3}}`},
		"foreign ad group":   {adGroupBidModifiersUpdateID, update, `{"results":[{"resourceName":"` + good + `","adGroupBidModifier":{"adGroup":"` + foreign + `/adGroups/31"}}]}`},
		"other group inside": {adGroupBidModifiersUpdateID, update, `{"results":[{"resourceName":"` + good + `","adGroupBidModifier":{"adGroup":"` + own + `/adGroups/32"}}]}`},
		"foreign base":       {adGroupBidModifiersUpdateID, update, `{"results":[{"resourceName":"` + good + `","adGroupBidModifier":{"baseAdGroup":"` + foreign + `/adGroups/31"}}]}`},
		"other name inside":  {adGroupBidModifiersUpdateID, update, `{"results":[{"resourceName":"` + good + `","adGroupBidModifier":{"resourceName":"` + own + `/adGroupBidModifiers/31~30002"}}]}`},
		"wrong kind":         {adGroupBidModifiersUpdateID, update, `{"results":[{"resourceName":"` + good + `","adGroupBidModifier":{"device":"x"}}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+bidModifiersPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAdGroupBidModifierFailuresMapWithoutProviderText(t *testing.T) {
	arguments := bidModifierCreateArgs(mobile)
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "rangeError", "TOO_LOW", ""), "", provider.ClassProviderError},
		"permission": {403, adsFailure(403, "PERMISSION_DENIED", "authorizationError", "USER_PERMISSION_DENIED", ""), "",
			provider.ClassPermission},
		"quota": {429, adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED",
			`{"rateScope":"ACCOUNT","retryDelay":"3s"}`), "", provider.ClassRateLimited},
		"server": {503, googleError(503, "UNAVAILABLE", ""), "unknown", provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+bidModifiersPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(adGroupBidModifiersCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+bidModifiersPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(adGroupBidModifiersCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A manager account has no ad groups: a customer target that names one is refused before the change.
func TestAdGroupBidModifierChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(adGroupBidModifiersRemoveID, `{"customer_id":"`+managerID+`","resource_names":["customers/`+
		queryNumber(managerID)+`/adGroupBidModifiers/31~30001"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
