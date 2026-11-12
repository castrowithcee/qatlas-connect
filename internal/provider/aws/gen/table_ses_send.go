package gen

import "slices"

const (
	// sesSendRule is the part of the description that stands for the confirmation: what leaves the account,
	// what it costs, the sandbox, and that nothing is repeated. The core asks only for confirm, so the
	// description is where the caller learns what the confirmation covers.
	sesSendRule = "What is sent: the region, the sender, the recipients (To, Cc, and Bcc), the subject or the template, the " +
		"configuration set, and the content go to AWS and from there to the recipients and their mail providers, third " +
		"parties who cannot be asked to give the mail back. Costs: 0.10 USD per 1,000 mails, each recipient counted, plus " +
		"0.12 USD per GB of attachments, according to the AWS price list. An account without production access (the " +
		"sandbox) sends only to verified recipients. At most 50 recipients (To, Cc, and Bcc together) per call; more " +
		"recipients take more calls, each confirmed on its own. Calls of one connection and region start at least one " +
		"second apart. Never repeated automatically, not even after a timeout or a server error: such a failure says " +
		"that the mail may have gone out, so check before sending again. Raw MIME content is not offered. Needs confirmation."

	descSESAddress    = "Address, either local@domain or Display Name <local@domain>; a display name of other than printable ASCII must be MIME encoded-word"
	descSESFrom       = "Sender address of a verified identity of the bound account. " + descSESAddress
	descSESFromARN    = "Only for sending authorization: ARN of the identity of the sender, in the bound account and an allowed region, such as arn:aws:ses:REGION:ACCOUNT:identity/example.com"
	descSESReplyTo    = "Reply-To addresses. " + descSESAddress
	descSESFeedback   = "Address that receives bounce and complaint notifications, local@domain without a display name"
	descSESFeedbackID = "Only for sending authorization: ARN of the identity of the feedback address, in the bound account and an allowed region"
	descSESDest       = "Recipients: to_addresses, cc_addresses, and bcc_addresses, 50 together at most. " + descSESAddress
	descSESMsgTags    = "Message tags for event publishing, each with name and value"
	descSESConfigSet  = "Name of a configuration set of the bound account to send with"
	descSESTenant     = "Name of a tenant of the bound account to send through; every identity, configuration set, and template named must belong to it"
	descSESOverrides  = "Overrides of open and click tracking for this call only: ENABLED or DISABLED"
	descSESTemplate   = "template_name or template_arn of a stored template (the ARN in the bound account and an allowed region, " +
		"such as arn:aws:ses:REGION:ACCOUNT:template/NAME), or template_content with subject, text, and html; template_data as JSON " +
		"text, headers, and attachments"

	sesMessageID = "Message ID SES gave the mail"

	reasonSESRaw      = "raw MIME content carries headers and parts of its own that no check of the tool sees; the simple and template content are the structured way to send"
	reasonSESEndpoint = "a multi-region endpoint has its own host outside the endpoint table and needs a SigV4a signature; the tool sends only to the regional endpoint of the call"

	// sesRecipientCap is the SES limit of 50 recipients per message, To, Cc, and Bcc together (SES developer
	// guide, Service quotas: "Maximum number of recipients per message").
	sesRecipientCap    = 50
	reasonSESRecipient = "SES takes at most 50 recipients per message, To, Cc, and Bcc together"
)

// sesSend fills what the send tools share.
func sesSend(op Operation) Operation {
	op.Object, op.Effect, op.Idempotency, op.Confirm = "email", "execute", "non-idempotent", true
	op.DataSensitivity = "aws-ses-mail"
	op.Tags = []string{"aws", "ses", "mail"}
	op.TargetCheck, op.TargetArgs = "ses-identity-arn", []string{"FromEmailAddressIdentityArn", "FeedbackForwardingEmailAddressIdentityArn"}
	op.PolicyCheck = "none"
	op.Guards = []string{"ses-send"}
	op.Delivery = "ses-mail"
	return op
}

