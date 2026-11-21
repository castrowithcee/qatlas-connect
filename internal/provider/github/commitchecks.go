package github

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// github.commit-checks.list and github.commit-checks.wait read and await the checks of one commit that a
// branch, a tag, or a SHA names, with the evaluation of the pull request checks. The ref is resolved to its
// commit once at the start, so a branch that moves on while the wait runs does not change the commit.

// CommitChecks is the combined checks of one commit. Its sha also names the commit of the combined status.
type CommitChecks struct {
	Ref       string       `json:"ref"`
	SHA       string       `json:"sha"`
	Checks    []CheckEntry `json:"checks"`
	Overall   string       `json:"overall"`
	Truncated bool         `json:"truncated"`
	CheckWidth
}

// CommitChecksWait is the answer of the wait: how it ended and the checks of the commit when it did.
type CommitChecksWait struct {
	Ref       string       `json:"ref"`
	SHA       string       `json:"sha"`
	Result    string       `json:"result"`
	Checks    []CheckEntry `json:"checks"`
	Truncated bool         `json:"truncated"`
	Polls     int          `json:"polls"`
}

var commitChecksDefaultFields = []string{"ref", "sha", "checks", "overall", "truncated", "repository"}

var commitRefArgument = capability.Argument{Name: "ref", Description: "Branch, tag, or commit SHA whose " +
	"checks to read; it is resolved to one commit at the start of the call", Required: true}

var commitChecksList = capability.Descriptor{
	ID:            Provider + ".commit-checks.list",
	Version:       1,
	DefaultFields: commitChecksDefaultFields,
	Title:         "List the checks of a GitHub commit",
	Description: "List the check runs and the combined commit status of the commit a branch, tag, or SHA " +
		"names in a repository a connection allows, with one overall evaluation. To wait until they finish, " +
		"use " + Provider + ".commit-checks.wait instead of calling this again",
	Tags:        []string{"github", "commits", "checks", "list"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"ref":`+refSchema+`,`+checkFilterSchemas, "ref"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"ref":{"type":"string"},` +
		`"sha":{"type":"string"},"checks":{"type":"array","items":{"type":"object","properties":{` +
		checkEntryProperties + `},"required":["name","status"],"additionalProperties":false}},` +
		`"overall":{"type":"string"},"truncated":{"type":"boolean"},` + checkWidthMembers + `},` +
		`"required":["ref","sha","checks","overall","truncated"],"additionalProperties":false}`),
	Arguments: append([]capability.Argument{commitRefArgument}, checkFilterArguments...),
	Fields: append([]capability.Field{
		{Name: "ref", Description: "Branch, tag, or commit SHA as named"},
		{Name: "sha", Description: "Commit the ref resolved to; the checks and the combined status were read at it"},
		{Name: "checks", Description: "Check runs and legacy commit statuses of the commit, combined"},
		{Name: "overall", Description: "success, failure, pending, or none when nothing reported a check; with a " +
			"filter set, it covers the filtered check runs and every commit status"},
		{Name: "truncated", Description: "True when GitHub reported more checks or statuses than were returned"},
	}, checkWidthFieldsWithoutSHA()...),
	Examples: []capability.Example{{Description: "List the checks of the main branch",
		Arguments: json.RawMessage(`{"ref":"main"}`)}},
}

var commitChecksWait = capability.Descriptor{
	ID:      Provider + ".commit-checks.wait",
	Version: 1,
	Title:   "Wait for the checks of a GitHub commit",
	Description: "Wait until every check run and commit status of the commit a branch, tag, or SHA names in a " +
		"repository a connection allows has finished, then answer pass, fail, or none; fail as soon as one " +
		"finished check failed. The ref is resolved to one commit at the start, so a branch that moves on is " +
		"not followed. A call waits at most 50 seconds and then answers pending: call it again with the sha " +
		"it answered as ref. With a shell, prefer 'qconnect wait " + Provider + ".commit-checks.wait', which " +
		"waits for the end in one command",
	Tags:        []string{"github", "commits", "checks", "wait"},
	Risk:        readRisk,
	Provider:    Provider,
	Waits:       true,
	InputSchema: inputSchema(`"ref":`+refSchema+`,`+checkFilterSchemas, "ref"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"ref":{"type":"string"},` +
		`"sha":{"type":"string"},"result":{"type":"string","enum":["pass","fail","none","timeout","pending"]},` +
		`"checks":{"type":"array","items":{"type":"object","properties":{` + checkEntryProperties +
		`},"required":["name","status"],"additionalProperties":false}},` +
		`"truncated":{"type":"boolean"},"polls":{"type":"integer"}},` +
		`"required":["ref","sha","result","checks","truncated","polls"],"additionalProperties":false}`),
	Arguments: append([]capability.Argument{commitRefArgument}, checkFilterArguments...),
	Fields: []capability.Field{
		{Name: "result", Description: "pass when all checks finished and none failed, fail when a finished " +
			"check failed, none when no check appeared, timeout when the limit passed, pending when the call " +
			"is still waiting; with a filter set, it covers the filtered check runs and every commit status"},
		{Name: "sha", Description: "Commit awaited; pass it as ref to the next call after pending"},
		{Name: "checks", Description: "Check runs and commit statuses of the commit at the end of the wait: " +
			"name, status, conclusion, and address, untrusted data"},
		{Name: "truncated", Description: "True when GitHub reported more checks than were returned"},
		{Name: "polls", Description: "How often the checks were read"},
		{Name: "ref", Description: "Branch, tag, or commit SHA as named"},
	},
	Examples: []capability.Example{{Description: "Wait for the checks of the main branch",
		Arguments: json.RawMessage(`{"ref":"main"}`)}},
}

// checkCommitChecksArguments applies the bounds of ref and the check run filters before any credential is
// resolved.
func checkCommitChecksArguments(a *pullArguments, _ target) error {
	if err := checkRefArgumentValue(a.Ref); err != nil {
		return err
	}
	return checkFilterValues(a.CheckName, a.CheckStatus, a.CheckFilter, a.AppID)
}

const commitChecksOp = "list commit checks"

// commitChecks resolves the ref to its commit and reads the checks at it.
func (c *Client) commitChecks(ctx context.Context, a *pullArguments) (*CommitChecks, error) {
	sha, err := c.resolveCommitFor(ctx, commitChecksOp, a.Ref, checksReadPermission)
	if err != nil {
		return nil, err
	}
	read, err := c.checksAt(ctx, commitChecksOp, sha, a.checkRunQuery())
	if err != nil {
		return nil, err
	}
	return &CommitChecks{Ref: a.Ref, SHA: sha, Checks: read.Checks, Overall: read.Overall,
		Truncated: read.Truncated, CheckWidth: read.CheckWidth}, nil
}

// waitForCommitChecks resolves the ref once and polls the checks at that commit until they finish or the
// limit passes.
func (c *Client) waitForCommitChecks(ctx context.Context, a *pullArguments) (*CommitChecksWait, error) {
	sha, err := c.resolveCommitFor(ctx, commitChecksOp, a.Ref, checksReadPermission)
	if err != nil {
		return nil, err
	}
	query := a.checkRunQuery()
	end, err := pollChecks(ctx, "wait for commit checks", sha, func(ctx context.Context) (string, *commitChecks, error) {
		read, err := c.checksAt(ctx, commitChecksOp, sha, query)
		return sha, read, err
	})
	if err != nil {
		return nil, err
	}
	return &CommitChecksWait{Ref: a.Ref, SHA: sha, Result: end.result, Checks: end.checks.Checks,
		Truncated: end.checks.Truncated, Polls: end.polls}, nil
}
