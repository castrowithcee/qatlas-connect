package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// ServiceAccountSegment is the placeholder of OAuth.ImpersonateURL that stands for the service account.
const ServiceAccountSegment = "{service_account}"

const (
	impersonateSubject = "the impersonation endpoint"
	// impersonationLifetime is the longest lifetime an impersonated token is asked for and kept.
	impersonationLifetime = time.Hour
)

// validateImpersonation checks the impersonation endpoint and its scope, which a provider sets together or
// not at all. The placeholder must stand in the path, so the service account can never reach the host.
func (o OAuth) validateImpersonation() error {
	if o.ImpersonateURL == "" && o.ImpersonateScope == "" {
		return nil
	}
	if o.ImpersonateScope == "" || strings.ContainsAny(o.ImpersonateScope, " \t\r\n") {
		return errors.New("the impersonation endpoint needs the one scope its request requires")
	}
	if strings.Count(o.ImpersonateURL, ServiceAccountSegment) != 1 {
		return errors.New("the impersonation endpoint must hold the service account placeholder exactly once")
	}
	const marker = "service-account-placeholder"
	u, err := url.Parse(strings.Replace(o.ImpersonateURL, ServiceAccountSegment, marker, 1))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || u.Opaque != "" || !strings.Contains(u.Path, marker) || strings.Contains(u.Host, marker) {
		return errors.New("the impersonation endpoint must be a fixed https address without query or fragment, " +
			"with the service account placeholder in its path")
	}
	return nil
}

// impersonationEndpoint is the fixed impersonation address with account put in as one escaped segment.
// account comes from the connection alone and must pass config.ValidServiceAccount.
func (o OAuth) impersonationEndpoint(account string) (string, bool) {
	if o.ImpersonateURL == "" || o.validateImpersonation() != nil || !config.ValidServiceAccount(account) {
		return "", false
	}
	return strings.Replace(o.ImpersonateURL, ServiceAccountSegment, url.PathEscape(account), 1), true
}

// ImpersonationGrant returns the grant of config.TokenImpersonation. It redeems the refresh token of the
// credential through RefreshGrant, asking for nothing but ImpersonateScope, for the token of the person who
// gave the consent, and then asks the fixed impersonation endpoint for an access token of the service account
// the connection names, with the scopes of the connection, for at most an hour. Only direct impersonation is
// requested, never a chain over further service accounts. Every token reaches the redactor of the call
// before it is used, a refresh token the first answer rotated is stored before the token of the consent is
// sent on, and no text of either endpoint reaches an error.
func (o OAuth) ImpersonationGrant() TokenGrant {
	return func(ctx context.Context, req TokenRequest) (Token, error) {
		register := func(values ...string) {
			if req.Register != nil {
				req.Register(values...)
			}
		}
		if req.Procedure != config.TokenImpersonation || req.ServiceAccount == "" {
			return Token{}, &Error{Class: ClassCredentialUnusable, Op: tokenOp,
				Message: "the connection names no service account to impersonate"}
		}
		endpoint, ok := o.impersonationEndpoint(req.ServiceAccount)
		if !ok {
			return Token{}, Fail(tokenOp, "the impersonation endpoint or the service account of the connection is unusable")
		}
		if len(req.Scopes) == 0 {
			return Token{}, Fail(tokenOp, "the connection needs no scope, so no access token is requested")
		}

		consent := req
		consent.Scopes = []string{o.ImpersonateScope}
		user, err := o.RefreshGrant()(ctx, consent)
		register(user.Access, user.Refresh)
		if err != nil {
			return Token{}, err
		}
		refresh := user.Refresh
		if refresh != "" && refresh != req.Secrets.RefreshToken && req.Rotate != nil {
			if refresh, err = req.Rotate(ctx, refresh); err != nil {
				return Token{}, err
			}
		}
		if user.Scopes != nil && !slices.Contains(user.Scopes, o.ImpersonateScope) {
			return Token{Refresh: refresh}, &Error{Class: ClassScopeMissing, Op: tokenOp, Message: "the consent " +
				"of the credential lacks the scope the impersonation needs; nothing was sent to " + impersonateSubject}
		}

		start := time.Now()
		body, err := json.Marshal(struct {
			Scope    []string `json:"scope"`
			Lifetime string   `json:"lifetime"`
		}{req.Scopes, fmt.Sprintf("%ds", int64(impersonationLifetime/time.Second))})
		if err != nil {
			return Token{Refresh: refresh}, Fail(tokenOp, "the request to "+impersonateSubject+" could not be built")
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return Token{Refresh: refresh}, Fail(tokenOp, "the request to "+impersonateSubject+" could not be built")
		}
		httpReq.Header.Set("Authorization", "Bearer "+user.Access)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		resp, err := NoRedirectClient(oauthTimeout, o.Transport).Do(httpReq)
		if err != nil {
			return Token{Refresh: refresh}, Transport(tokenOp, impersonateSubject, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			// The body stays unread beyond its bound: only the status decides, and none of its text is kept.
			_ = ReadErrorBody(resp.Body)
			return Token{Refresh: refresh}, impersonationError(resp.StatusCode)
		}
		var answer struct {
			AccessToken string `json:"accessToken"`
			ExpireTime  string `json:"expireTime"`
		}
		if err := ReadJSON(tokenOp, impersonateSubject, resp.Body, tokenAnswerLimit, &answer); err != nil {
			return Token{Refresh: refresh}, err
		}
		register(answer.AccessToken)
		if !validTokenValue(answer.AccessToken) {
			return Token{Refresh: refresh}, InvalidResponse(tokenOp, impersonateSubject+" returned no usable access token")
		}
		token := Token{Access: answer.AccessToken, Refresh: refresh}
		// An expiry that does not read, or lies in the past, leaves the token to the call that obtained it.
		if expiry, err := time.Parse(time.RFC3339, answer.ExpireTime); err == nil && expiry.After(start) {
			token.Expiry = expiry
			if limit := start.Add(impersonationLifetime); expiry.After(limit) {
				token.Expiry = limit
			}
		}
		return token, nil
	}
}

// impersonationError classifies a refusal of the impersonation endpoint by its status alone and keeps nothing
// but that status as the original.
func impersonationError(status int) *Error {
	failure := &Error{Op: tokenOp, Original: &Original{Status: status}}
	switch status {
	case http.StatusForbidden:
		failure.Class, failure.Message = ClassImpersonationDenied, impersonateSubject+" refused to create tokens "+
			"of the service account the connection names for the account that gave the consent; nothing was sent "+
			"to the provider's API"
	case http.StatusNotFound:
		failure.Class, failure.Message = ClassServiceAccountUnknown, impersonateSubject+" does not know the "+
			"service account the connection names; nothing was sent to the provider's API"
	case http.StatusUnauthorized:
		failure.Class, failure.Message = ClassAuth, impersonateSubject+" refused the token of the consent"
	case http.StatusTooManyRequests:
		failure.Class, failure.Message = ClassRateLimited, impersonateSubject+" is rate-limiting requests; wait and try again"
	default:
		failure.Class, failure.Message = ClassProviderError, impersonateSubject+" refused the request for an access token"
	}
	return failure
}
