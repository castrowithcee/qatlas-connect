package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The private registry tools read and delete the private registries an organization hands to Dependabot.
// GitHub answers metadata only: the decoded structs have no member for an encrypted value, so a value a server
// adds is dropped, and the public key route is not reachable. The name, the username, and the registry
// address are untrusted data; the address is answered as text and never fetched. Every tool names an
// organization as an owner target and checks it, and the exclusions, before a credential is resolved.

const privateRegistrySensitivity = "github-private-registry"

const (
	privateRegistryReadPermission = "GitHub refused this token the private registries of this organization; " +
		"reading them needs admin:org on a classic token, or the organization permission Organization private " +
		"registries: read on a fine-grained token"
	privateRegistryDeletePermission = "GitHub refused this token the deletion of a private registry of this " +
		"organization; deleting one needs admin:org on a classic token, or the organization permission " +
		"Organization private registries: write on a fine-grained token"
)

// privateRegistryLookupBudget bounds the repository lookups one call spends on telling the selected
// repositories of its registries inside from outside the targets; a repository beyond it is withheld.
const privateRegistryLookupBudget = 100

// privateRegistryNameSchema is one plain path segment of letters, digits, and _, as GitHub names secrets. No
// hyphen and no dot is allowed, so a segment can neither be . or .. nor name the public key route.
const privateRegistryNameSchema = `{"type":"string","minLength":1,"maxLength":255,"pattern":"^[A-Za-z0-9_]+$"}`

const privateRegistryProperties = `"name":{"type":"string"},"registry_type":{"type":"string"},` +
	`"auth_type":{"type":"string"},"url":{"type":"string"},"tenant_id":{"type":"string"},"client_id":{"type":"string"},"aws_region":{"type":"string"},"account_id":{"type":"string"},"role_name":{"type":"string"},"domain":{"type":"string"},"domain_owner":{"type":"string"},"jfrog_oidc_provider_name":{"type":"string"},"audience":{"type":"string"},"identity_mapping_name":{"type":"string"},"namespace":{"type":"string"},"service_slug":{"type":"string"},"api_host":{"type":"string"},"workload_identity_provider":{"type":"string"},"service_account":{"type":"string"},` +
	`"username":{"type":"string"},"replaces_base":{"type":"boolean"},"visibility":{"type":"string"},` +
	`"selected_repository_ids":{"type":"array","items":{"type":"integer"}},"withheld":{"type":"integer"},` +
	`"created_at":{"type":"string"},"updated_at":{"type":"string"}`

const privateRegistryRequired = `"required":["name"],"additionalProperties":false`

var privateRegistryMembers = []capability.Field{
	{Name: "name", Description: "Name of the registry's secret, as the other tools take it; untrusted data"},
	{Name: "registry_type", Description: "Kind of registry, such as maven_repository, npm_registry, or " +
		"docker_registry, as GitHub names it; untrusted data"},
	{Name: "auth_type", Description: "The authentication type: token, username_password, anonymous, or an oidc_ kind; untrusted data"},
	{Name: "url", Description: "Address of the registry; untrusted data, never fetched or used as an address by Qatlas"},
	{Name: "tenant_id", Description: "The tenant ID of the Azure AD application; untrusted data"},
	{Name: "client_id", Description: "The client ID of the Azure AD application; untrusted data"},
	{Name: "aws_region", Description: "The AWS region; untrusted data"},
	{Name: "account_id", Description: "The AWS account ID; untrusted data"},
	{Name: "role_name", Description: "The AWS IAM role name; untrusted data"},
	{Name: "domain", Description: "The CodeArtifact domain; untrusted data"},
	{Name: "domain_owner", Description: "The CodeArtifact domain owner; untrusted data"},
	{Name: "jfrog_oidc_provider_name", Description: "The JFrog OIDC provider name; untrusted data"},
	{Name: "audience", Description: "The OIDC audience; untrusted data"},
	{Name: "identity_mapping_name", Description: "The JFrog identity mapping name; untrusted data"},
	{Name: "namespace", Description: "The Cloudsmith organization namespace; untrusted data"},
	{Name: "service_slug", Description: "The Cloudsmith service account slug; untrusted data"},
	{Name: "api_host", Description: "The Cloudsmith API host; untrusted data"},
	{Name: "workload_identity_provider", Description: "The full resource name of the GCP Workload Identity Provider; untrusted data"},
	{Name: "service_account", Description: "The GCP service account email to impersonate; untrusted data"},
	{Name: "username", Description: "Username Dependabot signs in with, when GitHub reports one; untrusted data"},
	{Name: "replaces_base", Description: "True when Dependabot uses the registry instead of the ecosystem's " +
		"public default"},
	{Name: "visibility", Description: "all, private, or selected: which repositories of the organization may use " +
		"the registry"},
	{Name: "selected_repository_ids", Description: "Ids of the repositories that may use the registry when " +
		"visibility is selected, only those inside the connection's targets; GitHub documents it for the detail " +
		"read, and the list shows it when GitHub sends it"},
	{Name: "withheld", Description: "Count of selected repositories outside the connection's targets, in an " +
		"excluded repository, or not checkable, omitted when none were withheld"},
	{Name: "created_at", Description: "When the registry was created"},
	{Name: "updated_at", Description: "When the registry was last updated"},
}

