package googlecloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	imagesInsertID    = Provider + ".images.insert"
	imagesDeleteID    = Provider + ".images.delete"
	snapshotsDeleteID = Provider + ".snapshots.delete"
)

// This file holds the tool that creates an image and the two that delete a snapshot or an image (compute v1,
// revision 20261106). Each sends one request through the base in compute_delete.go. The source of an image is a
// name of the project of the call, never a URL: a resource of another project, or an archive in Cloud Storage
// (rawDisk), cannot be named.

const (
	// maxKeyFileContent bounds the base64 content of one secure boot key file; the discovery document gives no
	// length.
	maxKeyFileContent = 65536
	maxKeyFiles       = 16
)

const base64Expr = `^[A-Za-z0-9+/]*={0,2}$`

// imageChangeNote ends the description of images.insert.
const imageChangeNote = " Needs the confirmation of a person for each call. The project and every name are " +
	"checked against their form and the targets before any request. Google answers with a Compute operation and " +
	"the tool never waits for it: the result is the operation as Google returned it, and compute-operations.get " +
	"with scope global reads its state; an operation that is already done with an error is reported as a " +
	"provider error. The call is never repeated by the tool; after an unclear outcome, read the image with " +
	"images.get, or repeat the call with the same request_id. Customer-supplied key material (rawKey, " +
	"rsaEncryptedKey) is no argument and is never returned; only a Cloud KMS key is named. Nothing is shared, made " +
	"public, or bound to an IAM policy."

// --- images.insert ---

const keyFileProp = `{"type":"object","properties":{"content":{"type":"string","minLength":4,"maxLength":65536,` +
	`"pattern":"` + base64Expr + `"},"file_type":{"type":"string","enum":["BIN","X509"]}},` +
	`"required":["content"],"additionalProperties":false}`

const keyFilesProp = `{"type":"array","maxItems":16,"items":` + keyFileProp + `}`

var imagesInsert = capability.Descriptor{
	ID: imagesInsertID, Version: 1, Title: "Create a Compute Engine image",
	Description: "Create one image in a project (images.insert of Compute Engine v1) from a disk, a snapshot, or an " +
		"image of the project itself. The image is a global resource, stored in the storage location given or " +
		"chosen by Google and billed for its storage until it is deleted; images.get reads it. Exactly one source " +
		"is named, by name: source_disk with the zone source_disk_zone, source_snapshot, or source_image; the tool " +
		"builds the URL in the project of the call, so a resource of another project cannot be named, and an " +
		"archive in Cloud Storage (rawDisk) is not offered. Google refuses a disk that is attached to a running " +
		"instance unless force_create is true, which may give an image whose file system is not consistent." +
		imageChangeNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "images", "create"}, Risk: diskCreateRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + nameProp("name") + `,` +
		diskDescriptionProp + `,` + nameProp("family") + `,` + nameProp("source_disk") + `,` +
		nameProp("source_disk_zone") + `,` + nameProp("source_snapshot") + `,` + nameProp("source_image") + `,` +
		diskLabelsProp + `,` + storageLocationsProp + `,` + guestOsFeaturesProp + `,` + licensesProp + `,` +
		diskKMSProps + `,"architecture":{"type":"string","enum":["ARM64","X86_64"]},` +
		`"shielded_instance_initial_state":{"type":"object","properties":{"pk":` + keyFileProp + `,"keks":` +
		keyFilesProp + `,"dbs":` + keyFilesProp + `,"dbxs":` + keyFilesProp + `},"additionalProperties":false},` +
		`"force_create":{"type":"boolean"},` + computeRequestIDProp +
		`},"required":["project_id","name"],"additionalProperties":false}`),
	OutputSchema: computeDeleteOutput,
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "name", Required: true, Description: "Name of the new image such as web-base-v2: a lowercase letter, " +
			"then up to 62 lowercase letters, digits, or '-', not ending in '-'"},
		diskDescriptionArgument,
		{Name: "family", Description: "Image family such as web-base that the image joins; a disk created from the " +
			"family takes its newest image that is not deprecated"},
		{Name: "source_disk", Description: "Name of a disk of the project in source_disk_zone to create the image " +
			"from. Exactly one source of source_disk, source_snapshot, and source_image"},
		{Name: "source_disk_zone", Description: "Zone of source_disk such as us-central1-a; only with source_disk, " +
			"and needed with it"},
		{Name: "source_snapshot", Description: "Name of a snapshot of the project to create the image from"},
		{Name: "source_image", Description: "Name of an image of the project to copy"},
		diskLabelsArgument,
		{Name: "storage_locations", Description: "Cloud Storage location of the image, regional or multi-regional, " +
			"such as us or europe-west1; Google currently takes one. Without it Google picks the multi-region " +
			"nearest the source"},
		{Name: "guest_os_features", Description: "Features to enable on the guest operating system of a bootable " +
			"image, such as UEFI_COMPATIBLE or GVNIC; each once"},
		{Name: "licenses", Description: "Licenses to attach, each {license, project}: license is the name of a " +
			"license, project one of Google's public image projects (" + strings.Join(publicImageProjects, ", ") +
			"), or left out for a license of the project itself; at most 32"},
		diskKMSArgument, diskKMSAccountArgument,
		{Name: "architecture", Description: "Architecture of the image: ARM64 or X86_64"},
		{Name: "shielded_instance_initial_state", Description: "Secure boot keys of Shielded VM instances created " +
			"from the image, as {pk, keks, dbs, dbxs}: pk is the platform key, keks the key exchange keys, dbs the " +
			"allowed and dbxs the forbidden key database, each file {content, file_type} with content base64 of at " +
			"most 65536 characters and file_type BIN or X509; at most 16 files per list. Google describes these as " +
			"public keys"},
		{Name: "force_create", Description: "True to create the image from a disk even while it is attached to a " +
			"running instance (Google's forceCreate)"},
		instanceRequestIDArgument},
	Fields: computeDeleteOperationFields,
	Examples: []capability.Example{{
		Description: "Create an image of a disk into a US multi-region and a family",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","name":"web-base-v2","family":"web-base",` +
			`"source_disk":"data-disk-a","source_disk_zone":"us-central1-a","storage_locations":["us"]}`),
	}},
}

