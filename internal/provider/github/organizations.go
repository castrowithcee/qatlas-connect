package github

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// The organization read tools read one organization, its members, one membership, its organization roles,
// its outside collaborators, and its pending invitations. Every tool names an organization as an owner
// target. The member and outside collaborator lists never send GitHub's filter for accounts without
// two-factor authentication, and no output carries the two-factor state of another account; the only such
// member is two_factor_requirement_enabled, a setting of the organization itself. An invitation is read from
// the members GitHub documents for it, so neither a token nor a link that accepts it can reach the output.

// ---- Organization

const organizationExtraProperties = `"default_repository_branch":{"type":"string"},` +
	`"members_can_delete_repositories":{"type":"boolean"},"members_can_change_repo_visibility":{"type":"boolean"},` +
	`"members_can_invite_outside_collaborators":{"type":"boolean"},"members_can_delete_issues":{"type":"boolean"},` +
	`"display_commenter_full_name_setting_enabled":{"type":"boolean"},"readers_can_create_discussions":{"type":"boolean"},` +
	`"members_can_create_teams":{"type":"boolean"},"members_can_view_dependency_insights":{"type":"boolean"},` +
	`"advanced_security_enabled_for_new_repositories":{"type":"boolean"},` +
	`"dependabot_alerts_enabled_for_new_repositories":{"type":"boolean"},` +
	`"dependabot_security_updates_enabled_for_new_repositories":{"type":"boolean"},` +
	`"dependency_graph_enabled_for_new_repositories":{"type":"boolean"},` +
	`"secret_scanning_enabled_for_new_repositories":{"type":"boolean"},` +
	`"secret_scanning_push_protection_enabled_for_new_repositories":{"type":"boolean"},` +
	`"secret_scanning_push_protection_custom_link_enabled":{"type":"boolean"},` +
	`"secret_scanning_push_protection_custom_link":{"type":"string"},` +
	`"deploy_keys_enabled_for_repositories":{"type":"boolean"}`

var organizationDetailsOutput = json.RawMessage(`{"type":"object","properties":{` + teamOrganizationProperties + `,` +
	organizationExtraProperties + `},"required":["login"],"additionalProperties":false}`)

// organizationDetailsFields names every member of GitHub's full organization object.
var organizationDetailsFields = func() []capability.Field {
	pairs := [][2]string{
		{"login", "Login of the organization"},
		{"id", "Numeric organization id"},
		{"node_id", "GraphQL node id, untrusted data"},
		{"avatar_url", "Avatar address, untrusted data, never fetched"},
		{"description", "Profile description, untrusted data"},
		{"name", "Display name, untrusted data"},
		{"company", "Company, untrusted data"},
		{"blog", "Website as the profile states it, untrusted data, never fetched"},
		{"location", "Location, untrusted data"},
		{"email", "Public contact email address, untrusted data"},
		{"twitter_username", "Twitter or X username, untrusted data"},
		{"is_verified", "Whether the organization is verified"},
		{"has_organization_projects", "Whether the organization uses projects"},
		{"has_repository_projects", "Whether repositories of the organization use projects"},
		{"public_repos", "Number of public repositories"},
		{"public_gists", "Number of public gists"},
		{"followers", "Number of followers"},
		{"following", "Number of accounts followed"},
		{"url", "Web address of the organization, untrusted data, never fetched"},
		{"created_at", "Creation time"},
		{"type", "Account type, untrusted data"},
		{"total_private_repos", "Number of private repositories"},
		{"owned_private_repos", "Number of private repositories the organization owns"},
		{"private_gists", "Number of private gists"},
		{"disk_usage", "Disk usage as GitHub counts it"},
		{"collaborators", "Number of collaborators on private repositories"},
		{"billing_email", "Billing contact as GitHub reports it, untrusted data"},
		{"plan", "Billing plan: name, space, private_repos, filled_seats, and seats; untrusted data"},
		{"default_repository_permission", "Base permission members get on repositories, untrusted data"},
		{"members_can_create_repositories", "Whether members may create repositories"},
		{"two_factor_requirement_enabled", "Whether the organization requires two-factor authentication of its " +
			"members; a setting of the organization, not the state of any account"},
		{"members_allowed_repository_creation_type", "Kind of repository members may create, untrusted data"},
		{"members_can_create_public_repositories", "Whether members may create public repositories"},
		{"members_can_create_private_repositories", "Whether members may create private repositories"},
		{"members_can_create_internal_repositories", "Whether members may create internal repositories"},
		{"members_can_create_pages", "Whether members may create Pages sites"},
		{"members_can_create_public_pages", "Whether members may create public Pages sites"},
		{"members_can_create_private_pages", "Whether members may create private Pages sites"},
		{"members_can_fork_private_repositories", "Whether members may fork private repositories"},
		{"web_commit_signoff_required", "Whether web commits need a sign-off"},
		{"updated_at", "Time of the last change"},
		{"archived_at", "Time the organization was archived"},
		{"links", "REST url, repos_url, events_url, hooks_url, issues_url, members_url, and public_members_url " +
			"by their GitHub names, untrusted data, never fetched"},
		{"default_repository_branch", "Default branch name of new repositories, untrusted data"},
		{"members_can_delete_repositories", "Whether members may delete or transfer repositories"},
		{"members_can_change_repo_visibility", "Whether members may change the visibility of repositories"},
		{"members_can_invite_outside_collaborators", "Whether members may invite outside collaborators"},
		{"members_can_delete_issues", "Whether members may delete issues"},
		{"display_commenter_full_name_setting_enabled", "Whether the full name of a commenter is shown"},
		{"readers_can_create_discussions", "Whether readers may create discussions"},
		{"members_can_create_teams", "Whether members may create teams"},
		{"members_can_view_dependency_insights", "Whether members may view dependency insights"},
		{"advanced_security_enabled_for_new_repositories", "Whether Advanced Security is enabled for new repositories"},
		{"dependabot_alerts_enabled_for_new_repositories", "Whether Dependabot alerts are enabled for new repositories"},
		{"dependabot_security_updates_enabled_for_new_repositories", "Whether Dependabot security updates are " +
			"enabled for new repositories"},
		{"dependency_graph_enabled_for_new_repositories", "Whether the dependency graph is enabled for new repositories"},
		{"secret_scanning_enabled_for_new_repositories", "Whether secret scanning is enabled for new repositories"},
		{"secret_scanning_push_protection_enabled_for_new_repositories", "Whether push protection is enabled for " +
			"new repositories"},
		{"secret_scanning_push_protection_custom_link_enabled", "Whether push protection shows a custom link"},
		{"secret_scanning_push_protection_custom_link", "Custom link of push protection, untrusted data, never fetched"},
		{"deploy_keys_enabled_for_repositories", "Whether repositories may use deploy keys"},
	}
	fields := make([]capability.Field, len(pairs))
	for i, pair := range pairs {
		fields[i] = capability.Field{Name: pair[0], Description: pair[1]}
	}
	return fields
}()

