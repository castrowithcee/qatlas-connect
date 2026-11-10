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

// iamWriteTools are the IAM tools that create, change, or delete roles and instance profiles, with their
// effect and idempotency, and whether they refuse the connection's own role.
var iamWriteTools = []struct {
	id      string
	effect  capability.Effect
	idem    capability.Idempotency
	ownRole bool
}{
	{"aws.iam.role.create", capability.EffectCreate, capability.IdempotencyNonIdempotent, false},
	{"aws.iam.role.update", capability.EffectUpdate, capability.IdempotencyIdempotent, true},
	{"aws.iam.role.update-trust-policy", capability.EffectUpdate, capability.IdempotencyIdempotent, true},
	{"aws.iam.role.put-permissions-boundary", capability.EffectUpdate, capability.IdempotencyIdempotent, true},
	{"aws.iam.role.delete-permissions-boundary", capability.EffectDelete, capability.IdempotencyNonIdempotent, true},
	{"aws.iam.role.tag", capability.EffectUpdate, capability.IdempotencyIdempotent, true},
	{"aws.iam.role.untag", capability.EffectUpdate, capability.IdempotencyIdempotent, true},
	{"aws.iam.role.delete", capability.EffectDelete, capability.IdempotencyNonIdempotent, true},
	{"aws.iam.instance-profile.create", capability.EffectCreate, capability.IdempotencyNonIdempotent, false},
	{"aws.iam.instance-profile.delete", capability.EffectDelete, capability.IdempotencyNonIdempotent, false},
	{"aws.iam.instance-profile.add-role", capability.EffectUpdate, capability.IdempotencyNonIdempotent, false},
	{"aws.iam.instance-profile.remove-role", capability.EffectUpdate, capability.IdempotencyNonIdempotent, true},
}

// The operations that stay out of the role tools: service-linked roles, the deprecated description update,
// and every policy attachment or inline policy write.
var iamRoleOmitted = []string{"CreateServiceLinkedRole", "DeleteServiceLinkedRole", "UpdateRoleDescription",
	"AttachRolePolicy", "DetachRolePolicy", "PutRolePolicy", "DeleteRolePolicy", "CreatePolicy", "CreatePolicyVersion"}

const (
	ownRoleIdentity = `<GetCallerIdentityResponse><GetCallerIdentityResult><Arn>arn:aws:sts::123456789012:assumed-role/Deploy/qatlas-x</Arn>` +
		`<UserId>AROAEXAMPLEEXAMPLE1:qatlas-x</UserId><Account>123456789012</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`
	ec2Trust = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
)

func trustArgs(doc string) string {
	text, _ := json.Marshal(doc)
	return `{"role_name":"web","assume_role_policy_document":` + string(text) + `}`
}

func specOf(t *testing.T, id string) toolSpec {
	t.Helper()
	for _, s := range generatedSpecs {
		if s.descriptor.ID == id {
			return s
		}
	}
	t.Fatalf("no tool %s", id)
	return toolSpec{}
}

// Every role tool is a listed-only write with confirmation, the effect and own-role check it is meant to have,
// and the trust and boundary checks on the arguments that carry a trust policy or a boundary. The operations
// left out are no tools.
func TestIAMRoleToolsAreListedOnlyWrites(t *testing.T) {
	for _, w := range iamWriteTools {
		s := specOf(t, w.id)
		d := s.descriptor
		if !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired || d.Risk.Effect != w.effect ||
			!d.Risk.OpenWorld || d.Risk.Idempotency != w.idem || d.Risk.DataSensitivity == "" {
			t.Errorf("%s descriptor = %+v", w.id, d)
		}
		if (s.selfRole == "role_name") != w.ownRole || (s.selfRole != "" && s.selfRole != "role_name") {
			t.Errorf("%s own-role check on %q", w.id, s.selfRole)
		}
	}
	for id, want := range map[string]string{
		"aws.iam.role.create":              "assume_role_policy_document",
		"aws.iam.role.update-trust-policy": "assume_role_policy_document",
	} {
		if s := specOf(t, id); s.policyCheck != "trust-policy" || !slices.Equal(s.policyArgs, []string{want}) {
			t.Errorf("%s policy check %s on %v", id, s.policyCheck, s.policyArgs)
		}
	}
	for _, id := range []string{"aws.iam.role.create", "aws.iam.role.put-permissions-boundary"} {
		if s := specOf(t, id); s.targetCheck != "arn-account-or-managed-policy" || !slices.Equal(s.targetArgs, []string{"permissions_boundary"}) {
			t.Errorf("%s target check %s on %v", id, s.targetCheck, s.targetArgs)
		}
	}
	for _, s := range generatedSpecs {
		if s.service == "iam" && s.descriptor.Risk.Effect != capability.EffectRead && !s.descriptor.RequiresToolAllowList {
			t.Errorf("%s writes IAM without the allow-list", s.descriptor.ID)
		}
		if slices.Contains(iamRoleOmitted, s.action) || slices.Contains(excludedIAMActions, s.action) {
			t.Errorf("%s sends %s, which is no tool", s.descriptor.ID, s.action)
		}
	}
}

