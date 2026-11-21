package googlemerchant

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// A node describes one member type of an answer whose members are too many to hold as Go structs, such as the
// product attributes. The answer is read against the node: a member the node does not list is never held, a
// value of the wrong type is dropped, texts are cut, and lists keep a bounded number of entries. The output
// schema of a tool is written from the same nodes, so schema and filter cannot drift apart.
type node struct {
	kind   nodeKind
	limit  int // runes of a text
	fields []field
	item   *node
}

type nodeKind int

const (
	nodeText nodeKind = iota
	nodeBool
	nodeNumber
	nodeInt // a 64-bit integer, which the API writes as a string
	nodeObject
	nodeList
)

type field struct {
	name string
	node *node
}

// maxTextRunes is the cut of descriptions and other long free text. maxDepth bounds the nesting of objects,
// which only the groups of custom attributes can make deeper than the document.
const (
	maxTextRunes = 1000
	maxDepth     = 6
)

var (
	tID   = &node{kind: nodeText, limit: maxIDRunes}   // codes, enum values, times, units
	tName = &node{kind: nodeText, limit: maxNameRunes} // titles and other free text
	tLong = &node{kind: nodeText, limit: maxTextRunes} // descriptions
	tURL  = &node{kind: nodeText, limit: maxURLRunes}  // links, never fetched
	tBool = &node{kind: nodeBool}
	tNum  = &node{kind: nodeNumber}
	tInt  = &node{kind: nodeInt}
)

func f(name string, n *node) field { return field{name: name, node: n} }
func obj(fields ...field) *node    { return &node{kind: nodeObject, fields: fields} }
func list(item *node) *node        { return &node{kind: nodeList, item: item} }

var integerForm = regexp.MustCompile(`^-?[0-9]{1,20}$`)

// clean reads raw against the node. It reports false for a value that is missing, of the wrong type, or empty.
func (n *node) clean(raw json.RawMessage, depth int) (any, bool) {
	switch n.kind {
	case nodeText:
		text := clipJSON(raw, n.limit)
		return text, text != ""
	case nodeBool:
		var v *bool
		if json.Unmarshal(raw, &v) != nil || v == nil {
			return nil, false
		}
		return *v, true
	case nodeNumber:
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return nil, false
		}
		number, ok := v.(float64)
		return number, ok
	case nodeInt:
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return nil, false
		}
		switch value := v.(type) {
		case string:
			return value, integerForm.MatchString(value)
		case float64:
			if value == float64(int64(value)) {
				return strconv.FormatInt(int64(value), 10), true
			}
		}
		return nil, false
	case nodeObject:
		if depth >= maxDepth {
			return nil, false
		}
		var members map[string]json.RawMessage
		if json.Unmarshal(raw, &members) != nil {
			return nil, false
		}
		out := map[string]any{}
		for _, fl := range n.fields {
			if member, found := members[fl.name]; found {
				if value, ok := fl.node.clean(member, depth+1); ok {
					out[fl.name] = value
				}
			}
		}
		return out, len(out) > 0
	default:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return nil, false
		}
		var out []any
		for _, item := range items {
			if len(out) == maxListItems {
				break
			}
			if value, ok := n.item.clean(item, depth); ok {
				out = append(out, value)
			}
		}
		return out, len(out) > 0
	}
}

// cleanObject reads one answer object; a missing or malformed one is an empty object.
func cleanObject(n *node, raw json.RawMessage) map[string]any {
	if value, ok := n.clean(raw, 0); ok {
		return value.(map[string]any)
	}
	return map[string]any{}
}

// cleanObjects reads a list of answer objects and drops those without any documented member.
func cleanObjects(n *node, raws []json.RawMessage) []map[string]any {
	out := []map[string]any{}
	for _, raw := range raws {
		if value, ok := n.clean(raw, 0); ok {
			out = append(out, value.(map[string]any))
		}
	}
	return out
}

// schema writes the JSON schema of the node. A node met again inside itself ends as a plain object.
func (n *node) schema(open map[*node]bool) string {
	switch n.kind {
	case nodeText, nodeInt:
		return stringSchema
	case nodeBool:
		return boolSchema
	case nodeNumber:
		return `{"type":"number"}`
	case nodeList:
		return `{"type":"array","items":` + n.item.schema(open) + `}`
	}
	if open[n] {
		return `{"type":"object"}`
	}
	open[n] = true
	defer delete(open, n)
	members := make([]string, 0, len(n.fields))
	for _, fl := range n.fields {
		members = append(members, member(fl.name, fl.node.schema(open)))
	}
	return `{"type":"object","properties":{` + strings.Join(members, ",") + `}}`
}

func (n *node) outputSchema() string { return n.schema(map[*node]bool{}) }

// names lists the members of an object node.
func (n *node) names() []string {
	out := make([]string, len(n.fields))
	for i, fl := range n.fields {
		out[i] = fl.name
	}
	return out
}
