package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The team management tools read one team, its membership of one account, and its repositories, and change a
// team, its members, and its repository permissions. Every tool names an organization as an owner target. The
// tools that change something are offered only where a connection's tools list names them. GitHub invites a
// user who is not yet a member of the organization when that user is added to a team, so team-members.add
// reads the organization membership first and refuses without a change unless it is active.

const (
	maxTeamNameLength        = 255
	maxTeamDescriptionLength = 1024
)

const (
	teamNameSchema = `{"type":"string","minLength":1,"maxLength":255}`
	teamTextSchema = `{"type":"string","maxLength":1024}`

	teamPrivacySchema      = `{"type":"string","enum":["secret","closed"]}`
	teamNotificationSchema = `{"type":"string","enum":["notifications_enabled","notifications_disabled"]}`
	teamParentSchema       = `{"type":"integer","minimum":1,"maximum":9007199254740991}`
	teamParentNullSchema   = `{"anyOf":[` + teamParentSchema + `,{"type":"null"}]}`
	teamRoleSchema         = `{"type":"string","enum":["member","maintainer"]}`
	teamPermissionSchema   = `{"type":"string","enum":["pull","triage","push","maintain","admin"]}`
)

var (
	teamPrivacyValues      = []string{"secret", "closed"}
	teamNotificationValues = []string{"notifications_enabled", "notifications_disabled"}
	teamMembershipRoles    = []string{"member", "maintainer"}
	teamPermissionValues   = []string{"pull", "triage", "push", "maintain", "admin"}
)

// Permission messages. GitHub decides on every request; a message names what the request needs without
// claiming what the configured token holds.
const (
	teamReadPermission = "GitHub refused this token this team data; reading it needs read:org on a classic " +
		"token, or Members: read of the organization on a fine-grained token"
	teamChangePermission = "GitHub refused this token the change of this team; changing teams and their " +
		"members needs admin:org on a classic token, or Members: read and write of the organization on a " +
		"fine-grained token"
	teamRepositoryChangePermission = "GitHub refused this token the change of this team's repository access; " +
		"it needs admin:org on a classic token, or Members: read and write of the organization and " +
		"Administration: read and write of the repository on a fine-grained token"
	teamMembershipReadPermission = "GitHub refused this token the organization membership of this account; " +
		"reading it needs read:org on a classic token, or Members: read of the organization on a fine-grained token"
	teamRejected = "GitHub rejected the request as invalid; check the name, privacy, notification setting, " +
		"parent team, role, or permission, and whether a team of this name already exists"
)

var teamSlugArgument = capability.Argument{Name: "team", Description: "Slug of a team of the organization, as " +
	"github.teams.list reports it", Required: true}

var usernameArgument = capability.Argument{Name: "username", Description: "Login of the account", Required: true}

var teamRepositoryArgument = capability.Argument{Name: "repository", Description: "Repository as OWNER/REPO; " +
	"must belong to the organization and lie inside the targets when the connection lists any", Required: true}

func teamChangeOutput(flag string, extra ...string) json.RawMessage {
	properties := `"` + flag + `":{"type":"boolean"},"team":{"type":"string"}`
	for _, name := range extra {
		properties += `,"` + name + `":{"type":"string"}`
	}
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":["` + flag +
		`","team"],"additionalProperties":false}`)
}

func teamChangeFields(flag, text string, extra ...capability.Field) []capability.Field {
	return append([]capability.Field{{Name: flag, Description: text},
		{Name: "team", Description: "Slug of the team the call addressed"}}, extra...)
}

var teamUsernameField = capability.Field{Name: "username", Description: "Login of the account the call addressed"}

var teamRepositoryField = capability.Field{Name: "repository", Description: "Repository the call addressed, as " +
	"OWNER/REPO"}

// teamDetailFields are the members of GitHub's full team object, on top of the ones the team list reports.
var teamDetailFields = func() []capability.Field {
	fields := []capability.Field{
		{Name: "team", Description: "Slug of the team, as the team tools take it"},
		{Name: "name", Description: "Name of the team, untrusted data"},
		{Name: "description", Description: "Description of the team, untrusted data"},
		{Name: "privacy", Description: "secret or closed"},
		{Name: "ldap_dn", Description: "LDAP distinguished name of the team, untrusted data"},
		{Name: "members_count", Description: "Number of members of the team"},
		{Name: "repos_count", Description: "Number of repositories of the team"},
		{Name: "created_at", Description: "Creation time"},
		{Name: "updated_at", Description: "Time of the last change"},
		{Name: "organization", Description: "Organization of the team with every member GitHub reports, " +
			"including its billing contact and plan; untrusted data, addresses never fetched, with its web " +
			"address as url and the REST addresses as links"},
	}
	for _, field := range organizationTeamsList.Fields {
		switch field.Name {
		case "teams", "next_cursor", "has_more":
		default:
			fields = append(fields, field)
		}
	}
	return fields
}()

