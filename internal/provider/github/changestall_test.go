package github

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// changeStall holds a request until the client of its call has given up and ends that call with a deadline
// once the request has arrived, so the outcome never depends on how fast a loaded host runs the calls that
// come before the change. A held request never gets an HTTP answer: its connection is closed without one.
type changeStall struct {
	release chan struct{}
	mu      sync.Mutex
	call    *stalledCall
}

// stalledCall is the state of one call, so a request can neither end nor outlast a call other than its own.
type stalledCall struct {
	arrived chan struct{} // closed once a request of the call arrived, or the guard minute ran out
	ended   chan struct{} // closed once the client of the call has returned
	arrive  func()
	end     func()
}

func newChangeStall(t *testing.T) *changeStall {
	t.Helper()
	s := &changeStall{release: make(chan struct{})}
	t.Cleanup(func() { close(s.release) })
	return s
}

func (s *changeStall) answer(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	call := s.call
	s.mu.Unlock()
	// A request outside a call holds nothing up; it is dropped at once.
	if call != nil {
		call.arrive()
		select {
		case <-call.ended:
		case <-s.release:
		}
	}
	drop(w)
}

// route is answer for a handler that reports whether it took the request.
func (s *changeStall) route(w http.ResponseWriter, r *http.Request) bool {
	s.answer(w, r)
	return true
}

// drop closes the connection of a held request without writing a response. A handler that just returned
// would send an empty 200, which a client still waiting would read as success.
func drop(w http.ResponseWriter) {
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		conn.Close()
		return
	}
	panic(http.ErrAbortHandler)
}

// expiredOnArrival is a context that ends with context.DeadlineExceeded, the error of a client timeout, as
// soon as the stalled request has arrived; the parent's minute only guards against a hang.
type expiredOnArrival struct {
	context.Context
	expired <-chan struct{}
}

func (c *expiredOnArrival) Done() <-chan struct{} { return c.expired }

func (c *expiredOnArrival) Err() error {
	if err := c.Context.Err(); err != nil {
		return err
	}
	select {
	case <-c.expired:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// context returns the client context of one call. Its cancel function ends the call and lets the requests
// held for it go without an answer.
func (s *changeStall) context() (context.Context, context.CancelFunc) {
	parent, cancel := context.WithTimeout(context.Background(), time.Minute)
	call := &stalledCall{arrived: make(chan struct{}), ended: make(chan struct{})}
	call.arrive = sync.OnceFunc(func() { close(call.arrived) })
	call.end = sync.OnceFunc(func() { close(call.ended) })
	stop := context.AfterFunc(parent, call.arrive)
	s.mu.Lock()
	s.call = call
	s.mu.Unlock()
	return &expiredOnArrival{Context: parent, expired: call.arrived}, func() {
		stop()
		cancel()
		call.arrive()
		s.mu.Lock()
		if s.call == call {
			s.call = nil
		}
		s.mu.Unlock()
		call.end()
	}
}
