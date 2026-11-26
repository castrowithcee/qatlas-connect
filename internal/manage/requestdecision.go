package manage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/approval"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
)

// RequestAction is what a person decides on tools of an open request.
type RequestAction string

// The decisions.
const (
	// RequestAccept offers the tools on the connection.
	RequestAccept RequestAction = "accept"
	// RequestDecline closes the tools of the request and changes nothing else.
	RequestDecline RequestAction = "decline"
	// RequestBlock closes the tools and records them in the connection's blocked_tools.
	RequestBlock RequestAction = "block"
)

// AcceptMode is how accepting changes a connection that offers its tools by effect. A connection with a
// tools list needs no mode.
type AcceptMode string

// The ways to accept on a connection by effect.
const (
	// AcceptAddEffects adds the effects of the selected tools, so every tool of those effects is offered,
	// future ones included.
	AcceptAddEffects AcceptMode = "add-effects"
	// AcceptSwitchToList replaces the effects by a tools list of every tool offered today plus the selected
	// ones. Future tools no longer join by themselves.
	AcceptSwitchToList AcceptMode = "switch-to-list"
)

// The codes of a *RequestDecisionError.
const (
	RequestCodeAdminRequired = "admin-required"
	RequestCodeNotFound      = "not-found"
	RequestCodeInvalid       = "invalid-request"
	RequestCodeNeedsList     = "needs-tool-list"
	RequestCodeUnavailable   = "unavailable"
	// RequestCodeSetupInTUI refuses to accept or block a request to create a connection outside the setup
	// of the TUI.
	RequestCodeSetupInTUI = "setup-in-tui"
	// RequestCodeNotSaved refuses to close a request to create a connection when no connection of its
	// provider was saved.
	RequestCodeNotSaved = "connection-not-saved"
)

// RequestDecisionError is why a decision was refused before it changed anything. Code is one of the
// RequestCode constants. The message never carries a request's reason text.
type RequestDecisionError struct {
	Code    string
	Message string
}

func (e *RequestDecisionError) Error() string { return e.Message }

