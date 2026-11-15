package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// GitHub Actions secrets of a repository, of one environment of a repository, or of an organization. The
// read tools return names and metadata only; no output schema has a member for a value, and the answers are
// decoded into structs without one, so a value a server adds is dropped. github.action-secrets.set takes the
// value from a local file (see actionsecretsset.go) and never returns it. The target follows the Actions
// variable tools: exactly one of repository or organization, environment only with repository, checked before
// a credential is resolved. github.action-secrets.delete and .set are offered only where a connection's tools
// list names them. The helpers below take a secretFamily so a second secret family with the same routes can
// share them.

// secretSensitivity classifies a secret tool: the answer holds only names and times, but the name and the
// reach of a secret still describe what a repository or an organization can do.
const secretSensitivity = "github-actions-secret"

// secretFamily names one family of secrets: the REST segment below a repository or an organization and the
// label of messages, the fine-grained permission that grants them, and whether the family has secrets of an
// environment: Dependabot has none, so its path never takes the environment route.
type secretFamily struct {
	route, label, grant string
	environments        bool
}

var actionSecretFamily = secretFamily{route: "actions", label: "Actions", grant: "Secrets", environments: true}

const secretNameSchema = variableNameSchema

const secretListLimitSchema = limitSchema

const secretProperties = `"name":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"},` +
	`"visibility":{"type":"string"},"selected_repositories_url":{"type":"string"}`

const secretRequired = `"required":["name"],"additionalProperties":false`

// secretListOutputFor, secretOutputFor, and secretDeletedOutputFor give the output schemas for the target
// members a family names: Dependabot answers no environment.
func secretListOutputFor(targets string) string {
	return `{"type":"object","properties":{"secrets":{"type":"array","items":{"type":"object",` +
		`"properties":{` + secretProperties + `},` + secretRequired + `}},"total_count":{"type":"integer"},` +
		`"next_cursor":{"type":"string"},"has_more":{"type":"boolean"},` + targets + `},` +
		`"required":["secrets","total_count","has_more"],"additionalProperties":false}`
}

func secretOutputFor(targets string) string {
	return `{"type":"object","properties":{` + secretProperties + `,` + targets + `},` + secretRequired + `}`
}

func secretDeletedOutputFor(targets string) string {
	return `{"type":"object","properties":{"deleted":{"type":"boolean"},"name":{"type":"string"},` + targets +
		`},"required":["deleted","name"],"additionalProperties":false}`
}

var secretListOutput = secretListOutputFor(variableTargetProperties)

var secretOutput = secretOutputFor(variableTargetProperties)

var secretRepositoriesOutput = `{"type":"object","properties":{"repositories":{"type":"array","items":` +
	restRepositorySchema + `},"total_count":{"type":"integer"},"withheld":{"type":"integer"},` +
	`"next_cursor":{"type":"string"},"has_more":{"type":"boolean"},"organization":{"type":"string"}},` +
	`"required":["repositories","total_count","has_more"],"additionalProperties":false}`

var secretDeletedOutput = secretDeletedOutputFor(variableTargetProperties)

var secretMemberFields = secretMembersOf("github.action-secret-repositories.list")

func secretMembersOf(repositoriesTool string) []capability.Field {
	return []capability.Field{
		{Name: "name", Description: "Name of the secret as GitHub stores it, untrusted data; the value is never read"},
		{Name: "created_at", Description: "Creation time"},
		{Name: "updated_at", Description: "Time of the last change"},
		{Name: "visibility", Description: "For an organization secret: all, private, or selected repositories"},
		{Name: "selected_repositories_url", Description: "For an organization secret: REST address of its selected " +
			"repositories, untrusted data, never fetched; " + repositoriesTool + " reads them"},
	}
}

var secretNameArgument = capability.Argument{Name: "name", Description: "Name of the secret: letters, digits, " +
	"and _, not starting with a digit or GITHUB_, compared by GitHub without case", Required: true}

