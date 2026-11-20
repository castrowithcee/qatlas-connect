package config

import (
	"reflect"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

func blockedConfig(t *testing.T, edit func(*Connection)) *Config {
	t.Helper()
	cfg := problemsBase(t)
	conn := cfg.Connections["gh"]
	edit(&conn)
	cfg.Connections["gh"] = conn
	return cfg
}

func blockedProblems(cfg *Config) []string {
	var codes []string
	for _, p := range Problems(cfg.Validate()) {
		if p.Field == "blocked_tools" {
			codes = append(codes, p.Code)
		}
	}
	return codes
}

func TestBlockedToolsValidation(t *testing.T) {
	for name, tt := range map[string]struct {
		edit func(*Connection)
		want []string
	}{
		"valid exact and prefix": {func(c *Connection) {
			c.BlockedTools = []string{"github.issues.get", "github.comments.*"}
		}, nil},
		"prefix covers a tool that needs the allow-list": {func(c *Connection) {
			c.BlockedTools = []string{"github.actions-permissions.*"}
		}, nil},
		"whole provider prefix": {func(c *Connection) { c.BlockedTools = []string{"github.*"} },
			[]string{CodeInvalidBlockedTool}},
		"other provider": {func(c *Connection) { c.BlockedTools = []string{"bookstack.pages.get"} },
			[]string{CodeBlockedToolOfOtherProvider}},
		"other provider prefix": {func(c *Connection) { c.BlockedTools = []string{"bookstack.pages.*"} },
			[]string{CodeBlockedToolOfOtherProvider}},
		"unknown id": {func(c *Connection) { c.BlockedTools = []string{"github.issues.nothing"} },
			[]string{CodeInvalidBlockedTool}},
		"prefix without a tool": {func(c *Connection) { c.BlockedTools = []string{"github.nothing.*"} },
			[]string{CodeInvalidBlockedTool}},
		"duplicate": {func(c *Connection) { c.BlockedTools = []string{"github.issues.get", "github.issues.get"} },
			[]string{CodeDuplicateTool}},
		"covered by tools exactly": {func(c *Connection) {
			c.Tools = []string{"github.issues.get"}
			c.BlockedTools = []string{"github.issues.get"}
		}, []string{CodeBlockedToolListed}},
		"covered by tools prefix": {func(c *Connection) {
			c.Tools = []string{"github.issues.*"}
			c.BlockedTools = []string{"github.issues.get"}
		}, []string{CodeBlockedToolListed}},
		"prefix covered by the same prefix": {func(c *Connection) {
			c.Tools = []string{"github.issues.*"}
			c.BlockedTools = []string{"github.issues.*"}
		}, []string{CodeBlockedToolListed}},
		"prefix only partly listed stays": {func(c *Connection) {
			c.Tools = []string{"github.issues.get"}
			c.BlockedTools = []string{"github.issues.*"}
		}, nil},
		"tools list without the tool": {func(c *Connection) {
			c.Tools = []string{"github.issues.list"}
			c.BlockedTools = []string{"github.issues.get"}
		}, nil},
		"effects connection offering the tool": {func(c *Connection) {
			c.Effects = []Effect{EffectRead}
			c.BlockedTools = []string{"github.issues.get"}
		}, nil},
		"allow-list tool not covered by a tools prefix": {func(c *Connection) {
			c.Tools = []string{"github.actions-permissions.*"}
			c.BlockedTools = []string{"github.actions-permissions.get"}
		}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := blockedProblems(blockedConfig(t, tt.edit))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("blocked_tools problems = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlockedToolsPrefixBlocksAllowListTool(t *testing.T) {
	cfg := blockedConfig(t, func(c *Connection) { c.BlockedTools = []string{"github.actions-permissions.*", "github.issues.get"} })
	for tool, want := range map[string]bool{
		"github.actions-permissions.update": true, "github.actions-permissions.get": true,
		"github.issues.get": true, "github.issues.list": false, "github.comments.list": false,
	} {
		if got := cfg.BlocksTool("gh", tool); got != want {
			t.Errorf("BlocksTool(%s) = %v, want %v", tool, got, want)
		}
	}
	if cfg.BlocksTool("nope", "github.issues.get") {
		t.Error("an unknown connection blocks a tool")
	}
}

func TestBlockedToolsAreWrittenAndKept(t *testing.T) {
	cfg := blockedConfig(t, func(c *Connection) { c.BlockedTools = []string{"github.issues.get", "github.comments.*"} })
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(strings.NewReader(string(data)), cfg.providerCatalog())
	if err != nil {
		t.Fatalf("Decode() = %v\n%s", err, data)
	}
	if got := back.Connections["gh"].BlockedTools; !reflect.DeepEqual(got, []string{"github.issues.get", "github.comments.*"}) {
		t.Errorf("BlockedTools = %v", got)
	}
	if got := back.Version; got != cfg.Version {
		t.Errorf("Version = %v, want %v", got, cfg.Version)
	}
}

func TestEmptyBlockedToolsAreNotWritten(t *testing.T) {
	for name, list := range map[string][]string{"nil": nil, "empty": {}} {
		cfg := blockedConfig(t, func(c *Connection) { c.BlockedTools = list })
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "blocked_tools") {
			t.Errorf("%s list was written:\n%s", name, data)
		}
	}
}

func TestCloneCopiesBlockedToolsDeeply(t *testing.T) {
	cfg := blockedConfig(t, func(c *Connection) { c.BlockedTools = []string{"github.issues.get"} })
	clone := cfg.Clone()
	clone.Connections["gh"].BlockedTools[0] = "changed"
	if cfg.Connections["gh"].BlockedTools[0] != "github.issues.get" {
		t.Error("Clone shares the blocked_tools list")
	}
}

func TestChangeOfBlockedToolsOnlyIsAConnectionChange(t *testing.T) {
	before := blockedConfig(t, func(c *Connection) {})
	after := blockedConfig(t, func(c *Connection) { c.BlockedTools = []string{"github.issues.get"} })
	changes := ChangedConnections(before, after)
	if len(changes) != 1 || changes[0].Name != "gh" || changes[0].Kind != ConnectionChanged {
		t.Errorf("changes = %v", changes)
	}
	if !sameConnection(before.Connections["wiki"], after.Connections["wiki"]) ||
		sameConnection(before.Connections["gh"], after.Connections["gh"]) {
		t.Error("sameConnection does not tell the changed connection apart")
	}
}

func TestResolvedIsUnchangedByBlockedTools(t *testing.T) {
	plain := blockedConfig(t, func(c *Connection) {})
	blocked := blockedConfig(t, func(c *Connection) { c.BlockedTools = []string{"github.issues.get"} })
	a, errA := plain.Resolve("gh", "")
	b, errB := blocked.Resolve("gh", "")
	if errA != nil || errB != nil {
		t.Fatalf("Resolve() = %v, %v", errA, errB)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("Resolved differs:\n%+v\n%+v", a, b)
	}
}

func TestReleaseCoveredBlocks(t *testing.T) {
	cfg := blockedConfig(t, func(c *Connection) {
		c.Tools = []string{"github.issues.get", "github.comments.*"}
		c.BlockedTools = []string{"github.issues.get", "github.comments.create", "github.comments.*",
			"github.issues.*", "github.issues.list", "github.actions-permissions.get", "github.nope"}
	})
	changed := cfg.ReleaseCoveredBlocks()
	if !reflect.DeepEqual(changed, []string{"gh"}) {
		t.Errorf("changed = %v", changed)
	}
	want := []string{"github.issues.*", "github.issues.list", "github.actions-permissions.get", "github.nope"}
	if got := cfg.Connections["gh"].BlockedTools; !reflect.DeepEqual(got, want) {
		t.Errorf("BlockedTools = %v, want %v", got, want)
	}
	if again := cfg.ReleaseCoveredBlocks(); len(again) != 0 {
		t.Errorf("second run changed %v", again)
	}

	all := blockedConfig(t, func(c *Connection) {
		c.Tools = []string{"github.issues.*"}
		c.BlockedTools = []string{"github.issues.get"}
	})
	all.ReleaseCoveredBlocks()
	if got := all.Connections["gh"].BlockedTools; got != nil {
		t.Errorf("BlockedTools = %v, want none", got)
	}
	if err := all.Validate(); err != nil {
		t.Errorf("Validate() after release = %v", err)
	}

	effects := blockedConfig(t, func(c *Connection) { c.BlockedTools = []string{"github.issues.get"} })
	if changed := effects.ReleaseCoveredBlocks(); len(changed) != 0 {
		t.Errorf("a connection by effects lost a block: %v", changed)
	}
}

func TestRenamedToolIsRewrittenInBlockedTools(t *testing.T) {
	text := strings.Replace(renamedConfig,
		"    tools: [acmechat.messages.fetch, acme.chat.messages.list, acmechat.messages.purge]\n",
		"    tools: [acme.chat.messages.list]\n    blocked_tools: [acmechat.messages.fetch]\n", 1)
	cfg, err := Load(writeRenamed(t, text), renameProviders)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Connections["conn"].BlockedTools; !reflect.DeepEqual(got, []string{"acme.chat.messages.get"}) {
		t.Errorf("BlockedTools = %v", got)
	}
	var tools int
	for _, r := range cfg.ConnectionRenamings("conn") {
		if r.Kind == RenamedTool {
			tools++
		}
	}
	if tools != 1 {
		t.Errorf("ConnectionRenamings = %v, want one tool renaming", cfg.ConnectionRenamings("conn"))
	}
}
