package invokelog

import (
	"fmt"

	"github.com/castrowithcee/qatlas-kit/auditlog"
)

// DayReport is one day file's part of a Report.
type DayReport struct {
	Date string
	// Entries counts the invocation entries of the day; a retention-cut marker does not count as one.
	Entries int
	// Unverified counts the invocation entries of the day whose MAC is empty: written without the log key,
	// which is expected while the vault is locked without a vault process, unencrypted, or absent, and
	// never breaks the chain by itself.
	Unverified int
	// Unchecked counts the invocation entries of the day that carry a MAC nobody checked, because Verify
	// was given no Checker.
	Unchecked int
	// Changed counts the lines of the day, entries and retention-cut markers alike, whose MAC is malformed,
	// does not match the line, or is one under the former label after one under the current label. Each
	// one breaks the chain.
	Changed int
	// Missing counts the sequence numbers skipped right before an entry of this day that no retention cut
	// documents: entries deleted from the middle, or a whole day file.
	Missing uint64
	// Problem names the first way this day's entries fail to chain from what precedes them, or fail their
	// check value, or "" when neither happens.
	Problem string
}

// Report is the result of Verify: one row per day file that exists, in date order.
type Report struct {
	Days []DayReport
	// Checked is true when the check values were checked, which takes a Checker.
	Checked bool
	// Broken is true when the chain is broken somewhere that is not a documented retention cut: a changed
	// or deleted entry, a day file missing from the middle, or a check value that does not match.
	Broken bool
}

// VerifyWith checks the hash chain and, with checker, the check values of every day file under
// vaultDir/logs; see auditlog.Log.Verify for the rules. A signed line under the former label after the
// first one under the current label counts as changed, with or without a checker, as does a signed line
// whose mac_key does not stand right before mac. A missing logs
// directory is an installation that never invoked anything yet, an intact, empty chain.
func VerifyWith(vaultDir string, checker Checker) (Report, error) {
	log, err := openLog(vaultDir)
	if err != nil {
		return Report{}, err
	}
	r, err := log.Verify(checker)
	if err != nil {
		return Report{}, err
	}
	// The kit counts markers in Unverified and Unchecked; qconnect counts only entries. A second walk
	// without a checker tells which of those lines are markers; see markerStates.
	markers, err := markerStates(log, r.Days)
	if err != nil {
		return Report{}, err
	}
	report := Report{Checked: r.Checked, Broken: r.Broken}
	for _, d := range r.Days {
		unverified, unchecked := max(d.Unverified-markers[d.Date][MACNone], 0), d.Unchecked
		if checker == nil {
			unchecked = max(unchecked-markers[d.Date][MACUnchecked], 0)
		}
		report.Days = append(report.Days, DayReport{Date: d.Date, Entries: d.Entries, Unverified: unverified,
			Unchecked: unchecked, Changed: d.Changed, Missing: d.Missing, Problem: problemText(d)})
	}
	return report, nil
}

// markerStates counts, per day, the marker lines by their check value state, as a walk without a checker
// sees it: MACNone for an empty check value, MACUnchecked for one nobody checked. The files are read
// again after the first walk, so only the lines that walk can have seen count: those up to the last of the
// day's first Entries entries. A marker appended since is left out, and an unaccounted one stays counted
// as unverified or unchecked rather than hiding an entry.
func markerStates(log *auditlog.Log[content], first []auditlog.DayReport) (map[string]map[MACState]int, error) {
	entries := make(map[string]int, len(first))
	for _, d := range first {
		entries[d.Date] = d.Entries
	}
	days, err := log.VerifyLines(nil, "", "")
	if err != nil {
		return nil, err
	}
	out := map[string]map[MACState]int{}
	for _, d := range days {
		counts := map[MACState]int{}
		seen := 0
		for _, l := range d.Lines {
			if seen == entries[d.Date] {
				break
			}
			switch {
			case !l.Parsed:
			case l.Record.Kind != "":
				counts[l.MAC]++
			default:
				seen++
			}
		}
		out[d.Date] = counts
	}
	return out, nil
}

// problemText is how qconnect names the first problem of a day.
func problemText(d auditlog.DayReport) string {
	switch d.Problem {
	case auditlog.ProblemNone:
		return ""
	case auditlog.ProblemUnparsable:
		return "a line does not parse as a log entry"
	case auditlog.ProblemGenesis:
		return "the first entry does not chain from the genesis hash"
	case auditlog.ProblemMissing:
		return fmt.Sprintf("entries before sequence %d are missing without a retention cut", d.ProblemSeq)
	case auditlog.ProblemKeyIDChange:
		return fmt.Sprintf("entry with sequence %d is signed under the former log key after the current one", d.ProblemSeq)
	case auditlog.ProblemChain:
		return fmt.Sprintf("entry with sequence %d does not chain from the previous entry", d.ProblemSeq)
	case auditlog.ProblemMAC:
		return fmt.Sprintf("entry with sequence %d does not match its check value", d.ProblemSeq)
	}
	return fmt.Sprintf("entry with sequence %d: %s", d.ProblemSeq, d.Problem)
}

// MACState is what a check found about one line's check value.
type MACState = auditlog.MACState

// The MAC states of a line.
const (
	MACNone      = auditlog.MACNone
	MACUnchecked = auditlog.MACUnchecked
	MACValid     = auditlog.MACValid
	MACInvalid   = auditlog.MACInvalid
)

// ChainState is where, if anywhere, the hash chain breaks at one line.
type ChainState = auditlog.ChainState

// The chain states of a line.
const (
	ChainIntact   = auditlog.ChainIntact
	ChainUnparsed = auditlog.ChainUnparsed
	ChainGenesis  = auditlog.ChainGenesis
	ChainNext     = auditlog.ChainNext
	ChainPrevious = auditlog.ChainPrevious
)

// LineResult is what VerifyLines found for one line of a day file.
type LineResult struct {
	// Entry is the line decoded; the zero Entry for a line that does not parse.
	Entry Entry
	// Parsed is false for a line that does not parse as a log entry; MAC then says nothing about it.
	Parsed bool
	MAC    MACState
	Chain  ChainState
	// Missing counts the sequence numbers skipped right before this line that no retention cut documents.
	Missing uint64
}

// DayLines holds the LineResult of every line of one day file, in file order.
type DayLines struct {
	Date  string
	Lines []LineResult
}

// VerifyLines walks the chain exactly as VerifyWith does, across every day file, but reports what it found
// line by line for the day files from from through to (YYYY-MM-DD, inclusive; "" leaves that end open),
// in date order. Only the check values of those days are handed to checker, so a narrow range asks the
// vault process for no more than it shows. A missing logs directory holds no line.
func VerifyLines(vaultDir string, checker Checker, from, to string) ([]DayLines, error) {
	log, err := openLog(vaultDir)
	if err != nil {
		return nil, err
	}
	days, err := log.VerifyLines(checker, from, to)
	if err != nil {
		return nil, err
	}
	var out []DayLines
	for _, d := range days {
		lines := make([]LineResult, len(d.Lines))
		for i, l := range d.Lines {
			lines[i] = LineResult{Parsed: l.Parsed, MAC: l.MAC, Chain: l.Chain, Missing: l.Missing}
			if l.Parsed {
				lines[i].Entry = entryOf(l.Record)
			}
		}
		out = append(out, DayLines{Date: d.Date, Lines: lines})
	}
	return out, nil
}
