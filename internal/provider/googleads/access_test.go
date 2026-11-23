package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const (
	invitationID = "1122334455"
	reviewID     = "5566778899"
)

// address assembles a synthetic e-mail address at run time, so that no literal address stands in the tests.
func address(local string) string { return local + string(rune(64)) + "invitees.example" }

func invitationsPath(id string) string {
	return "/v26/customers/" + id + "/customerUserAccessInvitations:mutate"
}
func reviewResolvePath(id string) string {
	return "/v26/customers/" + id + "/multiPartyAuthReview:resolve"
}

func invitationNameOf(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/customerUserAccessInvitations/" + id
}
func reviewNameOf(customer, id string) string {
	return "customers/" + queryNumber(customer) + "/multiPartyAuthReviews/" + id
}

// invitationItem is a customer_user_access_invitation row of the account.
func invitationItem(customer, id, email, status string) string {
	return fmt.Sprintf(`{"customerUserAccessInvitation":{"resourceName":"%s","invitationId":"%s","accessRole":"STANDARD",`+
		`"emailAddress":"%s","creationDateTime":"2026-11-02 09:15:00","invitationStatus":"%s"}}`,
		invitationNameOf(customer, id), id, email, status)
}

func reviewItem(customer, id string) string {
	return fmt.Sprintf(`{"multiPartyAuthReview":{"resourceName":"%s","multiPartyAuthReviewId":"%s","creationDateTime":`+
		`"2026-11-03 10:00:00","reviewStatus":"PENDING","justification":"%s","requestUserEmail":"%s",`+
		`"operationType":"UPDATE","targetResource":"CUSTOMER_USER_ACCESS","customerUserAccessReview":{`+
		`"oldCustomerUserAccess":"customers/%s/customerUserAccesses/77","newCustomerUserAccess":{`+
		`"resourceName":"customers/%s/customerUserAccesses/77","userId":"77","emailAddress":"%s","accessRole":"ADMIN",`+
		`"accessCreationDateTime":"2026-01-01 08:00:00","inviterUserEmailAddress":"%s","passkeyEnabled":true,`+
		`"pendingMultiPartyAuthReview":"%s"}}}}`,
		reviewNameOf(customer, id), id, "needs\\u0007 admin", address("requester"), queryNumber(customer),
		queryNumber(customer), address("member"), address("boss"), reviewNameOf(customer, id))
}

func TestAccessToolsAreRegisteredAsDescribed(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for id, effect := range map[string]capability.Effect{customerUserAccessInvitationsListID: capability.EffectRead,
		multiPartyAuthReviewsListID: capability.EffectRead, customerUserAccessInvitationsRemoveID: capability.EffectDelete,
		multiPartyAuthReviewsResolveID: capability.EffectUpdate} {
		d, _, ok := reg.Lookup(id)
		if !ok || d.Version != 1 || d.Risk.Effect != effect || !d.Risk.OpenWorld || len(d.OutputSchema) == 0 {
			t.Fatalf("%s = %+v", id, d)
		}
		listed := effect != capability.EffectRead
		if d.RequiresToolAllowList != listed || slices.Contains(metadata.Profiles[0].Tools, id) == listed {
			t.Errorf("%s: tools list required = %v, in read profile = %v", id, d.RequiresToolAllowList,
				slices.Contains(metadata.Profiles[0].Tools, id))
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
			"validate_only", "path", "url"} {
			if _, found := input.Properties[forbidden]; found {
				t.Errorf("%s offers the argument %s", id, forbidden)
			}
		}
		if !listed {
			if !strings.Contains(d.Risk.DataSensitivity, "personal data of third parties") ||
				!strings.Contains(d.Description, "personal data of other people") {
				t.Errorf("%s does not name the personal data: %q", id, d.Risk.DataSensitivity)
			}
		}
	}
	// No tool sends an invitation or approves a request.
	for _, tool := range metadata.Tools {
		if strings.HasSuffix(tool.ID, ".create") && strings.Contains(tool.ID, "access") ||
			strings.Contains(tool.ID, "approve") {
			t.Errorf("tool %s must not exist", tool.ID)
		}
	}
	remove, _, _ := reg.Lookup(customerUserAccessInvitationsRemoveID)
	var removeInput struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	_ = json.Unmarshal(remove.InputSchema, &removeInput)
	if len(removeInput.Properties) != 2 {
		t.Errorf("remove offers %v, want customer_id and invitation_id only", removeInput.Properties)
	}
	resolve, _, _ := reg.Lookup(multiPartyAuthReviewsResolveID)
	var resolveInput struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(resolve.InputSchema, &resolveInput)
	if !reflect.DeepEqual(resolveInput.Properties["new_status"].Enum, []string{"REJECTED", "REVOKED"}) ||
		strings.Contains(string(resolve.InputSchema), "APPROVED") {
		t.Errorf("resolve schema = %s", resolve.InputSchema)
	}
	for _, id := range []string{customerUserAccessInvitationsRemoveID, multiPartyAuthReviewsResolveID} {
		d, _, _ := reg.Lookup(id)
		for _, word := range []string{"no validate_only", "never repeats", "outcome is unknown"} {
			if !strings.Contains(d.Description, word) {
				t.Errorf("%s: description lacks %q", id, word)
			}
		}
	}
}

