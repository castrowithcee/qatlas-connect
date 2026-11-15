package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The assignment tools attach a code security configuration to repositories, detach repositories from their
// configuration, and set the default for new repositories. Repositories are named as OWNER/REPO inside the
// connection's targets and resolved to identifiers by reading each one, never taken from an argument. A scope
// other than selected reaches repositories the call does not name, so it is allowed only where the targets
// name no single repository of the organization. Each tool is exactly one request, never repeated, and
// offered only where a connection's tools list names it.

const codeSecurityAssignPermission = "GitHub refused this token the assignment of code security configurations " +
	"in this organization; it needs write:org on a classic token, and the account must be an organization " +
	"owner or security manager; a configuration owned by an enterprise cannot be assigned"

var (
	codeSecurityScopeValues   = []string{"all", "all_without_configurations", "public", "private_or_internal", "selected"}
	codeSecurityDefaultValues = []string{"all", "none", "private_and_internal", "public"}
)

var codeSecurityAssignRepositoriesArgument = func(required bool, what string) capability.Argument {
	return capability.Argument{Name: "selected_repositories", Required: required, Description: what +
		", as OWNER/REPO of the organization, at most 50; each must lie inside the connection's repository " +
		"targets; Qatlas resolves their identifiers by reading each repository"}
}

var codeSecurityAssignAccepted = []capability.Field{
	{Name: "accepted", Description: "True when GitHub accepted the request; GitHub applies it asynchronously, so " +
		"the status of a repository is read with github.code-security-configuration-repositories.list"},
}

var codeSecurityConfigurationsAttach = capability.Descriptor{
	ID:      Provider + ".code-security-configurations.attach",
	Version: 1,
	Title:   "Attach a GitHub code security configuration to repositories",
	Description: "Attach one code security configuration of an organization an explicit connection allows to " +
		"repositories; scope selected needs selected_repositories inside the connection's targets, and any other " +
		"scope (all, all_without_configurations, public, private_or_internal) reaches repositories the call " +
		"does not name, so it is refused where the connection's targets or exclusions narrow the repositories " +
		"of the organization; GitHub applies the attachment asynchronously and the answer only reports that it " +
		"was accepted; without GitHub Advanced Security licenses only the free features are enabled; " +
		"idempotent, since attaching again leaves GitHub in the same state; offered only where a connection's " +
		"tools list names it",
	Tags:                  []string{"github", "security", "code-security", "organization", "attach"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeSecurityConfigurationSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"configuration_id":`+codeSecurityIDSchema+`,"scope":`+codeSecurityEnum(codeSecurityScopeValues)+
		`,"selected_repositories":`+selectedRepositoriesSchema, "configuration_id", "scope"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"accepted":{"type":"boolean"},` +
		`"configuration_id":{"type":"integer"},"scope":{"type":"string"},` +
		`"selected_repositories":{"type":"array","items":{"type":"string"}}},"required":["accepted"],"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "configuration_id", Description: "Id of the configuration, as github.code-security-configurations.list " +
			"reports it", Required: true},
		{Name: "scope", Required: true, Description: "selected, or all, all_without_configurations, public, or " +
			"private_or_internal; every scope but selected is refused when the connection's targets or exclusions " +
			"narrow the repositories of the organization"},
		codeSecurityAssignRepositoriesArgument(false, "Only with scope selected, and required with it: the "+
			"repositories to attach the configuration to"),
	},
	Fields: append([]capability.Field{
		{Name: "configuration_id", Description: "Id of the configuration"},
		{Name: "scope", Description: "Scope sent"},
		{Name: "selected_repositories", Description: "Repositories sent, as OWNER/REPO; absent for another scope"},
	}, codeSecurityAssignAccepted...),
	Examples: []capability.Example{{
		Description: "Attach a configuration to two repositories",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org","configuration_id":1325,"scope":"selected",` +
			`"selected_repositories":["octo-org/example","octo-org/tools"]}`),
	}},
}

