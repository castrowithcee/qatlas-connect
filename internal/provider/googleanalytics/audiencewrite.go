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

// Bounds of the Audience resource, read from the Discovery document analyticsadmin v1alpha (revision
// 20261115): a membership lasts at most 540 days, and the filter clauses, the membership duration and the
// exclusion duration mode cannot be changed after creation. Google documents no length for the display name or
// the description; the bounds below are Qatlas bounds, the same as for what a result shows.
const maxMembershipDays = 540

// checkAudienceDisplayName refuses a blank name, one that is not valid text or holds a control character, and
// one longer than a result shows. The refusal never quotes the name.
func checkAudienceDisplayName(value string) error {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return badRequest("display_name must be non-blank text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest("display_name must not hold control characters")
		}
	}
	if utf8.RuneCountInString(value) > maxTextRunes {
		return badRequest("display_name must be at most 300 characters")
	}
	return nil
}

// checkAudienceDescription refuses a blank description, since Google requires one, and applies the bounds of
// a metric description otherwise.
func checkAudienceDescription(value string) error {
	if strings.TrimSpace(value) == "" {
		return badRequest("description must be non-blank text")
	}
	return checkMetricDescription(value)
}

const audienceWriteNote = "Provider content (names, descriptions, filter values) is untrusted data: it is passed on " +
	"and returned, never run; the result cuts names to 300, descriptions to 4096 and filter values to 300 " +
	"characters. Qatlas checks the structure and size of the filter clauses before the call (at most 20 clauses, " +
	"20 sequence steps, 200 expressions, 100 values per list and 64 KiB); Google checks their meaning. " +
	"Creating an audience links nothing: an export to a linked advertising account exists only through links this " +
	"tool does not create. Reads no report data."

const (
	audDisplayNameSchema = `{"type":"string","minLength":1,"maxLength":300}`
	audDescriptionSchema = `{"type":"string","minLength":1,"maxLength":4096}`
	audTriggerSchema     = `{"type":"object","properties":{"event_name":{"type":"string","pattern":"^[A-Za-z][A-Za-z0-9_]{0,38}$"},` +
		`"log_condition":{"type":"string","enum":["AUDIENCE_JOINED","AUDIENCE_MEMBERSHIP_RENEWED"]}},` +
		`"required":["event_name","log_condition"],"additionalProperties":false}`
)

