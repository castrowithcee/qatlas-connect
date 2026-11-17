package googlecloud

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	addressesListID = Provider + ".addresses.list"
	addressesGetID  = Provider + ".addresses.get"
)

// addressMembers are the documented members of the Address resource of compute v1 (revision 20261106). Google
// marks none deprecated or input only. The addresses are operating data, not secrets, and pass unchanged.
var addressMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"address", "string"}, {"prefixLength", "integer"}, {"status", "string"},
	{"region", "string"}, {"selfLink", "string"}, {"users", "array"}, {"ipVersion", "string"},
	{"ipv6EndpointType", "string"}, {"addressType", "string"}, {"purpose", "string"}, {"subnetwork", "string"},
	{"network", "string"}, {"networkTier", "string"}, {"labels", "object"}, {"labelFingerprint", "string"},
	{"ipCollection", "string"},
}

const addressFields = "{kind, id, creationTimestamp, name, description, address, prefixLength, status " +
	"(RESERVING, RESERVED, IN_USE), region (URL), selfLink, users (URLs), ipVersion, ipv6EndpointType, " +
	"addressType (INTERNAL or EXTERNAL), purpose, subnetwork (URL), network (URL), networkTier, labels, " +
	"labelFingerprint, ipCollection}; untrusted text"

const computeAddressProp = `"address":{"type":"string","minLength":1,"maxLength":63,` +
	`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`

var addressesList = capability.Descriptor{
	ID: addressesListID, Version: 1, Title: "List the reserved IP addresses of a region",
	Description: "List the reserved IP addresses of one region of a project from Compute Engine, internal and " +
		"external, with address, type, status, users, network, subnetwork, purpose, IP version, and network " +
		"tier, optionally filtered, sorted, and with partial results accepted. Global and aggregated lists are " +
		"not offered, and addresses cannot be reserved or changed." + computeTail,
	Tags: []string{"google", "cloud", "compute", "addresses", "ip", "network"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps + `,` + computeRegionProp +
		`},"required":["project_id","region"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"addresses":{"type":"array","items":` +
		membersSchema(addressMembers) + `},"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["addresses","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeRegionRequired, computeFilterArgument,
		computeMaxArgument, computeOrderArgument, computePartialArgument, computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "addresses", Description: "Addresses of the page, each " + addressFields},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the external addresses of a region",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","region":"us-central1","filter":"addressType = EXTERNAL"}`),
	}},
}

var addressesGet = capability.Descriptor{
	ID: addressesGetID, Version: 1, Title: "Get one reserved IP address",
	Description: "Read one reserved IP address of a region of a project from Compute Engine, by its name or its " +
		"numeric ID, with the documented fields of the address." + computeTail,
	Tags: []string{"google", "cloud", "compute", "addresses", "ip", "network"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeRegionProp +
		`,` + computeAddressProp + `},"required":["project_id","region","address"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(membersSchema(addressMembers)),
	Arguments: []capability.Argument{serviceProjectArgument, computeRegionRequired,
		{Name: "address", Required: true, Description: "Name of the address resource, or its numeric ID; never " +
			"a path, URL, or the IP address itself"}},
	Fields: []capability.Field{{Name: "name", Description: "The address as one object: " + addressFields}},
	Examples: []capability.Example{{
		Description: "Read one address",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","region":"us-central1","address":"web-ip"}`),
	}},
}

type addressesAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

func handleAddressesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list addresses"
	var arguments regionalListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if arguments.IncludeAllScopes {
		return nil, badRequest("include_all_scopes is not an argument of this tool")
	}
	if arguments.Zone != "" {
		return nil, badRequest("zone is not an argument of this tool")
	}
	if arguments.Views != "" {
		return nil, badRequest("views is not an argument of this tool")
	}
	if err := arguments.check(); err != nil {
		return nil, err
	}
	binding := arguments.binding(addressesListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer addressesAnswer
	path := computeRegionPath(arguments.ProjectID, arguments.Region) + "/addresses"
	if err := c.get(ctx, op, addressesListID, serviceCompute, path, arguments.values(token), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := boundedMembers(op, item, addressMembers)
		if err != nil {
			return nil, err
		}
		if _, err := computeName(op, row); err != nil {
			return nil, err
		}
		if !inRegion(row, arguments.Region) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		rows = append(rows, row)
	}
	out := map[string]any{"addresses": rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

func handleAddressesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get address"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Region    string `json:"region"`
		Address   string `json:"address"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	switch {
	case !validProject(arguments.ProjectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(arguments.Region):
		return nil, badRequest("region must be a region name such as us-central1, never a path")
	case !validComputeNameOrID(arguments.Address):
		return nil, badRequest("address must be an address resource name or its numeric ID, never a path")
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire json.RawMessage
	path := computeRegionPath(arguments.ProjectID, arguments.Region) + "/addresses/" + arguments.Address
	if err := c.get(ctx, op, addressesGetID, serviceCompute, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := boundedMembers(op, wire, addressMembers)
	if err != nil {
		return nil, err
	}
	name, err := computeName(op, row)
	if err != nil {
		return nil, err
	}
	if id, _ := row["id"].(string); name != arguments.Address && id != arguments.Address ||
		!inRegion(row, arguments.Region) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}
