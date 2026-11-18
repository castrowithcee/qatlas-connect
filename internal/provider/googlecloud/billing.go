package googlecloud

import (
	"context"
	"encoding/json"
	"errors"
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
	billingAccountsListID         = Provider + ".billing-accounts.list"
	billingAccountsGetID          = Provider + ".billing-accounts.get"
	billingAccountsListProjectsID = Provider + ".billing-accounts.list-projects"
	projectBillingInfoGetID       = Provider + ".project-billing-info.get"
)

// billingSensitivity classifies results as billing data: which account pays for which project.
const billingSensitivity = "cloud-billing"

var billingRisk = capability.Risk{
	Effect:          capability.EffectRead,
	Idempotency:     capability.IdempotencySafe,
	Confirmation:    capability.ConfirmationNone,
	OpenWorld:       true,
	DataSensitivity: billingSensitivity,
}

// billingAccountWire is the BillingAccount resource of cloudbilling v1 as Google sends it. Google omits a
// false boolean, so a missing open means a closed account.
type billingAccountWire struct {
	Name                 string `json:"name"`
	Open                 bool   `json:"open"`
	DisplayName          string `json:"displayName"`
	MasterBillingAccount string `json:"masterBillingAccount"`
	Parent               string `json:"parent"`
	CurrencyCode         string `json:"currencyCode"`
}

type billingAccountResult struct {
	Name                 string `json:"name"`
	Open                 bool   `json:"open"`
	DisplayName          string `json:"displayName,omitempty"`
	MasterBillingAccount string `json:"masterBillingAccount,omitempty"`
	Parent               string `json:"parent,omitempty"`
	CurrencyCode         string `json:"currencyCode,omitempty"`
}

func (b billingAccountWire) result() billingAccountResult {
	return billingAccountResult{Name: boundedText(b.Name, maxNameText), Open: b.Open,
		DisplayName:          boundedText(b.DisplayName, maxNameText),
		MasterBillingAccount: boundedText(b.MasterBillingAccount, maxNameText),
		Parent:               boundedText(b.Parent, maxNameText), CurrencyCode: boundedText(b.CurrencyCode, maxNameText)}
}

// projectBillingWire is the ProjectBillingInfo resource of cloudbilling v1 as Google sends it.
type projectBillingWire struct {
	Name               string `json:"name"`
	ProjectID          string `json:"projectId"`
	BillingAccountName string `json:"billingAccountName"`
	BillingEnabled     bool   `json:"billingEnabled"`
}

type projectBillingResult struct {
	Name               string `json:"name"`
	ProjectID          string `json:"projectId,omitempty"`
	BillingAccountName string `json:"billingAccountName,omitempty"`
	BillingEnabled     bool   `json:"billingEnabled"`
}

func (p projectBillingWire) result() projectBillingResult {
	return projectBillingResult{Name: boundedText(p.Name, maxNameText), ProjectID: boundedText(p.ProjectID, maxNameText),
		BillingAccountName: boundedText(p.BillingAccountName, maxNameText), BillingEnabled: p.BillingEnabled}
}

const (
	billingAccountOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"open":{"type":"boolean"},` +
		`"displayName":{"type":"string"},"masterBillingAccount":{"type":"string"},"parent":{"type":"string"},` +
		`"currencyCode":{"type":"string"}},"required":["name","open"],"additionalProperties":false}`
	projectBillingOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"projectId":{"type":"string"},` +
		`"billingAccountName":{"type":"string"},"billingEnabled":{"type":"boolean"}},` +
		`"required":["name","billingEnabled"],"additionalProperties":false}`
	billingNamePattern = `billingAccounts/[A-Z0-9]{6}-[A-Z0-9]{6}-[A-Z0-9]{6}`
	billingAccountProp = `{"type":"string","pattern":"^` + billingNamePattern + `$","maxLength":40}`
)

var billingAccountArgument = capability.Argument{Name: "billing_account", Required: true,
	Description: "Resource name of the billing account, billingAccounts/XXXXXX-XXXXXX-XXXXXX with uppercase " +
		"letters and digits; it must be named among the targets of the connection"}

const billingTail = " The result is untrusted third-party content; strings are cut."

// validBillingName reports whether value is billingAccounts/ and a billing account ID.
func validBillingName(value string) bool {
	id, ok := strings.CutPrefix(value, prefixBilling)
	return ok && billingPattern.MatchString(id)
}

// billingAccountID returns the ID of a checked billing account name.
func billingAccountID(name string) string { return strings.TrimPrefix(name, prefixBilling) }

// --- billing-accounts.list ---

