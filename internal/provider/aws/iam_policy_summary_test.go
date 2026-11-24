package aws

import (
	"strings"
	"testing"
	"unicode/utf8"
)

const wideDoc = `{"Version":"2012-10-17","Statement":[` +
	`{"Effect":"Allow","Action":["iam:*","s3:GetObject"],"Resource":"arn:aws:s3:::example-logs/*"},` +
	`{"Effect":"Allow","Action":"s3:ListBucket","Resource":["*","arn:aws:s3:::other-owner:x","arn:aws:kms:us-east-1:210987654321:key/abc"]},` +
	`{"Effect":"Deny","Action":"*","Resource":"*"}]}`

// Every tool gets a text that names the policy and the identity with its kind, through the core, without a request.
func TestIAMPolicyConfirmationTexts(t *testing.T) {
	for _, c := range []struct {
		id   string
		want []string
	}{
		{"aws.iam.policy.create", []string{"Create IAM managed policy reader", "grants nothing until attached", "3 statements (1 Deny)", "1 allow with wildcard Action [iam:*]", "2 allow with wildcard Resource", "210987654321"}},
		{"aws.iam.policy.delete", []string{"Delete IAM managed policy " + ownPolicyARN, "(account 123456789012)"}},
		{"aws.iam.policy-version.create", []string{"version of IAM managed policy " + ownPolicyARN, "make it the default", "takes effect at once", "3 statements", "iam:*"}},
		{"aws.iam.policy-version.set-default", []string{"version v2", ownPolicyARN, "takes effect at once"}},
		{"aws.iam.policy-version.delete", []string{"Delete version v2", ownPolicyARN}},
		{"aws.iam.policy.tag", []string{"Tag IAM managed policy " + ownPolicyARN, "tag keys k", "policy conditions"}},
		{"aws.iam.policy.untag", []string{"Remove tag keys k", ownPolicyARN}},
		{"aws.iam.attached-policy.attach-to-user", []string{"Attach managed policy " + managedARN, "(AWS managed)", "IAM user web"}},
		{"aws.iam.attached-policy.attach-to-group", []string{"Attach managed policy " + managedARN, "IAM group web"}},
		{"aws.iam.attached-policy.attach-to-role", []string{"Attach managed policy " + managedARN, "IAM role web"}},
		{"aws.iam.attached-policy.detach-from-user", []string{"Detach managed policy " + managedARN, "from IAM user web"}},
		{"aws.iam.attached-policy.detach-from-group", []string{"Detach managed policy " + managedARN, "from IAM group web"}},
		{"aws.iam.attached-policy.detach-from-role", []string{"Detach managed policy " + managedARN, "from IAM role web"}},
		{"aws.iam.inline-policy.put-for-user", []string{"Write inline policy p of IAM user web", "3 statements", "iam:*", "210987654321"}},
		{"aws.iam.inline-policy.put-for-group", []string{"Write inline policy p of IAM group web", "3 statements"}},
		{"aws.iam.inline-policy.put-for-role", []string{"Write inline policy p of IAM role web", "3 statements"}},
		{"aws.iam.inline-policy.delete-for-user", []string{"Delete inline policy p of IAM user web"}},
		{"aws.iam.inline-policy.delete-for-group", []string{"Delete inline policy p of IAM group web"}},
		{"aws.iam.inline-policy.delete-for-role", []string{"Delete inline policy p of IAM role web"}},
		{"aws.iam.user.put-permissions-boundary", []string{"permissions boundary of IAM user web to " + managedARN, "(AWS managed)"}},
		{"aws.iam.user.delete-permissions-boundary", []string{"Remove the permissions boundary of IAM user web", "widens"}},
	} {
		w := iamToolCase(t, c.id)
		f := direct(func(sent, int) reply { return reply{status: 500} })
		e := iamWriteEnv(t, f, c.id)
		got := confirmationSummary(t, e, c.id, iamPolicyArgs(w.args, "web", wideDoc))
		for _, part := range c.want {
			if !strings.Contains(got, part) {
				t.Errorf("%s: text %q lacks %q", c.id, got, part)
			}
		}
		if len(f.requests) != 0 {
			t.Errorf("%s: an unconfirmed call reached the network", c.id)
		}
	}
}

func iamToolCase(t *testing.T, id string) iamPolicyCase {
	t.Helper()
	for _, w := range iamPolicyTools {
		if w.id == id {
			return w
		}
	}
	t.Fatalf("no case for %s", id)
	return iamPolicyCase{}
}

// A policy tool without a text would confirm blind, so every one of them registers one.
func TestIAMPolicyToolsHaveConfirmationTexts(t *testing.T) {
	for _, w := range iamPolicyTools {
		if summaries[w.id] == nil {
			t.Errorf("%s has no confirmation text", w.id)
		}
	}
}

// The document is never quoted: the text holds figures and a few actions, a NotAction counts as wide, and an
// unreadable document gives no figures.
func TestIAMDocumentSummary(t *testing.T) {
	got := iamDocumentSummary(wideDoc)
	if strings.Contains(got, "GetObject") || strings.Contains(got, "example-logs") || strings.Contains(got, "2012-10-17") {
		t.Errorf("the text quotes the document: %q", got)
	}
	notAction := `{"Version":"2012-10-17","Statement":{"Effect":"Allow","NotAction":"iam:*","NotResource":"arn:aws:s3:::b"}}`
	if got := iamDocumentSummary(notAction); !strings.Contains(got, "1 statements") || !strings.Contains(got, "NotAction iam:*") ||
		!strings.Contains(got, "1 allow with wildcard Resource") {
		t.Errorf("NotAction text = %q", got)
	}
	for _, bad := range []string{"", "not json", `{"Version":"2012-10-17"}`, `{"Statement":"x"}`} {
		if got := iamDocumentSummary(bad); got != "" {
			t.Errorf("document %q gives %q", bad, got)
		}
	}
	if got := iamPolicyCreateSummary([]byte(`{"policy_name":"p","policy_document":"{"}`)); strings.Contains(got, "statements") {
		t.Errorf("unreadable document text = %q", got)
	}
}

// Long names, many actions, and many accounts are cut, and the text stays inside the default bound of the core.
func TestIAMPolicyConfirmationBounds(t *testing.T) {
	long := strings.Repeat("a", 500)
	var actions, resources []string
	for i := range 20 {
		actions = append(actions, `"`+long[:60]+string(rune('a'+i))+`:*"`)
		resources = append(resources, `"arn:aws:s3::`+strings.Repeat("1", 12)[:11]+string(rune('a'+i))+`:x"`)
	}
	doc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":[` + strings.Join(actions, ",") +
		`],"Resource":[` + strings.Join(resources, ",") + `]}]}`
	args := iamPolicyArgs(`{"policy_arn":"arn:aws:iam::123456789012:policy/`+long+`","set_as_default":true,"policy_document":%q}`, "", doc)
	got := iamVersionCreateSummary([]byte(args))
	if strings.Contains(got, long[:200]) || !strings.Contains(got, "more") {
		t.Errorf("values not cut: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > 500 {
		t.Errorf("text has %d runes, over the core default of 500", n)
	}
	tags := iamTagSummary([]byte(`{"policy_arn":"` + ownPolicyARN + `","tags":[{"key":"a"},{"key":"b"},{"key":"c"},{"key":"d"},{"key":"e"},{"key":"f"}]}`))
	if !strings.Contains(tags, "+2 more") {
		t.Errorf("tag keys not counted: %q", tags)
	}
}
