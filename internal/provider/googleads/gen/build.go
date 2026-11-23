package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// indexField is one entry of fields_index.json, the machine-readable download of the field reference.
type indexField struct {
	Category   string `json:"category"`
	DataType   string `json:"data_type"`
	Filterable bool   `json:"filterable"`
	Repeated   bool   `json:"is_repeated"`
	Selectable bool   `json:"selectable"`
	Sortable   bool   `json:"sortable"`
}

func parseIndex(r io.Reader) (map[string]indexField, error) {
	var m map[string]indexField
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return nil, fmt.Errorf("parse field index: %w", err)
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("field index is empty")
	}
	return m, nil
}

// sources is everything the catalog is built from.
type sources struct {
	version   string
	resources []string // the resource names of the overview
	index     map[string]indexField
	pages     map[string]*pageDoc // resource pages by resource name
	segments  *pageDoc
	metrics   *pageDoc
}

// Flag bits of a generated field; catalog.go reads them with the same values.
const (
	flagSelectable = 1 << iota
	flagFilterable
	flagSortable
	flagRepeated
)

type catalogField struct {
	name, category, dataType string
	enum                     int // index into enums, -1 for none
	flags                    int
}

type catalogEnum struct {
	name   string
	values []string
}

type catalogResource struct {
	name       string
	attributed []string
	with       int // index into sets: the segments and metrics usable with the resource in FROM
}

type catalog struct {
	version   string
	fields    []catalogField
	enums     []catalogEnum
	resources []catalogResource
	sets      [][]int // field indexes, sorted; sets[0] is empty
}

func categoryOf(name string) string {
	switch {
	case strings.HasPrefix(name, "segments."):
		return "SEGMENT"
	case strings.HasPrefix(name, "metrics."):
		return "METRIC"
	}
	return "ATTRIBUTE"
}

func ownerOf(name string) string {
	owner, _, _ := strings.Cut(name, ".")
	return owner
}

// build joins the sources into the catalog and fails on every inconsistency between them.
func build(src sources) (*catalog, error) {
	var errs []string
	fail := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	names := make([]string, 0, len(src.index))
	for n := range src.index {
		names = append(names, n)
	}
	sort.Strings(names)
	pos := make(map[string]int, len(names))
	for i, n := range names {
		pos[n] = i
	}

	// Resources of the overview against the owners of the index.
	resources := slices.Clone(src.resources)
	sort.Strings(resources)
	isResource := map[string]bool{}
	for _, r := range resources {
		isResource[r] = true
	}
	owners := map[string]bool{}
	for _, n := range names {
		f := src.index[n]
		if f.Category != categoryOf(n) {
			fail("%s: category %s does not match its name", n, f.Category)
		}
		if o := ownerOf(n); o != "segments" && o != "metrics" {
			owners[o] = true
		}
	}
	for r := range isResource {
		if !owners[r] {
			fail("resource %s of the overview has no field in the index", r)
		}
	}
	for o := range owners {
		if !isResource[o] {
			fail("owner %s of index fields is not a resource of the overview", o)
		}
	}

	// Details of all pages merged; the same field must read the same everywhere.
	details := map[string]fieldDetail{}
	merge := func(from string, p *pageDoc) {
		for n, d := range p.details {
			if old, ok := details[n]; ok {
				if old.dataType != d.dataType || old.typeName != d.typeName || !slices.Equal(old.enum, d.enum) {
					fail("%s: page %s disagrees with another page", n, from)
				}
				continue
			}
			details[n] = d
		}
	}
	for _, r := range resources {
		if src.pages[r] == nil {
			fail("resource %s has no page", r)
			continue
		}
		merge(r, src.pages[r])
	}
	merge("segments", src.segments)
	merge("metrics", src.metrics)
	for owner, p := range map[string]*pageDoc{"segments": src.segments, "metrics": src.metrics} {
		var want, got []string
		for _, n := range names {
			if ownerOf(n) == owner {
				want = append(want, n)
			}
		}
		for n := range p.details {
			if ownerOf(n) == owner {
				got = append(got, n)
			}
		}
		sort.Strings(got)
		if !slices.Equal(got, want) {
			fail("%s page lists %d fields, the index %d", owner, len(got), len(want))
		}
	}

	// Enum table by type name.
	enumAt := map[string]int{}
	var enums []catalogEnum
	fields := make([]catalogField, len(names))
	for i, n := range names {
		f := src.index[n]
		cf := catalogField{name: n, category: f.Category, dataType: f.DataType, enum: -1}
		for bit, on := range map[int]bool{flagSelectable: f.Selectable, flagFilterable: f.Filterable, flagSortable: f.Sortable, flagRepeated: f.Repeated} {
			if on {
				cf.flags |= bit
			}
		}
		d, ok := details[n]
		if ok && d.dataType != f.DataType {
			fail("%s: index says %s, page says %s", n, f.DataType, d.dataType)
		}
		if f.DataType == "ENUM" {
			if !ok || len(d.enum) == 0 {
				fail("%s: enum field without values on any page", n)
			} else {
				key := d.typeName
				if key == "" {
					key = n
				}
				at, seen := enumAt[key]
				if !seen {
					at = len(enums)
					enumAt[key] = at
					enums = append(enums, catalogEnum{name: key, values: d.enum})
				} else if !slices.Equal(enums[at].values, d.enum) {
					fail("%s: enum %s lists other values than on another field", n, key)
				}
				cf.enum = at
			}
		} else if ok && len(d.enum) > 0 {
			fail("%s: values listed for %s", n, f.DataType)
		}
		fields[i] = cf
	}
	// Stable enum order: by name.
	order := make([]int, len(enums))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return enums[order[a]].name < enums[order[b]].name })
	remap := make([]int, len(enums))
	sorted := make([]catalogEnum, len(enums))
	for to, from := range order {
		remap[from] = to
		sorted[to] = enums[from]
	}
	for i := range fields {
		if fields[i].enum >= 0 {
			fields[i].enum = remap[fields[i].enum]
		}
	}

	// Resources: own fields against the index, attributed resources, FROM-compatible segments and metrics.
	sets := [][]int{{}}
	setAt := map[string]int{"": 0}
	var out []catalogResource
	for _, r := range resources {
		p := src.pages[r]
		if p == nil {
			continue
		}
		var want []string
		for _, n := range names {
			if ownerOf(n) == r {
				want = append(want, n)
			}
		}
		var got []string
		for n := range p.details {
			if ownerOf(n) == r {
				got = append(got, n)
			}
		}
		sort.Strings(got)
		if !slices.Equal(got, want) {
			fail("resource %s: the page lists %d fields, the index %d", r, len(got), len(want))
		}
		att := slices.Clone(p.attributed)
		sort.Strings(att)
		for _, a := range att {
			if !isResource[a] {
				fail("resource %s: attributed resource %s is unknown", r, a)
			}
		}
		var with []int
		for _, list := range [][]listEntry{p.segments, p.metrics} {
			for _, e := range list {
				i, ok := pos[e.name]
				switch {
				case !ok:
					fail("resource %s: %s is not in the index", r, e.name)
				case e.fromOK:
					with = append(with, i)
				}
			}
		}
		sort.Ints(with)
		with = slices.Compact(with)
		key := strings.Join(strings.Fields(fmt.Sprint(with)), ",")
		if len(with) == 0 {
			key = ""
		}
		at, ok := setAt[key]
		if !ok {
			at = len(sets)
			setAt[key] = at
			sets = append(sets, with)
		}
		out = append(out, catalogResource{name: r, attributed: att, with: at})
	}
	if len(errs) > 0 {
		if len(errs) > 25 {
			errs = append(errs[:25], fmt.Sprintf("... and %d more", len(errs)-25))
		}
		return nil, fmt.Errorf("catalog sources disagree:\n  %s", strings.Join(errs, "\n  "))
	}
	return &catalog{version: src.version, fields: fields, enums: sorted, resources: out, sets: sets}, nil
}

