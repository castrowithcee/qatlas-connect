package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const operationsGetID = Provider + ".operations.get"

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

// operationResult is what a tool returns for an operation that has not failed. A mutation of a later package
// returns it as it is and never waits: the operation continues at Google, and operations.get reads its state.
type operationResult struct {
	Name     string `json:"name"`
	Done     bool   `json:"done"`
	Metadata any    `json:"metadata,omitempty"`
	Response any    `json:"response,omitempty"`
}

// operationOf turns the operation Google answered into a result, or into a provider error when it is done
// and carries an error. Metadata and response are provider data and are bounded. The message and the details
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
	if wire.Done {
		result.Response = boundedValue(wire.Response)
	}
	return result, nil
}

// operationServices are the services whose operations the tool reads; later packages add theirs.
var operationServices = []service{serviceResourceManager, serviceUsage}

// operationPaths is the path below the origin at which a service reads an operation by its ID.
var operationPaths = map[service]string{serviceResourceManager: "/v3/operations/", serviceUsage: "/v1/operations/"}

var operationNamePattern = regexp.MustCompile(`^operations/[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

const operationOutputSchema = `{"type":"object","properties":{` +
	`"service":{"type":"string"},"name":{"type":"string"},"done":{"type":"boolean"},` +
	`"metadata":{"type":"object"},"response":{"type":"object"}},` +
	`"required":["service","name","done"],"additionalProperties":false}`

var operationsGet = capability.Descriptor{
	ID: operationsGetID, Version: 1, Title: "Get a Google Cloud operation",
	Description: "Read the state of a long-running Google Cloud operation by its name, as a change returned it. " +
		"done is false while the operation runs, with its metadata; once it is done the result carries the " +
		"response, and an operation that ended with an error is reported as a provider error with its code. " +
		"service picks the Google service that owns the operation: resource-manager, or service-usage for the " +
		"operation of a change of services. An operation belongs to no folder or " +
		"project that Qatlas could prove, so a connection with targets reads operations only if it names an " +
		"organization. Metadata and response are untrusted third-party data; they are cut.",
	Tags: []string{"google", "cloud", "operations"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"service":{"type":"string","enum":["resource-manager","service-usage"]},` +
		`"name":{"type":"string","minLength":12,"maxLength":211,` +
		`"pattern":"^operations/[A-Za-z0-9][A-Za-z0-9._-]{0,199}$"}},` +
		`"required":["service","name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(operationOutputSchema),
	Arguments: []capability.Argument{
		{Name: "service", Required: true, Description: "Google service that owns the operation: " +
			"resource-manager or service-usage"},
		{Name: "name", Required: true, Description: "Name of the operation as Google returned it, such as " +
			"operations/abc123"},
	},
	Fields: []capability.Field{
		{Name: "service", Description: "The service the tool asked"},
		{Name: "name", Description: "Name of the operation"},
		{Name: "done", Description: "False while the operation runs, true once it ended"},
		{Name: "metadata", Description: "Metadata of the operation with its @type; untrusted data, cut to a bounded size"},
		{Name: "response", Description: "Response of a finished operation with its @type; untrusted data, cut to a bounded size"},
	},
	Examples: []capability.Example{{
		Description: "Read an operation of Resource Manager",
		Arguments:   json.RawMessage(`{"service":"resource-manager","name":"operations/abc123"}`),
	}},
}

func handleOperationsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get operation"
	var arguments struct {
		Service string `json:"service"`
		Name    string `json:"name"`
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
	svc := service(arguments.Service)
	known := false
	for _, candidate := range operationServices {
		known = known || candidate == svc
	}
	if !known {
		return nil, badRequest("service must be resource-manager or service-usage")
	}
	if !operationNamePattern.MatchString(arguments.Name) {
		return nil, badRequest("name must be an operation name such as operations/abc123")
	}
	if !list.reachesWithoutResource() {
		return nil, badRequest("operations are read only by a connection without targets or with an organization " +
			"among them; Qatlas cannot tie an operation to a folder or a project")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire operationWire
	path := operationPaths[svc] + url.PathEscape(arguments.Name[len("operations/"):])
	if err := c.get(ctx, op, operationsGetID, svc, path, nil, &wire); err != nil {
		return nil, err
	}
	if wire.Name != arguments.Name {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	result, err := operationOf(op, wire)
	if err != nil {
		return nil, err
	}
	return struct {
		Service string `json:"service"`
		operationResult
	}{string(svc), result}, nil
}
