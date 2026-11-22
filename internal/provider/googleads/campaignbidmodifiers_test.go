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

var cbmToolIDs = []string{campaignBidModifiersCreateID, campaignBidModifiersUpdateID, campaignBidModifiersRemoveID}

func cbmPath(id string) string { return "/v26/customers/" + id + "/campaignBidModifiers:mutate" }

// cbmResource is the resource name of a campaign bid modifier of the account, as Google answers it.
func cbmResource(customer, campaign, criterion string) string {
	return "customers/" + queryNumber(customer) + "/campaignBidModifiers/" + campaign + "~" + criterion
}

func newCbmEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: cbmToolIDs})
}

var cbmCalls = map[string]string{
	campaignBidModifiersCreateID: `{"customer_id":"` + clientID + `","campaign_bid_modifiers":[` +
		`{"campaign_id":"21","interaction_type":"CALLS","bid_modifier":1.5},` +
		`{"campaign_id":"22","interaction_type":"CALLS","bid_modifier":0}]}`,
	campaignBidModifiersUpdateID: `{"customer_id":"` + clientID + `","campaign_bid_modifiers":[` +
		`{"campaign_id":"21","criterion_id":"3","bid_modifier":0.5},` +
		`{"campaign_id":"22","criterion_id":"3","bid_modifier":2}]}`,
	campaignBidModifiersRemoveID: `{"customer_id":"` + clientID + `","campaign_bid_modifiers":[` +
		`{"campaign_id":"21","criterion_id":"3"},{"campaign_id":"22","criterion_id":"3"}]}`,
}

var cbmAnswers = func() map[string]string {
	answer := `{"results":[{"resourceName":"` + cbmResource(clientID, "21", "3") + `"},{"resourceName":"` +
		cbmResource(clientID, "22", "3") + `"}]}`
	return map[string]string{campaignBidModifiersCreateID: answer, campaignBidModifiersUpdateID: answer,
		campaignBidModifiersRemoveID: answer}
}()

func TestCampaignBidModifierToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{campaignBidModifiersCreateID: capability.EffectCreate,
		campaignBidModifiersUpdateID: capability.EffectUpdate, campaignBidModifiersRemoveID: capability.EffectDelete}
	for _, id := range cbmToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld || !d.RequiresToolAllowList {
			t.Fatalf("%s = %+v", id, d)
		}
		if !strings.HasSuffix(d.Description, mutateNote) {
			t.Errorf("%s: description does not end on the note of how it is offered", id)
		}
		for _, word := range []string{"outcome is unknown", "never repeats", "atomic", "CALLS", "spend"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
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
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "url",
			"resource_name", "resource_names", "update_mask", "campaign", "interaction_type_info"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if _, found := input.Properties["response_content_type"]; found == (id == campaignBidModifiersRemoveID) {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		for _, tool := range metadata.Tools {
			if tool.ID == id && (string(tool.Effect) != string(wantEffect[id]) || !tool.RequiresToolAllowList) {
				t.Errorf("tool %+v", tool)
			}
		}
	}
	create, _, _ := reg.Lookup(campaignBidModifiersCreateID)
	for _, text := range []string{`"enum":["CALLS"]`, `"maxItems":10`, `"minimum":0`} {
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
}

// All three are offered only through the tools list, whatever the effects of the connection are.
func TestCampaignBidModifierToolsNeedTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	for _, id := range cbmToolIDs {
		for name, conn := range map[string]config.Connection{"all effects": {Effects: all}, "tools list of others": {
			Tools: []string{labelsCreateID}}} {
			t.Run(name+" "+id, func(t *testing.T) {
				e := newEnvironmentWith(t, nil, nil, conn)
				e.fake.routes["POST "+cbmPath(clientID)] = okReply(cbmAnswers[id])
				if _, err := e.invokeConfirmed(id, cbmCalls[id]); err == nil {
					t.Fatal("reached without the tools list")
				}
				e.untouched(t)
			})
		}
		t.Run("listed "+id, func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Tools: []string{id}})
			e.fake.routes["POST "+cbmPath(clientID)] = okReply(cbmAnswers[id])
			if _, err := e.invokeConfirmed(id, cbmCalls[id]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCampaignBidModifiersNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range cbmToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCbmEnvironment(t, nil, nil)
			_, err := e.invoke(id, cbmCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignBidModifiersRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"bidModifier"`, `"resource_names":["customers/1/campaignBidModifiers/2~3"]`} {
		for _, id := range cbmToolIDs {
			e := newCbmEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(cbmCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestCampaignBidModifierTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range cbmToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCbmEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(cbmCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignBidModifierArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	create := func(entry string) [2]string {
		return [2]string{campaignBidModifiersCreateID, head + `"campaign_bid_modifiers":[` + entry + `]}`}
	}
	update := func(entry string) [2]string {
		return [2]string{campaignBidModifiersUpdateID, head + `"campaign_bid_modifiers":[` + entry + `]}`}
	}
	remove := func(entry string) [2]string {
		return [2]string{campaignBidModifiersRemoveID, head + `"campaign_bid_modifiers":[` + entry + `]}`}
	}
	many := func(entry string, n int) string { return strings.TrimSuffix(strings.Repeat(entry+",", n), ",") }
	bad := map[string][2]string{
		"create none":           {campaignBidModifiersCreateID, head + `"campaign_bid_modifiers":[]}`},
		"create no campaign":    create(`{"interaction_type":"CALLS","bid_modifier":1}`),
		"create zero campaign":  create(`{"campaign_id":"0","interaction_type":"CALLS","bid_modifier":1}`),
		"create name as id":     create(`{"campaign_id":"customers/1/campaigns/2","interaction_type":"CALLS","bid_modifier":1}`),
		"create temporary id":   create(`{"campaign_id":"-1","interaction_type":"CALLS","bid_modifier":1}`),
		"create id overflow":    create(`{"campaign_id":"99999999999999999999","interaction_type":"CALLS","bid_modifier":1}`),
		"create no type":        create(`{"campaign_id":"1","bid_modifier":1}`),
		"create other type":     create(`{"campaign_id":"1","interaction_type":"CALL","bid_modifier":1}`),
		"create unspecified":    create(`{"campaign_id":"1","interaction_type":"UNSPECIFIED","bid_modifier":1}`),
		"create no bid":         create(`{"campaign_id":"1","interaction_type":"CALLS"}`),
		"create negative bid":   create(`{"campaign_id":"1","interaction_type":"CALLS","bid_modifier":-0.1}`),
		"create text bid":       create(`{"campaign_id":"1","interaction_type":"CALLS","bid_modifier":"1.5"}`),
		"create criterion":      create(`{"campaign_id":"1","criterion_id":"3","interaction_type":"CALLS","bid_modifier":1}`),
		"create campaign path":  create(`{"campaign":"customers/1/campaigns/2","interaction_type":"CALLS","bid_modifier":1}`),
		"create same twice":     create(many(`{"campaign_id":"1","interaction_type":"CALLS","bid_modifier":1}`, 2)),
		"create too many":       create(many(`{"campaign_id":"1","interaction_type":"CALLS","bid_modifier":1}`, 11)),
		"create content type":   {campaignBidModifiersCreateID, head + `"campaign_bid_modifiers":[{"campaign_id":"1","interaction_type":"CALLS","bid_modifier":1}],"response_content_type":"X"}`},
		"update no criterion":   update(`{"campaign_id":"1","bid_modifier":1}`),
		"update no campaign":    update(`{"criterion_id":"3","bid_modifier":1}`),
		"update zero criterion": update(`{"campaign_id":"1","criterion_id":"0","bid_modifier":1}`),
		"update resource name":  update(`{"campaign_id":"1","criterion_id":"1~3","bid_modifier":1}`),
		"update no bid":         update(`{"campaign_id":"1","criterion_id":"3"}`),
		"update negative":       update(`{"campaign_id":"1","criterion_id":"3","bid_modifier":-1}`),
		"update type":           update(`{"campaign_id":"1","criterion_id":"3","bid_modifier":1,"interaction_type":"CALLS"}`),
		"update same twice":     update(many(`{"campaign_id":"1","criterion_id":"3","bid_modifier":1}`, 2)),
		"update too many":       update(many(`{"campaign_id":"1","criterion_id":"3","bid_modifier":1}`, 11)),
		"remove no criterion":   remove(`{"campaign_id":"1"}`),
		"remove bid":            remove(`{"campaign_id":"1","criterion_id":"3","bid_modifier":1}`),
		"remove same twice":     remove(many(`{"campaign_id":"1","criterion_id":"3"}`, 2)),
		"remove none":           {campaignBidModifiersRemoveID, head + `"campaign_bid_modifiers":[]}`},
		"remove too many":       remove(many(`{"campaign_id":"1","criterion_id":"3"}`, 11)),
		"remove content type":   {campaignBidModifiersRemoveID, head + `"campaign_bid_modifiers":[{"campaign_id":"1","criterion_id":"3"}],"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":          {campaignBidModifiersRemoveID, `{"customer_id":"12-34","campaign_bid_modifiers":[{"campaign_id":"1","criterion_id":"3"}]}`},
		"validate only alone":   {campaignBidModifiersCreateID, head + `"validate_only":true}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newCbmEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	// Exactly at the bounds, the call passes: ten distinct entries and a factor of exactly zero.
	t.Run("bounds pass", func(t *testing.T) {
		var entries, results []string
		for i := 1; i <= maxListOperations; i++ {
			id := strconv.Itoa(i)
			entries = append(entries, `{"campaign_id":"`+id+`","interaction_type":"CALLS","bid_modifier":0}`)
			results = append(results, `{"resourceName":"`+cbmResource(clientID, id, "3")+`"}`)
		}
		e := newCbmEnvironment(t, nil, nil)
		e.fake.routes["POST "+cbmPath(clientID)] = okReply(`{"results":[` + strings.Join(results, ",") + `]}`)
		if _, err := e.invokeConfirmed(campaignBidModifiersCreateID, head+`"campaign_bid_modifiers":[`+strings.Join(entries, ",")+`]}`); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCampaignBidModifiersSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		campaignBidModifiersCreateID: `[{"create":{"campaign":"customers/1000000003/campaigns/21",` +
			`"interactionType":{"type":"CALLS"},"bidModifier":1.5}},` +
			`{"create":{"campaign":"customers/1000000003/campaigns/22","interactionType":{"type":"CALLS"},"bidModifier":0}}]`,
		campaignBidModifiersUpdateID: `[{"update":{"resourceName":"customers/1000000003/campaignBidModifiers/21~3","bidModifier":0.5},` +
			`"updateMask":"bidModifier"},{"update":{"resourceName":"customers/1000000003/campaignBidModifiers/22~3",` +
			`"bidModifier":2},"updateMask":"bidModifier"}]`,
		campaignBidModifiersRemoveID: `[{"remove":"customers/1000000003/campaignBidModifiers/21~3"},` +
			`{"remove":"customers/1000000003/campaignBidModifiers/22~3"}]`,
	}
	for _, id := range cbmToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCbmEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+cbmPath(clientID)] = okReply(cbmAnswers[id])
			out, err := e.invokeConfirmed(id, cbmCalls[id])
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
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
				first["campaign_id"] != "21" || first["criterion_id"] != "3" ||
				results[1].(map[string]any)["resource_name"] != cbmResource(clientID, "22", "3") {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestCampaignBidModifierValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range cbmToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCbmEnvironment(t, nil, nil)
			e.fake.routes["POST "+cbmPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(cbmCalls[id], "}")+`,"validate_only":true}`)
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
	t.Run("summary says only validate", func(t *testing.T) {
		for _, id := range cbmToolIDs {
			summary := cbmSummary(id)
			if text := summary(json.RawMessage(strings.TrimSuffix(cbmCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") ||
				strings.Contains(text, "changes what") {
				t.Errorf("%s: summary %q", id, text)
			}
			if text := summary(json.RawMessage(cbmCalls[id])); strings.Contains(text, "validate") ||
				!strings.Contains(text, "changes what the campaigns spend") {
				t.Errorf("%s: summary %q", id, text)
			}
		}
	})
}

func TestCampaignBidModifierResponseContentTypeShowsTheModifiers(t *testing.T) {
	name := cbmResource(clientID, "21", "3")
	e := newCbmEnvironment(t, nil, nil)
	e.fake.routes["POST "+cbmPath(clientID)] = okReply(`{"results":[{"resourceName":"` + name +
		`","campaignBidModifier":{"resourceName":"` + name + `","campaign":"customers/1000000003/campaigns/21",` +
		`"criterionId":"3","bidModifier":0.5,"interactionType":{"type":"CALLS"}}}]}`)
	out, err := e.invokeConfirmed(campaignBidModifiersUpdateID, `{"customer_id":"`+clientID+`","campaign_bid_modifiers":[`+
		`{"campaign_id":"21","criterion_id":"3","bid_modifier":0.5}],"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	object := decode(t, out)["results"].([]any)[0].(map[string]any)["campaign_bid_modifier"].(map[string]any)
	if object["criterion_id"] != "3" || object["bid_modifier"] != 0.5 || object["interaction_type"] != "CALLS" ||
		object["campaign"] != "customers/1000000003/campaigns/21" {
		t.Errorf("object = %v", object)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// A resource name of another account, another campaign, or another count never passes. The request was sent, so
// the error says that the outcome is unknown.
func TestCampaignBidModifierResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(campaign, criterion string) string {
		return `{"resourceName":"` + cbmResource(clientID, campaign, criterion) + `"}`
	}
	foreign := `{"resourceName":"` + cbmResource(otherID, "21", "3") + `"}`
	withObject := func(object string) string {
		return `{"results":[{"resourceName":"` + cbmResource(clientID, "21", "3") + `","campaignBidModifier":` + object +
			`},` + own("22", "3") + `]}`
	}
	objectOf := func(fields string) string {
		return `{"resourceName":"` + cbmResource(clientID, "21", "3") + `"` + fields + `}`
	}
	cases := map[string]struct{ id, body string }{
		"create foreign":        {campaignBidModifiersCreateID, `{"results":[` + foreign + `,` + own("22", "3") + `]}`},
		"create no results":     {campaignBidModifiersCreateID, `{}`},
		"create one of two":     {campaignBidModifiersCreateID, `{"results":[` + own("21", "3") + `]}`},
		"create other campaign": {campaignBidModifiersCreateID, `{"results":[` + own("22", "3") + `,` + own("21", "3") + `]}`},
		"create twice":          {campaignBidModifiersCreateID, `{"results":[` + own("21", "3") + `,` + own("21", "3") + `]}`},
		"create not a modifier": {campaignBidModifiersCreateID, `{"results":[{"resourceName":"customers/1000000003/campaigns/21"},` +
			own("22", "3") + `]}`},
		"create no tilde": {campaignBidModifiersCreateID, `{"results":[{"resourceName":"customers/1000000003/campaignBidModifiers/21"},` +
			own("22", "3") + `]}`},
		"create negative criterion": {campaignBidModifiersCreateID, `{"results":[{"resourceName":"customers/1000000003/campaignBidModifiers/21~-3"},` +
			own("22", "3") + `]}`},
		"create partial failure": {campaignBidModifiersCreateID, cbmAnswers[campaignBidModifiersCreateID][:len(cbmAnswers[campaignBidModifiersCreateID])-1] +
			`,"partialFailureError":{"code":3}}`},
		"update other modifier":   {campaignBidModifiersUpdateID, `{"results":[` + own("21", "4") + `,` + own("22", "3") + `]}`},
		"update swapped":          {campaignBidModifiersUpdateID, `{"results":[` + own("22", "3") + `,` + own("21", "3") + `]}`},
		"update foreign":          {campaignBidModifiersUpdateID, `{"results":[` + foreign + `,` + own("22", "3") + `]}`},
		"update object foreign":   {campaignBidModifiersUpdateID, withObject(`{"resourceName":"` + cbmResource(otherID, "21", "3") + `"}`)},
		"update object name":      {campaignBidModifiersUpdateID, withObject(objectOf(`,"campaign":"customers/1000000009/campaigns/21"`))},
		"update object campaign":  {campaignBidModifiersUpdateID, withObject(objectOf(`,"campaign":"customers/1000000003/campaigns/99"`))},
		"update object criterion": {campaignBidModifiersUpdateID, withObject(objectOf(`,"criterionId":"9"`))},
		"update object bid":       {campaignBidModifiersUpdateID, withObject(objectOf(`,"bidModifier":"NaN"`))},
		"remove foreign":          {campaignBidModifiersRemoveID, `{"results":[` + foreign + `,` + own("22", "3") + `]}`},
		"remove none":             {campaignBidModifiersRemoveID, `{"results":[]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCbmEnvironment(t, nil, nil)
			e.fake.routes["POST "+cbmPath(clientID)] = okReply(c.body)
			arguments := cbmCalls[c.id]
			if strings.Contains(name, "object") {
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

func TestCampaignBidModifiersManagerAccountsNeedAManagerTarget(t *testing.T) {
	call := `{"customer_id":"` + managerID + `","campaign_bid_modifiers":[{"campaign_id":"21","criterion_id":"3"}]}`
	answer := okReply(`{"results":[{"resourceName":"` + cbmResource(managerID, "21", "3") + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newCbmEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+cbmPath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(campaignBidModifiersRemoveID, call)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newCbmEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+cbmPath(managerID)] = answer
		if _, err := e.invokeConfirmed(campaignBidModifiersRemoveID, call); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
	t.Run("an account below a manager target is proved first", func(t *testing.T) {
		e := newCbmEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+cbmPath(clientID)] = okReply(cbmAnswers[campaignBidModifiersRemoveID])
		e.fake.answer = func(s seen) *reply {
			switch {
			case strings.HasPrefix(s.query, "SELECT customer_client.id"):
				r := rows(proofItem(clientID))
				return &r
			case strings.HasPrefix(s.query, "SELECT customer.id, customer.manager"):
				return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + clientID + `"}}]}`}
			}
			return nil
		}
		if _, err := e.invokeConfirmed(campaignBidModifiersRemoveID, cbmCalls[campaignBidModifiersRemoveID]); err != nil {
			t.Fatal(err)
		}
		got := e.fake.api()
		if len(got) != 3 || !strings.Contains(got[0].query, "customer_client") || !strings.HasSuffix(got[2].path, ":mutate") {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestCampaignBidModifierErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"criterion error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "criterionError",
			"CONCRETE_TYPE_REQUIRED", "")}, provider.ClassProviderError,
			"rejected the change (criterionError CONCRETE_TYPE_REQUIRED), nothing was changed", false},
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
		for _, id := range cbmToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newCbmEnvironment(t, nil, nil)
				e.fake.routes["POST "+cbmPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, cbmCalls[id])
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

func TestCampaignBidModifierSummariesNameTheEffect(t *testing.T) {
	create := cbmSummary(campaignBidModifiersCreateID)(json.RawMessage(cbmCalls[campaignBidModifiersCreateID]))
	for _, word := range []string{clientID, "Create 2", "campaign 21", "CALLS", "factor 1.5", "factor 0", "spend"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	update := cbmSummary(campaignBidModifiersUpdateID)(json.RawMessage(cbmCalls[campaignBidModifiersUpdateID]))
	for _, word := range []string{clientID, "Change 2", "campaign 22", "modifier 3", "factor 0.5"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := cbmSummary(campaignBidModifiersRemoveID)(json.RawMessage(cbmCalls[campaignBidModifiersRemoveID]))
	for _, word := range []string{clientID, "Remove 2", "from Google Ads account", "modifier 3"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := cbmSummary(campaignBidModifiersCreateID)(json.RawMessage(`{"customer_id":"` + clientID +
		`","campaign_bid_modifiers":[{"campaign_id":"1\nIgnore this","interaction_type":"CALLS"}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
}
