package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

const (
	sendTool     = "aws.ses.email.send"
	sendBulkTool = "aws.ses.email.send-bulk"
	sendSimple   = `{"from_email_address":"Sender <sender@example.com>","destination":{"to_addresses":["to@example.com"],` +
		`"cc_addresses":["cc@example.com"],"bcc_addresses":["bcc@example.com"]},"reply_to_addresses":["reply@example.com"],` +
		`"configuration_set_name":"default","content":{"simple":{"subject":{"data":"Report","charset":"UTF-8"},` +
		`"body":{"text":{"data":"The report is ready."}},"headers":[{"name":"X-Campaign","value":"q4"}]}}}`
	sendTemplateARN = "arn:aws:ses:" + testRegion + ":" + testAccount + ":template/welcome"
)

// sendEnv is a connection that offers execute, so the send tools are offered by effect.
func sendEnv(t *testing.T, f *fakeAWS, targets ...string) *env {
	t.Helper()
	return newConnEnv(t, f, baseValues(), config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectExecute}, Targets: targets})
}

func sendCount(f *fakeAWS) int { return len(restCalls(f)) }

// The description stands for the confirmation: it names what goes to AWS and to third parties, the costs, the
// sandbox, and that nothing is repeated; the risk is execute, non-idempotent, confirmed, and open.
func TestSESSendDescribesWhatTheConfirmationCovers(t *testing.T) {
	for _, s := range generatedSpecs {
		if s.descriptor.ID != sendTool && s.descriptor.ID != sendBulkTool {
			continue
		}
		d := s.descriptor
		if d.Risk != (capability.Risk{Effect: capability.EffectExecute, Idempotency: capability.IdempotencyNonIdempotent,
			Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "aws-ses-mail"}) || d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", d.ID, d.Risk)
		}
		for _, part := range []string{"the region", "the sender", "the recipients (To, Cc, and Bcc)", "the subject or the template",
			"the configuration set", "the content", "third parties", "0.10 USD per 1,000 mails", "sandbox", "verified recipients",
			"Never repeated automatically", "may have gone out", "At most 50 recipients", "Raw MIME content is not offered", "Needs confirmation"} {
			if !strings.Contains(d.Description, part) {
				t.Errorf("%s description lacks %q", d.ID, part)
			}
		}
		if s.delivery != "ses-mail" || s.descriptor.Risk.Idempotency != capability.IdempotencyNonIdempotent {
			t.Errorf("%s delivery = %q", d.ID, s.delivery)
		}
	}
}

// Raw MIME content and the multi-region endpoint are no arguments, and SendCustomVerificationEmail is no tool.
func TestSESSendOffersNoRawContent(t *testing.T) {
	found := 0
	for _, s := range generatedSpecs {
		if s.service != "ses" {
			continue
		}
		if strings.Contains(strings.ToLower(s.op), "custom verification") || strings.Contains(s.rest.URI, "custom-verification") {
			t.Errorf("%s sends a custom verification email", s.descriptor.ID)
		}
		if s.descriptor.ID != sendTool && s.descriptor.ID != sendBulkTool {
			continue
		}
		found++
		schema := string(s.descriptor.InputSchema)
		for _, banned := range []string{`"raw"`, `"endpoint_id"`} {
			if strings.Contains(schema, banned) {
				t.Errorf("%s input schema offers %s", s.descriptor.ID, banned)
			}
		}
	}
	if found != 2 {
		t.Errorf("%d send tools, want 2", found)
	}
	f := sesFake(func(sent) reply { return ok(`{"MessageId":"m-1"}`) })
	e := sendEnv(t, f)
	raw := `{"from_email_address":"sender@example.com","destination":{"to_addresses":["to@example.com"]},"content":{"raw":{"data":"AA=="}}}`
	if _, err := e.invokeConfirmed(sendTool, raw, true); err == nil || sendCount(f) != 0 {
		t.Errorf("raw content: %v after %d requests", err, sendCount(f))
	}
}

