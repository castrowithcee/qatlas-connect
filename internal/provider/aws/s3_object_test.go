package aws

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var s3ObjectToolIDs = []string{"aws.s3.object.list", "aws.s3.object.list-versions", "aws.s3.object.head", "aws.s3.object.get-attributes",
	"aws.s3.object.copy", "aws.s3.object.delete", "aws.s3.object.delete-many", "aws.s3.object.restore", "aws.s3.object-tagging.get",
	"aws.s3.object-tagging.put", "aws.s3.object-tagging.delete", "aws.s3.object-acl.get"}

func s3ObjectEnv(t *testing.T, f *fakeAWS) *env {
	t.Helper()
	return newConnEnv(t, f, baseValues(), config.Connection{Tools: s3ObjectToolIDs,
		Targets: []string{"account/" + testAccount, "region/" + testRegion}})
}

func queryOf(t *testing.T, raw string) url.Values {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// Every object tool sends its own request to the bucket of the bound account with the bound account as the
// expected owner, takes no owner argument, and needs the confirmation before any request when it changes something.
func TestS3ObjectToolsSendOwnerAndNeedConfirmation(t *testing.T) {
	cases := []struct {
		id, args, method, path, query string
		write                         bool
		reply                         reply
	}{
		{"aws.s3.object.list", `{"bucket":"my-bucket"}`, "GET", "/", "list-type", false, xmlReply(`<ListBucketResult ` + s3NS + `><IsTruncated>false</IsTruncated></ListBucketResult>`)},
		{"aws.s3.object.list-versions", `{"bucket":"my-bucket"}`, "GET", "/", "versions", false, xmlReply(`<ListVersionsResult ` + s3NS + `><IsTruncated>false</IsTruncated></ListVersionsResult>`)},
		{"aws.s3.object.head", `{"bucket":"my-bucket","key":"reports/2026.csv"}`, "HEAD", "/reports/2026.csv", "", false, reply{status: 200, header: map[string]string{"Content-Length": "5", "ETag": `"abc"`}}},
		{"aws.s3.object.get-attributes", `{"bucket":"my-bucket","key":"reports/2026.csv","object_attributes":["ETag"]}`, "GET", "/reports/2026.csv", "attributes", false,
			xmlReply(`<GetObjectAttributesResponse ` + s3NS + `><ETag>abc</ETag></GetObjectAttributesResponse>`)},
		{"aws.s3.object.copy", `{"bucket":"my-bucket","key":"b/2026.csv","copy_source":"my-bucket/reports/2026.csv"}`, "PUT", "/b/2026.csv", "x-id", true,
			xmlReply(`<CopyObjectResult ` + s3NS + `><ETag>"abc"</ETag><LastModified>2026-01-02T03:04:05.000Z</LastModified></CopyObjectResult>`)},
		{"aws.s3.object.delete", `{"bucket":"my-bucket","key":"reports/old.csv","version_id":"v1"}`, "DELETE", "/reports/old.csv", "versionId", true, reply{status: 204}},
		{"aws.s3.object.delete-many", `{"bucket":"my-bucket","delete":{"objects":[{"key":"a.txt"},{"key":"b.txt"}]}}`, "POST", "/", "delete", true,
			xmlReply(`<DeleteResult ` + s3NS + `><Deleted><Key>a.txt</Key></Deleted><Deleted><Key>b.txt</Key></Deleted></DeleteResult>`)},
		{"aws.s3.object.restore", `{"bucket":"my-bucket","key":"archive/2020.csv","restore_request":{"days":7,"glacier_job_parameters":{"tier":"Standard"}}}`, "POST", "/archive/2020.csv", "restore", true, reply{status: 202}},
		{"aws.s3.object-tagging.get", `{"bucket":"my-bucket","key":"reports/2026.csv"}`, "GET", "/reports/2026.csv", "tagging", false,
			xmlReply(`<Tagging ` + s3NS + `><TagSet><Tag><Key>team</Key><Value>data</Value></Tag></TagSet></Tagging>`)},
		{"aws.s3.object-tagging.put", `{"bucket":"my-bucket","key":"reports/2026.csv","tagging":{"tag_set":[{"key":"team","value":"a & b"}]}}`, "PUT", "/reports/2026.csv", "tagging", true, reply{status: 200}},
		{"aws.s3.object-tagging.delete", `{"bucket":"my-bucket","key":"reports/2026.csv"}`, "DELETE", "/reports/2026.csv", "tagging", true, reply{status: 204}},
		{"aws.s3.object-acl.get", `{"bucket":"my-bucket","key":"reports/2026.csv"}`, "GET", "/reports/2026.csv", "acl", false,
			xmlReply(`<AccessControlPolicy ` + s3NS + `><Owner><ID>owner-id</ID></Owner><AccessControlList></AccessControlList></AccessControlPolicy>`)},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			f := restAnswer(c.reply)
			e := s3ObjectEnv(t, f)
			if c.write {
				if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
					t.Fatalf("without confirmation: %v", err)
				}
				if len(f.requests) != 0 {
					t.Fatalf("an unconfirmed call reached the network: %v", f.actions())
				}
			}
			text, err := e.invokeConfirmed(c.id, c.args, true)
			if err != nil {
				t.Fatal(err)
			}
			if got := decodeResult(t, text); got["region"] != testRegion {
				t.Errorf("result = %s", text)
			}
			reqs := restRequests(f)
			if len(reqs) != 1 {
				t.Fatalf("requests = %d, want 1", len(reqs))
			}
			r := reqs[0]
			if _, has := queryOf(t, r.rawQuery)[c.query]; r.method != c.method || r.path != c.path || (c.query != "" && !has) ||
				r.host != "my-bucket.s3."+testRegion+".amazonaws.com" || r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount ||
				r.header.Get("X-Amz-Source-Expected-Bucket-Owner") != map[bool]string{true: testAccount, false: ""}[c.id == "aws.s3.object.copy"] {
				t.Errorf("request = %+v", r)
			}
			before := len(f.requests)
			bad := strings.Replace(c.args, `{"bucket"`, `{"expected_bucket_owner":"999999999999","bucket"`, 1)
			if _, err := e.invokeConfirmed(c.id, bad, true); err == nil {
				t.Error("an owner argument was accepted")
			}
			if len(f.requests) != before {
				t.Error("a refused call was sent")
			}
		})
	}
}

