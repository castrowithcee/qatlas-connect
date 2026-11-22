package googleads

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountLinksRemoveID           = Provider + ".account-links.remove"
	productLinksRemoveID           = Provider + ".product-links.remove"
	productLinkInvitationsUpdateID = Provider + ".product-link-invitations.update"
	dataLinksRemoveID              = Provider + ".data-links.remove"
	dataLinksUpdateID              = Provider + ".data-links.update"

	accountLinkService       = "/accountLinks"
	productLinkService       = "/productLinks"
	productInvitationService = "/productLinkInvitations"
	dataLinkService          = "/dataLinks"
)

// These five tools end or step back from a link to another account, product or data source. None creates a link
// or invitation and none accepts one: ACCEPTED, ENABLED, APPROVED and ACTIVE are no value of any argument. Every
// resource name that is sent is built from checked IDs and the checked account; none comes from the caller.
//
// The services differ in form, and each tool follows its method of v26: AccountLinkService.MutateAccountLink
// takes one operation and validate_only; ProductLinkService.RemoveProductLink takes the resource name and
// validate_only; ProductLinkInvitationService.UpdateProductLinkInvitation, DataLinkService.UpdateDataLink and
// DataLinkService.RemoveDataLink take the resource name (and the status) and have no validate_only, so the tools
// for them offer none. Every request changes exactly one object and is therefore atomic.

// The statuses an update may set. The enums of v26 describe REJECTED as the answer of the invitee or approver
// and REVOKED as the withdrawal by the requester; DISABLED ends a data link in effect. ACCEPTED and ENABLED
// approve a link and are not reachable. The method pages do not say which transitions Google allows, so Google
// decides and refuses a transition that is not allowed.
var (
	invitationUpdateStatuses = []string{"REJECTED", "REVOKED"}
	dataLinkUpdateStatuses   = []string{"DISABLED", "REVOKED", "REJECTED"}
)

// linkRemovalArguments are the arguments of the five tools; a tool uses the members it names in its schema, and
// the schema refuses the others.
type linkRemovalArguments struct {
	CustomerID    string `json:"customer_id"`
	AccountLinkID string `json:"account_link_id"`
	ProductLinkID string `json:"product_link_id"`
	InvitationID  string `json:"product_link_invitation_id"`
	DataLinkID    string `json:"data_link_id"`
	Status        string `json:"status"`
	ValidateOnly  bool   `json:"validate_only"`
}

// linkRemovalKind is one of the five tools.
type linkRemovalKind struct {
	id, op, service, action string
	// name builds the resource name from the checked arguments and reports the argument that failed.
	name func(a linkRemovalArguments) (string, error)
	// statuses is set for an update; validateOnly tells whether the method offers validate_only.
	statuses     []string
	validateOnly bool
	// nested is set when the answer holds the result below "result" (MutateAccountLink).
	nested bool
}

