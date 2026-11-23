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

var assetGroupSignalToolIDs = []string{assetGroupSignalsCreateID, assetGroupSignalsRemoveID}

const signalsPathTail = "/assetGroupSignals:mutate"

func signalsPath(customer string) string { return "/v26/customers/" + customer + signalsPathTail }

func signalRes(customer, collection, id string) string {
	return "customers/" + queryNumber(customer) + "/" + collection + "/" + id
}

var (
	signalGroup    = signalRes(clientID, "assetGroups", "31")
	signalAudience = signalRes(clientID, "audiences", "55")
	signalNameA    = signalRes(clientID, "assetGroupSignals", "31~41")
	signalNameB    = signalRes(clientID, "assetGroupSignals", "31~42")
)

func signalHead() string { return `{"customer_id":"` + clientID + `",` }

func signalCreateArgs(entries string, extra string) string {
	return signalHead() + `"asset_group_signals":[` + entries + `]` + extra + `}`
}

const signalTheme = `{"asset_group":"customers/1000000003/assetGroups/31","search_theme":{"text":"running shoes"}}`
const signalAud = `{"asset_group":"customers/1000000003/assetGroups/31","audience":{"audience":"customers/1000000003/audiences/55"}}`

var signalCreate = signalCreateArgs(signalTheme+","+signalAud, "")
var signalRemove = signalHead() + `"resource_names":["` + signalNameA + `","` + signalNameB + `"]}`

func signalAnswer() string {
	return `{"results":[{"resourceName":"` + signalNameA + `"},{"resourceName":"` + signalNameB + `"}]}`
}

func newSignalEnvironment(t *testing.T, targets []string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
		config.EffectCreate, config.EffectDelete}})
}

func signalCall(verb string) string {
	if verb == "create" {
		return signalCreate
	}
	return signalRemove
}

func signalTool(verb string) string { return Provider + ".asset-group-signals." + verb }

func TestAssetGroupSignalToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{"create": capability.EffectCreate, "remove": capability.EffectDelete}
	for verb, effect := range wantEffect {
		id := signalTool(verb)
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != effect || len(d.OutputSchema) == 0 || len(d.InputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		// Like the other links, neither tool is listed-only: a connection with the matching effect offers it.
		if d.RequiresToolAllowList || !strings.HasSuffix(d.Description, mutateListNote) {
			t.Errorf("%s: offered by the tools list only = %v", id, d.RequiresToolAllowList)
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
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "update_mask"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if _, found := input.Properties["response_content_type"]; found != (verb == "create") {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		for _, word := range []string{"outcome is unknown", "atomic", "MutateAssetGroupSignals"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
	}
	d, _, _ := reg.Lookup(signalTool("create"))
	for _, word := range []string{`"search_theme"`, `"audience"`, `"exempt_policy_violation_keys"`} {
		if !strings.Contains(string(d.InputSchema), word) {
			t.Errorf("create schema lacks %s", word)
		}
	}
	for _, word := range []string{"approval_status", "disapproval_reasons", "search_theme"} {
		if !strings.Contains(string(d.OutputSchema), word) {
			t.Errorf("create output schema lacks %s", word)
		}
	}
	if _, _, ok := reg.Lookup(signalTool("update")); ok {
		t.Error("there is an update tool")
	}
}

func TestAssetGroupSignalToolsAreReachedByTheMatchingEffectOrTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, verb string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+signalsPath(clientID)] = okReply(signalAnswer())
		_, err := e.invokeConfirmed(signalTool(verb), signalCall(verb))
		if err != nil {
			e.untouched(t)
		}
		return err == nil
	}
	cases := []struct {
		name         string
		conn         config.Connection
		create, remv bool
	}{
		{"all effects", config.Connection{Effects: all}, true, true},
		{"create effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}}, true, false},
		{"delete effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectDelete}}, false, true},
		{"update effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectUpdate}}, false, false},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}}, false, false},
		{"remove listed", config.Connection{Tools: []string{signalTool("remove")}}, false, true},
		{"create listed", config.Connection{Tools: []string{signalTool("create")}}, true, false},
	}
	for _, k := range cases {
		t.Run(k.name, func(t *testing.T) {
			if got := reached(k.conn, "create"); got != k.create {
				t.Errorf("create reached = %v, want %v", got, k.create)
			}
			if got := reached(k.conn, "remove"); got != k.remv {
				t.Errorf("remove reached = %v, want %v", got, k.remv)
			}
		})
	}
}

func TestAssetGroupSignalsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, verb := range []string{"create", "remove"} {
		t.Run(verb, func(t *testing.T) {
			e := newSignalEnvironment(t, nil)
			_, err := e.invoke(signalTool(verb), signalCall(verb))
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestAssetGroupSignalsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"status"`} {
		for _, verb := range []string{"create", "remove"} {
			e := newSignalEnvironment(t, nil)
			arguments := strings.TrimSuffix(signalCall(verb), "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(signalTool(verb), arguments); err == nil {
				t.Errorf("%s accepted %s", verb, extra)
			}
			e.untouched(t)
		}
	}
	e := newSignalEnvironment(t, nil)
	arguments := strings.TrimSuffix(signalRemove, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
	if _, err := e.invokeConfirmed(signalTool("remove"), arguments); err == nil {
		t.Error("remove accepted response_content_type")
	}
	e.untouched(t)
}

func TestAssetGroupSignalTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, verb := range []string{"create", "remove"} {
		t.Run(verb, func(t *testing.T) {
			e := newSignalEnvironment(t, []string{"customers/" + clientID})
			arguments := strings.ReplaceAll(signalCall(verb), clientID, otherID)
			_, err := e.invokeConfirmed(signalTool(verb), arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestAssetGroupSignalArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	group := `"asset_group":"` + signalGroup + `"`
	theme := `"search_theme":{"text":"shoes"}`
	aud := `"audience":{"audience":"` + signalAudience + `"}`
	name := func(id string) string { return `"` + signalRes(clientID, "assetGroupSignals", id) + `"` }
	var many []string
	for i := 0; i < 11; i++ {
		many = append(many, `{`+group+`,`+theme+`}`)
	}
	var manyNames []string
	for _, id := range []string{"1~1", "1~2", "1~3", "1~4", "1~5", "1~6", "1~7", "1~8", "1~9", "1~10", "1~11"} {
		manyNames = append(manyNames, name(id))
	}
	bad := map[string][2]string{
		"create none":            {"create", signalCreateArgs("", "")},
		"create too many":        {"create", signalCreateArgs(strings.Join(many, ","), "")},
		"create not object":      {"create", signalCreateArgs(`"x"`, "")},
		"create no signal":       {"create", signalCreateArgs(`{`+group+`}`, "")},
		"create both":            {"create", signalCreateArgs(`{`+group+`,`+aud+`,`+theme+`}`, "")},
		"create no group":        {"create", signalCreateArgs(`{`+theme+`}`, "")},
		"create foreign group":   {"create", signalCreateArgs(`{"asset_group":"`+signalRes(otherID, "assetGroups", "31")+`",`+theme+`}`, "")},
		"create other kind":      {"create", signalCreateArgs(`{"asset_group":"`+signalRes(clientID, "campaigns", "31")+`",`+theme+`}`, "")},
		"create temporary group": {"create", signalCreateArgs(`{"asset_group":"`+signalRes(clientID, "assetGroups", "-31")+`",`+theme+`}`, "")},
		"create zero group":      {"create", signalCreateArgs(`{"asset_group":"`+signalRes(clientID, "assetGroups", "0")+`",`+theme+`}`, "")},
		"create foreign audience": {"create", signalCreateArgs(`{`+group+`,"audience":{"audience":"`+
			signalRes(otherID, "audiences", "55")+`"}}`, "")},
		"create audience kind": {"create", signalCreateArgs(`{`+group+`,"audience":{"audience":"`+
			signalRes(clientID, "userLists", "55")+`"}}`, "")},
		"create temporary audience": {"create", signalCreateArgs(`{`+group+`,"audience":{"audience":"`+
			signalRes(clientID, "audiences", "-5")+`"}}`, "")},
		"create audience extra": {"create", signalCreateArgs(`{`+group+`,"audience":{"audience":"`+signalAudience+`","x":"y"}}`, "")},
		"create audience empty": {"create", signalCreateArgs(`{`+group+`,"audience":{}}`, "")},
		"create theme blank":    {"create", signalCreateArgs(`{`+group+`,"search_theme":{"text":"  "}}`, "")},
		"create theme empty":    {"create", signalCreateArgs(`{`+group+`,"search_theme":{"text":""}}`, "")},
		"create theme control":  {"create", signalCreateArgs(`{`+group+`,"search_theme":{"text":"a\nb"}}`, "")},
		"create theme long":     {"create", signalCreateArgs(`{`+group+`,"search_theme":{"text":"`+strings.Repeat("a", 256)+`"}}`, "")},
		"create theme number":   {"create", signalCreateArgs(`{`+group+`,"search_theme":{"text":5}}`, "")},
		"create vertical foreign": {"create", signalCreateArgs(`{`+group+`,"vertical_ads_item_group_rule_list":{"shared_set":"`+
			signalRes(otherID, "sharedSets", "5")+`"}}`, "")},
		"create vertical kind": {"create", signalCreateArgs(`{`+group+`,"vertical_ads_item_group_rule_list":{"shared_set":"`+
			signalRes(clientID, "campaigns", "5")+`"}}`, "")},
		"create vertical temporary": {"create", signalCreateArgs(`{`+group+`,"vertical_ads_item_group_rule_list":{"shared_set":"`+
			signalRes(clientID, "sharedSets", "-5")+`"}}`, "")},
		"create vertical extra": {"create", signalCreateArgs(`{`+group+`,"vertical_ads_item_group_rule_list":{"shared_set":"`+
			signalRes(clientID, "sharedSets", "5")+`","x":"y"}}`, "")},
		"create local blank":     {"create", signalCreateArgs(`{`+group+`,"local_services_id":{"service_id":" "}}`, "")},
		"create local empty":     {"create", signalCreateArgs(`{`+group+`,"local_services_id":{}}`, "")},
		"create local control":   {"create", signalCreateArgs(`{`+group+`,"local_services_id":{"service_id":"a\nb"}}`, "")},
		"create local long":      {"create", signalCreateArgs(`{`+group+`,"local_services_id":{"service_id":"`+strings.Repeat("a", 129)+`"}}`, "")},
		"create local and theme": {"create", signalCreateArgs(`{`+group+`,`+theme+`,"local_services_id":{"service_id":"x"}}`, "")},
		"create unknown member":  {"create", signalCreateArgs(`{`+group+`,`+theme+`,"status":"ENABLED"}`, "")},
		"create duplicate":       {"create", signalCreateArgs(`{`+group+`,`+theme+`},{`+group+`,`+theme+`}`, "")},
		"create exempt empty":    {"create", signalCreateArgs(`{`+group+`,`+theme+`,"exempt_policy_violation_keys":[]}`, "")},
		"create exempt no name":  {"create", signalCreateArgs(`{`+group+`,`+theme+`,"exempt_policy_violation_keys":[{"violating_text":"x"}]}`, "")},
		"create content type":    {"create", signalCreateArgs(`{`+group+`,`+theme+`}`, `,"response_content_type":"ALL"`)},
		"create only flag":       {"create", signalHead() + `"validate_only":true}`},
		"remove none":            {"remove", signalHead() + `"resource_names":[]}`},
		"remove too many":        {"remove", signalHead() + `"resource_names":[` + strings.Join(manyNames, ",") + `]}`},
		"remove one id":          {"remove", signalHead() + `"resource_names":[` + name("31") + `]}`},
		"remove three ids":       {"remove", signalHead() + `"resource_names":[` + name("1~2~3") + `]}`},
		"remove foreign":         {"remove", signalHead() + `"resource_names":["` + signalRes(otherID, "assetGroupSignals", "31~41") + `"]}`},
		"remove other kind":      {"remove", signalHead() + `"resource_names":["` + signalRes(clientID, "assetGroups", "31~41") + `"]}`},
		"remove zero":            {"remove", signalHead() + `"resource_names":[` + name("0~41") + `]}`},
		"remove temporary":       {"remove", signalHead() + `"resource_names":[` + name("-31~41") + `]}`},
		"remove padded":          {"remove", signalHead() + `"resource_names":[` + name("031~41") + `]}`},
		"remove duplicate":       {"remove", signalHead() + `"resource_names":[` + name("31~41") + `,` + name("31~41") + `]}`},
		"remove text":            {"remove", signalHead() + `"resource_names":["x"]}`},
	}
	for label, k := range bad {
		t.Run(label, func(t *testing.T) {
			e := newSignalEnvironment(t, nil)
			if _, err := e.invokeConfirmed(signalTool(k[0]), k[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newSignalEnvironment(t, nil)
		var items, results []string
		for i := 1; i <= 10; i++ {
			items = append(items, `{`+group+`,"search_theme":{"text":"theme `+strconv.Itoa(i)+`"}}`)
			results = append(results, `{"resourceName":"`+signalRes(clientID, "assetGroupSignals", "31~"+strconv.Itoa(40+i))+`"}`)
		}
		e.fake.routes["POST "+signalsPath(clientID)] = okReply(`{"results":[` + strings.Join(results, ",") + `]}`)
		if _, err := e.invokeConfirmed(signalTool("create"), signalCreateArgs(strings.Join(items, ","), "")); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAssetGroupSignalsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		"create": `[{"create":{"assetGroup":"` + signalGroup + `","searchTheme":{"text":"running shoes"}}},` +
			`{"create":{"assetGroup":"` + signalGroup + `","audience":{"audience":"` + signalAudience + `"}}}]`,
		"remove": `[{"remove":"` + signalNameA + `"},{"remove":"` + signalNameB + `"}]`,
	}
	for _, verb := range []string{"create", "remove"} {
		t.Run(verb, func(t *testing.T) {
			e := newEnvironmentWith(t, nil, map[string]string{optionLoginCustomerID: loginID},
				config.Connection{Tools: assetGroupSignalToolIDs})
			e.fake.routes["POST "+signalsPath(clientID)] = okReply(signalAnswer())
			out, err := e.invokeConfirmed(signalTool(verb), signalCall(verb))
			if err != nil {
				t.Fatal(err)
			}
			posts := e.posts()
			if len(posts) != 1 || posts[0].login != loginID || posts[0].developerToken != "" {
				t.Fatalf("posts = %+v", posts)
			}
			body := bodyOf(t, posts[0])
			if len(body) != 1 || !reflect.DeepEqual(body["operations"], wantWire(t, want[verb])) {
				t.Errorf("body = %s", posts[0].body)
			}
			got := decode(t, out)
			results := got["results"].([]any)
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
				results[1].(map[string]any)["resource_name"] != signalNameB || len(results[1].(map[string]any)) != 1 {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestAssetGroupSignalsLocalServicesAndVerticalAdsSendTheirOwnMembers(t *testing.T) {
	set := signalRes(clientID, "sharedSets", "5")
	entries := `{"asset_group":"` + signalGroup + `","local_services_id":{"service_id":"install_faucet"}},` +
		`{"asset_group":"` + signalGroup + `","vertical_ads_item_group_rule_list":{"shared_set":"` + set + `"},` +
		`"exempt_policy_violation_keys":[{"policy_name":"P"}]}`
	e := newSignalEnvironment(t, nil)
	e.fake.routes["POST "+signalsPath(clientID)] = okReply(signalAnswer())
	if _, err := e.invokeConfirmed(signalTool("create"), signalCreateArgs(entries, "")); err != nil {
		t.Fatal(err)
	}
	want := `[{"create":{"assetGroup":"` + signalGroup + `","localServicesId":{"serviceId":"install_faucet"}}},` +
		`{"create":{"assetGroup":"` + signalGroup + `","verticalAdsItemGroupRuleList":{"sharedSet":"` + set + `"}},` +
		`"exemptPolicyViolationKeys":[{"policyName":"P"}]}]`
	if got := bodyOf(t, e.posts()[0])["operations"]; !reflect.DeepEqual(got, wantWire(t, want)) {
		t.Errorf("operations = %v", got)
	}
	text := assetGroupSignalSummary(assetGroupSignalsCreateID)(json.RawMessage(signalCreateArgs(entries, "")))
	for _, word := range []string{"local services ID install_faucet", "vertical ads rule list", "selects which feed items may serve", "policy exemption"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
	// The answer shows both members, bound to the account.
	e = newSignalEnvironment(t, nil)
	signal := `{"resourceName":"` + signalNameA + `","assetGroup":"` + signalGroup + `","localServicesId":{"serviceId":"x"},` +
		`"verticalAdsItemGroupRuleList":{"sharedSet":"` + set + `"}}`
	e.fake.routes["POST "+signalsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + signalNameA +
		`","assetGroupSignal":` + signal + `},{"resourceName":"` + signalNameB + `"}]}`)
	out, err := e.invokeConfirmed(signalTool("create"), strings.TrimSuffix(signalCreate, "}")+`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	item := decode(t, out)["results"].([]any)[0].(map[string]any)["asset_group_signal"].(map[string]any)
	if item["local_services_id"].(map[string]any)["service_id"] != "x" ||
		item["vertical_ads_item_group_rule_list"].(map[string]any)["shared_set"] != set {
		t.Errorf("signal = %v", item)
	}
}

func TestAssetGroupSignalExemptionKeysGoIntoTheOperation(t *testing.T) {
	e := newSignalEnvironment(t, nil)
	e.fake.routes["POST "+signalsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + signalNameA + `"}]}`)
	entry := `{"asset_group":"` + signalGroup + `","search_theme":{"text":"shoes"},"exempt_policy_violation_keys":` +
		`[{"policy_name":"HEALTH","violating_text":"cure"}]}`
	if _, err := e.invokeConfirmed(signalTool("create"), signalCreateArgs(entry, "")); err != nil {
		t.Fatal(err)
	}
	want := `[{"create":{"assetGroup":"` + signalGroup + `","searchTheme":{"text":"shoes"}},` +
		`"exemptPolicyViolationKeys":[{"policyName":"HEALTH","violatingText":"cure"}]}]`
	if got := bodyOf(t, e.posts()[0])["operations"]; !reflect.DeepEqual(got, wantWire(t, want)) {
		t.Errorf("operations = %v", got)
	}
	text := assetGroupSignalSummary(assetGroupSignalsCreateID)(json.RawMessage(signalCreateArgs(entry, "")))
	for _, word := range []string{"policy exemption", "cannot serve", "shoes"} {
		if !strings.Contains(text, word) {
			t.Errorf("summary %q lacks %q", text, word)
		}
	}
}

func TestAssetGroupSignalsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, verb := range []string{"create", "remove"} {
		t.Run(verb, func(t *testing.T) {
			e := newSignalEnvironment(t, nil)
			e.fake.routes["POST "+signalsPath(clientID)] = okReply(`{}`)
			arguments := strings.TrimSuffix(signalCall(verb), "}") + `,"validate_only":true}`
			out, err := e.invokeConfirmed(signalTool(verb), arguments)
			if err != nil {
				t.Fatal(err)
			}
			body := bodyOf(t, e.posts()[0])
			got := decode(t, out)
			if body["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil && len(got["results"].([]any)) != 0 {
				t.Errorf("body = %v, out = %s", body, out)
			}
		})
		summary := assetGroupSignalSummary(signalTool(verb))
		if text := summary(json.RawMessage(strings.TrimSuffix(signalCall(verb), "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
			t.Errorf("%s: summary %q", verb, text)
		}
		if text := summary(json.RawMessage(signalCall(verb))); strings.Contains(text, "validate") {
			t.Errorf("%s: summary %q kept state", verb, text)
		}
	}
}

func TestAssetGroupSignalsResponseContentTypeShowsTheSignalAndItsPolicyReview(t *testing.T) {
	e := newSignalEnvironment(t, nil)
	signal := `{"resourceName":"` + signalNameA + `","assetGroup":"` + signalGroup + `","searchTheme":{"text":"running shoes"},` +
		`"approvalStatus":"LIMITED","disapprovalReasons":["R1","R2"],"unknownMember":"x"}`
	e.fake.routes["POST "+signalsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + signalNameA +
		`","assetGroupSignal":` + signal + `},{"resourceName":"` + signalNameB + `"}]}`)
	out, err := e.invokeConfirmed(signalTool("create"), strings.TrimSuffix(signalCreate, "}")+`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	item := decode(t, out)["results"].([]any)[0].(map[string]any)["asset_group_signal"].(map[string]any)
	if item["approval_status"] != "LIMITED" || item["resource_name"] != signalNameA || item["unknownMember"] != nil ||
		item["asset_group"] != signalGroup || item["search_theme"].(map[string]any)["text"] != "running shoes" ||
		len(item["disapproval_reasons"].([]any)) != 2 || len(item) != 5 {
		t.Errorf("signal = %v", item)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// A resource name of another account, group or count never passes. The request was sent, so the error says that
// the outcome is unknown.
func TestAssetGroupSignalAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(name string) string { return `{"resourceName":"` + name + `"}` }
	foreign := signalRes(otherID, "assetGroupSignals", "31~41")
	withSignal := func(signal string) string {
		return `{"results":[{"resourceName":"` + signalNameA + `","assetGroupSignal":` + signal + `},` + own(signalNameB) + `]}`
	}
	cases := map[string]struct{ verb, body string }{
		"create foreign":         {"create", `{"results":[` + own(foreign) + `,` + own(signalNameB) + `]}`},
		"create no results":      {"create", `{}`},
		"create one of two":      {"create", `{"results":[` + own(signalNameA) + `]}`},
		"create other group":     {"create", `{"results":[` + own(signalRes(clientID, "assetGroupSignals", "99~41")) + `,` + own(signalNameB) + `]}`},
		"create one id":          {"create", `{"results":[` + own(signalRes(clientID, "assetGroupSignals", "31")) + `,` + own(signalNameB) + `]}`},
		"create three ids":       {"create", `{"results":[` + own(signalRes(clientID, "assetGroupSignals", "31~4~5")) + `,` + own(signalNameB) + `]}`},
		"create other kind":      {"create", `{"results":[` + own(signalRes(clientID, "assetGroups", "31~41")) + `,` + own(signalNameB) + `]}`},
		"create no name":         {"create", `{"results":[{},` + own(signalNameB) + `]}`},
		"create partial failure": {"create", `{"results":[` + own(signalNameA) + `,` + own(signalNameB) + `],"partialFailureError":{"code":3}}`},
		"create signal name":     {"create", withSignal(`{"resourceName":"` + signalNameB + `"}`)},
		"create signal group":    {"create", withSignal(`{"assetGroup":"` + signalRes(clientID, "assetGroups", "99") + `"}`)},
		"create signal foreign":  {"create", withSignal(`{"assetGroup":"` + signalRes(otherID, "assetGroups", "31") + `"}`)},
		"create audience":        {"create", withSignal(`{"audience":{"audience":"` + signalRes(otherID, "audiences", "55") + `"}}`)},
		"create shared set":      {"create", withSignal(`{"verticalAdsItemGroupRuleList":{"sharedSet":"` + signalRes(otherID, "sharedSets", "5") + `"}}`)},
		"create shape":           {"create", withSignal(`{"approvalStatus":5}`)},
		"create not object":      {"create", withSignal(`"text"`)},
		"remove foreign":         {"remove", `{"results":[` + own(foreign) + `,` + own(signalNameB) + `]}`},
		"remove other order":     {"remove", `{"results":[` + own(signalNameB) + `,` + own(signalNameA) + `]}`},
		"remove none":            {"remove", `{"results":[]}`},
	}
	for label, k := range cases {
		t.Run(label, func(t *testing.T) {
			e := newSignalEnvironment(t, nil)
			e.fake.routes["POST "+signalsPath(clientID)] = okReply(k.body)
			arguments := signalCall(k.verb)
			if strings.Contains(label, "signal") || strings.Contains(label, "audience") || strings.Contains(label, "shared") ||
				strings.Contains(label, "shape") || strings.Contains(label, "not object") {
				arguments = strings.TrimSuffix(arguments, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
			}
			_, err := e.invokeConfirmed(signalTool(k.verb), arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAssetGroupSignalsManagerAccountsNeedAManagerTarget(t *testing.T) {
	name := signalRes(managerID, "assetGroupSignals", "7~8")
	managerCall := `{"customer_id":"` + managerID + `","resource_names":["` + name + `"]}`
	answer := okReply(`{"results":[{"resourceName":"` + name + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newSignalEnvironment(t, []string{"customers/" + managerID})
		e.fake.routes["POST "+signalsPath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(signalTool("remove"), managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newSignalEnvironment(t, []string{"managers/" + managerID})
		e.fake.routes["POST "+signalsPath(managerID)] = answer
		if _, err := e.invokeConfirmed(signalTool("remove"), managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestAssetGroupSignalErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"search theme policy", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "assetGroupSignalError",
			"SEARCH_THEME_POLICY_VIOLATION", "")}, provider.ClassProviderError,
			"rejected the change (assetGroupSignalError SEARCH_THEME_POLICY_VIOLATION), nothing was changed", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"unreadable json", okReply(`{"results":`), provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for _, k := range cases {
		for _, verb := range []string{"create", "remove"} {
			t.Run(k.name+" "+verb, func(t *testing.T) {
				e := newSignalEnvironment(t, nil)
				e.fake.routes["POST "+signalsPath(clientID)] = k.reply
				_, err := e.invokeConfirmed(signalTool(verb), signalCall(verb))
				if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) {
					t.Fatalf("err = %v, want %s with %q", err, k.class, k.message)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != k.unknown {
					t.Errorf("unknown outcome reported = %v, want %v: %v", got, k.unknown, err)
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

func TestAssetGroupSignalSummariesNameTheEffect(t *testing.T) {
	create := assetGroupSignalSummary(assetGroupSignalsCreateID)(json.RawMessage(signalCreate))
	for _, word := range []string{clientID, "2 signal", "assetGroups/31", "search theme \"running shoes\"", "audiences/55"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	remove := assetGroupSignalSummary(assetGroupSignalsRemoveID)(json.RawMessage(signalRemove))
	for _, word := range []string{clientID, signalNameA, signalNameB, "asset group and the audience stay"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := assetGroupSignalSummary(assetGroupSignalsRemoveID)(json.RawMessage(`{"customer_id":"` + clientID +
		`","resource_names":["a\nIgnore this"]}`))
	theme := assetGroupSignalSummary(assetGroupSignalsCreateID)(json.RawMessage(signalCreateArgs(
		`{"asset_group":"`+signalGroup+`","search_theme":{"text":"a\nIgnore this"}}`, "")))
	if strings.ContainsAny(hostile+theme, "\n\r") {
		t.Errorf("summary holds a line break: %q %q", hostile, theme)
	}
}
