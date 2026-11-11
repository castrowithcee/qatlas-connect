package httpbind

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
)

// MaxStreamBytes is the most one call may upload or download as a stream: 100 MiB, exactly this many allowed.
const MaxStreamBytes int64 = 100 << 20

// StreamBody is the payload of an upload: a reader of exactly Size bytes, such as a local file opened for
// upload. It is read once, as the request is sent, so the call is never repeated and its payload hash is
// UNSIGNED-PAYLOAD over HTTPS; the SHA-256 of what was sent is reported in Meta.
type StreamBody struct {
	Reader io.Reader
	Size   int64
}

// Sink receives the payload of a download, such as a local file created for download. The client announces
// the size the answer states before it writes, writes at most that many bytes and reports success only when
// all arrived. On any failure the caller discards the sink; it never keeps a half file as a result.
// A sink may also implement ExpectSHA256(hex string) error, which the client calls with the SHA-256 the
// answer states when it states one.
type Sink interface {
	io.Writer
	ExpectSize(size int64) error
}

type shaExpecter interface{ ExpectSHA256(sum string) error }

var (
	errStreamSize = errors.New("httpbind: the stream is larger than the limit or has no reader")
	errStreamRole = errors.New("httpbind: the operation has no streaming payload of this direction")
)

// StreamsInput reports whether the operation uploads its payload as a stream, StreamsOutput whether it
// downloads it as one.
func (o *Operation) StreamsInput() bool  { return streams(o, true) }
func (o *Operation) StreamsOutput() bool { return streams(o, false) }

func streams(o *Operation, input bool) bool {
	prep, err := o.prepare()
	if err != nil {
		return false
	}
	top := prep.out
	if input {
		top = prep.in
	}
	return top.payload != nil && top.payload.Stream
}

// SetStream attaches the payload of an upload to an encoded request of o. The size limit is checked here,
// before anything is sent.
func (r *Request) SetStream(o *Operation, s StreamBody) error {
	if !o.StreamsInput() || r.Stream != nil || len(r.Body) > 0 {
		return shapeErr("%v", errStreamRole)
	}
	if s.Reader == nil || s.Size < 0 || s.Size > MaxStreamBytes {
		return valueErr("the payload is larger than the limit of %d bytes or has no reader", MaxStreamBytes)
	}
	r.Stream = &s
	if r.Header.Get("Content-Type") == "" {
		r.Header.Set("Content-Type", "application/octet-stream")
	}
	return nil
}

// SetSink names the destination of the payload of a download of o.
func (r *Request) SetSink(o *Operation, s Sink) error {
	if !o.StreamsOutput() || r.Sink != nil || s == nil {
		return shapeErr("%v", errStreamRole)
	}
	r.Sink = s
	return nil
}

// meter reads or writes through a hash and counts, and refuses more than limit bytes.
type meter struct {
	r     io.Reader
	hash  hash.Hash
	n     int64
	limit int64
	err   error
}

func newMeter(r io.Reader, limit int64) *meter {
	return &meter{r: r, hash: sha256.New(), limit: limit}
}

var errLonger = errors.New("httpbind: the stream is longer than announced")

func (m *meter) Read(p []byte) (n int, err error) {
	defer func() {
		if err != nil && err != io.EOF {
			m.err = err
		}
	}()
	if m.n >= m.limit {
		// One more byte would exceed the announced size.
		var probe [1]byte
		k, rerr := m.r.Read(probe[:])
		if k > 0 {
			return 0, errLonger
		}
		return 0, rerr
	}
	if int64(len(p)) > m.limit-m.n {
		p = p[:m.limit-m.n]
	}
	n, err = m.r.Read(p)
	m.hash.Write(p[:n])
	m.n += int64(n)
	return n, err
}

func (m *meter) sum() string { return hex.EncodeToString(m.hash.Sum(nil)) }
