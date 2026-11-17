package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	computeOperationsGetID  = Provider + ".compute-operations.get"
	computeOperationsListID = Provider + ".compute-operations.list"
)

// computeOperationSensitivity classifies the results of the Compute operation tools: next to the state of
// changes to cloud infrastructure they name the person who started each change.
const computeOperationSensitivity = "cloud-operation-personal-data"

// defaultOperationResults is the page size asked for when the caller names none.
const defaultOperationResults = 100

// The three scopes of a Compute operation and the Google method each one reads.
const (
	scopeZone   = "zone"
	scopeRegion = "region"
	scopeGlobal = "global"
)

// computeOperationRisk is the risk of the operation tools: that of every read tool, with the sensitivity
// that says the result names a person.
var computeOperationRisk = func() capability.Risk {
	risk := readRisk
	risk.DataSensitivity = computeOperationSensitivity
	return risk
}()

// computeOperationMembers are the documented, not deprecated members of the Operation resource of compute v1
// (revision 20261106). creationTimestamp is left out: Google marks it deprecated. user is the email address
// of the person or account that started the operation.
var computeOperationMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"name", "string"}, {"zone", "string"}, {"region", "string"},
	{"description", "string"}, {"operationType", "string"}, {"targetLink", "string"}, {"targetId", "string"},
	{"status", "string"}, {"statusMessage", "string"}, {"user", "string"}, {"progress", "integer"},
	{"insertTime", "string"}, {"startTime", "string"}, {"endTime", "string"}, {"error", "object"},
	{"warnings", "array"}, {"httpErrorStatusCode", "integer"}, {"httpErrorMessage", "string"},
	{"selfLink", "string"}, {"clientOperationId", "string"}, {"operationGroupId", "string"},
	{"setCommonInstanceMetadataOperationMetadata", "object"}, {"getHealthOperationMetadata", "object"},
	{"getVersionOperationMetadata", "object"}, {"instancesBulkInsertOperationMetadata", "object"},
}

const computeOperationPersonNote = " The user member of each operation is the email address of the person or " +
	"account that started it and is personal data; use it only for the purpose of the request."

const computeOperationTail = computeOperationPersonNote + computeTail

const computeOperationFields = "each operation with the documented members of the Operation resource: kind, id, " +
	"name, zone, region, description, operationType, targetLink, targetId, status (PENDING, RUNNING, or DONE), " +
	"statusMessage, user (email address of the person or account that started it: personal data), progress, " +
	"insertTime, startTime, endTime, error (errors with code, location, and message), warnings, " +
	"httpErrorStatusCode, httpErrorMessage, selfLink, clientOperationId, operationGroupId, and the metadata of " +
	"bulk insert, health, version, and common instance metadata operations; untrusted text"

const (
	computeScopeProp     = `"scope":{"type":"string","enum":["zone","region","global"]}`
	computeOperationProp = `"operation":{"type":"string","minLength":1,"maxLength":63,` +
		`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`
)

var (
	computeScopeArgument = capability.Argument{Name: "scope", Required: true, Description: "Where the operation " +
		"lives: zone (needs zone), region (needs region), or global; the combination with zone and region " +
		"is checked before anything is sent"}
	computeOperationZoneArgument = capability.Argument{Name: "zone", Description: "Zone name such as " +
		"us-central1-a; required for scope zone and refused otherwise"}
	computeOperationRegionArgument = capability.Argument{Name: "region", Description: "Region name such as " +
		"us-central1; required for scope region and refused otherwise"}
	computeOperationArgument = capability.Argument{Name: "operation", Required: true, Description: "Name of the " +
		"operation as a change returned it, such as operation-1700000000000-abc123-def456, or its numeric ID"}
)

var computeOperationsGet = capability.Descriptor{
	ID: computeOperationsGetID, Version: 1, Title: "Get a Compute Engine operation",
	Description: "Read the state of one Compute Engine operation of a project: zoneOperations.get, " +
		"regionOperations.get, or globalOperations.get, chosen by scope. status is PENDING, RUNNING, or DONE. " +
		"An operation that is DONE without an error is returned as it is; one that is DONE with an error is " +
		"reported as a provider error whose original entries carry the code, location, and message of each " +
		"error, and whose class follows the HTTP status of the operation (404 not found, 429 rate limited, " +
		"every other status a provider error). Compute operations are not the long-running operations that " +
		"operations.get reads." + computeOperationTail,
	Tags: []string{"google", "cloud", "compute", "operations"}, Risk: computeOperationRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeScopeProp + `,` +
		computeZoneProp + `,` + computeRegionProp + `,` + computeOperationProp +
		`},"required":["project_id","scope","operation"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"operation":` + membersSchema(computeOperationMembers) +
		`},"required":["operation"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeScopeArgument, computeOperationZoneArgument,
		computeOperationRegionArgument, computeOperationArgument},
	Fields: []capability.Field{
		{Name: "operation", Description: "The operation: " + computeOperationFields},
	},
	Examples: []capability.Example{{
		Description: "Read a zone operation that an instance change returned",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","scope":"zone","zone":"us-central1-a",` +
			`"operation":"operation-1700000000000-abc123-def456"}`),
	}},
}

