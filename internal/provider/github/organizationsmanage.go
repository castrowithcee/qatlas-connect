package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The organization management tools change the settings of an organization, the role of an existing member,
// and remove members, outside collaborators, and invitations. Every tool names an organization as an owner
// target and is offered only where a connection's tools list names it. GitHub invites an account that is not
// a member when it is given a membership, so the tools that set a role read the membership first and refuse
// without a change unless it is active. Nothing here invites anyone or reads the billing contact for a change.

const (
	organizationTextSchema = `{"type":"string","maxLength":255}`
	organizationIDSchema   = `{"type":"integer","minimum":1,"maximum":9007199254740991}`

	organizationBaseRoleSchema          = `{"type":"string","enum":["admin","member"]}`
	organizationDefaultPermSchema       = `{"type":"string","enum":["read","write","admin","none"]}`
	maxOrganizationTextLength           = 255
	maxOrganizationNumericID      int64 = 9007199254740991
)

var (
	organizationBaseRoles    = []string{"admin", "member"}
	organizationDefaultPerms = []string{"read", "write", "admin", "none"}
)

// Permission messages. GitHub decides on every request; a message names what the request needs without
// claiming what the configured token holds.
const (
	organizationChangePermission = "GitHub refused this token the change of this organization; it needs " +
		"admin:org on a classic token, or Administration: read and write of the organization on a fine-grained token"
	organizationMembersChangePermission = "GitHub refused this token this change of the people of this " +
		"organization; it needs admin:org on a classic token, or Members: read and write of the organization on a " +
		"fine-grained token, and organization owner rights"
	organizationRoleAssignPermission = "GitHub refused this token the assignment of this organization role; it " +
		"needs admin:org on a classic token, or Members: read and write of the organization on a fine-grained " +
		"token, and organization administrator rights or the role permission to assign organization roles"
	organizationUpdateRejected = "GitHub rejected the change as invalid; check the profile texts and the " +
		"default repository permission, and whether the organization's plan allows the creation settings"
	organizationRoleRejected = "GitHub rejected the request as invalid; check the role"
	organizationRemoveReject = "GitHub rejected the removal; check the account and that it still belongs to the " +
		"organization"
	outsideCollaboratorRejected = "GitHub rejected the removal; the account may be a member of the organization " +
		"and not an outside collaborator"
	organizationInvitationRejected = "GitHub rejected the request as invalid; check the invitation id"
	organizationRoleAssignRejected = "GitHub rejected the assignment; check the role id and that the account is " +
		"an active member of the organization"
)

var organizationUsernameArgument = capability.Argument{Name: "username", Description: "Login of the account",
	Required: true}

func organizationChangeTool(id, title, description string, tags []string, risk capability.Risk, input, output json.RawMessage,
	arguments []capability.Argument, fields []capability.Field, example capability.Example) capability.Descriptor {
	return capability.Descriptor{ID: Provider + "." + id, Version: 1, Title: title, Description: description,
		Tags: append([]string{"github", "organization"}, tags...), Risk: risk, Provider: Provider,
		RequiresToolAllowList: true, InputSchema: input, OutputSchema: output, Arguments: arguments, Fields: fields,
		Examples: []capability.Example{example}}
}

