package github

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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

// Deploy keys of a repository. github.deploy-keys.list and github.deploy-keys.get read them; the key text
// itself is a credential-grade value and is never shown, so a reader sees the fingerprint GitHub's own
// interface shows, computed here from the public key, with the title and whether the key is read-only.
// github.deploy-keys.delete revokes one key and is offered only where a connection's tools list names it.
// Creating a key is out of reach. The identifier is bound to the repository by a read below it before the
// delete, and the delete is sent at most once.

// accessGrantSensitivity classifies the revocation of a deploy key or of a repository invitation as its own
// guarded change: it removes a way into the repository and cannot be undone by this provider.
const accessGrantSensitivity = "github-access-grant"

var deployKeyLinkKeys = []string{"url"}

var deployKeyProperties = `"id":{"type":"integer"},"title":{"type":"string"},"fingerprint":{"type":"string"},` +
	`"read_only":{"type":"boolean"},"verified":{"type":"boolean"},"created_at":{"type":"string"},` +
	`"added_by":{"type":"string"},"last_used":{"type":"string"},"enabled":{"type":"boolean"},` +
	`"links":` + linksSchema(deployKeyLinkKeys)

const deployKeyRequired = `"required":["id","title","read_only"],"additionalProperties":false`

var deployKeyOutput = `{"type":"object","properties":{` + deployKeyProperties + `},` + deployKeyRequired + `}`

var deployKeyFields = []capability.Field{
	{Name: "id", Description: "Numeric deploy key id"},
	{Name: "title", Description: "Title of the key, untrusted data"},
	{Name: "fingerprint", Description: "SHA256 fingerprint of the public key as GitHub shows it, computed here; " +
		"absent when the key text could not be read; the key text itself is never shown"},
	{Name: "read_only", Description: "True when the key can only read the repository, false when it can also push"},
	{Name: "verified", Description: "Whether GitHub verified the key"},
	{Name: "created_at", Description: "Time the key was added"},
	{Name: "added_by", Description: "Login of the account that added the key, untrusted data"},
	{Name: "last_used", Description: "Time the key was last used; absent when it never was"},
	{Name: "enabled", Description: "Whether the key is enabled"},
	{Name: "links", Description: "REST url of the key, untrusted data, never fetched"},
}

