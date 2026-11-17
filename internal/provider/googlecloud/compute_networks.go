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
	networksListID = Provider + ".networks.list"
	networksGetID  = Provider + ".networks.get"
)

// networkMembers are the documented, not deprecated, not input-only members of the Network resource of compute
// v1 (revision 20261106). IPv4Range is left out because Google marks it deprecated, and params because it is
// input only.
var networkMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"gatewayIPv4", "string"}, {"selfLink", "string"}, {"selfLinkWithId", "string"},
	{"autoCreateSubnetworks", "boolean"}, {"subnetworks", "array"}, {"peerings", "array"},
	{"routingConfig", "object"}, {"mtu", "integer"}, {"networkFirewallPolicyEnforcementOrder", "string"},
	{"firewallPolicy", "string"}, {"enableUlaInternalIpv6", "boolean"}, {"internalIpv6Range", "string"},
	{"networkProfile", "string"},
}

const networkFields = "{kind, id, creationTimestamp, name, description, gatewayIPv4, selfLink, selfLinkWithId, " +
	"autoCreateSubnetworks (true in the auto mode, false in the custom mode), subnetworks (URLs), peerings, " +
	"routingConfig, mtu, networkFirewallPolicyEnforcementOrder, firewallPolicy, enableUlaInternalIpv6, " +
	"internalIpv6Range, networkProfile}; untrusted text"

var networksList = capability.Descriptor{
	ID: networksListID, Version: 1, Title: "List the VPC networks of a project",
	Description: "List the VPC networks of a project from Compute Engine, with their mode (auto or custom " +
		"subnets), subnetwork URLs, peerings, routing configuration, MTU, and firewall policy enforcement order, " +
		"optionally filtered, sorted, and with partial results accepted." + computeTail,
	Tags: []string{"google", "cloud", "compute", "networks", "vpc"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps +
		`},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"networks":{"type":"array","items":` +
		membersSchema(networkMembers) + `},"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["networks","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeFilterArgument, computeMaxArgument,
		computeOrderArgument, computePartialArgument, computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "networks", Description: "Networks of the page, each " + networkFields},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the VPC networks of a project in the custom mode",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","filter":"autoCreateSubnetworks = false"}`),
	}},
}

var networksGet = capability.Descriptor{
	ID: networksGetID, Version: 1, Title: "Get one VPC network",
	Description: "Read one VPC network of a project from Compute Engine, by its name or its numeric ID, with the " +
		"documented fields of the network." + computeTail,
	Tags: []string{"google", "cloud", "compute", "networks", "vpc"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeNetworkProp +
		`},"required":["project_id","network"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(membersSchema(networkMembers)),
	Arguments: []capability.Argument{serviceProjectArgument, {Name: "network", Required: true,
		Description: "Name of the network such as default, or its numeric ID; never a path or URL"}},
	Fields: []capability.Field{{Name: "name", Description: "The network as one object: " + networkFields}},
	Examples: []capability.Example{{
		Description: "Read the default network",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","network":"default"}`),
	}},
}

const computeNetworkProp = `"network":{"type":"string","minLength":1,"maxLength":63,` +
	`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`

type networksAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

// networkResult keeps the documented members of one network, bounded.
func networkResult(op string, raw json.RawMessage) (map[string]any, error) {
	return boundedMembers(op, raw, networkMembers)
}

// boundedMembers keeps the documented members of one resource, bounded.
func boundedMembers(op string, raw json.RawMessage, members []computeMember) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	budget := maxNodes
	bounded, _ := boundNode(pickMembers(row, members), 0, &budget).(map[string]any)
	return bounded, nil
}

func handleNetworksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list networks"
	var arguments computeListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if arguments.IncludeAllScopes {
		return nil, badRequest("include_all_scopes is not an argument of this tool")
	}
	if err := arguments.check(false); err != nil {
		return nil, err
	}
	binding := arguments.binding(networksListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer networksAnswer
	path := computeProjectPath(arguments.ProjectID) + "/global/networks"
	if err := c.get(ctx, op, networksListID, serviceCompute, path, arguments.query(token, 0), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := networkResult(op, item)
		if err != nil {
			return nil, err
		}
		if _, err := computeName(op, row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	out := map[string]any{"networks": rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

func handleNetworksGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get network"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Network   string `json:"network"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	switch {
	case !validProject(arguments.ProjectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeNameOrID(arguments.Network):
		return nil, badRequest("network must be a network name or its numeric ID, never a path")
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire json.RawMessage
	path := computeProjectPath(arguments.ProjectID) + "/global/networks/" + arguments.Network
	if err := c.get(ctx, op, networksGetID, serviceCompute, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := networkResult(op, wire)
	if err != nil {
		return nil, err
	}
	name, err := computeName(op, row)
	if err != nil {
		return nil, err
	}
	if id, _ := row["id"].(string); name != arguments.Network && id != arguments.Network {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}
