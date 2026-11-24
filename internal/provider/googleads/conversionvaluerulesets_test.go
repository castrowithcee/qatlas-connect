package googleads

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func ruleSetPath(id string) string { return "/v26/customers/" + id + "/conversionValueRuleSets:mutate" }

func ruleSetResourceName(id, n string) string {
	return "customers/" + queryNumber(id) + "/conversionValueRuleSets/" + n
}

var ruleSetToolIDs = []string{conversionValueRuleSetsCreateID, conversionValueRuleSetsUpdateID, conversionValueRuleSetsRemoveID}

const (
	ruleSetCreateArgs = `"conversion_value_rule_sets":[{"attachment_type":"CAMPAIGN","campaign":"customers/1000000003/campaigns/9",` +
		`"conversion_action_categories":["PURCHASE","SIGNUP"],"dimensions":["DEVICE","GEO_LOCATION"],` +
		`"conversion_value_rules":["customers/1000000003/conversionValueRules/11"]}]`
	ruleSetUpdateArgs = `"conversion_value_rule_sets":[{"conversion_value_rule_set_id":"7","dimensions":["AUDIENCE"],` +
		`"conversion_value_rules":["customers/1000000003/conversionValueRules/12"]}]`
	ruleSetRemoveArgs = `"conversion_value_rule_set_ids":["7","8"]`
)

func ruleSetCalls() map[string]string {
	return map[string]string{
		conversionValueRuleSetsCreateID: `{"customer_id":"` + clientID + `",` + ruleSetCreateArgs + `}`,
		conversionValueRuleSetsUpdateID: `{"customer_id":"` + clientID + `",` + ruleSetUpdateArgs + `}`,
		conversionValueRuleSetsRemoveID: `{"customer_id":"` + clientID + `",` + ruleSetRemoveArgs + `}`,
	}
}

func TestConversionValueRuleSetToolsAreDescribed(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	for _, id := range ruleSetToolIDs {
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
		// status is output only and a set is removed only by remove.
		for _, banned := range []string{"partial_failure", "owner_customer", "resource_name", "login_customer_id", "REMOVED", `"status"`} {
			if strings.Contains(string(d.InputSchema), banned) {
				t.Errorf("%s offers %s", id, banned)
			}
		}
		if _, found := input.Properties["validate_only"]; !found || len(d.OutputSchema) == 0 ||
			!strings.Contains(d.Description, "10") || !d.RequiresToolAllowList {
			t.Errorf("%s: validate_only, output schema, bound or tools list missing", id)
		}
	}
	d, _, _ := e.reg.Lookup(conversionValueRuleSetsUpdateID)
	for _, immutable := range []string{"attachment_type", "conversion_action_categories"} {
		if strings.Contains(string(d.InputSchema), `"`+immutable+`"`) {
			t.Errorf("update offers the immutable %s", immutable)
		}
	}
}

func TestConversionValueRuleSetChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	for id, arguments := range ruleSetCalls() {
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
		t.Run(id+" target", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invokeConfirmed(id, strings.Replace(arguments, clientID, otherID, 1))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestConversionValueRuleSetArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	sets := func(set string) string {
		return `{"customer_id":"` + clientID + `","conversion_value_rule_sets":[` + set + `]}`
	}
	const customer = `"attachment_type":"CUSTOMER"`
	cases := []struct{ id, arguments string }{
		{conversionValueRuleSetsCreateID, sets(`{}`)},
		{conversionValueRuleSetsCreateID, sets(`{"attachment_type":"ACCOUNT"}`)},
		{conversionValueRuleSetsCreateID, sets(`{"attachment_type":"CAMPAIGN"}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"campaign":"customers/` + clientID + `/campaigns/9"}`)},
		{conversionValueRuleSetsCreateID, sets(`{"attachment_type":"CAMPAIGN","campaign":"customers/` + otherID + `/campaigns/9"}`)},
		{conversionValueRuleSetsCreateID, sets(`{"attachment_type":"CAMPAIGN","campaign":"customers/` + clientID + `/campaigns/9/../8"}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"status":"PAUSED"}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"conversion_value_set_id":"5"}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"conversion_value_rule_set_id":"5"}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"conversion_action_categories":["UNKNOWN"]}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"conversion_action_categories":["PURCHASE","PURCHASE"]}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"dimensions":["AGE"]}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"dimensions":["DEVICE","DEVICE"]}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"conversion_value_rules":["customers/` + otherID + `/conversionValueRules/11"]}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"conversion_value_rules":["customers/` + clientID + `/conversionValueRules/x"]}`)},
		{conversionValueRuleSetsCreateID, sets(`{` + customer + `,"conversion_value_rules":["customers/` + clientID + `/conversionValueRules/1","customers/` + clientID + `/conversionValueRules/1"]}`)},
		{conversionValueRuleSetsCreateID, `{"customer_id":"` + clientID + `","conversion_value_rule_sets":[]}`},
		{conversionValueRuleSetsUpdateID, sets(`{"conversion_value_rule_set_id":"5"}`)},
		{conversionValueRuleSetsUpdateID, sets(`{"dimensions":["DEVICE"]}`)},
		{conversionValueRuleSetsUpdateID, sets(`{"conversion_value_rule_set_id":"5","attachment_type":"CUSTOMER"}`)},
		{conversionValueRuleSetsUpdateID, sets(`{"conversion_value_rule_set_id":"5","conversion_action_categories":["PURCHASE"]}`)},
		{conversionValueRuleSetsUpdateID, sets(`{"conversion_value_rule_set_id":"5","status":"PAUSED"}`)},
		{conversionValueRuleSetsUpdateID, sets(`{"conversion_value_rule_set_id":"0","dimensions":["DEVICE"]}`)},
		{conversionValueRuleSetsUpdateID, sets(`{"conversion_value_rule_set_id":"customers/` + otherID + `/conversionValueRuleSets/5","dimensions":["DEVICE"]}`)},
		{conversionValueRuleSetsUpdateID, sets(`{"conversion_value_rule_set_id":"5","campaign":"customers/` + otherID + `/campaigns/9"}`)},
		{conversionValueRuleSetsUpdateID, sets(`{"conversion_value_rule_set_id":"5","dimensions":["DEVICE"]},{"conversion_value_rule_set_id":"5","dimensions":["AUDIENCE"]}`)},
		{conversionValueRuleSetsRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_set_ids":["5","5"]}`},
		{conversionValueRuleSetsRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_set_ids":["abc"]}`},
		{conversionValueRuleSetsRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_set_ids":[]}`},
		{conversionValueRuleSetsRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_set_ids":["1","2","3","4","5","6","7","8","9","10","11"]}`},
		{conversionValueRuleSetsRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_set_ids":["5"],"partial_failure":true}`},
		{conversionValueRuleSetsRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_set_ids":["5"],"response_content_type":"MUTABLE_RESOURCE"}`},
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

func TestConversionValueRuleSetCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+ruleSetPath(clientID)] = okReply(`{"results":[{"resourceName":"` + ruleSetResourceName(clientID, "777") + `"}]}`)
	out, err := e.invokeConfirmed(conversionValueRuleSetsCreateID, ruleSetCalls()[conversionValueRuleSetsCreateID])
	if err != nil {
		t.Fatal(err)
	}
	first := decode(t, out)["results"].([]any)[0].(map[string]any)
	if first["conversion_value_rule_set_id"] != "777" || first["resource_name"] != ruleSetResourceName(clientID, "777") ||
		first["conversion_value_rule_set"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || posts[0].login != loginID || posts[0].developerToken != "" {
		t.Fatalf("posts = %+v", posts)
	}
	body := bodyOf(t, posts[0])
	if len(body) != 1 || strings.Contains(posts[0].body, "partialFailure") {
		t.Errorf("body = %s, want the operations alone", posts[0].body)
	}
	op := body["operations"].([]any)[0].(map[string]any)
	create := op["create"].(map[string]any)
	if _, found := op["updateMask"]; found || create["attachmentType"] != "CAMPAIGN" ||
		create["campaign"] != "customers/"+clientID+"/campaigns/9" ||
		create["conversionActionCategories"].([]any)[1] != "SIGNUP" ||
		create["dimensions"].([]any)[0] != "DEVICE" ||
		create["conversionValueRules"].([]any)[0] != "customers/"+clientID+"/conversionValueRules/11" ||
		create["resourceName"] != nil || create["status"] != nil {
		t.Errorf("create = %v", create)
	}
}

func TestConversionValueRuleSetUpdateSendsTheMaskOfTheGivenSettings(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+ruleSetPath(clientID)] = okReply(`{"results":[{"resourceName":"` + ruleSetResourceName(clientID, "7") + `"}]}`)
	if _, err := e.invokeConfirmed(conversionValueRuleSetsUpdateID, ruleSetCalls()[conversionValueRuleSetsUpdateID]); err != nil {
		t.Fatal(err)
	}
	op := bodyOf(t, e.posts()[0])["operations"].([]any)[0].(map[string]any)
	update := op["update"].(map[string]any)
	if op["updateMask"] != "conversionValueRules,dimensions" || update["resourceName"] != ruleSetResourceName(clientID, "7") ||
		len(update) != 3 {
		t.Errorf("operation = %v", op)
	}
}

func TestConversionValueRuleSetRemoveSendsOneRemovePerID(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+ruleSetPath(clientID)] = okReply(`{"results":[{"resourceName":"` + ruleSetResourceName(clientID, "7") +
		`"},{"resourceName":"` + ruleSetResourceName(clientID, "8") + `"}]}`)
	out, err := e.invokeConfirmed(conversionValueRuleSetsRemoveID, ruleSetCalls()[conversionValueRuleSetsRemoveID])
	if err != nil {
		t.Fatal(err)
	}
	operations := bodyOf(t, e.posts()[0])["operations"].([]any)
	if len(decode(t, out)["results"].([]any)) != 2 || len(operations) != 2 ||
		operations[0].(map[string]any)["remove"] != ruleSetResourceName(clientID, "7") ||
		operations[1].(map[string]any)["remove"] != ruleSetResourceName(clientID, "8") {
		t.Errorf("out = %s, operations = %v", out, operations)
	}
}

func TestConversionValueRuleSetValidateOnlyAnswersWithoutResults(t *testing.T) {
	for id, arguments := range ruleSetCalls() {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+ruleSetPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if results, _ := decode(t, out)["results"].([]any); decode(t, out)["validate_only"] != true || len(results) != 0 {
				t.Errorf("out = %s", out)
			}
			if bodyOf(t, e.posts()[0])["validateOnly"] != true {
				t.Errorf("body = %s", e.posts()[0].body)
			}
		})
	}
}

func TestConversionValueRuleSetMutableResourceIsMappedAndBound(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	name := ruleSetResourceName(clientID, "777")
	e.fake.routes["POST "+ruleSetPath(clientID)] = okReply(`{"results":[{"resourceName":"` + name + `","conversionValueRuleSet":{` +
		`"resourceName":"` + name + `","id":"777","ownerCustomer":"customers/` + clientID + `","status":"ENABLED\u0007",` +
		`"attachmentType":"CAMPAIGN","campaign":"customers/` + clientID + `/campaigns/9",` +
		`"conversionActionCategories":["PURCHASE"],"dimensions":["DEVICE"],` +
		`"conversionValueRules":["customers/` + clientID + `/conversionValueRules/11"]}}]}`)
	out, err := e.invokeConfirmed(conversionValueRuleSetsCreateID, strings.TrimSuffix(ruleSetCalls()[conversionValueRuleSetsCreateID], "}")+
		`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	set := decode(t, out)["results"].([]any)[0].(map[string]any)["conversion_value_rule_set"].(map[string]any)
	if set["id"] != "777" || set["owner_customer"] != "customers/"+clientID || set["status"] != "ENABLED " ||
		set["attachment_type"] != "CAMPAIGN" || set["campaign"] != "customers/"+clientID+"/campaigns/9" ||
		set["conversion_action_categories"].([]any)[0] != "PURCHASE" || set["dimensions"].([]any)[0] != "DEVICE" ||
		set["conversion_value_rules"].([]any)[0] != "customers/"+clientID+"/conversionValueRules/11" {
		t.Errorf("set = %v", set)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that does not fit the request is never taken for the change: the request was sent, so the error
// says that the outcome is unknown.
func TestConversionValueRuleSetAnswersAreBoundToTheCheckedAccountAndRequest(t *testing.T) {
	good := ruleSetResourceName(clientID, "7")
	foreign := ruleSetResourceName(otherID, "7")
	withSet := func(set string) string {
		return `{"results":[{"resourceName":"` + good + `","conversionValueRuleSet":` + set + `}]}`
	}
	cases := map[string]struct{ id, body string }{
		"foreign account":  {conversionValueRuleSetsUpdateID, `{"results":[{"resourceName":"` + foreign + `"}]}`},
		"other set":        {conversionValueRuleSetsUpdateID, `{"results":[{"resourceName":"` + ruleSetResourceName(clientID, "9") + `"}]}`},
		"no results":       {conversionValueRuleSetsUpdateID, `{}`},
		"too many":         {conversionValueRuleSetsUpdateID, `{"results":[{"resourceName":"` + good + `"},{"resourceName":"` + good + `"}]}`},
		"create foreign":   {conversionValueRuleSetsCreateID, `{"results":[{"resourceName":"` + foreign + `"}]}`},
		"create no id":     {conversionValueRuleSetsCreateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) + `/conversionValueRuleSets/x"}]}`},
		"remove twice":     {conversionValueRuleSetsRemoveID, `{"results":[{"resourceName":"` + good + `"},{"resourceName":"` + good + `"}]}`},
		"partial failure":  {conversionValueRuleSetsUpdateID, `{"results":[{"resourceName":"` + good + `"}],"partialFailureError":{"code":3}}`},
		"set of another":   {conversionValueRuleSetsUpdateID, withSet(`{"resourceName":"` + foreign + `"}`)},
		"id of another":    {conversionValueRuleSetsUpdateID, withSet(`{"id":"1"}`)},
		"owner of another": {conversionValueRuleSetsUpdateID, withSet(`{"ownerCustomer":"customers/` + otherID + `"}`)},
		"foreign campaign": {conversionValueRuleSetsUpdateID, withSet(`{"campaign":"customers/` + otherID + `/campaigns/5"}`)},
		"foreign rule": {conversionValueRuleSetsUpdateID, withSet(`{"conversionValueRules":["customers/` +
			otherID + `/conversionValueRules/5"]}`)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+ruleSetPath(clientID)] = okReply(c.body)
			arguments := strings.TrimSuffix(ruleSetCalls()[c.id], "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
			if c.id == conversionValueRuleSetsRemoveID {
				arguments = `{"customer_id":"` + clientID + `","conversion_value_rule_set_ids":["7"]}`
			}
			_, err := e.invokeConfirmed(c.id, arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestConversionValueRuleSetErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		unknown bool
	}{
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, false},
		{"set error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "conversionValueRuleSetError",
			"INVALID_DIMENSION_TYPE", "")}, provider.ClassProviderError, false},
		{"timeout", reply{err: errors.New("i/o timeout")}, provider.ClassUnreachable, true},
	}
	for _, c := range cases {
		for id, arguments := range ruleSetCalls() {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+ruleSetPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, arguments)
				if classOf(err) != c.class {
					t.Fatalf("err = %v, want %s", err, c.class)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != c.unknown {
					t.Errorf("unknown outcome reported = %v, want %v: %v", got, c.unknown, err)
				}
				if len(e.posts()) != 1 {
					t.Errorf("posts = %d, want exactly one", len(e.posts()))
				}
			})
		}
	}
}