// The trust check admits only principals of the bound account and AWS service principals, in every statement,
// and refuses everything else before any request, without quoting the document.
func TestTrustPolicyCheck(t *testing.T) {
	b := binding{account: testAccount, regions: []string{testRegion}}
	stmt := func(effect, principal string) string {
		return `{"Effect":"` + effect + `","Principal":` + principal + `,"Action":"sts:AssumeRole"}`
	}
	doc := func(statements ...string) string {
		return `{"Version":"2012-10-17","Statement":[` + strings.Join(statements, ",") + `]}`
	}
	allow := func(principal string) string { return doc(stmt("Allow", principal)) }
	for _, c := range []struct {
		name, doc, secret string
		ok                bool
	}{
		{"own account as ID", allow(`{"AWS":"` + testAccount + `"}`), "", true},
		{"own root", allow(`{"AWS":"arn:aws:iam::` + testAccount + `:root"}`), "", true},
		{"own role", allow(`{"AWS":"arn:aws:iam::` + testAccount + `:role/ops/deployer"}`), "", true},
		{"own user", allow(`{"AWS":"arn:aws:iam::` + testAccount + `:user/alice"}`), "", true},
		{"ec2", allow(`{"Service":"ec2.amazonaws.com"}`), "", true},
		{"lambda list", allow(`{"Service":["lambda.amazonaws.com","ec2.amazonaws.com"]}`), "", true},
		{"single statement object", `{"Statement":` + stmt("Allow", `{"Service":"ec2.amazonaws.com"}`) + `}`, "", true},
		{"own account and service", allow(`{"AWS":["` + testAccount + `"],"Service":"ec2.amazonaws.com"}`), "", true},

		{"foreign account", allow(`{"AWS":"arn:aws:iam::210987654321:root"}`), "210987654321", false},
		{"foreign account ID", allow(`{"AWS":"210987654321"}`), "210987654321", false},
		{"wildcard string", allow(`"*"`), "", false},
		{"AWS wildcard", allow(`{"AWS":"*"}`), "", false},
		{"wildcard in ARN", allow(`{"AWS":"arn:aws:iam::` + testAccount + `:role/*"}`), "role/*", false},
		{"question mark in ARN", allow(`{"AWS":"arn:aws:iam::` + testAccount + `:role/a?"}`), "role/a?", false},
		{"federated", allow(`{"Federated":"accounts.google.com"}`), "accounts.google.com", false},
		{"canonical user", allow(`{"CanonicalUser":"79a59df900b949e55d96a1e698fbacedfd6e09d98eacf8f8d5218e7cd47ef2be"}`), "79a59df9", false},
		{"not principal", doc(`{"Effect":"Allow","NotPrincipal":{"AWS":"arn:aws:iam::210987654321:root"},"Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}`), "210987654321", false},
		{"unique ID", allow(`{"AWS":"AROAEXAMPLEEXAMPLE123"}`), "AROAEXAMPLE", false},
		{"user unique ID", allow(`{"AWS":"AIDAEXAMPLEEXAMPLE123"}`), "AIDAEXAMPLE", false},
		{"aws-cn ARN", allow(`{"AWS":"arn:aws-cn:iam::` + testAccount + `:root"}`), "aws-cn", false},
		{"aws-us-gov ARN", allow(`{"AWS":"arn:aws-us-gov:iam::` + testAccount + `:root"}`), "aws-us-gov", false},
		{"sts ARN", allow(`{"AWS":"arn:aws:sts::` + testAccount + `:assumed-role/r/s"}`), "assumed-role", false},
		{"other resource", allow(`{"AWS":"arn:aws:iam::` + testAccount + `:group/admins"}`), "group/admins", false},
		{"service without amazonaws.com", allow(`{"Service":"ec2.example.com"}`), "ec2.example.com", false},
		{"service in upper case", allow(`{"Service":"EC2.amazonaws.com"}`), "EC2.amazonaws.com", false},
		{"service of another partition", allow(`{"Service":"ec2.amazonaws.com.cn"}`), "amazonaws.com.cn", false},
		{"service wildcard", allow(`{"Service":"*.amazonaws.com"}`), "", false},
		{"no principal", doc(`{"Effect":"Allow","Action":"sts:AssumeRole"}`), "", false},
		{"empty principal", allow(`{}`), "", false},
		{"empty AWS list", allow(`{"AWS":[]}`), "", false},
		{"principal number", allow(`{"AWS":123456789012}`), "", false},
		{"invalid JSON", `{"Statement":[`, "", false},
		{"trailing data", allow(`{"Service":"ec2.amazonaws.com"}`) + `{}`, "", false},
		{"not an object", `["Statement"]`, "", false},
		{"no statement", `{"Version":"2012-10-17"}`, "", false},
		{"empty statement list", `{"Statement":[]}`, "", false},
		{"statement not an object", `{"Statement":["x"]}`, "", false},
		{"one valid, one invalid", doc(stmt("Allow", `{"Service":"ec2.amazonaws.com"}`), stmt("Allow", `{"AWS":"arn:aws:iam::210987654321:root"}`)), "210987654321", false},
		{"deny with a foreign principal", doc(stmt("Allow", `{"Service":"ec2.amazonaws.com"}`), stmt("Deny", `{"AWS":"arn:aws:iam::210987654321:root"}`)), "210987654321", false},
		{"too long", `{"Id":"` + strings.Repeat("x", maxTrustPolicyChars) + `","Statement":[` + stmt("Allow", `{"Service":"ec2.amazonaws.com"}`) + `]}`, "xxxxxxxx", false},
		{"duplicate principal", doc(`{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Principal":{"AWS":"arn:aws:iam::210987654321:root"},"Action":"sts:AssumeRole"}`), "210987654321", false},
		{"duplicate principal key", allow(`{"AWS":"` + testAccount + `","AWS":"arn:aws:iam::210987654321:root"}`), "210987654321", false},
		{"duplicate statement", `{"Statement":[` + stmt("Allow", `{"Service":"ec2.amazonaws.com"}`) + `],"Statement":[]}`, "", false},
		{"escaped duplicate", doc(`{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Principal":{"AWS":"*"},"Action":"sts:AssumeRole"}`), "", false},
		{"principal in lower case", doc(`{"Effect":"Allow","principal":{"AWS":"arn:aws:iam::210987654321:root"},"Action":"sts:AssumeRole"}`), "210987654321", false},
		{"principal case beside principal", doc(`{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"PRINCIPAL":{"AWS":"*"},"Action":"sts:AssumeRole"}`), "", false},
		{"not principal in other case", doc(`{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"notprincipal":{"AWS":"*"},"Action":"sts:AssumeRole"}`), "", false},
		{"principal key in other case", allow(`{"aws":"arn:aws:iam::210987654321:root"}`), "210987654321", false},
		{"service key in other case", allow(`{"service":"ec2.amazonaws.com"}`), "", false},
		{"statement key in other case", `{"statement":[` + stmt("Allow", `{"AWS":"*"}`) + `]}`, "", false},
		{"unknown statement element", doc(`{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Extra":1,"Action":"sts:AssumeRole"}`), "", false},
	} {
		err := checkTrustPolicy(b, "assume_role_policy_document", c.doc)
		if c.ok != (err == nil) {
			t.Errorf("%s: err = %v, want ok %v", c.name, err, c.ok)
			continue
		}
		if err == nil {
			continue
		}
		var invalid *provider.InvalidRequestError
		if !errorsAs(err, &invalid) || !strings.HasPrefix(err.Error(), "assume_role_policy_document: ") {
			t.Errorf("%s: %T %v, want an invalid request that names the argument", c.name, err, err)
		}
		for _, leaked := range []string{c.secret, testAccount, "ec2.amazonaws.com", "sts:AssumeRole"} {
			if leaked != "" && strings.Contains(err.Error(), leaked) {
				t.Errorf("%s: the refusal quotes %q: %v", c.name, leaked, err)
			}
		}
	}
	if err := checkTrustPolicy(b, "d", doc(stmt("Allow", `{"Service":"ec2.amazonaws.com"}`), stmt("Allow", `{"AWS":"*"}`))); err == nil ||
		!strings.Contains(err.Error(), "statement 2") {
		t.Errorf("the refusal names no position: %v", err)
	}
}

