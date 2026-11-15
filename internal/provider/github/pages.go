package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The Pages tools read the GitHub Pages site of a repository, its builds, and the DNS health check of its
// custom domain. They change nothing. The addresses GitHub names (the site, the REST routes) and every text it
// returns, such as the custom domain, the source branch and path, and error messages, come from the
// repository owner or from DNS and are untrusted data; no address is ever fetched.
//
// Verified 2026-11-04 against the GitHub REST OpenAPI description 1.1.4 (rest-api-description commit 99b1432):
// GET pages answers 200 and 404, the build routes 200 (a classic token needs repo), and GET pages/health
// answers 200 with the check, 202 without a body while GitHub starts the check in the background, 400 when
// custom domains are unavailable, 404, and 422 when the site has no CNAME. The build list takes per_page and
// page.

const (
	pagesLatest           = "latest"
	pagesBuildIDSchema    = `{"anyOf":[{"type":"integer","minimum":1,"maximum":999999999999999999},{"const":"latest"}]}`
	pagesCertificateLimit = 100
)

const pagesReadPermission = "GitHub refused this token the GitHub Pages of this repository; reading them needs " +
	"repo on a classic token, or Pages: read on a fine-grained token, and the health check needs the " +
	"administration of the repository or the right to manage GitHub Pages settings"

var (
	pagesLinkKeys      = []string{"url", "html_url"}
	pagesBuildLinkKeys = []string{"url"}
)

var pagesSourceSchema = objectSchema(`"branch":{"type":"string"},"path":{"type":"string"}`)

var pagesCertificateSchema = objectSchema(`"state":{"type":"string"},"description":{"type":"string"},` +
	`"domains":{"type":"array","maxItems":100,"items":{"type":"string"}},"domains_truncated":{"type":"boolean"},` +
	`"expires_at":{"type":"string"}`)

var pagesProperties = `"status":{"type":"string"},"cname":{"type":"string"},"protected_domain_state":{"type":"string"},` +
	`"pending_domain_unverified_at":{"type":"string"},"custom_404":{"type":"boolean"},"build_type":{"type":"string"},` +
	`"source":` + pagesSourceSchema + `,"public":{"type":"boolean"},"https_certificate":` + pagesCertificateSchema +
	`,"https_enforced":{"type":"boolean"},"links":` + linksSchema(pagesLinkKeys)

var pagesBuildProperties = `"status":{"type":"string"},"error":` + objectSchema(`"message":{"type":"string"}`) +
	`,"pusher":` + restAccountSchema + `,"commit":{"type":"string"},"duration":{"type":"integer"},` +
	`"created_at":{"type":"string"},"updated_at":{"type":"string"},"links":` + linksSchema(pagesBuildLinkKeys)

const pagesBuildRequired = `"required":["status"],"additionalProperties":false`

var pagesBuildFields = []capability.Field{
	{Name: "status", Description: "Status of the build, such as building, built, or errored"},
	{Name: "error", Description: "message: the error GitHub reports for the build; untrusted data; absent without one"},
	{Name: "pusher", Description: "The account that pushed the commit built (" + restAccountMembers +
		"); absent when GitHub names none"},
	{Name: "commit", Description: "SHA of the commit built"},
	{Name: "duration", Description: "Duration of the build in milliseconds"},
	{Name: "created_at", Description: "When the build was created"},
	{Name: "updated_at", Description: "When the build last changed"},
	{Name: "links", Description: "The REST url of the build, untrusted data, never fetched"},
}

