package aws

import (
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

const (
	ownKMS     = "arn:aws:kms:" + testRegion + ":" + testAccount + ":key/abc-123"
	reportHost = "reports.s3." + testRegion + ".amazonaws.com"
)

var s3ReportToolIDs = []string{"aws.s3.inventory.list", "aws.s3.inventory.get", "aws.s3.inventory.put", "aws.s3.inventory.delete",
	"aws.s3.analytics.list", "aws.s3.analytics.get", "aws.s3.analytics.put", "aws.s3.analytics.delete"}

// s3ReportEnv offers the inventory and analytics tools on a connection bound to the account and the given regions.
func s3ReportEnv(t *testing.T, f *fakeAWS, regions ...string) *env {
	t.Helper()
	targets := []string{"account/" + testAccount}
	for _, r := range regions {
		targets = append(targets, "region/"+r)
	}
	return newConnEnv(t, f, baseValues(), config.Connection{Tools: s3ReportToolIDs, Targets: targets})
}

func inventoryArgs(account, bucket, encryption string) string {
	acct := ""
	if account != "" {
		acct = `"account_id":"` + account + `",`
	}
	if encryption != "" {
		encryption = `,"encryption":` + encryption
	}
	return `{"bucket":"my-bucket","id":"weekly","inventory_configuration":{"id":"weekly","is_enabled":true,` +
		`"included_object_versions":"All","schedule":{"frequency":"Weekly"},"destination":{"s3_bucket_destination":{` +
		acct + `"bucket":"` + bucket + `","format":"CSV"` + encryption + `}}}}`
}

func analyticsArgs(account, bucket string) string {
	acct := ""
	if account != "" {
		acct = `"bucket_account_id":"` + account + `",`
	}
	return `{"bucket":"my-bucket","id":"all","analytics_configuration":{"id":"all","storage_class_analysis":{"data_export":{` +
		`"output_schema_version":"V_1","destination":{"s3_bucket_destination":{` + acct + `"format":"CSV","bucket":"` + bucket + `"}}}}}}`
}

func forbidden(code string) reply {
	return reply{status: 403, header: map[string]string{"Content-Type": "application/xml"}, body: `<Error><Code>` + code + `</Code><Message>m</Message></Error>`}
}

// The destination of a report must be a bucket of the bound account: what the arguments say is judged before any
// request, and HeadBucket with the bound account as expected owner confirms it. Anything else sends no change.
func TestS3ReportDestinationIsBoundToTheAccount(t *testing.T) {
	const own = "arn:aws:s3:::reports"
	sse := func(key string) string { return `{"ssekms":{"key_id":"` + key + `"}}` }
	cases := []struct {
		name, tool, args string
		head             reply
		allowed          bool
		heads            int
	}{
		{"inventory in the own account", "aws.s3.inventory.put", inventoryArgs(testAccount, own, ""), reply{status: 200}, true, 1},
		{"inventory with an own SSE-KMS key", "aws.s3.inventory.put", inventoryArgs(testAccount, own, sse(ownKMS)), reply{status: 200}, true, 1},
		{"inventory with SSE-S3", "aws.s3.inventory.put", inventoryArgs(testAccount, own, `{"sses3":{}}`), reply{status: 200}, true, 1},
		{"inventory of another account", "aws.s3.inventory.put", inventoryArgs("999999999999", own, ""), reply{status: 200}, false, 0},
		{"inventory without an account", "aws.s3.inventory.put", inventoryArgs("", own, ""), reply{status: 200}, false, 0},
		{"inventory with a malformed account", "aws.s3.inventory.put", inventoryArgs("1234", own, ""), reply{status: 200}, false, 0},
		{"inventory with a bare name", "aws.s3.inventory.put", inventoryArgs(testAccount, "reports", ""), reply{status: 200}, false, 0},
		{"inventory with an ARN of another service", "aws.s3.inventory.put", inventoryArgs(testAccount, "arn:aws:sns:::reports", ""), reply{status: 200}, false, 0},
		{"inventory with an account in the ARN", "aws.s3.inventory.put", inventoryArgs(testAccount, "arn:aws:s3::"+testAccount+":reports", ""), reply{status: 200}, false, 0},
		{"inventory with another partition", "aws.s3.inventory.put", inventoryArgs(testAccount, "arn:aws-cn:s3:::reports", ""), reply{status: 200}, false, 0},
		{"inventory with an invalid name", "aws.s3.inventory.put", inventoryArgs(testAccount, "arn:aws:s3:::a/b", ""), reply{status: 200}, false, 0},
		{"inventory with a key of another account", "aws.s3.inventory.put",
			inventoryArgs(testAccount, own, sse("arn:aws:kms:"+testRegion+":999999999999:key/abc")), reply{status: 200}, false, 0},
		{"inventory with a bare key ID", "aws.s3.inventory.put", inventoryArgs(testAccount, own, sse("abc-123")), reply{status: 200}, false, 0},
		{"inventory with an alias", "aws.s3.inventory.put",
			inventoryArgs(testAccount, own, sse("arn:aws:kms:"+testRegion+":"+testAccount+":alias/a")), reply{status: 200}, false, 0},
		{"inventory with a key in a region not allowed", "aws.s3.inventory.put",
			inventoryArgs(testAccount, own, sse("arn:aws:kms:us-west-2:"+testAccount+":key/abc")), reply{status: 200}, false, 0},
		{"inventory, HeadBucket reports another owner", "aws.s3.inventory.put", inventoryArgs(testAccount, own, ""), forbidden("AccessDenied"), false, 1},
		{"inventory, HeadBucket finds no bucket", "aws.s3.inventory.put", inventoryArgs(testAccount, own, ""), reply{status: 404}, false, 1},
		{"inventory, HeadBucket fails", "aws.s3.inventory.put", inventoryArgs(testAccount, own, ""), reply{status: 500}, false, -1},
		{"analytics in the own account", "aws.s3.analytics.put", analyticsArgs(testAccount, own), reply{status: 200}, true, 1},
		{"analytics of another account", "aws.s3.analytics.put", analyticsArgs("999999999999", own), reply{status: 200}, false, 0},
		{"analytics without an account", "aws.s3.analytics.put", analyticsArgs("", own), reply{status: 200}, false, 0},
		{"analytics with a bare name", "aws.s3.analytics.put", analyticsArgs(testAccount, "reports"), reply{status: 200}, false, 0},
		{"analytics with an account in the ARN", "aws.s3.analytics.put", analyticsArgs(testAccount, "arn:aws:s3::"+testAccount+":reports"), reply{status: 200}, false, 0},
		{"analytics, HeadBucket reports another owner", "aws.s3.analytics.put", analyticsArgs(testAccount, own), forbidden("AccessDenied"), false, 1},
		{"analytics, HeadBucket fails", "aws.s3.analytics.put", analyticsArgs(testAccount, own), reply{status: 500}, false, -1},
		{"analytics without a data export", "aws.s3.analytics.put",
			`{"bucket":"my-bucket","id":"all","analytics_configuration":{"id":"all","filter":{"prefix":"a/"},"storage_class_analysis":{}}}`, reply{status: 200}, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				if r.method == "HEAD" {
					return c.head
				}
				return reply{status: 200}
			})
			e := s3ReportEnv(t, f, testRegion)
			_, err := e.invokeConfirmed(c.tool, c.args, true)
			var heads, puts int
			for _, r := range restRequests(f) {
				switch r.method {
				case "HEAD":
					heads++
					if r.host != reportHost || r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount || r.rawQuery != "" {
						t.Errorf("HeadBucket = %+v", r)
					}
				case "PUT":
					puts++
				}
			}
			// A server error is retried by the transport; -1 stands for "at least one".
			if (c.heads >= 0 && heads != c.heads) || (c.heads < 0 && heads < 1) {
				t.Errorf("HeadBucket requests = %d, want %d", heads, c.heads)
			}
			if c.allowed {
				if err != nil || puts != 1 {
					t.Fatalf("err = %v, puts = %d, want one change", err, puts)
				}
				if got := restRequests(f); c.heads == 1 && (got[0].method != "HEAD" || got[1].method != "PUT") {
					t.Errorf("order = %s then %s, want the check first", got[0].method, got[1].method)
				}
				return
			}
			if err == nil || puts != 0 {
				t.Fatalf("err = %v, puts = %d, want a refusal without a change", err, puts)
			}
			// The refusal names the rule and none of the values it judged.
			for _, foreign := range []string{"999999999999", "a/b", "reports"} {
				if strings.Contains(err.Error(), foreign) {
					t.Errorf("refusal %q quotes %q", err, foreign)
				}
			}
		})
	}
}

