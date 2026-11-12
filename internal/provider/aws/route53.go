package aws

import (
	"encoding/json"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// vpcID is the form of an EC2 VPC ID: vpc- and 8 or 17 hexadecimal digits.
var vpcID = regexp.MustCompile(`^vpc-([0-9a-f]{8}|[0-9a-f]{17})$`)

// checkRoute53VPC judges the VPC of a new hosted zone before any request. A VPC names a region and an ID that
// both must be there and well formed, and the region must be one the connection allows. A private zone needs a
// VPC, and a VPC makes no sense for a public zone. A VPC ID carries no account, so whether the VPC belongs to
// the bound account is left to Route 53, which refuses the VPC of another account that did not authorize it.
func checkRoute53VPC(b binding, _ string, args map[string]any) error {
	config, _ := args["hosted_zone_config"].(map[string]any)
	private, _ := config["private_zone"].(bool)
	vpc, set := args["vpc"].(map[string]any)
	if !set {
		if private {
			return &provider.InvalidRequestError{Message: "vpc is required for a private zone"}
		}
		return nil
	}
	if !private {
		return &provider.InvalidRequestError{Message: "vpc belongs to a private zone: set hosted_zone_config.private_zone to true or leave vpc out"}
	}
	region, _ := vpc["vpc_region"].(string)
	if !slices.Contains(b.regions, region) {
		return &provider.InvalidRequestError{Message: "vpc.vpc_region must be a region the connection allows"}
	}
	if id, _ := vpc["vpc_id"].(string); !vpcID.MatchString(id) {
		return &provider.InvalidRequestError{Message: "vpc.vpc_id must be a VPC ID such as vpc-0123456789abcdef0"}
	}
	return nil
}

// checkRoute53VPCLookup judges the VPC of a lookup of the hosted zones of a VPC before any request: the region
// must be one the connection allows and the ID a well formed VPC ID. A VPC ID carries no account, and Route 53
// answers only the zones of the bound account.
func checkRoute53VPCLookup(b binding, _ string, args map[string]any) error {
	if region, _ := args["vpc_region"].(string); !slices.Contains(b.regions, region) {
		return &provider.InvalidRequestError{Message: "vpc_region must be a region the connection allows"}
	}
	if id, _ := args["vpc_id"].(string); !vpcID.MatchString(id) {
		return &provider.InvalidRequestError{Message: "vpc_id must be a VPC ID such as vpc-0123456789abcdef0"}
	}
	return nil
}

// checkRoute53VPCMember judges the VPC that a zone is associated with, released from, or no longer authorized for,
// before any request: a region the connection allows and a well formed ID. A VPC ID carries no account, so the
// account of the VPC is not checked.
func checkRoute53VPCMember(b binding, _ string, args map[string]any) error {
	vpc, _ := args["vpc"].(map[string]any)
	if region, _ := vpc["vpc_region"].(string); !slices.Contains(b.regions, region) {
		return &provider.InvalidRequestError{Message: "vpc.vpc_region must be a region the connection allows"}
	}
	if id, _ := vpc["vpc_id"].(string); !vpcID.MatchString(id) {
		return &provider.InvalidRequestError{Message: "vpc.vpc_id must be a VPC ID such as vpc-0123456789abcdef0"}
	}
	return nil
}

// Limits of one ChangeResourceRecordSets call that Route 53 documents and the model does not state: at most 1000
// record values and 32000 characters of values in a batch, an UPSERT counting twice.
const (
	maxBatchValues = 1000
	maxBatchChars  = 32000
)

// dnsNameChar is a character a record name may carry without an escape: letters, digits, hyphen, underscore.
// A literal asterisk is only the whole first label.
func dnsNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// validDNSName judges the form of a record name: dot separated labels of letters, digits, hyphens, and underscores,
// \ddd escapes (octal, such as \052 for *) for any other byte, a whole-label asterisk only first, an optional
// trailing dot, and no empty label. The name is data and is never interpreted beyond this form.
func validDNSName(name string) bool {
	if name == "." {
		return true
	}
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 1024 {
		return false
	}
	for i, label := range strings.Split(name, ".") {
		if label == "" || label == "*" && i != 0 {
			return false
		}
		if label == "*" {
			continue
		}
		for j := 0; j < len(label); j++ {
			switch c := label[j]; {
			case dnsNameChar(c):
			case c == '\\' && j+3 < len(label) && isOctal(label[j+1:j+4]):
				j += 3
			default:
				return false
			}
		}
	}
	return true
}

func isOctal(s string) bool {
	return len(s) == 3 && s[0] >= '0' && s[0] <= '3' && s[1] >= '0' && s[1] <= '7' && s[2] >= '0' && s[2] <= '7'
}

// checkRoute53Name judges the record name of a DNS answer test before any request.
func checkRoute53Name(_ binding, _ string, args map[string]any) error {
	if name, _ := args["record_name"].(string); !validDNSName(name) {
		return &provider.InvalidRequestError{Message: "record_name must be a domain name: dot separated labels of letters, digits, hyphens, and underscores, with \\052 style escapes for other bytes"}
	}
	return nil
}

// checkRoute53Change judges the change batch before any request: at least one change, a well formed name in
// each, and the documented size of the batch. Types, actions, and TTLs are bound by the schema. Alias targets are
// not judged: a DNS name carries no account.
func checkRoute53Change(_ binding, _ string, args map[string]any) error {
	batch, _ := args["change_batch"].(map[string]any)
	changes, _ := batch["changes"].([]any)
	if len(changes) == 0 {
		return &provider.InvalidRequestError{Message: "change_batch.changes needs at least one change"}
	}
	values, chars := 0, 0
	for i, c := range changes {
		change, _ := c.(map[string]any)
		set, _ := change["resource_record_set"].(map[string]any)
		if name, _ := set["name"].(string); !validDNSName(name) {
			return &provider.InvalidRequestError{Message: "change_batch.changes[" + strconv.Itoa(i) + "].resource_record_set.name must be a domain name: " +
				"dot separated labels of letters, digits, hyphens, and underscores, with \\052 style escapes for other bytes"}
		}
		weight := 1
		if change["action"] == "UPSERT" {
			weight = 2
		}
		records, _ := set["resource_records"].([]any)
		for _, r := range records {
			record, _ := r.(map[string]any)
			value, _ := record["value"].(string)
			values += weight
			chars += weight * utf8.RuneCountInString(value)
		}
	}
	if values > maxBatchValues || chars > maxBatchChars {
		return &provider.InvalidRequestError{Message: "change_batch holds too many record values: Route 53 allows " + strconv.Itoa(maxBatchValues) +
			" values and " + strconv.Itoa(maxBatchChars) + " characters in one batch, an UPSERT counting twice; split it into several calls"}
	}
	return nil
}

// blockedV4 and blockedV6 are the ranges of an address a health check may not probe, beyond what netip classifies:
// "this" network, shared address space, protocol assignments, documentation, 6to4 relay, benchmarking, and the
// reserved block. Private, loopback, link-local, multicast, and unspecified addresses are judged by netip itself.
var blockedV4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
}

