package gen

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awsquery"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/idempotency"
)

// Names the generator adds to tools. A model member that would take one of them stops the generation.
const (
	argRegion      = "region"
	argClientToken = "client_token"
	fieldNext      = "next_cursor"
	fieldHasMore   = "has_more"
	fieldTruncated = "truncated"
	// Names of the result members of a CSV tool.
	fieldRows    = "rows"
	fieldDropped = "dropped_columns"
	fieldTotal   = "total_rows"
	// fieldDNSRecords is the result member of the dns-records hook.
	fieldDNSRecords = "dns_records"
	csvMemberKey    = "~csv"
	csvMaxDropped   = 32
)

var reservedNames = []string{argRegion, awsquery.ArgPageSize, awsquery.ArgCursor, argClientToken, fieldNext, fieldHasMore, fieldTruncated,
	fieldRows, fieldDropped, fieldTotal, fieldDNSRecords}

// regionMarker stands for the region schema in a rendered schema; the renderer replaces it with the
// provider's regionSchema constant, so the schema has one source.
const regionMarker = "\x00region\x00"

// Default caps of what an answer may hold when the model sets none.
const (
	defaultMaxString = awsquery.DefaultMaxString
	defaultMaxItems  = awsquery.DefaultMaxItems
)

// accountCheckMembers are the members the account check answers with (STS GetCallerIdentity).
var accountCheckMembers = []string{"UserId", "Account", "Arn"}

// credentialName matches the name of a text output member that holds a credential. Such a member must be
// hidden even when the model does not mark it sensitive.
var credentialName = regexp.MustCompile(`(?i)(secret|secretaccesskey|secretkey|password|privatekey|sessiontoken)$`)

// formSamples are values that pass a form check, for generated tests.
var formSamples = map[string]string{"account-id": "123456789012"}

// tool is one operation of a table, bound to its model.
type tool struct {
	table   *Table
	op      Operation
	id      string
	opName  string
	action  string
	version string
	global  bool

	input, output *awsquery.Shape
	pager         *awsquery.Pager
	multi         *httpbind.MultiPager
	hasToken      bool

	// rest is true for a restJson1 or restXml tool; xml says it is restXml, and noWrap is the service's
	// noErrorWrapping. restBind is the binding of its operation; restOp is the description
	// the runtime sends and decodes with, assembled from the converted shapes. inModel and outModel map the
	// key of a top-level member of the converted input and output to its member key in the binding.
	rest, xml, noWrap bool
	restBind          *RestBinding
	restOp            *httpbind.Operation
	plan              *pagingPlan
	inModel, outModel map[string]string
	// nestedIn are the dotted HiddenInputs paths, true once the converter left the member out; enumHit the
	// ExcludeEnum members the converter narrowed.
	nestedIn map[string]bool
	enumHit  map[string]bool
	// released are the Release members of the table, true once a sensitive member of that path was kept.
	released map[string]bool
	// kept are the KeepDeprecated members of the table, true once the converter kept one.
	kept map[string]bool
	// limited are the Limits of the table, true once a member of that path took its cap.
	limited map[string]bool

	inputSchema, outputSchema string
	args                      []descArg
	fields                    []descField
	rules                     []outputRule
	targetArgs, policyArgs    []string
	selfRole                  string
	csv                       *CSVReport
	// nested are the dotted HiddenOutputs paths, true once the converter left the member out.
	nested map[string]bool

	// Generated test of a restJson1 tool: the request the sample arguments encode to.
	sampleMethod, samplePath, sampleQuery, sampleBody string
	// sampleOwner is the expected-owner header the sample request carries, sampleBucket the bucket that is the
	// first label of its host, sampleHeader the headers of the sample answer.
	sampleOwner, sampleBucket string
	sampleHeader              map[string]string

	// Generated test: the arguments, the form parameters they encode to, the answer, and the result keys.
	sampleArgs   string
	sampleForm   map[string]string
	sampleAnswer string
	resultKeys   []string
}

type descArg struct {
	name, description string
	required          bool
	region            bool
}

type descField struct {
	name, description string
	region            bool
}

// outputRule is a check the runtime makes on a top-level result member.
type outputRule struct {
	key, form string
	required  bool
}

// failf names the table, the operation, and the reason.
func (t *tool) failf(format string, args ...any) error {
	return fmt.Errorf("%s %s: %s", t.table.Service, t.op.Operation, fmt.Sprintf(format, args...))
}

// build binds one table operation to the model.
func build(m *Model, table *Table, op Operation) (*tool, error) {
	t := &tool{table: table, op: op, action: op.Operation, version: m.Version, global: table.Geltung == Global,
		id: "aws." + table.Segment + "." + op.Object + "." + op.Action, opName: words(op.Operation)}
	shape, err := m.operation(op.Operation)
	if err != nil {
		return nil, t.failf("%v", err)
	}
	if shape.obj("traits").has("smithy.api#deprecated") {
		return nil, t.failf("the operation is deprecated")
	}
	if op.AccountCheck && (table.Service != "sts" || op.Operation != "GetCallerIdentity") {
		return nil, t.failf("only STS GetCallerIdentity is the account check")
	}
	if op.TargetCheck == "bucket-owner" && m.Protocol != ProtocolRestXML {
		return nil, t.failf("target_check bucket-owner needs the S3 protocol package (restXml), not %s", m.Protocol)
	}
	if m.Protocol == ProtocolRestJSON || m.Protocol == ProtocolRestXML {
		if op.AccountCheck || op.CSV != nil {
			return nil, t.failf("the account check and csv reports belong to awsQuery tools")
		}
		t.rest, t.action, t.version = true, "", ""
		t.xml, t.noWrap = m.Protocol == ProtocolRestXML, m.NoErrorWrapping
		if t.restBind, err = m.BindRestCurated(table, op.Operation); err != nil {
			return nil, t.failf("%v", err)
		}
	}
	in, err := t.structOf(m, shape.obj("input").str("target"))
	if err != nil {
		return nil, err
	}
	out, err := t.structOf(m, shape.obj("output").str("target"))
	if err != nil {
		return nil, err
	}
	pag, err := t.paging(m, shape, in, out)
	if err != nil {
		return nil, err
	}
	t.plan = pag
	if op.CSV != nil {
		if pag != nil {
			return nil, t.failf("a csv tool pages over its rows, not over a service token")
		}
		if mm, ok := find(out, op.CSV.Member); !ok || mm.kind != "blob" {
			return nil, t.failf("csv member %s is not an output blob member", op.CSV.Member)
		}
		t.csv = op.CSV
	}
	t.released, t.limited, t.kept = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, k := range op.KeepDeprecated {
		t.kept[k.Member] = false
	}
	for _, r := range op.Release {
		t.released[r.Member] = false
	}
	for _, l := range op.Limits {
		t.limited[l.Member] = false
	}
	if err := t.bindInput(m, in, pag); err != nil {
		return nil, err
	}
	if err := t.bindOutput(m, out, pag); err != nil {
		return nil, err
	}
	for name, hit := range t.released {
		if !hit {
			return nil, t.failf("release %s is not a sensitive member of the input or the output", name)
		}
	}
	for name, hit := range t.kept {
		if !hit {
			return nil, t.failf("keep_deprecated %s is not a deprecated member of the output", name)
		}
	}
	for name, hit := range t.limited {
		if !hit {
			return nil, t.failf("limit %s is not a string or list member of the input", name)
		}
	}
	if t.rest {
		if err := t.assembleRest(); err != nil {
			return nil, err
		}
	}
	if err := t.makeSamples(m); err != nil {
		return nil, err
	}
	if !t.rest {
		for _, s := range []*awsquery.Shape{t.input, t.output} {
			if err := s.Validate(); err != nil {
				return nil, t.failf("the bound shape is invalid: %v", err)
			}
		}
	}
	return t, nil
}

