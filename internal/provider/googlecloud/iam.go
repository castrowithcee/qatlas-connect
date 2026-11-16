package googlecloud

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// accessSensitivity classifies results that say who may do what in a cloud account.
const accessSensitivity = "access-control-data"

// iamRisk is the risk of the IAM tools. They read through POST (getIamPolicy and testIamPermissions change
// nothing and may be repeated) and return principals, which are personal data.
var iamRisk = capability.Risk{
	Effect:          capability.EffectRead,
	Idempotency:     capability.IdempotencySafe,
	Confirmation:    capability.ConfirmationNone,
	OpenWorld:       true,
	DataSensitivity: accessSensitivity,
}

// Bounds of IAM answers. The size of the response is bounded as well, so these only keep a result readable.
const (
	maxIAMList     = 500
	maxIAMBindings = 500
	maxIAMRules    = 100
	maxCondition   = 4096
)

// The kinds of resource an IAM tool names. A service account belongs to a project that Qatlas learns from
// Google, never from the caller; the other three are the resources of the hierarchy.
const kindServiceAccount = "service-account"

var (
	// Service account emails end in a subdomain of gserviceaccount.com: the iam subdomain of the project, or
	// the appspot and developer domains of Google's default service accounts.
	serviceAccountPattern = regexp.MustCompile(`^([0-9]{1,30}|[A-Za-z0-9][A-Za-z0-9._-]{0,63}@[a-z0-9][a-z0-9.-]{0,100}\.gserviceaccount\.com)$`)
	policyIDPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
)

// iamResource is a resource whose IAM state a tool reads, built from structured arguments only.
type iamResource struct {
	kind, id, project string
}

// name is the resource name Google uses for the resource.
func (r iamResource) name() string {
	switch r.kind {
	case kindOrganization:
		return prefixOrganization + r.id
	case kindFolder:
		return prefixFolder + r.id
	case kindServiceAccount:
		return prefixProject + r.project + "/serviceAccounts/" + r.id
	}
	return prefixProject + r.id
}

// escaped is the resource name with every segment escaped, for a request path.
func (r iamResource) escaped() string {
	switch r.kind {
	case kindOrganization:
		return prefixOrganization + url.PathEscape(r.id)
	case kindFolder:
		return prefixFolder + url.PathEscape(r.id)
	case kindServiceAccount:
		return prefixProject + url.PathEscape(r.project) + "/serviceAccounts/" + url.PathEscape(r.id)
	}
	return prefixProject + url.PathEscape(r.id)
}

// service names the Google service that owns the IAM methods of the resource.
func (r iamResource) service() (service, string) {
	if r.kind == kindServiceAccount {
		return serviceIAM, "/v1/"
	}
	return serviceResourceManager, "/v3/"
}

// attachmentPoint is the full resource name the deny policies of IAM v2 are attached to, as one path segment.
func (r iamResource) attachmentPoint() string {
	return url.PathEscape("cloudresourcemanager.googleapis.com/" + r.name())
}

// policyArguments are the arguments that name a resource, for every IAM tool.
type policyArguments struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

// resourceOf builds the resource from the arguments and refuses what the targets already decide, before any
// secret is read. kinds are the resource types the tool offers.
func resourceOf(a policyArguments, list allowlist, kinds ...string) (iamResource, error) {
	r := iamResource{kind: a.ResourceType, id: a.ResourceID}
	known := false
	for _, kind := range kinds {
		known = known || kind == r.kind
	}
	if !known {
		return r, badRequest("resource_type must be one of " + strings.Join(kinds, ", "))
	}
	switch r.kind {
	case kindProject:
		if !validProject(r.id) {
			return r, badRequest("resource_id must be a project ID or a project number")
		}
	case kindFolder, kindOrganization:
		if !numberPattern.MatchString(r.id) {
			return r, badRequest("resource_id must be the numeric ID of the " + r.kind)
		}
	case kindServiceAccount:
		if !serviceAccountPattern.MatchString(r.id) {
			return r, badRequest("resource_id must be the email or the numeric unique ID of the service account")
		}
		// Google resolves the account across projects with the placeholder -, so the project of the request
		// is the one the proof read names, or - without targets.
		r.project = "-"
	}
	switch r.kind {
	case kindOrganization:
		// Only a binding that names the organization reaches it, an unbound connection included.
		if !list.has(kindOrganization, r.id) {
			return r, refuseOutside("the resource")
		}
	case kindFolder:
		if list.bound() && !list.has(kindFolder, r.id) && !list.hasKind(kindOrganization, kindFolder) {
			return r, refuseOutside("the resource")
		}
	case kindProject:
		if allowed, decided := list.decidesProject(r.id); decided && !allowed {
			return r, refuseOutside("the resource")
		}
	}
	return r, nil
}

// serviceAccountWire is the part of a ServiceAccount of IAM v1 that proves where the account lives.
type serviceAccountWire struct {
	Name      string `json:"name"`
	ProjectID string `json:"projectId"`
	Email     string `json:"email"`
	UniqueID  string `json:"uniqueId"`
}

// prove reads the service account across projects and returns the project that owns it. The answer must be
// the account that was asked for.
func (r iamResource) prove(ctx context.Context, c *client, op, tool string) (string, error) {
	var account serviceAccountWire
	if err := c.get(ctx, op, tool, serviceIAM, "/v1/projects/-/serviceAccounts/"+url.PathEscape(r.id), nil,
		&account); err != nil {
		return "", err
	}
	if (account.Email != r.id && account.UniqueID != r.id) || !validProject(account.ProjectID) ||
		!strings.HasPrefix(account.Name, prefixProject+account.ProjectID+"/serviceAccounts/") {
		return "", invalidRow(op)
	}
	return account.ProjectID, nil
}