// header is the first line of the generated file. The generator overwrites only a file that starts with it.
const header = "// Code generated by adsgen from the public Google Ads API field reference; DO NOT EDIT.\n"

// render writes the catalog as the Go source of package googleads.
func render(c *catalog) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("\npackage googleads\n\n")
	fmt.Fprintf(&b, "// catalogAPIVersion is the API version whose field reference the catalog was generated from.\nconst catalogAPIVersion = %s\n\n", strconv.Quote(c.version))
	b.WriteString("var catalogEnums = []catalogEnum{\n")
	for _, e := range c.enums {
		fmt.Fprintf(&b, "\t{%s, []string{%s}},\n", strconv.Quote(e.name), quoteList(e.values))
	}
	b.WriteString("}\n\n// Name, category, data type, enum, flags: 1 selectable, 2 filterable, 4 sortable, 8 repeated.\nvar catalogFields = []catalogFieldData{\n")
	for _, f := range c.fields {
		fmt.Fprintf(&b, "\t{%s, %s, %s, %d, %d},\n", strconv.Quote(f.name), strconv.Quote(f.category), strconv.Quote(f.dataType), f.enum, f.flags)
	}
	b.WriteString("}\n\n// Indexes into catalogFields.\nvar catalogSets = [][]uint16{\n")
	for _, s := range c.sets {
		var parts []string
		for _, i := range s {
			parts = append(parts, strconv.Itoa(i))
		}
		fmt.Fprintf(&b, "\t{%s},\n", strings.Join(parts, ", "))
	}
	b.WriteString("}\n\nvar catalogResources = []catalogResourceData{\n")
	for _, r := range c.resources {
		fmt.Fprintf(&b, "\t{%s, []string{%s}, %d},\n", strconv.Quote(r.name), quoteList(r.attributed), r.with)
	}
	b.WriteString("}\n")
	out, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w", err)
	}
	return out, nil
}

func quoteList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = strconv.Quote(s)
	}
	return strings.Join(q, ", ")
}
