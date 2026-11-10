package github

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Deployments of a repository and their statuses, read only: github.deployments.list and .get, and
// github.deployment-statuses.list and .get. Every route lies below the bound repository; an answer is shown
// only when its repository_url, and for a status its deployment_url, name that repository and the deployment
// the call asked for. The custom deployment protection rules of an environment live in
// deploymentprotectionrules.go.

const maxDeploymentTaskLength = 255

const deploymentTaskSchema = `{"type":"string","minLength":1,"maxLength":255}`

var deploymentStatusLinkKeys = []string{"url", "deployment_url", "repository_url", "target_url",
	"environment_url", "log_url"}

var deploymentProperties = `"id":{"type":"integer"},"node_id":{"type":"string"},"sha":{"type":"string"},` +
	`"ref":{"type":"string"},"task":{"type":"string"},"payload":{"type":"string"},` +
	`"payload_truncated":{"type":"boolean"},"original_environment":{"type":"string"},` +
	`"environment":{"type":"string"},"description":{"type":"string"},"creator":` + restAccountSchema + `,` +
	`"created_at":{"type":"string"},"updated_at":{"type":"string"},"transient_environment":{"type":"boolean"},` +
	`"production_environment":{"type":"boolean"},"performed_via_github_app":` + restAppSchema + `,` +
	`"links":` + linksSchema(checkDeploymentLinks)

var deploymentStatusProperties = `"id":{"type":"integer"},"node_id":{"type":"string"},"state":{"type":"string"},` +
	`"creator":` + restAccountSchema + `,"description":{"type":"string"},"environment":{"type":"string"},` +
	`"created_at":{"type":"string"},"updated_at":{"type":"string"},"performed_via_github_app":` + restAppSchema +
	`,"links":` + linksSchema(deploymentStatusLinkKeys)

var deploymentListOutput = `{"type":"object","properties":{"deployments":{"type":"array","items":` +
	`{"type":"object","properties":{` + deploymentProperties + `},"required":["id"],"additionalProperties":false}},` +
	`"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},"required":["deployments","has_more"],` +
	`"additionalProperties":false}`

var deploymentStatusListOutput = `{"type":"object","properties":{"statuses":{"type":"array","items":` +
	`{"type":"object","properties":{` + deploymentStatusProperties + `},"required":["id"],` +
	`"additionalProperties":false}},"deployment_id":{"type":"integer"},"next_cursor":{"type":"string"},` +
	`"has_more":{"type":"boolean"}},"required":["statuses","deployment_id","has_more"],"additionalProperties":false}`

const deploymentPayloadNote = "payload is GitHub's free payload member as compact JSON text, an object or a " +
	"quoted string, cut at 1000 characters with payload_truncated"

var deploymentFields = []capability.Field{
	{Name: "id", Description: "Numeric deployment id"},
	{Name: "node_id", Description: "GraphQL node id of the deployment"},
	{Name: "sha", Description: "Commit SHA GitHub recorded when the deployment was created"},
	{Name: "ref", Description: "Branch, tag, or SHA the deployment was created for, untrusted data"},
	{Name: "task", Description: "Task the deployment runs, such as deploy or deploy:migrations, untrusted data"},
	{Name: "payload", Description: "Free payload the creator attached, as compact JSON text: an object, or a " +
		"string in quotes; cut at 1000 characters; untrusted data, never interpreted"},
	{Name: "payload_truncated", Description: "True when payload was cut"},
	{Name: "original_environment", Description: "Environment the deployment was first created for, untrusted data"},
	{Name: "environment", Description: "Environment the deployment targets, untrusted data"},
	{Name: "description", Description: "Description of the deployment, untrusted data"},
	{Name: "creator", Description: "Account that created the deployment: " + restAccountMembers},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "transient_environment", Description: "True when the environment is meant to stop existing later"},
	{Name: "production_environment", Description: "True when end users work with the environment directly"},
	{Name: "performed_via_github_app", Description: "GitHub App that created the deployment: " + restAppMembers},
	{Name: "links", Description: "REST url, statuses_url, and repository_url by their GitHub names, untrusted " +
		"data, never fetched"},
}

