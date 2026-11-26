package vaultproc

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/localipc"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// Connect's operations, beside the kit's status, lock and handover.
const (
	opGet    = "get"
	opSet    = "set"
	opDelete = "delete"
	opCheck  = "check"
	opBind   = "bind"
	// opLog appends one invocation log entry, signed by the process; opLogCheck checks the check values of
	// stored log lines. Neither answers with the log key or the vault's key.
	opLog      = "log"
	opLogCheck = "logcheck"
	// opApproveToken approves the open connection changes an agent token covers; it never answers with a
	// token's value.
	opApproveToken = "approve-token"
	// opNames asks for the entry names and approval keys the vault holds, with their config ids.
	opNames = "names"
	// opUpdate asks for the vault's update setting, as the process reads it from settings.age.
	opUpdate = "update"
	// opStoreRefresh replaces the rotated refresh token of the credential a connection reads, in the vault on
	// disk and in the process, only while it still holds the old value the request names, and only for a
	// connection the vault approved as it is now. It writes no other role.
	opStoreRefresh = "store-refresh"
)

// Connect's answer codes. Fixed words, never built from a request.
const (
	codeApproval       = "approval-required"
	codeNoLog          = "no-log"
	codeLogFailed      = "log-failed"
	codeNoNames        = "no-names"
	codeNamesFailed    = "names-failed"
	codeNoTokens       = "no-tokens"
	codeTokenUnknown   = "token-unknown"
	codeTokenExpired   = "token-expired"
	codeTokensTampered = "tokens-tampered"
	codeTokenFailed    = "token-failed"
	codeRoleRefused    = "role-refused"
	codeStoreFailed    = "store-failed"
	// codeBadRequest answers a body that cannot be read. The kit's own word for it is not an application's.
	codeBadRequest = "invalid-request"
)

// The bodies and results of Connect's operations.
type (
	secretRequest struct {
		Credential string       `json:"credential,omitempty"`
		Role       string       `json:"role,omitempty"`
		Value      string       `json:"value,omitempty"`
		Scope      *vault.Scope `json:"scope,omitempty"`
	}
	// storeRefreshRequest is a store-refresh: Old is the value it replaces, and Value its replacement.
	storeRefreshRequest struct {
		Credential string       `json:"credential,omitempty"`
		Role       string       `json:"role,omitempty"`
		Old        string       `json:"old,omitempty"`
		Value      string       `json:"value,omitempty"`
		Scope      *vault.Scope `json:"scope,omitempty"`
	}
	getResult struct {
		Found bool   `json:"found,omitempty"`
		Value string `json:"value,omitempty"`
	}
	bindRequest struct {
		Bindings *vault.Bindings `json:"bindings"`
	}
	logRequest struct {
		Entry *logEntry `json:"entry"`
	}
	logCheckRequest struct {
		Lines [][]byte `json:"lines"`
	}
	logCheckResult struct {
		Valid []bool `json:"valid"`
	}
	tokenRequest struct {
		Token         string            `json:"token,omitempty"`
		Scopes        []vault.Scope     `json:"scopes,omitempty"`
		Names         []string          `json:"names,omitempty"`
		ConnectionIDs map[string]string `json:"connection_ids,omitempty"`
	}
	tokenResult struct {
		Approval *vault.TokenApproval `json:"approval,omitempty"`
		Unlogged bool                 `json:"unlogged,omitempty"`
	}
	namesResult struct {
		Names *vault.Names `json:"names"`
	}
	updateResult struct {
		Update            string `json:"update"`
		SettingsUntrusted bool   `json:"settings_untrusted,omitempty"`
	}
)

// logEntry is what a client sends for one invoke to log: invokelog.Fields, without anything the process
// computes itself, which is the time, the sequence number, the previous entry's hash, and the check value.
type logEntry struct {
	Path             string                `json:"path"`
	Client           *invokelog.ClientInfo `json:"client,omitempty"`
	Operation        string                `json:"operation,omitempty"`
	Version          int                   `json:"version,omitempty"`
	Connection       string                `json:"connection,omitempty"`
	ConnectionID     string                `json:"connection_id,omitempty"`
	FormerConnection string                `json:"former_connection,omitempty"`
	Tool             string                `json:"tool,omitempty"`
	Effect           string                `json:"effect,omitempty"`
	Result           string                `json:"result"`
	DurationMS       int64                 `json:"duration_ms,omitempty"`
	ReqBytes         *int64                `json:"req_bytes,omitempty"`
	RespBytes        *int64                `json:"resp_bytes,omitempty"`
	Session          string                `json:"session,omitempty"`
}

