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

// mail composes a synthetic address at run time, so that no address stands in the source.
func mail(local string) string { return local + "@" + "mail" + ".example" + ".test" }

const (
	userA = "1122334455"
	userB = "1122334466"
)

func accessPath(id string) string { return "/v26/customers/" + id + "/customerUserAccesses:mutate" }

func accessName(customer, user string) string {
	return "customers/" + queryNumber(customer) + "/customerUserAccesses/" + user
}

// accessItem is a customer_user_access row. passkeyEnabled is sent on purpose: it must never come out.
func accessItem(customer, user, role string) string {
	return `{"customerUserAccess":{"resourceName":"` + accessName(customer, user) + `","userId":"` + user +
		`","emailAddress":"` + mail("user"+user) + `","accessRole":"` + role +
		`","accessCreationDateTime":"2026-01-02 03:04:05","inviterUserEmailAddress":"` + mail("owner") +
		`","passkeyEnabled":true,"pendingMultiPartyAuthReview":"customers/` + queryNumber(customer) +
		`/multiPartyAuthReviews/77"}}`
}

func TestUserAccessToolsAreDescribedAndScoped(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	list, _, ok := reg.Lookup(customerUserAccessesListID)
	if !ok || list.RequiresToolAllowList || list.Risk.Effect != capability.EffectRead || list.Version != 1 ||
		!slices.Contains(metadata.Profiles[0].Tools, customerUserAccessesListID) {
		t.Fatalf("list = %+v", list)
	}
	for _, word := range []string{"personal data", "passkey", "manager target", "never returned"} {
		if !strings.Contains(list.Description, word) {
			t.Errorf("list description lacks %q", word)
		}
	}
	if !strings.Contains(list.Risk.DataSensitivity, "e-mail addresses") {
		t.Errorf("sensitivity = %q", list.Risk.DataSensitivity)
	}
	for _, id := range []string{customerUserAccessesListID, customerUserAccessesUpdateID, customerUserAccessesRemoveID} {
		d, _, _ := reg.Lookup(id)
		for _, schemaText := range []string{string(d.InputSchema), string(d.OutputSchema)} {
			if strings.Contains(schemaText, "passkey") {
				t.Errorf("%s: a schema names passkey", id)
			}
		}
		for _, field := range d.Fields {
			if strings.Contains(strings.ToLower(field.Name), "passkey") {
				t.Errorf("%s: field %s", id, field.Name)
			}
		}
	}
	update, _, _ := reg.Lookup(customerUserAccessesUpdateID)
	remove, _, _ := reg.Lookup(customerUserAccessesRemoveID)
	if update.Risk.Effect != capability.EffectUpdate || remove.Risk.Effect != capability.EffectDelete {
		t.Errorf("effects = %s, %s", update.Risk.Effect, remove.Risk.Effect)
	}
	for _, d := range []capability.Descriptor{update, remove} {
		for _, word := range []string{"invitation", "multi-party", "no validate-only"} {
			if !strings.Contains(strings.ToLower(d.Description), strings.ToLower(word)) {
				t.Errorf("%s lacks %q", d.ID, word)
			}
		}
		var in struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		_ = json.Unmarshal(d.InputSchema, &in)
		for _, forbidden := range []string{"validate_only", "response_content_type", "operations", "partial_failure",
			"resource_name", "email_address", "login_customer_id"} {
			if _, found := in.Properties[forbidden]; found {
				t.Errorf("%s offers %s", d.ID, forbidden)
			}
		}
	}
}

