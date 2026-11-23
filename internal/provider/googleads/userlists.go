package googleads

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	userListsCreateID = Provider + ".user-lists.create"
	userListsUpdateID = Provider + ".user-lists.update"
	userListsRemoveID = Provider + ".user-lists.remove"

	userListService = "/userLists"
	// maxUserListOperations bounds a call: a list can carry a large rule tree, and the confirmation has to name
	// every list. Google's own bound is the 10,000 operations of every Mutate request.
	userListMaxUserListOperations = 20
	// maxMembershipLifeSpan is the largest membership_life_span Google accepts, in days.
	userListMaxMembershipLifeSpan = 540
)

// These tools act on the account that owns the lists: on a manager account a manager target must name it, see
// callEnv.settle. Members are never uploaded or returned.

// userListArgs is one entry of user_lists: a list to create or a list to update.
type userListArgs struct {
	UserListID            string                 `json:"user_list_id"`
	Name                  *string                `json:"name"`
	Description           *string                `json:"description"`
	MembershipStatus      string                 `json:"membership_status"`
	IntegrationCode       *string                `json:"integration_code"`
	MembershipLifeSpan    *int64                 `json:"membership_life_span"`
	EligibleForSearch     *bool                  `json:"eligible_for_search"`
	AccountUserListStatus string                 `json:"account_user_list_status"`
	ClosingReason         string                 `json:"closing_reason"`
	BasicUserList         *userListBasicList     `json:"basic_user_list"`
	RuleBasedUserList     *userListRuleBasedList `json:"rule_based_user_list"`
	LogicalUserList       *userListLogicalList   `json:"logical_user_list"`
	CrmBasedUserList      *userListCrmList       `json:"crm_based_user_list"`
	LookalikeUserList     *userListLookalikeList `json:"lookalike_user_list"`
}

type userListsArguments struct {
	CustomerID   string         `json:"customer_id"`
	UserLists    []userListArgs `json:"user_lists"`
	ValidateOnly bool           `json:"validate_only"`
}

type userListRemoveArguments struct {
	CustomerID   string   `json:"customer_id"`
	UserListIDs  []string `json:"user_list_ids"`
	ValidateOnly bool     `json:"validate_only"`
}

// checkUserListID checks the ID of a user list: a positive int64 in decimal form.
func userListCheckID(argument, value string) error {
	if _, err := strconv.ParseInt(value, 10, 64); err != nil || !userListListIDPattern.MatchString(value) {
		return badRequest(argument + " must be the ID of a user list, a positive number")
	}
	return nil
}

func userListName(customerID, id string) string { return customerName(customerID) + "/userLists/" + id }

// typeName names the type of list the entry carries, for the confirmation; "" when it carries none.
func (l userListArgs) typeName() string {
	switch {
	case l.BasicUserList != nil:
		return "basic"
	case l.RuleBasedUserList != nil:
		return "rule based"
	case l.LogicalUserList != nil:
		return "logical"
	case l.CrmBasedUserList != nil:
		return "Customer Match (empty)"
	case l.LookalikeUserList != nil:
		return "lookalike"
	}
	return ""
}