func s3ListPage(keys []string, truncated bool, next, prefixes string) string {
	var b strings.Builder
	b.WriteString(`<ListBucketResult ` + s3NS + `><Name>my-bucket</Name><Prefix></Prefix><KeyCount>` + strconv.Itoa(len(keys)) + `</KeyCount>`)
	b.WriteString(`<IsTruncated>` + strconv.FormatBool(truncated) + `</IsTruncated>`)
	if next != "" {
		b.WriteString(`<NextContinuationToken>` + next + `</NextContinuationToken>`)
	}
	for _, k := range keys {
		b.WriteString(`<Contents><Key>` + k + `</Key><Size>3</Size><StorageClass>STANDARD</StorageClass></Contents>`)
	}
	b.WriteString(prefixes + `</ListBucketResult>`)
	return b.String()
}

// The list pages by the continuation token, which stays in the cursor; a delimiter, prefix, and page size reach S3.
func TestS3ObjectListPagesByCursor(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		switch queryOf(t, r.rawQuery).Get("continuation-token") {
		case "":
			return xmlReply(s3ListPage([]string{"a/1"}, true, "tok-1", `<CommonPrefixes><Prefix>a/</Prefix></CommonPrefixes>`))
		case "tok-1":
			return xmlReply(s3ListPage([]string{"a/2"}, true, "tok-2", ""))
		}
		return xmlReply(s3ListPage([]string{"a/3"}, false, "", ""))
	})
	e := s3ObjectEnv(t, f)
	cursor, pages := "", 0
	for {
		args := `{"bucket":"my-bucket","prefix":"a/","delimiter":"/","page_size":1}`
		if cursor != "" {
			args = `{"bucket":"my-bucket","prefix":"a/","delimiter":"/","page_size":1,"cursor":"` + cursor + `"}`
		}
		text, err := e.invoke("aws.s3.object.list", args)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		got := decodeResult(t, text)
		contents, _ := got["contents"].([]any)
		if len(contents) != 1 || contents[0].(map[string]any)["key"] != "a/"+strconv.Itoa(pages+1) {
			t.Errorf("page %d = %s", pages, text)
		}
		if pages == 0 {
			if p, _ := got["common_prefixes"].([]any); len(p) != 1 {
				t.Errorf("common prefixes = %s", text)
			}
		}
		pages++
		next, _ := got["next_cursor"].(string)
		if got["has_more"] != (next != "") || strings.Contains(next, "tok-") {
			t.Errorf("page %d: has_more/next = %v / %q", pages, got["has_more"], next)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3", pages)
	}
	reqs := restRequests(f)
	q := queryOf(t, reqs[2].rawQuery)
	if q.Get("continuation-token") != "tok-2" || q.Get("prefix") != "a/" || q.Get("delimiter") != "/" || q.Get("max-keys") != "1" || q.Get("list-type") != "2" {
		t.Errorf("last query = %v", q)
	}
	before := len(f.requests)
	if _, err := e.invoke("aws.s3.object.list", `{"bucket":"other-bucket","cursor":"`+cursor+`"}`); err != nil && len(f.requests) != before {
		t.Errorf("a cursor of another bucket reached the network: %v", err)
	}
}

