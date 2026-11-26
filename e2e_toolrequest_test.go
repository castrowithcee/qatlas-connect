package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/secret"
)

// TestToolRequestEndToEnd drives the built binary: an agent asks for a tool its read-only connection does
// not offer over the CLI and over MCP, both write requests.yaml beside the configuration and request entries
// to the invocation log, the discovery names the connection to ask, and neither the reason nor an invoke
// entry reaches the log.
func TestToolRequestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("the acceptance run builds the binary")
	}
	dir := t.TempDir()
	bin := buildBinary(t, dir)
	configPath := filepath.Join(dir, "config.yaml")
	config := `version: 4
services:
  wiki:
    provider: bookstack
    base_url: https://example.invalid
credentials:
  reader:
    type: env
    values:
      token-id: E2E_REQ_ID
      token-secret: E2E_REQ_SECRET
connections:
  primary:
    id: 0a1b2c3d4e5f60718293a4b5c6d7e8f9
    service: wiki
    credential: reader
    effects: [read]
defaults:
  connections:
    bookstack: primary
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	c := &runner{bin: bin, seen: &seen, env: []string{
		"HOME=" + dir,
		"PATH=" + os.Getenv("PATH"),
		"QCONNECT_CONFIG=" + configPath,
		secret.StoreSelector + "=none",
		"E2E_REQ_ID=" + canaryPrimaryID,
		"E2E_REQ_SECRET=" + canaryPrimarySecret,
	}}
	const reasonMarker = "reasonmarker-synthetic-5c2e"

	if code, stdout, stderr := c.run(t, "tools", "bookstack", "content", "--all", "--output", "json"); code != 0 ||
		!strings.Contains(stdout, `"id":"bookstack.pages.delete"`) || !strings.Contains(stdout, `"requestable_on":"primary"`) {
		t.Fatalf("tools --all exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, stdout, stderr := c.run(t, "request", "bookstack.pages.delete", "--reason", reasonMarker)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "status: open") {
		t.Fatalf("request exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	queue := filepath.Join(dir, "requests.yaml")
	if data, err := os.ReadFile(queue); err != nil || !strings.Contains(string(data), "bookstack.pages.delete") {
		t.Fatalf("requests.yaml = %q, %v", data, err)
	}

	// The same tool over MCP joins the open request, a further tool of the group is new.
	mcpInput := `{"jsonrpc":"2.0","id":"describe","method":"tools/call","params":{` + mcpMeta +
		`,"name":"describe","arguments":{"operation":"qconnect.request"}}}` + "\n" +
		`{"jsonrpc":"2.0","id":"request","method":"tools/call","params":{` + mcpMeta +
		`,"name":"invoke","arguments":{"operation":"qconnect.request","arguments":{"tools":["bookstack.pages.delete","bookstack.comments.delete"],"reason":"` + reasonMarker + `"}}}}` + "\n"
	code, stdout, stderr = c.runInput(t, mcpInput, "mcp")
	if code != 0 || stderr != "" {
		t.Fatalf("mcp exit=%d stderr=%q", code, stderr)
	}
	responses := decodeMCP(t, stdout)
	if responses["describe"].Result.IsError || responses["request"].Result.IsError {
		t.Fatalf("MCP answers = %+v", responses)
	}
	if data, _ := os.ReadFile(queue); !strings.Contains(string(data), "bookstack.comments.delete") {
		t.Fatalf("requests.yaml lacks the MCP request: %s", data)
	}

	logs, err := filepath.Glob(filepath.Join(dir, "vault", "logs", "*.jsonl"))
	if err != nil || len(logs) == 0 {
		t.Fatalf("no log: %v %v", logs, err)
	}
	var log strings.Builder
	for _, name := range logs {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		log.Write(data)
	}
	entries := log.String()
	if got := strings.Count(entries, `"operation":"config.request.create"`); got != 2 {
		t.Fatalf("request entries = %d, want 2 (one per newly requested tool):\n%s", got, entries)
	}
	for _, line := range strings.Split(strings.TrimSpace(entries), "\n") {
		if strings.Contains(line, `"path":"mcp"`) && strings.Contains(line, `"operation":"config.request.create"`) &&
			(!strings.Contains(line, `"session"`) || strings.Contains(line, `"req_bytes"`)) {
			t.Errorf("MCP request entry = %s", line)
		}
		if strings.Contains(line, `"path":"mcp"`) && !strings.Contains(line, `"operation"`) {
			t.Errorf("an invoke entry without operation: %s", line)
		}
	}
	if strings.Contains(entries, reasonMarker) {
		t.Fatalf("the reason reached the log:\n%s", entries)
	}
}

const e2eRemovalConfig = `version: 4
services:
  wiki:
    provider: bookstack
    base_url: https://example.invalid
