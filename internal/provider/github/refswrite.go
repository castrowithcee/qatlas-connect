package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Ref writes beyond branch creation: renaming a branch, syncing a fork branch with its upstream, merging a
// branch or commit into another branch, and creating or deleting a tag. Every path is fixed and lies below
// the chosen repository. Each change is one request that is never repeated; github.tags.create with a
// message is the only tool with two, and it reports the tag object it made when the ref step fails. Answers
// carry names and SHAs only, and no text GitHub sent ever reaches a message.

const (
	branchesRenamePermission = "GitHub refused to rename this branch; renaming needs repo on a classic token, " +
		"or Administration: read and write on a fine-grained token, and a repository rule may forbid it as well"
	forkSyncPermission = "GitHub refused to sync this branch; it needs repo on a classic token, or Contents: " +
		"read and write on a fine-grained token, and a branch protection or repository rule may forbid it as well"
	mergesPermission = "GitHub refused this merge; it needs repo on a classic token, or Contents: read and " +
		"write on a fine-grained token, and a branch protection or repository rule may forbid it as well; a " +
		"head that changes workflow files may need the workflow scope or Workflows: read and write"
	tagsCreatePermission = "GitHub refused to create this tag; it needs repo on a classic token, or Contents: " +
		"read and write on a fine-grained token, and a repository rule may forbid it as well"
	tagsDeletePermission = "GitHub refused to delete this tag; a repository rule, such as one that makes tags " +
		"immutable, may forbid it, and the token needs repo on a classic token, or Contents: read and write on " +
		"a fine-grained token"
)

const branchRenamedOutput = `{"type":"object","properties":{"branch":{"type":"string"},"new_name":{"type":"string"},` +
	`"ref":{"type":"string"},"sha":{"type":"string"}},"required":["branch","new_name","ref","sha"],` +
	`"additionalProperties":false}`

var branchesRename = capability.Descriptor{
	ID:      Provider + ".branches.rename",
	Version: 1,
	Title:   "Rename a GitHub branch",
	Description: "Rename one branch of a repository a connection allows, the default branch included; GitHub " +
		"retargets open pull requests, branch protection, and the default branch setting to the new name, but " +
		"links and local clones keep the old one; offered only where a connection's tools list names it, " +
		"because a rename breaks everything that still uses the old name",
	Tags:                  []string{"github", "branches", "rename"},
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"branch":`+refSchema+`,"new_name":`+refSchema, "branch", "new_name"),
	OutputSchema:          json.RawMessage(branchRenamedOutput),
	Arguments: []capability.Argument{
		{Name: "branch", Description: "Current name of the branch, without refs/heads/", Required: true},
		{Name: "new_name", Description: "New name of the branch, without refs/heads/; a name already taken is refused",
			Required: true},
	},
	Fields: []capability.Field{
		{Name: "branch", Description: "Former name of the branch"},
		{Name: "new_name", Description: "Name GitHub gave the branch"},
		{Name: "ref", Description: "Full ref of the renamed branch"},
		{Name: "sha", Description: "Commit the renamed branch points at"},
	},
	Examples: []capability.Example{{
		Description: "Rename master to main",
		Arguments:   json.RawMessage(`{"branch":"master","new_name":"main"}`),
	}},
}

const forkSyncedOutput = `{"type":"object","properties":{"branch":{"type":"string"},"merge_type":` +
	`{"type":"string","enum":["merge","fast-forward","none"]},"base_branch":{"type":"string"}},` +
	`"required":["branch","merge_type"],"additionalProperties":false}`

var branchesSyncFork = capability.Descriptor{
	ID:      Provider + ".branches.sync-fork",
	Version: 1,
	Title:   "Sync a GitHub fork branch with its upstream",
	Description: "Bring one branch of a fork a connection allows up to date with the same branch of its upstream " +
		"repository, by fast-forward or, where the fork has commits of its own, by a merge commit; only a fork " +
		"has an upstream; a conflict is reported and nothing is changed; offered only where a connection's " +
		"tools list names it, because the merge writes to the branch",
	Tags:                  []string{"github", "branches", "sync-fork"},
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"branch":`+refSchema, "branch"),
	OutputSchema:          json.RawMessage(forkSyncedOutput),
	Arguments: []capability.Argument{
		{Name: "branch", Description: "Branch of the fork to update, without refs/heads/", Required: true},
	},
	Fields: []capability.Field{
		{Name: "branch", Description: "Branch of the fork that was synced"},
		{Name: "merge_type", Description: "merge, fast-forward, or none when the branch was already up to date"},
		{Name: "base_branch", Description: "Upstream branch the sync read from, as GitHub names it (untrusted text)"},
	},
	Examples: []capability.Example{{
		Description: "Update the main branch of a fork",
		Arguments:   json.RawMessage(`{"branch":"main"}`),
	}},
}

