package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Open invitations to a repository. github.repository-invitations.list reads them; github.repository-
// invitations.cancel withdraws one and is offered only where a connection's tools list names it. Creating and
// accepting an invitation are out of reach. GitHub's own url and html_url of an invitation are the addresses
// through which the invitee accepts it, so neither is shown. The identifier is bound to the repository by
// finding it in the repository's own invitation list before the cancel, and the cancel is sent at most once.

// Bounds of the proof read of a cancel: it searches the newest invitations only.
const (
	invitationProofPageSize = 100
	maxInvitationProofPages = 10
)

var invitationProperties = `"id":{"type":"integer"},"node_id":{"type":"string"},"repository":` +
	restRepositorySchema + `,"invitee":` + restAccountSchema + `,"inviter":` + restAccountSchema + `,` +
	`"permissions":{"type":"string"},"created_at":{"type":"string"},"expired":{"type":"boolean"}`

const invitationRequired = `"required":["id","permissions"],"additionalProperties":false`

var repositoryInvitationsList = capability.Descriptor{
	ID:      Provider + ".repository-invitations.list",
	Version: 1,
	Title:   "List GitHub repository invitations",
	Description: "List one bounded batch of the open invitations to a repository a connection allows, with " +
		"invitee, inviter, permission, and whether each has expired; the addresses that let the invitee " +
		"accept are never shown",
	Tags:         []string{"github", "repositories", "invitations", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(pagingKeys),
	OutputSchema: listOutput("invitations", invitationProperties, invitationRequired),
	Arguments:    pagingArguments,
	Fields: append([]capability.Field{
		{Name: "invitations", Description: "Invitations with id, node_id, repository, invitee and inviter " +
			"(a person's login, name, and email address as GitHub reports them, untrusted data), permissions, " +
			"created_at, and expired; GitHub's url and html_url are left out because they let the invitee accept"},
	}, pagingFields...),
	Examples: []capability.Example{{Description: "List the open invitations", Arguments: json.RawMessage(`{}`)}},
}

const invitationCancelledOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},"id":{"type":"integer"},` +
	`"invitee":{"type":"string"},"permissions":{"type":"string"}},"required":["deleted","id"],` +
	`"additionalProperties":false}`

var repositoryInvitationsCancel = capability.Descriptor{
	ID:      Provider + ".repository-invitations.cancel",
	Version: 1,
	Title:   "Cancel a GitHub repository invitation",
	Description: "Withdraw one open invitation to a repository a connection allows by its id, after the " +
		"repository's own invitation list proves it belongs there; offered only where a connection's tools list " +
		"names it",
	Tags:                  []string{"github", "repositories", "invitations", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, accessGrantSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"id":`+grantIDSchema, "id"),
	OutputSchema:          json.RawMessage(invitationCancelledOutput),
	Arguments:             []capability.Argument{{Name: "id", Description: "Numeric id of the invitation", Required: true}},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub withdrew the invitation"},
		{Name: "id", Description: "Id of the withdrawn invitation"},
		{Name: "invitee", Description: "Login of the invitee, untrusted data; absent when GitHub reported none"},
		{Name: "permissions", Description: "Permission the invitation offered"},
	},
	Examples: []capability.Example{{Description: "Withdraw an invitation", Arguments: json.RawMessage(`{"id":1}`)}},
}

// repositoryInvitationsOperations binds every repository invitation tool to its handler.
func repositoryInvitationsOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: repositoryInvitationsList, Handler: grantsHandler(repositoryInvitationsList.ID,
			checkGrantsPaging("invitations"), func(ctx context.Context, c *Client, a *grantArguments) (any, error) {
				return c.listInvitations(ctx, a)
			})},
		{Descriptor: repositoryInvitationsCancel, Handler: grantsHandler(repositoryInvitationsCancel.ID,
			checkGrantID, func(ctx context.Context, c *Client, a *grantArguments) (any, error) {
				return c.cancelInvitation(ctx, a.ID)
			})},
	}
}

// invitationRaw is the REST repository invitation; its url and html_url are never decoded.
type invitationRaw struct {
	ID          *int64         `json:"id"`
	NodeID      *string        `json:"node_id"`
	Repository  *repositoryRaw `json:"repository"`
	Invitee     *accountRaw    `json:"invitee"`
	Inviter     *accountRaw    `json:"inviter"`
	Permissions *string        `json:"permissions"`
	CreatedAt   *string        `json:"created_at"`
	Expired     *bool          `json:"expired"`
}

