package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// GitHub Actions caches of a repository: a paged list with GitHub's filters, the usage summary, and a
// listed-only delete of one cache by identifier or of every cache with one complete key. Keys, refs, and
// versions are untrusted data. GitHub offers no read of a single cache, so a delete by identifier first finds
// the cache in the repository's own list; an identifier that list does not show is refused before anything
// is sent.

// Bounds of the cache tools. actions/cache refuses keys over 512 characters and keys with a comma. A delete
// by identifier searches at most maxCacheProofPages pages of the newest caches; a delete by key lists at most
// maxDeletedCaches of the caches GitHub reports deleted.
const (
	cacheKeyLimit      = 512
	cacheProofPageSize = 100
	maxCacheProofPages = 10
	maxDeletedCaches   = 100
)

const (
	cacheKeyPattern = `^[^,\\x00-\\x1f\\x7f]+$`
	cacheRefPattern = `^refs/[A-Za-z0-9_][A-Za-z0-9._/-]{0,249}$`
	cacheKeySchema  = `{"type":"string","minLength":1,"maxLength":512,"pattern":"` + cacheKeyPattern + `",` +
		`"x-form":"a cache key without comma or control character"}`
	cacheRefSchema = `{"type":"string","minLength":6,"maxLength":255,"pattern":"` + cacheRefPattern + `",` +
		`"x-form":"a full ref such as refs/heads/main or refs/pull/1/merge"}`
)

var cacheSorts = []string{"created_at", "last_accessed_at", "size_in_bytes"}

const cacheProperties = `"id":{"type":"integer"},"ref":{"type":"string"},"key":{"type":"string"},` +
	`"version":{"type":"string"},"last_accessed_at":{"type":"string"},"created_at":{"type":"string"},` +
	`"size_in_bytes":{"type":"integer"}`

const cachesProperty = `"caches":{"type":"array","items":{"type":"object","properties":{` + cacheProperties +
	`},"required":["id"],"additionalProperties":false}}`

var cacheEntryFields = []capability.Field{
	{Name: "key", Description: "Key of the cache, untrusted data"},
	{Name: "ref", Description: "Full ref the cache belongs to, such as refs/heads/main, untrusted data"},
	{Name: "version", Description: "Version hash GitHub derives from the cache paths and compression"},
}

var actionCachesList = capability.Descriptor{
	ID:      Provider + ".action-caches.list",
	Version: 1,
	Title:   "List GitHub Actions caches",
	Description: "List one bounded, filtered batch of the GitHub Actions caches of a repository a connection " +
		"allows, most recently used first unless sort says otherwise; cache contents are never read",
	Tags:     []string{"github", "actions", "caches", "list"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"key":` + cacheKeySchema + `,"ref":` + cacheRefSchema + `,` +
		`"sort":{"type":"string","enum":["` + strings.Join(cacheSorts, `","`) + `"]},` +
		`"direction":{"type":"string","enum":["asc","desc"]},` + pagingKeys),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + cachesProperty + `,` +
		`"total_count":{"type":"integer"},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
		`"required":["caches","total_count","has_more"],"additionalProperties":false}`),
	Arguments: append([]capability.Argument{
		{Name: "key", Description: "Return only caches whose key is this key or starts with it"},
		{Name: "ref", Description: "Return only caches of this full ref, such as refs/heads/main or " +
			"refs/pull/1/merge"},
		{Name: "sort", Description: "Order by " + strings.Join(cacheSorts, ", ") + "; last_accessed_at when omitted"},
		{Name: "direction", Description: "asc or desc; desc when omitted"},
	}, pagingArguments...),
	Fields: append(append([]capability.Field{
		{Name: "caches", Description: "Caches with identifier, key, ref, version, size_in_bytes, created_at, " +
			"and last_accessed_at"},
		{Name: "total_count", Description: "Number of caches that match the filters"},
	}, cacheEntryFields...), pagingFields...),
	Examples: []capability.Example{{
		Description: "List the largest caches of the main branch",
		Arguments:   json.RawMessage(`{"ref":"refs/heads/main","sort":"size_in_bytes","limit":10}`),
	}},
}

