package googletagmanager

import (
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

type (
	// zoneBoundary holds the conditions that, when conjoined, make up the boundary of a zone. The conditions
	// are those of a trigger filter, so their values are passed on and returned unchanged.
	zoneBoundary struct {
		CustomEvaluationTriggerID []string    `json:"customEvaluationTriggerId,omitempty"`
		Condition                 []condition `json:"condition,omitempty"`
	}
	zoneChildContainer struct {
		Nickname string `json:"nickname,omitempty"`
		PublicID string `json:"publicId,omitempty"`
	}
	zoneTypeRestriction struct {
		Enable            bool     `json:"enable,omitempty"`
		WhitelistedTypeID []string `json:"whitelistedTypeId,omitempty"`
	}
)

// zoneBody holds the members of a zone a caller can set.
type zoneBody struct {
	Name            string               `json:"name,omitempty"`
	Boundary        *zoneBoundary        `json:"boundary,omitempty"`
	ChildContainer  []zoneChildContainer `json:"childContainer,omitempty"`
	TypeRestriction *zoneTypeRestriction `json:"typeRestriction,omitempty"`
	Notes           string               `json:"notes,omitempty"`
}

// zoneEntity is a Zone of the API: the members of zoneBody and those Google assigns.
type zoneEntity struct {
	Path          idText  `json:"path,omitempty"`
	AccountID     idText  `json:"accountId,omitempty"`
	ContainerID   idText  `json:"containerId,omitempty"`
	WorkspaceID   idText  `json:"workspaceId,omitempty"`
	ZoneID        idText  `json:"zoneId,omitempty"`
	Fingerprint   idText  `json:"fingerprint,omitempty"`
	TagManagerURL urlText `json:"tagManagerUrl,omitempty"`
	zoneBody
}

type (
	zonesAnswer struct {
		Zone          []zoneEntity `json:"zone"`
		NextPageToken string       `json:"nextPageToken"`
	}
	zonesResult struct {
		Zones      []zoneEntity `json:"zones"`
		NextCursor string       `json:"next_cursor,omitempty"`
	}
	revertZoneAnswer struct {
		Zone *zoneEntity `json:"zone"`
	}
	revertZoneResult struct {
		Zone *zoneEntity `json:"zone,omitempty"`
	}
	deleteZoneResult struct {
		Zone    string `json:"zone"`
		Deleted bool   `json:"deleted"`
	}
)

// trees collects the parameter trees of the boundary conditions.
func (b zoneBody) trees() []parameter {
	var trees []parameter
	if b.Boundary != nil {
		for _, c := range b.Boundary.Condition {
			trees = append(trees, c.Parameter...)
		}
	}
	return trees
}

// checkInput refuses a zone the schema would refuse as well, for a handler reached without the schema.
func (b zoneBody) checkInput() error {
	if err := checkTrees(b.trees()); err != nil {
		return err
	}
	if strings.TrimSpace(b.Name) == "" {
		return invalidArgument("name is required")
	}
	return nil
}

// checkAnswer refuses an answer that holds more parameter entries than a request may carry back.
func (z zoneEntity) checkAnswer(op string) error {
	if countNodes(z.trees()) > maxParamNodes {
		return provider.InvalidResponse(op, subject+" returned a zone with more than "+strconv.Itoa(maxParamNodes)+
			" parameter entries")
	}
	return nil
}

// zoneMembers lists the schema members shared by the input of a change and the answer.
func zoneMembers(input bool) []string {
	name := stringSchema
	if input {
		name = `{"type":"string","minLength":1}`
	}
	closed := func(members ...string) string {
		schema := `{"type":"object","properties":{` + strings.Join(members, ",") + `}`
		if input {
			schema += `,"additionalProperties":false`
		}
		return schema + `}`
	}
	list := `{"type":"array","items":{"type":"string"}}`
	boundary := closed(member("customEvaluationTriggerId", list),
		member("condition", `{"type":"array","items":`+conditionSchema(input)+`}`))
	children := `{"type":"array","items":` + closed(member("nickname", stringSchema), member("publicId", stringSchema)) + `}`
	restriction := closed(member("enable", boolSchema), member("whitelistedTypeId", list))
	return []string{member("name", name), member("boundary", boundary), member("childContainer", children),
		member("typeRestriction", restriction), member("notes", stringSchema)}
}

var zoneSchema = objectSchema(append([]string{member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("workspaceId", stringSchema), member("zoneId", stringSchema),
	member("fingerprint", stringSchema), member("tagManagerUrl", stringSchema)}, zoneMembers(false)...)...)