func (e logEntry) fields() invokelog.Fields {
	return invokelog.Fields{
		Path: e.Path, Client: e.Client, Operation: e.Operation, Version: e.Version, Connection: e.Connection,
		ConnectionID: e.ConnectionID, FormerConnection: e.FormerConnection, Tool: e.Tool,
		Effect: e.Effect, Result: e.Result, Duration: time.Duration(e.DurationMS) * time.Millisecond,
		ReqBytes: e.ReqBytes, RespBytes: e.RespBytes, Session: e.Session,
	}
}

func logEntryOf(f invokelog.Fields) *logEntry {
	return &logEntry{
		Path: f.Path, Client: f.Client, Operation: f.Operation, Version: f.Version, Connection: f.Connection,
		ConnectionID: f.ConnectionID, FormerConnection: f.FormerConnection, Tool: f.Tool,
		Effect: f.Effect, Result: f.Result, DurationMS: f.Duration.Milliseconds(),
		ReqBytes: f.ReqBytes, RespBytes: f.RespBytes, Session: f.Session,
	}
}

// resultAdminRequired is the result a refused token approval is logged with.
const resultAdminRequired = "admin-required"

// errBadBody answers a body that cannot be read.
var errBadBody = &kitvaultproc.Error{Code: codeBadRequest}

// process is Connect's part of one vault process: what it derived from the vault's key and its directory.
// The kit runs its handlers and hooks one at a time, so it needs no lock of its own.
type process struct {
	// vaultDir is the vault directory the process serves, from its configuration, never from a request;
	// empty where it answers no approve-token and no names.
	vaultDir string
	// logKey signs and checks the invocation log; log writes it, nil where the process keeps none.
	logKey *invokelog.Key
	log    *invokelog.Logger
	// update says whether the process answers the update setting, which a process that hands over does.
	update bool
	// verified is set once the release of the handover under way verified, which is when the kit asks for
	// the update setting; onHandover tells by it in which phase a file was too large.
	verified bool
}

// processOptions say what a process does besides holding the secrets.
type processOptions struct {
	vaultDir string
	// keepLog makes the process the writer of the invocation log of vaultDir, with retentionDays.
	keepLog       bool
	retentionDays int
	tokens        bool
	update        bool
}

// newProcess derives the process's own state from key.
func newProcess(key *age.X25519Identity, opts processOptions) *process {
	p := &process{update: opts.update}
	if opts.tokens || opts.update {
		p.vaultDir = opts.vaultDir
	}
	// A key the log key cannot be derived from leaves the process without one; it then refuses log and
	// logcheck, and a client writes the entry itself.
	if logKey, err := invokelog.DeriveKey(key); err == nil {
		p.logKey = logKey
		if opts.keepLog {
			p.log = invokelog.New(opts.vaultDir, opts.retentionDays).WithKey(logKey)
		}
	}
	return p
}

// limits are the channel and the times a process locks after, from its configuration and settings.age.
type limits struct {
	endpoint    localipc.Endpoint
	idleTimeout time.Duration
	maxUnlock   time.Duration
}

// config returns the kit configuration of the process, its handlers and hooks.
func (p *process) config(l limits) kitvaultproc.Config {
	handlers := map[string]kitvaultproc.HandlerFunc{
		opGet: p.get, opSet: p.set, opDelete: p.delete, opCheck: p.check, opBind: p.bind,
		opLog: p.appendLog, opLogCheck: p.checkLog, opApproveToken: p.approveToken, opNames: p.names,
		opStoreRefresh: p.storeRefresh,
	}
	if p.update {
		handlers[opUpdate] = p.updateSetting
	}
	return kitvaultproc.Config{
		Endpoint: l.endpoint, Version: Version, ProofLabel: proofLabel, IdleTimeout: l.idleTimeout,
		MaxUnlock: l.maxUnlock, Handlers: handlers, OnHandover: p.onHandover, OnLock: p.onLock,
		// A replaced program locks the process at the next refused client, as before the kit, not on the
		// kit's own watch.
		ReplacedProgram: kitvaultproc.ReplacedLocksOnClient,
	}
}

// heldDocument decodes the document held. The caller wipes it.
func heldDocument(held *kitvaultproc.Held) (*document, error) { return decodeDocument(held.Document()) }

// replace makes d the held document.
func replace(held *kitvaultproc.Held, d *document) error {
	data, err := d.encode()
	if err != nil {
		return err
	}
	defer clear(data)
	held.Replace(data)
	return nil
}

