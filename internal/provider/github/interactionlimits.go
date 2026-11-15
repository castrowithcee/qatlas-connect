package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The interaction limits restrict, for a time, who may comment on, open issues or pull requests in, or
// react to a repository, to every repository of an organization, or to every public repository of the
// account behind the token. A call addresses exactly one of these three levels: repository, organization, or,
// with neither, the account. The limits only narrow what others may do and open no way out of the
// connection, so the changes are offered by their effect like other restrictions; GitHub accepts a change only
// while no limit of a wider level is set.

// GitHub's own values; anything else is refused before a request.
var (
	interactionLimitValues  = []string{"existing_users", "contributors_only", "collaborators_only"}
	interactionExpiryValues = []string{"one_day", "three_days", "one_week", "one_month", "six_months"}
)

const interactionLimitProperties = `"limit":{"type":"string"},"origin":{"type":"string"},"expires_at":{"type":"string"}`

const interactionLimitKeys = `"repository":` + repoSchema + `,"organization":` + ownerSchema

var interactionLimitTargetArguments = []capability.Argument{
	{Name: "repository", Description: "Repository as OWNER/REPO whose limit this call addresses; at most one of " +
		"repository or organization, never both; must lie inside the targets when the connection lists any"},
	{Name: "organization", Description: "Organization as orgs/LOGIN whose limit this call addresses; at most one " +
		"of repository or organization, never both; must be an owner target the connection allows when it lists " +
		"any. With neither, the call addresses the account behind the token, which a connection whose targets " +
		"name a repository or a project refuses"},
}

var interactionLimitTargetFields = []capability.Field{
	{Name: "repository", Description: "Repository this call addressed, as OWNER/REPO; present only for a repository"},
	{Name: "organization", Description: "Organization this call addressed, as orgs/LOGIN; present only for an " +
		"organization"},
}

var interactionLimitFields = []capability.Field{
	{Name: "limit", Description: "existing_users, contributors_only, or collaborators_only; absent while no " +
		"limit is set"},
	{Name: "origin", Description: "Level the limit comes from, as GitHub reports it, such as repository, " +
		"organization, or user; absent while no limit is set"},
	{Name: "expires_at", Description: "Time the limit ends, as GitHub reports it; absent while no limit is set"},
}

var interactionLimitsGet = capability.Descriptor{
	ID:      Provider + ".interaction-limits.get",
	Version: 1,
	Title:   "Get GitHub interaction limits",
	Description: "Read the interaction limit of one repository or, with organization instead, one organization a " +
		"connection allows, or, with neither, of the account behind the token: who may comment, open issues or " +
		"pull requests, and react, until when; an answer without limit says none is set; a connection whose " +
		"targets name a repository or a project may not read the account's",
	Tags:         []string{"github", "interaction-limits", "get", "moderation"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(interactionLimitKeys),
	OutputSchema: interactionLimitOutput(interactionLimitProperties),
	Arguments:    append([]capability.Argument{}, interactionLimitTargetArguments...),
	Fields:       append(append([]capability.Field{}, interactionLimitFields...), interactionLimitTargetFields...),
	Examples: []capability.Example{
		{Description: "Read the limit of a repository", Arguments: json.RawMessage(`{"repository":"octo-org/example"}`)},
		{Description: "Read the limit of an organization", Arguments: json.RawMessage(`{"organization":"orgs/octo-org"}`)},
		{Description: "Read the limit of the account behind the token", Arguments: json.RawMessage(`{}`)},
	},
}

var interactionLimitsSet = capability.Descriptor{
	ID:      Provider + ".interaction-limits.set",
	Version: 1,
	Title:   "Set GitHub interaction limits",
	Description: "Limit, for a time, who may interact with one repository or, with organization instead, one " +
		"organization a connection allows, or, with neither, with the public repositories of the account behind " +
		"the token; sends one request that is never repeated and replaces a limit already set of that level; " +
		"GitHub refuses it while a limit of a wider level is set",
	Tags:         []string{"github", "interaction-limits", "set", "moderation"},
	Risk:         changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:     Provider,
	InputSchema:  inputSchema(interactionLimitKeys+`,"limit":`+enumSchema(interactionLimitValues)+`,"expiry":`+enumSchema(interactionExpiryValues), "limit"),
	OutputSchema: interactionLimitOutput(interactionLimitProperties),
	Arguments: append([]capability.Argument{
		{Name: "limit", Description: "existing_users (accounts older than 24 hours), contributors_only (accounts " +
			"that contributed before), or collaborators_only (collaborators only)", Required: true},
		{Name: "expiry", Description: "one_day, three_days, one_week, one_month, or six_months; GitHub applies " +
			"one_day when omitted"},
	}, interactionLimitTargetArguments...),
	Fields: append(append([]capability.Field{}, interactionLimitFields...), interactionLimitTargetFields...),
	Examples: []capability.Example{
		{Description: "Let only collaborators interact with a repository for a week",
			Arguments: json.RawMessage(`{"repository":"octo-org/example","limit":"collaborators_only","expiry":"one_week"}`)},
		{Description: "Let only existing users interact with an organization",
			Arguments: json.RawMessage(`{"organization":"orgs/octo-org","limit":"existing_users"}`)},
	},
}

var interactionLimitsRemove = capability.Descriptor{
	ID:      Provider + ".interaction-limits.remove",
	Version: 1,
	Title:   "Remove GitHub interaction limits",
	Description: "Remove the interaction limit of one repository or, with organization instead, one organization a " +
		"connection allows, or, with neither, of the account behind the token; sends one request that is never " +
		"repeated; a level without a limit stays as it is",
	Tags:         []string{"github", "interaction-limits", "remove", "moderation"},
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	Provider:     Provider,
	InputSchema:  inputSchema(interactionLimitKeys),
	OutputSchema: interactionLimitOutput(`"removed":{"type":"boolean"}`),
	Arguments:    append([]capability.Argument{}, interactionLimitTargetArguments...),
	Fields: append([]capability.Field{{Name: "removed", Description: "True once GitHub accepted the removal; a " +
		"limit of another level may still apply"}}, interactionLimitTargetFields...),
	Examples: []capability.Example{
		{Description: "Remove the limit of a repository", Arguments: json.RawMessage(`{"repository":"octo-org/example"}`)},
	},
}

// interactionLimitOutput is a result object with the given members and the target members, none required,
// since a read without a limit holds nothing else.
func interactionLimitOutput(properties string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` + properties + `,"repository":{"type":"string"},` +
		`"organization":{"type":"string"}},"additionalProperties":false}`)
}

// InteractionLimit is the limit of one level as GitHub reports it; every member is absent while none is set.
type InteractionLimit struct {
	Limit     *string `json:"limit,omitempty"`
	Origin    *string `json:"origin,omitempty"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

