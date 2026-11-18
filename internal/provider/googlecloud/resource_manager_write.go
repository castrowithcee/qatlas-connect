package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// This file holds what the changing tools of Cloud Resource Manager share: binding a parent to the targets,
// binding a project that is read before it is changed, and returning the long-running operation of a change.

// resourceManagerWriteRisk is the risk of a changing tool of Resource Manager. Only the effect and the
// idempotency differ between the tools.
func resourceManagerWriteRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true, DataSensitivity: dataSensitivity}
}

// parentArgProp is the schema of a parent argument, in the form parentPattern checks.
func parentArgProp(name string) string {
	return `"` + name + `":{"type":"string","minLength":9,"maxLength":35,` +
		`"pattern":"^(organizations|folders)/[0-9]{1,20}$"}`
}

// maxConfirmationRunes mirrors the cap the application core puts on a confirmation summary. A summary that
// would be cut is never confirmed: the tool refuses before any request instead.
const maxConfirmationRunes = 500

// confirmationTooLong is the refusal of a call whose confirmation does not fit.
const confirmationTooLong = "the confirmation of this call would be longer than 500 characters, so it would be " +
	"cut; use fewer or shorter labels"

// summaryFits reports whether a confirmation reaches the person whole.
func summaryFits(text string) bool { return utf8.RuneCountInString(text) <= maxConfirmationRunes }

// decidesWriteParent reports before any request whether the targets already settle a parent. An organization is
// reached only by an organization target of that very organization, also on a connection without targets,
// because such a connection may reach organizations the person never meant to hand over. A folder is reached
// by a connection without targets and by a folder target; below an organization or another folder target it
// is settled only by reading the folders above it.
func (a allowlist) decidesWriteParent(parent string) (allowed, decided bool) {
	if id, ok := strings.CutPrefix(parent, prefixOrganization); ok {
		return a.has(kindOrganization, id), true
	}
	switch {
	case !a.bound():
		return true, true
	case !a.hasKind(kindOrganization, kindFolder):
		return false, true
	case a.has(kindFolder, strings.TrimPrefix(parent, prefixFolder)):
		return true, true
	}
	return false, false
}

// argumentParent binds an organization or folder given as an argument to the targets: by the targets alone where they
// decide, otherwise by reading the folders above it.
func (a *admitter) argumentParent(ctx context.Context, parent string) (bool, error) {
	if allowed, decided := a.list.decidesWriteParent(parent); decided {
		return allowed, nil
	}
	return a.parent(ctx, parent)
}

// boundProject reads a project that a tool is about to change and binds it to the targets, with the project
// target or the folders above it. The tool reads before it writes, so a project outside the targets is refused
// before any change.
func boundProject(ctx context.Context, c *client, adm *admitter, op, tool, id string) (projectWire, error) {
	project, err := c.project(ctx, op, tool, id)
	if err != nil {
		return projectWire{}, err
	}
	admitted, err := adm.project(ctx, project)
	if err != nil {
		return projectWire{}, err
	}
	if !admitted {
		return projectWire{}, refuseOutside("the project")
	}
	return project, nil
}

// boundFolder reads a folder that a tool is about to change and binds it to the targets, with the folder
// target or the folders above it. The tool reads before it writes, so a folder outside the targets is refused
// before any change.
func boundFolder(ctx context.Context, c *client, adm *admitter, op, tool, id string) (folderWire, error) {
	folder, err := c.folder(ctx, op, tool, id)
	if err != nil {
		return folderWire{}, err
	}
	admitted, err := adm.folder(ctx, folder)
	if err != nil {
		return folderWire{}, err
	}
	if !admitted {
		return folderWire{}, refuseOutside("the folder")
	}
	return folder, nil
}

// projectPath is the path of a project by its number, which the answer of the read names.
func projectPath(op string, project projectWire) (string, error) {
	number, ok := strings.CutPrefix(project.Name, prefixProject)
	if !ok || !numberPattern.MatchString(number) {
		return "", provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return "/v3/" + prefixProject + url.PathEscape(number), nil
}

// resourceManagerResult is what a changing Resource Manager tool returns: the operation as Google answered it.
type resourceManagerResult struct {
	Service string `json:"service"`
	operationResult
}

// changeResourceManager sends the one request of a changing tool, with no body if body is nil, and returns the
// operation of the answer without waiting for it: operations.get with service resource-manager reads its state.
// An answer that names no usable operation leaves the outcome open, like an answer that cannot be read.
func (c *client) changeResourceManager(ctx context.Context, op, tool, method, path string, query url.Values,
	body any) (any, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, failure(op, "the request could not be built")
		}
	}
	var wire operationWire
	if err := c.send(ctx, op, tool, serviceResourceManager, method, path, query, payload, &wire, true); err != nil {
		return nil, err
	}
	if !operationNamePattern.MatchString(wire.Name) {
		failed := provider.InvalidResponse(op, subject+" returned an invalid response")
		failed.Message += unsureNote
		return nil, failed
	}
	result, err := operationOf(op, wire)
	if err != nil {
		return nil, err
	}
	return resourceManagerResult{Service: string(serviceResourceManager), operationResult: result}, nil
}

var resourceManagerOperationFields = []capability.Field{
	{Name: "service", Description: "resource-manager: the value of service for operations.get"},
	{Name: "name", Description: "Name of the operation, operations/ID"},
	{Name: "done", Description: "False while the operation runs; true only if Google answered an operation that is already done"},
	{Name: "metadata", Description: "Metadata of the operation with its @type; untrusted data, cut to a bounded size"},
	{Name: "response", Description: "Response of a done operation with its @type, such as the project; untrusted data, cut to a bounded size"},
}