// The input schema of the filter clauses is written out level by level, because the schema language of the
// core has no references. The levels follow the grammar of checkFilterClauses.
const (
	audScopeSchema = `{"type":"string","enum":["AUDIENCE_FILTER_SCOPE_WITHIN_SAME_EVENT",` +
		`"AUDIENCE_FILTER_SCOPE_WITHIN_SAME_SESSION","AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS"]}`
	audNumberSchema = `{"type":"object","properties":{"int64_value":{"type":"string","pattern":"^-?[0-9]{1,18}$"},` +
		`"double_value":{"type":"number"}},"minProperties":1,"maxProperties":1,"additionalProperties":false}`
	audStringFilterSchema = `{"type":"object","properties":{"value":{"type":"string","minLength":1,"maxLength":300},` +
		`"match_type":{"type":"string","enum":["EXACT","BEGINS_WITH","ENDS_WITH","CONTAINS","FULL_REGEXP"]},` +
		`"case_sensitive":{"type":"boolean"}},"required":["value","match_type"],"additionalProperties":false}`
	audInListSchema = `{"type":"object","properties":{"values":{"type":"array","minItems":1,"maxItems":100,` +
		`"items":{"type":"string","minLength":1,"maxLength":300}},"case_sensitive":{"type":"boolean"}},` +
		`"required":["values"],"additionalProperties":false}`
	audNumericFilterSchema = `{"type":"object","properties":{"operation":{"type":"string",` +
		`"enum":["EQUAL","LESS_THAN","GREATER_THAN"]},"value":` + audNumberSchema + `},` +
		`"required":["operation","value"],"additionalProperties":false}`
	audBetweenSchema = `{"type":"object","properties":{"from_value":` + audNumberSchema + `,"to_value":` +
		audNumberSchema + `},"required":["from_value","to_value"],"additionalProperties":false}`
	audMetricFilterSchema = `{"type":"object","properties":{"field_name":{"type":"string","pattern":` +
		`"^[A-Za-z0-9_:.-]{1,64}$"},"string_filter":` + audStringFilterSchema + `,"in_list_filter":` + audInListSchema +
		`,"numeric_filter":` + audNumericFilterSchema + `,"between_filter":` + audBetweenSchema +
		`,"in_any_n_day_period":{"type":"integer","minimum":1,"maximum":60},"at_any_point_in_time":{"type":"boolean"}},` +
		`"required":["field_name"],"additionalProperties":false}`
	audNotSchema = `{"type":"object","properties":{"dimension_or_metric_filter":` + audMetricFilterSchema +
		`},"required":["dimension_or_metric_filter"],"additionalProperties":false}`
	// The parameter expression of an event filter: an and group of or groups of filters and negated filters.
	audParamOrItem = `{"type":"object","properties":{"dimension_or_metric_filter":` + audMetricFilterSchema +
		`,"not_expression":` + audNotSchema + `},"minProperties":1,"maxProperties":1,"additionalProperties":false}`
	audParamAndItem = `{"type":"object","properties":{"or_group":{"type":"array","minItems":1,"maxItems":50,` +
		`"items":` + audParamOrItem + `}},"required":["or_group"],"additionalProperties":false}`
	audParamTop = `{"type":"object","properties":{"and_group":{"type":"array","minItems":1,"maxItems":50,"items":` +
		audParamAndItem + `}},"required":["and_group"],"additionalProperties":false}`
	audEventFilterSchema = `{"type":"object","properties":{"event_name":{"type":"string","pattern":` +
		`"^[A-Za-z][A-Za-z0-9_]{0,39}$"},"event_parameter_filter_expression":` + audParamTop + `},` +
		`"required":["event_name"],"additionalProperties":false}`
	audOrItem = `{"type":"object","properties":{"dimension_or_metric_filter":` + audMetricFilterSchema +
		`,"event_filter":` + audEventFilterSchema + `,"not_expression":` + audNotSchema +
		`},"minProperties":1,"maxProperties":1,"additionalProperties":false}`
	audAndItem = `{"type":"object","properties":{"or_group":{"type":"array","minItems":1,"maxItems":50,"items":` +
		audOrItem + `}},"required":["or_group"],"additionalProperties":false}`
	audTopExpression = `{"type":"object","properties":{"and_group":{"type":"array","minItems":1,"maxItems":50,"items":` +
		audAndItem + `}},"required":["and_group"],"additionalProperties":false}`
	audDurationSchema = `{"type":"string","pattern":"^[0-9]{1,6}(\\.[0-9]{1,9})?s$"}`
	audSimpleSchema   = `{"type":"object","properties":{"scope":` + audScopeSchema + `,"filter_expression":` +
		audTopExpression + `},"required":["scope","filter_expression"],"additionalProperties":false}`
	audStepSchema = `{"type":"object","properties":{"scope":` + audScopeSchema + `,"immediately_follows":` +
		`{"type":"boolean"},"constraint_duration":` + audDurationSchema + `,"filter_expression":` + audTopExpression +
		`},"required":["scope","filter_expression"],"additionalProperties":false}`
	audSequenceSchema = `{"type":"object","properties":{"scope":` + audScopeSchema + `,"sequence_maximum_duration":` +
		audDurationSchema + `,"sequence_steps":{"type":"array","minItems":1,"maxItems":20,"items":` + audStepSchema +
		`}},"required":["scope","sequence_steps"],"additionalProperties":false}`
	audClausesSchema = `{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","properties":{` +
		`"clause_type":{"type":"string","enum":["INCLUDE","EXCLUDE"]},"simple_filter":` + audSimpleSchema +
		`,"sequence_filter":` + audSequenceSchema + `},"required":["clause_type"],"additionalProperties":false}}`
)

