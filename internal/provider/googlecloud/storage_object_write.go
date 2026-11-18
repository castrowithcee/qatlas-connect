package googlecloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	objectsInsertID  = Provider + ".objects.insert"
	objectsCopyID    = Provider + ".objects.copy"
	objectsComposeID = Provider + ".objects.compose"
	objectsPatchID   = Provider + ".objects.patch"
)

// This file holds the four tools that write objects (storage v1, revision 20261114): objects.insert (a multipart
// upload), objects.copy, objects.compose, and objects.patch. Every tool builds its request from structured
// arguments. An object is never overwritten unless overwrite is true, and no tool can make an object readable
// by anyone but the principals the bucket already names: the predefined ACLs publicRead and authenticatedRead
// and every acl object are no argument, and the metadata of a copy and the body of a patch are built from a
// fixed list of fields.

const (
	// maxUploadBytes bounds the content of one upload: 256 KiB, the bound of objects.download. It keeps the
	// content inside one multipart request and the arguments of one invoke well within the limits of the
	// invoke path; larger content needs a resumable upload, which this provider does not offer.
	maxUploadBytes = 256 << 10
	// maxUploadChars is the length of the base64 form of maxUploadBytes, the longest content text the schema accepts.
	maxUploadChars = (maxUploadBytes + 2) / 3 * 4

	// maxConfirmationRunes is the bound the core puts on the text of a confirmation. A tool whose confirmation
	// would not fit refuses the call before it writes, because a cut text could hide what is written.
	maxConfirmationRunes = 500

	maxObjectMetadataEntries = 100
	maxObjectMetadataKey     = 128
	maxObjectMetadataValue   = 1024
	maxObjectMetadataBytes   = 8192

	minComposeSources = 2
	maxComposeSources = 32
)

// writableACLs are the predefined ACLs a write may name: none of them reads to everyone.
var writableACLs = []string{"private", "projectPrivate", "bucketOwnerRead", "bucketOwnerFullControl"}

// writableClasses are the storage classes of an object a write may name.
var writableClasses = []string{"STANDARD", "NEARLINE", "COLDLINE", "ARCHIVE"}

var objectKMSKeyPattern = regexp.MustCompile(`^projects/[A-Za-z0-9:._-]{1,100}/locations/[A-Za-z0-9_-]{1,64}/` +
	`keyRings/[A-Za-z0-9_-]{1,63}/cryptoKeys/[A-Za-z0-9_-]{1,63}$`)

func jsonList(values []string) string {
	b, _ := json.Marshal(values)
	return string(b)
}

const (
	writeHeaderProp      = `{"type":"string","minLength":1,"maxLength":1024}`
	writeContentTypeProp = `{"type":"string","minLength":1,"maxLength":255}`
	writeCustomTimeProp  = `{"type":"string","minLength":20,"maxLength":40}`
	writeKMSProp         = `{"type":"string","minLength":1,"maxLength":512}`
	writeMetadataProp    = `{"type":"object","maxProperties":100,"additionalProperties":` +
		`{"type":"string","maxLength":1024}}`
	writeOverwriteProp  = `{"type":"boolean"}`
	writeProjectionProp = `{"type":"string","enum":["full","noAcl"]}`
	writeGenerationOne  = `{"type":"string","pattern":"^[0-9]{1,20}$","maxLength":20}`
)

var (
	writeACLProp   = `{"type":"string","enum":` + jsonList(writableACLs) + `}`
	writeClassProp = `{"type":"string","enum":` + jsonList(writableClasses) + `}`
)

// objectFieldProps are the properties of the object fields a caller may set; withClass adds storage_class.
func objectFieldProps(prefix string, withClass bool) string {
	props := []string{`"` + prefix + `content_type":` + writeContentTypeProp,
		`"` + prefix + `cache_control":` + writeHeaderProp, `"` + prefix + `content_disposition":` + writeHeaderProp,
		`"` + prefix + `content_encoding":` + writeHeaderProp, `"` + prefix + `content_language":` + writeHeaderProp,
		`"` + prefix + `metadata":` + writeMetadataProp, `"` + prefix + `contexts":` + writeMetadataProp,
		`"` + prefix + `custom_time":` + writeCustomTimeProp}
	if withClass {
		props = append(props, `"`+prefix+`storage_class":`+writeClassProp)
	}
	return strings.Join(props, ",")
}

var (
	writeContentTypeArgument = capability.Argument{Name: "content_type", Description: "Content type of the object " +
		"such as text/plain; at most 255 characters without control characters"}
	writeCacheControlArgument = capability.Argument{Name: "cache_control", Description: "Cache-Control header " +
		"value stored with the object, at most 1024 characters"}
	writeDispositionArgument = capability.Argument{Name: "content_disposition", Description: "Content-Disposition " +
		"header value stored with the object, at most 1024 characters"}
	writeEncodingArgument = capability.Argument{Name: "content_encoding", Description: "Content-Encoding header " +
		"value stored with the object such as gzip, at most 1024 characters; it does not encode the content"}
	writeLanguageArgument = capability.Argument{Name: "content_language", Description: "Content-Language header " +
		"value stored with the object such as en, at most 1024 characters"}
	writeMetadataArgument = capability.Argument{Name: "metadata", Description: "Custom metadata as an object of " +
		"text keys and text values: at most 100 entries, keys of 1 to 128 and values of at most 1024 bytes, " +
		"8 KiB in all, without control characters"}
	writeContextsArgument = capability.Argument{Name: "contexts", Description: "Custom object contexts as an object " +
		"of text keys and text values (sent as contexts.custom, each value as the payload value), with the limits " +
		"of metadata: at most 100 entries, keys of 1 to 128 and values of at most 1024 bytes, 8 KiB in all"}
	writeProjectionArgument = capability.Argument{Name: "projection", Description: "full to include the access " +
		"control list of the written object (acl) in the result, noAcl to leave it out; Google's default is noAcl"}
	writeIfGenerationNotMatchArgument = capability.Argument{Name: "if_generation_not_match", Description: "Only write " +
		"if the live generation of the object is not this decimal number. It does not lift the protection against " +
		"overwriting: without overwrite the call still sends ifGenerationMatch=0"}
	writeIfMetagenerationNotMatchArgument = capability.Argument{Name: "if_metageneration_not_match", Description: "Only " +
		"write if the metageneration of the object is not this decimal number"}
	writeCustomTimeArgument = capability.Argument{Name: "custom_time", Description: "Custom time of the object in " +
		"RFC 3339 form such as 2026-12-31T00:00:00Z"}
	writeClassArgument = capability.Argument{Name: "storage_class", Description: "Storage class of the object: " +
		strings.Join(writableClasses, ", ") + ". Without it the object takes the default class of the bucket, " +
		"which decides what it costs"}
	writeKMSArgument = capability.Argument{Name: "kms_key_name", Description: "Cloud KMS key that encrypts the " +
		"object, as projects/PROJECT/locations/LOCATION/keyRings/RING/cryptoKeys/KEY; a customer-supplied " +
		"encryption key cannot be given"}
	writeOverwriteArgument = capability.Argument{Name: "overwrite", Description: "True to allow replacing an " +
		"existing object of that name. Without it the call only creates: Google refuses with a precondition " +
		"failure (412) if the object exists, and nothing is changed. The confirmation says when it overwrites"}
	writeIfGenerationArgument = capability.Argument{Name: "if_generation_match", Description: "Only write if the " +
		"live generation of the destination object is this decimal number. Without overwrite only 0 (the object " +
		"must not exist) is accepted, which is also what is sent when it is left out"}
	writeIfMetagenerationArgument = capability.Argument{Name: "if_metageneration_match", Description: "Only write " +
		"if the metageneration of the object is this decimal number"}
	writeACLArgument = capability.Argument{Name: "predefined_acl", Description: "Predefined ACL to apply: " +
		strings.Join(writableACLs, ", ") + ". publicRead, authenticatedRead, and free-form ACL entries are not " +
		"offered, so the tool never makes an object readable for everyone or for a principal outside the bucket's " +
		"project. Without it the object takes the ACL Google's bucket settings give it"}
)