var actionSecretsList = capability.Descriptor{
	ID:      Provider + ".action-secrets.list",
	Version: 1,
	Title:   "List GitHub Actions secrets",
	Description: "List one bounded batch of the names and times of the GitHub Actions secrets of a repository, of " +
		"one environment of it, or, with organization instead, of an organization a connection allows; never " +
		"a value; exactly one of repository or organization is required",
	Tags:         []string{"github", "actions", "secrets", "list"},
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, secretSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(variableTargetKeys + `,"limit":` + secretListLimitSchema + `,"cursor":` + cursorSchema),
	OutputSchema: json.RawMessage(secretListOutput),
	Arguments:    append(append([]capability.Argument{}, variableTargetArguments...), pagingArguments...),
	Fields: variableFieldsWith(
		capability.Field{Name: "secrets", Description: "Secrets: name, created_at, updated_at, and, for an " +
			"organization, visibility and selected_repositories_url; names are untrusted data"},
		capability.Field{Name: "total_count", Description: "Number of secrets GitHub reports at this level"},
		pagingFields[0], pagingFields[1]),
	Examples: []capability.Example{{
		Description: "List the secrets of a repository",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example"}`),
	}, {
		Description: "List the secrets of an environment",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","environment":"production"}`),
	}, {
		Description: "List the secrets of an organization",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org"}`),
	}},
}

var actionSecretsGet = capability.Descriptor{
	ID:      Provider + ".action-secrets.get",
	Version: 1,
	Title:   "Get a GitHub Actions secret",
	Description: "Read the name and times of one GitHub Actions secret of a repository, of one environment of it, " +
		"or, with organization instead, of an organization a connection allows, by name; never the value; " +
		"exactly one of repository or organization is required",
	Tags:         []string{"github", "actions", "secrets", "get"},
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, secretSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(variableTargetKeys+`,"name":`+secretNameSchema, "name"),
	OutputSchema: json.RawMessage(secretOutput),
	Arguments:    append([]capability.Argument{secretNameArgument}, variableTargetArguments...),
	Fields:       variableFieldsWith(secretMemberFields...),
	Examples: []capability.Example{{
		Description: "Read one secret of a repository",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","name":"DEPLOY_TOKEN"}`),
	}},
}

var actionSecretRepositoriesList = capability.Descriptor{
	ID:      Provider + ".action-secret-repositories.list",
	Version: 1,
	Title:   "List the repositories of a GitHub Actions organization secret",
	Description: "List one bounded batch of the repositories that may use an organization's GitHub Actions secret " +
		"whose visibility is selected; the organization is required as orgs/LOGIN and must be an owner target " +
		"the connection allows; when the connection's targets name any, a repository outside them or in an " +
		"excluded repository is withheld and counted in withheld instead of being listed",
	Tags:         []string{"github", "actions", "secrets", "repositories", "list"},
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, secretSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(`"organization":`+ownerSchema+`,"name":`+secretNameSchema+`,"limit":`+secretListLimitSchema+`,"cursor":`+cursorSchema, "organization", "name"),
	OutputSchema: json.RawMessage(secretRepositoriesOutput),
	Arguments: append([]capability.Argument{secretNameArgument, {Name: "organization", Description: "Organization " +
		"as orgs/LOGIN whose secret this call addresses; must be an owner target the connection allows when it " +
		"lists any", Required: true}}, pagingArguments...),
	Fields: []capability.Field{
		{Name: "repositories", Description: "Repositories that may use the secret, each with the members of a " +
			"repository answer; a repository outside the connection's targets is not listed"},
		{Name: "total_count", Description: "Number of repositories GitHub reports for the secret, withheld ones " +
			"included"},
		{Name: "withheld", Description: "Count of repositories in this batch outside the connection's targets or " +
			"excluded, omitted when none were withheld"},
		pagingFields[0], pagingFields[1],
		{Name: "organization", Description: "Organization this call addressed, as orgs/LOGIN"},
	},
	Examples: []capability.Example{{
		Description: "List the repositories of an organization secret",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org","name":"DEPLOY_TOKEN"}`),
	}},
}

