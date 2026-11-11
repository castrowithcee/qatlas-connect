package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The self-hosted runner groups and the GitHub-hosted runners of an organization. A runner group decides which
// repositories may send jobs to which machines, so all four tools exist for an organization only, take it as
// an owner target, and are offered only where a connection's tools list names them. github.runner-groups.update
// changes the name and the visibility of one group with exactly one PATCH, after a read of the group below the
// bound organization has bound its identifier to that organization. Creating or deleting groups and hosted
// runners, and the selected repositories of a group, are not offered.

// Bounds of the runner group and hosted runner views. Every text GitHub sent is untrusted data; the network
// details of a hosted runner are cut hard.
const (
	maxRunnerGroupNameLength = 255
	maxSelectedWorkflows     = 100
	maxHostedRunnerIPs       = 10
	hostedRunnerPrefixLimit  = 64
)

const runnerGroupVisibilitySchema = `{"type":"string","enum":["selected","all","private"]}`

var runnerGroupVisibilities = []string{"selected", "all", "private"}

const runnerGroupNameSchema = `{"type":"string","minLength":1,"maxLength":255}`

const runnerGroupProperties = `"id":{"type":"integer"},"name":{"type":"string"},"name_truncated":{"type":"boolean"},` +
	`"visibility":{"type":"string"},"default":{"type":"boolean"},"selected_repositories_url":{"type":"string"},` +
	`"runners_url":{"type":"string"},"hosted_runners_url":{"type":"string"},"network_configuration_id":{"type":"string"},` +
	`"inherited":{"type":"boolean"},"inherited_allows_public_repositories":{"type":"boolean"},` +
	`"allows_public_repositories":{"type":"boolean"},"workflow_restrictions_read_only":{"type":"boolean"},` +
	`"restricted_to_workflows":{"type":"boolean"},"selected_workflows":{"type":"array","items":{"type":"string"}},` +
	`"selected_workflows_truncated":{"type":"boolean"}`

const runnerGroupRequired = `"required":["id","name","visibility"],"additionalProperties":false`

const organizationProperty = `"organization":{"type":"string"}`

var runnerGroupSchema = `{"type":"object","properties":{` + runnerGroupProperties + `},` + runnerGroupRequired + `}`

var runnerGroupOutput = `{"type":"object","properties":{` + runnerGroupProperties + `,` + organizationProperty + `},` +
	runnerGroupRequired + `}`

var runnerGroupListOutput = `{"type":"object","properties":{"runner_groups":{"type":"array","items":` + runnerGroupSchema +
	`},"total_count":{"type":"integer"},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"},` +
	organizationProperty + `},"required":["runner_groups","total_count","has_more"],"additionalProperties":false}`

const runnerGroupStateSchema = `{"type":"object","properties":{"name":{"type":"string"},"visibility":{"type":"string"}},` +
	`"required":["name","visibility"],"additionalProperties":false}`

var runnerGroupChangeOutput = `{"type":"object","properties":{"id":{"type":"integer"},"before":` + runnerGroupStateSchema +
	`,"after":` + runnerGroupSchema + `,` + organizationProperty + `},"required":["id","before","after"],` +
	`"additionalProperties":false}`

const hostedRunnerProperties = `"id":{"type":"integer"},"name":{"type":"string"},"name_truncated":{"type":"boolean"},` +
	`"runner_group_id":{"type":"integer"},"image_details":{"type":"object","properties":{"id":{"type":"string"},` +
	`"size_gb":{"type":"integer"},"display_name":{"type":"string"},"source":{"type":"string"},"version":{"type":"string"}},` +
	`"additionalProperties":false},"machine_size_details":{"type":"object","properties":{"id":{"type":"string"},` +
	`"cpu_cores":{"type":"integer"},"memory_gb":{"type":"integer"},"storage_gb":{"type":"integer"}},` +
	`"additionalProperties":false},"status":{"type":"string"},"platform":{"type":"string"},` +
	`"maximum_runners":{"type":"integer"},"public_ip_enabled":{"type":"boolean"},"public_ips":{"type":"array",` +
	`"items":{"type":"object","properties":{"enabled":{"type":"boolean"},"prefix":{"type":"string"},` +
	`"length":{"type":"integer"}},"additionalProperties":false}},"public_ips_truncated":{"type":"boolean"},` +
	`"last_active_on":{"type":"string"},"image_gen":{"type":"boolean"}`

