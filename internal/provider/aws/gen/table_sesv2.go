package gen

import "slices"

// deferSESv2 is the reason for every sesv2 operation not curated yet: the service package that curates the
// tools moves each operation it takes into operations or exclude.
const deferSESv2 = "not curated yet; the SES service package decides effect, confirmation and target first"

// Reasons of the sesv2 operations that stay deferred for a package of their own.
const (
	reasonSESDedicated = "dedicated IPs and pools follow in their own package"
	reasonSESTenants   = "tenants, reputation entities, and multi-region endpoints follow in their own package"
	reasonSESJobStart  = "starting and cancelling import and export jobs waits for a decision on where the data may go"
	reasonSESPricing   = "changing the pricing attributes of the account waits for a decision on its cost effect"
)

// sesv2Table curates the sesv2 model. The identity tools read, create, and configure the sender identities of
// the bound account: DKIM, MAIL FROM, feedback forwarding, the default configuration set, and tags. The
// remaining operations wait for the SES tools that follow.
var sesv2Table = Table{
	Service: "sesv2", Segment: "ses", Geltung: Regional,
	Operations: slices.Concat(sesIdentityTools, sesAccountTools, sesDeliverabilityTools),
	Exclude: []Omission{
		{Operation: "SendCustomVerificationEmail", Reason: "sends a verification mail to an address given as an argument, so the tool would send mail to any recipient; a tool never sends mail on its own initiative"},
	},
	Defer: []Omission{
		{Operation: "CancelExportJob", Reason: reasonSESJobStart},
		{Operation: "CreateConfigurationSet", Reason: deferSESv2},
		{Operation: "CreateConfigurationSetEventDestination", Reason: deferSESv2},
		{Operation: "CreateContact", Reason: deferSESv2},
		{Operation: "CreateContactList", Reason: deferSESv2},
		{Operation: "CreateCustomVerificationEmailTemplate", Reason: deferSESv2},
		{Operation: "CreateDedicatedIpPool", Reason: reasonSESDedicated},
		{Operation: "CreateEmailTemplate", Reason: deferSESv2},
		{Operation: "CreateExportJob", Reason: reasonSESJobStart},
		{Operation: "CreateImportJob", Reason: reasonSESJobStart},
		{Operation: "CreateMultiRegionEndpoint", Reason: reasonSESTenants},
		{Operation: "CreateTenant", Reason: reasonSESTenants},
		{Operation: "CreateTenantResourceAssociation", Reason: reasonSESTenants},
		{Operation: "DeleteConfigurationSet", Reason: deferSESv2},
		{Operation: "DeleteConfigurationSetEventDestination", Reason: deferSESv2},
		{Operation: "DeleteContact", Reason: deferSESv2},
		{Operation: "DeleteContactList", Reason: deferSESv2},
		{Operation: "DeleteCustomVerificationEmailTemplate", Reason: deferSESv2},
		{Operation: "DeleteDedicatedIpPool", Reason: reasonSESDedicated},
		{Operation: "DeleteEmailTemplate", Reason: deferSESv2},
		{Operation: "DeleteMultiRegionEndpoint", Reason: reasonSESTenants},
		{Operation: "DeleteSuppressedDestination", Reason: deferSESv2},
		{Operation: "DeleteTenant", Reason: reasonSESTenants},
		{Operation: "DeleteTenantResourceAssociation", Reason: reasonSESTenants},
		{Operation: "GetConfigurationSet", Reason: deferSESv2},
		{Operation: "GetConfigurationSetEventDestinations", Reason: deferSESv2},
		{Operation: "GetContact", Reason: deferSESv2},
		{Operation: "GetContactList", Reason: deferSESv2},
		{Operation: "GetCustomVerificationEmailTemplate", Reason: deferSESv2},
		{Operation: "GetDedicatedIp", Reason: reasonSESDedicated},
		{Operation: "GetDedicatedIpPool", Reason: reasonSESDedicated},
		{Operation: "GetDedicatedIps", Reason: reasonSESDedicated},
		{Operation: "GetEmailTemplate", Reason: deferSESv2},
		{Operation: "GetMultiRegionEndpoint", Reason: reasonSESTenants},
		{Operation: "GetReputationEntity", Reason: reasonSESTenants},
		{Operation: "GetSuppressedDestination", Reason: deferSESv2},
		{Operation: "GetTenant", Reason: reasonSESTenants},
		{Operation: "ListConfigurationSets", Reason: deferSESv2},
		{Operation: "ListContactLists", Reason: deferSESv2},
		{Operation: "ListContacts", Reason: deferSESv2},
		{Operation: "ListCustomVerificationEmailTemplates", Reason: deferSESv2},
		{Operation: "ListDedicatedIpPools", Reason: reasonSESDedicated},
		{Operation: "ListEmailTemplates", Reason: deferSESv2},
		{Operation: "ListMultiRegionEndpoints", Reason: reasonSESTenants},
		{Operation: "ListReputationEntities", Reason: reasonSESTenants},
		{Operation: "ListResourceTenants", Reason: reasonSESTenants},
		{Operation: "ListSuppressedDestinations", Reason: deferSESv2},
		{Operation: "ListTenantResources", Reason: reasonSESTenants},
		{Operation: "ListTenants", Reason: reasonSESTenants},
		{Operation: "PutAccountPricingAttributes", Reason: reasonSESPricing},
		{Operation: "PutConfigurationSetArchivingOptions", Reason: deferSESv2},
		{Operation: "PutConfigurationSetDeliveryOptions", Reason: deferSESv2},
		{Operation: "PutConfigurationSetReputationOptions", Reason: deferSESv2},
		{Operation: "PutConfigurationSetSendingOptions", Reason: deferSESv2},
		{Operation: "PutConfigurationSetSuppressionOptions", Reason: deferSESv2},
		{Operation: "PutConfigurationSetTrackingOptions", Reason: deferSESv2},
		{Operation: "PutConfigurationSetVdmOptions", Reason: deferSESv2},
		{Operation: "PutDedicatedIpInPool", Reason: reasonSESDedicated},
		{Operation: "PutDedicatedIpPoolScalingAttributes", Reason: reasonSESDedicated},
		{Operation: "PutDedicatedIpWarmupAttributes", Reason: reasonSESDedicated},
		{Operation: "PutSuppressedDestination", Reason: deferSESv2},
		{Operation: "PutTenantSuppressionAttributes", Reason: reasonSESTenants},
		{Operation: "SendBulkEmail", Reason: deferSESv2},
		{Operation: "SendEmail", Reason: deferSESv2},
		{Operation: "TestRenderEmailTemplate", Reason: deferSESv2},
		{Operation: "UpdateConfigurationSet", Reason: deferSESv2},
		{Operation: "UpdateConfigurationSetEventDestination", Reason: deferSESv2},
		{Operation: "UpdateContact", Reason: deferSESv2},
		{Operation: "UpdateContactList", Reason: deferSESv2},
		{Operation: "UpdateCustomVerificationEmailTemplate", Reason: deferSESv2},
		{Operation: "UpdateEmailTemplate", Reason: deferSESv2},
		{Operation: "UpdateReputationEntityCustomerManagedStatus", Reason: reasonSESTenants},
		{Operation: "UpdateReputationEntityPolicy", Reason: reasonSESTenants},
	},
}
