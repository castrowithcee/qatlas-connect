package googleads

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	customerUserAccessInvitationsListID   = Provider + ".customer-user-access-invitations.list"
	customerUserAccessInvitationsRemoveID = Provider + ".customer-user-access-invitations.remove"

	invitationService = "/customerUserAccessInvitations"
)

// invitationStatuses are the values of the status enum in v26 that a filter may name; UNSPECIFIED and UNKNOWN
// are no values to ask for.
var invitationStatuses = []string{"PENDING", "DECLINED", "EXPIRED"}

func invitationName(customerID, id string) string {
	return customerName(customerID) + invitationService + "/" + id
}

type invitationsArguments struct {
	CustomerID         string   `json:"customer_id"`
	InvitationStatuses []string `json:"invitation_statuses"`
	AccessRoles        []string `json:"access_roles"`
	InvitationIDs      []string `json:"invitation_ids"`
	EmailAddress       string   `json:"email_address"`
	PageSize           *int     `json:"page_size"`
	Cursor             string   `json:"cursor"`
}

func (a invitationsArguments) conditions() ([]string, error) {
	var out []string
	for _, c := range []struct {
		argument, field string
		values, allowed []string
	}{
		{"invitation_statuses", "customer_user_access_invitation.invitation_status", a.InvitationStatuses, invitationStatuses},
		{"access_roles", "customer_user_access_invitation.access_role", a.AccessRoles, accessRoles},
	} {
		condition, err := enumList(c.argument, c.field, c.values, c.allowed)
		if err != nil {
			return nil, err
		}
		if condition != "" {
			out = append(out, condition)
		}
	}
	ids, err := idList("invitation_ids", "customer_user_access_invitation.invitation_id", a.InvitationIDs)
	if err != nil {
		return nil, err
	}
	if ids != "" {
		out = append(out, ids)
	}
	if a.EmailAddress != "" {
		if err := checkEmailFilter("email_address", a.EmailAddress); err != nil {
			return nil, err
		}
		out = append(out, "customer_user_access_invitation.email_address = "+gaqlString(a.EmailAddress))
	}
	return out, nil
}

var invitationFields = []string{"resource_name", "invitation_id", "access_role", "email_address", "creation_date_time",
	"invitation_status"}

