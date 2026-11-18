package googlebusinessprofile

import (
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The Lodging message is described once, as the tables of lodging_fields.go, and everything else is derived
// from them: the input and output schemas, the check of what a caller sends, the cut of what Google answers,
// and the field paths a mask may name. Values travel as decoded JSON (objects, lists, booleans, strings, and
// float64 numbers); a value of the wrong type or outside its range never reaches Google or the caller.

type kind uint8

const (
	kindBool kind = iota + 1
	kindChoice
	kindCount
	kindMeasure
	kindText
	kindTime
	kindObject
	kindList
)

// Bounds of the values of a lodging. The texts of a lodging are few: a label and unit codes, and the BCP-47
// tag of a spoken language.
const (
	maxLabelRunes    = 200
	maxCodeRunes     = 100
	maxNameRunes     = 100
	maxLanguageRunes = 35
	maxTimeRunes     = 40
	maxInt32         = 1<<31 - 1
	maxFloat32       = math.MaxFloat32
)

// languageTag is the shape of a BCP-47 tag; Google decides which tags it accepts.
var languageTag = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8}){0,3}$`)

// field is one member of a message: its kind, the values or members of its kind, and whether Google only
// answers it.
type field struct {
	name     string
	kind     kind
	values   []string
	members  []field
	min, max int64
	runes    int
	tag      bool
	readOnly bool
	about    string
}

func boolean(name string) field { return field{name: name, kind: kindBool} }

func choice(name string, values []string) field {
	return field{name: name, kind: kindChoice, values: values}
}

func count(name string) field { return field{name: name, kind: kindCount, max: maxInt32} }

func bounded(name string, low, high int64) field {
	return field{name: name, kind: kindCount, min: low, max: high}
}

func measure(name string) field { return field{name: name, kind: kindMeasure} }

func text(name string, runes int) field { return field{name: name, kind: kindText, runes: runes} }

func languageCode(name string) field {
	return field{name: name, kind: kindText, runes: maxLanguageRunes, tag: true}
}

func timestamp(name string) field { return field{name: name, kind: kindTime, runes: maxTimeRunes} }

func group(name string, members []field) field {
	return field{name: name, kind: kindObject, members: members}
}

func listOf(name string, members []field) field {
	return field{name: name, kind: kindList, members: members}
}

// texts is a list of texts.
func texts(name string, runes int) field { return field{name: name, kind: kindList, runes: runes} }

func readOnly(f field) field {
	f.readOnly = true
	return f
}

func about(f field, description string) field {
	f.about = description
	return f
}

// schema is the JSON schema of one field. input gives the schema of what a caller sends: the read-only
// members are left out and the bounds are the ones the tool checks.
func (f field) schema(input bool) string {
	var s string
	switch f.kind {
	case kindBool:
		s = booleanSchema
	case kindChoice:
		s = `{"type":"string","enum":` + mustJSON(f.values) + `}`
	case kindCount:
		s = `{"type":"integer","minimum":` + strconv.FormatInt(f.min, 10) + `,"maximum":` +
			strconv.FormatInt(f.max, 10) + `}`
	case kindMeasure:
		s = `{"type":"number","minimum":0,"maximum":` + strconv.FormatFloat(maxFloat32, 'g', -1, 64) + `}`
	case kindText:
		s = f.textSchema(input)
	case kindTime:
		s = `{"type":"string","format":"date-time","maxLength":` + strconv.Itoa(f.runes) + `}`
	case kindObject:
		s = membersSchema(f.members, input)
	case kindList:
		item := stringSchema
		if f.members != nil {
			item = membersSchema(f.members, input)
		} else if input {
			item = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(f.runes) + `}`
		}
		s = `{"type":"array","items":` + item
		if input {
			s += `,"maxItems":` + strconv.Itoa(maxItems)
		}
		s += `}`
	}
	return s
}

func (f field) textSchema(input bool) string {
	if !input {
		return stringSchema
	}
	s := `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(f.runes)
	if f.tag {
		s += `,"pattern":` + mustJSON(languageTag.String())
	}
	return s + `}`
}

