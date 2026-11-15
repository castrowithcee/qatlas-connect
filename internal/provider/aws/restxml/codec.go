// Package restxml implements the XML body and the error shapes of the AWS restXml protocol, as used by S3 and
// Route 53. Everything that is HTTP, the method and URI pattern, labels, query, headers, payload and the
// exchange itself, lives in package httpbind; this package is its Codec for XML. An operation is bound by
// data: an httpbind.Operation whose Fields carry the element names of the model (xmlName) and the XML members
// of the Shape and Field types.
//
// Body rules of the specification that this codec follows: members are elements named by their xmlName or
// member name; xmlAttribute members are attributes; xmlFlattened lists and maps are repeated elements
// without a wrapper; a list otherwise wraps its items in elements named member (or the xmlName of the list
// member) and a map its entries in entry elements with key and value; xmlNamespace is declared on the element
// it belongs to; timestamps are date-time unless the shape names a format; blobs are base64 text; unions are
// structures with exactly one member. Errors come from the Code element of an Error element, wrapped in an
// ErrorResponse element unless the service says it does not wrap them, and the Message element is never read.
//
// Text that XML 1.0 cannot represent (control characters other than tab, line feed and carriage return,
// unpaired surrogates, U+FFFE and U+FFFF) is refused in a request, not escaped into a document the service
// would reject. In an answer a character reference to such a character is read as U+FFFD, so one odd key does
// not make a whole listing unreadable and the loss stays visible.
package restxml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
)

// ContentType is the media type of an XML body. Accept is what every request asks for.
const (
	ContentType = "application/xml"
	Accept      = "application/xml"
)

// Bounds of one document. The size limit of the answer bounds the bytes; these bound the work and the memory
// of the tree.
const (
	maxNodes    = 100000
	maxXMLDepth = 32
	// maxErrorCode bounds the length of an error code or type.
	maxErrorCode = 256
)

// Codec is the restXml body format.
type Codec struct {
	// NoErrorWrapping is the noErrorWrapping property of the service's restXml trait: an error answer is
	// a bare Error element (S3) instead of an ErrorResponse element holding it (Route 53). It also says
	// that an Error element in the body of a success status is an error.
	NoErrorWrapping bool
}

// ContentType implements httpbind.Codec.
func (Codec) ContentType() string { return ContentType }

// Accept implements httpbind.Codec.
func (Codec) Accept() string { return Accept }

// Marshal implements httpbind.Codec. s is a structure or union with a root element name (XMLName); v is a
// map[string]any. A key the shape does not know, a value of the wrong type, a union without exactly one
// member and text XML cannot carry are errors that name the member, never the value.
func (Codec) Marshal(s *httpbind.Shape, v any) ([]byte, error) {
	if s == nil || s.Kind != httpbind.KindStruct || s.XMLName == "" {
		return nil, httpbind.ErrShape
	}
	e := &encoder{budget: maxNodes}
	if err := e.element(s.XMLName, s.Namespace, s, v, "body", 0); err != nil {
		return nil, err
	}
	return e.buf.Bytes(), nil
}

type encoder struct {
	buf    bytes.Buffer
	budget int
}

func bad(member string) error {
	return fmt.Errorf("%w: %s has the wrong type or form", httpbind.ErrValue, member)
}

// validXMLChar reports a character of XML 1.0.
func validXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}

// escape writes text as XML character data or attribute value. It refuses what XML 1.0 cannot carry.
func (e *encoder) escape(text, member string) error {
	if !utf8.ValidString(text) {
		return bad(member)
	}
	for _, r := range text {
		if !validXMLChar(r) {
			return fmt.Errorf("%w: %s has a character that XML 1.0 cannot carry", httpbind.ErrValue, member)
		}
	}
	return xml.EscapeText(&e.buf, []byte(text))
}

func (e *encoder) open(name string, ns httpbind.Namespace, attrs []attrValue, member string) error {
	if !xmlName(name) {
		return httpbind.ErrShape
	}
	e.buf.WriteByte('<')
	e.buf.WriteString(name)
	if ns != (httpbind.Namespace{}) {
		e.buf.WriteString(" xmlns")
		if ns.Prefix != "" {
			e.buf.WriteString(":" + ns.Prefix)
		}
		e.buf.WriteString(`="`)
		if err := e.escape(ns.URI, member); err != nil {
			return err
		}
		e.buf.WriteByte('"')
	}
	for _, a := range attrs {
		e.buf.WriteString(" " + a.name + `="`)
		if err := e.escape(a.value, member); err != nil {
			return err
		}
		e.buf.WriteByte('"')
	}
	e.buf.WriteByte('>')
	return nil
}

func (e *encoder) close(name string) { e.buf.WriteString("</" + name + ">") }

type attrValue struct{ name, value string }

func xmlName(n string) bool {
	return httpbind.ValidXMLName(n)
}

