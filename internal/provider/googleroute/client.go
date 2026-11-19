package googleroute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// apiRoot is the one origin of the API and the only base URL a service configuration may name.
const apiRoot = "https://routeoptimization.googleapis.com"

// apiName names the API for the message of a disabled API. It is fixed here and never read from an answer.
const apiName = "the Route Optimization API (routeoptimization.googleapis.com)"

// defaultTimeout bounds one request. It stays below the 60 seconds an invoke may take, so a slow answer ends
// as a timeout of this provider and not as a cancelled call.
const defaultTimeout = 30 * time.Second

// An optimization answer can carry polylines, so the reading bound is large. It holds for the response of a
// finished operation too. What a tool hands back stays bounded by the cuts of toursResultOf, not by this bound.
const maxOptimizeResponseBytes = 8 << 20

// optimizeSlack is the time the POST gets beyond the solving time of the request, for the network and the
// reading of the answer. The solving time is at most toursMaxTimeout, so the request ends after at most
// 50 seconds and leaves 10 seconds of the 60 an invoke may take for the limiter wait and the token. The start
// of a long-running optimization passes toursStartTimeout instead, because Google answers it at once.
const optimizeSlack = 5 * time.Second

// unsureNote tells that an optimization whose request may have reached Google can have been billed.
const unsureNote = "; the request may have reached Google and may have been billed, and the tool does not " +
	"send it again, so a repeated call can be billed a second time"

// minInterval is the spacing of two requests of one credential. It is a cautious start of this provider, not a
// documented limit.
const minInterval = 500 * time.Millisecond

// maxHold bounds how long a Retry-After wait may delay the next request of this process.
const maxHold = time.Minute

const userAgent = "qatlas-connect"

// limiters holds the spacing of every credential this process has used.
var limiters = ratelimit.NewRegistry(minInterval)

// transport carries every request, the token requests included. A nil value is Go's default transport; the
// package's own tests replace it with a fake.
var transport http.RoundTripper

// checkConnection refuses what no request may start from: no connection and a base URL other than the
// official origin. The messages never quote the value.
func checkConnection(resolved *config.Resolved) error {
	switch {
	case resolved == nil:
		return failure("open", "no connection was selected")
	case !isAPIRoot(resolved.BaseURL):
		return failure("open", "a Google Route Optimization service must use the official API origin "+apiRoot)
	}
	return nil
}

// isAPIRoot reports whether a configured base URL names the official origin. An empty value and a trailing
// slash are the only variations a configuration may carry.
func isAPIRoot(raw string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	return trimmed == "" || trimmed == apiRoot
}

func tokenOptions() provider.TokenOptions {
	return provider.TokenOptions{Token: tokenMetadata, Grants: googleapi.TokenGrants(transport)}
}

// client sends the requests of one connection with the access tokens of its credential.
type client struct {
	http    *http.Client
	source  *provider.TokenSource
	limiter *ratelimit.Limiter
}

// open resolves the token roles of the credential. Everything wrong with the connection or its arguments ends
// before this call, so no secret is read for a request that would be refused.
func open(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (*client, error) {
	if secrets == nil {
		return nil, failure("open", "no credential resolver was configured")
	}
	source, err := provider.OpenTokenSource(ctx, resolved, secrets, red, tokenOptions())
	if err != nil {
		return nil, err
	}
	return withSource(resolved, source), nil
}

func withSource(resolved *config.Resolved, source *provider.TokenSource) *client {
	return &client{http: provider.NoRedirectClient(defaultTimeout, transport), source: source,
		limiter: limiters.For(resolved.Credential)}
}

// get sends one GET request to the fixed origin and decodes the JSON answer into out. path is below the origin
// and holds only checked segments. The token travels in the Authorization header alone. A 401 renews the token
// once, and nothing else repeats the request.
func (c *client) get(ctx context.Context, op, tool, path string, out any) error {
	build := func(access string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiRoot+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		return req, nil
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return provider.Waited(op, subject, err)
	}
	response, err := c.source.Read(ctx, c.http, tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		return provider.Transport(op, subject, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return c.statusFailure(op, response)
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return provider.InvalidResponse(op, subject+" returned an answer that is not JSON")
	}
	if failed := provider.ReadJSON(op, subject, response.Body, maxOptimizeResponseBytes, out); failed != nil {
		return failed
	}
	return nil
}

// statusFailure maps the answer of a failed status. A 429 holds the next request of the credential, and
// nothing repeats this one.
func (c *client) statusFailure(op string, response *http.Response) *provider.Error {
	body := provider.ReadErrorBody(response.Body)
	if response.StatusCode == http.StatusTooManyRequests {
		// A missing or unusable Retry-After still means the credential sent too much: the next call waits.
		c.limiter.HoldFor(min(max(provider.RetryAfter(response.Header), time.Second), maxHold))
	}
	return googleError(op, response.StatusCode, body)
}

// post sends the one POST of an optimization to the fixed origin and returns the raw JSON answer. It is never
// repeated: Mutate only drops the cached token on a 401, so the next call renews it. The request ends after
// timeout plus optimizeSlack. A failure that may follow a delivered request says that the call may have been
// billed; one that Google answered with a status that proves it did not process the request does not.
func (c *client) post(ctx context.Context, op, tool, path string, payload []byte, timeout time.Duration,
	out *json.RawMessage) error {
	unsure := func(failed *provider.Error) *provider.Error {
		failed.Message += unsureNote
		return failed
	}
	build := func(access string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiRoot+path, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		return req, nil
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return provider.Waited(op, subject, err)
	}
	limited := *c.http
	limited.Timeout = timeout + optimizeSlack
	response, err := c.source.Mutate(ctx, &limited, tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		return unsure(provider.Transport(op, subject, err))
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		if response.StatusCode == http.StatusBadRequest {
			return rejectedModel(op, provider.ReadErrorBody(response.Body))
		}
		failed := c.statusFailure(op, response)
		if response.StatusCode >= 500 {
			return unsure(failed)
		}
		return failed
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return unsure(provider.InvalidResponse(op, subject+" returned an answer that is not JSON"))
	}
	if failed := provider.ReadJSON(op, subject, response.Body, maxOptimizeResponseBytes, out); failed != nil {
		return unsure(failed)
	}
	return nil
}
