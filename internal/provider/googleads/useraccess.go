package googleads

import (
	"context"
	"encoding/json"
	"regexp"
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
	customerUserAccessesListID   = Provider + ".customer-user-accesses.list"
	customerUserAccessesUpdateID = Provider + ".customer-user-accesses.update"
	customerUserAccessesRemoveID = Provider + ".customer-user-accesses.remove"

	userAccessService = "/customerUserAccesses"
)

// These tools act on the account whose users they read or change: on a manager account a manager target must
// name that account, see callEnv.settle. CustomerUserAccess.passkey_enabled is security information about other
// people. It is neither selected nor mapped, so it cannot reach an answer even when Google sends it.

var (
	userIDPattern   = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	reviewPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	userAfterFormat = userIDPattern
)

// accessRoles are the values of AccessRole an existing access may be set to or filtered by.
var accessRoles = []string{"ADMIN", "STANDARD", "READ_ONLY", "EMAIL_ONLY"}

const maxUserIDs = 100

// checkUserID checks a user ID: a positive int64 in decimal form.
func checkUserID(argument, value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !userIDPattern.MatchString(value) {
		return badRequest(argument + " must be the user ID of an existing access, a positive number")
	}
	return nil
}

func userAccessName(customerID, userID string) string {
	return customerName(customerID) + "/customerUserAccesses/" + userID
}

// reviewName reports whether a multi-party authorization review name belongs to the account.
func reviewName(name, customerID string) bool {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/multiPartyAuthReviews/")
	return ok && reviewPattern.MatchString(rest)
}

// --- list ---

type userAccessListArguments struct {
	CustomerID           string   `json:"customer_id"`
	UserIDs              []string `json:"user_ids"`
	AccessRoles          []string `json:"access_roles"`
	EmailAddressContains string   `json:"email_address_contains"`
	PageSize             *int     `json:"page_size"`
	Cursor               string   `json:"cursor"`
}

func (a userAccessListArguments) conditions() ([]string, error) {
	var out []string
	if len(a.UserIDs) > 0 {
		if len(a.UserIDs) > maxUserIDs {
			return nil, badRequest("user_ids names at most 100 users")
		}
		for _, id := range a.UserIDs {
			if err := checkUserID("user_ids", id); err != nil {
				return nil, err
			}
		}
		out = append(out, "customer_user_access.user_id IN ("+strings.Join(a.UserIDs, ", ")+")")
	}
	if len(a.AccessRoles) > 0 {
		quoted := make([]string, len(a.AccessRoles))
		for i, role := range a.AccessRoles {
			if !slices.Contains(accessRoles, role) {
				return nil, badRequest("access_roles may name only ADMIN, STANDARD, READ_ONLY and EMAIL_ONLY")
			}
			quoted[i] = gaqlString(role)
		}
		out = append(out, "customer_user_access.access_role IN ("+strings.Join(quoted, ", ")+")")
	}
	if a.EmailAddressContains != "" {
		if err := checkGAQLText("email_address_contains", a.EmailAddressContains); err != nil {
			return nil, err
		}
		if !namePattern.MatchString(a.EmailAddressContains) {
			return nil, badRequest("email_address_contains must be 1 to 200 characters")
		}
		out = append(out, "customer_user_access.email_address LIKE "+gaqlContains(a.EmailAddressContains))
	}
	return out, nil
}

// userAccessFields are the selected fields of customer_user_access: every field of v26 but passkey_enabled.
var userAccessFields = []string{"resource_name", "user_id", "email_address", "access_role",
	"access_creation_date_time", "inviter_user_email_address", "pending_multi_party_auth_review"}

