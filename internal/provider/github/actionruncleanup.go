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

// Cleanup and history of GitHub Actions runs: the listed-only delete of a run or an artifact and force-cancel
// of a run, and the reads of one attempt of a run and of its deployment review history. Every tool reads the
// run or artifact first and checks that GitHub's own REST address of it names the bound repository and the
// requested object, so an identifier GitHub answers for another repository is refused before anything is
// changed or shown. Artifact contents, logs, and download addresses are never read.

// maxAttemptNumber bounds the attempt number a read accepts.
const maxAttemptNumber = 1000

const (
	runDeletedOutput = `{"type":"object","properties":{"run_id":{"type":"integer"},"deleted":{"type":"boolean"},` +
		`"already_deleted":{"type":"boolean"}},"required":["run_id","deleted","already_deleted"],` +
		`"additionalProperties":false}`
	runCancelOutput = `{"type":"object","properties":{"run_id":{"type":"integer"},` +
		`"accepted":{"type":"boolean"},"status":{"type":"string"},"conclusion":{"type":"string"}},` +
		`"required":["run_id","accepted"],"additionalProperties":false}`
	attemptSchema = `{"type":"integer","minimum":1,"maximum":1000}`
)

var artifactDeletedOutput = `{"type":"object","properties":{"artifact_id":{"type":"integer"},` +
	`"deleted":{"type":"boolean"},"already_deleted":{"type":"boolean"},"artifact":{"type":"object",` +
	`"properties":{` + artifactProperties + `},"additionalProperties":false}},` +
	`"required":["artifact_id","deleted","already_deleted"],"additionalProperties":false}`

var approvalEnvironmentSchema = objectSchema(`"id":{"type":"integer"},"node_id":{"type":"string"},` +
	`"name":{"type":"string"},"url":{"type":"string"},"created_at":{"type":"string"},` +
	`"updated_at":{"type":"string"},"links":` + linksSchema(environmentLinkKeys))

var runApprovalsOutput = `{"type":"object","properties":{"run_id":{"type":"integer"},"approvals":{"type":"array",` +
	`"items":{"type":"object","properties":{"state":{"type":"string"},"comment":{"type":"string"},` +
	`"comment_truncated":{"type":"boolean"},"user":` + restAccountSchema + `,"environments":{"type":"array",` +
	`"items":` + approvalEnvironmentSchema + `},"environments_truncated":{"type":"boolean"}},` +
	`"required":["state","environments"],"additionalProperties":false}},"truncated":{"type":"boolean"}},` +
	`"required":["run_id","approvals"],"additionalProperties":false}`

var runsDelete = capability.Descriptor{
	ID:      Provider + ".workflow-runs.delete",
	Version: 1,
	Title:   "Delete a GitHub Actions workflow run",
	Description: "Delete one completed workflow run of a repository a connection allows; a deleted run cannot " +
		"be restored, and a run that has not completed is refused. A run that is already gone counts as done. " +
		"Offered only by a connection whose tools list names it",
	Tags:                  []string{"github", "actions", "runs", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyIdempotent, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"run_id":`+actionsIDSchema, "run_id"),
	OutputSchema:          json.RawMessage(runDeletedOutput),
	Arguments:             []capability.Argument{{Name: "run_id", Description: "Completed workflow run identifier", Required: true}},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once the run is gone"},
		{Name: "already_deleted", Description: "True when the run was found but GitHub reported it gone when the " +
			"delete arrived, and Qatlas Connect deleted nothing"},
		{Name: "run_id", Description: "Run identifier"},
	},
	Examples: []capability.Example{{Description: "Delete a run", Arguments: json.RawMessage(`{"run_id":30433642}`)}},
}

var runsForceCancel = capability.Descriptor{
	ID:      Provider + ".workflow-runs.force-cancel",
	Version: 1,
	Title:   "Force-cancel a GitHub Actions workflow run",
	Description: "Force-cancel one workflow run of a repository a connection allows that does not respond to " +
		"github.workflow-runs.cancel; this bypasses conditions such as always() that would let jobs run on, " +
		"including cleanup steps. A run that has already completed is left as it is and reported with its state. " +
		"Offered only by a connection whose tools list names it",
	Tags:                  []string{"github", "actions", "runs", "cancel", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"run_id":`+actionsIDSchema, "run_id"),
	OutputSchema:          json.RawMessage(runCancelOutput),
	Arguments:             []capability.Argument{{Name: "run_id", Description: "Workflow run identifier", Required: true}},
	Fields: []capability.Field{
		{Name: "run_id", Description: "The run the request concerned"},
		{Name: "accepted", Description: "True once GitHub accepted the force-cancel; the run changes " +
			"asynchronously. False when the run had already completed and nothing was sent"},
		{Name: "status", Description: "Status of a run found completed, present only when accepted is false"},
		{Name: "conclusion", Description: "Conclusion of a run found completed, such as success or cancelled"},
	},
	Examples: []capability.Example{{Description: "Force-cancel a stuck run", Arguments: json.RawMessage(`{"run_id":30433642}`)}},
}