// words turns GetAccessKeyInfo into "get access key info", the operation name of errors.
func words(name string) string {
	return strings.ReplaceAll(snake(name), "_", " ")
}

// snake turns a model member name into snake_case: AccessKeyId into access_key_id, SAMLAssertion into
// saml_assertion.
func snake(name string) string {
	var b strings.Builder
	r := []rune(name)
	for i, c := range r {
		upper := c >= 'A' && c <= 'Z'
		if upper && i > 0 {
			prev := r[i-1]
			nextLower := i+1 < len(r) && r[i+1] >= 'a' && r[i+1] <= 'z'
			if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') || (prev >= 'A' && prev <= 'Z' && nextLower) {
				b.WriteByte('_')
			}
		}
		if upper {
			c += 'a' - 'A'
		}
		b.WriteRune(c)
	}
	return b.String()
}

// member is one top-level member of an input or output structure.
type member struct {
	name   string
	obj    *object
	target string
	kind   string
	traits *object
}

// structOf returns the members of an operation's input or output structure; Unit or none is empty.
func (t *tool) structOf(m *Model, target string) ([]member, error) {
	if target == "" || target == "smithy.api#Unit" {
		return nil, nil
	}
	kind, s, err := m.resolve(target)
	if err != nil || kind != "structure" {
		return nil, t.failf("%s is not a structure", target)
	}
	ms := s.obj("members")
	var out []member
	for _, name := range ms.keys {
		mo := ms.obj(name)
		k, ts, err := m.resolve(mo.str("target"))
		if err != nil {
			return nil, t.failf("member %s: %v", name, err)
		}
		out = append(out, member{name: name, obj: mo, target: mo.str("target"), kind: k, traits: mergedTraits(mo, ts)})
	}
	return out, nil
}

func find(ms []member, name string) (member, bool) {
	for _, mm := range ms {
		if mm.name == name {
			return mm, true
		}
	}
	return member{}, false
}

func deprecated(mm member) bool { return mm.traits.has("smithy.api#deprecated") }

// pagingPlan is what paging takes out of the public arguments and fields.
type pagingPlan struct {
	tokenIn, tokenOut, sizeMember, truncated string
	// ins and outs hold every member of the position, the first being tokenIn and tokenOut. More than one
	// makes a cursor over several members (Route 53 record sets).
	ins, outs []string
	// starts says that the members of ins are also offered as arguments of the first page.
	starts bool
}

// paging derives the pager from the paginated trait, or from Marker with IsTruncated without one.
func (t *tool) paging(m *Model, shape *object, in, out []member) (*pagingPlan, error) {
	var p pagingPlan
	trait := shape.obj("traits").obj("smithy.api#paginated")
	if spec := t.pagerSpec(); spec != nil {
		// The table names the paging of a list the model leaves unpaged or pages by several members.
		for _, pos := range spec.Positions {
			p.ins, p.outs = append(p.ins, pos.In), append(p.outs, pos.Out)
		}
		p.tokenIn, p.tokenOut, p.sizeMember, p.truncated = p.ins[0], p.outs[0], spec.PageSize, orDefault(spec.Truncated, "IsTruncated")
		if spec.TokenEnds {
			p.truncated = ""
		}
		if p.starts = spec.StartArguments; p.starts && len(p.ins) < 2 {
			return nil, t.failf("start arguments need a list of several positions")
		}
	} else if trait != nil {
		base := m.service.obj("traits").obj("smithy.api#paginated")
		pick := func(k string) string {
			if trait.has(k) {
				return trait.str(k)
			}
			return base.str(k)
		}
		p.tokenIn, p.tokenOut, p.sizeMember = pick("inputToken"), pick("outputToken"), pick("pageSize")
		if p.tokenIn == "" || p.tokenOut == "" {
			return nil, t.failf("the paginated trait names no input or output token")
		}
	} else {
		_, inMarker := find(in, "Marker")
		_, outMarker := find(out, "Marker")
		_, truncated := find(out, truncatedMember(out))
		if !inMarker || !outMarker || !truncated {
			return nil, nil
		}
		p.tokenIn, p.tokenOut = "Marker", "Marker"
		if _, ok := find(in, "MaxItems"); ok {
			p.sizeMember = "MaxItems"
		}
	}
	if p.ins == nil {
		p.ins, p.outs = []string{p.tokenIn}, []string{p.tokenOut}
	}
	for _, name := range slices.Concat(p.ins, p.outs, []string{p.sizeMember}) {
		if strings.Contains(name, ".") {
			return nil, t.failf("paging member %s is nested", name)
		}
	}
	for _, name := range p.ins {
		if mm, ok := find(in, name); !ok || !stringKind(mm.kind) {
			return nil, t.failf("paging input token %s is not a string member", name)
		}
	}
	for _, name := range p.outs {
		if mm, ok := find(out, name); !ok || !stringKind(mm.kind) {
			return nil, t.failf("paging output token %s is not a string member", name)
		}
	}
	truncated := truncatedMember(out)
	if t.pagerSpec() != nil {
		truncated = p.truncated
	}
	if mm, ok := find(out, truncated); ok {
		if mm.kind != "boolean" {
			return nil, t.failf("%s is not a boolean", mm.name)
		}
		p.truncated = mm.name
	}
	if p.sizeMember != "" {
		mm, ok := find(in, p.sizeMember)
		if !ok || !isInteger(mm.kind) {
			return nil, t.failf("page size %s is not an integer member", p.sizeMember)
		}
		if _, ok := t.pageMax(mm); !ok {
			return nil, t.failf("page size %s has no range maximum", p.sizeMember)
		}
	}
	return &p, nil
}