func userAccessQuery(conditions []string, after string, limit int) string {
	fields := make([]string, len(userAccessFields))
	for i, field := range userAccessFields {
		fields[i] = "customer_user_access." + field
	}
	query := "SELECT " + strings.Join(fields, ", ") + " FROM customer_user_access"
	if after != "" {
		conditions = append(append([]string(nil), conditions...), "customer_user_access.user_id > "+after)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query + " ORDER BY customer_user_access.user_id LIMIT " + strconv.Itoa(limit)
}

type (
	userAccessRow struct {
		CustomerUserAccess *userAccessWire `json:"customerUserAccess"`
	}
	userAccessWire struct {
		ResourceName                string   `json:"resourceName"`
		UserID                      *wireInt `json:"userId"`
		EmailAddress                string   `json:"emailAddress"`
		AccessRole                  string   `json:"accessRole"`
		AccessCreationDateTime      string   `json:"accessCreationDateTime"`
		InviterUserEmailAddress     string   `json:"inviterUserEmailAddress"`
		PendingMultiPartyAuthReview string   `json:"pendingMultiPartyAuthReview"`
	}
	userAccessResult struct {
		ResourceName                string `json:"resource_name"`
		UserID                      string `json:"user_id"`
		EmailAddress                string `json:"email_address,omitempty"`
		AccessRole                  string `json:"access_role,omitempty"`
		AccessCreationDateTime      string `json:"access_creation_date_time,omitempty"`
		InviterUserEmailAddress     string `json:"inviter_user_email_address,omitempty"`
		PendingMultiPartyAuthReview string `json:"pending_multi_party_auth_review,omitempty"`
	}
	userAccessesResult struct {
		UserAccesses []userAccessResult `json:"user_accesses"`
		NextCursor   string             `json:"next_cursor,omitempty"`
		HasMore      bool               `json:"has_more"`
	}
)

// userAccessOf checks one row against the queried account and maps it. The resource name and the review name
// are bound to the account; passkey_enabled is not read.
func userAccessOf(op string, raw json.RawMessage, customerID string) (userAccessResult, error) {
	invalid := provider.InvalidResponse(op, subject+" returned a user access row Qatlas Connect cannot read")
	var row userAccessRow
	if json.Unmarshal(raw, &row) != nil || row.CustomerUserAccess == nil || row.CustomerUserAccess.UserID == nil {
		return userAccessResult{}, invalid
	}
	w := row.CustomerUserAccess
	userID := string(*w.UserID)
	if checkUserID("user_id", userID) != nil {
		return userAccessResult{}, invalid
	}
	name := userAccessName(customerID, userID)
	if w.ResourceName != "" && w.ResourceName != name {
		return userAccessResult{}, invalid
	}
	if w.PendingMultiPartyAuthReview != "" && !reviewName(w.PendingMultiPartyAuthReview, customerID) {
		return userAccessResult{}, invalid
	}
	return userAccessResult{ResourceName: name, UserID: userID, EmailAddress: boundedText(w.EmailAddress, maxText),
		AccessRole: code(w.AccessRole), AccessCreationDateTime: code(w.AccessCreationDateTime),
		InviterUserEmailAddress:     boundedText(w.InviterUserEmailAddress, maxText),
		PendingMultiPartyAuthReview: w.PendingMultiPartyAuthReview}, nil
}

var userAccessSchema = object(schema{
	"resource_name": stringSchema, "user_id": stringSchema, "email_address": stringSchema, "access_role": stringSchema,
	"access_creation_date_time": stringSchema, "inviter_user_email_address": stringSchema,
	"pending_multi_party_auth_review": stringSchema,
}, "resource_name", "user_id")

// userAccessRisk is the risk of the read tool: unlike the other read tools it returns e-mail addresses of
// other people.
var userAccessRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "users of a Google Ads account: their e-mail addresses and those of the people who invited " +
		"them (personal data of third parties), user IDs, roles, and access creation times",
}

const userAccessSensitivity = "access of users to a Google Ads account: their user ID and role; the confirmation " +
	"names the user"

