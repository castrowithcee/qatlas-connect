package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountAdminsListID    = Provider + ".account-admins.list"
	accountAdminsUpdateID  = Provider + ".account-admins.update"
	locationAdminsListID   = Provider + ".location-admins.list"
	locationAdminsUpdateID = Provider + ".location-admins.update"
)

// Forms of an admin path. The ID of an admin is a short token of letters, digits, underscore, and hyphen, so
// no segment can be a path step and one admin has one spelling.
const (
	formAccountAdmin  = "accounts/ACCOUNT/admins/ADMIN"
	formLocationAdmin = "locations/LOCATION/admins/ADMIN"

	adminIDPattern       = `[A-Za-z0-9_-]{1,64}`
	accountAdminPattern  = `^accounts/` + idPattern + `/admins/` + adminIDPattern + `$`
	locationAdminPattern = `^locations/` + idPattern + `/admins/` + adminIDPattern + `$`
)

var (
	accountAdminForm  = regexp.MustCompile(accountAdminPattern)
	locationAdminForm = regexp.MustCompile(locationAdminPattern)
)

// The roles of an admin. PRIMARY_OWNER is never a target, and an admin who holds it is never changed: both
// would hand over or take away the ownership of a profile.
const (
	rolePrimaryOwner = "PRIMARY_OWNER"
	roleOwner        = "OWNER"
	roleManager      = "MANAGER"
	roleSiteManager  = "SITE_MANAGER"
)

const (
	targetRoleSchema   = `{"type":"string","enum":["OWNER","MANAGER","SITE_MANAGER"]}`
	previousRoleSchema = `{"type":"string","enum":["PRIMARY_OWNER","OWNER","MANAGER","SITE_MANAGER"]}`
	adminTextSchema    = `{"type":"string","minLength":1,"maxLength":500}`
	updateMaskRole     = "role"
	updateMaskSchema   = `{"type":"string","enum":["role"]}`
)

const adminPersonalNote = "The result names the people who administer the account or location by their first and last " +
	"name, which is personal data, and an invitation that is still pending by the invitee's email address. "

var adminsRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "personal data of other people: the first and last name of each admin, the email address of a " +
		"pending invitation, and the role of each admin",
}

func adminChangeRisk() capability.Risk {
	return capability.Risk{
		Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true,
		DataSensitivity: "personal data of another person: the name of the admin and the role that is changed",
	}
}

// admin is an administrator as the account management API documents it.
type (
	admin struct {
		Name              short `json:"name,omitempty"`
		Admin             line  `json:"admin,omitempty"`
		Account           short `json:"account,omitempty"`
		PendingInvitation flag  `json:"pendingInvitation,omitzero"`
		Role              short `json:"role,omitempty"`
	}
	accountAdminsAnswer struct {
		AccountAdmins list[admin] `json:"accountAdmins"`
	}
	locationAdminsAnswer struct {
		Admins list[admin] `json:"admins"`
	}
	adminsResult struct {
		Admins []admin `json:"admins"`
	}
)

var (
	adminSchema = objectSchema("name", stringSchema, "admin", stringSchema, "account", stringSchema,
		"pendingInvitation", booleanSchema, "role", stringSchema)
	adminsResultSchema = `{"type":"object","properties":{"admins":` + arraySchema(adminSchema) +
		`},"required":["admins"],"additionalProperties":false}`
)

var adminFields = []capability.Field{
	{Name: "name", Description: "accounts/ACCOUNT/admins/ADMIN or locations/LOCATION/admins/ADMIN"},
	{Name: "admin", Description: "First and last name of the admin, or the email address of the invitee while an " +
		"invitation is pending"},
	{Name: "account", Description: "For an admin that is a location group: accounts/ACCOUNT"},
	{Name: "pendingInvitation", Description: "True while the invitation of the admin is not accepted"},
	{Name: "role", Description: "PRIMARY_OWNER, OWNER, MANAGER, or SITE_MANAGER"},
}

const roleRaiseNote = "A higher role gives the admin rights over the locations and over the other admins. "

const adminChangeNote = "Changes the role of an admin that already exists; the tool takes no address, invites nobody, " +
	"and creates no admin. The target role is OWNER, MANAGER, or SITE_MANAGER: PRIMARY_OWNER is refused because it " +
	"transfers the ownership, and so is an admin who holds it or whose invitation is still pending. " + roleRaiseNote +
	"previous_admin and previous_role are required and only compared: the tool reads the admin list first and " +
	"changes nothing unless the admin still has exactly this name and role, so the confirmation shows what is " +
	"changed. Google counts at most 10 changes per minute and profile. "