var privateRegistryNameArgument = capability.Argument{Name: "secret_name", Description: "Name of the registry's " +
	"secret, as github.private-registries.list reports it in name; letters, digits, and _", Required: true}

var privateRegistriesList = capability.Descriptor{
	ID:      Provider + ".private-registries.list",
	Version: 1,
	Title:   "List the private registries of a GitHub organization",
	Description: "List one bounded batch of the private registries Dependabot uses in an organization an explicit " +
		"connection allows, with every member GitHub documents; never a value or key; names, usernames, and " +
		"addresses are untrusted data and the addresses are never fetched; when the connection's targets name any, selected repositories outside " +
		"them or in an excluded repository are counted in withheld instead of listed; needs organization admin " +
		"rights",
	Tags:        []string{"github", "security", "dependabot", "private-registries", "organization", "list"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(pagingKeys),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"configurations":{"type":"array","items":{` +
		`"type":"object","properties":{` + privateRegistryProperties + `},` + privateRegistryRequired + `}},` +
		`"total_count":{"type":"integer"},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
		`"required":["configurations","total_count","has_more"],"additionalProperties":false}`),
	Arguments: pagingArguments,
	Fields: append(append([]capability.Field{
		{Name: "configurations", Description: "Private registries with the members below; a member GitHub leaves " +
			"out or sends as null is absent"},
		{Name: "total_count", Description: "Number of private registries GitHub reports for the organization"},
	}, privateRegistryMembers...), pagingFields...),
	Examples: []capability.Example{{
		Description: "List the private registries of an organization",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org"}`),
	}},
}

var privateRegistriesGet = capability.Descriptor{
	ID:      Provider + ".private-registries.get",
	Version: 1,
	Title:   "Get a GitHub private registry",
	Description: "Read one private registry of an organization an explicit connection allows by its secret name, " +
		"with every member GitHub documents; never a value or key; name, username, and address are untrusted data and the address is never fetched; " +
		"selected repositories outside the connection's targets are counted in withheld; needs " +
		"organization admin rights",
	Tags:        []string{"github", "security", "dependabot", "private-registries", "organization", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"secret_name":`+privateRegistryNameSchema, "secret_name"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + privateRegistryProperties + `},` +
		privateRegistryRequired + `}`),
	Arguments: []capability.Argument{privateRegistryNameArgument},
	Fields:    privateRegistryMembers,
	Examples: []capability.Example{{
		Description: "Read one private registry",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","secret_name":"MAVEN_TOKEN"}`),
	}},
}

var privateRegistriesDelete = capability.Descriptor{
	ID:      Provider + ".private-registries.delete",
	Version: 1,
	Title:   "Delete a GitHub private registry",
	Description: "Delete one private registry of an organization an explicit connection allows by its secret name, " +
		"permanently; the registry belongs to the organization, so repositories outside the connection's targets " +
		"lose it as well; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "security", "dependabot", "private-registries", "organization", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, privateRegistrySensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"secret_name":`+privateRegistryNameSchema, "secret_name"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"deleted":{"type":"boolean"},` +
		`"name":{"type":"string"}},"required":["deleted","name"],"additionalProperties":false}`),
	Arguments: []capability.Argument{privateRegistryNameArgument},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the registry"},
		{Name: "name", Description: "Secret name of the deleted registry"}},
	Examples: []capability.Example{{
		Description: "Delete a private registry",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","secret_name":"MAVEN_TOKEN"}`),
	}},
}

