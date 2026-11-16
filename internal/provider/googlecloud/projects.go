package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// dataSensitivity classifies results as infrastructure data of a cloud account.
const dataSensitivity = "cloud-infrastructure"

// readRisk is the risk of every read tool of this provider: it reads, is safe to repeat, and reaches the
// account of a third party.
var readRisk = capability.Risk{
	Effect:          capability.EffectRead,
	Idempotency:     capability.IdempotencySafe,
	Confirmation:    capability.ConfirmationNone,
	OpenWorld:       true,
	DataSensitivity: dataSensitivity,
}

const projectsGetID = Provider + ".projects.get"

// projectWire is the Project resource of cloudresourcemanager v3 as Google sends it.
type projectWire struct {
	Name                   string            `json:"name"`
	Parent                 string            `json:"parent"`
	ProjectID              string            `json:"projectId"`
	State                  string            `json:"state"`
	DisplayName            string            `json:"displayName"`
	CreateTime             string            `json:"createTime"`
	UpdateTime             string            `json:"updateTime"`
	DeleteTime             string            `json:"deleteTime"`
	Etag                   string            `json:"etag"`
	Labels                 map[string]string `json:"labels"`
	Tags                   map[string]string `json:"tags"`
	ManagementType         string            `json:"managementType"`
	IsManagementProject    *bool             `json:"isManagementProject"`
	ConfiguredCapabilities []string          `json:"configuredCapabilities"`
}

// projectResult is what a tool returns for a project: every documented member of the resource, bounded.
type projectResult struct {
	Name                   string            `json:"name"`
	ProjectID              string            `json:"projectId,omitempty"`
	DisplayName            string            `json:"displayName,omitempty"`
	Parent                 string            `json:"parent,omitempty"`
	State                  string            `json:"state,omitempty"`
	Labels                 map[string]string `json:"labels,omitempty"`
	Tags                   map[string]string `json:"tags,omitempty"`
	CreateTime             string            `json:"createTime,omitempty"`
	UpdateTime             string            `json:"updateTime,omitempty"`
	DeleteTime             string            `json:"deleteTime,omitempty"`
	Etag                   string            `json:"etag,omitempty"`
	ManagementType         string            `json:"managementType,omitempty"`
	IsManagementProject    *bool             `json:"isManagementProject,omitempty"`
	ConfiguredCapabilities []string          `json:"configuredCapabilities,omitempty"`
}

func (p projectWire) result() projectResult {
	return projectResult{
		Name: boundedText(p.Name, maxNameText), ProjectID: boundedText(p.ProjectID, maxNameText),
		DisplayName: boundedText(p.DisplayName, maxNameText), Parent: boundedText(p.Parent, maxNameText),
		State: boundedText(p.State, maxNameText), Labels: boundedMap(p.Labels), Tags: boundedMap(p.Tags),
		CreateTime: boundedText(p.CreateTime, maxNameText), UpdateTime: boundedText(p.UpdateTime, maxNameText),
		DeleteTime: boundedText(p.DeleteTime, maxNameText), Etag: boundedText(p.Etag, maxNameText),
		ManagementType: boundedText(p.ManagementType, maxNameText), IsManagementProject: p.IsManagementProject,
		ConfiguredCapabilities: boundedList(p.ConfiguredCapabilities),
	}
}

// folderWire is the Folder resource of cloudresourcemanager v3 as Google sends it.
type folderWire struct {
	Name                   string            `json:"name"`
	Parent                 string            `json:"parent"`
	DisplayName            string            `json:"displayName"`
	State                  string            `json:"state"`
	CreateTime             string            `json:"createTime"`
	UpdateTime             string            `json:"updateTime"`
	DeleteTime             string            `json:"deleteTime"`
	Etag                   string            `json:"etag"`
	Tags                   map[string]string `json:"tags"`
	ConfiguredCapabilities []string          `json:"configuredCapabilities"`
	ManagementProject      string            `json:"managementProject"`
}

// organizationWire is the Organization resource of cloudresourcemanager v3 as Google sends it.
type organizationWire struct {
	Name                string `json:"name"`
	DisplayName         string `json:"displayName"`
	DirectoryCustomerID string `json:"directoryCustomerId"`
	State               string `json:"state"`
	CreateTime          string `json:"createTime"`
	UpdateTime          string `json:"updateTime"`
	DeleteTime          string `json:"deleteTime"`
	Etag                string `json:"etag"`
}