const mergeCreatedOutput = `{"type":"object","properties":{"base":{"type":"string"},"head":{"type":"string"},` +
	`"result":{"type":"string","enum":["merged","nothing-to-merge"]},"sha":{"type":"string"}},` +
	`"required":["base","head","result"],"additionalProperties":false}`

var mergesCreate = capability.Descriptor{
	ID:      Provider + ".merges.create",
	Version: 1,
	Title:   "Merge a GitHub branch or commit into a branch",
	Description: "Merge the existing branch or commit head into the branch base of a repository a connection " +
		"allows, as one merge commit on base; nothing is written but that commit, and head's workflow file " +
		"changes arrive like in a pull request merge; nothing to merge and a conflict are reported as such, " +
		"without a change; offered only where a connection's tools list names it, because it writes to base " +
		"without a pull request or review",
	Tags:                  []string{"github", "merges", "create"},
	Risk:                  changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"base":`+refSchema+`,"head":`+refSchema+`,"commit_message":`+commitMessageSchema,
		"base", "head"),
	OutputSchema: json.RawMessage(mergeCreatedOutput),
	Arguments: []capability.Argument{
		{Name: "base", Description: "Branch that receives the merge commit, without refs/heads/", Required: true},
		{Name: "head", Description: "Branch name or commit SHA of this repository to merge into base", Required: true},
		{Name: "commit_message", Description: "Message of the merge commit, at most 1000 characters; GitHub's " +
			"default when omitted"},
	},
	Fields: []capability.Field{
		{Name: "base", Description: "Branch that was merged into"},
		{Name: "head", Description: "Branch or commit that was merged"},
		{Name: "result", Description: "merged, or nothing-to-merge when base already contains head"},
		{Name: "sha", Description: "Merge commit GitHub created, when result is merged"},
	},
	Examples: []capability.Example{{
		Description: "Merge a release branch into main",
		Arguments:   json.RawMessage(`{"base":"main","head":"release/1.2"}`),
	}},
}

const tagCreatedOutput = `{"type":"object","properties":{"tag":{"type":"string"},"ref":{"type":"string"},` +
	`"sha":{"type":"string"},"commit_sha":{"type":"string"},"annotated":{"type":"boolean"}},` +
	`"required":["tag","ref","sha","commit_sha","annotated"],"additionalProperties":false}`

var tagsCreate = capability.Descriptor{
	ID:      Provider + ".tags.create",
	Version: 1,
	Title:   "Create a GitHub tag",
	Description: "Create one new tag of a repository a connection allows at a commit, given as a branch, a tag, " +
		"or a commit SHA; a lightweight tag, or, when message is given, an annotated tag object first and its " +
		"ref second; a tag name already taken is refused instead of moved; when only the ref step fails, the " +
		"error names the tag object that was created and nothing is repeated",
	Tags:         []string{"github", "tags", "create"},
	Risk:         changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	Provider:     Provider,
	InputSchema:  inputSchema(`"tag":`+refSchema+`,"target":`+refSchema+`,"message":`+commitMessageSchema, "tag", "target"),
	OutputSchema: json.RawMessage(tagCreatedOutput),
	Arguments: []capability.Argument{
		{Name: "tag", Description: "Name of the new tag, without refs/tags/", Required: true},
		{Name: "target", Description: "Branch, tag, or commit SHA the tag points at; resolved to a commit",
			Required: true},
		{Name: "message", Description: "Message of an annotated tag, at most 1000 characters; a lightweight tag " +
			"when omitted"},
	},
	Fields: []capability.Field{
		{Name: "tag", Description: "Name of the created tag"},
		{Name: "ref", Description: "Full ref GitHub created: refs/tags/ plus tag"},
		{Name: "sha", Description: "Object the ref points at: the commit, or the tag object of an annotated tag"},
		{Name: "commit_sha", Description: "Commit the tag points at, resolved from target"},
		{Name: "annotated", Description: "True when a tag object was created"},
	},
	Examples: []capability.Example{{
		Description: "Tag a commit lightweight",
		Arguments:   json.RawMessage(`{"tag":"v1.2.0","target":"main"}`),
	}, {
		Description: "Create an annotated tag",
		Arguments:   json.RawMessage(`{"tag":"v1.2.0","target":"main","message":"Release 1.2.0"}`),
	}},
}

const tagDeletedOutput = `{"type":"object","properties":{"tag":{"type":"string"},"ref":{"type":"string"},` +
	`"sha":{"type":"string"}},"required":["tag","ref","sha"],"additionalProperties":false}`

var tagsDelete = capability.Descriptor{
	ID:      Provider + ".tags.delete",
	Version: 1,
	Title:   "Delete a GitHub tag",
	Description: "Delete one tag of a repository a connection allows; a release that uses the tag loses it and " +
		"becomes a draft; offered only where a connection's tools list names it, because a deleted tag is not " +
		"restored by this tool, and a repository rule such as immutable tags may refuse it",
	Tags:                  []string{"github", "tags", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"tag":`+refSchema, "tag"),
	OutputSchema:          json.RawMessage(tagDeletedOutput),
	Arguments: []capability.Argument{
		{Name: "tag", Description: "Name of the tag to delete, without refs/tags/", Required: true},
	},
	Fields: []capability.Field{
		{Name: "tag", Description: "Name of the deleted tag"},
		{Name: "ref", Description: "Full ref of the deleted tag"},
		{Name: "sha", Description: "Object the tag pointed at before the deletion"},
	},
	Examples: []capability.Example{{
		Description: "Delete a tag created by mistake",
		Arguments:   json.RawMessage(`{"tag":"v1.2.0-rc1"}`),
	}},
}

