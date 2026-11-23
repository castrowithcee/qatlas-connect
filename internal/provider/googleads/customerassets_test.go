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

var customerAssetsToolIDs = []string{customerAssetsCreateID, customerAssetsUpdateID, customerAssetsRemoveID}

func customerAssetsPath(id string) string { return "/v26/customers/" + id + "/customerAssets:mutate" }

// customerAssetsResource is the resource name of a link of the account, as Google answers it.
func customerAssetsResource(customer, asset, field string) string {
	return "customers/" + queryNumber(customer) + "/customerAssets/" + asset + "~" + field
}

func newCustomerAssetsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: customerAssetsToolIDs})
}

const cuHead = `{"customer_id":"` + clientID + `",`

var customerAssetsCalls = map[string]string{
	customerAssetsCreateID: cuHead + `"customer_assets":[{"asset_id":"21","field_type":"SITELINK","status":"PAUSED"},` +
		`{"asset_id":"22","field_type":"CALLOUT"}]}`,
	customerAssetsUpdateID: cuHead + `"customer_assets":[{"asset_id":"21","field_type":"SITELINK","status":"ENABLED"},` +
		`{"asset_id":"22","field_type":"CALLOUT","status":"PAUSED"}]}`,
	customerAssetsRemoveID: cuHead + `"customer_assets":[{"asset_id":"21","field_type":"SITELINK"},` +
		`{"asset_id":"22","field_type":"CALLOUT"}]}`,
}

func customerAssetsResults(pairs ...[2]string) string {
	var items []string
	for _, p := range pairs {
		items = append(items, `{"resourceName":"`+customerAssetsResource(clientID, p[0], p[1])+`"}`)
	}
	return `{"results":[` + strings.Join(items, ",") + `]}`
}

var customerAssetsAnswers = map[string]string{
	customerAssetsCreateID: customerAssetsResults([2]string{"21", "SITELINK"}, [2]string{"22", "CALLOUT"}),
	customerAssetsUpdateID: customerAssetsResults([2]string{"21", "SITELINK"}, [2]string{"22", "CALLOUT"}),
	customerAssetsRemoveID: customerAssetsResults([2]string{"21", "SITELINK"}, [2]string{"22", "CALLOUT"}),
}

func TestCustomerAssetsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{customerAssetsCreateID: capability.EffectCreate,
		customerAssetsUpdateID: capability.EffectUpdate, customerAssetsRemoveID: capability.EffectDelete}
	for _, id := range customerAssetsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		remove := id == customerAssetsRemoveID
		if d.RequiresToolAllowList {
			t.Errorf("%s: tools list required", id)
		}
		if !strings.HasSuffix(d.Description, mutateListNote) || strings.HasSuffix(d.Description, mutateListListedNote) {
			t.Errorf("%s: description does not end on the note of how it is offered", id)
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
			"resource_name", "update_mask"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if _, found := input.Properties["response_content_type"]; found == remove {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		if !strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "never repeats") ||
			!strings.Contains(d.Description, "atomic") {
			t.Errorf("%s: description lacks the unknown outcome: %s", id, d.Description)
		}
		for _, tool := range metadata.Tools {
			if tool.ID == id && (string(tool.Effect) != string(wantEffect[id]) || tool.RequiresToolAllowList) {
				t.Errorf("tool %+v", tool)
			}
		}
	}
	for _, id := range customerAssetsToolIDs {
		d, _, _ := reg.Lookup(id)
		for _, text := range []string{`"maxItems":50`, `"HEADLINE"`, `"SITELINK"`} {
			if !strings.Contains(string(d.InputSchema), text) {
				t.Errorf("%s schema lacks %s", id, text)
			}
		}
		for _, text := range []string{`"UNSPECIFIED"`, `"UNKNOWN"`} {
			if strings.Contains(string(d.InputSchema), text) {
				t.Errorf("%s schema offers %s", id, text)
			}
		}
	}
	for _, id := range []string{customerAssetsCreateID, customerAssetsUpdateID} {
		d, _, _ := reg.Lookup(id)
		if !strings.Contains(string(d.InputSchema), `"enum":["ENABLED","PAUSED"]`) {
			t.Errorf("%s schema lacks the statuses", id)
		}
	}
}

func TestCustomerAssetsToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+customerAssetsPath(clientID)] = okReply(customerAssetsAnswers[id])
		_, err := e.invokeConfirmed(id, customerAssetsCalls[id])
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
		{"all effects, no tools list", config.Connection{Effects: all},
			map[string]bool{customerAssetsCreateID: true, customerAssetsUpdateID: true, customerAssetsRemoveID: true}},
		{"create effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}},
			map[string]bool{customerAssetsCreateID: true, customerAssetsUpdateID: false, customerAssetsRemoveID: false}},
		{"delete effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectDelete}},
			map[string]bool{customerAssetsCreateID: false, customerAssetsUpdateID: false, customerAssetsRemoveID: true}},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}},
			map[string]bool{customerAssetsCreateID: false, customerAssetsUpdateID: false, customerAssetsRemoveID: false}},
		{"remove listed", config.Connection{Tools: []string{customerAssetsRemoveID}},
			map[string]bool{customerAssetsCreateID: false, customerAssetsUpdateID: false, customerAssetsRemoveID: true}},
		{"all listed", config.Connection{Tools: customerAssetsToolIDs},
			map[string]bool{customerAssetsCreateID: true, customerAssetsUpdateID: true, customerAssetsRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range customerAssetsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestCustomerAssetsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range customerAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCustomerAssetsEnvironment(t, nil, nil)
			_, err := e.invoke(id, customerAssetsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestCustomerAssetsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"status"`, `"resource_name":"customers/1/customerAssets/1~2~HEADLINE"`} {
		for _, id := range customerAssetsToolIDs {
			e := newCustomerAssetsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(customerAssetsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
	for _, id := range customerAssetsToolIDs {
		e := newCustomerAssetsEnvironment(t, nil, nil)
		arguments := strings.Replace(customerAssetsCalls[id], `"asset_id":"21"`, `"asset_id":"21","asset":"customers/1/assets/2"`, 1)
		if _, err := e.invokeConfirmed(id, arguments); err == nil {
			t.Errorf("%s accepted a resource name inside an entry", id)
		}
		e.untouched(t)
	}
}

func TestCustomerAssetsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range customerAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCustomerAssetsEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(customerAssetsCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCustomerAssetsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	entry := func(asset, field string, rest ...string) string {
		return `{"asset_id":"` + asset + `","field_type":"` + field + `"` + strings.Join(rest, "") + `}`
	}
	list := func(items ...string) string { return cuHead + `"customer_assets":[` + strings.Join(items, ",") + `]}` }
	many := func(n int) []string {
		var items []string
		for i := 1; i <= n; i++ {
			items = append(items, entry(strconv.Itoa(i), "HEADLINE"))
		}
		return items
	}
	bad := map[string][2]string{
		"create none":              {customerAssetsCreateID, list()},
		"create no field":          {customerAssetsCreateID, list(`{"asset_id":"2"}`)},
		"create no asset":          {customerAssetsCreateID, list(`{"field_type":"HEADLINE"}`)},
		"create campaign":          {customerAssetsCreateID, list(`{"campaign_id":"1","asset_id":"2","field_type":"HEADLINE"}`)},
		"create temporary asset":   {customerAssetsCreateID, list(entry("-2", "HEADLINE"))},
		"create zero":              {customerAssetsCreateID, list(entry("0", "HEADLINE"))},
		"create overflow":          {customerAssetsCreateID, list(entry("99999999999999999999", "HEADLINE"))},
		"create name as id":        {customerAssetsCreateID, list(entry("customers/1/assets/2", "HEADLINE"))},
		"create tilde in id":       {customerAssetsCreateID, list(entry("2~3", "HEADLINE"))},
		"create unspecified":       {customerAssetsCreateID, list(entry("2", "UNSPECIFIED"))},
		"create unknown":           {customerAssetsCreateID, list(entry("2", "UNKNOWN"))},
		"create lower case":        {customerAssetsCreateID, list(entry("2", "headline"))},
		"create invented":          {customerAssetsCreateID, list(entry("2", "NOT_A_FIELD"))},
		"create tilde in field":    {customerAssetsCreateID, list(entry("2", "HEADLINE~3"))},
		"create removed":           {customerAssetsCreateID, list(entry("2", "HEADLINE", `,"status":"REMOVED"`))},
		"create lower status":      {customerAssetsCreateID, list(entry("2", "HEADLINE", `,"status":"paused"`))},
		"create twice":             {customerAssetsCreateID, list(entry("2", "HEADLINE"), entry("2", "HEADLINE"))},
		"create too many":          {customerAssetsCreateID, list(many(51)...)},
		"create content type":      {customerAssetsCreateID, strings.TrimSuffix(list(entry("2", "HEADLINE")), "}") + `,"response_content_type":"X"}`},
		"update no status":         {customerAssetsUpdateID, list(entry("2", "HEADLINE"))},
		"update removed":           {customerAssetsUpdateID, list(entry("2", "HEADLINE", `,"status":"REMOVED"`))},
		"update unspecified":       {customerAssetsUpdateID, list(entry("2", "HEADLINE", `,"status":"UNSPECIFIED"`))},
		"update twice":             {customerAssetsUpdateID, list(entry("2", "HEADLINE", `,"status":"PAUSED"`), entry("2", "HEADLINE", `,"status":"ENABLED"`))},
		"update field type change": {customerAssetsUpdateID, list(entry("2", "HEADLINE", `,"status":"PAUSED","new_field_type":"DESCRIPTION"`))},
		"remove none":              {customerAssetsRemoveID, list()},
		"remove twice":             {customerAssetsRemoveID, list(entry("2", "HEADLINE"), entry("2", "HEADLINE"))},
		"remove status":            {customerAssetsRemoveID, list(entry("2", "HEADLINE", `,"status":"PAUSED"`))},
		"remove too many":          {customerAssetsRemoveID, list(many(51)...)},
		"remove content type": {customerAssetsRemoveID, strings.TrimSuffix(list(entry("2", "HEADLINE")), "}") +
			`,"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":    {customerAssetsRemoveID, `{"customer_id":"12-34","customer_assets":[` + entry("2", "HEADLINE") + `]}`},
		"validate alone":  {customerAssetsCreateID, cuHead + `"validate_only":true}`},
		"bad customer id": {customerAssetsCreateID, `{"customer_id":"1","customer_assets":[` + entry("2", "HEADLINE") + `]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newCustomerAssetsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limit passes", func(t *testing.T) {
		e := newCustomerAssetsEnvironment(t, nil, nil)
		var pairs [][2]string
		for i := 1; i <= 50; i++ {
			pairs = append(pairs, [2]string{strconv.Itoa(i), "HEADLINE"})
		}
		e.fake.routes["POST "+customerAssetsPath(clientID)] = okReply(customerAssetsResults(pairs...))
		if _, err := e.invokeConfirmed(customerAssetsRemoveID, list(many(50)...)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("same asset with two field types passes", func(t *testing.T) {
		e := newCustomerAssetsEnvironment(t, nil, nil)
		e.fake.routes["POST "+customerAssetsPath(clientID)] = okReply(customerAssetsResults([2]string{"2", "HEADLINE"},
			[2]string{"2", "DESCRIPTION"}))
		if _, err := e.invokeConfirmed(customerAssetsRemoveID, list(entry("2", "HEADLINE"), entry("2", "DESCRIPTION"))); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCustomerAssetsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	n := "customers/" + queryNumber(clientID)
	want := map[string]string{
		customerAssetsCreateID: `[{"create":{"asset":"` + n + `/assets/21","fieldType":"SITELINK","status":"PAUSED"}},` +
			`{"create":{"asset":"` + n + `/assets/22","fieldType":"CALLOUT"}}]`,
		customerAssetsUpdateID: `[{"update":{"resourceName":"` + n + `/customerAssets/21~SITELINK","status":"ENABLED"},"updateMask":"status"},` +
			`{"update":{"resourceName":"` + n + `/customerAssets/22~CALLOUT","status":"PAUSED"},"updateMask":"status"}]`,
		customerAssetsRemoveID: `[{"remove":"` + n + `/customerAssets/21~SITELINK"},{"remove":"` + n + `/customerAssets/22~CALLOUT"}]`,
	}
	for _, id := range customerAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCustomerAssetsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+customerAssetsPath(clientID)] = okReply(customerAssetsAnswers[id])
			out, err := e.invokeConfirmed(id, customerAssetsCalls[id])
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
				first["asset_id"] != "21" || first["field_type"] != "SITELINK" ||
				first["resource_name"] != customerAssetsResource(clientID, "21", "SITELINK") ||
				first["customer_asset"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestCustomerAssetsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range customerAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCustomerAssetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+customerAssetsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(customerAssetsCalls[id], "}")+`,"validate_only":true}`)
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
		for id, summary := range map[string]capability.SummaryFunc{customerAssetsCreateID: customerAssetsCreateSummary,
			customerAssetsUpdateID: customerAssetsUpdateSummary, customerAssetsRemoveID: customerAssetsRemoveSummary} {
			if text := summary(json.RawMessage(strings.TrimSuffix(customerAssetsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
				t.Errorf("%s: summary %q", id, text)
			}
			if text := summary(json.RawMessage(customerAssetsCalls[id])); strings.Contains(text, "validate") {
				t.Errorf("%s: summary %q kept state", id, text)
			}
		}
	})
}

func TestCustomerAssetsResponseContentTypeShowsTheLinks(t *testing.T) {
	name := customerAssetsResource(clientID, "21", "SITELINK")
	e := newCustomerAssetsEnvironment(t, nil, nil)
	e.fake.routes["POST "+customerAssetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + name +
		`","customerAsset":{"resourceName":"` + name + `",` +
		`"asset":"customers/` + queryNumber(clientID) + `/assets/21","fieldType":"SITELINK","status":"PAUSED",` +
		`"source":"ADVERTISER","primaryStatus":"PAUSED","primaryStatusReasons":["ASSET_LINK_PAUSED","ASSET_DISAPPROVED"],` +
		`"primaryStatusDetails":[{"status":"NOT_ELIGIBLE","reason":"ASSET_DISAPPROVED","assetDisapproved":` +
		`{"offlineEvaluationErrorReasons":["LANDING_PAGE\u0007X"]}}]}}]}`)
	out, err := e.invokeConfirmed(customerAssetsUpdateID, cuHead+`"customer_assets":[{"asset_id":"21",`+
		`"field_type":"SITELINK","status":"PAUSED"}],"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	link := decode(t, out)["results"].([]any)[0].(map[string]any)["customer_asset"].(map[string]any)
	details := link["primary_status_details"].([]any)[0].(map[string]any)
	reasons := details["asset_disapproved"].(map[string]any)["offline_evaluation_error_reasons"].([]any)
	if link["asset"] != "customers/"+queryNumber(clientID)+"/assets/21" || link["field_type"] != "SITELINK" ||
		link["status"] != "PAUSED" || link["source"] != "ADVERTISER" || link["primary_status"] != "PAUSED" ||
		len(link["primary_status_reasons"].([]any)) != 2 || details["status"] != "NOT_ELIGIBLE" ||
		details["reason"] != "ASSET_DISAPPROVED" || len(reasons) != 1 || strings.ContainsAny(reasons[0].(string), "\u0007") {
		t.Errorf("link = %v", link)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// A resource name of another account, another link, or another count never passes. The request was sent, so the
// error says that the outcome is unknown.
func TestCustomerAssetsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(a, f string) string {
		return `{"resourceName":"` + customerAssetsResource(clientID, a, f) + `"}`
	}
	foreign := `{"resourceName":"` + customerAssetsResource(otherID, "21", "SITELINK") + `"}`
	first, second := own("21", "SITELINK"), own("22", "CALLOUT")
	linkOf := func(extra string) string {
		return `{"resourceName":"` + customerAssetsResource(clientID, "21", "SITELINK") + `","customerAsset":{` + extra + `}}`
	}
	name := `"resourceName":"` + customerAssetsResource(clientID, "21", "SITELINK") + `"`
	n := "customers/" + queryNumber(clientID)
	cases := map[string]struct{ id, body string }{
		"create foreign":     {customerAssetsCreateID, `{"results":[` + foreign + `,` + second + `]}`},
		"create none":        {customerAssetsCreateID, `{}`},
		"create one of two":  {customerAssetsCreateID, `{"results":[` + first + `]}`},
		"create twice":       {customerAssetsCreateID, `{"results":[` + first + `,` + first + `]}`},
		"create other asset": {customerAssetsCreateID, `{"results":[` + own("99", "SITELINK") + `,` + second + `]}`},
		"create other field": {customerAssetsCreateID, `{"results":[` + own("21", "HEADLINE") + `,` + second + `]}`},
		"create with a parent": {customerAssetsCreateID, `{"results":[{"resourceName":"` + n + `/customerAssets/1~21~SITELINK"},` +
			second + `]}`},
		"create no tilde": {customerAssetsCreateID, `{"results":[{"resourceName":"` + n + `/customerAssets/21"},` +
			second + `]}`},
		"create not a link": {customerAssetsCreateID, `{"results":[{"resourceName":"` + n + `/assets/21"},` + second + `]}`},
		"create partial failure": {customerAssetsCreateID, `{"results":[` + first + `,` + second +
			`],"partialFailureError":{"code":3}}`},
		"update other order": {customerAssetsUpdateID, `{"results":[` + own("22", "CALLOUT") + `,` + first + `]}`},
		"update foreign":     {customerAssetsUpdateID, `{"results":[` + foreign + `,` + own("22", "CALLOUT") + `]}`},
		"update link name": {customerAssetsUpdateID, `{"results":[` + linkOf(`"resourceName":"`+customerAssetsResource(clientID, "22", "SITELINK")+`"`) +
			`,` + own("22", "CALLOUT") + `]}`},
		"update link asset": {customerAssetsUpdateID, `{"results":[` + linkOf(name+`,"asset":"`+n+`/assets/99"`) +
			`,` + own("22", "CALLOUT") + `]}`},
		"update link field": {customerAssetsUpdateID, `{"results":[` + linkOf(name+`,"fieldType":"CALLOUT"`) +
			`,` + own("22", "CALLOUT") + `]}`},
		"update link unreadable": {customerAssetsUpdateID, `{"results":[` + linkOf(name+`,"asset":7`) +
			`,` + own("22", "CALLOUT") + `]}`},
		"remove foreign": {customerAssetsRemoveID, `{"results":[` + foreign + `,` + own("22", "CALLOUT") + `]}`},
		"remove none":    {customerAssetsRemoveID, `{"results":[]}`},
	}
	for caseName, c := range cases {
		t.Run(caseName, func(t *testing.T) {
			e := newCustomerAssetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+customerAssetsPath(clientID)] = okReply(c.body)
			arguments := customerAssetsCalls[c.id]
			if strings.HasPrefix(caseName, "update link") {
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

func TestCustomerAssetsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","customer_assets":[{"asset_id":"8","field_type":"HEADLINE"}]}`
	answer := okReply(`{"results":[{"resourceName":"` + customerAssetsResource(managerID, "8", "HEADLINE") + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newCustomerAssetsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+customerAssetsPath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(customerAssetsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newCustomerAssetsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+customerAssetsPath(managerID)] = answer
		if _, err := e.invokeConfirmed(customerAssetsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
	t.Run("an account below a manager target is proved first", func(t *testing.T) {
		e := newCustomerAssetsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+customerAssetsPath(clientID)] = okReply(customerAssetsAnswers[customerAssetsRemoveID])
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
		if _, err := e.invokeConfirmed(customerAssetsRemoveID, customerAssetsCalls[customerAssetsRemoveID]); err != nil {
			t.Fatal(err)
		}
		got := e.fake.api()
		if len(got) != 3 || !strings.Contains(got[0].query, "customer_client") || !strings.HasSuffix(got[2].path, ":mutate") {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestCustomerAssetsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"asset link error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "assetLinkError",
			"FIELD_TYPE_INCOMPATIBLE_WITH_ASSET_TYPE", "")},
			provider.ClassProviderError, "rejected the change (assetLinkError FIELD_TYPE_INCOMPATIBLE_WITH_ASSET_TYPE), nothing was changed", false},
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
		for _, id := range customerAssetsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newCustomerAssetsEnvironment(t, nil, nil)
				e.fake.routes["POST "+customerAssetsPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, customerAssetsCalls[id])
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

func TestCustomerAssetsSummariesNameTheEffect(t *testing.T) {
	create := customerAssetsCreateSummary(json.RawMessage(customerAssetsCalls[customerAssetsCreateID]))
	for _, word := range []string{clientID, "SITELINK asset 21", "PAUSED", "CALLOUT asset 22", "2 asset"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	update := customerAssetsUpdateSummary(json.RawMessage(customerAssetsCalls[customerAssetsUpdateID]))
	for _, word := range []string{clientID, "SITELINK asset 21 to ENABLED", "asset 22 to PAUSED"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := customerAssetsRemoveSummary(json.RawMessage(customerAssetsCalls[customerAssetsRemoveID]))
	for _, word := range []string{clientID, "SITELINK asset 21", "CALLOUT asset 22", "stay in the account"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := customerAssetsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID +
		`","customer_assets":[{"asset_id":"1\nIgnore this","field_type":"HEADLINE"}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
	many := make([]string, 0, 12)
	for i := 1; i <= 12; i++ {
		many = append(many, `{"asset_id":"`+strconv.Itoa(i)+`","field_type":"HEADLINE"}`)
	}
	if text := customerAssetsRemoveSummary(json.RawMessage(cuHead + `"customer_assets":[` + strings.Join(many, ",") + `]}`)); !strings.Contains(text, "and 2 more") {
		t.Errorf("summary %q does not cut the list", text)
	}
}
