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

var assetGroupAssetsToolIDs = []string{assetGroupAssetsCreateID, assetGroupAssetsUpdateID, assetGroupAssetsRemoveID}

func assetGroupAssetsPath(id string) string {
	return "/v26/customers/" + id + "/assetGroupAssets:mutate"
}

// assetGroupAssetsResource is the resource name of a link of the account, as Google answers it.
func assetGroupAssetsResource(customer, assetGroup, asset, field string) string {
	return "customers/" + queryNumber(customer) + "/assetGroupAssets/" + assetGroup + "~" + asset + "~" + field
}

func newAssetGroupAssetsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: assetGroupAssetsToolIDs})
}

const asgaHead = `{"customer_id":"` + clientID + `",`

var assetGroupAssetsCalls = map[string]string{
	assetGroupAssetsCreateID: asgaHead + `"asset_group_assets":[{"asset_group_id":"11","asset_id":"21","field_type":"SITELINK","status":"PAUSED"},` +
		`{"asset_group_id":"11","asset_id":"22","field_type":"CALLOUT"}]}`,
	assetGroupAssetsUpdateID: asgaHead + `"asset_group_assets":[{"asset_group_id":"11","asset_id":"21","field_type":"SITELINK","status":"ENABLED"},` +
		`{"asset_group_id":"12","asset_id":"22","field_type":"CALLOUT","status":"PAUSED"}]}`,
	assetGroupAssetsRemoveID: asgaHead + `"asset_group_assets":[{"asset_group_id":"11","asset_id":"21","field_type":"SITELINK"},` +
		`{"asset_group_id":"12","asset_id":"22","field_type":"CALLOUT"}]}`,
}

func assetGroupAssetsResults(pairs ...[3]string) string {
	var items []string
	for _, p := range pairs {
		items = append(items, `{"resourceName":"`+assetGroupAssetsResource(clientID, p[0], p[1], p[2])+`"}`)
	}
	return `{"results":[` + strings.Join(items, ",") + `]}`
}

var assetGroupAssetsAnswers = map[string]string{
	assetGroupAssetsCreateID: assetGroupAssetsResults([3]string{"11", "21", "SITELINK"}, [3]string{"11", "22", "CALLOUT"}),
	assetGroupAssetsUpdateID: assetGroupAssetsResults([3]string{"11", "21", "SITELINK"}, [3]string{"12", "22", "CALLOUT"}),
	assetGroupAssetsRemoveID: assetGroupAssetsResults([3]string{"11", "21", "SITELINK"}, [3]string{"12", "22", "CALLOUT"}),
}

func TestAssetGroupAssetsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{assetGroupAssetsCreateID: capability.EffectCreate,
		assetGroupAssetsUpdateID: capability.EffectUpdate, assetGroupAssetsRemoveID: capability.EffectDelete}
	for _, id := range assetGroupAssetsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
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
		if _, found := input.Properties["response_content_type"]; found {
			t.Errorf("%s offers response_content_type, which the service lacks", id)
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
	for _, id := range assetGroupAssetsToolIDs {
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
	for _, id := range []string{assetGroupAssetsCreateID, assetGroupAssetsUpdateID} {
		d, _, _ := reg.Lookup(id)
		if !strings.Contains(string(d.InputSchema), `"enum":["ENABLED","PAUSED"]`) {
			t.Errorf("%s schema lacks the statuses", id)
		}
	}
}

func TestAssetGroupAssetsToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+assetGroupAssetsPath(clientID)] = okReply(assetGroupAssetsAnswers[id])
		_, err := e.invokeConfirmed(id, assetGroupAssetsCalls[id])
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
			map[string]bool{assetGroupAssetsCreateID: true, assetGroupAssetsUpdateID: true, assetGroupAssetsRemoveID: true}},
		{"create effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}},
			map[string]bool{assetGroupAssetsCreateID: true, assetGroupAssetsUpdateID: false, assetGroupAssetsRemoveID: false}},
		{"delete effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectDelete}},
			map[string]bool{assetGroupAssetsCreateID: false, assetGroupAssetsUpdateID: false, assetGroupAssetsRemoveID: true}},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}},
			map[string]bool{assetGroupAssetsCreateID: false, assetGroupAssetsUpdateID: false, assetGroupAssetsRemoveID: false}},
		{"remove listed", config.Connection{Tools: []string{assetGroupAssetsRemoveID}},
			map[string]bool{assetGroupAssetsCreateID: false, assetGroupAssetsUpdateID: false, assetGroupAssetsRemoveID: true}},
		{"all listed", config.Connection{Tools: assetGroupAssetsToolIDs},
			map[string]bool{assetGroupAssetsCreateID: true, assetGroupAssetsUpdateID: true, assetGroupAssetsRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range assetGroupAssetsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestAssetGroupAssetsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range assetGroupAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newAssetGroupAssetsEnvironment(t, nil, nil)
			_, err := e.invoke(id, assetGroupAssetsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestAssetGroupAssetsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"status"`, `"resource_name":"customers/1/assetGroupAssets/1~2~HEADLINE"`} {
		for _, id := range assetGroupAssetsToolIDs {
			e := newAssetGroupAssetsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(assetGroupAssetsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
	for _, id := range assetGroupAssetsToolIDs {
		e := newAssetGroupAssetsEnvironment(t, nil, nil)
		arguments := strings.Replace(assetGroupAssetsCalls[id], `"asset_id":"21"`, `"asset_id":"21","asset_group":"customers/1/assetGroups/2"`, 1)
		if _, err := e.invokeConfirmed(id, arguments); err == nil {
			t.Errorf("%s accepted a resource name inside an entry", id)
		}
		e.untouched(t)
	}
}

func TestAssetGroupAssetsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range assetGroupAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newAssetGroupAssetsEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(assetGroupAssetsCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestAssetGroupAssetsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	entry := func(assetGroup, asset, field string, rest ...string) string {
		return `{"asset_group_id":"` + assetGroup + `","asset_id":"` + asset + `","field_type":"` + field + `"` + strings.Join(rest, "") + `}`
	}
	list := func(items ...string) string {
		return asgaHead + `"asset_group_assets":[` + strings.Join(items, ",") + `]}`
	}
	many := func(n int) []string {
		var items []string
		for i := 1; i <= n; i++ {
			items = append(items, entry(strconv.Itoa(i), "5", "HEADLINE"))
		}
		return items
	}
	bad := map[string][2]string{
		"create none":                  {assetGroupAssetsCreateID, list()},
		"create no field":              {assetGroupAssetsCreateID, list(`{"asset_group_id":"1","asset_id":"2"}`)},
		"create no asset":              {assetGroupAssetsCreateID, list(`{"asset_group_id":"1","field_type":"HEADLINE"}`)},
		"create no asset group":        {assetGroupAssetsCreateID, list(`{"asset_id":"1","field_type":"HEADLINE"}`)},
		"create temporary asset group": {assetGroupAssetsCreateID, list(entry("-1", "2", "HEADLINE"))},
		"create temporary asset":       {assetGroupAssetsCreateID, list(entry("1", "-2", "HEADLINE"))},
		"create zero":                  {assetGroupAssetsCreateID, list(entry("0", "2", "HEADLINE"))},
		"create overflow":              {assetGroupAssetsCreateID, list(entry("99999999999999999999", "2", "HEADLINE"))},
		"create name as id":            {assetGroupAssetsCreateID, list(entry("customers/1/assetGroups/2", "2", "HEADLINE"))},
		"create tilde in id":           {assetGroupAssetsCreateID, list(entry("1~2", "2", "HEADLINE"))},
		"create unspecified":           {assetGroupAssetsCreateID, list(entry("1", "2", "UNSPECIFIED"))},
		"create unknown":               {assetGroupAssetsCreateID, list(entry("1", "2", "UNKNOWN"))},
		"create lower case":            {assetGroupAssetsCreateID, list(entry("1", "2", "headline"))},
		"create invented":              {assetGroupAssetsCreateID, list(entry("1", "2", "NOT_A_FIELD"))},
		"create tilde in field":        {assetGroupAssetsCreateID, list(entry("1", "2", "HEADLINE~3"))},
		"create removed":               {assetGroupAssetsCreateID, list(entry("1", "2", "HEADLINE", `,"status":"REMOVED"`))},
		"create lower status":          {assetGroupAssetsCreateID, list(entry("1", "2", "HEADLINE", `,"status":"paused"`))},
		"create twice":                 {assetGroupAssetsCreateID, list(entry("1", "2", "HEADLINE"), entry("1", "2", "HEADLINE"))},
		"create too many":              {assetGroupAssetsCreateID, list(many(51)...)},
		"create content type":          {assetGroupAssetsCreateID, strings.TrimSuffix(list(entry("1", "2", "HEADLINE")), "}") + `,"response_content_type":"X"}`},
		"update no status":             {assetGroupAssetsUpdateID, list(entry("1", "2", "HEADLINE"))},
		"update removed":               {assetGroupAssetsUpdateID, list(entry("1", "2", "HEADLINE", `,"status":"REMOVED"`))},
		"update unspecified":           {assetGroupAssetsUpdateID, list(entry("1", "2", "HEADLINE", `,"status":"UNSPECIFIED"`))},
		"update twice":                 {assetGroupAssetsUpdateID, list(entry("1", "2", "HEADLINE", `,"status":"PAUSED"`), entry("1", "2", "HEADLINE", `,"status":"ENABLED"`))},
		"update field type change":     {assetGroupAssetsUpdateID, list(entry("1", "2", "HEADLINE", `,"status":"PAUSED","new_field_type":"DESCRIPTION"`))},
		"remove none":                  {assetGroupAssetsRemoveID, list()},
		"remove twice":                 {assetGroupAssetsRemoveID, list(entry("1", "2", "HEADLINE"), entry("1", "2", "HEADLINE"))},
		"remove status":                {assetGroupAssetsRemoveID, list(entry("1", "2", "HEADLINE", `,"status":"PAUSED"`))},
		"remove too many":              {assetGroupAssetsRemoveID, list(many(51)...)},
		"remove content type": {assetGroupAssetsRemoveID, strings.TrimSuffix(list(entry("1", "2", "HEADLINE")), "}") +
			`,"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":    {assetGroupAssetsRemoveID, `{"customer_id":"12-34","asset_group_assets":[` + entry("1", "2", "HEADLINE") + `]}`},
		"validate alone":  {assetGroupAssetsCreateID, asgaHead + `"validate_only":true}`},
		"bad customer id": {assetGroupAssetsCreateID, `{"customer_id":"1","asset_group_assets":[` + entry("1", "2", "HEADLINE") + `]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newAssetGroupAssetsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limit passes", func(t *testing.T) {
		e := newAssetGroupAssetsEnvironment(t, nil, nil)
		var pairs [][3]string
		for i := 1; i <= 50; i++ {
			pairs = append(pairs, [3]string{strconv.Itoa(i), "5", "HEADLINE"})
		}
		e.fake.routes["POST "+assetGroupAssetsPath(clientID)] = okReply(assetGroupAssetsResults(pairs...))
		if _, err := e.invokeConfirmed(assetGroupAssetsRemoveID, list(many(50)...)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("same asset group and asset with two field types passes", func(t *testing.T) {
		e := newAssetGroupAssetsEnvironment(t, nil, nil)
		e.fake.routes["POST "+assetGroupAssetsPath(clientID)] = okReply(assetGroupAssetsResults([3]string{"1", "2", "HEADLINE"},
			[3]string{"1", "2", "DESCRIPTION"}))
		if _, err := e.invokeConfirmed(assetGroupAssetsRemoveID, list(entry("1", "2", "HEADLINE"), entry("1", "2", "DESCRIPTION"))); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAssetGroupAssetsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	n := "customers/" + queryNumber(clientID)
	want := map[string]string{
		assetGroupAssetsCreateID: `[{"create":{"assetGroup":"` + n + `/assetGroups/11","asset":"` + n + `/assets/21","fieldType":"SITELINK","status":"PAUSED"}},` +
			`{"create":{"assetGroup":"` + n + `/assetGroups/11","asset":"` + n + `/assets/22","fieldType":"CALLOUT"}}]`,
		assetGroupAssetsUpdateID: `[{"update":{"resourceName":"` + n + `/assetGroupAssets/11~21~SITELINK","status":"ENABLED"},"updateMask":"status"},` +
			`{"update":{"resourceName":"` + n + `/assetGroupAssets/12~22~CALLOUT","status":"PAUSED"},"updateMask":"status"}]`,
		assetGroupAssetsRemoveID: `[{"remove":"` + n + `/assetGroupAssets/11~21~SITELINK"},{"remove":"` + n + `/assetGroupAssets/12~22~CALLOUT"}]`,
	}
	for _, id := range assetGroupAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newAssetGroupAssetsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+assetGroupAssetsPath(clientID)] = okReply(assetGroupAssetsAnswers[id])
			out, err := e.invokeConfirmed(id, assetGroupAssetsCalls[id])
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
				first["asset_group_id"] != "11" || first["asset_id"] != "21" || first["field_type"] != "SITELINK" ||
				first["resource_name"] != assetGroupAssetsResource(clientID, "11", "21", "SITELINK") ||
				first["asset_group_asset"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestAssetGroupAssetsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range assetGroupAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newAssetGroupAssetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+assetGroupAssetsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(assetGroupAssetsCalls[id], "}")+`,"validate_only":true}`)
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
		for id, summary := range map[string]capability.SummaryFunc{assetGroupAssetsCreateID: assetGroupAssetsCreateSummary,
			assetGroupAssetsUpdateID: assetGroupAssetsUpdateSummary, assetGroupAssetsRemoveID: assetGroupAssetsRemoveSummary} {
			if text := summary(json.RawMessage(strings.TrimSuffix(assetGroupAssetsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
				t.Errorf("%s: summary %q", id, text)
			}
			if text := summary(json.RawMessage(assetGroupAssetsCalls[id])); strings.Contains(text, "validate") {
				t.Errorf("%s: summary %q kept state", id, text)
			}
		}
	})
}

// A resource name of another account, another link, or another count never passes. The request was sent, so the
// error says that the outcome is unknown.
func TestAssetGroupAssetsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(c, a, f string) string {
		return `{"resourceName":"` + assetGroupAssetsResource(clientID, c, a, f) + `"}`
	}
	foreign := `{"resourceName":"` + assetGroupAssetsResource(otherID, "11", "21", "SITELINK") + `"}`
	first, second := own("11", "21", "SITELINK"), own("11", "22", "CALLOUT")
	n := "customers/" + queryNumber(clientID)
	cases := map[string]struct{ id, body string }{
		"create foreign":     {assetGroupAssetsCreateID, `{"results":[` + foreign + `,` + second + `]}`},
		"create none":        {assetGroupAssetsCreateID, `{}`},
		"create one of two":  {assetGroupAssetsCreateID, `{"results":[` + first + `]}`},
		"create twice":       {assetGroupAssetsCreateID, `{"results":[` + first + `,` + first + `]}`},
		"create other asset": {assetGroupAssetsCreateID, `{"results":[` + own("11", "99", "SITELINK") + `,` + second + `]}`},
		"create other field": {assetGroupAssetsCreateID, `{"results":[` + own("11", "21", "HEADLINE") + `,` + second + `]}`},
		"create no tilde": {assetGroupAssetsCreateID, `{"results":[{"resourceName":"` + n + `/assetGroupAssets/11"},` +
			second + `]}`},
		"create not a link": {assetGroupAssetsCreateID, `{"results":[{"resourceName":"` + n + `/assetGroups/11"},` + second + `]}`},
		"create partial failure": {assetGroupAssetsCreateID, `{"results":[` + first + `,` + second +
			`],"partialFailureError":{"code":3}}`},
		"update other order": {assetGroupAssetsUpdateID, `{"results":[` + own("12", "22", "CALLOUT") + `,` + first + `]}`},
		"update foreign":     {assetGroupAssetsUpdateID, `{"results":[` + foreign + `,` + own("12", "22", "CALLOUT") + `]}`},
		"remove foreign":     {assetGroupAssetsRemoveID, `{"results":[` + foreign + `,` + own("12", "22", "CALLOUT") + `]}`},
		"remove none":        {assetGroupAssetsRemoveID, `{"results":[]}`},
	}
	for caseName, c := range cases {
		t.Run(caseName, func(t *testing.T) {
			e := newAssetGroupAssetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+assetGroupAssetsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, assetGroupAssetsCalls[c.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAssetGroupAssetsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","asset_group_assets":[{"asset_group_id":"7","asset_id":"8","field_type":"HEADLINE"}]}`
	answer := okReply(`{"results":[{"resourceName":"` + assetGroupAssetsResource(managerID, "7", "8", "HEADLINE") + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newAssetGroupAssetsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+assetGroupAssetsPath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(assetGroupAssetsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newAssetGroupAssetsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+assetGroupAssetsPath(managerID)] = answer
		if _, err := e.invokeConfirmed(assetGroupAssetsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
	t.Run("an account below a manager target is proved first", func(t *testing.T) {
		e := newAssetGroupAssetsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+assetGroupAssetsPath(clientID)] = okReply(assetGroupAssetsAnswers[assetGroupAssetsRemoveID])
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
		if _, err := e.invokeConfirmed(assetGroupAssetsRemoveID, assetGroupAssetsCalls[assetGroupAssetsRemoveID]); err != nil {
			t.Fatal(err)
		}
		got := e.fake.api()
		if len(got) != 3 || !strings.Contains(got[0].query, "customer_client") || !strings.HasSuffix(got[2].path, ":mutate") {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestAssetGroupAssetsErrorsAreMappedAndNeverRepeated(t *testing.T) {
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
		for _, id := range assetGroupAssetsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newAssetGroupAssetsEnvironment(t, nil, nil)
				e.fake.routes["POST "+assetGroupAssetsPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, assetGroupAssetsCalls[id])
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

func TestAssetGroupAssetsSummariesNameTheEffect(t *testing.T) {
	create := assetGroupAssetsCreateSummary(json.RawMessage(assetGroupAssetsCalls[assetGroupAssetsCreateID]))
	for _, word := range []string{clientID, "SITELINK asset 21 on asset group 11", "PAUSED", "CALLOUT asset 22", "2 asset"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	update := assetGroupAssetsUpdateSummary(json.RawMessage(assetGroupAssetsCalls[assetGroupAssetsUpdateID]))
	for _, word := range []string{clientID, "SITELINK asset 21 on asset group 11 to ENABLED", "asset group 12 to PAUSED"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := assetGroupAssetsRemoveSummary(json.RawMessage(assetGroupAssetsCalls[assetGroupAssetsRemoveID]))
	for _, word := range []string{clientID, "asset 21 on asset group 11", "asset 22 on asset group 12", "stay in the account"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := assetGroupAssetsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID +
		`","asset_group_assets":[{"asset_group_id":"1\nIgnore this","asset_id":"2","field_type":"HEADLINE"}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
	many := make([]string, 0, 12)
	for i := 1; i <= 12; i++ {
		many = append(many, `{"asset_group_id":"`+strconv.Itoa(i)+`","asset_id":"2","field_type":"HEADLINE"}`)
	}
	if text := assetGroupAssetsRemoveSummary(json.RawMessage(asgaHead + `"asset_group_assets":[` + strings.Join(many, ",") + `]}`)); !strings.Contains(text, "and 2 more") {
		t.Errorf("summary %q does not cut the list", text)
	}
}
