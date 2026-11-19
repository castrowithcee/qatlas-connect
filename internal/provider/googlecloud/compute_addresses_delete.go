package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const addressesDeleteID = Provider + ".addresses.delete"

var addressesDelete = capability.Descriptor{
	ID: addressesDeleteID, Version: 1, Title: "Delete a reserved IP address",
	Description: "Delete one reserved IP address of a region of a project (addresses.delete of Compute Engine " +
		"v1), by its name or numeric ID. An external address is released and may be given to another customer, " +
		"so it cannot be got back; DNS records and allow lists that name it then point nowhere or elsewhere. An " +
		"address that is still in use (the users member of addresses.get) is refused by Google, and the tool " +
		"reports that refusal. Addresses cannot be reserved or changed." + listedOnlyNote + computeDeleteNote +
		computeTail,
	Tags: []string{"google", "cloud", "compute", "addresses", "ip", "network", "delete"}, Risk: computeDeleteRisk,
	Provider: Provider, RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeRegionProp +
		`,` + computeAddressProp + `,` + computeRequestIDProp +
		`},"required":["project_id","region","address"],"additionalProperties":false}`),
	OutputSchema: computeDeleteOutput,
	Arguments: []capability.Argument{serviceProjectArgument, computeRegionRequired,
		{Name: "address", Required: true, Description: "Name of the address resource, or its numeric ID; never " +
			"a path, URL, or the IP address itself"},
		computeRequestIDArgument},
	Fields: computeDeleteOperationFields,
	Examples: []capability.Example{{
		Description: "Delete a reserved address",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","region":"us-central1","address":"web-ip"}`),
	}},
}

type addressDeleteArguments struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region"`
	Address   string `json:"address"`
	RequestID string `json:"request_id"`
}

func handleAddressesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete address"
	var a addressDeleteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	switch {
	case !validProject(a.ProjectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(a.Region):
		return nil, badRequest("region must be a region name such as us-central1, never a path")
	case !validComputeNameOrID(a.Address):
		return nil, badRequest("address must be an address resource name or its numeric ID, never a path")
	}
	requestID, err := checkRequestID(a.RequestID)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := computeRegionPath(a.ProjectID, a.Region) + "/addresses/" + url.PathEscape(a.Address)
	return c.computeDelete(ctx, computeDeleteTarget{op: op, tool: addressesDeleteID, path: path, region: a.Region,
		requestID: requestID})
}

// addressDeleteSummary phrases the deletion from its arguments: project, type, region, and name.
func addressDeleteSummary(raw json.RawMessage) string {
	var a addressDeleteArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Delete reserved IP address " + boundedText(a.Address, maxNameText) + " in region " +
		boundedText(a.Region, maxNameText) + " of project " + boundedText(a.ProjectID, maxNameText) +
		": an external address is released and cannot be got back"
}
