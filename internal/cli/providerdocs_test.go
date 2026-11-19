package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every registered provider has a user page the editor links (config.ProviderMetadata.DocPath), and the page
// exists in the repository; every provider folder belongs to a registered provider.
func TestEveryProviderHasItsLinkedDocPage(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "providers")
	linked := map[string]bool{}
	for _, metadata := range defaultRegistry().ProviderMetadataAll() {
		path := metadata.DocPath()
		linked[filepath.Dir(path)] = true
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Errorf("provider %q links %s: %v", metadata.ID, path, err)
		}
		// setup.md is the target as long as it exists.
		if _, err := os.Stat(filepath.Join(root, filepath.Dir(path), "setup.md")); err == nil && filepath.Base(path) != "setup.md" {
			t.Errorf("provider %q links %s although setup.md exists", metadata.ID, path)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && !linked[e.Name()] {
			t.Errorf("docs/providers/%s belongs to no registered provider", e.Name())
		}
	}
}

// Every backticked `qconnect <word>` in a provider page names an existing subcommand of the root command.
func TestProviderDocsNameOnlyExistingCommands(t *testing.T) {
	known := map[string]bool{}
	for _, c := range newRootCommand(&Options{}, sharedRegistry()).Commands() {
		known[c.Name()] = true
		for _, alias := range c.Aliases {
			known[alias] = true
		}
	}
	mention := regexp.MustCompile("`qconnect ([A-Za-z][A-Za-z0-9-]*)[^`]*`")
	root := filepath.Join("..", "..", "docs", "providers")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range mention.FindAllStringSubmatch(line, -1) {
				if !known[m[1]] {
					t.Errorf("%s:%d names `qconnect %s`, which is not a command", path, i+1, m[1])
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
