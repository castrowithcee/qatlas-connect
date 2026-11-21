package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	multiPartyAuthReviewsListID    = Provider + ".multi-party-auth-reviews.list"
	multiPartyAuthReviewsResolveID = Provider + ".multi-party-auth-reviews.resolve"

	// The review service is one resource without a collection in its path: ...:resolve below
	// customers/ID/multiPartyAuthReview, while the reviews themselves are customers/ID/multiPartyAuthReviews/N.
	reviewService    = "/multiPartyAuthReview"
	reviewCollection = "multiPartyAuthReviews"
	userCollection   = "customerUserAccesses"
)

// The values of the enums in v26 that a filter may name, and the two statuses a resolution may carry.
// APPROVED is no status of a resolution here: approving creates an invitation or an access.
var (
	reviewStatuses  = []string{"PENDING", "APPROVED", "REJECTED", "REVOKED", "EXPIRED"}
	operationTypes  = []string{"CREATE", "UPDATE", "REMOVE"}
	targetResources = []string{"CUSTOMER_USER_ACCESS", "CUSTOMER_USER_ACCESS_INVITATION"}
	resolveStatuses = []string{"REJECTED", "REVOKED"}
)

func reviewResourceName(customerID, id string) string {
	return customerName(customerID) + "/" + reviewCollection + "/" + id
}

type reviewsArguments struct {
	CustomerID       string   `json:"customer_id"`
	ReviewStatuses   []string `json:"review_statuses"`
	OperationTypes   []string `json:"operation_types"`
	TargetResources  []string `json:"target_resources"`
	ReviewIDs        []string `json:"review_ids"`
	RequestUserEmail string   `json:"request_user_email"`
	PageSize         *int     `json:"page_size"`
	Cursor           string   `json:"cursor"`
}

func (a reviewsArguments) conditions() ([]string, error) {
	var out []string
	for _, c := range []struct {
		argument, field string
		values, allowed []string
	}{
		{"review_statuses", "multi_party_auth_review.review_status", a.ReviewStatuses, reviewStatuses},
		{"operation_types", "multi_party_auth_review.operation_type", a.OperationTypes, operationTypes},
		{"target_resources", "multi_party_auth_review.target_resource", a.TargetResources, targetResources},
	} {
		condition, err := enumList(c.argument, c.field, c.values, c.allowed)
		if err != nil {
			return nil, err
		}
		if condition != "" {
			out = append(out, condition)
		}
	}
	ids, err := idList("review_ids", "multi_party_auth_review.multi_party_auth_review_id", a.ReviewIDs)
	if err != nil {
		return nil, err
	}
	if ids != "" {
		out = append(out, ids)
	}
	if a.RequestUserEmail != "" {
		if err := checkEmailFilter("request_user_email", a.RequestUserEmail); err != nil {
			return nil, err
		}
		out = append(out, "multi_party_auth_review.request_user_email = "+gaqlString(a.RequestUserEmail))
	}
	return out, nil
}

// reviewFields are the fields of the multi_party_auth_review resource in v26, every one of them. The two
// messages of the review details come whole; the mapping below keeps only the fields it names.
var reviewFields = []string{"resource_name", "multi_party_auth_review_id", "creation_date_time", "review_status",
	"approval_date_time", "justification", "request_user_email", "operation_type", "target_resource",
	"customer_user_access_review.old_customer_user_access", "customer_user_access_review.new_customer_user_access",
	"customer_user_access_invitation_review.new_customer_user_access_invitation"}

