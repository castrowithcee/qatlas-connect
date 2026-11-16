package googletagmanager

import (
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The members a tag shares between the request of a change and the answer. A tag is replaced as a whole, and
// what a read shows is what an update sends back, so these members are plain strings and lists: nothing in
// them is cut, dropped, or changed. A value that does not fit stays an error of the answer, never a shorter
// value. The size of one answer is bounded by maxResponse, and the nesting of parameters is not.
type (
	parameter struct {
		Type            string      `json:"type,omitempty"`
		Key             string      `json:"key,omitempty"`
		Value           string      `json:"value,omitempty"`
		List            []parameter `json:"list,omitempty"`
		Map             []parameter `json:"map,omitempty"`
		IsWeakReference bool        `json:"isWeakReference,omitempty"`
	}
	setupTag struct {
		TagName            string `json:"tagName,omitempty"`
		StopOnSetupFailure bool   `json:"stopOnSetupFailure,omitempty"`
	}
	teardownTag struct {
		TagName               string `json:"tagName,omitempty"`
		StopTeardownOnFailure bool   `json:"stopTeardownOnFailure,omitempty"`
	}
	consentSettings struct {
		ConsentStatus string     `json:"consentStatus,omitempty"`
		ConsentType   *parameter `json:"consentType,omitempty"`
	}
)

// tagBody holds the members of a tag a caller can set.
type tagBody struct {
	Name                         string           `json:"name,omitempty"`
	Type                         string           `json:"type,omitempty"`
	Parameter                    []parameter      `json:"parameter,omitempty"`
	FiringTriggerID              []string         `json:"firingTriggerId,omitempty"`
	BlockingTriggerID            []string         `json:"blockingTriggerId,omitempty"`
	SetupTag                     []setupTag       `json:"setupTag,omitempty"`
	TeardownTag                  []teardownTag    `json:"teardownTag,omitempty"`
	Paused                       bool             `json:"paused,omitempty"`
	LiveOnly                     bool             `json:"liveOnly,omitempty"`
	Priority                     *parameter       `json:"priority,omitempty"`
	Notes                        string           `json:"notes,omitempty"`
	ScheduleStartMs              string           `json:"scheduleStartMs,omitempty"`
	ScheduleEndMs                string           `json:"scheduleEndMs,omitempty"`
	ParentFolderID               string           `json:"parentFolderId,omitempty"`
	TagFiringOption              string           `json:"tagFiringOption,omitempty"`
	MonitoringMetadata           *parameter       `json:"monitoringMetadata,omitempty"`
	MonitoringMetadataTagNameKey string           `json:"monitoringMetadataTagNameKey,omitempty"`
	ConsentSettings              *consentSettings `json:"consentSettings,omitempty"`
}

// tag is a Tag of the API: the members of tagBody and those Google assigns.
type tag struct {
	Path          idText  `json:"path,omitempty"`
	AccountID     idText  `json:"accountId,omitempty"`
	ContainerID   idText  `json:"containerId,omitempty"`
	WorkspaceID   idText  `json:"workspaceId,omitempty"`
	TagID         idText  `json:"tagId,omitempty"`
	Fingerprint   idText  `json:"fingerprint,omitempty"`
	TagManagerURL urlText `json:"tagManagerUrl,omitempty"`
	tagBody
}

type (
	tagsAnswer struct {
		Tag           []tag  `json:"tag"`
		NextPageToken string `json:"nextPageToken"`
	}
	tagsResult struct {
		Tags       []tag  `json:"tags"`
		NextCursor string `json:"next_cursor,omitempty"`
	}
	revertAnswer struct {
		Tag *tag `json:"tag"`
	}
	revertResult struct {
		Tag *tag `json:"tag,omitempty"`
	}
	deleteResult struct {
		Tag     string `json:"tag"`
		Deleted bool   `json:"deleted"`
	}
)

// Bounds of what a caller sends. Texts have no bound of their own: the whole request is bounded, as is every
// answer, so no text is refused that a read can return. maxParamDepth counts the levels of a parameter, the parameter itself being
// the first; the other parameter members the API documents are shallower by their own definition.
const (
	maxParamDepth  = 5
	maxParamNodes  = 5000
	maxRequestBody = 4 << 20
	maxTagIDRunes  = 100
)

// bounds counts the levels and the nodes below the parameters; depth 1 is a parameter without children.
func (p parameter) bounds() (depth, nodes int) {
	depth, nodes = 1, 1
	for _, child := range append(append([]parameter(nil), p.List...), p.Map...) {
		d, n := child.bounds()
		depth, nodes = max(depth, d+1), nodes+n
	}
	return depth, nodes
}

// checkTrees refuses parameters nested deeper or more numerous than a request may carry.
func checkTrees(trees []parameter) error {
	total := 0
	for _, p := range trees {
		depth, nodes := p.bounds()
		if depth > maxParamDepth {
			return invalidArgument("a parameter nests deeper than " + strconv.Itoa(maxParamDepth) + " levels")
		}
		total += nodes
	}
	if total > maxParamNodes {
		return invalidArgument("the parameters hold more than " + strconv.Itoa(maxParamNodes) + " entries")
	}
	return nil
}

// countNodes counts the parameter entries of the trees.
func countNodes(trees []parameter) int {
	total := 0
	for _, p := range trees {
		_, nodes := p.bounds()
		total += nodes
	}
	return total
}

// checkInput refuses a tag the schema would refuse as well, for a handler reached without the schema.
func (b tagBody) checkInput() error {
	trees := append([]parameter(nil), b.Parameter...)
	for _, extra := range []*parameter{b.Priority, b.MonitoringMetadata} {
		if extra != nil {
			trees = append(trees, *extra)
		}
	}
	if b.ConsentSettings != nil && b.ConsentSettings.ConsentType != nil {
		trees = append(trees, *b.ConsentSettings.ConsentType)
	}
	if err := checkTrees(trees); err != nil {
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
func (t tag) checkAnswer(op string) error {
	if countNodes(t.Parameter) > maxParamNodes {
		return provider.InvalidResponse(op, subject+" returned a tag with more than "+strconv.Itoa(maxParamNodes)+
			" parameter entries")
	}
	return nil
}

// Schemas. The input schema of a parameter is a closed object of bounded depth, because the schema language
// has no references; the answer schema keeps the same members but leaves the deepest level open.
func parameterSchema(depth int, input bool) string {
	typeSchema := stringSchema
	if input {
		typeSchema = `{"type":"string","enum":["typeUnspecified","template","integer","boolean","list","map",` +
			`"triggerReference","tagReference"]}`
	}
	members := []string{member("type", typeSchema), member("key", stringSchema), member("value", stringSchema),
		member("isWeakReference", boolSchema)}
	if depth > 1 {
		child := parameterSchema(depth-1, input)
		children := `{"type":"array","items":` + child + `}`
		members = append(members, member("list", children), member("map", children))
	}
	schema := `{"type":"object","properties":{` + strings.Join(members, ",") + `}`
	if input {
		schema += `,"required":["type"],"additionalProperties":false`
	}
	return schema + `}`
}