var blockedV6 = []netip.Prefix{
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var globalV6 = netip.MustParsePrefix("2000::/3")

// publicAddr reports whether an address is a public unicast address: IPv4 outside the blocked ranges, and IPv6
// inside the global unicast block 2000::/3 and outside the blocked ranges. An IPv4-mapped IPv6 address is judged as IPv4.
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.Zone() != "" || a.IsUnspecified() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() || a.IsMulticast() {
		return false
	}
	blocked := blockedV6
	if a.Is4() {
		blocked = blockedV4
	} else if !globalV6.Contains(a) {
		return false
	}
	return !slices.ContainsFunc(blocked, func(p netip.Prefix) bool { return p.Contains(a) })
}

// validHealthName judges the domain name a health check probes: the form of validDNSName without escapes and
// wildcards, at most 255 characters, not localhost, and not the text form of an IP address that is not public.
func validHealthName(name string) bool {
	host := strings.TrimSuffix(name, ".")
	if len(name) > 255 || !validDNSName(name) || name == "." || strings.ContainsAny(name, `\*`) {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return publicAddr(a)
	}
	return true
}

// cloudWatchAlarmARN is the form of the ARN of a CloudWatch alarm, as a name in an alarm identifier could carry it.
var cloudWatchAlarmARN = regexp.MustCompile(`^arn:aws:cloudwatch:([a-z0-9-]*):([0-9]*):alarm:.+$`)

// checkRoute53Health judges the target, the alarm, and the routing control of a health check before any request.
// Create nests them in health_check_config, update takes them at the top level. The target must be a valid name
// or a public address. The alarm names a region, which the connection must allow; the alarm itself is looked up by
// Route 53 in the bound account, as the identifier carries no account. A name given as an ARN must carry the
// bound account and an allowed region. A routing control ARN must carry the bound account.
func checkRoute53Health(b binding, _ string, args map[string]any) error {
	config := args
	if nested, ok := args["health_check_config"].(map[string]any); ok {
		config = nested
	}
	if name, set := config["fully_qualified_domain_name"].(string); set && !validHealthName(name) {
		return &provider.InvalidRequestError{Message: "fully_qualified_domain_name must be a domain name that is not localhost nor a non-public IP address: dot separated labels of letters, digits, hyphens, and underscores"}
	}
	if text, set := config["ip_address"].(string); set {
		if a, err := netip.ParseAddr(text); err != nil || !publicAddr(a) {
			return &provider.InvalidRequestError{Message: "ip_address must be a public IPv4 or IPv6 address: private, loopback, link-local, unspecified, multicast, shared (100.64.0.0/10), documentation, and reserved ranges are refused"}
		}
	}
	if alarm, set := config["alarm_identifier"].(map[string]any); set {
		region, _ := alarm["region"].(string)
		if !slices.Contains(b.regions, region) {
			return &provider.InvalidRequestError{Message: "alarm_identifier.region must be a region the connection allows"}
		}
		if name, _ := alarm["name"].(string); strings.HasPrefix(name, "arn:") {
			m := cloudWatchAlarmARN.FindStringSubmatch(name)
			if m == nil || m[2] != b.account || m[1] != region {
				return &provider.InvalidRequestError{Message: "alarm_identifier.name as an ARN must be an alarm of the account this connection is bound to, in the region of alarm_identifier.region"}
			}
		}
	}
	if arn, set := config["routing_control_arn"].(string); set {
		if parts := strings.SplitN(arn, ":", 6); len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[4] != b.account {
			return &provider.InvalidRequestError{Message: "routing_control_arn must be an ARN of the account this connection is bound to"}
		}
	}
	return nil
}

