package googleapi

import (
	"net/http"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// tokenTimeout bounds one request to the token endpoint of a service account grant.
const tokenTimeout = 30 * time.Second

// OAuth is the fixed authorization server that every Google API with a token credential shares: Google's
// consent, token and revocation endpoints, and the IAM Credentials endpoint that issues tokens of a service
// account. access_type=offline makes the consent return a refresh token, and prompt=consent makes a renewed
// consent return one again, which Google otherwise omits. The impersonation request needs the cloud-platform
// scope, the only scope the IAM Credentials API names for it.
var OAuth = provider.OAuth{
	AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:     "https://oauth2.googleapis.com/token",
	RevokeURL:    "https://oauth2.googleapis.com/revoke",
	Params:       map[string]string{"access_type": "offline", "prompt": "consent"},
	ImpersonateURL: "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/" +
		provider.ServiceAccountSegment + ":generateAccessToken",
	ImpersonateScope: "https://www.googleapis.com/auth/cloud-platform",
}

// TokenGrants returns the grant of every procedure a Google token credential offers: a person's consent, a
// service account key, and the impersonation of a service account. rt carries their requests; nil is Go's
// default transport.
func TokenGrants(rt http.RoundTripper) map[config.TokenProcedure]provider.TokenGrant {
	oauth := OAuth
	oauth.Transport = rt
	return map[config.TokenProcedure]provider.TokenGrant{
		config.TokenConsent:       oauth.RefreshGrant(),
		config.TokenImpersonation: oauth.ImpersonationGrant(),
		config.TokenServiceAccount: provider.ServiceAccountGrant(provider.ServiceAccountOptions{
			Endpoint: OAuth.TokenURL,
			Client:   provider.NoRedirectClient(tokenTimeout, rt),
		}),
	}
}