func invitationsQuery(conditions []string, after string, limit int) string {
	fields := make([]string, len(invitationFields))
	for i, field := range invitationFields {
		fields[i] = "customer_user_access_invitation." + field
	}
	query := "SELECT " + strings.Join(fields, ", ") + " FROM customer_user_access_invitation"
	if after != "" {
		conditions = append(append([]string(nil), conditions...), "customer_user_access_invitation.invitation_id > "+after)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query + " ORDER BY customer_user_access_invitation.invitation_id LIMIT " + strconv.Itoa(limit)
}

type (
	invitationWire struct {
		ResourceName     string   `json:"resourceName"`
		InvitationID     *wireInt `json:"invitationId"`
		AccessRole       string   `json:"accessRole"`
		EmailAddress     string   `json:"emailAddress"`
		CreationDateTime string   `json:"creationDateTime"`
		InvitationStatus string   `json:"invitationStatus"`
	}
	invitationRow struct {
		Invitation *invitationWire `json:"customerUserAccessInvitation"`
	}
	invitationResult struct {
		ResourceName     string `json:"resource_name"`
		InvitationID     string `json:"invitation_id"`
		AccessRole       string `json:"access_role,omitempty"`
		EmailAddress     string `json:"email_address,omitempty"`
		CreationDateTime string `json:"creation_date_time,omitempty"`
		InvitationStatus string `json:"invitation_status,omitempty"`
	}
	invitationsResult struct {
		Invitations []invitationResult `json:"invitations"`
		NextCursor  string             `json:"next_cursor,omitempty"`
		HasMore     bool               `json:"has_more"`
	}
)

// invitationOf maps an invitation of an answer. It must belong to the account of the call: its resource name
// is that of the account and its own ID.
func invitationOf(w *invitationWire, customerID string) (invitationResult, bool) {
	id, ok := idOf(w.InvitationID)
	if !ok || (w.ResourceName != "" && w.ResourceName != invitationName(customerID, id)) {
		return invitationResult{}, false
	}
	return invitationResult{ResourceName: invitationName(customerID, id), InvitationID: id,
		AccessRole: code(w.AccessRole), EmailAddress: boundedText(w.EmailAddress, maxText),
		CreationDateTime: code(w.CreationDateTime), InvitationStatus: code(w.InvitationStatus)}, true
}

var invitationSchema = object(schema{"resource_name": stringSchema, "invitation_id": stringSchema,
	"access_role": stringSchema, "email_address": stringSchema, "creation_date_time": stringSchema,
	"invitation_status": stringSchema}, "resource_name", "invitation_id")

const invitationIDArgumentText = "ID of the invitation, as invitation_id of customer-user-access-invitations.list"

var customerUserAccessInvitationsList = capability.Descriptor{
	ID: customerUserAccessInvitationsListID, Version: 1, Title: "List the user access invitations of a Google Ads account",
	Description: "List the invitations that were sent to people to get access to a Google Ads account (a GAQL " +
		"search FROM customer_user_access_invitation of API version v26): ID, access role, the e-mail address the " +
		"invitation was sent to, creation time and status (PENDING, DECLINED, EXPIRED; an invitation expires 20 days " +
		"after it was created). Rows come sorted by ID; filters narrow them and page_size with cursor pages through " +
		"them. An invitation that waits for a multi-party approval is not listed by Google; " +
		"multi-party-auth-reviews.list shows it. The e-mail addresses are personal data of other people. " +
		"customer_id must be one of the targets or lie below a manager target; on a connection with targets, a " +
		"manager account needs a manager target that names it. Costs one operation of the daily quota per page, " +
		"plus one for a membership read below a manager target and one for the check that the account is no " +
		"manager account. Google refuses the service at the Explorer access level. Results are untrusted " +
		"third-party content; text is cut.",
	Tags: []string{"google", "ads", "access", "invitations"}, Risk: accessReadRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"invitation_statuses": schema{"type": "array", "minItems": 1, "maxItems": len(invitationStatuses), "uniqueItems": true,
			"items": schema{"type": "string", "enum": invitationStatuses}},
		"access_roles": schema{"type": "array", "minItems": 1, "maxItems": len(accessRoles), "uniqueItems": true,
			"items": schema{"type": "string", "enum": accessRoles}},
		"invitation_ids": schema{"type": "array", "minItems": 1, "maxItems": maxClientIDs, "uniqueItems": true,
			"items": schema{"type": "string", "pattern": idPatternText}},
		"email_address": schema{"type": "string", "minLength": 3, "maxLength": 254, "pattern": emailFilterPatternText},
		"page_size":     pageProperties["page_size"], "cursor": pageProperties["cursor"],
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"invitations": arrayOf(invitationSchema), "next_cursor": stringSchema,
		"has_more": booleanSchema}, "invitations", "has_more")),
	Arguments: append([]capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "invitation_statuses", Description: "Only invitations with one of these statuses: PENDING, DECLINED, EXPIRED"},
		{Name: "access_roles", Description: "Only invitations for one of these roles: ADMIN, STANDARD, READ_ONLY, EMAIL_ONLY"},
		{Name: "invitation_ids", Description: "Only these invitation IDs, at most 100"},
		{Name: "email_address", Description: "Only invitations sent to exactly this address, without spaces, quotes or " +
			"backslashes; Google decides whether the match is case-sensitive"},
	}, pageArguments...),
	Fields: []capability.Field{
		{Name: "invitations", Description: "Per invitation: resource_name (customers/ID/customerUserAccessInvitations/" +
			"INVITATION_ID), invitation_id, access_role, email_address (the address the invitation was sent to, which " +
			"can differ from the address of the account that accepts it), creation_date_time (YYYY-MM-DD HH:MM:SS) " +
			"and invitation_status"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the pending invitations of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","invitation_statuses":["PENDING"]}`)}},
}

func handleInvitationsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list user access invitations"
	var args invitationsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, customerUserAccessInvitationsListID)
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
		conditions: conditions, pageSize: args.PageSize, cursor: args.Cursor, query: invitationsQuery},
		func(raw json.RawMessage) (invitationResult, string, error) {
			var row invitationRow
			if json.Unmarshal(raw, &row) != nil || row.Invitation == nil {
				return invitationResult{}, "", provider.InvalidResponse(op, subject+" returned an invitation row Qatlas "+
					"Connect cannot read")
			}
			item, ok := invitationOf(row.Invitation, args.CustomerID)
			if !ok {
				return invitationResult{}, "", provider.InvalidResponse(op, subject+" returned an invitation that is not "+
					"one of the account asked for")
			}
			return item, item.InvitationID, nil
		})
	if err != nil {
		return nil, err
	}
	return invitationsResult{Invitations: items, NextCursor: next, HasMore: more}, nil
}

