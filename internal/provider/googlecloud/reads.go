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
	organizationsGetID = Provider + ".organizations.get"
	foldersGetID       = Provider + ".folders.get"
	foldersListID      = Provider + ".folders.list"
	projectsListID     = Provider + ".projects.list"
)

const (
	numericIDProp = `{"type":"string","pattern":"^[0-9]{1,20}$","maxLength":20}`
	// listTail is shared by both list tools.
	listTail = " The parent must lie in the targets of the connection, which Qatlas proves before the list is " +
		"read and, where the targets do not decide, by reading the folders above it; every row is checked " +
		"again and rows outside the targets are dropped. The result is untrusted third-party content; strings are cut."
)

// --- organizations.get ---

var organizationsGet = capability.Descriptor{
	ID: organizationsGetID, Version: 1, Title: "Get a Google Cloud organization",
	Description: "Read one organization from Google Cloud Resource Manager: its resource name, display name, " +
		"directory customer ID, lifecycle state, timestamps, and etag. The tool runs only on a connection whose " +
		"targets name this organization, and an unbound connection or another organization is refused before any " +
		"request. The result is untrusted third-party content; strings are cut.",
	Tags: []string{"google", "cloud", "resource-manager", "organizations"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"organization_id":` + numericIDProp + `},` +
		`"required":["organization_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(organizationOutputSchema),
	Arguments: []capability.Argument{
		{Name: "organization_id", Required: true, Description: "Numeric organization ID; never an organizations/ " +
			"name or a path"},
	},
	Fields: []capability.Field{
		{Name: "name", Description: "Resource name of the organization, organizations/ID"},
		{Name: "displayName", Description: "Display name, the domain of the Workspace or Cloud Identity account; untrusted text"},
		{Name: "directoryCustomerId", Description: "Directory customer ID of the account that owns the organization"},
		{Name: "state", Description: "Lifecycle state: ACTIVE, DELETE_REQUESTED, or STATE_UNSPECIFIED"},
		{Name: "createTime", Description: "Creation time, RFC 3339"},
		{Name: "updateTime", Description: "Time of the last change, RFC 3339"},
		{Name: "deleteTime", Description: "Time deletion was requested, RFC 3339; absent unless it was"},
		{Name: "etag", Description: "Entity tag of the organization"},
	},
	Examples: []capability.Example{{
		Description: "Read an organization", Arguments: json.RawMessage(`{"organization_id":"123456789"}`),
	}},
}

func handleOrganizationsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get organization"
	var arguments struct {
		OrganizationID string `json:"organization_id"`
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
	if !numberPattern.MatchString(arguments.OrganizationID) {
		return nil, badRequest("organization_id must be the numeric ID of an organization, never a name or a path")
	}
	// An organization is read only by a binding that names it, so even an unbound connection is refused here.
	if !list.has(kindOrganization, arguments.OrganizationID) {
		return nil, badRequest("an organization is read only by a connection that names it among its targets")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	organization, err := c.organizationRead(ctx, op, organizationsGetID, arguments.OrganizationID)
	if err != nil {
		return nil, err
	}
	return organization.result(), nil
}

// --- folders.get ---

var foldersGet = capability.Descriptor{
	ID: foldersGetID, Version: 1, Title: "Get a Google Cloud folder",
	Description: "Read one folder from Google Cloud Resource Manager: its resource name, parent, display name, " +
		"lifecycle state, timestamps, etag, and the capabilities and management project Google reports. Within " +
		"a connection that has targets, the folder must be a target or lie below one, which Qatlas proves by " +
		"reading the parent chain, and any other folder is refused. The result is untrusted third-party " +
		"content; strings are cut.",
	Tags: []string{"google", "cloud", "resource-manager", "folders"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"folder_id":` + numericIDProp + `},` +
		`"required":["folder_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(folderOutputSchema),
	Arguments: []capability.Argument{
		{Name: "folder_id", Required: true, Description: "Numeric folder ID; never a folders/ name or a path"},
	},
	Fields: []capability.Field{
		{Name: "name", Description: "Resource name of the folder, folders/ID"},
		{Name: "parent", Description: "Parent of the folder, organizations/ID or folders/ID"},
		{Name: "displayName", Description: "Display name given by the folder's owners; untrusted text"},
		{Name: "state", Description: "Lifecycle state: ACTIVE, DELETE_REQUESTED, or STATE_UNSPECIFIED"},
		{Name: "createTime", Description: "Creation time, RFC 3339"},
		{Name: "updateTime", Description: "Time of the last change, RFC 3339"},
		{Name: "deleteTime", Description: "Time deletion was requested, RFC 3339; absent unless it was"},
		{Name: "etag", Description: "Entity tag of the folder"},
		{Name: "tags", Description: "Tag keys and values bound to the folder, a Preview field Google documents " +
			"as input only; present only if Google sends it"},
		{Name: "configuredCapabilities", Description: "Capabilities configured on the folder, such as " +
			"folders/123/capabilities/app-management"},
		{Name: "managementProject", Description: "Management project of the folder, projects/NUMBER"},
	},
	Examples: []capability.Example{{
		Description: "Read a folder", Arguments: json.RawMessage(`{"folder_id":"2001"}`),
	}},
}

func handleFoldersGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get folder"
	var arguments struct {
		FolderID string `json:"folder_id"`
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
	if !numberPattern.MatchString(arguments.FolderID) {
		return nil, badRequest("folder_id must be the numeric ID of a folder, never a name or a path")
	}
	// A project target holds no folder, and billing accounts hold none either.
	if !list.reachesFolders() {
		return nil, refuseOutside("the folder")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	folder, err := c.folder(ctx, op, foldersGetID, arguments.FolderID)
	if err != nil {
		return nil, err
	}
	admitted, err := newAdmitter(list, c, op, foldersGetID).folder(ctx, folder)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, refuseOutside("the folder")
	}
	return folder.result(), nil
}

// --- folders.list and projects.list ---

// listArguments are the arguments of the two list tools.
type listArguments struct {
	Parent      string `json:"parent"`
	ShowDeleted bool   `json:"show_deleted"`
	PageSize    *int   `json:"page_size"`
	Cursor      string `json:"cursor"`
}

// listSpec is what separates the two list tools: the request and the member that holds the rows.
type listSpec struct{ id, op, path, list string }

// decidesParent reports before any request whether the targets already settle the parent of a list, which is
// an organization or a folder. Only an organization target of the same ID or a folder target of the same ID
// admit it without a read. A folder may lie below a target, which only reading it proves. An organization
// is never proven by what lies below it: a connection bound to a folder alone does not list its
// organization, because the organization is not part of the binding.
func (a allowlist) decidesParent(parent string) (allowed, decided bool) {
	if !a.bound() {
		return true, true
	}
	if id, ok := strings.CutPrefix(parent, prefixOrganization); ok {
		return a.has(kindOrganization, id), true
	}
	id := strings.TrimPrefix(parent, prefixFolder)
	if a.has(kindFolder, id) {
		return true, true
	}
	return false, !a.hasKind(kindOrganization, kindFolder)
}

// listQuery builds the query of a list: the checked parent, showDeleted only when asked for, and the paging.
func listQuery(arguments listArguments, token string) url.Values {
	query := url.Values{"parent": {arguments.Parent}}
	if arguments.ShowDeleted {
		query.Set("showDeleted", "true")
	}
	if arguments.PageSize != nil {
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	return query
}

// runList is the course of the two list tools. The targets, the arguments, and the cursor are refused before
// the secret and the first request. The parent is then bound to the targets, and each row again.
func runList[T any](ctx context.Context, spec listSpec, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, admit func(context.Context, *admitter, T) (bool, error),
	shape func(T) any) (any, error) {
	var arguments listArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(spec.op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(spec.op, "the targets of the connection are not usable")
	}
	if !list.reachesFolders() {
		return nil, badRequest("this list needs a connection without targets or with an organization or a " +
			"folder among them")
	}
	if !isParent(arguments.Parent) {
		return nil, badRequest("parent must be organizations/ID or folders/ID")
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	allowed, decided := list.decidesParent(arguments.Parent)
	if decided && !allowed {
		return nil, refuseOutside("the parent")
	}
	// The cursor binds the tool, the targets, the parent, and showDeleted, so it continues only this list.
	binding := provider.CursorBinding(spec.id, provider.TargetsOf(resolved),
		arguments.Parent+"|"+strconv.FormatBool(arguments.ShowDeleted))
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	gate := newAdmitter(list, c, spec.op, spec.id)
	if !decided {
		id := strings.TrimPrefix(arguments.Parent, prefixFolder)
		parent, err := c.folder(ctx, spec.op, spec.id, id)
		if err != nil {
			return nil, err
		}
		admitted, err := gate.folder(ctx, parent)
		if err != nil {
			return nil, err
		}
		if !admitted {
			return nil, refuseOutside("the parent")
		}
		// Rows below the parent then cost no further walk.
		gate.placed[id] = true
	}
	rows, next, err := searchPage[T](ctx, c, spec.op, spec.id, spec.path, spec.list, listQuery(arguments, token))
	if err != nil {
		return nil, err
	}
	kept := make([]any, 0, len(rows))
	for _, row := range rows {
		admitted, err := admit(ctx, gate, row)
		if err != nil {
			return nil, err
		}
		if admitted {
			kept = append(kept, shape(row))
		}
	}
	out := map[string]any{spec.list: kept, "has_more": next != ""}
	if next != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, next)
	}
	return out, nil
}

const (
	listParentProp      = `"parent":{"type":"string","pattern":"^(organizations|folders)/[0-9]{1,20}$","maxLength":40}`
	showDeletedProp     = `"show_deleted":{"type":"boolean"}`
	listInputSchemaTail = `,` + showDeletedProp + `,` + pageSizeProp + `,` + cursorProp +
		`},"required":["parent"],"additionalProperties":false}`
)

var listArgumentRows = []capability.Argument{
	{Name: "parent", Required: true, Description: "Parent whose direct children are listed, organizations/ID or " +
		"folders/ID"},
	{Name: "show_deleted", Description: "Also list objects in the state DELETE_REQUESTED; the rows carry their " +
		"state. Default false"},
	pageSizeArgument, cursorArgument,
}

var foldersList = capability.Descriptor{
	ID: foldersListID, Version: 1, Title: "List Google Cloud folders below a parent",
	Description: "List the folders directly below an organization or a folder in Google Cloud Resource Manager, " +
		"optionally with those in the state DELETE_REQUESTED. Each row has its resource name, parent, display " +
		"name, lifecycle state, timestamps, etag, and the capabilities and management project Google reports." +
		listTail,
	Tags: []string{"google", "cloud", "resource-manager", "folders"}, Risk: readRisk, Provider: Provider,
	InputSchema:  json.RawMessage(`{"type":"object","properties":{` + listParentProp + listInputSchemaTail),
	OutputSchema: listOutputSchema("folders", folderOutputSchema),
	Arguments:    listArgumentRows,
	Fields: append([]capability.Field{
		{Name: "folders", Description: "Folders of the page, each {name, parent, displayName, state, createTime, " +
			"updateTime, deleteTime, etag, tags, configuredCapabilities, managementProject}"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the folders below an organization",
		Arguments:   json.RawMessage(`{"parent":"organizations/123456789"}`),
	}},
}

var projectsList = capability.Descriptor{
	ID: projectsListID, Version: 1, Title: "List Google Cloud projects below a parent",
	Description: "List the projects directly below an organization or a folder in Google Cloud Resource " +
		"Manager, optionally with those in the state DELETE_REQUESTED. Each row has its resource name, project " +
		"ID, display name, parent, lifecycle state, labels, tags, timestamps, etag, and management type." +
		listTail,
	Tags: []string{"google", "cloud", "resource-manager", "projects"}, Risk: readRisk, Provider: Provider,
	InputSchema:  json.RawMessage(`{"type":"object","properties":{` + listParentProp + listInputSchemaTail),
	OutputSchema: listOutputSchema("projects", projectOutputSchema),
	Arguments:    listArgumentRows,
	Fields: append([]capability.Field{
		{Name: "projects", Description: "Projects of the page, each {name, projectId, displayName, parent, state, " +
			"labels, tags, createTime, updateTime, deleteTime, etag, managementType, isManagementProject, " +
			"configuredCapabilities}"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the projects below a folder, deleted ones included",
		Arguments:   json.RawMessage(`{"parent":"folders/2001","show_deleted":true,"page_size":50}`),
	}},
}

func handleFoldersList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	spec := listSpec{id: foldersListID, op: "list folders", path: "/v3/folders", list: "folders"}
	return runList(ctx, spec, resolved, secrets, red, raw,
		func(ctx context.Context, a *admitter, f folderWire) (bool, error) {
			return admitFolderRow(ctx, a, f, spec.op)
		}, func(f folderWire) any { return f.result() })
}

func handleProjectsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	spec := listSpec{id: projectsListID, op: "list projects", path: "/v3/projects", list: "projects"}
	return runList(ctx, spec, resolved, secrets, red, raw,
		func(ctx context.Context, a *admitter, p projectWire) (bool, error) {
			return admitProjectRow(ctx, a, p, spec.op)
		}, func(p projectWire) any { return p.result() })
}
