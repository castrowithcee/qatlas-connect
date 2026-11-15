package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The two tools below only narrow or hand over a repository. Neither reaches anyone new: the visibility
// never becomes public, and a repository moves only to an owner the connection itself names as a target.
// Making a repository public is a separate decision that no argument of any tool here offers.

// narrowedVisibilities are the only visibilities github.repositories.set-visibility sends.
var narrowedVisibilities = []string{"private", "internal"}

const maxTransferTeams = 50

const repositoryVisibilityPermission = "GitHub refused this change of the visibility of a repository; it needs " +
	"repo on a classic token with the Admin role on the repository, or Administration: read and write on a " +
	"fine-grained token, and an organization may allow only its owners to change it"

const repositoryTransferPermission = "GitHub refused this transfer of a repository; it needs repo on a classic " +
	"token with the Admin role on the repository, plus admin:org to transfer into an organization, or " +
	"Administration: read and write on a fine-grained token, and the new owner must allow the account to " +
	"create repositories"

const repositoryVisibilityOutput = `{"type":"object","properties":{"visibility":{"type":"string"},` +
	`"private":{"type":"boolean"}},"required":["visibility"],"additionalProperties":false}`

const repositoryTransferredOutput = `{"type":"object","properties":{"new_owner":{"type":"string"},"new_name":{"type":"string"},` +
	`"accepted":{"type":"boolean"}},"required":["new_owner","accepted"],"additionalProperties":false}`

var repositoriesSetVisibility = capability.Descriptor{
	ID:      Provider + ".repositories.set-visibility",
	Version: 1,
	Title:   "Narrow the visibility of a GitHub repository",
	Description: "Change the visibility of one repository a connection allows to private or internal; public " +
		"is not offered and is refused before any request, and only the visibility is sent. Offered only by " +
		"a connection whose tools list names it",
	Tags:                  []string{"github", "repositories", "visibility", "update"},
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"repository":`+repoSchema+`,"visibility":`+enumSchema(narrowedVisibilities),
		"visibility"),
	OutputSchema: json.RawMessage(repositoryVisibilityOutput),
	Arguments: []capability.Argument{
		{Name: "visibility", Description: "private or internal; internal needs an organization on GitHub " +
			"Enterprise", Required: true},
	},
	Fields: []capability.Field{
		{Name: "visibility", Description: "Visibility as GitHub reports it after the change"},
		{Name: "private", Description: "True while GitHub reports the repository as private"},
	},
	Examples: []capability.Example{{
		Description: "Make a repository private",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","visibility":"private"}`),
	}},
}

var repositoriesTransfer = capability.Descriptor{
	ID:      Provider + ".repositories.transfer",
	Version: 1,
	Title:   "Transfer a GitHub repository",
	Description: "Transfer one repository a connection allows to another owner; new_owner must be an owner " +
		"the connection's targets name themselves as users/LOGIN or orgs/LOGIN, checked before any credential " +
		"is resolved, and confirm_name must repeat the repository as owner/name exactly. GitHub reports only " +
		"that it accepted the request: a transfer to a user account takes effect once that user accepts it. " +
		"Offered only by a connection whose tools list names it",
	Tags:                  []string{"github", "repositories", "transfer", "update"},
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyUnknown),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"repository":`+repoSchema+`,"confirm_name":`+repoSchema+`,"new_owner":`+
		`{"type":"string","minLength":1,"maxLength":110,"x-form":"users/LOGIN or orgs/LOGIN"},"new_name":`+
		repositoryNameSchema+`,"team_ids":{"type":"array","maxItems":50,"items":{"type":"integer","minimum":1,`+
		`"maximum":9007199254740991}}`, "confirm_name", "new_owner"),
	OutputSchema: json.RawMessage(repositoryTransferredOutput),
	Arguments: []capability.Argument{
		{Name: "confirm_name", Description: "The repository's owner/name, repeated exactly, to confirm the " +
			"transfer", Required: true},
		{Name: "new_owner", Description: "Owner to transfer to, as users/LOGIN or orgs/LOGIN, one the " +
			"connection's targets name as an owner", Required: true},
		{Name: "new_name", Description: "New repository name under the new owner, 1 to 100 characters; the " +
			"current name when omitted"},
		{Name: "team_ids", Description: "IDs of teams to add to the repository, up to 50; only with an " +
			"organization as new_owner"},
	},
	Fields: []capability.Field{
		{Name: "new_owner", Description: "The owner asked for, as users/LOGIN or orgs/LOGIN"},
		{Name: "new_name", Description: "The new name asked for; absent when the name stays"},
		{Name: "accepted", Description: "True once GitHub accepted the request; the transfer continues " +
			"asynchronously and, to a user account, takes effect only after that user accepts it"},
	},
	Examples: []capability.Example{{
		Description: "Transfer a repository to an organization",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","confirm_name":"octo-org/example",` +
			`"new_owner":"orgs/octo-archive"}`),
	}},
}

func repositoryHandoverOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: repositoriesSetVisibility, Handler: capability.Handler(invokeRepositoriesSetVisibility)},
		{Descriptor: repositoriesTransfer, Handler: capability.Handler(invokeRepositoriesTransfer)},
	}
}

// invokeRepositoriesSetVisibility checks the repository and the visibility before a credential is resolved,
// so public, or any other value, never becomes a request.
func invokeRepositoriesSetVisibility(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	bound, err := selectTarget(resolved, kindRepository, raw)
	if err != nil {
		return nil, err
	}
	var arguments struct {
		Visibility string `json:"visibility"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, unreadable(repositoriesSetVisibility.ID)
	}
	if bound.pattern() {
		return nil, invalidRequest("repository must name one repository")
	}
	if !contains(narrowedVisibilities, arguments.Visibility) {
		return nil, invalidRequest("visibility must be private or internal; a repository is never made public here")
	}
	client, err := openAt(ctx, resolved, secrets, red, bound)
	if err != nil {
		return nil, err
	}
	return bound.locate(client.setRepositoryVisibility(ctx, arguments.Visibility))
}

// RepositoryVisibilitySet is the answer to a changed visibility; both members are what GitHub reports.
type RepositoryVisibilitySet struct {
	Visibility string `json:"visibility"`
	Private    *bool  `json:"private,omitempty"`
}

// setRepositoryVisibility sends the visibility alone in one request that is never repeated.
func (c *Client) setRepositoryVisibility(ctx context.Context, visibility string) (*RepositoryVisibilitySet, error) {
	const op = "set repository visibility"
	path := "/repos/" + url.PathEscape(c.target.owner) + "/" + url.PathEscape(c.target.repo)
	var answer struct {
		FullName   string `json:"full_name"`
		Visibility string `json:"visibility"`
		Private    *bool  `json:"private"`
	}
	if err := c.restChange(ctx, op, http.MethodPatch, path, map[string]string{"visibility": visibility},
		&answer); err != nil {
		return nil, writeFailure(err, "GitHub refused this visibility for the repository; it may not be "+
			"available for this owner or plan", repositoryVisibilityPermission)
	}
	// The answer must be this repository and state a visibility; anything else leaves the state unclear.
	if !strings.EqualFold(answer.FullName, c.target.argument()) || answer.Visibility == "" {
		return nil, invalidResponse(op, true)
	}
	return &RepositoryVisibilitySet{Visibility: answer.Visibility,
		Private: answer.Private}, nil
}

// transferOwner resolves new_owner against the targets of the connection. It must be an owner target the
// connection names itself; a repository or project target of that owner does not imply it, and a pattern
// never matches. The errors quote no owner.
func (a allowlist) transferOwner(value string) (target, error) {
	const form = "users/LOGIN or orgs/LOGIN"
	parsed, err := parseTarget(value)
	if err != nil || parsed.kind != kindOwner || strings.TrimSpace(value) != value {
		return target{}, invalidRequest("new_owner must be " + form)
	}
	if a.excluded(parsed) || a.excludedOwner(parsed.owner) {
		return target{}, invalidRequest("new_owner is not available on this connection; pass another one")
	}
	for _, entry := range a.targets {
		if entry.kind == kindOwner && strings.EqualFold(entry.String(), parsed.String()) {
			return entry, nil
		}
	}
	return target{}, invalidRequest("new_owner must be an owner the connection's targets name themselves as " +
		form + "; add it to the connection's targets first")
}

type repositoryTransferArguments struct {
	ConfirmName string  `json:"confirm_name"`
	NewOwner    string  `json:"new_owner"`
	NewName     string  `json:"new_name"`
	TeamIDs     []int64 `json:"team_ids"`
}

// invokeRepositoriesTransfer checks the repository, confirm_name, the new owner, the new name, and the teams
// before a credential is resolved, so a refused transfer never reaches GitHub.
func invokeRepositoriesTransfer(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	bound, err := selectTarget(resolved, kindRepository, raw)
	if err != nil {
		return nil, err
	}
	var arguments repositoryTransferArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, unreadable(repositoriesTransfer.ID)
	}
	if bound.pattern() {
		return nil, invalidRequest("repository must name one repository")
	}
	if arguments.ConfirmName != bound.argument() {
		return nil, invalidRequest("confirm_name must repeat the repository exactly: " + bound.argument())
	}
	allowed, err := allowlistOf(resolved)
	if err != nil {
		return nil, providerError("open", err.Error())
	}
	destination, err := allowed.transferOwner(arguments.NewOwner)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(destination.owner, bound.owner) {
		return nil, invalidRequest("new_owner must differ from the current owner")
	}
	name := bound.repo
	if arguments.NewName != "" {
		// GitHub refuses a name ending in .git, which validRepoName alone allows.
		if !validRepoName(arguments.NewName) || strings.HasSuffix(strings.ToLower(arguments.NewName), ".git") {
			return nil, invalidRequest("new_name must be a usable repository name")
		}
		name = arguments.NewName
	}
	if err := allowed.refuseDestination(destination.owner, name); err != nil {
		return nil, err
	}
	if len(arguments.TeamIDs) > 0 {
		if destination.scope != "orgs" {
			return nil, invalidRequest("team_ids can only be used when new_owner is an organization")
		}
		if len(arguments.TeamIDs) > maxTransferTeams {
			return nil, invalidRequest("team_ids may hold at most 50 teams")
		}
		for _, id := range arguments.TeamIDs {
			if id < 1 {
				return nil, invalidRequest("team_ids must hold positive team ids")
			}
		}
	}
	client, err := openAt(ctx, resolved, secrets, red, bound)
	if err != nil {
		return nil, err
	}
	return bound.locate(client.transferRepository(ctx, destination, &arguments))
}

// RepositoryTransferred is the answer to an accepted transfer request.
type RepositoryTransferred struct {
	NewOwner string `json:"new_owner"`
	NewName  string `json:"new_name,omitempty"`
	Accepted bool   `json:"accepted"`
}

// transferRepository asks GitHub for the transfer in one request that is never repeated.
func (c *Client) transferRepository(ctx context.Context, destination target,
	a *repositoryTransferArguments) (*RepositoryTransferred, error) {
	const op = "transfer repository"
	body := map[string]any{"new_owner": destination.owner}
	if a.NewName != "" {
		body["new_name"] = a.NewName
	}
	if len(a.TeamIDs) > 0 {
		body["team_ids"] = a.TeamIDs
	}
	var answer struct {
		FullName string `json:"full_name"`
	}
	if err := c.restChange(ctx, op, http.MethodPost, c.repoPath("transfer"), body, &answer); err != nil {
		return nil, writeFailure(err, "GitHub refused the transfer; the new owner or name is not allowed, the "+
			"name is taken there, or the repository cannot be transferred in its current state",
			repositoryTransferPermission)
	}
	if !validRepository(answer.FullName) {
		return nil, invalidResponse(op, true)
	}
	return &RepositoryTransferred{NewOwner: destination.String(),
		NewName: a.NewName, Accepted: true}, nil
}

// writeFailure turns GitHub's 422 into an invalid request that says what it means for this tool, and names
// the needed rights of a refusal. No provider text is copied.
func writeFailure(err error, rejected, permission string) error {
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Original != nil &&
		failure.Original.Status == http.StatusUnprocessableEntity {
		return invalidRequest(rejected)
	}
	return actionsFailure(err, permission)
}
