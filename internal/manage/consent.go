package manage

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/connlog"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
)

// ConsentTimeout is how long one consent start waits for the answer of the provider.
const ConsentTimeout = 5 * time.Minute

const (
	// consentPath is the fixed path of the redirect address.
	consentPath = "/consent"
	// maxAnswerBytes bounds a pasted address and the authorization code it carries.
	maxAnswerBytes = 8 << 10
	// receiverShutdown is how long the receiver lets the answer page of the winning request finish.
	receiverShutdown = 2 * time.Second
)

// ConsentSource gives the consent endpoints a provider registered, false for one that registered none. The
// caller supplies it, so this package knows no provider.
type ConsentSource func(providerID string) (provider.OAuth, bool)

// WithConsentSource returns a copy of s that gives and revokes consents through source.
func (s *Service) WithConsentSource(source ConsentSource) *Service {
	c := *s
	c.consent = source
	return &c
}

// ConsentReason says why a consent or a revocation ended without its effect.
type ConsentReason string

// The reasons of a ConsentError. The first ones rule a consent out before anything is sent (see
// ConsentError.Precondition); the others end a consent that was started.
const (
	ConsentAdminRequired   ConsentReason = "admin-required"
	ConsentNotStored       ConsentReason = "not-stored"
	ConsentNoProvider      ConsentReason = "no-provider"
	ConsentNoClientID      ConsentReason = "no-client-id"
	ConsentServiceAccount  ConsentReason = "service-account"
	ConsentNoConnection    ConsentReason = "no-connection"
	ConsentNoImpersonation ConsentReason = "no-impersonation"
	ConsentMissing         ConsentReason = "no-consent"

	ConsentExpired        ConsentReason = "expired"
	ConsentCancelled      ConsentReason = "cancelled"
	ConsentRefused        ConsentReason = "refused"
	ConsentNoCode         ConsentReason = "no-code"
	ConsentNoRefreshToken ConsentReason = "no-refresh-token"
)

// ConsentError is a consent or revocation that ended without its effect. Its text is fixed per reason and
// never carries anything a provider said or a value of the credential.
type ConsentError struct {
	Reason     ConsentReason
	Credential string
}

func (e *ConsentError) Error() string {
	name := strconv.Quote(e.Credential)
	switch e.Reason {
	case ConsentAdminRequired:
		return "giving or revoking a consent needs the admin proof of this run"
	case ConsentNotStored:
		return "credential " + name + " keeps no stored secrets: a consent needs a keyring or vault credential, " +
			"where its refresh token can be stored; nothing was sent"
	case ConsentNoProvider:
		return "credential " + name + " names no provider that takes a consent; nothing was sent"
	case ConsentNoClientID:
		return "credential " + name + " holds no client ID; store the ID of your own OAuth client in it first; " +
			"nothing was sent"
	case ConsentServiceAccount:
		return "credential " + name + " holds a service account key, which takes no consent; nothing was sent"
	case ConsentNoConnection:
		return "no connection uses credential " + name + ", so there is no scope to ask for; create its " +
			"connections first; nothing was sent"
	case ConsentNoImpersonation:
		return "a connection of credential " + name + " impersonates a service account, which its provider does " +
			"not offer; remove service_account from it; nothing was sent"
	case ConsentMissing:
		return "credential " + name + " holds no refresh token, so there is no consent to revoke"
	case ConsentExpired:
		return "the answer for the consent of credential " + name + " did not arrive in time; nothing was stored"
	case ConsentCancelled:
		return "the consent of credential " + name + " was cancelled; nothing was stored"
	case ConsentRefused:
		return "the provider answered that the consent of credential " + name + " was not given; nothing was stored"
	case ConsentNoCode:
		return "the answer for credential " + name + " carried no usable authorization code; nothing was stored"
	case ConsentNoRefreshToken:
		return "the token endpoint returned no refresh token for credential " + name + ", so nothing was " +
			"stored; check the setup of the provider and give the consent again"
	}
	return "the consent of credential " + name + " ended without effect"
}

// Precondition reports whether the credential or the run itself rules the consent or revocation out, before
// anything was sent or a link was given.
func (e *ConsentError) Precondition() bool {
	switch e.Reason {
	case ConsentAdminRequired, ConsentNotStored, ConsentNoProvider, ConsentNoClientID, ConsentServiceAccount,
		ConsentNoConnection, ConsentNoImpersonation, ConsentMissing:
		return true
	}
	return false
}

