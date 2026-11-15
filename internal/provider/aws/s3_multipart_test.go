package aws

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	mpCreate   = "aws.s3.multipart-upload.create"
	mpPart     = "aws.s3.multipart-upload.upload-part"
	mpCopy     = "aws.s3.multipart-upload.upload-part-copy"
	mpComplete = "aws.s3.multipart-upload.complete"
	mpAbort    = "aws.s3.multipart-upload.abort"
	mpList     = "aws.s3.multipart-upload.list"
	mpParts    = "aws.s3.multipart-upload.list-parts"
)

var multipartIDs = []string{mpCreate, mpPart, mpCopy, mpComplete, mpAbort, mpList, mpParts}

func multipartEnv(t *testing.T, answer func(s3Seen) streamReply) (*env, *s3Stream, transferDirs) {
	t.Helper()
	dirs := transferDirs{read: t.TempDir(), write: t.TempDir(), other: t.TempDir()}
	f := direct(nil)
	e := newConnEnv(t, f, baseValues(), config.Connection{Tools: multipartIDs, Files: config.Files{Read: []string{dirs.read}}})
	s := &s3Stream{inner: f, answer: answer}
	transport = s
	return e, s, dirs
}

func xmlOK(body string) streamReply {
	return textReply(200, map[string]string{"Content-Type": "application/xml"}, body)
}

