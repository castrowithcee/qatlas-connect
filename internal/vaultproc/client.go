package vaultproc

import (
	"context"
	"errors"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
	kitvaultproc "github.com/castrowithcee/qatlas-kit/vaultproc"
)

// Client reaches the vault process of one vault through the kit's client: every call opens a connection of
// its own, checks the process that listens (localipc.CheckHardened) and has it prove that it holds the
// vault's key before anything else is sent.
type Client struct {
	// Endpoint is the channel, usually from Endpoint.
	Endpoint localipc.Endpoint
	// Recipient is the vault's public recipient as age text; a client without one sends nothing.
	Recipient string
	// Timeout bounds one call; zero means DefaultRequestTimeout. The context of a call bounds it too.
	Timeout time.Duration
}

// ClientFor returns the client of the vault process for the vault in vaultDir, checked against recipient.
func ClientFor(vaultDir, recipient string) (*Client, error) {
	e, err := Endpoint(vaultDir)
	if err != nil {
		return nil, err
	}
	return &Client{Endpoint: e, Recipient: recipient}, nil
}

// Path returns the socket, or the pipe, the client reaches; empty where it cannot be told.
func (c *Client) Path() string {
	path, _ := c.Endpoint.Path()
	return path
}

// check refuses a client without a recipient it can read before anything is dialed.
func (c *Client) check() error {
	text := strings.TrimSpace(c.Recipient)
	if text == "" {
		return ErrNoRecipient
	}
	if _, err := age.ParseX25519Recipient(text); err != nil {
		return errors.New("the vault recipient cannot be read; the vault process is not asked")
	}
	return nil
}

// tell is translate, which names the process that answered replaced as well: the kit's client does not
// name it, and localipc's Holder only looks who serves the channel.
func (c *Client) tell(ctx context.Context, err error) error { return c.tellIn(ctx, err, false) }

// tellIn is tell, which tells a handover's reason as it is at its commit where commit is set.
func (c *Client) tellIn(ctx context.Context, err error, commit bool) error {
	var peer *localipc.PeerError
	if errors.Is(err, kitvaultproc.ErrReplaced) && !errors.As(err, &peer) && ctx.Err() == nil {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		pid, holderErr := c.Endpoint.Holder(probe)
		cancel()
		if holderErr == nil && pid > 0 {
			err = &localipc.PeerError{PID: pid, Err: err}
		}
	}
	return translateIn(err, commit)
}

func (c *Client) kit() *kitvaultproc.Client {
	return &kitvaultproc.Client{Endpoint: c.Endpoint, Recipient: c.Recipient, Version: Version,
		ProofLabel: proofLabel, Timeout: c.Timeout}
}

// Status is what a running vault process reports about itself. It holds no secret and no credential name.
type Status struct {
	PID int
	// LocksAt is when the process locks itself unless a get comes first.
	LocksAt time.Time
	// UnlockEndsAt is the end of the maximum unlock time; zero without a limit.
	UnlockEndsAt time.Time
	// MemoryLocked reports whether the system keeps the held vault out of swap.
	MemoryLocked bool
	// Update is the vault's update setting as the process reads it from settings.age with its own key;
	// SettingsUntrusted reports a settings.age it could not trust, which reads as lock. Update is empty for
	// a process that does not hand over (Windows).
	Update            vault.UpdateBehaviour
	SettingsUntrusted bool
}

func statusOf(st kitvaultproc.Status) Status {
	return Status{PID: st.PID, LocksAt: st.LocksAt, UnlockEndsAt: st.UnlockEndsAt, MemoryLocked: st.MemoryLocked}
}

// Status reports on the running vault process, with its update setting, which it answers as an operation of
// its own beside the kit's status. ErrNotRunning means there is none.
func (c *Client) Status(ctx context.Context) (Status, error) {
	if err := c.check(); err != nil {
		return Status{}, err
	}
	kit := c.kit()
	st, err := kit.Status(ctx)
	if err != nil {
		return Status{}, c.tell(ctx, err)
	}
	status := statusOf(st)
	var update updateResult
	switch err := kit.Call(ctx, opUpdate, nil, &update); {
	case err == nil:
		status.Update, status.SettingsUntrusted = vault.UpdateBehaviour(update.Update), update.SettingsUntrusted
	case errors.Is(err, kitvaultproc.ErrUnknownOperation):
	default:
		return Status{}, c.tell(ctx, err)
	}
	return status, nil
}

