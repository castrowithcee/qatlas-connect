package github

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// The discussion maintenance tools change, close, reopen, lock, unlock, and delete one discussion of the bound
// repository, and mark or unmark a comment as its answer, through GraphQL. The discussion number is read back
// from the bound repository, and a comment id is read back and must belong to a discussion of it; the
// mutation then uses the node id of that reading, never one the caller supplied. Every mutation is sent once
// and never repeated. Only a top-level comment of a discussion in an answerable category can be marked as the
// answer, which the reading checks before the mutation. Unverified against live GitHub: whether repeating a
// state change answers an error, so the state-setting tools are described by the state they leave.

const discussionSensitivity = "github-discussion"

var discussionCloseReasons = []string{"resolved", "outdated", "duplicate"}

const discussionChangedOutput = `{"type":"object","properties":{"id":{"type":"string"},"number":{"type":"integer"},` +
	`"url":{"type":"string"},"closed":{"type":"boolean"},"locked":{"type":"boolean"}},` +
	`"required":["id","number"],"additionalProperties":false}`

var discussionChangedFields = []capability.Field{
	{Name: "id", Description: "Node id of the discussion"},
	{Name: "number", Description: "Number of the discussion in the repository"},
	{Name: "url", Description: "Web address of the discussion"},
	{Name: "closed", Description: "Whether the discussion is closed after the change"},
	{Name: "locked", Description: "Whether the discussion is locked after the change"},
}

const discussionNumberDescription = "Discussion number in the repository"

var discussionsUpdate = capability.Descriptor{
	ID:      Provider + ".discussions.update",
	Version: 1,
	Title:   "Update a GitHub discussion",
	Description: "Change the title, the body, or the category of one discussion of a repository a connection " +
		"allows; at least one of them; a category of another repository is refused; the written text is not echoed",
	Tags:     []string{"github", "discussions", "update"},
	Risk:     changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"number":`+numberSchema+`,"title":`+titleSchema+`,"body":{"type":"string",`+
		`"minLength":1,"maxLength":65536},"category_id":`+discussionIDSchema, "number"),
	OutputSchema: json.RawMessage(discussionChangedOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: discussionNumberDescription, Required: true},
		{Name: "title", Description: "New title, 1 to 256 characters"},
		{Name: "body", Description: "New body in Markdown, 1 to 65536 characters; replaces the current body"},
		{Name: "category_id", Description: "Category id, as github.discussion-categories.list reports it"},
	},
	Fields: discussionChangedFields,
	Examples: []capability.Example{{
		Description: "Retitle a discussion",
		Arguments:   json.RawMessage(`{"number":12,"title":"Roadmap ideas 2027"}`),
	}},
}

var discussionsClose = capability.Descriptor{
	ID:      Provider + ".discussions.close",
	Version: 1,
	Title:   "Close a GitHub discussion",
	Description: "Close one discussion of a repository a connection allows, optionally as resolved, outdated, " +
		"or duplicate",
	Tags:     []string{"github", "discussions", "close", "update"},
	Risk:     changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"number":`+numberSchema+`,"reason":{"type":"string",`+
		`"enum":["resolved","outdated","duplicate"]}`, "number"),
	OutputSchema: json.RawMessage(discussionChangedOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: discussionNumberDescription, Required: true},
		{Name: "reason", Description: "resolved, outdated, or duplicate; GitHub's default when left out"},
	},
	Fields: discussionChangedFields,
	Examples: []capability.Example{{
		Description: "Close an answered discussion",
		Arguments:   json.RawMessage(`{"number":12,"reason":"resolved"}`),
	}},
}

var discussionsReopen = capability.Descriptor{
	ID:           Provider + ".discussions.reopen",
	Version:      1,
	Title:        "Reopen a GitHub discussion",
	Description:  "Reopen one closed discussion of a repository a connection allows",
	Tags:         []string{"github", "discussions", "reopen", "update"},
	Risk:         changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":`+numberSchema, "number"),
	OutputSchema: json.RawMessage(discussionChangedOutput),
	Arguments:    []capability.Argument{{Name: "number", Description: discussionNumberDescription, Required: true}},
	Fields:       discussionChangedFields,
	Examples: []capability.Example{{
		Description: "Reopen a discussion",
		Arguments:   json.RawMessage(`{"number":12}`),
	}},
}

