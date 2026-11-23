package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

var campaignBudgetsToolIDs = []string{campaignBudgetsCreateID, campaignBudgetsUpdateID, campaignBudgetsRemoveID}

func campaignBudgetsPath(id string) string { return "/v26/customers/" + id + "/campaignBudgets:mutate" }

// campaignBudgetsResource is the resource name of a budget of the account, as Google answers it.
func campaignBudgetsResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/campaignBudgets/" + id
}

// newCampaignBudgetsEnvironment is a connection whose tools list names the three budget tools.
func newCampaignBudgetsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: campaignBudgetsToolIDs})
}

var campaignBudgetsCalls = map[string]string{
	campaignBudgetsCreateID: `{"customer_id":"` + clientID + `","campaign_budgets":[{"name":"Spring sale",` +
		`"amount_micros":5000000,"delivery_method":"ACCELERATED","aligned_bidding_strategy_id":"77",` + networkArgs + `},` +
		`{"name":"Season","period":"CUSTOM_PERIOD","total_amount_micros":900000000,"type":"STANDARD"},` +
		`{"amount_micros":1,"explicitly_shared":false}]}`,
	campaignBudgetsUpdateID: `{"customer_id":"` + clientID + `","campaign_budgets":[{"budget_id":"111","amount_micros":7500000,` +
		`"delivery_method":"STANDARD"},{"budget_id":"222","name":"Shared now","explicitly_shared":true}]}`,
	campaignBudgetsRemoveID: `{"customer_id":"` + clientID + `","budget_ids":["111","222"]}`,
}

var campaignBudgetsAnswers = map[string]string{
	campaignBudgetsCreateID: `{"results":[{"resourceName":"` + campaignBudgetsResource(clientID, "111") + `"},{"resourceName":"` +
		campaignBudgetsResource(clientID, "222") + `"},{"resourceName":"` + campaignBudgetsResource(clientID, "333") + `"}]}`,
	campaignBudgetsUpdateID: `{"results":[{"resourceName":"` + campaignBudgetsResource(clientID, "111") + `"},{"resourceName":"` +
		campaignBudgetsResource(clientID, "222") + `"}]}`,
	campaignBudgetsRemoveID: `{"results":[{"resourceName":"` + campaignBudgetsResource(clientID, "111") + `"},{"resourceName":"` +
		campaignBudgetsResource(clientID, "222") + `"}]}`,
}

func TestCampaignBudgetsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{campaignBudgetsCreateID: capability.EffectCreate,
		campaignBudgetsUpdateID: capability.EffectUpdate, campaignBudgetsRemoveID: capability.EffectDelete}
	for _, id := range campaignBudgetsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld || !d.RequiresToolAllowList {
			t.Fatalf("%s = %+v", id, d)
		}
		if !strings.HasSuffix(d.Description, mutateListListedNote) {
			t.Errorf("%s: description does not end on the note that only the tools list offers it", id)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) || slices.Contains(readTools, id) {
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
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "url", "status",
			"resource_name", "update_mask"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if _, found := input.Properties["response_content_type"]; found == (id == campaignBudgetsRemoveID) {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		if !strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "never repeats") ||
			!strings.Contains(d.Description, "atomic") {
			t.Errorf("%s: description lacks the unknown outcome: %s", id, d.Description)
		}
		for _, tool := range metadata.Tools {
			if tool.ID == id && (string(tool.Effect) != string(wantEffect[id]) || !tool.RequiresToolAllowList) {
				t.Errorf("tool %+v", tool)
			}
		}
	}
	create, _, _ := reg.Lookup(campaignBudgetsCreateID)
	for _, text := range []string{`"maxLength":255`, `"maxItems":50`, `"minimum":1`, `"CUSTOM_PERIOD"`, `"ACCELERATED"`,
		`"FIXED_CPA"`, `"LOCAL_SERVICES"`} {
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
}

