package aws

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	acmARN        = "arn:aws:acm:" + testRegion + ":" + testAccount + ":certificate/1234abcd-12ab-34cd-56ef-1234567890ab"
	sesStartEnd   = `"start_date":"2026-01-01T00:00:00Z","end_date":"2026-01-08T00:00:00Z"`
	sesMetricsArg = `{"queries":[{"id":"q1","namespace":"VDM","metric":"SEND",` + sesStartEnd + `,"dimensions":{"EMAIL_IDENTITY":"example.com"}}]}`

	testReportArgs = `{"from_email_address":"example.com","content":{"simple":{"subject":{"data":"Test"},"body":{"text":{"data":"Hello"}}}}}`
)

var sesDeliverabilityTools = map[string]struct {
	effect      capability.Effect
	idem        capability.Idempotency
	sensitivity string
}{
	"aws.ses.metrics.batch-get":                       {capability.EffectRead, capability.IdempotencySafe, "aws-ses-deliverability"},
	"aws.ses.message-insights.get":                    {capability.EffectRead, capability.IdempotencySafe, "aws-ses-third-party-content"},
	"aws.ses.email-address-insights.get":              {capability.EffectRead, capability.IdempotencySafe, "aws-ses-third-party-content"},
	"aws.ses.blacklist-report.get":                    {capability.EffectRead, capability.IdempotencySafe, "aws-ses-deliverability"},
	"aws.ses.domain-statistics-report.get":            {capability.EffectRead, capability.IdempotencySafe, "aws-ses-deliverability"},
	"aws.ses.deliverability-campaign.get":             {capability.EffectRead, capability.IdempotencySafe, "aws-ses-third-party-content"},
	"aws.ses.deliverability-campaign.list":            {capability.EffectRead, capability.IdempotencySafe, "aws-ses-third-party-content"},
	"aws.ses.recommendation.list":                     {capability.EffectRead, capability.IdempotencySafe, "aws-ses-deliverability"},
	"aws.ses.deliverability-dashboard.get":            {capability.EffectRead, capability.IdempotencySafe, "aws-ses-deliverability"},
	"aws.ses.deliverability-dashboard.put-option":     {capability.EffectUpdate, capability.IdempotencyIdempotent, "aws-ses-deliverability"},
	"aws.ses.deliverability-test-report.get":          {capability.EffectRead, capability.IdempotencySafe, "aws-ses-third-party-content"},
	"aws.ses.deliverability-test-report.list":         {capability.EffectRead, capability.IdempotencySafe, "aws-ses-third-party-content"},
	"aws.ses.deliverability-test-report.create":       {capability.EffectCreate, capability.IdempotencyNonIdempotent, "aws-ses-deliverability"},
	"aws.ses.import-job.get":                          {capability.EffectRead, capability.IdempotencySafe, "aws-ses-jobs"},
	"aws.ses.import-job.list":                         {capability.EffectRead, capability.IdempotencySafe, "aws-ses-jobs"},
	"aws.ses.export-job.get":                          {capability.EffectRead, capability.IdempotencySafe, "aws-ses-jobs"},
	"aws.ses.export-job.list":                         {capability.EffectRead, capability.IdempotencySafe, "aws-ses-jobs"},
	"aws.ses.email-identity-certificate.list":         {capability.EffectRead, capability.IdempotencySafe, "aws-ses-deliverability"},
	"aws.ses.email-identity-certificate.associate":    {capability.EffectUpdate, capability.IdempotencyIdempotent, "aws-ses-deliverability"},
	"aws.ses.email-identity-certificate.disassociate": {capability.EffectUpdate, capability.IdempotencyIdempotent, "aws-ses-deliverability"},
}

func sesDeliverabilityDescriptors() map[string]capability.Descriptor {
	out := map[string]capability.Descriptor{}
	for _, s := range generatedSpecs {
		if _, ok := sesDeliverabilityTools[s.descriptor.ID]; ok {
			out[s.descriptor.ID] = s.descriptor
		}
	}
	return out
}

