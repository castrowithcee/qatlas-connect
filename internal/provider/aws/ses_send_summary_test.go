package aws

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func sendSummaryList(prefix string, n int) string {
	var out []string
	for i := range n {
		out = append(out, fmt.Sprintf(`"%s%d-%s@example.net"`, prefix, i, strings.Repeat("l", 60)))
	}
	return strings.Join(out, ",")
}

// Both texts name region, sender, the counts, the template or subject, the configuration set, the price, and
// that the send cannot be undone, and they reach the confirmation-required error without a network call.
func TestSESSendConfirmationTexts(t *testing.T) {
	bulk := `{"region":"` + testRegion + `","from_email_address":"sender@example.com","configuration_set_name":"default",` +
		`"default_content":{"template":{"template_name":"welcome","template_data":"{\"secret\":\"tdata\"}"}},` +
		`"bulk_email_entries":[{"destination":{"to_addresses":["a@example.com","b@example.com"]}},` +
		`{"destination":{"to_addresses":["c@example.com"],"cc_addresses":["d@example.com"],"bcc_addresses":["e@example.com"]}}]}`
	for _, c := range []struct {
		id, args string
		want     []string
	}{
		{sendTool, strings.Replace(sendSimple, `{"from`, `{"region":"`+testRegion+`","from`, 1), []string{"region " + testRegion, "sender@example.com", "3 recipients (To 1, Cc 1, Bcc 1)",
			"3 mails at 0.10 USD per 1,000 mails", "to@example.com, cc@example.com and 1 more", `Subject "Report"`,
			"Configuration set default", "Cannot be undone", "never retried automatically"}},
		{sendTool, `{"from_email_address":"s@example.com","destination":{"to_addresses":["a@example.com"]},` +
			`"content":{"template":{"template_arn":"` + sendTemplateARN + `","template_data":"tdata"}}}`,
			[]string{"the one region the connection allows", "1 recipients (To 1, Cc 0, Bcc 0)", "Template " + sendTemplateARN}},
		{sendBulkTool, bulk, []string{"region " + testRegion, "sender@example.com", "2 destinations, 5 recipients (To 3, Cc 1, Bcc 1)",
			"5 mails at 0.10 USD per 1,000 mails", "a@example.com, b@example.com and 3 more", "Template welcome",
			"Configuration set default", "Cannot be undone"}},
	} {
		f := restAnswer(reply{status: 200})
		got := confirmationSummary(t, sendEnv(t, f), c.id, c.args)
		for _, part := range append(c.want, "") {
			if !strings.Contains(got, part) {
				t.Errorf("%s: summary %q lacks %q", c.id, got, part)
			}
		}
		for _, banned := range []string{"tdata", "The report is ready"} {
			if strings.Contains(got, banned) {
				t.Errorf("%s: summary quotes %q", c.id, banned)
			}
		}
		if sendCount(f) != 0 {
			t.Errorf("%s: an unconfirmed call reached the network", c.id)
		}
	}
}

// Long lists, long values, and hostile characters keep the text under the core bound of 500 runes without a
// cut by the core, so price and counts always stand in it.
func TestSESSendConfirmationTextStaysBounded(t *testing.T) {
	long := strings.Repeat("x", 300)
	hostile := strings.Repeat(`\u202e\n`, 100)
	tpl := `"template":{"template_arn":"arn:aws:ses:` + long + `"}`
	simple := `{"region":"` + long + `","from_email_address":"` + long + `","configuration_set_name":"` + long +
		`","destination":{"to_addresses":[` + sendSummaryList("t", 20) + `],"cc_addresses":[` + sendSummaryList("c", 20) +
		`],"bcc_addresses":[` + sendSummaryList("b", 10) + `]},"content":{"simple":{"subject":{"data":"` + long + hostile + `"},"body":{"text":{"data":"a"}}}}}`
	tmpl := strings.Replace(simple, `"simple":{"subject":{"data":"`+long+hostile+`"},"body":{"text":{"data":"a"}}}`, tpl, 1)
	bulk := `{"region":"` + long + `","from_email_address":"` + long + `","configuration_set_name":"` + long + `","default_content":{` + tpl +
		`},"bulk_email_entries":[{"destination":{"to_addresses":[` + sendSummaryList("t", 25) + `]}},{"destination":{"to_addresses":[` +
		sendSummaryList("u", 25) + `]}}]}`
	for name, got := range map[string]string{"simple": sendSummary([]byte(simple)), "template": sendSummary([]byte(tmpl)),
		"bulk": sendBulkSummary([]byte(bulk))} {
		if n := utf8.RuneCountInString(got); n == 0 || n > 500 {
			t.Errorf("%s: %d runes: %q", name, n, got)
		}
		for _, part := range []string{"50 mails at 0.10 USD per 1,000 mails", "Cannot be undone", "never retried automatically", "and 48 more"} {
			if !strings.Contains(got, part) {
				t.Errorf("%s: %q lacks %q", name, got, part)
			}
		}
		if strings.ContainsAny(got, "\n‮") {
			t.Errorf("%s: hostile characters stay in %q", name, got)
		}
	}
	// The schema allows a region of 32 and a configuration set of 64 characters; the sender and the lists at
	// their limits are still longer than the text takes.
	valid := strings.Replace(strings.Replace(simple, `"region":"`+long, `"region":"`+"ab-"+strings.Repeat("a", 27)+"-1", 1),
		`"configuration_set_name":"`+long, `"configuration_set_name":"`+strings.Repeat("c", 64), 1)
	f := restAnswer(reply{status: 200})
	got := confirmationSummary(t, sendEnv(t, f), sendTool, valid)
	if n := utf8.RuneCountInString(got); n > 500 || !strings.HasSuffix(got, "….") && !strings.Contains(got, "Configuration set") {
		t.Errorf("core text: %d runes: %q", n, got)
	}
}

// An unreadable argument gives no text, and a call without recipients says so in numbers only.
func TestSESSendConfirmationTextUnreadable(t *testing.T) {
	for _, bad := range []string{`nope`, `{"destination":{"to_addresses":[1]}}`, `{"from_email_address":1}`} {
		if sendSummary([]byte(bad)) != "" {
			t.Errorf("%s gave a text", bad)
		}
	}
	if sendBulkSummary([]byte(`{"bulk_email_entries":[{"destination":{"cc_addresses":[1]}}]}`)) != "" {
		t.Error("an unreadable bulk argument gave a text")
	}
	if got := sendSummary([]byte(`{}`)); !strings.Contains(got, "0 recipients") || strings.Contains(got, "First recipients") {
		t.Errorf("summary = %q", got)
	}
}