// All three tools need the tools list, whatever effects the connection holds.
func TestCampaignBudgetsToolsAreReachedByTheToolsListOnly(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	for _, id := range campaignBudgetsToolIDs {
		for name, conn := range map[string]config.Connection{"all effects": {Effects: all},
			"listed": {Tools: []string{id}}, "other listed": {Tools: []string{labelsRemoveID}}} {
			t.Run(name+" "+id, func(t *testing.T) {
				e := newEnvironmentWith(t, nil, nil, conn)
				e.fake.routes["POST "+campaignBudgetsPath(clientID)] = okReply(campaignBudgetsAnswers[id])
				_, err := e.invokeConfirmed(id, campaignBudgetsCalls[id])
				if (err == nil) != (name == "listed") {
					t.Errorf("err = %v", err)
				}
				if err != nil {
					e.untouched(t)
				}
			})
		}
	}
}

func TestCampaignBudgetsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range campaignBudgetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignBudgetsEnvironment(t, nil, nil)
			_, err := e.invoke(id, campaignBudgetsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignBudgetsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"name"`} {
		for _, id := range campaignBudgetsToolIDs {
			e := newCampaignBudgetsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(campaignBudgetsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestCampaignBudgetsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range campaignBudgetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignBudgetsEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(campaignBudgetsCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

// amount_micros and total_amount_micros are checked in form, sign, and size before any request.
func TestCampaignBudgetsAmountsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `","campaign_budgets":[`
	for name, field := range map[string]string{"amount_micros": `{"name":"a","amount_micros":%s}`,
		"total_amount_micros": `{"name":"a","period":"CUSTOM_PERIOD","total_amount_micros":%s}`,
		"update amount":       `{"budget_id":"1","amount_micros":%s}`, "update total": `{"budget_id":"1","total_amount_micros":%s}`} {
		for _, bad := range []string{"0", "-5", "1.5", "5000000.0", "1e6", `"5000000"`, "null1", "true", "9223372036854775808",
			"99999999999999999999", "[]"} {
			t.Run(name+" "+bad, func(t *testing.T) {
				id := campaignBudgetsCreateID
				if strings.HasPrefix(name, "update") {
					id = campaignBudgetsUpdateID
				}
				e := newCampaignBudgetsEnvironment(t, nil, nil)
				entry := strings.Replace(field, "%s", bad, 1)
				if _, err := e.invokeConfirmed(id, head+entry+`]}`); err == nil {
					t.Fatal("accepted")
				}
				e.untouched(t)
			})
		}
	}
	t.Run("the largest amount and the smallest pass", func(t *testing.T) {
		for _, amount := range []string{"1", "9223372036854775807"} {
			e := newCampaignBudgetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignBudgetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
				campaignBudgetsResource(clientID, "5") + `"}]}`)
			if _, err := e.invokeConfirmed(campaignBudgetsCreateID, head+`{"name":"a","amount_micros":`+amount+`}]}`); err != nil {
				t.Fatal(err)
			}
			operation := bodyOf(t, e.posts()[0])["operations"].([]any)[0].(map[string]any)["create"].(map[string]any)
			if operation["amountMicros"] != amount {
				t.Errorf("amountMicros = %v, want the string %s", operation["amountMicros"], amount)
			}
		}
	})
}

func TestCampaignBudgetsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	bad := map[string]struct{ id, body string }{
		"create no budgets":          {campaignBudgetsCreateID, head + `"campaign_budgets":[]}`},
		"create shared without name": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"amount_micros":1}]}`},
		"create explicit shared":     {campaignBudgetsCreateID, head + `"campaign_budgets":[{"explicitly_shared":true,"amount_micros":1}]}`},
		"create blank name":          {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"  ","amount_micros":1}]}`},
		"create empty name":          {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"","amount_micros":1}]}`},
		"create name 256 bytes": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"` + strings.Repeat("a", 256) +
			`","amount_micros":1}]}`},
		"create name 128 umlauts": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"` + strings.Repeat("ä", 128) +
			`","amount_micros":1}]}`},
		"create control in name": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a\u0007b","amount_micros":1}]}`},
		"create no amount":       {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a"}]}`},
		"create daily with total": {campaignBudgetsCreateID, head +
			`"campaign_budgets":[{"name":"a","amount_micros":1,"total_amount_micros":1}]}`},
		"create daily total only": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a","total_amount_micros":1}]}`},
		"create custom with daily": {campaignBudgetsCreateID, head +
			`"campaign_budgets":[{"name":"a","period":"CUSTOM_PERIOD","amount_micros":1}]}`},
		"create custom no total": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a","period":"CUSTOM_PERIOD"}]}`},
		"create period":          {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a","period":"WEEKLY","amount_micros":1}]}`},
		"create unspecified period": {campaignBudgetsCreateID, head +
			`"campaign_budgets":[{"name":"a","period":"UNSPECIFIED","amount_micros":1}]}`},
		"create delivery": {campaignBudgetsCreateID, head +
			`"campaign_budgets":[{"name":"a","amount_micros":1,"delivery_method":"FAST"}]}`},
		"create type": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a","amount_micros":1,"type":"HOTEL"}]}`},
		"create strategy path": {campaignBudgetsCreateID, head +
			`"campaign_budgets":[{"name":"a","amount_micros":1,"aligned_bidding_strategy_id":"customers/1/biddingStrategies/2"}]}`},
		"create network empty": {campaignBudgetsCreateID, head +
			`"campaign_budgets":[{"name":"a","amount_micros":1,"budget_network_settings":[]}]}`},
		"create network type twice": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a","amount_micros":1,` +
			`"budget_network_settings":[{"advertising_channel_type":"SEARCH"},{"advertising_channel_type":"SEARCH"}]}]}`},
		"create network cap": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a","amount_micros":1,` +
			`"budget_network_settings":[{"advertising_channel_type":"SEARCH","budget_caps":[{"network_channel":` +
			`"GOOGLE_DISPLAY","cap_percent_millis":100001}]}]}]}`},
		"create too many": {campaignBudgetsCreateID, head + `"campaign_budgets":[` +
			strings.Repeat(`{"name":"a","amount_micros":1},`, 50) + `{"name":"a","amount_micros":1}]}`},
		"create status":           {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a","amount_micros":1,"status":"REMOVED"}]}`},
		"create reference count":  {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a","amount_micros":1,"reference_count":1}]}`},
		"create bad content type": {campaignBudgetsCreateID, head + `"campaign_budgets":[{"name":"a","amount_micros":1}],"response_content_type":"X"}`},
		"update no id":            {campaignBudgetsUpdateID, head + `"campaign_budgets":[{"amount_micros":1}]}`},
		"update zero id":          {campaignBudgetsUpdateID, head + `"campaign_budgets":[{"budget_id":"0","amount_micros":1}]}`},
		"update path as id": {campaignBudgetsUpdateID, head +
			`"campaign_budgets":[{"budget_id":"customers/1/campaignBudgets/2","amount_micros":1}]}`},
		"update id overflow": {campaignBudgetsUpdateID, head +
			`"campaign_budgets":[{"budget_id":"99999999999999999999","amount_micros":1}]}`},
		"update nothing":    {campaignBudgetsUpdateID, head + `"campaign_budgets":[{"budget_id":"1"}]}`},
		"update same twice": {campaignBudgetsUpdateID, head + `"campaign_budgets":[{"budget_id":"1","amount_micros":1},{"budget_id":"1","amount_micros":2}]}`},
		"update both amounts": {campaignBudgetsUpdateID, head +
			`"campaign_budgets":[{"budget_id":"1","amount_micros":1,"total_amount_micros":1}]}`},
		"update empty name": {campaignBudgetsUpdateID, head + `"campaign_budgets":[{"budget_id":"1","name":""}]}`},
		"update unshare":    {campaignBudgetsUpdateID, head + `"campaign_budgets":[{"budget_id":"1","name":"a","explicitly_shared":false}]}`},
		"update share without name": {campaignBudgetsUpdateID, head +
			`"campaign_budgets":[{"budget_id":"1","explicitly_shared":true}]}`},
		"update period": {campaignBudgetsUpdateID, head + `"campaign_budgets":[{"budget_id":"1","period":"DAILY"}]}`},
		"update type":   {campaignBudgetsUpdateID, head + `"campaign_budgets":[{"budget_id":"1","type":"STANDARD"}]}`},
		"update delivery": {campaignBudgetsUpdateID, head +
			`"campaign_budgets":[{"budget_id":"1","delivery_method":"UNSPECIFIED"}]}`},
		"remove no ids": {campaignBudgetsRemoveID, head + `"budget_ids":[]}`},
		"remove name":   {campaignBudgetsRemoveID, head + `"budget_ids":["customers/1/campaignBudgets/2"]}`},
		"remove twice":  {campaignBudgetsRemoveID, head + `"budget_ids":["1","1"]}`},
		"remove too many": {campaignBudgetsRemoveID, head + `"budget_ids":[` + func() string {
			var ids []string
			for i := 1; i <= 51; i++ {
				ids = append(ids, `"`+strconv.Itoa(i)+`"`)
			}
			return strings.Join(ids, ",")
		}() + `]}`},
		"remove content type": {campaignBudgetsRemoveID, head + `"budget_ids":["1"],"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":        {campaignBudgetsRemoveID, `{"customer_id":"12-34","budget_ids":["1"]}`},
		"validate only alone": {campaignBudgetsCreateID, head + `"validate_only":true}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newCampaignBudgetsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c.id, c.body); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newCampaignBudgetsEnvironment(t, nil, nil)
		e.fake.routes["POST "+campaignBudgetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
			campaignBudgetsResource(clientID, "5") + `"}]}`)
		// 127 umlauts are 254 bytes and the name is padded to 255 bytes with a letter.
		_, err := e.invokeConfirmed(campaignBudgetsCreateID, head+`"campaign_budgets":[{"name":"`+strings.Repeat("ä", 127)+
			`a","amount_micros":1}]}`)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestCampaignBudgetsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	network := `{"settings":[{"advertisingChannelType":"SEARCH","budgetCaps":[{"networkChannel":"SEARCH_PARTNER",` +
		`"capPercentMillis":20000}],"channelPriorities":[{"priorityTier":"TIER1","networkChannels":["OWNED_AND_OPERATED"]},` +
		`{"priorityTier":"TIER2","networkChannels":["SEARCH_PARTNER"]}]}]}`
	want := map[string]string{
		campaignBudgetsCreateID: `[{"create":{"name":"Spring sale","amountMicros":"5000000","deliveryMethod":"ACCELERATED",` +
			`"alignedBiddingStrategyId":"77","budgetNetworkSettings":` + network + `}},` +
			`{"create":{"name":"Season","period":"CUSTOM_PERIOD","totalAmountMicros":"900000000","type":"STANDARD"}},` +
			`{"create":{"amountMicros":"1","explicitlyShared":false}}]`,
		campaignBudgetsUpdateID: `[{"update":{"resourceName":"customers/1000000003/campaignBudgets/111","amountMicros":"7500000",` +
			`"deliveryMethod":"STANDARD"},"updateMask":"amountMicros,deliveryMethod"},` +
			`{"update":{"resourceName":"customers/1000000003/campaignBudgets/222","name":"Shared now","explicitlyShared":true},` +
			`"updateMask":"name,explicitlyShared"}]`,
		campaignBudgetsRemoveID: `[{"remove":"customers/1000000003/campaignBudgets/111"},` +
			`{"remove":"customers/1000000003/campaignBudgets/222"}]`,
	}
	for _, id := range campaignBudgetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignBudgetsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+campaignBudgetsPath(clientID)] = okReply(campaignBudgetsAnswers[id])
			out, err := e.invokeConfirmed(id, campaignBudgetsCalls[id])
			if err != nil {
				t.Fatal(err)
			}
			posts := e.posts()
			if len(posts) != 1 || posts[0].login != loginID || posts[0].developerToken != "" {
				t.Fatalf("posts = %+v", posts)
			}
			body := bodyOf(t, posts[0])
			if len(body) != 1 || !reflect.DeepEqual(body["operations"], wantWire(t, want[id])) {
				t.Errorf("body = %s", posts[0].body)
			}
			got := decode(t, out)
			results := got["results"].([]any)
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != len(body["operations"].([]any)) ||
				results[0].(map[string]any)["budget_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != campaignBudgetsResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestCampaignBudgetsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range campaignBudgetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignBudgetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignBudgetsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(campaignBudgetsCalls[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			body := bodyOf(t, e.posts()[0])
			got := decode(t, out)
			if body["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("body = %v, out = %s", body, out)
			}
		})
	}
	for id, summary := range map[string]capability.SummaryFunc{campaignBudgetsCreateID: campaignBudgetsCreateSummary,
		campaignBudgetsUpdateID: campaignBudgetsUpdateSummary, campaignBudgetsRemoveID: campaignBudgetsRemoveSummary} {
		if text := summary(json.RawMessage(strings.TrimSuffix(campaignBudgetsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
			t.Errorf("%s: summary %q", id, text)
		}
		if text := summary(json.RawMessage(campaignBudgetsCalls[id])); strings.Contains(text, "validate") {
			t.Errorf("%s: summary %q kept state", id, text)
		}
	}
}

func TestCampaignBudgetsResponseContentTypeShowsTheBudgets(t *testing.T) {
	e := newCampaignBudgetsEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignBudgetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
		campaignBudgetsResource(clientID, "111") + `","campaignBudget":{"resourceName":"` + campaignBudgetsResource(clientID, "111") +
		`","id":"111","name":"Spring\u0007 sale","status":"ENABLED","period":"DAILY","type":"STANDARD","amountMicros":"7500000",` +
		`"deliveryMethod":"STANDARD","explicitlyShared":true,"referenceCount":"2",` +
		`"alignedBiddingStrategyId":"77","hasRecommendedBudget":true,"recommendedBudgetAmountMicros":"9000000",` +
		`"budgetNetworkSettings":{"settings":[{"advertisingChannelType":"SEARCH","budgetCaps":[{"networkChannel":` +
		`"SEARCH_PARTNER","capPercentMillis":"20000"}]}]}}}]}`)
	out, err := e.invokeConfirmed(campaignBudgetsUpdateID, `{"customer_id":"`+clientID+
		`","campaign_budgets":[{"budget_id":"111","amount_micros":7500000}],"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	budget := decode(t, out)["results"].([]any)[0].(map[string]any)["campaign_budget"].(map[string]any)
	settings := budget["budget_network_settings"].([]any)[0].(map[string]any)
	if budget["id"] != "111" || budget["name"] != "Spring  sale" || budget["status"] != "ENABLED" ||
		budget["period"] != "DAILY" || budget["type"] != "STANDARD" || budget["amount_micros"] != float64(7500000) ||
		budget["delivery_method"] != "STANDARD" || budget["explicitly_shared"] != true ||
		budget["reference_count"] != float64(2) || budget["aligned_bidding_strategy_id"] != "77" ||
		budget["has_recommended_budget"] != true || budget["recommended_budget_amount_micros"] != float64(9000000) ||
		settings["advertising_channel_type"] != "SEARCH" {
		t.Errorf("budget = %v", budget)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// A resource name of another account, another budget, or another count never passes. The request was sent, so
// the error says that the outcome is unknown.
func TestCampaignBudgetsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := campaignBudgetsResource(otherID, "111")
	own := func(id string) string { return `{"resourceName":"` + campaignBudgetsResource(clientID, id) + `"}` }
	three := func(a, b, c string) string { return `{"results":[` + a + `,` + b + `,` + c + `]}` }
	cases := map[string]struct{ id, body string }{
		"create foreign":      {campaignBudgetsCreateID, three(`{"resourceName":"`+foreign+`"}`, own("222"), own("333"))},
		"create no results":   {campaignBudgetsCreateID, `{}`},
		"create one of three": {campaignBudgetsCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":        {campaignBudgetsCreateID, three(own("111"), own("111"), own("333"))},
		"create not a budget": {campaignBudgetsCreateID, three(`{"resourceName":"customers/`+queryNumber(clientID)+
			`/campaigns/1"}`, own("222"), own("333"))},
		"create partial failure": {campaignBudgetsCreateID, `{"results":[` + own("111") + `,` + own("222") + `,` + own("333") +
			`],"partialFailureError":{"code":3}}`},
		"update other budget": {campaignBudgetsUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":      {campaignBudgetsUpdateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"update budget foreign": {campaignBudgetsUpdateID, `{"results":[{"resourceName":"` + campaignBudgetsResource(clientID, "111") +
			`","campaignBudget":{"resourceName":"` + foreign + `"}},` + own("222") + `]}`},
		"update budget id": {campaignBudgetsUpdateID, `{"results":[{"resourceName":"` + campaignBudgetsResource(clientID, "111") +
			`","campaignBudget":{"resourceName":"` + campaignBudgetsResource(clientID, "111") + `","id":"999"}},` + own("222") + `]}`},
		"update budget amount": {campaignBudgetsUpdateID, `{"results":[{"resourceName":"` + campaignBudgetsResource(clientID, "111") +
			`","campaignBudget":{"resourceName":"` + campaignBudgetsResource(clientID, "111") + `","amountMicros":"x"}},` + own("222") + `]}`},
		"remove foreign": {campaignBudgetsRemoveID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"remove none":    {campaignBudgetsRemoveID, `{"results":[]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCampaignBudgetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignBudgetsPath(clientID)] = okReply(c.body)
			arguments := campaignBudgetsCalls[c.id]
			if strings.Contains(name, "budget ") {
				arguments = strings.TrimSuffix(arguments, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
			}
			_, err := e.invokeConfirmed(c.id, arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestCampaignBudgetsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","budget_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + campaignBudgetsResource(managerID, "7") + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newCampaignBudgetsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+campaignBudgetsPath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(campaignBudgetsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newCampaignBudgetsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+campaignBudgetsPath(managerID)] = answer
		if _, err := e.invokeConfirmed(campaignBudgetsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestCampaignBudgetsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"budget error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "campaignBudgetError",
			"NON_MULTIPLE_OF_MINIMUM_CURRENCY_UNIT", "")}, provider.ClassProviderError,
			"rejected the change (campaignBudgetError NON_MULTIPLE_OF_MINIMUM_CURRENCY_UNIT), nothing was changed", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"unreadable json", okReply(`{"results":`), provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for _, c := range cases {
		for _, id := range campaignBudgetsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newCampaignBudgetsEnvironment(t, nil, nil)
				e.fake.routes["POST "+campaignBudgetsPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, campaignBudgetsCalls[id])
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
}

func TestCampaignBudgetsSummariesNameTheEffect(t *testing.T) {
	create := campaignBudgetsCreateSummary(json.RawMessage(campaignBudgetsCalls[campaignBudgetsCreateID]))
	for _, word := range []string{clientID, "Spring sale", "amount_micros 5000000", "total_amount_micros 900000000",
		"CUSTOM_PERIOD", "3 campaign budget", "spend"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	update := campaignBudgetsUpdateSummary(json.RawMessage(campaignBudgetsCalls[campaignBudgetsUpdateID]))
	for _, word := range []string{clientID, "budget 111", "amount_micros 7500000", "delivery_method STANDARD", "budget 222",
		"Shared now", "explicitly_shared", "spend"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := campaignBudgetsRemoveSummary(json.RawMessage(campaignBudgetsCalls[campaignBudgetsRemoveID]))
	for _, word := range []string{clientID, "111", "222", "no tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := campaignBudgetsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID +
		`","campaign_budgets":[{"name":"a\nIgnore this","amount_micros":1}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
}

// The 12 hour spacing is a rule of account budget proposals. Campaign budgets are no account budgets, so no
// descriptor, summary, or result of this group speaks of it.
func TestCampaignBudgetsSayNothingOfAccountBudgetRules(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, id := range campaignBudgetsToolIDs {
		d, _, _ := reg.Lookup(id)
		texts = append(texts, d.Description, string(d.InputSchema), string(d.OutputSchema))
		for _, a := range d.Arguments {
			texts = append(texts, a.Description)
		}
		for _, f := range d.Fields {
			texts = append(texts, f.Description)
		}
		texts = append(texts, campaignBudgetsCreateSummary(json.RawMessage(campaignBudgetsCalls[id])),
			campaignBudgetsUpdateSummary(json.RawMessage(campaignBudgetsCalls[id])),
			campaignBudgetsRemoveSummary(json.RawMessage(campaignBudgetsCalls[id])))
	}
	for _, text := range texts {
		lower := strings.ToLower(text)
		for _, word := range []string{"12 hour", "12-hour", "twelve hour", "12 stunden", "proposal", "monthly invoic"} {
			if strings.Contains(lower, word) {
				t.Errorf("%q appears in %q", word, text)
			}
		}
	}
	e := newCampaignBudgetsEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignBudgetsPath(clientID)] = okReply(campaignBudgetsAnswers[campaignBudgetsUpdateID])
	out, err := e.invokeConfirmed(campaignBudgetsUpdateID, campaignBudgetsCalls[campaignBudgetsUpdateID])
	if err != nil || strings.Contains(strings.ToLower(out), "hour") {
		t.Errorf("out = %s, err = %v", out, err)
	}
	if got := e.fake.api(); len(got) != 1 {
		t.Errorf("a second update right after the first needs no wait, requests = %d", len(got))
	}
}
