package googlecloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	vpnGatewaysListID = Provider + ".vpn-gateways.list"
	vpnGatewaysGetID  = Provider + ".vpn-gateways.get"
	vpnTunnelsListID  = Provider + ".vpn-tunnels.list"
	vpnTunnelsGetID   = Provider + ".vpn-tunnels.get"
)

// vpnGatewayMembers are the documented, not input-only members of the VpnGateway resource of compute v1
// (revision 20261106). params is left out because it is input only.
var vpnGatewayMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"region", "string"}, {"network", "string"}, {"vpnInterfaces", "array"},
	{"selfLink", "string"}, {"labels", "object"}, {"labelFingerprint", "string"}, {"stackType", "string"},
	{"gatewayIpVersion", "string"},
}

// vpnTunnelMembers are the documented, not input-only members of the VpnTunnel resource of compute v1
// (revision 20261106). params is left out because it is input only. sharedSecret and sharedSecretHash are left
// out for a security reason: the secret of the tunnel never leaves the tool, in no form.
var vpnTunnelMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"region", "string"}, {"targetVpnGateway", "string"}, {"vpnGateway", "string"},
	{"vpnGatewayInterface", "integer"}, {"peerExternalGateway", "string"},
	{"peerExternalGatewayInterface", "integer"}, {"peerGcpGateway", "string"}, {"router", "string"},
	{"peerIp", "string"}, {"ikeVersion", "integer"}, {"status", "string"}, {"detailedStatus", "string"},
	{"localTrafficSelector", "array"}, {"remoteTrafficSelector", "array"}, {"labels", "object"},
	{"labelFingerprint", "string"}, {"cipherSuite", "object"}, {"selfLink", "string"},
}

const vpnGatewayFields = "{kind, id, creationTimestamp, name, description, region (URL), network (URL), " +
	"vpnInterfaces (each with id, ipAddress, ipv6Address, interconnectAttachment), selfLink, labels, " +
	"labelFingerprint, stackType, gatewayIpVersion}; untrusted text"

const vpnTunnelFields = "{kind, id, creationTimestamp, name, description, region (URL), targetVpnGateway (URL, " +
	"classic tunnels only), vpnGateway (URL), vpnGatewayInterface, peerExternalGateway (URL), " +
	"peerExternalGatewayInterface, peerGcpGateway (URL), router (URL), peerIp, ikeVersion, status, " +
	"detailedStatus, localTrafficSelector, remoteTrafficSelector, labels, labelFingerprint, cipherSuite, " +
	"selfLink}; untrusted text"

// vpnSecretNote ends the description of every VPN tunnel tool.
const vpnSecretNote = " The shared secret of a tunnel (sharedSecret) and its hash (sharedSecretHash) are never " +
	"returned, not in the result and not in an error."

const (
	computeVpnGatewayProp = `"vpn_gateway":{"type":"string","minLength":1,"maxLength":63,` +
		`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`
	computeVpnTunnelProp = `"vpn_tunnel":{"type":"string","minLength":1,"maxLength":63,` +
		`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`
)

// vpnListInput is the input schema of both VPN list tools.
const vpnListInput = `{"type":"object","properties":{` + computeListProps + `,` + computeRegionProp +
	`},"required":["project_id","region"],"additionalProperties":false}`

func vpnListOutput(key string, members []computeMember) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"` + key + `":{"type":"array","items":` +
		membersSchema(members) + `},"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["` + key + `","has_more"],"additionalProperties":false}`)
}

var vpnListArgs = []capability.Argument{serviceProjectArgument, computeRegionRequired, computeFilterArgument,
	computeMaxArgument, computeOrderArgument, computePartialArgument, computeCursorArgument}

