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
	serviceAccountsListID = Provider + ".service-accounts.list"
	serviceAccountsGetID  = Provider + ".service-accounts.get"
)

// Bounds of a service account page. IAM v1 documents 100 as its maximum and 20 as its default.
const (
	maxServiceAccountsPage     = 100
	defaultServiceAccountsPage = 100
)

// derivedProject matches the address of a service account that Google creates in a project; the project ID
// is the label before iam.gserviceaccount.com. Default accounts (appspot, developer) are not derivable.
var derivedProject = regexp.MustCompile(`^[^@]+@([a-z][a-z0-9-]{4,28}[a-z0-9])\.iam\.gserviceaccount\.com$`)

// accountArguments are the arguments that name a project and, for the tools that read one account, an account.
type accountArguments struct {
	ProjectID string `json:"project_id"`
	Account   string `json:"account"`
}

// openAccountScope is the course every service account tool shares: the connection and the targets, the
// arguments, and only then the secret and the proof reading. It returns the project ID that owns the account
// as Google reports it. account is empty for a tool that lists the accounts of a project. An account address
// that names another project is refused before any I/O, and again after the project is known, where the
// project was given by number.
func openAccountScope(ctx context.Context, op, tool string, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, a accountArguments) (*client, string, error) {
	if err := checkConnection(resolved); err != nil {
		return nil, "", err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, "", failure(op, "the targets of the connection are not usable")
	}
	if !list.reachesResources() {
		return nil, "", badRequest(serviceAccountRefusal)
	}
	if !validProject(a.ProjectID) {
		return nil, "", badRequest("project_id must be a project ID or a project number")
	}
	if a.Account != "" && !serviceAccountPattern.MatchString(a.Account) {
		return nil, "", badRequest("account must be the email or the numeric unique ID of the service account")
	}
	if allowed, decided := list.decidesProject(a.ProjectID); decided && !allowed {
		return nil, "", refuseOutside("the project")
	}
	if !projectOwnsAccount(a.ProjectID, a.Account) {
		return nil, "", refuseOtherProject()
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, "", err
	}
	owner := a.ProjectID
	if allowed, decided := list.decidesProject(a.ProjectID); !decided || !allowed || !projectIDShape.MatchString(owner) {
		name, err := scopeRef{typeProject, a.ProjectID}.prove(ctx, c, list, op, tool)
		if err != nil {
			return nil, "", err
		}
		owner = strings.TrimPrefix(name, prefixProject)
	}
	if !projectOwnsAccount(owner, a.Account) {
		return nil, "", refuseOtherProject()
	}
	return c, owner, nil
}

const serviceAccountRefusal = "service accounts are read only by a connection without targets or with an " +
	"organization, a folder, or a project among them; a connection bound to billing accounts alone reaches none"

func refuseOtherProject() error {
	return badRequest("the service account belongs to another project than project_id")
}

// projectOwnsAccount is false only when the address of the account proves another project than project. A
// project given by number cannot be compared; the caller asks again with the project ID.
func projectOwnsAccount(project, account string) bool {
	m := derivedProject.FindStringSubmatch(account)
	return m == nil || !projectIDShape.MatchString(project) || m[1] == project
}

// accountPath is the request path of a service account below a project, with every segment escaped.
func accountPath(project, account string) string {
	return "/v1/projects/" + url.PathEscape(project) + "/serviceAccounts/" + url.PathEscape(account)
}

// serviceAccountRow is the ServiceAccount of IAM v1 as Google sends it. etag is deprecated and left out.
type serviceAccountRow struct {
	Name           string `json:"name"`
	ProjectID      string `json:"projectId"`
	UniqueID       string `json:"uniqueId"`
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	Description    string `json:"description"`
	OAuth2ClientID string `json:"oauth2ClientId"`
	Disabled       bool   `json:"disabled"`
}

