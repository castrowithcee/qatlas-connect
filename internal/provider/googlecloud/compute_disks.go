package googlecloud

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	disksListID           = Provider + ".disks.list"
	disksAggregatedListID = Provider + ".disks.aggregated-list"
	disksGetID            = Provider + ".disks.get"
)

// diskMembers are the documented, not input-only members of the Disk resource of compute v1 (revision
// 20261106). options is left out because Google marks it internal use only, params because it is input only. The three
// encryption keys are listed here but never passed on with key material: see dropDiskKeyMaterial.
var diskMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"sizeGb", "string"}, {"zone", "string"}, {"region", "string"},
	{"replicaZones", "array"}, {"status", "string"}, {"sourceSnapshot", "string"}, {"sourceSnapshotId", "string"},
	{"sourceImage", "string"}, {"sourceImageId", "string"}, {"sourceDisk", "string"}, {"sourceDiskId", "string"},
	{"sourceInstantSnapshot", "string"}, {"sourceInstantSnapshotId", "string"},
	{"sourceConsistencyGroupPolicy", "string"}, {"sourceConsistencyGroupPolicyId", "string"},
	{"sourceStorageObject", "string"}, {"selfLink", "string"}, {"type", "string"}, {"labels", "object"},
	{"labelFingerprint", "string"}, {"licenses", "array"}, {"licenseCodes", "array"},
	{"guestOsFeatures", "array"}, {"diskEncryptionKey", "object"}, {"sourceImageEncryptionKey", "object"},
	{"sourceSnapshotEncryptionKey", "object"}, {"physicalBlockSizeBytes", "string"},
	{"resourcePolicies", "array"}, {"users", "array"}, {"lastAttachTimestamp", "string"},
	{"lastDetachTimestamp", "string"}, {"provisionedIops", "string"}, {"provisionedThroughput", "string"},
	{"satisfiesPzs", "boolean"}, {"satisfiesPzi", "boolean"}, {"locationHint", "string"},
	{"asyncPrimaryDisk", "object"}, {"asyncSecondaryDisks", "object"},
	{"resourceStatus", "object"}, {"architecture", "string"}, {"accessMode", "string"},
	{"storagePool", "string"}, {"enableConfidentialCompute", "boolean"},
}

const diskFields = "each disk with the documented members of the Disk resource: kind, id, creationTimestamp, " +
	"name, description, sizeGb, zone, region, replicaZones, status, sourceSnapshot, sourceSnapshotId, " +
	"sourceImage, sourceImageId, sourceDisk, sourceDiskId, sourceInstantSnapshot, sourceInstantSnapshotId, " +
	"sourceConsistencyGroupPolicy, sourceConsistencyGroupPolicyId, sourceStorageObject, selfLink, type, labels, " +
	"labelFingerprint, licenses, licenseCodes, guestOsFeatures, diskEncryptionKey, " +
	"sourceImageEncryptionKey, sourceSnapshotEncryptionKey (each with kmsKeyName, kmsKeyServiceAccount, and " +
	"sha256 only), physicalBlockSizeBytes, resourcePolicies, users, lastAttachTimestamp, lastDetachTimestamp, " +
	"provisionedIops, provisionedThroughput, satisfiesPzs, satisfiesPzi, locationHint, " +
	"asyncPrimaryDisk, asyncSecondaryDisks, resourceStatus, architecture, accessMode, storagePool, " +
	"enableConfidentialCompute; untrusted text, bounded in size and depth"

// diskKeyNote ends the description of every disk tool.
const diskKeyNote = " The key material of a customer-supplied encryption key (rawKey and rsaEncryptedKey) is " +
	"never returned; the KMS key name and the key hash stay."

const diskTail = diskKeyNote + computeTail

var diskOutputSchema = membersSchema(diskMembers)

// diskKeyMaterial reports whether a member name holds key material, in any letter case and with or without
// separators.
func diskKeyMaterial(name string) bool {
	flat := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(name))
	return flat == "rawkey" || flat == "rsaencryptedkey"
}

