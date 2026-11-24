package aws

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	readerPolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`
	managedARN   = "arn:aws:iam::aws:policy/ReadOnlyAccess"
	ownPolicyARN = "arn:aws:iam::123456789012:policy/reader"
)

type iamPolicyCase struct {
	id, action, args, self, formKey string
	effect                          capability.Effect
	idem                            capability.Idempotency
	arn, doc                        bool
}

// iamPolicyTools are the IAM policy write tools: each with its effect and idempotency, the AWS action, arguments that
// pass every check, the identity check it carries ("user", "role", or ""), whether it checks a policy ARN
// (target) and a document, and the form parameter that proves the request carried the argument.
var iamPolicyTools = []iamPolicyCase{
	{"aws.iam.policy.create", "CreatePolicy", `{"policy_name":"reader","policy_document":%q}`, "", "PolicyName", capability.EffectCreate, capability.IdempotencyNonIdempotent, false, true},
	{"aws.iam.policy.delete", "DeletePolicy", `{"policy_arn":"` + ownPolicyARN + `"}`, "", "PolicyArn", capability.EffectDelete, capability.IdempotencyNonIdempotent, true, false},
	{"aws.iam.policy-version.create", "CreatePolicyVersion", `{"policy_arn":"` + ownPolicyARN + `","set_as_default":true,"policy_document":%q}`, "", "SetAsDefault", capability.EffectCreate, capability.IdempotencyNonIdempotent, true, true},
	{"aws.iam.policy-version.set-default", "SetDefaultPolicyVersion", `{"policy_arn":"` + ownPolicyARN + `","version_id":"v2"}`, "", "VersionId", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.policy-version.delete", "DeletePolicyVersion", `{"policy_arn":"` + ownPolicyARN + `","version_id":"v2"}`, "", "VersionId", capability.EffectDelete, capability.IdempotencyNonIdempotent, true, false},
	{"aws.iam.policy.tag", "TagPolicy", `{"policy_arn":"` + ownPolicyARN + `","tags":[{"key":"k","value":"v"}]}`, "", "Tags.member.1.Key", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.policy.untag", "UntagPolicy", `{"policy_arn":"` + ownPolicyARN + `","tag_keys":["k"]}`, "", "TagKeys.member.1", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.attached-policy.attach-to-user", "AttachUserPolicy", `{"user_name":"%s","policy_arn":"` + managedARN + `"}`, "user", "UserName", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.attached-policy.attach-to-group", "AttachGroupPolicy", `{"group_name":"%s","policy_arn":"` + managedARN + `"}`, "", "GroupName", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.attached-policy.attach-to-role", "AttachRolePolicy", `{"role_name":"%s","policy_arn":"` + managedARN + `"}`, "role", "RoleName", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.attached-policy.detach-from-user", "DetachUserPolicy", `{"user_name":"%s","policy_arn":"` + managedARN + `"}`, "user", "UserName", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.attached-policy.detach-from-group", "DetachGroupPolicy", `{"group_name":"%s","policy_arn":"` + managedARN + `"}`, "", "GroupName", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.attached-policy.detach-from-role", "DetachRolePolicy", `{"role_name":"%s","policy_arn":"` + managedARN + `"}`, "role", "RoleName", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.inline-policy.put-for-user", "PutUserPolicy", `{"user_name":"%s","policy_name":"p","policy_document":%q}`, "user", "UserName", capability.EffectUpdate, capability.IdempotencyIdempotent, false, true},
	{"aws.iam.inline-policy.put-for-group", "PutGroupPolicy", `{"group_name":"%s","policy_name":"p","policy_document":%q}`, "", "GroupName", capability.EffectUpdate, capability.IdempotencyIdempotent, false, true},
	{"aws.iam.inline-policy.put-for-role", "PutRolePolicy", `{"role_name":"%s","policy_name":"p","policy_document":%q}`, "role", "RoleName", capability.EffectUpdate, capability.IdempotencyIdempotent, false, true},
	{"aws.iam.inline-policy.delete-for-user", "DeleteUserPolicy", `{"user_name":"%s","policy_name":"p"}`, "user", "UserName", capability.EffectDelete, capability.IdempotencyNonIdempotent, false, false},
	{"aws.iam.inline-policy.delete-for-group", "DeleteGroupPolicy", `{"group_name":"%s","policy_name":"p"}`, "", "GroupName", capability.EffectDelete, capability.IdempotencyNonIdempotent, false, false},
	{"aws.iam.inline-policy.delete-for-role", "DeleteRolePolicy", `{"role_name":"%s","policy_name":"p"}`, "role", "RoleName", capability.EffectDelete, capability.IdempotencyNonIdempotent, false, false},
	{"aws.iam.user.put-permissions-boundary", "PutUserPermissionsBoundary", `{"user_name":"%s","permissions_boundary":"` + managedARN + `"}`, "user", "UserName", capability.EffectUpdate, capability.IdempotencyIdempotent, true, false},
	{"aws.iam.user.delete-permissions-boundary", "DeleteUserPermissionsBoundary", `{"user_name":"%s"}`, "user", "UserName", capability.EffectDelete, capability.IdempotencyNonIdempotent, false, false},
}

// iamPolicyArgs fills the identity name and the document of a tool's arguments.
func iamPolicyArgs(pattern, name, doc string) string {
	text, _ := json.Marshal(doc)
	pattern = strings.ReplaceAll(pattern, "%q", string(text))
	return strings.ReplaceAll(pattern, "%s", name)
}

func iamPolicyAnswer(r sent) reply {
	switch r.action {
	case "CreatePolicy":
		return iamAnswer(r.action, `<Policy><PolicyName>reader</PolicyName><Arn>`+ownPolicyARN+`</Arn></Policy>`)
	case "CreatePolicyVersion":
		return iamAnswer(r.action, `<PolicyVersion><VersionId>v2</VersionId><IsDefaultVersion>true</IsDefaultVersion></PolicyVersion>`)
	}
	return iamAnswer(r.action, "")
}

// Every policy tool is a listed-only write with confirmation, the effect and idempotency it is meant to have,
// the identity, policy ARN, and document checks it needs, and none of the excluded operations is a tool.
func TestIAMPolicyToolsAreListedOnlyWrites(t *testing.T) {
	for _, w := range iamPolicyTools {
		s := specOf(t, w.id)
		d := s.descriptor
		if !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired || d.Risk.Effect != w.effect ||
			!d.Risk.OpenWorld || d.Risk.Idempotency != w.idem || d.Risk.DataSensitivity == "" || s.action != w.action {
			t.Errorf("%s descriptor = %+v", w.id, d)
		}
		if (s.selfRole != "") != (w.self == "role") || (s.selfUser != "") != (w.self == "user") {
			t.Errorf("%s own-identity check role %q user %q, want %q", w.id, s.selfRole, s.selfUser, w.self)
		}
		if w.doc != (s.policyCheck == "identity-policy") || (w.doc && !strings.HasSuffix(s.policyArgs[0], "policy_document")) {
			t.Errorf("%s policy check %s on %v", w.id, s.policyCheck, s.policyArgs)
		}
		if w.arn != (s.targetCheck == "arn-account-or-managed-policy") {
			t.Errorf("%s target check %s on %v", w.id, s.targetCheck, s.targetArgs)
		}
		if w.arn && len(s.targetArgs) != 1 || w.arn && s.targetArgs[0] != "policy_arn" && s.targetArgs[0] != "permissions_boundary" {
			t.Errorf("%s target arguments %v", w.id, s.targetArgs)
		}
	}
	for _, s := range generatedSpecs {
		if slices.Contains(excludedIAMActions, s.action) {
			t.Errorf("%s sends %s, which is no tool", s.descriptor.ID, s.action)
		}
	}
}

// Each tool sends its action once with its arguments, returns the new policy or version where AWS does, and
// needs the confirmation.
func TestIAMPolicyToolsSend(t *testing.T) {
	for _, w := range iamPolicyTools {
		f := direct(func(r sent, _ int) reply {
			if r.action == w.action {
				return iamPolicyAnswer(r)
			}
			return reply{status: 500}
		})
		e := iamWriteEnv(t, f, w.id)
		args := iamPolicyArgs(w.args, "web", readerPolicy)
		before := len(f.requests)
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke(w.id, args); !errorsAs(err, &unconfirmed) || len(f.requests) != before {
			t.Errorf("%s unconfirmed: %v after %v", w.id, err, f.actions())
		}
		text, err := e.invokeConfirmed(w.id, args, true)
		if err != nil || f.count(w.action) != 1 {
			t.Errorf("%s: %v, requests %v", w.id, err, f.actions())
			continue
		}
		last := f.requests[len(f.requests)-1]
		if last.action != w.action || last.form.Get(w.formKey) == "" {
			t.Errorf("%s sent %s without %s: %v", w.id, last.action, w.formKey, last.form)
		}
		if w.doc && last.form.Get("PolicyDocument") != readerPolicy {
			t.Errorf("%s did not send the document", w.id)
		}
		got := decodeResult(t, text)
		switch w.action {
		case "CreatePolicy":
			if policy, _ := got["policy"].(map[string]any); policy["policy_name"] != "reader" {
				t.Errorf("%s result = %v", w.id, got)
			}
		case "CreatePolicyVersion":
			if version, _ := got["policy_version"].(map[string]any); version["version_id"] != "v2" {
				t.Errorf("%s result = %v", w.id, got)
			}
		}
	}
}

// IAM errors keep their class: a missing entity is not found, an existing one and a delete conflict are
// conflicts, a quota is a limit, and a document AWS cannot parse is an invalid request of AWS.
func TestIAMPolicyWriteErrors(t *testing.T) {
	for _, c := range []struct {
		tool, code, text string
		status           int
		class            provider.Class
	}{
		{"aws.iam.attached-policy.attach-to-group", "NoSuchEntity", "not found", 404, provider.ClassNotFound},
		{"aws.iam.policy.create", "EntityAlreadyExists", "conflict", 409, provider.ClassProviderError},
		{"aws.iam.policy.create", "LimitExceeded", "limit or quota", 409, provider.ClassProviderError},
		{"aws.iam.policy.create", "MalformedPolicyDocument", "invalid", 400, provider.ClassProviderError},
		{"aws.iam.inline-policy.put-for-group", "MalformedPolicyDocument", "invalid", 400, provider.ClassProviderError},
		{"aws.iam.policy.delete", "DeleteConflict", "conflict", 409, provider.ClassProviderError},
	} {
		i := slices.IndexFunc(iamPolicyTools, func(x iamPolicyCase) bool { return x.id == c.tool })
		w := iamPolicyTools[i]
		f := direct(func(r sent, _ int) reply { return reply{status: c.status, body: iamErrorFor(c.code)} })
		e := iamWriteEnv(t, f, c.tool)
		_, err := e.invokeConfirmed(c.tool, iamPolicyArgs(w.args, "web", readerPolicy), true)
		var failed *provider.Error
		if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil || failed.Original.Entries[0].Code != c.code ||
			f.count(w.action) != 1 || (c.class == provider.ClassProviderError && !strings.Contains(failed.Message, c.text)) {
			t.Errorf("%s %s: %v after %v", c.tool, c.code, err, f.actions())
		}
	}
}

// A tool that attaches to, detaches from, or writes policies of a user or role refuses the identity whose
// credentials sign the call, in any case, and sends nothing for it. A group, another identity of the same
// kind, and the other kind are not affected. A failed or unreadable identity refuses the call.
func TestIAMPolicyOwnIdentityIsRefused(t *testing.T) {
	userCaller := func(arn string) string {
		return strings.Replace(identityXML, "arn:aws:iam::123456789012:user/Alice", arn, 1)
	}
	for _, w := range iamPolicyTools {
		run := func(identity func(int) reply, name string) (*fakeAWS, error) {
			f := &fakeAWS{answer: func(r sent, n int) reply {
				switch r.action {
				case "GetCallerIdentity":
					return identity(n)
				case w.action:
					return iamPolicyAnswer(r)
				}
				return reply{status: 500}
			}}
			e := iamWriteEnv(t, f, w.id)
			_, err := e.invokeConfirmed(w.id, iamPolicyArgs(w.args, name, readerPolicy), true)
			return f, err
		}
		always := func(body string) func(int) reply { return func(int) reply { return ok(body) } }
		if w.self == "" {
			if strings.Contains(w.args, "%s") {
				// A group has no own identity: even the name of the caller passes.
				if f, err := run(always(identityXML), "Alice"); err != nil || f.count(w.action) != 1 {
					t.Errorf("%s on a group: %v, requests %v", w.id, err, f.actions())
				}
			}
			continue
		}
		own, other, otherKind := "Alice", "Deploy", ownRoleIdentity
		callers := []string{identityXML, userCaller("arn:aws:iam::123456789012:user/ops/team/Alice")}
		if w.self == "role" {
			own, other, otherKind = "Deploy", "web", identityXML
			callers = []string{ownRoleIdentity}
		}
		for _, caller := range callers {
			for _, name := range []string{own, strings.ToLower(own), strings.ToUpper(own)} {
				f, err := run(always(caller), name)
				if !isInvalidRequest(err) || f.count(w.action) != 0 || strings.Contains(err.Error(), name) {
					t.Errorf("%s on the own %s %s: %v, requests %v", w.id, w.self, name, err, f.actions())
				}
			}
			if f, err := run(always(caller), "web"); err != nil || f.count(w.action) != 1 {
				t.Errorf("%s on another %s: %v, requests %v", w.id, w.self, err, f.actions())
			}
		}
		// The other kind of caller has no own identity of this kind.
		if f, err := run(always(otherKind), other); err != nil || f.count(w.action) != 1 {
			t.Errorf("%s with the other kind of caller: %v, requests %v", w.id, err, f.actions())
		}
		root := strings.Replace(identityXML, "user/Alice", "root", 1)
		if f, err := run(always(root), own); err != nil || f.count(w.action) != 1 {
			t.Errorf("%s as root: %v, requests %v", w.id, err, f.actions())
		}
		denied := func(n int) reply {
			if n == 1 {
				return ok(identityXML)
			}
			return reply{status: 403, body: errorXML("AccessDenied")}
		}
		if f, err := run(denied, "web"); err == nil || f.count(w.action) != 0 {
			t.Errorf("%s with a failing identity: %v, requests %v", w.id, err, f.actions())
		}
		for _, arn := range []string{"arn:aws:iam::123456789012:user/", "arn:aws:iam::123456789012:user/a/",
			"arn:aws:sts::123456789012:federated-user/bob", "arn:aws:iam::210987654321:user/Alice", "not-an-arn"} {
			if f, err := run(always(userCaller(arn)), "web"); classOf(err) != provider.ClassInvalidResponse || f.count(w.action) != 0 {
				t.Errorf("%s with caller %s: %v, requests %v", w.id, arn, err, f.actions())
			}
		}
	}
}

// A policy ARN of another account or partition is refused before any request, an AWS managed policy and a
// policy of the bound account pass; the permissions boundary of a user is held to the same rule.
func TestIAMPolicyARNIsChecked(t *testing.T) {
	for _, w := range iamPolicyTools {
		if !w.arn {
			continue
		}
		key := `"policy_arn":"`
		if strings.Contains(w.args, `"permissions_boundary":"`) {
			key = `"permissions_boundary":"`
		}
		original := key + managedARN
		if !strings.Contains(w.args, original) {
			original = key + ownPolicyARN
		}
		f := direct(func(r sent, _ int) reply { return iamPolicyAnswer(r) })
		e := iamWriteEnv(t, f, w.id)
		for arn, allowed := range map[string]bool{
			managedARN: true,
			"arn:aws:iam::aws:policy/job-function/Billing": true,
			ownPolicyARN: true,
			"arn:aws:iam::210987654321:policy/reader":        false,
			"arn:aws-cn:iam::aws:policy/ReadOnlyAccess":      false,
			"arn:aws-us-gov:iam::123456789012:policy/reader": false,
			"arn:aws:iam::aws:role/ReadOnlyAccess":           false,
			"arn:aws:s3:::bucket":                            false,
		} {
			before := len(f.requests)
			args := strings.Replace(iamPolicyArgs(w.args, "web", readerPolicy), original, key+arn, 1)
			_, err := e.invokeConfirmed(w.id, args, true)
			if allowed != (err == nil) || (!allowed && (!isInvalidRequest(err) || len(f.requests) != before || strings.Contains(err.Error(), "210987654321"))) {
				t.Errorf("%s with %s: %v, requests %v", w.id, arn, err, f.actions())
			}
		}
	}
}