func reviewsQuery(conditions []string, after string, limit int) string {
	fields := make([]string, len(reviewFields))
	for i, field := range reviewFields {
		fields[i] = "multi_party_auth_review." + field
	}
	query := "SELECT " + strings.Join(fields, ", ") + " FROM multi_party_auth_review"
	if after != "" {
		conditions = append(append([]string(nil), conditions...), "multi_party_auth_review.multi_party_auth_review_id > "+after)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query + " ORDER BY multi_party_auth_review.multi_party_auth_review_id LIMIT " + strconv.Itoa(limit)
}

type (
	// reviewAccessWire holds the fields of a CustomerUserAccess that a review shows. passkey_enabled, the
	// security setting of another person, has no member here and is therefore never decoded.
	reviewAccessWire struct {
		ResourceName                string   `json:"resourceName"`
		UserID                      *wireInt `json:"userId"`
		EmailAddress                string   `json:"emailAddress"`
		AccessRole                  string   `json:"accessRole"`
		AccessCreationDateTime      string   `json:"accessCreationDateTime"`
		InviterUserEmailAddress     string   `json:"inviterUserEmailAddress"`
		PendingMultiPartyAuthReview string   `json:"pendingMultiPartyAuthReview"`
	}
	reviewWire struct {
		ResourceName             string   `json:"resourceName"`
		ReviewID                 *wireInt `json:"multiPartyAuthReviewId"`
		CreationDateTime         string   `json:"creationDateTime"`
		ReviewStatus             string   `json:"reviewStatus"`
		ApprovalDateTime         string   `json:"approvalDateTime"`
		Justification            string   `json:"justification"`
		RequestUserEmail         string   `json:"requestUserEmail"`
		OperationType            string   `json:"operationType"`
		TargetResource           string   `json:"targetResource"`
		CustomerUserAccessReview *struct {
			OldCustomerUserAccess string            `json:"oldCustomerUserAccess"`
			NewCustomerUserAccess *reviewAccessWire `json:"newCustomerUserAccess"`
		} `json:"customerUserAccessReview"`
		CustomerUserAccessInvitationReview *struct {
			NewCustomerUserAccessInvitation *invitationWire `json:"newCustomerUserAccessInvitation"`
		} `json:"customerUserAccessInvitationReview"`
	}
	reviewRow struct {
		Review *reviewWire `json:"multiPartyAuthReview"`
	}

	reviewAccessResult struct {
		ResourceName                string `json:"resource_name,omitempty"`
		UserID                      string `json:"user_id,omitempty"`
		EmailAddress                string `json:"email_address,omitempty"`
		AccessRole                  string `json:"access_role,omitempty"`
		AccessCreationDateTime      string `json:"access_creation_date_time,omitempty"`
		InviterUserEmailAddress     string `json:"inviter_user_email_address,omitempty"`
		PendingMultiPartyAuthReview string `json:"pending_multi_party_auth_review,omitempty"`
	}
	userAccessReviewResult struct {
		OldCustomerUserAccess string              `json:"old_customer_user_access,omitempty"`
		NewCustomerUserAccess *reviewAccessResult `json:"new_customer_user_access,omitempty"`
	}
	invitationReviewResult struct {
		NewCustomerUserAccessInvitation *invitationResult `json:"new_customer_user_access_invitation,omitempty"`
	}
	reviewResult struct {
		ResourceName                       string                  `json:"resource_name"`
		ReviewID                           string                  `json:"multi_party_auth_review_id"`
		CreationDateTime                   string                  `json:"creation_date_time,omitempty"`
		ReviewStatus                       string                  `json:"review_status,omitempty"`
		ApprovalDateTime                   string                  `json:"approval_date_time,omitempty"`
		Justification                      string                  `json:"justification,omitempty"`
		RequestUserEmail                   string                  `json:"request_user_email,omitempty"`
		OperationType                      string                  `json:"operation_type,omitempty"`
		TargetResource                     string                  `json:"target_resource,omitempty"`
		CustomerUserAccessReview           *userAccessReviewResult `json:"customer_user_access_review,omitempty"`
		CustomerUserAccessInvitationReview *invitationReviewResult `json:"customer_user_access_invitation_review,omitempty"`
	}
	reviewsResult struct {
		Reviews    []reviewResult `json:"reviews"`
		NextCursor string         `json:"next_cursor,omitempty"`
		HasMore    bool           `json:"has_more"`
	}
)

// reviewAccessOf maps the access a review shows. Its names must belong to the account of the call.
func reviewAccessOf(w *reviewAccessWire, customerID string) (*reviewAccessResult, bool) {
	out := &reviewAccessResult{EmailAddress: boundedText(w.EmailAddress, maxText), AccessRole: code(w.AccessRole),
		AccessCreationDateTime:  code(w.AccessCreationDateTime),
		InviterUserEmailAddress: boundedText(w.InviterUserEmailAddress, maxText)}
	if w.UserID != nil {
		id, ok := idOf(w.UserID)
		if !ok {
			return nil, false
		}
		out.UserID = id
		if w.ResourceName != "" && w.ResourceName != customerName(customerID)+"/"+userCollection+"/"+id {
			return nil, false
		}
	}
	if w.ResourceName != "" && !ownedName(w.ResourceName, customerID, userCollection) {
		return nil, false
	}
	out.ResourceName = w.ResourceName
	if w.PendingMultiPartyAuthReview != "" && !ownedName(w.PendingMultiPartyAuthReview, customerID, reviewCollection) {
		return nil, false
	}
	out.PendingMultiPartyAuthReview = w.PendingMultiPartyAuthReview
	return out, true
}

// reviewOf maps one review. Every resource name in it must belong to the account of the call and the review
// to its own ID; a row that does not fit is an invalid answer.
func reviewOf(w *reviewWire, customerID string) (reviewResult, bool) {
	id, ok := idOf(w.ReviewID)
	if !ok || (w.ResourceName != "" && w.ResourceName != reviewResourceName(customerID, id)) {
		return reviewResult{}, false
	}
	out := reviewResult{ResourceName: reviewResourceName(customerID, id), ReviewID: id,
		CreationDateTime: code(w.CreationDateTime), ReviewStatus: code(w.ReviewStatus),
		ApprovalDateTime: code(w.ApprovalDateTime), Justification: boundedText(w.Justification, maxLongText),
		RequestUserEmail: boundedText(w.RequestUserEmail, maxText), OperationType: code(w.OperationType),
		TargetResource: code(w.TargetResource)}
	if r := w.CustomerUserAccessReview; r != nil {
		if r.OldCustomerUserAccess != "" && !ownedName(r.OldCustomerUserAccess, customerID, userCollection) {
			return reviewResult{}, false
		}
		out.CustomerUserAccessReview = &userAccessReviewResult{OldCustomerUserAccess: r.OldCustomerUserAccess}
		if r.NewCustomerUserAccess != nil {
			access, ok := reviewAccessOf(r.NewCustomerUserAccess, customerID)
			if !ok {
				return reviewResult{}, false
			}
			out.CustomerUserAccessReview.NewCustomerUserAccess = access
		}
	}
	if r := w.CustomerUserAccessInvitationReview; r != nil {
		out.CustomerUserAccessInvitationReview = &invitationReviewResult{}
		if r.NewCustomerUserAccessInvitation != nil {
			invitation, ok := invitationOf(r.NewCustomerUserAccessInvitation, customerID)
			if !ok {
				return reviewResult{}, false
			}
			out.CustomerUserAccessInvitationReview.NewCustomerUserAccessInvitation = &invitation
		}
	}
	return out, true
}

var (
	reviewAccessSchema = object(schema{"resource_name": stringSchema, "user_id": stringSchema, "email_address": stringSchema,
		"access_role": stringSchema, "access_creation_date_time": stringSchema,
		"inviter_user_email_address": stringSchema, "pending_multi_party_auth_review": stringSchema})
	reviewSchema = object(schema{"resource_name": stringSchema, "multi_party_auth_review_id": stringSchema,
		"creation_date_time": stringSchema, "review_status": stringSchema, "approval_date_time": stringSchema,
		"justification": stringSchema, "request_user_email": stringSchema, "operation_type": stringSchema,
		"target_resource": stringSchema,
		"customer_user_access_review": object(schema{"old_customer_user_access": stringSchema,
			"new_customer_user_access": reviewAccessSchema}),
		"customer_user_access_invitation_review": object(schema{
			"new_customer_user_access_invitation": invitationSchema})},
		"resource_name", "multi_party_auth_review_id")
)

var multiPartyAuthReviewsList = capability.Descriptor{
	ID: multiPartyAuthReviewsListID, Version: 1, Title: "List the multi-party approval requests of a Google Ads account",
	Description: "List the requests for a second administrator's approval of a sensitive change to who may use a " +
		"Google Ads account (a GAQL search FROM multi_party_auth_review of API version v26): inviting a person, " +
		"changing the role of a person, and removing a person. Per request the ID, status (PENDING, APPROVED, " +
		"REJECTED, REVOKED, EXPIRED; a request expires 20 days after it was made), times, the e-mail address of the " +
		"requester and their justification, the kind of operation and target, and what is under review: the " +
		"invitation that would be sent, or the access that would change, as far as it needs review. The security " +
		"setting of a person's sign-in (passkey) is never returned. Rows come sorted by ID; filters narrow them and " +
		"page_size with cursor pages through them. The e-mail addresses and the justification are personal data of " +
		"other people. customer_id must be one of the targets or lie below a manager target; on a connection with " +
		"targets, a manager account needs a manager target that names it. Costs one operation of the daily quota per " +
		"page, plus one for a membership read below a manager target and one for the check that the account is no " +
		"manager account. Google refuses the service at the Explorer access level. Results are untrusted " +
		"third-party content; text is cut.",
	Tags: []string{"google", "ads", "access", "approvals"}, Risk: accessReadRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"review_statuses": schema{"type": "array", "minItems": 1, "maxItems": len(reviewStatuses), "uniqueItems": true,
			"items": schema{"type": "string", "enum": reviewStatuses}},
		"operation_types": schema{"type": "array", "minItems": 1, "maxItems": len(operationTypes), "uniqueItems": true,
			"items": schema{"type": "string", "enum": operationTypes}},
		"target_resources": schema{"type": "array", "minItems": 1, "maxItems": len(targetResources), "uniqueItems": true,
			"items": schema{"type": "string", "enum": targetResources}},
		"review_ids": schema{"type": "array", "minItems": 1, "maxItems": maxClientIDs, "uniqueItems": true,
			"items": schema{"type": "string", "pattern": idPatternText}},
		"request_user_email": schema{"type": "string", "minLength": 3, "maxLength": 254, "pattern": emailFilterPatternText},
		"page_size":          pageProperties["page_size"], "cursor": pageProperties["cursor"],
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"reviews": arrayOf(reviewSchema), "next_cursor": stringSchema,
		"has_more": booleanSchema}, "reviews", "has_more")),
	Arguments: append([]capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "review_statuses", Description: "Only requests with one of these statuses: PENDING, APPROVED, REJECTED, REVOKED, EXPIRED"},
		{Name: "operation_types", Description: "Only requests for one of these operations: CREATE, UPDATE, REMOVE"},
		{Name: "target_resources", Description: "Only requests about these targets: CUSTOMER_USER_ACCESS, " +
			"CUSTOMER_USER_ACCESS_INVITATION"},
		{Name: "review_ids", Description: "Only these request IDs, at most 100"},
		{Name: "request_user_email", Description: "Only requests made by exactly this address, without spaces, quotes " +
			"or backslashes; Google decides whether the match is case-sensitive"},
	}, pageArguments...),
	Fields: []capability.Field{
		{Name: "reviews", Description: "Per request: resource_name (customers/ID/multiPartyAuthReviews/REVIEW_ID), " +
			"multi_party_auth_review_id, creation_date_time, review_status, approval_date_time, justification, " +
			"request_user_email, operation_type, target_resource, and the details under review: " +
			"customer_user_access_review with old_customer_user_access (the access that changes) and " +
			"new_customer_user_access (resource_name, user_id, email_address, access_role, access_creation_date_time, " +
			"inviter_user_email_address, pending_multi_party_auth_review; only the fields that need review are set, " +
			"and never the passkey setting), or customer_user_access_invitation_review with " +
			"new_customer_user_access_invitation (resource_name, invitation_id, access_role, email_address, " +
			"creation_date_time, invitation_status)"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the requests that wait for approval",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","review_statuses":["PENDING"]}`)}},
}

func handleReviewsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list multi-party auth reviews"
	var args reviewsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, multiPartyAuthReviewsListID)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	conditions, err := args.conditions()
	if err != nil {
		return nil, err
	}
	items, next, more, err := listAccess(env, listSpec{customerID: args.CustomerID, tool: env.tool, op: op,
		conditions: conditions, pageSize: args.PageSize, cursor: args.Cursor, query: reviewsQuery},
		func(raw json.RawMessage) (reviewResult, string, error) {
			var row reviewRow
			if json.Unmarshal(raw, &row) != nil || row.Review == nil {
				return reviewResult{}, "", provider.InvalidResponse(op, subject+" returned a review row Qatlas Connect "+
					"cannot read")
			}
			item, ok := reviewOf(row.Review, args.CustomerID)
			if !ok {
				return reviewResult{}, "", provider.InvalidResponse(op, subject+" returned a review that is not one of "+
					"the account asked for")
			}
			return item, item.ReviewID, nil
		})
	if err != nil {
		return nil, err
	}
	return reviewsResult{Reviews: items, NextCursor: next, HasMore: more}, nil
}

type reviewResolveArguments struct {
	CustomerID string `json:"customer_id"`
	ReviewID   string `json:"multi_party_auth_review_id"`
	NewStatus  string `json:"new_status"`
}

type reviewResolved struct {
	CustomerID           string `json:"customer_id"`
	MultiPartyAuthReview string `json:"multi_party_auth_review"`
	NewStatus            string `json:"new_status"`
}

var multiPartyAuthReviewsResolve = capability.Descriptor{
	ID: multiPartyAuthReviewsResolveID, Version: 1, Title: "Reject or revoke a multi-party approval request of a Google Ads account",
	Description: "Turn down a pending multi-party approval request of a Google Ads account " +
		"(MultiPartyAuthReviewService.ResolveMultiPartyAuthReview with one operation): new_status REJECTED denies a " +
		"request that another administrator made, REVOKED withdraws a request the credential's own user made, so " +
		"that the sensitive change (an invitation, a role change, a removal of a person) does not happen. Only these " +
		"two statuses are offered: approving a request would send an invitation or change who may use the " +
		"account, and this tool never does that; no status APPROVED is accepted. Google lets only a user other than " +
		"the requester reject, and only the requester revoke. The service has no validate_only, so this tool has none " +
		"either: the call resolves. The credential needs ADMIN access to the account. On a connection with " +
		"targets, a manager account needs a manager target that names it." + mutateNote,
	Tags: []string{"google", "ads", "access", "approvals", "resolve"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent,
		"access control of a Google Ads account: turns down a pending request to invite, change or remove a person"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"multi_party_auth_review_id": schema{"type": "string", "pattern": idPatternText},
		"new_status":                 schema{"type": "string", "enum": resolveStatuses}},
		"customer_id", "multi_party_auth_review_id", "new_status")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "multi_party_auth_review": stringSchema,
		"new_status": stringSchema}, "customer_id", "multi_party_auth_review", "new_status")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "multi_party_auth_review_id", Required: true, Description: "ID of the request, as " +
			"multi_party_auth_review_id of multi-party-auth-reviews.list"},
		{Name: "new_status", Required: true, Description: "REJECTED (deny a request of another administrator) or " +
			"REVOKED (withdraw your own request); APPROVED is not accepted"},
	},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "multi_party_auth_review", Description: "customers/ID/multiPartyAuthReviews/REVIEW_ID of the resolved request, as Google answered"},
		{Name: "new_status", Description: "The status the request was resolved to: REJECTED or REVOKED"},
	},
	Examples: []capability.Example{{Description: "Reject a pending request",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","multi_party_auth_review_id":"5566778899","new_status":"REJECTED"}`)}},
}

