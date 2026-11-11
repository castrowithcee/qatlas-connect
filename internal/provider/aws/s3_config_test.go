package aws

import (
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

// The deletes send DELETE with the expected owner and need the confirmation before any request.
func TestS3ConfigWritesNeedConfirmation(t *testing.T) {
	cases := []struct{ id, args, query string }{
		{"aws.s3.lifecycle.delete", `{"bucket":"my-bucket"}`, "lifecycle"},
		{"aws.s3.cors.delete", `{"bucket":"my-bucket"}`, "cors"},
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
		if r.method != "DELETE" || r.rawQuery != c.query || r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
			t.Errorf("%s request = %+v", c.id, r)
		}
	}
	for _, d := range registered(t).Provider(Provider) {
		switch d.ID {
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
		"aws.s3.lifecycle.delete":    {"Remove all lifecycle rules", "s3:PutLifecycleConfiguration"},
		"aws.s3.lifecycle.get":       {"covers the whole bucket", "old form shows its prefix on the rule itself", "s3:GetLifecycleConfiguration", "NoSuchLifecycleConfiguration"},
		"aws.s3.cors.delete":         {"Remove all CORS rules", "s3:PutBucketCORS"},
		"aws.s3.cors.get":            {"s3:GetBucketCORS", "NoSuchCORSConfiguration"},
		"aws.s3.acl.get":             {"s3:GetBucketAcl", "follows none of them and fetches no URL"},
		"aws.s3.logging.get":         {"s3:GetBucketLogging", "follows none of them and fetches no URL"},
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
// ownership, or the setting of lifecycle and CORS rules; only the two deletes write.
func TestS3ConfigWritesAreOnlyLifecycleAndCORSDeletes(t *testing.T) {
	writers := map[string]bool{}
	for _, d := range registered(t).Provider(Provider) {
		if strings.HasPrefix(d.ID, "aws.s3.") && d.Risk.Confirmation != "none" {
			writers[d.ID] = true
		}
	}
	for _, id := range []string{"aws.s3.acl.put", "aws.s3.website.put", "aws.s3.website.delete", "aws.s3.logging.put", "aws.s3.notification.put",
		"aws.s3.replication.put", "aws.s3.replication.delete", "aws.s3.ownership.put", "aws.s3.accelerate.put", "aws.s3.request-payment.put", "aws.s3.lifecycle.put", "aws.s3.cors.put"} {
		if writers[id] {
			t.Errorf("%s is registered", id)
		}
	}
	for _, id := range []string{"aws.s3.lifecycle.delete", "aws.s3.cors.delete"} {
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
// filter.
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
}