// A destination in another region than the call is not refused: S3 does not follow the redirect, so the other
// regions of the connection are tried; a region that confirms the owner lets the change through, and one that
// does not, or none, refuses it.
func TestS3ReportDestinationInAnotherRegion(t *testing.T) {
	const other = "us-east-1"
	for _, c := range []struct {
		name    string
		answer  func(sent) reply
		allowed bool
		heads   []string
	}{
		{"found in the other region", func(r sent) reply {
			if strings.Contains(r.host, other) {
				return reply{status: 200}
			}
			return reply{status: 301}
		}, true, []string{testRegion, other}},
		{"owner differs in the other region", func(r sent) reply {
			if strings.Contains(r.host, other) {
				return forbidden("AccessDenied")
			}
			return reply{status: 301}
		}, false, []string{testRegion, other}},
		{"in no allowed region", func(sent) reply { return reply{status: 301} }, false, []string{testRegion, other}},
		{"a refusal in the first region ends the search", func(sent) reply { return forbidden("AccessDenied") }, false, []string{testRegion}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				if r.method == "HEAD" {
					return c.answer(r)
				}
				return reply{status: 200}
			})
			e := s3ReportEnv(t, f, testRegion, other)
			_, err := e.invokeConfirmed("aws.s3.inventory.put", strings.Replace(inventoryArgs(testAccount, "arn:aws:s3:::reports", ""), `"id":"weekly","inventory`, `"region":"`+testRegion+`","id":"weekly","inventory`, 1), true)
			var heads []string
			puts := 0
			for _, r := range restRequests(f) {
				switch r.method {
				case "HEAD":
					heads = append(heads, strings.TrimSuffix(strings.TrimPrefix(r.host, "reports.s3."), ".amazonaws.com"))
					if r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount {
						t.Errorf("HeadBucket without the owner: %+v", r)
					}
				case "PUT":
					puts++
				}
			}
			if strings.Join(heads, ",") != strings.Join(c.heads, ",") {
				t.Errorf("HeadBucket regions = %v, want %v", heads, c.heads)
			}
			if (err == nil) != c.allowed || puts != map[bool]int{true: 1, false: 0}[c.allowed] {
				t.Errorf("err = %v, puts = %d, allowed = %v", err, puts, c.allowed)
			}
		})
	}
}