// The version list continues at two markers held by one cursor.
func TestS3ObjectListVersionsPagesByMarkers(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		q := queryOf(t, r.rawQuery)
		switch q.Get("key-marker") {
		case "":
			return xmlReply(`<ListVersionsResult ` + s3NS + `><IsTruncated>true</IsTruncated><NextKeyMarker>k1</NextKeyMarker><NextVersionIdMarker>v1</NextVersionIdMarker>` +
				`<Version><Key>k0</Key><VersionId>v0</VersionId><IsLatest>true</IsLatest><Size>1</Size></Version></ListVersionsResult>`)
		case "k1":
			return xmlReply(`<ListVersionsResult ` + s3NS + `><IsTruncated>true</IsTruncated><NextKeyMarker>k2</NextKeyMarker>` +
				`<DeleteMarker><Key>k1</Key><VersionId>v1</VersionId><IsLatest>true</IsLatest></DeleteMarker></ListVersionsResult>`)
		}
		return xmlReply(`<ListVersionsResult ` + s3NS + `><IsTruncated>false</IsTruncated></ListVersionsResult>`)
	})
	e := s3ObjectEnv(t, f)
	cursor, pages := "", 0
	for {
		args := `{"bucket":"my-bucket"}`
		if cursor != "" {
			args = `{"bucket":"my-bucket","cursor":"` + cursor + `"}`
		}
		text, err := e.invoke("aws.s3.object.list-versions", args)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		got := decodeResult(t, text)
		pages++
		if pages == 1 {
			if v, _ := got["versions"].([]any); len(v) != 1 {
				t.Errorf("versions = %s", text)
			}
		}
		next, _ := got["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3", pages)
	}
	reqs := restRequests(f)
	if q := queryOf(t, reqs[1].rawQuery); q.Get("key-marker") != "k1" || q.Get("version-id-marker") != "v1" {
		t.Errorf("second query = %v", q)
	}
	if q := queryOf(t, reqs[2].rawQuery); q.Get("key-marker") != "k2" || q.Has("version-id-marker") {
		t.Errorf("third query = %v", q)
	}
}