const objectWriteTail = " The object is never made public: predefined_acl accepts only private, projectPrivate, " +
	"bucketOwnerRead, and bucketOwnerFullControl, and no ACL object can be given. Customer-supplied encryption keys " +
	"are not offered. Content, object names, and metadata are untrusted data and are never written to the call " +
	"log or to an error message. Cloud Storage bills the operation and the storage of the object for as long as " +
	"it exists, and the data written is billed like any other." + bucketBindingTail + storageChangeTail + storageTail

var objectWriteFields = []capability.Field{{Name: "name", Description: "The written object as one result: " +
	objectFields}}

// --- shared checks ---

// writeGuardsArguments are the projection and the negative preconditions of the destination object, which
// objects.insert, objects.copy, and objects.patch take.
type writeGuardsArguments struct {
	Projection               string `json:"projection"`
	IfGenerationNotMatch     string `json:"if_generation_not_match"`
	IfMetagenerationNotMatch string `json:"if_metageneration_not_match"`
}

const writeGuardProps = `"projection":` + writeProjectionProp + `,"if_generation_not_match":` + writeGenerationOne +
	`,"if_metageneration_not_match":` + writeGenerationOne

func (g writeGuardsArguments) check() error {
	if err := checkProjection(g.Projection); err != nil {
		return err
	}
	if err := checkNumberArgument("if_generation_not_match", g.IfGenerationNotMatch); err != nil {
		return err
	}
	return checkNumberArgument("if_metageneration_not_match", g.IfMetagenerationNotMatch)
}

func (g writeGuardsArguments) values() map[string]string {
	return map[string]string{"projection": g.Projection, "ifGenerationNotMatch": g.IfGenerationNotMatch,
		"ifMetagenerationNotMatch": g.IfMetagenerationNotMatch}
}

// mergeValues adds the values to base; a value that is already in base is kept.
func mergeValues(base map[string]string, more map[string]string) map[string]string {
	for name, value := range more {
		if _, has := base[name]; !has {
			base[name] = value
		}
	}
	return base
}

// checkWriteName refuses an object name that cannot be written and shown honestly in a confirmation: empty,
// "." or "..", too long, not valid UTF-8, or holding a control, format, or separator character.
func checkWriteName(argument, value string) error {
	if value == "" || value == "." || value == ".." {
		return badRequest(argument + " must be an object name, not empty, \".\", or \"..\"")
	}
	if err := checkObjectText(argument, value); err != nil {
		return err
	}
	if strings.IndexFunc(value, func(r rune) bool { return unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) }) >= 0 {
		return badRequest(argument + " must not hold format or separator characters")
	}
	return nil
}

func checkWriteACL(argument, value string) error {
	if value != "" && !slices.Contains(writableACLs, value) {
		return badRequest(argument + " must be one of " + strings.Join(writableACLs, ", ") +
			"; public and free-form ACLs are not offered")
	}
	return nil
}

func checkWriteKMS(argument, value string) error {
	if value != "" && !objectKMSKeyPattern.MatchString(value) {
		return badRequest(argument + " must be a Cloud KMS key name projects/P/locations/L/keyRings/R/cryptoKeys/K")
	}
	return nil
}

// overwriteGeneration returns the ifGenerationMatch of a call that creates: 0 unless overwrite is true, then the
// value of the caller. A caller value other than 0 without overwrite is refused.
func overwriteGeneration(overwrite bool, ifGeneration string) (string, error) {
	if err := checkNumberArgument("if_generation_match", ifGeneration); err != nil {
		return "", err
	}
	if overwrite {
		return ifGeneration, nil
	}
	if ifGeneration != "" && ifGeneration != "0" {
		return "", badRequest("if_generation_match other than 0 needs overwrite=true: without overwrite the call " +
			"only creates a new object")
	}
	return "0", nil
}

// objectWriteFieldsArguments are the fields of an object a caller may set, in the form of the arguments.
type objectWriteFieldsArguments struct {
	ContentType        string            `json:"content_type"`
	CacheControl       string            `json:"cache_control"`
	ContentDisposition string            `json:"content_disposition"`
	ContentEncoding    string            `json:"content_encoding"`
	ContentLanguage    string            `json:"content_language"`
	Metadata           map[string]string `json:"metadata"`
	Contexts           map[string]string `json:"contexts"`
	CustomTime         string            `json:"custom_time"`
	StorageClass       string            `json:"storage_class"`
}

