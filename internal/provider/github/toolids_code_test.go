package github

import "github.com/castrowithcee/qatlas-connect/internal/capability"

// Expected tools of the "code" group: ID, output sample and risk contract, one tool per line in sorted order.
func init() {
	addExpectedTools("code", []expectedTool{
		{id: "github.blame.get", output: BlameResult{}, risk: readRisk},
		{id: "github.branches.create", output: BranchCreated{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.branches.delete", allow: true, output: BranchDeleted{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity)},
		{id: "github.branches.get", output: BranchDetail{}, risk: readRisk},
		{id: "github.branches.list", output: BranchList{}, risk: readRisk},
		{id: "github.branches.rename", allow: true, output: BranchRenamed{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent)},
		{id: "github.branches.sync-fork", born: true, allow: true, output: ForkSynced{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.code.search", output: CodeSearchList{}, risk: readRisk},
		{id: "github.commit-branches.list", born: true, output: CommitBranchList{}, risk: readRisk},
		{id: "github.commit-comments.get", born: true, output: RESTCommitComment{}, risk: readRisk},
		{id: "github.commit-comments.list", born: true, output: CommitCommentList{}, risk: readRisk},
		{id: "github.commit-pulls.list", born: true, output: CommitPullList{}, risk: readRisk},
		{id: "github.commits.get", output: CommitDetail{}, risk: readRisk},
		{id: "github.commits.list", output: CommitList{}, risk: readRisk},
		{id: "github.commits.search", output: CommitSearchList{}, risk: readRisk},
		{id: "github.compare.get", output: Comparison{}, risk: readRisk},
		{id: "github.contents.delete", allow: true, output: DeletedContents{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity)},
		{id: "github.contents.get", output: ContentsResult{}, risk: readRisk},
		{id: "github.contents.put", output: WrittenContents{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent)},
		{id: "github.files.push", allow: true, output: FilesPushed{}, risk: guardedRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, dataSensitivity)},
		{id: "github.merges.create", allow: true, output: MergeCreated{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.tags.create", output: TagCreated{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.tags.delete", allow: true, output: TagDeleted{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity)},
		{id: "github.tags.get", output: TagDetail{}, risk: readRisk},
		{id: "github.tags.list", output: TagList{}, risk: readRisk},
		{id: "github.trees.get", output: TreeResult{}, risk: readRisk},
	})
}
