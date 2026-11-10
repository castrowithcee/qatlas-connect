package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
)

const checksumsText = "0000000000000000000000000000000000000000000000000000000000000001  qconnect_v3.0.0_linux_amd64.tar.gz\n"

// The version decides the names and the signature: up to LastLegacyVersion the former ones, after it the
// current ones, a prerelease of the next version included.
func TestLayoutOfFollowsTheVersion(t *testing.T) {
	for version, legacy := range map[string]bool{
		"v0.30.0": true, "v2.1.0": true, LastLegacyVersion: true, "v2.2.0-rc.1": true,
		"v2.2.1-0": false, "v2.2.1": false, "v2.3.0-rc.1": false, "v2.3.0": false, "v3.0.0": false,
	} {
		layout, err := LayoutOf(version)
		if err != nil {
			t.Fatalf("LayoutOf(%s): %v", version, err)
		}
		want := Layout{Program: "qconnect", SignatureAsset: SignatureAsset, Namespace: Namespace}
		if legacy {
			want = Layout{Legacy: true, Program: "qatlas", SignatureAsset: LegacySignatureAsset, Namespace: LegacyNamespace}
		}
		if layout != want {
			t.Errorf("LayoutOf(%s) = %+v, want %+v", version, layout, want)
		}
	}
	if _, err := LayoutOf("2.3.0"); err == nil {
		t.Error("LayoutOf accepted a version without v")
	}
	current, _ := LayoutOf("v2.3.0")
	former, _ := LayoutOf("v2.1.0")
	for _, tt := range []struct{ got, want string }{
		{current.ArchiveName("v2.3.0", "linux", "amd64"), "qconnect_v2.3.0_linux_amd64.tar.gz"},
		{current.ArchiveName("v2.3.0", "windows", "arm64"), "qconnect_v2.3.0_windows_arm64.zip"},
		{former.ArchiveName("v2.1.0", "darwin", "arm64"), "qatlas_v2.1.0_darwin_arm64.tar.gz"},
		{current.ProgramFile("linux"), "bin/qconnect"},
		{current.ProgramFile("windows"), "bin/qconnect.exe"},
		{former.ProgramFile("windows"), "bin/qatlas.exe"},
		{current.ManpageFile(), "share/man/man1/qconnect.1"},
		{former.ManpageFile(), "share/man/man1/qatlas.1"},
	} {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

// An archive name only stands for a release in the exact form of its version's layout: a current version
// under the former name, or the reverse, is no release archive.
func TestParseArchiveName(t *testing.T) {
	for _, name := range []string{"qconnect_v2.3.0_linux_amd64.tar.gz", "qatlas_v2.1.0_windows_amd64.zip"} {
		layout, version, _, err := ParseArchiveName(name)
		if err != nil || layout.ArchiveName(version, map[bool]string{true: "windows", false: "linux"}[strings.HasSuffix(name, ".zip")], "amd64") != name {
			t.Errorf("ParseArchiveName(%s) = %+v, %s, %v", name, layout, version, err)
		}
	}
	for _, name := range []string{
		"qatlas_v2.3.0_linux_amd64.tar.gz", "qconnect_v2.1.0_linux_amd64.tar.gz", "qconnect_v2.3.0_linux_amd64.zip",
		"qconnect_v2.3.0_windows_amd64.tar.gz", "qconnect_2.3.0_linux_amd64.tar.gz", "qconnect_v2.3.0.tar.gz", "x",
	} {
		if _, _, _, err := ParseArchiveName(name); err == nil {
			t.Errorf("ParseArchiveName(%s) = nil error", name)
		}
	}
}

// VerifyRelease takes the namespace from the version in the archive name: a release after the rename is not
// installed on the former signature, however valid, and a release before it is on that one only.
func TestVerifyReleaseBindsTheNamespaceToTheVersion(t *testing.T) {
	signer := releasetest.NewSigner(t)
	keys := []ssh.PublicKey{signer.PublicKey()}
	data := []byte(checksumsText)
	current := releasetest.Sign(t, signer, Namespace, "sha512", data)
	former := releasetest.Sign(t, signer, LegacyNamespace, "sha512", data)
	for _, tt := range []struct {
		archive string
		sig     []byte
		ok      bool
	}{
		{"qconnect_v3.0.0_linux_amd64.tar.gz", current, true},
		{"qconnect_v3.0.0_linux_amd64.tar.gz", former, false},
		{"qatlas_v2.1.0_linux_amd64.tar.gz", former, true},
		{"qatlas_v2.1.0_linux_amd64.tar.gz", current, false},
		{"qatlas_v3.0.0_linux_amd64.tar.gz", former, false},
	} {
		err := VerifyRelease(data, tt.sig, tt.archive, keys)
		if (err == nil) != tt.ok {
			t.Errorf("VerifyRelease(%s) = %v, want ok %v", tt.archive, err, tt.ok)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	body := []byte(sum + "  a.tar.gz\n" + strings.ToUpper(sum) + " *b.zip\nzz  c.zip\n")
	for name, want := range map[string]string{"a.tar.gz": sum, "b.zip": sum} {
		got, err := ChecksumFor(body, name)
		if err != nil || got != want {
			t.Errorf("ChecksumFor(%s) = %q, %v", name, got, err)
		}
	}
	for _, name := range []string{"c.zip", "missing.zip"} {
		if _, err := ChecksumFor(body, name); err == nil {
			t.Errorf("ChecksumFor(%s) = nil error", name)
		}
	}
}

func TestProgramSHA256(t *testing.T) {
	program := []byte("program bytes")
	sum := sha256.Sum256(program)
	want := hex.EncodeToString(sum[:])

	tarball := func(name string) []byte {
		var tarball bytes.Buffer
		gz := gzip.NewWriter(&tarball)
		tw := tar.NewWriter(gz)
		for name, body := range map[string][]byte{"share/man/man1/" + name + ".1": []byte("man"), "bin/" + name: program} {
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			tw.Write(body)
		}
		tw.Close()
		gz.Close()
		return tarball.Bytes()
	}
	zipped := func(name string) []byte {
		var archive bytes.Buffer
		zw := zip.NewWriter(&archive)
		w, _ := zw.Create("bin/" + name + ".exe")
		w.Write(program)
		zw.Close()
		return archive.Bytes()
	}
	for name, archive := range map[string][]byte{
		"qconnect_v3.0.0_linux_amd64.tar.gz": tarball("qconnect"), "qconnect_v3.0.0_windows_amd64.zip": zipped("qconnect"),
		"qatlas_v2.1.0_linux_amd64.tar.gz": tarball("qatlas"), "qatlas_v2.1.0_windows_amd64.zip": zipped("qatlas"),
	} {
		if got, err := ProgramSHA256(name, archive); err != nil || got != want {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}
	for name, archive := range map[string][]byte{
		"qconnect_v3.0.0_windows_amd64.zip":  tarball("qconnect"),
		"qconnect_v3.0.0_linux_amd64.tar.gz": zipped("qconnect"),
		// The program has to be under the name of the archive's version.
		"qconnect_v3.0.0_linux_amd64.tar.gz ": tarball("qconnect"),
		"qatlas_v2.1.0_linux_amd64.tar.gz":    tarball("qconnect"),
	} {
		if _, err := ProgramSHA256(name, archive); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ProgramSHA256("qconnect_v3.0.0_linux_amd64.tar.gz", tarball("qatlas")); err == nil {
		t.Error("a current archive with the program under the former name accepted")
	}
}

func TestTrustedKeysMatchAllowedSigners(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "release-allowed-signers"))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "qatlas-release" {
			t.Fatalf("unexpected allowed signers line %q", line)
		}
		want = append(want, fields[1]+" "+fields[2])
	}
	got := trustedKeyLines
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("compiled keys %q differ from .github/release-allowed-signers %q", got, want)
	}
	var wantLines []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		wantLines = append(wantLines, line)
	}
	for _, installer := range []struct{ file, prefix, suffix string }{
		{"install.sh", "signer_line='", "'"},
		{"install.ps1", "$signerLine = '", "'"},
	} {
		script, err := os.ReadFile(filepath.Join("..", "..", "scripts", installer.file))
		if err != nil {
			t.Fatal(err)
		}
		var embedded []string
		for _, line := range strings.Split(string(script), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), installer.prefix); ok {
				embedded = append(embedded, strings.TrimSuffix(rest, installer.suffix))
			}
		}
		if strings.Join(embedded, "\n") != strings.Join(wantLines, "\n") {
			t.Fatalf("scripts/%s embeds %q, want %q from .github/release-allowed-signers", installer.file, embedded, wantLines)
		}
	}
	if len(TrustedKeys()) < len(got) {
		t.Fatal("TrustedKeys() lost a key")
	}
}

// A signature cut short is refused like any other broken one.
func TestVerifyReleaseRefusesATruncatedSignature(t *testing.T) {
	signer := releasetest.NewSigner(t)
	keys := []ssh.PublicKey{signer.PublicKey()}
	data := []byte(checksumsText)
	good := string(releasetest.Sign(t, signer, Namespace, "sha512", data))
	for name, sig := range map[string]string{
		"magic only": "-----BEGIN SSH SIGNATURE-----\nU1NIU0lH\n-----END SSH SIGNATURE-----\n",
		"truncated":  strings.Join(strings.SplitN(good, "\n", 3)[:2], "\n") + "\n-----END SSH SIGNATURE-----\n",
	} {
		if err := VerifyRelease(data, []byte(sig), "qconnect_v3.0.0_linux_amd64.tar.gz", keys); err == nil {
			t.Errorf("%s: VerifyRelease() accepted the signature", name)
		}
	}
	if err := VerifyRelease(data, []byte(good), "qconnect_v3.0.0_linux_amd64.tar.gz", keys); err != nil {
		t.Fatalf("VerifyRelease() refused the intact signature: %v", err)
	}
}
