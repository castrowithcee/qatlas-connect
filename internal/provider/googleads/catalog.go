package googleads

import (
	"slices"
	"sort"
	"strings"
)

//go:generate go run ./gen -out catalog_gen.go -version v26

// The field catalog of the Google Ads API: every resource of the field reference with its fields, their
// capabilities and enum values, and which segments and metrics a query on the resource may use. The data is
// generated (catalog_gen.go) from the public reference and fixed with the provider, so neither the build nor a
// call needs the network. A query is built only from names this catalog knows; LookupField and
// Resource.Compat tell a name without an entry apart from one that has an entry but does not fit the query.

// Field categories of the reference.
const (
	CategoryAttribute = "ATTRIBUTE"
	CategorySegment   = "SEGMENT"
	CategoryMetric    = "METRIC"
)

// Flag bits of catalogFieldData.
const (
	flagSelectable = 1 << iota
	flagFilterable
	flagSortable
	flagRepeated
)

type catalogFieldData struct {
	name, category, dataType string
	enum                     int // index into catalogEnums, -1 for none
	flags                    int
}

type catalogEnum struct {
	name   string
	values []string
}

type catalogResourceData struct {
	name       string
	attributed []string
	with       int // index into catalogSets
}

// Field is one field of the reference.
type Field struct {
	// Name is the full GAQL name, such as campaign.id or metrics.clicks.
	Name     string
	Category string
	// DataType is the data type of the reference: STRING, INT64, DOUBLE, BOOLEAN, DATE, ENUM, MESSAGE and so on.
	DataType                                   string
	Selectable, Filterable, Sortable, Repeated bool
	// EnumType and EnumValues are set for an ENUM. The values are the ones the reference lists, including
	// UNSPECIFIED and UNKNOWN, which are never valid in a condition.
	EnumType   string
	EnumValues []string
}

func newField(d *catalogFieldData) Field {
	f := Field{
		Name: d.name, Category: d.category, DataType: d.dataType,
		Selectable: d.flags&flagSelectable != 0, Filterable: d.flags&flagFilterable != 0,
		Sortable: d.flags&flagSortable != 0, Repeated: d.flags&flagRepeated != 0,
	}
	if d.enum >= 0 {
		f.EnumType = catalogEnums[d.enum].name
		f.EnumValues = slices.Clone(catalogEnums[d.enum].values)
	}
	return f
}

func fieldAt(name string) (int, bool) {
	return sort.Find(len(catalogFields), func(i int) int { return strings.Compare(name, catalogFields[i].name) })
}

// LookupField returns the catalog entry of a field by its full name. ok is false for a name the catalog does
// not have; such a name is not to be sent to the API.
func LookupField(name string) (Field, bool) {
	i, ok := fieldAt(name)
	if !ok {
		return Field{}, false
	}
	return newField(&catalogFields[i]), true
}

// ResourceNames lists every resource of the catalog in name order.
func ResourceNames() []string {
	names := make([]string, len(catalogResources))
	for i := range catalogResources {
		names[i] = catalogResources[i].name
	}
	return names
}

// Resource is a resource of the catalog, the name a query may have in its FROM clause.
type Resource struct{ data *catalogResourceData }

// LookupResource returns the resource of the catalog by name.
func LookupResource(name string) (Resource, bool) {
	i, ok := sort.Find(len(catalogResources), func(i int) int { return strings.Compare(name, catalogResources[i].name) })
	if !ok {
		return Resource{}, false
	}
	return Resource{&catalogResources[i]}, true
}

// Name is the resource name as written in a FROM clause.
func (r Resource) Name() string { return r.data.name }

// AttributedResources lists the resources whose fields can be selected together with this resource without
// segmenting its metrics.
func (r Resource) AttributedResources() []string { return slices.Clone(r.data.attributed) }

// Fields lists the fields the resource itself defines, in name order.
func (r Resource) Fields() []Field {
	prefix := r.data.name + "."
	start, _ := sort.Find(len(catalogFields), func(i int) int { return strings.Compare(prefix, catalogFields[i].name) })
	var out []Field
	for i := start; i < len(catalogFields) && strings.HasPrefix(catalogFields[i].name, prefix); i++ {
		out = append(out, newField(&catalogFields[i]))
	}
	return out
}

// SegmentsAndMetrics lists the segments and metrics a query with this resource in FROM can use, in name order.
func (r Resource) SegmentsAndMetrics() []Field {
	set := catalogSets[r.data.with]
	out := make([]Field, len(set))
	for i, at := range set {
		out[i] = newField(&catalogFields[at])
	}
	return out
}

// Compat is how a field relates to a query on a resource.
type Compat int

const (
	// CompatUnknown: the catalog has no entry for the name.
	CompatUnknown Compat = iota
	// CompatNone: the field exists, but not together with this resource in FROM.
	CompatNone
	// CompatOK: the field is a field of the resource, of an attributed resource, or a segment or metric usable
	// with the resource. Whether it can be selected, filtered or sorted is up to the Field.
	CompatOK
)

// Compat tells whether a field name can appear in a query that has this resource in FROM.
func (r Resource) Compat(field string) Compat {
	at, ok := fieldAt(field)
	if !ok {
		return CompatUnknown
	}
	switch catalogFields[at].category {
	case CategoryAttribute:
		owner, _, _ := strings.Cut(field, ".")
		if owner == r.data.name || slices.Contains(r.data.attributed, owner) {
			return CompatOK
		}
	default:
		if _, found := slices.BinarySearch(catalogSets[r.data.with], uint16(at)); found {
			return CompatOK
		}
	}
	return CompatNone
}
