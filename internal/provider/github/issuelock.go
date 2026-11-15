package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// github.issues.lock and github.issues.unlock lock and unlock the conversation of one issue; github.issues.pin
// and github.issues.unpin pin it to the repository's issue list and release it. Every call reads the issue
// below the bound repository first, which binds the number to it and, for pin and unpin, refuses a pull
// request, since GitHub pins only issues; lock and unlock also take a pull request number. Lock
// and unlock are one REST request. GitHub offers pinning only as the GraphQL mutations pinIssue and
// unpinIssue, which take a node identifier: it is the node_id of that very read, never an argument. Each
// change is sent once and never repeated, and no answer carries issue text.

// lockReasons are the reasons GitHub accepts for locking a conversation.
var lockReasons = []string{"off-topic", "too heated", "resolved", "spam"}

const (
	issueLockOutput = `{"type":"object","properties":{"number":{"type":"integer"},"locked":{"type":"boolean"},` +
		`"lock_reason":{"type":"string"},"is_pull_request":{"type":"boolean"}},"required":["number","locked"],"additionalProperties":false}`
	issuePinOutput = `{"type":"object","properties":{"number":{"type":"integer"},"pinned":{"type":"boolean"}},` +
		`"required":["number","pinned"],"additionalProperties":false}`

	issueLockPermission = "GitHub refused to change the lock of this conversation; it needs push access to the " +
		"repository, and repo (or public_repo for a public repository) on a classic token, or Issues: read and " +
		"write on a fine-grained token"
	issuePinPermission = "GitHub refused to change the pin of this issue; as far as GitHub documents it, pinning " +
		"needs write access to the repository, and repo on a classic token, or Issues: read and write on a " +
		"fine-grained token"
	issuePinRefused = "GitHub refused to pin this issue, most likely because the repository already holds three " +
		"pinned issues, the most GitHub allows; unpin one first"
)

var issueLockFields = []capability.Field{
	{Name: "number", Description: "Number of the issue this call acted on"},
	{Name: "locked", Description: "Whether the conversation is locked after this call"},
	{Name: "lock_reason", Description: "Reason the conversation was locked with; absent when none was given"},
	{Name: "is_pull_request", Description: "True when the number names a pull request; absent for an issue"},
}

var issuePinFields = []capability.Field{
	{Name: "number", Description: "Number of the issue this call acted on"},
	{Name: "pinned", Description: "Whether GitHub reports the issue as pinned after this call"},
}

var issuesLock = capability.Descriptor{
	ID:      Provider + ".issues.lock",
	Version: 1,
	Title:   "Lock a GitHub issue or pull request conversation",
	Description: "Lock the conversation of one issue or pull request of a repository a connection allows, " +
		"optionally with a reason; only people with push access can comment on it afterwards; locking a locked " +
		"conversation succeeds; offered only where a connection's tools list names it, because it " +
		"shuts out everyone else's contributions",
	Tags:                  []string{"github", "issues", "lock", "update"},
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"number":`+numberSchema+`,"lock_reason":{"type":"string",`+
		`"enum":["off-topic","too heated","resolved","spam"]}`, "number"),
	OutputSchema: json.RawMessage(issueLockOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Issue or pull request number in the repository", Required: true},
		{Name: "lock_reason", Description: "off-topic, too heated, resolved, or spam; none when left out"},
	},
	Fields: issueLockFields,
	Examples: []capability.Example{{
		Description: "Lock an issue that went off topic",
		Arguments:   json.RawMessage(`{"number":42,"lock_reason":"off-topic"}`),
	}},
}

var issuesUnlock = capability.Descriptor{
	ID:           Provider + ".issues.unlock",
	Version:      1,
	Title:        "Unlock a GitHub issue or pull request conversation",
	Description:  "Unlock the conversation of one issue or pull request of a repository a connection allows; unlocking an unlocked conversation succeeds",
	Tags:         []string{"github", "issues", "unlock", "update"},
	Risk:         changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":`+numberSchema, "number"),
	OutputSchema: json.RawMessage(issueLockOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Issue or pull request number in the repository", Required: true},
	},
	Fields: issueLockFields,
	Examples: []capability.Example{{
		Description: "Unlock an issue",
		Arguments:   json.RawMessage(`{"number":42}`),
	}},
}