type serviceAccountResult struct {
	Name           string `json:"name"`
	ProjectID      string `json:"projectId"`
	UniqueID       string `json:"uniqueId,omitempty"`
	Email          string `json:"email,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
	Description    string `json:"description,omitempty"`
	OAuth2ClientID string `json:"oauth2ClientId,omitempty"`
	Disabled       bool   `json:"disabled,omitempty"`
}

// check refuses a row that is not an account of project: Google never returns one from another project for a
// request below this one, so it is an invalid response and is not shown.
func (r serviceAccountRow) check(op, project string) error {
	if r.ProjectID != project || !strings.HasPrefix(r.Name, prefixProject+project+"/serviceAccounts/") ||
		len(r.Name) == len(prefixProject+project+"/serviceAccounts/") {
		return invalidRow(op)
	}
	return nil
}

func (r serviceAccountRow) result() serviceAccountResult {
	return serviceAccountResult{Name: boundedText(r.Name, maxNameText), ProjectID: boundedText(r.ProjectID, maxNameText),
		UniqueID: boundedText(r.UniqueID, maxNameText), Email: boundedText(r.Email, maxNameText),
		DisplayName: boundedText(r.DisplayName, maxNameText), Description: boundedText(r.Description, maxText),
		OAuth2ClientID: boundedText(r.OAuth2ClientID, maxNameText), Disabled: r.Disabled}
}

// --- shared descriptor parts ---

const (
	projectIDProp = `"project_id":{"type":"string","minLength":1,"maxLength":30,` +
		`"pattern":"^([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$"}`
	accountProp = `"account":{"type":"string","minLength":1,"maxLength":200,` +
		`"pattern":"^([0-9]{1,30}|[A-Za-z0-9][A-Za-z0-9._-]{0,63}@[a-z0-9][a-z0-9.-]{0,100}\\.gserviceaccount\\.com)$"}`

	serviceAccountSchema = `{"type":"object","properties":{"name":{"type":"string"},"projectId":{"type":"string"},` +
		`"uniqueId":{"type":"string"},"email":{"type":"string"},"displayName":{"type":"string"},` +
		`"description":{"type":"string"},"oauth2ClientId":{"type":"string"},"disabled":{"type":"boolean"}},` +
		`"required":["name","projectId"],"additionalProperties":false}`

	serviceAccountFieldText = "{name, projectId, uniqueId, email, displayName, description, oauth2ClientId, disabled}"
)

var (
	projectIDArgument = capability.Argument{Name: "project_id", Required: true, Description: "Project that owns " +
		"the service account: its ID or number, never a projects/ name. The project must lie in the targets of " +
		"the connection"}
	accountArgument = capability.Argument{Name: "account", Required: true, Description: "Service account: its " +
		"email (NAME@PROJECT_ID.iam.gserviceaccount.com) or its numeric unique ID; never a resource name or a " +
		"path. An email that names another project than project_id is refused"}
	serviceAccountFields = []capability.Field{
		{Name: "name", Description: "Resource name, projects/PROJECT_ID/serviceAccounts/EMAIL"},
		{Name: "projectId", Description: "ID of the project that owns the account"},
		{Name: "uniqueId", Description: "Stable numeric ID of the account"},
		{Name: "email", Description: "Email address of the account"},
		{Name: "displayName", Description: "Display name; untrusted text, cut"},
		{Name: "description", Description: "Description; untrusted text, cut"},
		{Name: "oauth2ClientId", Description: "OAuth 2.0 client ID of the account"},
		{Name: "disabled", Description: "True for a disabled account; absent otherwise"},
	}
)

// --- service-accounts.list ---

var serviceAccountsList = capability.Descriptor{
	ID: serviceAccountsListID, Version: 1, Title: "List Google Cloud service accounts",
	Description: "List the service accounts of one project: name, project, unique ID, email, display name, " +
		"description, OAuth 2.0 client ID, and whether the account is disabled. The project must lie in the " +
		"targets of the connection, which Qatlas proves before the accounts are read, and every account must " +
		"belong to that project. Names and descriptions are untrusted third-party text and are cut. The " +
		"deprecated etag is not returned. Keys of an account are read with service-account-keys.list.",
	Tags: []string{"google", "cloud", "iam", "service-accounts"}, Risk: iamRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,` + pageSizeProp + `,` +
		cursorProp + `},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("accounts", serviceAccountSchema),
	Arguments: []capability.Argument{
		projectIDArgument,
		{Name: "page_size", Description: "Accounts per page, 1 to 100; default 100"},
		cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "accounts", Description: "Service accounts of the page, each " + serviceAccountFieldText},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the service accounts of a project",
		Arguments:   json.RawMessage(`{"project_id":"example-project-123"}`),
	}},
}

func handleServiceAccountsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list service accounts"
	var arguments struct {
		accountArguments
		PageSize *int   `json:"page_size"`
		Cursor   string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	pageSize, err := pageSizeOf(arguments.PageSize, defaultServiceAccountsPage, maxServiceAccountsPage)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(serviceAccountsListID, provider.TargetsOf(resolved), arguments.ProjectID)
	token, err := decodeCursor(binding, arguments.Cursor)
	if err != nil {
		return nil, err
	}
	c, project, err := openAccountScope(ctx, op, serviceAccountsListID, resolved, secrets, red,
		accountArguments{ProjectID: arguments.ProjectID})
	if err != nil {
		return nil, err
	}
	query := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	if token != "" {
		query.Set("pageToken", token)
	}
	var members map[string]json.RawMessage
	if err := c.get(ctx, op, serviceAccountsListID, serviceIAM, "/v1/projects/"+url.PathEscape(project)+
		"/serviceAccounts", query, &members); err != nil {
		return nil, err
	}
	rows, next, err := decodePage[serviceAccountRow](op, members, "accounts", 2*pageSize)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		if err := row.check(op, project); err != nil {
			return nil, err
		}
		out = append(out, row.result())
	}
	return pageResult("accounts", out, binding, next), nil
}

// --- service-accounts.get ---

var serviceAccountsGet = capability.Descriptor{
	ID: serviceAccountsGetID, Version: 1, Title: "Get a Google Cloud service account",
	Description: "Read one service account of a project by its email or numeric unique ID. The project must lie " +
		"in the targets of the connection, which Qatlas proves before the account is read. An email that names " +
		"another project than project_id is refused before any request, and an answer for an account of " +
		"another project is not returned. The account may be the one the credential itself uses. Names and " +
		"descriptions are untrusted third-party text and are cut. The deprecated etag is not returned.",
	Tags: []string{"google", "cloud", "iam", "service-accounts"}, Risk: iamRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + projectIDProp + `,` + accountProp +
		`},"required":["project_id","account"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(serviceAccountSchema),
	Arguments:    []capability.Argument{projectIDArgument, accountArgument},
	Fields:       serviceAccountFields,
	Examples: []capability.Example{{
		Description: "Read a service account by its email",
		Arguments: json.RawMessage(`{"project_id":"example-project-123",` +
			`"account":"deploy@example-project-123.iam.gserviceaccount.com"}`),
	}},
}

func handleServiceAccountsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get service account"
	var arguments accountArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	c, project, err := openAccountScope(ctx, op, serviceAccountsGetID, resolved, secrets, red, arguments)
	if err != nil {
		return nil, err
	}
	var row serviceAccountRow
	if err := c.get(ctx, op, serviceAccountsGetID, serviceIAM, accountPath(project, arguments.Account), nil,
		&row); err != nil {
		return nil, err
	}
	if err := row.check(op, project); err != nil {
		return nil, err
	}
	if row.Email != arguments.Account && row.UniqueID != arguments.Account {
		return nil, invalidRow(op)
	}
	return row.result(), nil
}
