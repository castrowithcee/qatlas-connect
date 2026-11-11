// Package selfupdate checks and installs signed qconnect releases with the kit's selfupdate package. What it
// adds is qconnect's: the repository, the names and the signature of a release by its version, the trusted
// keys, the installation under <prefix>/bin, the former name qatlas beside the program, the manual pages,
// and the words of its errors and questions.
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	kitselfupdate "github.com/castrowithcee/qatlas-kit/selfupdate"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/release"
)

const (
	repositoryAPI = "https://api.github.com/repos/castrowithcee/qatlas-connect"
	// MinVersion is the oldest release that can be listed or installed.
	MinVersion = "v0.30.0"
)

var ErrDevelopmentBuild = errors.New("self-update is unavailable in a dev build; install a tagged release first")

// UnsupportedInstallationError reports an installation that cannot be replaced safely in place.
type UnsupportedInstallationError struct{ Reason string }

func (e *UnsupportedInstallationError) Error() string {
	return "cannot self-update this installation: " + e.Reason
}

// TargetError reports a release that cannot be chosen, with the reason in words for the person.
type TargetError struct{ Reason string }

func (e *TargetError) Error() string { return e.Reason }

// Result is the stable payload of an update check or installation.
type Result struct {
	Current         string
	Latest          string
	UpdateAvailable bool
	Updated         bool
	// Signed reports that the installed release carries a valid signature over its checksums: set with
	// Updated, since nothing unverified is installed.
	Signed bool
}

// Entry is one release a person can choose.
type Entry = kitselfupdate.Entry

// Release names copies of the verified release files a hook of Client may read, in a private directory
// that exists until the installation returns.
type Release = kitselfupdate.ReleaseFiles

// Target is a release chosen for installation, resolved and checked but not yet downloaded.
type Target struct {
	Version    string
	Prerelease bool
	// Current reports that Version is the running version; installing it changes nothing.
	Current bool
	// Older reports that Version is older than the running version: installing it is a rollback.
	Older bool
	// running is the running version, for the result.
	running string
	target  kitselfupdate.Target
}

// Client checks the GitHub releases of qconnect and updates one installed executable. Exported fields are
// test seams; zero values select the production repository, architecture, executable, and HTTP client.
type Client struct {
	// BaseURL is the repository API URL, https://api.github.com/repos/<owner>/<repository> or a local test
	// server under the same path.
	BaseURL    string
	HTTPClient *http.Client
	Version    string
	Token      string
	// GOARCH selects the archive; the program and its paths are always those of runtime.GOOS.
	GOARCH     string
	Executable string
	// SigningKeys replaces the compiled-in release keys; tests only.
	SigningKeys []ssh.PublicKey
	// BeforeReplace, when set, runs once the release is downloaded and verified and the new files are staged
	// next to the installed ones, right before the first of them replaces an installed file. A process of the
	// old program that has to be stopped while it still passes for this program, such as the vault process,
	// is stopped there, or prepared for a successor. An error from it ends the update with nothing installed.
	BeforeReplace func(ctx context.Context, release Release) error
	// AfterReplace, when set, runs once the program is replaced and the names beside it are in order, with
	// the same release. The update is done by then, so it has no error to return.
	AfterReplace func(ctx context.Context, release Release)
	// ValidateRollback must check the program of an older release against an isolated copy of the
	// configuration (see Prepared.ReadsConfig) right before it is installed; without it an older release is
	// refused.
	ValidateRollback func(ctx context.Context, p *Prepared) error
	// Progress, when set, is called while the release archive downloads, with the bytes received so far and
	// the archive's size, or -1 when the server does not say.
	Progress func(done, total int64)
	// CheckTimeout bounds one run of a prepared program in ReadsConfig; zero selects the default.
	CheckTimeout time.Duration
}

// testBaseURL is set at build time only (-ldflags -X) to point the updater of a test binary at the
// repository API of a local release server instead of GitHub, so that end-to-end tests can run real
// binaries against test releases.
var testBaseURL string

// prepareTimeout bounds the downloads of a release as a whole: the kit only ends a download that stalls, so
// a server that keeps trickling would otherwise hold it open. A variable so that a test can shorten it.
var prepareTimeout = 30 * time.Minute

// New returns the production updater for version.
func New(version string) *Client { return &Client{Version: version, BaseURL: testBaseURL} }