var hostedRunnerListOutput = `{"type":"object","properties":{"runners":{"type":"array","items":{"type":"object",` +
	`"properties":{` + hostedRunnerProperties + `},"required":["id","name"],"additionalProperties":false}},` +
	`"total_count":{"type":"integer"},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"},` +
	organizationProperty + `},"required":["runners","total_count","has_more"],"additionalProperties":false}`

var runnerOrganizationArgument = capability.Argument{Name: "organization", Description: "Organization as orgs/LOGIN " +
	"whose runners this call addresses; required; must be an owner target the connection allows when it lists any",
	Required: true}

var runnerOrganizationField = capability.Field{Name: "organization", Description: "Organization this call " +
	"addressed, as orgs/LOGIN"}

var runnerGroupIDArgument = capability.Argument{Name: "id", Description: "Numeric identifier of the runner group, as " +
	"github.runner-groups.list reports it; GitHub resolves it below the organization, so an inherited group counts " +
	"as well", Required: true}

var runnerGroupFields = []capability.Field{
	{Name: "id", Description: "Identifier of the runner group"},
	{Name: "name", Description: "Name of the group, untrusted data, cut at 200 characters with name_truncated"},
	{Name: "name_truncated", Description: "True when name was cut"},
	{Name: "visibility", Description: "Which repositories may use the group: selected, all, or private, untrusted data"},
	{Name: "default", Description: "True for the default group"},
	{Name: "selected_repositories_url", Description: "REST address of the group's selected repositories, untrusted " +
		"data, never fetched"},
	{Name: "runners_url", Description: "REST address of the group's runners, untrusted data, never fetched"},
	{Name: "hosted_runners_url", Description: "REST address of the group's GitHub-hosted runners, untrusted data, " +
		"never fetched"},
	{Name: "network_configuration_id", Description: "Identifier of the hosted compute network configuration, " +
		"untrusted data"},
	{Name: "inherited", Description: "True when the group is inherited from the enterprise"},
	{Name: "inherited_allows_public_repositories", Description: "True when the enterprise group allows public " +
		"repositories"},
	{Name: "allows_public_repositories", Description: "True when public repositories may use the group"},
	{Name: "workflow_restrictions_read_only", Description: "True when the workflow restrictions cannot be changed here"},
	{Name: "restricted_to_workflows", Description: "True when the group runs only the selected_workflows"},
	{Name: "selected_workflows", Description: "Workflows the group may run, as GitHub names them, untrusted data; at " +
		"most 100, each cut at 1000 characters"},
	{Name: "selected_workflows_truncated", Description: "True when selected_workflows was shortened"},
}

var runnerGroupsList = capability.Descriptor{
	ID:      Provider + ".runner-groups.list",
	Version: 1,
	Title:   "List GitHub runner groups of an organization",
	Description: "List one bounded batch of the self-hosted runner groups of an organization a connection allows, " +
		"including those inherited from an enterprise; offered only where the connection lists it",
	Tags:                  []string{"github", "actions", "runners", "runner-groups", "list", "admin"},
	Risk:                  guardedRisk(capability.EffectRead, capability.IdempotencySafe, actionsSettingSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"organization":`+ownerSchema+`,"visible_to_repository":`+repoSchema+`,`+pagingKeys,
		"organization"),
	OutputSchema: json.RawMessage(runnerGroupListOutput),
	Arguments: append([]capability.Argument{runnerOrganizationArgument,
		{Name: "visible_to_repository", Description: "Only the groups the repository may use, as OWNER/REPO; it must " +
			"belong to the organization and lie inside the connection's repository targets; GitHub receives its name"}},
		pagingArguments...),
	Fields: []capability.Field{
		{Name: "runner_groups", Description: "Runner groups: " + fieldNames(runnerGroupFields) + "; every text and " +
			"address is untrusted data"},
		{Name: "total_count", Description: "Number of groups GitHub reports"},
		pagingFields[0], pagingFields[1], runnerOrganizationField,
	},
	Examples: []capability.Example{{
		Description: "List the runner groups of an organization",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org"}`),
	}, {
		Description: "List the runner groups one repository may use",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org","visible_to_repository":"octo-org/example"}`),
	}},
}