// body checks one entry and builds its wire form and, for an update, its mask: the fields that were given.
// The name of an object is built from the checked IDs; every reference to another object belongs to the account.
func (l userListArgs) body(customerID string, create bool) (map[string]any, []string, error) {
	body := map[string]any{}
	var mask []string
	set := func(path string, value any) {
		mask = append(mask, path)
		if value != nil {
			body[path] = value
		}
	}
	if !create {
		if err := userListCheckID("user_list_id", l.UserListID); err != nil {
			return nil, nil, err
		}
		body["resourceName"] = userListName(customerID, l.UserListID)
	} else if l.UserListID != "" {
		return nil, nil, badRequest("a list to create takes no user_list_id")
	}
	switch {
	case create && l.Name == nil:
		return nil, nil, badRequest("every list to create needs a name")
	case l.Name != nil:
		if err := userListPlainText("name", *l.Name, maxNameArgument); err != nil {
			return nil, nil, err
		}
		set("name", *l.Name)
	}
	if l.Description != nil {
		if len(*l.Description) > maxLongText || strings.ContainsFunc(*l.Description, userListIsControlRune) {
			return nil, nil, badRequest("description must be text of up to 4096 bytes without control characters, " +
				"or empty to clear it")
		}
		set("description", nonEmpty(*l.Description))
	}
	if l.IntegrationCode != nil {
		if len(*l.IntegrationCode) > maxNameArgument || strings.ContainsFunc(*l.IntegrationCode, userListIsControlRune) {
			return nil, nil, badRequest("integration_code must be text of up to 1024 bytes without control " +
				"characters, or empty to clear it")
		}
		set("integrationCode", nonEmpty(*l.IntegrationCode))
	}
	if l.MembershipStatus != "" {
		if !slices.Contains(userListMembershipStatuses, l.MembershipStatus) {
			return nil, nil, badRequest("membership_status must be OPEN or CLOSED")
		}
		set("membershipStatus", l.MembershipStatus)
	}
	if v := l.MembershipLifeSpan; v != nil {
		if *v < 0 || *v > userListMaxMembershipLifeSpan {
			return nil, nil, badRequest("membership_life_span must be 0 to 540 days")
		}
		set("membershipLifeSpan", *v)
	}
	if v := l.EligibleForSearch; v != nil {
		set("eligibleForSearch", *v)
	}
	if l.AccountUserListStatus != "" {
		if !slices.Contains(userListAccessStatuses, l.AccountUserListStatus) {
			return nil, nil, badRequest("account_user_list_status must be ENABLED or DISABLED")
		}
		set("accountUserListStatus", l.AccountUserListStatus)
	}
	if l.ClosingReason != "" {
		if !slices.Contains(userListClosingReasons, l.ClosingReason) {
			return nil, nil, badRequest("closing_reason must be UNUSED")
		}
		set("closingReason", l.ClosingReason)
	}
	types := 0
	typed := func(path string, wire map[string]any, err error) error {
		types++
		if err == nil {
			set(path, wire)
		}
		return err
	}
	var err error
	if l.BasicUserList != nil {
		wire, e := l.BasicUserList.wire(customerID)
		err = typed("basicUserList", wire, e)
	}
	if err == nil && l.RuleBasedUserList != nil {
		wire, e := l.RuleBasedUserList.wire()
		err = typed("ruleBasedUserList", wire, e)
	}
	if err == nil && l.LogicalUserList != nil {
		wire, e := l.LogicalUserList.wire(customerID)
		err = typed("logicalUserList", wire, e)
	}
	if err == nil && l.CrmBasedUserList != nil {
		wire, e := l.CrmBasedUserList.wire()
		err = typed("crmBasedUserList", wire, e)
	}
	if err == nil && l.LookalikeUserList != nil {
		if !create {
			return nil, nil, badRequest("lookalike_user_list is immutable: it can only be given when a list is created")
		}
		wire, e := l.LookalikeUserList.wire()
		err = typed("lookalikeUserList", wire, e)
	}
	if err != nil {
		return nil, nil, err
	}
	switch {
	case types > 1:
		return nil, nil, badRequest("a list has one type: give at most one of basic_user_list, rule_based_user_list, " +
			"logical_user_list, crm_based_user_list and lookalike_user_list")
	case create && types == 0:
		return nil, nil, badRequest("every list to create needs exactly one of basic_user_list, rule_based_user_list, " +
			"logical_user_list, crm_based_user_list and lookalike_user_list")
	case !create && len(mask) == 0:
		return nil, nil, badRequest("every list to update needs at least one field to change")
	}
	if create {
		return body, nil, nil
	}
	return body, mask, nil
}

func userListIsControlRune(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) }