func queryValues(t *testing.T, s3 s3Seen) url.Values {
	t.Helper()
	q, err := url.ParseQuery(s3.rawQuery)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// mpAnswer is a small S3 for the multipart tools: it answers by the request and keeps no state.
func mpAnswer(r s3Seen) streamReply {
	q, _ := url.ParseQuery(r.rawQuery)
	switch {
	case r.method == "POST" && q.Has("uploads"):
		return xmlOK(`<InitiateMultipartUploadResult ` + s3NS + `><Bucket>my-bucket</Bucket><Key>big.bin</Key><UploadId>UP1</UploadId></InitiateMultipartUploadResult>`)
	case r.method == "PUT" && q.Has("partNumber") && r.header.Get("X-Amz-Copy-Source") != "":
		return xmlOK(`<CopyPartResult ` + s3NS + `><ETag>"copy-` + q.Get("partNumber") + `"</ETag><LastModified>2026-11-01T10:00:00.000Z</LastModified></CopyPartResult>`)
	case r.method == "PUT" && q.Has("partNumber"):
		return streamReply{status: 200, body: strings.NewReader(""), header: map[string]string{
			"ETag": `"etag-` + q.Get("partNumber") + `"`, "X-Amz-Checksum-Sha256": r.header.Get("X-Amz-Checksum-Sha256")}}
	case r.method == "POST" && q.Has("uploadId"):
		return xmlOK(`<CompleteMultipartUploadResult ` + s3NS + `><Location>https://my-bucket.s3.amazonaws.com/big.bin</Location><Bucket>my-bucket</Bucket>` +
			`<Key>big.bin</Key><ETag>"final-3"</ETag></CompleteMultipartUploadResult>`)
	case r.method == "DELETE":
		return streamReply{status: 204, body: strings.NewReader("")}
	}
	return xmlError(400, "InvalidRequest")
}

// Create, three parts from three ranges of one file, complete: every part carries the SHA-256 of its range only,
// the content is signed as an unsigned stream, and complete sends the parts as the list says.
func TestS3MultipartFlow(t *testing.T) {
	e, s, dirs := multipartEnv(t, mpAnswer)
	const content = "0123456789abcdefghijKLMNOPQRST"
	path := writeFile(t, dirs.read, "big.bin", content)
	args := func(part int, extra string) string {
		return `{"bucket":"my-bucket","key":"big.bin","upload_id":"UP1","part_number":` + strconv.Itoa(part) + `,"local_path":"` + jsonText(path) + `"` + extra + `}`
	}

	text, err := e.invokeConfirmed(mpCreate, `{"bucket":"my-bucket","key":"big.bin","content_type":"application/octet-stream","metadata":{"k":"v"}}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeResult(t, text); got["upload_id"] != "UP1" || got["key"] != "big.bin" {
		t.Errorf("create result = %v", got)
	}
	create := s.requests()[0]
	if create.method != "POST" || create.rawQuery != "uploads=" && create.rawQuery != "uploads" || create.path != "/big.bin" ||
		create.header.Get("X-Amz-Checksum-Algorithm") != "SHA256" || create.header.Get("X-Amz-Meta-K") != "v" ||
		create.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
		t.Errorf("create request = %+v", create)
	}

	ranges := []struct {
		args string
		want string
	}{
		{args(1, `,"offset":0,"length":10`), content[:10]},
		{args(2, `,"offset":10,"length":10`), content[10:20]},
		{args(3, `,"offset":20`), content[20:]},
	}
	var etags []string
	for i, r := range ranges {
		text, err := e.invokeConfirmed(mpPart, r.args, true)
		if err != nil {
			t.Fatalf("part %d: %v", i+1, err)
		}
		sum, b64 := sumOf(r.want)
		got := decodeResult(t, text)
		if got["sha256"] != sum || got["size"] != float64(len(r.want)) || got["etag"] != `"etag-`+strconv.Itoa(i+1)+`"` || got["checksum_sha256"] != b64 {
			t.Errorf("part %d result = %v", i+1, got)
		}
		if strings.Contains(text, r.want) {
			t.Errorf("part %d result holds the content: %s", i+1, text)
		}
		req := s.requests()[i+1]
		q := queryValues(t, req)
		if req.method != "PUT" || q.Get("partNumber") != strconv.Itoa(i+1) || q.Get("uploadId") != "UP1" || req.bytes != int64(len(r.want)) ||
			req.contentLength != int64(len(r.want)) || req.sha256 != sum || req.header.Get("X-Amz-Checksum-Sha256") != b64 ||
			req.header.Get("X-Amz-Content-Sha256") != "UNSIGNED-PAYLOAD" || req.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
			t.Errorf("part %d request = %+v", i+1, req)
		}
		etags = append(etags, got["etag"].(string))
	}

	var list []string
	for i, tag := range etags {
		_, b64 := sumOf(ranges[i].want)
		list = append(list, `{"part_number":`+strconv.Itoa(i+1)+`,"e_tag":`+strconv.Quote(tag)+`,"checksum_sha256":"`+b64+`"}`)
	}
	text, err = e.invokeConfirmed(mpComplete, `{"bucket":"my-bucket","key":"big.bin","upload_id":"UP1","multipart_upload":{"parts":[`+strings.Join(list, ",")+`]}}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeResult(t, text); got["etag"] != `"final-3"` || got["bucket"] != "my-bucket" {
		t.Errorf("complete result = %v", got)
	}
	done := s.requests()[4]
	if done.method != "POST" || queryValues(t, done).Get("uploadId") != "UP1" || len(s.requests()) != 5 {
		t.Errorf("complete request = %+v of %d", done, len(s.requests()))
	}
	for i := 1; i <= 3; i++ {
		part := `<Part><ETag>&#34;etag-` + strconv.Itoa(i) + `&#34;</ETag>`
		if !strings.Contains(done.body, part) && !strings.Contains(done.body, strings.ReplaceAll(part, "&#34;", `"`)) &&
			!strings.Contains(done.body, strings.ReplaceAll(part, "&#34;", "&quot;")) {
			t.Errorf("complete body lacks part %d: %s", i, done.body)
		}
		if !strings.Contains(done.body, "<PartNumber>"+strconv.Itoa(i)+"</PartNumber>") {
			t.Errorf("complete body lacks the number of part %d: %s", i, done.body)
		}
	}
	if a, b := strings.Index(done.body, "<PartNumber>1<"), strings.Index(done.body, "<PartNumber>3<"); a < 0 || b < a {
		t.Errorf("parts are not in the order given: %s", done.body)
	}
}

// The same part number sent again is another request that replaces the part; nothing deduplicates or refuses it.
func TestS3MultipartPartRepeated(t *testing.T) {
	e, s, dirs := multipartEnv(t, mpAnswer)
	path := writeFile(t, dirs.read, "a.bin", "0123456789")
	args := `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":1,"local_path":"` + jsonText(path) + `"}`
	for range 2 {
		if _, err := e.invokeConfirmed(mpPart, args, true); err != nil {
			t.Fatal(err)
		}
	}
	reqs := s.requests()
	if len(reqs) != 2 || reqs[0].rawQuery != reqs[1].rawQuery || reqs[0].sha256 != reqs[1].sha256 {
		t.Errorf("requests = %+v", reqs)
	}
}

// A part is one range of at most 100 MiB: exactly that much goes through; one byte more, a range outside the file,
// and a whole file that is too large are refused before anything is sent.
func TestS3MultipartPartLimits(t *testing.T) {
	e, s, dirs := multipartEnv(t, mpAnswer)
	big := sparseFile(t, dirs.read, "big.bin", maxMiB100+10)
	small := writeFile(t, dirs.read, "small.bin", "0123456789")
	base := func(path, extra string) string {
		return `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":1,"local_path":"` + jsonText(path) + `"` + extra + `}`
	}

	text, err := e.invokeConfirmed(mpPart, base(big, `,"offset":10,"length":`+strconv.FormatInt(maxMiB100, 10)), true)
	if err != nil {
		t.Fatalf("100 MiB part: %v", err)
	}
	got := decodeResult(t, text)
	reqs := s.requests()
	if got["size"] != float64(maxMiB100) || got["sha256"] != zerosSHA(maxMiB100) || len(reqs) != 1 || reqs[0].bytes != maxMiB100 {
		t.Errorf("result = %v, requests = %d", got, len(reqs))
	}

	for _, c := range []struct{ name, args string }{
		{"one byte over", base(big, `,"offset":0,"length":`+strconv.FormatInt(maxMiB100+1, 10))},
		{"whole file over", base(big, ``)},
		{"to the end over", base(big, `,"offset":5`)},
		{"offset beyond the file", base(small, `,"offset":10`)},
		{"length beyond the file", base(small, `,"offset":5,"length":6`)},
		{"negative offset", base(small, `,"offset":-1`)},
		{"zero length", base(small, `,"length":0`)},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := len(s.requests())
			_, err := e.invokeConfirmed(mpPart, c.args, true)
			if err == nil {
				t.Fatal("accepted")
			}
			if n := len(s.requests()) - before; n != 0 {
				t.Errorf("a refused part sent %d requests", n)
			}
		})
	}
}

