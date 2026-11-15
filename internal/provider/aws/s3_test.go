package aws

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/localfile"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/endpoints"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/gen"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/restxml"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/sigv4"
)

// No S3 or Route 53 tool exists yet. These specs are what a service package will write for one: the
// binding comes from the real model and the curation table, the rest from the table of the test.

// modelDir is the directory of the pinned extracts, seen from this package.
var modelDir = filepath.Join("gen", gen.ModelDir)

func tableOf(t *testing.T, service string) gen.Table {
	t.Helper()
	for _, table := range gen.Tables {
		if table.Service == service {
			return table
		}
	}
	t.Fatalf("no table for %s", service)
	return gen.Table{}
}

func bindingOf2(t *testing.T, service, op string) *gen.RestBinding {
	t.Helper()
	table := tableOf(t, service)
	pin, err := gen.LoadPin(modelDir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := gen.LoadTableModel(modelDir, pin, table)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.BindRestCurated(&table, op)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// xmlSpec is a read tool of S3 (bucket-owner check on Bucket) or Route 53 (wrapped errors, global).
func xmlSpec(t *testing.T, service, op string) toolSpec {
	t.Helper()
	b := bindingOf2(t, service, op)
	s := toolSpec{
		descriptor: capability.Descriptor{ID: "aws." + service + ".test." + strings.ToLower(op), Group: "sts",
			Risk: capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe}},
		op: op, rest: b.Operation, pager: b.Pager, multi: b.Multi, targetCheck: "none", policyCheck: "none",
	}
	if service == "s3" {
		s.service, s.codec = endpoints.S3, restxml.Codec{NoErrorWrapping: true}
		s.targetCheck, s.targetArgs = "bucket-owner", []string{"Bucket"}
	} else {
		s.service, s.codec = endpoints.Route53, restxml.Codec{}
	}
	if err := s.check(); err != nil {
		t.Fatal(err)
	}
	return s
}

func xmlReply(body string) reply {
	return reply{status: 200, header: map[string]string{"Content-Type": "application/xml"}, body: body}
}

const emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// A bucket name that is one DNS label is the host's first label; any other valid name goes in the path.
func TestS3AddressingByBucketName(t *testing.T) {
	f := direct(func(sent, int) reply {
		return xmlReply(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
	})
	install(t, f)
	s := xmlSpec(t, "s3", "ListObjectsV2")
	for _, c := range []struct{ bucket, host, path string }{
		{"my-bucket", "my-bucket.s3." + testRegion + ".amazonaws.com", "/"},
		{"abc", "abc.s3." + testRegion + ".amazonaws.com", "/"},
		{"a1-2-3", "a1-2-3.s3." + testRegion + ".amazonaws.com", "/"},
		{"my.bucket", "s3." + testRegion + ".amazonaws.com", "/my.bucket"},
		{"MyBucket", "s3." + testRegion + ".amazonaws.com", "/MyBucket"},
		{"my_bucket", "s3." + testRegion + ".amazonaws.com", "/my_bucket"},
		{"1.2.3.x", "s3." + testRegion + ".amazonaws.com", "/1.2.3.x"},
	} {
		before := len(f.requests)
		if _, err := s.call(`{"Bucket":"` + c.bucket + `"}`); err != nil {
			t.Fatalf("%s: %v", c.bucket, err)
		}
		calls := restRequests(f)
		r := calls[len(calls)-1]
		if len(f.requests) <= before || r.host != c.host || r.path != c.path || r.rawQuery != "list-type=2" || r.method != "GET" {
			t.Errorf("%s: host %s, path %s, query %s", c.bucket, r.host, r.path, r.rawQuery)
		}
		auth := r.header.Get("Authorization")
		if !strings.Contains(auth, "/"+testRegion+"/s3/aws4_request") || !strings.Contains(auth, "x-amz-content-sha256") ||
			r.header.Get("X-Amz-Content-Sha256") != emptyHash || r.header.Get("Accept") != "application/xml" {
			t.Errorf("%s: headers %v", c.bucket, r.header)
		}
	}
	// A key keeps its own segments after the host-style bucket.
	f2 := direct(func(sent, int) reply { return reply{status: 200, header: map[string]string{"Content-Length": "0"}} })
	install(t, f2)
	head := xmlSpec(t, "s3", "HeadObject")
	if _, err := head.call(`{"Bucket":"my-bucket","Key":"a b/c%d"}`); err != nil {
		t.Fatal(err)
	}
	if r := restRequests(f2)[0]; r.host != "my-bucket.s3."+testRegion+".amazonaws.com" || r.path != "/a%20b/c%25d" || r.method != "HEAD" {
		t.Errorf("HeadObject = %+v", r)
	}
}

// A name that is no bucket name is refused before the identity call and before any request.
func TestInvalidBucketNameIsRefusedBeforeAnyIO(t *testing.T) {
	f := direct(func(sent, int) reply { return xmlReply(`<ListBucketResult/>`) })
	install(t, f)
	s := xmlSpec(t, "s3", "ListObjectsV2")
	for _, bucket := range []string{"ab", "a/b", "../x", "my bucket", "-bucket", "bucket-", "bu..cket", "bu.-cket", "192.168.0.1",
		"xn--bucket", "sthree-x", "bucket-s3alias", "bucket--x-s3", "a.mrap", strings.Repeat("a", 64), "bucket\n", "evil.com/x", "bucket?x=1",
		"bucket:443", "arn:aws:s3:eu-west-1:123456789012:accesspoint/x", ""} {
		args := `{"Bucket":` + jsonString(bucket) + `}`
		if _, err := s.call(args); !isInvalidRequest(err) {
			t.Errorf("%q: %v, want an invalid request", bucket, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused bucket reached the network: %v", f.requests)
	}
	// The same check runs inside the encoder, so a tool without the table entry is as safe.
	s.targetCheck, s.targetArgs = "none", nil
	if _, err := s.call(`{"Bucket":"a/b"}`); !isInvalidRequest(err) || len(f.requests) != 0 {
		t.Errorf("encoder: %v, requests %d", err, len(f.requests))
	}
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteString(hex.EncodeToString([]byte{byte(r)}))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// The expected owner is the account of the connection on every operation that has the header, and no argument
// can name it or change it.
func TestExpectedBucketOwnerIsTheBoundAccount(t *testing.T) {
	f := direct(func(sent, int) reply {
		return xmlReply(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
	})
	install(t, f)
	s := xmlSpec(t, "s3", "ListObjectsV2")
	if _, err := s.call(`{"Bucket":"my-bucket"}`); err != nil {
		t.Fatal(err)
	}
	if got := restRequests(f)[0].header.Get("X-Amz-Expected-Bucket-Owner"); got != testAccount {
		t.Errorf("owner = %q, want %q", got, testAccount)
	}
	before := len(f.requests)
	for _, args := range []string{`{"Bucket":"my-bucket","ExpectedBucketOwner":"999999999999"}`,
		`{"Bucket":"my-bucket","ExpectedBucketOwner":"` + testAccount + `"}`, `{"Bucket":"my-bucket","expected_bucket_owner":"1"}`} {
		if _, err := s.call(args); !isInvalidRequest(err) {
			t.Errorf("%s: %v", args, err)
		}
	}
	if len(f.requests) != before {
		t.Error("a refused call was sent")
	}
	// A copy names the owner of its source as well, and an operation without the header sends none.
	f2 := direct(func(sent, int) reply { return xmlReply(`<CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`) })
	install(t, f2)
	cp := xmlSpec(t, "s3", "CopyObject")
	cp.descriptor.Risk = capability.Risk{Effect: capability.EffectCreate, Idempotency: capability.IdempotencyNonIdempotent}
	if _, err := cp.call(`{"Bucket":"my-bucket","Key":"k","CopySource":"src-bucket/key"}`); err != nil {
		t.Fatal(err)
	}
	r := restRequests(f2)[0]
	if r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount || r.header.Get("X-Amz-Source-Expected-Bucket-Owner") != testAccount {
		t.Errorf("CopyObject headers = %v", r.header)
	}
	f3 := direct(func(sent, int) reply { return xmlReply(`<ListAllMyBucketsResult><Buckets/></ListAllMyBucketsResult>`) })
	install(t, f3)
	lb := xmlSpec(t, "s3", "ListBuckets")
	if _, err := lb.call(`{}`); err != nil {
		t.Fatal(err)
	}
	if r := restRequests(f3)[0]; r.header.Get("X-Amz-Expected-Bucket-Owner") != "" || r.host != "s3."+testRegion+".amazonaws.com" || r.path != "/" {
		t.Errorf("ListBuckets = %+v", r)
	}
}

// A request body is signed by its hash, a required checksum is the layer's, and an argument cannot set either.
func TestPayloadHashAndChecksumHeaders(t *testing.T) {
	f := direct(func(sent, int) reply { return reply{status: 200} })
	install(t, f)
	s := xmlSpec(t, "s3", "PutBucketVersioning")
	s.descriptor.Risk = capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent}
	if _, err := s.call(`{"Bucket":"my-bucket","VersioningConfiguration":{"Status":"Enabled"}}`); err != nil {
		t.Fatal(err)
	}
	r := restRequests(f)[0]
	sum := sha256.Sum256([]byte(r.body))
	md5Free := base64.StdEncoding.EncodeToString(sum[:])
	if r.method != "PUT" || r.rawQuery != "versioning" || !strings.HasPrefix(r.body, "<VersioningConfiguration") ||
		r.header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) ||
		r.header.Get("X-Amz-Checksum-Sha256") != md5Free || r.header.Get("X-Amz-Sdk-Checksum-Algorithm") != "SHA256" ||
		r.header.Get("Content-Type") != "application/xml" {
		t.Errorf("request = %+v", r)
	}
	for _, signed := range []string{"x-amz-checksum-sha256", "x-amz-content-sha256", "x-amz-sdk-checksum-algorithm"} {
		if !strings.Contains(r.header.Get("Authorization"), signed) {
			t.Errorf("%s is not signed: %s", signed, r.header.Get("Authorization"))
		}
	}
	for _, arg := range []string{"ChecksumSHA256", "ChecksumAlgorithm", "ContentMD5", "ContentLength"} {
		before := len(f.requests)
		if _, err := s.call(`{"Bucket":"my-bucket","` + arg + `":"x"}`); !isInvalidRequest(err) || len(f.requests) != before {
			t.Errorf("%s as an argument: %v", arg, err)
		}
	}
}

// Wrong region: a redirect or a malformed authorization is an error that names the region of
// x-amz-bucket-region when it has the form of one, is not followed, and is not repeated.
func TestWrongRegionIsAnErrorWithAHint(t *testing.T) {
	for _, c := range []struct {
		name   string
		reply  reply
		hint   string
		banned string
	}{
		{"permanent redirect", reply{status: 301, header: map[string]string{"X-Amz-Bucket-Region": "us-west-2", "Location": "https://other.example.org/"},
			body: `<Error><Code>PermanentRedirect</Code><Message>x</Message><Endpoint>b.s3.us-west-2.amazonaws.com</Endpoint></Error>`}, "us-west-2", "other.example.org"},
		{"redirect without a body", reply{status: 301, header: map[string]string{"X-Amz-Bucket-Region": "ap-southeast-2"}}, "ap-southeast-2", ""},
		{"authorization header malformed", reply{status: 400, header: map[string]string{"X-Amz-Bucket-Region": "eu-west-1"},
			body: `<Error><Code>AuthorizationHeaderMalformed</Code><Region>eu-west-1</Region></Error>`}, "eu-west-1", ""},
		{"a region that is no region", reply{status: 301, header: map[string]string{"X-Amz-Bucket-Region": "evil.example.org/x"},
			body: `<Error><Code>PermanentRedirect</Code></Error>`}, "", "evil.example.org"},
		{"another partition", reply{status: 301, header: map[string]string{"X-Amz-Bucket-Region": "cn-north-1"}}, "", "cn-north-1"},
		{"no header", reply{status: 400, body: `<Error><Code>AuthorizationHeaderMalformed</Code></Error>`}, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := direct(func(sent, int) reply { return c.reply })
			install(t, f)
			s := xmlSpec(t, "s3", "HeadBucket")
			_, err := s.call(`{"Bucket":"my-bucket"}`)
			var failed *provider.Error
			if !errors.As(err, &failed) || failed.Class != provider.ClassProviderError || len(restRequests(f)) != 1 {
				t.Fatalf("err = %v, requests = %d", err, len(restRequests(f)))
			}
			if c.hint != "" && !strings.Contains(failed.Message, "call again with region "+c.hint) {
				t.Errorf("message = %q", failed.Message)
			}
			if c.hint == "" && strings.Contains(failed.Message, "call again with region") {
				t.Errorf("message = %q", failed.Message)
			}
			if c.banned != "" && strings.Contains(failed.Error()+failed.Message, c.banned) {
				t.Errorf("a provider value leaked: %q", failed.Message)
			}
		})
	}
}

// The tool pages a list by two markers through the same handler: the cursor holds both, and the next
// request carries them.
func TestToolPagesByTwoMarkers(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		switch r.rawQuery {
		case "versions":
			return xmlReply(`<ListVersionsResult><IsTruncated>true</IsTruncated><NextKeyMarker>k1</NextKeyMarker><NextVersionIdMarker>v1</NextVersionIdMarker></ListVersionsResult>`)
		case "versions&key-marker=k1&version-id-marker=v1":
			return xmlReply(`<ListVersionsResult><IsTruncated>true</IsTruncated><NextKeyMarker>k2</NextKeyMarker></ListVersionsResult>`)
		}
		return xmlReply(`<ListVersionsResult><IsTruncated>false</IsTruncated></ListVersionsResult>`)
	})
	install(t, f)
	s := xmlSpec(t, "s3", "ListObjectVersions")
	cursor, pages := "", 0
	for {
		args := `{"Bucket":"my-bucket"}`
		if cursor != "" {
			args = `{"Bucket":"my-bucket","cursor":"` + cursor + `"}`
		}
		got, err := s.call(args)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		next, _ := got["next_cursor"].(string)
		if got["has_more"] != (next != "") || strings.Contains(next, "k1") {
			t.Errorf("page %d = %v", pages, got)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if pages != 3 {
		t.Fatalf("pages = %d", pages)
	}
	qs := []string{}
	for _, r := range restRequests(f) {
		qs = append(qs, r.rawQuery)
	}
	if len(qs) != 3 || qs[2] != "versions&key-marker=k2" {
		t.Errorf("queries = %q", qs)
	}
	before := len(f.requests)
	if _, err := s.call(`{"Bucket":"other-bucket","cursor":"` + cursor + `"}`); !isInvalidRequest(err) || len(f.requests) != before {
		t.Errorf("a cursor of another bucket: %v", err)
	}
}

// Route 53 through the handler: the wrapped error form, the zone ID with its prefix, and a three-member cursor.
func TestRoute53ToolPagesAndMapsErrors(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		switch r.rawQuery {
		case "":
			return xmlReply(`<ListResourceRecordSetsResponse><IsTruncated>true</IsTruncated><NextRecordName>a.example.org.</NextRecordName><NextRecordType>A</NextRecordType><NextRecordIdentifier>w1</NextRecordIdentifier></ListResourceRecordSetsResponse>`)
		case "identifier=w1&name=a.example.org.&type=A":
			return xmlReply(`<ListResourceRecordSetsResponse><IsTruncated>false</IsTruncated></ListResourceRecordSetsResponse>`)
		}
		return reply{status: 404, body: `<ErrorResponse><Error><Type>Sender</Type><Code>NoSuchHostedZone</Code><Message>m</Message></Error></ErrorResponse>`}
	})
	install(t, f)
	s := xmlSpec(t, "route-53", "ListResourceRecordSets")
	first, err := s.call(`{"HostedZoneId":"/hostedzone/Z0123456789"}`)
	if err != nil || first["has_more"] != true {
		t.Fatalf("first = %v, %v", first, err)
	}
	second, err := s.call(`{"HostedZoneId":"Z0123456789","cursor":"` + first["next_cursor"].(string) + `"}`)
	if err != nil || second["has_more"] != false {
		t.Fatalf("second = %v, %v", second, err)
	}
	for _, r := range restRequests(f) {
		if r.host != "route53.amazonaws.com" || r.path != "/2013-04-01/hostedzone/Z0123456789/rrset" ||
			!strings.Contains(r.header.Get("Authorization"), "/us-east-1/route53/aws4_request") || r.header.Get("X-Amz-Content-Sha256") != "" {
			t.Errorf("request = %+v", r)
		}
	}
	// The cursor of one zone does not continue another zone's list.
	before := len(f.requests)
	if _, err := s.call(`{"HostedZoneId":"Z9999999999","cursor":"` + first["next_cursor"].(string) + `"}`); !isInvalidRequest(err) || len(f.requests) != before {
		t.Errorf("a cursor of another zone: %v", err)
	}
	// A zone ID that would add a path segment is refused before any request.
	for _, id := range []string{"Z1/../x", "/hostedzone/a/b", "/change/C1"} {
		if _, err := s.call(`{"HostedZoneId":` + jsonString(id) + `}`); !isInvalidRequest(err) || len(f.requests) != before {
			t.Errorf("%q: %v", id, err)
		}
	}
	// A wrapped error maps by its code.
	f2 := direct(func(sent, int) reply {
		return reply{status: 404, body: `<ErrorResponse><Error><Type>Sender</Type><Code>NoSuchHostedZone</Code><Message>m</Message></Error></ErrorResponse>`}
	})
	install(t, f2)
	if _, err := s.call(`{"HostedZoneId":"Z0123456789"}`); classOf(err) != provider.ClassNotFound {
		t.Errorf("no such zone: %v", err)
	}
}

// The registration check knows the new parts of a spec.
func TestRestXMLSpecIsChecked(t *testing.T) {
	good := xmlSpec(t, "s3", "ListObjectVersions")
	both := good
	both.pager = &httpbind.Pager{TokenIn: "a", TokenOut: "~b"}
	query := listSpec()
	query.multi = good.multi
	codec := listSpec()
	codec.codec = restxml.Codec{}
	owner := listSpec()
	owner.targetCheck, owner.targetArgs = "bucket-owner", []string{"prefix"}
	broken := good
	broken.multi = &httpbind.MultiPager{}
	for name, s := range map[string]toolSpec{"two pagers": both, "multi on awsQuery": query, "codec on awsQuery": codec,
		"bucket-owner on awsQuery": owner, "broken multi pager": broken} {
		if s.check() == nil {
			t.Errorf("%s: the spec was accepted", name)
		}
	}
}

// --- streams ---

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// zeros is a reader of n zero bytes, so a limit test needs no file of that size.
type zeros struct{ n int64 }

func (z *zeros) Read(p []byte) (int, error) {
	if z.n <= 0 {
		return 0, io.EOF
	}
	k := min(int64(len(p)), z.n)
	clear(p[:k])
	z.n -= k
	return int(k), nil
}

func zerosHash(n int64) string {
	h := sha256.New()
	_, _ = io.Copy(h, &zeros{n})
	return hex.EncodeToString(h.Sum(nil))
}

var streamCreds = sigv4.Credentials{AccessKeyID: testKeyID, SecretAccessKey: testSecret}

func streamClient(t *testing.T, rt http.RoundTripper) *Client {
	t.Helper()
	install(t, direct(nil))
	return &Client{name: "prod", http: provider.NoRedirectClient(time.Minute, rt)}
}

func putObject(t *testing.T) (*httpbind.Operation, httpbind.Request) {
	t.Helper()
	b := bindingOf2(t, "s3", "PutObject")
	req, err := b.Operation.EncodeWith(restxml.Codec{NoErrorWrapping: true},
		map[string]any{"Bucket": "my-bucket", "Key": "dir/file.bin", "ContentType": "text/plain"}, httpbind.EncodeOptions{Owner: testAccount})
	if err != nil {
		t.Fatal(err)
	}
	return b.Operation, req
}

func sendPut(t *testing.T, c *Client, o *httpbind.Operation, req httpbind.Request) (httpbind.Meta, *provider.Error) {
	t.Helper()
	var out map[string]any
	return c.sendRest(t.Context(), "put object", endpoints.S3, testRegion, streamCreds, capability.IdempotencyIdempotent,
		restxml.Codec{NoErrorWrapping: true}, o, req, &out)
}

// An upload is read once, as long as announced, signed UNSIGNED-PAYLOAD over https, and reported with the
// SHA-256 of what was sent. Exactly 100 MiB goes; one byte more is refused before the network.
func TestUploadStreamLimitAndSignature(t *testing.T) {
	const limit = httpbind.MaxStreamBytes
	var got struct {
		n      int64
		sum    string
		header http.Header
		host   string
		length int64
		calls  int
	}
	c := streamClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got.calls++
		h := sha256.New()
		got.n, _ = io.Copy(h, r.Body)
		got.sum, got.header, got.host, got.length = hex.EncodeToString(h.Sum(nil)), r.Header.Clone(), r.URL.Host, r.ContentLength
		return &http.Response{StatusCode: 200, Header: http.Header{"Etag": {`"e"`}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}))
	o, req := putObject(t)
	if err := req.SetStream(o, httpbind.StreamBody{Reader: &zeros{limit + 1}, Size: limit + 1}); err == nil {
		t.Fatal("100 MiB + 1 was accepted")
	}
	if got.calls != 0 {
		t.Fatal("a refused upload reached the network")
	}
	if err := req.SetStream(o, httpbind.StreamBody{Reader: &zeros{limit}, Size: limit}); err != nil {
		t.Fatal(err)
	}
	meta, failure := sendPut(t, c, o, req)
	if failure != nil {
		t.Fatal(failure)
	}
	if got.calls != 1 || got.n != limit || got.length != limit || meta.Size != limit || meta.SHA256 != zerosHash(limit) || got.sum != meta.SHA256 {
		t.Errorf("calls %d, sent %d, length %d, meta %+v", got.calls, got.n, got.length, meta)
	}
	if got.header.Get("X-Amz-Content-Sha256") != sigv4.UnsignedPayload || got.host != "my-bucket.s3."+testRegion+".amazonaws.com" ||
		got.header.Get("Content-Type") != "text/plain" || got.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount ||
		!strings.Contains(got.header.Get("Authorization"), "x-amz-content-sha256") {
		t.Errorf("headers = %v, host %s", got.header, got.host)
	}
	// A reader that is shorter or longer than announced fails the call and is not repeated.
	for name, reader := range map[string]io.Reader{"shorter": &zeros{5}, "longer": &zeros{50}} {
		got.calls = 0
		_, req := putObject(t)
		if err := req.SetStream(o, httpbind.StreamBody{Reader: reader, Size: 10}); err != nil {
			t.Fatal(err)
		}
		if _, failure := sendPut(t, c, o, req); failure == nil || got.calls > 1 {
			t.Errorf("%s: failure = %v, calls %d", name, failure, got.calls)
		}
	}
	// An upload cannot name a stream the operation does not take, and cannot carry a body twice.
	get := bindingOf2(t, "s3", "GetObject").Operation
	r, _ := get.Encode(restxml.Codec{}, map[string]any{"Bucket": "my-bucket", "Key": "k"}, nil)
	if err := r.SetStream(get, httpbind.StreamBody{Reader: &zeros{1}, Size: 1}); err == nil {
		t.Error("a stream was attached to a download")
	}
	// A server failure is not repeated for a stream, whatever the operation's idempotency says.
	n := 0
	c2 := streamClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n++
		_, _ = io.Copy(io.Discard, r.Body)
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}))
	_, req = putObject(t)
	_ = req.SetStream(o, httpbind.StreamBody{Reader: &zeros{3}, Size: 3})
	if _, failure := sendPut(t, c2, o, req); failure == nil || n != 1 {
		t.Errorf("failure = %v after %d calls", failure, n)
	}
	// A stream needs a signer for it; without one the call is refused.
	_, req = putObject(t)
	_ = req.SetStream(o, httpbind.StreamBody{Reader: &zeros{3}, Size: 3})
	bare := &httpbind.Client{HTTP: http.DefaultClient, Codec: restxml.Codec{}, URL: "https://s3." + testRegion + ".amazonaws.com", Signer: &sigv4.Signer{}}
	if _, failure := bare.Do(t.Context(), "put", o, req, nil); failure == nil {
		t.Error("a stream was sent without a stream signer")
	}
}

