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

// github.organization-invitations.create invites an existing GitHub account to an organization, and
// github.team-members.invite does so through a team. Both take the login only: it must be one the connection's
// targets name as users/LOGIN, see selectInvitee, and the account is read before anything is written. An
// account that is a member already, or whose invitation is open, is refused without a change; the maintenance
// tools handle existing members. They are offered only where a connection's tools list names them.

const (
	maxInvitedTeams  = 20
	maxTeamListPages = 10
)

const (
	organizationInvitePermission = "GitHub refused this invitation; inviting to an organization needs admin:org " +
		"on a classic token, or Members: read and write of the organization on a fine-grained token, and " +
		"organization owner rights"
	teamInvitePermission = "GitHub refused this team invitation; it needs admin:org on a classic token, or " +
		"Members: read and write of the organization on a fine-grained token"
	invitationRejected = "GitHub refused this invitation; the role or a team may not be allowed, or the account " +
		"may not be invited to this organization"
)

var (
	invitationRoles         = []string{"admin", "direct_member"}
	invitationTeamIDsSchema = `{"type":"array","minItems":1,"maxItems":20,"uniqueItems":true,"items":` +
		organizationIDSchema + `}`
)

var invitationUsernameArgument = capability.Argument{Name: "username", Description: "Login of the existing " +
	"GitHub account to invite; the connection's targets must name it as users/LOGIN; no e-mail address or " +
	"numeric ID", Required: true}

