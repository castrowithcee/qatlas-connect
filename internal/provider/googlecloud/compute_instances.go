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
	instancesListID           = Provider + ".instances.list"
	instancesAggregatedListID = Provider + ".instances.aggregated-list"
	instancesGetID            = Provider + ".instances.get"
)

// defaultInstanceResults is the page size asked for when the caller names none. An instance is large, and 500
// of them would exceed the size one answer may have.
const defaultInstanceResults = 100

// instanceMembers are the documented, not deprecated members of the Instance resource of compute v1 (revision
// 20261106), the result of every instance tool. metadata is listed here but never passed on as Google sends it:
// see instanceResult.
var instanceMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"tags", "object"}, {"machineType", "string"}, {"status", "string"},
	{"statusMessage", "string"}, {"zone", "string"}, {"canIpForward", "boolean"},
	{"networkInterfaces", "array"}, {"disks", "array"}, {"metadata", "object"}, {"serviceAccounts", "array"},
	{"selfLink", "string"}, {"scheduling", "object"}, {"cpuPlatform", "string"}, {"labels", "object"},
	{"labelFingerprint", "string"}, {"minCpuPlatform", "string"}, {"guestAccelerators", "array"},
	{"startRestricted", "boolean"}, {"deletionProtection", "boolean"}, {"resourcePolicies", "array"},
	{"sourceMachineImage", "string"}, {"sourceMachineImageEncryptionKey", "object"},
	{"instanceEncryptionKey", "object"}, {"reservationAffinity", "object"}, {"hostname", "string"},
	{"displayDevice", "object"}, {"shieldedInstanceConfig", "object"},
	{"shieldedInstanceIntegrityPolicy", "object"}, {"confidentialInstanceConfig", "object"},
	{"fingerprint", "string"}, {"privateIpv6GoogleAccess", "string"}, {"advancedMachineFeatures", "object"},
	{"lastStartTimestamp", "string"}, {"lastStopTimestamp", "string"}, {"lastSuspendedTimestamp", "string"},
	{"satisfiesPzs", "boolean"}, {"satisfiesPzi", "boolean"}, {"resourceStatus", "object"},
	{"networkPerformanceConfig", "object"}, {"keyRevocationActionType", "string"}, {"params", "object"},
	{"workloadIdentityConfig", "object"}, {"localSsdEncryptionMode", "string"},
}

// secretKeyMembers are the members of a customer-supplied encryption key that hold key material. Google marks
// all three deprecated (customer-supplied keys are no longer supported); none is ever passed on, wherever an
// encryption key appears. The names of the KMS key stay.
var secretKeyMembers = []string{"rawKey", "rsaEncryptedKey", "sha256"}

// instanceResult turns the Instance Google sent into a result: the documented members only, the metadata
// reduced to the names of its keys and its fingerprint, key material removed, and everything bounded. The
// values of metadata.items can hold start-up scripts, credentials, or keys, so they are not copied.
func instanceResult(op string, raw json.RawMessage) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	row = pickMembers(row, instanceMembers)
	if metadata, ok := row["metadata"].(map[string]any); ok {
		row["metadata"] = metadataKeys(metadata)
	}
	removeKeyMaterial(row)
	budget := maxNodes
	bounded, _ := boundNode(row, 0, &budget).(map[string]any)
	return bounded, nil
}

// metadataKeys keeps of the metadata of an instance the names of its entries, in Google's order, and the
// fingerprint, never a value.
func metadataKeys(metadata map[string]any) map[string]any {
	out := map[string]any{}
	keys := []any{}
	items, _ := metadata["items"].([]any)
	for _, item := range items {
		entry, _ := item.(map[string]any)
		if key, ok := entry["key"].(string); ok && len(keys) < maxListItems {
			keys = append(keys, key)
		}
	}
	out["keys"] = keys
	for _, name := range []string{"fingerprint", "kind"} {
		if value, ok := metadata[name].(string); ok {
			out[name] = value
		}
	}
	return out
}

// removeKeyMaterial deletes the key material of every customer-supplied encryption key in the tree.
func removeKeyMaterial(node any) {
	switch node := node.(type) {
	case map[string]any:
		for name, value := range node {
			if strings.HasSuffix(name, "EncryptionKey") {
				if key, ok := value.(map[string]any); ok {
					for _, secretName := range secretKeyMembers {
						delete(key, secretName)
					}
				}
			}
			removeKeyMaterial(value)
		}
	case []any:
		for _, item := range node {
			removeKeyMaterial(item)
		}
	}
}

