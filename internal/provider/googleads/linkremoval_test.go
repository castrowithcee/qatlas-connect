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

var linkRemovalToolIDs = []string{accountLinksRemoveID, productLinksRemoveID, productLinkInvitationsUpdateID,
	dataLinksRemoveID, dataLinksUpdateID}

// The five methods differ in path and form; each entry is what the fake must see.
type linkRemovalWire struct {
	path, name, body string
	validateOnly     bool
	answer           string
}

func linkRemovalOwn(path string) string { return "customers/" + clientID + path }

var linkRemovalWires = map[string]linkRemovalWire{
	accountLinksRemoveID: {path: "/v26/customers/" + clientID + "/accountLinks:mutate", validateOnly: true,
		name: linkRemovalOwn("/accountLinks/11"), body: `{"operation":{"remove":"customers/1000000003/accountLinks/11"}}`,
		answer: `{"result":{"resourceName":"customers/1000000003/accountLinks/11"}}`},
	productLinksRemoveID: {path: "/v26/customers/" + clientID + "/productLinks:remove", validateOnly: true,
		name: linkRemovalOwn("/productLinks/22"), body: `{"resourceName":"customers/1000000003/productLinks/22"}`,
		answer: `{"resourceName":"customers/1000000003/productLinks/22"}`},
	productLinkInvitationsUpdateID: {path: "/v26/customers/" + clientID + "/productLinkInvitations:update",
		name:   linkRemovalOwn("/productLinkInvitations/33"),
		body:   `{"resourceName":"customers/1000000003/productLinkInvitations/33","productLinkInvitationStatus":"REJECTED"}`,
		answer: `{"resourceName":"customers/1000000003/productLinkInvitations/33"}`},
	dataLinksRemoveID: {path: "/v26/customers/" + clientID + "/dataLinks:remove",
		name: linkRemovalOwn("/dataLinks/22~44"), body: `{"resourceName":"customers/1000000003/dataLinks/22~44"}`,
		answer: `{"resourceName":"customers/1000000003/dataLinks/22~44"}`},
	dataLinksUpdateID: {path: "/v26/customers/" + clientID + "/dataLinks:update",
		name:   linkRemovalOwn("/dataLinks/22~44"),
		body:   `{"resourceName":"customers/1000000003/dataLinks/22~44","dataLinkStatus":"DISABLED"}`,
		answer: `{"resourceName":"customers/1000000003/dataLinks/22~44"}`},
}

var linkRemovalCalls = map[string]string{
	accountLinksRemoveID:           `{"customer_id":"` + clientID + `","account_link_id":"11"}`,
	productLinksRemoveID:           `{"customer_id":"` + clientID + `","product_link_id":"22"}`,
	productLinkInvitationsUpdateID: `{"customer_id":"` + clientID + `","product_link_invitation_id":"33","status":"REJECTED"}`,
	dataLinksRemoveID:              `{"customer_id":"` + clientID + `","product_link_id":"22","data_link_id":"44"}`,
	dataLinksUpdateID:              `{"customer_id":"` + clientID + `","product_link_id":"22","data_link_id":"44","status":"DISABLED"}`,
}

func newLinkRemovalEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: linkRemovalToolIDs})
}

func linkRemovalRoute(e *environment, id, answer string) {
	e.fake.routes["POST "+linkRemovalWires[id].path] = okReply(answer)
}

func TestLinkRemovalToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	wantEffect := map[string]capability.Effect{accountLinksRemoveID: capability.EffectDelete,
		productLinksRemoveID: capability.EffectDelete, dataLinksRemoveID: capability.EffectDelete,
		productLinkInvitationsUpdateID: capability.EffectUpdate, dataLinksUpdateID: capability.EffectUpdate}
	for _, id := range linkRemovalToolIDs {
		d, _, ok := reg.Lookup(id)
		remove := wantEffect[id] == capability.EffectDelete
		if !ok || d.Version != 1 || d.Risk.Effect != wantEffect[id] || len(d.OutputSchema) == 0 ||
			d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld {
			t.Fatalf("%s = %+v", id, d)
		}
		if d.RequiresToolAllowList != remove {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		note := changeNote
		if remove {
			note = mutateNote
		}
		if !strings.HasSuffix(d.Description, note) || (!remove && strings.HasSuffix(d.Description, mutateNote)) {
			t.Errorf("%s: description does not end on the note of how it is offered", id)
		}
		if slices.Contains(metadata.Profiles[0].Tools, id) {
			t.Errorf("the read profile holds %s", id)
		}
		if got := metadata.Token.Scopes[id]; !reflect.DeepEqual(got, []string{scopeAdwords}) {
			t.Errorf("scopes of %s = %v", id, got)
		}
		var input struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
			Additional bool `json:"additionalProperties"`
		}
		if json.Unmarshal(d.InputSchema, &input) != nil || input.Additional {
			t.Errorf("%s: input schema is open", id)
		}
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "operation", "path", "url",
			"resource_name", "update_mask", "response_content_type"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; found != linkRemovalWires[id].validateOnly {
			t.Errorf("%s: validate_only present = %v", id, found)
		}
		for _, word := range []string{"outcome is unknown", "never repeats", "atomic"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
		if !linkRemovalWires[id].validateOnly && !strings.Contains(d.Description, "no validate_only") {
			t.Errorf("%s: description does not say that the method has no validate_only", id)
		}
		// No tool makes or accepts anything.
		if strings.Contains(string(d.InputSchema), "ACCEPTED") || strings.Contains(string(d.InputSchema), "ENABLED") {
			t.Errorf("%s: schema offers an accepting status", id)
		}
	}
	enum := func(id string) []string {
		d, _, _ := reg.Lookup(id)
		var input struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		_ = json.Unmarshal(d.InputSchema, &input)
		return input.Properties["status"].Enum
	}
	if got := enum(productLinkInvitationsUpdateID); !reflect.DeepEqual(got, []string{"REJECTED", "REVOKED"}) {
		t.Errorf("invitation statuses = %v", got)
	}
	if got := enum(dataLinksUpdateID); !reflect.DeepEqual(got, []string{"DISABLED", "REVOKED", "REJECTED"}) {
		t.Errorf("data link statuses = %v", got)
	}
	for _, id := range []string{accountLinksRemoveID, productLinksRemoveID, dataLinksRemoveID} {
		if enum(id) != nil {
			t.Errorf("%s offers a status", id)
		}
	}
	for _, tool := range metadata.Tools {
		if (strings.Contains(tool.ID, "-links.") || strings.Contains(tool.ID, "-link-")) &&
			(strings.HasSuffix(tool.ID, ".create") || strings.Contains(tool.ID, "accept")) {
			t.Errorf("tool %s must not exist", tool.ID)
		}
	}
}

// update tools are reached by the update effect, remove tools only by the tools list.
func TestLinkRemovalToolsAreReachedByEffectOrByTheToolsList(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete,
		config.EffectExecute}
	reached := func(conn config.Connection, id string) bool {
		e := newEnvironmentWith(t, nil, nil, conn)
		linkRemovalRoute(e, id, linkRemovalWires[id].answer)
		_, err := e.invokeConfirmed(id, linkRemovalCalls[id])
		if err != nil {
			e.untouched(t)
		}
		return err == nil
	}
	for _, id := range linkRemovalToolIDs {
		update := id == productLinkInvitationsUpdateID || id == dataLinksUpdateID
		t.Run(id, func(t *testing.T) {
			if got := reached(config.Connection{Effects: all}, id); got != update {
				t.Errorf("all effects: reached = %v, want %v", got, update)
			}
			if reached(config.Connection{Effects: []config.Effect{config.EffectRead}}, id) {
				t.Error("read effect reaches it")
			}
			if !reached(config.Connection{Tools: []string{id}}, id) {
				t.Error("tools list does not reach it")
			}
		})
	}
}

