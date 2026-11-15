package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Changes to an existing pull request review: dismissing it, replacing its text, and deleting a pending one.
// Every tool reads the review first, below the pull request the caller names, so a review identifier that
// belongs to another pull request or repository answers not-found before anything changes. Dismissing
// discards an approval or a request for changes of another person, so it is offered only where a connection's
// tools list names it.

// reviewSensitivity classifies github.pull-request-reviews.dismiss as its own guarded change.
const reviewSensitivity = "github-review"

const reviewMessageSchema = `{"type":"string","minLength":1,"maxLength":65536}`

const reviewDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},` +
	`"number":{"type":"integer"},"review_id":{"type":"integer"}},` +
	`"required":["deleted","number","review_id"],"additionalProperties":false}`

var pullRequestReviewsDismiss = capability.Descriptor{
	ID:      Provider + ".pull-request-reviews.dismiss",
	Version: 1,
	Title:   "Dismiss a GitHub pull request review",
	Description: "Dismiss one approval or request for changes of one pull request of a repository a connection " +
		"allows, with a required message; offered only where a connection's tools list names it, because it " +
		"discards a decision of another reviewer; a review of another pull request or repository is refused",
	Tags:                  []string{"github", "pulls", "pullrequests", "reviews", "dismiss"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, reviewSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"number":`+numberSchema+`,"review_id":`+actionsIDSchema+`,"message":`+
		reviewMessageSchema, "number", "review_id", "message"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + reviewProperties + `},` + reviewRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Pull request number in the repository", Required: true},
		{Name: "review_id", Description: "Review to dismiss, as github.pull-request-reviews.list reports it",
			Required: true},
		{Name: "message", Description: "Why the review is dismissed, 1 to 65536 characters; GitHub shows it " +
			"with the dismissal", Required: true},
	},
	Fields: reviewFields,
	Examples: []capability.Example{{
		Description: "Dismiss a stale request for changes",
		Arguments:   json.RawMessage(`{"number":42,"review_id":80,"message":"Addressed in the latest commits."}`),
	}},
}

var pullRequestReviewsUpdate = capability.Descriptor{
	ID:      Provider + ".pull-request-reviews.update",
	Version: 1,
	Title:   "Update a GitHub pull request review",
	Description: "Replace the summary text of one review of one pull request of a repository a connection " +
		"allows; GitHub changes only a pending review or one the token's user wrote; a review of another " +
		"pull request or repository is refused",
	Tags:     []string{"github", "pulls", "pullrequests", "reviews", "update"},
	Risk:     changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"number":`+numberSchema+`,"review_id":`+actionsIDSchema+`,"body":`+reviewMessageSchema,
		"number", "review_id", "body"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + reviewProperties + `},` + reviewRequired + `}`),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Pull request number in the repository", Required: true},
		{Name: "review_id", Description: "Review to change, as github.pull-request-reviews.list reports it",
			Required: true},
		{Name: "body", Description: "New review summary in Markdown, 1 to 65536 characters; replaces the " +
			"current one, stored as given", Required: true},
	},
	Fields: reviewFields,
	Examples: []capability.Example{{
		Description: "Correct the summary of a review",
		Arguments:   json.RawMessage(`{"number":42,"review_id":80,"body":"One thing to fix, see the line comment."}`),
	}},
}

var pullRequestReviewsDeletePending = capability.Descriptor{
	ID:      Provider + ".pull-request-reviews.delete-pending",
	Version: 1,
	Title:   "Delete a pending GitHub pull request review",
	Description: "Delete one pending, not yet submitted review of one pull request of a repository a " +
		"connection allows, with its line comments; a submitted review, or one of another pull request or " +
		"repository, is refused without a change",
	Tags:         []string{"github", "pulls", "pullrequests", "reviews", "delete"},
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyUnknown),
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":`+numberSchema+`,"review_id":`+actionsIDSchema, "number", "review_id"),
	OutputSchema: json.RawMessage(reviewDeletedOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Pull request number in the repository", Required: true},
		{Name: "review_id", Description: "Pending review to delete, as github.pull-request-reviews.list " +
			"reports it", Required: true},
	},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the pending review"},
		{Name: "number", Description: "Number of the pull request"},
		{Name: "review_id", Description: "Identifier of the deleted review"},
	},
	Examples: []capability.Example{{
		Description: "Delete a pending review",
		Arguments:   json.RawMessage(`{"number":42,"review_id":80}`),
	}},
}

func checkReviewMaintenanceArguments(a *reviewArguments) error {
	if err := checkNumber(a.Number); err != nil {
		return err
	}
	if a.ReviewID < 1 {
		return invalidRequest("review_id must be a positive review identifier")
	}
	return nil
}

func checkReviewsDismissArguments(a *reviewArguments, _ target) error {
	if err := checkReviewMaintenanceArguments(a); err != nil {
		return err
	}
	if strings.TrimSpace(a.Message) == "" || utf8.RuneCountInString(a.Message) > maxBodyLength {
		return invalidRequest(fmt.Sprintf("message must hold 1 to %d characters", maxBodyLength))
	}
	return nil
}

func checkReviewsUpdateArguments(a *reviewArguments, _ target) error {
	if err := checkReviewMaintenanceArguments(a); err != nil {
		return err
	}
	return checkCommentBody(a.Body)
}

func checkReviewsDeletePendingArguments(a *reviewArguments, _ target) error {
	return checkReviewMaintenanceArguments(a)
}

