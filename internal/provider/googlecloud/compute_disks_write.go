package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
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
	disksInsertID         = Provider + ".disks.insert"
	disksResizeID         = Provider + ".disks.resize"
	disksCreateSnapshotID = Provider + ".disks.create-snapshot"
)

// This file holds the three tools that create a disk, grow a disk, and snapshot a disk (compute v1, revision
// 20261106). Each sends one POST through the base in compute_delete.go. Google takes every other resource as a
// URL; the tools build each one from checked names, so no argument takes a URL or a path, and no argument takes
// encryption key material.

const (
	// maxDiskSizeGb is the largest size Google documents for a persistent disk or a Hyperdisk (64 TiB).
	maxDiskSizeGb = 65536
	// maxDiskPerformance bounds provisioned IOPS and throughput. Google's ranges depend on the disk type and
	// Google checks them; this is Qatlas's bound.
	maxDiskPerformance = 10000000
	// maxDiskDescription bounds a description; the discovery document gives no length.
	maxDiskDescription = 2048
	maxDiskLabels      = 64
	maxDiskLicenses    = 32
	maxDiskReferences  = 10
)

const computeNameExpr = `^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`

const (
	kmsKeyNameExpr = `^projects/[a-z][-a-z0-9]{4,28}[a-z0-9]/locations/[a-z]([-a-z0-9]{0,61}[a-z0-9])?/` +
		`keyRings/[-_a-zA-Z0-9]{1,63}/cryptoKeys/[-_a-zA-Z0-9]{1,63}$`
	kmsAccountExpr = `^([a-z][-a-z0-9]{4,28}[a-z0-9]@[a-z][-a-z0-9]{4,28}[a-z0-9][.]iam[.]gserviceaccount[.]com|` +
		`[0-9]{1,20}-compute@developer[.]gserviceaccount[.]com)$`
	labelValueExpr = `^[a-z0-9_-]{0,63}$`
)

var (
	kmsKeyNamePattern = regexp.MustCompile(kmsKeyNameExpr)
	kmsAccountPattern = regexp.MustCompile(kmsAccountExpr)
	labelValueOrEmpty = regexp.MustCompile(labelValueExpr)
)

// publicImageProjects are the projects of Google's public OS images, as Google's list of operating system
// details names them. A source or license of any other project is refused, because naming any project ID would
// let a disk copy an image of a project outside the targets.
var publicImageProjects = []string{"almalinux-cloud", "centos-cloud", "cos-cloud", "debian-cloud",
	"fedora-coreos-cloud", "oracle-linux-cloud", "rhel-cloud", "rhel-sap-cloud", "rocky-linux-accelerator-cloud",
	"rocky-linux-cloud", "suse-byos-cloud", "suse-cloud", "suse-sap-cloud", "ubuntu-os-accelerator-images",
	"ubuntu-os-cloud", "ubuntu-os-pro-cloud", "windows-cloud", "windows-sql-cloud"}

// guestOsFeatureTypes are the documented types of GuestOsFeature without FEATURE_TYPE_UNSPECIFIED.
var guestOsFeatureTypes = []string{"BARE_METAL_LINUX_COMPATIBLE", "BMSAI_CAPABLE", "CCA_CAPABLE", "GVNIC", "IDPF",
	"MULTI_IP_SUBNET", "SECURE_BOOT", "SEV_CAPABLE", "SEV_LIVE_MIGRATABLE", "SEV_LIVE_MIGRATABLE_V2",
	"SEV_SNP_CAPABLE", "SNP_SVSM_CAPABLE", "SUSPEND_SAFE_FPR", "TDX_CAPABLE", "UEFI_COMPATIBLE",
	"VIRTIO_SCSI_MULTIQUEUE", "WINDOWS"}

var (
	diskCreateRisk = capability.Risk{Effect: capability.EffectCreate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: computeOperationSensitivity}
	diskUpdateRisk = capability.Risk{Effect: capability.EffectUpdate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: computeOperationSensitivity}
)

// computeResourceURL is the URL Google takes for a Compute resource, built from checked segments.
func computeResourceURL(segments ...string) string {
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		escaped[i] = url.PathEscape(segment)
	}
	return "https://compute.googleapis.com" + computeBasePath + "/" + strings.Join(escaped, "/")
}

// zoneRegion is the region of a zone: the zone name without its last part, as us-central1 of us-central1-a.
func zoneRegion(zone string) (string, bool) {
	i := strings.LastIndexByte(zone, '-')
	if i < 1 {
		return "", false
	}
	return zone[:i], validComputeName(zone[:i])
}

func validDiskText(value string) bool {
	return utf8.RuneCountInString(value) <= maxDiskDescription && utf8.ValidString(value) &&
		!strings.ContainsFunc(value, unicode.IsControl)
}

func checkDiskLabels(labels map[string]string) error {
	if len(labels) > maxDiskLabels {
		return badRequest("labels takes at most " + strconv.Itoa(maxDiskLabels) + " entries")
	}
	for key, value := range labels {
		if !labelKeyPattern.MatchString(key) || !labelValueOrEmpty.MatchString(value) {
			return badRequest("a label key must be 1 to 63 lowercase letters, digits, '_', or '-' starting with a " +
				"letter, and a value 0 to 63 of them")
		}
	}
	return nil
}

