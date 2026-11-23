package googleads

import (
	"context"
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

var experimentArmsToolIDs = []string{experimentArmsCreateID, experimentArmsUpdateID, experimentArmsRemoveID}

func experimentArmsPath(id string) string { return "/v26/customers/" + id + "/experimentArms:mutate" }

func experimentArmsResource(customer, experiment, arm string) string {
	return "customers/" + queryNumber(customer) + "/experimentArms/" + experiment + "~" + arm
}

func newExperimentArmsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: experimentArmsToolIDs})
}

var experimentArmsCalls = map[string]string{
	experimentArmsCreateID: `{"customer_id":"` + clientID + `","experiment_arms":[{"experiment_id":"111","name":"Treatment",` +
		`"control":false,"traffic_split":50,"campaign_id":"31"},` +
		`{"experiment_id":"111","name":"Assets","asset_group":{"asset_group_id":"41","asset_group_assets":[` +
		`{"asset_id":"51","field_type":"HEADLINE"}]},"asset_testing_info":{"asset_variation_infos":[{"base_ad_id":"61",` +
		`"base_ad_group_id":"71","asset_details":[{"asset_id":"52","asset_detail_operation":"ADD","field_type":"DESCRIPTION"}]}]}},` +
		`{"experiment_id":"222","name":"Shop","performance_max_settings":{"bidding_strategy_type":"TARGET_ROAS",` +
		`"budget_amount_micros":5000000,"target_cpa_micros":2000000,"target_roas":3.5}}]}`,
	experimentArmsUpdateID: `{"customer_id":"` + clientID + `","experiment_arms":[{"experiment_id":"111","arm_id":"1",` +
		`"name":"Renamed","traffic_split":30},{"experiment_id":"222","arm_id":"2","control":true,"campaign_id":"32"}]}`,
	experimentArmsRemoveID: `{"customer_id":"` + clientID + `","experiment_arms":[{"experiment_id":"111","arm_id":"1"},` +
		`{"experiment_id":"222","arm_id":"2"}]}`,
}

func experimentArmsAnswer(pairs ...[2]string) string {
	var parts []string
	for _, p := range pairs {
		parts = append(parts, `{"resourceName":"`+experimentArmsResource(clientID, p[0], p[1])+`"}`)
	}
	return `{"results":[` + strings.Join(parts, ",") + `]}`
}

var experimentArmsAnswers = map[string]string{
	experimentArmsCreateID: experimentArmsAnswer([2]string{"111", "1"}, [2]string{"111", "2"}, [2]string{"222", "3"}),
	experimentArmsUpdateID: experimentArmsAnswer([2]string{"111", "1"}, [2]string{"222", "2"}),
	experimentArmsRemoveID: experimentArmsAnswer([2]string{"111", "1"}, [2]string{"222", "2"}),
}

func TestExperimentArmsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{experimentArmsCreateID: capability.EffectCreate,
		experimentArmsUpdateID: capability.EffectUpdate, experimentArmsRemoveID: capability.EffectDelete}
	for _, id := range experimentArmsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		if d.RequiresToolAllowList != (id != experimentArmsUpdateID) {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		note := mutateListListedNote
		if id == experimentArmsUpdateID {
			note = mutateListNote
		}
		if !strings.HasSuffix(d.Description, note) || (id == experimentArmsUpdateID && strings.HasSuffix(d.Description, mutateListListedNote)) {
			t.Errorf("%s: description does not end on the note of how it is offered", id)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "resource_name",
			"update_mask", "response_content_type"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if got := metadata.Token.Scopes[id]; !reflect.DeepEqual(got, []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, got)
		}
	}
	create, _, _ := reg.Lookup(experimentArmsCreateID)
	for _, text := range []string{`"maxLength":1024`, `"maximum":100`, `"maxItems":10`, `"TARGET_ROAS"`, `"HEADLINE"`, `"ADD"`} {
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
	for _, unknown := range []string{`"UNKNOWN"`, `"UNSPECIFIED"`, `"INVALID"`, `"PAGE_ONE_PROMOTED"`} {
		if strings.Contains(string(create.InputSchema), unknown) {
			t.Errorf("create schema offers %s", unknown)
		}
	}
}

func TestExperimentArmsToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+experimentArmsPath(clientID)] = okReply(experimentArmsAnswers[id])
		_, err := e.invokeConfirmed(id, experimentArmsCalls[id])
		if err != nil {
			e.untouched(t)
		}
		return err == nil
	}
	cases := []struct {
		name string
		conn config.Connection
		want map[string]bool
	}{
		{"all effects", config.Connection{Effects: all},
			map[string]bool{experimentArmsCreateID: false, experimentArmsUpdateID: true, experimentArmsRemoveID: false}},
		{"read only", config.Connection{Effects: []config.Effect{config.EffectRead}},
			map[string]bool{experimentArmsCreateID: false, experimentArmsUpdateID: false, experimentArmsRemoveID: false}},
		{"create listed", config.Connection{Tools: []string{experimentArmsCreateID}},
			map[string]bool{experimentArmsCreateID: true, experimentArmsUpdateID: false, experimentArmsRemoveID: false}},
		{"remove listed", config.Connection{Tools: []string{experimentArmsRemoveID}},
			map[string]bool{experimentArmsCreateID: false, experimentArmsUpdateID: false, experimentArmsRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range experimentArmsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestExperimentArmsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range experimentArmsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExperimentArmsEnvironment(t, nil, nil)
			_, err := e.invoke(id, experimentArmsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			if id == experimentArmsCreateID && (!strings.Contains(needed.Summary, "budget 5000000 micros") ||
				!strings.Contains(needed.Summary, "bidding TARGET_ROAS") || !strings.Contains(needed.Summary, "target ROAS 3.5")) {
				t.Errorf("summary hides the budget and bidding: %s", needed.Summary)
			}
			e.untouched(t)
		})
	}
}

func TestExperimentArmsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"update_mask":"name"`, `"response_content_type":"MUTABLE_RESOURCE"`, `"resource_name":"customers/1/experimentArms/1~2"`} {
		for _, id := range experimentArmsToolIDs {
			e := newExperimentArmsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(experimentArmsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestExperimentArmsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for id, call := range experimentArmsCalls {
		t.Run(id, func(t *testing.T) {
			e := newExperimentArmsEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invokeConfirmed(id, strings.Replace(call, clientID, otherID, 1))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestExperimentArmsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	create := func(fields string) string {
		return head + `"experiment_arms":[{"experiment_id":"1","name":"a"` + fields + `}]}`
	}
	edit := func(fields string) string {
		return head + `"experiment_arms":[{"experiment_id":"1","arm_id":"2"` + fields + `}]}`
	}
	many := func(entry string, n int) string {
		return head + `"experiment_arms":[` + strings.TrimSuffix(strings.Repeat(entry+",", n), ",") + `]}`
	}
	ref := `{"experiment_id":"1","arm_id":"2"}`
	bad := map[string][2]string{
		"create none":              {experimentArmsCreateID, head + `"experiment_arms":[]}`},
		"create no experiment":     {experimentArmsCreateID, head + `"experiment_arms":[{"name":"a"}]}`},
		"create experiment name":   {experimentArmsCreateID, head + `"experiment_arms":[{"experiment_id":"customers/1/experiments/2","name":"a"}]}`},
		"create no name":           {experimentArmsCreateID, head + `"experiment_arms":[{"experiment_id":"1"}]}`},
		"create empty name":        {experimentArmsCreateID, create(`,"name":""`)},
		"create name too long":     {experimentArmsCreateID, head + `"experiment_arms":[{"experiment_id":"1","name":"` + strings.Repeat("a", 1025) + `"}]}`},
		"create line feed":         {experimentArmsCreateID, head + `"experiment_arms":[{"experiment_id":"1","name":"a\nb"}]}`},
		"create split zero":        {experimentArmsCreateID, create(`,"traffic_split":0`)},
		"create split over":        {experimentArmsCreateID, create(`,"traffic_split":101`)},
		"create campaign name":     {experimentArmsCreateID, create(`,"campaign_id":"customers/9/campaigns/1"`)},
		"create campaigns list":    {experimentArmsCreateID, create(`,"campaigns":["customers/9/campaigns/1"]`)},
		"create campaign zero":     {experimentArmsCreateID, create(`,"campaign_id":"0"`)},
		"create group name":        {experimentArmsCreateID, create(`,"asset_group":{"asset_group_id":"customers/9/assetGroups/1"}`)},
		"create group asset type":  {experimentArmsCreateID, create(`,"asset_group":{"asset_group_id":"1","asset_group_assets":[{"asset_id":"2","field_type":"UNKNOWN"}]}`)},
		"create group assets none": {experimentArmsCreateID, create(`,"asset_group":{"asset_group_id":"1","asset_group_assets":[]}`)},
		"create variation none":    {experimentArmsCreateID, create(`,"asset_testing_info":{"asset_variation_infos":[]}`)},
		"create base ad name":      {experimentArmsCreateID, create(`,"asset_testing_info":{"asset_variation_infos":[{"base_ad_id":"customers/9/ads/1","base_ad_group_id":"2"}]}`)},
		"create detail operation":  {experimentArmsCreateID, create(`,"asset_testing_info":{"asset_variation_infos":[{"base_ad_id":"1","base_ad_group_id":"2","asset_details":[{"asset_id":"3","asset_detail_operation":"UNKNOWN","field_type":"HEADLINE"}]}]}`)},
		"create pmax empty":        {experimentArmsCreateID, create(`,"performance_max_settings":{}`)},
		"create pmax strategy":     {experimentArmsCreateID, create(`,"performance_max_settings":{"bidding_strategy_type":"PAGE_ONE_PROMOTED"}`)},
		"create pmax budget zero":  {experimentArmsCreateID, create(`,"performance_max_settings":{"budget_amount_micros":0}`)},
		"create pmax budget frac":  {experimentArmsCreateID, create(`,"performance_max_settings":{"budget_amount_micros":1.5}`)},
		"create pmax roas zero":    {experimentArmsCreateID, create(`,"performance_max_settings":{"target_roas":0}`)},
		"create pmax roas high":    {experimentArmsCreateID, create(`,"performance_max_settings":{"target_roas":1001}`)},
		"create too many":          {experimentArmsCreateID, many(`{"experiment_id":"1","name":"a"}`, 11)},
		"update no ids":            {experimentArmsUpdateID, head + `"experiment_arms":[{"name":"a"}]}`},
		"update no arm":            {experimentArmsUpdateID, head + `"experiment_arms":[{"experiment_id":"1","name":"a"}]}`},
		"update composite arm":     {experimentArmsUpdateID, head + `"experiment_arms":[{"experiment_id":"1","arm_id":"1~2","name":"a"}]}`},
		"update zero arm":          {experimentArmsUpdateID, head + `"experiment_arms":[{"experiment_id":"1","arm_id":"0","name":"a"}]}`},
		"update nothing":           {experimentArmsUpdateID, edit(``)},
		"update twice":             {experimentArmsUpdateID, head + `"experiment_arms":[{"experiment_id":"1","arm_id":"2","name":"a"},{"experiment_id":"1","arm_id":"2","name":"b"}]}`},
		"update experiment change": {experimentArmsUpdateID, edit(`,"name":"a","experiment":"customers/1/experiments/3"`)},
		"update pmax":              {experimentArmsUpdateID, edit(`,"performance_max_settings":{"target_roas":2}`)},
		"update split":             {experimentArmsUpdateID, edit(`,"traffic_split":0`)},
		"remove none":              {experimentArmsRemoveID, head + `"experiment_arms":[]}`},
		"remove name":              {experimentArmsRemoveID, head + `"experiment_arms":[{"experiment_id":"customers/1/experiments/2","arm_id":"2"}]}`},
		"remove twice":             {experimentArmsRemoveID, head + `"experiment_arms":[` + ref + `,` + ref + `]}`},
		"remove too many": {experimentArmsRemoveID, head + `"experiment_arms":[` + func() string {
			var parts []string
			for i := 1; i <= 11; i++ {
				parts = append(parts, strings.Replace(ref, `"2"`, `"`+strconv.Itoa(i+10)+`"`, 1))
			}
			return strings.Join(parts, ",")
		}() + `]}`},
		"bad customer": {experimentArmsRemoveID, `{"customer_id":"12-34","experiment_arms":[` + ref + `]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newExperimentArmsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newExperimentArmsEnvironment(t, nil, nil)
		e.fake.routes["POST "+experimentArmsPath(clientID)] = okReply(experimentArmsAnswer([2]string{"1", "5"}))
		_, err := e.invokeConfirmed(experimentArmsCreateID, head+`"experiment_arms":[{"experiment_id":"1","name":"`+
			strings.Repeat("ä", 1024)+`","traffic_split":100}]}`)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestExperimentArmsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		experimentArmsCreateID: `[{"create":{"experiment":"customers/1000000003/experiments/111","name":"Treatment",` +
			`"control":false,"trafficSplit":"50","campaigns":["customers/1000000003/campaigns/31"]}},` +
			`{"create":{"experiment":"customers/1000000003/experiments/111","name":"Assets","assetGroups":[{` +
			`"assetGroup":"customers/1000000003/assetGroups/41","assetGroupAssets":[{"asset":"customers/1000000003/assets/51",` +
			`"fieldType":"HEADLINE"}]}],"assetTestingInfo":{"assetVariationInfos":[{"baseAd":"customers/1000000003/ads/61",` +
			`"baseAdGroup":"customers/1000000003/adGroups/71","assetDetails":[{"asset":"customers/1000000003/assets/52",` +
			`"assetDetailOperation":"ADD","fieldType":"DESCRIPTION"}]}]}}},` +
			`{"create":{"experiment":"customers/1000000003/experiments/222","name":"Shop","performanceMaxExperimentArmInfo":{` +
			`"experimentalPerformanceMaxCampaignSettings":{"biddingStrategyType":"TARGET_ROAS","budgetAmountMicros":"5000000",` +
			`"targetCpaMicros":"2000000","targetRoas":3.5}}}}]`,
		experimentArmsUpdateID: `[{"update":{"resourceName":"customers/1000000003/experimentArms/111~1","name":"Renamed",` +
			`"trafficSplit":"30"},"updateMask":"name,trafficSplit"},` +
			`{"update":{"resourceName":"customers/1000000003/experimentArms/222~2","control":true,` +
			`"campaigns":["customers/1000000003/campaigns/32"]},"updateMask":"control,campaigns"}]`,
		experimentArmsRemoveID: `[{"remove":"customers/1000000003/experimentArms/111~1"},` +
			`{"remove":"customers/1000000003/experimentArms/222~2"}]`,
	}
	for _, id := range experimentArmsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExperimentArmsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+experimentArmsPath(clientID)] = okReply(experimentArmsAnswers[id])
			out, err := e.invokeConfirmed(id, experimentArmsCalls[id])
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
			first := results[0].(map[string]any)
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != len(wantWire(t, want[id]).([]any)) ||
				first["experiment_id"] != "111" || first["arm_id"] != "1" ||
				first["resource_name"] != experimentArmsResource(clientID, "111", "1") {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestExperimentArmsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range experimentArmsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExperimentArmsEnvironment(t, nil, nil)
			e.fake.routes["POST "+experimentArmsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(experimentArmsCalls[id], "}")+`,"validate_only":true}`)
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
	for id, summary := range map[string]capability.SummaryFunc{experimentArmsCreateID: experimentArmsCreateSummary,
		experimentArmsUpdateID: experimentArmsUpdateSummary, experimentArmsRemoveID: experimentArmsRemoveSummary} {
		if text := summary(json.RawMessage(strings.TrimSuffix(experimentArmsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
			t.Errorf("%s: summary %q", id, text)
		}
		if text := summary(json.RawMessage(experimentArmsCalls[id])); strings.Contains(text, "validate") || !strings.Contains(text, clientID) {
			t.Errorf("%s: summary %q", id, text)
		}
	}
}

func TestExperimentArmsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := experimentArmsResource(otherID, "111", "1")
	own := func(e, a string) string { return `{"resourceName":"` + experimentArmsResource(clientID, e, a) + `"}` }
	cases := map[string]struct{ id, body string }{
		"update foreign":         {experimentArmsUpdateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222", "2") + `]}`},
		"update other arm":       {experimentArmsUpdateID, `{"results":[` + own("111", "9") + `,` + own("222", "2") + `]}`},
		"update swapped":         {experimentArmsUpdateID, `{"results":[` + own("222", "2") + `,` + own("111", "1") + `]}`},
		"update one of two":      {experimentArmsUpdateID, `{"results":[` + own("111", "1") + `]}`},
		"update partial failure": {experimentArmsUpdateID, `{"results":[` + own("111", "1") + `,` + own("222", "2") + `],"partialFailureError":{"code":3}}`},
		"remove foreign":         {experimentArmsRemoveID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222", "2") + `]}`},
		"remove an experiment": {experimentArmsRemoveID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) +
			`/experiments/111"},` + own("222", "2") + `]}`},
		"remove no tilde": {experimentArmsRemoveID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) +
			`/experimentArms/111"},` + own("222", "2") + `]}`},
		"remove none":    {experimentArmsRemoveID, `{"results":[]}`},
		"create twice":   {experimentArmsCreateID, `{"results":[` + own("111", "1") + `,` + own("111", "1") + `,` + own("222", "3") + `]}`},
		"create foreign": {experimentArmsCreateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("111", "2") + `,` + own("222", "3") + `]}`},
		"create none":    {experimentArmsCreateID, `{}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newExperimentArmsEnvironment(t, nil, nil)
			e.fake.routes["POST "+experimentArmsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, experimentArmsCalls[c.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestExperimentArmsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","experiment_arms":[{"experiment_id":"7","arm_id":"8"}]}`
	answer := `{"results":[{"resourceName":"` + experimentArmsResource(managerID, "7", "8") + `"}]}`
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newExperimentArmsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+experimentArmsPath(managerID)] = okReply(answer)
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(experimentArmsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newExperimentArmsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+experimentArmsPath(managerID)] = okReply(answer)
		if _, err := e.invokeConfirmed(experimentArmsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
	})
}

func TestExperimentArmsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"arm error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "experimentArmError", "TRAFFIC_SPLIT_NOT_EQUAL_TO_100", "")},
			provider.ClassProviderError, "rejected the change (experimentArmError TRAFFIC_SPLIT_NOT_EQUAL_TO_100), nothing was changed", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
	}
	for _, c := range cases {
		for _, id := range experimentArmsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newExperimentArmsEnvironment(t, nil, nil)
				e.fake.routes["POST "+experimentArmsPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, experimentArmsCalls[id])
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
