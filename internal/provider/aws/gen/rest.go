package gen

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
)

// RestBinding is one operation of a REST service model, bound to the httpbind runtime.
type RestBinding struct {
	Operation *httpbind.Operation
	// Pager is set when the operation is paginated by one token. Its members are bound under the key
	// "~"+name in Operation, so that they never collide with an argument.
	Pager *httpbind.Pager
	// Multi is set instead of Pager when the list continues at a position of several members.
	Multi *httpbind.MultiPager
	// UnboundedPageSize names the page size member of the paginated trait when the model gives it no range
	// maximum; the Pager then carries no SizeParam.
	UnboundedPageSize string
	// Code is the success status of the http trait. The runtime accepts every 2xx status; the code is kept
	// for tests and for the generated tool's documentation.
	Code int
	// Unsupported lists what the model asks for that the runtime does not carry yet (streaming, checksums,
	// host prefixes, event streams, request headers the protocol owns), one text per place. A restXml
	// operation is bound anyway, so that the whole model is covered; no tool may be rendered from a binding
	// that lists anything here. restJson1 refuses such an operation instead.
	Unsupported []string
}

// Traits that change what goes on the wire in a way httpbind does not model. A model that uses one of them
// on an operation or on a member of its input or output stops the binding instead of being sent wrongly.
var unsupportedTraits = []string{
	"smithy.api#streaming", "smithy.api#requiresLength", "smithy.api#mediaType", "smithy.api#httpChecksumRequired",
	"aws.protocols#httpChecksum", "smithy.api#endpoint", "smithy.api#hostLabel", "smithy.api#requestCompression",
	"smithy.api#eventPayload", "smithy.api#eventHeader",
}

// The binding traits of a top-level member, mapped to their place.
var placeTraits = []struct {
	trait string
	loc   httpbind.Loc
}{
	{"smithy.api#httpLabel", httpbind.LocLabel}, {"smithy.api#httpQuery", httpbind.LocQuery},
	{"smithy.api#httpQueryParams", httpbind.LocQueryParams}, {"smithy.api#httpHeader", httpbind.LocHeader},
	{"smithy.api#httpPrefixHeaders", httpbind.LocPrefixHeaders}, {"smithy.api#httpPayload", httpbind.LocPayload},
	{"smithy.api#httpResponseCode", httpbind.LocResponseCode},
}

// checksumHeader matches the request headers that carry the checksum of the body in one algorithm.
var checksumHeader = regexp.MustCompile(`^x-amz-checksum-(crc32|crc32c|crc64nvme|sha1|sha256|sha512|md5|xxhash64|xxhash3|xxhash128)$`)

var timestampFormats = map[string]httpbind.Format{
	"epoch-seconds": httpbind.FormatEpochSeconds, "date-time": httpbind.FormatDateTime, "http-date": httpbind.FormatHTTPDate,
}

// BindRest is BindRestCurated without a table: the paging comes from the paginated trait only.
func (m *Model) BindRest(name string) (*RestBinding, error) { return m.BindRestCurated(nil, name) }

