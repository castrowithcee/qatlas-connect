package accessrequest

import (
	"errors"
	"slices"
)

// The refusals of CloseCreated besides ErrNotOpen.
var (
	// ErrConnectionMissing is returned when no connection of the given name exists.
	ErrConnectionMissing = errors.New("no connection of this name exists")
	// ErrProviderMismatch is returned when the connection belongs to another provider than the request.
	ErrProviderMismatch = errors.New("the connection belongs to another provider than the request")
)

// CloseCreated closes the whole request to create a connection with the given ID because a person saved the
// connection named connection, which may differ from the requested name. The connection must exist in
// env.Config and belong to the provider of the request. The request is taken out before pruning, so a
// connection saved under the requested name does not make it lapse (no drop event for it); the other
// requests are pruned as Open does. The events are those of the pruned requests. It returns the closed
// request. Anything but a request to create a connection is ErrNotOpen.
func (q *Queue) CloseCreated(env Env, id, connection string) (Request, []Event, error) {
	var closed Request
	var events []Event
	_, err := q.store.Update(func(d document) (document, error) {
		at := slices.IndexFunc(d.Requests, func(r Request) bool { return r.ID == id && r.Kind == KindCreateConnection })
		if at < 0 {
			return d, ErrNotOpen
		}
		closed = d.Requests[at]
		conn, ok := env.Config.Connections[connection]
		switch {
		case !ok:
			return d, ErrConnectionMissing
		case env.Config.Services[conn.Service].Provider != closed.Provider:
			return d, ErrProviderMismatch
		}
		rest := document{Version: d.Version, Requests: slices.Delete(slices.Clone(d.Requests), at, at+1)}
		next, pruned := env.prune(rest)
		events = pruned
		return next, nil
	})
	switch {
	case errors.Is(err, ErrNotOpen), errors.Is(err, ErrConnectionMissing), errors.Is(err, ErrProviderMismatch):
		return Request{}, nil, err
	case err != nil:
		return Request{}, nil, q.invalid(err)
	}
	return closed, events, nil
}
