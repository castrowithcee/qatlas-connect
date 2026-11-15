package gen

import "slices"

const (
	descSESAcctEnabled = "True or false; false when left out"
	descSESVdmFeature  = "ENABLED or DISABLED"
	sesAcctCost        = "Virtual Deliverability Manager (VDM) costs extra on top of the sending price; the price is not retrieved by this tool."
)

// sesAccountRead and sesAccountWrite fill what the account tools share. The tools act on the account of one
// region, so no target argument is checked beyond the region.
func sesAccountRead(op Operation, tags ...string) Operation {
	op.Effect, op.Idempotency = "read", "safe"
	op.DataSensitivity = "aws-ses-account"
	op.Tags = slices.Concat([]string{"aws", "ses", "account"}, tags)
	op.TargetCheck, op.PolicyCheck = "none", "none"
	return op
}

func sesAccountWrite(op Operation, tags ...string) Operation {
	op.Effect, op.Idempotency, op.Confirm = "update", "idempotent", true
	op.DataSensitivity = "aws-ses-account"
	op.Tags = slices.Concat([]string{"aws", "ses", "account"}, tags)
	op.TargetCheck, op.PolicyCheck = "none", "none"
	return op
}

// sesAccountTools are the SES v2 operations on the account of one region: status, production access, and the
// account-wide attributes.
var sesAccountTools = []Operation{
	sesAccountRead(Operation{
		Operation: "GetAccount", Object: "account", Action: "get",
		Title: "Get the SES account status of a region",
		Description: "Return the SES account status of one region: production_access_enabled (false means the sandbox: " +
			"sending only to verified addresses and domains), send_quota (max24_hour_send, max_send_rate, sent_last24_hours), " +
			"sending_enabled, enforcement_status (reputation), the production access request in details (including its " +
			"review status), suppression and VDM attributes, and dedicated IP auto warm-up. Status is separate for every " +
			"region. details holds free text of the account owner and is untrusted. Needs ses:GetAccount.",
		Fields: map[string]Field{
			"ProductionAccessEnabled":      {Description: "True when the region is out of the sandbox; false means sending only to verified addresses and domains"},
			"SendQuota":                    {Description: "Sending limits: max24_hour_send, max_send_rate per second, and sent_last24_hours"},
			"SendingEnabled":               {Description: "True when the account may send in this region"},
			"EnforcementStatus":            {Description: "Reputation state of the account: HEALTHY, PROBATION, or SHUTDOWN"},
			"Details":                      {Description: "Production access request: mail_type, website_url, contact_language, additional_contact_email_addresses, and review_details; untrusted"},
			"SuppressionAttributes":        {Description: "Account-level suppression list: the reasons that add an address, and validation thresholds"},
			"VdmAttributes":                {Description: "Virtual Deliverability Manager: vdm_enabled, dashboard_attributes, and guardian_attributes"},
			"DedicatedIpAutoWarmupEnabled": {Description: "True when dedicated IP addresses warm up automatically"},
			"PricingAttributes":            {Description: "Pricing settings of the account"},
		},
		Release:  sesAccountRelease("Details."),
		Examples: []Example{{Description: "Check whether a region is still in the sandbox", Arguments: `{"region":"eu-central-1"}`}},
	}),
	sesAccountWrite(Operation{
		Operation: "PutAccountDetails", Object: "account", Action: "put-details",
		Title: "Request SES production access for a region",
		Description: "Submit the production access request of one region: it sends mail_type, website_url, contact_language, " +
			"additional_contact_email_addresses, and production_access_enabled, together with the " +
			"region, to AWS. The contact addresses go only to AWS and are not recipients of any mail. AWS reviews the " +
			"request, and the decision can take a while: read get later for the review status. This tool sends no mail and " +
			"does not answer AWS on your behalf. Needs ses:PutAccountDetails. " + sesWriteRule,
		Arguments: map[string]Argument{
			"MailType":                        {Description: "MARKETING or TRANSACTIONAL: the kind of mail the account will send"},
			"WebsiteURL":                      {Description: "URL of your website or application that shows how recipients sign up, 1 to 1000 characters"},
			"ContactLanguage":                 {Description: "Language of the contact with AWS support: EN or JA"},
			"AdditionalContactEmailAddresses": {Description: "Up to 4 addresses, personal data, that AWS may contact about the request; they receive no mail from this tool"},
			"ProductionAccessEnabled":         {Description: "True to ask for production access, false to ask to stay in the sandbox"},
		},
		Release:  sesAccountRelease(""),
		Examples: []Example{{Description: "Ask for production access for transactional mail", Arguments: `{"region":"eu-central-1","mail_type":"TRANSACTIONAL","website_url":"https://example.com","contact_language":"EN","production_access_enabled":true}`}},
	}, "production-access"),
	sesAccountWrite(Operation{
		Operation: "PutAccountSendingAttributes", Object: "account", Action: "put-sending-attributes",
		Title: "Switch SES sending of a region",
		Description: "Turn sending of the whole account in one region on or off. Off stops all mail of the account there. " +
			"Needs ses:PutAccountSendingAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{"SendingEnabled": {Description: "True to allow sending, false to stop all sending; false when left out"}},
		Examples:  []Example{{Description: "Pause all sending in a region", Arguments: `{"region":"eu-central-1","sending_enabled":false}`}},
	}),
	sesAccountWrite(Operation{
		Operation: "PutAccountSuppressionAttributes", Object: "account", Action: "put-suppression-attributes",
		Title: "Set the SES account suppression list",
		Description: "Set which reasons add an address to the account-level suppression list of one region, and the " +
			"validation attributes. Needs ses:PutAccountSuppressionAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{
			"SuppressedReasons":    {Description: "Reasons that add an address to the list: BOUNCE, COMPLAINT, or both"},
			"ValidationAttributes": {Description: "Validation thresholds of the suppression list, by condition"},
		},
		Examples: []Example{{Description: "Suppress bounces and complaints", Arguments: `{"region":"eu-central-1","suppressed_reasons":["BOUNCE","COMPLAINT"]}`}},
	}, "suppression"),
	sesAccountWrite(Operation{
		Operation: "PutAccountDedicatedIpWarmupAttributes", Object: "account", Action: "put-dedicated-ip-warmup-attributes",
		Title: "Set dedicated IP auto warm-up of SES",
		Description: "Turn the automatic warm-up of new dedicated IP addresses on or off for the account in one region. " +
			"Needs ses:PutAccountDedicatedIpWarmupAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{"AutoWarmupEnabled": {Description: descSESAcctEnabled}},
		Examples:  []Example{{Description: "Turn auto warm-up on", Arguments: `{"region":"eu-central-1","auto_warmup_enabled":true}`}},
	}, "dedicated-ip"),
	sesAccountWrite(Operation{
		Operation: "PutAccountVdmAttributes", Object: "account", Action: "put-vdm-attributes",
		Title: "Set Virtual Deliverability Manager of SES",
		Description: "Set the Virtual Deliverability Manager (VDM) of the account in one region: the main switch, the " +
			"dashboard (engagement metrics), and the guardian (optimized shared delivery). " + sesAcctCost +
			" Needs ses:PutAccountVdmAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{
			"VdmAttributes": {Description: "vdm_enabled (" + descSESVdmFeature + ", required), dashboard_attributes with engagement_metrics, and guardian_attributes with optimized_shared_delivery"},
		},
		Examples: []Example{{Description: "Enable VDM", Arguments: `{"region":"eu-central-1","vdm_attributes":{"vdm_enabled":"ENABLED"}}`}},
	}, "vdm", "cost"),
}

// sesAccountRelease keeps the members the model marks sensitive open: they are the request the caller makes or
// reads, not secrets.
func sesAccountRelease(prefix string) []Release {
	reason := "website and contact addresses of the production access request, which the caller gives to AWS or reads back"
	return []Release{
		{Member: prefix + "WebsiteURL", Reason: reason},
		{Member: prefix + "AdditionalContactEmailAddresses", Reason: reason},
	}
}