// sesDestinationLimits caps each recipient list of a destination at the per-message limit; the guard checks
// the sum.
func sesDestinationLimits(prefix string) []InputLimit {
	var out []InputLimit
	for _, list := range []string{"ToAddresses", "CcAddresses", "BccAddresses"} {
		out = append(out, InputLimit{Member: prefix + "Destination." + list, MaxItems: sesRecipientCap, Reason: reasonSESRecipient})
	}
	return out
}

// sesSendArguments are the arguments both send tools take.
var sesSendArguments = map[string]Argument{
	"FromEmailAddress":                          {Description: descSESFrom},
	"FromEmailAddressIdentityArn":               {Description: descSESFromARN},
	"ReplyToAddresses":                          {Description: descSESReplyTo},
	"FeedbackForwardingEmailAddress":            {Description: descSESFeedback},
	"FeedbackForwardingEmailAddressIdentityArn": {Description: descSESFeedbackID},
	"ConfigurationSetName":                      {Description: descSESConfigSet},
	"TenantName":                                {Description: descSESTenant},
	"ConfigurationOverrides":                    {Description: descSESOverrides},
}

func withArguments(extra map[string]Argument) map[string]Argument {
	out := map[string]Argument{}
	for k, v := range sesSendArguments {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// The sample addresses of the generated tests end their local part in "!", a character RFC 5322 allows, so the
// generated files hold no text in the form of a mail address.
const sesSendSample = `"from_email_address":"sender!@example.com","reply_to_addresses":["reply!@example.com"],` +
	`"feedback_forwarding_email_address":"bounce!@example.com","from_email_address_identity_arn":"arn:aws:ses:eu-central-1:123456789012:identity/example.com",` +
	`"feedback_forwarding_email_address_identity_arn":"arn:aws:ses:eu-central-1:123456789012:identity/example.com"`

const sesSampleTemplate = `{"template_name":"a","template_arn":"arn:aws:ses:eu-central-1:123456789012:template/a","template_content":{"subject":"a","text":"a","html":"a"},` +
	`"template_data":"{}","headers":[{"name":"X-Tag","value":"a"}],"attachments":[{"raw_content":"AA==","content_disposition":"ATTACHMENT","file_name":"a",` +
	`"content_description":"a","content_id":"a","content_transfer_encoding":"BASE64","content_type":"text/plain"}]}`

// sesSendTools are the SES v2 operations that send mail.
var sesSendTools = []Operation{
	sesSend(Operation{
		Operation: "SendEmail", Action: "send",
		Title: "Send a mail with SES",
		Description: "Send one mail through SES v2 from a sender identity of the bound account, with simple content (subject, " +
			"text, HTML, headers, and attachments) or a template (stored, by name or ARN, or given inline, with template data), " +
			"optionally to a contact list topic for unsubscribe handling. Returns the message ID. Needs ses:SendEmail. " + sesSendRule,
		Arguments: withArguments(map[string]Argument{
			"Destination": {Description: descSESDest},
			"Content": {Description: "Content, exactly one of simple (subject and body with text or html, each with data and " +
				"charset, plus headers and attachments) or template (" + descSESTemplate + ")"},
			"EmailTags":             {Description: descSESMsgTags},
			"ListManagementOptions": {Description: "Contact list (contact_list_name) and topic (topic_name) the mail belongs to, for unsubscribe handling"},
		}),
		Fields: map[string]Field{"MessageId": {Description: sesMessageID}},
		HiddenInputs: []Hidden{
			{Member: "EndpointId", Reason: reasonSESEndpoint},
			{Member: "Content.Raw", Reason: reasonSESRaw},
		},
		Limits: sesDestinationLimits(""),
		SampleArgs: `{` + sesSendSample + `,"destination":{"to_addresses":["to!@example.com"],"cc_addresses":["cc!@example.com"],"bcc_addresses":["bcc!@example.com"]},` +
			`"content":{"simple":{"subject":{"data":"a","charset":"UTF-8"},"body":{"text":{"data":"a","charset":"UTF-8"},"html":{"data":"a","charset":"UTF-8"}},` +
			`"headers":[{"name":"X-Tag","value":"a"}],"attachments":[{"raw_content":"AA==","content_disposition":"ATTACHMENT","file_name":"a",` +
			`"content_description":"a","content_id":"a","content_transfer_encoding":"BASE64","content_type":"text/plain"}]},"template":` + sesSampleTemplate + `}}`,
		Examples: []Example{
			{Description: "Send a plain text mail; SENDER and RECIPIENT stand for addresses",
				Arguments: `{"from_email_address":"SENDER","destination":{"to_addresses":["RECIPIENT"]},"content":{"simple":{"subject":{"data":"Report"},"body":{"text":{"data":"The report is ready."}}}}}`},
			{Description: "Send with a stored template; SENDER and RECIPIENT stand for addresses",
				Arguments: `{"from_email_address":"SENDER","destination":{"to_addresses":["RECIPIENT"]},"content":{"template":{"template_name":"welcome","template_data":"{\"name\":\"Alex\"}"}}}`},
		},
	}),
	sesSend(Operation{
		Operation: "SendBulkEmail", Action: "send-bulk",
		Title: "Send a templated mail to several destinations with SES",
		Description: "Send one template through SES v2 from a sender identity of the bound account to several destinations, " +
			"each its own mail with its own recipients, template data, headers, and tags. Returns status, error, and message ID " +
			"per destination in request order; a destination can fail while others succeed. The 50 recipients count across " +
			"all destinations of the call. Needs ses:SendBulkEmail. " + sesSendRule,
		Arguments: withArguments(map[string]Argument{
			"DefaultContent":   {Description: "Template of every destination: " + descSESTemplate},
			"BulkEmailEntries": {Description: "Destinations, each with destination (recipients as in send), replacement_email_content.replacement_template.replacement_template_data, replacement_headers, and replacement_tags"},
			"DefaultEmailTags": {Description: descSESMsgTags + ", for every destination"},
		}),
		Fields: map[string]Field{"BulkEmailEntryResults": {Description: "One entry per destination in request order: status " +
			"(SUCCESS or the reason of the failure, such as MESSAGE_REJECTED), error text of AWS (untrusted), and message_id", Required: true}},
		HiddenInputs: []Hidden{{Member: "EndpointId", Reason: reasonSESEndpoint}},
		Limits: slices.Concat(sesDestinationLimits("BulkEmailEntries."), []InputLimit{
			{Member: "BulkEmailEntries", MaxItems: sesRecipientCap, Reason: "every destination has at least one recipient, and SES takes at most 50 recipients per call"},
		}),
		SampleArgs: `{` + sesSendSample + `,"default_content":{"template":` + sesSampleTemplate + `},` +
			`"bulk_email_entries":[{"destination":{"to_addresses":["to!@example.com"],"cc_addresses":["cc!@example.com"],"bcc_addresses":["bcc!@example.com"]},` +
			`"replacement_tags":[{"name":"a","value":"a"}],"replacement_email_content":{"replacement_template":{"replacement_template_data":"{}"}},` +
			`"replacement_headers":[{"name":"X-Tag","value":"a"}]}]}`,
		Examples: []Example{{Description: "Send a stored template to two destinations; SENDER, FIRST, and SECOND stand for addresses",
			Arguments: `{"from_email_address":"SENDER","default_content":{"template":{"template_name":"welcome","template_data":"{}"}},` +
				`"bulk_email_entries":[{"destination":{"to_addresses":["FIRST"]},"replacement_email_content":{"replacement_template":{"replacement_template_data":"{\"name\":\"Alex\"}"}}},` +
				`{"destination":{"to_addresses":["SECOND"]}}]}`}},
	}),
}