var codeSecurityConfigurationsDetach = capability.Descriptor{
	ID:      Provider + ".code-security-configurations.detach",
	Version: 1,
	Title:   "Detach GitHub repositories from their code security configuration",
	Description: "Detach the named repositories of an organization an explicit connection allows from the code " +
		"security configuration each is attached to; the repositories keep their current settings; GitHub " +
		"applies the detachment asynchronously and the answer only reports that it was accepted; idempotent, " +
		"since detaching again leaves GitHub in the same state; offered only where a connection's tools list " +
		"names it",
	Tags:                  []string{"github", "security", "code-security", "organization", "detach"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeSecurityConfigurationSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"selected_repositories":`+selectedRepositoriesSchema, "selected_repositories"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"accepted":{"type":"boolean"},` +
		`"selected_repositories":{"type":"array","items":{"type":"string"}}},"required":["accepted"],"additionalProperties":false}`),
	Arguments: []capability.Argument{codeSecurityAssignRepositoriesArgument(true, "The repositories to detach")},
	Fields: append([]capability.Field{
		{Name: "selected_repositories", Description: "Repositories sent, as OWNER/REPO"},
	}, codeSecurityAssignAccepted...),
	Examples: []capability.Example{{
		Description: "Detach two repositories",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org","selected_repositories":["octo-org/example",` +
			`"octo-org/tools"]}`),
	}},
}

var codeSecurityConfigurationsSetDefault = capability.Descriptor{
	ID:      Provider + ".code-security-configurations.set-default",
	Version: 1,
	Title:   "Set a GitHub code security configuration as default for new repositories",
	Description: "Set one code security configuration of an organization an explicit connection allows as the " +
		"default for new repositories of a kind (all, none, private_and_internal, or public); it applies only " +
		"to repositories created afterwards and attaches nothing to existing ones; none removes the default of " +
		"that configuration; idempotent, since setting the same default again leaves GitHub in the same state; " +
		"offered only where a connection's tools list names it",
	Tags:                  []string{"github", "security", "code-security", "organization", "default"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeSecurityConfigurationSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"configuration_id":`+codeSecurityIDSchema+`,"default_for_new_repos":`+
		codeSecurityEnum(codeSecurityDefaultValues), "configuration_id", "default_for_new_repos"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"default_for_new_repos":{"type":"string"},` +
		`"configuration":{"type":"object","properties":{` + codeSecurityConfigurationProperties + `},` +
		codeSecurityConfigurationRequired + `}},"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "configuration_id", Description: "Id of the configuration, as github.code-security-configurations.list " +
			"reports it", Required: true},
		{Name: "default_for_new_repos", Required: true, Description: "all, none, private_and_internal, or public: " +
			"the kind of new repository the configuration is applied to"},
	},
	Fields: []capability.Field{
		{Name: "default_for_new_repos", Description: "The kind of new repository GitHub reports the configuration " +
			"is applied to"},
		{Name: "configuration", Description: "The configuration, with the members of " +
			"github.code-security-configurations.get; untrusted data"},
	},
	Examples: []capability.Example{{
		Description: "Apply a configuration to every new repository",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","configuration_id":1325,"default_for_new_repos":"all"}`),
	}},
}

// codeSecurityAssignToolIDs are the three tools that assign code security configurations.
var codeSecurityAssignToolIDs = []string{codeSecurityConfigurationsAttach.ID, codeSecurityConfigurationsDetach.ID,
	codeSecurityConfigurationsSetDefault.ID}

// codeSecurityAssign holds the arguments of the three assignment tools.
type codeSecurityAssign struct {
	ConfigurationID      int64     `json:"configuration_id"`
	Scope                *string   `json:"scope"`
	SelectedRepositories *[]string `json:"selected_repositories"`
	DefaultForNewRepos   *string   `json:"default_for_new_repos"`

	selected []target
}

