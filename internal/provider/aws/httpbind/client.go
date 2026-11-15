package httpbind

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awserr"
)

// DefaultResponseLimit caps a success response when Client.ResponseLimit is zero.
const DefaultResponseLimit = 4 << 20

// Codec is the body format of a REST protocol. restjson implements it; a restXml package would too.
type Codec interface {
	// Marshal serializes v, a value of the struct, document or payload shape s, into a whole body.
	Marshal(s *Shape, v any) ([]byte, error)
	// Unmarshal reads a non-empty body as a value of s. A struct comes back as map[string]any with the
	// Field.Key names. The second result says that a string was cut at its cap.
	Unmarshal(s *Shape, data []byte) (v any, cut bool, err error)
	// ContentType is the Content-Type of a body Marshal made; Accept is the Accept header of every request.
	ContentType() string
	Accept() string
	// ErrorCode returns the bare provider error code of a failed answer from its headers and its first
	// bytes, or empty when there is none. It never returns the provider's message.
	ErrorCode(header http.Header, body []byte) string
}

// ErrorClassifier is implemented by a Codec whose protocol brings its own error codes (restXml). The client
// then asks it instead of awserr.ClassifyStrict.
type ErrorClassifier interface {
	ClassifyError(op string, status int, header http.Header, code, errType string) (*provider.Error, time.Duration)
}

// ErrorTyper is implemented by a Codec whose error answers carry a type besides the code (Route 53 Type). Like
// the code it is a bare token and never the provider's message.
type ErrorTyper interface {
	ErrorType(header http.Header, body []byte) string
}

// ErrorMessenger is implemented by a Codec whose error answer for some codes lists messages the caller needs, such
// as the single problems of a Route 53 InvalidChangeBatch. They are provider text: bounded, cleaned of control
// characters, and kept in Original, never in the message of Qatlas.
type ErrorMessenger interface {
	ErrorMessages(code string, body []byte) []string
}

// SuccessErrorDetector is implemented by a Codec of a protocol that can answer an error with a success status
// (S3 CopyObject, CompleteMultipartUpload: 200 with an Error element in the body). SuccessError reports the
// error code of such a body, found being false for a normal answer. The client asks it only when the output
// of the operation is a document, never for a raw payload, which may hold any bytes.
type SuccessErrorDetector interface {
	SuccessError(body []byte) (code, errType string, found bool)
}

// Signer signs a request for the exact body that is sent. *sigv4.Signer satisfies it.
type Signer interface {
	Sign(req *http.Request, body []byte) error
}

// Doer sends a request. Callers pass a client that refuses redirects (provider.NoRedirectClient) or a fake.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client sends the calls of one REST protocol to one fixed endpoint. The host is the endpoint's, never a
// value of the request.
type Client struct {
	HTTP   Doer
	Signer Signer
	Codec  Codec
	// URL is the https endpoint from the endpoint table, for example https://email.eu-central-1.amazonaws.com.
	URL string
	// UserAgent is sent when set. It is not signed.
	UserAgent string
	// ResponseLimit caps the answer in bytes; zero means DefaultResponseLimit.
	ResponseLimit int64
	// BucketHost addresses a request with a bucket (Request.Bucket) as <bucket>.<endpoint host> when the name
	// is a single DNS label, and path-style on the endpoint host otherwise. The host is built from the
	// checked name and the fixed endpoint only.
	BucketHost bool
	// StreamSigner signs a request with a streamed payload, whose hash is not known beforehand: for S3 a
	// signer with UnsignedPayload. Without it a streamed upload is refused.
	StreamSigner Signer
}

// Meta is what the answer says about the exchange itself.
type Meta struct {
	// RequestID is AWS's request ID, kept only when it is a well-formed token.
	RequestID string
	// RetryAfter is the pause a rate-limited answer asked for; zero otherwise.
	RetryAfter time.Duration
	// Truncated is true when a decoded string was cut at its cap.
	Truncated bool
	// Size and SHA256 describe a streamed payload that was sent or received completely: its length and the
	// lowercase hex SHA-256 of what passed.
	Size   int64
	SHA256 string
}

var requestID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

func safeID(s string) string {
	if requestID.MatchString(s) {
		return s
	}
	return ""
}