func vpnListFields(key, what, fields string) []capability.Field {
	return append([]capability.Field{
		{Name: key, Description: what + " of the page, each " + fields},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...)
}

var vpnGatewaysList = capability.Descriptor{
	ID: vpnGatewaysListID, Version: 1, Title: "List the HA VPN gateways of a region",
	Description: "List the HA VPN gateways of one region of a project from Compute Engine, with their network, " +
		"interfaces (IP addresses and interconnect attachments), stack type, IP version, and labels, optionally " +
		"filtered, sorted, and with partial results accepted. Classic target VPN gateways, external VPN " +
		"gateways, and aggregated lists over all regions are not offered." + computeTail,
	Tags: []string{"google", "cloud", "compute", "vpn", "vpn-gateways"}, Risk: readRisk, Provider: Provider,
	InputSchema:  json.RawMessage(vpnListInput),
	OutputSchema: vpnListOutput("vpn_gateways", vpnGatewayMembers),
	Arguments:    vpnListArgs,
	Fields:       vpnListFields("vpn_gateways", "HA VPN gateways", vpnGatewayFields),
	Examples: []capability.Example{{
		Description: "List the HA VPN gateways of a region",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","region":"us-central1"}`),
	}},
}

var vpnGatewaysGet = capability.Descriptor{
	ID: vpnGatewaysGetID, Version: 1, Title: "Get one HA VPN gateway",
	Description: "Read one HA VPN gateway of a region of a project from Compute Engine, by its name or its " +
		"numeric ID, with the documented fields of the gateway." + computeTail,
	Tags: []string{"google", "cloud", "compute", "vpn", "vpn-gateways"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeRegionProp +
		`,` + computeVpnGatewayProp + `},"required":["project_id","region","vpn_gateway"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(membersSchema(vpnGatewayMembers)),
	Arguments: []capability.Argument{serviceProjectArgument, computeRegionRequired,
		{Name: "vpn_gateway", Required: true, Description: "Name of the HA VPN gateway such as ha-vpn-a, or its " +
			"numeric ID; never a path or URL"}},
	Fields: []capability.Field{{Name: "name", Description: "The HA VPN gateway as one object: " + vpnGatewayFields}},
	Examples: []capability.Example{{
		Description: "Read one HA VPN gateway",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","region":"us-central1","vpn_gateway":"ha-vpn-a"}`),
	}},
}

var vpnTunnelsList = capability.Descriptor{
	ID: vpnTunnelsListID, Version: 1, Title: "List the VPN tunnels of a region",
	Description: "List the VPN tunnels of one region of a project from Compute Engine, with their status, " +
		"detailed status, peer address, peer gateway, IKE version, router, traffic selectors, VPN gateway and " +
		"interface, and cipher suite, optionally filtered, sorted, and with partial results accepted. Aggregated " +
		"lists over all regions are not offered." + vpnSecretNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "vpn", "vpn-tunnels"}, Risk: readRisk, Provider: Provider,
	InputSchema:  json.RawMessage(vpnListInput),
	OutputSchema: vpnListOutput("vpn_tunnels", vpnTunnelMembers),
	Arguments:    vpnListArgs,
	Fields:       vpnListFields("vpn_tunnels", "VPN tunnels", vpnTunnelFields),
	Examples: []capability.Example{{
		Description: "List the established VPN tunnels of a region",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","region":"us-central1","filter":"status = ESTABLISHED"}`),
	}},
}

var vpnTunnelsGet = capability.Descriptor{
	ID: vpnTunnelsGetID, Version: 1, Title: "Get one VPN tunnel",
	Description: "Read one VPN tunnel of a region of a project from Compute Engine, by its name or its numeric " +
		"ID, with the documented fields of the tunnel." + vpnSecretNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "vpn", "vpn-tunnels"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeRegionProp +
		`,` + computeVpnTunnelProp + `},"required":["project_id","region","vpn_tunnel"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(membersSchema(vpnTunnelMembers)),
	Arguments: []capability.Argument{serviceProjectArgument, computeRegionRequired,
		{Name: "vpn_tunnel", Required: true, Description: "Name of the VPN tunnel such as tunnel-a, or its " +
			"numeric ID; never a path or URL"}},
	Fields: []capability.Field{{Name: "name", Description: "The VPN tunnel as one object: " + vpnTunnelFields}},
	Examples: []capability.Example{{
		Description: "Read one VPN tunnel",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","region":"us-central1","vpn_tunnel":"tunnel-a"}`),
	}},
}

// vpnListArguments are the arguments of a VPN list: the shared ones and the region.
type vpnListArguments struct {
	computeListArguments
	Region string `json:"region"`
}

// check refuses an argument that is not in its documented form. The shared check runs without a zone.
func (a vpnListArguments) check() error {
	if err := a.computeListArguments.check(false); err != nil {
		return err
	}
	if !validComputeName(a.Region) {
		return badRequest("region must be a region name such as us-central1, never a path")
	}
	return nil
}

// binding ties a cursor to the tool, the targets, the region, and everything that shapes the list.
func (a vpnListArguments) binding(tool string, resolved *config.Resolved) []byte {
	return provider.CursorBinding(tool, provider.TargetsOf(resolved), a.ProjectID, "region/"+a.Region, a.Filter,
		a.OrderBy, a.ReturnPartialSuccess)
}

