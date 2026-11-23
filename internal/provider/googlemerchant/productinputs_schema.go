package googlemerchant

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The attributes of a product input are the members of nProductAttributes, the one tree of the 145 documented
// attributes: the product tools read an answer against it, and the argument of a write tool is checked against
// it and its input schema is written from it. The tree says which member is an object, a list, a number, an
// int64, a flag, an address, or text. What it does not say, the values of an enum, which text is a date and
// time, and the documented bounds of single members, is the data of productinputs_constraints.json, keyed by
// the path of the member (price.currencyCode, certifications.certificationName). The file names only the
// members that have such a constraint, never the attributes themselves.

//go:embed productinputs_constraints.json
var constraintsJSON []byte

type inputConstraints struct {
	Enums     map[string][]string   `json:"enums"`
	DateTimes []string              `json:"dateTimes"`
	Ranges    map[string][2]float64 `json:"ranges"`
	MaxItems  map[string]int        `json:"maxItems"`
	MaxRunes  map[string]int        `json:"maxRunes"`
}

var constraints = loadConstraints()

func loadConstraints() inputConstraints {
	var c inputConstraints
	if err := json.Unmarshal(constraintsJSON, &c); err != nil {
		panic("googlemerchant: the embedded product attribute constraints are unreadable")
	}
	return c
}

// Bounds of the arguments. Google documents the custom attribute limits; the other caps are Qatlas' own and
// differ from the cuts of a result, which only keep an answer small.
const (
	maxAttributeText     = 5000
	maxAttributeURLRunes = 2000
	maxAttributeList     = 250
	maxCustomAttributes  = 2500   // documented
	maxCustomRunes       = 10240  // documented: name and value of one custom attribute together
	maxCustomBytes       = 102400 // documented: total size of the custom attributes
	maxGroupValues       = 100
	maxCustomNameRunes   = 100
	maxOfferIDRunes      = 100
	maxProductBody       = 1 << 20
)

// attributeNames are the names of the attributes in order.
func attributeNames() []string {
	names := nProductAttributes.names()
	slices.Sort(names)
	return names
}

func isAttributeName(name string) bool { return slices.Contains(nProductAttributes.names(), name) }