var actionSecretsDelete = capability.Descriptor{
	ID:      Provider + ".action-secrets.delete",
	Version: 1,
	Title:   "Delete a GitHub Actions secret",
	Description: "Delete one GitHub Actions secret of a repository, one environment of it, or, with " +
		"organization instead, an organization a connection allows, permanently; exactly one of repository " +
		"or organization is required; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "actions", "secrets", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, secretSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(variableTargetKeys+`,"name":`+secretNameSchema, "name"),
	OutputSchema:          json.RawMessage(secretDeletedOutput),
	Arguments:             append([]capability.Argument{secretNameArgument}, variableTargetArguments...),
	Fields: variableFieldsWith(
		capability.Field{Name: "deleted", Description: "True once GitHub deleted the secret"},
		capability.Field{Name: "name", Description: "Name of the deleted secret"}),
	Examples: []capability.Example{{
		Description: "Delete an organization secret",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org","name":"DEPLOY_TOKEN"}`),
	}},
}

// actionSecretsOperations binds every Actions secret tool to its handler.
func actionSecretsOperations() []capability.Operation {
	return append(secretOperations(actionSecretFamily, actionSecretsList, actionSecretsGet,
		actionSecretRepositoriesList, actionSecretsDelete), secretSetOperation(actionSecretFamily, actionSecretsSet))
}

// secretOperations binds the four tools of one secret family to their handlers.
func secretOperations(family secretFamily, list, get, repositories, remove capability.Descriptor) []capability.Operation {
	return []capability.Operation{
		{Descriptor: list, Handler: secretsHandler(family, list.ID, false, checkSecretsList,
			func(ctx context.Context, c *Client, a *secretArguments) (any, error) {
				return c.listSecrets(ctx, family, a)
			})},
		{Descriptor: get, Handler: secretsHandler(family, get.ID, false, checkSecretName,
			func(ctx context.Context, c *Client, a *secretArguments) (any, error) {
				return c.getSecret(ctx, family, a)
			})},
		{Descriptor: repositories, Handler: secretsHandler(family, repositories.ID, true, checkSecretRepositories,
			func(ctx context.Context, c *Client, a *secretArguments) (any, error) {
				return c.listSecretRepositories(ctx, family, a)
			})},
		{Descriptor: remove, Handler: secretsHandler(family, remove.ID, false, checkSecretName,
			func(ctx context.Context, c *Client, a *secretArguments) (any, error) {
				return c.deleteSecret(ctx, family, a)
			})},
	}
}

// secretArguments holds the arguments of every secret tool; the input schema of each admits only its own.
// environment, allowed, page, perPage, and binding are derived by the checks. Nothing here can hold a value.
type secretArguments struct {
	Name                 string    `json:"name"`
	Limit                int       `json:"limit"`
	Cursor               string    `json:"cursor"`
	LocalPath            *string   `json:"local_path"`
	Visibility           *string   `json:"visibility"`
	SelectedRepositories *[]string `json:"selected_repositories"`

	environment   string
	allowed       allowlist
	selected      []target
	page, perPage int
	binding       []byte
	// value is the content of the local file a set reads, held only for the call and cleared after it.
	value []byte
}

// secretsHandler decodes the arguments, selects the repository, environment, or organization, and applies
// every check before a credential is resolved, so a refused request never reaches a secret or GitHub.
// organizationOnly refuses a repository level, for the tool that reads an organization secret's repositories.
func secretsHandler(family secretFamily, id string, organizationOnly bool,
	check func(secretFamily, *secretArguments, target) error,
	call func(context.Context, *Client, *secretArguments) (any, error)) capability.Handler {
	return secretsHandlerReading(family, id, organizationOnly, check, nil, call)
}