// instanceRows decodes the instances of an answer. Each must name an instance of the zone asked for.
func instanceRows(op, zone string, raw []json.RawMessage) ([]map[string]any, error) {
	rows := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		row, err := instanceResult(op, item)
		if err != nil {
			return nil, err
		}
		if _, err := computeName(op, row); err != nil {
			return nil, err
		}
		if zone != "" && !inZone(row, zone) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

const instanceMetadataNote = " The metadata of an instance can hold start-up scripts, credentials, and keys, so " +
	"the result carries only the names of the metadata keys (metadata.keys) and the metadata fingerprint, never " +
	"a value. The service accounts (email and scopes) and the network interfaces with their external addresses are " +
	"returned. Customer-supplied encryption key material is never returned."

const instanceListTail = instanceMetadataNote + computeTail

var instanceFields = "each instance with the documented members of the Instance resource: kind, id, " +
	"creationTimestamp, name, description, tags, machineType, status, statusMessage, zone, canIpForward, " +
	"networkInterfaces, disks, metadata (keys and fingerprint only), serviceAccounts, selfLink, scheduling, " +
	"cpuPlatform, labels, labelFingerprint, minCpuPlatform, guestAccelerators, startRestricted, " +
	"deletionProtection, resourcePolicies, sourceMachineImage, sourceMachineImageEncryptionKey, " +
	"instanceEncryptionKey, reservationAffinity, hostname, displayDevice, shieldedInstanceConfig, " +
	"shieldedInstanceIntegrityPolicy, confidentialInstanceConfig, fingerprint, privateIpv6GoogleAccess, " +
	"advancedMachineFeatures, lastStartTimestamp, lastStopTimestamp, lastSuspendedTimestamp, satisfiesPzs, " +
	"satisfiesPzi, resourceStatus, networkPerformanceConfig, keyRevocationActionType, params, " +
	"workloadIdentityConfig, localSsdEncryptionMode; untrusted text, bounded in size and depth"

var instanceOutputSchema = membersSchema(instanceMembers)

const warningSchema = `{"type":"object"}`

// --- instances.list ---

var instancesList = capability.Descriptor{
	ID: instancesListID, Version: 1, Title: "List the Compute Engine instances of a zone",
	Description: "List the virtual machine instances of one zone of a project from Compute Engine, with the " +
		"documented fields of each instance, optionally filtered, sorted, and with partial results accepted. " +
		"A page without max_results holds at most 100 instances." + instanceListTail,
	Tags: []string{"google", "cloud", "compute", "instances"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps + `,` + computeZoneProp +
		`},"required":["project_id","zone"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"instances":{"type":"array","items":` +
		instanceOutputSchema + `},"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["instances","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument, computeFilterArgument,
		computeMaxArgument, computeOrderArgument, computePartialArgument, computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "instances", Description: "Instances of the page: " + instanceFields},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the running instances of a zone",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","filter":"status = RUNNING"}`),
	}},
}

type instancesAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

func handleInstancesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list instances"
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
	binding := arguments.binding(instancesListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer instancesAnswer
	path := computeZonePath(arguments.ProjectID, arguments.Zone) + "/instances"
	if err := c.get(ctx, op, instancesListID, serviceCompute, path,
		arguments.query(token, defaultInstanceResults), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows, err := instanceRows(op, arguments.Zone, answer.Items)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"instances": rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

// --- instances.aggregated-list ---

var instancesAggregatedList = capability.Descriptor{
	ID: instancesAggregatedListID, Version: 1, Title: "List the Compute Engine instances of all zones",
	Description: "List the virtual machine instances of every zone of a project from Compute Engine in one " +
		"call, grouped by zone: each entry of scopes names its scope (zones/ZONE) and holds the instances found " +
		"there. Scopes without instances and without a warning other than the empty-page one are left out. " +
		"With return_partial_success a zone that cannot be read is named in unreachables and warning instead " +
		"of failing the list; the tool never hides them. A page without max_results holds at most 100 " +
		"instances." + instanceListTail,
	Tags: []string{"google", "cloud", "compute", "instances"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps + `,` + computeAllScopes +
		`},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"scopes":{"type":"array","items":` +
		`{"type":"object","properties":{"scope":{"type":"string"},"instances":{"type":"array","items":` +
		instanceOutputSchema + `},"warning":` + warningSchema + `},"required":["scope"],` +
		`"additionalProperties":false}},"warning":` + warningSchema + `,"unreachables":{"type":"array",` +
		`"items":{"type":"string"}},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
		`"required":["scopes","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeFilterArgument, computeMaxArgument,
		computeOrderArgument, computePartialArgument,
		{Name: "include_all_scopes", Description: "True to include every visible scope for each scope type; " +
			"Google documents that for instances every scope is returned in any case"},
		computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "scopes", Description: "Entries of the page, each {scope, instances, warning}: scope is zones/ZONE; " +
			"instances are " + instanceFields},
		{Name: "warning", Description: "Warning of the list as Google sends it; untrusted text"},
		{Name: "unreachables", Description: "Scopes Google could not reach, with return_partial_success; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the instances of every zone, accepting partial results",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","return_partial_success":true}`),
	}},
}

type scopedInstances struct {
	Instances []json.RawMessage `json:"instances"`
	Warning   json.RawMessage   `json:"warning"`
}

type instancesAggregatedAnswer struct {
	Items         map[string]scopedInstances `json:"items"`
	NextPageToken string                     `json:"nextPageToken"`
	Warning       json.RawMessage            `json:"warning"`
	Unreachables  []string                   `json:"unreachables"`
}

// emptyPageWarning is the code Google gives the scopes of an aggregated list that hold nothing on the page.
const emptyPageWarning = "NO_RESULTS_ON_PAGE"

func handleInstancesAggregatedList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list instances of all zones"
	var arguments computeListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := arguments.check(false); err != nil {
		return nil, err
	}
	binding := arguments.binding(instancesAggregatedListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer instancesAggregatedAnswer
	path := computeProjectPath(arguments.ProjectID) + "/aggregated/instances"
	if err := c.get(ctx, op, instancesAggregatedListID, serviceCompute, path,
		arguments.query(token, defaultInstanceResults), &answer); err != nil {
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
		zone := ""
		if after, ok := strings.CutPrefix(name, "zones/"); ok {
			zone = after
		}
		rows, err := instanceRows(op, zone, scoped.Instances)
		if err != nil {
			return nil, err
		}
		if total += len(rows); total > maxComputeResults {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		warning := computeWarning(scoped.Warning)
		if len(rows) == 0 && (warning == nil || isEmptyPage(warning)) {
			continue
		}
		entry := map[string]any{"scope": boundedText(name, maxNameText)}
		if len(rows) > 0 {
			entry["instances"] = rows
		}
		if warning != nil {
			entry["warning"] = warning
		}
		scopes = append(scopes, entry)
	}
	out := map[string]any{"scopes": scopes}
	if warning := computeWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	if len(answer.Unreachables) > 0 {
		out["unreachables"] = boundedList(answer.Unreachables)
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

// isEmptyPage reports whether a bounded warning is the one of a scope with nothing on the page.
func isEmptyPage(warning any) bool {
	fields, _ := warning.(map[string]any)
	return fields["code"] == emptyPageWarning
}

// --- instances.get ---

var instancesGet = capability.Descriptor{
	ID: instancesGetID, Version: 1, Title: "Get one Compute Engine instance",
	Description: "Read one virtual machine instance of a zone of a project from Compute Engine, by its name or " +
		"its numeric ID, with the documented fields of the instance." + instanceListTail,
	Tags: []string{"google", "cloud", "compute", "instances"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeZoneProp + `,` +
		`"instance":{"type":"string","minLength":1,"maxLength":63,` +
		`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}},` +
		`"required":["project_id","zone","instance"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(instanceOutputSchema),
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument,
		{Name: "instance", Required: true, Description: "Name of the instance such as my-vm, or its numeric ID; " +
			"never a path or URL"}},
	Fields: []capability.Field{{Name: "name", Description: "The instance as one object: " + instanceFields}},
	Examples: []capability.Example{{
		Description: "Read one instance",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","instance":"my-vm"}`),
	}},
}

func handleInstancesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get instance"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Zone      string `json:"zone"`
		Instance  string `json:"instance"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	switch {
	case !validProject(arguments.ProjectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(arguments.Zone):
		return nil, badRequest("zone must be a zone name such as us-central1-a, never a path")
	case !validComputeNameOrID(arguments.Instance):
		return nil, badRequest("instance must be an instance name or its numeric ID, never a path")
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire json.RawMessage
	path := computeZonePath(arguments.ProjectID, arguments.Zone) + "/instances/" + arguments.Instance
	if err := c.get(ctx, op, instancesGetID, serviceCompute, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := instanceResult(op, wire)
	if err != nil {
		return nil, err
	}
	name, err := computeName(op, row)
	if err != nil {
		return nil, err
	}
	if id, _ := row["id"].(string); name != arguments.Instance && id != arguments.Instance ||
		!inZone(row, arguments.Zone) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}