// InteractionLimitRemoved is the answer to a removal.
type InteractionLimitRemoved struct {
	Removed bool `json:"removed"`
}

// interactionLimitRaw is the answer of the limit routes. GitHub answers a level without a limit with an empty
// object, which a body of no content may stand for.
type interactionLimitRaw struct {
	Limit     *string `json:"limit"`
	Origin    *string `json:"origin"`
	ExpiresAt *string `json:"expires_at"`
}

func (*interactionLimitRaw) allowsEmptyBody() {}

func (r *interactionLimitRaw) project() *InteractionLimit {
	return &InteractionLimit{Limit: shortText(r.Limit), Origin: shortText(r.Origin), ExpiresAt: shortText(r.ExpiresAt)}
}

type interactionLimitArguments struct {
	Repository   string `json:"repository"`
	Organization string `json:"organization"`
	Limit        string `json:"limit"`
	Expiry       string `json:"expiry"`
}

func interactionLimitOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: interactionLimitsGet, Handler: interactionLimitHandler(interactionLimitsGet.ID, nil,
			func(ctx context.Context, c *Client, _ *interactionLimitArguments) (any, error) {
				return c.readInteractionLimit(ctx)
			})},
		{Descriptor: interactionLimitsSet, Handler: interactionLimitHandler(interactionLimitsSet.ID, checkInteractionLimit,
			func(ctx context.Context, c *Client, a *interactionLimitArguments) (any, error) {
				return c.setInteractionLimit(ctx, a)
			})},
		{Descriptor: interactionLimitsRemove, Handler: interactionLimitHandler(interactionLimitsRemove.ID, nil,
			func(ctx context.Context, c *Client, _ *interactionLimitArguments) (any, error) {
				return c.removeInteractionLimit(ctx)
			})},
	}
}

func checkInteractionLimit(a *interactionLimitArguments) error {
	if !contains(interactionLimitValues, a.Limit) {
		return invalidRequest("limit must be one of " + strings.Join(interactionLimitValues, ", "))
	}
	if a.Expiry != "" && !contains(interactionExpiryValues, a.Expiry) {
		return invalidRequest("expiry must be one of " + strings.Join(interactionExpiryValues, ", "))
	}
	return nil
}

