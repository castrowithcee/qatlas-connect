package accessrequest

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"slices"
	"sort"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-kit/configfile"
)

// Queue is the file of open requests. Every change runs under a cross-process lock on the file's current
// revision, so two writers never lose each other's request. A file that exists but cannot be read is never
// replaced: the error is a *config.InvalidError.
type Queue struct {
	path  string
	store *configfile.Store[document]
}

// NewQueue returns the queue stored in the file at path. Nothing is created until the first request.
func NewQueue(path string) (*Queue, error) {
	store, err := newStore(path)
	if err != nil {
		return nil, err
	}
	return &Queue{path: store.Path(), store: store}, nil
}

// Path is the queue file.
func (q *Queue) Path() string { return q.path }

func (q *Queue) invalid(err error) error { return &config.InvalidError{Path: q.path, Err: err} }

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func stamp(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

// prune removes what expired or became obsolete and returns the events for it. A request whose connection
// was renamed takes the new name. The returned document shares no slice with d.
func (e Env) prune(d document) (document, []Event) {
	var events []Event
	names := map[string]string{}
	for name, conn := range e.Config.Connections {
		if conn.ID != "" {
			names[conn.ID] = name
		}
	}
	out := document{Version: d.Version}
	for _, r := range d.Requests {
		name, found := names[r.ConnectionID]
		switch {
		case !e.Now.Before(r.ExpiresAt):
			for _, tool := range r.Tools {
				events = append(events, Event{EventExpire, r.ConnectionID, r.Connection, tool})
			}
			continue
		case !found:
			for _, tool := range r.Tools {
				events = append(events, Event{EventDrop, r.ConnectionID, r.Connection, tool})
			}
			continue
		}
		provider := e.Config.Services[e.Config.Connections[name].Service].Provider
		var kept []string
		for _, tool := range r.Tools {
			if e.alive(name, provider, tool) {
				kept = append(kept, tool)
			} else {
				events = append(events, Event{EventDrop, r.ConnectionID, name, tool})
			}
		}
		if len(kept) == 0 {
			continue
		}
		r.Tools, r.Connection = kept, name
		out.Requests = append(out.Requests, r)
	}
	return out, events
}

// Open returns every open request, newest first, after it removed the expired and obsolete ones and
// persisted that. The events say what was removed. A missing file means no request.
func (q *Queue) Open(env Env) ([]Request, []Event, error) {
	doc, _, err := q.store.LoadVersioned()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil, nil
	case err != nil:
		return nil, nil, q.invalid(err)
	}
	pruned, events := env.prune(doc)
	if len(events) > 0 {
		// Persist under the lock, from the file as it is then; the events are those of that run.
		if _, err := q.store.Update(func(d document) (document, error) {
			pruned, events = env.prune(d)
			return pruned, nil
		}); err != nil {
			return nil, nil, q.invalid(err)
		}
	}
	requests := slices.Clone(pruned.Requests)
	sort.SliceStable(requests, func(i, j int) bool { return requests[i].RequestedAt.After(requests[j].RequestedAt) })
	return requests, events, nil
}

// Add judges a request and stores it, merged into the open request of the same connection if there is one.
// Nothing is written when a tool is not requestable. The merged request takes the new tools, the newest
// reason and project, and a new expiry; tools requested before stay once.
func (q *Queue) Add(env Env, in Input) (Outcome, error) {
	tools, err := Check(env, in.Connection, in.Tools)
	if err != nil {
		return Outcome{}, err
	}
	id, err := newID()
	if err != nil {
		return Outcome{}, err
	}
	var out Outcome
	_, err = q.store.Update(func(d document) (document, error) {
		next, events := env.prune(d)
		now := stamp(env.Now)
		provider := env.Config.Services[env.Config.Connections[in.Connection].Service].Provider
		at := -1
		for i, r := range next.Requests {
			if r.ConnectionID == in.ConnectionID {
				at = i
			}
		}
		var req Request
		var added []string
		if at >= 0 {
			req = next.Requests[at]
			req.Tools = slices.Clone(req.Tools)
		} else {
			req = Request{ID: id, ConnectionID: in.ConnectionID, Provider: provider}
		}
		for _, tool := range tools {
			if !slices.Contains(req.Tools, tool) {
				req.Tools = append(req.Tools, tool)
				added = append(added, tool)
			}
		}
		sort.Strings(req.Tools)
		req.Connection, req.Reason, req.Project = in.Connection, in.Reason, in.Project
		req.RequestedAt, req.ExpiresAt = now, now.Add(Lifetime)
		if at >= 0 {
			next.Requests[at] = req
		} else {
			next.Requests = append(next.Requests, req)
		}
		for _, tool := range added {
			events = append(events, Event{EventCreate, in.ConnectionID, in.Connection, tool})
		}
		out = Outcome{Request: req, New: added, Events: events}
		return next, nil
	})
	if err != nil {
		return Outcome{}, q.invalid(err)
	}
	return out, nil
}