var accessCalls = map[string]string{
	customerUserAccessInvitationsListID:   `{"customer_id":"%s"}`,
	multiPartyAuthReviewsListID:           `{"customer_id":"%s"}`,
	customerUserAccessInvitationsRemoveID: `{"customer_id":"%s","invitation_id":"` + invitationID + `"}`,
	multiPartyAuthReviewsResolveID:        `{"customer_id":"%s","multi_party_auth_review_id":"` + reviewID + `","new_status":"REJECTED"}`,
}

func call(id, customer string) string { return fmt.Sprintf(accessCalls[id], customer) }

func newAccessEnvironment(t *testing.T, targets []string, options map[string]string) *environment {
	t.Helper()
	tools := append(slices.Clone(changeToolIDs), customerUserAccessInvitationsListID, multiPartyAuthReviewsListID)
	return newEnvironmentWith(t, targets, options, config.Connection{Tools: tools})
}

func TestAccessTargetsAreCheckedBeforeSecretAndIO(t *testing.T) {
	for id := range accessCalls {
		t.Run(id, func(t *testing.T) {
			e := newAccessEnvironment(t, []string{"customers/" + clientID}, nil)
			_, err := e.invokeConfirmed(id, call(id, otherID))
			if !isInvalidRequest(err) || strings.Contains(err.Error(), otherID) {
				t.Fatalf("err = %v, want invalid-request without the ID", err)
			}
			e.untouched(t)
		})
		t.Run(id+" malformed customer_id", func(t *testing.T) {
			e := newAccessEnvironment(t, nil, nil)
			for _, bad := range []string{"123-456-7890", "123456789", "", "1234567890 OR 1=1"} {
				if _, err := e.invokeConfirmed(id, call(id, bad)); err == nil {
					t.Errorf("customer_id %q accepted", bad)
				}
			}
			e.untouched(t)
		})
	}
}

func TestAccessChangesNeedTheToolsListAndTheConfirmation(t *testing.T) {
	for _, id := range []string{customerUserAccessInvitationsRemoveID, multiPartyAuthReviewsResolveID} {
		t.Run(id+" without the tools list", func(t *testing.T) {
			e := newEnvironmentWith(t, nil, nil, config.Connection{Effects: []config.Effect{config.EffectRead,
				config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}})
			if _, err := e.invokeConfirmed(id, call(id, clientID)); err == nil {
				t.Fatal("a connection with all effects but no tools list ran the tool")
			}
			e.untouched(t)
		})
		t.Run(id+" without confirmation", func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			_, err := e.invoke(id, call(id, clientID))
			var needed *application.ConfirmationRequiredError
			if !errors.As(err, &needed) || !strings.Contains(needed.Summary, clientID) {
				t.Fatalf("err = %v, want a confirmation that names the account", err)
			}
			e.untouched(t)
		})
		t.Run(id+" over the read effect", func(t *testing.T) {
			e := newEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, call(id, clientID)); err == nil {
				t.Fatal("a read connection ran the tool")
			}
			e.untouched(t)
		})
	}
}

// create of an invitation is not reachable: there is no tool for it and no argument that names an address or a
// role, nor an operation, nor validate_only, since the services have none of them for these calls.
func TestInvitationsCannotBeCreatedAndReviewsNeverApproved(t *testing.T) {
	foreign := []string{`"email_address":"` + address("new") + `"`, `"access_role":"ADMIN"`, `"create":{}`, `"operation":{}`,
		`"operations":[]`, `"validate_only":true`, `"partial_failure":true`, `"login_customer_id":"` + loginID + `"`,
		`"response_content_type":"MUTABLE_RESOURCE"`}
	for _, extra := range foreign {
		for id, arguments := range map[string]string{
			customerUserAccessInvitationsRemoveID: `{"customer_id":"` + clientID + `","invitation_id":"` + invitationID + `",`,
			multiPartyAuthReviewsResolveID: `{"customer_id":"` + clientID + `","multi_party_auth_review_id":"` + reviewID +
				`","new_status":"REJECTED",`} {
			e := newMutationEnvironment(t, nil, nil)
			if _, err := e.invokeConfirmed(id, arguments+extra+`}`); err == nil {
				t.Errorf("%s accepted %s", id, extra)
			}
			e.untouched(t)
		}
	}
	e := newMutationEnvironment(t, nil, nil)
	for _, status := range []string{"APPROVED", "approved", "PENDING", "EXPIRED", "UNSPECIFIED", "UNKNOWN", ""} {
		_, err := e.invokeConfirmed(multiPartyAuthReviewsResolveID, `{"customer_id":"`+clientID+
			`","multi_party_auth_review_id":"`+reviewID+`","new_status":"`+status+`"}`)
		if err == nil {
			t.Errorf("new_status %q accepted", status)
		}
	}
	e.untouched(t)
	// The check in the handler holds as well when the schema is bypassed.
	_, err := handleReviewsResolve(context.Background(), e.resolved(), e.secrets(), e.red,
		json.RawMessage(`{"customer_id":"`+clientID+`","multi_party_auth_review_id":"`+reviewID+`","new_status":"APPROVED"}`))
	if !isInvalidRequest(err) {
		t.Errorf("handler err = %v, want invalid-request", err)
	}
	e.untouched(t)
}

