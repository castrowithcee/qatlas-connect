package aws

// The IAM account setting tools carry a confirmation text.

func init() {
	expectSummary(
		"aws.iam.account.update-password-policy",
		"aws.iam.account.delete-password-policy",
		"aws.iam.account.create-alias",
		"aws.iam.account.delete-alias",
		"aws.iam.oidc-provider.delete",
		"aws.iam.saml-provider.delete",
	)
}
