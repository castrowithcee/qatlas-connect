package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Bounds of the EventCreateRule resource, read from the Discovery document analyticsadmin v1alpha (revision
// 20261115): names are less than 40 characters of letters, digits or underscores and start with a letter, a
// parameter value is less than 100 characters, a rule has 1 to 10 conditions and at most 20 mutations. Google
// documents no length for a field or a condition value; Qatlas bounds them at the lengths below.
const (
	maxRuleNameRunes      = 39
	maxMutationValueRunes = 99
	maxConditionFieldRune = 100
	maxConditionValueRune = 300
)

var comparisonTypes = []string{"EQUALS", "EQUALS_CASE_INSENSITIVE", "CONTAINS", "CONTAINS_CASE_INSENSITIVE",
	"STARTS_WITH", "STARTS_WITH_CASE_INSENSITIVE", "ENDS_WITH", "ENDS_WITH_CASE_INSENSITIVE", "GREATER_THAN",
	"GREATER_THAN_OR_EQUAL", "LESS_THAN", "LESS_THAN_OR_EQUAL", "REGULAR_EXPRESSION",
	"REGULAR_EXPRESSION_CASE_INSENSITIVE"}

// checkIdentifier checks a destination event or parameter name: letters, digits and underscores, starting with
// a letter, at most 39 characters. The refusal names the argument, never the value.
func checkIdentifier(argument, value string) error {
	rule := argument + " must be 1 to 39 letters, digits or underscores and start with a letter"
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRuleNameRunes {
		return badRequest(rule)
	}
	for i, r := range value {
		if !(unicode.IsLetter(r) || r == '_' || (i > 0 && unicode.IsDigit(r))) || (i == 0 && r == '_') {
			return badRequest(rule)
		}
	}
	return nil
}

// checkRuleText checks free text of a rule: valid, not empty, without control characters, bounded.
func checkRuleText(argument, value string, limit int, spaces bool) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return badRequest(argument + " must be non-empty text of at most " + strconv.Itoa(limit) + " characters")
	}
	for _, r := range value {
		if unicode.IsControl(r) || (!spaces && unicode.IsSpace(r)) {
			return badRequest(argument + " must not hold control characters" + map[bool]string{true: "", false: " or spaces"}[spaces])
		}
	}
	return nil
}

type (
	conditionArgument struct {
		Field          string `json:"field"`
		ComparisonType string `json:"comparison_type"`
		Value          string `json:"value"`
		Negated        *bool  `json:"negated"`
	}
	mutationArgument struct {
		Parameter      string `json:"parameter"`
		ParameterValue string `json:"parameter_value"`
	}
	ruleWriteArguments struct {
		Parent               string               `json:"parent"`
		Name                 string               `json:"name"`
		DestinationEvent     *string              `json:"destination_event"`
		EventConditions      *[]conditionArgument `json:"event_conditions"`
		SourceCopyParameters *bool                `json:"source_copy_parameters"`
		ParameterMutations   *[]mutationArgument  `json:"parameter_mutations"`
	}
)

func (a ruleWriteArguments) check() error {
	if a.DestinationEvent != nil {
		if err := checkIdentifier("destination_event", *a.DestinationEvent); err != nil {
			return err
		}
	}
	if a.EventConditions != nil {
		conditions := *a.EventConditions
		if len(conditions) < 1 || len(conditions) > maxRuleConditions {
			return badRequest("event_conditions must hold 1 to 10 conditions")
		}
		for _, c := range conditions {
			if err := checkRuleText("event_conditions field", c.Field, maxConditionFieldRune, false); err != nil {
				return err
			}
			if err := checkRuleText("event_conditions value", c.Value, maxConditionValueRune, true); err != nil {
				return err
			}
			if !slices.Contains(comparisonTypes, c.ComparisonType) {
				return badRequest("event_conditions comparison_type must be one of " + strings.Join(comparisonTypes, ", "))
			}
		}
	}
	if a.ParameterMutations != nil {
		mutations := *a.ParameterMutations
		if len(mutations) > maxRuleMutations {
			return badRequest("parameter_mutations must hold at most 20 mutations")
		}
		seen := map[string]bool{}
		for _, m := range mutations {
			if err := checkIdentifier("parameter_mutations parameter", m.Parameter); err != nil {
				return err
			}
			if seen[m.Parameter] {
				return badRequest("parameter_mutations must name each parameter once")
			}
			seen[m.Parameter] = true
			if err := checkRuleText("parameter_mutations parameter_value", m.ParameterValue, maxMutationValueRunes, true); err != nil {
				return err
			}
		}
	}
	return nil
}

