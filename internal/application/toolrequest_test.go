package application

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

var requestEpoch = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

// requestCore is a core over connections that offer only fake.pages.get by their tools list, with a queue in
// a temporary directory, a log, and a fixed clock. Connections named in paths are bound to them.
func requestCore(t *testing.T, names []string, paths map[string][]string) (*Core, *accessrequest.Queue, string) {
	t.Helper()
	core, _ := testCore(t, nil, nil, true)
	list := testDescriptor("fake.pages.list", capability.EffectRead, capability.ConfirmationNone)
	handler := capability.Handler(func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err := core.registry.Register("fake", capability.Operation{Descriptor: list, Handler: handler}); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		core.all.Services[name+"-service"] = config.Service{Provider: "fake", BaseURL: "https://example.invalid"}
		core.all.Credentials[name+"-credential"] = config.Credential{
			Type: config.CredentialTypeEnv, Values: map[string]string{"token": "FAKE_TOKEN"},
		}
		core.all.Connections[name] = config.Connection{
			Service: name + "-service", Credential: name + "-credential",
			Tools: []string{"fake.pages.get"}, Paths: paths[name],
		}
	}
	core.all.AssignIDs()
	dir := t.TempDir()
	queue, err := accessrequest.NewQueue(accessrequest.PathFor(filepath.Join(dir, "config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	core.SetRequestQueue(queue)
	core.SetInvokeLog(invokelog.New(dir, 90), "cli", nil)
	core.SetClock(func() time.Time { return requestEpoch })
	return core, queue, dir
}

func logEntries(t *testing.T, dir string) []invokelog.Entry {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "logs", "*.jsonl"))
	var entries []invokelog.Entry
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var entry invokelog.Entry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			entries = append(entries, entry)
		}
		_ = f.Close()
	}
	return entries
}

func queueFileMissing(t *testing.T, queue *accessrequest.Queue) {
	t.Helper()
	if _, err := os.Stat(queue.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the queue file exists: %v", err)
	}
}

func TestRequestToolsStoresAndLogsOneEntryPerTool(t *testing.T) {
	core, queue, dir := requestCore(t, []string{"only"}, nil)
	response, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.delete", "fake.pages.delete"}, Reason: "to clean up"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ID) != 32 || response.Connection != "only" || response.Status != "open" ||
		strings.Join(response.Tools, ",") != "fake.pages.delete" || !response.ExpiresAt.Equal(requestEpoch.Add(14*24*time.Hour)) {
		t.Fatalf("response = %+v", response)
	}
	info, err := os.Stat(queue.Path())
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) || filepath.Dir(queue.Path()) != dir {
		t.Fatalf("queue file = %v, %v", info, err)
	}
	entries := logEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("log = %+v, want one entry", entries)
	}
	e := entries[0]
	if e.Operation != invokelog.OperationRequestCreate || e.Tool != "fake.pages.delete" || e.Connection != "only" ||
		e.ConnectionID != core.all.Connections["only"].ID || e.Path != "cli" || e.Effect != "create" || e.Result != "success" {
		t.Fatalf("entry = %+v", e)
	}

	// A second call merges and logs only the new tool.
	again, err := core.RequestTools(ToolRequest{Connection: "only", Tools: []string{"fake.pages.delete", "fake.pages.*"}})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != response.ID || len(again.Tools) != 2 {
		t.Fatalf("merged = %+v", again)
	}
	if entries := logEntries(t, dir); len(entries) != 2 || entries[1].Tool != "fake.pages.*" {
		t.Fatalf("log = %+v", entries)
	}
	open, err := core.OpenToolRequests()
	if err != nil || len(open) != 1 || open[0].Reason != "" || open[0].ConnectionID == "" {
		t.Fatalf("open = %+v, %v", open, err)
	}
}

