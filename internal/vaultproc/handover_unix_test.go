//go:build linux || darwin

package vaultproc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/release"
	"github.com/castrowithcee/qatlas-connect/internal/vault"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
)

// writeRelease writes a release whose archive holds program as bin/qconnect, signed by signer.
func writeRelease(t *testing.T, program []byte, signer ssh.Signer) ReleaseFiles {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "bin/" + programName, Mode: 0o755, Size: int64(len(program)),
		Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write(program)
	_ = tw.Close()
	_ = gz.Close()
	const name = "qconnect_v9.9.9_linux_amd64.tar.gz"
	sum := sha256.Sum256(archive.Bytes())
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	dir := t.TempDir()
	files := ReleaseFiles{Checksums: filepath.Join(dir, "checksums.txt"),
		Signature: filepath.Join(dir, release.SignatureAsset), Archive: filepath.Join(dir, name), ArchiveName: name}
	for path, content := range map[string][]byte{files.Checksums: checksums, files.Archive: archive.Bytes(),
		files.Signature: releasetest.Sign(t, signer, release.Namespace, "sha256", checksums)} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// A handover the process refuses over a real request, a release that does not verify at the prepare or a
// program that was not replaced at the commit, locks the process, is told with Connect's reason, and is
// logged as before the kit, as one signed entry.
func TestRefusedHandoversAreLoggedAsBefore(t *testing.T) {
	trusted := releasetest.NewSigner(t)
	for name, tc := range map[string]struct {
		signer ssh.Signer
		commit bool
		reason string
		logged string
	}{
		"a release that does not verify": {releasetest.NewSigner(t), false, "it could not verify the release",
			"release-unverified"},
		"a program that was not replaced": {trusted, true, "its program was not replaced", "program-unchanged"},
	} {
		t.Run(name, func(t *testing.T) {
			tv := newTestVault(t)
			server, client := tv.serve(t, ServerOptions{KeepLog: true, RetentionDays: 90,
				TrustedKeys: []ssh.PublicKey{trusted.PublicKey()}, ConfigPath: filepath.Join(tv.dir, "config.yaml")})
			ctx := withTimeout(t)
			behaviour, handover, err := client.PrepareHandover(ctx, writeRelease(t, []byte("synthetic program"), tc.signer))
			if tc.commit {
				if err != nil || behaviour != vault.UpdateHandover || handover == nil {
					t.Fatalf("PrepareHandover() = %q, %v, %v, want a handover to commit", behaviour, handover, err)
				}
				_, err = handover.Commit(ctx)
			}
			if !errors.Is(err, ErrHandover) || !strings.Contains(err.Error(), ErrHandover.Error()+": "+tc.reason) {
				t.Fatalf("the handover = %v, want ErrHandover: %s", err, tc.reason)
			}
			select {
			case <-server.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("the process still serves after the refused handover")
			}
			if got, want := handoverLog(t, tv.vault.Dir()), []string{tc.logged}; !slices.Equal(got, want) {
				t.Fatalf("the handover entries = %q, want %q", got, want)
			}
		})
	}
}