// BindRestCurated reads the http trait, the member traits and the paginated trait of an operation of a
// restJson1 or restXml model and returns the description the runtime encodes and decodes with. The table, when
// given, adds what the model does not say: the paging of operations without a paginated trait and the prefixes
// of identifiers. It knows no operation names. What the model does not bind unambiguously is an error that
// names the operation and the member; it is never guessed. What the runtime does not carry is an error for
// restJson1 and is listed in RestBinding.Unsupported for restXml.
func (m *Model) BindRestCurated(t *Table, name string) (*RestBinding, error) {
	if m.Protocol != ProtocolRestJSON && m.Protocol != ProtocolRestXML {
		return nil, fmt.Errorf("operation %s: the service speaks neither restJson1 nor restXml", name)
	}
	b := &restBinder{m: m, op: name, xml: m.Protocol == ProtocolRestXML, table: t}
	shape, err := m.operation(name)
	if err != nil {
		return nil, b.failf("%v", err)
	}
	traits := shape.obj("traits")
	// The checksum of a restXml request is the layer's business, see applyChecksum.
	carried := []string{}
	if b.xml {
		carried = append(carried, "aws.protocols#httpChecksum")
	}
	if err := b.refuseTraits(traits, "the operation", carried...); err != nil {
		return nil, err
	}
	h := traits.obj("smithy.api#http")
	if h == nil || h.str("method") == "" || h.str("uri") == "" {
		return nil, b.failf("the operation has no http trait with a method and a uri")
	}
	code := 200
	if n, ok := number(h, "code"); ok {
		code = int(n)
	}
	if code < 200 || code > 299 {
		return nil, b.failf("the success status %d is not a 2xx status", code)
	}
	plan, err := b.paging(traits)
	if err != nil {
		return nil, err
	}
	b.plan = plan
	b.unwrapped = traits.has("aws.customizations#s3UnwrappedXmlOutput")
	if sum := traits.obj("aws.protocols#httpChecksum"); b.xml && sum != nil {
		for _, k := range []string{"requestAlgorithmMember", "requestValidationModeMember"} {
			if m := sum.str(k); m != "" {
				b.checksumMembers = append(b.checksumMembers, m)
			}
		}
	}
	in, err := b.top(shape.obj("input").str("target"), true)
	if err != nil {
		return nil, err
	}
	out, err := b.top(shape.obj("output").str("target"), false)
	if err != nil {
		return nil, err
	}
	o := &httpbind.Operation{Method: h.str("method"), URI: h.str("uri"), Input: in, Output: out}
	if b.xml {
		sum := traits.obj("aws.protocols#httpChecksum")
		o.ChecksumRequired = sum.get("requestChecksumRequired") == true
		o.ValidateResponseChecksum = sum.str("requestValidationModeMember") != ""
	}
	if err := o.Validate(); err != nil {
		return nil, b.failf("%v", err)
	}
	res := &RestBinding{Operation: o, Code: code, Unsupported: b.unsupported}
	if plan != nil {
		if res.Pager, res.Multi, err = b.pager(plan, in, out); err != nil {
			return nil, err
		}
		if size := plan.size; size != "" && ((res.Pager != nil && res.Pager.SizeParam == "") || (res.Multi != nil && res.Multi.SizeParam == "")) {
			res.UnboundedPageSize = size
		}
	}
	return res, nil
}

type restBinder struct {
	m         *Model
	op        string
	xml       bool
	table     *Table
	plan      *restPlan
	unwrapped bool
	// checksumMembers are the members of the input that carry the checksum of the request or ask for the one
	// of the answer, named by the httpChecksum trait. The layer sets what they stand for.
	checksumMembers []string
	// unsupported collects what a restXml binding does not carry, see RestBinding.Unsupported.
	unsupported []string
	stack       []string
}

func (b *restBinder) failf(format string, args ...any) error {
	return fmt.Errorf("%s binding of operation %s: %s", b.m.Protocol, b.op, fmt.Sprintf(format, args...))
}

// refuseTraits stops the binding at a trait the runtime does not carry; for restXml it records the trait and
// goes on, see RestBinding.Unsupported.
func (b *restBinder) refuseTraits(traits *object, where string, carried ...string) error {
	for _, t := range unsupportedTraits {
		if traits.has(t) && !slices.Contains(carried, t) {
			if b.xml {
				b.note(where + " uses " + t)
				continue
			}
			return b.failf("%s uses %s, which the runtime does not carry", where, t)
		}
	}
	return nil
}

func (b *restBinder) note(text string) {
	if !slices.Contains(b.unsupported, text) {
		b.unsupported = append(b.unsupported, text)
	}
}

// restPlan is what paging takes out of the public arguments and fields of a REST operation.
type restPlan struct {
	// in and out are the request and output members of the position, parallel; required says which an answer
	// that reports more entries must carry. One entry is a plain token, several travel in one cursor.
	in, out  []string
	required []bool
	// size is the page size member, empty for none.
	size string
	// truncated is the boolean output member that decides the end of the list; empty for a token list whose
	// end is a missing token.
	truncated string
	// explicit says the table named truncated, so the output must have it.
	explicit bool
}

func (p *restPlan) in_(name string) bool  { return slices.Contains(p.in, name) }
func (p *restPlan) out_(name string) bool { return slices.Contains(p.out, name) }

