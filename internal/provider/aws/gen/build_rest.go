package gen

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awsquery"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/restjson"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/restxml"
)

// assembleRest builds the description the runtime sends and decodes a REST tool with. The converted
// shapes (input and output of the tool, after hidden members and enum exclusions) carry the member keys and
// caps; the binding of the model gives the method, the URI, the place of each top-level member and, for
// restXml, the XML form of the document. A member the tool does not offer is not in the description, so it
// can neither be sent nor returned, except what the layer owns: the expected-owner header, which the layer
// sets from the connection and no argument can.
func (t *tool) assembleRest() error {
	bound := t.restBind.Operation
	if len(t.restBind.Unsupported) > 0 {
		return t.failf("the runtime does not carry what the model asks for: %s", strings.Join(t.restBind.Unsupported, "; "))
	}
	op := &httpbind.Operation{Method: bound.Method, URI: bound.URI,
		ChecksumRequired: bound.ChecksumRequired, ValidateResponseChecksum: bound.ValidateResponseChecksum}
	var owners []httpbind.Field
	if bound.Input != nil {
		for _, f := range bound.Input.Fields {
			if f.Owner {
				owners = append(owners, f)
			}
		}
	}
	if len(t.input.Fields) > 0 || t.pager != nil || t.multi != nil || len(owners) > 0 {
		op.Input = &httpbind.Shape{Kind: httpbind.KindStruct}
		t.rootOf(op.Input, bound.Input)
	}
	for _, f := range t.input.Fields {
		bf := fieldByKey(bound.Input, t.inModel[f.Key])
		if bf == nil {
			return t.failf("input member %s is not in the binding", t.inModel[f.Key])
		}
		if bf.Stream {
			return t.failf("input member %s is a stream, which a tool argument does not carry yet", t.inModel[f.Key])
		}
		field, err := t.restField(f, bf)
		if err != nil {
			return err
		}
		field.Required = bf.Required
		op.Input.Fields = append(op.Input.Fields, field)
	}
	if len(owners) > 0 {
		op.Input.Fields = append(op.Input.Fields, owners...)
	}
	if t.pager != nil || t.multi != nil {
		// The paging members are bound under "~"+name; the table's page size maximum may apply to a member
		// the model leaves unbounded, which the binding then carries without a size parameter.
		for _, name := range append(slices.Clone(t.plan.ins), t.plan.sizeMember) {
			if name == "" {
				continue
			}
			bf := fieldByKey(bound.Input, "~"+name)
			if bf == nil {
				return t.failf("paging member %s is not in the binding", name)
			}
			op.Input.Fields = append(op.Input.Fields, *bf)
			switch {
			case name == t.plan.sizeMember && t.multi != nil:
				t.multi.SizeParam = bf.Name
			case name == t.plan.sizeMember:
				t.pager.SizeParam = bf.Name
			case t.pager != nil:
				t.pager.TokenIn = bf.Name
			}
		}
	}
	if len(t.output.Fields) > 0 {
		op.Output = &httpbind.Shape{Kind: httpbind.KindStruct}
		t.rootOf(op.Output, bound.Output)
	}
	for _, f := range t.output.Fields {
		bf := fieldByKey(bound.Output, t.outModel[f.Key])
		if bf == nil {
			return t.failf("output member %s is not in the binding", t.outModel[f.Key])
		}
		if bf.Stream {
			return t.failf("output member %s is a stream, which a result does not carry", t.outModel[f.Key])
		}
		field, err := t.restField(f, bf)
		if err != nil {
			return err
		}
		op.Output.Fields = append(op.Output.Fields, field)
	}
	if t.op.TargetCheck == "bucket-owner" {
		for _, name := range t.targetArgs {
			if f := fieldByKey(op.Input, name); f == nil || !f.Bucket {
				return t.failf("bucket-owner checks %s, which is not the bucket label of the operation", name)
			}
		}
	}
	if err := op.Validate(); err != nil {
		return t.failf("%v", err)
	}
	t.restOp = op
	return nil
}

