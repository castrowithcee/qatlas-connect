package aws

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var (
	s3PolicyReads  = []string{"aws.s3.bucket-policy.get", "aws.s3.bucket-policy.get-status"}
	s3PolicyWrites = []string{"aws.s3.bucket-policy.put", "aws.s3.bucket-policy.delete"}
)

// s3PolicyWrite reports whether a tool ID is a bucket policy write, which is listed-only.
func s3PolicyWrite(id string) bool { return id == s3PolicyWrites[0] || id == s3PolicyWrites[1] }

// bucketPolicyOf is a one-statement policy for the checks of a bucket policy.
func bucketPolicyOf(effect, principal, condition string) string {
	c := ""
	if condition != "" {
		c = `,"Condition":` + condition
	}
	return `{"Version":"2012-10-17","Statement":[{"Effect":"` + effect + `","Principal":` + principal +
		`,"Action":"s3:GetObject","Resource":"arn:aws:s3:::my-bucket/*"` + c + `}]}`
}

func TestBucketPolicyCheck(t *testing.T) {
	b := binding{account: testAccount, regions: []string{testRegion}}
	const foreign = "210987654321"
	allow := func(principal, condition string) string { return bucketPolicyOf("Allow", principal, condition) }
	acct := func(op, key, v string) string { return `{"` + op + `":{"` + key + `":` + v + `}}` }
	own := mustJSON(testAccount)
	ownArn := func(service string) string {
		return mustJSON("arn:aws:" + service + ":" + testRegion + ":" + testAccount + ":resource")
	}
	svc := func(name string) string { return `{"Service":"` + name + `"}` }
	both := `{"StringEquals":{"aws:SourceAccount":` + own + `},"ArnLike":{"aws:SourceArn":` + ownArn("logs") + `}}`

	good := map[string]string{
		"account id":               allow(`{"AWS":"`+testAccount+`"}`, ""),
		"root":                     allow(`{"AWS":"`+ownRoot+`"}`, ""),
		"role":                     allow(`{"AWS":"arn:aws:iam::`+testAccount+`:role/path/r"}`, ""),
		"user and role list":       allow(`{"AWS":["arn:aws:iam::`+testAccount+`:user/u","arn:aws:iam::`+testAccount+`:role/r"]}`, ""),
		"cloudtrail with arn":      allow(svc("cloudtrail.amazonaws.com"), acct("StringEquals", "aws:SourceArn", ownArn("cloudtrail"))),
		"cloudtrail with arnlike":  allow(svc("cloudtrail.amazonaws.com"), acct("ArnLike", "aws:SourceArn", ownArn("cloudtrail"))),
		"logs with account":        allow(svc("logs.amazonaws.com"), acct("StringEquals", "aws:SourceAccount", own)),
		"logs with stringlike":     allow(svc("logs.amazonaws.com"), acct("StringLike", "aws:SourceAccount", own)),
		"delivery with both":       allow(svc("delivery.logs.amazonaws.com"), both),
		"org id as an addition":    allow(`{"AWS":"`+testAccount+`"}`, `{"StringEquals":{"aws:PrincipalOrgID":"o-abc1234567"}}`),
		"org id beside the source": allow(svc("logs.amazonaws.com"), `{"StringEquals":{"aws:SourceAccount":`+own+`,"aws:PrincipalOrgID":"o-abc1234567"}}`),
		"single statement object":  `{"Statement":{"Effect":"Allow","Principal":{"AWS":"` + testAccount + `"},"Action":"s3:GetObject","Resource":"*"}}`,
	}
	for name, doc := range good {
		if err := checkBucketPolicy(b, "policy", doc); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	good2 := allow(`{"AWS":"`+testAccount+`"}`, "")
	statements := func(list ...string) string {
		var out []string
		for _, d := range list {
			var doc struct{ Statement []json.RawMessage }
			_ = json.Unmarshal([]byte(d), &doc)
			out = append(out, string(doc.Statement[0]))
		}
		return `{"Version":"2012-10-17","Statement":[` + strings.Join(out, ",") + `]}`
	}
	bad := map[string]string{
		"star":                  allow(`"*"`, ""),
		"aws star":              allow(`{"AWS":"*"}`, ""),
		"deny star":             bucketPolicyOf("Deny", `"*"`, `{"Bool":{"aws:SecureTransport":"false"}}`),
		"deny aws star":         bucketPolicyOf("Deny", `{"AWS":"*"}`, ""),
		"foreign account":       allow(`{"AWS":"`+foreign+`"}`, ""),
		"foreign root":          allow(`{"AWS":"arn:aws:iam::`+foreign+`:root"}`, ""),
		"other partition":       allow(`{"AWS":"arn:aws-cn:iam::`+testAccount+`:root"}`, ""),
		"unique id":             allow(`{"AWS":"AIDAEXAMPLEUNIQUEID1"}`, ""),
		"service bare":          allow(svc("cloudtrail.amazonaws.com"), ""),
		"service foreign acct":  allow(svc("logs.amazonaws.com"), acct("StringEquals", "aws:SourceAccount", mustJSON(foreign))),
		"service like wildcard": allow(svc("logs.amazonaws.com"), acct("StringLike", "aws:SourceAccount", `"1234567890*"`)),
		"service like star":     allow(svc("logs.amazonaws.com"), acct("StringLike", "aws:SourceAccount", `"*"`)),
		"arn without account":   allow(svc("cloudtrail.amazonaws.com"), acct("ArnLike", "aws:SourceArn", `"arn:aws:s3:::my-bucket"`)),
		"arn foreign account":   allow(svc("cloudtrail.amazonaws.com"), acct("ArnLike", "aws:SourceArn", mustJSON("arn:aws:cloudtrail:"+testRegion+":"+foreign+":trail/t"))),
		"arn wildcard account":  allow(svc("cloudtrail.amazonaws.com"), acct("ArnLike", "aws:SourceArn", `"arn:aws:cloudtrail:*:*:trail/t"`)),
		"arn wildcard partition": allow(svc("cloudtrail.amazonaws.com"), acct("StringLike", "aws:SourceArn",
			mustJSON("arn:*:cloudtrail:"+testRegion+":"+testAccount+":trail/t"))),
		"org id only":                     allow(svc("logs.amazonaws.com"), `{"StringEquals":{"aws:PrincipalOrgID":"o-abc1234567"}}`),
		"org paths only":                  allow(svc("logs.amazonaws.com"), `{"ForAnyValue:StringLike":{"aws:PrincipalOrgPaths":"o-abc/*"}}`),
		"org id for user":                 allow(`{"AWS":"`+foreign+`"}`, `{"StringEquals":{"aws:PrincipalOrgID":"o-abc1234567"}}`),
		"federated":                       allow(`{"Federated":"cognito-identity.amazonaws.com"}`, ""),
		"canonical user":                  allow(`{"CanonicalUser":"79a59df900b949e55d96a1e698fbaced"}`, ""),
		"not principal":                   `{"Statement":[{"Effect":"Deny","NotPrincipal":{"AWS":"` + testAccount + `"},"Action":"s3:*","Resource":"*"}]}`,
		"no principal":                    `{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`,
		"invalid json":                    `{"Statement":`,
		"list with one invalid statement": statements(good2, allow(`"*"`, "")),
		"too large":                       padPolicy(maxBucketPolicyBytes),
	}
	for name, doc := range bad {
		err := checkBucketPolicy(b, "policy", doc)
		if !isInvalidRequest(err) {
			t.Errorf("%s: %v, want a refusal", name, err)
			continue
		}
		if strings.Contains(err.Error(), foreign) || strings.Contains(err.Error(), "cognito") || strings.Contains(err.Error(), "o-abc") ||
			!strings.HasPrefix(err.Error(), "policy: ") {
			t.Errorf("%s: message %q shows a value or lacks the name", name, err)
		}
	}
	// The refusal of a list names the position of the statement.
	if err := checkBucketPolicy(b, "policy", bad["list with one invalid statement"]); err == nil || !strings.Contains(err.Error(), "statement 2") {
		t.Errorf("position = %v, want statement 2", err)
	}
	// Exactly the limit passes, one byte more does not.
	fit := maxBucketPolicyBytes - len(padPolicy(0))
	if err := checkBucketPolicy(b, "policy", padPolicy(fit)); err != nil {
		t.Errorf("a document of %d bytes: %v", len(padPolicy(fit)), err)
	}
	if err := checkBucketPolicy(b, "policy", padPolicy(fit+1)); err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Errorf("a document of %d bytes: %v", len(padPolicy(fit+1)), err)
	}
}

