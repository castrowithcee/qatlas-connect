package provider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth is what a provider whose credential takes a person's consent gives the core: the fixed endpoints of
// its authorization server and what it adds to them. Every endpoint is a fixed https address of the
// provider; none comes from configuration, a file or an answer. A parameter such as a tenant is a checked
// segment the provider puts into these addresses itself.
type OAuth struct {
	// AuthorizeURL is where a person gives the consent, TokenURL where a code or a refresh token is redeemed,
	// RevokeURL where a refresh token is revoked (RFC 7009).
	AuthorizeURL, TokenURL, RevokeURL string
	// Params are added to the authorization link, such as the one that asks for lasting access. They never
	// replace a parameter the core sets.
	Params map[string]string
	// NarrowRefresh asks for only the scopes of the connection when a refresh token is redeemed. Only a
	// provider that allows asking for fewer scopes than the consent granted sets it.
	NarrowRefresh bool
	// Transport carries the requests to the token and revocation endpoints; nil is Go's default transport.
	Transport http.RoundTripper
}

const (
	consentOp = "consent"
	revokeOp  = "revoke consent"
	// oauthTimeout bounds one request to a token or revocation endpoint.
	oauthTimeout = 30 * time.Second
	// revokeSubject names the revocation endpoint in a message.
	revokeSubject = "the revocation endpoint"
)

// reservedAuthorizeParams are the parameters of the authorization link the core sets itself.
var reservedAuthorizeParams = []string{
	"response_type", "client_id", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method",
}

// Validate checks that every endpoint is an absolute https address without user information, query or
// fragment, and that no parameter replaces one the core sets. A provider registers only a valid OAuth.
func (o OAuth) Validate() error {
	for _, endpoint := range []struct{ name, value string }{
		{"authorization", o.AuthorizeURL}, {"token", o.TokenURL}, {"revocation", o.RevokeURL},
	} {
		u, err := url.Parse(endpoint.value)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" ||
			u.Fragment != "" || u.Opaque != "" {
			return fmt.Errorf("the %s endpoint must be a fixed https address without query or fragment", endpoint.name)
		}
	}
	for name, value := range o.Params {
		for _, reserved := range reservedAuthorizeParams {
			if strings.EqualFold(name, reserved) {
				return fmt.Errorf("the authorization parameter %q is set by the core", name)
			}
		}
		if name == "" || value == "" {
			return errors.New("an authorization parameter needs a name and a value")
		}
	}
	return nil
}

// AuthorizationURL is the link a person opens to give the consent: an authorization code request with the
// PKCE challenge (S256) and state (RFC 6749, section 4.1.1; RFC 7636). It carries no secret.
func (o OAuth) AuthorizationURL(clientID, redirectURI string, scopes []string, state, challenge string) string {
	query := url.Values{}
	for name, value := range o.Params {
		query.Set(name, value)
	}
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", strings.Join(scopes, " "))
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	return o.AuthorizeURL + "?" + query.Encode()
}

// Exchange redeems an authorization code with the PKCE verifier and exactly the redirect address of the
// link (RFC 6749, section 4.1.3). The client secret is sent only when the credential holds one. A refusal
// keeps only its status and error code, as every answer of a token endpoint does.
func (o OAuth) Exchange(ctx context.Context, secrets TokenSecrets, code, verifier, redirectURI string) (Token, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	return o.redeem(ctx, consentOp, secrets, form)
}

// RefreshGrant is the grant of the consent procedure for TokenOptions.Grants: it redeems the refresh token
// of the credential at the fixed token endpoint (RFC 6749, section 6). invalid_grant becomes the class of an
// expired or revoked consent, which the token source reports without a retry and without deleting anything.
func (o OAuth) RefreshGrant() TokenGrant {
	return func(ctx context.Context, req TokenRequest) (Token, error) {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {req.Secrets.RefreshToken}}
		if o.NarrowRefresh && len(req.Scopes) > 0 {
			form.Set("scope", strings.Join(req.Scopes, " "))
		}
		return o.redeem(ctx, tokenOp, req.Secrets, form)
	}
}

func (o OAuth) redeem(ctx context.Context, op string, secrets TokenSecrets, form url.Values) (Token, error) {
	form.Set("client_id", secrets.ClientID)
	if secrets.ClientSecret != "" {
		form.Set("client_secret", secrets.ClientSecret)
	}
	resp, err := o.post(ctx, op, tokenSubject, o.TokenURL, form)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	return ReadTokenAnswer(op, resp, time.Now())
}

// Revoke asks the revocation endpoint to revoke the refresh token of the credential (RFC 7009). A nil error
// is a confirmed revocation; any error leaves open whether the consent still holds at the provider.
func (o OAuth) Revoke(ctx context.Context, secrets TokenSecrets) error {
	form := url.Values{
		"token":           {secrets.RefreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {secrets.ClientID},
	}
	if secrets.ClientSecret != "" {
		form.Set("client_secret", secrets.ClientSecret)
	}
	resp, err := o.post(ctx, revokeOp, revokeSubject, o.RevokeURL, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return TokenEndpointError(revokeOp, resp.StatusCode, ReadErrorBody(resp.Body))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, ErrorBodyLimit))
	return nil
}

// post sends form to one fixed endpoint. Every value travels in the body, never in the address.
func (o OAuth) post(ctx context.Context, op, subject, endpoint string, form url.Values) (*http.Response, error) {
	if err := o.Validate(); err != nil {
		return nil, Fail(op, "the provider declares no usable endpoints for a consent")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, Fail(op, "the request could not be built")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := NoRedirectClient(oauthTimeout, o.Transport).Do(req)
	if err != nil {
		return nil, Transport(op, subject, err)
	}
	return resp, nil
}

// NewPKCE returns a fresh PKCE verifier and its S256 challenge (RFC 7636, section 4): 32 random bytes as
// base64url without padding, and the base64url SHA-256 of that text.
func NewPKCE() (verifier, challenge string, err error) {
	verifier, err = randomText()
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// NewState returns a fresh state value: 32 random bytes as base64url without padding.
func NewState() (string, error) { return randomText() }

func randomText() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