// Get returns the secret of one credential role for the connection scope describes. found is false when the
// process holds nothing for the pair. vault.ErrApprovalRequired means the vault has not approved the
// connection as scope describes it.
func (c *Client) Get(ctx context.Context, credential, role string, scope vault.Scope) (string, bool, error) {
	var result getResult
	err := c.call(ctx, opGet, secretRequest{Credential: credential, Role: role, Scope: &scope}, &result)
	if err != nil {
		return "", false, err
	}
	return result.Value, result.Found, nil
}

// Check reports whether the connection scope describes may read its credential, without reading a secret.
func (c *Client) Check(ctx context.Context, scope vault.Scope) error {
	return c.call(ctx, opCheck, secretRequest{Scope: &scope}, nil)
}

// CheckCredential is Check for one forward credential of the scope.
func (c *Client) CheckCredential(ctx context.Context, scope vault.Scope, credential string) error {
	return c.call(ctx, opCheck, secretRequest{Credential: credential, Scope: &scope}, nil)
}

// Bind replaces the bindings the running process checks every get with. It does not write the vault.
func (c *Client) Bind(ctx context.Context, bindings vault.Bindings) error {
	return c.call(ctx, opBind, bindRequest{Bindings: &bindings}, nil)
}

// Set stores or replaces the secret of one credential role in the running process. It does not write the
// vault.
func (c *Client) Set(ctx context.Context, credential, role, value string) error {
	return c.call(ctx, opSet, secretRequest{Credential: credential, Role: role, Value: value}, nil)
}

// StoreRefreshToken replaces the refresh token of credential, the one the connection scope describes reads,
// with rotated, in the vault on disk and in the process, as long as it still holds old. It returns the
// refresh token stored afterwards: rotated, or the value an earlier writer stored instead, "" for none.
// vault.ErrApprovalRequired means the vault has not approved the connection as scope describes it;
// ErrNotRunning means the vault is not unlocked in a process; ErrNoRefreshStore means a process of an
// earlier version, which cannot store it.
func (c *Client) StoreRefreshToken(ctx context.Context, credential string, scope vault.Scope, old,
	rotated string) (string, error) {
	var result getResult
	err := c.call(ctx, opStoreRefresh, storeRefreshRequest{Credential: credential, Role: config.RoleRefreshToken,
		Old: old, Value: rotated, Scope: &scope}, &result)
	if errors.Is(err, errInvalidRequest) || errors.Is(err, kitvaultproc.ErrBadRequest) ||
		errors.Is(err, kitvaultproc.ErrUnknownOperation) {
		return "", ErrNoRefreshStore
	}
	if err != nil {
		return "", err
	}
	return result.Value, nil
}

// ErrNoRefreshStore reports a running vault process of an earlier version, which cannot store a rotated
// refresh token. Locking the vault and unlocking it again starts one that can.
var ErrNoRefreshStore = errors.New("the running vault process is of an earlier version and cannot store a " +
	"rotated refresh token")

// ErrRoleRefused reports a write of a role the vault process does not let a run write: it writes the refresh
// token alone.
var ErrRoleRefused = errors.New("the vault process writes no role but the refresh token")

// errInvalidRequest reports a request the vault process could not read.
var errInvalidRequest = errors.New("the vault process could not read the request")

// Delete removes the secret of one credential role from the running process. Removing a pair it does not
// hold is not an error.
func (c *Client) Delete(ctx context.Context, credential, role string) error {
	return c.call(ctx, opDelete, secretRequest{Credential: credential, Role: role}, nil)
}

// Lock asks the vault process to overwrite its secrets and end. ErrNotRunning means there was none.
//
// A vault process of another protocol version, one left from before an update, cannot be asked, and neither
// can one that still answers replaced once the kit asked again: its handover did not finish. Such a process
// is ended with localipc's EndHolder, which signals it only as another process of this user behind a private
// channel whose lock it holds, checked again just before the signal; on SIGTERM it overwrites its secrets
// as on a lock, and on Windows TerminateProcess takes its memory with it.
func (c *Client) Lock(ctx context.Context) error {
	_, err := c.lock(ctx)
	return err
}