func organizationChangeOutput(properties string, required ...string) json.RawMessage {
	quoted := make([]string, len(required))
	for i, name := range required {
		quoted[i] = `"` + name + `"`
	}
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` +
		strings.Join(quoted, ",") + `],"additionalProperties":false}`)
}

// organizationTextSettings and organizationFlagSettings are the settings organizations.update changes: the profile, the default repository
// permission, and what members may create or fork. Billing contact, security switches for new repositories,
// and deploy keys are not among them.
var organizationTextSettings = []string{"name", "description", "company", "email", "twitter_username", "location", "blog"}

var organizationFlagSettings = []string{"has_organization_projects", "has_repository_projects",
	"members_can_create_repositories", "members_can_create_public_repositories",
	"members_can_create_private_repositories", "members_can_create_internal_repositories",
	"members_can_create_pages", "members_can_fork_private_repositories", "web_commit_signoff_required"}

var organizationUpdateInput = func() json.RawMessage {
	var properties []string
	for _, key := range organizationTextSettings {
		properties = append(properties, `"`+key+`":`+organizationTextSchema)
	}
	properties = append(properties, `"default_repository_permission":`+organizationDefaultPermSchema)
	for _, key := range organizationFlagSettings {
		properties = append(properties, `"`+key+`":{"type":"boolean"}`)
	}
	return inputSchema(strings.Join(properties, ","))
}()

var organizationUpdateArguments = []capability.Argument{
	{Name: "name", Description: "Display name, at most 255 characters, untrusted text"},
	{Name: "description", Description: "Profile description, at most 255 characters, untrusted text"},
	{Name: "company", Description: "Company, at most 255 characters, untrusted text"},
	{Name: "email", Description: "Public contact email address as text, at most 255 characters; this is not the " +
		"billing contact, which no tool changes"},
	{Name: "twitter_username", Description: "Twitter or X username, untrusted text"},
	{Name: "location", Description: "Location, at most 255 characters, untrusted text"},
	{Name: "blog", Description: "Website as text, at most 255 characters, untrusted text"},
	{Name: "default_repository_permission", Description: "read, write, admin, or none: the permission every " +
		"member gets on every repository of the organization; write and admin are rights elevations for all members"},
	{Name: "has_organization_projects", Description: "Whether the organization uses projects"},
	{Name: "has_repository_projects", Description: "Whether repositories of the organization use projects"},
	{Name: "members_can_create_repositories", Description: "Whether members may create repositories"},
	{Name: "members_can_create_public_repositories", Description: "Whether members may create public repositories"},
	{Name: "members_can_create_private_repositories", Description: "Whether members may create private repositories"},
	{Name: "members_can_create_internal_repositories", Description: "Whether members may create internal repositories"},
	{Name: "members_can_create_pages", Description: "Whether members may create Pages sites"},
	{Name: "members_can_fork_private_repositories", Description: "Whether members may fork private repositories"},
	{Name: "web_commit_signoff_required", Description: "Whether web commits need a sign-off"},
}

var organizationsUpdate = organizationChangeTool("organizations.update", "Update a GitHub organization",
	"Change the profile, the default repository permission, or what members may create or fork in an "+
		"organization an explicit connection allows; only the arguments named are sent, and the billing contact "+
		"cannot be changed; the default repository permission write or admin raises the rights of every member and "+
		"is security relevant; idempotent, since sending the same values again leaves GitHub in the same state; "+
		"offered only where a connection's tools list names it",
	[]string{"update"}, changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	organizationUpdateInput, organizationDetailsOutput, organizationUpdateArguments, organizationDetailsFields,
	capability.Example{Description: "Make new repositories private for members", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","members_can_create_public_repositories":false}`)})

