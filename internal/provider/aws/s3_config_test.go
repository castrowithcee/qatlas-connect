package aws

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const s3NS = `xmlns="http://s3.amazonaws.com/doc/2006-03-01/"`

// s3ConfigReadIDs are the readers of bucket configuration that have no writing tool in this group.
var s3ConfigReadIDs = []string{"aws.s3.lifecycle.get", "aws.s3.cors.get", "aws.s3.acl.get", "aws.s3.logging.get", "aws.s3.website.get", "aws.s3.notification.get",
	"aws.s3.replication.get", "aws.s3.ownership.get", "aws.s3.accelerate.get", "aws.s3.request-payment.get"}

var s3ConfigReadAnswers = map[string]struct {
	query string
	reply reply
}{
	"aws.s3.lifecycle.get": {"lifecycle", reply{status: 200, header: map[string]string{"Content-Type": "application/xml",
		"X-Amz-Transition-Default-Minimum-Object-Size": "varies_by_storage_class"},
		body: `<LifecycleConfiguration ` + s3NS + `><Rule><ID>expire-logs</ID><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status>` +
			`<Expiration><Days>30</Days></Expiration></Rule><Rule><ID>tagged</ID><Filter><And><Prefix>a/</Prefix><Tag><Key>k</Key><Value>v</Value></Tag>` +
			`<ObjectSizeGreaterThan>10</ObjectSizeGreaterThan></And></Filter><Status>Disabled</Status>` +
			`<Transition><Days>10</Days><StorageClass>GLACIER</StorageClass></Transition>` +
			`<NoncurrentVersionExpiration><NoncurrentDays>5</NoncurrentDays><NewerNoncurrentVersions>2</NewerNoncurrentVersions></NoncurrentVersionExpiration>` +
			`<AbortIncompleteMultipartUpload><DaysAfterInitiation>7</DaysAfterInitiation></AbortIncompleteMultipartUpload></Rule></LifecycleConfiguration>`}},
	"aws.s3.cors.get": {"cors", xmlReply(`<CORSConfiguration ` + s3NS + `><CORSRule><ID>r1</ID><AllowedOrigin>*</AllowedOrigin>` +
		`<AllowedMethod>GET</AllowedMethod><AllowedMethod>HEAD</AllowedMethod><AllowedHeader>*</AllowedHeader><ExposeHeader>ETag</ExposeHeader>` +
		`<MaxAgeSeconds>3000</MaxAgeSeconds></CORSRule></CORSConfiguration>`)},
	"aws.s3.acl.get": {"acl", xmlReply(`<AccessControlPolicy ` + s3NS + `><Owner><ID>owner-id</ID><DisplayName>me</DisplayName></Owner><AccessControlList>` +
		`<Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="CanonicalUser"><ID>owner-id</ID><DisplayName>me</DisplayName></Grantee>` +
		`<Permission>FULL_CONTROL</Permission></Grant>` +
		`<Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="Group"><URI>http://acs.amazonaws.com/groups/global/AllUsers</URI></Grantee>` +
		`<Permission>READ</Permission></Grant></AccessControlList></AccessControlPolicy>`)},
	"aws.s3.logging.get": {"logging", xmlReply(`<BucketLoggingStatus ` + s3NS + `><LoggingEnabled><TargetBucket>foreign-log-bucket</TargetBucket>` +
		`<TargetPrefix>logs/</TargetPrefix><TargetGrants><Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="AmazonCustomerByEmail">` +
		`<EmailAddress>a@example.com</EmailAddress></Grantee><Permission>READ</Permission></Grant></TargetGrants></LoggingEnabled></BucketLoggingStatus>`)},
	"aws.s3.website.get": {"website", xmlReply(`<WebsiteConfiguration ` + s3NS + `><IndexDocument><Suffix>index.html</Suffix></IndexDocument>` +
		`<ErrorDocument><Key>error.html</Key></ErrorDocument></WebsiteConfiguration>`)},
	"aws.s3.notification.get": {"notification", xmlReply(`<NotificationConfiguration ` + s3NS + `><TopicConfiguration><Id>t1</Id>` +
		`<Topic>arn:aws:sns:eu-west-1:999999999999:foreign-topic</Topic><Event>s3:ObjectCreated:*</Event></TopicConfiguration>` +
		`<QueueConfiguration><Id>q1</Id><Queue>arn:aws:sqs:eu-west-1:999999999999:q</Queue><Event>s3:ObjectRemoved:*</Event></QueueConfiguration>` +
		`<CloudFunctionConfiguration><Id>l1</Id><CloudFunction>arn:aws:lambda:eu-west-1:999999999999:function:f</CloudFunction><Event>s3:ObjectCreated:Put</Event>` +
		`</CloudFunctionConfiguration><EventBridgeConfiguration></EventBridgeConfiguration></NotificationConfiguration>`)},
	"aws.s3.replication.get": {"replication", xmlReply(`<ReplicationConfiguration ` + s3NS + `><Role>arn:aws:iam::123456789012:role/repl</Role>` +
		`<Rule><ID>all</ID><Priority>1</Priority><Status>Enabled</Status><Filter><Prefix></Prefix></Filter>` +
		`<Destination><Bucket>arn:aws:s3:::foreign-bucket</Bucket><Account>999999999999</Account><StorageClass>STANDARD</StorageClass></Destination>` +
		`<DeleteMarkerReplication><Status>Disabled</Status></DeleteMarkerReplication></Rule></ReplicationConfiguration>`)},
	"aws.s3.ownership.get":       {"ownershipControls", xmlReply(`<OwnershipControls ` + s3NS + `><Rule><ObjectOwnership>BucketOwnerEnforced</ObjectOwnership></Rule></OwnershipControls>`)},
	"aws.s3.accelerate.get":      {"accelerate", xmlReply(`<AccelerateConfiguration ` + s3NS + `><Status>Enabled</Status></AccelerateConfiguration>`)},
	"aws.s3.request-payment.get": {"requestPayment", xmlReply(`<RequestPaymentConfiguration ` + s3NS + `><Payer>Requester</Payer></RequestPaymentConfiguration>`)},
}