// pageMax is the largest page the tool offers: the range maximum of the model, which the table may only
// lower, or the table's own maximum when the model has none. Only a restJson1 tool takes the table's.
func (t *tool) pageMax(mm member) (int64, bool) {
	max, ok := number(mm.traits.obj("smithy.api#range"), "max")
	switch {
	case ok && (t.op.PageSizeMax == 0 || int64(t.op.PageSizeMax) <= max):
		if t.op.PageSizeMax > 0 {
			return int64(t.op.PageSizeMax), true
		}
		return max, true
	case ok:
		return 0, false
	case t.rest && t.op.PageSizeMax > 0:
		return int64(t.op.PageSizeMax), true
	}
	return 0, false
}

// truncatedMember names the output member that says more entries follow: IsTruncated, which one operation
// (ListDelegationRequests) spells isTruncated.
func truncatedMember(out []member) string {
	if _, ok := find(out, "isTruncated"); ok {
		return "isTruncated"
	}
	return "IsTruncated"
}

func isInteger(kind string) bool {
	return slices.Contains([]string{"byte", "short", "integer", "long", "intEnum"}, kind)
}

// number reads an integer member of a trait object.
func number(o *object, key string) (int64, bool) {
	n, ok := o.get(key).(json.Number)
	if !ok {
		return 0, false
	}
	v, err := n.Int64()
	return v, err == nil
}

func hiddenSet(list []Hidden) map[string]bool {
	out := map[string]bool{}
	for _, h := range list {
		out[h.Member] = true
	}
	return out
}

// bindInput builds the input shape, the input schema, and the arguments.
func (t *tool) bindInput(m *Model, in []member, pag *pagingPlan) error {
	hidden := hiddenSet(t.op.HiddenInputs)
	t.nestedIn, t.enumHit, t.inModel = map[string]bool{}, map[string]bool{}, map[string]string{}
	for name := range hidden {
		if top, _, nested := strings.Cut(name, "."); nested {
			// A dotted name hides a member below a top-level one, such as Attributes.PrivateKey.
			t.nestedIn[name] = false
			delete(hidden, name)
			name = top
		}
		if _, ok := find(in, name); !ok {
			return t.failf("hidden input %s is not an input member", name)
		}
	}
	for _, e := range t.op.ExcludeEnum {
		t.enumHit[e.Member] = false
	}
	shape := &awsquery.Shape{Kind: awsquery.KindStruct}
	props := newObject()
	var required []string
	used := map[string]bool{}
	offered := map[string]bool{}
	for _, mm := range in {
		if deprecated(mm) || hidden[mm.name] {
			continue
		}
		if t.layerOwned(mm.name) {
			continue
		}
		startArg := pag != nil && pag.starts && slices.Contains(pag.ins, mm.name)
		if pag != nil && !startArg && (slices.Contains(pag.ins, mm.name) || mm.name == pag.sizeMember) {
			continue
		}
		if mm.traits.has("smithy.api#idempotencyToken") || (t.op.TokenMember != "" && mm.name == t.op.TokenMember) {
			if t.rest && !t.xml {
				return t.failf("the idempotency token %s of a restJson1 tool is not rendered yet", mm.name)
			}
			if t.hasToken || mm.kind != "string" || !idempotency.IsParam(mm.name) {
				return t.failf("idempotency token %s is not one known string member", mm.name)
			}
			t.hasToken = true
			t.inModel[argClientToken] = mm.name
			shape.Fields = append(shape.Fields, awsquery.Field{Key: argClientToken, Name: t.wire(mm), Shape: &awsquery.Shape{Kind: awsquery.KindString}, IdempotencyToken: true})
			continue
		}
		a, ok := t.op.Arguments[mm.name]
		if !ok {
			return t.failf("input member %s has no argument entry in the table", mm.name)
		}
		offered[mm.name] = true
		name := orDefault(a.Name, snake(mm.name))
		if slices.Contains(reservedNames, name) || used[name] {
			return t.failf("argument name %s is reserved or used twice", name)
		}
		used[name] = true
		c := converter{m: m, t: t, path: mm.name}
		s, schema, err := c.convert(mm.target, mm.traits, false, 0)
		if err != nil {
			return err
		}
		if !startArg {
			// A start argument is only in the schema: the page carries it to the request, like the cursor.
			shape.Fields = append(shape.Fields, awsquery.Field{Key: name, Name: t.wire(mm), Shape: s, Flattened: mm.traits.has("smithy.api#xmlFlattened")})
			t.inModel[name] = mm.name
		}
		props.set(name, schema)
		isRequired := mm.traits.has("smithy.api#required")
		if isRequired {
			required = append(required, name)
		}
		t.args = append(t.args, descArg{name: name, description: a.Description, required: isRequired})
	}
	for name := range t.op.Arguments {
		if !offered[name] {
			return t.failf("argument entry %s is not an offered input member", name)
		}
	}
	for name, hit := range t.nestedIn {
		if !hit {
			return t.failf("hidden input %s is not a member below an offered input member", name)
		}
	}
	for name, hit := range t.enumHit {
		if !hit {
			return t.failf("enum exclusion %s is not an enum member of an offered input", name)
		}
	}
	for _, list := range []struct {
		names []string
		out   *[]string
	}{{t.op.TargetArgs, &t.targetArgs}, {t.op.PolicyArgs, &t.policyArgs}} {
		for _, name := range list.names {
			mm, ok := find(in, name)
			if !ok || !offered[name] || mm.kind != "string" {
				return t.failf("checked argument %s is not an offered top-level string member", name)
			}
			*list.out = append(*list.out, orDefault(t.op.Arguments[name].Name, snake(name)))
		}
	}
	if name := t.op.SelfRole; name != "" {
		// A required member, so the own-role check can never be skipped by leaving the argument out.
		mm, ok := find(in, name)
		if !ok || !offered[name] || mm.kind != "string" || !mm.traits.has("smithy.api#required") {
			return t.failf("self_role %s is not an offered, required top-level string member", name)
		}
		t.selfRole = orDefault(t.op.Arguments[name].Name, snake(name))
	}
	if pag != nil {
		if pag.sizeMember != "" {
			mm, _ := find(in, pag.sizeMember)
			max, _ := t.pageMax(mm)
			props.set(awsquery.ArgPageSize, newObject("type", "integer", "minimum", 1, "maximum", max))
			t.args = append(t.args, descArg{name: awsquery.ArgPageSize, description: fmt.Sprintf(
				"Entries per page, 1 to %d; the service's default when left out", max)})
			t.pager = &awsquery.Pager{SizeParam: t.wire(mm), MaxSize: int(max)}
		} else {
			t.pager = &awsquery.Pager{}
		}
		tokenIn, _ := find(in, pag.tokenIn)
		t.pager.TokenIn = t.wire(tokenIn)
		if len(pag.ins) > 1 {
			// The positions come from the binding of the model; assembleRest sets the page size name.
			if t.restBind == nil || t.restBind.Multi == nil {
				return t.failf("paging by several members needs a REST binding")
			}
			t.multi = &httpbind.MultiPager{Positions: slices.Clone(t.restBind.Multi.Positions), Truncated: t.restBind.Multi.Truncated,
				SizeParam: t.pager.SizeParam, MaxSize: t.pager.MaxSize}
			if pag.starts {
				for i, name := range pag.ins {
					t.multi.Positions[i].Start = orDefault(t.op.Arguments[name].Name, snake(name))
				}
			}
			t.pager = nil
		}
		props.set(awsquery.ArgCursor, newObject("type", "string", "maxLength", awsquery.MaxCursorLength))
		t.args = append(t.args, descArg{name: awsquery.ArgCursor, description: "next_cursor of the previous page, to " +
			"continue the list; valid only with the same arguments"})
	}
	if t.csv != nil {
		props.set(awsquery.ArgPageSize, newObject("type", "integer", "minimum", 1, "maximum", t.csv.MaxPage))
		t.args = append(t.args, descArg{name: awsquery.ArgPageSize, description: fmt.Sprintf(
			"Rows per page, 1 to %d; %d when left out", t.csv.MaxPage, t.csv.DefaultPage)})
		props.set(awsquery.ArgCursor, newObject("type", "string", "maxLength", awsquery.MaxCursorLength))
		t.args = append(t.args, descArg{name: awsquery.ArgCursor, description: "next_cursor of the previous page, to " +
			"continue the rows; valid only for the same report"})
	}
	if t.hasToken {
		props.set(argClientToken, newObject("type", "string", "minLength", 1, "maxLength", 128, "pattern", `^[\x21-\x7e]{1,128}$`))
		t.args = append(t.args, descArg{name: argClientToken, description: "Idempotency token to send instead of a " +
			"generated one, 1 to 128 printable ASCII characters; pass the client_token of an earlier result to repeat " +
			"that request after a timeout"})
	}
	if !t.global {
		props.set(argRegion, rawJSON(regionMarker))
		t.args = append(t.args, descArg{name: argRegion, region: true})
	}
	schema := newObject("type", "object", "properties", props)
	if len(required) > 0 {
		schema.set("required", required)
	}
	schema.set("additionalProperties", false)
	t.input = shape
	t.inputSchema = encodeCompact(schema)
	return nil
}

