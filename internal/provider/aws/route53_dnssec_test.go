package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	dnssecZone = "Z0123456789ABCDEFGHIJ"
	kskARN     = "arn:aws:kms:us-east-1:" + testAccount + ":key/1234abcd-12ab-34cd-56ef-1234567890ab"
)

// route53DNSSECWrite reports whether a tool ID is a DNSSEC write, which is listed-only.
func route53DNSSECWrite(id string) bool {
	switch id {
	case "aws.route53.dnssec.enable", "aws.route53.dnssec.disable", "aws.route53.key-signing-key.create",
		"aws.route53.key-signing-key.activate", "aws.route53.key-signing-key.deactivate", "aws.route53.key-signing-key.delete":
		return true
	}
	return false
}

var dnssecWrites = []struct{ id, args string }{
	{"aws.route53.dnssec.enable", `{"hosted_zone_id":"` + dnssecZone + `"}`},
	{"aws.route53.dnssec.disable", `{"hosted_zone_id":"` + dnssecZone + `"}`},
	{"aws.route53.key-signing-key.create", `{"hosted_zone_id":"` + dnssecZone + `","key_management_service_arn":"` + kskARN + `","name":"ksk_1","status":"INACTIVE"}`},
	{"aws.route53.key-signing-key.activate", `{"hosted_zone_id":"` + dnssecZone + `","name":"ksk_1"}`},
	{"aws.route53.key-signing-key.deactivate", `{"hosted_zone_id":"` + dnssecZone + `","name":"ksk_1"}`},
	{"aws.route53.key-signing-key.delete", `{"hosted_zone_id":"` + dnssecZone + `","name":"ksk_1"}`},
}

const kskXML = `<KeySigningKey><Name>ksk_1</Name><KmsArn>` + kskARN + `</KmsArn><Flag>257</Flag><SigningAlgorithmMnemonic>ECDSAP256SHA256</SigningAlgorithmMnemonic>` +
	`<SigningAlgorithmType>13</SigningAlgorithmType><DigestAlgorithmMnemonic>SHA-256</DigestAlgorithmMnemonic><DigestAlgorithmType>2</DigestAlgorithmType>` +
	`<KeyTag>12345</KeyTag><DigestValue>ABCD</DigestValue><PublicKey>PK</PublicKey><DSRecord>12345 13 2 ABCD</DSRecord>` +
	`<DNSKEYRecord>example.com. IN DNSKEY 257 3 13 PK</DNSKEYRecord><Status>ACTIVE</Status><StatusMessage>m</StatusMessage>` +
	`<CreatedDate>2026-01-02T03:04:05Z</CreatedDate><LastModifiedDate>2026-01-02T03:04:05Z</LastModifiedDate></KeySigningKey>`