// A copy names the source in the bound account as expected owner of both buckets, reports an error that S3
// answers with a success status, and refuses a source it cannot bind to a bucket before any request.
func TestS3ObjectCopySource(t *testing.T) {
	const args = `{"bucket":"my-bucket","key":"b/2026.csv","copy_source":"my-bucket/reports/2026.csv?versionId=v1"}`
	f := restAnswer(xmlReply(`<CopyObjectResult ` + s3NS + `><ETag>"abc"</ETag></CopyObjectResult>`))
	e := s3ObjectEnv(t, f)
	if _, err := e.invokeConfirmed("aws.s3.object.copy", args, true); err != nil {
		t.Fatal(err)
	}
	r := restRequests(f)[0]
	if r.header.Get("X-Amz-Copy-Source") != "my-bucket/reports/2026.csv?versionId=v1" ||
		r.header.Get("X-Amz-Source-Expected-Bucket-Owner") != testAccount || r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
		t.Errorf("headers = %v", r.header)
	}

	// An error element in a success status is an error, and the copy is not repeated.
	f = restAnswer(xmlReply(`<Error><Code>InternalError</Code><Message>try again</Message></Error>`))
	e = s3ObjectEnv(t, f)
	_, err := e.invokeConfirmed("aws.s3.object.copy", args, true)
	var failed *provider.Error
	if !errorsAs(err, &failed) || failed.Class != provider.ClassProviderError {
		t.Fatalf("error in a success status: %v", err)
	}
	if n := len(restRequests(f)); n != 1 {
		t.Errorf("requests = %d, a copy was repeated", n)
	}

	// A bucket of another account passes the name check; S3 refuses it on the expected-owner header.
	f = restAnswer(reply{status: 403, header: map[string]string{"Content-Type": "application/xml"}, body: `<Error><Code>AccessDenied</Code><Message>m</Message></Error>`})
	e = s3ObjectEnv(t, f)
	_, err = e.invokeConfirmed("aws.s3.object.copy", `{"bucket":"my-bucket","key":"k","copy_source":"foreign-bucket/k"}`, true)
	if classOf(err) != provider.ClassPermission {
		t.Errorf("foreign source: %v", err)
	}
	if r := restRequests(f)[0]; r.header.Get("X-Amz-Source-Expected-Bucket-Owner") != testAccount {
		t.Errorf("headers = %v", r.header)
	}

	f = restAnswer(reply{status: 200})
	e = s3ObjectEnv(t, f)
	for _, c := range []struct{ name, extra string }{
		{"access point ARN", `"copy_source":"arn:aws:s3:eu-central-1:999999999999:accesspoint/ap/object/k"`},
		{"outpost ARN", `"copy_source":"arn:aws:s3-outposts:eu-central-1:999999999999:outpost/o/accesspoint/ap/object/k"`},
		{"access point alias", `"copy_source":"ap-x-s3alias/k"`},
		{"no key", `"copy_source":"my-bucket"`},
		{"empty key", `"copy_source":"my-bucket/"`},
		{"bad bucket", `"copy_source":"My Bucket/k"`},
		{"other query", `"copy_source":"my-bucket/k?x=1"`},
		{"empty version", `"copy_source":"my-bucket/k?versionId="`},
		{"extra query member", `"copy_source":"my-bucket/k?versionId=v&x=1"`},
		{"source owner", `"copy_source":"my-bucket/k","expected_source_bucket_owner":"999999999999"`},
		{"canned ACL", `"copy_source":"my-bucket/k","acl":"public-read"`},
		{"grant", `"copy_source":"my-bucket/k","grant_read":"uri=x"`},
		{"grant ACP", `"copy_source":"my-bucket/k","grant_read_acp":"uri=x"`},
		{"SSE-C", `"copy_source":"my-bucket/k","sse_customer_key":"x","sse_customer_algorithm":"AES256"`},
		{"copy source SSE-C", `"copy_source":"my-bucket/k","copy_source_sse_customer_key":"x"`},
		{"website redirect", `"copy_source":"my-bucket/k","website_redirect_location":"https://example.org/"`},
		{"object lock", `"copy_source":"my-bucket/k","object_lock_mode":"COMPLIANCE"`},
		{"request payer", `"copy_source":"my-bucket/k","request_payer":"requester"`},
		{"bare KMS key", `"copy_source":"my-bucket/k","server_side_encryption":"aws:kms","ssekms_key_id":"1234abcd-12ab-34cd-56ef-1234567890ab"`},
		{"foreign KMS key", `"copy_source":"my-bucket/k","server_side_encryption":"aws:kms","ssekms_key_id":"arn:aws:kms:eu-central-1:999999999999:key/a"`},
		{"fsx encryption", `"copy_source":"my-bucket/k","server_side_encryption":"aws:fsx"`},
	} {
		if _, err := e.invokeConfirmed("aws.s3.object.copy", `{"bucket":"my-bucket","key":"k",`+c.extra+`}`, true); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
	if n := len(f.requests); n != 0 {
		t.Errorf("a refused copy reached the network: %v", f.actions())
	}
	own := `"server_side_encryption":"aws:kms","ssekms_key_id":"arn:aws:kms:eu-central-1:` + testAccount + `:key/a"`
	if _, err := e.invokeConfirmed("aws.s3.object.copy", `{"bucket":"my-bucket","key":"k","copy_source":"/my-bucket/k",`+own+`}`, true); err != nil {
		t.Errorf("own KMS key and a leading slash: %v", err)
	}
}

