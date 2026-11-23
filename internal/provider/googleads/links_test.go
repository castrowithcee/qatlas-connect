package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const linkNo = "7788990011"

var linkTools = []string{customerManagerLinksListID, customerManagerLinksUpdateID, customerClientLinksListID,
	customerClientLinksUpdateID}

func managerLinksPath(id string) string {
	return "/v26/customers/" + id + "/customerManagerLinks:mutate"
}
func clientLinksPath(id string) string { return "/v26/customers/" + id + "/customerClientLinks:mutate" }

func managerLinkNameOf(customer, manager, id string) string {
	return "customers/" + queryNumber(customer) + "/customerManagerLinks/" + queryNumber(manager) + "~" + id
}
func clientLinkNameOf(customer, client, id string) string {
	return "customers/" + queryNumber(customer) + "/customerClientLinks/" + queryNumber(client) + "~" + id
}

func managerLinkItem(customer, manager, id, status string) string {
	return fmt.Sprintf(`{"customerManagerLink":{"resourceName":"%s","managerLinkId":"%s","managerCustomer":"customers/%s",`+
		`"status":"%s"}}`, managerLinkNameOf(customer, manager, id), id, manager, status)
}

func clientLinkItem(customer, client, id, status string, hidden bool) string {
	return fmt.Sprintf(`{"customerClientLink":{"resourceName":"%s","managerLinkId":"%s","clientCustomer":"customers/%s",`+
		`"status":"%s","hidden":%t}}`, clientLinkNameOf(customer, client, id), id, client, status, hidden)
}

// linkCalls hold a valid call of each tool; the account of the call is the first verb.
var linkCalls = map[string]string{
	customerManagerLinksListID:   `{"customer_id":"%s"}`,
	customerClientLinksListID:    `{"customer_id":"%s"}`,
	customerManagerLinksUpdateID: `{"customer_id":"%s","manager_customer_id":"` + managerID + `","manager_link_id":"` + linkNo + `","status":"REFUSED"}`,
	customerClientLinksUpdateID:  `{"customer_id":"%s","client_customer_id":"` + clientID + `","manager_link_id":"` + linkNo + `","status":"CANCELED"}`,
}

func linkCall(id, customer string) string { return fmt.Sprintf(linkCalls[id], customer) }

func newLinksEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: append(slices.Clone(changeToolIDs),
		customerManagerLinksListID, customerClientLinksListID)})
}

func TestLinkToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, effect := range map[string]capability.Effect{customerManagerLinksListID: capability.EffectRead,
		customerClientLinksListID: capability.EffectRead, customerManagerLinksUpdateID: capability.EffectUpdate,
		customerClientLinksUpdateID: capability.EffectUpdate} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != effect || !d.Risk.OpenWorld || len(d.OutputSchema) == 0 {
			t.Fatalf("%s = %+v", id, d)
		}
		listed := effect != capability.EffectRead
		if d.RequiresToolAllowList != listed || slices.Contains(metadata.Profiles[0].Tools, id) == listed {
			t.Errorf("%s: tools list required = %v", id, d.RequiresToolAllowList)
		}
		if (d.Risk.Confirmation == capability.ConfirmationRequired) != listed {
			t.Errorf("%s: confirmation = %v", id, d.Risk.Confirmation)
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
		for _, forbidden := range []string{"login_customer_id", "partial_failure", "operations", "operation", "create",
			"update_mask", "resource_name", "new_manager", "previous_customer_manager_link", "response_content_type",
			"path", "url"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if _, found := input.Properties["validate_only"]; found == !listed {
			t.Errorf("%s: validate_only present = %v", id, found)
		}
	}
	// No tool creates, accepts or moves a link.
	for _, tool := range metadata.Tools {
		if strings.Contains(tool.ID, "link") && (strings.HasSuffix(tool.ID, ".create") || strings.Contains(tool.ID, ".move") ||
			strings.Contains(tool.ID, "accept")) {
			t.Errorf("tool %s must not exist", tool.ID)
		}
	}
	enum := func(id, property string) []string {
		d, _, _ := reg.Lookup(id)
		var input struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		_ = json.Unmarshal(d.InputSchema, &input)
		return input.Properties[property].Enum
	}
	if got := enum(customerManagerLinksUpdateID, "status"); !reflect.DeepEqual(got, []string{"INACTIVE", "REFUSED"}) {
		t.Errorf("manager link statuses = %v", got)
	}
	if got := enum(customerClientLinksUpdateID, "status"); !reflect.DeepEqual(got, []string{"CANCELED"}) {
		t.Errorf("client link statuses = %v", got)
	}
	for _, id := range []string{customerManagerLinksUpdateID, customerClientLinksUpdateID} {
		d, _, _ := reg.Lookup(id)
		for _, word := range []string{"never repeats", "outcome is unknown", "ACTIVE, PENDING"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
		// The two values that make or accept a link appear in the text only as words that are refused.
		schemaText := string(d.InputSchema)
		for _, word := range []string{`"ACTIVE"`, `"PENDING"`, `"ENABLED"`} {
			if strings.Contains(schemaText, word) {
				t.Errorf("%s: schema holds %s", id, word)
			}
		}
	}
	d, _, _ := reg.Lookup(customerClientLinksUpdateID)
	if !strings.Contains(string(d.InputSchema), `"hidden"`) {
		t.Errorf("client link update lacks hidden: %s", d.InputSchema)
	}
}

func TestLinkTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for _, id := range linkTools {
		t.Run(id, func(t *testing.T) {
			e := newLinksEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invokeConfirmed(id, linkCall(id, otherID))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
		t.Run(id+" malformed customer_id", func(t *testing.T) {
			e := newLinksEnvironment(t, nil, nil)
			for _, bad := range []string{"123-456-7890", "123456789", "", "1234567890 OR 1=1"} {
				if _, err := e.invokeConfirmed(id, linkCall(id, bad)); err == nil {
					t.Errorf("customer_id %q accepted", bad)
				}
			}
			e.untouched(t)
		})
	}
}

func TestLinkUpdatesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	for _, id := range []string{customerManagerLinksUpdateID, customerClientLinksUpdateID} {
		t.Run(id+" without the tools list", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
				config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}})
			if _, err := e.invokeConfirmed(id, linkCall(id, clientID)); err == nil {
				t.Fatal("a connection with all effects but no tools list ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			_, err := e.invoke(id, linkCall(id, clientID))
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) || !strings.Contains(needed.Summary, linkNo) {
				t.Fatalf("err = %v, want a confirmation that names the account and the link", err)
			}
			e.untouched(t)
		})
		t.Run(id+" over the read effect", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, linkCall(id, clientID)); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			e.untouched(t)
		})
	}
}

// Only INACTIVE, REFUSED, CANCELED and hidden are reachable, each on the side it belongs to. A value that makes
// or accepts a link, and a value of the other side, is refused before any secret is read or request is made, by
// the schema and by the handler alike.
func TestLinkUpdatesReachOnlyTheAllowedStatusesBeforeIO(t *testing.T) {
	manager := func(status string) string {
		return `{"customer_id":"` + clientID + `","manager_customer_id":"` + managerID + `","manager_link_id":"` + linkNo +
			`","status":"` + status + `"}`
	}
	client := func(status string) string {
		return `{"customer_id":"` + managerID + `","client_customer_id":"` + clientID + `","manager_link_id":"` + linkNo +
			`","status":"` + status + `"}`
	}
	for _, status := range []string{"ACTIVE", "active", "PENDING", "UNSPECIFIED", "UNKNOWN", "CANCELED", "ENABLED", "REMOVED", "", " INACTIVE"} {
		e := newMutationEnvironment(t, nil, nil)
		if _, err := e.invokeConfirmed(customerManagerLinksUpdateID, manager(status)); err == nil {
			t.Errorf("manager link status %q accepted", status)
		}
		e.untouched(t)
		_, err := handleManagerLinkUpdate(context.Background(), e.resolved(), e.secrets(), e.red, json.RawMessage(manager(status)))
		if !isInvalidRequest(err) {
			t.Errorf("manager link handler status %q: err = %v, want invalid-request", status, err)
		}
		e.untouched(t)
	}
	for _, status := range []string{"ACTIVE", "PENDING", "REFUSED", "INACTIVE", "UNSPECIFIED", "UNKNOWN", "canceled", "ENABLED", "REMOVED"} {
		e := newMutationEnvironment(t, nil, nil)
		if _, err := e.invokeConfirmed(customerClientLinksUpdateID, client(status)); err == nil {
			t.Errorf("client link status %q accepted", status)
		}
		e.untouched(t)
		_, err := handleClientLinkUpdate(context.Background(), e.resolved(), e.secrets(), e.red, json.RawMessage(client(status)))
		if !isInvalidRequest(err) {
			t.Errorf("client link handler status %q: err = %v, want invalid-request", status, err)
		}
		e.untouched(t)
	}
	// An update without status and hidden, and a hidden on a manager link, are no update.
	e := newMutationEnvironment(t, nil, nil)
	for id, arguments := range map[string]string{
		customerClientLinksUpdateID: `{"customer_id":"` + managerID + `","client_customer_id":"` + clientID + `","manager_link_id":"` + linkNo + `"}`,
		customerManagerLinksUpdateID: `{"customer_id":"` + clientID + `","manager_customer_id":"` + managerID +
			`","manager_link_id":"` + linkNo + `","status":"REFUSED","hidden":true}`,
	} {
		if _, err := e.invokeConfirmed(id, arguments); err == nil {
			t.Errorf("%s accepted %s", id, arguments)
		}
		if _, err := handleClientLinkUpdate(context.Background(), e.resolved(), e.secrets(), e.red, json.RawMessage(arguments)); id == customerClientLinksUpdateID && !isInvalidRequest(err) {
			t.Errorf("handler accepted %s: %v", arguments, err)
		}
		if _, err := handleManagerLinkUpdate(context.Background(), e.resolved(), e.secrets(), e.red, json.RawMessage(arguments)); id == customerManagerLinksUpdateID && !isInvalidRequest(err) {
			t.Errorf("handler accepted %s: %v", arguments, err)
		}
	}
	e.untouched(t)
}