// A statement with Principal or NotPrincipal is never sent, in any tool that carries a document.
func TestIAMPolicyDocumentWithPrincipalIsRefused(t *testing.T) {
	for _, w := range iamPolicyTools {
		if !w.doc {
			continue
		}
		f := direct(func(r sent, _ int) reply { return iamPolicyAnswer(r) })
		e := iamWriteEnv(t, f, w.id)
		for _, element := range []string{"Principal", "NotPrincipal"} {
			doc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","` + element + `":"*","Action":"s3:GetObject","Resource":"*"}]}`
			_, err := e.invokeConfirmed(w.id, iamPolicyArgs(w.args, "web", doc), true)
			if !isInvalidRequest(err) || len(f.requests) != 0 || !strings.Contains(err.Error(), "policy_document: ") {
				t.Errorf("%s with %s: %v, requests %v", w.id, element, err, f.actions())
			}
		}
	}
}

// The identity policy check admits a well-formed document without a principal, and refuses everything else
// naming the rule and the statement position, never a value.
func TestIdentityPolicyCheck(t *testing.T) {
	b := binding{account: testAccount, regions: []string{testRegion}}
	stmt := `{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::secretbucket/*"}`
	doc := func(statements ...string) string {
		return `{"Version":"2012-10-17","Statement":[` + strings.Join(statements, ",") + `]}`
	}
	for _, c := range []struct {
		name, doc, secret string
		ok                bool
	}{
		{"one statement", doc(stmt), "", true},
		{"statement object", `{"Version":"2012-10-17","Statement":` + stmt + `}`, "", true},
		{"wildcards", doc(`{"Effect":"Allow","Action":"*","Resource":"*"}`), "", true},
		{"foreign account resource", doc(`{"Effect":"Allow","Action":"s3:*","Resource":"arn:aws:s3:::x","Condition":{"StringEquals":{"aws:PrincipalAccount":"210987654321"}}}`), "", true},
		{"id and sid", `{"Version":"2012-10-17","Id":"i","Statement":[{"Sid":"s","Effect":"Deny","NotAction":"s3:*","NotResource":"*"}]}`, "", true},
		{"principal", doc(`{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::210987654321:root"},"Action":"s3:*","Resource":"*"}`), "210987654321", false},
		{"principal wildcard", doc(`{"Effect":"Allow","Principal":"*","Action":"s3:*","Resource":"*"}`), "", false},
		{"not principal", doc(`{"Effect":"Allow","NotPrincipal":{"Service":"secretservice.amazonaws.com"},"Action":"s3:*","Resource":"*"}`), "secretservice", false},
		{"second statement has a principal", doc(stmt, `{"Effect":"Allow","Principal":"*","Action":"s3:*","Resource":"*"}`), "", false},
		{"principal in other case", doc(`{"Effect":"Allow","principal":"*","Action":"s3:*","Resource":"*"}`), "", false},
		{"unknown element", doc(`{"Effect":"Allow","Extra":1,"Action":"s3:*","Resource":"*"}`), "", false},
		{"duplicate principal", doc(`{"Effect":"Allow","Principal":"*","Principal":{"AWS":"x"},"Action":"s3:*","Resource":"*"}`), "", false},
		{"duplicate top key", `{"Version":"2012-10-17","Statement":[],"Statement":[` + stmt + `]}`, "", false},
		{"no version", `{"Statement":[` + stmt + `]}`, "", false},
		{"version not a string", `{"Version":2012,"Statement":[` + stmt + `]}`, "", false},
		{"no statement", `{"Version":"2012-10-17"}`, "", false},
		{"empty statement list", `{"Version":"2012-10-17","Statement":[]}`, "", false},
		{"statement not an object", `{"Version":"2012-10-17","Statement":["secretbucket"]}`, "secretbucket", false},
		{"top key in other case", `{"version":"2012-10-17","Statement":[` + stmt + `]}`, "", false},
		{"not an object", `["secretbucket"]`, "secretbucket", false},
		{"invalid JSON", `{"Version":`, "", false},
		{"trailing data", doc(stmt) + `{}`, "", false},
		{"empty", ``, "", false},
		{"too long", `{"Version":"2012-10-17","Id":"` + strings.Repeat("x", maxIdentityPolicyChars) + `","Statement":[` + stmt + `]}`, "xxxxxxxx", false},
		{"invalid UTF-8", "{\"Version\":\"\xff\",\"Statement\":[" + stmt + "]}", "", false},
	} {
		err := checkIdentityPolicy(b, "policy_document", c.doc)
		if c.ok != (err == nil) {
			t.Errorf("%s: err = %v, want ok %v", c.name, err, c.ok)
			continue
		}
		if err == nil {
			continue
		}
		if !isInvalidRequest(err) || !strings.HasPrefix(err.Error(), "policy_document: ") {
			t.Errorf("%s: %T %v, want an invalid request that names the argument", c.name, err, err)
		}
		for _, leaked := range []string{c.secret, testAccount, "s3:GetObject"} {
			if leaked != "" && strings.Contains(err.Error(), leaked) {
				t.Errorf("%s: the refusal quotes %q: %v", c.name, leaked, err)
			}
		}
	}
	if err := checkIdentityPolicy(b, "d", doc(stmt, `{"Effect":"Allow","Principal":"*"}`)); err == nil || !strings.Contains(err.Error(), "statement 2") {
		t.Errorf("the refusal names no position: %v", err)
	}
	// Exactly the modelled length passes the length rule.
	pad := maxIdentityPolicyChars - len(`{"Version":"2012-10-17","Id":"","Statement":[`+stmt+`]}`)
	if err := checkIdentityPolicy(b, "d", `{"Version":"2012-10-17","Id":"`+strings.Repeat("x", pad)+`","Statement":[`+stmt+`]}`); err != nil {
		t.Errorf("a document at the modelled limit is refused: %v", err)
	}
}