var computeOperationsList = capability.Descriptor{
	ID: computeOperationsListID, Version: 1, Title: "List the Compute Engine operations of a project",
	Description: "List the Compute Engine operations of a project in one scope: zoneOperations.list, " +
		"regionOperations.list, or globalOperations.list, chosen by scope, optionally filtered, sorted, and with " +
		"partial results accepted. An operation that ended with an error is listed with its error; nothing is " +
		"turned into a provider error here." + computeOperationTail,
	Tags: []string{"google", "cloud", "compute", "operations"}, Risk: computeOperationRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + computeListProps + `,` + computeScopeProp + `,` +
		computeZoneProp + `,` + computeRegionProp + `},"required":["project_id","scope"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"operations":{"type":"array","items":` +
		membersSchema(computeOperationMembers) + `},` + computeListAnswerFields + `},"required":["operations","has_more"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument, computeScopeArgument, computeOperationZoneArgument,
		computeOperationRegionArgument, computeFilterArgument, computeMaxArgument, computeOrderArgument,
		computePartialArgument, computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "operations", Description: "Operations of the page, " + computeOperationFields},
		{Name: "warning", Description: "Warning of the list as Google sends it, with code, message, and data; " +
			"present when Google sent one; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the operations that are still running in a region",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","scope":"region","region":"us-central1",` +
			`"filter":"status != DONE"}`),
	}},
}

// checkOperationScope refuses a scope that is unknown and a zone or region that does not fit it: zone is
// needed for scope zone, region for scope region, and neither is allowed for scope global or the other scope.
func checkOperationScope(scope, zone, region string) error {
	switch scope {
	case scopeZone:
		if !validComputeName(zone) {
			return badRequest("zone must be a zone name such as us-central1-a, never a path, when scope is zone")
		}
		if region != "" {
			return badRequest("region is not an argument when scope is zone")
		}
	case scopeRegion:
		if !validComputeName(region) {
			return badRequest("region must be a region name such as us-central1, never a path, when scope is region")
		}
		if zone != "" {
			return badRequest("zone is not an argument when scope is region")
		}
	case scopeGlobal:
		if zone != "" || region != "" {
			return badRequest("zone and region are not arguments when scope is global")
		}
	default:
		return badRequest("scope must be zone, region, or global")
	}
	return nil
}

// computeOperationsPath is the path of the operations of a scope, or of one operation when name is not empty.
// The project, zone, region, and name were checked.
func computeOperationsPath(projectID, scope, zone, region, name string) string {
	var path string
	switch scope {
	case scopeZone:
		path = computeZonePath(projectID, zone)
	case scopeRegion:
		path = computeRegionPath(projectID, region)
	default:
		path = computeProjectPath(projectID) + "/global"
	}
	path += "/operations"
	if name != "" {
		path += "/" + url.PathEscape(name)
	}
	return path
}

// inScope reports whether a row names the zone or region asked for, like inZone. A global operation names
// neither.
func inScope(row map[string]any, scope, zone, region string) bool {
	switch scope {
	case scopeZone:
		return inZone(row, zone)
	case scopeRegion:
		value, ok := row["region"].(string)
		return !ok || value == region || strings.HasSuffix(value, "/regions/"+region)
	}
	return true
}

// computeOperationResult keeps the documented members of one operation, bounded.
func computeOperationResult(op string, raw json.RawMessage) (map[string]any, error) {
	row, err := decodeRow(op, raw)
	if err != nil {
		return nil, err
	}
	budget := maxNodes
	bounded, _ := boundNode(pickMembers(row, computeOperationMembers), 0, &budget).(map[string]any)
	return bounded, nil
}