// Do sends req, an encoded input of o, and decodes the answer into out (a pointer to a map; nil skips the
// result). op names the operation in errors. The returned error is nil on success; test it with != nil.
// Every 2xx status is a success; any other status goes through awserr, which maps the codes the models
// define and answers invalid-provider-response for a client error code it does not know.
func (c *Client) Do(ctx context.Context, op string, o *Operation, req Request, out *map[string]any) (Meta, *provider.Error) {
	endpoint, err := url.Parse(c.URL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return Meta{}, provider.Fail(op, "the AWS endpoint is not an https URL")
	}
	if (o.StreamsOutput() && req.Sink == nil) || (req.Sink != nil && !o.StreamsOutput()) ||
		(req.Stream != nil && (!o.StreamsInput() || c.StreamSigner == nil || len(req.Body) > 0)) {
		return Meta{}, provider.Fail(op, "the streamed payload does not fit the operation")
	}
	target := url.URL{Scheme: "https", Host: endpoint.Host, Path: req.Path, RawPath: req.RawPath, RawQuery: req.RawQuery}
	if c.BucketHost && req.Bucket != "" && VirtualHostable(req.Bucket) {
		prefix := "/" + req.Bucket
		rest, okRaw := strings.CutPrefix(req.RawPath, prefix)
		plain, okPath := strings.CutPrefix(req.Path, prefix)
		if !okRaw || !okPath || (rest != "" && rest[0] != '/') || (plain != "" && plain[0] != '/') {
			return Meta{}, provider.Fail(op, "the AWS request could not be addressed")
		}
		target.Host = req.Bucket + "." + endpoint.Host
		target.Path, target.RawPath = orSlash(plain), orSlash(rest)
	}
	var reader io.Reader = http.NoBody
	if len(req.Body) > 0 {
		reader = bytes.NewReader(req.Body)
	}
	var sent *meter
	if req.Stream != nil && req.Stream.Size > 0 {
		sent = newMeter(req.Stream.Reader, req.Stream.Size)
		reader = sent
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, target.String(), reader)
	if err != nil {
		return Meta{}, provider.Fail(op, "the AWS request could not be built")
	}
	signer := c.Signer
	if req.Stream != nil {
		httpReq.ContentLength = req.Stream.Size
		signer = c.StreamSigner
	}
	if req.Sink != nil {
		// Asked for explicitly, so the transport does not decompress and the length stays the object's.
		httpReq.Header.Set("Accept-Encoding", "identity")
	}
	for name, values := range req.Header {
		httpReq.Header[name] = append([]string(nil), values...)
	}
	httpReq.Header.Set("Accept", c.Codec.Accept())
	if c.UserAgent != "" {
		httpReq.Header.Set("User-Agent", c.UserAgent)
	}
	if err := signer.Sign(httpReq, req.Body); err != nil {
		return Meta{}, provider.Fail(op, "the AWS request could not be signed")
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return Meta{}, provider.Transport(op, awserr.Subject, err)
	}
	defer resp.Body.Close()
	meta := Meta{RequestID: safeID(resp.Header.Get("X-Amzn-Requestid"))}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body := provider.ReadErrorBody(resp.Body)
		code := c.Codec.ErrorCode(resp.Header, body)
		errType := ""
		if t, ok := c.Codec.(ErrorTyper); ok {
			errType = t.ErrorType(resp.Header, body)
		}
		failure, hold := c.classify(op, resp.StatusCode, resp.Header, code, errType)
		if m, ok := c.Codec.(ErrorMessenger); ok && failure.Original != nil && code != "" {
			var entries []provider.OriginalEntry
			for _, text := range m.ErrorMessages(code, body) {
				entries = append(entries, provider.OriginalEntry{Type: errType, Code: code, Message: text})
			}
			if len(entries) > 0 {
				failure.Original.Entries = entries
			}
		}
		meta.RetryAfter = hold
		return meta, failure
	}
	if req.Stream != nil {
		// Whatever the answer says, an upload that was not read as announced is not a success.
		switch {
		case sent != nil && (sent.n != req.Stream.Size || sent.err != nil):
			return meta, provider.Fail(op, "the payload was not sent as announced")
		case sent != nil:
			meta.Size, meta.SHA256 = sent.n, sent.sum()
		default:
			meta.SHA256 = newMeter(nil, 0).sum()
		}
	}
	if req.Sink != nil {
		return c.download(op, o, resp, req.Sink, meta, out)
	}
	limit := c.ResponseLimit
	if limit <= 0 {
		limit = DefaultResponseLimit
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return meta, provider.InvalidResponse(op, "the AWS response could not be read within the size limit")
	}
	if d, ok := c.Codec.(SuccessErrorDetector); ok && o.DocumentBody() {
		if code, errType, found := d.SuccessError(data); found {
			failure, hold := c.classify(op, resp.StatusCode, resp.Header, code, errType)
			meta.RetryAfter = hold
			return meta, failure
		}
	}
	decoded, cut, err := o.Decode(c.Codec, resp.StatusCode, resp.Header, data)
	if errors.Is(err, ErrShape) {
		return meta, provider.Fail(op, "the AWS result description is invalid")
	}
	if err != nil {
		return meta, provider.InvalidResponse(op, "AWS returned an invalid response")
	}
	meta.Truncated = cut
	if out != nil {
		*out = decoded
	}
	return meta, nil
}

