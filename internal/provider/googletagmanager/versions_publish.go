package googletagmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const versionsPublishID = Provider + ".versions.publish"

const publishScope = "https://www.googleapis.com/auth/tagmanager.publish"

// The comparison arguments are narrow on purpose: they are phrased into the confirmation as given.
const (
	livePattern     = `^(none|(0|` + idPattern + `))$`
	publicIDPattern = `^GTM-[A-Z0-9]{1,16}$`
	noLiveVersion   = "none"
)

var (
	liveIDForm   = regexp.MustCompile(livePattern)
	publicIDForm = regexp.MustCompile(publicIDPattern)
)

const publishNote = "Makes the version the live version of its container, which changes what runs on every site " +
	"or app that loads the container. The Custom HTML, Custom JavaScript, and template code of the version run " +
	"for visitors once it is live. The only way back is to publish an earlier version with this tool again. " +
	"Before it publishes, the tool reads the version and the live version through the same client and publishes " +
	"nothing when either read fails; the previous live version is returned as previousLiveVersion. The " +
	"fingerprint, previous_live_version_id, and container_public_id must be the values as last read: they are " +
	"compared with what the reads show and never sent, and the confirmation names them, so a person sees the " +
	"container, the version, and the live version it replaces; nothing is published when one differs. When Google answers with compilerError, the " +
	"version is not assumed to be live: the live version is read again and returned as liveVersion. Not " +
	"idempotent: after an unclear outcome the tool never repeats the request, so read the live version " +
	"before publishing again."

var versionsPublish = capability.Descriptor{
	ID: versionsPublishID, Version: 1, Title: "Publish a Tag Manager container version",
	Description:           "Publish one container version so that it becomes live. " + publishNote + listedOnly,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "tag-manager", "versions", "execute"}, Provider: Provider,
	Risk: changeRisk(capability.EffectExecute, capability.IdempotencyNonIdempotent),
	InputSchema: inputSchema([]string{"version", "fingerprint", "previous_live_version_id", "container_public_id"},
		member("version", versionArgSchema), member("fingerprint", fingerprintSchema),
		member("previous_live_version_id", `{"type":"string","maxLength":20,"pattern":"`+livePattern+`"}`),
		member("container_public_id", `{"type":"string","maxLength":20,"pattern":"`+publicIDPattern+`"}`)),
	OutputSchema: json.RawMessage(objectSchema(member("containerVersion", versionSchema),
		member("compilerError", boolSchema), member("previousLiveVersion", versionRefSchema),
		member("liveVersion", versionRefSchema), member("liveVersionUnread", boolSchema))),
	Arguments: []capability.Argument{{Name: "version", Required: true, Description: "Version as " + formVersion +
		", from version-headers.list; it must lie inside the target of the connection"},
		{Name: "fingerprint", Required: true, Description: "Fingerprint of the version as last read, from " +
			"versions.get; nothing is published when it differs from the stored one"},
		{Name: "previous_live_version_id", Required: true, Description: "ID of the version that is live now, as " +
			"versions.live shows it, or none when it shows no version; compared with the live version read " +
			"before publishing, never sent, and named in the confirmation"},
		{Name: "container_public_id", Required: true, Description: "publicId of the container such as GTM-XXXX, " +
			"from versions.get (container); compared with the container of the version read before publishing, " +
			"never sent, and named in the confirmation"}},
	Fields: []capability.Field{
		{Name: "containerVersion", Description: "The version as Google returns it after publishing, as versions.get shows it"},
		{Name: "compilerError", Description: "True when Google reports a compiler error; the version may not be live"},
		{Name: "previousLiveVersion", Description: "The version that was live before: path, containerVersionId, " +
			"name, fingerprint; missing when none was live"},
		{Name: "liveVersion", Description: "Present with compilerError: the live version read again afterwards, " +
			"with the same members as previousLiveVersion"},
		{Name: "liveVersionUnread", Description: "True when compilerError was set and the live version could not be read again"},
	},
	Examples: []capability.Example{{Description: "Publish one version",
		Arguments: json.RawMessage(`{"version":"accounts/1000001/containers/2000002/versions/7",` +
			`"fingerprint":"1700000000000","previous_live_version_id":"3","container_public_id":"GTM-EXAMPLE"}`)}},
}

type publishArguments struct {
	versionArguments
	PreviousLiveVersionID string `json:"previous_live_version_id"`
	ContainerPublicID     string `json:"container_public_id"`
}

func (v containerVersion) containerPublicID() string {
	if v.Container == nil {
		return ""
	}
	return string(v.Container.PublicID)
}

// shownRead names a value read from Google in an error only when it has the plain form an argument may have;
// any other text of the provider stays out of the message.
func shownRead(form *regexp.Regexp, read string) string {
	if read == "" || len(read) > 20 || !form.MatchString(read) {
		return " (the read shows another value)"
	}
	return " (the read shows " + read + ")"
}

// versionRef names a version without its content.
type versionRef struct {
	Path               idText   `json:"path,omitempty"`
	ContainerVersionID idText   `json:"containerVersionId,omitempty"`
	Name               nameText `json:"name,omitempty"`
	Fingerprint        idText   `json:"fingerprint,omitempty"`
}