var actionCacheUsageGet = capability.Descriptor{
	ID:      Provider + ".action-cache-usage.get",
	Version: 1,
	Title:   "Get the GitHub Actions cache usage of a repository",
	Description: "Read the number and total size of the active GitHub Actions caches of a repository a " +
		"connection allows; GitHub refreshes these numbers about every five minutes",
	Tags:        []string{"github", "actions", "caches", "usage", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(""),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"full_name":{"type":"string"},` +
		`"active_caches_size_in_bytes":{"type":"integer"},"active_caches_count":{"type":"integer"}},` +
		`"required":["active_caches_size_in_bytes","active_caches_count"],"additionalProperties":false}`),
	Fields: []capability.Field{
		{Name: "active_caches_count", Description: "Number of active caches"},
		{Name: "active_caches_size_in_bytes", Description: "Total size of the active caches in bytes"},
		{Name: "full_name", Description: "Repository the usage is reported for, as GitHub names it, untrusted data"},
	},
	Examples: []capability.Example{{Description: "Read the cache usage", Arguments: json.RawMessage(`{}`)}},
}

var actionCachesDelete = capability.Descriptor{
	ID:      Provider + ".action-caches.delete",
	Version: 1,
	Title:   "Delete GitHub Actions caches",
	Description: "Delete GitHub Actions caches of a repository a connection allows: one cache by cache_id, or " +
		"every cache with one complete key on one ref or, with all_refs, on every ref; deleted caches cannot be " +
		"restored, and later runs build them again. Nothing to delete counts as done. Offered only by a " +
		"connection whose tools list names it",
	Tags:                  []string{"github", "actions", "caches", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyIdempotent, dataSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"cache_id":` + actionsIDSchema + `,"key":` + cacheKeySchema + `,"ref":` +
		cacheRefSchema + `,"all_refs":{"type":"boolean"}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"cache_id":{"type":"integer"},` +
		`"key":{"type":"string"},"ref":{"type":"string"},"all_refs":{"type":"boolean"},` +
		`"deleted_count":{"type":"integer"},"already_deleted":{"type":"boolean"},` + cachesProperty + `,` +
		`"truncated":{"type":"boolean"}},"required":["deleted_count","already_deleted","caches"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "cache_id", Description: "Identifier of one cache, as github.action-caches.list reports it; " +
			"exactly one of cache_id or key is required"},
		{Name: "key", Description: "Complete key of the caches to delete, compared exactly, not as a prefix; " +
			"needs ref or all_refs"},
		{Name: "ref", Description: "Full ref whose caches with key are deleted, such as refs/heads/main; only " +
			"with key"},
		{Name: "all_refs", Description: "true deletes the caches with key on every ref; only with key and " +
			"without ref"},
	},
	Fields: []capability.Field{
		{Name: "deleted_count", Description: "Number of caches this call deleted"},
		{Name: "already_deleted", Description: "True when there was nothing left to delete: the cache found by " +
			"cache_id was gone when the delete arrived, or no cache had key on the ref"},
		{Name: "caches", Description: "The deleted caches with the members of github.action-caches.list, at most " +
			"100; key and ref are untrusted data"},
		{Name: "truncated", Description: "True when more caches were deleted than caches lists"},
		{Name: "cache_id", Description: "Identifier of the cache a delete by cache_id concerned"},
		{Name: "key", Description: "Key a delete by key concerned"},
		{Name: "ref", Description: "Ref a delete by key concerned"},
		{Name: "all_refs", Description: "True when a delete by key concerned every ref"},
	},
	Examples: []capability.Example{
		{Description: "Delete one cache", Arguments: json.RawMessage(`{"cache_id":505}`)},
		{Description: "Delete the caches of one key on a pull request ref",
			Arguments: json.RawMessage(`{"key":"Linux-node-958aff96","ref":"refs/pull/12/merge"}`)},
	},
}

func actionCachesOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*actionsArguments, target) error,
		call func(context.Context, *Client, *actionsArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: actionsHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(actionCachesList, checkCacheList, func(ctx context.Context, c *Client, a *actionsArguments) (any, error) {
			return c.listCaches(ctx, a)
		}),
		bind(actionCacheUsageGet, func(*actionsArguments, target) error { return nil },
			func(ctx context.Context, c *Client, _ *actionsArguments) (any, error) {
				return c.cacheUsage(ctx)
			}),
		bind(actionCachesDelete, checkCacheDelete, func(ctx context.Context, c *Client, a *actionsArguments) (any, error) {
			if a.CacheID != 0 {
				return c.deleteCache(ctx, a.CacheID)
			}
			return c.deleteCachesByKey(ctx, a.Key, a.Ref)
		}),
	}
}

func validCacheKey(key string) bool {
	return key != "" && utf8.ValidString(key) && utf8.RuneCountInString(key) <= cacheKeyLimit &&
		!strings.ContainsRune(key, ',') && strings.IndexFunc(key, unicode.IsControl) < 0
}

// validCacheRef accepts a full ref, as GitHub documents the ref of a cache.
func validCacheRef(ref string) bool {
	return strings.HasPrefix(ref, "refs/") && validRef(ref)
}

func checkCacheList(a *actionsArguments, bound target) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	switch {
	case a.Key != "" && !validCacheKey(a.Key):
		return invalidRequest("key must be a cache key of at most 512 characters without comma or control character")
	case a.Ref != "" && !validCacheRef(a.Ref):
		return invalidRequest("ref must be a full ref such as refs/heads/main or refs/pull/1/merge")
	case a.Sort != "" && !slices.Contains(cacheSorts, a.Sort):
		return invalidRequest("sort must be one of " + strings.Join(cacheSorts, ", "))
	case a.Direction != "" && a.Direction != "asc" && a.Direction != "desc":
		return invalidRequest("direction must be asc or desc")
	}
	a.binding = fingerprint("actions", "caches", bound.String(), a.Key, a.Ref, a.Sort, a.Direction)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

// checkCacheDelete accepts exactly one of cache_id or key; a key needs ref or an explicit all_refs: true, so a
// delete on every ref is never the result of a ref left out.
func checkCacheDelete(a *actionsArguments, _ target) error {
	allRefs := a.AllRefs != nil && *a.AllRefs
	switch {
	case (a.CacheID != 0) == (a.Key != ""):
		return invalidRequest("exactly one of cache_id or key is required")
	case a.CacheID != 0 && a.CacheID < 1:
		return invalidRequest("cache_id must be a positive cache identifier")
	case a.CacheID != 0 && (a.Ref != "" || a.AllRefs != nil):
		return invalidRequest("ref and all_refs belong to a delete by key, not to one by cache_id")
	case a.CacheID != 0:
		return nil
	case !validCacheKey(a.Key):
		return invalidRequest("key must be a cache key of at most 512 characters without comma or control character")
	case a.Ref == "" && !allRefs:
		return invalidRequest("a delete by key needs ref, or all_refs: true to delete the caches with this key on every ref")
	case a.Ref != "" && allRefs:
		return invalidRequest("give ref or all_refs: true, not both")
	case a.Ref != "" && !validCacheRef(a.Ref):
		return invalidRequest("ref must be a full ref such as refs/heads/main or refs/pull/1/merge")
	}
	return nil
}

// ActionCache is one Actions cache. Key and ref are untrusted data.
type ActionCache struct {
	ID             int64  `json:"id"`
	Ref            string `json:"ref,omitempty"`
	Key            string `json:"key,omitempty"`
	Version        string `json:"version,omitempty"`
	LastAccessedAt string `json:"last_accessed_at,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	SizeInBytes    *int64 `json:"size_in_bytes,omitempty"`
}