// wire is the name of a member on the wire: the jsonName of a restJson1 member, the xmlName of an awsQuery or
// restXml member, else its name. mm.traits holds the merged traits of the member.
func (t *tool) wire(mm member) string {
	if t.rest && !t.xml {
		if name := mm.traits.str("smithy.api#jsonName"); name != "" {
			return name
		}
		return mm.name
	}
	if name := mm.obj.obj("traits").str("smithy.api#xmlName"); name != "" {
		return name
	}
	return mm.name
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// bindOutput builds the output shape, the output schema, the fields, and the runtime rules.
func (t *tool) bindOutput(m *Model, out []member, pag *pagingPlan) error {
	hidden := hiddenSet(t.op.HiddenOutputs)
	t.nested, t.outModel = map[string]bool{}, map[string]string{}
	for name := range hidden {
		if top, _, nested := strings.Cut(name, "."); nested {
			// A dotted name hides a member below a top-level one, such as Devices.Seed; lists add no segment.
			t.nested[name] = false
			delete(hidden, name)
			name = top
		}
		if _, ok := find(out, name); !ok {
			return t.failf("hidden output %s is not an output member", name)
		}
	}
	shape := &awsquery.Shape{Kind: awsquery.KindStruct}
	props := newObject()
	required := []string{}
	used := map[string]bool{}
	returned := map[string]bool{}
	if !t.global {
		props.set(argRegion, rawJSON(regionMarker))
		required = append(required, argRegion)
		t.fields = append(t.fields, descField{name: argRegion, region: true})
	}
	for _, mm := range out {
		if deprecated(mm) || hidden[mm.name] {
			continue
		}
		if t.op.AccountCheck && !slices.Contains(accountCheckMembers, mm.name) {
			return t.failf("the account check answers only %v, not %s", accountCheckMembers, mm.name)
		}
		if pag != nil && (slices.Contains(pag.outs, mm.name) || mm.name == pag.truncated) {
			// Kept in the shape for the pager, never returned.
			c := converter{m: m, t: t, path: mm.name, output: true}
			s, _, err := c.convert(mm.target, mm.traits, true, 0)
			if err != nil {
				return err
			}
			shape.Fields = append(shape.Fields, awsquery.Field{Key: "~" + mm.name, Name: t.wire(mm), Shape: s})
			t.outModel["~"+mm.name] = "~" + mm.name
			continue
		}
		if t.csv != nil && mm.name == t.csv.Member {
			// Kept in the shape for the CSV step of the runtime, never returned as it is.
			c := converter{m: m, t: t, path: mm.name, output: true, maxLen: t.csv.MaxBytes}
			s, _, err := c.convert(mm.target, mm.traits, true, 0)
			if err != nil {
				return err
			}
			shape.Fields = append(shape.Fields, awsquery.Field{Key: csvMemberKey, Name: t.wire(mm), Shape: s})
			continue
		}
		f, ok := t.op.Fields[mm.name]
		if !ok {
			return t.failf("output member %s has no field entry in the table", mm.name)
		}
		returned[mm.name] = true
		name := orDefault(f.Name, snake(mm.name))
		if slices.Contains(reservedNames, name) || used[name] {
			return t.failf("field name %s is reserved or used twice", name)
		}
		used[name] = true
		if f.Form != "" && mm.kind != "string" {
			return t.failf("field %s has a form but is not a string", mm.name)
		}
		if f.MaxLen > 0 && mm.kind != "string" {
			return t.failf("field %s has a cap but is not a string", mm.name)
		}
		if f.MaxItems > 0 && mm.kind != "list" && mm.kind != "set" {
			return t.failf("field %s has an item cap but is not a list", mm.name)
		}
		c := converter{m: m, t: t, path: mm.name, output: true, maxLen: f.MaxLen, maxItems: f.MaxItems}
		s, schema, err := c.convert(mm.target, mm.traits, true, 0)
		if err != nil {
			return err
		}
		if f.Form != "" {
			// A checked value is decoded whole: cut to its cap first, a longer value could pass the check.
			s.MaxLen = defaultMaxString
		}
		shape.Fields = append(shape.Fields, awsquery.Field{Key: name, Name: t.wire(mm), Shape: s, Flattened: mm.traits.has("smithy.api#xmlFlattened")})
		t.outModel[name] = mm.name
		props.set(name, schema)
		if f.Required {
			required = append(required, name)
		}
		if f.Required || f.Form != "" {
			t.rules = append(t.rules, outputRule{key: name, form: f.Form, required: f.Required})
		}
		t.fields = append(t.fields, descField{name: name, description: f.Description})
	}
	for name := range t.op.Fields {
		if !returned[name] {
			return t.failf("field entry %s is not a returned output member", name)
		}
	}
	for name, hit := range t.nested {
		if !hit {
			return t.failf("hidden output %s is not a member below an output member", name)
		}
	}
	if pag != nil {
		for _, f := range shape.Fields {
			if t.pager == nil {
				break
			}
			switch f.Name {
			case t.pagingWire(out, pag.tokenOut):
				t.pager.TokenOut = f.Key
			case t.pagingWire(out, pag.truncated):
				t.pager.Truncated = f.Key
			}
		}
		props.set(fieldNext, newObject("type", "string", "maxLength", awsquery.MaxCursorLength))
		props.set(fieldHasMore, newObject("type", "boolean"))
		required = append(required, fieldHasMore)
		t.fields = append(t.fields,
			descField{name: fieldNext, description: "Cursor of the next page; absent at the end of the list"},
			descField{name: fieldHasMore, description: "True when more entries follow; a full page alone never says so"})
	}
	if t.csv != nil {
		rowProps, rowRequired := newObject(), []string{}
		for _, col := range t.csv.Columns {
			if col.Bool {
				rowProps.set(col.Name, newObject("type", "boolean"))
			} else {
				rowProps.set(col.Name, newObject("type", "string", "maxLength", t.csv.MaxField))
			}
			if col.Required {
				rowRequired = append(rowRequired, col.Name)
			}
		}
		props.set(fieldRows, newObject("type", "array", "maxItems", t.csv.MaxPage, "items",
			newObject("type", "object", "properties", rowProps, "required", rowRequired, "additionalProperties", false)))
		props.set(fieldDropped, newObject("type", "array", "maxItems", csvMaxDropped, "items", newObject("type", "string", "maxLength", 64)))
		props.set(fieldTotal, newObject("type", "integer", "minimum", 0))
		props.set(fieldNext, newObject("type", "string", "maxLength", awsquery.MaxCursorLength))
		props.set(fieldHasMore, newObject("type", "boolean"))
		required = append(required, fieldRows, fieldTotal, fieldHasMore)
		t.fields = append(t.fields,
			descField{name: fieldRows, description: "Rows of this page, one per IAM user and the root account, with the known report columns " +
				"under their names in the report; a field that is empty, N/A, or not_supported is left out"},
			descField{name: fieldDropped, description: "Names of report columns that this tool does not know and therefore dropped; absent when none"},
			descField{name: fieldTotal, description: "Number of rows in the whole report"},
			descField{name: fieldNext, description: "Cursor of the next page; absent at the end of the rows"},
			descField{name: fieldHasMore, description: "True when more rows follow"})
	}
	if t.hasToken {
		props.set(argClientToken, newObject("type", "string", "maxLength", 128))
		required = append(required, argClientToken)
		t.fields = append(t.fields, descField{name: argClientToken, description: "Idempotency token the request " +
			"was sent with; pass it as client_token to repeat this request after a timeout"})
	}
	if hook := t.op.ResultHook; hook != "" {
		def := resultHookFields[hook]
		props.set(def.name, def.schema())
		required = append(required, def.name)
		t.fields = append(t.fields, descField{name: def.name, description: def.description})
	}
	if t.op.Truncated != "" {
		props.set(fieldTruncated, newObject("type", "boolean"))
		required = append(required, fieldTruncated)
		t.fields = append(t.fields, descField{name: fieldTruncated, description: t.op.Truncated})
	}
	schema := newObject("type", "object", "properties", props, "required", required, "additionalProperties", false)
	// A result member that is empty is left out of the answer, so the generated test cannot expect a hook's
	// member, whose sample is empty.
	t.resultKeys = slices.DeleteFunc(slices.Clone(required), func(k string) bool { return t.op.ResultHook != "" && k == resultHookFields[t.op.ResultHook].name })
	t.output = shape
	t.outputSchema = encodeCompact(schema)
	return nil
}

func (t *tool) pagingWire(out []member, name string) string {
	if name == "" {
		return "\x00"
	}
	mm, _ := find(out, name)
	return t.wire(mm)
}

// converter turns one model shape into a runtime shape and a JSON schema.
type converter struct {
	m      *Model
	t      *tool
	path   string
	output bool
	// maxLen is the table cap of a top-level output string, maxItems that of a top-level output list.
	maxLen, maxItems int
	stack            []string
}

func (c *converter) failf(format string, args ...any) error {
	return c.t.failf("%s: %s", c.path, fmt.Sprintf(format, args...))
}

// pattern translates a model pattern into the RE2 syntax of the schema validator. Smithy patterns are
// ECMA-262 expressions, whose \uXXXX is one UTF-16 code unit and which RE2 writes \x{XXXX}; that is exact
// outside the surrogate range, so a surrogate escape, and any pattern RE2 then refuses, stops the generation
// instead of being guessed at.
func (c *converter) pattern(p string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] != '\\' || i+1 >= len(p) {
			b.WriteByte(p[i])
			continue
		}
		if p[i+1] == 'u' {
			code, err := strconv.ParseUint(p[min(i+2, len(p)):min(i+6, len(p))], 16, 32)
			if err != nil || i+6 > len(p) || (code >= 0xD800 && code <= 0xDFFF) {
				return "", c.failf("pattern uses a \\u escape RE2 cannot express")
			}
			b.WriteString(`\x{` + p[i+2:i+6] + `}`)
			i += 5
			continue
		}
		b.WriteString(p[i : i+2])
		i++
	}
	out := b.String()
	if _, err := regexp.Compile(out); err != nil {
		return "", c.failf("pattern is not a valid RE2 expression")
	}
	return out, nil
}

