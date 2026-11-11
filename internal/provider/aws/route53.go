package aws

import (
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