func decisionError(code, format string, args ...any) error {
	return &RequestDecisionError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// RequestSource is what deciding needs besides the configuration: the queue of open requests, every
// registered tool, and the clock (nil is time.Now).
type RequestSource struct {
	Queue *accessrequest.Queue
	Tools []accessrequest.Tool
	Now   func() time.Time
}

// WithRequests returns a copy of s that decides the requests of src.
func (s *Service) WithRequests(src RequestSource) *Service {
	c := *s
	c.requests = src
	return &c
}

// RequestSelection names an open request and the entries of it a decision covers. Tools holds entries of
// the request exactly as it lists them (a tool ID or a prefix); empty means all of them.
type RequestSelection struct {
	Request string
	Tools   []string
}

// RequestDecision is one decision on a selection. Mode is needed to accept on a connection by effect.
type RequestDecision struct {
	RequestSelection
	Action RequestAction
	Mode   AcceptMode
}

// RequestTool is an entry of a request with the group the provider sorts it into, empty where the provider
// has no groups.
type RequestTool struct {
	Tool       string
	Group      string
	GroupTitle string
}

// EffectGain is the number of tools an effect newly offers, with the same effect of tools registered later.
type EffectGain struct {
	Effect config.Effect
	Tools  int
}

// RequestPreview is what a decision on a selection would do, as data. Entries are the selected entries that
// are still requestable now; Dropped are those that are not (offered or blocked meanwhile, or the tool is
// gone) and fall away with the decision.
type RequestPreview struct {
	Request    string
	Kind       accessrequest.Kind
	Connection string
	// ByEffects is true for a connection that offers its tools by effect, false for one with a tools list.
	ByEffects bool
	Entries   []RequestTool
	Dropped   []string
	// NewTools is the number of tools accepting offers that they were not before, for a tools list.
	NewTools int
	// AddEffects lists, for a connection by effect, the effects accepting would add and the tools each brings.
	// Tools registered later with such an effect join by themselves. It is empty when AddEffectsBlockedBy is
	// not.
	AddEffects []EffectGain
	// AddEffectsBlockedBy lists the entries that only an exact tools-list entry can release, so that adding
	// effects is not possible and the connection has to be switched to a list.
	AddEffectsBlockedBy []string
	// SwitchTools is the number of tools the connection offers after switching to a tools list. Tools
	// registered later no longer join by themselves.
	SwitchTools int
	// Remaining is, for a request to remove tools, the number of tools the connection offers after accepting;
	// for a connection by effect that is the switch to a tools list, which accepting then requires.
	Remaining int
	// SplitPrefixes are, for a request to remove tools from a tools list, the prefix entries accepting
	// replaces by exact IDs, so tools added later under those prefixes no longer join by themselves.
	SplitPrefixes []string
	// EmptyList is true when accepting leaves the connection with a tools list that offers no tool.
	EmptyList bool
	// ApprovalExpected is true when accepting also approves the connection: its credential is in an
	// encrypted vault.
	ApprovalExpected bool
	// Create is set for a request to create a connection and is then the only other field besides Request
	// and Kind that is filled.
	Create *ConnectionPrefill
}

// ApprovalState is what happened to the approval of an accepted connection.
type ApprovalState string

// The states.
const (
	// ApprovalNotNeeded: the connection reads no credential of an encrypted vault.
	ApprovalNotNeeded ApprovalState = "not-needed"
	// ApprovalGranted: the change was approved in the same step.
	ApprovalGranted ApprovalState = "approved"
	// ApprovalStaysOpen: the change is saved, but the connection is open under Approvals.
	ApprovalStaysOpen ApprovalState = "open"
)

// RequestApproval is the approval half of an accept. Detail says why it stays open and carries no secret.
type RequestApproval struct {
	State  ApprovalState
	Detail string
}

// RequestDecisionResult is the outcome of a decision. The decision counts as made when it returns without
// an error, also when Approval stays open or Warnings is not empty.
type RequestDecisionResult struct {
	Action     RequestAction
	Request    string
	Connection string
	Mode       AcceptMode
	// Decided are the entries the decision applied to.
	Decided []string
	// Dropped are selected entries that were no longer requestable and were removed without a change.
	Dropped []string
	// Remaining are the entries that stay open in the request.
	Remaining []string
	Approval  RequestApproval
	// Warnings are redacted notes: a failed log entry, a vault process hint, a queue that could not be
	// updated after the configuration was saved.
	Warnings []string
}

// decisionState is the request and configuration a decision or its preview is computed from.
type decisionState struct {
	cfg      *config.Config
	rev      config.Revision
	env      accessrequest.Env
	req      accessrequest.Request
	name     string
	provider string
	// selected are the entries that passed the re-check, dropped those that did not.
	selected []string
	dropped  []string
}

func (s *Service) requestNow() time.Time {
	if s.requests.Now != nil {
		return s.requests.Now()
	}
	return time.Now()
}

// openDecision loads the configuration and the request and re-checks the selection against the connection
// as it is now with accessrequest.CheckKind, the check a new request of its kind passes. Nothing is written.
func (s *Service) openDecision(sel RequestSelection) (*decisionState, error) {
	if s.requests.Queue == nil {
		return nil, decisionError(RequestCodeUnavailable, "tool requests are not available here")
	}
	cfg, rev, err := s.store.LoadVersioned()
	if err != nil {
		return nil, err
	}
	d := &decisionState{cfg: cfg, rev: rev, env: accessrequest.Env{Config: cfg, Tools: s.requests.Tools, Now: s.requestNow()}}
	req, gone, err := s.requests.Queue.Lookup(d.env, sel.Request)
	if errors.Is(err, accessrequest.ErrNotOpen) {
		return nil, decisionError(RequestCodeNotFound, "there is no open request with this ID")
	}
	if err != nil {
		return nil, err
	}
	d.req, d.name = req, req.Connection
	if req.Kind == accessrequest.KindCreateConnection {
		// It decides as a whole and names no connection yet, so there is nothing to re-check per entry.
		if len(sel.Tools) > 0 {
			return nil, decisionError(RequestCodeInvalid, "a request to create a connection is decided as a whole, not per tool")
		}
		d.provider, d.selected = req.Provider, slices.Clone(req.Tools)
		return d, nil
	}
	d.provider = cfg.Services[cfg.Connections[d.name].Service].Provider
	entries := sel.Tools
	if len(entries) == 0 {
		entries = req.Tools
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry] {
			continue
		}
		seen[entry] = true
		switch {
		case slices.Contains(gone, entry):
			d.dropped = append(d.dropped, entry)
		case !slices.Contains(req.Tools, entry):
			return nil, decisionError(RequestCodeInvalid, "tool %q is not part of this request", shortEntry(entry))
		default:
			if _, err := accessrequest.CheckKind(d.env, req.Kind, d.name, []string{entry}); err != nil {
				d.dropped = append(d.dropped, entry)
				continue
			}
			d.selected = append(d.selected, entry)
		}
	}
	sort.Strings(d.selected)
	sort.Strings(d.dropped)
	return d, nil
}

