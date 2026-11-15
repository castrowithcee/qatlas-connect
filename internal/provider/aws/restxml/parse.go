package restxml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
)

var errInvalid = errors.New("restxml: the body is not valid XML of the expected form")

// node is one element. Text is the character data directly inside it.
type node struct {
	name  string
	attrs []attr
	text  strings.Builder
	kids  []*node
}

type attr struct {
	local    string
	prefixed bool
	value    string
}

// charRef finds the numeric character references; badRefs replaces those to characters XML 1.0 does not
// have with U+FFFD before the decoder, which would otherwise refuse the whole document.
var charRef = regexp.MustCompile(`&#(?:[xX]([0-9a-fA-F]{1,8})|([0-9]{1,10}));`)

func sanitizeRefs(data []byte) []byte {
	if !bytes.Contains(data, []byte("&#")) {
		return data
	}
	return charRef.ReplaceAllFunc(data, func(ref []byte) []byte {
		m := charRef.FindSubmatch(ref)
		var n uint64
		var err error
		if len(m[1]) > 0 {
			n, err = strconv.ParseUint(string(m[1]), 16, 32)
		} else {
			n, err = strconv.ParseUint(string(m[2]), 10, 32)
		}
		if err == nil && validXMLChar(rune(n)) {
			return ref
		}
		return []byte("�")
	})
}

// parse reads exactly one element tree, bounded in nodes and depth. A document type declaration is refused,
// so no entity is ever defined or expanded, and only the UTF-8 encoding is read.
func parse(data []byte) (*node, error) {
	d := xml.NewDecoder(bytes.NewReader(sanitizeRefs(data)))
	d.Strict = true
	var stack []*node
	var root *node
	budget := maxNodes
	for {
		tok, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errInvalid
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if budget--; budget < 0 || len(stack) >= maxXMLDepth || (root != nil && len(stack) == 0) {
				return nil, errInvalid
			}
			n := &node{name: t.Name.Local}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue
				}
				n.attrs = append(n.attrs, attr{local: a.Name.Local, prefixed: a.Name.Space != "", value: a.Value})
			}
			if len(stack) == 0 {
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.kids = append(parent.kids, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].name != t.Name.Local {
				return nil, errInvalid
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			} else if len(bytes.TrimSpace(t)) > 0 {
				return nil, errInvalid
			}
		case xml.ProcInst:
			if t.Target != "xml" || root != nil {
				return nil, errInvalid
			}
		case xml.Directive:
			return nil, errInvalid
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, errInvalid
	}
	return root, nil
}

// Unmarshal implements httpbind.Codec. A structure comes back as map[string]any under the Field.Key names;
// members the shape does not know are ignored, so a service may add members. A list or map above its cap, a
// member given twice, a union with two known members and a value of the wrong form are errors.
func (Codec) Unmarshal(s *httpbind.Shape, data []byte) (any, bool, error) {
	root, err := parse(data)
	if err != nil {
		return nil, false, err
	}
	var dec decoder
	v, err := dec.value(s, root, 0)
	return v, dec.cut, err
}

type decoder struct{ cut bool }

func (n *node) child(name string) (*node, error) {
	var found *node
	for _, k := range n.kids {
		if k.name == name {
			if found != nil {
				return nil, errInvalid
			}
			found = k
		}
	}
	return found, nil
}

func (n *node) children(name string) []*node {
	var out []*node
	for _, k := range n.kids {
		if k.name == name {
			out = append(out, k)
		}
	}
	return out
}

func (n *node) attribute(wire string) (string, bool) {
	prefix, local, hasPrefix := strings.Cut(wire, ":")
	if !hasPrefix {
		local = wire
	}
	_ = prefix
	for _, a := range n.attrs {
		if a.local == local && a.prefixed == hasPrefix {
			return a.value, true
		}
	}
	return "", false
}

