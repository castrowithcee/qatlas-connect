package googlecloud

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// mediaAnswer is the body of a media download, read up to a fixed limit, with the headers the caller needs.
type mediaAnswer struct {
	body   []byte
	status int
	header http.Header
}

// getMedia sends one GET request whose answer is not JSON, such as the content of an object, and reads at most
// limit bytes of the body; the rest is not read, and the connection is dropped. byteRange is either empty or a
// Range value the caller built from checked integers. The body is returned as bytes and never interpreted.
// Like get, it renews the token once on a 401 and never repeats the request otherwise.
func (c *client) getMedia(ctx context.Context, op, tool string, svc service, path string, query url.Values,
	byteRange string, limit int64) (*mediaAnswer, error) {
	origin, ok := serviceOrigins[svc]
	if !ok {
		return nil, failure(op, "the service is not one of this provider")
	}
	endpoint := origin + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	build := func(access string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "*/*")
		req.Header.Set("User-Agent", userAgent)
		if byteRange != "" {
			req.Header.Set("Range", byteRange)
		}
		return req, nil
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, provider.Waited(op, subject, err)
	}
	response, err := c.source.Read(ctx, c.http, tool, build)
	if err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			return nil, err
		}
		return nil, provider.Transport(op, subject, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, c.statusFailure(op, svc, response)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return nil, provider.InvalidResponse(op, subject+" returned an answer that could not be read")
	}
	return &mediaAnswer{body: body, status: response.StatusCode, header: response.Header}, nil
}

// contentRangeTotal parses a Content-Range value "bytes START-END/TOTAL" and returns START and TOTAL, with
// total -1 where Google does not know it.
func contentRangeTotal(value string) (start, total int64, ok bool) {
	rest, found := strings.CutPrefix(value, "bytes ")
	if !found {
		return 0, 0, false
	}
	span, size, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, false
	}
	first, _, found := strings.Cut(span, "-")
	if !found {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}
	if size == "*" {
		return start, -1, true
	}
	total, err = strconv.ParseInt(size, 10, 64)
	if err != nil || total < 0 {
		return 0, 0, false
	}
	return start, total, true
}
