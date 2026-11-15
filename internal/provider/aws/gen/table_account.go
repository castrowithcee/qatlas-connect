package gen

import "slices"

// accountTable curates the account model: regions, contacts, and the name and primary email of the account.
var accountTable = Table{
	Service: "account", Segment: "account", Geltung: Global,
	Operations: slices.Concat(accountRegionTools, accountContactTools),
	Exclude: []Omission{
		{Operation: "StartPrimaryEmailUpdate", Reason: "starts the change of the root user email address of the account, which decides who owns the account; a tool never changes it"},
		{Operation: "AcceptPrimaryEmailUpdate", Reason: "completes the change of the root user email address of the account, which decides who owns the account; a tool never changes it"},
		{Operation: "GetPrimaryEmailUpdateStatus", Reason: "reports on the change of the root user email address, which is not a tool; it has no use without it"},
	},
	Defer: []Omission{
		{Operation: "GetGovCloudAccountInformation", Reason: "reads the GovCloud account linked to the account, which belongs to the aws-us-gov partition; that partition is not supported"},
		{Operation: "SendPhoneNumberVerification", Reason: "starts the verification of a phone number by a call or message to it; no tool offers it yet"},
		{Operation: "VerifyPhoneNumber", Reason: "completes the verification of a phone number with the code that was sent; no tool offers it yet"},
	},
}
