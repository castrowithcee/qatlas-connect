package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// GitHub Actions variables of a repository, of one environment of a repository, or of an organization; never
// secrets. Every call addresses exactly one of repository or organization, the same exclusive selection the
// ruleset tools use, and environment narrows a repository call to that environment; it never comes with an
// organization. github.action-variables.list and .get read variables, .create and .update change them with
// their own confirmation, and .delete is offered only where a connection's tools list names it. An
// organization variable restricted to selected repositories takes them as OWNER/REPO: each must belong to the
// organization and lie inside the connection's repository targets, checked before a credential is resolved,
// and is resolved to its numeric identifier only afterwards, so a caller never hands GitHub a free identifier.

// variableSensitivity classifies an Actions variable: GitHub keeps its value as plain text that workflows read,
// and a value may still carry data its owner did not mean to publish.
const variableSensitivity = "github-actions-variable"

// Bounds of an Actions variable. GitHub documents 48 KB per value, case-insensitive names of letters, digits,
// and _, and environment names of up to 255 characters (maxEnvironmentNameLength); it documents no name
// length, so 255 bounds it here.
// A list page holds at most 30 variables, the most GitHub serves on these routes.
const (
	maxVariableNameLength   = 255
	maxVariableValueBytes   = 48 << 10
	maxSelectedRepositories = 50
	maxVariablesPerPage     = 30
	variableListValueLimit  = 1000
	variableValueLimit      = 20000
)

const (
	variableNamePattern = `^[A-Za-z_][A-Za-z0-9_]*$`
	variableNameSchema  = `{"type":"string","minLength":1,"maxLength":255,"pattern":"` + variableNamePattern + `",` +
		`"x-form":"letters, digits, and _, not starting with a digit or GITHUB_"}`
	variableValueSchema        = `{"type":"string","minLength":1,"maxLength":49152}`
	variableVisibilitySchema   = `{"type":"string","enum":["all","private","selected"]}`
	selectedRepositoriesSchema = `{"type":"array","maxItems":50,"uniqueItems":true,"items":` + repoSchema + `}`
	variableLimitSchema        = `{"type":"integer","minimum":1,"maximum":30}`
	variableTargetKeys         = `"repository":` + repoSchema + `,"environment":` + environmentNameSchema +
		`,"organization":` + ownerSchema
)

var variableTargetArguments = []capability.Argument{
	{Name: "repository", Description: "Repository as OWNER/REPO whose variables this call addresses; exactly one " +
		"of repository or organization is required, never both; must lie inside the targets when the " +
		"connection lists any"},
	{Name: "environment", Description: "Environment of repository whose variables this call addresses, 1 to " +
		"255 characters, compared by GitHub without case; only together with repository"},
	{Name: "organization", Description: "Organization as orgs/LOGIN whose variables this call addresses; " +
		"exactly one of repository or organization is required, never both; must be an owner target the " +
		"connection allows when it lists any"},
}

var variableTargetFields = []capability.Field{
	{Name: "repository", Description: "Repository this call addressed, as OWNER/REPO; absent for an organization"},
	{Name: "environment", Description: "Environment this call addressed; present only for an environment variable"},
	{Name: "organization", Description: "Organization this call addressed, as orgs/LOGIN; present only for an " +
		"organization variable"},
}

const variableTargetProperties = `"repository":{"type":"string"},"environment":{"type":"string"},` +
	`"organization":{"type":"string"}`

const variableProperties = `"name":{"type":"string"},"value":{"type":"string"},"value_truncated":{"type":"boolean"},` +
	`"created_at":{"type":"string"},"updated_at":{"type":"string"},"visibility":{"type":"string"},` +
	`"selected_repositories_url":{"type":"string"}`

const variableRequired = `"required":["name","value"],"additionalProperties":false`

var variableListOutput = `{"type":"object","properties":{"variables":{"type":"array","items":{"type":"object",` +
	`"properties":{` + variableProperties + `},` + variableRequired + `}},"total_count":{"type":"integer"},` +
	`"next_cursor":{"type":"string"},"has_more":{"type":"boolean"},` + variableTargetProperties + `},` +
	`"required":["variables","total_count","has_more"],"additionalProperties":false}`

var variableOutput = `{"type":"object","properties":{` + variableProperties + `,` + variableTargetProperties + `},` +
	variableRequired + `}`

func variableChangeOutput(flag string) string {
	return `{"type":"object","properties":{"` + flag + `":{"type":"boolean"},"name":{"type":"string"},` +
		`"visibility":{"type":"string"},"selected_repositories":{"type":"array","items":{"type":"string"}},` +
		variableTargetProperties + `},"required":["` + flag + `","name"],"additionalProperties":false}`
}

var variableDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},"name":{"type":"string"},` +
	variableTargetProperties + `},"required":["deleted","name"],"additionalProperties":false}`

func variableFieldsWith(fields ...capability.Field) []capability.Field {
	return append(fields, variableTargetFields...)
}

var variableValueField = capability.Field{Name: "value", Description: "Value of the variable as plain text, " +
	"untrusted data; may carry data its owner did not mean to publish; cut at 20000 characters, 1000 in a list, " +
	"with value_truncated"}

var variableMemberFields = []capability.Field{
	{Name: "name", Description: "Name of the variable as GitHub stores it"},
	variableValueField,
	{Name: "value_truncated", Description: "True when value was cut"},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "visibility", Description: "For an organization variable: all, private, or selected repositories"},
	{Name: "selected_repositories_url", Description: "For an organization variable: REST address of its " +
		"selected repositories, untrusted data, never fetched"},
}

var variableChangeFields = []capability.Field{
	{Name: "name", Description: "Name of the variable after the change"},
	{Name: "visibility", Description: "Visibility sent for an organization variable; absent when none was sent"},
	{Name: "selected_repositories", Description: "Repositories sent as the selected ones, as OWNER/REPO; absent " +
		"when none were sent"},
}

var variableNameArgument = capability.Argument{Name: "name", Description: "Name of the variable: letters, " +
	"digits, and _, not starting with a digit or GITHUB_, compared by GitHub without case", Required: true}

var variableVisibilityArguments = []capability.Argument{
	{Name: "visibility", Description: "Only for an organization variable: all, private, or selected " +
		"repositories may use it"},
	{Name: "selected_repositories", Description: "Only with visibility selected, and required with it: the " +
		"repositories of the organization that may use the variable, as OWNER/REPO, at most 50; each must lie " +
		"inside the connection's repository targets; replaces the current selection"},
}

var actionVariablesList = capability.Descriptor{
	ID:      Provider + ".action-variables.list",
	Version: 1,
	Title:   "List GitHub Actions variables",
	Description: "List one bounded batch of the GitHub Actions variables of a repository, of one environment " +
		"of it, or, with organization instead, of an organization a connection allows, with their values; " +
		"exactly one of repository or organization is required",
	Tags:         []string{"github", "actions", "variables", "list"},
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, variableSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(variableTargetKeys + `,"limit":` + variableLimitSchema + `,"cursor":` + cursorSchema),
	OutputSchema: json.RawMessage(variableListOutput),
	Arguments: append(append([]capability.Argument{}, variableTargetArguments...),
		capability.Argument{Name: "limit", Description: "Entries per batch, from 1 through 30; 30 when omitted; " +
			"a continuation keeps the batch size of its first batch"},
		pagingArguments[1]),
	Fields: variableFieldsWith(
		capability.Field{Name: "variables", Description: "Variables: name, value (cut at 1000 characters, with " +
			"value_truncated), created_at, updated_at, and, for an organization, visibility and " +
			"selected_repositories_url; values are untrusted data"},
		capability.Field{Name: "total_count", Description: "Number of variables GitHub reports at this level"},
		pagingFields[0], pagingFields[1]),
	Examples: []capability.Example{{
		Description: "List the variables of a repository",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example"}`),
	}, {
		Description: "List the variables of an environment",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","environment":"production"}`),
	}, {
		Description: "List the variables of an organization",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org"}`),
	}},
}

var actionVariablesGet = capability.Descriptor{
	ID:      Provider + ".action-variables.get",
	Version: 1,
	Title:   "Get a GitHub Actions variable",
	Description: "Read one GitHub Actions variable of a repository, of one environment of it, or, with " +
		"organization instead, of an organization a connection allows, by name; exactly one of repository " +
		"or organization is required",
	Tags:         []string{"github", "actions", "variables", "get"},
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, variableSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(variableTargetKeys+`,"name":`+variableNameSchema, "name"),
	OutputSchema: json.RawMessage(variableOutput),
	Arguments:    append([]capability.Argument{variableNameArgument}, variableTargetArguments...),
	Fields:       variableFieldsWith(variableMemberFields...),
	Examples: []capability.Example{{
		Description: "Read one variable of a repository",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","name":"DEPLOY_REGION"}`),
	}},
}