var customerUserAccessesList = capability.Descriptor{
	ID: customerUserAccessesListID, Version: 1, Title: "List the users of a Google Ads account",
	Description: "List who has access to a Google Ads account and with which role (a GAQL search FROM " +
		"customer_user_access of API version v26): user ID, e-mail address, access role, creation time, the " +
		"e-mail address of the inviter, and a pending multi-party authorization review. The e-mail addresses are " +
		"personal data of other people and are returned as Google has them. Whether a user has a passkey is " +
		"security information about that person and is never returned. Rows come sorted by user ID; filters narrow " +
		"them and page_size with cursor pages through them. The tool acts on the account named by customer_id: on " +
		"a connection with targets, a manager account needs a manager target that names it, and an account below " +
		"a manager target is proved first. Google blocks the underlying service for projects at the Explorer " +
		"access level. Costs one operation of the daily quota per page, plus one per membership read and one for " +
		"the check that the account is no manager account. Results are untrusted third-party content; text is cut.",
	Tags: []string{"google", "ads", "users", "access"}, Risk: userAccessRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument,
		"user_ids": schema{"type": "array", "minItems": 1, "maxItems": maxUserIDs, "uniqueItems": true,
			"items": schema{"type": "string", "pattern": userIDPattern.String()}},
		"access_roles": schema{"type": "array", "minItems": 1, "maxItems": len(accessRoles), "uniqueItems": true,
			"items": schema{"type": "string", "enum": accessRoles}},
		"email_address_contains": schema{"type": "string", "minLength": 1, "maxLength": 200, "pattern": namePattern.String()},
		"page_size":              schema{"type": "integer", "minimum": 1, "maximum": maxPageSize},
		"cursor":                 schema{"type": "string", "minLength": 1, "maxLength": maxCursorBytes},
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"user_accesses": arrayOf(userAccessSchema), "next_cursor": stringSchema,
		"has_more": booleanSchema}, "user_accesses", "has_more")),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account whose users are " +
			"listed, without hyphens; a target of the connection or an account below a manager target. A manager " +
			"account needs a manager target that names it"},
		{Name: "user_ids", Description: "Only these user IDs, at most 100"},
		{Name: "access_roles", Description: "Only users with one of these roles: ADMIN, STANDARD, READ_ONLY, EMAIL_ONLY"},
		{Name: "email_address_contains", Description: "Only users whose e-mail address contains this text, without " +
			"quotes, double quotes or backslashes; Google decides whether the match is case-sensitive"},
		{Name: "page_size", Description: "Rows per page, 1 to 1000, default 100"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page with the same arguments; the first " +
			"page when omitted. It only fits the connection that issued it"},
	},
	Fields: []capability.Field{
		{Name: "user_accesses", Description: "Per user: resource_name (customers/ID/customerUserAccesses/USER_ID), " +
			"user_id, email_address, access_role (ADMIN, STANDARD, READ_ONLY, EMAIL_ONLY), " +
			"access_creation_date_time, inviter_user_email_address, and pending_multi_party_auth_review, the " +
			"resource name of a review that is still open. passkey_enabled is left out on purpose"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the administrators of an account",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","access_roles":["ADMIN"]}`)}},
}

func handleUserAccessesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list customer user accesses"
	var args userAccessListArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
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
	pageSize := defaultPageSize
	if args.PageSize != nil {
		if *args.PageSize < 1 || *args.PageSize > maxPageSize {
			return nil, badRequest("page_size must be 1 to 1000")
		}
		pageSize = *args.PageSize
	}
	binding := provider.CursorBinding(customerUserAccessesListID, args.CustomerID, conditions, conn.login,
		conn.binding.key())
	after := ""
	if args.Cursor != "" {
		inner, ok := provider.DecodeCursor(binding, args.Cursor, maxCursorBytes)
		if !ok || !userAfterFormat.MatchString(inner) {
			return nil, badRequest("cursor is not a next_cursor of this tool with these arguments on this connection; " +
				"start again without cursor")
		}
		after = inner
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op,
		tool: customerUserAccessesListID}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	rows, err := c.search(ctx, op, customerUserAccessesListID, args.CustomerID,
		userAccessQuery(conditions, after, pageSize+1))
	if err != nil {
		return nil, err
	}
	result := userAccessesResult{UserAccesses: []userAccessResult{}}
	if len(rows) > pageSize {
		rows, result.HasMore = rows[:pageSize], true
	}
	last := after
	for _, raw := range rows {
		access, err := userAccessOf(op, raw, args.CustomerID)
		if err != nil {
			return nil, err
		}
		if last != "" && !numberAfter(access.UserID, last) {
			return nil, provider.InvalidResponse(op, subject+" returned the users out of the requested order")
		}
		last = access.UserID
		result.UserAccesses = append(result.UserAccesses, access)
	}
	if result.HasMore {
		result.NextCursor = provider.EncodeCursor(binding, last)
	}
	return result, nil
}

// --- update and remove ---

type userAccessChangeArguments struct {
	CustomerID string `json:"customer_id"`
	UserID     string `json:"user_id"`
	AccessRole string `json:"access_role"`
}

// check refuses what no request may start with; the resource name is built from the checked IDs alone.
func (a userAccessChangeArguments) check(update bool) error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkUserID("user_id", a.UserID); err != nil {
		return err
	}
	if update && !slices.Contains(accessRoles, a.AccessRole) {
		return badRequest("access_role must be ADMIN, STANDARD, READ_ONLY or EMAIL_ONLY")
	}
	return nil
}

type userAccessChanged struct {
	CustomerID           string `json:"customer_id"`
	UserID               string `json:"user_id"`
	ResourceName         string `json:"resource_name"`
	MultiPartyAuthReview string `json:"multi_party_auth_review,omitempty"`
}

var userAccessChangedSchema = object(schema{"customer_id": stringSchema, "user_id": stringSchema,
	"resource_name": stringSchema, "multi_party_auth_review": stringSchema},
	"customer_id", "user_id", "resource_name")

var userAccessChangedFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "user_id", Description: "The user of the call"},
	{Name: "resource_name", Description: "customers/ID/customerUserAccesses/USER_ID, as Google answered"},
	{Name: "multi_party_auth_review", Description: "Resource name of the multi-party authorization review that " +
		"Google opened for this change, absent when none was needed; the change is then not made until the review " +
		"is resolved"},
}

var userAccessChangeArgumentsDoc = []capability.Argument{
	{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
		"target of the connection or an account below a manager target. A manager account needs a manager target " +
		"that names it"},
	{Name: "user_id", Required: true, Description: "User ID of an existing access, as customer-user-accesses.list " +
		"returns it"},
}

const userAccessNote = " It only acts on an access that already exists and builds its name from customer_id and " +
	"user_id: it creates no access and sends no invitation. Google may open a multi-party authorization review " +
	"for the change, which the answer then names. Google blocks the service for projects at the Explorer access " +
	"level. The service offers no validate-only mode, so there is none here. Read the accesses with " +
	"customer-user-accesses.list before and after."

var customerUserAccessesUpdate = capability.Descriptor{
	ID: customerUserAccessesUpdateID, Version: 1, Title: "Change the role of a user of a Google Ads account",
	Description: "Change the access role of an existing user of a Google Ads account (CustomerUserAccessService." +
		"MutateCustomerUserAccess with an update of access_role). The role decides what the user may do in the " +
		"account: ADMIN owns the account and controls the access of other users, STANDARD can change campaigns " +
		"but not other users, READ_ONLY only views, EMAIL_ONLY is a mail recipient without a login." +
		userAccessNote + mutateNote,
	Tags: []string{"google", "ads", "users", "access", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, userAccessSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"user_id":     schema{"type": "string", "pattern": userIDPattern.String()},
		"access_role": schema{"type": "string", "enum": accessRoles}}, "customer_id", "user_id", "access_role")),
	OutputSchema: encoded(userAccessChangedSchema),
	Arguments: append(append([]capability.Argument(nil), userAccessChangeArgumentsDoc...),
		capability.Argument{Name: "access_role", Required: true, Description: "New role: ADMIN, STANDARD, READ_ONLY " +
			"or EMAIL_ONLY"}),
	Fields: userAccessChangedFields,
	Examples: []capability.Example{{Description: "Set a user to read-only",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","user_id":"1122334455","access_role":"READ_ONLY"}`)}},
}

