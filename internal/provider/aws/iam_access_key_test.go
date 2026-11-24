package aws

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const accessKeyOther = "OTHERKEYIDEXAMPLE01"

var accessKeyTools = []struct {
	id, action string
	effect     capability.Effect
	idem       capability.Idempotency
}{
	{"aws.iam.access-key.deactivate", "UpdateAccessKey", capability.EffectUpdate, capability.IdempotencyIdempotent},
	{"aws.iam.access-key.delete", "DeleteAccessKey", capability.EffectDelete, capability.IdempotencyNonIdempotent},
}

func accessKeyArgs(key string) string {
	return `{"access_key_id":"` + key + `","user_name":"web"}`
}

func accessKeyEnv(t *testing.T, f *fakeAWS) *env {
	t.Helper()
	return iamWriteEnv(t, f, "aws.iam.access-key.deactivate", "aws.iam.access-key.delete")
}

// Both tools are listed-only writes with confirmation and the own-key check; deactivate offers no status.
func TestAccessKeyToolsAreListedOnlyWrites(t *testing.T) {
	for _, w := range accessKeyTools {
		s := specOf(t, w.id)
		d := s.descriptor
		if !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired || d.Risk.Effect != w.effect ||
			!d.Risk.OpenWorld || d.Risk.Idempotency != w.idem || s.action != w.action {
			t.Errorf("%s descriptor = %+v", w.id, d)
		}
		if s.selfKey != "access_key_id" || s.selfUser != "" || s.selfRole != "" {
			t.Errorf("%s own-identity check key %q user %q role %q", w.id, s.selfKey, s.selfUser, s.selfRole)
		}
		schema := string(d.InputSchema)
		if strings.Contains(schema, "status") || strings.Contains(schema, "Active") || strings.Contains(d.Description, "Active") {
			t.Errorf("%s offers or names a status: %s", w.id, schema)
		}
	}
}

// deactivate always sends Status=Inactive; a status argument is refused by the schema.
func TestAccessKeyDeactivateSendsInactive(t *testing.T) {
	f := direct(func(r sent, _ int) reply { return iamAnswer(r.action, "") })
	e := accessKeyEnv(t, f)
	if _, err := e.invokeConfirmed("aws.iam.access-key.deactivate", accessKeyArgs(accessKeyOther), true); err != nil {
		t.Fatal(err)
	}
	last := f.requests[len(f.requests)-1]
	if last.action != "UpdateAccessKey" || last.form.Get("Status") != "Inactive" ||
		last.form.Get("AccessKeyId") != accessKeyOther || last.form.Get("UserName") != "web" {
		t.Errorf("form = %v", last.form)
	}
	before := len(f.requests)
	for _, status := range []string{"Active", "Inactive"} {
		args := `{"access_key_id":"` + accessKeyOther + `","status":"` + status + `"}`
		if _, err := e.invokeConfirmed("aws.iam.access-key.deactivate", args, true); !isInvalidRequest(err) || len(f.requests) != before {
			t.Errorf("status %s: %v, requests %v", status, err, f.actions())
		}
	}
	if _, err := e.invokeConfirmed("aws.iam.access-key.delete", accessKeyArgs(accessKeyOther), true); err != nil ||
		f.count("DeleteAccessKey") != 1 || f.requests[len(f.requests)-1].form.Get("Status") != "" {
		t.Errorf("delete: %v, requests %v", err, f.actions())
	}
}

// The key of the credential and the key that signs are refused before any change, without the value in the
// message, in any case; another key passes.
func TestAccessKeyOwnKeyIsRefused(t *testing.T) {
	for _, w := range accessKeyTools {
		answer := func(r sent, _ int) reply { return iamAnswer(r.action, "") }

		f := direct(answer)
		e := accessKeyEnv(t, f)
		for _, key := range []string{testKeyID, strings.ToLower(testKeyID)} {
			_, err := e.invokeConfirmed(w.id, accessKeyArgs(key), true)
			if !isInvalidRequest(err) || f.count(w.action) != 0 || strings.Contains(strings.ToLower(err.Error()), strings.ToLower(key)) {
				t.Errorf("%s own key: %v, requests %v", w.id, err, f.actions())
			}
		}
		for _, key := range []string{accessKeyOther} {
			if _, err := e.invokeConfirmed(w.id, accessKeyArgs(key), true); err != nil {
				t.Errorf("%s other key %s: %v", w.id, key, err)
			}
		}

		// With a role, the session key signs and the key of the credential assumes the role: both are refused.
		var rf *fakeAWS
		rf = &fakeAWS{answer: func(r sent, n int) reply {
			switch r.action {
			case "AssumeRole":
				return ok(assumeNumbered(testNow.Add(time.Hour), n))
			case "GetCallerIdentity":
				return ok(identityXML)
			}
			return answer(r, n)
		}}
		re := newConnEnv(t, rf, roleValues(), config.Connection{Tools: []string{w.id}})
		for _, key := range []string{sessionKeyID, testKeyID} {
			_, err := re.invokeConfirmed(w.id, accessKeyArgs(key), true)
			if !isInvalidRequest(err) || rf.count(w.action) != 0 || strings.Contains(err.Error(), key) {
				t.Errorf("%s role session key %s: %v, requests %v", w.id, key, err, rf.actions())
			}
		}
		if _, err := re.invokeConfirmed(w.id, accessKeyArgs(accessKeyOther), true); err != nil || rf.count(w.action) != 1 {
			t.Errorf("%s other key with a role: %v, requests %v", w.id, err, rf.actions())
		}
	}
}