// checkDiskKey refuses a KMS key name or service account that is not in its documented form.
func checkDiskKey(name, account string) error {
	switch {
	case name != "" && !kmsKeyNamePattern.MatchString(name):
		return badRequest("kms_key_name must be projects/PROJECT/locations/LOCATION/keyRings/RING/cryptoKeys/KEY")
	case account != "" && name == "":
		return badRequest("kms_key_service_account needs kms_key_name")
	case account != "" && !kmsAccountPattern.MatchString(account):
		return badRequest("kms_key_service_account must be the email address of a service account")
	}
	return nil
}

func diskKeyBody(name, account string) map[string]string {
	key := map[string]string{"kmsKeyName": name}
	if account != "" {
		key["kmsKeyServiceAccount"] = account
	}
	return key
}

// --- shared descriptor parts ---

func nameProp(field string) string {
	return `"` + field + `":{"type":"string","minLength":1,"maxLength":63,"pattern":"` + computeNameExpr + `"}`
}

func enumOf(values []string) string {
	return `["` + strings.Join(values, `","`) + `"]`
}

const (
	diskOrIDProp = `"disk":{"type":"string","minLength":1,"maxLength":63,` +
		`"pattern":"^([a-z]([-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19})$"}`
	diskDescriptionProp = `"description":{"type":"string","maxLength":2048}`
	diskLabelsProp      = `"labels":{"type":"object","maxProperties":64,"additionalProperties":{"type":"string",` +
		`"maxLength":63,"pattern":"` + labelValueExpr + `"}}`
	diskKMSProps = `"kms_key_name":{"type":"string","minLength":1,"maxLength":512,"pattern":"` + kmsKeyNameExpr + `"},` +
		`"kms_key_service_account":{"type":"string","minLength":1,"maxLength":254,"pattern":"` + kmsAccountExpr + `"}`
	storageLocationsProp = `"storage_locations":{"type":"array","minItems":1,"maxItems":10,"uniqueItems":true,` +
		`"items":{"type":"string","minLength":1,"maxLength":63,"pattern":"` + computeNameExpr + `"}}`
)

var (
	licensesProp = `"licenses":{"type":"array","maxItems":32,"items":{"type":"object","properties":{` +
		`"project":{"type":"string","enum":` + enumOf(publicImageProjects) + `},` + nameProp("license") +
		`},"required":["license"],"additionalProperties":false}}`
	guestOsFeaturesProp = `"guest_os_features":{"type":"array","maxItems":17,"uniqueItems":true,"items":{"type":` +
		`"string","enum":` + enumOf(guestOsFeatureTypes) + `}}`
)

var (
	diskOrIDArgument = capability.Argument{Name: "disk", Required: true, Description: "Name of the disk such as " +
		"data-disk-a, or its numeric ID; never a path or URL"}
	diskDescriptionArgument = capability.Argument{Name: "description", Description: "Description of up to 2048 " +
		"characters without control characters; untrusted text the next reader sees"}
	diskLabelsArgument = capability.Argument{Name: "labels", Description: "Labels as an object of key to value, at " +
		"most 64: a key is 1 to 63 lowercase ASCII letters, digits, '_', or '-' starting with a letter, a value 0 to " +
		"63 of them"}
	diskKMSArgument = capability.Argument{Name: "kms_key_name", Description: "Cloud KMS key that encrypts the " +
		"resource (a customer-managed key) as projects/PROJECT/locations/LOCATION/keyRings/RING/cryptoKeys/KEY. " +
		"The service agent of Compute Engine needs the right to use it. Without it Google encrypts with its own key"}
	diskKMSAccountArgument = capability.Argument{Name: "kms_key_service_account", Description: "Service account " +
		"that makes the encryption requests to Cloud KMS, as its email address; only with kms_key_name. Without it " +
		"Google uses the Compute Engine default service account"}
)

// diskChangeNote ends the description of every tool of this file.
const diskChangeNote = " Needs the confirmation of a person for each call. The project, the zone, and every " +
	"name are checked against their form and the targets before any request. Google answers with a Compute " +
	"operation and the tool never waits for it: the result is the operation as Google returned it, and " +
	"compute-operations.get with scope zone and the zone of the result reads its state; an operation that is " +
	"already done with an error is reported as a provider error. The call is never repeated by the tool; after " +
	"an unclear outcome, read the resource, or repeat the call with the same request_id. Customer-supplied key " +
	"material (rawKey, rsaEncryptedKey) is no argument and is never returned; only a Cloud KMS key is named. " +
	"Nothing is shared, made public, or bound to an IAM policy."

// --- disks.insert ---

