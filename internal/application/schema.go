package application

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/castrowithcee/qatlas-kit/jsonschema"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// ValidateJSON checks raw against an input schema with the kit's strict mode; the kit names the first
// violation by its path, such as "$.foo is not allowed".
func ValidateJSON(schema, raw json.RawMessage) error {
	return jsonschema.ValidateJSON(schema, raw)
}

// validateOutput checks a decoded provider response against an output schema. The kit's lenient mode does
// not refuse a response for member counts, uniqueness, anyOf or schemas under additionalProperties.
func validateOutput(schema json.RawMessage, value any) error {
	return jsonschema.Validate(schema, value, jsonschema.Lenient)
}

// CompactDescriptor is the contract describe publishes unless the complete descriptor is asked for:
// everything invoking a tool needs, with both schemas condensed into tables instead of being repeated
// beside them. Arguments has one row per argument, derived from the input schema, and Fields names every
// top-level field of the result, derived from the output schema; the fields of a result that is an array
// are those of its entries. The members of the risk stand beside the others rather than in an object of
// their own, and all are ordered by meaning: the identifier, its descriptions, the risk, then the tables.
//
// SelectableFields names the members of the result list entries that invoke can select with fields. For a
// tool with default fields, Fields lists only those and SelectableFields names the other top-level members,
// which fields or full returns. It is left out otherwise.
//
// The provider is the prefix of the ID, the tags serve the search that found the tool, and whether a tool
// needs a tools list that names it only decides which connections offer it, which the connections beside
// the contract answer; these, the types of the result fields, and both schemas stay in the complete
// descriptor. Arguments are always validated against the complete input schema, not against this view.
type CompactDescriptor struct {
	ID               string                  `json:"id"`
	Version          int                     `json:"version"`
	Title            string                  `json:"title"`
	Description      string                  `json:"description"`
	Effect           capability.Effect       `json:"effect"`
	Idempotency      capability.Idempotency  `json:"idempotency"`
	Confirmation     capability.Confirmation `json:"confirmation"`
	OpenWorld        bool                    `json:"open_world"`
	DataSensitivity  string                  `json:"data_sensitivity"`
	Arguments        []ArgumentRow           `json:"arguments"`
	Fields           []capability.Field      `json:"fields"`
	SelectableFields []string                `json:"selectable_fields,omitempty"`
	Examples         []capability.Example    `json:"examples"`
}

// ArgumentRow is one argument of a compact contract, derived from the input schema. A member of an object
// argument has a row of its own, named with a dot, such as address.city, and inside an array with [], such
// as line_items[].name; Required then means required within that object. Type is the schema type, with []
// for an array of that type. Form lists the allowed values separated by |, or names the written form a
// pattern checks, and is empty otherwise. Limits names the bounds: a range such as 1..100 for a number,
// with len for the characters of a string, items for the entries of an array, and keys for the members of
// an object, and each for the bounds of every entry.
type ArgumentRow struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Form        string `json:"form"`
	Limits      string `json:"limits"`
	Description string `json:"description"`
}

// Compact condenses a descriptor into its compact contract. The rows follow the order of the descriptor's
// Arguments and Fields, which list the important names first; members the schemas declare beyond them
// follow in name order, and the members of an object argument list its required ones first.
func Compact(d capability.Descriptor) CompactDescriptor {
	argumentText := map[string]string{}
	var argumentOrder []string
	for _, argument := range d.Arguments {
		argumentText[argument.Name] = argument.Description
		argumentOrder = append(argumentOrder, argument.Name)
	}
	arguments := []ArgumentRow{}
	input := parseSchema(d.InputSchema)
	for _, name := range memberOrder(input, argumentOrder) {
		arguments = input.argumentRows(arguments, name, name, argumentText[name])
	}

	// The descriptor's fields keep their descriptions and come first; the other top-level fields the output
	// schema declares follow with the schema's own description, which is usually none.
	fields := append([]capability.Field{}, d.Fields...)
	listed := map[string]bool{}
	for _, field := range d.Fields {
		listed[field.Name] = true
	}
	result := parseSchema(d.OutputSchema)
	if result.Items != nil {
		result = *result.Items
	}
	for _, name := range memberOrder(result, nil) {
		if !listed[name] {
			fields = append(fields, capability.Field{Name: name, Description: result.Properties[name].Description})
		}
	}
	selectable := SelectableFields(d)
	if len(d.DefaultFields) > 0 {
		members := resultMembers(d.OutputSchema)
		shown := fields[:0:0]
		for _, field := range fields {
			if keptWith(field.Name, d.DefaultFields) {
				shown = append(shown, field)
			} else if containsString(members, field.Name) {
				selectable = append(selectable, field.Name)
			}
		}
		fields = shown
	}

	return CompactDescriptor{
		ID: d.ID, Version: d.Version, Title: d.Title, Description: d.Description,
		Effect: d.Risk.Effect, Idempotency: d.Risk.Idempotency, Confirmation: d.Risk.Confirmation,
		OpenWorld: d.Risk.OpenWorld, DataSensitivity: d.Risk.DataSensitivity,
		Arguments: arguments, Fields: fields,
		SelectableFields: selectable,
		Examples:         append([]capability.Example{}, d.Examples...),
	}
}

// requiredArguments names the required top-level arguments of d as the compact contract lists them, each
// followed by :form where it has one. Members of an object argument are not listed: they are
// required only within that object.
func requiredArguments(d capability.Descriptor) []string {
	var required []string
	for _, row := range Compact(d).Arguments {
		if !row.Required || strings.ContainsAny(row.Name, ".[") {
			continue
		}
		if row.Form != "" {
			required = append(required, row.Name+":"+row.Form)
		} else {
			required = append(required, row.Name)
		}
	}
	return required
}

