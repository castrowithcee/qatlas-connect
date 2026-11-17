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
	zonesListID   = Provider + ".zones.list"
	regionsListID = Provider + ".regions.list"
)

// defaultRegionResults is the page size asked for when the caller names none. A region carries its quotas, and
// 500 of them would exceed the size one answer may have.
const defaultRegionResults = 25

// maxRegionQuotas bounds the quotas of one region; Google lists well below it.
const maxRegionQuotas = 500

// zoneMembers are the documented, not deprecated members of the Zone resource of compute v1 (revision
// 20261106).
var zoneMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"status", "string"}, {"deprecated", "object"}, {"region", "string"},
	{"selfLink", "string"}, {"availableCpuPlatforms", "array"}, {"supportsPzs", "boolean"},
	{"resourceStatus", "object"},
}

// regionMembers are the documented, not deprecated members of the Region resource of compute v1 (revision
// 20261106). quotas is rebuilt by regionResult so that it keeps every entry.
var regionMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"status", "string"}, {"zones", "array"}, {"quotas", "array"},
	{"quotaStatusWarning", "object"}, {"deprecated", "object"}, {"selfLink", "string"}, {"supportsPzs", "boolean"},
}

const computeListAnswerFields = `"warning":` + warningSchema + `,"next_cursor":{"type":"string"},` +
	`"has_more":{"type":"boolean"}`

var zonesList = capability.Descriptor{
	ID: zonesListID, Version: 1, Title: "List the Compute Engine zones of a project",
	Description: "List the zones available to a project from Compute Engine, with their region, status, " +
		"description, available CPU platforms, support for Persistent Disk of the zone, resource status, and " +
		"deprecation status, optionally filtered, sorted, and with partial results accepted." + computeTail,
	Tags: []string{"google", "cloud", "compute", "zones"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps +
		`},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"zones":{"type":"array","items":` +
		membersSchema(zoneMembers) + `},` + computeListAnswerFields + `},"required":["zones","has_more"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeFilterArgument, computeMaxArgument,
		computeOrderArgument, computePartialArgument, computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "zones", Description: "Zones of the page, each {kind, id, creationTimestamp, name, description, " +
			"status, deprecated, region, selfLink, availableCpuPlatforms, supportsPzs, resourceStatus}; untrusted text"},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the zones of a project whose name starts with europe-west1",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","filter":"name eq europe-west1-.*"}`),
	}},
}

var regionsList = capability.Descriptor{
	ID: regionsListID, Version: 1, Title: "List the Compute Engine regions of a project",
	Description: "List the regions available to a project from Compute Engine, with their status, zones, " +
		"deprecation status, and the quotas of the project in each region (metric, limit, usage, owner), " +
		"optionally filtered, sorted, and with partial results accepted. The quotas are the usage figures Google " +
		"reports for the region; they say nothing of quotas of other services." + computeTail,
	Tags: []string{"google", "cloud", "compute", "regions"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps +
		`},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"regions":{"type":"array","items":` +
		membersSchema(regionMembers) + `},` + computeListAnswerFields + `},"required":["regions","has_more"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeFilterArgument, computeMaxArgument,
		computeOrderArgument, computePartialArgument, computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "regions", Description: "Regions of the page, each {kind, id, creationTimestamp, name, description, " +
			"status, zones, quotas, quotaStatusWarning, deprecated, selfLink, supportsPzs}; quotas is a list of " +
			"{metric, limit, usage, owner}; untrusted text"},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the regions of a project with their quotas, ten at a time",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","max_results":10}`),
	}},
}

type zonesRegionsAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Warning       json.RawMessage   `json:"warning"`
}

// zoneResult keeps the documented members of one zone, bounded.
func zoneResult(op string, raw json.RawMessage) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	budget := maxNodes
	bounded, _ := boundNode(pickMembers(row, zoneMembers), 0, &budget).(map[string]any)
	return bounded, nil
}

// regionResult keeps the documented members of one region, bounded. The quotas keep their four documented
// members and up to maxRegionQuotas entries, which the general bound of a list would cut at a hundred.
func regionResult(op string, raw json.RawMessage) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	picked := pickMembers(row, regionMembers)
	quotas, _ := picked["quotas"].([]any)
	delete(picked, "quotas")
	budget := maxNodes
	bounded, _ := boundNode(picked, 0, &budget).(map[string]any)
	kept := make([]any, 0, min(len(quotas), maxRegionQuotas))
	for _, entry := range quotas[:min(len(quotas), maxRegionQuotas)] {
		quota, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		out := map[string]any{}
		for _, name := range []string{"metric", "owner"} {
			if value, ok := quota[name].(string); ok {
				out[name] = boundedText(value, maxNameText)
			}
		}
		for _, name := range []string{"limit", "usage"} {
			if value, ok := quota[name].(float64); ok {
				out[name] = value
			}
		}
		kept = append(kept, out)
	}
	if len(kept) > 0 {
		bounded["quotas"] = kept
	}
	return bounded, nil
}

// listProjectCollection reads one page of a list below the project: the zones or the regions.
func listProjectCollection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, op, tool, collection string, defaultResults int,
	result func(string, json.RawMessage) (map[string]any, error)) (any, error) {
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
	var answer zonesRegionsAnswer
	path := computeProjectPath(arguments.ProjectID) + "/" + collection
	if err := c.get(ctx, op, tool, serviceCompute, path, arguments.query(token, defaultResults), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := result(op, item)
		if err != nil {
			return nil, err
		}
		if _, err := computeName(op, row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	out := map[string]any{collection: rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

func handleZonesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	return listProjectCollection(ctx, resolved, secrets, red, raw, "list zones", zonesListID, "zones", 0, zoneResult)
}

func handleRegionsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return listProjectCollection(ctx, resolved, secrets, red, raw, "list regions", regionsListID, "regions",
		defaultRegionResults, regionResult)
}
