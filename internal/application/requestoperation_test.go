package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
)

func TestInvokeRequestOperationStoresARequestWithoutAnInvokeEntry(t *testing.T) {
	core, queue, dir := requestCore(t, []string{"only"}, nil)
	response, err := core.Invoke(context.Background(), InvokeRequest{
		Operation: OperationRequest, Arguments: json.RawMessage(`{"tools":["fake.pages.delete"],"reason":"cleanup"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var result ToolRequestResponse
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if response.Operation != OperationRequest || response.Connection != "only" || len(result.ID) != 32 ||
		result.Status != "open" || strings.Join(result.Tools, ",") != "fake.pages.delete" || result.ExpiresAt.IsZero() {
		t.Fatalf("response = %+v, result = %+v", response, result)
	}
	for _, entry := range logEntries(t, dir) {
		if entry.Operation != invokelog.OperationRequestCreate {
			t.Errorf("entry %+v is no request entry", entry)
		}
	}
	if open, err := core.OpenToolRequests(); err != nil || len(open) != 1 {
		t.Fatalf("open = %+v, %v (queue %s)", open, err, queue.Path())
	}
}

func TestInvokeRequestOperationChecksItsContract(t *testing.T) {
	core, queue, dir := requestCore(t, []string{"only"}, nil)
	for _, arguments := range []string{`{}`, `{"tools":[]}`, `{"tools":"fake.pages.delete"}`,
		`{"tools":["fake.pages.delete"],"extra":1}`} {
		_, err := core.Invoke(context.Background(), InvokeRequest{Operation: OperationRequest, Arguments: json.RawMessage(arguments)})
		var invalid *InvalidRequestError
		if !errors.As(err, &invalid) {
			t.Errorf("arguments %s: err = %v, want invalid-request", arguments, err)
		}
	}
	// Tools the connection offers cannot be asked for.
	_, err := core.Invoke(context.Background(), InvokeRequest{
		Operation: OperationRequest, Arguments: json.RawMessage(`{"tools":["fake.pages.get"]}`),
	})
	var invalid *InvalidRequestError
	if !errors.As(err, &invalid) {
		t.Errorf("an offered tool: err = %v", err)
	}
	queueFileMissing(t, queue)
	if entries := logEntries(t, dir); len(entries) != 0 {
		t.Errorf("log = %+v, want no entry", entries)
	}
}

func TestBlockedToolEndsTheRequestOperationWithItsOwnError(t *testing.T) {
	core, _, _ := requestCore(t, []string{"only"}, nil)
	conn := core.all.Connections["only"]
	conn.BlockedTools = []string{"fake.pages.delete"}
	core.all.Connections["only"] = conn
	_, err := core.Invoke(context.Background(), InvokeRequest{
		Operation: OperationRequest, Arguments: json.RawMessage(`{"tools":["fake.pages.delete"]}`),
	})
	var blocked *RequestBlockedError
	if !errors.As(err, &blocked) || ErrorCode(err) != CodeRequestBlocked {
		t.Fatalf("err = %v, want request-blocked", err)
	}
}

func TestDescribeRequestOperationIsAFixedContract(t *testing.T) {
	core, _, _ := requestCore(t, []string{"only"}, nil)
	response, err := core.Describe(DescribeRequest{Operation: OperationRequest})
	if err != nil {
		t.Fatal(err)
	}
	compact := Compact(response.Operation)
	names := map[string]bool{}
	for _, row := range compact.Arguments {
		names[row.Name] = row.Required
	}
	if required, ok := names["tools"]; !ok || !required {
		t.Errorf("rows = %+v, want tools as required argument", compact.Arguments)
	}
	if _, ok := names["reason"]; !ok || len(names) != 2 {
		t.Errorf("rows = %+v, want tools and reason", compact.Arguments)
	}
	if response.Connections == nil {
		t.Error("connections is null, want an empty list")
	}
	if _, err := core.Describe(DescribeRequest{Operation: OperationRequest, Connection: "absent"}); err == nil {
		t.Error("an unknown connection was accepted")
	}
	// It is no tool: discovery never lists it.
	found, err := core.Search(SearchRequest{All: true, Query: "request"})
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range found.Operations {
		if hit.ID == OperationRequest {
			t.Errorf("search lists %s", hit.ID)
		}
	}
}

func hitOf(t *testing.T, core *Core, request SearchRequest, id string) SearchHit {
	t.Helper()
	request.All = true
	got, err := core.Search(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range got.Operations {
		if hit.ID == id {
			return hit
		}
	}
	t.Fatalf("no hit %s", id)
	return SearchHit{}
}

func TestRequestableOnNamesTheConnectionToAsk(t *testing.T) {
	core, _, _ := requestCore(t, []string{"alpha", "beta"}, nil)
	if got := hitOf(t, core, SearchRequest{}, "fake.pages.delete").RequestableOn; got != "alpha" {
		t.Errorf("without a filter requestable_on = %q, want the first connection", got)
	}
	if got := hitOf(t, core, SearchRequest{Connection: "beta"}, "fake.pages.delete").RequestableOn; got != "beta" {
		t.Errorf("with a filter requestable_on = %q, want beta", got)
	}
	// A blocked tool on the first connection falls through to the next one, and on every one it has none.
	conn := core.all.Connections["alpha"]
	conn.BlockedTools = []string{"fake.pages.delete"}
	core.all.Connections["alpha"] = conn
	if got := hitOf(t, core, SearchRequest{}, "fake.pages.delete").RequestableOn; got != "beta" {
		t.Errorf("with alpha blocking it requestable_on = %q, want beta", got)
	}
	if got := hitOf(t, core, SearchRequest{Connection: "alpha"}, "fake.pages.delete").RequestableOn; got != "" {
		t.Errorf("blocked requestable_on = %q, want none", got)
	}
	// The offered tool has no reason and no field.
	if got := hitOf(t, core, SearchRequest{}, "fake.pages.get"); got.RequestableOn != "" || got.Reason != "" {
		t.Errorf("an offered tool = %+v", got)
	}
	// Without all there is no field at all.
	plain, err := core.Search(SearchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range plain.Operations {
		if hit.RequestableOn != "" {
			t.Errorf("hit %+v has requestable_on without all", hit)
		}
	}
	// The field is a scalar of the hit, in the JSON and in the table.
	encoded, err := json.Marshal(hitOf(t, core, SearchRequest{Connection: "beta"}, "fake.pages.delete"))
	if err != nil || !strings.Contains(string(encoded), `"requestable_on":"beta"`) {
		t.Errorf("encoded = %s, %v", encoded, err)
	}
}

func TestRequestableOnIsMissingWhereNoDecisionOffersTheTool(t *testing.T) {
	core := reachCore(t, map[string][]string{"a": {"folder/d1/f1"}})
	hit := hitOf(t, core, SearchRequest{}, "fake.pages.delete")
	if hit.Reason == "" || hit.RequestableOn != "" {
		t.Errorf("hit = %+v, want a reason and no requestable_on for outside-targets", hit)
	}
}

func TestRequestableOnSkipsAnInactiveConnection(t *testing.T) {
	core, _, _ := requestCore(t, []string{"alpha", "beta", "gamma"}, nil)
	clockAt(core, limit.Add(time.Second))
	off := core.all.Connections["alpha"]
	off.Disabled = true
	core.all.Connections["alpha"] = off
	limitConnection(core, "beta")

	if got := hitOf(t, core, SearchRequest{}, "fake.pages.delete").RequestableOn; got != "gamma" {
		t.Errorf("requestable_on = %q, want the first active connection gamma", got)
	}
	for _, name := range []string{"alpha", "beta"} {
		if hit := hitOf(t, core, SearchRequest{Connection: name}, "fake.pages.delete"); hit.RequestableOn != "" || hit.Reason == "" {
			t.Errorf("filter %s: hit = %+v, want a reason and no requestable_on", name, hit)
		}
	}
	if got := hitOf(t, core, SearchRequest{Connection: "gamma"}, "fake.pages.delete").RequestableOn; got != "gamma" {
		t.Errorf("filter gamma: requestable_on = %q", got)
	}
}

func TestRequestableOnIsMissingWhereOnlyInactiveConnectionsExist(t *testing.T) {
	core, _, _ := requestCore(t, []string{"only"}, nil)
	conn := core.all.Connections["only"]
	conn.Disabled = true
	core.all.Connections["only"] = conn
	if hit := hitOf(t, core, SearchRequest{}, "fake.pages.delete"); hit.RequestableOn != "" || hit.Reason == "" {
		t.Errorf("hit = %+v, want a reason and no requestable_on", hit)
	}
}

// A named connection is never refused as disabled by RequestTools, whatever mix of connections exists.
func TestRequestableOnNeverLeadsToConnectionDisabled(t *testing.T) {
	for mask := 0; mask < 8; mask++ {
		core, _, _ := requestCore(t, []string{"a", "b", "c"}, nil)
		clockAt(core, limit.Add(time.Second))
		for i, name := range []string{"a", "b", "c"} {
			conn := core.all.Connections[name]
			switch {
			case mask&(1<<i) == 0:
			case i == 1:
				conn.ActiveUntil = limit.Format(time.RFC3339)
			default:
				conn.Disabled = true
			}
			core.all.Connections[name] = conn
		}
		on := hitOf(t, core, SearchRequest{}, "fake.pages.delete").RequestableOn
		if on == "" {
			continue
		}
		if _, err := core.RequestTools(ToolRequest{Connection: on, Tools: []string{"fake.pages.delete"}}); err != nil {
			t.Errorf("mask %d: requestable_on %s: %v", mask, on, err)
		}
	}
}
