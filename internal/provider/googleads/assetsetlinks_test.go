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

var setLinkToolIDs = []string{assetSetAssetsCreateID, assetSetAssetsRemoveID, campaignAssetSetsCreateID,
	campaignAssetSetsRemoveID, adGroupAssetSetsCreateID, adGroupAssetSetsRemoveID, customerAssetSetsCreateID,
	customerAssetSetsRemoveID}

// setLinkCase is one family with the calls and the answers that the tests of both tools use.
type setLinkCase struct {
	family     setLinkFamily
	create     string
	remove     string
	argument   string
	linkNames  [2]string
	createBody string
	// other is a resource name of a collection that is none of the parts of the family.
	otherCollection string
}

func (c setLinkCase) path(customer string) string {
	return "/v26/customers/" + customer + c.family.service + ":mutate"
}

func linkResource(customer, collection, id string) string {
	return "customers/" + queryNumber(customer) + "/" + collection + "/" + id
}

func (c setLinkCase) link(customer, key string) string {
	return linkResource(customer, c.family.collection, key)
}

func (c setLinkCase) call(verb string) string {
	if verb == "create" {
		return c.create
	}
	return c.remove
}

func (c setLinkCase) answer(verb string) string {
	return `{"results":[{"resourceName":"` + c.linkNames[0] + `"},{"resourceName":"` + c.linkNames[1] + `"}]}`
}

func (c setLinkCase) tool(verb string) string { return c.family.id(verb) }

func newSetLinkCases() []setLinkCase {
	asset := func(id string) string { return linkResource(clientID, "assets", id) }
	set := func(id string) string { return linkResource(clientID, "assetSets", id) }
	campaign := func(id string) string { return linkResource(clientID, "campaigns", id) }
	adGroup := func(id string) string { return linkResource(clientID, "adGroups", id) }
	head := `{"customer_id":"` + clientID + `",`
	return []setLinkCase{
		{family: assetSetAssetsFamily, argument: "asset_set_assets",
			create: head + `"asset_set_assets":[{"asset_set":"` + set("31") + `","asset":"` + asset("41") + `"},` +
				`{"asset_set":"` + set("31") + `","asset":"` + asset("42") + `"}]}`,
			remove: head + `"resource_names":["` + linkResource(clientID, "assetSetAssets", "31~41") + `","` +
				linkResource(clientID, "assetSetAssets", "31~42") + `"]}`,
			linkNames: [2]string{linkResource(clientID, "assetSetAssets", "31~41"), linkResource(clientID, "assetSetAssets", "31~42")},
			createBody: `[{"create":{"assetSet":"` + set("31") + `","asset":"` + asset("41") + `"}},` +
				`{"create":{"assetSet":"` + set("31") + `","asset":"` + asset("42") + `"}}]`,
			otherCollection: "labels"},
		{family: campaignAssetSetsFamily, argument: "campaign_asset_sets",
			create: head + `"campaign_asset_sets":[{"campaign":"` + campaign("21") + `","asset_set":"` + set("31") + `"},` +
				`{"campaign":"` + campaign("22") + `","asset_set":"` + set("31") + `"}]}`,
			remove: head + `"resource_names":["` + linkResource(clientID, "campaignAssetSets", "21~31") + `","` +
				linkResource(clientID, "campaignAssetSets", "22~31") + `"]}`,
			linkNames: [2]string{linkResource(clientID, "campaignAssetSets", "21~31"), linkResource(clientID, "campaignAssetSets", "22~31")},
			createBody: `[{"create":{"campaign":"` + campaign("21") + `","assetSet":"` + set("31") + `"}},` +
				`{"create":{"campaign":"` + campaign("22") + `","assetSet":"` + set("31") + `"}}]`,
			otherCollection: "labels"},
		{family: adGroupAssetSetsFamily, argument: "ad_group_asset_sets",
			create: head + `"ad_group_asset_sets":[{"ad_group":"` + adGroup("51") + `","asset_set":"` + set("31") + `"},` +
				`{"ad_group":"` + adGroup("52") + `","asset_set":"` + set("31") + `"}]}`,
			remove: head + `"resource_names":["` + linkResource(clientID, "adGroupAssetSets", "51~31") + `","` +
				linkResource(clientID, "adGroupAssetSets", "52~31") + `"]}`,
			linkNames: [2]string{linkResource(clientID, "adGroupAssetSets", "51~31"), linkResource(clientID, "adGroupAssetSets", "52~31")},
			createBody: `[{"create":{"adGroup":"` + adGroup("51") + `","assetSet":"` + set("31") + `"}},` +
				`{"create":{"adGroup":"` + adGroup("52") + `","assetSet":"` + set("31") + `"}}]`,
			otherCollection: "labels"},
	}
}

func newSetLinkEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: setLinkToolIDs})
}

func TestSetLinkToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{"create": capability.EffectCreate, "remove": capability.EffectDelete}
	for _, c := range newSetLinkCases() {
		for _, verb := range []string{"create", "remove"} {
			id := c.tool(verb)
			d, _, ok := reg.Lookup(id)
			if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[verb] || len(d.OutputSchema) == 0 ||
				d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
				t.Fatalf("%s = %+v", id, d)
			}
			// Neither tool is a listed-only tool: a connection with the matching effect offers both.
			if d.RequiresToolAllowList || !strings.HasSuffix(d.Description, mutateListNote) ||
				strings.HasSuffix(d.Description, mutateListListedNote) {
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
				Required   []string                   `json:"required"`
				Additional bool                       `json:"additionalProperties"`
			}
			if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
				t.Errorf("%s: input schema is open", id)
			}
			for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "url",
				"update_mask", "status"} {
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
			if !strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "atomic") ||
				!strings.Contains(d.Description, c.family.rpc) {
				t.Errorf("%s: description lacks the unknown outcome or the method", id)
			}
		}
		if _, _, ok := reg.Lookup(Provider + "." + c.family.tool + ".update"); ok {
			t.Errorf("%s has an update tool", c.family.tool)
		}
	}
}

func TestSetLinkToolsAreReachedByTheMatchingEffectOrTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	for _, c := range newSetLinkCases() {
		reached := func(conn config.Connection, verb string) bool {
			e := newEnvironmentWith(t, nil, nil, conn)
			e.fake.routes["POST "+c.path(clientID)] = okReply(c.answer(verb))
			_, err := e.invokeConfirmed(c.tool(verb), c.call(verb))
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
			{"remove listed", config.Connection{Tools: []string{c.tool("remove")}}, false, true},
			{"create listed", config.Connection{Tools: []string{c.tool("create")}}, true, false},
		}
		for _, k := range cases {
			t.Run(c.family.tool+" "+k.name, func(t *testing.T) {
				if got := reached(k.conn, "create"); got != k.create {
					t.Errorf("create reached = %v, want %v", got, k.create)
				}
				if got := reached(k.conn, "remove"); got != k.remv {
					t.Errorf("remove reached = %v, want %v", got, k.remv)
				}
			})
		}
	}
}

func TestSetLinksNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, c := range newSetLinkCases() {
		for _, verb := range []string{"create", "remove"} {
			t.Run(c.tool(verb), func(t *testing.T) {
				e := newSetLinkEnvironment(t, nil, nil)
				_, err := e.invoke(c.tool(verb), c.call(verb))
				var needed *application.ConfirmationRequiredError
				if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
					t.Fatalf("err = %v, want a confirmation that names the account", err)
				}
				e.untouched(t)
			})
		}
	}
}

func TestSetLinksRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"status"`, `"status":"ENABLED"`} {
		for _, c := range newSetLinkCases() {
			for _, verb := range []string{"create", "remove"} {
				e := newSetLinkEnvironment(t, nil, nil)
				arguments := strings.TrimSuffix(c.call(verb), "}") + "," + extra + "}"
				if _, err := e.invokeConfirmed(c.tool(verb), arguments); err == nil {
					t.Errorf("%s accepted %s", c.tool(verb), extra)
				}
				e.untouched(t)
			}
		}
	}
	// The remove tools offer no response_content_type.
	for _, c := range newSetLinkCases() {
		e := newSetLinkEnvironment(t, nil, nil)
		arguments := strings.TrimSuffix(c.remove, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
		if _, err := e.invokeConfirmed(c.tool("remove"), arguments); err == nil {
			t.Errorf("%s accepted response_content_type", c.tool("remove"))
		}
		e.untouched(t)
	}
}

func TestSetLinkTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, c := range newSetLinkCases() {
		for _, verb := range []string{"create", "remove"} {
			t.Run(c.tool(verb), func(t *testing.T) {
				e := newSetLinkEnvironment(t, []string{"customers/" + clientID}, nil)
				arguments := strings.ReplaceAll(c.call(verb), clientID, otherID)
				_, err := e.invokeConfirmed(c.tool(verb), arguments)
				if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
					t.Fatalf("err = %v, want invalid-request without the ID", err)
				}
				e.untouched(t)
			})
		}
	}
}

func TestSetLinkArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, c := range newSetLinkCases() {
		head := `{"customer_id":"` + clientID + `",`
		first, second := c.family.parts[0], c.family.parts[1]
		part := func(p setLinkPart, id string) string {
			return `"` + p.name + `":"` + linkResource(clientID, p.collection, id) + `"`
		}
		entry := func(a, b string) string { return `{` + a + `,` + b + `}` }
		good := entry(part(first, "5"), part(second, "6"))
		list := func(items ...string) string { return head + `"` + c.argument + `":[` + strings.Join(items, ",") + `]}` }
		names := func(items ...string) string { return head + `"resource_names":[` + strings.Join(items, ",") + `]}` }
		link := func(key string) string { return `"` + c.link(clientID, key) + `"` }
		many := func(n int, item func(i int) string) []string {
			var out []string
			for i := 1; i <= n; i++ {
				out = append(out, item(i))
			}
			return out
		}
		bad := map[string][2]string{
			"create none":       {"create", list()},
			"create too many":   {"create", list(many(11, func(i int) string { return entry(part(first, strconv.Itoa(i)), part(second, "6")) })...)},
			"create not object": {"create", list(`"x"`)},
			"create one part":   {"create", list(`{` + part(first, "5") + `}`)},
			"create extra part": {"create", list(`{` + part(first, "5") + `,` + part(second, "6") + `,"status":"ENABLED"}`)},
			"create swapped":    {"create", list(entry(part(first, "5"), `"`+first.name+`":"`+linkResource(clientID, second.collection, "6")+`"`))},
			"create foreign":    {"create", list(entry(part(first, "5"), `"`+second.name+`":"`+linkResource(otherID, second.collection, "6")+`"`))},
			"create wrong kind": {"create", list(entry(part(first, "5"), `"`+second.name+`":"`+linkResource(clientID, c.otherCollection, "6")+`"`))},
			"create temporary":  {"create", list(entry(part(first, "5"), `"`+second.name+`":"`+linkResource(clientID, second.collection, "-6")+`"`))},
			"create zero":       {"create", list(entry(part(first, "5"), `"`+second.name+`":"`+linkResource(clientID, second.collection, "0")+`"`))},
			"create padded":     {"create", list(entry(part(first, "5"), `"`+second.name+`":"`+linkResource(clientID, second.collection, "06")+`"`))},
			"create number":     {"create", list(entry(part(first, "5"), `"`+second.name+`":6`))},
			"create duplicate":  {"create", list(good, good)},
			"create only flag":  {"create", head + `"validate_only":true}`},
			"remove none":       {"remove", names()},
			"remove too many":   {"remove", names(many(11, func(i int) string { return link(strconv.Itoa(i) + "~7") })...)},
			"remove one id":     {"remove", names(link("5"))},
			"remove three ids":  {"remove", names(link("5~6~7"))},
			"remove foreign":    {"remove", names(`"` + c.link(otherID, "5~6") + `"`)},
			"remove other kind": {"remove", names(`"` + linkResource(clientID, c.otherCollection, "5~6") + `"`)},
			"remove zero":       {"remove", names(link("0~6"))},
			"remove temporary":  {"remove", names(link("-5~6"))},
			"remove padded":     {"remove", names(link("05~6"))},
			"remove duplicate":  {"remove", names(link("5~6"), link("5~6"))},
			"remove text":       {"remove", names(`"x"`)},
		}
		for name, k := range bad {
			t.Run(c.family.tool+" "+name, func(t *testing.T) {
				e := newSetLinkEnvironment(t, nil, nil)
				if _, err := e.invokeConfirmed(c.tool(k[0]), k[1]); err == nil {
					t.Fatal("accepted")
				}
				e.untouched(t)
			})
		}
		t.Run(c.family.tool+" limits pass", func(t *testing.T) {
			e := newSetLinkEnvironment(t, nil, nil)
			var items, results []string
			for i := 1; i <= 10; i++ {
				items = append(items, entry(part(first, strconv.Itoa(i)), part(second, "6")))
				results = append(results, `{"resourceName":"`+c.link(clientID, strconv.Itoa(i)+"~6")+`"}`)
			}
			e.fake.routes["POST "+c.path(clientID)] = okReply(`{"results":[` + strings.Join(results, ",") + `]}`)
			if _, err := e.invokeConfirmed(c.tool("create"), list(items...)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSetLinksSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	for _, c := range newSetLinkCases() {
		want := map[string]string{"create": c.createBody}
		var removes []string
		for _, name := range c.linkNames {
			removes = append(removes, `{"remove":"`+name+`"}`)
		}
		want["remove"] = "[" + strings.Join(removes, ",") + "]"
		for _, verb := range []string{"create", "remove"} {
			t.Run(c.tool(verb), func(t *testing.T) {
				e := newSetLinkEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
				e.fake.routes["POST "+c.path(clientID)] = okReply(c.answer(verb))
				out, err := e.invokeConfirmed(c.tool(verb), c.call(verb))
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
					results[1].(map[string]any)["resource_name"] != c.linkNames[1] || len(results[1].(map[string]any)) != 1 {
					t.Errorf("out = %s", out)
				}
			})
		}
	}
}

func TestSetLinksValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, c := range newSetLinkCases() {
		for _, verb := range []string{"create", "remove"} {
			t.Run(c.tool(verb), func(t *testing.T) {
				e := newSetLinkEnvironment(t, nil, nil)
				e.fake.routes["POST "+c.path(clientID)] = okReply(`{}`)
				out, err := e.invokeConfirmed(c.tool(verb), strings.TrimSuffix(c.call(verb), "}")+`,"validate_only":true}`)
				if err != nil {
					t.Fatal(err)
				}
				body := bodyOf(t, e.posts()[0])
				got := decode(t, out)
				if body["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
					t.Errorf("body = %v, out = %s", body, out)
				}
			})
			summary := c.family.summary(verb)
			if text := summary(json.RawMessage(strings.TrimSuffix(c.call(verb), "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
				t.Errorf("%s: summary %q", c.tool(verb), text)
			}
			if text := summary(json.RawMessage(c.call(verb))); strings.Contains(text, "validate") {
				t.Errorf("%s: summary %q kept state", c.tool(verb), text)
			}
		}
	}
}

func TestSetLinksResponseContentTypeShowsTheLink(t *testing.T) {
	for _, c := range newSetLinkCases() {
		t.Run(c.family.tool, func(t *testing.T) {
			e := newSetLinkEnvironment(t, nil, nil)
			first, second := c.family.parts[0], c.family.parts[1]
			link := `{"resourceName":"` + c.linkNames[0] + `","` + camel(first.name) + `":"` + linkResource(clientID, first.collection,
				strings.Split(strings.TrimPrefix(c.linkNames[0], c.link(clientID, "")), "~")[0]) + `","` + camel(second.name) + `":"` +
				linkResource(clientID, second.collection, strings.Split(strings.TrimPrefix(c.linkNames[0], c.link(clientID, "")), "~")[1]) +
				`","status":"ENABLED","unknownMember":"x"}`
			e.fake.routes["POST "+c.path(clientID)] = okReply(`{"results":[{"resourceName":"` + c.linkNames[0] + `","` + c.family.key +
				`":` + link + `},{"resourceName":"` + c.linkNames[1] + `"}]}`)
			out, err := e.invokeConfirmed(c.tool("create"), strings.TrimSuffix(c.create, "}")+`,"response_content_type":"MUTABLE_RESOURCE"}`)
			if err != nil {
				t.Fatal(err)
			}
			item := decode(t, out)["results"].([]any)[0].(map[string]any)[c.family.resultKey].(map[string]any)
			if item["status"] != "ENABLED" || item["resource_name"] != c.linkNames[0] || item["unknownMember"] != nil ||
				item[first.name] == nil || item[second.name] == nil || len(item) != 4 {
				t.Errorf("link = %v", item)
			}
			if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
				t.Errorf("body = %s", e.posts()[0].body)
			}
		})
	}
}