func TestAccessIDsAreCheckedBeforeSecretAndIO(t *testing.T) {
	bad := []string{"0", "01", "-1", "abc", "1 OR 1=1", "99999999999999999999", "9223372036854775808", "",
		"customers/" + clientID + "/customerUserAccessInvitations/5", "../" + clientID}
	for _, id := range bad {
		e := newMutationEnvironment(t, nil, nil)
		if _, err := e.invokeConfirmed(customerUserAccessInvitationsRemoveID, `{"customer_id":"`+clientID+`","invitation_id":"`+id+`"}`); err == nil {
			t.Errorf("invitation_id %q accepted", id)
		}
		if _, err := e.invokeConfirmed(multiPartyAuthReviewsResolveID, `{"customer_id":"`+clientID+
			`","multi_party_auth_review_id":"`+id+`","new_status":"REVOKED"}`); err == nil {
			t.Errorf("multi_party_auth_review_id %q accepted", id)
		}
		e.untouched(t)
	}
}

func TestInvitationRemoveSendsOneRemoveOfTheNamedInvitation(t *testing.T) {
	e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+invitationsPath(clientID)] = okReply(`{"result":{"resourceName":"` +
		invitationNameOf(clientID, invitationID) + `"}}`)
	out, err := e.invokeConfirmed(customerUserAccessInvitationsRemoveID, call(customerUserAccessInvitationsRemoveID, clientID))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	if got["customer_id"] != clientID || got["resource_name"] != invitationNameOf(clientID, invitationID) ||
		got["multi_party_auth_review"] != nil {
		t.Errorf("out = %s", out)
	}
	posts := e.posts()
	if len(posts) != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("requests = %+v", e.fake.api())
	}
	p := posts[0]
	if p.url != "https://googleads.googleapis.com"+invitationsPath(clientID) || p.login != loginID ||
		p.auth != "Bearer synthetic-access-1" || p.developerToken != "" {
		t.Errorf("request = %+v", p)
	}
	if !reflect.DeepEqual(bodyOf(t, p), map[string]any{"operation": map[string]any{"remove": invitationNameOf(clientID, invitationID)}}) {
		t.Errorf("body = %s", p.body)
	}
}

func TestInvitationRemoveMayWaitForApproval(t *testing.T) {
	e := newMutationEnvironment(t, nil, nil)
	e.fake.routes["POST "+invitationsPath(clientID)] = okReply(`{"result":{"multiPartyAuthReview":"` +
		reviewNameOf(clientID, reviewID) + `"}}`)
	out, err := e.invokeConfirmed(customerUserAccessInvitationsRemoveID, call(customerUserAccessInvitationsRemoveID, clientID))
	got := decode(t, out)
	if err != nil || got["multi_party_auth_review"] != reviewNameOf(clientID, reviewID) || got["resource_name"] != nil {
		t.Fatalf("out = %s, err = %v", out, err)
	}
}

func TestReviewResolveSendsOneOperationWithTheGivenStatus(t *testing.T) {
	for _, status := range []string{"REJECTED", "REVOKED"} {
		t.Run(status, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
			e.fake.routes["POST "+reviewResolvePath(clientID)] = okReply(`{"resultOrError":[{"result":{` +
				`"multiPartyAuthReview":"` + reviewNameOf(clientID, reviewID) + `"}}]}`)
			out, err := e.invokeConfirmed(multiPartyAuthReviewsResolveID, `{"customer_id":"`+clientID+
				`","multi_party_auth_review_id":"`+reviewID+`","new_status":"`+status+`"}`)
			if err != nil {
				t.Fatal(err)
			}
			got := decode(t, out)
			if got["customer_id"] != clientID || got["new_status"] != status ||
				got["multi_party_auth_review"] != reviewNameOf(clientID, reviewID) {
				t.Errorf("out = %s", out)
			}
			posts := e.posts()
			if len(posts) != 1 || len(e.fake.api()) != 1 || posts[0].login != loginID {
				t.Fatalf("requests = %+v", e.fake.api())
			}
			want := map[string]any{"operations": []any{map[string]any{"multiPartyAuthReview": reviewNameOf(clientID, reviewID),
				"newStatus": status}}}
			if !reflect.DeepEqual(bodyOf(t, posts[0]), want) || strings.Contains(posts[0].body, "APPROVED") {
				t.Errorf("body = %s", posts[0].body)
			}
		})
	}
}