func TestLinkRemovalNeedsTheConfirmationOfTheCall(t *testing.T) {
	for _, id := range linkRemovalToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newLinkRemovalEnvironment(t, nil, nil)
			_, err := e.invoke(id, linkRemovalCalls[id])
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
	}
}

func TestLinkRemovalRefusesForeignArguments(t *testing.T) {
	for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"partial_failure":true`, `"operations":[]`,
		`"resource_name":"customers/1/accountLinks/2"`, `"path":"/x"`} {
		for _, id := range linkRemovalToolIDs {
			e := newLinkRemovalEnvironment(t, nil, nil)
			arguments := strings.TrimSuffix(linkRemovalCalls[id], "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
	for _, id := range []string{productLinkInvitationsUpdateID, dataLinksRemoveID, dataLinksUpdateID} {
		e := newLinkRemovalEnvironment(t, nil, nil)
		if _, err := e.invokeConfirmed(id, strings.TrimSuffix(linkRemovalCalls[id], "}")+`,"validate_only":true}`); err == nil {
			t.Errorf("%s accepted validate_only although the method has none", id)
		}
		e.untouched(t)
	}
}

func TestLinkRemovalTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range linkRemovalToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newLinkRemovalEnvironment(t, []string{"customers/" + clientID}, nil)
			arguments := strings.Replace(linkRemovalCalls[id], clientID, otherID, 1)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
	}
}

func TestLinkRemovalArgumentsAreCheckedBeforeSecretAndIO(t *testing.T) {
	head := `{"customer_id":"` + clientID + `",`
	bad := map[string][2]string{
		"bad customer":            {accountLinksRemoveID, `{"customer_id":"12-34","account_link_id":"1"}`},
		"account no id":           {accountLinksRemoveID, head + `"validate_only":true}`},
		"account zero id":         {accountLinksRemoveID, head + `"account_link_id":"0"}`},
		"account name as id":      {accountLinksRemoveID, head + `"account_link_id":"customers/1/accountLinks/2"}`},
		"account overflow":        {accountLinksRemoveID, head + `"account_link_id":"99999999999999999999"}`},
		"account leading zero":    {accountLinksRemoveID, head + `"account_link_id":"011"}`},
		"account status":          {accountLinksRemoveID, head + `"account_link_id":"1","status":"REVOKED"}`},
		"product no id":           {productLinksRemoveID, head + `"validate_only":false}`},
		"product other id":        {productLinksRemoveID, head + `"account_link_id":"1"}`},
		"product path id":         {productLinksRemoveID, head + `"product_link_id":"1/../2"}`},
		"invitation no status":    {productLinkInvitationsUpdateID, head + `"product_link_invitation_id":"3"}`},
		"invitation accepted":     {productLinkInvitationsUpdateID, head + `"product_link_invitation_id":"3","status":"ACCEPTED"}`},
		"invitation requested":    {productLinkInvitationsUpdateID, head + `"product_link_invitation_id":"3","status":"REQUESTED"}`},
		"invitation expired":      {productLinkInvitationsUpdateID, head + `"product_link_invitation_id":"3","status":"EXPIRED"}`},
		"invitation lower case":   {productLinkInvitationsUpdateID, head + `"product_link_invitation_id":"3","status":"rejected"}`},
		"invitation bad id":       {productLinkInvitationsUpdateID, head + `"product_link_invitation_id":"x","status":"REVOKED"}`},
		"data remove no data id":  {dataLinksRemoveID, head + `"product_link_id":"2"}`},
		"data remove no product":  {dataLinksRemoveID, head + `"data_link_id":"2"}`},
		"data remove joined":      {dataLinksRemoveID, head + `"product_link_id":"2~3","data_link_id":"4"}`},
		"data remove status":      {dataLinksRemoveID, head + `"product_link_id":"2","data_link_id":"4","status":"DISABLED"}`},
		"data update no status":   {dataLinksUpdateID, head + `"product_link_id":"2","data_link_id":"4"}`},
		"data update enabled":     {dataLinksUpdateID, head + `"product_link_id":"2","data_link_id":"4","status":"ENABLED"}`},
		"data update requested":   {dataLinksUpdateID, head + `"product_link_id":"2","data_link_id":"4","status":"REQUESTED"}`},
		"data update pending":     {dataLinksUpdateID, head + `"product_link_id":"2","data_link_id":"4","status":"PENDING_APPROVAL"}`},
		"data update unspecified": {dataLinksUpdateID, head + `"product_link_id":"2","data_link_id":"4","status":"UNSPECIFIED"}`},
		"data update accepted":    {dataLinksUpdateID, head + `"product_link_id":"2","data_link_id":"4","status":"ACCEPTED"}`},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newLinkRemovalEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(c[0], c[1]); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

// Only the backward statuses pass, and each reaches the wire as the matching enum value.
func TestLinkRemovalUpdatesSendOnlyBackwardStatuses(t *testing.T) {
	cases := []struct {
		id, status, body string
	}{
		{productLinkInvitationsUpdateID, "REJECTED", `{"resourceName":"customers/1000000003/productLinkInvitations/33","productLinkInvitationStatus":"REJECTED"}`},
		{productLinkInvitationsUpdateID, "REVOKED", `{"resourceName":"customers/1000000003/productLinkInvitations/33","productLinkInvitationStatus":"REVOKED"}`},
		{dataLinksUpdateID, "DISABLED", `{"resourceName":"customers/1000000003/dataLinks/22~44","dataLinkStatus":"DISABLED"}`},
		{dataLinksUpdateID, "REVOKED", `{"resourceName":"customers/1000000003/dataLinks/22~44","dataLinkStatus":"REVOKED"}`},
		{dataLinksUpdateID, "REJECTED", `{"resourceName":"customers/1000000003/dataLinks/22~44","dataLinkStatus":"REJECTED"}`},
	}
	for _, c := range cases {
		t.Run(c.id+" "+c.status, func(t *testing.T) {
			e := newLinkRemovalEnvironment(t, nil, nil)
			linkRemovalRoute(e, c.id, linkRemovalWires[c.id].answer)
			arguments := strings.Replace(linkRemovalCalls[c.id], linkRemovalCalls[c.id][strings.Index(linkRemovalCalls[c.id], `"status":"`):],
				`"status":"`+c.status+`"}`, 1)
			out, err := e.invokeConfirmed(c.id, arguments)
			if err != nil {
				t.Fatal(err)
			}
			posts := e.postsOf(linkRemovalWires[c.id].path)
			if len(posts) != 1 || !reflect.DeepEqual(bodyOf(t, posts[0]), wantWire(t, c.body)) {
				t.Fatalf("posts = %+v", posts)
			}
			if got := decode(t, out); got["status"] != c.status || got["resource_name"] != linkRemovalWires[c.id].name ||
				got["customer_id"] != clientID {
				t.Errorf("out = %s", out)
			}
		})
	}
}

