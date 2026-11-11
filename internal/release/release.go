// Package release names the files, signature, and trusted keys of a qconnect release by its version. The
// checks themselves are those of the kit's release package.
package release

import (
	"fmt"
	"strings"

	kitrelease "github.com/castrowithcee/qatlas-kit/release"
	"golang.org/x/crypto/ssh"
)

const (
	// Namespace is the SSHSIG namespace of the signature a release after LastLegacyVersion is verified with,
	// published as SignatureAsset.
	Namespace = "qatlas-connect-release"
	// SignatureAsset is the release file that holds the signature under Namespace.
	SignatureAsset = "checksums.txt.qconnect.sig"

	// LegacyNamespace and LegacySignatureAsset are the signature of the releases up to LastLegacyVersion, the
	// only one their updaters check. A rollback to such a release is verified with it. A later release
	// carries it for those updaters as well, made with the same key, but is never verified with it here.
	// Transition until the next major release.
	LegacyNamespace      = "qatlas-release"
	LegacySignatureAsset = "checksums.txt.sig"

	// LastLegacyVersion is the newest release before the rename: the last one whose assets are named qatlas
	// and that is signed under LegacyNamespace only. Every later version, prereleases included, ships
	// qconnect assets and is verified under Namespace, whatever number the first of them gets. The version a
	// release is checked for is the one asked for, never inferred from the files a release offers, which an
	// attacker can leave out; the signed checksums.txt binds it, since it lists each archive by a name that
	// carries the version.
	LastLegacyVersion = "v2.2.0"
)

// trustedKeyLines lists the public keys that may sign a release, in authorized_keys format. A successor
// key is added one release before a rotation so that installed programs already trust it. The list is
// fixed at build time: nothing at run time extends or replaces it. It must match
// .github/release-allowed-signers.
var trustedKeyLines = []string{
	"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ6euNyMyw+0rV5TVSa24o374+wDn75ueHvdYAGZnEv5",
}

// extraKeyLines is set at build time only (-ldflags -X) to trust further keys, one authorized_keys line
// per line, so that end-to-end tests can run real binaries against test releases.
var extraKeyLines string

// TrustedKeys returns the compiled-in public keys that may sign a release.
func TrustedKeys() []ssh.PublicKey {
	lines := append([]string(nil), trustedKeyLines...)
	for _, line := range strings.Split(extraKeyLines, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	keys := make([]ssh.PublicKey, 0, len(lines))
	for _, line := range lines {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			panic("release: invalid compiled-in public key: " + err.Error())
		}
		keys = append(keys, key)
	}
	return keys
}

// Layout names the files of a release by its version: up to LastLegacyVersion the former names, after it
// the current ones.
type Layout struct {
	// Legacy marks a release up to LastLegacyVersion. Transition until the next major release.
	Legacy bool
	// Program is the name of the program, which also names its archive and manual page.
	Program string
	// SignatureAsset is the release file that holds the signature, and Namespace its SSHSIG namespace.
	SignatureAsset, Namespace string
}

// LayoutOf returns the layout of the release version.
func LayoutOf(version string) (Layout, error) {
	v, err := ParseVersion(version)
	if err != nil {
		return Layout{}, err
	}
	if v.Compare(lastLegacy()) <= 0 {
		return Layout{Legacy: true, Program: "qatlas", SignatureAsset: LegacySignatureAsset, Namespace: LegacyNamespace}, nil
	}
	return Layout{Program: "qconnect", SignatureAsset: SignatureAsset, Namespace: Namespace}, nil
}

func lastLegacy() Version {
	v, err := ParseVersion(LastLegacyVersion)
	if err != nil {
		panic(err)
	}
	return v
}

// ArchiveName is the name of the archive of version for goos and goarch: a .zip for Windows, else a .tar.gz.
func (l Layout) ArchiveName(version, goos, goarch string) string {
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("%s_%s_%s_%s%s", l.Program, version, goos, goarch, extension)
}

// ProgramFile is the program's path inside an archive for goos.
func (l Layout) ProgramFile(goos string) string {
	if goos == "windows" {
		return "bin/" + l.Program + ".exe"
	}
	return "bin/" + l.Program
}

// ManpageFile is the manual page's path inside an archive.
func (l Layout) ManpageFile() string { return "share/man/man1/" + l.Program + ".1" }

// ParseArchiveName reads the layout, version, and platform an archive name stands for. A name is only
// accepted in the exact form ArchiveName gives the layout of its version.
func ParseArchiveName(name string) (layout Layout, version, goos string, err error) {
	parts := strings.SplitN(name, "_", 4)
	if len(parts) != 4 {
		return Layout{}, "", "", fmt.Errorf("%q is not a release archive name", name)
	}
	version, goos = parts[1], parts[2]
	goarch, _, _ := strings.Cut(parts[3], ".")
	layout, err = LayoutOf(version)
	if err != nil || layout.ArchiveName(version, goos, goarch) != name {
		return Layout{}, "", "", fmt.Errorf("%q is not a release archive name", name)
	}
	return layout, version, goos, nil
}

// VerifyRelease checks that signature is a valid signature over checksums for the release archiveName
// belongs to: under Namespace for a version after LastLegacyVersion, under LegacyNamespace up to it.
func VerifyRelease(checksums, signature []byte, archiveName string, keys []ssh.PublicKey) error {
	layout, _, _, err := ParseArchiveName(archiveName)
	if err != nil {
		return err
	}
	return kitrelease.VerifyChecksums(checksums, signature, layout.Namespace, keys)
}

// ChecksumFor returns the lowercase hex SHA-256 that checksums lists for archiveName. The caller
// verifies checksums with VerifyRelease first.
func ChecksumFor(checksums []byte, archiveName string) (string, error) {
	return kitrelease.ChecksumFor(checksums, archiveName)
}

// ProgramSHA256 returns the lowercase hex SHA-256 of the program inside a release archive, at the place
// the layout of archiveName's version names for its platform (see ParseArchiveName).
func ProgramSHA256(archiveName string, archive []byte) (string, error) {
	layout, _, goos, err := ParseArchiveName(archiveName)
	if err != nil {
		return "", err
	}
	return kitrelease.ProgramSHA256(archiveName, archive, layout.ProgramFile(goos))
}
