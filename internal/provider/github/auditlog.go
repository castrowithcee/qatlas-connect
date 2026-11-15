package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The audit log tool reads one bounded batch of the audit log of an organization and nothing else: the
// enterprise audit log and the audit log streams are not reachable. The route is fixed and the organization
// is an owner target checked against the targets and exclusions before a secret is read. GitHub documents the
// route in its Enterprise Cloud description only, so a plan without it answers 403 or 404, which ends as the
// state unavailable with a fixed reason. An entry gives only the members GitHub documents for it; every other
// key is dropped. Every text of an entry is untrusted data, bounded, and no address is ever fetched. The phrase
// is passed on as text and never interpreted. The next batch is read through the cursor GitHub names in its
// Link header; the address itself is never requested.

const (
	auditLogSensitivity = "github-audit-log"
	// auditLogMaxPhrase bounds the search phrase in runes.
	auditLogMaxPhrase = 500
	// auditLogMaxAfter bounds the GitHub cursor a Qatlas cursor carries.
	auditLogMaxAfter = 600

	auditLogUnavailableState  = "unavailable"
	auditLogUnavailableReason = "GitHub did not give the audit log of this organization: the plan may not offer " +
		"the audit log through the API, the token may lack the audit log right, the account behind the token may " +
		"not be an owner of the organization, or the organization may not exist"
	auditLogPermission = "GitHub refused this token the audit log of this organization"
)

var auditLogRisk = capability.Risk{
	Effect:          capability.EffectRead,
	Idempotency:     capability.IdempotencySafe,
	Confirmation:    capability.ConfirmationNone,
	OpenWorld:       true,
	DataSensitivity: auditLogSensitivity,
}

var auditLogIncludes = []string{"web", "git", "all"}
var auditLogOrders = []string{"desc", "asc"}

// ---- Descriptor

const (
	auditLogPhraseSchema  = `{"type":"string","minLength":1,"maxLength":500}`
	auditLogIncludeSchema = `{"type":"string","enum":["web","git","all"]}`
	auditLogOrderSchema   = `{"type":"string","enum":["desc","asc"]}`
	auditLogScalar        = `{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"}]}`
	auditLogObject        = `{"type":"object","additionalProperties":` + auditLogScalar + `}`
	auditLogObjectList    = `{"type":"array","items":` + auditLogObject + `}`
	auditLogString        = `{"type":"string"}`
	auditLogInteger       = `{"type":"integer"}`
	auditLogBoolean       = `{"type":"boolean"}`
)

