package accessrequest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	yaml "go.yaml.in/yaml/v3"
)

// withKindConnections adds the connections the kind tests need: one that lists tools, one of which a person
// blocked, and one that lists an allow-list tool.
func (f *fixture) withKindConnections() {
	conn := func(tools ...string) config.Connection {
		return config.Connection{Service: "svc", Credential: "cred", Tools: tools}
	}
	f.cfg.Connections["both"] = conn("fake.pages.get", "fake.pages.list")
	f.cfg.Connections["guarded"] = conn("fake.pages.get", "fake.pages.delete")
	g := f.cfg.Connections["guarded"]
	g.BlockedTools = []string{"fake.pages.delete"}
	f.cfg.Connections["guarded"] = g
	f.cfg.Connections["allowed"] = conn("fake.pages.get", "fake.settings.update")
	f.cfg.AssignIDs()
}

func (f *fixture) inputOf(kind Kind, conn string, entries ...string) Input {
	in := f.input(conn, entries...)
	in.Kind = kind
	return in
}

func TestCheckRemove(t *testing.T) {
	many := make([]string, MaxTools+1)
	for i := range many {
		many[i] = fmt.Sprintf("fake.pages.t%d", i)
	}
	for name, tt := range map[string]struct {
		conn    string
		entries []string
		want    string // the sorted result, or empty with errPart
		errPart string
	}{
		"offered":                {"both", []string{"fake.pages.list", "fake.pages.get", "fake.pages.get"}, "fake.pages.get,fake.pages.list", ""},
		"not offered":            {"listed", []string{"fake.pages.delete"}, "", "request it to be added"},
		"prefix over an offered": {"both", []string{"fake.pages.*"}, "fake.pages.*", ""},
		"prefix over none":       {"listed", []string{"fake.settings.*"}, "", "nothing to remove"},
		"prefix skips allow-list": {"allowed", []string{"fake.settings.*"}, "",
			"require the allow-list"},
		"allow-list by exact id": {"allowed", []string{"fake.settings.update"}, "fake.settings.update", ""},
		"other provider":         {"both", []string{"other.items.get"}, "", "another provider"},
		"all or nothing":         {"both", []string{"fake.pages.get", "fake.pages.delete"}, "", "nothing was requested"},
		"unknown tool":           {"both", []string{"fake.pages.nope"}, "", "not a registered tool"},
		"empty":                  {"both", nil, "", "at least one tool"},
		"too many":               {"both", many, "", "at most"},
		"blocked tool offered":   {"guarded", []string{"fake.pages.delete"}, "fake.pages.delete", ""},
		"by effects":             {"reader", []string{"fake.pages.list"}, "fake.pages.list", ""},
		"by effects not offered": {"reader", []string{"fake.pages.delete"}, "", "nothing to remove"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.withKindConnections()
			got, err := CheckRemove(f.env, tt.conn, tt.entries)
			if tt.errPart != "" {
				var invalid *InvalidError
				if !errors.As(err, &invalid) || !strings.Contains(err.Error(), tt.errPart) {
					t.Fatalf("CheckRemove = %v, %v; want invalid with %q", got, err, tt.errPart)
				}
				return
			}
			if err != nil || strings.Join(got, ",") != tt.want {
				t.Fatalf("CheckRemove = %v, %v; want %s", got, err, tt.want)
			}
		})
	}
}

func TestAddAndRemoveToSameConnectionStayTwoRequests(t *testing.T) {
	f := newFixture(t)
	f.withKindConnections()
	for _, in := range []Input{
		f.inputOf("", "both", "fake.pages.delete"),
		f.inputOf(KindRemoveTools, "both", "fake.pages.get"),
		f.inputOf(KindAddTools, "both", "fake.settings.update"),
		f.inputOf(KindRemoveTools, "both", "fake.pages.list"),
	} {
		if _, err := f.q.Add(f.env, in); err != nil {
			t.Fatalf("Add(%+v) = %v", in, err)
		}
	}
	open, _ := f.open(t)
	if len(open) != 2 {
		t.Fatalf("open = %+v, want two requests", open)
	}
	got := map[Kind]string{}
	for _, r := range open {
		got[r.Kind] = strings.Join(r.Tools, ",")
	}
	if got[KindAddTools] != "fake.pages.delete,fake.settings.update" || got[KindRemoveTools] != "fake.pages.get,fake.pages.list" {
		t.Errorf("tools by kind = %v", got)
	}
}

