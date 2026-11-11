package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/uiprefs"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// termColors are the colours the terminal answered; empty until it has.
var termColors kittui.TerminalColors

// terminalColourQuery asks the terminal for its foreground and background where the theme mixes from them.
// Nothing is asked of a terminal whose colours the theme could not use.
func terminalColourQuery(name string) tea.Cmd {
	if !themeSet.AsksTerminalColors(name, currentProfile()) {
		return nil
	}
	return tea.Batch(tea.RequestForegroundColor, tea.RequestBackgroundColor)
}

// takeTerminalColour keeps a colour the terminal answered and draws the terminal theme anew with it.
func (m *Model) takeTerminalColour(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.ForegroundColorMsg:
		if msg.Color == nil {
			return
		}
		termColors.Foreground = kittui.ColorOf(msg.Color)
	case tea.BackgroundColorMsg:
		if msg.Color == nil {
			return
		}
		termColors.Background = kittui.ColorOf(msg.Color)
	}
	if themeOrDefault(m.prefs.Theme) == uiprefs.ThemeTerminal {
		applyTheme(uiprefs.ThemeTerminal)
	}
}