const filterClausesHelp = "List of 1 to 20 clauses that are AND'ed together. Each clause has clause_type (INCLUDE or " +
	"EXCLUDE) and exactly one of simple_filter (scope, filter_expression) and sequence_filter (scope, " +
	"sequence_maximum_duration such as 3600s, sequence_steps of scope, immediately_follows, constraint_duration and " +
	"filter_expression). scope is AUDIENCE_FILTER_SCOPE_WITHIN_SAME_EVENT, _WITHIN_SAME_SESSION or " +
	"_ACROSS_ALL_SESSIONS. A filter_expression is {and_group:[{or_group:[...]}]}: the and group holds or groups, an " +
	"or group holds dimension_or_metric_filter, event_filter and not_expression members, one per entry. A " +
	"dimension_or_metric_filter has field_name and one of string_filter (value, match_type EXACT, BEGINS_WITH, " +
	"ENDS_WITH, CONTAINS or FULL_REGEXP, case_sensitive), in_list_filter (values, case_sensitive), numeric_filter " +
	"(operation EQUAL, LESS_THAN or GREATER_THAN, value with int64_value as text or double_value) and " +
	"between_filter (from_value, to_value), plus in_any_n_day_period (1 to 60) and at_any_point_in_time. A " +
	"not_expression holds one dimension_or_metric_filter. An event_filter has event_name and optionally " +
	"event_parameter_filter_expression, an and group of or groups of filters and negated filters. Filter clauses " +
	"cannot be changed after creation"

