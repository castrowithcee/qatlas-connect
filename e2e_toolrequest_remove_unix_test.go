//go:build linux || darwin

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestToolRemovalAcceptedAtATerminal accepts a request to remove tools with a person at a terminal: the
// connection loses exactly the requested tool and keeps the others.
func TestToolRemovalAcceptedAtATerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("the acceptance run builds the binary")
	}
	c, dir := newRemovalRunner(t)
	configPath := filepath.Join(dir, "config.yaml")
	if code, stdout, stderr := c.run(t, "request", "--remove", "bookstack.pages.get"); code != 0 {
		t.Fatalf("request --remove exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, _ := c.run(t, "requests")
	if code != 0 || !strings.Contains(stdout, "remove-tools") {
		t.Fatalf("requests exit=%d stdout=%q", code, stdout)
	}
	id := requestIDIn(t, c)
	out, code := runAtPTY(t, c, "", "requests", "approve", id, "--output", "json")
	if code != 0 || !strings.Contains(out, `"result":"accepted"`) {
		t.Fatalf("approve: exit %d, output %q", code, out)
	}
	data, _ := os.ReadFile(configPath)
	if strings.Contains(string(data), "bookstack.pages.get") || !strings.Contains(string(data), "bookstack.pages.list") ||
		!strings.Contains(string(data), "bookstack.books.list") {
		t.Errorf("the connection after accepting:\n%s", data)
	}
	if code, stdout, _ := c.run(t, "requests", "--output", "json"); code != 0 || strings.Contains(stdout, "bookstack.pages.get") {
		t.Errorf("the request stays open: exit=%d stdout=%q", code, stdout)
	}
}

// requestIDIn is the ID of the one open request.
func requestIDIn(t *testing.T, c *runner) string {
	t.Helper()
	code, stdout, stderr := c.run(t, "requests", "--output", "json")
	var list struct {
		Requests []struct {
			ID string `json:"id"`
		} `json:"requests"`
	}
	if err := json.Unmarshal([]byte(stdout), &list); code != 0 || err != nil || len(list.Requests) != 1 {
		t.Fatalf("requests exit=%d stdout=%q stderr=%q err=%v", code, stdout, stderr, err)
	}
	return list.Requests[0].ID
}