func descriptionOf(t *testing.T, id string) string {
	t.Helper()
	for _, d := range registered(t).Provider(Provider) {
		if d.ID == id {
			return d.Description
		}
	}
	t.Fatalf("%s is not registered", id)
	return ""
}

// Every reader asks for its own sub-resource with GET, sends the bound account as expected bucket owner, takes
// no owner argument, and decodes the sample answer. The answers that name targets of other accounts are shown
// and nothing else is requested.
func TestS3ConfigReadersDecodeAndSendOwner(t *testing.T) {
	for _, id := range s3ConfigReadIDs {
		t.Run(id, func(t *testing.T) {
			c := s3ConfigReadAnswers[id]
			f := restAnswer(c.reply)
			e := newToolsEnv(t, f, baseValues(), s3ConfigReadIDs)
			text, err := e.invoke(id, `{"bucket":"my-bucket"}`)
			if err != nil {
				t.Fatal(err)
			}
			got := decodeResult(t, text)
			if got["region"] != testRegion {
				t.Errorf("result = %s", text)
			}
			reqs := restRequests(f)
			if len(reqs) != 1 {
				t.Fatalf("requests = %d, a reader sent more than one request: %v", len(reqs), reqs)
			}
			r := reqs[0]
			if r.method != "GET" || r.rawQuery != c.query || r.host != "my-bucket.s3."+testRegion+".amazonaws.com" ||
				r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
				t.Errorf("request = %+v", r)
			}
			if _, err := e.invoke(id, `{"bucket":"my-bucket","expected_bucket_owner":"999999999999"}`); err == nil {
				t.Error("an owner argument was accepted")
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("a refused call was sent: %d requests", n)
			}
		})
	}
}

