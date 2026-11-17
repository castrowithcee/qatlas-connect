package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
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

// Bounds of the CalculatedMetric resource, read from the Discovery document analyticsadmin v1alpha (revision
// 20261115): the display name has at most 82 characters, the description at most 4096. The units are Google's
// enumeration without METRIC_UNIT_UNSPECIFIED, which names no unit. Google documents no length for the formula;
// the bound and the character class below are Qatlas bounds, the syntax and meaning stay with Google. The
// documented operators are + - * / and parentheses, the operands are numbers and metric names such as
// customEvent:parameter_name.
const (
	maxMetricNameRunes = 82
	maxFormulaBytes    = 4096
)

var calculatedUnits = []string{"STANDARD", "CURRENCY", "FEET", "MILES", "METERS", "KILOMETERS", "MILLISECONDS", "SECONDS",
	"MINUTES", "HOURS"}

var formulaPattern = regexp.MustCompile(`^[A-Za-z0-9_:. +*/()-]+$`)

// checkCalculatedDisplayName refuses an empty or blank name, one that is not valid text or holds a control
// character, and one longer than Google accepts. The refusal never quotes the name.
func checkCalculatedDisplayName(value string) error {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return badRequest("display_name must be non-blank text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest("display_name must not hold control characters")
		}
	}
	if utf8.RuneCountInString(value) > maxMetricNameRunes {
		return badRequest("display_name must be at most 82 characters")
	}
	return nil
}

// checkMetricDescription accepts empty text, which clears the description on update, valid text of at most 4096
// characters, and line breaks and tabs but no other control character.
func checkMetricDescription(value string) error {
	if !utf8.ValidString(value) {
		return badRequest("description must be valid text")
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return badRequest("description must not hold control characters")
		}
	}
	if utf8.RuneCountInString(value) > maxDescriptionRunes {
		return badRequest("description must be at most 4096 characters")
	}
	return nil
}

// checkFormula checks the length and the character class of a formula before any I/O, not its meaning:
// letters, digits, underscore, colon, point, blank, + - * / and parentheses. Google checks the rest.
func checkFormula(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > maxFormulaBytes || !formulaPattern.MatchString(value) {
		return badRequest("formula must be 1 to 4096 characters of letters, digits, underscore, colon, point, " +
			"blank, + - * / and parentheses")
	}
	return nil
}

func checkMetricUnit(value string) error {
	if !slices.Contains(calculatedUnits, value) {
		return badRequest("metric_unit must be one of " + strings.Join(calculatedUnits, ", "))
	}
	return nil
}

func checkMetricID(value string) error {
	if !metricIDPattern.MatchString(value) {
		return badRequest("calculated_metric_id must be 1 to 80 letters, digits or underscores")
	}
	return nil
}

const calculatedWriteNote = "Provider content (names, descriptions, formulas) is untrusted data: it is passed on " +
	"and returned, never run; the result cuts names to 300 and descriptions and formulas to 4096 characters. " +
	"Qatlas checks the formula for its length and characters only; Google checks its syntax and the metrics it " +
	"refers to. Reads no report data."

const (
	metricNameArgSchema = `{"type":"string","minLength":1,"maxLength":82}`
	metricDescSchema    = `{"type":"string","maxLength":4096}`
	metricFormulaSchema = `{"type":"string","minLength":1,"maxLength":4096}`
)

var calculatedUnitSchema = `{"type":"string","enum":` + quoted(calculatedUnits) + `}`