// DeleteObjects answers success with the keys it could not delete; the tool returns both lists, and takes 1000 keys at most.
func TestS3ObjectDeleteManyPartialErrors(t *testing.T) {
	f := restAnswer(xmlReply(`<DeleteResult ` + s3NS + `><Deleted><Key>a.txt</Key></Deleted>` +
		`<Error><Key>b.txt</Key><Code>AccessDenied</Code><Message>Access Denied</Message></Error></DeleteResult>`))
	e := s3ObjectEnv(t, f)
	text, err := e.invokeConfirmed("aws.s3.object.delete-many", `{"bucket":"my-bucket","delete":{"objects":[{"key":"a.txt"},{"key":"b.txt","version_id":"v2"}],"quiet":false}}`, true)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	deleted, _ := got["deleted"].([]any)
	failures, _ := got["errors"].([]any)
	if len(deleted) != 1 || len(failures) != 1 || failures[0].(map[string]any)["code"] != "AccessDenied" || failures[0].(map[string]any)["key"] != "b.txt" {
		t.Errorf("result = %s", text)
	}
	r := restRequests(f)[0]
	if !strings.Contains(r.body, "<Key>a.txt</Key>") || !strings.Contains(r.body, "<VersionId>v2</VersionId>") ||
		(r.header.Get("Content-MD5") == "" && r.header.Get("X-Amz-Sdk-Checksum-Algorithm") == "" && r.header.Get("X-Amz-Checksum-Crc32") == "") {
		t.Errorf("request = %+v", r)
	}

	keys := func(n int) string {
		items := make([]string, n)
		for i := range items {
			items[i] = `{"key":"k` + strconv.Itoa(i) + `"}`
		}
		return `{"bucket":"my-bucket","delete":{"objects":[` + strings.Join(items, ",") + `]}}`
	}
	before := len(f.requests)
	for _, args := range []string{keys(1001), `{"bucket":"my-bucket","delete":{"objects":[{"key":"a"}]},"bypass_governance_retention":true}`,
		`{"bucket":"my-bucket","delete":{"objects":[{"key":"a"}]},"mfa":"123 456"}`} {
		if _, err := e.invokeConfirmed("aws.s3.object.delete-many", args, true); err == nil {
			t.Errorf("%.80s was accepted", args)
		}
	}
	if len(f.requests) != before {
		t.Error("a refused delete reached the network")
	}
	if _, err := e.invokeConfirmed("aws.s3.object.delete-many", keys(1000), true); err != nil {
		t.Errorf("1000 keys: %v", err)
	}
}

