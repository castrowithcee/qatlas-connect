package gen

import "slices"

const (
	sesWriteRule = "Needs confirmation."

	descSESIdentity = "Identity: a domain such as example.com, or an email address; letters, digits, dots, hyphens, and for an address also +_ in the local part"
	descSESARN      = "ARN of an identity in the bound account and an allowed region, such as arn:aws:ses:REGION:ACCOUNT:identity/example.com"
	descSESTags     = "Tags, up to 50, each with key (1 to 128 characters) and value (0 to 256 characters)"
	descSESConfig   = "Name of a configuration set of the bound account"
	descSESKeyLen   = "Easy DKIM key length of the next key: RSA_1024_BIT or RSA_2048_BIT"
	descSESOrigin   = "Where the signing key comes from: AWS_SES, or an AWS_SES_<REGION> value to use the Easy DKIM keys of that region; " +
		"EXTERNAL (your own key) is not offered"
	descSESDkimSign = "Easy DKIM settings; your own key (selector and private key) is not offered"
	descSESDKIMAttr = "DKIM state: signing switch, status, the tokens for the CNAME records, key lengths, and where the key comes from"

	reasonSESKey = "a private key never passes through a tool, and with the key the selector has no use"
)

// sesIdentityRead and sesIdentityWrite fill what the identity tools share.
func sesIdentityRead(op Operation, tags ...string) Operation {
	op.Effect, op.Idempotency = "read", "safe"
	op.DataSensitivity = "aws-ses-sender-identity"
	op.Tags = slices.Concat([]string{"aws", "ses", "identity"}, tags)
	if op.TargetCheck == "" {
		op.TargetCheck, op.TargetArgs = "ses-identity", []string{"EmailIdentity"}
	}
	op.PolicyCheck = "none"
	return op
}

func sesIdentityWrite(op Operation, effect, idempotency string, tags ...string) Operation {
	op.Effect, op.Idempotency, op.Confirm = effect, idempotency, true
	op.DataSensitivity = "aws-ses-sender-identity"
	op.Tags = slices.Concat([]string{"aws", "ses", "identity"}, tags)
	if op.TargetCheck == "" {
		op.TargetCheck, op.TargetArgs = "ses-identity", []string{"EmailIdentity"}
	}
	op.PolicyCheck = "none"
	return op
}

