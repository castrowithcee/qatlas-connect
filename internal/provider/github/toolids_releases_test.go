package github

import "github.com/castrowithcee/qatlas-connect/internal/capability"

// Expected tools of the "releases" group: ID, output sample and risk contract, one tool per line in sorted order.
func init() {
	addExpectedTools("releases", []expectedTool{
		{id: "github.release-assets.delete", born: true, allow: true, output: DeletedReleaseAsset{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity)},
		{id: "github.release-assets.list", output: ReleaseAssetList{}, risk: readRisk},
		{id: "github.release-assets.update", born: true, output: ReleaseAsset{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.release-notes.generate", born: true, output: ReleaseNotes{}, risk: readRisk},
		{id: "github.releases.create", output: Release{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.releases.delete", allow: true, output: DeletedRelease{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity)},
		{id: "github.releases.get", output: Release{}, risk: readRisk},
		{id: "github.releases.list", output: ReleaseList{}, risk: readRisk},
		{id: "github.releases.update", output: Release{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
	})
}
