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

const (
	instancesStartID          = Provider + ".instances.start"
	instancesStopID           = Provider + ".instances.stop"
	instancesResetID          = Provider + ".instances.reset"
	instancesSetMachineTypeID = Provider + ".instances.set-machine-type"
)

// This file holds the four tools that change the state of an existing instance. Each sends one POST that is
// never repeated and returns the Compute operation as Google answered; compute-operations.get with scope zone
// reads its state.

// instanceLifecycleRisk is the risk of every tool of this file. requestId makes a repeated call idempotent:
// Google ignores a request whose ID it already received.
var instanceLifecycleRisk = capability.Risk{
	Effect:          capability.EffectUpdate,
	Idempotency:     capability.IdempotencyIdempotent,
	Confirmation:    capability.ConfirmationRequired,
	OpenWorld:       true,
	DataSensitivity: computeOperationSensitivity,
}

var instanceLifecycleOutput = json.RawMessage(`{"type":"object","properties":{"operation":` +
	membersSchema(computeOperationMembers) + `,"scope":{"type":"string","enum":["zone"]},` +
	`"zone":{"type":"string"},"request_id":{"type":"string"}},` +
	`"required":["operation","scope","zone","request_id"],"additionalProperties":false}`)

var instanceLifecycleFields = []capability.Field{
	{Name: "operation", Description: "The Compute operation: " + computeOperationFields},
	{Name: "scope", Description: "Always zone: the scope of the operation for compute-operations.get"},
	{Name: "zone", Description: "Zone of the operation for compute-operations.get"},
	{Name: "request_id", Description: "The request_id sent to Google: the one given, or the one the tool created"},
}

const instanceLifecycleNote = " Needs the confirmation of a person for each call, which names the project, " +
	"zone, instance, and the effect. The project, zone, and instance are checked against their form and the " +
	"targets before any request. Google answers with a Compute operation and the tool never waits for it: the " +
	"result is the operation as Google returned it, and compute-operations.get with scope zone and the zone of " +
	"the result reads its state; an operation that is already done with an error is reported as a provider " +
	"error. The call is never repeated by the tool; after an unclear outcome, read the instance with " +
	"instances.get, or repeat the call with the same request_id."

const instanceLifecycleProps = serviceProjectProp + `,` + computeZoneProp + `,` +
	`"instance":{"type":"string","minLength":1,"maxLength":63,"pattern":"^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$"}`

var instanceArgument = capability.Argument{Name: "instance", Required: true,
	Description: "Name of the instance such as my-vm; never a path, URL, or numeric ID"}

var instanceRequestIDArgument = capability.Argument{Name: "request_id", Description: "UUID that identifies " +
	"the change. Repeating the call with the same request_id is safe: Google ignores a request whose ID it " +
	"already received. Without it the tool creates one and returns it as request_id; the all-zero UUID is not " +
	"allowed"}

func instanceLifecycleDescriptor(id, title, description, tag, extraProps string, extra []capability.Argument,
	required string, example string) capability.Descriptor {
	args := append([]capability.Argument{serviceProjectArgument, computeZoneArgument, instanceArgument}, extra...)
	return capability.Descriptor{
		ID: id, Version: 1, Title: title,
		Description: description + instanceLifecycleNote + computeTail,
		Tags:        []string{"google", "cloud", "compute", "instances", tag}, Risk: instanceLifecycleRisk,
		Provider: Provider,
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + instanceLifecycleProps + extraProps + `,` +
			computeRequestIDProp + `},"required":["project_id","zone","instance"` + required +
			`],"additionalProperties":false}`),
		OutputSchema: instanceLifecycleOutput,
		Arguments:    append(args, instanceRequestIDArgument),
		Fields:       instanceLifecycleFields,
		Examples: []capability.Example{{Description: title,
			Arguments: json.RawMessage(example)}},
	}
}

const instanceExampleBase = `"project_id":"my-project-123","zone":"us-central1-a","instance":"my-vm"`

var (
	instancesStart = instanceLifecycleDescriptor(instancesStartID, "Start a Compute Engine instance",
		"Start one stopped instance of a zone of a project (instances.start of Compute Engine v1), by its name. "+
			"A running instance is billed for its CPUs, memory, and attached resources until it is stopped.",
		"start", "", nil, "", `{`+instanceExampleBase+`}`)

	instancesStop = instanceLifecycleDescriptor(instancesStopID, "Stop a Compute Engine instance",
		"Stop one running instance of a zone of a project (instances.stop of Compute Engine v1), by its name, "+
			"with the clean shutdown Compute Engine performs. With discard_local_ssd true the contents of the "+
			"local SSDs of the instance are discarded and lost for good; without it Compute Engine keeps them, "+
			"which Google supports only for some machine types.",
		"stop", `,"discard_local_ssd":{"type":"boolean"}`, []capability.Argument{{Name: "discard_local_ssd",
			Description: "True to discard the contents of the local SSDs when the instance stops (Google's " +
				"discardLocalSsd); the data on them is lost. Default false"}}, "",
		`{`+instanceExampleBase+`,"discard_local_ssd":false}`)

	instancesReset = instanceLifecycleDescriptor(instancesResetID, "Reset a Compute Engine instance",
		"Hard-reset one instance of a zone of a project (instances.reset of Compute Engine v1), by its name: "+
			"like pressing the reset button, without a clean shutdown of the guest. The contents of the "+
			"memory are lost and unsaved data in the guest may be lost or corrupted.",
		"reset", "", nil, "", `{`+instanceExampleBase+`}`)

	instancesSetMachineType = instanceLifecycleDescriptor(instancesSetMachineTypeID,
		"Change the machine type of a Compute Engine instance",
		"Change the machine type of one instance of a zone of a project (instances.setMachineType of Compute "+
			"Engine v1). The instance must be stopped, otherwise Google refuses the change. The machine type is "+
			"given by its name; the tool builds the URL from the project and the zone itself, so a machine type "+
			"of another zone or project cannot be named. The change alters the price of the instance once it "+
			"runs again.",
		"machine-type", `,"machine_type":{"type":"string","minLength":1,"maxLength":63,`+
			`"pattern":"^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$"}`, []capability.Argument{{Name: "machine_type",
			Required: true, Description: "Name of the machine type in the zone of the instance such as " +
				"e2-standard-4 or custom-4-8192; never a path or URL. machine-types.list lists them"}},
		`,"machine_type"`, `{`+instanceExampleBase+`,"machine_type":"e2-standard-4"}`)
)

