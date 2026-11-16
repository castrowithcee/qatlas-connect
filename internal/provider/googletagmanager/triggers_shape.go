package googletagmanager

import (
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// condition is one predicate of a trigger filter: an operator and its parameters (arg0, arg1, negate, ...).
// Like every member of a trigger it is plain text: filter values such as regular expressions and selectors are
// data, passed on and returned unchanged, never run or cut.
type condition struct {
	Type      string      `json:"type,omitempty"`
	Parameter []parameter `json:"parameter,omitempty"`
}

// triggerBody holds the members of a trigger a caller can set. Most are single parameters, as Google defines
// them, such as an integer or a list of percentages.
type triggerBody struct {
	Name                           string      `json:"name,omitempty"`
	Type                           string      `json:"type,omitempty"`
	Filter                         []condition `json:"filter,omitempty"`
	AutoEventFilter                []condition `json:"autoEventFilter,omitempty"`
	CustomEventFilter              []condition `json:"customEventFilter,omitempty"`
	EventName                      *parameter  `json:"eventName,omitempty"`
	Parameter                      []parameter `json:"parameter,omitempty"`
	WaitForTags                    *parameter  `json:"waitForTags,omitempty"`
	CheckValidation                *parameter  `json:"checkValidation,omitempty"`
	WaitForTagsTimeout             *parameter  `json:"waitForTagsTimeout,omitempty"`
	UniqueTriggerID                *parameter  `json:"uniqueTriggerId,omitempty"`
	IntervalSeconds                *parameter  `json:"intervalSeconds,omitempty"`
	MaxTimerLengthSeconds          *parameter  `json:"maxTimerLengthSeconds,omitempty"`
	Limit                          *parameter  `json:"limit,omitempty"`
	Interval                       *parameter  `json:"interval,omitempty"`
	VerticalScrollPercentageList   *parameter  `json:"verticalScrollPercentageList,omitempty"`
	HorizontalScrollPercentageList *parameter  `json:"horizontalScrollPercentageList,omitempty"`
	VisibilitySelector             *parameter  `json:"visibilitySelector,omitempty"`
	VisiblePercentageMin           *parameter  `json:"visiblePercentageMin,omitempty"`
	VisiblePercentageMax           *parameter  `json:"visiblePercentageMax,omitempty"`
	ContinuousTimeMinMilliseconds  *parameter  `json:"continuousTimeMinMilliseconds,omitempty"`
	TotalTimeMinMilliseconds       *parameter  `json:"totalTimeMinMilliseconds,omitempty"`
	Selector                       *parameter  `json:"selector,omitempty"`
	ParentFolderID                 string      `json:"parentFolderId,omitempty"`
	Notes                          string      `json:"notes,omitempty"`
}

// trigger is a Trigger of the API: the members of triggerBody and those Google assigns.
type trigger struct {
	Path          idText  `json:"path,omitempty"`
	AccountID     idText  `json:"accountId,omitempty"`
	ContainerID   idText  `json:"containerId,omitempty"`
	WorkspaceID   idText  `json:"workspaceId,omitempty"`
	TriggerID     idText  `json:"triggerId,omitempty"`
	Fingerprint   idText  `json:"fingerprint,omitempty"`
	TagManagerURL urlText `json:"tagManagerUrl,omitempty"`
	triggerBody
}

type (
	triggersAnswer struct {
		Trigger       []trigger `json:"trigger"`
		NextPageToken string    `json:"nextPageToken"`
	}
	triggersResult struct {
		Triggers   []trigger `json:"triggers"`
		NextCursor string    `json:"next_cursor,omitempty"`
	}
	revertTriggerAnswer struct {
		Trigger *trigger `json:"trigger"`
	}
	revertTriggerResult struct {
		Trigger *trigger `json:"trigger,omitempty"`
	}
	deleteTriggerResult struct {
		Trigger string `json:"trigger"`
		Deleted bool   `json:"deleted"`
	}
)

// singles lists the single-parameter members of a trigger.
func (b triggerBody) singles() []*parameter {
	return []*parameter{b.EventName, b.WaitForTags, b.CheckValidation, b.WaitForTagsTimeout, b.UniqueTriggerID,
		b.IntervalSeconds, b.MaxTimerLengthSeconds, b.Limit, b.Interval, b.VerticalScrollPercentageList,
		b.HorizontalScrollPercentageList, b.VisibilitySelector, b.VisiblePercentageMin, b.VisiblePercentageMax,
		b.ContinuousTimeMinMilliseconds, b.TotalTimeMinMilliseconds, b.Selector}
}

// trees collects every parameter tree of the trigger: its parameters, single members, and filter parameters.
func (b triggerBody) trees() []parameter {
	trees := append([]parameter(nil), b.Parameter...)
	for _, p := range b.singles() {
		if p != nil {
			trees = append(trees, *p)
		}
	}
	for _, group := range [][]condition{b.Filter, b.AutoEventFilter, b.CustomEventFilter} {
		for _, c := range group {
			trees = append(trees, c.Parameter...)
		}
	}
	return trees
}

// checkInput refuses a trigger the schema would refuse as well, for a handler reached without the schema.
func (b triggerBody) checkInput() error {
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
func (t trigger) checkAnswer(op string) error {
	if countNodes(t.trees()) > maxParamNodes {
		return provider.InvalidResponse(op, subject+" returned a trigger with more than "+strconv.Itoa(maxParamNodes)+
			" parameter entries")
	}
	return nil
}

// Schemas. The single-parameter members and the parameters of a condition are two levels deep, enough for a
// list of percentages; the parameters of the trigger itself nest as far as those of a tag.
const (
	triggerTypeEnum = `{"type":"string","enum":["eventTypeUnspecified","pageview","domReady","windowLoaded",` +
		`"customEvent","triggerGroup","init","consentInit","serverPageview","always","firebaseAppException",` +
		`"firebaseAppUpdate","firebaseCampaign","firebaseFirstOpen","firebaseInAppPurchase",` +
		`"firebaseNotificationDismiss","firebaseNotificationForeground","firebaseNotificationOpen",` +
		`"firebaseNotificationReceive","firebaseOsUpdate","firebaseSessionStart","firebaseUserEngagement",` +
		`"formSubmission","click","linkClick","jsError","historyChange","timer","ampClick","ampTimer","ampScroll",` +
		`"ampVisibility","youTubeVideo","scrollDepth","elementVisibility"]}`
	conditionTypeEnum = `{"type":"string","enum":["conditionTypeUnspecified","equals","contains","startsWith",` +
		`"endsWith","matchRegex","greater","greaterOrEquals","less","lessOrEquals","cssSelector","urlMatches"]}`
	triggerParamDepth = 2
)

// triggerSingles names the single-parameter members in the order of singles.
var triggerSingles = []string{"eventName", "waitForTags", "checkValidation", "waitForTagsTimeout", "uniqueTriggerId",
	"intervalSeconds", "maxTimerLengthSeconds", "limit", "interval", "verticalScrollPercentageList",
	"horizontalScrollPercentageList", "visibilitySelector", "visiblePercentageMin", "visiblePercentageMax",
	"continuousTimeMinMilliseconds", "totalTimeMinMilliseconds", "selector"}

func conditionSchema(input bool) string {
	typeSchema := stringSchema
	if input {
		typeSchema = conditionTypeEnum
	}
	schema := `{"type":"object","properties":{` + member("type", typeSchema) + `,` +
		member("parameter", `{"type":"array","items":`+parameterSchema(triggerParamDepth, input)+`}`) + `}`
	if input {
		schema += `,"additionalProperties":false`
	}
	return schema + `}`
}

// triggerMembers lists the schema members shared by the input of a change and the answer.
func triggerMembers(input bool) []string {
	typeSchema := stringSchema
	if input {
		typeSchema = triggerTypeEnum
	}
	conditions := `{"type":"array","items":` + conditionSchema(input) + `}`
	out := []string{member("name", stringSchema), member("type", typeSchema),
		member("filter", conditions), member("autoEventFilter", conditions), member("customEventFilter", conditions),
		member("parameter", `{"type":"array","items":`+parameterSchema(maxParamDepth, input)+`}`)}
	for _, name := range triggerSingles {
		out = append(out, member(name, parameterSchema(triggerParamDepth, input)))
	}
	return append(out, member("parentFolderId", stringSchema), member("notes", stringSchema))
}

var triggerSchema = objectSchema(append([]string{member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("workspaceId", stringSchema), member("triggerId", stringSchema),
	member("fingerprint", stringSchema), member("tagManagerUrl", stringSchema)}, triggerMembers(false)...)...)
