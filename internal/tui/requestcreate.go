// The part of the request detail that belongs to a request to create a connection. It is accepted by setting
// the connection up in the guided setup, prefilled with what the request asks for and nothing else, and it is
// closed once that setup saved the connection. Declining goes through the decision of the core like any other
// request; the rules of what a request may ask are the core's (manage.ConnectionPrefill).
package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/manage"
)

// creates reports whether a request asks to create a connection.
func creates(r manage.OpenRequest) bool { return r.Kind == accessrequest.KindCreateConnection }

// loadPrefill reads the prefill of the open request again; it is what the detail shows. A request that
// cannot be read leaves none, and the detail then shows only what the list holds.
func (m *Model) loadPrefill(r manage.OpenRequest) {
	m.requestPrefill = nil
	if !creates(r) {
		return
	}
	if preview, err := m.svc.PreviewRequestDecision(manage.RequestSelection{Request: r.ID}); err == nil {
		m.requestPrefill = preview.Create
	}
}

// createFacts are the rows a request to create a connection adds to the detail: provider, targets and, when
// the request no longer passes the core's check, why only declining is left.
func (m *Model) createFacts(r manage.OpenRequest) []styledFact {
	fact := func(label, value string) styledFact {
		return styledFact{detailFact: detailFact{label: label, value: value}}
	}
	facts := []styledFact{fact(tr(tRequestLblProvider), m.logText(r.Provider))}
	if p := m.requestPrefill; p != nil {
		targets := tr(tRequestNoTargets)
		if len(p.Targets) > 0 {
			targets = m.logText(strings.Join(p.Targets, ", "))
		}
		facts = append(facts, fact(tr(tRequestLblTargets), targets))
		if p.Problem != "" {
			facts = append(facts, styledFact{detailFact: detailFact{value: tr(tRequestCreateProblem, m.logText(p.Problem))}, style: styles.warning})
		}
	}
	return facts
}

// createKeys is the key text of the detail: a request that no longer passes can only be declined.
func (m *Model) createKeys() string {
	if m.requestPrefill != nil && m.requestPrefill.Problem != "" {
		return keysRequestCreateDeclineBody
	}
	return keysRequestCreateBody
}

// acceptCreate opens the guided setup for the request. It reads the request again, so what the setup is
// filled with is what the core holds now, and nothing is written before the person saves.
func (m *Model) acceptCreate(r manage.OpenRequest) tea.Cmd {
	preview, err := m.svc.PreviewRequestDecision(manage.RequestSelection{Request: r.ID})
	if err != nil {
		m.fail = m.redactor.Apply(err.Error())
		m.refreshRequests()
		return nil
	}
	if preview.Create == nil {
		return nil
	}
	if preview.Create.Problem != "" {
		m.requestPrefill = preview.Create
		m.fail = tr(tRequestCreateProblem, m.logText(preview.Create.Problem))
		return nil
	}
	m.closeRequest()
	m.startSetupFromRequest(r.ID, *preview.Create)
	return nil
}

// startSetupFromRequest starts the guided setup past its provider step, on the provider of the request. The
// rows of the later steps are filled by setupPrefillRequest; no credential, service or secret is part of it.
func (m *Model) startSetupFromRequest(id string, prefill manage.ConnectionPrefill) {
	m.clearMessages()
	m.stopTest()
	m.testName = ""
	m.wizard = &setup{provider: prefill.Provider, request: id, create: &prefill}
	m.wizard.pages = [][]field{m.setupPage(stepProvider)}
	m.setupShow(stepService)
	m.status = tr(tRequestSetupStarted)
}

// setupPrefillRequest fills the rows of a step from the request: the name, the targets and the tools list,
// which the access step shows ticked instead of the recommended profile.
func (m *Model) setupPrefillRequest(step int, page []field) {
	p := m.wizard.create
	switch step {
	case stepScope:
		for i := range page {
			switch page[i].label {
			case nameLabel:
				page[i].input.SetValue(p.Name)
			case targetsLabel:
				page[i] = targetsField(slices.Clone(p.Targets)).withHint(page[i].hint).withDetail(page[i].detail).inSection(page[i].section)
			}
		}
	case stepEffects:
		for _, row := range m.accessFields(m.wizard.provider, nil, slices.Clone(p.Tools)) {
			for i := range page {
				if page[i].label == row.label {
					page[i] = row.inSection(page[i].section)
				}
			}
		}
	}
}

// requestClosedMsg carries the outcome of closing the request after the setup saved its connection.
type requestClosedMsg struct {
	result manage.RequestDecisionResult
	err    error
}

// closeCreated closes the request as accepted under the name the connection was saved with, which the person
// may have changed. The save has just run through the admin session, so the proof is that session's.
func (m *Model) closeCreated(request, saved string) tea.Cmd {
	proof, _ := m.admin.Authorize(m.adminTimeout())
	svc := m.svc
	return func() tea.Msg {
		res, err := svc.CloseConnectionRequest(proof, request, saved)
		return requestClosedMsg{result: res, err: err}
	}
}

// handleRequestClosed reports it. A failure is a warning only: the connection stays saved and the request
// stays open until it is declined or accepted again.
func (m *Model) handleRequestClosed(msg requestClosedMsg) {
	m.refreshRequests()
	if msg.err != nil {
		m.status += tr(tNoteWarning, tr(tRequestCreateNotClosed, m.redactor.Apply(msg.err.Error())))
		return
	}
	m.status += tr(tRequestCreateClosed)
	for _, w := range msg.result.Warnings {
		m.status += tr(tNoteWarning, m.redactor.Apply(w))
	}
}