func (e *environment) postsOf(path string) []seen {
	var out []seen
	for _, s := range e.fake.api() {
		if s.method == "POST" && s.path == path {
			out = append(out, s)
		}
	}
	return out
}

func TestLinkRemovalSendsOneAtomicRequestWithTheCheckedName(t *testing.T) {
	for _, id := range linkRemovalToolIDs {
		t.Run(id, func(t *testing.T) {
			e := newLinkRemovalEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			linkRemovalRoute(e, id, linkRemovalWires[id].answer)
			out, err := e.invokeConfirmed(id, linkRemovalCalls[id])
			if err != nil {
				t.Fatal(err)
			}
			w := linkRemovalWires[id]
			posts := e.postsOf(w.path)
			if len(posts) != 1 || len(e.fake.api()) != 1 || posts[0].login != loginID || posts[0].developerToken != "" {
				t.Fatalf("posts = %+v", e.fake.api())
			}
			if !reflect.DeepEqual(bodyOf(t, posts[0]), wantWire(t, w.body)) {
				t.Errorf("body = %s", posts[0].body)
			}
			got := decode(t, out)
			if got["customer_id"] != clientID || got["resource_name"] != w.name {
				t.Errorf("out = %s", out)
			}
			if _, has := got["validate_only"]; has != w.validateOnly {
				t.Errorf("validate_only in the answer = %v", has)
			}
		})
	}
}