type imageKeyFile struct {
	Content  string `json:"content"`
	FileType string `json:"file_type"`
}

type imageInitialState struct {
	PK   *imageKeyFile  `json:"pk"`
	KEKs []imageKeyFile `json:"keks"`
	DBs  []imageKeyFile `json:"dbs"`
	DBXs []imageKeyFile `json:"dbxs"`
}

type imageInsertArguments struct {
	ProjectID                    string             `json:"project_id"`
	Name                         string             `json:"name"`
	Description                  string             `json:"description"`
	Family                       string             `json:"family"`
	SourceDisk                   string             `json:"source_disk"`
	SourceDiskZone               string             `json:"source_disk_zone"`
	SourceSnapshot               string             `json:"source_snapshot"`
	SourceImage                  string             `json:"source_image"`
	Labels                       map[string]string  `json:"labels"`
	StorageLocations             []string           `json:"storage_locations"`
	GuestOsFeatures              []string           `json:"guest_os_features"`
	Licenses                     []diskLicense      `json:"licenses"`
	KmsKeyName                   string             `json:"kms_key_name"`
	KmsKeyServiceAccount         string             `json:"kms_key_service_account"`
	Architecture                 string             `json:"architecture"`
	ShieldedInstanceInitialState *imageInitialState `json:"shielded_instance_initial_state"`
	ForceCreate                  bool               `json:"force_create"`
	RequestID                    string             `json:"request_id"`
}

// check refuses an argument that is not in its documented form, before any secret is read. The refusal never
// repeats the value.
func (a imageInsertArguments) check() error {
	switch {
	case !validProject(a.ProjectID):
		return badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(a.Name):
		return badRequest("name must be an image name such as web-base-v2")
	case !validDiskText(a.Description):
		return badRequest("description must be at most " + strconv.Itoa(maxDiskDescription) +
			" characters of valid text without control characters")
	case !optionalName(a.Family):
		return badRequest("family must be 1 to 63 lowercase letters, digits, or '-' starting with a letter")
	case !optionalName(a.SourceDisk) || !optionalName(a.SourceDiskZone) || !optionalName(a.SourceSnapshot) ||
		!optionalName(a.SourceImage):
		return badRequest("a source must be a name of the project, never a path or URL")
	case a.Architecture != "" && a.Architecture != "ARM64" && a.Architecture != "X86_64":
		return badRequest("architecture must be ARM64 or X86_64")
	}
	sources := 0
	for _, source := range []string{a.SourceDisk, a.SourceSnapshot, a.SourceImage} {
		if source != "" {
			sources++
		}
	}
	switch {
	case sources != 1:
		return badRequest("name exactly one source: source_disk, source_snapshot, or source_image")
	case (a.SourceDisk == "") != (a.SourceDiskZone == ""):
		return badRequest("source_disk needs source_disk_zone, and source_disk_zone is only taken with source_disk")
	}
	if err := checkDiskLabels(a.Labels); err != nil {
		return err
	}
	if err := checkStorageLocations(a.StorageLocations); err != nil {
		return err
	}
	if err := checkGuestOsFeatures(a.GuestOsFeatures); err != nil {
		return err
	}
	if err := checkLicenses(a.Licenses); err != nil {
		return err
	}
	if err := checkDiskKey(a.KmsKeyName, a.KmsKeyServiceAccount); err != nil {
		return err
	}
	return a.ShieldedInstanceInitialState.check()
}

