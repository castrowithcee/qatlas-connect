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

func TestRequestCreateConnectionStoresAnOpenRequestWithoutCredentials(t *testing.T) {
	registry, path := requestFixture(t, "")
	cfgBefore, _ := os.ReadFile(path)
	code, stdout, stderr := runRequestCLI(t, registry, path, "request", "--create-connection", "--provider", "fake",
		"--name", "fake-admin", "--reason", requestReasonMarker, "fake.pages.delete", "fake.pages.get")
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	for _, want := range []string{"kind: create-connection", "status: open", "connection: fake-admin", "provider: fake",
		"fake.pages.delete", "nothing was created"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	entries, raw := readMCPLog(t, path)
	if len(entries) != 2 || entries[0].Operation != invokelog.OperationRequestConnectionCreate || entries[0].Path != "cli" ||
		entries[0].Connection != "fake-admin" {
		t.Fatalf("log = %+v", entries)
	}
	if strings.Contains(raw, requestReasonMarker) {
		t.Error("the reason reached the log")
	}
	if cfgAfter, _ := os.ReadFile(path); string(cfgBefore) != string(cfgAfter) {
		t.Error("the request changed the configuration")
	}

	for name, args := range map[string][]string{
		"with --connection":     {"request", "--create-connection", "--provider", "fake", "--name", "x", "--connection", "primary", "fake.pages.get"},
		"with --remove":         {"request", "--create-connection", "--remove", "--provider", "fake", "--name", "x", "fake.pages.get"},
		"name taken":            {"request", "--create-connection", "--provider", "fake", "--name", "primary", "fake.pages.get"},
		"no provider":           {"request", "--create-connection", "--name", "x", "fake.pages.get"},
		"provider without kind": {"request", "--provider", "fake", "fake.pages.delete"},
		"unknown flag":          {"request", "--create-connection", "--credential", "x", "fake.pages.get"},
	} {
		if code, _, stderr := runRequestCLI(t, registry, path, args...); code != exitUsage {
			t.Errorf("%s: exit %d stderr %q, want a usage error", name, code, stderr)
		}
	}
}

// createRequestsFixture adds an open request to create a connection to the requests fixture.
func createRequestsFixture(t *testing.T) (cfgPath, id string) {
	t.Helper()
	cfgPath, _, _ = requestsFixture(t)
	reg := sharedRegistry()
	cfg, err := config.Load(cfgPath, reg)
	if err != nil {
		t.Fatal(err)
	}
	src, err := manage.NewRequestSource(cfgPath, reg)
	if err != nil {
		t.Fatal(err)
	}
	out, err := src.Queue.Add(accessrequest.Env{Config: cfg, Tools: src.Tools, Now: time.Now()}, accessrequest.Input{
		Kind: accessrequest.KindCreateConnection, Provider: "bookstack", Connection: "wiki-admin",
		Tools: []string{"bookstack.pages.get", "bookstack.pages.update"}, Reason: "synthetic reason", ReasonRaw: "synthetic reason",
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfgPath, out.Request.ID
}

func TestRequestsListsDeclinesAndRefusesToApproveACreateRequest(t *testing.T) {
	cfgPath, id := createRequestsFixture(t)
	queuePath := accessrequest.PathFor(cfgPath)
	code, stdout, stderr := runWithInput(t, &Options{}, "", "requests", "--config", cfgPath, "--output", "json")
	var list requestListDocument
	if code != exitOK || json.Unmarshal([]byte(stdout), &list) != nil {
		t.Fatalf("list: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	var found *requestDocument
	for i := range list.Requests {
		if list.Requests[i].ID == id {
			found = &list.Requests[i]
		}
	}
	if found == nil || found.Kind != "create-connection" || found.Connection != "wiki-admin" || found.Provider != "bookstack" ||
		len(found.Groups) == 0 {
		t.Fatalf("request = %+v", found)
	}
	if code, text, _ := runWithInput(t, &Options{}, "", "requests", "--config", cfgPath); code != exitOK ||
		!strings.Contains(text, "create-connection") || !strings.Contains(text, "wiki-admin") {
		t.Errorf("text list: exit %d, %q", code, text)
	}

	queueBefore, _ := os.ReadFile(queuePath)
	for _, verb := range []string{"approve", "block"} {
		code, stdout, stderr := runWithInput(t, &Options{}, "", "requests", verb, id, "--config", cfgPath)
		if code != exitUsage || stdout != "" || !strings.Contains(stderr, "invalid-request") ||
			!strings.Contains(stderr, "qconnect tui") {
			t.Errorf("%s: exit %d stdout %q stderr %q", verb, code, stdout, stderr)
		}
	}
	if queueAfter, _ := os.ReadFile(queuePath); string(queueBefore) != string(queueAfter) {
		t.Error("a refused decision changed the queue")
	}

	code, stdout, stderr = runWithInput(t, &Options{}, "", "requests", "decline", id, "--config", cfgPath, "--output", "json")
	var doc requestDecisionDocument
	if code != exitOK || json.Unmarshal([]byte(stdout), &doc) != nil || doc.Connection != "wiki-admin" || len(doc.Tools) != 2 ||
		doc.Tools[0].Result != "declined" {
		t.Fatalf("decline: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	code, stdout, _ = runWithInput(t, &Options{}, "", "requests", "--config", cfgPath, "--output", "json")
	if strings.Contains(stdout, id) {
		t.Errorf("the declined request is still open: %q", stdout)
	}
}