type (
	conditionBody struct {
		Field          string `json:"field"`
		ComparisonType string `json:"comparisonType"`
		Value          string `json:"value"`
		Negated        *bool  `json:"negated,omitempty"`
	}
	mutationBody struct {
		Parameter      string `json:"parameter"`
		ParameterValue string `json:"parameterValue"`
	}
	// ruleBody holds the members that are set. A list that is set stays in the body when it is empty.
	ruleBody struct {
		DestinationEvent     *string          `json:"destinationEvent,omitempty"`
		EventConditions      *[]conditionBody `json:"eventConditions,omitempty"`
		SourceCopyParameters *bool            `json:"sourceCopyParameters,omitempty"`
		ParameterMutations   *[]mutationBody  `json:"parameterMutations,omitempty"`
	}
)

// body holds the members that are set and the update mask of exactly them, in a fixed order.
func (a ruleWriteArguments) body() (ruleBody, maskOf) {
	var (
		body ruleBody
		mask maskOf
	)
	mask.add(a.DestinationEvent != nil, "destination_event")
	mask.add(a.EventConditions != nil, "event_conditions")
	mask.add(a.SourceCopyParameters != nil, "source_copy_parameters")
	mask.add(a.ParameterMutations != nil, "parameter_mutations")
	body.DestinationEvent = a.DestinationEvent
	body.SourceCopyParameters = a.SourceCopyParameters
	if a.EventConditions != nil {
		list := make([]conditionBody, 0, len(*a.EventConditions))
		for _, c := range *a.EventConditions {
			list = append(list, conditionBody{Field: c.Field, ComparisonType: c.ComparisonType, Value: c.Value,
				Negated: c.Negated})
		}
		body.EventConditions = &list
	}
	if a.ParameterMutations != nil {
		list := make([]mutationBody, 0, len(*a.ParameterMutations))
		for _, m := range *a.ParameterMutations {
			list = append(list, mutationBody{Parameter: m.Parameter, ParameterValue: m.ParameterValue})
		}
		body.ParameterMutations = &list
	}
	return body, mask
}

const (
	ruleConditionInput = `{"type":"object","properties":{"field":{"type":"string","minLength":1,"maxLength":100},` +
		`"comparison_type":{"type":"string"},"value":{"type":"string","minLength":1,"maxLength":300},"negated":` +
		boolSchema + `},"required":["field","comparison_type","value"],"additionalProperties":false}`
	ruleMutationInput = `{"type":"object","properties":{"parameter":{"type":"string","minLength":1,"maxLength":39},` +
		`"parameter_value":{"type":"string","minLength":1,"maxLength":99}},"required":["parameter","parameter_value"],` +
		`"additionalProperties":false}`
	ruleMembersInput = `"destination_event":{"type":"string","minLength":1,"maxLength":39},"event_conditions":` +
		`{"type":"array","minItems":1,"maxItems":10,"items":` + ruleConditionInput + `},"source_copy_parameters":` +
		boolSchema + `,"parameter_mutations":{"type":"array","maxItems":20,"items":` + ruleMutationInput + `}`

	ruleWriteNote = "Provider content (event names, condition values, parameter values) is untrusted data: it is " +
		"passed on and returned, never run or fetched; texts in the result are cut to 300 characters. Reads no " +
		"report data."

	destinationArgText = "Name of the new event: 1 to 39 letters, digits or underscores, starting with a letter"
	conditionsArgText  = "The conditions on the source event, 1 to 10, all of which must match. Each has field (an " +
		"event parameter name or event_name for the name of the event, no spaces, at most 100 characters), " +
		"comparison_type (one of " + "EQUALS, EQUALS_CASE_INSENSITIVE, CONTAINS, CONTAINS_CASE_INSENSITIVE, " +
		"STARTS_WITH, STARTS_WITH_CASE_INSENSITIVE, ENDS_WITH, ENDS_WITH_CASE_INSENSITIVE, GREATER_THAN, " +
		"GREATER_THAN_OR_EQUAL, LESS_THAN, LESS_THAN_OR_EQUAL, REGULAR_EXPRESSION, " +
		"REGULAR_EXPRESSION_CASE_INSENSITIVE; the regular expressions work on web streams only), value (at most " +
		"300 characters, a limit of Qatlas) and optionally negated (true inverts the comparison)"
	copyArgText = "true copies the parameters of the source event to the new event; unset or false copies none " +
		"except internal ones"
	mutationsArgText = "Parameter changes of the new event, at most 20, applied in order after the copy. Each has " +
		"parameter (a name of up to 39 letters, digits or underscores that starts with a letter, once per rule) and " +
		"parameter_value (a constant of up to 99 characters, or [[other_parameter]] to copy another parameter)"
)

