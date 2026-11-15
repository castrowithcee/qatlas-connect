package github

import (
	"sort"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
)

func TestRegistryHoldsExactlyTheCurrentToolIDs(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range reg.All() {
		got = append(got, d.ID)
	}
	sort.Strings(got)
	want := expectedToolIDs(allExpectedTools)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("registry holds %d tools, differing from the expected %d IDs", len(got), len(want))
	}
	if err := reg.ValidateRenames(); err != nil {
		t.Errorf("ValidateRenames() = %v", err)
	}
}

func TestRenamedToolsCoverEveryChangedID(t *testing.T) {
	if len(renamedTools) != 115 {
		t.Errorf("renamedTools has %d entries, want 115", len(renamedTools))
	}
	current := map[string]bool{}
	for _, id := range expectedToolIDs(allExpectedTools) {
		current[id] = true
	}
	for old, next := range renamedTools {
		if current[old] || !current[next] {
			t.Errorf("rename %s -> %s: old must be gone and new registered", old, next)
		}
		if strings.ReplaceAll(next, "-", "") != old {
			t.Errorf("rename %s -> %s changes more than word boundaries", old, next)
		}
	}
	// Every ID with a hyphen in a tool segment is the target of exactly one rename.
	targets := map[string]bool{}
	for _, next := range renamedTools {
		targets[next] = true
	}
	// A tool registered with a hyphen from the start has no former ID.
	bornHyphenated := map[string]bool{}
	for _, id := range expectedToolIDs(func(tool expectedTool) bool { return tool.born }) {
		bornHyphenated[id] = true
	}
	for id := range current {
		if strings.Contains(id, "-") && !targets[id] && !bornHyphenated[id] {
			t.Errorf("hyphenated tool %s has no former ID", id)
		}
	}
}

func TestFormerGitHubToolIDsLoadWithWarnings(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	document := `version: 1
services:
  gh:
    provider: github
    base_url: https://api.github.com
credentials:
  gh-reader:
    provider: github
    type: env
    values:
      token: QATLAS_GH_TOKEN
connections:
  planning:
    service: gh
    credential: gh-reader
    tools: [github.projectitems.list, github.pullrequestreviews.list, github.workflowruns.usage]
`
	cfg, err := config.Decode(strings.NewReader(document), reg)
	if err != nil {
		t.Fatal(err)
	}
	warnings := strings.Join(cfg.ToolWarnings(), "\n")
	for old, next := range map[string]string{
		"github.projectitems.list":       "github.project-items.list",
		"github.pullrequestreviews.list": "github.pull-request-reviews.list",
		"github.workflowruns.usage":      "github.workflow-runs.usage",
	} {
		if !strings.Contains(warnings, `tool "`+old+`" was renamed to "`+next+`"`) {
			t.Errorf("warnings lack the rename %s -> %s:\n%s", old, next, warnings)
		}
	}
}
