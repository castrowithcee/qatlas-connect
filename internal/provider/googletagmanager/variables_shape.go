package googletagmanager

import (
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// formatValue converts the value of a variable: case, number, boolean, and replacements for null, undefined,
// true, and false. Like every member of a variable it is plain data, passed on and returned unchanged.
type formatValue struct {
	CaseConversionType      string     `json:"caseConversionType,omitempty"`
	ConvertNullToValue      *parameter `json:"convertNullToValue,omitempty"`
	ConvertToNumber         string     `json:"convertToNumber,omitempty"`
	ConvertUndefinedToValue *parameter `json:"convertUndefinedToValue,omitempty"`
	ConvertToBoolean        bool       `json:"convertToBoolean,omitempty"`
	ConvertTrueToValue      *parameter `json:"convertTrueToValue,omitempty"`
	ConvertFalseToValue     *parameter `json:"convertFalseToValue,omitempty"`
}

// variableBody holds the members of a variable a caller can set.
type variableBody struct {
	Name               string       `json:"name,omitempty"`
	Type               string       `json:"type,omitempty"`
	Parameter          []parameter  `json:"parameter,omitempty"`
	Notes              string       `json:"notes,omitempty"`
	ScheduleStartMs    string       `json:"scheduleStartMs,omitempty"`
	ScheduleEndMs      string       `json:"scheduleEndMs,omitempty"`
	DisablingTriggerID []string     `json:"disablingTriggerId,omitempty"`
	EnablingTriggerID  []string     `json:"enablingTriggerId,omitempty"`
	ParentFolderID     string       `json:"parentFolderId,omitempty"`
	FormatValue        *formatValue `json:"formatValue,omitempty"`
}

// variable is a Variable of the API: the members of variableBody and those Google assigns.
type variable struct {
	Path          idText  `json:"path,omitempty"`
	AccountID     idText  `json:"accountId,omitempty"`
	ContainerID   idText  `json:"containerId,omitempty"`
	WorkspaceID   idText  `json:"workspaceId,omitempty"`
	VariableID    idText  `json:"variableId,omitempty"`
	Fingerprint   idText  `json:"fingerprint,omitempty"`
	TagManagerURL urlText `json:"tagManagerUrl,omitempty"`
	variableBody
}

type (
	variablesAnswer struct {
		Variable      []variable `json:"variable"`
		NextPageToken string     `json:"nextPageToken"`
	}
	variablesResult struct {
		Variables  []variable `json:"variables"`
		NextCursor string     `json:"next_cursor,omitempty"`
	}
	revertVariableAnswer struct {
		Variable *variable `json:"variable"`
	}
	revertVariableResult struct {
		Variable *variable `json:"variable,omitempty"`
	}
	deleteVariableResult struct {
		Variable string `json:"variable"`
		Deleted  bool   `json:"deleted"`
	}
)

// trees collects every parameter tree of the variable: its parameters and those of its format value.
func (b variableBody) trees() []parameter {
	trees := append([]parameter(nil), b.Parameter...)
	if f := b.FormatValue; f != nil {
		for _, p := range []*parameter{f.ConvertNullToValue, f.ConvertUndefinedToValue, f.ConvertTrueToValue,
			f.ConvertFalseToValue} {
			if p != nil {
				trees = append(trees, *p)
			}
		}
	}
	return trees
}

// checkInput refuses a variable the schema would refuse as well, for a handler reached without the schema.
func (b variableBody) checkInput() error {
	if err := checkTrees(b.trees()); err != nil {
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
func (v variable) checkAnswer(op string) error {
	if countNodes(v.trees()) > maxParamNodes {
		return provider.InvalidResponse(op, subject+" returned a variable with more than "+strconv.Itoa(maxParamNodes)+
			" parameter entries")
	}
	return nil
}

// Schemas. The members of a format value that hold one parameter are two levels deep, like those of a trigger.
const variableParamDepth = 2

// formatValueSchema is the schema of formatValue; the input form is closed and limits the enumerations.
func formatValueSchema(input bool) string {
	caseType, number := stringSchema, stringSchema
	if input {
		caseType = `{"type":"string","enum":["none","lowercase","uppercase"]}`
		number = `{"type":"string","enum":["decimalSeparatorTypeUnspecified","period","comma","automatic"]}`
	}
	one := parameterSchema(variableParamDepth, input)
	schema := `{"type":"object","properties":{` + strings.Join([]string{member("caseConversionType", caseType),
		member("convertNullToValue", one), member("convertToNumber", number),
		member("convertUndefinedToValue", one), member("convertToBoolean", boolSchema),
		member("convertTrueToValue", one), member("convertFalseToValue", one)}, ",") + `}`
	if input {
		schema += `,"additionalProperties":false`
	}
	return schema + `}`
}

// variableMembers lists the schema members shared by the input of a change and the answer.
func variableMembers(input bool) []string {
	name, typ, ms := stringSchema, stringSchema, stringSchema
	if input {
		name, typ = `{"type":"string","minLength":1}`, `{"type":"string","minLength":1}`
		ms = `{"type":"string","pattern":"^-?[0-9]{1,19}$"}`
	}
	return []string{member("name", name), member("type", typ),
		member("parameter", `{"type":"array","items":`+parameterSchema(maxParamDepth, input)+`}`),
		member("notes", stringSchema), member("scheduleStartMs", ms), member("scheduleEndMs", ms),
		member("disablingTriggerId", listSchema), member("enablingTriggerId", listSchema),
		member("parentFolderId", stringSchema), member("formatValue", formatValueSchema(input))}
}

var variableSchema = objectSchema(append([]string{member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("workspaceId", stringSchema), member("variableId", stringSchema),
	member("fingerprint", stringSchema), member("tagManagerUrl", stringSchema)}, variableMembers(false)...)...)