// padPolicy is a valid policy with a Sid of n characters.
func padPolicy(n int) string {
	return `{"Version":"2012-10-17","Statement":[{"Sid":"` + strings.Repeat("a", n) + `","Effect":"Allow","Principal":{"AWS":"` +
		testAccount + `"},"Action":"s3:GetObject","Resource":"*"}]}`
}

// The policy tools: listed-only writes with confirmation, reads open, and no argument that removes the
// access of the connection itself.
func TestBucketPolicyDescriptors(t *testing.T) {
	reg := registered(t)
	seen := 0
	for _, d := range reg.Provider(Provider) {
		if !strings.HasPrefix(d.ID, "aws.s3.bucket-policy.") {
			continue
		}
		seen++
		write := s3PolicyWrite(d.ID)
		if d.RequiresToolAllowList != write || (d.Risk.Effect != "read") != write ||
			(d.Risk.Confirmation == "required") != write {
			t.Errorf("%s = listed-only %t, risk %+v", d.ID, d.RequiresToolAllowList, d.Risk)
		}
		if strings.Contains(strings.ToLower(string(d.InputSchema)), "confirm") || strings.Contains(strings.ToLower(string(d.InputSchema)), "owner") {
			t.Errorf("%s offers an argument that loosens a check: %s", d.ID, d.InputSchema)
		}
	}
	if seen != 4 {
		t.Errorf("bucket policy tools = %d, want 4", seen)
	}
}

