package aws

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
)

// The confirmation texts of the SES send tools. The core cuts a text above its bound from the end, so the
// numbers, the price, and the warning come first and the untrusted values come last, each cut short. The worst
// case of every limit below stays under the core bound.

const (
	sendSummaryShown   = 2  // recipient addresses named, the rest only counted
	sendSummaryAddress = 28 // runes of one address
	sendSummarySender  = 32
	sendSummarySubject = 40
	sendSummaryRef     = 64 // runes of a template name or ARN
	sendSummaryRegion  = 16
	sendSummaryName    = 16 // runes of one attachment name
	sendSummaryNames   = 1  // attachment names shown, the rest only counted
)

func init() {
	registerSummary("aws.ses.email.send", sendSummary)
	registerSummary("aws.ses.email.send-bulk", sendBulkSummary)
}

// sendSummaryValue cuts an untrusted value to max runes. Format and separator characters, which the core would
// expand to long escapes, become spaces like control characters do.
func sendSummaryValue(v string, max int) string {
	cut := false
	if r := []rune(v); len(r) > max {
		v, cut = string(r[:max]), true
	}
	v = summaryValue(strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, v))
	if cut {
		return v + "…"
	}
	return v
}

type sendDestination struct {
	To  []string `json:"to_addresses"`
	Cc  []string `json:"cc_addresses"`
	Bcc []string `json:"bcc_addresses"`
}

type sendTemplate struct {
	Name    string `json:"template_name"`
	ARN     string `json:"template_arn"`
	Content struct {
		Subject string `json:"subject"`
	} `json:"template_content"`
}

// sendCommon is what both tools name in the same way.
type sendCommon struct {
	Region    string `json:"region"`
	From      string `json:"from_email_address"`
	ConfigSet string `json:"configuration_set_name"`
}

// sendRecipients names the counts, the price, and the effect of a send of mails to the given destinations.
func sendRecipients(c sendCommon, kind string, dests []sendDestination) (head, first string) {
	var to, cc, bcc int
	var addrs []string
	for _, d := range dests {
		to, cc, bcc = to+len(d.To), cc+len(d.Cc), bcc+len(d.Bcc)
		for _, l := range [][]string{d.To, d.Cc, d.Bcc} {
			addrs = append(addrs, l...)
		}
	}
	n := to + cc + bcc
	head = " from " + sendSummaryValue(c.From, sendSummarySender) + " via SES " +
		summaryRegion(sendSummaryValue(c.Region, sendSummaryRegion)) + ": " + kind + strconv.Itoa(n) + " recipients (To " + strconv.Itoa(to) +
		", Cc " + strconv.Itoa(cc) + ", Bcc " + strconv.Itoa(bcc) + "), so " + strconv.Itoa(n) +
		" mails at 0.10 USD per 1,000 mails (attachments extra). Cannot be undone, never retried automatically."
	if n == 0 {
		return head, ""
	}
	shown := min(len(addrs), sendSummaryShown)
	for i := range shown {
		addrs[i] = sendSummaryValue(addrs[i], sendSummaryAddress)
	}
	first = " First recipients: " + strings.Join(addrs[:shown], ", ")
	if rest := len(addrs) - shown; rest > 0 {
		first += " and " + strconv.Itoa(rest) + " more"
	}
	return head, first + "."
}

func (t sendTemplate) text() string {
	switch {
	case t.Name != "":
		return " Template " + sendSummaryValue(t.Name, sendSummaryRef) + "."
	case t.ARN != "":
		return " Template " + sendSummaryValue(t.ARN, sendSummaryRef) + "."
	case t.Content.Subject != "":
		return " Inline template, subject \"" + sendSummaryValue(t.Content.Subject, sendSummarySubject) + "\"."
	}
	return ""
}

func (c sendCommon) configSet() string {
	if c.ConfigSet == "" {
		return ""
	}
	return " Configuration set " + sendSummaryValue(c.ConfigSet, sendSummaryAddress) + "."
}

func sendSummary(raw json.RawMessage) string {
	var a struct {
		sendCommon
		Destination sendDestination `json:"destination"`
		Content     struct {
			Simple struct {
				Subject struct {
					Data string `json:"data"`
				} `json:"subject"`
			} `json:"simple"`
			Template sendTemplate `json:"template"`
			Raw      struct {
				Data string `json:"data"`
			} `json:"raw"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	head, first := sendRecipients(a.sendCommon, "", []sendDestination{a.Destination})
	content := a.Content.Template.text()
	if a.Content.Raw.Data != "" {
		content = rawSummary(a.Content.Raw.Data)
	}
	if s := a.Content.Simple.Subject.Data; s != "" {
		content = " Subject \"" + sendSummaryValue(s, sendSummarySubject) + "\"."
	}
	return "Send one mail" + head + first + content + a.configSet()
}

func sendBulkSummary(raw json.RawMessage) string {
	var a struct {
		sendCommon
		Default struct {
			Template sendTemplate `json:"template"`
		} `json:"default_content"`
		Entries []struct {
			Destination sendDestination `json:"destination"`
		} `json:"bulk_email_entries"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	dests := make([]sendDestination, len(a.Entries))
	for i, e := range a.Entries {
		dests[i] = e.Destination
	}
	head, first := sendRecipients(a.sendCommon, strconv.Itoa(len(dests))+" destinations, ", dests)
	return "Send template mail per destination" + head + first + a.Default.Template.text() + a.configSet()
}

// rawSummary names From header, subject, and attachments of raw MIME content, each cut short. The content
// itself never enters the text; one that cannot be read is named as such, and the guard refuses it.
func rawSummary(data string) string {
	m, err := parseRawMessage(data)
	if err != nil {
		return " Raw MIME content, unreadable."
	}
	out := " Raw MIME from \"" + sendSummaryValue(m.from, 20) + "\", subject \"" +
		sendSummaryValue(m.subject, 24) + "\", " + strconv.Itoa(len(m.attachments)) + " attachments"
	shown := min(len(m.attachments), sendSummaryNames)
	if shown == 0 {
		return out + "."
	}
	names := make([]string, shown)
	for i := range shown {
		names[i] = "\"" + sendSummaryValue(m.attachments[i], sendSummaryName) + "\""
	}
	out += ": " + strings.Join(names, ", ")
	if rest := len(m.attachments) - shown; rest > 0 {
		out += " and " + strconv.Itoa(rest) + " more"
	}
	return out + "."
}
