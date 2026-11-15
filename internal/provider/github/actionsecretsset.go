package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/crypto/nacl/box"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/localfile"
)

// github.action-secrets.set creates or replaces one secret of a repository, of one environment of it, or of
// an organization. The value never passes through the caller: the tool takes the path of a local file the
// connection releases for reading (files.read), reads it whole and unchanged, seals it locally with the public
// key GitHub serves for the same level, and sends only the sealed text. There is no argument that carries a
// value. The call checks its arguments and its targets, then the file, and only then resolves a credential;
// GitHub sees one read of the public key and exactly one write, never repeated. The helpers below take a
// secretFamily so a second secret family with the same routes can share them.

// secretSetSensitivity classifies the set tool: it handles the secret value itself, though only sealed text
// ever leaves the machine.
const secretSetSensitivity = "github-actions-secret-value"

// maxSecretValueBytes is GitHub's limit for the value of a secret.
const maxSecretValueBytes = 48 << 10

// secretPublicKeyBytes is the size of a Curve25519 public key, as libsodium sealed boxes use it.
const secretPublicKeyBytes = 32

// secretRejected is what a refused write (422) means for this request; GitHub's own text stays apart.
const secretRejected = "GitHub rejected the secret as invalid; the key may have been replaced since it was read, " +
	"or the name, the visibility, or the selected repositories are not accepted; nothing was stored"

var secretVisibilityArguments = []capability.Argument{
	{Name: "visibility", Description: "Only for an organization secret, and required there: all, private, or " +
		"selected repositories may use it; all and private are refused when the connection's targets or " +
		"exclusions narrow the repositories of the organization, since the secret would reach repositories " +
		"outside them"},
	{Name: "selected_repositories", Description: "Only with visibility selected, and required with it: the " +
		"repositories of the organization that may use the secret, as OWNER/REPO, at most 50; each must lie " +
		"inside the connection's repository targets; Qatlas resolves their identifiers; replaces the current " +
		"selection"},
}

var secretLocalPathArgument = capability.Argument{Name: localfile.LocalPathArgument, Required: true,
	Description: "File that holds the value, at most 48 KB, absolute or starting with ~/, inside a directory the " +
		"connection releases for reading; the whole content is the value, so a line break at the end of the " +
		"file is part of it"}

func secretSetInput(targets string) json.RawMessage {
	return inputSchema(targets+`,"name":`+secretNameSchema+`,"`+localfile.LocalPathArgument+`":`+
		localfile.LocalPathSchema+`,"visibility":`+variableVisibilitySchema+`,"selected_repositories":`+
		selectedRepositoriesSchema, "name", localfile.LocalPathArgument)
}

func secretSetOutputFor(targets string) string {
	return `{"type":"object","properties":{"state":{"type":"string","enum":["created","updated"]},` +
		`"name":{"type":"string"},"visibility":{"type":"string"},` +
		`"selected_repositories":{"type":"array","items":{"type":"string"}},` + targets + `},` +
		`"required":["state","name"],"additionalProperties":false}`
}

var secretSetFields = []capability.Field{
	{Name: "state", Description: "created when GitHub made a new secret, updated when it replaced the value of " +
		"an existing one"},
	{Name: "name", Description: "Name of the secret"},
	{Name: "visibility", Description: "Visibility sent for an organization secret"},
	{Name: "selected_repositories", Description: "Repositories sent as the selected ones, as OWNER/REPO; absent " +
		"when none were sent"},
}

