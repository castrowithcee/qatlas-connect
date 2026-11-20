package manage

import (
	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// NewRequestSource returns the source of the open requests that belong to the configuration file at
// configPath: its queue file and every tool the registry holds. Surfaces pass it to Service.WithRequests.
func NewRequestSource(configPath string, reg *capability.Registry) (RequestSource, error) {
	queue, err := accessrequest.NewQueue(accessrequest.PathFor(configPath))
	if err != nil {
		return RequestSource{}, err
	}
	var tools []accessrequest.Tool
	for _, d := range reg.All() {
		tools = append(tools, accessrequest.Tool{ToolMetadata: d.Tool(), Provider: d.Provider})
	}
	return RequestSource{Queue: queue, Tools: tools}, nil
}
