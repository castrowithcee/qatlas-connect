package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	sandboxAccountJSON = `{"ProductionAccessEnabled":false,"SendingEnabled":true,"EnforcementStatus":"HEALTHY",` +
		`"SendQuota":{"Max24HourSend":200,"MaxSendRate":1,"SentLast24Hours":3},"DedicatedIpAutoWarmupEnabled":false,` +
		`"Details":{"MailType":"TRANSACTIONAL","WebsiteURL":"https://example.com","ContactLanguage":"EN",` +
		`"AdditionalContactEmailAddresses":["ops@example.com"],"ReviewDetails":{"Status":"PENDING","CaseId":"1234"}},` +
		`"SuppressionAttributes":{"SuppressedReasons":["BOUNCE"]},"VdmAttributes":{"VdmEnabled":"DISABLED"}}`
	productionAccountJSON = `{"ProductionAccessEnabled":true,"SendingEnabled":true,"EnforcementStatus":"HEALTHY",` +
		`"SendQuota":{"Max24HourSend":50000,"MaxSendRate":14,"SentLast24Hours":120},"DedicatedIpAutoWarmupEnabled":true,` +
		`"VdmAttributes":{"VdmEnabled":"ENABLED","DashboardAttributes":{"EngagementMetrics":"ENABLED"}}}`
)

var sesAccountTools = map[string]struct {
	effect capability.Effect
	idem   capability.Idempotency
}{
	"aws.ses.account.get":                                {capability.EffectRead, capability.IdempotencySafe},
	"aws.ses.account.put-details":                        {capability.EffectUpdate, capability.IdempotencyIdempotent},
	"aws.ses.account.put-sending-attributes":             {capability.EffectUpdate, capability.IdempotencyIdempotent},
	"aws.ses.account.put-suppression-attributes":         {capability.EffectUpdate, capability.IdempotencyIdempotent},
	"aws.ses.account.put-dedicated-ip-warmup-attributes": {capability.EffectUpdate, capability.IdempotencyIdempotent},
	"aws.ses.account.put-vdm-attributes":                 {capability.EffectUpdate, capability.IdempotencyIdempotent},
}

// The six account tools exist, get is a safe read, every Put is an idempotent update that needs confirmation,
// and the descriptions carry the sandbox hint, the review hint, and the cost.
func TestSESAccountToolsRiskAndDescriptions(t *testing.T) {
	seen := map[string]capability.Descriptor{}
	for _, s := range generatedSpecs {
		if strings.HasPrefix(s.descriptor.ID, "aws.ses.account.") {
			seen[s.descriptor.ID] = s.descriptor
		}
	}
	if len(seen) != len(sesAccountTools) {
		t.Fatalf("%d account tools, want %d", len(seen), len(sesAccountTools))
	}
	for id, w := range sesAccountTools {
		d, ok := seen[id]
		confirmation := capability.ConfirmationRequired
		if w.effect == capability.EffectRead {
			confirmation = capability.ConfirmationNone
		}
		if !ok || d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idem || d.Risk.Confirmation != confirmation ||
			!d.Risk.OpenWorld || d.Risk.DataSensitivity != "aws-ses-account" || d.RequiresToolAllowList || d.Group != "ses" {
			t.Errorf("%s descriptor = %+v", id, d)
		}
	}
	has := func(id string, parts ...string) {
		t.Helper()
		for _, p := range parts {
			if !strings.Contains(seen[id].Description, p) {
				t.Errorf("%s description lacks %q: %s", id, p, seen[id].Description)
			}
		}
	}
	has("aws.ses.account.get", "production_access_enabled", "sandbox", "only to verified")
	has("aws.ses.account.put-details", "region", "mail_type", "website_url", "additional_contact_email_addresses",
		"only to AWS", "not recipients", "AWS reviews", "can take a while", "sends no mail", "Needs confirmation")
	has("aws.ses.account.put-vdm-attributes", "costs extra", "price is not retrieved")
	if got := seen["aws.ses.account.put-details"].Description; strings.Contains(got, "use_case") {
		t.Errorf("put-details offers the deprecated use case text: %s", got)
	}
}

