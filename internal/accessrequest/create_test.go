package accessrequest

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// createFixture is a fixture whose provider has targets: spaces/KEY, several at once, and a tool that only
// a binding to some target reaches.
func createFixture(t *testing.T) *fixture {
	t.Helper()
	meta := config.ProviderMetadata{ID: "fake", Target: config.TargetMetadata{
		Label: "space", Multiple: true,
		Kinds: []config.TargetKind{{Name: "space", Forms: []string{"spaces/KEY"}}},
		Reaches: func(tool string, targets []string) (bool, []string) {
			return tool != "fake.pages.remote" || len(targets) > 0, []string{"spaces/KEY"}
		},
	}}
	f := newFixtureWith(t, meta)
	f.env.Redact = func(s string) string { return strings.ReplaceAll(s, "hunter2hunter2", "[redacted]") }
	return f
}

func createInput(name string, targets []string, tools ...string) Input {
	return Input{Kind: KindCreateConnection, Provider: "fake", Connection: name, Targets: targets, Tools: tools,
		Reason: "needed", ReasonRaw: "needed"}
}

func TestCheckCreate(t *testing.T) {
	f := createFixture(t)
	spaces := []string{"spaces/ENG"}
	many := make([]string, MaxTools+1)
	for i := range many {
		many[i] = "fake.pages.get"
		if i > 0 {
			many[i] += strings.Repeat("x", i)
		}
	}
	for _, tt := range []struct {
		name string
		in   CreateInput
		want string // empty: valid; else a part of the problem
	}{
		{"valid", CreateInput{"fake", "docs", spaces, []string{"fake.pages.get", "fake.pages.*"}, "r"}, ""},
		{"valid without targets", CreateInput{"fake", "docs", nil, []string{"fake.pages.get"}, ""}, ""},
		{"name taken", CreateInput{"fake", "listed", nil, []string{"fake.pages.get"}, ""}, "exists"},
		{"name invalid", CreateInput{"fake", "-bad name", nil, []string{"fake.pages.get"}, ""}, "name must"},
		{"name empty", CreateInput{"fake", "", nil, []string{"fake.pages.get"}, ""}, "name must not be empty"},
		{"name too long", CreateInput{"fake", strings.Repeat("a", MaxNameRunes+1), nil, []string{"fake.pages.get"}, ""}, "longer than"},
		{"unknown provider", CreateInput{"nope", "docs", nil, []string{"fake.pages.get"}, ""}, "not registered"},
		{"no tool", CreateInput{"fake", "docs", nil, nil, ""}, "at least one tool"},
		{"prefix without match", CreateInput{"fake", "docs", nil, []string{"fake.nothing.*"}, ""}, "matches no tool"},
		{"prefix of allow-list tool only", CreateInput{"fake", "docs", nil, []string{"fake.settings.*"}, ""}, "covers no tool"},
		{"unregistered tool", CreateInput{"fake", "docs", nil, []string{"fake.pages.nope"}, ""}, "not a registered tool"},
		{"tool of another provider", CreateInput{"fake", "docs", nil, []string{"other.items.get"}, ""}, "other-provider"},
		{"target invalid", CreateInput{"fake", "docs", []string{"elsewhere/ENG"}, []string{"fake.pages.get"}, ""}, "not valid"},
		{"target padded", CreateInput{"fake", "docs", []string{" spaces/ENG"}, []string{"fake.pages.get"}, ""}, "padded"},
		{"too many targets", CreateInput{"fake", "docs", make([]string, MaxTargets+1), []string{"fake.pages.get"}, ""}, "at most"},
		{"tool outside targets", CreateInput{"fake", "docs", nil, []string{"fake.pages.remote"}, ""}, "outside-targets"},
		{"tool outside targets with hint", CreateInput{"fake", "docs", nil, []string{"fake.pages.remote"}, ""}, "needs spaces/KEY"},
		{"over the tool limit", CreateInput{"fake", "docs", nil, many, ""}, "at most 50"},
		{"secret in name", CreateInput{"fake", "hunter2hunter2", nil, []string{"fake.pages.get"}, ""}, "known secret"},
		{"secret in target", CreateInput{"fake", "docs", []string{"spaces/hunter2hunter2"}, []string{"fake.pages.get"}, ""}, "known secret"},
		{"secret in tool", CreateInput{"fake", "docs", nil, []string{"fake.hunter2hunter2.*"}, ""}, "known secret"},
		{"secret in reason", CreateInput{"fake", "docs", nil, []string{"fake.pages.get"}, "use hunter2hunter2"}, "known secret"},
	} {
		got, err := CheckCreate(f.env, tt.in)
		var invalid *InvalidError
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("%s: %v", tt.name, err)
		case tt.want == "" && (len(got.Tools) == 0 || got.Provider != "fake"):
			t.Errorf("%s: got %+v", tt.name, got)
		case tt.want != "" && (!errors.As(err, &invalid) || !strings.Contains(err.Error(), tt.want)):
			t.Errorf("%s: err = %v, want *InvalidError containing %q", tt.name, err, tt.want)
		}
	}
	got, err := CheckCreate(f.env, CreateInput{"fake", "docs", []string{"spaces/B", "spaces/A", "spaces/B"},
		[]string{"fake.pages.list", "fake.pages.get", "fake.pages.get"}, ""})
	if err != nil || strings.Join(got.Tools, ",") != "fake.pages.get,fake.pages.list" || strings.Join(got.Targets, ",") != "spaces/A,spaces/B" {
		t.Errorf("normalized = %+v, %v", got, err)
	}
}