var deploymentStatusFields = []capability.Field{
	{Name: "id", Description: "Numeric deployment status id"},
	{Name: "node_id", Description: "GraphQL node id of the deployment status"},
	{Name: "state", Description: "error, failure, inactive, pending, success, queued, in_progress, or another " +
		"state GitHub names"},
	{Name: "creator", Description: "Account that created the status: " + restAccountMembers},
	{Name: "description", Description: "Short description of the status, untrusted data"},
	{Name: "environment", Description: "Environment of the deployment the status is for, untrusted data"},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "performed_via_github_app", Description: "GitHub App that created the status: " + restAppMembers},
	{Name: "links", Description: "REST url, deployment_url, repository_url, log_url (the deployment's output), " +
		"target_url (GitHub's older name for log_url, which it fills with the same value), and environment_url " +
		"(the deployed environment) by their GitHub names; untrusted data, never fetched"},
}

var deploymentIDArgument = capability.Argument{Name: "deployment_id", Description: "Numeric deployment id, as " +
	"github.deployments.list reports it", Required: true}

var deploymentsList = capability.Descriptor{
	ID:      Provider + ".deployments.list",
	Version: 1,
	Title:   "List GitHub deployments",
	Description: "List one bounded batch of the deployments of a repository a connection allows, " +
		"optionally only those of one commit SHA, ref, task, or environment; " + deploymentPayloadNote,
	Tags:     []string{"github", "actions", "deployments", "list"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"sha":` + blobSHASchema + `,"ref":` + refSchema + `,"task":` + deploymentTaskSchema +
		`,"environment":` + environmentNameSchema + `,` + pagingKeys),
	OutputSchema: json.RawMessage(deploymentListOutput),
	Arguments: append([]capability.Argument{
		{Name: "sha", Description: "Full commit SHA GitHub recorded when the deployment was created"},
		{Name: "ref", Description: "Branch, tag, or SHA the deployment was created for, as it was given"},
		{Name: "task", Description: "Task of the deployment, such as deploy or deploy:migrations"},
		{Name: "environment", Description: "Environment the deployment targets, such as production"},
	}, pagingArguments...),
	Fields: append([]capability.Field{
		{Name: "deployments", Description: "Deployments with the members of github.deployments.get; untrusted data"},
	}, pagingFields...),
	Examples: []capability.Example{{Description: "List the deployments to production",
		Arguments: json.RawMessage(`{"environment":"production"}`)}},
}

var deploymentsGet = capability.Descriptor{
	ID:      Provider + ".deployments.get",
	Version: 1,
	Title:   "Get a GitHub deployment",
	Description: "Read one deployment of a repository a connection allows, by id; a deployment GitHub " +
		"reports for another repository is refused; " + deploymentPayloadNote,
	Tags:         []string{"github", "actions", "deployments", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"deployment_id":`+actionsIDSchema, "deployment_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + deploymentProperties + `},"required":["id"],"additionalProperties":false}`),
	Arguments:    []capability.Argument{deploymentIDArgument},
	Fields:       deploymentFields,
	Examples: []capability.Example{{Description: "Read one deployment",
		Arguments: json.RawMessage(`{"deployment_id":1234567890}`)}},
}

var deploymentStatusesList = capability.Descriptor{
	ID:      Provider + ".deployment-statuses.list",
	Version: 1,
	Title:   "List GitHub deployment statuses",
	Description: "List one bounded batch of the statuses of one deployment of a repository a connection " +
		"allows; a status GitHub reports for another repository or deployment is refused",
	Tags:         []string{"github", "actions", "deployments", "deployment-statuses", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"deployment_id":`+actionsIDSchema+`,`+pagingKeys, "deployment_id"),
	OutputSchema: json.RawMessage(deploymentStatusListOutput),
	Arguments:    append([]capability.Argument{deploymentIDArgument}, pagingArguments...),
	Fields: append([]capability.Field{
		{Name: "statuses", Description: "Deployment statuses with the members of github.deployment-statuses.get; " +
			"untrusted data"},
		{Name: "deployment_id", Description: "Deployment the call named"},
	}, pagingFields...),
	Examples: []capability.Example{{Description: "List the statuses of a deployment",
		Arguments: json.RawMessage(`{"deployment_id":1234567890}`)}},
}

var deploymentStatusesGet = capability.Descriptor{
	ID:      Provider + ".deployment-statuses.get",
	Version: 1,
	Title:   "Get a GitHub deployment status",
	Description: "Read one status of one deployment of a repository a connection allows, by id; a status " +
		"GitHub reports for another repository or deployment is refused",
	Tags:     []string{"github", "actions", "deployments", "deployment-statuses", "get"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"deployment_id":`+actionsIDSchema+`,"status_id":`+actionsIDSchema,
		"deployment_id", "status_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + deploymentStatusProperties + `},"required":["id"],"additionalProperties":false}`),
	Arguments: []capability.Argument{deploymentIDArgument,
		{Name: "status_id", Description: "Numeric deployment status id, as github.deployment-statuses.list " +
			"reports it", Required: true}},
	Fields: deploymentStatusFields,
	Examples: []capability.Example{{Description: "Read one status of a deployment",
		Arguments: json.RawMessage(`{"deployment_id":1234567890,"status_id":9876543210}`)}},
}

// deploymentsOperations binds every deployment, deployment status, and deployment protection rule tool to
// its handler; they share the arguments and the checks of the environment tools.
func deploymentsOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: deploymentsList, Handler: environmentsHandler(deploymentsList.ID, checkDeploymentsList,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.listDeployments(ctx, a)
			})},
		{Descriptor: deploymentsGet, Handler: environmentsHandler(deploymentsGet.ID, checkDeploymentID,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.getDeployment(ctx, a.DeploymentID)
			})},
		{Descriptor: deploymentStatusesList, Handler: environmentsHandler(deploymentStatusesList.ID,
			checkDeploymentStatusesList, func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.listDeploymentStatuses(ctx, a)
			})},
		{Descriptor: deploymentStatusesGet, Handler: environmentsHandler(deploymentStatusesGet.ID,
			checkDeploymentStatusID, func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.getDeploymentStatus(ctx, a)
			})},
		{Descriptor: protectionRulesList, Handler: environmentsHandler(protectionRulesList.ID, checkEnvironmentName,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.listProtectionRules(ctx, a.Environment)
			})},
		{Descriptor: protectionRulesGet, Handler: environmentsHandler(protectionRulesGet.ID, checkProtectionRuleID,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.getProtectionRule(ctx, a)
			})},
		{Descriptor: protectionRulesDelete, Handler: environmentsHandler(protectionRulesDelete.ID, checkProtectionRuleID,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.deleteProtectionRule(ctx, a)
			})},
	}
}