// layerOwned reports an input member of a restXml tool that never becomes an argument: the expected-owner
// header, which the layer sets from the connection, and what the binder leaves out because the layer sets it
// (content length, checksums).
func (t *tool) layerOwned(name string) bool {
	if !t.xml {
		return false
	}
	f := fieldByKey(t.restBind.Operation.Input, name)
	return f == nil && fieldByKey(t.restBind.Operation.Input, "~"+name) == nil || f != nil && f.Owner
}

// rootOf gives a top-level shape the XML root of the binding.
func (t *tool) rootOf(dst, src *httpbind.Shape) {
	if t.xml && src != nil {
		dst.XMLName, dst.Namespace = src.XMLName, src.Namespace
	}
}

// restField builds a top-level field from the converted member f and the field bf of the binding: the place,
// the wire name and, for restXml, the XML form and the roles of the layer.
func (t *tool) restField(f awsquery.Field, bf *httpbind.Field) (httpbind.Field, error) {
	field := httpbind.Field{Key: f.Key, Name: bf.Name, Shape: restShape(f.Shape), Loc: bf.Loc, IdempotencyToken: f.IdempotencyToken}
	if field.Shape.Kind == httpbind.KindString && bf.Shape != nil && (bf.Loc == httpbind.LocLabel || bf.Loc == httpbind.LocQuery) {
		// What an identifier argument may carry in front of the value, such as /hostedzone/.
		field.Shape.Prefixes = bf.Shape.Prefixes
	}
	if !t.xml {
		return field, nil
	}
	if err := carryXMLForm(field.Shape, bf.Shape, bf.Key); err != nil {
		return field, t.failf("%v", err)
	}
	t.rootOf(field.Shape, bf.Shape)
	field.Flattened, field.Attribute, field.Unwrapped, field.Namespace = bf.Flattened, bf.Attribute, bf.Unwrapped, bf.Namespace
	field.Bucket, field.Owner, field.ContentType = bf.Bucket, bf.Owner, bf.ContentType
	if bf.Loc == httpbind.LocPayload && bf.Shape.Kind == httpbind.KindStruct {
		field.Shape.XMLName, field.Shape.Namespace = bf.Shape.XMLName, bf.Shape.Namespace
	}
	return field, nil
}

// carryXMLForm gives the shape its timestamp format and the members below the top level the XML form of the binding that the converted shapes do not
// carry: an attribute (such as the xsi:type of a grantee) and the namespace declared on the element of a member. The
// converted members are matched to the bound ones by their snake_case key; a member the binding lacks is a defect.
func carryXMLForm(dst, src *httpbind.Shape, path string) error {
	if dst == nil || src == nil {
		return nil
	}
	if dst.Kind == httpbind.KindTimestamp && src.Kind == httpbind.KindTimestamp {
		// The explicit format of the trait; the converted shape has none.
		dst.Format = src.Format
	}
	for i := range dst.Fields {
		df := &dst.Fields[i]
		j := slices.IndexFunc(src.Fields, func(f httpbind.Field) bool { return snake(f.Key) == df.Key })
		if j < 0 {
			return fmt.Errorf("member %s.%s is not in the binding", path, df.Key)
		}
		sf := src.Fields[j]
		if sf.Attribute || sf.Namespace != (httpbind.Namespace{}) {
			df.Attribute, df.Namespace, df.Name = sf.Attribute, sf.Namespace, sf.Name
		}
		if err := carryXMLForm(df.Shape, sf.Shape, path+"."+sf.Key); err != nil {
			return err
		}
	}
	return carryXMLForm(dst.Elem, src.Elem, path+"[]")
}