// iamWriteEnv offers exactly the listed tools.
func iamWriteEnv(t *testing.T, f *fakeAWS, tools ...string) *env {
	t.Helper()
	return newToolsEnv(t, f, baseValues(), tools)
}

// CreateRole sends the checked trust policy and returns the role; a refused trust policy sends nothing; the
// conflicts and limits of AWS keep their class and code; the call is not repeated.
func TestIAMCreateRole(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		if r.action == "CreateRole" {
			return iamAnswer("CreateRole", `<Role>`+iamRoleXML(`%7B%7D`)+`</Role>`)
		}
		return reply{status: 500}
	})
	e := iamWriteEnv(t, f, "aws.iam.role.create")
	text, err := e.invokeConfirmed("aws.iam.role.create", trustArgs(ec2Trust), true)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	role, _ := got["role"].(map[string]any)
	if role["role_name"] != "r" || role["assume_role_policy_document"] != "{}" || got["truncated"] != false {
		t.Errorf("result = %v", got)
	}
	if f.count("CreateRole") != 1 || f.requests[len(f.requests)-1].form.Get("AssumeRolePolicyDocument") != ec2Trust {
		t.Errorf("requests = %v", f.requests)
	}

	// Without confirmation the core refuses before the handler runs.
	before := len(f.requests)
	var unconfirmed *application.ConfirmationRequiredError
	if _, err := e.invoke("aws.iam.role.create", trustArgs(ec2Trust)); !errorsAs(err, &unconfirmed) || len(f.requests) != before {
		t.Errorf("unconfirmed: %v after %d requests", err, len(f.requests)-before)
	}

	// A trust policy the check refuses is never sent: not even the account check runs.
	f2 := direct(nil)
	e2 := iamWriteEnv(t, f2, "aws.iam.role.create", "aws.iam.role.update-trust-policy")
	foreign := `{"Statement":{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::210987654321:root"},"Action":"sts:AssumeRole"}}`
	for _, id := range []string{"aws.iam.role.create", "aws.iam.role.update-trust-policy"} {
		if _, err := e2.invokeConfirmed(id, trustArgs(foreign), true); !isInvalidRequest(err) || len(f2.requests) != 0 ||
			strings.Contains(err.Error(), "210987654321") {
			t.Errorf("%s with a foreign principal: %v, requests %v", id, err, f2.actions())
		}
	}
	// A boundary of another account is refused the same way.
	if _, err := e2.invokeConfirmed("aws.iam.role.create", strings.TrimSuffix(trustArgs(ec2Trust), "}")+
		`,"permissions_boundary":"arn:aws:iam::210987654321:policy/pb"}`, true); !isInvalidRequest(err) || len(f2.requests) != 0 {
		t.Errorf("foreign boundary: %v, requests %v", err, f2.actions())
	}

	for code, c := range map[string]struct {
		status int
		text   string
	}{
		"EntityAlreadyExists": {409, "conflict"},
		"LimitExceeded":       {409, "limit or quota"},
	} {
		f := direct(func(r sent, _ int) reply { return reply{status: c.status, body: iamErrorFor(code)} })
		e := iamWriteEnv(t, f, "aws.iam.role.create")
		_, err := e.invokeConfirmed("aws.iam.role.create", trustArgs(ec2Trust), true)
		var failed *provider.Error
		if !errorsAs(err, &failed) || failed.Class != provider.ClassProviderError || !strings.Contains(failed.Message, c.text) ||
			failed.Original == nil || failed.Original.Entries[0].Code != code || f.count("CreateRole") != 1 {
			t.Errorf("%s: %v after %d requests", code, err, f.count("CreateRole"))
		}
	}
}