var issuesPin = capability.Descriptor{
	ID:      Provider + ".issues.pin",
	Version: 1,
	Title:   "Pin a GitHub issue",
	Description: "Pin one issue of a repository a connection allows to the top of its issue list; a repository " +
		"holds at most three pinned issues and GitHub refuses a fourth; a pull request number is refused",
	Tags:         []string{"github", "issues", "pin", "update"},
	Risk:         changeRisk(capability.EffectUpdate, capability.IdempotencyUnknown),
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":`+numberSchema, "number"),
	OutputSchema: json.RawMessage(issuePinOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Issue number in the repository", Required: true},
	},
	Fields: issuePinFields,
	Examples: []capability.Example{{
		Description: "Pin an issue",
		Arguments:   json.RawMessage(`{"number":42}`),
	}},
}

var issuesUnpin = capability.Descriptor{
	ID:           Provider + ".issues.unpin",
	Version:      1,
	Title:        "Unpin a GitHub issue",
	Description:  "Release the pin of one issue of a repository a connection allows; a pull request number is refused",
	Tags:         []string{"github", "issues", "unpin", "update"},
	Risk:         changeRisk(capability.EffectUpdate, capability.IdempotencyUnknown),
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":`+numberSchema, "number"),
	OutputSchema: json.RawMessage(issuePinOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Issue number in the repository", Required: true},
	},
	Fields: issuePinFields,
	Examples: []capability.Example{{
		Description: "Unpin an issue",
		Arguments:   json.RawMessage(`{"number":42}`),
	}},
}

// IssueLock is the answer of github.issues.lock and github.issues.unlock.
type IssueLock struct {
	Number     int    `json:"number"`
	Locked     bool   `json:"locked"`
	LockReason string `json:"lock_reason,omitempty"`
	// IsPullRequest is true when the number names a pull request, whose conversation GitHub locks on the
	// same route.
	IsPullRequest bool `json:"is_pull_request,omitempty"`
}

// IssuePin is the answer of github.issues.pin and github.issues.unpin: the state GitHub reports.
type IssuePin struct {
	Number int  `json:"number"`
	Pinned bool `json:"pinned"`
}

type issueLockArgs struct {
	Number     int    `json:"number"`
	LockReason string `json:"lock_reason"`
}

func checkIssueLock(a *issueLockArgs, lock bool) error {
	if err := checkNumber(a.Number); err != nil {
		return err
	}
	if a.LockReason == "" {
		return nil
	}
	if !lock {
		return invalidRequest("lock_reason is only allowed when locking")
	}
	for _, reason := range lockReasons {
		if a.LockReason == reason {
			return nil
		}
	}
	return invalidRequest("lock_reason must be one of off-topic, too heated, resolved, spam")
}

// issueLockHandler checks the arguments and the repository before a credential is resolved, so a refused
// request never becomes a provider call.
func issueLockHandler(id string, call func(context.Context, *Client, *issueLockArgs) (any, error),
	lock bool) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments issueLockArgs
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if err := checkIssueLock(&arguments, lock); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

func issueLockOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: issuesLock, Handler: issueLockHandler(issuesLock.ID,
			func(ctx context.Context, c *Client, a *issueLockArgs) (any, error) {
				return c.setIssueLock(ctx, a, true)
			}, true)},
		{Descriptor: issuesUnlock, Handler: issueLockHandler(issuesUnlock.ID,
			func(ctx context.Context, c *Client, a *issueLockArgs) (any, error) {
				return c.setIssueLock(ctx, a, false)
			}, false)},
		{Descriptor: issuesPin, Handler: issueLockHandler(issuesPin.ID,
			func(ctx context.Context, c *Client, a *issueLockArgs) (any, error) {
				return c.setIssuePin(ctx, a.Number, true)
			}, false)},
		{Descriptor: issuesUnpin, Handler: issueLockHandler(issuesUnpin.ID,
			func(ctx context.Context, c *Client, a *issueLockArgs) (any, error) {
				return c.setIssuePin(ctx, a.Number, false)
			}, false)},
	}
}

