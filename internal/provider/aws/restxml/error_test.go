package restxml_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awsretry"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/restxml"
)

// secretMessage stands for the free text of a provider error. It must never reach an error, a message or
// Original.
const secretMessage = "internal detail do-not-print 4711"

// headBucket is a HEAD with no body, deleteObject answers 204, copyObject has a structure payload like
// S3's, getObject a raw blob.
var (
	headBucket = &httpbind.Operation{Method: "HEAD", URI: "/{Bucket}",
		Input: obj(at(httpbind.LocLabel, f("bucket", "Bucket", str())))}
	copyObject = &httpbind.Operation{Method: "PUT", URI: "/{Bucket}/{Key+}",
		Input: obj(at(httpbind.LocLabel, f("bucket", "Bucket", str())), at(httpbind.LocLabel, f("key", "Key", str()))),
		Output: obj(at(httpbind.LocPayload, f("result", "CopyObjectResult", obj(f("etag", "ETag", str()), f("modified", "LastModified", stamp())))),
			at(httpbind.LocHeader, f("version", "x-amz-version-id", str())))}
	getObject = &httpbind.Operation{Method: "GET", URI: "/{Bucket}/{Key+}",
		Input:  obj(at(httpbind.LocLabel, f("bucket", "Bucket", str())), at(httpbind.LocLabel, f("key", "Key", str()))),
		Output: obj(at(httpbind.LocPayload, f("body", "Body", blob())))}
	changeRecords = &httpbind.Operation{Method: "POST", URI: "/2013-04-01/hostedzone/{HostedZoneId}/rrset",
		Input: root("ChangeResourceRecordSetsRequest", obj(at(httpbind.LocLabel, f("zone", "HostedZoneId", str())),
			f("batch", "ChangeBatch", obj(f("comment", "Comment", str()))))),
		Output: obj(f("status", "Status", str()))}
)

func s3Args() map[string]any { return map[string]any{"bucket": "b", "key": "k"} }

func TestS3ErrorAnswers(t *testing.T) {
	err := func(code string) string {
		return `<?xml version="1.0" encoding="UTF-8"?><Error><Code>` + code + `</Code><Message>` + secretMessage + `</Message><RequestId>4442587FB7D0A2F9</RequestId><HostId>h</HostId></Error>`
	}
	for _, c := range []struct {
		name   string
		status int
		header map[string]string
		body   string
		class  provider.Class
		code   string
		hold   time.Duration
	}{
		{"no such bucket", 404, nil, err("NoSuchBucket"), provider.ClassNotFound, "NoSuchBucket", 0},
		{"no such key", 404, nil, err("NoSuchKey"), provider.ClassNotFound, "NoSuchKey", 0},
		{"access denied", 403, nil, err("AccessDenied"), provider.ClassPermission, "AccessDenied", 0},
		{"signature", 403, nil, err("SignatureDoesNotMatch"), provider.ClassAuth, "SignatureDoesNotMatch", 0},
		{"slow down", 503, map[string]string{"Retry-After": "3"}, err("SlowDown"), provider.ClassRateLimited, "SlowDown", 3 * time.Second},
		{"slow down without Retry-After", 503, nil, err("SlowDown"), provider.ClassRateLimited, "SlowDown", 0},
		{"slow down elsewhere is not trusted", 400, nil, err("SlowDown"), provider.ClassInvalidResponse, "SlowDown", 0},
		{"throttling", 400, nil, err("Throttling"), provider.ClassRateLimited, "Throttling", 0},
		{"bucket exists", 409, nil, err("BucketAlreadyOwnedByYou"), provider.ClassProviderError, "BucketAlreadyOwnedByYou", 0},
		{"archived object", 403, nil, err("InvalidObjectState"), provider.ClassProviderError, "InvalidObjectState", 0},
		{"invalid request", 400, nil, err("InvalidRequest"), provider.ClassProviderError, "InvalidRequest", 0},
		{"unknown code", 400, nil, err("BrandNewError"), provider.ClassInvalidResponse, "BrandNewError", 0},
		{"unknown code with 403", 403, nil, err("BrandNewError"), provider.ClassPermission, "BrandNewError", 0},
		{"internal error", 500, nil, err("InternalError"), provider.ClassProviderError, "InternalError", 0},
		{"service unavailable without body", 503, nil, "", provider.ClassProviderError, "", 0},
		{"unreadable 4xx", 400, nil, "<html>" + secretMessage + "</html>", provider.ClassProviderError, "", 0},
		{"unreadable 5xx", 502, nil, "\x00\x01 " + secretMessage, provider.ClassProviderError, "", 0},
		{"truncated xml", 400, nil, `<Error><Code>NoSuchBucket</Code><Message>` + secretMessage, provider.ClassProviderError, "", 0},
		{"wrapped form from S3 is not its form", 404, nil, `<ErrorResponse><Error><Code>NoSuchBucket</Code></Error></ErrorResponse>`, provider.ClassNotFound, "", 0},
		{"code with markup", 400, nil, `<Error><Code>No&lt;Such</Code></Error>`, provider.ClassProviderError, "", 0},
		{"code too long", 400, nil, `<Error><Code>` + strings.Repeat("x", 300) + `</Code></Error>`, provider.ClassProviderError, "", 0},
		{"a HEAD answer has no body", 404, nil, "", provider.ClassNotFound, "", 0},
		{"429", 429, map[string]string{"Retry-After": "2"}, "", provider.ClassRateLimited, "", 2 * time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := &exchange{status: c.status, header: c.header, body: c.body}
			_, meta, failure := call(t, e, true, copyObject, s3Args())
			if failure == nil || failure.Class != c.class {
				t.Fatalf("failure = %v, want class %s", failure, c.class)
			}
			if meta.RetryAfter != c.hold {
				t.Errorf("hold = %v, want %v", meta.RetryAfter, c.hold)
			}
			got := ""
			if failure.Original != nil && len(failure.Original.Entries) > 0 {
				got = failure.Original.Entries[0].Code
			}
			if got != c.code {
				t.Errorf("Original code = %q, want %q", got, c.code)
			}
			if failure.Original == nil || failure.Original.Status != c.status {
				t.Errorf("Original = %+v", failure.Original)
			}
			everything := failure.Error() + " " + failure.Message + " " + string(failure.Cause)
			if failure.Original != nil {
				for _, entry := range failure.Original.Entries {
					everything += " " + entry.Message + entry.Path + entry.Type
				}
			}
			if strings.Contains(everything, "do-not-print") || strings.Contains(everything, "<html>") {
				t.Errorf("the provider text leaked: %s", everything)
			}
			if len(e.got) != 1 {
				t.Errorf("requests = %d", len(e.got))
			}
		})
	}
	// A HEAD for a missing bucket.
	e := &exchange{status: 404}
	_, _, failure := call(t, e, true, headBucket, map[string]any{"bucket": "b"})
	if failure == nil || failure.Class != provider.ClassNotFound || e.got[0].method != "HEAD" {
		t.Errorf("HEAD 404 = %v", failure)
	}
}