var runnerGroupsGet = capability.Descriptor{
	ID:      Provider + ".runner-groups.get",
	Version: 1,
	Title:   "Get a GitHub runner group of an organization",
	Description: "Read one self-hosted runner group of an organization a connection allows, by identifier; offered " +
		"only where the connection lists it",
	Tags:                  []string{"github", "actions", "runners", "runner-groups", "get", "admin"},
	Risk:                  guardedRisk(capability.EffectRead, capability.IdempotencySafe, actionsSettingSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"organization":`+ownerSchema+`,"id":`+actionsIDSchema, "organization", "id"),
	OutputSchema:          json.RawMessage(runnerGroupOutput),
	Arguments:             []capability.Argument{runnerGroupIDArgument, runnerOrganizationArgument},
	Fields:                append(append([]capability.Field{}, runnerGroupFields...), runnerOrganizationField),
	Examples: []capability.Example{{
		Description: "Read one runner group",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org","id":2}`),
	}},
}

var runnerGroupsUpdate = capability.Descriptor{
	ID:      Provider + ".runner-groups.update",
	Version: 1,
	Title:   "Change a GitHub runner group of an organization",
	Description: "Change the name or the visibility of one self-hosted runner group of an organization a connection " +
		"allows with one request; the visibility decides which repositories may send jobs to the group's machines " +
		"and is security relevant; the selected repositories of a group are not offered, so selected leaves them " +
		"as GitHub holds them; a value left out stays as it is; idempotent, since sending the same values again " +
		"leaves GitHub in the same state; offered only where the connection lists it",
	Tags:                  []string{"github", "actions", "runners", "runner-groups", "update", "admin"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, actionsSettingSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"organization":`+ownerSchema+`,"id":`+actionsIDSchema+`,"name":`+runnerGroupNameSchema+
		`,"visibility":`+runnerGroupVisibilitySchema, "organization", "id"),
	OutputSchema: json.RawMessage(runnerGroupChangeOutput),
	Arguments: []capability.Argument{runnerGroupIDArgument, runnerOrganizationArgument,
		{Name: "name", Description: "New name of the group, 1 to 255 characters; GitHub requires a name with every " +
			"change, so the current one is sent when only visibility is given"},
		{Name: "visibility", Description: "selected, all, or private: which repositories may use the group; at least " +
			"one of name or visibility is required"}},
	Fields: []capability.Field{
		{Name: "id", Description: "Identifier of the changed group"},
		{Name: "before", Description: "name and visibility the group held before the change, untrusted data"},
		{Name: "after", Description: "The group as GitHub answered the change: " + fieldNames(runnerGroupFields)},
		runnerOrganizationField,
	},
	Examples: []capability.Example{{
		Description: "Restrict a runner group to private repositories",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org","id":2,"visibility":"private"}`),
	}},
}

var hostedRunnersList = capability.Descriptor{
	ID:      Provider + ".hosted-runners.list",
	Version: 1,
	Title:   "List GitHub-hosted runners of an organization",
	Description: "List one bounded batch of the GitHub-hosted runners of an organization a connection allows; the " +
		"public IP details are untrusted and cut hard; offered only where the connection lists it",
	Tags:                  []string{"github", "actions", "runners", "hosted-runners", "list", "admin"},
	Risk:                  guardedRisk(capability.EffectRead, capability.IdempotencySafe, actionsSettingSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"organization":`+ownerSchema+`,`+pagingKeys, "organization"),
	OutputSchema:          json.RawMessage(hostedRunnerListOutput),
	Arguments:             append([]capability.Argument{runnerOrganizationArgument}, pagingArguments...),
	Fields: []capability.Field{
		{Name: "runners", Description: "Hosted runners: id, name (cut at 200 characters, name_truncated), " +
			"runner_group_id, image_details (id, size_gb, display_name, source, version), machine_size_details (id, " +
			"cpu_cores, memory_gb, storage_gb), status, platform, maximum_runners, public_ip_enabled, public_ips " +
			"(enabled, prefix, length; at most 10, public_ips_truncated), last_active_on, image_gen; every text and " +
			"every network detail is untrusted data"},
		{Name: "total_count", Description: "Number of hosted runners GitHub reports"},
		pagingFields[0], pagingFields[1], runnerOrganizationField,
	},
	Examples: []capability.Example{{
		Description: "List the hosted runners of an organization",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org"}`),
	}},
}

// fieldNames lists the names of fields for a description.
func fieldNames(fields []capability.Field) string {
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = field.Name
	}
	return strings.Join(names, ", ")
}

