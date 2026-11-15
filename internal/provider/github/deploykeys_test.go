package github

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

// keyBlob stands for the binary part of an OpenSSH public key; the key text must never reach a result, the
// fingerprint of it must.
var (
	keyBlob          = []byte("qatlas-test-key-blob-0123456789")
	keyText          = "ssh-ed25519 " + base64.StdEncoding.EncodeToString(keyBlob) + " deploy@example"
	keyTextCanary    = base64.StdEncoding.EncodeToString(keyBlob)
	keyFingerprint   = "SHA256:" + base64.RawStdEncoding.EncodeToString(sha256Of(keyBlob))
	inviteURLCanary  = "user/repository_invitations/777-accept-canary"
	inviteHTMLCanary = "invitations-html-canary"
)

func sha256Of(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// fakeGrants answers the deploy key and invitation routes of octo-org/example through the failure hook.
type fakeGrants struct {
	mu          sync.Mutex
	f           *fakeGitHub
	keys        map[int64]string // id -> key JSON, in id order below
	keyOrder    []int64
	invitations []string // invitation JSON, in order
	// keyURL replaces the REST url a key answers with.
	keyURL string
	// repoName is the full name every invitation reports.
	repoName string
	// keyDelete and inviteDelete replace the answer of a DELETE.
	keyDelete, inviteDelete func(http.ResponseWriter)
	// keyRead replaces the answer of a read of one key.
	keyRead func(http.ResponseWriter)
}

func keyJSON(id int64, title string, readOnly bool, url string) string {
	return fmt.Sprintf(`{"id":%d,"key":%q,"url":%q,"title":%q,"verified":true,"created_at":"2026-01-01T00:00:00Z",`+
		`"read_only":%t,"added_by":"octocat","last_used":null,"enabled":true}`, id, keyText, url, title, readOnly)
}

func invitationJSON(id int64, repo string) string {
	return fmt.Sprintf(`{"id":%d,"node_id":"RI_%d","repository":{"id":9,"node_id":"R_9","name":"example",`+
		`"full_name":%q,"private":true,"html_url":"https://github.com/%s"},`+
		`"invitee":{"login":"hubot","id":5,"type":"User","name":"Hu Bot","email":"hubot@example.com"},`+
		`"inviter":{"login":"octocat","id":1,"type":"User"},"permissions":"write","created_at":"2026-01-02T00:00:00Z",`+
		`"expired":false,"url":"https://api.github.com/%s","html_url":"https://github.com/%s/%s"}`,
		id, id, repo, repo, inviteURLCanary, repo, inviteHTMLCanary)
}

func serveGrants(t *testing.T) (*fakeGitHub, *fakeGrants, string) {
	t.Helper()
	g := &fakeGrants{keys: map[int64]string{}, repoName: "octo-org/example"}
	g.addKey(1, "ci", true)
	g.addKey(2, "release", false)
	g.invitations = []string{invitationJSON(10, "octo-org/example"), invitationJSON(11, "octo-org/example")}
	f := &fakeGitHub{}
	g.f = f
	f.failure = g.route
	return f, g, serve(t, f)
}

func (g *fakeGrants) addKey(id int64, title string, readOnly bool) {
	g.keys[id] = keyJSON(id, title, readOnly, "https://api.github.com/api/v3/repos/octo-org/example/keys/"+strconv.FormatInt(id, 10))
	g.keyOrder = append(g.keyOrder, id)
}

func (g *fakeGrants) route(w http.ResponseWriter, req *http.Request) bool {
	rest, ok := strings.CutPrefix(req.URL.Path, actionsPrefix)
	if !ok || rest != "keys" && !strings.HasPrefix(rest, "keys/") && rest != "invitations" && !strings.HasPrefix(rest, "invitations/") {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	notFound := func() {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}
	switch {
	case req.Method == http.MethodGet && rest == "keys":
		entries := []string{}
		for _, id := range g.keyOrder {
			entries = append(entries, g.keys[id])
		}
		fmt.Fprintf(w, `[%s]`, strings.Join(entries, ","))
	case strings.HasPrefix(rest, "keys/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(rest, "keys/"), 10, 64)
		entry, exists := g.keys[id]
		switch {
		case req.Method == http.MethodGet && g.keyRead != nil:
			g.keyRead(w)
		case req.Method == http.MethodGet && !exists:
			notFound()
		case req.Method == http.MethodGet:
			if g.keyURL != "" {
				entry = keyJSON(id, "ci", true, g.keyURL)
			}
			fmt.Fprint(w, entry)
		case req.Method == http.MethodDelete && g.keyDelete != nil:
			g.keyDelete(w)
		case req.Method == http.MethodDelete && !exists:
			notFound()
		case req.Method == http.MethodDelete:
			delete(g.keys, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			return false
		}
	case req.Method == http.MethodGet && rest == "invitations":
		perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		start := min((page-1)*perPage, len(g.invitations))
		end := min(start+perPage, len(g.invitations))
		if end < len(g.invitations) {
			w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
		}
		fmt.Fprintf(w, `[%s]`, strings.Join(g.invitations[start:end], ","))
	case req.Method == http.MethodDelete && strings.HasPrefix(rest, "invitations/"):
		if g.inviteDelete != nil {
			g.inviteDelete(w)
			return true
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

var grantToolIDs = []string{deployKeysList.ID, deployKeysGet.ID, deployKeysDelete.ID,
	repositoryInvitationsList.ID, repositoryInvitationsCancel.ID}

func grantsConfig(base string) *config.Config {
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete}
	cfg.Connections["repo"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget, Effects: all}
	cfg.Connections["listed"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: []string{deployKeysDelete.ID, repositoryInvitationsCancel.ID}}
	cfg.Connections["foreign"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Target: "repos/octo-org/other", Effects: all, Tools: []string{deployKeysDelete.ID, repositoryInvitationsCancel.ID}}
	cfg.Connections["ex"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: all,
		Tools: []string{deployKeysDelete.ID, repositoryInvitationsCancel.ID}, Exclude: []string{repoTarget}}
	return cfg
}

func grantsCore(t *testing.T) (*fakeGitHub, *fakeGrants, *application.Core, *int) {
	t.Helper()
	f, g, base := serveGrants(t)
	reads := 0
	red := &redact.Redactor{}
	return f, g, application.New(registry(t), grantsConfig(base), resolver(red, &reads), red), &reads
}

func TestDeployKeyToolsReadWithoutTheKeyTextAndRevokeOnce(t *testing.T) {
	f, g, core, _ := grantsCore(t)

	listed, err := invoke(t, core, deployKeysList.ID, "repo", `{}`, false)
	if err != nil || !strings.Contains(string(listed), `"title":"ci"`) || !strings.Contains(string(listed), `"read_only":true`) ||
		!strings.Contains(string(listed), `"read_only":false`) || !strings.Contains(string(listed), keyFingerprint) ||
		!strings.Contains(string(listed), `"repository":"octo-org/example"`) {
		t.Fatalf("list = %s, %v", listed, err)
	}
	got, err := invoke(t, core, deployKeysGet.ID, "repo", `{"id":2}`, false)
	if err != nil || !strings.Contains(string(got), `"title":"release"`) || !strings.Contains(string(got), `"added_by":"octocat"`) ||
		!strings.Contains(string(got), keyFingerprint) || !strings.Contains(string(got), `"verified":true`) ||
		!strings.Contains(string(got), `"enabled":true`) || !strings.Contains(string(got), `"links"`) {
		t.Fatalf("get = %s, %v", got, err)
	}
	for _, out := range []string{string(listed), string(got)} {
		if strings.Contains(out, keyTextCanary) || strings.Contains(out, "ssh-ed25519") || strings.Contains(out, `"key"`) {
			t.Errorf("a read tool shows the key text: %s", out)
		}
	}

	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, core, deployKeysDelete.ID, "repo", `{"id":1}`, true); !errors.As(err, &unsupported) {
		t.Errorf("delete without a tools list = %v, want unsupported-capability", err)
	}
	before := len(f.recorded())
	unconfirmed := &application.ConfirmationRequiredError{}
	if _, err := invoke(t, core, deployKeysDelete.ID, "listed", `{"id":1}`, false); !errors.As(err, &unconfirmed) ||
		len(f.recorded()) != before {
		t.Errorf("delete without confirm = %v, want confirmation-required without a request", err)
	}
	deleted, err := invoke(t, core, deployKeysDelete.ID, "listed", `{"id":1}`, true)
	if err != nil || !strings.Contains(string(deleted), `"deleted":true`) || !strings.Contains(string(deleted), `"title":"ci"`) ||
		!strings.Contains(string(deleted), keyFingerprint) || strings.Contains(string(deleted), keyTextCanary) {
		t.Fatalf("delete = %s, %v", deleted, err)
	}
	if _, exists := g.keys[1]; exists || deletes(f) != 1 {
		t.Errorf("key kept or deletes = %d", deletes(f))
	}
	// The read that proves the key comes first.
	requests := f.recorded()
	last, prior := requests[len(requests)-1], requests[len(requests)-2]
	if last.method != http.MethodDelete || prior.method != http.MethodGet || prior.path != last.path {
		t.Errorf("last requests = %+v then %+v, want a read of the key then its delete", prior, last)
	}
}

func TestDeployKeyIDOfAnotherRepositoryIsNeverDeleted(t *testing.T) {
	f, g, core, _ := grantsCore(t)
	g.keyURL = "https://api.github.com/api/v3/repos/octo-org/other/keys/1"
	if _, err := invoke(t, core, deployKeysGet.ID, "repo", `{"id":1}`, false); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("get of a foreign key = %v, want an invalid response", err)
	}
	if _, err := invoke(t, core, deployKeysDelete.ID, "listed", `{"id":1}`, true); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("delete of a foreign key = %v, want an invalid response", err)
	}
	if deletes(f) != 0 {
		t.Error("a foreign key was deleted")
	}
	g.keyURL = ""
	if _, err := invoke(t, core, deployKeysDelete.ID, "listed", `{"id":99}`, true); classOf(err) != provider.ClassNotFound ||
		deletes(f) != 0 {
		t.Errorf("unknown key = %v, deletes %d, want not-found without a delete", err, deletes(f))
	}
}

func TestGrantToolsMapRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		class  provider.Class
	}{
		{"forbidden", http.StatusForbidden, provider.ClassPermission},
		{"missing", http.StatusNotFound, provider.ClassNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, g, core, _ := grantsCore(t)
			refuse := func(w http.ResponseWriter) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, `{"message":"Resource not accessible SECRETTEXT"}`)
			}
			g.keyRead = refuse
			g.invitations = nil
			for _, call := range []struct{ id, connection, arguments string }{
				{deployKeysGet.ID, "repo", `{"id":1}`}, {deployKeysDelete.ID, "listed", `{"id":1}`},
			} {
				_, err := invoke(t, core, call.id, call.connection, call.arguments, true)
				if classOf(err) != tt.class || err == nil || strings.Contains(err.Error(), "SECRETTEXT") {
					t.Errorf("%s = %v, want class %s without provider text", call.id, err, tt.class)
				}
				if tt.class == provider.ClassPermission && !strings.Contains(err.Error(), "Administration") {
					t.Errorf("%s message = %v, want the needed rights", call.id, err)
				}
			}
		})
	}
}