// A resource name of another account never passes, and an answer that names an invitation or an access after
// a rejection is no rejection: in both cases the change may have been made.
func TestAccessAnswersAreBoundToTheCheckedAccountAndTheRequest(t *testing.T) {
	foreign := reviewNameOf(otherID, reviewID)
	cases := map[string]struct{ id, path, body string }{
		"remove another invitation": {customerUserAccessInvitationsRemoveID, invitationsPath(clientID),
			`{"result":{"resourceName":"` + invitationNameOf(clientID, "999") + `"}}`},
		"remove another account": {customerUserAccessInvitationsRemoveID, invitationsPath(clientID),
			`{"result":{"resourceName":"` + invitationNameOf(otherID, invitationID) + `"}}`},
		"remove foreign review": {customerUserAccessInvitationsRemoveID, invitationsPath(clientID),
			`{"result":{"multiPartyAuthReview":"` + foreign + `"}}`},
		"remove without a result": {customerUserAccessInvitationsRemoveID, invitationsPath(clientID), `{}`},
		"remove empty result":     {customerUserAccessInvitationsRemoveID, invitationsPath(clientID), `{"result":{}}`},
		"resolve another account": {multiPartyAuthReviewsResolveID, reviewResolvePath(clientID),
			`{"resultOrError":[{"result":{"multiPartyAuthReview":"` + foreign + `"}}]}`},
		"resolve another review": {multiPartyAuthReviewsResolveID, reviewResolvePath(clientID),
			`{"resultOrError":[{"result":{"multiPartyAuthReview":"` + reviewNameOf(clientID, "999") + `"}}]}`},
		"resolve without a result": {multiPartyAuthReviewsResolveID, reviewResolvePath(clientID), `{}`},
		"resolve two results": {multiPartyAuthReviewsResolveID, reviewResolvePath(clientID),
			`{"resultOrError":[{"result":{"multiPartyAuthReview":"` + reviewNameOf(clientID, reviewID) + `"}},` +
				`{"result":{"multiPartyAuthReview":"` + reviewNameOf(clientID, reviewID) + `"}}]}`},
		"resolve empty entry": {multiPartyAuthReviewsResolveID, reviewResolvePath(clientID), `{"resultOrError":[{}]}`},
		"resolve created an invitation": {multiPartyAuthReviewsResolveID, reviewResolvePath(clientID),
			`{"resultOrError":[{"result":{"multiPartyAuthReview":"` + reviewNameOf(clientID, reviewID) +
				`","customerUserAccessInvitation":"` + invitationNameOf(clientID, invitationID) + `"}}]}`},
		"resolve created an access": {multiPartyAuthReviewsResolveID, reviewResolvePath(clientID),
			`{"resultOrError":[{"result":{"multiPartyAuthReview":"` + reviewNameOf(clientID, reviewID) +
				`","customerUserAccess":"customers/` + queryNumber(clientID) + `/customerUserAccesses/5"}}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+c.path] = okReply(c.body)
			_, err := e.invokeConfirmed(c.id, call(c.id, clientID))
			if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") ||
				len(e.posts()) != 1 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
		})
	}
}

func TestReviewResolveMapsAFailedOperation(t *testing.T) {
	failure := func(category, code string) string {
		return fmt.Sprintf(`{"resultOrError":[{"partialFailureError":{"code":3,"message":"%s","details":[{"@type":`+
			`"type.googleapis.com/google.ads.googleads.v26.errors.GoogleAdsFailure","errors":[{"errorCode":{"%s":"%s"},`+
			`"message":"%s","trigger":{"stringValue":"%s"}}],"requestId":"synthetic-request-2"}]}}]}`,
			detailCanary, category, code, detailCanary, triggerCanary)
	}
	cases := []struct {
		name, body string
		class      provider.Class
		message    string
	}{
		{"documented code", failure("authorizationError", "USER_PERMISSION_DENIED"), provider.ClassPermission, optionLoginCustomerID},
		{"quota", failure("quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED"), provider.ClassRateLimited, "too many requests"},
		{"review error", failure("multiPartyAuthReviewError", "REVIEW_ALREADY_RESOLVED"), provider.ClassProviderError,
			"rejected the change (multiPartyAuthReviewError REVIEW_ALREADY_RESOLVED), nothing was changed"},
		{"no ads failure", `{"resultOrError":[{"partialFailureError":{"code":3,"message":"` + detailCanary + `"}}]}`,
			provider.ClassProviderError, "nothing was changed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newMutationEnvironment(t, nil, nil)
			e.fake.routes["POST "+reviewResolvePath(clientID)] = okReply(c.body)
			_, err := e.invokeConfirmed(multiPartyAuthReviewsResolveID, call(multiPartyAuthReviewsResolveID, clientID))
			if classOf(err) != c.class || !strings.Contains(err.Error(), c.message) ||
				strings.Contains(err.Error(), "outcome of the change is unknown") {
				t.Fatalf("err = %v, want %s with %q", err, c.class, c.message)
			}
			for _, leaked := range []string{detailCanary, triggerCanary} {
				if strings.Contains(err.Error(), leaked) {
					t.Errorf("error carries %q", leaked)
				}
			}
			if len(e.posts()) != 1 {
				t.Errorf("posts = %d, want one and no repeat", len(e.posts()))
			}
		})
	}
}

func TestAccessChangeErrorsAreMappedAndNeverRepeated(t *testing.T) {
	cases := []struct {
		name    string
		reply   reply
		class   provider.Class
		message string
		unknown bool
	}{
		{"permission", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"USER_PERMISSION_DENIED", "")}, provider.ClassPermission, optionLoginCustomerID, false},
		{"explorer access", reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED", "authorizationError",
			"CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION", "")}, provider.ClassPermission, "Explorer, Basic or Standard", false},
		{"access invitation error", reply{status: 400, body: adsFailure(400, "INVALID_ARGUMENT", "accessInvitationError",
			"INVALID_INVITATION_STATUS", "")}, provider.ClassProviderError,
			"rejected the change (accessInvitationError INVALID_INVITATION_STATUS), nothing was changed", false},
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
		{"unreadable json", okReply(`{"result":`), provider.ClassInvalidResponse, "outcome of the change is unknown", true},
	}
	for _, id := range []string{customerUserAccessInvitationsRemoveID, multiPartyAuthReviewsResolveID} {
		path := invitationsPath(clientID)
		if id == multiPartyAuthReviewsResolveID {
			path = reviewResolvePath(clientID)
		}
		for _, c := range cases {
			t.Run(id+" "+c.name, func(t *testing.T) {
				e := newMutationEnvironment(t, nil, nil)
				e.fake.routes["POST "+path] = c.reply
				_, err := e.invokeConfirmed(id, call(id, clientID))
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
	t.Run("a rate limit holds the next request", func(t *testing.T) {
		e := newMutationEnvironment(t, nil, nil)
		e.fake.routes["POST "+invitationsPath(clientID)] = reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED",
			"quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"30s"}`)}
		args := call(customerUserAccessInvitationsRemoveID, clientID)
		_, _ = e.invokeConfirmed(customerUserAccessInvitationsRemoveID, args)
		_, _ = e.invokeConfirmed(customerUserAccessInvitationsRemoveID, args)
		if len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] < 29_000_000_000 || len(e.posts()) != 2 {
			t.Errorf("sleeps = %v, posts = %d", e.sleeps, len(e.posts()))
		}
	})
}

