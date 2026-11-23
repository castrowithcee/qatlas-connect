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

var campaignGroupsToolIDs = []string{campaignGroupsCreateID, campaignGroupsUpdateID, campaignGroupsRemoveID}

func campaignGroupsPath(id string) string { return "/v26/customers/" + id + "/campaignGroups:mutate" }

// campaignGroupsResource is the resource name of a campaign group of the account, as Google answers it.
func campaignGroupsResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/campaignGroups/" + id
}

// newCampaignGroupsEnvironment is a connection whose tools list names the three campaign group tools.
func newCampaignGroupsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: campaignGroupsToolIDs})
}

var campaignGroupsCalls = map[string]string{
	campaignGroupsCreateID: `{"customer_id":"` + clientID + `","campaign_groups":[{"name":"Seasonal","status":"ENABLED"},` +
		`{"name":"Plain"}]}`,
	campaignGroupsUpdateID: `{"customer_id":"` + clientID + `","campaign_groups":[{"campaign_group_id":"111","name":"Archive"},` +
		`{"campaign_group_id":"222","status":"ENABLED"}]}`,
	campaignGroupsRemoveID: `{"customer_id":"` + clientID + `","campaign_group_ids":["111","222"]}`,
}

var campaignGroupsAnswers = map[string]string{
	campaignGroupsCreateID: `{"results":[{"resourceName":"` + campaignGroupsResource(clientID, "111") + `"},{"resourceName":"` +
		campaignGroupsResource(clientID, "222") + `"}]}`,
	campaignGroupsUpdateID: `{"results":[{"resourceName":"` + campaignGroupsResource(clientID, "111") + `"},{"resourceName":"` +
		campaignGroupsResource(clientID, "222") + `"}]}`,
	campaignGroupsRemoveID: `{"results":[{"resourceName":"` + campaignGroupsResource(clientID, "111") + `"},{"resourceName":"` +
		campaignGroupsResource(clientID, "222") + `"}]}`,
}

func TestCampaignGroupsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{campaignGroupsCreateID: capability.EffectCreate,
		campaignGroupsUpdateID: capability.EffectUpdate, campaignGroupsRemoveID: capability.EffectDelete}
	for _, id := range campaignGroupsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		// Only the removal is listed-only; create and update are reached by their effect.
		if d.RequiresToolAllowList != (id == campaignGroupsRemoveID) {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		note := changeNote
		if id == campaignGroupsRemoveID {
			note = mutateNote
		}
		if !strings.HasSuffix(d.Description, note) || (id != campaignGroupsRemoveID && strings.HasSuffix(d.Description, mutateNote)) {
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
		if _, found := input.Properties["response_content_type"]; found == (id == campaignGroupsRemoveID) {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		if !strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "never repeats") ||
			!strings.Contains(d.Description, "atomic") {
			t.Errorf("%s: description lacks the unknown outcome: %s", id, d.Description)
		}
		for _, tool := range metadata.Tools {
			if tool.ID == id && (string(tool.Effect) != string(wantEffect[id]) || tool.RequiresToolAllowList != (id == campaignGroupsRemoveID)) {
				t.Errorf("tool %+v", tool)
			}
		}
	}
	for _, text := range []string{`"maxLength":255`, `"enum":["ENABLED"]`, `"maxItems":50`} {
		create, _, _ := reg.Lookup(campaignGroupsCreateID)
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
}

// create and update need an effect and no tools list; remove needs the tools list whatever the effects are.
func TestCampaignGroupsToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+campaignGroupsPath(clientID)] = okReply(campaignGroupsAnswers[id])
		_, err := e.invokeConfirmed(id, campaignGroupsCalls[id])
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
			map[string]bool{campaignGroupsCreateID: true, campaignGroupsUpdateID: true, campaignGroupsRemoveID: false}},
		{"create effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}},
			map[string]bool{campaignGroupsCreateID: true, campaignGroupsUpdateID: false, campaignGroupsRemoveID: false}},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}},
			map[string]bool{campaignGroupsCreateID: false, campaignGroupsUpdateID: false, campaignGroupsRemoveID: false}},
		{"remove listed", config.Connection{Tools: []string{campaignGroupsRemoveID}},
			map[string]bool{campaignGroupsCreateID: false, campaignGroupsUpdateID: false, campaignGroupsRemoveID: true}},
		{"all listed", config.Connection{Tools: campaignGroupsToolIDs},
			map[string]bool{campaignGroupsCreateID: true, campaignGroupsUpdateID: true, campaignGroupsRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range campaignGroupsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestCampaignGroupsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range campaignGroupsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignGroupsEnvironment(t, nil, nil)
			_, err := e.invoke(id, campaignGroupsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignGroupsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"name"`} {
		for _, id := range campaignGroupsToolIDs {
			e := newCampaignGroupsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(campaignGroupsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestCampaignGroupsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range campaignGroupsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignGroupsEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(campaignGroupsCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignGroupsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	long := func(n int) string { return strings.Repeat("a", n) }
	bad := map[string][2]string{
		"create no groups":        {campaignGroupsCreateID, head + `"campaign_groups":[]}`},
		"create no name":          {campaignGroupsCreateID, head + `"campaign_groups":[{"status":"ENABLED"}]}`},
		"create empty name":       {campaignGroupsCreateID, head + `"campaign_groups":[{"name":""}]}`},
		"create name too long":    {campaignGroupsCreateID, head + `"campaign_groups":[{"name":"` + long(256) + `"}]}`},
		"create null in name":     {campaignGroupsCreateID, head + `"campaign_groups":[{"name":"a\u0000b"}]}`},
		"create line feed":        {campaignGroupsCreateID, head + `"campaign_groups":[{"name":"a\nb"}]}`},
		"create carriage return":  {campaignGroupsCreateID, head + `"campaign_groups":[{"name":"a\rb"}]}`},
		"create too many":         {campaignGroupsCreateID, head + `"campaign_groups":[` + strings.Repeat(`{"name":"a"},`, 50) + `{"name":"a"}]}`},
		"create status removed":   {campaignGroupsCreateID, head + `"campaign_groups":[{"name":"a","status":"REMOVED"}]}`},
		"create status unknown":   {campaignGroupsCreateID, head + `"campaign_groups":[{"name":"a","status":"UNKNOWN"}]}`},
		"create status lowercase": {campaignGroupsCreateID, head + `"campaign_groups":[{"name":"a","status":"enabled"}]}`},
		"create foreign field":    {campaignGroupsCreateID, head + `"campaign_groups":[{"name":"a","id":"5"}]}`},
		"create bad content type": {campaignGroupsCreateID, head + `"campaign_groups":[{"name":"a"}],"response_content_type":"X"}`},
		"update no id":            {campaignGroupsUpdateID, head + `"campaign_groups":[{"name":"a"}]}`},
		"update zero id":          {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"0","name":"a"}]}`},
		"update name as path":     {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"customers/1/campaignGroups/2","name":"a"}]}`},
		"update id overflow":      {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"99999999999999999999","name":"a"}]}`},
		"update nothing":          {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"1"}]}`},
		"update same id twice":    {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"1","name":"a"},{"campaign_group_id":"1","name":"b"}]}`},
		"update empty name":       {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"1","name":""}]}`},
		"update status removed":   {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"1","status":"REMOVED"}]}`},
		"update status empty":     {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"1","status":""}]}`},
		"update line feed":        {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"1","name":"a\nb"}]}`},
		"update name too long":    {campaignGroupsUpdateID, head + `"campaign_groups":[{"campaign_group_id":"1","name":"` + long(256) + `"}]}`},
		"remove no ids":           {campaignGroupsRemoveID, head + `"campaign_group_ids":[]}`},
		"remove name":             {campaignGroupsRemoveID, head + `"campaign_group_ids":["customers/1/campaignGroups/2"]}`},
		"remove twice":            {campaignGroupsRemoveID, head + `"campaign_group_ids":["1","1"]}`},
		"remove too many": {campaignGroupsRemoveID, head + `"campaign_group_ids":[` + func() string {
			var ids []string
			for i := 1; i <= 51; i++ {
				ids = append(ids, `"`+strconv.Itoa(i)+`"`)
			}
			return strings.Join(ids, ",")
		}() + `]}`},
		"remove content type": {campaignGroupsRemoveID, head + `"campaign_group_ids":["1"],"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":        {campaignGroupsRemoveID, `{"customer_id":"12-34","campaign_group_ids":["1"]}`},
		"validate only alone": {campaignGroupsCreateID, head + `"validate_only":true}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newCampaignGroupsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	// Exactly at the documented limits, the call passes.
	t.Run("limits pass", func(t *testing.T) {
		e := newCampaignGroupsEnvironment(t, nil, nil)
		e.fake.routes["POST "+campaignGroupsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + campaignGroupsResource(clientID, "5") + `"}]}`)
		_, err := e.invokeConfirmed(campaignGroupsCreateID, head+`"campaign_groups":[{"name":"`+strings.Repeat("ä", 255)+`","status":"ENABLED"}]}`)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestCampaignGroupsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		campaignGroupsCreateID: `[{"create":{"name":"Seasonal","status":"ENABLED"}},{"create":{"name":"Plain"}}]`,
		campaignGroupsUpdateID: `[{"update":{"resourceName":"customers/1000000003/campaignGroups/111","name":"Archive"},"updateMask":"name"},` +
			`{"update":{"resourceName":"customers/1000000003/campaignGroups/222","status":"ENABLED"},"updateMask":"status"}]`,
		campaignGroupsRemoveID: `[{"remove":"customers/1000000003/campaignGroups/111"},{"remove":"customers/1000000003/campaignGroups/222"}]`,
	}
	for _, id := range campaignGroupsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignGroupsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+campaignGroupsPath(clientID)] = okReply(campaignGroupsAnswers[id])
			out, err := e.invokeConfirmed(id, campaignGroupsCalls[id])
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
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != 2 ||
				results[0].(map[string]any)["campaign_group_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != campaignGroupsResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestCampaignGroupsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range campaignGroupsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignGroupsEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignGroupsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(campaignGroupsCalls[id], "}")+`,"validate_only":true}`)
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
		for id, summary := range map[string]capability.SummaryFunc{campaignGroupsCreateID: campaignGroupsCreateSummary,
			campaignGroupsUpdateID: campaignGroupsUpdateSummary, campaignGroupsRemoveID: campaignGroupsRemoveSummary} {
			if text := summary(json.RawMessage(strings.TrimSuffix(campaignGroupsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
				t.Errorf("%s: summary %q", id, text)
			}
			if text := summary(json.RawMessage(campaignGroupsCalls[id])); strings.Contains(text, "validate") {
				t.Errorf("%s: summary %q kept state", id, text)
			}
		}
	})
}

