package application

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
)

type targetCatalog struct{}

func (targetCatalog) ProviderMetadata(id string) (config.ProviderMetadata, bool) {
	return targetCatalog{}.meta(), id == "fake"
}
func (targetCatalog) ProviderMetadataAll() []config.ProviderMetadata {
	return []config.ProviderMetadata{targetCatalog{}.meta()}
}
func (targetCatalog) meta() config.ProviderMetadata {
	return config.ProviderMetadata{ID: "fake", Target: config.TargetMetadata{
		Label: "space", Multiple: true,
		Kinds: []config.TargetKind{{Name: "space", Forms: []string{"spaces/KEY"}}},
	}}
}

// createCore is a request core whose configuration knows the targets of the provider.
func createCore(t *testing.T) (*Core, *accessrequest.Queue, string) {
	t.Helper()
	core, queue, dir := requestCore(t, []string{"only"}, nil)
	cfg := config.New(targetCatalog{})
	cfg.Services, cfg.Credentials, cfg.Connections = core.all.Services, core.all.Credentials, core.all.Connections
	core.all, core.config = cfg, cfg
	return core, queue, dir
}

func invokeRequestOf(core *Core, connection, arguments string) (InvokeResponse, error) {
	return core.Invoke(context.Background(), InvokeRequest{
		Operation: OperationRequest, Connection: connection, Arguments: json.RawMessage(arguments),
	})
}

const createArguments = `{"kind":"create-connection","provider":"fake","name":"wiki","targets":["spaces/ENG"],` +
	`"tools":["fake.pages.delete","fake.pages.list"],"reason":"The task cleans pages"}`

func TestRequestCreateConnectionStoresAndLogsAndChangesNothing(t *testing.T) {
	core, queue, dir := createCore(t)
	before := core.all.Clone()
	response, err := invokeRequestOf(core, "", createArguments)
	if err != nil {
		t.Fatal(err)
	}
	var result ToolRequestResponse
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if response.Connection != "" || result.Kind != "create-connection" || result.Connection != "wiki" || result.Provider != "fake" ||
		result.Status != "open" || strings.Join(result.Targets, ",") != "spaces/ENG" ||
		strings.Join(result.Tools, ",") != "fake.pages.delete,fake.pages.list" ||
		!strings.Contains(result.Message, "open") || !strings.Contains(result.Message, "Stop") {
		t.Fatalf("response = %+v, result = %+v", response, result)
	}
	entries := logEntries(t, dir)
	if len(entries) != 2 {
		t.Fatalf("log = %+v, want one entry per tool", entries)
	}
	for i, tool := range []string{"fake.pages.delete", "fake.pages.list"} {
		e := entries[i]
		if e.Operation != invokelog.OperationRequestConnectionCreate || e.Connection != "wiki" || e.ConnectionID != "" ||
			e.Tool != tool || e.Effect != "create" || e.Result != "success" {
			t.Errorf("entry %d = %+v", i, e)
		}
	}
	// A request never creates a connection nor touches the configuration.
	if _, ok := core.all.Connections["wiki"]; ok || len(core.all.Connections) != len(before.Connections) {
		t.Error("a request created a connection")
	}
	data, _ := os.ReadFile(queue.Path())
	if strings.Contains(string(data), "cleans pages") == false {
		t.Errorf("the reason is missing from the queue:\n%s", data)
	}
	open, err := core.OpenToolRequests()
	if err != nil || len(open) != 1 || open[0].Kind != "create-connection" || open[0].Connection != "wiki" ||
		open[0].ConnectionID != "" || strings.Join(open[0].Targets, ",") != "spaces/ENG" || open[0].Reason != "The task cleans pages" {
		t.Fatalf("open = %+v, %v", open, err)
	}

	// A repeated request merges and logs only the new tool.
	if _, err := invokeRequestOf(core, "", `{"kind":"create-connection","provider":"fake","name":"wiki","tools":["fake.pages.get"]}`); err != nil {
		t.Fatal(err)
	}
	if entries := logEntries(t, dir); len(entries) != 3 || entries[2].Tool != "fake.pages.get" {
		t.Fatalf("log = %+v", entries)
	}
	if open, _ := core.OpenToolRequests(); len(open) != 1 || len(open[0].Tools) != 3 {
		t.Fatalf("open = %+v", open)
	}
}

func TestRequestCreateConnectionLeavesTheConfigFileAlone(t *testing.T) {
	core, _, dir := createCore(t)
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	statBefore, _ := os.Stat(path)
	if _, err := invokeRequestOf(core, "", createArguments); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	statAfter, _ := os.Stat(path)
	if string(before) != string(after) || !statBefore.ModTime().Equal(statAfter.ModTime()) {
		t.Error("a request changed config.yaml")
	}
}

