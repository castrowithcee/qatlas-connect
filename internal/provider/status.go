package provider

import (
	"errors"
	"io"

	"github.com/castrowithcee/qatlas-kit/httpx"
)

// StatusTexts are the provider-specific parts of the standard status messages. Subject names the provider
// in the messages for 429, 503, 504, a redirect, and any other status. Auth, Permission, and NotFound are the
// complete messages for 401, 403, and 404, because their wording differs from provider to provider.
type StatusTexts = httpx.StatusTexts

// ClassifyStatus maps an HTTP status of a failed request to a stable class and message. The provider body is
// never read into the message. It covers 401, 403, 404, 429, 503, 504, every 3xx, and any other status.
// Holding a rate limiter for a 429 is up to the caller, which has the response headers; see RetryAfter.
func ClassifyStatus(op string, status int, texts StatusTexts) *Error {
	return fromKit(httpx.ClassifyStatus(op, status, texts), status, texts.Subject)
}

// ReadJSON reads a response body of at most limit bytes and decodes it into out. A body that cannot be read
// or exceeds the limit, and a body that is not valid JSON for out, are invalid-response errors. Subject names
// the provider in the message, for example "Baserow". It returns nil on success; callers must test the
// result with != nil before returning it as an error interface.
func ReadJSON(op, subject string, r io.Reader, limit int64, out any) *Error {
	err := httpx.ReadJSON(op, subject, r, limit, out)
	if err == nil {
		return nil
	}
	var failure *httpx.Error
	if errors.As(err, &failure) {
		return fromKit(failure, 0, subject)
	}
	return InvalidResponse(op, subject+" returned an invalid response")
}

// fromKit is the one place that turns a Kit failure into a Connect error: it maps the Kit class to the
// Connect class and keeps the Connect wording of a redirect, which names the product.
func fromKit(failure *httpx.Error, status int, subject string) *Error {
	converted := &Error{Class: classOf(failure.Class), Op: failure.Op, Message: failure.Message,
		Original: failure.Original}
	if status >= 300 && status < 400 {
		converted.Message = subject + " answered with a redirect, which Qatlas Connect does not follow for this request"
	}
	return converted
}

func classOf(class httpx.Class) Class {
	switch class {
	case httpx.ClassAuth:
		return ClassAuth
	case httpx.ClassPermission:
		return ClassPermission
	case httpx.ClassNotFound:
		return ClassNotFound
	case httpx.ClassRateLimited:
		return ClassRateLimited
	case httpx.ClassUnreachable:
		return ClassUnreachable
	case httpx.ClassTimeout:
		return ClassTimeout
	case httpx.ClassInvalidResponse:
		return ClassInvalidResponse
	}
	return ClassProviderError
}
