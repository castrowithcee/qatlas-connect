package googleroute

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const operationsGetID = Provider + ".operations.get"

// dataSensitivity classifies results as location and vehicle data, which is personal data.
const dataSensitivity = "location-and-route-data"

// readRisk is the risk of every read tool of this provider: it reads, is safe to repeat, and reaches the
// account of a third party.
var readRisk = capability.Risk{
	Effect:          capability.EffectRead,
	Idempotency:     capability.IdempotencySafe,
	Confirmation:    capability.ConfirmationNone,
	OpenWorld:       true,
	DataSensitivity: dataSensitivity,
}

// operationNamePattern is the name of an operation: projects/PROJECT/locations/LOCATION/operations/ID. The
// location is checked for its shape only, because Google assigns it in the answer of an optimization.
var operationNamePattern = regexp.MustCompile(
	`^projects/([A-Za-z0-9-]{1,30})/locations/([A-Za-z0-9-]{1,64})/operations/([A-Za-z0-9][A-Za-z0-9._-]{0,199})$`)

// operationRef is the checked parts of an operation name.
type operationRef struct{ project, location, id string }

func parseOperationName(name string) (operationRef, bool) {
	match := operationNamePattern.FindStringSubmatch(name)
	if match == nil || !validProject(match[1]) {
		return operationRef{}, false
	}
	return operationRef{match[1], match[2], match[3]}, true
}

func operationName(project, location, id string) string {
	return prefixProject + project + "/locations/" + location + "/operations/" + id
}

// sameOperation reports whether Google answered for the operation that was asked for. The location and the
// ID must match. A project that is spelled as ID in one name and as number in the other is the same project
// as far as Qatlas can tell, because it resolves neither.
func sameOperation(asked, answered operationRef) bool {
	if asked.location != answered.location || asked.id != answered.id {
		return false
	}
	return asked.project == answered.project ||
		numberPattern.MatchString(asked.project) != numberPattern.MatchString(answered.project)
}

// operationWire is the Operation object of a Google long-running operation: its name, whether it is done,
// the metadata while it runs, and once it is done either an error or a response. Metadata and response are
// open objects that carry an @type.
type operationWire struct {
	Name     string           `json:"name"`
	Done     bool             `json:"done"`
	Metadata json.RawMessage  `json:"metadata"`
	Error    *operationStatus `json:"error"`
	Response json.RawMessage  `json:"response"`
}

// operationStatus is the google.rpc.Status of a failed operation.
type operationStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// operationResult is what the tool returns for an operation that has not failed.
type operationResult struct {
	Name     string `json:"name"`
	Done     bool   `json:"done"`
	Metadata any    `json:"metadata,omitempty"`
	Response any    `json:"response,omitempty"`
}

// operation reads the operation of a checked name and verifies that Google answered for it.
func (c *client) operation(ctx context.Context, op, name string) (operationWire, error) {
	var wire operationWire
	if err := c.get(ctx, op, operationsGetID, "/v1/"+name, &wire); err != nil {
		return operationWire{}, err
	}
	asked, _ := parseOperationName(name)
	if answered, ok := parseOperationName(wire.Name); !ok || !sameOperation(asked, answered) {
		return operationWire{}, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return wire, nil
}

// operationOf turns the operation Google answered into a result, or into a provider error when it is done
// and carries an error. The metadata is provider data and is bounded, the response of a done operation is the
// OptimizeToursResponse with the cuts of toursResultOf. The message and the details
// of the error are provider text: they go along as the Original only. The classes follow google.rpc.Code,
// which Google documents for the code of a Status: 5 NOT_FOUND, 7 PERMISSION_DENIED, 8 RESOURCE_EXHAUSTED and
// 16 UNAUTHENTICATED; every other code is a provider error.
func operationOf(op string, wire operationWire) (operationResult, error) {
	if wire.Name == "" {
		return operationResult{}, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	if wire.Done && wire.Error != nil {
		class := provider.ClassProviderError
		switch wire.Error.Code {
		case 5:
			class = provider.ClassNotFound
		case 7:
			class = provider.ClassPermission
		case 8:
			class = provider.ClassRateLimited
		case 16:
			class = provider.ClassAuth
		}
		return operationResult{}, &provider.Error{Class: class, Op: op,
			Message: "the operation ended with an error (code " + strconv.Itoa(wire.Error.Code) + ")",
			Original: &provider.Original{Entries: []provider.OriginalEntry{{
				Code: strconv.Itoa(wire.Error.Code), Message: wire.Error.Message}}}}
	}
	result := operationResult{Name: boundedText(wire.Name, maxNameText), Done: wire.Done,
		Metadata: boundedValue(wire.Metadata)}
	if wire.Done && len(wire.Response) > 0 && string(wire.Response) != "null" {
		typed, err := toursResultOf(op, wire.Response, true)
		if err != nil {
			return operationResult{}, err
		}
		result.Response = typed
	}
	return result, nil
}

// operationOutput is the output schema of a tool that returns an operation: the response is the typed
// OptimizeToursResponse, with its @type.
var operationOutput = func() json.RawMessage {
	out, err := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{
		"name": map[string]any{"type": "string"}, "done": map[string]any{"type": "boolean"},
		"metadata": map[string]any{"type": "object"}, "response": toursOutputSchema(true)},
		"required": []string{"name", "done"}, "additionalProperties": false})
	if err != nil {
		panic("googleroute: the operation output schema cannot be encoded: " + err.Error())
	}
	return out
}()