func shortEntry(entry string) string {
	if r := []rune(entry); len(r) > 80 {
		return string(r[:79]) + "…"
	}
	return entry
}

// ownTools are the registered tools of the connection's provider.
func (d *decisionState) ownTools() []config.ToolMetadata {
	var out []config.ToolMetadata
	for _, tool := range d.env.Tools {
		if tool.Provider == d.provider {
			out = append(out, tool.ToolMetadata)
		}
	}
	return out
}

// covered are the tools an entry names today. A prefix leaves out tools that need the allow-list.
func (d *decisionState) covered(entry string) []config.ToolMetadata {
	var out []config.ToolMetadata
	for _, tool := range d.ownTools() {
		if tool.ID == entry || config.ToolPrefixCovers(d.provider, entry, tool) {
			out = append(out, tool)
		}
	}
	return out
}

// offers reports whether cfg's connection offers tool, as a disabled connection would.
func (d *decisionState) offers(cfg *config.Config, tool config.ToolMetadata) bool {
	refusal := cfg.ConnectionRefusal(d.name, tool, d.env.Now)
	return refusal == "" || refusal == config.RefusalDisabled
}

// gained returns the tools cfg offers that the current configuration does not.
func (d *decisionState) gained(cfg *config.Config) []config.ToolMetadata {
	var out []config.ToolMetadata
	for _, tool := range d.ownTools() {
		if !d.offers(d.cfg, tool) && d.offers(cfg, tool) {
			out = append(out, tool)
		}
	}
	return out
}

// withConnection returns a clone of the configuration with edit applied to the connection.
func (d *decisionState) withConnection(edit func(*config.Connection)) *config.Config {
	cfg := d.cfg.Clone()
	conn := cfg.Connections[d.name]
	edit(&conn)
	cfg.Connections[d.name] = conn
	return cfg
}

