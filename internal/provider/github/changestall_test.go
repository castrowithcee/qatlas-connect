package github

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// changeStall holds a request until the test ends or the client has gone, and ends the client's call with a
// deadline once the request has arrived, so the outcome never depends on how fast a loaded host runs the
// calls that come before the change.
type changeStall struct {
	entered chan struct{}
	release chan struct{}
}

func newChangeStall(t *testing.T) *changeStall {
	t.Helper()
	s := &changeStall{entered: make(chan struct{}, 1), release: make(chan struct{})}
	t.Cleanup(func() { close(s.release) })
	return s
}

func (s *changeStall) answer(_ http.ResponseWriter, r *http.Request) {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
	case <-r.Context().Done():
	}
}

// route is answer for a handler that reports whether it took the request.
func (s *changeStall) route(w http.ResponseWriter, r *http.Request) bool {
	s.answer(w, r)
	return true
}

// expiredOnArrival is a context that ends with context.DeadlineExceeded, the error of a client timeout, as
// soon as the stalled request has arrived; the parent's minute only guards against a hang.
type expiredOnArrival struct {
	context.Context
	expired chan struct{}
	once    sync.Once
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

// context returns the client context of one call.
func (s *changeStall) context() (context.Context, context.CancelFunc) {
	parent, cancel := context.WithTimeout(context.Background(), time.Minute)
	c := &expiredOnArrival{Context: parent, expired: make(chan struct{})}
	end := func() { c.once.Do(func() { close(c.expired) }) }
	go func() {
		select {
		case <-s.entered:
		case <-parent.Done():
		}
		end()
	}()
	return c, func() { cancel(); end() }
}