func (c *converter) convert(target string, traits *object, output bool, depth int) (*awsquery.Shape, *object, error) {
	if depth > 10 || slices.Contains(c.stack, target) {
		return nil, nil, c.failf("shape %s is recursive or nested too deep", target)
	}
	c.stack = append(c.stack, target)
	defer func() { c.stack = c.stack[:len(c.stack)-1] }()
	kind, s, err := c.m.resolve(target)
	if err != nil {
		return nil, nil, c.failf("%v", err)
	}
	if traits.has("smithy.api#sensitive") {
		if _, released := c.t.released[c.path]; !released {
			return nil, nil, c.failf("the member is sensitive; hide it with a reason, or exclude the operation")
		}
		c.t.released[c.path] = true
	}
	if output && (kind == "string" || kind == "blob") && credentialName.MatchString(lastSegment(c.path)) {
		return nil, nil, c.failf("the member looks like a credential; hide it with a reason, or exclude the operation")
	}
	length := traits.obj("smithy.api#length")
	switch kind {
	case "string", "enum":
		schema := newObject("type", "string")
		shape := &awsquery.Shape{Kind: awsquery.KindString}
		if output {
			limit := c.maxLen
			if enc, ok := c.t.urlEncoded(c.m, target); ok {
				shape.URLDecoded = true
				if limit == 0 {
					limit = enc.MaxLen
				}
			}
			if limit == 0 {
				if max, ok := number(length, "max"); ok {
					limit = int(max)
				} else {
					limit = defaultMaxString
				}
			}
			shape.MaxLen = limit
			schema.set("maxLength", limit)
			c.maxLen = 0
			return shape, schema, nil
		}
		if min, ok := number(length, "min"); ok {
			schema.set("minLength", min)
		}
		if max, ok := number(length, "max"); ok {
			schema.set("maxLength", max)
		}
		if l, ok := c.limit(false); ok {
			if max, bounded := number(length, "max"); !bounded || int64(l.MaxLen) < max {
				schema.set("maxLength", l.MaxLen)
			}
		}
		if p := traits.str("smithy.api#pattern"); p != "" {
			p, err := c.pattern(p)
			if err != nil {
				return nil, nil, err
			}
			schema.set("pattern", p)
		}
		if values := enumValues(kind, s, traits); len(values) > 0 {
			if values, err = c.narrow(values); err != nil {
				return nil, nil, err
			}
			schema.set("enum", values)
		}
		return shape, schema, nil
	case "byte", "short", "integer", "long", "intEnum":
		schema := newObject("type", "integer")
		if !output {
			r := traits.obj("smithy.api#range")
			if min, ok := number(r, "min"); ok {
				schema.set("minimum", min)
			}
			if max, ok := number(r, "max"); ok {
				schema.set("maximum", max)
			}
		}
		return &awsquery.Shape{Kind: awsquery.KindInteger}, schema, nil
	case "float", "double":
		return &awsquery.Shape{Kind: awsquery.KindFloat}, newObject("type", "number"), nil
	case "boolean":
		return &awsquery.Shape{Kind: awsquery.KindBoolean}, newObject("type", "boolean"), nil
	case "timestamp":
		// A restXml body writes timestamps as date-time, so that format needs no rendering below the top level; a
		// top-level member can be a header, whose default differs.
		if f := traits.str("smithy.api#timestampFormat"); c.t.rest && f != "" && !(f == "date-time" && c.t.xml && depth > 0) {
			return nil, nil, c.failf("timestamp format %s is not rendered for restJson1 tools yet", f)
		} else if f != "" && f != "date-time" {
			return nil, nil, c.failf("timestamp format %s is not the query protocol's date-time", f)
		}
		return &awsquery.Shape{Kind: awsquery.KindTimestamp}, newObject("type", "string", "maxLength", 64), nil
	case "blob":
		shape := &awsquery.Shape{Kind: awsquery.KindBlob}
		schema := newObject("type", "string")
		if output {
			limit := defaultMaxString
			if c.maxLen > 0 {
				limit, c.maxLen = c.maxLen, 0
			}
			shape.MaxLen = limit
			schema.set("maxLength", limit)
		}
		return shape, schema, nil
	case "structure":
		return c.structure(s, output, depth)
	case "union":
		shape, schema, err := c.structure(s, output, depth)
		if err == nil && !output {
			// A union is a structure with exactly one member set; the query protocol encodes it the same way.
			schema.set("minProperties", 1)
			schema.set("maxProperties", 1)
		}
		return shape, schema, err
	case "list", "set":
		mo := s.obj("member")
		_, ts, err := c.m.resolve(mo.str("target"))
		if err != nil {
			return nil, nil, c.failf("%v", err)
		}
		elem, items, err := c.convert(mo.str("target"), mergedTraits(mo, ts), output, depth+1)
		if err != nil {
			return nil, nil, err
		}
		shape := &awsquery.Shape{Kind: awsquery.KindList, Elem: elem, ElemName: mo.obj("traits").str("smithy.api#xmlName")}
		schema := newObject("type", "array", "items", items)
		c.items(shape, schema, length, output, depth)
		if l, ok := c.limit(true); ok {
			if max, bounded := number(length, "max"); !bounded || int64(l.MaxItems) < max {
				schema.set("maxItems", l.MaxItems)
			}
		}
		return shape, schema, nil
	case "map":
		ko, vo := s.obj("key"), s.obj("value")
		kk, _, err := c.m.resolve(ko.str("target"))
		if err != nil || (kk != "string" && kk != "enum") {
			return nil, nil, c.failf("map key is not a string")
		}
		_, vs, err := c.m.resolve(vo.str("target"))
		if err != nil {
			return nil, nil, c.failf("%v", err)
		}
		elem, values, err := c.convert(vo.str("target"), mergedTraits(vo, vs), output, depth+1)
		if err != nil {
			return nil, nil, err
		}
		shape := &awsquery.Shape{Kind: awsquery.KindMap, Elem: elem, KeyName: ko.obj("traits").str("smithy.api#xmlName"),
			ValueName: vo.obj("traits").str("smithy.api#xmlName")}
		schema := newObject("type", "object", "additionalProperties", values)
		if output {
			shape.MaxItems = defaultMaxItems
			schema.set("maxProperties", defaultMaxItems)
		}
		return shape, schema, nil
	}
	return nil, nil, c.failf("shape type %s has no query-protocol binding here", kind)
}