func (s *imageInitialState) check() error {
	if s == nil {
		return nil
	}
	files := append(append(append([]imageKeyFile{}, s.KEKs...), s.DBs...), s.DBXs...)
	if s.PK != nil {
		files = append(files, *s.PK)
	}
	for _, list := range [][]imageKeyFile{s.KEKs, s.DBs, s.DBXs} {
		if len(list) > maxKeyFiles {
			return badRequest("shielded_instance_initial_state takes at most " + strconv.Itoa(maxKeyFiles) +
				" files per list")
		}
	}
	for _, file := range files {
		decoded, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil || len(decoded) == 0 || len(file.Content) > maxKeyFileContent {
			return badRequest("the content of a secure boot key file must be base64 of at most " +
				strconv.Itoa(maxKeyFileContent) + " characters")
		}
		if file.FileType != "" && file.FileType != "BIN" && file.FileType != "X509" {
			return badRequest("file_type of a secure boot key file must be BIN or X509")
		}
	}
	return nil
}

func keyFileBody(file imageKeyFile) map[string]string {
	out := map[string]string{"content": file.Content}
	if file.FileType != "" {
		out["fileType"] = file.FileType
	}
	return out
}

func keyFilesBody(files []imageKeyFile) []map[string]string {
	out := make([]map[string]string, len(files))
	for i, file := range files {
		out[i] = keyFileBody(file)
	}
	return out
}

// body builds the Image Google takes from the checked arguments. Every reference is a URL built here.
func (a imageInsertArguments) body() map[string]any {
	p := a.ProjectID
	body := map[string]any{"name": a.Name}
	set := func(key, value string) {
		if value != "" {
			body[key] = value
		}
	}
	set("description", a.Description)
	set("family", a.Family)
	switch {
	case a.SourceDisk != "":
		body["sourceDisk"] = computeResourceURL("projects", p, "zones", a.SourceDiskZone, "disks", a.SourceDisk)
	case a.SourceSnapshot != "":
		body["sourceSnapshot"] = computeResourceURL("projects", p, "global", "snapshots", a.SourceSnapshot)
	case a.SourceImage != "":
		body["sourceImage"] = computeResourceURL("projects", p, "global", "images", a.SourceImage)
	}
	if len(a.Labels) > 0 {
		body["labels"] = a.Labels
	}
	if len(a.StorageLocations) > 0 {
		body["storageLocations"] = a.StorageLocations
	}
	if len(a.GuestOsFeatures) > 0 {
		body["guestOsFeatures"] = guestOsFeatureBody(a.GuestOsFeatures)
	}
	if len(a.Licenses) > 0 {
		body["licenses"] = licenseURLs(p, a.Licenses)
	}
	if a.KmsKeyName != "" {
		body["imageEncryptionKey"] = diskKeyBody(a.KmsKeyName, a.KmsKeyServiceAccount)
	}
	set("architecture", a.Architecture)
	if s := a.ShieldedInstanceInitialState; s != nil {
		state := map[string]any{}
		if s.PK != nil {
			state["pk"] = keyFileBody(*s.PK)
		}
		for key, files := range map[string][]imageKeyFile{"keks": s.KEKs, "dbs": s.DBs, "dbxs": s.DBXs} {
			if len(files) > 0 {
				state[key] = keyFilesBody(files)
			}
		}
		body["shieldedInstanceInitialState"] = state
	}
	return body
}

func handleImagesInsert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create image"
	var a imageInsertArguments
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
	payload, err := json.Marshal(a.body())
	if err != nil {
		return nil, failure(op, "the request could not be built")
	}
	var query url.Values
	if a.ForceCreate {
		query = url.Values{"forceCreate": {"true"}}
	}
	c, err := computeClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return c.computeDelete(ctx, computeDeleteTarget{op: op, tool: imagesInsertID, method: http.MethodPost,
		path: computeProjectPath(a.ProjectID) + "/global/images", requestID: requestID, payload: payload,
		query: query, keyed: true})
}

