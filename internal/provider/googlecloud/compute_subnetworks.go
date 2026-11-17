package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	subnetworksListID = Provider + ".subnetworks.list"
	subnetworksGetID  = Provider + ".subnetworks.get"
)

// subnetworkMembers are the documented, not input-only members of the Subnetwork resource of compute v1
// (revision 20261106). params is left out because it is input only. utilizationDetails is filled only with
// the view WITH_UTILIZATION.
var subnetworkMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"network", "string"}, {"ipCidrRange", "string"}, {"reservedInternalRange", "string"},
	{"gatewayAddress", "string"}, {"region", "string"}, {"selfLink", "string"}, {"privateIpGoogleAccess", "boolean"},
	{"secondaryIpRanges", "array"}, {"fingerprint", "string"}, {"purpose", "string"}, {"role", "string"},
	{"state", "string"}, {"logConfig", "object"}, {"enableFlowLogs", "boolean"},
	{"privateIpv6GoogleAccess", "string"}, {"ipv6CidrRange", "string"}, {"externalIpv6Prefix", "string"},
	{"internalIpv6Prefix", "string"}, {"ipv6AccessType", "string"}, {"stackType", "string"},
	{"ipv6GceEndpoint", "string"}, {"systemReservedExternalIpv6Ranges", "array"},
	{"systemReservedInternalIpv6Ranges", "array"}, {"allowSubnetCidrRoutesOverlap", "boolean"},
	{"utilizationDetails", "object"}, {"resolveSubnetMask", "string"}, {"ipCollection", "string"},
}

const subnetworkFields = "{kind, id, creationTimestamp, name, description, network (URL), ipCidrRange, " +
	"reservedInternalRange, gatewayAddress, region (URL), selfLink, privateIpGoogleAccess, secondaryIpRanges, " +
	"fingerprint, purpose, role, state, logConfig, enableFlowLogs, privateIpv6GoogleAccess, ipv6CidrRange, " +
	"externalIpv6Prefix, internalIpv6Prefix, ipv6AccessType, stackType, ipv6GceEndpoint, " +
	"systemReservedExternalIpv6Ranges, systemReservedInternalIpv6Ranges, allowSubnetCidrRoutesOverlap, " +
	"utilizationDetails (only with views WITH_UTILIZATION), resolveSubnetMask, ipCollection}; untrusted text"

const (
	computeSubnetworkProp = `"subnetwork":{"type":"string","minLength":1,"maxLength":63,` +
		`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`
	computeViewsProp = `"views":{"type":"string","enum":["DEFAULT","WITH_UTILIZATION"]}`
)

var (
	computeRegionRequired = func() capability.Argument {
		arg := computeRegionArgument
		arg.Required = true
		return arg
	}()
	computeViewsArgument = capability.Argument{Name: "views", Description: "DEFAULT, or WITH_UTILIZATION to add " +
		"the IP utilization of the subnetwork as utilizationDetails"}
)

var subnetworksList = capability.Descriptor{
	ID: subnetworksListID, Version: 1, Title: "List the subnetworks of a region",
	Description: "List the subnetworks of one region of a project from Compute Engine, with their IP ranges, " +
		"secondary ranges, network, gateway, purpose, role, stack type, IPv6 access type, and flow log " +
		"configuration, optionally filtered, sorted, and with partial results accepted. Aggregated lists over " +
		"all regions are not offered." + computeTail,
	Tags: []string{"google", "cloud", "compute", "subnetworks", "vpc"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps + `,` + computeRegionProp +
		`,` + computeViewsProp + `},"required":["project_id","region"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"subnetworks":{"type":"array","items":` +
		membersSchema(subnetworkMembers) + `},"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["subnetworks","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeRegionRequired, computeFilterArgument,
		computeMaxArgument, computeOrderArgument, computePartialArgument, computeViewsArgument,
		computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "subnetworks", Description: "Subnetworks of the page, each " + subnetworkFields},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the subnetworks of a region",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","region":"us-central1"}`),
	}},
}

