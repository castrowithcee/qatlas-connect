package invokelog

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/cryptotest"
	"time"

	"filippo.io/age"
)

// legacyFixture holds day files written under legacyKeyLabel by the release before keyLabel, signed with
// fixtureKey: a signed retention cut, signed and unsigned invokes, a connection change and an mcp exchange
// over three days. They are never rewritten; a test copies them.
const legacyFixture = "testdata/legacy-label"

// fixtureRecipient is the public key of fixtureKey, so a change to how the deterministic identity comes
// about fails here rather than as a mismatching check value.
const fixtureRecipient = "age1y2gxulng8qv84pn9kahajj92q8x5yr7zuaufaj0edm0e22k0afuswad364"

// fixtureKey returns the vault key legacyFixture was signed with and its log key. The identity comes from a
// fixed random stream, so no key text needs to be stored.
func fixtureKey(t *testing.T) (*Key, *age.X25519Identity) {
	t.Helper()
	cryptotest.SetGlobalRandom(t, 981)
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if id.Recipient().String() != fixtureRecipient {
		t.Fatal("the fixed test vault key changed; the fixture can no longer be checked")
	}
	key, err := DeriveKey(id)
	if err != nil {
		t.Fatal(err)
	}
	return key, id
}

// v220Fixture holds day files that release v2.2.0 itself wrote, signed with fixtureKey: a signed retention
// cut, signed and unsigned invokes, an mcp exchange, a rename and an approval over three days with a day
// between them without an entry. generate.sh beside them writes them again from that release's code.
const v220Fixture = "testdata/v2.2.0"

// copyFixture copies legacyFixture into a fresh vault directory and returns it.
func copyFixture(t *testing.T) string {
	t.Helper()
	return copyFixtureFrom(t, legacyFixture)
}