func TestUserAccessChangesNeedToolsListConfirmationAndForeignArgumentsAreRefused(t *testing.T) {
	calls := map[string]string{
		customerUserAccessesUpdateID: `{"customer_id":"` + clientID + `","user_id":"` + userA + `","access_role":"READ_ONLY"}`,
		customerUserAccessesRemoveID: `{"customer_id":"` + clientID + `","user_id":"` + userA + `"}`,
	}
	for id, arguments := range calls {
		t.Run(id+" without the tools list", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			_, err := e.invoke(id, arguments)
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) ||
				!strings.Contains(needed.Summary, userA) {
				t.Fatalf("err = %v", err)
			}
			e.untouched(t)
		})
		for _, extra := range []string{`"login_customer_id":"` + loginID + `"`, `"validate_only":true`, `"partial_failure":true`,
			`"operations":[]`, `"response_content_type":"MUTABLE_RESOURCE"`, `"email_address":"` + mail("x") + `"`,
			`"resource_name":"customers/` + otherID + `/customerUserAccesses/1"`} {
			t.Run(id+" "+extra, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				if _, err := e.invokeConfirmed(id, strings.TrimSuffix(arguments, "}")+","+extra+"}"); err == nil {
					t.Fatal("accepted")
				}
				e.untouched(t)
			})
		}
		t.Run(id+" outside the targets", func(t *testing.T) {
			e := newMutationEnvironment(t, []string{"customers/" + otherID}, nil)
			_, err := e.invokeConfirmed(id, arguments)
			if !isInvalidRequest(err) || strings.Contains(err.Error(), clientID) {
				t.Fatalf("err = %v", err)
			}
			e.untouched(t)
		})
	}
	bad := map[string][]string{
		customerUserAccessesUpdateID: {
			`{"customer_id":"` + clientID + `","user_id":"` + userA + `","access_role":"OWNER"}`,
			`{"customer_id":"` + clientID + `","user_id":"` + userA + `","access_role":"UNSPECIFIED"}`,
			`{"customer_id":"` + clientID + `","user_id":"` + userA + `"}`,
		},
	}
	for _, id := range []string{customerUserAccessesUpdateID, customerUserAccessesRemoveID} {
		role := ""
		if id == customerUserAccessesUpdateID {
			role = `,"access_role":"ADMIN"`
		}
		for _, userID := range []string{"0", "-1", "01", "1/../2", "abc", "9223372036854775808", " 1", ""} {
			bad[id] = append(bad[id], `{"customer_id":"`+clientID+`","user_id":"`+userID+`"`+role+`}`)
		}
		bad[id] = append(bad[id], `{"customer_id":"123","user_id":"`+userA+`"`+role+`}`,
			`{"user_id":"`+userA+`"`+role+`}`, `{"customer_id":"`+clientID+`"`+role+`}`)
	}
	for id, list := range bad {
		for _, arguments := range list {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments); err == nil {
				t.Errorf("%s accepted %s", id, arguments)
			}
			e.untouched(t)
		}
	}
}

func TestUserAccessUpdateAndRemoveSendOneOperation(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
		e.fake.routes["POST "+accessPath(clientID)] = okReply(`{"result":{"resourceName":"` + accessName(clientID, userA) + `"}}`)
		out, err := e.invokeConfirmed(customerUserAccessesUpdateID,
			`{"customer_id":"`+clientID+`","user_id":"`+userA+`","access_role":"STANDARD"}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		if got["resource_name"] != accessName(clientID, userA) || got["user_id"] != userA || got["customer_id"] != clientID ||
			got["multi_party_auth_review"] != nil {
			t.Errorf("out = %s", out)
		}
		posts := e.posts()
		if len(posts) != 1 || len(e.fake.api()) != 1 {
			t.Fatalf("requests = %+v", e.fake.api())
		}
		p := posts[0]
		if p.url != "https://googleads.googleapis.com"+accessPath(clientID) || p.login != loginID ||
			p.auth != "Bearer synthetic-access-1" || p.developerToken != "" {
			t.Errorf("request = %+v", p)
		}
		want := map[string]any{"operation": map[string]any{"updateMask": "accessRole", "update": map[string]any{
			"resourceName": accessName(clientID, userA), "accessRole": "STANDARD"}}}
		if !reflect.DeepEqual(bodyOf(t, p), want) {
			t.Errorf("body = %s", p.body)
		}
	})
	t.Run("remove", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+accessPath(clientID)] = okReply(`{"result":{"resourceName":"` + accessName(clientID, userA) + `"}}`)
		if _, err := e.invokeConfirmed(customerUserAccessesRemoveID,
			`{"customer_id":"`+clientID+`","user_id":"`+userA+`"}`); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"operation": map[string]any{"remove": accessName(clientID, userA)}}
		if posts := e.posts(); len(posts) != 1 || !reflect.DeepEqual(bodyOf(t, posts[0]), want) {
			t.Errorf("posts = %+v", posts)
		}
	})
	t.Run("a review of Google is reported", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		review := "customers/" + queryNumber(clientID) + "/multiPartyAuthReviews/88"
		e.fake.routes["POST "+accessPath(clientID)] = okReply(`{"result":{"resourceName":"` + accessName(clientID, userA) +
			`","multiPartyAuthReview":"` + review + `"}}`)
		out, err := e.invokeConfirmed(customerUserAccessesUpdateID,
			`{"customer_id":"`+clientID+`","user_id":"`+userA+`","access_role":"ADMIN"}`)
		if err != nil || decode(t, out)["multi_party_auth_review"] != review {
			t.Fatalf("out = %s, err = %v", out, err)
		}
	})
}

