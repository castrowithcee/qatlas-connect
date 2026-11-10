package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/redact"
)

// cacheProviderCanary stands for GitHub's own text of a refusal; it must never reach a message.
const cacheProviderCanary = "cache-provider-text-canary-5e1f"

type fakeCache struct {
	id       int64
	key, ref string
	size     int64
	created  string
}

// fakeCaches answers the Actions cache routes of octo-org/example through the failure hook of fakeGitHub.
// GitHub's key filter of the list matches a prefix, the delete by key an exact key.
type fakeCaches struct {
	mu     sync.Mutex
	caches []fakeCache
	// readStatus, when set, answers every cache read with this status and GitHub's own text.
	readStatus int
	// deleteAnswer, when set, answers every delete in place of the fake.
	deleteAnswer func(http.ResponseWriter)
}

func cacheJSON(c fakeCache) string {
	data, _ := json.Marshal(map[string]any{"id": c.id, "ref": c.ref, "key": c.key,
		"version":          "73885106f58cc52a7df9ec4d4a5622a5614813162cb516c759a30af6bf56e6f0",
		"last_accessed_at": c.created, "created_at": c.created, "size_in_bytes": c.size})
	return string(data)
}

func refuse(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"message":%q}`, cacheProviderCanary)
}

func (c *fakeCaches) route(w http.ResponseWriter, r *http.Request) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, actionsPrefix)
	if !ok || !(rest == "actions/caches" || strings.HasPrefix(rest, "actions/caches/") || rest == "actions/cache/usage") {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	query := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && c.readStatus != 0:
		refuse(w, c.readStatus)
	case r.Method == http.MethodDelete && c.deleteAnswer != nil:
		c.deleteAnswer(w)
	case r.Method == http.MethodGet && rest == "actions/cache/usage":
		var size int64
		for _, cache := range c.caches {
			size += cache.size
		}
		fmt.Fprintf(w, `{"full_name":"octo-org/example","active_caches_size_in_bytes":%d,"active_caches_count":%d}`,
			size, len(c.caches))
	case r.Method == http.MethodGet && rest == "actions/caches":
		matching := []fakeCache{}
		for _, cache := range c.caches {
			if strings.HasPrefix(cache.key, query.Get("key")) && (query.Get("ref") == "" || cache.ref == query.Get("ref")) {
				matching = append(matching, cache)
			}
		}
		less := func(a, b fakeCache) bool {
			if query.Get("sort") == "size_in_bytes" {
				return a.size < b.size
			}
			return a.created < b.created
		}
		sort.SliceStable(matching, func(i, j int) bool {
			if query.Get("direction") == "asc" {
				return less(matching[i], matching[j])
			}
			return less(matching[j], matching[i])
		})
		perPage, _ := strconv.Atoi(query.Get("per_page"))
		number, _ := strconv.Atoi(query.Get("page"))
		perPage, number = max(perPage, 1), max(number, 1)
		start := min((number-1)*perPage, len(matching))
		end := min(start+perPage, len(matching))
		entries := []string{}
		for _, cache := range matching[start:end] {
			entries = append(entries, cacheJSON(cache))
		}
		fmt.Fprintf(w, `{"total_count":%d,"actions_caches":[%s]}`, len(matching), strings.Join(entries, ","))
	case r.Method == http.MethodDelete && rest == "actions/caches":
		kept, deleted := []fakeCache{}, []string{}
		for _, cache := range c.caches {
			if cache.key == query.Get("key") && (query.Get("ref") == "" || cache.ref == query.Get("ref")) {
				deleted = append(deleted, cacheJSON(cache))
				continue
			}
			kept = append(kept, cache)
		}
		if len(deleted) == 0 {
			refuse(w, http.StatusNotFound)
			return true
		}
		c.caches = kept
		fmt.Fprintf(w, `{"total_count":%d,"actions_caches":[%s]}`, len(deleted), strings.Join(deleted, ","))
	case r.Method == http.MethodDelete && strings.HasPrefix(rest, "actions/caches/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(rest, "actions/caches/"), 10, 64)
		for i, cache := range c.caches {
			if cache.id == id {
				c.caches = append(c.caches[:i], c.caches[i+1:]...)
				w.WriteHeader(http.StatusNoContent)
				return true
			}
		}
		refuse(w, http.StatusNotFound)
	default:
		refuse(w, http.StatusNotFound)
	}
	return true
}

func defaultCaches() []fakeCache {
	return []fakeCache{
		{id: 501, key: "Linux-node-aaa", ref: "refs/heads/main", size: 1024, created: "2026-01-01T00:00:00Z"},
		{id: 502, key: "Linux-node-aaa", ref: "refs/pull/7/merge", size: 2048, created: "2026-01-02T00:00:00Z"},
		{id: 503, key: "Linux-node-aaa-extra", ref: "refs/heads/main", size: 4096, created: "2026-01-03T00:00:00Z"},
		{id: 504, key: "Linux-go-bbb\x1b[31m", ref: "refs/heads/main", size: 512, created: "2026-01-04T00:00:00Z"},
		{id: 505, key: "Windows-node-ccc", ref: "refs/heads/dev", size: 8192, created: "2026-01-05T00:00:00Z"},
	}
}

var cacheTools = []string{actionCachesList.ID, actionCacheUsageGet.ID, actionCachesDelete.ID}

// cachesConfig binds a repository connection that lists the cache tools, one that selects by effects only,
// and one whose targets cover the organization's repositories but exclude octo-org/example.
func cachesConfig(base string) *config.Config {
	cfg := coreConfig(base)
	effects := []config.Effect{config.EffectRead, config.EffectDelete}
	cfg.Connections["caches"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: effects, Tools: cacheTools}
	cfg.Connections["caches-unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: effects}
	cfg.Connections["caches-excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Exclude: []string{"repos/octo-org/example"}, Effects: effects,
		Tools: cacheTools}
	return cfg
}

func serveCaches(t *testing.T, caches []fakeCache, reads *int) (*fakeGitHub, *fakeCaches, *application.Core) {
	t.Helper()
	c := &fakeCaches{caches: caches}
	f := &fakeGitHub{failure: c.route}
	base := serve(t, f)
	red := &redact.Redactor{}
	return f, c, application.New(registry(t), cachesConfig(base), resolver(red, reads), red)
}

func requestsOf(f *fakeGitHub, before int, method string) []recorded {
	var matching []recorded
	for _, request := range f.recorded()[before:] {
		if request.method == method {
			matching = append(matching, request)
		}
	}
	return matching
}

// The list passes every filter GitHub documents, pages by a cursor bound to those filters, answers every
// member of a cache, and cleans the untrusted key.
func TestActionCachesListFiltersPagesAndAnswersEveryMember(t *testing.T) {
	f, _, core := serveCaches(t, defaultCaches(), nil)
	arguments := `{"key":"Linux-node-aaa","ref":"refs/heads/main","sort":"size_in_bytes","direction":"asc","limit":1}`
	result, err := invoke(t, core, actionCachesList.ID, "caches", arguments, false)
	if err != nil {
		t.Fatal(err)
	}
	var list ActionCacheList
	if err := json.Unmarshal(result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Caches) != 1 || list.TotalCount != 2 || !list.HasMore || list.NextCursor == "" {
		t.Fatalf("list = %s", result)
	}
	var first map[string]any
	var answer struct {
		Caches     []map[string]any `json:"caches"`
		Repository string           `json:"repository"`
	}
	if err := json.Unmarshal(result, &answer); err != nil || answer.Repository != "octo-org/example" {
		t.Fatalf("answer = %s, %v", result, err)
	}
	first = answer.Caches[0]
	for _, member := range []string{"id", "ref", "key", "version", "last_accessed_at", "created_at", "size_in_bytes"} {
		if _, ok := first[member]; !ok {
			t.Errorf("cache lacks %s: %s", member, result)
		}
	}
	if first["id"] != float64(501) || first["size_in_bytes"] != float64(1024) {
		t.Errorf("first cache = %v, want 501 as the smallest", first)
	}
	requests := f.recorded()
	query, _ := url.ParseQuery(requests[len(requests)-1].query)
	want := url.Values{"key": {"Linux-node-aaa"}, "ref": {"refs/heads/main"}, "sort": {"size_in_bytes"},
		"direction": {"asc"}, "per_page": {"1"}, "page": {"1"}}
	if query.Encode() != want.Encode() || !strings.HasSuffix(requests[len(requests)-1].path, "/repos/octo-org/example/actions/caches") {
		t.Errorf("request = %+v, want %v", requests[len(requests)-1], want)
	}

	next := `{"key":"Linux-node-aaa","ref":"refs/heads/main","sort":"size_in_bytes","direction":"asc","cursor":"` + list.NextCursor + `"}`
	result, err = invoke(t, core, actionCachesList.ID, "caches", next, false)
	var second ActionCacheList
	if err != nil || json.Unmarshal(result, &second) != nil || len(second.Caches) != 1 || second.Caches[0].ID != 503 ||
		second.HasMore {
		t.Errorf("second batch = %s, %v; want cache 503 and the end", result, err)
	}
	other := `{"key":"Linux","cursor":"` + list.NextCursor + `"}`
	if _, err := invoke(t, core, actionCachesList.ID, "caches", other, false); !isInvalidRequest(err) {
		t.Errorf("cursor with other filters = %v, want an invalid request", err)
	}

	result, err = invoke(t, core, actionCachesList.ID, "caches", `{"key":"Linux-go"}`, false)
	if err != nil || json.Unmarshal(result, &list) != nil || len(list.Caches) != 1 || list.Caches[0].Key != "Linux-go-bbb [31m" {
		t.Errorf("control characters in a key = %s, %v; want them replaced", result, err)
	}
}

func TestActionCacheUsageAnswersEveryMember(t *testing.T) {
	f, _, core := serveCaches(t, defaultCaches(), nil)
	result, err := invoke(t, core, actionCacheUsageGet.ID, "caches", `{}`, false)
	var usage ActionCacheUsage
	if err != nil || json.Unmarshal(result, &usage) != nil {
		t.Fatalf("usage = %s, %v", result, err)
	}
	if usage != (ActionCacheUsage{FullName: "octo-org/example", ActiveCachesSizeInBytes: 15872, ActiveCachesCount: 5}) {
		t.Errorf("usage = %+v", usage)
	}
	requests := f.recorded()
	if !strings.HasSuffix(requests[len(requests)-1].path, "/repos/octo-org/example/actions/cache/usage") {
		t.Errorf("request = %+v", requests[len(requests)-1])
	}
}

// A delete by cache_id finds the cache in the repository's own list first and sends exactly one delete; a
// cache found but gone when the delete arrives counts as done; an identifier the list does not show, such as
// one of another repository, is refused before anything is deleted.
func TestActionCachesDeleteByIDIsBoundToTheRepository(t *testing.T) {
	f, c, core := serveCaches(t, defaultCaches(), nil)
	before := len(f.recorded())
	result, err := invoke(t, core, actionCachesDelete.ID, "caches", `{"cache_id":502}`, true)
	var deleted ActionCachesDeleted
	if err != nil || json.Unmarshal(result, &deleted) != nil {
		t.Fatalf("delete = %s, %v", result, err)
	}
	if deleted.CacheID != 502 || deleted.DeletedCount != 1 || deleted.AlreadyDeleted || len(deleted.Caches) != 1 ||
		deleted.Caches[0].Ref != "refs/pull/7/merge" || deleted.Caches[0].Key != "Linux-node-aaa" {
		t.Errorf("delete = %s", result)
	}
	deletes := requestsOf(f, before, http.MethodDelete)
	if len(deletes) != 1 || !strings.HasSuffix(deletes[0].path, "/repos/octo-org/example/actions/caches/502") {
		t.Errorf("deletes = %+v, want exactly one below the repository", deletes)
	}
	if reads := requestsOf(f, before, http.MethodGet); len(reads) != 1 || !strings.Contains(reads[0].query, "sort=created_at") {
		t.Errorf("proof reads = %+v, want one page of the newest caches", reads)
	}

	// A foreign or unknown identifier is never sent.
	for _, id := range []string{"502", "999999"} {
		before = len(f.recorded())
		_, err = invoke(t, core, actionCachesDelete.ID, "caches", `{"cache_id":`+id+`}`, true)
		if classOf(err) != provider.ClassNotFound || strings.Contains(err.Error(), uncertain) {
			t.Errorf("delete of unknown cache %s = %v, want not found", id, err)
		}
		if deletes := requestsOf(f, before, http.MethodDelete); len(deletes) != 0 {
			t.Errorf("delete of unknown cache %s sent %+v", id, deletes)
		}
	}

	// GitHub answers not found when the cache went between the proof read and the delete.
	c.deleteAnswer = func(w http.ResponseWriter) { refuse(w, http.StatusNotFound) }
	result, err = invoke(t, core, actionCachesDelete.ID, "caches", `{"cache_id":501}`, true)
	deleted = ActionCachesDeleted{}
	if err != nil || json.Unmarshal(result, &deleted) != nil || !deleted.AlreadyDeleted || deleted.DeletedCount != 0 ||
		len(deleted.Caches) != 0 {
		t.Errorf("delete of a vanished cache = %s, %v; want already_deleted", result, err)
	}
}

// The proof read pages through the newest caches until it meets the identifier, and stops at its bound.
func TestActionCachesDeleteByIDPagesTheProofRead(t *testing.T) {
	caches := []fakeCache{}
	for i := 0; i < 1050; i++ {
		caches = append(caches, fakeCache{id: int64(2000 + i), key: "k", ref: "refs/heads/main", size: 1,
			created: fmt.Sprintf("2026-01-01T00:00:00Z%05d", i)})
	}
	f, _, core := serveCaches(t, caches, nil)
	// Cache 2900 is the 150th newest and sits on the second page.
	before := len(f.recorded())
	if _, err := invoke(t, core, actionCachesDelete.ID, "caches", `{"cache_id":2900}`, true); err != nil {
		t.Fatal(err)
	}
	if reads, deletes := requestsOf(f, before, http.MethodGet), requestsOf(f, before, http.MethodDelete); len(reads) != 2 ||
		len(deletes) != 1 {
		t.Errorf("reads = %d, deletes = %d; want two pages and one delete", len(reads), len(deletes))
	}
	// Cache 2000 is older than the newest 1000 and is refused without a delete.
	before = len(f.recorded())
	_, err := invoke(t, core, actionCachesDelete.ID, "caches", `{"cache_id":2000}`, true)
	if classOf(err) != provider.ClassNotFound || !strings.Contains(err.Error(), "newest 1000") {
		t.Errorf("delete beyond the bound = %v, want not found naming the bound", err)
	}
	if reads, deletes := requestsOf(f, before, http.MethodGet), requestsOf(f, before, http.MethodDelete); len(reads) != 10 ||
		len(deletes) != 0 {
		t.Errorf("reads = %d, deletes = %d; want ten pages and no delete", len(reads), len(deletes))
	}
}

// A delete by key removes the caches with exactly that key on the given ref, or on every ref with all_refs,
// and answers how many went; no matching cache counts as done with a count of zero.
func TestActionCachesDeleteByKey(t *testing.T) {
	f, c, core := serveCaches(t, defaultCaches(), nil)
	before := len(f.recorded())
	result, err := invoke(t, core, actionCachesDelete.ID, "caches", `{"key":"Linux-node-aaa","ref":"refs/heads/main"}`, true)
	var deleted ActionCachesDeleted
	if err != nil || json.Unmarshal(result, &deleted) != nil {
		t.Fatalf("delete = %s, %v", result, err)
	}
	if deleted.DeletedCount != 1 || deleted.AlreadyDeleted || deleted.AllRefs || deleted.Ref != "refs/heads/main" ||
		len(deleted.Caches) != 1 || deleted.Caches[0].ID != 501 || deleted.Truncated {
		t.Errorf("delete on one ref = %s", result)
	}
	deletes := requestsOf(f, before, http.MethodDelete)
	if len(deletes) != 1 || deletes[0].query != "key=Linux-node-aaa&ref=refs%2Fheads%2Fmain" ||
		!strings.HasSuffix(deletes[0].path, "/repos/octo-org/example/actions/caches") {
		t.Errorf("deletes = %+v, want one with key and ref", deletes)
	}

	// all_refs deletes the key on every ref and leaves a longer key with the same prefix.
	before = len(f.recorded())
	result, err = invoke(t, core, actionCachesDelete.ID, "caches", `{"key":"Linux-node-aaa","all_refs":true}`, true)
	deleted = ActionCachesDeleted{}
	if err != nil || json.Unmarshal(result, &deleted) != nil || deleted.DeletedCount != 1 || !deleted.AllRefs ||
		deleted.Caches[0].ID != 502 {
		t.Errorf("delete on every ref = %s, %v", result, err)
	}
	if deletes := requestsOf(f, before, http.MethodDelete); len(deletes) != 1 || deletes[0].query != "key=Linux-node-aaa" {
		t.Errorf("deletes = %+v, want one with the key alone", deletes)
	}
	if len(c.caches) != 3 || c.caches[0].id != 503 {
		t.Errorf("remaining caches = %+v, want the longer key kept", c.caches)
	}

	// Nothing left to delete is done, with a count of zero.
	result, err = invoke(t, core, actionCachesDelete.ID, "caches", `{"key":"Linux-node-aaa","all_refs":true}`, true)
	deleted = ActionCachesDeleted{}
	if err != nil || json.Unmarshal(result, &deleted) != nil || deleted.DeletedCount != 0 || !deleted.AlreadyDeleted {
		t.Errorf("delete without a match = %s, %v; want already_deleted with a count of zero", result, err)
	}
}

// Every refusal of the arguments, a repository outside the targets, and an excluded repository end before a
// credential is resolved and before GitHub is contacted.
func TestActionCachesArgumentsAndTargetAreCheckedBeforeIO(t *testing.T) {
	reads := 0
	f, _, core := serveCaches(t, defaultCaches(), &reads)
	for _, tt := range []struct{ name, id, connection, arguments string }{
		{"neither cache_id nor key", actionCachesDelete.ID, "caches", `{}`},
		{"cache_id and key", actionCachesDelete.ID, "caches", `{"cache_id":1,"key":"k","ref":"refs/heads/main"}`},
		{"key without ref or all_refs", actionCachesDelete.ID, "caches", `{"key":"k"}`},
		{"key with all_refs false", actionCachesDelete.ID, "caches", `{"key":"k","all_refs":false}`},
		{"key with ref and all_refs", actionCachesDelete.ID, "caches", `{"key":"k","ref":"refs/heads/main","all_refs":true}`},
		{"cache_id with ref", actionCachesDelete.ID, "caches", `{"cache_id":1,"ref":"refs/heads/main"}`},
		{"cache_id with all_refs", actionCachesDelete.ID, "caches", `{"cache_id":1,"all_refs":true}`},
		{"a short ref", actionCachesDelete.ID, "caches", `{"key":"k","ref":"main"}`},
		{"a ref with ..", actionCachesList.ID, "caches", `{"ref":"refs/heads/a..b"}`},
		{"a key with a comma", actionCachesDelete.ID, "caches", `{"key":"a,b","all_refs":true}`},
		{"a key with a C1 control character", actionCachesList.ID, "caches", `{"key":"a\u0085b"}`},
		{"an unknown sort", actionCachesList.ID, "caches", `{"sort":"key"}`},
		{"an unknown direction", actionCachesList.ID, "caches", `{"direction":"up"}`},
		{"a repository outside the targets", actionCachesDelete.ID, "caches", `{"repository":"octo-org/other","cache_id":1}`},
		{"a repository outside the targets of the list", actionCachesList.ID, "caches", `{"repository":"other-org/example"}`},
		{"an excluded repository", actionCachesDelete.ID, "caches-excluding",
			`{"repository":"octo-org/example","key":"k","all_refs":true}`},
		{"an excluded repository of the usage", actionCacheUsageGet.ID, "caches-excluding", `{"repository":"octo-org/example"}`},
		{"an excluded repository of the list", actionCachesList.ID, "caches-excluding", `{"repository":"octo-org/example"}`},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// The delete is offered only where a connection's tools list names it and needs confirmation; the reads are
// offered by effects. Both refusals end before a secret is read.
func TestActionCachesDeleteIsListedOnlyAndConfirmed(t *testing.T) {
	reads := 0
	f, _, core := serveCaches(t, defaultCaches(), &reads)
	for _, id := range []string{actionCachesList.ID, actionCacheUsageGet.ID} {
		if _, err := core.Describe(application.DescribeRequest{Operation: id, Connection: "caches-unlisted"}); err != nil {
			t.Errorf("describe %s without a tools list = %v, want it offered by effects", id, err)
		}
	}
	var unsupported *capability.UnsupportedError
	if _, err := core.Describe(application.DescribeRequest{Operation: actionCachesDelete.ID, Connection: "caches-unlisted"}); !errors.As(err, &unsupported) {
		t.Errorf("describe without a tools list = %v, want unsupported", err)
	}
	if _, err := invoke(t, core, actionCachesDelete.ID, "caches-unlisted", `{"cache_id":501}`, true); !errors.As(err, &unsupported) {
		t.Errorf("delete without a tools list = %v, want unsupported", err)
	}
	var confirm *application.ConfirmationRequiredError
	for _, arguments := range []string{`{"cache_id":501}`, `{"key":"Linux-node-aaa","all_refs":true}`} {
		if _, err := invoke(t, core, actionCachesDelete.ID, "caches", arguments, false); !errors.As(err, &confirm) {
			t.Errorf("unconfirmed delete %s = %v, want confirmation-required", arguments, err)
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
}

// A refusal names what the request needs in Qatlas' own words, never GitHub's text, and an unclear outcome of
// a delete says that it may have been applied, after exactly one delete.
func TestActionCachesFailuresAreNamedAndDeletesSentOnce(t *testing.T) {
	f, c, core := serveCaches(t, defaultCaches(), nil)
	byID, byKey := `{"cache_id":501}`, `{"key":"Linux-node-aaa","ref":"refs/heads/main"}`
	noText := func(name string, err error) {
		if err != nil && strings.Contains(err.Error(), cacheProviderCanary) {
			t.Errorf("%s quotes GitHub's text: %v", name, err)
		}
	}

	for _, tt := range []struct {
		status   int
		class    provider.Class
		fragment string
	}{
		{http.StatusForbidden, provider.ClassPermission, "Actions: read"},
		{http.StatusNotFound, provider.ClassNotFound, "does not hold the Actions caches"},
	} {
		c.readStatus = tt.status
		for _, id := range []string{actionCachesList.ID, actionCacheUsageGet.ID} {
			_, err := invoke(t, core, id, "caches", `{}`, false)
			if classOf(err) != tt.class || !strings.Contains(err.Error(), tt.fragment) {
				t.Errorf("%s answered %d = %v, want %s naming %q", id, tt.status, err, tt.class, tt.fragment)
			}
			noText(id, err)
		}
		// A refused proof read ends a delete before it is sent.
		for _, arguments := range []string{byID, byKey} {
			before := len(f.recorded())
			_, err := invoke(t, core, actionCachesDelete.ID, "caches", arguments, true)
			if classOf(err) != tt.class || strings.Contains(err.Error(), uncertain) {
				t.Errorf("delete %s with a read answered %d = %v, want %s", arguments, tt.status, err, tt.class)
			}
			if deletes := requestsOf(f, before, http.MethodDelete); len(deletes) != 0 {
				t.Errorf("delete %s with a refused read sent %d deletes", arguments, len(deletes))
			}
		}
	}
	c.readStatus = 0

	for _, tt := range []struct {
		name      string
		answer    func(http.ResponseWriter)
		class     provider.Class
		fragment  string
		uncertain bool
	}{
		{"forbidden", func(w http.ResponseWriter) { refuse(w, http.StatusForbidden) }, provider.ClassPermission,
			"Actions: read and write", false},
		{"conflict", func(w http.ResponseWriter) { refuse(w, http.StatusConflict) }, provider.ClassProviderError,
			conflictMessage, false},
		{"gateway timeout", func(w http.ResponseWriter) { refuse(w, http.StatusGatewayTimeout) }, provider.ClassTimeout,
			"did not answer in time", true},
		{"server error", func(w http.ResponseWriter) { refuse(w, http.StatusBadGateway) }, provider.ClassProviderError,
			"", true},
		{"dropped connection", func(w http.ResponseWriter) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		}, "", "", true},
	} {
		c.deleteAnswer = tt.answer
		for _, arguments := range []string{byID, byKey} {
			before := len(f.recorded())
			_, err := invoke(t, core, actionCachesDelete.ID, "caches", arguments, true)
			if err == nil || (tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain {
				t.Errorf("%s delete %s = %v, want %s naming %q, uncertain %t", tt.name, arguments, err, tt.class,
					tt.fragment, tt.uncertain)
			}
			noText(tt.name, err)
			if deletes := requestsOf(f, before, http.MethodDelete); len(deletes) != 1 {
				t.Errorf("%s delete %s sent %d deletes, want exactly one", tt.name, arguments, len(deletes))
			}
		}
	}

	// An unreadable answer to a delete by key leaves its outcome open as well.
	c.deleteAnswer = func(w http.ResponseWriter) { fmt.Fprint(w, "x") }
	before := len(f.recorded())
	if _, err := invoke(t, core, actionCachesDelete.ID, "caches", byKey, true); err == nil ||
		!strings.Contains(err.Error(), uncertain) {
		t.Errorf("unreadable delete answer = %v, want an uncertain outcome", err)
	}
	if deletes := requestsOf(f, before, http.MethodDelete); len(deletes) != 1 {
		t.Errorf("unreadable delete answer sent %d deletes, want exactly one", len(deletes))
	}
}
