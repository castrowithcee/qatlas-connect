package gen

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awsquery"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/restxml"
)

func loadXML(t *testing.T, table Table) *Model {
	t.Helper()
	p, err := LoadPin(ModelDir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := LoadTableModel(ModelDir, p, table)
	if err != nil {
		t.Fatal(err)
	}
	if m.Protocol != ProtocolRestXML {
		t.Fatalf("%s speaks %s", table.Service, m.Protocol)
	}
	return m
}

func bindXML(t *testing.T, table Table, op string) *RestBinding {
	t.Helper()
	b, err := loadXML(t, table).BindRestCurated(&table, op)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// s3Unsupported is what the S3 model still asks for that the runtime does not carry: the event stream of
// SelectObjectContent and the host prefix of WriteGetObjectResponse. Nothing else is left over.
var s3Unsupported = map[string][]string{
	"SelectObjectContent": {"member Details uses smithy.api#eventPayload", "member Payload uses smithy.api#eventPayload",
		"member Payload uses smithy.api#streaming"},
	"WriteGetObjectResponse": {"member RequestRoute uses smithy.api#hostLabel", "the operation uses smithy.api#endpoint"},
}

// Every operation of both real models binds to the runtime, every table lists exactly these operations as tools,
// in exclude or in defer, and what the runtime does not carry yet is named, never sent wrongly.
func TestEveryOperationOfTheXMLModelsBinds(t *testing.T) {
	for _, c := range []struct {
		table       Table
		count       int
		noWrap      bool
		unsupported map[string][]string
	}{{s3Table, 112, true, s3Unsupported}, {route53Table, 71, false, nil}} {
		m := loadXML(t, c.table)
		if m.NoErrorWrapping != c.noWrap {
			t.Errorf("%s: noErrorWrapping = %t", c.table.Service, m.NoErrorWrapping)
		}
		ops, err := m.Operations()
		if err != nil {
			t.Fatal(err)
		}
		if listed := len(c.table.Defer) + len(c.table.Operations) + len(c.table.Exclude); len(ops) != c.count || listed != c.count {
			t.Errorf("%s: %d operations in the model, %d deferred, %d tools, %d excluded", c.table.Service, len(ops),
				len(c.table.Defer), len(c.table.Operations), len(c.table.Exclude))
		}
		seen := map[string][]string{}
		for _, op := range ops {
			b, err := m.BindRestCurated(&c.table, op)
			if err != nil {
				t.Errorf("%s: %v", c.table.Service, err)
				continue
			}
			if err := b.Operation.Validate(); err != nil {
				t.Errorf("%s %s: %v", c.table.Service, op, err)
			}
			if b.Code < 200 || b.Code > 299 {
				t.Errorf("%s %s: success code %d", c.table.Service, op, b.Code)
			}
			if len(b.Unsupported) > 0 {
				seen[op] = slices.Sorted(slices.Values(b.Unsupported))
			}
		}
		if len(seen) != len(c.unsupported) {
			t.Errorf("%s: unsupported = %q, want %q", c.table.Service, seen, c.unsupported)
		}
		for op, want := range c.unsupported {
			if !slices.Equal(seen[op], want) {
				t.Errorf("%s %s: unsupported = %q, want %q", c.table.Service, op, seen[op], want)
			}
		}
	}
}

// A table with an operation the model lacks, or an id prefix on a shape it lacks, is refused.
func TestXMLTablesAreChecked(t *testing.T) {
	m := loadXML(t, route53Table)
	bad := route53Table
	bad.Pagers = append(slices.Clone(bad.Pagers), PagerSpec{Operation: "NoSuchOperation", Positions: []PositionSpec{{In: "A", Out: "B"}}})
	if err := Complete(bad, m); err == nil || !strings.Contains(err.Error(), "NoSuchOperation") {
		t.Errorf("a pager of an unknown operation: %v", err)
	}
	bad = route53Table
	bad.IDPrefixes = []IDPrefix{{Shape: "NoSuchShape", Prefix: "/x/"}}
	if err := Complete(bad, m); err == nil {
		t.Error("an id prefix of an unknown shape was accepted")
	}
	both := route53Table
	both.Pagers = []PagerSpec{{Operation: "ListHostedZones", Positions: []PositionSpec{{In: "Marker", Out: "NextMarker"}}}}
	if _, err := m.BindRestCurated(&both, "ListHostedZones"); err == nil || !strings.Contains(err.Error(), "both page") {
		t.Errorf("an operation paged by the model and the table: %v", err)
	}
}

func encodeXML(t *testing.T, b *RestBinding, args map[string]any) httpbind.Request {
	t.Helper()
	req, err := b.Operation.Encode(restxml.Codec{}, args, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// The XML shapes of the model are bound as the specification says: root names, namespaces, attributes,
// flattened lists and the unwrapped answer.
func TestXMLBindingTraits(t *testing.T) {
	versioning := bindXML(t, s3Table, "PutBucketVersioning")
	payload := fieldByKey(versioning.Operation.Input, "VersioningConfiguration")
	if payload == nil || payload.Loc != httpbind.LocPayload || payload.Shape.XMLName != "VersioningConfiguration" ||
		payload.Shape.Namespace.URI != "http://s3.amazonaws.com/doc/2006-03-01/" {
		t.Fatalf("payload = %+v", payload)
	}
	req := encodeXML(t, versioning, map[string]any{"Bucket": "my-bucket", "VersioningConfiguration": map[string]any{"Status": "Enabled", "MFADelete": "Disabled"}})
	if req.Method != "PUT" || req.Path != "/my-bucket" || req.RawQuery != "versioning" || req.Header.Get("Content-Type") != "application/xml" ||
		!strings.HasPrefix(string(req.Body), `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`) ||
		!strings.Contains(string(req.Body), "<Status>Enabled</Status>") || !strings.Contains(string(req.Body), "<MfaDelete>Disabled</MfaDelete>") {
		t.Errorf("PutBucketVersioning = %+v, body %s", req, req.Body)
	}

	// The grantee carries an attribute named with a prefix, and the namespace of that prefix.
	acl := bindXML(t, s3Table, "PutBucketAcl")
	req = encodeXML(t, acl, map[string]any{"Bucket": "my-bucket", "AccessControlPolicy": map[string]any{
		"Grants": []any{map[string]any{"Permission": "READ", "Grantee": map[string]any{"Type": "CanonicalUser", "ID": "abc"}}}}})
	want := `<Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="CanonicalUser"><ID>abc</ID></Grantee>`
	if !strings.Contains(string(req.Body), want) || !strings.Contains(string(req.Body), "<AccessControlList><Grant>") {
		t.Errorf("PutBucketAcl body = %s", req.Body)
	}

	// The location answer is the member itself, not a document with a member.
	loc := bindXML(t, s3Table, "GetBucketLocation")
	out, _, err := loc.Operation.Decode(restxml.Codec{}, 200, http.Header{},
		[]byte(`<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">eu-central-1</LocationConstraint>`))
	if err != nil || out["LocationConstraint"] != "eu-central-1" {
		t.Errorf("GetBucketLocation answer = %v, %v", out, err)
	}

	// A list of the model is flattened in the answer of ListObjectsV2.
	list := bindXML(t, s3Table, "ListObjectsV2")
	if f := fieldByKey(list.Operation.Output, "Contents"); f == nil || !f.Flattened || f.Name != "Contents" {
		t.Errorf("Contents = %+v", f)
	}
}

const route53Namespace = "https://route53.amazonaws.com/doc/2013-04-01/"

// The Route 53 change batch: the input structure is the root, with the namespace of the service, and a name
// with characters XML escapes comes out escaped. A character XML 1.0 cannot carry is refused, not sent.
func TestRoute53ChangeBatchEncodesAndEscapes(t *testing.T) {
	change := bindXML(t, route53Table, "ChangeResourceRecordSets")
	batch := func(name string) map[string]any {
		return map[string]any{"HostedZoneId": "/hostedzone/Z0123456789", "ChangeBatch": map[string]any{
			"Comment": "a<b>&\"c'",
			"Changes": []any{map[string]any{"Action": "UPSERT", "ResourceRecordSet": map[string]any{
				"Name": name, "Type": "TXT", "TTL": float64(60),
				"ResourceRecords": []any{map[string]any{"Value": `"v=1 & <x>"`}}}}}}}
	}
	req := encodeXML(t, change, batch("a&b<c>.example.org."))
	if req.Method != "POST" || req.Path != "/2013-04-01/hostedzone/Z0123456789/rrset/" && req.Path != "/2013-04-01/hostedzone/Z0123456789/rrset" {
		t.Errorf("request = %+v", req)
	}
	body := string(req.Body)
	for _, want := range []string{
		`<ChangeResourceRecordSetsRequest xmlns="` + route53Namespace + `">`,
		"<Comment>a&lt;b&gt;&amp;&#34;c&#39;</Comment>",
		"<Name>a&amp;b&lt;c&gt;.example.org.</Name>",
		"<ResourceRecords><ResourceRecord><Value>&#34;v=1 &amp; &lt;x&gt;&#34;</Value></ResourceRecord></ResourceRecords>",
		"<Changes><Change><Action>UPSERT</Action>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "HostedZoneId") {
		t.Errorf("the label is in the body: %s", body)
	}
	for name, value := range map[string]string{"control": "a\x01b", "nul": "a\x00b", "fffe": "a\uFFFEb", "bad utf-8": "a\xffb"} {
		if _, err := change.Operation.Encode(restxml.Codec{}, batch(value), nil); err == nil || !strings.Contains(err.Error(), "Name ") || strings.Contains(err.Error(), "\x01") {
			t.Errorf("%s: err = %v, want a refusal that names the member", name, err)
		}
	}
	// Tab, line feed and carriage return are XML characters and survive as references.
	req = encodeXML(t, change, batch("a\tb\nc\rd."))
	if !strings.Contains(string(req.Body), "<Name>a&#x9;b&#xA;c&#xD;d.</Name>") {
		t.Errorf("white space = %s", req.Body)
	}
}

// Zone IDs are accepted with and without the /hostedzone/ prefix and give the same path; nothing of an
// argument becomes a path separator.
func TestRoute53ZoneIDsAreNormalized(t *testing.T) {
	get := bindXML(t, route53Table, "GetHostedZone")
	for _, id := range []string{"Z0123456789", "/hostedzone/Z0123456789"} {
		req := encodeXML(t, get, map[string]any{"Id": id})
		if req.Path != "/2013-04-01/hostedzone/Z0123456789" || req.RawPath != req.Path || req.Method != "GET" {
			t.Errorf("%q: %+v", id, req)
		}
	}
	for _, id := range []string{"/hostedzone/a/b", "a/b", "/hostedzone/", "", "../Z1", "/hostedzone/../x", "/other/Z1"} {
		if _, err := get.Operation.Encode(restxml.Codec{}, map[string]any{"Id": id}, nil); err == nil {
			t.Errorf("%q was accepted", id)
		}
	}
	// A listing with a record name of the zone keeps the query form and takes no prefix of its own.
	rr := bindXML(t, route53Table, "ListResourceRecordSets")
	req := encodeXML(t, rr, map[string]any{"HostedZoneId": "/hostedzone/Z1"})
	if req.Path != "/2013-04-01/hostedzone/Z1/rrset" {
		t.Errorf("path = %s", req.Path)
	}
}

// The model's lists decode: namespaces are ignored, the flattened list and nested structures come out, a
// timestamp is date-time, and an unknown member is skipped.
func TestS3ListAnswerDecodes(t *testing.T) {
	list := bindXML(t, s3Table, "ListObjectsV2")
	body := `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>bucket</Name><Prefix/><KeyCount>2</KeyCount><MaxKeys>2</MaxKeys>
<IsTruncated>true</IsTruncated><NextContinuationToken>tok&amp;1</NextContinuationToken><Extra>x</Extra>
<Contents><Key>a&amp;b</Key><LastModified>2026-01-02T03:04:05.000Z</LastModified><ETag>&quot;e1&quot;</ETag><Size>10</Size><StorageClass>STANDARD</StorageClass></Contents>
<Contents><Key>c</Key><LastModified>2026-01-02T03:04:06Z</LastModified><Size>0</Size></Contents></ListBucketResult>`
	out, _, err := list.Operation.Decode(restxml.Codec{}, 200, http.Header{}, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := out["Contents"].([]any)
	first, _ := contents[0].(map[string]any)
	if len(contents) != 2 || first["Key"] != "a&b" || first["ETag"] != `"e1"` || first["Size"] != int64(10) ||
		first["LastModified"] != "2026-01-02T03:04:05Z" || out["~NextContinuationToken"] != "tok&1" || out["~IsTruncated"] != true ||
		out["Name"] != "bucket" || out["Prefix"] != "" || out["Extra"] != nil {
		t.Errorf("answer = %v", out)
	}
	// An answer that is not the document the model describes is an error, not a partial result.
	for name, bad := range map[string]string{
		"not xml":          "<html",
		"two roots":        "<ListBucketResult/><ListBucketResult/>",
		"bad number":       "<ListBucketResult><KeyCount>many</KeyCount></ListBucketResult>",
		"bad boolean":      "<ListBucketResult><IsTruncated>maybe</IsTruncated></ListBucketResult>",
		"duplicate member": "<ListBucketResult><Name>a</Name><Name>b</Name></ListBucketResult>",
		"doctype":          `<!DOCTYPE x [<!ENTITY e "boom">]><ListBucketResult><Name>&e;</Name></ListBucketResult>`,
		"other encoding":   `<?xml version="1.0" encoding="ISO-8859-1"?><ListBucketResult/>`,
	} {
		if _, _, err := list.Operation.Decode(restxml.Codec{}, 200, http.Header{}, []byte(bad)); err == nil {
			t.Errorf("%s: the answer was accepted", name)
		}
	}
}

func decodeAnswer(t *testing.T, b *RestBinding, body string) map[string]any {
	t.Helper()
	out, _, err := b.Operation.Decode(restxml.Codec{}, 200, http.Header{}, []byte(body))
	if err != nil {
		t.Fatalf("answer: %v\n%s", err, body)
	}
	return out
}

// ListObjectsV2 over three pages: ContinuationToken in the query, NextContinuationToken out, has_more by
// IsTruncated, and a cursor that was altered or belongs to other arguments is refused.
func TestContinuationTokenPagesOverThreePages(t *testing.T) {
	b := bindXML(t, s3Table, "ListObjectsV2")
	if b.Pager == nil || b.Multi != nil || b.Pager.TokenIn != "continuation-token" || b.Pager.TokenOut != "~NextContinuationToken" ||
		b.Pager.Truncated != "~IsTruncated" || b.Pager.SizeParam != "" || b.UnboundedPageSize != "MaxKeys" {
		t.Fatalf("pager = %+v, unbounded %q", b.Pager, b.UnboundedPageSize)
	}
	binding := awsquery.Binding("aws.s3.object.list", map[string]any{"Bucket": "my-bucket"})
	answers := []string{
		`<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>t1</NextContinuationToken></ListBucketResult>`,
		`<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>t2</NextContinuationToken></ListBucketResult>`,
		`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`,
	}
	cursor := ""
	var queries []string
	for i, answer := range answers {
		page, err := b.Pager.Start(binding, 0, cursor)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		args, err := httpbind.ApplyPage(b.Operation.Input, map[string]any{"Bucket": "my-bucket"}, page)
		if err != nil {
			t.Fatal(err)
		}
		queries = append(queries, encodeXML(t, b, args).RawQuery)
		next, failure := page.Next("op", decodeAnswer(t, b, answer))
		if failure != nil {
			t.Fatalf("page %d: %v", i, failure)
		}
		if (next != "") != (i < 2) {
			t.Errorf("page %d: next = %q", i, next)
		}
		if i == 0 {
			if _, err := b.Pager.Start(binding, 0, next+"x"); err == nil {
				t.Error("an altered cursor was accepted")
			}
			if _, err := b.Pager.Start(awsquery.Binding("aws.s3.object.list", map[string]any{"Bucket": "other"}), 0, next); err == nil {
				t.Error("a cursor of other arguments was accepted")
			}
		}
		cursor = next
	}
	if queries[0] != "list-type=2" || queries[1] != "continuation-token=t1&list-type=2" && queries[1] != "list-type=2&continuation-token=t1" ||
		!strings.Contains(queries[2], "continuation-token=t2") {
		t.Errorf("queries = %q", queries)
	}
	// IsTruncated without a token cannot end the list and cannot continue it.
	page, _ := b.Pager.Start(binding, 0, "")
	if _, failure := page.Next("op", decodeAnswer(t, b, `<ListBucketResult><IsTruncated>true</IsTruncated></ListBucketResult>`)); failure == nil {
		t.Error("a truncated answer without a token was accepted")
	}
}

// ListObjectVersions: the position is KeyMarker with VersionIdMarker, in one cursor; the version marker is
// optional, the list ends with IsTruncated, and a position that does not move is refused.
func TestTwoMarkersPageVersions(t *testing.T) {
	b := bindXML(t, s3Table, "ListObjectVersions")
	if b.Multi == nil || b.Pager != nil || len(b.Multi.Positions) != 2 || b.Multi.Truncated != "~IsTruncated" {
		t.Fatalf("multi = %+v", b.Multi)
	}
	binding := awsquery.Binding("aws.s3.version.list", map[string]any{"Bucket": "my-bucket"})
	page, err := b.Multi.Start(binding, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	next, failure := page.Next("op", decodeAnswer(t, b, `<ListVersionsResult><IsTruncated>true</IsTruncated>
<NextKeyMarker>dir/a b</NextKeyMarker><NextVersionIdMarker>v2</NextVersionIdMarker>
<Version><Key>dir/a b</Key><VersionId>v3</VersionId><IsLatest>true</IsLatest></Version></ListVersionsResult>`))
	if failure != nil || next == "" {
		t.Fatalf("next = %q, %v", next, failure)
	}
	second, err := b.Multi.Start(binding, 0, next)
	if err != nil {
		t.Fatal(err)
	}
	args, err := httpbind.ApplyPage(b.Operation.Input, map[string]any{"Bucket": "my-bucket"}, second)
	if err != nil {
		t.Fatal(err)
	}
	if q := encodeXML(t, b, args).RawQuery; q != "key-marker=dir%2Fa%20b&versions=&version-id-marker=v2" &&
		q != "versions&key-marker=dir%2Fa%20b&version-id-marker=v2" {
		t.Errorf("query = %s", q)
	}
	if _, err := b.Multi.Start(binding, 0, next+"A"); err == nil {
		t.Error("an altered cursor was accepted")
	}
	if _, err := b.Multi.Start(awsquery.Binding("aws.s3.version.list", map[string]any{"Bucket": "x"}), 0, next); err == nil {
		t.Error("a cursor of other arguments was accepted")
	}
	// Only the key marker: the version marker is left out of the next request.
	only, failure := page.Next("op", decodeAnswer(t, b, `<ListVersionsResult><IsTruncated>true</IsTruncated><NextKeyMarker>k</NextKeyMarker></ListVersionsResult>`))
	if failure != nil || only == "" {
		t.Fatalf("only a key marker: %q, %v", only, failure)
	}
	end, failure := page.Next("op", decodeAnswer(t, b, `<ListVersionsResult><IsTruncated>false</IsTruncated><NextKeyMarker>ignored</NextKeyMarker></ListVersionsResult>`))
	if failure != nil || end != "" {
		t.Errorf("the end of the list: %q, %v", end, failure)
	}
	for name, body := range map[string]string{
		"no marker":     `<ListVersionsResult><IsTruncated>true</IsTruncated></ListVersionsResult>`,
		"only a second": `<ListVersionsResult><IsTruncated>true</IsTruncated><NextVersionIdMarker>v</NextVersionIdMarker></ListVersionsResult>`,
	} {
		if _, failure := page.Next("op", decodeAnswer(t, b, body)); failure == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The same position again would loop.
	if _, failure := second.Next("op", decodeAnswer(t, b, `<ListVersionsResult><IsTruncated>true</IsTruncated><NextKeyMarker>dir/a b</NextKeyMarker><NextVersionIdMarker>v2</NextVersionIdMarker></ListVersionsResult>`)); failure == nil {
		t.Error("a repeated position was accepted")
	}
}

// ListResourceRecordSets: records of one name come in several types, and a page can end between them. The
// type travels with the name, and the identifier when the set has one.
func TestRecordSetPagesKeepNameAndType(t *testing.T) {
	b := bindXML(t, route53Table, "ListResourceRecordSets")
	if b.Multi == nil || len(b.Multi.Positions) != 3 || b.Multi.SizeParam != "" || b.UnboundedPageSize != "MaxItems" {
		t.Fatalf("multi = %+v, unbounded %q", b.Multi, b.UnboundedPageSize)
	}
	binding := awsquery.Binding("aws.route53.record-set.list", map[string]any{"HostedZoneId": "Z1"})
	page, _ := b.Multi.Start(binding, 0, "")
	answer := func(name, typ, id string) string {
		s := `<ListResourceRecordSetsResponse xmlns="` + route53Namespace + `"><ResourceRecordSets/><IsTruncated>true</IsTruncated><MaxItems>2</MaxItems>` +
			`<NextRecordName>` + name + `</NextRecordName><NextRecordType>` + typ + `</NextRecordType>`
		if id != "" {
			s += `<NextRecordIdentifier>` + id + `</NextRecordIdentifier>`
		}
		return s + `</ListResourceRecordSetsResponse>`
	}
	// The same name continues with another type: that is a new position, not a repeat.
	first, failure := page.Next("op", decodeAnswer(t, b, answer("www.example.org.", "A", "")))
	if failure != nil {
		t.Fatal(failure)
	}
	second, _ := b.Multi.Start(binding, 0, first)
	third, failure := second.Next("op", decodeAnswer(t, b, answer("www.example.org.", "AAAA", "")))
	if failure != nil || third == "" || third == first {
		t.Fatalf("type change: %q, %v", third, failure)
	}
	// A weighted set adds its identifier.
	weighted, failure := second.Next("op", decodeAnswer(t, b, answer("w.example.org.", "A", "set-1")))
	if failure != nil {
		t.Fatal(failure)
	}
	page3, _ := b.Multi.Start(binding, 0, weighted)
	args, err := httpbind.ApplyPage(b.Operation.Input, map[string]any{"HostedZoneId": "Z1"}, page3)
	if err != nil {
		t.Fatal(err)
	}
	req := encodeXML(t, b, args)
	if req.RawQuery != "identifier=set-1&name=w.example.org.&type=A" {
		t.Errorf("query = %s", req.RawQuery)
	}
	// The same name and type again would loop; a name without a type is invalid.
	if _, failure := second.Next("op", decodeAnswer(t, b, answer("www.example.org.", "A", ""))); failure == nil {
		t.Error("a repeated position was accepted")
	}
	noType := `<ListResourceRecordSetsResponse><IsTruncated>true</IsTruncated><NextRecordName>a.</NextRecordName></ListResourceRecordSetsResponse>`
	if _, failure := page.Next("op", decodeAnswer(t, b, noType)); failure == nil {
		t.Error("a name without a type was accepted")
	}
	if _, err := b.Multi.Start(binding, 0, first+"z"); err == nil {
		t.Error("an altered cursor was accepted")
	}
	if _, err := b.Multi.Start(binding, 5, ""); err == nil {
		t.Error("a page size the operation does not offer was accepted")
	}
}

// ListHostedZones and ListHealthChecks page by Marker and NextMarker, and IsTruncated decides the end.
func TestMarkerPagesRoute53Lists(t *testing.T) {
	for _, op := range []string{"ListHostedZones", "ListHealthChecks"} {
		b := bindXML(t, route53Table, op)
		if b.Pager == nil || b.Pager.TokenIn != "marker" || b.Pager.TokenOut != "~NextMarker" || b.Pager.Truncated != "~IsTruncated" {
			t.Fatalf("%s pager = %+v", op, b.Pager)
		}
		binding := awsquery.Binding("aws.route53.list", map[string]any{})
		page, _ := b.Pager.Start(binding, 0, "")
		next, failure := page.Next(op, decodeAnswer(t, b, `<R><IsTruncated>true</IsTruncated><NextMarker>m1</NextMarker><Marker/></R>`))
		if failure != nil || next == "" {
			t.Fatalf("%s: next = %q, %v", op, next, failure)
		}
		again, _ := b.Pager.Start(binding, 0, next)
		args, err := httpbind.ApplyPage(b.Operation.Input, map[string]any{}, again)
		if err != nil || encodeXML(t, b, args).RawQuery != "marker=m1" {
			t.Errorf("%s: %v", op, err)
		}
		end, failure := again.Next(op, decodeAnswer(t, b, `<R><IsTruncated>false</IsTruncated></R>`))
		if failure != nil || end != "" {
			t.Errorf("%s end = %q, %v", op, end, failure)
		}
		if _, failure := again.Next(op, decodeAnswer(t, b, `<R><IsTruncated>true</IsTruncated></R>`)); failure == nil {
			t.Errorf("%s: a truncated answer without NextMarker was accepted", op)
		}
		if _, err := b.Pager.Start(binding, 0, next+"="); err == nil {
			t.Errorf("%s: an altered cursor was accepted", op)
		}
	}
}

// The error answers of both models give the code and type the runtime classifies, and nothing of the message.
func TestModelErrorShapesAreRead(t *testing.T) {
	s3 := restxml.Codec{NoErrorWrapping: true}
	if got := s3.ErrorCode(nil, []byte(`<Error><Code>NoSuchBucket</Code><Message>x</Message><RequestId>r</RequestId></Error>`)); got != "NoSuchBucket" {
		t.Errorf("S3 code = %q", got)
	}
	r53 := restxml.Codec{}
	body := []byte(`<ErrorResponse xmlns="` + route53Namespace + `"><Error><Type>Sender</Type><Code>NoSuchHostedZone</Code><Message>m</Message></Error><RequestId>r</RequestId></ErrorResponse>`)
	if r53.ErrorCode(nil, body) != "NoSuchHostedZone" || r53.ErrorType(nil, body) != "Sender" {
		t.Errorf("Route 53 = %q %q", r53.ErrorCode(nil, body), r53.ErrorType(nil, body))
	}
}

// bodyOf returns the document shape of the body members of an input, as the runtime builds it.
func bodyOf(in *httpbind.Shape) *httpbind.Shape {
	b := &httpbind.Shape{Kind: httpbind.KindStruct, XMLName: in.XMLName, Namespace: in.Namespace}
	for _, f := range in.Fields {
		if f.Loc == httpbind.LocBody {
			b.Fields = append(b.Fields, f)
		}
	}
	return b
}

// Documents of both models go out and come back as the same value: namespaces, attributes, wrapped and
// flattened lists, and an entry whose name holds markup, a backslash escape and non-ASCII text.
func TestXMLRoundTripWithExamplesOfBothModels(t *testing.T) {
	codec := restxml.Codec{}
	change := bindXML(t, route53Table, "ChangeResourceRecordSets").Operation.Input
	value := map[string]any{"ChangeBatch": map[string]any{
		"Comment": "move <www> & co",
		"Changes": []any{
			map[string]any{"Action": "UPSERT", "ResourceRecordSet": map[string]any{
				"Name": `\052.a&b.exämple.org.`, "Type": "TXT", "TTL": int64(300), "SetIdentifier": "set \"one\"", "Weight": int64(10),
				"ResourceRecords": []any{map[string]any{"Value": `"x <y> & 'z'"`}, map[string]any{"Value": `"2"`}}}},
			map[string]any{"Action": "DELETE", "ResourceRecordSet": map[string]any{
				"Name": "alias.example.org.", "Type": "A",
				"AliasTarget": map[string]any{"HostedZoneId": "Z2", "DNSName": "lb.example.net.", "EvaluateTargetHealth": true}}}}}}
	data, err := codec.Marshal(bodyOf(change), value)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := codec.Unmarshal(bodyOf(change), data)
	if err != nil || !reflect.DeepEqual(got, value) {
		t.Errorf("Route 53 round trip: %v\n%s\n%#v\nwant\n%#v", err, data, got, value)
	}

	acl := bindXML(t, s3Table, "PutBucketAcl").Operation.Input
	payload := fieldByKey(acl, "AccessControlPolicy").Shape
	policy := map[string]any{
		"Owner": map[string]any{"ID": "owner", "DisplayName": "O & <W>"},
		"Grants": []any{
			map[string]any{"Permission": "FULL_CONTROL", "Grantee": map[string]any{"Type": "CanonicalUser", "ID": "id-1", "DisplayName": "ä<b>"}},
			map[string]any{"Permission": "READ", "Grantee": map[string]any{"Type": "Group", "URI": "http://acs.amazonaws.com/groups/global/AllUsers"}}}}
	data, err = codec.Marshal(payload, policy)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err = codec.Unmarshal(payload, data)
	if err != nil || !reflect.DeepEqual(got, policy) {
		t.Errorf("S3 round trip: %v\n%s\n%#v\nwant\n%#v", err, data, got, policy)
	}

	del := fieldByKey(bindXML(t, s3Table, "DeleteObjects").Operation.Input, "Delete").Shape
	batch := map[string]any{"Quiet": true, "Objects": []any{map[string]any{"Key": "a & b/<c>\t.txt", "VersionId": "v1"}, map[string]any{"Key": "ü"}}}
	if data, err = codec.Marshal(del, batch); err != nil {
		t.Fatal(err)
	}
	if got, _, err = codec.Unmarshal(del, data); err != nil || !reflect.DeepEqual(got, batch) {
		t.Errorf("DeleteObjects round trip: %v\n%s\n%#v", err, data, got)
	}
	// A key with a control character cannot be sent in a delete batch: the request is refused, not corrupted.
	batch["Objects"] = []any{map[string]any{"Key": "a\x01b"}}
	if _, err := codec.Marshal(del, batch); err == nil {
		t.Error("a key with a control character was written")
	}
}

// A restXml model is read, and a model with two protocols is not.
func TestRestXMLProtocolIsRead(t *testing.T) {
	ok := `{"smithy":"2.0","shapes":{"com.example#S":{"type":"service","version":"2020-01-01","traits":{"aws.protocols#restXml":{"noErrorWrapping":true}}}}}`
	m, err := LoadModel([]byte(ok))
	if err != nil || m.Protocol != ProtocolRestXML || !m.NoErrorWrapping {
		t.Errorf("model = %+v, %v", m, err)
	}
	both := `{"smithy":"2.0","shapes":{"com.example#S":{"type":"service","version":"2020-01-01","traits":{"aws.protocols#restXml":{},"aws.protocols#restJson1":{}}}}}`
	if _, err := LoadModel([]byte(both)); err == nil {
		t.Error("a model with two protocols was accepted")
	}
	// restJson1 still refuses an operation with a trait the runtime does not carry; restXml lists it.
	if _, err := syntheticRest(t).BindRest("Streaming"); err == nil {
		t.Error("restJson1 accepted a streaming operation")
	}
}

func keys(s *httpbind.Shape) []string {
	var out []string
	for _, f := range s.Fields {
		out = append(out, f.Key)
	}
	return out
}

// What the layer owns is marked in the description and never an argument: the bucket label, the owner
// headers, the stream payload and the media type are roles; the length, the checksums and the algorithm
// choice are gone from the input and come back as settings of the operation.
func TestS3LayerOwnedMembers(t *testing.T) {
	put := bindXML(t, s3Table, "PutObject")
	in := put.Operation.Input
	roles := map[string]string{}
	for _, f := range in.Fields {
		switch {
		case f.Bucket:
			roles["bucket"] = f.Key
		case f.Owner:
			roles["owner"] = f.Key
		case f.Stream:
			roles["stream"] = f.Key
		case f.ContentType:
			roles["type"] = f.Key
		}
	}
	if roles["bucket"] != "Bucket" || roles["owner"] != "ExpectedBucketOwner" || roles["stream"] != "Body" || roles["type"] != "ContentType" {
		t.Errorf("roles = %v", roles)
	}
	for _, gone := range []string{"ContentLength", "ContentMD5", "ChecksumAlgorithm", "ChecksumCRC32", "ChecksumSHA256", "ChecksumCRC64NVME"} {
		if slices.Contains(keys(in), gone) {
			t.Errorf("PutObject still offers %s", gone)
		}
	}
	if len(put.Unsupported) != 0 || put.Operation.ChecksumRequired || put.Operation.ValidateResponseChecksum {
		t.Errorf("PutObject = %+v", put)
	}
	if !put.Operation.StreamsInput() || put.Operation.StreamsOutput() {
		t.Error("PutObject streams the wrong way")
	}
	get := bindXML(t, s3Table, "GetObject")
	if !get.Operation.StreamsOutput() || !get.Operation.ValidateResponseChecksum || slices.Contains(keys(get.Operation.Input), "ChecksumMode") {
		t.Errorf("GetObject = %+v", get)
	}
	if !bindXML(t, s3Table, "DeleteObjects").Operation.ChecksumRequired || !bindXML(t, s3Table, "PutBucketPolicy").Operation.ChecksumRequired {
		t.Error("an operation that requires a checksum does not say so")
	}
	// The owner header is on every operation that has it, and the copy names the source owner too.
	copyOp := bindXML(t, s3Table, "CopyObject").Operation.Input
	owners := 0
	for _, f := range copyOp.Fields {
		if f.Owner {
			owners++
		}
	}
	if owners != 2 {
		t.Errorf("CopyObject has %d owner members", owners)
	}
	for _, op := range []string{"ListBuckets", "CreateBucket"} {
		for _, f := range bindXML(t, s3Table, op).Operation.Input.Fields {
			if f.Owner {
				t.Errorf("%s has an owner member", op)
			}
		}
	}
	// Every operation with an expected-owner member has it as a layer-owned member, never as an argument.
	m := loadXML(t, s3Table)
	ops, _ := m.Operations()
	for _, op := range ops {
		b, err := m.BindRestCurated(&s3Table, op)
		if err != nil || b.Operation.Input == nil {
			continue
		}
		for _, f := range b.Operation.Input.Fields {
			if f.Loc == httpbind.LocHeader && strings.Contains(strings.ToLower(f.Name), "expected-bucket-owner") && !f.Owner {
				t.Errorf("%s: %s is an argument", op, f.Key)
			}
			if f.Loc == httpbind.LocLabel && f.Name == "Bucket" && !f.Bucket {
				t.Errorf("%s: the Bucket label is not checked", op)
			}
		}
	}
}

// A restXml tool is assembled from the binding: a header of the answer is a header member, the expected-owner
// header is the layer's and never an argument, the bucket label is checked, and the KMS key ID is released by the
// table.
func TestRestXMLToolsAreAssembled(t *testing.T) {
	m := loadXML(t, s3Table)
	built := map[string]*tool{}
	for _, op := range s3Table.Operations {
		tl, err := build(m, &s3Table, op)
		if err != nil {
			t.Fatal(err)
		}
		built[op.Operation] = tl
	}
	head := built["HeadBucket"].restOp
	region := fieldByKey(head.Output, "bucket_region")
	owner := fieldByKey(head.Input, "ExpectedBucketOwner")
	bucket := fieldByKey(head.Input, "bucket")
	if region == nil || region.Loc != httpbind.LocHeader || region.Name != "x-amz-bucket-region" ||
		owner == nil || !owner.Owner || owner.Loc != httpbind.LocHeader || bucket == nil || !bucket.Bucket || !bucket.Required {
		t.Errorf("HeadBucket = %+v", head)
	}
	for _, name := range []string{"expected_bucket_owner", "ExpectedBucketOwner"} {
		if strings.Contains(built["HeadBucket"].inputSchema, `"`+name+`"`) {
			t.Errorf("the owner is an argument: %s", built["HeadBucket"].inputSchema)
		}
	}
	if list := built["ListBuckets"]; list.op.TargetCheck != "none" || ownerHeaderOf(list.restOp) != "" || list.pager == nil || list.pager.MaxSize != 10000 {
		t.Errorf("ListBuckets = %+v", list.pager)
	}
	if loc := fieldByKey(built["GetBucketLocation"].restOp.Output, "location_constraint"); loc == nil || !loc.Unwrapped {
		t.Errorf("GetBucketLocation output = %+v", loc)
	}
	enc := built["GetBucketEncryption"]
	if !strings.Contains(enc.outputSchema, "kms_master_key_id") || !strings.Contains(enc.outputSchema, "sse_algorithm") {
		t.Errorf("encryption schema = %s", enc.outputSchema)
	}
	// bucket-owner on a member that is not the bucket label is refused, and so is a restJson1 tool with it.
	var list Operation
	for _, op := range s3Table.Operations {
		if op.Operation == "ListBuckets" {
			list = op
		}
	}
	list.TargetCheck, list.TargetArgs = "bucket-owner", []string{"Prefix"}
	list.Arguments = map[string]Argument{"Prefix": {Description: "d"}, "BucketRegion": {Description: "d"}}
	if _, err := build(m, &s3Table, list); err == nil || !strings.Contains(err.Error(), "bucket label") {
		t.Errorf("bucket-owner on a prefix: %v", err)
	}
	rj := loadRest(t, sesv2Table)
	sesOp := sesv2Table.Operations[0]
	sesOp.TargetCheck, sesOp.TargetArgs = "bucket-owner", []string{"Filter"}
	if _, err := build(rj, &sesv2Table, sesOp); err == nil || !strings.Contains(err.Error(), "bucket-owner needs the S3 protocol package") {
		t.Errorf("bucket-owner on restJson1: %v", err)
	}
}
