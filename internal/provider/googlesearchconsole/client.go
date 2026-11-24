package googlesearchconsole

import (
	"bytes"
	"context"
	"encoding/json"
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

// queryInterval spaces two Search Analytics queries of one property and user. Google allows that method
// 1,200 queries per minute per site and per user, which is 50 ms. The load quota behind it has no number
// Google publishes, so a heavy query is made rare by the tool description, not by this spacing.
const queryInterval = 50 * time.Millisecond

// maxQueryBytes caps the answer of a Search Analytics query, which holds up to 25,000 rows.
const maxQueryBytes = 16 << 20

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
	// queryLimiters holds the spacing per credential and property of the Search Analytics query, whose
	// quota Google counts per site and user and apart from the site methods.
	queryLimiters = ratelimit.NewRegistry(queryInterval)
)

// client sends the requests of one call with the access tokens of the connection's credential.
type client struct {
	source  *provider.TokenSource
	http    *http.Client
	limiter *ratelimit.Limiter
	key     string
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
	key := credentialKey(resolved)
	return &client{source: source, http: provider.NoRedirectClient(timeout, transport), limiter: limiters.For(key), key: key}
}

// credentialKey names the credential and the service account a rate limit follows.
func credentialKey(resolved *config.Resolved) string {
	return strings.Join([]string{resolved.Credential, resolved.Secrets.ID, resolved.ServiceAccount}, "\x00")
}

// get sends one GET of tool, once (the token source renews a rejected token once), and decodes the JSON
// object answer into out. path is built from constants and escaped segments; query holds arguments only.
func (c *client) get(ctx context.Context, op, tool, path string, query url.Values, notFound string, out any) error {
	address := apiRoot + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	return c.send(ctx, op, tool, http.MethodGet, address, nil, c.limiter, notFound, maxBytes, false, out)
}

// change sends the one request of a changing tool through the mutation path of the token source: it is never
// repeated, not even after a 401. Google answers these methods with an empty body, so success carries no
// result; the request has no body, as every argument lives in the path.
func (c *client) change(ctx context.Context, op, tool, method, path, notFound string) error {
	return c.send(ctx, op, tool, method, apiRoot+path, nil, c.limiter, notFound, maxBytes, true, nil)
}

// queryLimiter is the spacing of the Search Analytics queries of this credential on one property.
func (c *client) queryLimiter(site string) *ratelimit.Limiter {
	return queryLimiters.For(c.key + "\x00" + site)
}

// post sends one POST of a reading tool with a JSON body Qatlas built from checked arguments, once like get.
// The method stays a read for the token source: the tool only queries.
func (c *client) post(ctx context.Context, op, tool, path string, body any, limiter *ratelimit.Limiter,
	notFound string, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return failure(op, "the request body could not be built")
	}
	return c.send(ctx, op, tool, http.MethodPost, apiRoot+path, encoded, limiter, notFound, maxQueryBytes, false, out)
}

// send waits for the slot of limiter, sends the request and decodes a JSON answer of at most limit bytes.
// A nil out means the method has no answer to read: an empty 2xx answer is success whatever its status or
// content type, and only a JSON object may stand in for it.
func (c *client) send(ctx context.Context, op, tool, method, address string, body []byte, limiter *ratelimit.Limiter,
	notFound string, limit int64, mutate bool, out any) error {
	if err := limiter.Wait(ctx); err != nil {
		return provider.Waited(op, "Google", err)
	}
	send := c.source.Read
	if mutate {
		send = c.source.Mutate
	}
	response, err := send(ctx, c.http, tool, func(access string) (*http.Request, error) {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, address, reader)
		if err != nil {
			return nil, failure(op, "the request could not be built")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
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
			limiter.HoldFor(min(max(provider.RetryAfter(response.Header), time.Second), maxHold))
		}
		return googleError(op, response.StatusCode, raw, notFound)
	}
	if out == nil {
		return emptyAnswer(op, response)
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return provider.InvalidResponse(op, "Google returned an answer that is not JSON")
	}
	if failed := provider.ReadJSON(op, "Google", response.Body, limit, out); failed != nil {
		return failed
	}
	return nil
}

// emptyAnswer accepts the answer of a method that has none: no body at all, or an empty JSON object.
func emptyAnswer(op string, response *http.Response) error {
	const limit = 1 << 10
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return provider.InvalidResponse(op, "Google returned an unreadable answer")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" || len(raw) > limit {
		return provider.InvalidResponse(op, "Google returned an answer that is not JSON")
	}
	// ReadJSON returns a *provider.Error; a nil one must not become a non-nil error.
	if failed := provider.ReadJSON(op, "Google", bytes.NewReader(raw), limit, &struct{}{}); failed != nil {
		return failed
	}
	return nil
}
