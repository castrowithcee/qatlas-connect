package tui

import (
	"os"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// typing reports whether the current screen reads typed text right now, as a dialog with an input does.
// Letters are text there, so neither the vim movement keys, ? nor : mean anything else.
func (m *Model) typing() bool {
	if m.keyCapture {
		return true
	}
	if q, ok := m.currentAsk(); ok && q.input {
		return true
	}
	switch m.screen {
	case screenPicker:
		return true
	case screenList:
		return m.list.Searching() || m.barValuesOpen()
	case screenForm:
		if m.payloadRemoveAsk || m.payloadSaving || m.focus >= len(m.fields) {
			return false
		}
		switch f := m.fields[m.focus]; f.kind {
		case fieldText, fieldEnvName, fieldMasked, fieldPayloadNew:
			return !f.readOnly
		}
	case screenTargets:
		if m.targetPick != nil {
			return m.targetPick.top().list.Searching()
		}
		return m.targetEdit >= 0 || m.targetAdd != nil || m.labelEdit
	case screenTransfer:
		return m.export != nil && m.export.step == exportSelect && m.export.list.Searching()
	case screenLogs:
		return m.logs.list.Searching() || m.logs.bar != nil && m.logs.bar.Open()
	}
	return false
}

// helpReachable reports whether ? opens the help (and : the command line) on the current screen: wherever nothing is typed, and on
// the screens whose search line is always there, while that line is empty. A form keeps its letters for
// its text rows, so only the question inside it is covered.
func (m *Model) helpReachable() bool {
	switch m.screen {
	case screenList:
		// The open values and the modal of the filter bar take every key.
		return !m.typing() && !m.barOpen()
	case screenForm:
		return m.payloadRemoveAsk
	case screenTargets:
		if m.targetPick != nil {
			return !m.typing()
		}
		if m.targetAdd != nil {
			return m.targetAdd.query() == ""
		}
	}
	return !m.typing()
}

// helpF9Pressed reports whether the key is the second way of the help, which also reaches it from a text line
// where ? is a character. A dialog with an input keeps it, as a dialog of the kit does.
func (m *Model) helpF9Pressed(key tea.KeyPressMsg) bool {
	return keymap.Is(key, actHelpF9, kit.InputText) && !m.askingInput()
}

// askingInput reports whether the current question is a dialog with an input line.
func (m *Model) askingInput() bool {
	q, ok := m.currentAsk()
	return ok && q.input
}

// letterReachable reports whether a letter key (i, c) is a command on the current screen. A search line is
// text from its first character on, so unlike ? and : no letter acts there, even while the line is empty.
func (m *Model) letterReachable() bool {
	switch m.screen {
	case screenForm:
		return m.payloadRemoveAsk
	}
	return !m.typing()
}

// moveKey turns the vim keys into the arrow keys on a screen that reads no text, so
// every screen moves the same way and no handler needs to know the letters.
func (m *Model) moveKey(key tea.KeyPressMsg) tea.KeyPressMsg {
	if m.typing() {
		return key
	}
	return vimArrow(key)
}

// vimArrow is the arrow key a vim letter stands for, and any other key as it is.
func vimArrow(key tea.KeyPressMsg) tea.KeyPressMsg {
	for _, d := range []struct {
		act  action
		code rune
	}{{actVimLeft, tea.KeyLeft}, {actVimDown, tea.KeyDown}, {actVimUp, tea.KeyUp}, {actVimRight, tea.KeyRight}} {
		if keyIs(key, d.act) {
			return tea.KeyPressMsg{Code: d.code}
		}
	}
	return key
}

// pasteCode is the code of the key a paste travels as; no key of a keyboard has it.
const pasteCode rune = -1

// pasteKey is a paste as the key it travels as through the key handlers of the screens. It is named after the
// pasted text in brackets, so no key binding matches it, and inputMsg hands it to a text field as a paste.
func pasteKey(content string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: pasteCode, Text: "[" + content + "]"}
}

// pasted returns the text of a paste key and whether key is one.
func pasted(key tea.KeyPressMsg) (string, bool) {
	if key.Code != pasteCode || len(key.Text) < 2 {
		return "", false
	}
	return key.Text[1 : len(key.Text)-1], true
}

// inputMsg is what a text field is handed for key: a paste key as the paste it stands for, and alt with a
// character the field binds nothing to as that character, which a field has always typed.
func inputMsg(key tea.KeyPressMsg) tea.Msg {
	if content, ok := pasted(key); ok {
		return tea.PasteMsg{Content: content}
	}
	if key.Text != "" || key.Mod&^tea.ModShift != tea.ModAlt || !unicode.IsPrint(key.Code) || inputBinds(key) {
		return key
	}
	if key.Mod.Contains(tea.ModShift) {
		return textKey(string(unicode.ToUpper(key.Code)))
	}
	return textKey(string(key.Code))
}

// legacyKey names a key as the terminal library always named it where its input reader now names it
// otherwise: the shifted F1 to F8 of xterm-like terminals are F13 to F20, as a stored key binding knows them.
func legacyKey(key tea.KeyPressMsg) tea.KeyPressMsg {
	if key.Mod&^tea.ModAlt == tea.ModShift && key.Code >= tea.KeyF1 && key.Code <= tea.KeyF8 {
		key.Code += tea.KeyF13 - tea.KeyF1
		key.Mod &^= tea.ModShift
	}
	return key
}

// textKey is the key press of typed text.
func textKey(text string) tea.KeyPressMsg {
	code := tea.KeyExtended
	if runes := []rune(text); len(runes) == 1 {
		code = runes[0]
	}
	return tea.KeyPressMsg{Code: code, Text: text}
}

// keyboardModes removes what Bubble Tea writes to switch the keyboard enhancements of the terminal on, off, or
// to ask about them: modifyOtherKeys and the Kitty keyboard protocol. With them a terminal reports keys such as
// ctrl+i, shift+enter or the keypad's enter under names of their own; without them every key keeps the name
// the legacy encoding gives it, the name every key binding, stored ones included, was taken under.
var keyboardModes = strings.NewReplacer(ansi.SetModifyOtherKeys2, "", ansi.ResetModifyOtherKeys, "",
	ansi.PushKittyKeyboard(1), "", ansi.PopKittyKeyboard(1), "", ansi.RequestKittyKeyboard, "")

// legacyKeys is the terminal output of the editor without the keyboard enhancements. Bubble Tea writes each
// frame, and each set of terminal modes, in one write, so no sequence is split between two.
type legacyKeys struct{ f *os.File }

func (o legacyKeys) Write(p []byte) (int, error) {
	if _, err := o.f.WriteString(keyboardModes.Replace(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (o legacyKeys) Read(p []byte) (int, error) { return o.f.Read(p) }
func (o legacyKeys) Close() error               { return o.f.Close() }
func (o legacyKeys) Fd() uintptr                { return o.f.Fd() }