// vpnSecretKey reports whether a member name is one of the tunnel secrets, in any letter case.
func vpnSecretKey(key string) bool {
	return strings.EqualFold(key, "sharedSecret") || strings.EqualFold(key, "sharedSecretHash")
}

// dropVpnSecrets removes the secret members of a decoded value at every depth, before anything else is done
// with it.
func dropVpnSecrets(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, member := range value {
			if vpnSecretKey(key) {
				delete(value, key)
				continue
			}
			dropVpnSecrets(member)
		}
	case []any:
		for _, item := range value {
			dropVpnSecrets(item)
		}
	}
}

// vpnRow keeps the documented members of one VPN resource, bounded, after the secret members are dropped
// at every depth.
func vpnRow(op string, raw json.RawMessage, members []computeMember) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	dropVpnSecrets(row)
	budget := maxNodes
	bounded, _ := boundNode(pickMembers(row, members), 0, &budget).(map[string]any)
	return bounded, nil
}

// vpnGet sends the GET of a VPN tool. A failed answer keeps the status, type, and code of Google's error, but not
// its message: the text of an error is third-party text that could echo a request or a resource, so a VPN tool
// never passes it on, not even as the original of the error.
func vpnGet(ctx context.Context, c *client, op, tool, path string, query url.Values, out any) error {
	err := c.get(ctx, op, tool, serviceCompute, path, query, out)
	var failed *provider.Error
	if errors.As(err, &failed) && failed.Original != nil {
		scrubbed := *failed.Original
		scrubbed.Entries = make([]provider.OriginalEntry, len(failed.Original.Entries))
		for i, entry := range failed.Original.Entries {
			entry.Message = ""
			scrubbed.Entries[i] = entry
		}
		copied := *failed
		copied.Original = &scrubbed
		return &copied
	}
	return err
}

type vpnAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

// handleVpnList is the list of both VPN resources: resource is the path member, key the member of the result.
func handleVpnList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool, resource, key string, members []computeMember) (any, error) {
	var arguments vpnListArguments
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
	binding := arguments.binding(tool, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer vpnAnswer
	path := computeRegionPath(arguments.ProjectID, arguments.Region) + "/" + resource
	if err := vpnGet(ctx, c, op, tool, path, arguments.query(token, 0), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := vpnRow(op, item, members)
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
	out := map[string]any{key: rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		dropVpnSecrets(warning)
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

// handleVpnGet is the get of both VPN resources.
func handleVpnGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool, resource, member, noun string, members []computeMember) (any, error) {
	var arguments map[string]string
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	projectID, region, target := arguments["project_id"], arguments["region"], arguments[member]
	switch {
	case !validProject(projectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(region):
		return nil, badRequest("region must be a region name such as us-central1, never a path")
	case !validComputeNameOrID(target):
		return nil, badRequest(member + " must be a " + noun + " name or its numeric ID, never a path")
	}
	c, err := computeClient(ctx, op, projectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire json.RawMessage
	path := computeRegionPath(projectID, region) + "/" + resource + "/" + target
	if err := vpnGet(ctx, c, op, tool, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := vpnRow(op, wire, members)
	if err != nil {
		return nil, err
	}
	name, err := computeName(op, row)
	if err != nil {
		return nil, err
	}
	if id, _ := row["id"].(string); name != target && id != target || !inRegion(row, region) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}

func handleVpnGatewaysList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return handleVpnList(ctx, resolved, secrets, red, raw, "list VPN gateways", vpnGatewaysListID, "vpnGateways",
		"vpn_gateways", vpnGatewayMembers)
}

func handleVpnGatewaysGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return handleVpnGet(ctx, resolved, secrets, red, raw, "get VPN gateway", vpnGatewaysGetID, "vpnGateways",
		"vpn_gateway", "VPN gateway", vpnGatewayMembers)
}

func handleVpnTunnelsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return handleVpnList(ctx, resolved, secrets, red, raw, "list VPN tunnels", vpnTunnelsListID, "vpnTunnels",
		"vpn_tunnels", vpnTunnelMembers)
}

func handleVpnTunnelsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return handleVpnGet(ctx, resolved, secrets, red, raw, "get VPN tunnel", vpnTunnelsGetID, "vpnTunnels",
		"vpn_tunnel", "VPN tunnel", vpnTunnelMembers)
}