// A name of another account or user never passes, however the answer looks otherwise; the change may have been
// made, so the error says that the outcome is unknown.
func TestUserAccessResultsAreBoundToTheCheckedAccount(t *testing.T) {
	foreign := "customers/" + queryNumber(otherID)
	for name, body := range map[string]string{
		"other account": `{"result":{"resourceName":"` + accessName(otherID, userA) + `"}}`,
		"other user":    `{"result":{"resourceName":"` + accessName(clientID, userB) + `"}}`,
		"no result":     `{}`,
		"empty result":  `{"result":{}}`,
		"foreign review": `{"result":{"resourceName":"` + accessName(clientID, userA) +
			`","multiPartyAuthReview":"` + foreign + `/multiPartyAuthReviews/1"}}`,
		"review path": `{"result":{"resourceName":"` + accessName(clientID, userA) +
			`","multiPartyAuthReview":"customers/` + queryNumber(clientID) + `/multiPartyAuthReviews/../x"}}`,
	} {
		for _, id := range []string{customerUserAccessesUpdateID, customerUserAccessesRemoveID} {
			t.Run(name+" "+id, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+accessPath(clientID)] = okReply(body)
				arguments := `{"customer_id":"` + clientID + `","user_id":"` + userA + `"`
				if id == customerUserAccessesUpdateID {
					arguments += `,"access_role":"READ_ONLY"`
				}
				_, err := e.invokeConfirmed(id, arguments+`}`)
				if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
					len(e.posts()) != 1 {
					t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
				}
			})
		}
	}
}

func TestUserAccessErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"refused by Google", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "customerUserAccessError",
			"INVALID_USER_ID", "")}, provider.ClassProviderError, "customerUserAccessError INVALID_USER_ID", false},
		{"quota", reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError",
			"RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}, provider.ClassRateLimited, "too many requests", false},
		{"server error", reply{status: 503, body: googleError(503, "UNAVAILABLE", "")},
			provider.ClassifyStatus("op", 503, provider.StatusTexts{Subject: subject}).Class, "outcome of the change is unknown", true},
		{"cut connection", reply{err: errors.New("connection reset by peer")}, provider.ClassUnreachable,
			"outcome of the change is unknown", true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, "outcome of the change is unknown", true},
		{"unreadable json", okReply(`{"result":`), provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for _, c := range cases {
		for _, id := range []string{customerUserAccessesUpdateID, customerUserAccessesRemoveID} {
			t.Run(c.name+" "+id, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+accessPath(clientID)] = c.reply
				arguments := `{"customer_id":"` + clientID + `","user_id":"` + userA + `"`
				if id == customerUserAccessesUpdateID {
					arguments += `,"access_role":"READ_ONLY"`
				}
				_, err := e.invokeConfirmed(id, arguments+`}`)
				if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) {
					t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != c.unknown {
					t.Errorf("unknown outcome reported = %v, want %v: %v", got, c.unknown, err)
				}
				for _, leaked := range []string{detailCanary, triggerCanary} {
					if strings.Contains(err.Error(), leaked) {
						t.Errorf("error carries %q", leaked)
					}
				}
				if len(e.posts()) != 1 || len(e.fake.api()) != 1 {
					t.Errorf("requests = %+v, want one and no repeat", e.fake.api())
				}
			})
		}
	}
}