// privateRegistryReadToolIDs are the two read tools, the ones of the setup profile security.
var privateRegistryReadToolIDs = []string{privateRegistriesList.ID, privateRegistriesGet.ID}

// privateRegistryToolIDs are all private registry tools; each names an organization as its owner target.
var privateRegistryToolIDs = []string{privateRegistriesList.ID, privateRegistriesGet.ID, privateRegistriesDelete.ID}

// privateRegistryArguments holds the arguments of the three tools; the input schema of each admits only its
// own. Nothing here can hold a value.
type privateRegistryArguments struct {
	SecretName string `json:"secret_name"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`

	allowed       allowlist
	page, perPage int
	binding       []byte
}

func checkPrivateRegistryName(name string) error {
	if name == "" || len(name) > 255 {
		return invalidRequest("secret_name must be the name of a private registry, up to 255 characters")
	}
	for _, r := range name {
		if r != '_' && !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
			return invalidRequest("secret_name must hold only letters, digits, and _")
		}
	}
	return nil
}

func privateRegistryOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*privateRegistryArguments, target) error,
		call func(context.Context, *Client, *privateRegistryArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: privateRegistryHandler(descriptor.ID, check, call)}
	}
	named := func(a *privateRegistryArguments, _ target) error { return checkPrivateRegistryName(a.SecretName) }
	return []capability.Operation{
		bind(privateRegistriesList, func(a *privateRegistryArguments, bound target) error {
			limit, err := normalizeLimit(a.Limit)
			if err != nil {
				return err
			}
			a.binding = fingerprint("privateregistries", strings.ToLower(bound.String()))
			a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
			return err
		}, func(ctx context.Context, c *Client, a *privateRegistryArguments) (any, error) {
			return c.listPrivateRegistries(ctx, a)
		}),
		bind(privateRegistriesGet, named, func(ctx context.Context, c *Client, a *privateRegistryArguments) (any, error) {
			return c.getPrivateRegistry(ctx, a)
		}),
		bind(privateRegistriesDelete, named, func(ctx context.Context, c *Client, a *privateRegistryArguments) (any, error) {
			return c.deletePrivateRegistry(ctx, a)
		}),
	}
}