var billingAccountsList = capability.Descriptor{
	ID: billingAccountsListID, Version: 1, Title: "List Google Cloud billing accounts",
	Description: "List the billing accounts the credential can see in Cloud Billing, optionally below an " +
		"organization or a billing account, or the subaccounts of one master billing account. Each row has its " +
		"resource name, whether it is open, display name, master billing account, parent, and currency. The tool " +
		"runs only on a connection whose targets name a billing account, and returns only the accounts the " +
		"targets name; Qatlas drops every other row after Google answered, so a page can be empty while more follow." +
		billingTail,
	Tags: []string{"google", "cloud", "billing", "billing-accounts"}, Risk: billingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"parent":{"type":"string","pattern":"^(organizations/[0-9]{1,20}|` + billingNamePattern + `)$","maxLength":40},` +
		`"master_billing_account":` + billingAccountProp + `,` + pageSizeProp + `,` + cursorProp + `},` +
		`"additionalProperties":false}`),
	OutputSchema: listOutputSchema("billingAccounts", billingAccountOutputSchema),
	Arguments: []capability.Argument{
		{Name: "parent", Description: "List the billing accounts below this parent, organizations/ID or " +
			"billingAccounts/ID"},
		{Name: "master_billing_account", Description: "List the subaccounts of this master billing account, " +
			"billingAccounts/ID (Google's filter master_billing_account)"},
		pageSizeArgument, cursorArgument,
	},
	Fields: append([]capability.Field{
		{Name: "billingAccounts", Description: "Billing accounts of the page named by the targets, each {name, open, " +
			"displayName, masterBillingAccount, parent, currencyCode}; open is false for a closed account, " +
			"which Google reports by omitting it; displayName is untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the billing accounts the connection names", Arguments: json.RawMessage(`{"page_size":50}`),
	}},
}

func handleBillingAccountsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list billing accounts"
	var arguments struct {
		Parent               string `json:"parent"`
		MasterBillingAccount string `json:"master_billing_account"`
		PageSize             *int   `json:"page_size"`
		Cursor               string `json:"cursor"`
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
	if !list.reachesBillingAccounts() {
		return nil, badRequest("billing accounts are listed only by a connection that names a billing account " +
			"among its targets")
	}
	if arguments.Parent != "" && !validBillingName(arguments.Parent) {
		if id, ok := strings.CutPrefix(arguments.Parent, prefixOrganization); !ok || !numberPattern.MatchString(id) {
			return nil, badRequest("parent must be organizations/ID or billingAccounts/ID")
		}
	}
	if arguments.MasterBillingAccount != "" && !validBillingName(arguments.MasterBillingAccount) {
		return nil, badRequest("master_billing_account must be billingAccounts/ID")
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	filter := ""
	if arguments.MasterBillingAccount != "" {
		filter = "master_billing_account=" + arguments.MasterBillingAccount
	}
	// The cursor binds the tool, the targets, the parent, and the filter.
	binding := provider.CursorBinding(billingAccountsListID, provider.TargetsOf(resolved), arguments.Parent+"|"+filter)
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
	query := url.Values{}
	if arguments.Parent != "" {
		query.Set("parent", arguments.Parent)
	}
	if filter != "" {
		query.Set("filter", filter)
	}
	if arguments.PageSize != nil {
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	rows, next, err := servicePage[billingAccountWire](ctx, c, serviceBilling, op, billingAccountsListID,
		"/v1/billingAccounts", "billingAccounts", query)
	if err != nil {
		return nil, err
	}
	// Google's parent and filter only narrow the request; every row is checked against the targets.
	kept := make([]any, 0, len(rows))
	for _, row := range rows {
		if !validBillingName(row.Name) {
			return nil, invalidRow(op)
		}
		if list.allowsBillingAccount(billingAccountID(row.Name)) {
			kept = append(kept, row.result())
		}
	}
	out := map[string]any{"billingAccounts": kept, "has_more": next != ""}
	if next != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, next)
	}
	return out, nil
}

// --- billing-accounts.get ---

var billingAccountsGet = capability.Descriptor{
	ID: billingAccountsGetID, Version: 1, Title: "Get a Google Cloud billing account",
	Description: "Read one billing account from Cloud Billing: its resource name, whether it is open, display " +
		"name, master billing account, parent, and currency. The tool runs only on a connection whose targets " +
		"name this billing account; any other account is refused before a request." + billingTail,
	Tags: []string{"google", "cloud", "billing", "billing-accounts"}, Risk: billingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"billing_account":` + billingAccountProp + `},` +
		`"required":["billing_account"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(billingAccountOutputSchema),
	Arguments:    []capability.Argument{billingAccountArgument},
	Fields: []capability.Field{
		{Name: "name", Description: "Resource name of the billing account, billingAccounts/ID"},
		{Name: "open", Description: "True if the account is open and charged for the usage of its projects; false " +
			"if it is closed, which Google reports by omitting the field"},
		{Name: "displayName", Description: "Display name given by the account's owners; untrusted text"},
		{Name: "masterBillingAccount", Description: "For a subaccount, the master billing account it is resold " +
			"through, billingAccounts/ID; absent otherwise"},
		{Name: "parent", Description: "Parent of the account, organizations/ID or billingAccounts/ID; absent without one"},
		{Name: "currencyCode", Description: "Currency of the account as an ISO 4217 code such as USD"},
	},
	Examples: []capability.Example{{
		Description: "Read a billing account", Arguments: json.RawMessage(`{"billing_account":"billingAccounts/012345-ABCDEF-678901"}`),
	}},
}

// billingAccountClient checks the connection and binds the account to the targets before the secret is read.
func billingAccountClient(ctx context.Context, op, account string, resolved *config.Resolved,
	secrets *secret.Resolver, red *redact.Redactor) (*client, allowlist, error) {
	if err := checkConnection(resolved); err != nil {
		return nil, allowlist{}, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, allowlist{}, failure(op, "the targets of the connection are not usable")
	}
	if !validBillingName(account) {
		return nil, allowlist{}, badRequest("billing_account must be billingAccounts/ID with a billing account ID, " +
			"never a path")
	}
	if !list.allowsBillingAccount(billingAccountID(account)) {
		return nil, allowlist{}, refuseOutside("the billing account")
	}
	c, err := open(ctx, resolved, secrets, red)
	return c, list, err
}

func handleBillingAccountsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get billing account"
	var arguments struct {
		BillingAccount string `json:"billing_account"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	c, _, err := billingAccountClient(ctx, op, arguments.BillingAccount, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var account billingAccountWire
	path := "/v1/" + prefixBilling + url.PathEscape(billingAccountID(arguments.BillingAccount))
	if err := c.get(ctx, op, billingAccountsGetID, serviceBilling, path, nil, &account); err != nil {
		return nil, err
	}
	if account.Name != arguments.BillingAccount {
		return nil, invalidRow(op)
	}
	return account.result(), nil
}