// kit returns the kit client that reads, verifies, and installs the releases of qconnect, with program as
// the program name of the release.
func (c *Client) kit(program string) *kitselfupdate.Client {
	keys := c.SigningKeys
	if keys == nil {
		keys = release.TrustedKeys()
	}
	goarch := c.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return &kitselfupdate.Client{
		RepositoryAPI:  c.repository(),
		CurrentVersion: c.Version,
		MinimumVersion: MinVersion,
		// The version asked for decides the names and the signature, never the files the release offers.
		ArchiveName: func(version string) string {
			layout, err := release.LayoutOf(version)
			if err != nil {
				return ""
			}
			return layout.ArchiveName(version, runtime.GOOS, goarch)
		},
		UserAgent:      "qatlas-connect/" + c.Version,
		HTTPClient:     c.HTTPClient,
		Token:          c.token(),
		Namespace:      release.Namespace,
		SignatureAsset: release.SignatureAsset,
		TrustedKeys:    keys,
		PreviousSignature: &kitselfupdate.PreviousSignature{
			LastVersion: release.LastLegacyVersion, Namespace: release.LegacyNamespace,
			SignatureAsset: release.LegacySignatureAsset,
		},
		ProgramName: program,
		Progress:    c.Progress,
	}
}

// kitFor is kit for the release version: its program and, except on Windows, its manual page beside it,
// whose path is left to the installation.
func (c *Client) kitFor(version string) (*kitselfupdate.Client, release.Layout, error) {
	layout, err := release.LayoutOf(version)
	if err != nil {
		return nil, release.Layout{}, &TargetError{fmt.Sprintf("%q is not a release version: %v", version, err)}
	}
	k := c.kit(layout.Program)
	if runtime.GOOS != "windows" {
		k.Companions = []kitselfupdate.Companion{{Entry: layout.ManpageFile()}}
	}
	return k, layout, nil
}

func (c *Client) repository() string {
	if base := strings.TrimRight(c.BaseURL, "/"); base != "" {
		return base
	}
	return repositoryAPI
}

// token is the GitHub token for the production repository; another repository URL never receives one from
// the environment.
func (c *Client) token() string {
	if c.Token != "" {
		return c.Token
	}
	if c.repository() != repositoryAPI {
		return ""
	}
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GITHUB_TOKEN")
}

func (c *Client) development() bool { return c.Version == "" || c.Version == "dev" }

// Check reports whether GitHub has a newer signed stable release without downloading an artifact.
func (c *Client) Check(ctx context.Context) (Result, error) {
	if c.development() {
		return Result{}, ErrDevelopmentBuild
	}
	r, err := c.kit(currentName).Check(ctx)
	if err != nil {
		return Result{}, explain(err)
	}
	return Result{Current: r.Current, Latest: r.Latest, UpdateAvailable: r.UpdateAvailable}, nil
}

// Update installs the newest signed stable release when it is newer than the running version.
func (c *Client) Update(ctx context.Context) (Result, error) {
	result, err := c.Check(ctx)
	if err != nil || !result.UpdateAvailable {
		return result, err
	}
	target, err := c.Target(ctx, result.Latest)
	if err != nil {
		return Result{}, err
	}
	return c.UpdateTo(ctx, target)
}

// List returns the releases a person can choose, newest first: the ten newest stable releases, every
// prerelease newer than the newest stable one, and the running version when it is such a release. Only
// signed releases from MinVersion on appear.
func (c *Client) List(ctx context.Context) ([]Entry, error) {
	if c.development() {
		return nil, ErrDevelopmentBuild
	}
	entries, err := c.kit(currentName).List(ctx)
	return entries, explain(err)
}

// Target resolves tag to a release that can be installed. It refuses a tag that is not a signed release
// from MinVersion on. A version older than the running one is allowed and marked Older.
func (c *Client) Target(ctx context.Context, tag string) (Target, error) {
	if c.development() {
		return Target{}, ErrDevelopmentBuild
	}
	t, err := c.kit(currentName).Target(ctx, tag)
	if err != nil {
		return Target{}, explain(err)
	}
	return Target{Version: t.Version, Prerelease: t.Prerelease, Current: t.Current, Older: t.Older, running: c.Version, target: t}, nil
}

// UpdateTo installs the release t names, through the same download, verification, and replacement as
// Update. Nothing is changed when t is the running version. An older version is refused here: a rollback
// goes through Prepare, a check of the program against the configuration, and InstallPrepared.
func (c *Client) UpdateTo(ctx context.Context, t Target) (Result, error) {
	if t.Older {
		return Result{}, &TargetError{fmt.Sprintf("%s is older than the running version and is installed as a checked rollback", t.Version)}
	}
	if t.Current {
		return Result{Current: t.running, Latest: t.Version}, nil
	}
	p, err := c.Prepare(ctx, t)
	if err != nil {
		return Result{}, err
	}
	return c.InstallPrepared(ctx, p)
}