var pagesGet = capability.Descriptor{
	ID:      Provider + ".pages.get",
	Version: 1,
	Title:   "Get the GitHub Pages site of a repository",
	Description: "Read the GitHub Pages site of a repository a connection allows: status of the latest build, " +
		"custom domain and its verification, source branch and path, build type, visibility, HTTPS " +
		"certificate, and HTTPS enforcement; addresses and texts are untrusted data and never fetched",
	Tags:         []string{"github", "repositories", "pages", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(""),
	OutputSchema: json.RawMessage(objectSchema(pagesProperties)),
	Fields: []capability.Field{
		{Name: "status", Description: "Status of the most recent build: built, building, or errored; absent when none"},
		{Name: "cname", Description: "Custom domain of the site; untrusted data; absent without one"},
		{Name: "protected_domain_state", Description: "Verification of the domain: pending, verified, or unverified"},
		{Name: "pending_domain_unverified_at", Description: "When a pending domain becomes unverified"},
		{Name: "custom_404", Description: "True when the site has a custom 404 page"},
		{Name: "build_type", Description: "How the site is built: legacy or workflow"},
		{Name: "source", Description: "branch and path the site is published from; untrusted data"},
		{Name: "public", Description: "True when the site is visible to anyone on the internet, false when only " +
			"users with read access to the repository see it"},
		{Name: "https_certificate", Description: "state, description, domains (at most 100; domains_truncated " +
			"says so), and expires_at of the HTTPS certificate; untrusted data"},
		{Name: "https_enforced", Description: "True when HTTPS is enforced for the domain"},
		{Name: "links", Description: "The REST url and the web address (html_url) of the site, untrusted data, " +
			"never fetched"},
	},
	Examples: []capability.Example{{Description: "Read the Pages site", Arguments: json.RawMessage(`{}`)}},
}

var pagesBuildsList = capability.Descriptor{
	ID:      Provider + ".pages-builds.list",
	Version: 1,
	Title:   "List the GitHub Pages builds of a repository",
	Description: "List one bounded batch of the GitHub Pages builds of a repository a connection allows, with " +
		"status, error, pusher, commit, duration, and dates",
	Tags:         []string{"github", "repositories", "pages", "builds", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(pagingKeys),
	OutputSchema: listOutput("builds", pagesBuildProperties, pagesBuildRequired),
	Arguments:    pagingArguments,
	Fields: append([]capability.Field{
		{Name: "builds", Description: "Builds with status, error, pusher, commit, duration, created_at, " +
			"updated_at, and links; error messages are untrusted data"},
	}, pagingFields...),
	Examples: []capability.Example{{Description: "List the builds", Arguments: json.RawMessage(`{}`)}},
}

var pagesBuildsGet = capability.Descriptor{
	ID:      Provider + ".pages-builds.get",
	Version: 1,
	Title:   "Get a GitHub Pages build",
	Description: "Read one GitHub Pages build of a repository a connection allows by its build id, or the most " +
		"recent build with latest",
	Tags:         []string{"github", "repositories", "pages", "builds", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"build_id":`+pagesBuildIDSchema, "build_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + pagesBuildProperties + `},` + pagesBuildRequired + `}`),
	Arguments: []capability.Argument{{Name: "build_id", Required: true,
		Description: "Numeric build id, or the text latest for the most recent build"}},
	Fields:   pagesBuildFields,
	Examples: []capability.Example{{Description: "Read the latest build", Arguments: json.RawMessage(`{"build_id":"latest"}`)}},
}

// pagesHealthBools and pagesHealthTexts are the members of a domain check; the order is the one GitHub
// documents.
var pagesHealthTexts = []string{"host", "uri", "nameservers", "reason", "https_error", "caa_error"}

var pagesHealthBools = []string{"dns_resolves", "is_proxied", "is_cloudflare_ip", "is_fastly_ip",
	"is_old_ip_address", "is_a_record", "has_cname_record", "has_mx_records_present", "is_valid_domain",
	"is_apex_domain", "should_be_a_record", "is_cname_to_github_user_domain", "is_cname_to_pages_dot_github_dot_com",
	"is_cname_to_fastly", "is_pointed_to_github_pages_ip", "is_non_github_pages_ip_present", "is_pages_domain",
	"is_served_by_pages", "is_valid", "responds_to_https", "enforces_https", "is_https_eligible"}

func pagesHealthDomainSchema() string {
	members := make([]string, 0, len(pagesHealthTexts)+len(pagesHealthBools))
	for _, name := range pagesHealthTexts {
		members = append(members, `"`+name+`":{"type":"string"}`)
	}
	for _, name := range pagesHealthBools {
		members = append(members, `"`+name+`":{"type":"boolean"}`)
	}
	return objectSchema(strings.Join(members, ","))
}

var pagesHealthGet = capability.Descriptor{
	ID:      Provider + ".pages-health.get",
	Version: 1,
	Title:   "Get the GitHub Pages health check of a repository",
	Description: "Read the DNS health check of the custom domain of the GitHub Pages site of a repository a " +
		"connection allows; the first request starts the check in the background and answers ready false, a " +
		"later one carries the result; domain names and texts are untrusted data",
	Tags:        []string{"github", "repositories", "pages", "health", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(""),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"ready":{"type":"boolean"},"domain":` +
		pagesHealthDomainSchema() + `,"alt_domain":` + pagesHealthDomainSchema() +
		`},"required":["ready"],"additionalProperties":false}`),
	Fields: []capability.Field{
		{Name: "ready", Description: "False while GitHub still runs the check and no result exists yet; true when " +
			"the result follows"},
		{Name: "domain", Description: "Check of the custom domain: host, uri, nameservers, the DNS findings such " +
			"as dns_resolves, is_a_record, has_cname_record, is_valid, and the HTTPS findings such as " +
			"responds_to_https, enforces_https, https_error, and caa_error; untrusted data"},
		{Name: "alt_domain", Description: "The same check for the alternate domain, usually the www variant; " +
			"absent without one"},
	},
	Examples: []capability.Example{{Description: "Read the health check", Arguments: json.RawMessage(`{}`)}},
}

