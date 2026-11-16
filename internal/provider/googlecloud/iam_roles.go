package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	iamRolesListID        = Provider + ".iam-roles.list"
	iamRolesGetID         = Provider + ".iam-roles.get"
	iamRolesQueryID       = Provider + ".iam-roles.query-grantable"
	iamPermissionsQueryID = Provider + ".iam-permissions.query-testable"
)

// Bounds of the IAM reads. A role with includedPermissions can hold thousands of permissions, so a page of
// FULL roles is small, and a role is cut at maxRolePermissions. The discovery document allows larger pages
// (1000 for roles.list, 3000 for queryGrantableRoles, 1000 for queryTestablePermissions); these are Qatlas
// bounds that keep an answer inside the response limit.
const (
	maxRolesPage           = 500
	defaultRolesPage       = 100
	maxFullRolesPage       = 10
	maxPermissionsPage     = 500
	defaultPermissionsPage = 100
	maxRolePermissions     = 1000
)

const (
	typePredefined = "predefined"
	typeProject    = "project"
	typeFolder     = "folder"
	typeOrg        = "organization"
)

// resourceManagerName is the prefix of the full resource name of a Resource Manager resource.
const resourceManagerName = "//cloudresourcemanager.googleapis.com/"

var (
	roleIDPattern      = `[A-Za-z0-9][A-Za-z0-9._-]{0,199}`
	predefinedRoleName = regexp.MustCompile(`^roles/(` + roleIDPattern + `)$`)
	projectRoleName    = regexp.MustCompile(`^projects/([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])/roles/(` + roleIDPattern + `)$`)
	organizationRole   = regexp.MustCompile(`^organizations/([0-9]{1,20})/roles/(` + roleIDPattern + `)$`)
	permissionName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)
)

// iamArguments are the arguments of the four IAM tools; each takes the subset its schema names.
type iamArguments struct {
	ParentType   string `json:"parent_type"`
	ParentID     string `json:"parent_id"`
	Role         string `json:"role"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	View         string `json:"view"`
	ShowDeleted  bool   `json:"show_deleted"`
	PageSize     *int   `json:"page_size"`
	Cursor       string `json:"cursor"`
}

// scopeRef is a resource of the hierarchy that a tool names by type and ID. It is never free text: the type
// comes from a fixed list and the ID from a fixed pattern, and every name Qatlas sends is built from them.
type scopeRef struct{ typ, id string }

func (r scopeRef) valid() bool {
	switch r.typ {
	case typeProject:
		return validProject(r.id)
	case typeFolder, typeOrg:
		return numberPattern.MatchString(r.id)
	}
	return false
}

// precheck refuses before any secret is read what the targets already decide. A project the targets settle by
// name is refused here, and a folder needs an organization or folder target to lie in. An organization is
// reached only if a target names exactly that organization, even on a connection without targets.
func (r scopeRef) precheck(list allowlist) error {
	switch r.typ {
	case typeProject:
		if allowed, decided := list.decidesProject(r.id); decided && !allowed {
			return refuseOutside("the project")
		}
	case typeFolder:
		if !list.reachesFolders() {
			return refuseOutside("the folder")
		}
	case typeOrg:
		if !list.has(kindOrganization, r.id) {
			return badRequest("organization resources are used only by a connection that names exactly this " +
				"organization among its targets; a connection without targets does not")
		}
	}
	return nil
}

// prove binds the resource to the targets with the reading of the resource and, for a project or a folder,
// of the folders above it, and returns its canonical name: projects/PROJECT_ID, folders/ID, or
// organizations/ID. A project is named by the project ID that Google reports, which is the form the IAM
// methods document.
func (r scopeRef) prove(ctx context.Context, c *client, list allowlist, op, tool string) (string, error) {
	gate := newAdmitter(list, c, op, tool)
	switch r.typ {
	case typeProject:
		project, err := c.project(ctx, op, tool, r.id)
		if err != nil {
			return "", err
		}
		admitted, err := gate.project(ctx, project)
		if err != nil {
			return "", err
		}
		if !admitted {
			return "", refuseOutside("the project")
		}
		if !projectIDShape.MatchString(project.ProjectID) {
			return "", provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		return prefixProject + project.ProjectID, nil
	case typeFolder:
		folder, err := c.folder(ctx, op, tool, r.id)
		if err != nil {
			return "", err
		}
		admitted, err := gate.folder(ctx, folder)
		if err != nil {
			return "", err
		}
		if !admitted {
			return "", refuseOutside("the folder")
		}
		return folder.Name, nil
	}
	return prefixOrganization + r.id, nil
}

// --- role and permission rows ---

type roleWire struct {
	Name                string   `json:"name"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	IncludedPermissions []string `json:"includedPermissions"`
	Stage               string   `json:"stage"`
	Etag                string   `json:"etag"`
	Deleted             bool     `json:"deleted"`
}