var disksInsert = capability.Descriptor{
	ID: disksInsertID, Version: 1, Title: "Create a Compute Engine disk",
	Description: "Create one persistent disk or Hyperdisk in a zone of a project (disks.insert of Compute Engine " +
		"v1): empty, from an image, from a snapshot, as a clone of a disk, or from an instant snapshot. The disk " +
		"is billed for its size and type, and for provisioned IOPS and throughput, until it is deleted. Every " +
		"reference is a name: an image, image family, snapshot, disk, instant snapshot, disk type, storage pool, or " +
		"resource policy of the project itself (disks, instant snapshots, disk types, and storage pools of the " +
		"zone, resource policies of its region), or an image, image family, or license of one of Google's public " +
		"image projects; the tool builds each URL itself, so a resource of another project cannot be named." +
		diskChangeNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "disks", "create"}, Risk: diskCreateRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeZoneProp + `,` +
		nameProp("name") + `,` + diskDescriptionProp + `,` +
		`"size_gb":{"type":"integer","minimum":1,"maximum":65536},` + nameProp("type") + `,` +
		nameProp("source_image") + `,` + nameProp("source_image_family") + `,` +
		`"source_image_project":{"type":"string","enum":` + enumOf(publicImageProjects) + `},` +
		nameProp("source_snapshot") + `,` + nameProp("source_disk") + `,` + nameProp("source_instant_snapshot") + `,` +
		diskLabelsProp + `,` +
		`"provisioned_iops":{"type":"integer","minimum":1,"maximum":10000000},` +
		`"provisioned_throughput":{"type":"integer","minimum":1,"maximum":10000000},` +
		`"physical_block_size_bytes":{"type":"integer","enum":[4096,16384]},` +
		licensesProp + `,` + diskKMSProps + `,` +
		`"resource_policies":{"type":"array","maxItems":10,"uniqueItems":true,"items":{"type":"string",` +
		`"minLength":1,"maxLength":63,"pattern":"` + computeNameExpr + `"}},` +
		nameProp("storage_pool") + `,` +
		`"architecture":{"type":"string","enum":["ARM64","X86_64"]},` +
		guestOsFeaturesProp + `,` +
		`"access_mode":{"type":"string","enum":["READ_WRITE_SINGLE","READ_WRITE_MANY","READ_ONLY_MANY"]},` +
		`"enable_confidential_compute":{"type":"boolean"},` + computeRequestIDProp +
		`},"required":["project_id","zone","name"],"additionalProperties":false}`),
	OutputSchema: instanceLifecycleOutput,
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument,
		{Name: "name", Required: true, Description: "Name of the new disk such as data-disk-a: a lowercase letter, " +
			"then up to 62 lowercase letters, digits, or '-', not ending in '-'"},
		diskDescriptionArgument,
		{Name: "size_gb", Description: "Size in GB, 1 to 65536; with a source at least the size of the source. " +
			"Without it Google takes the size of the source or its default size"},
		{Name: "type", Description: "Disk type of the zone such as pd-balanced, pd-ssd, pd-standard, or " +
			"hyperdisk-balanced; never a path or URL. Without it Google takes pd-standard"},
		{Name: "source_image", Description: "Name of an image to restore onto the disk: of the project itself, or " +
			"of source_image_project. Only one source of source_image, source_image_family, source_snapshot, " +
			"source_disk, and source_instant_snapshot"},
		{Name: "source_image_family", Description: "Image family such as debian-12 whose latest image is restored: of " +
			"the project itself, or of source_image_project"},
		{Name: "source_image_project", Description: "One of Google's public image projects (" +
			strings.Join(publicImageProjects, ", ") + ") that holds source_image or source_image_family; without " +
			"it the image is one of the project itself. Any other project is refused"},
		{Name: "source_snapshot", Description: "Name of a snapshot of the project itself to restore onto the disk"},
		{Name: "source_disk", Description: "Name of a disk of the project itself in the same zone to clone"},
		{Name: "source_instant_snapshot", Description: "Name of an instant snapshot of the project itself in the same " +
			"zone to restore onto the disk"},
		diskLabelsArgument,
		{Name: "provisioned_iops", Description: "IOPS to provision, for disk types that take it; Google checks the " +
			"range of the type, and provisioned IOPS are billed"},
		{Name: "provisioned_throughput", Description: "Throughput to provision in MB per second, for disk types that " +
			"take it; Google checks the range of the type, and provisioned throughput is billed"},
		{Name: "physical_block_size_bytes", Description: "Physical block size, 4096 or 16384; Google refuses a size " +
			"it does not support for the project"},
		{Name: "licenses", Description: "Licenses to attach, each {license, project}: license is the name of a " +
			"license, project one of Google's public image projects, or left out for a license of the project " +
			"itself. Google reserves the field for its own use; at most 32"},
		diskKMSArgument, diskKMSAccountArgument,
		{Name: "resource_policies", Description: "Names of resource policies of the project in the region of the " +
			"zone, such as a snapshot schedule; at most 10"},
		{Name: "storage_pool", Description: "Name of a storage pool of the project in the same zone to create the " +
			"disk in"},
		{Name: "architecture", Description: "Architecture of the disk: ARM64 or X86_64"},
		{Name: "guest_os_features", Description: "Features to enable on the guest operating system of a bootable " +
			"disk, such as UEFI_COMPATIBLE or GVNIC; each once"},
		{Name: "access_mode", Description: "Access mode of a Hyperdisk: READ_WRITE_SINGLE (Google's default), " +
			"READ_WRITE_MANY, or READ_ONLY_MANY"},
		{Name: "enable_confidential_compute", Description: "True for a disk in confidential compute mode"},
		instanceRequestIDArgument},
	Fields: instanceLifecycleFields,
	Examples: []capability.Example{{
		Description: "Create a 20 GB balanced boot disk from the latest public Debian 12 image",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","name":"data-disk-a",` +
			`"size_gb":20,"type":"pd-balanced","source_image_family":"debian-12","source_image_project":"debian-cloud",` +
			`"labels":{"env":"test"}}`),
	}},
}