// Part numbers run from 1 to 100 for a part from a file and for a copied part; the schema refuses the rest.
func TestS3MultipartPartNumbers(t *testing.T) {
	e, s, dirs := multipartEnv(t, mpAnswer)
	path := writeFile(t, dirs.read, "a.bin", "0123456789")
	part := func(n int) string {
		return `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":` + strconv.Itoa(n) + `,"local_path":"` + jsonText(path) + `"}`
	}
	cp := func(n int) string {
		return `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":` + strconv.Itoa(n) + `,"copy_source":"my-bucket/src","copy_source_range":"bytes=0-9"}`
	}
	for _, n := range []int{1, 100} {
		for id, args := range map[string]string{mpPart: part(n), mpCopy: cp(n)} {
			if _, err := e.invokeConfirmed(id, args, true); err != nil {
				t.Errorf("%s part %d: %v", id, n, err)
			}
		}
	}
	sent := len(s.requests())
	for _, n := range []int{0, 101, 10000, -1} {
		for id, args := range map[string]string{mpPart: part(n), mpCopy: cp(n)} {
			if _, err := e.invokeConfirmed(id, args, true); err == nil {
				t.Errorf("%s accepted part number %d", id, n)
			}
		}
	}
	if len(s.requests()) != sent {
		t.Errorf("a refused part number reached S3")
	}
}

// S3's refusals keep their class; EntityTooSmall at complete names the minimum size; no request is repeated.
func TestS3MultipartErrors(t *testing.T) {
	complete := `{"bucket":"my-bucket","key":"k","upload_id":"UP1","multipart_upload":{"parts":[{"part_number":1,"e_tag":"\"a\""}]}}`
	for _, c := range []struct {
		name   string
		id     string
		answer streamReply
		class  provider.Class
		want   string
	}{
		{"too small", mpComplete, xmlError(400, "EntityTooSmall"), provider.ClassProviderError, "at least 5 MiB"},
		{"wrong order", mpComplete, xmlError(400, "InvalidPartOrder"), provider.ClassProviderError, ""},
		{"invalid part", mpComplete, xmlError(400, "InvalidPart"), provider.ClassProviderError, ""},
		{"no such upload on complete", mpComplete, xmlError(404, "NoSuchUpload"), provider.ClassNotFound, ""},
		{"no such upload on part", mpPart, xmlError(404, "NoSuchUpload"), provider.ClassNotFound, ""},
		{"no such upload on copy", mpCopy, xmlError(404, "NoSuchUpload"), provider.ClassNotFound, ""},
		{"no such upload on abort", mpAbort, xmlError(404, "NoSuchUpload"), provider.ClassNotFound, ""},
		{"no such upload on list-parts", mpParts, xmlError(404, "NoSuchUpload"), provider.ClassNotFound, ""},
		{"access denied", mpCreate, xmlError(403, "AccessDenied"), provider.ClassPermission, ""},
		{"error in a success status", mpComplete, xmlOK(`<Error><Code>InternalError</Code><Message>try again</Message></Error>`), provider.ClassProviderError, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, s, dirs := multipartEnv(t, func(s3Seen) streamReply { return c.answer })
			path := writeFile(t, dirs.read, "a.bin", "x")
			args := map[string]string{
				mpCreate:   `{"bucket":"my-bucket","key":"k"}`,
				mpPart:     `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":1,"local_path":"` + jsonText(path) + `"}`,
				mpCopy:     `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":1,"copy_source":"my-bucket/s","copy_source_range":"bytes=0-9"}`,
				mpComplete: complete,
				mpAbort:    `{"bucket":"my-bucket","key":"k","upload_id":"UP1"}`,
				mpParts:    `{"bucket":"my-bucket","key":"k","upload_id":"UP1"}`,
			}[c.id]
			_, err := e.invokeConfirmed(c.id, args, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || !strings.Contains(failed.Message, c.want) {
				t.Fatalf("err = %v, want class %s containing %q", err, c.class, c.want)
			}
			if n := len(s.requests()); n != 1 {
				t.Errorf("requests = %d, a refusal was repeated", n)
			}
		})
	}
}

