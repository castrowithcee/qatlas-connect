package github

import "github.com/castrowithcee/qatlas-connect/internal/capability"

// Expected tools of the "discussions" group: ID, output sample and risk contract, one tool per line in sorted order.
func init() {
	addExpectedTools("discussions", []expectedTool{
		{id: "github.discussion-categories.list", output: DiscussionCategoryList{}, risk: readRisk},
		{id: "github.discussion-comments.create", output: DiscussionCommentWritten{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.discussion-comments.delete", allow: true, output: map[string]any{"deleted": true, "comment_id": ""}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, discussionCommentSensitivity)},
		{id: "github.discussion-comments.list", output: DiscussionCommentList{}, risk: readRisk},
		{id: "github.discussion-comments.mark-answer", born: true, output: DiscussionAnswerChanged{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.discussion-comments.unmark-answer", born: true, output: DiscussionAnswerChanged{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.discussion-comments.update", output: DiscussionCommentWritten{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.discussions.close", output: DiscussionChanged{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.discussions.create", output: DiscussionCreated{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.discussions.delete", allow: true, output: map[string]any{"deleted": true, "number": 0}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, discussionSensitivity)},
		{id: "github.discussions.get", output: DiscussionGet{}, risk: readRisk},
		{id: "github.discussions.list", output: DiscussionList{}, risk: readRisk},
		{id: "github.discussions.lock", output: DiscussionChanged{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.discussions.reopen", output: DiscussionChanged{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.discussions.unlock", output: DiscussionChanged{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.discussions.update", output: DiscussionChanged{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
	})
}