// A resource name of another account, another link or another count never passes. The request was sent, so the
// error says that the outcome is unknown.
func TestSetLinksResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	for _, c := range newSetLinkCases() {
		first, second := c.family.parts[0], c.family.parts[1]
		foreign := c.link(otherID, "21~31")
		own := func(name string) string { return `{"resourceName":"` + name + `"}` }
		withLink := func(link string) string {
			return `{"results":[{"resourceName":"` + c.linkNames[0] + `","` + c.family.key + `":` + link + `},` + own(c.linkNames[1]) + `]}`
		}
		ids := strings.Split(strings.TrimPrefix(c.linkNames[0], c.link(clientID, "")), "~")
		part := func(p setLinkPart, id string) string {
			return `"` + camel(p.name) + `":"` + linkResource(clientID, p.collection, id) + `"`
		}
		cases := map[string]struct{ verb, body string }{
			"create foreign":         {"create", `{"results":[` + own(foreign) + `,` + own(c.linkNames[1]) + `]}`},
			"create no results":      {"create", `{}`},
			"create one of two":      {"create", `{"results":[` + own(c.linkNames[0]) + `]}`},
			"create other order":     {"create", `{"results":[` + own(c.linkNames[1]) + `,` + own(c.linkNames[0]) + `]}`},
			"create other link":      {"create", `{"results":[` + own(c.link(clientID, "21~99")) + `,` + own(c.linkNames[1]) + `]}`},
			"create one id":          {"create", `{"results":[` + own(c.link(clientID, "21")) + `,` + own(c.linkNames[1]) + `]}`},
			"create three ids":       {"create", `{"results":[` + own(c.link(clientID, "21~31~4")) + `,` + own(c.linkNames[1]) + `]}`},
			"create other kind":      {"create", `{"results":[` + own(linkResource(clientID, c.otherCollection, "21~31")) + `,` + own(c.linkNames[1]) + `]}`},
			"create no name":         {"create", `{"results":[{},` + own(c.linkNames[1]) + `]}`},
			"create partial failure": {"create", `{"results":[` + own(c.linkNames[0]) + `,` + own(c.linkNames[1]) + `],"partialFailureError":{"code":3}}`},
			"create link foreign":    {"create", withLink(`{"resourceName":"` + foreign + `"}`)},
			"create link part":       {"create", withLink(`{"resourceName":"` + c.linkNames[0] + `",` + part(first, "99") + `}`)},
			"create link other part": {"create", withLink(`{"resourceName":"` + c.linkNames[0] + `",` + part(second, "99") + `}`)},
			"create link part kind": {"create", withLink(`{"resourceName":"` + c.linkNames[0] + `","` + camel(first.name) + `":"` +
				linkResource(clientID, c.otherCollection, ids[0]) + `"}`)},
			"create link part acct": {"create", withLink(`{"resourceName":"` + c.linkNames[0] + `","` + camel(first.name) + `":"` +
				linkResource(otherID, first.collection, ids[0]) + `"}`)},
			"create link shape":      {"create", withLink(`{"resourceName":"` + c.linkNames[0] + `","status":5}`)},
			"create link not object": {"create", withLink(`"text"`)},
			"remove foreign":         {"remove", `{"results":[` + own(foreign) + `,` + own(c.linkNames[1]) + `]}`},
			"remove other link":      {"remove", `{"results":[` + own(c.linkNames[1]) + `,` + own(c.linkNames[0]) + `]}`},
			"remove none":            {"remove", `{"results":[]}`},
		}
		for name, k := range cases {
			t.Run(c.family.tool+" "+name, func(t *testing.T) {
				e := newSetLinkEnvironment(t, nil, nil)
				e.fake.routes["POST "+c.path(clientID)] = okReply(k.body)
				arguments := c.call(k.verb)
				if strings.Contains(name, "link ") {
					arguments = strings.TrimSuffix(arguments, "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`
				}
				_, err := e.invokeConfirmed(c.tool(k.verb), arguments)
				if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
					len(e.posts()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestSetLinksManagerAccountsNeedAManagerTarget(t *testing.T) {
	for _, c := range newSetLinkCases() {
		name := linkResource(managerID, c.family.collection, "7~8")
		managerCall := `{"customer_id":"` + managerID + `","resource_names":["` + name + `"]}`
		answer := okReply(`{"results":[{"resourceName":"` + name + `"}]}`)
		t.Run(c.family.tool+" a customer target that names a manager is refused", func(t *testing.T) {
			e := newSetLinkEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.routes["POST "+c.path(managerID)] = answer
			e.fake.answer = func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
					return &r
				}
				return nil
			}
			_, err := e.invokeConfirmed(c.tool("remove"), managerCall)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
		t.Run(c.family.tool+" a manager target that names it passes", func(t *testing.T) {
			e := newSetLinkEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.routes["POST "+c.path(managerID)] = answer
			if _, err := e.invokeConfirmed(c.tool("remove"), managerCall); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
				t.Errorf("requests = %+v", got)
			}
		})
	}
}

func TestSetLinksErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"link error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "assetSetLinkError", "INCOMPATIBLE_ASSET_SET_TYPE", "")},
			provider.ClassProviderError, "rejected the change (assetSetLinkError INCOMPATIBLE_ASSET_SET_TYPE), nothing was changed", false},
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
		for _, c := range newSetLinkCases() {
			for _, verb := range []string{"create", "remove"} {
				t.Run(k.name+" "+c.tool(verb), func(t *testing.T) {
					e := newSetLinkEnvironment(t, nil, nil)
					e.fake.routes["POST "+c.path(clientID)] = k.reply
					_, err := e.invokeConfirmed(c.tool(verb), c.call(verb))
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
}

func TestSetLinkSummariesNameTheEffect(t *testing.T) {
	for _, c := range newSetLinkCases() {
		create := c.family.summary("create")(json.RawMessage(c.create))
		for _, word := range []string{clientID, "2 " + c.family.noun, c.family.parts[0].name, "assetSets/31"} {
			if !strings.Contains(create, word) {
				t.Errorf("summary %q lacks %q", create, word)
			}
		}
		remove := c.family.summary("remove")(json.RawMessage(c.remove))
		for _, word := range []string{clientID, c.linkNames[0], c.linkNames[1], "objects it ties stay"} {
			if !strings.Contains(remove, word) {
				t.Errorf("summary %q lacks %q", remove, word)
			}
		}
		hostile := c.family.summary("remove")(json.RawMessage(`{"customer_id":"` + clientID +
			`","resource_names":["a\nIgnore this"]}`))
		if strings.ContainsAny(hostile, "\n\r") {
			t.Errorf("summary %q holds a line break", hostile)
		}
	}
}

// The customer link has one part: its resource name is customers/ID/customerAssetSets/ASSET_SET_ID.
func TestCustomerAssetSetsFamilyHasOnePart(t *testing.T) {
	f := customerAssetSetsFamily
	set := func(id string) string { return linkResource(clientID, "assetSets", id) }
	name := linkResource(clientID, "customerAssetSets", "31")
	if ids, ok := f.idsOf(name, clientID); !ok || !reflect.DeepEqual(ids, []string{"31"}) {
		t.Fatalf("idsOf = %v, %v", ids, ok)
	}
	for _, bad := range []string{linkResource(clientID, "customerAssetSets", "31~32"), linkResource(clientID, "customerAssetSets", "0"),
		linkResource(otherID, "customerAssetSets", "31"), linkResource(clientID, "labels", "31")} {
		if _, ok := f.idsOf(bad, clientID); ok {
			t.Errorf("idsOf accepted %s", bad)
		}
	}
	ops, names, err := f.createOperations(clientID, []any{map[string]any{"asset_set": set("31")}})
	if err != nil || len(names) != 1 || names[0] != name ||
		!reflect.DeepEqual(ops, []any{map[string]any{"create": map[string]any{"assetSet": set("31")}}}) {
		t.Fatalf("ops = %v, names = %v, err = %v", ops, names, err)
	}
	for label, items := range map[string][]any{
		"none":      nil,
		"extra":     {map[string]any{"asset_set": set("31"), "customer": "customers/" + clientID}},
		"foreign":   {map[string]any{"asset_set": linkResource(otherID, "assetSets", "31")}},
		"temporary": {map[string]any{"asset_set": set("-31")}},
		"twice":     {map[string]any{"asset_set": set("31")}, map[string]any{"asset_set": set("31")}},
	} {
		if _, _, err := f.createOperations(clientID, items); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
	if ops, err := f.removeOperations(clientID, []string{name}); err != nil || !reflect.DeepEqual(ops, []any{map[string]any{"remove": name}}) {
		t.Fatalf("remove ops = %v, %v", ops, err)
	}
	if _, err := f.removeOperations(clientID, []string{linkResource(clientID, "customerAssetSets", "31~32")}); err == nil {
		t.Error("remove accepted two IDs")
	}
	got, err := f.result("create", clientID, []listResult{{ResourceName: name,
		Resource: json.RawMessage(`{"resourceName":"` + name + `","assetSet":"` + set("31") + `","customer":"customers/` + clientID + `","status":"ENABLED"}`)}}, false)
	if err != nil {
		t.Fatal(err)
	}
	item := got["results"].([]map[string]any)[0][f.resultKey].(map[string]any)
	if item["asset_set"] != set("31") || item["status"] != "ENABLED" || item["customer"] != "customers/"+clientID {
		t.Errorf("link = %v", item)
	}
	if _, err := f.result("create", clientID, []listResult{{ResourceName: name,
		Resource: json.RawMessage(`{"assetSet":"` + set("32") + `"}`)}}, false); err == nil {
		t.Error("another asset set accepted")
	}
	if _, err := f.result("create", clientID, []listResult{{ResourceName: name,
		Resource: json.RawMessage(`{"customer":"customers/` + otherID + `"}`)}}, false); err == nil {
		t.Error("another account accepted")
	}
}

func TestCustomerAssetSetsSendTheCheckedOperations(t *testing.T) {
	f := customerAssetSetsFamily
	set := linkResource(clientID, "assetSets", "31")
	name := linkResource(clientID, "customerAssetSets", "31")
	for verb, call := range map[string]string{
		"create": `{"customer_id":"` + clientID + `","customer_asset_sets":[{"asset_set":"` + set + `"}]}`,
		"remove": `{"customer_id":"` + clientID + `","resource_names":["` + name + `"]}`,
	} {
		t.Run(verb, func(t *testing.T) {
			e := newSetLinkEnvironment(t, nil, nil)
			e.fake.routes["POST /v26/customers/"+clientID+f.service+":mutate"] = okReply(`{"results":[{"resourceName":"` + name + `"}]}`)
			out, err := e.invokeConfirmed(f.id(verb), call)
			if err != nil {
				t.Fatal(err)
			}
			want := `[{"remove":"` + name + `"}]`
			if verb == "create" {
				want = `[{"create":{"assetSet":"` + set + `"}}]`
			}
			if posts := e.posts(); len(posts) != 1 || !reflect.DeepEqual(bodyOf(t, posts[0])["operations"], wantWire(t, want)) {
				t.Fatalf("posts = %+v", posts)
			}
			if results := decode(t, out)["results"].([]any); len(results) != 1 || results[0].(map[string]any)["resource_name"] != name {
				t.Errorf("out = %s", out)
			}
		})
	}
}
