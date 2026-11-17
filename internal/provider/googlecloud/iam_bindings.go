package googlecloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	iamBindingsAddID    = Provider + ".iam-bindings.add-member"
	iamBindingsRemoveID = Provider + ".iam-bindings.remove-member"
	iamBindingsChangeID = Provider + ".iam-bindings.change-role"
)

// bindingRisk is the risk of the binding tools: each changes one binding of an IAM policy, needs the
// confirmation of a person, and repeating it leaves the policy as the first call left it.
var bindingRisk = capability.Risk{
	Effect:          capability.EffectUpdate,
	Idempotency:     capability.IdempotencyIdempotent,
	Confirmation:    capability.ConfirmationRequired,
	OpenWorld:       true,
	DataSensitivity: accessSensitivity,
}

// Bounds of the binding arguments. They are Qatlas bounds, wide enough for what Google sends, so that an
// existing binding can always be named exactly.
const (
	maxMemberText     = 1024
	maxRoleText       = 300
	maxConditionTitle = 256
	maxConditionExpr  = 12800
	maxConditionDesc  = 1024
)

const (
	memberAllUsers          = "allUsers"
	memberAllAuthenticated  = "allAuthenticatedUsers"
	memberDeletedPrefix     = "deleted:"
	memberServiceAccountPfx = "serviceAccount:"
)

// The three forms of a service account of one project that may be granted although the policy does not hold
// them yet: an account created in the project, and the Compute Engine and App Engine default accounts. The
// patterns are exact and lowercase, as Google writes service account emails; any other spelling is refused.
var (
	projectAccountMember = regexp.MustCompile(`^serviceAccount:[a-z][a-z0-9-]{4,28}[a-z0-9]@([a-z][a-z0-9-]{4,28}[a-z0-9])\.iam\.gserviceaccount\.com$`)
	computeAccountMember = regexp.MustCompile(`^serviceAccount:([0-9]{1,20})-compute@developer\.gserviceaccount\.com$`)
	appspotAccountMember = regexp.MustCompile(`^serviceAccount:([a-z][a-z0-9-]{4,28}[a-z0-9])@appspot\.gserviceaccount\.com$`)
)

// The changes a binding tool makes.
const (
	changeAdd    = "add"
	changeRemove = "remove"
	changeMove   = "move"
)

type conditionArgument struct {
	Title       string `json:"title"`
	Expression  string `json:"expression"`
	Description string `json:"description"`
}

type bindingArguments struct {
	policyArguments
	Role      string             `json:"role"`
	FromRole  string             `json:"from_role"`
	ToRole    string             `json:"to_role"`
	Member    string             `json:"member"`
	Condition *conditionArgument `json:"condition"`
}

func checkRole(name, value string) error {
	if len(value) > maxRoleText || !(predefinedRoleName.MatchString(value) || projectRoleName.MatchString(value) ||
		organizationRole.MatchString(value)) {
		return badRequest(name + " must be a role name: roles/ID, projects/PROJECT/roles/ID, or " +
			"organizations/ORGANIZATION/roles/ID")
	}
	return nil
}

// checkMember accepts the form of a principal identifier. It never rewrites the value: the member is compared
// with the policy as it is written.
func checkMember(value string) error {
	bad := strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == utf8.RuneError
	}) >= 0
	if value == "" || len(value) > maxMemberText || !utf8.ValidString(value) || bad ||
		(!strings.Contains(value, ":") && value != memberAllUsers && value != memberAllAuthenticated) {
		return badRequest("member must be a principal identifier such as user:EMAIL, group:EMAIL, or " +
			"serviceAccount:EMAIL, without spaces")
	}
	return nil
}

// neverGranted reports the members no tool grants, even when the policy already holds them. It ignores case
// so that no spelling of them slips through; it only ever refuses more.
func neverGranted(member string) bool {
	return strings.EqualFold(member, memberAllUsers) || strings.EqualFold(member, memberAllAuthenticated) ||
		(len(member) >= len(memberDeletedPrefix) && strings.EqualFold(member[:len(memberDeletedPrefix)], memberDeletedPrefix))
}

