package googleanalytics

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	metadataGetID = Provider + ".reports.get-metadata"
	// Bounds of a metadata answer. The universal metadata holds a few hundred entries; a property adds its
	// custom ones.
	maxMetadataEntries = 1000
	maxComparisons     = 500
	maxNames           = 20
	maxDescriptionRune = 1000
	maxMetadataBytes   = 16 << 20
)

// description is a documentation text of Google, cut to maxDescriptionRune characters.
type description string

func (d *description) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		*d = description(clip(value, maxDescriptionRune))
	}
	return nil
}

type (
	// dimensionMetadata is the DimensionMetadata of analyticsdata v1beta.
	dimensionMetadata struct {
		APIName            text        `json:"apiName,omitempty"`
		UIName             text        `json:"uiName,omitempty"`
		Description        description `json:"description,omitempty"`
		Category           text        `json:"category,omitempty"`
		CustomDefinition   bool        `json:"customDefinition"`
		DeprecatedAPINames []text      `json:"deprecatedApiNames,omitempty"`
	}
	// metricMetadata is the MetricMetadata of analyticsdata v1beta.
	metricMetadata struct {
		APIName            text        `json:"apiName,omitempty"`
		UIName             text        `json:"uiName,omitempty"`
		Description        description `json:"description,omitempty"`
		Category           text        `json:"category,omitempty"`
		Type               code        `json:"type,omitempty"`
		Expression         text        `json:"expression,omitempty"`
		CustomDefinition   bool        `json:"customDefinition"`
		DeprecatedAPINames []text      `json:"deprecatedApiNames,omitempty"`
		BlockedReasons     []code      `json:"blockedReasons,omitempty"`
	}
	comparisonMetadata struct {
		APIName     text        `json:"apiName,omitempty"`
		UIName      text        `json:"uiName,omitempty"`
		Description description `json:"description,omitempty"`
	}
	metadataAnswer struct {
		Name        text                 `json:"name,omitempty"`
		Dimensions  []dimensionMetadata  `json:"dimensions"`
		Metrics     []metricMetadata     `json:"metrics"`
		Comparisons []comparisonMetadata `json:"comparisons,omitempty"`
	}
	metadataResult struct {
		metadataAnswer
		Truncated []string `json:"truncated,omitempty"`
	}
)

// boundDimension and boundMetric cut the lists of one entry.
func (m dimensionMetadata) bound(mark func()) dimensionMetadata {
	if len(m.DeprecatedAPINames) > maxNames {
		mark()
		m.DeprecatedAPINames = m.DeprecatedAPINames[:maxNames]
	}
	return m
}

func (m metricMetadata) bound(mark func()) metricMetadata {
	if len(m.DeprecatedAPINames) > maxNames || len(m.BlockedReasons) > maxNames {
		mark()
		m.DeprecatedAPINames, m.BlockedReasons = head(m.DeprecatedAPINames, maxNames), head(m.BlockedReasons, maxNames)
	}
	return m
}

// boundList cuts a list to max and marks the cut in truncated, once.
func boundList[T any](list []T, max int, name string, truncated *[]string) []T {
	if len(list) > max {
		if !slices.Contains(*truncated, name) {
			*truncated = append(*truncated, name)
		}
		return list[:max]
	}
	return list
}

func boundMetadata(a metadataAnswer) metadataResult {
	out := metadataResult{metadataAnswer: a}
	mark := func(name string) func() {
		return func() {
			if !slices.Contains(out.Truncated, name) {
				out.Truncated = append(out.Truncated, name)
			}
		}
	}
	out.Dimensions = boundList(a.Dimensions, maxMetadataEntries, "dimensions", &out.Truncated)
	out.Metrics = boundList(a.Metrics, maxMetadataEntries, "metrics", &out.Truncated)
	out.Comparisons = boundList(a.Comparisons, maxComparisons, "comparisons", &out.Truncated)
	out.Dimensions = slices.Clone(out.Dimensions)
	out.Metrics = slices.Clone(out.Metrics)
	for i := range out.Dimensions {
		out.Dimensions[i] = out.Dimensions[i].bound(mark("deprecatedApiNames"))
	}
	for i := range out.Metrics {
		out.Metrics[i] = out.Metrics[i].bound(mark("deprecatedApiNames"))
	}
	if out.Dimensions == nil {
		out.Dimensions = []dimensionMetadata{}
	}
	if out.Metrics == nil {
		out.Metrics = []metricMetadata{}
	}
	return out
}