// OrganizationDetails is GitHub's full organization object, with every member it documents.
type OrganizationDetails struct {
	TeamOrganization
	DefaultRepositoryBranch                     *string `json:"default_repository_branch,omitempty"`
	MembersCanDeleteRepositories                *bool   `json:"members_can_delete_repositories,omitempty"`
	MembersCanChangeRepoVisibility              *bool   `json:"members_can_change_repo_visibility,omitempty"`
	MembersCanInviteOutsideCollaborators        *bool   `json:"members_can_invite_outside_collaborators,omitempty"`
	MembersCanDeleteIssues                      *bool   `json:"members_can_delete_issues,omitempty"`
	DisplayCommenterFullNameSettingEnabled      *bool   `json:"display_commenter_full_name_setting_enabled,omitempty"`
	ReadersCanCreateDiscussions                 *bool   `json:"readers_can_create_discussions,omitempty"`
	MembersCanCreateTeams                       *bool   `json:"members_can_create_teams,omitempty"`
	MembersCanViewDependencyInsights            *bool   `json:"members_can_view_dependency_insights,omitempty"`
	AdvancedSecurityEnabledForNewRepos          *bool   `json:"advanced_security_enabled_for_new_repositories,omitempty"`
	DependabotAlertsEnabledForNewRepos          *bool   `json:"dependabot_alerts_enabled_for_new_repositories,omitempty"`
	DependabotSecurityUpdatesEnabledForNewRepos *bool   `json:"dependabot_security_updates_enabled_for_new_repositories,omitempty"`
	DependencyGraphEnabledForNewRepos           *bool   `json:"dependency_graph_enabled_for_new_repositories,omitempty"`
	SecretScanningEnabledForNewRepos            *bool   `json:"secret_scanning_enabled_for_new_repositories,omitempty"`
	SecretScanningPushProtectionEnabledForNew   *bool   `json:"secret_scanning_push_protection_enabled_for_new_repositories,omitempty"`
	SecretScanningPushProtectionCustomLinkOn    *bool   `json:"secret_scanning_push_protection_custom_link_enabled,omitempty"`
	SecretScanningPushProtectionCustomLink      *string `json:"secret_scanning_push_protection_custom_link,omitempty"`
	DeployKeysEnabledForRepositories            *bool   `json:"deploy_keys_enabled_for_repositories,omitempty"`
}