// copyFixtureFrom copies the three day files of fixture into a fresh vault directory and returns it.
func copyFixtureFrom(t *testing.T, fixture string) string {
	t.Helper()
	dir := t.TempDir()
	logs := filepath.Join(dir, logsDirName)
	if err := os.MkdirAll(logs, dirMode); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(fixture, "*.jsonl"))
	if err != nil || len(files) != 3 {
		t.Fatalf("fixture files = %v, %v, want three days", files, err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(logs, filepath.Base(file)), data, fileMode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// labelMAC is the check value of unsigned under the key HKDF derives from id with label, computed here
// from the definition rather than through Key.
func labelMAC(t *testing.T, id *age.X25519Identity, label string, unsigned []byte) string {
	t.Helper()
	h := hmac.New(sha256.New, labelKey(t, id, label))
	h.Write(unsigned)
	return hex.EncodeToString(h.Sum(nil))
}

func TestLegacyFixtureVerifies(t *testing.T) {
	key, id := fixtureKey(t)
	dir := copyFixture(t)

	report := verifyWith(t, dir, key)
	if report.Broken || !report.Checked || len(report.Days) != 3 {
		t.Fatalf("report = %+v, want three intact, checked days", report)
	}
	want := []DayReport{{Date: "2026-03-01", Entries: 3, Unverified: 1}, {Date: "2026-03-02", Entries: 2},
		{Date: "2026-03-04", Entries: 1}}
	for i, day := range report.Days {
		if day != want[i] {
			t.Fatalf("day %d = %+v, want %+v", i, day, want[i])
		}
	}
	// Without a checker the signed retention cut is no unchecked entry; the two signed entries are.
	if report := verifyWith(t, dir, nil); report.Broken || report.Days[0].Unchecked != 2 {
		t.Fatalf("report without a checker = %+v, want an intact chain", report)
	}

	// The fixture is what the former release wrote: no mac_key, every check value under the former label.
	for _, line := range allLines(t, dir) {
		var entry storedLine
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(line, []byte("mac_key")) {
			t.Fatalf("fixture line %s names a log key", line)
		}
		if entry.MAC == "" {
			continue
		}
		unsigned, mac, err := unsignedOf(line)
		if err != nil || string(mac) != labelMAC(t, id, "qatlas invocation log v1", unsigned) {
			t.Fatalf("fixture line %d is not signed under the former label", entry.Seq)
		}
	}
	assertNoKeyMaterial(t, dir, id)
}

// TestAppendAfterLegacyFixtureChangesLabelOnce continues the former release's chain: the entries already
// there stay byte for byte as they were, the first new entry marks the change by naming the current label,
// every later one does too, and the whole chain checks.
func TestAppendAfterLegacyFixtureChangesLabelOnce(t *testing.T) {
	key, id := fixtureKey(t)
	dir := copyFixture(t)
	before := map[string][]byte{}
	for _, name := range []string{"2026-03-01.jsonl", "2026-03-02.jsonl", "2026-03-04.jsonl"} {
		before[name], _ = os.ReadFile(filepath.Join(dir, logsDirName, name))
	}

	signed, moveSigned := signedLogger(t, dir, key, time.Date(2026, 3, 4, 15, 0, 0, 0, time.UTC))
	unsigned, moveUnsigned := clockAt(time.Date(2026, 3, 4, 15, 30, 0, 0, time.UTC))
	unsigned.opts.Dir, unsigned.opts.RetentionDays = logsDir(dir), 90
	mustAppend(t, signed, "op.new.one")
	mustAppend(t, unsigned, "op.new.unsigned")
	moveSigned(time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC))
	moveUnsigned(time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC))
	mustAppend(t, signed, "op.new.two")

	for name, old := range before {
		now, _ := os.ReadFile(filepath.Join(dir, logsDirName, name))
		if !bytes.HasPrefix(now, old) {
			t.Fatalf("%s was rewritten", name)
		}
	}
	lines := allLines(t, dir)
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want the 7 of the fixture and 3 new ones", len(lines))
	}
	legacy, added := lines[:7], lines[7:]
	if !bytes.Contains(added[0], []byte(`,"mac_key":2,"mac":"`)) || !bytes.HasSuffix(added[1], []byte(`"prev_hash":"`+hashHex(added[0])+`","mac":""}`)) ||
		!bytes.Contains(added[2], []byte(`,"mac_key":2,"mac":"`)) {
		t.Fatalf("new lines = %s, want the current label on the signed ones only", added)
	}
	var first storedLine
	_ = json.Unmarshal(added[0], &first)
	if first.Seq != 9 || first.PrevHash != hashHex(legacy[6]) || first.MACKey != currentMACKey {
		t.Fatalf("first new entry = %+v, want it chained from the fixture's last line", first)
	}
	for _, line := range [][]byte{added[0], added[2]} {
		unsigned, mac, err := unsignedOf(line)
		if err != nil || string(mac) != labelMAC(t, id, "qatlas-connect invocation log v1", unsigned) {
			t.Fatalf("line %s is not signed under the current label", line)
		}
	}

	report := verifyWith(t, dir, key)
	if report.Broken || len(report.Days) != 4 {
		t.Fatalf("report = %+v, want four intact days", report)
	}
	for _, day := range report.Days {
		if day.Changed != 0 || day.Unchecked != 0 || day.Missing != 0 || day.Problem != "" {
			t.Fatalf("day = %+v, want every check value valid", day)
		}
	}
	n := 0
	for _, day := range verifyLines(t, dir, key, "", "") {
		for _, line := range day.Lines {
			want := MACNone
			if !bytes.HasSuffix(lines[n], []byte(`,"mac":""}`)) {
				want = MACValid
			}
			n++
			if line.MAC != want || line.Chain != ChainIntact {
				t.Fatalf("%s line %d = %+v, want intact and %v", day.Date, line.Entry.Seq, line, want)
			}
		}
	}
	assertNoKeyMaterial(t, dir, id)
}

func allLines(t *testing.T, dir string) [][]byte {
	t.Helper()
	files, _ := dayFiles(filepath.Join(dir, logsDirName))
	var lines [][]byte
	for _, name := range files {
		lines = append(lines, readLines(t, filepath.Join(dir, logsDirName, name))...)
	}
	return lines
}

func assertNoKeyMaterial(t *testing.T, dir string, id *age.X25519Identity) {
	t.Helper()
	data := bytes.Join(allLines(t, dir), nil)
	for _, secret := range []string{id.String(), strings.ToLower(id.String()),
		hex.EncodeToString(labelKey(t, id, keyLabel)), hex.EncodeToString(labelKey(t, id, legacyKeyLabel))} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("the log holds key material")
		}
	}
}

// signAs is how chainLines signs one line.
type signAs int