func TestLinkRemovalValidateOnlyValidatesWithoutAResult(t *testing.T) {
	for _, id := range []string{accountLinksRemoveID, productLinksRemoveID} {
		t.Run(id, func(t *testing.T) {
			e := newLinkRemovalEnvironment(t, nil, nil)
			linkRemovalRoute(e, id, `{}`)
			out, err := e.invokeConfirmed(id, strings.TrimSuffix(linkRemovalCalls[id], "}")+`,"validate_only":true}`)
			if err != nil {
				t.Fatal(err)
			}
			body := bodyOf(t, e.postsOf(linkRemovalWires[id].path)[0])
			got := decode(t, out)
			if body["validateOnly"] != true || got["validate_only"] != true || got["resource_name"] != nil {
				t.Errorf("body = %v, out = %s", body, out)
			}
			// false is the default and is not sent.
			e2 := newLinkRemovalEnvironment(t, nil, nil)
			linkRemovalRoute(e2, id, linkRemovalWires[id].answer)
			if _, err := e2.invokeConfirmed(id, strings.TrimSuffix(linkRemovalCalls[id], "}")+`,"validate_only":false}`); err != nil {
				t.Fatal(err)
			}
			if _, has := bodyOf(t, e2.postsOf(linkRemovalWires[id].path)[0])["validateOnly"]; has {
				t.Error("validateOnly sent for false")
			}
		})
	}
}

