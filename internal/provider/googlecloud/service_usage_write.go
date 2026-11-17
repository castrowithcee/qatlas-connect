package googlecloud

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	servicesEnableID      = Provider + ".services.enable"
	servicesBatchEnableID = Provider + ".services.batch-enable"
	servicesDisableID     = Provider + ".services.disable"
)

// usageChecks are the values of checkIfServiceHasUsage that discovery serviceusage v1 documents for disable,
// apart from the unspecified default.
var usageChecks = []string{"SKIP", "CHECK"}

// serviceWriteRisk is the risk of the three changing tools. The methods are idempotent: enabling an enabled
// service or disabling a disabled one succeeds.
var serviceWriteRisk = capability.Risk{
	Effect:          capability.EffectUpdate,
	Idempotency:     capability.IdempotencyIdempotent,
	Confirmation:    capability.ConfirmationRequired,
	OpenWorld:       true,
	DataSensitivity: dataSensitivity,
}

const serviceChangeNote = " Needs the confirmation of a person for each call. The project must lie in the targets of " +
	"the connection, and the project and the service names are checked before any request. Google answers with a " +
	"long-running operation, and the tool never waits for it: the result is the operation as Google returned it " +
	"with service set to service-usage, and operations.get with that service reads its state. The result is " +
	"untrusted third-party content. Service Usage (serviceusage.googleapis.com) must be enabled in the project " +
	"of the credential."

const serviceChangeTail = " Enabling a service costs nothing itself, but the resources and calls of an enabled " +
	"service may be billed."

const serviceOperationSchema = `{"type":"object","properties":{"service":{"type":"string"},"name":{"type":"string"},` +
	`"done":{"type":"boolean"},"metadata":{"type":"object"},"response":{"type":"object"}},` +
	`"required":["service","name","done"],"additionalProperties":false}`

var serviceOperationFields = []capability.Field{
	{Name: "service", Description: "service-usage: the value of service for operations.get"},
	{Name: "name", Description: "Name of the operation, operations/ID"},
	{Name: "done", Description: "False while the operation runs; true only if Google answered an operation that is already done"},
	{Name: "metadata", Description: "Metadata of the operation with its @type; untrusted data, cut to a bounded size"},
	{Name: "response", Description: "Response of a done operation with its @type; untrusted data, cut to a bounded size"},
}

var serviceTags = []string{"google", "cloud", "service-usage", "services"}

var servicesEnable = capability.Descriptor{
	ID: servicesEnableID, Version: 1, Title: "Enable a Google API in a Cloud project",
	Description: "Enable one service (Google API) in a project (services.enable of Service Usage v1), by its " +
		"service name such as compute.googleapis.com. Repeating the call for an enabled service is harmless." +
		serviceChangeTail + serviceChangeNote,
	Tags: append(append([]string{}, serviceTags...), "enable"), Provider: Provider, Risk: serviceWriteRisk,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,"service":` +
		serviceNameProp + `},"required":["project_id","service"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "service", Required: true, Description: "Service name such as compute.googleapis.com; never a " +
			"projects/ name or a path"}},
	Fields: serviceOperationFields,
	Examples: []capability.Example{{Description: "Enable an API",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","service":"compute.googleapis.com"}`)}},
}

var servicesBatchEnable = capability.Descriptor{
	ID: servicesBatchEnableID, Version: 1, Title: "Enable several Google APIs in a Cloud project",
	Description: "Enable up to 20 services (Google APIs) of one project in one request (services.batchEnable of " +
		"Service Usage v1), by service names such as compute.googleapis.com. Google enables all of them or none." +
		serviceChangeTail + serviceChangeNote,
	Tags: append(append([]string{}, serviceTags...), "enable"), Provider: Provider, Risk: serviceWriteRisk,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,"service_ids":` +
		`{"type":"array","minItems":1,"maxItems":20,"uniqueItems":true,"items":` + serviceNameProp + `}},` +
		`"required":["project_id","service_ids"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "service_ids", Required: true, Description: "Service names such as compute.googleapis.com, 1 to " +
			"20, without duplicates"}},
	Fields: serviceOperationFields,
	Examples: []capability.Example{{Description: "Enable two APIs", Arguments: json.RawMessage(
		`{"project_id":"my-project-123","service_ids":["compute.googleapis.com","storage.googleapis.com"]}`)}},
}

