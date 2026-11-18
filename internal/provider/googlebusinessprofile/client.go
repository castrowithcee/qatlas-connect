package googlebusinessprofile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The fixed origins of the Business Profile APIs. Each API is enabled in the Cloud project on its own. The
// tools of this package use the account management and the business information API; the others are the
// origins later tool groups use, named here so that no origin ever comes from configuration.
const (
	accountRoot       = "https://mybusinessaccountmanagement.googleapis.com"
	informationRoot   = "https://mybusinessbusinessinformation.googleapis.com"
	verificationsRoot = "https://mybusinessverifications.googleapis.com"
	performanceRoot   = "https://businessprofileperformance.googleapis.com"
	notificationsRoot = "https://mybusinessnotifications.googleapis.com"
	lodgingRoot       = "https://mybusinesslodging.googleapis.com"
	placeActionsRoot  = "https://mybusinessplaceactions.googleapis.com"
	legacyRoot        = "https://mybusiness.googleapis.com/v4"

	apiPrefix    = "/v1/"
	accountsPath = "accounts"
	subject      = "Google"
	userAgent    = "qatlas-connect"
)

// Bounds of one request. The timeout leaves room within the 60 seconds of an invoke for a token request and
// for the spacing before it. One page of 100 locations with every field stays far below the size cap.
const (
	timeout     = 20 * time.Second
	maxResponse = 8 << 20
)

// readInterval spaces two requests of one OAuth client: an approved Cloud project may send 300 requests per
// minute, shared by every user of its client, which is 200 ms.
const readInterval = 200 * time.Millisecond

// changeInterval spaces two changes of one profile: Google allows 10 changes per minute and profile, which
// is 6 seconds. Every tool that changes something waits for it with changeLimiter.
const changeInterval = 6 * time.Second

// maxHold bounds how long a rate-limit answer may delay the next request of this process.
const maxHold = time.Minute

// Test seams: transport carries every request, including those of the token grant, nil being Go's default
// transport; tokenCache nil is the cache of the process.
var (
	transport  http.RoundTripper
	tokenCache *provider.TokenCache
	// limiters holds the spacing of every OAuth client this process has used.
	limiters = ratelimit.NewRegistry(readInterval)
	// changeLimiters holds the spacing of the changes of every account or location this process has changed.
	// Google counts the changes per profile, whoever makes them, so the key is the account or location alone.
	changeLimiters = ratelimit.NewRegistry(changeInterval)
)

// client is one connection: its target, its token source, and the spacing its OAuth client shares.
type client struct {
	scope   scope
	source  *provider.TokenSource
	http    *http.Client
	limiter *ratelimit.Limiter
}

// tokenOptions offers only the grant of a person's consent; a credential with a service account key or a
// connection that impersonates a service account is unusable for this provider.
func tokenOptions() provider.TokenOptions {
	grants := googleapi.TokenGrants(transport)
	return provider.TokenOptions{
		Token:  config.TokenMetadata{Scopes: toolScopes},
		Grants: map[config.TokenProcedure]provider.TokenGrant{config.TokenConsent: grants[config.TokenConsent]},
		Cache:  tokenCache,
	}
}

// isDefaultRoot reports whether a configured base URL is the default or empty. The origins are fixed, so any
// other value could only suggest that it has an effect.
func isDefaultRoot(raw string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	return trimmed == "" || trimmed == accountRoot
}

const fixedOrigin = "a Google Business Profile service has fixed origins; leave the base URL as " + accountRoot

func validateBaseURL(raw string) error {
	if !isDefaultRoot(raw) {
		return errors.New(fixedOrigin)
	}
	return nil
}

// checkConnection refuses what no request may start from, before any secret is read, and returns the
// target. No message quotes a value.
func checkConnection(resolved *config.Resolved) (scope, error) {
	switch {
	case resolved == nil:
		return scope{}, provider.Fail("open", "no connection was selected")
	case !isDefaultRoot(resolved.BaseURL):
		return scope{}, provider.Fail("open", fixedOrigin)
	}
	return boundScope(resolved)
}

// open checks the connection and opens its token source. Nothing is sent yet.
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
// project of the OAuth client, so the spacing is keyed by the client ID; the registry keeps only a digest.
func withSource(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	source *provider.TokenSource) (*client, error) {
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	value, found, err := secrets.ResolveOptional(ctx, resolved.Credential, resolved.Secrets, config.RoleClientID)
	if err != nil {
		return nil, err
	}
	if !found || value.Secret == "" {
		return nil, &provider.Error{Class: provider.ClassCredentialUnusable, Op: "open",
			Message: "the credential holds no client ID; this provider takes only a consent through an OAuth client"}
	}
	return &client{scope: bound, source: source, http: provider.NoRedirectClient(timeout, transport),
		limiter: limiters.For("client:" + value.Secret)}, nil
}

// changeLimiter is the spacing of the changes of one account or location, named by its resource name.
func changeLimiter(key string) *ratelimit.Limiter { return changeLimiters.For(key) }

// request is one read of an API: a fixed origin, a path below its v1 prefix built from checked segments, and
// a query of checked arguments.
type request struct {
	root     string
	path     string
	query    url.Values
	notFound string
	// body is the JSON body of a POST that only reads, see post; a GET has none.
	body []byte
	// first marks the first request of a connection test, whose rate limit means a missing approval.
	first bool
}

// post sends the POST of a method that Google documents as a read, such as listing options. It is the same
// request as get with a JSON body, so it is spaced and renewed like a read and never counts as a change.
func (c *client) post(ctx context.Context, op, tool string, r request, out any) error {
	if r.body == nil {
		r.body = []byte("{}")
	}
	return c.get(ctx, op, tool, r, out)
}

// get sends one GET, once (the token source renews a rejected token once), and decodes the JSON object
// answer into out.
func (c *client) get(ctx context.Context, op, tool string, r request, out any) error {
	address := r.root + apiPrefix + r.path
	if len(r.query) > 0 {
		address += "?" + r.query.Encode()
	}
	build := func(access string) (*http.Request, error) {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, provider.Waited(op, subject, err)
		}
		method, payload := http.MethodGet, io.Reader(nil)
		if r.body != nil {
			method, payload = http.MethodPost, bytes.NewReader(r.body)
		}
		req, err := http.NewRequestWithContext(ctx, method, address, payload)
		if err != nil {
			return nil, provider.Fail(op, "the request could not be built")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		if r.body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("User-Agent", userAgent)
		return req, nil
	}
	resp, err := c.source.Read(ctx, c.http, tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		// Go's own error of a request quotes its URL; Transport keeps only the class and the cause.
		return provider.Transport(op, subject, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body := provider.ReadErrorBody(resp.Body)
		if resp.StatusCode == http.StatusTooManyRequests {
			// A missing or unusable Retry-After still holds the client; nothing repeats this request.
			c.limiter.HoldFor(min(max(provider.RetryAfter(resp.Header), time.Second), maxHold))
		}
		return googleError(op, resp.StatusCode, body, r.notFound, r.first)
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

// pageSizeQuery adds pageSize to query when size is set.
func pageSizeQuery(query url.Values, size int) url.Values {
	if size <= 0 {
		return query
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("pageSize", strconv.Itoa(size))
	return query
}
