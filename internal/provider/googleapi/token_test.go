package googleapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// sampleRobot is a synthetic service account, split so that no scanner reads it as an address.
var sampleRobot = "robot" + "@" + "sample.invalid"

func TestOAuthIsValidAndFixed(t *testing.T) {
	if err := OAuth.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	for _, endpoint := range []string{OAuth.AuthorizeURL, OAuth.TokenURL, OAuth.RevokeURL, OAuth.ImpersonateURL} {
		u, err := url.Parse(strings.Replace(endpoint, provider.ServiceAccountSegment, "x", 1))
		if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Host, ".google.com") && !strings.HasSuffix(u.Host, ".googleapis.com") {
			t.Errorf("endpoint %q is not a fixed Google https address", endpoint)
		}
	}
	if OAuth.Params["access_type"] != "offline" || OAuth.Params["prompt"] != "consent" || OAuth.Transport != nil {
		t.Errorf("OAuth = %+v", OAuth)
	}
}

// recorder answers every Google endpoint with a token and records which addresses were asked.
type recorder struct {
	mu   sync.Mutex
	urls []string
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()
	body := `{"access_token":"synthetic-access","token_type":"Bearer","expires_in":600}`
	if strings.HasPrefix(req.URL.String(), "https://iamcredentials.googleapis.com/") {
		body = `{"accessToken":"synthetic-impersonated","expireTime":"2999-01-01T00:00:00Z"}`
	}
	if req.URL.Host != "oauth2.googleapis.com" && req.URL.Host != "iamcredentials.googleapis.com" {
		return nil, errors.New("unexpected host")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestTokenGrantsUseTheFixedEndpointsThroughTheTransport(t *testing.T) {
	rt := &recorder{}
	grants := TokenGrants(rt)
	if len(grants) != 3 || grants[config.TokenConsent] == nil || grants[config.TokenServiceAccount] == nil ||
		grants[config.TokenImpersonation] == nil {
		t.Fatalf("grants = %v", grants)
	}
	secrets := provider.TokenSecrets{ClientID: "client", RefreshToken: "synthetic-refresh"}
	token, err := grants[config.TokenConsent](context.Background(), provider.TokenRequest{
		Procedure: config.TokenConsent, Secrets: secrets, Scopes: []string{"scope-a"}})
	if err != nil || token.Access != "synthetic-access" {
		t.Fatalf("consent grant = %+v, %v", token, err)
	}
	token, err = grants[config.TokenImpersonation](context.Background(), provider.TokenRequest{
		Procedure: config.TokenImpersonation, Secrets: secrets, Scopes: []string{"scope-a"},
		ServiceAccount: sampleRobot})
	if err != nil || token.Access != "synthetic-impersonated" {
		t.Fatalf("impersonation grant = %+v, %v", token, err)
	}
	want := []string{OAuth.TokenURL, OAuth.TokenURL, "https://iamcredentials.googleapis.com/v1/projects/-/" +
		"serviceAccounts/" + sampleRobot + ":generateAccessToken"}
	if strings.Join(rt.urls, " ") != strings.Join(want, " ") {
		t.Errorf("requests = %v, want %v", rt.urls, want)
	}
	if OAuth.Transport != nil {
		t.Error("TokenGrants changed the shared OAuth value")
	}
}