var discussionsLock = capability.Descriptor{
	ID:      Provider + ".discussions.lock",
	Version: 1,
	Title:   "Lock a GitHub discussion",
	Description: "Lock one discussion of a repository a connection allows, optionally with a reason; only " +
		"people with write access can comment on it afterwards",
	Tags:     []string{"github", "discussions", "lock", "update"},
	Risk:     changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"number":`+numberSchema+`,"lock_reason":{"type":"string",`+
		`"enum":["off-topic","too heated","resolved","spam"]}`, "number"),
	OutputSchema: json.RawMessage(discussionChangedOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: discussionNumberDescription, Required: true},
		{Name: "lock_reason", Description: "off-topic, too heated, resolved, or spam; none when left out"},
	},
	Fields: discussionChangedFields,
	Examples: []capability.Example{{
		Description: "Lock a discussion that went off topic",
		Arguments:   json.RawMessage(`{"number":12,"lock_reason":"off-topic"}`),
	}},
}

var discussionsUnlock = capability.Descriptor{
	ID:           Provider + ".discussions.unlock",
	Version:      1,
	Title:        "Unlock a GitHub discussion",
	Description:  "Unlock one discussion of a repository a connection allows",
	Tags:         []string{"github", "discussions", "unlock", "update"},
	Risk:         changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":`+numberSchema, "number"),
	OutputSchema: json.RawMessage(discussionChangedOutput),
	Arguments:    []capability.Argument{{Name: "number", Description: discussionNumberDescription, Required: true}},
	Fields:       discussionChangedFields,
	Examples: []capability.Example{{
		Description: "Unlock a discussion",
		Arguments:   json.RawMessage(`{"number":12}`),
	}},
}

const discussionAnswerOutput = `{"type":"object","properties":{"comment_id":{"type":"string"},` +
	`"number":{"type":"integer"},"answered":{"type":"boolean"}},` +
	`"required":["comment_id","number","answered"],"additionalProperties":false}`

var discussionAnswerFields = []capability.Field{
	{Name: "comment_id", Description: "Node id of the comment"},
	{Name: "number", Description: "Number of the discussion in the repository"},
	{Name: "answered", Description: "Whether the discussion has a chosen answer after the change"},
}

var discussionCommentsMarkAnswer = capability.Descriptor{
	ID:      Provider + ".discussion-comments.mark-answer",
	Version: 1,
	Title:   "Mark a GitHub discussion comment as the answer",
	Description: "Choose one top-level comment, by its node id, as the answer of its discussion in a repository " +
		"a connection allows; only discussions of a category that accepts answers have one, and a reply cannot " +
		"be chosen; a comment of another repository is refused",
	Tags:         []string{"github", "discussions", "comments", "answer", "update"},
	Risk:         changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:     Provider,
	InputSchema:  inputSchema(`"comment_id":`+discussionIDSchema, "comment_id"),
	OutputSchema: json.RawMessage(discussionAnswerOutput),
	Arguments: []capability.Argument{{Name: "comment_id", Required: true, Description: "Node id of a top-level " +
		"comment, as github.discussion-comments.list reports it"}},
	Fields: discussionAnswerFields,
	Examples: []capability.Example{{
		Description: "Accept a comment as the answer",
		Arguments:   json.RawMessage(`{"comment_id":"DC_kwDOExample"}`),
	}},
}