const (
	asUnsigned signAs = iota
	asLegacy          // under legacyKeyLabel, as the former release did
	asCurrent         // under keyLabel, as Append does
	// asCurrentClaimingLegacy and asLegacyClaimingCurrent sign under one label while the line names the other.
	asCurrentClaimingLegacy
	asLegacyClaimingCurrent
)

// chainLines writes one line per entry of how, the n-th on day 2026-03-01 plus days[n], chained the way
// Append chains them and signed with the log key of id as how says, and returns the lines.
func chainLines(t *testing.T, dir string, id *age.X25519Identity, how []signAs, days []int) [][]byte {
	t.Helper()
	logs := filepath.Join(dir, logsDirName)
	if err := os.MkdirAll(logs, dirMode); err != nil {
		t.Fatal(err)
	}
	prev := genesisHash
	var lines [][]byte
	for i, as := range how {
		at := time.Date(2026, 3, 1+days[i], 8, i, 0, 0, time.UTC)
		entry := storedLine{Entry: Entry{Seq: uint64(i + 1), Time: at, Path: "cli", Operation: "op.line",
			Effect: "read", Result: "success"}, PrevHash: prev}
		if as == asCurrent || as == asLegacyClaimingCurrent {
			entry.MACKey = currentMACKey
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		switch as {
		case asLegacy, asLegacyClaimingCurrent:
			raw = withMAC(raw, labelKey(t, id, legacyKeyLabel))
		case asCurrent, asCurrentClaimingLegacy:
			raw = withMAC(raw, labelKey(t, id, keyLabel))
		}
		lines = append(lines, raw)
		prev = hashHex(raw)
		if err := appendLines(filepath.Join(logs, at.Format("2006-01-02")+".jsonl"), [][]byte{raw}); err != nil {
			t.Fatal(err)
		}
	}
	return lines
}

// withMAC fills the empty mac field of unsigned with its HMAC under raw key bytes, bypassing Key.sign, the
// way a writer under either label would.
func withMAC(unsigned, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(unsigned)
	out := append([]byte(nil), bytes.TrimSuffix(unsigned, []byte(`""}`))...)
	return append(out, `"`+hex.EncodeToString(h.Sum(nil))+`"}`...)
}

func TestLabelTransitionRules(t *testing.T) {
	key, id := testKey(t)
	const (
		u = asUnsigned
		l = asLegacy
		c = asCurrent
	)
	for _, tt := range []struct {
		name        string
		how         []signAs
		days        []int
		broken      bool
		withoutKey  bool // also broken without a checker
		problemPart string
	}{
		{name: "a new chain starts under the current label", how: []signAs{c, c, u, c}},
		{name: "one change from the former label", how: []signAs{l, u, l, c, u, c}, days: []int{0, 0, 1, 1, 2, 2}},
		{name: "former label after the change", how: []signAs{l, c, c, l}, broken: true, withoutKey: true,
			problemPart: "entry with sequence 4 is signed under the former log key after the current one"},
		{name: "current back to former", how: []signAs{c, l}, broken: true, withoutKey: true,
			problemPart: "entry with sequence 2 is signed under the former log key after the current one"},
		{name: "an unsigned line does not reset the change", how: []signAs{l, c, u, l}, broken: true, withoutKey: true},
		{name: "a later day does not reset the change", how: []signAs{l, c, l}, days: []int{0, 1, 2}, broken: true,
			withoutKey: true},
		{name: "current key behind a line naming the former", how: []signAs{l, asCurrentClaimingLegacy, c}, broken: true,
			problemPart: "sequence 2 does not match its check value"},
		{name: "former key behind a line naming the current", how: []signAs{l, asLegacyClaimingCurrent, c}, broken: true,
			problemPart: "sequence 2 does not match its check value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.days == nil {
				tt.days = make([]int, len(tt.how))
			}
			dir := t.TempDir()
			chainLines(t, dir, id, tt.how, tt.days)
			report := verifyWith(t, dir, key)
			if report.Broken != tt.broken {
				t.Fatalf("report = %+v, want broken %v", report, tt.broken)
			}
			if tt.problemPart != "" && !strings.Contains(problems(report), tt.problemPart) {
				t.Fatalf("problems %q, want %q", problems(report), tt.problemPart)
			}
			if got := verifyWith(t, dir, nil).Broken; got != tt.withoutKey {
				t.Fatalf("broken without a checker = %v, want %v", got, tt.withoutKey)
			}
		})
	}
}