// check refuses a field that is not in its documented form; prefix names the argument a field belongs to.
func (f objectWriteFieldsArguments) check(prefix string) error {
	if f.ContentType != "" && (len(f.ContentType) > 255 || !asciiVisible(f.ContentType)) {
		return badRequest(prefix + "content_type must be a content type of at most 255 printable ASCII characters")
	}
	for _, text := range []struct{ name, value string }{{"cache_control", f.CacheControl},
		{"content_disposition", f.ContentDisposition}, {"content_encoding", f.ContentEncoding},
		{"content_language", f.ContentLanguage}} {
		if err := checkObjectText(prefix+text.name, text.value); err != nil {
			return err
		}
	}
	if f.CustomTime != "" {
		if _, err := time.Parse(time.RFC3339, f.CustomTime); err != nil {
			return badRequest(prefix + "custom_time must be an RFC 3339 time such as 2026-12-31T00:00:00Z")
		}
	}
	if f.StorageClass != "" && !slices.Contains(writableClasses, f.StorageClass) {
		return badRequest(prefix + "storage_class must be one of " + strings.Join(writableClasses, ", "))
	}
	if err := checkObjectMetadata(prefix+"contexts", f.Contexts); err != nil {
		return err
	}
	return checkObjectMetadata(prefix+"metadata", f.Metadata)
}

func asciiVisible(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func checkObjectMetadata(argument string, metadata map[string]string) error {
	if len(metadata) > maxObjectMetadataEntries {
		return badRequest(argument + " must hold at most " + strconv.Itoa(maxObjectMetadataEntries) + " entries")
	}
	total := 0
	for key, value := range metadata {
		if key == "" || len(key) > maxObjectMetadataKey || len(value) > maxObjectMetadataValue ||
			!utf8.ValidString(key) || !utf8.ValidString(value) ||
			strings.IndexFunc(key, unicode.IsControl) >= 0 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return badRequest(argument + " must hold keys of 1 to 128 and values of at most 1024 bytes of valid " +
				"UTF-8 without control characters")
		}
		total += len(key) + len(value)
	}
	if total > maxObjectMetadataBytes {
		return badRequest(argument + " must not exceed 8 KiB in all")
	}
	return nil
}

// body is the part of the object resource the fields set; no member is passed through from elsewhere.
func (f objectWriteFieldsArguments) body() map[string]any {
	out := map[string]any{}
	for name, value := range map[string]string{"contentType": f.ContentType, "cacheControl": f.CacheControl,
		"contentDisposition": f.ContentDisposition, "contentEncoding": f.ContentEncoding,
		"contentLanguage": f.ContentLanguage, "customTime": f.CustomTime, "storageClass": f.StorageClass} {
		if value != "" {
			out[name] = value
		}
	}
	if len(f.Metadata) > 0 {
		out["metadata"] = f.Metadata
	}
	if len(f.Contexts) > 0 {
		custom := map[string]any{}
		for key, value := range f.Contexts {
			custom[key] = map[string]any{"value": value}
		}
		out["contexts"] = map[string]any{"custom": custom}
	}
	return out
}

// fitConfirmation refuses a call whose confirmation text would be cut by the core.
func fitConfirmation(summary string) error {
	if utf8.RuneCountInString(summary) > maxConfirmationRunes {
		return badRequest("the names are too long for the confirmation, which must show them in full; nothing was " +
			"written")
	}
	return nil
}

// writtenObject checks the answer of a write and keeps the documented members.
func writtenObject(op string, wire json.RawMessage, bucket, name string) (map[string]any, error) {
	row, err := objectRow(op, wire)
	if err != nil {
		return nil, err
	}
	if got, _ := row["name"].(string); got != name || !objectOfBucket(row, bucket) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}

func overwriteNote(overwrite bool) string {
	if overwrite {
		return "OVERWRITES an object of that name if it exists"
	}
	return "creates it and fails if an object of that name exists"
}

func classNote(class, fallback string) string {
	if class != "" {
		return "storage class " + class
	}
	return fallback
}

func quoted(value string) string { return strconv.Quote(value) }

// --- objects.insert ---

type objectsInsertArguments struct {
	Bucket              string `json:"bucket"`
	Name                string `json:"name"`
	Content             string `json:"content"`
	ContentEncodingKind string `json:"content_encoding_kind"`
	objectWriteFieldsArguments
	writeGuardsArguments
	KMSKeyName            string `json:"kms_key_name"`
	PredefinedACL         string `json:"predefined_acl"`
	IfGenerationMatch     string `json:"if_generation_match"`
	IfMetagenerationMatch string `json:"if_metageneration_match"`
	Overwrite             bool   `json:"overwrite"`
	UserProject           string `json:"user_project"`
}

// decoded returns the bytes of the content, decoded from base64 where content_encoding_kind says so.
func (a objectsInsertArguments) decoded() ([]byte, error) {
	switch a.ContentEncodingKind {
	case "", "text":
		if len(a.Content) > maxUploadBytes {
			return nil, badRequest("content must be at most " + strconv.Itoa(maxUploadBytes) + " bytes")
		}
		return []byte(a.Content), nil
	case "base64":
		if len(a.Content) > maxUploadChars {
			return nil, badRequest("content must be at most " + strconv.Itoa(maxUploadBytes) + " bytes")
		}
		data, err := base64.StdEncoding.DecodeString(a.Content)
		if err != nil {
			return nil, badRequest("content is not valid base64 (standard alphabet with padding)")
		}
		if len(data) > maxUploadBytes {
			return nil, badRequest("content must be at most " + strconv.Itoa(maxUploadBytes) + " bytes")
		}
		return data, nil
	}
	return nil, badRequest("content_encoding_kind must be text or base64")
}

func (a objectsInsertArguments) check() ([]byte, error) {
	if err := checkBucket(a.Bucket); err != nil {
		return nil, err
	}
	if err := checkWriteName("name", a.Name); err != nil {
		return nil, err
	}
	data, err := a.decoded()
	if err != nil {
		return nil, err
	}
	if err := a.objectWriteFieldsArguments.check(""); err != nil {
		return nil, err
	}
	for _, check := range []error{checkWriteACL("predefined_acl", a.PredefinedACL),
		checkWriteKMS("kms_key_name", a.KMSKeyName),
		checkNumberArgument("if_metageneration_match", a.IfMetagenerationMatch)} {
		if check != nil {
			return nil, check
		}
	}
	if err := a.writeGuardsArguments.check(); err != nil {
		return nil, err
	}
	if _, err := overwriteGeneration(a.Overwrite, a.IfGenerationMatch); err != nil {
		return nil, err
	}
	return data, fitConfirmation(a.summary(len(data)))
}

func (a objectsInsertArguments) summary(size int) string {
	out := "Upload " + strconv.Itoa(size) + " bytes as object " + quoted(a.Name) + " in bucket " + quoted(a.Bucket) +
		" (" + classNote(a.StorageClass, "storage class: the default class of the bucket") + "); " +
		overwriteNote(a.Overwrite)
	if a.PredefinedACL != "" {
		out += "; predefined ACL " + a.PredefinedACL
	}
	return out
}