// The manager rule holds for the read and both changes: a manager account needs a manager target that names it.
// newReadChangeEnvironment names the list tool and the changing tools in the tools list.
func newReadChangeEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: append([]string{customerUserAccessesListID}, changeToolIDs...)})
}

func TestUserAccessOnManagerAccountsNeedAManagerTarget(t *testing.T) {
	managerCheck := func(manager bool) func(seen) *reply {
		return func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				r := rows(`{"customer":{"id":"` + managerID + `","manager":` + map[bool]string{true: "true", false: "false"}[manager] + `}}`)
				return &r
			}
			return nil
		}
	}
	type call struct{ id, arguments string }
	calls := []call{
		{customerUserAccessesListID, `{"customer_id":"` + managerID + `"}`},
		{customerUserAccessesUpdateID, `{"customer_id":"` + managerID + `","user_id":"` + userA + `","access_role":"READ_ONLY"}`},
		{customerUserAccessesRemoveID, `{"customer_id":"` + managerID + `","user_id":"` + userA + `"}`},
	}
	for _, c := range calls {
		t.Run(c.id+" customer target naming a manager", func(t *testing.T) {
			e := newReadChangeEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = managerCheck(true)
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") {
				t.Fatalf("err = %v", err)
			}
			for _, s := range e.fake.api() {
				if !strings.Contains(s.query, "customer.manager") {
					t.Errorf("request after the refusal: %+v", s)
				}
			}
		})
		t.Run(c.id+" manager target naming it", func(t *testing.T) {
			e := newReadChangeEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.routes["POST "+accessPath(managerID)] = okReply(`{"result":{"resourceName":"` + accessName(managerID, userA) + `"}}`)
			e.fake.routes["POST "+searchPath(managerID)] = rows(accessItem(managerID, userA, "ADMIN"))
			if _, err := e.invokeConfirmed(c.id, c.arguments); err != nil {
				t.Fatal(err)
			}
			for _, s := range e.fake.api() {
				if strings.Contains(s.query, "customer.manager") {
					t.Errorf("manager check although the target names the account: %+v", s)
				}
			}
		})
		t.Run(c.id+" unreadable check is never a no", func(t *testing.T) {
			e := newReadChangeEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = func(s seen) *reply {
				r := rows()
				return &r
			}
			_, err := e.invokeConfirmed(c.id, c.arguments)
			if classOf(err) != provider.ClassInvalidResponse || len(e.posts()) != 0 {
				t.Fatalf("err = %v", err)
			}
		})
	}
	t.Run("a client account below a manager target passes after the proof and the check", func(t *testing.T) {
		e := newMutationEnvironment(t, []string{"managers/" + managerID}, map[string]string{optionLoginCustomerID: managerID})
		e.fake.routes["POST "+accessPath(clientID)] = okReply(`{"result":{"resourceName":"` + accessName(clientID, userA) + `"}}`)
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
		if _, err := e.invokeConfirmed(customerUserAccessesRemoveID, `{"customer_id":"`+clientID+`","user_id":"`+userA+`"}`); err != nil {
			t.Fatal(err)
		}
		got := e.fake.api()
		if len(got) != 3 || !strings.Contains(got[0].query, "customer_client") || !strings.Contains(got[1].query, "customer.manager") ||
			!strings.HasSuffix(got[2].path, ":mutate") {
			t.Errorf("requests = %+v", got)
		}
	})
}

