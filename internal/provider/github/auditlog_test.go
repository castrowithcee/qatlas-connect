package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	auditUndocumentedCanary = "audit-undocumented-key-canary-6e1f"
	auditTextCanary         = "audit-provider-text-canary-2b9d"
	auditPath               = "/api/v3/orgs/octo-org/audit-log"
)

// auditEventJSON is an event with every documented member plus keys that must never reach a result.
var auditEventJSON = fmt.Sprintf(`{"@timestamp":1700000000000,"action":"repo.create","active":true,"active_was":false,`+
	`"actor":"hubot","actor_id":9,"actor_location":{"country_name":"Iceland","city":%[1]q},`+
	`"data":{"note":"text","count":3,"flag":true,"nothing":null,"nested":{"a":1}},"org_id":7,"user_id":8,`+
	`"business_id":6,"blocked_user":"mallory","business":"octo-biz","config":[{"url":"https://example.com","n":1}],`+
	`"config_was":[{"n":0}],"content_type":"json","operation_type":"create","created_at":1700000000001,`+
	`"deploy_key_fingerprint":"fp-1","_document_id":"doc-1","emoji":"x","events":[{"name":"push"}],`+
	`"events_were":[{"name":"issues"}],"explanation":"why","fingerprint":"fp-2","hook_id":5,`+
	`"limited_availability":false,"message":"hello","name":"thing","old_user":"old","openssh_public_key":"ssh-ed25519 AAAA",`+
	`"org":"octo-org","previous_visibility":"private","read_only":false,"repo":"octo-org/example",`+
	`"repository":"octo-org/example","repository_public":true,"target_login":"target","team":"core",`+
	`"transport_protocol":1,"transport_protocol_name":"ssh","user":"octocat","visibility":"public",`+
	`"actor_ip":%[1]q,"user_agent":%[1]q,"token":%[1]q}`, auditUndocumentedCanary)

type fakeAudit struct {
	mu     sync.Mutex
	events []string
	status int
	// next is the cursor the Link header names, empty for none.
	next string
	// last is the query of the latest audit log request.
	last url.Values
}

