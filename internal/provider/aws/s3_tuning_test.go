package aws

import (
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var s3TuningToolIDs = []string{
	"aws.s3.metrics.list", "aws.s3.metrics.get", "aws.s3.metrics.put", "aws.s3.metrics.delete",
	"aws.s3.intelligent-tiering.list", "aws.s3.intelligent-tiering.get", "aws.s3.intelligent-tiering.put", "aws.s3.intelligent-tiering.delete",
	"aws.s3.accelerate.get", "aws.s3.accelerate.put"}

func s3TuningEnv(t *testing.T, f *fakeAWS, regions ...string) *env {
	t.Helper()
	targets := []string{"account/" + testAccount}
	for _, r := range regions {
		targets = append(targets, "region/"+r)
	}
	return newConnEnv(t, f, baseValues(), config.Connection{Tools: s3TuningToolIDs, Targets: targets})
}

const (
	tieringArgs = `{"bucket":"my-bucket","id":"archive","intelligent_tiering_configuration":{"id":"archive","status":"Enabled",` +
		`"filter":{"prefix":"logs/"},"tierings":[{"access_tier":"ARCHIVE_ACCESS","days":90}]}}`
	metricsArgs = `{"bucket":"my-bucket","id":"images","metrics_configuration":{"id":"images","filter":{"and":{"prefix":"images/","tags":[{"key":"k","value":"v"}]}}}}`
)

// Every tool sends its method, query, body, and the bound account as expected owner; the writes need the confirmation
// before any request, and the owner is never an argument.
func TestS3TuningToolsSendTheRequestOfTheModel(t *testing.T) {
	cases := []struct {
		id, args, method, query, body, answer string
		write                                 bool
	}{
		{id: "aws.s3.metrics.list", args: `{"bucket":"my-bucket"}`, method: "GET", query: "metrics&x-id=ListBucketMetricsConfigurations",
			answer: `<ListMetricsConfigurationsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated></ListMetricsConfigurationsResult>`},
		{id: "aws.s3.metrics.get", args: `{"bucket":"my-bucket","id":"images"}`, method: "GET", query: "metrics&x-id=GetBucketMetricsConfiguration&id=images",
			answer: `<MetricsConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Id>images</Id></MetricsConfiguration>`},
		{id: "aws.s3.metrics.put", args: metricsArgs, method: "PUT", query: "metrics&id=images", write: true,
			body: `<Filter><And><Prefix>images/</Prefix><Tag><Key>k</Key><Value>v</Value></Tag></And></Filter>`},
		{id: "aws.s3.metrics.delete", args: `{"bucket":"my-bucket","id":"images"}`, method: "DELETE", query: "metrics&id=images", write: true},
		{id: "aws.s3.intelligent-tiering.list", args: `{"bucket":"my-bucket"}`, method: "GET", query: "intelligent-tiering&x-id=ListBucketIntelligentTieringConfigurations",
			answer: `<ListBucketIntelligentTieringConfigurationsOutput xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated></ListBucketIntelligentTieringConfigurationsOutput>`},
		{id: "aws.s3.intelligent-tiering.get", args: `{"bucket":"my-bucket","id":"archive"}`, method: "GET", query: "intelligent-tiering&x-id=GetBucketIntelligentTieringConfiguration&id=archive",
			answer: `<IntelligentTieringConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Id>archive</Id></IntelligentTieringConfiguration>`},
		{id: "aws.s3.intelligent-tiering.put", args: tieringArgs, method: "PUT", query: "intelligent-tiering&id=archive", write: true,
			body: `<Status>Enabled</Status><Tiering><Days>90</Days><AccessTier>ARCHIVE_ACCESS</AccessTier></Tiering>`},
		{id: "aws.s3.intelligent-tiering.delete", args: `{"bucket":"my-bucket","id":"archive"}`, method: "DELETE", query: "intelligent-tiering&id=archive", write: true},
		{id: "aws.s3.accelerate.get", args: `{"bucket":"my-bucket"}`, method: "GET", query: "accelerate",
			answer: `<AccelerateConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></AccelerateConfiguration>`},
		{id: "aws.s3.accelerate.put", args: `{"bucket":"my-bucket","accelerate_configuration":{"status":"Suspended"}}`, method: "PUT", query: "accelerate", write: true,
			body: `<Status>Suspended</Status>`},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				if r.method == "GET" {
					return xmlReply(c.answer)
				}
				return reply{status: 200}
			})
			e := s3TuningEnv(t, f, testRegion)
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
			if _, err := e.invokeConfirmed(c.id, strings.Replace(c.args, `{"bucket"`, `{"expected_bucket_owner":"999999999999","bucket"`, 1), true); err == nil {
				t.Error("an owner argument was accepted")
			}
		})
	}
}