// readConversation reads the issue route of the bound repository for a number that may name an issue or a
// pull request, since GitHub locks both on the issue lock route, and reports whether it is a pull request.
// The read binds the number to the repository the way readIssue does.
func (c *Client) readConversation(ctx context.Context, op string, number int) (bool, error) {
	if err := checkNumber(number); err != nil {
		return false, err
	}
	var raw restIssueJSON
	if err := c.rest(ctx, op, issuePath(c.target, number), &raw); err != nil {
		return false, err
	}
	if raw.Number != number {
		return false, invalidResponse(op, false)
	}
	return raw.PullRequest != nil, nil
}

// setIssueLock reads the issue to bind the number to the repository, then sends one request. GitHub answers
// 204 whether or not the conversation was already in the requested state.
func (c *Client) setIssueLock(ctx context.Context, a *issueLockArgs, lock bool) (*IssueLock, error) {
	op := "unlock issue"
	if lock {
		op = "lock issue"
	}
	if c.target.kind != kindRepository {
		return nil, providerError(op, "this connection is not bound to a repository")
	}
	isPull, err := c.readConversation(ctx, op, a.Number)
	if err != nil {
		return nil, err
	}
	path := issuePath(c.target, a.Number) + "/lock"
	method, body := http.MethodDelete, any(nil)
	if lock {
		// An empty object rather than JSON null when no reason is given.
		method, body = http.MethodPut, any(struct{}{})
		if a.LockReason != "" {
			body = map[string]any{"lock_reason": a.LockReason}
		}
	}
	if err := c.restChange(ctx, op, method, path, body, nil); err != nil {
		return nil, actionsFailure(err, issueLockPermission)
	}
	return &IssueLock{Number: a.Number, Locked: lock, LockReason: a.LockReason, IsPullRequest: isPull}, nil
}

const (
	pinIssueMutation   = `mutation($id:ID!){result:pinIssue(input:{issueId:$id}){issue{number isPinned}}}`
	unpinIssueMutation = `mutation($id:ID!){result:unpinIssue(input:{issueId:$id}){issue{number isPinned}}}`
)

// setIssuePin reads the issue below the bound repository and sends one GraphQL mutation with the node_id of
// that answer. GitHub's own state in the mutation answer is reported, so a repeated pin or unpin shows what
// GitHub did.
func (c *Client) setIssuePin(ctx context.Context, number int, pin bool) (*IssuePin, error) {
	op := "unpin issue"
	document := unpinIssueMutation
	if pin {
		op, document = "pin issue", pinIssueMutation
	}
	if c.target.kind != kindRepository {
		return nil, providerError(op, "this connection is not bound to a repository")
	}
	issue, err := c.readIssue(ctx, op, number)
	if err != nil {
		return nil, err
	}
	if issue.NodeID == nil || *issue.NodeID == "" {
		return nil, invalidEntry(op, "an issue")
	}
	var answer struct {
		Result *struct {
			Issue *struct {
				Number   int   `json:"number"`
				IsPinned *bool `json:"isPinned"`
			} `json:"issue"`
		} `json:"result"`
	}
	if err := c.mutate(ctx, op, document, map[string]any{"id": *issue.NodeID}, &answer); err != nil {
		var failure *provider.Error
		if errors.As(err, &failure) && failure.Class == provider.ClassProviderError &&
			failure.Message == "GitHub rejected the change" && pin {
			refused := *failure
			refused.Message = issuePinRefused
			return nil, &refused
		}
		return nil, actionsFailure(err, issuePinPermission)
	}
	if answer.Result == nil || answer.Result.Issue == nil || answer.Result.Issue.IsPinned == nil ||
		answer.Result.Issue.Number != number {
		return nil, invalidResponse(op, true)
	}
	return &IssuePin{Number: number, Pinned: *answer.Result.Issue.IsPinned}, nil
}