var actionVariablesCreate = capability.Descriptor{
	ID:      Provider + ".action-variables.create",
	Version: 1,
	Title:   "Create a GitHub Actions variable",
	Description: "Create one GitHub Actions variable in a repository, one environment of it, or, with " +
		"organization instead, an organization a connection allows; exactly one of repository or " +
		"organization is required; an organization variable needs visibility; a name that already exists " +
		"is refused with a clear message, so a repeated call creates nothing new",
	Tags:     []string{"github", "actions", "variables", "create"},
	Risk:     guardedRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, variableSensitivity),
	Provider: Provider,
	InputSchema: inputSchema(variableTargetKeys+`,"name":`+variableNameSchema+`,"value":`+variableValueSchema+
		`,"visibility":`+variableVisibilitySchema+`,"selected_repositories":`+selectedRepositoriesSchema,
		"name", "value"),
	OutputSchema: json.RawMessage(variableChangeOutput("created")),
	Arguments: append(append([]capability.Argument{variableNameArgument,
		{Name: "value", Description: "Value of the variable, plain text of at most 48 KB; GitHub stores it " +
			"unencrypted, so a secret belongs in an Actions secret instead", Required: true},
	}, variableVisibilityArguments...), variableTargetArguments...),
	Fields: variableFieldsWith(append([]capability.Field{
		{Name: "created", Description: "True once GitHub created the variable"},
	}, variableChangeFields...)...),
	Examples: []capability.Example{{
		Description: "Create a repository variable",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","name":"DEPLOY_REGION","value":"eu-central-1"}`),
	}, {
		Description: "Create an organization variable for selected repositories",
		Arguments: json.RawMessage(`{"organization":"orgs/octo-org","name":"DEPLOY_REGION","value":"eu-central-1",` +
			`"visibility":"selected","selected_repositories":["octo-org/example"]}`),
	}},
}

var actionVariablesUpdate = capability.Descriptor{
	ID:      Provider + ".action-variables.update",
	Version: 1,
	Title:   "Update a GitHub Actions variable",
	Description: "Change the value or the name of one GitHub Actions variable of a repository, one " +
		"environment of it, or, with organization instead, an organization a connection allows, and for an " +
		"organization variable its visibility and selected repositories; exactly one of repository or " +
		"organization is required; idempotent, since sending the same values again leaves GitHub in the same state",
	Tags:     []string{"github", "actions", "variables", "update"},
	Risk:     guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, variableSensitivity),
	Provider: Provider,
	InputSchema: inputSchema(variableTargetKeys+`,"name":`+variableNameSchema+`,"new_name":`+variableNameSchema+
		`,"value":`+variableValueSchema+`,"visibility":`+variableVisibilitySchema+`,"selected_repositories":`+
		selectedRepositoriesSchema, "name"),
	OutputSchema: json.RawMessage(variableChangeOutput("updated")),
	Arguments: append(append([]capability.Argument{
		{Name: "name", Description: "Current name of the variable, compared by GitHub without case", Required: true},
		{Name: "new_name", Description: "New name for the variable, by the same rules as name"},
		{Name: "value", Description: "New value, plain text of at most 48 KB"},
	}, variableVisibilityArguments...), variableTargetArguments...),
	Fields: variableFieldsWith(append([]capability.Field{
		{Name: "updated", Description: "True once GitHub applied the change"},
	}, variableChangeFields...)...),
	Examples: []capability.Example{{
		Description: "Change the value of an environment variable",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","environment":"production",` +
			`"name":"DEPLOY_REGION","value":"eu-west-1"}`),
	}},
}

var actionVariablesDelete = capability.Descriptor{
	ID:      Provider + ".action-variables.delete",
	Version: 1,
	Title:   "Delete a GitHub Actions variable",
	Description: "Delete one GitHub Actions variable of a repository, one environment of it, or, with " +
		"organization instead, an organization a connection allows, permanently; exactly one of repository " +
		"or organization is required; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "actions", "variables", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, variableSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(variableTargetKeys+`,"name":`+variableNameSchema, "name"),
	OutputSchema:          json.RawMessage(variableDeletedOutput),
	Arguments:             append([]capability.Argument{variableNameArgument}, variableTargetArguments...),
	Fields: variableFieldsWith(
		capability.Field{Name: "deleted", Description: "True once GitHub deleted the variable"},
		capability.Field{Name: "name", Description: "Name of the deleted variable"}),
	Examples: []capability.Example{{
		Description: "Delete an organization variable",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org","name":"DEPLOY_REGION"}`),
	}},
}

