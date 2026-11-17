package googletagmanager

import (
	"encoding/json"
)

// A changed entity of a workspace status keeps only what identifies it. Its parameters, code, constants, and
// every other value are never held: they can be secrets, and the get tool of the entity reads them.

// entityKinds are the members of an Entity of the API that name its kind, in the order one is picked.
var entityKinds = []string{"tag", "trigger", "variable", "folder", "builtInVariable", "transformation",
	"customTemplate", "zone", "gtagConfig", "client"}

type entityIdentity struct {
	Path          idText   `json:"path"`
	Name          nameText `json:"name"`
	Type          idText   `json:"type"`
	Fingerprint   idText   `json:"fingerprint"`
	TagManagerURL urlText  `json:"tagManagerUrl"`
}

type changedEntity struct {
	EntityType    string   `json:"entityType,omitempty"`
	ChangeStatus  idText   `json:"changeStatus,omitempty"`
	Path          idText   `json:"path,omitempty"`
	Name          nameText `json:"name,omitempty"`
	Type          idText   `json:"type,omitempty"`
	Fingerprint   idText   `json:"fingerprint,omitempty"`
	TagManagerURL urlText  `json:"tagManagerUrl,omitempty"`
}

func (e *changedEntity) UnmarshalJSON(b []byte) error {
	*e = changedEntity{}
	var members map[string]json.RawMessage
	if json.Unmarshal(b, &members) != nil {
		return nil
	}
	if status, ok := members["changeStatus"]; ok {
		_ = e.ChangeStatus.UnmarshalJSON(status)
	}
	for _, kind := range entityKinds {
		raw, ok := members[kind]
		if !ok {
			continue
		}
		var id entityIdentity
		if json.Unmarshal(raw, &id) != nil {
			continue
		}
		e.EntityType, e.Path, e.Name, e.Type, e.Fingerprint, e.TagManagerURL =
			kind, id.Path, id.Name, id.Type, id.Fingerprint, id.TagManagerURL
		break
	}
	return nil
}

type mergeConflict struct {
	EntityInBaseVersion *changedEntity `json:"entityInBaseVersion,omitempty"`
	EntityInWorkspace   *changedEntity `json:"entityInWorkspace,omitempty"`
}

type (
	statusAnswer struct {
		WorkspaceChange []changedEntity `json:"workspaceChange"`
		MergeConflict   []mergeConflict `json:"mergeConflict"`
	}
	// statusResult never has a null list, so a workspace without changes has empty lists.
	statusResult struct {
		WorkspaceChange []changedEntity `json:"workspaceChange"`
		MergeConflict   []mergeConflict `json:"mergeConflict"`
		Truncated       bool            `json:"truncated,omitempty"`
	}
	deleteWorkspaceResult struct {
		Workspace string `json:"workspace"`
		Deleted   bool   `json:"deleted"`
	}
)

var (
	changedEntitySchema = objectSchema(member("entityType", stringSchema), member("changeStatus", stringSchema),
		member("path", stringSchema), member("name", stringSchema), member("type", stringSchema),
		member("fingerprint", stringSchema), member("tagManagerUrl", stringSchema))
	statusSchema = objectSchema(
		member("workspaceChange", `{"type":"array","items":`+changedEntitySchema+`}`),
		member("mergeConflict", `{"type":"array","items":`+objectSchema(
			member("entityInBaseVersion", changedEntitySchema), member("entityInWorkspace", changedEntitySchema))+`}`),
		member("truncated", boolSchema))
)