// auditLogEntries lists the members of an event in schema order: name, schema, and description. The texts are
// those GitHub documents; the free-form members are cut to flat values.
var auditLogEntries = [][3]string{
	{"@timestamp", auditLogInteger, "Time the event occurred, a Unix timestamp as GitHub gives it"},
	{"action", auditLogString, "Name of the action performed, such as user.login or repo.create" + appUntrusted},
	{"active", auditLogBoolean, "Whether the item was active after the action"},
	{"active_was", auditLogBoolean, "Whether the item was active before the action"},
	{"actor", auditLogString, "Account that performed the action" + appUntrusted},
	{"actor_id", auditLogInteger, "Id of the account that performed the action"},
	{"actor_location", `{"type":"object","properties":{"country_name":` + auditLogString +
		`},"additionalProperties":false}`, "Location of the actor, as country_name" + appUntrusted},
	{"data", auditLogObject, "Further details of the event as flat values: texts, numbers, and booleans; nested " +
		"values are left out and counted in data_truncated" + appUntrusted},
	{"data_truncated", auditLogBoolean, "True when data had more keys than are given or nested values"},
	{"org_id", auditLogInteger, "Id of the organization"},
	{"user_id", auditLogInteger, "Id of the affected account"},
	{"business_id", auditLogInteger, "Id of the enterprise"},
	{"blocked_user", auditLogString, "Account that was blocked" + appUntrusted},
	{"business", auditLogString, "Enterprise of the event" + appUntrusted},
	{"config", auditLogObjectList, "Configuration after the action, one flat object per entry" + appUntrusted},
	{"config_truncated", auditLogBoolean, "True when config had more entries or keys than are given"},
	{"config_was", auditLogObjectList, "Configuration before the action, one flat object per entry" + appUntrusted},
	{"config_was_truncated", auditLogBoolean, "True when config_was had more entries or keys than are given"},
	{"content_type", auditLogString, "Content type of the event" + appUntrusted},
	{"operation_type", auditLogString, "Operation type of the event" + appUntrusted},
	{"created_at", auditLogInteger, "Time the event was recorded, a Unix timestamp as GitHub gives it"},
	{"deploy_key_fingerprint", auditLogString, "Fingerprint of the deploy key" + appUntrusted},
	{"_document_id", auditLogString, "Unique identifier of the event" + appUntrusted},
	{"emoji", auditLogString, "Emoji of the event" + appUntrusted},
	{"events", auditLogObjectList, "Webhook events after the action, one flat object per entry" + appUntrusted},
	{"events_truncated", auditLogBoolean, "True when events had more entries or keys than are given"},
	{"events_were", auditLogObjectList, "Webhook events before the action, one flat object per entry" + appUntrusted},
	{"events_were_truncated", auditLogBoolean, "True when events_were had more entries or keys than are given"},
	{"explanation", auditLogString, "Explanation of the event" + appUntrusted},
	{"fingerprint", auditLogString, "Fingerprint of the key" + appUntrusted},
	{"hook_id", auditLogInteger, "Id of the webhook"},
	{"limited_availability", auditLogBoolean, "Whether the event has limited availability"},
	{"message", auditLogString, "Message of the event" + appUntrusted},
	{"name", auditLogString, "Name of the item the event concerns" + appUntrusted},
	{"old_user", auditLogString, "Previous account of the event" + appUntrusted},
	{"openssh_public_key", auditLogString, "Public SSH key of the event" + appUntrusted},
	{"org", auditLogString, "Organization of the event" + appUntrusted},
	{"previous_visibility", auditLogString, "Visibility before the action" + appUntrusted},
	{"read_only", auditLogBoolean, "Whether the item is read-only"},
	{"repo", auditLogString, "Name of the repository" + appUntrusted},
	{"repository", auditLogString, "Name of the repository" + appUntrusted},
	{"repository_public", auditLogBoolean, "Whether the repository is public"},
	{"target_login", auditLogString, "Login of the target account" + appUntrusted},
	{"team", auditLogString, "Team of the event" + appUntrusted},
	{"transport_protocol", auditLogInteger, "Protocol used to transfer Git data, as a number"},
	{"transport_protocol_name", auditLogString, "Name of the protocol used to transfer Git data" + appUntrusted},
	{"user", auditLogString, "Account affected by the action" + appUntrusted},
	{"visibility", auditLogString, "Repository visibility, such as public or private" + appUntrusted},
}

func auditLogEventProperties() string {
	parts := make([]string, len(auditLogEntries))
	for i, entry := range auditLogEntries {
		parts[i] = strconv.Quote(entry[0]) + ":" + entry[1]
	}
	return strings.Join(parts, ",")
}

func auditLogFields() []capability.Field {
	events := "Events of the batch, each with the members GitHub documents: "
	names := make([]string, len(auditLogEntries))
	for i, entry := range auditLogEntries {
		names[i] = entry[0]
	}
	fields := []capability.Field{
		{Name: "events", Description: events + strings.Join(names, ", ") + "; a member GitHub leaves out stays " +
			"out and no undocumented key is output; texts are untrusted data and never fetched"},
		{Name: "truncated", Description: "True when GitHub answered more events than the batch size and the rest " +
			"was cut"},
		{Name: "withheld", Description: "Count of events of this batch left out because their repository is " +
			"excluded on this connection, absent when none"},
		{Name: "state", Description: "unavailable when GitHub refused the read with 403 or 404; then no event is " +
			"given; absent otherwise"},
		{Name: "reason", Description: "Fixed reason that accompanies state unavailable; it names the possible " +
			"causes and never quotes GitHub"},
	}
	return append(fields, pagingFields...)
}

