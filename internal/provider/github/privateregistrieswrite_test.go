package github

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
	"golang.org/x/crypto/nacl/box"
)

// registryWriteFake answers the registry write routes. A created registry is named NPM_REGISTRY_1 and its
// answer reflects the sealed value, the key id, and the address, which must never reach a result.
type registryWriteFake struct {
	mu                    sync.Mutex
	publicKey, privateKey *[32]byte
	keyAnswer             string
	keyStatus, changeCode int
	// createStatus and updateStatus replace the success status of the change.
	createStatus, updateStatus int
	createName                 string
	changes                    []map[string]any
	blockChange                *changeStall
	f                          *fakeGitHub
}

func newRegistryWriteFake() *registryWriteFake {
	p := &registryWriteFake{createName: "NPM_REGISTRY_1"}
	p.publicKey, p.privateKey, _ = box.GenerateKey(rand.Reader)
	return p
}

func (p *registryWriteFake) route(w http.ResponseWriter, r *http.Request) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if rest, ok := strings.CutPrefix(r.URL.Path, "/api/v3/repos/"); ok {
		switch strings.ToLower(rest) {
		case "octo-org/example":
			fmt.Fprint(w, `{"id":1296269,"full_name":"octo-org/example"}`)
		case "octo-org/tools":
			fmt.Fprint(w, `{"id":1296270,"full_name":"octo-org/tools"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
		return true
	}
	refuse := func(status int) bool {
		w.WriteHeader(status)
		fmt.Fprint(w, `{"message":"refused `+providerSecret+` `+registryCanary+`"}`)
		return true
	}
	switch {
	case r.URL.Path == privateRegistryRoute+"/public-key" && r.Method == http.MethodGet:
		if p.keyStatus != 0 {
			return refuse(p.keyStatus)
		}
		if p.keyAnswer != "" {
			fmt.Fprint(w, p.keyAnswer)
			return true
		}
		fmt.Fprintf(w, `{"key_id":"012345678","key":%q}`, base64.StdEncoding.EncodeToString(p.publicKey[:]))
	case r.Method == http.MethodPost && r.URL.Path == privateRegistryRoute,
		r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, privateRegistryRoute+"/"):
		if p.blockChange != nil {
			return p.blockChange.route(w, r)
		}
		if p.changeCode != 0 {
			return refuse(p.changeCode)
		}
		// The shared fake has read the body already and recorded it.
		requests := p.f.recorded()
		body := requests[len(requests)-1].body
		p.changes = append(p.changes, body)
		if r.Method == http.MethodPatch {
			status := http.StatusNoContent
			if p.updateStatus != 0 {
				status = p.updateStatus
			}
			w.WriteHeader(status)
			return true
		}
		status := http.StatusCreated
		if p.createStatus != 0 {
			status = p.createStatus
		}
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"name":%q,"encrypted_value":%q,"key_id":"012345678","value":%q,"url":%q}`, p.createName,
			body["encrypted_value"], registryCanary, body["url"])
	default:
		return false
	}
	return true
}

func (p *registryWriteFake) open(t *testing.T, body map[string]any) string {
	t.Helper()
	sealed, err := base64.StdEncoding.DecodeString(body["encrypted_value"].(string))
	if err != nil {
		t.Fatalf("encrypted_value is not Base64: %v", err)
	}
	plain, ok := box.OpenAnonymous(nil, sealed, p.publicKey, p.privateKey)
	if !ok {
		t.Fatal("the sealed box does not open with the key of the organization")
	}
	return string(plain)
}

const registryHost = "registry-hosts/registry.example.com"