// Invitations and reviews are access control: on a connection with targets every one of the four tools needs
// a manager target that names a manager account.
func TestAccessToolsNeedAManagerTargetOnManagerAccounts(t *testing.T) {
	changed := map[string]string{
		customerUserAccessInvitationsRemoveID: `{"result":{"resourceName":"` + invitationNameOf(managerID, invitationID) + `"}}`,
		multiPartyAuthReviewsResolveID: `{"resultOrError":[{"result":{"multiPartyAuthReview":"` +
			reviewNameOf(managerID, reviewID) + `"}}]}`,
		customerUserAccessInvitationsListID: `{"results":[]}`,
		multiPartyAuthReviewsListID:         `{"results":[]}`,
	}
	paths := map[string]string{customerUserAccessInvitationsRemoveID: invitationsPath(managerID),
		multiPartyAuthReviewsResolveID: reviewResolvePath(managerID)}
	managerAnswer := func(manager bool) func(s seen) *reply {
		return func(s seen) *reply {
			if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
				return &reply{status: 200, body: fmt.Sprintf(`{"results":[{"customer":{"id":"%s","manager":%t}}]}`, managerID, manager)}
			}
			if strings.Contains(s.query, "FROM customer_user_access_invitation") || strings.Contains(s.query, "FROM multi_party_auth_review") {
				return &reply{status: 200, body: changed[customerUserAccessInvitationsListID]}
			}
			return nil
		}
	}
	for id := range accessCalls {
		t.Run(id+" a customer target that names a manager is refused", func(t *testing.T) {
			e := newAccessEnvironment(t, []string{"customers/" + managerID}, nil)
			e.fake.answer = managerAnswer(true)
			if p, ok := paths[id]; ok {
				e.fake.routes["POST "+p] = okReply(changed[id])
			}
			_, err := e.invokeConfirmed(id, call(id, managerID))
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "manager target") || len(e.posts()) != 0 {
				t.Fatalf("err = %v, posts = %d", err, len(e.posts()))
			}
			for _, s := range e.fake.api() {
				if strings.Contains(s.query, "FROM customer_user_access_invitation") || strings.Contains(s.query, "FROM multi_party_auth_review") {
					t.Errorf("the tool read the manager's data: %+v", s)
				}
			}
		})
		t.Run(id+" a manager target that names it passes without the check", func(t *testing.T) {
			e := newAccessEnvironment(t, []string{"managers/" + managerID}, nil)
			e.fake.answer = managerAnswer(true)
			if p, ok := paths[id]; ok {
				e.fake.routes["POST "+p] = okReply(changed[id])
			}
			if _, err := e.invokeConfirmed(id, call(id, managerID)); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 1 {
				t.Errorf("requests = %+v, want the call alone", got)
			}
		})
		t.Run(id+" a client account passes after one check", func(t *testing.T) {
			e := newAccessEnvironment(t, []string{"customers/" + clientID}, nil)
			e.fake.answer = func(s seen) *reply {
				if strings.HasPrefix(s.query, "SELECT customer.id, customer.manager") {
					return &reply{status: 200, body: `{"results":[{"customer":{"id":"` + clientID + `"}}]}`}
				}
				if strings.Contains(s.query, "FROM ") {
					return &reply{status: 200, body: `{"results":[]}`}
				}
				return nil
			}
			switch id {
			case customerUserAccessInvitationsRemoveID:
				e.fake.routes["POST "+invitationsPath(clientID)] = okReply(`{"result":{"resourceName":"` +
					invitationNameOf(clientID, invitationID) + `"}}`)
			case multiPartyAuthReviewsResolveID:
				e.fake.routes["POST "+reviewResolvePath(clientID)] = okReply(`{"resultOrError":[{"result":{"multiPartyAuthReview":"` +
					reviewNameOf(clientID, reviewID) + `"}}]}`)
			}
			if _, err := e.invokeConfirmed(id, call(id, clientID)); err != nil {
				t.Fatal(err)
			}
			if got := e.fake.api(); len(got) != 2 || !strings.Contains(got[0].query, "customer.manager") {
				t.Errorf("requests = %+v", got)
			}
		})
	}
	t.Run("a connection without targets makes no check", func(t *testing.T) {
		e := newAccessEnvironment(t, nil, nil)
		e.fake.routes["POST "+invitationsPath(managerID)] = okReply(changed[customerUserAccessInvitationsRemoveID])
		if _, err := e.invokeConfirmed(customerUserAccessInvitationsRemoveID, call(customerUserAccessInvitationsRemoveID, managerID)); err != nil {
			t.Fatal(err)
		}
		if len(e.fake.api()) != 1 {
			t.Errorf("requests = %+v", e.fake.api())
		}
	})
}