// RepositoryInvitation is the view of one open invitation.
type RepositoryInvitation struct {
	ID          int64           `json:"id"`
	NodeID      *string         `json:"node_id,omitempty"`
	Repository  *RESTRepository `json:"repository,omitempty"`
	Invitee     *RESTAccount    `json:"invitee,omitempty"`
	Inviter     *RESTAccount    `json:"inviter,omitempty"`
	Permissions string          `json:"permissions"`
	CreatedAt   *string         `json:"created_at,omitempty"`
	Expired     *bool           `json:"expired,omitempty"`
}

func (c *Client) invitationView(r *invitationRaw) (RepositoryInvitation, bool) {
	if r.ID == nil || *r.ID < 1 || r.Permissions == nil || *r.Permissions == "" {
		return RepositoryInvitation{}, false
	}
	return RepositoryInvitation{ID: *r.ID, NodeID: shortText(r.NodeID), Repository: r.Repository.project(c.allowed),
		Invitee: r.Invitee.project(), Inviter: r.Inviter.project(), Permissions: *nameText(r.Permissions),
		CreatedAt: shortText(r.CreatedAt), Expired: r.Expired}, true
}

// RepositoryInvitationList is one batch of open invitations.
type RepositoryInvitationList struct {
	Invitations []RepositoryInvitation `json:"invitations"`
	NextCursor  string                 `json:"next_cursor,omitempty"`
	HasMore     bool                   `json:"has_more"`
}

func (c *Client) listInvitations(ctx context.Context, a *grantArguments) (*RepositoryInvitationList, error) {
	const op = "list repository invitations"
	var raw []invitationRaw
	hasNext, err := c.restPage(ctx, op, c.repoPath("invitations"), a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, grantsReadPermission)
	}
	result := &RepositoryInvitationList{Invitations: make([]RepositoryInvitation, 0, len(raw))}
	for i := range raw {
		invitation, ok := c.invitationView(&raw[i])
		if !ok {
			return nil, invalidEntry(op, "an invitation")
		}
		result.Invitations = append(result.Invitations, invitation)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

// findInvitation is the proof read of a cancel: GitHub offers no read of one invitation, so it pages through
// the newest invitations of the bound repository until it meets the identifier, and the entry must name the
// bound repository, so an identifier of another repository is never sent.
func (c *Client) findInvitation(ctx context.Context, op string, id int64) (RepositoryInvitation, error) {
	for page := 1; page <= maxInvitationProofPages; page++ {
		query := url.Values{"per_page": {strconv.Itoa(invitationProofPageSize)}, "page": {strconv.Itoa(page)}}
		var raw []invitationRaw
		hasNext, err := c.restPage(ctx, op, c.repoPath("invitations"), query, &raw)
		if err != nil {
			return RepositoryInvitation{}, actionsFailure(err, grantsReadPermission)
		}
		for i := range raw {
			if raw[i].ID == nil || *raw[i].ID != id {
				continue
			}
			if raw[i].Repository == nil || raw[i].Repository.FullName == nil ||
				!strings.EqualFold(*raw[i].Repository.FullName, c.target.owner+"/"+c.target.repo) {
				return RepositoryInvitation{}, foreignActionsObject(op, "an invitation")
			}
			invitation, ok := c.invitationView(&raw[i])
			if !ok {
				return RepositoryInvitation{}, invalidEntry(op, "an invitation")
			}
			return invitation, nil
		}
		if !hasNext {
			return RepositoryInvitation{}, &provider.Error{Class: provider.ClassNotFound, Op: op,
				Message: "this repository holds no open invitation with this id; nothing was changed"}
		}
	}
	return RepositoryInvitation{}, &provider.Error{Class: provider.ClassNotFound, Op: op,
		Message: "the newest " + strconv.Itoa(maxInvitationProofPages*invitationProofPageSize) +
			" invitations of this repository hold none with this id; nothing was changed"}
}

// InvitationCancelled is the answer of a withdrawn invitation.
type InvitationCancelled struct {
	Deleted     bool    `json:"deleted"`
	ID          int64   `json:"id"`
	Invitee     *string `json:"invitee,omitempty"`
	Permissions string  `json:"permissions,omitempty"`
}

// cancelInvitation sends the DELETE only after the invitation list of the bound repository answered the
// identifier; the DELETE is sent once and never repeated.
func (c *Client) cancelInvitation(ctx context.Context, id int64) (*InvitationCancelled, error) {
	const op = "cancel repository invitation"
	invitation, err := c.findInvitation(ctx, op, id)
	if err != nil {
		return nil, err
	}
	if err := c.restChange(ctx, op, http.MethodDelete, c.repoPath("invitations/"+strconv.FormatInt(id, 10)),
		struct{}{}, nil); err != nil {
		return nil, actionsFailure(err, grantsChangePermission)
	}
	result := &InvitationCancelled{Deleted: true, ID: id, Permissions: invitation.Permissions}
	if invitation.Invitee != nil {
		result.Invitee = invitation.Invitee.Login
	}
	return result, nil
}