func checkCondition(c *conditionArgument) error {
	if c == nil {
		return nil
	}
	if c.Title == "" || c.Expression == "" || !utf8.ValidString(c.Title) || !utf8.ValidString(c.Expression) ||
		!utf8.ValidString(c.Description) || utf8.RuneCountInString(c.Title) > maxConditionTitle ||
		utf8.RuneCountInString(c.Expression) > maxConditionExpr || utf8.RuneCountInString(c.Description) > maxConditionDesc {
		return badRequest("condition needs a title of at most 256 characters and an expression of at most 12800 " +
			"characters; its description holds at most 1024")
	}
	return nil
}

// check refuses before any I/O what no policy can make valid.
func (a bindingArguments) check(change string) error {
	if err := checkMember(a.Member); err != nil {
		return err
	}
	if err := checkCondition(a.Condition); err != nil {
		return err
	}
	if change == changeMove {
		if err := checkRole("from_role", a.FromRole); err != nil {
			return err
		}
		if err := checkRole("to_role", a.ToRole); err != nil {
			return err
		}
		if a.FromRole == a.ToRole {
			return badRequest("from_role and to_role must differ")
		}
	} else if err := checkRole("role", a.Role); err != nil {
		return err
	}
	if change != changeRemove && neverGranted(a.Member) {
		return refuseNeverGranted()
	}
	return nil
}

func refuseNeverGranted() error {
	return badRequest("allUsers, allAuthenticatedUsers, and deleted: members are never granted; only " +
		"remove-member takes them")
}

// --- the policy as read ---

// readPolicy is a policy as Google sent it, kept as raw JSON so that writing it back changes nothing but the
// bindings the tool changes: every other member of the policy and every other binding go back byte for byte.
type readPolicy struct {
	fields   map[string]json.RawMessage
	bindings []readBinding
	etag     string
	version  int
}

type readBinding struct {
	raw       json.RawMessage
	fields    map[string]json.RawMessage
	role      string
	condition *exprWire
	members   []json.RawMessage
	texts     []string
}

func parsePolicy(op string, data json.RawMessage) (readPolicy, error) {
	var p readPolicy
	if json.Unmarshal(data, &p.fields) != nil || p.fields == nil {
		return readPolicy{}, invalidRow(op)
	}
	// Without an etag the write would replace whatever the policy holds by then.
	if json.Unmarshal(p.fields["etag"], &p.etag) != nil || p.etag == "" {
		return readPolicy{}, invalidRow(op)
	}
	if raw, ok := p.fields["version"]; ok && json.Unmarshal(raw, &p.version) != nil {
		return readPolicy{}, invalidRow(op)
	}
	var bindings []json.RawMessage
	if raw, ok := p.fields["bindings"]; ok && json.Unmarshal(raw, &bindings) != nil {
		return readPolicy{}, invalidRow(op)
	}
	for _, raw := range bindings {
		b := readBinding{raw: raw}
		if json.Unmarshal(raw, &b.fields) != nil || b.fields == nil ||
			json.Unmarshal(b.fields["role"], &b.role) != nil || b.role == "" {
			return readPolicy{}, invalidRow(op)
		}
		if raw, ok := b.fields["condition"]; ok && json.Unmarshal(raw, &b.condition) != nil {
			return readPolicy{}, invalidRow(op)
		}
		if raw, ok := b.fields["members"]; ok && json.Unmarshal(raw, &b.members) != nil {
			return readPolicy{}, invalidRow(op)
		}
		b.texts = make([]string, len(b.members))
		for i, member := range b.members {
			if json.Unmarshal(member, &b.texts[i]) != nil {
				return readPolicy{}, invalidRow(op)
			}
		}
		p.bindings = append(p.bindings, b)
	}
	return p, nil
}

// sameCondition compares a condition of the policy with the argument field by field and exactly. A binding
// with a location, which no argument sets, is never the one meant.
func sameCondition(e *exprWire, c *conditionArgument) bool {
	if e == nil || c == nil {
		return e == nil && c == nil
	}
	return e.Expression == c.Expression && e.Title == c.Title && e.Description == c.Description && e.Location == ""
}

