package gen

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/endpoints"
)

// Table is the curation of one service model: which operations become tools and how, and why every other
// operation is not one. It is data; the generator reads it together with the model and refuses anything
// the two do not bind unambiguously.
type Table struct {
	// Service is the model name, the directory name in api-models-aws.
	Service string
	// Segment is the tool-ID segment from the segment table of the endpoints package.
	Segment string
	// Geltung is Regional or Global and must agree with the endpoint table.
	Geltung string
	// Operations become tools, in this order.
	Operations []Operation
	// Exclude lists operations that never become a tool, each with its reason.
	Exclude []Omission
	// Defer lists operations left for later, each with its reason.
	Defer []Omission
	// URLEncoded lists model string shapes whose values the service returns percent-encoded, such as the
	// IAM policy documents. Every output string of such a shape is decoded and then cut at MaxLen.
	URLEncoded []URLEncoded
	// Pagers describe the paging of REST list operations whose model has no paginated trait, or pages by
	// more than one member. A tool takes its pager from here, not from a guess.
	Pagers []PagerSpec
	// IDPrefixes list model string shapes whose arguments may carry a leading text that is not part of the
	// identifier, such as the /hostedzone/ of a Route 53 zone ID.
	IDPrefixes []IDPrefix
}

// PagerSpec is the paging of one REST list operation, by model member names.
type PagerSpec struct {
	Operation string
	// Positions are the members that together name the place the list continues at: the request member and
	// the output member that reports it. One position is a plain token; several travel in one cursor.
	Positions []PositionSpec
	// Truncated is the boolean output member that says whether the list continues; IsTruncated when empty.
	Truncated string
	// TokenEnds says that the output has no truncation member and the list ends where the token is absent.
	// It excludes Truncated and takes one position.
	TokenEnds bool
	// PageSize is the integer request member that sets the page size, empty for none.
	PageSize string
	// StartArguments also offers the position members as optional arguments of the first page, each with the
	// entry of its model member in the operation's Arguments. A later member needs the ones before it, and a
	// start argument cannot be combined with a cursor. Only a list of several positions takes it.
	StartArguments bool
}

// PositionSpec is one member of the position of a list.
type PositionSpec struct {
	In, Out string
	// Required says that an answer that reports more entries must carry the member.
	Required bool
}

// IDPrefix is a string shape of the model namespace and the leading text its arguments may carry.
type IDPrefix struct {
	Shape  string
	Prefix string
	// Operations limit the prefix to the arguments of these operations; empty for every operation.
	Operations []string
}

// URLEncoded is a model string shape whose output values arrive percent-encoded.
type URLEncoded struct {
	// Shape is the shape name in the model namespace, such as policyDocumentType.
	Shape string
	// MaxLen caps the decoded value in bytes; the model's length maximum applies when zero.
	MaxLen int
}

// Values of Table.Geltung.
const (
	Regional = "regional"
	Global   = "global"
)

// Omission is an operation that is not a tool, with the reason.
type Omission struct {
	Operation string
	Reason    string
}

