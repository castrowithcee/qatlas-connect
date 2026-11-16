package googleapi

import (
	"net/http"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The fixed endpoints of Google's OAuth 2.0 server, as its OpenID discovery document names them, and the
// IAM Credentials method that issues an access token of a service account for impersonation.
const (
	AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL     = "https://oauth2.googleapis.com/token"
	RevokeURL    = "https://oauth2.googleapis.com/revoke"
	// ImpersonateURL is generateAccessToken; the discovery document requires the wildcard project "-".
	ImpersonateURL = "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/" +
		provider.ServiceAccountSegment + ":generateAccessToken"
	// ImpersonateScope is the only scope the discovery document lists for generateAccessToken.
	ImpersonateScope = "https://www.googleapis.com/auth/cloud-platform"
)

// tokenTimeout bounds one request to a token endpoint.
const tokenTimeout = 30 * time.Second

// OAuth returns the consent endpoints every Google provider shares. rt carries the requests to the token,
// revocation and impersonation endpoints; nil is Go's default transport, and a test passes its fake.
//
// access_type=offline makes Google issue a refresh token and prompt=consent makes it issue one again for a
// person who consented before. Google documents no narrower scope set for a refresh request, so the refresh
// asks for none.
func OAuth(rt http.RoundTripper) provider.OAuth {
	return provider.OAuth{
		AuthorizeURL: AuthorizeURL, TokenURL: TokenURL, RevokeURL: RevokeURL,
		Params:         map[string]string{"access_type": "offline", "prompt": "consent"},
		ImpersonateURL: ImpersonateURL, ImpersonateScope: ImpersonateScope,
		Transport: rt,
	}
}

// TokenGrants returns the grant of every token procedure a Google provider offers: the consent, the service
// account key and the impersonation of a service account. rt is as for OAuth.
func TokenGrants(rt http.RoundTripper) map[config.TokenProcedure]provider.TokenGrant {
	oauth := OAuth(rt)
	return map[config.TokenProcedure]provider.TokenGrant{
		config.TokenConsent: oauth.RefreshGrant(),
		config.TokenServiceAccount: provider.ServiceAccountGrant(provider.ServiceAccountOptions{
			Endpoint: TokenURL, Client: provider.NoRedirectClient(tokenTimeout, rt),
		}),
		config.TokenImpersonation: oauth.ImpersonationGrant(),
	}
}
