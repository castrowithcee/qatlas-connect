package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The app tools read the GitHub Apps installed in an organization, read one app by its slug, and withdraw one
// repository from an installation. No tool adds a repository to an installation, suspends or deletes an
// installation, or creates an installation token: those routes grant access outward or need an app JWT.
// Tokens, token addresses (access_tokens_url), and secrets are never output. Every text and address of an app
// or an installation is untrusted data and no address is ever fetched.
//
// The removal reads the repository first (its numeric id and the type of its owner), then reads the
// installations of the repository's organization page by page until the installation id is found. An id that
// is not in that list, a repository of a user account, or a list that cannot be read completely ends the call
// without a write request. The write is one DELETE that is never repeated.

const (
	// maxInstallationPages bounds the page run that proves an installation id.
	maxInstallationPages = 50
	installationPageSize = 100
)

const (
	appInstallationsReadPermission = "GitHub refused this token the app installations of this organization; " +
		"reading them needs admin:read on a classic token, as an owner of the organization, or Organization " +
		"administration: read on a fine-grained token"
	appReadPermission   = "GitHub refused this token this app"
	appRemovePermission = "GitHub refused to remove this repository from the installation; it needs repo on a " +
		"classic token whose account administers the repository and the installation, and a fine-grained token " +
		"may not reach this route"
	appRemoveRejected = "GitHub rejected the removal; the installation must give access to selected repositories " +
		"and keep at least the access GitHub requires"
)

// ---- Descriptors

const (
	appPermissionsSchema = `{"type":"object","additionalProperties":{"type":"string"}}`
	appStringList        = `{"type":"array","items":{"type":"string"}}`
	installationIDSchema = `{"type":"integer","minimum":1,"maximum":9007199254740991}`
)

var appAccountLinks = restAccountLinkKeys

var appAccountProperties = `"login":{"type":"string"},"id":{"type":"integer"},"node_id":{"type":"string"},` +
	`"type":{"type":"string"},"user_view_type":{"type":"string"},"site_admin":{"type":"boolean"},` +
	`"name":{"type":"string"},"email":{"type":"string"},"avatar_url":{"type":"string"},` +
	`"gravatar_id":{"type":"string"},"starred_at":{"type":"string"},"html_url":{"type":"string"},` +
	`"description":{"type":"string"},"website_url":{"type":"string"},"slug":{"type":"string"},` +
	`"created_at":{"type":"string"},"updated_at":{"type":"string"},"links":` + linksSchema(appAccountLinks)

var appAccountSchema = objectSchema(appAccountProperties)

const appUntrusted = "; untrusted data"

var appAccountDescription = "Account as GitHub documents it for a user or an enterprise: login, id, node_id, type, " +
	"user_view_type, site_admin, name, email, avatar_url, gravatar_id, starred_at, html_url, and for an " +
	"enterprise description, website_url, slug, created_at, and updated_at, with the REST url and the other " +
	"*_url addresses as links; texts and addresses are untrusted data, never fetched"

const appInstallationProperties = `"id":{"type":"integer"},"account":%ACCOUNT%,"repository_selection":{"type":"string"},` +
	`"repositories_url":{"type":"string"},"html_url":{"type":"string"},"app_id":{"type":"integer"},` +
	`"client_id":{"type":"string"},"target_id":{"type":"integer"},"target_type":{"type":"string"},` +
	`"permissions":` + appPermissionsSchema + `,"permissions_truncated":{"type":"boolean"},"events":` + appStringList +
	`,"events_truncated":{"type":"boolean"},"created_at":{"type":"string"},"updated_at":{"type":"string"},` +
	`"single_file_name":{"type":"string"},"has_multiple_single_files":{"type":"boolean"},` +
	`"single_file_paths":` + appStringList + `,"single_file_paths_truncated":{"type":"boolean"},` +
	`"app_slug":{"type":"string"},"suspended_by":%ACCOUNT%,"suspended_at":{"type":"string"},` +
	`"contact_email":{"type":"string"}`

var appInstallationEntry = strings.ReplaceAll(appInstallationProperties, "%ACCOUNT%", appAccountSchema)

