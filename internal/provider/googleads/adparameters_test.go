package googleads

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func adParametersPath(id string) string { return "/v26/customers/" + id + "/adParameters:mutate" }

var (
	criterionName   = own + "/adGroupCriteria/31~51"
	adParameterName = own + "/adParameters/31~51~1"
)

func adParametersCall(items, extra string) string {
	return `{"customer_id":"` + clientID + `","ad_parameters":` + items + extra + `}`
}

func adParameterCreateArgs(members string) string {
	return adParametersCall(`[{"ad_group_criterion":"`+criterionName+`","parameter_index":1,"insertion_text":"$99.99"`+members+`}]`, "")
}

func adParameterUpdateArgs(members string) string {
	return adParametersCall(`[{"resource_name":"`+adParameterName+`"`+members+`}]`, "")
}

func adParameterRemoveArgs(extra string) string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + adParameterName + `"]` + extra + `}`
}

func adParameterResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

var adParameterCalls = map[string]string{
	adParametersCreateID: adParameterCreateArgs(``),
	adParametersUpdateID: adParameterUpdateArgs(`,"insertion_text":"$89.99"`),
	adParametersRemoveID: adParameterRemoveArgs(""),
}

func TestAdParameterToolsAreOfferedAsTheirEffectAndStayNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for id, listed := range map[string]bool{adParametersCreateID: false, adParametersUpdateID: false, adParametersRemoveID: true} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.RequiresToolAllowList != listed || d.Risk.Confirmation != capability.ConfirmationRequired || d.Version != 1 {
			t.Fatalf("%s = %+v", id, d)
		}
		for _, absent := range []string{"login_customer_id", "partial_failure", "operations", "local_path"} {
			if strings.Contains(string(d.InputSchema), `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	update, _, _ := reg.Lookup(adParametersUpdateID)
	for _, absent := range []string{"ad_group_criterion", "parameter_index"} {
		if strings.Contains(string(update.InputSchema), absent) {
			t.Errorf("ad-parameters.update offers %s, which Google marks immutable", absent)
		}
	}
	remove, _, _ := reg.Lookup(adParametersRemoveID)
	if strings.Contains(string(remove.InputSchema), "response_content_type") {
		t.Error("ad-parameters.remove offers a response content type")
	}
	var input struct {
		Properties struct {
			AdParameters struct {
				Items struct {
					Properties map[string]map[string]any `json:"properties"`
				} `json:"items"`
			} `json:"ad_parameters"`
		} `json:"properties"`
	}
	create, _, _ := reg.Lookup(adParametersCreateID)
	if json.Unmarshal(create.InputSchema, &input) != nil {
		t.Fatal("schema does not decode")
	}
	index := input.Properties.AdParameters.Items.Properties["parameter_index"]
	if index["minimum"] != float64(1) || index["maximum"] != float64(2) {
		t.Errorf("parameter_index = %v, want 1 to 2", index)
	}
}

// create and update need the effect of the tool and the confirmation of every call, remove the tools list too.
func TestAdParameterChangesNeedTheirEffectOrTheToolsListAndTheConfirmation(t *testing.T) {
	for id, arguments := range adParameterCalls {
		t.Run(id+" on a read connection", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a read connection ran the tool")
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
	for _, id := range []string{adParametersCreateID, adParametersUpdateID} {
		t.Run(id+" by the effect", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
				config.EffectCreate, config.EffectUpdate}})
			_, err := e.invoke(id, adParameterCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) {
				t.Fatalf("err = %v, want a confirmation", err)
			}
			e.untouched(t)
		})
	}
	t.Run("remove by the effect only", func(t *testing.T) {
		e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
			config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}})
		if _, err := e.invokeConfirmed(adParametersRemoveID, adParameterCalls[adParametersRemoveID]); err == nil {
			t.Fatal("a connection with all effects but no tools list ran ad-parameters.remove")
		}
		e.untouched(t)
	})
}

func TestAdParameterSummaryNamesTheEffects(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	var needed *application.ConfirmationRequiredError
	_, err := e.invoke(adParametersCreateID, adParametersCall(`[{"ad_group_criterion":"`+criterionName+
		`","parameter_index":2,"insertion_text":"99.99USD"}]`, `,"validate_only":true`))
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "adGroupCriteria/31~51", "index 2", "99.99USD", "show this number"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(adParametersRemoveID, adParameterRemoveArgs(""))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "31~51~1") || !strings.Contains(needed.Summary, "no longer get") {
		t.Errorf("summary = %v", err)
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestAdParameterArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	item := func(members string) string { return adParametersCall(`[{`+members+`}]`, "") }
	crit := `"ad_group_criterion":"` + criterionName + `"`
	bad := map[string]struct{ id, arguments string }{
		"no keyword":        {adParametersCreateID, item(`"parameter_index":1,"insertion_text":"1"`)},
		"no index":          {adParametersCreateID, item(crit + `,"insertion_text":"1"`)},
		"no text":           {adParametersCreateID, item(crit + `,"parameter_index":1`)},
		"index zero":        {adParametersCreateID, item(crit + `,"parameter_index":0,"insertion_text":"1"`)},
		"index three":       {adParametersCreateID, item(crit + `,"parameter_index":3,"insertion_text":"1"`)},
		"index text":        {adParametersCreateID, item(crit + `,"parameter_index":"1","insertion_text":"1"`)},
		"foreign keyword":   {adParametersCreateID, item(`"ad_group_criterion":"` + foreign + `/adGroupCriteria/31~51","parameter_index":1,"insertion_text":"1"`)},
		"temporary keyword": {adParametersCreateID, item(`"ad_group_criterion":"` + own + `/adGroupCriteria/31~-51","parameter_index":1,"insertion_text":"1"`)},
		"zero criterion":    {adParametersCreateID, item(`"ad_group_criterion":"` + own + `/adGroupCriteria/31~0","parameter_index":1,"insertion_text":"1"`)},
		"no criterion id":   {adParametersCreateID, item(`"ad_group_criterion":"` + own + `/adGroupCriteria/31","parameter_index":1,"insertion_text":"1"`)},
		"keyword kind":      {adParametersCreateID, item(`"ad_group_criterion":"` + own + `/adGroups/31","parameter_index":1,"insertion_text":"1"`)},
		"output only":       {adParametersCreateID, adParameterCreateArgs(`,"resource_name":"` + adParameterName + `"`)},
		"none":              {adParametersCreateID, adParametersCall(`[]`, "")},
		"eleven": {adParametersCreateID, adParametersCall(`[`+strings.Repeat(`{`+crit+`,"parameter_index":1,"insertion_text":"1"},`, 10)+
			`{`+crit+`,"parameter_index":2,"insertion_text":"1"}]`, "")},
		"same keyword and index": {adParametersCreateID, adParametersCall(`[{`+crit+`,"parameter_index":1,"insertion_text":"1"},{`+
			crit+`,"parameter_index":1,"insertion_text":"2"}]`, "")},
		"content type": {adParametersCreateID, adParametersCall(`[{`+crit+`,"parameter_index":1,"insertion_text":"1"}]`, `,"response_content_type":"ALL"`)},

		"update no text": {adParametersUpdateID, adParameterUpdateArgs(``)},
		"update keyword": {adParametersUpdateID, adParameterUpdateArgs(`,"insertion_text":"1","ad_group_criterion":"` + criterionName + `"`)},
		"update index":   {adParametersUpdateID, adParameterUpdateArgs(`,"insertion_text":"1","parameter_index":2`)},
		"update empty":   {adParametersUpdateID, adParameterUpdateArgs(`,"insertion_text":""`)},
		"update foreign": {adParametersUpdateID, adParametersCall(`[{"resource_name":"`+foreign+`/adParameters/31~51~1","insertion_text":"1"}]`, "")},
		"update index 3": {adParametersUpdateID, adParametersCall(`[{"resource_name":"`+own+`/adParameters/31~51~3","insertion_text":"1"}]`, "")},
		"update temp":    {adParametersUpdateID, adParametersCall(`[{"resource_name":"`+own+`/adParameters/31~-51~1","insertion_text":"1"}]`, "")},
		"update two ids": {adParametersUpdateID, adParametersCall(`[{"resource_name":"`+own+`/adParameters/31~51","insertion_text":"1"}]`, "")},
		"update kind":    {adParametersUpdateID, adParametersCall(`[{"resource_name":"`+own+`/adGroupCriteria/31~51","insertion_text":"1"}]`, "")},
		"update twice": {adParametersUpdateID, adParametersCall(`[{"resource_name":"`+adParameterName+`","insertion_text":"1"},`+
			`{"resource_name":"`+adParameterName+`","insertion_text":"2"}]`, "")},

		"remove foreign":   {adParametersRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + foreign + `/adParameters/31~51~1"]}`},
		"remove temporary": {adParametersRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adParameters/31~51~-1"]}`},
		"remove index 0":   {adParametersRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/adParameters/31~51~0"]}`},
		"remove twice": {adParametersRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + adParameterName + `","` +
			adParameterName + `"]}`},
		"remove none":    {adParametersRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
		"remove content": {adParametersRemoveID, adParameterRemoveArgs(`,"response_content_type":"MUTABLE_RESOURCE"`)},
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

func TestAdParameterInsertionTextFollowsTheDocumentedForms(t *testing.T) {
	for _, text := range []string{"99.99", "1,000,000.00", "2.000.000,10", "$99.99", "99.99$", "99.99USD", "EUR200", "1.0%", "1,0%",
		"-10.99", "25+", "4/1", "0.95/0.45", "€5", "7"} {
		if err := checkInsertionText("x", text); err != nil {
			t.Errorf("%q refused: %v", text, err)
		}
	}
	for _, text := range []string{"", "abc", "EURO200", "99 USD", "$ 5", "1;2", "<b>1</b>", "1\n", "USD", "$", "A1B", "12USD34EUR",
		"1 ", strings.Repeat("1", maxInsertionText+1)} {
		if err := checkInsertionText("x", text); err == nil {
			t.Errorf("%q accepted", text)
		}
	}
	e := newMutationEnvironment(t, nil, nil)
	if _, err := e.invokeConfirmed(adParametersUpdateID, adParameterUpdateArgs(`,"insertion_text":"cheap"`)); err == nil {
		t.Fatal("a text without a number was accepted")
	}
	e.untouched(t)
}

func TestAdParametersCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+adParametersPath(clientID)] = adParameterResults(adParameterName, own+"/adParameters/31~51~2")
	out, err := e.invokeConfirmed(adParametersCreateID, adParametersCall(`[{"ad_group_criterion":"`+criterionName+
		`","parameter_index":1,"insertion_text":"$99.99"},{"ad_group_criterion":"`+criterionName+
		`","parameter_index":2,"insertion_text":"25+"}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
		results[0].(map[string]any)["resource_name"] != adParameterName || results[0].(map[string]any)["ad_parameter"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+adParametersPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	want := wantWire(t, `[{"create":{"adGroupCriterion":"`+criterionName+`","parameterIndex":1,"insertionText":"$99.99"}},`+
		`{"create":{"adGroupCriterion":"`+criterionName+`","parameterIndex":2,"insertionText":"25+"}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

func TestAdParametersUpdateNamesTheInsertionTextInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adParametersPath(clientID)] = adParameterResults(adParameterName)
	if _, err := e.invokeConfirmed(adParametersUpdateID, adParameterUpdateArgs(`,"insertion_text":"$89.99"`)); err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	op := ops[0].(map[string]any)
	if len(ops) != 1 || op["updateMask"] != "insertionText" ||
		!reflect.DeepEqual(op["update"], wantWire(t, `{"resourceName":"`+adParameterName+`","insertionText":"$89.99"}`)) {
		t.Errorf("operations = %s", e.posts()[0].body)
	}
}

func TestAdParametersRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adParametersPath(clientID)] = adParameterResults(adParameterName, own+"/adParameters/31~51~2")
	out, err := e.invokeConfirmed(adParametersRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+adParameterName+`","`+
		own+`/adParameters/31~51~2"]}`)
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+adParameterName+`"},{"remove":"`+own+`/adParameters/31~51~2"}]`)) ||
		len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestAdParameterValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		adParametersCreateID: adParametersCall(`[{"ad_group_criterion":"`+criterionName+`","parameter_index":1,"insertion_text":"1"}]`, `,"validate_only":true`),
		adParametersUpdateID: adParametersCall(`[{"resource_name":"`+adParameterName+`","insertion_text":"1"}]`, `,"validate_only":true`),
		adParametersRemoveID: adParameterRemoveArgs(`,"validate_only":true`),
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+adParametersPath(clientID)] = okReply(answer)
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

func TestAdParameterResponseContentTypeShowsTheMutableAdParameter(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+adParametersPath(clientID)] = okReply(`{"results":[{"resourceName":"` + adParameterName +
		`","adParameter":{"resourceName":"` + adParameterName + `","adGroupCriterion":"` + criterionName +
		`","parameterIndex":"1","insertionText":"$99.99","unknownMember":"never held"}}]}`)
	out, err := e.invokeConfirmed(adParametersUpdateID, adParametersCall(`[{"resource_name":"`+adParameterName+
		`","insertion_text":"$99.99"}]`, `,"response_content_type":"MUTABLE_RESOURCE"`))
	if err != nil {
		t.Fatal(err)
	}
	item := decode(t, out)["results"].([]any)[0].(map[string]any)["ad_parameter"].(map[string]any)
	want := map[string]any{"resource_name": adParameterName, "ad_group_criterion": criterionName, "parameter_index": float64(1),
		"insertion_text": "$99.99"}
	if !reflect.DeepEqual(item, want) {
		t.Errorf("ad parameter = %v", item)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that names another account, another ad parameter, or another number of results than asked for
// never passes, and says that the outcome is unknown.
func TestAdParameterAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	content := `,"response_content_type":"MUTABLE_RESOURCE"`
	create := adParametersCall(`[{"ad_group_criterion":"`+criterionName+`","parameter_index":1,"insertion_text":"1"}]`, content)
	update := adParametersCall(`[{"resource_name":"`+adParameterName+`","insertion_text":"1"}]`, content)
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result": {adParametersCreateID, create, `{"results":[{"resourceName":"` + foreign + `/adParameters/31~51~1"}]}`},
		"other keyword":  {adParametersCreateID, create, `{"results":[{"resourceName":"` + own + `/adParameters/31~52~1"}]}`},
		"other index":    {adParametersCreateID, create, `{"results":[{"resourceName":"` + own + `/adParameters/31~51~2"}]}`},
		"other group":    {adParametersCreateID, create, `{"results":[{"resourceName":"` + own + `/adParameters/32~51~1"}]}`},
		"temporary":      {adParametersUpdateID, update, `{"results":[{"resourceName":"` + own + `/adParameters/31~51~-1"}]}`},
		"kind":           {adParametersUpdateID, update, `{"results":[{"resourceName":"` + own + `/adGroupCriteria/31~51"}]}`},
		"no name":        {adParametersUpdateID, update, `{"results":[{}]}`},
		"no results":     {adParametersUpdateID, update, `{}`},
		"too many": {adParametersUpdateID, update, `{"results":[{"resourceName":"` + adParameterName + `"},{"resourceName":"` +
			adParameterName + `"}]}`},
		"partial failure": {adParametersUpdateID, update, `{"results":[{"resourceName":"` + adParameterName + `"}],"partialFailureError":{"code":3}}`},
		"foreign keyword in": {adParametersUpdateID, update, `{"results":[{"resourceName":"` + adParameterName +
			`","adParameter":{"adGroupCriterion":"` + foreign + `/adGroupCriteria/31~51"}}]}`},
		"other keyword in": {adParametersUpdateID, update, `{"results":[{"resourceName":"` + adParameterName +
			`","adParameter":{"adGroupCriterion":"` + own + `/adGroupCriteria/31~52"}}]}`},
		"other index in": {adParametersUpdateID, update, `{"results":[{"resourceName":"` + adParameterName +
			`","adParameter":{"parameterIndex":"2"}}]}`},
		"other name in": {adParametersUpdateID, update, `{"results":[{"resourceName":"` + adParameterName +
			`","adParameter":{"resourceName":"` + own + `/adParameters/31~51~2"}}]}`},
		"member of the wrong kind": {adParametersUpdateID, update, `{"results":[{"resourceName":"` + adParameterName + `","adParameter":"x"}]}`},
		"remove foreign":           {adParametersRemoveID, adParameterRemoveArgs(""), `{"results":[{"resourceName":"` + foreign + `/adParameters/31~51~1"}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+adParametersPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAdParameterFailuresMapWithoutProviderText(t *testing.T) {
	arguments := adParameterCreateArgs(``)
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"not a keyword": {400, adsFailure(400, "INVALID_ARGUMENT", "adParameterError", "AD_GROUP_CRITERION_MUST_BE_KEYWORD", ""), "",
			provider.ClassProviderError},
		"bad text": {400, adsFailure(400, "INVALID_ARGUMENT", "adParameterError", "INVALID_INSERTION_TEXT_FORMAT", ""), "",
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
			e.fake.routes["POST "+adParametersPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(adParametersCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+adParametersPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(adParametersRemoveID, adParameterRemoveArgs(""))
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

func TestAdParameterChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(adParametersRemoveID, `{"customer_id":"`+managerID+`","resource_names":["customers/`+
		queryNumber(managerID)+`/adParameters/31~51~1"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