// pullRequestReviewMaintenanceOperations binds the review change tools to their handlers.
func pullRequestReviewMaintenanceOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*reviewArguments, target) error,
		call func(context.Context, *Client, *reviewArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: reviewsHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(pullRequestReviewsDismiss, checkReviewsDismissArguments,
			func(ctx context.Context, c *Client, a *reviewArguments) (any, error) {
				return c.dismissReview(ctx, a)
			}),
		bind(pullRequestReviewsUpdate, checkReviewsUpdateArguments,
			func(ctx context.Context, c *Client, a *reviewArguments) (any, error) {
				return c.updateReview(ctx, a)
			}),
		bind(pullRequestReviewsDeletePending, checkReviewsDeletePendingArguments,
			func(ctx context.Context, c *Client, a *reviewArguments) (any, error) {
				return c.deletePendingReview(ctx, a)
			}),
	}
}

// reviewPath is the REST route of one review of one pull request of the bound repository.
func (c *Client) reviewPath(number int, reviewID int64) string {
	return c.repoPath("pulls/" + strconv.Itoa(number) + "/reviews/" + strconv.FormatInt(reviewID, 10))
}

// reviewDetailJSON adds the pull request address the single review route reports.
type reviewDetailJSON struct {
	reviewJSON
	PullRequestURL string `json:"pull_request_url"`
}

// pullNumberFromURL reads the pull request number from an address of the form .../pulls/NUMBER.
func pullNumberFromURL(op, address string) (int, error) {
	head, tail, ok := strings.Cut(address[strings.LastIndex(address, "/pulls/")+1:], "/")
	number, err := strconv.Atoi(tail)
	if !ok || head != "pulls" || err != nil || number < 1 {
		return 0, invalidResponse(op, false)
	}
	return number, nil
}

// findReview reads one review below the named pull request of the bound repository before it is changed. The
// route answers not-found for a review of another pull request or repository; the answer is checked against
// the request as well, so a route that ignores the pull request still never lets a foreign review through.
func (c *Client) findReview(ctx context.Context, op string, number int, reviewID int64) (*reviewDetailJSON, error) {
	if c.target.kind != kindRepository {
		return nil, providerError(op, "this connection is not bound to a repository")
	}
	var raw reviewDetailJSON
	if err := c.rest(ctx, op, c.reviewPath(number, reviewID), &raw); err != nil {
		return nil, actionsFailure(err, reviewsChangePermission)
	}
	if raw.ID != reviewID {
		return nil, invalidEntry(op, "a review")
	}
	if got, err := pullNumberFromURL(op, raw.PullRequestURL); err != nil {
		return nil, err
	} else if got != number {
		return nil, notFound(op, subject{in: c.target, what: "review " + strconv.FormatInt(reviewID, 10)})
	}
	return &raw, nil
}

// reviewChangeFailure names what GitHub's rejection of a review change means for the tool; every other
// failure keeps its own message.
func reviewChangeFailure(err error, rejected string) error {
	err = actionsFailure(err, reviewsChangePermission)
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Class == provider.ClassProviderError && failure.Message == rejectedMessage {
		refused := *failure
		refused.Message = rejected
		return &refused
	}
	return err
}

// changeReview sends one review change and reads the review GitHub answers with.
func (c *Client) changeReview(ctx context.Context, op, method, path string, body any, rejected string) (*Review, error) {
	var raw reviewJSON
	if err := c.restChange(ctx, op, method, path, body, &raw); err != nil {
		return nil, reviewChangeFailure(err, rejected)
	}
	if raw.ID < 1 {
		return nil, invalidResponse(op, true)
	}
	view := raw.view()
	return &view, nil
}

// dismissReview dismisses one review of one pull request in one request that is never repeated.
func (c *Client) dismissReview(ctx context.Context, a *reviewArguments) (*Review, error) {
	const op = "dismiss pull request review"
	if _, err := c.findReview(ctx, op, a.Number, a.ReviewID); err != nil {
		return nil, err
	}
	return c.changeReview(ctx, op, http.MethodPut, c.reviewPath(a.Number, a.ReviewID)+"/dismissals",
		map[string]any{"message": a.Message},
		"GitHub refused to dismiss this review as invalid; only an approval or a request for changes that is "+
			"not dismissed yet can be dismissed")
}

// updateReview replaces the summary of one review in one request that is never repeated. Whether the token
// may change it, a pending or its own review, is GitHub's decision.
func (c *Client) updateReview(ctx context.Context, a *reviewArguments) (*Review, error) {
	const op = "update pull request review"
	if _, err := c.findReview(ctx, op, a.Number, a.ReviewID); err != nil {
		return nil, err
	}
	return c.changeReview(ctx, op, http.MethodPut, c.reviewPath(a.Number, a.ReviewID),
		map[string]any{"body": a.Body},
		"GitHub refused to change the text of this review as invalid; only a pending review or a review the "+
			"token's user wrote can change")
}

// deletePendingReview deletes one review in one request that is never repeated, and only when the read
// before it shows the review as pending.
func (c *Client) deletePendingReview(ctx context.Context, a *reviewArguments) (any, error) {
	const op = "delete pending pull request review"
	review, err := c.findReview(ctx, op, a.Number, a.ReviewID)
	if err != nil {
		return nil, err
	}
	if review.State != "PENDING" {
		return nil, invalidRequest(subject{in: c.target, what: "review " + strconv.FormatInt(a.ReviewID, 10)}.String() +
			" is not pending; only a pending review can be deleted")
	}
	if err := c.restChange(ctx, op, http.MethodDelete, c.reviewPath(a.Number, a.ReviewID), nil, nil); err != nil {
		return nil, reviewChangeFailure(err, "GitHub refused to delete this review as invalid; only a pending "+
			"review can be deleted")
	}
	return map[string]any{"deleted": true, "number": a.Number, "review_id": a.ReviewID}, nil
}
