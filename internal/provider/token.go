package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// Token is one access token a token endpoint issued.
type Token struct {
	Access string
	// Expiry is when Access stops working, zero when the answer did not say; such a token serves only the
	// call that obtained it.
	Expiry time.Time
	// Scopes are the scopes the answer granted, nil when it named none, which grants the requested ones.
	Scopes []string
	// Refresh is the refresh token the answer carried, empty for none.
	Refresh string
}

// TokenSecrets are the values of the token roles of a credential; an empty field is a role without a value.
type TokenSecrets struct {
	ClientID, ClientSecret, RefreshToken, ServiceAccountKey string
}

// TokenRequest is what a grant obtains an access token for: the procedure of the credential, its secrets,
// the scopes of the connection, and the service account the connection names, empty for none.
type TokenRequest struct {
	Procedure      config.TokenProcedure
	Secrets        TokenSecrets
	Scopes         []string
	ServiceAccount string
}

// TokenGrant obtains an access token for one procedure from a fixed endpoint of its provider. A failed
// answer becomes the error ReadTokenAnswer gives, so no text of the endpoint reaches a message.
type TokenGrant func(ctx context.Context, req TokenRequest) (Token, error)

const (
	tokenSubject = "the token endpoint"
	// tokenAnswerLimit bounds the answer of a token endpoint that is read.
	tokenAnswerLimit = 64 << 10
	// maxTokenLifetime caps the lifetime an answer claims, so a token is checked again at least daily.
	maxTokenLifetime = 24 * 60 * 60
	// maxTokenValue bounds an access or refresh token; a signed token with many claims stays well below it.
	maxTokenValue = 16 << 10
	// maxErrorCode bounds the error code of a refusal that is kept.
	maxErrorCode = 64
)

// ReadTokenAnswer reads the answer of a token endpoint to a token request (RFC 6749, section 5) and leaves
// the body to the caller to close. A success becomes a Token whose expiry counts from now. A refusal becomes
// the error of TokenEndpointError. The answer is untrusted: none of its text reaches a message.
func ReadTokenAnswer(op string, resp *http.Response, now time.Time) (Token, error) {
	if resp.StatusCode != http.StatusOK {
		return Token{}, TokenEndpointError(op, resp.StatusCode, ReadErrorBody(resp.Body))
	}
	var answer struct {
		AccessToken  string      `json:"access_token"`
		TokenType    string      `json:"token_type"`
		ExpiresIn    json.Number `json:"expires_in"`
		Scope        string      `json:"scope"`
		RefreshToken string      `json:"refresh_token"`
	}
	if err := ReadJSON(op, tokenSubject, resp.Body, tokenAnswerLimit, &answer); err != nil {
		return Token{}, err
	}
	if !strings.EqualFold(answer.TokenType, "bearer") || !validTokenValue(answer.AccessToken) ||
		answer.RefreshToken != "" && !validTokenValue(answer.RefreshToken) {
		return Token{}, InvalidResponse(op, tokenSubject+" returned no usable bearer token")
	}
	token := Token{Access: answer.AccessToken, Refresh: answer.RefreshToken}
	if seconds, err := answer.ExpiresIn.Int64(); err == nil && seconds > 0 {
		token.Expiry = now.Add(time.Duration(min(seconds, maxTokenLifetime)) * time.Second)
	}
	if scopes := strings.Fields(answer.Scope); len(scopes) > 0 {
		token.Scopes = scopes
	}
	return token, nil
}

// TokenEndpointError classifies a refusal of a token endpoint by its status and the error code of its body
// (RFC 6749, section 5.2). Unlike other provider answers it keeps only the status and that code as the
// original: a token endpoint may echo parts of the request, a secret among them, in its error text.
func TokenEndpointError(op string, status int, body []byte) *Error {
	var answer struct {
		Error string `json:"error"`
	}
	code := ""
	if json.Unmarshal(body, &answer) == nil && validErrorCode(answer.Error) {
		code = answer.Error
	}
	failure := &Error{Op: op, Original: &Original{Status: status}}
	if code != "" {
		failure.Original.Entries = []OriginalEntry{{Code: code}}
	}
	switch {
	case code == "invalid_grant":
		failure.Class, failure.Message = ClassConsentExpired, tokenSubject+" no longer accepts the grant of the credential"
	case code == "invalid_scope":
		failure.Class, failure.Message = ClassScopeMissing, tokenSubject+" refused a scope this connection needs"
	case code == "invalid_client", code == "unauthorized_client", status == http.StatusUnauthorized,
		status == http.StatusForbidden:
		failure.Class, failure.Message = ClassAuth, tokenSubject+" refused the client of the credential"
	case status == http.StatusTooManyRequests:
		failure.Class, failure.Message = ClassRateLimited, tokenSubject+" is rate-limiting requests; wait and try again"
	default:
		failure.Class, failure.Message = ClassProviderError, tokenSubject+" refused the request for an access token"
	}
	return failure
}

// validTokenValue reports whether value can travel in an Authorization header: printable ASCII without
// spaces, of a bounded length.
func validTokenValue(value string) bool {
	if value == "" || len(value) > maxTokenValue {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

// validErrorCode reports whether code is an error code in the character set RFC 6749 allows, of a bounded
// length. Anything else is dropped rather than shown.
func validErrorCode(code string) bool {
	if code == "" || len(code) > maxErrorCode {
		return false
	}
	for i := 0; i < len(code); i++ {
		if c := code[i]; c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}
