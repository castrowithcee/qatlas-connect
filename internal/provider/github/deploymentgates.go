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

// github.workflow-runs.approve and github.pending-deployments.review, both offered only where a connection's
// tools list names them. They are decisions made with the identity of the token's account; GitHub enforces
// prevent_self_review and the required reviewers, this code does not. Each tool reads the run first and
// checks that GitHub's own address of it names the bound repository, and sends nothing when the run does not
// wait for the decision or an environment is not pending for it.

const (
	// gateSensitivity classifies the approval gates: a decision releases code or deployments that were held back.
	gateSensitivity = "github-approval-gate"

	// maxReviewComment bounds the comment of a deployment review. GitHub documents no limit; 1000 characters is
	// what github.workflow-run-approvals.list shows of a comment, so a written comment can be read back whole.
	maxReviewComment = projectionTextLimit

	// maxReviewEnvironments bounds the environments one review names.
	maxReviewEnvironments = 20

	reviewStateApproved = "approved"
	reviewStateRejected = "rejected"

	// awaitingApproval is the status or conclusion GitHub reports for a fork run that waits for approval.
	awaitingApproval = "action_required"
)

const (
	runApprovedOutput = `{"type":"object","properties":{"run_id":{"type":"integer"},"approved":{"type":"boolean"}},` +
		`"required":["run_id","approved"],"additionalProperties":false}`
	deploymentReviewOutput = `{"type":"object","properties":{"run_id":{"type":"integer"},` +
		`"state":{"type":"string","enum":["approved","rejected"]},` +
		`"environment_ids":{"type":"array","items":{"type":"integer"}},` +
		`"deployment_ids":{"type":"array","items":{"type":"integer"}}},` +
		`"required":["run_id","state","environment_ids","deployment_ids"],"additionalProperties":false}`
	reviewStateSchema        = `{"type":"string","enum":["approved","rejected"]}`
	reviewCommentSchema      = `{"type":"string","minLength":1,"maxLength":1000}`
	reviewEnvironmentsSchema = `{"type":"array","minItems":1,"maxItems":20,"uniqueItems":true,"items":` +
		actionsIDSchema + `}`
)

var runsApprove = capability.Descriptor{
	ID:      Provider + ".workflow-runs.approve",
	Version: 1,
	Title:   "Approve a GitHub Actions workflow run of a fork",
	Description: "Approve one workflow run of a repository a connection allows that waits for approval because " +
		"its pull request comes from a fork; the run starts and its code runs with the repository's workflow " +
		"permissions. The decision is made with the identity of the token's account. The run is read first, and " +
		"nothing is sent when it belongs to another repository or does not wait for approval (status or " +
		"conclusion action_required). Offered only by a connection whose tools list names it",
	Tags:                  []string{"github", "actions", "runs", "approve", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyUnknown, gateSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"run_id":`+actionsIDSchema, "run_id"),
	OutputSchema:          json.RawMessage(runApprovedOutput),
	Arguments:             []capability.Argument{{Name: "run_id", Description: "Workflow run identifier", Required: true}},
	Fields: []capability.Field{
		{Name: "run_id", Description: "The run that was approved"},
		{Name: "approved", Description: "True once GitHub accepted the approval; the run starts asynchronously"},
	},
	Examples: []capability.Example{{Description: "Approve the run of a fork's pull request",
		Arguments: json.RawMessage(`{"run_id":30433642}`)}},
}

var pendingDeploymentsReview = capability.Descriptor{
	ID:      Provider + ".pending-deployments.review",
	Version: 1,
	Title:   "Approve or reject pending deployments of a GitHub Actions run",
	Description: "Approve or reject the deployments of one workflow run that wait for a required reviewer, for " +
		"the environments named. The decision is made with the identity of the token's account; GitHub " +
		"enforces that the account is a required reviewer and prevent_self_review, not this tool. The pending " +
		"deployments of the run are read first, and nothing is sent when the run belongs to another repository " +
		"or an environment is not pending for it. The comment is sent to GitHub as it is. Offered only by a " +
		"connection whose tools list names it",
	Tags:                  []string{"github", "actions", "deployments", "pending-deployments", "review", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyUnknown, gateSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"run_id":`+actionsIDSchema+`,"state":`+reviewStateSchema+`,"environment_ids":`+
		reviewEnvironmentsSchema+`,"comment":`+reviewCommentSchema, "run_id", "state", "environment_ids", "comment"),
	OutputSchema: json.RawMessage(deploymentReviewOutput),
	Arguments: []capability.Argument{
		{Name: "run_id", Description: "Workflow run identifier", Required: true},
		{Name: "state", Description: "approved or rejected", Required: true},
		{Name: "environment_ids", Description: "Numeric ids of the environments to decide, 1 to 20, each pending " +
			"for the run", Required: true},
		{Name: "comment", Description: "Comment of the review, 1 to 1000 characters, untrusted text sent as it is",
			Required: true},
	},
	Fields: []capability.Field{
		{Name: "run_id", Description: "The run the review concerned"},
		{Name: "state", Description: "approved or rejected, as sent"},
		{Name: "environment_ids", Description: "The environments decided"},
		{Name: "deployment_ids", Description: "Ids of the deployments GitHub answered with"},
	},
	Examples: []capability.Example{{Description: "Approve the deployment to one environment",
		Arguments: json.RawMessage(`{"run_id":30433642,"state":"approved","environment_ids":[161171787],"comment":"Ship it"}`)}},
}