var appInstallationFields = []capability.Field{
	{Name: "installations", Description: "Installations of GitHub Apps in the organization, each with id, account, " +
		"repository_selection (all or selected), repositories_url and html_url (addresses, untrusted data, never " +
		"fetched), app_id, client_id, target_id, target_type, permissions (permission name to read, write, or " +
		"admin), events, created_at, updated_at, single_file_name, has_multiple_single_files, single_file_paths, " +
		"app_slug, suspended_by, suspended_at, and contact_email; permissions, events, and single_file_paths are " +
		"capped and say so through permissions_truncated, events_truncated, and single_file_paths_truncated; " +
		"access_tokens_url and every token are left out on purpose; " + appAccountDescription + appUntrusted},
	{Name: "total_count", Description: "Number of installations GitHub reports for the organization"},
	{Name: "next_cursor", Description: "Cursor of the following batch, absent when has_more is false"},
	{Name: "has_more", Description: "True when further installations follow"},
}

var appInstallationsList = capability.Descriptor{
	ID:      Provider + ".app-installations.list",
	Version: 1,
	Title:   "List GitHub App installations of an organization",
	Description: "List one bounded batch of the GitHub Apps installed in an organization an explicit connection " +
		"allows, with app slug, account, repository selection, permissions, events, and suspension; no token " +
		"and no token address is output, and addresses are never fetched",
	Tags:        []string{"github", "apps", "organization-admin", "list"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(pagingKeys),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"installations":{"type":"array","items":` +
		`{"type":"object","properties":{` + appInstallationEntry + `},"additionalProperties":false}},` +
		`"total_count":{"type":"integer"},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
		`"required":["installations","has_more"],"additionalProperties":false}`),
	Arguments: pagingArguments,
	Fields:    appInstallationFields,
	Examples: []capability.Example{{Description: "List the installed apps of an organization",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org"}`)}},
}

var appFields = []capability.Field{
	{Name: "id", Description: "Numeric app id"},
	{Name: "slug", Description: "URL-friendly name of the app" + appUntrusted},
	{Name: "node_id", Description: "GraphQL node id of the app"},
	{Name: "client_id", Description: "Client id of the app" + appUntrusted},
	{Name: "owner", Description: "Account that owns the app. " + appAccountDescription},
	{Name: "name", Description: "Name of the app" + appUntrusted},
	{Name: "description", Description: "Description of the app" + appUntrusted},
	{Name: "external_url", Description: "Address of the app's own site, untrusted data, never fetched"},
	{Name: "html_url", Description: "Web address of the app, untrusted data, never fetched"},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "permissions", Description: "Permissions the app requests, permission name to read, write, or admin"},
	{Name: "permissions_truncated", Description: "True when more permissions followed than are given"},
	{Name: "events", Description: "Webhook events the app subscribes to"},
	{Name: "events_truncated", Description: "True when more events followed than are given"},
	{Name: "installations_count", Description: "Number of installations of the app"},
}

var appsGet = capability.Descriptor{
	ID:      Provider + ".apps.get",
	Version: 1,
	Title:   "Get a GitHub App",
	Description: "Read one GitHub App by its slug: name, description, owner, permissions, events, and installation " +
		"count; GitHub-wide, so it needs a connection whose targets name no repository or project; no secret " +
		"of the app is output, and addresses are never fetched",
	Tags:        []string{"github", "apps", "organization-admin", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"app_slug":`+slugSchema, "app_slug"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"},"slug":{"type":"string"},` +
		`"node_id":{"type":"string"},"client_id":{"type":"string"},"owner":` + appAccountSchema +
		`,"name":{"type":"string"},"description":{"type":"string"},"external_url":{"type":"string"},` +
		`"html_url":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"},` +
		`"permissions":` + appPermissionsSchema + `,"permissions_truncated":{"type":"boolean"},"events":` +
		appStringList + `,"events_truncated":{"type":"boolean"},"installations_count":{"type":"integer"}},` +
		`"required":["id"],"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "app_slug", Description: "Slug of the app, as in its GitHub address " +
		"or in the app_slug of an installation", Required: true}},
	Fields:   appFields,
	Examples: []capability.Example{{Description: "Read an app", Arguments: json.RawMessage(`{"app_slug":"example-app"}`)}},
}

var appInstallationRepositoriesRemove = capability.Descriptor{
	ID:      Provider + ".app-installation-repositories.remove",
	Version: 1,
	Title:   "Remove a repository from a GitHub App installation",
	Description: "Withdraw one repository inside the connection's targets from one installation of its organization, " +
		"after a read of the repository and of the organization's installations proves the installation belongs " +
		"to that organization; a repository of a user account or an installation outside the list is refused " +
		"without a write request; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "apps", "organization-admin", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, accessGrantSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"installation_id":`+installationIDSchema, "installation_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"removed":{"type":"boolean"},` +
		`"installation_id":{"type":"integer"},"repository_id":{"type":"integer"},"app_slug":{"type":"string"}},` +
		`"required":["removed","installation_id","repository_id"],"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "installation_id", Description: "Id of the installation, as " +
		"github.app-installations.list reports it for the organization of the repository; any other id is refused",
		Required: true}},
	Fields: []capability.Field{
		{Name: "removed", Description: "True once GitHub accepted the removal"},
		{Name: "installation_id", Description: "Id of the installation the repository left"},
		{Name: "repository_id", Description: "Numeric id of the repository, read from the repository itself"},
		{Name: "app_slug", Description: "Slug of the app of the installation" + appUntrusted},
	},
	Examples: []capability.Example{{Description: "Withdraw a repository from an installation",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","installation_id":1}`)}},
}

