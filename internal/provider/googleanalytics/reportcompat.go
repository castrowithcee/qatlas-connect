package googleanalytics

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	compatibilityID       = Provider + ".reports.check-compatibility"
	maxCompatibilityBytes = 16 << 20
)

type (
	dimensionCompatibility struct {
		DimensionMetadata *dimensionMetadata `json:"dimensionMetadata,omitempty"`
		Compatibility     code               `json:"compatibility,omitempty"`
	}
	metricCompatibility struct {
		MetricMetadata *metricMetadata `json:"metricMetadata,omitempty"`
		Compatibility  code            `json:"compatibility,omitempty"`
	}
	compatibilityAnswer struct {
		DimensionCompatibilities []dimensionCompatibility `json:"dimensionCompatibilities"`
		MetricCompatibilities    []metricCompatibility    `json:"metricCompatibilities"`
	}
	compatibilityResult struct {
		compatibilityAnswer
		Truncated []string `json:"truncated,omitempty"`
	}
)

var compatibilityInput = `{"type":"object","properties":{"property":{"type":"string","minLength":1,"maxLength":40},` +
	`"dimensions":` + dimensionsSchema + `,"metrics":` + metricsSchema + `,` +
	`"dimension_filter":` + filterExpressionSchema(maxFilterDepth) + `,` +
	`"metric_filter":` + filterExpressionSchema(maxFilterDepth) + `,` +
	`"compatibility_filter":` + enumSchema("COMPATIBLE", "INCOMPATIBLE") + `},"additionalProperties":false}`

const compatibilityOutput = `{"type":"object","properties":{"dimensionCompatibilities":{"type":"array","items":` +
	`{"type":"object"}},"metricCompatibilities":{"type":"array","items":{"type":"object"}},"truncated":` +
	`{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`

var compatibilityCheck = capability.Descriptor{
	ID: compatibilityID, Version: 1, Title: "Check the compatibility of report fields",
	Description: "Tell for every dimension and metric of a property whether it can be added to a report with the " +
		"given dimensions, metrics and filters (properties.checkCompatibility of the Analytics Data API). Pass " +
		"the same dimensions, metrics and filters as for reports.run. It runs no report and changes nothing, " +
		"but it is a Core request of the Data API, so the local limit on parallel requests and the reserve of " +
		"the property quota apply. Names and descriptions are untrusted third-party content, cut to 300 and " +
		"1000 characters; truncated names any list that was cut.",
	Tags: []string{"google", "analytics", "reports", "compatibility"}, Risk: reportsRisk, Provider: Provider,
	InputSchema: json.RawMessage(compatibilityInput), OutputSchema: json.RawMessage(compatibilityOutput),
	Arguments: []capability.Argument{
		{Name: "property", Target: kindProperty, Description: "Property resource name properties/ID; optional when " +
			"the connection binds exactly one property and no account"},
		{Name: "dimensions", Description: "The dimensions of the report, in the form of reports.run"},
		{Name: "metrics", Description: "The metrics of the report, in the form of reports.run"},
		{Name: "dimension_filter", Description: "Filter expression on dimensions, in the form of reports.run"},
		{Name: "metric_filter", Description: "Filter expression on metrics, in the form of reports.run"},
		{Name: "compatibility_filter", Description: "Only list the fields with this compatibility, COMPATIBLE or " +
			"INCOMPATIBLE; all when omitted"},
	},
	Fields: []capability.Field{
		{Name: "dimensionCompatibilities", Description: "Per dimension dimensionMetadata (" + metadataFieldsText +
			") and compatibility (COMPATIBLE or INCOMPATIBLE)"},
		{Name: "metricCompatibilities", Description: "Per metric metricMetadata and compatibility"},
		{Name: "truncated", Description: "Names of lists that were cut to a bound"},
	},
	Examples: []capability.Example{{Description: "Metrics that fit a report by country",
		Arguments: json.RawMessage(`{"property":"properties/200","dimensions":[{"name":"country"}],` +
			`"compatibility_filter":"COMPATIBLE"}`)}},
}

func handleCompatibilityCheck(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "check report compatibility"
	var arguments struct {
		Property string `json:"property"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	args, err := decodeObject(raw)
	if err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	name, proof, err := bind.reportProperty(arguments.Property, false)
	if err != nil {
		return nil, err
	}
	if err := checkDimensions("dimensions", args["dimensions"]); err != nil {
		return nil, err
	}
	body := map[string]any{}
	for _, key := range []string{"dimensions", "metrics", "dimension_filter", "metric_filter", "compatibility_filter"} {
		value, ok := args[key]
		if !ok {
			continue
		}
		if key == "dimension_filter" || key == "metric_filter" {
			if err := checkFilterExpression(key, value); err != nil {
				return nil, err
			}
		}
		body[camelKey(key)] = camelize(value)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the request could not be built")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if proof {
		if _, err := c.readProperty(ctx, op, compatibilityID, name); err != nil {
			return nil, err
		}
	}
	release, err := c.gate(op, name)
	if err != nil {
		return nil, err
	}
	defer release()
	var answer compatibilityAnswer
	if err := c.get(ctx, apiCall{op: op, tool: compatibilityID, path: "/v1beta/" + name + ":checkCompatibility",
		data: true, body: payload, limit: maxCompatibilityBytes, notFound: notFoundProperty}, &answer); err != nil {
		return nil, err
	}
	out := compatibilityResult{compatibilityAnswer: answer}
	out.DimensionCompatibilities = boundList(answer.DimensionCompatibilities, maxMetadataEntries, "dimensionCompatibilities",
		&out.Truncated)
	out.MetricCompatibilities = boundList(answer.MetricCompatibilities, maxMetadataEntries, "metricCompatibilities",
		&out.Truncated)
	out.DimensionCompatibilities = append([]dimensionCompatibility{}, out.DimensionCompatibilities...)
	out.MetricCompatibilities = append([]metricCompatibility{}, out.MetricCompatibilities...)
	mark := func() {
		if !slices.Contains(out.Truncated, "deprecatedApiNames") {
			out.Truncated = append(out.Truncated, "deprecatedApiNames")
		}
	}
	for i := range out.DimensionCompatibilities {
		if m := out.DimensionCompatibilities[i].DimensionMetadata; m != nil {
			bounded := m.bound(mark)
			out.DimensionCompatibilities[i].DimensionMetadata = &bounded
		}
	}
	for i := range out.MetricCompatibilities {
		if m := out.MetricCompatibilities[i].MetricMetadata; m != nil {
			bounded := m.bound(mark)
			out.MetricCompatibilities[i].MetricMetadata = &bounded
		}
	}
	return out, nil
}