func serveAudit(t *testing.T) (*fakeGitHub, *fakeAudit, *application.Core, *int) {
	t.Helper()
	a := &fakeAudit{events: []string{auditEventJSON}}
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path != auditPath {
			return false
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		a.last = req.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		if a.status != 0 {
			w.WriteHeader(a.status)
			fmt.Fprintf(w, `{"message":%q}`, auditTextCanary)
			return true
		}
		if a.next != "" {
			w.Header().Set("Link", `<https://api.github.com/orgs/octo-org/audit-log?after=`+url.QueryEscape(a.next)+
				`&per_page=2>; rel="next"`)
		}
		fmt.Fprintf(w, `[%s]`, strings.Join(a.events, ","))
		return true
	}
	base := serve(t, f)
	reads := 0
	red := &redact.Redactor{}
	cfg := appsConfig(base)
	cfg.Connections["audit-excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"orgs/octo-org"}, Exclude: []string{"repos/octo-org/secret"},
		Effects: []config.Effect{config.EffectRead}}
	return f, a, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func TestAuditLogListGivesEveryDocumentedMemberAndNoOther(t *testing.T) {
	f, a, core, _ := serveAudit(t)
	out, err := invoke(t, core, auditLogList.ID, "org",
		`{"owner":"orgs/octo-org","phrase":"action:repo.create","include":"all","order":"asc","limit":2}`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range auditLogEntries {
		if strings.HasSuffix(entry[0], "_truncated") {
			continue
		}
		if !strings.Contains(string(out), `"`+entry[0]+`":`) {
			t.Errorf("result lacks %s: %s", entry[0], out)
		}
	}
	for _, want := range []string{`"owner":"orgs/octo-org"`, `"country_name":"Iceland"`,
		`"data":{"count":3,"flag":true,"note":"text"}`, `"data_truncated":true`, `"has_more":false`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("result lacks %s: %s", want, out)
		}
	}
	for _, unwanted := range []string{auditUndocumentedCanary, "actor_ip", "user_agent", `"token"`, `"city"`, `"nothing"`} {
		if strings.Contains(string(out), unwanted) {
			t.Errorf("result shows %q: %s", unwanted, out)
		}
	}
	for key, want := range map[string]string{"phrase": "action:repo.create", "include": "all", "order": "asc", "per_page": "2"} {
		if got := a.last.Get(key); got != want {
			t.Errorf("request %s = %q, want %q", key, got, want)
		}
	}
	if got := f.recorded(); len(got) == 0 || got[len(got)-1].method != http.MethodGet {
		t.Errorf("requests = %+v, want one GET", got)
	}
}

func TestAuditLogListWithoutFiltersSendsOnlyThePageSize(t *testing.T) {
	_, a, core, _ := serveAudit(t)
	if _, err := invoke(t, core, auditLogList.ID, "org", `{"owner":"orgs/octo-org"}`, false); err != nil {
		t.Fatal(err)
	}
	if len(a.last) != 1 || a.last.Get("per_page") != "30" {
		t.Errorf("query = %v, want per_page=30 only", a.last)
	}
}

func TestAuditLogListPagesByTheLinkCursorAndNeverFetchesIt(t *testing.T) {
	f, a, core, _ := serveAudit(t)
	a.next = "cursor+with/odd=chars"
	first, err := invoke(t, core, auditLogList.ID, "org", `{"owner":"orgs/octo-org","limit":2,"phrase":"x"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	cursor := between(string(first), `"next_cursor":"`, `"`)
	if cursor == "" || !strings.Contains(string(first), `"has_more":true`) {
		t.Fatalf("first batch = %s, want a cursor", first)
	}
	a.next = ""
	last, err := invoke(t, core, auditLogList.ID, "org",
		`{"owner":"orgs/octo-org","phrase":"x","cursor":"`+cursor+`"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if a.last.Get("after") != "cursor+with/odd=chars" || a.last.Get("per_page") != "2" {
		t.Errorf("continuation query = %v, want the Link cursor and the first batch size", a.last)
	}
	if strings.Contains(string(last), `"next_cursor"`) {
		t.Errorf("last batch = %s, want no cursor", last)
	}
	for _, request := range f.recorded() {
		if request.path != auditPath {
			t.Errorf("request to %s, want only the audit log route", request.path)
		}
	}
	for name, arguments := range map[string]string{
		"other phrase": `{"owner":"orgs/octo-org","phrase":"y","cursor":"` + cursor + `"}`,
		"other order":  `{"owner":"orgs/octo-org","phrase":"x","order":"asc","cursor":"` + cursor + `"}`,
		"forged":       `{"owner":"orgs/octo-org","cursor":"AAAA"}`,
	} {
		if _, err := invoke(t, core, auditLogList.ID, "org", arguments, false); err == nil {
			t.Errorf("%s: cursor accepted", name)
		}
	}
}

func TestAuditLogListRefusalsAreSettledBeforeIO(t *testing.T) {
	f, _, core, reads := serveAudit(t)
	for _, tt := range []struct{ name, connection, arguments, message string }{
		{"owner outside the targets", "org", `{"owner":"orgs/other"}`, "outside the targets"},
		{"excluded owner", "excluded-owner", `{"owner":"orgs/octo-org"}`, "not available"},
		{"repository target only", "repo-only", `{}`, ""},
		{"user owner", "user", `{"owner":"users/octocat"}`, "organization"},
		{"malformed owner", "org", `{"owner":"octo-org"}`, ""},
		{"limit above the bound", "org", `{"owner":"orgs/octo-org","limit":101}`, ""},
		{"unknown include", "org", `{"owner":"orgs/octo-org","include":"everything"}`, ""},
		{"unknown order", "org", `{"owner":"orgs/octo-org","order":"random"}`, ""},
		{"control character in the phrase", "org", `{"owner":"orgs/octo-org","phrase":"a\u0000b"}`, ""},
		{"phrase over the bound", "org", `{"owner":"orgs/octo-org","phrase":"` + strings.Repeat("a", 501) + `"}`, ""},
		{"free route", "org", `{"owner":"orgs/octo-org","path":"/x"}`, ""},
		{"free page", "org", `{"owner":"orgs/octo-org","after":"x"}`, ""},
	} {
		*reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, auditLogList.ID, tt.connection, tt.arguments, false); err == nil ||
			(tt.message != "" && !strings.Contains(err.Error(), tt.message)) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential (%d) or GitHub", tt.name, *reads)
		}
	}
}

func TestAuditLogListPassesThePhraseAsTextOnly(t *testing.T) {
	_, a, core, _ := serveAudit(t)
	phrase := `action:repo.create&per_page=1 "quoted" ../x`
	arguments, _ := json.Marshal(map[string]any{"owner": "orgs/octo-org", "phrase": phrase})
	if _, err := invoke(t, core, auditLogList.ID, "org", string(arguments), false); err != nil {
		t.Fatal(err)
	}
	if a.last.Get("phrase") != phrase || a.last.Get("per_page") != "30" {
		t.Errorf("query = %v, want the phrase as one value", a.last)
	}
}

func TestAuditLogListRefusalIsTheUnavailableStateWithAFixedReason(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		_, a, core, _ := serveAudit(t)
		a.status = status
		out, err := invoke(t, core, auditLogList.ID, "org", `{"owner":"orgs/octo-org"}`, false)
		if err != nil {
			t.Fatalf("%d: %v", status, err)
		}
		if !strings.Contains(string(out), `"state":"unavailable"`) || !strings.Contains(string(out), `"reason":`) ||
			strings.Contains(string(out), auditTextCanary) || strings.Contains(string(out), `"events"`) {
			t.Errorf("%d: result = %s, want the state with the fixed reason", status, out)
		}
	}
}