func checkAccount(customerID string) error {
	if !customerIDPattern.MatchString(customerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	return nil
}

var linkRemovalKinds = map[string]linkRemovalKind{
	accountLinksRemoveID: {id: accountLinksRemoveID, op: "remove account link", service: accountLinkService,
		action: "mutate", validateOnly: true, nested: true,
		name: func(a linkRemovalArguments) (string, error) {
			if err := checkID("account_link_id", a.AccountLinkID); err != nil {
				return "", err
			}
			return accountLinkName(a.CustomerID, a.AccountLinkID), nil
		}},
	productLinksRemoveID: {id: productLinksRemoveID, op: "remove product link", service: productLinkService,
		action: "remove", validateOnly: true,
		name: func(a linkRemovalArguments) (string, error) {
			if err := checkID("product_link_id", a.ProductLinkID); err != nil {
				return "", err
			}
			return productLinkName(a.CustomerID, a.ProductLinkID), nil
		}},
	productLinkInvitationsUpdateID: {id: productLinkInvitationsUpdateID, op: "update product link invitation",
		service: productInvitationService, action: "update", statuses: invitationUpdateStatuses,
		name: func(a linkRemovalArguments) (string, error) {
			if err := checkID("product_link_invitation_id", a.InvitationID); err != nil {
				return "", err
			}
			return productInvitationName(a.CustomerID, a.InvitationID), nil
		}},
	dataLinksRemoveID: {id: dataLinksRemoveID, op: "remove data link", service: dataLinkService, action: "remove",
		name: dataLinkNameOf},
	dataLinksUpdateID: {id: dataLinksUpdateID, op: "update data link", service: dataLinkService, action: "update",
		statuses: dataLinkUpdateStatuses, name: dataLinkNameOf},
}

func dataLinkNameOf(a linkRemovalArguments) (string, error) {
	if err := checkID("product_link_id", a.ProductLinkID); err != nil {
		return "", err
	}
	if err := checkID("data_link_id", a.DataLinkID); err != nil {
		return "", err
	}
	return dataLinkName(a.CustomerID, a.ProductLinkID, a.DataLinkID), nil
}

// check refuses what no request may start with, before any secret or I/O, and builds the resource name.
func (k linkRemovalKind) check(a linkRemovalArguments) (string, error) {
	if err := checkAccount(a.CustomerID); err != nil {
		return "", err
	}
	name, err := k.name(a)
	if err != nil {
		return "", err
	}
	if k.statuses != nil && !slices.Contains(k.statuses, a.Status) {
		return "", badRequest("status must be " + joinOr(k.statuses) + "; a link or invitation is never accepted or " +
			"enabled by this tool")
	}
	if k.statuses == nil && a.Status != "" {
		return "", badRequest("this tool takes no status")
	}
	if a.ValidateOnly && !k.validateOnly {
		return "", badRequest("this tool has no validate_only: Google offers none for this method")
	}
	return name, nil
}

// body is the JSON request of the method, built from the checked name alone.
func (k linkRemovalKind) body(name string, a linkRemovalArguments) map[string]any {
	var body map[string]any
	switch {
	case k.nested:
		body = map[string]any{"operation": map[string]any{"remove": name}}
	case k.statuses != nil && k.service == dataLinkService:
		body = map[string]any{"resourceName": name, "dataLinkStatus": a.Status}
	case k.statuses != nil:
		body = map[string]any{"resourceName": name, "productLinkInvitationStatus": a.Status}
	default:
		body = map[string]any{"resourceName": name}
	}
	if a.ValidateOnly {
		body["validateOnly"] = true
	}
	return body
}

type linkRemoved struct {
	CustomerID   string `json:"customer_id"`
	ValidateOnly *bool  `json:"validate_only,omitempty"`
	ResourceName string `json:"resource_name,omitempty"`
}

type linkStepped struct {
	CustomerID   string `json:"customer_id"`
	ResourceName string `json:"resource_name"`
	Status       string `json:"status"`
}

func handleLinkRemoval(id string) func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor,
	json.RawMessage) (any, error) {
	k := linkRemovalKinds[id]
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var args linkRemovalArguments
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, failure(k.op, "the validated arguments could not be read")
		}
		env, err := handleEnv(ctx, resolved, secrets, red, k.op, k.id)
		if err != nil {
			return nil, err
		}
		name, err := k.check(args)
		if err != nil {
			return nil, err
		}
		c, err := env.settle(args.CustomerID, true)
		if err != nil {
			return nil, err
		}
		var answer struct {
			ResourceName        string          `json:"resourceName"`
			PartialFailureError json.RawMessage `json:"partialFailureError"`
			Result              *struct {
				ResourceName string `json:"resourceName"`
			} `json:"result"`
		}
		if err := c.mutate(ctx, mutation{op: k.op, tool: k.id, customerID: args.CustomerID, service: k.service,
			action: k.action, body: k.body(name, args)}, &answer); err != nil {
			return nil, err
		}
		if len(answer.PartialFailureError) > 0 && string(answer.PartialFailureError) != "null" {
			return nil, unsureResult(k.op, "answered with a partial failure although the request asked for none")
		}
		// Each method answers in one shape; the other one is not the answer asked for.
		got := answer.ResourceName
		if k.nested {
			if answer.ResourceName != "" {
				return nil, unsureResult(k.op, "answered with a shape this method does not have")
			}
			got = ""
			if answer.Result != nil {
				got = answer.Result.ResourceName
			}
		} else if answer.Result != nil {
			return nil, unsureResult(k.op, "answered with a shape this method does not have")
		}
		switch {
		case got == "" && args.ValidateOnly:
		case got == "":
			return nil, unsureResult(k.op, "answered without a result")
		case got != name:
			return nil, unsureResult(k.op, "answered with a result for another object than the one asked for")
		}
		if k.statuses != nil {
			return linkStepped{CustomerID: args.CustomerID, ResourceName: got, Status: args.Status}, nil
		}
		out := linkRemoved{CustomerID: args.CustomerID, ResourceName: got}
		if k.validateOnly {
			out.ValidateOnly = &args.ValidateOnly
		}
		return out, nil
	}
}