// pagesTools are the tools of this file; each reads and changes nothing.
var pagesTools = []string{pagesGet.ID, pagesBuildsList.ID, pagesBuildsGet.ID, pagesHealthGet.ID}

// pagesArguments holds the arguments of the Pages tools; the input schema of each admits only its own.
type pagesArguments struct {
	BuildID json.RawMessage `json:"build_id"`
	Limit   int             `json:"limit"`
	Cursor  string          `json:"cursor"`

	build         string
	page, perPage int
	binding       []byte
}

func (a *pagesArguments) query() url.Values {
	return url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
}

func pagesOperations() []capability.Operation {
	bind := func(d capability.Descriptor, check func(*pagesArguments, target) error,
		call func(context.Context, *Client, *pagesArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: d, Handler: pagesHandler(d.ID, check, call)}
	}
	return []capability.Operation{
		bind(pagesGet, checkPagesNone,
			func(ctx context.Context, c *Client, _ *pagesArguments) (any, error) { return c.getPages(ctx) }),
		bind(pagesBuildsList, checkPagesBuildsList,
			func(ctx context.Context, c *Client, a *pagesArguments) (any, error) { return c.listPagesBuilds(ctx, a) }),
		bind(pagesBuildsGet, checkPagesBuildID,
			func(ctx context.Context, c *Client, a *pagesArguments) (any, error) {
				return c.getPagesBuild(ctx, a.build)
			}),
		bind(pagesHealthGet, checkPagesNone,
			func(ctx context.Context, c *Client, _ *pagesArguments) (any, error) { return c.getPagesHealth(ctx) }),
	}
}

