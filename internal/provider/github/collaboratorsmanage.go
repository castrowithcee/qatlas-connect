package github

import (
	"context"
	"encoding/json"
	"errors"
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

// github.collaborators.update changes the role of an existing collaborator of a repository and
// github.collaborators.remove removes one. Both are offered only where a connection's tools list names them.
// The collaborator relation is read first: a user who is no collaborator is refused without a write, because
// GitHub answers the same PUT with an invitation for such a user. Each change is one request, never repeated.

const maxCollaboratorPermissionLength = 100

const collaboratorChangePermission = "GitHub refused this change of a collaborator; it needs repo on a " +
	"classic token with the Admin role on the repository, or Administration: read and write on a " +
	"fine-grained token"

const (
	collaboratorUsernameSchema   = `{"type":"string","minLength":1,"maxLength":100,"pattern":"` + loginPattern + `"}`
	collaboratorPermissionSchema = `{"type":"string","minLength":1,"maxLength":100,` +
		`"x-form":"pull, triage, push, maintain, admin, or the name of a custom repository role"}`
)

var collaboratorUsernameArgument = capability.Argument{Name: "username", Description: "Login of the " +
	"existing collaborator of the repository", Required: true}

var repositoryCollaboratorsUpdate = capability.Descriptor{
	ID:      Provider + ".collaborators.update",
	Version: 1,
	Title:   "Change the role of a GitHub repository collaborator",
	Description: "Change the permission of one existing collaborator of a repository a connection allows; a " +
		"user who is not a collaborator is refused and never invited. Raising a collaborator to admin hands " +
		"over control of the repository, so check it first. Offered only by a connection whose tools list " +
		"names it",
	Tags:                  []string{"github", "repositories", "collaborators", "update"},
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"username":`+collaboratorUsernameSchema+
		`,"permission":`+collaboratorPermissionSchema, "username", "permission"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"updated":{"type":"boolean"},` +
		`"username":{"type":"string"},"permission":{"type":"string"}},` +
		`"required":["updated","username","permission"],"additionalProperties":false}`),
	Arguments: []capability.Argument{
		collaboratorUsernameArgument,
		{Name: "permission", Description: "pull, triage, push, maintain, admin, or the name of a custom " +
			"repository role, up to 100 characters; admin is security-relevant", Required: true},
	},
	Fields: []capability.Field{
		{Name: "updated", Description: "True once GitHub changed the role"},
		{Name: "username", Description: "Login of the collaborator"},
		{Name: "permission", Description: "Permission that was sent"},
	},
	Examples: []capability.Example{{
		Description: "Give a collaborator write access",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","username":"octocat","permission":"push"}`),
	}},
}

var repositoryCollaboratorsRemove = capability.Descriptor{
	ID:      Provider + ".collaborators.remove",
	Version: 1,
	Title:   "Remove a GitHub repository collaborator",
	Description: "Remove one existing collaborator from a repository a connection allows; a user who is not a " +
		"collaborator is reported as not found. Offered only by a connection whose tools list names it",
	Tags:                  []string{"github", "repositories", "collaborators", "remove", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"username":`+collaboratorUsernameSchema, "username"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"removed":{"type":"boolean"},` +
		`"username":{"type":"string"},"repository":{"type":"string"}},"required":["removed","username"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{collaboratorUsernameArgument},
	Fields: []capability.Field{
		{Name: "removed", Description: "True once GitHub removed the collaborator"},
		{Name: "username", Description: "Login of the removed collaborator"},
	},
	Examples: []capability.Example{{
		Description: "Remove a collaborator",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","username":"octocat"}`),
	}},
}

func collaboratorsManageOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: repositoryCollaboratorsUpdate, Handler: collaboratorsChangeHandler(
			repositoryCollaboratorsUpdate.ID, true)},
		{Descriptor: repositoryCollaboratorsRemove, Handler: collaboratorsChangeHandler(
			repositoryCollaboratorsRemove.ID, false)},
	}
}

// CollaboratorUpdated is the answer to a changed collaborator role.
type CollaboratorUpdated struct {
	Updated    bool   `json:"updated"`
	Username   string `json:"username"`
	Permission string `json:"permission"`
}

