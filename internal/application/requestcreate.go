package application

import (
	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
)

// createRequestMessage tells the requester what an open request to create a connection means for it.
const createRequestMessage = "The request is open and nothing was created. Stop now and tell the user that you asked " +
	"for a new connection, with the name, tools and targets you named. The user sets it up and chooses the " +
	"credential. Search again later to see whether the tools are offered."

// requestCreateConnection stores a request that a person set up a connection. It selects no connection and
// writes neither the configuration nor a credential. The redactor judges the name, targets, tools and the
// reason as the requester wrote them, and any hit refuses the whole request.
func (c *Core) requestCreateConnection(env accessrequest.Env, request ToolRequest) (ToolRequestResponse, error) {
	if request.Connection != "" {
		return ToolRequestResponse{}, &InvalidRequestError{Message: "connection does not apply to a request to create a connection; " +
			"name the wished connection in name"}
	}
	r, err := c.addRequest(env, accessrequest.Input{
		Kind: accessrequest.KindCreateConnection, Connection: request.Name, Provider: request.Provider,
		Targets: request.Targets, Tools: request.Tools,
		Reason: c.cleanReason(request.Reason), ReasonRaw: request.Reason,
	})
	if err != nil {
		return ToolRequestResponse{}, err
	}
	return ToolRequestResponse{
		ID: r.ID, Kind: string(r.Kind), Connection: r.Connection, Provider: r.Provider, Targets: r.Targets, Tools: r.Tools,
		Status: accessrequest.StatusOpen, ExpiresAt: r.ExpiresAt, Message: createRequestMessage,
	}, nil
}
