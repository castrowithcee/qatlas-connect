package googlemerchant

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

// apiRoot is the only origin this provider sends an access token to. Every request path starts with the
// sub-API and its version, such as accounts/v1/.
const (
	apiRoot   = "https://merchantapi.googleapis.com"
	subject   = "Google Merchant"
	userAgent = "qatlas-connect"
)

// Bounds of one request. The timeout stays below the 60 seconds an invoke may take.
const (
	timeout     = 30 * time.Second
	maxResponse = 4 << 20
)

// holdMin and maxHold bound how long a rate-limit answer delays the next request of this process; the quota
// is counted per Merchant Center account and user, so no spacing between requests is kept otherwise.
const (
	holdMin = 10 * time.Second
	maxHold = time.Minute
)

// Test seams: transport carries every request, including those of the token grants, nil being Go's default
// transport; tokenCache nil is the cache of the process.
var (
	transport  http.RoundTripper
	tokenCache *provider.TokenCache
	limiters   = ratelimit.NewRegistry(0)
)

// client is one connection: its binding, its token source, and the hold its credential shares.
type client struct {
	scope   scope
	source  *provider.TokenSource
	http    *http.Client
	limiter *ratelimit.Limiter
}

func tokenOptions() provider.TokenOptions {
	return provider.TokenOptions{
		Token:  config.TokenMetadata{Scopes: toolScopes()},
		Grants: googleapi.TokenGrants(transport),
		Cache:  tokenCache,
	}
}

func isDefaultRoot(raw string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	return trimmed == "" || trimmed == apiRoot
}

func validateBaseURL(raw string) error {
	if !isDefaultRoot(raw) {
		return errors.New("the Merchant API has a fixed origin; leave the base URL as " + apiRoot)
	}
	return nil
}

// checkConnection refuses what no request may start from, before any secret is read.
func checkConnection(resolved *config.Resolved) (scope, error) {
	switch {
	case resolved == nil:
		return scope{}, provider.Fail("open", "no connection was selected")
	case !isDefaultRoot(resolved.BaseURL):
		return scope{}, provider.Fail("open", "the Merchant API has a fixed origin; leave the base URL as "+apiRoot)
	}
	return boundScope(resolved)
}

// open checks the connection and opens its token source.
func open(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor) (*client, error) {
	if _, err := checkConnection(resolved); err != nil {
		return nil, err
	}
	if secrets == nil {
		return nil, provider.Fail("open", "no credential resolver was configured")
	}
	source, err := provider.OpenTokenSource(ctx, resolved, secrets, red, tokenOptions())
	if err != nil {
		return nil, err
	}
	return withSource(ctx, resolved, secrets, source)
}

// withSource builds the client of a connection whose token source is open. The hold after a rate-limit answer
// is keyed by the OAuth client ID, or by the service account key, whose Cloud project carries the quota. The
// registry keeps only a digest of the key.
func withSource(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	source *provider.TokenSource) (*client, error) {
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	key := ""
	for _, role := range []struct{ name, prefix string }{
		{config.RoleClientID, "client:"}, {config.RoleServiceAccountKey, "service-account:"},
	} {
		value, found, err := secrets.ResolveOptional(ctx, resolved.Credential, resolved.Secrets, role.name)
		if err != nil {
			return nil, err
		}
		if found {
			key = role.prefix + value.Secret
			break
		}
	}
	if key == "" {
		return nil, &provider.Error{Class: provider.ClassCredentialUnusable, Op: "open",
			Message: "the credential holds neither a client ID nor a service account key"}
	}
	return &client{scope: bound, source: source, http: provider.NoRedirectClient(timeout, transport),
		limiter: limiters.For(key)}, nil
}

// get reads path once and decodes the JSON object answer into out. The path is built from the fixed
// sub-API path and account IDs that passed validID, never from free input. The access token travels only in
// the Authorization header; a read renews the token once on 401 and is sent again.
func (c *client) get(ctx context.Context, op, tool, path string, query url.Values, out any) error {
	target := apiRoot + "/" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	build := func(access string) (*http.Request, error) {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, provider.Waited(op, subject, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, provider.Fail(op, "the request could not be built")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		return req, nil
	}
	resp, err := c.source.Read(ctx, c.http, tool, build)
	if err != nil {
		// Go's own error of a request quotes its URL; every other error is already safe to return.
		var transportErr *url.Error
		if errors.As(err, &transportErr) {
			return provider.Transport(op, subject, err)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body := provider.ReadErrorBody(resp.Body)
		if resp.StatusCode == http.StatusTooManyRequests {
			c.limiter.HoldFor(min(max(provider.RetryAfter(resp.Header), holdMin), maxHold))
		}
		return googleError(op, resp.StatusCode, body)
	}
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		return provider.InvalidResponse(op, subject+" returned an answer that is not JSON")
	}
	if failed := provider.ReadJSON(op, subject, resp.Body, maxResponse, out); failed != nil {
		return failed
	}
	return nil
}
