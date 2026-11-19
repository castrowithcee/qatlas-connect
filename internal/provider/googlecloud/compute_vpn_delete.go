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

const (
	vpnTunnelsDeleteID  = Provider + ".vpn-tunnels.delete"
	vpnGatewaysDeleteID = Provider + ".vpn-gateways.delete"
)

// vpnDelete describes one deleting VPN tool: what it deletes, the argument that names it, and the path segment.
type vpnDelete struct {
	id, title, kind, argument, segment, prop, effect string
}

var (
	vpnTunnelDelete = vpnDelete{id: vpnTunnelsDeleteID, title: "Delete a VPN tunnel", kind: "VPN tunnel",
		argument: "vpn_tunnel", segment: "vpnTunnels", prop: computeVpnTunnelProp,
		effect: "The tunnel is gone afterwards and the traffic it carried stops; it cannot be restored."}
	vpnGatewayDelete = vpnDelete{id: vpnGatewaysDeleteID, title: "Delete an HA VPN gateway", kind: "HA VPN gateway",
		argument: "vpn_gateway", segment: "vpnGateways", prop: computeVpnGatewayProp,
		effect: "The gateway is gone afterwards and its interfaces' external IP addresses are released; Google " +
			"refuses a gateway that tunnels still use. It cannot be restored."}
)

func (v vpnDelete) descriptor() capability.Descriptor {
	return capability.Descriptor{
		ID: v.id, Version: 1, Title: v.title,
		Description: "Delete one " + v.kind + " of a region of a project (" + v.segment + ".delete of Compute " +
			"Engine v1), by its name or numeric ID. " + v.effect + " Tunnels and gateways cannot be created or " +
			"changed." + listedOnlyNote + computeDeleteNote + vpnSecretNote + computeTail,
		Tags: []string{"google", "cloud", "compute", "vpn", "network", "delete"}, Risk: computeDeleteRisk,
		Provider: Provider, RequiresToolAllowList: true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeRegionProp +
			`,` + v.prop + `,` + computeRequestIDProp + `},"required":["project_id","region","` + v.argument +
			`"],"additionalProperties":false}`),
		OutputSchema: computeDeleteOutput,
		Arguments: []capability.Argument{serviceProjectArgument, computeRegionRequired,
			{Name: v.argument, Required: true, Description: "Name of the " + v.kind + ", or its numeric ID; never a " +
				"path or URL"},
			computeRequestIDArgument},
		Fields: computeDeleteOperationFields,
		Examples: []capability.Example{{
			Description: "Delete a " + v.kind,
			Arguments: json.RawMessage(`{"project_id":"my-project-123","region":"us-central1","` + v.argument +
				`":"example-vpn"}`),
		}},
	}
}

var (
	vpnTunnelsDelete  = vpnTunnelDelete.descriptor()
	vpnGatewaysDelete = vpnGatewayDelete.descriptor()
)

func (v vpnDelete) handler(op string) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, failure(op, "the validated arguments could not be read")
		}
		project, region, name := m["project_id"], m["region"], m[v.argument]
		switch {
		case !validProject(project):
			return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
		case !validComputeName(region):
			return nil, badRequest("region must be a region name such as us-central1, never a path")
		case !validComputeNameOrID(name):
			return nil, badRequest(v.argument + " must be a " + v.kind + " name or its numeric ID, never a path")
		}
		requestID, err := checkRequestID(m["request_id"])
		if err != nil {
			return nil, err
		}
		c, err := computeClient(ctx, op, project, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		path := computeRegionPath(project, region) + "/" + v.segment + "/" + url.PathEscape(name)
		return c.computeDelete(ctx, computeDeleteTarget{op: op, tool: v.id, path: path, region: region,
			requestID: requestID, noMessages: true})
	}
}

// summary phrases the deletion from its arguments: project, type, region, and name.
func (v vpnDelete) summary(raw json.RawMessage) string {
	var m map[string]string
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return "Delete " + v.kind + " " + boundedText(m[v.argument], maxNameText) + " in region " +
		boundedText(m["region"], maxNameText) + " of project " + boundedText(m["project_id"], maxNameText) +
		": " + v.effect
}
