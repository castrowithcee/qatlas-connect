package gen

import "slices"

// Reasons of the sesv2 operations that stay deferred for a package of their own.
const (
	reasonSESMessageSecurity = "sets the message security options of a configuration set, the signing of sent mail, which is not curated yet and needs its own decision on the effect on sent mail"
	reasonSESPricing         = "changing the pricing attributes of the account waits for a decision on its cost effect"
)

const reasonSESCustomVerification = "custom verification templates only serve SendCustomVerificationEmail, which is excluded, so they have no purpose"

const reasonSESEvent = "an event destination sends the events of a configuration set to SNS, Firehose, EventBridge, or Pinpoint and so carries data out of the account; a check for targets of the bound account is not designed"

// sesv2Table curates the sesv2 model. The identity tools read, create, and configure the sender identities of
// the bound account: DKIM, MAIL FROM, feedback forwarding, the default configuration set, and tags. The
// configuration set tools create and configure configuration sets and manage the suppression list; event
// destinations are read only. The template and contact tools keep email templates, contact lists with topics,
// and their contacts. The send tools send mail with confirmation. The dedicated IP tools read and manage dedicated IP
// addresses and their pools. The tenant tools keep tenants with their resources and suppression attributes, and the
// reputation tools set the status and policy of an entity. The multi-region endpoint tools manage global endpoints
// whose regions are all allowed ones. The job tools start import and export jobs only with a bucket of the bound
// account. The remaining operations wait for the SES tools that follow.
var sesv2Table = Table{
	Service: "sesv2", Segment: "ses", Geltung: Regional,
	Operations: slices.Concat(sesIdentityTools, sesAccountTools, sesConfigurationTools, sesContactTools, sesSendTools, sesDeliverabilityTools, sesDedicatedTools, sesTenantTools, sesMultiRegionTools, sesJobWriteTools),
	Exclude: []Omission{
		{Operation: "CreateConfigurationSetEventDestination", Reason: reasonSESEvent},
		{Operation: "DeleteConfigurationSetEventDestination", Reason: reasonSESEvent},
		{Operation: "UpdateConfigurationSetEventDestination", Reason: reasonSESEvent},
		{Operation: "SendCustomVerificationEmail", Reason: "sends a verification mail to an address given as an argument, so the tool would send mail to any recipient; a tool never sends mail on its own initiative"},
		// The templates of that mail have no use without it.
		{Operation: "CreateCustomVerificationEmailTemplate", Reason: reasonSESCustomVerification},
		{Operation: "DeleteCustomVerificationEmailTemplate", Reason: reasonSESCustomVerification},
		{Operation: "GetCustomVerificationEmailTemplate", Reason: reasonSESCustomVerification},
		{Operation: "ListCustomVerificationEmailTemplates", Reason: reasonSESCustomVerification},
		{Operation: "UpdateCustomVerificationEmailTemplate", Reason: reasonSESCustomVerification},
	},
	Defer: []Omission{
		{Operation: "PutAccountPricingAttributes", Reason: reasonSESPricing},
		{Operation: "UpdateConfigurationSet", Reason: reasonSESMessageSecurity},
	},
}
