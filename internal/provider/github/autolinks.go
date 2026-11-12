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

// Autolink references of a repository. github.autolinks.list and github.autolinks.get read them;
// github.autolinks.create adds one and needs its own confirmation; github.autolinks.delete removes one and is
// offered only where a connection's tools list names it. An autolink turns text such as TICKET-123 into a
// link for every reader of the repository, so its url_template is untrusted data: create checks it locally
// and no tool ever requests it.

// autolinkSensitivity classifies github.autolinks.delete as its own guarded change.
const autolinkSensitivity = "github-autolink"

const (
	maxAutolinkTemplateLength = 2048
	maxAutolinkPrefixLength   = 255
	autolinkPlaceholder       = "<num>"
)

const (
	autolinkIDSchema       = `{"type":"integer","minimum":1}`
	autolinkPrefixSchema   = `{"type":"string","minLength":1,"maxLength":255}`
	autolinkTemplateSchema = `{"type":"string","minLength":1,"maxLength":2048,"x-form":"https URL with exactly one <num>"}`
)

const autolinkProperties = `"id":{"type":"integer"},"key_prefix":{"type":"string"},"url_template":{"type":"string"},` +
	`"is_alphanumeric":{"type":"boolean"},"updated_at":{"type":"string"}`

const autolinkRequired = `"required":["id","key_prefix","url_template","is_alphanumeric"],"additionalProperties":false`

var autolinkOutput = `{"type":"object","properties":{` + autolinkProperties + `},` + autolinkRequired + `}`

var autolinkFields = []capability.Field{
	{Name: "id", Description: "Numeric autolink id"},
	{Name: "key_prefix", Description: "Text that starts a reference, untrusted data"},
	{Name: "url_template", Description: "Address a reference links to, with <num> for the reference number; " +
		"untrusted data, never fetched, and it can lead a reader to an address outside the repository"},
	{Name: "is_alphanumeric", Description: "True when a reference number may hold letters as well as digits"},
	{Name: "updated_at", Description: "Time of the last change; absent when GitHub reports none"},
}

var autolinksList = capability.Descriptor{
	ID:      Provider + ".autolinks.list",
	Version: 1,
	Title:   "List GitHub autolinks",
	Description: "List the autolink references of a repository a connection allows; GitHub does not page " +
		"this list",
	Tags:        []string{"github", "autolinks", "list"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(``),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"autolinks":{"type":"array","items":{` +
		`"type":"object","properties":{` + autolinkProperties + `},` + autolinkRequired + `}}},` +
		`"required":["autolinks"],"additionalProperties":false}`),
	Fields: []capability.Field{
		{Name: "autolinks", Description: "Autolinks, key_prefix and url_template untrusted data, never fetched"},
	},
	Examples: []capability.Example{{Description: "List the autolinks", Arguments: json.RawMessage(`{}`)}},
}

var autolinksGet = capability.Descriptor{
	ID:           Provider + ".autolinks.get",
	Version:      1,
	Title:        "Get a GitHub autolink",
	Description:  "Read one autolink reference of a repository a connection allows",
	Tags:         []string{"github", "autolinks", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"id":`+autolinkIDSchema, "id"),
	OutputSchema: json.RawMessage(autolinkOutput),
	Arguments:    []capability.Argument{{Name: "id", Description: "Numeric autolink id", Required: true}},
	Fields:       autolinkFields,
	Examples:     []capability.Example{{Description: "Read one autolink", Arguments: json.RawMessage(`{"id":1}`)}},
}

