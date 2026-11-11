package gen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awsquery"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/restjson"
)

// assembleRest builds the description the runtime sends and decodes a restJson1 tool with. The converted
// shapes (input and output of the tool, after hidden members and enum exclusions) carry the member keys and
// caps; the binding of the model gives the method, the URI and the place of each top-level member. A member
// the tool does not offer is not in the description, so it can neither be sent nor returned.
func (t *tool) assembleRest() error {
	bound := t.restBind.Operation
	op := &httpbind.Operation{Method: bound.Method, URI: bound.URI}
	if len(t.input.Fields) > 0 || t.pager != nil {
		op.Input = &httpbind.Shape{Kind: httpbind.KindStruct}
	}
	for _, f := range t.input.Fields {
		bf := fieldByKey(bound.Input, t.inModel[f.Key])
		if bf == nil {
			return t.failf("input member %s is not in the binding", t.inModel[f.Key])
		}
		op.Input.Fields = append(op.Input.Fields, httpbind.Field{Key: f.Key, Name: bf.Name, Shape: restShape(f.Shape), Loc: bf.Loc, Required: bf.Required})
	}
	if t.pager != nil {
		// The paging members are bound under "~"+name; the table's page size maximum may apply to a member
		// the model leaves unbounded, which the binding then carries without a size parameter.
		for _, name := range []string{t.plan.tokenIn, t.plan.sizeMember} {
			if name == "" {
				continue
			}
			bf := fieldByKey(bound.Input, "~"+name)
			if bf == nil {
				return t.failf("paging member %s is not in the binding", name)
			}
			op.Input.Fields = append(op.Input.Fields, *bf)
			if name == t.plan.tokenIn {
				t.pager.TokenIn = bf.Name
			} else {
				t.pager.SizeParam = bf.Name
			}
		}
	}
	if len(t.output.Fields) > 0 {
		op.Output = &httpbind.Shape{Kind: httpbind.KindStruct}
	}
	for _, f := range t.output.Fields {
		bf := fieldByKey(bound.Output, t.outModel[f.Key])
		if bf == nil {
			return t.failf("output member %s is not in the binding", t.outModel[f.Key])
		}
		if bf.Loc != httpbind.LocBody {
			return t.failf("output member %s is bound outside the body, which the generated tests do not cover yet", t.outModel[f.Key])
		}
		op.Output.Fields = append(op.Output.Fields, httpbind.Field{Key: f.Key, Name: bf.Name, Shape: restShape(f.Shape)})
	}
	if err := op.Validate(); err != nil {
		return t.failf("%v", err)
	}
	t.restOp = op
	return nil
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
			out.Fields = append(out.Fields, httpbind.Field{Key: f.Key, Name: f.Name, Shape: restShape(f.Shape)})
		}
	case awsquery.KindList:
		out.Kind, out.Elem = httpbind.KindList, restShape(s.Elem)
	case awsquery.KindMap:
		out.Kind, out.Elem = httpbind.KindMap, restShape(s.Elem)
	}
	return out
}

// restSamples encodes the sample arguments with the runtime and derives the answer of the generated test.
func (t *tool) restSamples(plain map[string]any) error {
	req, err := t.restOp.Encode(restjson.Codec{}, plain, nil)
	if err != nil {
		return t.failf("sample arguments do not encode: %v", err)
	}
	t.sampleMethod, t.samplePath, t.sampleQuery, t.sampleBody = req.Method, req.Path, req.RawQuery, string(req.Body)
	t.sampleForm = map[string]string{}
	var body strings.Builder
	if t.restOp.Output != nil {
		if err := t.jsonStruct(&body, t.restOp.Output); err != nil {
			return err
		}
	} else {
		body.WriteString("{}")
	}
	t.sampleAnswer = body.String()
	return nil
}

// jsonStruct writes the members of a struct shape as the JSON object of an answer. Paging ends on the first page.
func (t *tool) jsonStruct(b *strings.Builder, s *httpbind.Shape) error {
	b.WriteByte('{')
	first := true
	for _, f := range s.Fields {
		if t.pager != nil && f.Key == t.pager.TokenOut {
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