func TestInvitationsListMapsAndPages(t *testing.T) {
	e := newAccessEnvironment(t, nil, map[string]string{optionLoginCustomerID: loginID})
	e.fake.routes["POST "+searchPath(clientID)] = rows(
		invitationItem(clientID, "11", address("anna"), "PENDING"),
		invitationItem(clientID, "12", address("ben"), "EXPIRED"),
		invitationItem(clientID, "13", address("cleo"), "DECLINED"))
	out, err := e.invoke(customerUserAccessInvitationsListID, `{"customer_id":"`+clientID+`","page_size":2,`+
		`"invitation_statuses":["PENDING","EXPIRED"],"access_roles":["STANDARD"],"invitation_ids":["11","12"],`+
		`"email_address":"`+address("anna")+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	list := got["invitations"].([]any)
	first := list[0].(map[string]any)
	if len(list) != 2 || got["has_more"] != true || got["next_cursor"] == "" || first["invitation_id"] != "11" ||
		first["email_address"] != address("anna") || first["access_role"] != "STANDARD" ||
		first["invitation_status"] != "PENDING" || first["creation_date_time"] != "2026-11-02 09:15:00" ||
		first["resource_name"] != invitationNameOf(clientID, "11") {
		t.Errorf("out = %s", out)
	}
	api := e.fake.api()
	want := "SELECT customer_user_access_invitation.resource_name, customer_user_access_invitation.invitation_id, " +
		"customer_user_access_invitation.access_role, customer_user_access_invitation.email_address, " +
		"customer_user_access_invitation.creation_date_time, customer_user_access_invitation.invitation_status " +
		"FROM customer_user_access_invitation WHERE customer_user_access_invitation.invitation_status IN (PENDING, EXPIRED) " +
		"AND customer_user_access_invitation.access_role IN (STANDARD) AND customer_user_access_invitation.invitation_id IN (11, 12) " +
		"AND customer_user_access_invitation.email_address = '" + address("anna") + "' " +
		"ORDER BY customer_user_access_invitation.invitation_id LIMIT 3"
	if len(api) != 1 || api[0].query != want || api[0].login != loginID {
		t.Errorf("request = %+v\nwant %s", api, want)
	}
	// The next page continues after the last ID of this one and is bound to the arguments.
	cursor := got["next_cursor"].(string)
	e.fake.routes["POST "+searchPath(clientID)] = rows(invitationItem(clientID, "13", address("cleo"), "DECLINED"))
	out, err = e.invoke(customerUserAccessInvitationsListID, `{"customer_id":"`+clientID+`","page_size":2,`+
		`"invitation_statuses":["PENDING","EXPIRED"],"access_roles":["STANDARD"],"invitation_ids":["11","12"],`+
		`"email_address":"`+address("anna")+`","cursor":"`+cursor+`"}`)
	if err != nil || decode(t, out)["has_more"] != false || !strings.Contains(e.fake.api()[1].query,
		"customer_user_access_invitation.invitation_id > 12 ORDER BY") {
		t.Fatalf("out = %s, err = %v, query = %s", out, err, e.fake.api()[1].query)
	}
	if _, err := e.invoke(customerUserAccessInvitationsListID, `{"customer_id":"`+clientID+`","cursor":"`+cursor+`"}`); err == nil {
		t.Error("a cursor of other arguments was accepted")
	}
}

func TestListFiltersAreCheckedBeforeSecretAndIO(t *testing.T) {
	bad := map[string][]string{
		customerUserAccessInvitationsListID: {`"invitation_statuses":["APPROVED"]`, `"invitation_statuses":["PENDING","PENDING"]`,
			`"access_roles":["OWNER"]`, `"invitation_ids":["1 OR 1=1"]`, `"invitation_ids":["0"]`,
			`"email_address":"a'b` + address("x") + `"`, `"email_address":"a\"b"`, `"email_address":"a\\b` + address("x") + `"`,
			`"email_address":"a b` + address("x") + `"`, `"email_address":"x"`, `"page_size":0`, `"page_size":1001`,
			`"cursor":"not-a-cursor"`, `"statuses":["PENDING"]`},
		multiPartyAuthReviewsListID: {`"review_statuses":["DENIED"]`, `"operation_types":["APPROVE"]`,
			`"target_resources":["CUSTOMER"]`, `"review_ids":["abc"]`, `"request_user_email":"a'b` + address("x") + `"`,
			`"request_user_email":"a\\b` + address("x") + `"`, `"page_size":-1`, `"cursor":"x"`},
	}
	for id, list := range bad {
		for _, filter := range list {
			e := newAccessEnvironment(t, nil, nil)
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`",`+filter+`}`); err == nil {
				t.Errorf("%s accepted %s", id, filter)
			}
			e.untouched(t)
		}
	}
	// The handlers refuse as well when the schema is bypassed.
	e := newAccessEnvironment(t, nil, nil)
	for _, filter := range []string{`"email_address":"a'b` + address("x") + `"`, `"invitation_statuses":["PENDING'"]`} {
		_, err := handleInvitationsList(context.Background(), e.resolved(), e.secrets(), e.red,
			json.RawMessage(`{"customer_id":"`+clientID+`",`+filter+`}`))
		if !isInvalidRequest(err) {
			t.Errorf("handler accepted %s: %v", filter, err)
		}
	}
	e.untouched(t)
}

