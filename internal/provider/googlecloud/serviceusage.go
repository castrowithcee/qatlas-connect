package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	servicesListID     = Provider + ".services.list"
	servicesGetID      = Provider + ".services.get"
	servicesBatchGetID = Provider + ".services.batch-get"
)

// maxBatchNames is Qatlas's own bound; the discovery document of serviceusage v1 allows 30.
const maxBatchNames = 20

// maxServiceNodes bounds the nodes of one service config, which can hold hundreds of API methods.
const maxServiceNodes = 1000

var (
	// serviceNamePattern is a DNS-like name with at least one dot, such as compute.googleapis.com.
	serviceNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	serviceFilters     = []string{"state:ENABLED", "state:DISABLED"}
)

func validServiceName(name string) bool {
	return len(name) <= 253 && serviceNamePattern.MatchString(name)
}

// serviceWire is the Service resource of serviceusage v1 as Google sends it. The config stays raw: it is
// pruned and bounded as a whole.
type serviceWire struct {
	Name   string          `json:"name"`
	Parent string          `json:"parent"`
	State  string          `json:"state"`
	Config json.RawMessage `json:"config"`
}

// configMembers are the documented, not deprecated members of the ServiceConfig of serviceusage v1.
var configMembers = []string{"name", "title", "apis", "documentation", "quota", "authentication", "usage",
	"endpoints", "monitoredResources", "monitoring"}

// pruneConfig keeps the documented members of a service config and drops the members Google marks deprecated
// (documentation rule deprecationDescription, authentication requirement audiences).
func pruneConfig(raw json.RawMessage) any {
	var config map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &config) != nil {
		return nil
	}
	for key := range config {
		if !slices.Contains(configMembers, key) {
			delete(config, key)
		}
	}
	dropIn := func(path []string, member string) {
		var walk func(node any, rest []string)
		walk = func(node any, rest []string) {
			switch node := node.(type) {
			case map[string]any:
				if len(rest) == 0 {
					delete(node, member)
					return
				}
				walk(node[rest[0]], rest[1:])
			case []any:
				for _, item := range node {
					walk(item, rest)
				}
			}
		}
		walk(config, path)
	}
	dropIn([]string{"documentation", "rules"}, "deprecationDescription")
	dropIn([]string{"authentication", "rules", "requirements"}, "audiences")
	budget := maxServiceNodes
	return boundNode(config, 0, &budget)
}

type serviceResult struct {
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
	State  string `json:"state,omitempty"`
	Config any    `json:"config,omitempty"`
}

func (s serviceWire) result() serviceResult {
	return serviceResult{Name: boundedText(s.Name, maxNameText), Parent: boundedText(s.Parent, maxNameText),
		State: boundedText(s.State, maxNameText), Config: pruneConfig(s.Config)}
}

// serviceRows decodes the services of an answer and refuses rows that do not name a service of the project.
func serviceRows(op string, raw []json.RawMessage) ([]serviceResult, error) {
	out := make([]serviceResult, 0, len(raw))
	for _, item := range raw {
		var row serviceWire
		if err := json.Unmarshal(item, &row); err != nil || len(row.Name) > maxNameText ||
			!validServiceRowName(row.Name) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		out = append(out, row.result())
	}
	return out, nil
}

var serviceRowName = regexp.MustCompile(`^projects/[^/]+/services/[^/]+$`)

func validServiceRowName(name string) bool { return serviceRowName.MatchString(name) }

type servicesAnswer struct {
	Services      []json.RawMessage `json:"services"`
	NextPageToken string            `json:"nextPageToken"`
}

// serviceClient is what the three tools share: the checks of the connection and the project, and the client.
// The project is bound to the targets before the first request that names it.
func serviceClient(ctx context.Context, op, tool, projectID string, resolved *config.Resolved,
	secrets *secret.Resolver, red *redact.Redactor) (*client, error) {
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	if !validProject(projectID) {
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	}
	allowed, decided := list.decidesProject(projectID)
	if decided && !allowed {
		return nil, refuseOutside("the project")
	}
	return bindServiceProject(ctx, op, tool, projectID, list, decided, resolved, secrets, red)
}