func registryWriteCore(t *testing.T, fake *registryWriteFake, dir string, reads *int) (*fakeGitHub, *application.Core) {
	t.Helper()
	f := &fakeGitHub{failure: fake.route}
	fake.f = f
	cfg := discoveryConfig(serve(t, f))
	add := func(name string, targets, excludes, tools []string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: config.Effects(),
			Targets: targets, Exclude: excludes, Tools: tools, Files: config.Files{Read: []string{dir}}}
	}
	tools := privateRegistryWriteToolIDs
	add("w-host", []string{"orgs/octo-org", registryHost}, nil, tools)
	add("w-port", []string{"orgs/octo-org", "registry-hosts/Registry.Example.com:8443"}, nil, tools)
	add("w-nohost", []string{"orgs/octo-org"}, nil, tools)
	add("w-open", nil, nil, tools)
	add("w-sel", []string{"orgs/octo-org", "repos/octo-org/*", registryHost}, []string{"repos/octo-org/secret"}, tools)
	add("w-one", []string{"orgs/octo-org", "repos/octo-org/example", registryHost}, nil, tools)
	add("w-other", []string{"orgs/other", registryHost}, nil, tools)
	add("w-hostonly", []string{registryHost}, nil, tools)
	add("w-ex-owner", []string{registryHost}, []string{"orgs/octo-org"}, tools)
	add("w-user", []string{"users/octocat", registryHost}, nil, tools)
	add("listed-none", []string{"orgs/octo-org", registryHost}, nil, nil)
	red := &redact.Redactor{}
	return f, application.New(registry(t), cfg, resolver(red, reads), red)
}

func registryFile(t *testing.T) (dir, path string) {
	return writeSecretFile(t, secretFileCanary)
}

func createArguments(path string, extra string) string {
	return `{"owner":"orgs/octo-org","registry_type":"npm_registry","url":"https://registry.example.com/npm",` +
		`"visibility":"all","local_path":` + strconvQuote(path) + extra + `}`
}

func assertNoRegistryLeak(t *testing.T, name, text string) {
	t.Helper()
	for _, leak := range []string{secretFileCanary, "file-value-canary", registryCanary, providerSecret, "encrypted_value",
		"key_id", "012345678", "registry.example.com"} {
		if strings.Contains(text, leak) {
			t.Errorf("%s leaks %q: %s", name, leak, text)
		}
	}
}

