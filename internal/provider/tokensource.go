package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// tokenReserve is how long before its expiry a cached token is renewed. It outlasts one invoke, so a token
// never expires during the call that took it.
const tokenReserve = 2 * time.Minute

const tokenOp = "access token"

// TokenCache holds access tokens in memory only, one per form of a connection: credential, procedure,
// scopes and service account. Calls of one process that need a new token at the same time share one grant.
type TokenCache struct {
	mu      sync.Mutex
	entries map[tokenKey]*tokenEntry
	now     func() time.Time
}

// NewTokenCache returns an empty cache that judges expiry by now.
func NewTokenCache(now func() time.Time) *TokenCache {
	return &TokenCache{entries: map[tokenKey]*tokenEntry{}, now: now}
}

var processTokens = NewTokenCache(time.Now)

// tokenKey is the form of a connection a token belongs to. The client ID and the key bind it to the
// credential's content as well, so a credential given another client or key never reuses a token.
type tokenKey struct {
	provider, credential, credentialID string
	clientID, serviceAccountKey        string
	procedure                          config.TokenProcedure
	scopes, serviceAccount             string
}

type tokenEntry struct {
	token Token
	// basis is the refresh token the token was obtained with; a credential that now stores another one,
	// which is neither it nor the one the answer rotated to, was given a new consent.
	basis  string
	flight *tokenFlight
}

// tokenFlight is one grant under way that other calls wait for.
type tokenFlight struct {
	done  chan struct{}
	token Token
	err   error
	// ended is set when the call that ran the grant ended first, so its error is no answer for the others.
	ended bool
}

// get returns a token of key that is not stale and stays valid beyond the reserve, obtaining one with fetch
// when there is none. A call that asks while another obtains one waits for it and shares its outcome.
func (c *TokenCache) get(ctx context.Context, key tokenKey, basis, stale string,
	fetch func(context.Context) (Token, error)) (Token, error) {
	for {
		c.mu.Lock()
		entry := c.entries[key]
		if entry == nil {
			entry = &tokenEntry{}
			c.entries[key] = entry
		}
		if flight := entry.flight; flight != nil {
			c.mu.Unlock()
			select {
			case <-flight.done:
			case <-ctx.Done():
				return Token{}, ctx.Err()
			}
			if flight.ended && ctx.Err() == nil {
				continue
			}
			return flight.token, flight.err
		}
		if c.fresh(entry, basis, stale) {
			token := entry.token
			c.mu.Unlock()
			return token, nil
		}
		flight := &tokenFlight{done: make(chan struct{})}
		entry.flight = flight
		c.mu.Unlock()

		token, err := fetch(ctx)

		c.mu.Lock()
		entry.flight = nil
		entry.token, entry.basis = Token{}, ""
		if err == nil && !token.Expiry.IsZero() {
			entry.token, entry.basis = token, basis
		}
		flight.token, flight.err, flight.ended = token, err, err != nil && ctx.Err() != nil
		c.mu.Unlock()
		close(flight.done)
		return token, err
	}
}

func (c *TokenCache) fresh(entry *tokenEntry, basis, stale string) bool {
	token := entry.token
	return token.Access != "" && token.Access != stale &&
		(basis == entry.basis || token.Refresh != "" && basis == token.Refresh) &&
		c.now().Add(tokenReserve).Before(token.Expiry)
}

// drop forgets the token of key if it is still access.
func (c *TokenCache) drop(key tokenKey, access string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[key]; entry != nil && entry.token.Access == access {
		entry.token, entry.basis = Token{}, ""
	}
}

// TokenOptions are what a provider gives OpenTokenSource.
type TokenOptions struct {
	// Token is the provider's ProviderMetadata.Token.
	Token config.TokenMetadata
	// Grants holds the grant of every procedure the provider offers; a credential whose procedure has none
	// is unusable.
	Grants map[config.TokenProcedure]TokenGrant
	// SaveRefresh stores a rotated refresh token in place of old before the access token it came with is
	// used. Without it a rotation ends the call, since a rotated refresh token is never dropped silently.
	SaveRefresh func(ctx context.Context, old, rotated string) error
	// Cache holds the tokens; nil is the cache of the process.
	Cache *TokenCache
}

// TokenSource hands out the access tokens of one connection. It is opened per call: opening resolves the
// credential anew, so a vault keeps deciding whether the connection may use it, while the token itself
// comes from the cache whenever one is still valid.
type TokenSource struct {
	cache      *TokenCache
	key        tokenKey
	request    TokenRequest
	grant      TokenGrant
	scopes     map[string][]string
	save       func(ctx context.Context, old, rotated string) error
	credential string
	red        *redact.Redactor

	// mu guards the refresh token of request, which a stored rotation replaces for every later grant.
	mu sync.Mutex
}