// ---- Results

// AppAccount is a user or enterprise account of an app or an installation.
type AppAccount struct {
	Login        *string `json:"login,omitempty"`
	ID           *int64  `json:"id,omitempty"`
	NodeID       *string `json:"node_id,omitempty"`
	Type         *string `json:"type,omitempty"`
	UserViewType *string `json:"user_view_type,omitempty"`
	SiteAdmin    *bool   `json:"site_admin,omitempty"`
	Name         *string `json:"name,omitempty"`
	Email        *string `json:"email,omitempty"`
	AvatarURL    *string `json:"avatar_url,omitempty"`
	GravatarID   *string `json:"gravatar_id,omitempty"`
	StarredAt    *string `json:"starred_at,omitempty"`
	HTMLURL      *string `json:"html_url,omitempty"`
	Description  *string `json:"description,omitempty"`
	WebsiteURL   *string `json:"website_url,omitempty"`
	Slug         *string `json:"slug,omitempty"`
	CreatedAt    *string `json:"created_at,omitempty"`
	UpdatedAt    *string `json:"updated_at,omitempty"`
	Links        Links   `json:"links,omitempty"`
}

// AppInstallation is one installation of a GitHub App, without its token address.
type AppInstallation struct {
	ID                       int64             `json:"id"`
	Account                  *AppAccount       `json:"account,omitempty"`
	RepositorySelection      *string           `json:"repository_selection,omitempty"`
	RepositoriesURL          *string           `json:"repositories_url,omitempty"`
	HTMLURL                  *string           `json:"html_url,omitempty"`
	AppID                    *int64            `json:"app_id,omitempty"`
	ClientID                 *string           `json:"client_id,omitempty"`
	TargetID                 *int64            `json:"target_id,omitempty"`
	TargetType               *string           `json:"target_type,omitempty"`
	Permissions              map[string]string `json:"permissions,omitempty"`
	PermissionsTruncated     bool              `json:"permissions_truncated,omitempty"`
	Events                   []string          `json:"events,omitempty"`
	EventsTruncated          bool              `json:"events_truncated,omitempty"`
	CreatedAt                *string           `json:"created_at,omitempty"`
	UpdatedAt                *string           `json:"updated_at,omitempty"`
	SingleFileName           *string           `json:"single_file_name,omitempty"`
	HasMultipleSingleFiles   *bool             `json:"has_multiple_single_files,omitempty"`
	SingleFilePaths          []string          `json:"single_file_paths,omitempty"`
	SingleFilePathsTruncated bool              `json:"single_file_paths_truncated,omitempty"`
	AppSlug                  *string           `json:"app_slug,omitempty"`
	SuspendedBy              *AppAccount       `json:"suspended_by,omitempty"`
	SuspendedAt              *string           `json:"suspended_at,omitempty"`
	ContactEmail             *string           `json:"contact_email,omitempty"`
}

