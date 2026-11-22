package googleads

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func userListsPath(id string) string { return "/v26/customers/" + id + "/userLists:mutate" }
func userListCustomerTypesPath(id string) string {
	return "/v26/customers/" + id + "/userListCustomerTypes:mutate"
}

var (
	userListAccount = "customers/" + queryNumber(clientID)
	userListForeign = "customers/" + queryNumber(otherID)
)

const userListRuleList = `{"name":"Shop visitors","membership_life_span":30,"rule_based_user_list":{"prepopulation_status":` +
	`"REQUESTED","flexible_rule_user_list":{"inclusive_rule_operator":"AND","inclusive_operands":[{"rule":{` +
	`"rule_item_groups":[{"rule_items":[{"name":"url__","string_rule_item":{"operator":"CONTAINS","value":"/shop"}},` +
	`{"name":"amount","number_rule_item":{"operator":"GREATER_THAN","value":12.5}}]},{"rule_items":[{"name":"seen",` +
	`"date_rule_item":{"operator":"AFTER","offset_in_days":7}}]}]},"lookback_window_days":30}],"exclusive_operands":[` +
	`{"rule":{"rule_type":"OR_OF_ANDS","rule_item_groups":[{"rule_items":[{"name":"ref_url__","string_rule_item":{` +
	`"operator":"EQUALS","value":"https://x.example.com/"}}]}]}}]}}}`

func (e *environment) userListsAnswer(path string, names ...string) {
	results := make([]string, len(names))
	for i, n := range names {
		results[i] = `{"resourceName":"` + n + `"}`
	}
	e.fake.routes["POST "+path] = okReply(`{"results":[` + strings.Join(results, ",") + `]}`)
}

// The tools that are not listed-only are offered by an effect profile, the removal of lists only by the tools
// list. Every one of them needs the confirmation of the call, and none reads a secret before.
func TestUserListToolsAreOfferedByTheirClass(t *testing.T) {
	calls := map[string]string{
		userListsCreateID:             `{"customer_id":"` + clientID + `","user_lists":[{"name":"A","crm_based_user_list":{}}]}`,
		userListsUpdateID:             `{"customer_id":"` + clientID + `","user_lists":[{"user_list_id":"5","description":"d"}]}`,
		userListsRemoveID:             `{"customer_id":"` + clientID + `","user_list_ids":["5"]}`,
		userListCustomerTypesCreateID: `{"customer_id":"` + clientID + `","customer_types":[{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`,
		userListCustomerTypesRemoveID: `{"customer_id":"` + clientID + `","customer_types":[{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`,
	}
	effects := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	for id, arguments := range calls {
		listed := id == userListsRemoveID
		t.Run(id+" by effects only", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: effects})
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if listed {
				if err == nil || errors.As(err, &needed) {
					t.Fatalf("a connection with all effects but no tools list was offered %s: %v", id, err)
				}
			} else if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
		t.Run(id+" over the read effect", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" with the tools list", func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || needed.Summary == "" {
				t.Fatalf("err = %v, want a confirmation", err)
			}
			e.untouched(t)
		})
	}
}

func TestUserListsCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.userListsAnswer(userListsPath(clientID), userListAccount+"/userLists/11", userListAccount+"/userLists/12", userListAccount+"/userLists/13",
		userListAccount+"/userLists/14", userListAccount+"/userLists/15")
	out, err := e.invokeConfirmed(userListsCreateID, `{"customer_id":"`+clientID+`","user_lists":[`+userListRuleList+`,`+
		`{"name":"Converters","description":"d","membership_status":"OPEN","integration_code":"x","eligible_for_search":true,`+
		`"basic_user_list":{"actions":[{"conversion_action":"`+userListAccount+`/conversionActions/5"},`+
		`{"remarketing_action":"`+userListAccount+`/remarketingActions/6"}]}},`+
		`{"name":"Combo","logical_user_list":{"rules":[{"operator":"ANY","rule_operands":[{"user_list":"`+userListAccount+
		`/userLists/11"},{"user_list":"`+userListAccount+`/userLists/12"}]}]}},`+
		`{"name":"Shell","crm_based_user_list":{"upload_key_type":"CONTACT_INFO","data_source_type":"FIRST_PARTY"}},`+
		`{"name":"Alike","lookalike_user_list":{"seed_user_list_ids":["11"],"expansion_level":"BALANCED","country_codes":["DE","AT"]}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	lists, _ := got["user_lists"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(lists) != 5 ||
		lists[0].(map[string]any)["user_list_id"] != "11" || lists[4].(map[string]any)["resource_name"] != userListAccount+"/userLists/15" {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || posts[0].path != userListsPath(clientID) {
		t.Fatalf("posts = %+v", posts)
	}
	body := bodyOf(t, posts[0])
	if _, found := body["partialFailure"]; found || body["validateOnly"] != nil || body["responseContentType"] != nil {
		t.Errorf("body = %s", posts[0].body)
	}
	ops := body["operations"].([]any)
	if len(ops) != 5 {
		t.Fatalf("operations = %d", len(ops))
	}
	first := ops[0].(map[string]any)["create"].(map[string]any)
	flex := first["ruleBasedUserList"].(map[string]any)
	if first["name"] != "Shop visitors" || first["membershipLifeSpan"] != float64(30) || flex["prepopulationStatus"] != "REQUESTED" {
		t.Errorf("first = %v", first)
	}
	rule := flex["flexibleRuleUserList"].(map[string]any)
	groups := rule["inclusiveOperands"].([]any)[0].(map[string]any)["rule"].(map[string]any)["ruleItemGroups"].([]any)
	items := groups[0].(map[string]any)["ruleItems"].([]any)
	if rule["inclusiveRuleOperator"] != "AND" || len(groups) != 2 ||
		items[1].(map[string]any)["numberRuleItem"].(map[string]any)["value"] != 12.5 ||
		groups[1].(map[string]any)["ruleItems"].([]any)[0].(map[string]any)["dateRuleItem"].(map[string]any)["offsetInDays"] != float64(7) ||
		rule["inclusiveOperands"].([]any)[0].(map[string]any)["lookbackWindowDays"] != float64(30) ||
		len(rule["exclusiveOperands"].([]any)) != 1 {
		t.Errorf("rule = %v", rule)
	}
	second := ops[1].(map[string]any)["create"].(map[string]any)
	actions := second["basicUserList"].(map[string]any)["actions"].([]any)
	if second["membershipStatus"] != "OPEN" || second["integrationCode"] != "x" || second["eligibleForSearch"] != true ||
		actions[0].(map[string]any)["conversionAction"] != userListAccount+"/conversionActions/5" ||
		actions[1].(map[string]any)["remarketingAction"] != userListAccount+"/remarketingActions/6" {
		t.Errorf("second = %v", second)
	}
	if ops[2].(map[string]any)["create"].(map[string]any)["logicalUserList"] == nil ||
		ops[3].(map[string]any)["create"].(map[string]any)["crmBasedUserList"].(map[string]any)["uploadKeyType"] != "CONTACT_INFO" ||
		ops[4].(map[string]any)["create"].(map[string]any)["lookalikeUserList"].(map[string]any)["expansionLevel"] != "BALANCED" {
		t.Errorf("operations = %s", posts[0].body)
	}
	if e.fake.tokenRequests() != 1 || len(e.fake.api()) != 1 {
		t.Errorf("tokens = %d, requests = %d", e.fake.tokenRequests(), len(e.fake.api()))
	}
}

func TestUserListsUpdateSendsTheMaskOfTheGivenFields(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.userListsAnswer(userListsPath(clientID), userListAccount+"/userLists/5", userListAccount+"/userLists/6")
	_, err := e.invokeConfirmed(userListsUpdateID, `{"customer_id":"`+clientID+`","user_lists":[`+
		`{"user_list_id":"5","name":"New","description":"","membership_status":"CLOSED","membership_life_span":0},`+
		`{"user_list_id":"6","basic_user_list":{"actions":[{"conversion_action":"`+userListAccount+`/conversionActions/9"}]}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, second := ops[0].(map[string]any), ops[1].(map[string]any)
	update := first["update"].(map[string]any)
	if update["resourceName"] != userListAccount+"/userLists/5" || update["name"] != "New" || update["membershipLifeSpan"] != float64(0) ||
		update["membershipStatus"] != "CLOSED" || first["updateMask"] != "name,description,membershipStatus,membershipLifeSpan" {
		t.Errorf("first = %v", first)
	}
	if _, found := update["description"]; found {
		t.Errorf("an empty description is cleared by the mask alone: %v", update)
	}
	if second["updateMask"] != "basicUserList" || second["update"].(map[string]any)["resourceName"] != userListAccount+"/userLists/6" {
		t.Errorf("second = %v", second)
	}
}

func TestUserListsRemoveAndCustomerTypes(t *testing.T) {
	t.Run("remove", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.userListsAnswer(userListsPath(clientID), userListAccount+"/userLists/5", userListAccount+"/userLists/6")
		out, err := e.invokeConfirmed(userListsRemoveID, `{"customer_id":"`+clientID+`","user_list_ids":["5","6"]}`)
		if err != nil {
			t.Fatal(err)
		}
		ops := bodyOf(t, e.posts()[0])["operations"].([]any)
		if len(ops) != 2 || ops[0].(map[string]any)["remove"] != userListAccount+"/userLists/5" ||
			ops[1].(map[string]any)["remove"] != userListAccount+"/userLists/6" || !strings.Contains(out, `"user_list_id":"6"`) {
			t.Errorf("ops = %v, out = %s", ops, out)
		}
	})
	pair := userListAccount + "/userListCustomerTypes/5~PURCHASERS"
	t.Run("customer types create", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.userListsAnswer(userListCustomerTypesPath(clientID), pair)
		out, err := e.invokeConfirmed(userListCustomerTypesCreateID, `{"customer_id":"`+clientID+
			`","customer_types":[{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		op := bodyOf(t, e.posts()[0])["operations"].([]any)[0].(map[string]any)["create"].(map[string]any)
		if op["userList"] != userListAccount+"/userLists/5" || op["customerTypeCategory"] != "PURCHASERS" ||
			!strings.Contains(out, `"resource_name":"`+pair+`"`) || !strings.Contains(out, `"customer_type_category":"PURCHASERS"`) {
			t.Errorf("op = %v, out = %s", op, out)
		}
	})
	t.Run("customer types remove", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.userListsAnswer(userListCustomerTypesPath(clientID), pair)
		if _, err := e.invokeConfirmed(userListCustomerTypesRemoveID, `{"customer_id":"`+clientID+
			`","customer_types":[{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`); err != nil {
			t.Fatal(err)
		}
		if op := bodyOf(t, e.posts()[0])["operations"].([]any)[0].(map[string]any); op["remove"] != pair {
			t.Errorf("op = %v", op)
		}
	})
}

func TestUserListOperationsHaveAFixedBound(t *testing.T) {
	list := func(n int, one string) string { return strings.TrimSuffix(strings.Repeat(one+",", n), ",") }
	e := newMutationEnvironment(t, nil, nil)
	names := make([]string, 20)
	for i := range names {
		names[i] = fmt.Sprintf("%s/userLists/%d", userListAccount, i+1)
	}
	e.userListsAnswer(userListsPath(clientID), names...)
	ok := `{"name":"A","crm_based_user_list":{}}`
	if _, err := e.invokeConfirmed(userListsCreateID, `{"customer_id":"`+clientID+`","user_lists":[`+list(20, ok)+`]}`); err != nil {
		t.Fatalf("20 entries: %v", err)
	}
	for id, arguments := range map[string]string{
		userListsCreateID: `{"customer_id":"` + clientID + `","user_lists":[` + list(21, ok) + `]}`,
		userListsRemoveID: `{"customer_id":"` + clientID + `","user_list_ids":[` + func() string {
			var ids []string
			for i := 1; i <= 21; i++ {
				ids = append(ids, fmt.Sprintf(`"%d"`, i))
			}
			return strings.Join(ids, ",")
		}() + `]}`,
		userListCustomerTypesCreateID: `{"customer_id":"` + clientID + `","customer_types":[` + func() string {
			var pairs []string
			for i := 1; i <= 51; i++ {
				pairs = append(pairs, fmt.Sprintf(`{"user_list_id":"%d","customer_type_category":"PURCHASERS"}`, i))
			}
			return strings.Join(pairs, ",")
		}() + `]}`,
		userListsUpdateID:             `{"customer_id":"` + clientID + `","user_lists":[]}`,
		userListCustomerTypesRemoveID: `{"customer_id":"` + clientID + `","customer_types":[]}`,
	} {
		e := newMutationEnvironment(t, nil, nil)
		if _, err := e.invokeConfirmed(id, arguments); err == nil {
			t.Errorf("%s accepted a list beyond its bound", id)
		}
		e.untouched(t)
	}
}

func TestUserListArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	c := `"customer_id":"` + clientID + `",`
	type call struct{ id, arguments string }
	create := func(entry string) call { return call{userListsCreateID, `{` + c + `"user_lists":[` + entry + `]}`} }
	update := func(entry string) call { return call{userListsUpdateID, `{` + c + `"user_lists":[` + entry + `]}`} }
	basic := func(action string) string { return `{"name":"A","basic_user_list":{"actions":[` + action + `]}}` }
	rule := func(item string) string {
		return `{"name":"A","rule_based_user_list":{"flexible_rule_user_list":{"inclusive_rule_operator":"OR",` +
			`"inclusive_operands":[{"rule":{"rule_item_groups":[{"rule_items":[` + item + `]}]}}]}}}`
	}
	cases := map[string]call{}
	add := func(name string, c call) { cases[name] = c }
	add2 := func(name, id, arguments string) { cases[name] = call{id, arguments} }
	add("no type", create(`{"name":"A"}`))
	add("two types", create(`{"name":"A","crm_based_user_list":{},"logical_user_list":{"rules":[{"operator":"ALL",`+
		`"rule_operands":[{"user_list":"`+userListAccount+`/userLists/1"}]}]}}`))
	add("no name", create(`{"crm_based_user_list":{}}`))
	add("blank name", create(`{"name":"  ","crm_based_user_list":{}}`))
	add("control character in name", create(`{"name":"a\u0007b","crm_based_user_list":{}}`))
	add("life span too long", create(`{"name":"A","membership_life_span":541,"crm_based_user_list":{}}`))
	add("life span negative", create(`{"name":"A","membership_life_span":-1,"crm_based_user_list":{}}`))
	add("status", create(`{"name":"A","membership_status":"ACTIVE","crm_based_user_list":{}}`))
	add("user list ID on create", create(`{"name":"A","user_list_id":"5","crm_based_user_list":{}}`))
	add("foreign conversion action", create(basic(`{"conversion_action":"`+userListForeign+`/conversionActions/5"}`)))
	add("foreign remarketing action", create(basic(`{"remarketing_action":"`+userListForeign+`/remarketingActions/5"}`)))
	add("action form", create(basic(`{"conversion_action":"conversionActions/5"}`)))
	add("two actions in one", create(basic(`{"conversion_action":"`+userListAccount+`/conversionActions/5","remarketing_action":"`+userListAccount+`/remarketingActions/5"}`)))
	add("empty action", create(basic(`{}`)))
	add("foreign operand", create(`{"name":"A","logical_user_list":{"rules":[{"operator":"ALL","rule_operands":[{"user_list":"`+userListForeign+`/userLists/1"}]}]}}`))
	add("operand form", create(`{"name":"A","logical_user_list":{"rules":[{"operator":"ALL","rule_operands":[{"user_list":"userLists/1"}]}]}}`))
	add("logical operator", create(`{"name":"A","logical_user_list":{"rules":[{"operator":"AND","rule_operands":[{"user_list":"`+userListAccount+`/userLists/1"}]}]}}`))
	add("rule item without kind", create(rule(`{"name":"x"}`)))
	add("rule item with two kinds", create(rule(`{"name":"x","number_rule_item":{"operator":"EQUALS","value":1},"string_rule_item":{"operator":"EQUALS","value":"a"}}`)))
	add("rule item name", create(rule(`{"name":"1x","number_rule_item":{"operator":"EQUALS","value":1}}`)))
	add("number without value", create(rule(`{"name":"x","number_rule_item":{"operator":"EQUALS"}}`)))
	add("string operator", create(rule(`{"name":"x","string_rule_item":{"operator":"LIKE","value":"a"}}`)))
	add("url with parenthesis", create(rule(`{"name":"url__","string_rule_item":{"operator":"CONTAINS","value":"a(b)"}}`)))
	add("referrer with quote", create(rule(`{"name":"ref_url__","string_rule_item":{"operator":"CONTAINS","value":"a\"b"}}`)))
	add("date with both", create(rule(`{"name":"x","date_rule_item":{"operator":"AFTER","value":"2026-01-01","offset_in_days":1}}`)))
	add("date form", create(rule(`{"name":"x","date_rule_item":{"operator":"AFTER","value":"01.01.2026"}}`)))
	add("and of ors in a flexible rule", create(`{"name":"A","rule_based_user_list":{"flexible_rule_user_list":{"inclusive_rule_operator":"OR",`+
		`"inclusive_operands":[{"rule":{"rule_type":"AND_OF_ORS","rule_item_groups":[{"rule_items":[{"name":"x","number_rule_item":{"operator":"EQUALS","value":1}}]}]}}]}}}`))
	add("prepopulation finished", create(`{"name":"A","rule_based_user_list":{"prepopulation_status":"FINISHED","flexible_rule_user_list":{"inclusive_rule_operator":"OR",`+
		`"inclusive_operands":[{"rule":{"rule_item_groups":[{"rule_items":[{"name":"x","number_rule_item":{"operator":"EQUALS","value":1}}]}]}}]}}}`))
	add("upload key type", create(`{"name":"A","crm_based_user_list":{"upload_key_type":"EMAIL"}}`))
	add("lookalike without seeds", create(`{"name":"A","lookalike_user_list":{"seed_user_list_ids":[],"expansion_level":"BROAD","country_codes":["DE"]}}`))
	add("lookalike country", create(`{"name":"A","lookalike_user_list":{"seed_user_list_ids":["1"],"expansion_level":"BROAD","country_codes":["de"]}}`))
	add("lookalike level", create(`{"name":"A","lookalike_user_list":{"seed_user_list_ids":["1"],"expansion_level":"HUGE","country_codes":["DE"]}}`))
	add("update without ID", update(`{"name":"A"}`))
	add("update ID form", update(`{"user_list_id":"customers/1/userLists/5","name":"A"}`))
	add("update without a field", update(`{"user_list_id":"5"}`))
	add("update the same list twice", update(`{"user_list_id":"5","name":"A"},{"user_list_id":"5","name":"B"}`))
	add("update lookalike", update(`{"user_list_id":"5","lookalike_user_list":{"seed_user_list_ids":["1"],"expansion_level":"BROAD","country_codes":["DE"]}}`))
	add("update with two types", update(`{"user_list_id":"5","crm_based_user_list":{},"logical_user_list":{"rules":[{"operator":"ALL",`+
		`"rule_operands":[{"user_list":"`+userListAccount+`/userLists/1"}]}]}}`))
	add("update empty name", update(`{"user_list_id":"5","name":""}`))
	add2("remove ID form", userListsRemoveID, `{`+c+`"user_list_ids":["customers/1/userLists/5"]}`)
	add2("remove ID zero", userListsRemoveID, `{`+c+`"user_list_ids":["0"]}`)
	add2("remove twice", userListsRemoveID, `{`+c+`"user_list_ids":["5","5"]}`)
	add2("remove with a resource name argument", userListsRemoveID, `{`+c+`"user_list_ids":["5"],"resource_name":"`+userListForeign+`/userLists/5"}`)
	add2("category", userListCustomerTypesCreateID, `{`+c+`"customer_types":[{"user_list_id":"5","customer_type_category":"UNSPECIFIED"}]}`)
	add2("customer type pair twice", userListCustomerTypesCreateID, `{`+c+`"customer_types":[`+
		`{"user_list_id":"5","customer_type_category":"PURCHASERS"},{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`)
	add2("customer type list ID", userListCustomerTypesRemoveID, `{`+c+`"customer_types":[{"user_list_id":"x","customer_type_category":"PURCHASERS"}]}`)
	add2("customer type foreign list", userListCustomerTypesCreateID, `{`+c+`"customer_types":[{"user_list_id":"`+userListForeign+`/userLists/5","customer_type_category":"PURCHASERS"}]}`)
	add2("customer id", userListsRemoveID, `{"customer_id":"123","user_list_ids":["5"]}`)
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(call.id, call.arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestUserListsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"response_content_type":"MUTABLE_RESOURCE"`, `"path":"/x"`} {
		for id, arguments := range map[string]string{
			userListsCreateID: `{"customer_id":"` + clientID + `","user_lists":[{"name":"A","crm_based_user_list":{}}],`,
			userListsRemoveID: `{"customer_id":"` + clientID + `","user_list_ids":["5"],`,
			userListCustomerTypesCreateID: `{"customer_id":"` + clientID + `","customer_types":[` +
				`{"user_list_id":"5","customer_type_category":"PURCHASERS"}],`} {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments+extra+`}`); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestUserListTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for id, arguments := range map[string]string{
		userListsCreateID: `{"customer_id":"` + otherID + `","user_lists":[{"name":"A","crm_based_user_list":{}}]}`,
		userListsUpdateID: `{"customer_id":"` + otherID + `","user_lists":[{"user_list_id":"5","name":"A"}]}`,
		userListsRemoveID: `{"customer_id":"` + otherID + `","user_list_ids":["5"]}`,
		userListCustomerTypesCreateID: `{"customer_id":"` + otherID + `","customer_types":[` +
			`{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`,
		userListCustomerTypesRemoveID: `{"customer_id":"` + otherID + `","customer_types":[` +
			`{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`,
	} {
		t.Run(id, func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestUserListsOnAManagerNeedAManagerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.routes["POST "+searchPath(managerID)] = rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
	_, err := e.invokeConfirmed(userListsRemoveID, `{"customer_id":"`+managerID+`","user_list_ids":["5"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}

func TestUserListsValidateOnly(t *testing.T) {
	for _, answer := range []string{`{}`, `{"results":[]}`, `{"results":[{}]}`} {
		t.Run(answer, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+userListsPath(clientID)] = okReply(answer)
			out, err := e.invokeConfirmed(userListsCreateID, `{"customer_id":"`+clientID+
				`","user_lists":[{"name":"A","crm_based_user_list":{}}],"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if lists, _ := got["user_lists"].([]any); got["validate_only"] != true || len(lists) != 0 {
				t.Errorf("out = %s", out)
			}
			if posts := e.posts(); len(posts) != 1 || bodyOf(t, posts[0])["validateOnly"] != true {
				t.Errorf("posts = %+v", posts)
			}
		})
	}
	t.Run("customer types", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+userListCustomerTypesPath(clientID)] = okReply(`{}`)
		out, err := e.invokeConfirmed(userListCustomerTypesRemoveID, `{"customer_id":"`+clientID+
			`","customer_types":[{"user_list_id":"5","customer_type_category":"PURCHASERS"}],"validate_only":true}`)
		if err != nil || decode(t, out)["validate_only"] != true || bodyOf(t, e.posts()[0])["validateOnly"] != true {
			t.Fatalf("out = %s, err = %v", out, err)
		}
	})
	t.Run("a validation error is the refusal of Google", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+userListsPath(clientID)] = reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT",
			"userListError", "INVALID_NAME", "")}
		_, err := e.invokeConfirmed(userListsCreateID, `{"customer_id":"`+clientID+
			`","user_lists":[{"name":"A","crm_based_user_list":{}}],"validate_only":true}`)
		if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "userListError INVALID_NAME") ||
			!strings.Contains(err.Error(), "nothing was changed") || strings.Contains(err.Error(), detailCanary) {
			t.Fatalf("err = %v", err)
		}
	})
}

// An answer that does not match what was sent never passes: the request was sent, so the outcome is unknown.
func TestUserListAnswersAreBoundToTheAccountAndTheOperations(t *testing.T) {
	two := `{"customer_id":"` + clientID + `","user_list_ids":["5","6"]}`
	cases := map[string]struct{ id, arguments, body string }{
		"another account": {userListsRemoveID, two, `{"results":[{"resourceName":"` + userListForeign + `/userLists/5"},` +
			`{"resourceName":"` + userListAccount + `/userLists/6"}]}`},
		"another list": {userListsRemoveID, two, `{"results":[{"resourceName":"` + userListAccount + `/userLists/5"},` +
			`{"resourceName":"` + userListAccount + `/userLists/7"}]}`},
		"another order": {userListsRemoveID, two, `{"results":[{"resourceName":"` + userListAccount + `/userLists/6"},` +
			`{"resourceName":"` + userListAccount + `/userLists/5"}]}`},
		"fewer results":            {userListsRemoveID, two, `{"results":[{"resourceName":"` + userListAccount + `/userLists/5"}]}`},
		"more results":             {userListsRemoveID, `{"customer_id":"` + clientID + `","user_list_ids":["5"]}`, `{"results":[{"resourceName":"` + userListAccount + `/userLists/5"},{"resourceName":"` + userListAccount + `/userLists/6"}]}`},
		"no result":                {userListsRemoveID, two, `{}`},
		"a partial failure":        {userListsRemoveID, two, `{"results":[{"resourceName":"` + userListAccount + `/userLists/5"},{"resourceName":"` + userListAccount + `/userLists/6"}],"partialFailureError":{"code":3}}`},
		"created in another":       {userListsCreateID, `{"customer_id":"` + clientID + `","user_lists":[{"name":"A","crm_based_user_list":{}}]}`, `{"results":[{"resourceName":"` + userListForeign + `/userLists/5"}]}`},
		"created with a name":      {userListsCreateID, `{"customer_id":"` + clientID + `","user_lists":[{"name":"A","crm_based_user_list":{}}]}`, `{"results":[{"resourceName":"` + userListAccount + `/userLists/x"}]}`},
		"created twice":            {userListsCreateID, `{"customer_id":"` + clientID + `","user_lists":[{"name":"A","crm_based_user_list":{}},{"name":"B","crm_based_user_list":{}}]}`, `{"results":[{"resourceName":"` + userListAccount + `/userLists/5"},{"resourceName":"` + userListAccount + `/userLists/5"}]}`},
		"type of another list":     {userListCustomerTypesCreateID, `{"customer_id":"` + clientID + `","customer_types":[{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`, `{"results":[{"resourceName":"` + userListAccount + `/userListCustomerTypes/6~PURCHASERS"}]}`},
		"type of another category": {userListCustomerTypesRemoveID, `{"customer_id":"` + clientID + `","customer_types":[{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`, `{"results":[{"resourceName":"` + userListAccount + `/userListCustomerTypes/5~ALL_CUSTOMERS"}]}`},
		"type of another account":  {userListCustomerTypesRemoveID, `{"customer_id":"` + clientID + `","customer_types":[{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`, `{"results":[{"resourceName":"` + userListForeign + `/userListCustomerTypes/5~PURCHASERS"}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			path := userListsPath(clientID)
			if strings.Contains(c.id, "customer-types") {
				path = userListCustomerTypesPath(clientID)
			}
			e.fake.routes["POST "+path] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestUserListChangeErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		unknown bool
	}{
		{"user list error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "userListError", "INVALID_NAME", "")},
			provider.ClassProviderError, false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, true},
		{"timeout", reply{err: errors.New("connection reset by peer")}, provider.ClassUnreachable, true},
		{"unreadable json", okReply(`{"results":`), provider.ClassInvalidResponse, true},
	}
	for id, arguments := range map[string]string{
		userListsRemoveID: `{"customer_id":"` + clientID + `","user_list_ids":["5"]}`,
		userListCustomerTypesCreateID: `{"customer_id":"` + clientID + `","customer_types":[` +
			`{"user_list_id":"5","customer_type_category":"PURCHASERS"}]}`,
	} {
		path := userListsPath(clientID)
		if strings.Contains(id, "customer-types") {
			path = userListCustomerTypesPath(clientID)
		}
		for _, c := range cases {
			t.Run(id+" "+c.name, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+path] = c.reply
				_, err := e.invokeConfirmed(id, arguments)
				if classOf(err) != c.class {
					t.Fatalf("err = %v, want %s", err, c.class)
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
}

func TestUserListAnswerTextIsNotTrustedForTheOutput(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+userListsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + userListAccount + `/userLists/5",` +
		`"note":"` + detailCanary + `"}]}`)
	out, err := e.invokeConfirmed(userListsRemoveID, `{"customer_id":"`+clientID+`","user_list_ids":["5"]}`)
	if err != nil || strings.Contains(out, detailCanary) {
		t.Fatalf("out = %s, err = %v", out, err)
	}
}

func TestUserListSummariesNameEveryObject(t *testing.T) {
	create := userListsSummary(true)(json.RawMessage(`{"customer_id":"` + clientID + `","user_lists":[` + userListRuleList + `,` +
		`{"name":"Shell","crm_based_user_list":{}},{"name":"` + strings.Repeat("x", 300) + `","logical_user_list":{"rules":[]}}],"validate_only":true}`))
	for _, word := range []string{clientID, `"Shop visitors", rule based`, `"Shell", Customer Match (empty)`, "3 user list(s)",
		"Only validate", "logical"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	if len(create) > 1200 {
		t.Errorf("summary is %d bytes: names are not cut", len(create))
	}
	update := userListsSummary(false)(json.RawMessage(`{"customer_id":"` + clientID + `","user_lists":[` +
		`{"user_list_id":"5","name":"N","membership_status":"CLOSED"},{"user_list_id":"6","description":"x"}]}`))
	for _, word := range []string{clientID, "5 (new name \"N\"), CLOSED", "6", "2 user list(s)"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := userListsRemoveSummary(json.RawMessage(`{"customer_id":"` + clientID + `","user_list_ids":["5","6"]}`))
	for _, word := range []string{clientID, "5, 6", "not reactivated", "no longer be targeted"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	types := userListCustomerTypesSummary(false)(json.RawMessage(`{"customer_id":"` + clientID + `","customer_types":[` +
		`{"user_list_id":"5","customer_type_category":"PURCHASERS"},{"user_list_id":"6","customer_type_category":"QUALIFIED_LEADS"}]}`))
	for _, word := range []string{clientID, "Detach 2", "list 5 as PURCHASERS", "list 6 as QUALIFIED_LEADS"} {
		if !strings.Contains(types, word) {
			t.Errorf("summary %q lacks %q", types, word)
		}
	}
}

func TestNoUserListToolOffersMemberUploadOrOutput(t *testing.T) {
	for _, d := range []string{userListsCreateID, userListsUpdateID, userListCustomerTypesCreateID} {
		e := newMutationEnvironment(t, nil, nil)
		desc, _, _ := e.reg.Lookup(d)
		text := strings.ToLower(string(desc.InputSchema))
		for _, word := range []string{"user_identifiers", "hashed", "email_address", "phone", "user_data", `"members"`, `"member_`} {
			if strings.Contains(text, word) {
				t.Errorf("%s: input schema names %q", d, word)
			}
		}
	}
}