func checkDeploymentsList(a *environmentsArguments, bound target, _ allowlist) error {
	switch {
	case a.SHA != "" && !validBlobSHA(a.SHA):
		return invalidRequest("sha must be a full commit SHA")
	case a.Ref != "" && !validRef(a.Ref):
		return invalidRequest("ref must be a branch name, a tag name, or a SHA")
	case len([]rune(a.Task)) > maxDeploymentTaskLength:
		return invalidRequest("task must have at most 255 characters")
	case len([]rune(a.Environment)) > maxEnvironmentNameLength:
		return invalidRequest("environment must have at most 255 characters")
	}
	if err := checkText("task", a.Task); err != nil {
		return err
	}
	if err := checkText("environment", a.Environment); err != nil {
		return err
	}
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint("deployments", "list", bound.String(), a.SHA, a.Ref, a.Task, a.Environment)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkDeploymentID(a *environmentsArguments, _ target, _ allowlist) error {
	if a.DeploymentID <= 0 {
		return invalidRequest("deployment_id must be a deployment id")
	}
	return nil
}

func checkDeploymentStatusesList(a *environmentsArguments, bound target, allowed allowlist) error {
	if err := checkDeploymentID(a, bound, allowed); err != nil {
		return err
	}
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint("deployment-statuses", "list", bound.String(), a.DeploymentID)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkDeploymentStatusID(a *environmentsArguments, bound target, allowed allowlist) error {
	if a.StatusID <= 0 {
		return invalidRequest("status_id must be a deployment status id")
	}
	return checkDeploymentID(a, bound, allowed)
}

const deploymentsReadPermission = "GitHub refused this token the deployments of this repository; reading them " +
	"needs repo on a classic token for a private repository, or Deployments: read on a fine-grained token"

// Deployment is the view of one deployment.
type Deployment struct {
	ID                    int64        `json:"id"`
	NodeID                *string      `json:"node_id,omitempty"`
	SHA                   *string      `json:"sha,omitempty"`
	Ref                   *string      `json:"ref,omitempty"`
	Task                  *string      `json:"task,omitempty"`
	Payload               *string      `json:"payload,omitempty"`
	PayloadTruncated      bool         `json:"payload_truncated,omitempty"`
	OriginalEnvironment   *string      `json:"original_environment,omitempty"`
	Environment           *string      `json:"environment,omitempty"`
	Description           *string      `json:"description,omitempty"`
	Creator               *RESTAccount `json:"creator,omitempty"`
	CreatedAt             *string      `json:"created_at,omitempty"`
	UpdatedAt             *string      `json:"updated_at,omitempty"`
	TransientEnvironment  *bool        `json:"transient_environment,omitempty"`
	ProductionEnvironment *bool        `json:"production_environment,omitempty"`
	PerformedViaGitHubApp *RESTApp     `json:"performed_via_github_app,omitempty"`
	Links                 Links        `json:"links,omitempty"`
}

// DeploymentList is one batch of deployments.
type DeploymentList struct {
	Deployments []Deployment `json:"deployments"`
	NextCursor  string       `json:"next_cursor,omitempty"`
	HasMore     bool         `json:"has_more"`
}

// DeploymentStatus is the view of one deployment status.
type DeploymentStatus struct {
	ID                    int64        `json:"id"`
	NodeID                *string      `json:"node_id,omitempty"`
	State                 *string      `json:"state,omitempty"`
	Creator               *RESTAccount `json:"creator,omitempty"`
	Description           *string      `json:"description,omitempty"`
	Environment           *string      `json:"environment,omitempty"`
	CreatedAt             *string      `json:"created_at,omitempty"`
	UpdatedAt             *string      `json:"updated_at,omitempty"`
	PerformedViaGitHubApp *RESTApp     `json:"performed_via_github_app,omitempty"`
	Links                 Links        `json:"links,omitempty"`
}

// DeploymentStatusList is one batch of the statuses of a deployment.
type DeploymentStatusList struct {
	Statuses     []DeploymentStatus `json:"statuses"`
	DeploymentID int64              `json:"deployment_id"`
	NextCursor   string             `json:"next_cursor,omitempty"`
	HasMore      bool               `json:"has_more"`
}

type deploymentRaw struct {
	ID                    *int64          `json:"id"`
	NodeID                *string         `json:"node_id"`
	SHA                   *string         `json:"sha"`
	Ref                   *string         `json:"ref"`
	Task                  *string         `json:"task"`
	Payload               json.RawMessage `json:"payload"`
	OriginalEnvironment   *string         `json:"original_environment"`
	Environment           *string         `json:"environment"`
	Description           *string         `json:"description"`
	Creator               *accountRaw     `json:"creator"`
	CreatedAt             *string         `json:"created_at"`
	UpdatedAt             *string         `json:"updated_at"`
	RepositoryURL         *string         `json:"repository_url"`
	TransientEnvironment  *bool           `json:"transient_environment"`
	ProductionEnvironment *bool           `json:"production_environment"`
	PerformedViaGitHubApp *appRaw         `json:"performed_via_github_app"`
	links                 Links
}

func (r *deploymentRaw) UnmarshalJSON(data []byte) (err error) {
	type plain deploymentRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, checkDeploymentLinks)
	return err
}

type deploymentStatusRaw struct {
	ID                    *int64      `json:"id"`
	NodeID                *string     `json:"node_id"`
	State                 *string     `json:"state"`
	Creator               *accountRaw `json:"creator"`
	Description           *string     `json:"description"`
	Environment           *string     `json:"environment"`
	CreatedAt             *string     `json:"created_at"`
	UpdatedAt             *string     `json:"updated_at"`
	DeploymentURL         *string     `json:"deployment_url"`
	RepositoryURL         *string     `json:"repository_url"`
	PerformedViaGitHubApp *appRaw     `json:"performed_via_github_app"`
	links                 Links
}

func (r *deploymentStatusRaw) UnmarshalJSON(data []byte) (err error) {
	type plain deploymentStatusRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, deploymentStatusLinkKeys)
	return err
}

