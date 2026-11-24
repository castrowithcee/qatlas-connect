package aws

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

type iamUserCase struct {
	id, action, args, formKey string
	effect                    capability.Effect
	idem                      capability.Idempotency
	self                      bool // refuses the own user named by user_name
}

// iamUserTools are the IAM user and group write tools: effect, idempotency, the AWS action, arguments that pass,
// and a form parameter that proves the request carried the arguments.
var iamUserTools = []iamUserCase{
	{"aws.iam.user.update", "UpdateUser", `{"user_name":"%s","new_user_name":"web-app","new_path":"/apps/"}`, "NewUserName", capability.EffectUpdate, capability.IdempotencyIdempotent, false},
	{"aws.iam.user.delete", "DeleteUser", `{"user_name":"%s"}`, "UserName", capability.EffectDelete, capability.IdempotencyNonIdempotent, true},
	{"aws.iam.user.tag", "TagUser", `{"user_name":"%s","tags":[{"key":"team","value":"web"}]}`, "Tags.member.1.Key", capability.EffectUpdate, capability.IdempotencyIdempotent, false},
	{"aws.iam.user.untag", "UntagUser", `{"user_name":"%s","tag_keys":["team"]}`, "TagKeys.member.1", capability.EffectUpdate, capability.IdempotencyIdempotent, false},
	{"aws.iam.group.create", "CreateGroup", `{"group_name":"developers","path":"/teams/"}`, "Path", capability.EffectCreate, capability.IdempotencyNonIdempotent, false},
	{"aws.iam.group.update", "UpdateGroup", `{"group_name":"developers","new_group_name":"engineers","new_path":"/teams/"}`, "NewGroupName", capability.EffectUpdate, capability.IdempotencyIdempotent, false},
	{"aws.iam.group.delete", "DeleteGroup", `{"group_name":"developers"}`, "GroupName", capability.EffectDelete, capability.IdempotencyNonIdempotent, false},
	{"aws.iam.group.add-user", "AddUserToGroup", `{"group_name":"developers","user_name":"%s"}`, "UserName", capability.EffectUpdate, capability.IdempotencyNonIdempotent, false},
	{"aws.iam.group.remove-user", "RemoveUserFromGroup", `{"group_name":"developers","user_name":"%s"}`, "UserName", capability.EffectUpdate, capability.IdempotencyNonIdempotent, true},
}

func iamUserArgs(w iamUserCase, user string) string { return strings.ReplaceAll(w.args, "%s", user) }

func iamUserAnswer(r sent) reply {
	if r.action == "CreateGroup" {
		return iamAnswer(r.action, `<Group><GroupName>developers</GroupName><Path>/teams/</Path></Group>`)
	}
	return iamAnswer(r.action, "")
}

// Every tool is a listed-only write with confirmation, the effect and idempotency it is meant to have, and the
// own-user check only where the contract names it. CreateUser is no tool.
func TestIAMUserToolsAreListedOnlyWrites(t *testing.T) {
	for _, w := range iamUserTools {
		s := specOf(t, w.id)
		d := s.descriptor
		if !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired || d.Risk.Effect != w.effect ||
			!d.Risk.OpenWorld || d.Risk.Idempotency != w.idem || d.Risk.DataSensitivity == "" || s.action != w.action {
			t.Errorf("%s descriptor = %+v", w.id, d)
		}
		if (s.selfUser == "user_name") != w.self || (s.selfUser != "" && s.selfUser != "user_name") || s.selfRole != "" {
			t.Errorf("%s own-identity check user %q role %q", w.id, s.selfUser, s.selfRole)
		}
	}
	for _, s := range generatedSpecs {
		if s.action == "CreateUser" {
			t.Errorf("%s sends CreateUser, which is no tool", s.descriptor.ID)
		}
	}
}

