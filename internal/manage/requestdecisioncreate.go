package manage

import (
	"errors"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
)

// ConnectionPrefill is what a request to create a connection puts into the setup of a connection, as data.
// It holds no credential and no service: the person chooses those. Tools are the requested entries (tool
// IDs or prefixes) for the connection's tools list, never effects. Reason is cleaned text of the requester.
type ConnectionPrefill struct {
	Provider string
	Name     string
	Targets  []string
	Tools    []string
	// Entries is Tools with the group the provider sorts each into.
	Entries []RequestTool
	Reason  string
	// Problem says why the request no longer passes the check a new request passes (a tool or target is gone).
	// The person can still decline it; setting it up from it is then pointless. Empty when it holds.
	Problem string
}

// previewCreate is the prefill of a request to create a connection. It re-checks the request like a new one
// (without the secret check, which Env of the manager does not carry) and reports a request that no longer
// passes as Problem, which leaves the person able to decline it.
func (s *Service) previewCreate(d *decisionState) (RequestPreview, error) {
	r := d.req
	problem := ""
	if _, err := accessrequest.CheckCreate(d.env, accessrequest.CreateInput{
		Provider: r.Provider, Name: r.Connection, Targets: r.Targets, Tools: r.Tools,
	}); err != nil {
		problem = s.redact(err.Error())
	}
	metadata, _ := d.cfg.ProviderMetadata(r.Provider)
	return RequestPreview{
		Request: r.ID, Kind: r.Kind,
		Create: &ConnectionPrefill{
			Provider: r.Provider, Name: r.Connection, Targets: append([]string(nil), r.Targets...),
			Tools: append([]string(nil), r.Tools...), Entries: d.entries(metadata), Reason: s.cleanText(r.Reason, accessrequest.MaxReasonRunes),
			Problem: problem,
		},
	}, nil
}

// declineCreate closes a whole request to create a connection. Accepting and blocking are refused: accepting
// is the setup of the TUI, which saves the connection first (see CloseConnectionRequest).
func (s *Service) declineCreate(d *decisionState, action RequestAction) (RequestDecisionResult, error) {
	if action != RequestDecline {
		return RequestDecisionResult{}, decisionError(RequestCodeSetupInTUI,
			"a request to create a connection is accepted by setting the connection up in 'qconnect tui' and "+
				"cannot be blocked; decline it here or leave it open")
	}
	events, err := s.requests.Queue.Settle(d.env, d.req.ID, d.selected, nil)
	if errors.Is(err, accessrequest.ErrNotOpen) {
		return RequestDecisionResult{}, decisionError(RequestCodeNotFound, "the request is no longer open")
	}
	if err != nil {
		return RequestDecisionResult{}, err
	}
	result := RequestDecisionResult{Action: RequestDecline, Request: d.req.ID, Connection: d.name, Decided: d.selected}
	s.recordCreateDecision(&result, invokelog.OperationRequestConnectionDecline, d.name, "", d.selected, events)
	return result, nil
}

func (s *Service) recordCreateDecision(result *RequestDecisionResult, operation, name, connID string, tools []string, events []accessrequest.Event) {
	recorder := connlog.NewRecorder(s.surface, s.store.Path(), s.vault(), s.processOK)
	if err := recorder.RecordRequest(operation, name, connID, tools); err != nil {
		result.Warnings = append(result.Warnings, s.redact("warning: "+err.Error()))
	}
	result.Warnings = append(result.Warnings, s.recordRequestEvents(recorder, events)...)
}

// CloseConnectionRequest closes a request to create a connection as accepted, after the person saved the
// connection named saved in the setup of the TUI. The name may differ from the requested one. It refuses,
// with RequestCodeNotSaved, when no such connection exists or it belongs to another provider than the
// request, and with RequestCodeNotFound when the request is no longer open. It closes the whole request,
// logs config.request.connection.accept per tool with the saved name and the requested tool entries, and
// never logs the reason. Like a decision it needs the admin proof of an encrypted vault.
func (s *Service) CloseConnectionRequest(proof AdminProof, request, saved string) (RequestDecisionResult, error) {
	if s.requests.Queue == nil {
		return RequestDecisionResult{}, decisionError(RequestCodeUnavailable, "tool requests are not available here")
	}
	if s.surface != connlog.SurfaceTUI {
		return RequestDecisionResult{}, decisionError(RequestCodeUnavailable, "a request to create a connection is accepted in the TUI")
	}
	required, _, err := AdminRequired(s.vault())
	if err != nil || required && !proof.granted {
		return RequestDecisionResult{}, decisionError(RequestCodeAdminRequired, "deciding a request needs the vault passphrase first")
	}
	cfg, _, err := s.store.LoadVersioned()
	if err != nil {
		return RequestDecisionResult{}, err
	}
	env := accessrequest.Env{Config: cfg, Tools: s.requests.Tools, Now: s.requestNow()}
	req, events, err := s.requests.Queue.CloseCreated(env, request, saved)
	switch {
	case errors.Is(err, accessrequest.ErrNotOpen):
		return RequestDecisionResult{}, decisionError(RequestCodeNotFound, "the request is no longer open")
	case errors.Is(err, accessrequest.ErrConnectionMissing):
		return RequestDecisionResult{}, decisionError(RequestCodeNotSaved, "no connection of this name is saved")
	case errors.Is(err, accessrequest.ErrProviderMismatch):
		return RequestDecisionResult{}, decisionError(RequestCodeNotSaved, "the saved connection belongs to another provider than the request")
	case err != nil:
		return RequestDecisionResult{}, err
	}
	result := RequestDecisionResult{Action: RequestAccept, Request: req.ID, Connection: saved, Decided: req.Tools}
	s.recordCreateDecision(&result, invokelog.OperationRequestConnectionAccept, saved, cfg.Connections[saved].ID, req.Tools, events)
	return result, nil
}