// runnerGroupTools are the reads of the runner tools, which join the Actions administrator profile, and the
// change, which stays unticked.
var (
	runnerGroupReads  = []string{runnerGroupsList.ID, runnerGroupsGet.ID, hostedRunnersList.ID}
	runnerGroupWrites = []string{runnerGroupsUpdate.ID}
)

// runnerGroupArguments holds the arguments of every runner group tool; the input schema of each admits only
// its own. id, page, perPage, binding, and repository are derived by the checks.
type runnerGroupArguments struct {
	ID                  int64   `json:"id"`
	Name                *string `json:"name"`
	Visibility          *string `json:"visibility"`
	VisibleToRepository string  `json:"visible_to_repository"`
	Limit               int     `json:"limit"`
	Cursor              string  `json:"cursor"`

	repository    string
	page, perPage int
	binding       []byte
}

// runnerGroupsHandler decodes the arguments, selects the organization, and applies every check before a
// credential is resolved, so a refused request never reaches a secret or GitHub.
func runnerGroupsHandler(id string, check func(*runnerGroupArguments, allowlist, target) error,
	call func(context.Context, *Client, *runnerGroupArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments runnerGroupArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		if resolved == nil {
			return nil, providerError("open", "no connection was selected")
		}
		var named struct {
			Organization string `json:"organization"`
		}
		if json.Unmarshal(raw, &named) != nil {
			return nil, unreadable(id)
		}
		allowed, err := allowlistOf(resolved)
		if err != nil {
			return nil, providerError("open", err.Error())
		}
		bound, _, err := parseAllowedOrganization(allowed, "organization", named.Organization)
		if err != nil {
			return nil, err
		}
		if err := check(&arguments, allowed, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		value, err := call(ctx, client, &arguments)
		return bound.rulesetLocate(value, err)
	}
}

// runnerGroupOperations binds the runner group and hosted runner tools to their handlers.
func runnerGroupOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: runnerGroupsList, Handler: runnerGroupsHandler(runnerGroupsList.ID, checkRunnerGroupsList,
			func(ctx context.Context, c *Client, a *runnerGroupArguments) (any, error) {
				return c.listRunnerGroups(ctx, a)
			})},
		{Descriptor: runnerGroupsGet, Handler: runnerGroupsHandler(runnerGroupsGet.ID, checkRunnerGroupID,
			func(ctx context.Context, c *Client, a *runnerGroupArguments) (any, error) {
				return c.getRunnerGroup(ctx, a)
			})},
		{Descriptor: runnerGroupsUpdate, Handler: runnerGroupsHandler(runnerGroupsUpdate.ID, checkRunnerGroupUpdate,
			func(ctx context.Context, c *Client, a *runnerGroupArguments) (any, error) {
				return c.updateRunnerGroup(ctx, a)
			})},
		{Descriptor: hostedRunnersList, Handler: runnerGroupsHandler(hostedRunnersList.ID, checkHostedRunnersList,
			func(ctx context.Context, c *Client, a *runnerGroupArguments) (any, error) {
				return c.listHostedRunners(ctx, a)
			})},
	}
}

// ---- checks

func checkRunnerGroupID(a *runnerGroupArguments, _ allowlist, _ target) error {
	if a.ID < 1 || a.ID > 9007199254740991 {
		return invalidRequest("id must be the numeric identifier of a runner group")
	}
	return nil
}

func checkRunnerGroupUpdate(a *runnerGroupArguments, allowed allowlist, bound target) error {
	if err := checkRunnerGroupID(a, allowed, bound); err != nil {
		return err
	}
	if a.Name == nil && a.Visibility == nil {
		return invalidRequest("give name, visibility, or both")
	}
	if a.Name != nil {
		if strings.TrimSpace(*a.Name) == "" || utf8.RuneCountInString(*a.Name) > maxRunnerGroupNameLength {
			return invalidRequest(fmt.Sprintf("name must hold 1 to %d characters", maxRunnerGroupNameLength))
		}
		if err := checkText("name", *a.Name); err != nil {
			return err
		}
		if strings.ContainsAny(*a.Name, "\t\n\r") {
			return invalidRequest("name must be one line of text")
		}
	}
	if a.Visibility != nil && !contains(runnerGroupVisibilities, *a.Visibility) {
		return invalidRequest("visibility must be one of " + strings.Join(runnerGroupVisibilities, ", "))
	}
	return nil
}

