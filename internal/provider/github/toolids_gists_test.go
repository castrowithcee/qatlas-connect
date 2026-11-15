package github

import "github.com/castrowithcee/qatlas-connect/internal/capability"

// Expected tools of the "gists" group: ID, output sample and risk contract, one tool per line in sorted order.
func init() {
	addExpectedTools("gists", []expectedTool{
		{id: "github.gist-comments.create", born: true, output: GistComment{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.gist-comments.delete", born: true, allow: true, output: GistCommentDeleted{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, gistSensitivity)},
		{id: "github.gist-comments.get", born: true, output: GistComment{}, risk: readRisk},
		{id: "github.gist-comments.list", born: true, output: GistCommentList{}, risk: readRisk},
		{id: "github.gist-comments.update", born: true, output: GistComment{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.gist-commits.list", born: true, output: GistCommitList{}, risk: readRisk},
		{id: "github.gist-forks.create", born: true, output: GistDetail{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.gist-forks.list", born: true, output: GistForkList{}, risk: readRisk},
		{id: "github.gist-stars.set", born: true, output: GistStarChange{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.gists.create", output: Gist{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.gists.delete", allow: true, output: GistDeleted{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, gistSensitivity)},
		{id: "github.gists.get", output: GistDetail{}, risk: readRisk},
		{id: "github.gists.list", output: GistList{}, risk: readRisk},
		{id: "github.gists.update", output: Gist{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
	})
}
