package aws

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

const iamBoundaryARN = "arn:aws:iam::aws:policy/PowerUserAccess"

func trustDoc(effect string, principal string) string {
	return `{"Version":"2012-10-17","Statement":[{"Effect":"` + effect + `","Principal":` + principal + `,"Action":"sts:AssumeRole"}]}`
}

// Each role tool has a text that names the role, the effect, and what the tool is about, and it reaches the
// confirmation-required error without a request.
func TestIAMRoleConfirmationTexts(t *testing.T) {
	multi := trustDoc("Allow", `{"AWS":["arn:aws:iam::123456789012:root","123456789012"],"Service":"ec2.amazonaws.com"}`)
	for _, c := range []struct {
		id, args string
		want     []string
	}{
		{"aws.iam.role.create", trustArgs(multi),
			[]string{"Create IAM role web", "ec2.amazonaws.com", "arn:aws:iam::123456789012:root", "123456789012"}},
		{"aws.iam.role.create", strings.TrimSuffix(trustArgs(ec2Trust), "}") + `,"permissions_boundary":"` + iamBoundaryARN + `"}`,
			[]string{"Create IAM role web", "ec2.amazonaws.com", "AWS managed policy PowerUserAccess"}},
		{"aws.iam.role.update", `{"role_name":"web","max_session_duration":7200}`,
			[]string{"maximum session duration to 7200 seconds", "IAM role web"}},
		{"aws.iam.role.update", `{"role_name":"web","description":"x"}`, []string{"Set the description of IAM role web"}},
		{"aws.iam.role.update-trust-policy", trustArgs(ec2Trust),
			[]string{"Replace the trust policy of IAM role web", "who may assume it", "ec2.amazonaws.com"}},
		{"aws.iam.role.put-permissions-boundary", `{"role_name":"web","permissions_boundary":"` + iamBoundaryARN + `"}`,
			[]string{"permissions boundary of IAM role web", "AWS managed policy PowerUserAccess", "replacing"}},
		{"aws.iam.role.put-permissions-boundary", `{"role_name":"web","permissions_boundary":"arn:aws:iam::123456789012:policy/pb"}`,
			[]string{"policy arn:aws:iam::123456789012:policy/pb"}},
		{"aws.iam.role.delete-permissions-boundary", `{"role_name":"web"}`,
			[]string{"Remove the permissions boundary of IAM role web", "may afterwards do more"}},
		{"aws.iam.role.tag", `{"role_name":"web","tags":[{"key":"team","value":"w"},{"key":"env","value":"p"}]}`,
			[]string{"2 tag(s) on IAM role web", "team, env", "policy conditions"}},
		{"aws.iam.role.untag", `{"role_name":"web","tag_keys":["team"]}`, []string{"Remove 1 tag(s) from IAM role web", "team"}},
		{"aws.iam.role.delete", `{"role_name":"old-web"}`, []string{"Delete IAM role old-web", "cannot be undone"}},
		{"aws.iam.instance-profile.create", `{"instance_profile_name":"web"}`, []string{"Create IAM instance profile web", "no role"}},
		{"aws.iam.instance-profile.delete", `{"instance_profile_name":"old"}`, []string{"Delete IAM instance profile old", "cannot be undone"}},
		{"aws.iam.instance-profile.add-role", `{"instance_profile_name":"p1","role_name":"r1"}`,
			[]string{"Add IAM role r1 to IAM instance profile p1", "receive the role's credentials"}},
		{"aws.iam.instance-profile.remove-role", `{"instance_profile_name":"p1","role_name":"r1"}`,
			[]string{"Remove IAM role r1 from IAM instance profile p1", "lose the role's credentials"}},
	} {
		f := direct(nil)
		e := iamWriteEnv(t, f, c.id)
		got := confirmationSummary(t, e, c.id, c.args)
		for _, part := range c.want {
			if !strings.Contains(got, part) {
				t.Errorf("%s: summary %q lacks %q", c.id, got, part)
			}
		}
		if len(f.requests) != 0 {
			t.Errorf("%s: an unconfirmed call reached the network", c.id)
		}
	}
}

// The text names the principals, counts what it leaves out, counts Deny statements, and never quotes the document.
func TestIAMTrustSummaryPrincipals(t *testing.T) {
	var ps []string
	for i := 0; i < 7; i++ {
		ps = append(ps, `"svc`+string(rune('a'+i))+`.amazonaws.com"`)
	}
	doc := `{"Version":"2012-10-17","Id":"secret-id","Statement":[{"Sid":"hidden-sid","Effect":"Allow","Principal":{"Service":[` +
		strings.Join(ps, ",") + `]},"Action":"sts:AssumeRole"},{"Effect":"Deny","Principal":{"AWS":"123456789012"},"Action":"sts:AssumeRole"}]}`
	got := roleTrustSummary([]byte(trustArgs(doc)))
	for _, want := range []string{"svca.amazonaws.com", "svcd.amazonaws.com", "and 3 more", "1 Deny statement(s)"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	for _, not := range []string{"svce.amazonaws.com", "secret-id", "hidden-sid", "sts:AssumeRole", "2012-10-17", "123456789012"} {
		if strings.Contains(got, not) {
			t.Errorf("summary %q holds %q", got, not)
		}
	}
	if got := roleTrustSummary([]byte(trustArgs(trustDoc("Allow", `"*"`)))); !strings.Contains(got, "assumable by *") {
		t.Errorf("wildcard summary = %q", got)
	}
}

// A long or hostile value is cut, the text stays inside the budget of the core, and an unreadable trust policy or
// argument gives a text without document details or none.
func TestIAMConfirmationTextBoundsValues(t *testing.T) {
	long := strings.Repeat("k", 1000) + `\n`
	var ps []string
	for i := 0; i < 20; i++ {
		ps = append(ps, `"`+strings.Repeat("p", 200)+strconv.Itoa(i)+`.amazonaws.com"`)
	}
	doc := trustDoc("Allow", `{"Service":[`+strings.Join(ps, ",")+`]}`)
	text, _ := json.Marshal(doc)
	args := `{"role_name":"` + long + `","assume_role_policy_document":` + string(text) + `,"permissions_boundary":"arn:aws:iam::` +
		strings.Repeat("9", 500) + `:policy/x"}`
	for _, f := range []func(json.RawMessage) string{roleCreateSummary, roleTrustSummary} {
		got := f([]byte(args))
		if strings.Contains(got, "\n") || strings.Contains(got, `\n`) || utf8.RuneCountInString(got) > 450 || !strings.Contains(got, "more") {
			t.Errorf("summary (%d runes) = %q", utf8.RuneCountInString(got), got)
		}
	}
	got := roleCreateSummary([]byte(trustArgs("not json")))
	if !strings.Contains(got, "IAM role web") || !strings.Contains(got, "not readable") || strings.Contains(got, "not json") {
		t.Errorf("unreadable document summary = %q", got)
	}
	for _, f := range []func(json.RawMessage) string{roleCreateSummary, roleUpdateSummary, roleTrustSummary, roleBoundarySummary,
		roleBoundaryRemoveSummary, roleTagSummary, roleUntagSummary, roleDeleteSummary, profileCreateSummary, profileDeleteSummary,
		profileAddRoleSummary, profileRemoveRoleSummary} {
		if f([]byte(`{"role_name":1}`)) != "" || f([]byte(`nope`)) != "" {
			t.Error("an unreadable argument gave a text")
		}
	}
}