func (r *deploymentRaw) project() Deployment {
	view := Deployment{ID: valueOr(r.ID), NodeID: nameText(r.NodeID), SHA: nameText(r.SHA), Ref: longText(r.Ref),
		Task: longText(r.Task), OriginalEnvironment: longText(r.OriginalEnvironment),
		Environment: longText(r.Environment), Description: longText(r.Description), Creator: r.Creator.project(),
		CreatedAt: nameText(r.CreatedAt), UpdatedAt: nameText(r.UpdatedAt),
		TransientEnvironment: r.TransientEnvironment, ProductionEnvironment: r.ProductionEnvironment,
		PerformedViaGitHubApp: r.PerformedViaGitHubApp.project(), Links: r.links}
	view.Payload, view.PayloadTruncated = payloadText(r.Payload)
	return view
}

func (r *deploymentStatusRaw) project() DeploymentStatus {
	return DeploymentStatus{ID: valueOr(r.ID), NodeID: nameText(r.NodeID), State: nameText(r.State),
		Creator: r.Creator.project(), Description: longText(r.Description), Environment: longText(r.Environment),
		CreatedAt: nameText(r.CreatedAt), UpdatedAt: nameText(r.UpdatedAt),
		PerformedViaGitHubApp: r.PerformedViaGitHubApp.project(), Links: r.links}
}