var organizationMembersSetRole = organizationChangeTool("organization-members.set-role",
	"Set the role of a GitHub organization member",
	"Set the role of an account that already is an active member of an organization an explicit connection "+
		"allows to admin or member; an account that is not an active member (missing, or invitation pending) is "+
		"refused before any change, so no invitation is ever created; the role admin makes the account an "+
		"organization owner and is security relevant; idempotent; offered only where a connection's tools list names it",
	[]string{"update", "members"}, changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	inputSchema(`"username":`+slugSchema+`,"role":`+organizationBaseRoleSchema, "username", "role"),
	json.RawMessage(`{"type":"object","properties":{`+organizationMembershipProperties+
		`},"required":["username"],"additionalProperties":false}`),
	[]capability.Argument{organizationUsernameArgument,
		{Name: "role", Description: "admin or member; admin makes the account an organization owner and is a " +
			"rights elevation", Required: true}},
	organizationMembersGet.Fields,
	capability.Example{Description: "Make a member an owner", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","username":"octocat","role":"admin"}`)})

var organizationMembersRemove = organizationChangeTool("organization-members.remove",
	"Remove a member from a GitHub organization",
	"Remove an account from an organization an explicit connection allows, with its membership in every team "+
		"and its access to the organization's repositories; for an invitation that is still pending, GitHub "+
		"withdraws the invitation; offered only where a connection's tools list names it",
	[]string{"delete", "members"}, changeRisk(capability.EffectDelete, capability.IdempotencyUnknown),
	inputSchema(`"username":`+slugSchema, "username"),
	organizationChangeOutput(`"removed":{"type":"boolean"},"username":{"type":"string"}`, "removed", "username"),
	[]capability.Argument{organizationUsernameArgument},
	[]capability.Field{{Name: "removed", Description: "True once GitHub removed the account"}, teamUsernameField},
	capability.Example{Description: "Remove a member", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","username":"octocat"}`)})

var organizationRolesAssign = organizationChangeTool("organization-roles.assign",
	"Assign an organization role to a GitHub organization member",
	"Assign an organization role, as organization-roles.list reports its id, to an account that already is an "+
		"active member of an organization an explicit connection allows; an account that is not an active member "+
		"(missing, or invitation pending) is refused before any change, so no invitation is ever created; a role "+
		"can carry administrative permissions and assigning it is security relevant; idempotent; offered only "+
		"where a connection's tools list names it",
	[]string{"update", "roles"}, changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	inputSchema(`"username":`+slugSchema+`,"role_id":`+organizationIDSchema, "username", "role_id"),
	organizationChangeOutput(`"assigned":{"type":"boolean"},"username":{"type":"string"},"role_id":{"type":"integer"}`,
		"assigned", "username", "role_id"),
	[]capability.Argument{organizationUsernameArgument,
		{Name: "role_id", Description: "Numeric id of the organization role, as github.organization-roles.list " +
			"reports it", Required: true}},
	[]capability.Field{{Name: "assigned", Description: "True once GitHub assigned the role"}, teamUsernameField,
		{Name: "role_id", Description: "Id of the role the call addressed"}},
	capability.Example{Description: "Assign a role", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","username":"octocat","role_id":8132}`)})

var outsideCollaboratorsRemove = organizationChangeTool("outside-collaborators.remove",
	"Remove an outside collaborator from a GitHub organization",
	"Remove an outside collaborator from all repositories of an organization an explicit connection allows; "+
		"GitHub refuses an account that is a member; offered only where a connection's tools list names it",
	[]string{"delete", "members"}, changeRisk(capability.EffectDelete, capability.IdempotencyUnknown),
	inputSchema(`"username":`+slugSchema, "username"),
	organizationChangeOutput(`"removed":{"type":"boolean"},"username":{"type":"string"}`, "removed", "username"),
	[]capability.Argument{organizationUsernameArgument},
	[]capability.Field{{Name: "removed", Description: "True once GitHub removed the outside collaborator"},
		teamUsernameField},
	capability.Example{Description: "Remove an outside collaborator", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","username":"octocat"}`)})

var organizationInvitationsCancel = organizationChangeTool("organization-invitations.cancel",
	"Cancel a pending invitation of a GitHub organization",
	"Cancel one pending invitation of an organization an explicit connection allows, by the id "+
		"organization-invitations.list reports; offered only where a connection's tools list names it",
	[]string{"delete", "invitations"}, changeRisk(capability.EffectDelete, capability.IdempotencyUnknown),
	inputSchema(`"invitation_id":`+organizationIDSchema, "invitation_id"),
	organizationChangeOutput(`"cancelled":{"type":"boolean"},"invitation_id":{"type":"integer"}`,
		"cancelled", "invitation_id"),
	[]capability.Argument{{Name: "invitation_id", Description: "Numeric id of the invitation, as " +
		"github.organization-invitations.list reports it", Required: true}},
	[]capability.Field{{Name: "cancelled", Description: "True once GitHub cancelled the invitation"},
		{Name: "invitation_id", Description: "Id of the invitation the call addressed"}},
	capability.Example{Description: "Cancel an invitation", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","invitation_id":77}`)})

// organizationChangeToolIDs are the organization tools that change something; every one names an organization
// as its owner target and is offered only where a connection's tools list names it.
var organizationChangeToolIDs = []string{organizationsUpdate.ID, organizationMembersSetRole.ID,
	organizationMembersRemove.ID, organizationRolesAssign.ID, outsideCollaboratorsRemove.ID,
	organizationInvitationsCancel.ID}

// organizationToolIDs are all organization tools that name an organization as their owner target.
var organizationToolIDs = slices.Concat(organizationReadToolIDs, organizationChangeToolIDs)

// ---- Arguments and checks

// organizationChangeArguments holds the arguments of the organization management tools; the input schema of
// each admits only its own. The pointers tell a setting left out from one sent.
type organizationChangeArguments struct {
	Name                                *string `json:"name"`
	Description                         *string `json:"description"`
	Company                             *string `json:"company"`
	Email                               *string `json:"email"`
	TwitterUsername                     *string `json:"twitter_username"`
	Location                            *string `json:"location"`
	Blog                                *string `json:"blog"`
	DefaultRepositoryPermission         *string `json:"default_repository_permission"`
	HasOrganizationProjects             *bool   `json:"has_organization_projects"`
	HasRepositoryProjects               *bool   `json:"has_repository_projects"`
	MembersCanCreateRepositories        *bool   `json:"members_can_create_repositories"`
	MembersCanCreatePublicRepositories  *bool   `json:"members_can_create_public_repositories"`
	MembersCanCreatePrivateRepositories *bool   `json:"members_can_create_private_repositories"`
	MembersCanCreateInternalRepos       *bool   `json:"members_can_create_internal_repositories"`
	MembersCanCreatePages               *bool   `json:"members_can_create_pages"`
	MembersCanForkPrivateRepositories   *bool   `json:"members_can_fork_private_repositories"`
	WebCommitSignoffRequired            *bool   `json:"web_commit_signoff_required"`

	Username     string `json:"username"`
	Role         string `json:"role"`
	RoleID       int64  `json:"role_id"`
	InvitationID int64  `json:"invitation_id"`
}

// settings is the body of the update: exactly the settings that were named.
func (a *organizationChangeArguments) settings() map[string]any {
	body := map[string]any{}
	for key, value := range map[string]*string{"name": a.Name, "description": a.Description, "company": a.Company,
		"email": a.Email, "twitter_username": a.TwitterUsername, "location": a.Location, "blog": a.Blog} {
		if value != nil {
			body[key] = *value
		}
	}
	if a.DefaultRepositoryPermission != nil {
		body["default_repository_permission"] = strings.ToLower(*a.DefaultRepositoryPermission)
	}
	for key, value := range map[string]*bool{"has_organization_projects": a.HasOrganizationProjects,
		"has_repository_projects":                  a.HasRepositoryProjects,
		"members_can_create_repositories":          a.MembersCanCreateRepositories,
		"members_can_create_public_repositories":   a.MembersCanCreatePublicRepositories,
		"members_can_create_private_repositories":  a.MembersCanCreatePrivateRepositories,
		"members_can_create_internal_repositories": a.MembersCanCreateInternalRepos,
		"members_can_create_pages":                 a.MembersCanCreatePages,
		"members_can_fork_private_repositories":    a.MembersCanForkPrivateRepositories,
		"web_commit_signoff_required":              a.WebCommitSignoffRequired} {
		if value != nil {
			body[key] = *value
		}
	}
	return body
}

func checkOrganizationUpdate(a *organizationChangeArguments, _ target) error {
	for argument, value := range map[string]*string{"name": a.Name, "description": a.Description,
		"company": a.Company, "email": a.Email, "twitter_username": a.TwitterUsername, "location": a.Location,
		"blog": a.Blog} {
		if value != nil {
			if err := checkTeamText(argument, *value, maxOrganizationTextLength, false); err != nil {
				return err
			}
		}
	}
	if a.DefaultRepositoryPermission != nil {
		if err := checkTeamEnum("default_repository_permission", *a.DefaultRepositoryPermission,
			organizationDefaultPerms); err != nil {
			return err
		}
	}
	if len(a.settings()) == 0 {
		return invalidRequest("give at least one setting to change")
	}
	return nil
}

func checkOrganizationSetRole(a *organizationChangeArguments, _ target) error {
	if err := checkUsername(a.Username); err != nil {
		return err
	}
	if err := checkTeamEnum("role", a.Role, organizationBaseRoles); err != nil {
		return err
	}
	a.Role = strings.ToLower(a.Role)
	return nil
}

func checkOrganizationUsername(a *organizationChangeArguments, _ target) error {
	return checkUsername(a.Username)
}

func checkOrganizationRoleAssign(a *organizationChangeArguments, _ target) error {
	if err := checkUsername(a.Username); err != nil {
		return err
	}
	if a.RoleID < 1 || a.RoleID > maxOrganizationNumericID {
		return invalidRequest("role_id must be a positive organization role id")
	}
	return nil
}

func checkOrganizationInvitationCancel(a *organizationChangeArguments, _ target) error {
	if a.InvitationID < 1 || a.InvitationID > maxOrganizationNumericID {
		return invalidRequest("invitation_id must be a positive invitation id")
	}
	return nil
}

// organizationChangeHandler decodes and checks the arguments and the organization before a credential is
// resolved, so a refused request never becomes a secret read or a provider call.
func organizationChangeHandler(id string, check func(*organizationChangeArguments, target) error,
	call func(context.Context, *Client, *organizationChangeArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments organizationChangeArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if err := check(&arguments, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

func organizationManagementOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: organizationsUpdate, Handler: organizationChangeHandler(organizationsUpdate.ID,
			checkOrganizationUpdate, func(ctx context.Context, c *Client, a *organizationChangeArguments) (any, error) {
				return c.updateOrganization(ctx, a)
			})},
		{Descriptor: organizationMembersSetRole, Handler: organizationChangeHandler(organizationMembersSetRole.ID,
			checkOrganizationSetRole, func(ctx context.Context, c *Client, a *organizationChangeArguments) (any, error) {
				return c.setOrganizationRole(ctx, a)
			})},
		{Descriptor: organizationMembersRemove, Handler: organizationChangeHandler(organizationMembersRemove.ID,
			checkOrganizationUsername, func(ctx context.Context, c *Client, a *organizationChangeArguments) (any, error) {
				return c.removeOrganizationMember(ctx, a)
			})},
		{Descriptor: organizationRolesAssign, Handler: organizationChangeHandler(organizationRolesAssign.ID,
			checkOrganizationRoleAssign, func(ctx context.Context, c *Client, a *organizationChangeArguments) (any, error) {
				return c.assignOrganizationRole(ctx, a)
			})},
		{Descriptor: outsideCollaboratorsRemove, Handler: organizationChangeHandler(outsideCollaboratorsRemove.ID,
			checkOrganizationUsername, func(ctx context.Context, c *Client, a *organizationChangeArguments) (any, error) {
				return c.removeOutsideCollaborator(ctx, a)
			})},
		{Descriptor: organizationInvitationsCancel, Handler: organizationChangeHandler(organizationInvitationsCancel.ID,
			checkOrganizationInvitationCancel, func(ctx context.Context, c *Client, a *organizationChangeArguments) (any, error) {
				return c.cancelOrganizationInvitation(ctx, a)
			})},
	}
}

// ---- Results

// OrganizationMemberRemoved reports an account removed from an organization.
type OrganizationMemberRemoved struct {
	Removed  bool   `json:"removed"`
	Username string `json:"username"`
}

// OrganizationRoleAssigned reports an organization role given to a member.
type OrganizationRoleAssigned struct {
	Assigned bool   `json:"assigned"`
	Username string `json:"username"`
	RoleID   int64  `json:"role_id"`
}

// OutsideCollaboratorRemoved reports an outside collaborator removed from an organization.
type OutsideCollaboratorRemoved struct {
	Removed  bool   `json:"removed"`
	Username string `json:"username"`
}

// OrganizationInvitationCancelled reports a cancelled invitation.
type OrganizationInvitationCancelled struct {
	Cancelled    bool  `json:"cancelled"`
	InvitationID int64 `json:"invitation_id"`
}

// ---- Requests

// requireActiveOrganizationMember reads the membership of the account and refuses, without any change, an
// account that is not an active member: giving such an account a membership makes GitHub invite it.
func (c *Client) requireActiveOrganizationMember(ctx context.Context, username string) error {
	const op = "read organization membership"
	var raw struct {
		State string `json:"state"`
	}
	err := c.rest(ctx, op, c.orgPath("memberships/"+url.PathEscape(username)), &raw)
	var failure *provider.Error
	switch {
	case errors.As(err, &failure) && failure.Class == provider.ClassNotFound:
		return invalidRequest(username + " is not a member of the organization; this tool changes only existing " +
			"members and never invites")
	case err != nil:
		return actionsFailure(err, organizationMembersReadPermission)
	case raw.State != "active":
		return invalidRequest(username + " is not an active member of the organization, its invitation is " +
			"still pending; this tool changes only existing members and never invites")
	}
	return nil
}

func (c *Client) updateOrganization(ctx context.Context, a *organizationChangeArguments) (*OrganizationDetails, error) {
	const op = "update organization"
	var raw organizationDetailsRaw
	if err := c.restChange(ctx, op, http.MethodPatch, "/orgs/"+url.PathEscape(c.target.owner), a.settings(),
		&raw); err != nil {
		return nil, variableFailure(err, organizationChangePermission, "", organizationUpdateRejected)
	}
	return raw.project(op)
}

func (c *Client) setOrganizationRole(ctx context.Context, a *organizationChangeArguments) (*OrganizationMembership, error) {
	const op = "set organization member role"
	if err := c.requireActiveOrganizationMember(ctx, a.Username); err != nil {
		return nil, err
	}
	var raw organizationMembershipRaw
	if err := c.restChange(ctx, op, http.MethodPut, c.orgPath("memberships/"+url.PathEscape(a.Username)),
		map[string]any{"role": a.Role}, &raw); err != nil {
		return nil, variableFailure(err, organizationMembersChangePermission, "", organizationRoleRejected)
	}
	return raw.project(a.Username), nil
}

func (c *Client) removeOrganizationMember(ctx context.Context, a *organizationChangeArguments) (*OrganizationMemberRemoved, error) {
	const op = "remove organization member"
	if err := c.restChange(ctx, op, http.MethodDelete, c.orgPath("members/"+url.PathEscape(a.Username)), nil,
		nil); err != nil {
		return nil, variableFailure(err, organizationMembersChangePermission, "", organizationRemoveReject)
	}
	return &OrganizationMemberRemoved{Removed: true, Username: a.Username}, nil
}

func (c *Client) assignOrganizationRole(ctx context.Context, a *organizationChangeArguments) (*OrganizationRoleAssigned, error) {
	const op = "assign organization role"
	if err := c.requireActiveOrganizationMember(ctx, a.Username); err != nil {
		return nil, err
	}
	path := c.orgPath("organization-roles/users/" + url.PathEscape(a.Username) + "/" +
		strconv.FormatInt(a.RoleID, 10))
	if err := c.restChange(ctx, op, http.MethodPut, path, map[string]any{}, nil); err != nil {
		return nil, variableFailure(err, organizationRoleAssignPermission, "", organizationRoleAssignRejected)
	}
	return &OrganizationRoleAssigned{Assigned: true, Username: a.Username, RoleID: a.RoleID}, nil
}

func (c *Client) removeOutsideCollaborator(ctx context.Context, a *organizationChangeArguments) (*OutsideCollaboratorRemoved, error) {
	const op = "remove outside collaborator"
	if err := c.restChange(ctx, op, http.MethodDelete, c.orgPath("outside_collaborators/"+url.PathEscape(a.Username)),
		nil, nil); err != nil {
		return nil, variableFailure(err, organizationMembersChangePermission, "", outsideCollaboratorRejected)
	}
	return &OutsideCollaboratorRemoved{Removed: true, Username: a.Username}, nil
}

func (c *Client) cancelOrganizationInvitation(ctx context.Context, a *organizationChangeArguments) (*OrganizationInvitationCancelled, error) {
	const op = "cancel organization invitation"
	if err := c.restChange(ctx, op, http.MethodDelete,
		c.orgPath("invitations/"+strconv.FormatInt(a.InvitationID, 10)), nil, nil); err != nil {
		return nil, variableFailure(err, organizationMembersChangePermission, "", organizationInvitationRejected)
	}
	return &OrganizationInvitationCancelled{Cancelled: true, InvitationID: a.InvitationID}, nil
}