type instanceLifecycleArguments struct {
	ProjectID       string `json:"project_id"`
	Zone            string `json:"zone"`
	Instance        string `json:"instance"`
	MachineType     string `json:"machine_type"`
	DiscardLocalSsd bool   `json:"discard_local_ssd"`
	RequestID       string `json:"request_id"`
}

// check refuses an argument that is not in its documented form, before any secret is read. The refusal never
// repeats the value.
func (a instanceLifecycleArguments) check(machineType bool) error {
	switch {
	case !validProject(a.ProjectID):
		return badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(a.Zone):
		return badRequest("zone must be a zone name such as us-central1-a, never a path")
	case !validComputeName(a.Instance):
		return badRequest("instance must be an instance name, never a path, URL, or ID")
	case machineType && !validComputeName(a.MachineType):
		return badRequest("machine_type must be the name of a machine type such as e2-standard-4, never a path or URL")
	}
	return nil
}

// instanceMutation is one change of an instance: the action segment, the query values it adds to requestId,
// and the JSON body, which only setMachineType has.
type instanceMutation struct {
	op, tool, action string
	query            url.Values
	payload          []byte
}

// instanceLifecycleHandler returns the handler of one tool. The machine type is checked here and turned into
// the URL of the zone of the instance.
func instanceLifecycleHandler(op, tool, action string) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var a instanceLifecycleArguments
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, failure(op, "the validated arguments could not be read")
		}
		isType := tool == instancesSetMachineTypeID
		if err := a.check(isType); err != nil {
			return nil, err
		}
		requestID, err := checkRequestID(a.RequestID)
		if err != nil {
			return nil, err
		}
		m := instanceMutation{op: op, tool: tool, action: action, query: url.Values{"requestId": {requestID}}}
		if tool == instancesStopID && a.DiscardLocalSsd {
			m.query.Set("discardLocalSsd", "true")
		}
		if isType {
			// Google takes the machine type as a URL; it is built from checked parts, never passed through.
			body, err := json.Marshal(map[string]string{"machineType": "projects/" + a.ProjectID + "/zones/" +
				a.Zone + "/machineTypes/" + a.MachineType})
			if err != nil {
				return nil, failure(op, "the request could not be built")
			}
			m.payload = body
		}
		c, err := computeClient(ctx, op, a.ProjectID, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		return c.mutateInstance(ctx, a, requestID, m)
	}
}

// mutateInstance sends the one POST and turns the operation of the answer into the result. The request is
// never repeated, and an answer that names no operation of this zone leaves the outcome open.
func (c *client) mutateInstance(ctx context.Context, a instanceLifecycleArguments, requestID string,
	m instanceMutation) (any, error) {
	path := computeZonePath(a.ProjectID, a.Zone) + "/instances/" + url.PathEscape(a.Instance) + "/" + m.action
	var wire json.RawMessage
	if err := c.send(ctx, m.op, m.tool, serviceCompute, http.MethodPost, path, m.query, m.payload, &wire, true); err != nil {
		return nil, err
	}
	unsure := func() error {
		failed := provider.InvalidResponse(m.op, subject+" returned an invalid response")
		failed.Message += unsureNote
		return failed
	}
	row, err := computeOperationResult(m.op, wire)
	if err != nil {
		return nil, unsure()
	}
	if _, err := computeName(m.op, row); err != nil || !inScope(row, scopeZone, a.Zone, "") {
		return nil, unsure()
	}
	if status, _ := row["status"].(string); status == "DONE" {
		if failed := computeOperationError(m.op, wire); failed != nil {
			return nil, failed
		}
	}
	return map[string]any{"operation": row, "scope": scopeZone, "zone": a.Zone, "request_id": requestID}, nil
}

// instanceLifecycleSummary phrases the change from its arguments alone: project, zone, instance, and what the
// change costs or destroys.
func instanceLifecycleSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a instanceLifecycleArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		target := " instance " + boundedText(a.Instance, maxNameText) + " in zone " + boundedText(a.Zone, maxNameText) +
			" of project " + boundedText(a.ProjectID, maxNameText)
		switch tool {
		case instancesStartID:
			return "Start" + target + ": a running instance is billed until it is stopped"
		case instancesStopID:
			if a.DiscardLocalSsd {
				return "Stop" + target + " and discard its local SSDs: the data on them is lost for good"
			}
			return "Stop" + target + ": the instance is shut down and its workload ends; its local SSDs are not discarded"
		case instancesResetID:
			return "Hard-reset" + target + " without a clean shutdown: the contents of its memory are lost and " +
				"unsaved data may be lost"
		}
		return "Change the machine type of" + target + " to " + boundedText(a.MachineType, maxNameText) +
			": the instance must be stopped, and the change alters its price"
	}
}
