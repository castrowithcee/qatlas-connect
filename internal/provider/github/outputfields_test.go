package github

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// unemittedMembers lists, per tool, the members of a Go type shared with another tool that this tool never
// sets, so its schema rightly omits them: a list leaves out what only the detail read adds, and a change
// sets what only its counterpart sets.
var unemittedMembers = map[string][]string{
	// GitHub documents no expiry for the single budget read.
	"github.billing-budgets.get":       {"expires_at"},
	"github.code-scanning-alerts.list": {"alerts[].linked_pull_requests", "alerts[].linked_pull_requests_truncated"},
	"github.gist-forks.create": {"files[].binary", "files[].content", "files[].content_truncated", "forks",
		"forks_truncated", "history", "history_truncated"},
	"github.gist-forks.list":         {"gists[].files[].binary", "gists[].files[].content", "gists[].files[].content_truncated"},
	"github.gists.create":            {"files[].binary", "files[].content", "files[].content_truncated"},
	"github.gists.update":            {"files[].binary", "files[].content", "files[].content_truncated"},
	"github.gists.get":               {"forks", "forks_truncated", "history", "history_truncated"},
	"github.gists.list":              {"gists[].files[].binary", "gists[].files[].content", "gists[].files[].content_truncated"},
	"github.issue-dependencies.add":  {"removed"},
	"github.sub-issues.add":          {"removed"},
	"github.sub-issues.reprioritize": {"removed", "sub_issues_summary"},
	"github.project-items.list":      {"items[].body"},
	// The repository search match carries the repository's members beside its compact ones, which already
	// hold the full name and the star count.
	"github.repositories.search": {"repositories[].full_name", "repositories[].stargazers_count"},
}

// checkOutput reports every member of the output of sample that the output schema or the fields lack, at any
// depth of the schema, and every top-level schema member without a field. Fields name top-level members; a
// field may also name a member of the entries of a top-level list, which documents that entry member.
// Members in unemitted are shared-type members the tool never sets; each must still be found, so a stale
// entry fails.
func checkOutput(sample any, rawSchema json.RawMessage, fields []capability.Field, unemitted []string) []string {
	var schema map[string]any
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		return []string{"output schema unreadable: " + err.Error()}
	}
	var findings []string
	properties, _ := schema["properties"].(map[string]any)
	named := map[string]bool{}
	for _, field := range fields {
		named[field.Name] = true
		if _, ok := properties[field.Name]; !ok && !isEntryMember(properties, field.Name) {
			findings = append(findings, field.Name+": field without schema member")
		}
	}
	for name := range properties {
		if !named[name] {
			findings = append(findings, name+": schema member without field")
		}
	}
	var missing []string
	if value := reflect.ValueOf(sample); value.Kind() == reflect.Map {
		for _, key := range value.MapKeys() {
			if _, ok := properties[key.String()]; !ok {
				missing = append(missing, key.String())
			}
		}
	} else {
		missing = outputMembers(reflect.TypeOf(sample), schema, "", 0)
	}
	skipped := map[string]bool{}
	for _, path := range unemitted {
		skipped[path] = true
	}
	for _, path := range missing {
		if skipped[path] {
			delete(skipped, path)
			continue
		}
		findings = append(findings, path+": not in schema")
		if top, _, _ := strings.Cut(path, "."); !named[strings.TrimSuffix(top, "[]")] {
			findings = append(findings, path+": top-level member not in fields")
		}
	}
	for path := range skipped {
		findings = append(findings, path+": listed as unemitted but in the schema or not a member")
	}
	sort.Strings(findings)
	return findings
}

// isEntryMember reports whether name is a member of the entries of a top-level list of properties.
func isEntryMember(properties map[string]any, name string) bool {
	for _, property := range properties {
		list, _ := property.(map[string]any)
		items, _ := list["items"].(map[string]any)
		members, _ := items["properties"].(map[string]any)
		if _, ok := members[name]; ok {
			return true
		}
	}
	return false
}

var timeType = reflect.TypeOf(time.Time{})