// BranchRenamed is the answer to a renamed branch.
type BranchRenamed struct {
	Branch  string `json:"branch"`
	NewName string `json:"new_name"`
	Ref     string `json:"ref"`
	SHA     string `json:"sha"`
}

// ForkSynced is the answer to a synced fork branch.
type ForkSynced struct {
	Branch     string `json:"branch"`
	MergeType  string `json:"merge_type"`
	BaseBranch string `json:"base_branch,omitempty"`
}

// MergeCreated is the answer to a merge; SHA is empty when there was nothing to merge.
type MergeCreated struct {
	Base   string `json:"base"`
	Head   string `json:"head"`
	Result string `json:"result"`
	SHA    string `json:"sha,omitempty"`
}

// TagCreated is the answer to a created tag.
type TagCreated struct {
	Tag       string `json:"tag"`
	Ref       string `json:"ref"`
	SHA       string `json:"sha"`
	CommitSHA string `json:"commit_sha"`
	Annotated bool   `json:"annotated"`
}

// TagDeleted is the answer to a deleted tag.
type TagDeleted struct {
	Tag string `json:"tag"`
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

func refsWriteOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: branchesRename, Handler: contentsWriteHandler(branchesRename.ID, checkBranchesRenameArguments,
			func(ctx context.Context, c *Client, a *contentsWriteArguments) (any, error) {
				return c.renameBranch(ctx, a)
			})},
		{Descriptor: branchesSyncFork, Handler: contentsWriteHandler(branchesSyncFork.ID, checkBranchNameArgument,
			func(ctx context.Context, c *Client, a *contentsWriteArguments) (any, error) {
				return c.syncFork(ctx, a)
			})},
		{Descriptor: mergesCreate, Handler: contentsWriteHandler(mergesCreate.ID, checkMergesCreateArguments,
			func(ctx context.Context, c *Client, a *contentsWriteArguments) (any, error) {
				return c.createMerge(ctx, a)
			})},
		{Descriptor: tagsCreate, Handler: contentsWriteHandler(tagsCreate.ID, checkTagsCreateArguments,
			func(ctx context.Context, c *Client, a *contentsWriteArguments) (any, error) {
				return c.createTag(ctx, a)
			})},
		{Descriptor: tagsDelete, Handler: contentsWriteHandler(tagsDelete.ID, checkTagsDeleteArguments,
			func(ctx context.Context, c *Client, a *contentsWriteArguments) (any, error) {
				return c.deleteTag(ctx, a)
			})},
	}
}

