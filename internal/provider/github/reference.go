package github

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/redact"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
)

// The reference tools read what GitHub serves to every account alike, or about the token itself: the rate
// limit of the token, GitHub's meta information with its IP ranges and domains, the license and .gitignore
// templates, the codes of conduct, and the emoji list; github.markdown.render renders Markdown the way GitHub
// does. None of them names a repository, project, or owner, and each follows the account-wide rule of
// accountWideAllowed: a connection whose targets name a repository or a project is refused before a secret is
// read. github.markdown.render is a POST that changes nothing and is a read. Its text may name any repository,
// issue, or user the token can see, and GitHub resolves such references whatever context says, so the
// account-wide rule holds with context as well; context must in addition be a repository the connection's
// targets allow and no exclusion covers. Template texts and the rendered HTML are untrusted data, cut at fixed
// lengths with a truncated member; the HTML is never executed and nothing it names is fetched.

// Bounds of the reference tools.
const (
	// referenceTextLimit cuts a license text, a .gitignore source, and a code of conduct body. It is above
	// the length of every template GitHub serves today, so a cut is the exception and says so.
	referenceTextLimit = 64000
	// referenceListLimit caps the entries of a reference list and of a list inside one entry.
	referenceListLimit = 1000
	// markdownTextLimit is the largest text github.markdown.render sends, in bytes; GitHub itself accepts up
	// to 400 KB.
	markdownTextLimit = 64 << 10
	// markdownHTMLLimit is the most of the rendered HTML read and answered, in bytes.
	markdownHTMLLimit = 256 << 10
	// metaListLimit caps each IP range and domain list of github.meta.get without services, and
	// metaServiceLimit with services, which names the lists a caller wants in full.
	metaListLimit    = 100
	metaServiceLimit = 10000
	// rateLimitResourceLimit caps the resource groups of github.rate-limit.get.
	rateLimitResourceLimit = 100
)

// referenceNamePattern admits a license key, a .gitignore template name, or a code of conduct key: it starts
// with a letter or a digit, so it is never a dot segment, and holds no slash.
const referenceNamePattern = `^[A-Za-z0-9][A-Za-z0-9._+-]{0,99}$`

var referenceName = regexp.MustCompile(referenceNamePattern)

const referenceNameSchema = `{"type":"string","minLength":1,"maxLength":100,"pattern":"` + referenceNamePattern + `"}`

// emojiNamePattern admits an emoji name or a part of one.
const emojiNamePattern = `^[a-z0-9_+-]{1,64}$`

var emojiName = regexp.MustCompile(emojiNamePattern)

// emojiCursorPrefix marks the offset an emoji cursor continues at.
const emojiCursorPrefix = "offset:"

var rateLimitResourceName = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// metaServices are the lists of github.meta.get that services may name, in GitHub's order.
var metaServices = []string{"hooks", "web", "api", "git", "github_enterprise_importer", "packages", "pages",
	"importer", "actions", "actions_macos", "codespaces", "dependabot", "copilot", "shared_ingress", "ssh_keys",
	"commit_signing_keys", "domains"}

const (
	referenceLinks   = `"links":{"type":"object","properties":{"url":{"type":"string"}},"additionalProperties":false}`
	rateBucketSchema = `{"type":"object","properties":{"limit":{"type":"integer"},"remaining":{"type":"integer"},` +
		`"reset":{"type":"integer"},"used":{"type":"integer"}},"additionalProperties":false}`
)

var referenceLinksField = capability.Field{Name: "links", Description: "REST address of the entry as url, " +
	"never fetched"}

var rateLimitGet = capability.Descriptor{
	ID:      Provider + ".rate-limit.get",
	Version: 1,
	Title:   "Get the GitHub rate limit of this connection's token",
	Description: "Read the rate limit of the connection's token for every resource group GitHub reports, such " +
		"as core, graphql, and search; the read does not count against the limit; offered only by a connection " +
		"whose targets name no repository and no project",
	Tags:        []string{"github", "rate-limit", "account", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(""),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"resources":{"type":"object",` +
		`"additionalProperties":` + rateBucketSchema + `},"rate":` + rateBucketSchema + `},` +
		`"required":["resources"],"additionalProperties":false}`),
	Fields: []capability.Field{
		{Name: "resources", Description: "Resource groups by GitHub's name, each with limit, remaining, reset (Unix " +
			"seconds), and used; the set is GitHub's and is not fixed by Qatlas"},
		{Name: "rate", Description: "Core limit in GitHub's deprecated rate member, the same as resources.core"},
	},
	Examples: []capability.Example{{Description: "Read the rate limit", Arguments: json.RawMessage(`{}`)}},
}

var metaFingerprintsSchema = objectSchema(`"SHA256_RSA":{"type":"string"},"SHA256_DSA":{"type":"string"},` +
	`"SHA256_ECDSA":{"type":"string"},"SHA256_ED25519":{"type":"string"}`)

var metaDomainsSchema = objectSchema(`"website":` + stringListSchema + `,"codespaces":` + stringListSchema +
	`,"copilot":` + stringListSchema + `,"packages":` + stringListSchema + `,"storage":` + stringListSchema +
	`,"actions":` + stringListSchema + `,"actions_inbound":` + objectSchema(`"full_domains":`+stringListSchema+
	`,"wildcard_domains":`+stringListSchema) + `,"artifact_attestations":` +
	objectSchema(`"trust_domain":{"type":"string"},"services":`+stringListSchema))

func metaListProperties() string {
	var b strings.Builder
	for _, name := range metaServices {
		if name == "domains" {
			continue
		}
		b.WriteString(`,"` + name + `":` + stringListSchema)
	}
	return b.String()
}

var metaGet = capability.Descriptor{
	ID:      Provider + ".meta.get",
	Version: 1,
	Title:   "Get GitHub's meta information",
	Description: "Read GitHub's meta information: whether password authentication is verifiable, the SSH host " +
		"key fingerprints and keys, and the IP ranges and domains of its services; each list holds at most " +
		strconv.Itoa(metaListLimit) + " entries unless services names it; offered only by a connection whose " +
		"targets name no repository and no project",
	Tags:     []string{"github", "meta", "reference", "get"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"services":{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(len(metaServices)) +
		`,"uniqueItems":true,"items":` + enumSchema(metaServices) + `}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"verifiable_password_authentication":` +
		`{"type":"boolean"},"ssh_key_fingerprints":` + metaFingerprintsSchema + metaListProperties() +
		`,"domains":` + metaDomainsSchema + `,"truncated":{"type":"boolean"}},` +
		`"required":["verifiable_password_authentication","truncated"],"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "services", Description: "Lists to answer, each in full up to " +
		strconv.Itoa(metaServiceLimit) + " entries: " + strings.Join(metaServices, ", ") + "; every list, each " +
		"cut at " + strconv.Itoa(metaListLimit) + " entries, when omitted"}},
	Fields: append(append([]capability.Field{
		{Name: "verifiable_password_authentication", Description: "Whether GitHub verifies password authentication"},
		{Name: "ssh_key_fingerprints", Description: "SHA256 fingerprints of GitHub's SSH host keys by key type"},
	}, metaListFields()...),
		capability.Field{Name: "domains", Description: "Domains of website, codespaces, copilot, packages, " +
			"storage, actions, actions_inbound (full_domains, wildcard_domains), and artifact_attestations " +
			"(trust_domain, services)"},
		capability.Field{Name: "truncated", Description: "True when a list was cut; ask for it with services"}),
	Examples: []capability.Example{{
		Description: "Read the IP ranges of webhooks and Actions in full",
		Arguments:   json.RawMessage(`{"services":["hooks","actions"]}`),
	}},
}

