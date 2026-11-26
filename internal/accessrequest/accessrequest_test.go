package accessrequest

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

type catalog struct{ metadata config.ProviderMetadata }

func (c catalog) ProviderMetadata(id string) (config.ProviderMetadata, bool) {
	return c.metadata, id == c.metadata.ID
}
func (c catalog) ProviderMetadataAll() []config.ProviderMetadata {
	return []config.ProviderMetadata{c.metadata}
}

var tools = []Tool{
	{config.ToolMetadata{ID: "fake.pages.get", Effect: config.EffectRead}, "fake"},
	{config.ToolMetadata{ID: "fake.pages.list", Effect: config.EffectRead}, "fake"},
	{config.ToolMetadata{ID: "fake.pages.delete", Effect: config.EffectDelete}, "fake"},
	{config.ToolMetadata{ID: "fake.pages.remote", Effect: config.EffectRead}, "fake"},
	{config.ToolMetadata{ID: "fake.settings.update", Effect: config.EffectUpdate, RequiresToolAllowList: true}, "fake"},
	{config.ToolMetadata{ID: "fake.files.save", Effect: config.EffectRead, LocalFiles: config.LocalFilesWrite}, "fake"},
	{config.ToolMetadata{ID: "other.items.get", Effect: config.EffectRead}, "other"},
}

var epoch = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

// fixture holds a configuration with one connection per case.
type fixture struct {
	cfg *config.Config
	env Env
	q   *Queue
	dir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWith(t, config.ProviderMetadata{ID: "fake", Target: config.TargetMetadata{
		Reaches: func(tool string, targets []string) (bool, []string) {
			return tool != "fake.pages.remote" || len(targets) > 0, nil
		},
	}})
}

func newFixtureWith(t *testing.T, meta config.ProviderMetadata) *fixture {
	t.Helper()
	cfg := config.New(catalog{meta})
	cfg.Services["svc"] = config.Service{Provider: "fake", BaseURL: "https://example.invalid"}
	cfg.Credentials["cred"] = config.Credential{Type: config.CredentialTypeEnv, Values: map[string]string{"token": "FAKE_TOKEN"}}
	conn := func(edit func(*config.Connection)) config.Connection {
		c := config.Connection{Service: "svc", Credential: "cred"}
		edit(&c)
		return c
	}
	cfg.Connections["listed"] = conn(func(c *config.Connection) { c.Tools = []string{"fake.pages.get"} })
	cfg.Connections["reader"] = conn(func(c *config.Connection) { c.Effects = []config.Effect{config.EffectRead} })
	cfg.Connections["blocking"] = conn(func(c *config.Connection) {
		c.Tools = []string{"fake.pages.get"}
		c.BlockedTools = []string{"fake.pages.delete"}
	})
	cfg.AssignIDs()
	dir := t.TempDir()
	q, err := NewQueue(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{cfg: cfg, env: Env{Config: cfg, Tools: tools, Now: epoch}, q: q, dir: dir}
}

func (f *fixture) input(conn string, entries ...string) Input {
	return Input{Connection: conn, ConnectionID: f.cfg.Connections[conn].ID, Tools: entries, Reason: "needed"}
}

func (f *fixture) add(t *testing.T, conn string, entries ...string) Outcome {
	t.Helper()
	out, err := f.q.Add(f.env, f.input(conn, entries...))
	if err != nil {
		t.Fatalf("Add(%s, %v) = %v", conn, entries, err)
	}
	return out
}

func (f *fixture) open(t *testing.T) ([]Request, []Event) {
	t.Helper()
	r, e, err := f.q.Open(f.env)
	if err != nil {
		t.Fatal(err)
	}
	return r, e
}

func TestAddByToolsListAndByEffects(t *testing.T) {
	for _, tt := range []struct {
		conn    string
		entries []string
	}{
		{"listed", []string{"fake.pages.delete"}},
		{"listed", []string{"fake.pages.*"}},
		{"reader", []string{"fake.pages.delete"}},
		{"reader", []string{"fake.pages.*"}},
		{"reader", []string{"fake.settings.update"}},
		{"listed", []string{"fake.pages.list", "fake.pages.delete"}},
	} {
		f := newFixture(t)
		out := f.add(t, tt.conn, tt.entries...)
		if len(out.Request.ID) != 32 || out.Request.Connection != tt.conn || len(out.Events) != len(out.New) {
			t.Errorf("%v: outcome %+v", tt, out)
		}
		if got, _ := f.open(t); len(got) != 1 || strings.Join(got[0].Tools, ",") == "" {
			t.Errorf("%v: open = %+v", tt, got)
		}
	}
}

func TestAddRefusesWithoutStoring(t *testing.T) {
	for name, tt := range map[string]struct {
		conn    string
		entries []string
		tweak   func(*config.Config)
		reason  string
	}{
		"offered":               {"listed", []string{"fake.pages.get"}, nil, "already offered"},
		"not registered":        {"listed", []string{"fake.pages.nothing"}, nil, "not a registered tool"},
		"other provider":        {"listed", []string{"other.items.get"}, nil, string(config.RefusalOtherProvider)},
		"other provider prefix": {"listed", []string{"other.items.*"}, nil, string(config.RefusalOtherProvider)},
		"bad prefix":            {"listed", []string{"fake.*"}, nil, "not a valid tool prefix"},
		"prefix without a tool": {"listed", []string{"fake.nothing.*"}, nil, "matches no tool"},
		"outside targets":       {"listed", []string{"fake.pages.remote"}, nil, string(config.RefusalOutsideTargets)},
		"no local files":        {"listed", []string{"fake.files.save"}, nil, string(config.RefusalNoLocalFiles)},
		"outdated targets": {"listed", []string{"fake.pages.delete"}, func(c *config.Config) {
			cc := c.Connections["listed"]
			cc.TargetsOutdated = true
			c.Connections["listed"] = cc
		}, string(config.RefusalTargetsOutdated)},
		"prefix over allow-list":  {"reader", []string{"fake.settings.*"}, nil, "require the allow-list"},
		"valid next to a refusal": {"listed", []string{"fake.pages.delete", "fake.pages.get"}, nil, "already offered"},
		"too many":                {"listed", manyTools(MaxTools + 1), nil, "at most"},
		"none":                    {"listed", nil, nil, "at least one"},
	} {
		f := newFixture(t)
		if tt.tweak != nil {
			tt.tweak(f.cfg)
		}
		_, err := f.q.Add(f.env, f.input(tt.conn, tt.entries...))
		var invalid *InvalidError
		if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, tt.reason) {
			t.Errorf("%s: err = %v, want invalid containing %q", name, err, tt.reason)
		}
		if _, statErr := os.Stat(f.q.Path()); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("%s: the queue file exists after a refusal", name)
		}
	}
}