// Abort deletes by upload ID with the bound account as the expected owner.
func TestS3MultipartAbort(t *testing.T) {
	e, s, _ := multipartEnv(t, mpAnswer)
	if _, err := e.invokeConfirmed(mpAbort, `{"bucket":"my-bucket","key":"big.bin","upload_id":"UP1"}`, true); err != nil {
		t.Fatal(err)
	}
	r := s.requests()[0]
	if r.method != "DELETE" || r.path != "/big.bin" || queryValues(t, r).Get("uploadId") != "UP1" || r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
		t.Errorf("request = %+v", r)
	}
}

// The writes need confirmation and the lists do not; without it nothing is sent.
func TestS3MultipartConfirmation(t *testing.T) {
	e, s, dirs := multipartEnv(t, mpAnswer)
	path := writeFile(t, dirs.read, "a.bin", "x")
	for id, args := range map[string]string{
		mpCreate:   `{"bucket":"my-bucket","key":"k"}`,
		mpPart:     `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":1,"local_path":"` + jsonText(path) + `"}`,
		mpCopy:     `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":1,"copy_source":"my-bucket/s","copy_source_range":"bytes=0-9"}`,
		mpComplete: `{"bucket":"my-bucket","key":"k","upload_id":"UP1","multipart_upload":{"parts":[{"part_number":1,"e_tag":"a"}]}}`,
		mpAbort:    `{"bucket":"my-bucket","key":"k","upload_id":"UP1"}`,
	} {
		if _, err := e.invoke(id, args); !isConfirmationRequired(err) {
			t.Errorf("%s without confirmation: %v", id, err)
		}
	}
	if n := len(s.requests()); n != 0 {
		t.Errorf("%d requests without confirmation", n)
	}
}

