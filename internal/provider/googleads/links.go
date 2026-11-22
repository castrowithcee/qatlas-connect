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
	customerManagerLinksListID   = Provider + ".customer-manager-links.list"
	customerManagerLinksUpdateID = Provider + ".customer-manager-links.update"
	customerClientLinksListID    = Provider + ".customer-client-links.list"
	customerClientLinksUpdateID  = Provider + ".customer-client-links.update"

	managerLinkService = "/customerManagerLinks"
	clientLinkService  = "/customerClientLinks"
)

// The two services describe one link from its two ends. A customer-manager link is read and changed on the
// client account (customer_id is the client, the counterpart is its manager); a customer-client link on the
// manager account (customer_id is the manager, the counterpart is the client). Both are access control, so
// on a connection with targets a manager account needs a manager target that names it (callEnv.settle).
//
// A link is never made and never accepted: no create exists, ACTIVE and PENDING are no value of any argument,
// and MoveManagerLink is not offered. Only the ends of a relationship may be changed here.

// linkStatuses are the values of ManagerLinkStatus in v26 that a filter may name.
var linkStatuses = []string{"PENDING", "ACTIVE", "INACTIVE", "REFUSED", "CANCELED"}

// The statuses an update may set. REFUSED declines a request and INACTIVE ends a link, both from the client
// side; CANCELED withdraws a request the manager made. ACTIVE accepts a link and PENDING requests one, so
// neither is reachable. The enum text assigns REFUSED to the client and CANCELED to the manager, and the
// methods' pages document no more: each tool offers only the values of its own side.
var (
	managerLinkUpdateStatuses = []string{"INACTIVE", "REFUSED"}
	clientLinkUpdateStatuses  = []string{"CANCELED"}
)

func managerLinkName(customerID, managerID, linkID string) string {
	return customerName(customerID) + managerLinkService + "/" + queryNumber(managerID) + "~" + linkID
}

func clientLinkName(customerID, clientID, linkID string) string {
	return customerName(customerID) + clientLinkService + "/" + queryNumber(clientID) + "~" + linkID
}

// counterpartOf reads the customers/ID member of a link row into its canonical ID.
func counterpartOf(name string) (string, bool) {
	match := accessibleNamePattern.FindStringSubmatch(name)
	if match == nil {
		return "", false
	}
	return canonicalID(wireInt(match[1]))
}

// --- list ---

type linksArguments struct {
	CustomerID         string   `json:"customer_id"`
	Statuses           []string `json:"statuses"`
	ManagerLinkIDs     []string `json:"manager_link_ids"`
	ManagerCustomerIDs []string `json:"manager_customer_ids"`
	ClientCustomerIDs  []string `json:"client_customer_ids"`
	Hidden             *bool    `json:"hidden"`
	PageSize           *int     `json:"page_size"`
	Cursor             string   `json:"cursor"`
}

// customerIDList builds "field IN ('customers/1', ...)" from ten-digit customer IDs.
func customerIDList(argument, field string, values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > maxClientIDs {
		return "", badRequest(argument + " names at most 100 accounts")
	}
	quoted := make([]string, len(values))
	for i, v := range values {
		if !customerIDPattern.MatchString(v) {
			return "", badRequest(argument + " must hold ten-digit customer IDs without hyphens")
		}
		quoted[i] = gaqlString(customerName(v))
	}
	return field + " IN (" + strings.Join(quoted, ", ") + ")", nil
}

// conditions builds the filters of one resource; counterpart is the argument and field of the other end.
func (a linksArguments) conditions(resource, counterpartArgument, counterpartField string, counterparts []string,
	withHidden bool) ([]string, error) {
	var out []string
	add := func(condition string, err error) error {
		if err != nil {
			return err
		}
		if condition != "" {
			out = append(out, condition)
		}
		return nil
	}
	if err := add(enumList("statuses", resource+".status", a.Statuses, linkStatuses)); err != nil {
		return nil, err
	}
	if err := add(idList("manager_link_ids", resource+".manager_link_id", a.ManagerLinkIDs)); err != nil {
		return nil, err
	}
	if err := add(customerIDList(counterpartArgument, resource+"."+counterpartField, counterparts)); err != nil {
		return nil, err
	}
	if withHidden && a.Hidden != nil {
		out = append(out, resource+".hidden = "+strconv.FormatBool(*a.Hidden))
	}
	return out, nil
}