func TestGrantDeletesNeverRepeatAnUnclearOutcome(t *testing.T) {
	answers := map[string]func(http.ResponseWriter){
		"server error":    func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) },
		"gateway timeout": func(w http.ResponseWriter) { w.WriteHeader(http.StatusGatewayTimeout) },
		"dropped connection": func(w http.ResponseWriter) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		},
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			f, g, core, _ := grantsCore(t)
			g.keyDelete, g.inviteDelete = answer, answer
			for id, arguments := range map[string]string{deployKeysDelete.ID: `{"id":1}`,
				repositoryInvitationsCancel.ID: `{"id":10}`} {
				before := deletes(f)
				_, err := invoke(t, core, id, "listed", arguments, true)
				if err == nil || !strings.Contains(err.Error(), "may have been applied") {
					t.Errorf("%s = %v, want an unclear outcome", id, err)
				}
				if deletes(f)-before != 1 {
					t.Errorf("%s sent %d deletes, want exactly one", id, deletes(f)-before)
				}
			}
		})
	}
}

func TestRepositoryInvitationsListWithholdsAcceptanceAddresses(t *testing.T) {
	_, _, core, _ := grantsCore(t)
	out, err := invoke(t, core, repositoryInvitationsList.ID, "repo", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{`"id":10`, `"permissions":"write"`, `"expired":false`, `"login":"hubot"`,
		`"email":"hubot@example.com"`, `"login":"octocat"`, `"full_name":"octo-org/example"`,
		`"created_at":"2026-01-02T00:00:00Z"`, `"node_id":"RI_10"`} {
		if !strings.Contains(text, want) {
			t.Errorf("list lacks %s: %s", want, text)
		}
	}
	for _, unwanted := range []string{inviteURLCanary, inviteHTMLCanary, "repository_invitations"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("list shows %q: %s", unwanted, text)
		}
	}
}

