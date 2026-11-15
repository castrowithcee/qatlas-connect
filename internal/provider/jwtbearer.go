package provider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

const (
	jwtBearerGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	serviceAccountType = "service_account"
	// assertionLifetime stays below the hour an endpoint accepts at most, so clock skew cannot push the
	// assertion over the limit.
	assertionLifetime = 55 * time.Minute
	minRSABits        = 2048
	maxKeyFile        = 64 << 10
)

// ServiceAccountOptions configure the JWT-bearer grant of a provider.
type ServiceAccountOptions struct {
	// Endpoint is the fixed token endpoint of the provider. The key file must name exactly this address, and
	// the exchange always goes there.
	Endpoint string
	// Client sends the exchange; build it with NoRedirectClient.
	Client *http.Client
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// ServiceAccountKey is the part of a service account key file the grant uses.
type ServiceAccountKey struct {
	Email, KeyID string
	private      *rsa.PrivateKey
	// pem is the decoded private key text, which a message must never show.
	pem string
}

// ParseServiceAccountKey checks a key file against the fixed token endpoint of the provider. The file is
// untrusted input: an error names the field that is wrong, never a value.
func ParseServiceAccountKey(raw, endpoint string) (ServiceAccountKey, error) {
	reject := func(format string, args ...any) (ServiceAccountKey, error) {
		return ServiceAccountKey{}, &Error{Class: ClassCredentialUnusable, Op: tokenOp,
			Message: "the service account key is unusable: " + fmt.Sprintf(format, args...)}
	}
	if len(raw) > maxKeyFile {
		return reject("the file is too large")
	}
	var fields map[string]any
	if json.Unmarshal([]byte(raw), &fields) != nil || fields == nil {
		return reject("the file is not a JSON object")
	}
	text := func(name string) (string, bool) {
		value, _ := fields[name].(string)
		return value, value != ""
	}
	for _, name := range []string{"type", "client_email", "private_key", "private_key_id", "token_uri"} {
		if _, ok := text(name); !ok {
			return reject("field %q is missing or not a string", name)
		}
	}
	if kind, _ := text("type"); kind != serviceAccountType {
		return reject("field %q is not %q", "type", serviceAccountType)
	}
	if uri, _ := text("token_uri"); uri != endpoint {
		return reject("field %q is not the token endpoint of this provider", "token_uri")
	}
	email, _ := text("client_email")
	keyID, _ := text("private_key_id")
	pemText, _ := text("private_key")
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return reject("field %q holds no PEM block", "private_key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	private, ok := parsed.(*rsa.PrivateKey)
	if err != nil || !ok {
		return reject("field %q is not a PKCS#8 RSA key", "private_key")
	}
	if private.N.BitLen() < minRSABits {
		return reject("field %q is a key shorter than %d bits", "private_key", minRSABits)
	}
	return ServiceAccountKey{Email: email, KeyID: keyID, private: private, pem: pemText}, nil
}

// Assertion signs the JWT of the bearer grant (RFC 7523) for scopes with RS256. It names the key by its
// ID, the service account as issuer and the token endpoint as audience, and expires within the hour.
func (k ServiceAccountKey) Assertion(endpoint string, scopes []string, now time.Time) (string, error) {
	segment := func(v any) (string, error) {
		body, err := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(body), err
	}
	header, err := segment(map[string]string{"alg": "RS256", "typ": "JWT", "kid": k.KeyID})
	if err != nil {
		return "", err
	}
	claims, err := segment(map[string]any{"iss": k.Email, "scope": strings.Join(scopes, " "), "aud": endpoint,
		"iat": now.Unix(), "exp": now.Add(assertionLifetime).Unix()})
	if err != nil {
		return "", err
	}
	signing := header + "." + claims
	digest := sha256.Sum256([]byte(signing))
	signature, err := rsa.SignPKCS1v15(rand.Reader, k.private, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// ServiceAccountGrant returns the grant of config.TokenServiceAccount for a provider with a fixed token
// endpoint: it signs an assertion with the key of the credential and exchanges it there for an access
// token. The key, the assertion and the token reach the redactor of the call before anything is sent, no
// refresh token is kept, and no text of the endpoint reaches an error.
func ServiceAccountGrant(opts ServiceAccountOptions) TokenGrant {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return func(ctx context.Context, req TokenRequest) (Token, error) {
		register := func(values ...string) {
			if req.Register != nil {
				req.Register(values...)
			}
		}
		raw := req.Secrets.ServiceAccountKey
		if req.Procedure != config.TokenServiceAccount || raw == "" {
			return Token{}, &Error{Class: ClassCredentialUnusable, Op: tokenOp,
				Message: "the credential holds no service account key"}
		}
		register(raw)
		key, err := ParseServiceAccountKey(raw, opts.Endpoint)
		if err != nil {
			return Token{}, err
		}
		register(key.pem)
		if len(req.Scopes) == 0 {
			return Token{}, Fail(tokenOp, "the connection needs no scope, so no access token is requested")
		}
		start := now()
		assertion, err := key.Assertion(opts.Endpoint, req.Scopes, start)
		if err != nil {
			return Token{}, Fail(tokenOp, "the assertion for the token endpoint could not be signed")
		}
		register(assertion, assertion[strings.LastIndexByte(assertion, '.')+1:])
		form := url.Values{"grant_type": {jwtBearerGrantType}, "assertion": {assertion}}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.Endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return Token{}, Fail(tokenOp, "the request to the token endpoint could not be built")
		}
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		httpReq.Header.Set("Accept", "application/json")
		resp, err := opts.Client.Do(httpReq)
		if err != nil {
			return Token{}, Transport(tokenOp, tokenSubject, err)
		}
		defer resp.Body.Close()
		token, err := ReadTokenAnswer(tokenOp, resp, now())
		if err != nil {
			return Token{}, err
		}
		// A service account has no consent to renew, so an answer's refresh token is never kept.
		register(token.Refresh)
		token.Refresh = ""
		return token, nil
	}
}
