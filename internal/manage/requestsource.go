package manage

import (
	"github.com/castrowithcee/qatlas-connect/internal/accessrequest"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// NewRequestSource opens the request queue beside the configuration file at configPath and lists every tool
// that reg registers, which is what listing and deciding requests need besides the configuration.
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
