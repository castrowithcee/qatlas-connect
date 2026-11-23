package googleads

import (
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

var customizerAttributesToolIDs = []string{customizerAttributesCreateID, customizerAttributesRemoveID}

func customizerAttributesPath(id string) string {
	return "/v26/customers/" + id + "/customizerAttributes:mutate"
}

func customizerAttributeResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/customizerAttributes/" + id
}

func newCustomizerAttributesEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: customizerAttributesToolIDs})
}

func customizerAttributesCalls(customer string) map[string]string {
	return map[string]string{
		customizerAttributesCreateID: `{"customer_id":"` + customer + `","customizer_attributes":[` +
			`{"name":"Season","type":"TEXT"},{"name":"Discount","type":"PERCENT"}]}`,
		customizerAttributesRemoveID: `{"customer_id":"` + customer + `","customizer_attribute_ids":["111","222"]}`,
	}
}

func customizerAttributesAnswer(customer string) string {
	return `{"results":[{"resourceName":"` + customizerAttributeResource(customer, "111") + `"},{"resourceName":"` +
		customizerAttributeResource(customer, "222") + `"}]}`
}

func TestCustomizerAttributeNamesAreBuiltAndReadForTheAccountOnly(t *testing.T) {
	if got := customizerAttributeName(clientID, "5"); got != customizerAttributeResource(clientID, "5") {
		t.Errorf("name = %s", got)
	}
	if id, ok := customizerAttributeIDOfName(customizerAttributeResource(clientID, "5"), clientID); !ok || id != "5" {
		t.Errorf("id = %s, %v", id, ok)
	}
	for _, name := range []string{customizerAttributeResource(otherID, "5"), customizerAttributeResource(clientID, "0"),
		customizerAttributeResource(clientID, "5/6"), customizerAttributeResource(clientID, "-5"),
		"customers/" + queryNumber(clientID) + "/labels/5"} {
		if _, ok := customizerAttributeIDOfName(name, clientID); ok {
			t.Errorf("accepted %s", name)
		}
	}
}

func TestCustomizerAttributeToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, id := range customizerAttributesToolIDs {
		remove := id == customizerAttributesRemoveID
		d, _, ok := reg.Lookup(id)
		wantEffect := capability.EffectCreate
		wantNote := mutateListNote
		if remove {
			wantEffect, wantNote = capability.EffectDelete, mutateListListedNote
		}
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || d.RequiresToolAllowList != remove ||
			!strings.HasSuffix(d.Description, wantNote) || !strings.Contains(d.Description, "no update") {
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
		if _, found := input.Properties["response_content_type"]; found == remove {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
	}
	if _, _, ok := reg.Lookup(Provider + ".customizer-attributes.update"); ok {
		t.Error("customizer-attributes has an update tool")
	}
}

func TestCustomizerAttributeToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+customizerAttributesPath(clientID)] = okReply(customizerAttributesAnswer(clientID))
		_, err := e.invokeConfirmed(id, customizerAttributesCalls(clientID)[id])
		return err == nil
	}
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	if !reached(config.Connection{Effects: all}, customizerAttributesCreateID) ||
		reached(config.Connection{Effects: all}, customizerAttributesRemoveID) ||
		reached(config.Connection{Effects: []config.Effect{config.EffectRead}}, customizerAttributesCreateID) ||
		!reached(config.Connection{Tools: []string{customizerAttributesRemoveID}}, customizerAttributesRemoveID) {
		t.Error("tools are not reached as described")
	}
}