// imageInsertSummary names project, image, source, family, and storage location: what the image holds and costs.
func imageInsertSummary(raw json.RawMessage) string {
	var a imageInsertArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	source := "no source"
	switch {
	case a.SourceDisk != "":
		source = "disk " + boundedText(a.SourceDisk, maxNameText) + " in zone " + boundedText(a.SourceDiskZone, maxNameText)
	case a.SourceSnapshot != "":
		source = "snapshot " + boundedText(a.SourceSnapshot, maxNameText)
	case a.SourceImage != "":
		source = "image " + boundedText(a.SourceImage, maxNameText)
	}
	location := "a location Google picks near the source"
	if len(a.StorageLocations) > 0 {
		location = strings.Join(boundedTexts(a.StorageLocations, maxNameText), ", ")
	}
	out := "Create image " + boundedText(a.Name, maxNameText) + " in project " + boundedText(a.ProjectID, maxNameText) +
		" from " + source + " (stored in " + location
	if a.Family != "" {
		out += ", family " + boundedText(a.Family, maxNameText)
	}
	out += ")"
	if a.ForceCreate {
		out += ", even if the disk is attached to a running instance"
	}
	return out + ": the image is billed for its storage until it is deleted"
}

// --- snapshots.delete and images.delete ---

// globalDelete describes one tool that deletes a global resource of a project without reading it first.
type globalDelete struct {
	id, title, kind, argument, segment, prop, effect, loss, read string
}

var (
	snapshotDelete = globalDelete{id: snapshotsDeleteID, title: "Delete a disk snapshot", kind: "snapshot",
		argument: "snapshot", segment: "snapshots", prop: computeSnapshotProp, read: "snapshots.get",
		effect: "The snapshot is gone afterwards and the backup it holds is lost for good; it cannot be restored. " +
			"Google moves data that a later snapshot of the same disk still needs into that snapshot.",
		loss: "the backup it holds is lost and cannot be restored"}
	imageDelete = globalDelete{id: imagesDeleteID, title: "Delete a Compute Engine image", kind: "image",
		argument: "image", segment: "images", prop: computeImageProp, read: "images.get",
		effect: "The image is gone afterwards and lost for good; it cannot be restored. Disks and instances created " +
			"from it keep running, but it can no longer be used as a source; if it is the newest image of its family, " +
			"the family then names the next newest one.",
		loss: "the image is lost and cannot be restored, and it can no longer be used as a source"}
)

func (g globalDelete) descriptor() capability.Descriptor {
	return capability.Descriptor{
		ID: g.id, Version: 1, Title: g.title,
		Description: "Delete one " + g.kind + " of a project (" + g.segment + ".delete of Compute Engine v1), by its " +
			"name or numeric ID. " + g.effect + computeDeleteNote + computeTail,
		Tags: []string{"google", "cloud", "compute", g.segment, "delete"}, Risk: computeDeleteRisk, Provider: Provider,
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + g.prop + `,` +
			computeRequestIDProp + `},"required":["project_id","` + g.argument + `"],"additionalProperties":false}`),
		OutputSchema: computeDeleteOutput,
		Arguments: []capability.Argument{serviceProjectArgument,
			{Name: g.argument, Required: true, Description: "Name of the " + g.kind + ", or its numeric ID, as " +
				g.read + " shows it; never a path or URL"},
			computeRequestIDArgument},
		Fields: computeDeleteOperationFields,
		Examples: []capability.Example{{
			Description: "Delete a " + g.kind,
			Arguments:   json.RawMessage(`{"project_id":"my-project-123","` + g.argument + `":"old-` + g.kind + `"}`),
		}},
	}
}

var (
	snapshotsDelete = snapshotDelete.descriptor()
	imagesDelete    = imageDelete.descriptor()
)

func (g globalDelete) handler(op string) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, failure(op, "the validated arguments could not be read")
		}
		project, name := m["project_id"], m[g.argument]
		switch {
		case !validProject(project):
			return nil, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
		case !validComputeNameOrID(name):
			return nil, badRequest(g.argument + " must be a " + g.kind + " name or its numeric ID, never a path")
		}
		requestID, err := checkRequestID(m["request_id"])
		if err != nil {
			return nil, err
		}
		c, err := computeClient(ctx, op, project, resolved, secrets, red)
		if err != nil {
			return nil, err
		}
		path := computeProjectPath(project) + "/global/" + g.segment + "/" + url.PathEscape(name)
		return c.computeDelete(ctx, computeDeleteTarget{op: op, tool: g.id, path: path, requestID: requestID,
			keyed: true})
	}
}

// summary phrases the deletion from its arguments: project, type, and name, and what is lost.
func (g globalDelete) summary(raw json.RawMessage) string {
	var m map[string]string
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return "Delete " + g.kind + " " + boundedText(m[g.argument], maxNameText) + " of project " +
		boundedText(m["project_id"], maxNameText) + " for good: " + g.loss
}