// actionVariablesOperations binds every Actions variable tool to its handler.
func actionVariablesOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: actionVariablesList, Handler: variablesHandler(actionVariablesList.ID, checkVariablesList,
			func(ctx context.Context, c *Client, a *variableArguments) (any, error) {
				return c.listVariables(ctx, a)
			})},
		{Descriptor: actionVariablesGet, Handler: variablesHandler(actionVariablesGet.ID, checkVariableName,
			func(ctx context.Context, c *Client, a *variableArguments) (any, error) { return c.getVariable(ctx, a) })},
		{Descriptor: actionVariablesCreate, Handler: variablesHandler(actionVariablesCreate.ID, checkVariableCreate,
			func(ctx context.Context, c *Client, a *variableArguments) (any, error) {
				return c.createVariable(ctx, a)
			})},
		{Descriptor: actionVariablesUpdate, Handler: variablesHandler(actionVariablesUpdate.ID, checkVariableUpdate,
			func(ctx context.Context, c *Client, a *variableArguments) (any, error) {
				return c.updateVariable(ctx, a)
			})},
		{Descriptor: actionVariablesDelete, Handler: variablesHandler(actionVariablesDelete.ID, checkVariableName,
			func(ctx context.Context, c *Client, a *variableArguments) (any, error) {
				return c.deleteVariable(ctx, a)
			})},
	}
}

// variableArguments holds the arguments of every variable tool; the input schema of each admits only its own.
// value, new_name, visibility, and selected_repositories are pointers so a field left out differs from one
// sent. environment, selected, page, perPage, and binding are derived by the checks.
type variableArguments struct {
	Name                 string    `json:"name"`
	NewName              *string   `json:"new_name"`
	Value                *string   `json:"value"`
	Visibility           *string   `json:"visibility"`
	SelectedRepositories *[]string `json:"selected_repositories"`
	Limit                int       `json:"limit"`
	Cursor               string    `json:"cursor"`

	environment   string
	selected      []target
	page, perPage int
	binding       []byte
}

// variablesHandler decodes the arguments, selects the repository, environment, or organization, and applies
// every check before a credential is resolved, so a refused request never reaches a secret or GitHub.
func variablesHandler(id string, check func(*variableArguments, allowlist, target) error,
	call func(context.Context, *Client, *variableArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments variableArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, environment, err := selectVariableTarget(resolved, raw)
		if err != nil {
			return nil, err
		}
		arguments.environment = environment
		allowed, err := allowlistOf(resolved)
		if err != nil {
			return nil, providerError("open", err.Error())
		}
		if err := check(&arguments, allowed, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		value, err := call(ctx, client, &arguments)
		return bound.variableLocate(environment, value, err)
	}
}

// selectVariableTarget reads the exclusive repository or organization argument and the optional environment
// of a repository, the way selectRulesetTarget reads its own two: never both, never neither, and never an
// environment without a repository, so a call never falls back to a level the caller did not name.
func selectVariableTarget(resolved *config.Resolved, raw json.RawMessage) (target, string, error) {
	if resolved == nil {
		return target{}, "", providerError("open", "no connection was selected")
	}
	var arguments struct {
		Repository   string  `json:"repository"`
		Organization string  `json:"organization"`
		Environment  *string `json:"environment"`
	}
	if json.Unmarshal(raw, &arguments) != nil {
		return target{}, "", unreadable("select variable target")
	}
	hasRepository := strings.TrimSpace(arguments.Repository) != ""
	hasOrganization := strings.TrimSpace(arguments.Organization) != ""
	if arguments.Environment != nil && !hasRepository {
		return target{}, "", invalidRequest("environment needs repository and never comes with organization; " +
			"an organization variable has no environment")
	}
	if hasRepository == hasOrganization {
		return target{}, "", invalidRequest("give exactly one of repository or organization, never both or neither")
	}
	environment := ""
	if arguments.Environment != nil {
		environment = *arguments.Environment
		if err := checkVariableEnvironment(environment); err != nil {
			return target{}, "", err
		}
	}
	if hasRepository {
		bound, err := selectTarget(resolved, kindRepository, raw)
		return bound, environment, err
	}
	allowed, err := allowlistOf(resolved)
	if err != nil {
		return target{}, "", providerError("open", err.Error())
	}
	bound, _, err := parseAllowedOrganization(allowed, "organization", arguments.Organization)
	return bound, "", err
}

// checkVariableEnvironment bounds an environment name, which travels as one opaque path segment: one line of
// text without control characters. "." and ".." are refused because a dot segment could address another
// route once a server resolves the path.
func checkVariableEnvironment(name string) error {
	if !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return invalidRequest("environment must be one line of text without control characters")
	}
	if count := utf8.RuneCountInString(name); count < 1 || count > maxEnvironmentNameLength {
		return invalidRequest(fmt.Sprintf("environment must have 1 to %d characters", maxEnvironmentNameLength))
	}
	if name == "." || name == ".." {
		return invalidRequest("environment must not be . or ..")
	}
	return nil
}