// outputMembers returns the path of every member of t that schema lacks. A free-form value (any, raw JSON,
// a map of such) ends the descent: its members are not a Go type. A schema node of an object without
// properties counts as having none, so a typed member below it is reported rather than skipped.
func outputMembers(t reflect.Type, schema map[string]any, path string, depth int) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if depth > 16 || t == timeType {
		return nil
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		items, ok := schema["items"].(map[string]any)
		if !ok {
			if isTyped(t.Elem()) {
				return []string{path + "[]: no items schema"}
			}
			return nil
		}
		return outputMembers(t.Elem(), items, path+"[]", depth+1)
	case reflect.Map:
		if !isTyped(t.Elem()) {
			return nil
		}
		values, ok := schema["additionalProperties"].(map[string]any)
		if !ok {
			return []string{path + "{}: no value schema"}
		}
		return outputMembers(t.Elem(), values, path+"{}", depth+1)
	case reflect.Struct:
		if t.Implements(reflect.TypeOf((*json.Marshaler)(nil)).Elem()) {
			return []string{path + ": custom marshaler hides the members"}
		}
		properties, _ := schema["properties"].(map[string]any)
		var missing []string
		for name, member := range jsonMembers(t) {
			child := joinPath(path, name)
			sub, ok := properties[name].(map[string]any)
			if !ok {
				missing = append(missing, child)
				continue
			}
			missing = append(missing, outputMembers(member, sub, child, depth+1)...)
		}
		return missing
	}
	return nil
}

// isTyped reports whether a value of t has members of its own that the schema must describe.
func isTyped(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return false
	}
	switch t.Kind() {
	case reflect.Struct:
		return true
	case reflect.Slice, reflect.Array, reflect.Map:
		return isTyped(t.Elem())
	}
	return false
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// jsonMembers returns the JSON name and type of every member encoding/json emits for struct t, including
// those of an embedded struct without a name of its own.
func jsonMembers(t reflect.Type) map[string]reflect.Type {
	members := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if tag == "-" || !field.IsExported() && !field.Anonymous {
			continue
		}
		if field.Anonymous && name == "" {
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				for inner, innerType := range jsonMembers(embedded) {
					members[inner] = innerType
				}
				continue
			}
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		members[name] = field.Type
	}
	return members
}

func TestEveryOutputMemberIsInSchemaAndFields(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	expected := expectedByID()
	registered := map[string]bool{}
	for _, descriptor := range reg.All() {
		registered[descriptor.ID] = true
		tool, ok := expected[descriptor.ID]
		if !ok {
			t.Errorf("%s has no expected tool entry", descriptor.ID)
			continue
		}
		if findings := checkOutput(tool.output, descriptor.OutputSchema, descriptor.Fields, unemittedMembers[descriptor.ID]); len(findings) > 0 {
			t.Errorf("%s: %s", descriptor.ID, strings.Join(findings, "; "))
		}
	}
	for id := range expected {
		if !registered[id] {
			t.Errorf("the expected tools name %s, which no tool carries", id)
		}
	}
}

type extraNested struct {
	Known   string `json:"known"`
	Hidden  string `json:"hidden"`
	private string
}

type extraOutput struct {
	Name    string        `json:"name"`
	Extra   string        `json:"extra"`
	Items   []extraNested `json:"items"`
	Skipped string        `json:"-"`
}

func TestCheckOutputReportsAnUndeclaredMember(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"items":{"type":"array",` +
		`"items":{"type":"object","properties":{"known":{"type":"string"}},"additionalProperties":false}}},` +
		`"additionalProperties":false}`)
	fields := []capability.Field{{Name: "name"}, {Name: "items"}}
	got := strings.Join(checkOutput(extraOutput{}, schema, fields, nil), "; ")
	for _, want := range []string{"extra: not in schema", "extra: top-level member not in fields",
		"items[].hidden: not in schema"} {
		if !strings.Contains(got, want) {
			t.Errorf("findings lack %q: %s", want, got)
		}
	}
	for _, unwanted := range []string{"name:", "known", "Skipped", "private"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("findings carry %q: %s", unwanted, got)
		}
	}
	if findings := checkOutput(extraOutput{}, schema, append(fields, capability.Field{Name: "gone"}), nil); !strings.Contains(
		strings.Join(findings, ";"), "gone: field without schema member") {
		t.Errorf("a field without schema member is not reported: %v", findings)
	}
}