type organizationDetailsRaw struct {
	base  teamOrganizationRaw
	extra struct {
		DefaultRepositoryBranch                     *string `json:"default_repository_branch"`
		MembersCanDeleteRepositories                *bool   `json:"members_can_delete_repositories"`
		MembersCanChangeRepoVisibility              *bool   `json:"members_can_change_repo_visibility"`
		MembersCanInviteOutsideCollaborators        *bool   `json:"members_can_invite_outside_collaborators"`
		MembersCanDeleteIssues                      *bool   `json:"members_can_delete_issues"`
		DisplayCommenterFullNameSettingEnabled      *bool   `json:"display_commenter_full_name_setting_enabled"`
		ReadersCanCreateDiscussions                 *bool   `json:"readers_can_create_discussions"`
		MembersCanCreateTeams                       *bool   `json:"members_can_create_teams"`
		MembersCanViewDependencyInsights            *bool   `json:"members_can_view_dependency_insights"`
		AdvancedSecurityEnabledForNewRepos          *bool   `json:"advanced_security_enabled_for_new_repositories"`
		DependabotAlertsEnabledForNewRepos          *bool   `json:"dependabot_alerts_enabled_for_new_repositories"`
		DependabotSecurityUpdatesEnabledForNewRepos *bool   `json:"dependabot_security_updates_enabled_for_new_repositories"`
		DependencyGraphEnabledForNewRepos           *bool   `json:"dependency_graph_enabled_for_new_repositories"`
		SecretScanningEnabledForNewRepos            *bool   `json:"secret_scanning_enabled_for_new_repositories"`
		SecretScanningPushProtectionEnabledForNew   *bool   `json:"secret_scanning_push_protection_enabled_for_new_repositories"`
		SecretScanningPushProtectionCustomLinkOn    *bool   `json:"secret_scanning_push_protection_custom_link_enabled"`
		SecretScanningPushProtectionCustomLink      *string `json:"secret_scanning_push_protection_custom_link"`
		DeployKeysEnabledForRepositories            *bool   `json:"deploy_keys_enabled_for_repositories"`
	}
}

func (r *organizationDetailsRaw) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &r.base); err != nil {
		return err
	}
	return json.Unmarshal(data, &r.extra)
}

func (r *organizationDetailsRaw) project(op string) (*OrganizationDetails, error) {
	if r.base.Login == nil || *r.base.Login == "" {
		return nil, invalidEntry(op, "the organization")
	}
	e := &r.extra
	return &OrganizationDetails{TeamOrganization: *r.base.project(),
		DefaultRepositoryBranch:                     shortText(e.DefaultRepositoryBranch),
		MembersCanDeleteRepositories:                e.MembersCanDeleteRepositories,
		MembersCanChangeRepoVisibility:              e.MembersCanChangeRepoVisibility,
		MembersCanInviteOutsideCollaborators:        e.MembersCanInviteOutsideCollaborators,
		MembersCanDeleteIssues:                      e.MembersCanDeleteIssues,
		DisplayCommenterFullNameSettingEnabled:      e.DisplayCommenterFullNameSettingEnabled,
		ReadersCanCreateDiscussions:                 e.ReadersCanCreateDiscussions,
		MembersCanCreateTeams:                       e.MembersCanCreateTeams,
		MembersCanViewDependencyInsights:            e.MembersCanViewDependencyInsights,
		AdvancedSecurityEnabledForNewRepos:          e.AdvancedSecurityEnabledForNewRepos,
		DependabotAlertsEnabledForNewRepos:          e.DependabotAlertsEnabledForNewRepos,
		DependabotSecurityUpdatesEnabledForNewRepos: e.DependabotSecurityUpdatesEnabledForNewRepos,
		DependencyGraphEnabledForNewRepos:           e.DependencyGraphEnabledForNewRepos,
		SecretScanningEnabledForNewRepos:            e.SecretScanningEnabledForNewRepos,
		SecretScanningPushProtectionEnabledForNew:   e.SecretScanningPushProtectionEnabledForNew,
		SecretScanningPushProtectionCustomLinkOn:    e.SecretScanningPushProtectionCustomLinkOn,
		SecretScanningPushProtectionCustomLink:      address(e.SecretScanningPushProtectionCustomLink),
		DeployKeysEnabledForRepositories:            e.DeployKeysEnabledForRepositories}, nil
}

// ---- Accounts, memberships, roles, invitations

// OrganizationAccount is a person or bot GitHub reports in an organization read, by login.
type OrganizationAccount struct {
	Login string `json:"login"`
	AccountPerson
	Type      *string `json:"type,omitempty"`
	StarredAt *string `json:"starred_at,omitempty"`
	URL       *string `json:"url,omitempty"`
}

// organizationAccount projects an account; ok is false while GitHub names no login.
func (r *accountRaw) organizationAccount() (OrganizationAccount, bool) {
	if r == nil || r.Login == nil || *r.Login == "" {
		return OrganizationAccount{}, false
	}
	return OrganizationAccount{Login: *nameText(r.Login), AccountPerson: r.person(), Type: shortText(r.Type),
		StarredAt: shortText(r.StarredAt), URL: address(r.HTMLURL)}, true
}

// optionalAccount projects an account GitHub documents as nullable; one without a login stays out.
func optionalAccount(r *accountRaw) *OrganizationAccount {
	if account, ok := r.organizationAccount(); ok {
		return &account
	}
	return nil
}

var organizationAccountProperties = `"login":{"type":"string"},` + accountPersonProperties + `,"type":{"type":"string"},` +
	`"starred_at":{"type":"string"},"url":{"type":"string"}`

var organizationAccountSchema = `{"type":"object","properties":{` + organizationAccountProperties +
	`},"required":["login"],"additionalProperties":false}`

var organizationAccountFields = append(append([]capability.Field(nil), accountPersonFields...),
	capability.Field{Name: "type", Description: "Account type, untrusted data"},
	capability.Field{Name: "starred_at", Description: "Star time GitHub reports for the account, untrusted data"},
	capability.Field{Name: "url", Description: "Web address of the account, untrusted data, never fetched"})