func handleReviewsResolve(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "resolve multi-party auth review"
	var args reviewResolveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, multiPartyAuthReviewsResolveID)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkID("multi_party_auth_review_id", args.ReviewID); err != nil {
		return nil, err
	}
	if !slices.Contains(resolveStatuses, args.NewStatus) {
		return nil, badRequest("new_status must be REJECTED or REVOKED; a request is never approved by this tool")
	}
	name := reviewResourceName(args.CustomerID, args.ReviewID)
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		ResultOrError []struct {
			Result *struct {
				MultiPartyAuthReview         string `json:"multiPartyAuthReview"`
				CustomerUserAccessInvitation string `json:"customerUserAccessInvitation"`
				CustomerUserAccess           string `json:"customerUserAccess"`
			} `json:"result"`
			PartialFailureError json.RawMessage `json:"partialFailureError"`
		} `json:"resultOrError"`
	}
	operation := map[string]any{"multiPartyAuthReview": name, "newStatus": args.NewStatus}
	if err := c.mutate(ctx, mutation{op: op, tool: env.tool, customerID: args.CustomerID, service: reviewService,
		action: "resolve", body: map[string]any{"operations": []any{operation}}}, &answer); err != nil {
		return nil, err
	}
	if len(answer.ResultOrError) != 1 {
		return nil, unsureResult(op, "answered with another number of results than the one operation asked for")
	}
	entry := answer.ResultOrError[0]
	if len(entry.PartialFailureError) > 0 && string(entry.PartialFailureError) != "null" {
		return nil, resolveFailure(op, entry.PartialFailureError)
	}
	r := entry.Result
	switch {
	case r == nil:
		return nil, unsureResult(op, "answered without a result")
	case r.MultiPartyAuthReview != name:
		return nil, unsureResult(op, "answered with a result for another request than the one asked for")
	case r.CustomerUserAccessInvitation != "" || r.CustomerUserAccess != "":
		return nil, unsureResult(op, "answered with an invitation or an access that a rejection or revocation does not "+
			"produce")
	}
	return reviewResolved{CustomerID: args.CustomerID, MultiPartyAuthReview: r.MultiPartyAuthReview,
		NewStatus: args.NewStatus}, nil
}

// resolveFailure maps the google.rpc.Status of a failed operation. The request is atomic and has one
// operation, so a failure means that nothing was changed.
func resolveFailure(op string, status json.RawMessage) error {
	body, _ := json.Marshal(map[string]json.RawMessage{"error": status})
	failed, _ := adsError(op, 400, body, true)
	if failed.Class == provider.ClassInvalidResponse {
		failed.Class = provider.ClassProviderError
		failed.Message = subject + " refused to resolve the request; nothing was changed"
	}
	return failed
}

// reviewResolveSummary phrases the change for the confirmation, from the arguments alone.
func reviewResolveSummary(raw json.RawMessage) string {
	var a reviewResolveArguments
	_ = json.Unmarshal(raw, &a)
	verb := "Reject"
	if a.NewStatus == "REVOKED" {
		verb = "Revoke"
	}
	return verb + " multi-party approval request " + a.ReviewID + " of Google Ads account " + a.CustomerID +
		": the invitation, role change or removal under review does not happen."
}