func problems(r Report) string {
	var out []string
	for _, day := range r.Days {
		out = append(out, day.Problem)
	}
	return strings.Join(out, "; ")
}

// TestVerifyDetectsAnAttackOnTheLabelChange changes the line that marks the change, the first one naming
// the current label, every way someone without the vault key could, and expects each to break the chain.
func TestVerifyDetectsAnAttackOnTheLabelChange(t *testing.T) {
	key, id := testKey(t)
	how := []signAs{asLegacy, asLegacy, asCurrent, asCurrent, asCurrent}
	const mark = 2 // the index of the line that marks the change
	for _, tt := range []struct {
		name string
		edit func(lines [][]byte) [][]byte
		// withoutKey is whether the chain alone already shows the break.
		withoutKey bool
	}{
		{name: "mark removed", withoutKey: true, edit: func(lines [][]byte) [][]byte {
			return append(append([][]byte(nil), lines[:mark]...), lines[mark+1:]...)
		}},
		{name: "mark duplicated", withoutKey: true, edit: func(lines [][]byte) [][]byte {
			return append(append([][]byte(nil), lines[:mark+1]...), lines[mark:]...)
		}},
		{name: "mark moved earlier", withoutKey: true, edit: func(lines [][]byte) [][]byte {
			return [][]byte{lines[0], lines[mark], lines[1], lines[3], lines[4]}
		}},
		{name: "mark moved later", withoutKey: true, edit: func(lines [][]byte) [][]byte {
			return [][]byte{lines[0], lines[1], lines[3], lines[mark], lines[4]}
		}},
		{name: "mark's label dropped", withoutKey: true, edit: func(lines [][]byte) [][]byte {
			return replaceAt(lines, mark, bytes.Replace(lines[mark], []byte(`,"mac_key":2`), nil, 1))
		}},
		{name: "mark's label changed", withoutKey: true, edit: func(lines [][]byte) [][]byte {
			return replaceAt(lines, mark, bytes.Replace(lines[mark], []byte(`"mac_key":2`), []byte(`"mac_key":3`), 1))
		}},
		{name: "mark's label moved within the line", withoutKey: true, edit: func(lines [][]byte) [][]byte {
			moved := bytes.Replace(lines[mark], []byte(`,"mac_key":2`), nil, 1)
			return replaceAt(lines, mark, bytes.Replace(moved, []byte(`{"seq":3,`), []byte(`{"seq":3,"mac_key":2,`), 1))
		}},
		{name: "a second mark put on a former line", withoutKey: true, edit: func(lines [][]byte) [][]byte {
			return replaceAt(lines, 1, bytes.Replace(lines[1], []byte(`,"mac":"`), []byte(`,"mac_key":2,"mac":"`), 1))
		}},
		{name: "every line after the mark relabeled as former", withoutKey: true, edit: func(lines [][]byte) [][]byte {
			out := append([][]byte(nil), lines...)
			for i := mark + 1; i < len(out); i++ {
				out[i] = bytes.Replace(out[i], []byte(`,"mac_key":2`), nil, 1)
			}
			return out
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			lines := chainLines(t, dir, id, how, make([]int, len(how)))
			if report := verifyWith(t, dir, key); report.Broken {
				t.Fatalf("intact report = %+v", report)
			}
			writeLines(t, filepath.Join(dir, logsDirName, "2026-03-01.jsonl"), tt.edit(lines))
			if report := verifyWith(t, dir, key); !report.Broken {
				t.Fatalf("report = %+v, want the chain broken", report)
			}
			if got := verifyWith(t, dir, nil).Broken; got != tt.withoutKey {
				t.Fatalf("broken without a checker = %v, want %v", got, tt.withoutKey)
			}
		})
	}

	// A mark on the last line has no successor whose prev_hash would catch a change; its check value does.
	dir := t.TempDir()
	lines := chainLines(t, dir, id, []signAs{asLegacy, asCurrent}, []int{0, 0})
	writeLines(t, filepath.Join(dir, logsDirName, "2026-03-01.jsonl"),
		replaceAt(lines, 1, bytes.Replace(lines[1], []byte(`,"mac_key":2`), nil, 1)))
	if report := verifyWith(t, dir, key); !report.Broken || report.Days[0].Changed != 1 {
		t.Fatalf("report = %+v, want the last line's dropped label changed", report)
	}
	// A label anywhere but right before mac names key ID zero, the former label, under which the check value
	// does not match.
	moved := bytes.Replace(lines[1], []byte(`,"mac_key":2`), nil, 1)
	moved = bytes.Replace(moved, []byte(`{"seq":2,`), []byte(`{"seq":2,"mac_key":2,`), 1)
	writeLines(t, filepath.Join(dir, logsDirName, "2026-03-01.jsonl"), replaceAt(lines, 1, moved))
	for name, checker := range map[string]Checker{"with the key": key, "without a checker": nil} {
		report := verifyWith(t, dir, checker)
		if !report.Broken || report.Days[0].Changed != 1 || !strings.Contains(report.Days[0].Problem, "sequence 2 ") {
			t.Fatalf("%s: report = %+v, want the last line's moved label changed", name, report)
		}
	}
}