// kskRegion is the only region of the KMS key of a key signing key.
const kskRegion = "us-east-1"

// checkRoute53KSK judges the KMS key of a new key signing key before any request. DNSSEC signing keys must lie in
// us-east-1, which the connection need not allow, so the region is fixed and not tied to the allow-list. The key
// must be named by the ARN of a key (not an alias) in the bound account.
func checkRoute53KSK(b binding, _ string, args map[string]any) error {
	arn, _ := args["key_management_service_arn"].(string)
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) == 6 && parts[0] == "arn" && parts[1] == "aws" && parts[2] == "kms" && parts[3] == kskRegion && parts[4] == b.account {
		if id, ok := strings.CutPrefix(parts[5], "key/"); ok && kmsKeyID.MatchString(id) {
			return nil
		}
	}
	return &provider.InvalidRequestError{Message: "key_management_service_arn must be the ARN of a KMS key (key/...) in us-east-1 in the account this connection is bound to"}
}

// Limits of a ChangeCidrCollection call: the model allows 1000 changes and 1000 blocks per list; the tool also caps
// the blocks of the whole call.
const maxCIDRBlocks = 1000

// checkRoute53CIDR judges the blocks of a CIDR collection change before any request: each must be an IPv4 or IPv6
// address with a prefix length (CIDR notation, no zone), and the whole call holds at most maxCIDRBlocks blocks.
// Location names and the action are bound by the schema; block sizes and quotas are left to Route 53.
func checkRoute53CIDR(_ binding, _ string, args map[string]any) error {
	changes, _ := args["changes"].([]any)
	total := 0
	for i, c := range changes {
		change, _ := c.(map[string]any)
		blocks, _ := change["cidr_list"].([]any)
		for j, b := range blocks {
			total++
			text, _ := b.(string)
			if p, err := netip.ParsePrefix(text); err != nil || p.Addr().Zone() != "" {
				return &provider.InvalidRequestError{Message: "changes[" + strconv.Itoa(i) + "].cidr_list[" + strconv.Itoa(j) +
					"] must be an IPv4 or IPv6 block in CIDR notation such as 192.0.2.0/24 or 2001:db8::/48"}
			}
		}
	}
	if total > maxCIDRBlocks {
		return &provider.InvalidRequestError{Message: "changes hold too many CIDR blocks: at most " + strconv.Itoa(maxCIDRBlocks) + " in one call; split it into several calls"}
	}
	return nil
}

// logGroupResource is the resource of a log group ARN: the type and a log group name, nothing else. Wildcards,
// further colons, and the ":*" suffix of a console ARN do not match.
var logGroupResource = regexp.MustCompile(`^log-group:[A-Za-z0-9_./#-]{1,512}$`)

// checkRoute53LogGroup judges the log group of a new query logging configuration before any request. Route 53
// query logs can only go to us-east-1, so the region is fixed and must also be one the connection allows. The ARN
// must name a log group (not another resource or service) of the bound account, without wildcards.
func checkRoute53LogGroup(b binding, _ string, args map[string]any) error {
	arn, _ := args["cloud_watch_logs_log_group_arn"].(string)
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) == 6 && parts[0] == "arn" && parts[1] == "aws" && parts[2] == "logs" && parts[3] == kskRegion &&
		parts[4] == b.account && logGroupResource.MatchString(parts[5]) {
		if !slices.Contains(b.regions, kskRegion) {
			return &provider.InvalidRequestError{Message: "cloud_watch_logs_log_group_arn is in us-east-1, which this connection does not allow"}
		}
		return nil
	}
	return &provider.InvalidRequestError{Message: "cloud_watch_logs_log_group_arn must be the ARN of a log group (log-group:<name>, no wildcard) in us-east-1 in the account this connection is bound to"}
}

// maxPolicyDocument is the length limit of a traffic policy document in the Route 53 model, in characters.
const maxPolicyDocument = 102400

// checkRoute53Policy judges the document of a traffic policy before any request: within the model's limit and
// valid JSON. The content is not interpreted; Route 53 validates it.
func checkRoute53Policy(_ binding, _ string, args map[string]any) error {
	doc, _ := args["document"].(string)
	if utf8.RuneCountInString(doc) > maxPolicyDocument {
		return &provider.InvalidRequestError{Message: "document is longer than " + strconv.Itoa(maxPolicyDocument) + " characters, the limit of a traffic policy document"}
	}
	if !json.Valid([]byte(doc)) {
		return &provider.InvalidRequestError{Message: "document must be valid JSON text"}
	}
	return nil
}
