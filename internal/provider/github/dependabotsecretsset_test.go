package github

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// A Dependabot set seals the file unchanged with the key of the Dependabot level, reads the key right before
// the one write, and never shows the value, even when the server echoes it.
func TestDependabotSecretsSetSealsTheFileAndWritesOnce(t *testing.T) {
	f, s, base := serveSecrets(t)
	dir, path := writeSecretFile(t, secretFileCanary)
	core := secretsSetCore(t, base, dir, nil)
	file := `"local_path":` + strconvQuote(path)
	for _, tt := range []struct {
		name, connection, target, secret, state, route string
		extra                                          []string
	}{
		{"new repository secret", "repo-listed", `"repository":"octo-org/example"`, "FRESH", "created",
			"/api/v3/repos/octo-org/example/dependabot/secrets", nil},
		{"existing repository secret", "repo-listed", `"repository":"octo-org/example"`, "registry_token", "updated",
			"/api/v3/repos/octo-org/example/dependabot/secrets", nil},
		{"organization secret for all", "org-only", `"organization":"orgs/octo-org","visibility":"all"`, "ORG_ALL",
			"created", "/api/v3/orgs/octo-org/dependabot/secrets", nil},
		{"organization secret for selected", "org-listed",
			`"organization":"orgs/octo-org","visibility":"selected","selected_repositories":["octo-org/example","Octo-Org/tools"]`,
			"SHARED_TOKEN", "created", "/api/v3/orgs/octo-org/dependabot/secrets",
			[]string{"/api/v3/repos/octo-org/example", "/api/v3/repos/Octo-Org/tools"}},
	} {
		before, putsBefore := len(f.recorded()), len(s.puts)
		got, err := invoke(t, core, dependabotSecretsSet.ID, tt.connection,
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
		if len(requests) != len(tt.extra)+2 {
			t.Fatalf("%s sent %d requests: %+v", tt.name, len(requests), requests)
		}
		key, put := requests[len(requests)-2], requests[len(requests)-1]
		if key.method != http.MethodGet || key.path != tt.route+"/public-key" {
			t.Errorf("%s key request = %s %s", tt.name, key.method, key.path)
		}
		if put.method != http.MethodPut || put.path != tt.route+"/"+tt.secret {
			t.Errorf("%s write = %s %s", tt.name, put.method, put.path)
		}
		if len(s.puts) != putsBefore+1 {
			t.Fatalf("%s: the server accepted %d writes", tt.name, len(s.puts)-putsBefore)
		}
		body := s.puts[len(s.puts)-1]
		if plain := s.open(t, body); plain != secretFileCanary {
			t.Errorf("%s sealed %q, want the file content unchanged", tt.name, plain)
		}
		if data, _ := json.Marshal(body); strings.Contains(string(data), "file-value-canary") {
			t.Errorf("%s sent the value in the clear", tt.name)
		}
		for member := range body {
			switch member {
			case "encrypted_value", "key_id", "visibility", "selected_repository_ids":
			default:
				t.Errorf("%s sent unexpected member %s", tt.name, member)
			}
		}
	}
}

// Refusals before the credential end without a secret read, a request, or a read of the file.
func TestDependabotSecretsSetRefusesBeforeFileAndCredential(t *testing.T) {
	f, _, base := serveSecrets(t)
	dir, path := writeSecretFile(t, "value")
	reads := 0
	core := secretsSetCore(t, base, dir, &reads)
	file := `"local_path":"/nonexistent-qatlas-dir/value"`
	org := `"organization":"orgs/octo-org"`
	for _, tt := range []struct {
		name, connection, arguments, fragment string
	}{
		{"neither target", "repo-listed", `{"name":"A",` + file + `}`, "exactly one"},
		{"an environment", "repo-listed", `{"repository":"octo-org/example","environment":"production","name":"A",` + file + `}`, "environment"},
		{"an environment with organization", "org-listed", `{` + org + `,"environment":"production","name":"A",` + file + `}`, "environment"},
		{"repository outside the targets", "repo-listed", `{"repository":"other-org/example","name":"A",` + file + `}`, ""},
		{"organization outside the targets", "repo-listed", `{` + org + `,"name":"A","visibility":"all",` + file + `}`, ""},
		{"an organization without visibility", "org-only", `{` + org + `,"name":"A",` + file + `}`, "visibility is required"},
		{"a selected repository outside the targets", "org-listed",
			`{` + org + `,"name":"A","visibility":"selected","selected_repositories":["octo-org/secret"],` + file + `}`, "not available"},
		{"a selected repository of another organization", "org-only",
			`{` + org + `,"name":"A","visibility":"selected","selected_repositories":["other-org/x"],` + file + `}`, "organization only"},
		{"all with repository targets", "org-listed", `{` + org + `,"name":"A","visibility":"all",` + file + `}`, "outside the targets"},
		{"a value argument", "repo-listed", `{"repository":"octo-org/example","name":"A","value":"x",` + file + `}`, ""},
		{"a content argument", "repo-listed", `{"repository":"octo-org/example","name":"A","content_base64":"eA==",` + file + `}`, ""},
		{"no file", "repo-listed", `{"repository":"octo-org/example","name":"A"}`, ""},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, dependabotSecretsSet.ID, tt.connection, tt.arguments, true)
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
	if _, err := invoke(t, core, dependabotSecretsSet.ID, "org-only", `{`+org+`,"name":"A","visibility":"private",`+
		`"local_path":`+strconvQuote(path)+`}`, true); err != nil {
		t.Errorf("private for an unrestricted organization = %v", err)
	}
}

// Unusable files are refused before the credential; without files.read the tool is not offered.
func TestDependabotSecretsSetRefusesUnusableFilesBeforeTheCredential(t *testing.T) {
	f, _, base := serveSecrets(t)
	dir, _ := writeSecretFile(t, "inside")
	_, outside := writeSecretFile(t, secretFileCanary)
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", maxSecretValueBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	core := secretsSetCore(t, base, dir, &reads)
	repo := `"repository":"octo-org/example","name":"A","local_path":`
	for _, tt := range []struct{ name, path string }{
		{"outside the released directory", outside},
		{"a missing file", filepath.Join(dir, "missing")},
		{"a file over 48 KB", big},
	} {
		reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, dependabotSecretsSet.ID, "repo-listed", `{`+repo+strconvQuote(tt.path)+`}`, true)
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
	plain := secretsCore(t, base, &reads)
	reads = 0
	before := len(f.recorded())
	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, plain, dependabotSecretsSet.ID, "repo-listed", `{`+repo+strconvQuote(path0(dir))+`}`, true); !asError(err, &unsupported) {
		t.Errorf("set without files.read = %v, want unsupported", err)
	}
	if reads != 0 || len(f.recorded()) != before {
		t.Errorf("a set without files.read reached the credential or GitHub")
	}
}