type roleResult struct {
	Name                     string   `json:"name"`
	Title                    string   `json:"title,omitempty"`
	Description              string   `json:"description,omitempty"`
	IncludedPermissions      []string `json:"includedPermissions,omitempty"`
	IncludedPermissionsTotal int      `json:"includedPermissionsTotal,omitempty"`
	Stage                    string   `json:"stage,omitempty"`
	Etag                     string   `json:"etag,omitempty"`
	Deleted                  bool     `json:"deleted,omitempty"`
}

// result bounds a role. A role with more than maxRolePermissions permissions is cut, and the count of what
// Google sent says so.
func (r roleWire) result() roleResult {
	out := roleResult{Name: boundedText(r.Name, maxNameText), Title: boundedText(r.Title, maxNameText),
		Description: boundedText(r.Description, maxText), Stage: boundedText(r.Stage, maxNameText),
		Etag: boundedText(r.Etag, maxNameText), Deleted: r.Deleted}
	kept := r.IncludedPermissions[:min(len(r.IncludedPermissions), maxRolePermissions)]
	for _, name := range kept {
		out.IncludedPermissions = append(out.IncludedPermissions, boundedText(name, maxNameText))
	}
	if len(r.IncludedPermissions) > len(kept) {
		out.IncludedPermissionsTotal = len(r.IncludedPermissions)
	}
	return out
}

type permissionWire struct {
	Name                    string `json:"name"`
	Title                   string `json:"title"`
	Description             string `json:"description"`
	Stage                   string `json:"stage"`
	CustomRolesSupportLevel string `json:"customRolesSupportLevel"`
	APIDisabled             bool   `json:"apiDisabled"`
	PrimaryPermission       string `json:"primaryPermission"`
}

type permissionResult struct {
	Name                    string `json:"name"`
	Title                   string `json:"title,omitempty"`
	Description             string `json:"description,omitempty"`
	Stage                   string `json:"stage,omitempty"`
	CustomRolesSupportLevel string `json:"customRolesSupportLevel,omitempty"`
	APIDisabled             bool   `json:"apiDisabled,omitempty"`
	PrimaryPermission       string `json:"primaryPermission,omitempty"`
}

func (p permissionWire) result() permissionResult {
	return permissionResult{Name: boundedText(p.Name, maxNameText), Title: boundedText(p.Title, maxNameText),
		Description: boundedText(p.Description, maxText), Stage: boundedText(p.Stage, maxNameText),
		CustomRolesSupportLevel: boundedText(p.CustomRolesSupportLevel, maxNameText), APIDisabled: p.APIDisabled,
		PrimaryPermission: boundedText(p.PrimaryPermission, maxNameText)}
}

