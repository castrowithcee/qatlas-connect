package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
	"github.com/castrowithcee/qatlas-kit/output"
)

const (
	requestsEffectsID = "0123456789abcdef0123456789abcdef"
	requestsListedID  = "fedcba9876543210fedcba9876543210"
)

// requestsFixture writes a configuration with a connection by effect and one with a tools list, and one open
// request for each. It returns the configuration path and the request IDs.
func requestsFixture(t *testing.T) (cfgPath, effectsRequest, listedRequest string) {
	t.Helper()
	cfgPath = writeConfig(t, `version: 4
services:
  wiki:
    provider: bookstack
    base_url: https://wiki.example.test
credentials:
  reader:
    type: env
    values:
      token-id: REQUESTS_TOKEN_ID
      token-secret: REQUESTS_TOKEN_SECRET
connections:
  effects:
    id: `+requestsEffectsID+`
    service: wiki
    credential: reader
    effects: [read]
  listed:
    id: `+requestsListedID+`
    service: wiki
    credential: reader
    tools: [bookstack.pages.list]
`)
	reg := sharedRegistry()
	cfg, err := config.Load(cfgPath, reg)
	if err != nil {
		t.Fatal(err)
	}
	src, err := manage.NewRequestSource(cfgPath, reg)
	if err != nil {
		t.Fatal(err)
	}
	env := accessrequest.Env{Config: cfg, Tools: src.Tools, Now: time.Now()}
	add := func(name, reason string, tools ...string) string {
		out, err := src.Queue.Add(env, accessrequest.Input{
			Connection: name, ConnectionID: cfg.Connections[name].ID, Tools: tools, Reason: reason,
			Project: "synthetic-project",
		})
		if err != nil {
			t.Fatal(err)
		}
		return out.Request.ID
	}
	effectsRequest = add("effects", "edit \x1b[31mpages\x1b[0m", "bookstack.pages.update", "bookstack.comments.create")
	listedRequest = add("listed", "", "bookstack.pages.update", "bookstack.pages.create")
	return cfgPath, effectsRequest, listedRequest
}

func TestRequestsListsGroupedToolsAndCleansTheReason(t *testing.T) {
	cfgPath, effects, _ := requestsFixture(t)
	withInteractive(t, false) // listing needs no terminal

	code, stdout, stderr := runWithInput(t, &Options{}, "", "requests", "--config", cfgPath, "--output", "json")
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	var doc requestListDocument
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("%q: %v", stdout, err)
	}
	if len(doc.Requests) != 2 {
		t.Fatalf("requests = %+v, want two", doc.Requests)
	}
	for _, r := range doc.Requests {
		if r.ID != effects {
			continue
		}
		if strings.ContainsRune(r.Reason, 0x1b) || !strings.Contains(r.Reason, "pages") {
			t.Errorf("reason = %q, want it cleaned and kept", r.Reason)
		}
		if len(r.Groups) != 2 || r.Groups[0].Title == "" || r.Groups[0].Tools == nil || r.Project != "synthetic-project" {
			t.Errorf("request = %+v, want two titled groups and the project", r)
		}
	}
	code, stdout, _ = runWithInput(t, &Options{}, "", "requests", "--config", cfgPath)
	if code != exitOK || !strings.Contains(stdout, "requests[2]") {
		t.Errorf("toon: exit %d, stdout %q", code, stdout)
	}
	code, _, stderr = runWithInput(t, &Options{}, "", "requests", "--config", cfgPath, "--output", "compact")
	if code != exitUsage {
		t.Errorf("compact: exit %d, stderr %q, want a usage error", code, stderr)
	}
}

