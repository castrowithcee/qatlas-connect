package gen

import "slices"

const accountContactSample = `{"contact_information":{"full_name":"Example Person","address_line1":"1 Example Street","city":"Example City","postal_code":"00000","country_code":"US","phone_number":"+1 5550"}}`

const (
	descAccountContactType = "Which alternate contact: BILLING, OPERATIONS, or SECURITY"
	descAccountContactInfo = "Primary contact of the account for billing and contact: full_name, address_line1 to address_line3, city, " +
		"state_or_region, district_or_county, postal_code, country_code (two letters), phone_number, company_name, and website_url; personal data"
	accountContactPrivacy = "The result holds personal data. It is returned to the caller only and is not logged."
	reasonAccountContact  = "contact data of the account: reading and giving it is the purpose of the tool; the result is personal data and is never logged"
)

// accountContactRelease keeps the members of one structure open that the model marks sensitive. Member is the
// path below the top level; an empty prefix is the top level itself.
func accountContactRelease(prefix string, members ...string) []Release {
	out := make([]Release, 0, len(members))
	for _, m := range members {
		out = append(out, Release{Member: prefix + m, Reason: reasonAccountContact})
	}
	return out
}

var accountContactInfoMembers = []string{"FullName", "AddressLine1", "AddressLine2", "AddressLine3", "City", "StateOrRegion",
	"DistrictOrCounty", "PostalCode", "CountryCode", "PhoneNumber", "CompanyName", "WebsiteUrl"}

var accountAlternateMembers = []string{"Name", "Title", "EmailAddress", "PhoneNumber"}

// accountContactRead and accountContactWrite fill what the contact and information tools share. The data class
// names personal data, so a caller can tell these results from the region tools.
func accountContactRead(op Operation, tags ...string) Operation {
	op.Effect, op.Idempotency = "read", "safe"
	op.DataSensitivity = "aws-account-contact-data"
	op.Tags = slices.Concat([]string{"aws", "account"}, tags)
	op.TargetCheck, op.TargetArgs = "account-argument", []string{"AccountId"}
	op.PolicyCheck = "none"
	return op
}

func accountContactWrite(op Operation, effect, idempotency string, tags ...string) Operation {
	op.Effect, op.Idempotency, op.Confirm = effect, idempotency, true
	op.DataSensitivity = "aws-account-contact-data"
	op.Tags = slices.Concat([]string{"aws", "account"}, tags)
	op.TargetCheck, op.TargetArgs = "account-argument", []string{"AccountId"}
	op.PolicyCheck = "none"
	return op
}