// limit returns the table cap of the input member at c.path and marks it used.
func (c *converter) limit(items bool) (InputLimit, bool) {
	if c.output {
		return InputLimit{}, false
	}
	for _, l := range c.t.op.Limits {
		if l.Member == c.path && (l.MaxItems > 0) == items {
			c.t.limited[l.Member] = true
			return l, true
		}
	}
	return InputLimit{}, false
}

// narrow removes the values the table excludes from the enum of the member at c.path.
func (c *converter) narrow(values []any) ([]any, error) {
	for _, e := range c.t.op.ExcludeEnum {
		if e.Member != c.path {
			continue
		}
		c.t.enumHit[e.Member] = true
		values = slices.DeleteFunc(slices.Clone(values), func(v any) bool {
			text, _ := v.(string)
			return slices.Contains(e.Values, text)
		})
		if len(values) == 0 {
			return nil, c.failf("the enum exclusion leaves no value")
		}
	}
	return values, nil
}

func lastSegment(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

func (c *converter) items(shape *awsquery.Shape, schema, length *object, output bool, depth int) {
	if output {
		limit := defaultMaxItems
		if max, ok := number(length, "max"); ok {
			limit = int(max)
		}
		if _, bounded := number(length, "max"); c.maxItems > 0 && depth == 0 && (!bounded || c.maxItems < limit) {
			limit = c.maxItems
		}
		shape.MaxItems = limit
		schema.set("maxItems", limit)
		return
	}
	if min, ok := number(length, "min"); ok {
		schema.set("minItems", min)
	}
	if max, ok := number(length, "max"); ok {
		schema.set("maxItems", max)
	}
}

func (c *converter) structure(s *object, output bool, depth int) (*awsquery.Shape, *object, error) {
	shape := &awsquery.Shape{Kind: awsquery.KindStruct}
	props := newObject()
	var required []string
	ms := s.obj("members")
	parent := c.path
	defer func() { c.path = parent }()
	for _, name := range ms.keys {
		mo := ms.obj(name)
		_, ts, err := c.m.resolve(mo.str("target"))
		if err != nil {
			return nil, nil, c.failf("%v", err)
		}
		traits := mergedTraits(mo, ts)
		c.path = parent + "." + name
		if traits.has("smithy.api#deprecated") {
			if _, keep := c.t.kept[c.path]; !output || !keep {
				continue
			}
			c.t.kept[c.path] = true
		}
		if _, hide := c.t.nested[c.path]; output && hide {
			c.t.nested[c.path] = true
			continue
		}
		if _, hide := c.t.nestedIn[c.path]; !output && hide {
			if traits.has("smithy.api#required") {
				return nil, nil, c.failf("a required member cannot be hidden")
			}
			c.t.nestedIn[c.path] = true
			continue
		}
		if traits.has("smithy.api#idempotencyToken") {
			return nil, nil, c.failf("an idempotency token below the top level has no binding")
		}
		key := snake(name)
		elem, schema, err := c.convert(mo.str("target"), traits, output, depth+1)
		if err != nil {
			return nil, nil, err
		}
		shape.Fields = append(shape.Fields, awsquery.Field{Key: key, Name: c.t.wire(member{obj: mo, name: name, traits: traits}), Shape: elem,
			Flattened: traits.has("smithy.api#xmlFlattened")})
		props.set(key, schema)
		if !output && traits.has("smithy.api#required") {
			required = append(required, key)
		}
	}
	schema := newObject("type", "object", "properties", props)
	if len(required) > 0 {
		schema.set("required", required)
	}
	schema.set("additionalProperties", false)
	return shape, schema, nil
}

// enumValues lists the values of an enum shape or of the legacy enum trait.
func enumValues(kind string, s, traits *object) []any {
	var values []any
	if kind == "enum" {
		ms := s.obj("members")
		for _, name := range ms.keys {
			v := ms.obj(name).obj("traits").str("smithy.api#enumValue")
			if v == "" {
				v = name
			}
			values = append(values, v)
		}
		return values
	}
	list, _ := traits.get("smithy.api#enum").([]any)
	for _, item := range list {
		if o, ok := item.(*object); ok {
			values = append(values, o.str("value"))
		}
	}
	return values
}

// makeSamples derives the generated test of the tool from its shapes and schema: argument values that
// pass the schema, the form parameters they encode to, and an answer the output shape decodes.
func (t *tool) makeSamples(m *Model) error {
	var schema map[string]any
	d := json.NewDecoder(strings.NewReader(strings.ReplaceAll(t.inputSchema, regionMarker, "{}")))
	d.UseNumber()
	if err := d.Decode(&schema); err != nil {
		return t.failf("input schema does not parse: %v", err)
	}
	args := newObject()
	props, _ := schema["properties"].(map[string]any)
	for _, f := range t.input.Fields {
		if f.IdempotencyToken {
			continue
		}
		if sample, ok := targetSamples[t.op.TargetCheck]; ok && slices.Contains(t.targetArgs, f.Key) {
			// The generated test binds this account; a repeated character would fail the target check.
			args.set(f.Key, sample)
			continue
		}
		if strings.HasPrefix(t.op.TargetCheck, "arn-") && slices.Contains(t.targetArgs, f.Key) {
			args.set(f.Key, sampleARN)
			continue
		}
		if t.op.PolicyCheck == "trust-policy" && slices.Contains(t.policyArgs, f.Key) {
			args.set(f.Key, sampleTrustPolicy)
			continue
		}
		if t.op.PolicyCheck == "resource-policy-own-account" && slices.Contains(t.policyArgs, f.Key) {
			args.set(f.Key, sampleResourcePolicy)
			continue
		}
		v, err := t.sample(f.Key, f.Shape, props[f.Key])
		if err != nil {
			return err
		}
		args.set(f.Key, v)
	}
	if t.op.SampleArgs != "" {
		var over map[string]json.RawMessage
		if err := json.Unmarshal([]byte(t.op.SampleArgs), &over); err != nil {
			return t.failf("sample arguments of the table do not parse: %v", err)
		}
		for _, name := range sortedRawKeys(over) {
			if !args.has(name) {
				return t.failf("sample argument %s is not an argument of the generated test", name)
			}
			args.set(name, rawJSON(over[name]))
		}
	}
	t.sampleArgs = encodeCompact(args)
	var plain map[string]any
	pd := json.NewDecoder(strings.NewReader(t.sampleArgs))
	pd.UseNumber()
	if err := pd.Decode(&plain); err != nil {
		return t.failf("sample arguments do not parse: %v", err)
	}
	if t.rest {
		if err := t.restSamples(plain); err != nil {
			return err
		}
		return nil
	}
	req, err := awsquery.Encode(t.input, plain, bytes.NewReader(make([]byte, 16)))
	if err != nil {
		return t.failf("sample arguments do not encode: %v", err)
	}
	t.sampleForm = map[string]string{}
	for k, v := range req.Params {
		if !t.hasToken || v != req.Token {
			t.sampleForm[k] = v
		}
	}
	var body strings.Builder
	if err := t.answer(&body, t.output); err != nil {
		return err
	}
	ns := ""
	if m.Namespace != "" {
		ns = ` xmlns="` + m.Namespace + `"`
	}
	t.sampleAnswer = "<" + t.action + "Response" + ns + "><" + t.action + "Result>" + body.String() + "</" + t.action +
		"Result></" + t.action + "Response>"
	return nil
}

// sampleCSV is the report of a generated CSV test: the required columns and the root account.
const sampleCSV = "user,arn\n<root_account>,arn:aws:iam::123456789012:root\n"

// sampleChars are the characters a sample string is made of, tried in order until one fits the pattern.
var sampleChars = []string{"a", "A", "0", "x", "-", "_", "/", " "}

// sampleARN is an ARN that passes the ARN target checks in the account the generated tests bind.
const sampleARN = "arn:aws:iam::123456789012:role/a"

// targetSamples are values that pass a target check in the account and region the generated tests bind, by
// the name of the check.
var targetSamples = map[string]string{
	"bucket-owner":     "my-bucket",
	"ses-identity":     "example.com",
	"ses-identity-arn": "arn:aws:ses:eu-central-1:123456789012:identity/example.com",
}

// sampleTrustPolicy is a trust policy that passes the trust-policy check: one AWS service principal.
const sampleTrustPolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`

// sampleResourcePolicy passes the resource-policy-own-account check in the account the generated tests bind.
const sampleResourcePolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"123456789012"},"Action":"ses:SendEmail","Resource":"*"}]}`

// sampleWords are whole values tried after the repeated characters, for patterns with a fixed prefix such as
// the version IDs of IAM policies the UUID of a Route 53 CIDR collection, an IPv4 address, or an address of
// the form a@b, which stays outside the mail form that generated code must not hold.
var sampleWords = []string{"v1", "00000000-0000-0000-0000-000000000000", "192.0.2.1", "aaa@bbb"}

func (t *tool) sample(path string, s *awsquery.Shape, rawSchema any) (any, error) {
	schema, _ := rawSchema.(map[string]any)
	switch s.Kind {
	case awsquery.KindString:
		if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
			return values[0], nil
		}
		n := intOf(schema["minLength"], 1)
		if n < 1 {
			n = 1
		}
		var re *regexp.Regexp
		if p, ok := schema["pattern"].(string); ok {
			re = regexp.MustCompile(p)
		}
		for _, ch := range sampleChars {
			v := strings.Repeat(ch, n)
			if max := intOf(schema["maxLength"], len(v)); len(v) <= max && (re == nil || re.MatchString(v)) {
				return v, nil
			}
		}
		for _, v := range sampleWords {
			if len(v) >= n && len(v) <= intOf(schema["maxLength"], len(v)) && re != nil && re.MatchString(v) {
				return v, nil
			}
		}
		return nil, t.failf("%s: no sample value fits its pattern", path)
	case awsquery.KindInteger:
		n := intOf(schema["minimum"], 1)
		if max := intOf(schema["maximum"], n); n > max {
			n = max
		}
		return n, nil
	case awsquery.KindFloat:
		return json.Number("1.5"), nil
	case awsquery.KindBoolean:
		return true, nil
	case awsquery.KindTimestamp:
		return "2026-01-02T03:04:05Z", nil
	case awsquery.KindBlob:
		return "AA==", nil
	case awsquery.KindStruct:
		props, _ := schema["properties"].(map[string]any)
		o := newObject()
		for _, f := range s.Fields {
			if _, one := schema["maxProperties"]; one && len(o.keys) == 1 {
				break
			}
			v, err := t.sample(path+"."+f.Key, f.Shape, props[f.Key])
			if err != nil {
				return nil, err
			}
			o.set(f.Key, v)
		}
		return o, nil
	case awsquery.KindList:
		v, err := t.sample(path+"[]", s.Elem, schema["items"])
		return []any{v}, err
	case awsquery.KindMap:
		v, err := t.sample(path+"{}", s.Elem, schema["additionalProperties"])
		return newObject("k", v), err
	}
	return nil, t.failf("%s: no sample for this shape", path)
}

