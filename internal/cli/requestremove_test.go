package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
)

func TestRequestRemoveStoresARequestOfKindRemoveTools(t *testing.T) {
	registry, path := requestFixture(t, "")
	code, stdout, stderr := runRequestCLI(t, registry, path, "request", "--remove", "fake.pages.get", "--reason", requestReasonMarker)
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	for _, want := range []string{"kind: remove-tools", "status: open", "connection: primary", "fake.pages.get"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	entries, raw := readMCPLog(t, path)
	if len(entries) != 1 || entries[0].Operation != invokelog.OperationRequestRemoveCreate || entries[0].Tool != "fake.pages.get" ||
		entries[0].Path != "cli" || entries[0].Connection != "primary" {
		t.Fatalf("log = %+v", entries)
	}
	if strings.Contains(raw, requestReasonMarker) {
		t.Error("the reason reached the log")
	}
	if cfg, err := os.ReadFile(path); err != nil || strings.Contains(string(cfg), "fake.pages.get") {
		t.Errorf("the request changed the configuration: %v", err)
	}
}

func TestRequestRemoveRefusesAToolTheConnectionDoesNotOffer(t *testing.T) {
	registry, path := requestFixture(t, "")
	code, _, stderr := runRequestCLI(t, registry, path, "request", "--remove", "fake.pages.delete")
	if code != exitUsage || !strings.Contains(stderr, "invalid-request") || !strings.Contains(stderr, "nothing to remove") {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
	code, _, stderr = runRequestCLI(t, registry, path, "request", "fake.pages.get")
	if code != exitUsage || !strings.Contains(stderr, "invalid-request") {
		t.Errorf("adding an offered tool: exit %d stderr %q", code, stderr)
	}
	if _, err := os.Stat(accessrequest.PathFor(path)); err == nil {
		t.Error("a refused request was stored")
	}
}

// removeRequestsFixture is a connection by effect and one with a tools list, each with a request to remove.
func removeRequestsFixture(t *testing.T) (cfgPath, effectsRequest, listedRequest string) {
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
    tools: [bookstack.pages.list, bookstack.pages.get]
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
	add := func(name, tool string) string {
		out, err := src.Queue.Add(env, accessrequest.Input{
			Kind: accessrequest.KindRemoveTools, Connection: name, ConnectionID: cfg.Connections[name].ID, Tools: []string{tool},
		})
		if err != nil {
			t.Fatal(err)
		}
		return out.Request.ID
	}
	return cfgPath, add("effects", "bookstack.pages.get"), add("listed", "bookstack.pages.get")
}

func TestRequestsShowsTheKindAndDecidesARequestToRemove(t *testing.T) {
	cfgPath, effects, listed := removeRequestsFixture(t)
	code, stdout, stderr := runWithInput(t, &Options{}, "", "requests", "--config", cfgPath, "--output", "json")
	var doc requestListDocument
	if code != exitOK || json.Unmarshal([]byte(stdout), &doc) != nil || len(doc.Requests) != 2 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for _, r := range doc.Requests {
		if r.Kind != "remove-tools" {
			t.Errorf("kind = %q", r.Kind)
		}
	}
	cfgBefore, _ := os.ReadFile(cfgPath)
	for _, args := range [][]string{{"block", listed}, {"block", effects}} {
		code, stdout, stderr := runWithInput(t, &Options{}, "", append(append([]string{"requests"}, args...), "--config", cfgPath)...)
		if code != exitUsage || stdout != "" || !strings.Contains(stderr, "invalid-request") || !strings.Contains(stderr, "not blocked") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
	code, _, stderr = runWithInput(t, &Options{}, "", "requests", "approve", effects, "--config", cfgPath)
	if code != exitUsage || !strings.Contains(stderr, "--as tools") || !strings.Contains(stderr, "tools list of ") {
		t.Errorf("approve on effects: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = runWithInput(t, &Options{}, "", "requests", "approve", effects, "--as", "effect", "--config", cfgPath)
	if code != exitUsage || !strings.Contains(stderr, "--as tools") {
		t.Errorf("--as effect: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = runWithInput(t, &Options{}, "", "requests", "approve", listed, "--as", "tools", "--config", cfgPath)
	if code != exitUsage || !strings.Contains(stderr, "does not apply") {
		t.Errorf("--as on a list: exit %d, stderr %q", code, stderr)
	}
	if cfgAfter, _ := os.ReadFile(cfgPath); string(cfgBefore) != string(cfgAfter) {
		t.Error("a refused decision changed the configuration")
	}

	run := func(args ...string) requestDecisionDocument {
		t.Helper()
		code, stdout, stderr := runWithInput(t, &Options{}, "", append(append([]string{"requests"}, args...),
			"--config", cfgPath, "--output", "json")...)
		var d requestDecisionDocument
		if code != exitOK || json.Unmarshal([]byte(stdout), &d) != nil {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
		return d
	}
	if d := run("approve", listed); len(d.Tools) != 1 || d.Tools[0] != (requestToolResult{"bookstack.pages.get", "accepted"}) {
		t.Errorf("approve on a list = %+v", d)
	}
	if d := run("approve", effects, "--as", "tools"); d.Mode != "switch-to-list" || d.Tools[0].Result != "accepted" {
		t.Errorf("approve on effects = %+v", d)
	}
	data, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(data), "bookstack.pages.get") || strings.Contains(string(data), "effects: [read]") {
		t.Errorf("configuration after approving:\n%s", data)
	}
}

func TestRequestsDeclineOfARequestToRemoveChangesNothing(t *testing.T) {
	cfgPath, _, listed := removeRequestsFixture(t)
	before, _ := os.ReadFile(cfgPath)
	code, stdout, stderr := runWithInput(t, &Options{}, "", "requests", "decline", listed, "--config", cfgPath, "--output", "json")
	var d requestDecisionDocument
	if code != exitOK || json.Unmarshal([]byte(stdout), &d) != nil || d.Tools[0].Result != "declined" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if after, _ := os.ReadFile(cfgPath); string(before) != string(after) {
		t.Error("declining changed the configuration")
	}
}
