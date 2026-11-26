// Package accessrequest keeps the open tool requests of agents in their own file beside the configuration.
//
// A request names tools that a connection does not offer. It changes nothing by itself: it waits there until
// a person decides. The file holds no secret, is never exported, and its reasons are untrusted text that the
// caller has already cleaned. The package judges what may be requested, merges repeated requests, lets them
// expire, and drops what the configuration made obsolete. It reports what happened as events and logs
// nothing itself.
package accessrequest

import (
	"fmt"
	"time"
)

const (
	// FileName is the name of the queue file, beside the configuration file.
	FileName = "requests.yaml"
	// Lifetime is how long a request stays open after its last request.
	Lifetime = 14 * 24 * time.Hour
	// MaxTools bounds the tools of one call.
	MaxTools = 50
	// MaxReasonRunes bounds a cleaned reason.
	MaxReasonRunes = 500
	// StatusOpen is the only status a stored request has: a decision removes it.
	StatusOpen = "open"

	fileVersion = 1
)

// Kind says what a request asks for.
type Kind string

const (
	// KindAddTools asks that a connection offer tools it does not. It is the default and is never written to
	// the file, so a file without another kind stays readable by a Connect that predates kinds.
	KindAddTools Kind = "add-tools"
	// KindRemoveTools asks that a connection stop offering tools it offers now. A file holding one carries a
	// field an older Connect does not know, which it refuses to read instead of misreading it.
	KindRemoveTools Kind = "remove-tools"
)

// ParseKind returns the kind a caller wrote: empty is KindAddTools, an unknown value is *InvalidError.
func ParseKind(s string) (Kind, error) {
	switch Kind(s) {
	case "", KindAddTools:
		return KindAddTools, nil
	case KindRemoveTools:
		return KindRemoveTools, nil
	}
	return "", &InvalidError{fmt.Sprintf("unknown request kind %q; use %q or %q", shorten(s), KindAddTools, KindRemoveTools)}
}

// Request is one open request. Connection is the name at the time of the last write; the ID of the
// connection decides, so a renamed connection keeps its request.
type Request struct {
	ID           string    `yaml:"id" json:"id"`
	Kind         Kind      `yaml:"kind,omitempty" json:"kind"`
	ConnectionID string    `yaml:"connection_id" json:"connection_id"`
	Connection   string    `yaml:"connection" json:"connection"`
	Provider     string    `yaml:"provider" json:"provider"`
	Tools        []string  `yaml:"tools" json:"tools"`
	Reason       string    `yaml:"reason,omitempty" json:"reason,omitempty"`
	Project      string    `yaml:"project,omitempty" json:"project,omitempty"`
	RequestedAt  time.Time `yaml:"requested_at" json:"requested_at"`
	ExpiresAt    time.Time `yaml:"expires_at" json:"expires_at"`
}

// Input is a request to add: the connection already resolved, the tools as the agent wrote them, and the
// cleaned reason and project path.
type Input struct {
	// Kind is empty for KindAddTools.
	Kind         Kind
	Connection   string
	ConnectionID string
	Tools        []string
	Reason       string
	Project      string
}

// EventKind says what happened to one tool of a request.
type EventKind string

// The kinds of event, in the order of the log operations they map to. The Remove events are those of a
// request of KindRemoveTools; a decision is no event here.
const (
	EventCreate EventKind = "create"
	EventExpire EventKind = "expire"
	EventDrop   EventKind = "drop"

	EventRemoveCreate EventKind = "remove-create"
	EventRemoveExpire EventKind = "remove-expire"
	EventRemoveDrop   EventKind = "remove-drop"
)

// eventFor returns the event of the request kind that corresponds to event.
func eventFor(kind Kind, event EventKind) EventKind {
	if kind != KindRemoveTools {
		return event
	}
	return "remove-" + event
}

// Event is one tool that was requested, expired, or dropped, with its connection.
type Event struct {
	Kind         EventKind
	ConnectionID string
	Connection   string
	Tool         string
}

// Outcome is what adding a request produced: the open request after merging, and the events to log.
type Outcome struct {
	Request Request
	// New lists the tools this call added to the request.
	New    []string
	Events []Event
}

type document struct {
	Version  int       `yaml:"version"`
	Requests []Request `yaml:"requests,omitempty"`
}
