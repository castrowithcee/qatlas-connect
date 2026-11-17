package googletagmanager

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// clientBody holds the members of a client a caller can set.
type clientBody struct {
	Name           string      `json:"name,omitempty"`
	Type           string      `json:"type,omitempty"`
	Parameter      []parameter `json:"parameter,omitempty"`
	Priority       *int32      `json:"priority,omitempty"`
	Notes          string      `json:"notes,omitempty"`
	ParentFolderID string      `json:"parentFolderId,omitempty"`
}

// clientEntity is a Client of the API: the members of clientBody and those Google assigns.
type clientEntity struct {
	Path          idText  `json:"path,omitempty"`
	AccountID     idText  `json:"accountId,omitempty"`
	ContainerID   idText  `json:"containerId,omitempty"`
	WorkspaceID   idText  `json:"workspaceId,omitempty"`
	ClientID      idText  `json:"clientId,omitempty"`
	Fingerprint   idText  `json:"fingerprint,omitempty"`
	TagManagerURL urlText `json:"tagManagerUrl,omitempty"`
	clientBody
}

type (
	clientsAnswer struct {
		Client        []clientEntity `json:"client"`
		NextPageToken string         `json:"nextPageToken"`
	}
	clientsResult struct {
		Clients    []clientEntity `json:"clients"`
		NextCursor string         `json:"next_cursor,omitempty"`
	}
	revertClientAnswer struct {
		Client *clientEntity `json:"client"`
	}
	revertClientResult struct {
		Client *clientEntity `json:"client,omitempty"`
	}
	deleteClientResult struct {
		Client  string `json:"client"`
		Deleted bool   `json:"deleted"`
	}
)

// checkInput refuses a client the schema would refuse as well, for a handler reached without the schema.
func (b clientBody) checkInput() error {
	if err := checkTrees(b.Parameter); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(b.Name) == "":
		return invalidArgument("name is required")
	case b.Type == "":
		return invalidArgument("type is required")
	}
	return nil
}

// checkAnswer refuses an answer that holds more parameter entries than a request may carry back.
func (c clientEntity) checkAnswer(op string) error {
	if countNodes(c.Parameter) > maxParamNodes {
		return provider.InvalidResponse(op, subject+" returned a client with more than "+strconv.Itoa(maxParamNodes)+
			" parameter entries")
	}
	return nil
}

// withoutPriority drops the priority member: the shared call arguments read it as a tag priority, which is a
// parameter, while a client priority is an integer. The client body reads it from the raw arguments.
func withoutPriority(raw json.RawMessage) json.RawMessage {
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return raw
	}
	delete(members, "priority")
	out, err := json.Marshal(members)
	if err != nil {
		return raw
	}
	return out
}

const prioritySchema = `{"type":"integer","minimum":-2147483648,"maximum":2147483647}`

func clientMembers(input bool) []string {
	name, typ := stringSchema, stringSchema
	if input {
		name, typ = `{"type":"string","minLength":1}`, `{"type":"string","minLength":1}`
	}
	priority := `{"type":"integer"}`
	if input {
		priority = prioritySchema
	}
	return []string{member("name", name), member("type", typ),
		member("parameter", `{"type":"array","items":`+parameterSchema(maxParamDepth, input)+`}`),
		member("priority", priority), member("notes", stringSchema), member("parentFolderId", stringSchema)}
}

var clientSchema = objectSchema(append([]string{member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("workspaceId", stringSchema), member("clientId", stringSchema),
	member("fingerprint", stringSchema), member("tagManagerUrl", stringSchema)}, clientMembers(false)...)...)