const (
	policyStatusPublic = `<PolicyStatus xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsPublic>true</IsPublic></PolicyStatus>`
	policyStatusClosed = `<PolicyStatus xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsPublic>false</IsPublic></PolicyStatus>`
)

func s3Failure(status int, code string) reply {
	return reply{status: status, header: map[string]string{"Content-Type": "application/xml"},
		body: `<Error><Code>` + code + `</Code><Message>m</Message></Error>`}
}

func policyEnv(t *testing.T, f *fakeAWS, tools []string) *env {
	t.Helper()
	return newConnEnv(t, f, baseValues(), config.Connection{Tools: tools, Targets: []string{"account/" + testAccount, "region/" + testRegion}})
}

func policyPutArgs(policy string) string {
	out, _ := json.Marshal(map[string]string{"bucket": "my-bucket", "policy": policy})
	return string(out)
}

func TestS3BucketPolicyPut(t *testing.T) {
	good := bucketPolicyOf("Allow", `{"AWS":"`+testAccount+`"}`, "")
	allTools := append(append([]string{}, s3PolicyReads...), s3PolicyWrites...)
	put := s3PolicyWrites[0]

	for _, c := range []struct {
		name       string
		status     reply
		wantPublic any // bool, or "unavailable"
	}{
		{"closed", xmlReply(policyStatusClosed), false},
		{"public", xmlReply(policyStatusPublic), true},
		{"status denied", s3Failure(403, "AccessDenied"), "unavailable"},
		{"status unreadable", reply{status: 200, body: "x"}, "unavailable"},
	} {
		t.Run("success, "+c.name, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				if r.method == "GET" {
					return c.status
				}
				return reply{status: 200}
			})
			text, err := policyEnv(t, f, allTools).invokeConfirmed(put, policyPutArgs(good), true)
			if err != nil {
				t.Fatal(err)
			}
			calls := restRequests(f)
			if len(calls) != 2 || calls[0].method != "PUT" || calls[0].rawQuery != "policy" || calls[0].body != good ||
				calls[1].method != "GET" || calls[1].rawQuery != "policyStatus" {
				t.Fatalf("calls = %+v, want the write and then the status read", calls)
			}
			for _, r := range calls {
				if r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount || r.host != "my-bucket.s3."+testRegion+".amazonaws.com" {
					t.Errorf("request = %+v", r)
				}
			}
			status, _ := decodeResult(t, text)["public_status"].(map[string]any)
			if c.wantPublic == "unavailable" {
				if _, ok := status["unavailable"]; !ok || len(status) != 1 {
					t.Errorf("public_status = %v, want unavailable", status)
				}
			} else if status["is_public"] != c.wantPublic {
				t.Errorf("public_status = %v, want is_public %v", status, c.wantPublic)
			}
		})
	}

	for _, c := range []struct {
		name  string
		reply reply
		class provider.Class
		hint  bool
	}{
		{"malformed policy", s3Failure(400, "MalformedPolicy"), provider.ClassProviderError, true},
		{"access denied", s3Failure(403, "AccessDenied"), provider.ClassPermission, true},
		{"no such bucket policy", s3Failure(404, "NoSuchBucketPolicy"), provider.ClassNotFound, false},
		{"no such bucket", s3Failure(404, "NoSuchBucket"), provider.ClassNotFound, false},
	} {
		t.Run("failure, "+c.name, func(t *testing.T) {
			f := restAnswer(c.reply)
			_, err := policyEnv(t, f, allTools).invokeConfirmed(put, policyPutArgs(good), true)
			if err == nil || (classOf(err) != c.class && !(isInvalidRequest(err) && c.class == provider.ClassProviderError)) {
				t.Fatalf("err = %v, class %q, want %q", err, classOf(err), c.class)
			}
			if got := strings.Contains(err.Error(), "root user"); got != c.hint {
				t.Errorf("err = %v, hint about the root user = %t, want %t", err, got, c.hint)
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("requests = %d, want the write only, no status read after a failure", n)
			}
		})
	}

	t.Run("refused document sends nothing", func(t *testing.T) {
		f := restAnswer(reply{status: 200})
		_, err := policyEnv(t, f, allTools).invokeConfirmed(put, policyPutArgs(bucketPolicyOf("Deny", `"*"`, "")), true)
		if !isInvalidRequest(err) || len(f.requests) != 0 {
			t.Errorf("err = %v, requests = %d", err, len(f.requests))
		}
	})
	t.Run("confirmation missing", func(t *testing.T) {
		for _, id := range s3PolicyWrites {
			f := restAnswer(reply{status: 200})
			args := policyPutArgs(good)
			if id == s3PolicyWrites[1] {
				args = `{"bucket":"my-bucket"}`
			}
			if _, err := policyEnv(t, f, allTools).invoke(id, args); !isConfirmationRequired(err) || len(f.requests) != 0 {
				t.Errorf("%s: err = %v, requests = %d", id, err, len(f.requests))
			}
		}
	})
	t.Run("not offered without an entry in tools", func(t *testing.T) {
		all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
		for name, conn := range map[string]config.Connection{
			"effects": {Effects: all},
			"default": {},
			"prefix":  {Tools: []string{"aws.s3.bucket-policy.*"}},
			"reads":   {Tools: s3PolicyReads},
		} {
			f := restAnswer(reply{status: 200})
			e := newConnEnv(t, f, baseValues(), conn)
			for _, id := range s3PolicyWrites {
				args := policyPutArgs(good)
				if id == s3PolicyWrites[1] {
					args = `{"bucket":"my-bucket"}`
				}
				if _, err := e.invokeConfirmed(id, args, true); err == nil || len(f.requests) != 0 {
					t.Errorf("%s offers %s: %v", name, id, err)
				}
			}
		}
	})
}

