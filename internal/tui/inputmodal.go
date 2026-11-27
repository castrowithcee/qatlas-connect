package tui

import (
	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// testResultShown reports whether a finished connection test waits to be read and closed.
func (m *Model) testResultShown() bool {
	if m.testing || m.testClass == "" {
		return false
	}
	switch m.screen {
	case screenList:
		return m.section == sectionConnections
	case screenSummary:
		return m.wizard != nil && m.wizard.saved != ""
	}
	return false
}

// testAsk is the result of a connection test as a notice under the name of the connection: the result line
// and, where it helps, the provider guide. Enter and esc close it; every other key waits behind it.
func (m *Model) testAsk() ask {
	line, failed, guide := m.testResult()
	style := kittui.StyleText
	if failed {
		style = kittui.StyleFail
	}
	details := []kittui.DialogLine{{Text: line, Style: style}}
	if guide != "" {
		details = append(details, kittui.DialogLine{Text: tr(tDocGuide), Style: kittui.StyleHint},
			kittui.DialogLine{Text: guide, Style: kittui.StyleHint})
	}
	return ask{title: tConnTestTitle, question: m.testName, details: details, notice: true, keep: actEnter,
		answers: []kittui.Answer{{Action: actEnter, Label: hintLabelID("{enter} or {back} close")},
			{Action: actBack, Label: hintLabelID("{enter} or {back} close")}}}
}

// answerTest closes the result of a connection test.
func (m *Model) answerTest(act action) tea.Cmd {
	if act == actEnter || act == actBack {
		m.testClass = ""
	}
	return nil
}
