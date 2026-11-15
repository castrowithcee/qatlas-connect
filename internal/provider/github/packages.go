package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The package tools read the GitHub Packages of a user or an organization and their versions. Every tool
// names an owner as users/LOGIN or orgs/LOGIN, which the connection's targets must allow as an owner target
// itself: an owner reached only through a repository target is not enough. The route follows the kind of the
// owner. The owner is settled before a credential is resolved. No tool reads package content or talks to a
// registry. A package name, a tag, a metadata value, an address, and the repository a package is linked to
// are untrusted data and never fetched; a linked repository outside the targets stays visible as text.

// packageTypes is the fixed list GitHub's routes accept; a value outside it never becomes part of a path.
var packageTypes = []string{"npm", "maven", "rubygems", "docker", "nuget", "container"}

var packageVisibilities = []string{"public", "private", "internal"}

var packageVersionStates = []string{"active", "deleted"}

const (
	packageTypeSchema       = `{"type":"string","enum":["npm","maven","rubygems","docker","nuget","container"]}`
	packageVisibilitySchema = `{"type":"string","enum":["public","private","internal"]}`
	packageStateSchema      = `{"type":"string","enum":["active","deleted"]}`
	packageNameSchema       = `{"type":"string","minLength":1,"maxLength":255}`
)

var packagesOwnerArgument = capability.Argument{Name: "owner", Description: "Owner as users/LOGIN for a user " +
	"or orgs/LOGIN for an organization; optional when the connection's targets name exactly one owner; must " +
	"lie inside the targets when the connection lists any, as an owner target itself, since a repository or a " +
	"project target of the owner is not enough"}

var packagesOwnerField = capability.Field{Name: "owner", Description: "Owner the tool read, as users/LOGIN or " +
	"orgs/LOGIN; the one a default chose when the argument was left out"}

var packageLinkKeys = []string{"url"}

var packageVersionLinkKeys = []string{"url", "package_html_url"}

const packageTagsProperties = `"tags":{"type":"array","maxItems":100,"items":{"type":"string"}},` +
	`"tags_truncated":{"type":"boolean"}`

var packageProperties = `"id":{"type":"integer"},"name":{"type":"string"},"package_type":{"type":"string"},` +
	`"visibility":{"type":"string"},"url":{"type":"string"},"version_count":{"type":"integer"},` +
	`"created_at":{"type":"string"},"updated_at":{"type":"string"},"repository":{"type":"string"},` +
	`"links":` + `{"type":"object","properties":{"url":{"type":"string"}},"additionalProperties":false},` +
	detailProperty("owner", restAccountSchema, false) + `,` +
	detailProperty("repository", restRepositorySchema, false)

const packageRequired = `"required":["id","name"],"additionalProperties":false`

var packageVersionProperties = `"id":{"type":"integer"},"name":{"type":"string"},"url":{"type":"string"},` +
	`"license":{"type":"string"},"description":{"type":"string"},"created_at":{"type":"string"},` +
	`"updated_at":{"type":"string"},"deleted_at":{"type":"string"},"metadata":` +
	objectSchema(`"package_type":{"type":"string"},"container":`+objectSchema(packageTagsProperties)+
		`,"docker":`+objectSchema(`"tag":{"type":"array","maxItems":100,"items":{"type":"string"}},"tag_truncated":{"type":"boolean"}`)) +
	`,"links":` + linksSchema(packageVersionLinkKeys)

const packageVersionRequired = `"required":["id","name"],"additionalProperties":false`

var packageFields = []capability.Field{
	{Name: "id", Description: "Numeric package id"},
	{Name: "name", Description: "Package name, untrusted data; the value package_name takes"},
	{Name: "package_type", Description: "npm, maven, rubygems, docker, nuget, or container; untrusted data"},
	{Name: "visibility", Description: "public or private, untrusted data"},
	{Name: "url", Description: "Web address of the package, untrusted data, never fetched"},
	{Name: "version_count", Description: "Number of versions of the package"},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "repository", Description: "Full name OWNER/REPO of the repository the package is linked to, as " +
		"text, untrusted data; the repository may lie outside the connection's targets, and nothing is read " +
		"from it"},
	{Name: "links", Description: "REST url of the package, untrusted data, never fetched"},
	{Name: "owner_detail", Description: "Account that owns the package: " + restAccountMembers},
	detailField("repository", "Repository the package is linked to, with the members of GitHub's minimal "+
		"repository; a repository outside the connection's targets keeps only id, node_id, and full_name; the "+
		"repository", false),
}