func TestRequestCreateConnectionContract(t *testing.T) {
	core, queue, dir := createCore(t)
	for name, arguments := range map[string]string{
		"credential":       `{"kind":"create-connection","provider":"fake","name":"wiki","tools":["fake.pages.get"],"credential":"c"}`,
		"token":            `{"kind":"create-connection","provider":"fake","name":"wiki","tools":["fake.pages.get"],"token":"t"}`,
		"url":              `{"kind":"create-connection","provider":"fake","name":"wiki","tools":["fake.pages.get"],"url":"https://example.invalid"}`,
		"service":          `{"kind":"create-connection","provider":"fake","name":"wiki","tools":["fake.pages.get"],"service":"s"}`,
		"unknown":          `{"kind":"create-connection","provider":"fake","name":"wiki","tools":["fake.pages.get"],"extra":1}`,
		"targets type":     `{"kind":"create-connection","provider":"fake","name":"wiki","tools":["fake.pages.get"],"targets":"spaces/ENG"}`,
		"unknown kind":     `{"kind":"other","provider":"fake","name":"wiki","tools":["fake.pages.get"]}`,
		"without name":     `{"kind":"create-connection","provider":"fake","tools":["fake.pages.get"]}`,
		"without provider": `{"kind":"create-connection","name":"wiki","tools":["fake.pages.get"]}`,
		"name taken":       `{"kind":"create-connection","provider":"fake","name":"only","tools":["fake.pages.get"]}`,
		"bad target":       `{"kind":"create-connection","provider":"fake","name":"wiki","targets":["nowhere/x"],"tools":["fake.pages.get"]}`,
	} {
		_, err := invokeRequestOf(core, "", arguments)
		var invalid *InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want invalid-request", name, err)
		}
	}
	// A connection on the call does not apply.
	_, err := invokeRequestOf(core, "only", createArguments)
	var invalid *InvalidRequestError
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "connection") {
		t.Errorf("connection on the call: err = %v", err)
	}
	queueFileMissing(t, queue)
	if entries := logEntries(t, dir); len(entries) != 0 {
		t.Errorf("log = %+v, want none", entries)
	}
}

func TestRequestKindsToolsRefuseTheFieldsOfCreate(t *testing.T) {
	core, queue, _ := createCore(t)
	for _, arguments := range []string{
		`{"tools":["fake.pages.delete"],"provider":"fake"}`,
		`{"kind":"add-tools","tools":["fake.pages.delete"],"name":"wiki"}`,
		`{"kind":"remove-tools","tools":["fake.pages.get"],"targets":["spaces/ENG"]}`,
	} {
		_, err := invokeRequestOf(core, "", arguments)
		var invalid *InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want invalid-request", arguments, err)
		}
	}
	queueFileMissing(t, queue)
	// The existing kinds work as before.
	if _, err := invokeRequestOf(core, "", `{"tools":["fake.pages.delete"]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := invokeRequestOf(core, "", `{"kind":"remove-tools","tools":["fake.pages.get"]}`); err != nil {
		t.Fatal(err)
	}
}

func TestRequestCreateConnectionRefusesASecretAndNeverLogsTheReason(t *testing.T) {
	core, queue, dir := createCore(t)
	const canary = "synthetic-value-for-the-redactor"
	core.redactor.Add(canary)
	for name, arguments := range map[string]string{
		"reason": `{"kind":"create-connection","provider":"fake","name":"wiki","tools":["fake.pages.get"],"reason":"use ` + canary + `"}`,
		"name":   `{"kind":"create-connection","provider":"fake","name":"` + canary + `","tools":["fake.pages.get"]}`,
		"target": `{"kind":"create-connection","provider":"fake","name":"wiki","targets":["spaces/` + canary + `"],"tools":["fake.pages.get"]}`,
	} {
		_, err := invokeRequestOf(core, "", arguments)
		var invalid *InvalidRequestError
		if !errors.As(err, &invalid) || strings.Contains(err.Error(), canary) {
			t.Errorf("%s: err = %v, want invalid-request without the value", name, err)
		}
	}
	queueFileMissing(t, queue)

	if _, err := invokeRequestOf(core, "", `{"kind":"create-connection","provider":"fake","name":"wiki","tools":["fake.pages.get"],`+
		`"reason":"line one\u001b[31m\nline two"}`); err != nil {
		t.Fatal(err)
	}
	open, _ := core.OpenToolRequests()
	if len(open) != 1 || strings.ContainsAny(open[0].Reason, "\x1b\n") || open[0].Reason == "" {
		t.Fatalf("reason = %q, want it neutralized", open[0].Reason)
	}
	for _, entry := range logEntries(t, dir) {
		raw, _ := json.Marshal(entry)
		if strings.Contains(string(raw), "line one") || strings.Contains(string(raw), canary) {
			t.Errorf("entry %s holds the reason", raw)
		}
	}
}

func TestDescribeRequestOperationNamesTheCreateKind(t *testing.T) {
	core, _, _ := createCore(t)
	response, err := core.Describe(DescribeRequest{Operation: OperationRequest})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response.Operation)
	for _, want := range []string{"create-connection", `"provider":"example"`, `"name"`, `"targets"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("describe lacks %s", want)
		}
	}
}