// The refusals of an answer that does not belong to the consent waiting for it. None of them uses up the
// consent, and none repeats what the answer carried.
var (
	ErrNotRedirect  = errors.New("this is not the address the browser was sent to: scheme, host, port and path must match the redirect address of this consent")
	ErrOtherConsent = errors.New("this address belongs to another consent start; paste the address of the current one")
	ErrConsentEnded = errors.New("this consent no longer waits for an answer")
)

// Consent is one started consent of a credential: the link a person opens, a receiver on the loopback
// address that the provider sends the browser back to, and the state and PKCE verifier that bind the answer
// to this start. State and verifier live only in this value. Exactly one answer whose state matches is
// taken; an answer that does not match takes nothing.
type Consent struct {
	svc         *Service
	cfg         *config.Config
	credential  string
	entry       config.Credential
	oauth       provider.OAuth
	secrets     provider.TokenSecrets
	scopes      []string
	connections []string

	state, verifier string
	host, redirect  string
	link            string
	deadline        time.Time

	server    *http.Server
	closeOnce sync.Once

	mu      sync.Mutex
	ended   bool
	answers chan consentAnswer
}

// consentAnswer is the one answer taken: an authorization code, or a refusal.
type consentAnswer struct {
	code   string
	reason ConsentReason
}

// ConsentOutcome is what a stored consent leaves for the surface to show.
type ConsentOutcome struct {
	// Connections are the connections of the credential, sorted; each got an entry in the connection log.
	Connections []string
	// MissingScopes are scopes the consent asked for and the token endpoint did not grant.
	MissingScopes []string
	// ProcessWarning is the redacted warning of handing the refresh token on to a vault process.
	ProcessWarning string
	// LogWarning is the redacted warning of the connection log.
	LogWarning string
}

// StartConsent starts the consent of credential and returns it with the link to open: it checks that the
// credential is stored in a keyring or the vault, names a provider that takes a consent and holds a client
// ID, asks for the scopes all its connections need, and listens on 127.0.0.1 with a port the system picks.
// A connection that impersonates a service account needs only the scope of the impersonation request, since
// its own scopes go to that request.
// Nothing is sent and nothing is stored before Wait. proof is the admin proof of this run; it covers the
// whole consent. The errors are a *ConsentError, a *config.NotThereError, or what reading the store gave.
func (s *Service) StartConsent(ctx context.Context, proof AdminProof, credential string) (*Consent, error) {
	if !proof.granted {
		return nil, &ConsentError{Reason: ConsentAdminRequired, Credential: credential}
	}
	cfg, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	entry, oauth, ok, err := s.consentTarget(cfg, credential)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &ConsentError{Reason: ConsentNoProvider, Credential: credential}
	}
	var secrets provider.TokenSecrets
	var key string
	for _, role := range []struct {
		name  string
		value *string
	}{
		{config.RoleClientID, &secrets.ClientID},
		{config.RoleClientSecret, &secrets.ClientSecret},
		{config.RoleServiceAccountKey, &key},
	} {
		if *role.value, _, err = s.storedRole(ctx, credential, entry, role.name); err != nil {
			return nil, err
		}
	}
	s.register(secrets.ClientSecret, key)
	switch {
	case key != "":
		return nil, &ConsentError{Reason: ConsentServiceAccount, Credential: credential}
	case secrets.ClientID == "":
		return nil, &ConsentError{Reason: ConsentNoClientID, Credential: credential}
	}
	connections, scopes, impersonates := consentScopes(cfg, credential, oauth.ImpersonateScope)
	if impersonates && oauth.ImpersonateScope == "" {
		return nil, &ConsentError{Reason: ConsentNoImpersonation, Credential: credential}
	}
	if len(connections) == 0 || len(scopes) == 0 {
		return nil, &ConsentError{Reason: ConsentNoConnection, Credential: credential}
	}
	state, err := provider.NewState()
	if err != nil {
		return nil, err
	}
	verifier, challenge, err := provider.NewPKCE()
	if err != nil {
		return nil, err
	}
	// The state travels in the link and is no secret; the verifier never leaves this process.
	s.register(verifier)

	// Only 127.0.0.1 is ever bound: localhost may resolve elsewhere, and any other interface would take an
	// answer from another machine.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("the receiver of the consent could not listen on 127.0.0.1: %w", err)
	}
	host := "127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	timeout := s.consentTimeout
	if timeout <= 0 {
		timeout = ConsentTimeout
	}
	c := &Consent{
		svc: s, cfg: cfg, credential: credential, entry: entry, oauth: oauth, secrets: secrets,
		scopes: scopes, connections: connections, state: state, verifier: verifier,
		host: host, redirect: "http://" + host + consentPath, deadline: time.Now().Add(timeout),
		answers: make(chan consentAnswer, 1),
	}
	c.link = oauth.AuthorizationURL(secrets.ClientID, c.redirect, scopes, state, challenge)
	c.server = &http.Server{
		Handler: http.HandlerFunc(c.receive), ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: maxAnswerBytes,
		// The server's own log could carry a request line, and with it an authorization code.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	go func() { _ = c.server.Serve(listener) }()
	return c, nil
}