// secretsHandlerReading is secretsHandler with a step between the checks and the credential: load, when not
// nil, reads what the call needs from the local disk, so a refused file ends the call before any secret or
// request, and whatever it left in the arguments is cleared when the call ends.
func secretsHandlerReading(family secretFamily, id string, organizationOnly bool,
	check func(secretFamily, *secretArguments, target) error,
	load func(context.Context, *config.Resolved, *secretArguments) error,
	call func(context.Context, *Client, *secretArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments secretArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		defer func() { clear(arguments.value) }()
		bound, environment, err := selectVariableTarget(resolved, raw, "secret")
		if err != nil {
			return nil, err
		}
		if organizationOnly && bound.kind != kindOwner {
			return nil, invalidRequest("organization is required as orgs/LOGIN")
		}
		if environment != "" && !family.environments {
			return nil, invalidRequest("environment is not available for " + family.label + " secrets")
		}
		arguments.environment = environment
		allowed, err := allowlistOf(resolved)
		if err != nil {
			return nil, providerError("open", err.Error())
		}
		arguments.allowed = allowed
		if err := check(family, &arguments, bound); err != nil {
			return nil, err
		}
		if load != nil {
			if err := load(ctx, resolved, &arguments); err != nil {
				return nil, err
			}
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		value, err := call(ctx, client, &arguments)
		return bound.variableLocate(environment, value, err)
	}
}

// checkSecretsList checks the batch size and binds the cursor to the level, the environment, and the family.
func checkSecretsList(family secretFamily, a *secretArguments, bound target) error {
	return pageSecrets(a, fingerprint("secrets", family.route, "list", bound.String(), a.environment))
}

// checkSecretRepositories checks the secret name and the paging of the repository list of one secret.
func checkSecretRepositories(family secretFamily, a *secretArguments, bound target) error {
	if err := checkVariableNameValue("name", a.Name); err != nil {
		return err
	}
	return pageSecrets(a, fingerprint("secrets", family.route, "repositories", bound.String(),
		strings.ToUpper(a.Name)))
}

func pageSecrets(a *secretArguments, binding []byte) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = binding
	a.page, a.perPage, err = pageOf(binding, a.Cursor, limit)
	return err
}

// checkSecretName applies GitHub's naming rules for secrets, the same as for variables, so a name is one plain
// path segment of letters, digits, and _.
func checkSecretName(_ secretFamily, a *secretArguments, _ target) error {
	return checkVariableNameValue("name", a.Name)
}

// path is the REST route of the secrets of the bound repository, its environment, or the bound organization,
// or of one secret by name. The environment and the name are each escaped as one opaque path segment, so
// neither can address another route.
func (f secretFamily) path(c *Client, environment, name string) string {
	var path string
	switch {
	case c.target.kind == kindOwner:
		path = c.orgPath(f.route + "/secrets")
	case environment != "" && f.environments:
		path = c.repoPath("environments/" + url.PathEscape(environment) + "/secrets")
	default:
		path = c.repoPath(f.route + "/secrets")
	}
	if name != "" {
		path += "/" + url.PathEscape(name)
	}
	return path
}

// permission names what a refused secret request needs, by level, without claiming what the configured token
// holds; the fine-grained permissions follow GitHub's own permission tables.
func (f secretFamily) permission(t target, change bool) string {
	if change {
		return f.changePermission(t, "deleting them needs")
	}
	return f.changePermission(t, "")
}

// changePermission is permission for a request that changes secrets; verb says which change needs the right.
// An empty verb names a read.
func (f secretFamily) changePermission(t target, verb string) string {
	access := "read and write"
	if verb == "" {
		access, verb = "read", "reading them needs"
	}
	if t.kind == kindOwner {
		return "GitHub refused this token the " + f.label + " secrets of this organization; " + verb + " admin:org " +
			"on a classic token, and repo as well where a private repository is involved, or the organization " +
			"permission " + f.grant + ": " + access + " on a fine-grained token"
	}
	return "GitHub refused this token the " + f.label + " secrets of this repository or environment; " + verb +
		" repo on a classic token, or " + f.grant + ": " + access + " on a fine-grained token"
}

