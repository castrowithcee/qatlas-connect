package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	accountWriteOIDC = "arn:aws:iam::123456789012:oidc-provider/example.com"
	accountWriteSAML = "arn:aws:iam::123456789012:saml-provider/corp"
)

var accountWriteTools = []struct {
	id, action, args, errCode string
	effect                    capability.Effect
	idem                      capability.Idempotency
	wantText                  string
}{
	{"aws.iam.account.update-password-policy", "UpdateAccountPasswordPolicy",
		`{"minimum_password_length":14,"require_numbers":true,"require_symbols":true,"max_password_age":90,"password_reuse_prevention":5}`,
		"LimitExceeded", capability.EffectUpdate, capability.IdempotencyIdempotent,
		"minimum length 14, required characters digits, symbols, expiry after 90 days, reuse last 5 blocked"},
	{"aws.iam.account.delete-password-policy", "DeleteAccountPasswordPolicy", `{}`,
		"NoSuchEntity", capability.EffectDelete, capability.IdempotencyNonIdempotent, "AWS default policy applies"},
	{"aws.iam.account.create-alias", "CreateAccountAlias", `{"account_alias":"example-team"}`,
		"EntityAlreadyExists", capability.EffectCreate, capability.IdempotencyNonIdempotent, "alias example-team; the sign-in address"},
	{"aws.iam.account.delete-alias", "DeleteAccountAlias", `{"account_alias":"example-team"}`,
		"NoSuchEntity", capability.EffectDelete, capability.IdempotencyNonIdempotent, "alias example-team; the sign-in address"},
	{"aws.iam.oidc-provider.delete", "DeleteOpenIDConnectProvider", `{"open_id_connect_provider_arn":"` + accountWriteOIDC + `"}`,
		"NoSuchEntity", capability.EffectDelete, capability.IdempotencyNonIdempotent, accountWriteOIDC + "; sign-ins through it end"},
	{"aws.iam.saml-provider.delete", "DeleteSAMLProvider", `{"saml_provider_arn":"` + accountWriteSAML + `"}`,
		"NoSuchEntity", capability.EffectDelete, capability.IdempotencyNonIdempotent, accountWriteSAML + "; sign-ins through it end"},
}

func accountWriteEnv(t *testing.T, f *fakeAWS) *env {
	t.Helper()
	ids := make([]string, len(accountWriteTools))
	for i, w := range accountWriteTools {
		ids[i] = w.id
	}
	return iamWriteEnv(t, f, ids...)
}

// Every tool is a listed-only write with confirmation and sends its action.
func TestAccountWriteToolsAreListedOnlyWrites(t *testing.T) {
	for _, w := range accountWriteTools {
		s := specOf(t, w.id)
		d := s.descriptor
		if !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired || d.Risk.Effect != w.effect ||
			!d.Risk.OpenWorld || d.Risk.Idempotency != w.idem || s.action != w.action {
			t.Errorf("%s descriptor = %+v", w.id, d)
		}
		f := direct(func(r sent, _ int) reply { return iamAnswer(r.action, "") })
		if _, err := accountWriteEnv(t, f).invokeConfirmed(w.id, w.args, true); err != nil || f.count(w.action) != 1 {
			t.Errorf("%s: %v, requests %v", w.id, err, f.actions())
		}
	}
}

// The password policy sends every given setting.
func TestAccountWritePasswordPolicyForm(t *testing.T) {
	f := direct(func(r sent, _ int) reply { return iamAnswer(r.action, "") })
	if _, err := accountWriteEnv(t, f).invokeConfirmed(accountWriteTools[0].id, accountWriteTools[0].args, true); err != nil {
		t.Fatal(err)
	}
	form := f.requests[len(f.requests)-1].form
	for k, v := range map[string]string{"MinimumPasswordLength": "14", "RequireNumbers": "true", "RequireSymbols": "true", "MaxPasswordAge": "90", "PasswordReusePrevention": "5"} {
		if form.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, form.Get(k), v)
		}
	}
}