// Link is the authorization link to open. It carries the state and the PKCE challenge, never a secret.
func (c *Consent) Link() string { return c.link }

// Deadline is when the consent stops waiting.
func (c *Consent) Deadline() time.Time { return c.deadline }

// Paste takes the address a browser on another device was sent back to. It is taken only when its scheme,
// host, port and path are those of the redirect address and its state matches; otherwise it returns
// ErrNotRedirect, ErrOtherConsent or ErrConsentEnded and the consent keeps waiting, unless it ended.
func (c *Consent) Paste(raw string) error {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxAnswerBytes {
		return ErrNotRedirect
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host != c.host || u.Path != consentPath || u.User != nil ||
		u.Opaque != "" {
		return ErrNotRedirect
	}
	return c.take(u.Query())
}

// receive is the receiver on the loopback address. Against DNS rebinding it takes only a GET of the fixed
// path whose Host is exactly the redirect address; anything else is refused without using up the consent.
// No answer page holds a code, a token or a script.
func (c *Consent) receive(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'")
	if r.Host != c.host || r.URL.Path != consentPath {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		h.Set("Allow", http.MethodGet)
		http.Error(w, "only GET is accepted", http.StatusMethodNotAllowed)
		return
	}
	switch err := c.take(r.URL.Query()); {
	case err == nil:
		_, _ = io.WriteString(w, "qconnect received the answer. Return to the terminal; you can close this page.\n")
	case errors.Is(err, ErrConsentEnded):
		http.Error(w, "qconnect no longer waits for this consent.", http.StatusGone)
	default:
		http.Error(w, "This address does not belong to the consent qconnect waits for.", http.StatusBadRequest)
	}
}

// take takes one answer with a matching state, once, before the deadline.
func (c *Consent) take(query url.Values) error {
	if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(c.state)) != 1 {
		return ErrOtherConsent
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ended || !time.Now().Before(c.deadline) {
		c.ended = true
		return ErrConsentEnded
	}
	c.ended = true
	code := query.Get("code")
	c.svc.register(code)
	switch {
	case query.Has("error"):
		// The provider's error and its description stay unread: neither reaches a message.
		c.answers <- consentAnswer{reason: ConsentRefused}
	case !validCode(code):
		c.answers <- consentAnswer{reason: ConsentNoCode}
	default:
		c.answers <- consentAnswer{code: code}
	}
	return nil
}

// end stops taking answers.
func (c *Consent) end() {
	c.mu.Lock()
	c.ended = true
	c.mu.Unlock()
}

// Close stops the receiver and discards the consent; nothing is stored. It may be called more than once.
func (c *Consent) Close() {
	c.end()
	c.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), receiverShutdown)
		defer cancel()
		if c.server.Shutdown(ctx) != nil {
			_ = c.server.Close()
		}
	})
}

