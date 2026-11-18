package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const disksDeleteID = Provider + ".disks.delete"

// maxDiskUsers bounds the instances a deletion may expect on a disk. Google refuses to delete a disk that an
// instance still uses, so a longer list could never be confirmed usefully.
const maxDiskUsers = 128

// shownDiskUsers is how many expected instances the confirmation names before it counts the rest.
const shownDiskUsers = 10

var disksDelete = capability.Descriptor{
	ID: disksDeleteID, Version: 1, Title: "Delete a Compute Engine disk",
	Description: "Delete one persistent disk or Hyperdisk of a zone of a project (disks.delete of Compute Engine v1), " +
		"by its name or numeric ID. The disk and all data on it are gone afterwards; this cannot be undone, and its " +
		"snapshots stay. Google refuses a disk that an instance still uses. The confirmation names the size and the " +
		"attached instances you expect, as disks.get shows them: after the confirmation the tool reads the disk and " +
		"deletes it only if its size and its users still match; otherwise it fails without deleting, and the disk " +
		"is to be read again with disks.get. Offered only where a connection's tools list names it." +
		diskChangeNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "disks", "delete"}, Risk: computeDeleteRisk, Provider: Provider,
	RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeZoneProp + `,` +
		diskOrIDProp + `,"expected_size_gb":{"type":"integer","minimum":1,"maximum":65536},` +
		`"expected_users":{"type":"array","maxItems":128,"uniqueItems":true,"items":{"type":"string","minLength":1,` +
		`"maxLength":63,"pattern":"` + computeNameExpr + `"}},` + computeRequestIDProp +
		`},"required":["project_id","zone","disk","expected_size_gb","expected_users"],"additionalProperties":false}`),
	OutputSchema: instanceLifecycleOutput,
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument, diskOrIDArgument,
		{Name: "expected_size_gb", Required: true, Description: "Size in GB the disk has according to disks.get " +
			"(sizeGb); only compared, never sent"},
		{Name: "expected_users", Required: true, Description: "Names of the instances the disk is attached to " +
			"according to disks.get (the last part of each users URL), or an empty list for a disk that no instance " +
			"uses; only compared, never sent"},
		computeRequestIDArgument},
	Fields: instanceLifecycleFields,
	Examples: []capability.Example{{
		Description: "Delete a disk that no instance uses after reading it",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","disk":"data-disk-a",` +
			`"expected_size_gb":100,"expected_users":[]}`),
	}},
}

type diskDeleteArguments struct {
	ProjectID      string   `json:"project_id"`
	Zone           string   `json:"zone"`
	Disk           string   `json:"disk"`
	ExpectedSizeGb *int64   `json:"expected_size_gb"`
	ExpectedUsers  []string `json:"expected_users"`
	RequestID      string   `json:"request_id"`
}

func (a diskDeleteArguments) check() error {
	if err := checkDiskTarget(a.ProjectID, a.Zone, a.Disk); err != nil {
		return err
	}
	switch {
	case a.ExpectedSizeGb == nil || !inRange(a.ExpectedSizeGb, 1, maxDiskSizeGb):
		return badRequest("expected_size_gb must be 1 to " + strconv.Itoa(maxDiskSizeGb))
	case a.ExpectedUsers == nil:
		return badRequest("expected_users must list the attached instances, or be empty for a disk no instance uses")
	case len(a.ExpectedUsers) > maxDiskUsers:
		return badRequest("expected_users takes at most " + strconv.Itoa(maxDiskUsers) + " instances")
	}
	for i, user := range a.ExpectedUsers {
		if !validComputeName(user) || slices.Contains(a.ExpectedUsers[:i], user) {
			return badRequest("expected_users must be distinct instance names, never a path or URL")
		}
	}
	return nil
}

// diskUsers turns the users of a disk into instance names. Each must be an instance URL of the zone of the disk;
// any other form leaves nothing to compare.
func diskUsers(zone string, users []string) ([]string, bool) {
	names := make([]string, 0, len(users))
	for _, user := range users {
		_, name, found := strings.Cut(user, "/zones/"+zone+"/instances/")
		if !found || !validComputeName(name) {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

func handleDisksDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete disk"
	var a diskDeleteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	requestID, err := checkRequestID(a.RequestID)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	// The disk is read after the confirmation and compared with what the person confirmed. The read uses the
	// token of this tool, whose scope covers it.
	path := computeZonePath(a.ProjectID, a.Zone) + "/disks/" + url.PathEscape(a.Disk)
	var wire json.RawMessage
	if err := c.get(ctx, op, disksDeleteID, serviceCompute, path, nil, &wire); err != nil {
		return nil, err
	}
	row, err := diskResult(op, wire)
	if err != nil {
		return nil, err
	}
	name, err := computeName(op, row)
	if err != nil {
		return nil, err
	}
	// users is read in full: the bounded row may cut a long list, and a cut list must not match.
	var attached struct {
		Users []string `json:"users"`
	}
	sizeText, _ := row["sizeGb"].(string)
	size, sizeErr := strconv.ParseInt(sizeText, 10, 64)
	users, usersOK := []string(nil), false
	if json.Unmarshal(wire, &attached) == nil {
		users, usersOK = diskUsers(a.Zone, attached.Users)
	}
	if id, _ := row["id"].(string); sizeErr != nil || size < 1 || !usersOK || name != a.Disk && id != a.Disk ||
		!inZone(row, a.Zone) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	expected := slices.Clone(a.ExpectedUsers)
	slices.Sort(expected)
	slices.Sort(users)
	if size != *a.ExpectedSizeGb || !slices.Equal(users, expected) {
		return nil, badRequest("the disk no longer matches expected_size_gb and expected_users, so nothing was " +
			"deleted; read the disk again with disks.get and confirm what you want to delete")
	}
	return c.computeDelete(ctx, computeDeleteTarget{op: op, tool: disksDeleteID, path: path, zone: a.Zone,
		requestID: requestID, keyed: true})
}

// diskDeleteSummary phrases the deletion from its arguments alone: the project, zone, and disk, and the size and
// attached instances the person confirms.
func diskDeleteSummary(raw json.RawMessage) string {
	var a diskDeleteArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	size := "?"
	if a.ExpectedSizeGb != nil {
		size = strconv.FormatInt(*a.ExpectedSizeGb, 10)
	}
	users := "attached to no instance"
	if len(a.ExpectedUsers) > 0 {
		shown := a.ExpectedUsers[:min(len(a.ExpectedUsers), shownDiskUsers)]
		users = "attached to instances " + strings.Join(boundedTexts(shown, maxNameText), ", ")
		if rest := len(a.ExpectedUsers) - len(shown); rest > 0 {
			users += " and " + strconv.Itoa(rest) + " more"
		}
	}
	return "Delete disk " + boundedText(a.Disk, maxNameText) + " in zone " + boundedText(a.Zone, maxNameText) +
		" of project " + boundedText(a.ProjectID, maxNameText) + " (" + size + " GB, " + users + ") for good: " +
		"the disk and all data on it are removed and cannot be restored; the disk is deleted only if it still " +
		"has this size and these instances"
}
