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

var experimentsToolIDs = []string{experimentsCreateID, experimentsUpdateID, experimentsRemoveID}

func experimentsPath(id string) string { return "/v26/customers/" + id + "/experiments:mutate" }

func experimentsResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/experiments/" + id
}

func asyncErrorsPath(customer, id string) string {
	return "/v26/" + experimentsResource(customer, id) + ":listExperimentAsyncErrors"
}

// newExperimentsEnvironment is a connection whose tools list names the three changing experiment tools.
func newExperimentsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: experimentsToolIDs})
}

var experimentsCalls = map[string]string{
	experimentsCreateID: `{"customer_id":"` + clientID + `","experiments":[{"name":"Bidding test","type":"SEARCH_CUSTOM",` +
		`"description":"Compare bidding","suffix":"-exp","status":"SETUP","start_date":"2030-01-02","end_date":"2030-02-03",` +
		`"sync_enabled":true,"goals":[{"metric":"CONVERSIONS","direction":"INCREASE"}]},` +
		`{"name":"Assets","type":"OPTIMIZE_ASSETS","optimize_assets_experiment_subtype":"COMPARE_ASSETS"},` +
		`{"name":"Clips","type":"YOUTUBE_CUSTOM","video_experiment_subtype":"ASSET"}]}`,
	experimentsUpdateID: `{"customer_id":"` + clientID + `","experiments":[{"experiment_id":"111","name":"Renamed",` +
		`"goals":[{"metric":"CLICKS","direction":"NO_CHANGE_OR_INCREASE"}]},` +
		`{"experiment_id":"222","status":"HALTED","end_date":"2030-03-04"}]}`,
	experimentsRemoveID: `{"customer_id":"` + clientID + `","experiment_ids":["111","222"]}`,
}

var experimentsAnswers = map[string]string{
	experimentsCreateID: `{"results":[{"resourceName":"` + experimentsResource(clientID, "111") + `"},{"resourceName":"` +
		experimentsResource(clientID, "222") + `"},{"resourceName":"` + experimentsResource(clientID, "333") + `"}]}`,
	experimentsUpdateID: `{"results":[{"resourceName":"` + experimentsResource(clientID, "111") + `"},{"resourceName":"` +
		experimentsResource(clientID, "222") + `"}]}`,
	experimentsRemoveID: `{"results":[{"resourceName":"` + experimentsResource(clientID, "111") + `"},{"resourceName":"` +
		experimentsResource(clientID, "222") + `"}]}`,
}

func TestExperimentsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{experimentsCreateID: capability.EffectCreate,
		experimentsUpdateID: capability.EffectUpdate, experimentsRemoveID: capability.EffectDelete}
	for _, id := range experimentsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		if d.RequiresToolAllowList != (id == experimentsRemoveID) {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		note := mutateListNote
		if id == experimentsRemoveID {
			note = mutateListListedNote
		}
		if !strings.HasSuffix(d.Description, note) || (id != experimentsRemoveID && strings.HasSuffix(d.Description, mutateListListedNote)) {
			t.Errorf("%s: description does not end on the note of how it is offered", id)
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
			"resource_name", "update_mask", "response_content_type"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
	}
	d, _, ok := reg.Lookup(experimentsListAsyncErrorsID)
	if !ok || d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone ||
		d.RequiresToolAllowList || !slices.Contains(metadata.Profiles[0].Tools, experimentsListAsyncErrorsID) ||
		len(d.OutputSchema) == 0 {
		t.Fatalf("list-async-errors = %+v", d)
	}
	for _, id := range append(slices.Clone(experimentsToolIDs), experimentsListAsyncErrorsID) {
		if got := metadata.Token.Scopes[id]; !reflect.DeepEqual(got, []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, got)
		}
	}
	create, _, _ := reg.Lookup(experimentsCreateID)
	for _, text := range []string{`"maxLength":1024`, `"maxLength":2048`, `"maxItems":10`, `"SEARCH_CUSTOM"`, `"CONVERSIONS"`} {
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
	for _, unknown := range []string{`"UNKNOWN"`, `"UNSPECIFIED"`, `"REMOVED"`, `"PROMOTED"`, `"GRADUATED"`} {
		if strings.Contains(string(create.InputSchema), unknown) {
			t.Errorf("create schema offers %s", unknown)
		}
	}
}

func TestExperimentsToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+experimentsPath(clientID)] = okReply(experimentsAnswers[id])
		_, err := e.invokeConfirmed(id, experimentsCalls[id])
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
			map[string]bool{experimentsCreateID: true, experimentsUpdateID: true, experimentsRemoveID: false}},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}},
			map[string]bool{experimentsCreateID: false, experimentsUpdateID: false, experimentsRemoveID: false}},
		{"remove listed", config.Connection{Tools: []string{experimentsRemoveID}},
			map[string]bool{experimentsCreateID: false, experimentsUpdateID: false, experimentsRemoveID: true}},
		{"all listed", config.Connection{Tools: experimentsToolIDs},
			map[string]bool{experimentsCreateID: true, experimentsUpdateID: true, experimentsRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range experimentsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestExperimentsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range experimentsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExperimentsEnvironment(t, nil, nil)
			_, err := e.invoke(id, experimentsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestExperimentsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"name"`, `"response_content_type":"MUTABLE_RESOURCE"`} {
		for _, id := range experimentsToolIDs {
			e := newExperimentsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(experimentsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestExperimentsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	calls := map[string]string{experimentsListAsyncErrorsID: `{"customer_id":"` + clientID + `","experiment_id":"111"}`}
	for id, call := range experimentsCalls {
		calls[id] = call
	}
	for id, call := range calls {
		t.Run(id, func(t *testing.T) {
			e := newExperimentsEnvironment(t, []string{"customers/" + clientID}, nil)
			if id == experimentsListAsyncErrorsID {
				e = newEnvironment(t, []string{"customers/" + clientID}, nil)
			}
			_, err := e.invokeConfirmed(id, strings.Replace(call, clientID, otherID, 1))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestExperimentsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	long := func(n int) string { return strings.Repeat("a", n) }
	one := func(fields string) string {
		return head + `"experiments":[{"name":"a","type":"SEARCH_CUSTOM"` + fields + `}]}`
	}
	edit := func(fields string) string { return head + `"experiments":[{"experiment_id":"1"` + fields + `}]}` }
	bad := map[string][2]string{
		"create none":              {experimentsCreateID, head + `"experiments":[]}`},
		"create no name":           {experimentsCreateID, head + `"experiments":[{"type":"SEARCH_CUSTOM"}]}`},
		"create no type":           {experimentsCreateID, head + `"experiments":[{"name":"a"}]}`},
		"create type unknown":      {experimentsCreateID, head + `"experiments":[{"name":"a","type":"UNKNOWN"}]}`},
		"create type unspecified":  {experimentsCreateID, head + `"experiments":[{"name":"a","type":"UNSPECIFIED"}]}`},
		"create empty name":        {experimentsCreateID, head + `"experiments":[{"name":"","type":"SEARCH_CUSTOM"}]}`},
		"create name too long":     {experimentsCreateID, head + `"experiments":[{"name":"` + long(1025) + `","type":"SEARCH_CUSTOM"}]}`},
		"create line feed":         {experimentsCreateID, head + `"experiments":[{"name":"a\nb","type":"SEARCH_CUSTOM"}]}`},
		"create description long":  {experimentsCreateID, one(`,"description":"` + long(2049) + `"`)},
		"create description empty": {experimentsCreateID, one(`,"description":""`)},
		"create suffix long":       {experimentsCreateID, one(`,"suffix":"` + long(256) + `"`)},
		"create status removed":    {experimentsCreateID, one(`,"status":"REMOVED"`)},
		"create status promoted":   {experimentsCreateID, one(`,"status":"PROMOTED"`)},
		"create bad date":          {experimentsCreateID, one(`,"start_date":"2030-13-01"`)},
		"create date form":         {experimentsCreateID, one(`,"end_date":"2030/01/01"`)},
		"create end before start":  {experimentsCreateID, one(`,"start_date":"2030-02-01","end_date":"2030-01-01"`)},
		"create no goals":          {experimentsCreateID, one(`,"goals":[]`)},
		"create goal metric":       {experimentsCreateID, one(`,"goals":[{"metric":"UNKNOWN","direction":"INCREASE"}]`)},
		"create goal direction":    {experimentsCreateID, one(`,"goals":[{"metric":"CLICKS","direction":"UP"}]`)},
		"create goal no direction": {experimentsCreateID, one(`,"goals":[{"metric":"CLICKS"}]`)},
		"create too many goals": {experimentsCreateID, one(`,"goals":[` + strings.Repeat(`{"metric":"CLICKS","direction":"INCREASE"},`, 10) +
			`{"metric":"CLICKS","direction":"INCREASE"}]`)},
		"create optimize subtype wrong type": {experimentsCreateID, one(`,"optimize_assets_experiment_subtype":"COMPARE_ASSETS"`)},
		"create video subtype wrong type":    {experimentsCreateID, one(`,"video_experiment_subtype":"ASSET"`)},
		"create subtype unknown": {experimentsCreateID, head + `"experiments":[{"name":"a","type":"OPTIMIZE_ASSETS",` +
			`"optimize_assets_experiment_subtype":"UNKNOWN"}]}`},
		"create foreign field": {experimentsCreateID, one(`,"experiment_id":"5"`)},
		"create too many": {experimentsCreateID, head + `"experiments":[` + strings.Repeat(`{"name":"a","type":"SEARCH_CUSTOM"},`, 10) +
			`{"name":"a","type":"SEARCH_CUSTOM"}]}`},
		"update no id":         {experimentsUpdateID, head + `"experiments":[{"name":"a"}]}`},
		"update zero id":       {experimentsUpdateID, head + `"experiments":[{"experiment_id":"0","name":"a"}]}`},
		"update name as id":    {experimentsUpdateID, head + `"experiments":[{"experiment_id":"customers/1/experiments/2","name":"a"}]}`},
		"update id overflow":   {experimentsUpdateID, head + `"experiments":[{"experiment_id":"99999999999999999999","name":"a"}]}`},
		"update nothing":       {experimentsUpdateID, edit(``)},
		"update same id twice": {experimentsUpdateID, head + `"experiments":[{"experiment_id":"1","name":"a"},{"experiment_id":"1","name":"b"}]}`},
		"update type":          {experimentsUpdateID, edit(`,"type":"SEARCH_CUSTOM"`)},
		"update sync":          {experimentsUpdateID, edit(`,"sync_enabled":true`)},
		"update status":        {experimentsUpdateID, edit(`,"status":"GRADUATED"`)},
		"update empty name":    {experimentsUpdateID, edit(`,"name":""`)},
		"update end before":    {experimentsUpdateID, edit(`,"start_date":"2030-02-01","end_date":"2030-01-01"`)},
		"update goals empty":   {experimentsUpdateID, edit(`,"goals":[]`)},
		"remove none":          {experimentsRemoveID, head + `"experiment_ids":[]}`},
		"remove name":          {experimentsRemoveID, head + `"experiment_ids":["customers/1/experiments/2"]}`},
		"remove twice":         {experimentsRemoveID, head + `"experiment_ids":["1","1"]}`},
		"remove too many": {experimentsRemoveID, head + `"experiment_ids":[` + func() string {
			var ids []string
			for i := 1; i <= 11; i++ {
				ids = append(ids, `"`+strconv.Itoa(i)+`"`)
			}
			return strings.Join(ids, ",")
		}() + `]}`},
		"bad customer": {experimentsRemoveID, `{"customer_id":"12-34","experiment_ids":["1"]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newExperimentsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	t.Run("limits pass", func(t *testing.T) {
		e := newExperimentsEnvironment(t, nil, nil)
		e.fake.routes["POST "+experimentsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + experimentsResource(clientID, "5") + `"}]}`)
		_, err := e.invokeConfirmed(experimentsCreateID, head+`"experiments":[{"name":"`+strings.Repeat("ä", 1024)+
			`","type":"SEARCH_CUSTOM","description":"`+strings.Repeat("ä", 2048)+`","start_date":"2030-01-01","end_date":"2030-01-01"}]}`)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestExperimentsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		experimentsCreateID: `[{"create":{"name":"Bidding test","type":"SEARCH_CUSTOM","description":"Compare bidding",` +
			`"suffix":"-exp","status":"SETUP","startDate":"2030-01-02","endDate":"2030-02-03","syncEnabled":true,` +
			`"goals":[{"metric":"CONVERSIONS","direction":"INCREASE"}]}},` +
			`{"create":{"name":"Assets","type":"OPTIMIZE_ASSETS","optimizeAssetsExperiment":{"optimizeAssetsExperimentSubtype":"COMPARE_ASSETS"}}},` +
			`{"create":{"name":"Clips","type":"YOUTUBE_CUSTOM","videoExperiment":{"videoExperimentSubtype":"ASSET"}}}]`,
		experimentsUpdateID: `[{"update":{"resourceName":"customers/1000000003/experiments/111","name":"Renamed",` +
			`"goals":[{"metric":"CLICKS","direction":"NO_CHANGE_OR_INCREASE"}]},"updateMask":"name,goals"},` +
			`{"update":{"resourceName":"customers/1000000003/experiments/222","status":"HALTED","endDate":"2030-03-04"},` +
			`"updateMask":"status,endDate"}]`,
		experimentsRemoveID: `[{"remove":"customers/1000000003/experiments/111"},{"remove":"customers/1000000003/experiments/222"}]`,
	}
	for _, id := range experimentsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExperimentsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+experimentsPath(clientID)] = okReply(experimentsAnswers[id])
			out, err := e.invokeConfirmed(id, experimentsCalls[id])
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
			if got["customer_id"] != clientID || got["validate_only"] != false || len(results) != len(wantWire(t, want[id]).([]any)) ||
				results[0].(map[string]any)["experiment_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != experimentsResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestExperimentsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range experimentsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newExperimentsEnvironment(t, nil, nil)
			e.fake.routes["POST "+experimentsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(experimentsCalls[id], "}")+`,"validate_only":true}`)
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
	for id, summary := range map[string]capability.SummaryFunc{experimentsCreateID: experimentsCreateSummary,
		experimentsUpdateID: experimentsUpdateSummary, experimentsRemoveID: experimentsRemoveSummary} {
		if text := summary(json.RawMessage(strings.TrimSuffix(experimentsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
			t.Errorf("%s: summary %q", id, text)
		}
		if text := summary(json.RawMessage(experimentsCalls[id])); strings.Contains(text, "validate") || !strings.Contains(text, clientID) {
			t.Errorf("%s: summary %q", id, text)
		}
	}
}

func TestExperimentsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := experimentsResource(otherID, "111")
	own := func(id string) string { return `{"resourceName":"` + experimentsResource(clientID, id) + `"}` }
	cases := map[string]struct{ id, body string }{
		"update foreign":         {experimentsUpdateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"update other":           {experimentsUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update one of two":      {experimentsUpdateID, `{"results":[` + own("111") + `]}`},
		"update partial failure": {experimentsUpdateID, `{"results":[` + own("111") + `,` + own("222") + `],"partialFailureError":{"code":3}}`},
		"remove foreign":         {experimentsRemoveID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"remove not an experiment": {experimentsRemoveID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) +
			`/campaigns/111"},` + own("222") + `]}`},
		"remove none":    {experimentsRemoveID, `{"results":[]}`},
		"create twice":   {experimentsCreateID, `{"results":[` + own("111") + `,` + own("111") + `,` + own("333") + `]}`},
		"create foreign": {experimentsCreateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `,` + own("333") + `]}`},
		"create none":    {experimentsCreateID, `{}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newExperimentsEnvironment(t, nil, nil)
			e.fake.routes["POST "+experimentsPath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, experimentsCalls[c.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestExperimentsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","experiment_ids":["7"]}`
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newExperimentsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+experimentsPath(managerID)] = okReply(`{"results":[{"resourceName":"` +
			experimentsResource(managerID, "7") + `"}]}`)
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(experimentsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newExperimentsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+experimentsPath(managerID)] = okReply(`{"results":[{"resourceName":"` +
			experimentsResource(managerID, "7") + `"}]}`)
		if _, err := e.invokeConfirmed(experimentsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestExperimentsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"experiment error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "experimentError", "DUPLICATE_EXPERIMENT_NAME", "")},
			provider.ClassProviderError, "rejected the change (experimentError DUPLICATE_EXPERIMENT_NAME), nothing was changed", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
	}
	for _, c := range cases {
		for _, id := range experimentsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newExperimentsEnvironment(t, nil, nil)
				e.fake.routes["POST "+experimentsPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, experimentsCalls[id])
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

// --- list-async-errors ---

func TestExperimentsListAsyncErrorsReadsPagesBoundToTheExperiment(t *testing.T) {
	call := `{"customer_id":"` + clientID + `","experiment_id":"111","page_size":2}`
	e := newEnvironment(t, nil, nil)
	e.fake.answer = func(s seen) *reply {
		if s.path != asyncErrorsPath(clientID, "111") {
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
	out, err := e.invoke(experimentsListAsyncErrorsID, call)
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
	if cursor == "" || strings.Contains(cursor, "tok-1") && cursor == "tok-1" {
		t.Fatalf("cursor = %q", cursor)
	}
	api := e.fake.api()
	if api[len(api)-1].method != http.MethodGet || !strings.Contains(api[len(api)-1].url, "pageSize=2") ||
		strings.Contains(api[len(api)-1].url, "pageToken") {
		t.Errorf("first request = %+v", api[len(api)-1])
	}
	out, err = e.invoke(experimentsListAsyncErrorsID, strings.TrimSuffix(call, "}")+`,"cursor":"`+cursor+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	last := decode(t, out)
	if len(last["errors"].([]any)) != 1 || last["next_cursor"] != nil {
		t.Errorf("last page = %s", out)
	}
	// A cursor of one experiment does not continue another.
	before := len(e.fake.api())
	_, err = e.invoke(experimentsListAsyncErrorsID, `{"customer_id":"`+clientID+`","experiment_id":"112","page_size":2,"cursor":"`+cursor+`"}`)
	if !isInvalidRequest(err) || len(e.fake.api()) != before {
		t.Errorf("foreign cursor: err = %v", err)
	}
}

func TestExperimentsListAsyncErrorsChecksItsArgumentsBeforeIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	for name, call := range map[string]string{
		"no experiment":   head + `"page_size":5}`,
		"zero id":         head + `"experiment_id":"0"}`,
		"name as id":      head + `"experiment_id":"customers/1/experiments/2"}`,
		"page size zero":  head + `"experiment_id":"1","page_size":0}`,
		"page size large": head + `"experiment_id":"1","page_size":1001}`,
		"forged cursor":   head + `"experiment_id":"1","cursor":"forged"}`,
		"page token":      head + `"experiment_id":"1","page_token":"x"}`,
		"resource name":   head + `"experiment_id":"1","resource_name":"customers/1/experiments/1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invoke(experimentsListAsyncErrorsID, call); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestExperimentsListAsyncErrorsMapsErrorsAndBoundsTheAnswer(t *testing.T) {
	call := `{"customer_id":"` + clientID + `","experiment_id":"111"}`
	t.Run("permission", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["GET "+asyncErrorsPath(clientID, "111")] = reply{status: 403, body: adsFailure(403,
			"PERMISSION_DENIED", "authorizationError", "USER_PERMISSION_DENIED", "")}
		_, err := e.invoke(experimentsListAsyncErrorsID, call)
		if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), detailCanary) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("long message is cut", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["GET "+asyncErrorsPath(clientID, "111")] = okReply(`{"errors":[{"code":2,"message":"` +
			strings.Repeat("m", 5000) + `"}]}`)
		out, err := e.invoke(experimentsListAsyncErrorsID, call)
		if err != nil {
			t.Fatal(err)
		}
		if message := decode(t, out)["errors"].([]any)[0].(map[string]any)["message"].(string); len(message) > maxText {
			t.Errorf("message of %d bytes", len(message))
		}
	})
	t.Run("oversized page token is refused", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["GET "+asyncErrorsPath(clientID, "111")] = okReply(`{"errors":[],"nextPageToken":"` +
			strings.Repeat("t", maxAsyncPageToken+1) + `"}`)
		_, err := e.invoke(experimentsListAsyncErrorsID, call)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no errors", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["GET "+asyncErrorsPath(clientID, "111")] = okReply(`{}`)
		out, err := e.invoke(experimentsListAsyncErrorsID, call)
		if err != nil || decode(t, out)["errors"] != nil {
			t.Fatalf("out = %s, err = %v", out, err)
		}
	})
}