func checkRefName(name, value, kind string) error {
	switch {
	case strings.HasPrefix(value, "refs/"):
		return invalidRequest(name + " must be the " + kind + " name without refs/")
	case !validRef(value):
		return invalidRequest(name + " must be a usable " + kind + " name")
	}
	return nil
}

func checkBranchNameArgument(a *contentsWriteArguments) error {
	return checkRefName("branch", a.Branch, "branch")
}

func checkBranchesRenameArguments(a *contentsWriteArguments) error {
	if err := checkRefName("branch", a.Branch, "branch"); err != nil {
		return err
	}
	if err := checkRefName("new_name", a.NewName, "branch"); err != nil {
		return err
	}
	if a.Branch == a.NewName {
		return invalidRequest("new_name must differ from branch")
	}
	return nil
}

func checkMergesCreateArguments(a *contentsWriteArguments) error {
	if err := checkRefName("base", a.Base, "branch"); err != nil {
		return err
	}
	if !validRef(a.Head) {
		return invalidRequest("head must be a branch name or a commit SHA")
	}
	if a.CommitMessage != "" {
		if utf8.RuneCountInString(a.CommitMessage) > maxCommitMessage {
			return invalidRequest(fmt.Sprintf("commit_message must hold at most %d characters", maxCommitMessage))
		}
		return checkText("commit_message", a.CommitMessage)
	}
	return nil
}

func checkTagsCreateArguments(a *contentsWriteArguments) error {
	if err := checkRefName("tag", a.Tag, "tag"); err != nil {
		return err
	}
	if !validRef(a.Target) {
		return invalidRequest("target must be a branch, a tag, or a commit SHA")
	}
	if a.Message != "" {
		if utf8.RuneCountInString(a.Message) > maxCommitMessage {
			return invalidRequest(fmt.Sprintf("message must hold at most %d characters", maxCommitMessage))
		}
		return checkText("message", a.Message)
	}
	return nil
}

func checkTagsDeleteArguments(a *contentsWriteArguments) error {
	return checkRefName("tag", a.Tag, "tag")
}

// refusedByRule reports whether a 422 text says a protection or a repository rule stopped the change.
func refusedByRule(text []byte) bool {
	return bytes.Contains(text, []byte("protected")) || bytes.Contains(text, []byte("repository rule")) ||
		bytes.Contains(text, []byte("rule violation"))
}

// permissionRefusal turns a rule refusal into a permission failure with the tool's own message.
func permissionRefusal(err error, message string) error {
	var failure *provider.Error
	if errors.As(err, &failure) {
		refused := *failure
		refused.Class = provider.ClassPermission
		refused.Message = message
		return &refused
	}
	return err
}

// sendRef sends exactly one change request below the chosen repository and returns the status and body of a
// successful answer. A transport failure that may have reached GitHub, a 5xx, and an unreadable answer say the
// change may have been applied. On a refusal, refine sees the status and the lower-cased text, which never
// reaches a message, and may return a more precise error; a nil result keeps the generic classification with
// the tool's permission message.
func (c *Client) sendRef(ctx context.Context, op, method, path string, body any, permission string,
	refine func(status int, text []byte, err error) error) (int, []byte, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, providerError(op, "the request could not be built")
		}
		payload = bytes.NewReader(data)
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return 0, nil, provider.Waited(op, "GitHub", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoints.rest+path, payload)
	if err != nil {
		return 0, nil, providerError(op, "the request could not be built")
	}
	c.authorize(req)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	defer c.limiter.HoldFor(mutationInterval)
	if err != nil {
		failure := provider.Transport(op, "GitHub", err)
		if failure.MayHaveArrived() {
			failure.Message += uncertain
		}
		return 0, nil, failure
	}
	defer response.Body.Close()
	c.observeRateLimit(response.Header)
	if response.StatusCode < 200 || response.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body = io.NopCloser(bytes.NewReader(data))
		err := c.statusError(op, response, true)
		if refine != nil {
			if refined := refine(response.StatusCode, bytes.ToLower(data), err); refined != nil {
				return 0, nil, refined
			}
		}
		return 0, nil, actionsFailure(err, permission)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return 0, nil, &provider.Error{Class: provider.ClassInvalidResponse, Op: op,
			Message: "the GitHub response could not be read within the size limit" + uncertain}
	}
	return response.StatusCode, data, nil
}