var autolinksCreate = capability.Descriptor{
	ID:      Provider + ".autolinks.create",
	Version: 1,
	Title:   "Create a GitHub autolink",
	Description: "Create one autolink reference in a repository a connection allows; url_template must be an " +
		"https address without user information and with exactly one <num>; not idempotent, since a " +
		"repeated call is refused by GitHub or creates a second reference, and the address is never fetched",
	Tags:     []string{"github", "autolinks", "create"},
	Risk:     changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"key_prefix":`+autolinkPrefixSchema+`,"url_template":`+autolinkTemplateSchema+
		`,"is_alphanumeric":{"type":"boolean"}`, "key_prefix", "url_template"),
	OutputSchema: json.RawMessage(autolinkOutput),
	Arguments: []capability.Argument{
		{Name: "key_prefix", Description: "Text that starts a reference, such as TICKET-", Required: true},
		{Name: "url_template", Description: "https address with exactly one <num>, at most 2048 characters", Required: true},
		{Name: "is_alphanumeric", Description: "Whether a reference number may hold letters; GitHub's default is true"},
	},
	Fields: autolinkFields,
	Examples: []capability.Example{{
		Description: "Create an autolink",
		Arguments:   json.RawMessage(`{"key_prefix":"TICKET-","url_template":"https://tickets.example.com/<num>"}`),
	}},
}

const autolinkDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},"id":{"type":"integer"}},` +
	`"required":["deleted","id"],"additionalProperties":false}`

var autolinksDelete = capability.Descriptor{
	ID:      Provider + ".autolinks.delete",
	Version: 1,
	Title:   "Delete a GitHub autolink",
	Description: "Delete one autolink reference of a repository a connection allows; the autolink is read " +
		"under the repository first, then deleted with one request; offered only where a connection's tools " +
		"list names it",
	Tags:                  []string{"github", "autolinks", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, autolinkSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"id":`+autolinkIDSchema, "id"),
	OutputSchema:          json.RawMessage(autolinkDeletedOutput),
	Arguments:             []capability.Argument{{Name: "id", Description: "Numeric autolink id to delete", Required: true}},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the autolink"},
		{Name: "id", Description: "Autolink id that was deleted"},
	},
	Examples: []capability.Example{{Description: "Delete an autolink", Arguments: json.RawMessage(`{"id":1}`)}},
}

// autolinksOperations binds every autolink tool to its handler.
func autolinksOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: autolinksList, Handler: autolinksHandler(autolinksList.ID, nil,
			func(ctx context.Context, c *Client, _ *autolinksArguments) (any, error) { return c.listAutolinks(ctx) })},
		{Descriptor: autolinksGet, Handler: autolinksHandler(autolinksGet.ID, checkAutolinkID,
			func(ctx context.Context, c *Client, a *autolinksArguments) (any, error) {
				return c.getAutolink(ctx, a.ID)
			})},
		{Descriptor: autolinksCreate, Handler: autolinksHandler(autolinksCreate.ID, checkAutolinkCreate,
			func(ctx context.Context, c *Client, a *autolinksArguments) (any, error) {
				return c.createAutolink(ctx, a)
			})},
		{Descriptor: autolinksDelete, Handler: autolinksHandler(autolinksDelete.ID, checkAutolinkID,
			func(ctx context.Context, c *Client, a *autolinksArguments) (any, error) {
				return c.deleteAutolink(ctx, a.ID)
			})},
	}
}

// autolinksArguments holds the arguments of every autolink tool; each input schema admits only its own.
type autolinksArguments struct {
	ID             int64  `json:"id"`
	KeyPrefix      string `json:"key_prefix"`
	URLTemplate    string `json:"url_template"`
	IsAlphanumeric *bool  `json:"is_alphanumeric"`
}

// autolinksHandler decodes and checks the arguments and the repository before a credential is resolved, so a
// refused request never becomes a provider call.
func autolinksHandler(id string, check func(*autolinksArguments) error,
	call func(context.Context, *Client, *autolinksArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments autolinksArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if check != nil {
			if err := check(&arguments); err != nil {
				return nil, err
			}
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

func checkAutolinkID(a *autolinksArguments) error {
	if a.ID <= 0 {
		return invalidRequest("id must be a positive autolink id")
	}
	return nil
}

func checkAutolinkCreate(a *autolinksArguments) error {
	if a.KeyPrefix == "" || len(a.KeyPrefix) > maxAutolinkPrefixLength {
		return invalidRequest("key_prefix must be 1 to 255 characters")
	}
	if err := checkText("key_prefix", a.KeyPrefix); err != nil {
		return err
	}
	return checkAutolinkTemplate(a.URLTemplate)
}

// checkAutolinkTemplate keeps a template to a plain https address with exactly one <num>; it only parses the
// text and never resolves or requests the address.
func checkAutolinkTemplate(template string) error {
	if template == "" || len(template) > maxAutolinkTemplateLength {
		return invalidRequest("url_template must be 1 to 2048 characters")
	}
	if err := checkText("url_template", template); err != nil {
		return err
	}
	if strings.Count(template, autolinkPlaceholder) != 1 {
		return invalidRequest("url_template must contain the placeholder <num> exactly once")
	}
	parsed, err := url.Parse(template)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" {
		return invalidRequest("url_template must be an https address")
	}
	if parsed.User != nil || strings.Contains(parsed.Host, "@") {
		return invalidRequest("url_template must not contain user information")
	}
	return nil
}

const (
	autolinksReadPermission = "GitHub refused this token the autolinks of this repository; reading them needs " +
		"repo on a classic token, as a repository administrator, or Administration: read on a fine-grained token"
	autolinksChangePermission = "GitHub refused this change of an autolink; it needs repo on a classic token, " +
		"as a repository administrator, or Administration: read and write on a fine-grained token"
)

func (c *Client) autolinkPath(id int64) string {
	if id == 0 {
		return c.repoPath("autolinks")
	}
	return c.repoPath("autolinks/" + strconv.FormatInt(id, 10))
}

type autolinkRaw struct {
	ID             *int64  `json:"id"`
	KeyPrefix      *string `json:"key_prefix"`
	URLTemplate    *string `json:"url_template"`
	IsAlphanumeric *bool   `json:"is_alphanumeric"`
	UpdatedAt      *string `json:"updated_at"`
}

// Autolink is the view of one autolink; every documented member of GitHub's autolink is kept.
type Autolink struct {
	ID             int64   `json:"id"`
	KeyPrefix      string  `json:"key_prefix"`
	URLTemplate    string  `json:"url_template"`
	IsAlphanumeric bool    `json:"is_alphanumeric"`
	UpdatedAt      *string `json:"updated_at,omitempty"`
}

func (r *autolinkRaw) project() (Autolink, bool) {
	if r.ID == nil || r.KeyPrefix == nil || r.URLTemplate == nil || r.IsAlphanumeric == nil {
		return Autolink{}, false
	}
	return Autolink{ID: *r.ID, KeyPrefix: *r.KeyPrefix, URLTemplate: *r.URLTemplate,
		IsAlphanumeric: *r.IsAlphanumeric, UpdatedAt: nameText(r.UpdatedAt)}, true
}

// AutolinkList is the whole autolink list of a repository.
type AutolinkList struct {
	Autolinks []Autolink `json:"autolinks"`
}

func (c *Client) listAutolinks(ctx context.Context) (*AutolinkList, error) {
	const op = "list autolinks"
	var raw []autolinkRaw
	if err := c.rest(ctx, op, c.autolinkPath(0), &raw); err != nil {
		return nil, actionsFailure(err, autolinksReadPermission)
	}
	result := &AutolinkList{Autolinks: make([]Autolink, 0, len(raw))}
	for i := range raw {
		autolink, ok := raw[i].project()
		if !ok {
			return nil, invalidEntry(op, "an autolink")
		}
		result.Autolinks = append(result.Autolinks, autolink)
	}
	return result, nil
}

func (c *Client) getAutolink(ctx context.Context, id int64) (*Autolink, error) {
	const op = "get autolink"
	var raw autolinkRaw
	if err := c.rest(ctx, op, c.autolinkPath(id), &raw); err != nil {
		return nil, actionsFailure(err, autolinksReadPermission)
	}
	autolink, ok := raw.project()
	if !ok {
		return nil, invalidEntry(op, "an autolink")
	}
	return &autolink, nil
}

func (c *Client) createAutolink(ctx context.Context, a *autolinksArguments) (*Autolink, error) {
	const op = "create autolink"
	body := map[string]any{"key_prefix": a.KeyPrefix, "url_template": a.URLTemplate}
	if a.IsAlphanumeric != nil {
		body["is_alphanumeric"] = *a.IsAlphanumeric
	}
	var raw autolinkRaw
	if err := c.restChange(ctx, op, http.MethodPost, c.autolinkPath(0), body, &raw); err != nil {
		return nil, actionsFailure(err, autolinksChangePermission)
	}
	autolink, ok := raw.project()
	if !ok {
		return nil, invalidResponse(op, true)
	}
	return &autolink, nil
}

// deleteAutolink binds the id to the repository by reading it first, so an id that is not one of this
// repository's autolinks never reaches the DELETE; the DELETE itself is sent exactly once.
func (c *Client) deleteAutolink(ctx context.Context, id int64) (any, error) {
	const op = "delete autolink"
	if _, err := c.getAutolink(ctx, id); err != nil {
		return nil, err
	}
	if err := c.restChange(ctx, op, http.MethodDelete, c.autolinkPath(id), nil, nil); err != nil {
		return nil, actionsFailure(err, autolinksChangePermission)
	}
	return map[string]any{"deleted": true, "id": id}, nil
}

// autolinksSubject names the autolink a repository path below /autolinks addresses.
func autolinksSubject(path string) string {
	if path == "autolinks" {
		return "the autolinks of this repository"
	}
	rest, ok := strings.CutPrefix(path, "autolinks/")
	if !ok {
		return ""
	}
	if id, err := strconv.ParseInt(rest, 10, 64); err != nil || id <= 0 {
		return "an autolink"
	}
	return "autolink " + rest
}