// restShape turns a converted shape into the description of the REST runtime.
func restShape(s *awsquery.Shape) *httpbind.Shape {
	out := &httpbind.Shape{MaxLen: s.MaxLen, MaxItems: s.MaxItems}
	switch s.Kind {
	case awsquery.KindString:
		out.Kind = httpbind.KindString
	case awsquery.KindInteger:
		out.Kind = httpbind.KindInteger
	case awsquery.KindFloat:
		out.Kind = httpbind.KindFloat
	case awsquery.KindBoolean:
		out.Kind = httpbind.KindBoolean
	case awsquery.KindTimestamp:
		out.Kind = httpbind.KindTimestamp
	case awsquery.KindBlob:
		out.Kind = httpbind.KindBlob
	case awsquery.KindStruct:
		out.Kind = httpbind.KindStruct
		for _, f := range s.Fields {
			out.Fields = append(out.Fields, httpbind.Field{Key: f.Key, Name: f.Name, Shape: restShape(f.Shape), Flattened: f.Flattened})
		}
	case awsquery.KindList:
		out.Kind, out.Elem, out.ElemName = httpbind.KindList, restShape(s.Elem), s.ElemName
	case awsquery.KindMap:
		out.Kind, out.Elem, out.KeyName, out.ValueName = httpbind.KindMap, restShape(s.Elem), s.KeyName, s.ValueName
	}
	return out
}

// sampleOwner is the account the generated tests bind, which a restXml request names as the expected owner.
const sampleOwner = "123456789012"

// restSamples encodes the sample arguments with the runtime and derives the answer of the generated test.
func (t *tool) restSamples(plain map[string]any) error {
	var codec httpbind.Codec = restjson.Codec{}
	if t.xml {
		codec = restxml.Codec{NoErrorWrapping: t.noWrap}
	}
	req, err := t.restOp.EncodeWith(codec, plain, httpbind.EncodeOptions{Owner: sampleOwner, Rand: bytes.NewReader(make([]byte, 16))})
	if err != nil {
		return t.failf("sample arguments do not encode: %v", err)
	}
	t.sampleMethod, t.samplePath, t.sampleQuery, t.sampleBody = req.Method, req.Path, req.RawQuery, string(req.Body)
	t.sampleOwner = req.Header.Get(ownerHeaderOf(t.restOp))
	if httpbind.VirtualHostable(req.Bucket) {
		// The client sends the bucket as the first label of the host and leaves it out of the path.
		t.sampleBucket = req.Bucket
		t.samplePath = "/" + strings.TrimPrefix(strings.TrimPrefix(req.Path, "/"+req.Bucket), "/")
	}
	t.sampleForm = map[string]string{}
	t.sampleHeader = map[string]string{}
	if t.restOp.Output == nil {
		t.sampleAnswer = "{}"
		if t.xml {
			t.sampleAnswer = ""
		}
		return nil
	}
	if t.xml {
		return t.xmlAnswer()
	}
	var body strings.Builder
	if err := t.jsonStruct(&body, t.restOp.Output); err != nil {
		return err
	}
	t.sampleAnswer = body.String()
	return t.sampleHeaders()
}

// ownerHeaderOf is the name of the expected-owner header of the operation, empty for none.
func ownerHeaderOf(o *httpbind.Operation) string {
	if o.Input != nil {
		for _, f := range o.Input.Fields {
			if f.Owner {
				return f.Name
			}
		}
	}
	return ""
}

// sampleHeaders fills the answer headers of the members bound to a header.
func (t *tool) sampleHeaders() error {
	for _, f := range t.restOp.Output.Fields {
		if f.Loc != httpbind.LocHeader {
			continue
		}
		if f.Shape.Kind == httpbind.KindList || !textScalar(f.Shape) {
			return t.failf("output header %s has no sample", f.Key)
		}
		t.sampleHeader[f.Name] = t.headerText(f)
	}
	return nil
}

func textScalar(s *httpbind.Shape) bool {
	return s.Kind != httpbind.KindBlob && s.Kind != httpbind.KindStruct && s.Kind != httpbind.KindMap
}

// headerText is the sample value of a header member.
func (t *tool) headerText(f httpbind.Field) string {
	switch f.Shape.Kind {
	case httpbind.KindInteger:
		return "1"
	case httpbind.KindFloat:
		return "1.5"
	case httpbind.KindBoolean:
		return "true"
	case httpbind.KindTimestamp:
		switch f.Shape.Format {
		case httpbind.FormatDateTime:
			return "2026-01-02T03:04:05Z"
		case httpbind.FormatEpochSeconds:
			return "1767323045"
		}
		return "Fri, 02 Jan 2026 03:04:05 GMT"
	}
	return "x"
}