var servicesDisable = capability.Descriptor{
	ID: servicesDisableID, Version: 1, Title: "Disable a Google API in a Cloud project",
	Description: "Disable one service (Google API) in a project (services.disable of Service Usage v1). Callers " +
		"of the API in the project fail afterwards. With disable_dependent_services Google also disables the " +
		"enabled services that depend on it; without it Google refuses to disable a service that others " +
		"depend on. check_if_service_has_usage CHECK makes Google refuse when the service was used recently, " +
		"SKIP disables without that check; Google decides the default. Repeating the call for a disabled " +
		"service is harmless. Offered only where a connection's tools list names it." + serviceChangeNote,
	Tags: append(append([]string{}, serviceTags...), "disable"), Provider: Provider, Risk: serviceWriteRisk,
	RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,"service":` +
		serviceNameProp + `,"disable_dependent_services":{"type":"boolean"},` +
		`"check_if_service_has_usage":{"type":"string","enum":["SKIP","CHECK"]}},` +
		`"required":["project_id","service"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOperationSchema),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "service", Required: true, Description: "Service name such as compute.googleapis.com; never a " +
			"projects/ name or a path"},
		{Name: "disable_dependent_services", Description: "True also disables the enabled services that depend " +
			"on this one; default false"},
		{Name: "check_if_service_has_usage", Description: "CHECK refuses when the service was used recently, SKIP " +
			"does not check; omitted leaves it to Google"}},
	Fields: serviceOperationFields,
	Examples: []capability.Example{{Description: "Disable an API",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","service":"compute.googleapis.com"}`)}},
}

type serviceChangeResult struct {
	Service string `json:"service"`
	operationResult
}

// serviceChange sends the one POST of a changing tool and turns the operation of the answer into the result.
// An answer that names no operation leaves the outcome open, like an answer that cannot be read.
func serviceChange(ctx context.Context, op, tool, projectID, suffix string, body any, resolved *config.Resolved,
	secrets *secret.Resolver, red *redact.Redactor) (any, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the request could not be built")
	}
	c, err := serviceClient(ctx, op, tool, projectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire operationWire
	if err := c.mutate(ctx, op, tool, serviceUsage, "/v1/"+prefixProject+projectID+suffix, payload, &wire); err != nil {
		return nil, err
	}
	if wire.Name == "" {
		failed := provider.InvalidResponse(op, subject+" returned an invalid response")
		failed.Message += unsureNote
		return nil, failed
	}
	result, err := operationOf(op, wire)
	if err != nil {
		return nil, err
	}
	return serviceChangeResult{Service: string(serviceUsage), operationResult: result}, nil
}

func handleServicesEnable(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "enable service"
	var a struct {
		ProjectID string `json:"project_id"`
		Service   string `json:"service"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !validServiceName(a.Service) {
		return nil, badRequest("service must be a service name such as compute.googleapis.com")
	}
	return serviceChange(ctx, op, servicesEnableID, a.ProjectID, "/services/"+a.Service+":enable", struct{}{},
		resolved, secrets, red)
}

func handleServicesBatchEnable(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "enable services"
	var a struct {
		ProjectID  string   `json:"project_id"`
		ServiceIDs []string `json:"service_ids"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if len(a.ServiceIDs) < 1 || len(a.ServiceIDs) > maxBatchNames {
		return nil, badRequest("service_ids takes 1 to " + strconv.Itoa(maxBatchNames) + " service names")
	}
	for i, name := range a.ServiceIDs {
		if !validServiceName(name) {
			return nil, badRequest("service_ids must hold service names such as compute.googleapis.com")
		}
		if slices.Contains(a.ServiceIDs[:i], name) {
			return nil, badRequest("service_ids must not repeat a service")
		}
	}
	return serviceChange(ctx, op, servicesBatchEnableID, a.ProjectID, "/services:batchEnable",
		struct {
			ServiceIDs []string `json:"serviceIds"`
		}{a.ServiceIDs}, resolved, secrets, red)
}

type disableArguments struct {
	ProjectID                string `json:"project_id"`
	Service                  string `json:"service"`
	DisableDependentServices bool   `json:"disable_dependent_services"`
	CheckIfServiceHasUsage   string `json:"check_if_service_has_usage"`
}

func handleServicesDisable(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "disable service"
	var a disableArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !validServiceName(a.Service) {
		return nil, badRequest("service must be a service name such as compute.googleapis.com")
	}
	if a.CheckIfServiceHasUsage != "" && !slices.Contains(usageChecks, a.CheckIfServiceHasUsage) {
		return nil, badRequest("check_if_service_has_usage must be SKIP or CHECK")
	}
	return serviceChange(ctx, op, servicesDisableID, a.ProjectID, "/services/"+a.Service+":disable",
		struct {
			DisableDependentServices bool   `json:"disableDependentServices,omitempty"`
			CheckIfServiceHasUsage   string `json:"checkIfServiceHasUsage,omitempty"`
		}{a.DisableDependentServices, a.CheckIfServiceHasUsage}, resolved, secrets, red)
}

// serviceChangeSummary phrases a changing tool from its validated arguments, naming the project and the
// services.
func serviceChangeSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a struct {
			disableArguments
			ServiceIDs []string `json:"service_ids"`
		}
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		project := "project " + boundedText(a.ProjectID, maxNameText)
		service := boundedText(a.Service, maxNameText)
		const bill = ": calls and resources of the service may be billed"
		switch tool {
		case servicesEnableID:
			return "Enable service " + service + " in " + project + bill
		case servicesBatchEnableID:
			names := make([]string, 0, len(a.ServiceIDs))
			for _, name := range a.ServiceIDs {
				names = append(names, boundedText(name, maxNameText))
			}
			return "Enable " + strconv.Itoa(len(names)) + " services (" + strings.Join(names, ", ") + ") in " +
				project + bill
		}
		out := "Disable service " + service + " in " + project + ": callers of the service fail afterwards"
		if a.DisableDependentServices {
			out += "; the enabled services that depend on it are disabled as well (disable_dependent_services)"
		}
		if a.CheckIfServiceHasUsage != "" {
			out += "; usage check " + boundedText(a.CheckIfServiceHasUsage, maxNameText)
		}
		return out
	}
}