// TestVerifyDetectsAMisspelledLabelOnTheLastLine spells the last line's mac_key in another case, which a
// reader that matches member names ignoring case takes for mac_key. No successor's prev_hash covers that
// line, so without a checker only the format of mac_key shows the change.
func TestVerifyDetectsAMisspelledLabelOnTheLastLine(t *testing.T) {
	key, id := testKey(t)
	for name, edit := range map[string]func([]byte) []byte{
		"MAC_KEY": func(l []byte) []byte { return bytes.Replace(l, []byte(`"mac_key":2`), []byte(`"MAC_KEY":2`), 1) },
		"Mac_Key": func(l []byte) []byte { return bytes.Replace(l, []byte(`"mac_key":2`), []byte(`"Mac_Key":2`), 1) },
		"MAC_KEY beside mac_key": func(l []byte) []byte {
			return bytes.Replace(l, []byte(`{"seq":2,`), []byte(`{"seq":2,"MAC_KEY":2,`), 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			lines := chainLines(t, dir, id, []signAs{asCurrent, asCurrent}, []int{0, 0})
			edited := edit(lines[1])
			if bytes.Equal(edited, lines[1]) {
				t.Fatal("the edit changed nothing")
			}
			writeLines(t, filepath.Join(dir, logsDirName, "2026-03-01.jsonl"), replaceAt(lines, 1, edited))
			for checkerName, checker := range map[string]Checker{"with the key": key, "without a checker": nil} {
				report := verifyWith(t, dir, checker)
				if !report.Broken || report.Days[0].Changed != 1 ||
					report.Days[0].Problem != "entry with sequence 2 does not match its check value" {
					t.Fatalf("%s: report = %+v, want the last line changed", checkerName, report)
				}
			}
		})
	}
}

func replaceAt(lines [][]byte, i int, line []byte) [][]byte {
	out := append([][]byte(nil), lines...)
	out[i] = line
	return out
}

// TestNothingIsSignedUnderTheFormerLabel holds Append to the current label, on a new chain and after one
// written under the former label alike: every line it signs names currentMACKey and carries the check value
// under keyLabel, never the one under legacyKeyLabel.
func TestNothingIsSignedUnderTheFormerLabel(t *testing.T) {
	key, id := fixtureKey(t)
	for name, dir := range map[string]string{"new chain": t.TempDir(), "former chain": copyFixtureFrom(t, v220Fixture)} {
		old := len(allLines(t, dir))
		logger, moveTo := signedLogger(t, dir, key, time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
		mustAppend(t, logger, "op.one")
		added := allLines(t, dir)[old:]
		// Retention removes every earlier day, and a retention cut documents it.
		moveTo(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC))
		mustAppend(t, logger, "op.two")
		added = append(added, allLines(t, dir)...)
		if len(added) != 3 || !bytes.Contains(added[1], []byte(`"kind":"retention-cut"`)) {
			t.Fatalf("%s: new lines = %s, want an entry, a retention cut and an entry", name, added)
		}
		for _, line := range added {
			unsigned, mac, err := unsignedOf(line)
			if err != nil || !bytes.HasSuffix(unsigned, []byte(`,"mac_key":2,"mac":""}`)) ||
				string(mac) != labelMAC(t, id, keyLabel, unsigned) ||
				string(mac) == labelMAC(t, id, legacyKeyLabel, unsigned) {
				t.Fatalf("%s: line %s is not signed under the current label only", name, line)
			}
		}
		if report := verifyWith(t, dir, key); report.Broken {
			t.Fatalf("%s: report = %+v, want an intact chain", name, report)
		}
	}
}