func intOf(v any, fallback int) int {
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	}
	return fallback
}

// answer writes the members of a struct shape as the XML of an answer. Paging ends on the first page.
func (t *tool) answer(b *strings.Builder, s *awsquery.Shape) error {
	for _, f := range s.Fields {
		if t.pagedOut(f.Key) {
			continue
		}
		var inner strings.Builder
		if err := t.answerValue(&inner, f); err != nil {
			return err
		}
		b.WriteString(inner.String())
	}
	return nil
}

func (t *tool) answerValue(b *strings.Builder, f awsquery.Field) error {
	el := func(name, text string) { b.WriteString("<" + name + ">" + text + "</" + name + ">") }
	switch f.Shape.Kind {
	case awsquery.KindStruct:
		var inner strings.Builder
		if err := t.answer(&inner, f.Shape); err != nil {
			return err
		}
		el(f.Name, inner.String())
	case awsquery.KindList:
		var inner strings.Builder
		elemName := orDefault(f.Shape.ElemName, "member")
		if f.Flattened {
			elemName = f.Name
		}
		if err := t.answerValue(&inner, awsquery.Field{Name: elemName, Shape: f.Shape.Elem}); err != nil {
			return err
		}
		if f.Flattened {
			b.WriteString(inner.String())
		} else {
			el(f.Name, inner.String())
		}
	case awsquery.KindMap:
		var inner strings.Builder
		el2 := func(name, text string) { inner.WriteString("<" + name + ">" + text + "</" + name + ">") }
		el2(orDefault(f.Shape.KeyName, "key"), "k")
		var value strings.Builder
		if err := t.answerValue(&value, awsquery.Field{Name: orDefault(f.Shape.ValueName, "value"), Shape: f.Shape.Elem}); err != nil {
			return err
		}
		inner.WriteString(value.String())
		if f.Flattened {
			el(f.Name, inner.String())
		} else {
			el(f.Name, "<entry>"+inner.String()+"</entry>")
		}
	default:
		el(f.Name, t.scalarSample(f))
	}
	return nil
}