// Errors keep their class and code, and a missing confirmation sends nothing.
func TestAccountWriteErrorsAndConfirmation(t *testing.T) {
	for _, w := range accountWriteTools {
		var unconfirmed *application.ConfirmationRequiredError
		f := direct(nil)
		if _, err := accountWriteEnv(t, f).invoke(w.id, w.args); !errorsAs(err, &unconfirmed) || len(f.requests) != 0 {
			t.Errorf("%s unconfirmed: %v after %v", w.id, err, f.actions())
		}
		for _, c := range []struct {
			code   string
			status int
			class  provider.Class
		}{{w.errCode, 409, provider.ClassProviderError}, {"NoSuchEntity", 404, provider.ClassNotFound}, {"LimitExceeded", 409, provider.ClassProviderError}} {
			if c.code == "NoSuchEntity" {
				c.class = provider.ClassNotFound
			}
			f := direct(func(sent, int) reply { return reply{status: c.status, body: iamErrorFor(c.code)} })
			_, err := accountWriteEnv(t, f).invokeConfirmed(w.id, w.args, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil ||
				failed.Original.Entries[0].Code != c.code || f.count(w.action) != 1 {
				t.Errorf("%s %s: %v after %v", w.id, c.code, err, f.actions())
			}
		}
	}
}

// A provider ARN of another account or partition is refused before any request.
func TestAccountWriteProviderARNIsChecked(t *testing.T) {
	for _, w := range accountWriteTools[4:] {
		own := accountWriteOIDC
		if strings.Contains(w.args, "saml") {
			own = accountWriteSAML
		}
		for _, bad := range []string{
			strings.Replace(own, "123456789012", "210987654321", 1),
			strings.Replace(own, "arn:aws:", "arn:aws-cn:", 1),
			"arn:aws:s3:::bucket",
		} {
			f := direct(func(r sent, _ int) reply { return iamAnswer(r.action, "") })
			_, err := accountWriteEnv(t, f).invokeConfirmed(w.id, strings.Replace(w.args, own, bad, 1), true)
			if !isInvalidRequest(err) || len(f.requests) != 0 || strings.Contains(err.Error(), "210987654321") {
				t.Errorf("%s with %s: %v, requests %v", w.id, bad, err, f.actions())
			}
		}
	}
}

// The text reaches the confirmation-required error without a request.
func TestAccountWriteConfirmationTexts(t *testing.T) {
	for _, w := range accountWriteTools {
		f := direct(nil)
		e := newToolsEnv(t, f, baseValues(), []string{w.id})
		if got := confirmationSummary(t, e, w.id, w.args); !strings.Contains(got, w.wantText) || len(f.requests) != 0 {
			t.Errorf("%s: text %q lacks %q", w.id, got, w.wantText)
		}
	}
	long := strings.Repeat("a", 500)
	if got := accountSummaryOf(func(a accountSummaryArgs) string { return summaryClip(a.OIDCArn, accountSummaryARNRunes) })([]byte(`{"open_id_connect_provider_arn":"` + long + `"}`)); strings.Contains(got, long[:300]) {
		t.Errorf("value not cut: %q", got)
	}
	if got := accountPolicySummary(accountSummaryArgs{}); len([]rune(got)) >= 500 || !strings.Contains(got, "minimum length 6") {
		t.Errorf("default policy text %q", got)
	}
	if got := accountSummaryOf(accountPolicySummary)([]byte(`[`)); got != "" {
		t.Errorf("unreadable arguments gave %q", got)
	}
}

// Without an exact entry in tools the tools are not offered.
func TestAccountWriteToolsNeedTheirExactName(t *testing.T) {
	for name, c := range map[string]config.Connection{
		"by every effect": {Effects: []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}},
		"by default":      {},
		"by prefix":       {Tools: []string{"aws.iam.account.*", "aws.iam.oidc-provider.*", "aws.iam.saml-provider.*"}},
	} {
		f := direct(nil)
		e := newConnEnv(t, f, baseValues(), c)
		for _, w := range accountWriteTools {
			_, err := e.invokeConfirmed(w.id, w.args, true)
			var notOffered *capability.UnsupportedError
			if !errorsAs(err, &notOffered) || len(f.requests) != 0 {
				t.Errorf("%s %s: %v, requests %v", name, w.id, err, f.actions())
			}
		}
	}
}
