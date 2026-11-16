package googleapi

import (
	"net/http"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// tokenTimeout bounds one request to a Google token or impersonation endpoint.
const tokenTimeout = 30 * time.Second

// OAuth holds the fixed endpoints of Google's authorization server that every Google provider whose credential
// issues access tokens registers: the authorization, token and revocation endpoints that Google's OpenID
// configuration names, and the IAM Credentials method that issues an access token of a service account
// (generateAccessToken, which needs the cloud-platform scope). access_type=offline asks for a refresh token,
// and prompt=consent makes Google issue one again when a person gives the consent once more.
var OAuth = provider.OAuth{
	AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:     "https://oauth2.googleapis.com/token",
	RevokeURL:    "https://oauth2.googleapis.com/revoke",
	Params:       map[string]string{"access_type": "offline", "prompt": "consent"},
	ImpersonateURL: "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/" +
		provider.ServiceAccountSegment + ":generateAccessToken",
	ImpersonateScope: "https://www.googleapis.com/auth/cloud-platform",
}

// TokenGrants returns the grant of every procedure Google supports for provider.TokenOptions.Grants: a
// person's consent, a service account key, and the impersonation of a service account. Every request goes
// to an endpoint of OAuth through rt; nil is Go's default transport.
func TokenGrants(rt http.RoundTripper) map[config.TokenProcedure]provider.TokenGrant {
	o := OAuth
	o.Transport = rt
	return map[config.TokenProcedure]provider.TokenGrant{
		config.TokenConsent: o.RefreshGrant(),
		config.TokenServiceAccount: provider.ServiceAccountGrant(provider.ServiceAccountOptions{
			Endpoint: o.TokenURL, Client: provider.NoRedirectClient(tokenTimeout, rt),
		}),
		config.TokenImpersonation: o.ImpersonationGrant(),
	}
}
