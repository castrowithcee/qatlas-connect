package googlecloud

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	snapshotsListID = Provider + ".snapshots.list"
	snapshotsGetID  = Provider + ".snapshots.get"
)

// snapshotMembers are the documented, not deprecated, not input-only members of the Snapshot resource of compute
// v1 (revision 20261106). storageBytesStatus is left out because Google marks it deprecated, params and guestFlush
// because they are input only. The encryption key members stay in the schema; their raw key material is dropped
// from every answer (see dropKeyMaterial).
var snapshotMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"status", "string"}, {"sourceDisk", "string"}, {"sourceDiskId", "string"},
	{"diskSizeGb", "string"}, {"storageBytes", "string"}, {"downloadBytes", "string"},
	{"creationSizeBytes", "string"}, {"licenses", "array"}, {"licenseCodes", "array"}, {"labels", "object"},
	{"labelFingerprint", "string"}, {"snapshotEncryptionKey", "object"}, {"sourceDiskEncryptionKey", "object"},
	{"sourceInstantSnapshotEncryptionKey", "object"}, {"selfLink", "string"}, {"storageLocations", "array"},
	{"autoCreated", "boolean"}, {"chainName", "string"}, {"snapshotType", "string"},
	{"sourceInstantSnapshot", "string"}, {"sourceInstantSnapshotId", "string"},
	{"sourceDiskForRecoveryCheckpoint", "string"}, {"sourceSnapshotSchedulePolicy", "string"},
	{"sourceSnapshotSchedulePolicyId", "string"}, {"snapshotGroupName", "string"}, {"snapshotGroupId", "string"},
	{"guestOsFeatures", "array"}, {"architecture", "string"}, {"locationHint", "string"}, {"region", "string"},
	{"enableConfidentialCompute", "boolean"}, {"satisfiesPzs", "boolean"}, {"satisfiesPzi", "boolean"},
}

const snapshotFields = "{kind, id, creationTimestamp, name, description, status, sourceDisk (URL), sourceDiskId, " +
	"diskSizeGb, storageBytes, downloadBytes, creationSizeBytes, licenses, licenseCodes, labels, labelFingerprint, " +
	"snapshotEncryptionKey, sourceDiskEncryptionKey, sourceInstantSnapshotEncryptionKey (each with kmsKeyName, " +
	"kmsKeyServiceAccount, and sha256 only), selfLink, storageLocations, autoCreated, chainName, snapshotType, " +
	"sourceInstantSnapshot, sourceInstantSnapshotId, sourceDiskForRecoveryCheckpoint, sourceSnapshotSchedulePolicy, " +
	"sourceSnapshotSchedulePolicyId, snapshotGroupName, snapshotGroupId, guestOsFeatures, architecture, " +
	"locationHint, region, enableConfidentialCompute, satisfiesPzs, satisfiesPzi}; untrusted text"

// keyMaterialNote ends the description of every snapshot and image tool.
const keyMaterialNote = " Customer-supplied key material of an encryption key (rawKey and rsaEncryptedKey) is " +
	"never returned; the Cloud KMS key name (kmsKeyName) stays."

const computeSnapshotProp = `"snapshot":{"type":"string","minLength":1,"maxLength":63,` +
	`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`

func globalListOutput(key string, members []computeMember) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"` + key + `":{"type":"array","items":` +
		membersSchema(members) + `},"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["` + key + `","has_more"],"additionalProperties":false}`)
}

var globalListArgs = []capability.Argument{serviceProjectArgument, computeFilterArgument, computeMaxArgument,
	computeOrderArgument, computePartialArgument, computeCursorArgument}

func globalListFields(key, what, fields string) []capability.Field {
	return append([]capability.Field{
		{Name: key, Description: what + " of the page, each " + fields},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...)
}

// globalListInput is the input schema of the list tools of the global resources of a project.
const globalListInput = `{"type":"object","properties":{` + computeListProps +
	`},"required":["project_id"],"additionalProperties":false}`

var snapshotsList = capability.Descriptor{
	ID: snapshotsListID, Version: 1, Title: "List the disk snapshots of a project",
	Description: "List the global disk snapshots of a project from Compute Engine, with their status, source disk, " +
		"sizes, type, storage locations, labels, and Cloud KMS key names, optionally filtered, sorted, and with " +
		"partial results accepted." + keyMaterialNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "snapshots", "disks"}, Risk: readRisk, Provider: Provider,
	InputSchema:  json.RawMessage(globalListInput),
	OutputSchema: globalListOutput("snapshots", snapshotMembers),
	Arguments:    globalListArgs,
	Fields:       globalListFields("snapshots", "Snapshots", snapshotFields),
	Examples: []capability.Example{{
		Description: "List the snapshots that are ready",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","filter":"status = READY"}`),
	}},
}

