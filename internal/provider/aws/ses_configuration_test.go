package aws

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	sesArchiveARN = "arn:aws:ses:" + testRegion + ":" + testAccount + ":mailmanager-archive/a-0123456789abcdefghijklmn"
	setsPath      = "/v2/email/configuration-sets"
)

func sesSpec(t *testing.T, id string) toolSpec {
	t.Helper()
	for _, s := range generatedSpecs {
		if s.descriptor.ID == id {
			return s
		}
	}
	t.Fatalf("tool %s is not registered", id)
	return toolSpec{}
}

// Create sends every option as the structured member of the SES API, and nothing else.
func TestSESConfigurationSetCreateWithAllOptions(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	args := `{"configuration_set_name":"newsletter",` +
		`"tracking_options":{"custom_redirect_domain":"track.example.com","https_policy":"REQUIRE"},` +
		`"delivery_options":{"tls_policy":"REQUIRE","sending_pool_name":"pool-1","max_delivery_seconds":600},` +
		`"reputation_options":{"reputation_metrics_enabled":true},"sending_options":{"sending_enabled":true},` +
		`"tags":[{"key":"team","value":"mail"}],` +
		`"suppression_options":{"suppressed_reasons":["BOUNCE","COMPLAINT"]},` +
		`"vdm_options":{"dashboard_options":{"engagement_metrics":"ENABLED"},"guardian_options":{"optimized_shared_delivery":"DISABLED"}},` +
		`"archiving_options":{"archive_arn":"` + sesArchiveARN + `"}}`
	if _, err := e.invokeConfirmed("aws.ses.configuration-set.create", args, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	calls := restCalls(f)
	want := `{"ConfigurationSetName":"newsletter",` +
		`"TrackingOptions":{"CustomRedirectDomain":"track.example.com","HttpsPolicy":"REQUIRE"},` +
		`"DeliveryOptions":{"TlsPolicy":"REQUIRE","SendingPoolName":"pool-1","MaxDeliverySeconds":600},` +
		`"ReputationOptions":{"ReputationMetricsEnabled":true},"SendingOptions":{"SendingEnabled":true},` +
		`"Tags":[{"Key":"team","Value":"mail"}],` +
		`"SuppressionOptions":{"SuppressedReasons":["BOUNCE","COMPLAINT"]},` +
		`"VdmOptions":{"DashboardOptions":{"EngagementMetrics":"ENABLED"},"GuardianOptions":{"OptimizedSharedDelivery":"DISABLED"}},` +
		`"ArchivingOptions":{"ArchiveArn":"` + sesArchiveARN + `"}}`
	if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != setsPath ||
		calls[0].host != "email."+testRegion+".amazonaws.com" || !jsonEqual(calls[0].body, want) || f.count("GetCallerIdentity") != 1 {
		t.Fatalf("calls = %+v", calls)
	}
}

// An existing name, a limit, and an unknown set map to the classes of awserr with the code kept in Original,
// and a create is sent once.
func TestSESConfigurationSetErrors(t *testing.T) {
	for _, c := range []struct {
		code, tool, args string
		status           int
		class            provider.Class
	}{
		{"AlreadyExistsException", "aws.ses.configuration-set.create", `{"configuration_set_name":"newsletter"}`, 400, provider.ClassProviderError},
		{"NotFoundException", "aws.ses.configuration-set.get", `{"configuration_set_name":"gone"}`, 404, provider.ClassNotFound},
		{"NotFoundException", "aws.ses.configuration-set.delete", `{"configuration_set_name":"gone"}`, 404, provider.ClassNotFound},
		{"NotFoundException", "aws.ses.suppressed-destination.get", `{"email_address":"user@example.com"}`, 404, provider.ClassNotFound},
		{"NotFoundException", "aws.ses.suppressed-destination.delete", `{"email_address":"user@example.com"}`, 404, provider.ClassNotFound},
	} {
		t.Run(c.tool+"/"+c.code, func(t *testing.T) {
			f := sesFake(func(sent) reply {
				return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code}, body: `{"message":"secret text"}`}
			})
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
			_, err := e.invokeConfirmed(c.tool, c.args, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil || len(failed.Original.Entries) != 1 ||
				failed.Original.Entries[0].Code != c.code || strings.Contains(err.Error(), "secret text") {
				t.Fatalf("error = %v (%+v)", err, failed)
			}
			if n := len(restCalls(f)); n != 1 {
				t.Errorf("sent %d times", n)
			}
		})
	}
}