// TestV220FixtureVerifiesAndChangesLabelOnce checks what release v2.2.0 wrote, byte for byte as it wrote it,
// continues its chain under the current label, and then refuses every way back to the former label and
// every change to a line of that release.
func TestV220FixtureVerifiesAndChangesLabelOnce(t *testing.T) {
	key, id := fixtureKey(t)
	dir := copyFixtureFrom(t, v220Fixture)
	report := verifyWith(t, dir, key)
	want := []DayReport{{Date: "2026-02-03", Entries: 3, Unverified: 1}, {Date: "2026-02-04", Entries: 2},
		{Date: "2026-02-06", Entries: 1}}
	if report.Broken || !report.Checked || len(report.Days) != len(want) {
		t.Fatalf("report = %+v, want three intact, checked days", report)
	}
	for i, day := range report.Days {
		if day != want[i] {
			t.Fatalf("day %d = %+v, want %+v", i, day, want[i])
		}
	}
	if report := verifyWith(t, dir, nil); report.Broken || report.Days[0].Unchecked != 2 {
		t.Fatalf("report without a checker = %+v, want an intact chain", report)
	}
	fixture := allLines(t, dir)
	for _, line := range fixture {
		if bytes.Contains(line, []byte("mac_key")) {
			t.Fatalf("fixture line %s names a log key", line)
		}
		if unsigned, mac, err := unsignedOf(line); err == nil && len(mac) > 0 &&
			string(mac) != labelMAC(t, id, legacyKeyLabel, unsigned) {
			t.Fatalf("fixture line %s is not signed under the former label", line)
		}
	}

	signed, moveTo := signedLogger(t, dir, key, time.Date(2026, 2, 6, 13, 0, 0, 0, time.UTC))
	mustAppend(t, signed, "op.new.one")
	mustAppend(t, New(dir, 0).WithClock(func() time.Time { return time.Date(2026, 2, 6, 14, 0, 0, 0, time.UTC) }),
		"op.new.unsigned")
	moveTo(time.Date(2026, 2, 7, 9, 0, 0, 0, time.UTC))
	mustAppend(t, signed, "op.new.two")
	lines := allLines(t, dir)
	for i, line := range fixture {
		if !bytes.Equal(lines[i], line) {
			t.Fatalf("fixture line %d was rewritten", i)
		}
	}
	added := lines[len(fixture):]
	if len(added) != 3 || !bytes.HasSuffix(added[1], []byte(`"prev_hash":"`+hashHex(added[0])+`","mac":""}`)) ||
		!bytes.Contains(added[0], []byte(`"seq":9,`)) ||
		!bytes.Contains(added[0], []byte(`"prev_hash":"`+hashHex(fixture[len(fixture)-1])+`"`)) {
		t.Fatalf("new lines = %s, want them chained from the fixture's last line", added)
	}
	for _, line := range [][]byte{added[0], added[2]} {
		unsigned, mac, err := unsignedOf(line)
		if err != nil || !bytes.HasSuffix(unsigned, []byte(`,"mac_key":2,"mac":""}`)) ||
			string(mac) != labelMAC(t, id, keyLabel, unsigned) {
			t.Fatalf("line %s is not signed under the current label", line)
		}
	}
	if report := verifyWith(t, dir, key); report.Broken || len(report.Days) != 4 {
		t.Fatalf("report after the change = %+v, want four intact days", report)
	}
	n := 0
	for _, day := range verifyLines(t, dir, key, "", "") {
		for _, line := range day.Lines {
			want := MACValid
			if bytes.HasSuffix(lines[n], []byte(`,"mac":""}`)) {
				want = MACNone
			}
			if !line.Parsed || line.MAC != want || line.Chain != ChainIntact || line.Missing != 0 {
				t.Fatalf("%s line %d = %+v, want intact and %v", day.Date, line.Entry.Seq, line, want)
			}
			n++
		}
	}

	// A line signed under the former label after the change, chained correctly, is refused with the key and
	// without it.
	back := storedLine{Entry: Entry{Seq: 12, Time: time.Date(2026, 2, 7, 10, 0, 0, 0, time.UTC), Path: "cli",
		Operation: "op.back", Effect: "read", Result: "success"}, PrevHash: hashHex(lines[len(lines)-1])}
	raw, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	raw = withMAC(raw, labelKey(t, id, legacyKeyLabel))
	rolledBack := t.TempDir()
	if err := os.CopyFS(filepath.Join(rolledBack, logsDirName), os.DirFS(filepath.Join(dir, logsDirName))); err != nil {
		t.Fatal(err)
	}
	if err := appendLines(filepath.Join(rolledBack, logsDirName, "2026-02-07.jsonl"), [][]byte{raw}); err != nil {
		t.Fatal(err)
	}
	for name, checker := range map[string]Checker{"with the key": key, "without a checker": nil} {
		report := verifyWith(t, rolledBack, checker)
		last := report.Days[len(report.Days)-1]
		if !report.Broken || last.Changed != 1 ||
			last.Problem != "entry with sequence 12 is signed under the former log key after the current one" {
			t.Fatalf("%s: report = %+v, want the line under the former label refused", name, report)
		}
	}

	// Every change to the lines of v2.2.0 breaks the chain.
	day := filepath.Join(logsDirName, "2026-02-03.jsonl")
	for _, tt := range []struct {
		name string
		edit func(lines [][]byte) [][]byte
	}{
		{"line changed", func(l [][]byte) [][]byte {
			return replaceAt(l, 1, bytes.Replace(l[1], []byte(`"duration_ms":9`), []byte(`"duration_ms":8`), 1))
		}},
		{"unsigned line changed", func(l [][]byte) [][]byte {
			return replaceAt(l, 2, bytes.Replace(l[2], []byte(`"resp_bytes":345`), []byte(`"resp_bytes":346`), 1))
		}},
		{"line removed", func(l [][]byte) [][]byte { return append(append([][]byte(nil), l[:1]...), l[2:]...) }},
		{"lines swapped", func(l [][]byte) [][]byte { return [][]byte{l[0], l[2], l[1], l[3]} }},
		{"line torn", func(l [][]byte) [][]byte { return replaceAt(l, 3, l[3][:len(l[3])/2]) }},
		{"retention cut removed", func(l [][]byte) [][]byte { return l[1:] }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			edited := copyFixtureFrom(t, v220Fixture)
			path := filepath.Join(edited, day)
			writeLines(t, path, tt.edit(readLines(t, path)))
			if report := verifyWith(t, edited, key); !report.Broken {
				t.Fatalf("report = %+v, want the chain broken", report)
			}
			if report := verifyWith(t, edited, nil); !report.Broken {
				t.Fatalf("report without a checker = %+v, want the chain broken", report)
			}
		})
	}
	// A whole day file removed from the middle breaks it as well.
	edited := copyFixtureFrom(t, v220Fixture)
	if err := os.Remove(filepath.Join(edited, logsDirName, "2026-02-04.jsonl")); err != nil {
		t.Fatal(err)
	}
	if report := verifyWith(t, edited, key); !report.Broken || report.Days[1].Missing != 2 {
		t.Fatalf("report = %+v, want two entries missing", report)
	}
}

