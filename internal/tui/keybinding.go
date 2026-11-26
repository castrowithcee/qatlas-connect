package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	kit "github.com/castrowithcee/qatlas-kit/tui"
)

// keyRows are the key rows of the General form: the label of a row, the action it binds, its one-line hint,
// and the explanation behind it.
var keyRows = []struct {
	label        string
	action       action
	hint, detail textID
}{
	{"save key", actSave, tKeySaveHint, tKeySaveDetail},
	{"help key", actHelp, tKeyHelpHint, tKeyHelpDetail},
	{"command key", actCommand, tKeyCommandHint, tKeyCommandDetail},
	{"lock key", actLock, tKeyLockHint, tKeyLockDetail},
	{"info key", actInfo, tKeyInfoHint, tKeyInfoDetail},
}

// standardKey is the key a key row holds without a binding of its own: the key of the table, and none for
// the second info key.
func standardKey(a action) string {
	if a == actInfo {
		return ""
	}
	return tableKey(a)
}

// keyFields are the key rows of the General form for the keys that differ from the standard.
func keyFields(keys map[action]string) []field {
	var fields []field
	for _, r := range keyRows {
		bound, ok := keys[r.action]
		if !ok {
			bound = standardKey(r.action)
		}
		fields = append(fields, field{label: r.label, kind: fieldKey, action: string(r.action), bound: bound,
			detail: tr(r.detail) + tr(tKeyDetailSuffix)}.withHint(tr(r.hint)))
	}
	return fields
}

// keysOfForm reads the keys the key rows of the form hold, the standard ones included.
func (m *Model) keysOfForm() map[action]string {
	keys := map[action]string{}
	for _, f := range m.fields {
		if f.kind == fieldKey {
			keys[action(f.action)] = f.bound
		}
	}
	return keys
}

// keyRowValue is how a key row shows its binding: the key by name, and where the key is the standard or
// none, said in words, so colour carries nothing.
func keyRowValue(f field) string {
	switch {
	case action(f.action) == actInfo && f.bound == "":
		return tr(tKeyInfoOnly, tableKey(actInfo))
	case action(f.action) == actInfo:
		return tr(tKeyInfoAnd, tableKey(actInfo), shownKey(f))
	case f.bound == standardKey(action(f.action)):
		return tr(tKeyStandard, shownKey(f))
	}
	return shownKey(f)
}

// shownKey is the key of a key row as a footer would name it. The row holds a key that is not bound yet, which
// the keymap can only label once it holds it, so it is bound in a copy of the table; a key the table refuses
// stays as typed.
func shownKey(f field) string {
	a := action(f.action)
	km, err := newKeymap(keyTable)
	if err != nil || km.Bind(map[action]string{a: f.bound}) != nil {
		return f.bound
	}
	if a == actInfo {
		label, _ := km.Labels(kit.InputText).KeyLabel(a)
		return label
	}
	return km.Label(a)
}

// keyRowKey handles the keys of a focused key row: enter waits for the new key, d removes the own binding so the standard applies again.
func (m *Model) keyRowKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	f := &m.fields[m.focus]
	switch {
	case keyIs(key, actEnter):
		m.clearMessages()
		m.keyCapture = true
		m.status = tr(tKeyPressStatus, trOr(textID("keyaction."+f.action), strings.TrimSuffix(f.label, " key")))
		return nil, true
	case keyIs(key, actDelete):
		m.clearMessages()
		f.bound = standardKey(action(f.action))
		return nil, true
	}
	return nil, false
}

// captureKey takes the next key press as the binding of the focused key row. It runs ahead of every global
// key, so ?, :, F2, and ctrl+l can be bound too; only a rule keeps a key from being taken, and the refusal
// names it.
func (m *Model) captureKey(key tea.KeyPressMsg) tea.Cmd {
	if _, ok := pasted(key); ok {
		return nil
	}
	m.keyCapture = false
	if m.focus < 0 || m.focus >= len(m.fields) || m.fields[m.focus].kind != fieldKey {
		return nil
	}
	m.clearMessages()
	if keyIs(key, actBack) {
		m.status = tr(tKeyKept)
		return nil
	}
	f := &m.fields[m.focus]
	// The other rows of the form are what the key must not collide with, so a rebinding in several steps
	// checks against what is shown, not against what was saved.
	name := key.String()
	if name == keyLabel(actSpace) {
		// CheckBinding trims a blank key to "", which info accepts as "no second key", so a space would
		// silently clear it; refuse it here with the rule's wording for every action.
		m.fail = tr(tKeySpaceRefused)
		return nil
	}
	others := m.keysOfForm()
	delete(others, action(f.action))
	chosen, err := keymap.CheckBinding(action(f.action), name, others)
	if err != nil {
		m.fail = err.Error()
		return nil
	}
	f.bound = chosen
	return nil
}