// S3 answers CopyObject and CompleteMultipartUpload with 200 and an Error element when it fails after it
// started the answer. That is an error, not an empty result, and it says the operation may not have completed.
func TestErrorInSuccessStatus(t *testing.T) {
	failing := func(code string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<Error><Code>` + code + `</Code><Message>` + secretMessage + `</Message><RequestId>r</RequestId></Error>`
	}
	for _, c := range []struct {
		name, body string
		class      provider.Class
		code       string
	}{
		{"internal error", failing("InternalError"), provider.ClassProviderError, "InternalError"},
		{"no such key", failing("NoSuchKey"), provider.ClassNotFound, "NoSuchKey"},
		{"access denied", failing("AccessDenied"), provider.ClassPermission, "AccessDenied"},
		{"without a code", `<Error><Message>` + secretMessage + `</Message></Error>`, provider.ClassProviderError, ""},
		{"empty element", `<Error/>`, provider.ClassProviderError, ""},
		{"unknown code", failing("Odd"), provider.ClassProviderError, "Odd"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := &exchange{status: 200, body: c.body}
			out, _, failure := call(t, e, true, copyObject, s3Args())
			if failure == nil || failure.Class != c.class || out != nil {
				t.Fatalf("out = %v, failure = %v", out, failure)
			}
			if strings.Contains(failure.Error()+failure.Message, "do-not-print") {
				t.Error("the provider text leaked")
			}
			if failure.Original == nil || failure.Original.Status != 200 {
				t.Errorf("Original = %+v", failure.Original)
			}
			if got := failure.Message; c.name == "internal error" && !strings.Contains(got, "may not have completed") {
				t.Errorf("message = %q", got)
			}
		})
	}
	// A normal answer is a result.
	e := &exchange{status: 200, header: map[string]string{"x-amz-version-id": "v1"},
		body: `<CopyObjectResult><ETag>&quot;e&quot;</ETag><LastModified>2026-01-02T03:04:05.000Z</LastModified></CopyObjectResult>`}
	out, _, failure := call(t, e, true, copyObject, s3Args())
	res, _ := out["result"].(map[string]any)
	if failure != nil || res["etag"] != `"e"` || out["version"] != "v1" {
		t.Errorf("out = %v, failure = %v", out, failure)
	}
	// An object whose bytes look like an error is still an object: a raw payload is never judged.
	e = &exchange{status: 200, body: `<Error><Code>AccessDenied</Code></Error>`}
	out, _, failure = call(t, e, true, getObject, s3Args())
	if failure != nil || out["body"] == nil {
		t.Errorf("a raw payload was taken for an error: %v, %v", out, failure)
	}
	// Route 53 does not wrap an error in a success: its codec never reads one there.
	e = &exchange{status: 200, body: `<Error><Code>AccessDenied</Code></Error>`}
	if _, _, failure := call(t, e, false, changeRecords, map[string]any{"zone": "Z1"}); failure != nil {
		t.Errorf("Route 53 success judged as an error: %v", failure)
	}
}

func TestRoute53ErrorAnswers(t *testing.T) {
	wrapped := func(typ, code string) string {
		return `<?xml version="1.0"?><ErrorResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/"><Error><Type>` + typ +
			`</Type><Code>` + code + `</Code><Message>` + secretMessage + `</Message></Error><RequestId>r</RequestId></ErrorResponse>`
	}
	for _, c := range []struct {
		name   string
		status int
		header map[string]string
		body   string
		class  provider.Class
		code   string
		typ    string
	}{
		{"no such hosted zone", 404, nil, wrapped("Sender", "NoSuchHostedZone"), provider.ClassNotFound, "NoSuchHostedZone", "Sender"},
		{"invalid change batch", 400, nil, wrapped("Sender", "InvalidChangeBatch"), provider.ClassProviderError, "InvalidChangeBatch", "Sender"},
		{"invalid change batch without wrapper", 400, nil,
			`<InvalidChangeBatch xmlns="x"><Messages><Message>` + secretMessage + `</Message></Messages><RequestId>r</RequestId></InvalidChangeBatch>`,
			provider.ClassProviderError, "InvalidChangeBatch", ""},
		{"prior request not complete", 400, map[string]string{"Retry-After": "5"}, wrapped("Sender", "PriorRequestNotComplete"), provider.ClassRateLimited, "PriorRequestNotComplete", "Sender"},
		{"throttling", 400, nil, wrapped("Sender", "Throttling"), provider.ClassRateLimited, "Throttling", "Sender"},
		{"access denied", 403, nil, wrapped("Sender", "AccessDenied"), provider.ClassPermission, "AccessDenied", "Sender"},
		{"zone not empty", 400, nil, wrapped("Sender", "HostedZoneNotEmpty"), provider.ClassProviderError, "HostedZoneNotEmpty", "Sender"},
		{"unknown code", 400, nil, wrapped("Sender", "BrandNew"), provider.ClassInvalidResponse, "BrandNew", "Sender"},
		{"receiver failure", 500, nil, wrapped("Receiver", "InternalFailure"), provider.ClassProviderError, "InternalFailure", "Receiver"},
		{"unknown bare element is not a code", 400, nil, `<Whatever><Code>NoSuchHostedZone</Code></Whatever>`, provider.ClassProviderError, "", ""},
		{"S3 form is not this one", 404, nil, `<Error><Code>NoSuchHostedZone</Code></Error>`, provider.ClassNotFound, "", ""},
		{"html", 502, nil, `<html><body>` + secretMessage + `</body></html>`, provider.ClassProviderError, "", ""},
		{"empty", 503, nil, ``, provider.ClassProviderError, "", ""},
		{"type with markup", 400, nil, `<ErrorResponse><Error><Type>a b</Type><Code>InvalidInput</Code></Error></ErrorResponse>`, provider.ClassProviderError, "InvalidInput", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := &exchange{status: c.status, header: c.header, body: c.body}
			_, _, failure := call(t, e, false, changeRecords, map[string]any{"zone": "Z1", "batch": map[string]any{"comment": "c"}})
			if failure == nil || failure.Class != c.class {
				t.Fatalf("failure = %v, want class %s", failure, c.class)
			}
			var code, typ string
			if failure.Original != nil && len(failure.Original.Entries) > 0 {
				code, typ = failure.Original.Entries[0].Code, failure.Original.Entries[0].Type
			}
			if code != c.code || typ != c.typ {
				t.Errorf("Original = %q %q, want %q %q", code, typ, c.code, c.typ)
			}
			if strings.Contains(failure.Error()+failure.Message, "do-not-print") {
				t.Error("the provider text leaked")
			}
		})
	}
}