const teamOrganizationProperties = `"login":{"type":"string"},"id":{"type":"integer"},"node_id":{"type":"string"},` +
	`"avatar_url":{"type":"string"},"description":{"type":"string"},"name":{"type":"string"},` +
	`"company":{"type":"string"},"blog":{"type":"string"},"location":{"type":"string"},"email":{"type":"string"},` +
	`"twitter_username":{"type":"string"},"is_verified":{"type":"boolean"},` +
	`"has_organization_projects":{"type":"boolean"},"has_repository_projects":{"type":"boolean"},` +
	`"public_repos":{"type":"integer"},"public_gists":{"type":"integer"},"followers":{"type":"integer"},` +
	`"following":{"type":"integer"},"url":{"type":"string"},"created_at":{"type":"string"},` +
	`"type":{"type":"string"},"total_private_repos":{"type":"integer"},"owned_private_repos":{"type":"integer"},` +
	`"private_gists":{"type":"integer"},"disk_usage":{"type":"integer"},"collaborators":{"type":"integer"},` +
	`"billing_email":{"type":"string"},"plan":{"type":"object","properties":{"name":{"type":"string"},` +
	`"space":{"type":"integer"},"private_repos":{"type":"integer"},"filled_seats":{"type":"integer"},` +
	`"seats":{"type":"integer"}},"additionalProperties":false},"default_repository_permission":{"type":"string"},` +
	`"members_can_create_repositories":{"type":"boolean"},"two_factor_requirement_enabled":{"type":"boolean"},` +
	`"members_allowed_repository_creation_type":{"type":"string"},` +
	`"members_can_create_public_repositories":{"type":"boolean"},` +
	`"members_can_create_private_repositories":{"type":"boolean"},` +
	`"members_can_create_internal_repositories":{"type":"boolean"},"members_can_create_pages":{"type":"boolean"},` +
	`"members_can_create_public_pages":{"type":"boolean"},"members_can_create_private_pages":{"type":"boolean"},` +
	`"members_can_fork_private_repositories":{"type":"boolean"},"web_commit_signoff_required":{"type":"boolean"},` +
	`"updated_at":{"type":"string"},"archived_at":{"type":"string"},"links":{"type":"object","properties":{` +
	`"url":{"type":"string"},"repos_url":{"type":"string"},"events_url":{"type":"string"},` +
	`"hooks_url":{"type":"string"},"issues_url":{"type":"string"},"members_url":{"type":"string"},` +
	`"public_members_url":{"type":"string"}},"additionalProperties":false}`

var teamDetailProperties = organizationTeamProperties + `,"ldap_dn":{"type":"string"},` +
	`"members_count":{"type":"integer"},"repos_count":{"type":"integer"},"created_at":{"type":"string"},` +
	`"updated_at":{"type":"string"},"organization":{"type":"object","properties":{` + teamOrganizationProperties +
	`},"additionalProperties":false}`

var teamDetailOutput = json.RawMessage(`{"type":"object","properties":{` + teamDetailProperties + `},` +
	organizationTeamRequired + `}`)

const teamMembershipProperties = `"team":{"type":"string"},"username":{"type":"string"},` +
	`"role":{"type":"string"},"state":{"type":"string"},"url":{"type":"string"}`

var teamMembershipOutput = json.RawMessage(`{"type":"object","properties":{` + teamMembershipProperties +
	`},"required":["team","username"],"additionalProperties":false}`)

var teamMembershipFields = []capability.Field{
	{Name: "team", Description: "Slug of the team the call addressed"},
	{Name: "username", Description: "Login of the account the call addressed"},
	{Name: "role", Description: "member or maintainer, untrusted data"},
	{Name: "state", Description: "active, or pending for an invitation GitHub has not seen accepted; untrusted data"},
	{Name: "url", Description: "REST address of the membership, untrusted data, never fetched"},
}

var teamRepositoriesOutput = json.RawMessage(`{"type":"object","properties":{"team":{"type":"string"},` +
	`"repositories":{"type":"array","items":` + restRepositorySchema + `},"next_cursor":{"type":"string"},` +
	`"has_more":{"type":"boolean"}},"required":["team","repositories","has_more"],"additionalProperties":false}`)

func teamTool(id, title, description string, tags []string, risk capability.Risk, listed bool, input, output json.RawMessage,
	arguments []capability.Argument, fields []capability.Field, example capability.Example) capability.Descriptor {
	return capability.Descriptor{ID: Provider + "." + id, Version: 1, Title: title, Description: description,
		Tags: append([]string{"github", "teams", "organization"}, tags...), Risk: risk, Provider: Provider,
		RequiresToolAllowList: listed, InputSchema: input, OutputSchema: output, Arguments: arguments,
		Fields: fields, Examples: []capability.Example{example}}
}

var teamsGet = teamTool("teams.get", "Get a GitHub team",
	"Read one team of an organization an explicit connection allows by its slug, with every member GitHub reports",
	[]string{"get"}, readRisk, false, inputSchema(`"team":`+slugSchema, "team"), teamDetailOutput,
	[]capability.Argument{teamSlugArgument}, teamDetailFields,
	capability.Example{Description: "Read a team", Arguments: json.RawMessage(`{"owner":"orgs/octo-org","team":"design"}`)})

var teamChangeArguments = []capability.Argument{
	{Name: "description", Description: "Description of the team, at most 1024 characters, untrusted text"},
	{Name: "privacy", Description: "secret (visible to owners and members of the team) or closed (visible to all " +
		"members of the organization); a parent or child team must be closed"},
	{Name: "notification_setting", Description: "notifications_enabled or notifications_disabled"},
}

const teamChangeKeys = `"description":` + teamTextSchema + `,"privacy":` + teamPrivacySchema +
	`,"notification_setting":` + teamNotificationSchema

