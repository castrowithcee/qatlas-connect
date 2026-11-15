package github

import "github.com/castrowithcee/qatlas-connect/internal/capability"

// Expected tools of the "security" group: ID, output sample and risk contract, one tool per line in sorted order.
func init() {
	addExpectedTools("security", []expectedTool{
		{id: "github.code-quality-findings.get", output: CodeQualityFinding{}, risk: readRisk},
		{id: "github.code-scanning-alert-instances.list", born: true, output: CodeScanningInstanceList{}, risk: readRisk},
		{id: "github.code-scanning-alerts.get", output: CodeScanningAlert{}, risk: readRisk},
		{id: "github.code-scanning-alerts.list", output: CodeScanningAlertList{}, risk: readRisk},
		{id: "github.code-scanning-alerts.update", born: true, allow: true, output: CodeScanningAlert{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent)},
		{id: "github.code-scanning-analyses.get", born: true, output: CodeScanningAnalysis{}, risk: readRisk},
		{id: "github.code-scanning-analyses.list", born: true, output: CodeScanningAnalysisList{}, risk: readRisk},
		{id: "github.code-scanning-setup.get", born: true, output: CodeScanningSetup{}, risk: readRisk},
		{id: "github.code-scanning-setup.update", born: true, allow: true, output: CodeScanningSetupChange{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeScanningSetupSensitivity)},
		{id: "github.code-security-configuration-repositories.list", born: true, output: CodeSecurityConfigurationRepositoryList{}, risk: readRisk},
		{id: "github.code-security-configurations.attach", born: true, allow: true, output: CodeSecurityAttachAccepted{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeSecurityConfigurationSensitivity)},
		{id: "github.code-security-configurations.create", born: true, allow: true, output: CodeSecurityConfiguration{}, risk: guardedRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, codeSecurityConfigurationSensitivity)},
		{id: "github.code-security-configurations.delete", born: true, allow: true, output: CodeSecurityConfigurationDeleted{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, codeSecurityConfigurationSensitivity)},
		{id: "github.code-security-configurations.detach", born: true, allow: true, output: CodeSecurityDetachAccepted{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeSecurityConfigurationSensitivity)},
		{id: "github.code-security-configurations.get", born: true, output: CodeSecurityConfiguration{}, risk: readRisk},
		{id: "github.code-security-configurations.list", born: true, output: CodeSecurityConfigurationList{}, risk: readRisk},
		{id: "github.code-security-configurations.set-default", born: true, allow: true, output: CodeSecurityDefaultSet{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeSecurityConfigurationSensitivity)},
		{id: "github.code-security-configurations.update", born: true, allow: true, output: CodeSecurityConfigurationUpdated{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeSecurityConfigurationSensitivity)},
		{id: "github.code-security-defaults.get", born: true, output: CodeSecurityDefaultList{}, risk: readRisk},
		{id: "github.dependabot-alerts.get", output: DependabotAlert{}, risk: readRisk},
		{id: "github.dependabot-alerts.list", output: DependabotAlertList{}, risk: readRisk},
		{id: "github.dependabot-alerts.update", born: true, allow: true, output: DependabotAlert{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.dependency-review.compare", born: true, output: DependencyReview{}, risk: readRisk},
		{id: "github.dependency-snapshots.create", born: true, allow: true, output: DependencySnapshotCreated{}, risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent)},
		{id: "github.global-advisories.get", output: GlobalAdvisory{}, risk: readRisk},
		{id: "github.global-advisories.list", output: GlobalAdvisoryList{}, risk: readRisk},
		{id: "github.organization-advisories.list", output: RepositoryAdvisoryList{}, risk: readRisk},
		{id: "github.repository-advisories.get", born: true, output: RepositoryAdvisory{}, risk: readRisk},
		{id: "github.repository-advisories.list", output: RepositoryAdvisoryList{}, risk: readRisk},
		{id: "github.secret-scanning-alerts.get", output: SecretScanningAlert{}, risk: readRisk},
		{id: "github.secret-scanning-alerts.list", output: SecretScanningAlertList{}, risk: readRisk},
		{id: "github.secret-scanning-alerts.update", born: true, allow: true, output: SecretScanningAlert{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent)},
		{id: "github.secret-scanning-locations.list", born: true, output: SecretScanningLocationList{}, risk: readRisk},
		{id: "github.security-settings.get", born: true, output: SecuritySettings{}, risk: readRisk},
		{id: "github.security-settings.update", born: true, allow: true, output: SecuritySettingsChange{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, securitySettingSensitivity)},
	})
}