// CodeSecurityAttachAccepted is the answer to an attach: GitHub accepted it and applies it asynchronously.
type CodeSecurityAttachAccepted struct {
	Accepted             bool     `json:"accepted"`
	ConfigurationID      int64    `json:"configuration_id,omitempty"`
	Scope                string   `json:"scope,omitempty"`
	SelectedRepositories []string `json:"selected_repositories,omitempty"`
}

// CodeSecurityDetachAccepted is the answer to a detach: GitHub accepted it and applies it asynchronously.
type CodeSecurityDetachAccepted struct {
	Accepted             bool     `json:"accepted"`
	SelectedRepositories []string `json:"selected_repositories,omitempty"`
}

// CodeSecurityDefaultSet is the answer to a set default.
type CodeSecurityDefaultSet struct {
	DefaultForNewRepos *string                    `json:"default_for_new_repos,omitempty"`
	Configuration      *CodeSecurityConfiguration `json:"configuration,omitempty"`
}

func codeSecurityAssignOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*codeSecurityAssign, allowlist, target) error,
		call func(context.Context, *Client, *codeSecurityAssign) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: codeSecurityAssignHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(codeSecurityConfigurationsAttach, checkCodeSecurityAttach,
			func(ctx context.Context, c *Client, a *codeSecurityAssign) (any, error) {
				return c.attachCodeSecurityConfiguration(ctx, a)
			}),
		bind(codeSecurityConfigurationsDetach, checkCodeSecurityDetach,
			func(ctx context.Context, c *Client, a *codeSecurityAssign) (any, error) {
				return c.detachCodeSecurityConfiguration(ctx, a)
			}),
		bind(codeSecurityConfigurationsSetDefault, checkCodeSecuritySetDefault,
			func(ctx context.Context, c *Client, a *codeSecurityAssign) (any, error) {
				return c.setCodeSecurityDefault(ctx, a)
			}),
	}
}

// codeSecurityAssignHandler decodes and checks the arguments, the organization, and the repositories against
// the connection's targets before a credential is resolved.
func codeSecurityAssignHandler(id string, check func(*codeSecurityAssign, allowlist, target) error,
	call func(context.Context, *Client, *codeSecurityAssign) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments codeSecurityAssign
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
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
		return bound.locate(call(ctx, client, &arguments))
	}
}

func checkCodeSecurityRepositories(a *codeSecurityAssign, allowed allowlist, bound target) error {
	if a.SelectedRepositories == nil || len(*a.SelectedRepositories) == 0 {
		return invalidRequest("selected_repositories must name at least one repository")
	}
	selected, err := checkSelectedRepositories(*a.SelectedRepositories, allowed, bound)
	a.selected = selected
	return err
}

func checkCodeSecurityAttach(a *codeSecurityAssign, allowed allowlist, bound target) error {
	if err := checkCodeSecurityConfigurationID(&codeSecurityArguments{ConfigurationID: a.ConfigurationID}); err != nil {
		return err
	}
	if a.Scope == nil || !absentOrOneOf(a.Scope, codeSecurityScopeValues) {
		return invalidRequest("scope must be selected, all, all_without_configurations, public, or private_or_internal")
	}
	if a.DefaultForNewRepos != nil {
		return invalidRequest("default_for_new_repos does not apply to an attach")
	}
	if *a.Scope == "selected" {
		return checkCodeSecurityRepositories(a, allowed, bound)
	}
	if a.SelectedRepositories != nil {
		return invalidRequest("selected_repositories needs scope selected in the same call")
	}
	if allowed.restrictsRepositories(bound.owner) {
		return invalidRequest("scope " + *a.Scope + " would reach repositories of the organization outside the " +
			"targets of this connection; use selected with repositories inside the targets")
	}
	return nil
}

func checkCodeSecurityDetach(a *codeSecurityAssign, allowed allowlist, bound target) error {
	if a.ConfigurationID != 0 || a.Scope != nil || a.DefaultForNewRepos != nil {
		return invalidRequest("detach takes only selected_repositories")
	}
	return checkCodeSecurityRepositories(a, allowed, bound)
}