var teamsCreate = teamTool("teams.create", "Create a GitHub team",
	"Create one team in an organization an explicit connection allows, without members or repositories; a "+
		"repeated call is refused by GitHub while the name exists; offered only where a connection's tools list names it",
	[]string{"create"}, changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent), true,
	inputSchema(`"name":`+teamNameSchema+`,`+teamChangeKeys+`,"parent_team_id":`+teamParentSchema, "name"),
	teamDetailOutput,
	append([]capability.Argument{{Name: "name", Description: "Name of the team, 1 to 255 characters", Required: true}},
		append(append([]capability.Argument{}, teamChangeArguments...),
			capability.Argument{Name: "parent_team_id", Description: "Numeric id of an existing team of the " +
				"organization that becomes the parent; both teams must then be closed"})...),
	teamDetailFields,
	capability.Example{Description: "Create a closed team", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","name":"Design","description":"Design team","privacy":"closed"}`)})

var teamsUpdate = teamTool("teams.update", "Update a GitHub team",
	"Change the name, description, privacy, notification setting, or parent of one team of an organization an "+
		"explicit connection allows; idempotent, since sending the same values again leaves GitHub in the same "+
		"state; offered only where a connection's tools list names it",
	[]string{"update"}, changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent), true,
	inputSchema(`"team":`+slugSchema+`,"name":`+teamNameSchema+`,`+teamChangeKeys+`,"parent_team_id":`+
		teamParentNullSchema, "team"),
	teamDetailOutput,
	append(append([]capability.Argument{teamSlugArgument,
		{Name: "name", Description: "New name of the team, 1 to 255 characters"}}, teamChangeArguments...),
		capability.Argument{Name: "parent_team_id", Description: "Numeric id of the new parent team, or null to " +
			"make the team a top-level team"}),
	teamDetailFields,
	capability.Example{Description: "Rename a team", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","team":"design","name":"Product design"}`)})

var teamsDelete = teamTool("teams.delete", "Delete a GitHub team",
	"Delete one team of an organization an explicit connection allows, permanently, with the access it granted; "+
		"offered only where a connection's tools list names it",
	[]string{"delete"}, changeRisk(capability.EffectDelete, capability.IdempotencyUnknown), true,
	inputSchema(`"team":`+slugSchema, "team"), teamChangeOutput("deleted"),
	[]capability.Argument{teamSlugArgument},
	teamChangeFields("deleted", "True once GitHub deleted the team"),
	capability.Example{Description: "Delete a team", Arguments: json.RawMessage(`{"owner":"orgs/octo-org","team":"design"}`)})

var teamMembersAdd = teamTool("team-members.add", "Add a member to a GitHub team",
	"Add an active member of the organization to a team, or change the role of a team member; an account that is "+
		"not an active member of the organization (missing, or invitation pending) is refused before any change, "+
		"so no invitation is ever created; the role maintainer raises the account's rights over the team and "+
		"is security relevant; idempotent; offered only where a connection's tools list names it",
	[]string{"update", "members"}, changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent), true,
	inputSchema(`"team":`+slugSchema+`,"username":`+slugSchema+`,"role":`+teamRoleSchema, "team", "username"),
	teamMembershipOutput,
	[]capability.Argument{teamSlugArgument, usernameArgument,
		{Name: "role", Description: "member or maintainer; member when omitted. maintainer lets the account manage " +
			"the team and is a rights elevation"}},
	teamMembershipFields,
	capability.Example{Description: "Add a member", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","team":"design","username":"octocat"}`)})

var teamMembersRemove = teamTool("team-members.remove", "Remove a member from a GitHub team",
	"Remove an account from a team of an organization an explicit connection allows; the account stays in the "+
		"organization; offered only where a connection's tools list names it",
	[]string{"delete", "members"}, changeRisk(capability.EffectDelete, capability.IdempotencyUnknown), true,
	inputSchema(`"team":`+slugSchema+`,"username":`+slugSchema, "team", "username"),
	teamChangeOutput("removed", "username"),
	[]capability.Argument{teamSlugArgument, usernameArgument},
	teamChangeFields("removed", "True once GitHub removed the account from the team", teamUsernameField),
	capability.Example{Description: "Remove a member", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","team":"design","username":"octocat"}`)})

var teamMembershipsGet = teamTool("team-memberships.get", "Get the team membership of a GitHub account",
	"Read the role and state of one account in one team of an organization an explicit connection allows",
	[]string{"get", "members"}, readRisk, false,
	inputSchema(`"team":`+slugSchema+`,"username":`+slugSchema, "team", "username"), teamMembershipOutput,
	[]capability.Argument{teamSlugArgument, usernameArgument}, teamMembershipFields,
	capability.Example{Description: "Read a membership", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","team":"design","username":"octocat"}`)})

var teamRepositoriesList = teamTool("team-repositories.list", "List the repositories of a GitHub team",
	"List one bounded batch of the repositories a team of an organization an explicit connection allows has "+
		"access to, with the team's permission on each; a repository outside the connection's targets keeps only "+
		"its identifiers",
	[]string{"repositories", "list"}, readRisk, false,
	inputSchema(`"team":`+slugSchema+`,`+pagingKeys, "team"), teamRepositoriesOutput,
	append([]capability.Argument{teamSlugArgument}, pagingArguments...),
	append([]capability.Field{{Name: "team", Description: "Slug of the team the call addressed"},
		{Name: "repositories", Description: "Repositories with the members of GitHub's repository by their " +
			"GitHub names, including permissions and role_name of the team on each, and the owner of each as " +
			"an account; untrusted data, addresses never fetched; a repository outside the connection's " +
			"targets keeps only id, node_id, and full_name"}}, pagingFields...),
	capability.Example{Description: "List the repositories of a team", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","team":"design"}`)})

var teamRepositoriesSet = teamTool("team-repositories.set", "Set the permission of a GitHub team on a repository",
	"Give a team of an organization an explicit connection allows a permission on one repository of that "+
		"organization inside the connection's targets, replacing the team's current one; the permission admin "+
		"raises the team's rights over the repository and is security relevant; idempotent; offered only where a "+
		"connection's tools list names it",
	[]string{"update", "repositories"}, changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent), true,
	inputSchema(`"team":`+slugSchema+`,"repository":`+repoSchema+`,"permission":`+teamPermissionSchema,
		"team", "repository", "permission"),
	teamChangeOutput("set", "repository", "permission"),
	[]capability.Argument{teamSlugArgument, teamRepositoryArgument,
		{Name: "permission", Description: "pull, triage, push, maintain, or admin; admin is a rights elevation. " +
			"A custom repository role name is not accepted", Required: true}},
	teamChangeFields("set", "True once GitHub accepted the permission", teamRepositoryField,
		capability.Field{Name: "permission", Description: "Permission sent"}),
	capability.Example{Description: "Give a team write access", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","team":"design","repository":"octo-org/example","permission":"push"}`)})

var teamRepositoriesRemove = teamTool("team-repositories.remove", "Remove a GitHub team from a repository",
	"Withdraw the access of a team of an organization an explicit connection allows from one repository of that "+
		"organization inside the connection's targets; offered only where a connection's tools list names it",
	[]string{"delete", "repositories"}, changeRisk(capability.EffectDelete, capability.IdempotencyUnknown), true,
	inputSchema(`"team":`+slugSchema+`,"repository":`+repoSchema, "team", "repository"),
	teamChangeOutput("removed", "repository"),
	[]capability.Argument{teamSlugArgument, teamRepositoryArgument},
	teamChangeFields("removed", "True once GitHub withdrew the team's access", teamRepositoryField),
	capability.Example{Description: "Withdraw a team's access", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","team":"design","repository":"octo-org/example"}`)})

// teamToolIDs are the team management tools that name an organization as their owner target.
var teamToolIDs = []string{teamsGet.ID, teamsCreate.ID, teamsUpdate.ID, teamsDelete.ID, teamMembersAdd.ID,
	teamMembersRemove.ID, teamMembershipsGet.ID, teamRepositoriesList.ID, teamRepositoriesSet.ID,
	teamRepositoriesRemove.ID}

// teamArguments holds the arguments of the team management tools; the input schema of each admits only its
// own. The pointers tell a member left out from one sent. repository, allowed, page, perPage, and binding are
// derived by the handler and the checks.
type teamArguments struct {
	Team                string          `json:"team"`
	Username            string          `json:"username"`
	Name                *string         `json:"name"`
	Description         *string         `json:"description"`
	Privacy             *string         `json:"privacy"`
	NotificationSetting *string         `json:"notification_setting"`
	ParentTeamID        json.RawMessage `json:"parent_team_id"`
	Role                string          `json:"role"`
	Permission          string          `json:"permission"`
	Limit               int             `json:"limit"`
	Cursor              string          `json:"cursor"`

	repository    target
	allowed       allowlist
	page, perPage int
	binding       []byte
}

func checkTeamText(argument, value string, limit int, required bool) error {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return invalidRequest(argument + " must be one line of text without control characters")
	}
	if count := utf8.RuneCountInString(value); count > limit || (required && count < 1) {
		return invalidRequest(fmt.Sprintf("%s must have at most %d characters", argument, limit))
	}
	return nil
}

func checkUsername(username string) error {
	if !validLogin(username) {
		return invalidRequest("username must be a login of letters, digits, hyphens, and underscores")
	}
	return nil
}

func checkTeamEnum(argument, value string, values []string) error {
	if !containsFold(values, value) {
		return invalidRequest(argument + " must be " + strings.Join(values, ", "))
	}
	return nil
}

// checkTeamFields checks the members create and update share.
func (a *teamArguments) checkTeamFields() error {
	if a.Name != nil {
		if err := checkTeamText("name", *a.Name, maxTeamNameLength, true); err != nil {
			return err
		}
	}
	if a.Description != nil {
		if err := checkTeamText("description", *a.Description, maxTeamDescriptionLength, false); err != nil {
			return err
		}
	}
	if a.Privacy != nil {
		if err := checkTeamEnum("privacy", *a.Privacy, teamPrivacyValues); err != nil {
			return err
		}
	}
	if a.NotificationSetting != nil {
		if err := checkTeamEnum("notification_setting", *a.NotificationSetting, teamNotificationValues); err != nil {
			return err
		}
	}
	return nil
}

// parentValue is the parent_team_id to send: nil when it was left out, otherwise a positive id, or the JSON
// null that makes a team top-level.
func (a *teamArguments) parentValue(allowNull bool) (value any, present bool, err error) {
	if len(a.ParentTeamID) == 0 {
		return nil, false, nil
	}
	if string(a.ParentTeamID) == "null" {
		if !allowNull {
			return nil, false, invalidRequest("parent_team_id must be a positive team id")
		}
		return nil, true, nil
	}
	var id int64
	if json.Unmarshal(a.ParentTeamID, &id) != nil || id < 1 || id > 9007199254740991 {
		return nil, false, invalidRequest("parent_team_id must be a positive team id")
	}
	return id, true, nil
}

func checkTeamOnly(a *teamArguments, _ target) error { return checkTeam("team", a.Team) }

func checkTeamMember(a *teamArguments, _ target) error {
	if err := checkTeam("team", a.Team); err != nil {
		return err
	}
	return checkUsername(a.Username)
}

func checkTeamCreate(a *teamArguments, _ target) error {
	if a.Name == nil {
		return invalidRequest("name is required")
	}
	if err := a.checkTeamFields(); err != nil {
		return err
	}
	_, _, err := a.parentValue(false)
	return err
}

func checkTeamUpdate(a *teamArguments, _ target) error {
	if err := checkTeam("team", a.Team); err != nil {
		return err
	}
	if err := a.checkTeamFields(); err != nil {
		return err
	}
	_, hasParent, err := a.parentValue(true)
	if err != nil {
		return err
	}
	if a.Name == nil && a.Description == nil && a.Privacy == nil && a.NotificationSetting == nil && !hasParent {
		return invalidRequest("give at least one of name, description, privacy, notification_setting, or parent_team_id")
	}
	return nil
}

func checkTeamMemberAdd(a *teamArguments, bound target) error {
	if err := checkTeamMember(a, bound); err != nil {
		return err
	}
	if a.Role != "" {
		if err := checkTeamEnum("role", a.Role, teamMembershipRoles); err != nil {
			return err
		}
		a.Role = strings.ToLower(a.Role)
	}
	return nil
}

func checkTeamRepositoriesList(a *teamArguments, bound target) error {
	if err := checkTeam("team", a.Team); err != nil {
		return err
	}
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint("teamrepositories", strings.ToLower(bound.String()), strings.ToLower(a.Team))
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkTeamRepositorySet(a *teamArguments, _ target) error {
	if err := checkTeam("team", a.Team); err != nil {
		return err
	}
	if err := checkTeamEnum("permission", a.Permission, teamPermissionValues); err != nil {
		return err
	}
	a.Permission = strings.ToLower(a.Permission)
	return nil
}

// teamHandler decodes and checks the arguments, the organization, and, for the tools that name one, the
// repository before a credential is resolved, so a refused request never becomes a secret read or a provider
// call. A repository must belong to the organization the team belongs to.
func teamHandler(id string, repository bool, check func(*teamArguments, target) error,
	call func(context.Context, *Client, *teamArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments teamArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if repository {
			if arguments.repository, err = selectTarget(resolved, kindRepository, raw); err != nil {
				return nil, err
			}
			if !strings.EqualFold(arguments.repository.owner, bound.owner) {
				return nil, invalidRequest("repository must belong to the organization " + bound.argument())
			}
		}
		if arguments.allowed, err = allowlistOf(resolved); err != nil {
			return nil, providerError("open", err.Error())
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

func teamManagementOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: teamsGet, Handler: teamHandler(teamsGet.ID, false, checkTeamOnly,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) { return c.getTeam(ctx, a) })},
		{Descriptor: teamsCreate, Handler: teamHandler(teamsCreate.ID, false, checkTeamCreate,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) { return c.createTeam(ctx, a) })},
		{Descriptor: teamsUpdate, Handler: teamHandler(teamsUpdate.ID, false, checkTeamUpdate,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) { return c.updateTeam(ctx, a) })},
		{Descriptor: teamsDelete, Handler: teamHandler(teamsDelete.ID, false, checkTeamOnly,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) { return c.deleteTeam(ctx, a) })},
		{Descriptor: teamMembersAdd, Handler: teamHandler(teamMembersAdd.ID, false, checkTeamMemberAdd,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) { return c.addTeamMember(ctx, a) })},
		{Descriptor: teamMembersRemove, Handler: teamHandler(teamMembersRemove.ID, false, checkTeamMember,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) { return c.removeTeamMember(ctx, a) })},
		{Descriptor: teamMembershipsGet, Handler: teamHandler(teamMembershipsGet.ID, false, checkTeamMember,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) {
				return c.getTeamMembership(ctx, a)
			})},
		{Descriptor: teamRepositoriesList, Handler: teamHandler(teamRepositoriesList.ID, false, checkTeamRepositoriesList,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) {
				return c.listTeamRepositories(ctx, a)
			})},
		{Descriptor: teamRepositoriesSet, Handler: teamHandler(teamRepositoriesSet.ID, true, checkTeamRepositorySet,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) {
				return c.setTeamRepository(ctx, a)
			})},
		{Descriptor: teamRepositoriesRemove, Handler: teamHandler(teamRepositoriesRemove.ID, true, checkTeamOnly,
			func(ctx context.Context, c *Client, a *teamArguments) (any, error) {
				return c.removeTeamRepository(ctx, a)
			})},
	}
}

// ---- Results

// TeamOrganizationPlan is the plan of the organization of a team.
type TeamOrganizationPlan struct {
	Name         *string `json:"name,omitempty"`
	Space        *int64  `json:"space,omitempty"`
	PrivateRepos *int64  `json:"private_repos,omitempty"`
	FilledSeats  *int64  `json:"filled_seats,omitempty"`
	Seats        *int64  `json:"seats,omitempty"`
}

// TeamOrganization is the organization object inside a full team.
type TeamOrganization struct {
	Login                               *string               `json:"login,omitempty"`
	ID                                  *int64                `json:"id,omitempty"`
	NodeID                              *string               `json:"node_id,omitempty"`
	AvatarURL                           *string               `json:"avatar_url,omitempty"`
	Description                         *string               `json:"description,omitempty"`
	Name                                *string               `json:"name,omitempty"`
	Company                             *string               `json:"company,omitempty"`
	Blog                                *string               `json:"blog,omitempty"`
	Location                            *string               `json:"location,omitempty"`
	Email                               *string               `json:"email,omitempty"`
	TwitterUsername                     *string               `json:"twitter_username,omitempty"`
	IsVerified                          *bool                 `json:"is_verified,omitempty"`
	HasOrganizationProjects             *bool                 `json:"has_organization_projects,omitempty"`
	HasRepositoryProjects               *bool                 `json:"has_repository_projects,omitempty"`
	PublicRepos                         *int64                `json:"public_repos,omitempty"`
	PublicGists                         *int64                `json:"public_gists,omitempty"`
	Followers                           *int64                `json:"followers,omitempty"`
	Following                           *int64                `json:"following,omitempty"`
	URL                                 *string               `json:"url,omitempty"`
	CreatedAt                           *string               `json:"created_at,omitempty"`
	Type                                *string               `json:"type,omitempty"`
	TotalPrivateRepos                   *int64                `json:"total_private_repos,omitempty"`
	OwnedPrivateRepos                   *int64                `json:"owned_private_repos,omitempty"`
	PrivateGists                        *int64                `json:"private_gists,omitempty"`
	DiskUsage                           *int64                `json:"disk_usage,omitempty"`
	Collaborators                       *int64                `json:"collaborators,omitempty"`
	BillingEmail                        *string               `json:"billing_email,omitempty"`
	Plan                                *TeamOrganizationPlan `json:"plan,omitempty"`
	DefaultRepositoryPermission         *string               `json:"default_repository_permission,omitempty"`
	MembersCanCreateRepositories        *bool                 `json:"members_can_create_repositories,omitempty"`
	TwoFactorRequirementEnabled         *bool                 `json:"two_factor_requirement_enabled,omitempty"`
	MembersAllowedRepositoryCreation    *string               `json:"members_allowed_repository_creation_type,omitempty"`
	MembersCanCreatePublicRepositories  *bool                 `json:"members_can_create_public_repositories,omitempty"`
	MembersCanCreatePrivateRepositories *bool                 `json:"members_can_create_private_repositories,omitempty"`
	MembersCanCreateInternalRepos       *bool                 `json:"members_can_create_internal_repositories,omitempty"`
	MembersCanCreatePages               *bool                 `json:"members_can_create_pages,omitempty"`
	MembersCanCreatePublicPages         *bool                 `json:"members_can_create_public_pages,omitempty"`
	MembersCanCreatePrivatePages        *bool                 `json:"members_can_create_private_pages,omitempty"`
	MembersCanForkPrivateRepositories   *bool                 `json:"members_can_fork_private_repositories,omitempty"`
	WebCommitSignoffRequired            *bool                 `json:"web_commit_signoff_required,omitempty"`
	UpdatedAt                           *string               `json:"updated_at,omitempty"`
	ArchivedAt                          *string               `json:"archived_at,omitempty"`
	Links                               Links                 `json:"links,omitempty"`
}

type teamOrganizationRaw struct {
	Login                               *string `json:"login"`
	ID                                  *int64  `json:"id"`
	NodeID                              *string `json:"node_id"`
	AvatarURL                           *string `json:"avatar_url"`
	Description                         *string `json:"description"`
	Name                                *string `json:"name"`
	Company                             *string `json:"company"`
	Blog                                *string `json:"blog"`
	Location                            *string `json:"location"`
	Email                               *string `json:"email"`
	TwitterUsername                     *string `json:"twitter_username"`
	IsVerified                          *bool   `json:"is_verified"`
	HasOrganizationProjects             *bool   `json:"has_organization_projects"`
	HasRepositoryProjects               *bool   `json:"has_repository_projects"`
	PublicRepos                         *int64  `json:"public_repos"`
	PublicGists                         *int64  `json:"public_gists"`
	Followers                           *int64  `json:"followers"`
	Following                           *int64  `json:"following"`
	HTMLURL                             *string `json:"html_url"`
	CreatedAt                           *string `json:"created_at"`
	Type                                *string `json:"type"`
	TotalPrivateRepos                   *int64  `json:"total_private_repos"`
	OwnedPrivateRepos                   *int64  `json:"owned_private_repos"`
	PrivateGists                        *int64  `json:"private_gists"`
	DiskUsage                           *int64  `json:"disk_usage"`
	Collaborators                       *int64  `json:"collaborators"`
	BillingEmail                        *string `json:"billing_email"`
	DefaultRepositoryPermission         *string `json:"default_repository_permission"`
	MembersCanCreateRepositories        *bool   `json:"members_can_create_repositories"`
	TwoFactorRequirementEnabled         *bool   `json:"two_factor_requirement_enabled"`
	MembersAllowedRepositoryCreation    *string `json:"members_allowed_repository_creation_type"`
	MembersCanCreatePublicRepositories  *bool   `json:"members_can_create_public_repositories"`
	MembersCanCreatePrivateRepositories *bool   `json:"members_can_create_private_repositories"`
	MembersCanCreateInternalRepos       *bool   `json:"members_can_create_internal_repositories"`
	MembersCanCreatePages               *bool   `json:"members_can_create_pages"`
	MembersCanCreatePublicPages         *bool   `json:"members_can_create_public_pages"`
	MembersCanCreatePrivatePages        *bool   `json:"members_can_create_private_pages"`
	MembersCanForkPrivateRepositories   *bool   `json:"members_can_fork_private_repositories"`
	WebCommitSignoffRequired            *bool   `json:"web_commit_signoff_required"`
	UpdatedAt                           *string `json:"updated_at"`
	ArchivedAt                          *string `json:"archived_at"`
	Plan                                *struct {
		Name         *string `json:"name"`
		Space        *int64  `json:"space"`
		PrivateRepos *int64  `json:"private_repos"`
		FilledSeats  *int64  `json:"filled_seats"`
		Seats        *int64  `json:"seats"`
	} `json:"plan"`
	links Links
}

var teamOrganizationLinkKeys = []string{"url", "repos_url", "events_url", "hooks_url", "issues_url", "members_url",
	"public_members_url"}

func (r *teamOrganizationRaw) UnmarshalJSON(data []byte) (err error) {
	type plain teamOrganizationRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, teamOrganizationLinkKeys)
	return err
}

func (r *teamOrganizationRaw) project() *TeamOrganization {
	if r == nil {
		return nil
	}
	out := &TeamOrganization{Login: shortText(r.Login), ID: r.ID, NodeID: shortText(r.NodeID),
		AvatarURL: address(r.AvatarURL), Description: bulkText(r.Description), Name: shortText(r.Name),
		Company: shortText(r.Company), Blog: bulkText(r.Blog), Location: shortText(r.Location),
		Email: shortText(r.Email), TwitterUsername: shortText(r.TwitterUsername), IsVerified: r.IsVerified,
		HasOrganizationProjects: r.HasOrganizationProjects, HasRepositoryProjects: r.HasRepositoryProjects,
		PublicRepos: r.PublicRepos, PublicGists: r.PublicGists, Followers: r.Followers, Following: r.Following,
		URL: address(r.HTMLURL), CreatedAt: shortText(r.CreatedAt), Type: shortText(r.Type),
		TotalPrivateRepos: r.TotalPrivateRepos, OwnedPrivateRepos: r.OwnedPrivateRepos, PrivateGists: r.PrivateGists,
		DiskUsage: r.DiskUsage, Collaborators: r.Collaborators, BillingEmail: shortText(r.BillingEmail),
		DefaultRepositoryPermission:         shortText(r.DefaultRepositoryPermission),
		MembersCanCreateRepositories:        r.MembersCanCreateRepositories,
		TwoFactorRequirementEnabled:         r.TwoFactorRequirementEnabled,
		MembersAllowedRepositoryCreation:    shortText(r.MembersAllowedRepositoryCreation),
		MembersCanCreatePublicRepositories:  r.MembersCanCreatePublicRepositories,
		MembersCanCreatePrivateRepositories: r.MembersCanCreatePrivateRepositories,
		MembersCanCreateInternalRepos:       r.MembersCanCreateInternalRepos,
		MembersCanCreatePages:               r.MembersCanCreatePages,
		MembersCanCreatePublicPages:         r.MembersCanCreatePublicPages,
		MembersCanCreatePrivatePages:        r.MembersCanCreatePrivatePages,
		MembersCanForkPrivateRepositories:   r.MembersCanForkPrivateRepositories,
		WebCommitSignoffRequired:            r.WebCommitSignoffRequired, UpdatedAt: shortText(r.UpdatedAt),
		ArchivedAt: shortText(r.ArchivedAt), Links: r.links}
	if p := r.Plan; p != nil {
		out.Plan = &TeamOrganizationPlan{Name: shortText(p.Name), Space: p.Space, PrivateRepos: p.PrivateRepos,
			FilledSeats: p.FilledSeats, Seats: p.Seats}
	}
	return out
}

// TeamDetails is GitHub's full team object: the members the team list reports and the ones only a single
// team carries.
type TeamDetails struct {
	OrganizationTeam
	LdapDN       *string           `json:"ldap_dn,omitempty"`
	MembersCount *int64            `json:"members_count,omitempty"`
	ReposCount   *int64            `json:"repos_count,omitempty"`
	CreatedAt    *string           `json:"created_at,omitempty"`
	UpdatedAt    *string           `json:"updated_at,omitempty"`
	Organization *TeamOrganization `json:"organization,omitempty"`
}

type teamFullRaw struct {
	team  teamRaw
	extra struct {
		MembersCount *int64               `json:"members_count"`
		ReposCount   *int64               `json:"repos_count"`
		CreatedAt    *string              `json:"created_at"`
		UpdatedAt    *string              `json:"updated_at"`
		Organization *teamOrganizationRaw `json:"organization"`
	}
}

func (r *teamFullRaw) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &r.team); err != nil {
		return err
	}
	return json.Unmarshal(data, &r.extra)
}

