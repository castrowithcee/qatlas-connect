// Package invokelog appends one entry per CLI or MCP invoke to the invocation log under the vault directory
// and checks it. The log is an auditlog.Log: daily files vault/logs/YYYY-MM-DD.jsonl, linked by a SHA-256
// hash chain across day boundaries, written under an exclusive lock on vault/logs/.lock. What an entry
// holds is this package's: Fields, Entry and their validation.
//
// A logger given the log key (see Key and Logger.WithKey) also signs every line it writes, retention-cut
// markers included, under keyLabel and names that label as mac_key currentMACKey; lines written before
// under legacyKeyLabel carry no mac_key and stay checkable. A logger without one leaves mac empty, which
// marks a line unverified: Verify counts such a line but never treats it as a break.
//
// A failed Append is reported to the caller, never written silently and never left to abort the invoke it
// was called for.
package invokelog

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/auditlog"
)

const (
	dirMode      = 0o700
	fileMode     = 0o600
	logsDirName  = "logs"
	lockFileName = ".lock"
)

// KindCut marks a retention-cut marker entry (see Entry.Kind), as opposed to the empty Kind of an
// invocation entry.
const KindCut = auditlog.KindCut

// ClientInfo names the MCP client that made an invoke, from its initialize request or its per-request
// declaration; nil on the CLI and wherever a client never declared one.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Cut documents a retention deletion: the sequence number of the last entry the deletion removed.
type Cut = auditlog.Cut