// membersSchema is the closed object schema of the members; on input the read-only ones are left out.
func membersSchema(members []field, input bool) string {
	pairs := make([]string, 0, 2*len(members))
	for _, m := range members {
		if input && m.readOnly {
			continue
		}
		pairs = append(pairs, m.name, m.schema(input))
	}
	return objectSchema(pairs...)
}

// memberOf returns the member of a message by name.
func memberOf(members []field, name string) (field, bool) {
	for _, m := range members {
		if m.name == name {
			return m, true
		}
	}
	return field{}, false
}

// clean cuts what Google answered to the table: an unknown member, a value of the wrong type, a value outside
// the enumeration, and an empty object or list are dropped, and a text is cut. It returns false for a value
// that is nothing.
func clean(f field, raw any) (any, bool) {
	switch f.kind {
	case kindBool:
		v, ok := raw.(bool)
		return v, ok
	case kindChoice:
		v, ok := raw.(string)
		return v, ok && slices.Contains(f.values, v)
	case kindCount:
		v, ok := raw.(float64)
		return v, ok && v == math.Trunc(v) && v >= float64(f.min) && v <= float64(f.max)
	case kindMeasure:
		v, ok := raw.(float64)
		return v, ok && !math.IsInf(v, 0) && !math.IsNaN(v) && v >= 0
	case kindText, kindTime:
		v, ok := raw.(string)
		if !ok {
			return nil, false
		}
		v = strings.ToValidUTF8(v, "")
		if utf8.RuneCountInString(v) > f.runes {
			v = string([]rune(v)[:f.runes]) + "…"
		}
		return v, true
	case kindObject:
		out := cleanObject(f.members, raw)
		return out, len(out) > 0
	case kindList:
		all, ok := raw.([]any)
		if !ok {
			return nil, false
		}
		out := []any{}
		for _, entry := range all {
			if len(out) == maxItems {
				break
			}
			var v any
			if f.members != nil {
				obj := cleanObject(f.members, entry)
				if len(obj) == 0 {
					continue
				}
				v = obj
			} else {
				s, ok := clean(field{kind: kindText, runes: f.runes}, entry)
				if !ok {
					continue
				}
				v = s
			}
			out = append(out, v)
		}
		return out, len(out) > 0
	}
	return nil, false
}

// cleanObject cuts an object to the table; a value that is no object is an empty one.
func cleanObject(members []field, raw any) map[string]any {
	given, _ := raw.(map[string]any)
	out := map[string]any{}
	for name, value := range given {
		m, ok := memberOf(members, name)
		if !ok {
			continue
		}
		if v, ok := clean(m, value); ok {
			out[name] = v
		}
	}
	return out
}

