package googleads

import (
	"context"
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

// compositeCase is a label family whose object has two IDs, so its link key has three.
type compositeCase struct {
	family   setLinkFamily
	object   string // collection of the object that gets the label
	wireName string // member of the create body that names the object
}

func compositeCases() []compositeCase {
	return []compositeCase{
		{adGroupAdLabelsFamily, "adGroupAds", "adGroupAd"},
		{adGroupCriterionLabelsFamily, "adGroupCriteria", "adGroupCriterion"},
	}
}

func (c compositeCase) entry(customer, object, label string) string {
	return `{"` + c.family.parts[0].name + `":"` + linkResource(customer, c.object, object) + `","label":"` +
		linkResource(customer, "labels", label) + `"}`
}

func (c compositeCase) name(customer, key string) string {
	return linkResource(customer, c.family.collection, key)
}

func (c compositeCase) create() string {
	return `{"customer_id":"` + clientID + `","` + c.family.argument + `":[` + c.entry(clientID, "51~71", "61") + `,` +
		c.entry(clientID, "51~72", "61") + `]}`
}

func (c compositeCase) remove() string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + c.name(clientID, "51~71~61") + `","` +
		c.name(clientID, "51~72~61") + `"]}`
}

func (c compositeCase) path() string {
	return "/v26/customers/" + clientID + c.family.service + ":mutate"
}

func (c compositeCase) answer() string {
	return `{"results":[{"resourceName":"` + c.name(clientID, "51~71~61") + `"},{"resourceName":"` +
		c.name(clientID, "51~72~61") + `"}]}`
}

func TestCompositeLabelAssignmentsSendTheCheckedOperations(t *testing.T) {
	for _, c := range compositeCases() {
		e := newLabelEnvironment(t, nil, nil)
		e.fake.routes["POST "+c.path()] = okReply(c.answer())
		out, err := e.invokeConfirmed(c.family.id("create"), c.create())
		if err != nil {
			t.Fatal(err)
		}
		want := `[{"create":{"` + c.wireName + `":"` + linkResource(clientID, c.object, "51~71") + `","label":"` +
			linkResource(clientID, "labels", "61") + `"}},{"create":{"` + c.wireName + `":"` +
			linkResource(clientID, c.object, "51~72") + `","label":"` + linkResource(clientID, "labels", "61") + `"}}]`
		posts := e.posts()
		if len(posts) != 1 || !reflect.DeepEqual(bodyOf(t, posts[0])["operations"], wantWire(t, want)) {
			t.Errorf("%s: posts = %+v", c.family.tool, posts)
		}
		if results := decode(t, out)["results"].([]any); len(results) != 2 ||
			results[1].(map[string]any)["resource_name"] != c.name(clientID, "51~72~61") {
			t.Errorf("%s: out = %s", c.family.tool, out)
		}

		e = newLabelEnvironment(t, nil, nil)
		e.fake.routes["POST "+c.path()] = okReply(c.answer())
		if _, err := e.invokeConfirmed(c.family.id("remove"), c.remove()); err != nil {
			t.Fatal(err)
		}
		want = `[{"remove":"` + c.name(clientID, "51~71~61") + `"},{"remove":"` + c.name(clientID, "51~72~61") + `"}]`
		if !reflect.DeepEqual(bodyOf(t, e.posts()[0])["operations"], wantWire(t, want)) {
			t.Errorf("%s remove: body = %s", c.family.tool, e.posts()[0].body)
		}
	}
}

func TestCompositeLabelAssignmentArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, c := range compositeCases() {
		head := `{"customer_id":"` + clientID + `",`
		list := func(items ...string) string {
			return head + `"` + c.family.argument + `":[` + strings.Join(items, ",") + `]}`
		}
		names := func(items ...string) string { return head + `"resource_names":[` + strings.Join(items, ",") + `]}` }
		label := linkResource(clientID, "labels", "61")
		object := c.family.parts[0].name
		bad := map[string][2]string{
			"create one id":          {"create", list(c.entry(clientID, "51", "61"))},
			"create three ids":       {"create", list(c.entry(clientID, "51~71~9", "61"))},
			"create zero":            {"create", list(c.entry(clientID, "51~0", "61"))},
			"create temporary":       {"create", list(c.entry(clientID, "51~-71", "61"))},
			"create foreign object":  {"create", list(c.entry(otherID, "51~71", "61"))},
			"create foreign label":   {"create", list(`{"` + object + `":"` + linkResource(clientID, c.object, "51~71") + `","label":"` + linkResource(otherID, "labels", "61") + `"}`)},
			"create other kind":      {"create", list(`{"` + object + `":"` + linkResource(clientID, "adGroups", "51~71") + `","label":"` + label + `"}`)},
			"create two-id label":    {"create", list(`{"` + object + `":"` + linkResource(clientID, c.object, "51~71") + `","label":"` + linkResource(clientID, "labels", "61~2") + `"}`)},
			"create duplicate":       {"create", list(c.entry(clientID, "51~71", "61"), c.entry(clientID, "51~71", "61"))},
			"remove two ids":         {"remove", names(`"` + c.name(clientID, "51~71") + `"`)},
			"remove four ids":        {"remove", names(`"` + c.name(clientID, "51~71~61~2") + `"`)},
			"remove foreign":         {"remove", names(`"` + c.name(otherID, "51~71~61") + `"`)},
			"remove other kind":      {"remove", names(`"` + linkResource(clientID, "adGroupLabels", "51~71~61") + `"`)},
			"remove duplicate":       {"remove", names(`"`+c.name(clientID, "51~71~61")+`"`, `"`+c.name(clientID, "51~71~61")+`"`)},
			"remove temporary":       {"remove", names(`"` + c.name(clientID, "51~-71~61") + `"`)},
			"create response format": {"create", strings.TrimSuffix(c.create(), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
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

func TestCompositeLabelAssignmentResultsAreBoundToTheCheckedLinks(t *testing.T) {
	for _, c := range compositeCases() {
		own := func(name string) string { return `{"resourceName":"` + name + `"}` }
		first, second := c.name(clientID, "51~71~61"), c.name(clientID, "51~72~61")
		cases := map[string]string{
			"foreign account": `{"results":[` + own(c.name(otherID, "51~71~61")) + `,` + own(second) + `]}`,
			"other ad":        `{"results":[` + own(c.name(clientID, "51~79~61")) + `,` + own(second) + `]}`,
			"other label":     `{"results":[` + own(c.name(clientID, "51~71~99")) + `,` + own(second) + `]}`,
			"two ids":         `{"results":[` + own(c.name(clientID, "51~71")) + `,` + own(second) + `]}`,
			"other order":     `{"results":[` + own(second) + `,` + own(first) + `]}`,
		}
		for name, body := range cases {
			t.Run(c.family.tool+" "+name, func(t *testing.T) {
				e := newLabelEnvironment(t, nil, nil)
				e.fake.routes["POST "+c.path()] = okReply(body)
				_, err := e.invokeConfirmed(c.family.id("create"), c.create())
				if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
					len(e.posts()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestCompositeLabelAssignmentDescriptorsAndSummaries(t *testing.T) {
	for _, c := range compositeCases() {
		create, remove := c.family.descriptors()
		for _, d := range []capability.Descriptor{create, remove} {
			if d.Version != 1 || len(d.OutputSchema) == 0 || strings.Contains(string(d.InputSchema), "response_content_type") {
				t.Errorf("%s: %+v", d.ID, d)
			}
		}
		if !strings.Contains(string(create.InputSchema), `[0-9]{0,18}~[1-9][0-9]{0,18}$`) ||
			!strings.Contains(string(remove.InputSchema), `~[1-9][0-9]{0,18}~[1-9][0-9]{0,18}$`) {
			t.Errorf("%s: key patterns missing", c.family.tool)
		}
		if !strings.Contains(remove.Arguments[1].Description, "customers/ID/"+c.family.collection+"/N~N~N") ||
			!strings.Contains(create.Arguments[1].Description, "N~N or N") {
			t.Errorf("%s: argument texts lack the key forms", c.family.tool)
		}
		text := c.family.summary("create")(json.RawMessage(c.create()))
		for _, word := range []string{clientID, "2 " + c.family.noun, c.object + "/51~71", "labels/61"} {
			if !strings.Contains(text, word) {
				t.Errorf("summary %q lacks %q", text, word)
			}
		}
		text = c.family.summary("remove")(json.RawMessage(c.remove()))
		if !strings.Contains(text, c.name(clientID, "51~71~61")) {
			t.Errorf("summary %q", text)
		}
	}
}

func (c compositeCase) call(verb string) string {
	if verb == "create" {
		return c.create()
	}
	return c.remove()
}

func TestCompositeLabelAssignmentsAreReachedByTheMatchingEffectOrTheToolsList(t *testing.T) {
	for _, c := range compositeCases() {
		reached := func(conn config.Connection, verb string) bool {
			e := newEnvironmentWith(t, nil, nil, conn)
			e.fake.routes["POST "+c.path()] = okReply(c.answer())
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

func TestCompositeLabelAssignmentsNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, c := range compositeCases() {
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

func TestCompositeLabelAssignmentsManagerAccountsNeedAManagerTarget(t *testing.T) {
	for _, c := range compositeCases() {
		name := linkResource(managerID, c.family.collection, "7~8~9")
		call := `{"customer_id":"` + managerID + `","resource_names":["` + name + `"]}`
		answer := okReply(`{"results":[{"resourceName":"` + name + `"}]}`)
		path := "/v26/customers/" + managerID + c.family.service + ":mutate"
		t.Run(c.family.tool+" customer target naming a manager is refused", func(t *testing.T) {
			e := newLabelEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.routes["POST "+path] = answer
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
			e.fake.routes["POST "+path] = answer
			if _, err := e.invokeConfirmed(c.family.id("remove"), call); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
				t.Errorf("requests = %+v", got)
			}
		})
	}
}

func TestCompositeLabelAssignmentErrorsAreMappedAndNeverRepeated(t *testing.T) {
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
		for _, c := range compositeCases() {
			for _, verb := range []string{"create", "remove"} {
				t.Run(k.name+" "+c.family.id(verb), func(t *testing.T) {
					e := newLabelEnvironment(t, nil, nil)
					e.fake.routes["POST "+c.path()] = k.reply
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