// renameBranch sends one rename. GitHub refuses a name that is taken or unusable with 422.
func (c *Client) renameBranch(ctx context.Context, a *contentsWriteArguments) (*BranchRenamed, error) {
	const op = "rename branch"
	path := c.repoPath("branches/" + url.PathEscape(a.Branch) + "/rename")
	_, data, err := c.sendRef(ctx, op, http.MethodPost, path, map[string]string{"new_name": a.NewName},
		branchesRenamePermission, func(status int, text []byte, err error) error {
			switch {
			case status != http.StatusUnprocessableEntity:
				return nil
			case refusedByRule(text):
				return permissionRefusal(err, branchesRenamePermission)
			}
			return invalidRequest("GitHub rejected the rename; the new name may already be taken by another " +
				"branch or may not be usable, and nothing was changed")
		})
	if err != nil {
		return nil, err
	}
	var answer struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if json.Unmarshal(data, &answer) != nil || answer.Name != a.NewName || !validCommitSHA(answer.Commit.SHA) {
		return nil, invalidResponse(op, true)
	}
	return &BranchRenamed{Branch: a.Branch, NewName: a.NewName, Ref: "refs/heads/" + a.NewName,
		SHA: answer.Commit.SHA}, nil
}

// syncFork sends one merge-upstream request. GitHub answers 409 for a conflict and 422 for a repository
// without an upstream or a branch the upstream lacks.
func (c *Client) syncFork(ctx context.Context, a *contentsWriteArguments) (*ForkSynced, error) {
	const op = "sync fork branch"
	_, data, err := c.sendRef(ctx, op, http.MethodPost, c.repoPath("merge-upstream"),
		map[string]string{"branch": a.Branch}, forkSyncPermission, func(status int, text []byte, err error) error {
			switch {
			case status == http.StatusConflict:
				return providerError(op, "the branch could not be synced because it conflicts with its upstream "+
					"branch; nothing was changed")
			case status != http.StatusUnprocessableEntity:
				return nil
			case refusedByRule(text):
				return permissionRefusal(err, forkSyncPermission)
			}
			return invalidRequest("GitHub rejected the sync; the repository must be a fork, and the branch must " +
				"exist in its upstream repository")
		})
	if err != nil {
		return nil, err
	}
	var answer struct {
		MergeType  string `json:"merge_type"`
		BaseBranch string `json:"base_branch"`
	}
	if json.Unmarshal(data, &answer) != nil {
		return nil, invalidResponse(op, true)
	}
	switch answer.MergeType {
	case "merge", "fast-forward", "none":
	default:
		return nil, invalidResponse(op, true)
	}
	result := &ForkSynced{Branch: a.Branch, MergeType: answer.MergeType}
	if len(answer.BaseBranch) <= 255 && utf8.ValidString(answer.BaseBranch) {
		result.BaseBranch = answer.BaseBranch
	}
	return result, nil
}

// createMerge sends one merge. 201 is a merge commit, 204 means base already holds head, and 409 is a
// conflict; the last two are answers about the state, not failures of the request.
func (c *Client) createMerge(ctx context.Context, a *contentsWriteArguments) (*MergeCreated, error) {
	const op = "merge branches"
	body := map[string]string{"base": a.Base, "head": a.Head}
	if a.CommitMessage != "" {
		body["commit_message"] = a.CommitMessage
	}
	status, data, err := c.sendRef(ctx, op, http.MethodPost, c.repoPath("merges"), body, mergesPermission,
		func(status int, text []byte, err error) error {
			switch {
			case status == http.StatusConflict:
				return providerError(op, "the merge has a conflict that cannot be resolved automatically, so "+
					"nothing was merged; resolve it in a pull request or locally")
			case status != http.StatusUnprocessableEntity:
				return nil
			case refusedByRule(text):
				return permissionRefusal(err, mergesPermission)
			}
			return invalidRequest("GitHub rejected the merge; base must be a branch and head a branch or " +
				"commit of this repository")
		})
	if err != nil {
		return nil, err
	}
	result := &MergeCreated{Base: a.Base, Head: a.Head}
	switch status {
	case http.StatusNoContent:
		result.Result = "nothing-to-merge"
	case http.StatusCreated:
		var answer struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(data, &answer) != nil || !validCommitSHA(answer.SHA) {
			return nil, invalidResponse(op, true)
		}
		result.Result, result.SHA = "merged", answer.SHA
	default:
		return nil, invalidResponse(op, true)
	}
	return result, nil
}