type memSink struct {
	data     []byte
	expected int64
	expSHA   string
	refuse   bool
}

func (m *memSink) Write(p []byte) (int, error) { m.data = append(m.data, p...); return len(p), nil }
func (m *memSink) ExpectSize(n int64) error {
	if m.refuse {
		return errors.New("no")
	}
	m.expected = n
	return nil
}
func (m *memSink) ExpectSHA256(sum string) error { m.expSHA = sum; return nil }

func getObjectCall(t *testing.T, c *Client, sink httpbind.Sink) (httpbind.Meta, *provider.Error, map[string]any) {
	t.Helper()
	b := bindingOf2(t, "s3", "GetObject")
	req, err := b.Operation.EncodeWith(restxml.Codec{NoErrorWrapping: true}, map[string]any{"Bucket": "my.bucket", "Key": "k"}, httpbind.EncodeOptions{Owner: testAccount})
	if err != nil {
		t.Fatal(err)
	}
	if err := req.SetSink(b.Operation, sink); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	meta, failure := c.sendRest(t.Context(), "get object", endpoints.S3, testRegion, streamCreds, capability.IdempotencySafe,
		restxml.Codec{NoErrorWrapping: true}, b.Operation, req, &out)
	return meta, failure, out
}

func answerWith(length int64, body io.Reader, header http.Header) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if header == nil {
			header = http.Header{}
		}
		return &http.Response{StatusCode: 200, Header: header, ContentLength: length, Body: io.NopCloser(body), Request: r}, nil
	})
}

