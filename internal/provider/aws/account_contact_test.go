package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var accountContactTools = map[string]capability.Effect{
	"aws.account.information.get":          capability.EffectRead,
	"aws.account.name.put":                 capability.EffectUpdate,
	"aws.account.contact-information.get":  capability.EffectRead,
	"aws.account.contact-information.put":  capability.EffectUpdate,
	"aws.account.alternate-contact.get":    capability.EffectRead,
	"aws.account.alternate-contact.put":    capability.EffectUpdate,
	"aws.account.alternate-contact.delete": capability.EffectDelete,
	"aws.account.primary-email.get":        capability.EffectRead,
}

const (
	contactInfoArgs = `{"contact_information":{"full_name":"Example Person","address_line1":"1 Example Street","address_line2":"Floor 2","address_line3":"Room 3","city":"Example City","state_or_region":"Example State","district_or_county":"Example County","postal_code":"00000","country_code":"US","phone_number":"+1 5550","company_name":"Example Corp","website_url":"https://example.com"}}`
	contactInfoBody = `{"ContactInformation":{"FullName":"Example Person","AddressLine1":"1 Example Street","AddressLine2":"Floor 2","AddressLine3":"Room 3","City":"Example City","StateOrRegion":"Example State","DistrictOrCounty":"Example County","PostalCode":"00000","CountryCode":"US","PhoneNumber":"+1 5550","CompanyName":"Example Corp","WebsiteUrl":"https://example.com"}}`
)

// The eight tools carry the risk of the contract: reads are safe, writes need confirmation, only delete is not
// repeatable, and every result is classified as contact data.
func TestAccountContactToolsRisk(t *testing.T) {
	seen := 0
	for _, s := range generatedSpecs {
		d := s.descriptor
		effect, want := accountContactTools[d.ID]
		if !want {
			continue
		}
		seen++
		idem, confirmation := capability.IdempotencyIdempotent, capability.ConfirmationRequired
		switch effect {
		case capability.EffectRead:
			idem, confirmation = capability.IdempotencySafe, capability.ConfirmationNone
		case capability.EffectDelete:
			idem = capability.IdempotencyNonIdempotent
		}
		if d.Risk.Effect != effect || d.Risk.Idempotency != idem || d.Risk.Confirmation != confirmation || !d.Risk.OpenWorld ||
			d.Risk.DataSensitivity != "aws-account-contact-data" || d.RequiresToolAllowList || d.Group != "account" {
			t.Errorf("%s descriptor = %+v", d.ID, d)
		}
	}
	if seen != len(accountContactTools) {
		t.Fatalf("%d contact tools, want %d", seen, len(accountContactTools))
	}
}

// The confirmation text is the description: it names the type and recipient of the notices, singles out the
// security contact, and says that the contact data applies to billing and contact.
func TestAccountContactDescriptions(t *testing.T) {
	text := map[string]string{}
	for _, s := range generatedSpecs {
		text[s.descriptor.ID] = s.descriptor.Description
	}
	for id, parts := range map[string][]string{
		"aws.account.alternate-contact.put":    {"BILLING", "OPERATIONS", "SECURITY", "email_address", "security notices", "sends no mail itself"},
		"aws.account.alternate-contact.delete": {"SECURITY", "no longer reach"},
		"aws.account.contact-information.put":  {"billing and contact", "in full"},
		"aws.account.contact-information.get":  {"personal data", "not logged"},
	} {
		for _, part := range parts {
			if !strings.Contains(text[id], part) {
				t.Errorf("%s description lacks %q", id, part)
			}
		}
	}
}

// The e-mail change of the root user is no tool: only the read of the address is registered.
func TestAccountPrimaryEmailUpdateIsNotATool(t *testing.T) {
	for _, s := range generatedSpecs {
		id := s.descriptor.ID
		if strings.HasPrefix(id, "aws.account.") && strings.Contains(id, "email") && id != "aws.account.primary-email.get" {
			t.Errorf("%s is registered", id)
		}
		if s.rest != nil && strings.Contains(s.rest.URI, "PrimaryEmailUpdate") {
			t.Errorf("%s sends %s", id, s.rest.URI)
		}
	}
}

