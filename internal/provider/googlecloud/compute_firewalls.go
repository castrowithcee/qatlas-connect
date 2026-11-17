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
	firewallsListID = Provider + ".firewalls.list"
	firewallsGetID  = Provider + ".firewalls.get"
)

// firewallMembers are the documented, not input-only members of the Firewall resource of compute v1 (revision
// 20261106). params is left out because it is input only. Source ranges and tags are operating data, not
// secrets, and are passed on as they are, bounded.
var firewallMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"network", "string"}, {"priority", "integer"}, {"sourceRanges", "array"},
	{"destinationRanges", "array"}, {"sourceTags", "array"}, {"targetTags", "array"},
	{"sourceServiceAccounts", "array"}, {"targetServiceAccounts", "array"}, {"allowed", "array"},
	{"denied", "array"}, {"direction", "string"}, {"logConfig", "object"}, {"disabled", "boolean"},
	{"selfLink", "string"},
}

const firewallFields = "{kind, id, creationTimestamp, name, description, network (URL), priority, direction " +
	"(INGRESS or EGRESS), allowed, denied (each with IPProtocol and ports), sourceRanges, destinationRanges, " +
	"sourceTags, targetTags, sourceServiceAccounts, targetServiceAccounts, disabled, logConfig, selfLink}; " +
	"untrusted text"

const computeFirewallProp = `"firewall":{"type":"string","minLength":1,"maxLength":63,` +
	`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`

var firewallsList = capability.Descriptor{
	ID: firewallsListID, Version: 1, Title: "List the firewall rules of a project",
	Description: "List the VPC firewall rules of a project from Compute Engine, with network, direction, priority, " +
		"allowed and denied protocols and ports, source and destination ranges, tags, service accounts, " +
		"disabled state, and log configuration, optionally filtered, sorted, and with partial results " +
		"accepted. Rules cannot be created or changed." + computeTail,
	Tags: []string{"google", "cloud", "compute", "firewalls", "vpc", "network"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps +
		`},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"firewalls":{"type":"array","items":` +
		membersSchema(firewallMembers) + `},"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["firewalls","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeFilterArgument, computeMaxArgument,
		computeOrderArgument, computePartialArgument, computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "firewalls", Description: "Firewall rules of the page, each " + firewallFields},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the ingress rules of a project",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","filter":"direction = INGRESS"}`),
	}},
}

var firewallsGet = capability.Descriptor{
	ID: firewallsGetID, Version: 1, Title: "Get one firewall rule",
	Description: "Read one VPC firewall rule of a project from Compute Engine, by its name or its numeric ID, with " +
		"the documented fields of the rule." + computeTail,
	Tags: []string{"google", "cloud", "compute", "firewalls", "vpc", "network"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeFirewallProp +
		`},"required":["project_id","firewall"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(membersSchema(firewallMembers)),
	Arguments: []capability.Argument{serviceProjectArgument, {Name: "firewall", Required: true,
		Description: "Name of the firewall rule such as default-allow-ssh, or its numeric ID; never a path or URL"}},
	Fields: []capability.Field{{Name: "name", Description: "The firewall rule as one object: " + firewallFields}},
	Examples: []capability.Example{{
		Description: "Read one firewall rule",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","firewall":"default-allow-ssh"}`),
	}},
}

type firewallsAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

func handleFirewallsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list firewalls"
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
	binding := arguments.binding(firewallsListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer firewallsAnswer
	path := computeProjectPath(arguments.ProjectID) + "/global/firewalls"
	if err := c.get(ctx, op, firewallsListID, serviceCompute, path, arguments.query(token, 0), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := boundedMembers(op, item, firewallMembers)
		if err != nil {
			return nil, err
		}
		if _, err := computeName(op, row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	out := map[string]any{"firewalls": rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

func handleFirewallsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get firewall"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Firewall  string `json:"firewall"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	switch {
	case !validProject(arguments.ProjectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeNameOrID(arguments.Firewall):
		return nil, badRequest("firewall must be a firewall rule name or its numeric ID, never a path")
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire json.RawMessage
	path := computeProjectPath(arguments.ProjectID) + "/global/firewalls/" + arguments.Firewall
	if err := c.get(ctx, op, firewallsGetID, serviceCompute, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := boundedMembers(op, wire, firewallMembers)
	if err != nil {
		return nil, err
	}
	name, err := computeName(op, row)
	if err != nil {
		return nil, err
	}
	if id, _ := row["id"].(string); name != arguments.Firewall && id != arguments.Firewall {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}
