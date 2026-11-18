package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountAdminsDeleteID  = Provider + ".account-admins.delete"
	locationAdminsDeleteID = Provider + ".location-admins.delete"
)

func adminRemoveRisk() capability.Risk {
	return capability.Risk{
		Effect: capability.EffectDelete, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true,
		DataSensitivity: "personal data of another person: the name of the admin (the email address while the " +
			"invitation is pending) and the role that is removed",
	}
}

// removedResult names what was removed; Google answers an empty object.
type removedResult struct {
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
}

var removedSchema = objectSchema("name", stringSchema, "removed", booleanSchema)

const adminRemoveNote = "Removes an existing admin, or revokes the invitation of an admin that is still pending; the " +
	"tool takes no address, invites nobody, and creates no admin. The admin loses access at once, and only an " +
	"invitation from another admin gives it back. An admin who holds PRIMARY_OWNER is refused because that would " +
	"take away the ownership. previous_admin and previous_role are required and only compared: the tool reads the " +
	"admin list first and removes nothing unless the admin still has exactly this name and role, so the " +
	"confirmation shows who is removed. The tool cannot tell whether the entry belongs to the consenting " +
	"account itself, so removing it can lock that account out. Google counts at most 10 changes per minute and " +
	"profile. " + adminPersonalNote

func adminRemoveDescriptor(id, title, noun, form, pattern, example string) capability.Descriptor {
	return capability.Descriptor{
		ID: id, Version: 1, Title: title, Tags: []string{"google", "business-profile", noun + "-admins", "delete"},
		Risk: adminRemoveRisk(), Provider: Provider,
		InputSchema: inputSchema([]string{"admin", "previous_admin", "previous_role"},
			"admin", `{"type":"string","maxLength":120,"pattern":"`+pattern+`","x-form":"`+form+`"}`,
			"previous_admin", adminTextSchema, "previous_role", previousRoleSchema),
		OutputSchema: json.RawMessage(removedSchema),
		Arguments: []capability.Argument{{Name: "admin", Required: true, Description: "Path of an existing admin as " +
			"listed: " + form}, previousAdminArgument, previousRoleArgument},
		Fields: []capability.Field{{Name: "name", Description: "Path of the removed admin"},
			{Name: "removed", Description: "true once Google confirmed the removal"}},
		Examples: []capability.Example{{Description: "Remove a manager", Arguments: json.RawMessage(example)}},
	}
}

var accountAdminsDelete = func() capability.Descriptor {
	d := adminRemoveDescriptor(accountAdminsDeleteID, "Remove a Business Profile account admin", "account",
		formAccountAdmin, accountAdminPattern,
		`{"admin":"accounts/1000001/admins/3000003","previous_admin":"Sample Person","previous_role":"MANAGER"}`)
	d.Description = "Remove an admin from a Google Business Profile account. " + adminRemoveNote +
		"A connection bound to a location does not offer this tool." + listedOnlyNote + " " + untrustedNote
	d.RequiresToolAllowList = true
	return d
}()

var locationAdminsDelete = func() capability.Descriptor {
	d := adminRemoveDescriptor(locationAdminsDeleteID, "Remove a Business Profile location admin", "location",
		formLocationAdmin, locationAdminPattern,
		`{"admin":"locations/2000002/admins/3000003","previous_admin":"Sample Person","previous_role":"MANAGER"}`)
	d.Description = "Remove an admin from a Google Business Profile location. " + adminRemoveNote +
		"On a connection bound to an account the location is first proved to belong to it." + listedOnlyNote +
		" " + untrustedNote
	d.RequiresToolAllowList = true
	return d
}()

// checkAdminRemoval refuses before any I/O what is not the removal of an admin who is not the primary owner.
func checkAdminRemoval(args adminArguments) error {
	switch {
	case args.PreviousRole == rolePrimaryOwner:
		return invalidArgument("an admin who holds the role PRIMARY_OWNER is not removed: that would take away the " +
			"ownership of the profile")
	case args.PreviousRole != roleOwner && args.PreviousRole != roleManager && args.PreviousRole != roleSiteManager:
		return invalidArgument("previous_role must be OWNER, MANAGER, or SITE_MANAGER")
	case strings.TrimSpace(args.PreviousAdmin) == "":
		return invalidArgument("previous_admin is required: the admin member of the entry as last read with the list tool")
	}
	return nil
}

func handleAccountAdminsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return removeAdmin(ctx, resolved, secrets, red, raw, "remove account admin", accountAdminsDeleteID, true)
}

func handleLocationAdminsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return removeAdmin(ctx, resolved, secrets, red, raw, "remove location admin", locationAdminsDeleteID, false)
}

// removeAdmin deletes an admin that the admin list holds with exactly the name and role the call names.
// Everything that can be refused is refused before a secret is read, and the DELETE is sent once.
func removeAdmin(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
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
	if err := checkAdminRemoval(args); err != nil {
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
	// A pending invitation is revocable, so it may be removed.
	if _, err := matchAdmin(admins, args, "removed", true); err != nil {
		return nil, err
	}
	notFound := notFoundAccount
	if !accountAdmin {
		notFound = notFoundLocation
	}
	var empty struct{}
	if err := c.remove(ctx, op, tool, change{root: accountRoot, path: args.Admin, key: parent,
		notFound: notFound}, &empty); err != nil {
		return nil, err
	}
	return removedResult{Name: args.Admin, Removed: true}, nil
}

// adminRemoveSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
func adminRemoveSummary(raw json.RawMessage) string {
	var a adminArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	from := a.Admin
	if i := strings.Index(a.Admin, "/admins/"); i > 0 {
		from = a.Admin[:i]
	}
	return "Remove the admin " + a.Admin + " (" + a.PreviousAdmin + ") with the role " + a.PreviousRole + " from " +
		from + "; the admin loses access at once and only a new invitation gives it back; nobody is invited and no " +
		"admin is created"
}