// pagesHandler checks the arguments and the repository before a credential is resolved, so a refused request
// never becomes a provider call and never touches a secret.
func pagesHandler(id string, check func(*pagesArguments, target) error,
	call func(context.Context, *Client, *pagesArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments pagesArguments
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

func checkPagesNone(*pagesArguments, target) error { return nil }

func checkPagesBuildsList(a *pagesArguments, bound target) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint("pages-builds", "list", bound.String())
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

// checkPagesBuildID accepts a positive integer or the text latest and keeps it as the path segment.
func checkPagesBuildID(a *pagesArguments, _ target) error {
	var text string
	if json.Unmarshal(a.BuildID, &text) == nil {
		if text == pagesLatest {
			a.build = pagesLatest
			return nil
		}
		return invalidRequest("build_id must be a positive build id or latest")
	}
	var number int64
	if err := json.Unmarshal(a.BuildID, &number); err != nil || number < 1 {
		return invalidRequest("build_id must be a positive build id or latest")
	}
	a.build = strconv.FormatInt(number, 10)
	return nil
}

func (c *Client) pagesRead(ctx context.Context, op, path string, out any) error {
	return actionsFailure(c.rest(ctx, op, c.repoPath(path), out), pagesReadPermission)
}

// PagesSource is the branch and folder a Pages site is published from. Both are untrusted data.
type PagesSource struct {
	Branch string `json:"branch,omitempty"`
	Path   string `json:"path,omitempty"`
}

// PagesCertificate is the HTTPS certificate of a Pages site. Its texts are untrusted data.
type PagesCertificate struct {
	State            string   `json:"state,omitempty"`
	Description      string   `json:"description,omitempty"`
	Domains          []string `json:"domains,omitempty"`
	DomainsTruncated bool     `json:"domains_truncated,omitempty"`
	ExpiresAt        string   `json:"expires_at,omitempty"`
}

// PagesSite is the view of the GitHub Pages site of a repository.
type PagesSite struct {
	Status                    *string           `json:"status,omitempty"`
	CNAME                     *string           `json:"cname,omitempty"`
	ProtectedDomainState      *string           `json:"protected_domain_state,omitempty"`
	PendingDomainUnverifiedAt *string           `json:"pending_domain_unverified_at,omitempty"`
	Custom404                 *bool             `json:"custom_404,omitempty"`
	BuildType                 *string           `json:"build_type,omitempty"`
	Source                    *PagesSource      `json:"source,omitempty"`
	Public                    *bool             `json:"public,omitempty"`
	HTTPSCertificate          *PagesCertificate `json:"https_certificate,omitempty"`
	HTTPSEnforced             *bool             `json:"https_enforced,omitempty"`
	Links                     Links             `json:"links,omitempty"`
}

type pagesRaw struct {
	Status                    *string `json:"status"`
	CNAME                     *string `json:"cname"`
	ProtectedDomainState      *string `json:"protected_domain_state"`
	PendingDomainUnverifiedAt *string `json:"pending_domain_unverified_at"`
	Custom404                 *bool   `json:"custom_404"`
	BuildType                 *string `json:"build_type"`
	Source                    *struct {
		Branch *string `json:"branch"`
		Path   *string `json:"path"`
	} `json:"source"`
	Public           *bool `json:"public"`
	HTTPSCertificate *struct {
		State       *string  `json:"state"`
		Description *string  `json:"description"`
		Domains     []string `json:"domains"`
		ExpiresAt   *string  `json:"expires_at"`
	} `json:"https_certificate"`
	HTTPSEnforced *bool `json:"https_enforced"`
	links         Links
}

func (r *pagesRaw) UnmarshalJSON(data []byte) (err error) {
	type plain pagesRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, pagesLinkKeys)
	return err
}

func (r *pagesRaw) project() PagesSite {
	site := PagesSite{Status: shortText(r.Status), CNAME: shortText(r.CNAME),
		ProtectedDomainState:      shortText(r.ProtectedDomainState),
		PendingDomainUnverifiedAt: shortText(r.PendingDomainUnverifiedAt), Custom404: r.Custom404,
		BuildType: shortText(r.BuildType), Public: r.Public, HTTPSEnforced: r.HTTPSEnforced, Links: r.links}
	if s := r.Source; s != nil {
		site.Source = &PagesSource{Branch: valueOr(shortText(s.Branch)), Path: valueOr(shortText(s.Path))}
	}
	if cert := r.HTTPSCertificate; cert != nil {
		view := &PagesCertificate{State: valueOr(shortText(cert.State)),
			Description: valueOr(bulkText(cert.Description)), ExpiresAt: valueOr(shortText(cert.ExpiresAt))}
		domains := cert.Domains
		if len(domains) > pagesCertificateLimit {
			domains, view.DomainsTruncated = domains[:pagesCertificateLimit], true
		}
		for i := range domains {
			view.Domains = append(view.Domains, valueOr(nameText(&domains[i])))
		}
		site.HTTPSCertificate = view
	}
	return site
}

func (c *Client) getPages(ctx context.Context) (*PagesSite, error) {
	const op = "get GitHub Pages site"
	var raw pagesRaw
	if err := c.pagesRead(ctx, op, "pages", &raw); err != nil {
		return nil, err
	}
	address := raw.links["url"]
	if address == "" || !c.belongsHere(&address, "/pages") {
		return nil, foreignActionsObject(op, "a Pages site")
	}
	site := raw.project()
	return &site, nil
}

// PagesBuild is the view of one GitHub Pages build.
type PagesBuild struct {
	Status    string           `json:"status"`
	Error     *PagesBuildError `json:"error,omitempty"`
	Pusher    *RESTAccount     `json:"pusher,omitempty"`
	Commit    string           `json:"commit,omitempty"`
	Duration  *int64           `json:"duration,omitempty"`
	CreatedAt string           `json:"created_at,omitempty"`
	UpdatedAt string           `json:"updated_at,omitempty"`
	Links     Links            `json:"links,omitempty"`
}

// PagesBuildError is the error GitHub reports for a build. The message is untrusted data.
type PagesBuildError struct {
	Message string `json:"message,omitempty"`
}

type pagesBuildRaw struct {
	Status *string `json:"status"`
	Error  *struct {
		Message *string `json:"message"`
	} `json:"error"`
	Pusher    *accountRaw `json:"pusher"`
	Commit    *string     `json:"commit"`
	Duration  *int64      `json:"duration"`
	CreatedAt *string     `json:"created_at"`
	UpdatedAt *string     `json:"updated_at"`
	links     Links
}

func (r *pagesBuildRaw) UnmarshalJSON(data []byte) (err error) {
	type plain pagesBuildRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, pagesBuildLinkKeys)
	return err
}