func TestRequestsListIsEmptyWithoutAQueue(t *testing.T) {
	cfgPath := writeConfig(t, "version: 4\n")
	code, stdout, stderr := runWithInput(t, &Options{}, "", "requests", "--config", cfgPath, "--output", "json")
	if code != exitOK || strings.TrimSpace(stdout) != `{"requests":[]}` {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestRequestsDecisionsNeedATerminalBeforeAnything(t *testing.T) {
	cfgPath, effects, listed := requestsFixture(t)
	queuePath := accessrequest.PathFor(cfgPath)
	cfgBefore, _ := os.ReadFile(cfgPath)
	queueBefore, _ := os.ReadFile(queuePath)
	withInteractive(t, false)

	for _, args := range [][]string{
		{"approve", listed}, {"approve", effects}, {"decline", listed}, {"block", listed},
		{"approve", "unknown-id", "--as", "nope"},
	} {
		code, stdout, stderr := runWithInput(t, &Options{}, "", append(append([]string{"requests"}, args...),
			"--config", cfgPath)...)
		if code != exitUsage || stdout != "" || !strings.Contains(stderr, "admin-required") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
	cfgAfter, _ := os.ReadFile(cfgPath)
	queueAfter, _ := os.ReadFile(queuePath)
	if string(cfgBefore) != string(cfgAfter) || string(queueBefore) != string(queueAfter) {
		t.Error("a refused decision changed a file")
	}
}

func TestRequestsApproveNeedsAChoiceOnAConnectionByEffect(t *testing.T) {
	cfgPath, effects, listed := requestsFixture(t)
	cfgBefore, _ := os.ReadFile(cfgPath)

	code, stdout, stderr := runWithInput(t, &Options{}, "", "requests", "approve", effects, "--config", cfgPath)
	for _, want := range []string{"invalid-request", "--as effect adds ", "update (", "--as tools switches the connection to a tools list of "} {
		if code != exitUsage || stdout != "" || !strings.Contains(stderr, want) {
			t.Errorf("exit %d, stdout %q, stderr %q, want %q", code, stdout, stderr, want)
		}
	}
	code, _, stderr = runWithInput(t, &Options{}, "", "requests", "approve", listed, "--as", "tools", "--config", cfgPath)
	if code != exitUsage || !strings.Contains(stderr, "invalid-request") || !strings.Contains(stderr, "tools list") {
		t.Errorf("--as on a tools list: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = runWithInput(t, &Options{}, "", "requests", "approve", effects, "--as", "both", "--config", cfgPath)
	if code != exitUsage || !strings.Contains(stderr, "effect or tools") {
		t.Errorf("--as both: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = runWithInput(t, &Options{}, "", "requests", "approve", "0000", "--config", cfgPath)
	if code != exitUsage || !strings.Contains(stderr, "invalid-request") || !strings.Contains(stderr, "'qconnect requests'") {
		t.Errorf("unknown request: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = runWithInput(t, &Options{}, "", "requests", "decline", effects, "--tool", "bookstack.pages.get",
		"--config", cfgPath)
	if code != exitUsage || !strings.Contains(stderr, "not part of this request") {
		t.Errorf("foreign tool: exit %d, stderr %q", code, stderr)
	}
	if cfgAfter, _ := os.ReadFile(cfgPath); string(cfgBefore) != string(cfgAfter) {
		t.Error("a refused decision changed the configuration")
	}
}

func TestRequestsDecisionsReportEveryTool(t *testing.T) {
	cfgPath, effects, listed := requestsFixture(t)
	run := func(args ...string) requestDecisionDocument {
		t.Helper()
		code, stdout, stderr := runWithInput(t, &Options{}, "", append(append([]string{"requests"}, args...),
			"--config", cfgPath, "--output", "json")...)
		if code != exitOK {
			t.Fatalf("%v: exit %d, stderr %q", args, code, stderr)
		}
		var doc requestDecisionDocument
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
			t.Fatalf("%q: %v", stdout, err)
		}
		return doc
	}

	doc := run("approve", listed, "--tool", "bookstack.pages.update")
	if len(doc.Tools) != 1 || doc.Tools[0] != (requestToolResult{"bookstack.pages.update", "accepted"}) ||
		len(doc.Remaining) != 1 || doc.Remaining[0] != "bookstack.pages.create" || doc.Approval.State != "not-needed" {
		t.Errorf("approve some = %+v", doc)
	}
	doc = run("block", listed)
	if len(doc.Tools) != 1 || doc.Tools[0].Result != "blocked" || len(doc.Remaining) != 0 {
		t.Errorf("block = %+v", doc)
	}
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "blocked_tools:") || !strings.Contains(string(data), "bookstack.pages.create") {
		t.Errorf("blocked tools not saved:\n%s", data)
	}
	doc = run("approve", effects, "--as", "effect", "--tool", "bookstack.pages.update")
	if doc.Mode != "add-effects" || len(doc.Tools) != 1 || doc.Tools[0].Result != "accepted" ||
		len(doc.Remaining) != 1 {
		t.Errorf("approve by effect = %+v", doc)
	}
	doc = run("decline", effects)
	if len(doc.Tools) != 1 || doc.Tools[0].Result != "declined" || len(doc.Remaining) != 0 {
		t.Errorf("decline = %+v", doc)
	}
	code, stdout, _ := runWithInput(t, &Options{}, "", "requests", "--config", cfgPath, "--output", "json")
	if code != exitOK || strings.TrimSpace(stdout) != `{"requests":[]}` {
		t.Errorf("open after all decisions: %q", stdout)
	}
}

func TestRequestFailureMapsTheCodesOfTheCore(t *testing.T) {
	for _, tt := range []struct {
		code string
		want output.Code
	}{
		{manage.RequestCodeAdminRequired, output.CodeAdminRequired},
		{manage.RequestCodeNotFound, output.CodeInvalidRequest},
		{manage.RequestCodeInvalid, output.CodeInvalidRequest},
		{manage.RequestCodeNeedsList, output.CodeInvalidRequest},
	} {
		err := requestFailure(&manage.RequestDecisionError{Code: tt.code, Message: "refused"})
		var usage *UsageError
		if !errors.As(err, &usage) || codeFor(err) != tt.want {
			t.Errorf("%s: %v with code %s, want a usage error with %s", tt.code, err, codeFor(err), tt.want)
		}
	}
	err := requestFailure(&manage.RequestDecisionError{Code: manage.RequestCodeUnavailable, Message: "nope"})
	if codeFor(err) != output.CodeRuntime {
		t.Errorf("unavailable: code %s, want runtime", codeFor(err))
	}
}

func TestRequestSourceFollowsTheConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	src, err := manage.NewRequestSource(path, sharedRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if src.Queue.Path() != accessrequest.PathFor(path) || len(src.Tools) == 0 {
		t.Errorf("queue %q with %d tools", src.Queue.Path(), len(src.Tools))
	}
}
