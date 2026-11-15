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

// Changes to an existing line comment of a pull request, addressed by the comment_id
// github.pull-request-review-comments.list reports. Both tools read the comment first, below the bound
// repository; an optional number additionally pins it to one pull request. Deleting is offered only where a
// connection's tools list names it.

// reviewCommentSensitivity classifies github.pull-request-review-comments.delete as its own guarded change:
// deleting a line comment cannot be undone.
const reviewCommentSensitivity = "github-review-comment"

const reviewCommentDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},` +
	`"comment_id":{"type":"integer"}},"required":["deleted","comment_id"],"additionalProperties":false}`

const reviewCommentNumberArgument = "Pull request the line comment must belong to; a comment of another " +
	"pull request is refused. Any pull request of the repository when omitted"

var pullRequestReviewCommentsUpdate = capability.Descriptor{
	ID:      Provider + ".pull-request-review-comments.update",
	Version: 1,
	Title:   "Update a GitHub pull request line comment",
	Description: "Replace the body of one line comment of a pull request of a repository a connection " +
		"allows, by its comment_id; GitHub changes only a comment the token's user wrote; a comment of " +
		"another repository, or of another pull request when number is given, is refused",
	Tags:     []string{"github", "pulls", "pullrequests", "reviews", "comments", "update"},
	Risk:     changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"comment_id":`+actionsIDSchema+`,"number":`+numberSchema+
		`,"body":{"type":"string","minLength":1,"maxLength":65536}`, "comment_id", "body"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + reviewCommentProperties + `},` +
		reviewCommentRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "comment_id", Description: "Line comment to change, as github.pull-request-review-comments.list " +
			"reports it", Required: true},
		{Name: "number", Description: reviewCommentNumberArgument},
		{Name: "body", Description: "New comment in Markdown, 1 to 65536 characters; replaces the current " +
			"body, stored as given", Required: true},
	},
	Fields: []capability.Field{
		{Name: "id", Description: "Line comment identifier"},
		{Name: "in_reply_to", Description: "Identifier of the comment this one answers"},
		{Name: "author", Description: "Login of the author"},
		{Name: "body", Description: "Comment body in Markdown, untrusted data"},
		{Name: "path", Description: "File the comment is on"},
		{Name: "line", Description: "Line the comment is on"},
		{Name: "side", Description: "Side of the diff: LEFT or RIGHT"},
		{Name: "commit_sha", Description: "Commit the comment was made at"},
		{Name: "created_at", Description: "When the comment was created"},
		{Name: "updated_at", Description: "When the comment last changed"},
	},
	Examples: []capability.Example{{
		Description: "Correct a line comment",
		Arguments:   json.RawMessage(`{"comment_id":1,"number":42,"body":"Use the existing helper here."}`),
	}},
}

