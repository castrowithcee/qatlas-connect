package aws

import (
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var (
	tokenA = strings.Repeat("a", 32)
	tokenB = strings.Repeat("b", 32)
	tokenC = strings.Repeat("c", 32)
)

const (
	sesIdentityARN = "arn:aws:ses:" + testRegion + ":" + testAccount + ":identity/example.com"
	sesSetARN      = "arn:aws:ses:" + testRegion + ":" + testAccount + ":configuration-set/newsletter"
	dkimTokensJSON = `["` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `","` + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + `","` + "cccccccccccccccccccccccccccccccc" + `"]`
)

// sesEnv is a connection that offers every effect, so the writes are offered by effect and not by name.
func sesEnv(t *testing.T, f *fakeAWS, targets ...string) *env {
	t.Helper()
	conn := config.Connection{Effects: []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete},
		Targets: targets}
	return newConnEnv(t, f, baseValues(), conn)
}

func sesFake(answer func(sent) reply) *fakeAWS {
	return direct(func(r sent, _ int) reply { return answer(r) })
}

func restCalls(f *fakeAWS) []sent { return restRequests(f) }

// The identity tools and their risk: reads are safe and open, writes need confirmation, Put* repeat safely,
// create and delete do not, and only the identity policy writes are listed-only.
func TestSESIdentityToolsRisk(t *testing.T) {
	want := map[string]struct {
		effect capability.Effect
		idem   capability.Idempotency
	}{
		"aws.ses.email-identity.list":                             {capability.EffectRead, capability.IdempotencySafe},
		"aws.ses.email-identity.get":                              {capability.EffectRead, capability.IdempotencySafe},
		"aws.ses.email-identity.list-tags":                        {capability.EffectRead, capability.IdempotencySafe},
		"aws.ses.email-identity-policy.list":                      {capability.EffectRead, capability.IdempotencySafe},
		"aws.ses.email-identity-policy.create":                    {capability.EffectCreate, capability.IdempotencyNonIdempotent},
		"aws.ses.email-identity-policy.update":                    {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.email-identity-policy.delete":                    {capability.EffectDelete, capability.IdempotencyNonIdempotent},
		"aws.ses.email-identity.create":                           {capability.EffectCreate, capability.IdempotencyNonIdempotent},
		"aws.ses.email-identity.delete":                           {capability.EffectDelete, capability.IdempotencyNonIdempotent},
		"aws.ses.email-identity.put-dkim-attributes":              {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.email-identity.put-dkim-signing-attributes":      {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.email-identity.put-mail-from-attributes":         {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.email-identity.put-feedback-attributes":          {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.email-identity.put-configuration-set-attributes": {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.email-identity.tag":                              {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.email-identity.untag":                            {capability.EffectUpdate, capability.IdempotencyIdempotent},

		"aws.ses.configuration-set.list":                    {capability.EffectRead, capability.IdempotencySafe},
		"aws.ses.configuration-set.get":                     {capability.EffectRead, capability.IdempotencySafe},
		"aws.ses.configuration-set.create":                  {capability.EffectCreate, capability.IdempotencyNonIdempotent},
		"aws.ses.configuration-set.delete":                  {capability.EffectDelete, capability.IdempotencyNonIdempotent},
		"aws.ses.configuration-set.put-delivery-options":    {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.configuration-set.put-reputation-options":  {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.configuration-set.put-sending-options":     {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.configuration-set.put-suppression-options": {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.configuration-set.put-tracking-options":    {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.configuration-set.put-vdm-options":         {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.configuration-set.put-archiving-options":   {capability.EffectUpdate, capability.IdempotencyIdempotent},
		"aws.ses.configuration-set-event-destination.get":   {capability.EffectRead, capability.IdempotencySafe},
		"aws.ses.suppressed-destination.list":               {capability.EffectRead, capability.IdempotencySafe},
		"aws.ses.suppressed-destination.get":                {capability.EffectRead, capability.IdempotencySafe},
		"aws.ses.suppressed-destination.put":                {capability.EffectCreate, capability.IdempotencyIdempotent},
		"aws.ses.suppressed-destination.delete":             {capability.EffectDelete, capability.IdempotencyNonIdempotent},
	}
	seen := 0
	for _, s := range generatedSpecs {
		if s.service != "ses" {
			continue
		}
		seen++
		w, ok := want[s.descriptor.ID]
		d := s.descriptor
		confirmation := capability.ConfirmationNone
		if w.effect != capability.EffectRead {
			confirmation = capability.ConfirmationRequired
		}
		if !ok || d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idem || d.Risk.Confirmation != confirmation ||
			!d.Risk.OpenWorld || d.Risk.DataSensitivity == "" || d.RequiresToolAllowList != (w.effect != capability.EffectRead && strings.HasPrefix(d.ID, "aws.ses.email-identity-policy.")) || d.Group != "ses" || s.rest == nil {
			t.Errorf("%s descriptor = %+v", d.ID, d)
		}
	}
	if seen != len(want) {
		t.Errorf("%d SES tools, want %d", seen, len(want))
	}
}

// No tool sends SendCustomVerificationEmail, and no tool takes key material of your own: the schemas name no
// selector, no private key, and not the EXTERNAL origin.
func TestSESExclusionsAndNoKeyMaterial(t *testing.T) {
	for _, s := range generatedSpecs {
		if s.service != "ses" {
			continue
		}
		if strings.Contains(s.rest.URI, "custom-verification") || strings.Contains(strings.ToLower(s.op), "custom verification") {
			t.Errorf("%s sends a custom verification email", s.descriptor.ID)
		}
		schema := strings.ToLower(string(s.descriptor.InputSchema))
		for _, banned := range []string{"private", "selector", "external"} {
			if strings.Contains(schema, banned) {
				t.Errorf("%s input schema mentions %q", s.descriptor.ID, banned)
			}
		}
		if strings.Contains(strings.ToLower(string(s.descriptor.OutputSchema)), "private") {
			t.Errorf("%s output schema mentions a private key", s.descriptor.ID)
		}
	}
}

// Create makes a domain and an address, sends exactly the described request, and returns the DKIM tokens and
// the DNS records derived from the domain the caller gave.
func TestSESCreateDomainAndAddress(t *testing.T) {
	f := sesFake(func(r sent) reply {
		return ok(`{"IdentityType":"DOMAIN","VerifiedForSendingStatus":false,"DkimAttributes":{"SigningEnabled":true,` +
			`"Status":"PENDING","Tokens":` + dkimTokensJSON + `,"SigningHostedZone":"dkim.eu-central-2.amazonses.com","SigningAttributesOrigin":"AWS_SES","NextSigningKeyLength":"RSA_2048_BIT"}}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	text, err := e.invokeConfirmed("aws.ses.email-identity.create",
		`{"email_identity":"Example.com","tags":[{"key":"team","value":"mail"}],"configuration_set_name":"default",`+
			`"dkim_signing_attributes":{"next_signing_key_length":"RSA_2048_BIT","domain_signing_attributes_origin":"AWS_SES"}}`, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got := decodeResult(t, text)
	dkim, _ := got["dkim_attributes"].(map[string]any)
	tokens, _ := dkim["tokens"].([]any)
	if got["region"] != testRegion || got["identity_type"] != "DOMAIN" || len(tokens) != 3 {
		t.Fatalf("result = %v", got)
	}
	records, _ := got["dns_records"].([]any)
	if len(records) != 3 {
		t.Fatalf("dns_records = %v", records)
	}
	first, _ := records[0].(map[string]any)
	if first["type"] != "CNAME" || first["purpose"] != "dkim" || first["name"] != tokenA+"._domainkey.example.com" ||
		first["value"] != tokenA+".dkim.eu-central-2.amazonses.com" {
		t.Errorf("first record = %v", first)
	}
	calls := restCalls(f)
	if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != "/v2/email/identities" ||
		calls[0].host != "email."+testRegion+".amazonaws.com" || f.count("GetCallerIdentity") != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	for _, part := range []string{`"EmailIdentity":"Example.com"`, `"Tags":[{"Key":"team","Value":"mail"}]`, `"ConfigurationSetName":"default"`,
		`"NextSigningKeyLength":"RSA_2048_BIT"`, `"DomainSigningAttributesOrigin":"AWS_SES"`} {
		if !strings.Contains(calls[0].body, part) {
			t.Errorf("body %s lacks %s", calls[0].body, part)
		}
	}

	// An address needs no DNS records and has no tokens in the answer.
	f2 := sesFake(func(sent) reply { return ok(`{"IdentityType":"EMAIL_ADDRESS","VerifiedForSendingStatus":false}`) })
	e2 := sesEnv(t, f2, "account/"+testAccount, "region/"+testRegion)
	text, err = e2.invokeConfirmed("aws.ses.email-identity.create", `{"email_identity":"sender@example.com"}`, true)
	if err != nil {
		t.Fatalf("create address: %v", err)
	}
	got = decodeResult(t, text)
	if got["identity_type"] != "EMAIL_ADDRESS" || got["dns_records"] != nil {
		t.Errorf("address result = %v", got)
	}
	if calls := restCalls(f2); len(calls) != 1 || !strings.Contains(calls[0].body, `"EmailIdentity":"sender@example.com"`) {
		t.Errorf("address calls = %+v", calls)
	}
}

// The four errors of the identity operations map to the classes of awserr, keep the code in Original, and
// never repeat a create.
func TestSESErrors(t *testing.T) {
	for _, c := range []struct {
		code    string
		status  int
		class   provider.Class
		message string
	}{
		{"AlreadyExistsException", 400, provider.ClassProviderError, "conflict"},
		{"LimitExceededException", 400, provider.ClassProviderError, "limit or quota"},
		{"NotFoundException", 404, provider.ClassNotFound, "did not find"},
		{"BadRequestException", 400, provider.ClassProviderError, "invalid"},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := sesFake(func(sent) reply {
				return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code}, body: `{"message":"secret text"}`}
			})
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
			_, err := e.invokeConfirmed("aws.ses.email-identity.create", `{"email_identity":"example.com"}`, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || !strings.Contains(failed.Message, c.message) ||
				failed.Original == nil || len(failed.Original.Entries) != 1 || failed.Original.Entries[0].Code != c.code ||
				strings.Contains(err.Error(), "secret text") {
				t.Fatalf("error = %v (%+v)", err, failed)
			}
			if n := len(restCalls(f)); n != 1 {
				t.Errorf("create was sent %d times", n)
			}
		})
	}
}

func getAnswer(verified bool, mailFrom string) reply {
	status, ok2 := "PENDING", "false"
	if verified {
		status, ok2 = "SUCCESS", "true"
	}
	body := `{"IdentityType":"DOMAIN","FeedbackForwardingStatus":true,"VerifiedForSendingStatus":` + ok2 + `,"VerificationStatus":"` + status + `",` +
		`"DkimAttributes":{"SigningEnabled":true,"Status":"` + status + `","Tokens":` + dkimTokensJSON + `,"SigningHostedZone":"dkim.amazonses.com"},` +
		`"Policies":{"p":"{}"},"Tags":[{"Key":"team","Value":"mail"}],"ConfigurationSetName":"default"`
	if mailFrom != "" {
		body += `,"MailFromAttributes":{"MailFromDomain":"` + mailFrom + `","MailFromDomainStatus":"SUCCESS","BehaviorOnMxFailure":"REJECT_MESSAGE"}`
	}
	return ok(body + `}`)
}

// Get with and without a verified status returns the status as the answer says, and the DNS help lists the
// DKIM CNAMEs and the MAIL FROM MX and SPF records for the region of the call.
func TestSESGetAndDNSHelp(t *testing.T) {
	for _, verified := range []bool{true, false} {
		f := sesFake(func(r sent) reply { return getAnswer(verified, "mail.example.com") })
		e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
		text, err := e.invoke("aws.ses.email-identity.get", `{"email_identity":"example.com"}`)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		got := decodeResult(t, text)
		// A false value is left out of the answer.
		if (got["verified_for_sending_status"] == true) != verified || (got["verification_status"] == "SUCCESS") != verified {
			t.Errorf("verified = %v: result %v", verified, got)
		}
		records, _ := got["dns_records"].([]any)
		if len(records) != 5 {
			t.Fatalf("dns_records = %v", records)
		}
		mx, _ := records[3].(map[string]any)
		spf, _ := records[4].(map[string]any)
		if mx["type"] != "MX" || mx["name"] != "mail.example.com" || mx["value"] != "10 feedback-smtp."+testRegion+".amazonses.com" ||
			spf["type"] != "TXT" || spf["value"] != "v=spf1 include:amazonses.com ~all" {
			t.Errorf("mail from records = %v, %v", mx, spf)
		}
		if calls := restCalls(f); len(calls) != 1 || calls[0].method != "GET" || calls[0].path != "/v2/email/identities/example.com" {
			t.Errorf("calls = %+v", calls)
		}
	}
}

// Values of the answer that have not the expected form never reach the DNS help: a forged token, a MAIL FROM
// domain outside the identity, and an address identity. The help is built from the identity the caller gave.
func TestSESDNSHelpRefusesManipulatedValues(t *testing.T) {
	records := func(result map[string]any, identity string) []any {
		addDNSRecords(result, testRegion, map[string]any{"email_identity": identity})
		return result["dns_records"].([]any)
	}
	zoned := func(zone any, tokens ...any) map[string]any {
		return map[string]any{"dkim_attributes": map[string]any{"tokens": tokens, "signing_hosted_zone": zone}}
	}
	forged := []any{tokenA, "a.evil.example", "AAAA", "x y", "tok\nen", tokenB + ";", "", 7, tokenC}
	got := records(zoned("dkim.amazonses.com", forged...), "example.com")
	var names []string
	for _, r := range got {
		names = append(names, r.(map[string]any)["name"].(string))
	}
	if !slices.Equal(names, []string{tokenA + "._domainkey.example.com", tokenC + "._domainkey.example.com"}) {
		t.Errorf("records for forged tokens = %v", names)
	}
	for _, domain := range []string{"mail.evil.example", "example.com", "evil.example.com.evil.net", "mail.example.com ", "a b.example.com",
		"-mail.example.com", "mail.example.com\n", "", "xexample.com"} {
		if got := records(map[string]any{"mail_from_attributes": map[string]any{"mail_from_domain": domain}}, "example.com"); len(got) != 0 {
			t.Errorf("mail from %q gave %v", domain, got)
		}
	}
	for _, identity := range []string{"sender@example.com", "evil..com", ""} {
		got := records(map[string]any{"dkim_attributes": map[string]any{"tokens": []any{tokenA}, "signing_hosted_zone": "dkim.amazonses.com"},
			"mail_from_attributes": map[string]any{"mail_from_domain": "mail.example.com"}}, identity)
		if len(got) != 0 {
			t.Errorf("identity %q gave %v", identity, got)
		}
	}
	// The DKIM target is the zone of the answer in the narrow form, and nothing else.
	for zone, want := range map[any]string{
		"dkim.amazonses.com": "dkim.amazonses.com", "dkim.eu-central-2.amazonses.com": "dkim.eu-central-2.amazonses.com",
		"dkim.af-south-1.amazonses.com": "dkim.af-south-1.amazonses.com",
		nil:                             "", "": "", 7: "", "evil.example": "", "dkim.amazonses.com.evil.example": "", "amazonses.com.evil.example": "",
		"dkim.evil.example": "", "DKIM.AMAZONSES.COM": "", "dkim.amazonses.com\n": "", "dkim.a b.amazonses.com": "",
		"dkim.\x00.amazonses.com": "", "amazonses.com": "", "evil.amazonses.com": "", "dkim.a.b.amazonses.com": "",
		".dkim.amazonses.com": "", "dkim..amazonses.com": "", "dkim.-a.amazonses.com": "",
	} {
		got := records(zoned(zone, tokenA), "example.com")
		switch {
		case want == "" && len(got) != 0:
			t.Errorf("zone %q gave %v", zone, got)
		case want != "" && (len(got) != 1 || got[0].(map[string]any)["value"] != tokenA+"."+want):
			t.Errorf("zone %q gave %v", zone, got)
		}
	}
	if got := records(map[string]any{"dkim_attributes": map[string]any{"tokens": []any{tokenA}}}, "example.com"); len(got) != 0 {
		t.Errorf("no zone gave %v", got)
	}
	many := make([]any, 40)
	for i := range many {
		many[i] = tokenA
	}
	if got := records(zoned("dkim.amazonses.com", many...), "example.com"); len(got) != maxDKIMTokens {
		t.Errorf("%d records for 40 tokens", len(got))
	}
	// A region that is not a region name gives no MX record.
	result := map[string]any{"mail_from_attributes": map[string]any{"mail_from_domain": "mail.example.com"}}
	addDNSRecords(result, "eu-central-1.evil.example", map[string]any{"email_identity": "example.com"})
	if got := result["dns_records"].([]any); len(got) != 0 {
		t.Errorf("records for a bad region = %v", got)
	}
}

// The answer of a tool whose provider returns a forged token still validates and names no forged record.
func TestSESGetWithForgedTokenInTheAnswer(t *testing.T) {
	f := sesFake(func(sent) reply {
		return ok(`{"IdentityType":"DOMAIN","DkimAttributes":{"SigningEnabled":true,"Status":"PENDING","SigningHostedZone":"dkim.amazonses.com","Tokens":["x.evil.example","` + tokenA + `"]}}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	text, err := e.invoke("aws.ses.email-identity.get", `{"email_identity":"example.com"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(text, "_domainkey") != 1 || !strings.Contains(text, tokenA+"._domainkey.example.com") {
		t.Errorf("result = %s", text)
	}
}

// Put MAIL FROM sends both behaviors, and the other Put tools send their member and nothing else.
func TestSESPutTools(t *testing.T) {
	for _, c := range []struct {
		tool, args, method, path, body string
	}{
		{"aws.ses.email-identity.put-mail-from-attributes", `{"email_identity":"example.com","mail_from_domain":"mail.example.com","behavior_on_mx_failure":"USE_DEFAULT_VALUE"}`,
			"PUT", "/v2/email/identities/example.com/mail-from", `{"BehaviorOnMxFailure":"USE_DEFAULT_VALUE","MailFromDomain":"mail.example.com"}`},
		{"aws.ses.email-identity.put-mail-from-attributes", `{"email_identity":"example.com","mail_from_domain":"mail.example.com","behavior_on_mx_failure":"REJECT_MESSAGE"}`,
			"PUT", "/v2/email/identities/example.com/mail-from", `{"BehaviorOnMxFailure":"REJECT_MESSAGE","MailFromDomain":"mail.example.com"}`},
		{"aws.ses.email-identity.put-dkim-attributes", `{"email_identity":"example.com","signing_enabled":false}`,
			"PUT", "/v2/email/identities/example.com/dkim", `{"SigningEnabled":false}`},
		{"aws.ses.email-identity.put-feedback-attributes", `{"email_identity":"example.com","email_forwarding_enabled":false}`,
			"PUT", "/v2/email/identities/example.com/feedback", `{"EmailForwardingEnabled":false}`},
		{"aws.ses.email-identity.put-configuration-set-attributes", `{"email_identity":"example.com","configuration_set_name":"default"}`,
			"PUT", "/v2/email/identities/example.com/configuration-set", `{"ConfigurationSetName":"default"}`},
		{"aws.ses.email-identity.delete", `{"email_identity":"example.com"}`, "DELETE", "/v2/email/identities/example.com", ""},
	} {
		f := sesFake(func(sent) reply { return ok(`{}`) })
		e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err != nil {
			t.Errorf("%s: %v", c.tool, err)
			continue
		}
		calls := restCalls(f)
		if len(calls) != 1 || calls[0].method != c.method || calls[0].path != c.path || !jsonEqual(calls[0].body, c.body) {
			t.Errorf("%s calls = %+v, want %s %s %s", c.tool, calls, c.method, c.path, c.body)
		}
	}
	f := sesFake(func(sent) reply {
		return ok(`{"DkimStatus":"PENDING","DkimTokens":` + dkimTokensJSON + `,"SigningHostedZone":"dkim.amazonses.com"}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	text, err := e.invokeConfirmed("aws.ses.email-identity.put-dkim-signing-attributes",
		`{"email_identity":"example.com","signing_attributes_origin":"AWS_SES_EU_WEST_1","signing_attributes":{"next_signing_key_length":"RSA_1024_BIT"}}`, true)
	got := decodeResult(t, text)
	calls := restCalls(f)
	if err != nil || got["dkim_status"] != "PENDING" || len(calls) != 1 || calls[0].path != "/v2/email/identities/example.com/dkim/signing" ||
		!jsonEqual(calls[0].body, `{"SigningAttributesOrigin":"AWS_SES_EU_WEST_1","SigningAttributes":{"NextSigningKeyLength":"RSA_1024_BIT"}}`) {
		t.Errorf("signing attributes: %v, %v, %+v", err, got, calls)
	}
}

func jsonEqual(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	var x, y any
	return unmarshalAny(a, &x) && unmarshalAny(b, &y) && sameJSON(x, y)
}

// Key material of your own is not an argument: the selector, the private key, and the EXTERNAL origin are
// refused before any request, at both levels.
func TestSESRefusesKeyMaterial(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	for _, c := range [][2]string{
		{"aws.ses.email-identity.create", `{"email_identity":"example.com","dkim_signing_attributes":{"domain_signing_private_key":"QUJD","domain_signing_selector":"s"}}`},
		{"aws.ses.email-identity.create", `{"email_identity":"example.com","dkim_signing_attributes":{"domain_signing_private_key":"QUJD"}}`},
		{"aws.ses.email-identity.create", `{"email_identity":"example.com","dkim_signing_attributes":{"domain_signing_selector":"s"}}`},
		{"aws.ses.email-identity.create", `{"email_identity":"example.com","dkim_signing_attributes":{"domain_signing_attributes_origin":"EXTERNAL"}}`},
		{"aws.ses.email-identity.put-dkim-signing-attributes", `{"email_identity":"example.com","signing_attributes_origin":"EXTERNAL"}`},
		{"aws.ses.email-identity.put-dkim-signing-attributes", `{"email_identity":"example.com","signing_attributes_origin":"AWS_SES","signing_attributes":{"domain_signing_private_key":"QUJD"}}`},
		{"aws.ses.email-identity.put-dkim-signing-attributes", `{"email_identity":"example.com","signing_attributes_origin":"AWS_SES","signing_attributes":{"domain_signing_selector":"s"}}`},
		{"aws.ses.email-identity.put-dkim-signing-attributes", `{"email_identity":"example.com","signing_attributes_origin":"AWS_SES","signing_attributes":{"domain_signing_attributes_origin":"EXTERNAL"}}`},
	} {
		if _, err := e.invokeConfirmed(c[0], c[1], true); err == nil {
			t.Errorf("%s %s was accepted", c[0], c[1])
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("requests = %v", f.actions())
	}
}

// The list pages over three pages by NextToken, hides the token behind the cursor, and sends PageSize; both
// members are in the body of this operation.
func TestSESListPagesOverThreePages(t *testing.T) {
	f := sesFake(func(r sent) reply {
		var body struct{ NextToken string }
		_ = json.Unmarshal([]byte(r.body), &body)
		page := func(name, next string) reply {
			body := `{"EmailIdentities":[{"IdentityType":"DOMAIN","IdentityName":"` + name + `","SendingEnabled":true,"VerificationStatus":"SUCCESS"}]`
			if next != "" {
				body += `,"NextToken":"` + next + `"`
			}
			return ok(body + `}`)
		}
		switch body.NextToken {
		case "":
			return page("a.example.com", "tok-1")
		case "tok-1":
			return page("b.example.com", "tok-2")
		}
		return page("c.example.com", "")
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	var names []string
	cursor, issued := "", ""
	for i := 0; i < 3; i++ {
		args := `{"page_size":1,"filter":{"IDENTITY_TYPE":"DOMAIN"}`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		text, err := e.invoke("aws.ses.email-identity.list", args+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		got := decodeResult(t, text)
		items, _ := got["email_identities"].([]any)
		names = append(names, items[0].(map[string]any)["identity_name"].(string))
		next, _ := got["next_cursor"].(string)
		if (next != "") != (i < 2) || strings.Contains(text, "tok-") || strings.Contains(text, "~") {
			t.Fatalf("page %d result = %s", i, text)
		}
		if issued == "" {
			issued = next
		}
		cursor = next
	}
	if !slices.Equal(names, []string{"a.example.com", "b.example.com", "c.example.com"}) {
		t.Errorf("names = %v", names)
	}
	calls := restCalls(f)
	if len(calls) != 3 || calls[0].method != "POST" || calls[0].path != "/v2/email/list-identities" {
		t.Fatalf("calls = %+v", calls)
	}
	if !jsonEqual(calls[1].body, `{"Filter":{"IDENTITY_TYPE":"DOMAIN"},"NextToken":"tok-1","PageSize":1}`) {
		t.Errorf("second page = %+v", calls[1])
	}
	// A page size above the maximum, and a cursor of other arguments, are refused before any request.
	before := len(f.requests)
	for _, args := range []string{`{"page_size":1001}`, `{"cursor":"` + issued + `"}`} {
		if _, err := e.invoke("aws.ses.email-identity.list", args); err == nil {
			t.Errorf("%s was accepted", args)
		}
	}
	if len(f.requests) != before {
		t.Errorf("a refused page was sent")
	}
}

// The region must be one of the allowlist, an ARN of another account or region or kind is refused, an
// identity name of no valid form is refused, and a write without confirmation is refused: all before any I/O.
func TestSESRefusalsBeforeAnyIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	foreign := "arn:aws:ses:" + testRegion + ":210987654321:identity/example.com"
	for _, c := range []struct {
		tool, args string
		confirmed  bool
	}{
		{"aws.ses.email-identity.get", `{"email_identity":"example.com","region":"us-east-1"}`, false},
		{"aws.ses.email-identity.list", `{"region":"ap-south-1"}`, false},
		{"aws.ses.email-identity.list-tags", `{"resource_arn":"` + foreign + `"}`, false},
		{"aws.ses.email-identity.tag", `{"resource_arn":"` + foreign + `","tags":[{"key":"a","value":"b"}]}`, true},
		{"aws.ses.email-identity.untag", `{"resource_arn":"` + foreign + `","tag_keys":["a"]}`, true},
		{"aws.ses.email-identity.tag", `{"resource_arn":"arn:aws:ses:us-east-1:` + testAccount + `:identity/example.com","tags":[{"key":"a","value":"b"}]}`, true},
		{"aws.ses.email-identity.tag", `{"resource_arn":"arn:aws:ses:` + testRegion + `:` + testAccount + `:configuration-set/a b","tags":[{"key":"a","value":"b"}]}`, true},
		{"aws.ses.email-identity.tag", `{"resource_arn":"arn:aws:ses:` + testRegion + `:` + testAccount + `:dedicated-ip-pool/pool","tags":[{"key":"a","value":"b"}]}`, true},
		{"aws.ses.email-identity.untag", `{"resource_arn":"arn:aws:ses:` + testRegion + `:210987654321:configuration-set/default","tag_keys":["a"]}`, true},
		{"aws.ses.email-identity.list-tags", `{"resource_arn":"arn:aws:ses:us-east-1:` + testAccount + `:configuration-set/default"}`, false},
		{"aws.ses.email-identity.tag", `{"resource_arn":"arn:aws:sns:` + testRegion + `:` + testAccount + `:identity/example.com","tags":[{"key":"a","value":"b"}]}`, true},
		{"aws.ses.email-identity.tag", `{"resource_arn":"arn:aws-cn:ses:` + testRegion + `:` + testAccount + `:identity/example.com","tags":[{"key":"a","value":"b"}]}`, true},
		{"aws.ses.email-identity.get", `{"email_identity":"a/b.example.com"}`, false},
		{"aws.ses.email-identity.get", `{"email_identity":"../example.com"}`, false},
		{"aws.ses.email-identity.delete", `{"email_identity":"example.com?x=1"}`, true},
		{"aws.ses.email-identity.create", `{"email_identity":"nodots"}`, true},
		{"aws.ses.email-identity.create", `{"email_identity":"user@"}`, true},
		{"aws.ses.email-identity.create", `{"email_identity":"a@b@example.com"}`, true},
		{"aws.ses.email-identity.create", `{"email_identity":"example.com\n"}`, true},
		{"aws.ses.email-identity.create", `{"email_identity":"12.34"}`, true},
	} {
		if _, err := e.invokeConfirmed(c.tool, c.args, c.confirmed); err == nil {
			t.Errorf("%s %s was accepted", c.tool, c.args)
		}
	}
	for _, c := range [][2]string{
		{"aws.ses.email-identity.create", `{"email_identity":"example.com"}`},
		{"aws.ses.email-identity.delete", `{"email_identity":"example.com"}`},
		{"aws.ses.email-identity.put-dkim-attributes", `{"email_identity":"example.com","signing_enabled":true}`},
		{"aws.ses.email-identity.tag", `{"resource_arn":"` + sesIdentityARN + `","tags":[{"key":"a","value":"b"}]}`},
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

// Tags and the policies read only: the ARN of an identity in the bound account is sent as a query member.
func TestSESTagsAndPolicies(t *testing.T) {
	f := sesFake(func(r sent) reply {
		switch {
		case r.method == "GET" && strings.HasSuffix(r.path, "/policies"):
			return ok(`{"Policies":{"p1":"{}"}}`)
		case r.method == "GET":
			return ok(`{"Tags":[{"Key":"team","Value":"mail"}]}`)
		}
		return ok(`{}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	if text, err := e.invoke("aws.ses.email-identity.list-tags", `{"resource_arn":"`+sesIdentityARN+`"}`); err != nil || !strings.Contains(text, `"team"`) {
		t.Errorf("list-tags: %v %s", err, text)
	}
	if text, err := e.invoke("aws.ses.email-identity-policy.list", `{"email_identity":"example.com"}`); err != nil || !strings.Contains(text, `"p1"`) {
		t.Errorf("policy list: %v %s", err, text)
	}
	if _, err := e.invokeConfirmed("aws.ses.email-identity.tag", `{"resource_arn":"`+sesIdentityARN+`","tags":[{"key":"team","value":"mail"}]}`, true); err != nil {
		t.Errorf("tag: %v", err)
	}
	if _, err := e.invokeConfirmed("aws.ses.email-identity.untag", `{"resource_arn":"`+sesIdentityARN+`","tag_keys":["team","x"]}`, true); err != nil {
		t.Errorf("untag: %v", err)
	}
	calls := restCalls(f)
	if len(calls) != 4 {
		t.Fatalf("calls = %+v", calls)
	}
	if q, _ := url.ParseQuery(calls[0].rawQuery); calls[0].path != "/v2/email/tags" || q.Get("ResourceArn") != sesIdentityARN {
		t.Errorf("list-tags = %+v", calls[0])
	}
	if calls[1].path != "/v2/email/identities/example.com/policies" {
		t.Errorf("policy list = %+v", calls[1])
	}
	if calls[2].method != "POST" || !jsonEqual(calls[2].body, `{"ResourceArn":"`+sesIdentityARN+`","Tags":[{"Key":"team","Value":"mail"}]}`) {
		t.Errorf("tag = %+v", calls[2])
	}
	if q, _ := url.ParseQuery(calls[3].rawQuery); calls[3].method != "DELETE" || q.Get("ResourceArn") != sesIdentityARN ||
		!slices.Equal(q["TagKeys"], []string{"team", "x"}) {
		t.Errorf("untag = %+v", calls[3])
	}
}

// The identity and ARN checks, by table.
func TestSESIdentityForms(t *testing.T) {
	b := binding{account: testAccount, regions: []string{testRegion}}
	for _, good := range []string{"example.com", "Mail.Example.COM", "xn--bcher-kva.example", "a-b.example.co.uk", "user@example.com", "first.last+tag@example.com"} {
		if err := checkSESIdentity(b, false, "email_identity", good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"", "example", "example.", ".example.com", "a..com", "-a.com", "a-.com", "a_b.com", "a b.com", "example.com/x", "12.34",
		"@example.com", "a@b@example.com", "us er@example.com", "a/b@example.com", strings.Repeat("a", 64) + ".com", strings.Repeat("a.", 130) + "com",
		strings.Repeat("a", 65) + "@example.com", "example.com\n", "exämple.com"} {
		if err := checkSESIdentity(b, false, "email_identity", bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	for _, good := range []string{sesIdentityARN, "arn:aws:ses:" + testRegion + ":" + testAccount + ":identity/user@example.com",
		sesSetARN, "arn:aws:ses:" + testRegion + ":" + testAccount + ":configuration-set/My_set-1"} {
		if err := checkSESIdentityARN(b, false, "resource_arn", good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"", "example.com", "arn:aws:ses:eu-central-1:123456789012:identity/", "arn:aws:ses:eu-central-1:123456789012:identity/a b",
		"arn:aws:ses:eu-central-1:210987654321:identity/example.com", "arn:aws:ses:us-east-1:123456789012:identity/example.com",
		"arn:aws:ses:eu-central-1:123456789012:configuration-set/", "arn:aws:ses:eu-central-1:123456789012:configuration-set/a b",
		"arn:aws:ses:eu-central-1:123456789012:configuration-set/" + strings.Repeat("a", 65), "arn:aws:ses:eu-central-1:210987654321:configuration-set/x",
		"arn:aws:ses:us-east-1:123456789012:configuration-set/x", "arn:aws:ses:eu-central-1:123456789012:dedicated-ip-pool/x",
		"arn:aws-cn:ses:eu-central-1:123456789012:configuration-set/x", "arn:aws:ses:eu-central-1:123456789012:identity",
		"arn:aws:iam:eu-central-1:123456789012:identity/example.com", "arn:aws-cn:ses:eu-central-1:123456789012:identity/example.com"} {
		if err := checkSESIdentityARN(b, false, "resource_arn", bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func unmarshalAny(text string, v *any) bool { return json.Unmarshal([]byte(text), v) == nil }

func sameJSON(a, b any) bool { return reflect.DeepEqual(a, b) }