// OrganizationMemberList is one batch of the members of an organization.
type OrganizationMemberList struct {
	Members    []OrganizationAccount `json:"members"`
	NextCursor string                `json:"next_cursor,omitempty"`
	HasMore    bool                  `json:"has_more"`
}

// OutsideCollaboratorList is one batch of the outside collaborators of an organization.
type OutsideCollaboratorList struct {
	Collaborators []OrganizationAccount `json:"collaborators"`
	NextCursor    string                `json:"next_cursor,omitempty"`
	HasMore       bool                  `json:"has_more"`
}

// OrganizationSimple is GitHub's short organization object inside a membership.
type OrganizationSimple struct {
	Login       *string `json:"login,omitempty"`
	ID          *int64  `json:"id,omitempty"`
	NodeID      *string `json:"node_id,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
	Description *string `json:"description,omitempty"`
	Links       Links   `json:"links,omitempty"`
}

type organizationSimpleRaw struct {
	Login       *string `json:"login"`
	ID          *int64  `json:"id"`
	NodeID      *string `json:"node_id"`
	AvatarURL   *string `json:"avatar_url"`
	Description *string `json:"description"`
	links       Links
}

func (r *organizationSimpleRaw) UnmarshalJSON(data []byte) (err error) {
	type plain organizationSimpleRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, teamOrganizationLinkKeys)
	return err
}

func (r *organizationSimpleRaw) project() *OrganizationSimple {
	if r == nil {
		return nil
	}
	return &OrganizationSimple{Login: shortText(r.Login), ID: r.ID, NodeID: shortText(r.NodeID),
		AvatarURL: address(r.AvatarURL), Description: bulkText(r.Description), Links: r.links}
}

// OrganizationMembership is the state and role of one account in an organization.
type OrganizationMembership struct {
	Username                                   string                         `json:"username"`
	URL                                        *string                        `json:"url,omitempty"`
	State                                      *string                        `json:"state,omitempty"`
	Role                                       *string                        `json:"role,omitempty"`
	DirectMembership                           *bool                          `json:"direct_membership,omitempty"`
	EnterpriseTeamsProvidingIndirectMembership []string                       `json:"enterprise_teams_providing_indirect_membership,omitempty"`
	OrganizationURL                            *string                        `json:"organization_url,omitempty"`
	Organization                               *OrganizationSimple            `json:"organization,omitempty"`
	User                                       *OrganizationAccount           `json:"user,omitempty"`
	Permissions                                *OrganizationMemberPermissions `json:"permissions,omitempty"`
}

// OrganizationMemberPermissions are the permissions GitHub lists for a membership.
type OrganizationMemberPermissions struct {
	CanCreateRepository *bool `json:"can_create_repository,omitempty"`
}

type organizationMembershipRaw struct {
	URL                                        *string                        `json:"url"`
	State                                      *string                        `json:"state"`
	Role                                       *string                        `json:"role"`
	DirectMembership                           *bool                          `json:"direct_membership"`
	EnterpriseTeamsProvidingIndirectMembership []string                       `json:"enterprise_teams_providing_indirect_membership"`
	OrganizationURL                            *string                        `json:"organization_url"`
	Organization                               *organizationSimpleRaw         `json:"organization"`
	User                                       *accountRaw                    `json:"user"`
	Permissions                                *OrganizationMemberPermissions `json:"permissions"`
}

var organizationMembershipProperties = `"username":{"type":"string"},"url":{"type":"string"},` +
	`"state":{"type":"string"},"role":{"type":"string"},"direct_membership":{"type":"boolean"},` +
	`"enterprise_teams_providing_indirect_membership":{"type":"array","items":{"type":"string"}},` +
	`"organization_url":{"type":"string"},"organization":{"type":"object","properties":{` +
	`"login":{"type":"string"},"id":{"type":"integer"},"node_id":{"type":"string"},"avatar_url":{"type":"string"},` +
	`"description":{"type":"string"},"links":` + `{"type":"object","properties":{"url":{"type":"string"},` +
	`"repos_url":{"type":"string"},"events_url":{"type":"string"},"hooks_url":{"type":"string"},` +
	`"issues_url":{"type":"string"},"members_url":{"type":"string"},"public_members_url":{"type":"string"}},` +
	`"additionalProperties":false}},"additionalProperties":false},"user":` + organizationAccountSchema + `,` +
	`"permissions":{"type":"object","properties":{"can_create_repository":{"type":"boolean"}},` +
	`"additionalProperties":false}`

// OrganizationRole is one organization role GitHub lists.
type OrganizationRole struct {
	ID           int64                `json:"id"`
	Name         string               `json:"name"`
	Description  *string              `json:"description,omitempty"`
	BaseRole     *string              `json:"base_role,omitempty"`
	Source       *string              `json:"source,omitempty"`
	Permissions  []string             `json:"permissions,omitempty"`
	Organization *OrganizationAccount `json:"organization,omitempty"`
	CreatedAt    *string              `json:"created_at,omitempty"`
	UpdatedAt    *string              `json:"updated_at,omitempty"`
}

// OrganizationRoleList is every organization role of an organization; GitHub pages this route by neither
// page nor cursor.
type OrganizationRoleList struct {
	TotalCount int64              `json:"total_count"`
	Roles      []OrganizationRole `json:"roles"`
}

type organizationRoleRaw struct {
	ID           *int64      `json:"id"`
	Name         *string     `json:"name"`
	Description  *string     `json:"description"`
	BaseRole     *string     `json:"base_role"`
	Source       *string     `json:"source"`
	Permissions  []string    `json:"permissions"`
	Organization *accountRaw `json:"organization"`
	CreatedAt    *string     `json:"created_at"`
	UpdatedAt    *string     `json:"updated_at"`
}

// OrganizationInvitation is one pending invitation, without any token or link that accepts it.
type OrganizationInvitation struct {
	ID                 int64                `json:"id"`
	Login              *string              `json:"login,omitempty"`
	Email              *string              `json:"email,omitempty"`
	Role               *string              `json:"role,omitempty"`
	CreatedAt          *string              `json:"created_at,omitempty"`
	FailedAt           *string              `json:"failed_at,omitempty"`
	FailedReason       *string              `json:"failed_reason,omitempty"`
	Inviter            *OrganizationAccount `json:"inviter,omitempty"`
	TeamCount          *int64               `json:"team_count,omitempty"`
	NodeID             *string              `json:"node_id,omitempty"`
	InvitationTeamsURL *string              `json:"invitation_teams_url,omitempty"`
	InvitationSource   *string              `json:"invitation_source,omitempty"`
}

// OrganizationInvitationList is one batch of the pending invitations of an organization.
type OrganizationInvitationList struct {
	Invitations []OrganizationInvitation `json:"invitations"`
	NextCursor  string                   `json:"next_cursor,omitempty"`
	HasMore     bool                     `json:"has_more"`
}

type organizationInvitationRaw struct {
	ID                 *int64      `json:"id"`
	Login              *string     `json:"login"`
	Email              *string     `json:"email"`
	Role               *string     `json:"role"`
	CreatedAt          *string     `json:"created_at"`
	FailedAt           *string     `json:"failed_at"`
	FailedReason       *string     `json:"failed_reason"`
	Inviter            *accountRaw `json:"inviter"`
	TeamCount          *int64      `json:"team_count"`
	NodeID             *string     `json:"node_id"`
	InvitationTeamsURL *string     `json:"invitation_teams_url"`
	InvitationSource   *string     `json:"invitation_source"`
}

// ---- Descriptors

var (
	organizationMemberRoleValues = []string{"all", "admin", "member"}
	invitationRoleValues         = []string{"all", "admin", "direct_member", "billing_manager", "hiring_manager"}
	invitationSourceValues       = []string{"all", "member", "scim"}
)

const (
	organizationMemberRoleSchema = `{"type":"string","enum":["all","admin","member"]}`
	invitationRoleSchema         = `{"type":"string","enum":["all","admin","direct_member","billing_manager","hiring_manager"]}`
	invitationSourceSchema       = `{"type":"string","enum":["all","member","scim"]}`
)

func organizationReadTool(id, title, description string, tags []string, input, output json.RawMessage,
	arguments []capability.Argument, fields []capability.Field, example capability.Example) capability.Descriptor {
	return capability.Descriptor{ID: Provider + "." + id, Version: 1, Title: title, Description: description,
		Tags: append([]string{"github", "organization"}, tags...), Risk: readRisk, Provider: Provider,
		InputSchema: input, OutputSchema: output, Arguments: arguments, Fields: fields,
		Examples: []capability.Example{example}}
}

var organizationsGet = organizationReadTool("organizations.get", "Get a GitHub organization",
	"Read one organization an explicit connection allows by its owner target, with every member GitHub "+
		"documents for it, including its settings; what GitHub leaves out for a token below organization owner "+
		"stays out",
	[]string{"get"}, inputSchema(""), organizationDetailsOutput, nil, organizationDetailsFields,
	capability.Example{Description: "Read an organization", Arguments: json.RawMessage(`{"owner":"orgs/octo-org"}`)})

var organizationMembersList = organizationReadTool("organization-members.list", "List the members of a GitHub organization",
	"List one bounded batch of the members of an organization an explicit connection allows, in the order "+
		"GitHub returns them, by login; GitHub's filter for accounts without two-factor authentication is not offered",
	[]string{"members", "list"},
	inputSchema(`"role":`+organizationMemberRoleSchema+`,`+pagingKeys),
	listOutput("members", organizationAccountProperties, `"required":["login"],"additionalProperties":false`),
	append([]capability.Argument{{Name: "role", Description: "all, admin, or member; GitHub's default when omitted"}},
		pagingArguments...),
	append(append([]capability.Field{{Name: "members", Description: "Members by login, with the members of " +
		"GitHub's user object, including a person's name and email address; untrusted data"}},
		organizationAccountFields...), pagingFields...),
	capability.Example{Description: "List the administrators", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","role":"admin"}`)})