// selectInteractionLimitTarget reads the repository or organization argument and resolves it against the
// connection's targets; with neither, the call addresses the account behind the token, which only a
// connection without a repository or project target and, with targets, with a users/LOGIN target may do; the
// login is checked after the credential, by requireOwnAccount. It runs before a credential is resolved.
func selectInteractionLimitTarget(resolved *config.Resolved, raw json.RawMessage) (bound target, account bool, err error) {
	if resolved == nil {
		return target{}, false, providerError("open", "no connection was selected")
	}
	var arguments interactionLimitArguments
	if json.Unmarshal(raw, &arguments) != nil {
		return target{}, false, unreadable("select interaction limit target")
	}
	hasRepository := strings.TrimSpace(arguments.Repository) != ""
	hasOrganization := strings.TrimSpace(arguments.Organization) != ""
	switch {
	case hasRepository && hasOrganization:
		return target{}, false, invalidRequest("give at most one of repository or organization, never both")
	case hasRepository || hasOrganization:
		bound, err = selectRulesetTarget(resolved, raw)
		return bound, false, err
	}
	allowed, err := allowlistOf(resolved)
	if err != nil {
		return target{}, false, providerError("open", err.Error())
	}
	if err := accountWideAllowed(allowed); err != nil {
		return target{}, true, err
	}
	if allowed.bound() && !allowed.namesUser("") {
		return target{}, true, invalidRequest("the targets of this connection name no user, so it may not address the " +
			"account behind its token; add the token's account as users/LOGIN to its targets or use another connection")
	}
	return target{}, true, nil
}

// interactionLimitHandler checks the target and the values before a credential is resolved, so a refused
// request never reaches a secret or GitHub.
func interactionLimitHandler(id string, check func(*interactionLimitArguments) error,
	call func(context.Context, *Client, *interactionLimitArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments interactionLimitArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, account, err := selectInteractionLimitTarget(resolved, raw)
		if err != nil {
			return nil, err
		}
		if check != nil {
			if err := check(&arguments); err != nil {
				return nil, err
			}
		}
		if account {
			client, err := Open(ctx, resolved, secrets, red)
			if err != nil {
				return nil, err
			}
			if err := client.requireOwnAccount(ctx, id); err != nil {
				return nil, err
			}
			return call(ctx, client, &arguments)
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.rulesetLocate(call(ctx, client, &arguments))
	}
}

// interactionLimitPath is the limit route of the bound repository or organization, or of the token's account
// while no target is bound.
func (c *Client) interactionLimitPath() string {
	switch c.target.kind {
	case kindRepository:
		return c.repoPath("interaction-limits")
	case kindOwner:
		return c.orgPath("interaction-limits")
	}
	return "/user/interaction-limits"
}

// interactionLimitPermission names what a request needs without claiming what the configured token holds.
func interactionLimitPermission(t target, change bool) string {
	access := "read"
	if change {
		access = "read and write"
	}
	switch t.kind {
	case kindRepository:
		return "GitHub refused this token the interaction limit of this repository; it needs repo on a classic " +
			"token of a repository administrator, or Administration: " + access + " on a fine-grained token"
	case kindOwner:
		return "GitHub refused this token the interaction limit of this organization; it needs admin:org on a " +
			"classic token, or Administration: " + access + " of the organization on a fine-grained token"
	}
	return "GitHub refused this token the interaction limit of its account; it needs repo or user on a classic " +
		"token, or, as far as GitHub documents it, Interaction limits: " + access + " on a fine-grained token"
}

const (
	interactionLimitConflict = "GitHub refused the interaction limit in the current state; a limit of a wider " +
		"level (the organization or the account) may be set and has to go first, so nothing was changed"
	interactionLimitRejected = "GitHub rejected the interaction limit as invalid for this level; the repository " +
		"or account may not allow it, so nothing was changed"
)

func (c *Client) readInteractionLimit(ctx context.Context) (*InteractionLimit, error) {
	const op = "get interaction limit"
	var raw interactionLimitRaw
	var status int
	err := c.do(ctx, op, http.MethodGet, c.endpoints.rest+c.interactionLimitPath(), nil, &raw, false, nil, &status)
	if err != nil {
		return nil, actionsFailure(err, interactionLimitPermission(c.target, false))
	}
	return raw.project(), nil
}

// setInteractionLimit sends the limit once; GitHub answers with the limit now in force.
func (c *Client) setInteractionLimit(ctx context.Context, a *interactionLimitArguments) (*InteractionLimit, error) {
	const op = "set interaction limit"
	body := map[string]string{"limit": a.Limit}
	if a.Expiry != "" {
		body["expiry"] = a.Expiry
	}
	var raw interactionLimitRaw
	var status int
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, providerError(op, "the request could not be built")
	}
	err = c.do(ctx, op, http.MethodPut, c.endpoints.rest+c.interactionLimitPath(), payload, &raw, true, nil, &status)
	if err != nil {
		return nil, variableFailure(err, interactionLimitPermission(c.target, true), interactionLimitConflict,
			interactionLimitRejected)
	}
	return raw.project(), nil
}

// removeInteractionLimit sends the removal once.
func (c *Client) removeInteractionLimit(ctx context.Context) (*InteractionLimitRemoved, error) {
	const op = "remove interaction limit"
	err := c.restChange(ctx, op, http.MethodDelete, c.interactionLimitPath(), struct{}{}, nil)
	if err != nil {
		return nil, variableFailure(err, interactionLimitPermission(c.target, true), interactionLimitConflict,
			interactionLimitRejected)
	}
	return &InteractionLimitRemoved{Removed: true}, nil
}
