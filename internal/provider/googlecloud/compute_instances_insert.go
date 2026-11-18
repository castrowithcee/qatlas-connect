package googlecloud

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const instancesInsertID = Provider + ".instances.insert"

// This file builds the one request of instances.insert. The tool never passes a body through: every argument
// is a structured member, each name is turned into a path below the project and zone of the call, and what
// would open the instance to an outside party has no argument and is refused if it arrives anyway.

const (
	maxInstanceDisks      = 24
	maxInstanceInterfaces = 8
	maxMetadataItems      = 128
	maxMetadataValue      = 262144
	maxMetadataTotal      = 524288
	maxInstanceLabels     = 64
	maxInstanceTags       = 64
	maxServiceScopes      = 30
	maxAccelerators       = 8
)

var (
	metadataKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	instanceLabelKey   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	instanceLabelValue = regexp.MustCompile(`^[a-z0-9_-]{0,63}$`)
	tagPattern         = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`)
	scopeURLPattern    = regexp.MustCompile(`^https://www\.googleapis\.com/auth/[A-Za-z0-9._-]{1,128}$`)
	cpuPlatformPattern = regexp.MustCompile(`^[A-Za-z0-9 ._-]{1,64}$`)
	kmsKeyPattern      = regexp.MustCompile(`^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/[a-z0-9-]{1,63}/keyRings/[A-Za-z0-9_-]{1,63}/cryptoKeys/[A-Za-z0-9_-]{1,63}(/cryptoKeyVersions/[0-9]{1,10})?$`)
	ownAccountLocal    = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	defaultAccount     = regexp.MustCompile(`^([0-9]{1,20})-compute@developer\.gserviceaccount\.com$`)
	zoneRegionPattern  = regexp.MustCompile(`^([a-z]+(?:-[a-z0-9]+)+)-[a-z]$`)
)

// sshAccessKeys are the metadata keys through which a guest accepts the public key of a person. The comparison
// ignores case, which is narrower than Google's own: a spelling Google would treat as another key is refused
// as well. windows-keys carries the public key of a Windows password reset, so it opens an account like
// ssh-keys does. block-project-ssh-keys only closes access and stays allowed.
var sshAccessKeys = []string{"ssh-keys", "sshkeys", "windows-keys"}

// forbiddenMembers are names no argument may carry at any depth, compared without case, hyphens, or
// underscores: external addresses and the key material of customer-supplied encryption keys.
var forbiddenMembers = map[string]string{
	"accessconfigs":     "network interfaces take no accessConfigs: an instance is created without an external address",
	"ipv6accessconfigs": "network interfaces take no ipv6AccessConfigs: an instance is created without an external address",
	"rawkey":            "customer-supplied key material is no argument; use kms_key_name",
	"rsaencryptedkey":   "customer-supplied key material is no argument; use kms_key_name",
}

type metadataArgument struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type initializeArguments struct {
	DiskName              string            `json:"disk_name"`
	DiskSizeGB            *int64            `json:"disk_size_gb"`
	DiskType              string            `json:"disk_type"`
	SourceImage           string            `json:"source_image"`
	Description           string            `json:"description"`
	Labels                map[string]string `json:"labels"`
	ProvisionedIops       *int64            `json:"provisioned_iops"`
	ProvisionedThroughput *int64            `json:"provisioned_throughput"`
}

type diskArgument struct {
	Type             string               `json:"type"`
	Boot             *bool                `json:"boot"`
	AutoDelete       *bool                `json:"auto_delete"`
	Mode             string               `json:"mode"`
	Interface        string               `json:"interface"`
	DeviceName       string               `json:"device_name"`
	Source           string               `json:"source"`
	InitializeParams *initializeArguments `json:"initialize_params"`
	KMSKeyName       string               `json:"kms_key_name"`
}

type interfaceArgument struct {
	Network    string `json:"network"`
	Subnetwork string `json:"subnetwork"`
	NetworkIP  string `json:"network_ip"`
	NicType    string `json:"nic_type"`
	StackType  string `json:"stack_type"`
	QueueCount *int   `json:"queue_count"`
}

type instanceAccountArgument struct {
	Email  string   `json:"email"`
	Scopes []string `json:"scopes"`
}

type schedulingArgument struct {
	ProvisioningModel         string `json:"provisioning_model"`
	OnHostMaintenance         string `json:"on_host_maintenance"`
	AutomaticRestart          *bool  `json:"automatic_restart"`
	InstanceTerminationAction string `json:"instance_termination_action"`
	MaxRunDurationSeconds     *int64 `json:"max_run_duration_seconds"`
}

type acceleratorArgument struct {
	Type  string `json:"type"`
	Count *int   `json:"count"`
}

type shieldedArgument struct {
	EnableSecureBoot          *bool `json:"enable_secure_boot"`
	EnableVtpm                *bool `json:"enable_vtpm"`
	EnableIntegrityMonitoring *bool `json:"enable_integrity_monitoring"`
}

type confidentialArgument struct {
	EnableConfidentialCompute *bool  `json:"enable_confidential_compute"`
	ConfidentialInstanceType  string `json:"confidential_instance_type"`
}

type reservationArgument struct {
	ConsumeReservationType string   `json:"consume_reservation_type"`
	Values                 []string `json:"values"`
}

type displayArgument struct {
	EnableDisplay *bool `json:"enable_display"`
}

type machineFeaturesArgument struct {
	EnableNestedVirtualization *bool `json:"enable_nested_virtualization"`
	ThreadsPerCore             *int  `json:"threads_per_core"`
	VisibleCoreCount           *int  `json:"visible_core_count"`
	EnableUefiNetworking       *bool `json:"enable_uefi_networking"`
}