var (
	previousAdminArgument = capability.Argument{Name: "previous_admin", Required: true, Description: "The admin " +
		"member of the entry as last read with the list tool (the name of the person); only compared with what Google " +
		"holds, never sent, and the change is refused when it differs"}
	previousRoleArgument = capability.Argument{Name: "previous_role", Required: true, Description: "The role of " +
		"the admin as last read with the list tool; only compared, never sent, and the change is refused when it " +
		"differs"}
	roleArgument = capability.Argument{Name: "role", Required: true, Description: "The new role: OWNER, " +
		"MANAGER, or SITE_MANAGER. PRIMARY_OWNER is refused"}
	adminUpdateMaskArgument = capability.Argument{Name: "update_mask", Description: "The fields to update; only " +
		"role is editable and the default"}
)

var accountAdminsList = capability.Descriptor{
	ID: accountAdminsListID, Version: 1, Title: "List Business Profile account admins",
	Description: "List the admins of one Google Business Profile account with their role and whether their " +
		"invitation is still pending. Google offers no paging for this list; at most 250 entries are shown. " +
		adminPersonalNote + "A connection bound to a location does not offer this tool. " + untrustedNote,
	Tags: []string{"google", "business-profile", "account-admins"}, Risk: adminsRisk, Provider: Provider,
	InputSchema: inputSchema(nil, "account", accountArgSchema), OutputSchema: json.RawMessage(adminsResultSchema),
	Arguments: []capability.Argument{{Name: "account", Target: "account", Description: "Account as " +
		"accounts/ACCOUNT; optional when the connection is bound to an account, and then it must be that account"}},
	Fields: []capability.Field{{Name: "admins", Description: "Admins with name, admin, account, " +
		"pendingInvitation, and role"}},
	Examples: []capability.Example{{Description: "Admins of one account",
		Arguments: json.RawMessage(`{"account":"accounts/1000001"}`)}},
}

var locationAdminsList = capability.Descriptor{
	ID: locationAdminsListID, Version: 1, Title: "List Business Profile location admins",
	Description: "List the admins of one Google Business Profile location with their role and whether their " +
		"invitation is still pending. Google offers no paging for this list; at most 250 entries are shown. " +
		adminPersonalNote + "On a connection bound to an account the location is first proved to belong to it. " +
		untrustedNote,
	Tags: []string{"google", "business-profile", "location-admins"}, Risk: adminsRisk, Provider: Provider,
	InputSchema: inputSchema(nil, "location", locationArgSchema), OutputSchema: json.RawMessage(adminsResultSchema),
	Arguments: []capability.Argument{{Name: "location", Target: "location", Description: "Location as " +
		"locations/LOCATION; optional when the connection is bound to a location, and then it must be that location"}},
	Fields: []capability.Field{{Name: "admins", Description: "Admins with name, admin, account, " +
		"pendingInvitation, and role"}},
	Examples: []capability.Example{{Description: "Admins of one location",
		Arguments: json.RawMessage(`{"location":"locations/2000002"}`)}},
}

var accountAdminsUpdate = capability.Descriptor{
	ID: accountAdminsUpdateID, Version: 1, Title: "Change the role of a Business Profile account admin",
	Description: "Change the role of an existing admin of a Google Business Profile account. " + adminChangeNote +
		"A connection bound to a location does not offer this tool. " + untrustedNote,
	Tags: []string{"google", "business-profile", "account-admins", "update"}, Risk: adminChangeRisk(),
	Provider: Provider,
	InputSchema: inputSchema([]string{"admin", "previous_admin", "previous_role", "role"},
		"admin", `{"type":"string","maxLength":120,"pattern":"`+accountAdminPattern+`","x-form":"`+formAccountAdmin+`"}`,
		"previous_admin", adminTextSchema, "previous_role", previousRoleSchema, "role", targetRoleSchema,
		"update_mask", updateMaskSchema),
	OutputSchema: json.RawMessage(adminSchema),
	Arguments: []capability.Argument{{Name: "admin", Required: true, Description: "Path of an existing admin as " +
		"listed: " + formAccountAdmin}, previousAdminArgument, previousRoleArgument, roleArgument,
		adminUpdateMaskArgument},
	Fields: adminFields,
	Examples: []capability.Example{{Description: "Make an owner a manager", Arguments: json.RawMessage(
		`{"admin":"accounts/1000001/admins/3000003","previous_admin":"Sample Person","previous_role":"OWNER",` +
			`"role":"MANAGER"}`)}},
}