func (t *tool) scalarSample(f awsquery.Field) string {
	switch f.Shape.Kind {
	case awsquery.KindInteger:
		return "1"
	case awsquery.KindFloat:
		return "1.5"
	case awsquery.KindBoolean:
		if t.pagedTruncated(f.Key) {
			return "false"
		}
		return "true"
	case awsquery.KindTimestamp:
		return "2026-01-02T03:04:05Z"
	case awsquery.KindBlob:
		if t.csv != nil && f.Key == csvMemberKey {
			return base64.StdEncoding.EncodeToString([]byte(sampleCSV))
		}
		return "AA=="
	}
	for _, r := range t.rules {
		if r.key == f.Key && r.form != "" {
			return formSamples[r.form]
		}
	}
	return "x"
}

func sortedRawKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// sortedKeys returns the keys of a form parameter map in order, for a deterministic rendering.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// urlEncoded reports whether the table lists the string shape target as returned percent-encoded.
func (t *tool) urlEncoded(m *Model, target string) (URLEncoded, bool) {
	if !strings.HasPrefix(target, m.namespace()+"#") {
		return URLEncoded{}, false
	}
	for _, e := range t.table.URLEncoded {
		if e.Shape == shortName(target) {
			return e, true
		}
	}
	return URLEncoded{}, false
}

// pagerSpec is the table's paging of the operation, nil for none.
func (t *tool) pagerSpec() *PagerSpec {
	if t.table == nil || !t.rest {
		return nil
	}
	for i := range t.table.Pagers {
		if t.table.Pagers[i].Operation == t.op.Operation {
			return &t.table.Pagers[i]
		}
	}
	return nil
}

// pagedOut reports an output key that carries a position of the list: it never reaches a result, and a
// generated answer leaves it out so that the list ends on the first page.
func (t *tool) pagedOut(key string) bool {
	if t.pager != nil {
		return key == t.pager.TokenOut
	}
	if t.multi != nil {
		return slices.ContainsFunc(t.multi.Positions, func(p httpbind.Position) bool { return p.Out == key })
	}
	return false
}

// pagedTruncated reports the output key that says whether the list continues.
func (t *tool) pagedTruncated(key string) bool {
	if t.pager != nil {
		return key == t.pager.Truncated
	}
	return t.multi != nil && key == t.multi.Truncated
}

// stringKind reports a member that is text on the wire: a string, or an enum, which a position of a list may be.
func stringKind(kind string) bool { return kind == "string" || kind == "enum" }