func manyTools(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "fake.pages.get"
	}
	return out
}

func TestAddNamesToolAndReason(t *testing.T) {
	f := newFixture(t)
	_, err := f.q.Add(f.env, f.input("listed", "fake.pages.remote"))
	if err == nil || !strings.Contains(err.Error(), "fake.pages.remote") || !strings.Contains(err.Error(), "outside-targets") {
		t.Fatalf("err = %v, want tool and reason", err)
	}
}

func TestBlockedToolIsRefusedWithoutStoring(t *testing.T) {
	f := newFixture(t)
	_, err := f.q.Add(f.env, f.input("blocking", "fake.pages.list", "fake.pages.delete"))
	var blocked *BlockedError
	if !errors.As(err, &blocked) || len(blocked.Tools) != 1 || blocked.Tools[0] != "fake.pages.delete" ||
		!strings.Contains(err.Error(), "do not request them again") {
		t.Fatalf("err = %v, want blocked fake.pages.delete", err)
	}
	if _, statErr := os.Stat(f.q.Path()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("the queue file exists after a blocked request")
	}
	// A prefix whose every requestable tool is blocked is blocked as well, one with a free tool is not.
	if _, err := f.q.Add(f.env, f.input("blocking", "fake.pages.*")); err != nil {
		t.Fatalf("prefix with free tools = %v", err)
	}
}