var packageVersionFields = []capability.Field{
	{Name: "id", Description: "Numeric version id; the value package_version_id takes"},
	{Name: "name", Description: "Version name, such as a tag or a digest, untrusted data"},
	{Name: "url", Description: "Web address of the version, untrusted data, never fetched"},
	{Name: "license", Description: "License of the version, untrusted data"},
	{Name: "description", Description: "Description of the version, untrusted data"},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "deleted_at", Description: "Deletion time of a deleted version; absent for an active one"},
	{Name: "metadata", Description: "package_type, and for a container or Docker version its tags, at most " +
		"100 each with a truncated marker; untrusted data"},
	{Name: "links", Description: "REST url and package_html_url of the version, untrusted data, never fetched"},
}

// packageDefaultFields leaves the two detail objects out of a package read unless the caller asks for them.
var packageDefaultFields = []string{"id", "name", "package_type", "visibility", "url", "version_count",
	"created_at", "updated_at", "repository", "links", "owner"}

var packageTypeArgument = capability.Argument{Name: "package_type", Required: true,
	Description: "npm, maven, rubygems, docker, nuget, or container; Docker images in the Container registry " +
		"are container, and Gradle packages are maven"}

var packageNameArgument = capability.Argument{Name: "package_name", Required: true,
	Description: "Name of the package as github.packages.list reports it, 1 to 255 characters; sent as one " +
		"URL-encoded path segment"}

var packagesList = capability.Descriptor{
	ID:      Provider + ".packages.list",
	Version: 1,
	Title:   "List GitHub packages",
	Description: "List one bounded batch of the packages of one type that a user or an organization " +
		"owns, that a connection allows, in the order GitHub returns them, with visibility, version count, the " +
		"linked repository as text, and the members of GitHub's package",
	Tags:     []string{"github", "packages", "list"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"package_type":`+packageTypeSchema+`,"visibility":`+packageVisibilitySchema+`,`+
		pagingKeys, "package_type"),
	OutputSchema: listOutput("packages", packageProperties, packageRequired),
	Arguments: append([]capability.Argument{packageTypeArgument,
		{Name: "visibility", Description: "public, private, or internal; every visibility when omitted; internal " +
			"counts as private for a registry without granular permissions"}}, pagingArguments...),
	Fields: append(append([]capability.Field{{Name: "packages", Description: "Packages of the owner, with the " +
		"members described below"}}, packageFields...), pagingFields...),
	Examples: []capability.Example{{
		Description: "List the container packages of an organization",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","package_type":"container"}`),
	}},
}

var packagesGet = capability.Descriptor{
	ID:            Provider + ".packages.get",
	Version:       1,
	Title:         "Get a GitHub package",
	Description:   "Read one package of a user or an organization a connection allows by its type and name",
	Tags:          []string{"github", "packages", "get"},
	Risk:          readRisk,
	Provider:      Provider,
	InputSchema:   inputSchema(`"package_type":`+packageTypeSchema+`,"package_name":`+packageNameSchema, "package_type", "package_name"),
	OutputSchema:  json.RawMessage(`{"type":"object","properties":{` + packageProperties + `},` + packageRequired + `}`),
	Arguments:     []capability.Argument{packageTypeArgument, packageNameArgument},
	Fields:        packageFields,
	DefaultFields: packageDefaultFields,
	Examples: []capability.Example{{
		Description: "Read one package",
		Arguments:   json.RawMessage(`{"owner":"users/octocat","package_type":"npm","package_name":"left-pad"}`),
	}},
}

var packageVersionsList = capability.Descriptor{
	ID:      Provider + ".package-versions.list",
	Version: 1,
	Title:   "List GitHub package versions",
	Description: "List one bounded batch of the versions of one package of a user or an organization a " +
		"connection allows, active ones unless state says deleted, with name, metadata, and the members of " +
		"GitHub's package version",
	Tags:     []string{"github", "packages", "versions", "list"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"package_type":`+packageTypeSchema+`,"package_name":`+packageNameSchema+
		`,"state":`+packageStateSchema+`,`+pagingKeys, "package_type", "package_name"),
	OutputSchema: listOutput("versions", packageVersionProperties, packageVersionRequired),
	Arguments: append([]capability.Argument{packageTypeArgument, packageNameArgument,
		{Name: "state", Description: "active or deleted; active when omitted"}}, pagingArguments...),
	Fields: append(append([]capability.Field{{Name: "versions", Description: "Versions of the package, with " +
		"the members described below"}}, packageVersionFields...), pagingFields...),
	Examples: []capability.Example{{
		Description: "List the versions of a package",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","package_type":"container","package_name":"app"}`),
	}},
}