// Operation is one model operation that becomes one tool.
type Operation struct {
	// Operation is the operation name in the model, such as GetCallerIdentity.
	Operation string
	// Object and Action are the last two parts of the tool ID aws.<segment>.<object>.<action>.
	Object, Action string
	// Effect is one of read, create, update, delete, execute.
	Effect string
	// Confirm makes the tool require confirmation. Every operation that is not a read must set it.
	Confirm bool
	// ListedOnly makes the tool require an allow-list entry (requires_tool_allow_list).
	ListedOnly bool
	// Idempotency is one of safe, idempotent, non-idempotent. A read is safe; nothing else is.
	Idempotency string
	Title       string
	// Description is the tool description; the generator appends the untrusted-data warning.
	Description string
	Tags        []string
	// DataSensitivity classifies the result for the risk contract.
	DataSensitivity string
	// Arguments describes every input member the tool offers, by model member name. Every offered member
	// needs an entry, since the extracts carry no documentation.
	Arguments map[string]Argument
	// Fields describes every output member the tool returns, by model member name.
	Fields map[string]Field
	// HiddenInputs and HiddenOutputs leave top-level members out for a security reason.
	HiddenInputs, HiddenOutputs []Hidden
	// TargetCheck names the hand-written check that runs on TargetArgs before any I/O.
	TargetCheck string
	// TargetArgs are the input members (top-level strings) TargetCheck reads. Empty for "none".
	TargetArgs []string
	// PolicyCheck names the client-side document check of the service package that runs on PolicyArgs
	// before the call.
	PolicyCheck string
	PolicyArgs  []string
	// SelfRole is the input member (a top-level string) that names a role the tool changes or deletes. The
	// runtime asks GetCallerIdentity before the call and refuses it when that role is the one whose session
	// signs it, so a connection never changes or deletes its own role. Only a tool that is not a read may set it.
	SelfRole string
	// AccountCheck marks the operation that is the account check itself (STS GetCallerIdentity): its answer
	// is the result, so the call is made once and is its own proof.
	AccountCheck bool
	// Truncated, when set, adds a boolean result field truncated with this description, true when a string
	// of the result was cut at its limit.
	Truncated string
	// CSV turns one blob output member that holds a CSV table into rows with the tool's own paging.
	CSV *CSVReport
	// PageSizeMax bounds page_size of a paginated tool whose model gives the page size member no range
	// maximum. The model's own maximum applies, and must not be exceeded, when it has one.
	PageSizeMax int
	// ResultHook names a hand-written step of the provider package that adds the result field the hook
	// declares to the result, computed from the arguments and the answer. Empty for none.
	ResultHook string
	// FailureHint names a hand-written step of the provider package that adds to the message of a failed call
	// what a person needs to know. Empty for none.
	FailureHint string
	// ExcludeEnum narrows the enum of an input member, so a value that would bring something the tool must
	// not take (such as key material) cannot be sent.
	ExcludeEnum []EnumExclusion
	// Release keeps a member open that the model marks sensitive, because it is an identifier and not a
	// secret, such as the ID of a KMS key. It applies to input and output, also below the top level.
	Release []Release
	// KeepDeprecated keeps a deprecated output member below the top level that the generator would leave out, because
	// leaving it out would change what the answer means, such as the old rule-level prefix of an S3 lifecycle rule.
	// Member is dotted like Release.Member. Input members that are deprecated stay out.
	KeepDeprecated []Release
	// TokenMember names a required top-level string member of a restXml input that serves as the idempotency token
	// although the model gives it no idempotencyToken trait, such as the CallerReference of Route 53. The tool
	// generates it and returns it as client_token, like a member with the trait.
	TokenMember string
	// Limits cap a string or a list of the input that the model leaves unbounded.
	Limits []InputLimit
	// Guards name hand-written steps of the provider package that judge the arguments before any I/O, each
	// with the binding of the connection and the region of the call.
	Guards []string
	// Delivery names a hand-written step of a tool that delivers to third parties, such as a mail: it spaces
	// the calls of one connection and region, and says when a failed call may still have delivered. Only a
	// non-idempotent tool that is not a read may name one.
	Delivery string
	// Readback names a hand-written step that reads the current state with a call of its own after the
	// account check and before the change, and refuses the change on what it finds. Only a tool that is not a
	// read may name one.
	Readback string
	// ReadbackAnswer is the XML answer of the generated test to that read; required with Readback.
	ReadbackAnswer string
	// SampleArgs replaces top-level arguments of the generated test, a JSON object by argument name, where
	// the derived sample cannot pass a guard.
	SampleArgs string
	Examples   []Example
}

// InputLimit caps one input member: MaxLen a string, MaxItems a list. Member is dotted like Release.Member; a
// list adds no segment, so the members of its elements follow the list's own name.
type InputLimit struct {
	Member           string
	MaxLen, MaxItems int
	Reason           string
}

// Release is a sensitive model member that stays open, with the reason.
type Release struct {
	// Member is the model member, dotted below the top level, as for HiddenOutputs.
	Member string
	Reason string
}

// EnumExclusion removes values from the enum of an input member.
type EnumExclusion struct {
	// Member is the model member, dotted below the top level, such as SigningAttributes.Origin.
	Member string
	Values []string
	Reason string
}

// CSVReport describes a CSV table returned as a base64 blob, such as the IAM credential report. The
// runtime decodes the blob, parses it as CSV, keeps only the listed columns, and returns the rows one page
// at a time; the blob itself is never part of a result.
type CSVReport struct {
	// Member is the output blob member, such as Content.
	Member string
	// Columns are the known header columns, in report order; any other column is dropped and named.
	Columns []CSVColumn
	// MaxBytes caps the base64 text of the blob, MaxRows the data rows, MaxField one field in bytes.
	MaxBytes, MaxRows, MaxField int
	// DefaultPage and MaxPage are the page sizes in rows.
	DefaultPage, MaxPage int
}

// CSVColumn is one known column of a CSVReport.
type CSVColumn struct {
	Name string
	// Bool columns hold true or false and become booleans; every other column stays text.
	Bool bool
	// Required columns must be in the header and filled in every row.
	Required bool
}