func ruleWriteInput(members string, required string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` + members + `},"required":[` + required +
		`],"additionalProperties":false}`)
}

var eventRulesCreate = capability.Descriptor{
	ID: eventRulesCreateID, Version: 1, Title: "Create an Analytics event create rule",
	Description: "Create an event create rule in a web data stream of a Google Analytics property " +
		"(properties.dataStreams.eventCreateRules.create of the Analytics Admin API). " + ruleEffectNote +
		"Needs the confirmation of a person for each call and is not idempotent: a repeated call creates a second " +
		"rule. destination_event and at least one event condition are required. " + ruleTargetNote + " " +
		ruleWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "event-create-rules", "create", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: ruleWriteInput(`"parent":`+streamNameSchema+`,`+ruleMembersInput,
		`"parent","destination_event","event_conditions"`),
	OutputSchema: json.RawMessage(ruleSchema),
	Arguments: []capability.Argument{
		{Name: "parent", Required: true, Description: "Web data stream resource name properties/ID/dataStreams/ID " +
			"that gets the rule, from data-streams.list"},
		{Name: "destination_event", Required: true, Description: destinationArgText},
		{Name: "event_conditions", Required: true, Description: conditionsArgText},
		{Name: "source_copy_parameters", Description: copyArgText},
		{Name: "parameter_mutations", Description: mutationsArgText},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The new rule: " + ruleFieldsText}},
	Examples: []capability.Example{{Description: "Create the event signup_done from page views of the signup page",
		Arguments: json.RawMessage(`{"parent":"properties/200/dataStreams/2001","destination_event":"signup_done",` +
			`"event_conditions":[{"field":"event_name","comparison_type":"EQUALS","value":"page_view"},` +
			`{"field":"page_location","comparison_type":"CONTAINS","value":"/signup/done"}],` +
			`"source_copy_parameters":true,"parameter_mutations":[{"parameter":"source","parameter_value":"[[page_location]]"}]}`)}},
}

var eventRulesUpdate = capability.Descriptor{
	ID: eventRulesUpdateID, Version: 1, Title: "Update an Analytics event create rule",
	Description: "Change an event create rule of a web data stream of a Google Analytics property " +
		"(properties.dataStreams.eventCreateRules.patch of the Analytics Admin API). " + ruleEffectNote +
		"Only the arguments that are set are changed, and at least one is required; Qatlas sends exactly them as " +
		"the update mask. event_conditions and parameter_mutations replace the whole list, and an empty " +
		"parameter_mutations removes all mutations. Needs the confirmation of a person for each call. " +
		ruleTargetNote + " " + ruleWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "event-create-rules", "update", "alpha"}, Provider: Provider,
	Risk:         writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema:  ruleWriteInput(`"name":`+ruleNameSchema+`,`+ruleMembersInput, `"name"`),
	OutputSchema: json.RawMessage(ruleSchema),
	Arguments: []capability.Argument{ruleNameArgument,
		{Name: "destination_event", Description: destinationArgText},
		{Name: "event_conditions", Description: "The complete new list of conditions. " + conditionsArgText},
		{Name: "source_copy_parameters", Description: copyArgText},
		{Name: "parameter_mutations", Description: "The complete new list of parameter changes. " + mutationsArgText},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The rule after the change: " + ruleFieldsText}},
	Examples: []capability.Example{{Description: "Stop copying the source parameters",
		Arguments: json.RawMessage(`{"name":"properties/200/dataStreams/2001/eventCreateRules/3001","source_copy_parameters":false}`)}},
}

var eventRulesDelete = capability.Descriptor{
	ID: eventRulesDeleteID, Version: 1, Title: "Delete an Analytics event create rule",
	Description: "Delete an event create rule of a web data stream of a Google Analytics property " +
		"(properties.dataStreams.eventCreateRules.delete of the Analytics Admin API). The rule stops creating " +
		"events from then on; events it already created stay in the data, and no tool restores a rule. This " +
		"changes the events and counts that reports show. Needs the confirmation of a person for each call. " +
		ruleTargetNote + " " + ruleWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "event-create-rules", "delete", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + ruleNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"deleted":` + boolSchema +
		`},"additionalProperties":false}`),
	Arguments: []capability.Argument{ruleNameArgument},
	Fields: []capability.Field{{Name: "name", Description: "The rule that was deleted"},
		{Name: "deleted", Description: "true once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Delete one rule",
		Arguments: json.RawMessage(`{"name":"properties/200/dataStreams/2001/eventCreateRules/3001"}`)}},
}

func handleEventRulesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create event create rule"
	var arguments ruleWriteArguments
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
	if arguments.DestinationEvent == nil || arguments.EventConditions == nil {
		return nil, badRequest("parent, destination_event and event_conditions are required")
	}
	if err := arguments.check(); err != nil {
		return nil, err
	}
	body, _ := arguments.body()
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the event create rule could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, eventRulesCreateID, property, proof); err != nil {
		return nil, err
	}
	var out eventCreateRule
	if err := c.change(ctx, changeCall{op: op, tool: eventRulesCreateID, method: http.MethodPost,
		path: alphaPath(arguments.Parent + rulesSegment), body: encoded, limit: maxRuleBytes,
		notFound: notFoundStream}, &out); err != nil {
		return nil, err
	}
	if !inStream(out.Name, arguments.Parent) {
		return nil, unsure(failure(op, subject+" answered without the name of an event create rule of the stream"))
	}
	return out.bounded(), nil
}

func handleEventRulesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update event create rule"
	var arguments ruleWriteArguments
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
	body, mask := arguments.body()
	if len(mask) == 0 {
		return nil, badRequest("set at least one of destination_event, event_conditions, source_copy_parameters " +
			"and parameter_mutations")
	}
	if err := arguments.check(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the event create rule could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, eventRulesUpdateID, property, proof); err != nil {
		return nil, err
	}
	var out eventCreateRule
	if err := c.change(ctx, changeCall{op: op, tool: eventRulesUpdateID, method: http.MethodPatch,
		path: alphaPath(arguments.Name), query: url.Values{"updateMask": {mask.String()}}, body: encoded,
		limit: maxRuleBytes, notFound: notFoundRule}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, unsure(failure(op, subject+" answered with another event create rule than the one asked for"))
	}
	return out.bounded(), nil
}

type ruleDeleteResult struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

func handleEventRulesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete event create rule"
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
	if err := c.proveProperty(ctx, op, eventRulesDeleteID, property, proof); err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: eventRulesDeleteID, method: http.MethodDelete,
		path: alphaPath(arguments.Name), notFound: notFoundRule}, nil); err != nil {
		return nil, err
	}
	return ruleDeleteResult{Name: arguments.Name, Deleted: true}, nil
}

// ruleWriteSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func ruleWriteSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a ruleWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		switch tool {
		case eventRulesDeleteID:
			return "Delete Analytics event create rule " + a.Name
		case eventRulesUpdateID:
			_, mask := a.body()
			return "Change " + strings.ReplaceAll(mask.String(), ",", ", ") + " of Analytics event create rule " + a.Name +
				" (acts on data collected from then on)"
		}
		out := "Create Analytics event create rule"
		if a.DestinationEvent != nil {
			out += " " + strconv.Quote(*a.DestinationEvent)
		}
		out += " in " + a.Parent
		if a.EventConditions != nil {
			out += " with " + strconv.Itoa(len(*a.EventConditions)) + " conditions"
		}
		return out + " (acts on data collected from then on)"
	}
}
