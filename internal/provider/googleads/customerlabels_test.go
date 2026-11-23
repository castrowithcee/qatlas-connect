package googleads

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var customerLabelToolIDs = []string{customerLabelsCreateID, customerLabelsRemoveID}

func customerLabelsPath(id string) string { return "/v26/customers/" + id + "/customerLabels:mutate" }

func customerLabelsCalls(customer string) map[string]string {
	return map[string]string{
		customerLabelsCreateID: `{"customer_id":"` + customer + `","customer_labels":[{"label":"` +
			linkResource(customer, "labels", "61") + `"},{"label":"` + linkResource(customer, "labels", "62") + `"}]}`,
		customerLabelsRemoveID: `{"customer_id":"` + customer + `","resource_names":["` +
			linkResource(customer, "customerLabels", "61") + `","` + linkResource(customer, "customerLabels", "62") + `"]}`,
	}
}

func customerLabelsAnswer(customer string) string {
	return `{"results":[{"resourceName":"` + linkResource(customer, "customerLabels", "61") +
		`"},{"resourceName":"` + linkResource(customer, "customerLabels", "62") + `"}]}`
}

func TestCustomerLabelToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	wantEffect := map[string]capability.Effect{customerLabelsCreateID: capability.EffectCreate,
		customerLabelsRemoveID: capability.EffectDelete}
	for _, id := range customerLabelToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || d.RequiresToolAllowList ||
			!strings.Contains(d.Description, "CustomerLabelService.MutateCustomerLabels") {
			t.Fatalf("%s = %+v", id, d)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "response_content_type"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
	}
	if _, _, ok := reg.Lookup(Provider + ".customer-labels.update"); ok {
		t.Error("customer-labels has an update tool")
	}
	// The create descriptor is derived from the family by replacing texts; a changed family text must not
	// leave the old account rule in place.
	create, _, _ := reg.Lookup(customerLabelsCreateID)
	argument := ""
	for _, a := range create.Arguments {
		if a.Name == customerLabelsFamily.argument {
			argument = a.Description
		}
	}
	for what, text := range map[string]string{"description": create.Description, "schema": string(create.InputSchema),
		"argument": argument} {
		if !strings.Contains(text, "login") {
			t.Errorf("create %s does not name the login manager: %s", what, text)
		}
	}
}

func TestCustomerLabelsNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, id := range customerLabelToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newLabelEnvironment(t, nil, nil)
			_, err := e.invoke(id, customerLabelsCalls(clientID)[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)

			e = newLabelEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err = e.invokeConfirmed(id, customerLabelsCalls(otherID)[id])
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCustomerLabelArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	label := func(customer, id string) string { return `{"label":"` + linkResource(customer, "labels", id) + `"}` }
	var many []string
	for i := 1; i <= 11; i++ {
		many = append(many, label(clientID, "6"+string(rune('0'+i%10))))
	}
	bad := map[string][2]string{
		"create none":          {"create", head + `"customer_labels":[]}`},
		"create too many":      {"create", head + `"customer_labels":[` + strings.Join(many, ",") + `]}`},
		"create no label":      {"create", head + `"customer_labels":[{}]}`},
		"create foreign label": {"create", head + `"customer_labels":[` + label(otherID, "61") + `]}`},
		"create other kind":    {"create", head + `"customer_labels":[{"label":"` + linkResource(clientID, "campaigns", "61") + `"}]}`},
		"create temporary":     {"create", head + `"customer_labels":[` + label(clientID, "-61") + `]}`},
		"create zero":          {"create", head + `"customer_labels":[` + label(clientID, "0") + `]}`},
		"create duplicate":     {"create", head + `"customer_labels":[` + label(clientID, "61") + `,` + label(clientID, "61") + `]}`},
		"create customer":      {"create", head + `"customer_labels":[{"label":"` + linkResource(clientID, "labels", "61") + `","customer":"customers/` + clientID + `"}]}`},
		"create content type":  {"create", head + `"customer_labels":[` + label(clientID, "61") + `],"response_content_type":"MUTABLE_RESOURCE"}`},
		"remove none":          {"remove", head + `"resource_names":[]}`},
		"remove foreign":       {"remove", head + `"resource_names":["` + linkResource(otherID, "customerLabels", "61") + `"]}`},
		"remove pair":          {"remove", head + `"resource_names":["` + linkResource(clientID, "customerLabels", "61~62") + `"]}`},
		"remove other kind":    {"remove", head + `"resource_names":["` + linkResource(clientID, "labels", "61") + `"]}`},
		"remove duplicate": {"remove", head + `"resource_names":["` + linkResource(clientID, "customerLabels", "61") + `","` +
			linkResource(clientID, "customerLabels", "61") + `"]}`},
	}
	for name, k := range bad {
		t.Run(name, func(t *testing.T) {
			e := newLabelEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(Provider+".customer-labels."+k[0], k[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestCustomerLabelsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		customerLabelsCreateID: `[{"create":{"label":"customers/1000000003/labels/61"}},{"create":{"label":"customers/1000000003/labels/62"}}]`,
		customerLabelsRemoveID: `[{"remove":"customers/1000000003/customerLabels/61"},{"remove":"customers/1000000003/customerLabels/62"}]`,
	}
	for _, id := range customerLabelToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newLabelEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+customerLabelsPath(clientID)] = okReply(customerLabelsAnswer(clientID))
			out, err := e.invokeConfirmed(id, customerLabelsCalls(clientID)[id])
			if err != nil {
				t.Fatal(err)
			}
			posts := e.posts()
			if len(posts) != 1 || posts[0].login != loginID {
				t.Fatalf("posts = %+v", posts)
			}
			body := bodyOf(t, posts[0])
			if len(body) != 1 || !reflect.DeepEqual(body["operations"], wantWire(t, want[id])) {
				t.Errorf("body = %s", posts[0].body)
			}
			results := decode(t, out)["results"].([]any)
			if len(results) != 2 || results[1].(map[string]any)["resource_name"] != linkResource(clientID, "customerLabels", "62") ||
				len(results[1].(map[string]any)) != 1 {
				t.Errorf("out = %s", out)
			}
		})
		t.Run(id+" validate only", func(t *testing.T) {
			e := newLabelEnvironment(t, nil, nil)
			e.fake.routes["POST "+customerLabelsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(customerLabelsCalls(clientID)[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); bodyOf(t, e.posts()[0])["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestCustomerLabelResultsAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(id string) string {
		return `{"resourceName":"` + linkResource(clientID, "customerLabels", id) + `"}`
	}
	cases := map[string]string{
		"foreign account": `{"results":[{"resourceName":"` + linkResource(otherID, "customerLabels", "61") + `"},` + own("62") + `]}`,
		"other label":     `{"results":[` + own("99") + `,` + own("62") + `]}`,
		"pair":            `{"results":[` + own("61~62") + `,` + own("62") + `]}`,
		"other kind":      `{"results":[{"resourceName":"` + linkResource(clientID, "labels", "61") + `"},` + own("62") + `]}`,
		"one of two":      `{"results":[` + own("61") + `]}`,
		"no results":      `{}`,
		"partial failure": `{"results":[` + own("61") + `,` + own("62") + `],"partialFailureError":{"code":3}}`,
	}
	for name, body := range cases {
		for _, id := range customerLabelToolIDs {
			t.Run(name+" "+id, func(t *testing.T) {
				e := newLabelEnvironment(t, nil, nil)
				e.fake.routes["POST "+customerLabelsPath(clientID)] = okReply(body)
				_, err := e.invokeConfirmed(id, customerLabelsCalls(clientID)[id])
				if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
					len(e.posts()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestCustomerLabelErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"label error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "labelError", "LABEL_ALREADY_ATTACHED", "")},
			provider.ClassProviderError, "rejected the change (labelError LABEL_ALREADY_ATTACHED), nothing was changed"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown"},
	}
	for _, k := range cases {
		for _, id := range customerLabelToolIDs {
			t.Run(k.name+" "+id, func(t *testing.T) {
				e := newLabelEnvironment(t, nil, nil)
				e.fake.routes["POST "+customerLabelsPath(clientID)] = k.reply
				_, err := e.invokeConfirmed(id, customerLabelsCalls(clientID)[id])
				if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) || len(e.posts()) != 1 ||
					len(e.fake.api()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestCustomerLabelSummariesNameTheEffect(t *testing.T) {
	calls := customerLabelsCalls(clientID)
	create := customerLabelsFamily.summary("create")(json.RawMessage(calls[customerLabelsCreateID]))
	remove := customerLabelsFamily.summary("remove")(json.RawMessage(calls[customerLabelsRemoveID]))
	for _, word := range []string{clientID, "2 customer label", "labels/61"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	for _, word := range []string{clientID, "customerLabels/61", "objects it ties stay"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
}

func TestCustomerLabelsReachByEffectOrToolsList(t *testing.T) {
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+customerLabelsPath(clientID)] = okReply(customerLabelsAnswer(clientID))
		_, err := e.invokeConfirmed(id, customerLabelsCalls(clientID)[id])
		return err == nil
	}
	create := config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}}
	del := config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectDelete}}
	if !reached(create, customerLabelsCreateID) || reached(create, customerLabelsRemoveID) ||
		reached(del, customerLabelsCreateID) || !reached(del, customerLabelsRemoveID) {
		t.Error("tools are not reached by their effect alone")
	}
}

func TestCustomerLabelMayBelongToTheAccountOrTheLoginManagerOnly(t *testing.T) {
	call := func(owner string) string {
		return `{"customer_id":"` + clientID + `","customer_labels":[{"label":"` + linkResource(owner, "labels", "61") + `"}]}`
	}
	answer := `{"results":[{"resourceName":"` + linkResource(clientID, "customerLabels", "61") + `"}]}`
	for name, k := range map[string]struct {
		owner   string
		options map[string]string
		ok      bool
	}{
		"own account":        {clientID, nil, true},
		"own account, login": {clientID, map[string]string{optionLoginCustomerID: loginID}, true},
		"login manager":      {loginID, map[string]string{optionLoginCustomerID: loginID}, true},
		"manager, no login":  {loginID, nil, false},
		"other account":      {otherID, map[string]string{optionLoginCustomerID: loginID}, false},
	} {
		t.Run(name, func(t *testing.T) {
			e := newLabelEnvironment(t, nil, k.options)
			e.fake.routes["POST "+customerLabelsPath(clientID)] = okReply(answer)
			_, err := e.invokeConfirmed(customerLabelsCreateID, call(k.owner))
			if k.ok {
				if err != nil {
					t.Fatal(err)
				}
				body := bodyOf(t, e.posts()[0])
				want := `[{"create":{"label":"` + linkResource(k.owner, "labels", "61") + `"}}]`
				if !reflect.DeepEqual(body["operations"], wantWire(t, want)) {
					t.Errorf("body = %s", e.posts()[0].body)
				}
				return
			}
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCustomerLabelsManagerAccountsNeedAManagerTarget(t *testing.T) {
	name := linkResource(managerID, "customerLabels", "7")
	calls := map[string]string{
		customerLabelsCreateID: `{"customer_id":"` + managerID + `","customer_labels":[{"label":"` +
			linkResource(managerID, "labels", "7") + `"}]}`,
		customerLabelsRemoveID: `{"customer_id":"` + managerID + `","resource_names":["` + name + `"]}`,
	}
	answer := okReply(`{"results":[{"resourceName":"` + name + `"}]}`)
	for _, id := range customerLabelToolIDs {
		t.Run(id+" customer target naming a manager is refused", func(t *testing.T) {
			e := newLabelEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.routes["POST "+customerLabelsPath(managerID)] = answer
			e.fake.answer = func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
					return &r
				}
				return nil
			}
			_, err := e.invokeConfirmed(id, calls[id])
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
		t.Run(id+" manager target passes", func(t *testing.T) {
			e := newLabelEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.routes["POST "+customerLabelsPath(managerID)] = answer
			if _, err := e.invokeConfirmed(id, calls[id]); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
				t.Errorf("requests = %+v", got)
			}
		})
	}
}