// Argument describes one offered input member.
type Argument struct {
	// Name replaces the derived snake_case name, for readability only.
	Name        string
	Description string
}

// Field describes one returned output member.
type Field struct {
	// Name replaces the derived snake_case name, for readability only.
	Name        string
	Description string
	// MaxLen caps a string; the model's length maximum or the default cap applies when zero.
	MaxLen int
	// MaxItems caps a top-level list; the default cap applies when zero. It is raised for a list whose page can
	// be larger than the default, and it never overrides a smaller maximum of the model.
	MaxItems int
	// Form names a hand-written check of the value, such as account-id.
	Form string
	// Required makes the tool refuse an answer without the member, and the schema require it.
	Required bool
}

// Hidden is a member left out of a tool, with the security reason.
type Hidden struct {
	Member string
	Reason string
}

// Example is one example invocation; Arguments is a JSON object.
type Example struct {
	Description string
	Arguments   string
}

// Names the generator knows. The checks themselves are hand-written in the provider package and in later
// service packages; a name outside these lists is refused.
var (
	Effects      = []string{"read", "create", "update", "delete", "execute"}
	Idempotency  = []string{"safe", "idempotent", "non-idempotent"}
	TargetChecks = []string{"none", "account-argument", "arn-account", "arn-account-or-managed-policy", "arn-region", "bucket-owner",
		"ses-identity", "ses-identity-arn", "ses-configuration-set"}
	PolicyChecks = []string{"none", "trust-policy", "resource-policy-own-account", "bucket-policy", "identity-policy"}
	// Guards are the pure argument steps and Readbacks the steps with a read of their own; see Operation.
	Guards     = []string{"location-constraint", "new-bucket-name", "kms-key-arn", "ownership-enforce", "route53-vpc", "route53-vpc-lookup", "route53-change", "route53-name", "route53-health", "route53-ksk", "route53-log-group", "route53-policy", "ses-configuration-set-values", "copy-source", "object-kms-key", "ses-send"}
	Readbacks  = []string{"pab-tighten"}
	Deliveries = []string{"ses-mail"}
	// FailureHints are the hand-written failure steps a table may name.
	FailureHints = []string{"bucket-policy-lockout"}
	Forms        = []string{"account-id"}
	// ResultHooks are the hand-written result steps a table may name; see resultHookFields.
	ResultHooks = []string{"dns-records", "bucket-policy-status"}
)