func TestCheckCreateAProviderWithoutTargetsTakesNone(t *testing.T) {
	f := newFixtureWith(t, config.ProviderMetadata{ID: "fake"})
	if _, err := CheckCreate(f.env, CreateInput{"fake", "docs", []string{"anything"}, []string{"fake.pages.get"}, ""}); err == nil ||
		!strings.Contains(err.Error(), "accepts no targets") {
		t.Errorf("err = %v", err)
	}
}

func TestAddCreateMergesByProviderAndName(t *testing.T) {
	f := createFixture(t)
	first, err := f.q.Add(f.env, createInput("docs", []string{"spaces/A"}, "fake.pages.get"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Request.ConnectionID != "" || first.Request.Connection != "docs" || len(first.Events) != 1 ||
		first.Events[0].Kind != EventCreateConnectionCreate || first.Events[0].Connection != "docs" || first.Events[0].ConnectionID != "" {
		t.Fatalf("first = %+v", first)
	}
	second, err := f.q.Add(f.env, createInput("docs", []string{"spaces/B"}, "fake.pages.get", "fake.pages.list"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Request.ID != first.Request.ID || strings.Join(second.Request.Tools, ",") != "fake.pages.get,fake.pages.list" ||
		strings.Join(second.Request.Targets, ",") != "spaces/A,spaces/B" || len(second.Events) != 1 || second.Events[0].Tool != "fake.pages.list" {
		t.Fatalf("second = %+v", second)
	}
	if _, err := f.q.Add(f.env, createInput("other", nil, "fake.pages.get")); err != nil {
		t.Fatal(err)
	}
	if open, _ := f.open(t); len(open) != 2 {
		t.Fatalf("open = %+v, want one request per name", open)
	}
	// An add-tools request on a connection of that name stays a separate entry.
	f.add(t, "listed", "fake.pages.delete")
	if open, _ := f.open(t); len(open) != 3 {
		t.Fatalf("open = %d, want 3", len(open))
	}
}

func TestAddCreateJudgesTheMergedRequest(t *testing.T) {
	f := createFixture(t)
	if _, err := f.q.Add(f.env, createInput("docs", nil, "fake.pages.get")); err != nil {
		t.Fatal(err)
	}
	var targets []string
	for i := 0; i < MaxTargets; i++ {
		targets = append(targets, "spaces/T"+strings.Repeat("x", i))
	}
	if _, err := f.q.Add(f.env, createInput("docs", targets, "fake.pages.get")); err != nil {
		t.Fatal(err)
	}
	var invalid *InvalidError
	if _, err := f.q.Add(f.env, createInput("docs", []string{"spaces/Extra"}, "fake.pages.get")); !errors.As(err, &invalid) {
		t.Errorf("Add past the target limit = %v, want *InvalidError", err)
	}
}

func TestAddCreateDropsWhenTheNameIsTakenOrProviderGone(t *testing.T) {
	f := createFixture(t)
	f.add(t, "listed", "fake.pages.delete")
	if _, err := f.q.Add(f.env, createInput("docs", nil, "fake.pages.get")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.Add(f.env, createInput("gone", nil, "fake.pages.get")); err != nil {
		t.Fatal(err)
	}

	f.cfg.Connections["docs"] = f.cfg.Connections["listed"]
	open, events := f.open(t)
	if len(open) != 2 || len(events) != 1 || events[0].Kind != EventCreateConnectionDrop || events[0].Connection != "docs" {
		t.Fatalf("open = %d, events = %+v", len(open), events)
	}

	var rest []Tool
	for _, tool := range f.env.Tools {
		if tool.Provider != "fake" {
			rest = append(rest, tool)
		}
	}
	f.env.Tools = rest
	open, events = f.open(t)
	dropped := 0
	for _, e := range events {
		if e.Kind == EventCreateConnectionDrop {
			dropped++
		}
	}
	if len(open) != 0 || dropped != 1 {
		t.Fatalf("open = %+v, events = %+v", open, events)
	}
}

func TestAddCreateExpires(t *testing.T) {
	f := createFixture(t)
	if _, err := f.q.Add(f.env, createInput("docs", nil, "fake.pages.get", "fake.pages.list")); err != nil {
		t.Fatal(err)
	}
	f.env.Now = epoch.Add(Lifetime)
	open, events := f.open(t)
	if len(open) != 0 || len(events) != 2 || events[0].Kind != EventCreateConnectionExpire || events[0].Connection != "docs" {
		t.Fatalf("open = %+v, events = %+v", open, events)
	}
}

func TestAddCreateRefusesWithoutStoring(t *testing.T) {
	f := createFixture(t)
	for name, in := range map[string]Input{
		"names a connection": {Kind: KindCreateConnection, Provider: "fake", Connection: "docs", ConnectionID: "x", Tools: []string{"fake.pages.get"}},
		"secret in reason":   {Kind: KindCreateConnection, Provider: "fake", Connection: "docs", Tools: []string{"fake.pages.get"}, ReasonRaw: "hunter2hunter2"},
		"name taken":         createInput("listed", nil, "fake.pages.get"),
	} {
		var invalid *InvalidError
		if _, err := f.q.Add(f.env, in); !errors.As(err, &invalid) {
			t.Errorf("%s: Add = %v, want *InvalidError", name, err)
		}
	}
	if _, err := os.Stat(f.q.Path()); err == nil {
		t.Error("a refused request created the file")
	}
}

func TestValidateByKind(t *testing.T) {
	good := func(edit func(*Request)) document {
		r := Request{ID: "0123456789abcdef0123456789abcdef", Kind: KindCreateConnection, Connection: "docs", Provider: "fake",
			Tools: []string{"fake.pages.get"}, Targets: []string{"spaces/A"}, RequestedAt: epoch, ExpiresAt: epoch.Add(Lifetime)}
		edit(&r)
		return document{Version: 1, Requests: []Request{r}}
	}
	for name, tt := range map[string]struct {
		doc document
		ok  bool
	}{
		"create":                  {good(func(*Request) {}), true},
		"create with an id":       {good(func(r *Request) { r.ConnectionID = "abc" }), false},
		"create without name":     {good(func(r *Request) { r.Connection = "" }), false},
		"create without provider": {good(func(r *Request) { r.Provider = "" }), false},
		"create without tool":     {good(func(r *Request) { r.Tools = nil }), false},
		"add without connection":  {good(func(r *Request) { r.Kind, r.Targets = KindAddTools, nil }), false},
		"add with targets":        {good(func(r *Request) { r.Kind, r.ConnectionID = KindAddTools, "abc" }), false},
		"remove with connection":  {good(func(r *Request) { r.Kind, r.ConnectionID, r.Targets = KindRemoveTools, "abc", nil }), true},
		"unknown kind with an id": {good(func(r *Request) { r.Kind = "nope" }), false},
	} {
		if err := validate(tt.doc); (err == nil) != tt.ok {
			t.Errorf("%s: validate = %v, want ok %v", name, err, tt.ok)
		}
	}
}

// A file without a request to create a connection keeps its format, and an older Connect, which knows no
// kind and no targets, refuses a file with one instead of misreading it.
func TestCreateKeepsTheFileFormatSafe(t *testing.T) {
	f := createFixture(t)
	f.add(t, "listed", "fake.pages.delete")
	data, _ := os.ReadFile(f.q.Path())
	if bytes.Contains(data, []byte("kind")) || bytes.Contains(data, []byte("targets")) || !bytes.Contains(data, []byte("connection_id: ")) {
		t.Errorf("a request to add changed the format:\n%s", data)
	}
	if err := decodeLegacy(f.q.Path()); err != nil {
		t.Errorf("an older Connect cannot read a file without the new kind: %v", err)
	}
	if _, err := f.q.Add(f.env, createInput("docs", []string{"spaces/A"}, "fake.pages.get")); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(f.q.Path())
	if !bytes.Contains(data, []byte("kind: create-connection")) || bytes.Contains(data, []byte("connection_id: \"\"")) ||
		strings.Count(string(data), "connection_id:") != 1 {
		t.Errorf("unexpected file:\n%s", data)
	}
	if err := decodeLegacy(f.q.Path()); err == nil {
		t.Error("an older Connect read a file with a request to create a connection")
	}
}