// UpdateAssumeRolePolicy passes a malformed document AWS refuses as an invalid request of AWS; DeleteRole keeps
// the conflict of a role that still has attachments.
func TestIAMRoleWriteErrors(t *testing.T) {
	for _, c := range []struct {
		tool, action, args, code, text string
		status                         int
	}{
		{"aws.iam.role.update-trust-policy", "UpdateAssumeRolePolicy", trustArgs(ec2Trust), "MalformedPolicyDocument", "invalid", 400},
		{"aws.iam.role.delete", "DeleteRole", `{"role_name":"web"}`, "DeleteConflict", "conflict", 409},
		{"aws.iam.instance-profile.delete", "DeleteInstanceProfile", `{"instance_profile_name":"web"}`, "DeleteConflict", "conflict", 409},
	} {
		f := direct(func(r sent, _ int) reply {
			if r.action == c.action {
				return reply{status: c.status, body: iamErrorFor(c.code)}
			}
			return reply{status: 500}
		})
		e := iamWriteEnv(t, f, c.tool)
		_, err := e.invokeConfirmed(c.tool, c.args, true)
		var failed *provider.Error
		if !errorsAs(err, &failed) || failed.Class != provider.ClassProviderError || !strings.Contains(failed.Message, c.text) ||
			failed.Original == nil || failed.Original.Entries[0].Code != c.code || f.count(c.action) != 1 {
			t.Errorf("%s %s: %v after %v", c.tool, c.code, err, f.actions())
		}
	}
}