// operations checks the arguments of create or update and builds the operations in the order given. The names
// of the lists to update are returned for the binding of the answer.
func (a userListsArguments) operations(create bool) ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if n := len(a.UserLists); n == 0 || n > userListMaxUserListOperations {
		return nil, nil, badRequest("user_lists must hold 1 to 20 entries")
	}
	operations := make([]any, 0, len(a.UserLists))
	want := make([]string, len(a.UserLists))
	for i, l := range a.UserLists {
		body, mask, err := l.body(a.CustomerID, create)
		if err != nil {
			return nil, nil, err
		}
		if create {
			operations = append(operations, map[string]any{"create": body})
			continue
		}
		want[i] = userListName(a.CustomerID, l.UserListID)
		if slices.Contains(want[:i], want[i]) {
			return nil, nil, badRequest("user_lists names a user list once at most")
		}
		operations = append(operations, map[string]any{"update": body, "updateMask": strings.Join(mask, ",")})
	}
	return operations, want, nil
}

func (a userListRemoveArguments) operations() ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if n := len(a.UserListIDs); n == 0 || n > userListMaxUserListOperations {
		return nil, nil, badRequest("user_list_ids must hold 1 to 20 IDs")
	}
	operations := make([]any, 0, len(a.UserListIDs))
	want := make([]string, 0, len(a.UserListIDs))
	for _, id := range a.UserListIDs {
		if err := userListCheckID("user_list_ids", id); err != nil {
			return nil, nil, err
		}
		name := userListName(a.CustomerID, id)
		if slices.Contains(want, name) {
			return nil, nil, badRequest("user_list_ids names a user list once at most")
		}
		want = append(want, name)
		operations = append(operations, map[string]any{"remove": name})
	}
	return operations, want, nil
}

// userListSendOperations sends the operations as one atomic request and binds the answer to the account and to what
// was sent: one result per operation, in order, each with the name that was expected, or for a new object a
// name of the collection of the account. want[i] is "" for a new object. With validate_only Google answers no
// results. The names of the results are returned.
func (e callEnv) userListSendOperations(c *client, customerID, service, collection string, operations []any, want []string,
	validateOnly bool) ([]string, error) {
	var answer struct {
		Results []struct {
			ResourceName string `json:"resourceName"`
		} `json:"results"`
		PartialFailureError json.RawMessage `json:"partialFailureError"`
	}
	if err := c.mutate(e.ctx, mutation{op: e.op, tool: e.tool, customerID: customerID, service: service,
		body: mutateOperationsBody(operations, validateOnly, "")}, &answer); err != nil {
		return nil, err
	}
	// The request was sent, so an answer for other objects leaves the outcome open.
	if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
		return nil, unsureResult(e.op, "answered with a partial failure although none was asked for")
	}
	names := make([]string, len(answer.Results))
	empty := true
	for i, r := range answer.Results {
		names[i] = r.ResourceName
		empty = empty && r.ResourceName == ""
	}
	if validateOnly && empty {
		return nil, nil
	}
	if len(names) != len(operations) {
		return nil, unsureResult(e.op, "answered with another number of results than operations sent")
	}
	for i, name := range names {
		if want[i] != "" && name != want[i] || want[i] == "" && !ownedName(name, customerID, collection) ||
			slices.Contains(names[:i], name) {
			return nil, unsureResult(e.op, "answered with a result for another object than the one asked for")
		}
	}
	return names, nil
}

type (
	userListResult struct {
		ResourceName string `json:"resource_name"`
		UserListID   string `json:"user_list_id"`
	}
	userListsChanged struct {
		CustomerID   string           `json:"customer_id"`
		ValidateOnly bool             `json:"validate_only"`
		UserLists    []userListResult `json:"user_lists"`
	}
)

func userListsOutput(customerID string, validateOnly bool, names []string) userListsChanged {
	out := userListsChanged{CustomerID: customerID, ValidateOnly: validateOnly, UserLists: []userListResult{}}
	for _, name := range names {
		out.UserLists = append(out.UserLists, userListResult{ResourceName: name, UserListID: name[strings.LastIndex(name, "/")+1:]})
	}
	return out
}