var auditLogList = capability.Descriptor{
	ID:      Provider + ".audit-log.list",
	Version: 1,
	Title:   "List the audit log of an organization",
	Description: "List one bounded batch of the audit log of an organization an explicit connection allows, newest " +
		"first unless order says otherwise, optionally narrowed by a search phrase and by the event types; the " +
		"audit log needs an organization owner and a plan that offers it, otherwise the state is unavailable; " +
		"only the members GitHub documents are output and addresses are never fetched",
	Tags:     []string{"github", "audit-log", "organization-admin", "list"},
	Risk:     auditLogRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"phrase":` + auditLogPhraseSchema + `,"include":` + auditLogIncludeSchema + `,"order":` +
		auditLogOrderSchema + `,` + pagingKeys),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"events":{"type":"array","items":{"type":"object",` +
		`"properties":{` + auditLogEventProperties() + `},"additionalProperties":false}},"truncated":{"type":"boolean"},"withheld":{"type":"integer"},` +
		`"state":{"type":"string"},"reason":{"type":"string"},"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: append([]capability.Argument{
		{Name: "phrase", Description: "Search phrase as GitHub documents it, such as action:repo.create; passed on " +
			"as text and never interpreted, at most 500 characters without control characters"},
		{Name: "include", Description: "Event types to read: web (the default), git, or all"},
		{Name: "order", Description: "desc for the newest events first (the default) or asc for the oldest first"},
	}, pagingArguments...),
	Fields: auditLogFields(),
	Examples: []capability.Example{{Description: "List the repository creations of an organization",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org","phrase":"action:repo.create","include":"web"}`)}},
}

// ---- Results

// AuditLogLocation is the location of the actor of an event.
type AuditLogLocation struct {
	CountryName *string `json:"country_name,omitempty"`
}

// AuditLogEvent is one audit log event with the members GitHub documents for it.
type AuditLogEvent struct {
	Timestamp             *int64            `json:"@timestamp,omitempty"`
	Action                *string           `json:"action,omitempty"`
	Active                *bool             `json:"active,omitempty"`
	ActiveWas             *bool             `json:"active_was,omitempty"`
	Actor                 *string           `json:"actor,omitempty"`
	ActorID               *int64            `json:"actor_id,omitempty"`
	ActorLocation         *AuditLogLocation `json:"actor_location,omitempty"`
	Data                  map[string]any    `json:"data,omitempty"`
	DataTruncated         bool              `json:"data_truncated,omitempty"`
	OrgID                 *int64            `json:"org_id,omitempty"`
	UserID                *int64            `json:"user_id,omitempty"`
	BusinessID            *int64            `json:"business_id,omitempty"`
	BlockedUser           *string           `json:"blocked_user,omitempty"`
	Business              *string           `json:"business,omitempty"`
	Config                []map[string]any  `json:"config,omitempty"`
	ConfigTruncated       bool              `json:"config_truncated,omitempty"`
	ConfigWas             []map[string]any  `json:"config_was,omitempty"`
	ConfigWasTruncated    bool              `json:"config_was_truncated,omitempty"`
	ContentType           *string           `json:"content_type,omitempty"`
	OperationType         *string           `json:"operation_type,omitempty"`
	CreatedAt             *int64            `json:"created_at,omitempty"`
	DeployKeyFingerprint  *string           `json:"deploy_key_fingerprint,omitempty"`
	DocumentID            *string           `json:"_document_id,omitempty"`
	Emoji                 *string           `json:"emoji,omitempty"`
	Events                []map[string]any  `json:"events,omitempty"`
	EventsTruncated       bool              `json:"events_truncated,omitempty"`
	EventsWere            []map[string]any  `json:"events_were,omitempty"`
	EventsWereTruncated   bool              `json:"events_were_truncated,omitempty"`
	Explanation           *string           `json:"explanation,omitempty"`
	Fingerprint           *string           `json:"fingerprint,omitempty"`
	HookID                *int64            `json:"hook_id,omitempty"`
	LimitedAvailability   *bool             `json:"limited_availability,omitempty"`
	Message               *string           `json:"message,omitempty"`
	Name                  *string           `json:"name,omitempty"`
	OldUser               *string           `json:"old_user,omitempty"`
	OpenSSHPublicKey      *string           `json:"openssh_public_key,omitempty"`
	Org                   *string           `json:"org,omitempty"`
	PreviousVisibility    *string           `json:"previous_visibility,omitempty"`
	ReadOnly              *bool             `json:"read_only,omitempty"`
	Repo                  *string           `json:"repo,omitempty"`
	Repository            *string           `json:"repository,omitempty"`
	RepositoryPublic      *bool             `json:"repository_public,omitempty"`
	TargetLogin           *string           `json:"target_login,omitempty"`
	Team                  *string           `json:"team,omitempty"`
	TransportProtocol     *int64            `json:"transport_protocol,omitempty"`
	TransportProtocolName *string           `json:"transport_protocol_name,omitempty"`
	User                  *string           `json:"user,omitempty"`
	Visibility            *string           `json:"visibility,omitempty"`
}

