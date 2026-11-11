package aws

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// s3ReadAnswers is a valid answer of each bucket tool, by tool ID.
var s3ReadAnswers = map[string]reply{
	"aws.s3.bucket.head": {status: 200, header: map[string]string{"X-Amz-Bucket-Region": "us-west-2",
		"X-Amz-Bucket-Arn": "arn:aws:s3:::my-bucket"}},
	"aws.s3.bucket.get-location": xmlReply(`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">eu-west-1</LocationConstraint>`),
	"aws.s3.versioning.get": xmlReply(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` +
		`<Status>Enabled</Status><MfaDelete>Disabled</MfaDelete></VersioningConfiguration>`),
	"aws.s3.encryption.get": xmlReply(`<ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule>` +
		`<ApplyServerSideEncryptionByDefault><SSEAlgorithm>aws:kms</SSEAlgorithm><KMSMasterKeyID>arn:aws:kms:eu-central-1:123456789012:key/k</KMSMasterKeyID>` +
		`</ApplyServerSideEncryptionByDefault><BucketKeyEnabled>true</BucketKeyEnabled>` +
		`<BlockedEncryptionTypes><EncryptionType>SSE-C</EncryptionType></BlockedEncryptionTypes></Rule>` +
		`<Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault></Rule>` +
		`</ServerSideEncryptionConfiguration>`),
	"aws.s3.tagging.get": xmlReply(`<Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><TagSet>` +
		`<Tag><Key>team</Key><Value>a &amp; b</Value></Tag><Tag><Key>env</Key><Value>prod</Value></Tag></TagSet></Tagging>`),
	"aws.s3.public-access-block.get": xmlReply(`<PublicAccessBlockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` +
		`<BlockPublicAcls>true</BlockPublicAcls><IgnorePublicAcls>true</IgnorePublicAcls>` +
		`<BlockPublicPolicy>false</BlockPublicPolicy><RestrictPublicBuckets>true</RestrictPublicBuckets></PublicAccessBlockConfiguration>`),
}

var s3BucketToolIDs = []string{"aws.s3.bucket.head", "aws.s3.bucket.get-location", "aws.s3.versioning.get",
	"aws.s3.encryption.get", "aws.s3.tagging.get", "aws.s3.public-access-block.get"}

func s3Fake(t *testing.T) *fakeAWS {
	t.Helper()
	return direct(func(r sent, _ int) reply {
		for id, answer := range s3ReadAnswers {
			want := map[string]string{"aws.s3.bucket.head": "", "aws.s3.bucket.get-location": "location", "aws.s3.versioning.get": "versioning",
				"aws.s3.encryption.get": "encryption", "aws.s3.tagging.get": "tagging", "aws.s3.public-access-block.get": "publicAccessBlock"}[id]
			if r.rawQuery == want && (r.method == "HEAD") == (id == "aws.s3.bucket.head") {
				return answer
			}
		}
		return reply{status: 500}
	})
}

// ListBuckets pages over three pages by ContinuationToken: the token goes in the query, page_size becomes
// max-buckets, the cursor hides the token, and the request names no expected owner.
func TestS3ListBucketsPagesByContinuationToken(t *testing.T) {
	page := func(name, token string) reply {
		body := `<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><DisplayName>me</DisplayName><ID>abc</ID></Owner>` +
			`<Buckets><Bucket><Name>` + name + `</Name><CreationDate>2026-01-02T03:04:05.000Z</CreationDate><BucketRegion>eu-central-1</BucketRegion></Bucket></Buckets>`
		if token != "" {
			body += `<ContinuationToken>` + token + `</ContinuationToken>`
		}
		return xmlReply(body + `</ListAllMyBucketsResult>`)
	}
	f := direct(func(r sent, _ int) reply {
		switch url.Values(mustQuery(t, r.rawQuery)).Get("continuation-token") {
		case "":
			return page("bucket-one", "tok-1")
		case "tok-1":
			return page("bucket-two", "tok-2")
		}
		return page("bucket-three", "")
	})
	e := newEnv(t, f, baseValues())
	cursor, firstCursor, names := "", "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"page_size":1,"prefix":"bucket-"}`
		if cursor != "" {
			args = `{"page_size":1,"prefix":"bucket-","cursor":"` + cursor + `"}`
		}
		text, err := e.invoke("aws.s3.bucket.list", args)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		got := decodeResult(t, text)
		buckets, _ := got["buckets"].([]any)
		first, _ := buckets[0].(map[string]any)
		names = append(names, first["name"].(string))
		next, _ := got["next_cursor"].(string)
		if got["has_more"] != (i < 2) || (next != "") != (i < 2) || strings.Contains(text, "tok-") || strings.Contains(next, "tok-") ||
			first["creation_date"] != "2026-01-02T03:04:05Z" || got["region"] != testRegion {
			t.Errorf("page %d = %s", i, text)
		}
		owner, _ := got["owner"].(map[string]any)
		if owner["id"] != "abc" {
			t.Errorf("owner = %v", got["owner"])
		}
		if i == 0 {
			firstCursor = next
		}
		cursor = next
	}
	if strings.Join(names, ",") != "bucket-one,bucket-two,bucket-three" {
		t.Errorf("names = %v", names)
	}
	calls := restRequests(f)
	if len(calls) != 3 {
		t.Fatalf("calls = %d", len(calls))
	}
	for i, r := range calls {
		q := mustQuery(t, r.rawQuery)
		if r.method != "GET" || r.path != "/" || r.host != "s3."+testRegion+".amazonaws.com" || q.Get("max-buckets") != "1" ||
			q.Get("prefix") != "bucket-" || q.Get("continuation-token") != []string{"", "tok-1", "tok-2"}[i] ||
			r.header.Get("X-Amz-Expected-Bucket-Owner") != "" {
			t.Errorf("request %d = %+v", i, r)
		}
	}
	// A cursor of another listing, a page size above the maximum and a bad region filter send nothing.
	before := len(f.requests)
	for _, args := range []string{`{"prefix":"other-","cursor":"` + firstCursor + `"}`, `{"page_size":10001}`, `{"page_size":0}`, `{"expected_bucket_owner":"1"}`} {
		if _, err := e.invoke("aws.s3.bucket.list", args); err == nil {
			t.Errorf("%s was accepted", args)
		}
	}
	if len(f.requests) != before {
		t.Error("a refused call was sent")
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// HeadBucket has no body: its region, ARN and the like come from the response headers.
func TestS3HeadBucketReturnsTheRegionFromTheHeader(t *testing.T) {
	f := s3Fake(t)
	e := newEnv(t, f, baseValues())
	text, err := e.invoke("aws.s3.bucket.head", `{"bucket":"my-bucket"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	if got["bucket_region"] != "us-west-2" || got["bucket_arn"] != "arn:aws:s3:::my-bucket" || got["region"] != testRegion {
		t.Errorf("result = %v", got)
	}
	if r := restRequests(f)[0]; r.method != "HEAD" || r.host != "my-bucket.s3."+testRegion+".amazonaws.com" || r.path != "/" {
		t.Errorf("request = %+v", r)
	}
}

// Every bucket tool sends the account of the connection as the expected owner, whatever the arguments say, and
// decodes its answer into the documented fields.
func TestS3BucketToolsSendTheBoundAccountAsOwner(t *testing.T) {
	f := s3Fake(t)
	e := newEnv(t, f, baseValues())
	results := map[string]map[string]any{}
	for _, id := range s3BucketToolIDs {
		text, err := e.invoke(id, `{"bucket":"my-bucket"}`)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		results[id] = decodeResult(t, text)
	}
	calls := restRequests(f)
	if len(calls) != len(s3BucketToolIDs) {
		t.Fatalf("calls = %d", len(calls))
	}
	for _, r := range calls {
		if got := r.header.Get("X-Amz-Expected-Bucket-Owner"); got != testAccount {
			t.Errorf("%s %s: owner = %q, want %q", r.method, r.rawQuery, got, testAccount)
		}
	}
	// No argument sets the header, under any spelling.
	before := len(f.requests)
	for _, id := range s3BucketToolIDs {
		for _, arg := range []string{"ExpectedBucketOwner", "expected_bucket_owner", "x-amz-expected-bucket-owner"} {
			if _, err := e.invoke(id, `{"bucket":"my-bucket","`+arg+`":"999999999999"}`); err == nil {
				t.Errorf("%s accepted %s", id, arg)
			}
		}
	}
	if len(f.requests) != before {
		t.Error("a refused call was sent")
	}

	if got := results["aws.s3.bucket.get-location"]; got["location_constraint"] != "eu-west-1" {
		t.Errorf("location = %v", got)
	}
	if got := results["aws.s3.versioning.get"]; got["status"] != "Enabled" || got["mfa_delete"] != "Disabled" {
		t.Errorf("versioning = %v", got)
	}
	enc, _ := results["aws.s3.encryption.get"]["server_side_encryption_configuration"].(map[string]any)
	rules, _ := enc["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("encryption = %v", results["aws.s3.encryption.get"])
	}
	first, _ := rules[0].(map[string]any)
	byDefault, _ := first["apply_server_side_encryption_by_default"].(map[string]any)
	blocked, _ := first["blocked_encryption_types"].(map[string]any)
	if byDefault["sse_algorithm"] != "aws:kms" || first["bucket_key_enabled"] != true || len(blocked["encryption_type"].([]any)) != 1 {
		t.Errorf("rule = %v", first)
	}
	// The KMS key ID is released by the table: an identifier, not a secret.
	if byDefault["kms_master_key_id"] != "arn:aws:kms:eu-central-1:123456789012:key/k" {
		t.Errorf("the KMS key is not in the result: %v", byDefault)
	}
	tags, _ := results["aws.s3.tagging.get"]["tag_set"].([]any)
	if len(tags) != 2 || tags[0].(map[string]any)["value"] != "a & b" {
		t.Errorf("tags = %v", results["aws.s3.tagging.get"])
	}
	pab, _ := results["aws.s3.public-access-block.get"]["public_access_block_configuration"].(map[string]any)
	if pab["block_public_acls"] != true || pab["block_public_policy"] != false || pab["restrict_public_buckets"] != true {
		t.Errorf("public access block = %v", results["aws.s3.public-access-block.get"])
	}
}

// A name that is no bucket name is refused by every bucket tool before the identity call and before any request.
func TestS3InvalidBucketNameSendsNothing(t *testing.T) {
	f := s3Fake(t)
	e := newEnv(t, f, baseValues())
	for _, id := range s3BucketToolIDs {
		for _, bucket := range []string{"ab", "a/b", "../x", "my bucket", "bucket-", "bu..cket", "192.168.0.1", "xn--bucket", strings.Repeat("a", 64),
			"arn:aws:s3:eu-west-1:123456789012:accesspoint/x", "bucket?x=1", ""} {
			if _, err := e.invoke(id, `{"bucket":`+jsonString(bucket)+`}`); err == nil {
				t.Errorf("%s accepted %q", id, bucket)
			}
		}
		if _, err := e.invoke(id, `{}`); err == nil {
			t.Errorf("%s accepted a call without a bucket", id)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call reached the network: %v", f.actions())
	}
}

// A bucket in another region is an error that names the right one, for every bucket tool.
func TestS3WrongRegionNamesTheRightOne(t *testing.T) {
	f := direct(func(sent, int) reply {
		return reply{status: 301, header: map[string]string{"X-Amz-Bucket-Region": "us-west-2"},
			body: `<Error><Code>PermanentRedirect</Code><Message>x</Message></Error>`}
	})
	e := newEnv(t, f, baseValues())
	for _, id := range s3BucketToolIDs {
		_, err := e.invoke(id, `{"bucket":"my-bucket"}`)
		var failed *provider.Error
		if !errorsAs(err, &failed) || failed.Class != provider.ClassProviderError || !strings.Contains(failed.Message, "call again with region us-west-2") {
			t.Errorf("%s: %v", id, err)
		}
	}
	if n := len(restRequests(f)); n != len(s3BucketToolIDs) {
		t.Errorf("requests = %d, a redirect was followed or repeated", n)
	}
}

// The answers for a configuration that does not exist are not-found, not an unknown code.
func TestS3MissingConfigurationIsNotFound(t *testing.T) {
	for _, c := range []struct{ id, code string }{
		{"aws.s3.encryption.get", "ServerSideEncryptionConfigurationNotFoundError"},
		{"aws.s3.tagging.get", "NoSuchTagSet"},
		{"aws.s3.public-access-block.get", "NoSuchPublicAccessBlockConfiguration"},
		{"aws.s3.versioning.get", "NoSuchBucket"},
		{"aws.s3.bucket.get-location", "NoSuchBucket"},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := direct(func(sent, int) reply {
				return reply{status: 404, header: map[string]string{"Content-Type": "application/xml"},
					body: `<Error><Code>` + c.code + `</Code><Message>m</Message></Error>`}
			})
			e := newEnv(t, f, baseValues())
			if _, err := e.invoke(c.id, `{"bucket":"my-bucket"}`); classOf(err) != provider.ClassNotFound {
				t.Errorf("err = %v, class %q, want not-found", err, classOf(err))
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("requests = %d, a not-found answer was repeated", n)
			}
		})
	}
	// HeadBucket has no body: the status says it.
	f := direct(func(sent, int) reply { return reply{status: 404} })
	e := newEnv(t, f, baseValues())
	if _, err := e.invoke("aws.s3.bucket.head", `{"bucket":"my-bucket"}`); classOf(err) != provider.ClassNotFound {
		t.Errorf("head of a missing bucket: %v", err)
	}
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
