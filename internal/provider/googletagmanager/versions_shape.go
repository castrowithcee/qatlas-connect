package googletagmanager

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

// A container version holds the whole configuration of a container, including Custom HTML, Custom JavaScript,
// and template code. Its entities reuse the types of the workspace tools. Unlike those, a version is only read,
// so what a read shows is bounded: a text longer than maxVersionTextRunes is cut, a list longer than its limit
// is shortened, and a version whose encoding still exceeds maxVersionBytes loses trailing entities of its
// largest lists. Every cut is reported in the result, so a partial output is never taken for the whole.

// Bounds of what a version result shows.
const (
	maxVersionTextRunes = 20000
	maxVersionListItems = 500
	maxVersionBytes     = 1 << 20
	// maxVersionResponse bounds what is read from Google before the result is bounded, well above maxResponse
	// because a large version is cut here instead of refused.
	maxVersionResponse = 32 << 20
)

// containerVersion is a ContainerVersion of the API. The members that Google does not list for it, such as an
// authorization code of a preview, are never held and so never shown.
type containerVersion struct {
	Path               idText                 `json:"path,omitempty"`
	AccountID          idText                 `json:"accountId,omitempty"`
	ContainerID        idText                 `json:"containerId,omitempty"`
	ContainerVersionID idText                 `json:"containerVersionId,omitempty"`
	Name               nameText               `json:"name,omitempty"`
	Description        noteText               `json:"description,omitempty"`
	Fingerprint        idText                 `json:"fingerprint,omitempty"`
	Deleted            bool                   `json:"deleted,omitempty"`
	TagManagerURL      urlText                `json:"tagManagerUrl,omitempty"`
	Container          *container             `json:"container,omitempty"`
	Tag                []tag                  `json:"tag,omitempty"`
	Trigger            []trigger              `json:"trigger,omitempty"`
	Variable           []variable             `json:"variable,omitempty"`
	BuiltInVariable    []builtInVariable      `json:"builtInVariable,omitempty"`
	Folder             []folder               `json:"folder,omitempty"`
	CustomTemplate     []template             `json:"customTemplate,omitempty"`
	Client             []clientEntity         `json:"client,omitempty"`
	Transformation     []transformationEntity `json:"transformation,omitempty"`
	Zone               []zoneEntity           `json:"zone,omitempty"`
	GtagConfig         []gtagConfigEntity     `json:"gtagConfig,omitempty"`
}

// versionResult is the shown form of a version: the bounded version and, when anything was cut, the report.
type versionResult struct {
	containerVersion
	Truncated  bool               `json:"truncated,omitempty"`
	Truncation *versionTruncation `json:"truncation,omitempty"`
}

type versionTruncation struct {
	Notice   string    `json:"notice"`
	TextsCut int       `json:"textsCut,omitempty"`
	ListsCut []listCut `json:"listsCut,omitempty"`
}

// listCut says how many entries of a list were kept. Nested lists are named by their path, such as
// tag.parameter, and add up over all entries that have one.
type listCut struct {
	List  string `json:"list"`
	Kept  int    `json:"kept"`
	Total int    `json:"total"`
}

const versionTruncationNotice = "Partial output: this version is larger than the output bounds, so texts were cut " +
	"and lists were shortened as listed here. What is shown is not the whole configuration of the version."

// versionOutput bounds a version for output. It is the one form in which any tool shows a container version.
func versionOutput(v containerVersion) versionResult {
	c := &cutter{lists: map[string]*listCut{}}
	c.walk(reflect.ValueOf(&v).Elem(), "", false)
	c.fit(&v)
	out := versionResult{containerVersion: v}
	if c.texts == 0 && len(c.lists) == 0 {
		return out
	}
	out.Truncated = true
	out.Truncation = &versionTruncation{Notice: versionTruncationNotice, TextsCut: c.texts}
	for _, entry := range c.lists {
		out.Truncation.ListsCut = append(out.Truncation.ListsCut, *entry)
	}
	slices.SortFunc(out.Truncation.ListsCut, func(a, b listCut) int { return strings.Compare(a.List, b.List) })
	return out
}

type cutter struct {
	texts int
	lists map[string]*listCut
}

// walk cuts every text and list below v in place. Entity lists of the version itself (top) hold up to
// maxEntries entries, every nested list up to maxVersionListItems.
func (c *cutter) walk(v reflect.Value, path string, nested bool) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			c.walk(v.Elem(), path, nested)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() && !f.Anonymous {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if f.Anonymous || name == "" {
				c.walk(v.Field(i), path, nested)
				continue
			}
			child := name
			if path != "" {
				child = path + "." + name
			}
			c.walk(v.Field(i), child, path != "")
		}
	case reflect.Slice:
		limit := maxEntries
		if nested {
			limit = maxVersionListItems
		}
		if v.Len() > limit {
			c.record(path, limit, v.Len(), nested)
			v.Set(v.Slice(0, limit))
		}
		for i := 0; i < v.Len(); i++ {
			c.walk(v.Index(i), path, nested)
		}
	case reflect.String:
		if clipped, cut := clipText(v.String(), maxVersionTextRunes); cut {
			v.SetString(clipped)
			c.texts++
		}
	}
}

