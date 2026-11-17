package googletagmanager

import (
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// transformationBody holds the members of a transformation a caller can set.
type transformationBody struct {
	Name           string      `json:"name,omitempty"`
	Type           string      `json:"type,omitempty"`
	Parameter      []parameter `json:"parameter,omitempty"`
	Notes          string      `json:"notes,omitempty"`
	ParentFolderID string      `json:"parentFolderId,omitempty"`
}

// transformationEntity is a Transformation of the API: the members of transformationBody and those Google assigns.
type transformationEntity struct {
	Path             idText  `json:"path,omitempty"`
	AccountID        idText  `json:"accountId,omitempty"`
	ContainerID      idText  `json:"containerId,omitempty"`
	WorkspaceID      idText  `json:"workspaceId,omitempty"`
	TransformationID idText  `json:"transformationId,omitempty"`
	Fingerprint      idText  `json:"fingerprint,omitempty"`
	TagManagerURL    urlText `json:"tagManagerUrl,omitempty"`
	transformationBody
}

type (
	transformationsAnswer struct {
		Transformation []transformationEntity `json:"transformation"`
		NextPageToken  string                 `json:"nextPageToken"`
	}
	transformationsResult struct {
		Transformations []transformationEntity `json:"transformations"`
		NextCursor      string                 `json:"next_cursor,omitempty"`
	}
	revertTransformationAnswer struct {
		Transformation *transformationEntity `json:"transformation"`
	}
	revertTransformationResult struct {
		Transformation *transformationEntity `json:"transformation,omitempty"`
	}
	deleteTransformationResult struct {
		Transformation string `json:"transformation"`
		Deleted        bool   `json:"deleted"`
	}
)

// checkInput refuses a transformation the schema would refuse as well, for a handler reached without the schema.
func (b transformationBody) checkInput() error {
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
func (c transformationEntity) checkAnswer(op string) error {
	if countNodes(c.Parameter) > maxParamNodes {
		return provider.InvalidResponse(op, subject+" returned a transformation with more than "+strconv.Itoa(maxParamNodes)+
			" parameter entries")
	}
	return nil
}

func transformationMembers(input bool) []string {
	name, typ := stringSchema, stringSchema
	if input {
		name, typ = `{"type":"string","minLength":1}`, `{"type":"string","minLength":1}`
	}
	return []string{member("name", name), member("type", typ),
		member("parameter", `{"type":"array","items":`+parameterSchema(maxParamDepth, input)+`}`),
		member("notes", stringSchema), member("parentFolderId", stringSchema)}
}

var transformationSchema = objectSchema(append([]string{member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("workspaceId", stringSchema), member("transformationId", stringSchema),
	member("fingerprint", stringSchema), member("tagManagerUrl", stringSchema)}, transformationMembers(false)...)...)