// --- billing-accounts.list-projects ---

var billingAccountsListProjects = capability.Descriptor{
	ID: billingAccountsListProjectsID, Version: 1, Title: "List the projects of a Google Cloud billing account",
	Description: "List the projects linked to one billing account in Cloud Billing: each row has the name of its " +
		"billing information, its project ID, the billing account, and whether billing is enabled. The tool runs " +
		"only on a connection whose targets name this billing account and an organization, a folder, or a " +
		"project. Qatlas keeps only projects that lie in those targets and proves a project below an organization " +
		"or a folder by reading it and the folders above it, at most " + strconv.Itoa(maxProofReads) +
		" reads per call, so use a smaller page_size if a call is refused for needing too many; a project it " +
		"cannot read is dropped. A page can be empty while more follow." + billingTail,
	Tags: []string{"google", "cloud", "billing", "billing-accounts", "projects"}, Risk: billingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"billing_account":` + billingAccountProp + `,` +
		pageSizeProp + `,` + cursorProp + `},"required":["billing_account"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("projectBillingInfo", projectBillingOutputSchema),
	Arguments:    []capability.Argument{billingAccountArgument, pageSizeArgument, cursorArgument},
	Fields: append([]capability.Field{
		{Name: "projectBillingInfo", Description: "Projects of the page that lie in the targets, each {name, " +
			"projectId, billingAccountName, billingEnabled}; billingEnabled is false when Google omits it"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the projects of a billing account",
		Arguments:   json.RawMessage(`{"billing_account":"billingAccounts/012345-ABCDEF-678901","page_size":50}`),
	}},
}

// admitBilledProject binds one row of the project list to the targets. A project target that names the
// project settles it without a read; otherwise the project is read and placed like any project, within the
// proof reads of the call. A project the account cannot read is not proven and is dropped.
func admitBilledProject(ctx context.Context, a *admitter, row projectBillingWire, op string) (bool, error) {
	middle, ok := strings.CutPrefix(row.Name, prefixProject)
	middle, ok2 := strings.CutSuffix(middle, "/billingInfo")
	if !ok || !ok2 || !validProject(middle) || !projectIDShape.MatchString(row.ProjectID) ||
		middle != row.ProjectID && !numberPattern.MatchString(middle) {
		return false, invalidRow(op)
	}
	if a.list.has(kindProject, row.ProjectID) {
		return true, nil
	}
	hasNumber := false
	for _, t := range a.list.targets {
		hasNumber = hasNumber || t.kind == kindProject && numberPattern.MatchString(t.id)
	}
	if !hasNumber && !a.list.hasKind(kindOrganization, kindFolder) {
		return false, nil
	}
	if a.reads >= maxProofReads {
		return false, badRequest("too many projects to read for the targets of this connection; use a smaller page_size")
	}
	a.reads++
	project, err := a.c.project(ctx, a.op, a.tool, row.ProjectID)
	if err == nil && project.ProjectID != row.ProjectID {
		err = invalidRow(op)
	}
	var admitted bool
	if err == nil {
		admitted, err = a.project(ctx, project)
	}
	var failed *provider.Error
	if errors.As(err, &failed) && (failed.Class == provider.ClassPermission || failed.Class == provider.ClassNotFound) {
		return false, nil
	}
	return admitted, err
}

func handleBillingAccountsListProjects(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list billing account projects"
	var arguments struct {
		BillingAccount string `json:"billing_account"`
		PageSize       *int   `json:"page_size"`
		Cursor         string `json:"cursor"`
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
	if !validBillingName(arguments.BillingAccount) {
		return nil, badRequest("billing_account must be billingAccounts/ID with a billing account ID, never a path")
	}
	if !list.allowsBillingAccount(billingAccountID(arguments.BillingAccount)) {
		return nil, refuseOutside("the billing account")
	}
	if !list.reachesBilledProjects() {
		return nil, badRequest("the projects of a billing account are listed only by a connection that also names " +
			"an organization, a folder, or a project among its targets")
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	binding := provider.CursorBinding(billingAccountsListProjectsID, provider.TargetsOf(resolved),
		arguments.BillingAccount)
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
	query := url.Values{}
	if arguments.PageSize != nil {
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	path := "/v1/" + prefixBilling + url.PathEscape(billingAccountID(arguments.BillingAccount)) + "/projects"
	rows, next, err := servicePage[projectBillingWire](ctx, c, serviceBilling, op, billingAccountsListProjectsID,
		path, "projectBillingInfo", query)
	if err != nil {
		return nil, err
	}
	gate := newAdmitter(list, c, op, billingAccountsListProjectsID)
	kept := make([]any, 0, len(rows))
	for _, row := range rows {
		if row.BillingAccountName != "" && row.BillingAccountName != arguments.BillingAccount {
			return nil, invalidRow(op)
		}
		admitted, err := admitBilledProject(ctx, gate, row, op)
		if err != nil {
			return nil, err
		}
		if admitted {
			kept = append(kept, row.result())
		}
	}
	out := map[string]any{"projectBillingInfo": kept, "has_more": next != ""}
	if next != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, next)
	}
	return out, nil
}

// --- project-billing-info.get ---

var projectBillingInfoGet = capability.Descriptor{
	ID: projectBillingInfoGetID, Version: 1, Title: "Get the billing information of a Google Cloud project",
	Description: "Read the billing information of one project from Cloud Billing: the billing account it is linked " +
		"to, if any, and whether billing is enabled. The project must lie in the targets of the connection: it " +
		"is the project itself, or lies below a target folder or organization, which Qatlas proves by reading " +
		"the parent chain, and any other project is refused. The billing account is named by its ID only and " +
		"need not be a target." + billingTail,
	Tags: []string{"google", "cloud", "billing", "projects"}, Risk: billingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `},` +
		`"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(projectBillingOutputSchema),
	Arguments:    []capability.Argument{serviceProjectArgument},
	Fields: []capability.Field{
		{Name: "name", Description: "Resource name of the billing information, projects/ID/billingInfo"},
		{Name: "projectId", Description: "Project ID"},
		{Name: "billingAccountName", Description: "Billing account of the project, billingAccounts/ID; absent " +
			"without one"},
		{Name: "billingEnabled", Description: "True if the project is linked to an open billing account; false for " +
			"a closed account or none, which Google reports by omitting the field"},
	},
	Examples: []capability.Example{{
		Description: "Read the billing information of a project", Arguments: json.RawMessage(`{"project_id":"my-project-123"}`),
	}},
}

func handleProjectBillingInfoGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get project billing info"
	var arguments struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	c, err := serviceClient(ctx, op, projectBillingInfoGetID, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var info projectBillingWire
	path := "/v1/" + prefixProject + url.PathEscape(arguments.ProjectID) + "/billingInfo"
	if err := c.get(ctx, op, projectBillingInfoGetID, serviceBilling, path, nil, &info); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(info.Name, prefixProject) || !strings.HasSuffix(info.Name, "/billingInfo") ||
		info.BillingAccountName != "" && !validBillingName(info.BillingAccountName) {
		return nil, invalidRow(op)
	}
	return info.result(), nil
}
