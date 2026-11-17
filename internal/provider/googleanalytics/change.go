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

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// changeCall is one request of a changing tool: a fixed method, a path that is a constant of this package
// with checked resource names, and for create and update the JSON body. tool names the tool whose scopes the
// access token must carry. With data set the request goes to the Data API origin instead of the Admin API.
type changeCall struct {
	op, tool, method, path string
	data                   bool
	query                  url.Values
	body                   []byte
	limit                  int64
	notFound               string
}

// unsureNote tells that a change whose request may have reached Google has an unknown outcome.
const unsureNote = "; the outcome of the change is unknown, so read the object before changing it again"

// unsure marks a failure after which the change may or may not have happened.
func unsure(failed *provider.Error) *provider.Error {
	failed.Message += unsureNote
	return failed
}

// change sends the one request of a mutation and decodes the JSON object answer into out; with a nil out only
// the status counts. The access token travels only in the Authorization header. A change is never sent twice:
// a 401 only drops the cached token (provider.TokenSource.Mutate), a 429 holds the next request of this
// credential. A transport failure, a 5xx answer and an answer that cannot be read leave the outcome unknown
// and are reported as such.
func (c *client) change(ctx context.Context, call changeCall, out any) error {
	root, api := adminRoot, "Admin"
	if call.data {
		root, api = dataRoot, "Data"
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
		req, err := http.NewRequestWithContext(ctx, call.method, target, body)
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
	response, err := c.source.Mutate(ctx, c.http, call.tool, build)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return unsure(provider.Transport(call.op, subject, err))
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		raw := provider.ReadErrorBody(response.Body)
		if response.StatusCode == http.StatusTooManyRequests {
			c.limiter.HoldFor(min(max(provider.RetryAfter(response.Header), time.Second), maxHold))
		}
		mapped := googleErrorFor(api, call.op, response.StatusCode, raw, call.notFound)
		if response.StatusCode >= 500 {
			unsure(mapped)
		}
		return mapped
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return nil
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		return unsure(provider.InvalidResponse(call.op, subject+" returned an answer that is not JSON"))
	}
	if failed := provider.ReadJSON(call.op, subject, response.Body, call.limit, out); failed != nil {
		return unsure(failed)
	}
	return nil
}