// The 20 tools exist with the effect, idempotency, and data sensitivity of the table; writes need
// confirmation, reads do not, and none is listed-only.
func TestSESDeliverabilityToolsRisk(t *testing.T) {
	seen := sesDeliverabilityDescriptors()
	if len(seen) != 20 || len(sesDeliverabilityTools) != 20 {
		t.Fatalf("%d tools, want 20", len(seen))
	}
	for id, w := range sesDeliverabilityTools {
		d := seen[id]
		confirmation := capability.ConfirmationRequired
		if w.effect == capability.EffectRead {
			confirmation = capability.ConfirmationNone
		}
		if d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idem || d.Risk.Confirmation != confirmation ||
			!d.Risk.OpenWorld || d.Risk.DataSensitivity != w.sensitivity || d.RequiresToolAllowList || d.Group != "ses" {
			t.Errorf("%s descriptor = %+v", id, d)
		}
	}
}

// The paid actions say so, the test report names the mail it sends, and insights name untrusted third-party data.
func TestSESDeliverabilityDescriptions(t *testing.T) {
	seen := sesDeliverabilityDescriptors()
	has := func(id string, parts ...string) {
		t.Helper()
		for _, p := range parts {
			if !strings.Contains(seen[id].Description, p) {
				t.Errorf("%s description lacks %q: %s", id, p, seen[id].Description)
			}
		}
	}
	has("aws.ses.deliverability-dashboard.put-option", "paid", "charged by AWS", "price is not retrieved", "Needs confirmation")
	has("aws.ses.deliverability-test-report.create", "sends one test message", "mailboxes at AWS", "cannot be recalled",
		"charged by AWS", "price is not retrieved", "Not repeated after an unclear outcome", "Needs confirmation")
	has("aws.ses.metrics.batch-get", "1 to 10", "16 calls and 160 queries per second")
	for _, id := range []string{"aws.ses.message-insights.get", "aws.ses.deliverability-campaign.get", "aws.ses.deliverability-campaign.list",
		"aws.ses.deliverability-test-report.get", "aws.ses.deliverability-test-report.list"} {
		has(id, "third parties", "untrusted")
	}
	has("aws.ses.email-identity-certificate.associate", "bound account", "Needs confirmation")
}

// The test report offers simple content only: no raw content, template, attachment, or header.
func TestSESDeliverabilityTestReportOffersSimpleContentOnly(t *testing.T) {
	d := sesDeliverabilityDescriptors()["aws.ses.deliverability-test-report.create"]
	schema := strings.ToLower(string(d.InputSchema))
	for _, banned := range []string{`"raw"`, `"template`, `"attachments"`, `"headers"`} {
		if strings.Contains(schema, banned) {
			t.Errorf("input schema offers %s", banned)
		}
	}
	if !strings.Contains(schema, `"simple"`) {
		t.Error("input schema lacks simple content")
	}
}

