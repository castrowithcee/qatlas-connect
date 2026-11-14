package aws

import (
	"context"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/endpoints"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/sigv4"
)

// checkAccountRegionName requires region_name to be a region of the aws partition by form. The model alone
// accepts any text up to 50 characters.
func checkAccountRegionName(_ binding, _ string, args map[string]any) error {
	name, _ := args["region_name"].(string)
	if err := endpoints.ValidRegion(name); err != nil {
		return &provider.InvalidRequestError{Message: "region_name must be a region of the aws partition, such as af-south-1"}
	}
	return nil
}

// addRegionAllowlist sets allowlist_note after a region was enabled or disabled. Neither call changes the
// region list of the connection, so an enabled region stays unusable for regional tools until the person adds
// it as a target.
func addRegionAllowlist(_ context.Context, c *Client, s toolSpec, _ sigv4.Credentials, result map[string]any, _ string, args map[string]any) {
	// The guard ran before the call, so the name is a region by form and safe to quote.
	name, _ := args["region_name"].(string)
	listed := slices.Contains(c.binding.regions, name)
	note := "The region list of this connection is unchanged."
	switch {
	case s.descriptor.ID == "aws.account.region.disable" && listed:
		note += " " + name + " is still in the region list: regional tools will fail there until region/" + name + " is removed from the targets."
	case s.descriptor.ID == "aws.account.region.disable":
		note += " " + name + " is not in the region list."
	default:
		note += " A regional tool uses " + name + " only when region/" + name + " is a target of the connection."
	}
	result["allowlist_note"] = note
}