// computeOperationError turns the error of an operation that is DONE into a provider error. Compute
// operations carry the error as error.errors[] (code, location, message) and the HTTP status Google would
// have answered with. The class follows that status where it names a condition of its own: 404 and 429. A
// 401 or 403 stays a provider error, because a quota failure and a permission failure share the 403. The
// message holds only the status; code, location, and message of each error are provider text and go along as
// the original.
func computeOperationError(op string, raw json.RawMessage) *provider.Error {
	var wire struct {
		Error struct {
			Errors []struct {
				Code     string `json:"code"`
				Location string `json:"location"`
				Message  string `json:"message"`
			} `json:"errors"`
		} `json:"error"`
		HTTPErrorStatusCode int `json:"httpErrorStatusCode"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return nil
	}
	if len(wire.Error.Errors) == 0 {
		return nil
	}
	class := provider.ClassProviderError
	switch wire.HTTPErrorStatusCode {
	case 404:
		class = provider.ClassNotFound
	case 429:
		class = provider.ClassRateLimited
	}
	message := "the operation ended with an error"
	if wire.HTTPErrorStatusCode > 0 {
		message += " (HTTP status " + strconv.Itoa(wire.HTTPErrorStatusCode) + ")"
	}
	entries := make([]provider.OriginalEntry, 0, min(len(wire.Error.Errors), maxListItems))
	for _, e := range wire.Error.Errors[:min(len(wire.Error.Errors), maxListItems)] {
		entries = append(entries, provider.OriginalEntry{Code: boundedText(e.Code, maxNameText),
			Path: boundedText(e.Location, maxNameText), Message: boundedText(e.Message, maxText)})
	}
	return &provider.Error{Class: class, Op: op, Message: message, Original: &provider.Original{Entries: entries}}
}

func handleComputeOperationsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get compute operation"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Scope     string `json:"scope"`
		Zone      string `json:"zone"`
		Region    string `json:"region"`
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	switch {
	case !validProject(arguments.ProjectID):
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeNameOrID(arguments.Operation):
		return nil, badRequest("operation must be an operation name such as operation-1700000000000-abc123, " +
			"or its numeric ID, never a path")
	}
	if err := checkOperationScope(arguments.Scope, arguments.Zone, arguments.Region); err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer json.RawMessage
	path := computeOperationsPath(arguments.ProjectID, arguments.Scope, arguments.Zone, arguments.Region, arguments.Operation)
	if err := c.get(ctx, op, computeOperationsGetID, serviceCompute, path, nil, &answer); err != nil {
		return nil, err
	}
	row, err := computeOperationResult(op, answer)
	if err != nil {
		return nil, err
	}
	if _, err := computeName(op, row); err != nil {
		return nil, err
	}
	if !inScope(row, arguments.Scope, arguments.Zone, arguments.Region) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	if status, _ := row["status"].(string); status == "DONE" {
		if failed := computeOperationError(op, answer); failed != nil {
			return nil, failed
		}
	}
	return map[string]any{"operation": row}, nil
}

// computeOperationsListArguments are the arguments of compute-operations.list: those of every list, with the
// scope and the region.
type computeOperationsListArguments struct {
	computeListArguments
	Scope  string `json:"scope"`
	Region string `json:"region"`
}

// binding ties a cursor to the scope as well.
func (a computeOperationsListArguments) binding(tool string, resolved *config.Resolved) []byte {
	return provider.CursorBinding(tool, provider.TargetsOf(resolved), a.ProjectID, a.Scope, a.Zone, a.Region, a.Filter,
		a.OrderBy, a.ReturnPartialSuccess)
}

func handleComputeOperationsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list compute operations"
	var arguments computeOperationsListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if arguments.IncludeAllScopes {
		return nil, badRequest("include_all_scopes is not an argument of this tool")
	}
	if err := checkOperationScope(arguments.Scope, arguments.Zone, arguments.Region); err != nil {
		return nil, err
	}
	if err := arguments.check(arguments.Scope == scopeZone); err != nil {
		return nil, err
	}
	binding := arguments.binding(computeOperationsListID, resolved)
	token, err := arguments.pageToken(binding)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer zonesRegionsAnswer
	path := computeOperationsPath(arguments.ProjectID, arguments.Scope, arguments.Zone, arguments.Region, "")
	if err := c.get(ctx, op, computeOperationsListID, serviceCompute, path,
		arguments.query(token, defaultOperationResults), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxComputeResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := computeOperationResult(op, item)
		if err != nil {
			return nil, err
		}
		if _, err := computeName(op, row); err != nil {
			return nil, err
		}
		if !inScope(row, arguments.Scope, arguments.Zone, arguments.Region) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		rows = append(rows, row)
	}
	out := map[string]any{"operations": rows}
	if warning := computeWarning(answer.Warning); warning != nil {
		out["warning"] = warning
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}