func TestCustomizerAttributesNeedConfirmationAndCheckTheTargetFirst(t *testing.T) {
	for _, id := range customizerAttributesToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCustomizerAttributesEnvironment(t, nil, nil)
			_, err := e.invoke(id, customizerAttributesCalls(clientID)[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)

			e = newCustomizerAttributesEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err = e.invokeConfirmed(id, customizerAttributesCalls(otherID)[id])
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCustomizerAttributeArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	attrs := func(items ...string) string {
		return head + `"customizer_attributes":[` + strings.Join(items, ",") + `]}`
	}
	ids := func(items ...string) string {
		return head + `"customizer_attribute_ids":[` + strings.Join(items, ",") + `]}`
	}
	var manyAttrs, manyIDs []string
	for i := 1; i <= 51; i++ {
		manyAttrs = append(manyAttrs, `{"name":"a","type":"TEXT"}`)
		manyIDs = append(manyIDs, `"`+strconv.Itoa(i)+`"`)
	}
	bad := map[string][2]string{
		"create none":         {customizerAttributesCreateID, attrs()},
		"create too many":     {customizerAttributesCreateID, attrs(manyAttrs...)},
		"create no name":      {customizerAttributesCreateID, attrs(`{"type":"TEXT"}`)},
		"create empty name":   {customizerAttributesCreateID, attrs(`{"name":"","type":"TEXT"}`)},
		"create name 41":      {customizerAttributesCreateID, attrs(`{"name":"` + strings.Repeat("a", 41) + `","type":"TEXT"}`)},
		"create line feed":    {customizerAttributesCreateID, attrs(`{"name":"a\nb","type":"TEXT"}`)},
		"create no type":      {customizerAttributesCreateID, attrs(`{"name":"a"}`)},
		"create type":         {customizerAttributesCreateID, attrs(`{"name":"a","type":"UNKNOWN"}`)},
		"create type unspec":  {customizerAttributesCreateID, attrs(`{"name":"a","type":"UNSPECIFIED"}`)},
		"create type lower":   {customizerAttributesCreateID, attrs(`{"name":"a","type":"text"}`)},
		"create status":       {customizerAttributesCreateID, attrs(`{"name":"a","type":"TEXT","status":"ENABLED"}`)},
		"create content type": {customizerAttributesCreateID, strings.TrimSuffix(attrs(`{"name":"a","type":"TEXT"}`), "}") + `,"response_content_type":"X"}`},
		"remove none":         {customizerAttributesRemoveID, ids()},
		"remove too many":     {customizerAttributesRemoveID, ids(manyIDs...)},
		"remove name":         {customizerAttributesRemoveID, ids(`"customers/1/customizerAttributes/2"`)},
		"remove zero":         {customizerAttributesRemoveID, ids(`"0"`)},
		"remove negative":     {customizerAttributesRemoveID, ids(`"-5"`)},
		"remove twice":        {customizerAttributesRemoveID, ids(`"1"`, `"1"`)},
		"remove content type": {customizerAttributesRemoveID, strings.TrimSuffix(ids(`"1"`), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":        {customizerAttributesRemoveID, `{"customer_id":"12-34","customizer_attribute_ids":["1"]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newCustomizerAttributesEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newCustomizerAttributesEnvironment(t, nil, nil)
		e.fake.routes["POST "+customizerAttributesPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
			customizerAttributeResource(clientID, "5") + `"}]}`)
		if _, err := e.invokeConfirmed(customizerAttributesCreateID, attrs(`{"name":"`+strings.Repeat("ä", 40)+`","type":"PRICE"}`)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCustomizerAttributesSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		customizerAttributesCreateID: `[{"create":{"name":"Season","type":"TEXT"}},{"create":{"name":"Discount","type":"PERCENT"}}]`,
		customizerAttributesRemoveID: `[{"remove":"customers/1000000003/customizerAttributes/111"},{"remove":"customers/1000000003/customizerAttributes/222"}]`,
	}
	for _, id := range customizerAttributesToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCustomizerAttributesEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+customizerAttributesPath(clientID)] = okReply(customizerAttributesAnswer(clientID))
			out, err := e.invokeConfirmed(id, customizerAttributesCalls(clientID)[id])
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
			got := decode(t, out)
			results := got["results"].([]any)
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
				results[0].(map[string]any)["customizer_attribute_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != customizerAttributeResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
		t.Run(id+" validate only", func(t *testing.T) {
			e := newCustomizerAttributesEnvironment(t, nil, nil)
			e.fake.routes["POST "+customizerAttributesPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(customizerAttributesCalls(clientID)[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := decode(t, out); bodyOf(t, e.posts()[0])["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestCustomizerAttributeResponseContentTypeShowsTheAttributes(t *testing.T) {
	e := newCustomizerAttributesEnvironment(t, nil, nil)
	e.fake.routes["POST "+customizerAttributesPath(clientID)] = okReply(`{"results":[{"resourceName":"` +
		customizerAttributeResource(clientID, "111") + `","customizerAttribute":{"resourceName":"` +
		customizerAttributeResource(clientID, "111") + `","id":"111","name":"Season","type":"TEXT","status":"ENABLED"}}]}`)
	out, err := e.invokeConfirmed(customizerAttributesCreateID, `{"customer_id":"`+clientID+
		`","customizer_attributes":[{"name":"Season","type":"TEXT"}],"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	attr := decode(t, out)["results"].([]any)[0].(map[string]any)["customizer_attribute"].(map[string]any)
	if attr["id"] != "111" || attr["name"] != "Season" || attr["type"] != "TEXT" || attr["status"] != "ENABLED" {
		t.Errorf("customizer attribute = %v", attr)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestCustomizerAttributeResultsAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(id string) string { return `{"resourceName":"` + customizerAttributeResource(clientID, id) + `"}` }
	shown := func(name, id string) string {
		return `{"resourceName":"` + customizerAttributeResource(clientID, "111") + `","customizerAttribute":{"resourceName":"` +
			name + `"` + id + `}}`
	}
	cases := map[string]struct{ id, body string }{
		"create foreign": {customizerAttributesCreateID, `{"results":[{"resourceName":"` + customizerAttributeResource(otherID, "111") +
			`"},` + own("222") + `]}`},
		"create none":     {customizerAttributesCreateID, `{}`},
		"create one":      {customizerAttributesCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":    {customizerAttributesCreateID, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create not attr": {customizerAttributesCreateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) + `/labels/1"},` + own("222") + `]}`},
		"create partial":  {customizerAttributesCreateID, `{"results":[` + own("111") + `,` + own("222") + `],"partialFailureError":{"code":3}}`},
		"create shown foreign": {customizerAttributesCreateID, `{"results":[` + shown(customizerAttributeResource(otherID, "111"), "") +
			`,` + own("222") + `]}`},
		"create shown id": {customizerAttributesCreateID, `{"results":[` + shown(customizerAttributeResource(clientID, "111"), `,"id":"999"`) +
			`,` + own("222") + `]}`},
		"remove other order": {customizerAttributesRemoveID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"remove foreign": {customizerAttributesRemoveID, `{"results":[{"resourceName":"` + customizerAttributeResource(otherID, "111") +
			`"},` + own("222") + `]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCustomizerAttributesEnvironment(t, nil, nil)
			e.fake.routes["POST "+customizerAttributesPath(clientID)] = okReply(c.body)
			arguments := customizerAttributesCalls(clientID)[c.id]
			if strings.Contains(name, "shown") {
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

func TestCustomizerAttributeManagerAccountsNeedAManagerTarget(t *testing.T) {
	call := `{"customer_id":"` + managerID + `","customizer_attribute_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + customizerAttributeResource(managerID, "7") + `"}]}`)
	e := newCustomizerAttributesEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.routes["POST "+customizerAttributesPath(managerID)] = answer
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	if _, err := e.invokeConfirmed(customizerAttributesRemoveID, call); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
	e = newCustomizerAttributesEnvironment(t, []string{"managers/" + managerID}, nil)
	e.fake.routes["POST "+customizerAttributesPath(managerID)] = answer
	if _, err := e.invokeConfirmed(customizerAttributesRemoveID, call); err != nil {
		t.Fatal(err)
	}
}

func TestCustomizerAttributeErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
	}{
		{"customizer error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "customizerAttributeError",
			"DUPLICATE_CUSTOMIZER_ATTRIBUTE_NAME", "")}, provider.ClassProviderError,
			"rejected the change (customizerAttributeError DUPLICATE_CUSTOMIZER_ATTRIBUTE_NAME), nothing was changed"},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests"},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown"},
	}
	for _, k := range cases {
		for _, id := range customizerAttributesToolIDs {
			t.Run(k.name+" "+id, func(t *testing.T) {
				e := newCustomizerAttributesEnvironment(t, nil, nil)
				e.fake.routes["POST "+customizerAttributesPath(clientID)] = k.reply
				_, err := e.invokeConfirmed(id, customizerAttributesCalls(clientID)[id])
				if classOf(err) != k.class || !strings.Contains(err.Error(), k.message) || len(e.posts()) != 1 ||
					len(e.fake.api()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestCustomizerAttributeSummariesNameTheEffect(t *testing.T) {
	calls := customizerAttributesCalls(clientID)
	create := customizerAttributesCreateSummary(json.RawMessage(calls[customizerAttributesCreateID]))
	remove := customizerAttributesRemoveSummary(json.RawMessage(calls[customizerAttributesRemoveID]))
	for _, word := range []string{clientID, "2 customizer attribute", `"Season" TEXT`} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	for _, word := range []string{clientID, "111, 222", "no tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	if text := customizerAttributesCreateSummary(json.RawMessage(strings.TrimSuffix(calls[customizerAttributesCreateID], "}") +
		`,"validate_only":true}`)); !strings.HasPrefix(text, "Only validate") {
		t.Errorf("summary %q", text)
	}
}
