package tui

import (
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/helptopics"
	kithelp "github.com/castrowithcee/qatlas-kit/helptopics"
	"github.com/castrowithcee/qatlas-kit/i18n"
	kit "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// The editor's own texts live in a catalog of typed keys per language (text_keys.go, text_en.go,
// text_de.go). A screen that is not translated yet keeps its English literals; translating one means adding
// its keys to both catalogs and replacing the literals with tr. Form row labels and choice values stay the
// internal keys of the forms (field.label, field.choices); they are translated only where they are drawn
// (labelText, valueText), and a label or value without an entry is drawn as it is.

// textID is the key of one text of the catalog.
type textID = kit.TextID

// Keys that are built from a name instead of being declared as constants.
func labelID(english string) textID       { return textID("label." + english) }
func valueID(label, choice string) textID { return textID("value." + label + "." + choice) }
func footerID(group string) textID        { return textID("footer." + group) }
func termID(id string) textID             { return textID(id) }
func columnID(title string) textID        { return textID("column." + title) }
func termTitleID(id string) textID        { return textID(id + ".title") }
func wordID(english string) textID        { return textID("word." + english) }
func sectionID(name string) textID        { return textID("section." + name) }
func entryID(kind string) textID          { return textID("entry." + kind) }

var catalogs = i18n.Catalogs[textID]{i18n.English: textEN, i18n.German: textDE}

// keyLabels resolves the key placeholders of the texts against the keymap in force at each call, so a
// rebinding shows without a new Translator. With extraInfo the info placeholder names the second info key,
// the one that also acts on text lines, as long as there is one.
type keyLabels struct{ extraInfo bool }

func (l keyLabels) KeyLabel(id action) (string, bool) {
	if l.extraInfo && id == actInfo {
		if extra := infoExtraLabel(); extra != "" {
			return extra, true
		}
	}
	return keymap.KeyLabel(id)
}

// translators are the Translators of the language in force: one for the keys of a screen that names the
// info key by its first key, one for a screen where the info key is only reachable by its second key.
var translators struct {
	lang        i18n.Lang
	plain, info kit.Translator
}

// translator returns the Translator of uiLang, built anew when the language changed.
func translator(extraInfo bool) kit.Translator {
	if translators.plain == nil || translators.lang != uiLang {
		plain, err := kit.NewTranslator(uiLang, catalogs, keyLabels{})
		if err != nil {
			panic("the text catalog is invalid: " + err.Error())
		}
		info, _ := kit.NewTranslator(uiLang, catalogs, keyLabels{extraInfo: true})
		translators.lang, translators.plain, translators.info = uiLang, plain, info
	}
	if extraInfo {
		return translators.info
	}
	return translators.plain
}

func init() {
	// The English words of a term are shared with the CLI and the manpage and stay in helptopics.
	for _, t := range helptopics.Terms() {
		textEN[termID(t.ID)] = t.Text
		textEN[termTitleID(t.ID)] = t.Title
	}
	// The label of a key group is the group's text without its keys, for the kit's hints, which name keys and
	// label apart.
	for id, group := range textEN {
		name, ok := strings.CutPrefix(string(id), "footer.")
		if !ok {
			continue
		}
		for _, catalog := range []map[textID]string{textEN, textDE} {
			text := catalog[id]
			if text == "" {
				text = group
			}
			if _, label := hintParts(text); label != "" {
				catalog[hintLabelID(name)] = label
			}
		}
	}
}

// uiLang is the language the editor draws in. Like the theme it is process-wide: one editor runs per
// process, and the text helpers are called from places that hold no model.
var uiLang = i18n.English

// setLanguage makes the preference the language of every text drawn from now on.
func setLanguage(pref string) {
	uiLang = kitprefs.Language(pref)
}

// tr is the text of id in the current language; args are formatted into it. A text names a key by the
// placeholder of its action ("{enter}"), resolved against the keys in force before the arguments are
// formatted in, so a rebinding shows and an argument that happens to hold braces stays as it is.
func tr(id textID, args ...any) string { return translator(false).Lookup(id, args...) }

// trOr is the text of id, or fallback when no catalog holds one.
func trOr(id textID, fallback string) string {
	if text, ok := translator(false).Find(id); ok {
		return text
	}
	return fallback
}

// labelText is how a form row label is drawn.
func labelText(english string) string { return trOr(labelID(english), english) }

// valueText is how a choice value of the row with the given label is drawn.
func valueText(rowLabel, choice string) string {
	if rowLabel == startLabel {
		if a, ok := areaByName(choice); ok {
			return strings.ToLower(a.title())
		}
	}
	return trOr(valueID(rowLabel, choice), choice)
}

// footerResolve translates the groups of a key line, separated by " · ", and resolves their key placeholders
// with t. A group the catalog does not hold, such as one built from a name, is not translated; the keys in
// it still resolve, because the Translator resolves the placeholders of an unknown ID as it returns it.
func footerResolve(t kit.Translator, text string) string {
	groups := strings.Split(text, " · ")
	for i, group := range groups {
		id := footerID(group)
		if _, ok := t.Find(id); !ok {
			id = textID(group)
		}
		groups[i] = t.Lookup(id)
	}
	return strings.Join(groups, " · ")
}

// footerKeys is footerResolve for a key line outside a screen's footer.
func footerKeys(text string) string { return footerResolve(translator(false), text) }

// footerKeys is the footer text of a screen as drawn: the info placeholder names the key that reaches the
// explanation of the focused row.
func (m *Model) footerKeys(text string) string {
	return footerResolve(translator(infoExtraLabel() != "" && !m.infoReachable()), text)
}

// termText is the explanation of a term, and termName its title.
func termText(t kithelp.Term) string { return trOr(termID(t.ID), t.Text) }
func termName(t kithelp.Term) string { return trOr(termTitleID(t.ID), t.Title) }

// wordText is how a word or short phrase is drawn that the editor takes from its own tables: the titles of
// a list bar, the labels of the detail pane, the words of an empty row. An unknown word is drawn as it is.
func wordText(english string) string { return trOr(wordID(english), english) }

// sectionText is the name of a form section as drawn; the sections themselves stay the English keys.
func sectionText(name string) string { return trOr(sectionID(name), name) }

// entryWord is what an entry of the given kind ("service", "default", ...) is called, in the language of the
// editor.
func entryWord(kind string) string { return trOr(entryID(kind), kind) }

// warningLine is a warning drawn under a row or a list.
func warningLine(text string) string { return tr(tWarningPrefix, text) }

// columnText is how a column title of a list is drawn.
func columnText(title string) string { return trOr(columnID(title), title) }

// kindText is the kind of a renamed entry ("service", "credential", "connection") as the sentence of the
// editor's language wants it: the German nouns are capitalised.
func kindText(kind string) string {
	if uiLang == i18n.English || kind == "" {
		return kind
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}