var packageVersionsGet = capability.Descriptor{
	ID:      Provider + ".package-versions.get",
	Version: 1,
	Title:   "Get a GitHub package version",
	Description: "Read one version of a package of a user or an organization a connection allows by its " +
		"numeric id",
	Tags:     []string{"github", "packages", "versions", "get"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"package_type":`+packageTypeSchema+`,"package_name":`+packageNameSchema+
		`,"package_version_id":`+actionsIDSchema, "package_type", "package_name", "package_version_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + packageVersionProperties + `},` +
		packageVersionRequired + `}`),
	Arguments: []capability.Argument{packageTypeArgument, packageNameArgument,
		{Name: "package_version_id", Required: true, Description: "Numeric id of the version as " +
			"github.package-versions.list reports it"}},
	Fields: packageVersionFields,
	Examples: []capability.Example{{
		Description: "Read one version",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org","package_type":"container","package_name":"app",` +
			`"package_version_id":786068}`),
	}},
}

// packageReadToolIDs are the package tools that only read.
var packageReadToolIDs = []string{packagesList.ID, packagesGet.ID, packageVersionsList.ID, packageVersionsGet.ID}

// packageChangeToolIDs are the package tools that delete or restore; each is offered only where a connection's
// tools list names it.
var packageChangeToolIDs = []string{packagesDelete.ID, packagesRestore.ID, packageVersionsDelete.ID,
	packageVersionsRestore.ID}

// packageToolIDs are the package tools; they take an owner as their target.
var packageToolIDs = append(slices.Clone(packageReadToolIDs), packageChangeToolIDs...)

func packagesOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: packagesList, Handler: packagesHandler(packagesList.ID, checkPackagesList,
			func(ctx context.Context, c *Client, a *packagesArguments) (any, error) { return c.listPackages(ctx, a) })},
		{Descriptor: packagesGet, Handler: packagesHandler(packagesGet.ID, checkPackageName,
			func(ctx context.Context, c *Client, a *packagesArguments) (any, error) { return c.getPackage(ctx, a) })},
		{Descriptor: packageVersionsList, Handler: packagesHandler(packageVersionsList.ID, checkPackageVersionsList,
			func(ctx context.Context, c *Client, a *packagesArguments) (any, error) {
				return c.listPackageVersions(ctx, a)
			})},
		{Descriptor: packageVersionsGet, Handler: packagesHandler(packageVersionsGet.ID, checkPackageVersionID,
			func(ctx context.Context, c *Client, a *packagesArguments) (any, error) {
				return c.getPackageVersion(ctx, a)
			})},
		{Descriptor: packagesDelete, Handler: packagesHandler(packagesDelete.ID, checkPackageName,
			func(ctx context.Context, c *Client, a *packagesArguments) (any, error) {
				return c.deletePackage(ctx, a)
			})},
		{Descriptor: packagesRestore, Handler: packagesHandler(packagesRestore.ID, checkPackageName,
			func(ctx context.Context, c *Client, a *packagesArguments) (any, error) {
				return c.restorePackage(ctx, a)
			})},
		{Descriptor: packageVersionsDelete, Handler: packagesHandler(packageVersionsDelete.ID, checkPackageVersionID,
			func(ctx context.Context, c *Client, a *packagesArguments) (any, error) {
				return c.deletePackageVersion(ctx, a)
			})},
		{Descriptor: packageVersionsRestore, Handler: packagesHandler(packageVersionsRestore.ID, checkPackageVersionID,
			func(ctx context.Context, c *Client, a *packagesArguments) (any, error) {
				return c.restorePackageVersion(ctx, a)
			})},
	}
}

// packagesArguments holds the arguments of every package tool; the input schema of each tool admits only its
// own. page, perPage, and binding are derived by the checks.
type packagesArguments struct {
	PackageType      string `json:"package_type"`
	PackageName      string `json:"package_name"`
	PackageVersionID int64  `json:"package_version_id"`
	Visibility       string `json:"visibility"`
	State            string `json:"state"`
	Limit            int    `json:"limit"`
	Cursor           string `json:"cursor"`

	page, perPage int
	binding       []byte
}