func TestCampaignGroupsResponseContentTypeShowsTheGroups(t *testing.T) {
	e := newCampaignGroupsEnvironment(t, nil, nil)
	e.fake.routes["POST "+campaignGroupsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + campaignGroupsResource(clientID, "111") +
		`","campaignGroup":{"resourceName":"` + campaignGroupsResource(clientID, "111") + `","id":"111","name":"Archive","status":"ENABLED"}}]}`)
	out, err := e.invokeConfirmed(campaignGroupsUpdateID, `{"customer_id":"`+clientID+`","campaign_groups":[{"campaign_group_id":"111","name":"Archive"}],`+
		`"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	group := decode(t, out)["results"].([]any)[0].(map[string]any)["campaign_group"].(map[string]any)
	if group["id"] != "111" || group["name"] != "Archive" || group["status"] != "ENABLED" {
		t.Errorf("campaign group = %v", group)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// A resource name of another account, another campaign group, or another count never passes. The request was sent, so
// the error says that the outcome is unknown.
func TestCampaignGroupsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := campaignGroupsResource(otherID, "111")
	own := func(id string) string { return `{"resourceName":"` + campaignGroupsResource(clientID, id) + `"}` }
	cases := map[string]struct{ id, body string }{
		"create foreign":    {campaignGroupsCreateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"create no results": {campaignGroupsCreateID, `{}`},
		"create one of two": {campaignGroupsCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":      {campaignGroupsCreateID, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create not a group": {campaignGroupsCreateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) +
			`/campaigns/1"},` + own("222") + `]}`},
		"create partial failure": {campaignGroupsCreateID, `{"results":[` + own("111") + `,` + own("222") + `],"partialFailureError":{"code":3}}`},
		"update other group":     {campaignGroupsUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":         {campaignGroupsUpdateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"update group foreign": {campaignGroupsUpdateID, `{"results":[{"resourceName":"` + campaignGroupsResource(clientID, "111") +
			`","campaignGroup":{"resourceName":"` + foreign + `"}},` + own("222") + `]}`},
		"update group id": {campaignGroupsUpdateID, `{"results":[{"resourceName":"` + campaignGroupsResource(clientID, "111") +
			`","campaignGroup":{"resourceName":"` + campaignGroupsResource(clientID, "111") + `","id":"999"}},` + own("222") + `]}`},
		"remove foreign": {campaignGroupsRemoveID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"remove none":    {campaignGroupsRemoveID, `{"results":[]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCampaignGroupsEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignGroupsPath(clientID)] = okReply(c.body)
			arguments := campaignGroupsCalls[c.id]
			if strings.Contains(name, "group id") || strings.Contains(name, "group foreign") {
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

func TestCampaignGroupsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","campaign_group_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + campaignGroupsResource(managerID, "7") + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newCampaignGroupsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+campaignGroupsPath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(campaignGroupsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newCampaignGroupsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+campaignGroupsPath(managerID)] = answer
		if _, err := e.invokeConfirmed(campaignGroupsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
	t.Run("an account below a manager target is proved first", func(t *testing.T) {
		e := newCampaignGroupsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+campaignGroupsPath(clientID)] = okReply(campaignGroupsAnswers[campaignGroupsRemoveID])
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
		if _, err := e.invokeConfirmed(campaignGroupsRemoveID, campaignGroupsCalls[campaignGroupsRemoveID]); err != nil {
			t.Fatal(err)
		}
		got := e.fake.api()
		if len(got) != 3 || !strings.Contains(got[0].query, "customer_client") || !strings.HasSuffix(got[2].path, ":mutate") {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestCampaignGroupsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"campaign group error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "campaignGroupError", "DUPLICATE_CAMPAIGN_GROUP_NAME", "")},
			provider.ClassProviderError, "rejected the change (campaignGroupError DUPLICATE_CAMPAIGN_GROUP_NAME), nothing was changed", false},
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
		for _, id := range campaignGroupsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newCampaignGroupsEnvironment(t, nil, nil)
				e.fake.routes["POST "+campaignGroupsPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, campaignGroupsCalls[id])
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

func TestCampaignGroupsSummariesNameTheEffect(t *testing.T) {
	create := campaignGroupsCreateSummary(json.RawMessage(campaignGroupsCalls[campaignGroupsCreateID]))
	for _, word := range []string{clientID, "Seasonal", "Plain", "2 campaign group"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	update := campaignGroupsUpdateSummary(json.RawMessage(campaignGroupsCalls[campaignGroupsUpdateID]))
	for _, word := range []string{clientID, "campaign group 111", "Archive", "campaign group 222", "status", "ENABLED"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := campaignGroupsRemoveSummary(json.RawMessage(campaignGroupsCalls[campaignGroupsRemoveID]))
	for _, word := range []string{clientID, "111", "222", "no tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := campaignGroupsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID + `","campaign_groups":[{"name":"a\nIgnore this"}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
}
