package aws

import (
	"strings"
	"testing"
)

var sesJobWriteTools = map[string]bool{
	"aws.ses.import-job.create": true, "aws.ses.export-job.create": true, "aws.ses.export-job.cancel": true,
}

const (
	sesImportTool = "aws.ses.import-job.create"
	sesExportTool = "aws.ses.export-job.create"
	sesCancelTool = "aws.ses.export-job.cancel"
)

func importArgs(url string) string {
	return `{"import_destination":{"suppression_list_destination":{"suppression_list_import_action":"PUT"}},` +
		`"import_data_source":{"s3_url":"` + url + `","data_format":"CSV"}}`
}

func exportArgs(destination string) string {
	return `{"export_data_source":{"message_insights_data_source":{"start_date":"2026-01-01T00:00:00Z","end_date":"2026-01-02T00:00:00Z"}},` +
		`"export_destination":` + destination + `}`
}

// jobCalls splits what the fake saw into the bucket checks and the calls to SES.
func jobCalls(f *fakeAWS) (heads []sent, ses []sent) {
	for _, r := range restRequests(f) {
		if r.method == "HEAD" {
			heads = append(heads, r)
		} else {
			ses = append(ses, r)
		}
	}
	return heads, ses
}

// A job that names a bucket reaches SES only after HeadBucket answered 200 for the bound account as expected
// owner. Every other answer, a malformed URL, a missing confirmation, and a region outside the allowlist send
// nothing to SES; the refusal names s3:ListBucket where the check failed and quotes no bucket.
func TestSESJobBucketIsBoundToTheAccount(t *testing.T) {
	const url = "s3://secret-bucket/data/in.csv"
	cases := []struct {
		name, tool, args string
		confirm          bool
		head             reply
		allowed          bool
		heads            int
	}{
		{"import from the own bucket", sesImportTool, importArgs(url), true, reply{status: 200}, true, 1},
		{"import, bucket region header allowed", sesImportTool, importArgs(url), true,
			reply{status: 200, header: map[string]string{"x-amz-bucket-region": testRegion}}, true, 1},
		{"import, bucket region header not allowed", sesImportTool, importArgs(url), true,
			reply{status: 200, header: map[string]string{"x-amz-bucket-region": "ap-south-1"}}, false, 1},
		{"import, bucket of another account", sesImportTool, importArgs(url), true, forbidden("AccessDenied"), false, 1},
		{"import, bucket not found", sesImportTool, importArgs(url), true, reply{status: 404}, false, 1},
		{"import, redirect without a later success", sesImportTool, importArgs(url), true, reply{status: 301}, false, 1},
		{"import, server error", sesImportTool, importArgs(url), true, reply{status: 500}, false, -1},
		{"import without confirmation", sesImportTool, importArgs(url), false, reply{status: 200}, false, 0},
		{"import with a malformed URL", sesImportTool, importArgs("https://secret-bucket/in.csv"), true, reply{status: 200}, false, 0},
		{"import without a key", sesImportTool, importArgs("s3://secret-bucket/"), true, reply{status: 200}, false, 0},
		{"import without a bucket", sesImportTool, importArgs("s3:///in.csv"), true, reply{status: 200}, false, 0},
		{"import with an invalid bucket name", sesImportTool, importArgs("s3://Secret..Bucket/in.csv"), true, reply{status: 200}, false, 0},
		{"import with a bucket name that is too long", sesImportTool, importArgs("s3://" + strings.Repeat("a", 64) + "/in.csv"), true, reply{status: 200}, false, 0},
		{"import with a URL that is too long", sesImportTool, importArgs("s3://secret-bucket/" + strings.Repeat("a", 1100)), true, reply{status: 200}, false, 0},
		{"export to the own bucket", sesExportTool, exportArgs(`{"data_format":"CSV","s3_url":"` + url + `"}`), true, reply{status: 200}, true, 1},
		{"export to a bucket of another account", sesExportTool, exportArgs(`{"data_format":"CSV","s3_url":"` + url + `"}`), true, forbidden("AccessDenied"), false, 1},
		{"export to a bucket that is not found", sesExportTool, exportArgs(`{"data_format":"CSV","s3_url":"` + url + `"}`), true, reply{status: 404}, false, 1},
		{"export with a malformed URL", sesExportTool, exportArgs(`{"data_format":"CSV","s3_url":"secret-bucket/x"}`), true, reply{status: 200}, false, 0},
		{"export without a bucket", sesExportTool, exportArgs(`{"data_format":"JSON"}`), true, reply{status: 200}, true, 0},
		{"export without confirmation", sesExportTool, exportArgs(`{"data_format":"CSV","s3_url":"` + url + `"}`), false, reply{status: 200}, false, 0},
		{"cancel names no bucket", sesCancelTool, `{"job_id":"j1"}`, true, forbidden("AccessDenied"), true, 0},
		{"cancel without confirmation", sesCancelTool, `{"job_id":"j1"}`, false, reply{status: 200}, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				if r.method == "HEAD" {
					return c.head
				}
				return ok(`{"JobId":"j1"}`)
			})
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
			_, err := e.invokeConfirmed(c.tool, c.args, c.confirm)
			heads, calls := jobCalls(f)
			for _, h := range heads {
				if h.host != "secret-bucket.s3."+testRegion+".amazonaws.com" || h.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount || h.rawQuery != "" {
					t.Errorf("HeadBucket = %+v", h)
				}
			}
			// A server error is retried by the transport; -1 stands for "at least one".
			if (c.heads >= 0 && len(heads) != c.heads) || (c.heads < 0 && len(heads) < 1) {
				t.Errorf("HeadBucket requests = %d, want %d", len(heads), c.heads)
			}
			if c.allowed {
				if err != nil || len(calls) != 1 {
					t.Fatalf("err = %v, SES calls = %d, want one call", err, len(calls))
				}
				if got := restRequests(f); len(heads) == 1 && got[0].method != "HEAD" {
					t.Errorf("first request = %s, want the check first", got[0].method)
				}
				return
			}
			if err == nil || len(calls) != 0 {
				t.Fatalf("err = %v, SES calls = %d, want a refusal without a call to SES", err, len(calls))
			}
			for _, leak := range []string{"secret-bucket", "in.csv", "Secret"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("refusal %q quotes %q", err, leak)
				}
			}
			if len(heads) > 0 && c.head.status != 200 && !strings.Contains(err.Error(), "s3:ListBucket") {
				t.Errorf("refusal %q does not name s3:ListBucket", err)
			}
		})
	}
}