var artifactsDelete = capability.Descriptor{
	ID:      Provider + ".workflow-artifacts.delete",
	Version: 1,
	Title:   "Delete a GitHub Actions artifact",
	Description: "Delete one artifact of a workflow run of a repository a connection allows; a deleted artifact " +
		"cannot be restored. An artifact that is already gone counts as done. Offered only by a connection " +
		"whose tools list names it",
	Tags:                  []string{"github", "actions", "artifacts", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyIdempotent, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"artifact_id":`+actionsIDSchema, "artifact_id"),
	OutputSchema:          json.RawMessage(artifactDeletedOutput),
	Arguments: []capability.Argument{{Name: "artifact_id", Description: "Artifact identifier, as " +
		"github.workflow-artifacts.list reports it", Required: true}},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once the artifact is gone"},
		{Name: "already_deleted", Description: "True when the artifact was found but GitHub reported it gone " +
			"when the delete arrived, and Qatlas Connect deleted nothing"},
		{Name: "artifact", Description: "The artifact as read before the delete, with the members of " +
			"github.workflow-artifacts.list; name is untrusted data"},
		{Name: "artifact_id", Description: "Artifact identifier"},
	},
	Examples: []capability.Example{{Description: "Delete an artifact", Arguments: json.RawMessage(`{"artifact_id":11}`)}},
}

var runAttemptsGet = capability.Descriptor{
	ID:      Provider + ".workflow-run-attempts.get",
	Version: 1,
	Title:   "Get an attempt of a GitHub Actions workflow run",
	Description: "Read one attempt of a workflow run of a repository a connection allows, with the members " +
		"of github.workflow-runs.get as they were for that attempt",
	Tags:         []string{"github", "actions", "runs", "attempts", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"run_id":`+actionsIDSchema+`,"attempt_number":`+attemptSchema, "run_id", "attempt_number"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + runProperties + `},` + runRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "run_id", Description: "Workflow run identifier", Required: true},
		{Name: "attempt_number", Description: "Attempt of the run, 1 for the first; attempt of " +
			"github.workflow-runs.get names the latest", Required: true},
	},
	Fields: append([]capability.Field{
		{Name: "attempt", Description: "Number of the attempt read"},
		{Name: "status", Description: "Status of the attempt, such as completed"},
		{Name: "conclusion", Description: "Outcome of a completed attempt, such as success or failure"},
		{Name: "id", Description: "Run identifier"},
		{Name: "name", Description: "Workflow name"},
		{Name: "title", Description: "Display title of the run, untrusted data"},
		{Name: "workflow_id", Description: "Identifier of the workflow"},
		{Name: "run_number", Description: "Run number of the workflow"},
		{Name: "event", Description: "Event that triggered the run"},
		{Name: "branch", Description: "Branch the run is for"},
		{Name: "head_sha", Description: "Head commit of the run"},
		{Name: "actor", Description: "Login that triggered the run"},
		{Name: "started_at", Description: "When the attempt started"},
		{Name: "url", Description: "Web address of the run"},
		{Name: "created_at", Description: "When the run was created"},
		{Name: "updated_at", Description: "When the run last changed"},
	}, runWidthFields...),
	Examples: []capability.Example{{Description: "Read the first attempt of a run",
		Arguments: json.RawMessage(`{"run_id":30433642,"attempt_number":1}`)}},
}