// OpenTokenSource resolves the token roles of the connection's credential and picks its procedure. It ends
// before any request when the roles fit no procedure the provider offers, and when a consent is still
// missing; the errors name the credential, never a value.
func OpenTokenSource(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, opts TokenOptions) (*TokenSource, error) {
	var values TokenSecrets
	for _, role := range []struct {
		name  string
		value *string
	}{
		{config.RoleClientID, &values.ClientID},
		{config.RoleClientSecret, &values.ClientSecret},
		{config.RoleRefreshToken, &values.RefreshToken},
		{config.RoleServiceAccountKey, &values.ServiceAccountKey},
	} {
		value, found, err := secrets.ResolveOptional(ctx, resolved.Credential, resolved.Secrets, role.name)
		if err != nil {
			return nil, err
		}
		if found {
			*role.value = value.Secret
		}
	}
	unusable := func(format string) error {
		return &Error{Class: ClassCredentialUnusable, Op: tokenOp, Message: fmt.Sprintf(format, resolved.Credential)}
	}
	clientID, key := values.ClientID != "", values.ServiceAccountKey != ""
	procedure, ok := config.TokenProcedureOf(clientID, key, "")
	switch {
	case !ok && clientID:
		return nil, unusable("credential %q holds both a client ID and a service account key; remove one of them")
	case !ok:
		return nil, unusable("credential %q holds neither a client ID nor a service account key")
	case opts.Grants[procedure] == nil:
		return nil, unusable("credential %q holds the roles of a procedure this provider does not offer")
	case procedure != config.TokenServiceAccount && values.RefreshToken == "":
		return nil, &Error{Class: ClassConsentRequired, Op: tokenOp, Message: fmt.Sprintf(
			"credential %q of connection %q has no consent yet; nothing was sent", resolved.Credential, resolved.Name)}
	}
	cache := opts.Cache
	if cache == nil {
		cache = processTokens
	}
	scopes := append([]string(nil), resolved.TokenScopes...)
	return &TokenSource{
		cache: cache,
		key: tokenKey{
			provider: resolved.Provider, credential: resolved.Credential, credentialID: resolved.Secrets.ID,
			clientID: values.ClientID, serviceAccountKey: values.ServiceAccountKey,
			procedure: procedure, scopes: strings.Join(scopes, " "),
		},
		request:    TokenRequest{Procedure: procedure, Secrets: values, Scopes: scopes},
		grant:      opts.Grants[procedure],
		scopes:     opts.Token.Scopes,
		save:       opts.SaveRefresh,
		credential: resolved.Credential,
		red:        red,
	}, nil
}

// Read sends a request of a reading tool, which build creates for an access token. On 401 it renews the
// token once and sends the request once more; whatever answers then is returned, so it never loops. 403
// renews nothing. The caller classifies the answer and closes its body.
func (s *TokenSource) Read(ctx context.Context, client *http.Client, tool string,
	build func(accessToken string) (*http.Request, error)) (*http.Response, error) {
	return s.do(ctx, client, tool, false, build)
}

// Mutate sends the one request of a changing tool. It is never repeated: on 401 the cached token is
// dropped, so the next call renews it, and the answer is returned for the caller to classify.
func (s *TokenSource) Mutate(ctx context.Context, client *http.Client, tool string,
	build func(accessToken string) (*http.Request, error)) (*http.Response, error) {
	return s.do(ctx, client, tool, true, build)
}

func (s *TokenSource) do(ctx context.Context, client *http.Client, tool string, mutation bool,
	build func(string) (*http.Request, error)) (*http.Response, error) {
	needed, ok := s.scopes[tool]
	if !ok {
		return nil, Fail(tool, "the tool declares no scopes, so no access token is sent for it")
	}
	token, err := s.tokenFor(ctx, "", tool, needed)
	if err != nil {
		return nil, err
	}
	resp, err := sendWithToken(client, build, token.Access)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	if mutation {
		s.cache.drop(s.key, token.Access)
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, ErrorBodyLimit))
	_ = resp.Body.Close()
	token, err = s.tokenFor(ctx, token.Access, tool, needed)
	if err != nil {
		return nil, err
	}
	return sendWithToken(client, build, token.Access)
}