var (
	userListsOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"user_lists": arrayOf(object(schema{"resource_name": stringSchema, "user_list_id": stringSchema},
			"resource_name", "user_list_id"))}, "customer_id", "validate_only", "user_lists")
	userListsFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "user_lists", Description: "Per list, in the order of the call: resource_name " +
			"(customers/ID/userLists/N, as Google answered) and user_list_id; empty after validate_only, as Google " +
			"answers no result then"},
	}
	userListCustomerIDDescription = "Ten-digit customer ID of the account without hyphens; a target of the connection or an " +
		"account below a manager target. A manager account needs a manager target that names it"
)

// The members of an entry that create and update share.
var userListScalarProperties = schema{
	"description":              schema{"type": "string", "maxLength": maxLongText},
	"membership_status":        userListEnumSchema(userListMembershipStatuses),
	"integration_code":         schema{"type": "string", "maxLength": maxNameArgument},
	"membership_life_span":     schema{"type": "integer", "minimum": 0, "maximum": userListMaxMembershipLifeSpan},
	"eligible_for_search":      booleanSchema,
	"account_user_list_status": userListEnumSchema(userListAccessStatuses),
	"closing_reason":           userListEnumSchema(userListClosingReasons),
}

func userListEntrySchema(create bool) schema {
	properties := schema{"name": schema{"type": "string", "minLength": 1, "maxLength": maxNameArgument}}
	for k, v := range userListScalarProperties {
		properties[k] = v
	}
	for k, v := range userListTypeProperties {
		properties[k] = v
	}
	if create {
		properties["lookalike_user_list"] = userListLookalikeSchema
		return object(properties, "name")
	}
	properties["user_list_id"] = userListIdStringSchema
	return object(properties, "user_list_id")
}

var userListScalarArguments = []capability.Argument{
	{Name: "description", Description: "Description of the list, up to 4096 bytes; empty clears it on an update"},
	{Name: "membership_status", Description: "OPEN: the list gathers members and can be targeted; CLOSED: no new " +
		"members, it cannot be targeted"},
	{Name: "integration_code", Description: "An ID from an external system, up to 1024 bytes; empty clears it on an " +
		"update"},
	{Name: "membership_life_span", Description: "Days a member stays on the list after the last addition, 0 to " +
		"540; Google ignores it for rule based and logical lists"},
	{Name: "eligible_for_search", Description: "true when the list is eligible for the Google Search Network"},
	{Name: "account_user_list_status", Description: "ENABLED (the default) or DISABLED; the owner of a shared list " +
		"sets DISABLED to revoke the share"},
	{Name: "closing_reason", Description: "UNUSED: the reason a list was closed for not being used for over a " +
		"year; Google sets it and clears it when the list is opened again"},
}

const userListNote = " The tool changes the audience lists of the account only: it uploads no member, reads none and " +
	"returns no personal data; a Customer Match list can be created as an empty shell. Texts in rules are only " +
	"compared and never fetched. The service takes operations[]: all entries are sent as one atomic request " +
	"without partial_failure, so Google applies all of them or none; at most 20 entries per call. Read the lists " +
	"with reports.search on user_list before and after."

