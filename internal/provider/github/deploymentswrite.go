package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// github.deployments.create and github.deployments.delete, both offered only where a connection's tools list
// names them. A deployment is a request other systems act on, so the call never sets required_contexts, which
// would bypass GitHub's check of the required statuses, and never sets auto_merge, which would change a
// branch. The payload is text for GitHub and is not interpreted here.

// deploymentSensitivity classifies the guarded deployment changes: a deployment starts a release in other
// systems, and a deleted one is gone from the history of its environment.
const deploymentSensitivity = "github-deployment"

const (
	maxDeploymentDescriptionLength = 140
	maxDeploymentPayloadLength     = 4096
)

const deploymentDescriptionSchema = `{"type":"string","minLength":1,"maxLength":140}`

const deploymentPayloadSchema = `{"type":"string","minLength":1,"maxLength":4096}`

const (
	deploymentCreated  = "created"
	deploymentAccepted = "accepted"
	deploymentConflict = "conflict"
)

var deploymentCreatedOutput = `{"type":"object","properties":{"state":{"type":"string","enum":["` +
	deploymentCreated + `","` + deploymentAccepted + `","` + deploymentConflict + `"]},` + deploymentProperties +
	`},"required":["state"],"additionalProperties":false}`

const deploymentDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},` +
	`"deployment_id":{"type":"integer"}},"required":["deleted","deployment_id"],"additionalProperties":false}`

var deploymentCreateDefaultFields = []string{"state", "id", "sha", "ref", "task", "environment", "repository"}

var deploymentsCreate = capability.Descriptor{
	ID:            Provider + ".deployments.create",
	Version:       1,
	DefaultFields: deploymentCreateDefaultFields,
	Title:         "Create a GitHub deployment",
	Description: "Create one deployment of a ref in a repository a connection allows, which other systems act " +
		"on; offered only where a connection's tools list names it. GitHub checks that every required status of " +
		"the ref passes, and this tool cannot skip that check or merge the default branch into the ref. The " +
		"answer's state is created (the deployment is in the members), accepted (GitHub accepted the request " +
		"without a deployment to show, for example after it merged a branch), or conflict (GitHub refused it in " +
		"the current state, for example a failing required status or a merge conflict; nothing was created). A " +
		"repeated call creates a second deployment; " + deploymentPayloadNote,
	Tags:                  []string{"github", "actions", "deployments", "create"},
	Risk:                  guardedRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, deploymentSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"ref":`+refSchema+`,"environment":`+environmentNameSchema+`,"task":`+
		deploymentTaskSchema+`,"description":`+deploymentDescriptionSchema+`,"payload":`+deploymentPayloadSchema+
		`,"transient_environment":{"type":"boolean"},"production_environment":{"type":"boolean"}`, "ref"),
	OutputSchema: json.RawMessage(deploymentCreatedOutput),
	Arguments: []capability.Argument{
		{Name: "ref", Description: "Branch, tag, or SHA to deploy", Required: true},
		{Name: "environment", Description: "Environment to deploy to, 1 to 255 characters; production when omitted"},
		{Name: "task", Description: "Task to run, 1 to 255 characters, such as deploy or deploy:migrations; " +
			"deploy when omitted"},
		{Name: "description", Description: "Short description, 1 to 140 characters"},
		{Name: "payload", Description: "Free text for the systems that act on the deployment, 1 to 4096 " +
			"characters; sent to GitHub as it is and never interpreted"},
		{Name: "transient_environment", Description: "True when the environment stops existing later; false " +
			"when omitted"},
		{Name: "production_environment", Description: "True when end users work with the environment " +
			"directly; GitHub's default when omitted"},
	},
	Fields: append([]capability.Field{
		{Name: "state", Description: "created, accepted, or conflict; only created carries the deployment members"},
	}, deploymentFields...),
	Examples: []capability.Example{{Description: "Deploy a branch to staging",
		Arguments: json.RawMessage(`{"ref":"main","environment":"staging"}`)}},
}

var deploymentsDelete = capability.Descriptor{
	ID:      Provider + ".deployments.delete",
	Version: 1,
	Title:   "Delete a GitHub deployment",
	Description: "Delete one inactive deployment of a repository a connection allows, by id; the deployment is " +
		"read first and nothing is sent when it is missing or belongs to another repository; GitHub refuses the " +
		"active deployment of an environment; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "actions", "deployments", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, deploymentSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"deployment_id":`+actionsIDSchema, "deployment_id"),
	OutputSchema:          json.RawMessage(deploymentDeletedOutput),
	Arguments:             []capability.Argument{deploymentIDArgument},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the deployment"},
		{Name: "deployment_id", Description: "Id of the deleted deployment"},
	},
	Examples: []capability.Example{{Description: "Delete an inactive deployment",
		Arguments: json.RawMessage(`{"deployment_id":1234567890}`)}},
}

