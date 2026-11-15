package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Auto-merge of a pull request. GitHub offers it only through GraphQL; both mutations take the node
// identifier of the pull request, which comes from the REST read that binds the number to the bound
// repository, never from the caller.

const pullAutoMergeOutput = `{"type":"object","properties":{"number":{"type":"integer"},` +
	`"auto_merge":{"type":"boolean"},"merge_method":{"type":"string","enum":["merge","squash","rebase"]}},` +
	`"required":["number","auto_merge"],"additionalProperties":false}`

var pullAutoMergeFields = []capability.Field{
	{Name: "number", Description: "Pull request number"},
	{Name: "auto_merge", Description: "True while auto-merge is enabled on the pull request, false once it is off"},
	{Name: "merge_method", Description: "merge, squash, or rebase while auto-merge is enabled; absent otherwise"},
}

var pullAutoMergeEnable = capability.Descriptor{
	ID:      Provider + ".pull-request-auto-merge.enable",
	Version: 1,
	Title:   "Enable auto-merge of a GitHub pull request",
	Description: "Enable auto-merge on one open pull request of a repository a connection allows, only while " +
		"its head is still the given commit; GitHub then merges it once its required checks and reviews pass. " +
		"Auto-merge must be allowed in the repository's settings. Offered only where a connection's tools list " +
		"names it, because the merge later lands in the wrong repository's branch if aimed wrongly; a head " +
		"that changed since ends as an invalid request naming the expected commit",
	Tags:                  []string{"github", "pulls", "pullrequests", "merge", "auto-merge", "enable"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"number":`+numberSchema+`,"expected_head_sha":`+blobSHASchema+`,"merge_method":{`+
		`"type":"string","enum":["merge","squash","rebase"]},"commit_title":`+titleSchema+`,"commit_message":`+bodySchema,
		"number", "expected_head_sha", "merge_method"),
	OutputSchema: json.RawMessage(pullAutoMergeOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Pull request number in the repository", Required: true},
		{Name: "expected_head_sha", Description: "Head commit the pull request must still be at; " +
			"github.pull-requests.get reports the current one", Required: true},
		{Name: "merge_method", Description: "merge, squash, or rebase", Required: true},
		{Name: "commit_title", Description: "Title of the merge commit, 1 to 256 characters; GitHub's default " +
			"when omitted"},
		{Name: "commit_message", Description: "Extra detail for the merge commit, at most 65536 characters"},
	},
	Fields: pullAutoMergeFields,
	Examples: []capability.Example{{
		Description: "Enable auto-merge by squashing",
		Arguments: json.RawMessage(`{"number":42,"expected_head_sha":"ebca79b1db4fcbb136e6094c13e8451428c8a6ab",` +
			`"merge_method":"squash"}`),
	}},
}

var pullAutoMergeDisable = capability.Descriptor{
	ID:      Provider + ".pull-request-auto-merge.disable",
	Version: 1,
	Title:   "Disable auto-merge of a GitHub pull request",
	Description: "Disable auto-merge on one pull request of a repository a connection allows; a pull request " +
		"without auto-merge is left as it is and reported with auto-merge off",
	Tags:         []string{"github", "pulls", "pullrequests", "merge", "auto-merge", "disable"},
	Risk:         changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:     Provider,
	InputSchema:  inputSchema(`"number":`+numberSchema, "number"),
	OutputSchema: json.RawMessage(pullAutoMergeOutput),
	Arguments: []capability.Argument{
		{Name: "number", Description: "Pull request number in the repository", Required: true},
	},
	Fields:   pullAutoMergeFields,
	Examples: []capability.Example{{Description: "Disable auto-merge", Arguments: json.RawMessage(`{"number":42}`)}},
}

func checkPullAutoMergeEnableArguments(a *pullArguments, bound target) error {
	if err := checkPullBranchUpdateArguments(a, bound); err != nil {
		return err
	}
	a.MergeMethod = strings.ToLower(a.MergeMethod)
	if !containsFold(mergeMethods, a.MergeMethod) {
		return invalidRequest("merge_method must be merge, squash, or rebase")
	}
	if a.CommitTitle != "" {
		if err := checkPullTitle("commit_title", a.CommitTitle); err != nil {
			return err
		}
	}
	if a.CommitMessage != "" {
		return checkBoundedText("commit_message", a.CommitMessage, maxBodyLength)
	}
	return nil
}

// PullRequestAutoMerge is the answer of an auto-merge change: whether it is enabled afterwards, and how.
type PullRequestAutoMerge struct {
	Number      int    `json:"number"`
	AutoMerge   bool   `json:"auto_merge"`
	MergeMethod string `json:"merge_method,omitempty"`
}

// Passing the head as expectedHeadOid makes GitHub refuse a head that moved after the read.
const (
	enableAutoMergeMutation = `mutation($id:ID!,$method:PullRequestMergeMethod!,$oid:GitObjectID!,` +
		`$headline:String,$body:String){enable:enablePullRequestAutoMerge(input:{pullRequestId:$id,` +
		`mergeMethod:$method,expectedHeadOid:$oid,commitHeadline:$headline,commitBody:$body}){` +
		`pullRequest{number autoMergeRequest{mergeMethod}}}}`
	disableAutoMergeMutation = `mutation($id:ID!){disable:disablePullRequestAutoMerge(input:{pullRequestId:$id}){` +
		`pullRequest{number autoMergeRequest{mergeMethod}}}}`
)

const autoMergePermission = "GitHub refused this change of auto-merge on a pull request of this repository; it " +
	"needs repo on a classic token, or Pull requests: read and write plus Contents: read and write on a " +
	"fine-grained token"

// autoMergeAnswer is the part of either mutation's answer this provider reads.
type autoMergeAnswer struct {
	Number           int `json:"number"`
	AutoMergeRequest *struct {
		MergeMethod string `json:"mergeMethod"`
	} `json:"autoMergeRequest"`
}

// autoMergeFailure names what a refused auto-merge change most likely means. GitHub's own text never enters
// the message; it is matched only to tell the three known refusals apart, and stays in the failure's original
// account. A refused head comparison reads the pull request again to name the current head.
func (c *Client) autoMergeFailure(ctx context.Context, op string, number int, expected string, err error) error {
	var failure *provider.Error
	if !errors.As(err, &failure) {
		return err
	}
	if failure.Class == provider.ClassPermission {
		refused := *failure
		refused.Message = autoMergePermission
		return &refused
	}
	if failure.Class != provider.ClassProviderError || failure.Original == nil {
		return err
	}
	mentions := func(text ...string) bool {
		for _, entry := range failure.Original.Entries {
			message := strings.ToLower(entry.Message)
			for _, t := range text {
				if strings.Contains(message, t) {
					return true
				}
			}
		}
		return false
	}
	refused := *failure
	switch {
	case mentions("auto merge is not allowed", "auto-merge is not allowed"):
		refused.Message = fmt.Sprintf("GitHub refused to enable auto-merge on pull request #%d because "+
			"auto-merge is not allowed in this repository; allow auto-merge in the repository's settings first", number)
	case expected != "" && mentions("head branch was modified"):
		current := "unknown"
		if pull, readErr := c.pullRequest(ctx, op, number); readErr == nil {
			current = pull.Head.SHA
		}
		return invalidRequest(fmt.Sprintf("GitHub refused to enable auto-merge on pull request #%d because its "+
			"head branch changed: expected head commit %s, now %s; read the pull request again and enable "+
			"auto-merge with its current head commit", number, expected, current))
	case mentions("clean status"):
		refused.Message = fmt.Sprintf("GitHub refused to enable auto-merge on pull request #%d because it is "+
			"already ready to merge, so there is nothing to wait for; merge it instead", number)
	default:
		return err
	}
	return &refused
}

// enablePullRequestAutoMerge enables auto-merge on one open pull request in one mutation that is never
// repeated, only while its head is still a.ExpectedHeadSHA: compared after the read, and again by GitHub.
func (c *Client) enablePullRequestAutoMerge(ctx context.Context, a *pullArguments) (*PullRequestAutoMerge, error) {
	const op = "enable pull request auto-merge"
	current, err := c.pullRequest(ctx, op, a.Number)
	if err != nil {
		return nil, err
	}
	if current.NodeID == "" {
		return nil, invalidResponse(op, false)
	}
	if current.State != "open" || current.Merged {
		return nil, invalidRequest(fmt.Sprintf("pull request #%d is not open", a.Number))
	}
	if current.Head.SHA != a.ExpectedHeadSHA {
		return nil, invalidRequest(fmt.Sprintf("pull request #%d has head commit %s, not the expected %s; read "+
			"the pull request again and enable auto-merge with its current head commit", a.Number,
			current.Head.SHA, a.ExpectedHeadSHA))
	}
	variables := map[string]any{"id": current.NodeID, "method": strings.ToUpper(a.MergeMethod),
		"oid": a.ExpectedHeadSHA, "headline": nil, "body": nil}
	if a.CommitTitle != "" {
		variables["headline"] = a.CommitTitle
	}
	if a.CommitMessage != "" {
		variables["body"] = a.CommitMessage
	}
	var answer struct {
		Enable *struct {
			PullRequest autoMergeAnswer `json:"pullRequest"`
		} `json:"enable"`
	}
	if err := c.mutate(ctx, op, enableAutoMergeMutation, variables, &answer); err != nil {
		return nil, c.autoMergeFailure(ctx, op, a.Number, a.ExpectedHeadSHA, err)
	}
	if answer.Enable == nil || answer.Enable.PullRequest.Number != a.Number ||
		answer.Enable.PullRequest.AutoMergeRequest == nil {
		return nil, invalidResponse(op, true)
	}
	return &PullRequestAutoMerge{Number: a.Number, AutoMerge: true,
		MergeMethod: strings.ToLower(answer.Enable.PullRequest.AutoMergeRequest.MergeMethod)}, nil
}

// disablePullRequestAutoMerge disables auto-merge on one pull request in one mutation that is never repeated.
// A pull request without auto-merge is reported as such without a mutation.
func (c *Client) disablePullRequestAutoMerge(ctx context.Context, number int) (*PullRequestAutoMerge, error) {
	const op = "disable pull request auto-merge"
	current, err := c.pullRequest(ctx, op, number)
	if err != nil {
		return nil, err
	}
	if current.AutoMerge == nil {
		return &PullRequestAutoMerge{Number: number}, nil
	}
	if current.NodeID == "" {
		return nil, invalidResponse(op, false)
	}
	var answer struct {
		Disable *struct {
			PullRequest autoMergeAnswer `json:"pullRequest"`
		} `json:"disable"`
	}
	if err := c.mutate(ctx, op, disableAutoMergeMutation, map[string]any{"id": current.NodeID}, &answer); err != nil {
		return nil, c.autoMergeFailure(ctx, op, number, "", err)
	}
	if answer.Disable == nil || answer.Disable.PullRequest.Number != number {
		return nil, invalidResponse(op, true)
	}
	if request := answer.Disable.PullRequest.AutoMergeRequest; request != nil {
		return &PullRequestAutoMerge{Number: number, AutoMerge: true, MergeMethod: strings.ToLower(request.MergeMethod)}, nil
	}
	return &PullRequestAutoMerge{Number: number}, nil
}