// createTag resolves target to a commit and creates the ref; with a message it creates the tag object first.
// Only the ref step makes the tag, so a failure after the object step is reported with the object's SHA.
func (c *Client) createTag(ctx context.Context, a *contentsWriteArguments) (*TagCreated, error) {
	const op = "create tag"
	commit, err := c.resolveCommitFor(ctx, op, a.Target, tagsCreatePermission)
	if err != nil {
		return nil, err
	}
	object := commit
	if a.Message != "" {
		_, data, err := c.sendRef(ctx, op, http.MethodPost, c.repoPath("git/tags"),
			map[string]string{"tag": a.Tag, "message": a.Message, "object": commit, "type": "commit"},
			tagsCreatePermission, nil)
		if err != nil {
			return nil, err
		}
		var created struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(data, &created) != nil || !validCommitSHA(created.SHA) {
			return nil, invalidResponse(op, true)
		}
		object = created.SHA
	}
	ref := "refs/tags/" + a.Tag
	_, data, err := c.sendRef(ctx, op, http.MethodPost, c.repoPath("git/refs"),
		map[string]string{"ref": ref, "sha": object}, tagsCreatePermission,
		func(status int, text []byte, err error) error {
			switch {
			case status != http.StatusUnprocessableEntity:
				return nil
			case bytes.Contains(text, []byte("already exists")):
				return invalidRequest("GitHub already holds a tag named " + a.Tag + "; choose another name")
			case refusedByRule(text):
				return permissionRefusal(err, tagsCreatePermission)
			}
			return nil
		})
	if err == nil {
		var answer struct {
			Ref    string `json:"ref"`
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}
		if json.Unmarshal(data, &answer) != nil || answer.Ref != ref || !validCommitSHA(answer.Object.SHA) {
			err = invalidResponse(op, true)
		} else {
			return &TagCreated{Tag: a.Tag, Ref: ref, SHA: answer.Object.SHA, CommitSHA: commit,
				Annotated: a.Message != ""}, nil
		}
	}
	if a.Message == "" {
		return nil, err
	}
	return nil, withTagObject(err, object)
}

// withTagObject adds the created tag object to a failed ref step, keeping the failure's class.
func withTagObject(err error, object string) error {
	note := "; the annotated tag object " + object + " was created but no tag refers to it, and nothing was repeated"
	var invalid *provider.InvalidRequestError
	if errors.As(err, &invalid) {
		return &provider.InvalidRequestError{Message: invalid.Message + note}
	}
	var failure *provider.Error
	if errors.As(err, &failure) {
		partial := *failure
		partial.Message += note
		return &partial
	}
	return err
}

// deleteTag reads the tag's ref first, so a missing tag is answered without a delete and the answer can name
// what the tag pointed at, then sends one DELETE.
func (c *Client) deleteTag(ctx context.Context, a *contentsWriteArguments) (*TagDeleted, error) {
	const op = "delete tag"
	ref := "refs/tags/" + a.Tag
	var raw struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.rest(ctx, op, c.repoPath("git/ref/tags/"+url.PathEscape(a.Tag)), &raw); err != nil {
		return nil, actionsFailure(err, tagsDeletePermission)
	}
	if raw.Ref != ref || !validCommitSHA(raw.Object.SHA) {
		return nil, invalidEntry(op, "a tag")
	}
	status, _, err := c.sendRef(ctx, op, http.MethodDelete, c.repoPath("git/refs/tags/"+url.PathEscape(a.Tag)), nil,
		tagsDeletePermission, func(status int, text []byte, err error) error {
			if status != http.StatusUnprocessableEntity {
				return nil
			}
			if bytes.Contains(text, []byte("reference does not exist")) {
				var failure *provider.Error
				if errors.As(err, &failure) {
					missing := notFound(op, subject{what: "tag " + a.Tag})
					missing.Original = failure.Original
					return missing
				}
			}
			if refusedByRule(text) {
				return permissionRefusal(err, tagsDeletePermission)
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	if status != http.StatusNoContent {
		return nil, invalidResponse(op, true)
	}
	return &TagDeleted{Tag: a.Tag, Ref: ref, SHA: raw.Object.SHA}, nil
}