// get reads the status with and without DNSSEC and returns the DS record the owner enters at the registrar.
func TestRoute53DNSSECGet(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		if strings.Contains(r.path, "/Znone/") {
			return xmlReply(`<GetDNSSECResponse ` + route53NS + `><Status><ServeSignature>NOT_SIGNING</ServeSignature></Status><KeySigningKeys/></GetDNSSECResponse>`)
		}
		return xmlReply(`<GetDNSSECResponse ` + route53NS + `><Status><ServeSignature>SIGNING</ServeSignature></Status><KeySigningKeys><member>` +
			strings.TrimSuffix(strings.TrimPrefix(kskXML, "<KeySigningKey>"), "</KeySigningKey>") + `</member></KeySigningKeys></GetDNSSECResponse>`)
	})
	e := route53Env(t, f, testRegion)
	text, err := e.invoke("aws.route53.dnssec.get", `{"hosted_zone_id":"/hostedzone/Z1"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	status, _ := got["status"].(map[string]any)
	keys, _ := got["key_signing_keys"].([]any)
	key, _ := keys[0].(map[string]any)
	if status["serve_signature"] != "SIGNING" || len(keys) != 1 || key["ds_record"] != "12345 13 2 ABCD" || key["kms_arn"] != kskARN ||
		key["status"] != "ACTIVE" || key["dnskey_record"] == "" {
		t.Errorf("result = %s", text)
	}
	if r := restRequests(f)[0]; r.method != "GET" || r.path != "/2013-04-01/hostedzone/Z1/dnssec" {
		t.Errorf("request = %+v", r)
	}
	text, err = e.invoke("aws.route53.dnssec.get", `{"hosted_zone_id":"Znone"}`)
	if err != nil {
		t.Fatal(err)
	}
	got = decodeResult(t, text)
	if keys, _ := got["key_signing_keys"].([]any); len(keys) != 0 || got["status"].(map[string]any)["serve_signature"] != "NOT_SIGNING" {
		t.Errorf("result without DNSSEC = %s", text)
	}
}

// The writes send their request in the shape of the model and return the change.
func TestRoute53DNSSECWritesSendTheirRequest(t *testing.T) {
	change := `<ChangeInfo><Id>/change/C1</Id><Status>PENDING</Status><SubmittedAt>2026-01-02T03:04:05Z</SubmittedAt></ChangeInfo>`
	f := direct(func(r sent, _ int) reply {
		if strings.HasSuffix(r.path, "/keysigningkey") {
			rep := xmlReply(`<CreateKeySigningKeyResponse ` + route53NS + `>` + change + kskXML + `</CreateKeySigningKeyResponse>`)
			rep.status, rep.header["Location"] = 201, "https://route53.amazonaws.com/2013-04-01/keysigningkey/"+dnssecZone+"/ksk_1"
			return rep
		}
		return xmlReply(`<R ` + route53NS + `>` + change + `</R>`)
	})
	e := newToolsEnv(t, f, baseValues(), route53ToolIDs)
	for _, c := range dnssecWrites {
		text, err := e.invokeConfirmed(c.id, c.args, true)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		got := decodeResult(t, text)
		info, _ := got["change_info"].(map[string]any)
		if info["id"] != "/change/C1" || info["status"] != "PENDING" {
			t.Errorf("%s result = %s", c.id, text)
		}
		if c.id == "aws.route53.key-signing-key.create" {
			if got["client_token"] == "" || got["key_signing_key"].(map[string]any)["ds_record"] != "12345 13 2 ABCD" {
				t.Errorf("create result = %s", text)
			}
		}
	}
	z := "/2013-04-01/hostedzone/" + dnssecZone
	k := "/2013-04-01/keysigningkey/" + dnssecZone + "/ksk_1"
	for i, want := range []struct{ method, path string }{
		{"POST", z + "/enable-dnssec"}, {"POST", z + "/disable-dnssec"}, {"POST", "/2013-04-01/keysigningkey"},
		{"POST", k + "/activate"}, {"POST", k + "/deactivate"}, {"DELETE", k},
	} {
		if r := restRequests(f)[i]; r.method != want.method || r.path != want.path {
			t.Errorf("request %d = %s %s, want %s %s", i, r.method, r.path, want.method, want.path)
		}
	}
	body := restRequests(f)[2].body
	for _, part := range []string{"<CallerReference>", "<HostedZoneId>" + dnssecZone + "</HostedZoneId>", "<KeyManagementServiceArn>" + kskARN + "</KeyManagementServiceArn>",
		"<Name>ksk_1</Name>", "<Status>INACTIVE</Status>"} {
		if !strings.Contains(body, part) {
			t.Errorf("create body lacks %s: %s", part, body)
		}
	}
}

// The KMS key must be of the bound account and in us-east-1, whatever regions the connection allows; anything
// else is refused before any request.
func TestRoute53KeySigningKeyChecksTheKMSKeyBeforeAnyRequest(t *testing.T) {
	const key = "key/1234abcd-12ab-34cd-56ef-1234567890ab"
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, testRegion)
	for _, arn := range []string{
		"arn:aws:kms:us-east-1:999999999999:" + key, "arn:aws:kms:eu-central-1:" + testAccount + ":" + key,
		"arn:aws:kms:" + testRegion + ":" + testAccount + ":" + key, "arn:aws:kms:us-east-1:" + testAccount + ":alias/a",
		"arn:aws:kms:us-east-1:" + testAccount + ":key/", "arn:aws:iam::" + testAccount + ":role/a", "arn:aws-cn:kms:us-east-1:" + testAccount + ":" + key,
		"1234abcd-12ab-34cd-56ef-1234567890ab", "arn:aws:kms:us-east-1::" + key, "",
	} {
		args := `{"hosted_zone_id":"` + dnssecZone + `","key_management_service_arn":"` + arn + `","name":"ksk_1","status":"INACTIVE"}`
		if _, err := e.invokeConfirmed("aws.route53.key-signing-key.create", args, true); err == nil {
			t.Errorf("%q was accepted", arn)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call reached the network: %v", f.actions())
	}
	// us-east-1 need not be allowed by the connection.
	ok := route53Env(t, restAnswer(xmlReply(`<CreateKeySigningKeyResponse `+route53NS+`><ChangeInfo><Id>c</Id><Status>PENDING</Status><SubmittedAt>2026-01-02T03:04:05Z</SubmittedAt></ChangeInfo>`+kskXML+`</CreateKeySigningKeyResponse>`)), "eu-central-1")
	args := `{"hosted_zone_id":"` + dnssecZone + `","key_management_service_arn":"` + kskARN + `","name":"ksk_1","status":"ACTIVE"}`
	if _, err := ok.invokeConfirmed("aws.route53.key-signing-key.create", args, true); err != nil {
		t.Errorf("own account in us-east-1: %v", err)
	}
}

// Errors keep their class and code, and a failed write is not repeated.
func TestRoute53DNSSECErrors(t *testing.T) {
	zone := `{"hosted_zone_id":"` + dnssecZone + `"}`
	ksk := `{"hosted_zone_id":"` + dnssecZone + `","name":"ksk_1"}`
	create := dnssecWrites[2].args
	for _, c := range []struct {
		id, args, code string
		status         int
		class          provider.Class
	}{
		{"aws.route53.key-signing-key.create", create, "KeySigningKeyAlreadyExists", 409, provider.ClassProviderError},
		{"aws.route53.key-signing-key.create", create, "InvalidKMSArn", 400, provider.ClassProviderError},
		{"aws.route53.key-signing-key.create", create, "InvalidKeySigningKeyStatus", 400, provider.ClassProviderError},
		{"aws.route53.key-signing-key.create", create, "NoSuchHostedZone", 404, provider.ClassNotFound},
		{"aws.route53.key-signing-key.create", create, "TooManyKeySigningKeys", 400, provider.ClassProviderError},
		{"aws.route53.key-signing-key.activate", ksk, "InvalidKMSArn", 400, provider.ClassProviderError},
		{"aws.route53.key-signing-key.activate", ksk, "NoSuchKeySigningKey", 404, provider.ClassNotFound},
		{"aws.route53.key-signing-key.deactivate", ksk, "KeySigningKeyInParentDSRecord", 400, provider.ClassProviderError},
		{"aws.route53.key-signing-key.deactivate", ksk, "KeySigningKeyInUse", 400, provider.ClassProviderError},
		{"aws.route53.key-signing-key.delete", ksk, "InvalidKeySigningKeyStatus", 400, provider.ClassProviderError},
		{"aws.route53.dnssec.disable", zone, "KeySigningKeyInParentDSRecord", 400, provider.ClassProviderError},
		{"aws.route53.dnssec.disable", zone, "DNSSECNotFound", 400, provider.ClassNotFound},
		{"aws.route53.dnssec.enable", zone, "NoSuchHostedZone", 404, provider.ClassNotFound},
		{"aws.route53.dnssec.enable", zone, "KeySigningKeyWithActiveStatusNotFound", 400, provider.ClassNotFound},
		{"aws.route53.dnssec.get", zone, "NoSuchHostedZone", 404, provider.ClassNotFound},
	} {
		t.Run(c.id+"/"+c.code, func(t *testing.T) {
			f := restAnswer(route53Error(c.status, c.code))
			e := newToolsEnv(t, f, baseValues(), route53ToolIDs)
			_, err := e.invokeConfirmed(c.id, c.args, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class {
				t.Fatalf("err = %v, want class %s", err, c.class)
			}
			if failed.Original == nil || len(failed.Original.Entries) != 1 || failed.Original.Entries[0].Code != c.code {
				t.Errorf("original = %+v", failed.Original)
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("requests = %d, a failed call was repeated", n)
			}
		})
	}
}

// Every write needs confirmation before it reaches the network.
func TestRoute53DNSSECWritesNeedConfirmation(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := newToolsEnv(t, f, baseValues(), route53ToolIDs)
	for _, c := range dnssecWrites {
		if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
			t.Errorf("%s: err = %v, want a confirmation", c.id, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("an unconfirmed write reached the network: %v", f.actions())
	}
}

// Every DNSSEC write is listed-only with confirmation and carries the warning; get is an open read.
func TestRoute53DNSSECToolsAreListedOnlyWrites(t *testing.T) {
	seen := 0
	for _, d := range registered(t).Provider(Provider) {
		if !strings.HasPrefix(d.ID, "aws.route53.dnssec.") && !strings.HasPrefix(d.ID, "aws.route53.key-signing-key.") {
			continue
		}
		seen++
		if d.ID == "aws.route53.dnssec.get" {
			if d.RequiresToolAllowList || d.Risk.Effect != capability.EffectRead || d.Risk.Confirmation != capability.ConfirmationNone {
				t.Errorf("%s = %+v", d.ID, d)
			}
			continue
		}
		if !route53DNSSECWrite(d.ID) || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired ||
			!d.Risk.OpenWorld || d.Risk.DataSensitivity == "" || !strings.Contains(d.Description, "unreachable") {
			t.Errorf("%s = %+v", d.ID, d)
		}
		idem := capability.IdempotencyIdempotent
		if d.ID == "aws.route53.key-signing-key.delete" {
			idem = capability.IdempotencyNonIdempotent
		}
		if d.Risk.Idempotency != idem {
			t.Errorf("%s idempotency = %s, want %s", d.ID, d.Risk.Idempotency, idem)
		}
	}
	if seen != 7 {
		t.Errorf("seen %d DNSSEC tools", seen)
	}
	for _, id := range []string{"aws.route53.dnssec.disable", "aws.route53.key-signing-key.create"} {
		d := specOf(t, id).descriptor
		if !strings.Contains(d.Description, "registrar") || (id == "aws.route53.key-signing-key.create" && !strings.Contains(d.Description, "dnssec-route53.amazonaws.com")) {
			t.Errorf("%s description lacks its warning: %s", id, d.Description)
		}
	}
}

// Without a tools entry the DNSSEC writes are not offered, not by an effect, a prefix, or another tool; a
// connection that names them offers them.
func TestRoute53DNSSECWritesAreOnlyOfferedByName(t *testing.T) {
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	for name, c := range map[string]struct {
		conn   config.Connection
		reason config.Refusal
	}{
		"by every effect":         {config.Connection{Effects: all}, config.RefusalToolAllowList},
		"by default":              {config.Connection{}, ""},
		"by dnssec prefix":        {config.Connection{Tools: []string{"aws.route53.dnssec.*", "aws.route53.key-signing-key.*"}}, config.RefusalToolsList},
		"by route53 prefix":       {config.Connection{Tools: []string{"aws.route53.*"}}, config.RefusalToolsList},
		"by another route53 tool": {config.Connection{Tools: []string{"aws.route53.dnssec.get"}}, config.RefusalToolsList},
	} {
		f := direct(nil)
		e := newConnEnv(t, f, baseValues(), c.conn)
		for _, w := range dnssecWrites {
			_, err := e.invokeConfirmed(w.id, w.args, true)
			var notOffered *capability.UnsupportedError
			if !errorsAs(err, &notOffered) || (c.reason != "" && notOffered.Reason != c.reason) || len(f.requests) != 0 {
				t.Errorf("%s %s: %v, requests %v", name, w.id, err, f.actions())
			}
		}
		found, err := e.core.Search(application.SearchRequest{Provider: Provider, Group: "route53", Connection: "prod"})
		if err != nil {
			t.Fatalf("%s: search: %v", name, err)
		}
		for _, hit := range found.Operations {
			if route53DNSSECWrite(hit.ID) {
				t.Errorf("%s offers %s", name, hit.ID)
			}
		}
	}
	f := restAnswer(xmlReply(`<R ` + route53NS + `><ChangeInfo><Id>c</Id><Status>PENDING</Status><SubmittedAt>2026-01-02T03:04:05Z</SubmittedAt></ChangeInfo></R>`))
	e := newToolsEnv(t, f, baseValues(), []string{"aws.route53.dnssec.enable"})
	if _, err := e.invokeConfirmed("aws.route53.dnssec.enable", dnssecWrites[0].args, true); err != nil {
		t.Errorf("listed: %v", err)
	}
}