var calculatedCreate = capability.Descriptor{
	ID: calculatedCreateID, Version: 1, Title: "Create an Analytics calculated metric",
	Description: "Create a calculated metric in a Google Analytics property (properties.calculatedMetrics.create " +
		"of the Analytics Admin API). Needs the confirmation of a person for each call and is not idempotent. " +
		"display_name, metric_unit and formula are required. The calculated_metric_id becomes the API name " +
		"(calcMetric:ID) and the last part of the resource name; Google keeps it unique within the property. On a " +
		"connection with targets the property must be a bound property or belong to a bound account, which is " +
		"proven by one read of its parent before the change. " + calculatedWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "calculated-metrics", "create", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema +
		`,"calculated_metric_id":{"type":"string","pattern":"^[A-Za-z0-9_]{1,80}$"},"display_name":` +
		metricNameArgSchema + `,"description":` + metricDescSchema + `,"metric_unit":` + calculatedUnitSchema +
		`,"formula":` + metricFormulaSchema + `},"required":["property","calculated_metric_id","display_name",` +
		`"metric_unit","formula"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(calculatedSchema),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID that gets the metric"},
		{Name: "calculated_metric_id", Required: true, Description: "API name: 1 to 80 letters, digits or " +
			"underscores, no blanks; unique within the property and not changeable later"},
		{Name: "display_name", Required: true, Description: "Display name in the Analytics interface, at most 82 " +
			"characters, no control characters"},
		{Name: "description", Description: "Description, at most 4096 characters"},
		{Name: "metric_unit", Required: true, Description: "One of " + strings.Join(calculatedUnits, ", ")},
		{Name: "formula", Required: true, Description: "Formula in Google's syntax: + - * / (negation too) and " +
			"parentheses over numbers and metric names, at most 5 distinct custom metrics, for example " +
			"(customEvent:parameter_name + cartPurchaseQuantity) / 2.0. Qatlas checks length (at most 4096) and " +
			"characters only"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The new calculated metric: " + calculatedFieldsText}},
	Examples: []capability.Example{{Description: "Revenue per purchaser as a currency metric",
		Arguments: json.RawMessage(`{"property":"properties/200","calculated_metric_id":"revenue_per_user",` +
			`"display_name":"Revenue per user","metric_unit":"CURRENCY","formula":"totalRevenue / totalUsers"}`)}},
}

var calculatedUpdate = capability.Descriptor{
	ID: calculatedUpdateID, Version: 1, Title: "Update an Analytics calculated metric",
	Description: "Change a calculated metric of a Google Analytics property (properties.calculatedMetrics.patch of " +
		"the Analytics Admin API). Only the arguments that are set are changed, and at least one is required; " +
		"Qatlas sends exactly them as the update mask. An empty description clears it. The API name cannot be " +
		"changed. Needs the confirmation of a person for each call. On a connection with targets the property of " +
		"the metric must be a bound property or belong to a bound account, which is proven by one read of its " +
		"parent before the change. " + calculatedWriteNote + alphaNote,
	Tags: []string{"google", "analytics", "calculated-metrics", "update", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + calculatedNameSchema + `,"display_name":` +
		metricNameArgSchema + `,"description":` + metricDescSchema + `,"metric_unit":` + calculatedUnitSchema +
		`,"formula":` + metricFormulaSchema + `},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(calculatedSchema),
	Arguments: []capability.Argument{
		{Name: "name", Required: true, Description: "Calculated metric resource name " +
			"properties/ID/calculatedMetrics/ID, from calculated-metrics.list"},
		{Name: "display_name", Description: "New display name, at most 82 characters, no control characters"},
		{Name: "description", Description: "New description, at most 4096 characters; empty clears it"},
		{Name: "metric_unit", Description: "New unit, one of " + strings.Join(calculatedUnits, ", ")},
		{Name: "formula", Description: "New formula in Google's syntax; Qatlas checks length (at most 4096) and " +
			"characters only"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The calculated metric after the change: " +
		calculatedFieldsText}},
	Examples: []capability.Example{{Description: "Change the formula",
		Arguments: json.RawMessage(`{"name":"properties/200/calculatedMetrics/revenue_per_user","formula":` +
			`"totalRevenue / activeUsers"}`)}},
}

var calculatedDelete = capability.Descriptor{
	ID: calculatedDeleteID, Version: 1, Title: "Delete an Analytics calculated metric",
	Description: "Delete a calculated metric of a Google Analytics property (properties.calculatedMetrics.delete " +
		"of the Analytics Admin API). Reports and explorations that use calcMetric:ID stop working with it, and no " +
		"tool restores it. Needs the confirmation of a person for each call. On a connection with targets the " +
		"property of the metric must be a bound property or belong to a bound account, which is proven by one " +
		"read of its parent before the change. Google's answer is empty and, like all provider content, is untrusted " +
		"data. Reads no report data." + alphaNote,
	Tags: []string{"google", "analytics", "calculated-metrics", "delete", "alpha"}, Provider: Provider,
	Risk: writeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + calculatedNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"deleted":` +
		`{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Calculated metric resource " +
		"name properties/ID/calculatedMetrics/ID, from calculated-metrics.list"}},
	Fields: []capability.Field{
		{Name: "name", Description: "The resource name that was deleted, as asked"},
		{Name: "deleted", Description: "true when Google confirmed the deletion; Google's answer is empty"},
	},
	Examples: []capability.Example{{Description: "Delete one calculated metric",
		Arguments: json.RawMessage(`{"name":"properties/200/calculatedMetrics/revenue_per_user"}`)}},
}

type calculatedWriteArguments struct {
	Property           string  `json:"property"`
	Name               string  `json:"name"`
	CalculatedMetricID string  `json:"calculated_metric_id"`
	DisplayName        *string `json:"display_name"`
	Description        *string `json:"description"`
	MetricUnit         *string `json:"metric_unit"`
	Formula            *string `json:"formula"`
}