// --- descriptors ---

var (
	removedSchema = object(schema{"customer_id": stringSchema, "validate_only": booleanSchema,
		"resource_name": stringSchema}, "customer_id", "validate_only")
	steppedSchema = object(schema{"customer_id": stringSchema, "resource_name": stringSchema,
		"status": stringSchema}, "customer_id", "resource_name", "status")

	removedFields = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "validate_only", Description: "true when Google only validated the removal"},
		{Name: "resource_name", Description: "Resource name of the removed object, as Google answered, always of " +
			"the account of the call; absent after validate_only"},
	}
	removedFieldsNoValidate = []capability.Field{removedFields[0], removedFields[2]}
	steppedFields           = []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "resource_name", Description: "Resource name of the changed object, as Google answered, always of " +
			"the account of the call"},
		{Name: "status", Description: "The status the call set"},
	}

	linkIDSchema = schema{"type": "string", "pattern": idPatternText}

	customerLinkArgument = capability.Argument{Name: "customer_id", Required: true, Description: "Ten-digit " +
		"customer ID of the account without hyphens; a target of the connection or an account below a manager " +
		"target. A manager account needs a manager target that names it"}
)

const linkRemovalNote = " It acts on an object that already exists and builds its resource name from customer_id " +
	"and the IDs (as the list returns them): it creates no link or invitation and accepts none. Google decides " +
	"whether the change is allowed, and a refused change leaves everything as it was. Read the object with the " +
	"list before and after. Google refuses the link services at the Explorer access level; they need at least Basic."

const (
	validateOnlyNote   = " The request is one atomic call of one object, and validate_only checks it without making it."
	noValidateOnlyNote = " The method has no validate_only in v26, so this tool has none either: the call changes the " +
		"object. The request is one atomic call of one object."
)

const (
	accountLinkSensitivity = "link of a Google Ads account to a third-party app analytics provider: ends the data " +
		"exchange with that provider; no sharing values"
	productLinkSensitivity = "link of a Google Ads account to a product or account (Merchant Center, data partner, " +
		"Google Ads, advertising partner): removing it ends what the link provides"
	invitationSensitivity = "invitation to link a Google Ads account with another account or product: rejects or " +
		"withdraws it"
	dataLinkSensitivity = "link of a Google Ads account to data of a third party such as a YouTube video: ends or " +
		"withdraws it; the account name and ID were shared with the third party when the link was made"
)

