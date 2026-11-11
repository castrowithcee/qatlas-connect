package selfupdate

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

// notChosen is the refusal of a release that cannot be chosen because a file it needs is missing.
const notChosen = "is not signed or is incomplete and cannot be chosen"

// refusedEverywhere checks that the release tag installs through no path of client, which reads it from
// the running version installed at executable in prefix: Update, Target, Prepare, and UpdateTo, with no
// hook run, and the installation stays as it was. offered says whether the release may still be chosen, its
// files being complete; then download and verification refuse it with cause, and an Update to it fails
// where it is a stable release newer than the running version. Otherwise the list leaves it out, Target refuses it, and
// Update passes it over.
func refusedEverywhere(t *testing.T, client *Client, prefix, executable, tag string, offered bool, cause string) {
	t.Helper()
	forbidReplace(t, client)
	client.AfterReplace = func(context.Context, Release) { t.Errorf("AfterReplace ran for %s", tag) }
	client.ValidateRollback = func(context.Context, *Prepared) error { return nil }
	entries, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, e := range entries {
		listed = listed || e.Version == tag
	}
	if listed != offered {
		t.Errorf("List() = %s, want %s offered %v", tags(entries), tag, offered)
	}
	refused := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "could not be downloaded and verified, nothing was changed") ||
			!strings.Contains(err.Error(), cause) {
			t.Errorf("%s(%s) = %v, want the verification refused with %q", what, tag, err, cause)
		}
	}
	target, err := client.Target(context.Background(), tag)
	switch {
	case !offered:
		var refusal *TargetError
		if !errors.As(err, &refusal) || !strings.Contains(err.Error(), notChosen) {
			t.Errorf("Target(%s) = %+v, %v, want a TargetError with %q", tag, target, err, notChosen)
		}
	case err != nil:
		t.Errorf("Target(%s) = %v, want the release offered", tag, err)
	default:
		_, err = client.Prepare(context.Background(), target)
		refused("Prepare", err)
		if !target.Older {
			result, err := client.UpdateTo(context.Background(), target)
			refused("UpdateTo", err)
			if result.Updated {
				t.Errorf("UpdateTo() installed %s", tag)
			}
		}
	}
	chosen := offered && !target.Older && !target.Prerelease
	result, err := client.Update(context.Background())
	switch {
	case result.Updated:
		t.Errorf("Update() installed %s", tag)
	case chosen && err == nil:
		t.Errorf("Update() = %+v, want the verification of %s refused", result, tag)
	case chosen:
		refused("Update", err)
	case err != nil || result.UpdateAvailable:
		t.Errorf("Update() = %+v, %v, want %s passed over", result, err, tag)
	}
	assertUntouched(t, prefix, executable)
}

// refusedRelease serves rel and checks it with refusedEverywhere from a fresh installation of running, the
// client trusting signer and the extra keys.
func refusedRelease(t *testing.T, rel selfupdatetest.Release, signer ssh.Signer, running string, offered bool, cause string, keys ...ssh.PublicKey) {
	t.Helper()
	server := selfupdatetest.NewServer(t, rel)
	prefix, executable := installedPrefix(t)
	client := clientFor(server, signer, running, executable)
	client.SigningKeys = append(client.SigningKeys, keys...)
	refusedEverywhere(t, client, prefix, executable, rel.Tag, offered, cause)
}

func rsaSigner(t *testing.T) ssh.Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// A release whose signature is invalid installs nothing, whatever the flaw: an unknown key, the wrong
// namespace, an RSA key even when trusted, a signature over other data, or no signature at all in the
// file, for a stable release and a prerelease alike.
func TestUpdateRefusesAnInvalidSignatureWithoutChanges(t *testing.T) {
	signer := releasetest.NewSigner(t)
	rsaKey := rsaSigner(t)
	for _, tag := range []string{"v3.1.0", "v3.1.0-rc.1"} {
		for name, tc := range map[string]struct {
			sign  func(checksums []byte) []byte
			cause string
		}{
			"unknown key": {func(c []byte) []byte {
				return releasetest.Sign(t, releasetest.NewSigner(t), selfupdatetest.Current.Namespace, "sha512", c)
			}, "is not trusted"},
			"wrong namespace": {func(c []byte) []byte { return releasetest.Sign(t, signer, "file", "sha512", c) }, `is for namespace "file"`},
			"rsa key": {func(c []byte) []byte {
				return releasetest.Sign(t, rsaKey, selfupdatetest.Current.Namespace, "sha512", c)
			}, "is not Ed25519"},
			"other data": {func(c []byte) []byte {
				return releasetest.Sign(t, signer, selfupdatetest.Current.Namespace, "sha512", append([]byte("x"), c...))
			}, "does not match checksums"},
			"broken blob": {func([]byte) []byte { return []byte("not a signature") }, "not an armored SSH signature"},
		} {
			t.Run(tag+"/"+name, func(t *testing.T) {
				rel := programRelease(t, signer, tag)
				rel.Assets[selfupdatetest.Current.Asset] = tc.sign(rel.Assets["checksums.txt"])
				refusedRelease(t, rel, signer, "v3.0.0", true, tc.cause, rsaKey.PublicKey())
			})
		}
	}
}