func TestS3ConfigReaderResults(t *testing.T) {
	read := func(id string) (map[string]any, string) {
		f := restAnswer(s3ConfigReadAnswers[id].reply)
		text, err := newToolsEnv(t, f, baseValues(), s3ConfigReadIDs).invoke(id, `{"bucket":"my-bucket"}`)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return decodeResult(t, text), text
	}
	got, _ := read("aws.s3.lifecycle.get")
	rules, _ := got["rules"].([]any)
	if len(rules) != 2 || got["transition_default_minimum_object_size"] != "varies_by_storage_class" {
		t.Fatalf("lifecycle = %v", got)
	}
	second, _ := rules[1].(map[string]any)
	and := second["filter"].(map[string]any)["and"].(map[string]any)
	if second["status"] != "Disabled" || and["prefix"] != "a/" || and["object_size_greater_than"] != float64(10) ||
		second["noncurrent_version_expiration"].(map[string]any)["newer_noncurrent_versions"] != float64(2) ||
		second["abort_incomplete_multipart_upload"].(map[string]any)["days_after_initiation"] != float64(7) {
		t.Errorf("rule = %v", second)
	}
	got, _ = read("aws.s3.cors.get")
	rule := got["cors_rules"].([]any)[0].(map[string]any)
	if rule["allowed_origins"].([]any)[0] != "*" || len(rule["allowed_methods"].([]any)) != 2 || rule["max_age_seconds"] != float64(3000) {
		t.Errorf("cors = %v", got)
	}
	got, _ = read("aws.s3.acl.get")
	grants := got["grants"].([]any)
	g0, g1 := grants[0].(map[string]any), grants[1].(map[string]any)
	gr0, gr1 := g0["grantee"].(map[string]any), g1["grantee"].(map[string]any)
	if len(grants) != 2 || got["owner"].(map[string]any)["id"] != "owner-id" || gr0["type"] != "CanonicalUser" || gr0["id"] != "owner-id" ||
		gr0["display_name"] != "me" || g0["permission"] != "FULL_CONTROL" || gr1["type"] != "Group" ||
		gr1["uri"] != "http://acs.amazonaws.com/groups/global/AllUsers" || g1["permission"] != "READ" {
		t.Errorf("acl = %v", got)
	}
	// The target of the log delivery is shown as it is, also when it is not a bucket of this account.
	got, _ = read("aws.s3.logging.get")
	le := got["logging_enabled"].(map[string]any)
	tg := le["target_grants"].([]any)[0].(map[string]any)
	if le["target_bucket"] != "foreign-log-bucket" || le["target_prefix"] != "logs/" ||
		tg["grantee"].(map[string]any)["type"] != "AmazonCustomerByEmail" || tg["grantee"].(map[string]any)["email_address"] != "a@example.com" ||
		tg["permission"] != "READ" {
		t.Errorf("logging = %v", got)
	}
	got, _ = read("aws.s3.website.get")
	if got["index_document"].(map[string]any)["suffix"] != "index.html" {
		t.Errorf("website = %v", got)
	}
	got, _ = read("aws.s3.ownership.get")
	if got["ownership_controls"].(map[string]any)["rules"].([]any)[0].(map[string]any)["object_ownership"] != "BucketOwnerEnforced" {
		t.Errorf("ownership = %v", got)
	}
	if got, _ = read("aws.s3.accelerate.get"); got["status"] != "Enabled" {
		t.Errorf("accelerate = %v", got)
	}
	if got, _ = read("aws.s3.request-payment.get"); got["payer"] != "Requester" {
		t.Errorf("request payment = %v", got)
	}
	// Targets of other accounts are shown as they are.
	got, _ = read("aws.s3.notification.get")
	topic := got["topic_configurations"].([]any)[0].(map[string]any)
	if topic["topic_arn"] != "arn:aws:sns:eu-west-1:999999999999:foreign-topic" ||
		got["queue_configurations"].([]any)[0].(map[string]any)["queue_arn"] != "arn:aws:sqs:eu-west-1:999999999999:q" ||
		got["lambda_function_configurations"].([]any)[0].(map[string]any)["lambda_function_arn"] != "arn:aws:lambda:eu-west-1:999999999999:function:f" {
		t.Errorf("notification = %v", got)
	}
	got, _ = read("aws.s3.replication.get")
	dest := got["replication_configuration"].(map[string]any)["rules"].([]any)[0].(map[string]any)["destination"].(map[string]any)
	if dest["bucket"] != "arn:aws:s3:::foreign-bucket" || dest["account"] != "999999999999" {
		t.Errorf("replication = %v", got)
	}
}