// packagesHandler decodes and checks the arguments and resolves the owner before a credential is resolved, so
// a refused request never becomes a provider call. It requires the owner as a target itself, which keeps an
// owner reached only through a repository target out.
func packagesHandler(id string, check func(*packagesArguments, target) error,
	call func(context.Context, *Client, *packagesArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments packagesArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOwner(resolved, kindOwner, raw)
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

func checkPackageType(value string) error {
	for _, known := range packageTypes {
		if value == known {
			return nil
		}
	}
	return invalidRequest("package_type must be npm, maven, rubygems, docker, nuget, or container")
}

// checkPackageNameValue refuses what cannot be one path segment of a package. Escaping already keeps a name
// in its segment, but a dot segment would still address the parent route.
func checkPackageNameValue(value string) error {
	if value == "" || len(value) > 255 {
		return invalidRequest("package_name must be 1 to 255 bytes")
	}
	if value == "." || value == ".." {
		return invalidRequest("package_name must not be a dot segment")
	}
	return checkText("package_name", value)
}

func (a *packagesArguments) startPage(bound target, filters ...any) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	parts := append([]any{"packages", bound.String()}, filters...)
	a.binding = fingerprint(parts...)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkPackagesList(a *packagesArguments, bound target) error {
	if err := checkPackageType(a.PackageType); err != nil {
		return err
	}
	if a.Visibility != "" && !containsExact(packageVisibilities, a.Visibility) {
		return invalidRequest("visibility must be public, private, or internal")
	}
	return a.startPage(bound, "list", a.PackageType, a.Visibility)
}

func checkPackageName(a *packagesArguments, _ target) error {
	if err := checkPackageType(a.PackageType); err != nil {
		return err
	}
	return checkPackageNameValue(a.PackageName)
}

func checkPackageVersionsList(a *packagesArguments, bound target) error {
	if err := checkPackageName(a, bound); err != nil {
		return err
	}
	if a.State != "" && !containsExact(packageVersionStates, a.State) {
		return invalidRequest("state must be active or deleted")
	}
	return a.startPage(bound, "versions", a.PackageType, a.PackageName, a.State)
}

func checkPackageVersionID(a *packagesArguments, bound target) error {
	if err := checkPackageName(a, bound); err != nil {
		return err
	}
	if a.PackageVersionID < 1 || a.PackageVersionID > 9007199254740991 {
		return invalidRequest("package_version_id must be a positive number")
	}
	return nil
}

// Permission message of the package reads. GitHub decides on every request; the message names what such a
// request needs without claiming what the configured token holds. GitHub documents no fine-grained token
// permission for these routes.
const packagesReadPermission = "GitHub refused this token the packages of this owner; reading them needs " +
	"read:packages on a classic token, and GitHub documents no fine-grained token permission for these routes"

// packagesPath is the REST route below the packages of the bound owner, which a user and an organization
// each have under their own prefix. Delete and restore build on it as well.
func (c *Client) packagesPath(rest string) string {
	return "/" + c.target.scope + "/" + url.PathEscape(c.target.owner) + "/packages" + rest
}

// packagePath is the route of one package. The type comes from the fixed list and the name is escaped as one
// opaque path segment, so neither can address another route.
func (c *Client) packagePath(packageType, name string) string {
	return c.packagesPath("/" + url.PathEscape(packageType) + "/" + url.PathEscape(name))
}

// packageVersionPath is the route of one version of a package, or of its versions without an id.
func (c *Client) packageVersionPath(packageType, name string, id int64) string {
	path := c.packagePath(packageType, name) + "/versions"
	if id == 0 {
		return path
	}
	return path + "/" + strconv.FormatInt(id, 10)
}

// packageRaw is the REST package; links holds its REST url.
type packageRaw struct {
	ID           *int64         `json:"id"`
	Name         *string        `json:"name"`
	PackageType  *string        `json:"package_type"`
	Visibility   *string        `json:"visibility"`
	HTMLURL      *string        `json:"html_url"`
	VersionCount *int64         `json:"version_count"`
	Owner        *accountRaw    `json:"owner"`
	Repository   *repositoryRaw `json:"repository"`
	CreatedAt    *string        `json:"created_at"`
	UpdatedAt    *string        `json:"updated_at"`
	links        Links
}

func (r *packageRaw) UnmarshalJSON(data []byte) (err error) {
	type plain packageRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, packageLinkKeys)
	return err
}

// Package is the view of one package. OwnerDetail and RepositoryDetail carry GitHub's owner and repository
// objects; Repository is the text of the linked repository's full name.
type Package struct {
	ID               *int64          `json:"id,omitempty"`
	Name             string          `json:"name"`
	PackageType      *string         `json:"package_type,omitempty"`
	Visibility       *string         `json:"visibility,omitempty"`
	URL              *string         `json:"url,omitempty"`
	VersionCount     *int64          `json:"version_count,omitempty"`
	CreatedAt        *string         `json:"created_at,omitempty"`
	UpdatedAt        *string         `json:"updated_at,omitempty"`
	Repository       *string         `json:"repository,omitempty"`
	Links            Links           `json:"links,omitempty"`
	OwnerDetail      *RESTAccount    `json:"owner_detail,omitempty"`
	RepositoryDetail *RESTRepository `json:"repository_detail,omitempty"`
}

func (r *packageRaw) project(allowed allowlist) (Package, bool) {
	name := longText(filled(r.Name))
	if r.ID == nil || name == nil {
		return Package{}, false
	}
	out := Package{ID: r.ID, Name: *name, PackageType: shortText(r.PackageType), Visibility: shortText(r.Visibility),
		URL: address(r.HTMLURL), VersionCount: r.VersionCount, CreatedAt: shortText(r.CreatedAt),
		UpdatedAt: shortText(r.UpdatedAt), Links: r.links, OwnerDetail: r.Owner.project()}
	if r.Repository != nil {
		out.RepositoryDetail = r.Repository.project(allowed)
		out.Repository = out.RepositoryDetail.FullName
	}
	return out, true
}

// PackageList is one batch of packages.
type PackageList struct {
	Packages   []Package `json:"packages"`
	NextCursor string    `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more"`
}

// PackageVersionMetadata is the metadata of a package version.
type PackageVersionMetadata struct {
	PackageType *string            `json:"package_type,omitempty"`
	Container   *PackageTags       `json:"container,omitempty"`
	Docker      *PackageDockerTags `json:"docker,omitempty"`
}

// PackageTags are the tags of a container version, at most 100.
type PackageTags struct {
	Tags          []string `json:"tags,omitempty"`
	TagsTruncated bool     `json:"tags_truncated,omitempty"`
}

// PackageDockerTags are the tags of a Docker version, at most 100, under the member name GitHub documents.
type PackageDockerTags struct {
	Tag          []string `json:"tag,omitempty"`
	TagTruncated bool     `json:"tag_truncated,omitempty"`
}

// PackageVersion is the view of one package version.
type PackageVersion struct {
	ID          *int64                  `json:"id,omitempty"`
	Name        string                  `json:"name"`
	URL         *string                 `json:"url,omitempty"`
	License     *string                 `json:"license,omitempty"`
	Description *string                 `json:"description,omitempty"`
	CreatedAt   *string                 `json:"created_at,omitempty"`
	UpdatedAt   *string                 `json:"updated_at,omitempty"`
	DeletedAt   *string                 `json:"deleted_at,omitempty"`
	Metadata    *PackageVersionMetadata `json:"metadata,omitempty"`
	Links       Links                   `json:"links,omitempty"`
}

// packageVersionRaw is the REST package version; links holds its REST url and the web address of its package.
type packageVersionRaw struct {
	ID          *int64  `json:"id"`
	Name        *string `json:"name"`
	HTMLURL     *string `json:"html_url"`
	License     *string `json:"license"`
	Description *string `json:"description"`
	CreatedAt   *string `json:"created_at"`
	UpdatedAt   *string `json:"updated_at"`
	DeletedAt   *string `json:"deleted_at"`
	Metadata    *struct {
		PackageType *string `json:"package_type"`
		Container   *struct {
			Tags []*string `json:"tags"`
		} `json:"container"`
		Docker *struct {
			Tag []*string `json:"tag"`
		} `json:"docker"`
	} `json:"metadata"`
	links Links
}

func (r *packageVersionRaw) UnmarshalJSON(data []byte) (err error) {
	type plain packageVersionRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, packageVersionLinkKeys)
	return err
}

