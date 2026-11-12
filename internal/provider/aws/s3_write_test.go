package aws

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var s3WriteToolIDs = []string{"aws.s3.bucket.create", "aws.s3.bucket.delete", "aws.s3.versioning.put", "aws.s3.encryption.put",
	"aws.s3.encryption.delete", "aws.s3.tagging.put", "aws.s3.tagging.delete", "aws.s3.public-access-block.put", "aws.s3.lifecycle.delete",
	"aws.s3.cors.delete", "aws.s3.ownership.put"}

// s3WriteEnv offers the S3 write tools by name on a connection bound to the account and the given regions.
func s3WriteEnv(t *testing.T, f *fakeAWS, regions ...string) *env {
	t.Helper()
	targets := []string{"account/" + testAccount}
	for _, r := range regions {
		targets = append(targets, "region/"+r)
	}
	return newConnEnv(t, f, baseValues(), config.Connection{Tools: s3WriteToolIDs, Targets: targets})
}

// restAnswer answers every request but the identity call with the same reply.
func restAnswer(r reply) *fakeAWS { return direct(func(sent, int) reply { return r }) }

func isConfirmationRequired(err error) bool {
	var c *application.ConfirmationRequiredError
	return errors.As(err, &c)
}

// CreateBucket needs the region of the call as location constraint outside us-east-1 and none in it, names no
// owner, and never offers ACLs, Object Lock, directory buckets, or object ownership that turns ACLs on.
func TestS3CreateBucketLocationConstraint(t *testing.T) {
	f := restAnswer(reply{status: 200, header: map[string]string{"Location": "/my-new-bucket", "X-Amz-Bucket-Arn": "arn:aws:s3:::my-new-bucket"}})
	e := s3WriteEnv(t, f, "eu-central-1", "eu-west-1")
	for _, region := range []string{"eu-central-1", "eu-west-1"} {
		args := `{"bucket":"my-new-bucket","region":"` + region + `","create_bucket_configuration":{"location_constraint":"` + region + `"}}`
		text, err := e.invokeConfirmed("aws.s3.bucket.create", args, true)
		if err != nil {
			t.Fatalf("%s: %v", region, err)
		}
		got := decodeResult(t, text)
		if got["location"] != "/my-new-bucket" || got["bucket_arn"] != "arn:aws:s3:::my-new-bucket" || got["region"] != region {
			t.Errorf("result = %v", got)
		}
		r := restRequests(f)[len(restRequests(f))-1]
		if r.method != "PUT" || r.host != "my-new-bucket.s3."+region+".amazonaws.com" || r.rawQuery != "" ||
			!strings.Contains(r.body, "<LocationConstraint>"+region+"</LocationConstraint>") || r.header.Get("X-Amz-Expected-Bucket-Owner") != "" ||
			r.header.Get("X-Amz-Acl") != "" || r.header.Get("X-Amz-Object-Ownership") != "" {
			t.Errorf("request = %+v", r)
		}
	}
	// Tags in the configuration travel with it; ownership bucket owner enforced is the one offered value.
	if _, err := e.invokeConfirmed("aws.s3.bucket.create", `{"bucket":"my-new-bucket","region":"eu-west-1","object_ownership":"BucketOwnerEnforced",`+
		`"create_bucket_configuration":{"location_constraint":"eu-west-1","tags":[{"key":"team","value":"a"}]}}`, true); err != nil {
		t.Fatal(err)
	}
	r := restRequests(f)[len(restRequests(f))-1]
	if r.header.Get("X-Amz-Object-Ownership") != "BucketOwnerEnforced" || !strings.Contains(r.body, "<Tag><Key>team</Key><Value>a</Value></Tag>") {
		t.Errorf("request = %+v", r)
	}

	before := len(f.requests)
	for _, c := range []struct{ name, args string }{
		{"constraint of another allowed region", `{"bucket":"my-new-bucket","region":"eu-central-1","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
		{"constraint outside the allowlist", `{"bucket":"my-new-bucket","region":"eu-central-1","create_bucket_configuration":{"location_constraint":"us-west-2"}}`},
		{"region outside the allowlist", `{"bucket":"my-new-bucket","region":"us-west-2","create_bucket_configuration":{"location_constraint":"us-west-2"}}`},
		{"no constraint outside us-east-1", `{"bucket":"my-new-bucket","region":"eu-central-1"}`},
		{"tags without a constraint", `{"bucket":"my-new-bucket","region":"eu-central-1","create_bucket_configuration":{"tags":[{"key":"a","value":"b"}]}}`},
		{"the old name EU", `{"bucket":"my-new-bucket","region":"eu-west-1","create_bucket_configuration":{"location_constraint":"EU"}}`},
		{"us-east-1 as constraint", `{"bucket":"my-new-bucket","region":"eu-west-1","create_bucket_configuration":{"location_constraint":"us-east-1"}}`},
		{"canned ACL", `{"bucket":"b-ucket","region":"eu-west-1","acl":"public-read","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
		{"grant", `{"bucket":"b-ucket","region":"eu-west-1","grant_read":"uri=x","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
		{"Object Lock", `{"bucket":"b-ucket","region":"eu-west-1","object_lock_enabled_for_bucket":true,"create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
		{"ownership object writer", `{"bucket":"b-ucket","region":"eu-west-1","object_ownership":"ObjectWriter","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
		{"ownership bucket owner preferred", `{"bucket":"b-ucket","region":"eu-west-1","object_ownership":"BucketOwnerPreferred","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
		{"directory bucket location", `{"bucket":"b-ucket","region":"eu-west-1","create_bucket_configuration":{"location_constraint":"eu-west-1","location":{"type":"AvailabilityZone","name":"x"}}}`},
		{"directory bucket info", `{"bucket":"b-ucket","region":"eu-west-1","create_bucket_configuration":{"location_constraint":"eu-west-1","bucket":{"type":"Directory"}}}`},
		{"namespace", `{"bucket":"b-ucket","region":"eu-west-1","bucket_namespace":"account-regional","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
		{"expected owner", `{"bucket":"b-ucket","region":"eu-west-1","expected_bucket_owner":"999999999999","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
		{"upper case bucket name", `{"bucket":"My-Bucket","region":"eu-west-1","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
		{"underscore bucket name", `{"bucket":"my_bucket","region":"eu-west-1","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`},
	} {
		if _, err := e.invokeConfirmed("aws.s3.bucket.create", c.args, true); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
	if len(f.requests) != before {
		t.Errorf("a refused create reached the network: %v", f.actions()[before:])
	}
	if _, err := e.invoke("aws.s3.bucket.create", `{"bucket":"my-new-bucket","region":"eu-west-1","create_bucket_configuration":{"location_constraint":"eu-west-1"}}`); !isConfirmationRequired(err) {
		t.Errorf("create without confirmation: %v", err)
	}
	if len(f.requests) != before {
		t.Error("an unconfirmed create reached the network")
	}
}

// In us-east-1 the constraint is left out: the model does not list the region, and a constraint of another
// region is refused.
func TestS3CreateBucketInUSEast1HasNoConstraint(t *testing.T) {
	f := restAnswer(reply{status: 200})
	e := s3WriteEnv(t, f, "us-east-1")
	if _, err := e.invokeConfirmed("aws.s3.bucket.create", `{"bucket":"my-new-bucket"}`, true); err != nil {
		t.Fatal(err)
	}
	if r := restRequests(f)[0]; r.method != "PUT" || r.host != "my-new-bucket.s3.us-east-1.amazonaws.com" || r.body != "" {
		t.Errorf("request = %+v", r)
	}
	before := len(f.requests)
	for _, args := range []string{`{"bucket":"my-new-bucket","create_bucket_configuration":{"location_constraint":"eu-central-1"}}`,
		`{"bucket":"my-new-bucket","create_bucket_configuration":{"location_constraint":"us-east-1"}}`} {
		if _, err := e.invokeConfirmed("aws.s3.bucket.create", args, true); err == nil {
			t.Errorf("%s was accepted", args)
		}
	}
	if len(f.requests) != before {
		t.Error("a refused create reached the network")
	}
}

// The two conflicts of CreateBucket and a bucket that is not empty are provider errors that say so, and are
// not repeated: none of the writes is idempotent.
func TestS3WriteConflictsAreReportedAndNotRepeated(t *testing.T) {
	for _, c := range []struct {
		id, args, code string
		class          provider.Class
	}{
		{"aws.s3.bucket.create", `{"bucket":"my-bucket","create_bucket_configuration":{"location_constraint":"eu-central-1"}}`, "BucketAlreadyOwnedByYou", provider.ClassProviderError},
		{"aws.s3.bucket.create", `{"bucket":"my-bucket","create_bucket_configuration":{"location_constraint":"eu-central-1"}}`, "BucketAlreadyExists", provider.ClassProviderError},
		{"aws.s3.bucket.create", `{"bucket":"my-bucket","create_bucket_configuration":{"location_constraint":"eu-central-1"}}`, "TooManyBuckets", provider.ClassProviderError},
		{"aws.s3.bucket.delete", `{"bucket":"my-bucket"}`, "BucketNotEmpty", provider.ClassProviderError},
		{"aws.s3.bucket.delete", `{"bucket":"my-bucket"}`, "NoSuchBucket", provider.ClassNotFound},
		{"aws.s3.encryption.delete", `{"bucket":"my-bucket"}`, "ServerSideEncryptionConfigurationNotFoundError", provider.ClassNotFound},
		{"aws.s3.tagging.delete", `{"bucket":"my-bucket"}`, "NoSuchTagSet", provider.ClassNotFound},
	} {
		t.Run(c.id+"/"+c.code, func(t *testing.T) {
			f := restAnswer(reply{status: 409, header: map[string]string{"Content-Type": "application/xml"},
				body: `<Error><Code>` + c.code + `</Code><Message>m</Message></Error>`})
			e := s3WriteEnv(t, f, testRegion)
			_, err := e.invokeConfirmed(c.id, c.args, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class {
				t.Fatalf("err = %v, want class %s", err, c.class)
			}
			if c.class == provider.ClassProviderError && c.code != "TooManyBuckets" && !strings.Contains(failed.Message, "conflict") {
				t.Errorf("message = %q, want a conflict", failed.Message)
			}
			if failed.Original == nil || len(failed.Original.Entries) != 1 || failed.Original.Entries[0].Code != c.code {
				t.Errorf("original = %+v", failed.Original)
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("requests = %d, a failed write was repeated", n)
			}
		})
	}
}

// Every write except CreateBucket sends the bound account as the expected owner and the bucket as the host's
// first label, needs the confirmation before any request, and takes no owner, MFA, or ACL argument.
func TestS3BucketWritesSendOwnerAndNeedConfirmation(t *testing.T) {
	const pab = `{"block_public_acls":true,"ignore_public_acls":true,"block_public_policy":true,"restrict_public_buckets":true}`
	cases := []struct {
		id, args, method, query, body string
	}{
		{"aws.s3.bucket.delete", `{"bucket":"my-bucket"}`, "DELETE", "", ""},
		{"aws.s3.versioning.put", `{"bucket":"my-bucket","versioning_configuration":{"status":"Enabled"}}`, "PUT", "versioning", "<Status>Enabled</Status>"},
		{"aws.s3.versioning.put", `{"bucket":"my-bucket","versioning_configuration":{"status":"Suspended"}}`, "PUT", "versioning", "<Status>Suspended</Status>"},
		{"aws.s3.encryption.put", `{"bucket":"my-bucket","server_side_encryption_configuration":{"rules":[{"apply_server_side_encryption_by_default":{"sse_algorithm":"AES256"},"bucket_key_enabled":true}]}}`,
			"PUT", "encryption", "<SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault><BucketKeyEnabled>true</BucketKeyEnabled>"},
		{"aws.s3.encryption.delete", `{"bucket":"my-bucket"}`, "DELETE", "encryption", ""},
		{"aws.s3.tagging.put", `{"bucket":"my-bucket","tagging":{"tag_set":[{"key":"team","value":"a & b"}]}}`, "PUT", "tagging", "<Key>team</Key><Value>a &amp; b</Value>"},
		{"aws.s3.tagging.delete", `{"bucket":"my-bucket"}`, "DELETE", "tagging", ""},
		{"aws.s3.public-access-block.put", `{"bucket":"my-bucket","public_access_block_configuration":` + pab + `}`, "PUT", "publicAccessBlock", "<BlockPublicAcls>true</BlockPublicAcls>"},
	}
	for _, c := range cases {
		t.Run(c.id+"/"+c.body, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				if r.method == "GET" {
					return reply{status: 404, body: `<Error><Code>NoSuchPublicAccessBlockConfiguration</Code></Error>`}
				}
				return reply{status: 200}
			})
			e := s3WriteEnv(t, f, testRegion)
			if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
				t.Fatalf("without confirmation: %v", err)
			}
			if len(f.requests) != 0 {
				t.Fatalf("an unconfirmed write reached the network: %v", f.actions())
			}
			text, err := e.invokeConfirmed(c.id, c.args, true)
			if err != nil {
				t.Fatal(err)
			}
			if got := decodeResult(t, text); got["region"] != testRegion {
				t.Errorf("result = %v", got)
			}
			calls := restRequests(f)
			r := calls[len(calls)-1]
			if r.method != c.method || r.rawQuery != c.query || r.host != "my-bucket.s3."+testRegion+".amazonaws.com" ||
				r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount || !strings.Contains(r.body, c.body) {
				t.Errorf("request = %+v", r)
			}
			// Only the public access block reads before it writes.
			if want := map[bool]int{true: 2, false: 1}[c.id == "aws.s3.public-access-block.put"]; len(calls) != want {
				t.Errorf("rest requests = %d, want %d", len(calls), want)
			}
			// No spelling of the owner, MFA, or an ACL is an argument.
			before := len(f.requests)
			for _, extra := range []string{`"expected_bucket_owner":"999999999999"`, `"ExpectedBucketOwner":"999999999999"`, `"mfa":"123456 1"`, `"acl":"public-read"`} {
				if _, err := e.invokeConfirmed(c.id, strings.Replace(c.args, `{"bucket"`, `{`+extra+`,"bucket"`, 1), true); err == nil {
					t.Errorf("%s accepted %s", c.id, extra)
				}
			}
			if len(f.requests) != before {
				t.Error("a refused call was sent")
			}
		})
	}
	// MFA delete, the MFA header, and an invalid status are no input.
	f := restAnswer(reply{status: 200})
	e := s3WriteEnv(t, f, testRegion)
	for _, args := range []string{
		`{"bucket":"my-bucket","versioning_configuration":{"status":"Enabled","mfa_delete":"Enabled"}}`,
		`{"bucket":"my-bucket","mfa":"arn 123456","versioning_configuration":{"status":"Enabled"}}`,
		`{"bucket":"my-bucket","versioning_configuration":{"status":"Disabled"}}`,
		`{"bucket":"my-bucket"}`,
	} {
		if _, err := e.invokeConfirmed("aws.s3.versioning.put", args, true); err == nil {
			t.Errorf("versioning.put accepted %s", args)
		}
	}
	if len(f.requests) != 0 {
		t.Error("a refused versioning call was sent")
	}
}

// Invalid bucket names are refused by every write before any request.
func TestS3WritesRefuseInvalidBucketNames(t *testing.T) {
	f := restAnswer(reply{status: 200})
	e := s3WriteEnv(t, f, testRegion)
	for _, id := range s3WriteToolIDs {
		for _, bucket := range []string{"ab", "a/b", "../x", "my bucket", "192.168.0.1", "arn:aws:s3:eu-west-1:123456789012:accesspoint/x", ""} {
			if _, err := e.invokeConfirmed(id, `{"bucket":`+jsonString(bucket)+`}`, true); err == nil {
				t.Errorf("%s accepted %q", id, bucket)
			}
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call reached the network: %v", f.actions())
	}
}

// A KMS key of the encryption rules must be the ARN of a key or alias of the bound account in an allowed
// region; every rule is judged, and anything else sends nothing.
func TestS3EncryptionKMSKeysAreCheckedBeforeAnyRequest(t *testing.T) {
	rule := func(algorithm, key string) string {
		by := `{"sse_algorithm":"` + algorithm + `"`
		if key != "" {
			by += `,"kms_master_key_id":` + jsonString(key)
		}
		return `{"apply_server_side_encryption_by_default":` + by + `}}`
	}
	args := func(rules ...string) string {
		return `{"bucket":"my-bucket","server_side_encryption_configuration":{"rules":[` + strings.Join(rules, ",") + `]}}`
	}
	own := "arn:aws:kms:eu-central-1:" + testAccount + ":key/1234abcd-12ab-34cd-56ef-1234567890ab"
	f := restAnswer(reply{status: 200})
	e := s3WriteEnv(t, f, testRegion)
	for _, c := range []struct {
		name string
		args string
		want bool
	}{
		{"own key ARN", args(rule("aws:kms", own)), true},
		{"own multi-region key ARN", args(rule("aws:kms:dsse", "arn:aws:kms:eu-central-1:"+testAccount+":key/mrk-1234abcd12ab34cd56ef1234567890ab")), true},
		{"own alias ARN", args(rule("aws:kms", "arn:aws:kms:eu-central-1:"+testAccount+":alias/my-key")), true},
		{"own aws managed alias ARN", args(rule("aws:kms", "arn:aws:kms:eu-central-1:"+testAccount+":alias/aws/s3")), true},
		{"SSE-S3 without a key", args(rule("AES256", "")), true},
		{"KMS without a key", args(rule("aws:kms", "")), true},
		{"key of another account", args(rule("aws:kms", "arn:aws:kms:eu-central-1:999999999999:key/1234abcd")), false},
		{"alias of another account", args(rule("aws:kms", "arn:aws:kms:eu-central-1:999999999999:alias/x")), false},
		{"key in another region", args(rule("aws:kms", "arn:aws:kms:us-west-2:"+testAccount+":key/1234abcd")), false},
		{"bare key ID", args(rule("aws:kms", "1234abcd-12ab-34cd-56ef-1234567890ab")), false},
		{"bare alias name", args(rule("aws:kms", "alias/my-key")), false},
		{"ARN of another service", args(rule("aws:kms", "arn:aws:s3:eu-central-1:"+testAccount+":key/1234abcd")), false},
		{"ARN of another partition", args(rule("aws:kms", "arn:aws-cn:kms:eu-central-1:"+testAccount+":key/1234abcd")), false},
		{"ARN of something else in kms", args(rule("aws:kms", "arn:aws:kms:eu-central-1:"+testAccount+":grant/1234abcd")), false},
		{"empty key id", args(rule("aws:kms", "arn:aws:kms:eu-central-1:"+testAccount+":key/")), false},
		{"key id with a path", args(rule("aws:kms", "arn:aws:kms:eu-central-1:"+testAccount+":key/a/b")), false},
		{"no account", args(rule("aws:kms", "arn:aws:kms:eu-central-1::key/1234abcd")), false},
		{"second rule of another account", args(rule("aws:kms", own), rule("aws:kms", "arn:aws:kms:eu-central-1:999999999999:key/1234abcd")), false},
		{"algorithm of another service", args(rule("aws:fsx", "")), false},
		{"SSE-C is no algorithm", args(rule("SSE-C", "")), false},
	} {
		before := len(f.requests)
		_, err := e.invokeConfirmed("aws.s3.encryption.put", c.args, true)
		if (err == nil) != c.want {
			t.Errorf("%s: err = %v, want accepted = %v", c.name, err, c.want)
		}
		if !c.want && len(f.requests) != before {
			t.Errorf("%s reached the network: %v", c.name, f.actions()[before:])
		}
	}
	// An accepted key travels as given.
	last := restRequests(f)
	if len(last) == 0 || !strings.Contains(last[0].body, "<KMSMasterKeyID>"+own+"</KMSMasterKeyID>") {
		t.Errorf("first request = %+v", last)
	}
}

func pabAnswerOf(acls, ignore, policy, restrict string) reply {
	return xmlReply(`<PublicAccessBlockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><BlockPublicAcls>` + acls +
		`</BlockPublicAcls><IgnorePublicAcls>` + ignore + `</IgnorePublicAcls><BlockPublicPolicy>` + policy +
		`</BlockPublicPolicy><RestrictPublicBuckets>` + restrict + `</RestrictPublicBuckets></PublicAccessBlockConfiguration>`)
}

func pabArgs(settings string) string {
	return `{"bucket":"my-bucket","public_access_block_configuration":{` + settings + `}}`
}

const allOn = `"block_public_acls":true,"ignore_public_acls":true,"block_public_policy":true,"restrict_public_buckets":true`

// The public access block is only kept or tightened: the tool reads the block first, treats a bucket without
// one as all off, and refuses, without any change, a setting turned off or left out and every read it cannot
// understand.
func TestS3PublicAccessBlockIsNeverLoosened(t *testing.T) {
	missing := reply{status: 404, header: map[string]string{"Content-Type": "application/xml"},
		body: `<Error><Code>NoSuchPublicAccessBlockConfiguration</Code><Message>m</Message></Error>`}
	for _, c := range []struct {
		name    string
		current reply
		args    string
		wantPut bool
		class   provider.Class
	}{
		{"nothing present, all false", missing, pabArgs(`"block_public_acls":false,"ignore_public_acls":false,"block_public_policy":false,"restrict_public_buckets":false`), true, ""},
		{"nothing present, nothing given", missing, pabArgs(``), true, ""},
		{"nothing present, all on", missing, pabArgs(allOn), true, ""},
		{"on to off", pabAnswerOf("true", "true", "true", "true"), pabArgs(`"block_public_acls":false,"ignore_public_acls":true,"block_public_policy":true,"restrict_public_buckets":true`), false, provider.ClassProviderError},
		{"all on to all off", pabAnswerOf("true", "true", "true", "true"), pabArgs(`"block_public_acls":false,"ignore_public_acls":false,"block_public_policy":false,"restrict_public_buckets":false`), false, ""},
		{"on stays on", pabAnswerOf("true", "true", "true", "true"), pabArgs(allOn), true, ""},
		{"on, setting left out", pabAnswerOf("true", "false", "false", "false"), pabArgs(`"ignore_public_acls":true`), false, ""},
		{"on, nothing given", pabAnswerOf("false", "false", "true", "false"), pabArgs(``), false, ""},
		{"off to on", pabAnswerOf("false", "false", "false", "false"), pabArgs(allOn), true, ""},
		{"stricter, one more on", pabAnswerOf("true", "false", "false", "false"), pabArgs(`"block_public_acls":true,"restrict_public_buckets":true`), true, ""},
		{"setting left out of the answer counts as off", xmlReply(`<PublicAccessBlockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><BlockPublicAcls>true</BlockPublicAcls></PublicAccessBlockConfiguration>`),
			pabArgs(`"block_public_acls":true,"ignore_public_acls":true`), true, ""},
		{"read denied", reply{status: 403, header: map[string]string{"Content-Type": "application/xml"}, body: `<Error><Code>AccessDenied</Code><Message>m</Message></Error>`},
			pabArgs(allOn), false, provider.ClassPermission},
		{"bucket missing", reply{status: 404, header: map[string]string{"Content-Type": "application/xml"}, body: `<Error><Code>NoSuchBucket</Code><Message>m</Message></Error>`},
			pabArgs(allOn), false, provider.ClassNotFound},
		{"another not-found code", reply{status: 404, header: map[string]string{"Content-Type": "application/xml"}, body: `<Error><Code>NoSuchTagSet</Code><Message>m</Message></Error>`},
			pabArgs(allOn), false, provider.ClassNotFound},
		{"404 without a code", reply{status: 404}, pabArgs(allOn), false, provider.ClassNotFound},
		{"read fails", reply{err: errors.New("connection reset")}, pabArgs(allOn), false, ""},
		{"server error", reply{status: 500}, pabArgs(allOn), false, ""},
		{"empty answer", reply{status: 200}, pabArgs(allOn), false, provider.ClassInvalidResponse},
		{"answer that is no boolean", xmlReply(`<PublicAccessBlockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><BlockPublicAcls>maybe</BlockPublicAcls></PublicAccessBlockConfiguration>`),
			pabArgs(allOn), false, provider.ClassInvalidResponse},
		{"answer of another document", xmlReply(`<Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><TagSet></TagSet></Tagging>`), pabArgs(allOn), false, provider.ClassInvalidResponse},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				if r.method == "GET" {
					return c.current
				}
				return reply{status: 200}
			})
			e := s3WriteEnv(t, f, testRegion)
			_, err := e.invokeConfirmed("aws.s3.public-access-block.put", c.args, true)
			var puts int
			for _, r := range restRequests(f) {
				if r.method == "PUT" {
					puts++
				}
			}
			if c.wantPut {
				if err != nil || puts != 1 {
					t.Fatalf("err = %v, puts = %d, want one change", err, puts)
				}
				for _, r := range restRequests(f) {
					if r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount || r.host != "my-bucket.s3."+testRegion+".amazonaws.com" || r.rawQuery != "publicAccessBlock" {
						t.Errorf("request = %+v", r)
					}
				}
				if got := restRequests(f); got[0].method != "GET" || got[1].method != "PUT" {
					t.Errorf("order = %s then %s, want the read first", got[0].method, got[1].method)
				}
				return
			}
			if err == nil || puts != 0 {
				t.Fatalf("err = %v, puts = %d, want a refusal without a change", err, puts)
			}
			if c.class != "" && classOf(err) != c.class && !(c.class == provider.ClassProviderError && isInvalidRequest(err)) {
				t.Errorf("err = %v, class %q, want %q", err, classOf(err), c.class)
			}
		})
	}
}

// The readback belongs to the tool whose table names it: no other tool reads before it writes, and a tool
// that names an unknown one is a registration error.
func TestS3ReadbackBelongsToTheNamedToolOnly(t *testing.T) {
	var named []string
	for _, s := range generatedSpecs {
		if s.readback != "" {
			named = append(named, s.descriptor.ID)
		}
		if len(s.guards) > 0 && !slices.Contains([]string{"aws.s3.bucket.create", "aws.s3.encryption.put", "aws.s3.ownership.put", "aws.s3.object.copy", "aws.route53.hosted-zone.create", "aws.route53.hosted-zone.list-by-vpc", "aws.route53.record-set.change", "aws.route53.dns-answer.test", "aws.route53.health-check.create", "aws.route53.health-check.update", "aws.route53.key-signing-key.create", "aws.route53.cidr-collection.change", "aws.route53.query-logging-config.create", "aws.route53.traffic-policy.create", "aws.route53.traffic-policy.create-version", "aws.ses.configuration-set.create", "aws.ses.configuration-set.put-tracking-options", "aws.ses.configuration-set.put-archiving-options"}, s.descriptor.ID) {
			t.Errorf("%s names guards %v", s.descriptor.ID, s.guards)
		}
	}
	if !slices.Equal(named, []string{"aws.s3.public-access-block.put"}) {
		t.Errorf("tools with a readback = %v", named)
	}
	bad := generatedSpecs[slices.IndexFunc(generatedSpecs, func(s toolSpec) bool { return s.descriptor.ID == "aws.s3.versioning.put" })]
	bad.readback = "nothing"
	if err := bad.check(); err == nil {
		t.Error("an unknown readback passed the registration check")
	}
	bad.readback, bad.guards = "", []string{"nothing"}
	if err := bad.check(); err == nil {
		t.Error("an unknown guard passed the registration check")
	}
}

// The excluded operations are no tools, and no write offers an ACL, a website, logging, notifications,
// replication, a presigned URL, or the removal of a public access block. The readers of the website, logging,
// notification and replication configuration are not writes and are checked elsewhere.
func TestS3ExcludedOperationsCannotBeSent(t *testing.T) {
	want := []string{"DeleteBucketOwnershipControls", "DeletePublicAccessBlock", "PutBucketAcl", "PutObjectAcl", "PutBucketWebsite", "PutBucketLogging",
		"PutBucketNotificationConfiguration", "PutBucketReplication", "DeleteBucketReplication"}
	if len(excludedS3Actions) != len(want) {
		t.Fatalf("excluded = %v, want %v", excludedS3Actions, want)
	}
	for _, name := range want {
		if !slices.Contains(excludedS3Actions, name) {
			t.Errorf("%s is not excluded", name)
		}
	}
	reg := registered(t)
	tools := 0
	for _, d := range reg.Provider(Provider) {
		if d.Group != "s3" {
			continue
		}
		tools++
		schema := strings.ToLower(string(d.InputSchema) + string(d.OutputSchema))
		for _, banned := range []string{"presign", "object_lock"} {
			// Object Lock is only read: the answer of its reader names it, no input does.
			text := schema
			if banned == "object_lock" {
				text = strings.ToLower(string(d.InputSchema))
				if d.Risk.Effect != capability.EffectRead && strings.Contains(strings.ToLower(string(d.OutputSchema)), banned) {
					text += banned
				}
			}
			if strings.Contains(text, banned) && !(banned == "object_lock" && d.ID == "aws.s3.object-lock.get") {
				t.Errorf("%s offers %s: %s", d.ID, banned, schema)
			}
		}
		if d.Risk.Effect == capability.EffectRead {
			continue
		}
		for _, banned := range []string{`"acl"`, "grant", "website", "redirect", "logging", "notification", "replication", "target_bucket", "destination"} {
			if strings.Contains(schema, banned) {
				t.Errorf("%s offers %s: %s", d.ID, banned, schema)
			}
		}
		// MFA delete is read by versioning.get and nowhere an input.
		if strings.Contains(strings.ToLower(string(d.InputSchema)), "mfa") {
			t.Errorf("%s takes an MFA argument: %s", d.ID, d.InputSchema)
		}
		for _, banned := range []string{"acl", "website", "presign", "policy", "logging", "notification", "replication"} {
			// The bucket policy tools are the checked ones: their own tests hold them to the principal rules.
			if strings.Contains(d.ID, banned) && !strings.HasPrefix(d.ID, "aws.s3.bucket-policy.") {
				t.Errorf("%s names %s", d.ID, banned)
			}
		}
	}
	if tools != 47 {
		t.Errorf("s3 tools = %d, want 47", tools)
	}
	for _, s := range generatedSpecs {
		if s.service != "s3" {
			continue
		}
		uri := strings.ToLower(s.rest.URI)
		write := s.rest.Method != "GET" && s.rest.Method != "HEAD"
		if write && (strings.Contains(uri, "acl") || strings.Contains(uri, "website") || strings.Contains(uri, "logging") ||
			strings.Contains(uri, "notification") || strings.Contains(uri, "replication")) ||
			(s.rest.Method == "DELETE" && strings.Contains(uri, "publicaccessblock")) ||
			(s.rest.Method != "GET" && s.rest.Method != "HEAD" && s.rest.Method != "PUT" && s.rest.Method != "DELETE" &&
				// The two POST tools are the batch delete and the restore of an archived object.
				!(s.rest.Method == "POST" && (strings.HasSuffix(uri, "?delete") || strings.HasSuffix(uri, "?restore")))) {
			t.Errorf("%s sends %s %s", s.descriptor.ID, s.rest.Method, s.rest.URI)
		}
	}
	// The fake sees what a call of every write sends: none is an ACL, website, or presigned request.
	f := restAnswer(reply{status: 404})
	e := s3WriteEnv(t, f, testRegion)
	for _, id := range s3WriteToolIDs {
		_, _ = e.invokeConfirmed(id, `{"bucket":"my-bucket"}`, true)
	}
	for _, r := range restRequests(f) {
		if q := strings.ToLower(r.rawQuery); strings.Contains(q, "acl") || strings.Contains(q, "website") || strings.Contains(q, "signature") ||
			strings.Contains(q, "x-amz-expires") || r.header.Get("X-Amz-Acl") != "" {
			t.Errorf("request = %+v", r)
		}
	}
}