type actionCacheRaw struct {
	ID             int64  `json:"id"`
	Ref            string `json:"ref"`
	Key            string `json:"key"`
	Version        string `json:"version"`
	LastAccessedAt string `json:"last_accessed_at"`
	CreatedAt      string `json:"created_at"`
	SizeInBytes    *int64 `json:"size_in_bytes"`
}

type actionCacheListRaw struct {
	TotalCount int              `json:"total_count"`
	Caches     []actionCacheRaw `json:"actions_caches"`
}

// cacheText cleans a key or ref. The bound is the longest key actions/cache accepts, so a key is answered
// whole and stays usable as the key of a delete.
func cacheText(s string) string {
	cleaned, _ := cleanText(&s, cacheKeyLimit, false)
	return *cleaned
}

func (r actionCacheRaw) view() ActionCache {
	return ActionCache{ID: r.ID, Ref: cacheText(r.Ref), Key: cacheText(r.Key), Version: text(r.Version),
		LastAccessedAt: text(r.LastAccessedAt), CreatedAt: text(r.CreatedAt), SizeInBytes: r.SizeInBytes}
}

// ActionCacheList is one batch of Actions caches.
type ActionCacheList struct {
	Caches     []ActionCache `json:"caches"`
	TotalCount int           `json:"total_count"`
	NextCursor string        `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more"`
}