// AppInstallationList is one batch of the installations of an organization.
type AppInstallationList struct {
	Installations []AppInstallation `json:"installations"`
	TotalCount    *int64            `json:"total_count,omitempty"`
	NextCursor    string            `json:"next_cursor,omitempty"`
	HasMore       bool              `json:"has_more"`
}

// AppDetails is one GitHub App.
type AppDetails struct {
	ID                   int64             `json:"id"`
	Slug                 *string           `json:"slug,omitempty"`
	NodeID               *string           `json:"node_id,omitempty"`
	ClientID             *string           `json:"client_id,omitempty"`
	Owner                *AppAccount       `json:"owner,omitempty"`
	Name                 *string           `json:"name,omitempty"`
	Description          *string           `json:"description,omitempty"`
	ExternalURL          *string           `json:"external_url,omitempty"`
	HTMLURL              *string           `json:"html_url,omitempty"`
	CreatedAt            *string           `json:"created_at,omitempty"`
	UpdatedAt            *string           `json:"updated_at,omitempty"`
	Permissions          map[string]string `json:"permissions,omitempty"`
	PermissionsTruncated bool              `json:"permissions_truncated,omitempty"`
	Events               []string          `json:"events,omitempty"`
	EventsTruncated      bool              `json:"events_truncated,omitempty"`
	InstallationsCount   *int64            `json:"installations_count,omitempty"`
}

// AppInstallationRepositoryRemoved is the answer of a removal.
type AppInstallationRepositoryRemoved struct {
	Removed        bool    `json:"removed"`
	InstallationID int64   `json:"installation_id"`
	RepositoryID   int64   `json:"repository_id"`
	AppSlug        *string `json:"app_slug,omitempty"`
}

// ---- Raw answers

type appAccountRaw struct {
	Login        *string `json:"login"`
	ID           *int64  `json:"id"`
	NodeID       *string `json:"node_id"`
	Type         *string `json:"type"`
	UserViewType *string `json:"user_view_type"`
	SiteAdmin    *bool   `json:"site_admin"`
	Name         *string `json:"name"`
	Email        *string `json:"email"`
	AvatarURL    *string `json:"avatar_url"`
	GravatarID   *string `json:"gravatar_id"`
	StarredAt    *string `json:"starred_at"`
	HTMLURL      *string `json:"html_url"`
	Description  *string `json:"description"`
	WebsiteURL   *string `json:"website_url"`
	Slug         *string `json:"slug"`
	CreatedAt    *string `json:"created_at"`
	UpdatedAt    *string `json:"updated_at"`
	links        Links
}

func (r *appAccountRaw) UnmarshalJSON(data []byte) (err error) {
	type plain appAccountRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, appAccountLinks)
	return err
}

func (r *appAccountRaw) project() *AppAccount {
	if r == nil {
		return nil
	}
	return &AppAccount{Login: nameText(r.Login), ID: r.ID, NodeID: nameText(r.NodeID), Type: nameText(r.Type),
		UserViewType: nameText(r.UserViewType), SiteAdmin: r.SiteAdmin, Name: nameText(r.Name),
		Email: nameText(r.Email), AvatarURL: address(r.AvatarURL), GravatarID: nameText(r.GravatarID),
		StarredAt: nameText(r.StarredAt), HTMLURL: address(r.HTMLURL), Description: longText(r.Description),
		WebsiteURL: address(r.WebsiteURL), Slug: nameText(r.Slug), CreatedAt: nameText(r.CreatedAt),
		UpdatedAt: nameText(r.UpdatedAt), Links: r.links}
}

// appInstallationRaw holds every member GitHub documents for an installation; access_tokens_url is not decoded.
type appInstallationRaw struct {
	ID                     *int64            `json:"id"`
	Account                *appAccountRaw    `json:"account"`
	RepositorySelection    *string           `json:"repository_selection"`
	RepositoriesURL        *string           `json:"repositories_url"`
	HTMLURL                *string           `json:"html_url"`
	AppID                  *int64            `json:"app_id"`
	ClientID               *string           `json:"client_id"`
	TargetID               *int64            `json:"target_id"`
	TargetType             *string           `json:"target_type"`
	Permissions            map[string]string `json:"permissions"`
	Events                 []string          `json:"events"`
	CreatedAt              *string           `json:"created_at"`
	UpdatedAt              *string           `json:"updated_at"`
	SingleFileName         *string           `json:"single_file_name"`
	HasMultipleSingleFiles *bool             `json:"has_multiple_single_files"`
	SingleFilePaths        []string          `json:"single_file_paths"`
	AppSlug                *string           `json:"app_slug"`
	SuspendedBy            *appAccountRaw    `json:"suspended_by"`
	SuspendedAt            *string           `json:"suspended_at"`
	ContactEmail           *string           `json:"contact_email"`
}

