package googleads

import (
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// A node describes one member of a resource that a changing tool offers. One tree drives the input schema,
// the check of the arguments, the body and the field mask of the request, and the mapping of the object that
// Google answers with, so the three never disagree about which members exist.

type nodeKind int

const (
	kindText nodeKind = iota
	kindFlag
	kindWhole
	kindNumber
	kindEnum
	kindResourceName
	kindObject
)

type node struct {
	name, desc string
	kind       nodeKind

	// values are the allowed values of an enum.
	values []string
	// lo and hi bound a whole number; low and high bound a floating point number when bounded is set.
	lo, hi    int64
	low, high float64
	bounded   bool
	// maxLen bounds a text in bytes, pattern is the form it must have, check is a further rule.
	maxLen  int
	pattern string
	check   func(argument, value string) error
	// clears marks a text that an empty value clears on an update.
	clears bool
	// collection is the resource collection of a resource name, which must belong to the account of the call.
	collection string

	list     bool
	maxItems int
	fields   []node
	// exclusive names members of which an object holds at most one.
	exclusive []string
	// createOnly marks a member Google accepts only when the object is created.
	createOnly bool
	// allowEmpty lets a list of objects be empty, where an empty list has a meaning of its own.
	allowEmpty bool
	// required names members an object must hold when it is created. requiredAlways extends this to every
	// change, for an entry of a list that an update replaces as a whole.
	required       []string
	requiredAlways bool
	// emptyOK marks an object whose members are all optional, so that giving it without members is meaningful.
	emptyOK bool
}

func (n node) with(f func(*node)) node { f(&n); return n }

func textNode(name, desc string, maxLen int) node {
	return node{name: name, desc: desc, kind: kindText, maxLen: maxLen}
}
func flagNode(name, desc string) node { return node{name: name, desc: desc, kind: kindFlag} }
func wholeNode(name, desc string, lo, hi int64) node {
	return node{name: name, desc: desc, kind: kindWhole, lo: lo, hi: hi}
}

// microsNode is an amount in micros of the currency of the account: an int64 of at least zero.
func microsNode(name, desc string) node { return wholeNode(name, desc, 0, math.MaxInt64) }

func enumNode(name, desc string, values []string) node {
	return node{name: name, desc: desc, kind: kindEnum, values: values}
}
func resourceNode(name, desc, collection string) node {
	return node{name: name, desc: desc, kind: kindResourceName, collection: collection}
}
func objectNode(name, desc string, fields ...node) node {
	return node{name: name, desc: desc, kind: kindObject, fields: fields}
}
func listOf(n node, max int) node { n.list, n.maxItems = true, max; return n }
func createOnly(n node) node      { n.createOnly = true; return n }
func allowEmpty(n node) node      { n.allowEmpty = true; return n }

// enumOf is the list of values of the enum of a field of the catalog, without the two values that are never
// sent.
func enumOf(field string) []string {
	f, ok := LookupField(field)
	if !ok || f.DataType != "ENUM" {
		panic("googleads: " + field + " is no enum field of the catalog")
	}
	return sendable(f.EnumValues)
}

// enumNamed is the same for an enum type that no selectable field of the catalog carries.
func enumNamed(name string) []string {
	for _, e := range catalogEnums {
		if e.name == name {
			return sendable(e.values)
		}
	}
	panic("googleads: " + name + " is no enum of the catalog")
}

func sendable(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "UNSPECIFIED" && v != "UNKNOWN" {
			out = append(out, v)
		}
	}
	return out
}

// mutable is the node without the members Google accepts only at creation.
func (n node) mutable() node {
	if !n.requiredAlways {
		n.required = nil
	}
	fields := make([]node, 0, len(n.fields))
	for _, f := range n.fields {
		if !f.createOnly {
			fields = append(fields, f.mutable())
		}
	}
	n.fields = fields
	return n
}

func (n node) member(name string) (node, bool) {
	for _, f := range n.fields {
		if f.name == name {
			return f, true
		}
	}
	return node{}, false
}

// --- input schema ---

func (n node) resourcePattern() string {
	return `^customers/[0-9]{1,10}/` + n.collection + `/[0-9]{1,20}$`
}

