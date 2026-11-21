package connlog

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
)

func configWith(names ...string) *config.Config {
	cfg := &config.Config{Connections: map[string]config.Connection{}}
	for _, name := range names {
		cfg.Connections[name] = config.Connection{Service: "wiki", Credential: "reader"}
	}
	return cfg
}

func logLines(t *testing.T, vaultDir string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(vaultDir, "logs", "*.jsonl"))
	var lines []string
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		f.Close()
	}
	return lines
}

func TestRecordWritesOneValuelessEntryPerChangedConnection(t *testing.T) {
	dir := t.TempDir()
	recorder := &Recorder{Writer: invokelog.New(dir, 90), Surface: SurfaceTUI}
	after := configWith("new", "kept")
	conn := after.Connections["kept"]
	conn.Description = "ignore me"
	after.Connections["kept"] = conn

	if err := recorder.Record(configWith("kept", "gone"), after); err != nil {
		t.Fatalf("Record() = %v", err)
	}
	lines := logLines(t, dir)
	if len(lines) != 3 {
		t.Fatalf("%d entries, want 3: %v", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		`"path":"tui","operation":"config.connection.change","connection":"kept","effect":"update","result":"success"`,
		`"path":"tui","operation":"config.connection.create","connection":"new","effect":"create","result":"success"`,
		`"path":"tui","operation":"config.connection.delete","connection":"gone","effect":"delete","result":"success"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log lacks %s:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "ignore me") || strings.Contains(joined, "wiki") {
		t.Errorf("the log carries a value of a connection:\n%s", joined)
	}
	if !strings.Contains(lines[0], `"mac":""`) {
		t.Errorf("an unsigned entry is not marked unverified: %s", lines[0])
	}
	if err := (&Recorder{Writer: invokelog.New(dir, 90), Surface: SurfaceWeb}).Record(configWith("a"), configWith("a")); err != nil {
		t.Fatalf("Record() without a change = %v", err)
	}
	if got := len(logLines(t, dir)); got != 3 {
		t.Errorf("an unchanged configuration logged: %d entries", got)
	}
}

func TestRecordedEntriesVerifyWhenSigned(t *testing.T) {
	dir := t.TempDir()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	key, err := invokelog.DeriveKey(id)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &Recorder{Writer: invokelog.New(dir, 90).WithKey(key), Surface: SurfaceWeb}
	if err := recorder.Record(configWith(), configWith("a", "b")); err != nil {
		t.Fatal(err)
	}
	report, err := invokelog.VerifyWith(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Broken || !report.Checked {
		t.Fatalf("report = %+v, want an intact, checked chain", report)
	}
	for _, day := range report.Days {
		if day.Entries != 2 || day.Unverified != 0 {
			t.Errorf("day = %+v, want two signed entries", day)
		}
	}
}

func TestRecordFailureIsReturnedAndKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := &Recorder{Writer: invokelog.New(dir, 90), Surface: SurfaceTUI}
	err := recorder.Record(configWith(), configWith("secretish-name"))
	if err == nil || !strings.Contains(err.Error(), "the change was saved") {
		t.Fatalf("Record() = %v, want a warning that the change was saved", err)
	}
	if strings.Contains(err.Error(), "secretish-name") {
		t.Errorf("the warning names the connection: %v", err)
	}
}

func TestRecordRefusesAnInvalidNameWithoutLoggingIt(t *testing.T) {
	dir := t.TempDir()
	recorder := &Recorder{Writer: invokelog.New(dir, 90), Surface: SurfaceWeb}
	bad := "bad\nname"
	err := recorder.Record(configWith(), configWith(bad, "good"))
	if err == nil || strings.Contains(err.Error(), "bad") {
		t.Fatalf("Record() = %v, want a warning without the name", err)
	}
	lines := logLines(t, dir)
	if len(lines) != 1 || !strings.Contains(lines[0], `"connection":"good"`) {
		t.Errorf("log = %v, want only the valid entry", lines)
	}
}

func TestNilRecorderLogsNothing(t *testing.T) {
	var recorder *Recorder
	if err := recorder.Record(configWith(), configWith("a")); err != nil {
		t.Fatal(err)
	}
}

func TestNewRecorderWritesUnderTheVaultNextToTheConfig(t *testing.T) {
	root := t.TempDir()
	recorder := NewRecorder(SurfaceTUI, filepath.Join(root, "config.yaml"), nil, false)
	if err := recorder.Record(configWith(), configWith("a")); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(root, "*", "logs", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("log files = %v, want one under the vault directory", files)
	}
}

func TestRecordedExclusionChangeCarriesNoValue(t *testing.T) {
	dir := t.TempDir()
	before, after := configWith("kept"), configWith("kept")
	conn := after.Connections["kept"]
	conn.Exclude = []string{"repos/o/secret-object"}
	conn.TargetLabels = map[string]string{"repos/o/secret-object": "secret-label"}
	after.Connections["kept"] = conn
	if err := (&Recorder{Writer: invokelog.New(dir, 90), Surface: SurfaceTUI}).Record(before, after); err != nil {
		t.Fatalf("Record() = %v", err)
	}
	lines := logLines(t, dir)
	if len(lines) != 1 || !strings.Contains(lines[0], `"connection":"kept"`) {
		t.Fatalf("entries = %v, want one for the connection", lines)
	}
	if strings.Contains(lines[0], "secret-object") || strings.Contains(lines[0], "secret-label") {
		t.Errorf("the log carries an exclusion: %s", lines[0])
	}
}

func TestRecordRequestWritesOneEntryPerTool(t *testing.T) {
	dir := t.TempDir()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	key, err := invokelog.DeriveKey(id)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &Recorder{Writer: invokelog.New(dir, 90).WithKey(key), Surface: SurfaceCLI}
	const connectionID = "0123456789abcdef0123456789abcdef"
	if err := recorder.RecordRequest(invokelog.OperationRequestCreate, "wiki", connectionID,
		[]string{"wiki.pages.delete", "wiki.books.*"}); err != nil {
		t.Fatalf("RecordRequest() = %v", err)
	}
	lines := logLines(t, dir)
	if len(lines) != 2 {
		t.Fatalf("%d entries, want 2: %v", len(lines), lines)
	}
	for i, tool := range []string{"wiki.pages.delete", "wiki.books.*"} {
		for _, want := range []string{`"operation":"config.request.create"`, `"connection":"wiki"`,
			`"connection_id":"` + connectionID + `"`, `"tool":"` + tool + `"`, `"effect":"create"`} {
			if !strings.Contains(lines[i], want) {
				t.Errorf("entry %d lacks %s: %s", i, want, lines[i])
			}
		}
	}
	report, err := invokelog.VerifyWith(dir, key)
	if err != nil || report.Broken || !report.Checked || report.Days[0].Unverified != 0 {
		t.Fatalf("report = %+v, %v, want signed entries in an intact chain", report, err)
	}

	before := len(lines)
	if err := recorder.RecordRequest(invokelog.OperationRequestCreate, "wiki", connectionID, []string{"free text"}); err == nil {
		t.Error("RecordRequest() with free text = nil, want an error")
	}
	if err := recorder.RecordRequest(invokelog.OperationConnectionCreate, "wiki", connectionID, []string{"wiki.pages.get"}); err == nil {
		t.Error("RecordRequest() with another operation = nil, want an error")
	}
	if got := len(logLines(t, dir)); got != before {
		t.Errorf("an invalid event was logged: %d entries, want %d", got, before)
	}
	if err := (*Recorder)(nil).RecordRequest(invokelog.OperationRequestCreate, "wiki", "", []string{"wiki.pages.get"}); err != nil {
		t.Errorf("nil recorder: %v", err)
	}
}

func TestRecordRequestOverMCPNeedsTheSession(t *testing.T) {
	dir := t.TempDir()
	recorder := &Recorder{Writer: invokelog.New(dir, 90), Surface: "mcp"}
	if err := recorder.RecordRequest(invokelog.OperationRequestCreate, "wiki", "", []string{"wiki.pages.get"}); err == nil {
		t.Fatal("RecordRequest() without a session = nil, want an error")
	}
	session, err := invokelog.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	recorder.Session = session
	if err := recorder.RecordRequest(invokelog.OperationRequestCreate, "wiki", "", []string{"wiki.pages.get"}); err != nil {
		t.Fatalf("RecordRequest() = %v", err)
	}
}
