package googlemerchant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
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
	usersListID   = Provider + ".users.list"
	usersGetID    = Provider + ".users.get"
	usersUpdateID = Provider + ".users.update"
	usersDeleteID = Provider + ".users.delete"
)

// usersToolPrefix marks the tools that manage who may access an advanced account.
const usersToolPrefix = Provider + ".users."

// userSensitivity names the data of these tools: the e-mail addresses of the people with access, and what
// each of them may do.
const userSensitivity = "merchant-account-access-and-personal-data"

// meAlias is the documented spelling of the caller in the resource name of a user.
const meAlias = "me"

// maxUserPageSize is the documented maximum of users.list; Google sends 50 when it is left out.
const maxUserPageSize = 100

// accessRights are the documented values of User.accessRights, without the unused ACCESS_RIGHT_UNSPECIFIED.
var accessRights = []string{"STANDARD", "READ_ONLY", "ADMIN", "PERFORMANCE_REPORTING", "API_DEVELOPER"}

// A user is named by a plain ASCII e-mail address. The form is narrower than the address syntax on purpose:
// it has no character that could end or change the path segment, and no control or space character.
const (
	emailPattern = `[A-Za-z0-9._+'-]{1,64}@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+`
	maxEmail     = 254
)

// jsonEmailPattern is the pattern escaped for a JSON string.
var jsonEmailPattern = strings.ReplaceAll(emailPattern, `\`, `\\`)

var emailForm = regexp.MustCompile(`^` + emailPattern + `$`)

var (
	userArgSchema = `{"type":"string","minLength":1,"maxLength":254,"pattern":"^(` + meAlias + `|` + jsonEmailPattern + `)$",` +
		`"x-form":"the e-mail address of the user, or me for the caller"}`
	emailArgSchema = `{"type":"string","minLength":3,"maxLength":254,"pattern":"^` + jsonEmailPattern + `$",` +
		`"x-form":"the e-mail address of the user"}`
	accessRightsSchema = `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(len(accessRights)) +
		`,"uniqueItems":true,"items":{"type":"string","enum":` + jsonList(accessRights) + `}}`
	userPageSizeSchema = `{"type":"integer","minimum":1,"maximum":` + strconv.Itoa(maxUserPageSize) + `}`
)

func jsonList(items []string) string {
	out, _ := json.Marshal(items)
	return string(out)
}

var userReadRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true, DataSensitivity: userSensitivity,
}

func userChangeRisk(effect capability.Effect) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: userSensitivity}
}

// user is the User resource: the resource name holds the e-mail address, so a user is personal data.
type user struct {
	Name         nameText `json:"name,omitempty"`
	State        idText   `json:"state,omitempty"`
	AccessRights idList   `json:"accessRights,omitempty"`
}

type usersAnswer struct {
	Users         []user `json:"users"`
	NextPageToken string `json:"nextPageToken"`
}

