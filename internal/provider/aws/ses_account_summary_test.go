package aws

import (
	"strings"
	"testing"
)

// Each text names region and new value, and reaches the confirmation-required error without a network call.
func TestSESAccountConfirmationTexts(t *testing.T) {
	r := `"region":"` + testRegion + `"`
	for _, c := range []struct {
		id, args string
		want     []string
		not      []string
	}{
		{"aws.ses.account.put-details", `{` + r + `,"mail_type":"TRANSACTIONAL","website_url":"https://example.com","contact_language":"EN","production_access_enabled":true,` +
			`"additional_contact_email_addresses":["a@example.com","b@example.com","c@example.com","d@example.com"]}`,
			[]string{testRegion, "TRANSACTIONAL", "https://example.com", "EN", "4 additional", "a@example.com", "b@example.com", "and 2 more",
				"asks for production access", "AWS reviews the request", "can take a while"},
			[]string{"c@example.com", "d@example.com", "does not ask"}},
		{"aws.ses.account.put-details", `{"mail_type":"MARKETING","website_url":"https://example.com"}`,
			[]string{"the one region the connection allows", "MARKETING", "No additional contact", "does not ask for production access"}, nil},
		{"aws.ses.account.put-sending-attributes", `{` + r + `,"sending_enabled":false}`,
			[]string{testRegion, "off", "stops at once"}, nil},
		{"aws.ses.account.put-sending-attributes", `{` + r + `,"sending_enabled":true}`,
			[]string{testRegion, "on."}, []string{"stops"}},
		{"aws.ses.account.put-suppression-attributes", `{` + r + `,"suppressed_reasons":["BOUNCE","COMPLAINT"]}`,
			[]string{testRegion, "BOUNCE, COMPLAINT"}, nil},
		{"aws.ses.account.put-dedicated-ip-warmup-attributes", `{` + r + `,"auto_warmup_enabled":true}`,
			[]string{testRegion, "warm-up", "on."}, nil},
		{"aws.ses.account.put-vdm-attributes", `{` + r + `,"vdm_attributes":{"vdm_enabled":"ENABLED","guardian_attributes":{"optimized_shared_delivery":"ENABLED"}}}`,
			[]string{testRegion, "ENABLED", "costs extra"}, nil},
		{"aws.ses.account.put-vdm-attributes", `{` + r + `,"vdm_attributes":{"vdm_enabled":"DISABLED"}}`,
			[]string{testRegion, "DISABLED"}, []string{"costs extra"}},
	} {
		f := restAnswer(reply{status: 200})
		e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
		got := confirmationSummary(t, e, c.id, c.args)
		for _, part := range c.want {
			if !strings.Contains(got, part) {
				t.Errorf("%s: summary %q lacks %q", c.id, got, part)
			}
		}
		for _, part := range c.not {
			if strings.Contains(got, part) {
				t.Errorf("%s: summary %q contains %q", c.id, got, part)
			}
		}
		if len(f.requests) != 0 {
			t.Errorf("%s: an unconfirmed call reached the network", c.id)
		}
	}
}

func TestSESAccountConfirmationTextUnreadable(t *testing.T) {
	if accountDetailsSummary([]byte(`{"mail_type":1}`)) != "" || accountVdmSummary([]byte(`nope`)) != "" {
		t.Error("an unreadable argument gave a text")
	}
}
