package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var campaignDraftsMutateIDs = []string{campaignDraftsCreateID, campaignDraftsUpdateID, campaignDraftsRemoveID}

var campaignDraftsToolIDs = append(slices.Clone(campaignDraftsMutateIDs), campaignDraftsPromoteID)

func campaignDraftsPath(id string) string { return "/v26/customers/" + id + "/campaignDrafts:mutate" }

func campaignDraftName(customer, base, draft string) string {
	return "customers/" + queryNumber(customer) + "/campaignDrafts/" + base + "~" + draft
}

func campaignDraftsRoute(customer, base, draft, method string) string {
	return "/v26/" + campaignDraftName(customer, base, draft) + ":" + method
}

// newCampaignDraftsEnvironment is a connection whose tools list names the four changing draft tools.
func newCampaignDraftsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: campaignDraftsToolIDs})
}

var (
	draftOne = campaignDraftName(clientID, "21", "5")
	draftTwo = campaignDraftName(clientID, "22", "6")
	baseOne  = "customers/" + clientID + "/campaigns/21"
	baseTwo  = "customers/" + clientID + "/campaigns/22"
)

var campaignDraftsCalls = map[string]string{
	campaignDraftsCreateID: `{"customer_id":"` + clientID + `","campaign_drafts":[{"base_campaign":"` + baseOne +
		`","name":"Spring changes"},{"base_campaign":"` + baseTwo + `","name":"Autumn changes"}]}`,
	campaignDraftsUpdateID: `{"customer_id":"` + clientID + `","campaign_drafts":[{"campaign_draft":"` + draftOne +
		`","name":"Renamed"},{"campaign_draft":"` + draftTwo + `","name":"Renamed too"}]}`,
	campaignDraftsRemoveID:  `{"customer_id":"` + clientID + `","campaign_drafts":["` + draftOne + `","` + draftTwo + `"]}`,
	campaignDraftsPromoteID: `{"customer_id":"` + clientID + `","campaign_draft":"` + draftOne + `"}`,
}

var campaignDraftsAnswers = map[string]string{
	campaignDraftsCreateID:  `{"results":[{"resourceName":"` + draftOne + `"},{"resourceName":"` + draftTwo + `"}]}`,
	campaignDraftsUpdateID:  `{"results":[{"resourceName":"` + draftOne + `"},{"resourceName":"` + draftTwo + `"}]}`,
	campaignDraftsRemoveID:  `{"results":[{"resourceName":"` + draftOne + `"},{"resourceName":"` + draftTwo + `"}]}`,
	campaignDraftsPromoteID: `{"name":"customers/` + clientID + `/operations/abc","done":true,"response":{},"metadata":{}}`,
}

// campaignDraftsRouteOf is where the fake answers a tool.
func campaignDraftsRouteOf(id string) string {
	if id == campaignDraftsPromoteID {
		return "POST " + campaignDraftsRoute(clientID, "21", "5", "promote")
	}
	return "POST " + campaignDraftsPath(clientID)
}

// campaignDraftsPosts are the POST requests of the fake: the Mutate calls and the promotion.
func campaignDraftsPosts(e *environment) []seen {
	var out []seen
	for _, s := range e.fake.api() {
		if s.method == http.MethodPost && !strings.HasSuffix(s.path, ":search") {
			out = append(out, s)
		}
	}
	return out
}

func TestCampaignDraftsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{campaignDraftsCreateID: capability.EffectCreate,
		campaignDraftsUpdateID: capability.EffectUpdate, campaignDraftsRemoveID: capability.EffectDelete,
		campaignDraftsPromoteID: capability.EffectExecute}
	for _, id := range campaignDraftsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		listedOnly := id == campaignDraftsRemoveID || id == campaignDraftsPromoteID
		if d.RequiresToolAllowList != listedOnly {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "path", "url",
			"resource_name", "update_mask", "response_content_type", "status"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
	}
	for _, id := range campaignDraftsMutateIDs {
		d, _, _ := reg.Lookup(id)
		note := mutateListNote
		if id == campaignDraftsRemoveID {
			note = mutateListListedNote
		}
		if !strings.HasSuffix(d.Description, note) || (id != campaignDraftsRemoveID && strings.HasSuffix(d.Description, mutateListListedNote)) {
			t.Errorf("%s: description does not end on the note of how it is offered", id)
		}
	}
	if d, _, _ := reg.Lookup(campaignDraftsPromoteID); !strings.HasSuffix(d.Description, mutateNote) ||
		d.Risk.Idempotency != capability.IdempotencyNonIdempotent {
		t.Errorf("promote = %+v", d)
	}
	d, _, ok := reg.Lookup(campaignDraftsListAsyncErrorsID)
	if !ok || d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone ||
		d.RequiresToolAllowList || !slices.Contains(metadata.Profiles[0].Tools, campaignDraftsListAsyncErrorsID) ||
		len(d.OutputSchema) == 0 {
		t.Fatalf("list-async-errors = %+v", d)
	}
	for _, id := range append(slices.Clone(campaignDraftsToolIDs), campaignDraftsListAsyncErrorsID) {
		if got := metadata.Token.Scopes[id]; !reflect.DeepEqual(got, []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, got)
		}
	}
	create, _, _ := reg.Lookup(campaignDraftsCreateID)
	for _, text := range []string{`"maxLength":255`, `"maxItems":10`, `"base_campaign"`} {
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
}

func TestCampaignDraftsToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes[campaignDraftsRouteOf(id)] = okReply(campaignDraftsAnswers[id])
		_, err := e.invokeConfirmed(id, campaignDraftsCalls[id])
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
		{"all effects, no tools list", config.Connection{Effects: all}, map[string]bool{campaignDraftsCreateID: true,
			campaignDraftsUpdateID: true, campaignDraftsRemoveID: false, campaignDraftsPromoteID: false}},
		{"promote listed", config.Connection{Tools: []string{campaignDraftsPromoteID}},
			map[string]bool{campaignDraftsCreateID: false, campaignDraftsUpdateID: false, campaignDraftsRemoveID: false,
				campaignDraftsPromoteID: true}},
		{"remove listed", config.Connection{Tools: []string{campaignDraftsRemoveID}},
			map[string]bool{campaignDraftsCreateID: false, campaignDraftsUpdateID: false, campaignDraftsRemoveID: true,
				campaignDraftsPromoteID: false}},
		{"all listed", config.Connection{Tools: campaignDraftsToolIDs}, map[string]bool{campaignDraftsCreateID: true,
			campaignDraftsUpdateID: true, campaignDraftsRemoveID: true, campaignDraftsPromoteID: true}},
	}
	for _, c := range cases {
		for _, id := range campaignDraftsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestCampaignDraftsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range campaignDraftsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignDraftsEnvironment(t, nil, nil)
			_, err := e.invoke(id, campaignDraftsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignDraftsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"name"`, `"response_content_type":"MUTABLE_RESOURCE"`, `"status":"REMOVED"`} {
		for _, id := range campaignDraftsToolIDs {
			e := newCampaignDraftsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(campaignDraftsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestCampaignDraftsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	calls := map[string]string{campaignDraftsListAsyncErrorsID: `{"customer_id":"` + clientID + `","campaign_draft":"` + draftOne + `"}`}
	for id, call := range campaignDraftsCalls {
		calls[id] = call
	}
	for id, call := range calls {
		t.Run(id, func(t *testing.T) {
			e := newCampaignDraftsEnvironment(t, []string{"customers/" + clientID}, nil)
			if id == campaignDraftsListAsyncErrorsID {
				e = newEnvironment(t, []string{"customers/" + clientID}, nil)
			}
			_, err := e.invokeConfirmed(id, strings.ReplaceAll(call, clientID, otherID))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestCampaignDraftsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	foreignBase := "customers/" + otherID + "/campaigns/21"
	foreignDraft := campaignDraftName(otherID, "21", "5")
	create := func(entry string) string { return head + `"campaign_drafts":[` + entry + `]}` }
	draftsOf := func(entries string) string { return head + `"campaign_drafts":[` + entries + `]}` }
	many := func(entry string, n int) string { return strings.TrimSuffix(strings.Repeat(entry+",", n), ",") }
	bad := map[string][2]string{
		"create none":            {campaignDraftsCreateID, create(``)},
		"create no base":         {campaignDraftsCreateID, create(`{"name":"a"}`)},
		"create no name":         {campaignDraftsCreateID, create(`{"base_campaign":"` + baseOne + `"}`)},
		"create empty name":      {campaignDraftsCreateID, create(`{"base_campaign":"` + baseOne + `","name":""}`)},
		"create long name":       {campaignDraftsCreateID, create(`{"base_campaign":"` + baseOne + `","name":"` + strings.Repeat("a", 256) + `"}`)},
		"create line feed":       {campaignDraftsCreateID, create(`{"base_campaign":"` + baseOne + `","name":"a\nb"}`)},
		"create carriage return": {campaignDraftsCreateID, create(`{"base_campaign":"` + baseOne + `","name":"a\rb"}`)},
		"create nul":             {campaignDraftsCreateID, create(`{"base_campaign":"` + baseOne + `","name":"a\u0000b"}`)},
		"create foreign base":    {campaignDraftsCreateID, create(`{"base_campaign":"` + foreignBase + `","name":"a"}`)},
		"create base as id":      {campaignDraftsCreateID, create(`{"base_campaign":"21","name":"a"}`)},
		"create temporary base":  {campaignDraftsCreateID, create(`{"base_campaign":"customers/` + clientID + `/campaigns/-1","name":"a"}`)},
		"create zero base":       {campaignDraftsCreateID, create(`{"base_campaign":"customers/` + clientID + `/campaigns/0","name":"a"}`)},
		"create base overflow":   {campaignDraftsCreateID, create(`{"base_campaign":"customers/` + clientID + `/campaigns/99999999999999999999","name":"a"}`)},
		"create draft as base":   {campaignDraftsCreateID, create(`{"base_campaign":"` + draftOne + `","name":"a"}`)},
		"create status":          {campaignDraftsCreateID, create(`{"base_campaign":"` + baseOne + `","name":"a","status":"PROPOSED"}`)},
		"create too many":        {campaignDraftsCreateID, draftsOf(many(`{"base_campaign":"`+baseOne+`","name":"a"}`, 11))},
		"update none":            {campaignDraftsUpdateID, draftsOf(``)},
		"update no name":         {campaignDraftsUpdateID, draftsOf(`{"campaign_draft":"` + draftOne + `"}`)},
		"update empty name":      {campaignDraftsUpdateID, draftsOf(`{"campaign_draft":"` + draftOne + `","name":""}`)},
		"update foreign":         {campaignDraftsUpdateID, draftsOf(`{"campaign_draft":"` + foreignDraft + `","name":"a"}`)},
		"update id only":         {campaignDraftsUpdateID, draftsOf(`{"campaign_draft":"5","name":"a"}`)},
		"update no tilde":        {campaignDraftsUpdateID, draftsOf(`{"campaign_draft":"customers/` + clientID + `/campaignDrafts/21","name":"a"}`)},
		"update zero draft":      {campaignDraftsUpdateID, draftsOf(`{"campaign_draft":"` + campaignDraftName(clientID, "21", "0") + `","name":"a"}`)},
		"update base":            {campaignDraftsUpdateID, draftsOf(`{"campaign_draft":"` + draftOne + `","base_campaign":"` + baseTwo + `"}`)},
		"update twice":           {campaignDraftsUpdateID, draftsOf(`{"campaign_draft":"` + draftOne + `","name":"a"},{"campaign_draft":"` + draftOne + `","name":"b"}`)},
		"remove none":            {campaignDraftsRemoveID, draftsOf(``)},
		"remove foreign":         {campaignDraftsRemoveID, draftsOf(`"` + foreignDraft + `"`)},
		"remove campaign":        {campaignDraftsRemoveID, draftsOf(`"` + baseOne + `"`)},
		"remove twice":           {campaignDraftsRemoveID, draftsOf(`"` + draftOne + `","` + draftOne + `"`)},
		"remove too many":        {campaignDraftsRemoveID, draftsOf(many(`"`+draftOne+`"`, 11))},
		"promote foreign":        {campaignDraftsPromoteID, `{"customer_id":"` + clientID + `","campaign_draft":"` + foreignDraft + `"}`},
		"promote none":           {campaignDraftsPromoteID, head + `"validate_only":true}`},
		"promote list":           {campaignDraftsPromoteID, head + `"campaign_draft":["` + draftOne + `"]}`},
		"promote injected":       {campaignDraftsPromoteID, `{"customer_id":"` + clientID + `","campaign_draft":"` + draftOne + `:promote"}`},
		"bad customer":           {campaignDraftsRemoveID, `{"customer_id":"12-34","campaign_drafts":["` + draftOne + `"]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newCampaignDraftsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newCampaignDraftsEnvironment(t, nil, nil)
		e.fake.routes["POST "+campaignDraftsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + draftOne + `"}]}`)
		_, err := e.invokeConfirmed(campaignDraftsCreateID, create(`{"base_campaign":"`+baseOne+`","name":"`+strings.Repeat("ä", 255)+`"}`))
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestCampaignDraftsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		campaignDraftsCreateID: `[{"create":{"baseCampaign":"` + baseOne + `","name":"Spring changes"}},` +
			`{"create":{"baseCampaign":"` + baseTwo + `","name":"Autumn changes"}}]`,
		campaignDraftsUpdateID: `[{"update":{"resourceName":"` + draftOne + `","name":"Renamed"},"updateMask":"name"},` +
			`{"update":{"resourceName":"` + draftTwo + `","name":"Renamed too"},"updateMask":"name"}]`,
		campaignDraftsRemoveID: `[{"remove":"` + draftOne + `"},{"remove":"` + draftTwo + `"}]`,
	}
	for _, id := range campaignDraftsMutateIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignDraftsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+campaignDraftsPath(clientID)] = okReply(campaignDraftsAnswers[id])
			out, err := e.invokeConfirmed(id, campaignDraftsCalls[id])
			if err != nil {
				t.Fatal(err)
			}
			posts := campaignDraftsPosts(e)
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
				first["base_campaign_id"] != "21" || first["draft_id"] != "5" || first["resource_name"] != draftOne {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestCampaignDraftsPromoteSendsOneRequestAndReturnsTheOperation(t *testing.T) {
	e := newCampaignDraftsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes[campaignDraftsRouteOf(campaignDraftsPromoteID)] = okReply(campaignDraftsAnswers[campaignDraftsPromoteID])
	out, err := e.invokeConfirmed(campaignDraftsPromoteID, campaignDraftsCalls[campaignDraftsPromoteID])
	if err != nil {
		t.Fatal(err)
	}
	posts := campaignDraftsPosts(e)
	if len(posts) != 1 || posts[0].login != loginID || len(e.fake.api()) != 1 {
		t.Fatalf("posts = %+v", posts)
	}
	if body := bodyOf(t, posts[0]); !reflect.DeepEqual(body, map[string]any{"campaignDraft": draftOne}) {
		t.Errorf("body = %s", posts[0].body)
	}
	got := decode(t, out)
	if got["customer_id"] != clientID || got["campaign_draft"] != draftOne || got["validate_only"] != false ||
		got["operation"] != "customers/"+clientID+"/operations/abc" || got["done"] != true || got["error"] != nil {
		t.Errorf("out = %s", out)
	}

	t.Run("validate_only", func(t *testing.T) {
		e := newCampaignDraftsEnvironment(t, nil, nil)
		e.fake.routes[campaignDraftsRouteOf(campaignDraftsPromoteID)] = okReply(`{}`)
		out, err := e.invokeConfirmed(campaignDraftsPromoteID,
			strings.TrimSuffix(campaignDraftsCalls[campaignDraftsPromoteID], "}")+`,"validate_only":true}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		if body := bodyOf(t, campaignDraftsPosts(e)[0]); body["validateOnly"] != true || got["validate_only"] != true ||
			got["done"] != false || got["operation"] != nil {
			t.Errorf("body = %v, out = %s", body, out)
		}
	})
	t.Run("operation text is cut and an error is returned without details", func(t *testing.T) {
		e := newCampaignDraftsEnvironment(t, nil, nil)
		e.fake.routes[campaignDraftsRouteOf(campaignDraftsPromoteID)] = okReply(`{"name":"` + strings.Repeat("o", 5000) +
			`\u0007x","done":true,"error":{"code":9,"message":"` + strings.Repeat("m", 5000) +
			`","details":[{"note":"` + detailCanary + `"}]}}`)
		out, err := e.invokeConfirmed(campaignDraftsPromoteID, campaignDraftsCalls[campaignDraftsPromoteID])
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		failed := got["error"].(map[string]any)
		if len(got["operation"].(string)) > maxText || len(failed["message"].(string)) > maxText ||
			failed["code"] != float64(9) || strings.Contains(out, detailCanary) {
			t.Errorf("out = %.300s", out)
		}
	})
}

func TestCampaignDraftsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range campaignDraftsMutateIDs {
		t.Run(id, func(t *testing.T) {
			e := newCampaignDraftsEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignDraftsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(campaignDraftsCalls[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			body := bodyOf(t, campaignDraftsPosts(e)[0])
			got := decode(t, out)
			if body["validateOnly"] != true || got["validate_only"] != true || got["results"] != nil {
				t.Errorf("body = %v, out = %s", body, out)
			}
		})
	}
	for id, summary := range map[string]capability.SummaryFunc{campaignDraftsCreateID: campaignDraftsCreateSummary,
		campaignDraftsUpdateID: campaignDraftsUpdateSummary, campaignDraftsRemoveID: campaignDraftsRemoveSummary,
		campaignDraftsPromoteID: campaignDraftsPromoteSummary} {
		if text := summary(json.RawMessage(strings.TrimSuffix(campaignDraftsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
			t.Errorf("%s: summary %q", id, text)
		}
		if text := summary(json.RawMessage(campaignDraftsCalls[id])); strings.Contains(text, "validate") || !strings.Contains(text, clientID) {
			t.Errorf("%s: summary %q", id, text)
		}
	}
}

func TestCampaignDraftsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := campaignDraftName(otherID, "21", "5")
	own := func(base, draft string) string {
		return `{"resourceName":"` + campaignDraftName(clientID, base, draft) + `"}`
	}
	cases := map[string]struct{ id, body string }{
		"update foreign":         {campaignDraftsUpdateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("22", "6") + `]}`},
		"update other":           {campaignDraftsUpdateID, `{"results":[` + own("22", "6") + `,` + own("21", "5") + `]}`},
		"update one of two":      {campaignDraftsUpdateID, `{"results":[` + own("21", "5") + `]}`},
		"update partial failure": {campaignDraftsUpdateID, `{"results":[` + own("21", "5") + `,` + own("22", "6") + `],"partialFailureError":{"code":3}}`},
		"remove foreign":         {campaignDraftsRemoveID, `{"results":[{"resourceName":"` + foreign + `"},` + own("22", "6") + `]}`},
		"remove not a draft": {campaignDraftsRemoveID, `{"results":[{"resourceName":"customers/` + clientID +
			`/campaigns/21"},` + own("22", "6") + `]}`},
		"remove no tilde": {campaignDraftsRemoveID, `{"results":[{"resourceName":"customers/` + clientID +
			`/campaignDrafts/21"},` + own("22", "6") + `]}`},
		"remove none":       {campaignDraftsRemoveID, `{"results":[]}`},
		"create twice":      {campaignDraftsCreateID, `{"results":[` + own("21", "5") + `,` + own("21", "5") + `]}`},
		"create foreign":    {campaignDraftsCreateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("22", "6") + `]}`},
		"create other base": {campaignDraftsCreateID, `{"results":[` + own("21", "5") + `,` + own("23", "6") + `]}`},
		"create none":       {campaignDraftsCreateID, `{}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newCampaignDraftsEnvironment(t, nil, nil)
			e.fake.routes["POST "+campaignDraftsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, campaignDraftsCalls[c.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(campaignDraftsPosts(e)) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(campaignDraftsPosts(e)))
			}
		})
	}
}

func TestCampaignDraftsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","campaign_draft":"` + campaignDraftName(managerID, "21", "5") + `"}`
	route := "POST " + campaignDraftsRoute(managerID, "21", "5", "promote")
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newCampaignDraftsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes[route] = okReply(`{"done":true}`)
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(campaignDraftsPromoteID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(campaignDraftsPosts(e)) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(campaignDraftsPosts(e)))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newCampaignDraftsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes[route] = okReply(`{"done":true}`)
		if _, err := e.invokeConfirmed(campaignDraftsPromoteID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestCampaignDraftsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"draft error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "campaignDraftError", "DUPLICATE_DRAFT_NAME", "")},
			provider.ClassProviderError, "rejected the change (campaignDraftError DUPLICATE_DRAFT_NAME), nothing was changed", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"promote answer is not json", reply{status: 200, body: `<html>`}, provider.ClassInvalidResponse,
			"outcome of the change is unknown", true},
	}
	for _, c := range cases {
		for _, id := range campaignDraftsToolIDs {
			if c.name == "promote answer is not json" && id != campaignDraftsPromoteID {
				continue
			}
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newCampaignDraftsEnvironment(t, nil, nil)
				e.fake.routes[campaignDraftsRouteOf(id)] = c.reply
				_, err := e.invokeConfirmed(id, campaignDraftsCalls[id])
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
				if len(campaignDraftsPosts(e)) != 1 || len(e.fake.api()) != 1 {
					t.Errorf("mutate requests = %d of %d, want one and no repeat", len(campaignDraftsPosts(e)), len(e.fake.api()))
				}
			})
		}
	}
}

// --- list-async-errors ---

func TestCampaignDraftsListAsyncErrorsReadsPagesBoundToTheDraft(t *testing.T) {
	call := `{"customer_id":"` + clientID + `","campaign_draft":"` + draftOne + `","page_size":2}`
	route := campaignDraftsRoute(clientID, "21", "5", "listAsyncErrors")
	e := newEnvironment(t, nil, nil)
	e.fake.answer = func(s seen) *reply {
		if s.path != route {
			return nil
		}
		if strings.Contains(s.url, "pageToken=tok-1") {
			r := okReply(`{"errors":[{"code":3,"message":"third"}]}`)
			return &r
		}
		r := okReply(`{"errors":[{"code":9,"message":"first\u0007line","details":[{"@type":"x","note":"` + detailCanary + `"}]},` +
			`{"message":"second"}],"nextPageToken":"tok-1"}`)
		return &r
	}
	out, err := e.invoke(campaignDraftsListAsyncErrorsID, call)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, detailCanary) {
		t.Errorf("details were returned: %s", out)
	}
	first := decode(t, out)
	errs := first["errors"].([]any)
	if first["customer_id"] != clientID || len(errs) != 2 || errs[0].(map[string]any)["code"] != float64(9) ||
		errs[0].(map[string]any)["message"] != "first line" || errs[1].(map[string]any)["code"] != float64(0) {
		t.Fatalf("out = %s", out)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" || cursor == "tok-1" {
		t.Fatalf("cursor = %q", cursor)
	}
	api := e.fake.api()
	if last := api[len(api)-1]; last.method != http.MethodGet || !strings.Contains(last.url, "pageSize=2") ||
		strings.Contains(last.url, "pageToken") {
		t.Errorf("first request = %+v", last)
	}
	out, err = e.invoke(campaignDraftsListAsyncErrorsID, strings.TrimSuffix(call, "}")+`,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if last := decode(t, out); len(last["errors"].([]any)) != 1 || last["next_cursor"] != nil {
		t.Errorf("last page = %s", out)
	}
	// A cursor of one draft does not continue another.
	before := len(e.fake.api())
	_, err = e.invoke(campaignDraftsListAsyncErrorsID, `{"customer_id":"`+clientID+`","campaign_draft":"`+draftTwo+
		`","page_size":2,"cursor":"`+cursor+`"}`)
	if !isInvalidRequest(err) || len(e.fake.api()) != before {
		t.Errorf("foreign cursor: err = %v", err)
	}
}

func TestCampaignDraftsListAsyncErrorsChecksItsArgumentsBeforeIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	for name, call := range map[string]string{
		"no draft":        head + `"page_size":5}`,
		"id only":         head + `"campaign_draft":"5"}`,
		"foreign draft":   head + `"campaign_draft":"` + campaignDraftName(otherID, "21", "5") + `"}`,
		"page size zero":  head + `"campaign_draft":"` + draftOne + `","page_size":0}`,
		"page size large": head + `"campaign_draft":"` + draftOne + `","page_size":1001}`,
		"forged cursor":   head + `"campaign_draft":"` + draftOne + `","cursor":"forged"}`,
		"page token":      head + `"campaign_draft":"` + draftOne + `","page_token":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invoke(campaignDraftsListAsyncErrorsID, call); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestCampaignDraftsListAsyncErrorsMapsErrors(t *testing.T) {
	call := `{"customer_id":"` + clientID + `","campaign_draft":"` + draftOne + `"}`
	e := newEnvironment(t, nil, nil)
	e.fake.routes["GET "+campaignDraftsRoute(clientID, "21", "5", "listAsyncErrors")] = reply{status: 400,
		body: adsFailure(400, "INVALID_ARGUMENT", "campaignDraftError", "CANNOT_LIST_ERRORS_BEFORE_PROMOTE", "")}
	if _, err := e.invoke(campaignDraftsListAsyncErrorsID, call); err == nil || strings.Contains(err.Error(), detailCanary) {
		t.Fatalf("err = %v", err)
	}
}