func path0(dir string) string { return filepath.Join(dir, "value") }

// A refused or lost write is reported in Qatlas' own words and sent once; the server's echo of the sealed
// value and the value never reach a message.
func TestDependabotSecretsSetFailuresAreSentOnce(t *testing.T) {
	f, s, base := serveSecrets(t)
	dir, path := writeSecretFile(t, secretFileCanary)
	core := secretsSetCore(t, base, dir, nil)
	for _, tt := range []struct {
		status   int
		class    provider.Class
		fragment string
	}{
		{http.StatusForbidden, provider.ClassPermission, "Dependabot secrets: read and write"},
		{http.StatusUnprocessableEntity, "", "rejected the secret"},
		{http.StatusGatewayTimeout, provider.ClassTimeout, "did not answer in time"},
	} {
		s.putStatus = tt.status
		before, putsBefore := len(f.recorded()), len(s.puts)
		_, err := invoke(t, core, dependabotSecretsSet.ID, "repo-listed",
			`{"repository":"octo-org/example","name":"FRESH","local_path":`+strconvQuote(path)+`}`, true)
		if err == nil || (tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.fragment) {
			t.Errorf("status %d = %v, want %s naming %q", tt.status, err, tt.class, tt.fragment)
			continue
		}
		if tt.status == http.StatusGatewayTimeout && !strings.Contains(err.Error(), uncertain) {
			t.Errorf("a timeout is not reported as uncertain: %v", err)
		}
		for _, leak := range []string{secretProviderCanary, "file-value-canary", "encrypted_value", dir} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("status %d leaks %q: %v", tt.status, leak, err)
			}
		}
		if requests := f.recorded()[before:]; len(requests) != 2 || len(s.puts) != putsBefore {
			t.Errorf("status %d sent %d requests, want a key read and one write", tt.status, len(requests))
		}
	}
	s.putStatus = 0
}

// The tool is offered only where a connection's tools list names it, needs confirmation, and carries no value
// argument or environment.
func TestDependabotSecretsSetIsListedOnlyConfirmedAndCarriesNoValue(t *testing.T) {
	f, _, base := serveSecrets(t)
	dir, path := writeSecretFile(t, "value")
	reads := 0
	core := secretsSetCore(t, base, dir, &reads)
	var unsupported *capability.UnsupportedError
	if _, err := core.Describe(application.DescribeRequest{Operation: dependabotSecretsSet.ID, Connection: "repo"}); !asError(err, &unsupported) {
		t.Errorf("describe on repo = %v, want unsupported", err)
	}
	if _, err := core.Describe(application.DescribeRequest{Operation: dependabotSecretsSet.ID, Connection: "repo-listed"}); err != nil {
		t.Errorf("describe on repo-listed = %v", err)
	}
	arguments := `{"repository":"octo-org/example","name":"A","local_path":` + strconvQuote(path) + `}`
	if _, err := invoke(t, core, dependabotSecretsSet.ID, "repo", arguments, true); !asError(err, &unsupported) {
		t.Errorf("invoke on repo = %v, want unsupported", err)
	}
	var unconfirmed *application.ConfirmationRequiredError
	if _, err := invoke(t, core, dependabotSecretsSet.ID, "repo-listed", arguments, false); !asError(err, &unconfirmed) {
		t.Errorf("invoke without confirmation = %v, want confirmation-required", err)
	}
	if reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("refused requests read %d credentials and sent %d requests", reads, len(f.recorded()))
	}
	d := dependabotSecretsSet
	if d.LocalFiles != config.LocalFilesRead || !d.RequiresToolAllowList || d.Risk.Confirmation != capability.ConfirmationRequired ||
		d.Risk.Effect != capability.EffectUpdate {
		t.Errorf("descriptor = %+v", d)
	}
	for _, argument := range d.Arguments {
		switch argument.Name {
		case "value", "content_base64", "encrypted_value", "environment":
			t.Errorf("argument %s must not exist", argument.Name)
		}
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"value", "content_base64", "encrypted_value", "key_id", "environment"} {
		if _, found := schema.Properties[member]; found {
			t.Errorf("the input schema has a member %s", member)
		}
	}
	for _, member := range []string{"name", "local_path", "visibility", "selected_repositories", "repository", "organization"} {
		if _, found := schema.Properties[member]; !found {
			t.Errorf("the input schema lacks %s", member)
		}
	}
}
