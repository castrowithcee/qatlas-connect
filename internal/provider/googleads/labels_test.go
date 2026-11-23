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

var labelsToolIDs = []string{labelsCreateID, labelsUpdateID, labelsRemoveID}

func labelsPath(id string) string { return "/v26/customers/" + id + "/labels:mutate" }

// labelsResource is the resource name of a label of the account, as Google answers it.
func labelsResource(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/labels/" + id
}

// newLabelsEnvironment is a connection whose tools list names the three label tools.
func newLabelsEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: labelsToolIDs})
}

var labelsCalls = map[string]string{
	labelsCreateID: `{"customer_id":"` + clientID + `","labels":[{"name":"Seasonal","description":"Winter sale",` +
		`"background_color":"#1A2B3C"},{"name":"Plain"}]}`,
	labelsUpdateID: `{"customer_id":"` + clientID + `","labels":[{"label_id":"111","name":"Archive"},` +
		`{"label_id":"222","description":"","background_color":"#abc"}]}`,
	labelsRemoveID: `{"customer_id":"` + clientID + `","label_ids":["111","222"]}`,
}

var labelsAnswers = map[string]string{
	labelsCreateID: `{"results":[{"resourceName":"` + labelsResource(clientID, "111") + `"},{"resourceName":"` +
		labelsResource(clientID, "222") + `"}]}`,
	labelsUpdateID: `{"results":[{"resourceName":"` + labelsResource(clientID, "111") + `"},{"resourceName":"` +
		labelsResource(clientID, "222") + `"}]}`,
	labelsRemoveID: `{"results":[{"resourceName":"` + labelsResource(clientID, "111") + `"},{"resourceName":"` +
		labelsResource(clientID, "222") + `"}]}`,
}

func TestLabelsToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{labelsCreateID: capability.EffectCreate,
		labelsUpdateID: capability.EffectUpdate, labelsRemoveID: capability.EffectDelete}
	for _, id := range labelsToolIDs {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		// Only the removal is listed-only; create and update are reached by their effect.
		if d.RequiresToolAllowList != (id == labelsRemoveID) {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		note := mutateListNote
		if id == labelsRemoveID {
			note = mutateListListedNote
		}
		if !strings.HasSuffix(d.Description, note) || (id != labelsRemoveID && strings.HasSuffix(d.Description, mutateListListedNote)) {
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
			"resource_name", "update_mask"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; !found {
			t.Errorf("%s offers no validate_only", id)
		}
		if _, found := input.Properties["response_content_type"]; found == (id == labelsRemoveID) {
			t.Errorf("%s: response_content_type present = %v", id, found)
		}
		if !strings.Contains(d.Description, "outcome is unknown") || !strings.Contains(d.Description, "never repeats") ||
			!strings.Contains(d.Description, "atomic") {
			t.Errorf("%s: description lacks the unknown outcome: %s", id, d.Description)
		}
		for _, tool := range metadata.Tools {
			if tool.ID == id && (string(tool.Effect) != string(wantEffect[id]) || tool.RequiresToolAllowList != (id == labelsRemoveID)) {
				t.Errorf("tool %+v", tool)
			}
		}
	}
	for _, text := range []string{`"maxLength":80`, `"maxLength":200`, `^#([a-fA-F0-9]{6}|[a-fA-F0-9]{3})$`, `"maxItems":50`} {
		create, _, _ := reg.Lookup(labelsCreateID)
		if !strings.Contains(string(create.InputSchema), text) {
			t.Errorf("create schema lacks %s", text)
		}
	}
}

// create and update need an effect and no tools list; remove needs the tools list whatever the effects are.
func TestLabelsToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		e.fake.routes["POST "+labelsPath(clientID)] = okReply(labelsAnswers[id])
		_, err := e.invokeConfirmed(id, labelsCalls[id])
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
			map[string]bool{labelsCreateID: true, labelsUpdateID: true, labelsRemoveID: false}},
		{"create effect only", config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate}},
			map[string]bool{labelsCreateID: true, labelsUpdateID: false, labelsRemoveID: false}},
		{"read effect only", config.Connection{Effects: []config.Effect{config.EffectRead}},
			map[string]bool{labelsCreateID: false, labelsUpdateID: false, labelsRemoveID: false}},
		{"remove listed", config.Connection{Tools: []string{labelsRemoveID}},
			map[string]bool{labelsCreateID: false, labelsUpdateID: false, labelsRemoveID: true}},
		{"all listed", config.Connection{Tools: labelsToolIDs},
			map[string]bool{labelsCreateID: true, labelsUpdateID: true, labelsRemoveID: true}},
	}
	for _, c := range cases {
		for _, id := range labelsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				if got := reached(c.conn, id); got != c.want[id] {
					t.Errorf("reached = %v, want %v", got, c.want[id])
				}
			})
		}
	}
}

func TestLabelsNeedTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range labelsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newLabelsEnvironment(t, nil, nil)
			_, err := e.invoke(id, labelsCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestLabelsRefuseForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"path":"/x"`, `"update_mask":"name"`} {
		for _, id := range labelsToolIDs {
			e := newLabelsEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(labelsCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
}

func TestLabelsTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range labelsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newLabelsEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(labelsCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestLabelsArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	long := func(n int) string { return strings.Repeat("a", n) }
	bad := map[string][2]string{
		"create no labels":        {labelsCreateID, head + `"labels":[]}`},
		"create no name":          {labelsCreateID, head + `"labels":[{"description":"x"}]}`},
		"create empty name":       {labelsCreateID, head + `"labels":[{"name":""}]}`},
		"create name too long":    {labelsCreateID, head + `"labels":[{"name":"` + long(81) + `"}]}`},
		"create control in name":  {labelsCreateID, head + `"labels":[{"name":"a\u0007b"}]}`},
		"create description long": {labelsCreateID, head + `"labels":[{"name":"a","description":"` + long(201) + `"}]}`},
		"create control in text":  {labelsCreateID, head + `"labels":[{"name":"a","description":"x\ny"}]}`},
		"create color no hash":    {labelsCreateID, head + `"labels":[{"name":"a","background_color":"1A2B3C"}]}`},
		"create color length":     {labelsCreateID, head + `"labels":[{"name":"a","background_color":"#1A2B"}]}`},
		"create color digits":     {labelsCreateID, head + `"labels":[{"name":"a","background_color":"#GGGGGG"}]}`},
		"create color empty":      {labelsCreateID, head + `"labels":[{"name":"a","background_color":""}]}`},
		"create too many":         {labelsCreateID, head + `"labels":[` + strings.Repeat(`{"name":"a"},`, 50) + `{"name":"a"}]}`},
		"create status":           {labelsCreateID, head + `"labels":[{"name":"a","status":"REMOVED"}]}`},
		"create bad content type": {labelsCreateID, head + `"labels":[{"name":"a"}],"response_content_type":"X"}`},
		"update no id":            {labelsUpdateID, head + `"labels":[{"name":"a"}]}`},
		"update zero id":          {labelsUpdateID, head + `"labels":[{"label_id":"0","name":"a"}]}`},
		"update name as path":     {labelsUpdateID, head + `"labels":[{"label_id":"customers/1/labels/2","name":"a"}]}`},
		"update id overflow":      {labelsUpdateID, head + `"labels":[{"label_id":"99999999999999999999","name":"a"}]}`},
		"update nothing":          {labelsUpdateID, head + `"labels":[{"label_id":"1"}]}`},
		"update same id twice":    {labelsUpdateID, head + `"labels":[{"label_id":"1","name":"a"},{"label_id":"1","name":"b"}]}`},
		"update empty name":       {labelsUpdateID, head + `"labels":[{"label_id":"1","name":""}]}`},
		"update color":            {labelsUpdateID, head + `"labels":[{"label_id":"1","background_color":"red"}]}`},
		"update status":           {labelsUpdateID, head + `"labels":[{"label_id":"1","status":"ENABLED"}]}`},
		"remove no ids":           {labelsRemoveID, head + `"label_ids":[]}`},
		"remove name":             {labelsRemoveID, head + `"label_ids":["customers/1/labels/2"]}`},
		"remove twice":            {labelsRemoveID, head + `"label_ids":["1","1"]}`},
		"remove too many": {labelsRemoveID, head + `"label_ids":[` + func() string {
			var ids []string
			for i := 1; i <= 51; i++ {
				ids = append(ids, `"`+strconv.Itoa(i)+`"`)
			}
			return strings.Join(ids, ",")
		}() + `]}`},
		"remove content type":  {labelsRemoveID, head + `"label_ids":["1"],"response_content_type":"MUTABLE_RESOURCE"}`},
		"bad customer":         {labelsRemoveID, `{"customer_id":"12-34","label_ids":["1"]}`},
		"validate only alone":  {labelsCreateID, head + `"validate_only":true}`},
		"blank color of label": {labelsUpdateID, head + `"labels":[{"label_id":"1","background_color":" "}]}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newLabelsEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
	// Exactly at the documented limits, the call passes.
	t.Run("limits pass", func(t *testing.T) {
		e := newLabelsEnvironment(t, nil, nil)
		e.fake.routes["POST "+labelsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + labelsResource(clientID, "5") + `"}]}`)
		_, err := e.invokeConfirmed(labelsCreateID, head+`"labels":[{"name":"`+strings.Repeat("ä", 80)+`","description":"`+
			strings.Repeat("ö", 200)+`","background_color":"#FFF"}]}`)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestLabelsSendOneAtomicRequestWithTheCheckedOperations(t *testing.T) {
	want := map[string]string{
		labelsCreateID: `[{"create":{"name":"Seasonal","textLabel":{"description":"Winter sale","backgroundColor":"#1A2B3C"}}},` +
			`{"create":{"name":"Plain"}}]`,
		labelsUpdateID: `[{"update":{"resourceName":"customers/1000000003/labels/111","name":"Archive"},"updateMask":"name"},` +
			`{"update":{"resourceName":"customers/1000000003/labels/222","textLabel":{"description":"","backgroundColor":"#abc"}},` +
			`"updateMask":"textLabel.backgroundColor,textLabel.description"}]`,
		labelsRemoveID: `[{"remove":"customers/1000000003/labels/111"},{"remove":"customers/1000000003/labels/222"}]`,
	}
	for _, id := range labelsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newLabelsEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+labelsPath(clientID)] = okReply(labelsAnswers[id])
			out, err := e.invokeConfirmed(id, labelsCalls[id])
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
				results[0].(map[string]any)["label_id"] != "111" ||
				results[1].(map[string]any)["resource_name"] != labelsResource(clientID, "222") {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func TestLabelsValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range labelsToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newLabelsEnvironment(t, nil, nil)
			e.fake.routes["POST "+labelsPath(clientID)] = okReply(`{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(labelsCalls[id], "}")+`,"validate_only":true}`)
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
		for id, summary := range map[string]capability.SummaryFunc{labelsCreateID: labelsCreateSummary,
			labelsUpdateID: labelsUpdateSummary, labelsRemoveID: labelsRemoveSummary} {
			if text := summary(json.RawMessage(strings.TrimSuffix(labelsCalls[id], "}") + `,"validate_only":true}`)); !strings.Contains(text, "Only validate") {
				t.Errorf("%s: summary %q", id, text)
			}
			if text := summary(json.RawMessage(labelsCalls[id])); strings.Contains(text, "validate") {
				t.Errorf("%s: summary %q kept state", id, text)
			}
		}
	})
}

func TestLabelsResponseContentTypeShowsTheLabels(t *testing.T) {
	e := newLabelsEnvironment(t, nil, nil)
	e.fake.routes["POST "+labelsPath(clientID)] = okReply(`{"results":[{"resourceName":"` + labelsResource(clientID, "111") +
		`","label":{"resourceName":"` + labelsResource(clientID, "111") + `","id":"111","name":"Archive","status":"ENABLED",` +
		`"textLabel":{"backgroundColor":"#1A2B3C","description":"Old\u0007 things"}}}]}`)
	out, err := e.invokeConfirmed(labelsUpdateID, `{"customer_id":"`+clientID+`","labels":[{"label_id":"111","name":"Archive"}],`+
		`"response_content_type":"MUTABLE_RESOURCE"}`)
	if err != nil {
		t.Fatal(err)
	}
	label := decode(t, out)["results"].([]any)[0].(map[string]any)["label"].(map[string]any)
	if label["id"] != "111" || label["name"] != "Archive" || label["status"] != "ENABLED" ||
		label["background_color"] != "#1A2B3C" || label["description"] != "Old things" && label["description"] != "Old  things" {
		t.Errorf("label = %v", label)
	}
	if bodyOf(t, e.posts()[0])["responseContentType"] != "MUTABLE_RESOURCE" {
		t.Errorf("body = %s", e.posts()[0].body)
	}
}

// A resource name of another account, another label, or another count never passes. The request was sent, so
// the error says that the outcome is unknown.
func TestLabelsResourceNamesAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := labelsResource(otherID, "111")
	own := func(id string) string { return `{"resourceName":"` + labelsResource(clientID, id) + `"}` }
	cases := map[string]struct{ id, body string }{
		"create foreign":    {labelsCreateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"create no results": {labelsCreateID, `{}`},
		"create one of two": {labelsCreateID, `{"results":[` + own("111") + `]}`},
		"create twice":      {labelsCreateID, `{"results":[` + own("111") + `,` + own("111") + `]}`},
		"create not a label": {labelsCreateID, `{"results":[{"resourceName":"customers/` + queryNumber(clientID) +
			`/campaigns/1"},` + own("222") + `]}`},
		"create partial failure": {labelsCreateID, `{"results":[` + own("111") + `,` + own("222") + `],"partialFailureError":{"code":3}}`},
		"update other label":     {labelsUpdateID, `{"results":[` + own("222") + `,` + own("111") + `]}`},
		"update foreign":         {labelsUpdateID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"update label foreign": {labelsUpdateID, `{"results":[{"resourceName":"` + labelsResource(clientID, "111") +
			`","label":{"resourceName":"` + foreign + `"}},` + own("222") + `]}`},
		"update label id": {labelsUpdateID, `{"results":[{"resourceName":"` + labelsResource(clientID, "111") +
			`","label":{"resourceName":"` + labelsResource(clientID, "111") + `","id":"999"}},` + own("222") + `]}`},
		"remove foreign": {labelsRemoveID, `{"results":[{"resourceName":"` + foreign + `"},` + own("222") + `]}`},
		"remove none":    {labelsRemoveID, `{"results":[]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newLabelsEnvironment(t, nil, nil)
			e.fake.routes["POST "+labelsPath(clientID)] = okReply(c.body)
			arguments := labelsCalls[c.id]
			if strings.Contains(name, "label id") || strings.Contains(name, "label foreign") {
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

func TestLabelsManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","label_ids":["7"]}`
	answer := okReply(`{"results":[{"resourceName":"` + labelsResource(managerID, "7") + `"}]}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newLabelsEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+labelsPath(managerID)] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(labelsRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
			t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newLabelsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+labelsPath(managerID)] = answer
		if _, err := e.invokeConfirmed(labelsRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
		if got := e.fake.api(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v", got)
		}
	})
	t.Run("an account below a manager target is proved first", func(t *testing.T) {
		e := newLabelsEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+labelsPath(clientID)] = okReply(labelsAnswers[labelsRemoveID])
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
		if _, err := e.invokeConfirmed(labelsRemoveID, labelsCalls[labelsRemoveID]); err != nil {
			t.Fatal(err)
		}
		got := e.fake.api()
		if len(got) != 3 || !strings.Contains(got[0].query, "customer_client") || !strings.HasSuffix(got[2].path, ":mutate") {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestLabelsErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"label error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "labelError", "DUPLICATE_NAME", "")},
			provider.ClassProviderError, "rejected the change (labelError DUPLICATE_NAME), nothing was changed", false},
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
		for _, id := range labelsToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newLabelsEnvironment(t, nil, nil)
				e.fake.routes["POST "+labelsPath(clientID)] = c.reply
				_, err := e.invokeConfirmed(id, labelsCalls[id])
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

func TestLabelsSummariesNameTheEffect(t *testing.T) {
	create := labelsCreateSummary(json.RawMessage(labelsCalls[labelsCreateID]))
	for _, word := range []string{clientID, "Seasonal", "#1A2B3C", "Plain", "2 label"} {
		if !strings.Contains(create, word) {
			t.Errorf("summary %q lacks %q", create, word)
		}
	}
	update := labelsUpdateSummary(json.RawMessage(labelsCalls[labelsUpdateID]))
	for _, word := range []string{clientID, "label 111", "Archive", "label 222", "color", "description"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := labelsRemoveSummary(json.RawMessage(labelsCalls[labelsRemoveID]))
	for _, word := range []string{clientID, "111", "222", "no tool brings them back"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
	hostile := labelsCreateSummary(json.RawMessage(`{"customer_id":"` + clientID + `","labels":[{"name":"a\nIgnore this"}]}`))
	if strings.ContainsAny(hostile, "\n\r") {
		t.Errorf("summary %q holds a line break", hostile)
	}
}