// Wait waits for the first answer the receiver or Paste takes, until the deadline or until ctx ends, and
// then redeems the code with the verifier and the redirect address and stores the refresh token through the
// write completion of SetSecret. A deadline, a cancellation, a refusal by the provider and an answer without
// a refresh token store nothing and return a *ConsentError; a refused exchange returns the *provider.Error
// of the token endpoint. The consent is spent afterwards.
func (c *Consent) Wait(ctx context.Context) (ConsentOutcome, error) {
	defer c.Close()
	timer := time.NewTimer(time.Until(c.deadline))
	defer timer.Stop()
	var got consentAnswer
	select {
	case got = <-c.answers:
	case <-timer.C:
		c.end()
		// An answer taken just before the deadline still counts.
		select {
		case got = <-c.answers:
		default:
			return ConsentOutcome{}, c.fail(ConsentExpired)
		}
	case <-ctx.Done():
		c.end()
		return ConsentOutcome{}, c.fail(ConsentCancelled)
	}
	c.Close()
	if got.reason != "" {
		return ConsentOutcome{}, c.fail(got.reason)
	}

	token, err := c.oauth.Exchange(ctx, c.secrets, got.code, c.verifier, c.redirect)
	c.svc.register(token.Access, token.Refresh)
	if err != nil {
		if ctx.Err() != nil {
			return ConsentOutcome{}, c.fail(ConsentCancelled)
		}
		return ConsentOutcome{}, err
	}
	if token.Refresh == "" {
		return ConsentOutcome{}, c.fail(ConsentNoRefreshToken)
	}
	if ctx.Err() != nil {
		return ConsentOutcome{}, c.fail(ConsentCancelled)
	}
	out := ConsentOutcome{Connections: c.connections}
	if token.Scopes != nil {
		for _, scope := range c.scopes {
			if !slices.Contains(token.Scopes, scope) {
				out.MissingScopes = append(out.MissingScopes, scope)
			}
		}
	}
	stored, err := c.svc.SetSecret(ctx, c.cfg, SecretWrite{
		Name: c.credential, Entry: c.entry, Role: config.RoleRefreshToken, Value: token.Refresh,
		Approval: ApprovalNone,
	})
	if err != nil {
		return ConsentOutcome{}, err
	}
	out.ProcessWarning = stored.ProcessWarning
	out.LogWarning = c.svc.recordConsent(c.cfg, c.connections, invokelog.OperationConsentGrant, "success")
	return out, nil
}

func (c *Consent) fail(reason ConsentReason) error {
	return &ConsentError{Reason: reason, Credential: c.credential}
}

// RevokeOutcome is what a revocation leaves for the surface to show.
type RevokeOutcome struct {
	// Confirmed reports that the provider confirmed the revocation. Without it the consent may still hold at
	// the provider, although the refresh token is removed here.
	Confirmed bool
	// Note is the text of Revocation.Note for the outcome.
	Note string
	// ProcessWarning is the redacted warning of handing the removal on to a vault process.
	ProcessWarning string
	// LogWarning is the redacted warning of the connection log.
	LogWarning string
}

// Revocation is what revoking a consent did before its refresh token was removed (see DeleteSecret).
type Revocation struct {
	Credential string
	// Confirmed reports that the provider confirmed the revocation.
	Confirmed bool
	// LogWarning is the redacted warning of the connection log, "" for none.
	LogWarning string
}

// Note is the text every surface shows for the revocation: that it was confirmed, or the warning that the
// consent may still be valid at the provider.
func (r *Revocation) Note() string {
	if r.Confirmed {
		return fmt.Sprintf("the consent of %s was revoked at the provider", r.Credential)
	}
	return fmt.Sprintf("warning: the provider did not confirm the revocation, so the consent of %s may still be "+
		"valid there; remove the access of your OAuth client in the account settings of the provider", r.Credential)
}

// RevokeConsent revokes the consent of credential and removes its refresh token, the way DeleteSecret
// removes the refresh token of a credential that holds a consent. proof is the admin proof of this run. The
// errors are a *ConsentError (ConsentMissing when nothing is stored), a *config.NotThereError, or what the
// store gave; when removing failed, the outcome still says whether the provider confirmed.
func (s *Service) RevokeConsent(ctx context.Context, proof AdminProof, credential string) (RevokeOutcome, error) {
	var out RevokeOutcome
	if !proof.granted {
		return out, &ConsentError{Reason: ConsentAdminRequired, Credential: credential}
	}
	cfg, err := s.store.Load()
	if err != nil {
		return out, err
	}
	entry, _, _, err := s.consentTarget(cfg, credential)
	if err != nil {
		return out, err
	}
	if _, found, err := s.storedRole(ctx, credential, entry, config.RoleRefreshToken); err != nil {
		return out, err
	} else if !found {
		return out, &ConsentError{Reason: ConsentMissing, Credential: credential}
	}
	removed, err := s.DeleteSecret(ctx, cfg, SecretWrite{
		Name: credential, Entry: entry, Role: config.RoleRefreshToken, Approval: ApprovalNone,
	})
	out.ProcessWarning = removed.ProcessWarning
	if removed.Revocation != nil {
		out.Confirmed, out.Note, out.LogWarning =
			removed.Revocation.Confirmed, removed.Revocation.Note(), removed.Revocation.LogWarning
	}
	return out, err
}