type usersResult struct {
	Users      []user `json:"users"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// userAccepted is the result of a change: Google accepted the request, Qatlas shows nothing it did not send.
type userAccepted struct {
	Accepted bool     `json:"accepted"`
	Name     string   `json:"name"`
	Updated  []string `json:"updated,omitempty"`
}

var (
	userSchema = objectSchema(member("name", stringSchema), member("state", stringSchema),
		member("accessRights", listSchema))
	userAcceptedSchema = objectSchema(member("accepted", boolSchema), member("name", stringSchema),
		member("updated", listSchema))
)

const (
	userFieldsDescription = "name (accounts/ACCOUNT/users/EMAIL, which holds the e-mail address), state (PENDING or " +
		"VERIFIED), and accessRights (STANDARD, READ_ONLY, ADMIN, PERFORMANCE_REPORTING, API_DEVELOPER)"
	userNote = "Users are managed on the advanced account itself (or a standalone account): a connection bound to " +
		"targets needs advanced-accounts/ACCOUNT for it. E-mail addresses are personal data and, like every " +
		"answer of Google, untrusted content."
	userAdminNote  = "Google requires the role Admin for this method."
	userAcceptNote = " Only the acceptance of the request is reported: Google applies the change with a delay, so " +
		"read the user again after a while to see the result."
	userRolesNote = "access_rights replaces the whole list of roles of the user: a role that is left out is removed, " +
		"and the list is never merged with the current one. Allowed are STANDARD, READ_ONLY, ADMIN, " +
		"PERFORMANCE_REPORTING, and API_DEVELOPER, each at most once; API_DEVELOPER only works for an account with " +
		"a developer registration."
	userDeleteNote = "Removes the user from the account and so permanently withdraws its access; the user has to be " +
		"invited again outside Qatlas, which this provider cannot do. The last admin of an account cannot be " +
		"deleted. The address must be given: the alias me is refused so that the access of the credential cannot " +
		"be withdrawn by accident."
	userExclusionNote = "Qatlas does not invite users (users.create) and does not accept an invitation " +
		"(users.me.verifySelf)."
)

var (
	userFields   = []capability.Field{{Name: "name", Description: "accounts/ACCOUNT/users/EMAIL"}, {Name: "state", Description: "PENDING or VERIFIED"}, {Name: "accessRights", Description: "the roles of the user"}}
	acceptFields = []capability.Field{{Name: "accepted", Description: "true when Google accepted the request"},
		{Name: "name", Description: "the user resource of the request"},
		{Name: "updated", Description: "the members that the request named; only for an update"}}
	userArgument = capability.Argument{Name: "user", Required: true, Description: "e-mail address of the user, plain " +
		"ASCII without spaces, at most 254 characters"}
)

var usersList = capability.Descriptor{
	ID: usersListID, Version: 1, Title: "List the users of a Merchant Center account",
	Description: "List the users of an advanced or standalone Merchant Center account with their state and roles, a " +
		"batch per call with a cursor to read on. " + userNote + " page_size is 1 to 100; Google sends 50 when it is " +
		"left out.",
	Tags: []string{"google", "merchant", "users"}, Risk: userReadRisk, Provider: Provider,
	InputSchema: inputSchema(nil, member("account", accountArgSchema), member("page_size", userPageSizeSchema),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("users", userSchema)),
	Arguments: []capability.Argument{advancedArgument,
		{Name: "page_size", Description: "Users per batch, 1 to 100; Google sends 50 when left out"}, cursorArgument},
	Fields: []capability.Field{{Name: "users", Description: "Users with " + userFieldsDescription},
		{Name: "next_cursor", Description: "Cursor to read on with; missing at the end"}},
	Examples: []capability.Example{{Description: "First batch of users", Arguments: json.RawMessage(`{"account":"1000001"}`)}},
}

var usersGet = capability.Descriptor{
	ID: usersGetID, Version: 1, Title: "Get a user of a Merchant Center account",
	Description: "Read one user of a Merchant Center account: state and roles. user is the e-mail address, or me for " +
		"the user the credential acts as. " + userNote,
	Tags: []string{"google", "merchant", "users"}, Risk: userReadRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"user"}, member("account", accountArgSchema), member("user", userArgSchema)),
	OutputSchema: json.RawMessage(userSchema),
	Arguments:    []capability.Argument{advancedArgument, userArgument},
	Fields:       []capability.Field{{Name: "user", Description: userFieldsDescription}},
	Examples: []capability.Example{{Description: "One user",
		Arguments: json.RawMessage(`{"account":"1000001","user":"person@example.com"}`)}},
}

var usersUpdate = capability.Descriptor{
	ID: usersUpdateID, Version: 1, Title: "Replace the roles of a user of a Merchant Center account",
	Description: "Replace the roles of a user of a Merchant Center account. " + userRolesNote + " This changes who may " +
		"do what in the account; the user can lose rights it has (for example ADMIN). The address must be given: " +
		"the alias me is refused. " + userAdminNote + userAcceptNote + " " + userExclusionNote + " " + userNote +
		" Offered only by a connection whose tools list names it.",
	Tags: []string{"google", "merchant", "users", "update"}, Risk: userChangeRisk(capability.EffectUpdate),
	Provider: Provider, RequiresToolAllowList: true,
	InputSchema: inputSchema([]string{"user", "access_rights"}, member("account", accountArgSchema),
		member("user", emailArgSchema), member("access_rights", accessRightsSchema)),
	OutputSchema: json.RawMessage(userAcceptedSchema),
	Arguments: []capability.Argument{advancedArgument, userArgument,
		{Name: "access_rights", Required: true, Description: "the complete new list of roles, at least one of " +
			strings.Join(accessRights, ", ") + ", without duplicates; replaces the current list"}},
	Fields: acceptFields,
	Examples: []capability.Example{{Description: "Give a user read-only access",
		Arguments: json.RawMessage(`{"account":"1000001","user":"person@example.com","access_rights":["READ_ONLY"]}`)}},
}

var usersDelete = capability.Descriptor{
	ID: usersDeleteID, Version: 1, Title: "Remove a user from a Merchant Center account",
	Description: userDeleteNote + " " + userAdminNote + userAcceptNote + " " + userExclusionNote + " " + userNote +
		" Offered only by a connection whose tools list names it.",
	Tags: []string{"google", "merchant", "users", "delete"}, Risk: userChangeRisk(capability.EffectDelete),
	Provider: Provider, RequiresToolAllowList: true,
	InputSchema:  inputSchema([]string{"user"}, member("account", accountArgSchema), member("user", emailArgSchema)),
	OutputSchema: json.RawMessage(userAcceptedSchema),
	Arguments:    []capability.Argument{advancedArgument, userArgument}, Fields: acceptFields,
	Examples: []capability.Example{{Description: "Remove one user",
		Arguments: json.RawMessage(`{"account":"1000001","user":"person@example.com"}`)}},
}

type userArguments struct {
	User         string   `json:"user"`
	AccessRights []string `json:"access_rights"`
}

// userSegment checks the user argument and returns the path segment of it. The alias me is taken only when
// allowMe is set. No error quotes the value.
func userSegment(given string, allowMe bool) (string, error) {
	switch {
	case given == meAlias && allowMe:
		return meAlias, nil
	case given == meAlias:
		return "", invalidArgument("user must be the e-mail address of a user; the alias me is not accepted here")
	case len(given) > maxEmail || !emailForm.MatchString(given):
		return "", invalidArgument("user must be a plain ASCII e-mail address of at most 254 characters, " +
			"without spaces or other special characters")
	}
	return url.PathEscape(given), nil
}

// checkAccessRights refuses a list that is empty, longer than the documented set, has a value outside it,
// or names a value twice.
func checkAccessRights(rights []string) error {
	if len(rights) == 0 || len(rights) > len(accessRights) {
		return invalidArgument("access_rights needs between 1 and " + strconv.Itoa(len(accessRights)) + " roles")
	}
	for i, right := range rights {
		if !slices.Contains(accessRights, right) {
			return invalidArgument("access_rights holds a role that is not one of " + strings.Join(accessRights, ", "))
		}
		if slices.Contains(rights[:i], right) {
			return invalidArgument("access_rights must not name a role twice")
		}
	}
	return nil
}

func usersPath(id string) string { return accountPath(id) + "/users" }

// settleUsers settles the account against the binding of the advanced account, which needs no secret and no
// request, then opens the client. Every refusal comes before the first secret.
func settleUsers(ctx context.Context, c call, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (*client, string, error) {
	id, err := c.scope.advancedOf(c.args.Account)
	if err != nil {
		return nil, "", err
	}
	cl, err := open(ctx, resolved, secrets, red)
	return cl, id, err
}

func handleUsersList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("list users", usersListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if c.args.PageSize > maxUserPageSize {
		return nil, invalidArgument("page_size must be between 1 and " + strconv.Itoa(maxUserPageSize))
	}
	id, err := c.scope.advancedOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(c.tool, id, c.args.PageSize, c.scope.key())
	query := url.Values{}
	if c.args.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(c.args.PageSize))
	}
	if c.args.Cursor != "" {
		token, ok := provider.DecodeCursor(binding, c.args.Cursor, maxCursorSize)
		if !ok || token == "" {
			return nil, refusedCursor()
		}
		query.Set("pageToken", token)
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer usersAnswer
	if err := cl.get(ctx, c.op, c.tool, usersPath(id), query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Users) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := usersResult{Users: append([]user{}, answer.Users...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleUsersGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("get user", usersGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var args userArguments
	if json.Unmarshal(raw, &args) != nil {
		return nil, invalidArgument("the arguments could not be read")
	}
	segment, err := userSegment(args.User, true)
	if err != nil {
		return nil, err
	}
	cl, id, err := settleUsers(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out user
	if err := cl.get(ctx, c.op, c.tool, usersPath(id)+"/"+segment, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleUsersUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("update user", usersUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var args userArguments
	if json.Unmarshal(raw, &args) != nil {
		return nil, invalidArgument("the arguments could not be read")
	}
	segment, err := userSegment(args.User, false)
	if err != nil {
		return nil, err
	}
	if err := checkAccessRights(args.AccessRights); err != nil {
		return nil, err
	}
	cl, id, err := settleUsers(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := usersPath(id) + "/" + segment
	// The mask names the one member Google supports, so nothing else of the user is touched.
	payload, err := json.Marshal(user{AccessRights: args.AccessRights})
	if err != nil {
		return nil, provider.Fail(c.op, "the request body could not be encoded")
	}
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodPatch, path: path,
		query: url.Values{"updateMask": {"accessRights"}}, body: payload, change: true}, &struct{}{}); err != nil {
		return nil, err
	}
	return userAccepted{Accepted: true, Name: path[len(accountsAPI):], Updated: []string{"accessRights"}}, nil
}

func handleUsersDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	c, err := prepare("delete user", usersDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var args userArguments
	if json.Unmarshal(raw, &args) != nil {
		return nil, invalidArgument("the arguments could not be read")
	}
	segment, err := userSegment(args.User, false)
	if err != nil {
		return nil, err
	}
	cl, id, err := settleUsers(ctx, c, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := usersPath(id) + "/" + segment
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodDelete, path: path, change: true},
		&struct{}{}); err != nil {
		return nil, err
	}
	return userAccepted{Accepted: true, Name: path[len(accountsAPI):]}, nil
}

func usersAccountText(id string) string {
	if id == "" {
		return "of the connection"
	}
	return id
}

func usersUpdateSummary(raw json.RawMessage) string {
	var args struct {
		Account string `json:"account"`
		userArguments
	}
	_ = json.Unmarshal(raw, &args)
	return "Replace the whole list of roles of " + args.User + " in Merchant Center account " +
		usersAccountText(args.Account) + " with: " + strings.Join(args.AccessRights, ", ") + ". Every role that is " +
		"not named is removed, so the user can lose rights it has. Google applies the change with a delay."
}

func usersDeleteSummary(raw json.RawMessage) string {
	var args struct {
		Account string `json:"account"`
		userArguments
	}
	_ = json.Unmarshal(raw, &args)
	return "Remove " + args.User + " from Merchant Center account " + usersAccountText(args.Account) +
		". This permanently withdraws its access; it has to be invited again outside Qatlas. Google applies the " +
		"change with a delay."
}
