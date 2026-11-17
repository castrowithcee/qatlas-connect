package googleanalytics

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// adminRoot is the origin of the Analytics Admin API and dataRoot the one of the Analytics Data API. Both are
// fixed and every path is a constant of this package.
const (
	adminRoot = "https://analyticsadmin.googleapis.com"
	dataRoot  = "https://analyticsdata.googleapis.com"
)

// Bounds of one request. The timeout stays well below the 60 seconds an invoke may take, also when a request
// is sent a second time after its access token was renewed.
const (
	timeout = 25 * time.Second
	// maxHold bounds how long a rate-limit wait may delay the next request of this process.
	maxHold = time.Minute
)

// minInterval spaces two requests of one credential. The Admin API allows 600 requests per minute per user
// and 1,200 per project; the per-user quota is the tighter one for a credential.
const minInterval = 100 * time.Millisecond

// userAgent names Qatlas honestly.
const userAgent = "qatlas-connect"

// limiters holds the spacing of every credential this process has used.
var limiters = ratelimit.NewRegistry(minInterval)

// transport carries every request, to the API and to Google's token endpoints. A nil value is Go's default
// transport; the package's own tests replace it.
var transport http.RoundTripper

// tokens caches the access tokens; nil is the cache of the process. The package's own tests replace it.
var tokens *provider.TokenCache

// client sends the requests of one connection with the access tokens of its credential.
type client struct {
	source  *provider.TokenSource
	http    *http.Client
	limiter *ratelimit.Limiter
	bind    binding
	// identity names the credential and service account whose quota the requests use.
	identity string
}

// open checks the connection and its targets, then resolves the token roles of its credential. Anything
// wrong with the connection itself ends before a secret is read.
func open(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor) (*client, error) {
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	if secrets == nil {
		return nil, failure("open", "no credential resolver was configured")
	}
	source, err := provider.OpenTokenSource(ctx, resolved, secrets, red, tokenOptions())
	if err != nil {
		return nil, err
	}
	return newClient(resolved, source, bind), nil
}

// newClient binds a token source to the rate limit its identity shares: the credential and, for an
// impersonation, the service account whose quota the requests use.
func newClient(resolved *config.Resolved, source *provider.TokenSource, bind binding) *client {
	identity := resolved.Credential + "\x00" + resolved.ServiceAccount
	return &client{source: source, http: provider.NoRedirectClient(timeout, transport),
		limiter: limiters.For(identity), bind: bind, identity: identity}
}

// apiCall is one request without a side effect. It is a GET on the Admin API; with data set it is a POST of
// body on the Data API, whose methods read only; method names another method than that default, which also
// lets a reading Admin API method be a POST. tool
// names the tool whose scopes the access token must carry, path is a constant of this package with checked
// resource names, and query holds the checked arguments.
type apiCall struct {
	op, tool, path string
	query          url.Values
	data           bool
	method         string
	body           []byte
	limit          int64
	notFound       string
}

// get sends one request and decodes a JSON object answer into out. The access token travels only in the
// Authorization header. A 401 renews the token once and sends once more (provider.TokenSource.Read); a second
// 401 is an auth failure. A 429 holds the next request of this credential and is never repeated.
func (c *client) get(ctx context.Context, call apiCall, out any) error {
	root, api, method := adminRoot, "Admin", http.MethodGet
	if call.data {
		root, api, method = dataRoot, "Data", http.MethodPost
	}
	if call.method != "" {
		method = call.method
	}
	target := root + call.path
	if len(call.query) > 0 {
		target += "?" + call.query.Encode()
	}
	build := func(access string) (*http.Request, error) {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, provider.Waited(call.op, subject, err)
		}
		var body io.Reader
		if call.body != nil {
			body = bytes.NewReader(call.body)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, body)
		if err != nil {
			return nil, failure(call.op, "the request could not be built")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		if call.body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	}
	response, err := c.source.Read(ctx, c.http, call.tool, build)
	if err != nil {
		return sendError(call.op, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		raw := provider.ReadErrorBody(response.Body)
		if response.StatusCode == http.StatusTooManyRequests {
			// A missing or unusable retry time still means the credential sent too much; the next call waits,
			// and nothing repeats this one.
			c.limiter.HoldFor(min(max(provider.RetryAfter(response.Header), time.Second), maxHold))
		}
		return googleErrorFor(api, call.op, response.StatusCode, raw, call.notFound)
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return provider.InvalidResponse(call.op, subject+" returned an answer that is not JSON")
	}
	if failed := provider.ReadJSON(call.op, subject, response.Body, call.limit, out); failed != nil {
		return failed
	}
	return nil
}

// sendError keeps a classified failure and the error of a secret store, such as a locked vault that would
// have to keep a rotated refresh token, and normalises a transport failure without its URL text.
func sendError(op string, err error) error {
	var (
		failed *provider.Error
		urlErr *url.Error
	)
	switch {
	case errors.As(err, &failed):
		return failed
	case errors.As(err, &urlErr), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return provider.Transport(op, subject, err)
	}
	return err
}

// failure is a provider error of the generic class.
func failure(op, message string) *provider.Error {
	return &provider.Error{Class: provider.ClassProviderError, Op: op, Message: message}
}

// badRequest is the refusal of an argument. It names the argument and the rule, never the value.
func badRequest(message string) error {
	return &provider.InvalidRequestError{Message: message}
}

const (
	notFoundGeneric = subject + " did not find the resource, or the credential may not see it"
	enableHint      = "check that the account of the credential has access to the Analytics account or property"
)

// googleError maps a failed answer to a provider error by its HTTP status. On top, a 403 with the
// documented reason SERVICE_DISABLED (google.api.ErrorReason) says that the Analytics API is not enabled
// in the Cloud project of the credential. Nothing else of the body decides: the message and details of the
// Google error object are provider text and travel only as the Original.
func googleError(op string, status int, body []byte, notFound string) *provider.Error {
	return googleErrorFor("Admin", op, status, body, notFound)
}

// googleErrorFor is googleError for the named API, Admin or Data.
func googleErrorFor(api, op string, status int, body []byte, notFound string) *provider.Error {
	_, reason := googleapi.StatusOf(body)
	var mapped *provider.Error
	switch {
	case status == http.StatusForbidden && reason == "SERVICE_DISABLED":
		mapped = &provider.Error{Class: provider.ClassPermission, Op: op, Message: "the Google Analytics " + api +
			" API is not enabled in the Cloud project of the credential; a person enables it there"}
	default:
		mapped = provider.ClassifyStatus(op, status, provider.StatusTexts{
			Subject:    subject,
			Auth:       subject + " rejected the access token of the credential",
			Permission: subject + " refused the request; " + enableHint,
			NotFound:   notFound,
		})
	}
	mapped.Original = provider.ErrorOriginal(status, body, googleapi.Fields)
	return mapped
}