func TestRepositoryInvitationsCancelBindsTheIDThroughTheList(t *testing.T) {
	f, g, core, _ := grantsCore(t)
	out, err := invoke(t, core, repositoryInvitationsCancel.ID, "listed", `{"id":11}`, true)
	if err != nil || !strings.Contains(string(out), `"deleted":true`) || !strings.Contains(string(out), `"invitee":"hubot"`) {
		t.Fatalf("cancel = %s, %v", out, err)
	}
	requests := f.recorded()
	if last := requests[len(requests)-1]; last.method != http.MethodDelete || last.path != actionsPrefix+"invitations/11" ||
		deletes(f) != 1 {
		t.Errorf("last request = %+v, deletes %d", last, deletes(f))
	}
	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, core, repositoryInvitationsCancel.ID, "repo", `{"id":11}`, true); !errors.As(err, &unsupported) {
		t.Errorf("cancel without a tools list = %v, want unsupported-capability", err)
	}

	before := deletes(f)
	if _, err := invoke(t, core, repositoryInvitationsCancel.ID, "listed", `{"id":999}`, true); classOf(err) != provider.ClassNotFound {
		t.Errorf("unknown invitation = %v, want not-found", err)
	}
	g.invitations = []string{invitationJSON(12, "octo-org/other")}
	if _, err := invoke(t, core, repositoryInvitationsCancel.ID, "listed", `{"id":12}`, true); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("foreign invitation = %v, want an invalid response", err)
	}
	if deletes(f) != before {
		t.Error("an unbound invitation was cancelled")
	}
}

