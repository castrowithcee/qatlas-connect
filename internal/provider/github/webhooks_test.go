package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	webhookCanary    = "webhook-provider-text-canary-5b1d"
	webhookPathToken = "XXXXTOKENXXXX1234567890"
	webhookQueryTok  = "querysecret-77aa"
	webhookSecret    = "hook-secret-value-9f3c"
	webhookSignature = "sha256=5f4dcc3b5aa765d61d8327deb882cf99"
	webhookCookie    = "session=cookie-value-6d2e"
	webhookBearer    = "Bearer delivery-bearer-31ff"
)

const (
	webhookRepoRoute = "/repos/octo-org/example/hooks"
	webhookOrgRoute  = "/orgs/octo-org/hooks"
)

// fakeWebhooks answers the webhook routes of the repository octo-org/example and the organization octo-org.
// A webhook exists at one level only. The delivery log is paged by GitHub's own cursor.
type fakeWebhooks struct {
	mu     sync.Mutex
	f      *fakeGitHub
	hooks  map[string]map[int]map[string]any // route -> id -> hook
	status map[string]int                    // method -> status every request of it answers with
	// wrongID makes the webhook read answer another webhook than the one asked for.
	wrongID bool
}

func newWebhook(id int, active bool) map[string]any {
	return map[string]any{"type": "Repository", "id": float64(id), "name": "web", "active": active,
		"events": []any{"push", "pull_request"},
		"config": map[string]any{"url": "https://hooks.example.com/services/T000/B000/" + webhookPathToken +
			"?token=" + webhookQueryTok, "content_type": "json", "insecure_ssl": "0", "secret": webhookSecret},
		"updated_at": "2026-11-01T10:00:00Z", "created_at": "2026-10-01T10:00:00Z",
		"url":            "https://api.example.com/repos/octo-org/example/hooks/" + strconv.Itoa(id),
		"test_url":       "https://api.example.com/repos/octo-org/example/hooks/" + strconv.Itoa(id) + "/test",
		"ping_url":       "https://api.example.com/repos/octo-org/example/hooks/" + strconv.Itoa(id) + "/pings",
		"deliveries_url": "https://api.example.com/repos/octo-org/example/hooks/" + strconv.Itoa(id) + "/deliveries",
		"last_response":  map[string]any{"code": float64(200), "status": "active", "message": "OK"}}
}

func webhookDeliverySummary(id int) map[string]any {
	return map[string]any{"id": float64(id), "guid": "guid-" + strconv.Itoa(id), "delivered_at": "2026-11-02T10:00:00Z",
		"redelivery": false, "duration": 0.27, "status": "OK", "status_code": float64(200), "event": "push",
		"action": nil, "installation_id": nil, "repository_id": float64(77), "throttled_at": nil}
}

func serveWebhooks(t *testing.T) (*fakeGitHub, *fakeWebhooks, string) {
	t.Helper()
	w := &fakeWebhooks{status: map[string]int{}, hooks: map[string]map[int]map[string]any{
		webhookRepoRoute: {12: newWebhook(12, true), 13: newWebhook(13, false)},
		webhookOrgRoute:  {21: newWebhook(21, true)},
	}}
	f := &fakeGitHub{}
	w.f = f
	f.failure = w.route
	return f, w, serve(t, f)
}

