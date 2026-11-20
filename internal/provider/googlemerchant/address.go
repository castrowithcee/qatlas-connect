package googlemerchant

import (
	"net/url"
	"strings"
	"unicode"
)

// checkHTTPSAddress refuses an address that sends customers somewhere unless it is an absolute https URL with
// a host and without user information. Placeholders such as {id} in the path or query of a URI template
// pass. The address is text handed to Google; it is never fetched.
func checkHTTPSAddress(name, raw string) error {
	refused := invalidArgument(name + " must be an https address with a host and without user information")
	// A browser reads a backslash as a slash, so it could move the host the check saw.
	if strings.ContainsFunc(raw, func(r rune) bool { return r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return refused
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.Hostname() == "" ||
		strings.Contains(parsed.Host, "@") {
		return refused
	}
	return nil
}
