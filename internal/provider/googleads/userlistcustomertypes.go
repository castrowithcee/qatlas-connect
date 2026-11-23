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
	userListCustomerTypesCreateID = Provider + ".user-list-customer-types.create"
	userListCustomerTypesRemoveID = Provider + ".user-list-customer-types.remove"

	userListCustomerTypeService = "/userListCustomerTypes"
	// maxCustomerTypeOperations bounds a call: an operation is a pair of IDs and a category, and the
	// confirmation names every pair.
	userListMaxCustomerTypeOperations = 50
)

// customerTypeCategories are the values of UserListCustomerTypeCategory in v26 that a request may name.
var userListCustomerTypeCategories = []string{"ALL_CUSTOMERS", "CART_ABANDONERS", "CONVERTED_LEADS", "DISENGAGED_CUSTOMERS",
	"HIGH_VALUE_CUSTOMERS", "LOYALTY_TIER_1_MEMBERS", "LOYALTY_TIER_2_MEMBERS", "LOYALTY_TIER_3_MEMBERS",
	"LOYALTY_TIER_4_MEMBERS", "LOYALTY_TIER_5_MEMBERS", "LOYALTY_TIER_6_MEMBERS", "LOYALTY_TIER_7_MEMBERS",
	"PAID_SUBSCRIBERS", "PURCHASERS", "QUALIFIED_LEADS"}

// customerTypeArgs is one entry of customer_types: the pair of a user list and a category that Google names
// customers/ID/userListCustomerTypes/USER_LIST_ID~CATEGORY.
type userListCustomerTypeArgs struct {
	UserListID           string `json:"user_list_id"`
	CustomerTypeCategory string `json:"customer_type_category"`
}

type userListCustomerTypesArguments struct {
	CustomerID    string                     `json:"customer_id"`
	CustomerTypes []userListCustomerTypeArgs `json:"customer_types"`
	ValidateOnly  bool                       `json:"validate_only"`
}

func userListCustomerTypeName(customerID string, t userListCustomerTypeArgs) string {
	return customerName(customerID) + "/userListCustomerTypes/" + t.UserListID + "~" + t.CustomerTypeCategory
}

// operations checks the arguments and builds the operations in the order given. Both names are built from the
// checked IDs and the category; the list named by a create belongs to the account of the call.
func (a userListCustomerTypesArguments) operations(create bool) ([]any, []string, error) {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return nil, nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if n := len(a.CustomerTypes); n == 0 || n > userListMaxCustomerTypeOperations {
		return nil, nil, badRequest("customer_types must hold 1 to 50 entries")
	}
	operations := make([]any, 0, len(a.CustomerTypes))
	want := make([]string, 0, len(a.CustomerTypes))
	for _, t := range a.CustomerTypes {
		if err := userListCheckID("user_list_id", t.UserListID); err != nil {
			return nil, nil, err
		}
		if !slices.Contains(userListCustomerTypeCategories, t.CustomerTypeCategory) {
			return nil, nil, badRequest("customer_type_category must be one of the categories of the API, such as " +
				"PURCHASERS")
		}
		name := userListCustomerTypeName(a.CustomerID, t)
		if slices.Contains(want, name) {
			return nil, nil, badRequest("customer_types names a pair of user list and category once at most")
		}
		want = append(want, name)
		if create {
			operations = append(operations, map[string]any{"create": map[string]any{
				"userList":             userListName(a.CustomerID, t.UserListID),
				"customerTypeCategory": t.CustomerTypeCategory}})
		} else {
			operations = append(operations, map[string]any{"remove": name})
		}
	}
	return operations, want, nil
}

type (
	userListCustomerTypeResult struct {
		ResourceName         string `json:"resource_name"`
		UserListID           string `json:"user_list_id"`
		CustomerTypeCategory string `json:"customer_type_category"`
	}
	userListCustomerTypesChanged struct {
		CustomerID    string                       `json:"customer_id"`
		ValidateOnly  bool                         `json:"validate_only"`
		CustomerTypes []userListCustomerTypeResult `json:"customer_types"`
	}
)

var (
	userListCustomerTypesOutputSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"customer_types": arrayOf(object(schema{"resource_name": stringSchema, "user_list_id": stringSchema,
			"customer_type_category": stringSchema}, "resource_name", "user_list_id", "customer_type_category"))},
		"customer_id", "validate_only", "customer_types")
	userListCustomerTypesFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the change"},
		{Name: "customer_types", Description: "Per pair, in the order of the call: resource_name " +
			"(customers/ID/userListCustomerTypes/USER_LIST_ID~CATEGORY, as Google answered), user_list_id and " +
			"customer_type_category; empty after validate_only, as Google answers no result then"},
	}
	userListCustomerTypesInputSchema = encoded(object(schema{"customer_id": customerIDArgument,
		"customer_types": userListBoundedArray(object(schema{"user_list_id": userListIdStringSchema,
			"customer_type_category": userListEnumSchema(userListCustomerTypeCategories)}, "user_list_id", "customer_type_category"),
			1, userListMaxCustomerTypeOperations),
		"validate_only": validateOnlySchema}, "customer_id", "customer_types"))
	userListCustomerTypesArgumentsDoc = []capability.Argument{
		{Name: "customer_id", Required: true, Description: userListCustomerIDDescription},
		{Name: "customer_types", Required: true, Description: "One to 50 distinct pairs, as one atomic request. Each " +
			"pair: user_list_id (a user list of customer_id) and customer_type_category: ALL_CUSTOMERS, " +
			"CART_ABANDONERS, CONVERTED_LEADS, DISENGAGED_CUSTOMERS, HIGH_VALUE_CUSTOMERS, " +
			"LOYALTY_TIER_1_MEMBERS to LOYALTY_TIER_7_MEMBERS, PAID_SUBSCRIBERS, PURCHASERS, QUALIFIED_LEADS"},
		validateOnlyArgument,
	}
)

