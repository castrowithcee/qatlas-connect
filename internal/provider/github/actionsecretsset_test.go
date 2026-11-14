package github

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
	"golang.org/x/crypto/nacl/box"
)

// secretFileCanary is the content of the value file; it must never reach an answer, a message, or a request
// in the clear.
const secretFileCanary = "file-value-canary-91d2 with spaces\n"

// secretsSetCore is secretsCore with the directory dir released for reading on every connection.
func secretsSetCore(t *testing.T, base, dir string, reads *int) *application.Core {
	t.Helper()
	cfg := secretsConfig(base)
	for name, connection := range cfg.Connections {
		if connection.Service == "gh" {
			connection.Files = config.Files{Read: []string{dir}}
			cfg.Connections[name] = connection
		}
	}
	red := &redact.Redactor{}
	return application.New(registry(t), cfg, resolver(red, reads), red)
}

// writeSecretFile writes content to a new file of a fresh released directory and returns both.
func writeSecretFile(t *testing.T, content string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	path = filepath.Join(dir, "value")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

// open decrypts a body a set sent with the private key of the fake.
func (s *fakeSecrets) open(t *testing.T, body map[string]any) string {
	t.Helper()
	sealed, err := base64.StdEncoding.DecodeString(body["encrypted_value"].(string))
	if err != nil {
		t.Fatalf("encrypted_value is not Base64: %v", err)
	}
	plain, ok := box.OpenAnonymous(nil, sealed, s.publicKey, s.privateKey)
	if !ok {
		t.Fatal("the sealed box does not open with the key of the level")
	}
	return string(plain)
}

// A set seals the file unchanged with the key of its own level, reads the key right before the one write,
// answers created or updated by GitHub's status, and never shows the value.
func TestActionSecretsSetSealsTheFileAndWritesOnce(t *testing.T) {
	f, s, base := serveSecrets(t)
	dir, path := writeSecretFile(t, secretFileCanary)
	core := secretsSetCore(t, base, dir, nil)
	file := `"local_path":"` + path + `"`
	for _, tt := range []struct {
		name, connection, target, secret, state, route string
		extra                                          []string
	}{
		{"new repository secret", "repo-listed", `"repository":"octo-org/example"`, "FRESH", "created",
			"/api/v3/repos/octo-org/example/actions/secrets", nil},
		{"existing repository secret", "repo-listed", `"repository":"octo-org/example"`, "deploy_token", "updated",
			"/api/v3/repos/octo-org/example/actions/secrets", nil},
		{"environment secret", "repo-listed", `"repository":"octo-org/example","environment":"production"`,
			"PROD_KEY", "updated", "/api/v3/repos/octo-org/example/environments/production/secrets", nil},
		{"organization secret for all", "org-only", `"organization":"orgs/octo-org","visibility":"all"`, "ORG_ALL",
			"created", "/api/v3/orgs/octo-org/actions/secrets", nil},
		{"organization secret for selected", "org-listed",
			`"organization":"orgs/octo-org","visibility":"selected","selected_repositories":["octo-org/example","Octo-Org/tools"]`,
			"SHARED_TOKEN", "updated", "/api/v3/orgs/octo-org/actions/secrets",
			[]string{"/api/v3/repos/octo-org/example", "/api/v3/repos/Octo-Org/tools"}},
	} {
		before, putsBefore := len(f.recorded()), len(s.puts)
		got, err := invoke(t, core, actionSecretsSet.ID, tt.connection,
			`{`+tt.target+`,"name":"`+tt.secret+`",`+file+`}`, true)
		if err != nil || !strings.Contains(string(got), `"state":"`+tt.state+`"`) ||
			!strings.Contains(string(got), `"name":"`+tt.secret+`"`) {
			t.Fatalf("%s = %s, %v", tt.name, got, err)
		}
		for _, leak := range []string{secretFileCanary, "file-value-canary", "encrypted_value", "key_id", "012345678"} {
			if strings.Contains(string(got), leak) {
				t.Errorf("%s answer leaks %q: %s", tt.name, leak, got)
			}
		}
		requests := f.recorded()[before:]
		want := len(tt.extra) + 2
		if len(requests) != want {
			t.Fatalf("%s sent %d requests, want %d: %+v", tt.name, len(requests), want, requests)
		}
		for i, path := range tt.extra {
			if requests[i].method != http.MethodGet || requests[i].path != path {
				t.Errorf("%s request %d = %s %s, want GET %s", tt.name, i, requests[i].method, requests[i].path, path)
			}
		}
		key, put := requests[len(requests)-2], requests[len(requests)-1]
		if key.method != http.MethodGet || key.path != tt.route+"/public-key" {
			t.Errorf("%s key request = %s %s, want GET %s/public-key", tt.name, key.method, key.path, tt.route)
		}
		if put.method != http.MethodPut || put.path != tt.route+"/"+tt.secret {
			t.Errorf("%s write = %s %s, want PUT %s/%s", tt.name, put.method, put.path, tt.route, tt.secret)
		}
		if len(s.puts) != putsBefore+1 {
			t.Fatalf("%s: the server accepted %d writes", tt.name, len(s.puts)-putsBefore)
		}
		body := s.puts[len(s.puts)-1]
		if plain := s.open(t, body); plain != secretFileCanary {
			t.Errorf("%s sealed %q, want the file content unchanged", tt.name, plain)
		}
		if body["key_id"] != "012345678" {
			t.Errorf("%s key_id = %v", tt.name, body["key_id"])
		}
		for member := range body {
			switch member {
			case "encrypted_value", "key_id":
			case "visibility", "selected_repository_ids":
				if !strings.Contains(tt.target, "organization") {
					t.Errorf("%s sent %s for a level without it", tt.name, member)
				}
			default:
				t.Errorf("%s sent unexpected member %s", tt.name, member)
			}
		}
		if data, _ := json.Marshal(body); strings.Contains(string(data), "file-value-canary") {
			t.Errorf("%s sent the value in the clear", tt.name)
		}
		if strings.Contains(tt.target, "selected") {
			ids, _ := json.Marshal(body["selected_repository_ids"])
			if string(ids) != "[1296269,1296270]" || body["visibility"] != "selected" ||
				!strings.Contains(string(got), `"selected_repositories":["octo-org/example","Octo-Org/tools"]`) {
				t.Errorf("selected = %s, visibility %v, answer %s", ids, body["visibility"], got)
			}
		}
	}
}

// Every refusal before the credential ends without a secret read and without a request, with the file never
// opened: the target and the visibility rules come first, then the file.
func TestActionSecretsSetRefusesBeforeFileAndCredential(t *testing.T) {
	f, _, base := serveSecrets(t)
	dir, path := writeSecretFile(t, "value")
	reads := 0
	core := secretsSetCore(t, base, dir, &reads)
	// The file does not exist: a refusal that came from the file would say so, not name the argument rule.
	file := `"local_path":"/nonexistent-qatlas-dir/value"`
	org := `"organization":"orgs/octo-org"`
	for _, tt := range []struct {
		name, connection, arguments, fragment string
	}{
		{"neither target", "repo-listed", `{"name":"A",` + file + `}`, "exactly one"},
		{"both targets", "org-listed", `{"repository":"octo-org/example",` + org + `,"name":"A",` + file + `}`, "exactly one"},
		{"environment with organization", "org-listed", `{` + org + `,"environment":"production","name":"A",` + file + `}`, "environment"},
		{"repository outside the targets", "repo-listed", `{"repository":"other-org/example","name":"A",` + file + `}`, ""},
		{"excluded repository", "org-listed", `{"repository":"octo-org/secret","name":"A",` + file + `}`, ""},
		{"organization outside the targets", "repo-listed", `{` + org + `,"name":"A","visibility":"all",` + file + `}`, ""},
		{"a name with a hyphen", "repo-listed", `{"repository":"octo-org/example","name":"A-B",` + file + `}`, "name"},
		{"the GITHUB_ prefix", "repo-listed", `{"repository":"octo-org/example","name":"github_x",` + file + `}`, "name"},
		{"an organization without visibility", "org-only", `{` + org + `,"name":"A",` + file + `}`, "visibility is required"},
		{"visibility for a repository", "repo-listed", `{"repository":"octo-org/example","name":"A","visibility":"all",` + file + `}`, "organization secret"},
		{"selected without a list", "org-only", `{` + org + `,"name":"A","visibility":"selected",` + file + `}`, "selected_repositories"},
		{"a list without selected", "org-only", `{` + org + `,"name":"A","visibility":"all","selected_repositories":["octo-org/example"],` + file + `}`, "selected_repositories"},
		{"a selected repository of another organization", "org-only",
			`{` + org + `,"name":"A","visibility":"selected","selected_repositories":["other-org/x"],` + file + `}`, "organization only"},
		{"an excluded selected repository", "org-listed",
			`{` + org + `,"name":"A","visibility":"selected","selected_repositories":["octo-org/secret"],` + file + `}`, "not available"},
		{"a selected repository outside the targets", "open-excluding",
			`{"repository":"octo-org/example","name":"A",` + file + `}`, ""},
		{"all with a repository exclusion", "org-listed", `{` + org + `,"name":"A","visibility":"all",` + file + `}`, "outside the targets"},
		{"private with a repository exclusion", "org-listed", `{` + org + `,"name":"A","visibility":"private",` + file + `}`, "outside the targets"},
		{"a value argument", "repo-listed", `{"repository":"octo-org/example","name":"A","value":"x",` + file + `}`, ""},
		{"a content argument", "repo-listed", `{"repository":"octo-org/example","name":"A","content_base64":"eA==",` + file + `}`, ""},
		{"no file", "repo-listed", `{"repository":"octo-org/example","name":"A"}`, ""},
		{"an empty file path", "repo-listed", `{"repository":"octo-org/example","name":"A","local_path":""}`, ""},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, actionSecretsSet.ID, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) && !isApplicationInvalid(err) {
			t.Errorf("%s = %v, want an invalid request", tt.name, err)
		}
		if err != nil && tt.fragment != "" && !strings.Contains(err.Error(), tt.fragment) {
			t.Errorf("%s = %v, want a message naming %q", tt.name, err, tt.fragment)
		}
		if err != nil && strings.Contains(err.Error(), "nonexistent-qatlas-dir") {
			t.Errorf("%s names the path: %v", tt.name, err)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s reached the credential or GitHub", tt.name)
		}
	}
	// An organization whose targets are only the owner or all its repositories admits all and private.
	if _, err := invoke(t, core, actionSecretsSet.ID, "org-only", `{`+org+`,"name":"A","visibility":"private",`+
		`"local_path":"`+path+`"}`, true); err != nil {
		t.Errorf("private for an unrestricted organization = %v", err)
	}
}