// A tool that changes a role refuses the role whose session signs the call, in any case, and sends nothing
// for it. A failed or unreadable identity refuses the call. An IAM user has no own role.
func TestIAMOwnRoleIsNeverChanged(t *testing.T) {
	args := map[string]string{
		"aws.iam.role.update":                      `{"role_name":"%s","description":"d"}`,
		"aws.iam.role.update-trust-policy":         strings.Replace(trustArgs(ec2Trust), `"web"`, `"%s"`, 1),
		"aws.iam.role.put-permissions-boundary":    `{"role_name":"%s","permissions_boundary":"arn:aws:iam::aws:policy/ReadOnlyAccess"}`,
		"aws.iam.role.delete-permissions-boundary": `{"role_name":"%s"}`,
		"aws.iam.role.tag":                         `{"role_name":"%s","tags":[{"key":"k","value":"v"}]}`,
		"aws.iam.role.untag":                       `{"role_name":"%s","tag_keys":["k"]}`,
		"aws.iam.role.delete":                      `{"role_name":"%s"}`,
		"aws.iam.instance-profile.remove-role":     `{"instance_profile_name":"p","role_name":"%s"}`,
	}
	for _, w := range iamWriteTools {
		if !w.ownRole {
			if _, has := args[w.id]; has {
				t.Errorf("%s has own-role arguments but no own-role check", w.id)
			}
			continue
		}
		pattern, has := args[w.id]
		if !has {
			t.Fatalf("no arguments for %s", w.id)
		}
		action := specOf(t, w.id).action
		run := func(identity func(n int) reply, role string) (*fakeAWS, error) {
			f := &fakeAWS{answer: func(r sent, n int) reply {
				switch r.action {
				case "GetCallerIdentity":
					return identity(n)
				case action:
					return iamAnswer(action, "")
				}
				return reply{status: 500}
			}}
			e := iamWriteEnv(t, f, w.id)
			_, err := e.invokeConfirmed(w.id, strings.Replace(pattern, "%s", role, 1), true)
			return f, err
		}
		always := func(body string) func(int) reply { return func(int) reply { return ok(body) } }

		for _, role := range []string{"Deploy", "deploy", "DEPLOY"} {
			f, err := run(always(ownRoleIdentity), role)
			if !isInvalidRequest(err) || f.count(action) != 0 || strings.Contains(err.Error(), role) {
				t.Errorf("%s on the own role %s: %v, requests %v", w.id, role, err, f.actions())
			}
		}
		if f, err := run(always(ownRoleIdentity), "web"); err != nil || f.count(action) != 1 {
			t.Errorf("%s on another role: %v, requests %v", w.id, err, f.actions())
		}
		if f, err := run(always(identityXML), "Deploy"); err != nil || f.count(action) != 1 {
			t.Errorf("%s as an IAM user: %v, requests %v", w.id, err, f.actions())
		}
		root := strings.Replace(identityXML, "user/Alice", "root", 1)
		if f, err := run(always(root), "Deploy"); err != nil || f.count(action) != 1 {
			t.Errorf("%s as root: %v, requests %v", w.id, err, f.actions())
		}
		// The account check passes, the own-role check's identity call fails: refused, nothing sent.
		denied := func(n int) reply {
			if n == 1 {
				return ok(identityXML)
			}
			return reply{status: 403, body: errorXML("AccessDenied")}
		}
		if f, err := run(denied, "web"); err == nil || f.count(action) != 0 {
			t.Errorf("%s with a failing identity: %v, requests %v", w.id, err, f.actions())
		}
		for _, arn := range []string{
			"arn:aws:sts::123456789012:federated-user/bob",
			"arn:aws:sts::123456789012:assumed-role/Deploy",
			"arn:aws:sts::210987654321:assumed-role/web/s",
			"arn:aws-cn:sts::123456789012:assumed-role/x/s",
			"not-an-arn",
		} {
			odd := strings.Replace(identityXML, "arn:aws:iam::123456789012:user/Alice", arn, 1)
			if f, err := run(always(odd), "web"); classOf(err) != provider.ClassInvalidResponse || f.count(action) != 0 {
				t.Errorf("%s with caller %s: %v, requests %v", w.id, arn, err, f.actions())
			}
		}
	}
}

