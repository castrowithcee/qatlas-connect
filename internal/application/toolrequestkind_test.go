package application

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
)

func TestRequestToolsToRemoveStoresLogsAndNeverLogsTheReason(t *testing.T) {
	core, queue, dir := requestCore(t, []string{"only"}, nil)
	const reason = "narrow this connection please"
	response, err := core.RequestTools(ToolRequest{Kind: "remove-tools", Tools: []string{"fake.pages.get"}, Reason: reason})
	if err != nil {
		t.Fatal(err)
	}
	if response.Kind != "remove-tools" || response.Status != "open" || strings.Join(response.Tools, ",") != "fake.pages.get" {
		t.Fatalf("response = %+v", response)
	}
	entries := logEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("log = %+v, want one entry", entries)
	}
	e := entries[0]
	if e.Operation != invokelog.OperationRequestRemoveCreate || e.Tool != "fake.pages.get" || e.Connection != "only" ||
		e.ConnectionID != core.all.Connections["only"].ID || e.Effect != "create" || e.Result != "success" {
		t.Fatalf("entry = %+v", e)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "logs", "*"))
	for _, name := range files {
		if content, _ := os.ReadFile(name); strings.Contains(string(content), reason) {
			t.Fatalf("%s holds the reason", name)
		}
	}

	// The default kind of a second request is a request of its own, not a merge.
	added, err := core.RequestTools(ToolRequest{Tools: []string{"fake.pages.delete"}})
	if err != nil || added.Kind != "add-tools" || added.ID == response.ID {
		t.Fatalf("add = %+v, %v", added, err)
	}
	open, err := core.OpenToolRequests()
	if err != nil || len(open) != 2 {
		t.Fatalf("open = %+v, %v (queue %s)", open, err, queue.Path())
	}
	kinds := map[string]string{}
	for _, r := range open {
		kinds[r.Kind] = strings.Join(r.Tools, ",")
	}
	if kinds["remove-tools"] != "fake.pages.get" || kinds["add-tools"] != "fake.pages.delete" {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestRequestKindRefusalsStoreNothing(t *testing.T) {
	core, queue, dir := requestCore(t, []string{"only"}, nil)
	for name, request := range map[string]ToolRequest{
		"unknown kind":            {Kind: "drop-tools", Tools: []string{"fake.pages.get"}},
		"remove of unoffered":     {Kind: "remove-tools", Tools: []string{"fake.pages.delete"}},
		"add of offered":          {Kind: "add-tools", Tools: []string{"fake.pages.get"}},
		"remove of unknown":       {Kind: "remove-tools", Tools: []string{"nope.x.y"}},
		"remove of another owner": {Kind: "remove-tools", Tools: []string{"fake.pages.get", "other.a.b"}},
	} {
		if _, err := core.RequestTools(request); ErrorCode(err) != "invalid-request" {
			t.Errorf("%s: RequestTools = %v, want invalid-request", name, err)
		}
	}
	for _, arguments := range []string{`{"kind":"drop-tools","tools":["fake.pages.get"]}`, `{"kind":5,"tools":["fake.pages.get"]}`,
		`{"kind":null,"tools":["fake.pages.get"]}`, `{"kind":"remove-tools","tools":["fake.pages.delete"]}`} {
		_, err := core.Invoke(context.Background(), InvokeRequest{Operation: OperationRequest, Arguments: json.RawMessage(arguments)})
		var invalid *InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("arguments %s: err = %v, want invalid-request", arguments, err)
		}
	}
	queueFileMissing(t, queue)
	if entries := logEntries(t, dir); len(entries) != 0 {
		t.Fatalf("log = %+v after refusals", entries)
	}
}