func metaListFields() []capability.Field {
	fields := make([]capability.Field, 0, len(metaServices))
	for _, name := range metaServices {
		switch name {
		case "domains":
		case "ssh_keys":
			fields = append(fields, capability.Field{Name: name, Description: "Public SSH host keys of GitHub"})
		case "commit_signing_keys":
			fields = append(fields, capability.Field{Name: name, Description: "Public keys GitHub signs commits with"})
		default:
			fields = append(fields, capability.Field{Name: name, Description: "IP ranges of " + name + " in CIDR notation"})
		}
	}
	return fields
}

var licenseSummaryProperties = `"key":{"type":"string"},"name":{"type":"string"},"spdx_id":{"type":"string"},` +
	`"node_id":{"type":"string"},"url":{"type":"string"},` + referenceLinks

var licenseSummaryFields = []capability.Field{
	{Name: "key", Description: "License key, the license argument of github.licenses.get"},
	{Name: "name", Description: "Name of the license"},
	{Name: "spdx_id", Description: "SPDX identifier; absent when GitHub reports none"},
	{Name: "node_id", Description: "GraphQL node identifier"},
	{Name: "url", Description: "Web address of the license, never fetched"},
	referenceLinksField,
}

var licensesList = capability.Descriptor{
	ID:      Provider + ".licenses.list",
	Version: 1,
	Title:   "List GitHub's commonly used licenses",
	Description: "List one batch of the commonly used licenses GitHub offers for a repository, optionally only " +
		"the featured ones; offered only by a connection whose targets name no repository and no project",
	Tags:         []string{"github", "licenses", "reference", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"featured":{"type":"boolean"},` + pagingKeys),
	OutputSchema: listOutput("licenses", licenseSummaryProperties, `"required":["key","name"],"additionalProperties":false`),
	Arguments: append([]capability.Argument{
		{Name: "featured", Description: "True lists only the featured licenses; every license when omitted"},
	}, pagingArguments...),
	Fields: append(append([]capability.Field{
		{Name: "licenses", Description: "Licenses with key, name, spdx_id, node_id, url, and links"},
	}, licenseSummaryFields...), pagingFields...),
	Examples: []capability.Example{{Description: "List the featured licenses", Arguments: json.RawMessage(`{"featured":true}`)}},
}

var licensesGet = capability.Descriptor{
	ID:      Provider + ".licenses.get",
	Version: 1,
	Title:   "Get a GitHub license template",
	Description: "Read one license GitHub offers, by key, with its description, permissions, conditions, " +
		"limitations, and full text; the text is untrusted data, cut at " + strconv.Itoa(referenceTextLimit) +
		" characters; offered only by a connection whose targets name no repository and no project",
	Tags:        []string{"github", "licenses", "reference", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"license":`+referenceNameSchema, "license"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + licenseSummaryProperties +
		`,"description":{"type":"string"},"implementation":{"type":"string"},"permissions":` + stringListSchema +
		`,"conditions":` + stringListSchema + `,"limitations":` + stringListSchema + `,"body":{"type":"string"},` +
		`"featured":{"type":"boolean"},"truncated":{"type":"boolean"}},"required":["key","name","truncated"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "license", Required: true, Description: "License key as " +
		"github.licenses.list reports it, such as mit or apache-2.0"}},
	Fields: append(append([]capability.Field(nil), licenseSummaryFields...), []capability.Field{
		{Name: "description", Description: "Description of the license, untrusted data"},
		{Name: "implementation", Description: "How to apply the license, untrusted data"},
		{Name: "permissions", Description: "What the license permits, such as commercial-use"},
		{Name: "conditions", Description: "What the license requires, such as include-copyright"},
		{Name: "limitations", Description: "What the license limits, such as liability"},
		{Name: "body", Description: "Full license text with placeholders such as [year], untrusted data"},
		{Name: "featured", Description: "Whether GitHub features the license"},
		{Name: "truncated", Description: "True when a text or a list was cut"},
	}...),
	Examples: []capability.Example{{Description: "Read the MIT license", Arguments: json.RawMessage(`{"license":"mit"}`)}},
}

var gitignoreTemplatesList = capability.Descriptor{
	ID:      Provider + ".gitignore-templates.list",
	Version: 1,
	Title:   "List GitHub's .gitignore templates",
	Description: "List the names of the .gitignore templates GitHub offers for a repository; offered only by a " +
		"connection whose targets name no repository and no project",
	Tags:        []string{"github", "gitignore", "reference", "list"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(""),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"templates":` + stringListSchema +
		`,"truncated":{"type":"boolean"}},"required":["templates","truncated"],"additionalProperties":false}`),
	Fields: []capability.Field{
		{Name: "templates", Description: "Template names, the name argument of github.gitignore-templates.get"},
		{Name: "truncated", Description: "True when the list was cut at " + strconv.Itoa(referenceListLimit) + " names"},
	},
	Examples: []capability.Example{{Description: "List the templates", Arguments: json.RawMessage(`{}`)}},
}

