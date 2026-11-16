package googlesearchconsole

import (
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	// domainPrefix starts a domain property.
	domainPrefix = "sc-domain:"
	// maxURLBytes bounds a property, a sitemap address, and a sitemap index address.
	maxURLBytes = 2048
	// maxDomainBytes and maxLabelBytes are the DNS limits of a domain name and of one of its labels.
	maxDomainBytes = 253
	maxLabelBytes  = 63
)

// checkProperty checks the spelling of a Search Console property: sc-domain: with a domain name, or an
// absolute http or https URL prefix ending in a slash, without user information, query, or fragment.
// Search Console names a property exactly this way, so a value of another form can name none.
func checkProperty(name, value string) error {
	if domain, ok := strings.CutPrefix(value, domainPrefix); ok {
		if !validDomain(domain) {
			return badRequest(name + " must be sc-domain: followed by a domain name such as example.com")
		}
		return nil
	}
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return badRequest(name + " must be sc-domain:DOMAIN or a URL prefix such as https://www.example.com/")
	}
	if err := checkAddress(name, value); err != nil {
		return err
	}
	parsed, _ := url.Parse(value)
	if parsed.RawQuery != "" || parsed.ForceQuery || strings.ContainsAny(value, "?#") || !strings.HasSuffix(parsed.Path, "/") {
		return badRequest(name + " must be sc-domain:DOMAIN or a URL prefix such as https://www.example.com/ " +
			"that ends in a slash and has no query or fragment")
	}
	return nil
}

// checkAddress checks an absolute http or https address that is handed to Google and never fetched: no
// other scheme, no user information that would carry a credential, no whitespace or control characters, and
// no fragment, which Google does not use.
func checkAddress(name, value string) error {
	lower := strings.ToLower(value)
	if value == "" || len(value) > maxURLBytes || !(strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) {
		return badRequest(name + " must be an absolute http or https address of at most 2048 characters")
	}
	for i := 0; i < len(value); i++ {
		if value[i] <= ' ' || value[i] == 0x7f {
			return badRequest(name + " must not contain spaces or control characters")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || parsed.Opaque != "" {
		return badRequest(name + " must be an absolute http or https address with a host")
	}
	if parsed.User != nil || strings.Contains(parsed.Host, "@") {
		return badRequest(name + " must not contain user information")
	}
	if strings.Contains(value, "#") {
		return badRequest(name + " must not contain a fragment")
	}
	return nil
}

// checkSitemap checks the address of a sitemap or a sitemap index. It may carry a query, as generated
// sitemaps often do.
func checkSitemap(name, value string) error {
	if strings.HasPrefix(value, domainPrefix) {
		return badRequest(name + " must be an absolute http or https address, not a property")
	}
	return checkAddress(name, value)
}

// validDomain reports whether domain is a DNS name of at least two labels of letters, digits, and inner
// hyphens. A punycode name (xn--) passes; a name in another script must be given as punycode.
func validDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > maxDomainBytes {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > maxLabelBytes || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// boundProperty returns the property the connection is bound to, empty for none.
func boundProperty(resolved *config.Resolved) (string, error) {
	targets := provider.TargetsOf(resolved)
	switch len(targets) {
	case 0:
		return "", nil
	case 1:
		if err := checkProperty("the property of the connection", targets[0]); err != nil {
			return "", failure("open", "the property of the connection is not a valid Search Console property")
		}
		return targets[0], nil
	}
	return "", failure("open", "a Google Search Console connection is bound to at most one property")
}

// siteOf settles the property of a call before any secret is resolved and before any request: the argument
// when given, or else the property the connection is bound to. A bound connection refuses every other
// property, spelled differently or not, without naming either.
func siteOf(resolved *config.Resolved, given string) (string, error) {
	bound, err := boundProperty(resolved)
	if err != nil {
		return "", err
	}
	if given == "" {
		if bound == "" {
			return "", badRequest("site_url is required unless the connection is bound to a property")
		}
		return bound, nil
	}
	if err := checkProperty("site_url", given); err != nil {
		return "", err
	}
	if bound != "" && given != bound {
		return "", badRequest("site_url lies outside the property this connection is bound to")
	}
	return given, nil
}

// segment escapes value as one path segment of a simple URI template expansion (RFC 6570): everything but
// the unreserved characters is percent-encoded, so the colon and the slashes of a property or a sitemap
// address stay inside their segment.
func segment(value string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

func sitePath(site string) string { return sitesPath + "/" + segment(site) }

func sitemapsPath(site string) string { return sitePath(site) + "/sitemaps" }

// accessLevels are the permission levels that grant access to the data of a property, in the spelling of the
// discovery document and in the one the API answers with. An unverified user, an unspecified level, and any
// unknown value grant none.
var accessLevels = map[string]bool{
	"SITE_OWNER": true, "SITE_FULL_USER": true, "SITE_RESTRICTED_USER": true,
	"siteOwner": true, "siteFullUser": true, "siteRestrictedUser": true,
}

func hasAccess(level string) bool { return accessLevels[level] }

func anyAccess(entries []site) bool {
	for _, entry := range entries {
		if hasAccess(string(entry.PermissionLevel)) {
			return true
		}
	}
	return false
}
