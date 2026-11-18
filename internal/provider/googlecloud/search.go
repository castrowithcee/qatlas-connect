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
	organizationsSearchID = Provider + ".organizations.search"
	foldersSearchID       = Provider + ".folders.search"
	projectsSearchID      = Provider + ".projects.search"
)

// Bounds of one search. The discovery document names no maximum page size, so the tool sets its own, and a
// page that holds more rows than twice that is an answer Qatlas does not trust.
const (
	maxPageSize       = 100
	maxPageRows       = 2 * maxPageSize
	maxCursorLength   = 4096
	maxPageTokenBytes = 3000
)

// searchArguments are the arguments of the three search tools; each tool takes the subset its schema names.
type searchArguments struct {
	Parent              string            `json:"parent"`
	ParentType          string            `json:"parent_type"`
	ParentID            string            `json:"parent_id"`
	ProjectID           string            `json:"project_id"`
	DisplayName         string            `json:"display_name"`
	State               string            `json:"state"`
	Labels              map[string]string `json:"labels"`
	LabelText           string            `json:"label_text"`
	DirectoryCustomerID string            `json:"directory_customer_id"`
	Domain              string            `json:"domain"`
	PageSize            *int              `json:"page_size"`
	Cursor              string            `json:"cursor"`
}

// searchSpec is what separates the three tools: the request, the member that holds the rows, the rule of the
// targets, and the query a tool builds from its arguments.
type searchSpec struct {
	id, op, path, list string
	// reaches says whether the targets let the tool run at all, so a refusal comes before any secret is read.
	reaches func(allowlist) bool
	refusal string
	filter  func(searchArguments) (string, error)
}

// searchPage reads one page of rows and the token of the next page. A row that is not a JSON object of the
// resource, or a page of an untrusted size, is an invalid response.
func searchPage[T any](ctx context.Context, c *client, op, tool, path string, list string, query url.Values) ([]T, string, error) {
	return servicePage[T](ctx, c, serviceResourceManager, op, tool, path, list, query)
}

// servicePage is searchPage for the list of any service.
func servicePage[T any](ctx context.Context, c *client, svc service, op, tool, path string, list string,
	query url.Values) ([]T, string, error) {
	var members map[string]json.RawMessage
	if err := c.get(ctx, op, tool, svc, path, query, &members); err != nil {
		return nil, "", err
	}
	invalid := func() error { return provider.InvalidResponse(op, subject+" returned an invalid response") }
	var rows []T
	if raw, found := members[list]; found && string(raw) != "null" {
		if err := json.Unmarshal(raw, &rows); err != nil || len(rows) > maxPageRows {
			return nil, "", invalid()
		}
	}
	var next string
	if raw, found := members["nextPageToken"]; found && string(raw) != "null" {
		if err := json.Unmarshal(raw, &next); err != nil || len(next) > maxPageTokenBytes {
			return nil, "", invalid()
		}
	}
	return rows, next, nil
}