func (c *Client) listCaches(ctx context.Context, a *actionsArguments) (*ActionCacheList, error) {
	const op = "list Actions caches"
	query := a.query()
	for name, value := range map[string]string{"key": a.Key, "ref": a.Ref, "sort": a.Sort, "direction": a.Direction} {
		if value != "" {
			query.Set(name, value)
		}
	}
	var raw actionCacheListRaw
	if err := c.actionsRead(ctx, op, "caches", query, &raw); err != nil {
		return nil, err
	}
	result := &ActionCacheList{Caches: make([]ActionCache, 0, len(raw.Caches)), TotalCount: raw.TotalCount}
	for _, cache := range raw.Caches {
		if cache.ID < 1 {
			return nil, invalidEntry(op, "a cache")
		}
		result.Caches = append(result.Caches, cache.view())
	}
	result.HasMore, result.NextCursor = a.more(len(raw.Caches), raw.TotalCount)
	return result, nil
}

// ActionCacheUsage is the cache usage of one repository.
type ActionCacheUsage struct {
	FullName                string `json:"full_name,omitempty"`
	ActiveCachesSizeInBytes int64  `json:"active_caches_size_in_bytes"`
	ActiveCachesCount       int64  `json:"active_caches_count"`
}

func (c *Client) cacheUsage(ctx context.Context) (*ActionCacheUsage, error) {
	var raw ActionCacheUsage
	if err := c.actionsRead(ctx, "get Actions cache usage", "cache/usage", nil, &raw); err != nil {
		return nil, err
	}
	raw.FullName = text(raw.FullName)
	return &raw, nil
}

// ActionCachesDeleted is the answer of a cache delete.
type ActionCachesDeleted struct {
	CacheID        int64         `json:"cache_id,omitempty"`
	Key            string        `json:"key,omitempty"`
	Ref            string        `json:"ref,omitempty"`
	AllRefs        bool          `json:"all_refs,omitempty"`
	DeletedCount   int           `json:"deleted_count"`
	AlreadyDeleted bool          `json:"already_deleted"`
	Caches         []ActionCache `json:"caches"`
	Truncated      bool          `json:"truncated,omitempty"`
}

func isNotFound(err error) bool {
	var failure *provider.Error
	return errors.As(err, &failure) && failure.Class == provider.ClassNotFound
}