type diskLicense struct {
	Project string `json:"project"`
	License string `json:"license"`
}

type diskInsertArguments struct {
	ProjectID                 string            `json:"project_id"`
	Zone                      string            `json:"zone"`
	Name                      string            `json:"name"`
	Description               string            `json:"description"`
	SizeGb                    *int64            `json:"size_gb"`
	Type                      string            `json:"type"`
	SourceImage               string            `json:"source_image"`
	SourceImageFamily         string            `json:"source_image_family"`
	SourceImageProject        string            `json:"source_image_project"`
	SourceSnapshot            string            `json:"source_snapshot"`
	SourceDisk                string            `json:"source_disk"`
	SourceInstantSnapshot     string            `json:"source_instant_snapshot"`
	Labels                    map[string]string `json:"labels"`
	ProvisionedIops           *int64            `json:"provisioned_iops"`
	ProvisionedThroughput     *int64            `json:"provisioned_throughput"`
	PhysicalBlockSizeBytes    *int64            `json:"physical_block_size_bytes"`
	Licenses                  []diskLicense     `json:"licenses"`
	KmsKeyName                string            `json:"kms_key_name"`
	KmsKeyServiceAccount      string            `json:"kms_key_service_account"`
	ResourcePolicies          []string          `json:"resource_policies"`
	StoragePool               string            `json:"storage_pool"`
	Architecture              string            `json:"architecture"`
	GuestOsFeatures           []string          `json:"guest_os_features"`
	AccessMode                string            `json:"access_mode"`
	EnableConfidentialCompute *bool             `json:"enable_confidential_compute"`
	RequestID                 string            `json:"request_id"`
}

// optionalName reports whether value is empty or a resource name.
func optionalName(value string) bool { return value == "" || validComputeName(value) }

func inRange(value *int64, low, high int64) bool {
	return value == nil || *value >= low && *value <= high
}

// check refuses an argument that is not in its documented form, before any secret is read. The refusal never
// repeats the value.
func (a diskInsertArguments) check() error {
	switch {
	case !validProject(a.ProjectID):
		return badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(a.Zone):
		return badRequest("zone must be a zone name such as us-central1-a, never a path")
	case !validComputeName(a.Name):
		return badRequest("name must be a disk name such as data-disk-a")
	case !validDiskText(a.Description):
		return badRequest("description must be at most " + strconv.Itoa(maxDiskDescription) +
			" characters of valid text without control characters")
	case !inRange(a.SizeGb, 1, maxDiskSizeGb):
		return badRequest("size_gb must be 1 to " + strconv.Itoa(maxDiskSizeGb))
	case !optionalName(a.Type) || !optionalName(a.StoragePool):
		return badRequest("type and storage_pool must be names, never a path or URL")
	case !optionalName(a.SourceImage) || !optionalName(a.SourceImageFamily) || !optionalName(a.SourceSnapshot) ||
		!optionalName(a.SourceDisk) || !optionalName(a.SourceInstantSnapshot):
		return badRequest("a source must be a name, never a path or URL")
	case !inRange(a.ProvisionedIops, 1, maxDiskPerformance) || !inRange(a.ProvisionedThroughput, 1, maxDiskPerformance):
		return badRequest("provisioned_iops and provisioned_throughput must be 1 to " + strconv.Itoa(maxDiskPerformance))
	case a.PhysicalBlockSizeBytes != nil && *a.PhysicalBlockSizeBytes != 4096 && *a.PhysicalBlockSizeBytes != 16384:
		return badRequest("physical_block_size_bytes must be 4096 or 16384")
	case a.Architecture != "" && a.Architecture != "ARM64" && a.Architecture != "X86_64":
		return badRequest("architecture must be ARM64 or X86_64")
	case a.AccessMode != "" && !slices.Contains([]string{"READ_WRITE_SINGLE", "READ_WRITE_MANY", "READ_ONLY_MANY"},
		a.AccessMode):
		return badRequest("access_mode must be READ_WRITE_SINGLE, READ_WRITE_MANY, or READ_ONLY_MANY")
	}
	if err := a.checkSource(); err != nil {
		return err
	}
	if err := checkDiskLabels(a.Labels); err != nil {
		return err
	}
	if err := checkDiskKey(a.KmsKeyName, a.KmsKeyServiceAccount); err != nil {
		return err
	}
	if err := checkLicenses(a.Licenses); err != nil {
		return err
	}
	if len(a.ResourcePolicies) > maxDiskReferences {
		return badRequest("resource_policies takes at most " + strconv.Itoa(maxDiskReferences) + " names")
	}
	for i, policy := range a.ResourcePolicies {
		if !validComputeName(policy) || slices.Contains(a.ResourcePolicies[:i], policy) {
			return badRequest("resource_policies must be distinct resource policy names, never a path or URL")
		}
	}
	if _, ok := zoneRegion(a.Zone); len(a.ResourcePolicies) > 0 && !ok {
		return badRequest("zone names no region, so resource_policies cannot be placed")
	}
	return checkGuestOsFeatures(a.GuestOsFeatures)
}