// find returns the index of the binding of role and condition, or -1. Two such bindings leave it open which one
// is meant, so the policy is not changed.
func (p readPolicy) find(op, role string, c *conditionArgument) (int, error) {
	found := -1
	for i, b := range p.bindings {
		if b.role == role && sameCondition(b.condition, c) {
			if found >= 0 {
				return -1, provider.InvalidResponse(op, subject+" returned a policy with two bindings of the same "+
					"role and condition; change it in the Cloud console")
			}
			found = i
		}
	}
	return found, nil
}

func (p readPolicy) has(i int, member string) bool {
	return i >= 0 && slices.Contains(p.bindings[i].texts, member)
}

// holds reports whether any binding of the policy names exactly this member text.
func (p readPolicy) holds(member string) bool {
	return slices.ContainsFunc(p.bindings, func(b readBinding) bool { return slices.Contains(b.texts, member) })
}

// --- admission ---

// admitMember is the rule of Entscheidung 4 for a member that is to be granted, applied to the policy just read:
// the exact member text already appears in a binding of the policy, or, on a project only, it is a service
// account of that project that exists. Everything else, a new user:, group:, domain:, principal:, or
// principalSet: identity among it, is refused, and allUsers, allAuthenticatedUsers, and deleted: members always.
func admitMember(ctx context.Context, c *client, op, tool string, r iamResource, p readPolicy, member string) error {
	if neverGranted(member) {
		return refuseNeverGranted()
	}
	if p.holds(member) {
		return nil
	}
	if r.kind == kindProject {
		admitted, err := projectAccount(ctx, c, op, tool, r.id, member)
		if err != nil || admitted {
			return err
		}
		return badRequest("member is not admitted: only a principal that already appears in the policy of the " +
			"resource, or an existing service account of the same project, is granted a role")
	}
	return badRequest("member is not admitted: only a principal that already appears in the policy of the " +
		"resource is granted a role on a folder or an organization")
}

// projectAccount reports whether member is a service account of the project: its address names the project
// (ID, or number for the Compute Engine default account, which projects.get proves), and serviceAccounts.get
// finds it in that project. A member of another form, or of another project, is not.
func projectAccount(ctx context.Context, c *client, op, tool, projectRef, member string) (bool, error) {
	email, ok := strings.CutPrefix(member, memberServiceAccountPfx)
	if !ok || !(projectAccountMember.MatchString(member) || computeAccountMember.MatchString(member) ||
		appspotAccountMember.MatchString(member)) {
		return false, nil
	}
	project, err := c.project(ctx, op, tool, projectRef)
	if err != nil {
		return false, err
	}
	id, number := project.ProjectID, strings.TrimPrefix(project.Name, prefixProject)
	if !projectIDShape.MatchString(id) || !numberPattern.MatchString(number) || (projectRef != id && projectRef != number) {
		return false, invalidRow(op)
	}
	var named string
	if m := projectAccountMember.FindStringSubmatch(member); m != nil {
		named = m[1]
	} else if m := appspotAccountMember.FindStringSubmatch(member); m != nil {
		named = m[1]
	} else if m := computeAccountMember.FindStringSubmatch(member); m != nil && m[1] == number {
		named = id
	}
	if named != id {
		return false, nil
	}
	var row serviceAccountRow
	if err := c.get(ctx, op, tool, serviceIAM, accountPath(id, email), nil, &row); err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) && failed.Class == provider.ClassNotFound {
			return false, badRequest("member is not admitted: the service account does not exist in the project")
		}
		return false, err
	}
	if row.check(op, id) != nil || row.Email != email {
		return false, invalidRow(op)
	}
	return true, nil
}

// --- the written policy ---