var organizationMembersGet = organizationReadTool("organization-members.get", "Get the membership of a GitHub account in an organization",
	"Read the role and state of one account in an organization an explicit connection allows; GitHub answers "+
		"only to a member of the organization",
	[]string{"members", "get"}, inputSchema(`"username":`+slugSchema, "username"),
	json.RawMessage(`{"type":"object","properties":{`+organizationMembershipProperties+
		`},"required":["username"],"additionalProperties":false}`),
	[]capability.Argument{usernameArgument},
	[]capability.Field{
		{Name: "username", Description: "Login of the account the call addressed"},
		{Name: "url", Description: "REST address of the membership, untrusted data, never fetched"},
		{Name: "state", Description: "active, or pending for an invitation GitHub has not seen accepted; untrusted data"},
		{Name: "role", Description: "admin, member, or billing_manager, untrusted data"},
		{Name: "direct_membership", Description: "Whether the account is a direct member of the organization"},
		{Name: "enterprise_teams_providing_indirect_membership", Description: "Slugs of the enterprise teams that " +
			"give the account indirect membership, at most 100; untrusted data"},
		{Name: "organization_url", Description: "REST address of the organization, untrusted data, never fetched"},
		{Name: "organization", Description: "Organization as GitHub's short object: login, id, node_id, " +
			"avatar_url, description, and links; untrusted data, addresses never fetched"},
		{Name: "user", Description: "Account of the membership with the members of GitHub's user object, " +
			"including a person's name and email address; untrusted data; absent while GitHub reports none"},
		{Name: "permissions", Description: "can_create_repository of the membership"},
	},
	capability.Example{Description: "Read a membership", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","username":"octocat"}`)})

var organizationRolesList = organizationReadTool("organization-roles.list", "List the organization roles of a GitHub organization",
	"List every organization role of an organization an explicit connection allows, predefined and custom; "+
		"GitHub answers in one piece",
	[]string{"roles", "list"}, inputSchema(""),
	json.RawMessage(`{"type":"object","properties":{"total_count":{"type":"integer"},"roles":{"type":"array",`+
		`"items":{"type":"object","properties":{"id":{"type":"integer"},"name":{"type":"string"},`+
		`"description":{"type":"string"},"base_role":{"type":"string"},"source":{"type":"string"},`+
		`"permissions":{"type":"array","items":{"type":"string"}},"organization":`+organizationAccountSchema+`,`+
		`"created_at":{"type":"string"},"updated_at":{"type":"string"}},"required":["id","name"],`+
		`"additionalProperties":false}}},"required":["total_count","roles"],"additionalProperties":false}`),
	nil,
	[]capability.Field{
		{Name: "total_count", Description: "Number of organization roles GitHub reports"},
		{Name: "roles", Description: "Roles with id, name, description (untrusted data), base_role, source " +
			"(Organization, Enterprise, or Predefined), the list of permissions, the creating account as " +
			"organization, and created_at and updated_at; the account carries the members of GitHub's user " +
			"object, untrusted data; organization is absent for a role without one"},
	},
	capability.Example{Description: "List the organization roles", Arguments: json.RawMessage(`{"owner":"orgs/octo-org"}`)})

var outsideCollaboratorsList = organizationReadTool("outside-collaborators.list", "List the outside collaborators of a GitHub organization",
	"List one bounded batch of the outside collaborators of an organization an explicit connection allows, in "+
		"the order GitHub returns them, by login; GitHub's filter for accounts without two-factor authentication "+
		"is not offered",
	[]string{"members", "list"}, inputSchema(pagingKeys),
	listOutput("collaborators", organizationAccountProperties, `"required":["login"],"additionalProperties":false`),
	append([]capability.Argument(nil), pagingArguments...),
	append(append([]capability.Field{{Name: "collaborators", Description: "Outside collaborators by login, with " +
		"the members of GitHub's user object, including a person's name and email address; untrusted data"}},
		organizationAccountFields...), pagingFields...),
	capability.Example{Description: "List the outside collaborators", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org"}`)})

var organizationInvitationProperties = `"id":{"type":"integer"},"login":{"type":"string"},` +
	`"email":{"type":"string"},"role":{"type":"string"},"created_at":{"type":"string"},` +
	`"failed_at":{"type":"string"},"failed_reason":{"type":"string"},"inviter":` + organizationAccountSchema + `,` +
	`"team_count":{"type":"integer"},"node_id":{"type":"string"},"invitation_teams_url":{"type":"string"},` +
	`"invitation_source":{"type":"string"}`

var organizationInvitationsList = organizationReadTool("organization-invitations.list", "List the pending invitations of a GitHub organization",
	"List one bounded batch of the pending invitations of an organization an explicit connection allows, in "+
		"the order GitHub returns them; no invitation token and no link that accepts an invitation is returned",
	[]string{"invitations", "list"},
	inputSchema(`"role":`+invitationRoleSchema+`,"invitation_source":`+invitationSourceSchema+`,`+pagingKeys),
	listOutput("invitations", organizationInvitationProperties,
		`"required":["id"],"additionalProperties":false`),
	append([]capability.Argument{
		{Name: "role", Description: "all, admin, direct_member, billing_manager, or hiring_manager; GitHub's " +
			"default when omitted"},
		{Name: "invitation_source", Description: "all, member, or scim; GitHub's default when omitted"},
	}, pagingArguments...),
	append([]capability.Field{
		{Name: "invitations", Description: "Pending invitations with id, login (absent for an invitee who is " +
			"not a GitHub member), email, role, created_at, failed_at, failed_reason, inviter, team_count, " +
			"node_id, invitation_teams_url, and invitation_source; untrusted data; the inviter carries the " +
			"members of GitHub's user object; addresses never fetched"},
	}, pagingFields...),
	capability.Example{Description: "List the pending invitations", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org"}`)})

// organizationReadToolIDs are the organization read tools that name an organization as their owner target.
var organizationReadToolIDs = []string{organizationsGet.ID, organizationMembersList.ID, organizationMembersGet.ID,
	organizationRolesList.ID, outsideCollaboratorsList.ID, organizationInvitationsList.ID}

// ---- Checks

// startPage derives the page of a list from the bound organization and the filters the list admits.
func (a *organizationArguments) startPage(kind string, bound target, filters ...any) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint(append([]any{kind, strings.ToLower(bound.String())}, filters...)...)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkOrganizationGet(*organizationArguments, target) error { return nil }

func checkOrganizationMembersList(a *organizationArguments, bound target) error {
	var filters []any
	if a.Role != "" {
		if !containsFold(organizationMemberRoleValues, a.Role) {
			return invalidRequest("role must be all, admin, or member")
		}
		a.role = strings.ToLower(a.Role)
		filters = append(filters, "role", a.role)
	}
	return a.startPage("orgmembers", bound, filters...)
}

func checkOrganizationMembersGet(a *organizationArguments, _ target) error {
	return checkUsername(a.Username)
}

func checkOutsideCollaboratorsList(a *organizationArguments, bound target) error {
	return a.startPage("outsidecollaborators", bound)
}

func checkOrganizationInvitationsList(a *organizationArguments, bound target) error {
	var filters []any
	if a.Role != "" {
		if !containsFold(invitationRoleValues, a.Role) {
			return invalidRequest("role must be all, admin, direct_member, billing_manager, or hiring_manager")
		}
		a.role = strings.ToLower(a.Role)
		filters = append(filters, "role", a.role)
	}
	if a.InvitationSource != "" {
		if !containsFold(invitationSourceValues, a.InvitationSource) {
			return invalidRequest("invitation_source must be all, member, or scim")
		}
		a.source = strings.ToLower(a.InvitationSource)
		filters = append(filters, "source", a.source)
	}
	return a.startPage("orginvitations", bound, filters...)
}

func organizationReadOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: organizationsGet, Handler: organizationHandler(organizationsGet.ID, checkOrganizationGet,
			func(ctx context.Context, c *Client, _ *organizationArguments) (any, error) {
				return c.getOrganization(ctx)
			})},
		{Descriptor: organizationMembersList, Handler: organizationHandler(organizationMembersList.ID,
			checkOrganizationMembersList, func(ctx context.Context, c *Client, a *organizationArguments) (any, error) {
				return c.listOrganizationMembers(ctx, a)
			})},
		{Descriptor: organizationMembersGet, Handler: organizationHandler(organizationMembersGet.ID,
			checkOrganizationMembersGet, func(ctx context.Context, c *Client, a *organizationArguments) (any, error) {
				return c.getOrganizationMembership(ctx, a)
			})},
		{Descriptor: organizationRolesList, Handler: organizationHandler(organizationRolesList.ID,
			checkOrganizationGet, func(ctx context.Context, c *Client, _ *organizationArguments) (any, error) {
				return c.listOrganizationRoles(ctx)
			})},
		{Descriptor: outsideCollaboratorsList, Handler: organizationHandler(outsideCollaboratorsList.ID,
			checkOutsideCollaboratorsList, func(ctx context.Context, c *Client, a *organizationArguments) (any, error) {
				return c.listOutsideCollaborators(ctx, a)
			})},
		{Descriptor: organizationInvitationsList, Handler: organizationHandler(organizationInvitationsList.ID,
			checkOrganizationInvitationsList, func(ctx context.Context, c *Client, a *organizationArguments) (any, error) {
				return c.listOrganizationInvitations(ctx, a)
			})},
	}
}

// ---- Requests

// Permission messages of the organization read tools. GitHub decides on every request; a message names what
// such a request needs without claiming what the configured token holds.
const (
	organizationReadPermission = "GitHub refused this token this organization; reading its full details needs " +
		"admin:org on a classic token, or Administration: read of the organization on a fine-grained token"
	organizationMembersReadPermission = "GitHub refused this token the people of this organization; reading " +
		"them needs read:org on a classic token, or Members: read of the organization on a fine-grained token"
	organizationRolesReadPermission = "GitHub refused this token the organization roles; reading them needs " +
		"admin:org on a classic token, or Members: read of the organization on a fine-grained token, and " +
		"organization administrator rights or the role permission to view organization roles"
)

func (c *Client) getOrganization(ctx context.Context) (*OrganizationDetails, error) {
	const op = "get organization"
	var raw organizationDetailsRaw
	if err := c.rest(ctx, op, "/orgs/"+url.PathEscape(c.target.owner), &raw); err != nil {
		return nil, actionsFailure(err, organizationReadPermission)
	}
	return raw.project(op)
}

func (c *Client) listOrganizationAccounts(ctx context.Context, a *organizationArguments, op, path string) (
	[]OrganizationAccount, bool, error) {
	var raw []accountRaw
	hasNext, err := c.restPage(ctx, op, c.orgPath(path), a.query(), &raw)
	if err != nil {
		return nil, false, actionsFailure(err, organizationMembersReadPermission)
	}
	accounts := make([]OrganizationAccount, 0, len(raw))
	for i := range raw {
		account, ok := raw[i].organizationAccount()
		if !ok {
			return nil, false, invalidEntry(op, "an account")
		}
		accounts = append(accounts, account)
	}
	return accounts, hasNext, nil
}

func (c *Client) listOrganizationMembers(ctx context.Context, a *organizationArguments) (*OrganizationMemberList, error) {
	accounts, hasNext, err := c.listOrganizationAccounts(ctx, a, "list organization members", "members")
	if err != nil {
		return nil, err
	}
	result := &OrganizationMemberList{Members: accounts}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) listOutsideCollaborators(ctx context.Context, a *organizationArguments) (*OutsideCollaboratorList, error) {
	accounts, hasNext, err := c.listOrganizationAccounts(ctx, a, "list outside collaborators", "outside_collaborators")
	if err != nil {
		return nil, err
	}
	result := &OutsideCollaboratorList{Collaborators: accounts}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) getOrganizationMembership(ctx context.Context, a *organizationArguments) (*OrganizationMembership, error) {
	const op = "get organization membership"
	var raw organizationMembershipRaw
	if err := c.rest(ctx, op, c.orgPath("memberships/"+url.PathEscape(a.Username)), &raw); err != nil {
		return nil, actionsFailure(err, organizationMembersReadPermission)
	}
	result := &OrganizationMembership{Username: a.Username, URL: address(raw.URL), State: shortText(raw.State),
		Role: shortText(raw.Role), DirectMembership: raw.DirectMembership, OrganizationURL: address(raw.OrganizationURL),
		Organization: raw.Organization.project(), User: optionalAccount(raw.User)}
	for _, slug := range raw.EnterpriseTeamsProvidingIndirectMembership {
		if text := shortText(&slug); text != nil && *text != "" {
			result.EnterpriseTeamsProvidingIndirectMembership = append(
				result.EnterpriseTeamsProvidingIndirectMembership, *text)
		}
	}
	if p := raw.Permissions; p != nil && p.CanCreateRepository != nil {
		result.Permissions = p
	}
	return result, nil
}

func (c *Client) listOrganizationRoles(ctx context.Context) (*OrganizationRoleList, error) {
	const op = "list organization roles"
	var raw struct {
		TotalCount *int64                `json:"total_count"`
		Roles      []organizationRoleRaw `json:"roles"`
	}
	if err := c.rest(ctx, op, c.orgPath("organization-roles"), &raw); err != nil {
		return nil, actionsFailure(err, organizationRolesReadPermission)
	}
	result := &OrganizationRoleList{Roles: make([]OrganizationRole, 0, len(raw.Roles))}
	for _, role := range raw.Roles {
		if role.ID == nil || role.Name == nil || *role.Name == "" {
			return nil, invalidEntry(op, "a role")
		}
		entry := OrganizationRole{ID: *role.ID, Name: *nameText(role.Name), Description: bulkText(role.Description),
			BaseRole: shortText(role.BaseRole), Source: shortText(role.Source),
			Organization: optionalAccount(role.Organization), CreatedAt: shortText(role.CreatedAt),
			UpdatedAt: shortText(role.UpdatedAt)}
		for _, permission := range role.Permissions {
			if text := shortText(&permission); text != nil {
				entry.Permissions = append(entry.Permissions, *text)
			}
		}
		result.Roles = append(result.Roles, entry)
	}
	result.TotalCount = int64(len(result.Roles))
	if raw.TotalCount != nil {
		result.TotalCount = *raw.TotalCount
	}
	return result, nil
}

func (c *Client) listOrganizationInvitations(ctx context.Context, a *organizationArguments) (*OrganizationInvitationList, error) {
	const op = "list organization invitations"
	var raw []organizationInvitationRaw
	hasNext, err := c.restPage(ctx, op, c.orgPath("invitations"), a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, organizationMembersReadPermission)
	}
	result := &OrganizationInvitationList{Invitations: make([]OrganizationInvitation, 0, len(raw))}
	for _, invitation := range raw {
		if invitation.ID == nil {
			return nil, invalidEntry(op, "an invitation")
		}
		result.Invitations = append(result.Invitations, OrganizationInvitation{ID: *invitation.ID,
			Login: shortText(invitation.Login), Email: shortText(invitation.Email), Role: shortText(invitation.Role),
			CreatedAt: shortText(invitation.CreatedAt), FailedAt: shortText(invitation.FailedAt),
			FailedReason: bulkText(invitation.FailedReason), Inviter: optionalAccount(invitation.Inviter),
			TeamCount: invitation.TeamCount, NodeID: shortText(invitation.NodeID),
			InvitationTeamsURL: address(invitation.InvitationTeamsURL),
			InvitationSource:   shortText(invitation.InvitationSource)})
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}