// Insights return capped strings, and the schema says so.
func TestSESInsightsAreCapped(t *testing.T) {
	d := sesDeliverabilityDescriptors()["aws.ses.message-insights.get"]
	var schema struct {
		Properties map[string]struct {
			MaxLength int `json:"maxLength"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(d.OutputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Properties["subject"].MaxLength != 998 || schema.Properties["from_email_address"].MaxLength != 320 {
		t.Errorf("output schema = %s", d.OutputSchema)
	}
}

// Metrics send one POST with the queries; results and per-query errors are returned.
func TestSESBatchGetMetricData(t *testing.T) {
	f := sesFake(func(sent) reply {
		return ok(`{"Results":[{"Id":"q1","Timestamps":[1767225600],"Values":[12]}],"Errors":[{"Id":"q2","Code":"INTERNAL_FAILURE","Message":"text"}]}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	text, err := e.invoke("aws.ses.metrics.batch-get", sesMetricsArg)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	results, _ := got["results"].([]any)
	errs, _ := got["errors"].([]any)
	if len(results) != 1 || len(errs) != 1 {
		t.Fatalf("result = %s", text)
	}
	calls := restCalls(f)
	if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != "/v2/email/metrics/batch" {
		t.Fatalf("calls = %+v", calls)
	}
	var body struct{ Queries []map[string]any }
	if err := json.Unmarshal([]byte(calls[0].body), &body); err != nil || len(body.Queries) != 1 ||
		body.Queries[0]["Metric"] != "SEND" || body.Queries[0]["Namespace"] != "VDM" {
		t.Errorf("body = %s (%v)", calls[0].body, err)
	}
}

// The limits of the model and the API reference that the schema carries hold before any I/O: 1 to 10 queries,
// only the listed metric and namespace, an id of 1 to 255 characters, and the dates. The number and names of the
// dimensions are left to AWS, and the documentation names no limit on the period, so none is checked.
func TestSESBatchGetMetricDataLimitsBeforeIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	query := func(id, metric, ns, dims string) string {
		q := `{"id":"` + id + `","namespace":"` + ns + `","metric":"` + metric + `",` + sesStartEnd
		if dims != "" {
			q += `,"dimensions":` + dims
		}
		return q + `}`
	}
	many := make([]string, 11)
	for i := range many {
		many[i] = query("q", "SEND", "VDM", "")
	}
	for name, args := range map[string]string{
		"no queries":        `{"queries":[]}`,
		"eleven queries":    `{"queries":[` + strings.Join(many, ",") + `]}`,
		"unknown metric":    `{"queries":[` + query("q", "BOUNCE", "VDM", "") + `]}`,
		"unknown namespace": `{"queries":[` + query("q", "SEND", "AWS", "") + `]}`,
		"empty id":          `{"queries":[` + query("", "SEND", "VDM", "") + `]}`,
		"long id":           `{"queries":[` + query(strings.Repeat("a", 256), "SEND", "VDM", "") + `]}`,
		"no dates":          `{"queries":[{"id":"q","namespace":"VDM","metric":"SEND"}]}`,
	} {
		if _, err := e.invoke("aws.ses.metrics.batch-get", args); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
	if _, err := e.invoke("aws.ses.metrics.batch-get", `{"queries":[`+strings.Join(many[:10], ",")+`]}`); err != nil {
		t.Errorf("ten queries: %v", err)
	}
	if n := len(restCalls(f)); n != 1 {
		t.Errorf("%d calls, want only the valid one", n)
	}
}

// Each read sends the request of its operation and returns the answer's members.
func TestSESDeliverabilityReads(t *testing.T) {
	for _, c := range []struct {
		tool, args, answer, method, path, query, member string
	}{
		{"aws.ses.message-insights.get", `{"message_id":"msg-1"}`, `{"MessageId":"msg-1","Subject":"Hello","Insights":[{"Isp":"X","Events":[{"Type":"SEND"}]}]}`,
			"GET", "/v2/email/insights/msg-1", "", "subject"},
		{"aws.ses.email-address-insights.get", `{"email_address":"user@example.com"}`, `{"MailboxValidation":{"IsValid":{"ConfidenceVerdict":"HIGH"}}}`,
			"POST", "/v2/email/email-address-insights", "", "mailbox_validation"},
		{"aws.ses.blacklist-report.get", `{"blacklist_item_names":["192.0.2.1"]}`, `{"BlacklistReport":{"192.0.2.1":[{"RblName":"x","Description":"d"}]}}`,
			"GET", "/v2/email/deliverability-dashboard/blacklist-report", "BlacklistItemNames=192.0.2.1", "blacklist_report"},
		{"aws.ses.domain-statistics-report.get", `{"domain":"example.com",` + sesStartEnd + `}`, `{"OverallVolume":{"ReadRatePercent":1},"DailyVolumes":[]}`,
			"GET", "/v2/email/deliverability-dashboard/statistics-report/example.com", "", "overall_volume"},
		{"aws.ses.deliverability-campaign.get", `{"campaign_id":"c1"}`, `{"DomainDeliverabilityCampaign":{"CampaignId":"c1","Subject":"s"}}`,
			"GET", "/v2/email/deliverability-dashboard/campaigns/c1", "", "domain_deliverability_campaign"},
		{"aws.ses.deliverability-dashboard.get", `{}`, `{"DashboardEnabled":false,"AccountStatus":"DISABLED"}`,
			"GET", "/v2/email/deliverability-dashboard", "", "dashboard_enabled"},
		{"aws.ses.deliverability-test-report.get", `{"report_id":"r1"}`, `{"DeliverabilityTestReport":{"ReportId":"r1"},"OverallPlacement":{"InboxPercentage":90},"IspPlacements":[{"IspName":"X"}],"Message":"m"}`,
			"GET", "/v2/email/deliverability-dashboard/test-reports/r1", "", "overall_placement"},
		{"aws.ses.import-job.get", `{"job_id":"j1"}`, `{"JobId":"j1","JobStatus":"COMPLETED","ProcessedRecordsCount":3}`,
			"GET", "/v2/email/import-jobs/j1", "", "job_status"},
		{"aws.ses.export-job.get", `{"job_id":"j2"}`, `{"JobId":"j2","JobStatus":"PROCESSING","ExportSourceType":"METRICS_DATA"}`,
			"GET", "/v2/email/export-jobs/j2", "", "job_status"},
		{"aws.ses.import-job.list", `{"import_destination_type":"SUPPRESSION_LIST"}`, `{"ImportJobs":[{"JobId":"j1"}]}`,
			"POST", "/v2/email/import-jobs/list", "", "import_jobs"},
		{"aws.ses.export-job.list", `{"job_status":"COMPLETED"}`, `{"ExportJobs":[{"JobId":"j2"}]}`,
			"POST", "/v2/email/list-export-jobs", "", "export_jobs"},
		{"aws.ses.recommendation.list", `{"filter":{"STATUS":"OPEN"}}`, `{"Recommendations":[{"Type":"DKIM"}]}`,
			"POST", "/v2/email/vdm/recommendations", "", "recommendations"},
		{"aws.ses.email-identity-certificate.list", `{"email_identity":"example.com"}`, `{"Certificates":[{"Status":"ACTIVE","CertificateArn":"` + acmARN + `"}]}`,
			"POST", "/v2/email/identity/certificates/list", "", "certificates"},
	} {
		t.Run(c.tool, func(t *testing.T) {
			f := sesFake(func(sent) reply { return ok(c.answer) })
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
			text, err := e.invoke(c.tool, c.args)
			if err != nil {
				t.Fatal(err)
			}
			if _, has := decodeResult(t, text)[c.member]; !has {
				t.Errorf("result lacks %s: %s", c.member, text)
			}
			calls := restCalls(f)
			if len(calls) != 1 || calls[0].method != c.method || calls[0].path != c.path || (c.query != "" && !strings.Contains(calls[0].rawQuery, c.query)) {
				t.Errorf("calls = %+v", calls)
			}
		})
	}
}

// The list tools page over three pages by NextToken and hide the token behind the cursor.
func TestSESDeliverabilityListsOverThreePages(t *testing.T) {
	for _, c := range []struct{ tool, args, key, item string }{
		{"aws.ses.import-job.list", `{"page_size":1`, "ImportJobs", `{"JobId":"j"}`},
		{"aws.ses.deliverability-test-report.list", `{"page_size":1`, "DeliverabilityTestReports", `{"ReportId":"r"}`},
		{"aws.ses.deliverability-campaign.list", `{"subscribed_domain":"example.com",` + sesStartEnd + `,"page_size":1`, "DomainDeliverabilityCampaigns", `{"CampaignId":"c"}`},
	} {
		t.Run(c.tool, func(t *testing.T) {
			f := sesFake(func(r sent) reply {
				seen := r.body + r.rawQuery
				switch {
				case strings.Contains(seen, "tok-2"):
					return ok(`{"` + c.key + `":[` + c.item + `]}`)
				case strings.Contains(seen, "tok-1"):
					return ok(`{"` + c.key + `":[` + c.item + `],"NextToken":"tok-2"}`)
				}
				return ok(`{"` + c.key + `":[` + c.item + `],"NextToken":"tok-1"}`)
			})
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
			cursor := ""
			for i := 0; i < 3; i++ {
				args := c.args
				if cursor != "" {
					args += `,"cursor":"` + cursor + `"`
				}
				text, err := e.invoke(c.tool, args+`}`)
				if err != nil {
					t.Fatalf("page %d: %v", i, err)
				}
				next, _ := decodeResult(t, text)["next_cursor"].(string)
				if (next != "") != (i < 2) || strings.Contains(text, "tok-") {
					t.Fatalf("page %d result = %s", i, text)
				}
				cursor = next
			}
			if n := len(restCalls(f)); n != 3 {
				t.Errorf("%d calls, want 3", n)
			}
		})
	}
}

// The service errors map to the classes of awserr, and a create is not repeated.
func TestSESDeliverabilityErrors(t *testing.T) {
	for _, c := range []struct {
		code   string
		status int
		class  provider.Class
	}{
		{"NotFoundException", 404, provider.ClassNotFound},
		{"BadRequestException", 400, provider.ClassProviderError},
		{"TooManyRequestsException", 429, provider.ClassRateLimited},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := sesFake(func(sent) reply {
				return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code}, body: `{"message":"secret text"}`}
			})
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
			_, err := e.invoke("aws.ses.import-job.get", `{"job_id":"j1"}`)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil ||
				failed.Original.Entries[0].Code != c.code || strings.Contains(err.Error(), "secret text") {
				t.Fatalf("read error = %v (%+v)", err, failed)
			}
			before := len(restCalls(f))
			_, err = e.invokeConfirmed("aws.ses.deliverability-test-report.create", testReportArgs, true)
			if err == nil || len(restCalls(f))-before != 1 {
				t.Errorf("create: err = %v, %d calls, want one failed call", err, len(restCalls(f))-before)
			}
		})
	}
}

