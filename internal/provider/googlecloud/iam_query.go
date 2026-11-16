package googlecloud

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The two queries are POST methods of IAM that only read: they send one JSON body and change nothing, so the
// tools are safe reads that may be repeated. The body is built here from checked arguments, never taken from
// the caller.

// resourceArgs reads the structured resource of a query. type and ID come from a fixed list and a fixed
// pattern, and the full resource name is built from the resource Qatlas proved, never from text.
func resourceArgs(arguments iamArguments) (scopeRef, error) {
	ref := scopeRef{arguments.ResourceType, arguments.ResourceID}
	if (ref.typ != typeProject && ref.typ != typeFolder && ref.typ != typeOrg) || !ref.valid() {
		return scopeRef{}, badRequest("resource_type must be project, folder, or organization, and resource_id " +
			"a project ID or number, or the digits of a folder or organization ID")
	}
	return ref, nil
}

var (
	resourceTypeArgument = capability.Argument{Name: "resource_type", Required: true, Description: "project, " +
		"folder, or organization; Qatlas builds the full resource name //cloudresourcemanager.googleapis.com/... " +
		"from it and resource_id. Resources of other services have no type here"}
	resourceIDArgument = capability.Argument{Name: "resource_id", Required: true, Description: "Project ID or " +
		"number, or the digits of the folder or organization ID; never a name or a path"}
)

const resourceProps = `"resource_type":{"type":"string","enum":["project","folder","organization"]},` +
	`"resource_id":{"type":"string","minLength":1,"maxLength":30,` +
	`"pattern":"^([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$"}`

// queryRun is the common course of the two queries.
func queryRun[T any](ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage, op, tool, path, list string, view bool, def, limit int,
	row func(ref scopeRef, owner string, list allowlist, r T) (any, error)) (any, error) {
	var arguments iamArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkConnection(resolved); err != nil {
		return nil, err
	}
	targets, err := allowlistOf(resolved)
	if err != nil {
		return nil, failure(op, "the targets of the connection are not usable")
	}
	if !targets.reachesResources() {
		return nil, badRequest(iamRefusal)
	}
	ref, err := resourceArgs(arguments)
	if err != nil {
		return nil, err
	}
	pageView := ""
	if view {
		if pageView, err = viewOf(arguments.View); err != nil {
			return nil, err
		}
		if pageView == "FULL" {
			def, limit = maxFullRolesPage, maxFullRolesPage
		}
	}
	pageSize, err := pageSizeOf(arguments.PageSize, def, limit)
	if err != nil {
		return nil, err
	}
	if err := ref.precheck(targets); err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(tool, provider.TargetsOf(resolved), ref.typ, ref.id, pageView)
	token, err := decodeCursor(binding, arguments.Cursor)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	owner, err := ref.prove(ctx, c, targets, op, tool)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"fullResourceName": resourceManagerName + owner, "pageSize": pageSize}
	if pageView != "" {
		body["view"] = pageView
	}
	if token != "" {
		body["pageToken"] = token
	}
	var members map[string]json.RawMessage
	if err := c.post(ctx, op, tool, serviceIAM, path, body, &members); err != nil {
		return nil, err
	}
	rows, next, err := decodePage[T](op, members, list, 2*pageSize)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		shaped, err := row(scopeRef{ref.typ, strings.TrimPrefix(owner, ref.typ+"s/")}, owner, targets, r)
		if err != nil {
			return nil, err
		}
		out = append(out, shaped)
	}
	return pageResult(list, out, binding, next), nil
}

// --- iam-roles.query-grantable ---