// secretRaw is a secret as GitHub answers it. It has no member for a value, so none can be decoded.
type secretRaw struct {
	Name                    *string `json:"name"`
	CreatedAt               *string `json:"created_at"`
	UpdatedAt               *string `json:"updated_at"`
	Visibility              *string `json:"visibility"`
	SelectedRepositoriesURL *string `json:"selected_repositories_url"`
}

// ActionSecret is the name and the metadata of one GitHub Actions secret; GitHub never answers the value.
type ActionSecret struct {
	Name                    string  `json:"name"`
	CreatedAt               *string `json:"created_at,omitempty"`
	UpdatedAt               *string `json:"updated_at,omitempty"`
	Visibility              *string `json:"visibility,omitempty"`
	SelectedRepositoriesURL *string `json:"selected_repositories_url,omitempty"`
}

// view projects a secret, or reports false for one without a name.
func (r secretRaw) view() (ActionSecret, bool) {
	name, _ := cleanText(filled(r.Name), maxVariableNameLength, false)
	if name == nil {
		return ActionSecret{}, false
	}
	return ActionSecret{Name: *name, CreatedAt: nameText(r.CreatedAt), UpdatedAt: nameText(r.UpdatedAt),
		Visibility: nameText(r.Visibility), SelectedRepositoriesURL: address(r.SelectedRepositoriesURL)}, true
}