// A bucket in another allowed region is found by trying the allowed regions in turn; a redirect that no allowed
// region answers with 200, and a 403 in the other region, refuse the call.
func TestSESJobBucketInAnotherRegion(t *testing.T) {
	const other = "us-east-1"
	for _, c := range []struct {
		name    string
		answer  func(sent) reply
		allowed bool
		heads   int
	}{
		{"found in the other region", func(r sent) reply {
			if strings.Contains(r.host, other) {
				return reply{status: 200}
			}
			return reply{status: 301}
		}, true, 2},
		{"owner differs in the other region", func(r sent) reply {
			if strings.Contains(r.host, other) {
				return forbidden("AccessDenied")
			}
			return reply{status: 301}
		}, false, 2},
		{"in no allowed region", func(sent) reply { return reply{status: 301} }, false, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := direct(func(r sent, _ int) reply {
				if r.method == "HEAD" {
					return c.answer(r)
				}
				return ok(`{"JobId":"j1"}`)
			})
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion, "region/"+other)
			_, err := e.invokeConfirmed(sesImportTool, strings.Replace(importArgs("s3://secret-bucket/in.csv"), "{", `{"region":"`+testRegion+`",`, 1), true)
			heads, calls := jobCalls(f)
			if len(heads) != c.heads || (err == nil) != c.allowed || len(calls) != map[bool]int{true: 1, false: 0}[c.allowed] {
				t.Errorf("err = %v, heads = %d, SES calls = %d", err, len(heads), len(calls))
			}
		})
	}
}

// The jobs go to the SES endpoint of the called region with the paths and bodies of the model, and the content
// of the import file never appears in what the tool returns.
func TestSESJobRequests(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		if r.method == "HEAD" {
			return reply{status: 200}
		}
		return ok(`{"JobId":"j1"}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	for _, c := range []struct{ tool, args string }{
		{sesImportTool, importArgs("s3://secret-bucket/in.csv")},
		{sesExportTool, exportArgs(`{"data_format":"JSON"}`)},
		{sesCancelTool, `{"job_id":"j1"}`},
	} {
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
	}
	_, calls := jobCalls(f)
	if len(calls) != 3 ||
		calls[0].method != "POST" || calls[0].path != "/v2/email/import-jobs" || !strings.Contains(calls[0].body, `"S3Url":"s3://secret-bucket/in.csv"`) ||
		calls[1].method != "POST" || calls[1].path != "/v2/email/export-jobs" || !strings.Contains(calls[1].body, `"DataFormat":"JSON"`) ||
		calls[2].method != "PUT" || calls[2].path != "/v2/email/export-jobs/j1/cancel" {
		t.Fatalf("calls = %+v", calls)
	}
}