type insertArguments struct {
	ProjectID                  string                     `json:"project_id"`
	Zone                       string                     `json:"zone"`
	RequestID                  string                     `json:"request_id"`
	Name                       string                     `json:"name"`
	Description                string                     `json:"description"`
	MachineType                string                     `json:"machine_type"`
	SourceInstanceTemplate     string                     `json:"source_instance_template"`
	SourceMachineImage         string                     `json:"source_machine_image"`
	Disks                      []diskArgument             `json:"disks"`
	NetworkInterfaces          []interfaceArgument        `json:"network_interfaces"`
	ServiceAccounts            *[]instanceAccountArgument `json:"service_accounts"`
	Metadata                   []metadataArgument         `json:"metadata"`
	Labels                     map[string]string          `json:"labels"`
	Tags                       []string                   `json:"tags"`
	Scheduling                 *schedulingArgument        `json:"scheduling"`
	GuestAccelerators          []acceleratorArgument      `json:"guest_accelerators"`
	MinCPUPlatform             string                     `json:"min_cpu_platform"`
	ShieldedInstanceConfig     *shieldedArgument          `json:"shielded_instance_config"`
	ConfidentialInstanceConfig *confidentialArgument      `json:"confidential_instance_config"`
	CanIPForward               *bool                      `json:"can_ip_forward"`
	DeletionProtection         *bool                      `json:"deletion_protection"`
	ReservationAffinity        *reservationArgument       `json:"reservation_affinity"`
	DisplayDevice              *displayArgument           `json:"display_device"`
	AdvancedMachineFeatures    *machineFeaturesArgument   `json:"advanced_machine_features"`
}

// decodeInsertArguments reads the arguments strictly and refuses a forbidden member at any depth, so that
// nothing reaches the body by a field the schema did not name.
func decodeInsertArguments(raw json.RawMessage) (insertArguments, error) {
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return insertArguments{}, badRequest("the arguments are not a JSON object")
	}
	if message := forbiddenMember(generic); message != "" {
		return insertArguments{}, badRequest(message)
	}
	var a insertArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&a); err != nil {
		return insertArguments{}, badRequest("an argument is not one of the arguments of this tool or has the wrong type")
	}
	return a, nil
}

func forbiddenMember(node any) string {
	switch node := node.(type) {
	case map[string]any:
		for name, value := range node {
			normal := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(name))
			if message, found := forbiddenMembers[normal]; found {
				return message
			}
			if message := forbiddenMember(value); message != "" {
				return message
			}
		}
	case []any:
		for _, item := range node {
			if message := forbiddenMember(item); message != "" {
				return message
			}
		}
	}
	return ""
}

// insertTarget is what the paths of a call are built from. region is the region of the zone.
type insertTarget struct{ project, zone, region string }

// resource turns a name or a relative path into the path of a resource of this project: NAME,
// SCOPE/COLLECTION/NAME, or projects/PROJECT/SCOPE/COLLECTION/NAME, where PROJECT is the project of the call.
// scopes lists the places the resource may lie, the first being the one a bare name means.
func (t insertTarget) resource(argument, value, collection string, scopes ...string) (string, error) {
	fail := badRequest(argument + " must be a name, or a path of the project, zone, or region of this call, " +
		"never a URL and never a resource of another project")
	scopeSegments := func(scope string) []string {
		switch scope {
		case scopeZone:
			return []string{"zones", t.zone}
		case scopeRegion:
			return []string{"regions", t.region}
		}
		return []string{"global"}
	}
	parts := strings.Split(value, "/")
	if len(parts) == 1 {
		if !validComputeName(value) {
			return "", fail
		}
		return t.path(scopeSegments(scopes[0]), collection, value), nil
	}
	if parts[0] == "projects" {
		if len(parts) < 3 || parts[1] != t.project {
			return "", fail
		}
		parts = parts[2:]
	}
	for _, scope := range scopes {
		segments := scopeSegments(scope)
		if len(parts) == len(segments)+2 && strings.Join(parts[:len(segments)], "/") == strings.Join(segments, "/") &&
			parts[len(segments)] == collection && validComputeName(parts[len(parts)-1]) {
			return t.path(segments, collection, parts[len(parts)-1]), nil
		}
	}
	return "", fail
}

func (t insertTarget) path(scope []string, collection, name string) string {
	return "projects/" + t.project + "/" + strings.Join(scope, "/") + "/" + collection + "/" + name
}

// image resolves a source image: an image or family of this project, or the family of another project as a
// project and family pair such as projects/debian-cloud/global/images/family/debian-12. No image of another
// project is accepted by its name.
func (t insertTarget) image(value string) (string, error) {
	fail := badRequest("source_image must be an image or image family of this project, or an image family of " +
		"another project as projects/PROJECT/global/images/family/FAMILY, never a URL")
	parts := strings.Split(value, "/")
	project := t.project
	if parts[0] == "projects" && len(parts) >= 3 {
		project, parts = parts[1], parts[2:]
		if project != t.project && (!projectIDShape.MatchString(project) ||
			len(parts) != 4 || parts[0] != "global" || parts[1] != "images" || parts[2] != "family") {
			return "", fail
		}
	}
	var family string
	var name string
	switch {
	case len(parts) == 1:
		name = parts[0]
	case len(parts) == 2 && parts[0] == "family":
		family = parts[1]
	case len(parts) == 3 && parts[0] == "global" && parts[1] == "images":
		name = parts[2]
	case len(parts) == 4 && parts[0] == "global" && parts[1] == "images" && parts[2] == "family":
		family = parts[3]
	default:
		return "", fail
	}
	switch {
	case family != "" && validComputeName(family):
		return "projects/" + project + "/global/images/family/" + family, nil
	case name != "" && validComputeName(name):
		return "projects/" + project + "/global/images/" + name, nil
	}
	return "", fail
}