func pageQuery(filter string, pageSize *int, token string) url.Values {
	query := url.Values{}
	if filter != "" {
		query.Set("query", filter)
	}
	if pageSize != nil {
		query.Set("pageSize", strconv.Itoa(*pageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	return query
}

// searchProjectsPage reads one page of projects.search without checking its rows. The connection test uses
// it with a page of one project, which costs nothing.
func (c *client) searchProjectsPage(ctx context.Context, op, tool string, pageSize int) error {
	_, _, err := searchPage[projectWire](ctx, c, op, tool, "/v3/projects:search", "projects", pageQuery("", &pageSize, ""))
	return err
}

// runSearch is the common course of the three tools. Everything Qatlas can refuse comes before the secret and
// the first request: the targets, the filter, and the cursor. Every row is then checked against the targets,
// whatever the server filter did, because Google may ignore a filter it does not understand.
func runSearch[T any](ctx context.Context, spec searchSpec, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage, admit func(context.Context, *admitter, T) (bool, error),
	shape func(T) any) (any, error) {
	var arguments searchArguments
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
	if !spec.reaches(list) {
		return nil, badRequest(spec.refusal)
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	filter, err := spec.filter(arguments)
	if err != nil {
		return nil, err
	}
	// The cursor holds Google's page token and binds the tool, the targets, and the filter, so it continues
	// only the search that issued it. The page size stays out: a next page may ask for another size.
	binding := provider.CursorBinding(spec.id, provider.TargetsOf(resolved), filter)
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this search; start again without cursor")
		}
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	rows, next, err := searchPage[T](ctx, c, spec.op, spec.id, spec.path, spec.list,
		pageQuery(filter, arguments.PageSize, token))
	if err != nil {
		return nil, err
	}
	gate := newAdmitter(list, c, spec.op, spec.id)
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

// invalidRow is the answer to a row that does not name the resource it should.
func invalidRow(op string) error {
	return provider.InvalidResponse(op, subject+" returned an invalid response")
}

// admitFolderRow and admitProjectRow check one row of a folder or project list: it must name its resource,
// and it must lie in the targets.
func admitFolderRow(ctx context.Context, a *admitter, f folderWire, op string) (bool, error) {
	if !strings.HasPrefix(f.Name, prefixFolder) || !numberPattern.MatchString(strings.TrimPrefix(f.Name, prefixFolder)) {
		return false, invalidRow(op)
	}
	return a.folder(ctx, f)
}

func admitProjectRow(ctx context.Context, a *admitter, p projectWire, op string) (bool, error) {
	if !strings.HasPrefix(p.Name, prefixProject) {
		return false, invalidRow(op)
	}
	return a.project(ctx, p)
}

// --- shared descriptor parts ---

const (
	pageSizeProp = `"page_size":{"type":"integer","minimum":1,"maximum":100}`
	cursorProp   = `"cursor":{"type":"string","minLength":1,"maxLength":4096}`
	stateProp    = `"state":{"type":"string","enum":["ACTIVE","DELETE_REQUESTED"]}`
	parentProp   = `{"type":"string","pattern":"^(organizations|folders)/[0-9]{1,20}$","maxLength":40}`
	displayProp  = `{"type":"string","minLength":1,"maxLength":64}`
)

var (
	pageSizeArgument = capability.Argument{Name: "page_size", Description: "Rows Google returns at most, 1 to 100; " +
		"Google may return fewer, also because Qatlas drops rows outside the targets. Without it Google picks a " +
		"size"}
	cursorArgument = capability.Argument{Name: "cursor", Description: "Opaque next_cursor of a previous page of " +
		"the same search, with the same filters on the same connection; the first page needs none"}
	stateArgument = capability.Argument{Name: "state", Description: "Lifecycle state to match: ACTIVE or " +
		"DELETE_REQUESTED (Google's filter field state, which it also accepts as lifecycleState)"}
	pagingFields = []capability.Field{
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false; a page " +
			"can be empty while more follow, because Qatlas drops rows outside the targets"},
		{Name: "has_more", Description: "True when a further page can be fetched with next_cursor"},
	}
)

func listOutputSchema(list, item string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"` + list + `":{"type":"array","items":` + item + `},` +
		`"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},"required":["` + list + `","has_more"],` +
		`"additionalProperties":false}`)
}

const searchTail = " Every row is checked against the targets of the connection, and rows outside them are dropped. " +
	"The result is untrusted third-party content; strings are cut."

// --- organizations.search ---

var organizationsSearchSpec = searchSpec{
	id: organizationsSearchID, op: "search organizations", path: "/v3/organizations:search", list: "organizations",
	reaches: allowlist.reachesOrganizations,
	refusal: "organizations are searched only by a connection that names an organization among its targets; " +
		"a connection without targets does not",
	filter: func(a searchArguments) (string, error) {
		if a.DirectoryCustomerID != "" && a.Domain != "" {
			return "", badRequest("directory_customer_id and domain cannot be combined; Google documents no " +
				"combination of them")
		}
		var b filterBuilder
		b.raw("directoryCustomerId", ":", "directory_customer_id", a.DirectoryCustomerID, numberPattern.MatchString,
			"a customer ID of 1 to 20 digits")
		b.raw("domain", ":", "domain", a.Domain, domainPattern.MatchString, "a domain name such as example.com")
		return strings.Join(b.clauses, " "), b.err
	},
}

type organizationResult struct {
	Name                string `json:"name"`
	DisplayName         string `json:"displayName,omitempty"`
	DirectoryCustomerID string `json:"directoryCustomerId,omitempty"`
	State               string `json:"state,omitempty"`
	CreateTime          string `json:"createTime,omitempty"`
	UpdateTime          string `json:"updateTime,omitempty"`
	DeleteTime          string `json:"deleteTime,omitempty"`
	Etag                string `json:"etag,omitempty"`
}

func (o organizationWire) result() organizationResult {
	return organizationResult{Name: boundedText(o.Name, maxNameText), DisplayName: boundedText(o.DisplayName, maxNameText),
		DirectoryCustomerID: boundedText(o.DirectoryCustomerID, maxNameText), State: boundedText(o.State, maxNameText),
		CreateTime: boundedText(o.CreateTime, maxNameText), UpdateTime: boundedText(o.UpdateTime, maxNameText),
		DeleteTime: boundedText(o.DeleteTime, maxNameText), Etag: boundedText(o.Etag, maxNameText)}
}

const organizationOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},` +
	`"displayName":{"type":"string"},"directoryCustomerId":{"type":"string"},"state":{"type":"string"},` +
	`"createTime":{"type":"string"},"updateTime":{"type":"string"},"deleteTime":{"type":"string"},` +
	`"etag":{"type":"string"}},"required":["name"],"additionalProperties":false}`

var organizationsSearch = capability.Descriptor{
	ID: organizationsSearchID, Version: 1, Title: "Search Google Cloud organizations",
	Description: "Search the organizations the credential can see in Google Cloud Resource Manager, optionally " +
		"by directory customer ID or by domain: each row has its resource name, display name, directory " +
		"customer ID, lifecycle state, timestamps, and etag. The tool runs only on a connection that names an " +
		"organization among its targets, and returns only the organizations it names." + searchTail,
	Tags: []string{"google", "cloud", "resource-manager", "organizations"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"directory_customer_id":{"type":"string","pattern":"^[0-9]{1,20}$","maxLength":20},` +
		`"domain":{"type":"string","minLength":1,"maxLength":253},` + pageSizeProp + `,` + cursorProp + `},` +
		`"additionalProperties":false}`),
	OutputSchema: listOutputSchema("organizations", organizationOutputSchema),
	Arguments: []capability.Argument{
		{Name: "directory_customer_id", Description: "Match organizations of this Google Workspace or Cloud " +
			"Identity customer ID (digits); cannot be combined with domain"},
		{Name: "domain", Description: "Match the organization of this Workspace domain, such as example.com; " +
			"cannot be combined with directory_customer_id"},
		pageSizeArgument, cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "organizations", Description: "Organizations of the page, each {name, displayName, " +
			"directoryCustomerId, state, createTime, updateTime, deleteTime, etag}; deleteTime is absent unless " +
			"deletion was requested"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "Find the organization of a domain", Arguments: json.RawMessage(`{"domain":"example.com"}`),
	}},
}