// Accelerate answers Enabled and Suspended as they are, and a bucket that never had it answers no status.
func TestS3AccelerateStatus(t *testing.T) {
	for _, c := range []struct{ answer, want string }{
		{`<Status>Enabled</Status>`, "Enabled"},
		{`<Status>Suspended</Status>`, "Suspended"},
		{``, ""},
	} {
		f := restAnswer(xmlReply(`<AccelerateConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` + c.answer + `</AccelerateConfiguration>`))
		text, err := s3TuningEnv(t, f, testRegion).invoke("aws.s3.accelerate.get", `{"bucket":"my-bucket"}`)
		if err != nil {
			t.Fatal(err)
		}
		got := decodeResult(t, text)
		if s, _ := got["status"].(string); s != c.want {
			t.Errorf("status = %v, want %q", got, c.want)
		}
	}
	e := s3TuningEnv(t, restAnswer(reply{status: 200}), testRegion)
	for _, status := range []string{"enabled", "Other", ""} {
		if _, err := e.invokeConfirmed("aws.s3.accelerate.put", `{"bucket":"my-bucket","accelerate_configuration":{"status":"`+status+`"}}`, true); err == nil {
			t.Errorf("status %q accepted", status)
		}
	}
}

// A bucket with a dot in its name is refused by S3; the error code of the answer is passed on, not hidden.
func TestS3AccelerateRefusalIsPassedOn(t *testing.T) {
	f := restAnswer(reply{status: 400, header: map[string]string{"Content-Type": "application/xml"},
		body: `<Error><Code>InvalidRequest</Code><Message>S3 Transfer Acceleration is not supported for buckets with periods (.) in their names</Message></Error>`})
	_, err := s3TuningEnv(t, f, testRegion).invokeConfirmed("aws.s3.accelerate.put", `{"bucket":"my.bucket","accelerate_configuration":{"status":"Enabled"}}`, true)
	var failed *provider.Error
	if !errorsAs(err, &failed) || failed.Original == nil || len(failed.Original.Entries) == 0 ||
		failed.Original.Entries[0].Code != "InvalidRequest" {
		t.Errorf("err = %v", err)
	}
}

// An unknown ID is not-found on every reader of a configuration.
func TestS3TuningMissingConfigurationIsNotFound(t *testing.T) {
	for _, c := range []struct{ id, args string }{
		{"aws.s3.metrics.get", `{"bucket":"my-bucket","id":"x"}`},
		{"aws.s3.intelligent-tiering.get", `{"bucket":"my-bucket","id":"x"}`},
	} {
		f := restAnswer(reply{status: 404, header: map[string]string{"Content-Type": "application/xml"},
			body: `<Error><Code>NoSuchConfiguration</Code><Message>m</Message></Error>`})
		if _, err := s3TuningEnv(t, f, testRegion).invoke(c.id, c.args); classOf(err) != provider.ClassNotFound {
			t.Errorf("%s: err = %v", c.id, err)
		}
	}
}