// project returns the view, or false when GitHub left out the status.
func (r *pagesBuildRaw) project() (PagesBuild, bool) {
	status := shortText(r.Status)
	if status == nil {
		return PagesBuild{}, false
	}
	build := PagesBuild{Status: *status, Pusher: r.Pusher.project(), Commit: valueOr(shortText(r.Commit)),
		Duration: r.Duration, CreatedAt: valueOr(shortText(r.CreatedAt)), UpdatedAt: valueOr(shortText(r.UpdatedAt)),
		Links: r.links}
	if r.Error != nil {
		if message := bulkText(r.Error.Message); message != nil {
			build.Error = &PagesBuildError{Message: *message}
		}
	}
	return build, true
}

// ownBuild reports whether the build address names a build below the bound repository; with a numeric
// request it must also end in that id. The route of latest answers with an id or the word itself.
func (c *Client) ownBuild(links Links, requested string) bool {
	address, ok := links["url"]
	if !ok {
		return false
	}
	cut := strings.LastIndex(address, "/")
	if cut < 0 {
		return false
	}
	parent := address[:cut]
	last := address[cut+1:]
	if !c.belongsHere(&parent, "/pages/builds") {
		return false
	}
	return requested == "" || requested == pagesLatest || last == requested
}

// PagesBuildList is one batch of Pages builds.
type PagesBuildList struct {
	Builds     []PagesBuild `json:"builds"`
	NextCursor string       `json:"next_cursor,omitempty"`
	HasMore    bool         `json:"has_more"`
}

func (c *Client) listPagesBuilds(ctx context.Context, a *pagesArguments) (*PagesBuildList, error) {
	const op = "list GitHub Pages builds"
	var raw []pagesBuildRaw
	hasNext, err := c.restPage(ctx, op, c.repoPath("pages/builds"), a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, pagesReadPermission)
	}
	result := &PagesBuildList{Builds: make([]PagesBuild, 0, len(raw))}
	for i := range raw {
		build, ok := raw[i].project()
		if !ok || !c.ownBuild(build.Links, "") {
			return nil, invalidEntry(op, "a Pages build")
		}
		result.Builds = append(result.Builds, build)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) getPagesBuild(ctx context.Context, build string) (*PagesBuild, error) {
	const op = "get GitHub Pages build"
	var raw pagesBuildRaw
	if err := c.pagesRead(ctx, op, "pages/builds/"+build, &raw); err != nil {
		return nil, err
	}
	view, ok := raw.project()
	if !ok {
		return nil, invalidEntry(op, "a Pages build")
	}
	if !c.ownBuild(view.Links, build) {
		return nil, foreignActionsObject(op, "a Pages build")
	}
	return &view, nil
}