var versionRefSchema = objectSchema(member("path", stringSchema), member("containerVersionId", stringSchema),
	member("name", stringSchema), member("fingerprint", stringSchema))

func refOf(v containerVersion) *versionRef {
	ref := versionRef{Path: v.Path, ContainerVersionID: v.ContainerVersionID, Name: v.Name, Fingerprint: v.Fingerprint}
	if ref == (versionRef{}) {
		return nil
	}
	return &ref
}

type publishAnswer struct {
	ContainerVersion *containerVersion `json:"containerVersion"`
	CompilerError    bool              `json:"compilerError"`
}

type publishResult struct {
	ContainerVersion    *versionResult `json:"containerVersion,omitempty"`
	CompilerError       bool           `json:"compilerError,omitempty"`
	PreviousLiveVersion *versionRef    `json:"previousLiveVersion,omitempty"`
	LiveVersion         *versionRef    `json:"liveVersion,omitempty"`
	LiveVersionUnread   bool           `json:"liveVersionUnread,omitempty"`
}

func handleVersionsPublish(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("publish version", versionsPublishID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var args publishArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	path, err := c.scope.versionOf(args.Version)
	if err != nil {
		return nil, err
	}
	query, err := c.needFingerprint()
	if err != nil {
		return nil, err
	}
	if len(args.PreviousLiveVersionID) > 20 || !liveIDForm.MatchString(args.PreviousLiveVersionID) {
		return nil, invalidArgument("previous_live_version_id must be a decimal ID or none")
	}
	if len(args.ContainerPublicID) > 20 || !publicIDForm.MatchString(args.ContainerPublicID) {
		return nil, invalidArgument("container_public_id must be a public ID such as GTM-XXXX")
	}
	if !capability.Confirmed(ctx) {
		return nil, invalidArgument("publishing a version needs the confirmation of the request")
	}
	container, _, _ := strings.Cut(path, "/versions/")
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	// Both reads come first and any failure ends the call: nothing is published without knowing what is
	// replaced. The stored fingerprint is compared here as well, so a stale one never reaches Google.
	stored, err := cl.getVersion(ctx, c.op, c.tool, path, nil)
	if err != nil {
		return nil, err
	}
	if string(stored.Fingerprint) != c.args.Fingerprint {
		return nil, invalidArgument("fingerprint is not the fingerprint of the stored version; read the version " +
			"again before publishing it")
	}
	if stored.Container == nil || string(stored.Container.PublicID) != args.ContainerPublicID {
		return nil, invalidArgument("container_public_id is not the public ID of the container of the version" +
			shownRead(publicIDForm, stored.containerPublicID()) + "; read the version with versions.get")
	}
	previous, err := cl.getVersion(ctx, c.op, c.tool, container+"/versions:live", nil)
	if err != nil {
		return nil, err
	}
	liveID := string(previous.ContainerVersionID)
	if liveID == "" {
		liveID = noLiveVersion
	}
	if liveID != args.PreviousLiveVersionID {
		return nil, invalidArgument("previous_live_version_id is not the live version of the container" +
			shownRead(liveIDForm, liveID) + "; read it with versions.live")
	}
	var answer publishAnswer
	r := request{method: http.MethodPost, path: path + ":publish", query: query, change: true,
		limit: maxVersionResponse}
	if err := cl.send(ctx, c.op, c.tool, r, &answer); err != nil {
		return nil, err
	}
	out := publishResult{CompilerError: answer.CompilerError, PreviousLiveVersion: refOf(previous)}
	if answer.ContainerVersion != nil {
		shown := versionOutput(*answer.ContainerVersion)
		out.ContainerVersion = &shown
	}
	if answer.CompilerError {
		// A compiler error does not say whether Google made the version live, so the state is read, not assumed.
		live, err := cl.getVersion(ctx, c.op, c.tool, container+"/versions:live", url.Values(nil))
		if err != nil {
			out.LiveVersionUnread = true
		} else {
			out.LiveVersion = refOf(live)
		}
	}
	return out, nil
}

// publishSummary phrases the confirmation from the arguments alone; the core allows no read of the provider
// there, so the container, version, and live version are named by the comparison arguments, which the handler
// checks against the reads before it publishes. Names are not part of it; they come back in the result.
func publishSummary(raw json.RawMessage) string {
	var a struct {
		Version               string `json:"version"`
		Fingerprint           string `json:"fingerprint"`
		PreviousLiveVersionID string `json:"previous_live_version_id"`
		ContainerPublicID     string `json:"container_public_id"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	container, id, _ := strings.Cut(a.Version, "/versions/")
	live := "version " + a.PreviousLiveVersionID
	if a.PreviousLiveVersionID == noLiveVersion {
		live = "no live version"
	}
	return "Publish version " + id + " (" + a.Version + ", fingerprint " + a.Fingerprint + ") of container " +
		a.ContainerPublicID + " (" + container + ") as the live version, replacing " + live + "; its Custom HTML, " +
		"Custom JavaScript, and template code then run for visitors, and the only way back is to publish an " +
		"earlier version. The call is refused unless the container and live version match what is read"
}

func versionPublishDescriptors() []capability.Descriptor {
	return []capability.Descriptor{versionsPublish}
}