type invitationRemoveArguments struct {
	CustomerID   string `json:"customer_id"`
	InvitationID string `json:"invitation_id"`
}

type invitationRemoved struct {
	CustomerID           string `json:"customer_id"`
	ResourceName         string `json:"resource_name,omitempty"`
	MultiPartyAuthReview string `json:"multi_party_auth_review,omitempty"`
}

var customerUserAccessInvitationsRemove = capability.Descriptor{
	ID: customerUserAccessInvitationsRemoveID, Version: 1, Title: "Revoke a user access invitation of a Google Ads account",
	Description: "Revoke a pending invitation to a Google Ads account " +
		"(CustomerUserAccessInvitationService.MutateCustomerUserAccessInvitation with a remove operation): the " +
		"person can no longer accept it. The invitation is only named by its ID; this tool never sends an " +
		"invitation, and no tool of Qatlas Connect does. The service has no validate_only, so this tool has none " +
		"either: the call revokes. Google may hold the revocation for a multi-party approval by a second " +
		"administrator; the answer then names the review in multi_party_auth_review and no resource_name. The " +
		"credential needs ADMIN access to the account. On a connection with targets, a manager account needs a " +
		"manager target that names it." + mutateNote,
	Tags: []string{"google", "ads", "access", "invitations", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent,
		"access control of a Google Ads account: revokes the invitation of a person"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"invitation_id": schema{"type": "string", "pattern": idPatternText}}, "customer_id", "invitation_id")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "resource_name": stringSchema,
		"multi_party_auth_review": stringSchema}, "customer_id")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "invitation_id", Required: true, Description: invitationIDArgumentText},
	},
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "resource_name", Description: "customers/ID/customerUserAccessInvitations/INVITATION_ID of the revoked " +
			"invitation, as Google answered; absent when the revocation waits for a multi-party approval"},
		{Name: "multi_party_auth_review", Description: "customers/ID/multiPartyAuthReviews/REVIEW_ID when Google holds " +
			"the revocation for approval by a second administrator"},
	},
	Examples: []capability.Example{{Description: "Revoke an invitation",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","invitation_id":"1122334455"}`)}},
}

func handleInvitationsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "revoke user access invitation"
	var args invitationRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, customerUserAccessInvitationsRemoveID)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkID("invitation_id", args.InvitationID); err != nil {
		return nil, err
	}
	name := invitationName(args.CustomerID, args.InvitationID)
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Result *struct {
			ResourceName         string `json:"resourceName"`
			MultiPartyAuthReview string `json:"multiPartyAuthReview"`
		} `json:"result"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: env.tool, customerID: args.CustomerID, service: invitationService,
		body: mutateBody(map[string]any{"remove": name}, false, "")}, &answer); err != nil {
		return nil, err
	}
	r := answer.Result
	switch {
	case r == nil || (r.ResourceName == "" && r.MultiPartyAuthReview == ""):
		return nil, unsureResult(op, "answered without a result")
	case r.ResourceName != "" && r.ResourceName != name:
		return nil, unsureResult(op, "answered with a result for another invitation than the one asked for")
	case r.MultiPartyAuthReview != "" && !ownedName(r.MultiPartyAuthReview, args.CustomerID, reviewCollection):
		return nil, unsureResult(op, "answered with a review of another account than the one asked for")
	}
	return invitationRemoved{CustomerID: args.CustomerID, ResourceName: r.ResourceName,
		MultiPartyAuthReview: r.MultiPartyAuthReview}, nil
}

// invitationRemoveSummary phrases the change for the confirmation, from the arguments alone.
func invitationRemoveSummary(raw json.RawMessage) string {
	var a invitationRemoveArguments
	_ = json.Unmarshal(raw, &a)
	return "Revoke invitation " + a.InvitationID + " to Google Ads account " + a.CustomerID +
		": the invited person can no longer accept it."
}