// A configuration that does not exist is not-found for every reader that names a code for it, and is not repeated.
func TestS3ConfigMissingIsNotFound(t *testing.T) {
	for _, c := range []struct{ id, code string }{
		{"aws.s3.lifecycle.get", "NoSuchLifecycleConfiguration"},
		{"aws.s3.cors.get", "NoSuchCORSConfiguration"},
		{"aws.s3.website.get", "NoSuchWebsiteConfiguration"},
		{"aws.s3.replication.get", "ReplicationConfigurationNotFoundError"},
		{"aws.s3.ownership.get", "OwnershipControlsNotFoundError"},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := restAnswer(reply{status: 404, header: map[string]string{"Content-Type": "application/xml"},
				body: `<Error><Code>` + c.code + `</Code><Message>m</Message></Error>`})
			e := newToolsEnv(t, f, baseValues(), s3ConfigReadIDs)
			if _, err := e.invoke(c.id, `{"bucket":"my-bucket"}`); classOf(err) != provider.ClassNotFound {
				t.Errorf("err = %v, class %q", err, classOf(err))
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("requests = %d", n)
			}
		})
	}
	// The deletes of a missing configuration are not-found as well.
	for _, c := range []struct{ id, code string }{
		{"aws.s3.lifecycle.delete", "NoSuchLifecycleConfiguration"}, {"aws.s3.cors.delete", "NoSuchCORSConfiguration"}} {
		f := restAnswer(reply{status: 404, header: map[string]string{"Content-Type": "application/xml"},
			body: `<Error><Code>` + c.code + `</Code><Message>m</Message></Error>`})
		if _, err := s3WriteEnv(t, f, testRegion).invokeConfirmed(c.id, `{"bucket":"my-bucket"}`, true); classOf(err) != provider.ClassNotFound {
			t.Errorf("%s: %v", c.id, err)
		}
	}
}

func checksumOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// The lifecycle put sends the rules as XML with the checksum the service requires and the expected owner; rules
// with expiration, tag and and-filters, a rule without a filter, and the default minimum size are encoded.
func TestS3LifecyclePutSendsRulesWithChecksum(t *testing.T) {
	f := restAnswer(reply{status: 200, header: map[string]string{"X-Amz-Transition-Default-Minimum-Object-Size": "all_storage_classes_128K"}})
	e := s3WriteEnv(t, f, testRegion)
	args := `{"bucket":"my-bucket","transition_default_minimum_object_size":"all_storage_classes_128K","lifecycle_configuration":{"rules":[` +
		`{"id":"logs","status":"Enabled","filter":{"prefix":"logs/"},"expiration":{"days":30}},` +
		`{"id":"tagged","status":"Enabled","filter":{"tag":{"key":"temp","value":"yes"}},"expiration":{"date":"2027-01-01T00:00:00Z"}},` +
		`{"id":"both","status":"Disabled","filter":{"and":{"prefix":"a/","tags":[{"key":"k","value":"v"}],"object_size_greater_than":100}},` +
		`"transitions":[{"days":10,"storage_class":"GLACIER"}],"noncurrent_version_expiration":{"noncurrent_days":5},` +
		`"abort_incomplete_multipart_upload":{"days_after_initiation":7}},` +
		`{"id":"whole","status":"Enabled","expiration":{"days":365}}]}}`
	text, err := e.invokeConfirmed("aws.s3.lifecycle.put", args, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeResult(t, text); got["transition_default_minimum_object_size"] != "all_storage_classes_128K" {
		t.Errorf("result = %s", text)
	}
	reqs := restRequests(f)
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	r := reqs[0]
	for _, want := range []string{
		`<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`,
		`<Rule><Expiration><Days>30</Days></Expiration><ID>logs</ID><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status></Rule>`,
		`<Filter><Tag><Key>temp</Key><Value>yes</Value></Tag></Filter>`, `<Expiration><Date>2027-01-01T00:00:00Z</Date></Expiration><ID>tagged</ID>`,
		`<Filter><And><Prefix>a/</Prefix><Tag><Key>k</Key><Value>v</Value></Tag><ObjectSizeGreaterThan>100</ObjectSizeGreaterThan></And></Filter>`,
		`<Transition><Days>10</Days><StorageClass>GLACIER</StorageClass></Transition>`,
		`<NoncurrentVersionExpiration><NoncurrentDays>5</NoncurrentDays></NoncurrentVersionExpiration>`,
		`<AbortIncompleteMultipartUpload><DaysAfterInitiation>7</DaysAfterInitiation></AbortIncompleteMultipartUpload>`,
		`<Rule><Expiration><Days>365</Days></Expiration><ID>whole</ID><Status>Enabled</Status></Rule>`,
	} {
		if !strings.Contains(r.body, want) {
			t.Errorf("body lacks %s: %s", want, r.body)
		}
	}
	if r.method != "PUT" || r.rawQuery != "lifecycle" || r.host != "my-bucket.s3."+testRegion+".amazonaws.com" ||
		r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount ||
		r.header.Get("X-Amz-Transition-Default-Minimum-Object-Size") != "all_storage_classes_128K" {
		t.Errorf("request = %+v", r)
	}
	if r.header.Get("X-Amz-Checksum-Sha256") != checksumOf(r.body) || r.header.Get("X-Amz-Sdk-Checksum-Algorithm") != "SHA256" {
		t.Errorf("checksum headers = %v", r.header)
	}
	if strings.Contains(r.body, "<Prefix></Prefix>") {
		t.Errorf("a rule without a filter gained a prefix: %s", r.body)
	}
}

// The CORS put sends several rules, also the origin *, with the checksum header and the expected owner.
func TestS3CORSPutSendsRulesWithChecksum(t *testing.T) {
	f := restAnswer(reply{status: 200})
	e := s3WriteEnv(t, f, testRegion)
	args := `{"bucket":"my-bucket","cors_configuration":{"cors_rules":[` +
		`{"id":"any","allowed_origins":["*"],"allowed_methods":["GET","HEAD"],"allowed_headers":["*"],"expose_headers":["ETag"],"max_age_seconds":3000},` +
		`{"allowed_origins":["https://a.example","https://b.example"],"allowed_methods":["PUT"]}]}}`
	if _, err := e.invokeConfirmed("aws.s3.cors.put", args, true); err != nil {
		t.Fatal(err)
	}
	r := restRequests(f)[0]
	for _, want := range []string{`<CORSConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`,
		`<CORSRule><ID>any</ID><AllowedHeader>*</AllowedHeader><AllowedMethod>GET</AllowedMethod><AllowedMethod>HEAD</AllowedMethod><AllowedOrigin>*</AllowedOrigin>`,
		`<ExposeHeader>ETag</ExposeHeader><MaxAgeSeconds>3000</MaxAgeSeconds></CORSRule>`,
		`<AllowedMethod>PUT</AllowedMethod><AllowedOrigin>https://a.example</AllowedOrigin><AllowedOrigin>https://b.example</AllowedOrigin>`} {
		if !strings.Contains(r.body, want) {
			t.Errorf("body lacks %s: %s", want, r.body)
		}
	}
	if r.method != "PUT" || r.rawQuery != "cors" || r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount ||
		r.header.Get("X-Amz-Checksum-Sha256") != checksumOf(r.body) || r.header.Get("X-Amz-Sdk-Checksum-Algorithm") != "SHA256" {
		t.Errorf("request = %+v", r)
	}
}

// A rejected document is an error, not a result, and is not repeated; invalid arguments send nothing.
func TestS3ConfigPutsRefuseBadInput(t *testing.T) {
	f := restAnswer(reply{status: 400, header: map[string]string{"Content-Type": "application/xml"},
		body: `<Error><Code>MalformedXML</Code><Message>m</Message></Error>`})
	e := s3WriteEnv(t, f, testRegion)
	for id, args := range map[string]string{
		"aws.s3.lifecycle.put": `{"bucket":"my-bucket","lifecycle_configuration":{"rules":[{"id":"a","status":"Enabled"}]}}`,
		"aws.s3.cors.put":      `{"bucket":"my-bucket","cors_configuration":{"cors_rules":[{"allowed_origins":["*"],"allowed_methods":["GET"]}]}}`,
	} {
		_, err := e.invokeConfirmed(id, args, true)
		var failed *provider.Error
		if !errorsAs(err, &failed) || failed.Class != provider.ClassProviderError || failed.Original == nil || failed.Original.Entries[0].Code != "MalformedXML" {
			t.Errorf("%s: %v", id, err)
		}
	}
	if n := len(restRequests(f)); n != 2 {
		t.Errorf("requests = %d, a failed write was repeated", n)
	}
	before := len(f.requests)
	for id, args := range map[string]string{
		"aws.s3.lifecycle.put": `{"bucket":"my-bucket","lifecycle_configuration":{"rules":[{"id":"a","status":"Maybe"}]}}`,
		"aws.s3.cors.put":      `{"bucket":"my-bucket","cors_configuration":{"cors_rules":[{"allowed_methods":["GET"]}]}}`,
	} {
		if _, err := e.invokeConfirmed(id, args, true); err == nil {
			t.Errorf("%s accepted %s", id, args)
		}
	}
	for _, id := range []string{"aws.s3.lifecycle.delete", "aws.s3.cors.delete"} {
		if _, err := e.invokeConfirmed(id, `{"bucket":"my-bucket","expected_bucket_owner":"999999999999"}`, true); err == nil {
			t.Errorf("%s accepted an owner argument", id)
		}
	}
	if len(f.requests) != before {
		t.Errorf("a refused call reached the network: %v", f.actions()[before:])
	}
}

// The deletes send DELETE with the expected owner; all four writes need the confirmation before any request.
func TestS3ConfigWritesNeedConfirmation(t *testing.T) {
	cases := []struct{ id, args, query string }{
		{"aws.s3.lifecycle.delete", `{"bucket":"my-bucket"}`, "lifecycle"},
		{"aws.s3.cors.delete", `{"bucket":"my-bucket"}`, "cors"},
		{"aws.s3.lifecycle.put", `{"bucket":"my-bucket","lifecycle_configuration":{"rules":[{"id":"a","status":"Enabled","filter":{"prefix":"x/"},"expiration":{"days":1}}]}}`, "lifecycle"},
		{"aws.s3.cors.put", `{"bucket":"my-bucket","cors_configuration":{"cors_rules":[{"allowed_origins":["*"],"allowed_methods":["GET"]}]}}`, "cors"},
	}
	for _, c := range cases {
		f := restAnswer(reply{status: 200})
		e := s3WriteEnv(t, f, testRegion)
		if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
			t.Errorf("%s without confirmation: %v", c.id, err)
		}
		if len(f.requests) != 0 {
			t.Errorf("%s: an unconfirmed call reached the network: %v", c.id, f.actions())
		}
		if _, err := e.invokeConfirmed(c.id, c.args, true); err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		r := restRequests(f)[0]
		want := "PUT"
		if strings.HasSuffix(c.id, ".delete") {
			want = "DELETE"
		}
		if r.method != want || r.rawQuery != c.query || r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
			t.Errorf("%s request = %+v", c.id, r)
		}
	}
	for _, d := range registered(t).Provider(Provider) {
		switch d.ID {
		case "aws.s3.lifecycle.put", "aws.s3.cors.put":
			if d.Risk.Idempotency != "idempotent" {
				t.Errorf("%s idempotency = %s", d.ID, d.Risk.Idempotency)
			}
		case "aws.s3.lifecycle.delete", "aws.s3.cors.delete":
			if d.Risk.Idempotency != "non_idempotent" {
				t.Errorf("%s idempotency = %s", d.ID, d.Risk.Idempotency)
			}
		}
	}
}