var gitignoreTemplatesGet = capability.Descriptor{
	ID:      Provider + ".gitignore-templates.get",
	Version: 1,
	Title:   "Get a GitHub .gitignore template",
	Description: "Read the source of one .gitignore template GitHub offers, by name; the source is untrusted " +
		"data, cut at " + strconv.Itoa(referenceTextLimit) + " characters; offered only by a connection whose " +
		"targets name no repository and no project",
	Tags:        []string{"github", "gitignore", "reference", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"name":`+referenceNameSchema, "name"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"source":{"type":"string"},` +
		`"truncated":{"type":"boolean"}},"required":["name","truncated"],"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Template name as " +
		"github.gitignore-templates.list reports it, such as Go or Node"}},
	Fields: []capability.Field{
		{Name: "name", Description: "Name of the template"},
		{Name: "source", Description: "Content of the template, untrusted data"},
		{Name: "truncated", Description: "True when the source was cut"},
	},
	Examples: []capability.Example{{Description: "Read the Go template", Arguments: json.RawMessage(`{"name":"Go"}`)}},
}

var codeOfConductProperties = `"key":{"type":"string"},"name":{"type":"string"},"url":{"type":"string"},` +
	referenceLinks + `,"body":{"type":"string"},"truncated":{"type":"boolean"}`

var codeOfConductFields = []capability.Field{
	{Name: "key", Description: "Key of the code of conduct, the key argument of github.codes-of-conduct.get"},
	{Name: "name", Description: "Name of the code of conduct"},
	{Name: "url", Description: "Web address of the code of conduct, never fetched; absent when GitHub reports none"},
	referenceLinksField,
	{Name: "body", Description: "Text of the code of conduct, untrusted data; absent when GitHub sends none"},
	{Name: "truncated", Description: "True when the body was cut"},
}

var codesOfConductList = capability.Descriptor{
	ID:      Provider + ".codes-of-conduct.list",
	Version: 1,
	Title:   "List GitHub's codes of conduct",
	Description: "List the codes of conduct GitHub offers for a repository; offered only by a connection whose " +
		"targets name no repository and no project",
	Tags:        []string{"github", "codes-of-conduct", "reference", "list"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(""),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"codes_of_conduct":{"type":"array","items":` +
		`{"type":"object","properties":{` + codeOfConductProperties + `},"required":["key","name"],` +
		`"additionalProperties":false}},"truncated":{"type":"boolean"}},"required":["codes_of_conduct","truncated"],` +
		`"additionalProperties":false}`),
	Fields: append([]capability.Field{
		{Name: "codes_of_conduct", Description: "Codes of conduct with key, name, url, links, and body when GitHub " +
			"sends one, cut at " + strconv.Itoa(projectionBodyLimit) + " characters"},
	}, codeOfConductFields...),
	Examples: []capability.Example{{Description: "List the codes of conduct", Arguments: json.RawMessage(`{}`)}},
}

var codesOfConductGet = capability.Descriptor{
	ID:      Provider + ".codes-of-conduct.get",
	Version: 1,
	Title:   "Get a GitHub code of conduct",
	Description: "Read one code of conduct GitHub offers, by key, with its text; the text is untrusted data, cut " +
		"at " + strconv.Itoa(referenceTextLimit) + " characters; offered only by a connection whose targets name " +
		"no repository and no project",
	Tags:        []string{"github", "codes-of-conduct", "reference", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"key":`+referenceNameSchema, "key"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + codeOfConductProperties + `},` +
		`"required":["key","name","truncated"],"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "key", Required: true, Description: "Key as " +
		"github.codes-of-conduct.list reports it, such as contributor_covenant"}},
	Fields: codeOfConductFields,
	Examples: []capability.Example{{
		Description: "Read the Contributor Covenant",
		Arguments:   json.RawMessage(`{"key":"contributor_covenant"}`),
	}},
}