func (dec *decoder) value(s *httpbind.Shape, n *node, depth int) (any, error) {
	if depth > maxXMLDepth {
		return nil, errInvalid
	}
	switch s.Kind {
	case httpbind.KindStruct:
		return dec.structure(s, n, depth)
	case httpbind.KindList:
		return dec.list(s, n.children(orName(s.ElemName, "member")), n.kids, depth)
	case httpbind.KindMap:
		return dec.mapEntries(s, n.children("entry"), depth)
	case httpbind.KindDocument:
		return nil, errInvalid
	}
	return dec.scalar(s, n)
}

func (dec *decoder) list(s *httpbind.Shape, items []*node, _ []*node, depth int) (any, error) {
	if len(items) > httpbind.OrInt(s.MaxItems, httpbind.DefaultMaxItems) {
		return nil, errInvalid
	}
	out := make([]any, 0, len(items))
	for _, it := range items {
		v, err := dec.value(s.Elem, it, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (dec *decoder) mapEntries(s *httpbind.Shape, entries []*node, depth int) (any, error) {
	if len(entries) > httpbind.OrInt(s.MaxItems, httpbind.DefaultMaxItems) {
		return nil, errInvalid
	}
	out := make(map[string]any, len(entries))
	keyShape := &httpbind.Shape{Kind: httpbind.KindString, MaxLen: s.MaxLen}
	for _, entry := range entries {
		kn, err := entry.child(orName(s.KeyName, "key"))
		if err != nil || kn == nil {
			return nil, errInvalid
		}
		vn, err := entry.child(orName(s.ValueName, "value"))
		if err != nil || vn == nil {
			return nil, errInvalid
		}
		kv, err := dec.scalar(keyShape, kn)
		if err != nil {
			return nil, err
		}
		key := kv.(string)
		if dec.cut {
			// A key cut at its cap would no longer be the key.
			return nil, errInvalid
		}
		if _, dup := out[key]; dup {
			return nil, errInvalid
		}
		v, err := dec.value(s.Elem, vn, depth+1)
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, nil
}

func (dec *decoder) structure(s *httpbind.Shape, n *node, depth int) (any, error) {
	out := map[string]any{}
	for _, f := range s.Fields {
		var v any
		var err error
		switch {
		case f.Unwrapped:
			v, err = dec.scalar(f.Shape, n)
		case f.Attribute:
			raw, ok := n.attribute(f.Name)
			if !ok {
				continue
			}
			v, err = dec.text(f.Shape, raw)
		case f.Flattened && f.Shape.Kind == httpbind.KindMap:
			entries := n.children(f.Name)
			if len(entries) == 0 {
				continue
			}
			v, err = dec.mapEntries(f.Shape, entries, depth+1)
		case f.Flattened:
			items := n.children(f.Name)
			if len(items) == 0 {
				continue
			}
			v, err = dec.list(f.Shape, items, nil, depth+1)
		default:
			var kid *node
			if kid, err = n.child(f.Name); err != nil || kid == nil {
				if err != nil {
					return nil, err
				}
				continue
			}
			v, err = dec.value(f.Shape, kid, depth+1)
		}
		if err != nil {
			return nil, err
		}
		if v != nil {
			out[f.Key] = v
		}
	}
	if s.Union && len(out) > 1 {
		return nil, errInvalid
	}
	return out, nil
}

func (dec *decoder) scalar(s *httpbind.Shape, n *node) (any, error) {
	if len(n.kids) > 0 {
		return nil, errInvalid
	}
	return dec.text(s, n.text.String())
}

func (dec *decoder) text(s *httpbind.Shape, text string) (any, error) {
	// Only a string keeps its white space; every other scalar is read without the indentation around it.
	if s.Kind != httpbind.KindString {
		text = strings.TrimSpace(text)
	}
	v, cut, err := httpbind.ParseText(s, text, httpbind.FormatDateTime)
	if err != nil {
		return nil, errInvalid
	}
	dec.cut = dec.cut || cut
	return v, nil
}
