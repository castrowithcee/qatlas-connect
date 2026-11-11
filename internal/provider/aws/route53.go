package aws

import (
	"regexp"
	"slices"

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
