package googlesearchconsole

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// apiRoot is the only origin of the Search Console API; the paths come from its discovery document.
const (
	apiRoot   = "https://searchconsole.googleapis.com"
	sitesPath = "/webmasters/v3/sites"
)

// Bounds of one request. The timeout leaves room within the 60 seconds of an invoke for a token request
// before it. The answers are small: a property list of the 1000 properties an account may hold stays far
// below the read limit.
const (
	timeout  = 20 * time.Second
	maxBytes = 4 << 20
)

// minInterval spaces two requests of one credential. Google allows the site and sitemap methods 200 queries
// per minute per user, which is 300 ms.
const minInterval = 300 * time.Millisecond

// maxHold bounds how long a rate-limit answer may delay the next request of this process.
const maxHold = time.Minute

// userAgent names Qatlas honestly.
const userAgent = "qatlas-connect"

var (
	// transport carries every request, to the API and to the token endpoints. A nil value is Go's default
	// transport; the package's own tests replace it.
	transport http.RoundTripper
	// tokenCache holds the access tokens; nil is the cache of the process. Tests replace it.
	tokenCache *provider.TokenCache
	// limiters holds the spacing of every credential this process has used.
	limiters = ratelimit.NewRegistry(minInterval)
)

// client sends the requests of one call with the access tokens of the connection's credential.
type client struct {
	source  *provider.TokenSource
	http    *http.Client
	limiter *ratelimit.Limiter
}

func tokenOptions() provider.TokenOptions {
	return provider.TokenOptions{
		Token:  config.TokenMetadata{Scopes: toolScopes},
		Grants: googleapi.TokenGrants(transport),
		Cache:  tokenCache,
	}
}

// isDefaultRoot reports whether a configured base URL is the default or empty. The base URL has no effect:
// the origin is fixed, so any other value could only suggest that it does.
func isDefaultRoot(raw string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	return trimmed == "" || trimmed == apiRoot
}

// checkConnection refuses what no request may start from. The messages never quote a value.
func checkConnection(resolved *config.Resolved, secrets *secret.Resolver) error {
	switch {
	case resolved == nil:
		return failure("open", "no connection was selected")
	case !isDefaultRoot(resolved.BaseURL):
		return failure("open", "a Google Search Console service has a fixed origin; leave the base URL as "+apiRoot)
	case secrets == nil:
		return failure("open", "no credential resolver was configured")
	}
	return nil
}

// open checks the connection and opens the token source of its credential. Nothing is sent yet.
func open(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor) (*client, error) {
	if err := checkConnection(resolved, secrets); err != nil {
		return nil, err
	}
	source, err := provider.OpenTokenSource(ctx, resolved, secrets, red, tokenOptions())
	if err != nil {
		return nil, err
	}
	return newClient(source, resolved), nil
}

// newClient binds a token source to the rate limit its credential shares. Google counts per user, which is
// the account behind the credential, so the limit follows the credential and the service account it
// impersonates, never a token value.
func newClient(source *provider.TokenSource, resolved *config.Resolved) *client {
	key := strings.Join([]string{resolved.Credential, resolved.Secrets.ID, resolved.ServiceAccount}, "\x00")
	return &client{source: source, http: provider.NoRedirectClient(timeout, transport), limiter: limiters.For(key)}
}

// get sends one GET of tool, once (the token source renews a rejected token once), and decodes the JSON
// object answer into out. path is built from constants and escaped segments; query holds arguments only.
func (c *client) get(ctx context.Context, op, tool, path string, query url.Values, notFound string, out any) error {
	address := apiRoot + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return provider.Waited(op, "Google", err)
	}
	response, err := c.source.Read(ctx, c.http, tool, func(access string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, failure(op, "the request could not be built")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		return req, nil
	})
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		return provider.Transport(op, "Google", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		raw := provider.ReadErrorBody(response.Body)
		if response.StatusCode == http.StatusTooManyRequests {
			// A missing retry time still means too many requests: the next call waits, nothing repeats this one.
			c.limiter.HoldFor(min(max(provider.RetryAfter(response.Header), time.Second), maxHold))
		}
		return googleError(op, response.StatusCode, raw, notFound)
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return provider.InvalidResponse(op, "Google returned an answer that is not JSON")
	}
	if failed := provider.ReadJSON(op, "Google", response.Body, maxBytes, out); failed != nil {
		return failed
	}
	return nil
}