func (a *runnerGroupArguments) pageBinding(bound target, parts ...string) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint(append([]string{bound.String()}, parts...))
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkRunnerGroupsList(a *runnerGroupArguments, allowed allowlist, bound target) error {
	if a.VisibleToRepository != "" {
		repository, err := parseArgument(kindRepository, a.VisibleToRepository)
		if err != nil {
			return invalidRequest("visible_to_repository must be OWNER/REPO")
		}
		if !strings.EqualFold(repository.owner, bound.owner) {
			return invalidRequest("visible_to_repository must name a repository of the organization")
		}
		if allowed.excluded(repository) {
			return invalidRequest("visible_to_repository names a repository that is not available on this connection")
		}
		if !allowed.allows(repository) {
			return invalidRequest("visible_to_repository names a repository outside the targets of this connection")
		}
		a.repository = repository.repo
	}
	return a.pageBinding(bound, "runner-groups", strings.ToLower(a.repository))
}

func checkHostedRunnersList(a *runnerGroupArguments, _ allowlist, bound target) error {
	return a.pageBinding(bound, "hosted-runners")
}

// ---- transport

const (
	runnerGroupsReadPermission = "GitHub refused this token the runner groups of this organization; reading them " +
		"needs admin:org on a classic token, or, as far as GitHub documents it, the self-hosted runners permission " +
		"of the organization on a fine-grained token"
	runnerGroupsChangePermission = "GitHub refused this token the change of this runner group; changing it needs " +
		"admin:org on a classic token, or, as far as GitHub documents it, the self-hosted runners permission of " +
		"the organization with write access on a fine-grained token"
	hostedRunnersReadPermission = "GitHub refused this token the GitHub-hosted runners of this organization; reading " +
		"them needs manage_runner:org on a classic token, or, as far as GitHub documents it, the hosted runners " +
		"permission of the organization on a fine-grained token"
	runnerGroupRejected = "GitHub rejected the name or the visibility of the runner group; the name may be in use " +
		"already or the plan or an enterprise policy may not offer the visibility, so nothing was changed"
	runnerGroupConflict = "GitHub refused the change of the runner group in its current state, so nothing was changed"
)

func (c *Client) runnerGroupPath(id int64) string {
	path := "actions/runner-groups"
	if id > 0 {
		path += "/" + strconv.FormatInt(id, 10)
	}
	return c.orgPath(path)
}

// ---- runner groups

type runnerGroupRaw struct {
	ID                                *int64   `json:"id"`
	Name                              *string  `json:"name"`
	Visibility                        *string  `json:"visibility"`
	Default                           *bool    `json:"default"`
	SelectedRepositoriesURL           *string  `json:"selected_repositories_url"`
	RunnersURL                        *string  `json:"runners_url"`
	HostedRunnersURL                  *string  `json:"hosted_runners_url"`
	NetworkConfigurationID            *string  `json:"network_configuration_id"`
	Inherited                         *bool    `json:"inherited"`
	InheritedAllowsPublicRepositories *bool    `json:"inherited_allows_public_repositories"`
	AllowsPublicRepositories          *bool    `json:"allows_public_repositories"`
	WorkflowRestrictionsReadOnly      *bool    `json:"workflow_restrictions_read_only"`
	RestrictedToWorkflows             *bool    `json:"restricted_to_workflows"`
	SelectedWorkflows                 []string `json:"selected_workflows"`
}

// RunnerGroup is one self-hosted runner group. Every text and address is untrusted data.
type RunnerGroup struct {
	ID                                int64    `json:"id"`
	Name                              string   `json:"name"`
	NameTruncated                     bool     `json:"name_truncated,omitempty"`
	Visibility                        string   `json:"visibility"`
	Default                           *bool    `json:"default,omitempty"`
	SelectedRepositoriesURL           *string  `json:"selected_repositories_url,omitempty"`
	RunnersURL                        *string  `json:"runners_url,omitempty"`
	HostedRunnersURL                  *string  `json:"hosted_runners_url,omitempty"`
	NetworkConfigurationID            *string  `json:"network_configuration_id,omitempty"`
	Inherited                         *bool    `json:"inherited,omitempty"`
	InheritedAllowsPublicRepositories *bool    `json:"inherited_allows_public_repositories,omitempty"`
	AllowsPublicRepositories          *bool    `json:"allows_public_repositories,omitempty"`
	WorkflowRestrictionsReadOnly      *bool    `json:"workflow_restrictions_read_only,omitempty"`
	RestrictedToWorkflows             *bool    `json:"restricted_to_workflows,omitempty"`
	SelectedWorkflows                 []string `json:"selected_workflows,omitempty"`
	SelectedWorkflowsTruncated        bool     `json:"selected_workflows_truncated,omitempty"`
}