func checkCodeSecuritySetDefault(a *codeSecurityAssign, _ allowlist, _ target) error {
	if err := checkCodeSecurityConfigurationID(&codeSecurityArguments{ConfigurationID: a.ConfigurationID}); err != nil {
		return err
	}
	if a.Scope != nil || a.SelectedRepositories != nil {
		return invalidRequest("set-default takes only configuration_id and default_for_new_repos")
	}
	if a.DefaultForNewRepos == nil || !absentOrOneOf(a.DefaultForNewRepos, codeSecurityDefaultValues) {
		return invalidRequest("default_for_new_repos must be all, none, private_and_internal, or public")
	}
	return nil
}

func selectedRepositoryNames(selected []target) []string {
	names := make([]string, 0, len(selected))
	for _, repository := range selected {
		names = append(names, repository.argument())
	}
	return names
}

func (c *Client) attachCodeSecurityConfiguration(ctx context.Context, a *codeSecurityAssign) (*CodeSecurityAttachAccepted, error) {
	const op = "attach code security configuration"
	body := struct {
		Scope string   `json:"scope"`
		IDs   *[]int64 `json:"selected_repository_ids,omitempty"`
	}{Scope: *a.Scope}
	result := &CodeSecurityAttachAccepted{Accepted: true, ConfigurationID: a.ConfigurationID, Scope: *a.Scope}
	if a.selected != nil {
		ids, err := c.selectedRepositoryIDs(ctx, op, a.selected)
		if err != nil {
			return nil, err
		}
		body.IDs = &ids
		result.SelectedRepositories = selectedRepositoryNames(a.selected)
	}
	status, err := c.restChangeStatus(ctx, op, http.MethodPost,
		c.codeSecurityPath(strconv.FormatInt(a.ConfigurationID, 10)+"/attach"), &body, nil)
	if err != nil {
		return nil, actionsFailure(err, codeSecurityAssignPermission)
	}
	if status != http.StatusAccepted {
		return nil, invalidResponse(op, true)
	}
	return result, nil
}

func (c *Client) detachCodeSecurityConfiguration(ctx context.Context, a *codeSecurityAssign) (*CodeSecurityDetachAccepted, error) {
	const op = "detach code security configuration"
	ids, err := c.selectedRepositoryIDs(ctx, op, a.selected)
	if err != nil {
		return nil, err
	}
	body := struct {
		IDs []int64 `json:"selected_repository_ids"`
	}{IDs: ids}
	status, err := c.restChangeStatus(ctx, op, http.MethodDelete, c.codeSecurityPath("detach"), &body, nil)
	if err != nil {
		return nil, actionsFailure(err, codeSecurityAssignPermission)
	}
	if status != http.StatusNoContent {
		return nil, invalidResponse(op, true)
	}
	return &CodeSecurityDetachAccepted{Accepted: true, SelectedRepositories: selectedRepositoryNames(a.selected)}, nil
}

func (c *Client) setCodeSecurityDefault(ctx context.Context, a *codeSecurityAssign) (*CodeSecurityDefaultSet, error) {
	const op = "set code security default"
	var raw struct {
		DefaultForNewRepos *string                       `json:"default_for_new_repos"`
		Configuration      *codeSecurityConfigurationRaw `json:"configuration"`
	}
	body := struct {
		Default string `json:"default_for_new_repos"`
	}{Default: *a.DefaultForNewRepos}
	status, err := c.restChangeStatus(ctx, op, http.MethodPut,
		c.codeSecurityPath(strconv.FormatInt(a.ConfigurationID, 10)+"/defaults"), &body, &raw)
	if err != nil {
		return nil, actionsFailure(err, codeSecurityAssignPermission)
	}
	if status != http.StatusOK {
		return nil, invalidResponse(op, true)
	}
	result := &CodeSecurityDefaultSet{DefaultForNewRepos: shortText(raw.DefaultForNewRepos)}
	if raw.Configuration != nil {
		configuration, err := raw.Configuration.project(op)
		if err != nil {
			return nil, err
		}
		result.Configuration = configuration
	}
	return result, nil
}
