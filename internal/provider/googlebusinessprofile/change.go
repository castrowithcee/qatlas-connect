package googlebusinessprofile

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

// change is one write to an API, a PATCH unless method says otherwise: a fixed origin, a path of checked
// segments, and a body built by the tool from checked arguments. key names what Google counts the change
// against, see changeLimiter.
type change struct {
	method   string
	root     string
	path     string
	query    url.Values
	body     []byte
	key      string
	notFound string
}

// patch sends one change, exactly once, and decodes the JSON object answer into out. A token Google rejects is
// dropped for the next call and the change is not sent again. Where the request may have reached Google and
// the outcome is unclear, the message says so and nothing repeats the change.
func (c *client) patch(ctx context.Context, op, tool string, r change, out any) error {
	address := r.root + apiPrefix + r.path
	if len(r.query) > 0 {
		address += "?" + r.query.Encode()
	}
	build := func(access string) (*http.Request, error) {
		if err := changeLimiter(r.key).Wait(ctx); err != nil {
			return nil, provider.Waited(op, subject, err)
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, provider.Waited(op, subject, err)
		}
		method := r.method
		if method == "" {
			method = http.MethodPatch
		}
		req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(r.body))
		if err != nil {
			return nil, provider.Fail(op, "the request could not be built")
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		if len(r.body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("User-Agent", userAgent)
		return req, nil
	}
	resp, err := c.source.Mutate(ctx, c.http, tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return err
		}
		return unsure(provider.Transport(op, subject, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body := provider.ReadErrorBody(resp.Body)
		if resp.StatusCode == http.StatusTooManyRequests {
			c.limiter.HoldFor(min(max(provider.RetryAfter(resp.Header), time.Second), maxHold))
		}
		failed := googleError(op, resp.StatusCode, body, r.notFound, false)
		if resp.StatusCode >= http.StatusInternalServerError {
			return unsure(failed)
		}
		return failed
	}
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		return unsure(provider.InvalidResponse(op, subject+" returned an answer that is not JSON"))
	}
	if failed := provider.ReadJSON(op, subject, resp.Body, maxResponse, out); failed != nil {
		return unsure(failed)
	}
	return nil
}

// unsure says that a change whose request may have reached Google has an unknown outcome: it is neither
// claimed nor denied.
func unsure(failed *provider.Error) *provider.Error {
	failed.Message += "; the outcome of the change is unknown, so read the object before changing it again"
	return failed
}