func TestRepositoryInvitationsCancelFindsAnInvitationOnALaterPage(t *testing.T) {
	f, g, core, _ := grantsCore(t)
	g.invitations = nil
	for id := int64(1); id <= 150; id++ {
		g.invitations = append(g.invitations, invitationJSON(id, "octo-org/example"))
	}
	if _, err := invoke(t, core, repositoryInvitationsCancel.ID, "listed", `{"id":140}`, true); err != nil {
		t.Fatalf("cancel on page 2 = %v", err)
	}
	if deletes(f) != 1 {
		t.Errorf("deletes = %d", deletes(f))
	}
}

func TestGrantToolsRefuseForeignAndExcludedTargetsAndBadArgumentsBeforeIO(t *testing.T) {
	f, _, core, reads := grantsCore(t)
	arguments := map[string]string{deployKeysList.ID: `{}`, deployKeysGet.ID: `{"id":1}`, deployKeysDelete.ID: `{"id":1}`,
		repositoryInvitationsList.ID: `{}`, repositoryInvitationsCancel.ID: `{"id":10}`}
	for _, id := range grantToolIDs {
		withRepository := `{"repository":"octo-org/example"}`
		if arguments[id] != `{}` {
			withRepository = `{"repository":"octo-org/example",` + strings.TrimPrefix(arguments[id], "{")
		}
		for _, connection := range []string{"foreign", "ex"} {
			*reads = 0
			before := len(f.recorded())
			if _, err := invoke(t, core, id, connection, withRepository, true); err == nil {
				t.Errorf("%s on %s succeeded, want a refusal", id, connection)
			}
			if *reads != 0 || len(f.recorded()) != before {
				t.Errorf("%s on %s reached the credential or GitHub", id, connection)
			}
		}
	}
	for _, tt := range []struct{ id, connection, arguments string }{
		{deployKeysGet.ID, "repo", `{}`}, {deployKeysGet.ID, "repo", `{"id":0}`},
		{deployKeysDelete.ID, "listed", `{"id":-1}`}, {repositoryInvitationsCancel.ID, "listed", `{}`},
		{deployKeysList.ID, "repo", `{"limit":1001}`}, {repositoryInvitationsList.ID, "repo", `{"cursor":"x"}`},
	} {
		*reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true); !isInvalidRequest(err) {
			t.Errorf("%s(%s) = %v, want an invalid request", tt.id, tt.arguments, err)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s(%s) reached the credential or GitHub", tt.id, tt.arguments)
		}
	}
}