// Prepared is a release that is downloaded and verified (signature, checksum, archive contents) but not
// installed. A caller can run the program it holds before it replaces anything (see ReadsConfig).
type Prepared struct {
	prepared *kitselfupdate.Prepared
	layout   release.Layout
	timeout  time.Duration
}

// Version is the release's tag.
func (p *Prepared) Version() string { return p.prepared.Version() }

// Prepare downloads and verifies the release t names without installing it. It refuses the running
// version, which has nothing to install.
func (c *Client) Prepare(ctx context.Context, t Target) (*Prepared, error) {
	if t.Current {
		return nil, &TargetError{fmt.Sprintf("%s is the running version", t.Version)}
	}
	k, layout, err := c.kitFor(t.Version)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, prepareTimeout)
	defer cancel()
	p, err := k.Prepare(ctx, t.target)
	if err != nil {
		var target *kitselfupdate.TargetError
		if errors.As(err, &target) {
			return nil, explain(err)
		}
		return nil, fmt.Errorf("release %s could not be downloaded and verified, nothing was changed: %w", t.Version, err)
	}
	return &Prepared{prepared: p, layout: layout, timeout: c.CheckTimeout}, nil
}

// ReadsConfig reports whether the program of the prepared release reads config, the bytes of a
// configuration file: the kit runs the verified program, under its own name, with 'config validate' against
// a copy in a private directory and an environment of its own, so neither the real file nor anything beside
// it is ever passed to it. A run that cannot be carried out is an error, not an answer.
func (p *Prepared) ReadsConfig(ctx context.Context, config []byte) (bool, error) {
	return p.prepared.ReadsConfig(ctx, kitselfupdate.ConfigCheck{
		Config:   config,
		FileName: "config.yaml",
		Args:     func(path string) []string { return []string{"--config", path, "config", "validate"} },
		Timeout:  p.timeout,
	})
}

// InstallPrepared installs a release Prepare returned, with the hooks of c, at the installation of the
// running program (see installationOf).
func (c *Client) InstallPrepared(ctx context.Context, p *Prepared) (Result, error) {
	inst, err := c.installationOf(p.layout)
	if err != nil {
		return Result{}, err
	}
	k, _, err := c.kitFor(p.Version())
	if err != nil {
		return Result{}, err
	}
	k.Executable = inst.program
	for i := range k.Companions {
		k.Companions[i].Path = inst.manpage
	}
	k.BeforeReplace = c.BeforeReplace
	if c.ValidateRollback != nil {
		k.ValidateRollback = func(ctx context.Context, _ *kitselfupdate.Prepared) error { return c.ValidateRollback(ctx, p) }
	}
	var finishErr error
	k.AfterReplace = func(ctx context.Context, files Release) {
		if finishErr = inst.finish(files); finishErr == nil && c.AfterReplace != nil {
			c.AfterReplace(ctx, files)
		}
	}
	undo, err := inst.prepare()
	if err != nil {
		return Result{}, err
	}
	if _, err := k.InstallPrepared(ctx, p.prepared); err != nil {
		undo()
		return Result{}, explain(err)
	}
	if finishErr != nil {
		return Result{}, finishErr
	}
	return Result{Current: c.Version, Latest: p.Version(), UpdateAvailable: true, Updated: true, Signed: true}, nil
}

// explain words the kit's errors about a release, the installation, or the running build for the person;
// any other error comes back as it is.
func explain(err error) error {
	var target *kitselfupdate.TargetError
	var unsupported *kitselfupdate.UnsupportedInstallationError
	switch {
	case errors.Is(err, kitselfupdate.ErrDevelopmentBuild):
		return ErrDevelopmentBuild
	case errors.As(err, &unsupported):
		return &UnsupportedInstallationError{Reason: unsupported.Reason + " (" + unsupported.Path + ")"}
	case !errors.As(err, &target):
		return err
	}
	switch target.Reason {
	case kitselfupdate.TargetInvalidVersion:
		return &TargetError{fmt.Sprintf("%q is not a release version", target.Version)}
	case kitselfupdate.TargetBelowMinimum:
		return &TargetError{fmt.Sprintf("%s is older than %s, the oldest version qconnect installs", target.Version, MinVersion)}
	case kitselfupdate.TargetNotFound:
		return &TargetError{fmt.Sprintf("release %s does not exist", target.Version)}
	case kitselfupdate.TargetRunning:
		return &TargetError{fmt.Sprintf("%s is the running version", target.Version)}
	default:
		return &TargetError{fmt.Sprintf("release %s is not signed or is incomplete and cannot be chosen", target.Version)}
	}
}