var discussionCommentsUnmarkAnswer = capability.Descriptor{
	ID:      Provider + ".discussion-comments.unmark-answer",
	Version: 1,
	Title:   "Unmark a GitHub discussion comment as the answer",
	Description: "Remove the answer mark from one comment, by its node id, of a discussion of a repository a " +
		"connection allows; a comment of another repository is refused",
	Tags:         []string{"github", "discussions", "comments", "answer", "update"},
	Risk:         changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:     Provider,
	InputSchema:  inputSchema(`"comment_id":`+discussionIDSchema, "comment_id"),
	OutputSchema: json.RawMessage(discussionAnswerOutput),
	Arguments: []capability.Argument{{Name: "comment_id", Required: true, Description: "Node id of the " +
		"comment that is marked as the answer"}},
	Fields: discussionAnswerFields,
	Examples: []capability.Example{{
		Description: "Withdraw an answer",
		Arguments:   json.RawMessage(`{"comment_id":"DC_kwDOExample"}`),
	}},
}

var discussionsDelete = capability.Descriptor{
	ID:      Provider + ".discussions.delete",
	Version: 1,
	Title:   "Delete a GitHub discussion",
	Description: "Delete one discussion of a repository a connection allows permanently, with its comments; " +
		"offered only where a connection's tools list names it",
	Tags:                  []string{"github", "discussions", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, discussionSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"number":`+numberSchema, "number"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"deleted":{"type":"boolean"},` +
		`"number":{"type":"integer"}},"required":["deleted","number"],"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "number", Description: "Number of the discussion to delete", Required: true}},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the discussion"},
		{Name: "number", Description: "Number of the deleted discussion"},
	},
	Examples: []capability.Example{{
		Description: "Delete a discussion",
		Arguments:   json.RawMessage(`{"number":12}`),
	}},
}

// DiscussionChanged is the answer of a discussion change; written text is not echoed.
type DiscussionChanged struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	URL    string `json:"url,omitempty"`
	Closed bool   `json:"closed"`
	Locked bool   `json:"locked"`
}

// DiscussionAnswerChanged is the answer of marking or unmarking an answer.
type DiscussionAnswerChanged struct {
	CommentID string `json:"comment_id"`
	Number    int    `json:"number"`
	Answered  bool   `json:"answered"`
}

type discussionChangedJSON struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	URL    string `json:"url"`
	Closed bool   `json:"closed"`
	Locked bool   `json:"locked"`
}

func (d *discussionChangedJSON) changed(op string) (*DiscussionChanged, error) {
	if d == nil || d.ID == "" || d.Number < 1 {
		return nil, invalidResponse(op, true)
	}
	return &DiscussionChanged{ID: d.ID, Number: d.Number, URL: d.URL, Closed: d.Closed, Locked: d.Locked}, nil
}

const discussionChangedSelection = `discussion{id number url closed locked}`

// boundDiscussionID reads the discussion number from the bound repository and returns the node id of that
// reading, which the mutation uses.
func (c *Client) boundDiscussionID(ctx context.Context, op string, number int) (string, error) {
	if c.target.kind != kindRepository {
		return "", providerError(op, "this connection is not bound to a repository")
	}
	got, err := c.getDiscussion(ctx, number)
	if err != nil {
		return "", actionsFailure(err, discussionWritePermission)
	}
	if got.Discussion.ID == "" {
		return "", invalidResponse(op, false)
	}
	return got.Discussion.ID, nil
}

const updateDiscussionMutation = `mutation($input:UpdateDiscussionInput!){updateDiscussion(input:$input){` +
	discussionChangedSelection + `}}`

// UpdateDiscussion changes the title, body, or category of one discussion of the bound repository.
func (c *Client) UpdateDiscussion(ctx context.Context, number int, a *discussionUpdateArguments) (*DiscussionChanged, error) {
	const op = "update discussion"
	if err := a.check(); err != nil {
		return nil, err
	}
	id, err := c.boundDiscussionID(ctx, op, number)
	if err != nil {
		return nil, err
	}
	input := map[string]any{"discussionId": id}
	if a.Title != nil {
		input["title"] = strings.TrimSpace(*a.Title)
	}
	if a.Body != nil {
		input["body"] = *a.Body
	}
	if a.CategoryID != nil {
		if _, err := c.checkDiscussionCategory(ctx, op, *a.CategoryID); err != nil {
			return nil, err
		}
		input["categoryId"] = *a.CategoryID
	}
	var answer struct {
		Update struct {
			Discussion *discussionChangedJSON `json:"discussion"`
		} `json:"updateDiscussion"`
	}
	if err := c.mutate(ctx, op, updateDiscussionMutation, map[string]any{"input": input}, &answer); err != nil {
		return nil, actionsFailure(err, discussionWritePermission)
	}
	return answer.Update.Discussion.changed(op)
}

const closeDiscussionMutation = `mutation($id:ID!,$reason:DiscussionCloseReason){` +
	`closeDiscussion(input:{discussionId:$id,reason:$reason}){` + discussionChangedSelection + `}}`

// CloseDiscussion closes one discussion of the bound repository.
func (c *Client) CloseDiscussion(ctx context.Context, number int, reason string) (*DiscussionChanged, error) {
	const op = "close discussion"
	id, err := c.boundDiscussionID(ctx, op, number)
	if err != nil {
		return nil, err
	}
	var enum any
	if reason != "" {
		enum = strings.ToUpper(reason)
	}
	var answer struct {
		Close struct {
			Discussion *discussionChangedJSON `json:"discussion"`
		} `json:"closeDiscussion"`
	}
	if err := c.mutate(ctx, op, closeDiscussionMutation, map[string]any{"id": id, "reason": enum}, &answer); err != nil {
		return nil, actionsFailure(err, discussionWritePermission)
	}
	return answer.Close.Discussion.changed(op)
}

const reopenDiscussionMutation = `mutation($id:ID!){reopenDiscussion(input:{discussionId:$id}){` +
	discussionChangedSelection + `}}`

// ReopenDiscussion reopens one discussion of the bound repository.
func (c *Client) ReopenDiscussion(ctx context.Context, number int) (*DiscussionChanged, error) {
	const op = "reopen discussion"
	id, err := c.boundDiscussionID(ctx, op, number)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Reopen struct {
			Discussion *discussionChangedJSON `json:"discussion"`
		} `json:"reopenDiscussion"`
	}
	if err := c.mutate(ctx, op, reopenDiscussionMutation, map[string]any{"id": id}, &answer); err != nil {
		return nil, actionsFailure(err, discussionWritePermission)
	}
	return answer.Reopen.Discussion.changed(op)
}

const lockDiscussionMutation = `mutation($id:ID!,$reason:LockReason){lockLockable(input:{lockableId:$id,lockReason:$reason}){` +
	`lockedRecord{... on Discussion{id number url closed locked}}}}`

const unlockDiscussionMutation = `mutation($id:ID!){unlockLockable(input:{lockableId:$id}){` +
	`unlockedRecord{... on Discussion{id number url closed locked}}}}`

// LockDiscussion locks one discussion of the bound repository.
func (c *Client) LockDiscussion(ctx context.Context, number int, reason string) (*DiscussionChanged, error) {
	const op = "lock discussion"
	id, err := c.boundDiscussionID(ctx, op, number)
	if err != nil {
		return nil, err
	}
	var enum any
	if reason != "" {
		enum = strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(reason))
	}
	var answer struct {
		Lock struct {
			Record *discussionChangedJSON `json:"lockedRecord"`
		} `json:"lockLockable"`
	}
	if err := c.mutate(ctx, op, lockDiscussionMutation, map[string]any{"id": id, "reason": enum}, &answer); err != nil {
		return nil, actionsFailure(err, discussionWritePermission)
	}
	return answer.Lock.Record.changed(op)
}

// UnlockDiscussion unlocks one discussion of the bound repository.
func (c *Client) UnlockDiscussion(ctx context.Context, number int) (*DiscussionChanged, error) {
	const op = "unlock discussion"
	id, err := c.boundDiscussionID(ctx, op, number)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Unlock struct {
			Record *discussionChangedJSON `json:"unlockedRecord"`
		} `json:"unlockLockable"`
	}
	if err := c.mutate(ctx, op, unlockDiscussionMutation, map[string]any{"id": id}, &answer); err != nil {
		return nil, actionsFailure(err, discussionWritePermission)
	}
	return answer.Unlock.Record.changed(op)
}

const deleteDiscussionMutation = `mutation($id:ID!){deleteDiscussion(input:{id:$id}){discussion{id number}}}`

// DeleteDiscussion removes one discussion of the bound repository permanently.
func (c *Client) DeleteDiscussion(ctx context.Context, number int) (any, error) {
	const op = "delete discussion"
	id, err := c.boundDiscussionID(ctx, op, number)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Delete struct {
			Discussion *discussionChangedJSON `json:"discussion"`
		} `json:"deleteDiscussion"`
	}
	if err := c.mutate(ctx, op, deleteDiscussionMutation, map[string]any{"id": id}, &answer); err != nil {
		return nil, actionsFailure(err, discussionWritePermission)
	}
	return map[string]any{"deleted": true, "number": number}, nil
}

const markAnswerMutation = `mutation($id:ID!){markDiscussionCommentAsAnswer(input:{id:$id}){discussion{number isAnswered}}}`

const unmarkAnswerMutation = `mutation($id:ID!){unmarkDiscussionCommentAsAnswer(input:{id:$id}){` +
	`discussion{number isAnswered}}}`

type discussionAnswerJSON struct {
	Discussion *struct {
		Number     int  `json:"number"`
		IsAnswered bool `json:"isAnswered"`
	} `json:"discussion"`
}

func (a discussionAnswerJSON) changed(op, comment string) (*DiscussionAnswerChanged, error) {
	if a.Discussion == nil || a.Discussion.Number < 1 {
		return nil, invalidResponse(op, true)
	}
	return &DiscussionAnswerChanged{CommentID: comment, Number: a.Discussion.Number, Answered: a.Discussion.IsAnswered}, nil
}

// MarkDiscussionAnswer chooses a top-level comment of a discussion in an answerable category as its answer.
func (c *Client) MarkDiscussionAnswer(ctx context.Context, id string) (*DiscussionAnswerChanged, error) {
	const op = "mark discussion answer"
	if c.target.kind != kindRepository {
		return nil, providerError(op, "this connection is not bound to a repository")
	}
	ref, err := c.readDiscussionComment(ctx, op, id)
	if err != nil {
		return nil, err
	}
	if ref.IsReply {
		return nil, invalidRequest("only a top-level comment can be marked as the answer")
	}
	if !ref.Answerable {
		return nil, invalidRequest("the category of this discussion does not accept answers")
	}
	var answer struct {
		Mark discussionAnswerJSON `json:"markDiscussionCommentAsAnswer"`
	}
	if err := c.mutate(ctx, op, markAnswerMutation, map[string]any{"id": ref.ID}, &answer); err != nil {
		return nil, actionsFailure(err, discussionWritePermission)
	}
	return answer.Mark.changed(op, ref.ID)
}

// UnmarkDiscussionAnswer removes the answer mark from a comment of the bound repository.
func (c *Client) UnmarkDiscussionAnswer(ctx context.Context, id string) (*DiscussionAnswerChanged, error) {
	const op = "unmark discussion answer"
	if c.target.kind != kindRepository {
		return nil, providerError(op, "this connection is not bound to a repository")
	}
	ref, err := c.readDiscussionComment(ctx, op, id)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Unmark discussionAnswerJSON `json:"unmarkDiscussionCommentAsAnswer"`
	}
	if err := c.mutate(ctx, op, unmarkAnswerMutation, map[string]any{"id": ref.ID}, &answer); err != nil {
		return nil, actionsFailure(err, discussionWritePermission)
	}
	return answer.Unmark.changed(op, ref.ID)
}

type discussionNumberArguments struct {
	Number int `json:"number"`
}

type discussionCloseArguments struct {
	Number int    `json:"number"`
	Reason string `json:"reason"`
}

type discussionLockArguments struct {
	Number     int    `json:"number"`
	LockReason string `json:"lock_reason"`
}

type discussionUpdateArguments struct {
	Number     int     `json:"number"`
	Title      *string `json:"title"`
	Body       *string `json:"body"`
	CategoryID *string `json:"category_id"`
}

func (a *discussionUpdateArguments) check() error {
	if err := checkNumber(a.Number); err != nil {
		return err
	}
	if a.Title == nil && a.Body == nil && a.CategoryID == nil {
		return invalidRequest("give at least one of title, body, and category_id")
	}
	if a.Title != nil {
		if t := strings.TrimSpace(*a.Title); t == "" || utf8.RuneCountInString(t) > 256 {
			return invalidRequest("title must hold 1 to 256 characters")
		}
	}
	if a.Body != nil {
		if err := checkCommentBody(*a.Body); err != nil {
			return err
		}
	}
	if a.CategoryID != nil {
		return checkDiscussionNodeID("category_id", *a.CategoryID)
	}
	return nil
}

func checkOneOf(name, value string, allowed []string) error {
	if value == "" {
		return nil
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return invalidRequest(name + " must be one of " + strings.Join(allowed, ", "))
}

func checkDiscussionNumber(a *discussionNumberArguments) error { return checkNumber(a.Number) }

func discussionMaintainOperations() []capability.Operation {
	numbered := func(d capability.Descriptor, call func(context.Context, *Client, int) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: d, Handler: discussionWriteHandler(d.ID, checkDiscussionNumber,
			func(ctx context.Context, c *Client, a *discussionNumberArguments) (any, error) {
				return call(ctx, c, a.Number)
			})}
	}
	commentOp := func(d capability.Descriptor, call func(context.Context, *Client, string) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: d, Handler: discussionWriteHandler(d.ID,
			func(a *discussionCommentChangeArguments) error {
				return checkDiscussionNodeID("comment_id", a.CommentID)
			},
			func(ctx context.Context, c *Client, a *discussionCommentChangeArguments) (any, error) {
				return call(ctx, c, a.CommentID)
			})}
	}
	return []capability.Operation{
		{Descriptor: discussionsUpdate, Handler: discussionWriteHandler(discussionsUpdate.ID,
			(*discussionUpdateArguments).check,
			func(ctx context.Context, c *Client, a *discussionUpdateArguments) (any, error) {
				return c.UpdateDiscussion(ctx, a.Number, a)
			})},
		{Descriptor: discussionsClose, Handler: discussionWriteHandler(discussionsClose.ID,
			func(a *discussionCloseArguments) error {
				if err := checkNumber(a.Number); err != nil {
					return err
				}
				return checkOneOf("reason", a.Reason, discussionCloseReasons)
			},
			func(ctx context.Context, c *Client, a *discussionCloseArguments) (any, error) {
				return c.CloseDiscussion(ctx, a.Number, a.Reason)
			})},
		numbered(discussionsReopen, func(ctx context.Context, c *Client, n int) (any, error) {
			return c.ReopenDiscussion(ctx, n)
		}),
		{Descriptor: discussionsLock, Handler: discussionWriteHandler(discussionsLock.ID,
			func(a *discussionLockArguments) error {
				if err := checkNumber(a.Number); err != nil {
					return err
				}
				return checkOneOf("lock_reason", a.LockReason, lockReasons)
			},
			func(ctx context.Context, c *Client, a *discussionLockArguments) (any, error) {
				return c.LockDiscussion(ctx, a.Number, a.LockReason)
			})},
		numbered(discussionsUnlock, func(ctx context.Context, c *Client, n int) (any, error) {
			return c.UnlockDiscussion(ctx, n)
		}),
		commentOp(discussionCommentsMarkAnswer, func(ctx context.Context, c *Client, id string) (any, error) {
			return c.MarkDiscussionAnswer(ctx, id)
		}),
		commentOp(discussionCommentsUnmarkAnswer, func(ctx context.Context, c *Client, id string) (any, error) {
			return c.UnmarkDiscussionAnswer(ctx, id)
		}),
		numbered(discussionsDelete, func(ctx context.Context, c *Client, n int) (any, error) {
			return c.DeleteDiscussion(ctx, n)
		}),
	}
}