// The description is the confirmation text: it names what the call deletes or opens and the right it needs.
func TestS3ConfigDescriptionsSayWhatTheyDo(t *testing.T) {
	for id, parts := range map[string][]string{
		"aws.s3.lifecycle.put": {"Replace all lifecycle rules", "expiration deletes objects", "noncurrent version expiration deletes noncurrent versions",
			"its filter", "without a filter, or with an empty prefix, covers the whole bucket", "s3:PutLifecycleConfiguration", "bound account", "set filter.prefix to the same value"},
		"aws.s3.lifecycle.delete":    {"Remove all lifecycle rules", "s3:PutLifecycleConfiguration"},
		"aws.s3.lifecycle.get":       {"covers the whole bucket", "s3:GetLifecycleConfiguration", "NoSuchLifecycleConfiguration"},
		"aws.s3.cors.put":            {"Replace all CORS rules", "origin of * lets a browser on any website", "not a permission", "s3:PutBucketCORS"},
		"aws.s3.cors.delete":         {"Remove all CORS rules", "s3:PutBucketCORS"},
		"aws.s3.cors.get":            {"s3:GetBucketCORS", "NoSuchCORSConfiguration"},
		"aws.s3.website.get":         {"s3:GetBucketWebsite", "NoSuchWebsiteConfiguration"},
		"aws.s3.notification.get":    {"s3:GetBucketNotification", "to EventBridge is not shown", "follows none of them and fetches no URL"},
		"aws.s3.replication.get":     {"s3:GetReplicationConfiguration", "ReplicationConfigurationNotFoundError", "other accounts", "follows none of them and fetches no URL"},
		"aws.s3.ownership.get":       {"s3:GetBucketOwnershipControls", "OwnershipControlsNotFoundError"},
		"aws.s3.accelerate.get":      {"s3:GetAccelerateConfiguration"},
		"aws.s3.request-payment.get": {"s3:GetBucketRequestPayment"},
	} {
		d := descriptionOf(t, id)
		for _, p := range parts {
			if !strings.Contains(d, p) {
				t.Errorf("%s: description lacks %q: %s", id, p, d)
			}
		}
		if strings.HasSuffix(id, ".get") && !strings.Contains(d, "bound account is sent as the expected bucket owner") {
			t.Errorf("%s: description does not name the owner binding", id)
		}
	}
}