// validVariableName applies GitHub's naming rules, which the input schema states only in part: letters,
// digits, and _, not starting with a digit, and no GITHUB_ prefix in any case.
func validVariableName(name string) bool {
	if name == "" || len(name) > maxVariableNameLength || strings.HasPrefix(strings.ToUpper(name), "GITHUB_") {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func checkVariableNameValue(field, name string) error {
	if !validVariableName(name) {
		return invalidRequest(field + " must hold only letters, digits, and _, must not start with a digit, " +
			"and must not start with GITHUB_")
	}
	return nil
}

// checkVariableValue bounds a value without ever quoting it: GitHub stores it as plain text of at most 48 KB.
func checkVariableValue(value string) error {
	if value == "" {
		return invalidRequest("value must not be empty")
	}
	if err := checkText("value", value); err != nil {
		return err
	}
	if len(value) > maxVariableValueBytes {
		return invalidRequest(fmt.Sprintf("value must be at most 48 KB (%d bytes of UTF-8)", maxVariableValueBytes))
	}
	return nil
}

func checkVariablesList(a *variableArguments, _ allowlist, bound target) error {
	limit := a.Limit
	if limit == 0 {
		limit = maxVariablesPerPage
	}
	if limit < 1 || limit > maxVariablesPerPage {
		return invalidRequest(fmt.Sprintf("limit must be between 1 and %d", maxVariablesPerPage))
	}
	a.binding = fingerprint("action-variables", "list", bound.String(), a.environment)
	var err error
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkVariableName(a *variableArguments, _ allowlist, _ target) error {
	return checkVariableNameValue("name", a.Name)
}

func checkVariableCreate(a *variableArguments, allowed allowlist, bound target) error {
	if err := checkVariableNameValue("name", a.Name); err != nil {
		return err
	}
	if a.Value == nil {
		return invalidRequest("value is required")
	}
	if err := checkVariableValue(*a.Value); err != nil {
		return err
	}
	if bound.kind == kindOwner && a.Visibility == nil {
		return invalidRequest("visibility is required for an organization variable: all, private, or selected")
	}
	return checkVariableVisibility(a, allowed, bound)
}

func checkVariableUpdate(a *variableArguments, allowed allowlist, bound target) error {
	if err := checkVariableNameValue("name", a.Name); err != nil {
		return err
	}
	if a.NewName == nil && a.Value == nil && a.Visibility == nil && a.SelectedRepositories == nil {
		return invalidRequest("give at least one of new_name, value, visibility, or selected_repositories to change")
	}
	if a.NewName != nil {
		if err := checkVariableNameValue("new_name", *a.NewName); err != nil {
			return err
		}
	}
	if a.Value != nil {
		if err := checkVariableValue(*a.Value); err != nil {
			return err
		}
	}
	return checkVariableVisibility(a, allowed, bound)
}

// checkVariableVisibility admits visibility and selected repositories only for an organization variable and
// binds every selected repository to the organization and the connection's repository targets. visibility
// selected always comes with the full list, so a call states exactly which repositories may read the value.
func checkVariableVisibility(a *variableArguments, allowed allowlist, bound target) error {
	if bound.kind != kindOwner {
		if a.Visibility != nil || a.SelectedRepositories != nil {
			return invalidRequest("visibility and selected_repositories apply only to an organization variable")
		}
		return nil
	}
	selected := a.Visibility != nil && *a.Visibility == "selected"
	if a.SelectedRepositories != nil && !selected {
		return invalidRequest("selected_repositories needs visibility selected in the same call")
	}
	if selected && a.SelectedRepositories == nil {
		return invalidRequest("visibility selected needs selected_repositories, the repositories that may use " +
			"the variable")
	}
	if !selected {
		return nil
	}
	if len(*a.SelectedRepositories) > maxSelectedRepositories {
		return invalidRequest(fmt.Sprintf("selected_repositories accepts at most %d entries", maxSelectedRepositories))
	}
	seen := map[string]bool{}
	a.selected = make([]target, 0, len(*a.SelectedRepositories))
	for _, value := range *a.SelectedRepositories {
		repository, err := parseArgument(kindRepository, value)
		if err != nil {
			return invalidRequest("selected_repositories entries must be OWNER/REPO")
		}
		if !strings.EqualFold(repository.owner, bound.owner) {
			return invalidRequest("selected_repositories must name repositories of the organization only")
		}
		if allowed.excluded(repository) {
			return invalidRequest("selected_repositories names a repository that is not available on this " +
				"connection; leave it out")
		}
		if !allowed.allows(repository) {
			return invalidRequest("selected_repositories names a repository outside the targets of this " +
				"connection; add it, or repos/LOGIN/* for every repository of the organization, to the targets")
		}
		key := strings.ToLower(repository.argument())
		if seen[key] {
			return invalidRequest("selected_repositories must not repeat a repository")
		}
		seen[key] = true
		a.selected = append(a.selected, repository)
	}
	return nil
}

// variableLocate adds the repository, the environment, or the organization a call addressed to its result,
// the way rulesetLocate does, with the environment under its own name.
func (t target) variableLocate(environment string, value any, err error) (any, error) {
	located, err := t.rulesetLocate(value, err)
	if err != nil || environment == "" {
		return located, err
	}
	fields, ok := located.(map[string]json.RawMessage)
	if !ok {
		return located, nil
	}
	fields["environment"], _ = json.Marshal(environment)
	return fields, nil
}

// variablesPath is the REST route of the variables of the bound repository, its environment, or the bound
// organization, or of one variable by name. The environment and the name are each escaped as one opaque
// path segment, so neither can address another route.
func (c *Client) variablesPath(environment, name string) string {
	var path string
	switch {
	case c.target.kind == kindOwner:
		path = c.orgPath("actions/variables")
	case environment != "":
		path = c.repoPath("environments/" + url.PathEscape(environment) + "/variables")
	default:
		path = c.repoPath("actions/variables")
	}
	if name != "" {
		path += "/" + url.PathEscape(name)
	}
	return path
}

// variablePermission names what a refused variable request needs, by level, without claiming what the
// configured token holds; the fine-grained permissions follow GitHub's own permission tables.
func variablePermission(t target, environment string, change bool) string {
	access, verb := "read", "reading them needs"
	if change {
		access, verb = "read and write", "changing them needs"
	}
	switch {
	case t.kind == kindOwner:
		return "GitHub refused this token the Actions variables of this organization; " + verb + " admin:org " +
			"on a classic token, and repo as well where a private repository is involved, or the organization " +
			"permission Variables: " + access + " on a fine-grained token"
	case environment != "":
		return "GitHub refused this token the variables of this environment; " + verb + " repo on a classic " +
			"token, or Environments: " + access + " on a fine-grained token"
	}
	return "GitHub refused this token the Actions variables of this repository; " + verb + " repo on a classic " +
		"token, or Variables: " + access + " on a fine-grained token"
}

const selectedRepositoryPermission = "GitHub refused this token a repository named in selected_repositories; " +
	"resolving it needs repo on a classic token for a private repository, or Metadata: read on a fine-grained " +
	"token with access to that repository"

// variableFailure turns a refused variable request into Qatlas' own message: the permission it needs, and
// for 409 and 422 what the conflict or the rejection means for this request. GitHub's text stays apart, in
// Original, and the value never reaches a message.
func variableFailure(err error, permission, conflict, rejected string) error {
	err = actionsFailure(err, permission)
	var failure *provider.Error
	if !errors.As(err, &failure) || failure.Original == nil {
		return err
	}
	message := ""
	switch failure.Original.Status {
	case http.StatusConflict:
		message = conflict
	case http.StatusUnprocessableEntity:
		message = rejected
	}
	if message == "" {
		return err
	}
	renamed := *failure
	renamed.Message = message
	return &renamed
}

const variableRejected = "GitHub rejected the variable as invalid; check the naming rules, a value of at most " +
	"48 KB, the limits on how many variables and how much data a repository, an environment, or an organization " +
	"may hold, and, for an organization, that every selected repository belongs to it"

// actionVariableRaw is a variable as GitHub answers it.
type actionVariableRaw struct {
	Name                    *string `json:"name"`
	Value                   *string `json:"value"`
	CreatedAt               *string `json:"created_at"`
	UpdatedAt               *string `json:"updated_at"`
	Visibility              *string `json:"visibility"`
	SelectedRepositoriesURL *string `json:"selected_repositories_url"`
}

// ActionVariable is one GitHub Actions variable. Value is untrusted data, cut with ValueTruncated.
type ActionVariable struct {
	Name                    string  `json:"name"`
	Value                   string  `json:"value"`
	ValueTruncated          bool    `json:"value_truncated,omitempty"`
	CreatedAt               *string `json:"created_at,omitempty"`
	UpdatedAt               *string `json:"updated_at,omitempty"`
	Visibility              *string `json:"visibility,omitempty"`
	SelectedRepositoriesURL *string `json:"selected_repositories_url,omitempty"`
}

// view projects a variable with its value cut at limit runes, or reports false for one without a name.
func (r actionVariableRaw) view(limit int) (ActionVariable, bool) {
	name, _ := cleanText(filled(r.Name), maxVariableNameLength, false)
	if name == nil {
		return ActionVariable{}, false
	}
	variable := ActionVariable{Name: *name, CreatedAt: nameText(r.CreatedAt), UpdatedAt: nameText(r.UpdatedAt),
		Visibility: nameText(r.Visibility), SelectedRepositoriesURL: address(r.SelectedRepositoriesURL)}
	if value, cut := cleanText(r.Value, limit, true); value != nil {
		variable.Value, variable.ValueTruncated = *value, cut
	}
	return variable, true
}

// ActionVariableList is one batch of variables.
type ActionVariableList struct {
	Variables  []ActionVariable `json:"variables"`
	TotalCount int64            `json:"total_count"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
}

// ActionVariableChange is what a create or an update sent, never the value; GitHub answers both without the
// variable.
type ActionVariableChange struct {
	Name                 string   `json:"name"`
	Visibility           string   `json:"visibility,omitempty"`
	SelectedRepositories []string `json:"selected_repositories,omitempty"`
}

// ActionVariableCreated is the answer to a created variable.
type ActionVariableCreated struct {
	Created bool `json:"created"`
	ActionVariableChange
}

// ActionVariableUpdated is the answer to a changed variable.
type ActionVariableUpdated struct {
	Updated bool `json:"updated"`
	ActionVariableChange
}

// ActionVariableDeleted is the answer to a deleted variable.
type ActionVariableDeleted struct {
	Deleted bool   `json:"deleted"`
	Name    string `json:"name"`
}

func (c *Client) listVariables(ctx context.Context, a *variableArguments) (*ActionVariableList, error) {
	const op = "list action variables"
	var raw struct {
		TotalCount int64               `json:"total_count"`
		Variables  []actionVariableRaw `json:"variables"`
	}
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if err := c.rest(ctx, op, c.variablesPath(a.environment, "")+"?"+query.Encode(), &raw); err != nil {
		return nil, variableFailure(err, variablePermission(c.target, a.environment, false), "", "")
	}
	result := &ActionVariableList{Variables: make([]ActionVariable, 0, len(raw.Variables)), TotalCount: raw.TotalCount}
	for _, entry := range raw.Variables {
		variable, ok := entry.view(variableListValueLimit)
		if !ok {
			return nil, invalidEntry(op, "a variable")
		}
		result.Variables = append(result.Variables, variable)
	}
	// A short page ends the list whatever the reported total says.
	if len(raw.Variables) == a.perPage && int64(a.page*a.perPage) < raw.TotalCount && a.page < maxPage {
		result.HasMore = true
		result.NextCursor = encodeCursor(a.binding, strconv.Itoa(a.page+1)+":"+strconv.Itoa(a.perPage))
	}
	return result, nil
}

func (c *Client) getVariable(ctx context.Context, a *variableArguments) (*ActionVariable, error) {
	const op = "get action variable"
	var raw actionVariableRaw
	if err := c.rest(ctx, op, c.variablesPath(a.environment, a.Name), &raw); err != nil {
		return nil, variableFailure(err, variablePermission(c.target, a.environment, false), "", "")
	}
	variable, ok := raw.view(variableValueLimit)
	if !ok {
		return nil, invalidEntry(op, "a variable")
	}
	return &variable, nil
}

// selectedRepositoryIDs resolves the selected repositories, already checked against the organization and the
// connection's targets, to the identifiers GitHub's body takes, one read each and before any change. A
// repository GitHub reports under another name, such as after a rename or a transfer, is refused.
func (c *Client) selectedRepositoryIDs(ctx context.Context, op string, selected []target) ([]int64, error) {
	ids := make([]int64, 0, len(selected))
	for _, repository := range selected {
		var raw struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		}
		path := "/repos/" + url.PathEscape(repository.owner) + "/" + url.PathEscape(repository.repo)
		if err := c.rest(ctx, op, path, &raw); err != nil {
			return nil, actionsFailure(err, selectedRepositoryPermission)
		}
		if raw.ID <= 0 || !strings.EqualFold(raw.FullName, repository.argument()) {
			return nil, invalidResponse(op, false)
		}
		ids = append(ids, raw.ID)
	}
	return ids, nil
}

// visibilityBody adds visibility and the resolved selected repositories to a create or update body, and
// reports what it sent for the answer.
func (c *Client) visibilityBody(ctx context.Context, op string, a *variableArguments, body map[string]any,
	change *ActionVariableChange) error {
	if a.Visibility == nil {
		return nil
	}
	body["visibility"] = *a.Visibility
	change.Visibility = *a.Visibility
	if *a.Visibility != "selected" {
		return nil
	}
	ids, err := c.selectedRepositoryIDs(ctx, op, a.selected)
	if err != nil {
		return err
	}
	body["selected_repository_ids"] = ids
	for _, repository := range a.selected {
		change.SelectedRepositories = append(change.SelectedRepositories, repository.argument())
	}
	return nil
}

// createVariable sends one create, never repeated. GitHub answers it without the variable.
func (c *Client) createVariable(ctx context.Context, a *variableArguments) (*ActionVariableCreated, error) {
	const op = "create action variable"
	body := map[string]any{"name": a.Name, "value": *a.Value}
	result := &ActionVariableCreated{Created: true, ActionVariableChange: ActionVariableChange{Name: a.Name}}
	if err := c.visibilityBody(ctx, op, a, body, &result.ActionVariableChange); err != nil {
		return nil, err
	}
	if err := c.restChange(ctx, op, http.MethodPost, c.variablesPath(a.environment, ""), body, nil); err != nil {
		return nil, variableFailure(err, variablePermission(c.target, a.environment, true),
			"GitHub already holds a variable named "+a.Name+" at this level; change it with "+
				actionVariablesUpdate.ID+" instead", variableRejected)
	}
	return result, nil
}

// updateVariable sends one change of the given members, never repeated. GitHub answers it without the variable.
func (c *Client) updateVariable(ctx context.Context, a *variableArguments) (*ActionVariableUpdated, error) {
	const op = "update action variable"
	body := map[string]any{}
	result := &ActionVariableUpdated{Updated: true, ActionVariableChange: ActionVariableChange{Name: a.Name}}
	if a.NewName != nil {
		body["name"] = *a.NewName
		result.Name = *a.NewName
	}
	if a.Value != nil {
		body["value"] = *a.Value
	}
	if err := c.visibilityBody(ctx, op, a, body, &result.ActionVariableChange); err != nil {
		return nil, err
	}
	conflict := ""
	if a.NewName != nil {
		conflict = "GitHub refused the rename; a variable named " + *a.NewName + " may already exist at this level"
	}
	if err := c.restChange(ctx, op, http.MethodPatch, c.variablesPath(a.environment, a.Name), body, nil); err != nil {
		return nil, variableFailure(err, variablePermission(c.target, a.environment, true), conflict, variableRejected)
	}
	return result, nil
}

// deleteVariable deletes one variable in a single request that is never repeated.
func (c *Client) deleteVariable(ctx context.Context, a *variableArguments) (*ActionVariableDeleted, error) {
	const op = "delete action variable"
	if err := c.restChange(ctx, op, http.MethodDelete, c.variablesPath(a.environment, a.Name), nil, nil); err != nil {
		return nil, variableFailure(err, variablePermission(c.target, a.environment, true), "", "")
	}
	return &ActionVariableDeleted{Deleted: true, Name: a.Name}, nil
}

// variableSubject names the variables a path below a repository or an organization addresses:
// actions/variables or environments/ENV/variables, with or without a variable name. It names only a
// variable name of the allowed form and an environment name of plain text, and is empty for any other path.
func variableSubject(path string) string {
	collection, member, of := "the Actions variables", "Actions variable", ""
	rest, ok := strings.CutPrefix(path, "actions/variables")
	if !ok {
		tail, found := strings.CutPrefix(path, "environments/")
		segment, after, cut := strings.Cut(tail, "/")
		if !found || !cut {
			return ""
		}
		if rest, ok = strings.CutPrefix(after, "variables"); !ok {
			return ""
		}
		of = " of an environment"
		if environment, err := url.PathUnescape(segment); err == nil && checkVariableEnvironment(environment) == nil {
			of = " of environment " + environment
		}
		collection, member = "the variables"+of, "variable"
	}
	if rest == "" {
		return collection
	}
	name, ok := strings.CutPrefix(rest, "/")
	if !ok {
		return ""
	}
	if unescaped, err := url.PathUnescape(name); err == nil && validVariableName(unescaped) {
		return member + " " + unescaped + of
	}
	return "a variable" + of
}