func (r *teamFullRaw) project(op string) (*TeamDetails, error) {
	team := &r.team
	if team.Slug == "" || team.Name == "" {
		return nil, invalidEntry(op, "a team")
	}
	return &TeamDetails{OrganizationTeam: OrganizationTeam{Team: team.Slug, Name: team.Name,
		Description: team.Description, Privacy: team.Privacy, ID: team.core.ID, NodeID: shortText(team.core.NodeID),
		Slug: shortText(team.core.Slug), NotificationSetting: shortText(team.core.NotificationSetting),
		Permission: shortText(team.core.Permission), Permissions: team.permissions(), Type: shortText(team.core.Type),
		AccessSource: shortText(team.core.AccessSource), OrganizationID: team.core.OrganizationID,
		EnterpriseID: team.core.EnterpriseID, Parent: team.Parent.simple(), URL: address(team.core.HTMLURL),
		Links: team.core.links},
		LdapDN: shortText(team.core.LdapDN), MembersCount: r.extra.MembersCount, ReposCount: r.extra.ReposCount,
		CreatedAt: shortText(r.extra.CreatedAt), UpdatedAt: shortText(r.extra.UpdatedAt),
		Organization: r.extra.Organization.project()}, nil
}

// TeamDeleted reports a deleted team.
type TeamDeleted struct {
	Deleted bool   `json:"deleted"`
	Team    string `json:"team"`
}