// revokeStored revokes the stored refresh token of credential at the revocation endpoint of its provider,
// before DeleteSecret removes it. It returns nil when nothing is stored. A provider without consent
// endpoints, a credential without a client ID, and any refusal or failure leave the revocation unconfirmed;
// the removal goes ahead all the same. A refresh token that cannot be read stops the removal, so no consent
// is ever removed here unrevoked.
func (s *Service) revokeStored(ctx context.Context, credential string, entry config.Credential) (*Revocation, error) {
	var secrets provider.TokenSecrets
	var found bool
	var err error
	if secrets.RefreshToken, found, err = s.storedRole(ctx, credential, entry, config.RoleRefreshToken); err != nil || !found {
		return nil, err
	}
	// A role that cannot be read only leaves the revocation unconfirmed.
	secrets.ClientID, _, _ = s.storedRole(ctx, credential, entry, config.RoleClientID)
	secrets.ClientSecret, _, _ = s.storedRole(ctx, credential, entry, config.RoleClientSecret)
	revocation := &Revocation{Credential: credential}
	if oauth, ok := s.oauthOf(entry.Provider); ok && secrets.ClientID != "" {
		revocation.Confirmed = oauth.Revoke(ctx, secrets) == nil
	}
	return revocation, nil
}

// HoldsConsent reports whether credential holds a consent whose refresh token is revoked before it is
// removed: a keyring or vault credential of a provider that issues access tokens.
func HoldsConsent(cfg *config.Config, credential string) bool {
	entry, ok := cfg.Credentials[credential]
	return ok && entryHoldsConsent(cfg, entry)
}

// entryHoldsConsent is HoldsConsent for an entry the caller holds, such as the unsaved state of a form.
func entryHoldsConsent(cfg *config.Config, entry config.Credential) bool {
	if entry.Forward || entry.Type != config.CredentialTypeKeyring && entry.Type != config.CredentialTypeVault {
		return false
	}
	metadata, known := cfg.ProviderMetadata(entry.Provider)
	return entry.Provider != "" && known && metadata.Token != nil
}

// oauthOf returns the consent endpoints of a provider, false where it registered none or no valid ones.
func (s *Service) oauthOf(providerID string) (provider.OAuth, bool) {
	if s.consent == nil {
		return provider.OAuth{}, false
	}
	oauth, found := s.consent(providerID)
	return oauth, found && oauth.Validate() == nil
}

// consentTarget checks that credential can hold a consent and returns its entry and, with true, the consent
// endpoints of its provider; false means the provider registered none.
func (s *Service) consentTarget(cfg *config.Config, credential string) (config.Credential, provider.OAuth, bool, error) {
	entry, ok := cfg.Credentials[credential]
	if !ok {
		return config.Credential{}, provider.OAuth{}, false, &config.NotThereError{Kind: "credential", Name: credential}
	}
	if entry.Forward || entry.Type != config.CredentialTypeKeyring && entry.Type != config.CredentialTypeVault {
		return config.Credential{}, provider.OAuth{}, false, &ConsentError{Reason: ConsentNotStored, Credential: credential}
	}
	if !entryHoldsConsent(cfg, entry) {
		return config.Credential{}, provider.OAuth{}, false, &ConsentError{Reason: ConsentNoProvider, Credential: credential}
	}
	oauth, ok := s.oauthOf(entry.Provider)
	return entry, oauth, ok, nil
}

// CredentialDeleted is the outcome of DeleteCredential.
type CredentialDeleted struct {
	// Config is the configuration as saved.
	Config *config.Config
	// Revocation is what revoking the consent of the credential did, nil when it held none.
	Revocation *Revocation
	// ProcessWarning is the redacted warning of handing the removed refresh token on to a vault process.
	ProcessWarning string
	// Warning is the redacted warning of the connection log, "" for none.
	Warning string
}