// checkLicenses allows at most maxDiskLicenses licenses, each a name of the project itself or of one of Google's
// public image projects.
func checkLicenses(licenses []diskLicense) error {
	if len(licenses) > maxDiskLicenses {
		return badRequest("licenses takes at most " + strconv.Itoa(maxDiskLicenses) + " entries")
	}
	for _, license := range licenses {
		if !validComputeName(license.License) ||
			license.Project != "" && !slices.Contains(publicImageProjects, license.Project) {
			return badRequest("a license must be a license name, of the project itself or of one of Google's " +
				"public image projects")
		}
	}
	return nil
}

// licenseURLs builds the URL of each license; a license without a project is one of project.
func licenseURLs(project string, licenses []diskLicense) []string {
	urls := make([]string, len(licenses))
	for i, license := range licenses {
		owner := license.Project
		if owner == "" {
			owner = project
		}
		urls[i] = computeResourceURL("projects", owner, "global", "licenses", license.License)
	}
	return urls
}

func checkGuestOsFeatures(features []string) error {
	if len(features) > len(guestOsFeatureTypes) {
		return badRequest("guest_os_features names a feature twice")
	}
	for i, feature := range features {
		if !slices.Contains(guestOsFeatureTypes, feature) || slices.Contains(features[:i], feature) {
			return badRequest("guest_os_features must be distinct documented feature types")
		}
	}
	return nil
}

func guestOsFeatureBody(features []string) []map[string]string {
	out := make([]map[string]string, len(features))
	for i, feature := range features {
		out[i] = map[string]string{"type": feature}
	}
	return out
}

func checkStorageLocations(locations []string) error {
	if len(locations) > maxDiskReferences {
		return badRequest("storage_locations takes at most " + strconv.Itoa(maxDiskReferences) + " locations")
	}
	for i, location := range locations {
		if !validComputeName(location) || slices.Contains(locations[:i], location) {
			return badRequest("storage_locations must be distinct location names such as us or europe-west1")
		}
	}
	return nil
}

// checkSource allows at most one source, and source_image_project only with an image or image family of one of
// Google's public image projects.
func (a diskInsertArguments) checkSource() error {
	sources := 0
	for _, source := range []string{a.SourceImage, a.SourceImageFamily, a.SourceSnapshot, a.SourceDisk,
		a.SourceInstantSnapshot} {
		if source != "" {
			sources++
		}
	}
	switch {
	case sources > 1:
		return badRequest("name at most one source: source_image, source_image_family, source_snapshot, " +
			"source_disk, or source_instant_snapshot")
	case a.SourceImageProject != "" && a.SourceImage == "" && a.SourceImageFamily == "":
		return badRequest("source_image_project needs source_image or source_image_family")
	case a.SourceImageProject != "" && !slices.Contains(publicImageProjects, a.SourceImageProject):
		return badRequest("source_image_project must be one of Google's public image projects; an image of the " +
			"project itself needs no source_image_project")
	}
	return nil
}

// imageProject is the project of the source image: a public image project, or the project of the disk.
func (a diskInsertArguments) imageProject() string {
	if a.SourceImageProject != "" {
		return a.SourceImageProject
	}
	return a.ProjectID
}