// Create sends exactly the simple content, returns the report ID, and the dashboard option sends the switch.
func TestSESDeliverabilityWrites(t *testing.T) {
	f := sesFake(func(r sent) reply {
		if r.method == "PUT" {
			return ok(`{}`)
		}
		return ok(`{"ReportId":"r1","DeliverabilityTestStatus":"IN_PROGRESS"}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	text, err := e.invokeConfirmed("aws.ses.deliverability-test-report.create", testReportArgs, true)
	if err != nil || decodeResult(t, text)["report_id"] != "r1" {
		t.Fatalf("create = %s, %v", text, err)
	}
	if _, err := e.invokeConfirmed("aws.ses.deliverability-dashboard.put-option", `{"dashboard_enabled":true,"subscribed_domains":[{"domain":"example.com"}]}`, true); err != nil {
		t.Fatal(err)
	}
	calls := restCalls(f)
	if len(calls) != 2 || calls[0].path != "/v2/email/deliverability-dashboard/test" || calls[1].method != "PUT" ||
		calls[1].path != "/v2/email/deliverability-dashboard" || !strings.Contains(calls[1].body, `"DashboardEnabled":true`) {
		t.Fatalf("calls = %+v", calls)
	}
	var body struct {
		FromEmailAddress string
		Content          map[string]map[string]any
	}
	if err := json.Unmarshal([]byte(calls[0].body), &body); err != nil || body.FromEmailAddress != "example.com" || len(body.Content) != 1 || body.Content["Simple"] == nil {
		t.Errorf("create body = %s", calls[0].body)
	}
}

// The certificate tools send the association for a certificate of the bound account.
func TestSESCertificateAssociation(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	args := `{"email_identity":"example.com","from_address":"user@example.com","certificate_arn":"` + acmARN + `"}`
	if _, err := e.invokeConfirmed("aws.ses.email-identity-certificate.associate", args, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.invokeConfirmed("aws.ses.email-identity-certificate.disassociate", `{"email_identity":"example.com","from_address":"user@example.com"}`, true); err != nil {
		t.Fatal(err)
	}
	calls := restCalls(f)
	if len(calls) != 2 || calls[0].path != "/v2/email/identity/certificates" || calls[1].path != "/v2/email/identity/certificates/delete" ||
		!strings.Contains(calls[0].body, `"CertificateArn":"`+acmARN+`"`) {
		t.Fatalf("calls = %+v", calls)
	}
}

// Everything that must not reach AWS is refused before any request: a region outside the allowlist, a
// certificate of another account, region, service, or partition, a bad identity, and a write without confirmation.
func TestSESDeliverabilityRefusalsBeforeAnyIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	cert := func(arn string) string {
		return `{"email_identity":"example.com","from_address":"user@example.com","certificate_arn":"` + arn + `"}`
	}
	for _, c := range []struct{ tool, args string }{
		{"aws.ses.import-job.get", `{"job_id":"j1","region":"us-east-1"}`},
		{"aws.ses.recommendation.list", `{"region":"ap-south-1"}`},
		{"aws.ses.email-identity-certificate.associate", cert("arn:aws:acm:" + testRegion + ":210987654321:certificate/a")},
		{"aws.ses.email-identity-certificate.associate", cert("arn:aws:acm:us-east-1:" + testAccount + ":certificate/a")},
		{"aws.ses.email-identity-certificate.associate", cert("arn:aws:iam::" + testAccount + ":certificate/a")},
		{"aws.ses.email-identity-certificate.associate", cert("arn:aws:acm:" + testRegion + ":" + testAccount + ":key/a")},
		{"aws.ses.email-identity-certificate.associate", cert("arn:aws-cn:acm:" + testRegion + ":" + testAccount + ":certificate/a")},
		{"aws.ses.email-identity-certificate.associate", `{"email_identity":"a/b.example.com","certificate_arn":"` + acmARN + `"}`},
		{"aws.ses.email-identity-certificate.associate", `{"email_identity":"example.com","from_address":"../x","certificate_arn":"` + acmARN + `"}`},
		{"aws.ses.email-identity-certificate.disassociate", `{"email_identity":"nodots"}`},
		{"aws.ses.email-identity-certificate.list", `{"email_identity":"example.com?x=1"}`},
		{"aws.ses.domain-statistics-report.get", `{"domain":"a/b.example.com",` + sesStartEnd + `}`},
		{"aws.ses.deliverability-campaign.list", `{"subscribed_domain":"../x",` + sesStartEnd + `}`},
		{"aws.ses.deliverability-test-report.create", `{"from_email_address":"nodots","content":{"simple":{"subject":{"data":"a"},"body":{"text":{"data":"a"}}}}}`},
	} {
		confirmed := strings.Contains(c.tool, "associate") || strings.HasSuffix(c.tool, ".create")
		if _, err := e.invokeConfirmed(c.tool, c.args, confirmed); err == nil {
			t.Errorf("%s %s was accepted", c.tool, c.args)
		}
	}
	for _, c := range [][2]string{
		{"aws.ses.deliverability-dashboard.put-option", `{"dashboard_enabled":true}`},
		{"aws.ses.deliverability-test-report.create", testReportArgs},
		{"aws.ses.email-identity-certificate.associate", cert(acmARN)},
		{"aws.ses.email-identity-certificate.disassociate", `{"email_identity":"example.com","from_address":"user@example.com"}`},
	} {
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke(c[0], c[1]); !errorsAs(err, &unconfirmed) {
			t.Errorf("%s without confirmation: %v", c[0], err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

// No sesv2 tool starts or cancels an import or export job: only their reads exist.
func TestSESJobToolsOnlyRead(t *testing.T) {
	for _, s := range generatedSpecs {
		if s.service != "ses" {
			continue
		}
		for _, job := range []string{"/import-jobs", "/export-jobs", "/list-export-jobs"} {
			if strings.Contains(s.rest.URI, job) && s.descriptor.Risk.Effect != capability.EffectRead {
				t.Errorf("%s changes jobs: %s", s.descriptor.ID, s.rest.URI)
			}
		}
	}
}