func handleOrganizationsSearch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return runSearch(ctx, organizationsSearchSpec, resolved, secrets, red, raw,
		func(_ context.Context, a *admitter, o organizationWire) (bool, error) {
			if !numberPattern.MatchString(strings.TrimPrefix(o.Name, prefixOrganization)) ||
				!strings.HasPrefix(o.Name, prefixOrganization) {
				return false, invalidRow(organizationsSearchSpec.op)
			}
			return a.organization(o), nil
		}, func(o organizationWire) any { return o.result() })
}

// --- folders.search ---

var foldersSearchSpec = searchSpec{
	id: foldersSearchID, op: "search folders", path: "/v3/folders:search", list: "folders",
	reaches: allowlist.reachesFolders,
	refusal: "folders are searched only by a connection without targets or with an organization or a folder " +
		"among them",
	// Google documents AND for folders, so the clauses narrow each other.
	filter: func(a searchArguments) (string, error) {
		var b filterBuilder
		b.raw("parent", "=", "parent", a.Parent, isParent, "organizations/ID or folders/ID")
		b.add("displayName", "=", "display_name", a.DisplayName, true)
		b.raw("state", "=", "state", a.State, isState, "ACTIVE or DELETE_REQUESTED")
		return strings.Join(b.clauses, " AND "), b.err
	},
}

