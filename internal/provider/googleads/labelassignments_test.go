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

var labelAssignmentToolIDs = []string{campaignLabelsCreateID, campaignLabelsRemoveID, adGroupLabelsCreateID,
	adGroupLabelsRemoveID, adGroupAdLabelsCreateID, adGroupAdLabelsRemoveID, adGroupCriterionLabelsCreateID,
	adGroupCriterionLabelsRemoveID, customerLabelsCreateID, customerLabelsRemoveID}

// labelCase is one label assignment family with the calls and answers both of its tools use.
type labelCase struct {
	family   setLinkFamily
	object   string // collection of the object that gets the label
	wireName string // member of the create body that names the object
}

func labelCases() []labelCase {
	return []labelCase{
		{family: campaignLabelsFamily, object: "campaigns", wireName: "campaign"},
		{family: adGroupLabelsFamily, object: "adGroups", wireName: "adGroup"},
	}
}

func (c labelCase) path(customer string) string {
	return "/v26/customers/" + customer + c.family.service + ":mutate"
}

func (c labelCase) entry(customer, objectID, labelID string) string {
	return `{"` + c.family.parts[0].name + `":"` + linkResource(customer, c.object, objectID) + `","label":"` +
		linkResource(customer, "labels", labelID) + `"}`
}

func (c labelCase) name(customer, key string) string {
	return linkResource(customer, c.family.collection, key)
}

func (c labelCase) create() string {
	return `{"customer_id":"` + clientID + `","` + c.family.argument + `":[` + c.entry(clientID, "21", "61") + `,` +
		c.entry(clientID, "22", "61") + `]}`
}

func (c labelCase) remove() string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + c.name(clientID, "21~61") + `","` +
		c.name(clientID, "22~61") + `"]}`
}

func (c labelCase) call(verb string) string {
	if verb == "create" {
		return c.create()
	}
	return c.remove()
}

func (c labelCase) answer() string {
	return `{"results":[{"resourceName":"` + c.name(clientID, "21~61") + `"},{"resourceName":"` +
		c.name(clientID, "22~61") + `"}]}`
}

func newLabelEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: labelAssignmentToolIDs})
}

func TestLabelAssignmentToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{"create": capability.EffectCreate, "remove": capability.EffectDelete}
	for _, c := range labelCases() {
		for _, verb := range []string{"create", "remove"} {
			id := c.family.id(verb)
			d, _, ok := reg.Lookup(id)
			if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[verb] || len(d.OutputSchema) == 0 ||
				d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld || d.RequiresToolAllowList {
				t.Fatalf("%s = %+v", id, d)
			}
			if slices.Contains(metadata.Profiles[0].Tools, id) {
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
			for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "response_content_type",
				"status"} {
				if _, found := input.Properties[forbidden]; found {
					t.Errorf("%s offers the argument %s", id, forbidden)
				}
			}
			if _, found := input.Properties["validate_only"]; !found {
				t.Errorf("%s offers no validate_only", id)
			}
			if strings.Contains(string(d.OutputSchema), c.family.resultKey) || !strings.Contains(d.Description, c.family.rpc) ||
				!strings.Contains(d.Description, "outcome is unknown") {
				t.Errorf("%s: description or output schema", id)
			}
		}
		if _, _, ok := reg.Lookup(Provider + "." + c.family.tool + ".update"); ok {
			t.Errorf("%s has an update tool", c.family.tool)
		}
	}
}

func TestLabelAssignmentsAreReachedByTheMatchingEffectOrTheToolsList(t *testing.T) {
	for _, c := range labelCases() {
		reached := func(conn config.Connection, verb string) bool {
			e := newEnvironmentWith(t, nil, nil, conn)
			e.fake.routes["POST "+c.path(clientID)] = okReply(c.answer())
			_, err := e.invokeConfirmed(c.family.id(verb), c.call(verb))
			return err == nil
		}
		cases := []struct {
			name         string
			conn         config.Connection
			create, remv bool
		}{
			{"create effect", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}}, true, false},
			{"delete effect", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectDelete}}, false, true},
			{"read effect", config.Connection{Effects: []config.Effect{config.EffectRead}}, false, false},
			{"remove listed", config.Connection{Tools: []string{c.family.id("remove")}}, false, true},
		}
		for _, k := range cases {
			if got := reached(k.conn, "create"); got != k.create {
				t.Errorf("%s %s: create reached = %v", c.family.tool, k.name, got)
			}
			if got := reached(k.conn, "remove"); got != k.remv {
				t.Errorf("%s %s: remove reached = %v", c.family.tool, k.name, got)
			}
		}
	}
}

func TestLabelAssignmentsNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, c := range labelCases() {
		for _, verb := range []string{"create", "remove"} {
			t.Run(c.family.id(verb), func(t *testing.T) {
				e := newLabelEnvironment(t, nil, nil)
				_, err := e.invoke(c.family.id(verb), c.call(verb))
				var needed *application.ConfirmationRequiredError
				if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
					t.Fatalf("err = %v, want a confirmation that names the account", err)
				}
				e.untouched(t)

				e = newLabelEnvironment(t, []string{"customers/" + clientID}, nil)
				arguments := strings.ReplaceAll(c.call(verb), clientID, otherID)
				_, err = e.invokeConfirmed(c.family.id(verb), arguments)
				if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
					t.Fatalf("err = %v, want invalid-request without the ID", err)
				}
				e.untouched(t)
			})
		}
	}
}

func TestLabelAssignmentArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, c := range labelCases() {
		head := `{"customer_id":"` + clientID + `",`
		list := func(items ...string) string {
			return head + `"` + c.family.argument + `":[` + strings.Join(items, ",") + `]}`
		}
		names := func(items ...string) string { return head + `"resource_names":[` + strings.Join(items, ",") + `]}` }
		object := c.family.parts[0].name
		var many []string
		for i := 1; i <= 11; i++ {
			many = append(many, c.entry(clientID, strconv.Itoa(i), "61"))
		}
		bad := map[string][2]string{
			"create none":          {"create", list()},
			"create too many":      {"create", list(many...)},
			"create no label":      {"create", list(`{"` + object + `":"` + linkResource(clientID, c.object, "21") + `"}`)},
			"create foreign label": {"create", list(`{"` + object + `":"` + linkResource(clientID, c.object, "21") + `","label":"` + linkResource(otherID, "labels", "61") + `"}`)},
			"create foreign owner": {"create", list(c.entry(otherID, "21", "61"))},
			"create other kind":    {"create", list(`{"` + object + `":"` + linkResource(clientID, "labels", "21") + `","label":"` + linkResource(clientID, "labels", "61") + `"}`)},
			"create temporary":     {"create", list(c.entry(clientID, "21", "-61"))},
			"create zero":          {"create", list(c.entry(clientID, "0", "61"))},
			"create duplicate":     {"create", list(c.entry(clientID, "21", "61"), c.entry(clientID, "21", "61"))},
			"create partial":       {"create", strings.TrimSuffix(c.create(), "}") + `,"partial_failure":true}`},
			"create content type":  {"create", strings.TrimSuffix(c.create(), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
			"remove none":          {"remove", names()},
			"remove foreign":       {"remove", names(`"` + c.name(otherID, "21~61") + `"`)},
			"remove one id":        {"remove", names(`"` + c.name(clientID, "21") + `"`)},
			"remove three ids":     {"remove", names(`"` + c.name(clientID, "21~61~7") + `"`)},
			"remove other kind":    {"remove", names(`"` + linkResource(clientID, "labels", "21~61") + `"`)},
			"remove duplicate":     {"remove", names(`"`+c.name(clientID, "21~61")+`"`, `"`+c.name(clientID, "21~61")+`"`)},
			"remove content type":  {"remove", strings.TrimSuffix(c.remove(), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
		}
		for name, k := range bad {
			t.Run(c.family.tool+" "+name, func(t *testing.T) {
				e := newLabelEnvironment(t, nil, nil)
				if _, err := e.invokeConfirmed(c.family.id(k[0]), k[1]); err == nil {
					t.Fatal("accepted")
				}
				e.untouched(t)
			})
		}
	}
}

func TestLabelAssignmentsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	for _, c := range labelCases() {
		want := map[string]string{
			"create": `[{"create":{"` + c.wireName + `":"` + linkResource(clientID, c.object, "21") + `","label":"` +
				linkResource(clientID, "labels", "61") + `"}},{"create":{"` + c.wireName + `":"` +
				linkResource(clientID, c.object, "22") + `","label":"` + linkResource(clientID, "labels", "61") + `"}}]`,
			"remove": `[{"remove":"` + c.name(clientID, "21~61") + `"},{"remove":"` + c.name(clientID, "22~61") + `"}]`,
		}
		for _, verb := range []string{"create", "remove"} {
			t.Run(c.family.id(verb), func(t *testing.T) {
				e := newLabelEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
				e.fake.routes["POST "+c.path(clientID)] = okReply(c.answer())
				out, err := e.invokeConfirmed(c.family.id(verb), c.call(verb))
				if err != nil {
					t.Fatal(err)
				}
				posts := e.posts()
				if len(posts) != 1 || posts[0].login != loginID {
					t.Fatalf("posts = %+v", posts)
				}
				body := bodyOf(t, posts[0])
				if len(body) != 1 || !reflect.DeepEqual(body["operations"], wantWire(t, want[verb])) {
					t.Errorf("body = %s", posts[0].body)
				}
				got := decode(t, out)
				results := got["results"].([]any)
				if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
					results[1].(map[string]any)["resource_name"] != c.name(clientID, "22~61") ||
					len(results[1].(map[string]any)) != 1 {
					t.Errorf("out = %s", out)
				}
			})
			t.Run(c.family.id(verb)+" validate only", func(t *testing.T) {
				e := newLabelEnvironment(t, nil, nil)
				e.fake.routes["POST "+c.path(clientID)] = okReply(`{}`)
				out, err := e.invokeConfirmed(c.family.id(verb), strings.TrimSuffix(c.call(verb), "}")+`,"validate_only":true}`)
				if err != nil {
					t.Fatal(err)
				}
				got := decode(t, out)
				if bodyOf(t, e.posts()[0])["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
					t.Errorf("out = %s", out)
				}
			})
		}
	}
}

// A resource name of another account, another label, another object kind or another count never passes. The
// request was sent, so the error says that the outcome is unknown.
func TestLabelAssignmentResultsAreBoundToTheCheckedAccount(t *testing.T) {
	for _, c := range labelCases() {
		own := func(name string) string { return `{"resourceName":"` + name + `"}` }
		first, second := c.name(clientID, "21~61"), c.name(clientID, "22~61")
		cases := map[string]string{
			"foreign account": `{"results":[` + own(c.name(otherID, "21~61")) + `,` + own(second) + `]}`,
			"other label":     `{"results":[` + own(c.name(clientID, "21~99")) + `,` + own(second) + `]}`,
			"other order":     `{"results":[` + own(second) + `,` + own(first) + `]}`,
			"one id":          `{"results":[` + own(c.name(clientID, "21")) + `,` + own(second) + `]}`,
			"other kind":      `{"results":[` + own(linkResource(clientID, "labels", "21~61")) + `,` + own(second) + `]}`,
			"no name":         `{"results":[{},` + own(second) + `]}`,
			"one of two":      `{"results":[` + own(first) + `]}`,
			"no results":      `{}`,
			"partial failure": `{"results":[` + own(first) + `,` + own(second) + `],"partialFailureError":{"code":3}}`,
		}
		for name, body := range cases {
			for _, verb := range []string{"create", "remove"} {
				t.Run(c.family.tool+" "+verb+" "+name, func(t *testing.T) {
					e := newLabelEnvironment(t, nil, nil)
					e.fake.routes["POST "+c.path(clientID)] = okReply(body)
					_, err := e.invokeConfirmed(c.family.id(verb), c.call(verb))
					if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
						len(e.posts()) != 1 {
						t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
					}
				})
			}
		}
	}
}

func TestLabelAssignmentsManagerAccountsNeedAManagerTarget(t *testing.T) {
	for _, c := range labelCases() {
		name := linkResource(managerID, c.family.collection, "7~8")
		call := `{"customer_id":"` + managerID + `","resource_names":["` + name + `"]}`
		answer := okReply(`{"results":[{"resourceName":"` + name + `"}]}`)
		t.Run(c.family.tool+" customer target naming a manager is refused", func(t *testing.T) {
			e := newLabelEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.routes["POST "+c.path(managerID)] = answer
			e.fake.answer = func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
					return &r
				}
				return nil
			}
			_, err := e.invokeConfirmed(c.family.id("remove"), call)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
		t.Run(c.family.tool+" manager target passes", func(t *testing.T) {
			e := newLabelEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.routes["POST "+c.path(managerID)] = answer
			if _, err := e.invokeConfirmed(c.family.id("remove"), call); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
				t.Errorf("requests = %+v", got)
			}
		})
	}
}

func TestLabelAssignmentErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"label error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "labelError", "LABEL_ALREADY_ATTACHED", "")},
			provider.ClassProviderError, "rejected the change (labelError LABEL_ALREADY_ATTACHED), nothing was changed", false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
	}
	for _, k := range cases {
		for _, c := range labelCases() {
			for _, verb := range []string{"create", "remove"} {
				t.Run(k.name+" "+c.family.id(verb), func(t *testing.T) {
					e := newLabelEnvironment(t, nil, nil)
					e.fake.routes["POST "+c.path(clientID)] = k.reply
					_, err := e.invokeConfirmed(c.family.id(verb), c.call(verb))
					if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) {
						t.Fatalf("err = %v, want %s with %q", err, k.class, k.message)
					}
					if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != k.unknown {
						t.Errorf("unknown outcome reported = %v, want %v: %v", got, k.unknown, err)
					}
					if len(e.posts()) != 1 || len(e.fake.api()) != 1 {
						t.Errorf("mutate requests = %d of %d, want one and no repeat", len(e.posts()), len(e.fake.api()))
					}
				})
			}
		}
	}
}

func TestLabelAssignmentSummariesNameTheEffect(t *testing.T) {
	for _, c := range labelCases() {
		create := c.family.summary("create")(json.RawMessage(c.create()))
		for _, word := range []string{clientID, "2 " + c.family.noun, "labels/61"} {
			if !strings.Contains(create, word) {
				t.Errorf("summary %q lacks %q", create, word)
			}
		}
		remove := c.family.summary("remove")(json.RawMessage(c.remove()))
		for _, word := range []string{clientID, c.name(clientID, "21~61"), "objects it ties stay"} {
			if !strings.Contains(remove, word) {
				t.Errorf("summary %q lacks %q", remove, word)
			}
		}
	}
}