// get answers one role of one credential, only for a connection the vault approved as it is now. A get
// restarts the idle period, as the only request that does.
func (p *process) get(_ context.Context, held *kitvaultproc.Held, body json.RawMessage) (any, error) {
	held.Touch()
	var req secretRequest
	if json.Unmarshal(body, &req) != nil || req.Credential == "" || req.Role == "" {
		return nil, errBadBody
	}
	d, err := heldDocument(held)
	if err != nil {
		return nil, err
	}
	defer d.wipe()
	roles, ok := d.Secrets[req.Credential]
	if !ok {
		return getResult{}, nil
	}
	// A get that names no connection, or another credential than its connection reads, is refused the same
	// way as one the vault did not approve.
	if req.Scope == nil || !d.Bindings.AllowsRole(*req.Scope, req.Credential, req.Role) {
		return nil, &kitvaultproc.Error{Code: codeApproval}
	}
	value, ok := roles[req.Role]
	if !ok {
		return getResult{}, nil
	}
	return getResult{Found: true, Value: string(value)}, nil
}

// check answers whether a connection may read a credential, its own or a forward one, without a value.
func (p *process) check(_ context.Context, held *kitvaultproc.Held, body json.RawMessage) (any, error) {
	var req secretRequest
	if json.Unmarshal(body, &req) != nil || req.Scope == nil {
		return nil, errBadBody
	}
	d, err := heldDocument(held)
	if err != nil {
		return nil, err
	}
	defer d.wipe()
	credential := req.Credential
	if credential == "" {
		credential = req.Scope.Credential
	}
	if _, ok := d.Secrets[credential]; !ok {
		return nil, nil
	}
	if !d.Bindings.Allows(*req.Scope, credential) {
		return nil, &kitvaultproc.Error{Code: codeApproval}
	}
	return getResult{Found: true}, nil
}

// set stores or replaces one role of one credential.
func (p *process) set(_ context.Context, held *kitvaultproc.Held, body json.RawMessage) (any, error) {
	var req secretRequest
	err := json.Unmarshal(body, &req)
	value := []byte(req.Value)
	req.Value = ""
	defer clear(value)
	if err != nil || req.Credential == "" || req.Role == "" {
		return nil, errBadBody
	}
	d, err := heldDocument(held)
	if err != nil {
		return nil, err
	}
	defer d.wipe()
	roles := d.Secrets[req.Credential]
	if roles == nil {
		roles = map[string][]byte{}
		d.Secrets[req.Credential] = roles
	}
	clear(roles[req.Role])
	roles[req.Role] = append([]byte(nil), value...)
	return nil, replace(held, d)
}

// delete removes one role of one credential; removing what is not held is no error.
func (p *process) delete(_ context.Context, held *kitvaultproc.Held, body json.RawMessage) (any, error) {
	var req secretRequest
	if json.Unmarshal(body, &req) != nil || req.Credential == "" || req.Role == "" {
		return nil, errBadBody
	}
	d, err := heldDocument(held)
	if err != nil {
		return nil, err
	}
	defer d.wipe()
	roles := d.Secrets[req.Credential]
	clear(roles[req.Role])
	delete(roles, req.Role)
	if len(roles) == 0 {
		// The vault removed the entry with its last role; one stored again under the same name is a new
		// entry with a new id, which no approval covers until the vault's bindings say so.
		delete(d.Secrets, req.Credential)
		delete(d.Bindings.IDs, req.Credential)
	}
	return nil, replace(held, d)
}

// bind replaces the bindings every get is checked against.
func (p *process) bind(_ context.Context, held *kitvaultproc.Held, body json.RawMessage) (any, error) {
	var req bindRequest
	if json.Unmarshal(body, &req) != nil || req.Bindings == nil {
		return nil, errBadBody
	}
	d, err := heldDocument(held)
	if err != nil {
		return nil, err
	}
	defer d.wipe()
	d.Bindings = copyBindings(*req.Bindings)
	return nil, replace(held, d)
}

// appendLog validates one entry and appends it, signed, to the invocation log the process keeps.
func (p *process) appendLog(_ context.Context, _ *kitvaultproc.Held, body json.RawMessage) (any, error) {
	if p.logKey == nil || p.log == nil {
		return nil, &kitvaultproc.Error{Code: codeNoLog}
	}
	var req logRequest
	// A duration beyond what Validate accepts is refused before it is converted, where it could overflow.
	if json.Unmarshal(body, &req) != nil || req.Entry == nil || req.Entry.DurationMS < 0 ||
		req.Entry.DurationMS > invokelog.MaxDuration.Milliseconds() {
		return nil, errBadBody
	}
	fields := req.Entry.fields()
	if fields.Validate() != nil {
		return nil, errBadBody
	}
	if p.log.Append(fields) != nil {
		return nil, &kitvaultproc.Error{Code: codeLogFailed}
	}
	return nil, nil
}