func objectsInsertSummary(raw json.RawMessage) string {
	var a objectsInsertArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	data, err := a.decoded()
	if err != nil {
		return "Upload content of an unreadable size as object " + quoted(a.Name) + " in bucket " + quoted(a.Bucket)
	}
	return a.summary(len(data))
}

var objectsInsertRisk = func() capability.Risk {
	risk := storageChangeRisk(capability.EffectCreate)
	risk.DataSensitivity = contentSensitivity
	return risk
}()

var objectsInsert = capability.Descriptor{
	ID: objectsInsertID, Version: 1, Title: "Upload an object to Cloud Storage",
	Description: "Upload one object into a bucket of Cloud Storage with a multipart upload (objects.insert): its " +
		"content as text or base64 (content_encoding_kind), at most 262144 bytes, with its content type, headers, " +
		"custom metadata, storage class, custom time, a Cloud KMS key, and a predefined ACL. Nothing is " +
		"overwritten unless overwrite is true: otherwise the call sets ifGenerationMatch=0 and Google refuses " +
		"with 412 when the object exists. Larger content needs a resumable upload, which is not offered. The " +
		"result is the object as Google describes it, not its content." + objectWriteTail,
	Tags: []string{"google", "cloud", "storage", "objects", "upload", "create"}, Risk: objectsInsertRisk,
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,"name":{"type":"string",` +
		`"minLength":1,"maxLength":1024},"content":{"type":"string","maxLength":` + strconv.Itoa(maxUploadChars) +
		`},"content_encoding_kind":{"type":"string","enum":["text","base64"]},` + objectFieldProps("", true) +
		`,"kms_key_name":` + writeKMSProp + `,"predefined_acl":` + writeACLProp + `,"if_generation_match":` +
		writeGenerationOne + `,"if_metageneration_match":` + writeGenerationOne + `,` + writeGuardProps +
		`,"overwrite":` + writeOverwriteProp + `,` + storageUserProjectProp + `},"required":["bucket","name","content"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(objectSchema),
	Arguments: []capability.Argument{storageBucketArgument,
		{Name: "name", Required: true, Description: "Name of the object to create, at most 1024 bytes of UTF-8 " +
			"without control or format characters; slashes are part of the name, and \".\" or \"..\" alone is " +
			"refused. It is part of the confirmation, so a name that does not fit is refused"},
		{Name: "content", Required: true, Description: "Content of the object: text, or base64 with " +
			"content_encoding_kind base64. At most 262144 bytes after decoding; more is refused before any request"},
		{Name: "content_encoding_kind", Description: "text (default) if content is the text itself, base64 if " +
			"content is the base64 form (standard alphabet with padding) of the bytes"},
		writeContentTypeArgument, writeCacheControlArgument, writeDispositionArgument, writeEncodingArgument,
		writeLanguageArgument, writeMetadataArgument, writeContextsArgument, writeCustomTimeArgument,
		writeClassArgument, writeKMSArgument, writeACLArgument, writeIfGenerationArgument,
		writeIfMetagenerationArgument, writeProjectionArgument, writeIfGenerationNotMatchArgument,
		writeIfMetagenerationNotMatchArgument, writeOverwriteArgument, storageUserProjectArgument},
	Fields: objectWriteFields,
	Examples: []capability.Example{{
		Description: "Create a text object that must not exist yet",
		Arguments:   json.RawMessage(`{"bucket":"example-bucket","name":"notes/readme.txt","content":"hello","content_type":"text/plain"}`),
	}, {
		Description: "Replace a binary object, given as base64",
		Arguments: json.RawMessage(`{"bucket":"example-bucket","name":"data/blob.bin","content":"AQID",` +
			`"content_encoding_kind":"base64","overwrite":true}`),
	}},
}

func handleObjectsInsert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "upload object"
	var a objectsInsertArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	data, err := a.check()
	if err != nil {
		return nil, err
	}
	generation, _ := overwriteGeneration(a.Overwrite, a.IfGenerationMatch)
	metadata := a.objectWriteFieldsArguments.body()
	metadata["name"] = a.Name
	delete(metadata, "contentType")
	contentType := "application/octet-stream"
	if a.ContentType != "" {
		contentType = a.ContentType
		metadata["contentType"] = a.ContentType
	} else if a.ContentEncodingKind == "" || a.ContentEncodingKind == "text" {
		contentType = "text/plain; charset=utf-8"
	}
	payload, header, err := multipartBody(metadata, contentType, data)
	if err != nil {
		return nil, failure(op, "the request could not be built")
	}
	s, err := openStorage(ctx, op, objectsInsertID, []namedProject{{"user_project", a.UserProject}}, resolved,
		secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{"uploadType": {"multipart"}}
	setQueryValues(query, mergeValues(map[string]string{"kmsKeyName": a.KMSKeyName, "predefinedAcl": a.PredefinedACL,
		"ifGenerationMatch": generation, "ifMetagenerationMatch": a.IfMetagenerationMatch}, a.writeGuardsArguments.values()))
	var wire json.RawMessage
	if err := s.upload(ctx, objectsInsertID, a.Bucket, a.UserProject, query, header, payload, &wire); err != nil {
		return nil, err
	}
	return writtenObject(op, wire, a.Bucket, a.Name)
}

// --- objects.copy ---

type objectsCopyArguments struct {
	SourceBucket      string `json:"source_bucket"`
	SourceObject      string `json:"source_object"`
	SourceGeneration  string `json:"source_generation"`
	DestinationBucket string `json:"destination_bucket"`
	DestinationObject string `json:"destination_object"`
	writeGuardsArguments
	IfMetagenerationMatch          string                      `json:"if_metageneration_match"`
	IfSourceGenerationNotMatch     string                      `json:"if_source_generation_not_match"`
	IfSourceMetagenerationMatch    string                      `json:"if_source_metageneration_match"`
	IfSourceMetagenerationNotMatch string                      `json:"if_source_metageneration_not_match"`
	DestinationMetadata            *objectWriteFieldsArguments `json:"destination_metadata"`
	DestinationKMSKeyName          string                      `json:"destination_kms_key_name"`
	DestinationPredefinedACL       string                      `json:"destination_predefined_acl"`
	IfGenerationMatch              string                      `json:"if_generation_match"`
	IfSourceGenerationMatch        string                      `json:"if_source_generation_match"`
	Overwrite                      bool                        `json:"overwrite"`
	UserProject                    string                      `json:"user_project"`
}

