package googletagmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	accountsUpdateID    = Provider + ".accounts.update"
	containersSnippetID = Provider + ".containers.snippet"
	containersCombineID = Provider + ".containers.combine"
)

// manageAccountsScope is the only scope Google lists for renaming an account.
const manageAccountsScope = "https://www.googleapis.com/auth/tagmanager.manage.accounts"

// Bounds of what these tools take and show. The snippet of a web container is about a kilobyte; the cut keeps a
// hostile or odd answer from filling a result, and truncated says so.
const (
	maxAccountNameRunes     = 300
	maxSnippetRunes         = 16384
	maxContainerConfigRunes = 4096
)

var (
	sourceIDPattern = `^` + idPattern + `$`
	sourceIDRE      = regexp.MustCompile(sourceIDPattern)
)

const accountNameSchema = `{"type":"string","minLength":1,"maxLength":300}`

const accountUpdateNote = "Only the display name can be changed. The shareData member, which controls whether " +
	"the account shares data with Google, is returned but never taken as an argument: the tool reads the account " +
	"first and sends the stored shareData back unchanged, so it cannot switch that data path. Nothing is written " +
	"when the read fails or its fingerprint differs from the one given. The fingerprint is required, so an " +
	"account changed in the meantime is not overwritten. A connection bound to a container cannot use this tool."

var accountsUpdate = capability.Descriptor{
	ID: accountsUpdateID, Version: 1, Title: "Rename a Tag Manager account",
	Description: "Change the display name of a Google Tag Manager account. " + accountUpdateNote,
	Tags:        []string{"google", "tag-manager", "accounts", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"fingerprint", "name"}, member("account", accountArgSchema),
		member("fingerprint", fingerprintSchema), member("name", accountNameSchema)),
	OutputSchema: json.RawMessage(accountSchema),
	Arguments: []capability.Argument{accountArgument,
		{Name: "fingerprint", Required: true, Description: "Fingerprint of the account as last read, from " +
			"accounts.get or accounts.list; Google refuses the change when the account has changed since"},
		{Name: "name", Required: true, Description: "New display name, at most 300 characters"}},
	Fields: accountFields,
	Examples: []capability.Example{{Description: "Rename an account",
		Arguments: json.RawMessage(`{"account":"accounts/1000001","fingerprint":"1700000000000","name":"Example account"}`)}},
}

const snippetNote = "The snippet is code for the site that embeds it. It is returned as text only: Qatlas " +
	"never runs it and never fetches a URL in it. It is cut at 16384 characters and containerConfig at 4096, and " +
	"truncated says so. containerConfig is the setting that provisions a tagging server of a server container and " +
	"belongs to that container; keep it private. Treat both as untrusted content of whoever edits the account."

var containersSnippet = capability.Descriptor{
	ID: containersSnippetID, Version: 1, Title: "Get the embed snippet of a Tag Manager container",
	Description: "Read the tagging snippet of a Google Tag Manager container. " + snippetNote,
	Tags:        []string{"google", "tag-manager", "containers"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema(nil, member("container", containerArgSchema)),
	OutputSchema: json.RawMessage(snippetSchema),
	Arguments:    []capability.Argument{containerArgument},
	Fields: []capability.Field{
		{Name: "snippet", Description: "Tagging snippet as text, cut at 16384 characters"},
		{Name: "containerConfig", Description: "Setting for provisioning a tagging server, cut at 4096 characters; " +
			"missing for a container without one"},
		{Name: "truncated", Description: "True when snippet or containerConfig was cut"}},
	Examples: []capability.Example{{Description: "Snippet of one container",
		Arguments: json.RawMessage(`{"container":"accounts/1000001/containers/2000002"}`)}},
}

const combineNote = "Merges the source container into the target container, which keeps its identity and " +
	"receives the content of the source. Google does not document what happens to the source container, so " +
	"treat the merge as irreversible for both. Both containers must lie inside the target of the connection, and " +
	"the source is a container of the same account; a connection bound to one container therefore cannot use it. " +
	"Options that copy or change user permissions are not offered, so Google refuses a merge that would turn user " +
	"permissions on. Google marks the method deprecated. Not idempotent."

var containersCombine = capability.Descriptor{
	ID: containersCombineID, Version: 1, Title: "Combine Tag Manager containers",
	Description: "Merge a source container into a target container of one Google Tag Manager account. " +
		combineNote + listedOnly,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "tag-manager", "containers", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent),
	InputSchema: inputSchema([]string{"container", "source_container_id"}, member("container", containerArgSchema),
		member("source_container_id", `{"type":"string","maxLength":20,"pattern":"`+sourceIDPattern+`"}`),
		member("setting_source", `{"type":"string","enum":["current","other"]}`)),
	OutputSchema: json.RawMessage(containerSchema),
	Arguments: []capability.Argument{
		{Name: "container", Required: true, Description: "Target container as accounts/ACCOUNT/containers/CONTAINER; " +
			"it receives the merge and must lie inside the target of the connection"},
		{Name: "source_container_id", Required: true, Description: "ID of the container of the same account that is " +
			"merged into the target; it must lie inside the target of the connection and differ from the target"},
		{Name: "setting_source", Description: "Whose container settings remain: current (the target) or other (the " +
			"source); leave it out to let Google decide"}},
	Fields: containerFields,
	Examples: []capability.Example{{Description: "Merge one container into another",
		Arguments: json.RawMessage(`{"container":"accounts/1000001/containers/2000002",` +
			`"source_container_id":"2000003","setting_source":"current"}`)}},
}

