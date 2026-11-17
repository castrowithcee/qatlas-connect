package googletagmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	userPermissionsListID   = Provider + ".user-permissions.list"
	userPermissionsGetID    = Provider + ".user-permissions.get"
	userPermissionsUpdateID = Provider + ".user-permissions.update"
	userPermissionsDeleteID = Provider + ".user-permissions.delete"
)

// userPermissionsProfile holds the four tools. Their API methods accept only the scope that manages users,
// which also allows changes, so no tool of the group is in the read or the write profile.
const userPermissionsProfile = "user-permissions"

func userPermissionIDs() []string {
	return []string{userPermissionsListID, userPermissionsGetID, userPermissionsUpdateID, userPermissionsDeleteID}
}

const (
	userPermissionPattern   = `^accounts/` + idPattern + `/user_permissions/` + idPattern + `$`
	formUserPermission      = "accounts/ACCOUNT/user_permissions/USER_PERMISSION"
	maxUserPermissionPath   = 100
	userPermissionSchemaArg = `{"type":"string","maxLength":100,"pattern":"` + userPermissionPattern +
		`","x-form":"` + formUserPermission + `"}`
	userDataSensitivity = "tag-manager-user-permissions"
)

var userPermissionPathRE = regexp.MustCompile(userPermissionPattern)

const personalNote = "emailAddress is personal data: it is returned uncut, no error or log entry carries it, " +
	"and only the confirmation of an update shows the address given as previous_email_address. Names and " +
	"addresses are untrusted content of whoever edits the account. Offered only by a connection that is bound " +
	"to an account or not bound at all, because user rights belong to the account as a whole. "

const selfNote = "Removing or reducing the rights of the user of this very connection can lock it out of the " +
	"account; the tool cannot tell, because the answer does not name the user of the token. "

var userPermissionArgument = capability.Argument{Name: "user_permission", Required: true, Description: "User permission " +
	"as " + formUserPermission + ", from user-permissions.list; it must lie inside the account of the connection"}

var userPermissionFields = []capability.Field{
	{Name: "path", Description: formUserPermission}, {Name: "accountId", Description: "Account ID"},
	{Name: "emailAddress", Description: "Address of the user; personal data"},
	{Name: "accountAccess", Description: "permission: noAccess, user, or admin"},
	{Name: "containerAccess", Description: "containerId and permission (noAccess, read, edit, approve, or " +
		"publish) per container"},
}

var userRisk = func(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	r := changeRisk(effect, idempotency)
	r.DataSensitivity = userDataSensitivity
	return r
}

var userPermissionsReadRisk = capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone, OpenWorld: true, DataSensitivity: userDataSensitivity}

func userPermissionUpdateInput() json.RawMessage {
	access := func(values []string) string {
		return `{"type":"object","properties":{"permission":` + enumSchema(values) +
			`},"required":["permission"],"additionalProperties":false}`
	}
	containers := func(values []string) string {
		return `{"type":"array","maxItems":1000,"items":{"type":"object","properties":{"container":` +
			containerArgSchema + `,"permission":` + enumSchema(values) +
			`},"required":["container","permission"],"additionalProperties":false}}`
	}
	return inputSchema([]string{"user_permission", "accountAccess", "containerAccess", "previous_email_address",
		"previous_account_access", "previous_container_access"},
		member("user_permission", userPermissionSchemaArg), member("accountAccess", access(accountPermissions)),
		member("containerAccess", containers(containerPermissions)),
		member("previous_email_address", `{"type":"string","minLength":3,"maxLength":254}`),
		member("previous_account_access", access(append([]string{"accountPermissionUnspecified"}, accountPermissions...))),
		member("previous_container_access", containers(append([]string{"containerPermissionUnspecified"},
			containerPermissions...))))
}

const userPermissionUpdateNote = "previous_email_address, previous_account_access, and previous_container_access " +
	"are required: the address and the rights of the user as last read with user-permissions.get, in the form " +
	"of the new values. They are only compared and never written, and there is no argument that sets an " +
	"address. The confirmation shows the address, the account right before and after, and each container " +
	"before and after, an added or removed one included. Before it changes anything, the tool reads the user " +
	"and refuses the call when the address or the rights are not exactly what it holds now, so what the " +
	"confirmation shows is what is stored. "