type (
	managerLinkWire struct {
		ResourceName    string   `json:"resourceName"`
		ManagerLinkID   *wireInt `json:"managerLinkId"`
		ManagerCustomer string   `json:"managerCustomer"`
		Status          string   `json:"status"`
	}
	managerLinkRow struct {
		Link *managerLinkWire `json:"customerManagerLink"`
	}
	managerLinkResult struct {
		ResourceName      string `json:"resource_name"`
		ManagerLinkID     string `json:"manager_link_id"`
		ManagerCustomer   string `json:"manager_customer"`
		ManagerCustomerID string `json:"manager_customer_id"`
		Status            string `json:"status,omitempty"`
	}
	managerLinksResult struct {
		ManagerLinks []managerLinkResult `json:"manager_links"`
		NextCursor   string              `json:"next_cursor,omitempty"`
		HasMore      bool                `json:"has_more"`
	}

	clientLinkWire struct {
		ResourceName   string   `json:"resourceName"`
		ManagerLinkID  *wireInt `json:"managerLinkId"`
		ClientCustomer string   `json:"clientCustomer"`
		Status         string   `json:"status"`
		Hidden         *bool    `json:"hidden"`
	}
	clientLinkRow struct {
		Link *clientLinkWire `json:"customerClientLink"`
	}
	clientLinkResult struct {
		ResourceName     string `json:"resource_name"`
		ManagerLinkID    string `json:"manager_link_id"`
		ClientCustomer   string `json:"client_customer"`
		ClientCustomerID string `json:"client_customer_id"`
		Status           string `json:"status,omitempty"`
		Hidden           *bool  `json:"hidden,omitempty"`
	}
	clientLinksResult struct {
		ClientLinks []clientLinkResult `json:"client_links"`
		NextCursor  string             `json:"next_cursor,omitempty"`
		HasMore     bool               `json:"has_more"`
	}
)

// managerLinkOf maps one row of the account. The counterpart must be an account name, the link ID a positive
// number, and a resource name Google sends must be the one built from the account, the counterpart and the ID.
func managerLinkOf(w *managerLinkWire, customerID string) (managerLinkResult, bool) {
	id, ok := idOf(w.ManagerLinkID)
	manager, okManager := counterpartOf(w.ManagerCustomer)
	if !ok || !okManager {
		return managerLinkResult{}, false
	}
	name := managerLinkName(customerID, manager, id)
	if w.ResourceName != "" && w.ResourceName != name {
		return managerLinkResult{}, false
	}
	return managerLinkResult{ResourceName: name, ManagerLinkID: id, ManagerCustomer: customerName(manager),
		ManagerCustomerID: manager, Status: code(w.Status)}, true
}

func clientLinkOf(w *clientLinkWire, customerID string) (clientLinkResult, bool) {
	id, ok := idOf(w.ManagerLinkID)
	client, okClient := counterpartOf(w.ClientCustomer)
	if !ok || !okClient {
		return clientLinkResult{}, false
	}
	name := clientLinkName(customerID, client, id)
	if w.ResourceName != "" && w.ResourceName != name {
		return clientLinkResult{}, false
	}
	return clientLinkResult{ResourceName: name, ManagerLinkID: id, ClientCustomer: customerName(client),
		ClientCustomerID: client, Status: code(w.Status), Hidden: w.Hidden}, true
}

// linksQuery builds the page query of one link resource, sorted by the link ID that is the keyset.
func linksQuery(resource string, fields []string) func([]string, string, int) string {
	return func(conditions []string, after string, limit int) string {
		selected := make([]string, len(fields))
		for i, field := range fields {
			selected[i] = resource + "." + field
		}
		query := "SELECT " + strings.Join(selected, ", ") + " FROM " + resource
		if after != "" {
			conditions = append(append([]string(nil), conditions...), resource+".manager_link_id > "+after)
		}
		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}
		return query + " ORDER BY " + resource + ".manager_link_id LIMIT " + strconv.Itoa(limit)
	}
}