// paging reads the pager of the operation: the table's PagerSpec, or the paginated trait of the operation over
// the one of the service, member by member. Both at once is an error: which one the tool follows would be
// a guess.
func (b *restBinder) paging(traits *object) (*restPlan, error) {
	var spec *PagerSpec
	if b.table != nil {
		for i := range b.table.Pagers {
			if b.table.Pagers[i].Operation == b.op {
				spec = &b.table.Pagers[i]
			}
		}
	}
	trait := traits.obj("smithy.api#paginated")
	if spec != nil {
		if trait != nil {
			return nil, b.failf("the model and the table both page the operation")
		}
		p := &restPlan{size: spec.PageSize, truncated: spec.Truncated, explicit: true}
		if p.truncated == "" {
			p.truncated = "IsTruncated"
		}
		for _, pos := range spec.Positions {
			p.in, p.out, p.required = append(p.in, pos.In), append(p.out, pos.Out), append(p.required, pos.Required)
		}
		return p, nil
	}
	if trait == nil {
		return nil, nil
	}
	base := b.m.service.obj("traits").obj("smithy.api#paginated")
	pick := func(k string) string {
		if trait.has(k) {
			return trait.str(k)
		}
		return base.str(k)
	}
	p := &restPlan{in: []string{pick("inputToken")}, out: []string{pick("outputToken")}, required: []bool{false}, size: pick("pageSize")}
	if b.xml {
		// The XML services say the end of a list with IsTruncated; a token alone is then not trusted to.
		p.truncated = "IsTruncated"
	}
	if p.in[0] == "" || p.out[0] == "" {
		return nil, b.failf("the paginated trait names no input or output token")
	}
	for _, n := range []string{p.in[0], p.out[0], p.size} {
		if strings.Contains(n, ".") {
			return nil, b.failf("paging member %s is nested", n)
		}
	}
	return p, nil
}

// pager builds the pager once the shapes are bound, and checks the paging members against them.
func (b *restBinder) pager(p *restPlan, in, out *httpbind.Shape) (*httpbind.Pager, *httpbind.MultiPager, error) {
	var size *httpbind.Field
	var sizeName string
	var maxSize int
	if p.size != "" {
		size = fieldByKey(in, "~"+p.size)
		if size == nil || size.Shape.Kind != httpbind.KindInteger || (size.Loc != httpbind.LocBody && size.Loc != httpbind.LocQuery) {
			return nil, nil, b.failf("page size %s is not an integer member of the body or the query", p.size)
		}
		// The Pager refuses a page size above MaxSize, and the model's range maximum is the only bound this
		// layer may use. Without one the member stays bound but the tool offers no page_size: no bound is
		// guessed, and the service package can set one with a curated cap.
		if max, ok := b.pageSizeMax(p.size); ok {
			sizeName, maxSize = size.Name, max
		}
	}
	var positions []httpbind.Position
	for i := range p.in {
		tokenIn := fieldByKey(in, "~"+p.in[i])
		tokenOut := fieldByKey(out, "~"+p.out[i])
		if tokenIn == nil || tokenIn.Shape.Kind != httpbind.KindString || (tokenIn.Loc != httpbind.LocBody && tokenIn.Loc != httpbind.LocQuery) {
			return nil, nil, b.failf("paging input token %s is not a string member of the body or the query", p.in[i])
		}
		if tokenOut == nil || tokenOut.Shape.Kind != httpbind.KindString {
			return nil, nil, b.failf("paging output token %s is not a string member", p.out[i])
		}
		positions = append(positions, httpbind.Position{In: tokenIn.Name, Out: tokenOut.Key, Required: p.required[i]})
	}
	truncated := ""
	if p.truncated != "" {
		f := fieldByKey(out, "~"+p.truncated)
		if f == nil || f.Shape.Kind != httpbind.KindBoolean {
			if p.explicit {
				return nil, nil, b.failf("truncation member %s is not a boolean member of the output", p.truncated)
			}
		} else {
			truncated = f.Key
		}
	}
	if len(positions) == 1 {
		return &httpbind.Pager{TokenIn: positions[0].In, TokenOut: positions[0].Out, Truncated: truncated,
			SizeParam: sizeName, MaxSize: maxSize}, nil, nil
	}
	multi := &httpbind.MultiPager{Positions: positions, Truncated: truncated, SizeParam: sizeName, MaxSize: maxSize}
	if err := multi.Check(); err != nil {
		return nil, nil, b.failf("%v", err)
	}
	return nil, multi, nil
}

func (b *restBinder) pageSizeMax(member string) (int, bool) {
	shape, _ := b.m.operation(b.op)
	ins, err := b.members(shape.obj("input").str("target"))
	if err != nil {
		return 0, false
	}
	mm, ok := find(ins, member)
	if !ok {
		return 0, false
	}
	n, ok := number(mm.traits.obj("smithy.api#range"), "max")
	return int(n), ok && n > 0
}

