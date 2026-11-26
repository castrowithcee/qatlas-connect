package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The command line (':') runs the editor's recurring actions by name. It is a register, not a second set of
// keys: every command calls the function its key already calls, and a command that is not possible right
// now says why instead of doing nothing. A further command is one more entry in commands().

// command is one entry of the register.
type command struct {
	name string
	// desc is the description, formatted with args.
	desc    textID
	args    []any
	aliases []string
	// hidden keeps the command out of the empty line's list; it still matches what is typed.
	hidden bool
	// act is the action whose key already does the same, and key the position of that one key where each key
	// of act does another thing; a command without such a key has an action of its own. area tells apart the
	// commands that share act.
	act  action
	key  int
	area kittui.AreaID
	// icon is the meaning of the glyph shown before the name; iconNone where there is none.
	icon iconID
	// unavailable returns the reason the command cannot run on m.screen now, or "" where it can.
	unavailable func(m *Model) string
	// run performs the action on the screen the command line was opened from.
	run func(m *Model) tea.Cmd
}

// commands is the register, in the order an empty line lists it.
func (m *Model) commands() []command {
	var list []command
	for a := area(0); a < areaCount; a++ {
		title := strings.ToLower(a.key())
		first := areaSections[a][0]
		list = append(list, command{
			name:        "go " + title,
			desc:        tCmdGoArea,
			args:        []any{a.title()},
			icon:        areaIcons[a],
			act:         actArea,
			key:         int(a) + 1,
			area:        kittui.AreaID(a.key()),
			aliases:     []string{title},
			unavailable: onScreens("switch areas", screenNav, screenList, screenLogs),
			run:         func(m *Model) tea.Cmd { return m.openSection(first) },
		})
		for _, s := range areaSections[a] {
			// The sub-tabs keep the names the sections had before the areas, so older habits still work; the
			// empty line lists only the areas.
			name := strings.ToLower(s.key())
			if name == title {
				continue
			}
			aliases := []string{name}
			if name == "update" {
				// The bare word belongs to the command that installs the release.
				aliases = nil
			}
			list = append(list, command{
				name:        "go " + name,
				desc:        tCmdGoSection,
				args:        []any{a.title(), s.title()},
				aliases:     aliases,
				hidden:      true,
				unavailable: onScreens("switch areas", screenNav, screenList, screenLogs),
				run:         func(m *Model) tea.Cmd { return m.openSection(s) },
			})
		}
	}
	list = append(list, m.selftestCommands()...)
	return append(list,
		command{
			name:        "setup",
			desc:        tCmdSetup,
			act:         actSetup,
			aliases:     []string{"guided setup", "new connection"},
			unavailable: onScreens("start the setup", screenNav, screenList),
			run:         func(m *Model) tea.Cmd { m.askSetupTemplate(); return nil },
		},
		command{
			name:    "add exclusion",
			desc:    tCmdAddExclusion,
			aliases: []string{"exclude", "exclusion"},
			// Only the target list carries it, so the empty line of other screens does not list it.
			hidden:      true,
			unavailable: func(m *Model) string { return m.addExclusionUnavailable() },
			run:         func(m *Model) tea.Cmd { m.setAsExclusion(true); return nil },
		},
		command{
			name:    "renames",
			desc:    tCmdRenames,
			aliases: []string{"adopt", "rename leftovers"},
			unavailable: func(m *Model) string {
				if r := onScreens("review renames", screenList)(m); r != "" {
					return r
				}
				switch m.section {
				case sectionApprovals, sectionConnections, sectionCredentials:
					return ""
				}
				return tr(tCmdRenamesWhere)
			},
			run: func(m *Model) tea.Cmd { return m.openFindings() },
		},
		command{
			name:    "update",
			desc:    tCmdUpdate,
			act:     actUpdate,
			aliases: []string{"upgrade"},
			unavailable: func(m *Model) string {
				if r := onScreens("update", screenNav, screenList)(m); r != "" {
					return r
				}
				return m.updateUnavailable()
			},
			run: func(m *Model) tea.Cmd { m.askUpdate(); return nil },
		},
		command{
			name:    "version",
			desc:    tCmdVersion,
			aliases: []string{"versions", "rollback", "downgrade", "prerelease"},
			unavailable: func(m *Model) string {
				if r := onScreens("choose a version", screenNav, screenList)(m); r != "" {
					return r
				}
				return m.versionsUnavailable()
			},
			run: func(m *Model) tea.Cmd { return m.openVersions() },
		},
		command{
			name:        "export",
			desc:        tCmdExport,
			aliases:     []string{"export config", "backup"},
			unavailable: onScreens("export", screenNav, screenList),
			run:         func(m *Model) tea.Cmd { return m.askExport() },
		},
		command{
			name:        "import",
			desc:        tCmdImport,
			aliases:     []string{"import config", "restore"},
			unavailable: onScreens("import", screenNav, screenList),
			run:         func(m *Model) tea.Cmd { return m.askImport() },
		},
		command{
			name:        "view heatmap",
			desc:        tCmdHeat,
			aliases:     []string{"heatmap"},
			unavailable: onScreens("change the view", screenLogs),
			run:         func(m *Model) tea.Cmd { return m.setHeat(true) },
		},
		command{
			name:        "view table",
			desc:        tCmdTable,
			aliases:     []string{"table"},
			unavailable: onScreens("change the view", screenLogs),
			run:         func(m *Model) tea.Cmd { return m.setHeat(false) },
		},
		command{
			name:        "lock vault",
			desc:        tCmdLock,
			act:         actLock,
			aliases:     []string{"lock"},
			unavailable: (*Model).lockUnavailable,
			run:         func(m *Model) tea.Cmd { return m.handleVaultLockKey() },
		},
		command{
			name:        "help",
			desc:        tCmdHelp,
			act:         actHelp,
			unavailable: func(m *Model) string { return "" },
			run:         func(m *Model) tea.Cmd { m.openHelp(); return nil },
		},
		command{
			name:        "quit",
			desc:        tCmdQuit,
			act:         actQuit,
			aliases:     []string{"exit"},
			unavailable: onScreens("quit", screenNav, screenList, screenLogs),
			run:         func(m *Model) tea.Cmd { return m.quit() },
		},
	)
}

