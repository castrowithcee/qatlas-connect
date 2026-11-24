package googlesearchconsole

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// Bounds of a batch answer. Google sends one HTTP answer per entry; a metadata answer holds two notifications
// of one URL, so a part stays far below its bound. What lies beyond the total bound leaves its entries
// without an answer instead of failing the others.
const (
	maxBatchBytes = 8 << 20
	maxPartBytes  = 64 << 10
)

// batchRequest is one call of a batch. method and path are constants of this package; only the escaped query
// of a metadata read and the JSON body of a notification carry checked arguments.
type batchRequest struct {
	method, path string
	body         []byte
}

// partAnswer is Google's answer to one entry of a batch. missing marks an entry without a readable answer:
// none came, its part was malformed or too long, or the batch answer ended early.
type partAnswer struct {
	missing     bool
	status      int
	contentType string
	header      http.Header
	body        []byte
}

// decode reads the JSON object of a successful entry into out.
func (a partAnswer) decode(out any) bool {
	mediaType, _, err := mime.ParseMediaType(a.contentType)
	return err == nil && mediaType == "application/json" && json.Unmarshal(a.body, out) == nil
}

// buildBatch writes the multipart/mixed body of a batch: one application/http part per request with its own
// Content-ID, a fixed request line, and fixed headers. The outer request carries the access token for every
// part, so no part holds one.
func buildBatch(op string, requests []batchRequest) ([]byte, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for i, r := range requests {
		var inner bytes.Buffer
		inner.WriteString(r.method + " " + r.path + " HTTP/1.1\r\nAccept: application/json\r\n")
		if r.body != nil {
			inner.WriteString("Content-Type: application/json\r\nContent-Length: " + strconv.Itoa(len(r.body)) + "\r\n")
		}
		inner.WriteString("\r\n")
		inner.Write(r.body)
		// The checks of the arguments keep spaces and line breaks out of the request line, and a boundary of
		// random letters cannot be guessed; both are verified, since a slip here would split a part.
		if strings.ContainsAny(r.path, " \r\n") || bytes.Contains(inner.Bytes(), []byte(writer.Boundary())) {
			return nil, "", failure(op, "the batch request could not be built")
		}
		part, err := writer.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"application/http"},
			"Content-Transfer-Encoding": {"binary"},
			"Content-ID":                {"<item-" + strconv.Itoa(i+1) + ">"},
		})
		if err != nil {
			return nil, "", failure(op, "the batch request could not be built")
		}
		if _, err := part.Write(inner.Bytes()); err != nil {
			return nil, "", failure(op, "the batch request could not be built")
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", failure(op, "the batch request could not be built")
	}
	return buffer.Bytes(), "multipart/mixed; boundary=" + writer.Boundary(), nil
}

// batch sends requests as one batch to the batch endpoint of the Indexing API and returns one answer per
// request, in their order. Google counts every entry, so the batch takes as many slots as it has entries. A
// changing batch goes through the mutation path and is never repeated. An answer that is no batch fails the
// whole call; a single entry without a readable answer fails only that entry.
func (c *client) batch(ctx context.Context, op, tool string, requests []batchRequest, slots []slot,
	mutate bool) ([]partAnswer, error) {
	body, contentType, err := buildBatch(op, requests)
	if err != nil {
		return nil, err
	}
	if err := reserve(ctx, op, len(requests), slots); err != nil {
		return nil, err
	}
	response, err := c.roundTrip(ctx, op, tool, http.MethodPost, indexingRoot+batchPath, body, contentType,
		"multipart/mixed", mutate)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		raw := provider.ReadErrorBody(response.Body)
		if response.StatusCode == http.StatusTooManyRequests {
			holdAfter429(response.Header, slots)
		}
		return nil, indexingError(op, response.StatusCode, raw)
	}
	answers, ok := readBatch(response.Body, response.Header.Get("Content-Type"), len(requests))
	if !ok {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		message := "Google returned an answer that is not a batch answer"
		if mutate {
			message += "; it may have taken the notifications, so check with indexing.get-metadata-batch before " +
				"sending them again"
		}
		return nil, provider.InvalidResponse(op, message)
	}
	for _, answer := range answers {
		if !answer.missing && answer.status == http.StatusTooManyRequests {
			holdAfter429(answer.header, slots)
			break
		}
	}
	return answers, nil
}

// readBatch splits a multipart/mixed batch answer into the answers of n entries. A part is matched to its
// entry by its Content-ID only, never by its position, since Google may answer in another order. A part of
// an unknown or repeated entry is skipped, and the number of parts read is bounded.
func readBatch(body io.Reader, contentType string, n int) ([]partAnswer, bool) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/mixed" || params["boundary"] == "" {
		return nil, false
	}
	answers := make([]partAnswer, n)
	for i := range answers {
		answers[i].missing = true
	}
	reader := multipart.NewReader(io.LimitReader(body, maxBatchBytes), params["boundary"])
	for range 2 * maxBatch {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		index, ok := answerIndex(part.Header.Get("Content-ID"), n)
		if !ok || !answers[index].missing {
			continue
		}
		answers[index] = readPart(part)
	}
	return answers, true
}

// answerIndex reads the entry of an answer part: Google names it response- and the Content-ID of the request.
func answerIndex(contentID string, n int) (int, bool) {
	id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(contentID), "<"), ">")
	number, ok := strings.CutPrefix(id, "response-item-")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(number)
	if err != nil || i < 1 || i > n {
		return 0, false
	}
	return i - 1, true
}

// readPart reads the HTTP answer one application/http part holds. Anything else, or a part beyond its bound,
// is no readable answer.
func readPart(part *multipart.Part) partAnswer {
	unreadable := partAnswer{missing: true}
	if mediaType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type")); err != nil || mediaType != "application/http" {
		return unreadable
	}
	raw, err := io.ReadAll(io.LimitReader(part, maxPartBytes+1))
	if err != nil || len(raw) > maxPartBytes {
		return unreadable
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), nil)
	if err != nil {
		return unreadable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return unreadable
	}
	return partAnswer{status: response.StatusCode, contentType: response.Header.Get("Content-Type"),
		header: response.Header, body: body}
}