var actionSecretsSet = capability.Descriptor{
	ID:      Provider + ".action-secrets.set",
	Version: 1,
	Title:   "Set a GitHub Actions secret",
	Description: "Create or replace one GitHub Actions secret of a repository, one environment of it, or, with " +
		"organization instead, an organization a connection allows; the value is read from a local file the " +
		"connection releases for reading, sealed locally with the public key of the same level, and only the " +
		"sealed text is sent; the value is never an argument and never part of an answer; an organization " +
		"secret needs visibility; exactly one of repository or organization is required; idempotent for the " +
		"same file content; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "actions", "secrets", "set", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, secretSetSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	LocalFiles:            config.LocalFilesRead,
	InputSchema:           secretSetInput(variableTargetKeys),
	OutputSchema:          json.RawMessage(secretSetOutputFor(variableTargetProperties)),
	Arguments: append(append([]capability.Argument{secretNameArgument, secretLocalPathArgument},
		secretVisibilityArguments...), variableTargetArguments...),
	Fields: variableFieldsWith(secretSetFields...),
	Examples: []capability.Example{{
		Description: "Set a repository secret from a file",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","name":"DEPLOY_TOKEN","local_path":"~/secrets/deploy-token"}`),
	}, {
		Description: "Set an organization secret for selected repositories",
		Arguments: json.RawMessage(`{"organization":"orgs/octo-org","name":"DEPLOY_TOKEN",` +
			`"local_path":"~/secrets/deploy-token","visibility":"selected","selected_repositories":["octo-org/example"]}`),
	}},
}

// secretSetOperation binds the set tool of one secret family to its handler.
func secretSetOperation(family secretFamily, set capability.Descriptor) capability.Operation {
	return capability.Operation{Descriptor: set, Handler: secretsHandlerReading(family, set.ID, false,
		checkSecretSet, loadSecretFile,
		func(ctx context.Context, c *Client, a *secretArguments) (any, error) {
			return c.setSecret(ctx, family, a)
		})}
}

// checkSecretSet checks the name, the presence of the file, and the visibility rules, all before the file is
// opened: an organization secret needs a visibility, selected repositories lie inside the connection's
// targets, and all or private are refused when the targets or exclusions narrow the repositories of the
// organization, since the secret would then reach repositories outside them.
func checkSecretSet(_ secretFamily, a *secretArguments, bound target) error {
	if err := checkVariableNameValue("name", a.Name); err != nil {
		return err
	}
	if a.LocalPath == nil || *a.LocalPath == "" {
		return invalidRequest(localfile.LocalPathArgument + " is required")
	}
	if bound.kind == kindOwner && a.Visibility == nil {
		return invalidRequest("visibility is required for an organization secret: all, private, or selected")
	}
	selected, err := checkVisibility("secret", a.Visibility, a.SelectedRepositories, a.allowed, bound)
	if err != nil {
		return err
	}
	a.selected = selected
	if a.Visibility != nil && *a.Visibility != "selected" && a.allowed.restrictsRepositories(bound.owner) {
		return invalidRequest("visibility " + *a.Visibility + " would reach repositories of the organization " +
			"outside the targets of this connection; use selected with repositories inside the targets")
	}
	return nil
}

// restrictsRepositories reports whether the list narrows the repositories of one owner: a target names a
// single repository of it, or an exclusion covers repositories of it.
func (a allowlist) restrictsRepositories(login string) bool {
	for _, entry := range a.targets {
		if entry.kind == kindRepository && strings.EqualFold(entry.owner, login) && entry.repo != "*" {
			return true
		}
	}
	for _, entry := range a.excludes {
		if entry.kind == kindRepository && strings.EqualFold(entry.owner, login) {
			return true
		}
	}
	return false
}

// loadSecretFile reads the file of a set whole and unchanged into the arguments, within GitHub's limit and
// before any request. No message names the path or the content.
func loadSecretFile(ctx context.Context, resolved *config.Resolved, a *secretArguments) error {
	upload, err := localfile.OpenForUpload(ctx, resolved, *a.LocalPath)
	if err != nil {
		return err
	}
	defer upload.Close()
	switch {
	case upload.Size > maxSecretValueBytes:
		return invalidRequest(fmt.Sprintf("the file must be at most 48 KB (%d bytes)", maxSecretValueBytes))
	case upload.Size == 0:
		return invalidRequest("the file is empty; a secret needs a value")
	}
	buffer := make([]byte, upload.Size)
	if _, err := io.ReadFull(upload, buffer); err != nil {
		clear(buffer)
		return err
	}
	// The probe makes the upload check that the file ended where it began.
	var probe [1]byte
	if n, err := upload.Read(probe[:]); n != 0 || !errors.Is(err, io.EOF) {
		clear(buffer)
		if err == nil {
			err = errors.New("the local file changed while it was read")
		}
		return err
	}
	a.value = buffer
	return nil
}

