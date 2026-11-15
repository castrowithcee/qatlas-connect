package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// github.collaborators.add invites an existing GitHub account to a repository, or adds it directly where
// GitHub does so, such as a member of the owning organization. The login must be one the connection's targets
// name as users/LOGIN; see selectInvitee. It is offered only where a connection's tools list names it.

const collaboratorAddPermission = "GitHub refused this invitation; it needs repo on a classic token with the " +
	"Admin role on the repository, or Administration: read and write on a fine-grained token"

var repositoryCollaboratorsAdd = capability.Descriptor{
	ID:      Provider + ".collaborators.add",
	Version: 1,
	Title:   "Invite a GitHub account to a repository",
	Description: "Invite one existing GitHub account as a collaborator of a repository a connection allows, or " +
		"add it directly where GitHub does that. Only a login that the connection's targets name as users/LOGIN " +
		"can be invited, never an e-mail address or a numeric ID; an account that is a collaborator already is " +
		"refused. Giving admin hands over control of the repository, so check it first. Offered only by a " +
		"connection whose tools list names it",
	Tags:                  []string{"github", "repositories", "collaborators", "add", "invite"},
	Risk:                  changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"username":`+collaboratorUsernameSchema+
		`,"permission":`+collaboratorPermissionSchema, "username", "permission"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"result":{"type":"string",` +
		`"enum":["invited","added"]},"username":{"type":"string"},"permission":{"type":"string"}},` +
		`"required":["result","username","permission"],"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "username", Description: "Login of the existing GitHub account to invite; the connection's " +
			"targets must name it as users/LOGIN; no e-mail address or numeric ID", Required: true},
		{Name: "permission", Description: "pull, triage, push, maintain, admin, or the name of a custom " +
			"repository role, up to 100 characters; admin is security-relevant", Required: true},
	},
	Fields: []capability.Field{
		{Name: "result", Description: "invited when GitHub sent an invitation, added when the account became a " +
			"collaborator directly"},
		{Name: "username", Description: "Login of the account, as GitHub spells it"},
		{Name: "permission", Description: "Permission that was sent"},
	},
	Examples: []capability.Example{{
		Description: "Invite an account with write access",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","username":"octocat","permission":"push"}`),
	}},
}

func collaboratorsAddOperations() []capability.Operation {
	return []capability.Operation{{Descriptor: repositoryCollaboratorsAdd, Handler: collaboratorsAddHandler()}}
}

// CollaboratorAdded is the answer to an invitation or a direct addition. It never carries an invitation ID
// or link.
type CollaboratorAdded struct {
	Result     string `json:"result"`
	Username   string `json:"username"`
	Permission string `json:"permission"`
}

// collaboratorsAddHandler checks the repository, the login, and the permission before a credential is
// resolved, so a refused request never reaches a secret or GitHub.
func collaboratorsAddHandler() capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments collaboratorChangeArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(repositoryCollaboratorsAdd.ID)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if bound.pattern() {
			return nil, invalidRequest("repository must name one repository")
		}
		login, err := selectInvitee(resolved, arguments.Username)
		if err != nil {
			return nil, err
		}
		if !validCollaboratorPermission(arguments.Permission) {
			return nil, invalidRequest("permission must be pull, triage, push, maintain, admin, or the name " +
				"of a custom repository role without control characters or slashes")
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(client.addCollaborator(ctx, login, arguments.Permission))
	}
}

func (c *Client) addCollaborator(ctx context.Context, login, permission string) (*CollaboratorAdded, error) {
	const op = "add collaborator"
	account, err := c.readInvitee(ctx, op, login)
	if err != nil {
		return nil, err
	}
	err = c.readCollaboration(ctx, op, account.Login)
	if err == nil {
		return nil, invalidRequest("the account is a collaborator of this repository already; change its " +
			"permission with github.collaborators.update; nothing was sent")
	}
	if !isNotFound(err) {
		return nil, actionsFailure(err, collaboratorAddPermission)
	}
	status, err := c.restChangeStatus(ctx, op, http.MethodPut, c.collaboratorPath(account.Login),
		map[string]string{"permission": permission}, nil)
	if err != nil {
		var failure *provider.Error
		if errors.As(err, &failure) && failure.Original != nil &&
			failure.Original.Status == http.StatusUnprocessableEntity {
			return nil, invalidRequest("GitHub refused this invitation; the permission may be unknown for this " +
				"repository, or the account may not be invited to it")
		}
		return nil, actionsFailure(err, collaboratorAddPermission)
	}
	switch status {
	case http.StatusCreated:
		return &CollaboratorAdded{Result: "invited", Username: account.Login, Permission: permission}, nil
	case http.StatusNoContent:
		return &CollaboratorAdded{Result: "added", Username: account.Login, Permission: permission}, nil
	}
	return nil, invalidResponse(op, true)
}