func isApplicationInvalid(err error) bool {
	var invalid *application.InvalidRequestError
	return err != nil && (asError(err, &invalid))
}

// The file is checked after the arguments and before the credential: outside the released directories, a
// symbolic link that leaves them, a directory, a missing, an empty, and an oversized file are refused without
// a secret read or a request, and no message names the path or the content.
func TestActionSecretsSetRefusesUnusableFilesBeforeTheCredential(t *testing.T) {
	f, _, base := serveSecrets(t)
	dir, _ := writeSecretFile(t, "inside")
	_, outside := writeSecretFile(t, secretFileCanary)
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skipf("no symbolic links: %v", err)
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", maxSecretValueBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	limit := filepath.Join(dir, "limit")
	if err := os.WriteFile(limit, []byte(strings.Repeat("a", maxSecretValueBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	core := secretsSetCore(t, base, dir, &reads)
	repo := `"repository":"octo-org/example","name":"A","local_path":`
	for _, tt := range []struct{ name, path string }{
		{"outside the released directory", outside},
		{"a symbolic link out", filepath.Join(dir, "link")},
		{"a directory", dir},
		{"a missing file", filepath.Join(dir, "missing")},
		{"a relative path", "value"},
		{"a dot-dot path", dir + "/../value"},
		{"an empty file", empty},
		{"a file over 48 KB", big},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, actionSecretsSet.ID, "repo-listed", `{`+repo+strconvQuote(tt.path)+`}`, true)
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
	// A file of exactly 48 KB is the largest accepted.
	if _, err := invoke(t, core, actionSecretsSet.ID, "repo-listed", `{`+repo+strconvQuote(limit)+`}`, true); err != nil {
		t.Errorf("a file of 48 KB = %v, want it accepted", err)
	}
	// Without files.read the tool is not offered, and nothing is read.
	plain := secretsCore(t, base, &reads)
	reads = 0
	before := len(f.recorded())
	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, plain, actionSecretsSet.ID, "repo-listed", `{`+repo+strconvQuote(limit)+`}`, true); !asError(err, &unsupported) {
		t.Errorf("set without files.read = %v, want unsupported", err)
	}
	if reads != 0 || len(f.recorded()) != before {
		t.Errorf("a set without files.read reached the credential or GitHub")
	}
}

// A key that is not Base64 of exactly 32 bytes, or a key without an id, ends the call before any write.
func TestActionSecretsSetRefusesAnUnusableKey(t *testing.T) {
	f, s, base := serveSecrets(t)
	dir, path := writeSecretFile(t, secretFileCanary)
	core := secretsSetCore(t, base, dir, nil)
	short := base64.StdEncoding.EncodeToString(make([]byte, 31))
	long := base64.StdEncoding.EncodeToString(make([]byte, 33))
	for name, answer := range map[string]string{
		"not Base64": `{"key_id":"1","key":"!!!not base64!!!"}`,
		"31 bytes":   `{"key_id":"1","key":"` + short + `"}`,
		"33 bytes":   `{"key_id":"1","key":"` + long + `"}`,
		"no key":     `{"key_id":"1"}`,
		"no key id":  `{"key":"` + base64.StdEncoding.EncodeToString(s.publicKey[:]) + `"}`,
		"empty id":   `{"key_id":"","key":"` + base64.StdEncoding.EncodeToString(s.publicKey[:]) + `"}`,
		"a string":   `"x"`,
	} {
		s.keyAnswer = answer
		before := len(f.recorded())
		_, err := invoke(t, core, actionSecretsSet.ID, "repo-listed",
			`{"repository":"octo-org/example","name":"A","local_path":`+strconvQuote(path)+`}`, true)
		if classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s = %v, want an invalid response", name, err)
		}
		requests := f.recorded()[before:]
		if len(requests) != 1 || requests[0].method != http.MethodGet || len(s.puts) != 0 {
			t.Errorf("%s sent %+v and %d writes, want one read of the key", name, requests, len(s.puts))
		}
		if err != nil && strings.Contains(err.Error(), "file-value-canary") {
			t.Errorf("%s quotes the value: %v", name, err)
		}
	}
}

// A refused or lost write is reported in Qatlas' own words, sent once and never repeated; neither GitHub's
// text, which here echoes the sealed value, nor the value reaches a message.
func TestActionSecretsSetFailuresAreNamedAndSentOnce(t *testing.T) {
	f, s, base := serveSecrets(t)
	dir, path := writeSecretFile(t, secretFileCanary)
	core := secretsSetCore(t, base, dir, nil)
	for _, level := range []struct{ name, connection, target, right string }{
		{"repository", "repo-listed", `"repository":"octo-org/example"`, "Secrets: read and write"},
		{"environment", "repo-listed", `"repository":"octo-org/example","environment":"production"`, "Secrets: read and write"},
		{"organization", "org-only", `"organization":"orgs/octo-org","visibility":"all"`, "admin:org"},
	} {
		for _, tt := range []struct {
			status    int
			class     provider.Class
			fragment  string
			uncertain bool
			onKey     bool
		}{
			{http.StatusForbidden, provider.ClassPermission, level.right, false, false},
			{http.StatusForbidden, provider.ClassPermission, level.right, false, true},
			{http.StatusNotFound, provider.ClassNotFound, "", false, true},
			{http.StatusUnprocessableEntity, "", "rejected the secret", false, false},
			{http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time", true, false},
			{http.StatusInternalServerError, "", "", true, false},
		} {
			s.status, s.putStatus = 0, 0
			if tt.onKey {
				s.status = tt.status
			} else {
				s.putStatus = tt.status
			}
			before, putsBefore := len(f.recorded()), len(s.puts)
			_, err := invoke(t, core, actionSecretsSet.ID, level.connection,
				`{`+level.target+`,"name":"FRESH","local_path":`+strconvQuote(path)+`}`, true)
			if err == nil || (tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.fragment) {
				t.Errorf("%s answered %d (key %v) = %v, want %s naming %q", level.name, tt.status, tt.onKey, err, tt.class, tt.fragment)
				continue
			}
			if got := strings.Contains(err.Error(), uncertain); got != (tt.uncertain && !tt.onKey) {
				t.Errorf("%s answered %d (key %v): uncertain = %v in %v", level.name, tt.status, tt.onKey, got, err)
			}
			for _, leak := range []string{secretProviderCanary, "file-value-canary", "encrypted_value", dir} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("%s answered %d leaks %q: %v", level.name, tt.status, leak, err)
				}
			}
			want := 2
			if tt.onKey {
				want = 1
			}
			if requests := f.recorded()[before:]; len(requests) != want || len(s.puts) != putsBefore {
				t.Errorf("%s answered %d (key %v) sent %d requests, want %d", level.name, tt.status, tt.onKey, len(requests), want)
			}
		}
	}
	s.status, s.putStatus = 0, 0
}