func (c *cutter) record(path string, kept, total int, add bool) {
	entry := c.lists[path]
	switch {
	case entry == nil:
		c.lists[path] = &listCut{List: path, Kept: kept, Total: total}
	case add:
		entry.Kept += kept
		entry.Total += total
	default:
		entry.Kept = kept
	}
}

func clipText(s string, limit int) (string, bool) {
	if len(s) <= limit || utf8.RuneCountInString(s) <= limit {
		return s, false
	}
	return string([]rune(s)[:limit-1]) + "…", true
}

// fit halves the largest entity list until the encoded version is within maxVersionBytes. The version header
// members stay, so even a version that loses every entity names itself.
func (c *cutter) fit(v *containerVersion) {
	sections := reflect.ValueOf(v).Elem()
	for size(v) > maxVersionBytes {
		best, bestSize := -1, 0
		for i := 0; i < sections.NumField(); i++ {
			f := sections.Field(i)
			if f.Kind() != reflect.Slice || f.Len() == 0 {
				continue
			}
			if n := len(mustEncode(f.Interface())); n > bestSize {
				best, bestSize = i, n
			}
		}
		if best < 0 {
			return
		}
		f := sections.Field(best)
		total, keep := f.Len(), f.Len()/2
		name, _, _ := strings.Cut(sections.Type().Field(best).Tag.Get("json"), ",")
		c.record(name, keep, total, false)
		f.Set(f.Slice(0, keep))
	}
}

func size(v *containerVersion) int { return len(mustEncode(v)) }

func mustEncode(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// numText is a count that Google sends as a string; a bare number is accepted as well. Anything else is empty.
type numText string

func (n *numText) UnmarshalJSON(b []byte) error {
	var number json.Number
	if json.Unmarshal(b, &number) == nil {
		text, _ := clipText(number.String(), maxIDRunes)
		*n = numText(text)
		return nil
	}
	*n = numText(clipJSON(b, maxIDRunes))
	return nil
}

// versionHeader is a ContainerVersionHeader: the identity of a version and how many objects of each type it holds.
type versionHeader struct {
	Path               idText   `json:"path,omitempty"`
	AccountID          idText   `json:"accountId,omitempty"`
	ContainerID        idText   `json:"containerId,omitempty"`
	ContainerVersionID idText   `json:"containerVersionId,omitempty"`
	Name               nameText `json:"name,omitempty"`
	Deleted            bool     `json:"deleted,omitempty"`
	NumMacros          numText  `json:"numMacros,omitempty"`
	NumRules           numText  `json:"numRules,omitempty"`
	NumTags            numText  `json:"numTags,omitempty"`
	NumTriggers        numText  `json:"numTriggers,omitempty"`
	NumVariables       numText  `json:"numVariables,omitempty"`
	NumZones           numText  `json:"numZones,omitempty"`
	NumCustomTemplates numText  `json:"numCustomTemplates,omitempty"`
	NumClients         numText  `json:"numClients,omitempty"`
	NumGtagConfigs     numText  `json:"numGtagConfigs,omitempty"`
	NumTransformations numText  `json:"numTransformations,omitempty"`
}

type (
	versionHeadersAnswer struct {
		ContainerVersionHeader []versionHeader `json:"containerVersionHeader"`
		NextPageToken          string          `json:"nextPageToken"`
	}
	versionHeadersResult struct {
		VersionHeaders []versionHeader `json:"versionHeaders"`
		NextCursor     string          `json:"next_cursor,omitempty"`
	}
)

func listOf(item string) string { return `{"type":"array","items":` + item + `}` }

var versionSchema = objectSchema(member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("containerVersionId", stringSchema), member("name", stringSchema),
	member("description", stringSchema), member("fingerprint", stringSchema), member("deleted", boolSchema),
	member("tagManagerUrl", stringSchema), member("container", containerSchema),
	member("tag", listOf(tagSchema)), member("trigger", listOf(triggerSchema)),
	member("variable", listOf(variableSchema)), member("builtInVariable", listOf(builtInSchema)),
	member("folder", listOf(folderSchema)), member("customTemplate", listOf(templateSchema)),
	member("client", listOf(clientSchema)), member("transformation", listOf(transformationSchema)),
	member("zone", listOf(zoneSchema)), member("gtagConfig", listOf(gtagConfigSchema)),
	member("truncated", boolSchema),
	member("truncation", objectSchema(member("notice", stringSchema),
		member("textsCut", `{"type":"integer"}`),
		member("listsCut", listOf(objectSchema(member("list", stringSchema), member("kept", `{"type":"integer"}`),
			member("total", `{"type":"integer"}`)))))))

var versionHeaderSchema = objectSchema(member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("containerVersionId", stringSchema), member("name", stringSchema),
	member("deleted", boolSchema), member("numMacros", stringSchema), member("numRules", stringSchema),
	member("numTags", stringSchema), member("numTriggers", stringSchema), member("numVariables", stringSchema),
	member("numZones", stringSchema), member("numCustomTemplates", stringSchema), member("numClients", stringSchema),
	member("numGtagConfigs", stringSchema), member("numTransformations", stringSchema))