// lock is Lock, which also returns the id of a process it ended, 0 where it ended none.
func (c *Client) lock(ctx context.Context) (int, error) {
	if err := c.check(); err != nil {
		return 0, err
	}
	err := c.kit().Lock(ctx)
	if !errors.Is(err, kitvaultproc.ErrVersion) && !errors.Is(err, kitvaultproc.ErrReplaced) {
		return 0, c.tell(ctx, err)
	}
	// Whatever answered but is no process EndHolder accepts, a socket without its lock among them, is left
	// alone, and the caller is told why it could not lock.
	pid, endErr := c.Endpoint.EndHolder(ctx)
	if endErr != nil {
		return 0, c.tell(ctx, err)
	}
	return pid, nil
}

// Log hands the fields of one completed invoke to the vault process, which checks them, signs them with
// the log key, and appends them to the invocation log. On any error nothing was written as far as the
// client can tell, and the caller writes the entry itself.
func (c *Client) Log(ctx context.Context, f invokelog.Fields) error {
	return c.call(ctx, opLog, logRequest{Entry: logEntryOf(f)}, nil)
}

// ApproveWithToken asks the vault process to approve every open connection change the agent token covers,
// or only those of the connections names lists; see vault.Vault.ApproveWithTokenConnections. unlogged is
// true when the process could not log a decision.
func (c *Client) ApproveWithToken(ctx context.Context, token string, scopes []vault.Scope,
	connectionIDs map[string]string, names []string) (vault.TokenApproval, bool, error) {
	var result tokenResult
	err := c.call(ctx, opApproveToken, tokenRequest{Token: token, Scopes: scopes, ConnectionIDs: connectionIDs,
		Names: names}, &result)
	if errors.Is(err, kitvaultproc.ErrUnknownOperation) {
		err = ErrNoTokens
	}
	if err != nil {
		return vault.TokenApproval{}, false, err
	}
	var approval vault.TokenApproval
	if result.Approval != nil {
		approval = *result.Approval
	}
	return approval, result.Unlogged, nil
}

// Names asks the vault process for the entry names and approval keys the vault holds, with the config id
// each records and the credential name each approval was given for, and nothing else.
func (c *Client) Names(ctx context.Context) (vault.Names, error) {
	var result namesResult
	err := c.call(ctx, opNames, nil, &result)
	switch {
	case errors.Is(err, kitvaultproc.ErrUnknownOperation):
		return vault.Names{}, ErrNoNames
	case err != nil:
		return vault.Names{}, err
	case result.Names == nil:
		return vault.Names{}, ErrNoNames
	}
	return *result.Names, nil
}

// maxLogCheckBatch bounds the stored lines one logcheck carries, before their base64 encoding, so a request
// stays well within the kit's message limit.
const maxLogCheckBatch = kitvaultproc.MaxMessage / 2

