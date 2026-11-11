package aws

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
)

const ownRoot = "arn:aws:iam::" + testAccount + ":root"

func policyOf(principal, condition string) string {
	c := ""
	if condition != "" {
		c = `,"Condition":` + condition
	}
	return `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":` + principal + `,"Action":"ses:SendEmail","Resource":"*"` + c + `}]}`
}

func TestResourcePolicyOwnAccount(t *testing.T) {
	b := binding{account: testAccount, regions: []string{testRegion}}
	srcAcct := `{"StringEquals":{"aws:SourceAccount":"` + testAccount + `"}}`
	srcArn := func(op, v string) string { return `{"` + op + `":{"aws:SourceArn":` + v + `}}` }
	ownArn := `"arn:aws:ses:` + testRegion + `:` + testAccount + `:identity/example.com"`
	svc := `{"Service":"ses.amazonaws.com"}`
	good := map[string]string{
		"account id":        policyOf(`{"AWS":"`+testAccount+`"}`, ""),
		"root":              policyOf(`{"AWS":"`+ownRoot+`"}`, ""),
		"user":              policyOf(`{"AWS":"arn:aws:iam::`+testAccount+`:user/a/b"}`, ""),
		"role list":         policyOf(`{"AWS":["arn:aws:iam::`+testAccount+`:role/r","`+testAccount+`"]}`, ""),
		"service account":   policyOf(svc, srcAcct),
		"service arn":       policyOf(svc, srcArn("ArnLike", ownArn)),
		"service arn list":  policyOf(svc, srcArn("StringEquals", `[`+ownArn+`,"arn:aws:ses:*:`+testAccount+`:*"]`)),
		"service key case":  policyOf(svc, `{"StringEquals":{"AWS:sourceaccount":"`+testAccount+`"}}`),
		"single statement":  `{"Statement":{"Effect":"Deny","Principal":{"AWS":"` + testAccount + `"},"Action":"*"}}`,
		"other cond narrow": policyOf(`{"AWS":"`+testAccount+`"}`, `{"ForAnyValue:StringEquals":{"aws:PrincipalTag/x":"y"}}`),
	}
	for name, doc := range good {
		if err := checkResourcePolicyOwnAccount(b, "policy", doc); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	foreign := "210987654321"
	bad := map[string]string{
		"star":                policyOf(`"*"`, ""),
		"aws star":            policyOf(`{"AWS":"*"}`, ""),
		"aws star list":       policyOf(`{"AWS":["`+testAccount+`","*"]}`, ""),
		"foreign account":     policyOf(`{"AWS":"`+foreign+`"}`, ""),
		"foreign root":        policyOf(`{"AWS":"arn:aws:iam::`+foreign+`:root"}`, ""),
		"account wildcard":    policyOf(`{"AWS":"arn:aws:iam::*:root"}`, ""),
		"user wildcard":       policyOf(`{"AWS":"arn:aws:iam::`+testAccount+`:user/*"}`, ""),
		"service bare":        policyOf(svc, ""),
		"service other key":   policyOf(svc, `{"StringEquals":{"aws:SourceOrg":"o-1"}}`),
		"service foreign":     policyOf(svc, `{"StringEquals":{"aws:SourceAccount":"`+foreign+`"}}`),
		"service mixed":       policyOf(svc, `{"StringEquals":{"aws:SourceAccount":["`+testAccount+`","`+foreign+`"]}}`),
		"service foreign arn": policyOf(svc, srcArn("ArnLike", `"arn:aws:ses:`+testRegion+`:`+foreign+`:identity/x"`)),
		"arn wildcard acct":   policyOf(svc, srcArn("ArnLike", `"arn:aws:ses:`+testRegion+`:*:identity/x"`)),
		"arn partial acct":    policyOf(svc, srcArn("ArnLike", `"arn:aws:ses:`+testRegion+`:1234567890*:identity/x"`)),
		"arn no account":      policyOf(svc, srcArn("ArnLike", `"arn:aws:s3:::bucket"`)),
		"account like":        policyOf(svc, `{"StringLike":{"aws:SourceAccount":"`+testAccount+`"}}`),
		"account not":         policyOf(svc, `{"StringNotEquals":{"aws:SourceAccount":"`+foreign+`"}}`),
		"arn not":             policyOf(svc, srcArn("ArnNotLike", ownArn)),
		"if exists":           policyOf(svc, `{"StringEqualsIfExists":{"aws:SourceAccount":"`+testAccount+`"}}`),
		"for any value":       policyOf(svc, `{"ForAnyValue:StringEquals":{"aws:SourceAccount":"`+testAccount+`"}}`),
		"for all values":      policyOf(svc, srcArn("ForAllValues:ArnLike", ownArn)),
		"two spellings":       policyOf(svc, `{"StringEquals":{"aws:SourceAccount":"`+testAccount+`","aws:sourceaccount":"`+foreign+`"}}`),
		"empty list":          policyOf(svc, `{"StringEquals":{"aws:SourceAccount":[]}}`),
		"number value":        policyOf(svc, `{"StringEquals":{"aws:SourceAccount":123456789012}}`),
		"condition string":    policyOf(svc, `"x"`),
		"service wildcard":    policyOf(`{"Service":"*.amazonaws.com"}`, srcAcct),
		"service not aws":     policyOf(`{"Service":"example.com"}`, srcAcct),
		"federated":           policyOf(`{"Federated":"cognito-identity.amazonaws.com"}`, ""),
		"canonical user":      policyOf(`{"CanonicalUser":"abc"}`, ""),
		"mixed type":          policyOf(`{"AWS":"`+testAccount+`","Federated":"x"}`, ""),
		"key case":            policyOf(`{"aws":"`+testAccount+`"}`, ""),
		"empty principal":     policyOf(`{}`, ""),
		"not principal":       `{"Statement":[{"Effect":"Allow","NotPrincipal":{"AWS":"` + testAccount + `"},"Action":"*"}]}`,
		"no principal":        `{"Statement":[{"Effect":"Allow","Action":"*"}]}`,
		"deny star":           `{"Statement":[{"Effect":"Deny","Principal":"*","Action":"*"}]}`,
		"second statement":    `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"` + testAccount + `"}},{"Effect":"Allow","Principal":"*"}]}`,
		"invalid json":        `{"Statement":`,
		"trailing data":       policyOf(`{"AWS":"`+testAccount+`"}`, "") + ` {}`,
		"duplicate key":       `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"` + testAccount + `","AWS":"*"}}]}`,
		"unknown element":     `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"` + testAccount + `"},"Extra":1}]}`,
		"unknown top":         `{"Statement":[],"Extra":1}`,
		"top case":            `{"statement":[{"Effect":"Allow","Principal":{"AWS":"` + testAccount + `"}}]}`,
		"no statement":        `{"Version":"2012-10-17"}`,
		"empty statements":    `{"Statement":[]}`,
		"not object":          `[]`,
		"too long":            policyOf(`{"AWS":"`+testAccount+`"}`, "")[:len(policyOf(`{"AWS":"`+testAccount+`"}`, ""))-1] + `,"Sid":"` + strings.Repeat("a", maxResourcePolicyChars) + `"}]}`,
		"invalid utf8":        "{\"Statement\":[{\"Principal\":{\"AWS\":\"\xff\"}}]}",
	}
	for name, doc := range bad {
		err := checkResourcePolicyOwnAccount(b, "policy", doc)
		if !isInvalidRequest(err) {
			t.Errorf("%s: %v, want a refusal", name, err)
			continue
		}
		if strings.Contains(err.Error(), foreign) || strings.Contains(err.Error(), "cognito") || !strings.HasPrefix(err.Error(), "policy: ") {
			t.Errorf("%s: message %q shows a value or lacks the name", name, err)
		}
	}
}

func policyArgs(id, policy string) string {
	args := map[string]string{"email_identity": "example.com", "policy_name": "send"}
	if id != "aws.ses.email-identity-policy.delete" {
		args["policy"] = policy
	}
	out, _ := json.Marshal(args)
	return string(out)
}

var sesPolicyWrites = []struct{ id, method string }{
	{"aws.ses.email-identity-policy.create", "POST"},
	{"aws.ses.email-identity-policy.update", "PUT"},
	{"aws.ses.email-identity-policy.delete", "DELETE"},
}

// A refused document sends nothing; a good one is sent as the member Policy to the policy path; the tools need
// their name in the tools list of the connection and a confirmation.
func TestSESPolicyWrites(t *testing.T) {
	good := policyOf(`{"AWS":"`+testAccount+`"}`, "")
	badDoc := policyOf(`"*"`, "")
	var ids []string
	for _, w := range sesPolicyWrites {
		ids = append(ids, w.id)
	}

	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := newToolsEnv(t, f, baseValues(), ids)
	for _, w := range sesPolicyWrites[:2] {
		if _, err := e.invokeConfirmed(w.id, policyArgs(w.id, badDoc), true); !isInvalidRequest(err) {
			t.Errorf("%s with a wildcard: %v", w.id, err)
		}
	}
	if n := len(f.requests); n != 0 {
		t.Fatalf("a refused document sent %d requests", n)
	}
	var unconfirmed *application.ConfirmationRequiredError
	for _, w := range sesPolicyWrites {
		if _, err := e.invoke(w.id, policyArgs(w.id, good)); !errorsAs(err, &unconfirmed) {
			t.Errorf("%s without confirmation: %v", w.id, err)
		}
	}
	if n := len(f.requests); n != 0 {
		t.Fatalf("an unconfirmed call sent %d requests", n)
	}
	for _, w := range sesPolicyWrites {
		if _, err := e.invokeConfirmed(w.id, policyArgs(w.id, good), true); err != nil {
			t.Errorf("%s: %v", w.id, err)
		}
	}
	calls := restCalls(f)
	if len(calls) != 3 {
		t.Fatalf("calls = %+v", calls)
	}
	for i, w := range sesPolicyWrites {
		if calls[i].method != w.method || calls[i].path != "/v2/email/identities/example.com/policies/send" {
			t.Errorf("%s = %+v", w.id, calls[i])
		}
	}
	if !jsonEqual(calls[0].body, `{"Policy":`+mustJSON(good)+`}`) || !jsonEqual(calls[1].body, calls[0].body) || calls[2].body != "" {
		t.Errorf("bodies = %q %q %q", calls[0].body, calls[1].body, calls[2].body)
	}

	// Not by effect, not by prefix, not by another tool.
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	for name, conn := range map[string]config.Connection{
		"effects": {Effects: all},
		"default": {},
		"prefix":  {Tools: []string{"aws.ses.email-identity-policy.*"}},
		"other":   {Tools: []string{"aws.ses.email-identity-policy.list", "aws.ses.email-identity.create"}},
	} {
		f := sesFake(func(sent) reply { return ok(`{}`) })
		e := newConnEnv(t, f, baseValues(), conn)
		for _, w := range sesPolicyWrites {
			if _, err := e.invokeConfirmed(w.id, policyArgs(w.id, good), true); err == nil || len(f.requests) != 0 {
				t.Errorf("%s offers %s: %v", name, w.id, err)
			}
		}
	}
}

func mustJSON(s string) string {
	out, _ := json.Marshal(s)
	return string(out)
}