// Confirmation is required, and IAM errors keep their class.
func TestAccessKeyErrorsAndConfirmation(t *testing.T) {
	for _, w := range accessKeyTools {
		f := direct(nil)
		e := accessKeyEnv(t, f)
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke(w.id, accessKeyArgs(accessKeyOther)); !errorsAs(err, &unconfirmed) || len(f.requests) != 0 {
			t.Errorf("%s unconfirmed: %v after %v", w.id, err, f.actions())
		}
		for _, c := range []struct {
			code   string
			status int
			class  provider.Class
		}{{"NoSuchEntity", 404, provider.ClassNotFound}, {"LimitExceeded", 409, provider.ClassProviderError}} {
			f := direct(func(sent, int) reply { return reply{status: c.status, body: iamErrorFor(c.code)} })
			_, err := accessKeyEnv(t, f).invokeConfirmed(w.id, accessKeyArgs(accessKeyOther), true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil ||
				failed.Original.Entries[0].Code != c.code || f.count(w.action) != 1 {
				t.Errorf("%s %s: %v after %v", w.id, c.code, err, f.actions())
			}
		}
	}
}

// The text reaches the confirmation-required error without a request, names the action, the key, and the
// user, and never holds the secret of the credential.
func TestAccessKeyConfirmationTexts(t *testing.T) {
	secretValue := fmt.Sprintf("zz-%d-unit-test-value", time.Now().UnixNano())
	for _, c := range []struct {
		id, args string
		want     []string
	}{
		{"aws.iam.access-key.deactivate", accessKeyArgs(accessKeyOther), []string{"Deactivate access key " + accessKeyOther + " of IAM user web", "refused from then on"}},
		{"aws.iam.access-key.delete", accessKeyArgs(accessKeyOther), []string{"Permanently delete access key " + accessKeyOther + " of IAM user web", "cannot be undone"}},
		{"aws.iam.access-key.delete", `{"access_key_id":"` + accessKeyOther + `"}`, []string{"of the calling user"}},
	} {
		f := direct(nil)
		e := newToolsEnv(t, f, baseValues().with(envSecret, secretValue), []string{c.id})
		got := confirmationSummary(t, e, c.id, c.args)
		for _, part := range c.want {
			if !strings.Contains(got, part) {
				t.Errorf("%s: text %q lacks %q", c.id, got, part)
			}
		}
		if strings.Contains(got, secretValue) || strings.Contains(got, testSecret) || len(f.requests) != 0 {
			t.Errorf("%s: text %q holds a secret or a request was sent", c.id, got)
		}
	}
	long := strings.Repeat("a", 500)
	if got := accessKeySummaryOf("Deactivate", "")([]byte(`{"access_key_id":"` + long + `","user_name":"` + long + `"}`)); strings.Contains(got, long[:100]) {
		t.Errorf("values not cut: %q", got)
	}
	if got := accessKeySummaryOf("Deactivate", "")([]byte(`[`)); got != "" {
		t.Errorf("unreadable arguments gave %q", got)
	}
}

// No excluded credential operation is a tool, and none is registered under an access key or login profile ID.
func TestAccessKeyExcludedOperationsAreNoTools(t *testing.T) {
	excluded := []string{"CreateAccessKey", "CreateLoginProfile", "UpdateLoginProfile", "DeleteLoginProfile", "ChangePassword",
		"CreateServiceSpecificCredential", "UploadSSHPublicKey", "UploadSigningCertificate", "CreateVirtualMFADevice",
		"EnableMFADevice", "DeactivateMFADevice", "CreateUser"}
	for _, a := range excluded {
		if !slices.Contains(excludedIAMActions, a) {
			t.Errorf("%s is not in the excluded list", a)
		}
	}
	for _, s := range generatedSpecs {
		if slices.Contains(excluded, s.action) {
			t.Errorf("%s sends the excluded operation %s", s.descriptor.ID, s.action)
		}
		if s.action == "UpdateAccessKey" && s.fixed["Status"] != "Inactive" {
			t.Errorf("%s does not fix the status to Inactive", s.descriptor.ID)
		}
	}
}

// Without an entry in tools the tools are not offered.
func TestAccessKeyToolsNeedTheirExactName(t *testing.T) {
	for name, c := range map[string]config.Connection{
		"by every effect": {Effects: []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}},
		"by default":      {},
		"by prefix":       {Tools: []string{"aws.iam.access-key.*"}},
		"by another tool": {Tools: []string{"aws.iam.access-key.list"}},
	} {
		f := direct(nil)
		e := newConnEnv(t, f, baseValues(), c)
		for _, w := range accessKeyTools {
			_, err := e.invokeConfirmed(w.id, accessKeyArgs(accessKeyOther), true)
			var notOffered *capability.UnsupportedError
			if !errorsAs(err, &notOffered) || len(f.requests) != 0 {
				t.Errorf("%s %s: %v, requests %v", name, w.id, err, f.actions())
			}
		}
	}
}