func uniqueSorted(lists ...[]string) []string {
	var out []string
	for _, list := range lists {
		for _, v := range list {
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	sort.Strings(out)
	return out
}

// appendNew keeps the entries of list as they are and appends the new ones of add, sorted.
func appendNew(list, add []string) []string {
	out := slices.Clone(list)
	var fresh []string
	for _, v := range add {
		if !slices.Contains(out, v) {
			fresh = append(fresh, v)
		}
	}
	sort.Strings(fresh)
	return append(out, fresh...)
}

// acceptPlan is the change accepting makes, computed once for the preview and the save.
type acceptPlan struct {
	// add is the configuration after AcceptAddEffects and addBlockedBy the entries that rule it out; the
	// configuration is nil then. For a tools list it is the only configuration.
	add          *config.Config
	addBlockedBy []string
	addGains     []EffectGain
	// list is the configuration after AcceptSwitchToList; nil for a connection with a tools list.
	list      *config.Config
	listTools int
	newTools  int
}

func (d *decisionState) planAccept() acceptPlan {
	var plan acceptPlan
	if d.cfg.Connections[d.name].Tools != nil {
		plan.add = d.withConnection(func(c *config.Connection) {
			c.Tools = appendNew(c.Tools, d.selected)
		})
		plan.newTools = len(d.gained(plan.add))
		return plan
	}
	var effects []config.Effect
	have := d.cfg.ConnectionEffects(d.name)
	for _, entry := range d.selected {
		for _, tool := range d.covered(entry) {
			switch {
			case tool.RequiresToolAllowList:
				if !slices.Contains(plan.addBlockedBy, entry) {
					plan.addBlockedBy = append(plan.addBlockedBy, entry)
				}
			case !slices.Contains(have, tool.Effect) && !slices.Contains(effects, tool.Effect):
				effects = append(effects, tool.Effect)
			}
		}
	}
	if len(plan.addBlockedBy) == 0 {
		plan.add = d.withConnection(func(c *config.Connection) {
			c.Effects = nil
			for _, e := range config.Effects() {
				if slices.Contains(have, e) || slices.Contains(effects, e) {
					c.Effects = append(c.Effects, e)
				}
			}
		})
		counts := map[config.Effect]int{}
		for _, tool := range d.gained(plan.add) {
			counts[tool.Effect]++
		}
		for _, e := range config.Effects() {
			if slices.Contains(effects, e) {
				plan.addGains = append(plan.addGains, EffectGain{Effect: e, Tools: counts[e]})
			}
		}
	}
	plan.list = d.withConnection(func(c *config.Connection) {
		var offered []string
		for _, tool := range d.ownTools() {
			if d.offers(d.cfg, tool) {
				offered = append(offered, tool.ID)
			}
		}
		c.Effects = nil
		c.Tools = uniqueSorted(offered, d.selected)
	})
	for _, tool := range d.ownTools() {
		if d.offers(plan.list, tool) {
			plan.listTools++
		}
	}
	return plan
}

func (d *decisionState) entries(metadata config.ProviderMetadata) []RequestTool {
	out := make([]RequestTool, len(d.selected))
	for i, entry := range d.selected {
		out[i] = RequestTool{Tool: entry}
		if group, ok := config.GroupOfEntry(metadata, entry); ok {
			out[i].Group, out[i].GroupTitle = group.ID, group.Title
		}
	}
	return out
}

// encryptedVaultCredential reports whether the connection of cfg reads a credential of an encrypted vault,
// which is what an approval is about.
func (s *Service) encryptedVaultCredential(cfg *config.Config, name string) bool {
	required, _, err := AdminRequired(s.vault())
	if err != nil || !required {
		return false
	}
	scopes, err := approval.VaultScopes(cfg)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(scopes, func(scope vault.Scope) bool { return scope.Connection == name })
}

// PreviewRequestDecision says what accepting the selection of an open request would do, as data: the
// selected entries with their groups, what no longer applies, and for a connection by effect both ways to
// accept with the number of tools each one offers. It reads only, needs no admin proof, and writes nothing,
// not even the pruning of expired requests.
func (s *Service) PreviewRequestDecision(sel RequestSelection) (RequestPreview, error) {
	d, err := s.openDecision(sel)
	if err != nil {
		return RequestPreview{}, err
	}
	if d.req.Kind == accessrequest.KindCreateConnection {
		return s.previewCreate(d)
	}
	metadata, _ := d.cfg.ProviderMetadata(d.provider)
	preview := RequestPreview{
		Request: d.req.ID, Kind: d.req.Kind, Connection: d.name, ByEffects: d.cfg.Connections[d.name].Tools == nil,
		Entries: d.entries(metadata), Dropped: d.dropped, ApprovalExpected: s.encryptedVaultCredential(d.cfg, d.name),
	}
	if len(d.selected) == 0 {
		return preview, nil
	}
	if d.req.Kind == accessrequest.KindRemoveTools {
		plan := d.planRemove()
		preview.Remaining, preview.SplitPrefixes, preview.EmptyList = plan.remaining, plan.split, plan.remaining == 0
		return preview, nil
	}
	plan := d.planAccept()
	preview.NewTools, preview.AddEffects = plan.newTools, plan.addGains
	preview.AddEffectsBlockedBy, preview.SwitchTools = plan.addBlockedBy, plan.listTools
	return preview, nil
}

// DecideRequest applies one decision to the selection of an open request: it accepts, declines or blocks
// the tools. It first re-checks the selection against the connection as it is now, with the check a new
// request passes: tools offered or blocked meanwhile fall away, and a deleted connection ends the request.
//
// Accepting and blocking change the configuration through SaveConfig with its revision check. The queue is
// written only after that save succeeded, so a conflict (errors.Is(err, config.ErrConflict)) changes
// neither file. If the save succeeded and the queue then cannot be updated, the decision still counts: the
// result carries a warning, and the request disappears when the queue is next opened, because the tools are
// offered or blocked by then. Accepting a connection that reads a credential of an encrypted vault also
// approves it through ApproveAfterChange in the same step; if that fails the configuration stays saved and
// the result's Approval says the connection is open under Approvals.
//
// Every decision on an encrypted vault needs proof from an admin session. The decisions are logged per
// tool as config.request.accept, .decline and .block with the surface of s, which must be the terminal or
// the TUI; the reason of a request is never logged.
func (s *Service) DecideRequest(ctx context.Context, proof AdminProof, in RequestDecision) (RequestDecisionResult, error) {
	var operation string
	switch in.Action {
	case RequestAccept:
		operation = invokelog.OperationRequestAccept
	case RequestDecline:
		operation = invokelog.OperationRequestDecline
	case RequestBlock:
		operation = invokelog.OperationRequestBlock
	default:
		return RequestDecisionResult{}, decisionError(RequestCodeInvalid, "unknown decision %q", string(in.Action))
	}
	if s.surface != connlog.SurfaceTUI && s.surface != connlog.SurfaceCLI {
		return RequestDecisionResult{}, decisionError(RequestCodeUnavailable, "requests are decided in the terminal or the TUI")
	}
	required, _, err := AdminRequired(s.vault())
	if err != nil || required && !proof.granted {
		return RequestDecisionResult{}, decisionError(RequestCodeAdminRequired, "deciding a request needs the vault passphrase first")
	}
	d, err := s.openDecision(in.RequestSelection)
	if err != nil {
		return RequestDecisionResult{}, err
	}
	if d.req.Kind == accessrequest.KindCreateConnection {
		return s.declineCreate(d, in.Action)
	}
	removing := d.req.Kind == accessrequest.KindRemoveTools
	if removing {
		// Blocking only keeps a tool from being requested to be offered, so it has no meaning here.
		switch in.Action {
		case RequestBlock:
			return RequestDecisionResult{}, decisionError(RequestCodeInvalid,
				"a request to remove tools is accepted or declined; blocking does not apply")
		case RequestAccept:
			operation = invokelog.OperationRequestRemoveAccept
		case RequestDecline:
			operation = invokelog.OperationRequestRemoveDecline
		}
	}
	result := RequestDecisionResult{Action: in.Action, Request: d.req.ID, Connection: d.name, Decided: d.selected, Dropped: d.dropped}

	var saved *config.Config
	if len(d.selected) > 0 {
		switch in.Action {
		case RequestAccept:
			if in.Mode != "" && d.cfg.Connections[d.name].Tools != nil {
				return RequestDecisionResult{}, decisionError(RequestCodeInvalid,
					"this connection offers a tools list, so accepting needs no choice between adding effects and a list")
			}
			if removing {
				if d.cfg.Connections[d.name].Tools == nil && in.Mode != AcceptSwitchToList {
					return RequestDecisionResult{}, decisionError(RequestCodeInvalid,
						"this connection offers its tools by effect; removing tools needs switching it to a tools list")
				}
				plan := d.planRemove()
				if !d.removalHolds(plan) {
					return RequestDecisionResult{}, decisionError(RequestCodeInvalid,
						"removing these tools would change what else the connection offers; nothing was changed")
				}
				saved = plan.cfg
				if d.cfg.Connections[d.name].Tools == nil {
					result.Mode = in.Mode
				}
				break
			}
			plan := d.planAccept()
			switch {
			case d.cfg.Connections[d.name].Tools != nil:
				saved = plan.add
			case in.Mode == AcceptSwitchToList:
				saved = plan.list
			case in.Mode == AcceptAddEffects && len(plan.addBlockedBy) > 0:
				return RequestDecisionResult{}, decisionError(RequestCodeNeedsList,
					"%s needs its exact ID in a tools list; switch the connection to a tools list to accept it",
					shortEntry(plan.addBlockedBy[0]))
			case in.Mode == AcceptAddEffects:
				saved = plan.add
			default:
				return RequestDecisionResult{}, decisionError(RequestCodeInvalid,
					"this connection offers its tools by effect; choose to add the effects or to switch to a tools list")
			}
			if d.cfg.Connections[d.name].Tools == nil {
				result.Mode = in.Mode
			}
		case RequestBlock:
			saved = d.withConnection(func(c *config.Connection) { c.BlockedTools = appendNew(c.BlockedTools, d.selected) })
		}
	}
	if saved != nil {
		warning, err := s.SaveConfig(d.cfg, saved, d.rev)
		if err != nil {
			return RequestDecisionResult{}, err
		}
		if warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}
	}

	events, err := s.requests.Queue.Settle(d.env, d.req.ID, d.selected, d.dropped)
	if err != nil {
		if saved == nil {
			if errors.Is(err, accessrequest.ErrNotOpen) {
				return RequestDecisionResult{}, decisionError(RequestCodeNotFound, "the request is no longer open")
			}
			return RequestDecisionResult{}, err
		}
		result.Warnings = append(result.Warnings, s.redact("the request could not be removed from the queue and "+
			"disappears when the queue is next opened: "+err.Error()))
	}
	for _, tool := range d.req.Tools {
		if !slices.Contains(d.selected, tool) && !slices.Contains(d.dropped, tool) {
			result.Remaining = append(result.Remaining, tool)
		}
	}

	result.Approval = RequestApproval{State: ApprovalNotNeeded}
	if in.Action == RequestAccept && saved != nil && s.encryptedVaultCredential(saved, d.name) {
		s.approveAccepted(ctx, saved, d.name, &result)
	}

	connID := d.cfg.Connections[d.name].ID
	recorder := connlog.NewRecorder(s.surface, s.store.Path(), s.vault(), s.processOK)
	if err := recorder.RecordRequest(operation, d.name, connID, d.selected); err != nil {
		result.Warnings = append(result.Warnings, s.redact("warning: "+err.Error()))
	}
	result.Warnings = append(result.Warnings, s.recordRequestEvents(recorder, events)...)
	return result, nil
}

// approveAccepted approves the accepted connection in the same step. A failure is no error of the decision:
// the connection then stays open under Approvals.
func (s *Service) approveAccepted(ctx context.Context, saved *config.Config, name string, result *RequestDecisionResult) {
	res := ApproveAfterChange(ctx, s.vault(), ApprovalSnapshot{}, saved, name, ApprovalDirectOnly)
	if res.Warning != "" {
		result.Warnings = append(result.Warnings, s.redact(res.Warning))
	}
	if slices.Contains(res.Approved, name) {
		result.Approval = RequestApproval{State: ApprovalGranted}
		return
	}
	detail := "the vault is not unlocked here"
	switch {
	case res.CheckErr != nil:
		detail = "the open approvals could not be read: " + res.CheckErr.Error()
	case res.ApproveErr != nil:
		detail = "approving failed: " + res.ApproveErr.Error()
	case res.StayedOpen != "":
		detail = res.StayReason
	}
	result.Approval = RequestApproval{State: ApprovalStaysOpen, Detail: s.redact(detail)}
}

var queueEventOperations = map[accessrequest.EventKind]string{
	accessrequest.EventExpire: invokelog.OperationRequestExpire,
	accessrequest.EventDrop:   invokelog.OperationRequestDrop,

	accessrequest.EventRemoveExpire: invokelog.OperationRequestRemoveExpire,
	accessrequest.EventRemoveDrop:   invokelog.OperationRequestRemoveDrop,

	accessrequest.EventCreateConnectionExpire: invokelog.OperationRequestConnectionExpire,
	accessrequest.EventCreateConnectionDrop:   invokelog.OperationRequestConnectionDrop,
}

// recordRequestEvents logs the tools that expired or were dropped while the queue was settled, one entry
// per tool, and returns the redacted warnings of entries that could not be written.
func (s *Service) recordRequestEvents(recorder *connlog.Recorder, events []accessrequest.Event) []string {
	type key struct {
		kind         accessrequest.EventKind
		connection   string
		connectionID string
	}
	var order []key
	tools := map[key][]string{}
	for _, event := range events {
		k := key{event.Kind, event.Connection, event.ConnectionID}
		if _, ok := tools[k]; !ok {
			order = append(order, k)
		}
		tools[k] = append(tools[k], event.Tool)
	}
	var warnings []string
	for _, k := range order {
		operation, ok := queueEventOperations[k.kind]
		if !ok {
			continue
		}
		if err := recorder.RecordRequest(operation, k.connection, k.connectionID, tools[k]); err != nil {
			warnings = append(warnings, s.redact("warning: "+err.Error()))
		}
	}
	return warnings
}