var locationAdminsUpdate = capability.Descriptor{
	ID: locationAdminsUpdateID, Version: 1, Title: "Change the role of a Business Profile location admin",
	Description: "Change the role of an existing admin of a Google Business Profile location. " + adminChangeNote +
		"On a connection bound to an account the location is first proved to belong to it. " + untrustedNote,
	Tags: []string{"google", "business-profile", "location-admins", "update"}, Risk: adminChangeRisk(),
	Provider: Provider,
	InputSchema: inputSchema([]string{"admin", "previous_admin", "previous_role", "role"},
		"admin", `{"type":"string","maxLength":120,"pattern":"`+locationAdminPattern+`","x-form":"`+formLocationAdmin+`"}`,
		"previous_admin", adminTextSchema, "previous_role", previousRoleSchema, "role", targetRoleSchema,
		"update_mask", updateMaskSchema),
	OutputSchema: json.RawMessage(adminSchema),
	Arguments: []capability.Argument{{Name: "admin", Required: true, Description: "Path of an existing admin as " +
		"listed: " + formLocationAdmin}, previousAdminArgument, previousRoleArgument, roleArgument,
		adminUpdateMaskArgument},
	Fields: adminFields,
	Examples: []capability.Example{{Description: "Make a manager a site manager", Arguments: json.RawMessage(
		`{"admin":"locations/2000002/admins/3000003","previous_admin":"Sample Person","previous_role":"MANAGER",` +
			`"role":"SITE_MANAGER"}`)}},
}

type adminArguments struct {
	Account       string `json:"account"`
	Location      string `json:"location"`
	Admin         string `json:"admin"`
	PreviousAdmin string `json:"previous_admin"`
	PreviousRole  string `json:"previous_role"`
	Role          string `json:"role"`
	UpdateMask    string `json:"update_mask"`
}

func readAdminArguments(op string, raw json.RawMessage) (adminArguments, error) {
	var args adminArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, provider.Fail(op, "the validated arguments could not be read")
	}
	return args, nil
}

// adminOf checks the path of an admin against its form and the target and returns the parent (account or
// location) it lies under. A location target refuses an account admin, and every target refuses another
// parent; no message names the other object.
func (s scope) adminOf(given string, accountAdmin bool) (parent string, err error) {
	form, name := locationAdminForm, "admin"
	if accountAdmin {
		form = accountAdminForm
	}
	if !form.MatchString(given) {
		if accountAdmin {
			return "", invalidArgument(name + " must be " + formAccountAdmin + " with a decimal ID")
		}
		return "", invalidArgument(name + " must be " + formLocationAdmin + " with a decimal ID")
	}
	parent = given[:strings.Index(given, "/admins/")]
	switch {
	case accountAdmin && s.location != "":
		return "", invalidArgument("account admins are not available on a connection bound to a location")
	case accountAdmin && s.account != "" && parent != s.account,
		!accountAdmin && s.location != "" && parent != s.location:
		return "", invalidArgument(name + " " + outsideTarget)
	}
	return parent, nil
}

// checkRoleChange refuses before any I/O what is not a plain role change of an existing admin.
func checkRoleChange(args adminArguments) error {
	switch {
	case args.Role == rolePrimaryOwner:
		return invalidArgument("role PRIMARY_OWNER is not offered: it transfers the ownership of the profile")
	case args.Role != roleOwner && args.Role != roleManager && args.Role != roleSiteManager:
		return invalidArgument("role must be OWNER, MANAGER, or SITE_MANAGER")
	case args.PreviousRole == rolePrimaryOwner:
		return invalidArgument("an admin who holds the role PRIMARY_OWNER is not changed: that would take away the " +
			"ownership of the profile")
	case args.PreviousRole != roleOwner && args.PreviousRole != roleManager && args.PreviousRole != roleSiteManager:
		return invalidArgument("previous_role must be OWNER, MANAGER, or SITE_MANAGER")
	case args.PreviousRole == args.Role:
		return invalidArgument("role is the role the admin has already; there is nothing to change")
	case strings.TrimSpace(args.PreviousAdmin) == "":
		return invalidArgument("previous_admin is required: the admin member of the entry as last read with the list tool")
	case args.UpdateMask != "" && args.UpdateMask != updateMaskRole:
		return invalidArgument("update_mask must be role, the only field of an admin that can be changed")
	}
	return nil
}

const notFoundAdmin = "the admin is not in the list of the account or location; read it with the list tool"