// body builds the Disk Google takes from the checked arguments. Every reference is a URL built here.
func (a diskInsertArguments) body() map[string]any {
	p, z := a.ProjectID, a.Zone
	body := map[string]any{"name": a.Name}
	set := func(key, value string) {
		if value != "" {
			body[key] = value
		}
	}
	setInt := func(key string, value *int64) {
		if value != nil {
			body[key] = strconv.FormatInt(*value, 10)
		}
	}
	set("description", a.Description)
	setInt("sizeGb", a.SizeGb)
	if a.Type != "" {
		body["type"] = computeResourceURL("projects", p, "zones", z, "diskTypes", a.Type)
	}
	switch {
	case a.SourceImage != "":
		body["sourceImage"] = computeResourceURL("projects", a.imageProject(), "global", "images", a.SourceImage)
	case a.SourceImageFamily != "":
		body["sourceImage"] = computeResourceURL("projects", a.imageProject(), "global", "images", "family",
			a.SourceImageFamily)
	case a.SourceSnapshot != "":
		body["sourceSnapshot"] = computeResourceURL("projects", p, "global", "snapshots", a.SourceSnapshot)
	case a.SourceDisk != "":
		body["sourceDisk"] = computeResourceURL("projects", p, "zones", z, "disks", a.SourceDisk)
	case a.SourceInstantSnapshot != "":
		body["sourceInstantSnapshot"] = computeResourceURL("projects", p, "zones", z, "instantSnapshots",
			a.SourceInstantSnapshot)
	}
	if len(a.Labels) > 0 {
		body["labels"] = a.Labels
	}
	setInt("provisionedIops", a.ProvisionedIops)
	setInt("provisionedThroughput", a.ProvisionedThroughput)
	setInt("physicalBlockSizeBytes", a.PhysicalBlockSizeBytes)
	if len(a.Licenses) > 0 {
		body["licenses"] = licenseURLs(p, a.Licenses)
	}
	if a.KmsKeyName != "" {
		body["diskEncryptionKey"] = diskKeyBody(a.KmsKeyName, a.KmsKeyServiceAccount)
	}
	if len(a.ResourcePolicies) > 0 {
		region, _ := zoneRegion(z)
		policies := make([]string, len(a.ResourcePolicies))
		for i, policy := range a.ResourcePolicies {
			policies[i] = computeResourceURL("projects", p, "regions", region, "resourcePolicies", policy)
		}
		body["resourcePolicies"] = policies
	}
	if a.StoragePool != "" {
		body["storagePool"] = computeResourceURL("projects", p, "zones", z, "storagePools", a.StoragePool)
	}
	set("architecture", a.Architecture)
	if len(a.GuestOsFeatures) > 0 {
		body["guestOsFeatures"] = guestOsFeatureBody(a.GuestOsFeatures)
	}
	set("accessMode", a.AccessMode)
	if a.EnableConfidentialCompute != nil {
		body["enableConfidentialCompute"] = *a.EnableConfidentialCompute
	}
	return body
}

func handleDisksInsert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create disk"
	var a diskInsertArguments
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
	c, err := computeClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	return c.computeDelete(ctx, computeDeleteTarget{op: op, tool: disksInsertID, method: http.MethodPost,
		path: computeZonePath(a.ProjectID, a.Zone) + "/disks", zone: a.Zone, requestID: requestID,
		payload: payload, keyed: true})
}

// diskInsertSummary names project, zone, name, size, type, and source: what the disk costs and holds.
func diskInsertSummary(raw json.RawMessage) string {
	var a diskInsertArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	size := "the size of the source or Google's default size"
	if a.SizeGb != nil {
		size = strconv.FormatInt(*a.SizeGb, 10) + " GB"
	}
	diskType := "Google's default type pd-standard"
	if a.Type != "" {
		diskType = "type " + boundedText(a.Type, maxNameText)
	}
	source := "empty"
	switch {
	case a.SourceImage != "":
		source = "from image " + boundedText(a.SourceImage, maxNameText) + " of project " +
			boundedText(a.imageProject(), maxNameText)
	case a.SourceImageFamily != "":
		source = "from the latest image of family " + boundedText(a.SourceImageFamily, maxNameText) +
			" of project " + boundedText(a.imageProject(), maxNameText)
	case a.SourceSnapshot != "":
		source = "from snapshot " + boundedText(a.SourceSnapshot, maxNameText)
	case a.SourceDisk != "":
		source = "as a clone of disk " + boundedText(a.SourceDisk, maxNameText)
	case a.SourceInstantSnapshot != "":
		source = "from instant snapshot " + boundedText(a.SourceInstantSnapshot, maxNameText)
	}
	out := "Create disk " + boundedText(a.Name, maxNameText) + " in zone " + boundedText(a.Zone, maxNameText) +
		" of project " + boundedText(a.ProjectID, maxNameText) + ": " + size + ", " + diskType + ", " + source
	if a.ProvisionedIops != nil {
		out += ", " + strconv.FormatInt(*a.ProvisionedIops, 10) + " provisioned IOPS"
	}
	if a.ProvisionedThroughput != nil {
		out += ", " + strconv.FormatInt(*a.ProvisionedThroughput, 10) + " MB/s provisioned throughput"
	}
	return out + "; the disk is billed for its size and type, and for what is provisioned, until it is deleted"
}

// --- disks.resize ---

var disksResize = capability.Descriptor{
	ID: disksResizeID, Version: 1, Title: "Grow a Compute Engine disk",
	Description: "Grow one persistent disk or Hyperdisk of a zone of a project to a larger size (disks.resize of " +
		"Compute Engine v1), by its name or numeric ID. A disk can never be shrunk again, and the larger size is " +
		"billed until the disk is deleted; the file system in the guest must be grown separately. After the " +
		"confirmation the tool reads the disk and sends the change only if size_gb is larger than its current " +
		"size; otherwise it fails without a change." + diskChangeNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "disks", "resize"}, Risk: diskUpdateRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeZoneProp + `,` +
		diskOrIDProp + `,"size_gb":{"type":"integer","minimum":1,"maximum":65536},` + computeRequestIDProp +
		`},"required":["project_id","zone","disk","size_gb"],"additionalProperties":false}`),
	OutputSchema: instanceLifecycleOutput,
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument, diskOrIDArgument,
		{Name: "size_gb", Required: true, Description: "New size in GB, larger than the current size (sizeGb of " +
			"disks.get), at most 65536"},
		instanceRequestIDArgument},
	Fields: instanceLifecycleFields,
	Examples: []capability.Example{{
		Description: "Grow a disk to 200 GB",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","disk":"data-disk-a","size_gb":200}`),
	}},
}