// payloadText renders GitHub's free payload member as compact JSON text, so an object and a string stay
// apart, and cuts it like any other untrusted text; a cut text is no longer JSON, which the marker says.
func payloadText(raw json.RawMessage) (*string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return nil, false
	}
	text := compact.String()
	return cleanText(&text, projectionTextLimit, false)
}

// belongsHere reports whether an address GitHub sent names the bound repository, followed by rest. Only the
// path counts: the answer came from the configured instance, whose host name GitHub may spell differently.
func (c *Client) belongsHere(address *string, rest string) bool {
	if address == nil {
		return false
	}
	parsed, err := url.Parse(*address)
	root, rootErr := url.Parse(c.endpoints.rest)
	if err != nil || rootErr != nil {
		return false
	}
	return strings.EqualFold(parsed.Path, root.Path+"/repos/"+c.target.owner+"/"+c.target.repo+rest)
}

// foreignAnswer refuses an answer that does not prove it belongs to the bound repository, without naming
// what GitHub sent instead.
func foreignAnswer(op, what string) error {
	return &provider.Error{Class: provider.ClassInvalidResponse, Op: op,
		Message: "GitHub returned " + what + " that does not belong to this repository or to the requested " +
			"deployment; nothing of it is shown"}
}

func deploymentRest(id int64) string {
	return "/deployments/" + strconv.FormatInt(id, 10)
}

func (r *deploymentRaw) check(c *Client, op string, id int64) error {
	switch {
	case r.ID == nil || *r.ID <= 0:
		return invalidEntry(op, "a deployment")
	case id != 0 && *r.ID != id, !c.belongsHere(r.RepositoryURL, ""):
		return foreignAnswer(op, "a deployment")
	}
	return nil
}