// TestCountersSkipRetentionCutMarkers holds Unverified and Unchecked to invocations: a retention cut,
// signed or not, counts as neither, with and without a checker.
func TestCountersSkipRetentionCutMarkers(t *testing.T) {
	key, _ := fixtureKey(t)
	for name, sign := range map[string]bool{"unsigned": false, "signed": true} {
		dir := t.TempDir()
		logger, moveTo := clockAt(time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
		logger.opts.Dir, logger.opts.RetentionDays = logsDir(dir), 90
		if sign {
			logger, moveTo = signedLogger(t, dir, key, time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
		}
		mustAppend(t, logger, "op.one")
		moveTo(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC))
		mustAppend(t, logger, "op.two")
		if lines := allLines(t, dir); len(lines) != 2 || !bytes.Contains(lines[0], []byte(`"kind":"retention-cut"`)) {
			t.Fatalf("%s: lines = %s, want a cut and an entry", name, lines)
		}
		want, wantNoKey := DayReport{Date: "2026-09-01", Entries: 1, Unverified: 1}, DayReport{Date: "2026-09-01", Entries: 1, Unverified: 1}
		if sign {
			want.Unverified, wantNoKey.Unverified, wantNoKey.Unchecked = 0, 0, 1
		}
		if report := verifyWith(t, dir, key); report.Broken || report.Days[0] != want {
			t.Fatalf("%s with the key: report = %+v, want %+v", name, report, want)
		}
		if report := verifyWith(t, dir, nil); report.Broken || report.Days[0] != wantNoKey {
			t.Fatalf("%s without a checker: report = %+v, want %+v", name, report, wantNoKey)
		}
	}
}

// markerAppender appends retention-cut markers to path while the check runs, between the two walks of
// VerifyWith, the way a concurrent writer or an attacker with write access could.
type markerAppender struct {
	inner Checker
	path  string
	count int
}

func (m markerAppender) Check(lines [][]byte) ([]bool, error) {
	f, err := os.OpenFile(m.path, os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	for range m.count {
		if _, err := f.WriteString(`{"seq":99,"time":"2026-03-01T00:00:00Z","kind":"retention-cut",` +
			`"cut":{"through_seq":1},"prev_hash":"00","mac":""}` + "\n"); err != nil {
			return nil, err
		}
	}
	return m.inner.Check(lines)
}

// TestCountersIgnoreMarkersAppendedDuringTheCheck holds Unverified to what the first walk counted: markers
// that appear after it are not subtracted, and the count never goes negative.
func TestCountersIgnoreMarkersAppendedDuringTheCheck(t *testing.T) {
	key, _ := fixtureKey(t)
	at := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	for _, added := range []int{0, 1, 2} {
		dir := t.TempDir()
		unsigned, _ := clockAt(at)
		unsigned.opts.Dir, unsigned.opts.RetentionDays = logsDir(dir), 90
		mustAppend(t, unsigned, "op.unsigned")
		signed, _ := signedLogger(t, dir, key, at)
		mustAppend(t, signed, "op.signed")
		path := filepath.Join(dir, logsDirName, "2026-03-01.jsonl")
		report, err := VerifyWith(dir, markerAppender{inner: key, path: path, count: added})
		if err != nil {
			t.Fatal(err)
		}
		day := report.Days[0]
		if day.Entries != 2 || day.Unverified != 1 || day.Unchecked != 0 {
			t.Fatalf("%d markers appended: day = %+v, want two entries and one unverified", added, day)
		}
	}
}

// TestMarkerStatesCountOnlyWhatTheFirstWalkSaw covers the walk without a checker, where no checker can
// append during the check: markers that appear after the first walk, behind a day's last entry or in a day
// file it never saw, are left out, so they never lower Unverified or Unchecked.
func TestMarkerStatesCountOnlyWhatTheFirstWalkSaw(t *testing.T) {
	key, _ := fixtureKey(t)
	dir := t.TempDir()
	logger, moveTo := signedLogger(t, dir, key, time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
	mustAppend(t, logger, "op.one")
	moveTo(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC))
	mustAppend(t, logger, "op.two")
	log, err := openLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := log.Verify(nil)
	if err != nil {
		t.Fatal(err)
	}
	day := filepath.Join(dir, logsDirName, "2026-09-01.jsonl")
	cut := readLines(t, day)[0]
	if !bytes.Contains(cut, []byte(`"kind":"retention-cut"`)) {
		t.Fatalf("first line = %s, want a signed retention cut", cut)
	}
	if err := appendLines(day, [][]byte{cut}); err != nil {
		t.Fatal(err)
	}
	if err := appendLines(filepath.Join(dir, logsDirName, "2026-09-02.jsonl"), [][]byte{cut}); err != nil {
		t.Fatal(err)
	}
	markers, err := markerStates(log, first.Days)
	if err != nil {
		t.Fatal(err)
	}
	if got := markers["2026-09-01"]; len(got) != 1 || got[MACUnchecked] != 1 {
		t.Fatalf("2026-09-01 markers = %v, want only the cut the first walk saw", got)
	}
	if got := markers["2026-09-02"]; len(got) != 0 {
		t.Fatalf("2026-09-02 markers = %v, want none from a day the first walk never saw", got)
	}
}
