package googlemeasurementprotocol

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// apiRoot is the only origin a connection may name. The path below it is fixed in this package.
const apiRoot = "https://www.google-analytics.com"

const collectPath = "/mp/collect"

// defaultTimeout stays below the 60 seconds an invoke may take.
const defaultTimeout = 30 * time.Second

// minInterval spaces two requests to one data stream. It is a cautious start of this provider, not a
// documented limit.
const minInterval = 500 * time.Millisecond

// maxHold bounds how long a Retry-After wait may delay the next request of this process.
const maxHold = time.Minute

const userAgent = "qatlas-connect"

// limiters holds the spacing of every data stream this process has used.
var limiters = ratelimit.NewRegistry(minInterval)

// transport carries every request. A nil value is Go's default transport; the package's own tests replace it.
var transport http.RoundTripper

// unsureSuffix is added to a failure that leaves open whether Google received the events.
const unsureSuffix = "; the events may have reached Google, so do not resend them without checking"

func failure(op, message string) *provider.Error {
	return &provider.Error{Class: provider.ClassProviderError, Op: op, Message: message}
}

func badRequest(message string) error { return &provider.InvalidRequestError{Message: message} }

func isAPIRoot(raw string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	return trimmed == "" || trimmed == apiRoot
}

// validSecret keeps an obviously unusable value out of the URL.
func validSecret(value string) bool { return provider.ValidHeaderToken(value) }

// open resolves the secret and registers it with the redactor at once.
func open(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor) (string, error) {
	if secrets == nil {
		return "", failure("open", "no credential resolver was configured")
	}
	value, err := secrets.Resolve(ctx, resolved.Credential, resolved.Secrets, roleSecret)
	if err != nil {
		return "", err
	}
	if red != nil {
		red.Add(value.Secret)
	}
	if !validSecret(value.Secret) {
		return "", &provider.Error{Class: provider.ClassAuth, Op: "open", Message: "the Google Measurement Protocol API secret is unusable"}
	}
	return value.Secret, nil
}

// collect sends one body once. The URL carries the secret and stays inside this function: Go's url.Error would
// quote it, and every error built here omits it. A 2xx answer is only "sent": Google reports nothing else.
func collect(ctx context.Context, op, id, apiSecret string, body []byte) error {
	endpoint := apiRoot + collectPath + "?" + url.Values{"measurement_id": {id}, "api_secret": {apiSecret}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return failure(op, "the request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	limiter := limiters.For(id)
	if err := limiter.Wait(ctx); err != nil {
		return provider.Waited(op, "Google Analytics", err)
	}
	response, err := provider.NoRedirectClient(defaultTimeout, transport).Do(req)
	if err != nil {
		failed := provider.Transport(op, "Google Analytics", err)
		if failed.MayHaveArrived() {
			failed.Message += unsureSuffix
		}
		return failed
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		return nil
	}
	if response.StatusCode == http.StatusTooManyRequests {
		limiter.HoldFor(min(max(provider.RetryAfter(response.Header), time.Second), maxHold))
	}
	failed := provider.ClassifyStatus(op, response.StatusCode, provider.StatusTexts{
		Subject:    "Google Analytics",
		Auth:       "Google Analytics rejected the request; check the API secret",
		Permission: "Google Analytics refused the request; check the API secret and the measurement ID",
		NotFound:   "Google Analytics did not find the endpoint; the API may have changed",
	})
	if response.StatusCode >= 300 && (response.StatusCode < 400 || response.StatusCode >= 500) {
		failed.Message += unsureSuffix
	}
	return failed
}