var emojisList = capability.Descriptor{
	ID:      Provider + ".emojis.list",
	Version: 1,
	Title:   "List GitHub emojis",
	Description: "List one batch of the emojis GitHub renders, by name in alphabetical order, optionally only " +
		"those whose name contains a text; offered only by a connection whose targets name no repository and no " +
		"project",
	Tags:         []string{"github", "emojis", "reference", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"name":{"type":"string","minLength":1,"maxLength":64,"pattern":"` + emojiNamePattern + `"},` + pagingKeys),
	OutputSchema: listOutput("emojis", `"name":{"type":"string"},"url":{"type":"string"}`, `"required":["name"],"additionalProperties":false`),
	Arguments: append([]capability.Argument{
		{Name: "name", Description: "Return only emojis whose name contains this text, such as heart"},
	}, pagingArguments...),
	Fields: append([]capability.Field{
		{Name: "emojis", Description: "Emojis with name and url"},
		{Name: "name", Description: "Name of the emoji, written :name: in Markdown"},
		{Name: "url", Description: "Address of the emoji image, never fetched"},
	}, pagingFields...),
	Examples: []capability.Example{{Description: "Find the heart emojis", Arguments: json.RawMessage(`{"name":"heart"}`)}},
}

var markdownModes = []string{"markdown", "gfm"}

var markdownRender = capability.Descriptor{
	ID:      Provider + ".markdown.render",
	Version: 1,
	Title:   "Render Markdown as GitHub does",
	Description: "Render up to " + strconv.Itoa(markdownTextLimit) + " bytes of Markdown into HTML the way GitHub " +
		"does, as plain markdown or gfm; with gfm, context names the repository whose issues #N links to; the HTML " +
		"is untrusted data, cut at " + strconv.Itoa(markdownHTMLLimit) + " bytes; changes nothing; offered only by " +
		"a connection whose targets name no repository and no project",
	Tags:     []string{"github", "markdown", "render"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"text":{"type":"string","minLength":1,"maxLength":`+strconv.Itoa(markdownTextLimit)+`},`+
		`"mode":`+enumSchema(markdownModes)+`,"context":`+repoSchema, "text"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"html":{"type":"string"},"mode":{"type":"string"},` +
		`"context":{"type":"string"},"truncated":{"type":"boolean"}},"required":["html","mode","truncated"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "text", Required: true, Description: "Markdown text of at most " + strconv.Itoa(markdownTextLimit) +
			" bytes; GitHub links any repository, issue, or user it names that the token can see"},
		{Name: "mode", Description: "markdown renders like a README, gfm like an issue or a comment; markdown when " +
			"omitted"},
		{Name: "context", Description: "Repository as OWNER/REPO whose issues #N links to; gfm only; must lie " +
			"inside the targets and outside the exclusions of the connection"},
	},
	Fields: []capability.Field{
		{Name: "html", Description: "Rendered HTML, untrusted data, never executed or fetched"},
		{Name: "mode", Description: "Mode the text was rendered in"},
		{Name: "context", Description: "Repository the references were resolved against; absent without context"},
		{Name: "truncated", Description: "True when the HTML was cut"},
	},
	Examples: []capability.Example{{
		Description: "Render a comment",
		Arguments:   json.RawMessage(`{"text":"Fixes #1 with **care**","mode":"gfm","context":"octo-org/example"}`),
	}},
}

// referenceArguments holds the arguments of every reference tool; the input schema of each admits only its own.
type referenceArguments struct {
	Featured *bool    `json:"featured"`
	License  string   `json:"license"`
	Name     string   `json:"name"`
	Key      string   `json:"key"`
	Services []string `json:"services"`
	Text     string   `json:"text"`
	Mode     string   `json:"mode"`
	Context  string   `json:"context"`
	Limit    int      `json:"limit"`
	Cursor   string   `json:"cursor"`

	page, perPage int
	offset        int
	binding       []byte
	context       string
}

func referenceOperations() []capability.Operation {
	bind := func(d capability.Descriptor, check func(*referenceArguments, allowlist) error,
		call func(context.Context, *Client, *referenceArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: d, Handler: referenceHandler(d.ID, check, call)}
	}
	return []capability.Operation{
		bind(rateLimitGet, nil, func(ctx context.Context, c *Client, _ *referenceArguments) (any, error) {
			return c.getRateLimit(ctx)
		}),
		bind(metaGet, checkMeta, func(ctx context.Context, c *Client, a *referenceArguments) (any, error) {
			return c.getMeta(ctx, a)
		}),
		bind(licensesList, checkLicensesList, func(ctx context.Context, c *Client, a *referenceArguments) (any, error) {
			return c.listLicenses(ctx, a)
		}),
		bind(licensesGet, checkReferenceName("license", func(a *referenceArguments) string { return a.License }),
			func(ctx context.Context, c *Client, a *referenceArguments) (any, error) {
				return c.getLicense(ctx, a.License)
			}),
		bind(gitignoreTemplatesList, nil, func(ctx context.Context, c *Client, _ *referenceArguments) (any, error) {
			return c.listGitignoreTemplates(ctx)
		}),
		bind(gitignoreTemplatesGet, checkReferenceName("name", func(a *referenceArguments) string { return a.Name }),
			func(ctx context.Context, c *Client, a *referenceArguments) (any, error) {
				return c.getGitignoreTemplate(ctx, a.Name)
			}),
		bind(codesOfConductList, nil, func(ctx context.Context, c *Client, _ *referenceArguments) (any, error) {
			return c.listCodesOfConduct(ctx)
		}),
		bind(codesOfConductGet, checkReferenceName("key", func(a *referenceArguments) string { return a.Key }),
			func(ctx context.Context, c *Client, a *referenceArguments) (any, error) {
				return c.getCodeOfConduct(ctx, a.Key)
			}),
		bind(emojisList, checkEmojisList, func(ctx context.Context, c *Client, a *referenceArguments) (any, error) {
			return c.listEmojis(ctx, a)
		}),
		bind(markdownRender, checkMarkdown, func(ctx context.Context, c *Client, a *referenceArguments) (any, error) {
			return c.renderMarkdown(ctx, a)
		}),
	}
}