// Both lists continue over several pages with the markers of S3.
func TestS3MultipartListsPage(t *testing.T) {
	e, s, _ := multipartEnv(t, func(r s3Seen) streamReply {
		q, _ := url.ParseQuery(r.rawQuery)
		if q.Has("uploads") {
			switch q.Get("key-marker") {
			case "":
				return xmlOK(`<ListMultipartUploadsResult ` + s3NS + `><Bucket>my-bucket</Bucket><IsTruncated>true</IsTruncated><NextKeyMarker>k1</NextKeyMarker><NextUploadIdMarker>u1</NextUploadIdMarker>` +
					`<Upload><Key>k0</Key><UploadId>u0</UploadId><Initiated>2026-11-01T10:00:00.000Z</Initiated><StorageClass>STANDARD</StorageClass></Upload></ListMultipartUploadsResult>`)
			case "k1":
				return xmlOK(`<ListMultipartUploadsResult ` + s3NS + `><Bucket>my-bucket</Bucket><IsTruncated>true</IsTruncated><NextKeyMarker>k2</NextKeyMarker>` +
					`<Upload><Key>k1</Key><UploadId>u1</UploadId></Upload></ListMultipartUploadsResult>`)
			}
			return xmlOK(`<ListMultipartUploadsResult ` + s3NS + `><Bucket>my-bucket</Bucket><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>`)
		}
		switch q.Get("part-number-marker") {
		case "":
			return xmlOK(`<ListPartsResult ` + s3NS + `><Bucket>my-bucket</Bucket><Key>k</Key><UploadId>UP1</UploadId><IsTruncated>true</IsTruncated><NextPartNumberMarker>1</NextPartNumberMarker>` +
				`<Part><PartNumber>1</PartNumber><ETag>"e1"</ETag><Size>10</Size></Part></ListPartsResult>`)
		case "1":
			return xmlOK(`<ListPartsResult ` + s3NS + `><Bucket>my-bucket</Bucket><Key>k</Key><UploadId>UP1</UploadId><IsTruncated>true</IsTruncated><NextPartNumberMarker>2</NextPartNumberMarker>` +
				`<Part><PartNumber>2</PartNumber><ETag>"e2"</ETag><Size>10</Size></Part></ListPartsResult>`)
		}
		return xmlOK(`<ListPartsResult ` + s3NS + `><Bucket>my-bucket</Bucket><Key>k</Key><UploadId>UP1</UploadId><IsTruncated>false</IsTruncated>` +
			`<Part><PartNumber>3</PartNumber><ETag>"e3"</ETag><Size>7</Size></Part></ListPartsResult>`)
	})
	for _, c := range []struct {
		id, args, list string
	}{
		{mpList, `{"bucket":"my-bucket"`, "uploads"},
		{mpParts, `{"bucket":"my-bucket","key":"k","upload_id":"UP1"`, "parts"},
	} {
		cursor, pages, entries := "", 0, 0
		for {
			args := c.args + `}`
			if cursor != "" {
				args = c.args + `,"cursor":"` + cursor + `"}`
			}
			text, err := e.invoke(c.id, args)
			if err != nil {
				t.Fatalf("%s page %d: %v", c.id, pages, err)
			}
			got := decodeResult(t, text)
			pages++
			list, _ := got[c.list].([]any)
			entries += len(list)
			if cursor, _ = got["next_cursor"].(string); cursor == "" {
				break
			}
		}
		if pages != 3 || entries != 2+map[string]int{mpList: 0, mpParts: 1}[c.id] {
			t.Errorf("%s: pages = %d, entries = %d", c.id, pages, entries)
		}
	}
	reqs := s.requests()
	if len(reqs) != 6 {
		t.Fatalf("requests = %d", len(reqs))
	}
	if q := queryValues(t, reqs[1]); q.Get("key-marker") != "k1" || q.Get("upload-id-marker") != "u1" {
		t.Errorf("second uploads query = %v", q)
	}
	if q := queryValues(t, reqs[2]); q.Get("key-marker") != "k2" || q.Has("upload-id-marker") {
		t.Errorf("third uploads query = %v", q)
	}
	if q := queryValues(t, reqs[4]); q.Get("part-number-marker") != "1" || q.Get("uploadId") != "UP1" {
		t.Errorf("second parts query = %v", q)
	}
	for _, r := range reqs {
		if r.method != "GET" || r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
			t.Errorf("request = %+v", r)
		}
	}
}