func (n node) scalarSchema() schema {
	var s schema
	switch n.kind {
	case kindText:
		s = schema{"type": "string", "maxLength": n.maxLen}
		if n.pattern != "" {
			s["pattern"] = n.pattern
			if n.clears {
				s["pattern"] = "^$|" + n.pattern
			}
		}
	case kindFlag:
		s = schema{"type": "boolean"}
	case kindWhole:
		s = schema{"type": "integer", "minimum": n.lo}
		if n.hi != math.MaxInt64 {
			s["maximum"] = n.hi
		}
	case kindNumber:
		s = schema{"type": "number"}
		if n.bounded {
			s["minimum"], s["maximum"] = n.low, n.high
		}
	case kindEnum:
		s = schema{"type": "string", "enum": n.values}
	case kindResourceName:
		s = schema{"type": "string", "maxLength": 100, "pattern": n.resourcePattern()}
	}
	return s
}

// inputSchema is the schema of the argument for this member.
func (n node) inputSchema() schema {
	var s schema
	if n.kind == kindObject {
		props := schema{}
		for _, f := range n.fields {
			props[f.name] = f.inputSchema()
		}
		s = object(props, n.required...)
		if len(n.fields) > 0 && !n.emptyOK {
			s["minProperties"] = 1
		}
	} else {
		s = n.scalarSchema()
	}
	if n.list {
		s = schema{"type": "array", "maxItems": n.maxItems, "items": s}
		if n.kind == kindObject && !n.allowEmpty {
			s["minItems"] = 1
		}
	}
	if n.desc != "" {
		s["description"] = n.desc
	}
	return s
}

// outputSchema is the schema of this member in an object Google answered with. It names no bound: what Google
// returns is cut, not refused.
func (n node) outputSchema() schema {
	var s schema
	switch n.kind {
	case kindFlag:
		s = booleanSchema
	case kindWhole:
		s = integerSchema
	case kindNumber:
		s = numberSchema
	case kindObject:
		props := schema{}
		for _, f := range n.fields {
			props[f.name] = f.outputSchema()
		}
		s = object(props)
	default:
		s = stringSchema
	}
	if n.list {
		return arrayOf(s)
	}
	return s
}

// --- arguments to request ---

// builder turns the decoded arguments of one object into the members of the request body and the field mask.
type builder struct {
	customerID string
	create     bool
	mask       []string
}

type pathOfMember struct{ snake, camel string }

func (p pathOfMember) child(n node) pathOfMember {
	snake, camelName := n.name, camel(n.name)
	if p.snake != "" {
		snake = p.snake + "." + n.name
	}
	if p.camel != "" {
		camelName = p.camel + "." + camelName
	}
	return pathOfMember{snake, camelName}
}

var timePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$`)

func hasControl(value string) bool { return strings.ContainsFunc(value, unicode.IsControl) }

// object checks the members of one object and builds it. Each member given must be one the fields name.
func (b *builder) object(n node, in map[string]any, at pathOfMember) (map[string]any, error) {
	out := map[string]any{}
	var given []string
	for key := range in {
		member, ok := n.member(key)
		if !ok {
			return nil, badRequest(joinPath(at.snake, key) + " is not an argument of this tool")
		}
		given = append(given, key)
		if member.createOnly && !b.create {
			return nil, badRequest(joinPath(at.snake, key) + " can be set only when the object is created")
		}
	}
	slices.Sort(given)
	if b.create || n.requiredAlways {
		for _, key := range n.required {
			if _, found := in[key]; !found {
				return nil, badRequest(joinPath(at.snake, key) + " is required")
			}
		}
	}
	if len(n.exclusive) > 0 {
		count := 0
		for _, key := range given {
			if slices.Contains(n.exclusive, key) {
				count++
			}
		}
		if count > 1 {
			return nil, badRequest("at most one of " + strings.Join(n.exclusive, ", ") + " may be given" +
				onPath(at.snake))
		}
	}
	for _, key := range given {
		member, _ := n.member(key)
		value, keep, err := b.member(member, in[key], at.child(member))
		if err != nil {
			return nil, err
		}
		if keep {
			out[camel(key)] = value
		}
	}
	return out, nil
}

func onPath(path string) string {
	if path == "" {
		return ""
	}
	return " in " + path
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// member checks and builds one member. keep is false for a text that an update clears: its path is in the
// mask and it has no value in the body.
func (b *builder) member(n node, value any, at pathOfMember) (any, bool, error) {
	if n.list {
		items, ok := value.([]any)
		if !ok || len(items) > n.maxItems || (n.kind == kindObject && len(items) == 0 && !n.allowEmpty) {
			return nil, false, badRequest(at.snake + " must be a list of at most " + strconv.Itoa(n.maxItems) + " entries")
		}
		out := make([]any, 0, len(items))
		for i, item := range items {
			built, _, err := b.single(n, item, pathOfMember{at.snake + "[" + strconv.Itoa(i) + "]", at.camel}, false)
			if err != nil {
				return nil, false, err
			}
			out = append(out, built)
		}
		b.mask = append(b.mask, at.camel)
		return out, true, nil
	}
	built, keep, err := b.single(n, value, at, true)
	return built, keep, err
}

// single checks one value; maskIt says whether this call adds the path of a scalar or an empty object.
func (b *builder) single(n node, value any, at pathOfMember, maskIt bool) (any, bool, error) {
	switch n.kind {
	case kindObject:
		in, ok := value.(map[string]any)
		if !ok {
			return nil, false, badRequest(at.snake + " must be an object")
		}
		if len(n.fields) == 0 || (n.emptyOK && len(in) == 0) {
			if len(in) != 0 {
				return nil, false, badRequest(at.snake + " takes no members")
			}
			if maskIt {
				b.mask = append(b.mask, at.camel)
			}
			return map[string]any{}, true, nil
		}
		if len(in) == 0 {
			return nil, false, badRequest(at.snake + " must name at least one member")
		}
		before := len(b.mask)
		out, err := b.object(n, in, at)
		if err != nil {
			return nil, false, err
		}
		if !maskIt {
			b.mask = b.mask[:before]
		}
		return out, true, nil
	case kindText:
		text, ok := value.(string)
		if !ok {
			return nil, false, badRequest(at.snake + " must be text")
		}
		if text == "" && n.clears {
			if !b.create {
				b.mask = append(b.mask, at.camel)
			}
			return nil, false, nil
		}
		if n.check != nil {
			if err := n.check(at.snake, text); err != nil {
				return nil, false, err
			}
		} else if text == "" || len(text) > n.maxLen || hasControl(text) {
			return nil, false, badRequest(at.snake + " must be text of 1 to " + strconv.Itoa(n.maxLen) +
				" bytes without control characters")
		}
		if n.pattern != "" && !regexp.MustCompile(n.pattern).MatchString(text) {
			return nil, false, badRequest(at.snake + " is not in the form " + n.patternHint())
		}
		if maskIt {
			b.mask = append(b.mask, at.camel)
		}
		return text, true, nil
	case kindFlag:
		flag, ok := value.(bool)
		if !ok {
			return nil, false, badRequest(at.snake + " must be true or false")
		}
		if maskIt {
			b.mask = append(b.mask, at.camel)
		}
		return flag, true, nil
	case kindWhole:
		number, ok := value.(json.Number)
		whole, err := strconv.ParseInt(string(number), 10, 64)
		if !ok || err != nil || whole < n.lo || whole > n.hi {
			return nil, false, badRequest(at.snake + " must be a whole number" + n.rangeHint())
		}
		if maskIt {
			b.mask = append(b.mask, at.camel)
		}
		return number, true, nil
	case kindNumber:
		number, ok := value.(json.Number)
		parsed, err := strconv.ParseFloat(string(number), 64)
		if !ok || err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) || (n.bounded && (parsed < n.low || parsed > n.high)) {
			return nil, false, badRequest(at.snake + " must be a number" + n.rangeHint())
		}
		if maskIt {
			b.mask = append(b.mask, at.camel)
		}
		return parsed, true, nil
	case kindEnum:
		text, ok := value.(string)
		if !ok || !slices.Contains(n.values, text) {
			return nil, false, badRequest(at.snake + " must be one of " + strings.Join(n.values, ", "))
		}
		if maskIt {
			b.mask = append(b.mask, at.camel)
		}
		return text, true, nil
	case kindResourceName:
		text, ok := value.(string)
		if !ok || !ownedResource(text, b.customerID, n.collection) {
			return nil, false, badRequest(at.snake + " must be customers/" + queryNumber(b.customerID) + "/" + n.collection +
				"/N of the account of customer_id; a temporary negative ID is not accepted")
		}
		if maskIt {
			b.mask = append(b.mask, at.camel)
		}
		return text, true, nil
	}
	return nil, false, badRequest(at.snake + " is not usable")
}

func (n node) patternHint() string {
	if n.desc != "" {
		return "its description"
	}
	return "the documented form"
}

func (n node) rangeHint() string {
	switch {
	case n.kind == kindWhole && n.hi != math.MaxInt64:
		return " from " + strconv.FormatInt(n.lo, 10) + " to " + strconv.FormatInt(n.hi, 10)
	case n.kind == kindWhole:
		return " of at least " + strconv.FormatInt(n.lo, 10)
	case n.bounded:
		return " from " + strconv.FormatFloat(n.low, 'g', -1, 64) + " to " + strconv.FormatFloat(n.high, 'g', -1, 64)
	}
	return ""
}

// ownedResource reports whether a resource name is customers/ID/<collection>/N of exactly the account, with a
// positive ID. A temporary ID, which is negative, never matches.
func ownedResource(name, customerID, collection string) bool {
	rest, ok := strings.CutPrefix(name, customerName(customerID)+"/"+collection+"/")
	return ok && numberPattern.MatchString(rest) && strings.Trim(rest, "0") != ""
}

// --- answer to output ---

// output maps a member of an object Google answered with. Members the tree does not name are dropped. ok is
// false for a value of the wrong kind or a resource name outside the account.
func (n node) output(raw any, customerID string) (any, bool) {
	if n.list {
		items, isList := raw.([]any)
		if !isList {
			return nil, false
		}
		out := make([]any, 0, min(len(items), maxListItems))
		for _, item := range items[:min(len(items), maxListItems)] {
			value, ok := n.outputOne(item, customerID)
			if !ok {
				return nil, false
			}
			out = append(out, value)
		}
		return out, true
	}
	return n.outputOne(raw, customerID)
}

func (n node) outputOne(raw any, customerID string) (any, bool) {
	switch n.kind {
	case kindObject:
		in, ok := raw.(map[string]any)
		if !ok {
			return nil, false
		}
		return outputMembers(n.fields, in, customerID)
	case kindFlag:
		flag, ok := raw.(bool)
		return flag, ok
	case kindWhole:
		switch v := raw.(type) {
		case json.Number:
			whole, err := strconv.ParseInt(string(v), 10, 64)
			return whole, err == nil
		case string:
			whole, err := strconv.ParseInt(v, 10, 64)
			return whole, err == nil && wireIntPattern.MatchString(v)
		}
		return nil, false
	case kindNumber:
		switch v := raw.(type) {
		case json.Number:
			number, err := strconv.ParseFloat(string(v), 64)
			return number, err == nil && !math.IsInf(number, 0)
		case string:
			number, err := strconv.ParseFloat(v, 64)
			return number, err == nil && !math.IsInf(number, 0) && !math.IsNaN(number)
		}
		return nil, false
	case kindEnum:
		text, ok := raw.(string)
		return code(text), ok
	case kindResourceName:
		text, ok := raw.(string)
		if !ok || !ownedResource(text, customerID, n.collection) {
			return nil, false
		}
		return text, true
	}
	text, ok := raw.(string)
	return boundedText(text, maxLongText), ok
}

func outputMembers(fields []node, in map[string]any, customerID string) (map[string]any, bool) {
	out := map[string]any{}
	for _, f := range fields {
		raw, present := in[camel(f.name)]
		if !present || raw == nil {
			continue
		}
		value, ok := f.output(raw, customerID)
		if !ok {
			return nil, false
		}
		out[f.name] = value
	}
	return out, true
}