// Without confirm the core refuses a send before the handler: no account check, no request.
func TestSESSendNeedsConfirmation(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{"MessageId":"m-1"}`) })
	e := sendEnv(t, f)
	for _, c := range [][2]string{{sendTool, sendSimple}, {sendBulkTool, `{"default_content":{"template":{"template_name":"welcome"}},` +
		`"bulk_email_entries":[{"destination":{"to_addresses":["to@example.com"]}}]}`}} {
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke(c[0], c[1]); !errorsAs(err, &unconfirmed) {
			t.Errorf("%s without confirmation: %v", c[0], err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("an unconfirmed send made requests: %v", f.actions())
	}
}

// A simple mail and a template mail go out as exactly one request each and return the message ID.
func TestSESSendSimpleAndTemplate(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{"MessageId":"0100018a-message"}`) })
	e := sendEnv(t, f)
	text, err := e.invokeConfirmed(sendTool, sendSimple, true)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := decodeResult(t, text); got["message_id"] != "0100018a-message" || got["region"] != testRegion {
		t.Fatalf("result = %v", got)
	}
	calls := restCalls(f)
	if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != "/v2/email/outbound-emails" ||
		calls[0].host != "email."+testRegion+".amazonaws.com" {
		t.Fatalf("calls = %+v", calls)
	}
	for _, part := range []string{`"FromEmailAddress":"Sender <sender@example.com>"`, `"ToAddresses":["to@example.com"]`,
		`"CcAddresses":["cc@example.com"]`, `"BccAddresses":["bcc@example.com"]`, `"ReplyToAddresses":["reply@example.com"]`,
		`"ConfigurationSetName":"default"`, `"Subject":{"Data":"Report","Charset":"UTF-8"}`, `"Text":{"Data":"The report is ready."}`,
		`"Headers":[{"Name":"X-Campaign","Value":"q4"}]`} {
		if !strings.Contains(calls[0].body, part) {
			t.Errorf("body %s lacks %s", calls[0].body, part)
		}
	}

	f2 := sesFake(func(sent) reply { return ok(`{"MessageId":"m-2"}`) })
	e2 := sendEnv(t, f2)
	text, err = e2.invokeConfirmed(sendTool, `{"from_email_address":"sender@example.com","destination":{"to_addresses":["to@example.com"]},`+
		`"from_email_address_identity_arn":"`+sesIdentityARN+`","content":{"template":{"template_arn":"`+sendTemplateARN+`","template_data":"{\"name\":\"Alex\"}"}}}`, true)
	if err != nil {
		t.Fatalf("template send: %v", err)
	}
	if got := decodeResult(t, text); got["message_id"] != "m-2" {
		t.Errorf("template result = %v", got)
	}
	if calls := restCalls(f2); len(calls) != 1 || !strings.Contains(calls[0].body, `"TemplateArn":"`+sendTemplateARN+`"`) ||
		!strings.Contains(calls[0].body, `"FromEmailAddressIdentityArn":"`+sesIdentityARN+`"`) {
		t.Errorf("template calls = %+v", calls)
	}
}

// A bulk send returns the outcome of every destination, a success next to a rejection.
func TestSESSendBulkMixedResults(t *testing.T) {
	f := sesFake(func(sent) reply {
		return ok(`{"BulkEmailEntryResults":[{"Status":"SUCCESS","MessageId":"m-1"},{"Status":"MESSAGE_REJECTED","Error":"Email address is not verified."}]}`)
	})
	e := sendEnv(t, f)
	text, err := e.invokeConfirmed(sendBulkTool, `{"from_email_address":"sender@example.com","default_content":{"template":{"template_name":"welcome","template_data":"{}"}},`+
		`"bulk_email_entries":[{"destination":{"to_addresses":["first@example.com"]},"replacement_email_content":{"replacement_template":{"replacement_template_data":"{\"name\":\"Alex\"}"}}},`+
		`{"destination":{"to_addresses":["second@example.com"]}}]}`, true)
	if err != nil {
		t.Fatalf("bulk: %v", err)
	}
	results, _ := decodeResult(t, text)["bulk_email_entry_results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %v", results)
	}
	first, _ := results[0].(map[string]any)
	second, _ := results[1].(map[string]any)
	if first["status"] != "SUCCESS" || first["message_id"] != "m-1" || second["status"] != "MESSAGE_REJECTED" || second["error"] == nil {
		t.Errorf("results = %v", results)
	}
	calls := restCalls(f)
	if len(calls) != 1 || calls[0].path != "/v2/email/outbound-bulk-emails" ||
		!strings.Contains(calls[0].body, `"BulkEmailEntries":[{"Destination":{"ToAddresses":["first@example.com"]}`) {
		t.Errorf("calls = %+v", calls)
	}
}

func addressList(prefix string, n int) string {
	var out []string
	for i := range n {
		out = append(out, fmt.Sprintf(`"%s%d@example.com"`, prefix, i))
	}
	return "[" + strings.Join(out, ",") + "]"
}