// A download needs a stated length of at most 100 MiB, announces it to the sink before writing, and succeeds
// only when exactly that much arrived; the headers of the answer are decoded, the payload is not a result.
func TestDownloadStreamLimitAndCompleteness(t *testing.T) {
	const limit = httpbind.MaxStreamBytes
	sum := sha256.Sum256([]byte("hello"))
	header := http.Header{"Etag": {`"e"`}, "X-Amz-Checksum-Sha256": {base64.StdEncoding.EncodeToString(sum[:])}}
	sink := &memSink{}
	var seen http.Header
	c := streamClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.Header.Clone()
		return &http.Response{StatusCode: 200, Header: header, ContentLength: 5, Body: io.NopCloser(strings.NewReader("hello")), Request: r}, nil
	}))
	meta, failure, out := getObjectCall(t, c, sink)
	if failure != nil || string(sink.data) != "hello" || sink.expected != 5 || sink.expSHA != hex.EncodeToString(sum[:]) ||
		meta.Size != 5 || meta.SHA256 != hex.EncodeToString(sum[:]) || out["ETag"] != `"e"` || out["Body"] != nil {
		t.Fatalf("failure %v, sink %+v, meta %+v, out %v", failure, sink, meta, out)
	}
	if seen.Get("X-Amz-Checksum-Mode") != "ENABLED" || seen.Get("Accept-Encoding") != "identity" ||
		seen.Get("X-Amz-Expected-Bucket-Owner") != testAccount || seen.Get("X-Amz-Content-Sha256") != emptyHash {
		t.Errorf("request headers = %v", seen)
	}
	// Exactly 100 MiB is a result; 100 MiB + 1 is refused before one byte is written.
	big := &memSink{}
	c = streamClient(t, answerWith(limit, &zeros{limit}, nil))
	if meta, failure, _ := getObjectCall(t, c, big); failure != nil || int64(len(big.data)) != limit || meta.SHA256 != zerosHash(limit) {
		t.Errorf("100 MiB: %v, %d bytes", failure, len(big.data))
	}
	over := &memSink{}
	c = streamClient(t, answerWith(limit+1, &zeros{limit + 1}, nil))
	if _, failure, _ := getObjectCall(t, c, over); failure == nil || len(over.data) != 0 || over.expected != 0 {
		t.Errorf("100 MiB + 1: %v, %d bytes written", failure, len(over.data))
	}
	for name, c := range map[string]*Client{
		"shorter than stated": streamClient(t, answerWith(10, strings.NewReader("hello"), nil)),
		"longer than stated":  streamClient(t, answerWith(3, strings.NewReader("hello"), nil)),
		"no length":           streamClient(t, answerWith(-1, strings.NewReader("hello"), nil)),
		"wrong checksum":      streamClient(t, answerWith(5, strings.NewReader("world"), header)),
	} {
		s := &memSink{}
		if _, failure, _ := getObjectCall(t, c, s); failure == nil {
			t.Errorf("%s: reported as a success", name)
		}
	}
	if _, failure, _ := getObjectCall(t, streamClient(t, answerWith(5, strings.NewReader("hello"), nil)), &memSink{refuse: true}); failure == nil {
		t.Error("a refused size was written anyway")
	}
	// A failure answer is an error, not a payload.
	c = streamClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`<Error><Code>NoSuchKey</Code></Error>`)), Request: r}, nil
	}))
	s := &memSink{}
	if _, failure, _ := getObjectCall(t, c, s); failure == nil || failure.Class != provider.ClassNotFound || len(s.data) != 0 {
		t.Errorf("404: %v", failure)
	}
	// An operation that streams needs its sink, and a sink needs a streaming operation.
	b := bindingOf2(t, "s3", "GetObject")
	req, _ := b.Operation.Encode(restxml.Codec{}, map[string]any{"Bucket": "my-bucket", "Key": "k"}, nil)
	c = streamClient(t, answerWith(0, strings.NewReader(""), nil))
	if _, failure := c.sendRest(t.Context(), "get", endpoints.S3, testRegion, streamCreds, capability.IdempotencySafe,
		restxml.Codec{}, b.Operation, req, nil); failure == nil {
		t.Error("a download without a sink was sent")
	}
	head := bindingOf2(t, "s3", "HeadObject").Operation
	hr, _ := head.Encode(restxml.Codec{}, map[string]any{"Bucket": "my-bucket", "Key": "k"}, nil)
	if err := hr.SetSink(head, &memSink{}); err == nil {
		t.Error("a sink was attached to an operation that does not stream")
	}
}