// admit binds the resource to the targets where the targets alone did not decide, with the reading of the
// project or folder and the folders above it. A service account is bound through its project, which one
// reading of the account proves; without targets that reading is not needed.
func (r *iamResource) admit(ctx context.Context, c *client, list allowlist, op, tool string) error {
	if !list.bound() {
		return nil
	}
	gate := newAdmitter(list, c, op, tool)
	switch r.kind {
	case kindOrganization:
		return nil
	case kindFolder:
		if list.has(kindFolder, r.id) {
			return nil
		}
		folder, err := c.folder(ctx, op, tool, r.id)
		if err != nil {
			return err
		}
		admitted, err := gate.folder(ctx, folder)
		if err != nil {
			return err
		}
		if !admitted {
			return refuseOutside("the resource")
		}
		return nil
	}
	owner := r.id
	if r.kind == kindServiceAccount {
		var err error
		if owner, err = r.prove(ctx, c, op, tool); err != nil {
			return err
		}
		r.project = owner
	}
	allowed, decided := list.decidesProject(owner)
	if decided && !allowed {
		return refuseOutside("the resource")
	}
	if decided {
		return nil
	}
	project, err := c.project(ctx, op, tool, owner)
	if err != nil {
		return err
	}
	admitted, err := gate.project(ctx, project)
	if err != nil {
		return err
	}
	if !admitted {
		return refuseOutside("the resource")
	}
	return nil
}

// openResource is the course every IAM tool shares: the connection and the targets, the resource, and only then
// the secret, the client, and the binding reads.
func openResource(ctx context.Context, op, tool string, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, a policyArguments, kinds ...string) (*client, iamResource, error) {
	if err := checkConnection(resolved); err != nil {
		return nil, iamResource{}, err
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return nil, iamResource{}, failure(op, "the targets of the connection are not usable")
	}
	if !list.reachesResources() {
		return nil, iamResource{}, badRequest("the targets of this connection name no organization, folder, or project")
	}
	r, err := resourceOf(a, list, kinds...)
	if err != nil {
		return nil, iamResource{}, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, iamResource{}, err
	}
	if err := r.admit(ctx, c, list, op, tool); err != nil {
		return nil, iamResource{}, err
	}
	return c, r, nil
}

// --- shared descriptor parts ---

const (
	resourceTypeProp = `"resource_type":{"type":"string","enum":["project","folder","organization","service-account"]}`
	hierarchyTypes   = `"resource_type":{"type":"string","enum":["project","folder","organization"]}`
	resourceIDProp   = `"resource_id":{"type":"string","minLength":1,"maxLength":128}`
)

var (
	policyResourceTypeArgument = capability.Argument{Name: "resource_type", Required: true, Description: "Kind of " +
		"resource: project, folder, organization, or service-account. An organization is read only by a " +
		"connection that names it among its targets"}
	hierarchyTypeArgument = capability.Argument{Name: "resource_type", Required: true, Description: "Kind of " +
		"resource the policies are attached to: project, folder, or organization. An organization is read only " +
		"by a connection that names it among its targets"}
	policyResourceIDArgument = capability.Argument{Name: "resource_id", Required: true, Description: "ID of the " +
		"resource: a project ID or number, the numeric ID of a folder or an organization, or for a " +
		"service-account its email or numeric unique ID; never a resource name or a path"}
	resourceField = capability.Field{Name: "resource", Description: "Resource name Qatlas built from the " +
		"arguments, such as projects/my-project-123; for a service account the project is the one Google reports, or - on a connection without targets"}
	truncatedField = capability.Field{Name: "truncated", Description: "True when a list or text of the answer " +
		"was cut to its bound; absent otherwise"}
)

// --- members, conditions, shared by the policy and the deny policy ---

// exprWire is the Expr of Google: a condition in the Common Expression Language.
type exprWire struct {
	Expression  string `json:"expression"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

type exprResult struct {
	Expression  string `json:"expression,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
}

// cutter bounds the provider data of one answer and remembers whether it cut anything.
type cutter struct{ cut bool }

func (c *cutter) text(value string, limit int) string {
	if len(value) > limit {
		c.cut = true
	}
	return boundedText(value, limit)
}

// list bounds a list of principals or permissions. They are third-party text and go out unchanged otherwise.
func (c *cutter) list(in []string, limit int) []string {
	if len(in) == 0 {
		return nil
	}
	if len(in) > limit {
		c.cut = true
	}
	out := make([]string, 0, min(len(in), limit))
	for _, item := range in[:min(len(in), limit)] {
		out = append(out, c.text(item, maxNameText))
	}
	return out
}

func (c *cutter) expr(e *exprWire) *exprResult {
	if e == nil {
		return nil
	}
	return &exprResult{Expression: c.text(e.Expression, maxCondition), Title: c.text(e.Title, maxNameText),
		Description: c.text(e.Description, maxText), Location: c.text(e.Location, maxNameText)}
}

const exprSchema = `{"type":"object","properties":{"expression":{"type":"string"},"title":{"type":"string"},` +
	`"description":{"type":"string"},"location":{"type":"string"}},"additionalProperties":false}`

const stringArraySchema = `{"type":"array","items":{"type":"string"}}`