// A success status other than 201 and 204 is reported as uncertain, since the write may have been applied.
func TestActionSecretsSetUnknownSuccessIsUncertain(t *testing.T) {
	_, s, base := serveSecrets(t)
	dir, path := writeSecretFile(t, "value")
	core := secretsSetCore(t, base, dir, nil)
	s.putStatus = http.StatusAccepted
	_, err := invoke(t, core, actionSecretsSet.ID, "repo-listed",
		`{"repository":"octo-org/example","name":"A","local_path":`+strconvQuote(path)+`}`, true)
	if err == nil || strings.Contains(err.Error(), secretProviderCanary) {
		t.Errorf("a 202 write = %v", err)
	}
}

// The tool is offered only where a connection's tools list names it, needs confirmation before anything is
// read, and has no argument or schema member that carries a value.
func TestActionSecretsSetIsListedOnlyConfirmedAndCarriesNoValue(t *testing.T) {
	f, _, base := serveSecrets(t)
	dir, path := writeSecretFile(t, "value")
	reads := 0
	core := secretsSetCore(t, base, dir, &reads)
	var unsupported *capability.UnsupportedError
	if _, err := core.Describe(application.DescribeRequest{Operation: actionSecretsSet.ID, Connection: "repo"}); !asError(err, &unsupported) {
		t.Errorf("describe on repo = %v, want unsupported", err)
	}
	if _, err := core.Describe(application.DescribeRequest{Operation: actionSecretsSet.ID, Connection: "repo-listed"}); err != nil {
		t.Errorf("describe on repo-listed = %v", err)
	}
	arguments := `{"repository":"octo-org/example","name":"A","local_path":` + strconvQuote(path) + `}`
	if _, err := invoke(t, core, actionSecretsSet.ID, "repo", arguments, true); !asError(err, &unsupported) {
		t.Errorf("invoke on repo = %v, want unsupported", err)
	}
	var unconfirmed *application.ConfirmationRequiredError
	if _, err := invoke(t, core, actionSecretsSet.ID, "repo-listed", arguments, false); !asError(err, &unconfirmed) {
		t.Errorf("invoke without confirmation = %v, want confirmation-required", err)
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
	d := actionSecretsSet
	if d.LocalFiles != config.LocalFilesRead || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired ||
		d.Risk.Effect != capability.EffectUpdate {
		t.Errorf("descriptor = %+v", d)
	}
	for _, argument := range d.Arguments {
		if argument.Name == "value" || argument.Name == "content_base64" || argument.Name == "encrypted_value" {
			t.Errorf("argument %s carries a value", argument.Name)
		}
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"value", "content_base64", "encrypted_value", "key_id", "selected_repository_ids"} {
		if _, found := schema.Properties[member]; found {
			t.Errorf("the input schema has a member %s", member)
		}
	}
	for _, member := range []string{"name", "local_path", "visibility", "selected_repositories", "repository", "environment", "organization"} {
		if _, found := schema.Properties[member]; !found {
			t.Errorf("the input schema lacks %s", member)
		}
	}
}

// A sealed box is random: the same file sealed twice differs, and a random key pair of a test never opens
// another level's key.
func TestActionSecretsSetSealsAnew(t *testing.T) {
	_, s, base := serveSecrets(t)
	dir, path := writeSecretFile(t, "same")
	core := secretsSetCore(t, base, dir, nil)
	arguments := `{"repository":"octo-org/example","name":"A","local_path":` + strconvQuote(path) + `}`
	for range 2 {
		if _, err := invoke(t, core, actionSecretsSet.ID, "repo-listed", arguments, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.puts) != 2 || s.puts[0]["encrypted_value"] == s.puts[1]["encrypted_value"] {
		t.Errorf("two sets sent the same sealed text")
	}
	otherPublic, otherPrivate, _ := box.GenerateKey(rand.Reader)
	sealed, _ := base64.StdEncoding.DecodeString(s.puts[0]["encrypted_value"].(string))
	if _, ok := box.OpenAnonymous(nil, sealed, otherPublic, otherPrivate); ok {
		t.Errorf("another key opened the sealed value")
	}
}

// restrictsRepositories follows the targets and the exclusions of the organization.
func TestAllowlistRestrictsRepositories(t *testing.T) {
	for _, tt := range []struct {
		targets, excludes []string
		want              bool
	}{
		{nil, nil, false},
		{[]string{"orgs/octo-org"}, nil, false},
		{[]string{"orgs/octo-org", "repos/octo-org/*"}, nil, false},
		{[]string{"orgs/octo-org", "repos/octo-org/example"}, nil, true},
		{[]string{"repos/octo-org/example"}, nil, true},
		{[]string{"repos/other-org/example"}, nil, false},
		{[]string{"orgs/octo-org"}, []string{"repos/octo-org/secret"}, true},
		{[]string{"orgs/octo-org"}, []string{"repos/other-org/secret"}, false},
	} {
		list, err := parseAllowlistExcluding(tt.targets, tt.excludes)
		if err != nil {
			t.Fatal(err)
		}
		if got := list.restrictsRepositories("Octo-Org"); got != tt.want {
			t.Errorf("restrictsRepositories(%v, %v) = %v, want %v", tt.targets, tt.excludes, got, tt.want)
		}
	}
}

// The public key route is named as such when GitHub refuses it.
func TestSecretSubjectNamesThePublicKey(t *testing.T) {
	for path, want := range map[string]string{
		"actions/secrets/public-key":              "the public key of the secrets",
		"environments/prod/secrets/public-key":    "the public key of the secrets of environment prod",
		"actions/secrets/public-key/repositories": "the repositories of a secret",
		"dependabot/secrets/public-key":           "the public key of the secrets",
	} {
		family := actionSecretFamily
		if strings.HasPrefix(path, "dependabot") {
			family = dependabotSecretFamily
		}
		if got := family.subject(path); got != want {
			t.Errorf("subject(%q) = %q, want %q", path, got, want)
		}
	}
}

func asError[T error](err error, target *T) bool { return errors.As(err, target) }

// strconvQuote is a JSON string for a path inside an arguments document.
func strconvQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
