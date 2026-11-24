package aws

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The confirmation texts of the IAM account setting tools. Alias and ARN come from untrusted arguments, so
// each is cut; the texts stay well below the 500 runes the core allows.
const (
	accountSummaryAliasRunes = 63
	accountSummaryARNRunes   = 200
)

func init() {
	registerSummary("aws.iam.account.update-password-policy", accountSummaryOf(accountPolicySummary))
	registerSummary("aws.iam.account.delete-password-policy", accountSummaryOf(func(accountSummaryArgs) string {
		return "Delete the password policy of the account; the AWS default policy applies to all IAM users afterwards"
	}))
	registerSummary("aws.iam.account.create-alias", accountSummaryOf(func(a accountSummaryArgs) string {
		return "Create the account alias " + summaryClip(a.AccountAlias, accountSummaryAliasRunes) + "; the sign-in address of the console changes"
	}))
	registerSummary("aws.iam.account.delete-alias", accountSummaryOf(func(a accountSummaryArgs) string {
		return "Delete the account alias " + summaryClip(a.AccountAlias, accountSummaryAliasRunes) + "; the sign-in address of the console changes"
	}))
	registerSummary("aws.iam.oidc-provider.delete", accountSummaryOf(func(a accountSummaryArgs) string {
		return "Permanently delete OpenID Connect provider " + summaryClip(a.OIDCArn, accountSummaryARNRunes) + "; sign-ins through it end"
	}))
	registerSummary("aws.iam.saml-provider.delete", accountSummaryOf(func(a accountSummaryArgs) string {
		return "Permanently delete SAML provider " + summaryClip(a.SAMLArn, accountSummaryARNRunes) + "; sign-ins through it end"
	}))
}

type accountSummaryArgs struct {
	AccountAlias string `json:"account_alias"`
	OIDCArn      string `json:"open_id_connect_provider_arn"`
	SAMLArn      string `json:"saml_provider_arn"`
	MinLength    *int   `json:"minimum_password_length"`
	Symbols      bool   `json:"require_symbols"`
	Numbers      bool   `json:"require_numbers"`
	Upper        bool   `json:"require_uppercase_characters"`
	Lower        bool   `json:"require_lowercase_characters"`
	MaxAge       *int   `json:"max_password_age"`
	Reuse        *int   `json:"password_reuse_prevention"`
}

func accountSummaryOf(text func(accountSummaryArgs) string) func(json.RawMessage) string {
	return func(raw json.RawMessage) string {
		var a accountSummaryArgs
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		return text(a)
	}
}

func accountPolicySummary(a accountSummaryArgs) string {
	min := 6
	if a.MinLength != nil {
		min = *a.MinLength
	}
	var classes []string
	for _, c := range []struct {
		on   bool
		name string
	}{{a.Upper, "uppercase"}, {a.Lower, "lowercase"}, {a.Numbers, "digits"}, {a.Symbols, "symbols"}} {
		if c.on {
			classes = append(classes, c.name)
		}
	}
	required := "none"
	if len(classes) > 0 {
		required = strings.Join(classes, ", ")
	}
	expiry, reuse := "never", "not limited"
	if a.MaxAge != nil {
		expiry = "after " + strconv.Itoa(*a.MaxAge) + " days"
	}
	if a.Reuse != nil {
		reuse = "last " + strconv.Itoa(*a.Reuse) + " blocked"
	}
	return "Set the password policy for all IAM users of the account: minimum length " + strconv.Itoa(min) +
		", required characters " + required + ", expiry " + expiry + ", reuse " + reuse + "; unset settings return to their defaults"
}