func (r *deploymentStatusRaw) check(c *Client, op string, deploymentID, id int64) error {
	switch {
	case r.ID == nil || *r.ID <= 0:
		return invalidEntry(op, "a deployment status")
	case id != 0 && *r.ID != id, !c.belongsHere(r.RepositoryURL, ""),
		!c.belongsHere(r.DeploymentURL, deploymentRest(deploymentID)):
		return foreignAnswer(op, "a deployment status")
	}
	return nil
}

func (c *Client) listDeployments(ctx context.Context, a *environmentsArguments) (*DeploymentList, error) {
	const op = "list deployments"
	query := a.query()
	for key, value := range map[string]string{"sha": a.SHA, "ref": a.Ref, "task": a.Task, "environment": a.Environment} {
		if value != "" {
			query.Set(key, value)
		}
	}
	var raw []deploymentRaw
	hasNext, err := c.restPage(ctx, op, c.repoPath("deployments"), query, &raw)
	if err != nil {
		return nil, actionsFailure(err, deploymentsReadPermission)
	}
	result := &DeploymentList{Deployments: make([]Deployment, 0, len(raw))}
	for i := range raw {
		if err := raw[i].check(c, op, 0); err != nil {
			return nil, err
		}
		result.Deployments = append(result.Deployments, raw[i].project())
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) getDeployment(ctx context.Context, id int64) (*Deployment, error) {
	const op = "get deployment"
	var raw deploymentRaw
	if err := c.rest(ctx, op, c.repoPath(strings.TrimPrefix(deploymentRest(id), "/")), &raw); err != nil {
		return nil, actionsFailure(err, deploymentsReadPermission)
	}
	if err := raw.check(c, op, id); err != nil {
		return nil, err
	}
	view := raw.project()
	return &view, nil
}

func (c *Client) listDeploymentStatuses(ctx context.Context, a *environmentsArguments) (*DeploymentStatusList, error) {
	const op = "list deployment statuses"
	var raw []deploymentStatusRaw
	path := c.repoPath(strings.TrimPrefix(deploymentRest(a.DeploymentID), "/") + "/statuses")
	hasNext, err := c.restPage(ctx, op, path, a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, deploymentsReadPermission)
	}
	result := &DeploymentStatusList{Statuses: make([]DeploymentStatus, 0, len(raw)), DeploymentID: a.DeploymentID}
	for i := range raw {
		if err := raw[i].check(c, op, a.DeploymentID, 0); err != nil {
			return nil, err
		}
		result.Statuses = append(result.Statuses, raw[i].project())
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) getDeploymentStatus(ctx context.Context, a *environmentsArguments) (*DeploymentStatus, error) {
	const op = "get deployment status"
	var raw deploymentStatusRaw
	path := c.repoPath(strings.TrimPrefix(deploymentRest(a.DeploymentID), "/") + "/statuses/" +
		strconv.FormatInt(a.StatusID, 10))
	if err := c.rest(ctx, op, path, &raw); err != nil {
		return nil, actionsFailure(err, deploymentsReadPermission)
	}
	if err := raw.check(c, op, a.DeploymentID, a.StatusID); err != nil {
		return nil, err
	}
	view := raw.project()
	return &view, nil
}

// deploymentsSubject names the deployments, one deployment, or the statuses of one, that a repository path
// addresses; it names only numeric ids and is empty for any other path.
func deploymentsSubject(path string) string {
	rest, ok := strings.CutPrefix(path, "deployments")
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/")) {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	if rest == "" {
		return "the deployments"
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		return "a deployment"
	}
	deployment := "deployment " + parts[0]
	switch {
	case len(parts) == 1:
		return deployment
	case parts[1] != "statuses":
		return "a resource of " + deployment
	case len(parts) == 2:
		return "the statuses of " + deployment
	}
	if status, err := strconv.ParseInt(parts[2], 10, 64); err == nil && status > 0 && len(parts) == 3 {
		return "status " + parts[2] + " of " + deployment
	}
	return "a status of " + deployment
}