const (
	deploymentsChangePermission = "GitHub refused this change of a deployment; it needs repo or repo_deployment " +
		"on a classic token, or Deployments: read and write on a fine-grained token"
	deploymentCreateRejected = "GitHub rejected the deployment; the ref may not exist in this repository, or an " +
		"argument may be invalid for it; nothing was created"
	deploymentDeleteRefused = "GitHub refused to delete the deployment; it deletes only inactive deployments, so " +
		"this one is probably the active deployment of its environment; nothing was changed"
)

func checkDeploymentCreate(a *environmentsArguments, _ target, _ allowlist) error {
	switch {
	case !validRef(a.Ref):
		return invalidRequest("ref must be a branch name, a tag name, or a SHA")
	case len([]rune(a.Environment)) > maxEnvironmentNameLength:
		return invalidRequest("environment must have at most 255 characters")
	case len([]rune(a.Task)) > maxDeploymentTaskLength:
		return invalidRequest("task must have at most 255 characters")
	case len([]rune(a.Description)) > maxDeploymentDescriptionLength:
		return invalidRequest("description must have at most 140 characters")
	case len([]rune(a.Payload)) > maxDeploymentPayloadLength:
		return invalidRequest("payload must have at most 4096 characters")
	}
	for name, value := range map[string]string{"environment": a.Environment, "task": a.Task,
		"description": a.Description, "payload": a.Payload} {
		if err := checkText(name, value); err != nil {
			return err
		}
	}
	return nil
}

// DeploymentCreated is the answer to a deployment request: the state, and the deployment only when GitHub
// created one.
type DeploymentCreated struct {
	State string `json:"state"`
	*Deployment
}

// DeploymentDeleted is the answer to a deleted deployment.
type DeploymentDeleted struct {
	Deleted      bool  `json:"deleted"`
	DeploymentID int64 `json:"deployment_id"`
}

// createDeployment sends one request that is never repeated. GitHub's 409 is a result, since it says the
// required statuses or a merge stand in the way and nothing was created.
func (c *Client) createDeployment(ctx context.Context, a *environmentsArguments) (*DeploymentCreated, error) {
	const op = "create deployment"
	body := map[string]any{"ref": a.Ref}
	for key, value := range map[string]string{"environment": a.Environment, "task": a.Task,
		"description": a.Description, "payload": a.Payload} {
		if value != "" {
			body[key] = value
		}
	}
	if a.TransientEnvironment != nil {
		body["transient_environment"] = *a.TransientEnvironment
	}
	if a.ProductionEnvironment != nil {
		body["production_environment"] = *a.ProductionEnvironment
	}
	var raw deploymentRaw
	status, err := c.restChangeStatus(ctx, op, http.MethodPost, c.repoPath("deployments"), body, &raw)
	if err != nil {
		var failure *provider.Error
		if errors.As(err, &failure) && failure.Message == conflictMessage {
			return &DeploymentCreated{State: deploymentConflict}, nil
		}
		if errors.As(err, &failure) && failure.Message == rejectedMessage {
			refused := *failure
			refused.Message = deploymentCreateRejected
			return nil, &refused
		}
		return nil, actionsFailure(err, deploymentsChangePermission)
	}
	switch status {
	case http.StatusAccepted:
		return &DeploymentCreated{State: deploymentAccepted}, nil
	case http.StatusCreated:
		// The deployment exists once GitHub answered 201, so an answer that cannot be shown leaves the outcome open.
		if raw.check(c, op, 0) != nil {
			return nil, invalidResponse(op, true)
		}
		view := raw.project()
		return &DeploymentCreated{State: deploymentCreated, Deployment: &view}, nil
	}
	return nil, invalidResponse(op, true)
}

// deleteDeployment reads the deployment of the bound repository first and sends the DELETE only after that
// read answered it; the DELETE is sent once and never repeated.
func (c *Client) deleteDeployment(ctx context.Context, id int64) (*DeploymentDeleted, error) {
	const op = "delete deployment"
	if _, err := c.getDeployment(ctx, id); err != nil {
		return nil, err
	}
	path := c.repoPath("deployments/" + strconv.FormatInt(id, 10))
	if err := c.restChange(ctx, op, http.MethodDelete, path, nil, nil); err != nil {
		var failure *provider.Error
		if errors.As(err, &failure) && failure.Message == rejectedMessage {
			refused := *failure
			refused.Message = deploymentDeleteRefused
			return nil, &refused
		}
		return nil, actionsFailure(err, deploymentsChangePermission)
	}
	return &DeploymentDeleted{Deleted: true, DeploymentID: id}, nil
}