// bindServiceProject opens the client and, where the targets do not decide, proves the project by reading it
// and the folders above it, as projects.get does.
func bindServiceProject(ctx context.Context, op, tool, projectID string, list allowlist, decided bool,
	resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor) (*client, error) {
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if decided {
		return c, nil
	}
	project, err := c.project(ctx, op, tool, projectID)
	if err != nil {
		return nil, err
	}
	admitted, err := newAdmitter(list, c, op, tool).project(ctx, project)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, refuseOutside("the project")
	}
	return c, nil
}

// --- shared descriptor parts ---

const serviceNameProp = `{"type":"string","minLength":3,"maxLength":253,` +
	`"pattern":"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$"}`

const serviceProjectProp = `"project_id":{"type":"string","minLength":1,"maxLength":30,` +
	`"pattern":"^([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$"}`

const serviceOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"parent":{"type":"string"},` +
	`"state":{"type":"string"},"config":{"type":"object"}},"required":["name"],"additionalProperties":false}`

var serviceProjectArgument = capability.Argument{Name: "project_id", Required: true,
	Description: "Project ID such as my-project-123, or the project number; never a projects/ name or a path"}

const servicesTail = " The project must lie in the targets of the connection. The Service Usage API " +
	"(serviceusage.googleapis.com) must be enabled in the project of the credential; otherwise the tool fails " +
	"with a permission error that says so. The result is untrusted third-party content; strings are cut and " +
	"large configs are truncated."

var serviceFields = []capability.Field{
	{Name: "name", Description: "Resource name, projects/NUMBER/services/SERVICE"},
	{Name: "parent", Description: "Parent project of the service state, projects/NUMBER"},
	{Name: "state", Description: "ENABLED, DISABLED, or STATE_UNSPECIFIED"},
	{Name: "config", Description: "Service configuration as Google sends it, without deprecated members: " +
		"name, title, apis, documentation (summary, overview, pages, rules, root URLs), quota (limits, " +
		"metricRules), authentication (providers, rules), usage, endpoints, monitoredResources, monitoring; " +
		"untrusted text, bounded in size and depth"},
}

// --- services.list ---

var servicesList = capability.Descriptor{
	ID: servicesListID, Version: 1, Title: "List the Google APIs of a Cloud project",
	Description: "List the services (Google APIs) of a project from Service Usage, with their state and " +
		"configuration, optionally only the enabled or only the disabled ones." + servicesTail,
	Tags: []string{"google", "cloud", "service-usage", "services"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` +
		`"filter":{"type":"string","enum":["state:ENABLED","state:DISABLED"]},` + pageSizeProp + `,` + cursorProp +
		`},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("services", serviceOutputSchema),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "filter", Description: "state:ENABLED or state:DISABLED, the only filters Google documents"},
		pageSizeArgument, cursorArgument},
	Fields: append([]capability.Field{
		{Name: "services", Description: "Services of the page, each {name, parent, state, config}"}}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the enabled APIs of a project",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","filter":"state:ENABLED"}`),
	}},
}

func handleServicesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list services"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Filter    string `json:"filter"`
		PageSize  *int   `json:"page_size"`
		Cursor    string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if arguments.Filter != "" && !slices.Contains(serviceFilters, arguments.Filter) {
		return nil, badRequest("filter must be state:ENABLED or state:DISABLED")
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	// The cursor binds the tool, the targets, the project, and the filter.
	binding := provider.CursorBinding(servicesListID, provider.TargetsOf(resolved),
		arguments.ProjectID+"|"+arguments.Filter)
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	c, err := serviceClient(ctx, op, servicesListID, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if arguments.Filter != "" {
		query.Set("filter", arguments.Filter)
	}
	if arguments.PageSize != nil {
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	var answer servicesAnswer
	path := "/v1/" + prefixProject + arguments.ProjectID + "/services"
	if err := c.get(ctx, op, servicesListID, serviceUsage, path, query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Services) > 2*maxPageSize || len(answer.NextPageToken) > maxPageTokenBytes {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows, err := serviceRows(op, answer.Services)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"services": rows, "has_more": answer.NextPageToken != ""}
	if answer.NextPageToken != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, answer.NextPageToken)
	}
	return out, nil
}