func TestS3BucketPolicyDeleteAndReads(t *testing.T) {
	allTools := append(append([]string{}, s3PolicyReads...), s3PolicyWrites...)
	f := restAnswer(reply{status: 204})
	if _, err := policyEnv(t, f, allTools).invokeConfirmed(s3PolicyWrites[1], `{"bucket":"my-bucket"}`, true); err != nil {
		t.Fatal(err)
	}
	calls := restRequests(f)
	if len(calls) != 1 || calls[0].method != "DELETE" || calls[0].rawQuery != "policy" ||
		calls[0].header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
		t.Errorf("calls = %+v", calls)
	}

	f = restAnswer(s3Failure(404, "NoSuchBucketPolicy"))
	if _, err := policyEnv(t, f, allTools).invokeConfirmed(s3PolicyWrites[1], `{"bucket":"my-bucket"}`, true); classOf(err) != provider.ClassNotFound {
		t.Errorf("delete without a policy: %v", err)
	}

	// The reads are open by effect and return the document as text, and the status as a boolean.
	f = direct(func(r sent, _ int) reply {
		if r.rawQuery == "policyStatus" {
			return xmlReply(policyStatusPublic)
		}
		return reply{status: 200, body: `{"Statement":[]}`}
	})
	e := newConnEnv(t, f, baseValues(), config.Connection{Targets: []string{"account/" + testAccount, "region/" + testRegion}})
	text, err := e.invoke(s3PolicyReads[0], `{"bucket":"my-bucket"}`)
	if err != nil || decodeResult(t, text)["policy"] != `{"Statement":[]}` {
		t.Errorf("get = %q, %v", text, err)
	}
	text, err = e.invoke(s3PolicyReads[1], `{"bucket":"my-bucket"}`)
	if status, _ := decodeResult(t, text)["policy_status"].(map[string]any); err != nil || status["is_public"] != true {
		t.Errorf("get-status = %q, %v", text, err)
	}
}