func TestRequestReasonIsCleanedAndNeverLogged(t *testing.T) {
	core, _, dir := requestCore(t, []string{"only"}, nil)
	const canary = "synthetic-value-for-the-redactor"
	core.redactor.Add(canary)
	reason := "line one\nline two \x1b[31m" + canary + strings.Repeat("x", 800)
	if _, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.delete"}, Reason: reason}); err != nil {
		t.Fatal(err)
	}
	open, err := core.OpenToolRequests()
	if err != nil || len(open) != 1 {
		t.Fatalf("open = %+v, %v", open, err)
	}
	got := open[0].Reason
	if len([]rune(got)) > accessrequest.MaxReasonRunes || strings.ContainsAny(got, "\n\x1b") ||
		strings.Contains(got, canary) || !strings.Contains(got, `\n`) || !strings.Contains(got, `\u001b`) {
		t.Fatalf("reason = %q", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, accessrequest.FileName))
	if err != nil || strings.Contains(string(data), canary) {
		t.Fatalf("queue file holds the secret: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "logs", "*"))
	for _, name := range files {
		content, _ := os.ReadFile(name)
		if strings.Contains(string(content), "line one") || strings.Contains(string(content), canary) {
			t.Fatalf("%s holds the reason", name)
		}
	}
}

func TestRequestRefusalsStoreNothingAndNameTheCode(t *testing.T) {
	core, queue, dir := requestCore(t, []string{"only"}, nil)
	conn := core.all.Connections["only"]
	conn.BlockedTools = []string{"fake.pages.delete"}
	core.all.Connections["only"] = conn

	_, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.get"}})
	var invalid *InvalidRequestError
	if !errors.As(err, &invalid) || ErrorCode(err) != "invalid-request" {
		t.Fatalf("offered tool: %v", err)
	}
	_, err = core.RequestTools(ToolRequest{Tools: []string{"fake.pages.delete"}, Reason: "secret reason"})
	var blocked *RequestBlockedError
	if !errors.As(err, &blocked) || ErrorCode(err) != CodeRequestBlocked || !strings.Contains(err.Error(), "fake.pages.delete") ||
		strings.Contains(err.Error(), "secret reason") {
		t.Fatalf("blocked tool: %v", err)
	}
	for _, request := range []ToolRequest{{}, {Tools: []string{"nope.x.y"}}, {Tools: []string{"fake.pages.get", "other.a.b"}}} {
		if _, err := core.RequestTools(request); ErrorCode(err) != "invalid-request" {
			t.Errorf("RequestTools(%+v) = %v, want invalid-request", request, err)
		}
	}
	queueFileMissing(t, queue)
	if entries := logEntries(t, dir); len(entries) != 0 {
		t.Fatalf("log = %+v after refusals", entries)
	}
}

func TestRequestResolvesTheConnectionLikeInvoke(t *testing.T) {
	core, queue, _ := requestCore(t, []string{"a", "b"}, nil)
	_, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.delete"}})
	var ambiguous *ConnectionAmbiguousError
	if !errors.As(err, &ambiguous) || len(ambiguous.Connections) != 2 {
		t.Fatalf("two candidates: %v", err)
	}
	if _, err := core.RequestTools(ToolRequest{Connection: "nothing", Tools: []string{"fake.pages.delete"}}); ErrorCode(err) != CodeUnknownConnection {
		t.Fatalf("unknown name: %v", err)
	}
	queueFileMissing(t, queue)

	byID, err := core.RequestTools(ToolRequest{Connection: "x|" + core.all.Connections["b"].ID, Tools: []string{"fake.pages.delete"}})
	if err != nil || byID.Connection != "b" {
		t.Fatalf("name|id: %+v, %v", byID, err)
	}
	core.all.Defaults.Connections = map[string]string{"fake": "a"}
	byDefault, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.list"}})
	if err != nil || byDefault.Connection != "a" {
		t.Fatalf("provider default: %+v, %v", byDefault, err)
	}
	core.all.Defaults.Connections = map[string]string{"fake": "a", "fake.pages.delete": "b"}
	byTool, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.delete"}})
	if err != nil || byTool.Connection != "b" {
		t.Fatalf("tool default: %+v, %v", byTool, err)
	}
}

func TestRequestFollowsTheProjectPathBinding(t *testing.T) {
	l := newBoundLayout(t)
	core, queue, _ := requestCore(t, []string{"bound", "free"}, map[string][]string{"bound": {l.kundeA}})
	core.SetProjects([]string{l.kundeB})

	_, err := core.RequestTools(ToolRequest{Connection: "bound", Tools: []string{"fake.pages.delete"}})
	if ErrorCode(err) != CodeUnknownConnection {
		t.Fatalf("outside the paths: %v", err)
	}
	queueFileMissing(t, queue)
	if _, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.delete"}}); err != nil {
		t.Fatalf("the free connection is the only candidate: %v", err)
	}
	core.SetProjects([]string{l.kundeA})
	response, err := core.RequestTools(ToolRequest{Connection: "bound", Tools: []string{"fake.pages.delete"}})
	if err != nil || response.Connection != "bound" {
		t.Fatalf("inside the paths: %+v, %v", response, err)
	}

	// A person sees every project's requests, the project path included.
	core.SetProjects([]string{l.kundeB})
	open, err := core.OpenToolRequests()
	if err != nil || len(open) != 2 {
		t.Fatalf("open = %+v, %v", open, err)
	}
	projects := map[string]string{}
	for _, r := range open {
		projects[r.Connection] = r.Project
	}
	if projects["bound"] != l.kundeA {
		t.Fatalf("projects = %v", projects)
	}
}