// check refuses a value a caller sends that does not fit its field, naming the path and never the value.
func check(f field, raw any, path string) error {
	bad := func(what string) error { return invalidArgument(path + " must be " + what) }
	switch f.kind {
	case kindBool:
		if _, ok := raw.(bool); !ok {
			return bad("true or false")
		}
	case kindChoice:
		if v, ok := raw.(string); !ok || !slices.Contains(f.values, v) {
			return bad("one of " + strings.Join(f.values, ", "))
		}
	case kindCount:
		if v, ok := raw.(float64); !ok || v != math.Trunc(v) || v < float64(f.min) || v > float64(f.max) {
			return bad("a whole number from " + strconv.FormatInt(f.min, 10) + " to " + strconv.FormatInt(f.max, 10))
		}
	case kindMeasure:
		if v, ok := raw.(float64); !ok || math.IsNaN(v) || v < 0 || v > maxFloat32 {
			return bad("a number from 0 to " + strconv.FormatFloat(maxFloat32, 'g', -1, 64))
		}
	case kindText:
		return checkText(f, raw, path)
	case kindTime:
		v, ok := raw.(string)
		if _, err := time.Parse(time.RFC3339Nano, v); !ok || err != nil || utf8.RuneCountInString(v) > f.runes {
			return bad("a time in RFC 3339 form")
		}
	case kindObject:
		return checkObject(f.members, raw, path)
	case kindList:
		all, ok := raw.([]any)
		if !ok || len(all) == 0 || len(all) > maxItems {
			return bad("a list of 1 to " + strconv.Itoa(maxItems) + " entries")
		}
		for i, entry := range all {
			entryPath := path + "[" + strconv.Itoa(i) + "]"
			if f.members != nil {
				if err := checkObject(f.members, entry, entryPath); err != nil {
					return err
				}
			} else if err := checkText(field{kind: kindText, runes: f.runes}, entry, entryPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkText(f field, raw any, path string) error {
	v, ok := raw.(string)
	switch {
	case !ok || strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > f.runes ||
		strings.IndexFunc(v, unicode.IsControl) >= 0 || !utf8.ValidString(v):
		return invalidArgument(path + " must be 1 to " + strconv.Itoa(f.runes) + " characters without control characters")
	case f.tag && !languageTag.MatchString(v):
		return invalidArgument(path + " must be a BCP-47 language tag")
	}
	return nil
}

// checkObject refuses an object with a member the table does not hold, or one Google only answers.
func checkObject(members []field, raw any, path string) error {
	given, ok := raw.(map[string]any)
	if !ok {
		return invalidArgument(path + " must be an object")
	}
	for name, value := range given {
		m, ok := memberOf(members, name)
		if !ok || m.readOnly {
			return invalidArgument(path + "." + name + " is not a field of the lodging that can be changed")
		}
		if err := check(m, value, path+"."+name); err != nil {
			return err
		}
	}
	return nil
}

// paths lists the field paths a mask may name, sorted: every field at every depth, a list as a whole, and
// without the read-only fields when the paths are for a change.
func paths(members []field, prefix string, forChange bool) []string {
	var out []string
	for _, m := range members {
		if forChange && m.readOnly {
			continue
		}
		path := prefix + m.name
		out = append(out, path)
		if m.kind == kindObject {
			out = append(out, paths(m.members, path+".", forChange)...)
		}
	}
	slices.Sort(out)
	return out
}

var (
	readPaths = paths(lodgingFields, "", false)
	// writePaths leaves out the read-only fields and the metadata, which the tool sends on its own.
	writePaths = slices.DeleteFunc(paths(lodgingFields, "", true), func(p string) bool {
		return p == "metadata" || strings.HasPrefix(p, "metadata.")
	})
)

// maskOf checks a mask against the known paths and returns it sorted: no empty mask, no unknown path, no path
// twice, and no path inside another, so that one set of fields has one spelling.
func maskOf(name string, given, known []string) ([]string, error) {
	if len(given) == 0 {
		return nil, invalidArgument(name + " is required: name at least one field path of the lodging")
	}
	mask := make([]string, 0, len(given))
	for _, path := range given {
		if !slices.Contains(known, path) {
			return nil, invalidArgument(name + " names a field path the lodging does not have or that cannot be used here")
		}
		if slices.Contains(mask, path) {
			return nil, invalidArgument(name + " names a field path twice")
		}
		mask = append(mask, path)
	}
	slices.Sort(mask)
	for i := 1; i < len(mask); i++ {
		if strings.HasPrefix(mask[i], mask[i-1]+".") {
			return nil, invalidArgument(name + " names a field and a field inside it; name only the outer one")
		}
	}
	return mask, nil
}

// valueAt returns the value at a dotted path of cleaned values.
func valueAt(object map[string]any, path string) (any, bool) {
	var current any = object
	for _, segment := range strings.Split(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		if current, ok = m[segment]; !ok {
			return nil, false
		}
	}
	return current, true
}

// leaves lists the paths of the values an object holds, a list as a whole.
func leaves(members []field, object map[string]any, prefix string) []string {
	var out []string
	for name, value := range object {
		m, ok := memberOf(members, name)
		if !ok {
			continue
		}
		if inner, isObject := value.(map[string]any); isObject && m.kind == kindObject {
			out = append(out, leaves(m.members, inner, prefix+name+".")...)
		} else {
			out = append(out, prefix+name)
		}
	}
	return out
}

// covered reports whether a mask names the path or a field around it.
func covered(mask []string, path string) bool {
	for _, m := range mask {
		if path == m || strings.HasPrefix(path, m+".") {
			return true
		}
	}
	return false
}

func sameValue(a any, aSet bool, b any, bSet bool) bool {
	return aSet == bSet && reflect.DeepEqual(a, b)
}