// readAdmins reads the admin list of an account or location, once. The list has no paging.
func (c *client) readAdmins(ctx context.Context, op, tool, parent string, account bool) ([]admin, error) {
	r := request{root: accountRoot, path: parent + "/admins", notFound: notFoundAccount}
	if !account {
		r.notFound = notFoundLocation
	}
	if account {
		var answer accountAdminsAnswer
		err := c.get(ctx, op, tool, r, &answer)
		return answer.AccountAdmins, err
	}
	var answer locationAdminsAnswer
	err := c.get(ctx, op, tool, r, &answer)
	return answer.Admins, err
}

func handleAccountAdminsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list account admins"
	args, err := readAdminArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.accountOf("account", args.Account, false)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	admins, err := c.readAdmins(ctx, op, accountAdminsListID, name, true)
	if err != nil {
		return nil, err
	}
	return adminsResult{Admins: append([]admin{}, admins...)}, nil
}

func handleLocationAdminsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list location admins"
	args, err := readAdminArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.locationOf(args.Location)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if bound.account != "" {
		if err := c.proveLocation(ctx, op, bound.account, name); err != nil {
			return nil, err
		}
	}
	admins, err := c.readAdmins(ctx, op, locationAdminsListID, name, false)
	if err != nil {
		return nil, err
	}
	return adminsResult{Admins: append([]admin{}, admins...)}, nil
}

func handleAccountAdminsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return updateAdmin(ctx, resolved, secrets, red, raw, "change account admin role", accountAdminsUpdateID, true)
}

func handleLocationAdminsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return updateAdmin(ctx, resolved, secrets, red, raw, "change location admin role", locationAdminsUpdateID, false)
}

// updateAdmin changes the role of an admin that the admin list holds with exactly the name and role the
// call names. Everything that can be refused is refused before a secret is read.
func updateAdmin(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool string, accountAdmin bool) (any, error) {
	args, err := readAdminArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	parent, err := bound.adminOf(args.Admin, accountAdmin)
	if err != nil {
		return nil, err
	}
	if err := checkRoleChange(args); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if !accountAdmin && bound.account != "" {
		if err := c.proveLocation(ctx, op, bound.account, parent); err != nil {
			return nil, err
		}
	}
	admins, err := c.readAdmins(ctx, op, tool, parent, accountAdmin)
	if err != nil {
		return nil, err
	}
	if _, err := matchAdmin(admins, args, "changed", false); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]string{"role": args.Role})
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
	}
	var out admin
	notFound := notFoundAccount
	if !accountAdmin {
		notFound = notFoundLocation
	}
	if err := c.patch(ctx, op, tool, change{root: accountRoot, path: args.Admin,
		query: url.Values{"updateMask": {updateMaskRole}}, body: body, key: parent, notFound: notFound}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// matchAdmin finds the admin of the call in the list and refuses, without any change, what the call must not
// touch: an admin who holds PRIMARY_OWNER, a pending invitation unless it may be revoked, and an entry whose
// name or role is not the one the call read last. action ("changed" or "removed") only words the message.
func matchAdmin(admins []admin, args adminArguments, action string, allowPending bool) (*admin, error) {
	var current *admin
	for i := range admins {
		if string(admins[i].Name) == args.Admin {
			current = &admins[i]
			break
		}
	}
	switch {
	case current == nil && len(admins) >= maxItems:
		return nil, invalidArgument("the admin is not among the first 250 admins of the list")
	case current == nil:
		return nil, invalidArgument(notFoundAdmin)
	case current.PendingInvitation.value && !allowPending:
		return nil, invalidArgument("the invitation of this admin is still pending: only the role of an existing " +
			"admin is changed")
	case string(current.Role) == rolePrimaryOwner:
		return nil, invalidArgument("an admin who holds the role PRIMARY_OWNER is not " + action + ": that would take " +
			"away the ownership of the profile")
	case string(current.Admin) != args.PreviousAdmin || string(current.Role) != args.PreviousRole:
		return nil, invalidArgument("previous_admin and previous_role are not what Google holds for this admin now; " +
			"read the admin with the list tool and send those values")
	}
	return current, nil
}

// adminSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
func adminSummary(raw json.RawMessage) string {
	var a adminArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Change the role of the existing admin " + a.Admin + " (" + a.PreviousAdmin + ") from " + a.PreviousRole +
		" to " + a.Role + "; a higher role gives rights over the locations and the other admins; nobody is invited " +
		"and no admin is created"
}
