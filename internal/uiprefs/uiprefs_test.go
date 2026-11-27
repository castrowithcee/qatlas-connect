package uiprefs

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.yaml.in/yaml/v3"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// testSchema is the schema of the editor with its own themes and areas and the kit's standard actions, whose
// rebindable keys are the editor's.
func testSchema(t *testing.T) kitprefs.Schema {
	t.Helper()
	keys, err := kittui.NewKeymap(kittui.StandardActions(), kittui.CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	return Schema([]string{kittui.ThemeTerminal, ThemeQatlasDark, ThemeQatlasLight},
		[]string{"overview", "library", "connections", "activity", "access", "settings"}, keys)
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A file of v2.0.0 names a former colour scheme and holds icons: none without a format marker; it loads as
// before, without a warning.
func TestFileOfV200LoadsWithoutWarning(t *testing.T) {
	schema := testSchema(t)
	for _, tc := range []struct{ file, theme string }{
		{"theme: auto\nstart: overview\nicons: none\nlanguage: english\nmouse: true\n", kittui.ThemeTerminal},
		{"theme: light\nstart: overview\nicons: none\nlanguage: english\nmouse: true\n", ThemeQatlasLight},
		{"theme: dark\nstart: library\nicons: none\nlanguage: english\nmouse: true\n", ThemeQatlasDark},
		{"theme: dark\nbackground: qatlas\nicons: none\n", ThemeQatlasDark},
	} {
		path := kitprefs.Path(t.TempDir())
		writeFile(t, path, tc.file)
		got, warnings := kitprefs.LoadWarnings(path, schema)
		if got.Theme != tc.theme || got.Icons != kitprefs.IconsUnicode || len(warnings) != 0 {
			t.Errorf("Load(%q) = theme %q icons %q %v; want %q, unicode and no warning", tc.file, got.Theme, got.Icons, warnings, tc.theme)
		}
	}
}

// The files v2.2.0 wrote, the default one and one with every preference changed, load without a warning.
func TestFileOfV220LoadsWithoutWarning(t *testing.T) {
	schema := testSchema(t)
	path := kitprefs.Path(t.TempDir())
	writeFile(t, path, "theme: terminal\nstart: overview\nicons: unicode\nlanguage: english\nmouse: true\nformat: 2\n")
	got, warnings := kitprefs.LoadWarnings(path, schema)
	if want := kitprefs.Default(schema); !reflect.DeepEqual(got, want) || len(warnings) != 0 {
		t.Errorf("default file = %+v %v; want %+v", got, warnings, want)
	}
	writeFile(t, path, "theme: qatlas dark\nstart: overview\nicons: nerdfont\nlanguage: deutsch\nmouse: false\nformat: 2\n"+
		"keys:\n  info: f3\n  save: f4\n")
	got, warnings = kitprefs.LoadWarnings(path, schema)
	want := kitprefs.Prefs{Theme: ThemeQatlasDark, Start: DefaultStart, Icons: kitprefs.IconsNerdFont,
		Language: kitprefs.LanguageGerman, Keys: map[kittui.ActionID]string{kittui.ActionInfo: "f3", kittui.ActionSave: "f4"},
		Navigation: kitprefs.NavigationTabs}
	if !reflect.DeepEqual(got, want) || len(warnings) != 0 {
		t.Errorf("changed file = %+v %v; want %+v", got, warnings, want)
	}
}

// Every navigation form loads without a warning; an unknown one warns and falls back to the tab line, which
// is also what a file without the setting gets.
func TestNavigationLoadsEachFormAndFallsBackToTabs(t *testing.T) {
	schema := testSchema(t)
	path := kitprefs.Path(t.TempDir())
	for _, form := range []string{kitprefs.NavigationTabs, kitprefs.NavigationPathLine, kitprefs.NavigationPathHeader,
		kitprefs.NavigationHeaderChoice} {
		writeFile(t, path, "navigation: "+form+"\n")
		if got, warnings := kitprefs.LoadWarnings(path, schema); got.Navigation != form || len(warnings) != 0 {
			t.Errorf("navigation %s = %q %v; want it without a warning", form, got.Navigation, warnings)
		}
	}
	writeFile(t, path, "navigation: sideways\n")
	if got, warnings := kitprefs.LoadWarnings(path, schema); got.Navigation != kitprefs.NavigationTabs || len(warnings) != 1 {
		t.Errorf("unknown navigation = %q %v; want tabs with one warning", got.Navigation, warnings)
	}
	writeFile(t, path, "theme: terminal\n")
	if got, warnings := kitprefs.LoadWarnings(path, schema); got.Navigation != kitprefs.NavigationTabs || len(warnings) != 0 {
		t.Errorf("no navigation = %q %v; want tabs without a warning", got.Navigation, warnings)
	}
}

// A saved none carries the format marker, so the kit keeps it and so does the reader of v2.2.0.
func TestSavedFileCarriesTheFormatForEarlierVersions(t *testing.T) {
	schema := testSchema(t)
	path := kitprefs.Path(t.TempDir())
	prefs := kitprefs.Default(schema)
	prefs.Icons = kitprefs.IconsNone
	if err := kitprefs.Save(path, prefs, schema); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\nformat: 2\n") {
		t.Fatalf("file = %q, want format: 2", data)
	}
	if got := iconsOfV220(t, data); got != kitprefs.IconsNone {
		t.Errorf("v2.2.0 reads icons %q, want none", got)
	}
	if got, warnings := kitprefs.LoadWarnings(path, schema); got.Icons != kitprefs.IconsNone || len(warnings) != 0 {
		t.Errorf("reload = icons %q %v, want none", got.Icons, warnings)
	}
}

// iconsOfV220 is the icons value the reader of v2.2.0 takes from data.
func iconsOfV220(t *testing.T, data []byte) string {
	t.Helper()
	var raw struct {
		Format *int    `yaml:"format"`
		Icons  *string `yaml:"icons"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	icons := kitprefs.IconsUnicode
	if raw.Icons != nil {
		icons = *raw.Icons
		if icons == kitprefs.IconsNone && (raw.Format == nil || *raw.Format < formatVersion) {
			icons = kitprefs.IconsUnicode
		}
	}
	return icons
}

// Two writers that save at once lose nothing: the file holds one save whole, and the backups beside it hold
// the file each save replaced.
func TestConcurrentSavesLoseNothing(t *testing.T) {
	schema := testSchema(t)
	dir := t.TempDir()
	path := kitprefs.Path(dir)
	first := kitprefs.Default(schema)
	if err := kitprefs.Save(path, first, schema); err != nil {
		t.Fatal(err)
	}
	light, dark := first, first
	light.Theme, dark.Theme = ThemeQatlasLight, ThemeQatlasDark
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, p := range []kitprefs.Prefs{light, dark} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = kitprefs.Save(path, p, schema)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	backups, err := filepath.Glob(filepath.Join(dir, "backups", kitprefs.FileName+"-*.bak"))
	if err != nil {
		t.Fatal(err)
	}
	var themes []string
	for _, file := range append(backups, path) {
		got, warnings := kitprefs.LoadWarnings(file, schema)
		if len(warnings) != 0 {
			t.Fatalf("%s: %v", file, warnings)
		}
		themes = append(themes, got.Theme)
	}
	slices.Sort(themes)
	if want := []string{ThemeQatlasDark, ThemeQatlasLight, kittui.ThemeTerminal}; !slices.Equal(themes, want) {
		t.Errorf("file and backups hold the themes %v, want %v", themes, want)
	}
}