func TestListsRefuseWrongOrForeignRows(t *testing.T) {
	foreign := invitationItem(otherID, "21", address("zed"), "PENDING")
	rowsOf := map[string][]string{
		customerUserAccessInvitationsListID: {foreign,
			strings.Replace(invitationItem(clientID, "21", address("zed"), "PENDING"), `"invitationId":"21"`, `"invitationId":"22"`, 1),
			`{"customerUserAccessInvitation":{"resourceName":"` + invitationNameOf(clientID, "21") + `"}}`, `{}`,
			`{"customerUserAccessInvitation":{"invitationId":"-4"}}`},
		multiPartyAuthReviewsListID: {
			strings.ReplaceAll(reviewItem(clientID, "31"), "customers/"+queryNumber(clientID)+"/customerUserAccesses/77",
				"customers/"+queryNumber(otherID)+"/customerUserAccesses/77"),
			strings.ReplaceAll(reviewItem(clientID, "31"), `"pendingMultiPartyAuthReview":"`+reviewNameOf(clientID, "31"),
				`"pendingMultiPartyAuthReview":"`+reviewNameOf(otherID, "31")),
			strings.Replace(reviewItem(clientID, "31"), `"multiPartyAuthReviewId":"31"`, `"multiPartyAuthReviewId":"32"`, 1),
			reviewItem(otherID, "31"), `{}`},
	}
	for id, bad := range rowsOf {
		for i, row := range bad {
			e := newAccessEnvironment(t, nil, nil)
			e.fake.routes["POST "+searchPath(clientID)] = rows(row)
			if _, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("%s row %d: err = %v, want invalid-provider-response", id, i, err)
			}
		}
	}
	t.Run("rows out of the requested order", func(t *testing.T) {
		e := newAccessEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = rows(invitationItem(clientID, "12", address("a"), "PENDING"),
			invitationItem(clientID, "11", address("b"), "PENDING"))
		if _, err := e.invoke(customerUserAccessInvitationsListID, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("err = %v", err)
		}
	})
}