// checkLog compares the check values of stored log lines; it writes nothing.
func (p *process) checkLog(_ context.Context, _ *kitvaultproc.Held, body json.RawMessage) (any, error) {
	if p.logKey == nil {
		return nil, &kitvaultproc.Error{Code: codeNoLog}
	}
	var req logCheckRequest
	if json.Unmarshal(body, &req) != nil || len(req.Lines) == 0 {
		return nil, errBadBody
	}
	valid, err := p.logKey.Check(req.Lines)
	if err != nil {
		return nil, &kitvaultproc.Error{Code: codeNoLog}
	}
	return logCheckResult{Valid: valid}, nil
}

// approveToken checks an agent token against the vault on disk, opened with the process's key, writes the
// approvals it covers into the vault, adopts the vault's bindings, and logs each decision with the token's
// name, never its value.
func (p *process) approveToken(_ context.Context, held *kitvaultproc.Held, body json.RawMessage) (any, error) {
	if p.vaultDir == "" {
		return nil, &kitvaultproc.Error{Code: codeNoTokens}
	}
	var req tokenRequest
	if json.Unmarshal(body, &req) != nil {
		return nil, errBadBody
	}
	v, err := vault.OpenWithKey(p.vaultDir, held.Identity())
	if err != nil {
		return nil, &kitvaultproc.Error{Code: codeTokenFailed}
	}
	result, err := v.ApproveWithTokenConnections(req.Token, req.Scopes, req.ConnectionIDs, req.Names, time.Now())
	refused := invokelog.Fields{Path: "cli", Operation: invokelog.OperationApprove, Result: resultAdminRequired,
		Token: result.Token}
	switch {
	case errors.Is(err, vault.ErrTokenUnknown):
		p.logDecision(refused)
		return nil, &kitvaultproc.Error{Code: codeTokenUnknown}
	case errors.Is(err, vault.ErrTokenExpired):
		p.logDecision(refused)
		return nil, &kitvaultproc.Error{Code: codeTokenExpired}
	case errors.Is(err, vault.ErrTokensTampered):
		return nil, &kitvaultproc.Error{Code: codeTokensTampered}
	case err != nil:
		return nil, &kitvaultproc.Error{Code: codeTokenFailed}
	}

	if bindings, err := v.Bindings(); err == nil {
		if d, err := heldDocument(held); err == nil {
			d.Bindings = copyBindings(bindings)
			_ = replace(held, d)
			d.wipe()
		}
	}
	logged := true
	for _, change := range result.Changes {
		fields := invokelog.Fields{Path: "cli", Operation: invokelog.OperationApprove, Connection: change.Connection,
			Effect: change.Kind, Result: resultAdminRequired, Token: result.Token}
		if change.Approved {
			fields.Result = "success"
		}
		if !p.logDecision(fields) {
			logged = false
		}
	}
	return tokenResult{Approval: &result, Unlogged: !logged}, nil
}

// names answers the entry names and approval keys of the vault on disk, opened with the process's key, and
// nothing else.
func (p *process) names(_ context.Context, held *kitvaultproc.Held, _ json.RawMessage) (any, error) {
	if p.vaultDir == "" {
		return nil, &kitvaultproc.Error{Code: codeNoNames}
	}
	v, err := vault.OpenWithKey(p.vaultDir, held.Identity())
	if err != nil {
		return nil, &kitvaultproc.Error{Code: codeNamesFailed}
	}
	names, err := v.KnownNames()
	if err != nil {
		return nil, &kitvaultproc.Error{Code: codeNamesFailed}
	}
	return namesResult{Names: &names}, nil
}