const (
	runApprovePermission = "GitHub refused this approval; it needs repo on a classic token, or Actions: read " +
		"and write on a fine-grained token, and the account must be allowed to approve runs"
	runApproveRefused = "GitHub refused to approve the run in its current state, for example because it was " +
		"approved or cancelled meanwhile; nothing was changed"
	runApproveRejected = "GitHub rejected the approval of the run; nothing was changed"
	reviewPermission   = "GitHub refused this review; it needs repo on a classic token, or Deployments: read " +
		"and write and Actions: read on a fine-grained token, and the account must be a required reviewer of " +
		"the environments; GitHub also enforces prevent_self_review"
	reviewRefused = "GitHub refused the review in the current state of the deployments, for example because " +
		"they were decided meanwhile; nothing was changed"
	reviewRejected = "GitHub rejected the review; the account may not be allowed to decide these environments, " +
		"for example under prevent_self_review; nothing was changed"
)

func deploymentGateOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: runsApprove, Handler: actionsHandler(runsApprove.ID, checkRunArgument,
			func(ctx context.Context, c *Client, a *actionsArguments) (any, error) {
				return c.approveRun(ctx, a.RunID)
			})},
		{Descriptor: pendingDeploymentsReview, Handler: actionsHandler(pendingDeploymentsReview.ID,
			checkDeploymentReview, func(ctx context.Context, c *Client, a *actionsArguments) (any, error) {
				return c.reviewPendingDeployments(ctx, a)
			})},
	}
}

func checkDeploymentReview(a *actionsArguments, bound target) error {
	if err := checkRunArgument(a, bound); err != nil {
		return err
	}
	if a.ReviewState != reviewStateApproved && a.ReviewState != reviewStateRejected {
		return invalidRequest("state must be approved or rejected")
	}
	if len(a.EnvironmentIDs) < 1 || len(a.EnvironmentIDs) > maxReviewEnvironments {
		return invalidRequest("environment_ids must name 1 to " + strconv.Itoa(maxReviewEnvironments) + " environments")
	}
	seen := map[int64]bool{}
	for _, id := range a.EnvironmentIDs {
		if id < 1 || seen[id] {
			return invalidRequest("environment_ids must be distinct positive environment identifiers")
		}
		seen[id] = true
	}
	if a.Comment == "" || len([]rune(a.Comment)) > maxReviewComment {
		return invalidRequest("comment must have 1 to " + strconv.Itoa(maxReviewComment) + " characters")
	}
	return checkText("comment", a.Comment)
}

// gateFailure names a refused gate decision in Qatlas' own words, without GitHub's text.
func gateFailure(err error, permission, conflict, rejected string) error {
	var failure *provider.Error
	if errors.As(err, &failure) {
		switch failure.Message {
		case conflictMessage:
			refused := *failure
			refused.Message = conflict
			return &refused
		case rejectedMessage:
			refused := *failure
			refused.Message = rejected
			return &refused
		}
	}
	return actionsFailure(err, permission)
}

