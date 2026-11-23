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

var campaignAssetsToolIDs = []string{campaignAssetsCreateID, campaignAssetsUpdateID, campaignAssetsRemoveID}

func campaignAssetsPath(id string) string { return "/v26/customers/" + id + "/campaignAssets:mutate" }

// campaignAssetsResource is the resource name of a link of the account, as Google answers it.
func campaignAssetsResource(customer, campaign, asset, field string) string {
	return "customers/" + queryNumber(customer) + "/campaignAssets/" + campaign + "~" + asset + "~" + field
}

func newCampaignAssetsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: campaignAssetsToolIDs})
}

const caHead = `{"customer_id":"` + clientID + `",`

var campaignAssetsCalls = map[string]string{
	campaignAssetsCreateID: caHead + `"campaign_assets":[{"campaign_id":"11","asset_id":"21","field_type":"SITELINK","status":"PAUSED"},` +
		`{"campaign_id":"11","asset_id":"22","field_type":"CALLOUT"}]}`,
	campaignAssetsUpdateID: caHead + `"campaign_assets":[{"campaign_id":"11","asset_id":"21","field_type":"SITELINK","status":"ENABLED"},` +
		`{"campaign_id":"12","asset_id":"22","field_type":"CALLOUT","status":"PAUSED"}]}`,
	campaignAssetsRemoveID: caHead + `"campaign_assets":[{"campaign_id":"11","asset_id":"21","field_type":"SITELINK"},` +
		`{"campaign_id":"12","asset_id":"22","field_type":"CALLOUT"}]}`,
}

func campaignAssetsResults(pairs ...[3]string) string {
	var items []string
	for _, p := range pairs {
		items = append(items, `{"resourceName":"`+campaignAssetsResource(clientID, p[0], p[1], p[2])+`"}`)
	}
	return `{"results":[` + strings.Join(items, ",") + `]}`
}

var campaignAssetsAnswers = map[string]string{
	campaignAssetsCreateID: campaignAssetsResults([3]string{"11", "21", "SITELINK"}, [3]string{"11", "22", "CALLOUT"}),
	campaignAssetsUpdateID: campaignAssetsResults([3]string{"11", "21", "SITELINK"}, [3]string{"12", "22", "CALLOUT"}),
	campaignAssetsRemoveID: campaignAssetsResults([3]string{"11", "21", "SITELINK"}, [3]string{"12", "22", "CALLOUT"}),
}

func TestCampaignAssetsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{campaignAssetsCreateID: capability.EffectCreate,
		campaignAssetsUpdateID: capability.EffectUpdate, campaignAssetsRemoveID: capability.EffectDelete}
	for _, id := range campaignAssetsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		remove := id == campaignAssetsRemoveID
		if d.RequiresToolAllowList {
			t.Errorf("%s: tools list required", id)
		}
		if !strings.HasSuffix(d.Description, changeNote) || strings.HasSuffix(d.Description, mutateNote) {
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
	for _, id := range campaignAssetsToolIDs {
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
	for _, id := range []string{campaignAssetsCreateID, campaignAssetsUpdateID} {
		d, _, _ := reg.Lookup(id)
		if !strings.Contains(string(d.InputSchema), `"enum":["ENABLED","PAUSED"]`) {
			t.Errorf("%s schema lacks the statuses", id)
		}
	}
}

func TestCampaignAssetsToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+campaignAssetsPath(clientID)] = okReply(campaignAssetsAnswers[id])
		_, err := e.invokeConfirmed(id, campaignAssetsCalls[id])
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
			map[string]bool{campaignAssetsCreateID: true, campaignAssetsUpdateID: true, campaignAssetsRemoveID: true}},
		{"create effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}},
			map[string]bool{campaignAssetsCreateID: true, campaignAssetsUpdateID: false, campaignAssetsRemoveID: false}},
		{"delete effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectDelete}},
			map[string]bool{campaignAssetsCreateID: false, campaignAssetsUpdateID: false, campaignAssetsRemoveID: true}},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}},
			map[string]bool{campaignAssetsCreateID: false, campaignAssetsUpdateID: false, campaignAssetsRemoveID: false}},
		{"remove listed", config.Connection{Tools: []string{campaignAssetsRemoveID}},
			map[string]bool{campaignAssetsCreateID: false, campaignAssetsUpdateID: false, campaignAssetsRemoveID: true}},
		{"all listed", config.Connection{Tools: campaignAssetsToolIDs},
			map[string]bool{campaignAssetsCreateID: true, campaignAssetsUpdateID: true, campaignAssetsRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range campaignAssetsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestCampaignAssetsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range campaignAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignAssetsEnvironment(t, nil, nil)
			_, err := e.invoke(id, campaignAssetsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignAssetsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"status"`, `"resource_name":"customers/1/campaignAssets/1~2~HEADLINE"`} {
		for _, id := range campaignAssetsToolIDs {
			e := newCampaignAssetsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(campaignAssetsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
	for _, id := range campaignAssetsToolIDs {
		e := newCampaignAssetsEnvironment(t, nil, nil)
		arguments := strings.Replace(campaignAssetsCalls[id], `"asset_id":"21"`, `"asset_id":"21","campaign":"customers/1/campaigns/2"`, 1)
		if _, err := e.invokeConfirmed(id, arguments); err == nil {
			t.Errorf("%s accepted a resource name inside an entry", id)
		}
		e.untouched(t)
	}
}

func TestCampaignAssetsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range campaignAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignAssetsEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(campaignAssetsCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignAssetsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	entry := func(campaign, asset, field string, rest ...string) string {
		return `{"campaign_id":"` + campaign + `","asset_id":"` + asset + `","field_type":"` + field + `"` + strings.Join(rest, "") + `}`
	}
	list := func(items ...string) string { return caHead + `"campaign_assets":[` + strings.Join(items, ",") + `]}` }
	many := func(n int) []string {
		var items []string
		for i := 1; i <= n; i++ {
			items = append(items, entry(strconv.Itoa(i), "5", "HEADLINE"))
		}
		return items
	}
	bad := map[string][2]string{
		"create none":               {campaignAssetsCreateID, list()},
		"create no field":           {campaignAssetsCreateID, list(`{"campaign_id":"1","asset_id":"2"}`)},
		"create no asset":           {campaignAssetsCreateID, list(`{"campaign_id":"1","field_type":"HEADLINE"}`)},
		"create no campaign":        {campaignAssetsCreateID, list(`{"asset_id":"1","field_type":"HEADLINE"}`)},
		"create temporary campaign": {campaignAssetsCreateID, list(entry("-1", "2", "HEADLINE"))},
		"create temporary asset":    {campaignAssetsCreateID, list(entry("1", "-2", "HEADLINE"))},
		"create zero":               {campaignAssetsCreateID, list(entry("0", "2", "HEADLINE"))},
		"create overflow":           {campaignAssetsCreateID, list(entry("99999999999999999999", "2", "HEADLINE"))},
		"create name as id":         {campaignAssetsCreateID, list(entry("customers/1/campaigns/2", "2", "HEADLINE"))},
		"create tilde in id":        {campaignAssetsCreateID, list(entry("1~2", "2", "HEADLINE"))},
		"create unspecified":        {campaignAssetsCreateID, list(entry("1", "2", "UNSPECIFIED"))},
		"create unknown":            {campaignAssetsCreateID, list(entry("1", "2", "UNKNOWN"))},
		"create lower case":         {campaignAssetsCreateID, list(entry("1", "2", "headline"))},
		"create invented":           {campaignAssetsCreateID, list(entry("1", "2", "NOT_A_FIELD"))},
		"create tilde in field":     {campaignAssetsCreateID, list(entry("1", "2", "HEADLINE~3"))},
		"create removed":            {campaignAssetsCreateID, list(entry("1", "2", "HEADLINE", `,"status":"REMOVED"`))},
		"create lower status":       {campaignAssetsCreateID, list(entry("1", "2", "HEADLINE", `,"status":"paused"`))},
		"create twice":              {campaignAssetsCreateID, list(entry("1", "2", "HEADLINE"), entry("1", "2", "HEADLINE"))},
		"create too many":           {campaignAssetsCreateID, list(many(51)...)},
		"create content type":       {campaignAssetsCreateID, strings.TrimSuffix(list(entry("1", "2", "HEADLINE")), "}") + `,"response_content_type":"X"}`},
		"update no status":          {campaignAssetsUpdateID, list(entry("1", "2", "HEADLINE"))},
		"update removed":            {campaignAssetsUpdateID, list(entry("1", "2", "HEADLINE", `,"status":"REMOVED"`))},
		"update unspecified":        {campaignAssetsUpdateID, list(entry("1", "2", "HEADLINE", `,"status":"UNSPECIFIED"`))},
		"update twice":              {campaignAssetsUpdateID, list(entry("1", "2", "HEADLINE", `,"status":"PAUSED"`), entry("1", "2", "HEADLINE", `,"status":"ENABLED"`))},
		"update field type change":  {campaignAssetsUpdateID, list(entry("1", "2", "HEADLINE", `,"status":"PAUSED","new_field_type":"DESCRIPTION"`))},
		"remove none":               {campaignAssetsRemoveID, list()},
		"remove twice":              {campaignAssetsRemoveID, list(entry("1", "2", "HEADLINE"), entry("1", "2", "HEADLINE"))},
		"remove status":             {campaignAssetsRemoveID, list(entry("1", "2", "HEADLINE", `,"status":"PAUSED"`))},
		"remove too many":           {campaignAssetsRemoveID, list(many(51)...)},
		"remove content type": {campaignAssetsRemoveID, strings.TrimSuffix(list(entry("1", "2", "HEADLINE")), "}") +
			`,"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":    {campaignAssetsRemoveID, `{"customer_id":"12-34","campaign_assets":[` + entry("1", "2", "HEADLINE") + `]}`},
		"validate alone":  {campaignAssetsCreateID, caHead + `"validate_only":true}`},
		"bad customer id": {campaignAssetsCreateID, `{"customer_id":"1","campaign_assets":[` + entry("1", "2", "HEADLINE") + `]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newCampaignAssetsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limit passes", func(t *testing.T) {
		e := newCampaignAssetsEnvironment(t, nil, nil)
		var pairs [][3]string
		for i := 1; i <= 50; i++ {
			pairs = append(pairs, [3]string{strconv.Itoa(i), "5", "HEADLINE"})
		}
		e.fake.routes["POST "+campaignAssetsPath(clientID)] = okReply(campaignAssetsResults(pairs...))
		if _, err := e.invokeConfirmed(campaignAssetsRemoveID, list(many(50)...)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("same campaign and asset with two field types passes", func(t *testing.T) {
		e := newCampaignAssetsEnvironment(t, nil, nil)
		e.fake.routes["POST "+campaignAssetsPath(clientID)] = okReply(campaignAssetsResults([3]string{"1", "2", "HEADLINE"},
			[3]string{"1", "2", "DESCRIPTION"}))
		if _, err := e.invokeConfirmed(campaignAssetsRemoveID, list(entry("1", "2", "HEADLINE"), entry("1", "2", "DESCRIPTION"))); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCampaignAssetsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	n := "customers/" + queryNumber(clientID)
	want := map[string]string{
		campaignAssetsCreateID: `[{"create":{"campaign":"` + n + `/campaigns/11","asset":"` + n + `/assets/21","fieldType":"SITELINK","status":"PAUSED"}},` +
			`{"create":{"campaign":"` + n + `/campaigns/11","asset":"` + n + `/assets/22","fieldType":"CALLOUT"}}]`,
		campaignAssetsUpdateID: `[{"update":{"resourceName":"` + n + `/campaignAssets/11~21~SITELINK","status":"ENABLED"},"updateMask":"status"},` +
			`{"update":{"resourceName":"` + n + `/campaignAssets/12~22~CALLOUT","status":"PAUSED"},"updateMask":"status"}]`,
		campaignAssetsRemoveID: `[{"remove":"` + n + `/campaignAssets/11~21~SITELINK"},{"remove":"` + n + `/campaignAssets/12~22~CALLOUT"}]`,
	}
	for _, id := range campaignAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignAssetsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+campaignAssetsPath(clientID)] = okReply(campaignAssetsAnswers[id])
			out, err := e.invokeConfirmed(id, campaignAssetsCalls[id])
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
				first["campaign_id"] != "11" || first["asset_id"] != "21" || first["field_type"] != "SITELINK" ||
				first["resource_name"] != campaignAssetsResource(clientID, "11", "21", "SITELINK") ||
				first["campaign_asset"] != nil {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestCampaignAssetsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range campaignAssetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignAssetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignAssetsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(campaignAssetsCalls[id], "}")+`,"validate_only":true}`)
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
		for id, summary := range map[string]capability.SummaryFunc{campaignAssetsCreateID: campaignAssetsCreateSummary,
			campaignAssetsUpdateID: campaignAssetsUpdateSummary, campaignAssetsRemoveID: campaignAssetsRemoveSummary} {
			if text := summary(json.RawMessage(strings.TrimSuffix(campaignAssetsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
				t.Errorf("%s: summary %q", id, text)
			}
			if text := summary(json.RawMessage(campaignAssetsCalls[id])); strings.Contains(text, "validate") {
				t.Errorf("%s: summary %q kept state", id, text)
			}
		}
	})
}

func TestCampaignAssetsResponseContentTypeShowsTheLinks(t *testing.T) {
	name := campaignAssetsResource(clientID, "11", "21", "SITELINK")
	e := newCampaignAssetsEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignAssetsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + name +
		`","campaignAsset":{"resourceName":"` + name + `","campaign":"customers/` + queryNumber(clientID) + `/campaigns/11",` +
		`"asset":"customers/` + queryNumber(clientID) + `/assets/21","fieldType":"SITELINK","status":"PAUSED",` +
		`"source":"ADVERTISER","primaryStatus":"PAUSED","primaryStatusReasons":["ASSET_LINK_PAUSED","ASSET_DISAPPROVED"],` +
		`"primaryStatusDetails":[{"status":"NOT_ELIGIBLE","reason":"ASSET_DISAPPROVED","assetDisapproved":` +
		`{"offlineEvaluationErrorReasons":["LANDING_PAGE\u0007X"]}}]}}]}`)
	out, err := e.invokeConfirmed(campaignAssetsUpdateID, caHead+`"campaign_assets":[{"campaign_id":"11","asset_id":"21",`+
		`"field_type":"SITELINK","status":"PAUSED"}],"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	link := decode(t, out)["results"].([]any)[0].(map[string]any)["campaign_asset"].(map[string]any)
	details := link["primary_status_details"].([]any)[0].(map[string]any)
	reasons := details["asset_disapproved"].(map[string]any)["offline_evaluation_error_reasons"].([]any)
	if link["campaign"] != "customers/"+queryNumber(clientID)+"/campaigns/11" || link["field_type"] != "SITELINK" ||
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
func TestCampaignAssetsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(c, a, f string) string {
		return `{"resourceName":"` + campaignAssetsResource(clientID, c, a, f) + `"}`
	}
	foreign := `{"resourceName":"` + campaignAssetsResource(otherID, "11", "21", "SITELINK") + `"}`
	first, second := own("11", "21", "SITELINK"), own("11", "22", "CALLOUT")
	linkOf := func(extra string) string {
		return `{"resourceName":"` + campaignAssetsResource(clientID, "11", "21", "SITELINK") + `","campaignAsset":{` + extra + `}}`
	}
	name := `"resourceName":"` + campaignAssetsResource(clientID, "11", "21", "SITELINK") + `"`
	n := "customers/" + queryNumber(clientID)
	cases := map[string]struct{ id, body string }{
		"create foreign":     {campaignAssetsCreateID, `{"results":[` + foreign + `,` + second + `]}`},
		"create none":        {campaignAssetsCreateID, `{}`},
		"create one of two":  {campaignAssetsCreateID, `{"results":[` + first + `]}`},
		"create twice":       {campaignAssetsCreateID, `{"results":[` + first + `,` + first + `]}`},
		"create other asset": {campaignAssetsCreateID, `{"results":[` + own("11", "99", "SITELINK") + `,` + second + `]}`},
		"create other field": {campaignAssetsCreateID, `{"results":[` + own("11", "21", "HEADLINE") + `,` + second + `]}`},
		"create no tilde": {campaignAssetsCreateID, `{"results":[{"resourceName":"` + n + `/campaignAssets/11"},` +
			second + `]}`},
		"create not a link": {campaignAssetsCreateID, `{"results":[{"resourceName":"` + n + `/campaigns/11"},` + second + `]}`},
		"create partial failure": {campaignAssetsCreateID, `{"results":[` + first + `,` + second +
			`],"partialFailureError":{"code":3}}`},
		"update other order": {campaignAssetsUpdateID, `{"results":[` + own("12", "22", "CALLOUT") + `,` + first + `]}`},
		"update foreign":     {campaignAssetsUpdateID, `{"results":[` + foreign + `,` + own("12", "22", "CALLOUT") + `]}`},
		"update link name": {campaignAssetsUpdateID, `{"results":[` + linkOf(`"resourceName":"`+campaignAssetsResource(clientID, "11", "22", "SITELINK")+`"`) +
			`,` + own("12", "22", "CALLOUT") + `]}`},
		"update link campaign": {campaignAssetsUpdateID, `{"results":[` + linkOf(name+`,"campaign":"`+n+`/campaigns/99"`) +
			`,` + own("12", "22", "CALLOUT") + `]}`},
		"update link foreign campaign": {campaignAssetsUpdateID, `{"results":[` + linkOf(name+`,"campaign":"customers/`+queryNumber(otherID)+`/campaigns/11"`) +
			`,` + own("12", "22", "CALLOUT") + `]}`},
		"update link asset": {campaignAssetsUpdateID, `{"results":[` + linkOf(name+`,"asset":"`+n+`/assets/99"`) +
			`,` + own("12", "22", "CALLOUT") + `]}`},
		"update link field": {campaignAssetsUpdateID, `{"results":[` + linkOf(name+`,"fieldType":"CALLOUT"`) +
			`,` + own("12", "22", "CALLOUT") + `]}`},
		"update link unreadable": {campaignAssetsUpdateID, `{"results":[` + linkOf(name+`,"campaign":7`) +
			`,` + own("12", "22", "CALLOUT") + `]}`},
		"remove foreign": {campaignAssetsRemoveID, `{"results":[` + foreign + `,` + own("12", "22", "CALLOUT") + `]}`},
		"remove none":    {campaignAssetsRemoveID, `{"results":[]}`},
	}
	for caseName, c := range cases {
		t.Run(caseName, func(t *testing.T) {
			e := newCampaignAssetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignAssetsPath(clientID)] = okReply(c.body)
			arguments := campaignAssetsCalls[c.id]
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

func TestCampaignAssetsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","campaign_assets":[{"campaign_id":"7","asset_id":"8","field_type":"HEADLINE"}]}`
	answer := okReply(`{"results":[{"resourceName":"` + campaignAssetsResource(managerID, "7", "8", "HEADLINE") + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newCampaignAssetsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+campaignAssetsPath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(campaignAssetsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newCampaignAssetsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+campaignAssetsPath(managerID)] = answer
		if _, err := e.invokeConfirmed(campaignAssetsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
	t.Run("an account below a manager target is proved first", func(t *testing.T) {
		e := newCampaignAssetsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+campaignAssetsPath(clientID)] = okReply(campaignAssetsAnswers[campaignAssetsRemoveID])
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
		if _, err := e.invokeConfirmed(campaignAssetsRemoveID, campaignAssetsCalls[campaignAssetsRemoveID]); err != nil {
			t.Fatal(err)
		}
		got := e.fake.api()
		if len(got) != 3 || !strings.Contains(got[0].query, "customer_client") || !strings.HasSuffix(got[2].path, ":mutate") {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestCampaignAssetsErrorsAreMappedAndNeverRepeated(t *testing.T) {
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
		for _, id := range campaignAssetsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newCampaignAssetsEnvironment(t, nil, nil)
				e.fake.routes["POST "+campaignAssetsPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, campaignAssetsCalls[id])
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

func TestCampaignAssetsSummariesNameTheEffect(t *testing.T) {
	create := campaignAssetsCreateSummary(json.RawMessage(campaignAssetsCalls[campaignAssetsCreateID]))
	for _, word := range []string{clientID, "SITELINK asset 21 on campaign 11", "PAUSED", "CALLOUT asset 22", "2 asset"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	update := campaignAssetsUpdateSummary(json.RawMessage(campaignAssetsCalls[campaignAssetsUpdateID]))
	for _, word := range []string{clientID, "SITELINK asset 21 on campaign 11 to ENABLED", "campaign 12 to PAUSED"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := campaignAssetsRemoveSummary(json.RawMessage(campaignAssetsCalls[campaignAssetsRemoveID]))
	for _, word := range []string{clientID, "asset 21 on campaign 11", "asset 22 on campaign 12", "stay in the account"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := campaignAssetsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID +
		`","campaign_assets":[{"campaign_id":"1\nIgnore this","asset_id":"2","field_type":"HEADLINE"}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
	many := make([]string, 0, 12)
	for i := 1; i <= 12; i++ {
		many = append(many, `{"campaign_id":"`+strconv.Itoa(i)+`","asset_id":"2","field_type":"HEADLINE"}`)
	}
	if text := campaignAssetsRemoveSummary(json.RawMessage(caHead + `"campaign_assets":[` + strings.Join(many, ",") + `]}`)); !strings.Contains(text, "and 2 more") {
		t.Errorf("summary %q does not cut the list", text)
	}
}