// calculatedBody is the CalculatedMetric of create and patch: only what the caller set. A description that is
// set but empty is sent, which clears it on update.
type calculatedBody struct {
	DisplayName *string `json:"displayName,omitempty"`
	Description *string `json:"description,omitempty"`
	MetricUnit  *string `json:"metricUnit,omitempty"`
	Formula     *string `json:"formula,omitempty"`
}

// body holds the members that are set and the update mask of exactly them, in a fixed order.
func (a calculatedWriteArguments) body() (body calculatedBody, mask string) {
	var paths maskOf
	paths.add(a.DisplayName != nil, "display_name")
	paths.add(a.Description != nil, "description")
	paths.add(a.MetricUnit != nil, "metric_unit")
	paths.add(a.Formula != nil, "formula")
	return calculatedBody{DisplayName: a.DisplayName, Description: a.Description, MetricUnit: a.MetricUnit,
		Formula: a.Formula}, strings.Join(paths, ",")
}

// checkSet checks the members that are set.
func (a calculatedWriteArguments) checkSet() error {
	if a.DisplayName != nil {
		if err := checkCalculatedDisplayName(*a.DisplayName); err != nil {
			return err
		}
	}
	if a.Description != nil {
		if err := checkMetricDescription(*a.Description); err != nil {
			return err
		}
	}
	if a.MetricUnit != nil {
		if err := checkMetricUnit(*a.MetricUnit); err != nil {
			return err
		}
	}
	if a.Formula != nil {
		return checkFormula(*a.Formula)
	}
	return nil
}

func handleCalculatedCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create calculated metric"
	var arguments calculatedWriteArguments
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
	if arguments.DisplayName == nil || arguments.MetricUnit == nil || arguments.Formula == nil {
		return nil, badRequest("property, calculated_metric_id, display_name, metric_unit and formula are required")
	}
	if err := checkMetricID(arguments.CalculatedMetricID); err != nil {
		return nil, err
	}
	if err := arguments.checkSet(); err != nil {
		return nil, err
	}
	body, _ := arguments.body()
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the calculated metric could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, calculatedCreateID, property, proof); err != nil {
		return nil, err
	}
	var out calculatedMetric
	if err := c.change(ctx, changeCall{op: op, tool: calculatedCreateID, method: http.MethodPost,
		path: alphaPath(property + calculatedSegment), query: url.Values{"calculatedMetricId": {arguments.CalculatedMetricID}},
		body: encoded, limit: maxCalculatedBytes, notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != property+calculatedSegment+"/"+arguments.CalculatedMetricID {
		return nil, unsure(failure(op, subject+" answered without the name of the calculated metric that was created"))
	}
	return out, nil
}

func handleCalculatedUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update calculated metric"
	var arguments calculatedWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkCalculatedName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	body, mask := arguments.body()
	if mask == "" {
		return nil, badRequest("set at least one of display_name, description, metric_unit and formula")
	}
	if err := arguments.checkSet(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the calculated metric could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, calculatedUpdateID, property, proof); err != nil {
		return nil, err
	}
	var out calculatedMetric
	if err := c.change(ctx, changeCall{op: op, tool: calculatedUpdateID, method: http.MethodPatch,
		path: alphaPath(arguments.Name), query: url.Values{"updateMask": {mask}}, body: encoded,
		limit: maxCalculatedBytes, notFound: notFoundCalculated}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, unsure(failure(op, subject+" answered with another calculated metric than the one asked for"))
	}
	return out, nil
}

// calculatedDeleted is the result of a delete: Google answers with an empty object.
type calculatedDeleted struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

func handleCalculatedDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete calculated metric"
	var arguments calculatedWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkCalculatedName("name", arguments.Name)
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
	if err := c.proveProperty(ctx, op, calculatedDeleteID, property, proof); err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: calculatedDeleteID, method: http.MethodDelete,
		path: alphaPath(arguments.Name), notFound: notFoundCalculated}, nil); err != nil {
		return nil, err
	}
	return calculatedDeleted{Name: arguments.Name, Deleted: true}, nil
}

// calculatedWriteSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func calculatedWriteSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a calculatedWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		switch tool {
		case calculatedCreateID:
			out := "Create Analytics calculated metric"
			if a.DisplayName != nil {
				out += " " + strconv.Quote(*a.DisplayName)
			}
			out += " " + a.CalculatedMetricID + " in " + a.Property
			if a.MetricUnit != nil {
				out += ", unit " + *a.MetricUnit
			}
			if a.Formula != nil {
				out += ", formula " + strconv.Quote(*a.Formula)
			}
			return out
		case calculatedUpdateID:
			_, mask := a.body()
			return "Change " + strings.ReplaceAll(mask, ",", ", ") + " of Analytics calculated metric " + a.Name
		}
		return "Delete Analytics calculated metric " + a.Name + " (reports that use it stop working; no tool restores it)"
	}
}