type diskResizeArguments struct {
	ProjectID string `json:"project_id"`
	Zone      string `json:"zone"`
	Disk      string `json:"disk"`
	SizeGb    *int64 `json:"size_gb"`
	RequestID string `json:"request_id"`
}

func checkDiskTarget(projectID, zone, disk string) error {
	switch {
	case !validProject(projectID):
		return badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(zone):
		return badRequest("zone must be a zone name such as us-central1-a, never a path")
	case !validComputeNameOrID(disk):
		return badRequest("disk must be a disk name or its numeric ID, never a path")
	}
	return nil
}

func handleDisksResize(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "resize disk"
	var a diskResizeArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkDiskTarget(a.ProjectID, a.Zone, a.Disk); err != nil {
		return nil, err
	}
	if a.SizeGb == nil || !inRange(a.SizeGb, 1, maxDiskSizeGb) {
		return nil, badRequest("size_gb must be 1 to " + strconv.Itoa(maxDiskSizeGb))
	}
	requestID, err := checkRequestID(a.RequestID)
	if err != nil {
		return nil, err
	}
	c, err := computeClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	// The disk is read first so that a size that would not grow it is refused without a change. The read uses
	// the token of this tool, whose scope covers it.
	path := computeZonePath(a.ProjectID, a.Zone) + "/disks/" + url.PathEscape(a.Disk)
	var wire json.RawMessage
	if err := c.get(ctx, op, disksResizeID, serviceCompute, path, nil, &wire); err != nil {
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
	sizeText, _ := row["sizeGb"].(string)
	current, err := strconv.ParseInt(sizeText, 10, 64)
	if id, _ := row["id"].(string); err != nil || current < 1 || name != a.Disk && id != a.Disk ||
		!inZone(row, a.Zone) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	if *a.SizeGb <= current {
		return nil, badRequest("size_gb must be larger than the current size of the disk, because a disk cannot " +
			"be shrunk; nothing was changed. Read the size with disks.get")
	}
	payload, err := json.Marshal(map[string]string{"sizeGb": strconv.FormatInt(*a.SizeGb, 10)})
	if err != nil {
		return nil, failure(op, "the request could not be built")
	}
	return c.computeDelete(ctx, computeDeleteTarget{op: op, tool: disksResizeID, method: http.MethodPost,
		path: path + "/resize", zone: a.Zone, requestID: requestID, payload: payload, keyed: true})
}

// diskResizeSummary names project, zone, disk, and the new size, and that the change cannot be undone.
func diskResizeSummary(raw json.RawMessage) string {
	var a diskResizeArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	size := "?"
	if a.SizeGb != nil {
		size = strconv.FormatInt(*a.SizeGb, 10)
	}
	return "Grow disk " + boundedText(a.Disk, maxNameText) + " in zone " + boundedText(a.Zone, maxNameText) +
		" of project " + boundedText(a.ProjectID, maxNameText) + " to " + size + " GB: a disk cannot be shrunk " +
		"again, and the larger size is billed until the disk is deleted; nothing is changed if the disk is " +
		"already this size or larger"
}

// --- disks.create-snapshot ---

var disksCreateSnapshot = capability.Descriptor{
	ID: disksCreateSnapshotID, Version: 1, Title: "Create a snapshot of a Compute Engine disk",
	Description: "Create one snapshot of a persistent disk or Hyperdisk of a zone of a project (disks.createSnapshot " +
		"of Compute Engine v1), by the name or numeric ID of the disk. The snapshot is a global resource of the " +
		"project, stored in the storage location given or chosen by Google, and billed for its storage until it is " +
		"deleted; snapshots.get reads it. With guest_flush Google asks the guest to prepare for an application " +
		"consistent snapshot." + diskChangeNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "disks", "snapshots", "create"}, Risk: diskCreateRisk,
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` + computeZoneProp + `,` +
		diskOrIDProp + `,` + nameProp("name") + `,` + diskDescriptionProp + `,` + diskLabelsProp + `,` +
		storageLocationsProp + `,` +
		`"snapshot_type":{"type":"string","enum":["STANDARD","ARCHIVE"]},` + nameProp("chain_name") + `,` +
		diskKMSProps + `,"guest_flush":{"type":"boolean"},` + computeRequestIDProp +
		`},"required":["project_id","zone","disk","name"],"additionalProperties":false}`),
	OutputSchema: instanceLifecycleOutput,
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument, diskOrIDArgument,
		{Name: "name", Required: true, Description: "Name of the new snapshot such as data-disk-a-daily: a lowercase " +
			"letter, then up to 62 lowercase letters, digits, or '-', not ending in '-'"},
		diskDescriptionArgument, diskLabelsArgument,
		{Name: "storage_locations", Description: "Cloud Storage location of the snapshot, regional or " +
			"multi-regional, such as us or europe-west1; Google currently takes one. Without it Google picks the " +
			"multi-region nearest the disk"},
		{Name: "snapshot_type", Description: "STANDARD (Google's default) or ARCHIVE, which costs less to store and " +
			"more to restore"},
		{Name: "chain_name", Description: "Name of the snapshot chain to create the snapshot in, for separate " +
			"chains such as for chargeback; an uncommon option"},
		diskKMSArgument, diskKMSAccountArgument,
		{Name: "guest_flush", Description: "True to ask the guest operating system to prepare for an application " +
			"consistent snapshot (Google's guestFlush)"},
		instanceRequestIDArgument},
	Fields: instanceLifecycleFields,
	Examples: []capability.Example{{
		Description: "Snapshot a disk into a US multi-region",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","disk":"data-disk-a",` +
			`"name":"data-disk-a-daily","storage_locations":["us"]}`),
	}},
}