func fieldByKey(s *httpbind.Shape, key string) *httpbind.Field {
	if s == nil {
		return nil
	}
	for i := range s.Fields {
		if s.Fields[i].Key == key {
			return &s.Fields[i]
		}
	}
	return nil
}

// members returns the members of a structure target with their merged traits; Unit or none is empty.
func (b *restBinder) members(target string) ([]member, error) {
	if target == "" || target == "smithy.api#Unit" {
		return nil, nil
	}
	kind, s, err := b.m.resolve(target)
	if err != nil || kind != "structure" {
		return nil, b.failf("%s is not a structure", target)
	}
	ms := s.obj("members")
	var out []member
	for _, name := range ms.keys {
		mo := ms.obj(name)
		k, ts, err := b.m.resolve(mo.str("target"))
		if err != nil {
			return nil, b.failf("member %s: %v", name, err)
		}
		out = append(out, member{name: name, obj: mo, target: mo.str("target"), kind: k, traits: mergedTraits(mo, ts)})
	}
	return out, nil
}

// top binds the top level of an input or an output: each member gets its place from its binding trait, a
// member without one lives in the document body. Unit or no shape is no description at all.
func (b *restBinder) top(target string, input bool) (*httpbind.Shape, error) {
	if target == "" || target == "smithy.api#Unit" {
		return nil, nil
	}
	ms, err := b.members(target)
	if err != nil {
		return nil, err
	}
	s := &httpbind.Shape{Kind: httpbind.KindStruct}
	if b.xml {
		_, so, _ := b.m.resolve(target)
		s.XMLName = orName(so.obj("traits").str("smithy.api#xmlName"), shortName(target))
		s.Namespace = b.namespace(so.obj("traits"), b.m.Namespace)
	}
	for _, mm := range ms {
		streaming := b.xml && mm.kind == "blob" && mm.traits.has("smithy.api#httpPayload")
		var carried []string
		if streaming {
			carried = []string{"smithy.api#streaming"}
		}
		if err := b.refuseTraits(mm.traits, "member "+mm.name, carried...); err != nil {
			return nil, err
		}
		f := httpbind.Field{Key: mm.name, Name: mm.name}
		var places []string
		for _, p := range placeTraits {
			if mm.traits.has(p.trait) {
				places = append(places, p.trait)
				f.Loc = p.loc
			}
		}
		if len(places) > 1 {
			return nil, b.failf("member %s carries more than one binding trait", mm.name)
		}
		switch f.Loc {
		case httpbind.LocQuery:
			f.Name = mm.traits.str("smithy.api#httpQuery")
		case httpbind.LocHeader:
			f.Name = mm.traits.str("smithy.api#httpHeader")
		case httpbind.LocPrefixHeaders:
			f.Name = mm.traits.str("smithy.api#httpPrefixHeaders")
		case httpbind.LocBody:
			f.Name = b.bodyName(mm)
		}
		if f.Name == "" && f.Loc != httpbind.LocPrefixHeaders {
			return nil, b.failf("member %s: the binding trait names no wire name", mm.name)
		}
		if b.xml && input {
			// What the layer owns never becomes an argument: it sets these itself, or the protocol does.
			switch name := strings.ToLower(f.Name); {
			case f.Loc == httpbind.LocHeader && (name == "content-length" || name == "content-md5" || checksumHeader.MatchString(name)):
				continue
			case slices.Contains(b.checksumMembers, mm.name):
				continue
			case f.Loc == httpbind.LocHeader && (name == "x-amz-expected-bucket-owner" || name == "x-amz-source-expected-bucket-owner"):
				f.Owner = true
			case f.Loc == httpbind.LocHeader && name == "content-type":
				f.ContentType = true
			case f.Loc == httpbind.LocLabel && mm.traits.obj("smithy.rules#contextParam").str("name") == "Bucket":
				f.Bucket = true
			}
		}
		if streaming && f.Loc == httpbind.LocPayload {
			f.Stream = true
		}
		if b.xml && input && f.Loc == httpbind.LocHeader && httpbind.ReservedHeader(f.Name) && !f.ContentType {
			// The protocol or the signature owns this request header, a model cannot make a tool set it.
			b.note("member " + mm.name + " is the header " + f.Name + ", which the protocol sets itself")
			continue
		}
		if input {
			// A required member the model also gives a default stays required: omitting it would send
			// nothing where the model says a value is always present, and the runtime refuses that call.
			f.Required = mm.traits.has("smithy.api#required")
			f.IdempotencyToken = mm.traits.has("smithy.api#idempotencyToken")
		}
		if f.Shape, err = b.shape(mm.target, mm.traits, 1); err != nil {
			return nil, b.failf("member %s: %v", mm.name, err)
		}
		if b.xml && f.Loc == httpbind.LocBody {
			f.Flattened = mm.traits.has("smithy.api#xmlFlattened")
			f.Attribute = mm.traits.has("smithy.api#xmlAttribute")
			f.Namespace = b.namespace(mm.traits, "")
			if f.Attribute && f.Flattened {
				return nil, b.failf("member %s is both an attribute and flattened", mm.name)
			}
		}
		if b.xml && f.Loc == httpbind.LocPayload && f.Shape.Kind == httpbind.KindStruct {
			// The root element of a structure payload: the member's xmlName, else the target's, else its name.
			ownName := mm.obj.obj("traits").str("smithy.api#xmlName")
			f.Shape.XMLName = orName(ownName, orName(b.m.shapes.obj(mm.target).obj("traits").str("smithy.api#xmlName"), shortName(mm.target)))
			f.Shape.Namespace = b.namespace(mm.traits, b.m.Namespace)
		}
		if b.unwrapped && !input && f.Loc == httpbind.LocBody {
			f.Unwrapped = true
		}
		if b.plan != nil {
			paging := (input && (b.plan.in_(mm.name) || (b.plan.size != "" && mm.name == b.plan.size))) ||
				(!input && (b.plan.out_(mm.name) || (b.plan.truncated != "" && mm.name == b.plan.truncated)))
			if paging {
				f.Key = "~" + mm.name
				f.Required = false
			}
		}
		s.Fields = append(s.Fields, f)
	}
	return s, nil
}