// TeamMemberRemoved reports an account removed from a team.
type TeamMemberRemoved struct {
	Removed  bool   `json:"removed"`
	Team     string `json:"team"`
	Username string `json:"username"`
}

// TeamRepositorySet reports the permission a team now holds on a repository.
type TeamRepositorySet struct {
	Set        bool   `json:"set"`
	Team       string `json:"team"`
	Repository string `json:"repository"`
	Permission string `json:"permission"`
}

// TeamRepositoryRemoved reports the access withdrawn from a team on a repository.
type TeamRepositoryRemoved struct {
	Removed    bool   `json:"removed"`
	Team       string `json:"team"`
	Repository string `json:"repository"`
}

// TeamMembership is the role and state of one account in one team.
type TeamMembership struct {
	Team     string  `json:"team"`
	Username string  `json:"username"`
	Role     *string `json:"role,omitempty"`
	State    *string `json:"state,omitempty"`
	URL      *string `json:"url,omitempty"`
}

type teamMembershipRaw struct {
	Role  *string `json:"role"`
	State *string `json:"state"`
	URL   *string `json:"url"`
}

// TeamRepositoryList is one batch of the repositories of a team.
type TeamRepositoryList struct {
	Team         string           `json:"team"`
	Repositories []RESTRepository `json:"repositories"`
	NextCursor   string           `json:"next_cursor,omitempty"`
	HasMore      bool             `json:"has_more"`
}