type diskSnapshotArguments struct {
	ProjectID            string            `json:"project_id"`
	Zone                 string            `json:"zone"`
	Disk                 string            `json:"disk"`
	Name                 string            `json:"name"`
	Description          string            `json:"description"`
	Labels               map[string]string `json:"labels"`
	StorageLocations     []string          `json:"storage_locations"`
	SnapshotType         string            `json:"snapshot_type"`
	ChainName            string            `json:"chain_name"`
	KmsKeyName           string            `json:"kms_key_name"`
	KmsKeyServiceAccount string            `json:"kms_key_service_account"`
	GuestFlush           bool              `json:"guest_flush"`
	RequestID            string            `json:"request_id"`
}

func (a diskSnapshotArguments) check() error {
	if err := checkDiskTarget(a.ProjectID, a.Zone, a.Disk); err != nil {
		return err
	}
	switch {
	case !validComputeName(a.Name):
		return badRequest("name must be a snapshot name such as data-disk-a-daily")
	case !validDiskText(a.Description):
		return badRequest("description must be at most " + strconv.Itoa(maxDiskDescription) +
			" characters of valid text without control characters")
	case a.SnapshotType != "" && a.SnapshotType != "STANDARD" && a.SnapshotType != "ARCHIVE":
		return badRequest("snapshot_type must be STANDARD or ARCHIVE")
	case !optionalName(a.ChainName):
		return badRequest("chain_name must be 1 to 63 lowercase letters, digits, or '-' starting with a letter")
	}
	if err := checkStorageLocations(a.StorageLocations); err != nil {
		return err
	}
	if err := checkDiskLabels(a.Labels); err != nil {
		return err
	}
	return checkDiskKey(a.KmsKeyName, a.KmsKeyServiceAccount)
}

func (a diskSnapshotArguments) body() map[string]any {
	body := map[string]any{"name": a.Name}
	if a.Description != "" {
		body["description"] = a.Description
	}
	if len(a.Labels) > 0 {
		body["labels"] = a.Labels
	}
	if len(a.StorageLocations) > 0 {
		body["storageLocations"] = a.StorageLocations
	}
	if a.SnapshotType != "" {
		body["snapshotType"] = a.SnapshotType
	}
	if a.ChainName != "" {
		body["chainName"] = a.ChainName
	}
	if a.KmsKeyName != "" {
		body["snapshotEncryptionKey"] = diskKeyBody(a.KmsKeyName, a.KmsKeyServiceAccount)
	}
	return body
}

func handleDisksCreateSnapshot(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create disk snapshot"
	var a diskSnapshotArguments
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
	if a.GuestFlush {
		query = url.Values{"guestFlush": {"true"}}
	}
	c, err := computeClient(ctx, op, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := computeZonePath(a.ProjectID, a.Zone) + "/disks/" + url.PathEscape(a.Disk) + "/createSnapshot"
	return c.computeDelete(ctx, computeDeleteTarget{op: op, tool: disksCreateSnapshotID, method: http.MethodPost,
		path: path, zone: a.Zone, requestID: requestID, payload: payload, query: query, keyed: true})
}

// diskSnapshotSummary names project, zone, disk, snapshot, type, and storage location: what the snapshot costs.
func diskSnapshotSummary(raw json.RawMessage) string {
	var a diskSnapshotArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	snapshotType := "STANDARD"
	if a.SnapshotType != "" {
		snapshotType = boundedText(a.SnapshotType, maxNameText)
	}
	location := "a location Google picks near the disk"
	if len(a.StorageLocations) > 0 {
		location = strings.Join(boundedTexts(a.StorageLocations, maxNameText), ", ")
	}
	out := "Create snapshot " + boundedText(a.Name, maxNameText) + " of disk " + boundedText(a.Disk, maxNameText) +
		" in zone " + boundedText(a.Zone, maxNameText) + " of project " + boundedText(a.ProjectID, maxNameText) +
		" (type " + snapshotType + ", stored in " + location
	if a.GuestFlush {
		out += ", with guest flush"
	}
	return out + "): the snapshot is billed for its storage until it is deleted"
}