// cleanTags cleans untrusted tags and keeps at most projectionListLimit of them.
func cleanTags(tags []*string) ([]string, bool) {
	var clean []string
	for _, tag := range tags {
		if text := nameText(tag); text != nil {
			clean = append(clean, *text)
		}
	}
	return capped(clean)
}

func (r *packageVersionRaw) project() (PackageVersion, bool) {
	name := longText(filled(r.Name))
	if r.ID == nil || name == nil {
		return PackageVersion{}, false
	}
	out := PackageVersion{ID: r.ID, Name: *name, URL: address(r.HTMLURL), License: shortText(r.License),
		Description: bulkText(r.Description), CreatedAt: shortText(r.CreatedAt), UpdatedAt: shortText(r.UpdatedAt),
		DeletedAt: shortText(r.DeletedAt), Links: r.links}
	if m := r.Metadata; m != nil {
		meta := &PackageVersionMetadata{PackageType: shortText(m.PackageType)}
		if m.Container != nil {
			tags, truncated := cleanTags(m.Container.Tags)
			meta.Container = &PackageTags{Tags: tags, TagsTruncated: truncated}
		}
		if m.Docker != nil {
			tags, truncated := cleanTags(m.Docker.Tag)
			meta.Docker = &PackageDockerTags{Tag: tags, TagTruncated: truncated}
		}
		out.Metadata = meta
	}
	return out, true
}