func TestGrantListsPageByLinkHeader(t *testing.T) {
	_, g, core, _ := grantsCore(t)
	g.invitations = nil
	for id := int64(1); id <= 35; id++ {
		g.invitations = append(g.invitations, invitationJSON(id, "octo-org/example"))
	}
	first, err := invoke(t, core, repositoryInvitationsList.ID, "repo", `{"limit":30}`, false)
	if err != nil {
		t.Fatal(err)
	}
	var page RepositoryInvitationList
	if err := json.Unmarshal(first, &page); err != nil || len(page.Invitations) != 30 || !page.HasMore {
		t.Fatalf("first batch = %s, %v", first, err)
	}
	second, err := invoke(t, core, repositoryInvitationsList.ID, "repo", `{"cursor":"`+page.NextCursor+`"}`, false)
	if err != nil || json.Unmarshal(second, &page) != nil || len(page.Invitations) != 5 || page.HasMore {
		t.Fatalf("second batch = %s, %v", second, err)
	}
}

func TestDeployKeyFingerprintMatchesTheSSHFormAndRefusesJunk(t *testing.T) {
	text := keyText
	if got := publicKeyFingerprint(&text); got != keyFingerprint {
		t.Errorf("fingerprint = %q, want %q", got, keyFingerprint)
	}
	for _, junk := range []string{"", "ssh-ed25519", "ssh-ed25519 !!!notbase64"} {
		if got := publicKeyFingerprint(&junk); got != "" {
			t.Errorf("fingerprint of %q = %q, want none", junk, got)
		}
	}
	if publicKeyFingerprint(nil) != "" {
		t.Error("fingerprint of a missing key")
	}
}

func TestGrantsSubjectNamesOnlyCheckedIDs(t *testing.T) {
	for path, want := range map[string]string{
		"keys": "the deploy keys of this repository", "keys/7": "deploy key 7", "keys/x": "the deploy keys of this repository",
		"invitations": "the invitations of this repository", "invitations/9": "invitation 9", "labels": "",
	} {
		if got := grantsSubject(path); got != want {
			t.Errorf("grantsSubject(%q) = %q, want %q", path, got, want)
		}
	}
}
