package application

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/invokelog"
)

// SetRequestQueue installs the file of open tool requests that RequestTools and OpenToolRequests use. The
// CLI and the MCP broker pass it beside the invocation log. Without one, requests are not available.
func (c *Core) SetRequestQueue(queue *accessrequest.Queue) { c.requests = queue }

// ToolRequest asks for tools a connection does not offer. Tools are exact tool IDs or prefixes of the form
// <provider>.<segment>.*. Connection selects the connection like an invoke does and may be empty. Reason is
// untrusted text of the requester; the core cleans it before anything stores or shows it.
//
// Kind is accessrequest.KindAddTools (the default, when empty) to ask for tools the connection does not
// offer, or accessrequest.KindRemoveTools to ask that it stop offering tools it offers.
type ToolRequest struct {
	Kind       string   `json:"kind,omitempty"`
	Connection string   `json:"connection,omitempty"`
	Tools      []string `json:"tools"`
	Reason     string   `json:"reason,omitempty"`
}

// ToolRequestResponse describes the open request after the call: the connection, every tool the request
// now holds (earlier ones included when it was merged), its status, and when it expires.
type ToolRequestResponse struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Connection string    `json:"connection"`
	Tools      []string  `json:"tools"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// OpenToolRequest is one open request as a person reads it. Reason and Project are untrusted display text,
// cleaned and shortened.
type OpenToolRequest struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Connection   string   `json:"connection"`
	ConnectionID string   `json:"connection_id"`
	Provider     string   `json:"provider"`
	Tools        []string `json:"tools"`
	// Entries is Tools again, each with the group the provider sorts it into (empty for a provider without
	// groups), so a surface shows the requested tools grouped like the tool picker.
	Entries     []OpenToolEntry `json:"entries"`
	Reason      string          `json:"reason,omitempty"`
	Project     string          `json:"project,omitempty"`
	RequestedAt time.Time       `json:"requested_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

// OpenToolEntry is one tool or prefix of an open request with its group.
type OpenToolEntry struct {
	Tool       string `json:"tool"`
	Group      string `json:"group,omitempty"`
	GroupTitle string `json:"group_title,omitempty"`
}

func (c *Core) requestEnv() accessrequest.Env {
	env := accessrequest.Env{Config: c.all, Now: c.now()}
	for _, d := range c.registry.All() {
		env.Tools = append(env.Tools, accessrequest.Tool{ToolMetadata: d.Tool(), Provider: d.Provider})
	}
	return env
}

// RequestTools stores a request for tools of an existing connection of the current project, all or nothing.
// It selects the connection like Invoke, refuses a disabled or expired one the same way, and merges the
// request into the open one of the connection. The reason is cleaned like a confirmation summary: known
// secrets redacted, control characters escaped, at most 500 characters. It is never logged. One
// config.request.create entry per newly requested tool (config.request.remove.create for a request to
// remove tools) goes to the invocation log, and so does every request that expired or became obsolete on the way.
func (c *Core) RequestTools(request ToolRequest) (ToolRequestResponse, error) {
	c.scope()
	if c.requests == nil {
		return ToolRequestResponse{}, errors.New("tool requests are not available here")
	}
	if len(request.Tools) == 0 {
		return ToolRequestResponse{}, &InvalidRequestError{Message: "a request names at least one tool"}
	}
	kind, err := accessrequest.ParseKind(request.Kind)
	if err != nil {
		return ToolRequestResponse{}, &InvalidRequestError{Message: err.Error()}
	}
	env := c.requestEnv()
	name, err := c.requestConnection(env, request)
	if err != nil {
		return ToolRequestResponse{}, err
	}
	conn := c.config.Connections[name]
	if conn.ID == "" {
		return ToolRequestResponse{}, fmt.Errorf("connection %q has no id yet, so a request cannot refer to it", name)
	}
	if conn.Off(c.now()) {
		return ToolRequestResponse{}, &capability.UnsupportedError{
			Connection: name, Capability: c.requestCapability(env, request.Tools[0]), Reason: config.RefusalDisabled,
		}
	}
	project := ""
	if len(c.projects) > 0 {
		project = boundSummary(neutralize(c.projects[0]), 0)
	}
	outcome, err := c.requests.Add(env, accessrequest.Input{
		Kind: kind, Connection: name, ConnectionID: conn.ID, Tools: request.Tools,
		Reason: c.cleanReason(request.Reason), Project: project,
	})
	c.logRequestEvents(outcome.Events)
	if err != nil {
		var invalid *accessrequest.InvalidError
		if errors.As(err, &invalid) {
			return ToolRequestResponse{}, &InvalidRequestError{Message: invalid.Message}
		}
		return ToolRequestResponse{}, err
	}
	r := outcome.Request
	return ToolRequestResponse{
		ID: r.ID, Kind: string(r.Kind), Connection: r.Connection, Tools: r.Tools, Status: accessrequest.StatusOpen, ExpiresAt: r.ExpiresAt,
	}, nil
}