func (a *packagesArguments) query(extra url.Values) url.Values {
	values := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	for key, value := range extra {
		values[key] = value
	}
	return values
}

func (c *Client) listPackages(ctx context.Context, a *packagesArguments) (*PackageList, error) {
	const op = "list packages"
	query := a.query(url.Values{"package_type": {a.PackageType}})
	if a.Visibility != "" {
		query.Set("visibility", a.Visibility)
	}
	var raw []packageRaw
	hasNext, err := c.restPage(ctx, op, c.packagesPath(""), query, &raw)
	if err != nil {
		return nil, actionsFailure(err, packagesReadPermission)
	}
	result := &PackageList{Packages: make([]Package, 0, len(raw))}
	for i := range raw {
		entry, ok := raw[i].project(c.allowed)
		if !ok {
			return nil, invalidEntry(op, "a package")
		}
		result.Packages = append(result.Packages, entry)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) getPackage(ctx context.Context, a *packagesArguments) (*Package, error) {
	const op = "get package"
	var raw packageRaw
	if err := c.rest(ctx, op, c.packagePath(a.PackageType, a.PackageName), &raw); err != nil {
		return nil, actionsFailure(err, packagesReadPermission)
	}
	entry, ok := raw.project(c.allowed)
	if !ok {
		return nil, invalidEntry(op, "a package")
	}
	return &entry, nil
}

func (c *Client) listPackageVersions(ctx context.Context, a *packagesArguments) (*PackageVersionList, error) {
	const op = "list package versions"
	query := a.query(nil)
	if a.State != "" {
		query.Set("state", a.State)
	}
	var raw []packageVersionRaw
	hasNext, err := c.restPage(ctx, op, c.packageVersionPath(a.PackageType, a.PackageName, 0), query, &raw)
	if err != nil {
		return nil, actionsFailure(err, packagesReadPermission)
	}
	result := &PackageVersionList{Versions: make([]PackageVersion, 0, len(raw))}
	for i := range raw {
		entry, ok := raw[i].project()
		if !ok {
			return nil, invalidEntry(op, "a package version")
		}
		result.Versions = append(result.Versions, entry)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) getPackageVersion(ctx context.Context, a *packagesArguments) (*PackageVersion, error) {
	const op = "get package version"
	var raw packageVersionRaw
	if err := c.rest(ctx, op, c.packageVersionPath(a.PackageType, a.PackageName, a.PackageVersionID), &raw); err != nil {
		return nil, actionsFailure(err, packagesReadPermission)
	}
	entry, ok := raw.project()
	if !ok {
		return nil, invalidEntry(op, "a package version")
	}
	return &entry, nil
}

// PackageVersionList is one batch of package versions.
type PackageVersionList struct {
	Versions   []PackageVersion `json:"versions"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
}

// packagesSubject names what a path below /orgs/OWNER/packages or /users/LOGIN/packages addresses. It names
// the packages of the owner only, never a package name or a version, since those are untrusted data.
func packagesSubject(scope, owner string) subject {
	return subject{what: "the packages of " + scope + "/" + owner}
}

const (
	packageChangedProperties = `"package_type":{"type":"string"},"package_name":{"type":"string"}`
	packageDeletedOutput     = `{"type":"object","properties":{"deleted":{"type":"boolean"},` +
		packageChangedProperties + `},"required":["deleted","package_type","package_name"],"additionalProperties":false}`
	packageRestoredOutput = `{"type":"object","properties":{"restored":{"type":"boolean"},` +
		packageChangedProperties + `},"required":["restored","package_type","package_name"],"additionalProperties":false}`
	packageVersionDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},` +
		packageChangedProperties + `,"package_version_id":{"type":"integer"}},` +
		`"required":["deleted","package_type","package_name","package_version_id"],"additionalProperties":false}`
	packageVersionRestoredOutput = `{"type":"object","properties":{"restored":{"type":"boolean"},` +
		packageChangedProperties + `,"package_version_id":{"type":"integer"}},` +
		`"required":["restored","package_type","package_name","package_version_id"],"additionalProperties":false}`
)

var packageChangedFields = []capability.Field{
	{Name: "package_type", Description: "Package type of the request"},
	{Name: "package_name", Description: "Package name of the request, as given"},
}

var packageVersionIDArgument = capability.Argument{Name: "package_version_id", Required: true,
	Description: "Numeric id of the version as github.package-versions.list reports it"}

var packagesDelete = capability.Descriptor{
	ID:      Provider + ".packages.delete",
	Version: 1,
	Title:   "Delete a GitHub package",
	Description: "Delete one package of a user or an organization a connection allows with all of its versions. " +
		"GitHub keeps a deleted package restorable for 30 days, if its namespace is not taken by a new package " +
		"meanwhile; GitHub refuses a public package with a version of more than 5,000 downloads. Offered only " +
		"by a connection whose tools list names it",
	Tags:                  []string{"github", "packages", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"package_type":`+packageTypeSchema+`,"package_name":`+packageNameSchema, "package_type", "package_name"),
	OutputSchema:          json.RawMessage(packageDeletedOutput),
	Arguments:             []capability.Argument{packageTypeArgument, packageNameArgument},
	Fields: append([]capability.Field{{Name: "deleted", Description: "True once GitHub deleted the package"}},
		packageChangedFields...),
	Examples: []capability.Example{{
		Description: "Delete a package",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","package_type":"npm","package_name":"left-pad"}`),
	}},
}