func (w *fakeWebhooks) route(rw http.ResponseWriter, req *http.Request) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	escaped := strings.TrimPrefix(req.URL.EscapedPath(), "/api/v3")
	var route string
	switch {
	case strings.HasPrefix(escaped, webhookRepoRoute):
		route = webhookRepoRoute
	case strings.HasPrefix(escaped, webhookOrgRoute):
		route = webhookOrgRoute
	default:
		return false
	}
	rw.Header().Set("Content-Type", "application/json")
	if status := w.status[req.Method]; status != 0 {
		rw.WriteHeader(status)
		fmt.Fprintf(rw, `{"message":%q}`, webhookCanary)
		return true
	}
	write := func(value any) bool {
		data, _ := json.Marshal(value)
		rw.Write(data)
		return true
	}
	notFound := func() bool {
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, `{"message":"Not Found"}`)
		return true
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(escaped, route), "/")
	if rest == "" {
		var all []any
		for _, id := range []int{12, 13, 21} {
			if hook, ok := w.hooks[route][id]; ok {
				all = append(all, hook)
			}
		}
		perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		start := min((page-1)*perPage, len(all))
		end := min(start+perPage, len(all))
		if end < len(all) {
			rw.Header().Set("Link", `<https://api.example.com/hooks?page=`+strconv.Itoa(page+1)+`>; rel="next"`)
		}
		return write(all[start:end])
	}
	parts := strings.Split(rest, "/")
	id, _ := strconv.Atoi(parts[0])
	hook, found := w.hooks[route][id]
	if !found {
		return notFound()
	}
	if len(parts) == 1 {
		switch req.Method {
		case http.MethodGet:
			if w.wrongID {
				return write(newWebhook(id+1, true))
			}
			return write(hook)
		case http.MethodPatch:
			var body map[string]any
			if requests := w.f.recorded(); len(requests) > 0 {
				body = requests[len(requests)-1].body
			}
			if active, ok := body["active"].(bool); ok {
				hook["active"] = active
			}
			return write(hook)
		case http.MethodDelete:
			delete(w.hooks[route], id)
			rw.WriteHeader(http.StatusNoContent)
			return true
		}
		return false
	}
	if parts[1] != "deliveries" || req.Method != http.MethodGet {
		return false
	}
	if len(parts) == 2 {
		ids := []int{4100, 4099, 4098, 4097, 4096}
		cursor := req.URL.Query().Get("cursor")
		perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
		start := 0
		if cursor != "" {
			start, _ = strconv.Atoi(strings.TrimPrefix(cursor, "v1_"))
		}
		end := min(start+perPage, len(ids))
		var entries []any
		for _, delivery := range ids[start:end] {
			entries = append(entries, webhookDeliverySummary(delivery))
		}
		if end < len(ids) {
			rw.Header().Set("Link", `<https://api.example.com/hooks/12/deliveries?per_page=`+strconv.Itoa(perPage)+
				`&cursor=v1_`+strconv.Itoa(end)+`>; rel="next"`)
		}
		return write(entries)
	}
	delivery, _ := strconv.Atoi(parts[2])
	if delivery != 4100 {
		return notFound()
	}
	detail := webhookDeliverySummary(delivery)
	detail["url"] = "https://hooks.example.com/services/T000/B000/" + webhookPathToken + "?token=" + webhookQueryTok
	detail["request"] = map[string]any{
		"headers": map[string]any{"X-GitHub-Event": "push", "Authorization": webhookBearer,
			"X-Hub-Signature-256": webhookSignature, "Content-Type": "application/json"},
		"payload": map[string]any{"ref": "refs/heads/main", "pusher": map[string]any{"name": "octocat"},
			"filler": strings.Repeat("x", 3000)}}
	detail["response"] = map[string]any{
		"headers": map[string]any{"Set-Cookie": webhookCookie, "Content-Length": "2"},
		"payload": "ok"}
	return write(detail)
}

