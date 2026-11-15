package github

import "github.com/castrowithcee/qatlas-connect/internal/capability"

// Expected tools of the "pull-requests" group: ID, output sample and risk contract, one tool per line in sorted order.
func init() {
	addExpectedTools("pull-requests", []expectedTool{
		{id: "github.check-annotations.list", born: true, output: CheckAnnotationList{}, risk: readRisk},
		{id: "github.check-runs.get", born: true, output: CheckRun{}, risk: readRisk},
		{id: "github.check-runs.list", born: true, output: CheckRunList{}, risk: readRisk},
		{id: "github.check-suite-runs.list", born: true, output: CheckSuiteRunList{}, risk: readRisk},
		{id: "github.check-suites.list", born: true, output: CheckSuiteList{}, risk: readRisk},
		{id: "github.copilot-reviews.request", output: PullRequest{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-request-auto-merge.disable", born: true, output: PullRequestAutoMerge{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-request-auto-merge.enable", born: true, allow: true, output: PullRequestAutoMerge{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, dataSensitivity)},
		{id: "github.pull-request-branches.update", output: PullRequestBranchUpdate{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent)},
		{id: "github.pull-request-checks.list", output: PullRequestChecks{}, risk: readRisk},
		{id: "github.pull-request-checks.wait", born: true, output: PullRequestChecksWait{}, risk: readRisk},
		{id: "github.pull-request-comments.create", output: Comment{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.pull-request-comments.list", output: CommentList{}, risk: readRisk},
		{id: "github.pull-request-commits.list", output: PullRequestCommitList{}, risk: readRisk},
		{id: "github.pull-request-diffs.get", output: PullRequestDiff{}, risk: readRisk},
		{id: "github.pull-request-files.list", output: PullRequestFileList{}, risk: readRisk},
		{id: "github.pull-request-review-comments.delete", born: true, allow: true, output: map[string]any{"deleted": true, "comment_id": int64(0)}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, reviewCommentSensitivity)},
		{id: "github.pull-request-review-comments.list", output: ReviewCommentList{}, risk: readRisk},
		{id: "github.pull-request-review-comments.reply", output: ReviewComment{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.pull-request-review-comments.update", born: true, output: ReviewComment{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-request-review-threads.list", output: ReviewThreadList{}, risk: readRisk},
		{id: "github.pull-request-review-threads.resolve", output: ReviewThreadState{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-request-review-threads.unresolve", output: ReviewThreadState{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-request-reviewers.remove", output: PullRequest{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-request-reviewers.request", output: PullRequest{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-request-reviews.approve", allow: true, output: Review{}, risk: guardedRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, dataSensitivity)},
		{id: "github.pull-request-reviews.create", output: Review{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.pull-request-reviews.delete-pending", born: true, output: map[string]any{"deleted": true, "number": 0, "review_id": int64(0)}, risk: changeRisk(capability.EffectDelete, capability.IdempotencyUnknown)},
		{id: "github.pull-request-reviews.dismiss", born: true, allow: true, output: Review{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, reviewSensitivity)},
		{id: "github.pull-request-reviews.list", output: ReviewList{}, risk: readRisk},
		{id: "github.pull-request-reviews.update", born: true, output: Review{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-requests.close", output: PullRequest{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-requests.create", output: PullRequest{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.pull-requests.get", output: PullRequestDetail{}, risk: readRisk},
		{id: "github.pull-requests.list", output: PullRequestList{}, risk: readRisk},
		{id: "github.pull-requests.merge", allow: true, output: PullRequestMerge{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, dataSensitivity)},
		{id: "github.pull-requests.reopen", output: PullRequest{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.pull-requests.search", output: PullRequestSearchList{}, risk: readRisk},
		{id: "github.pull-requests.update", output: PullRequest{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
	})
}