// Each tool needs the confirmation, then sends its action once with its arguments.
func TestIAMUserToolsSend(t *testing.T) {
	for _, w := range iamUserTools {
		f := direct(func(r sent, _ int) reply {
			if r.action == w.action {
				return iamUserAnswer(r)
			}
			return reply{status: 500}
		})
		e := iamWriteEnv(t, f, w.id)
		args := iamUserArgs(w, "web")
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke(w.id, args); !errorsAs(err, &unconfirmed) || len(f.requests) != 0 {
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
		if w.action == "UpdateUser" && (last.form.Get("NewPath") != "/apps/" || last.form.Get("UserName") != "web") {
			t.Errorf("%s form = %v", w.id, last.form)
		}
		if w.action == "CreateGroup" {
			got := decodeResult(t, text)
			if group, _ := got["group"].(map[string]any); group["group_name"] != "developers" {
				t.Errorf("%s result = %v", w.id, got)
			}
		}
	}
}

// IAM errors keep their class: a missing entity is not found, an existing one and a delete conflict are
// conflicts, a quota is a limit.
func TestIAMUserWriteErrors(t *testing.T) {
	for _, c := range []struct {
		tool, code, text string
		status           int
		class            provider.Class
	}{
		{"aws.iam.user.update", "NoSuchEntity", "not found", 404, provider.ClassNotFound},
		{"aws.iam.user.update", "EntityAlreadyExists", "conflict", 409, provider.ClassProviderError},
		{"aws.iam.user.delete", "NoSuchEntity", "not found", 404, provider.ClassNotFound},
		{"aws.iam.user.delete", "DeleteConflict", "conflict", 409, provider.ClassProviderError},
		{"aws.iam.user.tag", "NoSuchEntity", "not found", 404, provider.ClassNotFound},
		{"aws.iam.user.tag", "LimitExceeded", "limit or quota", 409, provider.ClassProviderError},
		{"aws.iam.user.untag", "NoSuchEntity", "not found", 404, provider.ClassNotFound},
		{"aws.iam.group.create", "EntityAlreadyExists", "conflict", 409, provider.ClassProviderError},
		{"aws.iam.group.create", "LimitExceeded", "limit or quota", 409, provider.ClassProviderError},
		{"aws.iam.group.update", "NoSuchEntity", "not found", 404, provider.ClassNotFound},
		{"aws.iam.group.update", "EntityAlreadyExists", "conflict", 409, provider.ClassProviderError},
		{"aws.iam.group.delete", "NoSuchEntity", "not found", 404, provider.ClassNotFound},
		{"aws.iam.group.delete", "DeleteConflict", "conflict", 409, provider.ClassProviderError},
		{"aws.iam.group.add-user", "NoSuchEntity", "not found", 404, provider.ClassNotFound},
		{"aws.iam.group.add-user", "LimitExceeded", "limit or quota", 409, provider.ClassProviderError},
		{"aws.iam.group.remove-user", "NoSuchEntity", "not found", 404, provider.ClassNotFound},
	} {
		w := iamUserTools[slices.IndexFunc(iamUserTools, func(x iamUserCase) bool { return x.id == c.tool })]
		f := direct(func(r sent, _ int) reply {
			if r.action == "GetCallerIdentity" {
				return ok(identityXML)
			}
			return reply{status: c.status, body: iamErrorFor(c.code)}
		})
		e := iamWriteEnv(t, f, c.tool)
		_, err := e.invokeConfirmed(c.tool, iamUserArgs(w, "web"), true)
		var failed *provider.Error
		if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil || failed.Original.Entries[0].Code != c.code ||
			f.count(w.action) != 1 || (c.class == provider.ClassProviderError && !strings.Contains(failed.Message, c.text)) {
			t.Errorf("%s %s: %v after %v", c.tool, c.code, err, f.actions())
		}
	}
}

// user.delete and group.remove-user refuse the IAM user whose key signs the call, in any case, and send nothing
// for it. Every other tool, including a rename of that user, is not affected. A failed identity refuses the call.
func TestIAMUserOwnUserIsRefused(t *testing.T) {
	for _, w := range iamUserTools {
		run := func(identity func(int) reply, name string) (*fakeAWS, error) {
			f := &fakeAWS{answer: func(r sent, n int) reply {
				switch r.action {
				case "GetCallerIdentity":
					return identity(n)
				case w.action:
					return iamUserAnswer(r)
				}
				return reply{status: 500}
			}}
			e := iamWriteEnv(t, f, w.id)
			_, err := e.invokeConfirmed(w.id, iamUserArgs(w, name), true)
			return f, err
		}
		always := func(body string) func(int) reply { return func(int) reply { return ok(body) } }
		if !w.self {
			if strings.Contains(w.args, "%s") {
				if f, err := run(always(identityXML), "Alice"); err != nil || f.count(w.action) != 1 {
					t.Errorf("%s on the caller's user: %v, requests %v", w.id, err, f.actions())
				}
			}
			continue
		}
		for _, caller := range []string{identityXML, strings.Replace(identityXML, "user/Alice", "user/ops/team/Alice", 1)} {
			for _, name := range []string{"Alice", "alice", "ALICE"} {
				f, err := run(always(caller), name)
				if !isInvalidRequest(err) || f.count(w.action) != 0 || strings.Contains(err.Error(), name) {
					t.Errorf("%s on the own user %s: %v, requests %v", w.id, name, err, f.actions())
				}
			}
			if f, err := run(always(caller), "web"); err != nil || f.count(w.action) != 1 {
				t.Errorf("%s on another user: %v, requests %v", w.id, err, f.actions())
			}
		}
		if f, err := run(always(ownRoleIdentity), "Alice"); err != nil || f.count(w.action) != 1 {
			t.Errorf("%s with a role as caller: %v, requests %v", w.id, err, f.actions())
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
		odd := strings.Replace(identityXML, "arn:aws:iam::123456789012:user/Alice", "not-an-arn", 1)
		if f, err := run(always(odd), "web"); classOf(err) != provider.ClassInvalidResponse || f.count(w.action) != 0 {
			t.Errorf("%s with an unreadable caller: %v, requests %v", w.id, err, f.actions())
		}
	}
}

// A tool is offered only by a connection whose tools list names it exactly: not by effect, not by a prefix,
// and not by naming another tool.
func TestIAMUserToolsNeedTheirExactName(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	for name, c := range map[string]config.Connection{
		"by every effect": {Effects: all},
		"by default":      {},
		"by prefix":       {Tools: []string{"aws.iam.user.*", "aws.iam.group.*"}},
		"by another tool": {Tools: []string{"aws.iam.user.get"}},
	} {
		f := direct(nil)
		e := newConnEnv(t, f, baseValues(), c)
		for _, w := range iamUserTools {
			_, err := e.invokeConfirmed(w.id, iamUserArgs(w, "web"), true)
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

// Every text names the action, the user or group, and where it applies the old and new name or path and the
// effect; it reaches the confirmation-required error through the core, without a request.
func TestIAMUserConfirmationTexts(t *testing.T) {
	for id, want := range map[string][]string{
		"aws.iam.user.update":       {"Change the name to web-app and the path to /apps/ of IAM user web", "old name or ARN"},
		"aws.iam.user.delete":       {"Delete IAM user web", "cannot be undone"},
		"aws.iam.user.tag":          {"Tag IAM user web with tag keys team", "policy conditions"},
		"aws.iam.user.untag":        {"Remove tag keys team from IAM user web"},
		"aws.iam.group.create":      {"Create IAM group developers with path /teams/"},
		"aws.iam.group.update":      {"Change the name to engineers and the path to /teams/ of IAM group developers", "old name or ARN"},
		"aws.iam.group.delete":      {"Delete IAM group developers", "cannot be undone"},
		"aws.iam.group.add-user":    {"Add IAM user web to IAM group developers", "gains the permissions"},
		"aws.iam.group.remove-user": {"Remove IAM user web from IAM group developers", "loses the permissions"},
	} {
		w := iamUserTools[slices.IndexFunc(iamUserTools, func(x iamUserCase) bool { return x.id == id })]
		f := direct(func(sent, int) reply { return reply{status: 500} })
		e := iamWriteEnv(t, f, id)
		got := confirmationSummary(t, e, id, iamUserArgs(w, "web"))
		for _, part := range want {
			if !strings.Contains(got, part) {
				t.Errorf("%s: text %q lacks %q", id, got, part)
			}
		}
		if len(f.requests) != 0 {
			t.Errorf("%s: an unconfirmed call reached the network", id)
		}
	}
	for _, w := range iamUserTools {
		if summaries[w.id] == nil {
			t.Errorf("%s has no confirmation text", w.id)
		}
	}
}

// Untrusted values are cut and counted, and a text stays under the 500 runes of the core.
func TestIAMUserConfirmationBounds(t *testing.T) {
	long := strings.Repeat("a", 500)
	got := userSummaryOf(userUpdateSummary)([]byte(`{"user_name":"` + long + `","new_user_name":"` + long + `","new_path":"/` + long + `/"}`))
	if strings.Contains(got, long[:100]) || utf8.RuneCountInString(got) > 500 {
		t.Errorf("values not cut: %q", got)
	}
	tags := userSummaryKeys([]string{"a", "b", "c", "d", "e", "f"})
	if !strings.Contains(tags, "+2 more") {
		t.Errorf("tag keys not counted: %q", tags)
	}
	if got := userSummaryOf(userUpdateSummary)([]byte(`{"user_name":"web"}`)); !strings.Contains(got, "changing nothing") {
		t.Errorf("update without change: %q", got)
	}
	if got := userSummaryOf(userUpdateSummary)([]byte(`[`)); got != "" {
		t.Errorf("unreadable arguments gave %q", got)
	}
}