var subnetworksGet = capability.Descriptor{
	ID: subnetworksGetID, Version: 1, Title: "Get one subnetwork",
	Description: "Read one subnetwork of a region of a project from Compute Engine, by its name or its numeric " +
		"ID, with the documented fields of the subnetwork." + computeTail,
	Tags: []string{"google", "cloud", "compute", "subnetworks", "vpc"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeRegionProp +
		`,` + computeSubnetworkProp + `,` + computeViewsProp +
		`},"required":["project_id","region","subnetwork"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(membersSchema(subnetworkMembers)),
	Arguments: []capability.Argument{serviceProjectArgument, computeRegionRequired,
		{Name: "subnetwork", Required: true, Description: "Name of the subnetwork such as default, or its " +
			"numeric ID; never a path or URL"}, computeViewsArgument},
	Fields: []capability.Field{{Name: "name", Description: "The subnetwork as one object: " + subnetworkFields}},
	Examples: []capability.Example{{
		Description: "Read one subnetwork",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","region":"us-central1","subnetwork":"default"}`),
	}},
}

// regionalListArguments are the arguments of a regional list: the shared ones, the region, and the view.
type regionalListArguments struct {
	computeListArguments
	Region string `json:"region"`
	Views  string `json:"views"`
}

func validComputeViews(value string) bool {
	return value == "" || value == "DEFAULT" || value == "WITH_UTILIZATION"
}

// check refuses an argument that is not in its documented form. The shared check runs without a zone.
func (a regionalListArguments) check() error {
	if err := a.computeListArguments.check(false); err != nil {
		return err
	}
	switch {
	case !validComputeName(a.Region):
		return badRequest("region must be a region name such as us-central1, never a path")
	case !validComputeViews(a.Views):
		return badRequest("views must be DEFAULT or WITH_UTILIZATION")
	}
	return nil
}

// binding ties a cursor to the tool, the targets, the region, and everything that shapes the list.
func (a regionalListArguments) binding(tool string, resolved *config.Resolved) []byte {
	return provider.CursorBinding(tool, provider.TargetsOf(resolved), a.ProjectID, "region/"+a.Region, a.Filter,
		a.OrderBy, a.ReturnPartialSuccess, a.Views)
}

func (a regionalListArguments) values(token string) url.Values {
	query := a.query(token, 0)
	if a.Views != "" {
		query.Set("views", a.Views)
	}
	return query
}

// inRegion reports whether a row that names its region names the region asked for. The region of a row is a
// URL whose last segment is the region.
func inRegion(row map[string]any, region string) bool {
	value, ok := row["region"].(string)
	return !ok || value == region || strings.HasSuffix(value, "/regions/"+region)
}

type subnetworksAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

func handleSubnetworksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list subnetworks"
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
	if err := arguments.check(); err != nil {
		return nil, err
	}
	binding := arguments.binding(subnetworksListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer subnetworksAnswer
	path := computeRegionPath(arguments.ProjectID, arguments.Region) + "/subnetworks"
	if err := c.get(ctx, op, subnetworksListID, serviceCompute, path, arguments.values(token), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := boundedMembers(op, item, subnetworkMembers)
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
	out := map[string]any{"subnetworks": rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

func handleSubnetworksGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get subnetwork"
	var arguments struct {
		ProjectID  string `json:"project_id"`
		Region     string `json:"region"`
		Subnetwork string `json:"subnetwork"`
		Views      string `json:"views"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	switch {
	case !validProject(arguments.ProjectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(arguments.Region):
		return nil, badRequest("region must be a region name such as us-central1, never a path")
	case !validComputeNameOrID(arguments.Subnetwork):
		return nil, badRequest("subnetwork must be a subnetwork name or its numeric ID, never a path")
	case !validComputeViews(arguments.Views):
		return nil, badRequest("views must be DEFAULT or WITH_UTILIZATION")
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var query url.Values
	if arguments.Views != "" {
		query = url.Values{"views": {arguments.Views}}
	}
	var wire json.RawMessage
	path := computeRegionPath(arguments.ProjectID, arguments.Region) + "/subnetworks/" + arguments.Subnetwork
	if err := c.get(ctx, op, subnetworksGetID, serviceCompute, path, query, &wire); err != nil {
		return nil, err
	}
	row, err := boundedMembers(op, wire, subnetworkMembers)
	if err != nil {
		return nil, err
	}
	name, err := computeName(op, row)
	if err != nil {
		return nil, err
	}
	if id, _ := row["id"].(string); name != arguments.Subnetwork && id != arguments.Subnetwork ||
		!inRegion(row, arguments.Region) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}