credentials:
  reader:
    type: env
    values:
      token-id: E2E_REQ_ID
      token-secret: E2E_REQ_SECRET
connections:
  primary:
    id: 0a1b2c3d4e5f60718293a4b5c6d7e8f9
    service: wiki
    credential: reader
    tools: [bookstack.books.list, bookstack.pages.get, bookstack.pages.list]
defaults:
  connections:
    bookstack: primary
`

// newRemovalRunner writes a configuration with a tools list and returns a runner over it.
func newRemovalRunner(t *testing.T) (*runner, string) {
	t.Helper()
	dir := t.TempDir()
	bin := buildBinary(t, dir)
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(e2eRemovalConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	return &runner{bin: bin, seen: &seen, env: []string{
		"HOME=" + dir,
		"PATH=" + os.Getenv("PATH"),
		"QCONNECT_CONFIG=" + configPath,
		"XDG_RUNTIME_DIR=" + shortRuntimeDir(t),
		secret.StoreSelector + "=none",
		"E2E_REQ_ID=" + canaryPrimaryID,
		"E2E_REQ_SECRET=" + canaryPrimarySecret,
	}}, dir
}

// TestToolRemovalRequestEndToEnd drives the built binary: an agent asks over the CLI and over MCP that its
// connection stop offering tools, both join one request of the kind remove-tools, the list names the kind, a
// tool the connection does not offer is refused, and without a person at a terminal nothing is decided.
func TestToolRemovalRequestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("the acceptance run builds the binary")
	}
	c, dir := newRemovalRunner(t)
	configPath := filepath.Join(dir, "config.yaml")

	code, stdout, stderr := c.run(t, "request", "--remove", "bookstack.pages.get", "--reason", "synthetic-reason-5c2e")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "status: open") || !strings.Contains(stdout, "remove-tools") {
		t.Fatalf("request --remove exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, _, stderr = c.run(t, "request", "--remove", "bookstack.pages.delete"); code != 2 || !strings.Contains(stderr, "invalid-request") {
		t.Fatalf("a tool the connection does not offer: exit=%d stderr=%q", code, stderr)
	}
	mcpInput := `{"jsonrpc":"2.0","id":"request","method":"tools/call","params":{` + mcpMeta +
		`,"name":"invoke","arguments":{"operation":"qconnect.request","arguments":{"kind":"remove-tools","tools":["bookstack.books.list"]}}}}` + "\n"
	code, stdout, stderr = c.runInput(t, mcpInput, "mcp")
	if responses := decodeMCP(t, stdout); code != 0 || stderr != "" || responses["request"].Result.IsError {
		t.Fatalf("mcp exit=%d stderr=%q answers=%+v", code, stderr, responses)
	}

	code, stdout, stderr = c.run(t, "requests", "--output", "json")
	var list struct {
		Requests []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Groups []struct {
				Tools []string `json:"tools"`
			} `json:"groups"`
		} `json:"requests"`
	}
	if err := json.Unmarshal([]byte(stdout), &list); code != 0 || err != nil || len(list.Requests) != 1 || list.Requests[0].Kind != "remove-tools" {
		t.Fatalf("requests exit=%d stdout=%q stderr=%q err=%v", code, stdout, stderr, err)
	}
	var tools []string
	for _, g := range list.Requests[0].Groups {
		tools = append(tools, g.Tools...)
	}
	if strings.Join(tools, " ") != "bookstack.books.list bookstack.pages.get" {
		t.Errorf("tools of the request = %v", tools)
	}

	before, _ := os.ReadFile(configPath)
	for _, verb := range []string{"approve", "decline"} {
		if code, _, stderr := c.run(t, "requests", verb, list.Requests[0].ID); code != 2 || !strings.Contains(stderr, "admin-required") {
			t.Errorf("%s without a terminal: exit=%d stderr=%q", verb, code, stderr)
		}
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(before) {
		t.Error("a refused decision changed the configuration")
	}
}
