package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const firewallsDeleteID = Provider + ".firewalls.delete"

const (
	maxFirewallPriority = 65535
	// The documented defaults of a rule that leaves direction and priority out.
	defaultFirewallDirection = "INGRESS"
	defaultFirewallPriority  = 1000
)

const firewallDeleteProps = serviceProjectProp + `,` + computeFirewallProp + `,` +
	`"expected_direction":{"type":"string","enum":["INGRESS","EGRESS"]},` +
	`"expected_action":{"type":"string","enum":["allow","deny"]},` +
	`"expected_priority":{"type":"integer","minimum":0,"maximum":65535},` + computeRequestIDProp

var firewallsDelete = capability.Descriptor{
	ID: firewallsDeleteID, Version: 1, Title: "Delete a firewall rule",
	Description: "Delete one VPC firewall rule of a project (firewalls.delete of Compute Engine v1), by its name or " +
		"numeric ID. The rule is gone afterwards and cannot be restored; deleting a deny rule can open access that " +
		"it blocked, and deleting an allow rule can cut access. The confirmation names the direction, action, and " +
		"priority you expect: after the confirmation the tool reads the rule and deletes it only if direction, " +
		"action (deny when the rule denies something, otherwise allow), and priority still match; otherwise it " +
		"fails without deleting, and the rule is to be read again with firewalls.get. Rules cannot be created or " +
		"changed." + computeDeleteNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "firewalls", "vpc", "network", "delete"}, Risk: computeDeleteRisk,
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + firewallDeleteProps +
		`},"required":["project_id","firewall","expected_direction","expected_action","expected_priority"],` +
		`"additionalProperties":false}`),
	OutputSchema: computeDeleteOutput,
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "firewall", Required: true, Description: "Name of the firewall rule such as allow-ssh, or its " +
			"numeric ID; never a path or URL"},
		{Name: "expected_direction", Required: true, Description: "INGRESS or EGRESS: the direction the rule has " +
			"according to firewalls.get; only compared, never sent"},
		{Name: "expected_action", Required: true, Description: "allow or deny: the action the rule has according " +
			"to firewalls.get (deny when it has denied entries); only compared, never sent"},
		{Name: "expected_priority", Required: true, Description: "Priority 0 to 65535 the rule has according to " +
			"firewalls.get; only compared, never sent"},
		computeRequestIDArgument},
	Fields: computeDeleteOperationFields,
	Examples: []capability.Example{{
		Description: "Delete an ingress allow rule after reading it",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","firewall":"allow-ssh",` +
			`"expected_direction":"INGRESS","expected_action":"allow","expected_priority":1000}`),
	}},
}

type firewallDeleteArguments struct {
	ProjectID         string `json:"project_id"`
	Firewall          string `json:"firewall"`
	ExpectedDirection string `json:"expected_direction"`
	ExpectedAction    string `json:"expected_action"`
	ExpectedPriority  *int   `json:"expected_priority"`
	RequestID         string `json:"request_id"`
}

// check refuses an argument that is not in its documented form, before any secret is read.
func (a firewallDeleteArguments) check() error {
	switch {
	case !validProject(a.ProjectID):
		return badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeNameOrID(a.Firewall):
		return badRequest("firewall must be a firewall rule name or its numeric ID, never a path")
	case a.ExpectedDirection != "INGRESS" && a.ExpectedDirection != "EGRESS":
		return badRequest("expected_direction must be INGRESS or EGRESS")
	case a.ExpectedAction != "allow" && a.ExpectedAction != "deny":
		return badRequest("expected_action must be allow or deny")
	case a.ExpectedPriority == nil || *a.ExpectedPriority < 0 || *a.ExpectedPriority > maxFirewallPriority:
		return badRequest("expected_priority must be 0 to " + strconv.Itoa(maxFirewallPriority))
	}
	return nil
}

// ruleAction is the action of a rule as firewalls.get returned it: deny when it has denied entries.
func ruleAction(row map[string]any) string {
	if denied, _ := row["denied"].([]any); len(denied) > 0 {
		return "deny"
	}
	return "allow"
}

func handleFirewallsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete firewall"
	var a firewallDeleteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	requestID, err := checkRequestID(a.RequestID)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	// The rule is read after the confirmation and compared with what the person confirmed. The read uses the
	// token of this tool, whose scope covers it.
	path := computeProjectPath(a.ProjectID) + "/global/firewalls/" + url.PathEscape(a.Firewall)
	var wire json.RawMessage
	if err := c.get(ctx, op, firewallsDeleteID, serviceCompute, path, nil, &wire); err != nil {
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
	if id, _ := row["id"].(string); name != a.Firewall && id != a.Firewall {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	direction, _ := row["direction"].(string)
	if direction == "" {
		direction = defaultFirewallDirection
	}
	priority := float64(defaultFirewallPriority)
	if value, ok := row["priority"].(float64); ok {
		priority = value
	}
	if direction != a.ExpectedDirection || ruleAction(row) != a.ExpectedAction || priority != float64(*a.ExpectedPriority) {
		return nil, badRequest("the firewall rule no longer matches expected_direction, expected_action, and " +
			"expected_priority, so nothing was deleted; read the rule again with firewalls.get and confirm what " +
			"you want to delete")
	}
	return c.computeDelete(ctx, computeDeleteTarget{op: op, tool: firewallsDeleteID, path: path, requestID: requestID})
}

// firewallDeleteSummary phrases the deletion from its arguments alone: the project, the rule, and the
// direction, action, and priority the person confirms. A deny rule is named as one that can open access.
func firewallDeleteSummary(raw json.RawMessage) string {
	var a firewallDeleteArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	priority := "?"
	if a.ExpectedPriority != nil {
		priority = strconv.Itoa(*a.ExpectedPriority)
	}
	out := "Delete firewall rule " + boundedText(a.Firewall, maxNameText) + " in project " +
		boundedText(a.ProjectID, maxNameText) + " (direction " + boundedText(a.ExpectedDirection, maxNameText) +
		", action " + boundedText(a.ExpectedAction, maxNameText) + ", priority " + priority + ")"
	if a.ExpectedAction == "deny" {
		out += ": deleting a deny rule can open access that it blocked"
	} else {
		out += ": deleting an allow rule can cut access that it granted"
	}
	return out + "; the rule is deleted only if it still has this direction, action, and priority"
}