func (a objectsCopyArguments) class() string {
	if a.DestinationMetadata != nil {
		return a.DestinationMetadata.StorageClass
	}
	return ""
}

func (a objectsCopyArguments) check() error {
	for _, check := range []error{checkBucket(a.SourceBucket), checkBucket(a.DestinationBucket),
		checkWriteName("source_object", a.SourceObject), checkWriteName("destination_object", a.DestinationObject),
		checkNumberArgument("source_generation", a.SourceGeneration),
		checkNumberArgument("if_source_generation_match", a.IfSourceGenerationMatch),
		checkNumberArgument("if_metageneration_match", a.IfMetagenerationMatch),
		checkNumberArgument("if_source_generation_not_match", a.IfSourceGenerationNotMatch),
		checkNumberArgument("if_source_metageneration_match", a.IfSourceMetagenerationMatch),
		checkNumberArgument("if_source_metageneration_not_match", a.IfSourceMetagenerationNotMatch),
		a.writeGuardsArguments.check(),
		checkWriteACL("destination_predefined_acl", a.DestinationPredefinedACL),
		checkWriteKMS("destination_kms_key_name", a.DestinationKMSKeyName)} {
		if check != nil {
			return check
		}
	}
	if a.DestinationMetadata != nil {
		if err := a.DestinationMetadata.check("destination_metadata."); err != nil {
			return err
		}
	}
	if _, err := overwriteGeneration(a.Overwrite, a.IfGenerationMatch); err != nil {
		return err
	}
	return fitConfirmation(a.summary())
}

func (a objectsCopyArguments) summary() string {
	source := "object " + quoted(a.SourceObject)
	if a.SourceGeneration != "" {
		source += " (generation " + a.SourceGeneration + ")"
	}
	out := "Copy " + source + " of bucket " + quoted(a.SourceBucket) + " to object " + quoted(a.DestinationObject) +
		" in bucket " + quoted(a.DestinationBucket) + " (size of the source object; " +
		classNote(a.class(), "no storage class given, so Google applies its default") + "); " +
		overwriteNote(a.Overwrite)
	if a.DestinationPredefinedACL != "" {
		out += "; predefined ACL " + a.DestinationPredefinedACL
	}
	return out
}

func objectsCopySummary(raw json.RawMessage) string {
	var a objectsCopyArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return a.summary()
}

var objectsCopyRisk = func() capability.Risk {
	risk := storageChangeRisk(capability.EffectCreate)
	risk.DataSensitivity = contentSensitivity
	return risk
}()

var objectsCopy = capability.Descriptor{
	ID: objectsCopyID, Version: 1, Title: "Copy a Cloud Storage object",
	Description: "Copy one object to another name, in the same bucket or another (objects.copy): optionally a " +
		"specific source generation, with new metadata for the copy (destination_metadata, from a fixed list of " +
		"fields), a Cloud KMS key, and a predefined ACL. Source and destination bucket must each belong to a " +
		"project in the targets: Qatlas proves both, one after the other, before the copy is sent. Nothing is " +
		"overwritten unless overwrite is true: otherwise the call sets ifGenerationMatch=0 and Google refuses " +
		"with 412 when the destination exists. The size of the copy is that of the source, which the " +
		"confirmation cannot show. The result is the new object, not its content." + objectWriteTail,
	Tags: []string{"google", "cloud", "storage", "objects", "copy", "create"}, Risk: objectsCopyRisk,
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"source_bucket":` + bucketProp() +
		`,"source_object":{"type":"string","minLength":1,"maxLength":1024},"source_generation":` + writeGenerationOne +
		`,"destination_bucket":` + bucketProp() + `,"destination_object":{"type":"string","minLength":1,` +
		`"maxLength":1024},"destination_metadata":{"type":"object","properties":{` + objectFieldProps("", true) +
		`},"additionalProperties":false},"destination_kms_key_name":` + writeKMSProp +
		`,"destination_predefined_acl":` + writeACLProp + `,"if_generation_match":` + writeGenerationOne +
		`,"if_source_generation_match":` + writeGenerationOne + `,"if_metageneration_match":` +
		writeGenerationOne + `,"if_source_generation_not_match":` + writeGenerationOne +
		`,"if_source_metageneration_match":` + writeGenerationOne + `,"if_source_metageneration_not_match":` +
		writeGenerationOne + `,` + writeGuardProps + `,"overwrite":` + writeOverwriteProp + `,` +
		storageUserProjectProp + `},"required":["source_bucket","source_object","destination_bucket",` +
		`"destination_object"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(objectSchema),
	Arguments: []capability.Argument{
		{Name: "source_bucket", Required: true, Description: "Bucket of the object to copy; never a path or URL"},
		{Name: "source_object", Required: true, Description: "Name of the object to copy, at most 1024 bytes"},
		{Name: "source_generation", Description: "Generation of the source object to copy, a decimal number; the " +
			"live version when omitted"},
		{Name: "destination_bucket", Required: true, Description: "Bucket the copy goes to; never a path or URL"},
		{Name: "destination_object", Required: true, Description: "Name of the copy, at most 1024 bytes of UTF-8 " +
			"without control or format characters. It is part of the confirmation, so a name that does not fit " +
			"is refused"},
		{Name: "destination_metadata", Description: "New metadata of the copy as an object with the members " +
			"content_type, cache_control, content_disposition, content_encoding, content_language, metadata, " +
			"contexts, custom_time, and storage_class (each as in objects.insert); without it the copy keeps the metadata " +
			"of the source"},
		{Name: "destination_kms_key_name", Description: writeKMSArgument.Description},
		{Name: "destination_predefined_acl", Description: writeACLArgument.Description},
		writeIfGenerationArgument,
		{Name: "if_source_generation_match", Description: "Only copy if the generation of the source object is " +
			"this decimal number"},
		writeIfMetagenerationArgument,
		{Name: "if_source_generation_not_match", Description: "Only copy if the generation of the source object is " +
			"not this decimal number"},
		{Name: "if_source_metageneration_match", Description: "Only copy if the metageneration of the source " +
			"object is this decimal number"},
		{Name: "if_source_metageneration_not_match", Description: "Only copy if the metageneration of the source " +
			"object is not this decimal number"},
		writeProjectionArgument, writeIfGenerationNotMatchArgument, writeIfMetagenerationNotMatchArgument,
		writeOverwriteArgument, storageUserProjectArgument},
	Fields: objectWriteFields,
	Examples: []capability.Example{{
		Description: "Copy an object into another bucket",
		Arguments: json.RawMessage(`{"source_bucket":"example-bucket","source_object":"a.txt",` +
			`"destination_bucket":"archive-bucket","destination_object":"2026/a.txt"}`),
	}},
}