// Check obtains an access token the way a call does and reports a scope of the connection it lacks.
func (s *TokenSource) Check(ctx context.Context) error {
	token, err := s.token(ctx, "")
	if err != nil {
		return err
	}
	return s.scopeError(tokenOp, s.missing(token, s.request.Scopes))
}

// TestTokenConnection is the connection test of a provider whose credential issues access tokens. It tells
// a missing consent, an expired or revoked one, a missing scope and a usable credential apart, without a
// token value. probe, when set, then makes the provider's smallest read with the source. A provider failure
// is the class with a nil error, as for every connection test.
func TestTokenConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, opts TokenOptions, probe func(context.Context, *TokenSource) (Class, error)) (Class, error) {
	source, err := OpenTokenSource(ctx, resolved, secrets, red, opts)
	if err == nil {
		err = source.Check(ctx)
	}
	if err == nil && probe != nil {
		var class Class
		class, err = probe(ctx, source)
		if err == nil {
			return class, nil
		}
	}
	var failed *Error
	switch {
	case err == nil:
		return ClassOK, nil
	case errors.As(err, &failed):
		return failed.Class, nil
	}
	return "", err
}

func (s *TokenSource) tokenFor(ctx context.Context, stale, tool string, needed []string) (Token, error) {
	token, err := s.token(ctx, stale)
	if err != nil {
		return Token{}, err
	}
	if err := s.scopeError(tool, s.missing(token, needed)); err != nil {
		return Token{}, err
	}
	return token, nil
}

func (s *TokenSource) token(ctx context.Context, stale string) (Token, error) {
	token, err := s.cache.get(ctx, s.key, s.current().Secrets.RefreshToken, stale, s.fetch)
	if err != nil {
		return Token{}, err
	}
	// A cached token may come from a call of another redactor, such as an earlier request of a server.
	s.register(token.Access)
	return token, nil
}

// fetch runs the grant. Every value it returns is registered before anything else could print it, and a
// rotated refresh token is stored before the access token that came with it is handed out.
func (s *TokenSource) fetch(ctx context.Context) (Token, error) {
	request := s.current()
	request.Register = s.register
	token, err := s.grant(ctx, request)
	s.register(token.Access, token.Refresh)
	if err != nil {
		return Token{}, s.grantError(err)
	}
	if !validTokenValue(token.Access) {
		return Token{}, InvalidResponse(tokenOp, tokenSubject+" returned no usable bearer token")
	}
	old := request.Secrets.RefreshToken
	if request.Procedure != config.TokenServiceAccount && token.Refresh != "" && token.Refresh != old {
		if s.save == nil {
			return Token{}, Fail(tokenOp, fmt.Sprintf("%s rotated the refresh token of credential %q, which "+
				"cannot be stored here, so the new access token was not used", tokenSubject, s.credential))
		}
		if err := s.save(ctx, old, token.Refresh); err != nil {
			return Token{}, err
		}
		s.mu.Lock()
		s.request.Secrets.RefreshToken = token.Refresh
		s.mu.Unlock()
	}
	return token, nil
}

func (s *TokenSource) current() TokenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.request
}

// grantError names the credential in a refused grant. A service account has no consent, so a grant its
// endpoint refuses is a rejected key.
func (s *TokenSource) grantError(err error) error {
	var failed *Error
	if !errors.As(err, &failed) || failed.Class != ClassConsentExpired {
		return err
	}
	named := *failed
	if s.request.Procedure == config.TokenServiceAccount {
		named.Class = ClassAuth
		named.Message = fmt.Sprintf("%s refused the service account key of credential %q", tokenSubject, s.credential)
		return &named
	}
	named.Message = fmt.Sprintf("%s no longer accepts the consent of credential %q: it expired or was revoked",
		tokenSubject, s.credential)
	return &named
}

func (s *TokenSource) missing(token Token, needed []string) []string {
	granted := token.Scopes
	if granted == nil {
		granted = s.request.Scopes
	}
	var missing []string
	for _, scope := range needed {
		if !slices.Contains(granted, scope) {
			missing = append(missing, scope)
		}
	}
	return missing
}

func (s *TokenSource) scopeError(op string, missing []string) error {
	if len(missing) == 0 {
		return nil
	}
	return &Error{Class: ClassScopeMissing, Op: op, Message: fmt.Sprintf(
		"the token of credential %q lacks the scope %s; nothing was sent", s.credential, strings.Join(missing, ", "))}
}

func (s *TokenSource) register(values ...string) {
	if s.red != nil {
		s.red.Add(values...)
	}
}

func sendWithToken(client *http.Client, build func(string) (*http.Request, error), access string) (*http.Response, error) {
	req, err := build(access)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