// Missing keys and buckets are not-found, a failed precondition is a provider error, and none is repeated.
func TestS3ObjectErrors(t *testing.T) {
	for _, c := range []struct {
		id, args, code string
		status         int
		class          provider.Class
	}{
		{"aws.s3.object-tagging.get", `{"bucket":"my-bucket","key":"k"}`, "NoSuchKey", 404, provider.ClassNotFound},
		{"aws.s3.object-acl.get", `{"bucket":"my-bucket","key":"k"}`, "NoSuchKey", 404, provider.ClassNotFound},
		{"aws.s3.object.list", `{"bucket":"my-bucket"}`, "NoSuchBucket", 404, provider.ClassNotFound},
		{"aws.s3.object.delete-many", `{"bucket":"my-bucket","delete":{"objects":[{"key":"k"}]}}`, "NoSuchBucket", 404, provider.ClassNotFound},
		{"aws.s3.object.copy", `{"bucket":"my-bucket","key":"k","copy_source":"my-bucket/s","if_none_match":"*"}`, "PreconditionFailed", 412, provider.ClassProviderError},
		{"aws.s3.object.delete", `{"bucket":"my-bucket","key":"k","if_match":"\"x\""}`, "PreconditionFailed", 412, provider.ClassProviderError},
		{"aws.s3.object.restore", `{"bucket":"my-bucket","key":"k","restore_request":{"days":1}}`, "RestoreAlreadyInProgress", 409, provider.ClassProviderError},
	} {
		t.Run(c.id+"/"+c.code, func(t *testing.T) {
			f := restAnswer(reply{status: c.status, header: map[string]string{"Content-Type": "application/xml"},
				body: `<Error><Code>` + c.code + `</Code><Message>m</Message></Error>`})
			_, err := s3ObjectEnv(t, f).invokeConfirmed(c.id, c.args, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class {
				t.Fatalf("err = %v, want class %s", err, c.class)
			}
			if failed.Original == nil || len(failed.Original.Entries) != 1 || failed.Original.Entries[0].Code != c.code {
				t.Errorf("original = %+v", failed.Original)
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("requests = %d, a failed call was repeated", n)
			}
		})
	}
	// HeadObject has no error body: the status says it.
	f := restAnswer(reply{status: 404})
	if _, err := s3ObjectEnv(t, f).invoke("aws.s3.object.head", `{"bucket":"my-bucket","key":"k"}`); classOf(err) != provider.ClassNotFound {
		t.Errorf("head of a missing key: %v", err)
	}
}

// A bucket in another region is an error that names the right one, for every object tool; it is not followed.
func TestS3ObjectWrongRegionNamesTheRightOne(t *testing.T) {
	f := restAnswer(reply{status: 301, header: map[string]string{"X-Amz-Bucket-Region": "us-west-2", "Location": "https://other.example.org/"},
		body: `<Error><Code>PermanentRedirect</Code><Message>x</Message><Endpoint>other.example.org</Endpoint></Error>`})
	e := s3ObjectEnv(t, f)
	for _, id := range s3ObjectToolIDs {
		args := `{"bucket":"my-bucket","key":"k","object_attributes":["ETag"],"copy_source":"my-bucket/s","delete":{"objects":[{"key":"k"}]},` +
			`"restore_request":{"days":1},"tagging":{"tag_set":[{"key":"a","value":"b"}]}}`
		switch id {
		case "aws.s3.object.list", "aws.s3.object.list-versions":
			args = `{"bucket":"my-bucket"}`
		case "aws.s3.object.delete-many":
			args = `{"bucket":"my-bucket","delete":{"objects":[{"key":"k"}]}}`
		case "aws.s3.object.copy":
			args = `{"bucket":"my-bucket","key":"k","copy_source":"my-bucket/s"}`
		case "aws.s3.object.restore":
			args = `{"bucket":"my-bucket","key":"k","restore_request":{"days":1}}`
		case "aws.s3.object-tagging.put":
			args = `{"bucket":"my-bucket","key":"k","tagging":{"tag_set":[{"key":"a","value":"b"}]}}`
		case "aws.s3.object.get-attributes":
			args = `{"bucket":"my-bucket","key":"k","object_attributes":["ETag"]}`
		default:
			args = `{"bucket":"my-bucket","key":"k"}`
		}
		_, err := e.invokeConfirmed(id, args, true)
		var failed *provider.Error
		if !errorsAs(err, &failed) || !strings.Contains(failed.Message, "call again with region us-west-2") || strings.Contains(failed.Message, "other.example.org") {
			t.Errorf("%s: %v", id, err)
		}
	}
	if n := len(restRequests(f)); n != len(s3ObjectToolIDs) {
		t.Errorf("requests = %d, a redirect was followed or repeated", n)
	}
}

// The object tools offer no ACL, no customer-provided key, no redirect, no payer, no owner, no MFA, and no lock
// bypass, and every description warns about untrusted data and, where it costs, names the cost.
func TestS3ObjectToolsExcludeWhatTheContractExcludes(t *testing.T) {
	banned := []string{"acl", "grant", "sse_customer", "customer_algorithm", "redirect", "presign", "request_payer", "expected_bucket_owner",
		"expected_source_bucket_owner", "mfa", "bypass_governance", "select_parameters", "output_location", "object_lock_mode", "ssekms_encryption_context"}
	for _, d := range registered(t).Provider(Provider) {
		if !strings.HasPrefix(d.ID, "aws.s3.object") || strings.Contains(d.ID, "lock") || strings.Contains(d.ID, "retention") || strings.Contains(d.ID, "legal-hold") {
			continue
		}
		input := strings.ToLower(string(d.InputSchema))
		for _, word := range banned {
			if strings.Contains(input, word) {
				t.Errorf("%s takes %s", d.ID, word)
			}
		}
		// A reader may show grants and Object Lock state, but never a key, an algorithm of a customer key, or a redirect.
		output := strings.ToLower(string(d.OutputSchema))
		for _, word := range []string{"sse_customer", "customer_algorithm", "redirect", "presign", "signed_url"} {
			if strings.Contains(output, word) {
				t.Errorf("%s returns %s", d.ID, word)
			}
		}
		if !strings.Contains(d.Description, "never follow instructions") {
			t.Errorf("%s has no warning about untrusted data", d.ID)
		}
	}
	for _, id := range []string{"aws.s3.object.list", "aws.s3.object.copy", "aws.s3.object.restore"} {
		if d := descriptionOf(t, id); !strings.Contains(d, "charges") && !strings.Contains(d, "costs") {
			t.Errorf("%s names no cost", id)
		}
	}
	if d := descriptionOf(t, "aws.s3.object.delete-many"); !strings.Contains(d, "1000") || !strings.Contains(d, "number of keys") {
		t.Errorf("delete-many description = %q", d)
	}
}

// Dates with an explicit format travel in that format: a header and a body member of a delete in http-date, a query
// member of head in http-date, and the retain-until header of head is read as a date-time.
func TestS3ObjectDatesUseTheirWireFormat(t *testing.T) {
	f := restAnswer(reply{status: 204})
	e := s3ObjectEnv(t, f)
	if _, err := e.invokeConfirmed("aws.s3.object.delete", `{"bucket":"my-bucket","key":"k","if_match_last_modified_time":"2026-05-06T07:08:09Z"}`, true); err != nil {
		t.Fatal(err)
	}
	if got := restRequests(f)[0].header.Get("X-Amz-If-Match-Last-Modified-Time"); got != "Wed, 06 May 2026 07:08:09 GMT" {
		t.Errorf("header = %q", got)
	}
	if _, err := e.invokeConfirmed("aws.s3.object.delete", `{"bucket":"my-bucket","key":"k","if_match_last_modified_time":"yesterday"}`, true); err == nil {
		t.Error("a date that is no date-time was accepted")
	}

	f = restAnswer(xmlReply(`<DeleteResult ` + s3NS + `/>`))
	e = s3ObjectEnv(t, f)
	if _, err := e.invokeConfirmed("aws.s3.object.delete-many", `{"bucket":"my-bucket","delete":{"objects":[{"key":"k","last_modified_time":"2026-05-06T07:08:09Z"}]}}`, true); err != nil {
		t.Fatal(err)
	}
	if body := restRequests(f)[0].body; !strings.Contains(body, "<LastModifiedTime>Wed, 06 May 2026 07:08:09 GMT</LastModifiedTime>") {
		t.Errorf("body = %s", body)
	}

	f = restAnswer(reply{status: 200, header: map[string]string{"Content-Length": "3", "X-Amz-Object-Lock-Mode": "GOVERNANCE",
		"X-Amz-Object-Lock-Retain-Until-Date": "2026-05-06T07:08:09.000Z"}})
	e = s3ObjectEnv(t, f)
	text, err := e.invoke("aws.s3.object.head", `{"bucket":"my-bucket","key":"k","response_expires":"2026-05-06T07:08:09Z"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeResult(t, text); got["object_lock_retain_until_date"] != "2026-05-06T07:08:09Z" || got["object_lock_mode"] != "GOVERNANCE" {
		t.Errorf("result = %s", text)
	}
	if q := restRequests(f)[0].rawQuery; !strings.Contains(q, "response-expires=") || strings.Contains(q, "2026-05-06T") {
		t.Errorf("query = %q, want an http-date", q)
	}
}
