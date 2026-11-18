package googlecloud

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

// Bounds of one request. The timeout stays below the 60 seconds an invoke may take, so a slow answer ends as
// a timeout of this provider and not as a cancelled call.
const (
	maxResponseBytes = 2 << 20
	defaultTimeout   = 30 * time.Second
)

// minInterval is the spacing of two requests of one credential. It is a cautious start of this provider, not a
// documented limit: the quotas of a project are set in its Cloud console and only partly readable.
const minInterval = 250 * time.Millisecond

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
		return failure("open", "a Google Cloud service must use the official API origin "+apiRoot)
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

// open resolves the token roles of the credential. Everything wrong with the connection or its targets ends
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

// get sends one GET request to a fixed service and decodes the JSON answer into out. tool is the tool the
// request serves, which decides the scopes of the token; path is below the origin and holds only checked
// segments. The token travels in the Authorization header alone. A 401 renews the token once, and nothing
// else repeats the request.
func (c *client) get(ctx context.Context, op, tool string, svc service, path string, query url.Values, out any) error {
	return c.send(ctx, op, tool, svc, http.MethodGet, path, query, nil, out, false)
}

// post sends one POST request of a reading tool, for the Google methods that read through POST, such as
// getIamPolicy and testIamPermissions. The body is a fixed JSON value built by the tool from checked
// arguments, or nil for a method without one. Like get, it renews the token once on a 401 and never repeats
// the request otherwise, which is safe because these methods only read.
func (c *client) post(ctx context.Context, op, tool string, svc service, path string, query url.Values, body any,
	out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return failure(op, "the request could not be built")
		}
	}
	return c.send(ctx, op, tool, svc, http.MethodPost, path, query, payload, out, false)
}

// unsureNote tells that a change whose request may have reached Google has an unknown outcome.
const unsureNote = "; the outcome of the change is unknown, so read the resource before changing it again"

// mutate sends the one POST of a changing tool with payload, a JSON value the tool built from checked
// arguments and the state it read. It is never repeated: a 401 only drops the cached token. A failure after
// which the request may have reached Google (a transport failure that may have arrived, a 5xx, an answer that
// cannot be read) says that the outcome is unknown.
func (c *client) mutate(ctx context.Context, op, tool string, svc service, path string, payload []byte, out any) error {
	return c.send(ctx, op, tool, svc, http.MethodPost, path, nil, payload, out, true)
}

func (c *client) send(ctx context.Context, op, tool string, svc service, method, path string, query url.Values,
	payload []byte, out any, mutation bool) error {
	origin, ok := serviceOrigins[svc]
	if !ok {
		return failure(op, "the service is not one of this provider")
	}
	endpoint := origin + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	build := func(access string) (*http.Request, error) {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return nil, err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return provider.Waited(op, subject, err)
	}
	unsure := func(failed *provider.Error) *provider.Error {
		if mutation {
			failed.Message += unsureNote
		}
		return failed
	}
	exchange := c.source.Read
	if mutation {
		exchange = c.source.Mutate
	}
	response, err := exchange(ctx, c.http, tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		failed = provider.Transport(op, subject, err)
		if failed.MayHaveArrived() {
			return unsure(failed)
		}
		return failed
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		mapped := c.statusFailure(op, svc, response)
		if response.StatusCode >= 500 {
			return unsure(mapped)
		}
		return mapped
	}
	if response.StatusCode == http.StatusNoContent && out == nil {
		// Methods such as the Cloud Storage deletions answer 204 without a body.
		return nil
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return unsure(provider.InvalidResponse(op, subject+" returned an answer that is not JSON"))
	}
	if failed := provider.ReadJSON(op, subject, response.Body, maxResponseBytes, out); failed != nil {
		return unsure(failed)
	}
	return nil
}

// statusFailure maps the answer of a failed status. A 429 holds the next request of the credential, and
// nothing repeats this one.
func (c *client) statusFailure(op string, svc service, response *http.Response) *provider.Error {
	body := provider.ReadErrorBody(response.Body)
	if response.StatusCode == http.StatusTooManyRequests {
		// A missing or unusable Retry-After still means the credential sent too much: the next call waits.
		c.limiter.HoldFor(min(max(provider.RetryAfter(response.Header), time.Second), maxHold))
	}
	return googleError(op, svc, response.StatusCode, body)
}