var snapshotsGet = capability.Descriptor{
	ID: snapshotsGetID, Version: 1, Title: "Get one disk snapshot",
	Description: "Read one global disk snapshot of a project from Compute Engine, by its name or its numeric ID, " +
		"with the documented fields of the snapshot." + keyMaterialNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "snapshots", "disks"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeSnapshotProp +
		`},"required":["project_id","snapshot"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(membersSchema(snapshotMembers)),
	Arguments: []capability.Argument{serviceProjectArgument, {Name: "snapshot", Required: true,
		Description: "Name of the snapshot such as snapshot-a, or its numeric ID; never a path or URL"}},
	Fields: []capability.Field{{Name: "name", Description: "The snapshot as one object: " + snapshotFields}},
	Examples: []capability.Example{{
		Description: "Read one snapshot",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","snapshot":"snapshot-a"}`),
	}},
}

// keyMaterialKey reports whether a member name is customer-supplied key material, in any letter case and with
// or without separators.
func keyMaterialKey(key string) bool {
	switch strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key)) {
	case "rawkey", "rsaencryptedkey":
		return true
	}
	return false
}

// dropKeyMaterial removes the key material members of a decoded value at every depth, before anything else is
// done with it.
func dropKeyMaterial(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, member := range value {
			if keyMaterialKey(key) {
				delete(value, key)
				continue
			}
			dropKeyMaterial(member)
		}
	case []any:
		for _, item := range value {
			dropKeyMaterial(item)
		}
	}
}

// keyedRow keeps the documented members of one resource, bounded, after the key material is dropped at every
// depth.
func keyedRow(op string, raw json.RawMessage, members []computeMember) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	dropKeyMaterial(row)
	budget := maxNodes
	bounded, _ := boundNode(pickMembers(row, members), 0, &budget).(map[string]any)
	return bounded, nil
}

type keyedAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

// handleKeyedList is the list of a global resource that may carry encryption keys: resource is the path member,
// key the member of the result.
func handleKeyedList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool, resource, key string, members []computeMember) (any, error) {
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
	binding := arguments.binding(tool, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer keyedAnswer
	path := computeProjectPath(arguments.ProjectID) + "/global/" + resource
	if err := c.get(ctx, op, tool, serviceCompute, path, arguments.query(token, 0), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := keyedRow(op, item, members)
		if err != nil {
			return nil, err
		}
		if _, err := computeName(op, row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	out := map[string]any{key: rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		dropKeyMaterial(warning)
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

// handleKeyedGet is the get of a global resource that may carry encryption keys.
func handleKeyedGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool, resource, member, noun string, members []computeMember) (any, error) {
	var arguments map[string]string
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	projectID, target := arguments["project_id"], arguments[member]
	switch {
	case !validProject(projectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeNameOrID(target):
		return nil, badRequest(member + " must be a " + noun + " name or its numeric ID, never a path")
	}
	c, err := computeClient(ctx, op, projectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire json.RawMessage
	path := computeProjectPath(projectID) + "/global/" + resource + "/" + target
	if err := c.get(ctx, op, tool, serviceCompute, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := keyedRow(op, wire, members)
	if err != nil {
		return nil, err
	}
	name, err := computeName(op, row)
	if err != nil {
		return nil, err
	}
	if id, _ := row["id"].(string); name != target && id != target {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}

func handleSnapshotsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return handleKeyedList(ctx, resolved, secrets, red, raw, "list snapshots", snapshotsListID, "snapshots",
		"snapshots", snapshotMembers)
}

func handleSnapshotsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return handleKeyedGet(ctx, resolved, secrets, red, raw, "get snapshot", snapshotsGetID, "snapshots",
		"snapshot", "snapshot", snapshotMembers)
}
