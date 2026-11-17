package googletagmanager

import (
	"strconv"
	"strings"
)

// maxTemplateDataBytes bounds templateData on input only. A template is sandboxed JavaScript plus its
// declaration of fields and permissions, and real ones stay far below a megabyte. The bound keeps a request
// well inside maxRequestBody and a stored value readable inside maxResponse after JSON escaping. A read never
// cuts the text: a stored value that does not fit an answer is an error of the answer.
const maxTemplateDataBytes = 1 << 20

// templateBody holds the members of a template a caller can set. A template is replaced as a whole, and what
// a read shows is what an update sends back, so these members are plain strings that nothing cuts or changes.
type templateBody struct {
	Name         string `json:"name,omitempty"`
	TemplateData string `json:"templateData,omitempty"`
}

// galleryReference links a template to its Community Template Gallery entry. It is set by Google and only
// read, never sent.
type galleryReference struct {
	Host                string   `json:"host,omitempty"`
	Owner               string   `json:"owner,omitempty"`
	Repository          string   `json:"repository,omitempty"`
	Version             idText   `json:"version,omitempty"`
	IsModified          bool     `json:"isModified,omitempty"`
	Signature           nameText `json:"signature,omitempty"`
	TemplateDeveloperID idText   `json:"templateDeveloperId,omitempty"`
	GalleryTemplateID   idText   `json:"galleryTemplateId,omitempty"`
}

// template is a CustomTemplate of the API: the members of templateBody and those Google assigns.
type template struct {
	Path             idText            `json:"path,omitempty"`
	AccountID        idText            `json:"accountId,omitempty"`
	ContainerID      idText            `json:"containerId,omitempty"`
	WorkspaceID      idText            `json:"workspaceId,omitempty"`
	TemplateID       idText            `json:"templateId,omitempty"`
	Fingerprint      idText            `json:"fingerprint,omitempty"`
	TagManagerURL    urlText           `json:"tagManagerUrl,omitempty"`
	GalleryReference *galleryReference `json:"galleryReference,omitempty"`
	templateBody
}

type (
	templatesAnswer struct {
		Template      []template `json:"template"`
		NextPageToken string     `json:"nextPageToken"`
	}
	templatesResult struct {
		Templates  []template `json:"templates"`
		NextCursor string     `json:"next_cursor,omitempty"`
	}
	revertTemplateAnswer struct {
		Template *template `json:"template"`
	}
	revertTemplateResult struct {
		Template *template `json:"template,omitempty"`
	}
	deleteTemplateResult struct {
		Template string `json:"template"`
		Deleted  bool   `json:"deleted"`
	}
)

// checkInput refuses a template the schema would refuse as well, for a handler reached without the schema.
func (b templateBody) checkInput() error {
	switch {
	case strings.TrimSpace(b.Name) == "":
		return invalidArgument("name is required")
	case b.TemplateData == "":
		return invalidArgument("templateData is required")
	case len(b.TemplateData) > maxTemplateDataBytes:
		return invalidArgument("templateData is larger than " + strconv.Itoa(maxTemplateDataBytes) + " bytes")
	}
	return nil
}

// templateMembers lists the schema members shared by the input of a change and the answer.
func templateMembers(input bool) []string {
	name, data := stringSchema, stringSchema
	if input {
		name = `{"type":"string","minLength":1}`
		data = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxTemplateDataBytes) + `}`
	}
	return []string{member("name", name), member("templateData", data)}
}

var galleryReferenceSchema = objectSchema(member("host", stringSchema), member("owner", stringSchema),
	member("repository", stringSchema), member("version", stringSchema), member("isModified", boolSchema),
	member("signature", stringSchema), member("templateDeveloperId", stringSchema),
	member("galleryTemplateId", stringSchema))

var templateSchema = objectSchema(append([]string{member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("workspaceId", stringSchema), member("templateId", stringSchema),
	member("fingerprint", stringSchema), member("tagManagerUrl", stringSchema),
	member("galleryReference", galleryReferenceSchema)}, templateMembers(false)...)...)