// dropDiskKeyMaterial removes the key material members of a decoded value at every depth, before anything else
// is done with it.
func dropDiskKeyMaterial(value any) {
	switch value := value.(type) {
	case map[string]any:
		for name, member := range value {
			if diskKeyMaterial(name) {
				delete(value, name)
				continue
			}
			dropDiskKeyMaterial(member)
		}
	case []any:
		for _, item := range value {
			dropDiskKeyMaterial(item)
		}
	}
}

// diskResult turns the Disk Google sent into a result: key material dropped first, then the documented members
// only, bounded.
func diskResult(op string, raw json.RawMessage) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	dropDiskKeyMaterial(row)
	budget := maxNodes
	bounded, _ := boundNode(pickMembers(row, diskMembers), 0, &budget).(map[string]any)
	return bounded, nil
}

// diskInScope reports whether a row that names its zone or region names the scope asked for. A scope is
// zones/ZONE or regions/REGION; an empty scope checks nothing.
func diskInScope(row map[string]any, scope string) bool {
	if zone, ok := strings.CutPrefix(scope, "zones/"); ok {
		return inZone(row, zone)
	}
	if region, ok := strings.CutPrefix(scope, "regions/"); ok {
		return inRegion(row, region)
	}
	return true
}

// diskRows decodes the disks of an answer. Each must name a disk of the scope asked for.
func diskRows(op, scope string, raw []json.RawMessage) ([]map[string]any, error) {
	rows := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		row, err := diskResult(op, item)
		if err != nil {
			return nil, err
		}
		if _, err := computeName(op, row); err != nil {
			return nil, err
		}
		if !diskInScope(row, scope) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// --- disks.list ---

var disksList = capability.Descriptor{
	ID: disksListID, Version: 1, Title: "List the Compute Engine disks of a zone",
	Description: "List the persistent disks of one zone of a project from Compute Engine, with the documented " +
		"fields of each disk (size, type, status, source image or snapshot, attached instances, labels, " +
		"encryption key names), optionally filtered, sorted, and with partial results accepted." + diskTail,
	Tags: []string{"google", "cloud", "compute", "disks"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps + `,` + computeZoneProp +
		`},"required":["project_id","zone"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"disks":{"type":"array","items":` +
		diskOutputSchema + `},"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["disks","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument, computeFilterArgument,
		computeMaxArgument, computeOrderArgument, computePartialArgument, computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "disks", Description: "Disks of the page: " + diskFields},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the disks of a zone that no instance uses",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","filter":"-users:*"}`),
	}},
}

type disksAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

// diskWarning bounds the warning of an answer and drops key material from it.
func diskWarning(raw json.RawMessage) any {
	warning := computeWarning(raw)
	dropDiskKeyMaterial(warning)
	return warning
}