func checkMetadata(items []metadataArgument) error {
	if len(items) > maxMetadataItems {
		return badRequest("metadata takes at most " + strconv.Itoa(maxMetadataItems) + " entries")
	}
	seen := map[string]bool{}
	total := 0
	for _, item := range items {
		if !metadataKeyPattern.MatchString(item.Key) {
			return badRequest("a metadata key must be 1 to 128 letters, digits, '_' or '-'")
		}
		for _, forbidden := range sshAccessKeys {
			if strings.EqualFold(item.Key, forbidden) {
				return badRequest("metadata must not hold ssh-keys, sshKeys, or windows-keys: no tool adds the key " +
					"of a person to an instance; block-project-ssh-keys is allowed")
			}
		}
		if seen[item.Key] {
			return badRequest("a metadata key must not repeat")
		}
		seen[item.Key] = true
		if len(item.Value) > maxMetadataValue {
			return badRequest("a metadata value holds at most " + strconv.Itoa(maxMetadataValue) + " bytes")
		}
		total += len(item.Key) + len(item.Value)
	}
	if total > maxMetadataTotal {
		return badRequest("metadata holds at most " + strconv.Itoa(maxMetadataTotal) + " bytes in all")
	}
	return nil
}

func checkLabels(name string, labels map[string]string) error {
	if len(labels) > maxInstanceLabels {
		return badRequest(name + " takes at most " + strconv.Itoa(maxInstanceLabels) + " entries")
	}
	for key, value := range labels {
		if !instanceLabelKey.MatchString(key) || !instanceLabelValue.MatchString(value) {
			return badRequest(name + " keys are lowercase letters, digits, '_' and '-' starting with a letter, " +
				"values lowercase letters, digits, '_' and '-', each at most 63 characters")
		}
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	if value == "" {
		return true
	}
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func within(value *int64, low, high int64) bool {
	return value == nil || (*value >= low && *value <= high)
}

func withinInt(value *int, low, high int) bool {
	return value == nil || (*value >= low && *value <= high)
}

// check refuses before any secret is read whatever cannot be right in any project, and returns the body
// without the service accounts, which need the project read.
func (a insertArguments) check() (map[string]any, insertTarget, error) {
	t := insertTarget{project: a.ProjectID, zone: a.Zone}
	switch {
	case !validProject(a.ProjectID):
		return nil, t, badRequest("project_id must be a project ID or a project number, never a projects/ name or a path")
	case !validComputeName(a.Zone):
		return nil, t, badRequest("zone must be a zone name such as us-central1-a, never a path")
	case !validComputeName(a.Name):
		return nil, t, badRequest("name must be an instance name such as my-vm: lowercase letters, digits, and '-', " +
			"at most 63 characters")
	case len(a.Description) > maxText:
		return nil, t, badRequest("description holds at most " + strconv.Itoa(maxText) + " bytes")
	}
	match := zoneRegionPattern.FindStringSubmatch(a.Zone)
	if match == nil {
		return nil, t, badRequest("zone must be a zone name such as us-central1-a")
	}
	t.region = match[1]
	templated := a.SourceInstanceTemplate != "" || a.SourceMachineImage != ""
	body := map[string]any{"name": a.Name}
	if a.SourceInstanceTemplate != "" && a.SourceMachineImage != "" {
		return nil, t, badRequest("source_instance_template and source_machine_image exclude each other")
	}
	if templated {
		// A template or machine image can bring its own network interfaces and service accounts, and with
		// them an external address or a foreign account, so the call must state both itself.
		if len(a.NetworkInterfaces) == 0 || a.ServiceAccounts == nil {
			return nil, t, badRequest("with source_instance_template or source_machine_image, network_interfaces " +
				"and service_accounts must be given as well (service_accounts may be empty), because the checks " +
				"of this tool cannot read what the template or image holds")
		}
	}
	if a.SourceInstanceTemplate != "" {
		path, err := t.resource("source_instance_template", a.SourceInstanceTemplate, "instanceTemplates",
			scopeGlobal, scopeRegion)
		if err != nil {
			return nil, t, err
		}
		body["sourceInstanceTemplate"] = path
	}
	if a.SourceMachineImage != "" {
		path, err := t.resource("source_machine_image", a.SourceMachineImage, "machineImages", scopeGlobal)
		if err != nil {
			return nil, t, err
		}
		body["sourceMachineImage"] = path
	}
	if a.MachineType == "" && !templated {
		return nil, t, badRequest("machine_type is required unless source_instance_template or source_machine_image is given")
	}
	if a.MachineType != "" {
		path, err := t.resource("machine_type", a.MachineType, "machineTypes", scopeZone)
		if err != nil {
			return nil, t, err
		}
		body["machineType"] = path
	}
	if a.Description != "" {
		body["description"] = a.Description
	}
	if len(a.Disks) == 0 && !templated {
		return nil, t, badRequest("disks needs at least the boot disk")
	}
	if len(a.Disks) > 0 {
		disks, err := a.diskBody(t)
		if err != nil {
			return nil, t, err
		}
		body["disks"] = disks
	}
	if len(a.NetworkInterfaces) > 0 {
		nics, err := a.interfaceBody(t)
		if err != nil {
			return nil, t, err
		}
		body["networkInterfaces"] = nics
	}
	if err := a.optionBody(t, body); err != nil {
		return nil, t, err
	}
	return body, t, nil
}

func (a insertArguments) diskBody(t insertTarget) ([]any, error) {
	if len(a.Disks) > maxInstanceDisks {
		return nil, badRequest("disks takes at most " + strconv.Itoa(maxInstanceDisks) + " entries")
	}
	boots := 0
	disks := make([]any, 0, len(a.Disks))
	for _, d := range a.Disks {
		if !oneOf(d.Type, "PERSISTENT", "SCRATCH") || !oneOf(d.Mode, "READ_WRITE", "READ_ONLY") ||
			!oneOf(d.Interface, "SCSI", "NVME") {
			return nil, badRequest("a disk has a type of PERSISTENT or SCRATCH, a mode of READ_WRITE or READ_ONLY, " +
				"and an interface of SCSI or NVME")
		}
		if d.DeviceName != "" && !validComputeName(d.DeviceName) {
			return nil, badRequest("device_name must be a name of lowercase letters, digits, and '-'")
		}
		if d.Source != "" && d.InitializeParams != nil {
			return nil, badRequest("a disk takes either source or initialize_params, not both")
		}
		disk := map[string]any{}
		if d.Boot != nil {
			disk["boot"] = *d.Boot
			if *d.Boot {
				boots++
			}
		}
		if d.AutoDelete != nil {
			disk["autoDelete"] = *d.AutoDelete
		}
		for key, value := range map[string]string{"type": d.Type, "mode": d.Mode, "interface": d.Interface,
			"deviceName": d.DeviceName} {
			if value != "" {
				disk[key] = value
			}
		}
		if d.Source != "" {
			path, err := t.resource("source", d.Source, "disks", scopeZone)
			if err != nil {
				return nil, err
			}
			disk["source"] = path
		}
		if d.KMSKeyName != "" {
			if !kmsKeyPattern.MatchString(d.KMSKeyName) {
				return nil, badRequest("kms_key_name must be projects/PROJECT/locations/LOCATION/keyRings/RING/" +
					"cryptoKeys/KEY, optionally with /cryptoKeyVersions/VERSION")
			}
			disk["diskEncryptionKey"] = map[string]any{"kmsKeyName": d.KMSKeyName}
		}
		if p := d.InitializeParams; p != nil {
			params, err := p.body(t)
			if err != nil {
				return nil, err
			}
			disk["initializeParams"] = params
		}
		disks = append(disks, disk)
	}
	if boots > 1 {
		return nil, badRequest("at most one disk is the boot disk")
	}
	return disks, nil
}

func (p initializeArguments) body(t insertTarget) (map[string]any, error) {
	if !within(p.DiskSizeGB, 1, 65536) || !within(p.ProvisionedIops, 1, 1000000) ||
		!within(p.ProvisionedThroughput, 1, 1000000) {
		return nil, badRequest("disk_size_gb is 1 to 65536, provisioned_iops and provisioned_throughput 1 to 1000000")
	}
	if p.DiskName != "" && !validComputeName(p.DiskName) {
		return nil, badRequest("disk_name must be a name of lowercase letters, digits, and '-'")
	}
	if len(p.Description) > maxText {
		return nil, badRequest("a disk description holds at most " + strconv.Itoa(maxText) + " bytes")
	}
	if err := checkLabels("disk labels", p.Labels); err != nil {
		return nil, err
	}
	params := map[string]any{}
	if p.DiskName != "" {
		params["diskName"] = p.DiskName
	}
	if p.DiskSizeGB != nil {
		params["diskSizeGb"] = strconv.FormatInt(*p.DiskSizeGB, 10)
	}
	if p.ProvisionedIops != nil {
		params["provisionedIops"] = strconv.FormatInt(*p.ProvisionedIops, 10)
	}
	if p.ProvisionedThroughput != nil {
		params["provisionedThroughput"] = strconv.FormatInt(*p.ProvisionedThroughput, 10)
	}
	if p.Description != "" {
		params["description"] = p.Description
	}
	if len(p.Labels) > 0 {
		params["labels"] = p.Labels
	}
	if p.DiskType != "" {
		path, err := t.resource("disk_type", p.DiskType, "diskTypes", scopeZone)
		if err != nil {
			return nil, err
		}
		params["diskType"] = path
	}
	if p.SourceImage != "" {
		path, err := t.image(p.SourceImage)
		if err != nil {
			return nil, err
		}
		params["sourceImage"] = path
	}
	return params, nil
}

func (a insertArguments) interfaceBody(t insertTarget) ([]any, error) {
	if len(a.NetworkInterfaces) > maxInstanceInterfaces {
		return nil, badRequest("network_interfaces takes at most " + strconv.Itoa(maxInstanceInterfaces) + " entries")
	}
	nics := make([]any, 0, len(a.NetworkInterfaces))
	for _, n := range a.NetworkInterfaces {
		if !oneOf(n.NicType, "VIRTIO_NET", "GVNIC") || !oneOf(n.StackType, "IPV4_ONLY", "IPV4_IPV6", "IPV6_ONLY") ||
			!withinInt(n.QueueCount, 1, 512) {
			return nil, badRequest("a network interface has nic_type VIRTIO_NET or GVNIC, stack_type IPV4_ONLY, " +
				"IPV4_IPV6, or IPV6_ONLY, and queue_count 1 to 512")
		}
		nic := map[string]any{}
		if n.Network != "" {
			path, err := t.resource("network", n.Network, "networks", scopeGlobal)
			if err != nil {
				return nil, err
			}
			nic["network"] = path
		}
		if n.Subnetwork != "" {
			path, err := t.resource("subnetwork", n.Subnetwork, "subnetworks", scopeRegion)
			if err != nil {
				return nil, err
			}
			nic["subnetwork"] = path
		}
		if n.NetworkIP != "" {
			if address, err := netip.ParseAddr(n.NetworkIP); err != nil || !address.Is4() {
				return nil, badRequest("network_ip must be an IPv4 address")
			}
			nic["networkIP"] = n.NetworkIP
		}
		if n.NicType != "" {
			nic["nicType"] = n.NicType
		}
		if n.StackType != "" {
			nic["stackType"] = n.StackType
		}
		if n.QueueCount != nil {
			nic["queueCount"] = *n.QueueCount
		}
		nics = append(nics, nic)
	}
	return nics, nil
}

// optionBody adds the members that need no project read. The service accounts are added by the handler.
func (a insertArguments) optionBody(t insertTarget, body map[string]any) error {
	if err := checkMetadata(a.Metadata); err != nil {
		return err
	}
	if len(a.Metadata) > 0 {
		items := make([]any, 0, len(a.Metadata))
		for _, item := range a.Metadata {
			items = append(items, map[string]string{"key": item.Key, "value": item.Value})
		}
		body["metadata"] = map[string]any{"items": items}
	}
	if err := checkLabels("labels", a.Labels); err != nil {
		return err
	}
	if len(a.Labels) > 0 {
		body["labels"] = a.Labels
	}
	if len(a.Tags) > maxInstanceTags {
		return badRequest("tags takes at most " + strconv.Itoa(maxInstanceTags) + " entries")
	}
	for _, tag := range a.Tags {
		if !tagPattern.MatchString(tag) {
			return badRequest("a tag is a name of lowercase letters, digits, and '-', at most 63 characters")
		}
	}
	if len(a.Tags) > 0 {
		body["tags"] = map[string]any{"items": a.Tags}
	}
	if s := a.Scheduling; s != nil {
		if !oneOf(s.ProvisioningModel, "STANDARD", "SPOT") || !oneOf(s.OnHostMaintenance, "MIGRATE", "TERMINATE") ||
			!oneOf(s.InstanceTerminationAction, "STOP", "DELETE") || !within(s.MaxRunDurationSeconds, 1, 315576000000) {
			return badRequest("scheduling has provisioning_model STANDARD or SPOT, on_host_maintenance MIGRATE or " +
				"TERMINATE, instance_termination_action STOP or DELETE, and max_run_duration_seconds above 0")
		}
		scheduling := map[string]any{}
		for key, value := range map[string]string{"provisioningModel": s.ProvisioningModel,
			"onHostMaintenance": s.OnHostMaintenance, "instanceTerminationAction": s.InstanceTerminationAction} {
			if value != "" {
				scheduling[key] = value
			}
		}
		if s.AutomaticRestart != nil {
			scheduling["automaticRestart"] = *s.AutomaticRestart
		}
		if s.MaxRunDurationSeconds != nil {
			scheduling["maxRunDuration"] = map[string]any{"seconds": strconv.FormatInt(*s.MaxRunDurationSeconds, 10)}
		}
		body["scheduling"] = scheduling
	}
	if len(a.GuestAccelerators) > 0 {
		if len(a.GuestAccelerators) > maxAccelerators {
			return badRequest("guest_accelerators takes at most " + strconv.Itoa(maxAccelerators) + " entries")
		}
		accelerators := make([]any, 0, len(a.GuestAccelerators))
		for _, g := range a.GuestAccelerators {
			if g.Count == nil || !withinInt(g.Count, 1, 16) {
				return badRequest("an accelerator needs a count of 1 to 16")
			}
			path, err := t.resource("guest_accelerators type", g.Type, "acceleratorTypes", scopeZone)
			if err != nil {
				return err
			}
			accelerators = append(accelerators, map[string]any{"acceleratorType": path, "acceleratorCount": *g.Count})
		}
		body["guestAccelerators"] = accelerators
	}
	if a.MinCPUPlatform != "" {
		if !cpuPlatformPattern.MatchString(a.MinCPUPlatform) {
			return badRequest("min_cpu_platform must be a CPU platform name such as Intel Skylake")
		}
		body["minCpuPlatform"] = a.MinCPUPlatform
	}
	if s := a.ShieldedInstanceConfig; s != nil {
		setBools(body, "shieldedInstanceConfig", map[string]*bool{"enableSecureBoot": s.EnableSecureBoot,
			"enableVtpm": s.EnableVtpm, "enableIntegrityMonitoring": s.EnableIntegrityMonitoring}, true)
	}
	if c := a.ConfidentialInstanceConfig; c != nil {
		if !oneOf(c.ConfidentialInstanceType, "SEV", "SEV_SNP", "TDX") {
			return badRequest("confidential_instance_type is SEV, SEV_SNP, or TDX")
		}
		setBools(body, "confidentialInstanceConfig", map[string]*bool{
			"enableConfidentialCompute": c.EnableConfidentialCompute}, true)
		if c.ConfidentialInstanceType != "" {
			body["confidentialInstanceConfig"].(map[string]any)["confidentialInstanceType"] = c.ConfidentialInstanceType
		}
	}
	if a.CanIPForward != nil {
		body["canIpForward"] = *a.CanIPForward
	}
	if a.DeletionProtection != nil {
		body["deletionProtection"] = *a.DeletionProtection
	}
	if r := a.ReservationAffinity; r != nil {
		if !oneOf(r.ConsumeReservationType, "ANY_RESERVATION", "SPECIFIC_RESERVATION", "NO_RESERVATION") ||
			(r.ConsumeReservationType == "SPECIFIC_RESERVATION") != (len(r.Values) > 0) || len(r.Values) > 10 {
			return badRequest("reservation_affinity needs values (reservation names, at most 10) exactly for " +
				"consume_reservation_type SPECIFIC_RESERVATION")
		}
		for _, value := range r.Values {
			if !validComputeName(value) {
				return badRequest("a reservation is given by its name in this project")
			}
		}
		affinity := map[string]any{}
		if r.ConsumeReservationType != "" {
			affinity["consumeReservationType"] = r.ConsumeReservationType
		}
		if len(r.Values) > 0 {
			affinity["key"] = "compute.googleapis.com/reservation-name"
			affinity["values"] = r.Values
		}
		body["reservationAffinity"] = affinity
	}
	if d := a.DisplayDevice; d != nil {
		setBools(body, "displayDevice", map[string]*bool{"enableDisplay": d.EnableDisplay}, true)
	}
	if m := a.AdvancedMachineFeatures; m != nil {
		if !withinInt(m.ThreadsPerCore, 1, 2) || !withinInt(m.VisibleCoreCount, 1, 1000) {
			return badRequest("advanced_machine_features has threads_per_core 1 or 2 and visible_core_count above 0")
		}
		setBools(body, "advancedMachineFeatures", map[string]*bool{
			"enableNestedVirtualization": m.EnableNestedVirtualization, "enableUefiNetworking": m.EnableUefiNetworking}, true)
		features := body["advancedMachineFeatures"].(map[string]any)
		if m.ThreadsPerCore != nil {
			features["threadsPerCore"] = *m.ThreadsPerCore
		}
		if m.VisibleCoreCount != nil {
			features["visibleCoreCount"] = *m.VisibleCoreCount
		}
	}
	return nil
}

// setBools creates body[name] when present and fills it with the booleans that were given.
func setBools(body map[string]any, name string, values map[string]*bool, present bool) {
	if !present {
		return
	}
	object, _ := body[name].(map[string]any)
	if object == nil {
		object = map[string]any{}
	}
	for key, value := range values {
		if value != nil {
			object[key] = *value
		}
	}
	body[name] = object
}

// checkAccounts refuses a service account that is not of this project and returns the body members. The
// accounts are those of the project ID (NAME@PROJECT_ID.iam.gserviceaccount.com) or the Compute Engine default
// account of the project number; both come from the project as projects.get returned it, never from an argument.
func checkAccounts(accounts []instanceAccountArgument, id, number string) ([]any, error) {
	fail := badRequest("service_accounts email must be a service account of this project: NAME@PROJECT_ID." +
		"iam.gserviceaccount.com, or the Compute Engine default account PROJECT_NUMBER-compute of the domain " +
		"developer.gserviceaccount.com")
	out := make([]any, 0, len(accounts))
	for _, account := range accounts {
		entry := map[string]any{"scopes": append([]string{}, account.Scopes...)}
		if account.Email != "" {
			local, domain, found := strings.Cut(account.Email, "@")
			own := found && ownAccountLocal.MatchString(local) && domain == id+".iam.gserviceaccount.com"
			standard := false
			if m := defaultAccount.FindStringSubmatch(account.Email); m != nil {
				standard = m[1] == number
			}
			if !own && !standard {
				return nil, fail
			}
			entry["email"] = account.Email
		}
		out = append(out, entry)
	}
	return out, nil
}

// accountShape refuses before any secret is read an email that cannot name an account of any project.
func accountShape(accounts *[]instanceAccountArgument) error {
	if accounts == nil {
		return nil
	}
	if len(*accounts) > 1 {
		return badRequest("service_accounts takes at most one account")
	}
	for _, account := range *accounts {
		if len(account.Scopes) > maxServiceScopes {
			return badRequest("a service account takes at most " + strconv.Itoa(maxServiceScopes) + " scopes")
		}
		for _, scope := range account.Scopes {
			if !scopeURLPattern.MatchString(scope) {
				return badRequest("a service account scope is a URL of the form https://www.googleapis.com/auth/NAME")
			}
		}
		if account.Email == "" {
			continue
		}
		if !strings.HasSuffix(account.Email, ".iam.gserviceaccount.com") &&
			!defaultAccount.MatchString(account.Email) {
			return badRequest("service_accounts email must be a service account of this project: NAME@PROJECT_ID." +
				"iam.gserviceaccount.com, or the Compute Engine default account PROJECT_NUMBER-compute of the domain " +
				"developer.gserviceaccount.com")
		}
	}
	return nil
}

func handleInstancesInsert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create instance"
	a, err := decodeInsertArguments(raw)
	if err != nil {
		return nil, err
	}
	body, _, err := a.check()
	if err != nil {
		return nil, err
	}
	if err := accountShape(a.ServiceAccounts); err != nil {
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
	if a.ServiceAccounts != nil {
		accounts := *a.ServiceAccounts
		// The project ID and number come from the project, so no argument can name another project's account.
		id, number := a.ProjectID, a.ProjectID
		if len(accounts) > 0 && accounts[0].Email != "" {
			project, err := c.project(ctx, op, projectsGetID, a.ProjectID)
			if err != nil {
				return nil, err
			}
			id, number = project.ProjectID, strings.TrimPrefix(project.Name, prefixProject)
			if !projectIDShape.MatchString(id) || !numberPattern.MatchString(number) ||
				(a.ProjectID != id && a.ProjectID != number) {
				return nil, invalidRow(op)
			}
		}
		entries, err := checkAccounts(accounts, id, number)
		if err != nil {
			return nil, err
		}
		body["serviceAccounts"] = entries
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the request could not be built")
	}
	path := computeZonePath(a.ProjectID, a.Zone) + "/instances"
	return c.zonalChange(ctx, op, instancesInsertID, http.MethodPost, path, requestID, a.Zone, payload)
}

// instanceInsertSummary names the cost drivers of the call from its arguments alone: project, zone, name,
// machine type, disk sizes, accelerators, and the scheduling model.
func instanceInsertSummary(raw json.RawMessage) string {
	var a insertArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	machine := boundedText(a.MachineType, maxNameText)
	switch {
	case machine != "":
	case a.SourceInstanceTemplate != "":
		machine = "as set by the instance template " + boundedText(a.SourceInstanceTemplate, maxNameText)
	case a.SourceMachineImage != "":
		machine = "as set by the machine image " + boundedText(a.SourceMachineImage, maxNameText)
	}
	disks := make([]string, 0, len(a.Disks))
	for _, d := range a.Disks {
		size := "default size"
		if d.InitializeParams != nil && d.InitializeParams.DiskSizeGB != nil {
			size = strconv.FormatInt(*d.InitializeParams.DiskSizeGB, 10) + " GB"
		} else if d.Source != "" {
			size = "existing disk"
		}
		disks = append(disks, size)
	}
	if len(disks) == 0 {
		disks = append(disks, "as set by the template or image")
	}
	accelerators := "none"
	if len(a.GuestAccelerators) > 0 {
		parts := make([]string, 0, len(a.GuestAccelerators))
		for _, g := range a.GuestAccelerators {
			count := 0
			if g.Count != nil {
				count = *g.Count
			}
			parts = append(parts, strconv.Itoa(count)+" x "+boundedText(g.Type, maxNameText))
		}
		accelerators = strings.Join(parts, ", ")
	}
	model := "STANDARD"
	if a.Scheduling != nil && a.Scheduling.ProvisioningModel != "" {
		model = boundedText(a.Scheduling.ProvisioningModel, maxNameText)
	}
	return "Create instance " + boundedText(a.Name, maxNameText) + " in zone " + boundedText(a.Zone, maxNameText) +
		" of project " + boundedText(a.ProjectID, maxNameText) + ": machine type " + machine + ", disks " +
		boundedText(strings.Join(disks, ", "), maxText) + ", accelerators " + boundedText(accelerators, maxText) +
		", scheduling model " + model + "; the instance and its disks are billed while they exist"
}

// --- descriptor ---

const (
	instanceRefPattern = `^[a-z][-A-Za-z0-9/]{0,299}$`
	instanceRefProp    = `{"type":"string","minLength":1,"maxLength":300,"pattern":"` + instanceRefPattern + `"}`
	instanceNameProp   = `{"type":"string","minLength":1,"maxLength":63,"pattern":"^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$"}`
	instanceLabelsProp = `{"type":"object","maxProperties":64,"additionalProperties":{"type":"string","maxLength":63}}`
	instanceBoolProp   = `{"type":"boolean"}`

	initializeProp = `{"type":"object","properties":{"disk_name":` + instanceNameProp + `,` +
		`"disk_size_gb":{"type":"integer","minimum":1,"maximum":65536},"disk_type":` + instanceRefProp + `,` +
		`"source_image":` + instanceRefProp + `,"description":{"type":"string","maxLength":1024},` +
		`"labels":` + instanceLabelsProp + `,"provisioned_iops":{"type":"integer","minimum":1,"maximum":1000000},` +
		`"provisioned_throughput":{"type":"integer","minimum":1,"maximum":1000000}},"additionalProperties":false}`

	diskProp = `{"type":"object","properties":{"type":{"type":"string","enum":["PERSISTENT","SCRATCH"]},` +
		`"boot":` + instanceBoolProp + `,"auto_delete":` + instanceBoolProp + `,` +
		`"mode":{"type":"string","enum":["READ_WRITE","READ_ONLY"]},` +
		`"interface":{"type":"string","enum":["SCSI","NVME"]},"device_name":` + instanceNameProp + `,` +
		`"source":` + instanceRefProp + `,"initialize_params":` + initializeProp + `,` +
		`"kms_key_name":{"type":"string","maxLength":400}},"additionalProperties":false}`

	interfaceProp = `{"type":"object","properties":{"network":` + instanceRefProp + `,"subnetwork":` + instanceRefProp + `,` +
		`"network_ip":{"type":"string","maxLength":15},"nic_type":{"type":"string","enum":["VIRTIO_NET","GVNIC"]},` +
		`"stack_type":{"type":"string","enum":["IPV4_ONLY","IPV4_IPV6","IPV6_ONLY"]},` +
		`"queue_count":{"type":"integer","minimum":1,"maximum":512}},"additionalProperties":false}`

	serviceAccountProp = `{"type":"object","properties":{"email":{"type":"string","maxLength":100},` +
		`"scopes":{"type":"array","maxItems":30,"items":{"type":"string","maxLength":160}}},"additionalProperties":false}`

	schedulingProp = `{"type":"object","properties":{"provisioning_model":{"type":"string","enum":["STANDARD","SPOT"]},` +
		`"on_host_maintenance":{"type":"string","enum":["MIGRATE","TERMINATE"]},"automatic_restart":` + instanceBoolProp + `,` +
		`"instance_termination_action":{"type":"string","enum":["STOP","DELETE"]},` +
		`"max_run_duration_seconds":{"type":"integer","minimum":1}},"additionalProperties":false}`

	instanceInsertProps = serviceProjectProp + `,` + computeZoneProp + `,"name":` + instanceNameProp + `,` +
		`"description":{"type":"string","maxLength":1024},"machine_type":` + instanceRefProp + `,` +
		`"source_instance_template":` + instanceRefProp + `,"source_machine_image":` + instanceRefProp + `,` +
		`"disks":{"type":"array","maxItems":24,"items":` + diskProp + `},` +
		`"network_interfaces":{"type":"array","maxItems":8,"items":` + interfaceProp + `},` +
		`"service_accounts":{"type":"array","maxItems":1,"items":` + serviceAccountProp + `},` +
		`"metadata":{"type":"array","maxItems":128,"items":{"type":"object","properties":{` +
		`"key":{"type":"string","minLength":1,"maxLength":128,"pattern":"^[A-Za-z0-9_-]+$"},` +
		`"value":{"type":"string","maxLength":262144}},"required":["key","value"],"additionalProperties":false}},` +
		`"labels":` + instanceLabelsProp + `,"tags":{"type":"array","maxItems":64,"items":` + instanceNameProp + `},` +
		`"scheduling":` + schedulingProp + `,"guest_accelerators":{"type":"array","maxItems":8,"items":{"type":"object",` +
		`"properties":{"type":` + instanceRefProp + `,"count":{"type":"integer","minimum":1,"maximum":16}},` +
		`"required":["type","count"],"additionalProperties":false}},"min_cpu_platform":{"type":"string","maxLength":64},` +
		`"shielded_instance_config":{"type":"object","properties":{"enable_secure_boot":` + instanceBoolProp + `,` +
		`"enable_vtpm":` + instanceBoolProp + `,"enable_integrity_monitoring":` + instanceBoolProp + `},"additionalProperties":false},` +
		`"confidential_instance_config":{"type":"object","properties":{"enable_confidential_compute":` + instanceBoolProp + `,` +
		`"confidential_instance_type":{"type":"string","enum":["SEV","SEV_SNP","TDX"]}},"additionalProperties":false},` +
		`"can_ip_forward":` + instanceBoolProp + `,"deletion_protection":` + instanceBoolProp + `,` +
		`"reservation_affinity":{"type":"object","properties":{"consume_reservation_type":{"type":"string",` +
		`"enum":["ANY_RESERVATION","SPECIFIC_RESERVATION","NO_RESERVATION"]},"values":{"type":"array","maxItems":10,` +
		`"items":` + instanceNameProp + `}},"additionalProperties":false},` +
		`"display_device":{"type":"object","properties":{"enable_display":` + instanceBoolProp + `},"additionalProperties":false},` +
		`"advanced_machine_features":{"type":"object","properties":{"enable_nested_virtualization":` + instanceBoolProp + `,` +
		`"threads_per_core":{"type":"integer","minimum":1,"maximum":2},"visible_core_count":{"type":"integer","minimum":1},` +
		`"enable_uefi_networking":` + instanceBoolProp + `},"additionalProperties":false},` + computeRequestIDProp
)

var instancesInsert = capability.Descriptor{
	ID: instancesInsertID, Version: 1, Title: "Create a Compute Engine instance",
	Description: "Create one virtual machine instance in a zone of a project (instances.insert of Compute Engine " +
		"v1). The tool builds the request body itself from structured arguments and passes no object through. " +
		"Machine type, disk types, source images, disks, networks, subnetworks, instance templates, machine " +
		"images, and accelerator types are names or paths of this project and its zone or region and are turned " +
		"into fixed paths; the only resource of another project is an image family as projects/PROJECT/global/" +
		"images/family/FAMILY, such as projects/debian-cloud/global/images/family/debian-12. The instance gets no " +
		"external address, because network interfaces take no accessConfigs or ipv6AccessConfigs, and the " +
		"metadata keys ssh-keys, sshKeys, and windows-keys are refused. A service account must belong to this " +
		"project (NAME@PROJECT_ID.iam.gserviceaccount.com, or the default account of the project number, which " +
		"is read from the project) or be left out; key material of customer-supplied encryption keys is no " +
		"argument, kms_key_name is. With source_instance_template or source_machine_image the tool cannot read " +
		"what they hold, so network_interfaces and service_accounts must be given as well. A start-up script in " +
		"the metadata can create accounts or reach anything the instance can reach, and the tool does not check " +
		"it: that limit is the person's. The instance and its disks are billed while they exist. The call " +
		"carries a request_id; repeating it with the same one is safe." + instanceWriteNote + computeTail,
	Tags: []string{"google", "cloud", "compute", "instances", "create", "cost"}, Provider: Provider,
	Risk: capability.Risk{Effect: capability.EffectCreate, Idempotency: capability.IdempotencyIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: computeOperationSensitivity},
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + instanceInsertProps +
		`},"required":["project_id","zone","name"],"additionalProperties":false}`),
	OutputSchema: instanceWriteOutput,
	Arguments: []capability.Argument{serviceProjectArgument, computeZoneArgument,
		{Name: "name", Required: true, Description: "Name of the new instance: lowercase letters, digits, and '-', " +
			"at most 63 characters, starting with a letter"},
		{Name: "description", Description: "Description of the instance"},
		{Name: "machine_type", Description: "Machine type name such as e2-medium, or zones/ZONE/machineTypes/NAME " +
			"of this zone; required unless a template or machine image is given"},
		{Name: "source_instance_template", Description: "Instance template of this project: NAME, global/" +
			"instanceTemplates/NAME, or regions/REGION/instanceTemplates/NAME of the region of the zone. Needs " +
			"network_interfaces and service_accounts"},
		{Name: "source_machine_image", Description: "Machine image of this project: NAME or global/machineImages/" +
			"NAME. Needs network_interfaces and service_accounts"},
		{Name: "disks", Description: "Disks, at most 24, the one with boot true being the boot disk. " +
			"Each: type PERSISTENT or SCRATCH, boot, auto_delete, mode, interface, device_name, source (an existing " +
			"disk of the zone, by name), initialize_params (disk_name, disk_size_gb, disk_type, source_image, " +
			"description, labels, provisioned_iops, provisioned_throughput), kms_key_name. source_image is an image " +
			"or family of this project, or projects/PROJECT/global/images/family/FAMILY of another"},
		{Name: "network_interfaces", Description: "Network interfaces, at most 8. Each: network, subnetwork (of the " +
			"region of the zone), network_ip (IPv4), nic_type, stack_type, queue_count. There is no way to give an " +
			"external address"},
		{Name: "service_accounts", Description: "At most one service account: email (a service account of this " +
			"project or its default account; left out, Google uses the default account) and scopes " +
			"(https://www.googleapis.com/auth/... URLs). Without it the instance has no service account"},
		{Name: "metadata", Description: "Metadata entries {key, value}. ssh-keys, sshKeys, and windows-keys are " +
			"refused, block-project-ssh-keys is allowed. A start-up script is not checked"},
		{Name: "labels", Description: "Labels, at most 64"},
		{Name: "tags", Description: "Network tags, at most 64"},
		{Name: "scheduling", Description: "provisioning_model (STANDARD or SPOT), on_host_maintenance, " +
			"automatic_restart, instance_termination_action, max_run_duration_seconds"},
		{Name: "guest_accelerators", Description: "Accelerators {type (an accelerator type name of the zone), count}"},
		{Name: "min_cpu_platform", Description: "Minimum CPU platform such as Intel Skylake"},
		{Name: "shielded_instance_config", Description: "enable_secure_boot, enable_vtpm, enable_integrity_monitoring"},
		{Name: "confidential_instance_config", Description: "enable_confidential_compute, confidential_instance_type"},
		{Name: "can_ip_forward", Description: "True to let the instance send and receive packets for other addresses"},
		{Name: "deletion_protection", Description: "True to protect the instance from deletion"},
		{Name: "reservation_affinity", Description: "consume_reservation_type, and for SPECIFIC_RESERVATION the " +
			"names of reservations of this project as values"},
		{Name: "display_device", Description: "enable_display"},
		{Name: "advanced_machine_features", Description: "enable_nested_virtualization, threads_per_core, " +
			"visible_core_count, enable_uefi_networking"},
		{Name: "request_id", Description: "UUID that identifies the creation. Repeating the call with the same " +
			"request_id is safe: Google ignores a request whose ID it already received. Without it the tool creates " +
			"one and returns it as request_id; the all-zero UUID is not allowed"}},
	Fields: instanceWriteFields,
	Examples: []capability.Example{{
		Description: "Create a small instance without an external address",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","zone":"us-central1-a","name":"my-vm",` +
			`"machine_type":"e2-medium","disks":[{"boot":true,"initialize_params":{"disk_size_gb":20,` +
			`"source_image":"projects/debian-cloud/global/images/family/debian-12"}}],` +
			`"network_interfaces":[{"network":"default"}]}`),
	}},
}
