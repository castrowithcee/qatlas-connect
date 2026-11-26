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
	// MaxTargets bounds the targets of a request to create a connection.
	MaxTargets = 20
	// MaxTargetRunes bounds one target of such a request.
	MaxTargetRunes = 200
	// MaxNameRunes bounds the requested name of a connection.
	MaxNameRunes = 64
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
	// KindCreateConnection asks that a person set up a connection of a provider under a name with targets and
	// tools. It names no existing connection (ConnectionID is empty, Connection is the requested name) and
	// carries no credential, service, or secret. A file holding one carries fields an older Connect does not
	// know, which it refuses to read.
	KindCreateConnection Kind = "create-connection"
)

// ParseKind returns the kind a caller wrote: empty is KindAddTools, an unknown value is *InvalidError.
func ParseKind(s string) (Kind, error) {
	switch Kind(s) {
	case "", KindAddTools:
		return KindAddTools, nil
	case KindRemoveTools:
		return KindRemoveTools, nil
	case KindCreateConnection:
		return KindCreateConnection, nil
	}
	return "", &InvalidError{fmt.Sprintf("unknown request kind %q; use %q, %q or %q", shorten(s), KindAddTools, KindRemoveTools, KindCreateConnection)}
}

// Request is one open request. Connection is the name at the time of the last write; the ID of the
// connection decides, so a renamed connection keeps its request. For KindCreateConnection there is no
// connection yet: ConnectionID is empty, Connection is the requested name, and Targets are the requested
// targets.
type Request struct {
	ID           string    `yaml:"id" json:"id"`
	Kind         Kind      `yaml:"kind,omitempty" json:"kind"`
	ConnectionID string    `yaml:"connection_id,omitempty" json:"connection_id"`
	Connection   string    `yaml:"connection" json:"connection"`
	Provider     string    `yaml:"provider" json:"provider"`
	Targets      []string  `yaml:"targets,omitempty" json:"targets,omitempty"`
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
	Kind Kind
	// Connection is the resolved connection, or the requested name for KindCreateConnection.
	Connection   string
	ConnectionID string
	// Provider and Targets are only for KindCreateConnection.
	Provider string
	Targets  []string
	Tools    []string
	Reason   string
	// ReasonRaw is the reason as the requester wrote it, which KindCreateConnection judges for a known
	// secret; Reason is the cleaned text that is stored.
	ReasonRaw string
	Project   string
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

	EventCreateConnectionCreate EventKind = "create-connection-create"
	EventCreateConnectionExpire EventKind = "create-connection-expire"
	EventCreateConnectionDrop   EventKind = "create-connection-drop"
)

// eventFor returns the event of the request kind that corresponds to event.
func eventFor(kind Kind, event EventKind) EventKind {
	switch kind {
	case KindRemoveTools:
		return "remove-" + event
	case KindCreateConnection:
		return "create-connection-" + event
	}
	return event
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