// RunApproved is the answer of a fork run approval.
type RunApproved struct {
	RunID    int64 `json:"run_id"`
	Approved bool  `json:"approved"`
}

// approveRun approves one run of the bound repository after its proof read, and only when the run waits for
// approval. The approval is one request that is never repeated.
func (c *Client) approveRun(ctx context.Context, id int64) (*RunApproved, error) {
	const op = "approve workflow run"
	run, err := c.ownRun(ctx, op, id)
	if err != nil {
		return nil, err
	}
	if run.Status != awaitingApproval && run.Conclusion != awaitingApproval {
		return nil, providerError(op, "the run does not wait for approval; only a run of a fork's pull request "+
			"that GitHub holds as action_required can be approved; nothing was changed")
	}
	err = c.restChange(ctx, op, http.MethodPost, c.actionsPath(runRoute(id)+"/approve"), struct{}{}, nil)
	if err != nil {
		return nil, gateFailure(err, runApprovePermission, runApproveRefused, runApproveRejected)
	}
	return &RunApproved{RunID: id, Approved: true}, nil
}

// DeploymentReviewed is the answer of a review of pending deployments.
type DeploymentReviewed struct {
	RunID          int64   `json:"run_id"`
	State          string  `json:"state"`
	EnvironmentIDs []int64 `json:"environment_ids"`
	DeploymentIDs  []int64 `json:"deployment_ids"`
}

type pendingDeploymentRaw struct {
	Environment *struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"environment"`
}

// pendingEnvironments reads the environments with a pending deployment of the run; each must name the bound
// repository by its REST url, otherwise nothing of the answer is used.
func (c *Client) pendingEnvironments(ctx context.Context, op string, id int64) (map[int64]bool, error) {
	var raw []pendingDeploymentRaw
	if err := c.actionsRead(ctx, op, runRoute(id)+"/pending_deployments", nil, &raw); err != nil {
		return nil, err
	}
	pending := map[int64]bool{}
	for _, entry := range raw {
		environment := entry.Environment
		if environment == nil || environment.ID < 1 ||
			!c.belongsHere(&environment.URL, "/environments/"+environment.Name) {
			return nil, foreignActionsObject(op, "a pending deployment")
		}
		pending[environment.ID] = true
	}
	return pending, nil
}

// reviewPendingDeployments reads the run and its pending deployments first and sends the review only when
// every named environment is pending for this run. The review is one request that is never repeated.
func (c *Client) reviewPendingDeployments(ctx context.Context, a *actionsArguments) (*DeploymentReviewed, error) {
	const op = "review pending deployments"
	if _, err := c.ownRun(ctx, op, a.RunID); err != nil {
		return nil, err
	}
	pending, err := c.pendingEnvironments(ctx, op, a.RunID)
	if err != nil {
		return nil, err
	}
	for _, id := range a.EnvironmentIDs {
		if !pending[id] {
			return nil, providerError(op, "an environment in environment_ids has no pending deployment for this "+
				"run; read the pending deployments of the run and name only those; nothing was changed")
		}
	}
	body := map[string]any{"environment_ids": a.EnvironmentIDs, "state": a.ReviewState, "comment": a.Comment}
	var answer []struct {
		ID int64 `json:"id"`
	}
	if err := c.restChange(ctx, op, http.MethodPost, c.actionsPath(runRoute(a.RunID)+"/pending_deployments"),
		body, &answer); err != nil {
		return nil, gateFailure(err, reviewPermission, reviewRefused, reviewRejected)
	}
	result := &DeploymentReviewed{RunID: a.RunID, State: a.ReviewState, EnvironmentIDs: a.EnvironmentIDs,
		DeploymentIDs: []int64{}}
	for _, deployment := range answer {
		if deployment.ID > 0 {
			result.DeploymentIDs = append(result.DeploymentIDs, deployment.ID)
		}
	}
	return result, nil
}