// Get answers for a sandbox region and a production region as the service says, and sends one GET.
func TestSESAccountGetSandboxAndProduction(t *testing.T) {
	for _, c := range []struct {
		body       string
		production bool
	}{{sandboxAccountJSON, false}, {productionAccountJSON, true}} {
		f := sesFake(func(sent) reply { return ok(c.body) })
		e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
		text, err := e.invoke("aws.ses.account.get", `{}`)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		got := decodeResult(t, text)
		quota, _ := got["send_quota"].(map[string]any)
		if got["region"] != testRegion || (got["production_access_enabled"] == true) != c.production || got["sending_enabled"] != true ||
			got["enforcement_status"] != "HEALTHY" || quota["max_send_rate"] == nil || quota["max24_hour_send"] == nil {
			t.Errorf("production=%v: result %v", c.production, got)
		}
		details, _ := got["details"].(map[string]any)
		if !c.production {
			review, _ := details["review_details"].(map[string]any)
			contacts, _ := details["additional_contact_email_addresses"].([]any)
			if details["mail_type"] != "TRANSACTIONAL" || review["status"] != "PENDING" || len(contacts) != 1 {
				t.Errorf("details = %v", details)
			}
		}
		calls := restCalls(f)
		if len(calls) != 1 || calls[0].method != "GET" || calls[0].path != "/v2/email/account" ||
			calls[0].host != "email."+testRegion+".amazonaws.com" {
			t.Errorf("calls = %+v", calls)
		}
	}
}

// Put-details with every offered field sends exactly those members to the regional endpoint.
func TestSESAccountPutDetailsAllFields(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	text, err := e.invokeConfirmed("aws.ses.account.put-details",
		`{"mail_type":"MARKETING","website_url":"https://example.com/signup","contact_language":"JA",`+
			`"additional_contact_email_addresses":["a@example.com","b@example.com"],"production_access_enabled":true}`, true)
	if err != nil {
		t.Fatalf("put-details: %v", err)
	}
	if got := decodeResult(t, text); got["region"] != testRegion {
		t.Errorf("result = %v", got)
	}
	calls := restCalls(f)
	want := `{"MailType":"MARKETING","WebsiteURL":"https://example.com/signup","ContactLanguage":"JA",` +
		`"AdditionalContactEmailAddresses":["a@example.com","b@example.com"],"ProductionAccessEnabled":true}`
	if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != "/v2/email/account/details" ||
		calls[0].host != "email."+testRegion+".amazonaws.com" || !jsonEqual(calls[0].body, want) || f.count("GetCallerIdentity") != 1 {
		t.Errorf("calls = %+v", calls)
	}
}

// The other Put tools send their member and nothing else.
func TestSESAccountPutTools(t *testing.T) {
	for _, c := range []struct{ tool, args, path, body string }{
		{"aws.ses.account.put-sending-attributes", `{"sending_enabled":false}`, "/v2/email/account/sending", `{"SendingEnabled":false}`},
		{"aws.ses.account.put-suppression-attributes", `{"suppressed_reasons":["BOUNCE","COMPLAINT"]}`, "/v2/email/account/suppression", `{"SuppressedReasons":["BOUNCE","COMPLAINT"]}`},
		{"aws.ses.account.put-dedicated-ip-warmup-attributes", `{"auto_warmup_enabled":true}`, "/v2/email/account/dedicated-ips/warmup", `{"AutoWarmupEnabled":true}`},
		{"aws.ses.account.put-vdm-attributes", `{"vdm_attributes":{"vdm_enabled":"ENABLED","dashboard_attributes":{"engagement_metrics":"ENABLED"},"guardian_attributes":{"optimized_shared_delivery":"DISABLED"}}}`,
			"/v2/email/account/vdm", `{"VdmAttributes":{"VdmEnabled":"ENABLED","DashboardAttributes":{"EngagementMetrics":"ENABLED"},"GuardianAttributes":{"OptimizedSharedDelivery":"DISABLED"}}}`},
	} {
		f := sesFake(func(sent) reply { return ok(`{}`) })
		e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err != nil {
			t.Errorf("%s: %v", c.tool, err)
			continue
		}
		calls := restCalls(f)
		if len(calls) != 1 || calls[0].method != "PUT" || calls[0].path != c.path || !jsonEqual(calls[0].body, c.body) {
			t.Errorf("%s calls = %+v, want PUT %s %s", c.tool, calls, c.path, c.body)
		}
	}
}