// jsonText encodes a string as JSON without HTML escaping, so that the text goes to Google as it was given.
func jsonText(value string) json.RawMessage {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// jsonObject writes the members in a fixed order; their values go out as they are.
func jsonObject(fields map[string]json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range slices.Sorted(maps.Keys(fields)) {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(jsonText(key))
		buf.WriteByte(':')
		buf.Write(fields[key])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

func jsonArray(items []json.RawMessage) json.RawMessage {
	buf := []byte{'['}
	for i, item := range items {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, item...)
	}
	return append(buf, ']')
}

// withMembers is the binding with another member list; role, condition, and anything else stay as read.
func (b readBinding) withMembers(members []json.RawMessage) json.RawMessage {
	fields := maps.Clone(b.fields)
	fields["members"] = jsonArray(members)
	return jsonObject(fields)
}

func newBinding(role, member string, c *conditionArgument) json.RawMessage {
	fields := map[string]json.RawMessage{"role": jsonText(role), "members": jsonArray([]json.RawMessage{jsonText(member)})}
	if c != nil {
		condition := map[string]json.RawMessage{"title": jsonText(c.Title), "expression": jsonText(c.Expression)}
		if c.Description != "" {
			condition["description"] = jsonText(c.Description)
		}
		fields["condition"] = jsonObject(condition)
	}
	return jsonObject(fields)
}

// edit is one change of the bindings: the binding at index (or a new one of role and condition when index is
// -1) gains or loses member.
type edit struct {
	index  int
	role   string
	member string
	add    bool
}

// apply returns the setIamPolicy body: the policy as read with the edits applied. A binding left without members
// is dropped. version becomes 3 when a binding with a condition remains, as Google requires; etag,
// auditConfigs, and every untouched binding go back unchanged.
func (p readPolicy) apply(edits []edit, c *conditionArgument) []byte {
	bindings := make([]json.RawMessage, 0, len(p.bindings)+1)
	conditional := false
	for i, b := range p.bindings {
		members := b.members
		touched := false
		for _, e := range edits {
			if e.index != i {
				continue
			}
			touched = true
			if e.add {
				members = append(slices.Clip(members), jsonText(e.member))
				continue
			}
			kept := make([]json.RawMessage, 0, len(members))
			for j, m := range members {
				if b.texts[j] != e.member {
					kept = append(kept, m)
				}
			}
			members = kept
		}
		if !touched {
			bindings = append(bindings, b.raw)
			conditional = conditional || b.condition != nil
			continue
		}
		if len(members) > 0 {
			bindings = append(bindings, b.withMembers(members))
			conditional = conditional || b.condition != nil
		}
	}
	for _, e := range edits {
		if e.index < 0 && e.add {
			bindings = append(bindings, newBinding(e.role, e.member, c))
			conditional = conditional || c != nil
		}
	}
	fields := maps.Clone(p.fields)
	fields["bindings"] = jsonArray(bindings)
	if conditional && p.version != 3 {
		fields["version"] = json.RawMessage("3")
	}
	return jsonObject(map[string]json.RawMessage{"policy": jsonObject(fields)})
}

// --- the result ---

type bindingChangeResult struct {
	Resource    string         `json:"resource"`
	Changed     bool           `json:"changed"`
	Binding     bindingResult  `json:"binding"`
	FromBinding *bindingResult `json:"from_binding,omitempty"`
	Version     *int           `json:"version,omitempty"`
	Etag        string         `json:"etag,omitempty"`
	Truncated   bool           `json:"truncated,omitempty"`
}

// bindingOf returns the binding of role and condition of a policy, cut, or an empty one when the policy holds
// none. contains says whether it names member.
func bindingOf(cut *cutter, bindings []bindingWire, role, member string, c *conditionArgument) (bindingResult, bool) {
	out := bindingResult{Role: cut.text(role, maxNameText), Members: []string{}}
	if c != nil {
		out.Condition = &exprResult{Expression: cut.text(c.Expression, maxCondition), Title: cut.text(c.Title, maxNameText),
			Description: cut.text(c.Description, maxText)}
	}
	for _, b := range bindings {
		if b.Role == role && sameCondition(b.Condition, c) {
			if members := cut.list(b.Members, maxIAMList); members != nil {
				out.Members = members
			}
			return out, slices.Contains(b.Members, member)
		}
	}
	return out, false
}

// describe builds the answer from a policy, the one read for a call that changed nothing, or the one Google
// answered the write with. A written policy that does not show the change leaves the outcome unknown.
func describe(op string, policy policyWire, r iamResource, a bindingArguments, change string, changed bool) (bindingChangeResult, error) {
	var cut cutter
	out := bindingChangeResult{Resource: r.name(), Changed: changed, Version: policy.Version,
		Etag: cut.text(policy.Etag, maxNameText)}
	role := a.Role
	if change == changeMove {
		role = a.ToRole
	}
	binding, present := bindingOf(&cut, policy.Bindings, role, a.Member, a.Condition)
	out.Binding = binding
	shown := present == (change != changeRemove)
	if change == changeMove {
		from, stays := bindingOf(&cut, policy.Bindings, a.FromRole, a.Member, a.Condition)
		out.FromBinding = &from
		shown = shown && !stays
	}
	if changed && !shown {
		return bindingChangeResult{}, provider.InvalidResponse(op, subject+" answered the change with a policy that "+
			"does not show it"+unsureNote)
	}
	out.Truncated = cut.cut
	return out, nil
}

// --- the course every binding tool shares ---

func changeBinding(ctx context.Context, op, tool, change string, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	var a bindingArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.check(change); err != nil {
		return nil, err
	}
	c, r, err := openResource(ctx, op, tool, resolved, secrets, red, a.policyArguments,
		kindProject, kindFolder, kindOrganization)
	if err != nil {
		return nil, err
	}
	svc, base := r.service()
	path := base + r.escaped()
	var data json.RawMessage
	if err := c.post(ctx, op, tool, svc, path+":getIamPolicy", nil,
		map[string]any{"options": map[string]int{"requestedPolicyVersion": 3}}, &data); err != nil {
		return nil, err
	}
	policy, err := parsePolicy(op, data)
	if err != nil {
		return nil, err
	}
	var read policyWire
	if err := json.Unmarshal(data, &read); err != nil {
		return nil, invalidRow(op)
	}
	var edits []edit
	switch change {
	case changeAdd:
		i, err := policy.find(op, a.Role, a.Condition)
		if err != nil {
			return nil, err
		}
		if policy.has(i, a.Member) {
			return describe(op, read, r, a, change, false)
		}
		edits = []edit{{index: i, role: a.Role, member: a.Member, add: true}}
	case changeRemove:
		i, err := policy.find(op, a.Role, a.Condition)
		if err != nil {
			return nil, err
		}
		if !policy.has(i, a.Member) {
			return describe(op, read, r, a, change, false)
		}
		edits = []edit{{index: i, member: a.Member}}
	case changeMove:
		from, err := policy.find(op, a.FromRole, a.Condition)
		if err != nil {
			return nil, err
		}
		to, err := policy.find(op, a.ToRole, a.Condition)
		if err != nil {
			return nil, err
		}
		if !policy.has(from, a.Member) {
			if policy.has(to, a.Member) {
				return describe(op, read, r, a, change, false)
			}
			return nil, badRequest("member is not in the binding of from_role with this condition")
		}
		edits = []edit{{index: from, member: a.Member}}
		if !policy.has(to, a.Member) {
			edits = append(edits, edit{index: to, role: a.ToRole, member: a.Member, add: true})
		}
	}
	if change != changeRemove {
		if err := admitMember(ctx, c, op, tool, r, policy, a.Member); err != nil {
			return nil, err
		}
	}
	var written policyWire
	if err := c.mutate(ctx, op, tool, svc, path+":setIamPolicy", policy.apply(edits, a.Condition), &written); err != nil {
		return nil, err
	}
	return describe(op, written, r, a, change, true)
}

func handleIAMBindingsAdd(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return changeBinding(ctx, "add iam binding member", iamBindingsAddID, changeAdd, resolved, secrets, red, raw)
}

func handleIAMBindingsRemove(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return changeBinding(ctx, "remove iam binding member", iamBindingsRemoveID, changeRemove, resolved, secrets, red, raw)
}

func handleIAMBindingsChange(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return changeBinding(ctx, "change iam binding role", iamBindingsChangeID, changeMove, resolved, secrets, red, raw)
}

// bindingSummary phrases the confirmation from the arguments alone: resource, role, member, and the change.
func bindingSummary(change string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a bindingArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		resource := iamResource{kind: a.ResourceType, id: a.ResourceID}.name()
		condition := ""
		if a.Condition != nil {
			condition = " with the condition " + strconv.Quote(a.Condition.Title)
		}
		var out string
		switch change {
		case changeAdd:
			out = "Grant " + a.Role + condition + " to " + a.Member + " on " + resource
		case changeRemove:
			out = "Remove " + a.Member + " from " + a.Role + condition + " on " + resource
		default:
			out = "Move " + a.Member + " from " + a.FromRole + " to " + a.ToRole + condition + " on " + resource
		}
		if change != changeRemove && (a.Role == "roles/owner" || a.ToRole == "roles/owner") {
			out += "; roles/owner gives full control of the resource, its IAM policy included"
		}
		return out
	}
}

// --- descriptors ---

const (
	roleProp      = `{"type":"string","minLength":1,"maxLength":300}`
	memberProp    = `"member":{"type":"string","minLength":1,"maxLength":1024}`
	conditionProp = `"condition":{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":256},` +
		`"expression":{"type":"string","minLength":1,"maxLength":12800},"description":{"type":"string","maxLength":1024}},` +
		`"required":["title","expression"],"additionalProperties":false}`
	bindingSchema = `{"type":"object","properties":{"role":{"type":"string"},"members":` + stringArraySchema +
		`,"condition":` + exprSchema + `},"required":["role","members"],"additionalProperties":false}`
	bindingChangeSchema = `{"type":"object","properties":{"resource":{"type":"string"},"changed":{"type":"boolean"},` +
		`"binding":` + bindingSchema + `,"from_binding":` + bindingSchema + `,"version":{"type":"integer"},` +
		`"etag":{"type":"string"},"truncated":{"type":"boolean"}},"required":["resource","changed","binding"],` +
		`"additionalProperties":false}`
)

const bindingNote = " Qatlas reads the policy with version 3, changes the one binding of role and condition in " +
	"memory, and writes the whole policy back with the etag it read, so every other binding, the audit configs, " +
	"and the version stay as they were; a condition is compared exactly, field by field. If the policy changed " +
	"in between, Google refuses the write and the call fails with a conflict without repeating it: read again. " +
	"The resource must lie in the targets of the connection, which Qatlas proves before the policy is read; an " +
	"organization only on a connection that names it. Needs the confirmation of a person for each call. Members " +
	"and conditions are untrusted third-party text; the answer holds the changed binding, cut, the new etag, and " +
	"the version, not the whole policy."

const admissionNote = " A member is granted only if exactly this member text (type prefix and value) already " +
	"appears in a binding of the policy of the resource, or, on a project only, if it is an existing service " +
	"account of that project (NAME@PROJECT_ID.iam.gserviceaccount.com, or the default account PROJECT_NUMBER-compute " +
	"in the domain developer.gserviceaccount.com or PROJECT_ID in the domain appspot.gserviceaccount.com, checked " +
	"with serviceAccounts.get). " +
	"allUsers, allAuthenticatedUsers, and deleted: members are never granted."

var (
	memberArgument = capability.Argument{Name: "member", Required: true, Description: "Principal exactly as the " +
		"policy writes it, such as user:EMAIL, group:EMAIL, serviceAccount:EMAIL, or domain:DOMAIN; compared " +
		"as text, without changing case or spaces"}
	conditionArgumentDoc = capability.Argument{Name: "condition", Description: "Condition of the binding, " +
		"{title, expression, description}; absent names the binding without a condition. It must match the " +
		"condition of the policy exactly, description included"}
	bindingFields = []capability.Field{
		resourceField,
		{Name: "changed", Description: "False when the policy already was as asked and nothing was written"},
		{Name: "binding", Description: "The binding of role (to_role for change-role) and condition after the " +
			"call, {role, members, condition}; members is empty when the binding no longer exists"},
		{Name: "version", Description: "Format version of the policy after the call"},
		{Name: "etag", Description: "Entity tag of the policy after the call"},
		truncatedField,
	}
)

var iamBindingsAdd = capability.Descriptor{
	ID: iamBindingsAddID, Version: 1, Title: "Add a member to a Google Cloud IAM binding",
	Description: "Grant a role to a member on a project, folder, or organization by adding the member to the " +
		"binding of that role and condition, which is created when it does not exist. A member the binding " +
		"already holds is a success without a write." + admissionNote + bindingNote,
	Tags: []string{"google", "cloud", "iam", "bindings"}, Risk: bindingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + hierarchyTypes + `,` + resourceIDProp +
		`,"role":` + roleProp + `,` + memberProp + `,` + conditionProp + `},` +
		`"required":["resource_type","resource_id","role","member"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(bindingChangeSchema),
	Arguments: []capability.Argument{
		bindingResourceTypeArgument, bindingResourceIDArgument,
		{Name: "role", Required: true, Description: "Role to grant: roles/ID, projects/PROJECT/roles/ID, or " +
			"organizations/ORGANIZATION/roles/ID"},
		memberArgument, conditionArgumentDoc,
	},
	Fields: bindingFields,
	Examples: []capability.Example{{
		Description: "Grant a service account of the project a role",
		Arguments: json.RawMessage(`{"resource_type":"project","resource_id":"example-project-123",` +
			`"role":"roles/storage.objectViewer","member":"serviceAccount:deploy@example-project-123.iam.gserviceaccount.com"}`),
	}},
}

var iamBindingsRemove = capability.Descriptor{
	ID: iamBindingsRemoveID, Version: 1, Title: "Remove a member from a Google Cloud IAM binding",
	Description: "Revoke a role from a member on a project, folder, or organization by removing the member " +
		"from the binding of that role and condition; a binding left without members is dropped. Any member " +
		"may be removed, allUsers and allAuthenticatedUsers included. A member the binding does not hold is a " +
		"success without a write." + bindingNote,
	Tags: []string{"google", "cloud", "iam", "bindings"}, Risk: bindingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + hierarchyTypes + `,` + resourceIDProp +
		`,"role":` + roleProp + `,` + memberProp + `,` + conditionProp + `},` +
		`"required":["resource_type","resource_id","role","member"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(bindingChangeSchema),
	Arguments: []capability.Argument{
		bindingResourceTypeArgument, bindingResourceIDArgument,
		{Name: "role", Required: true, Description: "Role of the binding: roles/ID, projects/PROJECT/roles/ID, " +
			"or organizations/ORGANIZATION/roles/ID"},
		memberArgument, conditionArgumentDoc,
	},
	Fields: bindingFields,
	Examples: []capability.Example{{
		Description: "Remove public read access from a project",
		Arguments: json.RawMessage(`{"resource_type":"project","resource_id":"example-project-123",` +
			`"role":"roles/viewer","member":"allUsers"}`),
	}},
}

var iamBindingsChange = capability.Descriptor{
	ID: iamBindingsChangeID, Version: 1, Title: "Move a member to another Google Cloud IAM role",
	Description: "Move a member from the binding of from_role to the binding of to_role, both with the same " +
		"condition, in one write; the to_role binding is created when it does not exist, and a from_role " +
		"binding left without members is dropped. The member must be in the from_role binding; a member that " +
		"is already only in to_role is a success without a write." + admissionNote + bindingNote,
	Tags: []string{"google", "cloud", "iam", "bindings"}, Risk: bindingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + hierarchyTypes + `,` + resourceIDProp +
		`,"from_role":` + roleProp + `,"to_role":` + roleProp + `,` + memberProp + `,` + conditionProp + `},` +
		`"required":["resource_type","resource_id","from_role","to_role","member"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(bindingChangeSchema),
	Arguments: []capability.Argument{
		bindingResourceTypeArgument, bindingResourceIDArgument,
		{Name: "from_role", Required: true, Description: "Role the member holds now"},
		{Name: "to_role", Required: true, Description: "Role the member gets instead; it must differ from from_role"},
		memberArgument, conditionArgumentDoc,
	},
	Fields: append(slices.Clone(bindingFields), capability.Field{Name: "from_binding", Description: "The " +
		"binding of from_role and condition after the call, {role, members, condition}"}),
	Examples: []capability.Example{{
		Description: "Move a member from editor to viewer",
		Arguments: json.RawMessage(`{"resource_type":"project","resource_id":"example-project-123",` +
			`"from_role":"roles/editor","to_role":"roles/viewer","member":"user:alice@example.com"}`),
	}},
}

var (
	bindingResourceTypeArgument = capability.Argument{Name: "resource_type", Required: true, Description: "Kind " +
		"of resource whose policy changes: project, folder, or organization. An organization is changed only " +
		"by a connection that names it among its targets"}
	bindingResourceIDArgument = capability.Argument{Name: "resource_id", Required: true, Description: "ID of " +
		"the resource: a project ID or number, or the numeric ID of a folder or an organization; never a " +
		"resource name or a path"}
)
