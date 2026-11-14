package gen

// deferAccount is the reason for every account operation: the table is a raw scaffold, and the service package that
// curates the tools moves each operation it takes into operations or exclude.
const deferAccount = "not curated yet; the Account service package decides effect, confirmation and account target first"

// accountTable curates the account model. The region tools are curated; the remaining operations wait for the account tools that follow.
var accountTable = Table{
	Service: "account", Segment: "account", Geltung: Global,
	Operations: accountRegionTools,
	Exclude: []Omission{
		{Operation: "StartPrimaryEmailUpdate", Reason: "starts the change of the root user email address of the account, which decides who owns the account; a tool never changes it"},
		{Operation: "AcceptPrimaryEmailUpdate", Reason: "completes the change of the root user email address of the account, which decides who owns the account; a tool never changes it"},
	},
	Defer: []Omission{
		{Operation: "DeleteAlternateContact", Reason: deferAccount},
		{Operation: "GetAccountInformation", Reason: deferAccount},
		{Operation: "GetAlternateContact", Reason: deferAccount},
		{Operation: "GetContactInformation", Reason: deferAccount},
		{Operation: "GetGovCloudAccountInformation", Reason: deferAccount},
		{Operation: "GetPrimaryEmail", Reason: deferAccount},
		{Operation: "GetPrimaryEmailUpdateStatus", Reason: deferAccount},
		{Operation: "PutAccountName", Reason: deferAccount},
		{Operation: "PutAlternateContact", Reason: deferAccount},
		{Operation: "PutContactInformation", Reason: deferAccount},
		{Operation: "SendPhoneNumberVerification", Reason: deferAccount},
		{Operation: "VerifyPhoneNumber", Reason: deferAccount},
	},
}