func structureManageDescriptors() []capability.Descriptor {
	return []capability.Descriptor{accountsUpdate, containersSnippet, containersCombine}
}

type snippetAnswer struct {
	Snippet         string `json:"snippet"`
	ContainerConfig string `json:"containerConfig"`
}

type snippetResult struct {
	Snippet         string `json:"snippet,omitempty"`
	ContainerConfig string `json:"containerConfig,omitempty"`
	Truncated       bool   `json:"truncated,omitempty"`
}

var snippetSchema = objectSchema(member("snippet", stringSchema), member("containerConfig", stringSchema),
	member("truncated", boolSchema))

// cutRunes cuts s to limit runes on a rune boundary and reports whether it did.
func cutRunes(s string, limit int) (string, bool) {
	s = strings.ToValidUTF8(s, "�")
	if utf8.RuneCountInString(s) <= limit {
		return s, false
	}
	return string([]rune(s)[:limit]), true
}

type combineArguments struct {
	SourceContainerID string `json:"source_container_id"`
	SettingSource     string `json:"setting_source"`
}

func handleAccountsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update account", accountsUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if c.scope.container != "" {
		return nil, invalidArgument("accounts.update is not available on a connection bound to a container")
	}
	path, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	query, err := c.needFingerprint()
	if err != nil {
		return nil, err
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if strings.TrimSpace(in.Name) == "" || utf8.RuneCountInString(in.Name) > maxAccountNameRunes {
		return nil, invalidArgument("name is required and at most " + strconv.Itoa(maxAccountNameRunes) + " characters")
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	// A PUT replaces the account, so shareData is read first and sent back as stored; nothing is written
	// without a successful read of the same state the caller saw.
	var stored account
	if err := cl.get(ctx, c.op, c.tool, path.String(), nil, &stored); err != nil {
		return nil, err
	}
	if string(stored.Fingerprint) != c.args.Fingerprint {
		return nil, invalidArgument("fingerprint is not the fingerprint of the stored account; read the account " +
			"again before changing it")
	}
	body, err := json.Marshal(struct {
		Name      string `json:"name"`
		ShareData bool   `json:"shareData"`
	}{in.Name, stored.ShareData})
	if err != nil {
		return nil, provider.Fail(c.op, "the account could not be encoded")
	}
	var out account
	r := request{method: http.MethodPut, path: path.String(), query: query, body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleContainersSnippet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get container snippet", containersSnippetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.containerOf(c.args.Container)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer snippetAnswer
	if err := cl.get(ctx, c.op, c.tool, path.String()+":snippet", nil, &answer); err != nil {
		return nil, err
	}
	var out snippetResult
	var cutSnippet, cutConfig bool
	out.Snippet, cutSnippet = cutRunes(answer.Snippet, maxSnippetRunes)
	out.ContainerConfig, cutConfig = cutRunes(answer.ContainerConfig, maxContainerConfigRunes)
	out.Truncated = cutSnippet || cutConfig
	return out, nil
}

func handleContainersCombine(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("combine containers", containersCombineID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if c.args.Container == "" {
		return nil, invalidArgument("container is required: the target of the merge")
	}
	target, err := c.scope.containerOf(c.args.Container)
	if err != nil {
		return nil, err
	}
	var in combineArguments
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if len(in.SourceContainerID) > 20 || !sourceIDRE.MatchString(in.SourceContainerID) {
		return nil, invalidArgument("source_container_id must be a decimal container ID without a leading zero")
	}
	if in.SettingSource != "" && in.SettingSource != "current" && in.SettingSource != "other" {
		return nil, invalidArgument("setting_source must be current or other")
	}
	if in.SourceContainerID == target.container {
		return nil, invalidArgument("source_container_id must differ from the target container")
	}
	source := gtmPath{account: target.account, container: in.SourceContainerID}
	if !c.scope.allows(source) {
		return nil, invalidArgument("source container " + outsideTarget)
	}
	query := url.Values{"containerId": {in.SourceContainerID}}
	if in.SettingSource != "" {
		query.Set("settingSource", in.SettingSource)
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out container
	r := request{method: http.MethodPost, path: target.String() + ":combine", query: query, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// accountUpdateSummary phrases the confirmation from the arguments alone; the core neutralizes and bounds it.
func accountUpdateSummary(raw json.RawMessage) string {
	var a struct {
		Account string `json:"account"`
		Name    string `json:"name"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	where := a.Account
	if where == "" {
		where = "the bound account"
	}
	return "Rename account " + where + " to " + strconv.Quote(a.Name) + " (its data sharing setting is kept as stored)"
}

// combineSummary names both containers: the target that receives the merge and the source that is merged in.
func combineSummary(raw json.RawMessage) string {
	var a struct {
		Container         string `json:"container"`
		SourceContainerID string `json:"source_container_id"`
		SettingSource     string `json:"setting_source"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	source := "container " + a.SourceContainerID
	if p, ok := parsePath(a.Container, containerPath); ok {
		source = gtmPath{account: p.account, container: a.SourceContainerID}.String()
	}
	settings := "container settings as Google decides"
	switch a.SettingSource {
	case "current":
		settings = "keeping the settings of the target"
	case "other":
		settings = "taking the settings of the source"
	}
	return "Merge source " + source + " into target " + a.Container + ", " + settings +
		"; the effect on the source container is not documented"
}