func bucketProp() string {
	return `{"type":"string","minLength":3,"maxLength":63,"pattern":"^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$"}`
}

func copyPath(a objectsCopyArguments) string {
	return objectPath(a.SourceBucket, a.SourceObject) + "/copyTo/b/" + url.PathEscape(a.DestinationBucket) + "/o/" +
		url.PathEscape(a.DestinationObject)
}

func handleObjectsCopy(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "copy object"
	var a objectsCopyArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	generation, _ := overwriteGeneration(a.Overwrite, a.IfGenerationMatch)
	var payload []byte
	if a.DestinationMetadata != nil {
		if body := a.DestinationMetadata.body(); len(body) > 0 {
			var err error
			if payload, err = json.Marshal(body); err != nil {
				return nil, failure(op, "the request could not be built")
			}
		}
	}
	s, err := openStorage(ctx, op, objectsCopyID, []namedProject{{"user_project", a.UserProject}}, resolved,
		secrets, red)
	if err != nil {
		return nil, err
	}
	// Both buckets are bound by their own reading before the copy is sent; change binds the destination.
	if a.SourceBucket != a.DestinationBucket {
		if err := s.bindBucket(ctx, objectsCopyID, a.SourceBucket, a.UserProject); err != nil {
			return nil, err
		}
	}
	query := url.Values{}
	setQueryValues(query, mergeValues(map[string]string{"sourceGeneration": a.SourceGeneration,
		"destinationKmsKeyName": a.DestinationKMSKeyName, "destinationPredefinedAcl": a.DestinationPredefinedACL,
		"ifGenerationMatch": generation, "ifSourceGenerationMatch": a.IfSourceGenerationMatch,
		"ifMetagenerationMatch": a.IfMetagenerationMatch, "ifSourceGenerationNotMatch": a.IfSourceGenerationNotMatch,
		"ifSourceMetagenerationMatch":    a.IfSourceMetagenerationMatch,
		"ifSourceMetagenerationNotMatch": a.IfSourceMetagenerationNotMatch}, a.writeGuardsArguments.values()))
	var wire json.RawMessage
	if err := s.change(ctx, storageChange{op: op, tool: objectsCopyID, method: http.MethodPost, path: copyPath(a),
		bucket: a.DestinationBucket, userProject: a.UserProject, query: query, payload: payload}, &wire); err != nil {
		return nil, err
	}
	return writtenObject(op, wire, a.DestinationBucket, a.DestinationObject)
}

// --- objects.compose ---

type composeSource struct {
	Name              string `json:"name"`
	Generation        string `json:"generation"`
	IfGenerationMatch string `json:"if_generation_match"`
}

type objectsComposeArguments struct {
	Bucket                string            `json:"bucket"`
	DestinationObject     string            `json:"destination_object"`
	SourceObjects         []composeSource   `json:"source_objects"`
	ContentType           string            `json:"content_type"`
	Contexts              map[string]string `json:"contexts"`
	DropContextGroups     []string          `json:"drop_context_groups"`
	IfMetagenerationMatch string            `json:"if_metageneration_match"`
	DestinationACL        string            `json:"destination_predefined_acl"`
	KMSKeyName            string            `json:"kms_key_name"`
	IfGenerationMatch     string            `json:"if_generation_match"`
	Overwrite             bool              `json:"overwrite"`
	UserProject           string            `json:"user_project"`
}

func (a objectsComposeArguments) check() error {
	if err := checkBucket(a.Bucket); err != nil {
		return err
	}
	if err := checkWriteName("destination_object", a.DestinationObject); err != nil {
		return err
	}
	if len(a.SourceObjects) < minComposeSources || len(a.SourceObjects) > maxComposeSources {
		return badRequest("source_objects must hold " + strconv.Itoa(minComposeSources) + " to " +
			strconv.Itoa(maxComposeSources) + " objects")
	}
	for _, source := range a.SourceObjects {
		for _, check := range []error{checkWriteName("source_objects.name", source.Name),
			checkNumberArgument("source_objects.generation", source.Generation),
			checkNumberArgument("source_objects.if_generation_match", source.IfGenerationMatch)} {
			if check != nil {
				return check
			}
		}
	}
	if err := (objectWriteFieldsArguments{ContentType: a.ContentType, Contexts: a.Contexts}).check(""); err != nil {
		return err
	}
	if len(a.DropContextGroups) > 1 || len(a.DropContextGroups) == 1 && a.DropContextGroups[0] != "custom" {
		return badRequest("drop_context_groups must be [\"custom\"] or left out")
	}
	if err := checkNumberArgument("if_metageneration_match", a.IfMetagenerationMatch); err != nil {
		return err
	}
	for _, check := range []error{checkWriteACL("destination_predefined_acl", a.DestinationACL),
		checkWriteKMS("kms_key_name", a.KMSKeyName)} {
		if check != nil {
			return check
		}
	}
	if _, err := overwriteGeneration(a.Overwrite, a.IfGenerationMatch); err != nil {
		return err
	}
	return fitConfirmation(a.summary())
}

func (a objectsComposeArguments) summary() string {
	out := "Compose " + strconv.Itoa(len(a.SourceObjects)) + " objects of bucket " + quoted(a.Bucket) +
		" into object " + quoted(a.DestinationObject) + " (size: the sum of the sources; storage class: the " +
		"default class of the bucket); " + overwriteNote(a.Overwrite)
	if a.DestinationACL != "" {
		out += "; predefined ACL " + a.DestinationACL
	}
	return out
}

func objectsComposeSummary(raw json.RawMessage) string {
	var a objectsComposeArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return a.summary()
}

var objectsComposeRisk = func() capability.Risk {
	risk := storageChangeRisk(capability.EffectCreate)
	risk.DataSensitivity = contentSensitivity
	return risk
}()