// Too many recipients, an address of no valid form, and an identity or template ARN of another account,
// region, or kind are refused before any I/O.
func TestSESSendRefusalsBeforeAnyIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{"MessageId":"m-1"}`) })
	e := sendEnv(t, f)
	simple := `"content":{"simple":{"subject":{"data":"a"},"body":{"text":{"data":"a"}}}}`
	send := func(fields string) string { return `{` + fields + `,` + simple + `}` }
	to := `"from_email_address":"sender@example.com","destination":{"to_addresses":["to@example.com"]}`
	bulk := func(entries string) string {
		return `{"from_email_address":"sender@example.com","default_content":{"template":{"template_name":"t"}},"bulk_email_entries":` + entries + `}`
	}
	var entries []string
	for i := range 26 {
		entries = append(entries, fmt.Sprintf(`{"destination":{"to_addresses":["a%d@example.com"],"cc_addresses":["b%d@example.com"]}}`, i, i))
	}
	foreignIdentity := "arn:aws:ses:" + testRegion + ":210987654321:identity/example.com"
	for _, c := range []struct{ tool, args string }{
		{sendTool, send(`"from_email_address":"sender@example.com","destination":{"to_addresses":` + addressList("t", 20) +
			`,"cc_addresses":` + addressList("c", 20) + `,"bcc_addresses":` + addressList("b", 11) + `}`)},
		{sendTool, send(`"from_email_address":"sender@example.com","destination":{"to_addresses":` + addressList("t", 51) + `}`)},
		{sendBulkTool, bulk("[" + strings.Join(entries, ",") + "]")},
		{sendTool, send(`"from_email_address":"sender","destination":{"to_addresses":["to@example.com"]}`)},
		{sendTool, send(`"from_email_address":"sender@example.com","destination":{"to_addresses":["to@example.com\nBcc: x@example.com"]}`)},
		{sendTool, send(`"from_email_address":"Sender, Inc <sender@example.com>","destination":{"to_addresses":["to@example.com"]}`)},
		{sendTool, send(`"from_email_address":"sender@example.com","destination":{"cc_addresses":["a..b@example.com"]}`)},
		{sendTool, send(`"from_email_address":"sender@example.com","destination":{"bcc_addresses":["to@localhost"]}`)},
		{sendTool, send(to + `,"reply_to_addresses":["reply@"]`)},
		{sendTool, send(to + `,"feedback_forwarding_email_address":"Bounce <bounce@example.com>"`)},
		{sendTool, send(`"from_email_address":"jörg@example.com","destination":{"to_addresses":["to@example.com"]}`)},
		{sendTool, send(to + `,"from_email_address_identity_arn":"` + foreignIdentity + `"`)},
		{sendTool, send(to + `,"feedback_forwarding_email_address_identity_arn":"` + foreignIdentity + `"`)},
		{sendTool, send(to + `,"from_email_address_identity_arn":"arn:aws:ses:us-east-1:` + testAccount + `:identity/example.com"`)},
		{sendTool, send(to + `,"from_email_address_identity_arn":"` + sendTemplateARN + `"`)},
		{sendTool, `{` + to + `,"content":{"template":{"template_arn":"arn:aws:ses:` + testRegion + `:210987654321:template/welcome"}}}`},
		{sendTool, `{` + to + `,"content":{"template":{"template_arn":"arn:aws:ses:us-east-1:` + testAccount + `:template/welcome"}}}`},
		{sendTool, `{` + to + `,"content":{"template":{"template_arn":"` + sesIdentityARN + `"}}}`},
		{sendBulkTool, `{"from_email_address":"sender@example.com","default_content":{"template":{"template_arn":"arn:aws:ses:` + testRegion +
			`:210987654321:template/welcome"}},"bulk_email_entries":[{"destination":{"to_addresses":["to@example.com"]}}]}`},
		{sendBulkTool, bulk(`[{"destination":{"to_addresses":["not an address"]}}]`)},
	} {
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err == nil {
			t.Errorf("%s %s was accepted", c.tool, c.args)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
	// Exactly 50 recipients pass, across the destinations of a bulk call too.
	if _, err := e.invokeConfirmed(sendTool, send(`"from_email_address":"sender@example.com","destination":{"to_addresses":`+addressList("t", 25)+
		`,"cc_addresses":`+addressList("c", 20)+`,"bcc_addresses":`+addressList("b", 5)+`}`), true); err != nil {
		t.Errorf("50 recipients: %v", err)
	}
	f.answer = func(r sent, _ int) reply {
		if r.action == "GetCallerIdentity" {
			return ok(identityXML)
		}
		return ok(`{"BulkEmailEntryResults":[]}`)
	}
	if _, err := e.invokeConfirmed(sendBulkTool, bulk("["+strings.Join(entries[:25], ",")+"]"), true); err != nil {
		t.Errorf("50 recipients in 25 destinations: %v", err)
	}
}

// The message size is bounded by the 40 MB of SES before any I/O.
func TestSESSendRefusesAMessageAboveTheSESSize(t *testing.T) {
	b := binding{account: testAccount, regions: []string{testRegion}}
	args := map[string]any{"content": map[string]any{"simple": map[string]any{"body": map[string]any{
		"text": map[string]any{"data": strings.Repeat("a", sesMaxMessage)}}}}}
	if err := checkSESSend(b, testRegion, args); !isInvalidRequest(err) {
		t.Errorf("oversized message: %v", err)
	}
	args["content"] = map[string]any{"simple": map[string]any{"body": map[string]any{"text": map[string]any{"data": "a"}}}}
	if err := checkSESSend(b, testRegion, args); err != nil {
		t.Errorf("small message: %v", err)
	}
}

func TestSESAddressForms(t *testing.T) {
	for _, c := range []struct {
		address        string
		bare, withName bool
	}{
		{"sender@example.com", true, true},
		{"first.last+tag@example.com", true, true},
		{"o'brien@example.com", true, true},
		{"a!#$%&'*+/=?^_`{|}~-@example.com", true, true},
		{"user@example.xn--bcher-kva", true, true},
		{"Sender <sender@example.com>", false, true},
		{`"Sender, Inc." <sender@example.com>`, false, true},
		{"=?UTF-8?B?SsO2cmc=?= <sender@example.com>", false, true},
		{"Sender<sender@example.com>", false, true},
		{"", false, false},
		{"sender", false, false},
		{"@example.com", false, false},
		{"sender@", false, false},
		{".sender@example.com", false, false},
		{"sender.@example.com", false, false},
		{"a..b@example.com", false, false},
		{"sender@example", false, false},
		{"sender@exa_mple.com", false, false},
		{"sender@example.123", false, false},
		{"jörg@example.com", false, false},
		{"sender@bücher.example", false, false},
		{strings.Repeat("a", 65) + "@example.com", false, false},
		{"a@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63) + "." + strings.Repeat("e", 63) + ".com", false, false},
		{"to@example.com\r\nBcc: x@example.com", false, false},
		{"<sender@example.com>", false, false},
		{" Sender <sender@example.com>", false, false},
		{"Sender, Inc <sender@example.com>", false, false},
		{"Sender <sender@example.com", false, false},
		{"Sender <a@example.com> <b@example.com>", false, false},
		{`"Sen"der" <sender@example.com>`, false, false},
		{"Sender\t<sender@example.com>", false, false},
		{"Sender <Other <sender@example.com>>", false, false},
	} {
		if got := validMailbox(c.address); got != c.bare {
			t.Errorf("validMailbox(%q) = %t", c.address, got)
		}
		if got := validAddress(c.address); got != c.withName {
			t.Errorf("validAddress(%q) = %t", c.address, got)
		}
	}
}

// The errors of the send operations map to Qatlas classes, keep the code, say nothing of an unclear outcome,
// and are never repeated, a rate limit with Retry-After included.
func TestSESSendErrors(t *testing.T) {
	for _, c := range []struct {
		code    string
		status  int
		class   provider.Class
		message string
	}{
		{"MessageRejected", 400, provider.ClassProviderError, "rejected the message"},
		{"MailFromDomainNotVerifiedException", 400, provider.ClassProviderError, "MAIL FROM domain"},
		{"AccountSuspendedException", 400, provider.ClassPermission, "may no longer send"},
		{"SendingPausedException", 400, provider.ClassProviderError, "sending is paused"},
		{"TooManyRequestsException", 429, provider.ClassRateLimited, "rate-limited"},
		{"LimitExceededException", 400, provider.ClassProviderError, "limit or quota"},
		{"NotFoundException", 404, provider.ClassNotFound, "did not find"},
		{"BadRequestException", 400, provider.ClassProviderError, "invalid"},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := sesFake(func(sent) reply {
				return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code, "Retry-After": "1"},
					body: `{"message":"secret text"}`}
			})
			e := sendEnv(t, f)
			_, err := e.invokeConfirmed(sendTool, sendSimple, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || !strings.Contains(failed.Message, c.message) ||
				strings.Contains(failed.Message, "may have gone out") || failed.Original == nil ||
				len(failed.Original.Entries) != 1 || failed.Original.Entries[0].Code != c.code || strings.Contains(err.Error(), "secret text") {
				t.Fatalf("error = %v (%+v)", err, failed)
			}
			if n := sendCount(f); n != 1 {
				t.Errorf("sent %d times", n)
			}
		})
	}
}