var pullRequestReviewCommentsDelete = capability.Descriptor{
	ID:      Provider + ".pull-request-review-comments.delete",
	Version: 1,
	Title:   "Delete a GitHub pull request line comment",
	Description: "Delete one line comment of a pull request of a repository a connection allows " +
		"permanently, by its comment_id; offered only where a connection's tools list names it; a comment of " +
		"another repository, or of another pull request when number is given, is refused",
	Tags:                  []string{"github", "pulls", "pullrequests", "reviews", "comments", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, reviewCommentSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"comment_id":`+actionsIDSchema+`,"number":`+numberSchema, "comment_id"),
	OutputSchema:          json.RawMessage(reviewCommentDeletedOutput),
	Arguments: []capability.Argument{
		{Name: "comment_id", Description: "Line comment to delete, as github.pull-request-review-comments.list " +
			"reports it", Required: true},
		{Name: "number", Description: reviewCommentNumberArgument},
	},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the line comment"},
		{Name: "comment_id", Description: "Identifier of the deleted line comment"},
	},
	Examples: []capability.Example{{
		Description: "Delete a line comment",
		Arguments:   json.RawMessage(`{"comment_id":1,"number":42}`),
	}},
}

func checkReviewCommentMaintenanceArguments(a *reviewCommentArguments) error {
	if a.CommentID < 1 {
		return invalidRequest("comment_id must be a positive line comment identifier")
	}
	if a.Number != 0 {
		return checkNumber(a.Number)
	}
	return nil
}

func checkReviewCommentsUpdateArguments(a *reviewCommentArguments, _ target) error {
	if err := checkReviewCommentMaintenanceArguments(a); err != nil {
		return err
	}
	return checkCommentBody(a.Body)
}

func checkReviewCommentsDeleteArguments(a *reviewCommentArguments, _ target) error {
	return checkReviewCommentMaintenanceArguments(a)
}

// pullRequestReviewCommentMaintenanceOperations binds the line comment change tools to their handlers.
func pullRequestReviewCommentMaintenanceOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*reviewCommentArguments, target) error,
		call func(context.Context, *Client, *reviewCommentArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: reviewCommentsHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(pullRequestReviewCommentsUpdate, checkReviewCommentsUpdateArguments,
			func(ctx context.Context, c *Client, a *reviewCommentArguments) (any, error) {
				return c.updateReviewComment(ctx, a)
			}),
		bind(pullRequestReviewCommentsDelete, checkReviewCommentsDeleteArguments,
			func(ctx context.Context, c *Client, a *reviewCommentArguments) (any, error) {
				return c.deleteReviewComment(ctx, a)
			}),
	}
}

// Permission message of the line comment change tools.
const reviewCommentsChangePermission = "GitHub refused this change of a line comment of a pull request; it " +
	"needs repo on a classic token, or Pull requests: read and write on a fine-grained token"

// reviewCommentPath is the REST route of one line comment of the bound repository.
func (c *Client) reviewCommentPath(commentID int64) string {
	return c.repoPath("pulls/comments/" + strconv.FormatInt(commentID, 10))
}

// reviewCommentDetailJSON adds the pull request address a single line comment reports.
type reviewCommentDetailJSON struct {
	reviewCommentJSON
	PullRequestURL string `json:"pull_request_url"`
}

// findReviewComment reads one line comment of the bound repository before it is changed; the route answers
// not-found for a comment of another repository. With a pull request number the comment must belong to that
// pull request too, and the refusal is the same not-found, so it names no other target.
func (c *Client) findReviewComment(ctx context.Context, op string, a *reviewCommentArguments) error {
	if c.target.kind != kindRepository {
		return providerError(op, "this connection is not bound to a repository")
	}
	var raw reviewCommentDetailJSON
	if err := c.rest(ctx, op, c.reviewCommentPath(a.CommentID), &raw); err != nil {
		return actionsFailure(err, reviewCommentsChangePermission)
	}
	if raw.ID != a.CommentID {
		return invalidEntry(op, "a line comment")
	}
	if a.Number == 0 {
		return nil
	}
	got, err := pullNumberFromURL(op, raw.PullRequestURL)
	if err != nil {
		return err
	}
	if got != a.Number {
		return notFound(op, subject{in: c.target, what: "review comment " + strconv.FormatInt(a.CommentID, 10)})
	}
	return nil
}

// reviewCommentChangeFailure names what GitHub's rejection of a line comment change means; every other
// failure keeps its own message.
func reviewCommentChangeFailure(err error, rejected string) error {
	err = actionsFailure(err, reviewCommentsChangePermission)
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Class == provider.ClassProviderError && failure.Message == rejectedMessage {
		refused := *failure
		refused.Message = rejected
		return &refused
	}
	return err
}

// updateReviewComment replaces the body of one line comment in one request that is never repeated.
func (c *Client) updateReviewComment(ctx context.Context, a *reviewCommentArguments) (*ReviewComment, error) {
	const op = "update pull request line comment"
	if err := c.findReviewComment(ctx, op, a); err != nil {
		return nil, err
	}
	var raw reviewCommentJSON
	if err := c.restChange(ctx, op, http.MethodPatch, c.reviewCommentPath(a.CommentID), map[string]any{"body": a.Body},
		&raw); err != nil {
		return nil, reviewCommentChangeFailure(err, "GitHub rejected the new body of this line comment as invalid")
	}
	if raw.ID < 1 {
		return nil, invalidResponse(op, true)
	}
	view := raw.view()
	return &view, nil
}

// deleteReviewComment removes one line comment in one request that is never repeated.
func (c *Client) deleteReviewComment(ctx context.Context, a *reviewCommentArguments) (any, error) {
	const op = "delete pull request line comment"
	if err := c.findReviewComment(ctx, op, a); err != nil {
		return nil, err
	}
	if err := c.restChange(ctx, op, http.MethodDelete, c.reviewCommentPath(a.CommentID), nil, nil); err != nil {
		return nil, reviewCommentChangeFailure(err, "GitHub refused to delete this line comment as invalid")
	}
	return map[string]any{"deleted": true, "comment_id": a.CommentID}, nil
}