// What would create a link, or bypass the checked name, has no argument.
func TestLinksCannotBeCreatedMovedOrNamedByTheCaller(t *testing.T) {
	foreign := []string{`"create":{}`, `"operation":{}`, `"operations":[]`, `"partial_failure":true`,
		`"login_customer_id":"` + loginID + `"`, `"response_content_type":"MUTABLE_RESOURCE"`, `"update_mask":"status"`,
		`"resource_name":"customers/` + clientID + `/customerManagerLinks/` + managerID + `~1"`,
		`"new_manager":"customers/` + otherID + `"`, `"previous_customer_manager_link":"x"`}
	for _, extra := range foreign {
		for _, id := range []string{customerManagerLinksUpdateID, customerClientLinksUpdateID} {
			e := newMutationEnvironment(t, nil, nil)
			arguments := linkCall(id, clientID)
			arguments = strings.TrimSuffix(arguments, "}") + "," + extra + "}"
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
	for _, id := range []string{customerManagerLinksUpdateID, customerClientLinksUpdateID} {
		for _, bad := range []string{`"manager_link_id":"0"`, `"manager_link_id":"01"`, `"manager_link_id":"1/../2"`,
			`"manager_link_id":"9999999999999999999"`, `"manager_link_id":"1~2"`} {
			e := newMutationEnvironment(t, nil, nil)
			arguments := strings.Replace(linkCall(id, clientID), `"manager_link_id":"`+linkNo+`"`, bad, 1)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, bad)
			}
			e.untouched(t)
		}
	}
	for id, bad := range map[string]string{customerManagerLinksUpdateID: `"manager_customer_id":"123"`,
		customerClientLinksUpdateID: `"client_customer_id":"customers/1000000003"`} {
		e := newMutationEnvironment(t, nil, nil)
		arguments := linkCall(id, clientID)
		arguments = regexpReplace(arguments, `"(manager|client)_customer_id":"[^"]*"`, bad)
		if _, err := e.invokeConfirmed(id, arguments); err == nil {
			t.Errorf("%s accepted %s", id, bad)
		}
		e.untouched(t)
	}
	// An account cannot be linked to itself.
	e := newMutationEnvironment(t, nil, nil)
	if _, err := e.invokeConfirmed(customerManagerLinksUpdateID, `{"customer_id":"`+managerID+`","manager_customer_id":"`+
		managerID+`","manager_link_id":"`+linkNo+`","status":"INACTIVE"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	e.untouched(t)
}

func regexpReplace(s, pattern, with string) string {
	return regexp.MustCompile(pattern).ReplaceAllString(s, with)
}

func TestManagerLinkUpdateSendsOneOperationWithTheGivenStatus(t *testing.T) {
	for _, status := range []string{"REFUSED", "INACTIVE"} {
		for _, validate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s validate_only=%t", status, validate), func(t *testing.T) {
				e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
				name := managerLinkNameOf(clientID, managerID, linkNo)
				answer := `{"results":[{"resourceName":"` + name + `"}]}`
				if validate {
					answer = `{"results":[{}]}`
				}
				e.fake.routes["POST "+managerLinksPath(clientID)] = okReply(answer)
				arguments := `{"customer_id":"` + clientID + `","manager_customer_id":"` + managerID + `","manager_link_id":"` +
					linkNo + `","status":"` + status + `","validate_only":` + fmt.Sprint(validate) + `}`
				out, err := e.invokeConfirmed(customerManagerLinksUpdateID, arguments)
				if err != nil {
					t.Fatal(err)
				}
				got := decode(t, out)
				if got["customer_id"] != clientID || got["status"] != status || got["validate_only"] != validate ||
					(got["resource_name"] == name) == validate {
					t.Errorf("out = %s", out)
				}
				posts := e.posts()
				if len(posts) != 1 || len(e.fake.api()) != 1 {
					t.Fatalf("requests = %+v", e.fake.api())
				}
				p := posts[0]
				if p.url != "https://googleads.googleapis.com"+managerLinksPath(clientID) || p.login != loginID ||
					p.auth != "Bearer synthetic-access-1" || p.developerToken != "" {
					t.Errorf("request = %+v", p)
				}
				want := map[string]any{"operations": []any{map[string]any{"update": map[string]any{"resourceName": name,
					"status": status}, "updateMask": "status"}}}
				if validate {
					want["validateOnly"] = true
				}
				if !reflect.DeepEqual(bodyOf(t, p), want) {
					t.Errorf("body = %s", p.body)
				}
			})
		}
	}
}