// No write exists for the configurations that can point at targets of other accounts, nor for ACLs, website,
// or ownership in this group; only the lifecycle and CORS tools write.
func TestS3ConfigWritesAreOnlyLifecycleAndCORS(t *testing.T) {
	writers := map[string]bool{}
	for _, d := range registered(t).Provider(Provider) {
		if strings.HasPrefix(d.ID, "aws.s3.") && d.Risk.Confirmation != "none" {
			writers[d.ID] = true
		}
	}
	for _, id := range []string{"aws.s3.acl.put", "aws.s3.website.put", "aws.s3.website.delete", "aws.s3.logging.put", "aws.s3.notification.put",
		"aws.s3.replication.put", "aws.s3.replication.delete", "aws.s3.ownership.put", "aws.s3.accelerate.put", "aws.s3.request-payment.put"} {
		if writers[id] {
			t.Errorf("%s is registered", id)
		}
	}
	for _, id := range []string{"aws.s3.lifecycle.put", "aws.s3.lifecycle.delete", "aws.s3.cors.put", "aws.s3.cors.delete"} {
		if !writers[id] {
			t.Errorf("%s is not a confirmed write", id)
		}
	}
	for _, s := range generatedSpecs {
		if s.service != "s3" || s.rest.Method == "GET" || s.rest.Method == "HEAD" {
			continue
		}
		for _, banned := range []string{"?logging", "?notification", "?replication", "?acl", "?website", "?accelerate", "?requestPayment", "?ownershipControls"} {
			if strings.Contains(s.rest.URI, banned) {
				t.Errorf("%s sends %s %s", s.descriptor.ID, s.rest.Method, s.rest.URI)
			}
		}
	}
}

// A rule of the old form, limited by a rule-level prefix, shows that prefix: it is not read as a rule without a
// filter. The put does not offer the old prefix.
func TestS3LifecycleGetShowsTheOldRulePrefix(t *testing.T) {
	f := restAnswer(xmlReply(`<LifecycleConfiguration ` + s3NS + `><Rule><ID>old</ID><Prefix>logs/</Prefix><Status>Enabled</Status>` +
		`<Expiration><Days>1</Days></Expiration></Rule></LifecycleConfiguration>`))
	text, err := newToolsEnv(t, f, baseValues(), s3ConfigReadIDs).invoke("aws.s3.lifecycle.get", `{"bucket":"my-bucket"}`)
	if err != nil {
		t.Fatal(err)
	}
	rule := decodeResult(t, text)["rules"].([]any)[0].(map[string]any)
	if rule["prefix"] != "logs/" || rule["filter"] != nil {
		t.Errorf("rule = %v", rule)
	}
	e := s3WriteEnv(t, restAnswer(reply{status: 200}), testRegion)
	if _, err := e.invokeConfirmed("aws.s3.lifecycle.put", `{"bucket":"my-bucket","lifecycle_configuration":{"rules":[{"id":"a","status":"Enabled","prefix":"logs/"}]}}`, true); err == nil {
		t.Error("the old rule-level prefix was accepted by the put")
	}
}