var deployKeysList = capability.Descriptor{
	ID:      Provider + ".deploy-keys.list",
	Version: 1,
	Title:   "List GitHub deploy keys",
	Description: "List one bounded batch of the deploy keys of a repository a connection allows, with title, " +
		"fingerprint, and whether each is read-only; the key text is never shown",
	Tags:         []string{"github", "repositories", "deploy-keys", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(pagingKeys),
	OutputSchema: listOutput("deploy_keys", deployKeyProperties, deployKeyRequired),
	Arguments:    pagingArguments,
	Fields: append([]capability.Field{
		{Name: "deploy_keys", Description: "Deploy keys with id, title, fingerprint, read_only, and the further members " +
			"GitHub documents; the key text is left out on purpose"},
	}, pagingFields...),
	Examples: []capability.Example{{Description: "List the deploy keys", Arguments: json.RawMessage(`{}`)}},
}

var deployKeysGet = capability.Descriptor{
	ID:           Provider + ".deploy-keys.get",
	Version:      1,
	Title:        "Get a GitHub deploy key",
	Description:  "Read one deploy key of a repository a connection allows by its id, without the key text",
	Tags:         []string{"github", "repositories", "deploy-keys", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"id":`+grantIDSchema, "id"),
	OutputSchema: json.RawMessage(deployKeyOutput),
	Arguments:    []capability.Argument{{Name: "id", Description: "Numeric id of the deploy key", Required: true}},
	Fields:       deployKeyFields,
	Examples:     []capability.Example{{Description: "Read one deploy key", Arguments: json.RawMessage(`{"id":1}`)}},
}

const deployKeyDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},"id":{"type":"integer"},` +
	`"title":{"type":"string"},"fingerprint":{"type":"string"}},"required":["deleted","id","title"],` +
	`"additionalProperties":false}`

var deployKeysDelete = capability.Descriptor{
	ID:      Provider + ".deploy-keys.delete",
	Version: 1,
	Title:   "Delete a GitHub deploy key",
	Description: "Revoke one deploy key of a repository a connection allows by its id, after a read proves the " +
		"key belongs to the repository; offered only where a connection's tools list names it, because a " +
		"revoked key cannot be restored by this tool",
	Tags:                  []string{"github", "repositories", "deploy-keys", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, accessGrantSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"id":`+grantIDSchema, "id"),
	OutputSchema:          json.RawMessage(deployKeyDeletedOutput),
	Arguments:             []capability.Argument{{Name: "id", Description: "Numeric id of the deploy key", Required: true}},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the key"},
		{Name: "id", Description: "Id of the deleted key"},
		{Name: "title", Description: "Title of the deleted key, untrusted data"},
		{Name: "fingerprint", Description: "Fingerprint of the deleted key; absent when it could not be computed"},
	},
	Examples: []capability.Example{{Description: "Revoke a deploy key", Arguments: json.RawMessage(`{"id":1}`)}},
}

const grantIDSchema = `{"type":"integer","minimum":1}`

// deployKeysOperations binds every deploy key tool to its handler.
func deployKeysOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: deployKeysList, Handler: grantsHandler(deployKeysList.ID, checkGrantsPaging("deploy-keys"),
			func(ctx context.Context, c *Client, a *grantArguments) (any, error) { return c.listDeployKeys(ctx, a) })},
		{Descriptor: deployKeysGet, Handler: grantsHandler(deployKeysGet.ID, checkGrantID,
			func(ctx context.Context, c *Client, a *grantArguments) (any, error) { return c.getDeployKey(ctx, a.ID) })},
		{Descriptor: deployKeysDelete, Handler: grantsHandler(deployKeysDelete.ID, checkGrantID,
			func(ctx context.Context, c *Client, a *grantArguments) (any, error) {
				return c.deleteDeployKey(ctx, a.ID)
			})},
	}
}

// grantArguments holds the arguments of the deploy key and repository invitation tools; the input schema of
// each tool admits only its own. page, perPage, and binding are derived by the check.
type grantArguments struct {
	ID     int64  `json:"id"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`

	page, perPage int
	binding       []byte
}

// grantsHandler decodes and checks the arguments and the repository before a credential is resolved, so a
// refused request never becomes a provider call.
func grantsHandler(id string, check func(*grantArguments, target) error,
	call func(context.Context, *Client, *grantArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments grantArguments
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

func checkGrantsPaging(kind string) func(*grantArguments, target) error {
	return func(a *grantArguments, bound target) error {
		limit, err := normalizeLimit(a.Limit)
		if err != nil {
			return err
		}
		a.binding = fingerprint(kind, "list", bound.String())
		a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
		return err
	}
}

func checkGrantID(a *grantArguments, _ target) error {
	if a.ID < 1 {
		return invalidRequest("id must be a positive numeric id")
	}
	return nil
}

func (a *grantArguments) query() url.Values {
	return url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
}

// Permission messages of the deploy key and invitation tools. GitHub decides on every request; a message names
// what such a request needs without claiming what the configured token holds.
const (
	grantsReadPermission = "GitHub refused this token the deploy keys or invitations of this repository; " +
		"reading them needs repo on a classic token, as a repository administrator, or Administration: read on " +
		"a fine-grained token"
	grantsChangePermission = "GitHub refused to revoke this access; it needs repo on a classic token, as a " +
		"repository administrator, or Administration: read and write on a fine-grained token"
)

func (c *Client) deployKeyPath(id int64) string {
	if id == 0 {
		return c.repoPath("keys")
	}
	return c.repoPath("keys/" + strconv.FormatInt(id, 10))
}

// deployKeyRaw is the REST deploy key. The key text is read only to compute the fingerprint and is never
// part of the view.
type deployKeyRaw struct {
	ID        *int64  `json:"id"`
	Key       *string `json:"key"`
	Title     *string `json:"title"`
	Verified  *bool   `json:"verified"`
	CreatedAt *string `json:"created_at"`
	ReadOnly  *bool   `json:"read_only"`
	AddedBy   *string `json:"added_by"`
	LastUsed  *string `json:"last_used"`
	Enabled   *bool   `json:"enabled"`
	links     Links
}

func (r *deployKeyRaw) UnmarshalJSON(data []byte) (err error) {
	type plain deployKeyRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, deployKeyLinkKeys)
	return err
}

// DeployKey is the view of one deploy key, without its key text.
type DeployKey struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Fingerprint string  `json:"fingerprint,omitempty"`
	ReadOnly    bool    `json:"read_only"`
	Verified    *bool   `json:"verified,omitempty"`
	CreatedAt   *string `json:"created_at,omitempty"`
	AddedBy     *string `json:"added_by,omitempty"`
	LastUsed    *string `json:"last_used,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
	Links       Links   `json:"links,omitempty"`
}

// publicKeyFingerprint returns the SHA256 fingerprint of an OpenSSH public key line the way GitHub and
// ssh-keygen show it, or "" for text that is no such key.
func publicKeyFingerprint(key *string) string {
	if key == nil {
		return ""
	}
	fields := strings.Fields(*key)
	if len(fields) < 2 {
		return ""
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(blob) == 0 {
		return ""
	}
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// project returns the view, or false when GitHub left out the id, the title, or the read-only flag, which
// would otherwise show a writable key as read-only.
func (r *deployKeyRaw) project() (DeployKey, bool) {
	if r.ID == nil || *r.ID < 1 || r.Title == nil || r.ReadOnly == nil {
		return DeployKey{}, false
	}
	return DeployKey{ID: *r.ID, Title: *nameText(r.Title), Fingerprint: publicKeyFingerprint(r.Key),
		ReadOnly: *r.ReadOnly, Verified: r.Verified, CreatedAt: shortText(r.CreatedAt),
		AddedBy: shortText(r.AddedBy), LastUsed: shortText(r.LastUsed), Enabled: r.Enabled, Links: r.links}, true
}

// DeployKeyList is one batch of deploy keys.
type DeployKeyList struct {
	DeployKeys []DeployKey `json:"deploy_keys"`
	NextCursor string      `json:"next_cursor,omitempty"`
	HasMore    bool        `json:"has_more"`
}

func (c *Client) listDeployKeys(ctx context.Context, a *grantArguments) (*DeployKeyList, error) {
	const op = "list deploy keys"
	var raw []deployKeyRaw
	hasNext, err := c.restPage(ctx, op, c.deployKeyPath(0), a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, grantsReadPermission)
	}
	result := &DeployKeyList{DeployKeys: make([]DeployKey, 0, len(raw))}
	for i := range raw {
		key, ok := raw[i].project()
		if !ok {
			return nil, invalidEntry(op, "a deploy key")
		}
		result.DeployKeys = append(result.DeployKeys, key)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

// getDeployKey reads one key and refuses an answer that is not the requested key of the bound repository,
// which is also the proof read of the delete.
func (c *Client) getDeployKey(ctx context.Context, id int64) (*DeployKey, error) {
	const op = "get deploy key"
	var raw deployKeyRaw
	if err := c.rest(ctx, op, c.deployKeyPath(id), &raw); err != nil {
		return nil, actionsFailure(err, grantsReadPermission)
	}
	key, ok := raw.project()
	if !ok {
		return nil, invalidEntry(op, "a deploy key")
	}
	if key.ID != id || !c.belongsHere(link(key.Links, "url"), "/keys/"+strconv.FormatInt(id, 10)) {
		return nil, foreignActionsObject(op, "a deploy key")
	}
	return &key, nil
}

// DeployKeyDeleted is the answer of a deploy key revocation.
type DeployKeyDeleted struct {
	Deleted     bool   `json:"deleted"`
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// deleteDeployKey reads the key below the bound repository first and sends the DELETE only after that read
// answered the key; the DELETE is sent once and never repeated.
func (c *Client) deleteDeployKey(ctx context.Context, id int64) (*DeployKeyDeleted, error) {
	const op = "delete deploy key"
	key, err := c.getDeployKey(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.restChange(ctx, op, http.MethodDelete, c.deployKeyPath(id), struct{}{}, nil); err != nil {
		return nil, actionsFailure(err, grantsChangePermission)
	}
	return &DeployKeyDeleted{Deleted: true, ID: id, Title: key.Title, Fingerprint: key.Fingerprint}, nil
}

// grantsSubject names the deploy key or the invitation a repository path addresses: keys or invitations for
// the collection, keys/ID or invitations/ID for one. It is empty for any other path.
func grantsSubject(path string) string {
	for _, kind := range []struct{ segment, one, all string }{
		{"keys", "deploy key ", "the deploy keys of this repository"},
		{"invitations", "invitation ", "the invitations of this repository"},
	} {
		if path == kind.segment {
			return kind.all
		}
		rest, ok := strings.CutPrefix(path, kind.segment+"/")
		if !ok {
			continue
		}
		if id, err := strconv.ParseInt(rest, 10, 64); err == nil && id > 0 {
			return kind.one + rest
		}
		return kind.all
	}
	return ""
}
