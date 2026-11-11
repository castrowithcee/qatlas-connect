package provider

import (
	"io"

	"github.com/castrowithcee/qatlas-kit/httpx"
)

// ErrorBodyLimit is the most bytes of a failed answer that are read to build an Original.
const ErrorBodyLimit = httpx.ErrorBodyLimit

// Original is the provider's own account of a refusal, kept apart from Qatlas' message. Only response
// content belongs here, never a request header or a credential. Everything in it is text a third party
// wrote.
type Original = httpx.Original

// OriginalEntry is one error of a provider answer. Every field is optional.
type OriginalEntry = httpx.OriginalEntry

// OriginalFields maps the JSON of one provider's error answer to an Original; see httpx.OriginalFields for
// the meaning of each field.
type OriginalFields = httpx.OriginalFields

// ReadErrorBody reads at most ErrorBodyLimit bytes of a failed answer. A read error keeps what was read, so
// the rest of the answer never decides the outcome. A nil reader is an empty body.
func ReadErrorBody(r io.Reader) []byte { return httpx.ReadErrorBody(r) }

// ErrorOriginal builds the Original of a failed answer from its status and body (see ReadErrorBody). A JSON
// body keeps only what fields names, a body that is not JSON is kept as its trimmed text, and an empty body
// leaves only the status.
func ErrorOriginal(status int, body []byte, fields OriginalFields) *Original {
	return httpx.ErrorOriginal(status, body, fields)
}

// ClassifyStatusBody is ClassifyStatus plus the Original of the answer, read from body. Class and message
// are exactly those of ClassifyStatus: the body never reaches them or Error().
func ClassifyStatusBody(op string, status int, texts StatusTexts, body io.Reader, fields OriginalFields) *Error {
	return fromKit(httpx.ClassifyStatusBody(op, status, texts, body, fields), status, texts.Subject)
}