func userListsDescriptor(id, title, description string, effect capability.Effect, idem capability.Idempotency,
	create bool) capability.Descriptor {
	properties := schema{"customer_id": customerIDArgument,
		"user_lists":    userListBoundedArray(userListEntrySchema(create), 1, userListMaxUserListOperations),
		"validate_only": validateOnlySchema}
	entry := "Name of the list (required, 1 to 1024 bytes, unique in the account), then "
	typ := "exactly one of the type members below"
	if !create {
		entry = "user_list_id (required), then at least one field to change: name, "
		typ = "and at most one type member, which replaces that part of the list as a whole"
	}
	fields := make([]string, 0, len(userListScalarArguments)+len(userListTypeArguments))
	for _, a := range append(slices.Clone(userListScalarArguments), userListTypeArguments...) {
		if create || a.Name != "lookalike_user_list" {
			fields = append(fields, a.Name+": "+a.Description)
		}
	}
	arguments := []capability.Argument{
		{Name: "customer_id", Required: true, Description: userListCustomerIDDescription},
		{Name: "user_lists", Required: true, Description: "One to 20 lists, as one atomic request. Each entry: " + entry +
			"the fields below, " + typ + ". Every reference to another object (action, operand, seed list) must belong " +
			"to customer_id. Members of an entry: " + strings.Join(fields, ". ") + "."},
		validateOnlyArgument,
	}
	example := `{"customer_id":"1234567890","user_lists":[{"name":"Visitors of the shop","membership_life_span":30,` +
		`"rule_based_user_list":{"flexible_rule_user_list":{"inclusive_rule_operator":"AND","inclusive_operands":[` +
		`{"rule":{"rule_item_groups":[{"rule_items":[{"name":"url__","string_rule_item":{"operator":"CONTAINS",` +
		`"value":"/shop"}}]}]}}]}}}],"validate_only":true}`
	if !create {
		example = `{"customer_id":"1234567890","user_lists":[{"user_list_id":"123456789","membership_status":"CLOSED"}]}`
	}
	d := capability.Descriptor{ID: id, Version: 1, Title: title, Description: description,
		Tags:     []string{"google", "ads", "audiences", "user-lists", map[bool]string{true: "create", false: "update"}[create]},
		Provider: Provider,
		Risk: changeRisk(effect, idem, "audience lists of a Google Ads account: names and rules; no members and no "+
			"personal data"),
		InputSchema: encoded(object(properties, "customer_id", "user_lists")), OutputSchema: encoded(userListsOutputSchema),
		Arguments: arguments, Fields: userListsFields,
		Examples: []capability.Example{{Description: "Validate a rule based list without creating it",
			Arguments: json.RawMessage(example)}},
	}
	return d
}

var userListsCreate = userListsDescriptor(userListsCreateID, "Create user lists in a Google Ads account",
	"Create audience lists in a Google Ads account (UserListService.MutateUserLists with create operations): "+
		"basic lists of conversion or remarketing actions, rule based lists, logical combinations of lists, "+
		"lookalike lists, and the empty shell of a Customer Match list. Lists are what campaigns target and what "+
		"reports segment by; a new list gathers members only from its definition."+userListNote+mutateListNote,
	capability.EffectCreate, capability.IdempotencyNonIdempotent, true)

var userListsUpdate = userListsDescriptor(userListsUpdateID, "Change user lists of a Google Ads account",
	"Change existing audience lists of a Google Ads account (UserListService.MutateUserLists with update "+
		"operations and a mask of the given fields). The name, description, status and settings of a list can "+
		"change, and the definition of a basic, rule based, logical or Customer Match list is replaced as a "+
		"whole; a lookalike list is immutable. Closing a list stops new members and its targeting; changing a "+
		"rule changes who is on the list."+userListNote+mutateListNote,
	capability.EffectUpdate, capability.IdempotencyIdempotent, false)

var userListsRemove = capability.Descriptor{
	ID: userListsRemoveID, Version: 1, Title: "Remove user lists from a Google Ads account",
	Description: "Remove audience lists from a Google Ads account (UserListService.MutateUserLists with remove " +
		"operations of customers/ID/userLists/N, built from customer_id and user_list_ids). A removed list can no " +
		"longer be targeted, campaigns and ad groups that use it lose that audience, and it is not reactivated. " +
		"One to 20 lists per call, as one atomic request without partial_failure." + mutateListListedNote,
	Tags: []string{"google", "ads", "audiences", "user-lists", "remove"}, Provider: Provider,
	RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent,
		"audience lists of a Google Ads account: their IDs; the confirmation names them"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument,
		"user_list_ids": schema{"type": "array", "minItems": 1, "maxItems": userListMaxUserListOperations, "uniqueItems": true,
			"items": userListIdStringSchema},
		"validate_only": validateOnlySchema}, "customer_id", "user_list_ids")),
	OutputSchema: encoded(userListsOutputSchema),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: userListCustomerIDDescription},
		{Name: "user_list_ids", Required: true, Description: "One to 20 distinct IDs of existing user lists of " +
			"customer_id, as reports.search on user_list returns them"},
		validateOnlyArgument},
	Fields: userListsFields,
	Examples: []capability.Example{{Description: "Remove two lists",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","user_list_ids":["123456789","123456790"]}`)}},
}

func userListsHandleCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return userListsChange(ctx, resolved, secrets, red, raw, true)
}

func userListsHandleUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return userListsChange(ctx, resolved, secrets, red, raw, false)
}

func userListsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, create bool) (any, error) {
	op, tool := "update user lists", userListsUpdateID
	if create {
		op, tool = "create user lists", userListsCreateID
	}
	var args userListsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	operations, want, err := args.operations(create)
	if err != nil {
		return nil, err
	}
	return env.userListChangeLists(args.CustomerID, operations, want, args.ValidateOnly)
}

func userListsHandleRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "remove user lists"
	var args userListRemoveArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, userListsRemoveID)
	if err != nil {
		return nil, err
	}
	operations, want, err := args.operations()
	if err != nil {
		return nil, err
	}
	return env.userListChangeLists(args.CustomerID, operations, want, args.ValidateOnly)
}

func (e callEnv) userListChangeLists(customerID string, operations []any, want []string, validateOnly bool) (any, error) {
	c, err := e.settle(customerID, true)
	if err != nil {
		return nil, err
	}
	names, err := e.userListSendOperations(c, customerID, userListService, "userLists", operations, want, validateOnly)
	if err != nil {
		return nil, err
	}
	return userListsOutput(customerID, validateOnly, names), nil
}

// listLabel quotes a text of the caller for the confirmation, cut so that it stays short.
func userListListLabel(text string) string { return strconv.Quote(boundedText(text, 80)) }

func userListValidateLead(validateOnly bool, verb string) string {
	if validateOnly {
		return "Only validate: " + verb
	}
	return verb
}

// userListsSummary phrases the creation or the update for the confirmation, from the arguments alone.
func userListsSummary(create bool) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a userListsArguments
		_ = json.Unmarshal(raw, &a)
		var parts []string
		for _, l := range a.UserLists {
			part := ""
			if create {
				if l.Name != nil {
					part = userListListLabel(*l.Name)
				}
			} else {
				part = l.UserListID
				if l.Name != nil {
					part += " (new name " + userListListLabel(*l.Name) + ")"
				}
			}
			if t := l.typeName(); t != "" {
				part += ", " + t
			}
			if l.MembershipStatus != "" {
				part += ", " + l.MembershipStatus
			}
			parts = append(parts, part)
		}
		verb := "Create " + strconv.Itoa(len(parts)) + " user list(s) in"
		if !create {
			verb = "Change " + strconv.Itoa(len(parts)) + " user list(s) of"
		}
		tail := " New lists can be targeted by campaigns."
		if !create {
			tail = " A changed rule or status changes who is on a list and whether it can be targeted."
		}
		return userListValidateLead(a.ValidateOnly, verb) + " Google Ads account " + a.CustomerID + ": " +
			strings.Join(parts, "; ") + "." + tail
	}
}

// userListsRemoveSummary phrases the removal for the confirmation, from the arguments alone.
func userListsRemoveSummary(raw json.RawMessage) string {
	var a userListRemoveArguments
	_ = json.Unmarshal(raw, &a)
	return userListValidateLead(a.ValidateOnly, "Remove user list(s) "+strings.Join(a.UserListIDs, ", ")+" of") +
		" Google Ads account " + a.CustomerID + ": they can no longer be targeted, campaigns and ad groups that use " +
		"them lose that audience, and a removed list is not reactivated."
}
