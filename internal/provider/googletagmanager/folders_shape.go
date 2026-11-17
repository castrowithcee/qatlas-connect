package googletagmanager

import "strings"

// folderBody holds the members of a folder a caller sets. A folder is replaced as a whole, so name and notes
// are plain strings that a read returns uncut: what a read shows can be sent back without losing text.
type folderBody struct {
	Name  string `json:"name,omitempty"`
	Notes string `json:"notes,omitempty"`
}

// folder is a Folder of the API: the members of folderBody and those Google assigns. A text that does not fit
// stays an error of the answer (maxResponse), never a shorter value.
type folder struct {
	Path          idText  `json:"path,omitempty"`
	AccountID     idText  `json:"accountId,omitempty"`
	ContainerID   idText  `json:"containerId,omitempty"`
	WorkspaceID   idText  `json:"workspaceId,omitempty"`
	FolderID      idText  `json:"folderId,omitempty"`
	Fingerprint   idText  `json:"fingerprint,omitempty"`
	TagManagerURL urlText `json:"tagManagerUrl,omitempty"`
	folderBody
}

type (
	foldersAnswer struct {
		Folder        []folder `json:"folder"`
		NextPageToken string   `json:"nextPageToken"`
	}
	foldersResult struct {
		Folders    []folder `json:"folders"`
		NextCursor string   `json:"next_cursor,omitempty"`
	}
	folderEntitiesAnswer struct {
		Tag           []tag      `json:"tag"`
		Trigger       []trigger  `json:"trigger"`
		Variable      []variable `json:"variable"`
		NextPageToken string     `json:"nextPageToken"`
	}
	folderEntitiesResult struct {
		Tags       []tag      `json:"tags"`
		Triggers   []trigger  `json:"triggers"`
		Variables  []variable `json:"variables"`
		NextCursor string     `json:"next_cursor,omitempty"`
	}
)

// checkInput refuses a folder the schema would refuse as well, for a handler reached without the schema.
func (b folderBody) checkInput() error {
	if strings.TrimSpace(b.Name) == "" {
		return invalidArgument("name is required")
	}
	return nil
}

var folderSchema = objectSchema(member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("workspaceId", stringSchema), member("folderId", stringSchema),
	member("name", stringSchema), member("fingerprint", stringSchema), member("tagManagerUrl", stringSchema),
	member("notes", stringSchema))

var folderEntitiesSchema = objectSchema(
	member("tags", `{"type":"array","items":`+tagSchema+`}`),
	member("triggers", `{"type":"array","items":`+triggerSchema+`}`),
	member("variables", `{"type":"array","items":`+variableSchema+`}`),
	member("next_cursor", stringSchema))
