package github

import "github.com/castrowithcee/qatlas-connect/internal/capability"

// Expected tools of the "organization-admin" group: ID, output sample and risk contract, one tool per line in sorted order.
func init() {
	addExpectedTools("organization-admin", []expectedTool{
		{id: "github.app-installation-repositories.remove", born: true, allow: true, output: AppInstallationRepositoryRemoved{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, accessGrantSensitivity)},
		{id: "github.app-installations.list", born: true, output: AppInstallationList{}, risk: readRisk},
		{id: "github.apps.get", output: AppDetails{}, risk: readRisk},
		{id: "github.audit-log.list", born: true, output: AuditLogList{}, risk: auditLogRisk},
		{id: "github.billing-ai-credits.get", born: true, output: BillingModelReport{}, risk: billingRisk},
		{id: "github.billing-budgets.get", born: true, output: BillingBudget{}, risk: billingRisk},
		{id: "github.billing-budgets.list", born: true, output: BillingBudgetList{}, risk: billingRisk},
		{id: "github.billing-premium-requests.get", born: true, output: BillingModelReport{}, risk: billingRisk},
		{id: "github.billing-usage-summary.get", born: true, output: BillingSummary{}, risk: billingRisk},
		{id: "github.billing-usage.get", born: true, output: BillingUsage{}, risk: billingRisk},
		{id: "github.network-configurations.create", born: true, allow: true, output: NetworkConfiguration{}, risk: guardedRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, networkConfigurationSensitivity)},
		{id: "github.network-configurations.delete", born: true, allow: true, output: NetworkConfigurationDeleted{}, risk: guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, networkConfigurationSensitivity)},
		{id: "github.network-configurations.get", born: true, output: NetworkConfiguration{}, risk: networkReadRisk},
		{id: "github.network-configurations.list", born: true, output: NetworkConfigurationList{}, risk: networkReadRisk},
		{id: "github.network-configurations.update", born: true, allow: true, output: NetworkConfigurationUpdated{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, networkConfigurationSensitivity)},
		{id: "github.network-settings.get", born: true, output: NetworkSettings{}, risk: networkReadRisk},
	})
}