// Every tool sends its method, path, query, body, and the bound account as expected owner, and the writes need
// the confirmation before any request.
func TestS3ReportToolsSendTheRequestOfTheModel(t *testing.T) {
	inv := inventoryArgs(testAccount, "arn:aws:s3:::reports", `{"ssekms":{"key_id":"`+ownKMS+`"}}`)
	an := analyticsArgs(testAccount, "arn:aws:s3:::reports")
	cases := []struct {
		id, args, method, query, body string
		write                         bool
		answer                        string
	}{
		{id: "aws.s3.inventory.list", args: `{"bucket":"my-bucket"}`, method: "GET", query: "inventory&x-id=ListBucketInventoryConfigurations",
			answer: `<ListInventoryConfigurationsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated></ListInventoryConfigurationsResult>`},
		{id: "aws.s3.inventory.get", args: `{"bucket":"my-bucket","id":"weekly"}`, method: "GET", query: "inventory&x-id=GetBucketInventoryConfiguration&id=weekly",
			answer: `<InventoryConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Id>weekly</Id></InventoryConfiguration>`},
		{id: "aws.s3.inventory.put", args: inv, method: "PUT", query: "inventory&id=weekly", write: true,
			body: `<Bucket>arn:aws:s3:::reports</Bucket><Format>CSV</Format><Encryption><SSE-KMS><KeyId>` + ownKMS + `</KeyId></SSE-KMS></Encryption>`},
		{id: "aws.s3.inventory.delete", args: `{"bucket":"my-bucket","id":"weekly"}`, method: "DELETE", query: "inventory&id=weekly", write: true},
		{id: "aws.s3.analytics.list", args: `{"bucket":"my-bucket"}`, method: "GET", query: "analytics&x-id=ListBucketAnalyticsConfigurations",
			answer: `<ListBucketAnalyticsConfigurationResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated></ListBucketAnalyticsConfigurationResult>`},
		{id: "aws.s3.analytics.get", args: `{"bucket":"my-bucket","id":"all"}`, method: "GET", query: "analytics&x-id=GetBucketAnalyticsConfiguration&id=all",
			answer: `<AnalyticsConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Id>all</Id></AnalyticsConfiguration>`},
		{id: "aws.s3.analytics.put", args: an, method: "PUT", query: "analytics&id=all", write: true,
			body: `<BucketAccountId>` + testAccount + `</BucketAccountId><Bucket>arn:aws:s3:::reports</Bucket>`},
		{id: "aws.s3.analytics.delete", args: `{"bucket":"my-bucket","id":"all"}`, method: "DELETE", query: "analytics&id=all", write: true},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				if r.method == "GET" {
					return xmlReply(c.answer)
				}
				return reply{status: 200}
			})
			e := s3ReportEnv(t, f, testRegion)
			if c.write {
				if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
					t.Fatalf("without confirmation: %v", err)
				}
				if len(f.requests) != 0 {
					t.Fatalf("an unconfirmed write reached the network: %v", f.actions())
				}
			}
			if _, err := e.invokeConfirmed(c.id, c.args, c.write); err != nil {
				t.Fatal(err)
			}
			calls := restRequests(f)
			r := calls[len(calls)-1]
			if r.method != c.method || r.rawQuery != c.query || r.host != "my-bucket.s3."+testRegion+".amazonaws.com" || r.path != "/" ||
				r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount || !strings.Contains(r.body, c.body) {
				t.Errorf("request = %+v", r)
			}
			// The owner is no argument.
			if _, err := e.invokeConfirmed(c.id, strings.Replace(c.args, `{"bucket"`, `{"expected_bucket_owner":"999999999999","bucket"`, 1), true); err == nil {
				t.Error("an owner argument was accepted")
			}
		})
	}
}

