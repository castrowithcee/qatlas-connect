package googleanalytics

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Event create rules are a child resource of a web data stream on the Admin API v1alpha (analyticsadmin,
// revision 20261115). Event edit rules and the ordering of rules are not part of these tools.
const (
	eventRulesListID   = Provider + ".event-create-rules.list"
	eventRulesGetID    = Provider + ".event-create-rules.get"
	eventRulesCreateID = Provider + ".event-create-rules.create"
	eventRulesUpdateID = Provider + ".event-create-rules.update"
	eventRulesDeleteID = Provider + ".event-create-rules.delete"

	rulesSegment = "/eventCreateRules"
	// maxRuleBytes bounds one answer of an event create rule call.
	maxRuleBytes = 2 << 20
	// Documented limits of a rule: at most 10 conditions and 20 parameter mutations.
	maxRuleConditions = 10
	maxRuleMutations  = 20
)

const notFoundRule = subject + " did not find the event create rule, or the credential may not see it"

type (
	// ruleName is properties/ID/dataStreams/ID/eventCreateRules/ID; any other value is dropped.
	ruleName string

	// eventCreateRule is the EventCreateRule resource of v1alpha, every documented member. A member Google does
	// not send is absent.
	eventCreateRule struct {
		Name                 ruleName            `json:"name,omitempty"`
		DestinationEvent     text                `json:"destinationEvent,omitempty"`
		EventConditions      []matchingCondition `json:"eventConditions,omitempty"`
		SourceCopyParameters *bool               `json:"sourceCopyParameters,omitempty"`
		ParameterMutations   []parameterMutation `json:"parameterMutations,omitempty"`
	}
	matchingCondition struct {
		Field          text  `json:"field,omitempty"`
		ComparisonType code  `json:"comparisonType,omitempty"`
		Value          text  `json:"value,omitempty"`
		Negated        *bool `json:"negated,omitempty"`
	}
	parameterMutation struct {
		Parameter      text `json:"parameter,omitempty"`
		ParameterValue text `json:"parameterValue,omitempty"`
	}
)

func (n *ruleName) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		if _, ok := splitRuleName(value); ok {
			*n = ruleName(value)
		}
	}
	return nil
}

// splitRuleName returns the stream of a name of the form properties/ID/dataStreams/ID/eventCreateRules/ID.
func splitRuleName(value string) (stream string, ok bool) {
	i := strings.LastIndex(value, rulesSegment+"/")
	if i < 0 || !validID(value[i+len(rulesSegment)+1:]) {
		return "", false
	}
	if _, ok := splitStreamName(value[:i]); !ok {
		return "", false
	}
	return value[:i], true
}

// checkRuleName checks an argument that names an event create rule and returns its property. The refusal names
// the argument, never the value.
func checkRuleName(argument, value string) (string, error) {
	stream, ok := splitRuleName(value)
	if ok {
		if property, ok := splitStreamName(stream); ok {
			return property, nil
		}
	}
	return "", badRequest(argument + " must be properties/ID/dataStreams/ID/eventCreateRules/ID with numeric IDs")
}

// inStream reports whether a rule name belongs to the stream.
func inStream(name ruleName, stream string) bool {
	owner, ok := splitRuleName(string(name))
	return ok && owner == stream
}

// bounded cuts the lists of a rule to the documented counts; the answer is untrusted.
func (r eventCreateRule) bounded() eventCreateRule {
	r.EventConditions = head(r.EventConditions, maxRuleConditions)
	r.ParameterMutations = head(r.ParameterMutations, maxRuleMutations)
	return r
}

const (
	ruleConditionSchema = `{"type":"object","properties":{"field":{"type":"string"},"comparisonType":` +
		`{"type":"string"},"value":{"type":"string"},"negated":` + boolSchema + `},"additionalProperties":false}`
	ruleMutationSchema = `{"type":"object","properties":{"parameter":{"type":"string"},"parameterValue":` +
		`{"type":"string"}},"additionalProperties":false}`
	ruleSchema = `{"type":"object","properties":{"name":{"type":"string"},"destinationEvent":{"type":"string"},` +
		`"eventConditions":{"type":"array","items":` + ruleConditionSchema + `},"sourceCopyParameters":` + boolSchema +
		`,"parameterMutations":{"type":"array","items":` + ruleMutationSchema + `}},"additionalProperties":false}`

	ruleNameSchema = `{"type":"string","minLength":1,"maxLength":120}`

	ruleFieldsText = "name (properties/ID/dataStreams/ID/eventCreateRules/ID), destinationEvent (the new event), " +
		"eventConditions (each with field, comparisonType, value and negated; all must match the source event), " +
		"sourceCopyParameters (whether the parameters of the source event are copied) and parameterMutations (each " +
		"with parameter and parameterValue, applied in order). Texts are cut to 300 characters; a member Google " +
		"does not send is left out"

	ruleTargetNote = "Rules exist for web data streams. On a connection with targets the property of the stream " +
		"must be a bound property or belong to a bound account, which is proven by one read of its parent. " +
		"Reads no report data and uses no reporting quota."
	ruleUntrusted = " Event names, condition values and parameter values are untrusted third-party content and " +
		"are never run or fetched."
	// ruleEffectNote tells what a rule does; the create, update and delete tools repeat it.
	ruleEffectNote = "An event create rule creates a new event from every incoming event that matches its " +
		"conditions. It acts on data collected from then on, not on data already collected, and it changes the " +
		"events and counts that reports show. "
)