// A write is never repeated after an unclear answer, whatever the code; a safe read is repeated only with a
// Retry-After or after a server failure.
func TestRetryPolicyOverTheXMLErrors(t *testing.T) {
	policy := awsretry.Default
	policy.Jitter = func() float64 { return 0 }
	answer := func(status int, header map[string]string, body string) *provider.Error {
		e := &exchange{status: status, header: header, body: body}
		_, _, failure := call(t, e, true, copyObject, s3Args())
		if failure == nil {
			t.Fatal("no failure")
		}
		return failure
	}
	inSuccess := answer(200, nil, `<Error><Code>InternalError</Code></Error>`)
	slow := answer(503, map[string]string{"Retry-After": "2"}, `<Error><Code>SlowDown</Code></Error>`)
	slowNoHint := answer(503, nil, `<Error><Code>SlowDown</Code></Error>`)
	server := answer(500, nil, `<Error><Code>InternalError</Code></Error>`)
	for _, idem := range []capability.Idempotency{capability.IdempotencyNonIdempotent, capability.IdempotencyIdempotent, capability.IdempotencyUnknown} {
		for name, f := range map[string]*provider.Error{"in success": inSuccess, "slow down": slow, "server": server} {
			if d := policy.Decide(idem, 0, f, 2*time.Second); d.Again {
				t.Errorf("%s, %s: repeated", idem, name)
			}
		}
	}
	if d := policy.Decide(capability.IdempotencySafe, 0, inSuccess, 0); d.Again {
		t.Error("an error in a success status was repeated")
	}
	if d := policy.Decide(capability.IdempotencySafe, 0, slow, 2*time.Second); !d.Again {
		t.Error("a safe read with Retry-After was not repeated")
	}
	if d := policy.Decide(capability.IdempotencySafe, 0, slowNoHint, 0); d.Again {
		t.Error("a rate limit without Retry-After was repeated")
	}
	if d := policy.Decide(capability.IdempotencySafe, 0, server, 0); !d.Again {
		t.Error("a safe read was not repeated after a server failure")
	}
}

