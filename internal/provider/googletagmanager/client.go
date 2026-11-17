package googletagmanager

import (
	"bytes"
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

// apiRoot is the only origin this provider sends an access token to; apiPrefix is the path of the API v2.
const (
	apiRoot   = "https://tagmanager.googleapis.com"
	apiPrefix = "/tagmanager/v2/"
	subject   = "Google"
	userAgent = "qatlas-connect"
)

// Bounds of one request. The timeout stays below the 60 seconds an invoke may take. One page of a list is
// far below the size cap.
const (
	timeout     = 30 * time.Second
	maxResponse = 4 << 20
)

// minInterval spaces two requests of one OAuth client: Tag Manager allows 0.25 queries per second per Cloud
// project, shared by every user of the client, so a burst would only draw a quota failure.
const minInterval = 4 * time.Second

// maxHold bounds how long a rate-limit answer may delay the next request of this process.
const maxHold = time.Minute

// Test seams: transport carries every request, including those of the token grants, nil being Go's default
// transport; tokenCache nil is the cache of the process.
var (
	transport  http.RoundTripper
	tokenCache *provider.TokenCache
	limiters   = ratelimit.NewRegistry(minInterval)
)

// client is one connection: its target, its token source, and the spacing its OAuth client shares.
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
		return errors.New("a Google Tag Manager service has a fixed origin; leave the base URL as " + apiRoot)
	}
	return nil
}

// checkConnection refuses what no request may start from, before any secret is read.
func checkConnection(resolved *config.Resolved) (scope, error) {
	switch {
	case resolved == nil:
		return scope{}, provider.Fail("open", "no connection was selected")
	case !isDefaultRoot(resolved.BaseURL):
		return scope{}, provider.Fail("open", "a Google Tag Manager service has a fixed origin; leave the base URL as "+apiRoot)
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

// withSource builds the client of a connection whose token source is open. The quota belongs to the Cloud
// project of the OAuth client, so the spacing is keyed by the client ID; a service account has none and is
// keyed by its key, whose project carries its quota. The registry keeps only a digest of the key.
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

// request is one call of the API: a fixed method, a path below the API prefix, and for a change the JSON
// body. A change goes out once; a read is sent again once after a renewed token.
type request struct {
	method string
	path   string
	query  url.Values
	body   []byte
	change bool
	// missing answers HTTP 404 as not-found with nothing of Google's answer, for a lookup that must not tell
	// a missing object from one outside the target.
	missing bool
}

// get reads path below the API prefix once and decodes the JSON object answer into out.
func (c *client) get(ctx context.Context, op, tool, path string, query url.Values, out any) error {
	return c.send(ctx, op, tool, request{method: http.MethodGet, path: path, query: query}, out)
}

// send makes the request and decodes the JSON answer into out; a nil out expects no content (a delete).
// The access token travels only in the Authorization header. A read renews the token once on 401 and sends
// again; a change is never sent twice, so it waits for the spacing of the client once.
func (c *client) send(ctx context.Context, op, tool string, r request, out any) error {
	target := apiRoot + apiPrefix + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}
	build := func(access string) (*http.Request, error) {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, provider.Waited(op, subject, err)
		}
		var body io.Reader
		if r.body != nil {
			body = bytes.NewReader(r.body)
		}
		req, err := http.NewRequestWithContext(ctx, r.method, target, body)
		if err != nil {
			return nil, provider.Fail(op, "the request could not be built")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		if r.body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	}
	send := c.source.Read
	if r.change {
		send = c.source.Mutate
	}
	resp, err := send(ctx, c.http, tool, build)
	if err != nil {
		// Go's own error of a request quotes its URL; every other error is already safe to return.
		var transportErr *url.Error
		if errors.As(err, &transportErr) {
			return unsure(provider.Transport(op, subject, err), r.change)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && (out != nil || resp.StatusCode != http.StatusNoContent) {
		if r.missing && resp.StatusCode == http.StatusNotFound {
			return notFound(op)
		}
		body := provider.ReadErrorBody(resp.Body)
		if resp.StatusCode == http.StatusTooManyRequests {
			// A missing or unusable Retry-After still holds the client; nothing repeats this request.
			c.limiter.HoldFor(min(max(provider.RetryAfter(resp.Header), minInterval), maxHold))
		}
		return unsure(googleError(op, resp.StatusCode, body), r.change && resp.StatusCode >= 500)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		return nil
	}
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		return unsure(provider.InvalidResponse(op, subject+" returned an answer that is not JSON"), r.change)
	}
	if failed := provider.ReadJSON(op, subject, resp.Body, maxResponse, out); failed != nil {
		return unsure(failed, r.change)
	}
	return nil
}

// unsure says that a change whose request may have reached Google has an unknown outcome: it is neither
// claimed nor denied, and never repeated here.
func unsure(failed *provider.Error, change bool) *provider.Error {
	if change {
		failed.Message += "; the outcome of the change is unknown, so read the object before changing it again"
	}
	return failed
}
