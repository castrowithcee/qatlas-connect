package googlecloud

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// This file is the base of the Compute tools that delete a resource or change one through a POST: the risk of a
// deletion, the request ID, the one request that is never repeated, and the result, which is the Compute
// operation as Google returned it. A tool never waits for the operation; compute-operations.get reads its state.
// The firewall, address, and VPN deletions use this base, and so do the disk and image changes
// (compute_disks_write.go, compute_disks_delete.go, compute_images_write.go).

// computeDeleteRisk is the risk of a deleting Compute tool. The method is idempotent through requestId: Google
// ignores a repeated request with the ID of one it already received. The result is an operation, which names
// the person who started it.
var computeDeleteRisk = capability.Risk{
	Effect:          capability.EffectDelete,
	Idempotency:     capability.IdempotencyIdempotent,
	Confirmation:    capability.ConfirmationRequired,
	OpenWorld:       true,
	DataSensitivity: computeOperationSensitivity,
}

// requestIDPattern is the UUID form Google documents for requestId.
var requestIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const nilRequestID = "00000000-0000-0000-0000-000000000000"

const (
	computeRequestIDProp = `"request_id":{"type":"string","minLength":36,"maxLength":36,` +
		`"pattern":"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"}`
)

// computeDeleteOutput is the output schema of every deleting Compute tool.
var computeDeleteOutput = json.RawMessage(`{"type":"object","properties":{"operation":` +
	membersSchema(computeOperationMembers) + `,"scope":{"type":"string","enum":["global","region"]},` +
	`"region":{"type":"string"},"request_id":{"type":"string"}},` +
	`"required":["operation","scope","request_id"],"additionalProperties":false}`)

var computeRequestIDArgument = capability.Argument{Name: "request_id", Description: "UUID that identifies the " +
	"deletion. Repeating the call with the same request_id is safe: Google ignores a request whose ID it already " +
	"received. Without it the tool creates one and returns it as request_id; the all-zero UUID is not allowed"}

// computeDeleteNote ends the description of every deleting Compute tool.
const computeDeleteNote = " Needs the confirmation of a person for each call. The project and the names are " +
	"checked against their form and the targets before any request. Google answers with a Compute operation and " +
	"the tool never waits for it: the result is the operation as Google returned it, and compute-operations.get " +
	"with the scope and region of the result reads its state; an operation that is already done with an error is " +
	"reported as a provider error. The call is never repeated by the tool; after an unclear outcome, read the " +
	"resource, or repeat the call with the same request_id."

var computeDeleteOperationFields = []capability.Field{
	{Name: "operation", Description: "The Compute operation: " + computeOperationFields},
	{Name: "scope", Description: "Scope of the operation for compute-operations.get: global or region"},
	{Name: "region", Description: "Region of the operation for compute-operations.get; present for scope region"},
	{Name: "request_id", Description: "The request_id sent to Google: the one given, or the one the tool created"},
}

// checkRequestID returns the request ID to send: the one given when it is a UUID that is not all zero, a new
// random one when none was given. It runs before any I/O.
func checkRequestID(value string) (string, error) {
	if value == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", failure("delete", "no request_id could be created")
		}
		raw[6] = raw[6]&0x0f | 0x40
		raw[8] = raw[8]&0x3f | 0x80
		return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
	}
	if !requestIDPattern.MatchString(value) || value == nilRequestID {
		return "", badRequest("request_id must be a UUID that is not all zero")
	}
	return value, nil
}

// computeDeleteTarget describes one request of the base. path is below the origin and was built from checked
// values; zone is set for a zonal resource and region for a regional one, neither for a global resource.
// method is empty for a DELETE; a change sets POST, its JSON payload, and the query values it adds to requestId.
// noMessages is set for a VPN resource: no Google error text is passed on, and the secret members of the
// answer are dropped, like those of the VPN reads (see vpnGet and dropVpnSecrets).
// keyed drops encryption key material from the answer, like the disk reads (see dropDiskKeyMaterial).
type computeDeleteTarget struct {
	op, tool, path, zone, region, requestID string
	method                                  string
	payload                                 []byte
	query                                   url.Values
	noMessages, keyed                       bool
}

// withoutMessages keeps the status, type, and code of a provider error but not the message of its original
// entries, which is third-party text.
func withoutMessages(err error) error {
	var failed *provider.Error
	if !errors.As(err, &failed) || failed.Original == nil {
		return err
	}
	scrubbed := *failed.Original
	scrubbed.Entries = make([]provider.OriginalEntry, len(failed.Original.Entries))
	for i, entry := range failed.Original.Entries {
		entry.Message = ""
		scrubbed.Entries[i] = entry
	}
	copied := *failed
	copied.Original = &scrubbed
	return &copied
}

// computeDelete sends the one request and turns the operation of the answer into the result. The
// request is never repeated, and an answer that names no operation leaves the outcome open.
func (c *client) computeDelete(ctx context.Context, t computeDeleteTarget) (any, error) {
	out, err := c.deleteResource(ctx, t)
	if err != nil && t.noMessages {
		return nil, withoutMessages(err)
	}
	return out, err
}

func (c *client) deleteResource(ctx context.Context, t computeDeleteTarget) (any, error) {
	query := url.Values{"requestId": {t.requestID}}
	for name, values := range t.query {
		query[name] = values
	}
	method := t.method
	if method == "" {
		method = http.MethodDelete
	}
	var wire json.RawMessage
	if err := c.send(ctx, t.op, t.tool, serviceCompute, method, t.path, query, t.payload, &wire, true); err != nil {
		return nil, err
	}
	unsure := func() error {
		failed := provider.InvalidResponse(t.op, subject+" returned an invalid response")
		failed.Message += unsureNote
		return failed
	}
	if t.noMessages || t.keyed {
		decoded, err := decodeRow(t.op, wire)
		if err != nil {
			return nil, unsure()
		}
		if t.noMessages {
			dropVpnSecrets(decoded)
		}
		if t.keyed {
			dropDiskKeyMaterial(decoded)
		}
		if wire, err = json.Marshal(decoded); err != nil {
			return nil, unsure()
		}
	}
	row, err := computeOperationResult(t.op, wire)
	if err != nil {
		return nil, unsure()
	}
	if _, err := computeName(t.op, row); err != nil {
		return nil, unsure()
	}
	scope := scopeGlobal
	switch {
	case t.zone != "":
		scope = scopeZone
	case t.region != "":
		scope = scopeRegion
	}
	if !inScope(row, scope, t.zone, t.region) {
		return nil, unsure()
	}
	if status, _ := row["status"].(string); status == "DONE" {
		if failed := computeOperationError(t.op, wire); failed != nil {
			return nil, failed
		}
	}
	out := map[string]any{"operation": row, "scope": scope, "request_id": t.requestID}
	if t.zone != "" {
		out["zone"] = t.zone
	}
	if t.region != "" {
		out["region"] = t.region
	}
	return out, nil
}