// webhooksConfig binds every shape of connection the webhook tools distinguish; the listed ones name all
// seven tools, the others none.
func webhooksConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	tools := append(append([]string{}, webhooksReads...), webhooksChanges...)
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["repo-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: tools}
	cfg.Connections["org-listed"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Effects: all, Tools: tools}
	cfg.Connections["open-listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: all, Tools: tools}
	cfg.Connections["repo-excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"repos/octo-org/example"}, Effects: all, Tools: tools}
	cfg.Connections["org-excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"orgs/octo-org"}, Effects: all, Tools: tools}
	return cfg
}

func webhooksCore(t *testing.T, base string, reads *int) *application.Core {
	t.Helper()
	red := &redact.Redactor{}
	return application.New(registry(t), webhooksConfig(base), resolver(red, reads), red)
}

func noWebhookCredentials(t *testing.T, text string) {
	t.Helper()
	for _, leaked := range []string{webhookPathToken, webhookQueryTok, webhookSecret, webhookSignature, webhookCookie,
		webhookBearer, "Authorization", "Set-Cookie", "secret"} {
		if strings.Contains(text, leaked) {
			t.Errorf("the output holds %q: %s", leaked, text)
		}
	}
}

// Both levels list and read their webhooks and deliveries with every documented member, the answers name the
// level, and no secret, token, signature, cookie, or authorization header reaches the output.
func TestWebhooksReadAtBothLevelsWithoutCredentials(t *testing.T) {
	_, _, base := serveWebhooks(t)
	core := webhooksCore(t, base, nil)
	hook := []string{`"id":12`, `"type":"Repository"`, `"name":"web"`, `"active":true`, `"events":["push","pull_request"]`,
		`"content_type":"json"`, `"insecure_ssl":"0"`, `"url":"https://hooks.example.com/services/***/***/***?token=***"`,
		`"url_masked":true`, `"updated_at":"2026-11-01T10:00:00Z"`, `"created_at":"2026-10-01T10:00:00Z"`,
		`"last_response":{"code":200,"message":"OK","status":"active"}`, `"deliveries_url":"https://api.example.com/`,
		`"test_url"`, `"ping_url"`}
	delivery := []string{`"id":4100`, `"guid":"guid-4100"`, `"delivered_at"`, `"redelivery":false`, `"duration":0.27`,
		`"status":"OK"`, `"status_code":200`, `"event":"push"`, `"repository_id":77`}
	for _, tt := range []struct {
		tool, connection, arguments string
		want                        []string
	}{
		{webhooksList.ID, "repo-listed", `{"repository":"octo-org/example"}`,
			append([]string{`"repository":"octo-org/example"`, `"id":13`}, hook...)},
		{webhooksList.ID, "org-listed", `{"organization":"orgs/octo-org"}`,
			[]string{`"organization":"orgs/octo-org"`, `"id":21`}},
		{webhooksGet.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":12}`,
			append([]string{`"repository":"octo-org/example"`}, hook...)},
		{webhooksGet.ID, "org-listed", `{"organization":"orgs/octo-org","hook_id":21}`,
			[]string{`"organization":"orgs/octo-org"`, `"id":21`}},
		{webhookDeliveriesList.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":12}`,
			append([]string{`"hook_id":12`, `"has_more":false`, `"id":4096`}, delivery...)},
		{webhookDeliveriesGet.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":12,"delivery_id":4100}`,
			append([]string{`"hook_id":12`, `"url":"https://hooks.example.com/services/***/***/***?token=***"`,
				`"url_masked":true`, `"X-GitHub-Event":"push"`, `"Content-Type":"application/json"`,
				`"headers_omitted":2`, `"headers_omitted":1`, `"payload_truncated":true`, `"payload":"ok"`,
				`{\"filler\":\"xxxx`}, delivery...)},
	} {
		got, err := invoke(t, core, tt.tool, tt.connection, tt.arguments, false)
		if err != nil {
			t.Errorf("%s %s = %v", tt.tool, tt.arguments, err)
			continue
		}
		for _, want := range tt.want {
			if !strings.Contains(string(got), want) {
				t.Errorf("%s %s lacks %s in %s", tt.tool, tt.arguments, want, got)
			}
		}
		noWebhookCredentials(t, string(got))
	}
}

// The list is paged by number and the delivery log by GitHub's cursor; a cursor of another list is refused.
func TestWebhookListsPage(t *testing.T) {
	_, _, base := serveWebhooks(t)
	core := webhooksCore(t, base, nil)
	first, err := invoke(t, core, webhooksList.ID, "repo-listed", `{"repository":"octo-org/example","limit":1}`, false)
	var page struct {
		Webhooks   []struct{ ID int } `json:"webhooks"`
		NextCursor string             `json:"next_cursor"`
		HasMore    bool               `json:"has_more"`
	}
	if err != nil || json.Unmarshal(first, &page) != nil || !page.HasMore || page.NextCursor == "" || len(page.Webhooks) != 1 {
		t.Fatalf("first batch = %s, %v", first, err)
	}
	second, err := invoke(t, core, webhooksList.ID, "repo-listed",
		`{"repository":"octo-org/example","limit":1,"cursor":"`+page.NextCursor+`"}`, false)
	if err != nil || !strings.Contains(string(second), `"id":13`) || !strings.Contains(string(second), `"has_more":false`) {
		t.Errorf("second batch = %s, %v", second, err)
	}
	if _, err := invoke(t, core, webhooksList.ID, "repo-listed",
		`{"organization":"orgs/octo-org","limit":1,"cursor":"`+page.NextCursor+`"}`, false); err == nil {
		t.Error("a cursor of another list was accepted")
	}

	var deliveries struct {
		Deliveries []struct{ ID int } `json:"deliveries"`
		NextCursor string             `json:"next_cursor"`
		HasMore    bool               `json:"has_more"`
	}
	args := `{"repository":"octo-org/example","hook_id":12,"limit":2`
	got, err := invoke(t, core, webhookDeliveriesList.ID, "repo-listed", args+`}`, false)
	if err != nil || json.Unmarshal(got, &deliveries) != nil || !deliveries.HasMore || len(deliveries.Deliveries) != 2 ||
		deliveries.Deliveries[0].ID != 4100 {
		t.Fatalf("first deliveries = %s, %v", got, err)
	}
	next, err := invoke(t, core, webhookDeliveriesList.ID, "repo-listed", args+`,"cursor":"`+deliveries.NextCursor+`"}`, false)
	if err != nil || !strings.Contains(string(next), `"id":4098`) || strings.Contains(string(next), `"id":4100`) {
		t.Errorf("second deliveries = %s, %v", next, err)
	}
	for name, cursor := range alteredCursors(t, deliveries.NextCursor) {
		if _, err := invoke(t, core, webhookDeliveriesList.ID, "repo-listed", args+`,"cursor":"`+cursor+`"}`, false); err == nil {
			t.Errorf("an altered cursor (%s) was accepted", name)
		}
	}
	if _, err := invoke(t, core, webhookDeliveriesList.ID, "repo-listed",
		`{"repository":"octo-org/example","hook_id":13,"limit":2,"cursor":"`+deliveries.NextCursor+`"}`, false); err == nil {
		t.Error("a delivery cursor of another webhook was accepted")
	}
}

// pause and resume send exactly one PATCH carrying only active, after one evidence read; delete sends one
// DELETE after one read.
func TestWebhookChangesSendOneRequestWithOnlyActive(t *testing.T) {
	f, w, base := serveWebhooks(t)
	core := webhooksCore(t, base, nil)
	for _, tt := range []struct {
		tool       string
		connection string
		arguments  string
		active     bool
		route      string
		id         int
	}{
		{webhooksPause.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":12}`, false, webhookRepoRoute, 12},
		{webhooksResume.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":13}`, true, webhookRepoRoute, 13},
		{webhooksPause.ID, "org-listed", `{"organization":"orgs/octo-org","hook_id":21}`, false, webhookOrgRoute, 21},
	} {
		before := len(f.recorded())
		got, err := invoke(t, core, tt.tool, tt.connection, tt.arguments, true)
		if err != nil || !strings.Contains(string(got), fmt.Sprintf(`"active":%t`, tt.active)) ||
			!strings.Contains(string(got), `"id":`+strconv.Itoa(tt.id)) {
			t.Fatalf("%s = %s, %v", tt.tool, got, err)
		}
		requests := f.recorded()[before:]
		if !reflect.DeepEqual(methodCounts(requests), map[string]int{http.MethodGet: 1, http.MethodPatch: 1}) {
			t.Errorf("%s sent %v, want one read and one change", tt.tool, methodCounts(requests))
		}
		for _, request := range requests {
			if request.method == http.MethodPatch && !reflect.DeepEqual(request.body, map[string]any{"active": tt.active}) {
				t.Errorf("%s body = %v, want only active=%t", tt.tool, request.body, tt.active)
			}
		}
		noWebhookCredentials(t, string(got))
		if w.hooks[tt.route][tt.id]["active"] != tt.active {
			t.Errorf("%s left the webhook active=%v", tt.tool, w.hooks[tt.route][tt.id]["active"])
		}
	}

	before := len(f.recorded())
	got, err := invoke(t, core, webhooksDelete.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":13}`, true)
	if err != nil || !strings.Contains(string(got), `"deleted":true`) || !strings.Contains(string(got), `"hook_id":13`) {
		t.Fatalf("delete = %s, %v", got, err)
	}
	if got := methodCounts(f.recorded()[before:]); !reflect.DeepEqual(got, map[string]int{http.MethodGet: 1, http.MethodDelete: 1}) {
		t.Errorf("delete sent %v, want one read and one delete", got)
	}
	if _, ok := w.hooks[webhookRepoRoute][13]; ok {
		t.Error("the webhook is still there")
	}
}

// A webhook of the other level, or an evidence read naming another webhook, ends the call before anything is
// changed.
func TestWebhookIDsAreBoundByAnEvidenceRead(t *testing.T) {
	f, w, base := serveWebhooks(t)
	core := webhooksCore(t, base, nil)
	before := len(f.recorded())
	for _, tt := range []struct{ tool, connection, arguments string }{
		{webhooksDelete.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":21}`},
		{webhooksDelete.ID, "org-listed", `{"organization":"orgs/octo-org","hook_id":12}`},
		{webhooksPause.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":21}`},
		{webhooksResume.ID, "org-listed", `{"organization":"orgs/octo-org","hook_id":12}`},
		{webhooksGet.ID, "org-listed", `{"organization":"orgs/octo-org","hook_id":12}`},
		{webhookDeliveriesGet.ID, "org-listed", `{"organization":"orgs/octo-org","hook_id":12,"delivery_id":4100}`},
		{webhookDeliveriesGet.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":12,"delivery_id":1}`},
	} {
		_, err := invoke(t, core, tt.tool, tt.connection, tt.arguments, true)
		if classOf(err) != provider.ClassNotFound || err == nil || strings.Contains(err.Error(), webhookCanary) {
			t.Errorf("%s %s = %v, want not-found", tt.tool, tt.arguments, err)
		}
	}
	for _, request := range f.recorded()[before:] {
		if request.method != http.MethodGet {
			t.Errorf("a foreign ID sent %s %s", request.method, request.path)
		}
	}
	w.wrongID = true
	for _, tool := range []string{webhooksDelete.ID, webhooksPause.ID, webhooksResume.ID} {
		before = len(f.recorded())
		_, err := invoke(t, core, tool, "repo-listed", `{"repository":"octo-org/example","hook_id":12}`, true)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s with an evidence read naming another webhook = %v, want invalid-provider-response", tool, err)
		}
		if got := methodCounts(f.recorded()[before:]); got[http.MethodDelete] != 0 || got[http.MethodPatch] != 0 {
			t.Errorf("%s sent %v after a wrong evidence read", tool, got)
		}
	}
}

// The three changes are offered only where a connection's tools list names them; otherwise they are refused
// before a secret is read, and a listed change needs its confirmation. The four reads need no entry.
func TestWebhookChangesNeedTheirNameInTheToolsList(t *testing.T) {
	f, _, base := serveWebhooks(t)
	reads := 0
	core := webhooksCore(t, base, &reads)
	calls := []struct{ id, arguments string }{
		{webhooksPause.ID, `{"repository":"octo-org/example","hook_id":12}`},
		{webhooksResume.ID, `{"repository":"octo-org/example","hook_id":12}`},
		{webhooksDelete.ID, `{"repository":"octo-org/example","hook_id":12}`},
	}
	for _, tt := range calls {
		var unsupported *capability.UnsupportedError
		if _, err := core.Describe(application.DescribeRequest{Operation: tt.id, Connection: "repo"}); !errors.As(err, &unsupported) {
			t.Errorf("describe %s = %v, want unsupported", tt.id, err)
		}
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, "repo", tt.arguments, true); !errors.As(err, &unsupported) {
			t.Errorf("invoke %s = %v, want unsupported", tt.id, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.id)
		}
	}
	unconfirmed := &application.ConfirmationRequiredError{}
	for _, tt := range calls {
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, "repo-listed", tt.arguments, false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirmation = %v, want confirmation-required", tt.id, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s reached GitHub before confirmation", tt.id)
		}
	}
	for _, tt := range []struct{ id, arguments string }{
		{webhooksList.ID, `{"repository":"octo-org/example"}`},
		{webhooksGet.ID, `{"repository":"octo-org/example","hook_id":12}`},
		{webhookDeliveriesList.ID, `{"repository":"octo-org/example","hook_id":12}`},
		{webhookDeliveriesGet.ID, `{"repository":"octo-org/example","hook_id":12,"delivery_id":4100}`},
	} {
		if _, err := core.Describe(application.DescribeRequest{Operation: tt.id, Connection: "repo"}); err != nil {
			t.Errorf("describe %s on a connection without a tools list = %v, want offered", tt.id, err)
		}
		got, err := invoke(t, core, tt.id, "repo", tt.arguments, false)
		if err != nil {
			t.Errorf("%s on a connection without a tools list = %v", tt.id, err)
		}
		noWebhookCredentials(t, string(got))
	}
}

// repository and organization are exclusive, a target outside the connection or excluded is refused, and every
// check, the identifiers included, runs before a credential is resolved.
func TestWebhookTargetsAndArgumentsAreCheckedBeforeIO(t *testing.T) {
	f, _, base := serveWebhooks(t)
	reads := 0
	core := webhooksCore(t, base, &reads)
	for _, tt := range []struct{ name, tool, connection, arguments string }{
		{"neither", webhooksGet.ID, "open-listed", `{"hook_id":12}`},
		{"both", webhooksGet.ID, "open-listed", `{"repository":"octo-org/example","organization":"orgs/octo-org","hook_id":12}`},
		{"an organization outside the targets", webhooksDelete.ID, "repo-listed", `{"organization":"orgs/other-org","hook_id":12}`},
		{"a repository outside the targets", webhooksPause.ID, "repo-listed", `{"repository":"octo-org/other","hook_id":12}`},
		{"an organization outside the targets of an organization", webhooksList.ID, "org-listed", `{"organization":"orgs/other-org"}`},
		{"a user owner", webhooksList.ID, "open-listed", `{"organization":"users/octocat"}`},
		{"an excluded repository", webhooksDelete.ID, "repo-excluded", `{"repository":"octo-org/example","hook_id":12}`},
		{"an excluded organization", webhooksResume.ID, "org-excluded", `{"organization":"orgs/octo-org","hook_id":21}`},
		{"an excluded organization read", webhookDeliveriesGet.ID, "org-excluded",
			`{"organization":"orgs/octo-org","hook_id":21,"delivery_id":4100}`},
		{"a missing hook_id", webhooksGet.ID, "repo-listed", `{"repository":"octo-org/example"}`},
		{"a zero hook_id", webhooksDelete.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":0}`},
		{"a negative hook_id", webhooksPause.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":-4}`},
		{"a missing delivery_id", webhookDeliveriesGet.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":12}`},
		{"a delivery_id as text", webhookDeliveriesGet.ID, "repo-listed",
			`{"repository":"octo-org/example","hook_id":12,"delivery_id":"4100"}`},
		{"a limit beyond the bound", webhooksList.ID, "repo-listed", `{"repository":"octo-org/example","limit":101}`},
		{"a config argument", webhooksPause.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":12,"config":{"url":"https://x.example"}}`},
		{"a secret argument", webhooksPause.ID, "repo-listed", `{"repository":"octo-org/example","hook_id":12,"secret":"x"}`},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.tool, tt.connection, tt.arguments, true)
		if err == nil {
			t.Errorf("%s was accepted", tt.name)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub (%v)", tt.name, err)
		}
	}
}

// GitHub's refusals become Qatlas' own messages naming the right each level needs, without GitHub's text; a
// refusal or an unclear outcome of a change is sent once and never repeated.
func TestWebhookFailuresNameTheirMeaningWithoutProviderText(t *testing.T) {
	f, w, base := serveWebhooks(t)
	core := webhooksCore(t, base, nil)
	levels := map[string]struct {
		connection, target, right string
		id                        int
	}{
		"repository":   {"repo-listed", `"repository":"octo-org/example"`, "admin:repo_hook", 12},
		"organization": {"org-listed", `"organization":"orgs/octo-org"`, "admin:org_hook", 21},
	}
	for name, level := range levels {
		for _, tt := range []struct {
			method    string
			status    int
			class     provider.Class
			fragment  string
			uncertain bool
			patches   int
			deletes   int
		}{
			{http.MethodGet, http.StatusForbidden, provider.ClassPermission, level.right, false, 0, 0},
			{http.MethodPatch, http.StatusForbidden, provider.ClassPermission, level.right, false, 1, 0},
			{http.MethodPatch, http.StatusUnprocessableEntity, provider.ClassProviderError, "nothing was changed", false, 1, 0},
			{http.MethodPatch, http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true, 1, 0},
			{http.MethodPatch, http.StatusBadGateway, provider.ClassProviderError, "HTTP 502", true, 1, 0},
			{http.MethodDelete, http.StatusForbidden, provider.ClassPermission, level.right, false, 0, 1},
			{http.MethodDelete, http.StatusUnprocessableEntity, provider.ClassProviderError, "nothing was deleted", false, 0, 1},
			{http.MethodDelete, http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true, 0, 1},
		} {
			w.status = map[string]int{tt.method: tt.status}
			tool := webhooksPause.ID
			if tt.method == http.MethodDelete {
				tool = webhooksDelete.ID
			}
			if tt.method == http.MethodGet {
				tool = webhooksGet.ID
			}
			before := len(f.recorded())
			_, err := invoke(t, core, tool, level.connection, `{`+level.target+`,"hook_id":`+strconv.Itoa(level.id)+`}`, true)
			if classOf(err) != tt.class || err == nil || !strings.Contains(err.Error(), tt.fragment) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain || strings.Contains(err.Error(), webhookCanary) {
				t.Errorf("%s %s answered %d = %v, want %s naming %q", name, tt.method, tt.status, err, tt.class, tt.fragment)
			}
			counts := methodCounts(f.recorded()[before:])
			if counts[http.MethodPatch] != tt.patches || counts[http.MethodDelete] != tt.deletes {
				t.Errorf("%s %s answered %d sent %v, want %d changes and %d deletes", name, tt.method, tt.status, counts,
					tt.patches, tt.deletes)
			}
		}
		w.status = map[string]int{http.MethodGet: http.StatusForbidden}
		for _, tool := range []string{webhooksList.ID, webhookDeliveriesList.ID} {
			_, err := invoke(t, core, tool, level.connection, `{`+level.target+`,"hook_id":`+strconv.Itoa(level.id)+`}`, false)
			if tool == webhooksList.ID {
				_, err = invoke(t, core, tool, level.connection, `{`+level.target+`}`, false)
			}
			if classOf(err) != provider.ClassPermission || err == nil || !strings.Contains(err.Error(), "reading them needs") ||
				strings.Contains(err.Error(), webhookCanary) {
				t.Errorf("%s %s read refused = %v, want the read right", name, tool, err)
			}
		}
		w.status = map[string]int{}
	}
}

func TestMaskWebhookURL(t *testing.T) {
	for _, tt := range []struct {
		raw    string
		want   string
		masked bool
	}{
		{"https://hooks.example.com/github-webhook", "https://hooks.example.com/github-webhook", false},
		{"https://hooks.slack.com/services/T000/B000/abcdefghij", "https://hooks.slack.com/services/***/***/***", true},
		{"https://discord.com/api/webhooks/123456/TokenValue_x", "https://discord.com/api/webhooks/***/***", true},
		{"https://example.com/hook?token=abc&x", "https://example.com/hook?token=***&x", true},
		{"https://user:pass@example.com:8443/hook#frag", "https://***@example.com:8443/hook", true},
		{"https://example.com/Hook", "https://example.com/***", true},
		{"ftp://example.com/hook", "[unreadable address]", true},
		{"not a url", "[unreadable address]", true},
		{"https://example.com/hook\x00x", "[unreadable address]", true},
	} {
		got, masked, _ := maskWebhookURL(tt.raw)
		if got != tt.want || masked != tt.masked {
			t.Errorf("maskWebhookURL(%q) = %q, %t, want %q, %t", tt.raw, got, masked, tt.want, tt.masked)
		}
	}
	long := "https://example.com/" + strings.Repeat("a", 300)
	if got, _, cut := maskWebhookURL("https://example.com/hook?" + strings.Repeat("k", 5) + "=" + long); len(got) > webhookURLLimit || cut {
		t.Errorf("a long address = %d bytes, cut %t", len(got), cut)
	}
	if got, _, cut := maskWebhookURL("https://example.com/hook?" + strings.Repeat("k=1&", 100)); !cut || len(got) != webhookURLLimit {
		t.Errorf("a long query = %d bytes, cut %t, want a cut at the bound", len(got), cut)
	}
}

func TestWebhookSecretHeaders(t *testing.T) {
	for name, want := range map[string]bool{"Authorization": true, "Proxy-Authorization": true, "Set-Cookie": true,
		"X-Hub-Signature-256": true, "X-Api-Key": true, "X-Auth-Token": true, "Content-Type": false,
		"X-GitHub-Event": false, "User-Agent": false} {
		if got := webhookSecretHeader(name); got != want {
			t.Errorf("webhookSecretHeader(%q) = %t, want %t", name, got, want)
		}
	}
}