var audiencesCreate = capability.Descriptor{
	ID: audiencesCreateID, Version: 1, Title: "Create an Analytics audience",
	Description: "Create an audience in a Google Analytics property (properties.audiences.create of the Analytics " +
		"Admin API). Needs the confirmation of a person for each call and is not idempotent. display_name, " +
		"description, membership_duration_days and filter_clauses are required. The filter clauses, the membership " +
		"duration and the exclusion duration mode cannot be changed afterwards. On a connection with targets the " +
		"property must be a bound property or belong to a bound account, which is proven by one read of its " +
		"parent before the change. " + audienceWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "audiences", "create", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema +
		`,"display_name":` + audDisplayNameSchema + `,"description":` + audDescriptionSchema +
		`,"membership_duration_days":{"type":"integer","minimum":1,"maximum":540},"filter_clauses":` +
		audClausesSchema + `,"event_trigger":` + audTriggerSchema + `,"exclusion_duration_mode":{"type":"string",` +
		`"enum":["EXCLUDE_TEMPORARILY","EXCLUDE_PERMANENTLY"]}},"required":["property","display_name",` +
		`"description","membership_duration_days","filter_clauses"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(audienceSchema),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID that gets the audience"},
		{Name: "display_name", Required: true, Description: "Display name, at most 300 characters, no control characters"},
		{Name: "description", Required: true, Description: "Description, 1 to 4096 characters"},
		{Name: "membership_duration_days", Required: true, Description: "Days a user stays in the audience, 1 to 540; " +
			"not changeable later"},
		{Name: "filter_clauses", Required: true, Description: filterClausesHelp},
		{Name: "event_trigger", Description: "Event to log when a user joins: event_name (letters, digits, underscores, " +
			"at most 39 characters) and log_condition (AUDIENCE_JOINED or AUDIENCE_MEMBERSHIP_RENEWED)"},
		{Name: "exclusion_duration_mode", Description: "EXCLUDE_TEMPORARILY or EXCLUDE_PERMANENTLY: how long an " +
			"exclusion lasts for users that meet an EXCLUDE clause; not changeable later"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The new audience: " + audienceFieldsText}},
	Examples: []capability.Example{{Description: "Users who purchased in the last 30 days",
		Arguments: json.RawMessage(`{"property":"properties/200","display_name":"Purchasers","description":` +
			`"Users with a purchase event","membership_duration_days":30,"filter_clauses":[{"clause_type":"INCLUDE",` +
			`"simple_filter":{"scope":"AUDIENCE_FILTER_SCOPE_ACROSS_ALL_SESSIONS","filter_expression":{"and_group":` +
			`[{"or_group":[{"event_filter":{"event_name":"purchase"}}]}]}}}]}`)}},
}

var audiencesUpdate = capability.Descriptor{
	ID: audiencesUpdateID, Version: 1, Title: "Update an Analytics audience",
	Description: "Change an audience of a Google Analytics property (properties.audiences.patch of the Analytics " +
		"Admin API). Only display_name, description and event_trigger can be changed; the filter clauses, the " +
		"membership duration and the exclusion duration mode cannot, and the tool refuses them. Only the arguments " +
		"that are set are changed, and at least one is required; Qatlas sends exactly them as the update mask. " +
		"Needs the confirmation of a person for each call. On a connection with targets the property of the " +
		"audience must be a bound property or belong to a bound account, which is proven by one read of its " +
		"parent before the change. " + audienceWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "audiences", "update", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + audienceNameSchema + `,"display_name":` +
		audDisplayNameSchema + `,"description":` + audDescriptionSchema + `,"event_trigger":` + audTriggerSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(audienceSchema),
	Arguments: []capability.Argument{
		{Name: "name", Required: true, Description: "Audience resource name properties/ID/audiences/ID, from audiences.list"},
		{Name: "display_name", Description: "New display name, at most 300 characters, no control characters"},
		{Name: "description", Description: "New description, 1 to 4096 characters"},
		{Name: "event_trigger", Description: "New event to log when a user joins: event_name and log_condition " +
			"(AUDIENCE_JOINED or AUDIENCE_MEMBERSHIP_RENEWED)"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The audience after the change: " + audienceFieldsText}},
	Examples: []capability.Example{{Description: "Rename an audience",
		Arguments: json.RawMessage(`{"name":"properties/200/audiences/7000","display_name":"Purchasers 30 days"}`)}},
}

var audiencesArchive = capability.Descriptor{
	ID: audiencesArchiveID, Version: 1, Title: "Archive an Analytics audience",
	Description: "Archive an audience of a Google Analytics property (properties.audiences.archive of the Analytics " +
		"Admin API). An archived audience can no longer be used, and no tool restores it. Needs the confirmation " +
		"of a person for each call. On a connection with targets the property of the audience must be a bound " +
		"property or belong to a bound account, which is proven by one read of its parent before the change. " +
		"The answer carries no provider content, and what Google returns is untrusted data. Reads no report data." + listedOnlyNote + alphaNote,
	Tags: []string{"google", "analytics", "audiences", "archive", "alpha"}, Provider: Provider,
	RequiresToolAllowList: true,
	Risk:                  writeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + audienceNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},` +
		`"archived":{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Audience resource name " +
		"properties/ID/audiences/ID, from audiences.list"}},
	Fields: []capability.Field{{Name: "name", Description: "The audience that was archived, as asked"},
		{Name: "archived", Description: "true once Google accepted the archiving"}},
	Examples: []capability.Example{{Description: "Archive one audience",
		Arguments: json.RawMessage(`{"name":"properties/200/audiences/7000"}`)}},
}

type audienceTriggerArgument struct {
	EventName    string `json:"event_name"`
	LogCondition string `json:"log_condition"`
}

type audienceWriteArguments struct {
	Property               string                   `json:"property"`
	Name                   string                   `json:"name"`
	DisplayName            *string                  `json:"display_name"`
	Description            *string                  `json:"description"`
	MembershipDurationDays *int                     `json:"membership_duration_days"`
	FilterClauses          json.RawMessage          `json:"filter_clauses"`
	EventTrigger           *audienceTriggerArgument `json:"event_trigger"`
	ExclusionDurationMode  *string                  `json:"exclusion_duration_mode"`
}

// checkSet checks the display name, description and event trigger when they are set.
func (a audienceWriteArguments) checkSet() error {
	if a.DisplayName != nil {
		if err := checkAudienceDisplayName(*a.DisplayName); err != nil {
			return err
		}
	}
	if a.Description != nil {
		if err := checkAudienceDescription(*a.Description); err != nil {
			return err
		}
	}
	if t := a.EventTrigger; t != nil {
		if !filterEventPattern.MatchString(t.EventName) || len(t.EventName) > maxRuleNameRunes {
			return badRequest("event_trigger event_name must be 1 to 39 letters, digits or underscores and start with a letter")
		}
		if !slices.Contains(logConditions, t.LogCondition) {
			return badRequest("event_trigger log_condition must be one of " + strings.Join(logConditions, ", "))
		}
	}
	return nil
}

// changedMembers holds the changeable members that are set and the update mask of exactly them, in a fixed order.
func (a audienceWriteArguments) changedMembers() (members map[string]any, mask string) {
	members = map[string]any{}
	var paths maskOf
	paths.add(a.DisplayName != nil, "display_name")
	paths.add(a.Description != nil, "description")
	paths.add(a.EventTrigger != nil, "event_trigger")
	if a.DisplayName != nil {
		members["displayName"] = *a.DisplayName
	}
	if a.Description != nil {
		members["description"] = *a.Description
	}
	if a.EventTrigger != nil {
		members["eventTrigger"] = map[string]any{"eventName": a.EventTrigger.EventName,
			"logCondition": a.EventTrigger.LogCondition}
	}
	return members, paths.String()
}

func handleAudiencesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create audience"
	var arguments audienceWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	if arguments.DisplayName == nil || arguments.Description == nil || arguments.MembershipDurationDays == nil {
		return nil, badRequest("property, display_name, description, membership_duration_days and filter_clauses are required")
	}
	if days := *arguments.MembershipDurationDays; days < 1 || days > maxMembershipDays {
		return nil, badRequest("membership_duration_days must be 1 to 540")
	}
	if err := arguments.checkSet(); err != nil {
		return nil, err
	}
	clauses, err := checkFilterClauses(arguments.FilterClauses)
	if err != nil {
		return nil, err
	}
	body, _ := arguments.changedMembers()
	body["membershipDurationDays"] = *arguments.MembershipDurationDays
	body["filterClauses"] = clauses
	if m := arguments.ExclusionDurationMode; m != nil {
		if !slices.Contains(exclusionModes, *m) {
			return nil, badRequest("exclusion_duration_mode must be one of " + strings.Join(exclusionModes, ", "))
		}
		body["exclusionDurationMode"] = *m
	}
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > 2*maxAudFilterBodySize {
		return nil, badRequest("the audience is too large to send")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, audiencesCreateID, property, proof); err != nil {
		return nil, err
	}
	var out audience
	if err := c.change(ctx, changeCall{op: op, tool: audiencesCreateID, method: http.MethodPost,
		path: alphaPath(property + audiencesSegmentPath), body: encoded, limit: maxAudienceResourceBytes,
		notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if !inPropertyAudience(out.Name, property) {
		return nil, unsure(failure(op, subject+" answered without the name of the audience that was created"))
	}
	return out.bound(), nil
}

func handleAudiencesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update audience"
	var arguments audienceWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkAudienceName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	// The filter clauses, the membership duration and the exclusion duration mode cannot be changed.
	if len(arguments.FilterClauses) > 0 || arguments.MembershipDurationDays != nil || arguments.ExclusionDurationMode != nil {
		return nil, badRequest("filter_clauses, membership_duration_days and exclusion_duration_mode cannot be changed after creation")
	}
	body, mask := arguments.changedMembers()
	if mask == "" {
		return nil, badRequest("set at least one of display_name, description and event_trigger")
	}
	if err := arguments.checkSet(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the audience could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, audiencesUpdateID, property, proof); err != nil {
		return nil, err
	}
	var out audience
	if err := c.change(ctx, changeCall{op: op, tool: audiencesUpdateID, method: http.MethodPatch,
		path: alphaPath(arguments.Name), query: url.Values{"updateMask": {mask}}, body: encoded,
		limit: maxAudienceResourceBytes, notFound: notFoundAudience}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, unsure(failure(op, subject+" answered with another audience than the one asked for"))
	}
	return out.bound(), nil
}

func handleAudiencesArchive(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "archive audience"
	var arguments audienceNameArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkAudienceName("name", arguments.Name)
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
	if err := c.proveProperty(ctx, op, audiencesArchiveID, property, proof); err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: audiencesArchiveID, method: http.MethodPost,
		path: alphaPath(arguments.Name) + ":archive", body: []byte(`{}`), notFound: notFoundAudience}, nil); err != nil {
		return nil, err
	}
	return archiveResult{Name: arguments.Name, Archived: true}, nil
}

// audienceWriteSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func audienceWriteSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a audienceWriteArguments
		var count struct {
			Clauses []json.RawMessage `json:"filter_clauses"`
		}
		if json.Unmarshal(raw, &a) != nil || json.Unmarshal(raw, &count) != nil {
			return ""
		}
		switch tool {
		case audiencesCreateID:
			out := "Create Analytics audience"
			if a.DisplayName != nil {
				out += " " + strconv.Quote(*a.DisplayName)
			}
			out += " in " + a.Property
			if a.MembershipDurationDays != nil {
				out += ", membership " + strconv.Itoa(*a.MembershipDurationDays) + " days"
			}
			return out + ", " + strconv.Itoa(len(count.Clauses)) + " filter clauses"
		case audiencesUpdateID:
			_, mask := a.changedMembers()
			return "Change " + strings.ReplaceAll(mask, ",", ", ") + " of Analytics audience " + a.Name
		}
		return "Archive Analytics audience " + a.Name + " (it can no longer be used; no tool restores it)"
	}
}