func handleDisksList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list disks"
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
	binding := arguments.binding(disksListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer disksAnswer
	path := computeZonePath(arguments.ProjectID, arguments.Zone) + "/disks"
	if err := c.get(ctx, op, disksListID, serviceCompute, path, arguments.query(token, 0), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows, err := diskRows(op, "zones/"+arguments.Zone, answer.Items)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"disks": rows}
	if warning := diskWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

// --- disks.aggregated-list ---

var disksAggregatedList = capability.Descriptor{
	ID: disksAggregatedListID, Version: 1, Title: "List the Compute Engine disks of all zones",
	Description: "List the persistent disks of every zone and region of a project from Compute Engine in one " +
		"call, grouped by scope: each entry of scopes names its scope (zones/ZONE or regions/REGION) and holds " +
		"the disks found there. Scopes without disks and without a warning other than the empty-page one are " +
		"left out. With return_partial_success a scope that cannot be read is named in unreachables and warning " +
		"instead of failing the list; the tool never hides them." + diskTail,
	Tags: []string{"google", "cloud", "compute", "disks"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps + `,` + computeAllScopes +
		`},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"scopes":{"type":"array","items":` +
		`{"type":"object","properties":{"scope":{"type":"string"},"disks":{"type":"array","items":` +
		diskOutputSchema + `},"warning":` + warningSchema + `},"required":["scope"],` +
		`"additionalProperties":false}},"warning":` + warningSchema + `,"unreachables":{"type":"array",` +
		`"items":{"type":"string"}},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
		`"required":["scopes","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeFilterArgument, computeMaxArgument,
		computeOrderArgument, computePartialArgument,
		{Name: "include_all_scopes", Description: "True to include every visible scope for each scope type"},
		computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "scopes", Description: "Entries of the page, each {scope, disks, warning}: scope is zones/ZONE or " +
			"regions/REGION; disks are " + diskFields},
		{Name: "warning", Description: "Warning of the list as Google sends it; untrusted text"},
		{Name: "unreachables", Description: "Scopes Google could not reach, with return_partial_success; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the disks of every zone, accepting partial results",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","return_partial_success":true}`),
	}},
}

type scopedDisks struct {
	Disks   []json.RawMessage `json:"disks"`
	Warning json.RawMessage   `json:"warning"`
}

type disksAggregatedAnswer struct {
	Items         map[string]scopedDisks `json:"items"`
	NextPageToken string                 `json:"nextPageToken"`
	Warning       json.RawMessage        `json:"warning"`
	Unreachables  []string               `json:"unreachables"`
}

func handleDisksAggregatedList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list disks of all zones"
	var arguments computeListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := arguments.check(false); err != nil {
		return nil, err
	}
	binding := arguments.binding(disksAggregatedListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer disksAggregatedAnswer
	path := computeProjectPath(arguments.ProjectID) + "/aggregated/disks"
	if err := c.get(ctx, op, disksAggregatedListID, serviceCompute, path, arguments.query(token, 0), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeScopes || len(answer.Unreachables) > maxComputeScopes {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	names := make([]string, 0, len(answer.Items))
	for name := range answer.Items {
		names = append(names, name)
	}
	sort.Strings(names)
	scopes := []map[string]any{}
	total := 0
	for _, name := range names {
		if !computeScopePattern.MatchString(name) && name != "global" {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		scoped := answer.Items[name]
		rows, err := diskRows(op, name, scoped.Disks)
		if err != nil {
			return nil, err
		}
		if total += len(rows); total > maxComputeResults {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		warning := diskWarning(scoped.Warning)
		if len(rows) == 0 && (warning == nil || isEmptyPage(warning)) {
			continue
		}
		entry := map[string]any{"scope": boundedText(name, maxNameText)}
		if len(rows) > 0 {
			entry["disks"] = rows
		}
		if warning != nil {
			entry["warning"] = warning
		}
		scopes = append(scopes, entry)
	}
	out := map[string]any{"scopes": scopes}
	if warning := diskWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	if len(answer.Unreachables) > 0 {
		out["unreachables"] = boundedList(answer.Unreachables)
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

// --- disks.get ---

var disksGet = capability.Descriptor{
	ID: disksGetID, Version: 1, Title: "Get one Compute Engine disk",
	Description: "Read one persistent disk of a zone of a project from Compute Engine, by its name or its " +
		"numeric ID, with the documented fields of the disk." + diskTail,
	Tags: []string{"google", "cloud", "compute", "disks"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeZoneProp + `,` +
		`"disk":{"type":"string","minLength":1,"maxLength":63,` +
		`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}},` +
		`"required":["project_id","zone","disk"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(diskOutputSchema),
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument,
		{Name: "disk", Required: true, Description: "Name of the disk such as data-disk-a, or its numeric ID; " +
			"never a path or URL"}},
	Fields: []capability.Field{{Name: "name", Description: "The disk as one object: " + diskFields}},
	Examples: []capability.Example{{
		Description: "Read one disk",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","disk":"data-disk-a"}`),
	}},
}

func handleDisksGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get disk"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Zone      string `json:"zone"`
		Disk      string `json:"disk"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	switch {
	case !validProject(arguments.ProjectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(arguments.Zone):
		return nil, badRequest("zone must be a zone name such as us-central1-a, never a path")
	case !validComputeNameOrID(arguments.Disk):
		return nil, badRequest("disk must be a disk name or its numeric ID, never a path")
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire json.RawMessage
	path := computeZonePath(arguments.ProjectID, arguments.Zone) + "/disks/" + arguments.Disk
	if err := c.get(ctx, op, disksGetID, serviceCompute, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := diskResult(op, wire)
	if err != nil {
		return nil, err
	}
	name, err := computeName(op, row)
	if err != nil {
		return nil, err
	}
	if id, _ := row["id"].(string); name != arguments.Disk && id != arguments.Disk ||
		!inZone(row, arguments.Zone) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}