// The lists page with the continuation token of S3 inside the cursor, over three pages, for both kinds.
func TestS3ReportListsPageByContinuationToken(t *testing.T) {
	for _, c := range []struct{ id, root, item, query string }{
		{"aws.s3.inventory.list", "ListInventoryConfigurationsResult", "InventoryConfiguration", "inventory"},
		{"aws.s3.analytics.list", "ListBucketAnalyticsConfigurationResult", "AnalyticsConfiguration", "analytics"},
	} {
		t.Run(c.id, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				token := r.rawQuery[strings.Index(r.rawQuery, "continuation-token=")+len("continuation-token="):]
				if !strings.Contains(r.rawQuery, "continuation-token=") {
					token = ""
				}
				next, more := map[string]string{"": "t1", "t1": "t2"}[token], token != "t2"
				body := `<` + c.root + ` xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>` + map[bool]string{true: "true", false: "false"}[more] + `</IsTruncated>`
				if more {
					body += `<NextContinuationToken>` + next + `</NextContinuationToken>`
				}
				return xmlReply(body + `<` + c.item + `><Id>id-` + token + `</Id></` + c.item + `></` + c.root + `>`)
			})
			e := s3ReportEnv(t, f, testRegion)
			cursor, pages := "", 0
			var queries []string
			for {
				args := `{"bucket":"my-bucket"}`
				if cursor != "" {
					args = `{"bucket":"my-bucket","cursor":"` + cursor + `"}`
				}
				text, err := e.invoke(c.id, args)
				if err != nil {
					t.Fatal(err)
				}
				got := decodeResult(t, text)
				pages++
				next, _ := got["next_cursor"].(string)
				if more, _ := got["has_more"].(bool); more != (next != "") {
					t.Fatalf("has_more = %v with cursor %q", more, next)
				}
				if next == "" {
					break
				}
				cursor = next
				if pages > 3 {
					t.Fatal("more than three pages")
				}
			}
			for _, r := range restRequests(f) {
				v, _ := url.ParseQuery(r.rawQuery)
				queries = append(queries, v.Get("continuation-token"))
			}
			if pages != 3 || strings.Join(queries, ",") != ",t1,t2" {
				t.Errorf("pages = %d, tokens = %q", pages, queries)
			}
		})
	}
}
