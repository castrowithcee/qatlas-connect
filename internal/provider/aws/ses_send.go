package aws

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

// The SES send tools. Everything here runs before any I/O except the pace and the wording of an unclear
// outcome: a send is never repeated, so a failure that may have delivered says so.

const (
	// sesMaxRecipients is the SES limit of recipients per message, To, Cc, and Bcc together (SES developer
	// guide, Service quotas). A bulk call counts every destination, as the SES guide asks for groups of 50.
	sesMaxRecipients = 50
	// sesMaxMessage is the largest message SES v2 accepts, 40 MB after base64 encoding (SES developer guide,
	// Service quotas). The arguments as JSON stand in for the message: attachments in them are base64 already.
	sesMaxMessage = 40_000_000
	// maxAddress is the longest address SMTP carries (RFC 5321, 4.5.3.1.3: a path of 256 octets with its
	// angle brackets), maxLocalPart the longest local part (4.5.3.1.1).
	maxAddress, maxLocalPart = 254, 64

	// deliveryInterval spaces the sends of one connection and region. It is the sending rate of an account in
	// the SES sandbox, one mail per second (SES developer guide, Service quotas), and so never more than any
	// account may send; a faster account answers a burst with a rate limit that is not repeated.
	deliveryInterval = time.Second

	sesUnclear = "; the mail may have gone out all the same, so check before sending it again: it is never repeated automatically"
)

// deliveries are the hand-written delivery steps, by the name a curation table gives them, each with the
// text a failure gets when the delivery may have happened.
var deliveries = map[string]string{"ses-mail": sesUnclear}

// deliveryLimiters hold the pace of every delivery, connection, and region; tests replace them.
var deliveryLimiters = ratelimit.NewRegistry(deliveryInterval)

func deliveryKey(delivery, connection, region string) string {
	return "aws-delivery|" + delivery + "|" + connection + "|" + region
}

// unclear adds the text of the delivery to a failure that leaves open whether the call delivered: a request
// that may have reached AWS, a server-side failure, and an answer after a success that could not be read.
func (s toolSpec) unclear(failure *provider.Error) *provider.Error {
	text, ok := deliveries[s.delivery]
	if !ok || failure == nil {
		return failure
	}
	if failure.MayHaveArrived() || (failure.Original != nil && failure.Original.Status >= 500) ||
		(failure.Class == provider.ClassInvalidResponse && failure.Original == nil) {
		failure.Message += text
	}
	return failure
}

var (
	// atom is a dot-atom of RFC 5322 (3.2.3): atext runs joined by single dots.
	atom = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+(\\.[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+)*$")
	// plainName is a display name without quotes: printable ASCII without the specials of RFC 5322 that
	// would change how a header reads it. quotedName is one in quotes, without a quote or backslash inside.
	plainName  = regexp.MustCompile(`^[ !#-'*+\-./0-9=?A-Z^-~]+$`)
	quotedName = regexp.MustCompile(`^"[ !#-\[\]-~]*"$`)
)

// validMailbox reports a bare address local@domain: a dot-atom local part and a domain name, 7-bit ASCII as
// SES requires (a Unicode domain must be Punycode).
func validMailbox(s string) bool {
	at := strings.LastIndexByte(s, '@')
	if at < 1 || len(s) > maxAddress {
		return false
	}
	local, domain := s[:at], s[at+1:]
	return len(local) <= maxLocalPart && atom.MatchString(local) && validDomain(domain)
}

// validAddress reports a bare address or Display Name <local@domain>. A display name is plain printable
// ASCII without specials, or quoted; a name of other characters must be MIME encoded-word, which is plain.
func validAddress(s string) bool {
	if !strings.HasSuffix(s, ">") {
		return validMailbox(s)
	}
	open := strings.LastIndexByte(s, '<')
	if open < 0 {
		return false
	}
	name := strings.TrimRight(s[:open], " ")
	if name == "" || name != strings.TrimLeft(name, " ") || !(plainName.MatchString(name) || quotedName.MatchString(name)) {
		return false
	}
	return validMailbox(s[open+1 : len(s)-1])
}

// checkSESSend judges the arguments of a send before any I/O: every address has the form of one, the
// recipients of the call are at most sesMaxRecipients, a template ARN names a template of the bound account
// in an allowed region, and the message stays within the SES size.
func checkSESSend(b binding, _ string, args map[string]any) error {
	if raw, err := json.Marshal(args); err != nil || len(raw) > sesMaxMessage {
		return &provider.InvalidRequestError{Message: "the message is larger than the 40 MB SES takes"}
	}
	addresses := func(name string, value any, display bool) error {
		var list []any
		switch v := value.(type) {
		case nil:
			return nil
		case string:
			list = []any{v}
		case []any:
			list = v
		}
		for _, item := range list {
			text, _ := item.(string)
			if display && !validAddress(text) {
				return &provider.InvalidRequestError{Message: name + " must hold addresses of the form local@domain or Display Name <local@domain>"}
			}
			if !display && !validMailbox(text) {
				return &provider.InvalidRequestError{Message: name + " must be an address of the form local@domain"}
			}
		}
		return nil
	}
	for _, c := range []struct {
		name    string
		display bool
	}{{"from_email_address", true}, {"reply_to_addresses", true}, {"feedback_forwarding_email_address", false}} {
		if err := addresses(c.name, args[c.name], c.display); err != nil {
			return err
		}
	}
	destinations := []any{}
	if d, ok := args["destination"]; ok {
		destinations = append(destinations, d)
	}
	entries, _ := args["bulk_email_entries"].([]any)
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		destinations = append(destinations, entry["destination"])
	}
	recipients := 0
	for _, d := range destinations {
		destination, _ := d.(map[string]any)
		for _, list := range []string{"to_addresses", "cc_addresses", "bcc_addresses"} {
			if err := addresses("destination."+list, destination[list], true); err != nil {
				return err
			}
			items, _ := destination[list].([]any)
			recipients += len(items)
		}
	}
	if recipients > sesMaxRecipients {
		return &provider.InvalidRequestError{Message: "a call sends to at most 50 recipients, To, Cc, and Bcc of every destination together; split the recipients over several calls"}
	}
	for _, content := range []string{"content", "default_content"} {
		outer, _ := args[content].(map[string]any)
		template, _ := outer["template"].(map[string]any)
		if arn, set := template["template_arn"]; set {
			text, _ := arn.(string)
			if err := checkSESTemplateARN(b, content+".template.template_arn", text); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkSESTemplateARN accepts only the ARN of an SES template in the bound account and an allowed region.
func checkSESTemplateARN(b binding, name, value string) error {
	if err := checkARNRegion(b, false, name, value); err != nil {
		return err
	}
	parts := strings.SplitN(value, ":", 6)
	template, isTemplate := strings.CutPrefix(parts[5], "template/")
	if parts[2] != "ses" || !isTemplate || template == "" || strings.Contains(template, "/") {
		return &provider.InvalidRequestError{Message: name + " must be the ARN of an SES template"}
	}
	return nil
}