func TestClientLinkUpdateSendsOneOperationWithStatusAndHidden(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name      string
		arguments string
		update    map[string]any
		mask      string
	}{
		{"cancel", `"status":"CANCELED"`, map[string]any{"status": "CANCELED"}, "status"},
		{"hide", `"hidden":true`, map[string]any{"hidden": yes}, "hidden"},
		{"show", `"hidden":false`, map[string]any{"hidden": no}, "hidden"},
		{"both", `"status":"CANCELED","hidden":true`, map[string]any{"status": "CANCELED", "hidden": yes}, "status,hidden"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			name := clientLinkNameOf(managerID, clientID, linkNo)
			e.fake.routes["POST "+clientLinksPath(managerID)] = okReply(`{"result":{"resourceName":"` + name + `"}}`)
			out, err := e.invokeConfirmed(customerClientLinksUpdateID, `{"customer_id":"`+managerID+`","client_customer_id":"`+
				clientID+`","manager_link_id":"`+linkNo+`",`+c.arguments+`}`)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if got["resource_name"] != name || got["customer_id"] != managerID || got["validate_only"] != false {
				t.Errorf("out = %s", out)
			}
			posts := e.posts()
			if len(posts) != 1 || len(e.fake.api()) != 1 {
				t.Fatalf("requests = %+v", e.fake.api())
			}
			update := map[string]any{"resourceName": name}
			for k, v := range c.update {
				update[k] = v
			}
			want := map[string]any{"operation": map[string]any{"update": update, "updateMask": c.mask}}
			if !reflect.DeepEqual(bodyOf(t, posts[0]), want) {
				t.Errorf("body = %s", posts[0].body)
			}
		})
	}
	t.Run("validate_only", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+clientLinksPath(managerID)] = okReply(`{}`)
		out, err := e.invokeConfirmed(customerClientLinksUpdateID, `{"customer_id":"`+managerID+`","client_customer_id":"`+
			clientID+`","manager_link_id":"`+linkNo+`","hidden":true,"validate_only":true}`)
		got := decode(t, out)
		if err != nil || got["validate_only"] != true || got["resource_name"] != nil || bodyOf(t, e.posts()[0])["validateOnly"] != true {
			t.Fatalf("out = %s, err = %v, body = %s", out, err, e.posts()[0].body)
		}
	})
}