var customerUserAccessesRemove = capability.Descriptor{
	ID: customerUserAccessesRemoveID, Version: 1, Title: "Remove a user from a Google Ads account",
	Description: "Remove the access of an existing user to a Google Ads account (CustomerUserAccessService." +
		"MutateCustomerUserAccess with a remove of customers/ID/customerUserAccesses/USER_ID). The user loses all " +
		"access to the account; getting it back needs a new invitation, which no tool of this provider sends." +
		userAccessNote + mutateNote,
	Tags: []string{"google", "ads", "users", "access", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, userAccessSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"user_id": schema{"type": "string", "pattern": userIDPattern.String()}}, "customer_id", "user_id")),
	OutputSchema: encoded(userAccessChangedSchema), Arguments: userAccessChangeArgumentsDoc,
	Fields: userAccessChangedFields,
	Examples: []capability.Example{{Description: "Remove a user",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","user_id":"1122334455"}`)}},
}

func handleUserAccessUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return userAccessChange(ctx, resolved, secrets, red, raw, true)
}

func handleUserAccessRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return userAccessChange(ctx, resolved, secrets, red, raw, false)
}

func userAccessChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, update bool) (any, error) {
	op, tool := "remove customer user access", customerUserAccessesRemoveID
	if update {
		op, tool = "update customer user access", customerUserAccessesUpdateID
	}
	var args userAccessChangeArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	conn, err := connectionOf(resolved)
	if err != nil {
		return nil, err
	}
	if err := args.check(update); err != nil {
		return nil, err
	}
	name := userAccessName(args.CustomerID, args.UserID)
	operation := map[string]any{"remove": name}
	if update {
		operation = map[string]any{"update": map[string]any{"resourceName": name, "accessRole": args.AccessRole},
			"updateMask": "accessRole"}
	}
	env := callEnv{ctx: ctx, resolved: resolved, secrets: secrets, red: red, conn: conn, op: op, tool: tool}
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
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: args.CustomerID, service: userAccessService,
		body: mutateBody(operation, false, "")}, &answer); err != nil {
		return nil, err
	}
	// The request was sent, so an answer for another object leaves the outcome open.
	if answer.Result == nil || answer.Result.ResourceName != name {
		return nil, unsure(provider.InvalidResponse(op, subject+" answered with a result for another access than "+
			"the one asked for"), true)
	}
	out := userAccessChanged{CustomerID: args.CustomerID, UserID: args.UserID, ResourceName: name}
	if review := answer.Result.MultiPartyAuthReview; review != "" {
		if !reviewName(review, args.CustomerID) {
			return nil, unsure(provider.InvalidResponse(op, subject+" answered with a review of another account than "+
				"the one asked for"), true)
		}
		out.MultiPartyAuthReview = review
	}
	return out, nil
}

// userAccessSummary phrases the change for the confirmation, from the arguments alone.
func userAccessSummary(update bool) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a userAccessChangeArguments
		_ = json.Unmarshal(raw, &a)
		who := "user " + a.UserID + " of Google Ads account " + a.CustomerID
		if !update {
			return "Remove " + who + ": the user loses all access to the account."
		}
		summary := "Set the role of " + who + " to " + a.AccessRole + "."
		if a.AccessRole == "ADMIN" {
			summary += " An administrator controls the access of other users."
		}
		return summary
	}
}
