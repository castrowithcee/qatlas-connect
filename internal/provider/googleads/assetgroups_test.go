package googleads

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var assetGroupToolIDs = []string{assetGroupsCreateID, assetGroupsUpdateID, assetGroupsRemoveID}

func assetGroupsPath(id string) string { return "/v26/customers/" + id + "/assetGroups:mutate" }

var assetGroupCampaign = `"campaign":"` + own + `/campaigns/21"`

var createdAssetGroup = `{"name":"Shoes","status":"PAUSED",` + assetGroupCampaign

func assetGroupsCall(items, extra string) string {
	return `{"customer_id":"` + clientID + `","asset_groups":` + items + extra + `}`
}

func assetGroupCreateArgs(members string) string {
	return assetGroupsCall(`[`+createdAssetGroup+members+`}]`, "")
}

func assetGroupUpdateArgs(members string) string {
	return assetGroupsCall(`[{"resource_name":"`+own+`/assetGroups/31"`+members+`}]`, "")
}

func assetGroupRemoveArgs(extra string) string {
	return `{"customer_id":"` + clientID + `","resource_names":["` + own + `/assetGroups/31"]` + extra + `}`
}

func assetGroupResults(names ...string) reply {
	var items []string
	for _, n := range names {
		items = append(items, `{"resourceName":"`+n+`"}`)
	}
	return okReply(`{"results":[` + strings.Join(items, ",") + `]}`)
}

func TestAssetGroupToolsAreListedOnlyConfirmedAndNarrow(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	for _, id := range assetGroupToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Fatalf("%s = %+v", id, d)
		}
		schema := string(d.InputSchema)
		for _, absent := range []string{"login_customer_id", "partial_failure", "response_content_type", "primary_status",
			"ad_strength", "asset_coverage", "id"} {
			if strings.Contains(schema, `"`+absent+`"`) {
				t.Errorf("%s offers %s", id, absent)
			}
		}
	}
	update, _, _ := reg.Lookup(assetGroupsUpdateID)
	if strings.Contains(string(update.InputSchema), `"campaign"`) || strings.Contains(string(update.InputSchema), `"category_id"`) {
		t.Error("asset-groups.update offers a member that only creation accepts")
	}
	create, _, _ := reg.Lookup(assetGroupsCreateID)
	for _, member := range []string{`"campaign"`, `"final_urls"`, `"url_custom_parameters"`, `"category_id"`} {
		if !strings.Contains(string(create.InputSchema), member) {
			t.Errorf("asset-groups.create lacks %s", member)
		}
	}
}

// Every scalar member that is not part of a repeated message is a field of the catalog of v26.
func TestAssetGroupMembersAreFieldsOfTheCatalog(t *testing.T) {
	var walk func(n node, path string)
	walk = func(n node, path string) {
		for _, f := range n.fields {
			full := path + "." + f.name
			if f.list {
				continue
			}
			if _, ok := LookupField(full); f.kind != kindObject && !ok {
				t.Errorf("%s is not in the catalog", full)
			}
			walk(f, full)
		}
	}
	walk(assetGroupTree, "asset_group")
}

func TestAssetGroupChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	calls := map[string]string{
		assetGroupsCreateID: assetGroupCreateArgs(``),
		assetGroupsUpdateID: assetGroupUpdateArgs(`,"status":"ENABLED"`),
		assetGroupsRemoveID: assetGroupRemoveArgs(""),
	}
	for id, arguments := range calls {
		t.Run(id+" without the tools list", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
				config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}})
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a connection with all effects but no tools list ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
		t.Run(id+" outside the targets", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + otherID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestAssetGroupSummaryNamesTheSpendingEffect(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	_, err := e.invoke(assetGroupsCreateID, assetGroupsCall(`[{"name":"Shoes","status":"ENABLED",`+assetGroupCampaign+`}]`,
		`,"validate_only":true`))
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatal(err)
	}
	for _, word := range []string{"Only validate", "Shoes", "ENABLED", "campaigns/21", "spends"} {
		if !strings.Contains(needed.Summary, word) {
			t.Errorf("summary %q lacks %q", needed.Summary, word)
		}
	}
	_, err = e.invoke(assetGroupsRemoveID, assetGroupRemoveArgs(""))
	if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "cannot be enabled again") {
		t.Errorf("summary = %v", err)
	}
}