const userListCustomerTypeNote = " A customer type tells Google which kind of customers a Customer Match list holds, so that " +
	"it can be used for the matching goals of an account. The tool only attaches or detaches that label: it " +
	"uploads no member and reads none. The service takes operations[]: all pairs are sent as one atomic " +
	"request without partial_failure, so Google applies all of them or none; at most 50 pairs per call."

var userListCustomerTypesCreate = capability.Descriptor{
	ID: userListCustomerTypesCreateID, Version: 1, Title: "Attach customer types to user lists of a Google Ads account",
	Description: "Attach a customer type category to a user list (UserListCustomerTypeService." +
		"MutateUserListCustomerTypes with create operations of a userList and a customerTypeCategory). The user " +
		"list must belong to customer_id, and Google refuses a pair that exists." + userListCustomerTypeNote + mutateListNote,
	Tags: []string{"google", "ads", "audiences", "user-lists", "customer-types", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent,
		"customer type labels of audience lists of a Google Ads account; no members and no personal data"),
	InputSchema: userListCustomerTypesInputSchema, OutputSchema: encoded(userListCustomerTypesOutputSchema),
	Arguments: userListCustomerTypesArgumentsDoc, Fields: userListCustomerTypesFields,
	Examples: []capability.Example{{Description: "Mark a list as the purchasers",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","customer_types":[{"user_list_id":"123456789",` +
			`"customer_type_category":"PURCHASERS"}]}`)}},
}

var userListCustomerTypesRemove = capability.Descriptor{
	ID: userListCustomerTypesRemoveID, Version: 1, Title: "Detach customer types from user lists of a Google Ads account",
	Description: "Detach a customer type category from a user list (UserListCustomerTypeService." +
		"MutateUserListCustomerTypes with remove operations of customers/ID/userListCustomerTypes/" +
		"USER_LIST_ID~CATEGORY, built from customer_id, the user list and the category). The list itself stays." +
		userListCustomerTypeNote + mutateListNote,
	Tags: []string{"google", "ads", "audiences", "user-lists", "customer-types", "remove"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent,
		"customer type labels of audience lists of a Google Ads account; no members and no personal data"),
	InputSchema: userListCustomerTypesInputSchema, OutputSchema: encoded(userListCustomerTypesOutputSchema),
	Arguments: userListCustomerTypesArgumentsDoc, Fields: userListCustomerTypesFields,
	Examples: []capability.Example{{Description: "Detach a category",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","customer_types":[{"user_list_id":"123456789",` +
			`"customer_type_category":"PURCHASERS"}]}`)}},
}

func userListHandleCustomerTypesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return userListCustomerTypesChange(ctx, resolved, secrets, red, raw, true)
}

func userListHandleCustomerTypesRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return userListCustomerTypesChange(ctx, resolved, secrets, red, raw, false)
}

func userListCustomerTypesChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, create bool) (any, error) {
	op, tool := "detach user list customer types", userListCustomerTypesRemoveID
	if create {
		op, tool = "attach user list customer types", userListCustomerTypesCreateID
	}
	var args userListCustomerTypesArguments
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
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	names, err := env.userListSendOperations(c, args.CustomerID, userListCustomerTypeService, "userListCustomerTypes",
		operations, want, args.ValidateOnly)
	if err != nil {
		return nil, err
	}
	out := userListCustomerTypesChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly,
		CustomerTypes: []userListCustomerTypeResult{}}
	for i, name := range names {
		t := args.CustomerTypes[i]
		out.CustomerTypes = append(out.CustomerTypes, userListCustomerTypeResult{ResourceName: name, UserListID: t.UserListID,
			CustomerTypeCategory: t.CustomerTypeCategory})
	}
	return out, nil
}

// customerTypesSummary phrases the change for the confirmation, from the arguments alone.
func userListCustomerTypesSummary(create bool) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a userListCustomerTypesArguments
		_ = json.Unmarshal(raw, &a)
		parts := make([]string, len(a.CustomerTypes))
		for i, t := range a.CustomerTypes {
			parts[i] = "list " + t.UserListID + " as " + t.CustomerTypeCategory
		}
		verb, tail := "Attach", " The lists keep their members; only the labels change."
		if !create {
			verb = "Detach"
		}
		return userListValidateLead(a.ValidateOnly, verb+" "+strconv.Itoa(len(parts))+" customer type(s) of") +
			" Google Ads account " + a.CustomerID + ": " + strings.Join(parts, "; ") + "." + tail
	}
}
