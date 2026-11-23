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

func valueRulePath(id string) string { return "/v26/customers/" + id + "/conversionValueRules:mutate" }

func valueRuleResourceName(id, n string) string {
	return "customers/" + queryNumber(id) + "/conversionValueRules/" + n
}

var valueRuleToolIDs = []string{conversionValueRulesCreateID, conversionValueRulesUpdateID, conversionValueRulesRemoveID}

const (
	ruleCreateArgs = `"conversion_value_rules":[{"action":{"operation":"MULTIPLY","value":1.5},"status":"PAUSED",` +
		`"geo_location_condition":{"geo_target_constants":["geoTargetConstants/2276"],"geo_match_type":"LOCATION_OF_PRESENCE",` +
		`"excluded_geo_target_constants":["geoTargetConstants/1003"],"excluded_geo_match_type":"ANY"},` +
		`"device_condition":{"device_types":["MOBILE","TABLET"]},` +
		`"audience_condition":{"user_lists":["customers/1000000003/userLists/55"],"user_interests":["customers/1000000003/userInterests/66"]},` +
		`"itinerary_condition":{"advance_booking_window":{"min_days":1,"max_days":30},"travel_length":{"min_nights":2,"max_nights":7},` +
		`"travel_start_day":{"friday":true,"saturday":true}}}]`
	ruleUpdateArgs = `"conversion_value_rules":[{"conversion_value_rule_id":"11","action":{"operation":"SET","value":0},` +
		`"device_condition":{"device_types":["DESKTOP"]}}]`
	ruleRemoveArgs = `"conversion_value_rule_ids":["11","12"]`
)

func valueRuleCalls() map[string]string {
	return map[string]string{
		conversionValueRulesCreateID: `{"customer_id":"` + clientID + `",` + ruleCreateArgs + `}`,
		conversionValueRulesUpdateID: `{"customer_id":"` + clientID + `",` + ruleUpdateArgs + `}`,
		conversionValueRulesRemoveID: `{"customer_id":"` + clientID + `",` + ruleRemoveArgs + `}`,
	}
}

func TestConversionValueRuleToolsAreDescribed(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	for _, id := range valueRuleToolIDs {
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
		for _, banned := range []string{"partial_failure", "owner_customer", "resource_name", "login_customer_id", "REMOVED"} {
			if strings.Contains(string(d.InputSchema), banned) {
				t.Errorf("%s offers %s", id, banned)
			}
		}
		if _, found := input.Properties["validate_only"]; !found || len(d.OutputSchema) == 0 ||
			!strings.Contains(d.Description, "10") || !d.RequiresToolAllowList {
			t.Errorf("%s: validate_only, output schema, bound or tools list missing", id)
		}
	}
}

func TestConversionValueRuleChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	for id, arguments := range valueRuleCalls() {
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

func TestConversionValueRuleArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	create := func(rule string) string {
		return `{"customer_id":"` + clientID + `","conversion_value_rules":[` + rule + `]}`
	}
	const action = `"action":{"operation":"ADD","value":2}`
	cases := []struct{ id, arguments string }{
		{conversionValueRulesCreateID, create(`{}`)},
		{conversionValueRulesCreateID, create(`{"action":{"operation":"ADD"}}`)},
		{conversionValueRulesCreateID, create(`{"action":{"operation":"DIVIDE","value":2}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"status":"REMOVED"}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"conversion_value_rule_id":"5"}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"owner_customer":"customers/` + clientID + `"}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"device_condition":{}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"device_condition":{"device_types":["CONNECTED_TV"]}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"geo_location_condition":{"geo_target_constants":["customers/` + clientID + `/geoTargetConstants/1"]}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"geo_location_condition":{"geo_target_constants":["geoTargetConstants/1","geoTargetConstants/1"]}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"geo_location_condition":{"geo_match_type":"NEAR"}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"audience_condition":{"user_lists":["customers/` + otherID + `/userLists/55"]}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"audience_condition":{"user_interests":["customers/` + otherID + `/userInterests/66"]}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"audience_condition":{"user_lists":["customers/` + clientID + `/userLists/55/../56"]}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"itinerary_condition":{"travel_length":{"min_nights":-1}}}`)},
		{conversionValueRulesCreateID, create(`{` + action + `,"itinerary_condition":{"travel_start_day":{}}}`)},
		{conversionValueRulesCreateID, `{"customer_id":"` + clientID + `","conversion_value_rules":[]}`},
		{conversionValueRulesUpdateID, create(`{"conversion_value_rule_id":"5"}`)},
		{conversionValueRulesUpdateID, create(`{"status":"PAUSED"}`)},
		{conversionValueRulesUpdateID, create(`{"conversion_value_rule_id":"0","status":"PAUSED"}`)},
		{conversionValueRulesUpdateID, create(`{"conversion_value_rule_id":"customers/` + otherID + `/conversionValueRules/5","status":"PAUSED"}`)},
		{conversionValueRulesUpdateID, create(`{"conversion_value_rule_id":"5","status":"PAUSED"},{"conversion_value_rule_id":"5","status":"ENABLED"}`)},
		{conversionValueRulesRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_ids":["5","5"]}`},
		{conversionValueRulesRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_ids":["abc"]}`},
		{conversionValueRulesRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_ids":[]}`},
		{conversionValueRulesRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_ids":["1","2","3","4","5","6","7","8","9","10","11"]}`},
		{conversionValueRulesRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_ids":["5"],"partial_failure":true}`},
		{conversionValueRulesRemoveID, `{"customer_id":"` + clientID + `","conversion_value_rule_ids":["5"],"response_content_type":"MUTABLE_RESOURCE"}`},
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

func TestConversionValueRuleCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+valueRulePath(clientID)] = okReply(`{"results":[{"resourceName":"` + valueRuleResourceName(clientID, "777") + `"}]}`)
	out, err := e.invokeConfirmed(conversionValueRulesCreateID, valueRuleCalls()[conversionValueRulesCreateID])
	if err != nil {
		t.Fatal(err)
	}
	first := decode(t, out)["results"].([]any)[0].(map[string]any)
	if first["conversion_value_rule_id"] != "777" || first["resource_name"] != valueRuleResourceName(clientID, "777") ||
		first["conversion_value_rule"] != nil {
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
	geo := create["geoLocationCondition"].(map[string]any)
	itinerary := create["itineraryCondition"].(map[string]any)
	if _, found := op["updateMask"]; found || create["status"] != "PAUSED" ||
		create["action"].(map[string]any)["operation"] != "MULTIPLY" || create["action"].(map[string]any)["value"] != 1.5 ||
		geo["geoMatchType"] != "LOCATION_OF_PRESENCE" || geo["excludedGeoMatchType"] != "ANY" ||
		geo["geoTargetConstants"].([]any)[0] != "geoTargetConstants/2276" ||
		geo["excludedGeoTargetConstants"].([]any)[0] != "geoTargetConstants/1003" ||
		len(create["deviceCondition"].(map[string]any)["deviceTypes"].([]any)) != 2 ||
		create["audienceCondition"].(map[string]any)["userLists"].([]any)[0] != "customers/"+clientID+"/userLists/55" ||
		create["audienceCondition"].(map[string]any)["userInterests"].([]any)[0] != "customers/"+clientID+"/userInterests/66" ||
		itinerary["advanceBookingWindow"].(map[string]any)["maxDays"] != float64(30) ||
		itinerary["travelLength"].(map[string]any)["minNights"] != float64(2) ||
		itinerary["travelStartDay"].(map[string]any)["friday"] != true || create["resourceName"] != nil {
		t.Errorf("create = %v", create)
	}
}

func TestConversionValueRuleUpdateSendsTheMaskOfTheGivenSettings(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+valueRulePath(clientID)] = okReply(`{"results":[{"resourceName":"` + valueRuleResourceName(clientID, "11") + `"}]}`)
	if _, err := e.invokeConfirmed(conversionValueRulesUpdateID, valueRuleCalls()[conversionValueRulesUpdateID]); err != nil {
		t.Fatal(err)
	}
	op := bodyOf(t, e.posts()[0])["operations"].([]any)[0].(map[string]any)
	update := op["update"].(map[string]any)
	// A value of 0 is a value: it must not be dropped from the request.
	if op["updateMask"] != "action,deviceCondition" || update["resourceName"] != valueRuleResourceName(clientID, "11") ||
		update["action"].(map[string]any)["value"] != float64(0) || len(update) != 3 {
		t.Errorf("operation = %v", op)
	}
}

func TestConversionValueRuleRemoveSendsOneRemovePerID(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+valueRulePath(clientID)] = okReply(`{"results":[{"resourceName":"` + valueRuleResourceName(clientID, "11") +
		`"},{"resourceName":"` + valueRuleResourceName(clientID, "12") + `"}]}`)
	out, err := e.invokeConfirmed(conversionValueRulesRemoveID, valueRuleCalls()[conversionValueRulesRemoveID])
	if err != nil {
		t.Fatal(err)
	}
	operations := bodyOf(t, e.posts()[0])["operations"].([]any)
	if len(decode(t, out)["results"].([]any)) != 2 || len(operations) != 2 ||
		operations[0].(map[string]any)["remove"] != valueRuleResourceName(clientID, "11") ||
		operations[1].(map[string]any)["remove"] != valueRuleResourceName(clientID, "12") {
		t.Errorf("out = %s, operations = %v", out, operations)
	}
}

func TestConversionValueRuleValidateOnlyAnswersWithoutResults(t *testing.T) {
	for id, arguments := range valueRuleCalls() {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+valueRulePath(clientID)] = okReply(`{}`)
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

func TestConversionValueRuleMutableResourceIsMappedAndBound(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	name := valueRuleResourceName(clientID, "777")
	e.fake.routes["POST "+valueRulePath(clientID)] = okReply(`{"results":[{"resourceName":"` + name + `","conversionValueRule":{` +
		`"resourceName":"` + name + `","id":"777","ownerCustomer":"customers/` + clientID + `","status":"ENABLED\u0007",` +
		`"action":{"operation":"ADD","value":3.5},` +
		`"geoLocationCondition":{"geoTargetConstants":["geoTargetConstants/2276"],"geoMatchType":"ANY"},` +
		`"deviceCondition":{"deviceTypes":["MOBILE"]},` +
		`"audienceCondition":{"userLists":["customers/` + clientID + `/userLists/55"]},` +
		`"itineraryCondition":{"travelLength":{"minNights":2}}}}]}`)
	out, err := e.invokeConfirmed(conversionValueRulesCreateID, strings.TrimSuffix(valueRuleCalls()[conversionValueRulesCreateID], "}")+
		`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	rule := decode(t, out)["results"].([]any)[0].(map[string]any)["conversion_value_rule"].(map[string]any)
	if rule["id"] != "777" || rule["owner_customer"] != "customers/"+clientID || rule["status"] != "ENABLED " ||
		rule["action"].(map[string]any)["value"] != 3.5 ||
		rule["geo_location_condition"].(map[string]any)["geo_target_constants"].([]any)[0] != "geoTargetConstants/2276" ||
		rule["device_condition"].(map[string]any)["device_types"].([]any)[0] != "MOBILE" ||
		rule["audience_condition"].(map[string]any)["user_lists"].([]any)[0] != "customers/"+clientID+"/userLists/55" ||
		rule["itinerary_condition"].(map[string]any)["travel_length"].(map[string]any)["min_nights"] != float64(2) {
		t.Errorf("rule = %v", rule)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// An answer that does not fit the request is never taken for the change: the request was sent, so the error
// says that the outcome is unknown.
func TestConversionValueRuleAnswersAreBoundToTheCheckedAccountAndRequest(t *testing.T) {
	good := valueRuleResourceName(clientID, "11")
	foreign := valueRuleResourceName(otherID, "11")
	withRule := func(rule string) string {
		return `{"results":[{"resourceName":"` + good + `","conversionValueRule":` + rule + `}]}`
	}
	cases := map[string]struct{ id, body string }{
		"foreign account":  {conversionValueRulesUpdateID, `{"results":[{"resourceName":"` + foreign + `"}]}`},
		"other rule":       {conversionValueRulesUpdateID, `{"results":[{"resourceName":"` + valueRuleResourceName(clientID, "9") + `"}]}`},
		"no results":       {conversionValueRulesUpdateID, `{}`},
		"too many":         {conversionValueRulesUpdateID, `{"results":[{"resourceName":"` + good + `"},{"resourceName":"` + good + `"}]}`},
		"create foreign":   {conversionValueRulesCreateID, `{"results":[{"resourceName":"` + foreign + `"}]}`},
		"create no id":     {conversionValueRulesCreateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) + `/conversionValueRules/x"}]}`},
		"remove twice":     {conversionValueRulesRemoveID, `{"results":[{"resourceName":"` + good + `"},{"resourceName":"` + good + `"}]}`},
		"partial failure":  {conversionValueRulesUpdateID, `{"results":[{"resourceName":"` + good + `"}],"partialFailureError":{"code":3}}`},
		"rule of another":  {conversionValueRulesUpdateID, withRule(`{"resourceName":"` + foreign + `"}`)},
		"id of another":    {conversionValueRulesUpdateID, withRule(`{"id":"1"}`)},
		"owner of another": {conversionValueRulesUpdateID, withRule(`{"ownerCustomer":"customers/` + otherID + `"}`)},
		"foreign list": {conversionValueRulesUpdateID, withRule(`{"audienceCondition":{"userLists":["customers/` +
			otherID + `/userLists/5"]}}`)},
		"foreign geo": {conversionValueRulesUpdateID, withRule(`{"geoLocationCondition":{"geoTargetConstants":["customers/` +
			otherID + `/geoTargetConstants/5"]}}`)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+valueRulePath(clientID)] = okReply(c.body)
			arguments := valueRuleCalls()[c.id]
			if c.id != conversionValueRulesRemoveID {
				arguments = strings.TrimSuffix(arguments, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
			}
			if c.id == conversionValueRulesRemoveID {
				arguments = `{"customer_id":"` + clientID + `","conversion_value_rule_ids":["11"]}`
			}
			_, err := e.invokeConfirmed(c.id, arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestConversionValueRuleErrorsAreMappedAndNeverRepeated(t *testing.T) {
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
		{"rule error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "conversionValueRuleError",
			"INVALID_VALUE_RULE_ACTION", "")}, provider.ClassProviderError, false},
		{"timeout", reply{err: errors.New("i/o timeout")}, provider.ClassUnreachable, true},
	}
	for _, c := range cases {
		for id, arguments := range valueRuleCalls() {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+valueRulePath(clientID)] = c.reply
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