// A create or update seals the file unchanged with the key of the organization, reads the key right before the
// one change, answers only what was sent, and never shows the value, the key, or a reflected member.
func TestPrivateRegistryWriteSealsTheFileAndSendsOnce(t *testing.T) {
	fake := newRegistryWriteFake()
	dir, path := registryFile(t)
	f, core := registryWriteCore(t, fake, dir, nil)
	for _, tt := range []struct {
		name, id, connection, arguments, method, route, state string
		before                                                []string
		sealed                                                bool
		keys                                                  []string
	}{
		{"create for all, upper-case host", privateRegistriesCreate.ID, "w-host",
			`{"owner":"orgs/octo-org","registry_type":"npm_registry","url":"https://REGISTRY.Example.COM/npm","visibility":"all",` +
				`"local_path":` + strconvQuote(path) + `}`,
			http.MethodPost, privateRegistryRoute, "created", nil, true,
			[]string{"registry_type", "url", "visibility", "encrypted_value", "key_id"}},
		{"create with every field", privateRegistriesCreate.ID, "w-sel",
			`{"owner":"orgs/octo-org","registry_type":"maven_repository","url":"https://registry.example.com/maven",` +
				`"auth_type":"username_password","username":"deploy-bot","replaces_base":true,"visibility":"selected",` +
				`"selected_repositories":["octo-org/example","Octo-Org/tools"],"local_path":` + strconvQuote(path) + `}`,
			http.MethodPost, privateRegistryRoute, "created",
			[]string{"/api/v3/repos/octo-org/example", "/api/v3/repos/Octo-Org/tools"}, true,
			[]string{"registry_type", "url", "visibility", "encrypted_value", "key_id", "auth_type", "username",
				"replaces_base", "selected_repository_ids"}},
		{"create for private on an unrestricted organization", privateRegistriesCreate.ID, "w-host",
			createArguments(path, ""), http.MethodPost, privateRegistryRoute,
			"created", nil, true, []string{"registry_type", "url", "visibility", "encrypted_value", "key_id"}},
		{"update with address and value", privateRegistriesUpdate.ID, "w-host",
			`{"owner":"orgs/octo-org","secret_name":"NPM_REGISTRY_1","url":"https://registry.example.com/other",` +
				`"local_path":` + strconvQuote(path) + `}`,
			http.MethodPatch, privateRegistryRoute + "/NPM_REGISTRY_1", "updated", nil, true,
			[]string{"url", "encrypted_value", "key_id"}},
		{"update with the value only needs no host approval", privateRegistriesUpdate.ID, "w-nohost",
			`{"owner":"orgs/octo-org","secret_name":"NPM_REGISTRY_1","local_path":` + strconvQuote(path) + `}`,
			http.MethodPatch, privateRegistryRoute + "/NPM_REGISTRY_1", "updated", nil, true,
			[]string{"encrypted_value", "key_id"}},
		{"update of the settings only sends no key request", privateRegistriesUpdate.ID, "w-sel",
			`{"owner":"orgs/octo-org","secret_name":"NPM_REGISTRY_1","replaces_base":false,"username":"deploy-bot",` +
				`"registry_type":"npm_registry","visibility":"selected","selected_repositories":["octo-org/example"]}`,
			http.MethodPatch, privateRegistryRoute + "/NPM_REGISTRY_1", "updated",
			[]string{"/api/v3/repos/octo-org/example"}, false,
			[]string{"replaces_base", "username", "registry_type", "visibility", "selected_repository_ids"}},
		{"update with the address of a host with a port", privateRegistriesUpdate.ID, "w-port",
			`{"owner":"orgs/octo-org","secret_name":"NPM_REGISTRY_1","url":"https://registry.example.com:8443/x"}`,
			http.MethodPatch, privateRegistryRoute + "/NPM_REGISTRY_1", "updated", nil, false, []string{"url"}},
	} {
		// The private case above differs from the first only in its visibility.
		if strings.Contains(tt.name, "private") {
			tt.arguments = strings.Replace(tt.arguments, `"visibility":"all"`, `"visibility":"private"`, 1)
		}
		beforeCount, changes := len(f.recorded()), len(fake.changes)
		got, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		if err != nil || !strings.Contains(string(got), `"state":"`+tt.state+`"`) ||
			!strings.Contains(string(got), `"owner":"orgs/octo-org"`) {
			t.Fatalf("%s = %s, %v", tt.name, got, err)
		}
		assertNoRegistryLeak(t, tt.name, string(got))
		wantName := "NPM_REGISTRY_1"
		if !strings.Contains(string(got), `"name":"`+wantName+`"`) {
			t.Errorf("%s = %s, want the name %s", tt.name, got, wantName)
		}
		requests := f.recorded()[beforeCount:]
		want := len(tt.before) + 1
		if tt.sealed {
			want++
		}
		if len(requests) != want {
			t.Fatalf("%s sent %d requests, want %d: %+v", tt.name, len(requests), want, requests)
		}
		for i, path := range tt.before {
			if requests[i].method != http.MethodGet || requests[i].path != path {
				t.Errorf("%s request %d = %s %s, want GET %s", tt.name, i, requests[i].method, requests[i].path, path)
			}
		}
		change := requests[len(requests)-1]
		if change.method != tt.method || change.path != tt.route {
			t.Errorf("%s change = %s %s, want %s %s", tt.name, change.method, change.path, tt.method, tt.route)
		}
		if tt.sealed {
			if key := requests[len(requests)-2]; key.method != http.MethodGet || key.path != privateRegistryRoute+"/public-key" {
				t.Errorf("%s key request = %s %s", tt.name, key.method, key.path)
			}
		}
		if len(fake.changes) != changes+1 {
			t.Fatalf("%s: the server accepted %d changes", tt.name, len(fake.changes)-changes)
		}
		body := fake.changes[len(fake.changes)-1]
		if len(body) != len(tt.keys) {
			t.Errorf("%s sent %v, want the members %v", tt.name, body, tt.keys)
		}
		for _, key := range tt.keys {
			if _, ok := body[key]; !ok {
				t.Errorf("%s body lacks %s", tt.name, key)
			}
		}
		if tt.sealed {
			if plain := fake.open(t, body); plain != secretFileCanary {
				t.Errorf("%s sealed %q, want the file content unchanged", tt.name, plain)
			}
			if body["key_id"] != "012345678" {
				t.Errorf("%s key_id = %v", tt.name, body["key_id"])
			}
		}
		if data, _ := json.Marshal(body); strings.Contains(string(data), "file-value-canary") {
			t.Errorf("%s sent the value in the clear", tt.name)
		}
		if ids, ok := body["selected_repository_ids"]; ok {
			data, _ := json.Marshal(ids)
			if tt.name == "create with every field" && string(data) != "[1296269,1296270]" {
				t.Errorf("%s selected_repository_ids = %s", tt.name, data)
			}
		}
	}
	// The reflected members never reach the answer of a create, even named in a field of the answer's own.
	got := mustInvokeWith(t, core, privateRegistriesCreate.ID, "w-host", createArguments(path, ""))
	assertNoRegistryLeak(t, "reflecting create", string(got))
}