// A timeout, a server-side failure, and an answer that cannot be read end the send after one request, and the
// failure says that the mail may have gone out.
func TestSESSendIsNotRepeatedAndSaysTheMailMayHaveGoneOut(t *testing.T) {
	for name, answer := range map[string]reply{
		"timeout":           {err: context.DeadlineExceeded},
		"server error":      {status: 500, header: map[string]string{"X-Amzn-Errortype": "InternalFailure"}, body: `{}`},
		"unavailable":       {status: 503, body: `{}`},
		"unreadable answer": ok(`{"MessageId":`),
	} {
		t.Run(name, func(t *testing.T) {
			f := sesFake(func(sent) reply { return answer })
			e := sendEnv(t, f)
			_, err := e.invokeConfirmed(sendTool, sendSimple, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || !strings.Contains(failed.Message, "may have gone out") ||
				!strings.Contains(failed.Message, "never repeated automatically") {
				t.Fatalf("error = %v", err)
			}
			if n := sendCount(f); n != 1 {
				t.Errorf("sent %d times", n)
			}
		})
	}
	// A bulk answer without its required results has been sent all the same.
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sendEnv(t, f)
	_, err := e.invokeConfirmed(sendBulkTool, `{"default_content":{"template":{"template_name":"t"}},"bulk_email_entries":[{"destination":{"to_addresses":["to@example.com"]}}]}`, true)
	if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "may have gone out") || sendCount(f) != 1 {
		t.Errorf("bulk without results: %v after %d requests", err, sendCount(f))
	}
}

