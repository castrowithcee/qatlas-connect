package application

import (
	"encoding/json"
	"fmt"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// OperationRequest is the reserved operation that asks for tools a connection does not offer. A tool ID has
// at least three segments, so this two-segment name can never be one, like the reserved names of the
// invocation log. Invoke answers it before looking at the registry, and Describe publishes its fixed contract.
const OperationRequest = "qconnect.request"

var requestInputSchema = json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
	`"kind":{"type":"string","enum":["add-tools","remove-tools"],"default":"add-tools",`+
	`"description":"add-tools asks for tools the connection does not offer; remove-tools asks that the connection stop offering tools it offers now"},`+
	`"tools":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":%d,`+
	`"description":"Tool IDs, or groups as prefixes of the form <provider>.<segment>.*, that the connection does not offer (add-tools) or offers (remove-tools); ask only for what the task needs"},`+
	`"reason":{"type":"string","description":"Why the task needs them, in a short sentence; the user reads it as untrusted text, and it is never logged"}},`+
	`"required":["tools"],"additionalProperties":false}`, accessrequest.MaxTools))

var requestOutputSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"id":{"type":"string","description":"ID of the open request"},` +
	`"kind":{"type":"string","enum":["add-tools","remove-tools"],"description":"The kind of the open request"},` +
	`"connection":{"type":"string","description":"Name of the connection the request names"},` +
	`"tools":{"type":"array","items":{"type":"string"},"description":"Every tool the open request holds, an earlier request of the connection included"},` +
	`"status":{"type":"string","enum":["open"],"description":"open: a person has not decided yet"},` +
	`"expires_at":{"type":"string","description":"When the request lapses unless a person decides"}},` +
	`"required":["id","kind","connection","tools","status","expires_at"],"additionalProperties":false}`)

// requestDescriptor is the fixed contract of OperationRequest. It belongs to no provider and no connection
// offers it, so discovery never lists it as a tool.
func requestDescriptor() capability.Descriptor {
	return capability.Descriptor{
		ID: OperationRequest, Version: 1, Title: "Request tools",
		Description: "Ask the user for tools the connection does not offer (kind add-tools, the default), or ask " +
			"that a connection stop offering tools it offers now (kind remove-tools), for example to narrow it. " +
			"A request changes nothing by itself: " +
			"it stays open until the user decides, so stop and tell the user what you asked for, and look at the " +
			"tools of the connection later to see whether they are offered.",
		Tags: []string{"request"},
		Risk: capability.Risk{
			Effect: capability.EffectCreate, Idempotency: capability.IdempotencyIdempotent,
			Confirmation: capability.ConfirmationNone, DataSensitivity: "none",
		},
		InputSchema: requestInputSchema, OutputSchema: requestOutputSchema,
		Arguments: []capability.Argument{
			{Name: "kind", Description: "add-tools (default) or remove-tools"},
			{Name: "tools", Required: true, Description: "Tool IDs or <provider>.<segment>.* prefixes to request"},
			{Name: "reason", Description: "Short reason for the user"},
		},
		Fields: []capability.Field{
			{Name: "id", Description: "ID of the open request"},
			{Name: "kind", Description: "add-tools or remove-tools"},
			{Name: "connection", Description: "Name of the connection the request names"},
			{Name: "tools", Description: "Every tool the open request holds"},
			{Name: "status", Description: "open until the user decides"},
			{Name: "expires_at", Description: "When the request lapses"},
		},
		Examples: []capability.Example{{
			Description: "Ask for one tool of a group the connection leaves out",
			Arguments:   json.RawMessage(`{"tools":["example.pages.delete"],"reason":"The task removes an obsolete page"}`),
		}, {
			Description: "Ask that the connection stop offering a group of tools it offers",
			Arguments:   json.RawMessage(`{"kind":"remove-tools","tools":["example.admin.*"],"reason":"The task needs no admin tools"}`),
		}},
	}
}

// describeRequest answers describe for OperationRequest. A connection narrows nothing here, since the
// request is about tools no connection offers; it is only checked to exist, like for any tool.
func (c *Core) describeRequest(request DescribeRequest) (DescribeResponse, error) {
	if request.Version != 0 && request.Version != 1 {
		return DescribeResponse{}, &UnknownOperationError{Operation: OperationRequest}
	}
	connections := []ConnectionRef{}
	if request.Connection != "" {
		name, err := c.connectionName(request.Connection)
		if err != nil {
			return DescribeResponse{}, unknownConnectionOf(err, "", OperationRequest)
		}
		connections = append(connections, c.connectionRef(name))
	}
	return DescribeResponse{Operation: PublishedDescriptor(requestDescriptor()), Connections: connections}, nil
}

// invokeRequest runs OperationRequest: it validates the arguments against the fixed contract and stores the
// request. It is no tool call, so it writes the request entries of the queue and no invoke entry.
func (c *Core) invokeRequest(request InvokeRequest) (InvokeResponse, error) {
	c.scope()
	if request.Version != 0 && request.Version != 1 {
		return InvokeResponse{}, &UnknownOperationError{Operation: OperationRequest}
	}
	if len(request.Fields) > 0 {
		return InvokeResponse{}, &InvalidRequestError{Message: "fields does not apply to " + OperationRequest}
	}
	if len(request.Arguments) == 0 {
		request.Arguments = json.RawMessage(`{}`)
	}
	if err := ValidateJSON(requestInputSchema, request.Arguments); err != nil {
		return InvokeResponse{}, &InvalidRequestError{Message: err.Error()}
	}
	var arguments struct {
		Kind   string   `json:"kind"`
		Tools  []string `json:"tools"`
		Reason string   `json:"reason"`
	}
	if err := json.Unmarshal(request.Arguments, &arguments); err != nil {
		return InvokeResponse{}, &InvalidRequestError{Message: "arguments do not satisfy the request contract"}
	}
	response, err := c.RequestTools(ToolRequest{
		Kind: arguments.Kind, Connection: request.Connection, Tools: arguments.Tools, Reason: arguments.Reason,
	})
	if err != nil {
		return InvokeResponse{}, err
	}
	result, err := json.Marshal(response)
	if err != nil {
		return InvokeResponse{}, fmt.Errorf("encode request response: %w", err)
	}
	return InvokeResponse{Operation: OperationRequest, Version: 1, Connection: response.Connection, Result: result}, nil
}