func (c *Client) classify(op string, status int, header http.Header, code, errType string) (*provider.Error, time.Duration) {
	if cl, ok := c.Codec.(ErrorClassifier); ok {
		return cl.ClassifyError(op, status, header, code, errType)
	}
	return awserr.ClassifyStrict(op, status, header, code, errType)
}

func orSlash(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

// download copies the payload of an answer into the sink. The answer must state its length, at most
// MaxStreamBytes, and the sink is told before the first byte is written. Fewer or more bytes than stated, or a
// checksum the answer states and the content lacks, is a failure: the caller then discards the sink, so a
// partial payload is never a result. The members of the answer other than the payload go to out.
func (c *Client) download(op string, o *Operation, resp *http.Response, sink Sink, meta Meta, out *map[string]any) (Meta, *provider.Error) {
	length := resp.ContentLength
	switch {
	case length < 0:
		return meta, provider.InvalidResponse(op, "AWS returned the payload without its length")
	case length > MaxStreamBytes:
		return meta, provider.Fail(op, "the payload is larger than the limit of 100 MiB; nothing was written")
	}
	if err := sink.ExpectSize(length); err != nil {
		return meta, provider.Fail(op, "the destination refused the payload size")
	}
	if want := statedSHA256(resp.Header); want != "" {
		if e, ok := sink.(shaExpecter); ok {
			if err := e.ExpectSHA256(want); err != nil {
				return meta, provider.Fail(op, "the destination refused the stated checksum")
			}
		}
	}
	m := newMeter(resp.Body, length)
	n, err := io.Copy(struct{ io.Writer }{sink}, m)
	switch {
	case errors.Is(err, errLonger):
		return meta, provider.InvalidResponse(op, "AWS returned more payload than it announced")
	case err != nil:
		return meta, provider.Fail(op, "the payload could not be received completely")
	case n != length:
		return meta, provider.InvalidResponse(op, "AWS returned less payload than it announced")
	}
	meta.Size, meta.SHA256 = n, m.sum()
	if want := statedSHA256(resp.Header); want != "" && want != meta.SHA256 {
		return meta, provider.InvalidResponse(op, "the payload does not match the checksum AWS stated")
	}
	decoded, _, err := o.Decode(c.Codec, resp.StatusCode, resp.Header, nil)
	if err != nil {
		return meta, provider.InvalidResponse(op, "AWS returned an invalid response")
	}
	if out != nil {
		*out = decoded
	}
	return meta, nil
}

// statedSHA256 is the SHA-256 of the whole object as hex when the answer states one in x-amz-checksum-sha256:
// exactly 32 bytes of base64. The checksum of a multipart object is a checksum of checksums and carries a
// suffix; it states nothing about the content and is ignored, and so is the checksum of an answer to a range request.
func statedSHA256(h http.Header) string {
	if h.Get("Content-Range") != "" {
		// A range or a part is not the object the checksum covers.
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(h.Get("X-Amz-Checksum-Sha256"))
	if err != nil || len(raw) != sha256.Size {
		return ""
	}
	return hex.EncodeToString(raw)
}