func (e *encoder) element(name string, ns httpbind.Namespace, s *httpbind.Shape, v any, member string, depth int) error {
	if depth > maxXMLDepth {
		return httpbind.ErrShape
	}
	if e.budget--; e.budget < 0 {
		return fmt.Errorf("%w: %s is too large", httpbind.ErrValue, member)
	}
	switch s.Kind {
	case httpbind.KindStruct:
		m, ok := v.(map[string]any)
		if !ok {
			return bad(member)
		}
		return e.structure(name, ns, s, m, member, depth)
	case httpbind.KindList:
		items, ok := v.([]any)
		if !ok {
			return bad(member)
		}
		if err := e.open(name, ns, nil, member); err != nil {
			return err
		}
		item := orName(s.ElemName, "member")
		for _, it := range items {
			if it == nil {
				return bad(member)
			}
			if err := e.element(item, httpbind.Namespace{}, s.Elem, it, member, depth+1); err != nil {
				return err
			}
		}
		e.close(name)
		return nil
	case httpbind.KindMap:
		m, ok := v.(map[string]any)
		if !ok {
			return bad(member)
		}
		if err := e.open(name, ns, nil, member); err != nil {
			return err
		}
		if err := e.entries(orName(s.KeyName, "key"), orName(s.ValueName, "value"), "entry", s, m, member, depth); err != nil {
			return err
		}
		e.close(name)
		return nil
	case httpbind.KindDocument:
		return httpbind.ErrShape
	}
	text, ok := httpbind.Text(s, v, httpbind.FormatDateTime)
	if !ok {
		return bad(member)
	}
	if err := e.open(name, ns, nil, member); err != nil {
		return err
	}
	if err := e.escape(text, member); err != nil {
		return err
	}
	e.close(name)
	return nil
}

func orName(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

// entries writes the entries of a map in key order, each as an element entryName with a key and a value.
func (e *encoder) entries(keyName, valueName, entryName string, s *httpbind.Shape, m map[string]any, member string, depth int) error {
	if len(m) > httpbind.DefaultMaxItems {
		return fmt.Errorf("%w: %s has too many entries", httpbind.ErrValue, member)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keyShape := &httpbind.Shape{Kind: httpbind.KindString}
	for _, k := range keys {
		if m[k] == nil {
			return bad(member)
		}
		if err := e.open(entryName, httpbind.Namespace{}, nil, member); err != nil {
			return err
		}
		if err := e.element(keyName, httpbind.Namespace{}, keyShape, k, member, depth+1); err != nil {
			return err
		}
		if err := e.element(valueName, httpbind.Namespace{}, s.Elem, m[k], member, depth+1); err != nil {
			return err
		}
		e.close(entryName)
	}
	return nil
}

func (e *encoder) structure(name string, ns httpbind.Namespace, s *httpbind.Shape, m map[string]any, member string, depth int) error {
	known := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		known[f.Key] = true
	}
	for k := range m {
		if !known[k] {
			return fmt.Errorf("%w: unknown member %q in %s", httpbind.ErrValue, k, member)
		}
	}
	set := 0
	for _, f := range s.Fields {
		if v, ok := m[f.Key]; ok && v != nil {
			set++
		} else if f.Required {
			return fmt.Errorf("%w: %s is required in %s", httpbind.ErrValue, f.Key, member)
		}
	}
	if s.Union && set != 1 {
		return fmt.Errorf("%w: %s must set exactly one member", httpbind.ErrValue, member)
	}
	var attrs []attrValue
	for _, f := range s.Fields {
		v, ok := m[f.Key]
		if !f.Attribute || !ok || v == nil {
			continue
		}
		text, ok := httpbind.Text(f.Shape, v, httpbind.FormatDateTime)
		if !ok || !xmlName(f.Name) {
			return bad(f.Key)
		}
		attrs = append(attrs, attrValue{f.Name, text})
	}
	if err := e.open(name, ns, attrs, member); err != nil {
		return err
	}
	for _, f := range s.Fields {
		v, ok := m[f.Key]
		if !ok || v == nil || f.Attribute {
			continue
		}
		if f.Flattened {
			if err := e.flattened(f, v, depth); err != nil {
				return err
			}
			continue
		}
		if err := e.element(f.Name, f.Namespace, f.Shape, v, f.Key, depth+1); err != nil {
			return err
		}
	}
	e.close(name)
	return nil
}

// flattened writes a list as repeated elements named like the member, a map as repeated entry elements.
func (e *encoder) flattened(f httpbind.Field, v any, depth int) error {
	if f.Shape.Kind == httpbind.KindMap {
		m, ok := v.(map[string]any)
		if !ok {
			return bad(f.Key)
		}
		return e.entries(orName(f.Shape.KeyName, "key"), orName(f.Shape.ValueName, "value"), f.Name, f.Shape, m, f.Key, depth)
	}
	items, ok := v.([]any)
	if !ok {
		return bad(f.Key)
	}
	for _, it := range items {
		if it == nil {
			return bad(f.Key)
		}
		if err := e.element(f.Name, f.Namespace, f.Shape.Elem, it, f.Key, depth+1); err != nil {
			return err
		}
	}
	return nil
}