// referenceHandler applies the account-wide rule and checks the arguments before a credential is resolved, so
// a refused call never reads a secret and never reaches GitHub.
func referenceHandler(id string, check func(*referenceArguments, allowlist) error,
	call func(context.Context, *Client, *referenceArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments referenceArguments
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
		if check != nil {
			if err := check(&arguments, allowed); err != nil {
				return nil, err
			}
		}
		client, err := Open(ctx, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		return call(ctx, client, &arguments)
	}
}

// validReferenceName reports whether a name may become one path segment: url.PathEscape keeps . and .. as
// they are, so a dot segment is refused here as well as by the pattern.
func validReferenceName(value string) bool {
	return value != "." && value != ".." && referenceName.MatchString(value)
}

func checkReferenceName(name string, of func(*referenceArguments) string) func(*referenceArguments, allowlist) error {
	return func(a *referenceArguments, _ allowlist) error {
		if !validReferenceName(of(a)) {
			return invalidRequest(name + " must be 1 to 100 letters, digits, dots, plus signs, hyphens, or " +
				"underscores, starting with a letter or a digit")
		}
		return nil
	}
}

func checkMeta(a *referenceArguments, _ allowlist) error {
	seen := map[string]bool{}
	for _, service := range a.Services {
		if !slices.Contains(metaServices, service) || seen[service] {
			return invalidRequest("services must name each of " + strings.Join(metaServices, ", ") + " at most once")
		}
		seen[service] = true
	}
	return nil
}

func checkLicensesList(a *referenceArguments, _ allowlist) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint("licenses", a.Featured)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkEmojisList(a *referenceArguments, _ allowlist) error {
	if a.Name != "" && !emojiName.MatchString(a.Name) {
		return invalidRequest("name must be 1 to 64 lowercase letters, digits, underscores, plus signs, or hyphens")
	}
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.perPage = limit
	a.binding = fingerprint("emojis", a.Name)
	after, err := decodeCursor(a.binding, a.Cursor)
	if err != nil || after == "" {
		return err
	}
	offset, ok := strings.CutPrefix(after, emojiCursorPrefix)
	if a.offset, err = strconv.Atoi(offset); !ok || err != nil || a.offset < 1 || a.offset > maxPage {
		return invalidRequest("cursor is not a next_cursor of this list; start the list again without cursor")
	}
	return nil
}

// checkMarkdown bounds the text and resolves context against the connection's targets and exclusions; an
// absent context never falls back to a target.
func checkMarkdown(a *referenceArguments, allowed allowlist) error {
	if a.Text == "" || len(a.Text) > markdownTextLimit {
		return invalidRequest("text must be 1 to " + strconv.Itoa(markdownTextLimit) + " bytes of Markdown")
	}
	if err := checkText("text", a.Text); err != nil {
		return err
	}
	if a.Mode == "" {
		a.Mode = "markdown"
	}
	if !slices.Contains(markdownModes, a.Mode) {
		return invalidRequest("mode must be markdown or gfm")
	}
	if a.Context == "" {
		return nil
	}
	if a.Mode != "gfm" {
		return invalidRequest("context applies only with mode gfm; pass mode gfm or leave context out")
	}
	chosen, err := allowed.choose(kindRepository, a.Context)
	if err != nil {
		return err
	}
	a.context = chosen.owner + "/" + chosen.repo
	return nil
}

// ---- Rate limit

// RateLimitBucket is the limit of one resource group.
type RateLimitBucket struct {
	Limit     *int64 `json:"limit,omitempty"`
	Remaining *int64 `json:"remaining,omitempty"`
	Reset     *int64 `json:"reset,omitempty"`
	Used      *int64 `json:"used,omitempty"`
}

// RateLimitStatus is the rate limit of the token by resource group.
type RateLimitStatus struct {
	Resources map[string]RateLimitBucket `json:"resources"`
	Rate      *RateLimitBucket           `json:"rate,omitempty"`
}

func (c *Client) getRateLimit(ctx context.Context) (*RateLimitStatus, error) {
	const op = "get rate limit"
	var raw struct {
		Resources map[string]*RateLimitBucket `json:"resources"`
		Rate      *RateLimitBucket            `json:"rate"`
	}
	if err := c.rest(ctx, op, "/rate_limit", &raw); err != nil {
		return nil, err
	}
	if raw.Resources == nil {
		return nil, invalidResponse(op, false)
	}
	names := make([]string, 0, len(raw.Resources))
	for name, bucket := range raw.Resources {
		if bucket != nil && rateLimitResourceName.MatchString(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > rateLimitResourceLimit {
		names = names[:rateLimitResourceLimit]
	}
	result := &RateLimitStatus{Resources: make(map[string]RateLimitBucket, len(names)), Rate: raw.Rate}
	for _, name := range names {
		result.Resources[name] = *raw.Resources[name]
	}
	return result, nil
}

// ---- Meta

// MetaFingerprints are the SHA256 fingerprints of GitHub's SSH host keys.
type MetaFingerprints struct {
	RSA     *string `json:"SHA256_RSA,omitempty"`
	DSA     *string `json:"SHA256_DSA,omitempty"`
	ECDSA   *string `json:"SHA256_ECDSA,omitempty"`
	ED25519 *string `json:"SHA256_ED25519,omitempty"`
}

// MetaInbound are the inbound domains of GitHub Actions.
type MetaInbound struct {
	FullDomains     []string `json:"full_domains,omitempty"`
	WildcardDomains []string `json:"wildcard_domains,omitempty"`
}

// MetaAttestations are the artifact attestation domains.
type MetaAttestations struct {
	TrustDomain *string  `json:"trust_domain,omitempty"`
	Services    []string `json:"services,omitempty"`
}

// MetaDomains are the domains of GitHub's services.
type MetaDomains struct {
	Website              []string          `json:"website,omitempty"`
	Codespaces           []string          `json:"codespaces,omitempty"`
	Copilot              []string          `json:"copilot,omitempty"`
	Packages             []string          `json:"packages,omitempty"`
	Storage              []string          `json:"storage,omitempty"`
	Actions              []string          `json:"actions,omitempty"`
	ActionsInbound       *MetaInbound      `json:"actions_inbound,omitempty"`
	ArtifactAttestations *MetaAttestations `json:"artifact_attestations,omitempty"`
}

// Meta is GitHub's meta information.
type Meta struct {
	VerifiablePasswordAuthentication bool              `json:"verifiable_password_authentication"`
	SSHKeyFingerprints               *MetaFingerprints `json:"ssh_key_fingerprints,omitempty"`
	Hooks                            []string          `json:"hooks,omitempty"`
	Web                              []string          `json:"web,omitempty"`
	API                              []string          `json:"api,omitempty"`
	Git                              []string          `json:"git,omitempty"`
	GitHubEnterpriseImporter         []string          `json:"github_enterprise_importer,omitempty"`
	Packages                         []string          `json:"packages,omitempty"`
	Pages                            []string          `json:"pages,omitempty"`
	Importer                         []string          `json:"importer,omitempty"`
	Actions                          []string          `json:"actions,omitempty"`
	ActionsMacOS                     []string          `json:"actions_macos,omitempty"`
	Codespaces                       []string          `json:"codespaces,omitempty"`
	Dependabot                       []string          `json:"dependabot,omitempty"`
	Copilot                          []string          `json:"copilot,omitempty"`
	SharedIngress                    []string          `json:"shared_ingress,omitempty"`
	SSHKeys                          []string          `json:"ssh_keys,omitempty"`
	CommitSigningKeys                []string          `json:"commit_signing_keys,omitempty"`
	Domains                          *MetaDomains      `json:"domains,omitempty"`
	Truncated                        bool              `json:"truncated"`
}

type metaRaw struct {
	VerifiablePasswordAuthentication *bool             `json:"verifiable_password_authentication"`
	SSHKeyFingerprints               *MetaFingerprints `json:"ssh_key_fingerprints"`
	Hooks                            []string          `json:"hooks"`
	Web                              []string          `json:"web"`
	API                              []string          `json:"api"`
	Git                              []string          `json:"git"`
	GitHubEnterpriseImporter         []string          `json:"github_enterprise_importer"`
	Packages                         []string          `json:"packages"`
	Pages                            []string          `json:"pages"`
	Importer                         []string          `json:"importer"`
	Actions                          []string          `json:"actions"`
	ActionsMacOS                     []string          `json:"actions_macos"`
	Codespaces                       []string          `json:"codespaces"`
	Dependabot                       []string          `json:"dependabot"`
	Copilot                          []string          `json:"copilot"`
	SharedIngress                    []string          `json:"shared_ingress"`
	SSHKeys                          []string          `json:"ssh_keys"`
	CommitSigningKeys                []string          `json:"commit_signing_keys"`
	Domains                          *MetaDomains      `json:"domains"`
}

// shortList cleans and caps a list of short texts and reports whether it was cut; empty entries stay out.
func shortList(values []string, limit int) ([]string, bool) {
	var out []string
	for i := range values {
		if len(out) == limit {
			return out, true
		}
		if text := shortText(&values[i]); text != nil {
			out = append(out, *text)
		}
	}
	return out, false
}

func (c *Client) getMeta(ctx context.Context, a *referenceArguments) (*Meta, error) {
	const op = "get meta"
	var raw metaRaw
	if err := c.rest(ctx, op, "/meta", &raw); err != nil {
		return nil, err
	}
	if raw.VerifiablePasswordAuthentication == nil {
		return nil, invalidResponse(op, false)
	}
	meta := &Meta{VerifiablePasswordAuthentication: *raw.VerifiablePasswordAuthentication}
	if f := raw.SSHKeyFingerprints; f != nil {
		meta.SSHKeyFingerprints = &MetaFingerprints{RSA: shortText(f.RSA), DSA: shortText(f.DSA),
			ECDSA: shortText(f.ECDSA), ED25519: shortText(f.ED25519)}
	}
	limit := metaListLimit
	if len(a.Services) > 0 {
		limit = metaServiceLimit
	}
	wants := func(name string) bool { return len(a.Services) == 0 || slices.Contains(a.Services, name) }
	take := func(values []string) []string {
		out, cut := shortList(values, limit)
		meta.Truncated = meta.Truncated || cut
		return out
	}
	for _, list := range []struct {
		name string
		in   []string
		out  *[]string
	}{
		{"hooks", raw.Hooks, &meta.Hooks}, {"web", raw.Web, &meta.Web}, {"api", raw.API, &meta.API},
		{"git", raw.Git, &meta.Git}, {"github_enterprise_importer", raw.GitHubEnterpriseImporter, &meta.GitHubEnterpriseImporter},
		{"packages", raw.Packages, &meta.Packages}, {"pages", raw.Pages, &meta.Pages},
		{"importer", raw.Importer, &meta.Importer}, {"actions", raw.Actions, &meta.Actions},
		{"actions_macos", raw.ActionsMacOS, &meta.ActionsMacOS}, {"codespaces", raw.Codespaces, &meta.Codespaces},
		{"dependabot", raw.Dependabot, &meta.Dependabot}, {"copilot", raw.Copilot, &meta.Copilot},
		{"shared_ingress", raw.SharedIngress, &meta.SharedIngress}, {"ssh_keys", raw.SSHKeys, &meta.SSHKeys},
		{"commit_signing_keys", raw.CommitSigningKeys, &meta.CommitSigningKeys},
	} {
		if wants(list.name) {
			*list.out = take(list.in)
		}
	}
	if d := raw.Domains; d != nil && wants("domains") {
		domains := &MetaDomains{Website: take(d.Website), Codespaces: take(d.Codespaces), Copilot: take(d.Copilot),
			Packages: take(d.Packages), Storage: take(d.Storage), Actions: take(d.Actions)}
		if in := d.ActionsInbound; in != nil {
			domains.ActionsInbound = &MetaInbound{FullDomains: take(in.FullDomains), WildcardDomains: take(in.WildcardDomains)}
		}
		if at := d.ArtifactAttestations; at != nil {
			domains.ArtifactAttestations = &MetaAttestations{TrustDomain: shortText(at.TrustDomain),
				Services: take(at.Services)}
		}
		meta.Domains = domains
	}
	return meta, nil
}

// ---- Licenses

// LicenseSummary is one license of the license list.
type LicenseSummary struct {
	Key    string  `json:"key"`
	Name   string  `json:"name"`
	SPDXID *string `json:"spdx_id,omitempty"`
	NodeID *string `json:"node_id,omitempty"`
	URL    *string `json:"url,omitempty"`
	Links  Links   `json:"links,omitempty"`
}

// LicenseList is one batch of licenses.
type LicenseList struct {
	Licenses   []LicenseSummary `json:"licenses"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
}

// License is one license with its text.
type License struct {
	LicenseSummary
	Description    *string  `json:"description,omitempty"`
	Implementation *string  `json:"implementation,omitempty"`
	Permissions    []string `json:"permissions,omitempty"`
	Conditions     []string `json:"conditions,omitempty"`
	Limitations    []string `json:"limitations,omitempty"`
	Body           *string  `json:"body,omitempty"`
	Featured       *bool    `json:"featured,omitempty"`
	Truncated      bool     `json:"truncated"`
}

// licenseDetailRaw is the answer of GET /licenses/{license}: the members the repository projection reads, and
// the text with its terms.
type licenseDetailRaw struct {
	licenseRaw
	detail struct {
		Description    *string  `json:"description"`
		Implementation *string  `json:"implementation"`
		Permissions    []string `json:"permissions"`
		Conditions     []string `json:"conditions"`
		Limitations    []string `json:"limitations"`
		Body           *string  `json:"body"`
		Featured       *bool    `json:"featured"`
	}
}

func (r *licenseDetailRaw) UnmarshalJSON(data []byte) error {
	if err := r.licenseRaw.UnmarshalJSON(data); err != nil {
		return err
	}
	return json.Unmarshal(data, &r.detail)
}

func (r *licenseRaw) summary() LicenseSummary {
	return LicenseSummary{Key: valueOr(shortText(r.Key)), Name: valueOr(shortText(r.Name)), SPDXID: shortText(r.SPDXID),
		NodeID: shortText(r.NodeID), URL: address(r.HTMLURL), Links: r.links}
}

func (c *Client) listLicenses(ctx context.Context, a *referenceArguments) (*LicenseList, error) {
	const op = "list licenses"
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if a.Featured != nil {
		query.Set("featured", strconv.FormatBool(*a.Featured))
	}
	var raw []licenseRaw
	hasNext, err := c.restPage(ctx, op, "/licenses", query, &raw)
	if err != nil {
		return nil, err
	}
	result := &LicenseList{Licenses: make([]LicenseSummary, 0, len(raw))}
	for i := range raw {
		license := raw[i].summary()
		if license.Key == "" || license.Name == "" {
			return nil, invalidEntry(op, "a license")
		}
		result.Licenses = append(result.Licenses, license)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) getLicense(ctx context.Context, key string) (*License, error) {
	const op = "get license"
	var raw licenseDetailRaw
	if err := c.rest(ctx, op, "/licenses/"+url.PathEscape(key), &raw); err != nil {
		return nil, err
	}
	license := &License{LicenseSummary: raw.summary(), Featured: raw.detail.Featured}
	if license.Key == "" || license.Name == "" {
		return nil, invalidEntry(op, "the license")
	}
	var cuts [6]bool
	license.Description, cuts[0] = cutText(raw.detail.Description, projectionTextLimit)
	license.Implementation, cuts[1] = cutText(raw.detail.Implementation, projectionTextLimit)
	license.Permissions, cuts[2] = shortList(raw.detail.Permissions, referenceListLimit)
	license.Conditions, cuts[3] = shortList(raw.detail.Conditions, referenceListLimit)
	license.Limitations, cuts[4] = shortList(raw.detail.Limitations, referenceListLimit)
	license.Body, cuts[5] = cutText(raw.detail.Body, referenceTextLimit)
	for _, cut := range cuts {
		license.Truncated = license.Truncated || cut
	}
	return license, nil
}

// ---- .gitignore templates

// GitignoreTemplateList is the names of the .gitignore templates.
type GitignoreTemplateList struct {
	Templates []string `json:"templates"`
	Truncated bool     `json:"truncated"`
}

// GitignoreTemplate is one .gitignore template.
type GitignoreTemplate struct {
	Name      string  `json:"name"`
	Source    *string `json:"source,omitempty"`
	Truncated bool    `json:"truncated"`
}

func (c *Client) listGitignoreTemplates(ctx context.Context) (*GitignoreTemplateList, error) {
	const op = "list gitignore templates"
	var raw []string
	if err := c.rest(ctx, op, "/gitignore/templates", &raw); err != nil {
		return nil, err
	}
	templates, cut := shortList(raw, referenceListLimit)
	if templates == nil {
		templates = []string{}
	}
	return &GitignoreTemplateList{Templates: templates, Truncated: cut}, nil
}

func (c *Client) getGitignoreTemplate(ctx context.Context, name string) (*GitignoreTemplate, error) {
	const op = "get gitignore template"
	var raw struct {
		Name   *string `json:"name"`
		Source *string `json:"source"`
	}
	if err := c.rest(ctx, op, "/gitignore/templates/"+url.PathEscape(name), &raw); err != nil {
		return nil, err
	}
	template := &GitignoreTemplate{Name: valueOr(shortText(raw.Name))}
	if template.Name == "" {
		return nil, invalidEntry(op, "the template")
	}
	template.Source, template.Truncated = cutText(raw.Source, referenceTextLimit)
	return template, nil
}

// ---- Codes of conduct

// CodeOfConduct is one code of conduct.
type CodeOfConduct struct {
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	URL       *string `json:"url,omitempty"`
	Links     Links   `json:"links,omitempty"`
	Body      *string `json:"body,omitempty"`
	Truncated bool    `json:"truncated"`
}

// CodeOfConductList is the codes of conduct.
type CodeOfConductList struct {
	CodesOfConduct []CodeOfConduct `json:"codes_of_conduct"`
	Truncated      bool            `json:"truncated"`
}

func (r *codeOfConductRaw) project(bodyLimit int) CodeOfConduct {
	code := CodeOfConduct{Key: valueOr(shortText(r.Key)), Name: valueOr(shortText(r.Name)), URL: address(r.HTMLURL),
		Links: r.links}
	code.Body, code.Truncated = cutText(r.Body, bodyLimit)
	return code
}

func (c *Client) listCodesOfConduct(ctx context.Context) (*CodeOfConductList, error) {
	const op = "list codes of conduct"
	var raw []codeOfConductRaw
	if err := c.rest(ctx, op, "/codes_of_conduct", &raw); err != nil {
		return nil, err
	}
	result := &CodeOfConductList{CodesOfConduct: []CodeOfConduct{}}
	for i := range raw {
		if len(result.CodesOfConduct) == referenceListLimit {
			result.Truncated = true
			break
		}
		code := raw[i].project(projectionBodyLimit)
		if code.Key == "" || code.Name == "" {
			return nil, invalidEntry(op, "a code of conduct")
		}
		result.CodesOfConduct = append(result.CodesOfConduct, code)
	}
	return result, nil
}

func (c *Client) getCodeOfConduct(ctx context.Context, key string) (*CodeOfConduct, error) {
	const op = "get code of conduct"
	var raw codeOfConductRaw
	if err := c.rest(ctx, op, "/codes_of_conduct/"+url.PathEscape(key), &raw); err != nil {
		return nil, err
	}
	code := raw.project(referenceTextLimit)
	if code.Key == "" || code.Name == "" {
		return nil, invalidEntry(op, "the code of conduct")
	}
	return &code, nil
}

// ---- Emojis

// Emoji is one emoji GitHub renders.
type Emoji struct {
	Name string  `json:"name"`
	URL  *string `json:"url,omitempty"`
}

// EmojiList is one batch of emojis.
type EmojiList struct {
	Emojis     []Emoji `json:"emojis"`
	NextCursor string  `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
}

// listEmojis reads GitHub's whole emoji map, which GitHub serves in one answer, and answers one batch of the
// names in alphabetical order, so a call stays bounded whatever the map holds. A name outside the emoji name
// form is left out.
func (c *Client) listEmojis(ctx context.Context, a *referenceArguments) (*EmojiList, error) {
	const op = "list emojis"
	var raw map[string]*string
	if err := c.rest(ctx, op, "/emojis", &raw); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		if emojiName.MatchString(name) && (a.Name == "" || strings.Contains(name, a.Name)) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := &EmojiList{Emojis: []Emoji{}}
	start := min(a.offset, len(names))
	end := min(start+a.perPage, len(names))
	for _, name := range names[start:end] {
		result.Emojis = append(result.Emojis, Emoji{Name: name, URL: address(raw[name])})
	}
	if end < len(names) && end < maxPage {
		result.HasMore, result.NextCursor = true, encodeCursor(a.binding, emojiCursorPrefix+strconv.Itoa(end))
	}
	return result, nil
}

// ---- Markdown

// RenderedMarkdown is the HTML GitHub rendered from a Markdown text. HTML is untrusted data.
type RenderedMarkdown struct {
	HTML      string `json:"html"`
	Mode      string `json:"mode"`
	Context   string `json:"context,omitempty"`
	Truncated bool   `json:"truncated"`
}

// markdownPermission names what rendering needs. GitHub decides on every request.
const markdownPermission = "GitHub refused this token the Markdown rendering; it needs no scope for public " +
	"references, or Contents: read on a fine-grained token, or repo on a classic token, for a private repository " +
	"as context"

// renderMarkdown sends the text once and reads the HTML GitHub answers as text/html, not JSON, up to
// markdownHTMLLimit; the HTML passes through memory into the answer only and is never fetched or run.
func (c *Client) renderMarkdown(ctx context.Context, a *referenceArguments) (*RenderedMarkdown, error) {
	const op = "render markdown"
	body := map[string]string{"text": a.Text, "mode": a.Mode}
	if a.context != "" {
		body["context"] = a.context
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, providerError(op, "the request could not be built")
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, provider.Waited(op, "GitHub", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoints.rest+"/markdown", bytes.NewReader(payload))
	if err != nil {
		return nil, providerError(op, "the request could not be built")
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, provider.Transport(op, "GitHub", err)
	}
	defer response.Body.Close()
	c.observeRateLimit(response.Header)
	c.observeScopes(response.Header)
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, actionsFailure(c.statusError(op, response, false), markdownPermission)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, markdownHTMLLimit+1))
	if err != nil {
		return nil, &provider.Error{Class: provider.ClassInvalidResponse, Op: op,
			Message: "the rendered HTML could not be read"}
	}
	truncated := len(data) > markdownHTMLLimit
	if truncated {
		data = data[:markdownHTMLLimit]
	}
	html := strings.ToValidUTF8(string(data), "")
	cleaned, _ := cleanText(&html, markdownHTMLLimit, true)
	return &RenderedMarkdown{HTML: *cleaned, Mode: a.Mode, Context: a.context, Truncated: truncated}, nil
}

// referenceSubject names what a reference route addresses, without the name the caller passed.
func referenceSubject(path string) string {
	path, _, _ = strings.Cut(path, "?")
	switch {
	case path == "/rate_limit":
		return "the rate limit of this token"
	case path == "/meta":
		return "GitHub's meta information"
	case path == "/licenses":
		return "the license list"
	case strings.HasPrefix(path, "/licenses/"):
		return "this license"
	case path == "/gitignore/templates":
		return "the .gitignore template list"
	case strings.HasPrefix(path, "/gitignore/templates/"):
		return "this .gitignore template"
	case path == "/codes_of_conduct":
		return "the code of conduct list"
	case strings.HasPrefix(path, "/codes_of_conduct/"):
		return "this code of conduct"
	case path == "/emojis":
		return "the emoji list"
	case path == "/markdown":
		return "the Markdown rendering"
	}
	return ""
}
