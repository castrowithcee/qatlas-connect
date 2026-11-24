package aws

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"
)

const rawTo = `"from_email_address":"sender@example.com","destination":{"to_addresses":["to@example.com"]}`

func rawB64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func rawArgs(mime string) string {
	return `{` + rawTo + `,"content":{"raw":{"data":"` + rawB64(mime) + `"}}}`
}

const rawPlain = "From: Sender <sender@example.com>\r\nSubject: Report\r\n\r\nbody-secret\r\n"

func rawMultipart(headers string, parts ...string) string {
	out := "From: sender@example.com\r\nSubject: Report\r\n" + headers + "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n"
	for _, p := range parts {
		out += "--b1\r\n" + p + "\r\n"
	}
	return out + "--b1--\r\n"
}

func rawAttachment(name string) string {
	return "Content-Type: text/plain\r\nContent-Disposition: attachment; filename=\"" + name + "\"\r\n\r\ndata-secret\r\n"
}

// A readable message goes out as one request that carries the content as given.
func TestSESSendRawValid(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{"MessageId":"m-raw"}`) })
	e := sendEnv(t, f)
	for _, m := range []string{rawPlain, rawMultipart("", "Content-Type: text/plain\r\n\r\nhello\r\n", rawAttachment("a.txt"))} {
		if _, err := e.invokeConfirmed(sendTool, rawArgs(m), true); err != nil {
			t.Errorf("valid raw content: %v", err)
		}
	}
	calls := restCalls(f)
	if len(calls) != 2 || !strings.Contains(calls[0].body, `"Raw":{"Data":"`+rawB64(rawPlain)+`"}`) {
		t.Errorf("calls = %+v", calls)
	}
}

// Every refusal comes before any I/O, and no error quotes the content.
func TestSESSendRawRefusals(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{"MessageId":"m-1"}`) })
	e := sendEnv(t, f)
	hdr := func(h string) string { return h + "Subject: Report\r\n\r\nbody-secret\r\n" }
	var many []string
	for range sesMaxAttachments + 1 {
		many = append(many, rawAttachment("n.txt"))
	}
	nested := "Content-Type: multipart/mixed; boundary=\"b2\"\r\n\r\n--b2\r\n" + rawAttachment("x.txt") + "\r\n" + "--b2\r\n" + rawAttachment("y.txt") + "\r\n--b2--\r\n"
	var tenAndNested []string
	for range sesMaxAttachments - 1 {
		tenAndNested = append(tenAndNested, rawAttachment("n.txt"))
	}
	tenAndNested = append(tenAndNested, nested)
	for name, c := range map[string]string{
		"bcc":              rawArgs(hdr("From: sender@example.com\r\nBcc: x@example.net\r\n")),
		"bcc case":         rawArgs(hdr("From: sender@example.com\r\nBCC: x@example.net\r\n")),
		"return-path":      rawArgs(hdr("From: sender@example.com\r\nReturn-Path: <x@example.net>\r\n")),
		"return-path case": rawArgs(hdr("From: sender@example.com\r\nreturn-PATH: <x@example.net>\r\n")),
		"no from":          rawArgs(hdr("")),
		"empty from":       rawArgs(hdr("From:\r\n")),
		"two from":         rawArgs(hdr("From: a@example.com\r\nFrom: b@example.com\r\n")),
		"two from case":    rawArgs(hdr("From: a@example.com\r\nfrom: b@example.com\r\n")),
		"folded from":      rawArgs(hdr("From: a@example.com\r\nSubject: x\r\n\tfolded\r\nFROM: b@example.com\r\n")),
		"folded bcc":       rawArgs(hdr("From: a@example.com\r\nBcc: x@example.net,\r\n y@example.net\r\n")),
		"not parsable":     rawArgs("this is no header\r\n\r\nbody"),
		"not base64":       `{` + rawTo + `,"content":{"raw":{"data":"%%%"}}}`,
		"empty":            rawArgs(""),
		"no boundary":      rawArgs("From: a@example.com\r\nContent-Type: multipart/mixed\r\n\r\nbody"),
		"broken parts":     rawArgs("From: a@example.com\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nbad header\r\n\r\nx\r\n--b--\r\n"),
		"too many":         rawArgs(rawMultipart("", many...)),
		"nested counted":   rawArgs(rawMultipart("", tenAndNested...)),
		"too large":        `{` + rawTo + `,"content":{"raw":{"data":"` + strings.Repeat("A", sesMaxRawBase64+4) + `"}}}`,
		"with simple": `{` + rawTo + `,"content":{"raw":{"data":"` + rawB64(rawPlain) +
			`"},"simple":{"subject":{"data":"a"},"body":{"text":{"data":"a"}}}}}`,
		"with template": `{` + rawTo + `,"content":{"raw":{"data":"` + rawB64(rawPlain) + `"},"template":{"template_name":"t"}}}`,
	} {
		_, err := e.invokeConfirmed(sendTool, c, true)
		if err == nil {
			t.Errorf("%s was accepted", name)
		} else if strings.Contains(err.Error(), "body-secret") || strings.Contains(err.Error(), rawB64(rawPlain)) {
			t.Errorf("%s: error quotes the content: %v", name, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
	// Exactly the cap of attachments passes, nested ones counted too.
	if _, err := e.invokeConfirmed(sendTool, rawArgs(rawMultipart("", many[:sesMaxAttachments]...)), true); err != nil {
		t.Errorf("attachment cap: %v", err)
	}
}

// The confirmation names sender, recipients, subject, and attachments, cut and cleaned, and never the content.
func TestSESSendRawConfirmationText(t *testing.T) {
	m := rawMultipart("", "Content-Type: text/plain\r\n\r\nbody-secret\r\n", rawAttachment("evil\u202e\tname.txt"), rawAttachment("report.pdf"), rawAttachment("c.txt"))
	m = strings.Replace(m, "Subject: Report", "Subject: =?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte("Hi‮ there "+strings.Repeat("s", 80)))+"?=", 1)
	f := restAnswer(reply{status: 200})
	got := confirmationSummary(t, sendEnv(t, f), sendTool, rawArgs(m))
	for _, part := range []string{"sender@example.com", "First recipients: to@example.com", "Raw MIME from sender@example.com,",
		`subject "Hi  there `, "…", `3 attachments: "evil  name.txt", "report.pdf", "c.txt".`, "Cannot be undone"} {
		if !strings.Contains(got, part) {
			t.Errorf("summary %q lacks %q", got, part)
		}
	}
	if strings.ContainsAny(got, "\t\n‮") || strings.Contains(got, "body-secret") || strings.Contains(got, "data-secret") ||
		strings.Contains(got, rawB64(rawPlain)) || utf8.RuneCountInString(got) > 500 {
		t.Errorf("summary = %q", got)
	}
	if sendCount(f) != 0 {
		t.Error("an unconfirmed call reached the network")
	}
	// At the limits of every shown value the text stays under the core bound.
	long := strings.Repeat("x", 300)
	worst := strings.Replace(rawMultipart("", rawAttachment(long), rawAttachment(long), rawAttachment(long)), "sender@example.com", long+"@example.com", 1)
	worst = strings.Replace(worst, "Subject: Report", "Subject: "+long, 1)
	args := `{"region":"` + strings.Repeat("r", 32) + `","from_email_address":"` + long + `","configuration_set_name":"` + strings.Repeat("c", 64) +
		`","destination":{"to_addresses":[` + sendSummaryList("t", 50) + `]},"content":{"raw":{"data":"` + rawB64(worst) + `"}}}`
	if n := utf8.RuneCountInString(sendSummary([]byte(args))); n > 500 {
		t.Errorf("worst case has %d runes", n)
	}
}

// A long display name cannot hide the sender's address: the address is named in full beside the cut name.
func TestSESSendRawConfirmationNamesTheSenderAddress(t *testing.T) {
	m := strings.Replace(rawPlain, "From: Sender <sender@example.com>",
		"From: \"Your Bank Customer Service Department\" <other@example.org>", 1)
	got := sendSummary([]byte(rawArgs(m)))
	if !strings.Contains(got, `Raw MIME from other@example.org ("Your Bank Customer S…")`) {
		t.Errorf("summary = %q", got)
	}
}

// Attachment names fill the room the core bound leaves; only the names that do not fit are counted.
func TestSESSendRawConfirmationNamesAttachmentsUpToTheBound(t *testing.T) {
	var short, parts []string
	for i := range sesMaxAttachments {
		short = append(short, rawAttachment(string(rune('a'+i))+".txt"))
		parts = append(parts, rawAttachment("file-"+strings.Repeat(string(rune('a'+i)), 12)+".txt"))
	}
	few := sendSummary([]byte(rawArgs(rawMultipart("", short...))))
	if !strings.Contains(few, `10 attachments: "a.txt", "b.txt",`) || !strings.Contains(few, `"j.txt".`) ||
		strings.Contains(few, "more") || utf8.RuneCountInString(few) > 500 {
		t.Errorf("short summary = %q", few)
	}
	m := rawMultipart("", parts...)
	long := strings.Repeat("x", 300)
	args := `{"region":"` + strings.Repeat("r", 32) + `","from_email_address":"` + long + `","configuration_set_name":"` +
		strings.Repeat("c", 64) + `","destination":{"to_addresses":[` + sendSummaryList("t", 50) + `]},"content":{"raw":{"data":"` +
		rawB64(m) + `"}}}`
	got := sendSummary([]byte(args))
	n := utf8.RuneCountInString(got)
	if n > 500 || !strings.Contains(got, `"file-aaaaaaaaaaa…"`) || !strings.Contains(got, "more.") {
		t.Errorf("full summary (%d runes) = %q", n, got)
	}
	if n < 500-len(`, "file-aaaaaaaaaaa…" and 9 more.`) {
		t.Errorf("summary leaves room for another name: %d runes", n)
	}
}

// A timeout after a raw send is not repeated and says the mail may have gone out.
func TestSESSendRawTimeoutIsNotRepeated(t *testing.T) {
	f := sesFake(func(sent) reply { return reply{err: context.DeadlineExceeded} })
	e := sendEnv(t, f)
	_, err := e.invokeConfirmed(sendTool, rawArgs(rawPlain), true)
	if err == nil || !strings.Contains(err.Error(), "may have gone out") || sendCount(f) != 1 {
		t.Errorf("error = %v after %d requests", err, sendCount(f))
	}
}