// ownIdentity reads the user name as the last segment of the user path, and the role of a session.
func TestOwnIdentity(t *testing.T) {
	for arn, want := range map[string][2]string{
		"arn:aws:iam::123456789012:user/Alice":            {"", "Alice"},
		"arn:aws:iam::123456789012:user/ops/team/Alice":   {"", "Alice"},
		"arn:aws:sts::123456789012:assumed-role/Deploy/s": {"Deploy", ""},
		"arn:aws:iam::123456789012:root":                  {"", ""},
	} {
		role, user, ok := ownIdentity(arn, testAccount)
		if !ok || role != want[0] || user != want[1] {
			t.Errorf("ownIdentity(%s) = %q, %q, %v", arn, role, user, ok)
		}
	}
	for _, arn := range []string{"arn:aws:iam::123456789012:user/", "arn:aws:iam::123456789012:user/ops/",
		"arn:aws:iam::210987654321:user/Alice", "arn:aws-cn:iam::123456789012:user/Alice", "arn:aws:iam::123456789012:group/g"} {
		if _, _, ok := ownIdentity(arn, testAccount); ok {
			t.Errorf("ownIdentity(%s) is readable", arn)
		}
	}
}

// A policy tool is offered only by a connection whose tools list names it exactly: not by effect, not by a
// prefix, and not by naming another tool.
func TestIAMPolicyToolsNeedTheirExactName(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	for name, c := range map[string]config.Connection{
		"by every effect": {Effects: all},
		"by default":      {},
		"by prefix": {Tools: []string{"aws.iam.policy.*", "aws.iam.policy-version.*", "aws.iam.attached-policy.*",
			"aws.iam.inline-policy.*", "aws.iam.user.*"}},
		"by another tool": {Tools: []string{"aws.iam.policy.get"}},
	} {
		f := direct(nil)
		e := newConnEnv(t, f, baseValues(), c)
		for _, w := range iamPolicyTools {
			_, err := e.invokeConfirmed(w.id, iamPolicyArgs(w.args, "web", readerPolicy), true)
			var notOffered *capability.UnsupportedError
			if !errorsAs(err, &notOffered) || len(f.requests) != 0 {
				t.Errorf("%s %s: %v, requests %v", name, w.id, err, f.actions())
			}
		}
		found, err := e.core.Search(application.SearchRequest{Provider: Provider, Group: "iam", Connection: "prod"})
		if err != nil {
			t.Fatalf("%s: search: %v", name, err)
		}
		for _, hit := range found.Operations {
			if hit.Effect != capability.EffectRead {
				t.Errorf("%s offers %s", name, hit.ID)
			}
		}
	}
}