func TestAuditLogListTimeoutAndServerErrorsAreErrorsAndNeverRepeated(t *testing.T) {
	for status, class := range map[int]provider.Class{
		http.StatusGatewayTimeout: provider.ClassTimeout,
		http.StatusBadGateway:     "",
	} {
		f, a, core, _ := serveAudit(t)
		a.status = status
		_, err := invoke(t, core, auditLogList.ID, "org", `{"owner":"orgs/octo-org"}`, false)
		if err == nil || strings.Contains(err.Error(), auditTextCanary) || (class != "" && classOf(err) != class) {
			t.Errorf("%d: err = %v, want an error without provider text", status, err)
		}
		if got := len(f.recorded()); got != 1 {
			t.Errorf("%d: sent %d requests, want one", status, got)
		}
	}
}

func TestAuditLogListBoundsTextsAndFreeFormMembers(t *testing.T) {
	_, a, core, _ := serveAudit(t)
	var data []string
	for i := 0; i < 120; i++ {
		data = append(data, fmt.Sprintf(`"k%03d":"v"`, i))
	}
	a.events = []string{`{"action":"x\u001b[31m","message":"` + strings.Repeat("m", 2000) + `","data":{` +
		strings.Join(data, ",") + `}}`, `{"action":"a"}`, `{"action":"b"}`}
	out, err := invoke(t, core, auditLogList.ID, "org", `{"owner":"orgs/octo-org","limit":2}`, false)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Truncated bool            `json:"truncated"`
		Events    []AuditLogEvent `json:"events"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Events) != 2 {
		t.Fatalf("events = %d, truncated = %v, want 2 and true", len(result.Events), result.Truncated)
	}
	first := result.Events[0]
	if strings.ContainsRune(*first.Action, 0x1b) || len([]rune(*first.Message)) > projectionTextLimit ||
		len(first.Data) != projectionListLimit || !first.DataTruncated {
		t.Errorf("first event = %+v, want cleaned, bounded texts and a capped data object", first)
	}
}

func TestAuditLogToolIsOneReadInTheOrganizationAdminGroup(t *testing.T) {
	reg := registry(t)
	var found []string
	for _, descriptor := range reg.Provider(Provider) {
		if strings.Split(descriptor.ID, ".")[1] != "audit-log" {
			continue
		}
		found = append(found, descriptor.ID)
		if descriptor.Group != "organization-admin" || descriptor.Risk.Effect != capability.EffectRead ||
			descriptor.RequiresToolAllowList {
			t.Errorf("%s = group %q, risk %+v, want a plain read of the organization-admin group", descriptor.ID,
				descriptor.Group, descriptor.Risk)
		}
		for _, word := range []string{"stream", "enterprise", "base_url", "path", "after", "before"} {
			if strings.Contains(string(descriptor.InputSchema), `"`+word+`"`) {
				t.Errorf("%s input offers %q", descriptor.ID, word)
			}
		}
	}
	if len(found) != 1 || found[0] != auditLogList.ID {
		t.Errorf("audit log tools = %v, want exactly the list", found)
	}
}

func TestAuditLogListWithholdsEventsOfExcludedRepositories(t *testing.T) {
	_, a, core, _ := serveAudit(t)
	a.events = []string{
		`{"action":"repo.create","repo":"octo-org/secret","message":"` + auditTextCanary + `"}`,
		`{"action":"repo.destroy","repository":"secret","name":"` + auditTextCanary + `"}`,
		`{"action":"repo.create","repo":"octo-org/open"}`,
		`{"action":"org.add_member","user":"octocat"}`,
	}
	out, err := invoke(t, core, auditLogList.ID, "audit-excluding", `{"owner":"orgs/octo-org"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), auditTextCanary) || strings.Contains(string(out), "secret") ||
		!strings.Contains(string(out), `"withheld":2`) || !strings.Contains(string(out), "octo-org/open") ||
		!strings.Contains(string(out), "org.add_member") {
		t.Errorf("result = %s, want the two events of the excluded repository withheld and counted", out)
	}
	// A connection without that exclusion shows every event and counts none.
	out, err = invoke(t, core, auditLogList.ID, "org", `{"owner":"orgs/octo-org"}`, false)
	if err != nil || strings.Contains(string(out), "withheld") || !strings.Contains(string(out), "octo-org/secret") {
		t.Errorf("unrestricted result = %s, %v, want all events and no count", out, err)
	}
}

// A rate limit is an error that names the wait, never the state unavailable.
func TestAuditLogListRateLimitIsAnError(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter){
		"403 with an empty budget": func(w http.ResponseWriter) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1")
			w.WriteHeader(http.StatusForbidden)
		},
		"429": func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		},
	} {
		f := &fakeGitHub{}
		f.failure = func(w http.ResponseWriter, req *http.Request) bool {
			if req.URL.Path != auditPath {
				return false
			}
			w.Header().Set("Content-Type", "application/json")
			answer(w)
			fmt.Fprintf(w, `{"message":%q}`, auditTextCanary)
			return true
		}
		base := serve(t, f)
		reads := 0
		red := &redact.Redactor{}
		core := application.New(registry(t), appsConfig(base), resolver(red, &reads), red)
		out, err := invoke(t, core, auditLogList.ID, "org", `{"owner":"orgs/octo-org"}`, false)
		if err == nil || classOf(err) != provider.ClassRateLimited || strings.Contains(err.Error(), auditTextCanary) ||
			strings.Contains(string(out), "unavailable") {
			t.Errorf("%s: result = %s, err = %v, want a rate-limited error without GitHub's text", name, out, err)
		}
	}
}