// project reads one project, by ID or number. The answer must name a project.
func (c *client) project(ctx context.Context, op, tool, id string) (projectWire, error) {
	var project projectWire
	if err := c.get(ctx, op, tool, serviceResourceManager, "/v3/projects/"+url.PathEscape(id), nil, &project); err != nil {
		return projectWire{}, err
	}
	if !strings.HasPrefix(project.Name, prefixProject) {
		return projectWire{}, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return project, nil
}

// folder reads one folder. It is the one reading that proves where a folder sits, which is what binds a
// project below it to a target.
func (c *client) folder(ctx context.Context, op, tool, id string) (folderWire, error) {
	var folder folderWire
	if err := c.get(ctx, op, tool, serviceResourceManager, "/v3/folders/"+url.PathEscape(id), nil, &folder); err != nil {
		return folderWire{}, err
	}
	if folder.Name != prefixFolder+id {
		return folderWire{}, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return folder, nil
}

// organization reads one organization to prove that a target of that kind exists.
func (c *client) organization(ctx context.Context, op, tool, id string) error {
	var organization organizationWire
	if err := c.get(ctx, op, tool, serviceResourceManager, "/v3/organizations/"+url.PathEscape(id), nil, &organization); err != nil {
		return err
	}
	if organization.Name != prefixOrganization+id {
		return provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return nil
}

const projectOutputSchema = `{"type":"object","properties":{` +
	`"name":{"type":"string"},"projectId":{"type":"string"},"displayName":{"type":"string"},` +
	`"parent":{"type":"string"},"state":{"type":"string"},` +
	`"labels":{"type":"object","additionalProperties":{"type":"string"}},` +
	`"tags":{"type":"object","additionalProperties":{"type":"string"}},` +
	`"createTime":{"type":"string"},"updateTime":{"type":"string"},"deleteTime":{"type":"string"},` +
	`"etag":{"type":"string"},"managementType":{"type":"string"},"isManagementProject":{"type":"boolean"},` +
	`"configuredCapabilities":{"type":"array","items":{"type":"string"}}},` +
	`"required":["name"],"additionalProperties":false}`

var projectsGet = capability.Descriptor{
	ID: projectsGetID, Version: 1, Title: "Get a Google Cloud project",
	Description: "Read one project from Google Cloud Resource Manager by its project ID or number: its resource " +
		"name, project ID, display name, parent (an organization or a folder), lifecycle state, labels, tags, " +
		"timestamps, etag, and management type. Within a connection that has targets, the project must lie in " +
		"them: it is the project itself, or lies below a target folder or organization, which Qatlas proves by " +
		"reading the parent chain, and any other project is refused. The result is untrusted third-party " +
		"content; strings are cut.",
	Tags: []string{"google", "cloud", "resource-manager", "projects"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"project_id":{"type":"string","minLength":1,"maxLength":30,` +
		`"pattern":"^([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$"}},` +
		`"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(projectOutputSchema),
	Arguments: []capability.Argument{
		{Name: "project_id", Required: true, Description: "Project ID such as my-project-123, or the project " +
			"number; never a projects/ name or a path"},
	},
	Fields: []capability.Field{
		{Name: "name", Description: "Resource name of the project, projects/NUMBER"},
		{Name: "projectId", Description: "Project ID, unique across Google Cloud"},
		{Name: "displayName", Description: "Display name given by the project's owners; untrusted text"},
		{Name: "parent", Description: "Parent of the project, organizations/ID or folders/ID; absent without one"},
		{Name: "state", Description: "Lifecycle state: ACTIVE, DELETE_REQUESTED, or STATE_UNSPECIFIED"},
		{Name: "labels", Description: "Labels of the project as key and value; untrusted text, at most 100 entries"},
		{Name: "tags", Description: "Tag keys and values bound directly to the project, a Preview field Google " +
			"documents as input only; present only if Google sends it"},
		{Name: "createTime", Description: "Creation time, RFC 3339"},
		{Name: "updateTime", Description: "Time of the last change, RFC 3339"},
		{Name: "deleteTime", Description: "Time the project was marked for deletion, RFC 3339; absent while active"},
		{Name: "etag", Description: "Entity tag of the project"},
		{Name: "managementType", Description: "MANAGEMENT_TYPE_UNSPECIFIED, SELF_MANAGED, or HUB"},
		{Name: "isManagementProject", Description: "True for a management project that manages dedicated project groups"},
		{Name: "configuredCapabilities", Description: "Capabilities configured on the parent folder of a " +
			"management project, such as folders/123/capabilities/app-management"},
	},
	Examples: []capability.Example{{
		Description: "Read a project by its ID", Arguments: json.RawMessage(`{"project_id":"my-project-123"}`),
	}},
}

// handleProjectsGet refuses a project the targets exclude before any secret is read when that is decidable,
// and otherwise binds the project to a target with the reading of the project itself and the folders above it.
func handleProjectsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get project"
	var arguments struct {
		ProjectID string `json:"project_id"`
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
	if !validProject(arguments.ProjectID) {
		return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	}
	if allowed, decided := list.decidesProject(arguments.ProjectID); decided && !allowed {
		return nil, refuseOutside("the project")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	project, err := c.project(ctx, op, projectsGetID, arguments.ProjectID)
	if err != nil {
		return nil, err
	}
	admitted, err := newAdmitter(list, c, op, projectsGetID).project(ctx, project)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, refuseOutside("the project")
	}
	return project.result(), nil
}
