package aws

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// The lifecycle text names bucket, region, and each deleting rule with its days or date and filter; a rule over the
// whole bucket comes first and is marked. The CORS text warns about the origin *. No unconfirmed call is sent.
func TestS3ConfigPutConfirmationTexts(t *testing.T) {
	rules := `{"bucket":"my-bucket","region":"` + testRegion + `","lifecycle_configuration":{"rules":[` +
		`{"id":"logs","status":"Enabled","filter":{"prefix":"logs/"},"expiration":{"days":30}},` +
		`{"id":"tagged","status":"Enabled","filter":{"tag":{"key":"temp","value":"yes"}},"expiration":{"date":"2027-01-01T00:00:00Z"}},` +
		`{"id":"both","status":"Disabled","filter":{"and":{"prefix":"a/","tags":[{"key":"k","value":"v"},{"key":"l","value":"w"}],"object_size_greater_than":100}},` +
		`"noncurrent_version_expiration":{"noncurrent_days":5}},` +
		`{"id":"move","status":"Enabled","filter":{"prefix":"m/"},"transitions":[{"days":10,"storage_class":"GLACIER"}]},` +
		`{"id":"whole","status":"Enabled","expiration":{"days":365}}]}}`
	for _, c := range []struct {
		id, args string
		want     []string
		not      []string
	}{
		{"aws.s3.lifecycle.put", rules, []string{"my-bucket", testRegion, "with 5 rules", "rules not given are removed", "4 rules delete data",
			": whole [WHOLE BUCKET] deletes objects after 365 days; logs (prefix logs/) deletes objects after 30 days",
			"tagged (tag temp=yes) deletes objects on 2027-01-01T00:00:00Z",
			"both [Disabled] (prefix a/, size > 100, tag k=v +1 more tags) deletes noncurrent versions after 5 days"}, []string{"move ("}},
		{"aws.s3.lifecycle.put", `{"bucket":"my-bucket","lifecycle_configuration":{"rules":[{"id":"a","status":"Enabled","filter":{"prefix":""},"expiration":{"days":1}}]}}`,
			[]string{"the one region the connection allows", "a [WHOLE BUCKET] deletes objects after 1 days"}, nil},
		{"aws.s3.lifecycle.put", `{"bucket":"my-bucket","lifecycle_configuration":{"rules":[{"id":"a","status":"Enabled","abort_incomplete_multipart_upload":{"days_after_initiation":7}}]}}`,
			[]string{"with 1 rules", "No rule deletes objects"}, []string{"WHOLE BUCKET"}},
		{"aws.s3.cors.put", `{"bucket":"my-bucket","region":"` + testRegion + `","cors_configuration":{"cors_rules":[` +
			`{"allowed_origins":["https://a.example","*"],"allowed_methods":["GET"]},{"allowed_origins":["https://b.example","https://c.example","*"],"allowed_methods":["PUT"]}]}}`,
			[]string{"my-bucket", testRegion, "with 2 rules", "origin * lets a browser on any website", "CORS is no permission",
				"Origins: https://a.example, *, https://b.example +1 more."}, nil},
		{"aws.s3.cors.put", `{"bucket":"my-bucket","cors_configuration":{"cors_rules":[{"allowed_origins":["https://a.example"],"allowed_methods":["GET"]}]}}`,
			[]string{"with 1 rules", "Origins: https://a.example."}, []string{"any website"}},
	} {
		f := restAnswer(reply{status: 200})
		e := s3WriteEnv(t, f, testRegion)
		got := confirmationSummary(t, e, c.id, c.args)
		for _, part := range c.want {
			if !strings.Contains(got, part) {
				t.Errorf("%s: summary %q lacks %q", c.id, got, part)
			}
		}
		for _, part := range c.not {
			if strings.Contains(got, part) {
				t.Errorf("%s: summary %q has %q", c.id, got, part)
			}
		}
		if len(f.requests) != 0 {
			t.Errorf("%s: an unconfirmed call reached the network", c.id)
		}
	}
}

// Many deleting rules stay within the budget, the rule over the whole bucket stays listed, and the rest is counted.
func TestS3LifecycleConfirmationTextIsBounded(t *testing.T) {
	var rules []string
	for i := range 60 {
		rules = append(rules, `{"id":"`+strings.Repeat("r", 60)+strconv.Itoa(i)+`","status":"Enabled","filter":{"prefix":"`+
			strings.Repeat("p", 200)+`"},"expiration":{"days":1}}`)
	}
	rules = append(rules, `{"id":"all","status":"Enabled","expiration":{"days":2}}`)
	got := lifecyclePutSummary(json.RawMessage(`{"bucket":"my-bucket","lifecycle_configuration":{"rules":[` + strings.Join(rules, ",") + `]}}`))
	if n := utf8.RuneCountInString(got); n > s3SummaryBudget || !strings.Contains(got, "all [WHOLE BUCKET] deletes objects after 2 days") ||
		!strings.Contains(got, "more not listed, check lifecycle_configuration") || !strings.Contains(got, "61 rules delete data") {
		t.Errorf("summary (%d runes) = %q", n, got)
	}
	for _, f := range []func(json.RawMessage) string{lifecyclePutSummary, corsPutSummary} {
		if f([]byte(`{"bucket":1}`)) != "" || f([]byte(`nope`)) != "" {
			t.Error("an unreadable argument gave a text")
		}
	}
}