func TestErrorCodeExtraction(t *testing.T) {
	s3 := restxml.Codec{NoErrorWrapping: true}
	r53 := restxml.Codec{}
	for _, c := range []struct {
		codec restxml.Codec
		body  string
		want  string
	}{
		{s3, `<Error><Code>NoSuchBucket</Code></Error>`, "NoSuchBucket"},
		{s3, `<Error><Code> NoSuchBucket </Code></Error>`, "NoSuchBucket"},
		{s3, `<Error><Code>A</Code><Code>B</Code></Error>`, ""},
		{s3, `<Error><Code><x/></Code></Error>`, ""},
		{s3, `<Error><Message>m</Message></Error>`, ""},
		{s3, `<Other><Code>A</Code></Other>`, ""},
		{s3, ``, ""},
		{s3, `{"__type":"A"}`, ""},
		{r53, `<ErrorResponse><Error><Code>NoSuchChange</Code></Error></ErrorResponse>`, "NoSuchChange"},
		{r53, `<ErrorResponse><Code>NoSuchChange</Code></ErrorResponse>`, ""},
		{r53, `<ErrorResponse><Error><Code>A</Code></Error><Error><Code>B</Code></Error></ErrorResponse>`, ""},
		{r53, `<NoSuchHostedZone><Messages/></NoSuchHostedZone>`, "NoSuchHostedZone"},
		{r53, `<Zzz/>`, ""},
	} {
		if got := c.codec.ErrorCode(http.Header{}, []byte(c.body)); got != c.want {
			t.Errorf("ErrorCode(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}

// The answer is bounded: a success answer above the limit is an error, never cut.
func TestAnswerAboveTheLimitIsRefused(t *testing.T) {
	big := `<CopyObjectResult><ETag>` + strings.Repeat("x", 2000) + `</ETag></CopyObjectResult>`
	e := &exchange{status: 200, body: big}
	req, err := copyObject.Encode(restxml.Codec{NoErrorWrapping: true}, s3Args(), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := newClient(e, true)
	client.ResponseLimit = 1000
	var out map[string]any
	_, failure := client.Do(t.Context(), "aws.test", copyObject, req, &out)
	if failure == nil || failure.Class != provider.ClassInvalidResponse || out != nil {
		t.Errorf("out = %v, failure = %v", out, failure)
	}
}