// CollaboratorRemoved is the answer to a removed collaborator.
type CollaboratorRemoved struct {
	Removed  bool   `json:"removed"`
	Username string `json:"username"`
}

type collaboratorChangeArguments struct {
	Username   string `json:"username"`
	Permission string `json:"permission"`
}

// validCollaboratorPermission accepts a role name as one line without path or control characters, since a
// custom repository role may carry any other name.
func validCollaboratorPermission(value string) bool {
	if value == "" || len(value) > maxCollaboratorPermissionLength || !utf8.ValidString(value) ||
		strings.TrimSpace(value) != value {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || r == '/' || r == '\\' }) < 0
}

// collaboratorsChangeHandler checks the target, the login, and the permission before a credential is
// resolved, so a refused request never reaches a secret or GitHub.
func collaboratorsChangeHandler(id string, update bool) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments collaboratorChangeArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if bound.pattern() {
			return nil, invalidRequest("repository must name one repository")
		}
		if !validLogin(arguments.Username) {
			return nil, invalidRequest("username must be a GitHub login")
		}
		if update && !validCollaboratorPermission(arguments.Permission) {
			return nil, invalidRequest("permission must be pull, triage, push, maintain, admin, or the name " +
				"of a custom repository role without control characters or slashes")
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		if update {
			return bound.locate(client.updateCollaborator(ctx, arguments))
		}
		return bound.locate(client.removeCollaborator(ctx, arguments.Username))
	}
}

func (c *Client) collaboratorPath(username string) string {
	return c.repoPath("collaborators/" + url.PathEscape(username))
}

// readCollaboration reads whether the user is a collaborator: 204 is one, 404 is not.
func (c *Client) readCollaboration(ctx context.Context, op, username string) error {
	return actionsFailure(c.rest(ctx, op, c.collaboratorPath(username), nil), collaboratorChangePermission)
}

func (c *Client) updateCollaborator(ctx context.Context, a collaboratorChangeArguments) (*CollaboratorUpdated, error) {
	const op = "update collaborator"
	if err := c.readCollaboration(ctx, op, a.Username); err != nil {
		if isNotFound(err) {
			return nil, invalidRequest("the user is not a collaborator of this repository, or the token cannot " +
				"see it; nothing was sent, since GitHub would invite the user")
		}
		return nil, err
	}
	status, err := c.restChangeStatus(ctx, op, http.MethodPut, c.collaboratorPath(a.Username),
		map[string]string{"permission": a.Permission}, nil)
	if err != nil {
		var failure *provider.Error
		if errors.As(err, &failure) && failure.Original != nil &&
			failure.Original.Status == http.StatusUnprocessableEntity {
			return nil, invalidRequest("GitHub refused this permission for the collaborator; it may be unknown " +
				"for this repository or not allowed")
		}
		return nil, actionsFailure(err, collaboratorChangePermission)
	}
	switch status {
	case http.StatusNoContent:
		return &CollaboratorUpdated{Updated: true, Username: a.Username, Permission: a.Permission}, nil
	case http.StatusCreated:
		return nil, &provider.Error{Class: provider.ClassInvalidResponse, Op: op,
			Message: "GitHub created an invitation instead of changing the role of a collaborator; the user " +
				"was not a collaborator after all, so check and cancel the pending invitation of this repository"}
	}
	return nil, invalidResponse(op, true)
}

func (c *Client) removeCollaborator(ctx context.Context, username string) (*CollaboratorRemoved, error) {
	const op = "remove collaborator"
	if err := c.readCollaboration(ctx, op, username); err != nil {
		return nil, err
	}
	status, err := c.restChangeStatus(ctx, op, http.MethodDelete, c.collaboratorPath(username), nil, nil)
	if err != nil {
		return nil, actionsFailure(err, collaboratorChangePermission)
	}
	if status != http.StatusNoContent {
		return nil, invalidResponse(op, true)
	}
	return &CollaboratorRemoved{Removed: true, Username: username}, nil
}