// What the arguments get wrong is refused before a secret is read or a request is made.
func TestAssetGroupArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	// at keeps the user-info samples from reading as mail addresses.
	const at = "@"
	bad := map[string]struct{ id, arguments string }{
		"no status":   {assetGroupsCreateID, assetGroupsCall(`[{"name":"x",`+assetGroupCampaign+`}]`, "")},
		"no name":     {assetGroupsCreateID, assetGroupsCall(`[{"status":"PAUSED",`+assetGroupCampaign+`}]`, "")},
		"no campaign": {assetGroupsCreateID, assetGroupsCall(`[{"name":"x","status":"PAUSED"}]`, "")},
		"removed":     {assetGroupsCreateID, assetGroupCreateArgs(`,"status":"REMOVED"`)},
		"none":        {assetGroupsCreateID, assetGroupsCall(`[]`, "")},
		"eleven":      {assetGroupsCreateID, assetGroupsCall(`[`+strings.Repeat(createdAssetGroup+`},`, 10)+createdAssetGroup+`}]`, "")},
		"empty name":  {assetGroupsCreateID, assetGroupsCall(`[{"name":"","status":"PAUSED",`+assetGroupCampaign+`}]`, "")},
		"line feed":   {assetGroupsCreateID, assetGroupsCall(`[{"name":"a\nb","status":"PAUSED",`+assetGroupCampaign+`}]`, "")},
		"long name":   {assetGroupsCreateID, assetGroupsCall(`[{"name":"`+strings.Repeat("a", 129)+`","status":"PAUSED",`+assetGroupCampaign+`}]`, "")},
		"path2 alone": {assetGroupsCreateID, assetGroupCreateArgs(`,"path2":"b"`)},
		"http url":    {assetGroupsCreateID, assetGroupCreateArgs(`,"final_urls":["http://www.example.com/"]`)},
		"user info url": {assetGroupsCreateID,
			assetGroupCreateArgs(`,"final_urls":["https://user:pw` + at + `www.example.com/"]`)},
		"http mobile url": {assetGroupsCreateID, assetGroupCreateArgs(`,"final_mobile_urls":["http://m.example.com/"]`)},
		"http template": {assetGroupsCreateID,
			assetGroupCreateArgs(`,"tracking_url_template":"http://t.example.com/?u={lpurl}"`)},
		"user info": {assetGroupsCreateID,
			assetGroupCreateArgs(`,"tracking_url_template":"https://user:pw` + at + `t.example.com/?u={lpurl}"`)},
		"suffix address":   {assetGroupsCreateID, assetGroupCreateArgs(`,"final_url_suffix":"https://x.example.com/"`)},
		"foreign campaign": {assetGroupsCreateID, assetGroupsCall(`[{"name":"x","status":"PAUSED","campaign":"`+foreign+`/campaigns/21"}]`, "")},
		"temp campaign":    {assetGroupsCreateID, assetGroupsCall(`[{"name":"x","status":"PAUSED","campaign":"`+own+`/campaigns/-1"}]`, "")},
		"output only":      {assetGroupsCreateID, assetGroupCreateArgs(`,"primary_status":"ELIGIBLE"`)},
		"partial failure":  {assetGroupsCreateID, assetGroupsCall(`[`+createdAssetGroup+`}]`, `,"partial_failure":true`)},
		"content type":     {assetGroupsCreateID, assetGroupsCall(`[`+createdAssetGroup+`}]`, `,"response_content_type":"MUTABLE_RESOURCE"`)},

		"update no member": {assetGroupsUpdateID, assetGroupUpdateArgs(``)},
		"update campaign":  {assetGroupsUpdateID, assetGroupUpdateArgs(`,"campaign":"` + own + `/campaigns/22"`)},
		"update removed":   {assetGroupsUpdateID, assetGroupUpdateArgs(`,"status":"REMOVED"`)},
		"update foreign":   {assetGroupsUpdateID, assetGroupsCall(`[{"resource_name":"`+foreign+`/assetGroups/31","status":"PAUSED"}]`, "")},
		"update temporary": {assetGroupsUpdateID, assetGroupsCall(`[{"resource_name":"`+own+`/assetGroups/-2","status":"PAUSED"}]`, "")},
		"update kind":      {assetGroupsUpdateID, assetGroupsCall(`[{"resource_name":"`+own+`/campaigns/31","status":"PAUSED"}]`, "")},
		"update twice": {assetGroupsUpdateID, assetGroupsCall(`[{"resource_name":"`+own+`/assetGroups/31","status":"PAUSED"},`+
			`{"resource_name":"`+own+`/assetGroups/31","status":"ENABLED"}]`, "")},
		"update template": {assetGroupsUpdateID, assetGroupUpdateArgs(`,"tracking_url_template":"https://evil.example` + at + `t.example.com/"`)},
		"update url":      {assetGroupsUpdateID, assetGroupUpdateArgs(`,"final_urls":["http://www.example.com/"]`)},

		"remove foreign":   {assetGroupsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + foreign + `/assetGroups/31"]}`},
		"remove temporary": {assetGroupsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/assetGroups/-31"]}`},
		"remove twice": {assetGroupsRemoveID, `{"customer_id":"` + clientID + `","resource_names":["` + own + `/assetGroups/31","` +
			own + `/assetGroups/31"]}`},
		"remove none": {assetGroupsRemoveID, `{"customer_id":"` + clientID + `","resource_names":[]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c.id, c.arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestAssetGroupsCreateSendsOneAtomicRequest(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+assetGroupsPath(clientID)] = assetGroupResults(own+"/assetGroups/31", own+"/assetGroups/32")
	out, err := e.invokeConfirmed(assetGroupsCreateID, assetGroupsCall(`[{"name":"Shoes","status":"PAUSED",`+assetGroupCampaign+
		`,"final_urls":["https://www.example.com/shoes"],"final_mobile_urls":["https://m.example.com/shoes"],`+
		`"path1":"shoes","path2":"sale","tracking_url_template":"https://t.example.com/?u={lpurl}",`+
		`"final_url_suffix":"utm_source=g","url_custom_parameters":[{"key":"season","value":"spring"}],`+
		`"google_local_services_info":{"category_id":"xcat:service_area_business_plumber","callouts":[{"callout_id":"c1"}]}},`+
		`{"name":"Boots","status":"ENABLED",`+assetGroupCampaign+`}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	results := got["results"].([]any)
	if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
		results[0].(map[string]any)["resource_name"] != own+"/assetGroups/31" ||
		results[1].(map[string]any)["resource_name"] != own+"/assetGroups/32" {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+assetGroupsPath(clientID) || p.auth != "Bearer synthetic-access-1" ||
		p.login != loginID || p.contentType != "application/json" || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	body := bodyOf(t, p)
	if len(body) != 1 || strings.Contains(p.body, "partialFailure") || strings.Contains(p.body, "validateOnly") ||
		strings.Contains(p.body, "responseContentType") {
		t.Errorf("body members = %v, want the operations alone", body)
	}
	want := wantWire(t, `[{"create":{"name":"Shoes","status":"PAUSED","campaign":"`+own+`/campaigns/21",`+
		`"finalUrls":["https://www.example.com/shoes"],"finalMobileUrls":["https://m.example.com/shoes"],`+
		`"path1":"shoes","path2":"sale","trackingUrlTemplate":"https://t.example.com/?u={lpurl}","finalUrlSuffix":"utm_source=g",`+
		`"urlCustomParameters":[{"key":"season","value":"spring"}],"googleLocalServicesInfo":{`+
		`"categoryId":"xcat:service_area_business_plumber","callouts":[{"calloutId":"c1"}]}}},`+
		`{"create":{"name":"Boots","status":"ENABLED","campaign":"`+own+`/campaigns/21"}}]`)
	if !reflect.DeepEqual(body["operations"], want) {
		t.Errorf("operations = %s", p.body)
	}
}

func TestAssetGroupsUpdateNamesExactlyTheGivenMembersInTheMask(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+assetGroupsPath(clientID)] = assetGroupResults(own+"/assetGroups/31", own+"/assetGroups/32")
	_, err := e.invokeConfirmed(assetGroupsUpdateID, assetGroupsCall(`[{"resource_name":"`+own+`/assetGroups/31","status":"ENABLED",`+
		`"final_urls":["https://www.example.com/new"],"url_custom_parameters":[{"key":"a","value":"b"}]},`+
		`{"resource_name":"`+own+`/assetGroups/32","tracking_url_template":"","final_url_suffix":"x=1"}]`, ""))
	if err != nil {
		t.Fatal(err)
	}
	ops := bodyOf(t, e.posts()[0])["operations"].([]any)
	first, second := ops[0].(map[string]any), ops[1].(map[string]any)
	if first["updateMask"] != "finalUrls,status,urlCustomParameters" {
		t.Errorf("first mask = %v", first["updateMask"])
	}
	if !reflect.DeepEqual(first["update"], wantWire(t, `{"resourceName":"`+own+`/assetGroups/31","status":"ENABLED",`+
		`"finalUrls":["https://www.example.com/new"],"urlCustomParameters":[{"key":"a","value":"b"}]}`)) {
		t.Errorf("first update = %v", first["update"])
	}
	// A text that is cleared is in the mask and has no value.
	if second["updateMask"] != "finalUrlSuffix,trackingUrlTemplate" ||
		!reflect.DeepEqual(second["update"], map[string]any{"resourceName": own + "/assetGroups/32", "finalUrlSuffix": "x=1"}) {
		t.Errorf("second operation = %v", second)
	}
}

func TestAssetGroupsRemoveSendsRemoveOperations(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+assetGroupsPath(clientID)] = assetGroupResults(own+"/assetGroups/31", own+"/assetGroups/32")
	out, err := e.invokeConfirmed(assetGroupsRemoveID, `{"customer_id":"`+clientID+`","resource_names":["`+own+
		`/assetGroups/31","`+own+`/assetGroups/32"]}`)
	if err != nil || len(decode(t, out)["results"].([]any)) != 2 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	body := bodyOf(t, e.posts()[0])
	if !reflect.DeepEqual(body["operations"], wantWire(t, `[{"remove":"`+own+`/assetGroups/31"},{"remove":"`+own+
		`/assetGroups/32"}]`)) || len(body) != 1 {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

func TestAssetGroupValidateOnlyAnswersNoResult(t *testing.T) {
	for id, arguments := range map[string]string{
		assetGroupsCreateID: assetGroupsCall(`[`+createdAssetGroup+`}]`, `,"validate_only":true`),
		assetGroupsUpdateID: assetGroupsCall(`[{"resource_name":"`+own+`/assetGroups/31","status":"PAUSED"}]`, `,"validate_only":true`),
		assetGroupsRemoveID: assetGroupRemoveArgs(`,"validate_only":true`),
	} {
		for _, answer := range []string{`{}`, `{"results":[]}`} {
			t.Run(id+" "+answer, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+assetGroupsPath(clientID)] = okReply(answer)
				out, err := e.invokeConfirmed(id, arguments)
				got := decode(t, out)
				if err != nil || got["validate_only"] != true || got["results"] != nil {
					t.Fatalf("out=%s err=%v", out, err)
				}
				if bodyOf(t, e.posts()[0])["validateOnly"] != true || len(e.posts()) != 1 {
					t.Errorf("posts = %+v", e.posts())
				}
			})
		}
	}
}

// An answer that names another account, another asset group, or another number of results than asked for never
// passes. The change was sent, so the error says that the outcome is unknown.
func TestAssetGroupAnswersAreBoundToTheCheckedAccount(t *testing.T) {
	update := assetGroupsCall(`[{"resource_name":"`+own+`/assetGroups/31","status":"PAUSED"}]`, "")
	create := assetGroupsCall(`[`+createdAssetGroup+`}]`, "")
	cases := map[string]struct{ id, arguments, body string }{
		"foreign result":  {assetGroupsCreateID, create, `{"results":[{"resourceName":"` + foreign + `/assetGroups/31"}]}`},
		"other group":     {assetGroupsUpdateID, update, `{"results":[{"resourceName":"` + own + `/assetGroups/99"}]}`},
		"campaign result": {assetGroupsCreateID, create, `{"results":[{"resourceName":"` + own + `/campaigns/31"}]}`},
		"no name":         {assetGroupsUpdateID, update, `{"results":[{}]}`},
		"no results":      {assetGroupsUpdateID, update, `{}`},
		"too many":        {assetGroupsCreateID, create, `{"results":[{"resourceName":"` + own + `/assetGroups/1"},{"resourceName":"` + own + `/assetGroups/2"}]}`},
		"partial failure": {assetGroupsUpdateID, update, `{"results":[{"resourceName":"` + own + `/assetGroups/31"}],"partialFailureError":{"code":3}}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+assetGroupsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestAssetGroupFailuresMapWithoutProviderText(t *testing.T) {
	arguments := assetGroupsCall(`[`+createdAssetGroup+`}]`, "")
	cases := map[string]struct {
		status        int
		body, outcome string
		class         provider.Class
	}{
		"validation": {400, adsFailure(400, "INVALID_ARGUMENT", "assetGroupError", "DUPLICATE_NAME", ""), "",
			provider.ClassProviderError},
		"permission": {403, adsFailure(403, "PERMISSION_DENIED", "authorizationError", "USER_PERMISSION_DENIED", ""), "",
			provider.ClassPermission},
		"quota": {429, adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED",
			`{"rateScope":"ACCOUNT","retryDelay":"3s"}`), "", provider.ClassRateLimited},
		"server": {503, googleError(503, "UNAVAILABLE", ""), "unknown", provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+assetGroupsPath(clientID)] = reply{status: c.status, body: c.body}
			_, err := e.invokeConfirmed(assetGroupsCreateID, arguments)
			if classOf(err) != c.class || strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), triggerCanary) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") != (c.outcome != "") || len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
	t.Run("a cut connection", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+assetGroupsPath(clientID)] = reply{err: errors.New("connection reset")}
		_, err := e.invokeConfirmed(assetGroupsCreateID, arguments)
		if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.posts()) != 1 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
}

// A manager account has no asset groups: a customer target that names one is refused before the change.
func TestAssetGroupChangesRefuseAManagerAccountOnACustomerTarget(t *testing.T) {
	e := newMutationEnvironment(t, []string{"customers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
			return &r
		}
		return nil
	}
	_, err := e.invokeConfirmed(assetGroupsRemoveID, `{"customer_id":"`+managerID+`","resource_names":["customers/`+
		queryNumber(managerID)+`/assetGroups/31"]}`)
	if !isInvalidRequest(err) || len(e.posts()) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
	}
}