// managerLinkFields and clientLinkFields are every field of the two resources in v26.
var (
	managerLinkFields = []string{"resource_name", "manager_link_id", "manager_customer", "status"}
	clientLinkFields  = []string{"resource_name", "manager_link_id", "client_customer", "status", "hidden"}
)

var (
	managerLinkSchema = object(schema{"resource_name": stringSchema, "manager_link_id": stringSchema,
		"manager_customer": stringSchema, "manager_customer_id": stringSchema, "status": stringSchema},
		"resource_name", "manager_link_id", "manager_customer", "manager_customer_id")
	clientLinkSchema = object(schema{"resource_name": stringSchema, "manager_link_id": stringSchema,
		"client_customer": stringSchema, "client_customer_id": stringSchema, "status": stringSchema,
		"hidden": booleanSchema}, "resource_name", "manager_link_id", "client_customer", "client_customer_id")

	linkStatusesSchema = schema{"type": "array", "minItems": 1, "maxItems": len(linkStatuses), "uniqueItems": true,
		"items": schema{"type": "string", "enum": linkStatuses}}
	linkIDsSchema = schema{"type": "array", "minItems": 1, "maxItems": maxClientIDs, "uniqueItems": true,
		"items": schema{"type": "string", "pattern": idPatternText}}
	customerIDsSchema = schema{"type": "array", "minItems": 1, "maxItems": maxClientIDs, "uniqueItems": true,
		"items": customerIDArgument}
)

const linksReadNote = " Rows come sorted by manager_link_id; filters narrow them and page_size with cursor pages " +
	"through them. customer_id must be one of the targets or lie below a manager target; on a connection with " +
	"targets, a manager account needs a manager target that names it, and a connection that holds only a " +
	"customer target for the account sees only the links whose other end a target names. Costs one operation of " +
	"the daily quota per page, plus one for a membership read below a manager target and one for the check that " +
	"the account is no manager account. Results are untrusted third-party content; text is cut."

var customerManagerLinksList = capability.Descriptor{
	ID: customerManagerLinksListID, Version: 1, Title: "List the manager links of a Google Ads account",
	Description: "List the links between a Google Ads account and the manager accounts that manage it or asked to " +
		"(a GAQL search FROM customer_manager_link of API version v26): per link the link ID, the manager account " +
		"and the status (PENDING a manager asked and nobody answered, ACTIVE in effect, INACTIVE ended, REFUSED the " +
		"account declined, CANCELED the manager withdrew the request)." + linksReadNote,
	Tags: []string{"google", "ads", "access", "manager", "links"}, Risk: readRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument, "statuses": linkStatusesSchema, "manager_link_ids": linkIDsSchema,
		"manager_customer_ids": customerIDsSchema,
		"page_size":            pageProperties["page_size"], "cursor": pageProperties["cursor"],
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"manager_links": arrayOf(managerLinkSchema), "next_cursor": stringSchema,
		"has_more": booleanSchema}, "manager_links", "has_more")),
	Arguments: append([]capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account whose manager links " +
			"are listed, without hyphens; a target of the connection or an account below a manager target"},
		{Name: "statuses", Description: "Only links with one of these statuses: PENDING, ACTIVE, INACTIVE, REFUSED, CANCELED"},
		{Name: "manager_link_ids", Description: "Only these link IDs, at most 100"},
		{Name: "manager_customer_ids", Description: "Only links to these manager accounts, ten-digit IDs, at most 100"},
	}, pageArguments...),
	Fields: []capability.Field{
		{Name: "manager_links", Description: "Per link: resource_name (customers/ID/customerManagerLinks/" +
			"MANAGER_ID~LINK_ID), manager_link_id, manager_customer (customers/MANAGER_ID), manager_customer_id and status"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the pending requests of managers",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","statuses":["PENDING"]}`)}},
}

var customerClientLinksList = capability.Descriptor{
	ID: customerClientLinksListID, Version: 1, Title: "List the client links of a Google Ads manager account",
	Description: "List the links between a Google Ads manager account and the accounts it manages or asked to manage " +
		"(a GAQL search FROM customer_client_link of API version v26): per link the link ID, the client account, " +
		"the status (PENDING the manager asked and the account has not answered, ACTIVE in effect, INACTIVE ended, " +
		"REFUSED the account declined, CANCELED the manager withdrew the request) and whether the link is hidden " +
		"in the Google Ads interface." + linksReadNote,
	Tags: []string{"google", "ads", "access", "manager", "links"}, Risk: readRisk, Provider: Provider,
	InputSchema: encoded(object(schema{
		"customer_id": customerIDArgument, "statuses": linkStatusesSchema, "manager_link_ids": linkIDsSchema,
		"client_customer_ids": customerIDsSchema, "hidden": booleanSchema,
		"page_size": pageProperties["page_size"], "cursor": pageProperties["cursor"],
	}, "customer_id")),
	OutputSchema: encoded(object(schema{"client_links": arrayOf(clientLinkSchema), "next_cursor": stringSchema,
		"has_more": booleanSchema}, "client_links", "has_more")),
	Arguments: append([]capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the manager account whose client " +
			"links are listed, without hyphens; a target of the connection or an account below a manager target"},
		{Name: "statuses", Description: "Only links with one of these statuses: PENDING, ACTIVE, INACTIVE, REFUSED, CANCELED"},
		{Name: "manager_link_ids", Description: "Only these link IDs, at most 100"},
		{Name: "client_customer_ids", Description: "Only links to these client accounts, ten-digit IDs, at most 100"},
		{Name: "hidden", Description: "true: only hidden links, false: only links that are not hidden"},
	}, pageArguments...),
	Fields: []capability.Field{
		{Name: "client_links", Description: "Per link: resource_name (customers/ID/customerClientLinks/" +
			"CLIENT_ID~LINK_ID), manager_link_id, client_customer (customers/CLIENT_ID), client_customer_id, status and hidden"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when more rows follow"},
	},
	Examples: []capability.Example{{Description: "List the active links that are not hidden",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","statuses":["ACTIVE"],"hidden":false}`)}},
}

func handleManagerLinksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list customer manager links"
	var args linksArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, customerManagerLinksListID)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if len(args.ClientCustomerIDs) > 0 || args.Hidden != nil {
		return nil, badRequest("a manager link has no client_customer_ids or hidden")
	}
	conditions, err := args.conditions("customer_manager_link", "manager_customer_ids", "manager_customer",
		args.ManagerCustomerIDs, false)
	if err != nil {
		return nil, err
	}
	r, err := reachOf(env.conn, args.CustomerID)
	if err != nil {
		return nil, err
	}
	items, next, more, err := listAccess(env, listSpec{customerID: args.CustomerID, tool: env.tool, op: op,
		conditions: conditions, pageSize: args.PageSize, cursor: args.Cursor,
		query: linksQuery("customer_manager_link", managerLinkFields)},
		func(raw json.RawMessage) (managerLinkResult, string, error) {
			var row managerLinkRow
			if json.Unmarshal(raw, &row) != nil || row.Link == nil {
				return managerLinkResult{}, "", provider.InvalidResponse(op, subject+" returned a manager link row Qatlas "+
					"Connect cannot read")
			}
			item, ok := managerLinkOf(row.Link, args.CustomerID)
			if !ok {
				return managerLinkResult{}, "", provider.InvalidResponse(op, subject+" returned a link that is not one "+
					"of the account asked for")
			}
			return item, item.ManagerLinkID, nil
		})
	if err != nil {
		return nil, err
	}
	// A customer target reaches the account alone: a link shows the other end only when a target names it.
	if r == reachCustomer {
		items = slices.DeleteFunc(items, func(l managerLinkResult) bool { return !env.conn.binding.names(l.ManagerCustomerID) })
	}
	return managerLinksResult{ManagerLinks: items, NextCursor: next, HasMore: more}, nil
}

func handleClientLinksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list customer client links"
	var args linksArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, customerClientLinksListID)
	if err != nil {
		return nil, err
	}
	if !customerIDPattern.MatchString(args.CustomerID) {
		return nil, badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if len(args.ManagerCustomerIDs) > 0 {
		return nil, badRequest("a client link has no manager_customer_ids")
	}
	conditions, err := args.conditions("customer_client_link", "client_customer_ids", "client_customer",
		args.ClientCustomerIDs, true)
	if err != nil {
		return nil, err
	}
	r, err := reachOf(env.conn, args.CustomerID)
	if err != nil {
		return nil, err
	}
	items, next, more, err := listAccess(env, listSpec{customerID: args.CustomerID, tool: env.tool, op: op,
		conditions: conditions, pageSize: args.PageSize, cursor: args.Cursor,
		query: linksQuery("customer_client_link", clientLinkFields)},
		func(raw json.RawMessage) (clientLinkResult, string, error) {
			var row clientLinkRow
			if json.Unmarshal(raw, &row) != nil || row.Link == nil {
				return clientLinkResult{}, "", provider.InvalidResponse(op, subject+" returned a client link row Qatlas "+
					"Connect cannot read")
			}
			item, ok := clientLinkOf(row.Link, args.CustomerID)
			if !ok {
				return clientLinkResult{}, "", provider.InvalidResponse(op, subject+" returned a link that is not one "+
					"of the account asked for")
			}
			return item, item.ManagerLinkID, nil
		})
	if err != nil {
		return nil, err
	}
	if r == reachCustomer {
		items = slices.DeleteFunc(items, func(l clientLinkResult) bool { return !env.conn.binding.names(l.ClientCustomerID) })
	}
	return clientLinksResult{ClientLinks: items, NextCursor: next, HasMore: more}, nil
}

// --- update ---

type linkUpdateArguments struct {
	CustomerID        string `json:"customer_id"`
	ManagerCustomerID string `json:"manager_customer_id"`
	ClientCustomerID  string `json:"client_customer_id"`
	ManagerLinkID     string `json:"manager_link_id"`
	Status            string `json:"status"`
	Hidden            *bool  `json:"hidden"`
	ValidateOnly      bool   `json:"validate_only"`
}

// check refuses what no request may start with, before any secret or I/O. The status must be a value of the
// side of the tool; anything else, ACTIVE and PENDING included, is refused here as in the schema.
func (a linkUpdateArguments) check(manager bool) error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkID("manager_link_id", a.ManagerLinkID); err != nil {
		return err
	}
	other, argument, allowed := a.ClientCustomerID, "client_customer_id", clientLinkUpdateStatuses
	if manager {
		other, argument, allowed = a.ManagerCustomerID, "manager_customer_id", managerLinkUpdateStatuses
	}
	if !customerIDPattern.MatchString(other) {
		return badRequest(argument + " must be the ten-digit customer ID without hyphens")
	}
	if other == a.CustomerID {
		return badRequest(argument + " must name another account than customer_id")
	}
	if manager {
		if !slices.Contains(allowed, a.Status) || a.Hidden != nil {
			return badRequest("status must be " + joinOr(allowed) + "; a link is never accepted or requested by this tool")
		}
		return nil
	}
	if a.Status == "" && a.Hidden == nil {
		return badRequest("give status " + joinOr(allowed) + " or hidden, or both")
	}
	if a.Status != "" && !slices.Contains(allowed, a.Status) {
		return badRequest("status may only be " + joinOr(allowed) + "; a link is never accepted or requested by this tool")
	}
	return nil
}

type linkChanged struct {
	CustomerID   string `json:"customer_id"`
	ValidateOnly bool   `json:"validate_only"`
	ResourceName string `json:"resource_name,omitempty"`
	Status       string `json:"status,omitempty"`
	Hidden       *bool  `json:"hidden,omitempty"`
}

var linkChangedSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
	"resource_name": stringSchema, "status": stringSchema, "hidden": booleanSchema}, "customer_id", "validate_only")

var linkChangedFields = []capability.Field{
	{Name: "customer_id", Description: "The account of the call"},
	{Name: "validate_only", Description: "true when Google only validated the change"},
	{Name: "resource_name", Description: "Resource name of the changed link, as Google answered; absent after validate_only"},
	{Name: "status", Description: "The status the call set, absent when it set none"},
	{Name: "hidden", Description: "The hidden flag the call set, absent when it set none"},
}