var iamRolesQuery = capability.Descriptor{
	ID: iamRolesQueryID, Version: 1, Title: "List the roles grantable on a Google Cloud resource",
	Description: "List the roles that can be granted on a project, a folder, or an organization: those whose " +
		"bindings the IAM policy of the resource can hold, which are the predefined roles and the custom roles " +
		"of the resource and of its organization. It is a read although Google sends it as POST. The resource " +
		"is named by type and ID, bound to the targets of the connection by reading it and the folders above " +
		"it, and an organization only if a target names exactly it. The custom roles of an organization that " +
		"the targets do not name are returned by name only. Roles are untrusted third-party content; strings " +
		"and permission lists are cut.",
	Tags: []string{"google", "cloud", "iam", "roles"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + resourceProps + `,` + viewProp + `,` +
		`"page_size":{"type":"integer","minimum":1,"maximum":500},` + cursorProp + `},` +
		`"required":["resource_type","resource_id"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("roles", roleOutputSchema),
	Arguments: []capability.Argument{
		resourceTypeArgument, resourceIDArgument, viewArgument,
		{Name: "page_size", Description: "Roles per page, 1 to 500 (10 with view FULL); default 100 (10 with FULL)"},
		cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "roles", Description: "Roles of the page, each " + roleFieldText + "; an organization role the " +
			"targets do not name carries its name only"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the roles grantable on a project",
		Arguments:   json.RawMessage(`{"resource_type":"project","resource_id":"my-project-123"}`),
	}},
}

func handleIAMRolesQuery(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "query grantable roles"
	return queryRun(ctx, resolved, secrets, red, raw, op, iamRolesQueryID, "/v1/roles:queryGrantableRoles", "roles",
		true, defaultRolesPage, maxRolesPage,
		func(ref scopeRef, owner string, targets allowlist, r roleWire) (any, error) {
			switch {
			case predefinedRoleName.MatchString(r.Name):
				return r.result(), nil
			case projectRoleName.MatchString(r.Name):
				// A project holds its own custom roles; a role of any other project is not an answer to this query.
				if ref.typ == typeProject && strings.HasPrefix(r.Name, owner+"/roles/") {
					return r.result(), nil
				}
			case organizationRole.MatchString(r.Name):
				id := organizationRole.FindStringSubmatch(r.Name)[1]
				if targets.has(kindOrganization, id) {
					return r.result(), nil
				}
				// The organization is not named by the targets, so only the identifier goes out.
				return roleResult{Name: boundedText(r.Name, maxNameText)}, nil
			}
			return nil, invalidRow(op)
		})
}

// --- iam-permissions.query-testable ---

var iamPermissionsQuery = capability.Descriptor{
	ID: iamPermissionsQueryID, Version: 1, Title: "List the permissions testable on a Google Cloud resource",
	Description: "List the permissions that can be tested on a project, a folder, or an organization: the ones " +
		"for which a principal's access to the resource can be checked. It is a read although Google sends it " +
		"as POST, and it tests nothing itself. The resource is named by type and ID, bound to the targets of the " +
		"connection by reading it and the folders above it, and an organization only if a target names exactly " +
		"it. Permissions are untrusted third-party content; strings are cut.",
	Tags: []string{"google", "cloud", "iam", "permissions"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + resourceProps + `,` +
		`"page_size":{"type":"integer","minimum":1,"maximum":500},` + cursorProp + `},` +
		`"required":["resource_type","resource_id"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("permissions", `{"type":"object","properties":{"name":{"type":"string"},`+
		`"title":{"type":"string"},"description":{"type":"string"},"stage":{"type":"string"},`+
		`"customRolesSupportLevel":{"type":"string"},"apiDisabled":{"type":"boolean"},`+
		`"primaryPermission":{"type":"string"}},"required":["name"],"additionalProperties":false}`),
	Arguments: []capability.Argument{
		resourceTypeArgument, resourceIDArgument,
		{Name: "page_size", Description: "Permissions per page, 1 to 500; default 100"},
		cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "permissions", Description: "Permissions of the page, each {name, title, description, stage, " +
			"customRolesSupportLevel, apiDisabled, primaryPermission}; apiDisabled is absent unless true, and " +
			"primaryPermission names the preferred permission when this one is an alias"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the permissions testable on a folder",
		Arguments:   json.RawMessage(`{"resource_type":"folder","resource_id":"2001","page_size":200}`),
	}},
}

func handleIAMPermissionsQuery(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "query testable permissions"
	return queryRun(ctx, resolved, secrets, red, raw, op, iamPermissionsQueryID,
		"/v1/permissions:queryTestablePermissions", "permissions", false, defaultPermissionsPage, maxPermissionsPage,
		func(_ scopeRef, _ string, _ allowlist, p permissionWire) (any, error) {
			if !permissionName.MatchString(p.Name) {
				return nil, invalidRow(op)
			}
			return p.result(), nil
		})
}