// operationFields are the result fields of every tool that returns an operation.
var operationFields = []capability.Field{
	{Name: "name", Description: "Name of the operation"},
	{Name: "done", Description: "False while the operation runs, true once it ended"},
	{Name: "metadata", Description: "Metadata of the operation with its @type; untrusted data, cut to a bounded size"},
	{Name: "response", Description: "OptimizeToursResponse of a finished optimization with its @type and the same " +
		"members, complete, and truncation as the result of tours.optimize; personal and untrusted data, cut to a bounded size"},
}

var operationsGet = capability.Descriptor{
	ID: operationsGetID, Version: 2, Title: "Get a Route Optimization operation",
	Description: "Read the state of a Route Optimization operation by its name, projects/PROJECT/locations/LOCATION/" +
		"operations/ID. done is false while the operation runs, with its metadata; once it is done the result " +
		"carries the response as an OptimizeToursResponse mapped like the result of tours.optimize, with complete and " +
		"truncation, and an operation that ended with an error is reported as a provider error with its " +
		"code. A connection with project targets reads only operations of those projects, which the name states. " +
		"The response holds locations, vehicles, and routes, which are personal data. Metadata and response are " +
		"untrusted third-party data; they are cut. The token holds the broad cloud-platform scope, the only one " +
		"Google lists for this method, so it may reach more than this tool does, although the tool changes nothing.",
	Tags: []string{"google", "route-optimization", "operations"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string","minLength":30,"maxLength":340,` +
		`"pattern":"^projects/[A-Za-z0-9-]{1,30}/locations/[A-Za-z0-9-]{1,64}/operations/[A-Za-z0-9][A-Za-z0-9._-]{0,199}$"}},` +
		`"required":["name"],"additionalProperties":false}`),
	OutputSchema: operationOutput,
	Arguments: []capability.Argument{
		{Name: "name", Required: true, Description: "Name of the operation as Google returned it, such as " +
			"projects/my-project-123/locations/us-central1/operations/abc123; the project must be one of the " +
			"targets of the connection, spelled the same way"},
	},
	Fields: operationFields,
	Examples: []capability.Example{{
		Description: "Read a Route Optimization operation",
		Arguments:   json.RawMessage(`{"name":"projects/my-project-123/locations/us-central1/operations/abc123"}`),
	}},
}

func handleOperationsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get operation"
	var arguments struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	ref, ok := parseOperationName(arguments.Name)
	if !ok {
		return nil, badRequest("name must be an operation name such as projects/PROJECT/locations/LOCATION/operations/ID")
	}
	if !list.allows(ref.project) {
		return nil, badRequest("the project of name is outside the targets of this connection")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	wire, err := c.operation(ctx, op, arguments.Name)
	if err != nil {
		return nil, err
	}
	return operationOf(op, wire)
}