// ActionSecretList is one batch of secrets.
type ActionSecretList struct {
	Secrets    []ActionSecret `json:"secrets"`
	TotalCount int64          `json:"total_count"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
}

// ActionSecretRepositoryList is one batch of the repositories of an organization secret. Withheld counts those
// outside the connection's targets, which are not listed.
type ActionSecretRepositoryList struct {
	Repositories []RESTRepository `json:"repositories"`
	TotalCount   int64            `json:"total_count"`
	Withheld     int              `json:"withheld,omitempty"`
	NextCursor   string           `json:"next_cursor,omitempty"`
	HasMore      bool             `json:"has_more"`
}

// ActionSecretDeleted is the answer to a deleted secret.
type ActionSecretDeleted struct {
	Deleted bool   `json:"deleted"`
	Name    string `json:"name"`
}

// hasMoreSecrets reports a following page; a short page ends the list whatever the reported total says.
func hasMoreSecrets(count, page, perPage int, total int64) bool {
	return count == perPage && int64(page*perPage) < total && page < maxPage
}

func (c *Client) listSecrets(ctx context.Context, f secretFamily, a *secretArguments) (*ActionSecretList, error) {
	op := "list " + f.label + " secrets"
	var raw struct {
		TotalCount int64       `json:"total_count"`
		Secrets    []secretRaw `json:"secrets"`
	}
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if err := c.rest(ctx, op, f.path(c, a.environment, "")+"?"+query.Encode(), &raw); err != nil {
		return nil, actionsFailure(err, f.permission(c.target, false))
	}
	result := &ActionSecretList{Secrets: make([]ActionSecret, 0, len(raw.Secrets)), TotalCount: raw.TotalCount}
	for _, entry := range raw.Secrets {
		secret, ok := entry.view()
		if !ok {
			return nil, invalidEntry(op, "a secret")
		}
		result.Secrets = append(result.Secrets, secret)
	}
	if hasMoreSecrets(len(raw.Secrets), a.page, a.perPage, raw.TotalCount) {
		result.HasMore = true
		result.NextCursor = encodeCursor(a.binding, strconv.Itoa(a.page+1)+":"+strconv.Itoa(a.perPage))
	}
	return result, nil
}

func (c *Client) getSecret(ctx context.Context, f secretFamily, a *secretArguments) (*ActionSecret, error) {
	op := "get " + f.label + " secret"
	var raw secretRaw
	if err := c.rest(ctx, op, f.path(c, a.environment, a.Name), &raw); err != nil {
		return nil, actionsFailure(err, f.permission(c.target, false))
	}
	secret, ok := raw.view()
	if !ok {
		return nil, invalidEntry(op, "a secret")
	}
	return &secret, nil
}

func (c *Client) listSecretRepositories(ctx context.Context, f secretFamily, a *secretArguments) (*ActionSecretRepositoryList, error) {
	op := "list the repositories of " + f.label + " secret"
	var raw struct {
		TotalCount   int64           `json:"total_count"`
		Repositories []repositoryRaw `json:"repositories"`
	}
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if err := c.rest(ctx, op, f.path(c, "", a.Name)+"/repositories?"+query.Encode(), &raw); err != nil {
		return nil, actionsFailure(err, f.permission(c.target, false))
	}
	result := &ActionSecretRepositoryList{Repositories: make([]RESTRepository, 0, len(raw.Repositories)),
		TotalCount: raw.TotalCount}
	for i := range raw.Repositories {
		entry := &raw.Repositories[i]
		if !entry.admitted(a.allowed) {
			result.Withheld++
			continue
		}
		result.Repositories = append(result.Repositories, *entry.project(a.allowed))
	}
	if hasMoreSecrets(len(raw.Repositories), a.page, a.perPage, raw.TotalCount) {
		result.HasMore = true
		result.NextCursor = encodeCursor(a.binding, strconv.Itoa(a.page+1)+":"+strconv.Itoa(a.perPage))
	}
	return result, nil
}

// deleteSecret deletes one secret in a single request that is never repeated.
func (c *Client) deleteSecret(ctx context.Context, f secretFamily, a *secretArguments) (*ActionSecretDeleted, error) {
	op := "delete " + f.label + " secret"
	if err := c.restChange(ctx, op, http.MethodDelete, f.path(c, a.environment, a.Name), nil, nil); err != nil {
		return nil, actionsFailure(err, f.permission(c.target, true))
	}
	return &ActionSecretDeleted{Deleted: true, Name: a.Name}, nil
}

// secretSubject names the secrets a path below a repository or an organization addresses: actions/secrets
// or environments/ENV/secrets, with or without a secret name, and below an organization the repositories of
// one secret. It names only a secret name of the allowed form and an environment name of plain text, and is
// empty for any other path. The public key route of a level is named as such.
func (f secretFamily) subject(path string) string {
	collection, member, of := "the "+f.label+" secrets", f.label+" secret", ""
	rest, ok := strings.CutPrefix(path, f.route+"/secrets")
	if !ok {
		if !f.environments {
			return ""
		}
		tail, found := strings.CutPrefix(path, "environments/")
		segment, after, cut := strings.Cut(tail, "/")
		if !found || !cut {
			return ""
		}
		if rest, ok = strings.CutPrefix(after, "secrets"); !ok {
			return ""
		}
		of = " of an environment"
		if environment, err := url.PathUnescape(segment); err == nil && checkVariableEnvironment(environment) == nil {
			of = " of environment " + environment
		}
		collection, member = "the secrets"+of, "secret"
	}
	if rest == "" {
		return collection
	}
	name, ok := strings.CutPrefix(rest, "/")
	if !ok {
		return ""
	}
	name, repositories := strings.CutSuffix(name, "/repositories")
	what := "a secret" + of
	if name == "public-key" && !repositories {
		return "the public key of the secrets" + of
	}
	if unescaped, err := url.PathUnescape(name); err == nil && validVariableName(unescaped) {
		what = member + " " + unescaped + of
	}
	if repositories {
		return "the repositories of " + what
	}
	if strings.Contains(name, "/") {
		return ""
	}
	return what
}