// onScreens is the availability of a command whose key works on these screens only.
func onScreens(what string, screens ...screen) func(m *Model) string {
	return func(m *Model) string {
		for _, s := range screens {
			if m.screen == s {
				return ""
			}
		}
		return tr(tCmdCannot, trOr(textID("cmdwhat."+what), what))
	}
}

// updateUnavailable is why u would do nothing now.
func (m *Model) updateUnavailable() string {
	switch {
	case m.updater == nil:
		return tr(tUpdateBuildCannot)
	case m.updating:
		return tr(tUpdateRunning)
	case m.updated != "":
		return tr(tUpdateAlreadyDone)
	case !m.release.UpdateAvailable:
		return tr(tUpdateNoneKnown)
	}
	return ""
}

// openCommandLine opens the command line over the current screen, which decides where each command acts.
func (m *Model) openCommandLine() {
	list := m.commands()
	kitList := make([]kittui.Command, len(list))
	for i, c := range list {
		act := c.act
		if act == "" {
			act = action("command." + strings.ReplaceAll(c.name, " ", "-"))
		}
		icon := m.icon(c.icon)
		if kittui.CheckIcon(icon) != nil {
			icon = ""
		}
		kitList[i] = kittui.Command{Name: c.name, Aliases: c.aliases, Icon: icon, Description: c.desc,
			DescriptionArgs: c.args, Action: act, Key: c.key, Area: c.area, Hidden: c.hidden,
			Available: func(kittui.Context) (bool, string) {
				reason := c.unavailable(m)
				return reason == "", reason
			}}
	}
	line, err := kittui.NewCommandLine(kittui.CommandLineSpec{
		Commands: kitList,
		Keys: kittui.CommandLineKeys{Up: actUp, Down: actDown, Next: actNext, Prev: actPrev, Enter: actEnter,
			Back: actBack},
		CloseOutside: true,
	}, keymap, translator(false))
	if err != nil {
		m.fail = err.Error()
		return
	}
	area, tab, _ := m.kitActive()
	line.SetContext(kittui.Context{Area: area, Tab: tab, Focus: kittui.InputNavigation})
	cl := &commandModal{CommandLine: line}
	m.clearMessages()
	m.showModal(cl, func(action) tea.Cmd {
		m.closeModal()
		for _, c := range list {
			if c.name == cl.chosen {
				return c.run(m)
			}
		}
		return nil
	})
}

// commandModal is the kit's command line as a modal. It answers with the action of the command it ran, or
// with back where it closed without one, and keeps the name of that command in chosen.
type commandModal struct {
	*kittui.CommandLine
	chosen string
}

func (c *commandModal) Update(msg tea.KeyPressMsg) (action, bool) {
	return c.answer(c.CommandLine.Update(msg))
}

func (c *commandModal) Click(r kittui.MouseResult) (action, bool) {
	return c.answer(c.CommandLine.Click(r))
}

// Input is the focus of the line's keys: it reads text, so a letter is never a key.
func (c *commandModal) Input() kittui.InputFocus { return kittui.InputText }

func (c *commandModal) answer(cmd kittui.Command, done bool) (action, bool) {
	if !done {
		return "", false
	}
	c.chosen = cmd.Name
	if cmd.Name == "" {
		return actBack, true
	}
	return cmd.Action, true
}

// commandLine is the command line open over the screen, or nil.
func (m *Model) commandLine() *kittui.CommandLine {
	if c, ok := m.modal.(*commandModal); ok {
		return c.CommandLine
	}
	return nil
}