func TestRequestToRemoveRefusesADisabledConnection(t *testing.T) {
	core, queue, _ := requestCore(t, []string{"off"}, nil)
	disable(core, "off")
	_, err := core.RequestTools(ToolRequest{Kind: "remove-tools", Connection: "off", Tools: []string{"fake.pages.get"}})
	refusalOf(t, err, "off")
	if ErrorCode(err) != CodeUnsupportedCapability {
		t.Errorf("code %s", ErrorCode(err))
	}
	queueFileMissing(t, queue)
}

func TestRequestToRemoveFollowsTheProjectPathBinding(t *testing.T) {
	l := newBoundLayout(t)
	core, queue, _ := requestCore(t, []string{"bound"}, map[string][]string{"bound": {l.kundeA}})
	core.SetProjects([]string{l.kundeB})
	_, err := core.RequestTools(ToolRequest{Kind: "remove-tools", Connection: "bound", Tools: []string{"fake.pages.get"}})
	if ErrorCode(err) != CodeUnknownConnection {
		t.Fatalf("outside the paths: %v", err)
	}
	queueFileMissing(t, queue)
	core.SetProjects([]string{l.kundeA})
	if response, err := core.RequestTools(ToolRequest{Kind: "remove-tools", Connection: "bound", Tools: []string{"fake.pages.get"}}); err != nil || response.Connection != "bound" {
		t.Fatalf("inside the paths: %+v, %v", response, err)
	}
}

func TestRequestToRemoveReasonIsCleaned(t *testing.T) {
	core, _, _ := requestCore(t, []string{"only"}, nil)
	const canary = "synthetic-value-for-the-redactor"
	core.redactor.Add(canary)
	reason := "one\ntwo \x1b[31m" + canary + strings.Repeat("x", 800)
	if _, err := core.RequestTools(ToolRequest{Kind: "remove-tools", Tools: []string{"fake.pages.get"}, Reason: reason}); err != nil {
		t.Fatal(err)
	}
	open, err := core.OpenToolRequests()
	if err != nil || len(open) != 1 {
		t.Fatalf("open = %+v, %v", open, err)
	}
	got := open[0].Reason
	if len([]rune(got)) > accessrequest.MaxReasonRunes || strings.ContainsAny(got, "\n\x1b") || strings.Contains(got, canary) {
		t.Fatalf("reason = %q", got)
	}
}

func TestRequestToRemoveDropIsLoggedWithItsKind(t *testing.T) {
	core, _, dir := requestCore(t, []string{"only"}, nil)
	if _, err := core.RequestTools(ToolRequest{Kind: "remove-tools", Tools: []string{"fake.pages.get"}}); err != nil {
		t.Fatal(err)
	}
	conn := core.all.Connections["only"]
	conn.Tools = []string{"fake.pages.list"}
	core.all.Connections["only"] = conn
	if open, err := core.OpenToolRequests(); err != nil || len(open) != 0 {
		t.Fatalf("open = %+v, %v", open, err)
	}
	entries := logEntries(t, dir)
	if len(entries) != 2 || entries[1].Operation != invokelog.OperationRequestRemoveDrop || entries[1].Tool != "fake.pages.get" ||
		entries[1].Effect != "delete" || entries[1].ConnectionID == "" {
		t.Fatalf("log = %+v", entries)
	}
}

func TestInvokeRequestOperationWithKind(t *testing.T) {
	core, _, _ := requestCore(t, []string{"only"}, nil)
	response, err := core.Invoke(context.Background(), InvokeRequest{
		Operation: OperationRequest, Arguments: json.RawMessage(`{"kind":"remove-tools","tools":["fake.pages.get"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var result ToolRequestResponse
	if err := json.Unmarshal(response.Result, &result); err != nil || result.Kind != "remove-tools" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if description, err := core.Describe(DescribeRequest{Operation: OperationRequest}); err != nil ||
		!strings.Contains(string(description.Operation.InputSchema), `"remove-tools"`) ||
		!strings.Contains(string(description.Operation.OutputSchema), `"kind"`) || len(description.Operation.Examples) < 2 {
		t.Fatalf("describe = %+v, %v", description, err)
	}
}