// ---- Requests

func (c *Client) teamPath(a *teamArguments, rest string) string {
	path := "teams/" + url.PathEscape(a.Team)
	if rest != "" {
		path += "/" + rest
	}
	return c.orgPath(path)
}

func (a *teamArguments) membershipPath(c *Client) string {
	return c.teamPath(a, "memberships/"+url.PathEscape(a.Username))
}

func (a *teamArguments) repositoryPath(c *Client) string {
	return c.teamPath(a, "repos/"+url.PathEscape(a.repository.owner)+"/"+url.PathEscape(a.repository.repo))
}

func (c *Client) readTeam(ctx context.Context, op, method, path string, body any, permission string, change bool,
	conflict string) (*TeamDetails, error) {
	var raw teamFullRaw
	var err error
	if change {
		err = c.restChange(ctx, op, method, path, body, &raw)
	} else {
		err = c.rest(ctx, op, path, &raw)
	}
	if err != nil {
		return nil, variableFailure(err, permission, conflict, teamRejected)
	}
	return raw.project(op)
}

func (c *Client) getTeam(ctx context.Context, a *teamArguments) (*TeamDetails, error) {
	return c.readTeam(ctx, "get team", http.MethodGet, c.teamPath(a, ""), nil, teamReadPermission, false, "")
}