// privateRegistryHandler decodes and checks the arguments and the organization before a credential is
// resolved, so a refused request never becomes a provider call or a secret read. The owner must be an
// organization the targets name as an owner and no exclusion covers.
func privateRegistryHandler(id string, check func(*privateRegistryArguments, target) error,
	call func(context.Context, *Client, *privateRegistryArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments privateRegistryArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if arguments.allowed, err = allowlistOf(resolved); err != nil {
			return nil, providerError("open", err.Error())
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

// privateRegistryRaw is a registry as GitHub answers it. It has no member for a value, so none can be decoded.
type privateRegistryRaw struct {
	AuthType                 *string `json:"auth_type"`
	URL                      *string `json:"url"`
	TenantID                 *string `json:"tenant_id"`
	ClientID                 *string `json:"client_id"`
	AWSRegion                *string `json:"aws_region"`
	AccountID                *string `json:"account_id"`
	RoleName                 *string `json:"role_name"`
	Domain                   *string `json:"domain"`
	DomainOwner              *string `json:"domain_owner"`
	JFrogOIDCProviderName    *string `json:"jfrog_oidc_provider_name"`
	Audience                 *string `json:"audience"`
	IdentityMappingName      *string `json:"identity_mapping_name"`
	Namespace                *string `json:"namespace"`
	ServiceSlug              *string `json:"service_slug"`
	APIHost                  *string `json:"api_host"`
	WorkloadIdentityProvider *string `json:"workload_identity_provider"`
	ServiceAccount           *string `json:"service_account"`
	Name                     *string `json:"name"`
	RegistryType             *string `json:"registry_type"`
	Username                 *string `json:"username"`
	ReplacesBase             *bool   `json:"replaces_base"`
	Visibility               *string `json:"visibility"`
	SelectedRepositoryIDs    []int64 `json:"selected_repository_ids"`
	CreatedAt                *string `json:"created_at"`
	UpdatedAt                *string `json:"updated_at"`
}

// PrivateRegistry is the metadata of one private registry of an organization; GitHub never answers a value.
// SelectedRepositoryIDs holds the selected repositories inside the connection's targets, Withheld counts the
// others.
type PrivateRegistry struct {
	AuthType                 *string `json:"auth_type,omitempty"`
	URL                      *string `json:"url,omitempty"`
	TenantID                 *string `json:"tenant_id,omitempty"`
	ClientID                 *string `json:"client_id,omitempty"`
	AWSRegion                *string `json:"aws_region,omitempty"`
	AccountID                *string `json:"account_id,omitempty"`
	RoleName                 *string `json:"role_name,omitempty"`
	Domain                   *string `json:"domain,omitempty"`
	DomainOwner              *string `json:"domain_owner,omitempty"`
	JFrogOIDCProviderName    *string `json:"jfrog_oidc_provider_name,omitempty"`
	Audience                 *string `json:"audience,omitempty"`
	IdentityMappingName      *string `json:"identity_mapping_name,omitempty"`
	Namespace                *string `json:"namespace,omitempty"`
	ServiceSlug              *string `json:"service_slug,omitempty"`
	APIHost                  *string `json:"api_host,omitempty"`
	WorkloadIdentityProvider *string `json:"workload_identity_provider,omitempty"`
	ServiceAccount           *string `json:"service_account,omitempty"`
	Name                     string  `json:"name"`
	RegistryType             *string `json:"registry_type,omitempty"`
	Username                 *string `json:"username,omitempty"`
	ReplacesBase             *bool   `json:"replaces_base,omitempty"`
	Visibility               *string `json:"visibility,omitempty"`
	SelectedRepositoryIDs    []int64 `json:"selected_repository_ids,omitempty"`
	Withheld                 int     `json:"withheld,omitempty"`
	CreatedAt                *string `json:"created_at,omitempty"`
	UpdatedAt                *string `json:"updated_at,omitempty"`
}

// PrivateRegistryList is one batch of the private registries of an organization.
type PrivateRegistryList struct {
	Configurations []PrivateRegistry `json:"configurations"`
	TotalCount     int64             `json:"total_count"`
	NextCursor     string            `json:"next_cursor,omitempty"`
	HasMore        bool              `json:"has_more"`
}

// PrivateRegistryDeleted is the answer to a deleted private registry.
type PrivateRegistryDeleted struct {
	Deleted bool   `json:"deleted"`
	Name    string `json:"name"`
}

// repositoryChecker tells repositories by id inside from outside the connection's targets. The owner is
// always allowed as a whole, so only an exclusion of one of its repositories can put a repository outside;
// without one no lookup happens. A repository whose lookup fails, whose answer names another id, or beyond the
// budget counts as outside.
type repositoryChecker struct {
	c       *Client
	allowed allowlist
	budget  int
	known   map[int64]bool
}

func (c *Client) repositoryChecker(allowed allowlist) *repositoryChecker {
	return &repositoryChecker{c: c, allowed: allowed, budget: privateRegistryLookupBudget, known: map[int64]bool{}}
}

func (r *repositoryChecker) lookupNeeded() bool {
	for _, entry := range r.allowed.excludes {
		if entry.kind == kindRepository && strings.EqualFold(entry.owner, r.c.target.owner) {
			return true
		}
	}
	return false
}

func (r *repositoryChecker) inside(ctx context.Context, op string, id int64) (bool, error) {
	if !r.lookupNeeded() {
		return true, nil
	}
	if known, ok := r.known[id]; ok {
		return known, nil
	}
	if id < 1 || r.budget == 0 {
		return false, nil
	}
	r.budget--
	var raw struct {
		ID       int64   `json:"id"`
		FullName *string `json:"full_name"`
	}
	if err := r.c.rest(ctx, op, "/repositories/"+strconv.FormatInt(id, 10), &raw); err != nil {
		if ctx.Err() != nil {
			return false, err
		}
		r.known[id] = false
		return false, nil
	}
	inside := raw.ID == id && (&repositoryRaw{FullName: raw.FullName}).admitted(r.allowed)
	r.known[id] = inside
	return inside, nil
}

func (raw *privateRegistryRaw) project(ctx context.Context, op string, checker *repositoryChecker) (*PrivateRegistry, error) {
	name, _ := cleanText(filled(raw.Name), 255, false)
	if name == nil {
		return nil, invalidEntry(op, "a private registry")
	}
	registry := &PrivateRegistry{
		Name:                     *name,
		AuthType:                 shortText(raw.AuthType),
		URL:                      address(raw.URL),
		TenantID:                 shortText(raw.TenantID),
		ClientID:                 shortText(raw.ClientID),
		AWSRegion:                shortText(raw.AWSRegion),
		AccountID:                shortText(raw.AccountID),
		RoleName:                 shortText(raw.RoleName),
		Domain:                   shortText(raw.Domain),
		DomainOwner:              shortText(raw.DomainOwner),
		JFrogOIDCProviderName:    shortText(raw.JFrogOIDCProviderName),
		Audience:                 shortText(raw.Audience),
		IdentityMappingName:      shortText(raw.IdentityMappingName),
		Namespace:                shortText(raw.Namespace),
		ServiceSlug:              shortText(raw.ServiceSlug),
		APIHost:                  shortText(raw.APIHost),
		WorkloadIdentityProvider: shortText(raw.WorkloadIdentityProvider),
		ServiceAccount:           shortText(raw.ServiceAccount),
		RegistryType:             shortText(raw.RegistryType),
		Username:                 shortText(raw.Username),
		ReplacesBase:             raw.ReplacesBase,
		Visibility:               shortText(raw.Visibility),
		CreatedAt:                nameText(raw.CreatedAt),
		UpdatedAt:                nameText(raw.UpdatedAt),
	}
	for _, id := range raw.SelectedRepositoryIDs {
		inside, err := checker.inside(ctx, op, id)
		if err != nil {
			return nil, err
		}
		if !inside {
			registry.Withheld++
			continue
		}
		registry.SelectedRepositoryIDs = append(registry.SelectedRepositoryIDs, id)
	}
	return registry, nil
}

func (c *Client) privateRegistryPath(name string) string {
	path := c.orgPath("private-registries")
	if name != "" {
		path += "/" + url.PathEscape(name)
	}
	return path
}

func (c *Client) listPrivateRegistries(ctx context.Context, a *privateRegistryArguments) (*PrivateRegistryList, error) {
	const op = "list private registries"
	var raw struct {
		TotalCount     int64                `json:"total_count"`
		Configurations []privateRegistryRaw `json:"configurations"`
	}
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if err := c.rest(ctx, op, c.privateRegistryPath("")+"?"+query.Encode(), &raw); err != nil {
		return nil, actionsFailure(err, privateRegistryReadPermission)
	}
	checker := c.repositoryChecker(a.allowed)
	result := &PrivateRegistryList{Configurations: make([]PrivateRegistry, 0, len(raw.Configurations)),
		TotalCount: raw.TotalCount}
	for i := range raw.Configurations {
		registry, err := raw.Configurations[i].project(ctx, op, checker)
		if err != nil {
			return nil, err
		}
		result.Configurations = append(result.Configurations, *registry)
	}
	if hasMoreSecrets(len(raw.Configurations), a.page, a.perPage, raw.TotalCount) {
		result.HasMore = true
		result.NextCursor = encodeCursor(a.binding, strconv.Itoa(a.page+1)+":"+strconv.Itoa(a.perPage))
	}
	return result, nil
}

func (c *Client) getPrivateRegistry(ctx context.Context, a *privateRegistryArguments) (*PrivateRegistry, error) {
	const op = "get private registry"
	var raw privateRegistryRaw
	if err := c.rest(ctx, op, c.privateRegistryPath(a.SecretName), &raw); err != nil {
		return nil, actionsFailure(err, privateRegistryReadPermission)
	}
	return raw.project(ctx, op, c.repositoryChecker(a.allowed))
}

// deletePrivateRegistry deletes one registry in a single request that is never repeated.
func (c *Client) deletePrivateRegistry(ctx context.Context, a *privateRegistryArguments) (*PrivateRegistryDeleted, error) {
	const op = "delete private registry"
	if err := c.restChange(ctx, op, http.MethodDelete, c.privateRegistryPath(a.SecretName), nil, nil); err != nil {
		return nil, actionsFailure(err, privateRegistryDeletePermission)
	}
	return &PrivateRegistryDeleted{Deleted: true, Name: a.SecretName}, nil
}

// privateRegistrySubject names what a path below an organization's private-registries addresses: the list, or
// one registry. It names only a secret name of the allowed form.
func privateRegistrySubject(rest []string) string {
	if len(rest) == 0 || rest[0] == "" {
		return "the private registries"
	}
	if name, err := url.PathUnescape(rest[0]); err == nil && checkPrivateRegistryName(name) == nil {
		return "private registry " + name
	}
	return "the private registries"
}
