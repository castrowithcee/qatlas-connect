package aws

import (
	"context"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/sigv4"
)

// The SES tools work on sender identities of the bound account. The checks here run before any I/O, and the
// DNS help is derived by the tool from checked values, never copied from an answer.

var (
	dnsLabel       = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$`)
	addressLocal   = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
	dkimToken      = regexp.MustCompile(`^[a-z0-9]{20,64}$`)
	regionNameForm = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]{1,2}$`)
	// configSetName is the form SES documents for a configuration set name; the model gives no pattern.
	configSetName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	archiveID     = regexp.MustCompile(`^mailmanager-archive/a-[a-z0-9]{24,62}$`)
)

// validDomain reports a domain name of at least two labels, each of letters, digits, and inner hyphens, whose
// last label is not all digits.
func validDomain(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if len(l) > 63 || !dnsLabel.MatchString(l) {
			return false
		}
	}
	return strings.Trim(labels[len(labels)-1], "0123456789") != ""
}

// validIdentity reports an SES identity name: a domain, or an email address of a domain.
func validIdentity(s string) bool {
	if local, domain, isAddress := strings.Cut(s, "@"); isAddress {
		return len(s) <= 254 && addressLocal.MatchString(local) && validDomain(domain)
	}
	return validDomain(s)
}

func checkSESIdentity(_ binding, _ bool, name, value string) error {
	if !validIdentity(value) {
		return &provider.InvalidRequestError{Message: name + " must be a domain name or an email address"}
	}
	return nil
}

// checkSESIdentityARN accepts the ARN of an identity or of a configuration set in the bound account and an
// allowed region; the tag tools take either.
func checkSESIdentityARN(b binding, global bool, name, value string) error {
	if err := checkARNRegion(b, global, name, value); err != nil {
		return err
	}
	parts := strings.SplitN(value, ":", 6)
	if parts[2] == "ses" {
		if resource, ok := strings.CutPrefix(parts[5], "identity/"); ok && validIdentity(resource) {
			return nil
		}
		if resource, ok := strings.CutPrefix(parts[5], "configuration-set/"); ok && configSetName.MatchString(resource) {
			return nil
		}
	}
	return &provider.InvalidRequestError{Message: name + " must be the ARN of an SES identity or configuration set"}
}

func checkSESConfigurationSet(_ binding, _ bool, name, value string) error {
	if !configSetName.MatchString(value) {
		return &provider.InvalidRequestError{Message: name + " must be 1 to 64 letters, digits, underscores, or hyphens"}
	}
	return nil
}

// checkSESArchiveARN accepts only the ARN of a mail archive in the bound account and an allowed region.
func checkSESArchiveARN(b binding, name, value string) error {
	if err := checkARNRegion(b, false, name, value); err != nil {
		return err
	}
	parts := strings.SplitN(value, ":", 6)
	if parts[2] != "ses" || !archiveID.MatchString(parts[5]) {
		return &provider.InvalidRequestError{Message: name + " must be the ARN of an SES mail archive"}
	}
	return nil
}

// checkSESConfigurationSetValues judges the values of the configuration set options that name something
// outside the set, at the top level of a Put tool and nested in the options of create: the archive ARN
// and the custom redirect domain.
func checkSESConfigurationSetValues(b binding, _ string, args map[string]any) error {
	nested := func(group string) map[string]any {
		m, _ := args[group].(map[string]any)
		return m
	}
	for _, c := range []struct {
		scope map[string]any
		path  string
		key   string
		arn   bool
	}{
		{args, "archive_arn", "archive_arn", true},
		{nested("archiving_options"), "archiving_options.archive_arn", "archive_arn", true},
		{args, "custom_redirect_domain", "custom_redirect_domain", false},
		{nested("tracking_options"), "tracking_options.custom_redirect_domain", "custom_redirect_domain", false},
	} {
		value, set := c.scope[c.key]
		if !set {
			continue
		}
		text, _ := value.(string)
		if c.arn {
			if err := checkSESArchiveARN(b, c.path, text); err != nil {
				return err
			}
		} else if !validDomain(text) {
			return &provider.InvalidRequestError{Message: c.path + " must be a domain name"}
		}
	}
	return nil
}

// resultHook adds a member to the result of a tool from its arguments and its checked answer. A hook may read
// with the credentials that passed the account check; creds is empty for the tool that is the account check.
type resultHook func(ctx context.Context, c *Client, s toolSpec, creds sigv4.Credentials, result map[string]any, region string, args map[string]any)

// resultHooks are the hand-written result steps, by the name a curation table gives them.
var resultHooks = map[string]resultHook{
	"dns-records": func(_ context.Context, _ *Client, _ toolSpec, _ sigv4.Credentials, result map[string]any, region string, args map[string]any) {
		addDNSRecords(result, region, args)
	},
	"bucket-policy-status": addBucketPolicyStatus,
}

// dkimZone matches the narrow form of an SES DKIM hosted zone: dkim.amazonses.com or
// dkim.<region-like label>.amazonses.com. SES names the zone per region and identity, so it is taken from the
// answer, but only in this form.
var dkimZone = regexp.MustCompile(`^dkim(\.[a-z0-9]+(-[a-z0-9]+)*)?\.amazonses\.com$`)

// maxDKIMTokens bounds the DKIM records of the help; SES issues three.
const maxDKIMTokens = 8

// addDNSRecords sets dns_records from the identity name the caller gave and the answer, taking from the
// answer only values of the expected form: a DKIM token of lowercase letters and digits, the DKIM signing
// hosted zone (without a valid one there are no DKIM records), and a MAIL FROM
// domain that is a subdomain of the identity. Anything else is left out of the help. An email address needs
// no records of its own.
func addDNSRecords(result map[string]any, region string, args map[string]any) {
	records := []any{}
	defer func() { result["dns_records"] = records }()
	identity, _ := args["email_identity"].(string)
	identity = strings.ToLower(identity)
	if !validDomain(identity) {
		return
	}
	add := func(purpose, kind, name, value string) {
		records = append(records, map[string]any{"purpose": purpose, "type": kind, "name": name, "value": value})
	}
	if dkim, ok := result["dkim_attributes"].(map[string]any); ok {
		tokens, _ := dkim["tokens"].([]any)
		zone, _ := dkim["signing_hosted_zone"].(string)
		if !dkimZone.MatchString(zone) {
			tokens = nil // no guessing of a target
		}
		for _, raw := range tokens {
			if token, _ := raw.(string); len(records) < maxDKIMTokens && dkimToken.MatchString(token) {
				add("dkim", "CNAME", token+"._domainkey."+identity, token+"."+zone)
			}
		}
	}
	if mail, ok := result["mail_from_attributes"].(map[string]any); ok && regionNameForm.MatchString(region) {
		domain, _ := mail["mail_from_domain"].(string)
		domain = strings.ToLower(domain)
		if validDomain(domain) && strings.HasSuffix(domain, "."+identity) {
			add("mail-from-mx", "MX", domain, "10 feedback-smtp."+region+".amazonses.com")
			add("mail-from-spf", "TXT", domain, "v=spf1 include:amazonses.com ~all")
		}
	}
}