var userPermissionsList = capability.Descriptor{
	ID: userPermissionsListID, Version: 1, Title: "List Tag Manager user permissions",
	Description: "List the users of one Google Tag Manager account with their account and container rights, a " +
		"batch per call with a cursor to read on. Needs the scope that manages users. " + personalNote,
	Tags: []string{"google", "tag-manager", "user-permissions"}, Risk: userPermissionsReadRisk, Provider: Provider,
	InputSchema:  inputSchema(nil, member("account", accountArgSchema), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("user_permissions", userPermissionSchema)),
	Arguments:    []capability.Argument{accountArgument, cursorArgument},
	Fields: []capability.Field{{Name: "user_permissions", Description: "User permissions with every member of " +
		"user-permissions.get"}, nextCursorField},
	Examples: []capability.Example{{Description: "Users of one account",
		Arguments: json.RawMessage(`{"account":"accounts/1000001"}`)}},
}

var userPermissionsGet = capability.Descriptor{
	ID: userPermissionsGetID, Version: 1, Title: "Get a Tag Manager user permission",
	Description: "Read the account and container rights of one user. Needs the scope that manages users. " +
		personalNote,
	Tags: []string{"google", "tag-manager", "user-permissions"}, Risk: userPermissionsReadRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"user_permission"}, member("user_permission", userPermissionSchemaArg)),
	OutputSchema: json.RawMessage(userPermissionSchema),
	Arguments:    []capability.Argument{userPermissionArgument}, Fields: userPermissionFields,
	Examples: []capability.Example{{Description: "One user",
		Arguments: json.RawMessage(`{"user_permission":"accounts/1000001/user_permissions/4000004"}`)}},
}

var userPermissionsUpdate = capability.Descriptor{
	ID: userPermissionsUpdateID, Version: 1, Title: "Update a Tag Manager user permission",
	Description: "Replace the account and container rights of an existing user as a whole, so send every " +
		"container the user keeps, as read with user-permissions.get, with the changes made. The user is " +
		"read first: a path that does not exist ends as not-found without a change, and the tool has no " +
		"argument for an address, so it never gives rights to a new person. Rights may be widened for the " +
		"existing user, also on containers not held so far. A container is a path of the same account. " +
		userPermissionUpdateNote + "Needs the scope that manages users. " + selfNote + personalNote,
	Tags: []string{"google", "tag-manager", "user-permissions", "update"}, Provider: Provider,
	Risk:         userRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema:  userPermissionUpdateInput(),
	OutputSchema: json.RawMessage(userPermissionSchema),
	Arguments: []capability.Argument{userPermissionArgument,
		{Name: "accountAccess", Required: true, Description: "Object with permission: noAccess, user, or admin"},
		{Name: "containerAccess", Required: true, Description: "List of objects with container (as " +
			"accounts/ACCOUNT/containers/CONTAINER, in the account of the user permission) and permission " +
			"(noAccess, read, edit, approve, or publish); an empty list leaves no container right"},
		{Name: "previous_email_address", Required: true, Description: "Address of the user as last read; compared, " +
			"never written"},
		{Name: "previous_account_access", Required: true, Description: "accountAccess as last read"},
		{Name: "previous_container_access", Required: true, Description: "containerAccess as last read, with " +
			"container as a path"}},
	Fields: userPermissionFields,
	Examples: []capability.Example{{Description: "Give an existing user edit rights on one container",
		Arguments: json.RawMessage(`{"user_permission":"accounts/1000001/user_permissions/4000004",` +
			`"accountAccess":{"permission":"user"},"containerAccess":[{"container":` +
			`"accounts/1000001/containers/2000002","permission":"edit"}],` +
			`"previous_email_address":"person@example.org","previous_account_access":{"permission":"user"},` +
			`"previous_container_access":[{"container":"accounts/1000001/containers/2000002","permission":"read"}]}`)}},
}

var userPermissionsDelete = capability.Descriptor{
	ID: userPermissionsDeleteID, Version: 1, Title: "Delete a Tag Manager user permission",
	Description: "Remove a user from the account, which revokes the access to it and to all its containers. " +
		"Needs the scope that manages users. " + selfNote + personalNote,
	Tags: []string{"google", "tag-manager", "user-permissions", "delete"}, Provider: Provider,
	Risk:         userRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema([]string{"user_permission"}, member("user_permission", userPermissionSchemaArg)),
	OutputSchema: json.RawMessage(objectSchema(member("user_permission", stringSchema), member("deleted", boolSchema))),
	Arguments:    []capability.Argument{userPermissionArgument},
	Fields: []capability.Field{{Name: "user_permission", Description: "The path that was deleted"},
		{Name: "deleted", Description: "True once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Remove one user",
		Arguments: json.RawMessage(`{"user_permission":"accounts/1000001/user_permissions/4000004"}`)}},
}