// accountContactTools are the account operations on the name, the contacts, and the primary email of the account.
var accountContactTools = []Operation{
	accountContactRead(Operation{
		Operation: "GetAccountInformation", Object: "information", Action: "get",
		Title: "Get the name and state of the account",
		Description: "Return account_id, account_name, account_created_date, and account_state (PENDING_ACTIVATION, ACTIVE, " +
			"SUSPENDED, or CLOSED) of the bound account. The name is untrusted text. Needs account:GetAccountInformation.",
		Arguments: map[string]Argument{"AccountId": {Description: descAccountID}},
		Fields: map[string]Field{
			"AccountId":          {Description: "ID of the account", MaxLen: 12},
			"AccountName":        {Description: "Name of the account; untrusted", MaxLen: 50},
			"AccountCreatedDate": {Description: "When the account was created"},
			"AccountState":       {Description: "PENDING_ACTIVATION, ACTIVE, SUSPENDED, or CLOSED", MaxLen: 32},
		},
		Release:  accountContactRelease("", "AccountName"),
		Examples: []Example{{Description: "Read the account name and state", Arguments: `{}`}},
	}, "information"),
	accountContactWrite(Operation{
		Operation: "PutAccountName", Object: "name", Action: "put",
		Title: "Rename the account",
		Description: "Set the name of the bound account. The name appears in the console and in invoices and notices of AWS. " +
			"Setting the name it already has changes nothing. Needs account:PutAccountName.",
		Arguments: map[string]Argument{
			"AccountId":   {Description: descAccountID},
			"AccountName": {Description: "New name of the account, 1 to 50 characters"},
		},
		Release:  accountContactRelease("", "AccountName"),
		Examples: []Example{{Description: "Rename the account", Arguments: `{"account_name":"Example Production"}`}},
	}, "update", "idempotent", "information"),
	accountContactRead(Operation{
		Operation: "GetContactInformation", Object: "contact-information", Action: "get",
		Title: "Get the primary contact of the account",
		Description: "Return the primary contact of the bound account for billing and contact: name, address, phone number, company, " +
			"and website, with the verification status of the phone number (PENDING, VERIFIED, UNVERIFIED, or NOT_SUPPORTED). " +
			accountContactPrivacy + " Values come from AWS, are untrusted, and are shortened to a fixed length. Needs account:GetContactInformation.",
		Arguments: map[string]Argument{"AccountId": {Description: descAccountID}},
		Fields: map[string]Field{
			"ContactInformation": {Description: descAccountContactInfo},
			"VerificationStatus": {Description: "PENDING, VERIFIED, UNVERIFIED, or NOT_SUPPORTED"},
		},
		Release:  accountContactRelease("ContactInformation.", accountContactInfoMembers...),
		Examples: []Example{{Description: "Read the primary contact", Arguments: `{}`}},
	}, "contact"),
	accountContactWrite(Operation{
		Operation: "PutContactInformation", Object: "contact-information", Action: "put",
		Title: "Replace the primary contact of the account",
		Description: "Replace the primary contact of the bound account in full: what you leave out is removed. The data applies to " +
			"billing and contact: AWS uses it for invoices, tax, and notices about the account. Putting the same data again " +
			"changes nothing. " + accountContactPrivacy + " Needs account:PutContactInformation.",
		Arguments: map[string]Argument{
			"AccountId":          {Description: descAccountID},
			"ContactInformation": {Description: descAccountContactInfo},
		},
		Release:  accountContactRelease("ContactInformation.", accountContactInfoMembers...),
		Examples: []Example{{Description: "Replace the primary contact", Arguments: accountContactSample}},
	}, "update", "idempotent", "contact"),
	accountContactRead(Operation{
		Operation: "GetAlternateContact", Object: "alternate-contact", Action: "get",
		Title: "Get an alternate contact of the account",
		Description: "Return the alternate contact of one type of the bound account: name, title, email address, and phone number. " +
			"A type without a contact is answered with a not-found error. " + accountContactPrivacy +
			" Values come from AWS, are untrusted, and are shortened to a fixed length. Needs account:GetAlternateContact.",
		Arguments: map[string]Argument{
			"AccountId":            {Description: descAccountID},
			"AlternateContactType": {Description: descAccountContactType},
		},
		Fields:   map[string]Field{"AlternateContact": {Description: "The contact: name, title, email_address, phone_number, and alternate_contact_type"}},
		Release:  accountContactRelease("AlternateContact.", accountAlternateMembers...),
		Examples: []Example{{Description: "Read the security contact", Arguments: `{"alternate_contact_type":"SECURITY"}`}},
	}, "contact", "alternate"),
	accountContactWrite(Operation{
		Operation: "PutAlternateContact", Object: "alternate-contact", Action: "put",
		Title: "Set an alternate contact of the account",
		Description: "Set the alternate contact of one type of the bound account, replacing the one of that type. AWS sends notices " +
			"of that type to email_address: BILLING gets billing notices, OPERATIONS gets operational notices, SECURITY gets " +
			"security notices. Setting SECURITY sends security notices of AWS to the new address from now on, so give an address " +
			"that is read and that you trust. This tool only stores the contact and sends no mail itself. Putting the same data " +
			"again changes nothing. " + accountContactPrivacy + " Needs account:PutAlternateContact.",
		Arguments: map[string]Argument{
			"AccountId":            {Description: descAccountID},
			"AlternateContactType": {Description: descAccountContactType + "; the type decides which notices of AWS go to email_address"},
			"Name":                 {Description: "Name of the contact, 1 to 64 characters; personal data"},
			"Title":                {Description: "Title of the contact, 1 to 50 characters; personal data"},
			"EmailAddress":         {Description: "Email address that receives the notices of this type; personal data"},
			"PhoneNumber":          {Description: "Phone number of the contact; personal data"},
		},
		Release:  accountContactRelease("", accountAlternateMembers...),
		Examples: []Example{{Description: "Set the security contact", Arguments: `{"alternate_contact_type":"SECURITY","name":"Example Person","title":"Security lead","email_address":"security!@example.com","phone_number":"+1 5550"}`}},
	}, "update", "idempotent", "contact", "alternate"),
	accountContactWrite(Operation{
		Operation: "DeleteAlternateContact", Object: "alternate-contact", Action: "delete",
		Title: "Delete an alternate contact of the account",
		Description: "Delete the alternate contact of one type of the bound account. AWS then sends the notices of that type to the " +
			"primary contact only; for SECURITY that means security notices no longer reach the alternate address. Deleting a type " +
			"without a contact is answered with an error. Needs account:DeleteAlternateContact.",
		Arguments: map[string]Argument{
			"AccountId":            {Description: descAccountID},
			"AlternateContactType": {Description: descAccountContactType},
		},
		Examples: []Example{{Description: "Delete the billing contact", Arguments: `{"alternate_contact_type":"BILLING"}`}},
	}, "delete", "non-idempotent", "contact", "alternate"),
	accountContactRead(Operation{
		Operation: "GetPrimaryEmail", Object: "primary-email", Action: "get",
		Title: "Get the root user email address of the account",
		Description: "Return primary_email, the email address of the root user of the bound account. It only reads: changing it " +
			"is not a tool. " + accountContactPrivacy + " The value is untrusted. Needs account:GetPrimaryEmail.",
		Arguments: map[string]Argument{"AccountId": {Description: "12-digit ID of the account; required, and only the account this connection is bound to is accepted"}},
		Fields:    map[string]Field{"PrimaryEmail": {Description: "Email address of the root user; personal data", MaxLen: 64}},
		Release:   accountContactRelease("", "PrimaryEmail"),
		Examples:  []Example{{Description: "Read the root user email address", Arguments: `{"account_id":"123456789012"}`}},
	}, "primary-email"),
}