func (n *node) field(name string) *node {
	for _, fl := range n.fields {
		if fl.name == name {
			return fl.node
		}
	}
	return nil
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func textLimit(n *node, path string) int {
	switch {
	case constraints.MaxRunes[path] > 0:
		return constraints.MaxRunes[path]
	case n == tURL:
		return maxAttributeURLRunes
	}
	return maxAttributeText
}

func listLimit(path string) int {
	if constraints.MaxItems[path] > 0 {
		return constraints.MaxItems[path]
	}
	return maxAttributeList
}

const int64Pattern = `^-?[0-9]{1,19}$`

var int64Form = regexp.MustCompile(int64Pattern)

func formatBound(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func sortedNames(fields []field) []string {
	names := make([]string, len(fields))
	for i, fl := range fields {
		names[i] = fl.name
	}
	slices.Sort(names)
	return names
}

// inputSchema writes the closed, bounded JSON Schema of an argument value for the node at path.
func (n *node) inputSchema(path string) string {
	switch n.kind {
	case nodeObject:
		members := make([]string, 0, len(n.fields))
		for _, name := range sortedNames(n.fields) {
			members = append(members, member(name, n.field(name).inputSchema(joinPath(path, name))))
		}
		return `{"type":"object","properties":{` + strings.Join(members, ",") + `},"additionalProperties":false}`
	case nodeList:
		return `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(listLimit(path)) + `,"items":` + n.item.inputSchema(path) + `}`
	case nodeBool:
		return boolSchema
	case nodeNumber:
		out := `{"type":"number"`
		if r, ok := constraints.Ranges[path]; ok {
			out += `,"minimum":` + formatBound(r[0]) + `,"maximum":` + formatBound(r[1])
		}
		return out + `}`
	case nodeInt:
		return `{"type":"string","pattern":"` + int64Pattern + `"}`
	}
	switch {
	case n == tURL:
		return `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(textLimit(n, path)) + `,"pattern":"^https://"}`
	case constraints.Enums[path] != nil:
		return `{"type":"string","enum":` + quoteList(constraints.Enums[path]) + `}`
	case slices.Contains(constraints.DateTimes, path):
		return `{"type":"string","minLength":20,"maxLength":40,"x-form":"RFC 3339 date and time such as 2030-01-31T09:00:00Z"}`
	}
	return `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(textLimit(n, path)) + `}`
}

// productText reports whether text can be handed to Google: valid UTF-8 without control characters other than
// the line breaks and tabs a description may hold.
func productText(value string, limit int) bool {
	if value == "" || utf8.RuneCountInString(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

// checkInput validates an argument value against the node at path. The messages name only members of the
// tree, never a value or a member the caller made up.
func (n *node) checkInput(value any, path string) error {
	shown := "attributes"
	if path != "" {
		shown += "." + path
	}
	bad := func(what string) error { return invalidArgument(shown + " " + what) }
	switch n.kind {
	case nodeObject:
		object, ok := value.(map[string]any)
		if !ok {
			return bad("must be an object")
		}
		for key, child := range object {
			sub := n.field(key)
			if sub == nil {
				return bad("holds a member that is not a documented product attribute")
			}
			if err := sub.checkInput(child, joinPath(path, key)); err != nil {
				return err
			}
		}
	case nodeList:
		list, ok := value.([]any)
		if !ok || len(list) == 0 || len(list) > listLimit(path) {
			return bad("must be a list of 1 to " + strconv.Itoa(listLimit(path)) + " entries")
		}
		for _, item := range list {
			if err := n.item.checkInput(item, path); err != nil {
				return err
			}
		}
	case nodeBool:
		if _, ok := value.(bool); !ok {
			return bad("must be true or false")
		}
	case nodeNumber:
		num, ok := value.(json.Number)
		if !ok {
			return bad("must be a number")
		}
		f, err := num.Float64()
		r, ranged := constraints.Ranges[path]
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || (ranged && (f < r[0] || f > r[1])) {
			return bad("must be a number within its documented range")
		}
	case nodeInt:
		text, ok := value.(string)
		if _, err := strconv.ParseInt(text, 10, 64); !ok || !int64Form.MatchString(text) || err != nil {
			return bad("must be a decimal integer as a string")
		}
	default:
		text, ok := value.(string)
		switch {
		case n == tURL:
			if !ok || !productText(text, textLimit(n, path)) {
				return bad("must be an address of 1 to " + strconv.Itoa(textLimit(n, path)) + " characters")
			}
			return checkHTTPSAddress(shown, text)
		case constraints.Enums[path] != nil:
			if !ok || !slices.Contains(constraints.Enums[path], text) {
				return bad("must be one of the documented values")
			}
		case slices.Contains(constraints.DateTimes, path):
			if _, err := time.Parse(time.RFC3339, text); !ok || err != nil || len(text) > 40 {
				return bad("must be an RFC 3339 date and time")
			}
		case !ok || !productText(text, textLimit(n, path)):
			return bad("must be text of 1 to " + strconv.Itoa(textLimit(n, path)) + " characters without control characters")
		}
	}
	return nil
}

// decodeTree reads a JSON value keeping numbers exact.
func decodeTree(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// objectAnswer is the JSON object Google answers with; any other kind of value is an invalid answer.
type objectAnswer json.RawMessage

func (o *objectAnswer) UnmarshalJSON(b []byte) error {
	if trimmed := bytes.TrimSpace(b); len(trimmed) == 0 || trimmed[0] != '{' {
		return errNotObject
	}
	*o = append((*o)[:0], b...)
	return nil
}

type notObject struct{}

func (notObject) Error() string { return "the answer is not a JSON object" }

var errNotObject error = notObject{}

// productInputNode is the ProductInput message as a result shows it. base64EncodedProduct is not a member
// of it here.
var productInputNode = obj(
	f("base64EncodedName", tName), f("contentLanguage", tID), f("customAttributes", list(nCustomAttribute)),
	f("feedLabel", tName), f("legacyLocal", tBool), f("name", tName), f("offerId", tName), f("product", tName),
	f("productAttributes", nProductAttributes), f("versionNumber", tInt))
