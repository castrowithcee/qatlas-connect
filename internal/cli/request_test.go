package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const requestReasonMarker = "reasonmarker-synthetic-7f3a"

// requestFixture is a registry with a read tool and a deleting tool, and a configuration whose one
// connection offers read tools only, so the deleting tool can be requested.
func requestFixture(t *testing.T, blocked string) (*capability.Registry, string) {
	t.Helper()
	registry := capability.NewRegistry()
	if err := registry.RegisterProvider(config.ProviderMetadata{ID: "fake", Name: "Fake"}, nil); err != nil {
		t.Fatal(err)
	}
	handler := capability.Handler(func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor,
		json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	for id, effect := range map[string]capability.Effect{"fake.pages.get": capability.EffectRead, "fake.pages.delete": capability.EffectDelete} {
		err := registry.Register("fake", capability.Operation{Handler: handler, Descriptor: capability.Descriptor{
			ID: id, Version: 1, Description: "A fake tool", Provider: "fake",
			Risk: capability.Risk{Effect: effect, Idempotency: capability.IdempotencySafe,
				Confirmation: capability.ConfirmationNone, DataSensitivity: "test"},
			InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false}`),
			OutputSchema: json.RawMessage(`{"type":"object"}`),
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	extra := ""
	if blocked != "" {
		extra = "    blocked_tools: [" + blocked + "]\n"
	}
	path := writeConfig(t, `version: 4
services:
  fake:
    provider: fake
    base_url: https://example.invalid
credentials:
  reader:
    type: keyring
connections:
  primary:
    id: 0a1b2c3d4e5f60718293a4b5c6d7e8f9
    service: fake
    credential: reader
    effects: [read]
`+extra+`defaults: {}
`)
	return registry, path
}

func runRequestCLI(t *testing.T, registry *capability.Registry, path string, args ...string) (int, string, string) {
	t.Helper()
	opts := mcpLogOptions("")
	var stdout, stderr bytes.Buffer
	cmd := newRootCommand(opts, registry)
	cmd.SetIn(strings.NewReader(""))
	code := run(cmd, opts, append([]string{"--config", path}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func requestEntries(t *testing.T, path string) []invokelog.Entry {
	t.Helper()
	entries, _ := readMCPLog(t, path)
	return entries
}

func TestRequestCommandStoresAnOpenRequestWithoutATerminal(t *testing.T) {
	registry, path := requestFixture(t, "")
	code, stdout, stderr := runRequestCLI(t, registry, path, "request", "fake.pages.delete", "--reason", requestReasonMarker)
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	for _, want := range []string{"status: open", "connection: primary", "fake.pages.delete", "expires_at:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	data, err := os.ReadFile(accessrequest.PathFor(path))
	if err != nil || !strings.Contains(string(data), "fake.pages.delete") {
		t.Fatalf("requests.yaml = %q, %v", data, err)
	}
	entries, raw := readMCPLog(t, path)
	if len(entries) != 1 || entries[0].Operation != invokelog.OperationRequestCreate || entries[0].Tool != "fake.pages.delete" ||
		entries[0].Path != "cli" || entries[0].Connection != "primary" {
		t.Fatalf("log = %+v", entries)
	}
	if strings.Contains(raw, requestReasonMarker) {
		t.Error("the reason reached the log")
	}

	code, stdout, stderr = runRequestCLI(t, registry, path, "request", "fake.pages.delete", "--output", "json", "--connection", "primary")
	var answer map[string]any
	if code != exitOK || json.Unmarshal([]byte(stdout), &answer) != nil || answer["status"] != "open" || stderr != "" {
		t.Fatalf("json: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestRequestCommandRefusalsAreRequestProblems(t *testing.T) {
	registry, path := requestFixture(t, "fake.pages.delete")
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"blocked":   {[]string{"request", "fake.pages.delete"}, "request-blocked"},
		"offered":   {[]string{"request", "fake.pages.get"}, "invalid-request"},
		"unknown":   {[]string{"request", "fake.pages.nothing"}, "invalid-request"},
		"no tool":   {[]string{"request"}, "usage"},
		"unknown c": {[]string{"request", "fake.pages.delete", "--connection", "absent"}, "unknown-connection"},
	} {
		code, stdout, stderr := runRequestCLI(t, registry, path, tc.args...)
		if code != exitUsage || stdout != "" || !strings.Contains(stderr, tc.want) || strings.Contains(stderr, "config.yaml") {
			t.Errorf("%s: exit %d stdout %q stderr %q, want exit 2 and %q", name, code, stdout, stderr, tc.want)
		}
	}
	if _, err := os.Stat(accessrequest.PathFor(path)); err == nil {
		t.Error("a refused request wrote the queue")
	}
}

func TestToolsAllNamesWhereARequestCanAsk(t *testing.T) {
	registry, path := requestFixture(t, "")
	_, all, _ := runRequestCLI(t, registry, path, "tools", "fake", "--all", "--output", "json")
	var listed struct {
		Tools []struct {
			ID            string `json:"id"`
			RequestableOn string `json:"requestable_on"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(all), &listed); err != nil || len(listed.Tools) != 2 {
		t.Fatalf("tools --all = %q, %v", all, err)
	}
	for _, tool := range listed.Tools {
		want := ""
		if tool.ID == "fake.pages.delete" {
			want = "primary"
		}
		if tool.RequestableOn != want {
			t.Errorf("%s requestable_on = %q, want %q", tool.ID, tool.RequestableOn, want)
		}
	}
	if _, toon, _ := runRequestCLI(t, registry, path, "tools", "fake", "--all"); !strings.Contains(toon, "requestable_on") {
		t.Errorf("TOON has no requestable_on column:\n%s", toon)
	}
	if _, plain, _ := runRequestCLI(t, registry, path, "tools", "fake"); strings.Contains(plain, "requestable_on") {
		t.Errorf("tools without --all shows requestable_on:\n%s", plain)
	}
	// A blocked tool is refused and has no field.
	blockedRegistry, blockedPath := requestFixture(t, "fake.pages.delete")
	if _, out, _ := runRequestCLI(t, blockedRegistry, blockedPath, "tools", "fake", "--all"); strings.Contains(out, "requestable_on") {
		t.Errorf("a blocked tool is requestable:\n%s", out)
	}
}

func TestMCPRequestIsAnInvokeOperationWithItsOwnContract(t *testing.T) {
	registry, path := requestFixture(t, "")
	byID, stderr := runMCPLines(t, registry, mcpLogOptions(path), []string{
		`{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{` + mcpTestMeta + `}}`,
		mcpLogCall("describe", "describe", `{"operation":"qconnect.request"}`),
		mcpLogCall("request", "invoke", `{"operation":"qconnect.request","arguments":{"tools":["fake.pages.delete"],"reason":"`+requestReasonMarker+`"}}`),
		mcpLogCall("again", "invoke", `{"operation":"qconnect.request","connection":"primary","arguments":{"tools":["fake.pages.delete"]}}`),
		mcpLogCall("bad", "invoke", `{"operation":"qconnect.request","arguments":{"tools":[]}}`),
		mcpLogCall("search", "search", `{"provider":"fake","all":true}`),
	})
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	var list struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(byID["list"]), &list); err != nil || len(list.Result.Tools) != 3 {
		t.Fatalf("tools/list = %s, %v", byID["list"], err)
	}
	if !strings.Contains(byID["describe"], `\"name\":\"tools\"`) || !strings.Contains(byID["describe"], `\"required\":true`) ||
		strings.Contains(byID["describe"], `"isError":true`) {
		t.Errorf("describe = %s", byID["describe"])
	}
	if !strings.Contains(byID["request"], `\"status\":\"open\"`) || strings.Contains(byID["request"], `"isError":true`) ||
		strings.Contains(byID["request"], requestReasonMarker) {
		t.Errorf("request = %s", byID["request"])
	}
	if strings.Contains(byID["again"], `"isError":true`) {
		t.Errorf("a repeated request failed: %s", byID["again"])
	}
	if !strings.Contains(byID["bad"], `"isError":true`) || !strings.Contains(byID["bad"], "invalid-request") {
		t.Errorf("bad = %s", byID["bad"])
	}
	if strings.Contains(byID["search"], "qconnect.request") || !strings.Contains(byID["search"], `requestable_on\":\"primary`) {
		t.Errorf("search = %s", byID["search"])
	}

	// Only the request entries of the first call are there, no invoke entry, and never the reason.
	entries, raw := readMCPLog(t, path)
	var created int
	for _, e := range entries {
		switch {
		case e.Operation == invokelog.OperationRequestCreate:
			created++
			if e.Path != "mcp" || e.Tool != "fake.pages.delete" || e.Session == "" || e.ReqBytes != nil || e.RespBytes != nil {
				t.Errorf("request entry = %+v", e)
			}
		case e.Operation == "" || strings.HasPrefix(e.Operation, "fake."):
			t.Errorf("an invoke entry was written: %+v", e)
		}
	}
	if created != 1 {
		t.Errorf("request entries = %d, want 1 (the repeat adds no tool)", created)
	}
	if strings.Contains(raw, requestReasonMarker) {
		t.Error("the reason reached the log")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), accessrequest.FileName)); err != nil {
		t.Errorf("no queue file: %v", err)
	}
}