func TestMergeAddsToolsTakesNewestReasonAndRenewsExpiry(t *testing.T) {
	f := newFixture(t)
	first := f.add(t, "listed", "fake.pages.delete")
	f.env.Now = epoch.Add(24 * time.Hour)
	in := f.input("listed", "fake.pages.delete", "fake.pages.list")
	in.Reason = "newer"
	out, err := f.q.Add(f.env, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Request.ID != first.Request.ID || strings.Join(out.Request.Tools, ",") != "fake.pages.delete,fake.pages.list" ||
		strings.Join(out.New, ",") != "fake.pages.list" || len(out.Events) != 1 || out.Events[0].Kind != EventCreate ||
		out.Request.Reason != "newer" || !out.Request.ExpiresAt.Equal(f.env.Now.Add(Lifetime)) {
		t.Fatalf("merged = %+v", out)
	}
	if got, _ := f.open(t); len(got) != 1 {
		t.Fatalf("open = %d requests, want 1", len(got))
	}
	other := f.add(t, "reader", "fake.pages.delete")
	if other.Request.ID == first.Request.ID {
		t.Fatal("another connection shares the request")
	}
}

func TestExpiryAtTheFourteenDayBoundary(t *testing.T) {
	f := newFixture(t)
	f.add(t, "listed", "fake.pages.delete", "fake.pages.list")
	f.env.Now = epoch.Add(Lifetime - time.Second)
	if got, events := f.open(t); len(got) != 1 || len(events) != 0 {
		t.Fatalf("just before: %v %v", got, events)
	}
	f.env.Now = epoch.Add(Lifetime)
	got, events := f.open(t)
	if len(got) != 0 || len(events) != 2 {
		t.Fatalf("at the boundary: %v %v", got, events)
	}
	for _, e := range events {
		if e.Kind != EventExpire || e.Connection != "listed" || e.ConnectionID == "" {
			t.Errorf("event %+v", e)
		}
	}
	if got, events := f.open(t); len(got) != 0 || len(events) != 0 {
		t.Fatalf("expiry was not persisted: %v %v", got, events)
	}
}

func TestAddPrunesExpiredRequests(t *testing.T) {
	f := newFixture(t)
	f.add(t, "reader", "fake.pages.delete")
	f.env.Now = epoch.Add(Lifetime)
	out := f.add(t, "listed", "fake.pages.delete")
	if len(out.Events) != 2 || out.Events[0].Kind != EventExpire || out.Events[1].Kind != EventCreate {
		t.Fatalf("events = %+v", out.Events)
	}
	if got, _ := f.open(t); len(got) != 1 || got[0].Connection != "listed" {
		t.Fatalf("open = %+v", got)
	}
}

func TestDropWhenDeletedOfferedOrBlocked(t *testing.T) {
	f := newFixture(t)
	f.add(t, "listed", "fake.pages.delete", "fake.pages.list")
	f.add(t, "reader", "fake.pages.delete")
	f.add(t, "blocking", "fake.pages.list")

	cfg := f.cfg
	delete(cfg.Connections, "reader")
	listed := cfg.Connections["listed"]
	listed.Tools = []string{"fake.pages.get", "fake.pages.list"}
	cfg.Connections["listed"] = listed
	blocking := cfg.Connections["blocking"]
	blocking.BlockedTools = append(blocking.BlockedTools, "fake.pages.list")
	cfg.Connections["blocking"] = blocking

	got, events := f.open(t)
	dropped := map[string]bool{}
	for _, e := range events {
		if e.Kind != EventDrop {
			t.Errorf("event %+v is no drop", e)
		}
		dropped[e.Connection+" "+e.Tool] = true
	}
	for _, want := range []string{"reader fake.pages.delete", "listed fake.pages.list", "blocking fake.pages.list"} {
		if !dropped[want] {
			t.Errorf("missing drop of %s in %v", want, dropped)
		}
	}
	if len(got) != 1 || got[0].Connection != "listed" || strings.Join(got[0].Tools, ",") != "fake.pages.delete" {
		t.Fatalf("open = %+v", got)
	}
}

func TestPrefixDropsWhenNothingBelowItStaysRequestable(t *testing.T) {
	f := newFixture(t)
	f.add(t, "listed", "fake.pages.*")
	listed := f.cfg.Connections["listed"]
	listed.Tools = []string{"fake.pages.*"}
	f.cfg.Connections["listed"] = listed
	got, events := f.open(t)
	if len(got) != 0 || len(events) != 1 || events[0].Tool != "fake.pages.*" || events[0].Kind != EventDrop {
		t.Fatalf("open = %v events = %v", got, events)
	}
}

func TestRenamedConnectionKeepsItsRequests(t *testing.T) {
	f := newFixture(t)
	f.add(t, "listed", "fake.pages.delete")
	f.cfg.Connections["renamed"] = f.cfg.Connections["listed"]
	delete(f.cfg.Connections, "listed")
	got, events := f.open(t)
	if len(events) != 0 || len(got) != 1 || got[0].Connection != "renamed" {
		t.Fatalf("open = %+v events = %v", got, events)
	}
	merged := f.add(t, "renamed", "fake.pages.list")
	if len(merged.Request.Tools) != 2 {
		t.Fatalf("merge after rename = %+v", merged.Request)
	}
}

func TestFileIsPrivateAndHoldsNoOtherData(t *testing.T) {
	f := newFixture(t)
	f.add(t, "listed", "fake.pages.delete")
	info, err := os.Stat(f.q.Path())
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("file = %v, %v, want mode 0600", info, err)
	}
	if got := PathFor(filepath.Join("a", "b", "config.yaml")); got != filepath.Join("a", "b", FileName) {
		t.Fatalf("PathFor = %q", got)
	}
}