// A release after the rename is installed on its current signature only, whatever it offers besides, from
// the first version after v2.2.0 on: the former signature alone, though validly made by the trusted key,
// installs nothing, and neither does a signature of the wrong namespace or of another key under the current
// name. With its current signature beside the former one, the same release installs.
func TestAReleaseAfterTheRenameNeedsItsCurrentSignature(t *testing.T) {
	signer := releasetest.NewSigner(t)
	stranger := releasetest.NewSigner(t)
	for _, tag := range []string{"v2.2.1", "v3.1.0"} {
		files := programFiles(t, tag, "new-binary", "new-manpage")
		wrongNamespace := published(t, signer, tag, files, selfupdatetest.Former)
		wrongNamespace.Assets[selfupdatetest.Current.Asset] = wrongNamespace.Assets[selfupdatetest.Former.Asset]
		for name, tc := range map[string]struct {
			release selfupdatetest.Release
			offered bool
			cause   string
		}{
			"former signature only": {published(t, signer, tag, files, selfupdatetest.Former), false, ""},
			"no signature": {
				published(t, signer, tag, files, selfupdatetest.Signature{Namespace: "unused", Asset: "unrelated.sig"}), false, "",
			},
			"former namespace under current name": {wrongNamespace, true, `is for namespace "` + selfupdatetest.Former.Namespace + `"`},
			"another namespace under current name": {
				published(t, signer, tag, files, selfupdatetest.Signature{Namespace: "file", Asset: selfupdatetest.Current.Asset}), true,
				`is for namespace "file"`,
			},
			"untrusted key": {published(t, stranger, tag, files, selfupdatetest.Current), true, "is not trusted"},
		} {
			t.Run(tag+"/"+name, func(t *testing.T) {
				refusedRelease(t, tc.release, signer, "v2.2.0", tc.offered, tc.cause)
			})
		}
		t.Run(tag+"/both signatures", func(t *testing.T) {
			server := selfupdatetest.NewServer(t, published(t, signer, tag, files, selfupdatetest.Current, selfupdatetest.Former))
			_, executable := installedPrefix(t)
			if result, err := clientFor(server, signer, "v2.2.0", executable).Update(context.Background()); err != nil || !result.Updated || !result.Signed {
				t.Fatalf("Update() = %+v, %v", result, err)
			}
			assertRegular(t, executable, "new-binary")
		})
	}
}

// A release up to v2.2.0 is verified with the former signature only: a rollback to it with the current
// signature, or with the former file signed in the current or another namespace, installs nothing.
func TestAReleaseBeforeTheRenameNeedsTheFormerSignature(t *testing.T) {
	signer := releasetest.NewSigner(t)
	files := programFiles(t, "v2.2.0", "new-binary", "new-manpage")
	for name, tc := range map[string]struct {
		release selfupdatetest.Release
		offered bool
		cause   string
	}{
		"current signature only": {published(t, signer, "v2.2.0", files, selfupdatetest.Current), false, ""},
		"current namespace under former name": {
			published(t, signer, "v2.2.0", files, selfupdatetest.Signature{Namespace: selfupdatetest.Current.Namespace, Asset: selfupdatetest.Former.Asset}),
			true, `is for namespace "` + selfupdatetest.Current.Namespace + `"`,
		},
		"file namespace under former name": {
			published(t, signer, "v2.2.0", files, selfupdatetest.Signature{Namespace: "file", Asset: selfupdatetest.Former.Asset}),
			true, `is for namespace "file"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			refusedRelease(t, tc.release, signer, "v3.0.0", tc.offered, tc.cause)
		})
	}
}

// renamed is rel published as the release tag: its files, archive names included, unchanged.
func renamed(rel selfupdatetest.Release, tag string) selfupdatetest.Release {
	rel.Tag, rel.Assets = tag, maps.Clone(rel.Assets)
	return rel
}

// withAsset is rel with the asset from renamed to to.
func withAsset(rel selfupdatetest.Release, from, to string) selfupdatetest.Release {
	rel.Assets = maps.Clone(rel.Assets)
	rel.Assets[to] = rel.Assets[from]
	delete(rel.Assets, from)
	return rel
}

// The signed checksums.txt binds each archive by a name that carries its version, so a validly signed
// release does not pass for another version: its files under another tag, its archive renamed to the
// other version's name, an archive with the program under the other layout's name, and the former files a
// later release carries for the updaters before the rename, offered as a release before it, all install
// nothing.
func TestAnArchiveOfAnotherVersionIsRefused(t *testing.T) {
	signer := releasetest.NewSigner(t)
	older := programRelease(t, signer, "v3.0.0")
	olderArchive, newerArchive := archiveName(t, "v3.0.0"), archiveName(t, "v3.1.0")
	laterFormer := strings.Replace(archiveName(t, "v2.2.0"), "v2.2.0", "v3.1.0", 1)
	_, laterFormerBody := archiveWith(t, "v2.2.0", programFiles(t, "v2.2.0", "new-binary", "new-manpage"))
	later := selfupdatetest.Signed(t, signer, "v3.1.0", map[string][]byte{laterFormer: laterFormerBody}, selfupdatetest.Current, selfupdatetest.Former)
	for name, tc := range map[string]struct {
		release selfupdatetest.Release
		offered bool
		cause   string
	}{
		"another version's files":           {renamed(older, "v3.1.0"), false, ""},
		"another version's archive renamed": {withAsset(renamed(older, "v3.1.0"), olderArchive, newerArchive), true, "checksum for " + newerArchive + " is missing"},
		"program under the former name": {
			published(t, signer, "v3.1.0", programFiles(t, "v2.2.0", "new-binary", "new-manpage")), true, "archive does not contain bin/qconnect",
		},
		"former files of a later release": {
			withAsset(renamed(later, "v2.2.0"), laterFormer, archiveName(t, "v2.2.0")), true,
			"checksum for " + archiveName(t, "v2.2.0") + " is missing",
		},
	} {
		t.Run(name, func(t *testing.T) {
			refusedRelease(t, tc.release, signer, "v3.0.1", tc.offered, tc.cause)
		})
	}
}