// The local file helpers fit the interfaces: a file is uploaded with its own size and hash, a download is
// created, written and committed only on success, and a failed one leaves nothing.
func TestStreamsWithLocalFiles(t *testing.T) {
	var _ httpbind.Sink = (*localfile.Download)(nil)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "in.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := &config.Resolved{Files: config.Files{Read: []string{dir}, Write: []string{dir}}}
	ctx := context.Background()

	upload, err := localfile.OpenForUpload(ctx, resolved, filepath.Join(dir, "in.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer upload.Close()
	var body string
	c := streamClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}))
	o, req := putObject(t)
	if err := req.SetStream(o, httpbind.StreamBody{Reader: upload, Size: upload.Size}); err != nil {
		t.Fatal(err)
	}
	meta, failure := sendPut(t, c, o, req)
	fileSum, _ := upload.SHA256()
	if failure != nil || body != "hello" || meta.SHA256 != fileSum {
		t.Errorf("upload: %v, body %q, meta %+v, file sum %s", failure, body, meta, fileSum)
	}

	target := filepath.Join(dir, "out.bin")
	download, err := localfile.CreateForDownload(ctx, resolved, target)
	if err != nil {
		t.Fatal(err)
	}
	c = streamClient(t, answerWith(5, strings.NewReader("hello"), nil))
	if _, failure, _ := getObjectCall(t, c, download); failure != nil {
		t.Fatal(failure)
	}
	if err := download.Commit(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "hello" {
		t.Errorf("downloaded = %q", data)
	}
	// A download that ends early is aborted by the caller and leaves neither the target nor a temporary file.
	partial := filepath.Join(dir, "partial.bin")
	download, err = localfile.CreateForDownload(ctx, resolved, partial)
	if err != nil {
		t.Fatal(err)
	}
	c = streamClient(t, answerWith(10, strings.NewReader("hello"), nil))
	if _, failure, _ := getObjectCall(t, c, download); failure == nil {
		t.Fatal("a short download was a success")
	}
	_ = download.Abort()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "in.txt" && e.Name() != "out.bin" {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}