type appInstallationsRaw struct {
	TotalCount    *int64               `json:"total_count"`
	Installations []appInstallationRaw `json:"installations"`
}

type appObjectRaw struct {
	ID                 *int64            `json:"id"`
	Slug               *string           `json:"slug"`
	NodeID             *string           `json:"node_id"`
	ClientID           *string           `json:"client_id"`
	Owner              *appAccountRaw    `json:"owner"`
	Name               *string           `json:"name"`
	Description        *string           `json:"description"`
	ExternalURL        *string           `json:"external_url"`
	HTMLURL            *string           `json:"html_url"`
	CreatedAt          *string           `json:"created_at"`
	UpdatedAt          *string           `json:"updated_at"`
	Permissions        map[string]string `json:"permissions"`
	Events             []string          `json:"events"`
	InstallationsCount *int64            `json:"installations_count"`
}

// cleanStrings cleans a list of untrusted texts and caps it.
func cleanStrings(values []string) ([]string, bool) {
	values, cut := capped(values)
	var out []string
	for i := range values {
		if text := nameText(&values[i]); text != nil {
			out = append(out, *text)
		}
	}
	return out, cut
}

// cleanPermissions cleans a permission map and caps it at the first entries in name order.
func cleanPermissions(values map[string]string) (map[string]string, bool) {
	if len(values) == 0 {
		return nil, false
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	names, cut := capped(names)
	out := make(map[string]string, len(names))
	for _, name := range names {
		value := values[name]
		key := nameText(&name)
		if key == nil {
			continue
		}
		out[*key] = *nameText(&value)
	}
	return out, cut
}

func (r *appInstallationRaw) project(op string) (*AppInstallation, error) {
	if r.ID == nil || *r.ID < 1 {
		return nil, invalidEntry(op, "an app installation")
	}
	view := &AppInstallation{ID: *r.ID, Account: r.Account.project(), RepositorySelection: shortText(r.RepositorySelection),
		RepositoriesURL: address(r.RepositoriesURL), HTMLURL: address(r.HTMLURL), AppID: r.AppID,
		ClientID: shortText(r.ClientID), TargetID: r.TargetID, TargetType: shortText(r.TargetType),
		CreatedAt: shortText(r.CreatedAt), UpdatedAt: shortText(r.UpdatedAt), SingleFileName: shortText(r.SingleFileName),
		HasMultipleSingleFiles: r.HasMultipleSingleFiles, AppSlug: shortText(r.AppSlug),
		SuspendedBy: r.SuspendedBy.project(), SuspendedAt: shortText(r.SuspendedAt), ContactEmail: shortText(r.ContactEmail)}
	view.Permissions, view.PermissionsTruncated = cleanPermissions(r.Permissions)
	view.Events, view.EventsTruncated = cleanStrings(r.Events)
	view.SingleFilePaths, view.SingleFilePathsTruncated = cleanStrings(r.SingleFilePaths)
	return view, nil
}

func (r *appObjectRaw) project(op string) (*AppDetails, error) {
	if r.ID == nil || *r.ID < 1 {
		return nil, invalidEntry(op, "an app")
	}
	view := &AppDetails{ID: *r.ID, Slug: shortText(r.Slug), NodeID: shortText(r.NodeID), ClientID: shortText(r.ClientID),
		Owner: r.Owner.project(), Name: nameText(r.Name), Description: longText(r.Description),
		ExternalURL: address(r.ExternalURL), HTMLURL: address(r.HTMLURL), CreatedAt: shortText(r.CreatedAt),
		UpdatedAt: shortText(r.UpdatedAt), InstallationsCount: r.InstallationsCount}
	view.Permissions, view.PermissionsTruncated = cleanPermissions(r.Permissions)
	view.Events, view.EventsTruncated = cleanStrings(r.Events)
	return view, nil
}

// ---- Arguments and handlers

type appArguments struct {
	Limit          int    `json:"limit"`
	Cursor         string `json:"cursor"`
	AppSlug        string `json:"app_slug"`
	InstallationID int64  `json:"installation_id"`

	page, perPage int
	binding       []byte
}

func checkAppInstallationsList(a *appArguments, bound target) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint("appinstallations", strings.ToLower(bound.String()))
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkAppSlug(a *appArguments, _ target) error {
	if !validLogin(a.AppSlug) {
		return invalidRequest("app_slug must be the slug of an app, letters, digits, hyphens, and underscores")
	}
	return nil
}

func checkInstallationID(a *appArguments, _ target) error {
	if a.InstallationID < 1 || a.InstallationID > 9007199254740991 {
		return invalidRequest("installation_id must be a positive installation id")
	}
	return nil
}

// appOrganizationHandler resolves the organization of an organization tool before a credential is read.
func appOrganizationHandler(id string, check func(*appArguments, target) error,
	call func(context.Context, *Client, *appArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments appArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if err := check(&arguments, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

// appRepositoryHandler resolves the repository of the removal against the targets and the exclusions before a
// credential is read.
func appRepositoryHandler(id string, check func(*appArguments, target) error,
	call func(context.Context, *Client, *appArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments appArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if err := check(&arguments, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

// appGlobalHandler serves the GitHub-wide app read: a connection whose targets name a repository or a project
// is refused before a secret is read.
func appGlobalHandler(id string, check func(*appArguments, target) error,
	call func(context.Context, *Client, *appArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments appArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		if resolved == nil {
			return nil, providerError("open", "no connection was selected")
		}
		allowed, err := allowlistOf(resolved)
		if err != nil {
			return nil, providerError("open", err.Error())
		}
		if err := accountWideAllowed(allowed); err != nil {
			return nil, err
		}
		if err := check(&arguments, target{}); err != nil {
			return nil, err
		}
		client, err := Open(ctx, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		return call(ctx, client, &arguments)
	}
}

func appOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: appInstallationsList, Handler: appOrganizationHandler(appInstallationsList.ID,
			checkAppInstallationsList, func(ctx context.Context, c *Client, a *appArguments) (any, error) {
				return c.listAppInstallations(ctx, a)
			})},
		{Descriptor: appsGet, Handler: appGlobalHandler(appsGet.ID, checkAppSlug,
			func(ctx context.Context, c *Client, a *appArguments) (any, error) { return c.getApp(ctx, a) })},
		{Descriptor: appInstallationRepositoriesRemove, Handler: appRepositoryHandler(
			appInstallationRepositoriesRemove.ID, checkInstallationID,
			func(ctx context.Context, c *Client, a *appArguments) (any, error) {
				return c.removeAppInstallationRepository(ctx, a)
			})},
	}
}

// ---- Requests

func installationsPath(owner string) string {
	return "/orgs/" + url.PathEscape(owner) + "/installations"
}

func (c *Client) listAppInstallations(ctx context.Context, a *appArguments) (*AppInstallationList, error) {
	const op = "list app installations"
	var raw appInstallationsRaw
	hasNext, err := c.restPage(ctx, op, installationsPath(c.target.owner), pageQuery(a.page, a.perPage), &raw)
	if err != nil {
		return nil, actionsFailure(err, appInstallationsReadPermission)
	}
	result := &AppInstallationList{Installations: make([]AppInstallation, 0, len(raw.Installations)),
		TotalCount: raw.TotalCount}
	for i := range raw.Installations {
		view, err := raw.Installations[i].project(op)
		if err != nil {
			return nil, err
		}
		result.Installations = append(result.Installations, *view)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) getApp(ctx context.Context, a *appArguments) (*AppDetails, error) {
	const op = "get app"
	var raw appObjectRaw
	if err := c.rest(ctx, op, "/apps/"+url.PathEscape(a.AppSlug), &raw); err != nil {
		return nil, actionsFailure(err, appReadPermission)
	}
	return raw.project(op)
}

// repositoryProof is what the proof read of the repository yields.
type repositoryProof struct {
	ID    *int64  `json:"id"`
	Name  *string `json:"name"`
	Owner *struct {
		Login *string `json:"login"`
		Type  *string `json:"type"`
	} `json:"owner"`
}

// proveRepository reads the bound repository and returns its numeric id; the owner must be an organization
// and must be the owner the call addressed.
func (c *Client) proveRepository(ctx context.Context, op string) (int64, error) {
	var raw repositoryProof
	path := "/repos/" + url.PathEscape(c.target.owner) + "/" + url.PathEscape(c.target.repo)
	if err := c.rest(ctx, op, path, &raw); err != nil {
		return 0, err
	}
	if raw.ID == nil || *raw.ID < 1 || raw.Name == nil || raw.Owner == nil || raw.Owner.Login == nil ||
		raw.Owner.Type == nil {
		return 0, invalidEntry(op, "a repository")
	}
	if !strings.EqualFold(*raw.Owner.Login, c.target.owner) || !strings.EqualFold(*raw.Name, c.target.repo) {
		return 0, foreignActionsObject(op, "a repository")
	}
	if *raw.Owner.Type != "Organization" {
		return 0, invalidRequest("the repository belongs to a user account, not to an organization; the " +
			"installations of an organization cannot be read for it, so no removal was sent")
	}
	return *raw.ID, nil
}

// findInstallation reads the installations of the organization page by page until the id is found. It gives up
// without an answer when the list cannot be read completely, so an id counts as foreign only after the whole
// list was read.
func (c *Client) findInstallation(ctx context.Context, op string, id int64) (*appInstallationRaw, error) {
	var seen int64
	for page := 1; page <= maxInstallationPages; page++ {
		var raw appInstallationsRaw
		hasNext, err := c.restPage(ctx, op, installationsPath(c.target.owner), pageQuery(page, installationPageSize), &raw)
		if err != nil {
			return nil, actionsFailure(err, appInstallationsReadPermission)
		}
		for i := range raw.Installations {
			if raw.Installations[i].ID != nil && *raw.Installations[i].ID == id {
				return &raw.Installations[i], nil
			}
		}
		seen += int64(len(raw.Installations))
		if !hasNext {
			if raw.TotalCount != nil && seen < *raw.TotalCount {
				return nil, providerError(op, "the installations of the organization could not be read completely; "+
					"no removal was sent")
			}
			return nil, invalidRequest("installation_id is not an installation of the organization " +
				c.target.owner + "; read github.app-installations.list for its installations")
		}
		if len(raw.Installations) == 0 {
			break
		}
	}
	return nil, providerError(op, "the organization has more installations than Qatlas reads to prove an "+
		"installation id; no removal was sent")
}

func (c *Client) removeAppInstallationRepository(ctx context.Context, a *appArguments) (*AppInstallationRepositoryRemoved, error) {
	const op = "remove app installation repository"
	repositoryID, err := c.proveRepository(ctx, op)
	if err != nil {
		return nil, actionsFailure(err, "GitHub refused this token the repository; reading it needs repo on a "+
			"classic token for a private repository, or Metadata: read on a fine-grained token")
	}
	installation, err := c.findInstallation(ctx, op, a.InstallationID)
	if err != nil {
		return nil, err
	}
	if installation.RepositorySelection == nil || *installation.RepositorySelection != "selected" {
		return nil, invalidRequest("the installation does not use a selection of repositories (it may give access " +
			"to all repositories of the organization), so GitHub cannot withdraw a single repository; no removal " +
			"was sent")
	}
	path := "/user/installations/" + strconv.FormatInt(a.InstallationID, 10) + "/repositories/" +
		strconv.FormatInt(repositoryID, 10)
	if err := c.restChange(ctx, op, http.MethodDelete, path, struct{}{}, nil); err != nil {
		return nil, variableFailure(err, appRemovePermission, "", appRemoveRejected)
	}
	return &AppInstallationRepositoryRemoved{Removed: true, InstallationID: a.InstallationID,
		RepositoryID: repositoryID, AppSlug: shortText(installation.AppSlug)}, nil
}