// storeRefresh replaces the refresh token of the credential the request's connection reads in the vault on
// disk, opened with the process's key under the vault's write lock, only while it still holds the request's
// old value, and then in the held document. The connection must be one the vault approved as it is now, the
// same check a get makes, and no role but the refresh token is written. The answer carries the value stored
// afterwards, which the connection may read anyway; each decision is logged with the connection and the
// outcome, never a value.
func (p *process) storeRefresh(_ context.Context, held *kitvaultproc.Held, body json.RawMessage) (any, error) {
	var req storeRefreshRequest
	if json.Unmarshal(body, &req) != nil {
		return nil, errBadBody
	}
	fields := invokelog.Fields{Path: "cli", Operation: invokelog.OperationStoreRefreshToken, Effect: "update"}
	if req.Scope != nil {
		fields.Connection = req.Scope.Connection
	}
	refuse := func(code string) (any, error) {
		fields.Result = code
		p.logDecision(fields)
		return nil, &kitvaultproc.Error{Code: code}
	}
	d, err := heldDocument(held)
	if err != nil {
		return nil, err
	}
	defer d.wipe()
	approved := req.Scope != nil && req.Credential != "" && req.Credential == req.Scope.Credential &&
		d.Bindings.AllowsRole(*req.Scope, req.Credential, req.Role)
	switch {
	case req.Role != config.RoleRefreshToken:
		return refuse(codeRoleRefused)
	case !approved:
		return refuse(codeApproval)
	case req.Old == "" || req.Value == "":
		return refuse(codeBadRequest)
	case p.vaultDir == "":
		return refuse(codeStoreFailed)
	}
	v, err := vault.OpenWithKey(p.vaultDir, held.Identity())
	if err != nil {
		return refuse(codeStoreFailed)
	}
	current, replaced, err := v.ReplaceRole(req.Credential, req.Role, req.Old, req.Value)
	if err != nil {
		return refuse(codeStoreFailed)
	}
	if roles := d.Secrets[req.Credential]; roles != nil && current != "" {
		clear(roles[req.Role])
		roles[req.Role] = []byte(current)
		_ = replace(held, d)
	}
	fields.Result = "success"
	if !replaced {
		fields.Result = "conflict"
	}
	p.logDecision(fields)
	return getResult{Found: current != "", Value: current}, nil
}

// updateSetting answers the vault's update setting as the process reads it from settings.age with its own
// key; a settings.age it cannot trust reads as lock.
func (p *process) updateSetting(_ context.Context, held *kitvaultproc.Held, _ json.RawMessage) (any, error) {
	behaviour, err := vault.ReadUpdateBehaviour(p.vaultDir, held.Identity())
	return updateResult{Update: string(behaviour), SettingsUntrusted: errors.Is(err, vault.ErrSettingsUntrusted)}, nil
}

// handsOver is the kit's HandoverConfig.HandsOver: whether settings.age asks an update to hand over.
func (p *process) handsOver(key *age.X25519Identity) bool {
	p.verified = true
	behaviour, _ := vault.ReadUpdateBehaviour(p.vaultDir, key)
	return behaviour == vault.UpdateHandover
}

// onHandover records the outcome of a handover in the invocation log, as Connect logged it before the kit.
func (p *process) onHandover(_ *kitvaultproc.Held, result kitvaultproc.HandoverResult) error {
	verified := p.verified
	p.verified = false
	if logged, ok := handoverResult(result, verified); ok {
		p.logDecision(invokelog.Fields{Path: "cli", Operation: invokelog.OperationHandover, Result: logged})
	}
	return nil
}

// handoverResult is the result a handover is logged with, as Connect logged it before the kit, and false
// where Connect logged none: a handover the update setting declined, and one refused because another was
// under way. A file over the limit is the release archive while the release did not verify yet, and the
// installed program once it did. Every reason Connect had no word for, a successor that did not start among
// them, is handover-failed.
func handoverResult(result kitvaultproc.HandoverResult, verified bool) (string, bool) {
	if result.HandedOver {
		return "success", true
	}
	switch result.Reason {
	case kitvaultproc.HandoverDeclined, kitvaultproc.HandoverBusy:
		return "", false
	case kitvaultproc.HandoverTooLarge:
		if verified {
			return "program-mismatch", true
		}
		return "release-unverified", true
	case kitvaultproc.HandoverNotVerified:
		return "release-unverified", true
	case kitvaultproc.HandoverProgramUnchanged:
		return "program-unchanged", true
	case kitvaultproc.HandoverProgramMismatch:
		return "program-mismatch", true
	case kitvaultproc.HandoverBadRequest:
		return "bad-request", true
	case kitvaultproc.HandoverLocked:
		return "locked", true
	}
	return "handover-failed", true
}

// onLock records a lock a refused client caused because the process's program was replaced, and drops the
// log key, which is derived from the vault's key.
func (p *process) onLock(_ *kitvaultproc.Held, event kitvaultproc.LockEvent) error {
	if event.Reason == kitvaultproc.LockProgramReplaced && event.Trigger == kitvaultproc.TriggerRefusedClient {
		p.logDecision(invokelog.Fields{Path: "cli", Operation: invokelog.OperationLock, Result: "program-replaced"})
	}
	p.logKey.Clear()
	p.logKey, p.log = nil, nil
	return nil
}

// logDecision appends one entry the process decided on and reports whether it was written.
func (p *process) logDecision(fields invokelog.Fields) bool {
	return p.log != nil && fields.Validate() == nil && p.log.Append(fields) == nil
}
