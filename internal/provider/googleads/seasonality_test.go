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

var seasonalityToolIDs = []string{seasonalityCreateID, seasonalityUpdateID, seasonalityRemoveID}

func seasonalityPath(id string) string {
	return "/v26/customers/" + id + "/biddingSeasonalityAdjustments:mutate"
}

func seasonalityName(id, n string) string {
	return "customers/" + queryNumber(id) + "/biddingSeasonalityAdjustments/" + n
}

const seasonalityBase = `"scope":"CAMPAIGN","start_date_time":"2027-03-01 00:00:00","end_date_time":"2027-03-04 00:00:00"`

func seasonalityCall(items, extra string) string {
	return `{"customer_id":"` + clientID + `","adjustments":` + items + extra + `}`
}

func seasonalityCreateArgs(members string) string {
	return seasonalityCall(`[{`+seasonalityBase+members+`}]`, "")
}

func seasonalityUpdateArgs(members string) string {
	return seasonalityCall(`[{"resource_name":"`+seasonalityName(clientID, "5")+`"`+members+`}]`, "")
}

func seasonalityRemoveArgs(names ...string) string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + strings.Join(names, `","`) + `"]}`
}

func seasonalityResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

func TestSeasonalityToolsAreDescribedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range seasonalityToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Fatalf("%s = %+v", id, d)
		}
		schema := string(d.InputSchema)
		for _, absent := range []string{"login_customer_id", "partial_failure", "operations", "status",
			"seasonality_adjustment_id", "CUSTOMER", "UNSPECIFIED", "UNKNOWN"} {
			if strings.Contains(schema, `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	create, _, _ := reg.Lookup(seasonalityCreateID)
	for _, present := range []string{`"conversion_rate_modifier"`, `"advertising_channel_types"`, `"devices"`,
		`"minimum":0.1`, `"maximum":10`, `"maxItems":2000`, `"enum":["DISPLAY","SEARCH","SHOPPING"]`,
		`"required":["scope","start_date_time","end_date_time"]`} {
		if !strings.Contains(string(create.InputSchema), present) {
			t.Errorf("seasonality create lacks %s", present)
		}
	}
	remove, _, _ := reg.Lookup(seasonalityRemoveID)
	if strings.Contains(string(remove.InputSchema), "response_content_type") {
		t.Error("remove offers a response content type")
	}
	if !strings.Contains(string(create.OutputSchema), "seasonality_adjustment_id") ||
		!strings.Contains(string(create.OutputSchema), `"status"`) {
		t.Error("the output schema lacks the ID and the status")
	}
}

// Every scalar member is a field of the catalog of v26.
func TestSeasonalityMembersAreFieldsOfTheCatalog(t *testing.T) {
	for _, f := range seasonalityOutputTree.fields {
		if _, ok := LookupField("bidding_seasonality_adjustment." + f.name); !ok {
			t.Errorf("%s is not in the catalog", f.name)
		}
	}
}

func TestSeasonalityChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		seasonalityCreateID: seasonalityCreateArgs(``),
		seasonalityUpdateID: seasonalityUpdateArgs(`,"conversion_rate_modifier":2`),
		seasonalityRemoveID: seasonalityRemoveArgs(seasonalityName(clientID, "5")),
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

func TestSeasonalitySummaryNamesTheEffect(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(seasonalityCreateID, seasonalityCall(`[{"name":"Spring sale",`+seasonalityBase+
		`,"conversion_rate_modifier":1.5,"campaigns":["`+own+`/campaigns/21"]}]`, `,"validate_only":true`))
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "Spring sale", "CAMPAIGN", "2027-03-01", "1.5", "1 campaign", "spend"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(seasonalityRemoveID, seasonalityRemoveArgs(seasonalityName(clientID, "5")))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "no tool brings it back") {
		t.Errorf("summary = %v", err)
	}
	_, err = e.invoke(seasonalityUpdateID, seasonalityUpdateArgs(`,"devices":[],"name":"x"`))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "members devices+name") {
		t.Errorf("summary = %v", err)
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestSeasonalityArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	campaign := `"` + own + `/campaigns/21"`
	bad := map[string]struct{ id, arguments string }{
		"no scope": {seasonalityCreateID, seasonalityCall(`[{"start_date_time":"2027-03-01 00:00:00","end_date_time":"2027-03-04 00:00:00"}]`, "")},
		"no start": {seasonalityCreateID, seasonalityCall(`[{"scope":"CAMPAIGN","end_date_time":"2027-03-04 00:00:00"}]`, "")},
		"no end":   {seasonalityCreateID, seasonalityCall(`[{"scope":"CAMPAIGN","start_date_time":"2027-03-04 00:00:00"}]`, "")},
		"customer scope": {seasonalityCreateID, seasonalityCall(`[{"scope":"CUSTOMER","start_date_time":"2027-03-01 00:00:00",`+
			`"end_date_time":"2027-03-04 00:00:00"}]`, "")},
		"no adjustment":  {seasonalityCreateID, seasonalityCall(`[]`, "")},
		"eleven":         {seasonalityCreateID, seasonalityCall(`[`+strings.Repeat(`{`+seasonalityBase+`},`, 10)+`{`+seasonalityBase+`}]`, "")},
		"date only":      {seasonalityCreateID, seasonalityCreateArgs(`,"start_date_time":"2027-03-01"`)},
		"unreal date":    {seasonalityCreateID, seasonalityCreateArgs(`,"start_date_time":"2027-02-30 00:00:00"`)},
		"end at start":   {seasonalityCreateID, seasonalityCreateArgs(`,"end_date_time":"2027-03-01 00:00:00"`)},
		"end before":     {seasonalityCreateID, seasonalityCreateArgs(`,"end_date_time":"2027-02-28 00:00:00"`)},
		"fifteen days":   {seasonalityCreateID, seasonalityCreateArgs(`,"end_date_time":"2027-03-16 00:00:01"`)},
		"modifier low":   {seasonalityCreateID, seasonalityCreateArgs(`,"conversion_rate_modifier":0.05`)},
		"modifier high":  {seasonalityCreateID, seasonalityCreateArgs(`,"conversion_rate_modifier":10.5`)},
		"modifier text":  {seasonalityCreateID, seasonalityCreateArgs(`,"conversion_rate_modifier":"2"`)},
		"long name":      {seasonalityCreateID, seasonalityCreateArgs(`,"name":"` + strings.Repeat("n", 256) + `"`)},
		"control name":   {seasonalityCreateID, seasonalityCreateArgs(`,"name":"a\nb"`)},
		"long text":      {seasonalityCreateID, seasonalityCreateArgs(`,"description":"` + strings.Repeat("d", 2049) + `"`)},
		"bad device":     {seasonalityCreateID, seasonalityCreateArgs(`,"devices":["WATCH"]`)},
		"bad channel":    {seasonalityCreateID, seasonalityCreateArgs(`,"advertising_channel_types":["VIDEO"]`)},
		"foreign":        {seasonalityCreateID, seasonalityCreateArgs(`,"campaigns":["` + foreign + `/campaigns/21"]`)},
		"temporary":      {seasonalityCreateID, seasonalityCreateArgs(`,"campaigns":["` + own + `/campaigns/-1"]`)},
		"wrong resource": {seasonalityCreateID, seasonalityCreateArgs(`,"campaigns":["` + own + `/campaignBudgets/1"]`)},
		"too many campaigns": {seasonalityCreateID, seasonalityCreateArgs(`,"campaigns":[` +
			strings.Repeat(campaign+`,`, 2000) + campaign + `]`)},
		"both lists":                 {seasonalityCreateID, seasonalityCreateArgs(`,"campaigns":[` + campaign + `],"advertising_channel_types":["SEARCH"]`)},
		"channels on campaign scope": {seasonalityCreateID, seasonalityCreateArgs(`,"advertising_channel_types":["SEARCH"]`)},
		"campaigns on channel scope": {seasonalityCreateID, seasonalityCreateArgs(`,"scope":"CHANNEL","campaigns":[` + campaign + `]`)},
		"status":                     {seasonalityCreateID, seasonalityCreateArgs(`,"status":"ENABLED"`)},
		"id":                         {seasonalityCreateID, seasonalityCreateArgs(`,"seasonality_adjustment_id":5`)},
		"unknown member":             {seasonalityCreateID, seasonalityCreateArgs(`,"partial_failure":true`)},
		"content type":               {seasonalityCreateID, seasonalityCall(`[{`+seasonalityBase+`}]`, `,"response_content_type":"ALL"`)},

		"update no member":  {seasonalityUpdateID, seasonalityUpdateArgs(``)},
		"update foreign":    {seasonalityUpdateID, seasonalityCall(`[{"resource_name":"`+seasonalityName(otherID, "5")+`","name":"x"}]`, "")},
		"update temporary":  {seasonalityUpdateID, seasonalityCall(`[{"resource_name":"`+seasonalityName(clientID, "-5")+`","name":"x"}]`, "")},
		"update collection": {seasonalityUpdateID, seasonalityCall(`[{"resource_name":"`+own+`/campaigns/5","name":"x"}]`, "")},
		"update twice": {seasonalityUpdateID, seasonalityCall(`[{"resource_name":"`+seasonalityName(clientID, "5")+`","name":"x"},`+
			`{"resource_name":"`+seasonalityName(clientID, "5")+`","name":"y"}]`, "")},
		"update modifier": {seasonalityUpdateID, seasonalityUpdateArgs(`,"conversion_rate_modifier":11`)},
		"update interval": {seasonalityUpdateID, seasonalityUpdateArgs(`,"start_date_time":"2027-03-10 00:00:00","end_date_time":"2027-03-01 00:00:00"`)},
		"update status":   {seasonalityUpdateID, seasonalityUpdateArgs(`,"status":"REMOVED"`)},
		"update both":     {seasonalityUpdateID, seasonalityUpdateArgs(`,"campaigns":[` + campaign + `],"advertising_channel_types":["SEARCH"]`)},

		"remove foreign":   {seasonalityRemoveID, seasonalityRemoveArgs(seasonalityName(otherID, "5"))},
		"remove temporary": {seasonalityRemoveID, seasonalityRemoveArgs(seasonalityName(clientID, "-5"))},
		"remove twice":     {seasonalityRemoveID, seasonalityRemoveArgs(seasonalityName(clientID, "5"), seasonalityName(clientID, "5"))},
		"remove none":      {seasonalityRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
		"remove content": {seasonalityRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + seasonalityName(clientID, "5") +
			`"],"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer": {seasonalityRemoveID, `{"customer_id":"12-34","resource_names":["` + seasonalityName(clientID, "5") + `"]}`},
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

func TestSeasonalityCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+seasonalityPath(clientID)] = seasonalityResults(seasonalityName(clientID, "5"), seasonalityName(clientID, "6"))
	out, err := e.invokeConfirmed(seasonalityCreateID, seasonalityCall(`[{"name":"Spring sale","description":"Sale of the season",`+
		`"scope":"CAMPAIGN","start_date_time":"2027-03-01 00:00:00","end_date_time":"2027-03-15 00:00:00",`+
		`"conversion_rate_modifier":1.5,"devices":["MOBILE","DESKTOP"],"campaigns":["`+own+`/campaigns/21","`+own+`/campaigns/22"]},`+
		`{"scope":"CHANNEL","start_date_time":"2027-04-01 08:30:00","end_date_time":"2027-04-02 08:30:00",`+
		`"advertising_channel_types":["SEARCH","SHOPPING"]}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
		results[0].(map[string]any)["resource_name"] != seasonalityName(clientID, "5") ||
		results[1].(map[string]any)["resource_name"] != seasonalityName(clientID, "6") || results[0].(map[string]any)["adjustment"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+seasonalityPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	want := wantWire(t, `[{"create":{"name":"Spring sale","description":"Sale of the season","scope":"CAMPAIGN",`+
		`"startDateTime":"2027-03-01 00:00:00","endDateTime":"2027-03-15 00:00:00","conversionRateModifier":1.5,`+
		`"devices":["MOBILE","DESKTOP"],"campaigns":["`+own+`/campaigns/21","`+own+`/campaigns/22"]}},`+
		`{"create":{"scope":"CHANNEL","startDateTime":"2027-04-01 08:30:00","endDateTime":"2027-04-02 08:30:00",`+
		`"advertisingChannelTypes":["SEARCH","SHOPPING"]}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

func TestSeasonalityUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+seasonalityPath(clientID)] = seasonalityResults(seasonalityName(clientID, "5"), seasonalityName(clientID, "6"))
	_, err := e.invokeConfirmed(seasonalityUpdateID, seasonalityCall(`[{"resource_name":"`+seasonalityName(clientID, "5")+
		`","conversion_rate_modifier":2,"end_date_time":"2027-03-05 00:00:00","campaigns":["`+own+`/campaigns/21"]},`+
		`{"resource_name":"`+seasonalityName(clientID, "6")+`","description":"","devices":[],"scope":"CHANNEL",`+
		`"campaigns":[],"advertising_channel_types":["DISPLAY"]}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, second := ops[0].(map[string]any), ops[1].(map[string]any)
	if first["updateMask"] != "campaigns,conversionRateModifier,endDateTime" ||
		!reflect.DeepEqual(first["update"], wantWire(t, `{"resourceName":"`+seasonalityName(clientID, "5")+
			`","conversionRateModifier":2,"endDateTime":"2027-03-05 00:00:00","campaigns":["`+own+`/campaigns/21"]}`)) {
		t.Errorf("first operation = %v", first)
	}
	// An empty text and an empty list are in the mask, and the lists hold nothing.
	if second["updateMask"] != "advertisingChannelTypes,campaigns,description,devices,scope" ||
		!reflect.DeepEqual(second["update"], wantWire(t, `{"resourceName":"`+seasonalityName(clientID, "6")+
			`","devices":[],"scope":"CHANNEL","campaigns":[],"advertisingChannelTypes":["DISPLAY"]}`)) {
		t.Errorf("second operation = %v", second)
	}
}

func TestSeasonalityRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	a, b := seasonalityName(clientID, "5"), seasonalityName(clientID, "6")
	e.fake.routes["POST "+seasonalityPath(clientID)] = seasonalityResults(a, b)
	out, err := e.invokeConfirmed(seasonalityRemoveID, seasonalityRemoveArgs(a, b))
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+a+`"},{"remove":"`+b+`"}]`)) || len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestSeasonalityValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		seasonalityCreateID: seasonalityCall(`[{`+seasonalityBase+`}]`, `,"validate_only":true`),
		seasonalityUpdateID: seasonalityCall(`[{"resource_name":"`+seasonalityName(clientID, "5")+`","name":"x"}]`, `,"validate_only":true`),
		seasonalityRemoveID: `{"customer_id":"` + clientID + `","resource_names":["` + seasonalityName(clientID, "5") + `"],"validate_only":true}`,
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+seasonalityPath(clientID)] = okReply(answer)
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

func TestSeasonalityResponseContentTypeShowsTheMutableAdjustment(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	name := seasonalityName(clientID, "5")
	e.fake.routes["POST "+seasonalityPath(clientID)] = okReply(`{"results":[{"resourceName":"` + name + `",` +
		`"biddingSeasonalityAdjustment":{"resourceName":"` + name + `","seasonalityAdjustmentId":"5","name":"Spring sale",` +
		`"scope":"CAMPAIGN","status":"ENABLED","startDateTime":"2027-03-01 00:00:00","endDateTime":"2027-03-04 00:00:00",` +
		`"conversionRateModifier":1.5,"devices":["MOBILE"],"campaigns":["` + own + `/campaigns/21"],"unknownMember":"never held"}}]}`)
	out, err := e.invokeConfirmed(seasonalityUpdateID, seasonalityCall(`[{"resource_name":"`+name+`","name":"Spring sale"}]`,
		`,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	adjustment := decode(t, out)["results"].([]any)[0].(map[string]any)["adjustment"].(map[string]any)
	want := map[string]any{"resource_name": name, "seasonality_adjustment_id": float64(5), "name": "Spring sale",
		"scope": "CAMPAIGN", "status": "ENABLED", "start_date_time": "2027-03-01 00:00:00",
		"end_date_time": "2027-03-04 00:00:00", "conversion_rate_modifier": 1.5, "devices": []any{"MOBILE"},
		"campaigns": []any{own + "/campaigns/21"}}
	if !reflect.DeepEqual(adjustment, want) {
		t.Errorf("adjustment = %v", adjustment)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that names another account, another adjustment, or another number of results than asked for never
// passes. The change may have been made, so the error says that the outcome is unknown.
func TestSeasonalityAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	name := seasonalityName(clientID, "5")
	update := seasonalityCall(`[{"resource_name":"`+name+`","name":"x"}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	create := seasonalityCall(`[{`+seasonalityBase+`}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)
	two := seasonalityCall(`[{`+seasonalityBase+`},{`+seasonalityBase+`}]`, "")
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":   {seasonalityCreateID, create, `{"results":[{"resourceName":"` + seasonalityName(otherID, "5") + `"}]}`},
		"other adjustment": {seasonalityUpdateID, update, `{"results":[{"resourceName":"` + seasonalityName(clientID, "9") + `"}]}`},
		"no name":          {seasonalityUpdateID, update, `{"results":[{}]}`},
		"no results":       {seasonalityUpdateID, update, `{}`},
		"too many":         {seasonalityCreateID, create, `{"results":[{"resourceName":"` + name + `"},{"resourceName":"` + seasonalityName(clientID, "6") + `"}]}`},
		"same twice":       {seasonalityCreateID, two, `{"results":[{"resourceName":"` + name + `"},{"resourceName":"` + name + `"}]}`},
		"partial failure":  {seasonalityUpdateID, update, `{"results":[{"resourceName":"` + name + `"}],"partialFailureError":{"code":3}}`},
		"foreign campaign": {seasonalityUpdateID, update, `{"results":[{"resourceName":"` + name + `","biddingSeasonalityAdjustment":{"resourceName":"` +
			name + `","campaigns":["` + foreign + `/campaigns/1"]}}]}`},
		"other adjustment inside": {seasonalityUpdateID, update, `{"results":[{"resourceName":"` + name + `","biddingSeasonalityAdjustment":{"resourceName":"` +
			seasonalityName(clientID, "9") + `"}}]}`},
		"member of the wrong kind": {seasonalityUpdateID, update, `{"results":[{"resourceName":"` + name + `","biddingSeasonalityAdjustment":{"resourceName":"` +
			name + `","devices":"MOBILE"}}]}`},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+seasonalityPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestSeasonalityFailuresMapWithoutProviderTextAndAreNeverRepeated(t *testing.T) {
	arguments := seasonalityCreateArgs(``)
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "biddingError", "BIDDING_STRATEGY_NOT_SUPPORTED", ""), "",
			provider.ClassProviderError},
		"permission": {403, adsFailure(403, "PERMISSION_DENIED", "authorizationError", "USER_PERMISSION_DENIED", ""), "",
			provider.ClassPermission},
		"quota": {429, adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED",
			`{"rateScope":"ACCOUNT","retryDelay":"3s"}`), "", provider.ClassRateLimited},
		"server": {503, googleError(503, "UNAVAILABLE", ""), "unknown", provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+seasonalityPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(seasonalityCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+seasonalityPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(seasonalityCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A manager account cannot hold seasonality adjustments: a customer target that names one is refused before the
// change, and a manager target is the only way to reach it.
func TestSeasonalityChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(seasonalityRemoveID, `{"customer_id":"`+managerID+`","resource_names":["`+
		seasonalityName(managerID, "5")+`"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