var accountLinksRemove = capability.Descriptor{
	ID: accountLinksRemoveID, Version: 1, Title: "Remove an account link of a Google Ads account",
	Description: "Remove an existing link of a Google Ads account to a third-party app analytics provider " +
		"(AccountLinkService.MutateAccountLink with one remove operation). Google marks it REMOVED and the " +
		"provider no longer gets or sends data through it; no tool of this provider makes a link or brings it " +
		"back." + linkRemovalNote + validateOnlyNote + mutateNote,
	Tags: []string{"google", "ads", "links", "account-links", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, accountLinkSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "account_link_id": linkIDSchema,
		"validate_only": validateOnlySchema}, "customer_id", "account_link_id")),
	OutputSchema: encoded(removedSchema),
	Arguments: []capability.Argument{customerLinkArgument,
		{Name: "account_link_id", Required: true, Description: "ID of the link, as account_link_id of account-links.list"},
		validateOnlyArgument},
	Fields: removedFields,
	Examples: []capability.Example{{Description: "Validate the removal of an account link without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","account_link_id":"1122334455","validate_only":true}`)}},
}

var productLinksRemove = capability.Descriptor{
	ID: productLinksRemoveID, Version: 1, Title: "Remove a product link of a Google Ads account",
	Description: "Remove an existing link of a Google Ads account to a product or account (ProductLinkService." +
		"RemoveProductLink): a data partner, a Google Ads account, a Merchant Center account or an advertising " +
		"partner. What the link provided, such as product data for ads, stops. No tool of this provider makes a " +
		"product link or brings it back." + linkRemovalNote + validateOnlyNote + mutateNote,
	Tags: []string{"google", "ads", "links", "product-links", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, productLinkSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "product_link_id": linkIDSchema,
		"validate_only": validateOnlySchema}, "customer_id", "product_link_id")),
	OutputSchema: encoded(removedSchema),
	Arguments: []capability.Argument{customerLinkArgument,
		{Name: "product_link_id", Required: true, Description: "ID of the link, as product_link_id of product-links.list"},
		validateOnlyArgument},
	Fields: removedFields,
	Examples: []capability.Example{{Description: "Validate the removal of a product link without making it",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","product_link_id":"1122334455","validate_only":true}`)}},
}

