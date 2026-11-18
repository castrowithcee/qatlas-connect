package googlebusinessprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The tools that replace a list with many entries as a whole (a menu, a service list) show a person what the
// replacement changes: the number of entries before and after and the entries that differ. entryList and
// compareLists build that from two lists, and checkBounds refuses a list or text that the bounded decoding of
// this package would silently cut, since a cut list must never be written back.

// entry is one entry of a replaced list with a digest of its own content, children left out.
type entry struct{ key, label, digest string }

// entryList is the flat sequence of the entries of one list at every level.
type entryList struct {
	entries []entry
	nouns   []string
	counts  map[string]int
	seen    map[string]int
}

func newEntryList() *entryList {
	return &entryList{counts: map[string]int{}, seen: map[string]int{}}
}

// add records one entry below parent (the key of its parent, empty at the top) and returns its key, which
// children name as their parent. Two entries with the same name below one parent stay apart by their order.
// noun is the plural counted in the totals, singular the word used when the entry is named.
func (l *entryList) add(noun, singular, parent, name string, content any) string {
	key := singular + "\x00" + parent + "\x00" + name
	n := l.seen[key]
	l.seen[key]++
	key += "#" + strconv.Itoa(n)
	encoded, _ := json.Marshal(content)
	sum := sha256.Sum256(encoded)
	if _, known := l.counts[noun]; !known {
		l.nouns = append(l.nouns, noun)
	}
	l.counts[noun]++
	l.entries = append(l.entries, entry{key: key, label: singular + " " + strconv.Quote(name),
		digest: hex.EncodeToString(sum[:8])})
	return key
}

// listDiff is the difference between two lists.
type listDiff struct {
	nouns                   []string
	before, after           map[string]int
	added, removed, changed []string
	reordered               bool
}

func compareLists(before, after *entryList) listDiff {
	d := listDiff{nouns: slices.Clone(after.nouns), before: before.counts, after: after.counts}
	for _, noun := range before.nouns {
		if !slices.Contains(d.nouns, noun) {
			d.nouns = append(d.nouns, noun)
		}
	}
	held := map[string]entry{}
	for _, e := range before.entries {
		held[e.key] = e
	}
	kept := map[string]bool{}
	for _, e := range after.entries {
		old, ok := held[e.key]
		switch {
		case !ok:
			d.added = append(d.added, e.label)
		case old.digest != e.digest:
			d.changed = append(d.changed, e.label)
			kept[e.key] = true
		default:
			kept[e.key] = true
		}
	}
	var was, now []string
	for _, e := range before.entries {
		if kept[e.key] {
			was = append(was, e.key)
		} else {
			d.removed = append(d.removed, e.label)
		}
	}
	for _, e := range after.entries {
		if kept[e.key] {
			now = append(now, e.key)
		}
	}
	d.reordered = !slices.Equal(was, now)
	return d
}

// empty reports that the replacement changes nothing.
func (d listDiff) empty() bool {
	return len(d.added)+len(d.removed)+len(d.changed) == 0 && !d.reordered
}

// describe phrases the counts before and after and every entry that differs, whole.
func (d listDiff) describe() string {
	parts := make([]string, 0, len(d.nouns))
	for _, noun := range d.nouns {
		parts = append(parts, noun+" "+strconv.Itoa(d.before[noun])+" -> "+strconv.Itoa(d.after[noun]))
	}
	text := strings.Join(parts, ", ")
	for _, group := range []struct {
		name   string
		labels []string
	}{{"added", d.added}, {"changed", d.changed}, {"removed", d.removed}} {
		if len(group.labels) > 0 {
			text += "; " + group.name + ": " + strings.Join(group.labels, ", ")
		}
	}
	if d.reordered {
		text += "; order changed"
	}
	return text
}

// textRule is the bound of a text member: its length in characters and whether it may span lines.
type textRule struct {
	max     int
	oneLine bool
}

// checkBounds walks a JSON value and refuses a list of more than maxItems entries and a text over the rule
// of its member (the name of the member, or of the list that holds it). With strict it refuses control,
// invisible formatting, and separator characters too, other than line breaks and tabs where the rule allows
// lines. Decoding into the bounded types would cut such a value, so what is written is checked first.
func checkBounds(raw []byte, rule func(member string) textRule, strict bool) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("is not valid JSON")
	}
	return walkBounds(value, "", rule, strict)
}

func walkBounds(value any, member string, rule func(string) textRule, strict bool) error {
	switch v := value.(type) {
	case map[string]any:
		for name, inner := range v {
			if err := walkBounds(inner, name, rule, strict); err != nil {
				return err
			}
		}
	case []any:
		if len(v) > maxItems {
			return errors.New("holds a list of more than " + strconv.Itoa(maxItems) + " entries")
		}
		for _, inner := range v {
			if err := walkBounds(inner, member, rule, strict); err != nil {
				return err
			}
		}
	case string:
		r := rule(member)
		if utf8.RuneCountInString(v) > r.max {
			return errors.New("holds a text in " + strconv.Quote(member) + " of more than " + strconv.Itoa(r.max) +
				" characters")
		}
		if !strict {
			return nil
		}
		for _, c := range v {
			if (c == '\n' || c == '\t') && !r.oneLine {
				continue
			}
			if unicode.In(c, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
				return errors.New("holds a text in " + strconv.Quote(member) +
					" with a control, invisible formatting, or separator character")
			}
		}
	}
	return nil
}