func (c *Client) createTeam(ctx context.Context, a *teamArguments) (*TeamDetails, error) {
	body := map[string]any{"name": *a.Name}
	a.teamBody(body)
	if parent, present, _ := a.parentValue(false); present {
		body["parent_team_id"] = parent
	}
	return c.readTeam(ctx, "create team", http.MethodPost, c.orgPath("teams"), body, teamChangePermission, true, "")
}

func (c *Client) updateTeam(ctx context.Context, a *teamArguments) (*TeamDetails, error) {
	body := map[string]any{}
	if a.Name != nil {
		body["name"] = *a.Name
	}
	a.teamBody(body)
	if parent, present, _ := a.parentValue(true); present {
		body["parent_team_id"] = parent
	}
	return c.readTeam(ctx, "update team", http.MethodPatch, c.teamPath(a, ""), body, teamChangePermission, true, "")
}

func (a *teamArguments) teamBody(body map[string]any) {
	if a.Description != nil {
		body["description"] = *a.Description
	}
	if a.Privacy != nil {
		body["privacy"] = strings.ToLower(*a.Privacy)
	}
	if a.NotificationSetting != nil {
		body["notification_setting"] = strings.ToLower(*a.NotificationSetting)
	}
}

func (c *Client) deleteTeam(ctx context.Context, a *teamArguments) (*TeamDeleted, error) {
	const op = "delete team"
	if err := c.restChange(ctx, op, http.MethodDelete, c.teamPath(a, ""), nil, nil); err != nil {
		return nil, variableFailure(err, teamChangePermission, "", teamRejected)
	}
	return &TeamDeleted{Deleted: true, Team: a.Team}, nil
}

