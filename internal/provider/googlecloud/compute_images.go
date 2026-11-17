package googlecloud

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	imagesListID = Provider + ".images.list"
	imagesGetID  = Provider + ".images.get"
)

// imageMembers are the documented, not input-only members of the Image resource of compute v1 (revision
// 20261106). params is left out because it is input only. The encryption key members stay in the schema; their
// raw key material is dropped from every answer (see dropKeyMaterial).
var imageMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"creationTimestamp", "string"}, {"name", "string"},
	{"description", "string"}, {"sourceType", "string"}, {"rawDisk", "object"}, {"deprecated", "object"},
	{"status", "string"}, {"archiveSizeBytes", "string"}, {"diskSizeGb", "string"}, {"sourceDisk", "string"},
	{"sourceDiskId", "string"}, {"licenses", "array"}, {"licenseCodes", "array"}, {"family", "string"},
	{"imageEncryptionKey", "object"}, {"sourceDiskEncryptionKey", "object"}, {"selfLink", "string"},
	{"labels", "object"}, {"labelFingerprint", "string"}, {"guestOsFeatures", "array"}, {"sourceImage", "string"},
	{"sourceImageId", "string"}, {"sourceImageEncryptionKey", "object"}, {"sourceSnapshot", "string"},
	{"sourceSnapshotId", "string"}, {"sourceSnapshotEncryptionKey", "object"}, {"storageLocations", "array"},
	{"shieldedInstanceInitialState", "object"}, {"architecture", "string"}, {"satisfiesPzs", "boolean"},
	{"satisfiesPzi", "boolean"}, {"enableConfidentialCompute", "boolean"},
}

const imageFields = "{kind, id, creationTimestamp, name, description, sourceType, rawDisk, deprecated (state, " +
	"replacement, and the deprecation timestamps), status, archiveSizeBytes, diskSizeGb, sourceDisk (URL), " +
	"sourceDiskId, licenses, licenseCodes, family, imageEncryptionKey, sourceDiskEncryptionKey, " +
	"sourceImageEncryptionKey, sourceSnapshotEncryptionKey (each with kmsKeyName, kmsKeyServiceAccount, and " +
	"sha256 only), selfLink, labels, labelFingerprint, guestOsFeatures, sourceImage, sourceImageId, " +
	"sourceSnapshot, sourceSnapshotId, storageLocations, shieldedInstanceInitialState, architecture, " +
	"satisfiesPzs, satisfiesPzi, enableConfidentialCompute}; untrusted text"

const computeImageProp = `"image":{"type":"string","minLength":1,"maxLength":63,` +
	`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`

var imagesList = capability.Descriptor{
	ID: imagesListID, Version: 1, Title: "List the images of a project",
	Description: "List the images of a project from Compute Engine, with their status, family, sizes, source, " +
		"licenses, guest OS features, storage locations, labels, deprecation state, and Cloud KMS key names, " +
		"optionally filtered, sorted, and with partial results accepted. Only the images of the project itself " +
		"are listed, not those of public image projects." + keyMaterialNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "images", "disks"}, Risk: readRisk, Provider: Provider,
	InputSchema:  json.RawMessage(globalListInput),
	OutputSchema: globalListOutput("images", imageMembers),
	Arguments:    globalListArgs,
	Fields:       globalListFields("images", "Images", imageFields),
	Examples: []capability.Example{{
		Description: "List the images of a family",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","filter":"family = web-base"}`),
	}},
}

var imagesGet = capability.Descriptor{
	ID: imagesGetID, Version: 1, Title: "Get one image",
	Description: "Read one image of a project from Compute Engine, by its name or its numeric ID, with the " +
		"documented fields of the image. Images of public image projects and the newest image of a family are " +
		"not offered." + keyMaterialNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "images", "disks"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeImageProp +
		`},"required":["project_id","image"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(membersSchema(imageMembers)),
	Arguments: []capability.Argument{serviceProjectArgument, {Name: "image", Required: true,
		Description: "Name of the image such as web-base-v1, or its numeric ID; never a path or URL"}},
	Fields: []capability.Field{{Name: "name", Description: "The image as one object: " + imageFields}},
	Examples: []capability.Example{{
		Description: "Read one image",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","image":"web-base-v1"}`),
	}},
}

func handleImagesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return handleKeyedList(ctx, resolved, secrets, red, raw, "list images", imagesListID, "images", "images",
		imageMembers)
}

func handleImagesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	return handleKeyedGet(ctx, resolved, secrets, red, raw, "get image", imagesGetID, "images", "image", "image",
		imageMembers)
}