const linkChangeNote = " It only acts on a link that already exists and builds its name from customer_id, the other " +
	"account and manager_link_id (as the list returns them): it creates no link, accepts no request and moves no " +
	"account, and no status that makes or accepts a link (ACTIVE, PENDING) is accepted. Google decides whether the " +
	"link may change to the status; a change that is not allowed is refused and nothing changes. Read the link " +
	"with the list before and after. Google refuses the link services at the Explorer access level; they need at " +
	"least Basic."

var customerManagerLinksUpdate = capability.Descriptor{
	ID: customerManagerLinksUpdateID, Version: 1, Title: "Decline or end a manager link of a Google Ads account",
	Description: "Set the status of an existing link between a Google Ads account and one of its managers " +
		"(CustomerManagerLinkService.MutateCustomerManagerLink with an update of status and one operation): " +
		"REFUSED declines a request a manager made, INACTIVE ends a link in effect, after which the manager loses " +
		"the access it had through the link, which can include the access of the credential in use. Both are steps " +
		"on the side of the account (customer_id is the account that is managed)." + linkChangeNote + mutateNote,
	Tags: []string{"google", "ads", "access", "manager", "links", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"access control of a Google Ads account: declines or ends the link to a manager account"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "manager_customer_id": customerIDArgument,
		"manager_link_id": schema{"type": "string", "pattern": idPatternText},
		"status":          schema{"type": "string", "enum": managerLinkUpdateStatuses},
		"validate_only":   validateOnlySchema},
		"customer_id", "manager_customer_id", "manager_link_id", "status")),
	OutputSchema: encoded(linkChangedSchema),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the managed account without " +
			"hyphens; a target of the connection or an account below a manager target. A manager account needs a " +
			"manager target that names it"},
		{Name: "manager_customer_id", Required: true, Description: "Ten-digit customer ID of the manager account at " +
			"the other end of the link, as manager_customer_id of customer-manager-links.list"},
		{Name: "manager_link_id", Required: true, Description: "ID of the link, as manager_link_id of customer-manager-links.list"},
		{Name: "status", Required: true, Description: "REFUSED (decline a request) or INACTIVE (end a link); ACTIVE and " +
			"PENDING are not accepted"},
		validateOnlyArgument},
	Fields: linkChangedFields,
	Examples: []capability.Example{{Description: "Decline the request of a manager",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","manager_customer_id":"1000000001","manager_link_id":"1122334455","status":"REFUSED"}`)}},
}

var customerClientLinksUpdate = capability.Descriptor{
	ID: customerClientLinksUpdateID, Version: 1, Title: "Cancel a request or hide a client link of a Google Ads manager account",
	Description: "Withdraw a request or change the visibility of an existing link between a Google Ads manager " +
		"account and a client (CustomerClientLinkService.MutateCustomerClientLink with an update and its field mask): " +
		"status CANCELED withdraws a request that the manager made and the account has not answered, hidden true or " +
		"false hides the link in the Google Ads interface or shows it again. At least one of the two is given. These " +
		"are steps on the side of the manager (customer_id is the manager account); an active link is not ended by " +
		"this tool." + linkChangeNote + mutateNote,
	Tags: []string{"google", "ads", "access", "manager", "links", "update"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent,
		"access control of a Google Ads manager account: withdraws the request to a client account or hides the link"),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "client_customer_id": customerIDArgument,
		"manager_link_id": schema{"type": "string", "pattern": idPatternText},
		"status":          schema{"type": "string", "enum": clientLinkUpdateStatuses},
		"hidden":          booleanSchema, "validate_only": validateOnlySchema},
		"customer_id", "client_customer_id", "manager_link_id")),
	OutputSchema: encoded(linkChangedSchema),
	Arguments: []capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the manager account without " +
			"hyphens; a target of the connection or an account below a manager target. A manager account needs a " +
			"manager target that names it"},
		{Name: "client_customer_id", Required: true, Description: "Ten-digit customer ID of the client account at the " +
			"other end of the link, as client_customer_id of customer-client-links.list"},
		{Name: "manager_link_id", Required: true, Description: "ID of the link, as manager_link_id of customer-client-links.list"},
		{Name: "status", Description: "CANCELED (withdraw a request); ACTIVE, PENDING and every other value are not accepted"},
		{Name: "hidden", Description: "true hides the link in the Google Ads interface, false shows it again"},
		validateOnlyArgument},
	Fields: linkChangedFields,
	Examples: []capability.Example{{Description: "Withdraw a request",
		Arguments: json.RawMessage(`{"customer_id":"1000000001","client_customer_id":"1234567890","manager_link_id":"1122334455","status":"CANCELED"}`)}},
}

func handleManagerLinkUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return linkUpdate(ctx, resolved, secrets, red, raw, true)
}

func handleClientLinkUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return linkUpdate(ctx, resolved, secrets, red, raw, false)
}

func linkUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, manager bool) (any, error) {
	op, tool, service := "update customer client link", customerClientLinksUpdateID, clientLinkService
	if manager {
		op, tool, service = "update customer manager link", customerManagerLinksUpdateID, managerLinkService
	}
	var args linkUpdateArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, tool)
	if err != nil {
		return nil, err
	}
	if err := args.check(manager); err != nil {
		return nil, err
	}
	// The name is built from the checked IDs alone; no resource name comes from the caller.
	name := clientLinkName(args.CustomerID, args.ClientCustomerID, args.ManagerLinkID)
	if manager {
		name = managerLinkName(args.CustomerID, args.ManagerCustomerID, args.ManagerLinkID)
	}
	update := map[string]any{"resourceName": name}
	var mask []string
	if args.Status != "" {
		update["status"] = args.Status
		mask = append(mask, "status")
	}
	if args.Hidden != nil {
		update["hidden"] = *args.Hidden
		mask = append(mask, "hidden")
	}
	operation := map[string]any{"update": update, "updateMask": strings.Join(mask, ",")}
	body := map[string]any{}
	if manager {
		body["operations"] = []any{operation}
	} else {
		body["operation"] = operation
	}
	if args.ValidateOnly {
		body["validateOnly"] = true
	}
	c, err := env.settle(args.CustomerID, true)
	if err != nil {
		return nil, err
	}
	type result struct {
		ResourceName string `json:"resourceName"`
	}
	var answer struct {
		Result  *result  `json:"result"`
		Results []result `json:"results"`
	}
	if err := c.mutate(ctx, mutation{op: op, tool: tool, customerID: args.CustomerID, service: service, body: body},
		&answer); err != nil {
		return nil, err
	}
	var got *result
	switch {
	case manager && len(answer.Results) == 1:
		got = &answer.Results[0]
	case manager && len(answer.Results) == 0 && args.ValidateOnly:
	case manager:
		return nil, unsureResult(op, "answered with another number of results than the one operation asked for")
	default:
		got = answer.Result
	}
	out := linkChanged{CustomerID: args.CustomerID, ValidateOnly: args.ValidateOnly, Status: args.Status, Hidden: args.Hidden}
	if got != nil {
		out.ResourceName = got.ResourceName
	}
	if args.ValidateOnly && out.ResourceName == "" {
		return out, nil
	}
	// The request was sent, so an answer for another link leaves the outcome open.
	if out.ResourceName != name {
		return nil, unsureResult(op, "answered with a result for another link than the one asked for")
	}
	return out, nil
}

// linkUpdateSummary phrases the change for the confirmation, from the arguments alone.
func linkUpdateSummary(manager bool) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a linkUpdateArguments
		_ = json.Unmarshal(raw, &a)
		prefix := ""
		if a.ValidateOnly {
			prefix = "Only validate: "
		}
		if manager {
			link := "link " + a.ManagerLinkID + " of Google Ads account " + a.CustomerID + " to manager account " +
				a.ManagerCustomerID
			if a.Status == "REFUSED" {
				return prefix + "Decline " + link + ": the manager does not get the access it asked for."
			}
			return prefix + "End " + link + ": the manager loses the access it has through this link."
		}
		link := "link " + a.ManagerLinkID + " of Google Ads manager account " + a.CustomerID + " to client account " +
			a.ClientCustomerID
		var parts []string
		if a.Status == "CANCELED" {
			parts = append(parts, "withdraw the request")
		}
		if a.Hidden != nil {
			parts = append(parts, map[bool]string{true: "hide the link", false: "show the link"}[*a.Hidden])
		}
		return prefix + "Change " + link + ": " + strings.Join(parts, ", ") + "."
	}
}
