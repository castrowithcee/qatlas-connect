package googlebusinessprofile

import (
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"
)

// What Google returns is third-party content. The answer types carry their bounds in the types of their
// members: a text type cuts itself while it is decoded, a list type cuts itself after decoding, and a member
// the type does not list is skipped by the decoder and never held.

// Bounds of what a result shows.
const (
	maxShortRunes = 100  // identifiers, enumerations, codes
	maxLineRunes  = 500  // names, address lines, phone numbers, labels
	maxURLRunes   = 2048 // websites and Google links, shown and never fetched
	maxLongRunes  = 4000 // descriptions
	maxItems      = 250  // every list inside an account or a location
)

// Text types. Each cuts to its limit and marks the cut with …; a value that is not a string is dropped.
type (
	short string
	line  string
	link  string
	long  string
)

func (s *short) UnmarshalJSON(b []byte) error { *s = short(clipJSON(b, maxShortRunes)); return nil }
func (s *line) UnmarshalJSON(b []byte) error  { *s = line(clipJSON(b, maxLineRunes)); return nil }
func (s *link) UnmarshalJSON(b []byte) error  { *s = link(clipJSON(b, maxURLRunes)); return nil }
func (s *long) UnmarshalJSON(b []byte) error  { *s = long(clipJSON(b, maxLongRunes)); return nil }

// clipJSON decodes a JSON string and cuts it to limit characters; a value that is no string is empty.
func clipJSON(b []byte, limit int) string {
	var value string
	if json.Unmarshal(b, &value) != nil {
		return ""
	}
	value = strings.ToValidUTF8(value, "")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}

// list is a list cut to maxItems entries; a value that is not a list of the entry type is dropped.
type list[T any] []T

func (l *list[T]) UnmarshalJSON(b []byte) error {
	var all []T
	if json.Unmarshal(b, &all) != nil {
		*l = nil
		return nil
	}
	if len(all) > maxItems {
		all = all[:maxItems]
	}
	*l = all
	return nil
}

// flag is a boolean that drops a value that is not one instead of failing the whole answer.
type flag struct{ set, value bool }

func (f *flag) UnmarshalJSON(b []byte) error {
	var value bool
	if json.Unmarshal(b, &value) == nil {
		*f = flag{set: true, value: value}
	}
	return nil
}

func (f flag) MarshalJSON() ([]byte, error) { return json.Marshal(f.value) }

func (f flag) IsZero() bool { return !f.set }

// integer is a whole number that drops a value that is not one, so a zero Google sends is kept and a value
// it leaves out stays out.
type integer struct {
	set   bool
	value int64
}

func (n *integer) UnmarshalJSON(b []byte) error {
	var value float64
	if json.Unmarshal(b, &value) == nil && value == math.Trunc(value) && math.Abs(value) < 1<<53 {
		*n = integer{set: true, value: int64(value)}
	}
	return nil
}

func (n integer) MarshalJSON() ([]byte, error) { return json.Marshal(n.value) }

func (n integer) IsZero() bool { return !n.set }

// number is a finite number that drops a value that is not one.
type number struct {
	set   bool
	value float64
}

func (n *number) UnmarshalJSON(b []byte) error {
	var value float64
	if json.Unmarshal(b, &value) == nil && !math.IsInf(value, 0) && !math.IsNaN(value) {
		*n = number{set: true, value: value}
	}
	return nil
}

func (n number) MarshalJSON() ([]byte, error) { return json.Marshal(n.value) }

func (n number) IsZero() bool { return !n.set }

// Output schema building blocks, written once for the types of this package.
const (
	stringSchema  = `{"type":"string"}`
	booleanSchema = `{"type":"boolean"}`
	integerSchema = `{"type":"integer"}`
	numberSchema  = `{"type":"number"}`
)

func arraySchema(item string) string { return `{"type":"array","items":` + item + `}` }

// objectSchema is a closed object schema of name and schema pairs.
func objectSchema(pairs ...string) string {
	members := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		members = append(members, `"`+pairs[i]+`":`+pairs[i+1])
	}
	return `{"type":"object","properties":{` + strings.Join(members, ",") + `},"additionalProperties":false}`
}

// postalAddress is Google's postal address type, shared by an organization and a location.
type postalAddress struct {
	Revision           integer    `json:"revision,omitzero"`
	RegionCode         short      `json:"regionCode,omitempty"`
	LanguageCode       short      `json:"languageCode,omitempty"`
	PostalCode         short      `json:"postalCode,omitempty"`
	SortingCode        short      `json:"sortingCode,omitempty"`
	AdministrativeArea line       `json:"administrativeArea,omitempty"`
	Locality           line       `json:"locality,omitempty"`
	Sublocality        line       `json:"sublocality,omitempty"`
	AddressLines       list[line] `json:"addressLines,omitempty"`
	Recipients         list[line] `json:"recipients,omitempty"`
	Organization       line       `json:"organization,omitempty"`
}

var postalAddressSchema = objectSchema(
	"revision", integerSchema, "regionCode", stringSchema, "languageCode", stringSchema,
	"postalCode", stringSchema, "sortingCode", stringSchema, "administrativeArea", stringSchema,
	"locality", stringSchema, "sublocality", stringSchema, "addressLines", arraySchema(stringSchema),
	"recipients", arraySchema(stringSchema), "organization", stringSchema)