var objectsCompose = capability.Descriptor{
	ID: objectsComposeID, Version: 1, Title: "Compose Cloud Storage objects into one",
	Description: "Concatenate 2 to 32 objects of one bucket into a new object of that bucket (objects.compose), " +
		"in the order given, each optionally at a specific generation. The source objects are kept: the tool " +
		"cannot delete them. All source objects must lie in the same bucket as the destination, which must " +
		"belong to a project in the targets. Nothing is overwritten unless overwrite is true: otherwise the call " +
		"sets ifGenerationMatch=0 and Google refuses with 412 when the destination exists. The result is the " +
		"composed object, not its content." + objectWriteTail,
	Tags: []string{"google", "cloud", "storage", "objects", "compose", "create"}, Risk: objectsComposeRisk,
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,"destination_object":` +
		`{"type":"string","minLength":1,"maxLength":1024},"source_objects":{"type":"array","minItems":2,` +
		`"maxItems":32,"items":{"type":"object","properties":{"name":{"type":"string","minLength":1,` +
		`"maxLength":1024},"generation":` + writeGenerationOne + `,"if_generation_match":` + writeGenerationOne +
		`},"required":["name"],"additionalProperties":false}},"content_type":` + writeContentTypeProp +
		`,"contexts":` + writeMetadataProp + `,"drop_context_groups":{"type":"array","maxItems":1,"items":` +
		`{"type":"string","enum":["custom"]}},"destination_predefined_acl":` + writeACLProp + `,"kms_key_name":` +
		writeKMSProp + `,"if_generation_match":` + writeGenerationOne + `,"if_metageneration_match":` +
		writeGenerationOne + `,"overwrite":` + writeOverwriteProp + `,` +
		storageUserProjectProp + `},"required":["bucket","destination_object","source_objects"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(objectSchema),
	Arguments: []capability.Argument{storageBucketArgument,
		{Name: "destination_object", Required: true, Description: "Name of the composed object, at most 1024 bytes " +
			"of UTF-8 without control or format characters. It is part of the confirmation, so a name that does " +
			"not fit is refused"},
		{Name: "source_objects", Required: true, Description: "2 to 32 source objects of the bucket, in order, each " +
			"{name, generation, if_generation_match}: name is required, generation picks a version, " +
			"if_generation_match requires that generation"},
		{Name: "content_type", Description: "Content type of the composed object; application/octet-stream when " +
			"omitted"},
		{Name: "destination_predefined_acl", Description: writeACLArgument.Description},
		writeContextsArgument,
		{Name: "drop_context_groups", Description: "[\"custom\"] to drop the custom contexts of the source " +
			"objects; without it and without contexts the sources' contexts are kept, and contexts given here " +
			"replace them"},
		writeKMSArgument, writeIfGenerationArgument, writeIfMetagenerationArgument, writeOverwriteArgument,
		storageUserProjectArgument},
	Fields: objectWriteFields,
	Examples: []capability.Example{{
		Description: "Join two log parts into one object",
		Arguments: json.RawMessage(`{"bucket":"example-bucket","destination_object":"logs/all.log",` +
			`"source_objects":[{"name":"logs/part1.log"},{"name":"logs/part2.log"}],"content_type":"text/plain"}`),
	}},
}