var (
	idPart    = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	argName   = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	opName    = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	shapeName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	tagValue  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// Validate checks a table on its own, before the model is consulted.
func (t Table) Validate() error {
	seg, err := endpoints.ModelOf(t.Segment)
	if err != nil || seg.Model != t.Service || seg.Reserved {
		return fmt.Errorf("table %s: segment %q is not the segment of model %q in the segment table", t.Service, t.Segment, t.Service)
	}
	global, err := endpoints.IsGlobal(endpoints.Service(t.Segment))
	if err != nil {
		return fmt.Errorf("table %s: segment %q has no endpoint", t.Service, t.Segment)
	}
	if want := map[bool]string{true: Global, false: Regional}[global]; t.Geltung != want {
		return fmt.Errorf("table %s: geltung %q, the endpoint table says %q", t.Service, t.Geltung, want)
	}
	seen := map[string]string{}
	mark := func(op, where string) error {
		if !opName.MatchString(op) {
			return fmt.Errorf("table %s: %q is not an operation name", t.Service, op)
		}
		if prior, dup := seen[op]; dup {
			return fmt.Errorf("table %s: operation %s is listed in %s and in %s", t.Service, op, prior, where)
		}
		seen[op] = where
		return nil
	}
	for _, list := range []struct {
		name string
		ops  []Omission
	}{{"exclude", t.Exclude}, {"defer", t.Defer}} {
		for _, o := range list.ops {
			if err := mark(o.Operation, list.name); err != nil {
				return err
			}
			if strings.TrimSpace(o.Reason) == "" {
				return fmt.Errorf("table %s: %s %s has no reason", t.Service, list.name, o.Operation)
			}
		}
	}
	for _, e := range t.URLEncoded {
		if !shapeName.MatchString(e.Shape) || e.MaxLen < 0 {
			return fmt.Errorf("table %s: url_encoded entry %q is not a shape name with a non-negative cap", t.Service, e.Shape)
		}
	}
	pagers := map[string]bool{}
	for _, p := range t.Pagers {
		if !opName.MatchString(p.Operation) || pagers[p.Operation] || len(p.Positions) == 0 || len(p.Positions) > 4 ||
			(p.Truncated != "" && !shapeName.MatchString(p.Truncated)) || (p.PageSize != "" && !shapeName.MatchString(p.PageSize)) {
			return fmt.Errorf("table %s: pager of %q is malformed or repeated", t.Service, p.Operation)
		}
		pagers[p.Operation] = true
		for _, pos := range p.Positions {
			if !shapeName.MatchString(pos.In) || !shapeName.MatchString(pos.Out) {
				return fmt.Errorf("table %s: pager of %s names a malformed member", t.Service, p.Operation)
			}
		}
		if p.StartArguments && !p.Positions[0].Required {
			return fmt.Errorf("table %s: pager of %s: the first position must be required to offer start arguments", t.Service, p.Operation)
		}
		if p.TokenEnds && (p.Truncated != "" || len(p.Positions) != 1) {
			return fmt.Errorf("table %s: pager of %s: a list that ends at a missing token has one position and no truncation member", t.Service, p.Operation)
		}
	}
	for _, p := range t.IDPrefixes {
		if !shapeName.MatchString(p.Shape) || p.Prefix == "" || len(p.Prefix) > 64 || strings.ContainsAny(p.Prefix, " \t\r\n\x00?#") {
			return fmt.Errorf("table %s: id prefix of %q is malformed", t.Service, p.Shape)
		}
	}
	ids := map[string]bool{}
	for _, op := range t.Operations {
		if err := mark(op.Operation, "operations"); err != nil {
			return err
		}
		if err := op.validate(); err != nil {
			return fmt.Errorf("table %s: operation %s: %w", t.Service, op.Operation, err)
		}
		id := op.Object + "." + op.Action
		if ids[id] {
			return fmt.Errorf("table %s: operation %s: tool ID part %s is used twice", t.Service, op.Operation, id)
		}
		ids[id] = true
	}
	return nil
}

func (op Operation) validate() error {
	switch {
	case !idPart.MatchString(op.Object) || !idPart.MatchString(op.Action):
		return fmt.Errorf("object %q and action %q must be lower case words joined by hyphens", op.Object, op.Action)
	case !slices.Contains(Effects, op.Effect):
		return fmt.Errorf("unknown effect %q", op.Effect)
	case !slices.Contains(Idempotency, op.Idempotency):
		return fmt.Errorf("unknown idempotency %q", op.Idempotency)
	case op.Effect != "read" && !op.Confirm:
		return fmt.Errorf("effect %s needs confirm: every operation that is not a read requires confirmation", op.Effect)
	case (op.Effect == "read") != (op.Idempotency == "safe"):
		return fmt.Errorf("effect %s with idempotency %s: a read is safe and nothing else is", op.Effect, op.Idempotency)
	case !slices.Contains(TargetChecks, op.TargetCheck):
		return fmt.Errorf("unknown target_check %q", op.TargetCheck)
	case !slices.Contains(PolicyChecks, op.PolicyCheck):
		return fmt.Errorf("unknown policy_check %q", op.PolicyCheck)
	case (op.TargetCheck == "none") != (len(op.TargetArgs) == 0):
		return fmt.Errorf("target_check %s needs the arguments it checks, and none only without", op.TargetCheck)
	case (op.PolicyCheck == "none") != (len(op.PolicyArgs) == 0):
		return fmt.Errorf("policy_check %s needs the arguments it checks, and none only without", op.PolicyCheck)
	case op.SelfRole != "" && (op.Effect == "read" || !opName.MatchString(op.SelfRole)):
		return fmt.Errorf("self_role %q must name an input member of a tool that is not a read", op.SelfRole)
	case op.ResultHook != "" && !slices.Contains(ResultHooks, op.ResultHook):
		return fmt.Errorf("unknown result_hook %q", op.ResultHook)
	case op.TokenMember != "" && (!opName.MatchString(op.TokenMember) || op.Effect != "create"):
		return fmt.Errorf("token_member %q must name a member of a tool that creates something", op.TokenMember)
	case op.PageSizeMax < 0:
		return fmt.Errorf("negative page size maximum")
	case strings.TrimSpace(op.Title) == "" || strings.TrimSpace(op.Description) == "" || op.DataSensitivity == "":
		return fmt.Errorf("title, description and data sensitivity are required")
	}
	for _, tag := range op.Tags {
		if !tagValue.MatchString(tag) {
			return fmt.Errorf("tag %q is not a lower case word", tag)
		}
	}
	for _, g := range op.Guards {
		if !slices.Contains(Guards, g) {
			return fmt.Errorf("unknown guard %q", g)
		}
	}
	if op.FailureHint != "" && !slices.Contains(FailureHints, op.FailureHint) {
		return fmt.Errorf("unknown failure_hint %q", op.FailureHint)
	}
	switch {
	case op.Readback != "" && !slices.Contains(Readbacks, op.Readback):
		return fmt.Errorf("unknown readback %q", op.Readback)
	case op.Readback != "" && (op.Effect == "read" || strings.TrimSpace(op.ReadbackAnswer) == ""):
		return fmt.Errorf("readback %s belongs to a tool that is not a read and needs the answer of its generated test", op.Readback)
	case op.Readback == "" && op.ReadbackAnswer != "":
		return fmt.Errorf("a readback answer without a readback")
	case op.Delivery != "" && !slices.Contains(Deliveries, op.Delivery):
		return fmt.Errorf("unknown delivery %q", op.Delivery)
	case op.Delivery != "" && op.Idempotency != "non-idempotent":
		return fmt.Errorf("delivery %s belongs to a non-idempotent tool", op.Delivery)
	}
	for _, k := range op.KeepDeprecated {
		if strings.TrimSpace(k.Member) == "" || !strings.Contains(k.Member, ".") || strings.TrimSpace(k.Reason) == "" {
			return fmt.Errorf("keep_deprecated needs a dotted member below the top level and a reason")
		}
	}
	limits := map[string]bool{}
	for _, l := range op.Limits {
		if strings.TrimSpace(l.Member) == "" || strings.TrimSpace(l.Reason) == "" || (l.MaxLen > 0) == (l.MaxItems > 0) ||
			l.MaxLen < 0 || l.MaxItems < 0 || limits[l.Member] {
			return fmt.Errorf("limit %q needs a reason, exactly one positive cap, and no repetition", l.Member)
		}
		limits[l.Member] = true
	}
	releases := map[string]bool{}
	for _, r := range op.Release {
		switch {
		case strings.TrimSpace(r.Member) == "" || strings.TrimSpace(r.Reason) == "":
			return fmt.Errorf("release needs a member and a reason")
		case releases[r.Member]:
			return fmt.Errorf("release %s is listed twice", r.Member)
		case credentialName.MatchString(lastSegment(r.Member)):
			return fmt.Errorf("release %s: the member looks like a credential and is never released", r.Member)
		}
		releases[r.Member] = true
	}
	for _, e := range op.ExcludeEnum {
		if strings.TrimSpace(e.Member) == "" || len(e.Values) == 0 || strings.TrimSpace(e.Reason) == "" {
			return fmt.Errorf("enum exclusion needs a member, values and a reason")
		}
	}
	for _, hidden := range [][]Hidden{op.HiddenInputs, op.HiddenOutputs} {
		for _, h := range hidden {
			if strings.TrimSpace(h.Reason) == "" {
				return fmt.Errorf("hidden member %s has no reason", h.Member)
			}
		}
	}
	if op.CSV != nil {
		if err := op.CSV.validate(); err != nil {
			return err
		}
		if op.Effect != "read" {
			return fmt.Errorf("csv belongs to a read")
		}
	}
	for member, a := range op.Arguments {
		if a.Name != "" && !argName.MatchString(a.Name) {
			return fmt.Errorf("argument %s: name %q is not snake_case", member, a.Name)
		}
		if strings.TrimSpace(a.Description) == "" {
			return fmt.Errorf("argument %s has no description", member)
		}
	}
	for member, f := range op.Fields {
		if f.Name != "" && !argName.MatchString(f.Name) {
			return fmt.Errorf("field %s: name %q is not snake_case", member, f.Name)
		}
		if strings.TrimSpace(f.Description) == "" {
			return fmt.Errorf("field %s has no description", member)
		}
		if f.Form != "" && !slices.Contains(Forms, f.Form) {
			return fmt.Errorf("field %s: unknown form %q", member, f.Form)
		}
		if f.MaxLen < 0 || f.MaxItems < 0 {
			return fmt.Errorf("field %s: negative cap", member)
		}
	}
	return nil
}

func (c CSVReport) validate() error {
	if !opName.MatchString(c.Member) || len(c.Columns) == 0 || len(c.Columns) > 64 ||
		c.MaxBytes < 1 || c.MaxRows < 1 || c.MaxField < 1 || c.DefaultPage < 1 || c.MaxPage < c.DefaultPage || c.MaxPage > c.MaxRows {
		return fmt.Errorf("csv description is incomplete or its limits disagree")
	}
	seen := map[string]bool{}
	for _, col := range c.Columns {
		if !argName.MatchString(col.Name) || seen[col.Name] {
			return fmt.Errorf("csv column %q is not a snake_case name or is listed twice", col.Name)
		}
		seen[col.Name] = true
	}
	return nil
}
