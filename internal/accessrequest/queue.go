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
		if r.Kind == KindCreateConnection {
			// It names no connection yet: it lapses when one of that name exists or the provider is gone.
			_, taken := e.Config.Connections[r.Connection]
			event := EventExpire
			if e.Now.Before(r.ExpiresAt) {
				if e.registered(r.Provider) && !taken {
					out.Requests = append(out.Requests, r)
					continue
				}
				event = EventDrop
			}
			for _, tool := range r.Tools {
				events = append(events, Event{eventFor(r.Kind, event), "", r.Connection, tool})
			}
			continue
		}
		name, found := names[r.ConnectionID]
		switch {
		case !e.Now.Before(r.ExpiresAt):
			for _, tool := range r.Tools {
				events = append(events, Event{eventFor(r.Kind, EventExpire), r.ConnectionID, r.Connection, tool})
			}
			continue
		case !found:
			for _, tool := range r.Tools {
				events = append(events, Event{eventFor(r.Kind, EventDrop), r.ConnectionID, r.Connection, tool})
			}
			continue
		}
		provider := e.Config.Services[e.Config.Connections[name].Service].Provider
		var kept []string
		for _, tool := range r.Tools {
			if e.alive(r.Kind, name, provider, tool) {
				kept = append(kept, tool)
			} else {
				events = append(events, Event{eventFor(r.Kind, EventDrop), r.ConnectionID, name, tool})
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

// Add judges a request and stores it, merged into the open request of the same connection and kind if
// there is one.
// Nothing is written when a tool is not requestable. The merged request takes the new tools, the newest
// reason and project, and a new expiry; tools requested before stay once.
func (q *Queue) Add(env Env, in Input) (Outcome, error) {
	kind, err := ParseKind(string(in.Kind))
	if err != nil {
		return Outcome{}, err
	}
	if kind == KindCreateConnection {
		return q.addCreate(env, in)
	}
	tools, err := CheckKind(env, kind, in.Connection, in.Tools)
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
			if r.ConnectionID == in.ConnectionID && r.Kind == kind {
				at = i
			}
		}
		var req Request
		var added []string
		if at >= 0 {
			req = next.Requests[at]
			req.Tools = slices.Clone(req.Tools)
		} else {
			req = Request{ID: id, Kind: kind, ConnectionID: in.ConnectionID, Provider: provider}
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
			events = append(events, Event{eventFor(kind, EventCreate), in.ConnectionID, in.Connection, tool})
		}
		out = Outcome{Request: req, New: added, Events: events}
		return next, nil
	})
	if err != nil {
		return Outcome{}, q.invalid(err)
	}
	return out, nil
}

// ErrNotOpen is returned for a request ID that is not open: it never existed, was decided, expired, or lost
// every tool to the configuration.
var ErrNotOpen = errors.New("no such open request")

// Lookup returns the open request with the given ID as it stands against env, without writing anything:
// expired or obsolete tools are already left out of it (see Open) and listed in gone. A request that is
// not open, or is no more after that, is ErrNotOpen.
func (q *Queue) Lookup(env Env, id string) (req Request, gone []string, err error) {
	doc, _, err := q.store.LoadVersioned()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Request{}, nil, ErrNotOpen
	case err != nil:
		return Request{}, nil, q.invalid(err)
	}
	var raw Request
	for _, r := range doc.Requests {
		if r.ID == id {
			raw = r
		}
	}
	if raw.ID == "" {
		return Request{}, nil, ErrNotOpen
	}
	pruned, _ := env.prune(doc)
	for _, r := range pruned.Requests {
		if r.ID != id {
			continue
		}
		for _, tool := range raw.Tools {
			if !slices.Contains(r.Tools, tool) {
				gone = append(gone, tool)
			}
		}
		return r, gone, nil
	}
	return Request{}, nil, ErrNotOpen
}

// Settle removes the tools in done from the open request with the given ID, because a person decided them,
// and the tools in drop, because they are no longer requestable, and removes the request once it holds no
// tool. It first prunes like Open. The events are the pruned tools and the dropped ones that were still
// there; a decided tool has no event here, the caller logs the decision. A request that is not open is
// ErrNotOpen.
func (q *Queue) Settle(env Env, id string, done, drop []string) ([]Event, error) {
	var events []Event
	_, err := q.store.Update(func(d document) (document, error) {
		next, pruned := env.prune(d)
		events = pruned
		found := false
		for i := 0; i < len(next.Requests); i++ {
			r := next.Requests[i]
			if r.ID != id {
				continue
			}
			found = true
			var kept []string
			for _, tool := range r.Tools {
				switch {
				case slices.Contains(done, tool):
				case slices.Contains(drop, tool):
					events = append(events, Event{eventFor(r.Kind, EventDrop), r.ConnectionID, r.Connection, tool})
				default:
					kept = append(kept, tool)
				}
			}
			if len(kept) == 0 {
				next.Requests = slices.Delete(slices.Clone(next.Requests), i, i+1)
			} else {
				next.Requests = slices.Clone(next.Requests)
				r.Tools = kept
				next.Requests[i] = r
			}
			break
		}
		if !found {
			return d, ErrNotOpen
		}
		return next, nil
	})
	switch {
	case errors.Is(err, ErrNotOpen):
		return nil, ErrNotOpen
	case err != nil:
		return nil, q.invalid(err)
	}
	return events, nil
}

// addCreate stores a request to create a connection, merged into the open one of the same provider and
// requested name: tools and targets are united, the union is judged again, and the newest reason and project
// and a new expiry win.
func (q *Queue) addCreate(env Env, in Input) (Outcome, error) {
	if in.ConnectionID != "" {
		return Outcome{}, &InvalidError{"a request to create a connection names no existing connection"}
	}
	check := CreateInput{Provider: in.Provider, Name: in.Connection, Targets: in.Targets, Tools: in.Tools, Reason: in.ReasonRaw}
	if _, err := CheckCreate(env, check); err != nil {
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
		at := slices.IndexFunc(next.Requests, func(r Request) bool {
			return r.Kind == KindCreateConnection && r.Provider == in.Provider && r.Connection == in.Connection
		})
		req := Request{ID: id, Kind: KindCreateConnection, Provider: in.Provider, Connection: in.Connection}
		if at >= 0 {
			req = next.Requests[at]
		}
		united := CreateInput{Provider: in.Provider, Name: in.Connection,
			Targets: append(slices.Clone(req.Targets), in.Targets...), Tools: append(slices.Clone(req.Tools), in.Tools...)}
		united, err := CheckCreate(env, united)
		if err != nil {
			return d, err
		}
		var added []string
		for _, tool := range united.Tools {
			if !slices.Contains(req.Tools, tool) {
				added = append(added, tool)
			}
		}
		req.Tools, req.Targets = united.Tools, united.Targets
		req.Reason, req.Project = in.Reason, in.Project
		req.RequestedAt, req.ExpiresAt = now, now.Add(Lifetime)
		next.Requests = slices.Clone(next.Requests)
		if at >= 0 {
			next.Requests[at] = req
		} else {
			next.Requests = append(next.Requests, req)
		}
		for _, tool := range added {
			events = append(events, Event{eventFor(KindCreateConnection, EventCreate), "", in.Connection, tool})
		}
		out = Outcome{Request: req, New: added, Events: events}
		return next, nil
	})
	var invalid *InvalidError
	switch {
	case errors.As(err, &invalid):
		return Outcome{}, invalid
	case err != nil:
		return Outcome{}, q.invalid(err)
	}
	return out, nil
}