// deleteCache deletes one cache that the proof read found in the bound repository. A not-found answer to the
// delete of a cache the read just found means that the cache is gone already. The delete is one request
// that is never repeated.
func (c *Client) deleteCache(ctx context.Context, id int64) (*ActionCachesDeleted, error) {
	const op = "delete Actions cache"
	cache, err := c.findCache(ctx, op, id)
	if err != nil {
		return nil, err
	}
	err = c.restChange(ctx, op, http.MethodDelete, c.actionsPath("caches/"+strconv.FormatInt(id, 10)), struct{}{}, nil)
	switch {
	case err == nil:
		return &ActionCachesDeleted{CacheID: id, DeletedCount: 1, Caches: []ActionCache{cache}}, nil
	case isNotFound(err):
		return &ActionCachesDeleted{CacheID: id, AlreadyDeleted: true, Caches: []ActionCache{}}, nil
	}
	return nil, actionsFailure(err, actionsChangePermission)
}

// findCache is the proof read of a delete by identifier: it pages through the newest caches of the bound
// repository until it meets the identifier, so an identifier of another repository is never sent.
func (c *Client) findCache(ctx context.Context, op string, id int64) (ActionCache, error) {
	for page := 1; page <= maxCacheProofPages; page++ {
		query := url.Values{"per_page": {strconv.Itoa(cacheProofPageSize)}, "page": {strconv.Itoa(page)},
			"sort": {"created_at"}, "direction": {"desc"}}
		var raw actionCacheListRaw
		if err := c.actionsRead(ctx, op, "caches", query, &raw); err != nil {
			return ActionCache{}, err
		}
		for _, cache := range raw.Caches {
			if cache.ID == id {
				return cache.view(), nil
			}
		}
		if len(raw.Caches) < cacheProofPageSize || page*cacheProofPageSize >= raw.TotalCount {
			return ActionCache{}, &provider.Error{Class: provider.ClassNotFound, Op: op,
				Message: "this repository holds no Actions cache with this cache_id; nothing was deleted"}
		}
	}
	return ActionCache{}, &provider.Error{Class: provider.ClassNotFound, Op: op,
		Message: "the newest " + strconv.Itoa(maxCacheProofPages*cacheProofPageSize) + " Actions caches of this " +
			"repository hold no cache with this cache_id; nothing was deleted; delete an older cache by key and ref"}
}

// deleteCachesByKey deletes every cache with key on ref, or on every ref when ref is empty. A read of the same
// filters first proves that this token sees the repository's caches, so a not-found answer to the delete means
// that no cache matched, not that the repository is out of reach. The delete is one request that is never
// repeated.
func (c *Client) deleteCachesByKey(ctx context.Context, key, ref string) (*ActionCachesDeleted, error) {
	const op = "delete Actions caches by key"
	query := url.Values{"key": {key}}
	if ref != "" {
		query.Set("ref", ref)
	}
	probe := url.Values{"key": {key}, "per_page": {"1"}}
	if ref != "" {
		probe.Set("ref", ref)
	}
	if err := c.actionsRead(ctx, op, "caches", probe, &actionCacheListRaw{}); err != nil {
		return nil, err
	}
	result := &ActionCachesDeleted{Key: key, Ref: ref, AllRefs: ref == "", Caches: []ActionCache{}}
	var raw actionCacheListRaw
	err := c.restChange(ctx, op, http.MethodDelete, c.actionsPath("caches?"+query.Encode()), struct{}{}, &raw)
	switch {
	case isNotFound(err):
		result.AlreadyDeleted = true
		return result, nil
	case err != nil:
		return nil, actionsFailure(err, actionsChangePermission)
	}
	result.DeletedCount = max(raw.TotalCount, len(raw.Caches))
	for _, cache := range raw.Caches {
		if cache.ID < 1 || len(result.Caches) == maxDeletedCaches {
			continue
		}
		result.Caches = append(result.Caches, cache.view())
	}
	result.Truncated = result.DeletedCount > len(result.Caches)
	result.AlreadyDeleted = result.DeletedCount == 0
	return result, nil
}