// view projects a group, or reports false for one without an identifier, a name, or a visibility.
func (r runnerGroupRaw) view() (RunnerGroup, bool) {
	if r.ID == nil || *r.ID < 1 {
		return RunnerGroup{}, false
	}
	name, cut := cleanText(filled(r.Name), projectionNameLimit, false)
	visibility := nameText(filled(r.Visibility))
	if name == nil || visibility == nil {
		return RunnerGroup{}, false
	}
	group := RunnerGroup{ID: *r.ID, Name: *name, NameTruncated: cut, Visibility: *visibility, Default: r.Default,
		SelectedRepositoriesURL: address(r.SelectedRepositoriesURL), RunnersURL: address(r.RunnersURL),
		HostedRunnersURL: address(r.HostedRunnersURL), NetworkConfigurationID: nameText(filled(r.NetworkConfigurationID)),
		Inherited: r.Inherited, InheritedAllowsPublicRepositories: r.InheritedAllowsPublicRepositories,
		AllowsPublicRepositories: r.AllowsPublicRepositories, WorkflowRestrictionsReadOnly: r.WorkflowRestrictionsReadOnly,
		RestrictedToWorkflows: r.RestrictedToWorkflows}
	for _, workflow := range r.SelectedWorkflows {
		if len(group.SelectedWorkflows) == maxSelectedWorkflows {
			group.SelectedWorkflowsTruncated = true
			break
		}
		text, _ := cleanText(&workflow, projectionTextLimit, false)
		group.SelectedWorkflows = append(group.SelectedWorkflows, *text)
	}
	return group, true
}

// RunnerGroupList is one batch of runner groups.
type RunnerGroupList struct {
	RunnerGroups []RunnerGroup `json:"runner_groups"`
	TotalCount   int64         `json:"total_count"`
	NextCursor   string        `json:"next_cursor,omitempty"`
	HasMore      bool          `json:"has_more"`
}

// more reports whether a list continues after the page just read; a short page ends it whatever the total says.
func (a *runnerGroupArguments) more(got int, total int64) (bool, string) {
	if got < a.perPage || int64(a.page*a.perPage) >= total || a.page >= maxPage {
		return false, ""
	}
	return true, encodeCursor(a.binding, strconv.Itoa(a.page+1)+":"+strconv.Itoa(a.perPage))
}

func (c *Client) listRunnerGroups(ctx context.Context, a *runnerGroupArguments) (*RunnerGroupList, error) {
	const op = "list runner groups"
	var raw struct {
		TotalCount   *int64           `json:"total_count"`
		RunnerGroups []runnerGroupRaw `json:"runner_groups"`
	}
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if a.repository != "" {
		query.Set("visible_to_repository", a.repository)
	}
	if _, err := c.restPage(ctx, op, c.runnerGroupPath(0), query, &raw); err != nil {
		return nil, actionsFailure(err, runnerGroupsReadPermission)
	}
	if raw.TotalCount == nil || *raw.TotalCount < 0 {
		return nil, invalidResponse(op, false)
	}
	result := &RunnerGroupList{RunnerGroups: make([]RunnerGroup, 0, len(raw.RunnerGroups)), TotalCount: *raw.TotalCount}
	for _, entry := range raw.RunnerGroups {
		group, ok := entry.view()
		if !ok {
			return nil, invalidEntry(op, "a runner group")
		}
		result.RunnerGroups = append(result.RunnerGroups, group)
	}
	result.HasMore, result.NextCursor = a.more(len(raw.RunnerGroups), *raw.TotalCount)
	return result, nil
}

// readRunnerGroup is the read that binds a group identifier to the bound organization: GitHub resolves the
// identifier below it, and the group it answers must carry the identifier that was asked for.
func (c *Client) readRunnerGroup(ctx context.Context, op string, id int64, change bool) (*runnerGroupRaw, error) {
	var raw runnerGroupRaw
	if err := c.rest(ctx, op, c.runnerGroupPath(id), &raw); err != nil {
		return nil, actionsFailure(err, runnerGroupsReadPermission)
	}
	if raw.ID == nil || *raw.ID != id {
		return nil, invalidResponse(op, change)
	}
	return &raw, nil
}