// A resource name of another account or object never passes. The request was sent, so the error says that the
// outcome is unknown.
func TestLinkRemovalResourceNamesAreBoundToTheCheckedObject(t *testing.T) {
	foreign := func(path string) string { return "customers/" + otherID + path }
	cases := map[string]struct{ id, body string }{
		"account foreign":     {accountLinksRemoveID, `{"result":{"resourceName":"` + foreign("/accountLinks/11") + `"}}`},
		"account other link":  {accountLinksRemoveID, `{"result":{"resourceName":"` + linkRemovalOwn("/accountLinks/12") + `"}}`},
		"account none":        {accountLinksRemoveID, `{}`},
		"account empty":       {accountLinksRemoveID, `{"result":{}}`},
		"account flat":        {accountLinksRemoveID, `{"resourceName":"` + linkRemovalOwn("/accountLinks/11") + `"}`},
		"account partial":     {accountLinksRemoveID, `{"result":{"resourceName":"` + linkRemovalOwn("/accountLinks/11") + `"},"partialFailureError":{"code":3}}`},
		"product foreign":     {productLinksRemoveID, `{"resourceName":"` + foreign("/productLinks/22") + `"}`},
		"product other kind":  {productLinksRemoveID, `{"resourceName":"` + linkRemovalOwn("/accountLinks/22") + `"}`},
		"product none":        {productLinksRemoveID, `{}`},
		"product nested":      {productLinksRemoveID, `{"result":{"resourceName":"` + linkRemovalOwn("/productLinks/22") + `"}}`},
		"invitation foreign":  {productLinkInvitationsUpdateID, `{"resourceName":"` + foreign("/productLinkInvitations/33") + `"}`},
		"invitation other id": {productLinkInvitationsUpdateID, `{"resourceName":"` + linkRemovalOwn("/productLinkInvitations/34") + `"}`},
		"invitation none":     {productLinkInvitationsUpdateID, `{}`},
		"data foreign":        {dataLinksRemoveID, `{"resourceName":"` + foreign("/dataLinks/22~44") + `"}`},
		"data other product":  {dataLinksRemoveID, `{"resourceName":"` + linkRemovalOwn("/dataLinks/23~44") + `"}`},
		"data other link":     {dataLinksUpdateID, `{"resourceName":"` + linkRemovalOwn("/dataLinks/22~45") + `"}`},
		"data none":           {dataLinksUpdateID, `{}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newLinkRemovalEnvironment(t, nil, nil)
			linkRemovalRoute(e, c.id, c.body)
			_, err := e.invokeConfirmed(c.id, linkRemovalCalls[c.id])
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.postsOf(linkRemovalWires[c.id].path)) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.fake.api()))
			}
		})
	}
}

func TestLinkRemovalManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCall := `{"customer_id":"` + managerID + `","product_link_id":"22"}`
	path := "/v26/customers/" + managerID + "/productLinks:remove"
	answer := okReply(`{"resourceName":"customers/` + managerID + `/productLinks/22"}`)
	t.Run("a customer target that names a manager is refused", func(t *testing.T) {
		e := newLinkRemovalEnvironment(t, []string{"customers/" + managerID}, nil)
		e.fake.routes["POST "+path] = answer
		e.fake.answer = func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":true}}`)
				return &r
			}
			return nil
		}
		_, err := e.invokeConfirmed(productLinksRemoveID, managerCall)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.postsOf(path)) != 0 {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a manager target that names it passes", func(t *testing.T) {
		e := newLinkRemovalEnvironment(t, []string{"managers/" + managerID}, nil)
		e.fake.routes["POST "+path] = answer
		if _, err := e.invokeConfirmed(productLinksRemoveID, managerCall); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLinkRemovalErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"link error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "dataLinkError", "INVALID_STATUS", "")},
			provider.ClassProviderError, "rejected the change (dataLinkError INVALID_STATUS), nothing was changed", false},
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"unreadable json", okReply(`{"resourceName":`), provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for _, c := range cases {
		for _, id := range linkRemovalToolIDs {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newLinkRemovalEnvironment(t, nil, nil)
				e.fake.routes["POST "+linkRemovalWires[id].path] = c.reply
				_, err := e.invokeConfirmed(id, linkRemovalCalls[id])
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
				if len(e.fake.api()) != 1 {
					t.Errorf("requests = %d, want one and no repeat", len(e.fake.api()))
				}
			})
		}
	}
}

func TestLinkRemovalSummariesNameTheEffect(t *testing.T) {
	want := map[string][]string{
		accountLinksRemoveID:           {clientID, "account link 11", "Remove"},
		productLinksRemoveID:           {clientID, "product link 22", "Remove"},
		productLinkInvitationsUpdateID: {clientID, "Reject", "invitation 33"},
		dataLinksRemoveID:              {clientID, "22~44", "Remove"},
		dataLinksUpdateID:              {clientID, "22~44", "DISABLED"},
	}
	for _, id := range linkRemovalToolIDs {
		text := linkRemovalSummary(id)(json.RawMessage(linkRemovalCalls[id]))
		for _, word := range want[id] {
			if !strings.Contains(text, word) {
				t.Errorf("%s: summary %q lacks %q", id, text, word)
			}
		}
		if strings.Contains(text, "validate") {
			t.Errorf("%s: summary %q kept state", id, text)
		}
	}
	revoke := linkRemovalSummary(productLinkInvitationsUpdateID)(json.RawMessage(strings.Replace(
		linkRemovalCalls[productLinkInvitationsUpdateID], "REJECTED", "REVOKED", 1)))
	if !strings.HasPrefix(revoke, "Revoke") {
		t.Errorf("summary %q", revoke)
	}
	for _, id := range []string{accountLinksRemoveID, productLinksRemoveID} {
		text := linkRemovalSummary(id)(json.RawMessage(strings.TrimSuffix(linkRemovalCalls[id], "}") + `,"validate_only":true}`))
		if !strings.Contains(text, "Only validate") {
			t.Errorf("%s: summary %q", id, text)
		}
	}
}