// xmlAnswer writes the answer of a restXml tool: the headers, and the document that holds the body members,
// the payload structure, or the one unwrapped member.
func (t *tool) xmlAnswer() error {
	out := t.restOp.Output
	if err := t.sampleHeaders(); err != nil {
		return err
	}
	byKey := map[string]awsquery.Field{}
	for _, f := range t.output.Fields {
		byKey[f.Key] = f
	}
	var body strings.Builder
	for _, f := range out.Fields {
		qf := byKey[f.Key]
		if t.pagedOut(f.Key) {
			continue
		}
		switch f.Loc {
		case httpbind.LocBody:
			if f.Unwrapped {
				t.sampleAnswer = "<" + out.XMLName + t.xmlns(out.Namespace) + ">" + t.scalarSample(qf) + "</" + out.XMLName + ">"
				return nil
			}
			qf.Flattened = f.Flattened
			if err := t.answerValue(&body, awsquery.Field{Name: f.Name, Shape: qf.Shape, Flattened: f.Flattened, Key: qf.Key}); err != nil {
				return err
			}
		case httpbind.LocPayload:
			if f.Shape.Kind != httpbind.KindStruct {
				return t.failf("payload %s has no sample", f.Key)
			}
			var inner strings.Builder
			if err := t.answer(&inner, qf.Shape); err != nil {
				return err
			}
			t.sampleAnswer = "<" + f.Shape.XMLName + t.xmlns(f.Shape.Namespace) + ">" + inner.String() + "</" + f.Shape.XMLName + ">"
			return nil
		}
	}
	if body.Len() == 0 {
		return nil
	}
	if out.XMLName == "" {
		return t.failf("the answer has body members but no root element")
	}
	t.sampleAnswer = "<" + out.XMLName + t.xmlns(out.Namespace) + ">" + body.String() + "</" + out.XMLName + ">"
	return nil
}

// xmlns is the namespace declaration of a root element, empty for none.
func (t *tool) xmlns(ns httpbind.Namespace) string {
	switch {
	case ns.URI == "":
		return ""
	case ns.Prefix != "":
		return " xmlns:" + ns.Prefix + `="` + ns.URI + `"`
	}
	return ` xmlns="` + ns.URI + `"`
}

// jsonStruct writes the body members of a struct shape as the JSON object of an answer. Paging ends on the first page.
func (t *tool) jsonStruct(b *strings.Builder, s *httpbind.Shape) error {
	b.WriteByte('{')
	first := true
	for _, f := range s.Fields {
		if t.pagedOut(f.Key) {
			continue
		}
		if f.Loc == httpbind.LocPayload {
			return t.failf("output member %s is a payload, which a restJson1 answer sample does not carry yet", f.Key)
		}
		if f.Loc != httpbind.LocBody {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(strconv.Quote(f.Name) + ":")
		if err := t.jsonValue(b, f.Key, f.Shape); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

func (t *tool) jsonValue(b *strings.Builder, key string, s *httpbind.Shape) error {
	switch s.Kind {
	case httpbind.KindStruct:
		return t.jsonStruct(b, s)
	case httpbind.KindList:
		b.WriteByte('[')
		if err := t.jsonValue(b, key, s.Elem); err != nil {
			return err
		}
		b.WriteByte(']')
	case httpbind.KindMap:
		b.WriteString(`{"k":`)
		if err := t.jsonValue(b, key, s.Elem); err != nil {
			return err
		}
		b.WriteByte('}')
	case httpbind.KindInteger:
		b.WriteString("1")
	case httpbind.KindFloat:
		b.WriteString("1.5")
	case httpbind.KindBoolean:
		b.WriteString("true")
	case httpbind.KindTimestamp:
		// Epoch seconds, the body format of restJson1; the tool returns it as 2026-01-02T03:04:05Z.
		b.WriteString("1767323045")
	case httpbind.KindBlob:
		b.WriteString(`"AA=="`)
	case httpbind.KindString:
		text := "x"
		for _, r := range t.rules {
			if r.key == key && r.form != "" {
				text = formSamples[r.form]
			}
		}
		b.WriteString(strconv.Quote(text))
	default:
		return fmt.Errorf("%s: no sample for this shape", key)
	}
	return nil
}