// Get and list read the set and its names; the option members come back by their model names.
func TestSESConfigurationSetGetAndList(t *testing.T) {
	f := sesFake(func(r sent) reply {
		if strings.HasSuffix(r.path, "/list-configuration-sets") {
			return ok(`{"ConfigurationSets":["a","b"]}`)
		}
		return ok(`{"ConfigurationSetName":"newsletter","SendingOptions":{"SendingEnabled":true},` +
			`"TrackingOptions":{"CustomRedirectDomain":"track.example.com","HttpsPolicy":"REQUIRE"},"Tags":[{"Key":"team","Value":"mail"}],` +
			`"ArchivingOptions":{"ArchiveArn":"` + sesArchiveARN + `"}}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	text, err := e.invoke("aws.ses.configuration-set.get", `{"configuration_set_name":"newsletter"}`)
	got := decodeResult(t, text)
	tracking, _ := got["tracking_options"].(map[string]any)
	if err != nil || got["configuration_set_name"] != "newsletter" || tracking["custom_redirect_domain"] != "track.example.com" || got["region"] != testRegion {
		t.Fatalf("get: %v, %s", err, text)
	}
	text, err = e.invoke("aws.ses.configuration-set.list", `{"page_size":2}`)
	if err != nil || !strings.Contains(text, `"a"`) || !strings.Contains(text, `"b"`) {
		t.Fatalf("list: %v, %s", err, text)
	}
	calls := restCalls(f)
	if len(calls) != 2 || calls[0].method != "GET" || calls[0].path != setsPath+"/newsletter" ||
		calls[1].method != "POST" || calls[1].path != "/v2/email/list-configuration-sets" || !jsonEqual(calls[1].body, `{"PageSize":2}`) {
		t.Errorf("calls = %+v", calls)
	}
}

// Every option has its own Put tool that sends its members to its own path and nothing else.
func TestSESConfigurationSetPutTools(t *testing.T) {
	for _, c := range []struct{ tool, args, path, body string }{
		{"aws.ses.configuration-set.put-delivery-options", `{"configuration_set_name":"s","tls_policy":"REQUIRE","sending_pool_name":"p"}`,
			"/delivery-options", `{"TlsPolicy":"REQUIRE","SendingPoolName":"p"}`},
		{"aws.ses.configuration-set.put-reputation-options", `{"configuration_set_name":"s","reputation_metrics_enabled":true}`,
			"/reputation-options", `{"ReputationMetricsEnabled":true}`},
		{"aws.ses.configuration-set.put-sending-options", `{"configuration_set_name":"s","sending_enabled":false}`,
			"/sending", `{"SendingEnabled":false}`},
		{"aws.ses.configuration-set.put-suppression-options", `{"configuration_set_name":"s","suppressed_reasons":["BOUNCE"]}`,
			"/suppression-options", `{"SuppressedReasons":["BOUNCE"]}`},
		{"aws.ses.configuration-set.put-tracking-options", `{"configuration_set_name":"s","custom_redirect_domain":"track.example.com","https_policy":"OPTIONAL"}`,
			"/tracking-options", `{"CustomRedirectDomain":"track.example.com","HttpsPolicy":"OPTIONAL"}`},
		{"aws.ses.configuration-set.put-vdm-options", `{"configuration_set_name":"s","vdm_options":{"dashboard_options":{"engagement_metrics":"ENABLED"}}}`,
			"/vdm-options", `{"VdmOptions":{"DashboardOptions":{"EngagementMetrics":"ENABLED"}}}`},
		{"aws.ses.configuration-set.put-archiving-options", `{"configuration_set_name":"s","archive_arn":"` + sesArchiveARN + `"}`,
			"/archiving-options", `{"ArchiveArn":"` + sesArchiveARN + `"}`},
	} {
		f := sesFake(func(sent) reply { return ok(`{}`) })
		e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err != nil {
			t.Errorf("%s: %v", c.tool, err)
			continue
		}
		calls := restCalls(f)
		if len(calls) != 1 || calls[0].method != "PUT" || calls[0].path != setsPath+"/s"+c.path || !jsonEqual(calls[0].body, c.body) {
			t.Errorf("%s calls = %+v, want PUT %s %s", c.tool, calls, c.path, c.body)
		}
	}
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	if _, err := e.invokeConfirmed("aws.ses.configuration-set.delete", `{"configuration_set_name":"s"}`, true); err != nil {
		t.Fatal(err)
	}
	if calls := restCalls(f); len(calls) != 1 || calls[0].method != "DELETE" || calls[0].path != setsPath+"/s" {
		t.Errorf("delete calls = %+v", calls)
	}
}

// The archive ARN is judged before any request, top level and nested: only a mail archive of the bound
// account and an allowed region passes. The tracking domain must have the form of a domain name.
func TestSESArchiveARNAndDomainRefusals(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	bad := []string{
		"arn:aws:ses:" + testRegion + ":210987654321:mailmanager-archive/a-0123456789abcdefghijklmn",
		"arn:aws:ses:us-east-1:" + testAccount + ":mailmanager-archive/a-0123456789abcdefghijklmn",
		"arn:aws:ses:" + testRegion + ":" + testAccount + ":identity/example.com",
		"arn:aws:ses:" + testRegion + ":" + testAccount + ":mailmanager-archive/x",
		"arn:aws:sns:" + testRegion + ":" + testAccount + ":mailmanager-archive/a-0123456789abcdefghijklmn",
		"arn:aws-cn:ses:" + testRegion + ":" + testAccount + ":mailmanager-archive/a-0123456789abcdefghijklmn",
		"mailmanager-archive/a-0123456789abcdefghijklmn", "",
	}
	for _, arn := range bad {
		for _, c := range []struct{ tool, args string }{
			{"aws.ses.configuration-set.put-archiving-options", `{"configuration_set_name":"s","archive_arn":"` + arn + `"}`},
			{"aws.ses.configuration-set.create", `{"configuration_set_name":"s","archiving_options":{"archive_arn":"` + arn + `"}}`},
		} {
			if _, err := e.invokeConfirmed(c.tool, c.args, true); !isInvalidRequest(err) {
				t.Errorf("%s with archive %q: %v", c.tool, arn, err)
			}
		}
	}
	for _, domain := range []string{"nodots", "a b.example.com", "track.example.com/x", "-a.example.com", "a..example.com", "12.34", "x.example.com\n", ""} {
		for _, c := range []struct{ tool, args string }{
			{"aws.ses.configuration-set.put-tracking-options", `{"configuration_set_name":"s","custom_redirect_domain":"` + domain + `"}`},
			{"aws.ses.configuration-set.create", `{"configuration_set_name":"s","tracking_options":{"custom_redirect_domain":"` + domain + `"}}`},
		} {
			if _, err := e.invokeConfirmed(c.tool, c.args, true); !isInvalidRequest(err) {
				t.Errorf("%s with domain %q: %v", c.tool, domain, err)
			}
		}
	}
	for _, name := range []string{"a b", "a/b", "../x", "a?b=1", strings.Repeat("a", 65), "x\n"} {
		if _, err := e.invokeConfirmed("aws.ses.configuration-set.delete", `{"configuration_set_name":"`+strings.ReplaceAll(name, "\n", `\n`)+`"}`, true); !isInvalidRequest(err) {
			t.Errorf("delete of %q: %v", name, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

// Every write needs confirmation and a call without it sends nothing; a region outside the allowlist is
// refused as well.
func TestSESConfigurationWritesNeedConfirmation(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	writes := map[string]string{
		"configuration-set.create":                  `{"configuration_set_name":"s"}`,
		"configuration-set.delete":                  `{"configuration_set_name":"s"}`,
		"configuration-set.put-delivery-options":    `{"configuration_set_name":"s"}`,
		"configuration-set.put-reputation-options":  `{"configuration_set_name":"s"}`,
		"configuration-set.put-sending-options":     `{"configuration_set_name":"s"}`,
		"configuration-set.put-suppression-options": `{"configuration_set_name":"s"}`,
		"configuration-set.put-tracking-options":    `{"configuration_set_name":"s"}`,
		"configuration-set.put-vdm-options":         `{"configuration_set_name":"s"}`,
		"configuration-set.put-archiving-options":   `{"configuration_set_name":"s"}`,
		"suppressed-destination.put":                `{"email_address":"user@example.com","reason":"BOUNCE"}`,
		"suppressed-destination.delete":             `{"email_address":"user@example.com"}`,
	}
	for id, args := range writes {
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke("aws.ses."+id, args); !errorsAs(err, &unconfirmed) {
			t.Errorf("%s without confirmation: %v", id, err)
		}
		withRegion := strings.TrimSuffix(args, "}") + `,"region":"us-east-1"}`
		if _, err := e.invokeConfirmed("aws.ses."+id, withRegion, true); err == nil {
			t.Errorf("%s accepted a region outside the allowlist", id)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

// The suppression tools carry a class of data of their own, and the descriptions say what the writes do.
func TestSESSuppressionRiskAndDescriptions(t *testing.T) {
	for _, id := range []string{"list", "get", "put", "delete"} {
		d := sesSpec(t, "aws.ses.suppressed-destination."+id).descriptor
		if d.Risk.DataSensitivity != "aws-ses-recipient-data" || !strings.Contains(d.Description, "personal data") {
			t.Errorf("%s: sensitivity %q, description %q", d.ID, d.Risk.DataSensitivity, d.Description)
		}
	}
	if d := sesSpec(t, "aws.ses.suppressed-destination.delete").descriptor; !strings.Contains(d.Description, "receives mail again") {
		t.Errorf("delete description = %q", d.Description)
	}
	if d := sesSpec(t, "aws.ses.suppressed-destination.put").descriptor; !strings.Contains(d.Description, "no longer sends mail") {
		t.Errorf("put description = %q", d.Description)
	}
	if d := sesSpec(t, "aws.ses.configuration-set.create").descriptor; d.Risk.DataSensitivity == "aws-ses-recipient-data" {
		t.Errorf("a configuration set tool carries recipient data: %q", d.Risk.DataSensitivity)
	}
	// The tenant of the SES API is not offered.
	for _, id := range []string{"list", "get", "put", "delete"} {
		if schema := string(sesSpec(t, "aws.ses.suppressed-destination."+id).descriptor.InputSchema); strings.Contains(schema, "tenant") {
			t.Errorf("suppressed-destination.%s offers a tenant", id)
		}
	}
}

// Put, get, and delete of an address send the address in the body, the path, and the path, and the answer of
// get is data of the provider.
func TestSESSuppressionPutGetDelete(t *testing.T) {
	f := sesFake(func(r sent) reply {
		if r.method == "GET" {
			return ok(`{"SuppressedDestination":{"EmailAddress":"user@example.com","Reason":"BOUNCE","LastUpdateTime":1767225600,` +
				`"Attributes":{"MessageId":"m-1","FeedbackId":"f-1"}}}`)
		}
		return ok(`{}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	if _, err := e.invokeConfirmed("aws.ses.suppressed-destination.put", `{"email_address":"user@example.com","reason":"COMPLAINT"}`, true); err != nil {
		t.Fatalf("put: %v", err)
	}
	text, err := e.invoke("aws.ses.suppressed-destination.get", `{"email_address":"user@example.com"}`)
	got := decodeResult(t, text)
	entry, _ := got["suppressed_destination"].(map[string]any)
	if err != nil || entry["email_address"] != "user@example.com" || entry["reason"] != "BOUNCE" {
		t.Fatalf("get: %v, %s", err, text)
	}
	if _, err := e.invokeConfirmed("aws.ses.suppressed-destination.delete", `{"email_address":"user@example.com"}`, true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	calls := restCalls(f)
	if len(calls) != 3 ||
		calls[0].method != "PUT" || calls[0].path != "/v2/email/suppression/addresses" || !jsonEqual(calls[0].body, `{"EmailAddress":"user@example.com","Reason":"COMPLAINT"}`) ||
		calls[1].method != "GET" || calls[1].path != "/v2/email/suppression/addresses/user@example.com" && calls[1].path != "/v2/email/suppression/addresses/user%40example.com" ||
		calls[2].method != "DELETE" || !strings.HasPrefix(calls[2].path, "/v2/email/suppression/addresses/user") {
		t.Errorf("calls = %+v", calls)
	}
	// A reason other than the two of the API is refused before any request.
	before := len(f.requests)
	if _, err := e.invokeConfirmed("aws.ses.suppressed-destination.put", `{"email_address":"user@example.com","reason":"OTHER"}`, true); err == nil || len(f.requests) != before {
		t.Errorf("reason OTHER: %v", err)
	}
}

// The list pages over three pages, filters by reason and period, and bounds the page size.
func TestSESSuppressionListPagesAndFilters(t *testing.T) {
	f := sesFake(func(r sent) reply {
		q, _ := url.ParseQuery(r.rawQuery)
		entry := func(addr, next string) reply {
			body := `{"SuppressedDestinationSummaries":[{"EmailAddress":"` + addr + `","Reason":"BOUNCE","LastUpdateTime":1767225600}]`
			if next != "" {
				body += `,"NextToken":"` + next + `"`
			}
			return ok(body + `}`)
		}
		switch q.Get("NextToken") {
		case "":
			return entry("a\\u0040example.com", "tok-1")
		case "tok-1":
			return entry("b\\u0040example.com", "tok-2")
		}
		return entry("c\\u0040example.com", "")
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	var found []string
	cursor := ""
	for i := 0; i < 3; i++ {
		args := `{"page_size":1,"reasons":["BOUNCE","COMPLAINT"],"start_date":"2026-01-01T00:00:00Z","end_date":"2026-01-31T23:59:59Z"`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		text, err := e.invoke("aws.ses.suppressed-destination.list", args+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		got := decodeResult(t, text)
		items, _ := got["suppressed_destination_summaries"].([]any)
		found = append(found, items[0].(map[string]any)["email_address"].(string))
		next, _ := got["next_cursor"].(string)
		if (next != "") != (i < 2) || strings.Contains(text, "tok-") {
			t.Fatalf("page %d result = %s", i, text)
		}
		cursor = next
	}
	if !slices.Equal(found, []string{"a@example.com", "b@example.com", "c@example.com"}) {
		t.Errorf("addresses = %v", found)
	}
	calls := restCalls(f)
	if len(calls) != 3 {
		t.Fatalf("calls = %+v", calls)
	}
	q, _ := url.ParseQuery(calls[1].rawQuery)
	if calls[1].method != "GET" || calls[1].path != "/v2/email/suppression/addresses" || !slices.Equal(q["Reason"], []string{"BOUNCE", "COMPLAINT"}) ||
		q.Get("StartDate") != "2026-01-01T00:00:00Z" || q.Get("EndDate") != "2026-01-31T23:59:59Z" || q.Get("PageSize") != "1" || q.Get("NextToken") != "tok-1" {
		t.Errorf("second page = %+v (%v)", calls[1], q)
	}
	before := len(f.requests)
	for _, args := range []string{`{"page_size":1001}`, `{"reasons":["OTHER"]}`, `{"start_date":"yesterday"}`} {
		if _, err := e.invoke("aws.ses.suppressed-destination.list", args); err == nil {
			t.Errorf("list %s was accepted", args)
		}
	}
	if len(f.requests) != before {
		t.Errorf("a refused list sent %v", f.actions())
	}
}

// A list is capped: a page within the cap passes, and an answer far beyond it is refused as invalid instead of
// reaching the caller.
func TestSESSuppressionListIsCapped(t *testing.T) {
	answer := func(n int) string {
		entry := `{"EmailAddress":"u\u0040example.com","Reason":"BOUNCE","LastUpdateTime":1767225600}`
		return `{"SuppressedDestinationSummaries":[` + strings.TrimSuffix(strings.Repeat(entry+",", n), ",") + `]}`
	}
	for n, wantErr := range map[int]bool{100: false, 5000: true} {
		f := sesFake(func(sent) reply { return ok(answer(n)) })
		e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
		text, err := e.invoke("aws.ses.suppressed-destination.list", `{}`)
		if (err != nil) != wantErr || (err != nil && strings.Contains(text, "example.com")) {
			t.Errorf("%d entries: %v", n, err)
		}
	}
}

// The event destinations are read only: the read tool exists, and no tool of the group writes an event
// destination, whatever its name or path.
func TestSESEventDestinationsAreReadOnly(t *testing.T) {
	f := sesFake(func(sent) reply {
		return ok(`{"EventDestinations":[{"Name":"d","Enabled":true,"MatchingEventTypes":["BOUNCE"],"SnsDestination":{"TopicArn":"arn:aws:sns:eu-central-1:123456789012:t"}}]}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	text, err := e.invoke("aws.ses.configuration-set-event-destination.get", `{"configuration_set_name":"s"}`)
	if err != nil || !strings.Contains(text, "sns_destination") {
		t.Fatalf("get: %v, %s", err, text)
	}
	if calls := restCalls(f); len(calls) != 1 || calls[0].method != "GET" || calls[0].path != setsPath+"/s/event-destinations" {
		t.Errorf("calls = %+v", calls)
	}
	for _, s := range generatedSpecs {
		if s.service != "ses" {
			continue
		}
		write := s.descriptor.Risk.Effect != "read"
		if strings.Contains(s.descriptor.ID, "event-destination") && write ||
			strings.Contains(s.rest.URI, "event-destinations") && (write || s.rest.Method != "GET") ||
			strings.Contains(s.op, "EventDestination") && s.op != "GetConfigurationSetEventDestinations" {
			t.Errorf("%s (%s) touches event destinations", s.descriptor.ID, s.op)
		}
	}
	if len(sesSpec(t, "aws.ses.configuration-set-event-destination.get").descriptor.Description) == 0 ||
		!strings.Contains(sesSpec(t, "aws.ses.configuration-set-event-destination.get").descriptor.Description, "not offered") {
		t.Error("the event destination read does not say that writing is not offered")
	}
}

// The tag tools take the ARN of a configuration set of the bound account as well as an identity ARN.
func TestSESTagToolsTakeConfigurationSets(t *testing.T) {
	f := sesFake(func(r sent) reply {
		if r.method == "GET" {
			return ok(`{"Tags":[{"Key":"team","Value":"mail"}]}`)
		}
		return ok(`{}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	if text, err := e.invoke("aws.ses.email-identity.list-tags", `{"resource_arn":"`+sesSetARN+`"}`); err != nil || !strings.Contains(text, "team") {
		t.Errorf("list-tags: %v %s", err, text)
	}
	if _, err := e.invokeConfirmed("aws.ses.email-identity.tag", `{"resource_arn":"`+sesSetARN+`","tags":[{"key":"team","value":"mail"}]}`, true); err != nil {
		t.Errorf("tag: %v", err)
	}
	if _, err := e.invokeConfirmed("aws.ses.email-identity.untag", `{"resource_arn":"`+sesSetARN+`","tag_keys":["team"]}`, true); err != nil {
		t.Errorf("untag: %v", err)
	}
	calls := restCalls(f)
	if len(calls) != 3 {
		t.Fatalf("calls = %+v", calls)
	}
	if q, _ := url.ParseQuery(calls[0].rawQuery); q.Get("ResourceArn") != sesSetARN || !jsonEqual(calls[1].body, `{"ResourceArn":"`+sesSetARN+`","Tags":[{"Key":"team","Value":"mail"}]}`) {
		t.Errorf("calls = %+v", calls)
	}
	for _, id := range []string{"tag", "untag", "list-tags"} {
		d := sesSpec(t, "aws.ses.email-identity."+id).descriptor
		if !strings.Contains(d.Title+d.Description, "configuration set") {
			t.Errorf("%s does not name configuration sets: %q", d.ID, d.Description)
		}
	}
}
