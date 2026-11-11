package restxml_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/restxml"
)

func marshal(t *testing.T, s *shape, v any) string {
	t.Helper()
	data, err := restxml.Codec{}.Marshal(s, v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(data)
}

// A document in the forms of both models: a namespace on the root, wrapped and flattened lists, attributes,
// a prefixed namespace on a member, a map, a timestamp, a blob, and nesting.
func exampleShape() *shape {
	grantee := obj(attrOf(f("type", "xsi:type", str())), f("id", "ID", str()), f("name", "DisplayName", str()))
	grant := obj(func() field {
		g := f("grantee", "Grantee", grantee)
		g.Namespace = httpbind.Namespace{URI: "http://www.w3.org/2001/XMLSchema-instance", Prefix: "xsi"}
		return g
	}(),
		f("permission", "Permission", str()))
	s := root("AccessControlPolicy", obj(
		f("grants", "AccessControlList", named(list(grant), "Grant")),
		flat(f("tags", "Tag", list(obj(f("key", "Key", str()), f("value", "Value", str()))))),
		f("meta", "Meta", mapOf(str())),
		flat(f("labels", "Label", mapOf(num()))),
		f("when", "When", stamp()),
		f("raw", "Raw", blob()),
		f("ids", "Ids", list(num())),
		f("on", "On", flag()),
		f("ratio", "Ratio", &shape{Kind: httpbind.KindFloat}),
		f("empty", "Empty", str()),
	))
	s.Namespace = httpbind.Namespace{URI: "http://s3.amazonaws.com/doc/2006-03-01/"}
	return s
}

func exampleValue() map[string]any {
	return map[string]any{
		"grants": []any{map[string]any{
			"grantee":    map[string]any{"type": "CanonicalUser", "id": "abc", "name": `Zoë <"q"> & 'x'`},
			"permission": "READ"}},
		"tags":   []any{map[string]any{"key": "a", "value": "1"}, map[string]any{"key": "b", "value": ""}},
		"meta":   map[string]any{"k2": "v2", "k1": "v1"},
		"labels": map[string]any{"x": int64(1)},
		"when":   "2026-01-02T03:04:05Z",
		"raw":    "aGVsbG8=",
		"ids":    []any{int64(1), int64(2)},
		"on":     true,
		"ratio":  1.5,
		"empty":  "",
	}
}

func TestMarshalFollowsTheXMLTraits(t *testing.T) {
	got := marshal(t, exampleShape(), exampleValue())
	for _, want := range []string{
		`<AccessControlPolicy xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`,
		`<AccessControlList><Grant>`,
		`<Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="CanonicalUser">`,
		`<DisplayName>Zoë &lt;&#34;q&#34;&gt; &amp; &#39;x&#39;</DisplayName>`,
		`<Tag><Key>a</Key><Value>1</Value></Tag><Tag><Key>b</Key><Value></Value></Tag>`,
		`<Meta><entry><key>k1</key><value>v1</value></entry><entry><key>k2</key><value>v2</value></entry></Meta>`,
		`<Label><key>x</key><value>1</value></Label>`,
		`<When>2026-01-02T03:04:05Z</When><Raw>aGVsbG8=</Raw>`,
		`<Ids><member>1</member><member>2</member></Ids>`,
		`<On>true</On><Ratio>1.5</Ratio><Empty></Empty></AccessControlPolicy>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("body lacks %s\n%s", want, got)
		}
	}
	// The same input always gives the same bytes.
	if again := marshal(t, exampleShape(), exampleValue()); again != got {
		t.Error("two encodings differ")
	}
}

// What was written reads back as the same value, in the key names of the shape.
func TestXMLRoundTrip(t *testing.T) {
	s := exampleShape()
	data := marshal(t, s, exampleValue())
	v, cut, err := restxml.Codec{}.Unmarshal(s, []byte(data))
	if err != nil || cut {
		t.Fatalf("Unmarshal: %v, cut %t", err, cut)
	}
	want := exampleValue()
	want["raw"] = "aGVsbG8="
	if !reflect.DeepEqual(v, want) {
		t.Errorf("round trip =\n%#v\nwant\n%#v", v, want)
	}
}

// Special characters and the three white space characters of XML survive; text XML 1.0 cannot carry is
// refused on the way out, with the member and no value in the message.
func TestSpecialAndControlCharacters(t *testing.T) {
	s := root("Doc", obj(f("name", "Name", str())))
	for name, text := range map[string]string{
		"markup": `<a href="x">&amp;</a>`, "white space": "a\tb\nc\rd", "edge space": "  x  ", "unicode": "日本語 \U0001F600",
		"entity-like": "&lt;&#65;&#x41;", "cdata end": "]]>", "empty": "",
	} {
		data := marshal(t, s, map[string]any{"name": text})
		v, _, err := restxml.Codec{}.Unmarshal(s, []byte(data))
		if err != nil || v.(map[string]any)["name"] != text {
			t.Errorf("%s: %q -> %s -> %v, %v", name, text, data, v, err)
		}
	}
	for name, text := range map[string]string{
		"bell": "a\a", "escape": "\x1b[0m", "nul": "\x00", "del is fine": "\x7f", "fffe": "￾", "ffff": "￿",
		"form feed": "\f", "unit separator": "\x1f", "invalid utf-8": "\xc3(", "lone surrogate": "\xed\xa0\x80",
	} {
		_, err := restxml.Codec{}.Marshal(s, map[string]any{"name": text})
		wantRefused := name != "del is fine"
		if (err != nil) != wantRefused {
			t.Errorf("%s: err = %v, want refused %t", name, err, wantRefused)
		}
		if err != nil && (!errors.Is(err, httpbind.ErrValue) || !strings.Contains(err.Error(), "name ") || strings.ContainsAny(err.Error(), "\x00\x1b\a\f")) {
			t.Errorf("%s: error = %q", name, err)
		}
	}
	// The same rules apply to an attribute, a map key and a namespace.
	attr := root("D", obj(attrOf(f("a", "id", str()))))
	if _, err := (restxml.Codec{}).Marshal(attr, map[string]any{"a": "x\x01"}); err == nil {
		t.Error("a control character in an attribute was written")
	}
	m := root("D", obj(f("m", "M", mapOf(str()))))
	if _, err := (restxml.Codec{}).Marshal(m, map[string]any{"m": map[string]any{"k\x01": "v"}}); err == nil {
		t.Error("a control character in a map key was written")
	}
}

// A character reference to a character XML 1.0 does not have is read as U+FFFD, so one odd key does not
// make a listing unreadable; a raw control character, a doctype and other encodings are errors.
func TestAnswersWithOddCharacters(t *testing.T) {
	s := root("R", obj(flat(f("keys", "Key", list(str())))))
	v, _, err := restxml.Codec{}.Unmarshal(s, []byte(`<R><Key>a&#x1;b</Key><Key>c&#0;d</Key><Key>&#xD800;</Key><Key>&#65;&#x42;&#xFFFF;</Key><Key>&#4294967295;</Key></R>`))
	if err != nil {
		t.Fatal(err)
	}
	got := v.(map[string]any)["keys"]
	if want := []any{"a�b", "c�d", "�", "AB�", "�"}; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %q, want %q", got, want)
	}
	for name, body := range map[string]string{
		"raw control":  "<R><Key>a\x01b</Key></R>",
		"huge ref":     "<R><Key>&#99999999999;</Key></R>",
		"doctype":      `<!DOCTYPE R [<!ENTITY x "y">]><R><Key>&x;</Key></R>`,
		"entity":       `<R><Key>&unknown;</Key></R>`,
		"latin-1":      `<?xml version="1.0" encoding="ISO-8859-1"?><R/>`,
		"second doc":   `<R/><R/>`,
		"text outside": `<R/>junk`,
		"unclosed":     `<R><Key>a</Key>`,
		"mismatch":     `<R><Key>a</Other></R>`,
		"empty":        ``,
		"pi":           `<R><?php x ?></R>`,
	} {
		if _, _, err := (restxml.Codec{}).Unmarshal(s, []byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A comment and the xml declaration are fine.
	if _, _, err := (restxml.Codec{}).Unmarshal(s, []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- c --><R><Key>a</Key></R>\n")); err != nil {
		t.Errorf("declaration and comment: %v", err)
	}
}

func TestMarshalRefusesWhatItCannotWrite(t *testing.T) {
	s := root("D", obj(f("a", "A", str()), f("n", "N", num()), flatList()))
	for name, c := range map[string]struct {
		shape  *shape
		value  any
		shaped bool
	}{
		"no root":        {obj(f("a", "A", str())), map[string]any{"a": "x"}, true},
		"unknown member": {s, map[string]any{"zzz": "x"}, false},
		"wrong type":     {s, map[string]any{"n": "x"}, false},
		"not a map":      {s, []any{}, false},
		"nil in list":    {s, map[string]any{"l": []any{nil}}, false},
		"document":       {root("D", obj(f("d", "Doc", &shape{Kind: httpbind.KindDocument}))), map[string]any{"d": 1}, true},
		"bad name":       {root("D", obj(f("a", "A b", str()))), map[string]any{"a": "x"}, true},
		"bad root":       {root("D>", obj(f("a", "A", str()))), map[string]any{"a": "x"}, true},
	} {
		_, err := restxml.Codec{}.Marshal(c.shape, c.value)
		if err == nil || errors.Is(err, httpbind.ErrShape) != c.shaped {
			t.Errorf("%s: err = %v, shape error %t", name, err, c.shaped)
		}
	}
	req := root("D", obj(func() field { x := f("a", "A", str()); x.Required = true; return x }()))
	if _, err := (restxml.Codec{}).Marshal(req, map[string]any{}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("missing required: %v", err)
	}
	un := root("D", &shape{Kind: httpbind.KindStruct, Union: true, Fields: []field{f("a", "A", str()), f("b", "B", str())}})
	if _, err := (restxml.Codec{}).Marshal(un, map[string]any{"a": "1", "b": "2"}); err == nil {
		t.Error("a union with two members was written")
	}
	if got := marshal(t, un, map[string]any{"b": "2"}); got != "<D><B>2</B></D>" {
		t.Errorf("union = %s", got)
	}
	if _, err := (restxml.Codec{}).Marshal(root("D", obj(f("n", "N", num()))), map[string]any{"n": math.NaN()}); err == nil {
		t.Error("NaN was written")
	}
	// An empty structure and an empty list are elements, an absent member is nothing.
	e := root("D", obj(f("l", "L", list(str())), f("o", "O", obj()), f("x", "X", str())))
	if got := marshal(t, e, map[string]any{"l": []any{}, "o": map[string]any{}}); got != "<D><L></L><O></O></D>" {
		t.Errorf("empty = %s", got)
	}
}

func flatList() field { return flat(f("l", "L", list(str()))) }

// The decoder reads empty elements as empty values, skips what the shape does not know, and refuses what is
// ambiguous or over its caps.
func TestUnmarshalForms(t *testing.T) {
	s := root("R", obj(
		f("name", "Name", str()), f("count", "Count", num()), f("list", "Items", list(str())), f("o", "Opt", obj(f("x", "X", str()))),
		f("m", "M", mapOf(str())), f("union", "U", &shape{Kind: httpbind.KindStruct, Union: true, Fields: []field{f("a", "A", str()), f("b", "B", str())}}),
		attrOf(f("id", "id", str())), flat(f("fl", "F", list(num()))), f("t", "T", stamp()), f("b", "B", flag()),
	))
	v, _, err := restxml.Codec{}.Unmarshal(s, []byte(`<R id="7"><Name/><Items/><Opt/><M/><Unknown><X>1</X></Unknown>
		<F>1</F><F>2</F><T>2026-01-02T03:04:05.5Z</T><B> true </B><Count> 12 </Count></R>`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"name": "", "items": nil}
	_ = want
	m := v.(map[string]any)
	if m["name"] != "" || !reflect.DeepEqual(m["list"], []any{}) || !reflect.DeepEqual(m["o"], map[string]any{}) ||
		!reflect.DeepEqual(m["m"], map[string]any{}) || m["id"] != "7" || !reflect.DeepEqual(m["fl"], []any{int64(1), int64(2)}) ||
		m["t"] != "2026-01-02T03:04:05.5Z" || m["b"] != true || m["count"] != int64(12) {
		t.Errorf("value = %#v", m)
	}
	for name, body := range map[string]string{
		"union with two":    `<R><U><A>1</A><B>2</B></U></R>`,
		"member twice":      `<R><Name>a</Name><Name>b</Name></R>`,
		"children in leaf":  `<R><Name><x/></Name></R>`,
		"bad timestamp":     `<R><T>yesterday</T></R>`,
		"empty number":      `<R><Count/></R>`,
		"map without value": `<R><M><entry><key>a</key></entry></M></R>`,
		"map key twice":     `<R><M><entry><key>a</key><value>1</value></entry><entry><key>a</key><value>2</value></entry></M></R>`,
	} {
		if _, _, err := (restxml.Codec{}).Unmarshal(s, []byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A union with no known member is empty, not an error.
	if v, _, err := (restxml.Codec{}).Unmarshal(s, []byte(`<R><U><Z>1</Z></U></R>`)); err != nil || !reflect.DeepEqual(v.(map[string]any)["union"], map[string]any{}) {
		t.Errorf("unknown union member: %v, %v", v, err)
	}
	// A list above its cap is refused: cutting it would show a partial list as complete.
	capped := root("R", obj(flat(f("k", "K", &shape{Kind: httpbind.KindList, Elem: str(), MaxItems: 2}))))
	if _, _, err := (restxml.Codec{}).Unmarshal(capped, []byte(`<R><K>a</K><K>b</K><K>c</K></R>`)); err == nil {
		t.Error("a list above its cap was accepted")
	}
	// A string above its cap is cut at a rune boundary and reported.
	cs := root("R", obj(f("n", "N", &shape{Kind: httpbind.KindString, MaxLen: 4})))
	v, cut, err := (restxml.Codec{}).Unmarshal(cs, []byte(`<R><N>ab€x</N></R>`))
	if err != nil || !cut || v.(map[string]any)["n"] != "ab" {
		t.Errorf("cut string = %v, cut %t, %v", v, cut, err)
	}
}

// The unwrapped member is the root element itself.
func TestUnwrappedMember(t *testing.T) {
	s := obj(func() field { x := f("loc", "LocationConstraint", str()); x.Unwrapped = true; return x }())
	v, _, err := restxml.Codec{}.Unmarshal(s, []byte(`<LocationConstraint xmlns="x">eu-west-1</LocationConstraint>`))
	if err != nil || v.(map[string]any)["loc"] != "eu-west-1" {
		t.Errorf("unwrapped = %v, %v", v, err)
	}
	if v, _, err := (restxml.Codec{}).Unmarshal(s, []byte(`<LocationConstraint/>`)); err != nil || v.(map[string]any)["loc"] != "" {
		t.Errorf("empty unwrapped = %v, %v", v, err)
	}
}

// Depth and size are bounded before anything is built.
func TestDocumentBounds(t *testing.T) {
	deep := strings.Repeat("<a>", 100) + strings.Repeat("</a>", 100)
	if _, _, err := (restxml.Codec{}).Unmarshal(obj(), []byte(deep)); err == nil {
		t.Error("a document nested 100 deep was accepted")
	}
	wide := "<R>" + strings.Repeat("<a/>", 150000) + "</R>"
	if _, _, err := (restxml.Codec{}).Unmarshal(obj(), []byte(wide)); err == nil {
		t.Error("a document of 150000 elements was accepted")
	}
	items := make([]any, 120000)
	for i := range items {
		items[i] = "x"
	}
	if _, err := (restxml.Codec{}).Marshal(root("R", obj(flat(f("k", "K", list(str()))))), map[string]any{"k": items}); err == nil {
		t.Error("a request of 120000 elements was written")
	}
}

// The XML members of a description are checked like the rest of it.
func TestInvalidXMLDescriptionsAreRefused(t *testing.T) {
	for name, bad := range map[string]*shape{
		"flattened scalar":  root("D", obj(flat(f("a", "A", str())))),
		"attribute list":    root("D", obj(attrOf(f("a", "A", list(str()))))),
		"elem name on map":  root("D", obj(f("a", "A", &shape{Kind: httpbind.KindMap, Elem: str(), ElemName: "x"}))),
		"bad xml name":      root("D", obj(f("a", "A", &shape{Kind: httpbind.KindList, Elem: str(), ElemName: "a b"}))),
		"namespace on text": root("D", obj(f("a", "A", &shape{Kind: httpbind.KindString, Namespace: httpbind.Namespace{URI: "u"}}))),
		"prefix on a list":  root("D", obj(f("a", "A", &shape{Kind: httpbind.KindList, Elem: str(), Prefixes: []string{"/x/"}}))),
	} {
		op := &httpbind.Operation{Method: "PUT", URI: "/x", Input: bad}
		if err := op.Validate(); !errors.Is(err, httpbind.ErrShape) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	two := obj(func() field { x := f("a", "A", str()); x.Unwrapped = true; return x }(), f("b", "B", str()))
	if err := (&httpbind.Operation{Method: "GET", URI: "/x", Output: two}).Validate(); !errors.Is(err, httpbind.ErrShape) {
		t.Errorf("unwrapped with a sibling: %v", err)
	}
	one := obj(func() field { x := f("a", "A", str()); x.Unwrapped = true; return x }())
	if err := (&httpbind.Operation{Method: "PUT", URI: "/x", Input: one}).Validate(); !errors.Is(err, httpbind.ErrShape) {
		t.Errorf("unwrapped in an input: %v", err)
	}
}