func userPermissionDescriptors() []capability.Descriptor {
	return []capability.Descriptor{userPermissionsList, userPermissionsGet, userPermissionsUpdate,
		userPermissionsDelete}
}

func isUserPermissionTool(id string) bool { return slices.Contains(userPermissionIDs(), id) }

// userPermissionOf settles the path of a user permission against the account of the connection.
func (s scope) userPermissionOf(given string) (gtmPath, error) {
	if len(given) > maxUserPermissionPath || !userPermissionPathRE.MatchString(given) {
		return gtmPath{}, invalidArgument("user_permission must be " + formUserPermission + " with decimal IDs")
	}
	p := gtmPath{account: strings.Split(given, "/")[1]}
	if !s.allows(p) {
		return gtmPath{}, invalidArgument("user_permission " + outsideTarget)
	}
	return p, nil
}

// requireAccountReach refuses a container-bound connection: user rights belong to the account as a whole.
func (c call) requireAccountReach() error {
	if c.scope.container != "" {
		return invalidArgument("user permissions belong to the account as a whole and are not offered to a " +
			"connection bound to a container")
	}
	return nil
}

// quiet drops Google's own words from a failure of this group, because a refusal may quote a user's address.
func quiet(err error) error {
	if failed, ok := err.(*provider.Error); ok && failed.Original != nil {
		failed.Original = &provider.Original{Status: failed.Original.Status}
	}
	return err
}

func handleUserPermissionsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list user permissions", userPermissionsListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountReach(); err != nil {
		return nil, err
	}
	parent, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(c.tool, parent.String(), c.scope.binding())
	query, err := pageQuery(binding, c.args.Cursor, nil)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer userPermissionsAnswer
	if err := cl.get(ctx, c.op, c.tool, parent.String()+"/user_permissions", query, &answer); err != nil {
		return nil, quiet(err)
	}
	if len(answer.UserPermission) > maxEntries {
		return nil, tooMany(c.op)
	}
	for _, u := range answer.UserPermission {
		if len(u.ContainerAccess) > maxEntries {
			return nil, tooMany(c.op)
		}
	}
	out := userPermissionsResult{UserPermissions: append([]userPermission{}, answer.UserPermission...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

// readUserPermission reads one user permission.
func (c call) readUserPermission(ctx context.Context, cl *client, path string) (userPermission, error) {
	var out userPermission
	if err := cl.get(ctx, c.op, c.tool, path, nil, &out); err != nil {
		return out, quiet(err)
	}
	if len(out.ContainerAccess) > maxEntries {
		return out, tooMany(c.op)
	}
	return out, nil
}

func handleUserPermissionsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get user permission", userPermissionsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountReach(); err != nil {
		return nil, err
	}
	_, err = c.scope.userPermissionOf(c.args.UserPermission)
	if err != nil {
		return nil, err
	}
	path := c.args.UserPermission
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return c.readUserPermission(ctx, cl, path)
}

// checkContainers checks the container paths of one list against the account of the user permission before
// any request and returns the rights by container ID.
func (c call) checkContainers(account gtmPath, list []containerRight) (map[string]string, error) {
	rights := map[string]string{}
	for _, item := range list {
		p, ok := parsePath(item.Container, containerPath)
		if !ok {
			return nil, invalidArgument("container must be accounts/ACCOUNT/containers/CONTAINER with decimal IDs")
		}
		if p.account != account.account || !c.scope.allows(p) {
			return nil, invalidArgument("a container of the container access does not belong to the account of " +
				"the user permission or lies outside the target of this connection")
		}
		if _, twice := rights[p.container]; twice {
			return nil, invalidArgument("a container access names a container twice")
		}
		rights[p.container] = item.Permission
	}
	return rights, nil
}

func handleUserPermissionsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update user permission", userPermissionsUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountReach(); err != nil {
		return nil, err
	}
	account, err := c.scope.userPermissionOf(c.args.UserPermission)
	if err != nil {
		return nil, err
	}
	path := c.args.UserPermission
	var a updateArguments
	if err := json.Unmarshal(raw, &a); err != nil || a.AccountAccess == nil || a.PreviousAccountAccess == nil ||
		a.PreviousEmailAddress == nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	next, err := c.checkContainers(account, a.ContainerAccess)
	if err != nil {
		return nil, err
	}
	previous, err := c.checkContainers(account, a.PreviousContainerAccess)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	before, address, err := c.readExisting(ctx, cl, path)
	if err != nil {
		return nil, err
	}
	if !sameRights(a, before, address, previous) {
		return nil, invalidArgument("previous_email_address, previous_account_access, or previous_container_access " +
			"is not what the user permission holds now; read it with user-permissions.get and send what it " +
			"returns as the previous_ arguments")
	}
	// The stored address goes back unchanged, so the request names the user the path already names.
	body := updateBody{EmailAddress: address, AccountAccess: *a.AccountAccess, ContainerAccess: []updateContainerPerm{}}
	for _, item := range a.ContainerAccess {
		id := strings.Split(item.Container, "/")[3]
		body.ContainerAccess = append(body.ContainerAccess, updateContainerPerm{ContainerID: id, Permission: next[id]})
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(c.op, "the user permission could not be encoded")
	}
	var out userPermission
	r := request{method: http.MethodPut, path: path, body: encoded, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, quiet(err)
	}
	return out, nil
}

// sameRights reports whether the previous_ arguments are exactly what the user permission holds.
func sameRights(a updateArguments, stored userPermission, address string, previous map[string]string) bool {
	if !strings.EqualFold(*a.PreviousEmailAddress, address) || stored.AccountAccess == nil ||
		string(stored.AccountAccess.Permission) != a.PreviousAccountAccess.Permission ||
		len(stored.ContainerAccess) != len(previous) {
		return false
	}
	for _, c := range stored.ContainerAccess {
		if p, ok := previous[string(c.ContainerID)]; !ok || p != string(c.Permission) {
			return false
		}
	}
	return true
}

// readExisting reads the entry an update would replace, with its address as stored. A missing entry ends the
// call as not-found, so no write can create one.
func (c call) readExisting(ctx context.Context, cl *client, path string) (userPermission, string, error) {
	var raw json.RawMessage
	if err := cl.get(ctx, c.op, c.tool, path, nil, &raw); err != nil {
		if failed, ok := err.(*provider.Error); ok && failed.Original != nil && failed.Original.Status == http.StatusNotFound {
			return userPermission{}, "", c.missing()
		}
		return userPermission{}, "", quiet(err)
	}
	var out userPermission
	var stored struct {
		EmailAddress string `json:"emailAddress"`
	}
	if json.Unmarshal(raw, &out) != nil || json.Unmarshal(raw, &stored) != nil {
		return out, "", provider.InvalidResponse(c.op, subject+" returned a user permission that could not be read")
	}
	if out.Path == "" && stored.EmailAddress == "" && out.AccountAccess == nil && len(out.ContainerAccess) == 0 {
		return out, "", c.missing()
	}
	if len(out.ContainerAccess) > maxEntries {
		return out, "", tooMany(c.op)
	}
	return out, stored.EmailAddress, nil
}

func (c call) missing() error {
	return &provider.Error{Class: provider.ClassNotFound, Op: c.op,
		Message: "the user permission does not exist; nothing was changed, and this tool never creates one"}
}

func handleUserPermissionsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("delete user permission", userPermissionsDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if err := c.requireAccountReach(); err != nil {
		return nil, err
	}
	_, err = c.scope.userPermissionOf(c.args.UserPermission)
	if err != nil {
		return nil, err
	}
	path := c.args.UserPermission
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodDelete, path: path, change: true}, nil); err != nil {
		return nil, quiet(err)
	}
	return deleteUserPermissionResult{UserPermission: path, Deleted: true}, nil
}

// userPermissionUpdateSummary phrases the change from the arguments alone: the address and the rights before
// and after come from the previous_ arguments, which the handler compares with the stored entry.
func userPermissionUpdateSummary(raw json.RawMessage) string {
	var a struct {
		UserPermission string `json:"user_permission"`
		updateArguments
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Replace the rights of user permission " + a.UserPermission + " (" + a.summaryOf() +
		"; the call is refused if the stored rights differ from the previous values)"
}

func userPermissionDeleteSummary(raw json.RawMessage) string {
	var a toolArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Remove user permission " + a.UserPermission + ": the user loses all access to the account and its containers"
}