// schemaNode is the part of a JSON schema the compact contract reads. A bound that is absent stays empty.
type schemaNode struct {
	Type          string                 `json:"type"`
	AnyOf         []*schemaNode          `json:"anyOf"`
	Description   string                 `json:"description"`
	Enum          []json.RawMessage      `json:"enum"`
	Form          string                 `json:"x-form"`
	Properties    map[string]*schemaNode `json:"properties"`
	Required      []string               `json:"required"`
	Items         *schemaNode            `json:"items"`
	Minimum       json.Number            `json:"minimum"`
	Maximum       json.Number            `json:"maximum"`
	MinLength     json.Number            `json:"minLength"`
	MaxLength     json.Number            `json:"maxLength"`
	MinItems      json.Number            `json:"minItems"`
	MaxItems      json.Number            `json:"maxItems"`
	MinProperties json.Number            `json:"minProperties"`
	MaxProperties json.Number            `json:"maxProperties"`
	UniqueItems   bool                   `json:"uniqueItems"`
}

// parseSchema reads a schema for the compact contract. A schema it cannot read yields no rows; describe
// still answers, and the complete descriptor shows the schema as it is.
func parseSchema(raw json.RawMessage) schemaNode {
	var node schemaNode
	_ = json.Unmarshal(raw, &node)
	return node
}

// memberOrder lists the members of an object schema: the names of first that it declares, in that order,
// then the others by name.
func memberOrder(node schemaNode, first []string) []string {
	names := make([]string, 0, len(node.Properties))
	listed := map[string]bool{}
	for _, name := range first {
		if _, ok := node.Properties[name]; ok && !listed[name] {
			names = append(names, name)
			listed[name] = true
		}
	}
	rest := make([]string, 0, len(node.Properties))
	for name := range node.Properties {
		if !listed[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(names, rest...)
}

// members returns the object schema whose members a row of node expands into, with the name prefix of
// those rows, or nil when node is neither an object nor an array of objects that declares members.
func (n schemaNode) members(name string) (*schemaNode, string) {
	switch {
	case len(n.Properties) > 0:
		return &n, name + "."
	case n.Items != nil && len(n.Items.Properties) > 0:
		return n.Items, name + "[]."
	}
	return nil, ""
}

// argumentRows appends the row of the member key of n under the name path, then the rows of its own
// members.
func (n schemaNode) argumentRows(rows []ArgumentRow, key, path, description string) []ArgumentRow {
	node := n.Properties[key]
	if node == nil {
		return rows
	}
	if description == "" {
		description = node.Description
	}
	rows = append(rows, ArgumentRow{
		Name: path, Type: node.typeName(), Required: contains(n.Required, key), Form: node.form(),
		Limits: node.limits(), Description: description,
	})
	if object, prefix := node.members(path); object != nil {
		for _, name := range memberOrder(*object, object.Required) {
			rows = object.argumentRows(rows, name, prefix+name, "")
		}
	}
	return rows
}

// typeName is the schema type, with [] for an array of that type, the types of the branches separated by |
// for a union, and any where the schema names none.
func (n schemaNode) typeName() string {
	if n.Type == "array" && n.Items != nil {
		item := n.Items.typeName()
		if strings.Contains(item, "|") {
			item = "(" + item + ")"
		}
		return item + "[]"
	}
	if n.Type == "" && len(n.AnyOf) > 0 {
		var names []string
		for _, branch := range n.AnyOf {
			if branch == nil || branch.typeName() == "any" {
				return "any"
			}
			if name := branch.typeName(); !contains(names, name) {
				names = append(names, name)
			}
		}
		return strings.Join(names, "|")
	}
	if n.Type == "" {
		return "any"
	}
	return n.Type
}

// form lists the allowed values of a value, or of every entry of an array, or names its written form.
func (n schemaNode) form() string {
	if n.Type == "array" && n.Items != nil && len(n.Enum) == 0 && n.Form == "" {
		return n.Items.form()
	}
	if len(n.Enum) > 0 {
		values := make([]string, len(n.Enum))
		for i, raw := range n.Enum {
			var text string
			if json.Unmarshal(raw, &text) != nil {
				text = string(raw)
			}
			values[i] = text
		}
		return strings.Join(values, "|")
	}
	return n.Form
}

// limits names the bounds of a value; see ArgumentRow.
func (n schemaNode) limits() string {
	var parts []string
	add := func(label string, minimum, maximum json.Number) {
		var bound string
		switch {
		case minimum != "" && minimum == maximum:
			bound = string(minimum)
		case minimum != "" && maximum != "":
			bound = string(minimum) + ".." + string(maximum)
		case minimum != "":
			bound = "min " + string(minimum)
		case maximum != "":
			bound = "max " + string(maximum)
		default:
			return
		}
		if label != "" {
			bound = label + " " + bound
		}
		parts = append(parts, bound)
	}
	add("", n.Minimum, n.Maximum)
	add("len", n.MinLength, n.MaxLength)
	add("items", n.MinItems, n.MaxItems)
	add("keys", n.MinProperties, n.MaxProperties)
	if n.UniqueItems {
		parts = append(parts, "unique")
	}
	if n.Type == "array" && n.Items != nil && len(n.Items.Properties) == 0 {
		if each := n.Items.limits(); each != "" {
			parts = append(parts, "each "+each)
		}
	}
	return strings.Join(parts, "; ")
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
