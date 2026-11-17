package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const instancesDeleteID = Provider + ".instances.delete"

// instanceWriteOutput is the output schema of the tools that create or delete an instance: the zonal Compute
// operation and what reads its state.
var instanceWriteOutput = json.RawMessage(`{"type":"object","properties":{"operation":` +
	membersSchema(computeOperationMembers) + `,"scope":{"type":"string","enum":["zone"]},` +
	`"zone":{"type":"string"},"request_id":{"type":"string"}},` +
	`"required":["operation","scope","zone","request_id"],"additionalProperties":false}`)

var instanceWriteFields = []capability.Field{
	{Name: "operation", Description: "The Compute operation: " + computeOperationFields},
	{Name: "scope", Description: "Scope of the operation for compute-operations.get: always zone"},
	{Name: "zone", Description: "Zone of the operation for compute-operations.get"},
	{Name: "request_id", Description: "The request_id sent to Google: the one given, or the one the tool created"},
}

// zonalChange sends the one request of an instance tool that returns a zonal operation. It is never repeated,
// and an answer that names no operation of the zone leaves the outcome open.
func (c *client) zonalChange(ctx context.Context, op, tool, method, path, requestID, zone string,
	payload []byte) (any, error) {
	var wire json.RawMessage
	query := url.Values{"requestId": {requestID}}
	if err := c.send(ctx, op, tool, serviceCompute, method, path, query, payload, &wire, true); err != nil {
		return nil, err
	}
	unsure := func() error {
		failed := provider.InvalidResponse(op, subject+" returned an invalid response")
		failed.Message += unsureNote
		return failed
	}
	row, err := computeOperationResult(op, wire)
	if err != nil {
		return nil, unsure()
	}
	if _, err := computeName(op, row); err != nil {
		return nil, unsure()
	}
	if !inScope(row, scopeZone, zone, "") {
		return nil, unsure()
	}
	if status, _ := row["status"].(string); status == "DONE" {
		if failed := computeOperationError(op, wire); failed != nil {
			return nil, failed
		}
	}
	return map[string]any{"operation": row, "scope": scopeZone, "zone": zone, "request_id": requestID}, nil
}

const instanceWriteNote = " Needs the confirmation of a person for each call. The project, zone, and names are " +
	"checked against their form and the targets before any request. Google answers with a Compute operation and " +
	"the tool never waits for it: the result is the operation as Google returned it, and compute-operations.get " +
	"with scope zone and the zone of the result reads its state; an operation that is already done with an error " +
	"(a quota, for example) is reported as a provider error. The call is never repeated by the tool; after an " +
	"unclear outcome, read the instance with instances.get, or repeat the call with the same request_id."

var instancesDelete = capability.Descriptor{
	ID: instancesDeleteID, Version: 1, Title: "Delete a Compute Engine instance",
	Description: "Delete one virtual machine instance of a zone of a project (instances.delete of Compute Engine " +
		"v1), by its name or numeric ID. The instance is gone afterwards, and so are the disks that are set to " +
		"autoDelete, which is the default of a boot disk; this cannot be undone. An instance with " +
		"deletionProtection is refused by Google. Offered only where a connection's tools list names it." +
		instanceWriteNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "instances", "delete"}, Risk: computeDeleteRisk, Provider: Provider,
	RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeZoneProp + `,` +
		`"instance":{"type":"string","minLength":1,"maxLength":63,` +
		`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"},` + computeRequestIDProp +
		`},"required":["project_id","zone","instance"],"additionalProperties":false}`),
	OutputSchema: instanceWriteOutput,
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument,
		{Name: "instance", Required: true, Description: "Name of the instance such as my-vm, or its numeric ID; " +
			"never a path or URL"}, computeRequestIDArgument},
	Fields: instanceWriteFields,
	Examples: []capability.Example{{
		Description: "Delete one instance",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","instance":"my-vm"}`),
	}},
}

type instanceDeleteArguments struct {
	ProjectID string `json:"project_id"`
	Zone      string `json:"zone"`
	Instance  string `json:"instance"`
	RequestID string `json:"request_id"`
}

func handleInstancesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete instance"
	var a instanceDeleteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	switch {
	case !validProject(a.ProjectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(a.Zone):
		return nil, badRequest("zone must be a zone name such as us-central1-a, never a path")
	case !validComputeNameOrID(a.Instance):
		return nil, badRequest("instance must be an instance name or its numeric ID, never a path")
	}
	requestID, err := checkRequestID(a.RequestID)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := computeZonePath(a.ProjectID, a.Zone) + "/instances/" + url.PathEscape(a.Instance)
	return c.zonalChange(ctx, op, instancesDeleteID, http.MethodDelete, path, requestID, a.Zone, nil)
}

// instanceDeleteSummary phrases the deletion from its arguments alone.
func instanceDeleteSummary(raw json.RawMessage) string {
	var a instanceDeleteArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Delete instance " + boundedText(a.Instance, maxNameText) + " in zone " + boundedText(a.Zone, maxNameText) +
		" of project " + boundedText(a.ProjectID, maxNameText) + " for good: the instance and its disks that are " +
		"set to autoDelete are removed and cannot be restored"
}