// Information and name: the answer carries the four members and the name goes out as the model names it.
func TestAccountInformationAndName(t *testing.T) {
	f := sesFake(func(r sent) reply {
		if r.path == "/getAccountInformation" {
			return ok(`{"AccountId":"` + testAccount + `","AccountName":"Example Account","AccountCreatedDate":"2026-01-02T03:04:05Z","AccountState":"ACTIVE"}`)
		}
		return ok(`{}`)
	})
	e := accountEnv(t, f)
	text, err := e.invoke("aws.account.information.get", `{"account_id":"`+testAccount+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	if got["account_id"] != testAccount || got["account_name"] != "Example Account" || got["account_state"] != "ACTIVE" ||
		!strings.HasPrefix(got["account_created_date"].(string), "2026-01-02T03:04:05") {
		t.Errorf("result = %v", got)
	}
	var unconfirmed *application.ConfirmationRequiredError
	before := len(f.requests)
	if _, err := e.invoke("aws.account.name.put", `{"account_name":"Renamed"}`); !errorsAs(err, &unconfirmed) || len(f.requests) != before {
		t.Fatalf("name without confirmation: %v", err)
	}
	if _, err := e.invokeConfirmed("aws.account.name.put", `{"account_name":"Renamed"}`, true); err != nil {
		t.Fatal(err)
	}
	calls := restCalls(f)
	if len(calls) != 2 || calls[1].path != "/putAccountName" || calls[1].host != "account.us-east-1.amazonaws.com" || !jsonEqual(calls[1].body, `{"AccountName":"Renamed"}`) {
		t.Errorf("calls = %+v", calls)
	}
}

// The primary contact is written with every member and read back with every member and the phone status.
func TestAccountContactInformationRoundTrip(t *testing.T) {
	f := sesFake(func(r sent) reply {
		if r.path == "/getContactInformation" {
			return ok(`{"ContactInformation":` + strings.TrimSuffix(strings.TrimPrefix(contactInfoBody, `{"ContactInformation":`), `}`) + `,"VerificationStatus":"VERIFIED"}`)
		}
		return ok(`{}`)
	})
	e := accountEnv(t, f)
	var unconfirmed *application.ConfirmationRequiredError
	if _, err := e.invoke("aws.account.contact-information.put", contactInfoArgs); !errorsAs(err, &unconfirmed) || len(f.requests) != 0 {
		t.Fatalf("put without confirmation: %v", err)
	}
	if _, err := e.invokeConfirmed("aws.account.contact-information.put", contactInfoArgs, true); err != nil {
		t.Fatal(err)
	}
	text, err := e.invoke("aws.account.contact-information.get", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	info, _ := got["contact_information"].(map[string]any)
	for _, key := range []string{"full_name", "address_line1", "address_line2", "address_line3", "city", "state_or_region", "district_or_county",
		"postal_code", "country_code", "phone_number", "company_name", "website_url"} {
		if s, _ := info[key].(string); s == "" {
			t.Errorf("result lacks %s: %v", key, got)
		}
	}
	calls := restCalls(f)
	if got["verification_status"] != "VERIFIED" || len(calls) != 2 || calls[0].path != "/putContactInformation" || !jsonEqual(calls[0].body, contactInfoBody) ||
		calls[1].path != "/getContactInformation" {
		t.Errorf("result %v calls %+v", got, calls)
	}
}

// Each type of alternate contact is written, read, and deleted under its own type; delete does not repeat.
func TestAccountAlternateContactPerType(t *testing.T) {
	for _, typ := range []string{"BILLING", "OPERATIONS", "SECURITY"} {
		f := sesFake(func(r sent) reply {
			if r.path == "/getAlternateContact" {
				return ok(`{"AlternateContact":{"Name":"Example Person","Title":"Lead","EmailAddress":"contact!@example.com","PhoneNumber":"+1 5550","AlternateContactType":"` + typ + `"}}`)
			}
			return ok(`{}`)
		})
		e := accountEnv(t, f)
		put := `{"alternate_contact_type":"` + typ + `","name":"Example Person","title":"Lead","email_address":"contact!@example.com","phone_number":"+1 5550"}`
		if _, err := e.invokeConfirmed("aws.account.alternate-contact.put", put, true); err != nil {
			t.Fatalf("%s put: %v", typ, err)
		}
		text, err := e.invoke("aws.account.alternate-contact.get", `{"alternate_contact_type":"`+typ+`"}`)
		if err != nil {
			t.Fatalf("%s get: %v", typ, err)
		}
		contact, _ := decodeResult(t, text)["alternate_contact"].(map[string]any)
		if contact["name"] != "Example Person" || contact["alternate_contact_type"] != typ || contact["email_address"] != "contact!@example.com" {
			t.Errorf("%s contact = %v", typ, contact)
		}
		var unconfirmed *application.ConfirmationRequiredError
		del := `{"alternate_contact_type":"` + typ + `"}`
		if _, err := e.invoke("aws.account.alternate-contact.delete", del); !errorsAs(err, &unconfirmed) {
			t.Fatalf("%s delete without confirmation: %v", typ, err)
		}
		if _, err := e.invokeConfirmed("aws.account.alternate-contact.delete", del, true); err != nil {
			t.Fatalf("%s delete: %v", typ, err)
		}
		calls := restCalls(f)
		if len(calls) != 3 || calls[0].path != "/putAlternateContact" ||
			!jsonEqual(calls[0].body, `{"AlternateContactType":"`+typ+`","Name":"Example Person","Title":"Lead","EmailAddress":"contact!@example.com","PhoneNumber":"+1 5550"}`) ||
			calls[1].path != "/getAlternateContact" || !jsonEqual(calls[1].body, `{"AlternateContactType":"`+typ+`"}`) ||
			calls[2].path != "/deleteAlternateContact" || !jsonEqual(calls[2].body, `{"AlternateContactType":"`+typ+`"}`) {
			t.Errorf("%s calls = %+v", typ, calls)
		}
	}
}

// The root user address is read for the bound account only.
func TestAccountPrimaryEmailGet(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{"PrimaryEmail":"root!@example.com"}`) })
	e := accountEnv(t, f)
	text, err := e.invoke("aws.account.primary-email.get", `{"account_id":"`+testAccount+`"}`)
	calls := restCalls(f)
	if err != nil || decodeResult(t, text)["primary_email"] != "root!@example.com" || len(calls) != 1 || calls[0].path != "/getPrimaryEmail" ||
		!jsonEqual(calls[0].body, `{"AccountId":"`+testAccount+`"}`) {
		t.Errorf("result %q err %v calls %+v", text, err, calls)
	}
}

// Another account, a malformed one, a missing required member, and bad values are refused before any I/O.
func TestAccountContactRefusalsBeforeAnyIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := accountEnv(t, f)
	other := `"account_id":"` + otherAccount + `"`
	for _, c := range []struct{ tool, args string }{
		{"aws.account.information.get", `{` + other + `}`},
		{"aws.account.name.put", `{"account_name":"x",` + other + `}`},
		{"aws.account.contact-information.get", `{` + other + `}`},
		{"aws.account.contact-information.put", strings.Replace(contactInfoArgs, `{"contact_information"`, `{`+other+`,"contact_information"`, 1)},
		{"aws.account.alternate-contact.get", `{"alternate_contact_type":"BILLING",` + other + `}`},
		{"aws.account.alternate-contact.put", `{"alternate_contact_type":"SECURITY","name":"a","title":"a","email_address":"a!@b.cc","phone_number":"1",` + other + `}`},
		{"aws.account.alternate-contact.delete", `{"alternate_contact_type":"BILLING",` + other + `}`},
		{"aws.account.primary-email.get", `{` + other + `}`},
		{"aws.account.primary-email.get", `{}`},
		{"aws.account.information.get", `{"account_id":"1"}`},
		{"aws.account.alternate-contact.get", `{"alternate_contact_type":"LEGAL"}`},
		{"aws.account.alternate-contact.put", `{"alternate_contact_type":"SECURITY","name":"a","title":"a","email_address":"not an address","phone_number":"1"}`},
		{"aws.account.alternate-contact.put", `{"alternate_contact_type":"SECURITY","name":"` + strings.Repeat("a", 65) + `","title":"a","email_address":"a!@b.cc","phone_number":"1"}`},
		{"aws.account.contact-information.put", `{"contact_information":{"full_name":"a"}}`},
		{"aws.account.name.put", `{}`},
	} {
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err == nil {
			t.Errorf("%s %s was accepted", c.tool, c.args)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

// The errors of the operations map to the classes of awserr and keep the code in Original, without the text of
// the provider, which can hold contact data.
func TestAccountContactErrors(t *testing.T) {
	for _, c := range []struct {
		code   string
		status int
		class  provider.Class
	}{
		{"ResourceNotFoundException", 404, provider.ClassNotFound},
		{"ValidationException", 400, provider.ClassProviderError},
		{"ConflictException", 409, provider.ClassProviderError},
		{"AccessDeniedException", 403, provider.ClassPermission},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := sesFake(func(sent) reply {
				return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code}, body: `{"message":"secret text"}`}
			})
			e := accountEnv(t, f)
			_, err := e.invoke("aws.account.alternate-contact.get", `{"alternate_contact_type":"SECURITY"}`)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil || len(failed.Original.Entries) == 0 ||
				failed.Original.Entries[0].Code != c.code || strings.Contains(err.Error(), "secret text") {
				t.Fatalf("error = %v (%+v)", err, failed)
			}
		})
	}
}

// A value of the answer is untrusted: a long name is cut or refused, never passed on unbounded.
func TestAccountContactAnswerIsBounded(t *testing.T) {
	long := strings.Repeat("x", 5000)
	f := sesFake(func(sent) reply {
		return ok(`{"ContactInformation":{"FullName":"` + long + `","AddressLine1":"a","City":"a","PostalCode":"a","CountryCode":"US","PhoneNumber":"+1"}}`)
	})
	text, err := accountEnv(t, f).invoke("aws.account.contact-information.get", `{}`)
	if err == nil && strings.Contains(text, long) {
		t.Errorf("an unbounded name was passed on")
	}
}