// sesIdentityTools are the SES v2 operations on sender identities.
var sesIdentityTools = []Operation{
	sesIdentityRead(Operation{
		Operation: "ListEmailIdentities", Object: "email-identity", Action: "list",
		Title: "List SES sender identities",
		Description: "List the sender identities (domains and email addresses) of the bound account in one region, with " +
			"type, sending status, and verification status. Needs ses:ListEmailIdentities.",
		Arguments: map[string]Argument{
			"Filter": {Description: "Filter as a map from IDENTITY_NAME_CONTAINS, IDENTITY_TYPE, or VERIFICATION_STATUS to a value"},
		},
		Fields:      map[string]Field{"EmailIdentities": {Description: "Identities of this page, each with identity_type, identity_name, sending_enabled, and verification_status"}},
		PageSizeMax: 1000,
		TargetCheck: "none",
		Examples:    []Example{{Description: "List the domains that are verified", Arguments: `{"filter":{"IDENTITY_TYPE":"DOMAIN","VERIFICATION_STATUS":"SUCCESS"}}`}},
	}),
	sesIdentityRead(Operation{
		Operation: "GetEmailIdentity", Object: "email-identity", Action: "get",
		Title: "Get an SES sender identity",
		Description: "Return one sender identity: verification and sending status, DKIM (tokens and status), MAIL FROM, " +
			"feedback forwarding, default configuration set, identity policies, and tags. dns_records lists the DNS " +
			"records to create, derived by this tool. Needs ses:GetEmailIdentity.",
		Arguments: map[string]Argument{"EmailIdentity": {Description: descSESIdentity}},
		Fields: map[string]Field{
			"IdentityType":             {Description: "EMAIL_ADDRESS, DOMAIN, or MANAGED_DOMAIN"},
			"FeedbackForwardingStatus": {Description: "True when bounce and complaint notifications are forwarded by email"},
			"VerifiedForSendingStatus": {Description: "True when the identity may send"},
			"DkimAttributes":           {Description: descSESDKIMAttr},
			"MailFromAttributes":       {Description: "MAIL FROM domain, its status, and what happens when its MX record is missing"},
			"Policies":                 {Description: "Identity policies by name, as JSON text; untrusted"},
			"Tags":                     {Description: "Tags of the identity"},
			"ConfigurationSetName":     {Description: "Default configuration set of the identity"},
			"VerificationStatus":       {Description: "PENDING, SUCCESS, FAILED, TEMPORARY_FAILURE, or NOT_STARTED"},
			"VerificationInfo":         {Description: "Last verification check, last success, error type, and the SOA record found"},
		},
		ResultHook: "dns-records",
		Examples:   []Example{{Description: "Show the state and the DNS records of a domain", Arguments: `{"email_identity":"example.com"}`}},
	}),
	sesIdentityWrite(Operation{
		Operation: "CreateEmailIdentity", Object: "email-identity", Action: "create",
		Title: "Create an SES sender identity",
		Description: "Create a sender identity, a domain or an email address, in the bound account, with optional tags, " +
			"default configuration set, and Easy DKIM key length. Returns the status and the DKIM tokens; dns_records " +
			"lists the CNAME records to create, derived by this tool. A domain sends only after its records verify, and " +
			"an account without production access sends only to verified recipients. Needs ses:CreateEmailIdentity, " +
			"and ses:TagResource with tags. Not repeated after an unclear outcome: read the identity before trying " +
			"again. " + sesWriteRule,
		Arguments: map[string]Argument{
			"EmailIdentity":         {Description: descSESIdentity},
			"Tags":                  {Description: descSESTags},
			"DkimSigningAttributes": {Description: descSESDkimSign},
			"ConfigurationSetName":  {Description: descSESConfig},
		},
		Fields: map[string]Field{
			"IdentityType":             {Description: "EMAIL_ADDRESS, DOMAIN, or MANAGED_DOMAIN"},
			"VerifiedForSendingStatus": {Description: "True when the identity may send"},
			"DkimAttributes":           {Description: descSESDKIMAttr},
		},
		HiddenInputs: []Hidden{
			{Member: "DkimSigningAttributes.DomainSigningSelector", Reason: reasonSESKey},
			{Member: "DkimSigningAttributes.DomainSigningPrivateKey", Reason: reasonSESKey},
		},
		ExcludeEnum: []EnumExclusion{{Member: "DkimSigningAttributes.DomainSigningAttributesOrigin", Values: []string{"EXTERNAL"},
			Reason: "EXTERNAL means a key of your own, whose private key a tool would have to take"}},
		ResultHook: "dns-records",
		Examples:   []Example{{Description: "Create a domain identity", Arguments: `{"email_identity":"example.com"}`}},
	}, "create", "non-idempotent"),
	sesIdentityWrite(Operation{
		Operation: "DeleteEmailIdentity", Object: "email-identity", Action: "delete",
		Title: "Delete an SES sender identity",
		Description: "Delete a sender identity of the bound account; it can no longer send. Needs ses:DeleteEmailIdentity. " +
			"Not repeated after an unclear outcome. " + sesWriteRule,
		Arguments: map[string]Argument{"EmailIdentity": {Description: descSESIdentity}},
		Examples:  []Example{{Description: "Delete a domain identity", Arguments: `{"email_identity":"example.com"}`}},
	}, "delete", "non-idempotent"),
	sesIdentityWrite(Operation{
		Operation: "PutEmailIdentityDkimAttributes", Object: "email-identity", Action: "put-dkim-attributes",
		Title: "Switch DKIM signing of an SES identity",
		Description: "Turn DKIM signing of a sender identity on or off. Needs ses:PutEmailIdentityDkimAttributes. " +
			sesWriteRule,
		Arguments: map[string]Argument{
			"EmailIdentity":  {Description: descSESIdentity},
			"SigningEnabled": {Description: "True to sign with DKIM, false to stop; false when left out"},
		},
		Examples: []Example{{Description: "Turn signing on", Arguments: `{"email_identity":"example.com","signing_enabled":true}`}},
	}, "update", "idempotent"),
	sesIdentityWrite(Operation{
		Operation: "PutEmailIdentityDkimSigningAttributes", Object: "email-identity", Action: "put-dkim-signing-attributes",
		Title: "Set the Easy DKIM key of an SES identity",
		Description: "Set the Easy DKIM key length of a sender identity, or where its signing key comes from, and return " +
			"the DKIM status and tokens. Your own key (selector and private key) is not offered. Needs " +
			"ses:PutEmailIdentityDkimSigningAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{
			"EmailIdentity":           {Description: descSESIdentity},
			"SigningAttributesOrigin": {Description: descSESOrigin},
			"SigningAttributes":       {Description: descSESDkimSign},
		},
		Fields: map[string]Field{
			"DkimStatus":        {Description: "PENDING, SUCCESS, FAILED, TEMPORARY_FAILURE, or NOT_STARTED"},
			"DkimTokens":        {Description: "Tokens for the DKIM CNAME records"},
			"SigningHostedZone": {Description: "DNS zone the DKIM CNAME values point into; differs by region and identity"},
		},
		HiddenInputs: []Hidden{
			{Member: "SigningAttributes.DomainSigningSelector", Reason: reasonSESKey},
			{Member: "SigningAttributes.DomainSigningPrivateKey", Reason: reasonSESKey},
		},
		ExcludeEnum: []EnumExclusion{
			{Member: "SigningAttributesOrigin", Values: []string{"EXTERNAL"}, Reason: "EXTERNAL means a key of your own, whose private key a tool would have to take"},
			{Member: "SigningAttributes.DomainSigningAttributesOrigin", Values: []string{"EXTERNAL"}, Reason: "EXTERNAL means a key of your own, whose private key a tool would have to take"},
		},
		Examples: []Example{{Description: "Use a 2048-bit Easy DKIM key", Arguments: `{"email_identity":"example.com","signing_attributes_origin":"AWS_SES","signing_attributes":{"next_signing_key_length":"RSA_2048_BIT"}}`}},
	}, "update", "idempotent"),
	sesIdentityWrite(Operation{
		Operation: "PutEmailIdentityMailFromAttributes", Object: "email-identity", Action: "put-mail-from-attributes",
		Title: "Set the MAIL FROM domain of an SES identity",
		Description: "Set the MAIL FROM domain of a sender identity, a subdomain of it, and what happens when its MX record " +
			"is missing. get returns the records to create. Needs ses:PutEmailIdentityMailFromAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{
			"EmailIdentity":       {Description: descSESIdentity},
			"MailFromDomain":      {Description: "MAIL FROM domain, a subdomain of the identity; leave out to remove it"},
			"BehaviorOnMxFailure": {Description: "USE_DEFAULT_VALUE sends from amazonses.com when the MX record is missing, REJECT_MESSAGE rejects the message; USE_DEFAULT_VALUE when left out"},
		},
		Examples: []Example{{Description: "Use a MAIL FROM subdomain", Arguments: `{"email_identity":"example.com","mail_from_domain":"mail.example.com","behavior_on_mx_failure":"REJECT_MESSAGE"}`}},
	}, "update", "idempotent"),
	sesIdentityWrite(Operation{
		Operation: "PutEmailIdentityFeedbackAttributes", Object: "email-identity", Action: "put-feedback-attributes",
		Title: "Set feedback forwarding of an SES identity",
		Description: "Turn the forwarding of bounce and complaint notifications by email on or off for a sender identity. " +
			"Needs ses:PutEmailIdentityFeedbackAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{
			"EmailIdentity":          {Description: descSESIdentity},
			"EmailForwardingEnabled": {Description: "True to forward notifications by email; false when left out"},
		},
		Examples: []Example{{Description: "Stop forwarding by email", Arguments: `{"email_identity":"example.com","email_forwarding_enabled":false}`}},
	}, "update", "idempotent"),
	sesIdentityWrite(Operation{
		Operation: "PutEmailIdentityConfigurationSetAttributes", Object: "email-identity", Action: "put-configuration-set-attributes",
		Title: "Set the default configuration set of an SES identity",
		Description: "Set the default configuration set of a sender identity; leave the name out to remove it. Needs " +
			"ses:PutEmailIdentityConfigurationSetAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{
			"EmailIdentity":        {Description: descSESIdentity},
			"ConfigurationSetName": {Description: descSESConfig},
		},
		Examples: []Example{{Description: "Use a default configuration set", Arguments: `{"email_identity":"example.com","configuration_set_name":"default"}`}},
	}, "update", "idempotent"),
	sesIdentityWrite(Operation{
		Operation: "TagResource", Object: "email-identity", Action: "tag",
		Title:       "Tag an SES sender identity",
		Description: "Add or replace tags on a sender identity. Needs ses:TagResource. " + sesWriteRule,
		Arguments: map[string]Argument{
			"ResourceArn": {Description: descSESARN},
			"Tags":        {Description: descSESTags},
		},
		TargetCheck: "ses-identity-arn", TargetArgs: []string{"ResourceArn"},
		Examples: []Example{{Description: "Tag an identity", Arguments: `{"resource_arn":"arn:aws:ses:eu-central-1:123456789012:identity/example.com","tags":[{"key":"team","value":"mail"}]}`}},
	}, "update", "idempotent"),
	sesIdentityWrite(Operation{
		Operation: "UntagResource", Object: "email-identity", Action: "untag",
		Title:       "Remove tags from an SES sender identity",
		Description: "Remove tags by key from a sender identity. Needs ses:UntagResource. " + sesWriteRule,
		Arguments: map[string]Argument{
			"ResourceArn": {Description: descSESARN},
			"TagKeys":     {Description: "Keys of the tags to remove"},
		},
		TargetCheck: "ses-identity-arn", TargetArgs: []string{"ResourceArn"},
		Examples: []Example{{Description: "Remove a tag", Arguments: `{"resource_arn":"arn:aws:ses:eu-central-1:123456789012:identity/example.com","tag_keys":["team"]}`}},
	}, "update", "idempotent"),
	sesIdentityRead(Operation{
		Operation: "ListTagsForResource", Object: "email-identity", Action: "list-tags",
		Title:       "List the tags of an SES sender identity",
		Description: "List the tags of a sender identity. Needs ses:ListTagsForResource.",
		Arguments:   map[string]Argument{"ResourceArn": {Description: descSESARN}},
		Fields:      map[string]Field{"Tags": {Description: "Tags of the identity", Required: true}},
		TargetCheck: "ses-identity-arn", TargetArgs: []string{"ResourceArn"},
		Examples: []Example{{Description: "List the tags", Arguments: `{"resource_arn":"arn:aws:ses:eu-central-1:123456789012:identity/example.com"}`}},
	}),
	sesIdentityRead(Operation{
		Operation: "GetEmailIdentityPolicies", Object: "email-identity-policy", Action: "list",
		Title:       "List the policies of an SES sender identity",
		Description: "Return the identity policies of a sender identity by name, as JSON text. Needs ses:GetEmailIdentityPolicies.",
		Arguments:   map[string]Argument{"EmailIdentity": {Description: descSESIdentity}},
		Fields:      map[string]Field{"Policies": {Description: "Identity policies by name, as JSON text; untrusted"}},
		Examples:    []Example{{Description: "Show the policies of a domain", Arguments: `{"email_identity":"example.com"}`}},
	}),
}