// A part copy names the source in the bound account as expected owner, needs a bounded range, and refuses a source
// it cannot bind to a bucket before any request; a source bucket of another account is S3's refusal.
func TestS3MultipartPartCopy(t *testing.T) {
	e, s, _ := multipartEnv(t, mpAnswer)
	text, err := e.invokeConfirmed(mpCopy, `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":2,`+
		`"copy_source":"my-bucket/reports/2026.csv?versionId=v1","copy_source_range":"bytes=0-104857599"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeResult(t, text); got["copy_part_result"] == nil {
		t.Errorf("result = %v", got)
	}
	r := s.requests()[0]
	if r.header.Get("X-Amz-Copy-Source") != "my-bucket/reports/2026.csv?versionId=v1" || r.header.Get("X-Amz-Copy-Source-Range") != "bytes=0-104857599" ||
		r.header.Get("X-Amz-Source-Expected-Bucket-Owner") != testAccount || r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount ||
		queryValues(t, r).Get("partNumber") != "2" {
		t.Errorf("request = %+v", r)
	}

	n := len(s.requests())
	base := func(source, rng string) string {
		extra := ""
		if rng != "" {
			extra = `,"copy_source_range":"` + rng + `"`
		}
		return `{"bucket":"my-bucket","key":"k","upload_id":"UP1","part_number":1,"copy_source":"` + source + `"` + extra + `}`
	}
	for _, c := range []struct{ name, args string }{
		{"no range", base("my-bucket/s", "")},
		{"over 100 MiB", base("my-bucket/s", "bytes=0-104857600")},
		{"over 100 MiB from an offset", base("my-bucket/s", "bytes=1000-105000000")},
		{"reversed", base("my-bucket/s", "bytes=9-0")},
		{"open ended", base("my-bucket/s", "bytes=10-")},
		{"suffix", base("my-bucket/s", "bytes=-10")},
		{"two ranges", base("my-bucket/s", "bytes=0-1,5-6")},
		{"not a range", base("my-bucket/s", "0-9")},
		{"access point", base("arn:aws:s3:eu-central-1:999999999999:accesspoint/ap/object/k", "bytes=0-9")},
		{"no key", base("my-bucket", "bytes=0-9")},
	} {
		if _, err := e.invokeConfirmed(mpCopy, c.args, true); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
	if len(s.requests()) != n {
		t.Errorf("a refused copy reached S3")
	}

	e, s, _ = multipartEnv(t, func(s3Seen) streamReply { return xmlError(403, "AccessDenied") })
	_, err = e.invokeConfirmed(mpCopy, base("foreign-bucket/k", "bytes=0-9"), true)
	if classOf(err) != provider.ClassPermission {
		t.Errorf("foreign source: %v", err)
	}
	if reqs := s.requests(); len(reqs) != 1 || reqs[0].header.Get("X-Amz-Source-Expected-Bucket-Owner") != testAccount {
		t.Errorf("requests = %+v", reqs)
	}
}

// Create fixes the checksum algorithm to the SHA-256 the parts carry, keeps ACLs and SSE-C out, and takes only
// KMS keys of the bound account.
func TestS3MultipartCreateArguments(t *testing.T) {
	e, s, _ := multipartEnv(t, mpAnswer)
	for _, args := range []string{
		`{"bucket":"my-bucket","key":"k","checksum_algorithm":"CRC32"}`,
		`{"bucket":"my-bucket","key":"k","acl":"public-read"}`,
		`{"bucket":"my-bucket","key":"k","sse_customer_key":"x"}`,
		`{"bucket":"my-bucket","key":"k","expected_bucket_owner":"210987654321"}`,
		`{"bucket":"my-bucket","key":"k","object_lock_mode":"COMPLIANCE"}`,
		`{"bucket":"my-bucket","key":"k","server_side_encryption":"aws:kms","ssekms_key_id":"arn:aws:kms:eu-central-1:999999999999:key/abc"}`,
	} {
		if _, err := e.invokeConfirmed(mpCreate, args, true); err == nil {
			t.Errorf("%s was accepted", args)
		}
	}
	if len(s.requests()) != 0 {
		t.Errorf("a refused create reached S3")
	}
	if _, err := e.invokeConfirmed(mpCreate, `{"bucket":"my-bucket","key":"k","checksum_algorithm":"SHA256","server_side_encryption":"aws:kms","ssekms_key_id":"arn:aws:kms:`+
		testRegion+`:`+testAccount+`:key/abc"}`, true); err != nil {
		t.Fatal(err)
	}
}

// The descriptions say what an open upload costs and what S3 expects of the parts.
func TestS3MultipartDescriptions(t *testing.T) {
	for _, id := range multipartIDs {
		i := -1
		for n, s := range generatedSpecs {
			if s.descriptor.ID == id {
				i = n
			}
		}
		if i < 0 {
			t.Fatalf("%s is not a tool", id)
		}
		d := generatedSpecs[i].descriptor
		want := map[string][]string{
			mpCreate:   {"AbortIncompleteMultipartUpload", "5 MiB", "100 MiB", "10 GiB"},
			mpPart:     {"AbortIncompleteMultipartUpload", "5 MiB", "100 MiB", "60 seconds"},
			mpCopy:     {"AbortIncompleteMultipartUpload", "5 MiB", "100 MiB"},
			mpComplete: {"AbortIncompleteMultipartUpload", "5 MiB", "EntityTooSmall"},
			mpAbort:    {"s3:AbortMultipartUpload"},
			mpList:     {"s3:ListBucketMultipartUploads"},
			mpParts:    {"s3:ListMultipartUploadParts"},
		}[id]
		for _, w := range want {
			if !strings.Contains(d.Description, w) {
				t.Errorf("%s: the description lacks %q", id, w)
			}
		}
	}
}