func (c *Client) getTeamMembership(ctx context.Context, a *teamArguments) (*TeamMembership, error) {
	const op = "get team membership"
	var raw teamMembershipRaw
	if err := c.rest(ctx, op, a.membershipPath(c), &raw); err != nil {
		return nil, actionsFailure(err, teamReadPermission)
	}
	return a.membership(raw), nil
}

func (a *teamArguments) membership(raw teamMembershipRaw) *TeamMembership {
	return &TeamMembership{Team: a.Team, Username: a.Username, Role: shortText(raw.Role),
		State: shortText(raw.State), URL: address(raw.URL)}
}

// requireActiveMember reads the organization membership of the account and refuses, without any change, an
// account that is not an active member: adding such an account to a team makes GitHub invite it.
func (c *Client) requireActiveMember(ctx context.Context, a *teamArguments) error {
	const op = "read organization membership"
	var raw struct {
		State string `json:"state"`
	}
	err := c.rest(ctx, op, c.orgPath("memberships/"+url.PathEscape(a.Username)), &raw)
	var failure *provider.Error
	switch {
	case errors.As(err, &failure) && failure.Class == provider.ClassNotFound:
		return invalidRequest(a.Username + " is not a member of the organization; adding it to a team would " +
			"invite it, which this tool never does")
	case err != nil:
		return actionsFailure(err, teamMembershipReadPermission)
	case raw.State != "active":
		return invalidRequest(a.Username + " is not an active member of the organization, its invitation is " +
			"still pending; adding it to a team would invite it, which this tool never does")
	}
	return nil
}

func (c *Client) addTeamMember(ctx context.Context, a *teamArguments) (*TeamMembership, error) {
	const op = "add team member"
	if err := c.requireActiveMember(ctx, a); err != nil {
		return nil, err
	}
	body := map[string]any{}
	if a.Role != "" {
		body["role"] = a.Role
	}
	var raw teamMembershipRaw
	if err := c.restChange(ctx, op, http.MethodPut, a.membershipPath(c), body, &raw); err != nil {
		return nil, variableFailure(err, teamChangePermission, "", teamRejected)
	}
	return a.membership(raw), nil
}

func (c *Client) removeTeamMember(ctx context.Context, a *teamArguments) (*TeamMemberRemoved, error) {
	const op = "remove team member"
	if err := c.restChange(ctx, op, http.MethodDelete, a.membershipPath(c), nil, nil); err != nil {
		return nil, variableFailure(err, teamChangePermission, "", teamRejected)
	}
	return &TeamMemberRemoved{Removed: true, Team: a.Team, Username: a.Username}, nil
}

func (c *Client) listTeamRepositories(ctx context.Context, a *teamArguments) (*TeamRepositoryList, error) {
	const op = "list team repositories"
	var raw []repositoryRaw
	hasNext, err := c.restPage(ctx, op, c.teamPath(a, "repos"), pageQuery(a.page, a.perPage), &raw)
	if err != nil {
		return nil, actionsFailure(err, teamReadPermission)
	}
	result := &TeamRepositoryList{Team: a.Team, Repositories: make([]RESTRepository, 0, len(raw))}
	for i := range raw {
		repository := raw[i].project(a.allowed)
		if repository.FullName == nil {
			return nil, invalidEntry(op, "a repository")
		}
		result.Repositories = append(result.Repositories, *repository)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func pageQuery(page, perPage int) url.Values {
	return url.Values{"per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(page)}}
}

func (c *Client) setTeamRepository(ctx context.Context, a *teamArguments) (*TeamRepositorySet, error) {
	const op = "set team repository permission"
	body := map[string]any{"permission": a.Permission}
	if err := c.restChange(ctx, op, http.MethodPut, a.repositoryPath(c), body, nil); err != nil {
		return nil, variableFailure(err, teamRepositoryChangePermission, "", teamRejected)
	}
	return &TeamRepositorySet{Set: true, Team: a.Team, Repository: a.repository.argumentValue(),
		Permission: a.Permission}, nil
}

func (c *Client) removeTeamRepository(ctx context.Context, a *teamArguments) (*TeamRepositoryRemoved, error) {
	const op = "remove team repository"
	if err := c.restChange(ctx, op, http.MethodDelete, a.repositoryPath(c), nil, nil); err != nil {
		return nil, variableFailure(err, teamRepositoryChangePermission, "", teamRejected)
	}
	return &TeamRepositoryRemoved{Removed: true, Team: a.Team, Repository: a.repository.argumentValue()}, nil
}

func (t target) argumentValue() string { return t.owner + "/" + t.repo }