// The sends of one connection and region start one second apart; another region has its own pace.
func TestSESSendIsPacedPerConnectionAndRegion(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{"MessageId":"m-1"}`) })
	e := sendEnv(t, f, "account/"+testAccount, "region/"+testRegion, "region/us-east-1")
	c := &testClock{now: testNow}
	slept := map[string][]time.Duration{}
	for _, region := range []string{testRegion, "us-east-1"} {
		deliveryLimiters.Replace(deliveryKey("ses-mail", "prod", region), ratelimit.New(deliveryInterval, c.Now,
			func(_ context.Context, d time.Duration) error { slept[region] = append(slept[region], d); return nil }))
	}
	withRegion := func(region string) string {
		var args map[string]any
		_ = json.Unmarshal([]byte(sendSimple), &args)
		args["region"] = region
		out, _ := json.Marshal(args)
		return string(out)
	}
	for _, region := range []string{testRegion, testRegion, "us-east-1"} {
		if _, err := e.invokeConfirmed(sendTool, withRegion(region), true); err != nil {
			t.Fatalf("send in %s: %v", region, err)
		}
	}
	if got := slept[testRegion]; len(got) != 1 || got[0] != time.Second {
		t.Errorf("waits in %s = %v, want one of 1s", testRegion, got)
	}
	if got := slept["us-east-1"]; len(got) != 0 {
		t.Errorf("waits in us-east-1 = %v, want none", got)
	}
	if sendCount(f) != 3 {
		t.Errorf("sent %d", sendCount(f))
	}
	if deliveryInterval != time.Second || deliveryKey("ses-mail", "prod", testRegion) == deliveryKey("ses-mail", "other", testRegion) {
		t.Error("the pace is not one second per connection and region")
	}
}

// A delivery belongs to a non-idempotent REST tool with an implementation; anything else is a registration error.
func TestSESDeliveryIsChecked(t *testing.T) {
	for _, edit := range []func(*toolSpec){
		func(s *toolSpec) { s.delivery = "nothing" },
		func(s *toolSpec) { s.descriptor.Risk.Idempotency = capability.IdempotencyIdempotent },
		func(s *toolSpec) { s.rest = nil },
	} {
		for _, s := range generatedSpecs {
			if s.descriptor.ID != sendTool {
				continue
			}
			edit(&s)
			if err := s.check(); err == nil {
				t.Errorf("a broken delivery passed the check: %+v", s.descriptor.Risk)
			}
		}
	}
}
