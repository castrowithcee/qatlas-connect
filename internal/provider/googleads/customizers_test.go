package googleads

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var customizerToolIDs = []string{campaignCustomizersCreateID, campaignCustomizersRemoveID, adGroupCustomizersCreateID,
	adGroupCustomizersRemoveID}

// customizerCase is one customizer family with the calls and answers both of its tools use.
type customizerCase struct {
	family   setLinkFamily
	object   string // collection of the object that gets the value
	wireName string // member of the create body that names the object
}

func customizerCases() []customizerCase {
	return []customizerCase{
		{family: campaignCustomizersFamily, object: "campaigns", wireName: "campaign"},
		{family: adGroupCustomizersFamily, object: "adGroups", wireName: "adGroup"},
	}
}

func (c customizerCase) path(customer string) string {
	return "/v26/customers/" + customer + c.family.service + ":mutate"
}

func (c customizerCase) name(customer, key string) string {
	return linkResource(customer, c.family.collection, key)
}

func (c customizerCase) entry(customer, objectID, attributeID, value string) string {
	return `{"` + c.family.parts[0].name + `":"` + linkResource(customer, c.object, objectID) +
		`","customizer_attribute":"` + linkResource(customer, "customizerAttributes", attributeID) +
		`","value":` + value + `}`
}

const (
	textValue   = `{"type":"TEXT","string_value":"Spring"}`
	numberValue = `{"type":"NUMBER","string_value":"15"}`
)

func (c customizerCase) create() string {
	return `{"customer_id":"` + clientID + `","` + c.family.argument + `":[` + c.entry(clientID, "21", "71", textValue) +
		`,` + c.entry(clientID, "22", "72", numberValue) + `]}`
}

func (c customizerCase) remove() string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + c.name(clientID, "21~71") + `","` +
		c.name(clientID, "22~72") + `"]}`
}

func (c customizerCase) call(verb string) string {
	if verb == "create" {
		return c.create()
	}
	return c.remove()
}

func (c customizerCase) answer() string {
	return `{"results":[{"resourceName":"` + c.name(clientID, "21~71") + `"},{"resourceName":"` +
		c.name(clientID, "22~72") + `"}]}`
}

func (c customizerCase) createBody() string {
	return `[{"create":{"` + c.wireName + `":"` + linkResource(clientID, c.object, "21") +
		`","customizerAttribute":"` + linkResource(clientID, "customizerAttributes", "71") +
		`","value":{"type":"TEXT","stringValue":"Spring"}}},{"create":{"` + c.wireName + `":"` +
		linkResource(clientID, c.object, "22") + `","customizerAttribute":"` +
		linkResource(clientID, "customizerAttributes", "72") + `","value":{"type":"NUMBER","stringValue":"15"}}}]`
}

func newCustomizerEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: customizerToolIDs})
}

func TestCustomizerToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{"create": capability.EffectCreate, "remove": capability.EffectDelete}
	for _, c := range customizerCases() {
		for _, verb := range []string{"create", "remove"} {
			id := c.family.id(verb)
			d, _, ok := reg.Lookup(id)
			if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[verb] || len(d.OutputSchema) == 0 ||
				d.Risk.Confirmation != capability.ConfirmationRequired || d.RequiresToolAllowList ||
				!strings.HasSuffix(d.Description, mutateListNote) || !strings.Contains(d.Description, c.family.rpc) ||
				!strings.Contains(d.Description, "no update") {
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
			for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "update_mask", "status"} {
				if _, found := input.Properties[forbidden]; found {
					t.Errorf("%s offers %s", id, forbidden)
				}
			}
			if _, found := input.Properties["response_content_type"]; found != (verb == "create") {
				t.Errorf("%s: response_content_type present = %v", id, found)
			}
			if verb == "create" && !strings.Contains(string(d.InputSchema), `"string_value"`) {
				t.Errorf("%s: input schema has no value", id)
			}
		}
		if _, _, ok := reg.Lookup(Provider + "." + c.family.tool + ".update"); ok {
			t.Errorf("%s has an update tool", c.family.tool)
		}
	}
}