func TestReviewsListMapsWithoutTheSecurityValuesOfPeople(t *testing.T) {
	e := newAccessEnvironment(t, nil, nil)
	invitationReview := fmt.Sprintf(`{"multiPartyAuthReview":{"resourceName":"%s","multiPartyAuthReviewId":"32",`+
		`"reviewStatus":"APPROVED","approvalDateTime":"2026-11-04 12:00:00","operationType":"CREATE",`+
		`"targetResource":"CUSTOMER_USER_ACCESS_INVITATION","customerUserAccessInvitationReview":{`+
		`"newCustomerUserAccessInvitation":{"accessRole":"READ_ONLY","emailAddress":"%s","invitationId":"41"}}}}`,
		reviewNameOf(clientID, "32"), address("guest"))
	e.fake.routes["POST "+searchPath(clientID)] = rows(reviewItem(clientID, "31"), invitationReview)
	out, err := e.invoke(multiPartyAuthReviewsListID, `{"customer_id":"`+clientID+`","review_statuses":["PENDING","APPROVED"],`+
		`"operation_types":["UPDATE","CREATE"],"target_resources":["CUSTOMER_USER_ACCESS"],"review_ids":["31","32"],`+
		`"request_user_email":"`+address("requester")+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"passkey", "Passkey", "true,\"inviter"} {
		if strings.Contains(out, leaked) {
			t.Errorf("out carries %q: %s", leaked, out)
		}
	}
	got := decode(t, out)
	list := got["reviews"].([]any)
	review := list[0].(map[string]any)
	access := review["customer_user_access_review"].(map[string]any)["new_customer_user_access"].(map[string]any)
	if len(list) != 2 || review["multi_party_auth_review_id"] != "31" || review["review_status"] != "PENDING" ||
		review["operation_type"] != "UPDATE" || review["target_resource"] != "CUSTOMER_USER_ACCESS" ||
		review["request_user_email"] != address("requester") || strings.ContainsAny(review["justification"].(string), "\a") ||
		!strings.HasPrefix(review["justification"].(string), "needs") ||
		access["user_id"] != "77" || access["email_address"] != address("member") || access["access_role"] != "ADMIN" ||
		access["inviter_user_email_address"] != address("boss") || access["access_creation_date_time"] != "2026-01-01 08:00:00" ||
		access["pending_multi_party_auth_review"] != reviewNameOf(clientID, "31") ||
		access["resource_name"] != "customers/"+queryNumber(clientID)+"/customerUserAccesses/77" ||
		review["customer_user_access_review"].(map[string]any)["old_customer_user_access"] != "customers/"+queryNumber(clientID)+"/customerUserAccesses/77" {
		t.Errorf("review = %v", review)
	}
	second := list[1].(map[string]any)
	invitation := second["customer_user_access_invitation_review"].(map[string]any)["new_customer_user_access_invitation"].(map[string]any)
	if second["review_status"] != "APPROVED" || second["approval_date_time"] != "2026-11-04 12:00:00" ||
		invitation["email_address"] != address("guest") || invitation["access_role"] != "READ_ONLY" ||
		invitation["invitation_id"] != "41" || invitation["resource_name"] != invitationNameOf(clientID, "41") {
		t.Errorf("second = %v", second)
	}
	api := e.fake.api()
	if len(api) != 1 || !strings.Contains(api[0].query, "FROM multi_party_auth_review WHERE "+
		"multi_party_auth_review.review_status IN (PENDING, APPROVED) AND multi_party_auth_review.operation_type IN (UPDATE, CREATE) "+
		"AND multi_party_auth_review.target_resource IN (CUSTOMER_USER_ACCESS) AND "+
		"multi_party_auth_review.multi_party_auth_review_id IN (31, 32) AND multi_party_auth_review.request_user_email = '"+
		address("requester")+"' ORDER BY multi_party_auth_review.multi_party_auth_review_id LIMIT 101") {
		t.Errorf("request = %+v", api)
	}
	if strings.Contains(api[0].query, "passkey") {
		t.Errorf("query selects the passkey setting: %s", api[0].query)
	}
}

func TestReviewsListPages(t *testing.T) {
	e := newAccessEnvironment(t, nil, nil)
	e.fake.routes["POST "+searchPath(clientID)] = rows(reviewItem(clientID, "31"), reviewItem(clientID, "32"))
	out, err := e.invoke(multiPartyAuthReviewsListID, `{"customer_id":"`+clientID+`","page_size":1}`)
	got := decode(t, out)
	if err != nil || got["has_more"] != true || len(got["reviews"].([]any)) != 1 || got["next_cursor"] == nil {
		t.Fatalf("out = %s, err = %v", out, err)
	}
	e.fake.routes["POST "+searchPath(clientID)] = rows(reviewItem(clientID, "32"))
	if _, err := e.invoke(multiPartyAuthReviewsListID, `{"customer_id":"`+clientID+`","page_size":1,"cursor":"`+
		got["next_cursor"].(string)+`"}`); err != nil || !strings.Contains(e.fake.api()[1].query,
		"multi_party_auth_review_id > 31 ORDER BY") {
		t.Fatalf("err = %v", err)
	}
}

func TestListErrorsAreMapped(t *testing.T) {
	for id := range map[string]string{customerUserAccessInvitationsListID: "", multiPartyAuthReviewsListID: ""} {
		e := newAccessEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reply{status: 403, body: adsFailure(403, "PERMISSION_DENIED",
			"authorizationError", "USER_PERMISSION_DENIED", "")}
		_, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`)
		if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), detailCanary) {
			t.Errorf("%s: err = %v", id, err)
		}
		e = newAccessEnvironment(t, nil, nil)
		e.fake.routes["POST "+searchPath(clientID)] = reply{status: 429, body: adsFailure(429, "RESOURCE_EXHAUSTED",
			"quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED", `{"retryDelay":"5s"}`)}
		if _, err := e.invoke(id, `{"customer_id":"`+clientID+`"}`); classOf(err) != provider.ClassRateLimited {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}

func TestAccessSummariesNameTheEffect(t *testing.T) {
	remove := invitationRemoveSummary(json.RawMessage(call(customerUserAccessInvitationsRemoveID, clientID)))
	resolve := reviewResolveSummary(json.RawMessage(call(multiPartyAuthReviewsResolveID, clientID)))
	revoke := reviewResolveSummary(json.RawMessage(`{"customer_id":"` + clientID + `","multi_party_auth_review_id":"` + reviewID +
		`","new_status":"REVOKED"}`))
	for _, c := range []struct{ text, word string }{{remove, invitationID}, {remove, clientID}, {remove, "Revoke"},
		{resolve, "Reject"}, {resolve, reviewID}, {resolve, clientID}, {revoke, "Revoke"}} {
		if !strings.Contains(c.text, c.word) {
			t.Errorf("summary %q lacks %q", c.text, c.word)
		}
	}
}
