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

const machineTypesListID = Provider + ".machine-types.list"

// machineTypeMembers are the documented, not deprecated members of the MachineType resource of compute v1
// (revision 20261106). imageSpaceGb is left out: Google marks it deprecated and never populated.
var machineTypeMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"guestCpus", "integer"}, {"memoryMb", "integer"},
	{"maximumPersistentDisks", "integer"}, {"maximumPersistentDisksSizeGb", "string"}, {"zone", "string"},
	{"selfLink", "string"}, {"isSharedCpu", "boolean"}, {"accelerators", "array"}, {"deprecated", "object"},
	{"architecture", "string"}, {"bundledLocalSsds", "object"},
}

var machineTypesList = capability.Descriptor{
	ID: machineTypesListID, Version: 1, Title: "List the Compute Engine machine types of a zone",
	Description: "List the machine types available in one zone of a project from Compute Engine, with their " +
		"virtual CPUs, memory, persistent disk limits, accelerators, architecture, and deprecation status, " +
		"optionally filtered, sorted, and with partial results accepted." + computeTail,
	Tags: []string{"google", "cloud", "compute", "machine-types"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps + `,` + computeZoneProp +
		`},"required":["project_id","zone"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"machine_types":{"type":"array","items":` +
		membersSchema(machineTypeMembers) + `},"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["machine_types","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument, computeFilterArgument,
		computeMaxArgument, computeOrderArgument, computePartialArgument, computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "machine_types", Description: "Machine types of the page, each {kind, id, creationTimestamp, name, " +
			"description, guestCpus, memoryMb, maximumPersistentDisks, maximumPersistentDisksSizeGb, zone, " +
			"selfLink, isSharedCpu, accelerators, deprecated, architecture, bundledLocalSsds}; untrusted text"},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the machine types of a zone whose name starts with e2-standard",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","filter":"name eq e2-standard.*"}`),
	}},
}

type machineTypesAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

// machineTypeResult keeps the documented members of one machine type, bounded.
func machineTypeResult(op string, raw json.RawMessage) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	budget := maxNodes
	bounded, _ := boundNode(pickMembers(row, machineTypeMembers), 0, &budget).(map[string]any)
	return bounded, nil
}

func handleMachineTypesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list machine types"
	var arguments computeListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if arguments.IncludeAllScopes {
		return nil, badRequest("include_all_scopes is not an argument of this tool")
	}
	if err := arguments.check(true); err != nil {
		return nil, err
	}
	binding := arguments.binding(machineTypesListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer machineTypesAnswer
	path := computeZonePath(arguments.ProjectID, arguments.Zone) + "/machineTypes"
	if err := c.get(ctx, op, machineTypesListID, serviceCompute, path, arguments.query(token, 0), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := machineTypeResult(op, item)
		if err != nil {
			return nil, err
		}
		if _, err := computeName(op, row); err != nil {
			return nil, err
		}
		if !inZone(row, arguments.Zone) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		rows = append(rows, row)
	}
	out := map[string]any{"machine_types": rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}