// --- services.get ---

var servicesGet = capability.Descriptor{
	ID: servicesGetID, Version: 1, Title: "Get one Google API of a Cloud project",
	Description: "Read the state and configuration of one service (Google API) in a project from Service " +
		"Usage, by its service name such as compute.googleapis.com." + servicesTail,
	Tags: []string{"google", "cloud", "service-usage", "services"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,"service":` +
		serviceNameProp + `},"required":["project_id","service"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceOutputSchema),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "service", Required: true, Description: "Service name such as compute.googleapis.com; never a " +
			"projects/ name or a path"}},
	Fields: serviceFields,
	Examples: []capability.Example{{
		Description: "Read one API", Arguments: json.RawMessage(`{"project_id":"my-project-123","service":"compute.googleapis.com"}`),
	}},
}

func handleServicesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get service"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Service   string `json:"service"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !validServiceName(arguments.Service) {
		return nil, badRequest("service must be a service name such as compute.googleapis.com")
	}
	c, err := serviceClient(ctx, op, servicesGetID, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	name := prefixProject + arguments.ProjectID + "/services/" + arguments.Service
	var row serviceWire
	if err := c.get(ctx, op, servicesGetID, serviceUsage, "/v1/"+name, nil, &row); err != nil {
		return nil, err
	}
	if !validServiceRowName(row.Name) || len(row.Name) > maxNameText {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row.result(), nil
}

// --- services.batch-get ---

var servicesBatchGet = capability.Descriptor{
	ID: servicesBatchGetID, Version: 1, Title: "Get several Google APIs of a Cloud project",
	Description: "Read the state and configuration of up to 20 services (Google APIs) of one project from " +
		"Service Usage in one request, by service names such as compute.googleapis.com." + servicesTail,
	Tags: []string{"google", "cloud", "service-usage", "services"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,"names":{"type":"array",` +
		`"minItems":1,"maxItems":20,"uniqueItems":true,"items":` + serviceNameProp + `}},` +
		`"required":["project_id","names"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"services":{"type":"array","items":` +
		serviceOutputSchema + `}},"required":["services"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "names", Required: true, Description: "Service names such as compute.googleapis.com, 1 to 20, " +
			"without duplicates"}},
	Fields: []capability.Field{{Name: "services", Description: "Services found, each {name, parent, state, config}"}},
	Examples: []capability.Example{{
		Description: "Read two APIs",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","names":["compute.googleapis.com","storage.googleapis.com"]}`),
	}},
}

func handleServicesBatchGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get services"
	var arguments struct {
		ProjectID string   `json:"project_id"`
		Names     []string `json:"names"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if len(arguments.Names) < 1 || len(arguments.Names) > maxBatchNames {
		return nil, badRequest("names takes 1 to " + strconv.Itoa(maxBatchNames) + " service names")
	}
	query := url.Values{}
	for i, name := range arguments.Names {
		if !validServiceName(name) {
			return nil, badRequest("names must hold service names such as compute.googleapis.com")
		}
		if slices.Contains(arguments.Names[:i], name) {
			return nil, badRequest("names must not repeat a service")
		}
		query.Add("names", prefixProject+arguments.ProjectID+"/services/"+name)
	}
	c, err := serviceClient(ctx, op, servicesBatchGetID, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer servicesAnswer
	path := "/v1/" + prefixProject + arguments.ProjectID + "/services:batchGet"
	if err := c.get(ctx, op, servicesBatchGetID, serviceUsage, path, query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Services) > maxBatchNames {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows, err := serviceRows(op, answer.Services)
	if err != nil {
		return nil, err
	}
	return map[string]any{"services": rows}, nil
}