type folderResult struct {
	Name                   string            `json:"name"`
	Parent                 string            `json:"parent,omitempty"`
	DisplayName            string            `json:"displayName,omitempty"`
	State                  string            `json:"state,omitempty"`
	CreateTime             string            `json:"createTime,omitempty"`
	UpdateTime             string            `json:"updateTime,omitempty"`
	DeleteTime             string            `json:"deleteTime,omitempty"`
	Etag                   string            `json:"etag,omitempty"`
	Tags                   map[string]string `json:"tags,omitempty"`
	ConfiguredCapabilities []string          `json:"configuredCapabilities,omitempty"`
	ManagementProject      string            `json:"managementProject,omitempty"`
}

func (f folderWire) result() folderResult {
	return folderResult{Name: boundedText(f.Name, maxNameText), Parent: boundedText(f.Parent, maxNameText),
		DisplayName: boundedText(f.DisplayName, maxNameText), State: boundedText(f.State, maxNameText),
		CreateTime: boundedText(f.CreateTime, maxNameText), UpdateTime: boundedText(f.UpdateTime, maxNameText),
		DeleteTime: boundedText(f.DeleteTime, maxNameText), Etag: boundedText(f.Etag, maxNameText),
		Tags: boundedMap(f.Tags), ConfiguredCapabilities: boundedList(f.ConfiguredCapabilities),
		ManagementProject: boundedText(f.ManagementProject, maxNameText)}
}

const folderOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"parent":{"type":"string"},` +
	`"displayName":{"type":"string"},"state":{"type":"string"},"createTime":{"type":"string"},` +
	`"updateTime":{"type":"string"},"deleteTime":{"type":"string"},"etag":{"type":"string"},` +
	`"tags":{"type":"object","additionalProperties":{"type":"string"}},` +
	`"configuredCapabilities":{"type":"array","items":{"type":"string"}},"managementProject":{"type":"string"}},` +
	`"required":["name"],"additionalProperties":false}`

var foldersSearch = capability.Descriptor{
	ID: foldersSearchID, Version: 1, Title: "Search Google Cloud folders",
	Description: "Search the folders the credential can see in Google Cloud Resource Manager, optionally by " +
		"parent, display name, and state; Google combines the filters with AND. Each row has its resource name, " +
		"parent, display name, lifecycle state, timestamps, etag, and the capabilities and management project " +
		"Google reports. Within a connection that has targets, a folder must be a target or lie below one, " +
		"which Qatlas proves from the parent of the row and, where that does not decide, by reading the folders " +
		"above it." + searchTail,
	Tags: []string{"google", "cloud", "resource-manager", "folders"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"parent":` + parentProp + `,` +
		`"display_name":` + displayProp + `,` + stateProp + `,` + pageSizeProp + `,` + cursorProp + `},` +
		`"additionalProperties":false}`),
	OutputSchema: listOutputSchema("folders", folderOutputSchema),
	Arguments: []capability.Argument{
		{Name: "parent", Description: "Match folders directly below this parent, organizations/ID or folders/ID"},
		{Name: "display_name", Description: "Match this display name; a value with whitespace is quoted for " +
			"Google, and a trailing * matches a prefix (not with whitespace); at most 64 characters, no quotes or " +
			"backslashes"},
		stateArgument, pageSizeArgument, cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "folders", Description: "Folders of the page, each {name, parent, displayName, state, createTime, " +
			"updateTime, deleteTime, etag, tags, configuredCapabilities, managementProject}; tags is a Preview " +
			"field Google documents as input only and appears only if Google sends it"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the active folders below an organization",
		Arguments:   json.RawMessage(`{"parent":"organizations/123456789","state":"ACTIVE"}`),
	}},
}

func handleFoldersSearch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return runSearch(ctx, foldersSearchSpec, resolved, secrets, red, raw,
		func(ctx context.Context, a *admitter, f folderWire) (bool, error) {
			return admitFolderRow(ctx, a, f, foldersSearchSpec.op)
		}, func(f folderWire) any { return f.result() })
}

// --- projects.search ---