// PagesHealthDomain is the check of one domain. Every member is untrusted data.
type PagesHealthDomain struct {
	Host                      *string `json:"host,omitempty"`
	URI                       *string `json:"uri,omitempty"`
	Nameservers               *string `json:"nameservers,omitempty"`
	DNSResolves               *bool   `json:"dns_resolves,omitempty"`
	IsProxied                 *bool   `json:"is_proxied,omitempty"`
	IsCloudflareIP            *bool   `json:"is_cloudflare_ip,omitempty"`
	IsFastlyIP                *bool   `json:"is_fastly_ip,omitempty"`
	IsOldIPAddress            *bool   `json:"is_old_ip_address,omitempty"`
	IsARecord                 *bool   `json:"is_a_record,omitempty"`
	HasCNAMERecord            *bool   `json:"has_cname_record,omitempty"`
	HasMXRecordsPresent       *bool   `json:"has_mx_records_present,omitempty"`
	IsValidDomain             *bool   `json:"is_valid_domain,omitempty"`
	IsApexDomain              *bool   `json:"is_apex_domain,omitempty"`
	ShouldBeARecord           *bool   `json:"should_be_a_record,omitempty"`
	IsCNAMEToGitHubUserDomain *bool   `json:"is_cname_to_github_user_domain,omitempty"`
	IsCNAMEToPagesDotGitHub   *bool   `json:"is_cname_to_pages_dot_github_dot_com,omitempty"`
	IsCNAMEToFastly           *bool   `json:"is_cname_to_fastly,omitempty"`
	IsPointedToGitHubPagesIP  *bool   `json:"is_pointed_to_github_pages_ip,omitempty"`
	IsNonGitHubPagesIPPresent *bool   `json:"is_non_github_pages_ip_present,omitempty"`
	IsPagesDomain             *bool   `json:"is_pages_domain,omitempty"`
	IsServedByPages           *bool   `json:"is_served_by_pages,omitempty"`
	IsValid                   *bool   `json:"is_valid,omitempty"`
	Reason                    *string `json:"reason,omitempty"`
	RespondsToHTTPS           *bool   `json:"responds_to_https,omitempty"`
	EnforcesHTTPS             *bool   `json:"enforces_https,omitempty"`
	HTTPSError                *string `json:"https_error,omitempty"`
	IsHTTPSEligible           *bool   `json:"is_https_eligible,omitempty"`
	CAAError                  *string `json:"caa_error,omitempty"`
}

// clean bounds the texts the check carries; they come from DNS and from the domain owner.
func (d *PagesHealthDomain) clean() *PagesHealthDomain {
	if d == nil {
		return nil
	}
	for _, text := range []**string{&d.Host, &d.URI, &d.Nameservers, &d.Reason, &d.HTTPSError, &d.CAAError} {
		*text = shortText(*text)
	}
	return d
}

// PagesHealth is the DNS health check of a Pages site; ready is false while GitHub still runs it.
type PagesHealth struct {
	Ready     bool               `json:"ready"`
	Domain    *PagesHealthDomain `json:"domain,omitempty"`
	AltDomain *PagesHealthDomain `json:"alt_domain,omitempty"`
}

func (c *Client) getPagesHealth(ctx context.Context) (*PagesHealth, error) {
	const op = "get GitHub Pages health check"
	var raw struct {
		Domain    *PagesHealthDomain `json:"domain"`
		AltDomain *PagesHealthDomain `json:"alt_domain"`
	}
	var status int
	err := c.do(ctx, op, http.MethodGet, c.endpoints.rest+c.repoPath("pages/health"), nil, &raw, false, nil, &status)
	if err != nil {
		return nil, pagesHealthFailure(actionsFailure(err, pagesReadPermission))
	}
	if status == http.StatusAccepted {
		return &PagesHealth{}, nil
	}
	return &PagesHealth{Ready: true, Domain: raw.Domain.clean(), AltDomain: raw.AltDomain.clean()}, nil
}

// pagesHealthFailure says what GitHub's two documented refusals of the check mean.
func pagesHealthFailure(err error) error {
	var failure *provider.Error
	if !errors.As(err, &failure) || failure.Class != provider.ClassProviderError || failure.Original == nil ||
		failure.Message != rejectedMessage {
		return err
	}
	renamed := *failure
	switch failure.Original.Status {
	case http.StatusBadRequest:
		renamed.Message = "GitHub does not offer custom domains for GitHub Pages of this repository"
	case http.StatusUnprocessableEntity:
		renamed.Message = "the GitHub Pages site of this repository has no custom domain (CNAME) to check"
	default:
		return err
	}
	return &renamed
}

// pagesSubject names what a repository path below pages addresses.
func pagesSubject(path string) string {
	switch {
	case path == "pages":
		return "the GitHub Pages site of this repository"
	case path == "pages/health":
		return "the GitHub Pages health check of this repository"
	case path == "pages/builds":
		return "the GitHub Pages builds of this repository"
	}
	build, ok := strings.CutPrefix(path, "pages/builds/")
	if !ok {
		return ""
	}
	if build == pagesLatest {
		return "the latest GitHub Pages build of this repository"
	}
	if n, err := strconv.ParseInt(build, 10, 64); err == nil && n > 0 {
		return "GitHub Pages build " + build
	}
	return "the GitHub Pages builds of this repository"
}