var packagesRestore = capability.Descriptor{
	ID:      Provider + ".packages.restore",
	Version: 1,
	Title:   "Restore a GitHub package",
	Description: "Restore one deleted package of a user or an organization a connection allows with its versions. " +
		"GitHub restores a package deleted within the last 30 days, if its namespace is not taken by a new " +
		"package. The tool offers no token argument. Offered only by a connection whose tools list names it",
	Tags:                  []string{"github", "packages", "restore", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyUnknown, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"package_type":`+packageTypeSchema+`,"package_name":`+packageNameSchema, "package_type", "package_name"),
	OutputSchema:          json.RawMessage(packageRestoredOutput),
	Arguments:             []capability.Argument{packageTypeArgument, packageNameArgument},
	Fields: append([]capability.Field{{Name: "restored", Description: "True once GitHub restored the package"}},
		packageChangedFields...),
	Examples: []capability.Example{{
		Description: "Restore a deleted package",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","package_type":"npm","package_name":"left-pad"}`),
	}},
}

var packageVersionsDelete = capability.Descriptor{
	ID:      Provider + ".package-versions.delete",
	Version: 1,
	Title:   "Delete a GitHub package version",
	Description: "Delete one version of a package of a user or an organization a connection allows. GitHub keeps " +
		"a deleted version restorable for 30 days, if its namespace is not taken by a new package meanwhile; " +
		"GitHub refuses the last version of a package and a public version of more than 5,000 downloads. " +
		"Offered only by a connection whose tools list names it",
	Tags:                  []string{"github", "packages", "versions", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"package_type":`+packageTypeSchema+`,"package_name":`+packageNameSchema+
		`,"package_version_id":`+actionsIDSchema, "package_type", "package_name", "package_version_id"),
	OutputSchema: json.RawMessage(packageVersionDeletedOutput),
	Arguments:    []capability.Argument{packageTypeArgument, packageNameArgument, packageVersionIDArgument},
	Fields: append([]capability.Field{{Name: "deleted", Description: "True once GitHub deleted the version"},
		{Name: "package_version_id", Description: "Version id of the request"}}, packageChangedFields...),
	Examples: []capability.Example{{
		Description: "Delete one version",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org","package_type":"container","package_name":"app",` +
			`"package_version_id":786068}`),
	}},
}

var packageVersionsRestore = capability.Descriptor{
	ID:      Provider + ".package-versions.restore",
	Version: 1,
	Title:   "Restore a GitHub package version",
	Description: "Restore one deleted version of a package of a user or an organization a connection allows. " +
		"GitHub restores a version deleted within the last 30 days, if its namespace is not taken by a new " +
		"package. Offered only by a connection whose tools list names it",
	Tags:                  []string{"github", "packages", "versions", "restore", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyUnknown, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"package_type":`+packageTypeSchema+`,"package_name":`+packageNameSchema+
		`,"package_version_id":`+actionsIDSchema, "package_type", "package_name", "package_version_id"),
	OutputSchema: json.RawMessage(packageVersionRestoredOutput),
	Arguments:    []capability.Argument{packageTypeArgument, packageNameArgument, packageVersionIDArgument},
	Fields: append([]capability.Field{{Name: "restored", Description: "True once GitHub restored the version"},
		{Name: "package_version_id", Description: "Version id of the request"}}, packageChangedFields...),
	Examples: []capability.Example{{
		Description: "Restore one deleted version",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org","package_type":"container","package_name":"app",` +
			`"package_version_id":786068}`),
	}},
}