// The region argument must be one the connection allows; nothing is sent otherwise.
func TestS3TuningRegionIsChecked(t *testing.T) {
	for _, c := range []struct {
		id, args string
		write    bool
	}{
		{"aws.s3.metrics.list", `{"bucket":"my-bucket","region":"ap-south-1"}`, false},
		{"aws.s3.intelligent-tiering.put", strings.Replace(tieringArgs, `{"bucket"`, `{"region":"ap-south-1","bucket"`, 1), true},
		{"aws.s3.accelerate.put", `{"bucket":"my-bucket","region":"ap-south-1","accelerate_configuration":{"status":"Enabled"}}`, true},
	} {
		f := restAnswer(reply{status: 200})
		if _, err := s3TuningEnv(t, f, testRegion).invokeConfirmed(c.id, c.args, c.write); err == nil {
			t.Errorf("%s: a region outside the connection was accepted", c.id)
		}
		if len(f.requests) != 0 {
			t.Errorf("%s: a request was sent: %v", c.id, f.actions())
		}
	}
}

// The lists page with the continuation token of S3 inside the cursor, over three pages.
func TestS3TuningListsPageByContinuationToken(t *testing.T) {
	for _, c := range []struct{ id, root, item string }{
		{"aws.s3.metrics.list", "ListMetricsConfigurationsResult", "MetricsConfiguration"},
		{"aws.s3.intelligent-tiering.list", "ListBucketIntelligentTieringConfigurationsOutput", "IntelligentTieringConfiguration"},
	} {
		t.Run(c.id, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				v, _ := url.ParseQuery(r.rawQuery)
				token := v.Get("continuation-token")
				next, more := map[string]string{"": "t1", "t1": "t2"}[token], token != "t2"
				body := `<` + c.root + ` xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>` + map[bool]string{true: "true", false: "false"}[more] + `</IsTruncated>`
				if more {
					body += `<NextContinuationToken>` + next + `</NextContinuationToken>`
				}
				return xmlReply(body + `<` + c.item + `><Id>id-` + token + `</Id></` + c.item + `></` + c.root + `>`)
			})
			e := s3TuningEnv(t, f, testRegion)
			cursor, pages := "", 0
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
			var tokens []string
			for _, r := range restRequests(f) {
				v, _ := url.ParseQuery(r.rawQuery)
				tokens = append(tokens, v.Get("continuation-token"))
			}
			if pages != 3 || strings.Join(tokens, ",") != ",t1,t2" {
				t.Errorf("pages = %d, tokens = %q", pages, tokens)
			}
		})
	}
}

// The descriptions name the right of each tool and the cost they can cause, as documented by AWS.
func TestS3TuningDescriptions(t *testing.T) {
	for id, parts := range map[string][]string{
		"aws.s3.metrics.list":               {"s3:GetMetricsConfiguration", "CloudWatch"},
		"aws.s3.metrics.get":                {"s3:GetMetricsConfiguration", "NoSuchConfiguration"},
		"aws.s3.metrics.put":                {"s3:PutMetricsConfiguration", "CloudWatch"},
		"aws.s3.metrics.delete":             {"s3:PutMetricsConfiguration"},
		"aws.s3.intelligent-tiering.list":   {"s3:GetIntelligentTieringConfiguration", "monitoring"},
		"aws.s3.intelligent-tiering.get":    {"s3:GetIntelligentTieringConfiguration", "NoSuchConfiguration"},
		"aws.s3.intelligent-tiering.put":    {"s3:PutIntelligentTieringConfiguration", "monitoring"},
		"aws.s3.intelligent-tiering.delete": {"s3:PutIntelligentTieringConfiguration"},
		"aws.s3.accelerate.put":             {"s3:PutAccelerateConfiguration", "fee per GB", "dot", "no request through that endpoint"},
	} {
		d := descriptionOf(t, id)
		for _, p := range parts {
			if !strings.Contains(d, p) {
				t.Errorf("%s: description lacks %q: %s", id, p, d)
			}
		}
	}
}
