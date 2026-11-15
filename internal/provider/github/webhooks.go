package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The existing webhooks of a repository or an organization: their list and detail, pausing and resuming one,
// deleting one, and the delivery log of one. Every call addresses exactly one of repository or organization,
// the same exclusive selection the ruleset tools use. The tools create no webhook, change no target address,
// configuration, or secret, and send no ping, test, or redelivery, since each of those would send a request
// to an address. Pausing and resuming send only the active member. The pause, resume, and delete tools are
// offered only where a connection's tools list names them, and each first reads the webhook below the chosen
// target, which binds hook_id to that target, and then sends one request that is never repeated.
//
// The target address, the headers, and the payloads of a webhook are untrusted data and carry credentials, so
// they are shown masked and cut, never the secret, and never fetched; see webhookmask.go.

// webhookSensitivity classifies webhook data: target addresses and delivery payloads describe integrations.
const webhookSensitivity = "github-webhooks"

const defaultWebhooksLimit = 30

var webhookCursorPattern = regexp.MustCompile(`^[A-Za-z0-9_.=-]{1,200}$`)

const (
	webhookIDSchema = actionsIDSchema
	// webhookIDMax mirrors the bound of webhookIDSchema.
	webhookIDMax = 9007199254740991
)

const webhookConfigSchema = `{"type":"object","properties":{"url":{"type":"string"},"url_masked":{"type":"boolean"},` +
	`"url_truncated":{"type":"boolean"},"content_type":{"type":"string"},"insecure_ssl":{"type":"string"}},` +
	`"additionalProperties":false}`

const webhookLastResponseSchema = `{"type":"object","properties":{"code":{"type":"integer"},` +
	`"status":{"type":"string"},"message":{"type":"string"}},"additionalProperties":false}`

var webhookProperties = `"id":{"type":"integer"},"type":{"type":"string"},"name":{"type":"string"},` +
	`"active":{"type":"boolean"},"events":{"type":"array","items":{"type":"string"}},` +
	`"config":` + webhookConfigSchema + `,"updated_at":{"type":"string"},"created_at":{"type":"string"},` +
	`"last_response":` + webhookLastResponseSchema + `,"links":` + linksSchema(webhookLinkKeys)

const webhookRequired = `"required":["id","active"],"additionalProperties":false`

var webhookLinkKeys = []string{"url", "test_url", "ping_url", "deliveries_url"}

var webhookListOutput = `{"type":"object","properties":{"webhooks":{"type":"array","items":{"type":"object",` +
	`"properties":{` + webhookProperties + `},` + webhookRequired + `}},"next_cursor":{"type":"string"},` +
	`"has_more":{"type":"boolean"},` + settingTargetProperties + `},"required":["webhooks","has_more"],` +
	`"additionalProperties":false}`

var webhookOutput = `{"type":"object","properties":{` + webhookProperties + `,` + settingTargetProperties + `},` +
	webhookRequired + `}`

var webhookDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},"hook_id":{"type":"integer"},` +
	settingTargetProperties + `},"required":["deleted","hook_id"],"additionalProperties":false}`

const webhookHeadersProperties = `"headers":{"type":"object","additionalProperties":{"type":"string"}},` +
	`"headers_omitted":{"type":"integer"},"payload":{"type":"string"},"payload_truncated":{"type":"boolean"}`

var webhookDeliverySummaryProperties = `"id":{"type":"integer"},"guid":{"type":"string"},` +
	`"delivered_at":{"type":"string"},"redelivery":{"type":"boolean"},"duration":{"type":"number"},` +
	`"status":{"type":"string"},"status_code":{"type":"integer"},"event":{"type":"string"},` +
	`"action":{"type":"string"},"installation_id":{"type":"integer"},"repository_id":{"type":"integer"},` +
	`"throttled_at":{"type":"string"}`

var webhookDeliveryProperties = webhookDeliverySummaryProperties + `,"url":{"type":"string"},` +
	`"url_masked":{"type":"boolean"},"url_truncated":{"type":"boolean"},` +
	`"request":{"type":"object","properties":{` + webhookHeadersProperties + `},"additionalProperties":false},` +
	`"response":{"type":"object","properties":{` + webhookHeadersProperties + `},"additionalProperties":false}`

const webhookDeliveryRequired = `"required":["id"],"additionalProperties":false`

var webhookDeliveryListOutput = `{"type":"object","properties":{"deliveries":{"type":"array","items":{"type":"object",` +
	`"properties":{` + webhookDeliverySummaryProperties + `},` + webhookDeliveryRequired + `}},` +
	`"hook_id":{"type":"integer"},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"},` +
	settingTargetProperties + `},"required":["deliveries","hook_id","has_more"],"additionalProperties":false}`

var webhookDeliveryOutput = `{"type":"object","properties":{` + webhookDeliveryProperties + `,"hook_id":{"type":"integer"},` +
	settingTargetProperties + `},"required":["id","hook_id"],"additionalProperties":false}`

var webhookIDArgument = capability.Argument{Name: "hook_id", Description: "Numeric identifier of the webhook, as " +
	"webhooks.list reports it; the webhook is read below the chosen repository or organization first", Required: true}

var webhookTargetArguments = []capability.Argument{
	{Name: "repository", Description: "Repository as OWNER/REPO whose webhooks this call addresses; exactly one of " +
		"repository or organization is required, never both; must lie inside the targets when the connection " +
		"lists any"},
	{Name: "organization", Description: "Organization as orgs/LOGIN whose webhooks this call addresses; exactly one " +
		"of repository or organization is required, never both; must be an owner target the connection allows " +
		"when it lists any"},
}

var webhookMemberFields = []capability.Field{
	{Name: "id", Description: "Numeric identifier of the webhook"},
	{Name: "type", Description: "Kind of the hook such as Repository or Organization, untrusted data"},
	{Name: "name", Description: "Name of the hook, web for a webhook, untrusted data"},
	{Name: "active", Description: "True while GitHub sends deliveries to the webhook"},
	{Name: "events", Description: "Events the webhook subscribes to, untrusted data"},
	{Name: "config", Description: "Configuration without the secret: url (the target address, untrusted data, never " +
		"fetched, with userinfo, every path segment other than a common route word such as hooks or services, and every query value " +
		"replaced by ***, and cut), url_masked (true when anything was replaced), url_truncated, content_type, " +
		"and insecure_ssl; the secret and every other configuration member are left out"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "created_at", Description: "Creation time"},
	{Name: "last_response", Description: "Outcome of the last delivery: code, status, and message, untrusted data"},
	{Name: "links", Description: "REST addresses of the webhook by their GitHub names, url, test_url, ping_url, and " +
		"deliveries_url, untrusted data, never fetched"},
}

func webhookFieldsWith(fields ...capability.Field) []capability.Field {
	return append(fields, settingTargetFields...)
}

func webhookTags(group, verb string) []string {
	return []string{"github", "repositories", group, verb, "governance"}
}

var webhooksList = capability.Descriptor{
	ID:      Provider + ".webhooks.list",
	Version: 1,
	Title:   "List GitHub webhooks",
	Description: "List one bounded batch of the existing webhooks of a repository or, with organization instead, of " +
		"an organization a connection allows, with their events, state, and masked target address but never " +
		"their secret; exactly one of repository or organization is required",
	Tags:         webhookTags("webhooks", "list"),
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, webhookSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(settingTargetKeys + `,` + pagingKeys),
	OutputSchema: json.RawMessage(webhookListOutput),
	Arguments:    append(append([]capability.Argument{}, webhookTargetArguments...), pagingArguments...),
	Fields: webhookFieldsWith(
		capability.Field{Name: "webhooks", Description: "Webhooks with the members of github.webhooks.get; untrusted data"},
		pagingFields[0], pagingFields[1]),
	Examples: []capability.Example{
		{Description: "List the webhooks of a repository", Arguments: json.RawMessage(`{"repository":"octo-org/example"}`)},
		{Description: "List the webhooks of an organization", Arguments: json.RawMessage(`{"organization":"orgs/octo-org"}`)},
	},
}

var webhooksGet = capability.Descriptor{
	ID:      Provider + ".webhooks.get",
	Version: 1,
	Title:   "Get a GitHub webhook",
	Description: "Read one existing webhook of a repository or, with organization instead, of an organization a " +
		"connection allows, with its events, state, last response, and masked target address but never its " +
		"secret; exactly one of repository or organization is required",
	Tags:         webhookTags("webhooks", "get"),
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, webhookSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(settingTargetKeys+`,"hook_id":`+webhookIDSchema, "hook_id"),
	OutputSchema: json.RawMessage(webhookOutput),
	Arguments:    append([]capability.Argument{webhookIDArgument}, webhookTargetArguments...),
	Fields:       webhookFieldsWith(webhookMemberFields...),
	Examples: []capability.Example{{Description: "Read one webhook of an organization",
		Arguments: json.RawMessage(`{"organization":"orgs/octo-org","hook_id":12}`)}},
}

var webhooksPause = capability.Descriptor{
	ID:      Provider + ".webhooks.pause",
	Version: 1,
	Title:   "Pause a GitHub webhook",
	Description: "Pause one existing webhook of a repository or, with organization instead, of an organization a " +
		"connection allows, so GitHub sends no further deliveries; the webhook is read below the chosen target " +
		"first, and only its active member is sent, never its configuration; idempotent; exactly one of " +
		"repository or organization is required; offered only where a connection's tools list names it",
	Tags:                  webhookTags("webhooks", "pause"),
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, webhookSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(settingTargetKeys+`,"hook_id":`+webhookIDSchema, "hook_id"),
	OutputSchema:          json.RawMessage(webhookOutput),
	Arguments:             append([]capability.Argument{webhookIDArgument}, webhookTargetArguments...),
	Fields:                webhookFieldsWith(webhookMemberFields...),
	Examples: []capability.Example{{Description: "Pause a repository webhook",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","hook_id":12}`)}},
}

var webhooksResume = capability.Descriptor{
	ID:      Provider + ".webhooks.resume",
	Version: 1,
	Title:   "Resume a GitHub webhook",
	Description: "Resume one existing, paused webhook of a repository or, with organization instead, of an " +
		"organization a connection allows, so GitHub sends deliveries again to its existing target; the webhook " +
		"is read below the chosen target first, and only its active member is sent, never its configuration; " +
		"idempotent; exactly one of repository or organization is required; offered only where a connection's " +
		"tools list names it",
	Tags:                  webhookTags("webhooks", "resume"),
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, webhookSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(settingTargetKeys+`,"hook_id":`+webhookIDSchema, "hook_id"),
	OutputSchema:          json.RawMessage(webhookOutput),
	Arguments:             append([]capability.Argument{webhookIDArgument}, webhookTargetArguments...),
	Fields:                webhookFieldsWith(webhookMemberFields...),
	Examples: []capability.Example{{Description: "Resume a repository webhook",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","hook_id":12}`)}},
}

var webhooksDelete = capability.Descriptor{
	ID:      Provider + ".webhooks.delete",
	Version: 1,
	Title:   "Delete a GitHub webhook",
	Description: "Delete one existing webhook of a repository or, with organization instead, of an organization a " +
		"connection allows, permanently, with its delivery log; the webhook is read below the chosen target first; " +
		"exactly one of repository or organization is required; offered only where a connection's tools list " +
		"names it",
	Tags:                  webhookTags("webhooks", "delete"),
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, webhookSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(settingTargetKeys+`,"hook_id":`+webhookIDSchema, "hook_id"),
	OutputSchema:          json.RawMessage(webhookDeletedOutput),
	Arguments:             append([]capability.Argument{webhookIDArgument}, webhookTargetArguments...),
	Fields: webhookFieldsWith(
		capability.Field{Name: "deleted", Description: "True once GitHub deleted the webhook"},
		capability.Field{Name: "hook_id", Description: "Identifier of the deleted webhook"}),
	Examples: []capability.Example{{Description: "Delete a repository webhook",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","hook_id":12}`)}},
}

var webhookDeliveryFields = []capability.Field{
	{Name: "id", Description: "Numeric identifier of the delivery"},
	{Name: "guid", Description: "Globally unique identifier of the delivery, untrusted data"},
	{Name: "delivered_at", Description: "Time of the delivery"},
	{Name: "redelivery", Description: "True when the delivery repeats an earlier one"},
	{Name: "duration", Description: "Seconds the delivery took"},
	{Name: "status", Description: "Outcome such as OK or a failure text, untrusted data"},
	{Name: "status_code", Description: "HTTP status code of the target's answer"},
	{Name: "event", Description: "Event that caused the delivery, untrusted data"},
	{Name: "action", Description: "Action of the event, untrusted data"},
	{Name: "installation_id", Description: "Identifier of the GitHub App installation of the event"},
	{Name: "repository_id", Description: "Identifier of the repository of the event"},
	{Name: "throttled_at", Description: "Time GitHub throttled the delivery"},
}

var webhookDeliveriesList = capability.Descriptor{
	ID:      Provider + ".webhook-deliveries.list",
	Version: 1,
	Title:   "List GitHub webhook deliveries",
	Description: "List one bounded batch of the recent deliveries of one webhook of a repository or, with " +
		"organization instead, of an organization a connection allows, newest first, without headers or payloads; " +
		"the webhook is read below the chosen target first; exactly one of repository or organization is required",
	Tags:         webhookTags("webhook-deliveries", "list"),
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, webhookSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(settingTargetKeys+`,"hook_id":`+webhookIDSchema+`,`+pagingKeys, "hook_id"),
	OutputSchema: json.RawMessage(webhookDeliveryListOutput),
	Arguments: append(append([]capability.Argument{webhookIDArgument}, webhookTargetArguments...),
		pagingArguments...),
	Fields: webhookFieldsWith(
		capability.Field{Name: "deliveries", Description: "Deliveries with id, guid, delivered_at, redelivery, duration, status, " +
			"status_code, event, action, installation_id, repository_id, and throttled_at; untrusted data"},
		capability.Field{Name: "hook_id", Description: "Identifier of the webhook the deliveries belong to"},
		pagingFields[0], pagingFields[1]),
	Examples: []capability.Example{{Description: "List the deliveries of a repository webhook",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","hook_id":12}`)}},
}

var webhookDeliveriesGet = capability.Descriptor{
	ID:      Provider + ".webhook-deliveries.get",
	Version: 1,
	Title:   "Get a GitHub webhook delivery",
	Description: "Read one delivery of one webhook of a repository or, with organization instead, of an " +
		"organization a connection allows, with the masked address it went to and, cut and without any header " +
		"that can carry a credential, the headers and payload of the request and the answer; exactly one of " +
		"repository or organization is required",
	Tags:         webhookTags("webhook-deliveries", "get"),
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, webhookSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(settingTargetKeys+`,"hook_id":`+webhookIDSchema+`,"delivery_id":`+webhookIDSchema, "hook_id", "delivery_id"),
	OutputSchema: json.RawMessage(webhookDeliveryOutput),
	Arguments: append([]capability.Argument{webhookIDArgument, {Name: "delivery_id", Description: "Numeric " +
		"identifier of the delivery, as webhook-deliveries.list reports it", Required: true}},
		webhookTargetArguments...),
	Fields: webhookFieldsWith(append(append([]capability.Field{}, webhookDeliveryFields...),
		capability.Field{Name: "hook_id", Description: "Identifier of the webhook the delivery belongs to"},
		capability.Field{Name: "url", Description: "Address the delivery was sent to, masked like config.url of " +
			"github.webhooks.get, untrusted data, never fetched"},
		capability.Field{Name: "url_masked", Description: "True when anything of url was replaced"},
		capability.Field{Name: "url_truncated", Description: "True when url was cut"},
		capability.Field{Name: "request", Description: "What GitHub sent: headers (name to value, at most 50, each " +
			"value cut; any header whose name suggests authorization, cookie, token, secret, signature, key, " +
			"password, credential, or session is left out and counted in headers_omitted) and payload (compact " +
			"JSON text cut at 2000 characters, payload_truncated says so); untrusted data"},
		capability.Field{Name: "response", Description: "What the target answered, in the same form as request; " +
			"untrusted data"})...),
	Examples: []capability.Example{{Description: "Read one delivery of a repository webhook",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","hook_id":12,"delivery_id":4096}`)}},
}

// webhooksReads and webhooksChanges are the tools of the webhooks. The reads are offered like every read; the
// changes stay unticked in every profile, since each is offered only where a connection's tools list names it.
var (
	webhooksReads   = []string{webhooksList.ID, webhooksGet.ID, webhookDeliveriesList.ID, webhookDeliveriesGet.ID}
	webhooksChanges = []string{webhooksPause.ID, webhooksResume.ID, webhooksDelete.ID}
)

// webhookArguments holds the arguments of every webhook tool; the input schema of each admits only its own.
type webhookArguments struct {
	HookID     *int64 `json:"hook_id"`
	DeliveryID *int64 `json:"delivery_id"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`

	page, perPage int
	after         string
	binding       []byte
}

func webhooksHandler(id string, check func(*webhookArguments, target) error,
	call func(context.Context, *Client, *webhookArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments webhookArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectRulesetTarget(resolved, raw)
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
		value, err := call(ctx, client, &arguments)
		return bound.rulesetLocate(value, err)
	}
}

// webhooksOperations binds every webhook tool to its handler.
func webhooksOperations() []capability.Operation {
	bind := func(d capability.Descriptor, check func(*webhookArguments, target) error,
		call func(context.Context, *Client, *webhookArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: d, Handler: webhooksHandler(d.ID, check, call)}
	}
	return []capability.Operation{
		bind(webhooksList, checkWebhooksList, func(ctx context.Context, c *Client, a *webhookArguments) (any, error) {
			return c.listWebhooks(ctx, a)
		}),
		bind(webhooksGet, checkWebhookID, func(ctx context.Context, c *Client, a *webhookArguments) (any, error) {
			return c.getWebhook(ctx, a)
		}),
		bind(webhooksPause, checkWebhookID, func(ctx context.Context, c *Client, a *webhookArguments) (any, error) {
			return c.setWebhookActive(ctx, a, false)
		}),
		bind(webhooksResume, checkWebhookID, func(ctx context.Context, c *Client, a *webhookArguments) (any, error) {
			return c.setWebhookActive(ctx, a, true)
		}),
		bind(webhooksDelete, checkWebhookID, func(ctx context.Context, c *Client, a *webhookArguments) (any, error) {
			return c.deleteWebhook(ctx, a)
		}),
		bind(webhookDeliveriesList, checkWebhookDeliveriesList, func(ctx context.Context, c *Client, a *webhookArguments) (any, error) {
			return c.listWebhookDeliveries(ctx, a)
		}),
		bind(webhookDeliveriesGet, checkWebhookDeliveryID, func(ctx context.Context, c *Client, a *webhookArguments) (any, error) {
			return c.getWebhookDelivery(ctx, a)
		}),
	}
}

// ---- checks

func checkWebhookID(a *webhookArguments, _ target) error {
	if a.HookID == nil || *a.HookID < 1 || *a.HookID > webhookIDMax {
		return invalidRequest("hook_id must be a positive whole number")
	}
	return nil
}

func checkWebhookDeliveryID(a *webhookArguments, bound target) error {
	if err := checkWebhookID(a, bound); err != nil {
		return err
	}
	if a.DeliveryID == nil || *a.DeliveryID < 1 || *a.DeliveryID > webhookIDMax {
		return invalidRequest("delivery_id must be a positive whole number")
	}
	return nil
}

func webhookLimit(a *webhookArguments) (int, error) {
	limit := a.Limit
	if limit == 0 {
		limit = defaultWebhooksLimit
	}
	if limit < 1 || limit > maxLimit {
		return 0, invalidRequest(fmt.Sprintf("limit must be between 1 and %d", maxLimit))
	}
	return limit, nil
}

func checkWebhooksList(a *webhookArguments, bound target) error {
	limit, err := webhookLimit(a)
	if err != nil {
		return err
	}
	a.binding = fingerprint("webhooks", "list", bound.String())
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

// checkWebhookDeliveriesList resolves the paging of the delivery log, which GitHub pages by its own cursor
// instead of by page number: the Qatlas cursor carries that cursor and the batch size.
func checkWebhookDeliveriesList(a *webhookArguments, bound target) error {
	if err := checkWebhookID(a, bound); err != nil {
		return err
	}
	limit, err := webhookLimit(a)
	if err != nil {
		return err
	}
	a.binding = fingerprint("webhook-deliveries", "list", bound.String(), *a.HookID)
	a.perPage = limit
	after, err := decodeCursor(a.binding, a.Cursor)
	if err != nil || after == "" {
		return err
	}
	size, cursor, ok := strings.Cut(after, ":")
	perPage, sizeErr := strconv.Atoi(size)
	if !ok || sizeErr != nil || perPage < 1 || perPage > maxLimit || !webhookCursorPattern.MatchString(cursor) {
		return invalidRequest("cursor is not a next_cursor of this list; start the list again without cursor")
	}
	a.perPage, a.after = perPage, cursor
	return nil
}

// ---- transport

// webhookPath is the route of the webhooks below the bound repository or organization, of one webhook, or of
// its deliveries.
func (c *Client) webhookPath(id int64, rest string) string {
	path := c.rulesetsPath("hooks")
	if id > 0 {
		path += "/" + strconv.FormatInt(id, 10)
	}
	return path + rest
}

// webhookPermission names what a refused webhook request needs without claiming what the configured token
// holds. The fine-grained organization permission is stated cautiously, since Qatlas cannot verify it
// against GitHub live.
func webhookPermission(t target, change bool) string {
	access, verb := "read", "reading them needs"
	if change {
		access, verb = "read and write", "changing them needs"
	}
	if t.kind == kindOwner {
		return "GitHub refused this token the webhooks of this organization; " + verb + " admin:org_hook on a " +
			"classic token, or, as far as GitHub documents it, Webhooks: " + access + " of the organization on a " +
			"fine-grained token"
	}
	return "GitHub refused this token the webhooks of this repository; " + verb + " admin:repo_hook or repo on a " +
		"classic token, or Webhooks: " + access + " on a fine-grained token"
}

const (
	webhookChangeRejected = "GitHub rejected the change of the webhook, so nothing was changed"
	webhookDeleteRejected = "GitHub refused to delete the webhook, so nothing was deleted"
)

func (c *Client) webhookFailure(err error, change bool, rejected string) error {
	return variableFailure(err, webhookPermission(c.target, change), "", rejected)
}

// ---- projections

type webhookConfigRaw struct {
	URL         *string         `json:"url"`
	ContentType *string         `json:"content_type"`
	InsecureSSL json.RawMessage `json:"insecure_ssl"`
}

type webhookLastResponseRaw struct {
	Code    *int64  `json:"code"`
	Status  *string `json:"status"`
	Message *string `json:"message"`
}

type webhookRaw struct {
	ID            *int64                  `json:"id"`
	Type          *string                 `json:"type"`
	Name          *string                 `json:"name"`
	Active        *bool                   `json:"active"`
	Events        []string                `json:"events"`
	Config        *webhookConfigRaw       `json:"config"`
	UpdatedAt     *string                 `json:"updated_at"`
	CreatedAt     *string                 `json:"created_at"`
	URL           *string                 `json:"url"`
	TestURL       *string                 `json:"test_url"`
	PingURL       *string                 `json:"ping_url"`
	DeliveriesURL *string                 `json:"deliveries_url"`
	LastResponse  *webhookLastResponseRaw `json:"last_response"`
}

// WebhookConfig is the configuration of a webhook without its secret. URL is the masked target address.
type WebhookConfig struct {
	URL          string `json:"url,omitempty"`
	URLMasked    bool   `json:"url_masked,omitempty"`
	URLTruncated bool   `json:"url_truncated,omitempty"`
	ContentType  string `json:"content_type,omitempty"`
	InsecureSSL  string `json:"insecure_ssl,omitempty"`
}

// WebhookLastResponse is the outcome of the last delivery of a webhook.
type WebhookLastResponse struct {
	Code    *int64 `json:"code,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// Webhook is one existing webhook. Every text of it is untrusted data and cut; its secret is never read into
// this view.
type Webhook struct {
	ID           int64                `json:"id"`
	Type         string               `json:"type,omitempty"`
	Name         string               `json:"name,omitempty"`
	Active       bool                 `json:"active"`
	Events       []string             `json:"events,omitempty"`
	Config       *WebhookConfig       `json:"config,omitempty"`
	UpdatedAt    *string              `json:"updated_at,omitempty"`
	CreatedAt    *string              `json:"created_at,omitempty"`
	LastResponse *WebhookLastResponse `json:"last_response,omitempty"`
	Links        Links                `json:"links,omitempty"`
}

const maxWebhookEvents = 200

// insecureSSLText reads the insecure_ssl member, which GitHub documents as a string or a number.
func insecureSSLText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

// view projects a webhook, or reports false for one without a usable identifier or state.
func (w *webhookRaw) view() (Webhook, bool) {
	if w.ID == nil || *w.ID < 1 || w.Active == nil || len(w.Events) > maxWebhookEvents {
		return Webhook{}, false
	}
	hook := Webhook{ID: *w.ID, Active: *w.Active, UpdatedAt: nameText(w.UpdatedAt), CreatedAt: nameText(w.CreatedAt)}
	if text := nameText(w.Type); text != nil {
		hook.Type = *text
	}
	if text := nameText(w.Name); text != nil {
		hook.Name = *text
	}
	for _, event := range w.Events {
		if text := nameText(&event); text != nil {
			hook.Events = append(hook.Events, *text)
		}
	}
	if w.Config != nil {
		config := WebhookConfig{InsecureSSL: insecureSSLText(w.Config.InsecureSSL)}
		if w.Config.URL != nil && *w.Config.URL != "" {
			config.URL, config.URLMasked, config.URLTruncated = maskWebhookURL(*w.Config.URL)
		}
		if text := nameText(w.Config.ContentType); text != nil {
			config.ContentType = *text
		}
		if config != (WebhookConfig{}) {
			hook.Config = &config
		}
	}
	if r := w.LastResponse; r != nil && (r.Code != nil || r.Status != nil || r.Message != nil) {
		last := &WebhookLastResponse{Code: r.Code}
		if text := nameText(r.Status); text != nil {
			last.Status = *text
		}
		if text := longText(r.Message); text != nil {
			last.Message = *text
		}
		hook.LastResponse = last
	}
	links := Links{}
	for key, value := range map[string]*string{"url": w.URL, "test_url": w.TestURL, "ping_url": w.PingURL,
		"deliveries_url": w.DeliveriesURL} {
		if a := address(value); a != nil {
			links[key] = *a
		}
	}
	if len(links) > 0 {
		hook.Links = links
	}
	return hook, true
}

// WebhookList is one batch of webhooks.
type WebhookList struct {
	Webhooks   []Webhook `json:"webhooks"`
	NextCursor string    `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more"`
}

// WebhookDeleted is the answer to a deleted webhook.
type WebhookDeleted struct {
	Deleted bool  `json:"deleted"`
	HookID  int64 `json:"hook_id"`
}

type webhookDeliveryRaw struct {
	ID             *int64          `json:"id"`
	GUID           *string         `json:"guid"`
	DeliveredAt    *string         `json:"delivered_at"`
	Redelivery     *bool           `json:"redelivery"`
	Duration       *float64        `json:"duration"`
	Status         *string         `json:"status"`
	StatusCode     *int64          `json:"status_code"`
	Event          *string         `json:"event"`
	Action         *string         `json:"action"`
	InstallationID *int64          `json:"installation_id"`
	RepositoryID   *int64          `json:"repository_id"`
	ThrottledAt    *string         `json:"throttled_at"`
	URL            *string         `json:"url"`
	Request        *webhookSideRaw `json:"request"`
	Response       *webhookSideRaw `json:"response"`
}

// WebhookDeliverySummary is one entry of the delivery log, without headers and payloads.
type WebhookDeliverySummary struct {
	ID             int64    `json:"id"`
	GUID           string   `json:"guid,omitempty"`
	DeliveredAt    *string  `json:"delivered_at,omitempty"`
	Redelivery     *bool    `json:"redelivery,omitempty"`
	Duration       *float64 `json:"duration,omitempty"`
	Status         string   `json:"status,omitempty"`
	StatusCode     *int64   `json:"status_code,omitempty"`
	Event          string   `json:"event,omitempty"`
	Action         string   `json:"action,omitempty"`
	InstallationID *int64   `json:"installation_id,omitempty"`
	RepositoryID   *int64   `json:"repository_id,omitempty"`
	ThrottledAt    *string  `json:"throttled_at,omitempty"`
}

// WebhookDelivery is one delivery with its masked address and its cut request and answer.
type WebhookDelivery struct {
	WebhookDeliverySummary
	HookID       int64                `json:"hook_id"`
	URL          string               `json:"url,omitempty"`
	URLMasked    bool                 `json:"url_masked,omitempty"`
	URLTruncated bool                 `json:"url_truncated,omitempty"`
	Request      *WebhookDeliverySide `json:"request,omitempty"`
	Response     *WebhookDeliverySide `json:"response,omitempty"`
}

func (d *webhookDeliveryRaw) summary() (WebhookDeliverySummary, bool) {
	if d.ID == nil || *d.ID < 1 {
		return WebhookDeliverySummary{}, false
	}
	s := WebhookDeliverySummary{ID: *d.ID, DeliveredAt: nameText(d.DeliveredAt), Redelivery: d.Redelivery,
		Duration: d.Duration, StatusCode: d.StatusCode, InstallationID: d.InstallationID,
		RepositoryID: d.RepositoryID, ThrottledAt: nameText(d.ThrottledAt)}
	for target, source := range map[*string]*string{&s.GUID: d.GUID, &s.Status: d.Status, &s.Event: d.Event,
		&s.Action: d.Action} {
		if text := nameText(source); text != nil {
			*target = *text
		}
	}
	return s, true
}

// WebhookDeliveryList is one batch of the delivery log of a webhook.
type WebhookDeliveryList struct {
	Deliveries []WebhookDeliverySummary `json:"deliveries"`
	HookID     int64                    `json:"hook_id"`
	NextCursor string                   `json:"next_cursor,omitempty"`
	HasMore    bool                     `json:"has_more"`
}

// ---- reads and changes

func (c *Client) listWebhooks(ctx context.Context, a *webhookArguments) (*WebhookList, error) {
	const op = "list webhooks"
	var raw []webhookRaw
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	hasNext, err := c.restPage(ctx, op, c.webhookPath(0, ""), query, &raw)
	if err != nil {
		return nil, c.webhookFailure(err, false, "")
	}
	result := &WebhookList{Webhooks: make([]Webhook, 0, len(raw))}
	for i := range raw {
		hook, ok := raw[i].view()
		if !ok {
			return nil, invalidEntry(op, "a webhook")
		}
		result.Webhooks = append(result.Webhooks, hook)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

// readWebhook reads one webhook below the bound target. It is the evidence that binds hook_id to the chosen
// repository or organization, and refuses an answer that names another webhook.
func (c *Client) readWebhook(ctx context.Context, op string, id int64) (Webhook, error) {
	var raw webhookRaw
	if err := c.rest(ctx, op, c.webhookPath(id, ""), &raw); err != nil {
		return Webhook{}, c.webhookFailure(err, false, "")
	}
	hook, ok := raw.view()
	if !ok || hook.ID != id {
		return Webhook{}, invalidEntry(op, "a webhook")
	}
	return hook, nil
}

func (c *Client) getWebhook(ctx context.Context, a *webhookArguments) (*Webhook, error) {
	hook, err := c.readWebhook(ctx, "get webhook", *a.HookID)
	if err != nil {
		return nil, err
	}
	return &hook, nil
}

// setWebhookActive sends the one PATCH that pauses or resumes a webhook, carrying the active member alone, so
// neither the target address nor the configuration nor the secret can change.
func (c *Client) setWebhookActive(ctx context.Context, a *webhookArguments, active bool) (*Webhook, error) {
	op := "pause webhook"
	if active {
		op = "resume webhook"
	}
	if _, err := c.readWebhook(ctx, op, *a.HookID); err != nil {
		return nil, err
	}
	var raw webhookRaw
	if err := c.restChange(ctx, op, http.MethodPatch, c.webhookPath(*a.HookID, ""),
		map[string]bool{"active": active}, &raw); err != nil {
		return nil, c.webhookFailure(err, true, webhookChangeRejected)
	}
	hook, ok := raw.view()
	if !ok || hook.ID != *a.HookID {
		return nil, invalidResponse(op, true)
	}
	return &hook, nil
}

func (c *Client) deleteWebhook(ctx context.Context, a *webhookArguments) (*WebhookDeleted, error) {
	const op = "delete webhook"
	if _, err := c.readWebhook(ctx, op, *a.HookID); err != nil {
		return nil, err
	}
	if err := c.restChange(ctx, op, http.MethodDelete, c.webhookPath(*a.HookID, ""), nil, nil); err != nil {
		return nil, c.webhookFailure(err, true, webhookDeleteRejected)
	}
	return &WebhookDeleted{Deleted: true, HookID: *a.HookID}, nil
}

// nextDeliveryCursor reads the GitHub cursor of the following page from the Link response header of the
// delivery log, which GitHub pages by cursor; it is empty when no page follows or the cursor is unusable.
func nextDeliveryCursor(header http.Header) string {
	for _, part := range strings.Split(header.Get("Link"), ",") {
		fields := strings.SplitN(part, ";", 2)
		if len(fields) != 2 || strings.TrimSpace(fields[1]) != `rel="next"` {
			continue
		}
		address := strings.Trim(strings.TrimSpace(fields[0]), "<>")
		parsed, err := url.Parse(address)
		if err != nil {
			return ""
		}
		if cursor := parsed.Query().Get("cursor"); webhookCursorPattern.MatchString(cursor) {
			return cursor
		}
	}
	return ""
}

func (c *Client) listWebhookDeliveries(ctx context.Context, a *webhookArguments) (*WebhookDeliveryList, error) {
	const op = "list webhook deliveries"
	if _, err := c.readWebhook(ctx, op, *a.HookID); err != nil {
		return nil, err
	}
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}}
	if a.after != "" {
		query.Set("cursor", a.after)
	}
	var raw []webhookDeliveryRaw
	var header http.Header
	path := c.webhookPath(*a.HookID, "/deliveries") + "?" + query.Encode()
	if err := c.do(ctx, op, http.MethodGet, c.endpoints.rest+path, nil, &raw, false, &header, nil); err != nil {
		return nil, c.webhookFailure(err, false, "")
	}
	result := &WebhookDeliveryList{Deliveries: make([]WebhookDeliverySummary, 0, len(raw)), HookID: *a.HookID}
	for i := range raw {
		delivery, ok := raw[i].summary()
		if !ok {
			return nil, invalidEntry(op, "a webhook delivery")
		}
		result.Deliveries = append(result.Deliveries, delivery)
	}
	if next := nextDeliveryCursor(header); next != "" {
		result.HasMore = true
		result.NextCursor = encodeCursor(a.binding, strconv.Itoa(a.perPage)+":"+next)
	}
	return result, nil
}

func (c *Client) getWebhookDelivery(ctx context.Context, a *webhookArguments) (*WebhookDelivery, error) {
	const op = "get webhook delivery"
	if _, err := c.readWebhook(ctx, op, *a.HookID); err != nil {
		return nil, err
	}
	var raw webhookDeliveryRaw
	path := c.webhookPath(*a.HookID, "/deliveries/"+strconv.FormatInt(*a.DeliveryID, 10))
	if err := c.rest(ctx, op, path, &raw); err != nil {
		return nil, c.webhookFailure(err, false, "")
	}
	summary, ok := raw.summary()
	if !ok || summary.ID != *a.DeliveryID {
		return nil, invalidEntry(op, "a webhook delivery")
	}
	delivery := &WebhookDelivery{WebhookDeliverySummary: summary, HookID: *a.HookID}
	if raw.URL != nil && *raw.URL != "" {
		delivery.URL, delivery.URLMasked, delivery.URLTruncated = maskWebhookURL(*raw.URL)
	}
	if delivery.Request, ok = raw.Request.view(); !ok {
		return nil, invalidEntry(op, "a webhook delivery")
	}
	if delivery.Response, ok = raw.Response.view(); !ok {
		return nil, invalidEntry(op, "a webhook delivery")
	}
	return delivery, nil
}

// webhooksSubject names the webhook or delivery a path below a repository or an organization addresses:
// hooks, hooks/ID, hooks/ID/deliveries, or hooks/ID/deliveries/ID.
func webhooksSubject(path string) string {
	rest, ok := strings.CutPrefix(path, "hooks")
	if !ok {
		return ""
	}
	rest, _, _ = strings.Cut(rest, "?")
	if rest == "" {
		return "the webhooks"
	}
	parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if !strings.HasPrefix(rest, "/") || err != nil || id < 1 {
		return ""
	}
	switch {
	case len(parts) == 1:
		return "webhook " + parts[0]
	case len(parts) == 2 && parts[1] == "deliveries":
		return "the deliveries of webhook " + parts[0]
	case len(parts) == 3 && parts[1] == "deliveries":
		if delivery, err := strconv.ParseInt(parts[2], 10, 64); err == nil && delivery > 0 {
			return "delivery " + parts[2] + " of webhook " + parts[0]
		}
	}
	return "webhook " + parts[0]
}