func TestRequestRefusesADisabledOrExpiredConnection(t *testing.T) {
	core, queue, _ := requestCore(t, []string{"off", "late"}, nil)
	disable(core, "off")
	late := core.all.Connections["late"]
	late.ActiveUntil = requestEpoch.Add(-time.Hour).Format(time.RFC3339)
	core.all.Connections["late"] = late
	for _, name := range []string{"off", "late"} {
		_, err := core.RequestTools(ToolRequest{Connection: name, Tools: []string{"fake.pages.delete"}})
		refusalOf(t, err, name)
		if ErrorCode(err) != CodeUnsupportedCapability {
			t.Errorf("%s: code %s", name, ErrorCode(err))
		}
	}
	queueFileMissing(t, queue)
}

func TestRequestExpiryDropAndRenameAreLogged(t *testing.T) {
	core, _, dir := requestCore(t, []string{"only", "second"}, nil)
	core.all.Defaults.Connections = map[string]string{"fake": "only"}
	if _, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.delete", "fake.pages.*"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.RequestTools(ToolRequest{Connection: "second", Tools: []string{"fake.pages.delete"}}); err != nil {
		t.Fatal(err)
	}
	core.all.Connections["renamed"] = core.all.Connections["second"]
	delete(core.all.Connections, "second")
	delete(core.all.Connections, "only")
	core.all.Defaults.Connections = nil

	open, err := core.OpenToolRequests()
	if err != nil || len(open) != 1 || open[0].Connection != "renamed" {
		t.Fatalf("open = %+v, %v", open, err)
	}
	drops := 0
	for _, e := range logEntries(t, dir) {
		if e.Operation == invokelog.OperationRequestDrop {
			drops++
			if e.Effect != "delete" || e.Connection != "only" {
				t.Errorf("drop entry = %+v", e)
			}
		}
	}
	if drops != 2 {
		t.Fatalf("drop entries = %d, want 2", drops)
	}

	core.SetClock(func() time.Time { return requestEpoch.Add(14 * 24 * time.Hour) })
	if open, err := core.OpenToolRequests(); err != nil || len(open) != 0 {
		t.Fatalf("after expiry: %+v, %v", open, err)
	}
	expired := 0
	for _, e := range logEntries(t, dir) {
		if e.Operation == invokelog.OperationRequestExpire && e.Connection == "renamed" {
			expired++
		}
	}
	if expired != 1 {
		t.Fatalf("expire entries = %d, want 1", expired)
	}
}

func TestRequestWithoutAQueueFails(t *testing.T) {
	core, _ := testCore(t, []string{"only"}, nil, true)
	if _, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.delete"}}); err == nil {
		t.Fatal("RequestTools without a queue succeeded")
	}
}
