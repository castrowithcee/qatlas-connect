package gen

const (
	descPolicyMinLength = "Minimum password length, 6 to 128; 6 when left out"
	descPolicyMaxAge    = "Days a password is valid, 1 to 1095; no expiry when left out"
	descPolicyReuse     = "Number of previous passwords a user cannot reuse, 1 to 24; none is remembered when left out"
	descPolicyHard      = "True to forbid users from changing an expired password so that an administrator must reset it"
	descAliasName       = "Account alias, 3 to 63 lowercase letters, digits, or hyphens, not starting or ending with a hyphen"
)

// iamAccountWriteTools are the IAM operations that change the password policy and the alias of the account and
// delete identity providers. Creating and changing a provider stays excluded.
var iamAccountWriteTools = []Operation{
	iamWrite(Operation{
		Operation: "UpdateAccountPasswordPolicy", Object: "account", Action: "update-password-policy",
		Title: "Set the password policy of the account",
		Description: "Set the password policy of the bound account, which applies to all its IAM users. A setting left " +
			"out returns to its default, so give every setting to keep. Needs iam:UpdateAccountPasswordPolicy. " + iamWriteRule,
		Arguments: map[string]Argument{
			"MinimumPasswordLength":      {Description: descPolicyMinLength},
			"RequireSymbols":             {Description: "True to require a non-alphanumeric character; false when left out"},
			"RequireNumbers":             {Description: "True to require a digit; false when left out"},
			"RequireUppercaseCharacters": {Description: "True to require an uppercase letter; false when left out"},
			"RequireLowercaseCharacters": {Description: "True to require a lowercase letter; false when left out"},
			"AllowUsersToChangePassword": {Description: "True to let IAM users change their own password; false when left out"},
			"MaxPasswordAge":             {Description: descPolicyMaxAge},
			"PasswordReusePrevention":    {Description: descPolicyReuse},
			"HardExpiry":                 {Description: descPolicyHard},
		},
		Examples: []Example{{Description: "Require 14 characters with digits and symbols", Arguments: `{"minimum_password_length":14,"require_numbers":true,"require_symbols":true,"max_password_age":90}`}},
	}, "update", "idempotent", "account", "password-policy"),
	iamWrite(Operation{
		Operation: "DeleteAccountPasswordPolicy", Object: "account", Action: "delete-password-policy",
		Title: "Delete the password policy of the account",
		Description: "Delete the password policy of the bound account; the AWS default policy applies afterwards. Needs " +
			"iam:DeleteAccountPasswordPolicy. " + iamWriteRule,
		Examples: []Example{{Description: "Delete the password policy", Arguments: `{}`}},
	}, "delete", "non-idempotent", "account", "password-policy"),
	iamWrite(Operation{
		Operation: "CreateAccountAlias", Object: "account", Action: "create-alias",
		Title: "Create the alias of the account",
		Description: "Create the alias of the bound account, which changes the sign-in address of the console. An alias " +
			"is unique across AWS and an account has one. Needs iam:CreateAccountAlias. Not repeated after an unclear " +
			"outcome: read the aliases before trying again. " + iamWriteRule,
		Arguments: map[string]Argument{"AccountAlias": {Description: descAliasName}},
		Examples:  []Example{{Description: "Create an alias", Arguments: `{"account_alias":"example-team"}`}},
	}, "create", "non-idempotent", "account", "alias"),
	iamWrite(Operation{
		Operation: "DeleteAccountAlias", Object: "account", Action: "delete-alias",
		Title: "Delete the alias of the account",
		Description: "Delete the alias of the bound account; the sign-in address of the console returns to the account ID. " +
			"Needs iam:DeleteAccountAlias. " + iamWriteRule,
		Arguments: map[string]Argument{"AccountAlias": {Description: descAliasName}},
		Examples:  []Example{{Description: "Delete an alias", Arguments: `{"account_alias":"example-team"}`}},
	}, "delete", "non-idempotent", "account", "alias"),
	withPrincipalArns(iamWrite(Operation{
		Operation: "DeleteOpenIDConnectProvider", Object: "oidc-provider", Action: "delete",
		Title: "Delete an OpenID Connect provider",
		Description: "Delete an OpenID Connect identity provider of the bound account for good; sign-in through it ends, " +
			"and roles that trust it can no longer be assumed with its tokens. Needs iam:DeleteOpenIDConnectProvider. " + iamWriteRule,
		Arguments: map[string]Argument{"OpenIDConnectProviderArn": {Description: descOIDCArn}},
		Examples:  []Example{{Description: "Delete a provider", Arguments: `{"open_id_connect_provider_arn":"arn:aws:iam::123456789012:oidc-provider/example.com"}`}},
	}, "delete", "non-idempotent", "oidc-provider"), "OpenIDConnectProviderArn"),
	withPrincipalArns(iamWrite(Operation{
		Operation: "DeleteSAMLProvider", Object: "saml-provider", Action: "delete",
		Title: "Delete a SAML provider",
		Description: "Delete a SAML identity provider of the bound account for good; sign-in through it ends, and roles " +
			"that trust it can no longer be assumed with its assertions. Needs iam:DeleteSAMLProvider. " + iamWriteRule,
		Arguments: map[string]Argument{"SAMLProviderArn": {Description: descSAMLArn}},
		Examples:  []Example{{Description: "Delete a provider", Arguments: `{"saml_provider_arn":"arn:aws:iam::123456789012:saml-provider/corp"}`}},
	}, "delete", "non-idempotent", "saml-provider"), "SAMLProviderArn"),
}