// bodyName is the wire name of a member of the document: its jsonName, or for restXml its own xmlName (the
// xmlName of the target belongs to the target's root element only), else its name.
func (b *restBinder) bodyName(mm member) string {
	if b.xml {
		return orName(mm.obj.obj("traits").str("smithy.api#xmlName"), mm.name)
	}
	return orName(mm.traits.str("smithy.api#jsonName"), mm.name)
}

// namespace reads the xmlNamespace of a set of traits, or returns fallback as a namespace without prefix.
func (b *restBinder) namespace(traits *object, fallback string) httpbind.Namespace {
	if ns := traits.obj("smithy.api#xmlNamespace"); ns != nil {
		return httpbind.Namespace{URI: ns.str("uri"), Prefix: ns.str("prefix")}
	}
	if fallback != "" {
		return httpbind.Namespace{URI: fallback}
	}
	return httpbind.Namespace{}
}

func orName(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

// shape converts a model shape. Only the top level has binding traits; below it every member is a body
// member, as the Smithy HTTP binding rules say.
func (b *restBinder) shape(target string, traits *object, depth int) (*httpbind.Shape, error) {
	if depth > httpbind.MaxDepth || slices.Contains(b.stack, target) {
		return nil, fmt.Errorf("shape %s is recursive or nested too deep", target)
	}
	b.stack = append(b.stack, target)
	defer func() { b.stack = b.stack[:len(b.stack)-1] }()
	kind, s, err := b.m.resolve(target)
	if err != nil {
		return nil, err
	}
	length := traits.obj("smithy.api#length")
	switch kind {
	case "string", "enum":
		return &httpbind.Shape{Kind: httpbind.KindString, MaxLen: tighter(length, httpbind.DefaultMaxString), Prefixes: b.prefixes(target)}, nil
	case "byte", "short", "integer", "long", "intEnum":
		return &httpbind.Shape{Kind: httpbind.KindInteger}, nil
	case "float", "double":
		return &httpbind.Shape{Kind: httpbind.KindFloat}, nil
	case "boolean":
		return &httpbind.Shape{Kind: httpbind.KindBoolean}, nil
	case "timestamp":
		var f httpbind.Format
		if name := traits.str("smithy.api#timestampFormat"); name != "" {
			var ok bool
			if f, ok = timestampFormats[name]; !ok {
				return nil, fmt.Errorf("timestamp format %q is unknown", name)
			}
		}
		return &httpbind.Shape{Kind: httpbind.KindTimestamp, Format: f}, nil
	case "blob":
		return &httpbind.Shape{Kind: httpbind.KindBlob, MaxLen: tighter(length, httpbind.DefaultMaxString)}, nil
	case "document":
		if b.xml {
			return nil, fmt.Errorf("the document type has no restXml form")
		}
		return &httpbind.Shape{Kind: httpbind.KindDocument}, nil
	case "structure", "union":
		out := &httpbind.Shape{Kind: httpbind.KindStruct, Union: kind == "union"}
		ms := s.obj("members")
		for _, name := range ms.keys {
			mo := ms.obj(name)
			_, ts, err := b.m.resolve(mo.str("target"))
			if err != nil {
				return nil, fmt.Errorf("member %s: %w", name, err)
			}
			merged := mergedTraits(mo, ts)
			if err := b.refuseTraits(merged, "member "+name); err != nil {
				return nil, err
			}
			wire := name
			if b.xml {
				wire = orName(mo.obj("traits").str("smithy.api#xmlName"), name)
			} else if j := merged.str("smithy.api#jsonName"); j != "" {
				wire = j
			}
			inner, err := b.shape(mo.str("target"), merged, depth+1)
			if err != nil {
				return nil, fmt.Errorf("member %s: %w", name, err)
			}
			f := httpbind.Field{Key: name, Name: wire, Shape: inner}
			if b.xml {
				f.Flattened, f.Attribute = merged.has("smithy.api#xmlFlattened"), merged.has("smithy.api#xmlAttribute")
				f.Namespace = b.namespace(merged, "")
				if f.Attribute && f.Flattened {
					return nil, fmt.Errorf("member %s is both an attribute and flattened", name)
				}
			}
			out.Fields = append(out.Fields, f)
		}
		return out, nil
	case "list", "set":
		mo := s.obj("member")
		_, ts, err := b.m.resolve(mo.str("target"))
		if err != nil {
			return nil, err
		}
		elem, err := b.shape(mo.str("target"), mergedTraits(mo, ts), depth+1)
		if err != nil {
			return nil, err
		}
		list := &httpbind.Shape{Kind: httpbind.KindList, Elem: elem, MaxItems: tighter(length, httpbind.DefaultMaxItems)}
		if b.xml {
			if mo.obj("traits").has("smithy.api#xmlNamespace") {
				return nil, fmt.Errorf("a namespace on a list member is not carried")
			}
			list.ElemName = mo.obj("traits").str("smithy.api#xmlName")
		}
		return list, nil
	case "map":
		ko, vo := s.obj("key"), s.obj("value")
		if kk, _, err := b.m.resolve(ko.str("target")); err != nil || (kk != "string" && kk != "enum") {
			return nil, fmt.Errorf("a map key is not a string")
		}
		_, vs, err := b.m.resolve(vo.str("target"))
		if err != nil {
			return nil, err
		}
		elem, err := b.shape(vo.str("target"), mergedTraits(vo, vs), depth+1)
		if err != nil {
			return nil, err
		}
		mp := &httpbind.Shape{Kind: httpbind.KindMap, Elem: elem, MaxItems: tighter(length, httpbind.DefaultMaxItems)}
		if b.xml {
			if ko.obj("traits").has("smithy.api#xmlNamespace") || vo.obj("traits").has("smithy.api#xmlNamespace") {
				return nil, fmt.Errorf("a namespace on a map member is not carried")
			}
			mp.KeyName, mp.ValueName = ko.obj("traits").str("smithy.api#xmlName"), vo.obj("traits").str("smithy.api#xmlName")
		}
		return mp, nil
	}
	return nil, fmt.Errorf("shape type %s has no %s binding here", kind, b.m.Protocol)
}

// prefixes returns the prefixes the table allows for a string shape of the service namespace.
func (b *restBinder) prefixes(target string) []string {
	if b.table == nil || !strings.HasPrefix(target, b.m.namespace()+"#") {
		return nil
	}
	var out []string
	for _, p := range b.table.IDPrefixes {
		if p.Shape == shortName(target) {
			out = append(out, p.Prefix)
		}
	}
	return out
}

// tighter returns the model's length maximum when it is below the runtime default, and zero (the default)
// otherwise: the model may lower a cap, never raise it.
func tighter(length *object, def int) int {
	if max, ok := number(length, "max"); ok && max > 0 && max < int64(def) {
		return int(max)
	}
	return 0
}