// ActionSecretSet is the answer to a set secret: what GitHub did and what was sent besides the sealed value,
// never the value.
type ActionSecretSet struct {
	State                string   `json:"state"`
	Name                 string   `json:"name"`
	Visibility           string   `json:"visibility,omitempty"`
	SelectedRepositories []string `json:"selected_repositories,omitempty"`
}

// publicKeyRaw is the key GitHub serves for sealing the secrets of one level.
type publicKeyRaw struct {
	KeyID *string `json:"key_id"`
	Key   *string `json:"key"`
}

// secretPublicKey reads the key of the bound level and checks that it is a Curve25519 key of exactly 32
// bytes. A key of any other form ends the call before the value is sealed.
func (c *Client) secretPublicKey(ctx context.Context, op string, f secretFamily, environment string) (string, *[secretPublicKeyBytes]byte, error) {
	var raw publicKeyRaw
	if err := c.rest(ctx, op, f.path(c, environment, "")+"/public-key", &raw); err != nil {
		return "", nil, actionsFailure(err, f.changePermission(c.target, "setting them needs"))
	}
	if raw.KeyID == nil || raw.Key == nil || *raw.KeyID == "" || len(*raw.KeyID) > 256 {
		return "", nil, invalidResponse(op, false)
	}
	decoded, err := base64.StdEncoding.DecodeString(*raw.Key)
	if err != nil || len(decoded) != secretPublicKeyBytes {
		return "", nil, invalidResponse(op, false)
	}
	var key [secretPublicKeyBytes]byte
	copy(key[:], decoded)
	return *raw.KeyID, &key, nil
}

// setSecret resolves the selected repositories, reads the public key of the level, seals the value, and sends
// one write that is never repeated. GitHub answers 201 for a new secret and 204 for a replaced one; any other
// success is reported as uncertain.
func (c *Client) setSecret(ctx context.Context, f secretFamily, a *secretArguments) (*ActionSecretSet, error) {
	op := "set " + f.label + " secret"
	body := map[string]any{}
	result := &ActionSecretSet{Name: a.Name}
	if a.Visibility != nil {
		body["visibility"] = *a.Visibility
		result.Visibility = *a.Visibility
		if *a.Visibility == "selected" {
			ids, err := c.selectedRepositoryIDs(ctx, op, a.selected)
			if err != nil {
				return nil, err
			}
			body["selected_repository_ids"] = ids
			for _, repository := range a.selected {
				result.SelectedRepositories = append(result.SelectedRepositories, repository.argument())
			}
		}
	}
	keyID, key, err := c.secretPublicKey(ctx, op, f, a.environment)
	if err != nil {
		return nil, err
	}
	sealed, err := box.SealAnonymous(nil, a.value, key, rand.Reader)
	if err != nil {
		return nil, providerError(op, "the value could not be sealed")
	}
	body["encrypted_value"] = base64.StdEncoding.EncodeToString(sealed)
	body["key_id"] = keyID
	status, err := c.restChangeStatus(ctx, op, http.MethodPut, f.path(c, a.environment, a.Name), body, nil)
	if err != nil {
		return nil, variableFailure(err, f.changePermission(c.target, "setting them needs"), "", secretRejected)
	}
	switch status {
	case http.StatusCreated:
		result.State = "created"
	case http.StatusNoContent:
		result.State = "updated"
	default:
		return nil, invalidResponse(op, true)
	}
	return result, nil
}