func TestAddRejectsUnknownKindAndWrongKindTools(t *testing.T) {
	f := newFixture(t)
	f.withKindConnections()
	for name, in := range map[string]Input{
		"unknown kind":          f.inputOf("drop-tools", "both", "fake.pages.get"),
		"remove of unoffered":   f.inputOf(KindRemoveTools, "both", "fake.pages.delete"),
		"add of offered":        f.inputOf(KindAddTools, "both", "fake.pages.get"),
		"default of offered":    f.inputOf("", "both", "fake.pages.get"),
		"remove of other owner": f.inputOf(KindRemoveTools, "both", "other.items.get"),
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

func TestRemoveEventsAndDropWhenNoLongerOffered(t *testing.T) {
	f := newFixture(t)
	f.withKindConnections()
	out, err := f.q.Add(f.env, f.inputOf(KindRemoveTools, "both", "fake.pages.get", "fake.pages.list"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 2 || out.Events[0].Kind != EventRemoveCreate || out.Events[0].Tool != "fake.pages.get" ||
		out.Events[0].ConnectionID != f.cfg.Connections["both"].ID || out.Request.Kind != KindRemoveTools {
		t.Fatalf("outcome = %+v", out)
	}

	// The connection stops offering one tool by itself: that entry falls away, the other stays.
	c := f.cfg.Connections["both"]
	c.Tools = []string{"fake.pages.get"}
	f.cfg.Connections["both"] = c
	open, events := f.open(t)
	if len(open) != 1 || strings.Join(open[0].Tools, ",") != "fake.pages.get" ||
		len(events) != 1 || events[0].Kind != EventRemoveDrop || events[0].Tool != "fake.pages.list" {
		t.Fatalf("open = %+v, events = %+v", open, events)
	}

	// Without any offered tool left the request is gone.
	c.Tools = []string{"fake.pages.list"}
	f.cfg.Connections["both"] = c
	if open, events = f.open(t); len(open) != 0 || len(events) != 1 || events[0].Kind != EventRemoveDrop {
		t.Fatalf("open = %+v, events = %+v", open, events)
	}
}

func TestRemoveExpires(t *testing.T) {
	f := newFixture(t)
	f.withKindConnections()
	if _, err := f.q.Add(f.env, f.inputOf(KindRemoveTools, "both", "fake.pages.get")); err != nil {
		t.Fatal(err)
	}
	f.env.Now = epoch.Add(Lifetime)
	open, events := f.open(t)
	if len(open) != 0 || len(events) != 1 || events[0].Kind != EventRemoveExpire {
		t.Fatalf("open = %+v, events = %+v", open, events)
	}
}

func TestRemoveRequestOfDisabledConnectionStays(t *testing.T) {
	f := newFixture(t)
	f.withKindConnections()
	if _, err := f.q.Add(f.env, f.inputOf(KindRemoveTools, "both", "fake.pages.get")); err != nil {
		t.Fatal(err)
	}
	c := f.cfg.Connections["both"]
	c.Disabled = true
	f.cfg.Connections["both"] = c
	if open, events := f.open(t); len(open) != 1 || len(events) != 0 {
		t.Fatalf("open = %+v, events = %+v", open, events)
	}
}

// legacyDocument is the file format as a Connect without kinds reads it: strict, so an unknown field fails.
type legacyDocument struct {
	Version  int `yaml:"version"`
	Requests []struct {
		ID           string    `yaml:"id"`
		ConnectionID string    `yaml:"connection_id"`
		Connection   string    `yaml:"connection"`
		Provider     string    `yaml:"provider"`
		Tools        []string  `yaml:"tools"`
		Reason       string    `yaml:"reason"`
		Project      string    `yaml:"project"`
		RequestedAt  time.Time `yaml:"requested_at"`
		ExpiresAt    time.Time `yaml:"expires_at"`
	} `yaml:"requests"`
}

func decodeLegacy(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(&legacyDocument{})
}

func TestFileFormatKeepsAnOlderConnectSafe(t *testing.T) {
	f := newFixture(t)
	f.withKindConnections()
	if _, err := f.q.Add(f.env, f.inputOf("", "both", "fake.pages.delete")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(f.q.Path())
	if strings.Contains(string(data), "kind") || !strings.Contains(string(data), "version: 1") {
		t.Errorf("a file without a remove request must stay in the old format:\n%s", data)
	}
	if err := decodeLegacy(f.q.Path()); err != nil {
		t.Errorf("an older Connect cannot read a file without a new kind: %v", err)
	}

	if _, err := f.q.Add(f.env, f.inputOf(KindRemoveTools, "both", "fake.pages.get")); err != nil {
		t.Fatal(err)
	}
	if data, _ = os.ReadFile(f.q.Path()); !strings.Contains(string(data), "kind: remove-tools") || strings.Count(string(data), "kind:") != 1 {
		t.Errorf("only the remove request carries a kind:\n%s", data)
	}
	// The same version stays safe: the unknown field makes the old strict decoder refuse the whole file.
	if err := decodeLegacy(f.q.Path()); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Errorf("an older Connect must refuse a file with a remove request, got %v", err)
	}
}

func TestFileWithoutKindReadsAsAddTools(t *testing.T) {
	f := newFixture(t)
	f.withKindConnections()
	id := f.cfg.Connections["listed"].ID
	raw := fmt.Sprintf(`version: 1
requests:
  - id: 0123456789abcdef0123456789abcdef
    connection_id: %s
    connection: listed
    provider: fake
    tools: [fake.pages.delete]
    requested_at: 2026-11-19T10:00:00Z
    expires_at: 2026-12-03T10:00:00Z
`, id)
	if err := os.WriteFile(f.q.Path(), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	open, _ := f.open(t)
	if len(open) != 1 || open[0].Kind != KindAddTools {
		t.Fatalf("open = %+v, want kind %s", open, KindAddTools)
	}
	// Writing it again keeps the default out of the file.
	if _, err := f.q.Add(f.env, f.inputOf("", "listed", "fake.pages.list")); err == nil {
		// fake.pages.list is not offered by "listed", so this adds to the same request.
		if data, _ := os.ReadFile(f.q.Path()); strings.Contains(string(data), "kind") {
			t.Errorf("default kind written:\n%s", data)
		}
	}
}

func TestFileWithUnknownKindIsInvalid(t *testing.T) {
	f := newFixture(t)
	f.withKindConnections()
	raw := fmt.Sprintf(`version: 1
requests:
  - id: 0123456789abcdef0123456789abcdef
    kind: unknown-kind
    connection_id: %s
    connection: listed
    provider: fake
    tools: [fake.pages.delete]
    requested_at: 2026-11-19T10:00:00Z
    expires_at: 2026-12-03T10:00:00Z
`, f.cfg.Connections["listed"].ID)
	if err := os.WriteFile(f.q.Path(), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.q.Open(f.env); err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("Open = %v, want an unknown kind error", err)
	}
}