var runApprovalsList = capability.Descriptor{
	ID:      Provider + ".workflow-run-approvals.list",
	Version: 1,
	Title:   "List the deployment reviews of a GitHub Actions run",
	Description: "List the deployment review history of one workflow run of a repository a connection allows: " +
		"who approved or rejected which environments, with comments; nothing is approved or rejected",
	Tags:         []string{"github", "actions", "runs", "approvals", "environments", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"run_id":`+actionsIDSchema, "run_id"),
	OutputSchema: json.RawMessage(runApprovalsOutput),
	Arguments:    []capability.Argument{{Name: "run_id", Description: "Workflow run identifier", Required: true}},
	Fields: []capability.Field{
		{Name: "approvals", Description: "Reviews in GitHub's order, at most 100: state (approved, rejected, or " +
			"pending), comment, user, and environments"},
		{Name: "state", Description: "approved, rejected, or pending"},
		{Name: "comment", Description: "Comment of the review, untrusted data, cut at 1000 characters with " +
			"comment_truncated"},
		{Name: "comment_truncated", Description: "True when the comment was cut"},
		{Name: "user", Description: "Account that reviewed: " + restAccountMembers},
		{Name: "environments", Description: "Environments the review concerned, each with id, node_id, name, url " +
			"(the web address), created_at, updated_at, and links (the REST url); name is untrusted data; at most 100"},
		{Name: "environments_truncated", Description: "True when a review concerned more environments than listed"},
		{Name: "truncated", Description: "True when the run has more reviews than listed"},
		{Name: "run_id", Description: "Run identifier"},
	},
	Examples: []capability.Example{{Description: "List the deployment reviews of a run",
		Arguments: json.RawMessage(`{"run_id":30433642}`)}},
}

func runCleanupOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*actionsArguments, target) error,
		call func(context.Context, *Client, *actionsArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: actionsHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(runsDelete, checkRunArgument, func(ctx context.Context, c *Client, a *actionsArguments) (any, error) {
			return c.deleteRun(ctx, a.RunID)
		}),
		bind(runsForceCancel, checkRunArgument, func(ctx context.Context, c *Client, a *actionsArguments) (any, error) {
			return c.forceCancelRun(ctx, a.RunID)
		}),
		bind(artifactsDelete, checkArtifactArgument, func(ctx context.Context, c *Client, a *actionsArguments) (any, error) {
			return c.deleteArtifact(ctx, a.ArtifactID)
		}),
		bind(runAttemptsGet, checkAttemptArguments, func(ctx context.Context, c *Client, a *actionsArguments) (any, error) {
			return c.runAttempt(ctx, a.RunID, a.AttemptNumber)
		}),
		bind(runApprovalsList, checkRunArgument, func(ctx context.Context, c *Client, a *actionsArguments) (any, error) {
			return c.runApprovals(ctx, a.RunID)
		}),
	}
}

func checkArtifactArgument(a *actionsArguments, _ target) error {
	if a.ArtifactID < 1 {
		return invalidRequest("artifact_id must be a positive artifact identifier")
	}
	return nil
}

func checkAttemptArguments(a *actionsArguments, bound target) error {
	if err := checkRunArgument(a, bound); err != nil {
		return err
	}
	if a.AttemptNumber < 1 || a.AttemptNumber > maxAttemptNumber {
		return invalidRequest("attempt_number must be an attempt from 1 through " + strconv.Itoa(maxAttemptNumber))
	}
	return nil
}

// runRoute and artifactRoute are the routes of a run and an artifact below the Actions of a repository.
func runRoute(id int64) string {
	return "runs/" + strconv.FormatInt(id, 10)
}

func artifactRoute(id int64) string {
	return "artifacts/" + strconv.FormatInt(id, 10)
}

// link returns the address links holds under key, or nil.
func link(links Links, key string) *string {
	if value, ok := links[key]; ok {
		return &value
	}
	return nil
}

// foreignActionsObject refuses an answer that does not prove it is the requested object of the bound
// repository, without naming what GitHub sent instead.
func foreignActionsObject(op, what string) error {
	return &provider.Error{Class: provider.ClassInvalidResponse, Op: op,
		Message: "GitHub returned " + what + " that does not prove it belongs to this repository; nothing was " +
			"changed and nothing of it is shown"}
}

// ownRun is the proof read of the run tools: the run must be the requested one, and its REST url must name
// the bound repository and the run.
func (c *Client) ownRun(ctx context.Context, op string, id int64) (runJSON, error) {
	run, err := c.run(ctx, op, id)
	if err != nil {
		return run, err
	}
	if !c.belongsHere(link(run.links, "url"), "/actions/"+runRoute(id)) {
		return run, foreignActionsObject(op, "a workflow run")
	}
	return run, nil
}

// changeFailure names a refused cleanup in Qatlas' own words: a conflict says what it means for this request,
// and a refused permission what such a request needs. Every other failure, an unclear outcome included,
// stays as it is.
func changeFailure(err error, conflict string) error {
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Message == conflictMessage {
		refused := *failure
		refused.Message = conflict
		return &refused
	}
	return actionsFailure(err, actionsChangePermission)
}

// RunDeleted is the answer of a run deletion.
type RunDeleted struct {
	RunID          int64 `json:"run_id"`
	Deleted        bool  `json:"deleted"`
	AlreadyDeleted bool  `json:"already_deleted"`
}

// deleteRun deletes one completed run of the bound repository after its proof read. A not-found answer to the
// delete of the run the read just found means that it is gone already. The delete is one request that is
// never repeated.
func (c *Client) deleteRun(ctx context.Context, id int64) (*RunDeleted, error) {
	const op = "delete workflow run"
	run, err := c.ownRun(ctx, op, id)
	if err != nil {
		return nil, err
	}
	if run.Status != "completed" {
		return nil, providerError(op, "the run has not completed yet; cancel it and wait until it has completed "+
			"before deleting it; nothing was deleted")
	}
	err = c.restChange(ctx, op, http.MethodDelete, c.actionsPath(runRoute(id)), struct{}{}, nil)
	switch {
	case err == nil:
		return &RunDeleted{RunID: id, Deleted: true}, nil
	case isNotFound(err):
		return &RunDeleted{RunID: id, Deleted: true, AlreadyDeleted: true}, nil
	}
	return nil, changeFailure(err, "GitHub refused to delete the run in its current state; nothing was deleted")
}

// forceCancelRun force-cancels one run of the bound repository after its proof read. A completed run is
// reported with its state without a request. The force-cancel is one request that is never repeated.
func (c *Client) forceCancelRun(ctx context.Context, id int64) (*RunCancel, error) {
	const op = "force-cancel workflow run"
	run, err := c.ownRun(ctx, op, id)
	if err != nil {
		return nil, err
	}
	if run.Status == "completed" {
		return &RunCancel{RunID: id, Status: run.Status, Conclusion: run.Conclusion}, nil
	}
	err = c.restChange(ctx, op, http.MethodPost, c.actionsPath(runRoute(id)+"/force-cancel"), struct{}{}, nil)
	if err == nil {
		return &RunCancel{RunID: id, Accepted: true}, nil
	}
	return nil, changeFailure(err, "GitHub refused to force-cancel the run in its current state, for example "+
		"because it has completed meanwhile; nothing was changed")
}

// ArtifactDeleted is the answer of an artifact deletion.
type ArtifactDeleted struct {
	ArtifactID     int64     `json:"artifact_id"`
	Deleted        bool      `json:"deleted"`
	AlreadyDeleted bool      `json:"already_deleted"`
	Artifact       *Artifact `json:"artifact,omitempty"`
}

// deleteArtifact deletes one artifact of the bound repository. The proof read finds the artifact and checks
// that its REST url names the bound repository and the artifact; a not-found answer to the delete then means
// that it is gone already. The delete is one request that is never repeated.
func (c *Client) deleteArtifact(ctx context.Context, id int64) (*ArtifactDeleted, error) {
	const op = "delete workflow artifact"
	var raw artifactJSON
	if err := c.actionsRead(ctx, op, artifactRoute(id), nil, &raw); err != nil {
		return nil, err
	}
	if raw.ID != id || !c.belongsHere(link(raw.links, "url"), "/actions/"+artifactRoute(id)) {
		return nil, foreignActionsObject(op, "an artifact")
	}
	artifact := raw.view()
	name, _ := cleanText(&artifact.Name, artifactNameLimit, false)
	artifact.Name = *name
	err := c.restChange(ctx, op, http.MethodDelete, c.actionsPath(artifactRoute(id)), struct{}{}, nil)
	switch {
	case err == nil:
		return &ArtifactDeleted{ArtifactID: id, Deleted: true, Artifact: &artifact}, nil
	case isNotFound(err):
		return &ArtifactDeleted{ArtifactID: id, Deleted: true, AlreadyDeleted: true, Artifact: &artifact}, nil
	}
	return nil, changeFailure(err, "GitHub refused to delete the artifact in its current state; nothing was deleted")
}

// runAttempt reads one attempt of a run of the bound repository. The answer must be the requested run and
// attempt, and its REST url must name the bound repository and the run.
func (c *Client) runAttempt(ctx context.Context, id int64, attempt int) (*WorkflowRun, error) {
	const op = "get workflow run attempt"
	var raw runJSON
	path := runRoute(id) + "/attempts/" + strconv.Itoa(attempt)
	if err := c.actionsRead(ctx, op, path, nil, &raw); err != nil {
		return nil, err
	}
	if raw.ID != id || raw.RunAttempt != attempt || raw.Status == "" || !c.belongsHere(link(raw.links, "url"), "/actions/"+runRoute(id)) {
		return nil, foreignActionsObject(op, "a workflow run attempt")
	}
	view := raw.view(c.allowed, true)
	return &view, nil
}

// RunApprovals is the deployment review history of one run.
type RunApprovals struct {
	RunID     int64         `json:"run_id"`
	Approvals []RunApproval `json:"approvals"`
	Truncated bool          `json:"truncated,omitempty"`
}

// RunApproval is one deployment review of a run. Comment and the account texts are untrusted data.
type RunApproval struct {
	State                 string                `json:"state"`
	Comment               string                `json:"comment,omitempty"`
	CommentTruncated      bool                  `json:"comment_truncated,omitempty"`
	User                  *RESTAccount          `json:"user,omitempty"`
	Environments          []ApprovalEnvironment `json:"environments"`
	EnvironmentsTruncated bool                  `json:"environments_truncated,omitempty"`
}

// ApprovalEnvironment is an environment a deployment review concerned.
type ApprovalEnvironment struct {
	ID        *int64  `json:"id,omitempty"`
	NodeID    *string `json:"node_id,omitempty"`
	Name      string  `json:"name"`
	URL       *string `json:"url,omitempty"`
	CreatedAt *string `json:"created_at,omitempty"`
	UpdatedAt *string `json:"updated_at,omitempty"`
	Links     Links   `json:"links,omitempty"`
}

type runApprovalRaw struct {
	State        *string          `json:"state"`
	Comment      *string          `json:"comment"`
	User         *accountRaw      `json:"user"`
	Environments []environmentRaw `json:"environments"`
}

// runApprovals reads the deployment review history of one run of the bound repository. The run is read
// first as proof that it belongs to the repository, and every environment of the answer must name the bound
// repository by its REST url; otherwise nothing of the answer is shown.
func (c *Client) runApprovals(ctx context.Context, id int64) (*RunApprovals, error) {
	const op = "list workflow run approvals"
	if _, err := c.ownRun(ctx, op, id); err != nil {
		return nil, err
	}
	var raw []runApprovalRaw
	if err := c.actionsRead(ctx, op, runRoute(id)+"/approvals", nil, &raw); err != nil {
		return nil, err
	}
	result := &RunApprovals{RunID: id, Approvals: []RunApproval{}}
	for _, entry := range raw {
		approval := RunApproval{State: text(valueOr(entry.State)), User: entry.User.project(),
			Environments: []ApprovalEnvironment{}}
		comment, cut := cleanText(entry.Comment, projectionTextLimit, true)
		approval.Comment, approval.CommentTruncated = valueOr(comment), cut
		for i := range entry.Environments {
			environment := &entry.Environments[i]
			view, ok := environment.project()
			if !ok || !c.belongsHere(link(environment.links, "url"), "/environments/"+valueOr(environment.Name)) {
				return nil, foreignActionsObject(op, "a deployment review")
			}
			approval.Environments = append(approval.Environments, ApprovalEnvironment{ID: view.ID,
				NodeID: view.NodeID, Name: view.Name, URL: view.URL, CreatedAt: view.CreatedAt,
				UpdatedAt: view.UpdatedAt, Links: view.Links})
		}
		approval.Environments, approval.EnvironmentsTruncated = capped(approval.Environments)
		result.Approvals = append(result.Approvals, approval)
	}
	result.Approvals, result.Truncated = capped(result.Approvals)
	return result, nil
}