// OpenToolRequests returns every open request of every connection, newest first, for a person to decide.
// Unlike a request it is not limited to the current project. It first removes the expired and obsolete
// requests and logs that.
func (c *Core) OpenToolRequests() ([]OpenToolRequest, error) {
	if c.requests == nil {
		return nil, errors.New("tool requests are not available here")
	}
	requests, events, err := c.requests.Open(c.requestEnv())
	c.logRequestEvents(events)
	if err != nil {
		return nil, err
	}
	open := make([]OpenToolRequest, len(requests))
	for i, r := range requests {
		metadata, _ := c.all.ProviderMetadata(r.Provider)
		entries := make([]OpenToolEntry, len(r.Tools))
		for j, tool := range r.Tools {
			entries[j] = OpenToolEntry{Tool: tool}
			if group, ok := config.GroupOfEntry(metadata, tool); ok {
				entries[j].Group, entries[j].GroupTitle = group.ID, group.Title
			}
		}
		open[i] = OpenToolRequest{
			ID: r.ID, Kind: string(r.Kind), Connection: r.Connection, ConnectionID: r.ConnectionID, Provider: r.Provider,
			Tools: r.Tools, Entries: entries, Reason: c.cleanReason(r.Reason), Project: boundSummary(neutralize(r.Project), 0),
			RequestedAt: r.RequestedAt, ExpiresAt: r.ExpiresAt,
		}
	}
	return open, nil
}

// cleanReason is the confirmation summary's path for untrusted text: secrets redacted before the cut,
// control characters escaped, bounded.
func (c *Core) cleanReason(reason string) string {
	if c.redactor != nil {
		reason = c.redactor.Apply(reason)
	}
	return boundSummary(neutralize(reason), accessrequest.MaxReasonRunes)
}

// requestCapability is a tool ID for the diagnostic that names the request: the entry itself, or the first
// registered tool under a prefix.
func (c *Core) requestCapability(env accessrequest.Env, entry string) string {
	if !config.IsToolPrefix(entry) {
		return entry
	}
	stem := strings.TrimSuffix(entry, "*")
	for _, tool := range env.Tools {
		if strings.HasPrefix(tool.ID, stem) {
			return tool.ID
		}
	}
	return entry
}

// requestConnection selects the connection of a request. An explicit value is resolved by connectionName,
// so name, id, and name|id work and a connection of another project is unknown. Without one the default of
// an exact tool ID wins, then the default of the provider of the tools, then the one connection of that
// provider in this project; several candidates are ambiguous. Unlike an invoke it does not look at what a
// connection offers, because a request is about tools it does not.
func (c *Core) requestConnection(env accessrequest.Env, request ToolRequest) (string, error) {
	first := request.Tools[0]
	if request.Connection != "" {
		name, err := c.connectionName(request.Connection)
		if err != nil {
			provider, _ := accessrequest.ProviderOf(env.Tools, first)
			return "", unknownConnectionOf(err, provider, "")
		}
		return name, nil
	}
	providers := map[string]bool{}
	for _, entry := range request.Tools {
		provider, ok := accessrequest.ProviderOf(env.Tools, entry)
		if !ok {
			return "", &InvalidRequestError{Message: fmt.Sprintf("tool %q is not a registered tool", shortTool(entry))}
		}
		providers[provider] = true
	}
	if len(providers) != 1 {
		return "", &InvalidRequestError{Message: "the tools of one request belong to one provider"}
	}
	var provider string
	for provider = range providers {
	}
	defaults := map[string]bool{}
	for _, entry := range request.Tools {
		if name := c.config.Defaults.Connections[entry]; name != "" && !config.IsToolPrefix(entry) {
			defaults[name] = true
		}
	}
	if len(defaults) == 0 {
		if name := c.config.Defaults.Connections[provider]; name != "" {
			defaults[name] = true
		}
	}
	names := sortedSet(defaults)
	if len(names) == 0 {
		names = c.connectionNames(provider)
	}
	switch len(names) {
	case 0:
		return "", &ConnectionSelectionError{Operation: c.requestCapability(env, first)}
	case 1:
		if _, ok := c.config.Connections[names[0]]; !ok {
			return "", c.unknownConnection(names[0])
		}
		return names[0], nil
	}
	refs := make([]ConnectionRef, len(names))
	for i, name := range names {
		refs[i] = c.connectionRef(name)
		refs[i].ID = c.config.Connections[name].ID
	}
	return "", &ConnectionAmbiguousError{Operation: c.requestCapability(env, first), Connections: refs}
}

func shortTool(entry string) string {
	if r := []rune(entry); len(r) > 80 {
		return string(r[:79]) + "…"
	}
	return entry
}

var requestOperations = map[accessrequest.EventKind]string{
	accessrequest.EventCreate: invokelog.OperationRequestCreate,
	accessrequest.EventExpire: invokelog.OperationRequestExpire,
	accessrequest.EventDrop:   invokelog.OperationRequestDrop,

	accessrequest.EventRemoveCreate: invokelog.OperationRequestRemoveCreate,
	accessrequest.EventRemoveExpire: invokelog.OperationRequestRemoveExpire,
	accessrequest.EventRemoveDrop:   invokelog.OperationRequestRemoveDrop,
}

var requestEffects = map[accessrequest.EventKind]string{
	accessrequest.EventCreate: "create",
	accessrequest.EventExpire: "delete",
	accessrequest.EventDrop:   "delete",

	accessrequest.EventRemoveCreate: "create",
	accessrequest.EventRemoveExpire: "delete",
	accessrequest.EventRemoveDrop:   "delete",
}

// logRequestEvents appends one entry per tool event, with the connection and the tool and nothing the
// requester wrote. A failed append is a warning like that of an invoke.
func (c *Core) logRequestEvents(events []accessrequest.Event) {
	if c.invokeLog == nil {
		return
	}
	for _, event := range events {
		fields := invokelog.Fields{
			Path: c.invokePath, Client: c.invokeClient, Operation: requestOperations[event.Kind],
			Connection: event.Connection, ConnectionID: event.ConnectionID, Tool: event.Tool,
			Effect: requestEffects[event.Kind], Result: "success",
		}
		if warning := InvokeLogWarning(c.invokeLog.Append(fields)); warning != "" {
			c.writeAuditText(warning)
		}
	}
}
