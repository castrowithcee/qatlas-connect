package github

import "github.com/castrowithcee/qatlas-connect/internal/capability"

// Expected tools of the "packages" group: ID, output sample and risk contract, one tool per line in sorted order.
func init() {
	addExpectedTools("packages", []expectedTool{
		{id: "github.package-versions.delete", born: true, allow: true, output: PackageVersionDeleted{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity)},
		{id: "github.package-versions.get", born: true, output: PackageVersion{}, risk: readRisk},
		{id: "github.package-versions.list", born: true, output: PackageVersionList{}, risk: readRisk},
		{id: "github.package-versions.restore", born: true, allow: true, output: PackageVersionRestored{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyUnknown, dataSensitivity)},
		{id: "github.packages.delete", allow: true, output: PackageDeleted{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity)},
		{id: "github.packages.get", output: Package{}, risk: readRisk},
		{id: "github.packages.list", output: PackageList{}, risk: readRisk},
		{id: "github.packages.restore", allow: true, output: PackageRestored{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyUnknown, dataSensitivity)},
	})
}