// Every write is refused without confirmation, and the contract of put-details holds: no request, a
// confirmation error, and the text a person confirms is the description of the tool.
func TestSESAccountWritesNeedConfirmation(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	for _, c := range [][2]string{
		{"aws.ses.account.put-details", `{"mail_type":"TRANSACTIONAL","website_url":"https://example.com"}`},
		{"aws.ses.account.put-sending-attributes", `{"sending_enabled":true}`},
		{"aws.ses.account.put-suppression-attributes", `{"suppressed_reasons":["BOUNCE"]}`},
		{"aws.ses.account.put-dedicated-ip-warmup-attributes", `{"auto_warmup_enabled":true}`},
		{"aws.ses.account.put-vdm-attributes", `{"vdm_attributes":{"vdm_enabled":"ENABLED"}}`},
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

// The region must be one of the allowlist, a required member must be present, and a value outside the model's
// enum is refused: all before any I/O.
func TestSESAccountRefusalsBeforeAnyIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	for _, c := range []struct{ tool, args string }{
		{"aws.ses.account.get", `{"region":"us-east-1"}`},
		{"aws.ses.account.put-details", `{"region":"us-east-1","mail_type":"TRANSACTIONAL","website_url":"https://example.com"}`},
		{"aws.ses.account.put-details", `{"website_url":"https://example.com"}`},
		{"aws.ses.account.put-details", `{"mail_type":"TRANSACTIONAL"}`},
		{"aws.ses.account.put-details", `{"mail_type":"BULK","website_url":"https://example.com"}`},
		{"aws.ses.account.put-details", `{"mail_type":"TRANSACTIONAL","website_url":"https://example.com","use_case_description":"x"}`},
		{"aws.ses.account.put-details", `{"mail_type":"TRANSACTIONAL","website_url":"https://example.com","additional_contact_email_addresses":["a@example.com","b@example.com","c@example.com","d@example.com","e@example.com"]}`},
		{"aws.ses.account.put-vdm-attributes", `{"vdm_attributes":{}}`},
	} {
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err == nil {
			t.Errorf("%s %s was accepted", c.tool, c.args)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

// The errors of the account operations map to the classes of awserr and keep the code in Original, without
// the provider's text.
func TestSESAccountErrors(t *testing.T) {
	for _, c := range []struct {
		code    string
		status  int
		class   provider.Class
		message string
	}{
		{"ConflictException", 409, provider.ClassProviderError, "conflict"},
		{"BadRequestException", 400, provider.ClassProviderError, "invalid"},
		{"TooManyRequestsException", 429, provider.ClassRateLimited, "rate-limited"},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := sesFake(func(sent) reply {
				return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code}, body: `{"message":"secret text"}`}
			})
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
			_, err := e.invokeConfirmed("aws.ses.account.put-details", `{"mail_type":"TRANSACTIONAL","website_url":"https://example.com"}`, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || !strings.Contains(failed.Message, c.message) ||
				failed.Original == nil || len(failed.Original.Entries) == 0 || failed.Original.Entries[0].Code != c.code ||
				strings.Contains(err.Error(), "secret text") {
				t.Fatalf("error = %v (%+v)", err, failed)
			}
		})
	}
}