var productLinkInvitationsUpdate = capability.Descriptor{
	ID: productLinkInvitationsUpdateID, Version: 1, Title: "Reject or withdraw a product link invitation",
	Description: "Set the status of an existing product link invitation of a Google Ads account " +
		"(ProductLinkInvitationService.UpdateProductLinkInvitation): REJECTED declines an invitation that another " +
		"account sent, REVOKED withdraws one that this account sent. ACCEPTED is not accepted: the tool never " +
		"establishes a link." + linkRemovalNote + noValidateOnlyNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		changeNote,
	Tags: []string{"google", "ads", "links", "product-link-invitations", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, invitationSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "product_link_invitation_id": linkIDSchema,
		"status": schema{"type": "string", "enum": invitationUpdateStatuses}},
		"customer_id", "product_link_invitation_id", "status")),
	OutputSchema: encoded(steppedSchema),
	Arguments: []capability.Argument{customerLinkArgument,
		{Name: "product_link_invitation_id", Required: true, Description: "ID of the invitation, as " +
			"product_link_invitation_id of product-link-invitations.list"},
		{Name: "status", Required: true, Description: "REJECTED (decline an invitation of the other account) or " +
			"REVOKED (withdraw an invitation of this account); ACCEPTED and every other value are not accepted"}},
	Fields: steppedFields,
	Examples: []capability.Example{{Description: "Reject an invitation",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","product_link_invitation_id":"1122334455","status":"REJECTED"}`)}},
}

var dataLinkArguments = []capability.Argument{customerLinkArgument,
	{Name: "product_link_id", Required: true, Description: "ID of the product link of the data link, as " +
		"product_link_id of data-links.list"},
	{Name: "data_link_id", Required: true, Description: "ID of the data link, as data_link_id of data-links.list"}}

var dataLinkIDProperties = schema{"customer_id": customerIDArgument, "product_link_id": linkIDSchema,
	"data_link_id": linkIDSchema}

var dataLinksRemove = capability.Descriptor{
	ID: dataLinksRemoveID, Version: 1, Title: "Remove a data link of a Google Ads account",
	Description: "Remove an existing data link of a Google Ads account, such as a link to a YouTube video " +
		"(DataLinkService.RemoveDataLink): the data of the third party is no longer linked to the account. No tool " +
		"of this provider makes a data link or brings it back." + linkRemovalNote + noValidateOnlyNote + mutateNote,
	Tags: []string{"google", "ads", "links", "data-links", "remove"}, Provider: Provider, RequiresToolAllowList: true,
	Risk:        changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent, dataLinkSensitivity),
	InputSchema: encoded(object(dataLinkIDProperties, "customer_id", "product_link_id", "data_link_id")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "resource_name": stringSchema},
		"customer_id")),
	Arguments: dataLinkArguments,
	Fields:    removedFieldsNoValidate,
	Examples: []capability.Example{{Description: "Remove a data link",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","product_link_id":"1122334455","data_link_id":"5566778899"}`)}},
}

var dataLinksUpdate = capability.Descriptor{
	ID: dataLinksUpdateID, Version: 1, Title: "Disable or withdraw a data link of a Google Ads account",
	Description: "Set the status of an existing data link of a Google Ads account " +
		"(DataLinkService.UpdateDataLink): DISABLED ends a link in effect, REVOKED withdraws a request this " +
		"account made, REJECTED declines a request of the other side. ENABLED is not accepted: the tool never " +
		"establishes a link." + linkRemovalNote + noValidateOnlyNote +
		" Offered by a connection with the update effect or whose tools list names it; no profile holds it." +
		changeNote,
	Tags: []string{"google", "ads", "links", "data-links", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, dataLinkSensitivity),
	InputSchema: encoded(object(schema{"customer_id": customerIDArgument, "product_link_id": linkIDSchema,
		"data_link_id": linkIDSchema, "status": schema{"type": "string", "enum": dataLinkUpdateStatuses}},
		"customer_id", "product_link_id", "data_link_id", "status")),
	OutputSchema: encoded(steppedSchema),
	Arguments: append(slices.Clone(dataLinkArguments), capability.Argument{Name: "status", Required: true,
		Description: "DISABLED (end a link), REVOKED (withdraw a request of this account) or REJECTED (decline a " +
			"request of the other side); ENABLED and every other value are not accepted"}),
	Fields: steppedFields,
	Examples: []capability.Example{{Description: "Disable a data link",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","product_link_id":"1122334455","data_link_id":"5566778899","status":"DISABLED"}`)}},
}

// --- confirmation ---

func linkRemovalSummary(id string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a linkRemovalArguments
		_ = json.Unmarshal(raw, &a)
		lead := ""
		if a.ValidateOnly {
			lead = "Only validate: "
		}
		account := " of Google Ads account " + a.CustomerID
		switch id {
		case accountLinksRemoveID:
			return lead + "Remove account link " + boundedText(a.AccountLinkID, maxCodeText) + account +
				": the app analytics provider no longer gets or sends data through it."
		case productLinksRemoveID:
			return lead + "Remove product link " + boundedText(a.ProductLinkID, maxCodeText) + account +
				": what the link provides stops."
		case productLinkInvitationsUpdateID:
			verb := "Reject"
			if a.Status == "REVOKED" {
				verb = "Revoke"
			}
			return verb + " product link invitation " + boundedText(a.InvitationID, maxCodeText) + account +
				": no link is made from it."
		case dataLinksRemoveID:
			return "Remove data link " + boundedText(a.ProductLinkID, maxCodeText) + "~" +
				boundedText(a.DataLinkID, maxCodeText) + account + ": the data is no longer linked to the account."
		}
		return "Set data link " + boundedText(a.ProductLinkID, maxCodeText) + "~" + boundedText(a.DataLinkID, maxCodeText) +
			account + " to " + boundedText(a.Status, maxCodeText) + ": the link no longer works as an enabled link."
	}
}