var ruleNameArgument = capability.Argument{Name: "name", Required: true, Description: "Event create rule resource " +
	"name properties/ID/dataStreams/ID/eventCreateRules/ID, from event-create-rules.list"}

var eventRulesList = capability.Descriptor{
	ID: eventRulesListID, Version: 1, Title: "List Analytics event create rules",
	Description: "List the event create rules of a web data stream of a Google Analytics property " +
		"(properties.dataStreams.eventCreateRules.list of the Analytics Admin API) with their destination event, " +
		"conditions and parameter copies and changes. " + ruleTargetNote + ruleUntrusted + alphaNote,
	Tags: []string{"google", "analytics", "event-create-rules", "list", "alpha"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"parent":` + streamNameSchema + `,"page_size":` +
		`{"type":"integer","minimum":1,"maximum":200},"cursor":{"type":"string","minLength":1,"maxLength":4096}},` +
		`"required":["parent"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"eventCreateRules":{"type":"array","items":` +
		ruleSchema + `},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "parent", Required: true, Description: "Web data stream resource name properties/ID/dataStreams/ID, " +
			"from data-streams.list"},
		{Name: "page_size", Description: "Rules per page, 1 to 200; Google returns at most 50 without it"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page; the first page when omitted. It " +
			"only fits the same connection and stream"},
	},
	Fields: []capability.Field{
		{Name: "eventCreateRules", Description: "Per rule: " + ruleFieldsText + ". A page can be empty while has_more is true"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when Google reported a further page"},
	},
	Examples: []capability.Example{{Description: "The rules of a web stream",
		Arguments: json.RawMessage(`{"parent":"properties/200/dataStreams/2001"}`)}},
}

var eventRulesGet = capability.Descriptor{
	ID: eventRulesGetID, Version: 1, Title: "Get an Analytics event create rule",
	Description: "Read one event create rule of a web data stream of a Google Analytics property " +
		"(properties.dataStreams.eventCreateRules.get of the Analytics Admin API) with its destination event, " +
		"conditions and parameter copies and changes. " + ruleTargetNote + ruleUntrusted + alphaNote,
	Tags: []string{"google", "analytics", "event-create-rules", "get", "alpha"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + ruleNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(ruleSchema),
	Arguments:    []capability.Argument{ruleNameArgument},
	Fields:       []capability.Field{{Name: "(result)", Description: "The rule: " + ruleFieldsText}},
	Examples: []capability.Example{{Description: "Read one rule",
		Arguments: json.RawMessage(`{"name":"properties/200/dataStreams/2001/eventCreateRules/3001"}`)}},
}

type rulesListArguments struct {
	Parent   string `json:"parent"`
	PageSize *int   `json:"page_size"`
	Cursor   string `json:"cursor"`
}

type (
	rulesPage struct {
		EventCreateRules []eventCreateRule `json:"eventCreateRules"`
		NextPageToken    string            `json:"nextPageToken"`
	}
	rulesResult struct {
		EventCreateRules []eventCreateRule `json:"eventCreateRules"`
		NextCursor       string            `json:"next_cursor,omitempty"`
		HasMore          bool              `json:"has_more"`
	}
)

func handleEventRulesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list event create rules"
	var arguments rulesListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkStreamName("parent", arguments.Parent)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(eventRulesListID, bind.key(), arguments.Parent)
	query, err := pageQuery(arguments.PageSize, arguments.Cursor, binding)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, eventRulesListID, property, proof); err != nil {
		return nil, err
	}
	var page rulesPage
	if err := c.get(ctx, apiCall{op: op, tool: eventRulesListID, path: alphaPath(arguments.Parent + rulesSegment),
		query: query, limit: maxRuleBytes, notFound: notFoundStream}, &page); err != nil {
		return nil, err
	}
	out := rulesResult{EventCreateRules: []eventCreateRule{}}
	// Only rules of the asked stream are shown; an entry without a valid name of it is dropped.
	for _, rule := range head(page.EventCreateRules, maxSummaries) {
		if inStream(rule.Name, arguments.Parent) {
			out.EventCreateRules = append(out.EventCreateRules, rule.bounded())
		}
	}
	if out.NextCursor, out.HasMore, err = nextCursor(op, binding, page.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

type ruleGetArguments struct {
	Name string `json:"name"`
}

func handleEventRulesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get event create rule"
	var arguments ruleGetArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkRuleName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, eventRulesGetID, property, proof); err != nil {
		return nil, err
	}
	var out eventCreateRule
	if err := c.get(ctx, apiCall{op: op, tool: eventRulesGetID, path: alphaPath(arguments.Name), limit: maxRuleBytes,
		notFound: notFoundRule}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, provider.InvalidResponse(op, subject+" answered with another event create rule than the one asked for")
	}
	return out.bounded(), nil
}