// Fixed messages of the package changes. GitHub's text is never quoted; GitHub documents no fine-grained token
// permission for these routes.
const (
	packagesDeletePermission = "GitHub refused this token to delete here; deleting needs read:packages and " +
		"delete:packages on a classic token and admin rights on the owner or the package, GitHub documents no " +
		"fine-grained token permission for these routes, and GitHub also refuses the last version of a package " +
		"and a public package with a version of more than 5,000 downloads; nothing was deleted"
	packagesRestorePermission = "GitHub refused this token to restore here; restoring needs read:packages and " +
		"delete:packages on a classic token and admin rights on the owner or the package, GitHub documents no " +
		"fine-grained token permission for these routes, and the token query argument GitHub may ask for is " +
		"never offered by this tool; nothing was restored"
	packagesDeleteRejected = "GitHub refused to delete it in its current state, for example because it is the " +
		"last version of the package or a public package has a version of more than 5,000 downloads; nothing " +
		"was deleted"
	packagesRestoreRejected = "GitHub refused to restore it, for example because it was deleted more than 30 " +
		"days ago or a new package took its namespace; nothing was restored"
)

// packageChangeFailure names a refused package change in Qatlas' own words. GitHub's 403 is the refusal of a
// missing right, of the last version, and of a package with many downloads alike; a 400 or 422 is a refusal of
// the state. Every other failure, an unclear outcome included, stays as it is.
func packageChangeFailure(err error, permission, rejected string) error {
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Class == provider.ClassProviderError && failure.Message == rejectedMessage {
		refused := *failure
		refused.Message = rejected
		return &refused
	}
	return actionsFailure(err, permission)
}

// changePackage sends one change of a package route, once; the answer is 204 without a body.
func (c *Client) changePackage(ctx context.Context, op, method, path, permission, rejected string) error {
	err := c.restChange(ctx, op, method, path, struct{}{}, nil)
	if err == nil {
		return nil
	}
	return packageChangeFailure(err, permission, rejected)
}

// PackageDeleted reports a deleted package.
type PackageDeleted struct {
	Deleted     bool   `json:"deleted"`
	PackageType string `json:"package_type"`
	PackageName string `json:"package_name"`
}

// PackageRestored reports a restored package.
type PackageRestored struct {
	Restored    bool   `json:"restored"`
	PackageType string `json:"package_type"`
	PackageName string `json:"package_name"`
}

// PackageVersionDeleted reports a deleted package version.
type PackageVersionDeleted struct {
	Deleted          bool   `json:"deleted"`
	PackageType      string `json:"package_type"`
	PackageName      string `json:"package_name"`
	PackageVersionID int64  `json:"package_version_id"`
}

// PackageVersionRestored reports a restored package version.
type PackageVersionRestored struct {
	Restored         bool   `json:"restored"`
	PackageType      string `json:"package_type"`
	PackageName      string `json:"package_name"`
	PackageVersionID int64  `json:"package_version_id"`
}

func (c *Client) deletePackage(ctx context.Context, a *packagesArguments) (*PackageDeleted, error) {
	path := c.packagePath(a.PackageType, a.PackageName)
	if err := c.changePackage(ctx, "delete package", http.MethodDelete, path, packagesDeletePermission,
		packagesDeleteRejected); err != nil {
		return nil, err
	}
	return &PackageDeleted{Deleted: true, PackageType: a.PackageType, PackageName: a.PackageName}, nil
}

func (c *Client) restorePackage(ctx context.Context, a *packagesArguments) (*PackageRestored, error) {
	path := c.packagePath(a.PackageType, a.PackageName) + "/restore"
	if err := c.changePackage(ctx, "restore package", http.MethodPost, path, packagesRestorePermission,
		packagesRestoreRejected); err != nil {
		return nil, err
	}
	return &PackageRestored{Restored: true, PackageType: a.PackageType, PackageName: a.PackageName}, nil
}

func (c *Client) deletePackageVersion(ctx context.Context, a *packagesArguments) (*PackageVersionDeleted, error) {
	path := c.packageVersionPath(a.PackageType, a.PackageName, a.PackageVersionID)
	if err := c.changePackage(ctx, "delete package version", http.MethodDelete, path, packagesDeletePermission,
		packagesDeleteRejected); err != nil {
		return nil, err
	}
	return &PackageVersionDeleted{Deleted: true, PackageType: a.PackageType, PackageName: a.PackageName,
		PackageVersionID: a.PackageVersionID}, nil
}

func (c *Client) restorePackageVersion(ctx context.Context, a *packagesArguments) (*PackageVersionRestored, error) {
	path := c.packageVersionPath(a.PackageType, a.PackageName, a.PackageVersionID) + "/restore"
	if err := c.changePackage(ctx, "restore package version", http.MethodPost, path, packagesRestorePermission,
		packagesRestoreRejected); err != nil {
		return nil, err
	}
	return &PackageVersionRestored{Restored: true, PackageType: a.PackageType, PackageName: a.PackageName,
		PackageVersionID: a.PackageVersionID}, nil
}
