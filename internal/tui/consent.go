package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
)

// credentialDeletedMsg is the outcome of deleting a credential that holds a consent.
type credentialDeletedMsg struct {
	name   string
	before manage.ApprovalSnapshot
	out    manage.CredentialDeleted
	err    error
}

// deleteConsentCredential deletes a credential that holds a consent through the core, which revokes its
// refresh token at the provider first. The revocation reaches the network, so it runs as a command and the
// editor stays usable meanwhile.
func (m *Model) deleteConsentCredential(name string, before manage.ApprovalSnapshot) tea.Cmd {
	m.problems = problemState{}
	m.screen = screenList
	m.writes++
	m.busy = tr(tSecretRemovingTheStoredSecretFor, name, config.RoleRefreshToken)
	svc, cfg, rev := m.svc, m.cfg, m.rev
	return func() tea.Msg {
		out, err := svc.DeleteCredential(context.Background(), cfg, rev, name)
		return credentialDeletedMsg{name: name, before: before, out: out, err: err}
	}
}

func (m *Model) handleCredentialDeleted(msg credentialDeletedMsg) tea.Cmd {
	if m.writes > 0 {
		m.writes--
	}
	if m.writes == 0 {
		m.busy = ""
	}
	note := revocationNote(msg.out.Revocation, msg.err)
	if msg.err != nil {
		if !m.conflicted(msg.err) {
			m.reportSaveError(msg.err, msg.name, false)
		}
		if note != "" {
			m.fail += "; " + note
		}
		return nil
	}
	m.adoptSaved(msg.out.Config)
	cmd := m.returnToList("")
	m.status = tr(tFormDeleted, msg.name)
	m.addWarning(note)
	m.addWarning(msg.out.ProcessWarning)
	m.addWarning(msg.out.Warning)
	return tea.Batch(cmd, m.autoApprove(msg.before, ""))
}