// AuditLogList is one batch of the audit log of an organization, or the state unavailable.
type AuditLogList struct {
	Events     []AuditLogEvent `json:"events,omitempty"`
	Truncated  bool            `json:"truncated,omitempty"`
	Withheld   int             `json:"withheld,omitempty"`
	State      string          `json:"state,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	NextCursor string          `json:"next_cursor,omitempty"`
	HasMore    bool            `json:"has_more"`
}

// ---- Raw answers

// auditLogEventRaw decodes only the documented members; every other key of an event is dropped by the decoder.
type auditLogEventRaw struct {
	Timestamp     *int64  `json:"@timestamp"`
	Action        *string `json:"action"`
	Active        *bool   `json:"active"`
	ActiveWas     *bool   `json:"active_was"`
	Actor         *string `json:"actor"`
	ActorID       *int64  `json:"actor_id"`
	ActorLocation *struct {
		CountryName *string `json:"country_name"`
	} `json:"actor_location"`
	Data                  map[string]json.RawMessage   `json:"data"`
	OrgID                 *int64                       `json:"org_id"`
	UserID                *int64                       `json:"user_id"`
	BusinessID            *int64                       `json:"business_id"`
	BlockedUser           *string                      `json:"blocked_user"`
	Business              *string                      `json:"business"`
	Config                []map[string]json.RawMessage `json:"config"`
	ConfigWas             []map[string]json.RawMessage `json:"config_was"`
	ContentType           *string                      `json:"content_type"`
	OperationType         *string                      `json:"operation_type"`
	CreatedAt             *int64                       `json:"created_at"`
	DeployKeyFingerprint  *string                      `json:"deploy_key_fingerprint"`
	DocumentID            *string                      `json:"_document_id"`
	Emoji                 *string                      `json:"emoji"`
	Events                []map[string]json.RawMessage `json:"events"`
	EventsWere            []map[string]json.RawMessage `json:"events_were"`
	Explanation           *string                      `json:"explanation"`
	Fingerprint           *string                      `json:"fingerprint"`
	HookID                *int64                       `json:"hook_id"`
	LimitedAvailability   *bool                        `json:"limited_availability"`
	Message               *string                      `json:"message"`
	Name                  *string                      `json:"name"`
	OldUser               *string                      `json:"old_user"`
	OpenSSHPublicKey      *string                      `json:"openssh_public_key"`
	Org                   *string                      `json:"org"`
	PreviousVisibility    *string                      `json:"previous_visibility"`
	ReadOnly              *bool                        `json:"read_only"`
	Repo                  *string                      `json:"repo"`
	Repository            *string                      `json:"repository"`
	RepositoryPublic      *bool                        `json:"repository_public"`
	TargetLogin           *string                      `json:"target_login"`
	Team                  *string                      `json:"team"`
	TransportProtocol     *int64                       `json:"transport_protocol"`
	TransportProtocolName *string                      `json:"transport_protocol_name"`
	User                  *string                      `json:"user"`
	Visibility            *string                      `json:"visibility"`
}

// flatValues cuts a free-form object to its flat values: texts cleaned and bounded, numbers as GitHub wrote
// them, and booleans. Nested values and nulls are left out; nested values mark the result as cut, as does a
// key beyond the bound. Keys are taken in name order.
func flatValues(raw map[string]json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	names, cut := capped(names)
	out := make(map[string]any, len(names))
	for _, name := range names {
		decoder := json.NewDecoder(bytes.NewReader(raw[name]))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil {
			cut = true
			continue
		}
		key := nameText(&name)
		switch v := value.(type) {
		case nil:
		case string:
			out[*key] = *longText(&v)
		case json.Number, bool:
			out[*key] = v
		default:
			cut = true
		}
	}
	if len(out) == 0 {
		return nil, cut
	}
	return out, cut
}

func flatList(raw []map[string]json.RawMessage) ([]map[string]any, bool) {
	raw, cut := capped(raw)
	var out []map[string]any
	for _, entry := range raw {
		values, inner := flatValues(entry)
		cut = cut || inner
		if values != nil {
			out = append(out, values)
		}
	}
	return out, cut
}

func (r *auditLogEventRaw) project() AuditLogEvent {
	event := AuditLogEvent{Timestamp: r.Timestamp, Action: shortText(r.Action), Active: r.Active,
		ActiveWas: r.ActiveWas, Actor: shortText(r.Actor), ActorID: r.ActorID, OrgID: r.OrgID, UserID: r.UserID,
		BusinessID: r.BusinessID, BlockedUser: shortText(r.BlockedUser), Business: shortText(r.Business),
		ContentType: shortText(r.ContentType), OperationType: shortText(r.OperationType), CreatedAt: r.CreatedAt,
		DeployKeyFingerprint: shortText(r.DeployKeyFingerprint), DocumentID: shortText(r.DocumentID),
		Emoji: shortText(r.Emoji), Explanation: longText(r.Explanation), Fingerprint: shortText(r.Fingerprint),
		HookID: r.HookID, LimitedAvailability: r.LimitedAvailability, Message: longText(r.Message),
		Name: shortText(r.Name), OldUser: shortText(r.OldUser), OpenSSHPublicKey: longText(r.OpenSSHPublicKey),
		Org: shortText(r.Org), PreviousVisibility: shortText(r.PreviousVisibility), ReadOnly: r.ReadOnly,
		Repo: shortText(r.Repo), Repository: shortText(r.Repository), RepositoryPublic: r.RepositoryPublic,
		TargetLogin: shortText(r.TargetLogin), Team: shortText(r.Team), TransportProtocol: r.TransportProtocol,
		TransportProtocolName: shortText(r.TransportProtocolName), User: shortText(r.User),
		Visibility: shortText(r.Visibility)}
	if r.ActorLocation != nil {
		event.ActorLocation = &AuditLogLocation{CountryName: shortText(r.ActorLocation.CountryName)}
	}
	event.Data, event.DataTruncated = flatValues(r.Data)
	event.Config, event.ConfigTruncated = flatList(r.Config)
	event.ConfigWas, event.ConfigWasTruncated = flatList(r.ConfigWas)
	event.Events, event.EventsTruncated = flatList(r.Events)
	event.EventsWere, event.EventsWereTruncated = flatList(r.EventsWere)
	return event
}

// ---- Arguments and handler

type auditLogArguments struct {
	Phrase  string `json:"phrase"`
	Include string `json:"include"`
	Order   string `json:"order"`
	Limit   int    `json:"limit"`
	Cursor  string `json:"cursor"`

	perPage int
	after   string
	binding []byte
}

func (a *auditLogArguments) check(bound target) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	if a.Phrase != "" && (utf8.RuneCountInString(a.Phrase) > auditLogMaxPhrase || !utf8.ValidString(a.Phrase) ||
		strings.IndexFunc(a.Phrase, unicode.IsControl) >= 0) {
		return invalidRequest("phrase must be a text of at most 500 characters without control characters")
	}
	if a.Include != "" && !containsExact(auditLogIncludes, a.Include) {
		return invalidRequest("include must be one of " + strings.Join(auditLogIncludes, ", "))
	}
	if a.Order != "" && !containsExact(auditLogOrders, a.Order) {
		return invalidRequest("order must be one of " + strings.Join(auditLogOrders, ", "))
	}
	a.binding = fingerprint("audit-log", strings.ToLower(bound.String()), a.Phrase, a.Include, a.Order)
	a.perPage = limit
	carried, err := decodeCursor(a.binding, a.Cursor)
	if err != nil || carried == "" {
		return err
	}
	// A continuation keeps the batch size of its first batch: the cursor reads size:after.
	size, after, _ := strings.Cut(carried, ":")
	a.perPage, err = strconv.Atoi(size)
	if err != nil || a.perPage < 1 || a.perPage > maxLimit || after == "" {
		return invalidRequest("cursor is not a next_cursor of this list; start the list again without cursor")
	}
	a.after = after
	return nil
}

func auditLogOperations() []capability.Operation {
	return []capability.Operation{{Descriptor: auditLogList, Handler: func(ctx context.Context, resolved *config.Resolved,
		secrets *secret.Resolver, red *redact.Redactor, raw json.RawMessage) (any, error) {
		var arguments auditLogArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(auditLogList.ID)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if err := arguments.check(bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(client.listAuditLog(ctx, &arguments))
	}}}
}

// ---- Request

// auditLogRefused reports a refusal GitHub answers with 403 or 404: a plan without the route, a missing right,
// a user who is no owner, or an unknown organization.
func auditLogRefused(err error) bool {
	var failure *provider.Error
	return errors.As(err, &failure) && (failure.Class == provider.ClassPermission || failure.Class == provider.ClassNotFound)
}

// nextAuditLogCursor returns the cursor of the rel="next" entry of a Link header. The address is only parsed.
func nextAuditLogCursor(header http.Header) (string, bool) {
	for _, part := range strings.Split(header.Get("Link"), ",") {
		address, params, ok := strings.Cut(part, ";")
		if !ok || strings.TrimSpace(params) != `rel="next"` {
			continue
		}
		address = strings.Trim(strings.TrimSpace(address), "<>")
		parsed, err := url.Parse(address)
		if err != nil {
			return "", true
		}
		return parsed.Query().Get("after"), true
	}
	return "", false
}

// auditLogHidden reports whether an event names a repository this connection excludes, as the bare name or as
// OWNER/REPO.
func (c *Client) auditLogHidden(r *auditLogEventRaw) bool {
	for _, name := range []*string{r.Repo, r.Repository} {
		if name == nil || *name == "" {
			continue
		}
		owner, repo := c.target.owner, *name
		if before, after, ok := strings.Cut(*name, "/"); ok {
			owner, repo = before, after
		}
		if !c.allowed.shows(c.target, target{kind: kindRepository, owner: owner, repo: repo}) {
			return true
		}
	}
	return false
}

func (c *Client) listAuditLog(ctx context.Context, a *auditLogArguments) (*AuditLogList, error) {
	const op = "list audit log"
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}}
	if a.Phrase != "" {
		query.Set("phrase", a.Phrase)
	}
	if a.Include != "" {
		query.Set("include", a.Include)
	}
	if a.Order != "" {
		query.Set("order", a.Order)
	}
	if a.after != "" {
		query.Set("after", a.after)
	}
	path := "/orgs/" + url.PathEscape(c.target.owner) + "/audit-log?" + query.Encode()
	var raw []auditLogEventRaw
	var header http.Header
	if err := c.do(ctx, op, http.MethodGet, c.endpoints.rest+path, nil, &raw, false, &header, nil); err != nil {
		if auditLogRefused(err) {
			return &AuditLogList{State: auditLogUnavailableState, Reason: auditLogUnavailableReason}, nil
		}
		return nil, actionsFailure(err, auditLogPermission)
	}
	result := &AuditLogList{Events: make([]AuditLogEvent, 0, len(raw))}
	if len(raw) > a.perPage {
		raw, result.Truncated = raw[:a.perPage], true
	}
	for i := range raw {
		// An excluded repository takes its events out of the result, not only out of the request.
		if c.auditLogHidden(&raw[i]) {
			result.Withheld++
			continue
		}
		result.Events = append(result.Events, raw[i].project())
	}
	if after, next := nextAuditLogCursor(header); next {
		if after == "" || len(after) > auditLogMaxAfter {
			return nil, providerError(op, "GitHub gave a continuation Qatlas cannot carry; start the list again with "+
				"a narrower phrase")
		}
		result.HasMore, result.NextCursor = true, encodeCursor(a.binding, strconv.Itoa(a.perPage)+":"+after)
	}
	return result, nil
}
