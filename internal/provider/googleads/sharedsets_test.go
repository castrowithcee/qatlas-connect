package googleads

import (
	"context"
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

var sharedSetsToolIDs = []string{sharedSetsCreateID, sharedSetsUpdateID, sharedSetsRemoveID,
	campaignSharedSetsCreateID, campaignSharedSetsRemoveID}

func sharedSetsPath(id, service string) string { return "/v26/customers/" + id + service + ":mutate" }

func sharedSetsPathOf(tool string) string {
	if strings.Contains(tool, ".campaign-shared-sets.") {
		return sharedSetsPath(clientID, campaignSharedSetsService)
	}
	return sharedSetsPath(clientID, sharedSetsService)
}

func setResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/sharedSets/" + id
}

func campaignSharedSetResource(customer, campaign, set string) string {
	return "customers/" + queryNumber(customer) + "/campaignSharedSets/" + campaign + "~" + set
}

func newSharedSetsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: sharedSetsToolIDs})
}

var sharedSetsCalls = map[string]string{
	sharedSetsCreateID: `{"customer_id":"` + clientID + `","shared_sets":[{"name":"Brand terms","type":"NEGATIVE_KEYWORDS"},` +
		`{"name":"Hotel rules","type":"VERTICAL_ADS_ITEM_GROUP_RULE_LIST","vertical_ads_item_vertical_type":"HOTELS"}]}`,
	sharedSetsUpdateID: `{"customer_id":"` + clientID + `","shared_sets":[{"shared_set_id":"111","name":"Archive"},` +
		`{"shared_set_id":"222","name":"Old terms"}]}`,
	sharedSetsRemoveID: `{"customer_id":"` + clientID + `","shared_set_ids":["111","222"]}`,
	campaignSharedSetsCreateID: `{"customer_id":"` + clientID + `","links":[{"campaign_id":"31","shared_set_id":"111"},` +
		`{"campaign_id":"32","shared_set_id":"111"}]}`,
	campaignSharedSetsRemoveID: `{"customer_id":"` + clientID + `","links":[{"campaign_id":"31","shared_set_id":"111"},` +
		`{"campaign_id":"32","shared_set_id":"111"}]}`,
}

var sharedSetsAnswers = func() map[string]string {
	sets := `{"results":[{"resourceName":"` + setResource(clientID, "111") + `"},{"resourceName":"` +
		setResource(clientID, "222") + `"}]}`
	links := `{"results":[{"resourceName":"` + campaignSharedSetResource(clientID, "31", "111") + `"},{"resourceName":"` +
		campaignSharedSetResource(clientID, "32", "111") + `"}]}`
	return map[string]string{sharedSetsCreateID: sets, sharedSetsUpdateID: sets, sharedSetsRemoveID: sets,
		campaignSharedSetsCreateID: links, campaignSharedSetsRemoveID: links}
}()

func isSharedSetsRemove(id string) bool {
	return id == sharedSetsRemoveID || id == campaignSharedSetsRemoveID
}

func TestSharedSetsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{sharedSetsCreateID: capability.EffectCreate,
		sharedSetsUpdateID: capability.EffectUpdate, sharedSetsRemoveID: capability.EffectDelete,
		campaignSharedSetsCreateID: capability.EffectCreate, campaignSharedSetsRemoveID: capability.EffectDelete}
	for _, id := range sharedSetsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		// Only the removals are listed-only; create and update are reached by their effect.
		if d.RequiresToolAllowList != isSharedSetsRemove(id) {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		wantNote := sharedSetsChangeNote
		if isSharedSetsRemove(id) {
			wantNote = sharedSetsListedNote
		}
		if !strings.Contains(d.Description, wantNote) || strings.HasSuffix(d.Description, sharedSetsListedNote) != isSharedSetsRemove(id) {
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
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "url", "status",
			"resource_name", "update_mask", "campaign", "shared_set"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if _, found := input.Properties["response_content_type"]; found == isSharedSetsRemove(id) {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		if !strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "never repeats") ||
			!strings.Contains(d.Description, "atomic") || !strings.Contains(d.Description, "per entry") {
			t.Errorf("%s: description lacks the unknown outcome or the quota: %s", id, d.Description)
		}
		for _, tool := range metadata.Tools {
			if tool.ID == id && (string(tool.Effect) != string(wantEffect[id]) || tool.RequiresToolAllowList != isSharedSetsRemove(id)) {
				t.Errorf("tool %+v", tool)
			}
		}
	}
	create, _, _ := reg.Lookup(sharedSetsCreateID)
	for _, text := range []string{`"maxLength":255`, `"maxItems":50`, `VERTICAL_ADS_ITEM_GROUP_RULE_LIST`, `VACATION_RENTALS`} {
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
	if strings.Contains(string(create.InputSchema), "WEBPAGES") || strings.Contains(string(create.InputSchema), "UNSPECIFIED") {
		t.Errorf("create schema offers a type that cannot be set: %s", create.InputSchema)
	}
}

// create and update need an effect and no tools list; a removal needs the tools list whatever the effects are.
func TestSharedSetsToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+sharedSetsPathOf(id)] = okReply(sharedSetsAnswers[id])
		_, err := e.invokeConfirmed(id, sharedSetsCalls[id])
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
		{"all effects, no tools list", config.Connection{Effects: all}, map[string]bool{sharedSetsCreateID: true,
			sharedSetsUpdateID: true, sharedSetsRemoveID: false, campaignSharedSetsCreateID: true, campaignSharedSetsRemoveID: false}},
		{"create effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}},
			map[string]bool{sharedSetsCreateID: true, campaignSharedSetsCreateID: true}},
		{"delete effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectDelete}},
			map[string]bool{}},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}}, map[string]bool{}},
		{"removals listed", config.Connection{Tools: []string{sharedSetsRemoveID, campaignSharedSetsRemoveID}},
			map[string]bool{sharedSetsRemoveID: true, campaignSharedSetsRemoveID: true}},
		{"all listed", config.Connection{Tools: sharedSetsToolIDs}, map[string]bool{sharedSetsCreateID: true,
			sharedSetsUpdateID: true, sharedSetsRemoveID: true, campaignSharedSetsCreateID: true, campaignSharedSetsRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range sharedSetsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestSharedSetsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range sharedSetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newSharedSetsEnvironment(t, nil, nil)
			_, err := e.invoke(id, sharedSetsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestSharedSetsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"name"`} {
		for _, id := range sharedSetsToolIDs {
			e := newSharedSetsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(sharedSetsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestSharedSetsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range sharedSetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newSharedSetsEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(sharedSetsCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestSharedSetsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	set := func(body string) string { return head + `"shared_sets":[` + body + `]}` }
	link := func(body string) string { return head + `"links":[` + body + `]}` }
	many := func(item string, n int) string { return strings.TrimSuffix(strings.Repeat(item+",", n), ",") }
	bad := map[string][2]string{
		"create none":              {sharedSetsCreateID, set(``)},
		"create no name":           {sharedSetsCreateID, set(`{"type":"NEGATIVE_KEYWORDS"}`)},
		"create empty name":        {sharedSetsCreateID, set(`{"name":"","type":"NEGATIVE_KEYWORDS"}`)},
		"create name over bytes":   {sharedSetsCreateID, set(`{"name":"` + strings.Repeat("ä", 128) + `","type":"NEGATIVE_KEYWORDS"}`)},
		"create control in name":   {sharedSetsCreateID, set(`{"name":"a\u0007b","type":"NEGATIVE_KEYWORDS"}`)},
		"create no type":           {sharedSetsCreateID, set(`{"name":"a"}`)},
		"create webpages":          {sharedSetsCreateID, set(`{"name":"a","type":"WEBPAGES"}`)},
		"create unspecified":       {sharedSetsCreateID, set(`{"name":"a","type":"UNSPECIFIED"}`)},
		"create vertical missing":  {sharedSetsCreateID, set(`{"name":"a","type":"VERTICAL_ADS_ITEM_GROUP_RULE_LIST"}`)},
		"create vertical unknown":  {sharedSetsCreateID, set(`{"name":"a","type":"VERTICAL_ADS_ITEM_GROUP_RULE_LIST","vertical_ads_item_vertical_type":"BOATS"}`)},
		"create vertical foreign":  {sharedSetsCreateID, set(`{"name":"a","type":"BRANDS","vertical_ads_item_vertical_type":"HOTELS"}`)},
		"create status":            {sharedSetsCreateID, set(`{"name":"a","type":"BRANDS","status":"REMOVED"}`)},
		"create output only":       {sharedSetsCreateID, set(`{"name":"a","type":"BRANDS","member_count":"3"}`)},
		"create too many":          {sharedSetsCreateID, set(many(`{"name":"a","type":"BRANDS"}`, 51))},
		"create bad content type":  {sharedSetsCreateID, set(`{"name":"a","type":"BRANDS"}`)[:len(set(`{"name":"a","type":"BRANDS"}`))-1] + `,"response_content_type":"X"}`},
		"update no id":             {sharedSetsUpdateID, set(`{"name":"a"}`)},
		"update zero id":           {sharedSetsUpdateID, set(`{"shared_set_id":"0","name":"a"}`)},
		"update id as path":        {sharedSetsUpdateID, set(`{"shared_set_id":"customers/1/sharedSets/2","name":"a"}`)},
		"update id overflow":       {sharedSetsUpdateID, set(`{"shared_set_id":"99999999999999999999","name":"a"}`)},
		"update no name":           {sharedSetsUpdateID, set(`{"shared_set_id":"1"}`)},
		"update empty name":        {sharedSetsUpdateID, set(`{"shared_set_id":"1","name":""}`)},
		"update same id twice":     {sharedSetsUpdateID, set(`{"shared_set_id":"1","name":"a"},{"shared_set_id":"1","name":"b"}`)},
		"update type":              {sharedSetsUpdateID, set(`{"shared_set_id":"1","name":"a","type":"BRANDS"}`)},
		"update status":            {sharedSetsUpdateID, set(`{"shared_set_id":"1","name":"a","status":"ENABLED"}`)},
		"remove none":              {sharedSetsRemoveID, head + `"shared_set_ids":[]}`},
		"remove name":              {sharedSetsRemoveID, head + `"shared_set_ids":["customers/1/sharedSets/2"]}`},
		"remove twice":             {sharedSetsRemoveID, head + `"shared_set_ids":["1","1"]}`},
		"remove content type":      {sharedSetsRemoveID, head + `"shared_set_ids":["1"],"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":             {sharedSetsRemoveID, `{"customer_id":"12-34","shared_set_ids":["1"]}`},
		"validate only alone":      {sharedSetsCreateID, head + `"validate_only":true}`},
		"link none":                {campaignSharedSetsCreateID, link(``)},
		"link no campaign":         {campaignSharedSetsCreateID, link(`{"shared_set_id":"1"}`)},
		"link no set":              {campaignSharedSetsCreateID, link(`{"campaign_id":"1"}`)},
		"link zero campaign":       {campaignSharedSetsCreateID, link(`{"campaign_id":"0","shared_set_id":"1"}`)},
		"link campaign as path":    {campaignSharedSetsCreateID, link(`{"campaign_id":"customers/9/campaigns/1","shared_set_id":"1"}`)},
		"link tilde injection":     {campaignSharedSetsRemoveID, link(`{"campaign_id":"1~2","shared_set_id":"3"}`)},
		"link set as path":         {campaignSharedSetsCreateID, link(`{"campaign_id":"1","shared_set_id":"customers/9/sharedSets/1"}`)},
		"link twice":               {campaignSharedSetsRemoveID, link(`{"campaign_id":"1","shared_set_id":"2"},{"campaign_id":"1","shared_set_id":"2"}`)},
		"link foreign member":      {campaignSharedSetsCreateID, link(`{"campaign_id":"1","shared_set_id":"2","status":"ENABLED"}`)},
		"link too many":            {campaignSharedSetsRemoveID, link(many(`{"campaign_id":"1","shared_set_id":"2"}`, 51))},
		"link remove content type": {campaignSharedSetsRemoveID, strings.TrimSuffix(link(`{"campaign_id":"1","shared_set_id":"2"}`), "}") + `,"response_content_type":"MUTABLE_RESOURCE"}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newSharedSetsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	// Exactly at the documented limits, the call passes.
	t.Run("limits pass", func(t *testing.T) {
		e := newSharedSetsEnvironment(t, nil, nil)
		e.fake.routes["POST "+sharedSetsPathOf(sharedSetsCreateID)] = okReply(`{"results":[{"resourceName":"` + setResource(clientID, "5") + `"}]}`)
		if _, err := e.invokeConfirmed(sharedSetsCreateID, set(`{"name":"`+strings.Repeat("a", 253)+`ä","type":"BRANDS"}`)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSharedSetsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		sharedSetsCreateID: `[{"create":{"name":"Brand terms","type":"NEGATIVE_KEYWORDS"}},` +
			`{"create":{"name":"Hotel rules","type":"VERTICAL_ADS_ITEM_GROUP_RULE_LIST","verticalAdsItemVerticalType":"HOTELS"}}]`,
		sharedSetsUpdateID: `[{"update":{"resourceName":"customers/1000000003/sharedSets/111","name":"Archive"},"updateMask":"name"},` +
			`{"update":{"resourceName":"customers/1000000003/sharedSets/222","name":"Old terms"},"updateMask":"name"}]`,
		sharedSetsRemoveID: `[{"remove":"customers/1000000003/sharedSets/111"},{"remove":"customers/1000000003/sharedSets/222"}]`,
		campaignSharedSetsCreateID: `[{"create":{"campaign":"customers/1000000003/campaigns/31","sharedSet":"customers/1000000003/sharedSets/111"}},` +
			`{"create":{"campaign":"customers/1000000003/campaigns/32","sharedSet":"customers/1000000003/sharedSets/111"}}]`,
		campaignSharedSetsRemoveID: `[{"remove":"customers/1000000003/campaignSharedSets/31~111"},` +
			`{"remove":"customers/1000000003/campaignSharedSets/32~111"}]`,
	}
	for _, id := range sharedSetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newSharedSetsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+sharedSetsPathOf(id)] = okReply(sharedSetsAnswers[id])
			out, err := e.invokeConfirmed(id, sharedSetsCalls[id])
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
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 {
				t.Fatalf("out = %s", out)
			}
			if id == campaignSharedSetsCreateID || id == campaignSharedSetsRemoveID {
				first := results[0].(map[string]any)
				if first["campaign_id"] != "31" || first["shared_set_id"] != "111" ||
					results[1].(map[string]any)["resource_name"] != campaignSharedSetResource(clientID, "32", "111") {
					t.Errorf("out = %s", out)
				}
			} else if results[0].(map[string]any)["shared_set_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != setResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestSharedSetsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range sharedSetsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newSharedSetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+sharedSetsPathOf(id)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(sharedSetsCalls[id], "}")+`,"validate_only":true}`)
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
		summaries := map[string]capability.SummaryFunc{sharedSetsCreateID: sharedSetsCreateSummary,
			sharedSetsUpdateID: sharedSetsUpdateSummary, sharedSetsRemoveID: sharedSetsRemoveSummary,
			campaignSharedSetsCreateID: campaignSharedSetsCreateSummary, campaignSharedSetsRemoveID: campaignSharedSetsRemoveSummary}
		for id, summary := range summaries {
			if text := summary(json.RawMessage(strings.TrimSuffix(sharedSetsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
				t.Errorf("%s: summary %q", id, text)
			}
			if text := summary(json.RawMessage(sharedSetsCalls[id])); strings.Contains(text, "validate") {
				t.Errorf("%s: summary %q kept state", id, text)
			}
		}
	})
}

func TestSharedSetsResponseContentTypeShowsTheObjects(t *testing.T) {
	t.Run("shared set", func(t *testing.T) {
		e := newSharedSetsEnvironment(t, nil, nil)
		e.fake.routes["POST "+sharedSetsPathOf(sharedSetsUpdateID)] = okReply(`{"results":[{"resourceName":"` + setResource(clientID, "111") +
			`","sharedSet":{"resourceName":"` + setResource(clientID, "111") + `","id":"111","name":"Archive\u0007 list",` +
			`"type":"NEGATIVE_KEYWORDS","status":"ENABLED","memberCount":"4","referenceCount":"2"}}]}`)
		out, err := e.invokeConfirmed(sharedSetsUpdateID, `{"customer_id":"`+clientID+`","shared_sets":[{"shared_set_id":"111","name":"Archive"}],`+
			`"response_content_type":"MUTABLE_RESOURCE"}`)
		if err != nil {
			t.Fatal(err)
		}
		s := decode(t, out)["results"].([]any)[0].(map[string]any)["shared_set"].(map[string]any)
		if s["id"] != "111" || s["type"] != "NEGATIVE_KEYWORDS" || s["status"] != "ENABLED" || s["member_count"] != "4" ||
			s["reference_count"] != "2" || !strings.HasPrefix(s["name"].(string), "Archive") || strings.Contains(s["name"].(string), "\a") {
			t.Errorf("shared set = %v", s)
		}
		if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
			t.Errorf("body = %s", e.posts()[0].body)
		}
	})
	t.Run("link", func(t *testing.T) {
		e := newSharedSetsEnvironment(t, nil, nil)
		name := campaignSharedSetResource(clientID, "31", "111")
		e.fake.routes["POST "+sharedSetsPathOf(campaignSharedSetsCreateID)] = okReply(`{"results":[{"resourceName":"` + name +
			`","campaignSharedSet":{"resourceName":"` + name + `","campaign":"customers/1000000003/campaigns/31",` +
			`"sharedSet":"customers/1000000003/sharedSets/111","status":"ENABLED"}}]}`)
		out, err := e.invokeConfirmed(campaignSharedSetsCreateID, `{"customer_id":"`+clientID+
			`","links":[{"campaign_id":"31","shared_set_id":"111"}],"response_content_type":"MUTABLE_RESOURCE"}`)
		if err != nil {
			t.Fatal(err)
		}
		l := decode(t, out)["results"].([]any)[0].(map[string]any)["campaign_shared_set"].(map[string]any)
		if l["campaign"] != "customers/1000000003/campaigns/31" || l["shared_set"] != "customers/1000000003/sharedSets/111" ||
			l["status"] != "ENABLED" {
			t.Errorf("link = %v", l)
		}
	})
}