var organizationInvitationsCreate = organizationChangeTool("organization-invitations.create",
	"Invite a GitHub account to an organization",
	"Invite one existing GitHub account to an organization an explicit connection allows, as a direct member or "+
		"an owner, optionally into teams of that organization. Only a login that the connection's targets name "+
		"as users/LOGIN can be invited, never an e-mail address or a numeric ID; an account that is a member "+
		"already or has an open invitation is refused. The role admin makes the account an organization owner "+
		"and is security relevant. Offered only by a connection whose tools list names it",
	[]string{"create", "invite", "members"}, changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	inputSchema(`"username":`+slugSchema+`,"role":{"type":"string","enum":["admin","direct_member"]},`+
		`"team_ids":`+invitationTeamIDsSchema, "username"),
	organizationChangeOutput(`"result":{"type":"string","enum":["invited"]},"username":{"type":"string"},`+
		`"role":{"type":"string"}`, "result", "username", "role"),
	[]capability.Argument{invitationUsernameArgument,
		{Name: "role", Description: "admin or direct_member; direct_member when omitted; admin makes the account " +
			"an organization owner and is a rights elevation"},
		{Name: "team_ids", Description: "Numeric ids of up to 20 teams of this organization the account joins on " +
			"accepting, as github.teams.list reports them; an id outside the organization is refused"}},
	[]capability.Field{{Name: "result", Description: "invited once GitHub created the invitation"},
		{Name: "username", Description: "Login of the account, as GitHub spells it"},
		{Name: "role", Description: "Role that was sent"}},
	capability.Example{Description: "Invite an account as a member", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","username":"octocat"}`)})

var teamMembersInvite = teamTool("team-members.invite", "Invite a GitHub account to an organization through a team",
	"Invite one existing GitHub account, which is not a member of the organization yet, to a team of an "+
		"organization an explicit connection allows; GitHub then invites it to the organization too. Only a login "+
		"that the connection's targets name as users/LOGIN can be invited, never an e-mail address or a numeric "+
		"ID; an account that is a member already or has an open invitation is refused. Offered only by a "+
		"connection whose tools list names it",
	[]string{"create", "invite", "members"}, changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent), true,
	inputSchema(`"team":`+slugSchema+`,"username":`+slugSchema+`,"role":`+teamRoleSchema, "team", "username"),
	json.RawMessage(`{"type":"object","properties":{"result":{"type":"string","enum":["invited","added"]},`+
		`"team":{"type":"string"},"username":{"type":"string"},"role":{"type":"string"}},`+
		`"required":["result","team","username","role"],"additionalProperties":false}`),
	[]capability.Argument{teamSlugArgument, invitationUsernameArgument,
		{Name: "role", Description: "member or maintainer; member when omitted"}},
	[]capability.Field{{Name: "result", Description: "invited once GitHub created the invitation"},
		{Name: "team", Description: "Slug of the team the call addressed"},
		{Name: "username", Description: "Login of the account, as GitHub spells it"},
		{Name: "role", Description: "Team role that was sent"}},
	capability.Example{Description: "Invite an account through a team", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","team":"design","username":"octocat"}`)})

// invitationToolIDs are the invitation tools that name an organization as their owner target.
var invitationToolIDs = []string{organizationInvitationsCreate.ID, teamMembersInvite.ID}

// invitationArguments holds the arguments of the two tools; the input schema of each admits only its own.
type invitationArguments struct {
	Team     string  `json:"team"`
	Username string  `json:"username"`
	Role     string  `json:"role"`
	TeamIDs  []int64 `json:"team_ids"`

	login string
}

// OrganizationInvited is the answer to an organization invitation. It never carries an invitation ID or link.
type OrganizationInvited struct {
	Result   string `json:"result"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// TeamMemberInvited is the answer to a team invitation. It never carries an invitation ID or link.
type TeamMemberInvited struct {
	Result   string `json:"result"`
	Team     string `json:"team"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func checkOrganizationInvite(a *invitationArguments) error {
	a.Role = strings.ToLower(a.Role)
	if a.Role == "" {
		a.Role = "direct_member"
	}
	if !slices.Contains(invitationRoles, a.Role) {
		return invalidRequest("role must be admin or direct_member")
	}
	if len(a.TeamIDs) > maxInvitedTeams {
		return invalidRequest("team_ids must name at most " + strconv.Itoa(maxInvitedTeams) + " teams")
	}
	seen := map[int64]bool{}
	for _, id := range a.TeamIDs {
		if id < 1 || id > maxOrganizationNumericID || seen[id] {
			return invalidRequest("team_ids must be distinct positive team ids")
		}
		seen[id] = true
	}
	return nil
}

func checkTeamInvite(a *invitationArguments) error {
	if err := checkTeam("team", a.Team); err != nil {
		return err
	}
	a.Role = strings.ToLower(a.Role)
	if a.Role == "" {
		a.Role = "member"
	}
	if !slices.Contains(teamMembershipRoles, a.Role) {
		return invalidRequest("role must be member or maintainer")
	}
	return nil
}

// invitationHandler checks the organization, the login, and the other arguments before a credential is
// resolved, so a refused request never reaches a secret or GitHub.
func invitationHandler(id string, check func(*invitationArguments) error,
	call func(context.Context, *Client, *invitationArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments invitationArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if arguments.login, err = selectInvitee(resolved, arguments.Username); err != nil {
			return nil, err
		}
		if err := check(&arguments); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

func invitationOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: organizationInvitationsCreate, Handler: invitationHandler(organizationInvitationsCreate.ID,
			checkOrganizationInvite, func(ctx context.Context, c *Client, a *invitationArguments) (any, error) {
				return c.inviteToOrganization(ctx, a)
			})},
		{Descriptor: teamMembersInvite, Handler: invitationHandler(teamMembersInvite.ID, checkTeamInvite,
			func(ctx context.Context, c *Client, a *invitationArguments) (any, error) {
				return c.inviteThroughTeam(ctx, a)
			})},
	}
}

// requireNoMembership reads the organization membership of the account and refuses, without a change, one
// that exists: an active member belongs to the maintenance tools, and a pending state is an open invitation.
// GitHub answers 404 for an account that is neither.
func (c *Client) requireNoMembership(ctx context.Context, login, maintenance string) error {
	const op = "read organization membership"
	var raw struct {
		State string `json:"state"`
	}
	err := c.rest(ctx, op, c.orgPath("memberships/"+url.PathEscape(login)), &raw)
	switch {
	case err == nil && raw.State == "pending":
		return invalidRequest("the account has an open invitation to this organization already; nothing was sent")
	case err == nil:
		return invalidRequest("the account is a member of this organization already; change its membership " +
			"with " + maintenance + "; nothing was sent")
	case isNotFound(err):
		return nil
	}
	return actionsFailure(err, organizationMembersReadPermission)
}

// requireOrganizationTeams reads the teams of the organization and refuses, without a change, an id that is
// not among them, or one it could not find within the pages it reads.
func (c *Client) requireOrganizationTeams(ctx context.Context, ids []int64) error {
	const op = "list organization teams"
	missing := map[int64]bool{}
	for _, id := range ids {
		missing[id] = true
	}
	for page := 1; page <= maxTeamListPages && len(missing) > 0; page++ {
		var teams []struct {
			ID int64 `json:"id"`
		}
		query := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}}
		hasNext, err := c.restPage(ctx, op, c.orgPath("teams"), query, &teams)
		if err != nil {
			return actionsFailure(err, organizationTeamsReadPermission)
		}
		for _, team := range teams {
			delete(missing, team.ID)
		}
		if !hasNext {
			break
		}
	}
	if len(missing) > 0 {
		return invalidRequest("team_ids must name teams of this organization, as github.teams.list reports " +
			"them; at least one was not found; nothing was sent")
	}
	return nil
}

func (c *Client) inviteToOrganization(ctx context.Context, a *invitationArguments) (*OrganizationInvited, error) {
	const op = "invite to organization"
	account, err := c.readInvitee(ctx, op, a.login)
	if err != nil {
		return nil, err
	}
	if err := c.requireNoMembership(ctx, account.Login, "github.organization-members.set-role"); err != nil {
		return nil, err
	}
	if len(a.TeamIDs) > 0 {
		if err := c.requireOrganizationTeams(ctx, a.TeamIDs); err != nil {
			return nil, err
		}
	}
	body := map[string]any{"invitee_id": account.ID, "role": a.Role}
	if len(a.TeamIDs) > 0 {
		body["team_ids"] = a.TeamIDs
	}
	status, err := c.restChangeStatus(ctx, op, http.MethodPost, c.orgPath("invitations"), body, nil)
	if err != nil {
		return nil, invitationFailure(err, organizationInvitePermission)
	}
	if status != http.StatusCreated {
		return nil, invalidResponse(op, true)
	}
	return &OrganizationInvited{Result: "invited", Username: account.Login, Role: a.Role}, nil
}

func (c *Client) inviteThroughTeam(ctx context.Context, a *invitationArguments) (*TeamMemberInvited, error) {
	const op = "invite through team"
	account, err := c.readInvitee(ctx, op, a.login)
	if err != nil {
		return nil, err
	}
	team := &teamArguments{Team: a.Team, Username: account.Login}
	if err := c.rest(ctx, op, c.teamPath(team, ""), nil); err != nil {
		if isNotFound(err) {
			return nil, invalidRequest("the team is not a team of this organization, or the token cannot see " +
				"it; nothing was sent")
		}
		return nil, actionsFailure(err, teamReadPermission)
	}
	if err := c.requireNoMembership(ctx, account.Login, "github.team-members.add"); err != nil {
		return nil, err
	}
	var raw struct {
		State string `json:"state"`
	}
	if err := c.restChange(ctx, op, http.MethodPut, team.membershipPath(c), map[string]string{"role": a.Role},
		&raw); err != nil {
		return nil, invitationFailure(err, teamInvitePermission)
	}
	result := "invited"
	if raw.State == "active" {
		result = "added"
	}
	return &TeamMemberInvited{Result: result, Team: a.Team, Username: account.Login, Role: a.Role}, nil
}

// invitationFailure maps a refused write without GitHub's text: 403 names the needed rights, 422 the likely
// causes.
func invitationFailure(err error, permission string) error {
	err = actionsFailure(err, permission)
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Original != nil &&
		failure.Original.Status == http.StatusUnprocessableEntity {
		return invalidRequest(invitationRejected)
	}
	return err
}