func TestConcurrentWritersLoseNoRequest(t *testing.T) {
	f := newFixture(t)
	other, err := NewQueue(f.q.Path())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i, run := range []struct {
		q    *Queue
		conn string
		tool string
	}{{f.q, "listed", "fake.pages.delete"}, {other, "reader", "fake.pages.delete"}, {f.q, "listed", "fake.pages.list"}, {other, "reader", "fake.settings.update"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := run.q.Add(f.env, f.input(run.conn, run.tool)); err != nil {
				t.Errorf("writer %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	got, _ := f.open(t)
	count := 0
	for _, r := range got {
		count += len(r.Tools)
	}
	if len(got) != 2 || count != 4 {
		t.Fatalf("open = %+v, want two requests with four tools", got)
	}
}

func TestUnreadableFileIsNeverOverwritten(t *testing.T) {
	f := newFixture(t)
	garbage := []byte("version: 1\nrequests: [not, a, request\n")
	if err := os.WriteFile(f.q.Path(), garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := f.q.Add(f.env, f.input("listed", "fake.pages.delete"))
	var invalid *config.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Add = %v, want *config.InvalidError", err)
	}
	if _, _, err := f.q.Open(f.env); !errors.As(err, &invalid) {
		t.Fatalf("Open = %v, want *config.InvalidError", err)
	}
	if data, _ := os.ReadFile(f.q.Path()); string(data) != string(garbage) {
		t.Fatalf("file was changed: %q", data)
	}
}

func TestMissingFileMeansNoRequests(t *testing.T) {
	f := newFixture(t)
	if got, events := f.open(t); got != nil || events != nil {
		t.Fatalf("open = %v %v", got, events)
	}
	if _, err := os.Stat(f.q.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reading created the file")
	}
}

func TestOpenListsNewestFirst(t *testing.T) {
	f := newFixture(t)
	f.add(t, "listed", "fake.pages.delete")
	f.env.Now = epoch.Add(time.Hour)
	f.add(t, "reader", "fake.pages.delete")
	got, _ := f.open(t)
	if len(got) != 2 || got[0].Connection != "reader" {
		t.Fatalf("open = %+v", got)
	}
}

func TestLookupReadsWithoutWritingAndSettleRemovesTools(t *testing.T) {
	f := newFixture(t)
	id := f.add(t, "listed", "fake.pages.delete", "fake.pages.list").Request.ID
	before, _ := os.ReadFile(f.q.Path())

	// One tool is offered meanwhile: Lookup leaves it out and reports it, and writes nothing.
	cc := f.cfg.Connections["listed"]
	cc.Tools = append(cc.Tools, "fake.pages.list")
	f.cfg.Connections["listed"] = cc
	req, gone, err := f.q.Lookup(f.env, id)
	if err != nil || strings.Join(req.Tools, ",") != "fake.pages.delete" || strings.Join(gone, ",") != "fake.pages.list" {
		t.Fatalf("Lookup() = %+v, %v, %v", req, gone, err)
	}
	if after, _ := os.ReadFile(f.q.Path()); string(after) != string(before) {
		t.Error("Lookup wrote the queue")
	}
	if _, _, err := f.q.Lookup(f.env, "0123456789abcdef0123456789abcdef"); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Lookup(unknown) = %v", err)
	}

	events, err := f.q.Settle(f.env, id, []string{"fake.pages.delete"}, nil)
	if err != nil || len(events) != 1 || events[0].Kind != EventDrop || events[0].Tool != "fake.pages.list" {
		t.Fatalf("Settle() = %v, %v, want the pruned tool as a drop event", events, err)
	}
	if open, _ := f.open(t); len(open) != 0 {
		t.Errorf("open = %+v, want the emptied request gone", open)
	}
	if _, err := f.q.Settle(f.env, id, nil, nil); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Settle(closed) = %v", err)
	}
}