// A resource name of another link or account never passes: the request was sent, so the outcome is open.
func TestLinkUpdateAnswersAreBoundToTheCheckedLinkAndTheRequest(t *testing.T) {
	cases := map[string]struct{ id, path, body string }{
		"manager another link": {customerManagerLinksUpdateID, managerLinksPath(clientID),
			`{"results":[{"resourceName":"` + managerLinkNameOf(clientID, managerID, "999") + `"}]}`},
		"manager another account": {customerManagerLinksUpdateID, managerLinksPath(clientID),
			`{"results":[{"resourceName":"` + managerLinkNameOf(otherID, managerID, linkNo) + `"}]}`},
		"manager another counterpart": {customerManagerLinksUpdateID, managerLinksPath(clientID),
			`{"results":[{"resourceName":"` + managerLinkNameOf(clientID, otherID, linkNo) + `"}]}`},
		"manager no results":   {customerManagerLinksUpdateID, managerLinksPath(clientID), `{}`},
		"manager empty result": {customerManagerLinksUpdateID, managerLinksPath(clientID), `{"results":[{}]}`},
		"manager two results": {customerManagerLinksUpdateID, managerLinksPath(clientID),
			`{"results":[{"resourceName":"` + managerLinkNameOf(clientID, managerID, linkNo) + `"},{"resourceName":"` +
				managerLinkNameOf(clientID, managerID, linkNo) + `"}]}`},
		"client another link": {customerClientLinksUpdateID, clientLinksPath(managerID),
			`{"result":{"resourceName":"` + clientLinkNameOf(managerID, clientID, "999") + `"}}`},
		"client another account": {customerClientLinksUpdateID, clientLinksPath(managerID),
			`{"result":{"resourceName":"` + clientLinkNameOf(otherID, clientID, linkNo) + `"}}`},
		"client a manager link": {customerClientLinksUpdateID, clientLinksPath(managerID),
			`{"result":{"resourceName":"` + managerLinkNameOf(managerID, clientID, linkNo) + `"}}`},
		"client no result":    {customerClientLinksUpdateID, clientLinksPath(managerID), `{}`},
		"client empty result": {customerClientLinksUpdateID, clientLinksPath(managerID), `{"result":{}}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			customer := clientID
			if c.id == customerClientLinksUpdateID {
				customer = managerID
			}
			e.fake.routes["POST "+c.path] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, linkCall(c.id, customer))
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestLinkUpdateErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"invalid status change", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "managerLinkError",
			"INVALID_STATUS_CHANGE", "")}, provider.ClassProviderError,
			"rejected the change (managerLinkError INVALID_STATUS_CHANGE), nothing was changed", false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"token refused", reply{status: 401, body: adsFailure(401, "UNAUTHENTICATED", "authenticationError",
			"OAUTH_TOKEN_EXPIRED", "")}, provider.ClassAuth, "access token", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"cut connection", reply{err: errors.New("connection reset by peer")}, provider.ClassUnreachable,
			"outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"not json", reply{status: 200, body: "<html>", header: http.Header{"Content-Type": {"text/html"}}},
			provider.ClassInvalidResponse, "outcome of the change is unknown", true},
		{"unreadable json", okReply(`{"results":`), provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for _, id := range []string{customerManagerLinksUpdateID, customerClientLinksUpdateID} {
		path, customer := managerLinksPath(clientID), clientID
		if id == customerClientLinksUpdateID {
			path, customer = clientLinksPath(managerID), managerID
		}
		for _, c := range cases {
			t.Run(id+" "+c.name, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+path] = c.reply
				_, err := e.invokeConfirmed(id, linkCall(id, customer))
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
				if len(e.posts()) != 1 || len(e.fake.api()) != 1 || e.fake.tokenRequests() != 1 {
					t.Errorf("requests = %d of %d, tokens = %d, want one and no repeat", len(e.posts()), len(e.fake.api()),
						e.fake.tokenRequests())
				}
			})
		}
	}
}

// Links are access control: on a connection with targets all four tools need a manager target that names a
// manager account.
func TestLinkToolsNeedAManagerTargetOnManagerAccounts(t *testing.T) {
	answers := map[string]string{
		customerManagerLinksUpdateID: `{"results":[{"resourceName":"` + managerLinkNameOf(managerID, managerID, linkNo) + `"}]}`,
		customerClientLinksUpdateID:  `{"result":{"resourceName":"` + clientLinkNameOf(managerID, clientID, linkNo) + `"}}`,
	}
	paths := map[string]string{customerManagerLinksUpdateID: managerLinksPath(managerID), customerClientLinksUpdateID: clientLinksPath(managerID)}
	// The manager link of the manager account names another manager, so its own call differs from the shared one.
	callOf := func(id string) string {
		if id == customerManagerLinksUpdateID {
			return `{"customer_id":"` + managerID + `","manager_customer_id":"` + subManager + `","manager_link_id":"` + linkNo + `","status":"INACTIVE"}`
		}
		return linkCall(id, managerID)
	}
	answers[customerManagerLinksUpdateID] = `{"results":[{"resourceName":"` + managerLinkNameOf(managerID, subManager, linkNo) + `"}]}`
	managerAnswer := func(manager bool) func(s seen) *reply {
		return func(s seen) *reply {
			switch {
			case strings.HasPrefix(s.query, "SELECT customer.id, customer.manager"):
				return &reply{status: 200, body: fmt.Sprintf(`{"results":[{"customer":{"id":"%s","manager":%t}}]}`, managerID, manager)}
			case strings.Contains(s.query, "FROM customer_manager_link") || strings.Contains(s.query, "FROM customer_client_link"):
				return &reply{status: 200, body: `{"results":[]}`}
			}
			return nil
		}
	}
	for _, id := range linkTools {
		call := func() string { return linkCall(id, managerID) }
		if id == customerManagerLinksUpdateID {
			call = func() string { return callOf(id) }
		}
		t.Run(id+" a customer target that names a manager is refused", func(t *testing.T) {
			e := newLinksEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = managerAnswer(true)
			if p, ok := paths[id]; ok {
				e.fake.routes["POST "+p] = okReply(answers[id])
			}
			_, err := e.invokeConfirmed(id, call())
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
			for _, s := range e.fake.api() {
				if strings.Contains(s.query, "FROM customer_manager_link") || strings.Contains(s.query, "FROM customer_client_link") {
					t.Errorf("the tool read the manager's data: %+v", s)
				}
			}
		})
		t.Run(id+" a manager target that names it passes without the check", func(t *testing.T) {
			e := newLinksEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.answer = managerAnswer(true)
			if p, ok := paths[id]; ok {
				e.fake.routes["POST "+p] = okReply(answers[id])
			}
			if _, err := e.invokeConfirmed(id, call()); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 {
				t.Errorf("requests = %+v, want the call alone", got)
			}
		})
	}
}

func TestManagerLinksListMapsAndPages(t *testing.T) {
	e := newEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+searchPath(clientID)] = rows(
		managerLinkItem(clientID, managerID, "11", "PENDING"),
		managerLinkItem(clientID, subManager, "12", "ACTIVE"),
		managerLinkItem(clientID, otherID, "13", "REFUSED"))
	out, err := e.invoke(customerManagerLinksListID, `{"customer_id":"`+clientID+`","page_size":2,`+
		`"statuses":["PENDING","ACTIVE"],"manager_link_ids":["11","12"],"manager_customer_ids":["`+managerID+`","`+subManager+`"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	list := got["manager_links"].([]any)
	first := list[0].(map[string]any)
	if len(list) != 2 || got["has_more"] != true || got["next_cursor"] == "" || first["manager_link_id"] != "11" ||
		first["manager_customer"] != "customers/"+managerID || first["manager_customer_id"] != managerID ||
		first["status"] != "PENDING" || first["resource_name"] != managerLinkNameOf(clientID, managerID, "11") {
		t.Errorf("out = %s", out)
	}
	api := e.fake.api()
	want := "SELECT customer_manager_link.resource_name, customer_manager_link.manager_link_id, " +
		"customer_manager_link.manager_customer, customer_manager_link.status FROM customer_manager_link WHERE " +
		"customer_manager_link.status IN (PENDING, ACTIVE) AND customer_manager_link.manager_link_id IN (11, 12) AND " +
		"customer_manager_link.manager_customer IN ('customers/" + managerID + "', 'customers/" + subManager + "') " +
		"ORDER BY customer_manager_link.manager_link_id LIMIT 3"
	if len(api) != 1 || api[0].query != want || api[0].login != loginID {
		t.Errorf("request = %+v\nwant %s", api, want)
	}
	cursor := got["next_cursor"].(string)
	e.fake.routes["POST "+searchPath(clientID)] = rows(managerLinkItem(clientID, otherID, "13", "REFUSED"))
	arguments := `{"customer_id":"` + clientID + `","page_size":2,"statuses":["PENDING","ACTIVE"],"manager_link_ids":["11","12"],` +
		`"manager_customer_ids":["` + managerID + `","` + subManager + `"],"cursor":"` + cursor + `"}`
	out, err = e.invoke(customerManagerLinksListID, arguments)
	if err != nil || decode(t, out)["has_more"] != false ||
		!strings.Contains(e.fake.api()[1].query, "customer_manager_link.manager_link_id > 12 ORDER BY") {
		t.Fatalf("out = %s, err = %v", out, err)
	}
	if _, err := e.invoke(customerManagerLinksListID, `{"customer_id":"`+clientID+`","cursor":"`+cursor+`"}`); err == nil {
		t.Error("a cursor of other arguments was accepted")
	}
}

func TestClientLinksListMapsAndFilters(t *testing.T) {
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(managerID)] = rows(
		clientLinkItem(managerID, clientID, "21", "ACTIVE", true),
		clientLinkItem(managerID, otherID, "22", "PENDING", false))
	out, err := e.invoke(customerClientLinksListID, `{"customer_id":"`+managerID+`","statuses":["ACTIVE","PENDING"],`+
		`"client_customer_ids":["`+clientID+`","`+otherID+`"],"hidden":true,"manager_link_ids":["21","22"]}`)
	if err != nil {
		t.Fatal(err)
	}
	list := decode(t, out)["client_links"].([]any)
	first, second := list[0].(map[string]any), list[1].(map[string]any)
	if len(list) != 2 || first["client_customer_id"] != clientID || first["client_customer"] != "customers/"+clientID ||
		first["hidden"] != true || first["status"] != "ACTIVE" ||
		first["resource_name"] != clientLinkNameOf(managerID, clientID, "21") ||
		second["hidden"] != nil && second["hidden"] != false {
		t.Errorf("out = %s", out)
	}
	want := "SELECT customer_client_link.resource_name, customer_client_link.manager_link_id, " +
		"customer_client_link.client_customer, customer_client_link.status, customer_client_link.hidden FROM " +
		"customer_client_link WHERE customer_client_link.status IN (ACTIVE, PENDING) AND " +
		"customer_client_link.manager_link_id IN (21, 22) AND customer_client_link.client_customer IN ('customers/" +
		clientID + "', 'customers/" + otherID + "') AND customer_client_link.hidden = true " +
		"ORDER BY customer_client_link.manager_link_id LIMIT 101"
	if api := e.fake.api(); len(api) != 1 || api[0].query != want {
		t.Errorf("request = %+v\nwant %s", api, want)
	}
}

func TestLinkListFiltersAreCheckedBeforeSecretAndIO(t *testing.T) {
	bad := map[string][]string{
		customerManagerLinksListID: {`"statuses":["ENABLED"]`, `"statuses":["ACTIVE","ACTIVE"]`, `"statuses":["ACTIVE'"]`,
			`"manager_link_ids":["1 OR 1=1"]`, `"manager_link_ids":["0"]`, `"manager_customer_ids":["123"]`,
			`"manager_customer_ids":["1000000001' OR '1'='1"]`, `"client_customer_ids":["` + clientID + `"]`, `"hidden":true`,
			`"page_size":0`, `"page_size":1001`, `"cursor":"not-a-cursor"`},
		customerClientLinksListID: {`"statuses":["UNKNOWN"]`, `"client_customer_ids":["customers/1"]`,
			`"manager_customer_ids":["` + managerID + `"]`, `"hidden":"yes"`, `"page_size":-1`, `"cursor":"x"`},
	}
	for id, list := range bad {
		for _, filter := range list {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`",`+filter+`}`); err == nil {
				t.Errorf("%s accepted %s", id, filter)
			}
			e.untouched(t)
		}
	}
	// The handlers refuse as well when the schema is bypassed.
	e := newEnvironment(t, nil, nil)
	for _, filter := range []string{`"statuses":["ACTIVE'"]`, `"manager_customer_ids":["1' OR '1"]`, `"hidden":true`} {
		_, err := handleManagerLinksList(context.Background(), e.resolved(), e.secrets(), e.red,
			json.RawMessage(`{"customer_id":"`+clientID+`",`+filter+`}`))
		if !isInvalidRequest(err) {
			t.Errorf("handler accepted %s: %v", filter, err)
		}
	}
	e.untouched(t)
}

func TestLinkListsRefuseWrongOrForeignRows(t *testing.T) {
	rowsOf := map[string][]string{
		customerManagerLinksListID: {
			managerLinkItem(otherID, managerID, "21", "PENDING"),
			strings.Replace(managerLinkItem(clientID, managerID, "21", "PENDING"), `"managerLinkId":"21"`, `"managerLinkId":"22"`, 1),
			strings.Replace(managerLinkItem(clientID, managerID, "21", "PENDING"), `"managerCustomer":"customers/`+managerID,
				`"managerCustomer":"customers/`+otherID, 1),
			`{"customerManagerLink":{"managerLinkId":"21"}}`,
			`{"customerManagerLink":{"managerCustomer":"customers/` + managerID + `"}}`,
			`{"customerManagerLink":{"managerLinkId":"-4","managerCustomer":"customers/` + managerID + `"}}`,
			`{"customerManagerLink":{"managerLinkId":"21","managerCustomer":"` + otherID + `"}}`, `{}`},
		customerClientLinksListID: {
			clientLinkItem(otherID, clientID, "21", "ACTIVE", false),
			strings.Replace(clientLinkItem(clientID, clientID, "21", "ACTIVE", false), `"managerLinkId":"21"`, `"managerLinkId":"22"`, 1),
			`{"customerClientLink":{"managerLinkId":"21"}}`, `{}`},
	}
	for id, bad := range rowsOf {
		for i, row := range bad {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = rows(row)
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("%s row %d: err = %v, want invalid-provider-response", id, i, err)
			}
		}
	}
	e := newEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = rows(managerLinkItem(clientID, managerID, "12", "ACTIVE"),
		managerLinkItem(clientID, subManager, "11", "ACTIVE"))
	if _, err := e.invoke(customerManagerLinksListID, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("rows out of order: err = %v", err)
	}
}

func TestLinkListsOfAnAccountWithACustomerTargetShowOnlyNamedOtherEnds(t *testing.T) {
	e := newEnvironmentWith(t, []string{"customers/" + clientID, "customers/" + subManager}, nil,
		config.Connection{Effects: []config.Effect{config.EffectRead}})
	e.fake.answer = func(s seen) *reply {
		if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
			return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + clientID + `"}}]}`}
		}
		return nil
	}
	e.fake.routes["POST "+searchPath(clientID)] = rows(managerLinkItem(clientID, managerID, "11", "ACTIVE"),
		managerLinkItem(clientID, subManager, "12", "ACTIVE"))
	out, err := e.invoke(customerManagerLinksListID, `{"customer_id":"`+clientID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	list := decode(t, out)["manager_links"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["manager_customer_id"] != subManager || strings.Contains(out, managerID) {
		t.Errorf("out = %s", out)
	}
	// Below a manager target every row is in reach.
	e = newEnvironment(t, []string{"managers/" + managerID}, nil)
	e.fake.answer = func(s seen) *reply {
		switch {
		case strings.Contains(s.query, "FROM customer_client WHERE"):
			return &reply{status: 200, body: `{"results":[` + proofItem(clientID) + `]}`}
		case strings.HasPrefix(s.query, "SELECT customer.id, customer.manager"):
			return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + clientID + `"}}]}`}
		}
		return nil
	}
	e.fake.routes["POST "+searchPath(clientID)] = rows(managerLinkItem(clientID, otherID, "11", "ACTIVE"))
	out, err = e.invoke(customerManagerLinksListID, `{"customer_id":"`+clientID+`"}`)
	if err != nil || len(decode(t, out)["manager_links"].([]any)) != 1 {
		t.Fatalf("out = %s, err = %v", out, err)
	}
}

func TestLinkListErrorsAreMapped(t *testing.T) {
	for _, id := range []string{customerManagerLinksListID, customerClientLinksListID} {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED",
			"authorizationError", "USER_PERMISSION_DENIED", "")}
		_, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`)
		if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), detailCanary) {
			t.Errorf("%s: err = %v", id, err)
		}
		e = newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED",
			"quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"5s"}`)}
		if _, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassRateLimited {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}

func TestLinkSummariesNameTheEffect(t *testing.T) {
	refuse := linkUpdateSummary(true)(json.RawMessage(linkCall(customerManagerLinksUpdateID, clientID)))
	end := linkUpdateSummary(true)(json.RawMessage(`{"customer_id":"` + clientID + `","manager_customer_id":"` + managerID +
		`","manager_link_id":"` + linkNo + `","status":"INACTIVE","validate_only":true}`))
	cancel := linkUpdateSummary(false)(json.RawMessage(linkCall(customerClientLinksUpdateID, managerID)))
	hide := linkUpdateSummary(false)(json.RawMessage(`{"customer_id":"` + managerID + `","client_customer_id":"` + clientID +
		`","manager_link_id":"` + linkNo + `","hidden":true}`))
	for _, c := range []struct{ text, word string }{{refuse, "Decline"}, {refuse, clientID}, {refuse, linkNo}, {refuse, managerID},
		{end, "End"}, {end, "Only validate"}, {cancel, "withdraw the request"}, {cancel, clientID}, {cancel, linkNo},
		{hide, "hide the link"}, {hide, managerID}} {
		if !strings.Contains(c.text, c.word) {
			t.Errorf("summary %q lacks %q", c.text, c.word)
		}
	}
}