// decodePage reads the rows under list and the next page token of one answer. A row that is not an object, a
// page of an untrusted size, and a token that is too long are an invalid response.
func decodePage[T any](op string, members map[string]json.RawMessage, list string, maxRows int) ([]T, string, error) {
	invalid := func() error { return provider.InvalidResponse(op, subject+" returned an invalid response") }
	var rows []T
	if raw, found := members[list]; found && string(raw) != "null" {
		if err := json.Unmarshal(raw, &rows); err != nil || len(rows) > maxRows {
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

// pageSizeOf returns the page size to send. Without one Qatlas picks its own default, so the size of an
// answer is always bounded.
func pageSizeOf(requested *int, def, limit int) (int, error) {
	if requested == nil {
		return min(def, limit), nil
	}
	if *requested < 1 || *requested > limit {
		return 0, badRequest("page_size must be 1 to " + strconv.Itoa(limit) + " for this request")
	}
	return *requested, nil
}

// viewOf checks the view argument. An empty view is Google's default, BASIC.
func viewOf(view string) (string, error) {
	switch view {
	case "", "BASIC", "FULL":
		return view, nil
	}
	return "", badRequest("view must be BASIC or FULL")
}

func rolesPageLimit(view string) int {
	if view == "FULL" {
		return maxFullRolesPage
	}
	return maxRolesPage
}

// pageResult builds the answer of a page: the rows, whether more follow, and the cursor bound to the request.
func pageResult(list string, rows []any, binding []byte, next string) map[string]any {
	out := map[string]any{list: rows, "has_more": next != ""}
	if next != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, next)
	}
	return out
}

// decodeCursor returns Google's page token a cursor continues with.
func decodeCursor(binding []byte, cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	token, ok := provider.DecodeCursor(binding, cursor, maxCursorLength)
	if !ok {
		return "", badRequest("cursor is not a next_cursor of this request; start again without cursor")
	}
	return token, nil
}

// --- shared descriptor parts ---

const (
	viewProp         = `"view":{"type":"string","enum":["BASIC","FULL"]}`
	roleOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"title":{"type":"string"},` +
		`"description":{"type":"string"},"includedPermissions":{"type":"array","items":{"type":"string"}},` +
		`"includedPermissionsTotal":{"type":"integer"},"stage":{"type":"string"},"etag":{"type":"string"},` +
		`"deleted":{"type":"boolean"}},"required":["name"],"additionalProperties":false}`
	roleFieldText = "{name, title, description, includedPermissions (only with view FULL), " +
		"includedPermissionsTotal, stage, etag, deleted}"
)

var (
	viewArgument = capability.Argument{Name: "view", Description: "BASIC (default) leaves out " +
		"includedPermissions, FULL returns them; a page of FULL roles holds at most 10 roles because a role can " +
		"list thousands of permissions"}
	roleFields = []capability.Field{
		{Name: "name", Description: "Full name of the role, roles/NAME, projects/PROJECT_ID/roles/ID, or " +
			"organizations/ID/roles/ID"},
		{Name: "title", Description: "Title of the role; untrusted text"},
		{Name: "description", Description: "Description of the role; untrusted text, cut"},
		{Name: "includedPermissions", Description: "Permissions the role grants; present only where Google " +
			"returns them (view FULL, and always for iam-roles.get), at most 1000 per role"},
		{Name: "includedPermissionsTotal", Description: "Number of permissions Google sent when " +
			"includedPermissions was cut; absent otherwise"},
		{Name: "stage", Description: "Launch stage: ALPHA, BETA, GA, DEPRECATED, DISABLED, or EAP; Google leaves " +
			"it out for ALPHA"},
		{Name: "etag", Description: "Entity tag of the role"},
		{Name: "deleted", Description: "True for a deleted role; absent otherwise"},
	}
)

var parentTypeArgument = capability.Argument{Name: "parent_type", Required: true, Description: "predefined for the " +
	"roles of IAM itself, project or organization for the custom roles defined there; an organization is read " +
	"only by a connection that names exactly this organization"}

// --- iam-roles.list ---

var iamRolesList = capability.Descriptor{
	ID: iamRolesListID, Version: 1, Title: "List Google Cloud IAM roles",
	Description: "List the predefined roles of IAM (parent_type predefined) or the custom roles defined in a " +
		"project or an organization, with view BASIC or FULL and optionally the deleted ones. A project is " +
		"bound to the targets of the connection by reading it and the folders above it before the roles are " +
		"read, and an organization only if a target names exactly it. Predefined roles belong to no target, so " +
		"a connection bound only to billing accounts does not run this tool. Roles and their descriptions are " +
		"untrusted third-party content; strings and permission lists are cut.",
	Tags: []string{"google", "cloud", "iam", "roles"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"parent_type":{"type":"string","enum":["predefined","project","organization"]},` +
		`"parent_id":{"type":"string","minLength":1,"maxLength":30,` +
		`"pattern":"^([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$"},` + viewProp + `,` +
		`"show_deleted":{"type":"boolean"},"page_size":{"type":"integer","minimum":1,"maximum":500},` + cursorProp +
		`},"required":["parent_type"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("roles", roleOutputSchema),
	Arguments: []capability.Argument{
		parentTypeArgument,
		{Name: "parent_id", Description: "Project ID or number, or the numeric organization ID; required for " +
			"project and organization, refused for predefined; never a projects/ name"},
		viewArgument,
		{Name: "show_deleted", Description: "Include deleted roles (Google's showDeleted)"},
		{Name: "page_size", Description: "Roles per page, 1 to 500 (1 to 10 with view FULL); default 100 " +
			"(10 with FULL)"},
		cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "roles", Description: "Roles of the page, each " + roleFieldText},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the custom roles of a project with their permissions",
		Arguments:   json.RawMessage(`{"parent_type":"project","parent_id":"my-project-123","view":"FULL"}`),
	}},
}

func handleIAMRolesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "list roles"
	var arguments iamArguments
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
	if !list.reachesResources() {
		return nil, badRequest(iamRefusal)
	}
	view, err := viewOf(arguments.View)
	if err != nil {
		return nil, err
	}
	pageSize, err := pageSizeOf(arguments.PageSize, rolesDefault(view), rolesPageLimit(view))
	if err != nil {
		return nil, err
	}
	var ref scopeRef
	switch arguments.ParentType {
	case typePredefined:
		if arguments.ParentID != "" {
			return nil, badRequest("parent_id must be left out for predefined roles")
		}
	case typeProject, typeOrg:
		ref = scopeRef{arguments.ParentType, arguments.ParentID}
		if arguments.ParentID == "" || !ref.valid() {
			return nil, badRequest("parent_id must be a project ID or number, or an organization ID of digits")
		}
		if err := ref.precheck(list); err != nil {
			return nil, err
		}
	default:
		return nil, badRequest("parent_type must be predefined, project, or organization")
	}
	binding := provider.CursorBinding(iamRolesListID, provider.TargetsOf(resolved), arguments.ParentType,
		arguments.ParentID, view, arguments.ShowDeleted)
	token, err := decodeCursor(binding, arguments.Cursor)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path, owner := "/v1/roles", ""
	if ref.typ != "" {
		if owner, err = ref.prove(ctx, c, list, op, iamRolesListID); err != nil {
			return nil, err
		}
		path = "/v1/" + escapedName(owner) + "/roles"
	}
	query := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	if view != "" {
		query.Set("view", view)
	}
	if arguments.ShowDeleted {
		query.Set("showDeleted", "true")
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	var members map[string]json.RawMessage
	if err := c.get(ctx, op, iamRolesListID, serviceIAM, path, query, &members); err != nil {
		return nil, err
	}
	rows, next, err := decodePage[roleWire](op, members, "roles", 2*pageSize)
	if err != nil {
		return nil, err
	}
	prefix := owner + "/roles/"
	if owner == "" {
		prefix = "roles/"
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		if !strings.HasPrefix(row.Name, prefix) || !roleNameOf(row.Name) {
			return nil, invalidRow(op)
		}
		out = append(out, row.result())
	}
	return pageResult("roles", out, binding, next), nil
}

func rolesDefault(view string) int {
	if view == "FULL" {
		return maxFullRolesPage
	}
	return defaultRolesPage
}

const iamRefusal = "IAM roles are read only by a connection without targets or with an organization, a folder, " +
	"or a project among them; a connection bound to billing accounts alone reaches none"

// roleNameOf reports whether a row names a role in one of the three forms Google documents.
func roleNameOf(name string) bool {
	return predefinedRoleName.MatchString(name) || projectRoleName.MatchString(name) ||
		organizationRole.MatchString(name)
}

// escapedName escapes every segment of a resource name such as projects/ID for a path.
func escapedName(name string) string {
	parts := strings.Split(name, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

// --- iam-roles.get ---

var iamRolesGet = capability.Descriptor{
	ID: iamRolesGetID, Version: 1, Title: "Get a Google Cloud IAM role",
	Description: "Read one role by its full name, with its permissions: a predefined role (roles/NAME), a custom " +
		"role of a project (projects/PROJECT_ID/roles/ID), or a custom role of an organization " +
		"(organizations/ID/roles/ID). A project is bound to the targets of the connection by reading it and " +
		"the folders above it, and an organization only if a target names exactly it. Predefined roles belong " +
		"to no target, so a connection bound only to billing accounts does not run this tool. The role is " +
		"untrusted third-party content; strings and the permission list are cut.",
	Tags: []string{"google", "cloud", "iam", "roles"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"role":{"type":"string","minLength":7,"maxLength":300,` +
		`"pattern":"^(roles/[A-Za-z0-9][A-Za-z0-9._-]{0,199}|` +
		`projects/([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])/roles/[A-Za-z0-9][A-Za-z0-9._-]{0,199}|` +
		`organizations/[0-9]{1,20}/roles/[A-Za-z0-9][A-Za-z0-9._-]{0,199})$"}},` +
		`"required":["role"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(roleOutputSchema),
	Arguments: []capability.Argument{
		{Name: "role", Required: true, Description: "Full role name such as roles/viewer, " +
			"projects/my-project-123/roles/myRole, or organizations/123456789/roles/myRole; no wildcard"},
	},
	Fields: roleFields,
	Examples: []capability.Example{{
		Description: "Read a predefined role", Arguments: json.RawMessage(`{"role":"roles/viewer"}`),
	}},
}

func handleIAMRolesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "get role"
	var arguments iamArguments
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
	if !list.reachesResources() {
		return nil, badRequest(iamRefusal)
	}
	var ref scopeRef
	var roleID string
	switch match := func(re *regexp.Regexp) []string { return re.FindStringSubmatch(arguments.Role) }; {
	case match(predefinedRoleName) != nil:
		roleID = match(predefinedRoleName)[1]
	case match(projectRoleName) != nil:
		m := match(projectRoleName)
		ref, roleID = scopeRef{typeProject, m[1]}, m[2]
	case match(organizationRole) != nil:
		m := match(organizationRole)
		ref, roleID = scopeRef{typeOrg, m[1]}, m[2]
	default:
		return nil, badRequest("role must be a full role name: roles/NAME, projects/PROJECT/roles/ID, or " +
			"organizations/ID/roles/ID")
	}
	if ref.typ != "" {
		if err := ref.precheck(list); err != nil {
			return nil, err
		}
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	want := "roles/" + roleID
	if ref.typ != "" {
		owner, err := ref.prove(ctx, c, list, op, iamRolesGetID)
		if err != nil {
			return nil, err
		}
		want = owner + "/roles/" + roleID
	}
	var role roleWire
	if err := c.get(ctx, op, iamRolesGetID, serviceIAM, "/v1/"+escapedName(want), nil, &role); err != nil {
		return nil, err
	}
	if role.Name != want {
		return nil, invalidRow(op)
	}
	return role.result(), nil
}