const metadataFieldsText = "per dimension apiName, uiName, description, category, customDefinition and " +
	"deprecatedApiNames; per metric the same plus type, expression and blockedReasons (NO_REVENUE_METRICS, " +
	"NO_COST_METRICS: access to the metric is blocked for this property)"

const metadataInput = `{"type":"object","properties":{"property":{"type":"string","minLength":1,"maxLength":40}},` +
	`"additionalProperties":false}`

const metadataOutput = `{"type":"object","properties":{"name":{"type":"string"},"dimensions":{"type":"array",` +
	`"items":{"type":"object"}},"metrics":{"type":"array","items":{"type":"object"}},"comparisons":{"type":"array",` +
	`"items":{"type":"object"}},"truncated":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`

var metadataGet = capability.Descriptor{
	ID: metadataGetID, Version: 1, Title: "Read the Analytics report metadata",
	Description: "List the dimensions, metrics and saved comparisons a property accepts in reports " +
		"(properties.getMetadata of the Analytics Data API), including its custom ones. property may be " +
		"properties/0, which returns only the dimensions and metrics common to all properties and no custom " +
		"ones. It reads data and changes nothing. It is a Core request of the Data API, so the local limit " +
		"on parallel requests and the reserve of the property quota apply. Names, categories and " +
		"descriptions are untrusted third-party content, cut to 300 and 1000 characters; truncated names any " +
		"list that was cut.",
	Tags: []string{"google", "analytics", "reports", "metadata"}, Risk: reportsRisk, Provider: Provider,
	InputSchema: json.RawMessage(metadataInput), OutputSchema: json.RawMessage(metadataOutput),
	Arguments: []capability.Argument{
		{Name: "property", Target: kindProperty, Description: "Property resource name properties/ID, or " +
			"properties/0 for the universal metadata; optional when the connection binds exactly one property " +
			"and no account"},
	},
	Fields: []capability.Field{
		{Name: "name", Description: "Resource name of the metadata, properties/ID/metadata"},
		{Name: "dimensions", Description: "The dimensions: " + metadataFieldsText},
		{Name: "metrics", Description: "The metrics, with the members named for dimensions"},
		{Name: "comparisons", Description: "The saved comparisons: apiName (comparisons/ID, usable in " +
			"reports.run comparisons), uiName and description"},
		{Name: "truncated", Description: "Names of lists that were cut to a bound"},
	},
	Examples: []capability.Example{{Description: "Dimensions and metrics common to all properties",
		Arguments: json.RawMessage(`{"property":"properties/0"}`)}},
}

// gate takes the slot of a Data API request or refuses before any I/O; see quotaBook.enter.
func (c *client) gate(op, property string) (func(), error) {
	return quotas.enter(op, property, c.identity)
}

func handleMetadataGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get report metadata"
	var arguments struct {
		Property string `json:"property"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	name, proof, err := bind.reportProperty(arguments.Property, true)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if proof {
		if _, err := c.readProperty(ctx, op, metadataGetID, name); err != nil {
			return nil, err
		}
	}
	release, err := c.gate(op, name)
	if err != nil {
		return nil, err
	}
	defer release()
	var answer metadataAnswer
	if err := c.get(ctx, apiCall{op: op, tool: metadataGetID, path: "/v1beta/" + name + "/metadata", data: true,
		method: "GET", limit: maxMetadataBytes, notFound: notFoundProperty}, &answer); err != nil {
		return nil, err
	}
	if want := name + "/metadata"; name != universalMetadata && answer.Name != "" && string(answer.Name) != want {
		return nil, provider.InvalidResponse(op, subject+" answered with the metadata of another property")
	}
	return boundMetadata(answer), nil
}