// CheckLog asks the vault process whether the check value of each of lines matches, one answer per line in
// the same order. The process only compares. Lines go in as many requests as the message limit calls for;
// a single line too long to send is not one the log wrote, and is answered as not matching.
func (c *Client) CheckLog(ctx context.Context, lines [][]byte) ([]bool, error) {
	valid := make([]bool, len(lines))
	var batch [][]byte
	var indexes []int
	size := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var result logCheckResult
		if err := c.call(ctx, opLogCheck, logCheckRequest{Lines: batch}, &result); err != nil {
			return err
		}
		if len(result.Valid) != len(batch) {
			return errors.New("the vault process answered a log check with the wrong number of results")
		}
		for i, ok := range result.Valid {
			valid[indexes[i]] = ok
		}
		batch, indexes, size = nil, nil, 0
		return nil
	}
	for i, line := range lines {
		if len(line) > maxLogCheckBatch {
			continue
		}
		if size+len(line) > maxLogCheckBatch {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		batch, indexes, size = append(batch, line), append(indexes, i), size+len(line)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return valid, nil
}

// LogChecker returns an invokelog.Checker that checks through CheckLog, bounded by ctx.
func (c *Client) LogChecker(ctx context.Context) invokelog.Checker {
	return logChecker{ctx: ctx, client: c}
}

type logChecker struct {
	ctx    context.Context
	client *Client
}

func (l logChecker) Check(lines [][]byte) ([]bool, error) { return l.client.CheckLog(l.ctx, lines) }

// ReleaseFiles names the files of a downloaded release a handover is prepared with, each by its absolute
// path, and the archive's name as checksums.txt lists it. The vault process reads and verifies them itself.
type ReleaseFiles struct {
	Checksums   string
	Signature   string
	Archive     string
	ArchiveName string
}

// Handover is a handover prepared on one connection, which its commit uses as well.
type Handover struct {
	kit    *kitvaultproc.Handover
	client *Client
}

// PrepareHandover asks the vault process to hand the vault over to a successor started from the program an
// update is about to install. The process verifies the release and reads the update setting itself. It
// answers vault.UpdateHandover with the Handover to commit once the program is replaced, or vault.UpdateLock
// once it locked itself as the setting asks. ErrHandover means it refused and locked itself; ErrNotRunning
// that there is nothing to hand over. The caller closes a Handover it does not commit.
func (c *Client) PrepareHandover(ctx context.Context, files ReleaseFiles) (vault.UpdateBehaviour, *Handover, error) {
	if err := c.check(); err != nil {
		return "", nil, err
	}
	handover, err := c.kit().PrepareHandover(ctx, selfupdate.ReleaseFiles{Checksums: files.Checksums,
		Signature: files.Signature, Archive: files.Archive, ArchiveName: files.ArchiveName})
	switch {
	case err != nil:
		return "", nil, c.tell(ctx, err)
	case handover == nil:
		return vault.UpdateLock, nil, nil
	}
	return vault.UpdateHandover, &Handover{kit: handover, client: c}, nil
}

// Commit tells the vault process that the program was replaced and waits until it handed the vault over. It
// returns the successor's process id; any other outcome locked the process.
func (h *Handover) Commit(ctx context.Context) (int, error) {
	pid, err := h.kit.Commit(ctx)
	return pid, h.client.tellIn(ctx, err, true)
}

// Close ends a handover that is not committed; the vault process locks itself.
func (h *Handover) Close() error { return h.kit.Close() }

// call runs one of Connect's operations and turns its codes into Connect's errors.
func (c *Client) call(ctx context.Context, op string, body, result any) error {
	if err := c.check(); err != nil {
		return err
	}
	err := c.kit().Call(ctx, op, body, result)
	if told := answerError(err); told != err {
		return told
	}
	return c.tell(ctx, err)
}

// answerError turns an application code into the error a caller tests for, keeping a peer's process id.
func answerError(err error) error {
	var app *kitvaultproc.Error
	if !errors.As(err, &app) {
		return err
	}
	told := appError(app.Code)
	if told == nil {
		return err
	}
	var peer *localipc.PeerError
	if errors.As(err, &peer) && peer.PID > 0 {
		return &PeerError{PID: peer.PID, Err: told}
	}
	return told
}

// appError is the error of one of Connect's answer codes, nil for another.
func appError(code string) error {
	switch code {
	case codeApproval:
		return vault.ErrApprovalRequired
	case codeNoLog:
		return ErrNoLog
	case codeLogFailed:
		return errors.New("the vault process could not write the invocation log")
	case codeNoNames:
		return ErrNoNames
	case codeNamesFailed:
		return errors.New("the vault process could not read the names of the vault")
	case codeNoTokens:
		return ErrNoTokens
	case codeTokenUnknown:
		return vault.ErrTokenUnknown
	case codeTokenExpired:
		return vault.ErrTokenExpired
	case codeTokensTampered:
		return vault.ErrTokensTampered
	case codeTokenFailed:
		return errors.New("the vault process could not approve with the agent token")
	case codeBadRequest:
		return errInvalidRequest
	case codeRoleRefused:
		return ErrRoleRefused
	case codeStoreFailed:
		return errors.New("the vault process could not store the refresh token")
	}
	return nil
}