func TestUserAccessesListQueryAndMapping(t *testing.T) {
	e := newEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+searchPath(clientID)] = rows(accessItem(clientID, userA, "ADMIN"), accessItem(clientID, userB, "READ_ONLY"))
	out, err := e.invoke(customerUserAccessesListID, `{"customer_id":"`+clientID+`","user_ids":["`+userA+`","`+userB+`"],`+
		`"access_roles":["ADMIN","READ_ONLY"],"email_address_contains":"user_1"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 1 || api[0].login != loginID {
		t.Fatalf("requests = %+v", api)
	}
	q := api[0].query
	for _, part := range []string{"FROM customer_user_access", "customer_user_access.user_id IN (" + userA + ", " + userB + ")",
		"customer_user_access.access_role IN ('ADMIN', 'READ_ONLY')",
		"customer_user_access.email_address LIKE '%user[_]1%'", "ORDER BY customer_user_access.user_id LIMIT 101",
		"customer_user_access.pending_multi_party_auth_review"} {
		if !strings.Contains(q, part) {
			t.Errorf("query %q lacks %q", q, part)
		}
	}
	if strings.Contains(q, "passkey") {
		t.Errorf("query selects passkey_enabled: %s", q)
	}
	if strings.Contains(out, "passkey") || strings.Contains(out, "true") {
		t.Errorf("out carries the passkey: %s", out)
	}
	got := decode(t, out)
	accesses := got["user_accesses"].([]any)
	first := accesses[0].(map[string]any)
	if len(accesses) != 2 || got["has_more"] != false || first["user_id"] != userA ||
		first["resource_name"] != accessName(clientID, userA) || first["email_address"] != mail("user"+userA) ||
		first["access_role"] != "ADMIN" || first["inviter_user_email_address"] != mail("owner") ||
		first["access_creation_date_time"] != "2026-01-02 03:04:05" ||
		first["pending_multi_party_auth_review"] != "customers/"+queryNumber(clientID)+"/multiPartyAuthReviews/77" {
		t.Errorf("out = %s", out)
	}
	if len(first) != 7 {
		t.Errorf("members = %v", first)
	}
}

func TestUserAccessesListPagesAndRefusesWrongAnswers(t *testing.T) {
	t.Run("cursor", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = rows(accessItem(clientID, userA, "ADMIN"), accessItem(clientID, userB, "ADMIN"))
		out, err := e.invoke(customerUserAccessesListID, `{"customer_id":"`+clientID+`","page_size":1}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decode(t, out)
		cursor, _ := got["next_cursor"].(string)
		if got["has_more"] != true || cursor == "" || len(got["user_accesses"].([]any)) != 1 {
			t.Fatalf("out = %s", out)
		}
		e.fake.routes["POST "+searchPath(clientID)] = rows(accessItem(clientID, userB, "ADMIN"))
		if _, err := e.invoke(customerUserAccessesListID, `{"customer_id":"`+clientID+`","page_size":1,"cursor":"`+cursor+`"}`); err != nil {
			t.Fatal(err)
		}
		if q := e.fake.api()[1].query; !strings.Contains(q, "customer_user_access.user_id > "+userA) {
			t.Errorf("query = %s", q)
		}
		if _, err := e.invoke(customerUserAccessesListID, `{"customer_id":"`+clientID+`","access_roles":["ADMIN"],"cursor":"`+cursor+`"}`); err == nil {
			t.Error("a cursor of other arguments was accepted")
		}
	})
	wrong := map[string]string{
		"other account": accessItem(otherID, userA, "ADMIN"),
		"other user": strings.Replace(accessItem(clientID, userA, "ADMIN"), accessName(clientID, userA),
			accessName(clientID, userB), 1),
		"foreign review": strings.Replace(accessItem(clientID, userA, "ADMIN"), "customers/"+queryNumber(clientID)+"/multiPartyAuthReviews",
			"customers/"+queryNumber(otherID)+"/multiPartyAuthReviews", 1),
		"no user":   `{"customerUserAccess":{"emailAddress":"x"}}`,
		"bad user":  `{"customerUserAccess":{"userId":"-5"}}`,
		"not a row": `{"other":1}`,
	}
	for name, item := range wrong {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = rows(item)
			if _, err := e.invoke(customerUserAccessesListID, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassInvalidResponse {
				t.Fatalf("err = %v", err)
			}
		})
	}
	t.Run("out of order", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = rows(accessItem(clientID, userB, "ADMIN"), accessItem(clientID, userA, "ADMIN"))
		if _, err := e.invoke(customerUserAccessesListID, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("text is cut and cleaned", func(t *testing.T) {
		e := newEnvironment(t, nil, nil)
		item := strings.Replace(accessItem(clientID, userA, "ADMIN"), mail("user"+userA), strings.Repeat("a", 3000)+`\u0007`, 1)
		e.fake.routes["POST "+searchPath(clientID)] = rows(item)
		out, err := e.invoke(customerUserAccessesListID, `{"customer_id":"`+clientID+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		email := decode(t, out)["user_accesses"].([]any)[0].(map[string]any)["email_address"].(string)
		if len(email) > maxText {
			t.Errorf("email = %d bytes", len(email))
		}
	})
}

func TestUserAccessesListChecksBeforeSecretAndIO(t *testing.T) {
	for name, arguments := range map[string]string{
		"outside the targets": `{"customer_id":"` + otherID + `"}`,
		"quote in the e-mail": `{"customer_id":"` + clientID + `","email_address_contains":"a'b"}`,
		"backslash":           `{"customer_id":"` + clientID + `","email_address_contains":"a\\b"}`,
		"control":             `{"customer_id":"` + clientID + `","email_address_contains":"a\u0001b"}`,
		"role":                `{"customer_id":"` + clientID + `","access_roles":["OWNER"]}`,
		"user":                `{"customer_id":"` + clientID + `","user_ids":["x"]}`,
		"page size":           `{"customer_id":"` + clientID + `","page_size":0}`,
		"foreign argument":    `{"customer_id":"` + clientID + `","passkey_enabled":true}`,
		"bad cursor":          `{"customer_id":"` + clientID + `","cursor":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, []string{"customers/" + clientID}, nil)
			if _, err := e.invoke(customerUserAccessesListID, arguments); err == nil {
				t.Fatal("accepted")
			}
			e.untouched(t)
		})
	}
}

func TestUserAccessesListMapsGoogleErrors(t *testing.T) {
	for name, r := range map[string]reply{
		"permission": {status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError", "USER_PERMISSION_DENIED", "")},
		"quota": {status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED", "quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED",
			`{"retryDelay":"5s"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = r
			_, err := e.invoke(customerUserAccessesListID, `{"customer_id":"`+clientID+`"}`)
			want := map[string]provider.Class{"permission": provider.ClassPermission, "quota": provider.ClassRateLimited}[name]
			if classOf(err) != want || strings.Contains(err.Error(), detailCanary) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestUserAccessSummaries(t *testing.T) {
	update := userAccessSummary(true)(json.RawMessage(`{"customer_id":"` + clientID + `","user_id":"` + userA + `","access_role":"ADMIN"}`))
	for _, word := range []string{clientID, userA, "ADMIN", "controls the access"} {
		if !strings.Contains(update, word) {
			t.Errorf("summary %q lacks %q", update, word)
		}
	}
	remove := userAccessSummary(false)(json.RawMessage(`{"customer_id":"` + clientID + `","user_id":"` + userA + `"}`))
	for _, word := range []string{clientID, userA, "Remove", "loses all access"} {
		if !strings.Contains(remove, word) {
			t.Errorf("summary %q lacks %q", remove, word)
		}
	}
}