// A resource name of another account, another object, or another count never passes. The request was sent, so
// the error says that the outcome is unknown.
func TestSharedSetsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	own := func(id string) string { return `{"resourceName":"` + setResource(clientID, id) + `"}` }
	ownLink := func(campaign, set string) string {
		return `{"resourceName":"` + campaignSharedSetResource(clientID, campaign, set) + `"}`
	}
	foreign := `{"resourceName":"` + setResource(otherID, "111") + `"}`
	foreignLink := `{"resourceName":"` + campaignSharedSetResource(otherID, "31", "111") + `"}`
	withSet := func(body string) string {
		return `{"resourceName":"` + setResource(clientID, "111") + `","sharedSet":` + body + `}`
	}
	cases := map[string]struct{ id, body string }{
		"create foreign":     {sharedSetsCreateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"create no results":  {sharedSetsCreateID, `{}`},
		"create one of two":  {sharedSetsCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":       {sharedSetsCreateID, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create other type":  {sharedSetsCreateID, `{"results":[{"resourceName":"customers/1000000003/campaigns/1"},` + own("222") + `]}`},
		"create partial":     {sharedSetsCreateID, `{"results":[` + own("111") + `,` + own("222") + `],"partialFailureError":{"code":3}}`},
		"update other set":   {sharedSetsUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":     {sharedSetsUpdateID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"update set foreign": {sharedSetsUpdateID, `{"results":[` + withSet(`{"resourceName":"`+setResource(otherID, "111")+`"}`) + `,` + own("222") + `]}`},
		"update set id":      {sharedSetsUpdateID, `{"results":[` + withSet(`{"resourceName":"`+setResource(clientID, "111")+`","id":"999"}`) + `,` + own("222") + `]}`},
		"remove foreign":     {sharedSetsRemoveID, `{"results":[` + foreign + `,` + own("222") + `]}`},
		"remove none":        {sharedSetsRemoveID, `{"results":[]}`},
		"link foreign":       {campaignSharedSetsCreateID, `{"results":[` + foreignLink + `,` + ownLink("32", "111") + `]}`},
		"link other pair":    {campaignSharedSetsCreateID, `{"results":[` + ownLink("32", "111") + `,` + ownLink("31", "111") + `]}`},
		"link other set":     {campaignSharedSetsRemoveID, `{"results":[` + ownLink("31", "222") + `,` + ownLink("32", "111") + `]}`},
		"link not a link":    {campaignSharedSetsRemoveID, `{"results":[` + own("111") + `,` + ownLink("32", "111") + `]}`},
		"link none":          {campaignSharedSetsRemoveID, `{"results":[]}`},
		"link partial": {campaignSharedSetsCreateID, `{"results":[` + ownLink("31", "111") + `,` + ownLink("32", "111") +
			`],"partialFailureError":{"code":3}}`},
		"link member foreign": {campaignSharedSetsCreateID, `{"results":[{"resourceName":"` + campaignSharedSetResource(clientID, "31", "111") +
			`","campaignSharedSet":{"resourceName":"` + campaignSharedSetResource(clientID, "31", "111") +
			`","campaign":"customers/` + queryNumber(otherID) + `/campaigns/31"}},` + ownLink("32", "111") + `]}`},
		"link member name": {campaignSharedSetsCreateID, `{"results":[{"resourceName":"` + campaignSharedSetResource(clientID, "31", "111") +
			`","campaignSharedSet":{"resourceName":"` + campaignSharedSetResource(clientID, "31", "222") + `"}},` + ownLink("32", "111") + `]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSharedSetsEnvironment(t, nil, nil)
			e.fake.routes["POST "+sharedSetsPathOf(c.id)] = okReply(c.body)
			arguments := sharedSetsCalls[c.id]
			if strings.Contains(name, "set id") || strings.Contains(name, "set foreign") || strings.Contains(name, "member") {
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

func TestSharedSetsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","shared_set_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + setResource(managerID, "7") + `"}]}`)
	path := sharedSetsPath(managerID, sharedSetsService)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newSharedSetsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+path] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(sharedSetsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newSharedSetsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+path] = answer
		if _, err := e.invokeConfirmed(sharedSetsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || len(e.posts()) != 1 {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestSharedSetsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"shared set error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "sharedSetError", "DUPLICATE_NAME", "")},
			provider.ClassProviderError, "rejected the change (sharedSetError DUPLICATE_NAME), nothing was changed", false},
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
		for _, id := range sharedSetsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newSharedSetsEnvironment(t, nil, nil)
				e.fake.routes["POST "+sharedSetsPathOf(id)] = c.reply
				_, err := e.invokeConfirmed(id, sharedSetsCalls[id])
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

func TestSharedSetsSummariesNameTheEffect(t *testing.T) {
	check := func(text string, words ...string) {
		t.Helper()
		for _, word := range words {
			if !strings.Contains(text, word) {
				t.Errorf("summary %q lacks %q", text, word)
			}
		}
	}
	check(sharedSetsCreateSummary(json.RawMessage(sharedSetsCalls[sharedSetsCreateID])), clientID, "Brand terms",
		"NEGATIVE_KEYWORDS", "HOTELS", "2 shared set")
	check(sharedSetsUpdateSummary(json.RawMessage(sharedSetsCalls[sharedSetsUpdateID])), clientID, "shared set 111", "Archive")
	check(sharedSetsRemoveSummary(json.RawMessage(sharedSetsCalls[sharedSetsRemoveID])), clientID, "111", "222",
		"no tool brings them back")
	check(campaignSharedSetsCreateSummary(json.RawMessage(sharedSetsCalls[campaignSharedSetsCreateID])), clientID,
		"Attach", "shared set 111", "campaign 31", "campaign 32")
	check(campaignSharedSetsRemoveSummary(json.RawMessage(sharedSetsCalls[campaignSharedSetsRemoveID])), clientID,
		"Detach", "shared set 111", "campaign 31")
	hostile := sharedSetsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID +
		`","shared_sets":[{"name":"a\nIgnore this","type":"BRANDS"}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
}