func (c *Client) getRunnerGroup(ctx context.Context, a *runnerGroupArguments) (*RunnerGroup, error) {
	const op = "get runner group"
	raw, err := c.readRunnerGroup(ctx, op, a.ID, false)
	if err != nil {
		return nil, err
	}
	group, ok := raw.view()
	if !ok {
		return nil, invalidEntry(op, "a runner group")
	}
	return &group, nil
}

// RunnerGroupState is the name and the visibility a group held before a change.
type RunnerGroupState struct {
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

// RunnerGroupChange names a change by the group as it stood before and as GitHub answered the change.
type RunnerGroupChange struct {
	ID     int64            `json:"id"`
	Before RunnerGroupState `json:"before"`
	After  RunnerGroup      `json:"after"`
}

// updateRunnerGroup reads the group below the bound organization, then sends one PATCH with the name and the
// visibility, never repeated. GitHub requires the name, so the current one is sent when the call gave none;
// it is sent as GitHub reported it, not as the cut view. The answer to the PATCH is the group after the change,
// so no further request follows and an unreadable answer is an open outcome.
func (c *Client) updateRunnerGroup(ctx context.Context, a *runnerGroupArguments) (*RunnerGroupChange, error) {
	const op = "change runner group"
	current, err := c.readRunnerGroup(ctx, op, a.ID, false)
	if err != nil {
		return nil, err
	}
	before, ok := current.view()
	if !ok || current.Name == nil || strings.TrimSpace(*current.Name) == "" {
		return nil, invalidEntry(op, "a runner group")
	}
	body := map[string]any{"name": *current.Name}
	if a.Name != nil {
		body["name"] = *a.Name
	}
	if a.Visibility != nil {
		body["visibility"] = *a.Visibility
	}
	var answer runnerGroupRaw
	err = variableFailure(c.restChange(ctx, op, http.MethodPatch, c.runnerGroupPath(a.ID), body, &answer),
		runnerGroupsChangePermission, runnerGroupConflict, runnerGroupRejected)
	if err != nil {
		return nil, err
	}
	after, ok := answer.view()
	if !ok || after.ID != a.ID {
		return nil, invalidResponse(op, true)
	}
	return &RunnerGroupChange{ID: a.ID, Before: RunnerGroupState{Name: before.Name, Visibility: before.Visibility},
		After: after}, nil
}

// ---- GitHub-hosted runners

type hostedRunnerRaw struct {
	ID            *int64  `json:"id"`
	Name          *string `json:"name"`
	RunnerGroupID *int64  `json:"runner_group_id"`
	ImageDetails  *struct {
		ID          *string `json:"id"`
		SizeGB      *int64  `json:"size_gb"`
		DisplayName *string `json:"display_name"`
		Source      *string `json:"source"`
		Version     *string `json:"version"`
	} `json:"image_details"`
	MachineSizeDetails *struct {
		ID        *string `json:"id"`
		CPUCores  *int64  `json:"cpu_cores"`
		MemoryGB  *int64  `json:"memory_gb"`
		StorageGB *int64  `json:"storage_gb"`
	} `json:"machine_size_details"`
	Status          *string `json:"status"`
	Platform        *string `json:"platform"`
	MaximumRunners  *int64  `json:"maximum_runners"`
	PublicIPEnabled *bool   `json:"public_ip_enabled"`
	PublicIPs       []struct {
		Enabled *bool   `json:"enabled"`
		Prefix  *string `json:"prefix"`
		Length  *int64  `json:"length"`
	} `json:"public_ips"`
	LastActiveOn *string `json:"last_active_on"`
	ImageGen     *bool   `json:"image_gen"`
}

// HostedRunnerImage is the image of a hosted runner.
type HostedRunnerImage struct {
	ID          *string `json:"id,omitempty"`
	SizeGB      *int64  `json:"size_gb,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	Source      *string `json:"source,omitempty"`
	Version     *string `json:"version,omitempty"`
}

// HostedRunnerMachine is the machine size of a hosted runner.
type HostedRunnerMachine struct {
	ID        *string `json:"id,omitempty"`
	CPUCores  *int64  `json:"cpu_cores,omitempty"`
	MemoryGB  *int64  `json:"memory_gb,omitempty"`
	StorageGB *int64  `json:"storage_gb,omitempty"`
}

// HostedRunnerIP is one public IP range of a hosted runner; untrusted data.
type HostedRunnerIP struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Prefix  *string `json:"prefix,omitempty"`
	Length  *int64  `json:"length,omitempty"`
}

// HostedRunner is one GitHub-hosted runner of an organization.
type HostedRunner struct {
	ID                 int64                `json:"id"`
	Name               string               `json:"name"`
	NameTruncated      bool                 `json:"name_truncated,omitempty"`
	RunnerGroupID      *int64               `json:"runner_group_id,omitempty"`
	ImageDetails       *HostedRunnerImage   `json:"image_details,omitempty"`
	MachineSizeDetails *HostedRunnerMachine `json:"machine_size_details,omitempty"`
	Status             *string              `json:"status,omitempty"`
	Platform           *string              `json:"platform,omitempty"`
	MaximumRunners     *int64               `json:"maximum_runners,omitempty"`
	PublicIPEnabled    *bool                `json:"public_ip_enabled,omitempty"`
	PublicIPs          []HostedRunnerIP     `json:"public_ips,omitempty"`
	PublicIPsTruncated bool                 `json:"public_ips_truncated,omitempty"`
	LastActiveOn       *string              `json:"last_active_on,omitempty"`
	ImageGen           *bool                `json:"image_gen,omitempty"`
}

func (r hostedRunnerRaw) view() (HostedRunner, bool) {
	if r.ID == nil || *r.ID < 1 {
		return HostedRunner{}, false
	}
	name, cut := cleanText(filled(r.Name), projectionNameLimit, false)
	if name == nil {
		return HostedRunner{}, false
	}
	runner := HostedRunner{ID: *r.ID, Name: *name, NameTruncated: cut, RunnerGroupID: r.RunnerGroupID,
		Status: nameText(r.Status), Platform: nameText(r.Platform), MaximumRunners: r.MaximumRunners,
		PublicIPEnabled: r.PublicIPEnabled, LastActiveOn: nameText(r.LastActiveOn), ImageGen: r.ImageGen}
	if image := r.ImageDetails; image != nil {
		runner.ImageDetails = &HostedRunnerImage{ID: nameText(image.ID), SizeGB: image.SizeGB,
			DisplayName: nameText(image.DisplayName), Source: nameText(image.Source), Version: nameText(image.Version)}
	}
	if machine := r.MachineSizeDetails; machine != nil {
		runner.MachineSizeDetails = &HostedRunnerMachine{ID: nameText(machine.ID), CPUCores: machine.CPUCores,
			MemoryGB: machine.MemoryGB, StorageGB: machine.StorageGB}
	}
	for _, ip := range r.PublicIPs {
		if len(runner.PublicIPs) == maxHostedRunnerIPs {
			runner.PublicIPsTruncated = true
			break
		}
		prefix, _ := cleanText(ip.Prefix, hostedRunnerPrefixLimit, false)
		runner.PublicIPs = append(runner.PublicIPs, HostedRunnerIP{Enabled: ip.Enabled, Prefix: prefix, Length: ip.Length})
	}
	return runner, true
}

// HostedRunnerList is one batch of hosted runners.
type HostedRunnerList struct {
	Runners    []HostedRunner `json:"runners"`
	TotalCount int64          `json:"total_count"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
}

func (c *Client) listHostedRunners(ctx context.Context, a *runnerGroupArguments) (*HostedRunnerList, error) {
	const op = "list hosted runners"
	var raw struct {
		TotalCount *int64            `json:"total_count"`
		Runners    []hostedRunnerRaw `json:"runners"`
	}
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if _, err := c.restPage(ctx, op, c.orgPath("actions/hosted-runners"), query, &raw); err != nil {
		return nil, actionsFailure(err, hostedRunnersReadPermission)
	}
	if raw.TotalCount == nil || *raw.TotalCount < 0 {
		return nil, invalidResponse(op, false)
	}
	result := &HostedRunnerList{Runners: make([]HostedRunner, 0, len(raw.Runners)), TotalCount: *raw.TotalCount}
	for _, entry := range raw.Runners {
		runner, ok := entry.view()
		if !ok {
			return nil, invalidEntry(op, "a hosted runner")
		}
		result.Runners = append(result.Runners, runner)
	}
	result.HasMore, result.NextCursor = a.more(len(raw.Runners), *raw.TotalCount)
	return result, nil
}