func handleObjectsCompose(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "compose object"
	var a objectsComposeArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	generation, _ := overwriteGeneration(a.Overwrite, a.IfGenerationMatch)
	sources := make([]map[string]any, 0, len(a.SourceObjects))
	for _, source := range a.SourceObjects {
		item := map[string]any{"name": source.Name}
		if source.Generation != "" {
			item["generation"] = source.Generation
		}
		if source.IfGenerationMatch != "" {
			item["objectPreconditions"] = map[string]any{"ifGenerationMatch": source.IfGenerationMatch}
		}
		sources = append(sources, item)
	}
	contentType := a.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	destination := objectWriteFieldsArguments{Contexts: a.Contexts}.body()
	destination["contentType"] = contentType
	payload, err := json.Marshal(map[string]any{"kind": "storage#composeRequest", "sourceObjects": sources,
		"destination": destination})
	if err != nil {
		return nil, failure(op, "the request could not be built")
	}
	s, err := openStorage(ctx, op, objectsComposeID, []namedProject{{"user_project", a.UserProject}}, resolved,
		secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	setQueryValues(query, map[string]string{"destinationPredefinedAcl": a.DestinationACL,
		"kmsKeyName": a.KMSKeyName, "ifGenerationMatch": generation, "ifMetagenerationMatch": a.IfMetagenerationMatch})
	if len(a.DropContextGroups) == 1 {
		query.Set("dropContextGroups", "custom")
	}
	var wire json.RawMessage
	if err := s.change(ctx, storageChange{op: op, tool: objectsComposeID, method: http.MethodPost,
		path: objectPath(a.Bucket, a.DestinationObject) + "/compose", bucket: a.Bucket, userProject: a.UserProject,
		query: query, payload: payload}, &wire); err != nil {
		return nil, err
	}
	return writtenObject(op, wire, a.Bucket, a.DestinationObject)
}

// --- objects.patch ---

type objectsPatchArguments struct {
	Bucket     string `json:"bucket"`
	Object     string `json:"object"`
	Generation string `json:"generation"`
	objectWriteFieldsArguments
	writeGuardsArguments
	RemoveMetadataKeys    []string `json:"remove_metadata_keys"`
	TemporaryHold         *bool    `json:"temporary_hold"`
	EventBasedHold        *bool    `json:"event_based_hold"`
	PredefinedACL         string   `json:"predefined_acl"`
	IfGenerationMatch     string   `json:"if_generation_match"`
	IfMetagenerationMatch string   `json:"if_metageneration_match"`
	UserProject           string   `json:"user_project"`
}

// changed names the fields the call changes, in a fixed order, without their values.
func (a objectsPatchArguments) changed() []string {
	var names []string
	for _, field := range []struct {
		name string
		set  bool
	}{{"content_type", a.ContentType != ""}, {"cache_control", a.CacheControl != ""},
		{"content_disposition", a.ContentDisposition != ""}, {"content_encoding", a.ContentEncoding != ""},
		{"content_language", a.ContentLanguage != ""}, {"custom_time", a.CustomTime != ""},
		{"contexts (" + strconv.Itoa(len(a.Contexts)) + " set)", len(a.Contexts) > 0},
		{"metadata (" + strconv.Itoa(len(a.Metadata)) + " set, " + strconv.Itoa(len(a.RemoveMetadataKeys)) +
			" removed)", len(a.Metadata) > 0 || len(a.RemoveMetadataKeys) > 0}} {
		if field.set {
			names = append(names, field.name)
		}
	}
	if a.TemporaryHold != nil {
		names = append(names, "temporary_hold="+strconv.FormatBool(*a.TemporaryHold))
	}
	if a.EventBasedHold != nil {
		names = append(names, "event_based_hold="+strconv.FormatBool(*a.EventBasedHold))
	}
	if a.PredefinedACL != "" {
		names = append(names, "predefined ACL "+a.PredefinedACL)
	}
	return names
}

func (a objectsPatchArguments) check() error {
	for _, check := range []error{checkBucket(a.Bucket), checkWriteName("object", a.Object),
		checkNumberArgument("generation", a.Generation),
		checkNumberArgument("if_generation_match", a.IfGenerationMatch),
		checkNumberArgument("if_metageneration_match", a.IfMetagenerationMatch),
		checkWriteACL("predefined_acl", a.PredefinedACL), a.writeGuardsArguments.check()} {
		if check != nil {
			return check
		}
	}
	if a.StorageClass != "" {
		return badRequest("storage_class cannot be changed by objects.patch")
	}
	if err := a.objectWriteFieldsArguments.check(""); err != nil {
		return err
	}
	for _, key := range a.RemoveMetadataKeys {
		if key == "" || len(key) > maxObjectMetadataKey || !utf8.ValidString(key) || strings.IndexFunc(key, unicode.IsControl) >= 0 {
			return badRequest("remove_metadata_keys must hold keys of 1 to 128 bytes of valid UTF-8 without " +
				"control characters")
		}
		if _, both := a.Metadata[key]; both {
			return badRequest("a key is in metadata and in remove_metadata_keys")
		}
	}
	if len(a.RemoveMetadataKeys) > maxObjectMetadataEntries {
		return badRequest("remove_metadata_keys must hold at most " + strconv.Itoa(maxObjectMetadataEntries) + " keys")
	}
	if len(a.changed()) == 0 {
		return badRequest("name at least one field to change")
	}
	return fitConfirmation(a.summary())
}

func (a objectsPatchArguments) summary() string {
	out := "Change the metadata of object " + quoted(a.Object) + " in bucket " + quoted(a.Bucket)
	if a.Generation != "" {
		out += " (generation " + a.Generation + ")"
	}
	return out + ": " + strings.Join(a.changed(), ", ") + "; the content stays"
}

func objectsPatchSummary(raw json.RawMessage) string {
	var a objectsPatchArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return a.summary()
}

var objectsPatch = capability.Descriptor{
	ID: objectsPatchID, Version: 1, Title: "Change the metadata of a Cloud Storage object",
	Description: "Change the metadata of one existing object in place (objects.patch), not its content: content " +
		"type, headers, custom metadata (keys set with metadata, keys removed with remove_metadata_keys), custom " +
		"time, the temporary and the event-based hold, and a predefined ACL. Only the fields named change. The " +
		"storage class and the ACL entries cannot be changed here. The method accepts only the scope " +
		"devstorage.full_control (or cloud-platform), which Google also uses for the access control lists. The " +
		"result is the changed object." + objectWriteTail,
	Tags: []string{"google", "cloud", "storage", "objects", "patch", "update"}, Risk: storageChangeRisk(capability.EffectUpdate),
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` + storageObjectProp +
		`,"generation":` + writeGenerationOne + `,` + objectFieldProps("", false) + `,"remove_metadata_keys":` +
		`{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,"maxLength":128}},` +
		`"temporary_hold":{"type":"boolean"},"event_based_hold":{"type":"boolean"},"predefined_acl":` + writeACLProp +
		`,"if_generation_match":` + writeGenerationOne + `,"if_metageneration_match":` + writeGenerationOne + `,` +
		writeGuardProps + `,` + storageUserProjectProp + `},"required":["bucket","object"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(objectSchema),
	Arguments: []capability.Argument{storageBucketArgument, storageObjectArgument,
		{Name: "generation", Description: "Generation of the object version to change, a decimal number; the live " +
			"version when omitted"},
		writeContentTypeArgument, writeCacheControlArgument, writeDispositionArgument, writeEncodingArgument,
		writeLanguageArgument,
		{Name: "metadata", Description: "Custom metadata keys to set or replace, as an object of text keys and " +
			"text values (limits as in objects.insert); other keys stay"},
		writeContextsArgument, writeCustomTimeArgument,
		{Name: "remove_metadata_keys", Description: "Custom metadata keys to remove, at most 100"},
		{Name: "temporary_hold", Description: "True to place, false to release the temporary hold, which protects " +
			"the object from deletion and replacement while set"},
		{Name: "event_based_hold", Description: "True to place, false to release the event-based hold"},
		writeACLArgument,
		{Name: "if_generation_match", Description: "Only change if the generation of the object is this decimal number"},
		writeIfMetagenerationArgument, writeProjectionArgument, writeIfGenerationNotMatchArgument,
		writeIfMetagenerationNotMatchArgument, storageUserProjectArgument},
	Fields: objectWriteFields,
	Examples: []capability.Example{{
		Description: "Set the cache control and a metadata key of an object",
		Arguments: json.RawMessage(`{"bucket":"example-bucket","object":"site/index.html",` +
			`"cache_control":"public, max-age=60","metadata":{"owner":"web"}}`),
	}},
}

func handleObjectsPatch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "patch object"
	var a objectsPatchArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	body := a.objectWriteFieldsArguments.body()
	delete(body, "metadata")
	if len(a.Metadata) > 0 || len(a.RemoveMetadataKeys) > 0 {
		metadata := map[string]any{}
		for key, value := range a.Metadata {
			metadata[key] = value
		}
		for _, key := range a.RemoveMetadataKeys {
			metadata[key] = nil
		}
		body["metadata"] = metadata
	}
	if a.TemporaryHold != nil {
		body["temporaryHold"] = *a.TemporaryHold
	}
	if a.EventBasedHold != nil {
		body["eventBasedHold"] = *a.EventBasedHold
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the request could not be built")
	}
	s, err := openStorage(ctx, op, objectsPatchID, []namedProject{{"user_project", a.UserProject}}, resolved,
		secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	setQueryValues(query, mergeValues(map[string]string{"generation": a.Generation, "predefinedAcl": a.PredefinedACL,
		"ifGenerationMatch": a.IfGenerationMatch, "ifMetagenerationMatch": a.IfMetagenerationMatch},
		a.writeGuardsArguments.values()))
	var wire json.RawMessage
	if err := s.change(ctx, storageChange{op: op, tool: objectsPatchID, method: http.MethodPatch,
		path: objectPath(a.Bucket, a.Object), bucket: a.Bucket, userProject: a.UserProject, query: query,
		payload: payload}, &wire); err != nil {
		return nil, err
	}
	return writtenObject(op, wire, a.Bucket, a.Object)
}