var projectsSearchSpec = searchSpec{
	id: projectsSearchID, op: "search projects", path: "/v3/projects:search", list: "projects",
	reaches: allowlist.reachesResources,
	refusal: "projects are searched only by a connection without targets or with an organization, a folder, or " +
		"a project among them",
	// Google documents that a project matches if it matches any of the clauses of its query, so the clauses
	// are separated by a space and widen each other.
	filter: func(a searchArguments) (string, error) {
		var b filterBuilder
		b.raw("parent", ":", "parent", a.Parent, isParent, "organizations/ID or folders/ID")
		b.raw("parent.type", ":", "parent_type", a.ParentType, func(v string) bool { return v == "folder" || v == "organization" },
			"folder or organization")
		b.raw("parent.id", ":", "parent_id", a.ParentID, numberPattern.MatchString, "an ID of 1 to 20 digits")
		b.raw("projectId", ":", "project_id", a.ProjectID, projectIDFilter.MatchString,
			"a project ID or the start of one followed by *")
		b.add("displayName", ":", "display_name", a.DisplayName, true)
		b.raw("state", ":", "state", a.State, isState, "ACTIVE or DELETE_REQUESTED")
		b.add("labels", ":", "label_text", a.LabelText, false)
		b.labels(a.Labels, ":")
		return strings.Join(b.clauses, " "), b.err
	},
}

var projectsSearch = capability.Descriptor{
	ID: projectsSearchID, Version: 1, Title: "Search Google Cloud projects",
	Description: "Search the projects the credential can see in Google Cloud Resource Manager, optionally by " +
		"parent, project ID, display name, state, and labels. Google matches a project that fits any one of " +
		"the filters given, so more filters return more projects, not fewer. Each row has its resource name, " +
		"project ID, display name, parent, lifecycle state, labels, tags, timestamps, etag, and management " +
		"type. Within a connection that has targets, a project must be a target or lie below one, which " +
		"Qatlas proves from the parent of the row and, where that does not decide, by reading the folders above " +
		"it." + searchTail,
	Tags: []string{"google", "cloud", "resource-manager", "projects"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"parent":` + parentProp + `,` +
		`"parent_type":{"type":"string","enum":["folder","organization"]},` +
		`"parent_id":{"type":"string","pattern":"^[0-9]{1,20}$","maxLength":20},` +
		`"project_id":{"type":"string","pattern":"^[a-z][a-z0-9-]{0,29}\\*?$","maxLength":31},` +
		`"display_name":` + displayProp + `,` + stateProp + `,` +
		`"label_text":` + displayProp + `,` +
		`"labels":{"type":"object","maxProperties":10,"additionalProperties":{"type":"string","minLength":1,"maxLength":63}},` +
		pageSizeProp + `,` + cursorProp + `},"additionalProperties":false}`),
	OutputSchema: listOutputSchema("projects", projectOutputSchema),
	Arguments: []capability.Argument{
		{Name: "parent", Description: "Match projects directly below this parent, organizations/ID or folders/ID " +
			"(Google's parent field, which it prefers over parent_type and parent_id)"},
		{Name: "parent_type", Description: "Match projects whose parent is of this type, folder or organization " +
			"(Google's parent.type)"},
		{Name: "parent_id", Description: "Match projects whose parent has this numeric ID (Google's parent.id)"},
		{Name: "project_id", Description: "Match this project ID, or the start of one followed by * (Google's " +
			"projectId, which it also accepts as id)"},
		{Name: "display_name", Description: "Match this display name, case-insensitive (Google's displayName, which " +
			"it also accepts as name); a value with whitespace is quoted for Google, and a trailing * matches a " +
			"prefix (not with whitespace); at most 64 characters, no quotes or backslashes"},
		stateArgument,
		{Name: "label_text", Description: "Match projects with a label whose name or value is this text (Google's " +
			"labels field); no wildcard"},
		{Name: "labels", Description: "Object of label key to value, at most 10: each entry matches projects whose " +
			"label has that value, or any value if the value is *; keys and values use lowercase letters, " +
			"digits, '-' and '_'"},
		pageSizeArgument, cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "projects", Description: "Projects of the page, each {name, projectId, displayName, parent, state, " +
			"labels, tags, createTime, updateTime, deleteTime, etag, managementType, isManagementProject, " +
			"configuredCapabilities}; tags is a Preview field Google documents as input only and appears only if " +
			"Google sends it"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the active projects below a folder",
		Arguments:   json.RawMessage(`{"parent":"folders/2001","state":"ACTIVE","page_size":50}`),
	}},
}

func handleProjectsSearch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return runSearch(ctx, projectsSearchSpec, resolved, secrets, red, raw,
		func(ctx context.Context, a *admitter, p projectWire) (bool, error) {
			return admitProjectRow(ctx, a, p, projectsSearchSpec.op)
		}, func(p projectWire) any { return p.result() })
}