// DeleteCredential removes the credential name from cfg, which was loaded at rev. A credential that holds a
// consent first has its refresh token revoked and removed through DeleteSecret, and is removed even when the
// provider did not confirm the revocation; when the refresh token cannot be removed, the credential stays.
// The other secrets of the credential are left where they are. A credential a connection still uses is
// refused before anything is sent. A file that changed since rev ends with a *config.ConflictError.
func (s *Service) DeleteCredential(ctx context.Context, cfg *config.Config, rev config.Revision, name string) (CredentialDeleted, error) {
	var out CredentialDeleted
	candidate := cfg.Clone()
	if err := candidate.DeleteCredential(name); err != nil {
		return out, err
	}
	if entry := cfg.Credentials[name]; entryHoldsConsent(cfg, entry) {
		removed, err := s.DeleteSecret(ctx, cfg, SecretWrite{
			Name: name, Entry: entry, Role: config.RoleRefreshToken, Approval: ApprovalNone,
		})
		out.Revocation, out.ProcessWarning = removed.Revocation, removed.ProcessWarning
		if err != nil && !errors.Is(err, secret.ErrNoEntry) {
			return out, err
		}
	}
	warning, err := s.SaveConfig(cfg, candidate, rev)
	if err != nil {
		return out, err
	}
	out.Config, out.Warning = candidate, warning
	return out, nil
}

// storedRole reads one role of a keyring or vault credential from where it is stored, never from the
// environment, which no consent writes. A vault is read as this process holds it, without asking for a
// passphrase. The value is registered before anything could print it.
func (s *Service) storedRole(ctx context.Context, name string, entry config.Credential, role string) (string, bool, error) {
	if s.secrets == nil {
		return "", false, errNoSecretStore
	}
	if entry.Type != config.CredentialTypeVault {
		value, state := s.secrets.StoreValue(ctx, name, role)
		if state == secret.StoreHolds || state == secret.StoreEmpty {
			s.register(value)
			return value, value != "", nil
		}
		_, err := s.previousKeyringSecret(name, role)
		if err == nil {
			err = secret.ErrUnavailable
		}
		return "", false, err
	}
	v := s.vault()
	if v == nil {
		return "", false, ErrNoVault
	}
	value, found, state, err := v.Get(name, role, nil)
	switch {
	case err != nil:
		return "", false, err
	case state == vault.StateLocked:
		return "", false, &secret.VaultLockedError{Credential: name, Role: role}
	}
	s.register(value)
	return value, found && value != "", nil
}

// consentScopes returns the connections that use credential and the union of the scopes their consent needs,
// each sorted and once, and whether one of them impersonates a service account: such a connection needs
// impersonateScope alone.
func consentScopes(cfg *config.Config, credential, impersonateScope string) (connections, scopes []string, impersonates bool) {
	add := func(scope string) {
		if scope != "" && !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	for name, conn := range cfg.Connections {
		if conn.Credential != credential {
			continue
		}
		connections = append(connections, name)
		if conn.ServiceAccount != "" {
			impersonates = true
			add(impersonateScope)
			continue
		}
		for _, scope := range cfg.TokenScopes(name) {
			add(scope)
		}
	}
	slices.Sort(connections)
	slices.Sort(scopes)
	return connections, scopes, impersonates
}

// recordConsent logs operation for every connection and returns the redacted warning of the first failure.
func (s *Service) recordConsent(cfg *config.Config, connections []string, operation, result string) string {
	if s.store == nil {
		return ""
	}
	recorder := connlog.NewRecorder(s.surface, s.store.Path(), s.vault(), s.processOK)
	for _, name := range connections {
		if err := recorder.RecordConsent(operation, name, cfg.Connections[name].ID, result); err != nil {
			return s.redact("warning: " + err.Error())
		}
	}
	return ""
}

// register adds values to the redaction of the service before anything could print them.
func (s *Service) register(values ...string) {
	if s.redactor != nil {
		s.redactor.Add(values...)
	}
}

// validCode reports whether code is an authorization code that fits a form body: printable ASCII, bounded.
func validCode(code string) bool {
	if code == "" || len(code) > maxAnswerBytes {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 0x21 || code[i] > 0x7e {
			return false
		}
	}
	return true
}