// ownRoleName reads only the forms it knows.
func TestOwnRoleName(t *testing.T) {
	for arn, want := range map[string]struct {
		role string
		ok   bool
	}{
		"arn:aws:sts::123456789012:assumed-role/Deploy/s":        {"Deploy", true},
		"arn:aws:sts::123456789012:assumed-role/Deploy/s/x":      {"Deploy", true},
		"arn:aws:iam::123456789012:user/a/b":                     {"", true},
		"arn:aws:iam::123456789012:root":                         {"", true},
		"arn:aws:iam::123456789012:user/":                        {"", false},
		"arn:aws:sts::123456789012:assumed-role//s":              {"", false},
		"arn:aws:sts::123456789012:assumed-role/Deploy/":         {"", false},
		"arn:aws:iam::123456789012:role/Deploy":                  {"", false},
		"arn:aws:sts:eu-central-1:123456789012:assumed-role/x/s": {"", false},
		"arn:aws:sts::210987654321:assumed-role/x/s":             {"", false},
	} {
		role, ok := ownRoleName(arn, testAccount)
		if role != want.role || ok != want.ok {
			t.Errorf("ownRoleName(%s) = %q, %v", arn, role, ok)
		}
	}
}

// A role tool is offered only by a connection whose tools list names it exactly: not by effect, not by a
// prefix, and not by naming another tool.
func TestIAMRoleToolsNeedTheirExactName(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	samples := map[string]string{}
	for _, c := range generatedCases {
		samples[c.tool] = c.arguments
	}
	for name, c := range map[string]struct {
		conn   config.Connection
		reason config.Refusal
	}{
		"by every effect": {config.Connection{Effects: all}, config.RefusalToolAllowList},
		"by default":      {config.Connection{}, ""},
		"by role prefix":  {config.Connection{Tools: []string{"aws.iam.role.*", "aws.iam.instance-profile.*"}}, config.RefusalToolsList},
		"by another tool": {config.Connection{Tools: []string{"aws.iam.role.get"}}, config.RefusalToolsList},
	} {
		f := direct(nil)
		e := newConnEnv(t, f, baseValues(), c.conn)
		for _, w := range iamWriteTools {
			_, err := e.invokeConfirmed(w.id, samples[w.id], true)
			var notOffered *capability.UnsupportedError
			if !errorsAs(err, &notOffered) || (c.reason != "" && notOffered.Reason != c.reason) || len(f.requests) != 0 {
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
	f := direct(func(r sent, _ int) reply { return iamAnswer(r.action, "") })
	e := iamWriteEnv(t, f, "aws.iam.role.delete")
	if _, err := e.invokeConfirmed("aws.iam.role.delete", `{"role_name":"web"}`, true); err != nil || f.count("DeleteRole") != 1 {
		t.Errorf("listed: %v, requests %v", err, f.actions())
	}
}