func mustInvokeWith(t *testing.T, core *application.Core, id, connection, arguments string) json.RawMessage {
	t.Helper()
	got, err := invoke(t, core, id, connection, arguments, true)
	if err != nil {
		t.Fatalf("%s = %v", id, err)
	}
	return got
}

// Every refusal before the credential ends without a secret read, a file read, or a request: the
// organization, the address, and the selected repositories come first, then the file.
func TestPrivateRegistryWriteRefusesBeforeFileAndCredential(t *testing.T) {
	fake := newRegistryWriteFake()
	dir, _ := registryFile(t)
	reads := 0
	f, core := registryWriteCore(t, fake, dir, &reads)
	// The file does not exist: a refusal that came from the file would say so instead of naming the rule.
	file := `"local_path":"/nonexistent-qatlas-dir/value"`
	create := func(owner, url, extra string) string {
		return `{"owner":"` + owner + `","registry_type":"npm_registry","url":"` + url + `","visibility":"all",` + file + extra + `}`
	}
	update := func(extra string) string {
		return `{"owner":"orgs/octo-org","secret_name":"NPM_REGISTRY_1",` + file + extra + `}`
	}
	good := "https://registry.example.com/npm"
	type refusal struct{ name, id, connection, arguments, fragment string }
	cases := []refusal{
		{"host not approved", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", "https://other.example.com/npm", ""), "approves"},
		{"only a subdomain", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", "https://sub.registry.example.com/npm", ""), "approves"},
		{"a superdomain", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", "https://example.com/npm", ""), "approves"},
		{"another port", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", "https://registry.example.com:8443/npm", ""), "approves"},
		{"no port for an approved port", privateRegistriesCreate.ID, "w-port", create("orgs/octo-org", good, ""), "approves"},
		{"http", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", "http://registry.example.com/npm", ""), "approves"},
		{"no host target at all", privateRegistriesCreate.ID, "w-nohost", create("orgs/octo-org", good, ""), "approves"},
		{"no targets at all", privateRegistriesCreate.ID, "w-open", create("orgs/octo-org", good, ""), "approves"},
		{"a pattern host", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", "https://*.example.com/npm", ""), "approves"},
		{"an address with a space", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", "https://registry.example.com/a b", ""), "approves"},
		{"an update with another host", privateRegistriesUpdate.ID, "w-host", update(`,"url":"https://other.example.com/x"`), "approves"},
		{"an update with a subdomain", privateRegistriesUpdate.ID, "w-host", update(`,"url":"https://sub.registry.example.com/x"`), "approves"},
		{"an update with another port", privateRegistriesUpdate.ID, "w-host", update(`,"url":"https://registry.example.com:1/x"`), "approves"},
		{"an update with http", privateRegistriesUpdate.ID, "w-host", update(`,"url":"http://registry.example.com/x"`), "approves"},
		{"an update with an address on a connection without hosts", privateRegistriesUpdate.ID, "w-nohost", update(`,"url":"` + good + `"`), "approves"},
		{"organization outside the targets", privateRegistriesCreate.ID, "w-other", create("orgs/octo-org", good, ""), ""},
		{"excluded organization", privateRegistriesCreate.ID, "w-ex-owner", create("orgs/octo-org", good, ""), ""},
		{"a connection with hosts only", privateRegistriesCreate.ID, "w-hostonly", create("orgs/octo-org", good, ""), ""},
		{"a user as owner", privateRegistriesCreate.ID, "w-user", create("users/octocat", good, ""), "organization"},
		{"a host as owner", privateRegistriesCreate.ID, "w-host", create("registry-hosts/registry.example.com", good, ""), ""},
		{"an update outside the targets", privateRegistriesUpdate.ID, "w-other", update(""), ""},
		{"all with a repository target", privateRegistriesCreate.ID, "w-one", create("orgs/octo-org", good, ""), "outside the targets"},
		{"private with an exclusion", privateRegistriesCreate.ID, "w-sel",
			strings.Replace(create("orgs/octo-org", good, ""), `"visibility":"all"`, `"visibility":"private"`, 1), "outside the targets"},
		{"an update to all with an exclusion", privateRegistriesUpdate.ID, "w-sel", update(`,"visibility":"all"`), "outside the targets"},
		{"an excluded selected repository", privateRegistriesCreate.ID, "w-sel",
			strings.Replace(create("orgs/octo-org", good, `,"selected_repositories":["octo-org/secret"]`), `"all"`, `"selected"`, 1), "not available"},
		{"a selected repository outside the targets", privateRegistriesCreate.ID, "w-one",
			strings.Replace(create("orgs/octo-org", good, `,"selected_repositories":["octo-org/tools"]`), `"all"`, `"selected"`, 1), "outside the targets"},
		{"a selected repository of another organization", privateRegistriesCreate.ID, "w-host",
			strings.Replace(create("orgs/octo-org", good, `,"selected_repositories":["other/x"]`), `"all"`, `"selected"`, 1), "organization only"},
		{"an update selecting outside the targets", privateRegistriesUpdate.ID, "w-one",
			update(`,"visibility":"selected","selected_repositories":["octo-org/tools"]`), "outside the targets"},
		{"selected without a list", privateRegistriesCreate.ID, "w-host",
			strings.Replace(create("orgs/octo-org", good, ""), `"all"`, `"selected"`, 1), "selected_repositories"},
		{"a list without selected", privateRegistriesCreate.ID, "w-host",
			create("orgs/octo-org", good, `,"selected_repositories":["octo-org/example"]`), "selected_repositories"},
		{"an update with a list and no visibility", privateRegistriesUpdate.ID, "w-host",
			update(`,"selected_repositories":["octo-org/example"]`), "selected_repositories"},
		{"selected repository ids as an argument", privateRegistriesCreate.ID, "w-host",
			create("orgs/octo-org", good, `,"selected_repository_ids":[1]`), ""},
		{"a value argument", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", good, `,"value":"x"`), ""},
		{"an encrypted value argument", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", good, `,"encrypted_value":"eA=="`), ""},
		{"a key id argument", privateRegistriesUpdate.ID, "w-host", update(`,"key_id":"1"`), ""},
		{"an oidc authentication", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", good, `,"auth_type":"oidc_aws"`), ""},
		{"anonymous authentication", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", good, `,"auth_type":"anonymous"`), ""},
		{"an oidc member", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", good, `,"api_host":"x.example.com"`), ""},
		{"auth_type on an update", privateRegistriesUpdate.ID, "w-host", update(`,"auth_type":"token"`), ""},
		{"an unknown registry type", privateRegistriesCreate.ID, "w-host",
			strings.Replace(create("orgs/octo-org", good, ""), "npm_registry", "other_registry", 1), ""},
		{"no registry type", privateRegistriesCreate.ID, "w-host", `{"owner":"orgs/octo-org","url":"` + good + `","visibility":"all",` + file + `}`, ""},
		{"no address", privateRegistriesCreate.ID, "w-host", `{"owner":"orgs/octo-org","registry_type":"npm_registry","visibility":"all",` + file + `}`, ""},
		{"no visibility", privateRegistriesCreate.ID, "w-host", `{"owner":"orgs/octo-org","registry_type":"npm_registry","url":"` + good + `",` + file + `}`, ""},
		{"no file", privateRegistriesCreate.ID, "w-host", `{"owner":"orgs/octo-org","registry_type":"npm_registry","url":"` + good + `","visibility":"all"}`, ""},
		{"an empty file path", privateRegistriesCreate.ID, "w-host", `{"owner":"orgs/octo-org","registry_type":"npm_registry","url":"` + good + `","visibility":"all","local_path":""}`, ""},
		{"an empty username", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", good, `,"username":""`), ""},
		{"a username with a control character", privateRegistriesCreate.ID, "w-host", create("orgs/octo-org", good, `,"username":"a\nb"`), "username"},
		{"an update without a change", privateRegistriesUpdate.ID, "w-host", `{"owner":"orgs/octo-org","secret_name":"NPM_REGISTRY_1"}`, "at least one"},
		{"an update without a name", privateRegistriesUpdate.ID, "w-host", `{"owner":"orgs/octo-org","replaces_base":true}`, ""},
		{"an update with a path as name", privateRegistriesUpdate.ID, "w-host", `{"owner":"orgs/octo-org","secret_name":"public-key","replaces_base":true}`, ""},
		{"an update with a slash in the name", privateRegistriesUpdate.ID, "w-host", `{"owner":"orgs/octo-org","secret_name":"A/B","replaces_base":true}`, ""},
	}
	for _, tt := range cases {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) && !isApplicationInvalid(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if err != nil {
			if tt.fragment != "" && !strings.Contains(err.Error(), tt.fragment) {
				t.Errorf("%s = %v, want a message naming %q", tt.name, err, tt.fragment)
			}
			for _, leak := range []string{"nonexistent-qatlas-dir", "other.example.com", "registry.example.com"} {
				if strings.Contains(strings.Replace(err.Error(), "registry-hosts/HOST", "", 1), leak) {
					t.Errorf("%s quotes %q: %v", tt.name, leak, err)
				}
			}
		}
		if reads != 0 || len(f.recorded()) != before || len(fake.changes) != 0 {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// The file is checked after the arguments and before the credential: outside the released directories, a
// directory, a missing, an empty, and an oversized file are refused without a secret read or a request.
func TestPrivateRegistryWriteRefusesUnusableFilesBeforeTheCredential(t *testing.T) {
	fake := newRegistryWriteFake()
	dir, _ := registryFile(t)
	_, outside := writeSecretFile(t, secretFileCanary)
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", maxSecretValueBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	f, core := registryWriteCore(t, fake, dir, &reads)
	for _, tt := range []struct{ name, path string }{
		{"outside the released directory", outside},
		{"a directory", dir},
		{"a missing file", filepath.Join(dir, "missing")},
		{"a relative path", "value"},
		{"a dot-dot path", dir + "/../value"},
		{"an empty file", empty},
		{"a file over 48 KB", big},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, privateRegistriesCreate.ID, "w-host", createArguments(tt.path, ""), true)
		if err == nil || !(isInvalidRequest(err) || isApplicationInvalid(err)) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
			continue
		}
		for _, leak := range []string{secretFileCanary, "file-value-canary", dir, outside} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s names %q: %v", tt.name, leak, err)
			}
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
}

// A key that is not Base64 of exactly 32 bytes, or a key without an id, ends the call before the change.
func TestPrivateRegistryWriteRefusesAnUnusableKey(t *testing.T) {
	fake := newRegistryWriteFake()
	dir, path := registryFile(t)
	f, core := registryWriteCore(t, fake, dir, nil)
	good := base64.StdEncoding.EncodeToString(fake.publicKey[:])
	for name, answer := range map[string]string{
		"not Base64": `{"key_id":"1","key":"!!!not base64!!!"}`,
		"31 bytes":   `{"key_id":"1","key":"` + base64.StdEncoding.EncodeToString(make([]byte, 31)) + `"}`,
		"no key":     `{"key_id":"1"}`,
		"no key id":  `{"key":"` + good + `"}`,
		"empty id":   `{"key_id":"","key":"` + good + `"}`,
	} {
		fake.keyAnswer = answer
		for _, id := range privateRegistryWriteToolIDs {
			arguments := createArguments(path, "")
			if id == privateRegistriesUpdate.ID {
				arguments = `{"owner":"orgs/octo-org","secret_name":"A","local_path":` + strconvQuote(path) + `}`
			}
			before := len(f.recorded())
			_, err := invoke(t, core, id, "w-host", arguments, true)
			if classOf(err) != provider.ClassInvalidResponse {
				t.Errorf("%s on %s = %v, want an invalid response", name, id, err)
			}
			if requests := f.recorded()[before:]; len(requests) != 1 || len(fake.changes) != 0 {
				t.Errorf("%s on %s sent %+v, want one read of the key", name, id, requests)
			}
		}
	}
}

// A refused or lost request is reported in Qatlas' own words and sent once; neither GitHub's text, which here
// echoes the sealed value, nor the value reaches a message.
func TestPrivateRegistryWriteFailuresAreNamedAndSentOnce(t *testing.T) {
	fake := newRegistryWriteFake()
	dir, path := registryFile(t)
	f, core := registryWriteCore(t, fake, dir, nil)
	for _, tool := range []struct{ id, arguments string }{
		{privateRegistriesCreate.ID, createArguments(path, "")},
		{privateRegistriesUpdate.ID, `{"owner":"orgs/octo-org","secret_name":"NPM_REGISTRY_1","local_path":` + strconvQuote(path) + `}`},
	} {
		for _, tt := range []struct {
			status    int
			class     provider.Class
			fragment  string
			uncertain bool
			onKey     bool
		}{
			{http.StatusForbidden, provider.ClassPermission, "admin:org", false, false},
			{http.StatusForbidden, provider.ClassPermission, "admin:org", false, true},
			{http.StatusNotFound, provider.ClassNotFound, "", false, true},
			{http.StatusNotFound, provider.ClassNotFound, "", false, false},
			{http.StatusUnprocessableEntity, "", "rejected the private registry", false, false},
			{http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true, false},
			{http.StatusInternalServerError, "", "", true, false},
		} {
			fake.keyStatus, fake.changeCode = 0, 0
			if tt.onKey {
				fake.keyStatus = tt.status
			} else {
				fake.changeCode = tt.status
			}
			before := len(f.recorded())
			_, err := invoke(t, core, tool.id, "w-host", tool.arguments, true)
			if err == nil || (tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.fragment) {
				t.Errorf("%s answered %d (key %v) = %v, want %s naming %q", tool.id, tt.status, tt.onKey, err, tt.class, tt.fragment)
				continue
			}
			if got := strings.Contains(err.Error(), strings.TrimSpace(uncertain)); got != (tt.uncertain && !tt.onKey) {
				t.Errorf("%s answered %d (key %v): uncertain = %v in %v", tool.id, tt.status, tt.onKey, got, err)
			}
			assertNoRegistryLeak(t, fmt.Sprintf("%s answered %d", tool.id, tt.status), err.Error())
			want := 2
			if tt.onKey {
				want = 1
			}
			if requests := f.recorded()[before:]; len(requests) != want || len(fake.changes) != 0 {
				t.Errorf("%s answered %d (key %v) sent %d requests, want %d", tool.id, tt.status, tt.onKey, len(requests), want)
			}
		}
	}
	fake.keyStatus, fake.changeCode = 0, 0
}

// A success status other than 201 for a create and 204 for an update, and an answer without a usable name, are
// reported as uncertain, since the change may have been applied.
func TestPrivateRegistryWriteUnknownSuccessIsUncertain(t *testing.T) {
	fake := newRegistryWriteFake()
	dir, path := registryFile(t)
	_, core := registryWriteCore(t, fake, dir, nil)
	update := `{"owner":"orgs/octo-org","secret_name":"A","local_path":` + strconvQuote(path) + `}`
	for name, tt := range map[string]struct {
		set       func()
		id, input string
	}{
		"create with 200":    {func() { fake.createStatus = http.StatusOK }, privateRegistriesCreate.ID, createArguments(path, "")},
		"create with a name": {func() { fake.createName = "bad/name" + registryCanary }, privateRegistriesCreate.ID, createArguments(path, "")},
		"create no name":     {func() { fake.createName = "" }, privateRegistriesCreate.ID, createArguments(path, "")},
		"update with 200":    {func() { fake.updateStatus = http.StatusOK }, privateRegistriesUpdate.ID, update},
		"update with 202":    {func() { fake.updateStatus = http.StatusAccepted }, privateRegistriesUpdate.ID, update},
	} {
		fake.createStatus, fake.updateStatus, fake.createName = 0, 0, "NPM_REGISTRY_1"
		tt.set()
		_, err := invoke(t, core, tt.id, "w-host", tt.input, true)
		if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), strings.TrimSpace(uncertain)) {
			t.Errorf("%s = %v, want an invalid response saying the change may have been applied", name, err)
			continue
		}
		assertNoRegistryLeak(t, name, err.Error())
	}
}

// A client-side deadline during the change is a timeout after exactly the key read and one change.
func TestPrivateRegistryWriteTimeoutIsNotRepeated(t *testing.T) {
	fake := newRegistryWriteFake()
	stall := newChangeStall(t)
	fake.blockChange = stall
	dir, path := registryFile(t)
	f, core := registryWriteCore(t, fake, dir, nil)
	for _, tool := range []struct{ id, arguments string }{
		{privateRegistriesCreate.ID, createArguments(path, "")},
		{privateRegistriesUpdate.ID, `{"owner":"orgs/octo-org","secret_name":"A","local_path":` + strconvQuote(path) + `}`},
	} {
		ctx, cancel := stall.context()
		before := len(f.recorded())
		_, err := core.Invoke(ctx, application.InvokeRequest{Operation: tool.id, Connection: "w-host", Confirmed: true,
			Arguments: json.RawMessage(tool.arguments)})
		cancel()
		if classOf(err) != provider.ClassTimeout || len(f.recorded()) != before+2 {
			t.Errorf("%s deadline = %v after %d requests, want a timeout after the key and one change", tool.id, err,
				len(f.recorded())-before)
		}
		if err != nil {
			assertNoRegistryLeak(t, tool.id, err.Error())
		}
	}
}

// Both tools are offered only where a connection's tools list names them and the connection releases files for
// reading, need confirmation before anything is read, and have no argument or schema member that carries a
// value.
func TestPrivateRegistryWriteIsListedOnlyConfirmedAndCarriesNoValue(t *testing.T) {
	fake := newRegistryWriteFake()
	dir, path := registryFile(t)
	reads := 0
	f, core := registryWriteCore(t, fake, dir, &reads)
	var unsupported *capability.UnsupportedError
	for _, tool := range []struct{ id, arguments string }{
		{privateRegistriesCreate.ID, createArguments(path, "")},
		{privateRegistriesUpdate.ID, `{"owner":"orgs/octo-org","secret_name":"A","local_path":` + strconvQuote(path) + `}`},
	} {
		if _, err := core.Describe(application.DescribeRequest{Operation: tool.id, Connection: "listed-none"}); !asError(err, &unsupported) {
			t.Errorf("describe %s without a tools entry = %v, want unsupported", tool.id, err)
		}
		if _, err := core.Describe(application.DescribeRequest{Operation: tool.id, Connection: "org"}); !asError(err, &unsupported) {
			t.Errorf("describe %s on a connection without tools = %v, want unsupported", tool.id, err)
		}
		if _, err := core.Describe(application.DescribeRequest{Operation: tool.id, Connection: "w-host"}); err != nil {
			t.Errorf("describe %s on w-host = %v", tool.id, err)
		}
		if _, err := invoke(t, core, tool.id, "listed-none", tool.arguments, true); !asError(err, &unsupported) {
			t.Errorf("invoke %s without a tools entry = %v, want unsupported", tool.id, err)
		}
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := invoke(t, core, tool.id, "w-host", tool.arguments, false); !asError(err, &unconfirmed) {
			t.Errorf("invoke %s without confirmation = %v, want confirmation-required", tool.id, err)
		}
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
	// Without files.read the tools are not offered.
	cfg := discoveryConfig(serve(t, &fakeGitHub{failure: fake.route}))
	cfg.Connections["plain"] = config.Connection{Service: "gh", Credential: "gh-reader", Effects: config.Effects(),
		Targets: []string{"orgs/octo-org", registryHost}, Tools: privateRegistryWriteToolIDs}
	red := &redact.Redactor{}
	plain := application.New(registry(t), cfg, resolver(red, &reads), red)
	if _, err := invoke(t, plain, privateRegistriesCreate.ID, "plain", createArguments(path, ""), true); !asError(err, &unsupported) {
		t.Errorf("create without files.read = %v, want unsupported", err)
	}
	for _, d := range []capability.Descriptor{privateRegistriesCreate, privateRegistriesUpdate} {
		if d.LocalFiles != config.LocalFilesRead || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired {
			t.Errorf("%s descriptor = %+v", d.ID, d)
		}
		for _, argument := range d.Arguments {
			switch argument.Name {
			case "value", "content_base64", "encrypted_value", "key_id", "selected_repository_ids":
				t.Errorf("%s argument %s carries a value or an identifier", d.ID, argument.Name)
			}
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		for _, member := range []string{"value", "content_base64", "encrypted_value", "key_id", "selected_repository_ids",
			"tenant_id", "api_host", "workload_identity_provider"} {
			if _, found := schema.Properties[member]; found {
				t.Errorf("%s input schema has a member %s", d.ID, member)
			}
		}
		for _, member := range []string{"url", "local_path", "visibility", "selected_repositories", "registry_type"} {
			if _, found := schema.Properties[member]; !found {
				t.Errorf("%s input schema lacks %s", d.ID, member)
			}
		}
	}
}

// A sealed box is random: the same file sealed twice differs.
func TestPrivateRegistryWriteSealsAnew(t *testing.T) {
	fake := newRegistryWriteFake()
	dir, path := registryFile(t)
	_, core := registryWriteCore(t, fake, dir, nil)
	for range 2 {
		mustInvokeWith(t, core, privateRegistriesCreate.ID, "w-host", createArguments(path, ""))
	}
	if len(fake.changes) != 2 || fake.changes[0]["encrypted_value"] == fake.changes[1]["encrypted_value"] {
		t.Errorf("two creates sent the same sealed text")
	}
}