func TestCustomizersAreReachedByTheMatchingEffectOrTheToolsList(t *testing.T) {
	for _, c := range customizerCases() {
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
			{"tools list", config.Connection{Tools: customizerToolIDs}, true, true},
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

func TestCustomizersNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, c := range customizerCases() {
		for _, verb := range []string{"create", "remove"} {
			t.Run(c.family.id(verb), func(t *testing.T) {
				e := newCustomizerEnvironment(t, nil, nil)
				_, err := e.invoke(c.family.id(verb), c.call(verb))
				var needed *application.ConfirmationRequiredError
				if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
					t.Fatalf("err = %v, want a confirmation that names the account", err)
				}
				if verb == "create" && !strings.Contains(needed.Summary, `TEXT value "Spring"`) {
					t.Errorf("summary %q lacks the value", needed.Summary)
				}
				e.untouched(t)

				e = newCustomizerEnvironment(t, []string{"customers/" + clientID}, nil)
				_, err = e.invokeConfirmed(c.family.id(verb), strings.ReplaceAll(c.call(verb), clientID, otherID))
				if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
					t.Fatalf("err = %v, want invalid-request without the ID", err)
				}
				e.untouched(t)
			})
		}
	}
}

func TestCustomizerArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	c := campaignCustomizersFamily
	head := `{"customer_id":"` + clientID + `","campaign_customizers":[`
	campaign := linkResource(clientID, "campaigns", "21")
	attribute := linkResource(clientID, "customizerAttributes", "71")
	entry := func(members string) string { return head + `{` + members + `}]}` }
	base := `"campaign":"` + campaign + `","customizer_attribute":"` + attribute + `"`
	var many []string
	for i := 0; i < 11; i++ {
		many = append(many, `{`+base+`,"value":`+textValue+`}`)
	}
	bad := map[string]string{
		"none":           head + `]}`,
		"too many":       head + strings.Join(many, ",") + `]}`,
		"no value":       entry(base),
		"no attribute":   entry(`"campaign":"` + campaign + `","value":` + textValue),
		"no campaign":    entry(`"customizer_attribute":"` + attribute + `","value":` + textValue),
		"value text":     entry(base + `,"value":"Spring"`),
		"value no type":  entry(base + `,"value":{"string_value":"Spring"}`),
		"value no text":  entry(base + `,"value":{"type":"TEXT"}`),
		"value type":     entry(base + `,"value":{"type":"UNKNOWN","string_value":"a"}`),
		"value lower":    entry(base + `,"value":{"type":"text","string_value":"a"}`),
		"value empty":    entry(base + `,"value":{"type":"TEXT","string_value":""}`),
		"value long":     entry(base + `,"value":{"type":"TEXT","string_value":"` + strings.Repeat("a", customizerValueMax+1) + `"}`),
		"value control":  entry(base + `,"value":{"type":"TEXT","string_value":"a\nb"}`),
		"value extra":    entry(base + `,"value":{"type":"TEXT","string_value":"a","status":"ENABLED"}`),
		"entry status":   entry(base + `,"value":` + textValue + `,"status":"ENABLED"`),
		"foreign campg":  entry(`"campaign":"` + linkResource(otherID, "campaigns", "21") + `","customizer_attribute":"` + attribute + `","value":` + textValue),
		"foreign attrib": entry(`"campaign":"` + campaign + `","customizer_attribute":"` + linkResource(otherID, "customizerAttributes", "71") + `","value":` + textValue),
		"temporary":      entry(`"campaign":"` + linkResource(clientID, "campaigns", "-1") + `","customizer_attribute":"` + attribute + `","value":` + textValue),
		"attribute kind": entry(`"campaign":"` + campaign + `","customizer_attribute":"` + linkResource(clientID, "labels", "71") + `","value":` + textValue),
		"twice":          head + `{` + base + `,"value":` + textValue + `},{` + base + `,"value":` + numberValue + `}]}`,
		"content type":   strings.TrimSuffix(entry(base+`,"value":`+textValue), "}") + `,"response_content_type":"X"}`,
	}
	for name, arguments := range bad {
		t.Run(name, func(t *testing.T) {
			e := newCustomizerEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c.id("create"), arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	removes := map[string]string{
		"remove none":         `[]`,
		"remove attribute id": `["customers/` + queryNumber(clientID) + `/campaignCustomizers/21"]`,
		"remove other kind":   `["customers/` + queryNumber(clientID) + `/adGroupCustomizers/21~71"]`,
		"remove foreign":      `["` + linkResource(otherID, "campaignCustomizers", "21~71") + `"]`,
		"remove twice":        `["` + linkResource(clientID, "campaignCustomizers", "21~71") + `","` + linkResource(clientID, "campaignCustomizers", "21~71") + `"]`,
	}
	for name, names := range removes {
		t.Run(name, func(t *testing.T) {
			e := newCustomizerEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c.id("remove"), `{"customer_id":"`+clientID+`","resource_names":`+names+`}`); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newCustomizerEnvironment(t, nil, nil)
		e.fake.routes["POST "+customizerCases()[0].path(clientID)] = okReply(`{"results":[{"resourceName":"` +
			customizerCases()[0].name(clientID, "21~71") + `"}]}`)
		arguments := entry(base + `,"value":{"type":"TEXT","string_value":"` + strings.Repeat("ä", customizerValueMax) + `"}`)
		if _, err := e.invokeConfirmed(c.id("create"), arguments); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCustomizersSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	for _, c := range customizerCases() {
		want := map[string]string{"create": c.createBody(),
			"remove": `[{"remove":"` + c.name(clientID, "21~71") + `"},{"remove":"` + c.name(clientID, "22~72") + `"}]`}
		for _, verb := range []string{"create", "remove"} {
			id := c.family.id(verb)
			t.Run(id, func(t *testing.T) {
				e := newCustomizerEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
				e.fake.routes["POST "+c.path(clientID)] = okReply(c.answer())
				out, err := e.invokeConfirmed(id, c.call(verb))
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
					results[1].(map[string]any)["resource_name"] != c.name(clientID, "22~72") {
					t.Errorf("out = %s", out)
				}
			})
			t.Run(id+" validate only", func(t *testing.T) {
				e := newCustomizerEnvironment(t, nil, nil)
				e.fake.routes["POST "+c.path(clientID)] = okReply(`{}`)
				out, err := e.invokeConfirmed(id, strings.TrimSuffix(c.call(verb), "}")+`,"validate_only":true}`)
				if err != nil {
					t.Fatal(err)
				}
				if got := decode(t, out); bodyOf(t, e.posts()[0])["validateOnly"] != true || got["validate_only"] != true ||
					got["results"] != nil {
					t.Errorf("out = %s", out)
				}
			})
		}
	}
}

func TestCustomizerResponseContentTypeShowsTheValue(t *testing.T) {
	c := customizerCases()[0]
	e := newCustomizerEnvironment(t, nil, nil)
	e.fake.routes["POST "+c.path(clientID)] = okReply(`{"results":[{"resourceName":"` + c.name(clientID, "21~71") +
		`","campaignCustomizer":{"resourceName":"` + c.name(clientID, "21~71") + `","campaign":"` +
		linkResource(clientID, "campaigns", "21") + `","customizerAttribute":"` +
		linkResource(clientID, "customizerAttributes", "71") + `","status":"ENABLED","value":{"type":"TEXT","stringValue":"Spring"}}},` +
		`{"resourceName":"` + c.name(clientID, "22~72") + `"}]}`)
	out, err := e.invokeConfirmed(c.family.id("create"), strings.TrimSuffix(c.create(), "}")+`,"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	item := decode(t, out)["results"].([]any)[0].(map[string]any)["campaign_customizer"].(map[string]any)
	value, _ := item["value"].(map[string]any)
	if item["status"] != "ENABLED" || item["customizer_attribute"] != linkResource(clientID, "customizerAttributes", "71") ||
		value["type"] != "TEXT" || value["string_value"] != "Spring" {
		t.Errorf("campaign_customizer = %v", item)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestCustomizerResultsAreBoundToTheCheckedAccount(t *testing.T) {
	c := customizerCases()[1]
	own := func(key string) string { return `{"resourceName":"` + c.name(clientID, key) + `"}` }
	shown := func(object string) string {
		return `{"resourceName":"` + c.name(clientID, "21~71") + `","adGroupCustomizer":{"resourceName":"` +
			c.name(clientID, "21~71") + `"` + object + `}}`
	}
	cases := map[string]struct{ verb, body string }{
		"foreign":      {"create", `{"results":[{"resourceName":"` + c.name(otherID, "21~71") + `"},` + own("22~72") + `]}`},
		"none":         {"create", `{}`},
		"one":          {"create", `{"results":[` + own("21~71") + `]}`},
		"other object": {"create", `{"results":[` + own("21~71") + `,` + own("23~72") + `]}`},
		"partial":      {"create", `{"results":[` + own("21~71") + `,` + own("22~72") + `],"partialFailureError":{"code":3}}`},
		"other kind": {"create", `{"results":[{"resourceName":"` + linkResource(clientID, "campaignCustomizers", "21~71") + `"},` +
			own("22~72") + `]}`},
		"shown group": {"create", `{"results":[` + shown(`,"adGroup":"`+linkResource(clientID, "adGroups", "99")+`"`) + `,` + own("22~72") + `]}`},
		"shown attr": {"create", `{"results":[` + shown(`,"customizerAttribute":"`+linkResource(otherID, "customizerAttributes", "71")+`"`) +
			`,` + own("22~72") + `]}`},
		"shown name": {"create", `{"results":[{"resourceName":"` + c.name(clientID, "21~71") + `","adGroupCustomizer":{"resourceName":"` +
			c.name(clientID, "21~99") + `"}},` + own("22~72") + `]}`},
		"remove order": {"remove", `{"results":[` + own("22~72") + `,` + own("21~71") + `]}`},
		"remove other": {"remove", `{"results":[` + own("21~71") + `,` + own("22~73") + `]}`},
	}
	for name, k := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCustomizerEnvironment(t, nil, nil)
			e.fake.routes["POST "+c.path(clientID)] = okReply(k.body)
			arguments := c.call(k.verb)
			if strings.HasPrefix(name, "shown") {
				arguments = strings.TrimSuffix(arguments, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
			}
			_, err := e.invokeConfirmed(c.family.id(k.verb), arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestCustomizerManagerAccountsNeedAManagerTarget(t *testing.T) {
	c := customizerCases()[0]
	call := `{"customer_id":"` + managerID + `","resource_names":["` + c.name(managerID, "21~71") + `"]}`
	answer := okReply(`{"results":[{"resourceName":"` + c.name(managerID, "21~71") + `"}]}`)
	e := newCustomizerEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.routes["POST "+c.path(managerID)] = answer
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	if _, err := e.invokeConfirmed(c.family.id("remove"), call); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
	e = newCustomizerEnvironment(t, []string{"managers/" + managerID}, nil)
	e.fake.routes["POST "+c.path(managerID)] = answer
	if _, err := e.invokeConfirmed(c.family.id("remove"), call); err != nil {
		t.Fatal(err)
	}
}

func TestCustomizerErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"customizer error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "customizerError",
			"INVALID_CUSTOMIZER_VALUE", "")}, provider.ClassProviderError,
			"rejected the change (customizerError INVALID_CUSTOMIZER_VALUE), nothing was changed"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown"},
	}
	for _, k := range cases {
		for _, c := range customizerCases() {
			for _, verb := range []string{"create", "remove"} {
				t.Run(k.name+" "+c.family.id(verb), func(t *testing.T) {
					e := newCustomizerEnvironment(t, nil, nil)
					e.fake.routes["POST "+c.path(clientID)] = k.reply
					_, err := e.invokeConfirmed(c.family.id(verb), c.call(verb))
					if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) || len(e.posts()) != 1 ||
						len(e.fake.api()) != 1 {
						t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
					}
				})
			}
		}
	}
}

func TestCustomizerSummariesNameTheEffect(t *testing.T) {
	c := customizerCases()[0]
	create := c.family.summary("create")(json.RawMessage(c.create()))
	remove := c.family.summary("remove")(json.RawMessage(c.remove()))
	for _, word := range []string{clientID, "2 campaign customizer", `TEXT value "Spring"`, `NUMBER value "15"`} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	for _, word := range []string{clientID, "campaignCustomizers/21~71", "objects it ties stay"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	long := strings.Replace(c.create(), "Spring", strings.Repeat("x", 500), 1)
	if text := c.family.summary("create")(json.RawMessage(long)); len(text) > 1500 {
		t.Errorf("summary holds %d bytes", len(text))
	}
}