// Entry is one line of the log as decoded, in the field order it is written. Kind is empty for an
// invocation and KindCut for a retention-cut marker, which carries only Cut. The members the log owns for
// its chain and check value (prev_hash, mac_key, mac) are not part of it. No field ever holds an argument,
// a result, a secret, a target, or a URL query.
type Entry struct {
	Seq        uint64      `json:"seq"`
	Time       time.Time   `json:"time"`
	Kind       string      `json:"kind,omitempty"`
	Path       string      `json:"path,omitempty"`
	Client     *ClientInfo `json:"client,omitempty"`
	Operation  string      `json:"operation,omitempty"`
	Version    int         `json:"version,omitempty"`
	Connection string      `json:"connection,omitempty"`
	// ConnectionID is the config id of the connection the entry names, which links entries written under
	// an earlier name; absent where the connection has none and on entries written before it existed.
	// FormerConnection is the name a rename replaced, present on a rename only.
	ConnectionID     string `json:"connection_id,omitempty"`
	FormerConnection string `json:"former_connection,omitempty"`
	// Tool is the one tool ID or "<provider>.<segment>.*" prefix a tool request event is about, present on
	// those events only; see Fields.Tool.
	Tool       string `json:"tool,omitempty"`
	Effect     string `json:"effect,omitempty"`
	Result     string `json:"result,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	// ReqBytes and RespBytes are the exact sizes of a CLI invoke's arguments and successful result. On the
	// mcp path they are the sizes of the whole JSON-RPC request and response messages, an error response
	// included. nil means not recorded, which is different from a recorded 0.
	ReqBytes  *int64 `json:"req_bytes,omitempty"`
	RespBytes *int64 `json:"resp_bytes,omitempty"`
	// Token is the name of the agent token an approval was made with, never its value; empty otherwise.
	Token string `json:"token,omitempty"`
	// Session is the random ID of the MCP process that wrote the entry; see NewSession. It is absent on
	// the CLI and on entries written before it existed.
	Session string `json:"session,omitempty"`
	Cut     *Cut   `json:"cut,omitempty"`
}

// content is what a line holds of an Entry beside the members the log owns, in the order it is written.
type content struct {
	Path             string      `json:"path,omitempty"`
	Client           *ClientInfo `json:"client,omitempty"`
	Operation        string      `json:"operation,omitempty"`
	Version          int         `json:"version,omitempty"`
	Connection       string      `json:"connection,omitempty"`
	ConnectionID     string      `json:"connection_id,omitempty"`
	FormerConnection string      `json:"former_connection,omitempty"`
	Tool             string      `json:"tool,omitempty"`
	Effect           string      `json:"effect,omitempty"`
	Result           string      `json:"result,omitempty"`
	DurationMS       int64       `json:"duration_ms,omitempty"`
	ReqBytes         *int64      `json:"req_bytes,omitempty"`
	RespBytes        *int64      `json:"resp_bytes,omitempty"`
	Token            string      `json:"token,omitempty"`
	Session          string      `json:"session,omitempty"`
}

func entryOf(r auditlog.Record[content]) Entry {
	c := r.Entry
	return Entry{Seq: r.Seq, Time: r.Time, Kind: r.Kind, Path: c.Path, Client: c.Client, Operation: c.Operation,
		Version: c.Version, Connection: c.Connection, ConnectionID: c.ConnectionID,
		FormerConnection: c.FormerConnection, Tool: c.Tool, Effect: c.Effect, Result: c.Result,
		DurationMS: c.DurationMS, ReqBytes: c.ReqBytes, RespBytes: c.RespBytes, Token: c.Token, Session: c.Session,
		Cut: r.Cut}
}

// Fields is what a caller supplies for one completed invoke; Append adds the members the log owns.
type Fields struct {
	// Path is "cli" or "mcp" for an invoke, and "tui", "web" or "cli" for a change qconnect made to a
	// connection in the configuration file (see the Operation constants).
	Path       string
	Client     *ClientInfo
	Operation  string
	Version    int
	Connection string
	// ConnectionID and FormerConnection are those of Entry; only a rename carries FormerConnection.
	ConnectionID     string
	FormerConnection string
	// Tool is the tool ID or "<provider>.<segment>.*" prefix of a tool request event, required there and
	// refused on every other operation. It is never free text, a reason, or an argument.
	Tool   string
	Effect string
	// Result is "success" or the failure's error code, the same value application.auditResult uses.
	Result   string
	Duration time.Duration
	// ReqBytes and RespBytes are nil where not recorded. On the cli path they are the size of the invoke's
	// arguments and of its compact result, and RespBytes stays nil for a failed invoke. On the mcp path
	// they are the sizes of the whole JSON-RPC request and response messages, and RespBytes is also
	// recorded for an error response. Neither is ever set for anything but an invoke or an mcp operation.
	ReqBytes  *int64
	RespBytes *int64
	// Token names the agent token an approval of a connection change was made with, never its value; empty
	// for an invoke.
	Token string
	// Session is the NewSession ID of the MCP process; the mcp path always has one and no other path has
	// any.
	Session string
}

// The bounds Validate holds the fields of an entry to.
const (
	maxTextLength   = 256
	maxClientLength = 128
	maxVersion      = 1 << 20
)

// MaxReqBytes and MaxRespBytes bound the size counters Validate accepts. The request bound is the CLI's
// limit for one agent request, which this package cannot import; the MCP reader refuses a longer message
// line, so a request message never exceeds it either.
const (
	MaxReqBytes  int64 = 8 << 20
	MaxRespBytes int64 = 1 << 30
)

// MaxDuration is the longest duration Validate accepts for one invoke.
const MaxDuration = 30 * 24 * time.Hour

// sessionPattern is the form of Fields.Session: 8 random bytes as lowercase hex.
var sessionPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ValidSession reports whether s has the form of a NewSession ID.
func ValidSession(s string) bool { return sessionPattern.MatchString(s) }

// IsReservedOperation reports whether name is one of the operations only an MCP session's own exchanges log,
// which no invoke entry may carry, whatever a client asked for.
func IsReservedOperation(name string) bool {
	_, reserved := mcpOperations[name]
	return reserved
}

// NewSession returns a random session ID for one MCP process. It is not derived from the client, the user,
// or anything a call carries, so entries of one session can be grouped but traced to nothing.
func NewSession() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("create a session ID: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// resultPattern is the form of Fields.Result: "success" or an error code, a lowercase word with dashes.
var resultPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// validEffects are the effects an operation can declare, and the empty effect of an invoke that failed
// before its operation was known.
var validEffects = map[string]bool{"": true, "read": true, "create": true, "update": true, "delete": true,
	"execute": true}

// The operations that log a change qconnect made to a connection in the configuration file, one entry per
// changed connection, with the connection's name and no value of it. Their Path is the surface that made the
// change, "tui", "web" or "cli", and their Effect is create, update, or delete respectively; a rename is an
// update that names the new name as Connection and the old one as FormerConnection.
const (
	OperationConnectionCreate = "config.connection.create"
	OperationConnectionChange = "config.connection.change"
	OperationConnectionDelete = "config.connection.delete"
	OperationConnectionRename = "config.connection.rename"
)

// The operations that log a consent a person gave or revoked for the credential of a connection, one entry
// per connection that uses the credential, with no value of it. Their Path is "cli" or "web", and a revocation
// also "tui". A grant is an update, a revocation a delete; the result of a revocation the provider did not
// confirm is RevokeUnconfirmed.
const (
	OperationConsentGrant  = "config.consent.grant"
	OperationConsentRevoke = "config.consent.revoke"
	RevokeUnconfirmed      = "unconfirmed"
)

// The operations that log a person's reading of a connection's provider to choose targets, on the surface
// ("tui" or "web") that did it; neither the CLI nor MCP chooses targets. Their Effect is read. A listing names the kind of the listed entries
// after a colon (TargetListOperation); neither entry holds a target, a name, or a search text.
const (
	OperationTargetList    = "config.target.list"
	OperationTargetResolve = "config.target.resolve"
)

// The operations of a target choice for a connection that is not saved yet, read before any approval of it
// with the surface's admin session. Their Connection is DraftConnection and they carry no connection id; a
// listing names its kind the way TargetListOperation does.
const (
	OperationDraftTargetList    = "config.target.draft.list"
	OperationDraftTargetResolve = "config.target.draft.resolve"
)

// DraftConnection is what the entry of a draft's target choice names as its connection. It is no valid
// connection name, so it never stands for a saved connection, and it carries nothing the person typed.
const DraftConnection = "(draft)"

// TargetListOperation is the operation of a listing of the entries of kind.
func TargetListOperation(kind string) string { return OperationTargetList + ":" + kind }

// DraftTargetListOperation is the operation of a listing of the entries of kind for a draft.
func DraftTargetListOperation(kind string) string { return OperationDraftTargetList + ":" + kind }

// IsTargetOperation reports whether operation is the one of a target listing or URL resolution, a draft's
// included.
func IsTargetOperation(operation string) bool {
	return operation == OperationTargetResolve || operation == OperationTargetList ||
		strings.HasPrefix(operation, OperationTargetList+":") || IsDraftTargetOperation(operation)
}

// IsDraftTargetOperation reports whether operation is the one of a draft's target listing or URL resolution.
func IsDraftTargetOperation(operation string) bool {
	return operation == OperationDraftTargetResolve || operation == OperationDraftTargetList ||
		strings.HasPrefix(operation, OperationDraftTargetList+":")
}

// The operations the vault process logs for its own decisions, which are not invokes: an approval with an
// agent token, a lock it decides on itself, and the outcome of a handover.
const (
	OperationApprove  = "vault.approve"
	OperationLock     = "vault.lock"
	OperationHandover = "vault.handover"
)

// OperationStoreRefreshToken logs one write of a rotated refresh token, the only role a run writes itself: by
// the vault process for an encrypted vault, by the run otherwise. Its Effect is update, its Connection the
// connection the write was made for, and its Result success, conflict where another writer stored first, or
// the code of the refusal or failure. It never holds a value or a hash of one.
const OperationStoreRefreshToken = "credential.store-refresh-token"

// The operations that log the events of a tool request, one entry per tool, with the connection and the
// requested tool (Fields.Tool) and nothing the requester wrote. A request is created over "cli" or "mcp", a
// decision is made over "tui" or "cli", and an expiry or a drop, which the process that notices it logs, may
// come from any path. Creating is a create, deciding an update, and expiring or dropping a delete. None of
// them is an invoke.
const (
	OperationRequestCreate  = "config.request.create"
	OperationRequestAccept  = "config.request.accept"
	OperationRequestDecline = "config.request.decline"
	OperationRequestBlock   = "config.request.block"
	OperationRequestExpire  = "config.request.expire"
	OperationRequestDrop    = "config.request.drop"
)

// The same events for a request to remove tools from a connection. Each entry still holds one tool, so the
// kind of the request is part of the operation name and never of a field the requester could fill.
const (
	OperationRequestRemoveCreate = "config.request.remove.create"
	OperationRequestRemoveExpire = "config.request.remove.expire"
	OperationRequestRemoveDrop   = "config.request.remove.drop"
	// A person's decision on such a request. Blocking does not exist for it.
	OperationRequestRemoveAccept  = "config.request.remove.accept"
	OperationRequestRemoveDecline = "config.request.remove.decline"
)

// requestOperations maps each tool request operation to its effect and the paths it may carry.
var requestOperations = map[string]struct {
	effect string
	paths  []string
}{
	OperationRequestCreate:  {"create", []string{"cli", "mcp"}},
	OperationRequestAccept:  {"update", []string{"tui", "cli"}},
	OperationRequestDecline: {"update", []string{"tui", "cli"}},
	OperationRequestBlock:   {"update", []string{"tui", "cli"}},
	OperationRequestExpire:  {"delete", []string{"cli", "mcp", "tui", "web"}},
	OperationRequestDrop:    {"delete", []string{"cli", "mcp", "tui", "web"}},

	OperationRequestRemoveCreate: {"create", []string{"cli", "mcp"}},
	OperationRequestRemoveExpire: {"delete", []string{"cli", "mcp", "tui", "web"}},
	OperationRequestRemoveDrop:   {"delete", []string{"cli", "mcp", "tui", "web"}},

	OperationRequestRemoveAccept:  {"update", []string{"tui", "cli"}},
	OperationRequestRemoveDecline: {"update", []string{"tui", "cli"}},
}

// IsRequestOperation reports whether operation is one of a tool request's events.
func IsRequestOperation(operation string) bool {
	_, ok := requestOperations[operation]
	return ok
}

// toolSegmentPattern is one dot-separated segment of a tool ID or prefix.
var toolSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validToolRef reports whether tool has the form of a tool ID (at least three segments) or of a prefix,
// "<provider>.<segment>.*" with one trailing wildcard. It checks the form only, not whether a tool exists.
func validToolRef(tool string) bool {
	segments := strings.Split(tool, ".")
	if len(segments) < 3 || len(tool) > maxTextLength {
		return false
	}
	if segments[len(segments)-1] == "*" {
		segments = segments[:len(segments)-1]
		if len(segments) < 2 {
			return false
		}
	}
	for _, segment := range segments {
		if !toolSegmentPattern.MatchString(segment) {
			return false
		}
	}
	return true
}

// OperationConfigMigrate logs that qconnect moved the configuration file to the schema named by Version. Its
// Path is "cli", whichever command migrated.
const OperationConfigMigrate = "config.migrate"

// The operations an MCP session logs besides the invokes it forwards, one entry per JSON-RPC exchange. The
// baseline pair is what every session loads before its first call; search and describe are the navigation
// that finds a tool. None of them names a connection, an effect, or anything the agent asked for. The
// baseline names contain a slash, which a tool ID segment never does, and the navigation names have two
// segments where a tool ID has at least three, so none can be the ID of a tool.
const (
	OperationMCPInitialize = "mcp/initialize"
	OperationMCPToolsList  = "mcp/tools-list"
	OperationMCPSearch     = "qconnect.search"
	OperationMCPDescribe   = "qconnect.describe"
)

// The navigation names of earlier releases. Entries written under them stay valid and are read as before;
// new entries never carry them. Transition: dropped with the next major release.
const (
	legacyOperationMCPSearch   = "qatlas.search"
	legacyOperationMCPDescribe = "qatlas.describe"
)

// mcpOperations maps each reserved mcp operation to its area.
var mcpOperations = map[string]Area{
	OperationMCPInitialize: AreaBaseline, OperationMCPToolsList: AreaBaseline,
	OperationMCPSearch: AreaNavigation, OperationMCPDescribe: AreaNavigation,
	legacyOperationMCPSearch: AreaNavigation, legacyOperationMCPDescribe: AreaNavigation,
}

// managementOperations holds every operation an entry can carry that is not an invoke, besides the mcp
// ones; IsInvoke excludes them, so a new one is added here.
var managementOperations = map[string]bool{
	OperationApprove: true, OperationLock: true, OperationHandover: true,
	OperationConnectionCreate: true, OperationConnectionChange: true, OperationConnectionDelete: true,
	OperationConnectionRename: true, OperationConfigMigrate: true, OperationStoreRefreshToken: true,
	OperationConsentGrant: true, OperationConsentRevoke: true,
	OperationRequestCreate: true, OperationRequestAccept: true, OperationRequestDecline: true,
	OperationRequestBlock: true, OperationRequestExpire: true, OperationRequestDrop: true,
	OperationRequestRemoveCreate: true, OperationRequestRemoveExpire: true, OperationRequestRemoveDrop: true,
	OperationRequestRemoveAccept: true, OperationRequestRemoveDecline: true,
}

// consentOperations maps each consent operation to the effect it must carry.
var consentOperations = map[string]string{
	OperationConsentGrant:  "update",
	OperationConsentRevoke: "delete",
}

// connectionOperations maps each connection operation to the effect it must carry.
var connectionOperations = map[string]string{
	OperationConnectionCreate: "create",
	OperationConnectionChange: "update",
	OperationConnectionDelete: "delete",
	OperationConnectionRename: "update",
}

// connectionIDPattern is the form of Fields.ConnectionID: a config id, 16 random bytes in lower-case hex.
var connectionIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Validate reports whether f is something an invoke of this program could log: a known path, a result of
// the form auditResult gives, a known effect, non-negative numbers, and text fields of bounded length that
// are valid UTF-8 without a control character. A writer that takes fields from another process checks
// them with it before anything reaches a file. The error names the field, never its value.
func (f Fields) Validate() error {
	switch f.Path {
	case "cli", "mcp":
	case "tui", "web":
		// A surface of the interactive editors only ever logs a change to a connection or a target choice, and
		// the web also a consent. The TUI only revokes one.
		if connectionOperations[f.Operation] == "" && !IsTargetOperation(f.Operation) &&
			!slices.Contains(requestOperations[f.Operation].paths, f.Path) &&
			(f.Path != "web" || consentOperations[f.Operation] == "") &&
			(f.Path != "tui" || f.Operation != OperationConsentRevoke) {
			return errors.New("operation is not a connection change")
		}
	default:
		return errors.New("path must be cli, mcp, tui, or web")
	}
	if (f.Path == "mcp" || f.Session != "") && (f.Path != "mcp" || !sessionPattern.MatchString(f.Session)) {
		return errors.New("session must be a NewSession ID, and only on the mcp path")
	}
	if _, reserved := mcpOperations[f.Operation]; reserved {
		// Nothing the agent supplied may be left in such an entry, so it has no connection even where the
		// call was filtered by one.
		switch {
		case f.Path != "mcp":
			return errors.New("an mcp operation needs the mcp path")
		case f.Connection != "" || f.Effect != "" || f.Version != 0:
			return errors.New("an mcp operation has no connection, effect, or version")
		case f.ReqBytes == nil || f.RespBytes == nil:
			return errors.New("an mcp operation needs both size counters")
		}
	}
	if effect, ok := connectionOperations[f.Operation]; ok && (f.Effect != effect || f.Connection == "") {
		return errors.New("a connection change needs its effect and a connection")
	}
	if effect, ok := consentOperations[f.Operation]; ok && (f.Effect != effect || f.Connection == "") {
		return errors.New("a consent needs its effect and a connection")
	}
	if rule, ok := requestOperations[f.Operation]; ok {
		if f.Effect != rule.effect || f.Connection == "" || !slices.Contains(rule.paths, f.Path) {
			return errors.New("a tool request event needs its effect, a connection, and one of its paths")
		}
		if !validToolRef(f.Tool) {
			return errors.New("a tool request event needs a tool ID or a prefix of the form <provider>.<segment>.*")
		}
	} else if f.Tool != "" {
		return errors.New("only a tool request event has a tool")
	}
	if IsTargetOperation(f.Operation) && (f.Effect != "read" || f.Connection == "" || f.Path != "tui" && f.Path != "web") {
		return errors.New("a target choice needs the read effect, a connection, and the tui or web path")
	}
	if IsTargetOperation(f.Operation) &&
		(IsDraftTargetOperation(f.Operation) != (f.Connection == DraftConnection) || IsDraftTargetOperation(f.Operation) && f.ConnectionID != "") {
		return errors.New("a draft's target choice names the draft and no connection id, and only it does")
	}
	if f.ConnectionID != "" && (f.Connection == "" || !connectionIDPattern.MatchString(f.ConnectionID)) {
		return errors.New("connection_id must be a config id, and needs a connection")
	}
	rename := f.Operation == OperationConnectionRename
	if rename != (f.FormerConnection != "") || rename && f.FormerConnection == f.Connection {
		return errors.New("a rename needs the former connection name, and only a rename has one")
	}
	if !resultPattern.MatchString(f.Result) {
		return errors.New("result must be success or an error code")
	}
	if !validEffects[f.Effect] {
		return errors.New("effect is not a known effect")
	}
	if f.Version < 0 || f.Version > maxVersion {
		return errors.New("version is out of range")
	}
	if f.Duration < 0 || f.Duration > MaxDuration {
		return errors.New("duration is out of range")
	}
	if f.ReqBytes != nil || f.RespBytes != nil {
		if connectionOperations[f.Operation] != "" || consentOperations[f.Operation] != "" || IsTargetOperation(f.Operation) ||
			IsRequestOperation(f.Operation) {
			return errors.New("a connection change has no size counters")
		}
		if f.RespBytes != nil && f.Result != "success" && f.Path != "mcp" {
			return errors.New("resp_bytes needs a successful result")
		}
	}
	if f.ReqBytes != nil && (*f.ReqBytes < 0 || *f.ReqBytes > MaxReqBytes) {
		return errors.New("req_bytes is out of range")
	}
	if f.RespBytes != nil && (*f.RespBytes < 0 || *f.RespBytes > MaxRespBytes) {
		return errors.New("resp_bytes is out of range")
	}
	if err := checkText("operation", f.Operation, maxTextLength); err != nil {
		return err
	}
	if err := checkText("connection", f.Connection, maxTextLength); err != nil {
		return err
	}
	if err := checkText("tool", f.Tool, maxTextLength); err != nil {
		return err
	}
	if err := checkText("former connection", f.FormerConnection, maxTextLength); err != nil {
		return err
	}
	if err := checkText("token", f.Token, maxTextLength); err != nil {
		return err
	}
	if f.Client != nil {
		if err := checkText("client name", f.Client.Name, maxClientLength); err != nil {
			return err
		}
		if err := checkText("client version", f.Client.Version, maxClientLength); err != nil {
			return err
		}
	}
	return nil
}

func checkText(name, value string, max int) error {
	if len(value) > max || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s must be at most %d bytes of text without control characters", name, max)
	}
	return nil
}

// Writer is what an invoke's entry is handed to: a *Logger, or a writer that decides per entry who signs it.
type Writer interface {
	Append(Fields) error
}

// UnsignedError reports an entry that was written, only without a check value, because Err kept the
// writer that would have signed it from doing so. The entry is in the log and chained like any other; the
// caller reports the reason as a warning.
type UnsignedError struct{ Err error }

func (e *UnsignedError) Error() string { return e.Err.Error() }

func (e *UnsignedError) Unwrap() error { return e.Err }

// Logger appends invoke entries under one vault directory's logs subdirectory. The zero value is not
// usable; use New.
type Logger struct {
	opts auditlog.Options[content]
	// key signs every entry written; nil leaves them unsigned.
	key *Key
}

// New returns a logger writing under vaultDir/logs that keeps retentionDays days of history. Zero keeps
// everything: the logger never removes a day file, which is what a writer without a trusted retention
// must do. It touches no file: like the vault itself, the directory and its files are created lazily, on the
// first Append, and New never initializes a vault.
func New(vaultDir string, retentionDays int) *Logger {
	return &Logger{opts: logOptions(vaultDir, retentionDays)}
}

// logOptions configures the log of vaultDir. Every line signed is signed under currentMACKey.
func logOptions(vaultDir string, retentionDays int) auditlog.Options[content] {
	return auditlog.Options[content]{
		Dir:      logsDir(vaultDir),
		LockName: lockFileName,
		// Append records whatever an invoke of this process produced; a writer that takes fields from another
		// process checks them with Fields.Validate first, so no entry is dropped here.
		Validate:      func(content) error { return nil },
		RetentionDays: retentionDays,
		KeyID:         currentMACKey,
	}
}

// openLog is the log of vaultDir for reading and checking.
func openLog(vaultDir string) (*auditlog.Log[content], error) {
	return auditlog.New(logOptions(vaultDir, 0))
}

func logsDir(vaultDir string) string { return filepath.Join(vaultDir, logsDirName) }

// WithKey returns a logger like l that signs every entry it writes with key; see Key. A nil key signs
// nothing, like l itself. The key stays the caller's, who clears it once no logger uses it any more.
func (l *Logger) WithKey(key *Key) *Logger {
	out := *l
	out.key = key
	return &out
}

// WithRetention returns a logger like l that keeps retentionDays days of history; zero never removes a
// day file. A writer sets it per append, once it knows which retention is trusted.
func (l *Logger) WithRetention(retentionDays int) *Logger {
	out := *l
	out.opts.RetentionDays = retentionDays
	return &out
}

// Signs reports whether l signs the entries it writes.
func (l *Logger) Signs() bool {
	log, err := auditlog.New(l.opts)
	return err == nil && log.WithKey(l.key).Signs()
}

// Append writes one entry for a completed invoke, chained to the last entry this installation ever wrote,
// across day files and across processes; retention runs first, under the same lock, and a removal is
// documented by a KindCut marker right before the entry. See auditlog.Log.Append.
//
// A returned error means the entry might not be durable. The caller reports it as a warning; it must never
// turn a completed invoke into a failure or change its result.
func (l *Logger) Append(f Fields) error {
	log, err := auditlog.New(l.opts)
	if err != nil {
		return err
	}
	return log.WithKey(l.key).Append(content{
		Path: f.Path, Client: f.Client, Operation: f.Operation, Version: f.Version, Connection: f.Connection,
		ConnectionID: f.ConnectionID, FormerConnection: f.FormerConnection, Tool: f.Tool, Effect: f.Effect,
		Result: f.Result, DurationMS: f.Duration.Milliseconds(), ReqBytes: f.ReqBytes, RespBytes: f.RespBytes,
		Token: f.Token, Session: f.Session,
	})
}
