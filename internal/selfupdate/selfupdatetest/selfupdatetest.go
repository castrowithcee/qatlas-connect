// Package selfupdatetest serves signed qconnect test releases the way the GitHub release API does, for
// tests only.
package selfupdatetest

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-connect/internal/release"
)

// RepositoryPath is the API path of the repository the server stands for.
const RepositoryPath = "/repos/castrowithcee/qatlas-connect"

// Release is one release of the server: its tag and its assets by name.
type Release struct {
	Tag        string
	Prerelease bool
	Draft      bool
	Assets     map[string][]byte
}

// Server is a local stand-in for the release API of the repository. It answers under RepositoryPath, as
// the current updater asks, and under the root, as the updaters of the releases before the rename were
// built to ask in their tests: the release list in pages of per_page releases (all on page 1 without
// per_page), /releases/latest (the first stable release), each release by tag, and each asset under
// RepositoryPath/releases/assets/<id>.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	releases []Release
	requests map[string]int
	deliver  func(w http.ResponseWriter, r *http.Request, name string, body []byte)
}

// NewServer starts a server with releases that is closed when the test ends.
func NewServer(t testing.TB, releases ...Release) *Server {
	t.Helper()
	s := &Server{releases: releases, requests: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Server.Close)
	return s
}

// RepositoryAPI is the repository API URL for the updater.
func (s *Server) RepositoryAPI() string { return s.URL + RepositoryPath }

// Set replaces the releases.
func (s *Server) Set(releases ...Release) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases = releases
}

// SetDelivery makes deliver send each asset, by its name and body, in place of the server, so that a test
// can answer slowly or stop answering; nil restores the plain answer.
func (s *Server) SetDelivery(deliver func(w http.ResponseWriter, r *http.Request, name string, body []byte)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliver = deliver
}

// Requests returns how often path was asked for.
func (s *Server) Requests(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[path]
}

func (s *Server) describe(i int) map[string]any {
	r := s.releases[i]
	assets := []map[string]any{}
	for j, name := range slices.Sorted(maps.Keys(r.Assets)) {
		assets = append(assets, map[string]any{"name": name, "url": s.URL + RepositoryPath + "/releases/assets/" + assetID(i, j)})
	}
	return map[string]any{"tag_name": r.Tag, "prerelease": r.Prerelease, "draft": r.Draft, "assets": assets}
}

func assetID(release, asset int) string { return strconv.Itoa((release+1)*1000 + asset) }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()
	s.requests[r.URL.Path]++
	path := strings.TrimPrefix(r.URL.Path, RepositoryPath)
	switch {
	case path == "/releases":
		first, last := 0, len(s.releases)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if perPage, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil && perPage > 0 {
			page = max(page, 1)
			first, last = min((page-1)*perPage, len(s.releases)), min(page*perPage, len(s.releases))
		} else if page > 1 {
			first = last
		}
		list := []map[string]any{}
		for i := first; i < last; i++ {
			list = append(list, s.describe(i))
		}
		_ = json.NewEncoder(w).Encode(list)
	case path == "/releases/latest":
		for i, rel := range s.releases {
			if !rel.Draft && !rel.Prerelease {
				_ = json.NewEncoder(w).Encode(s.describe(i))
				return
			}
		}
		http.NotFound(w, r)
	case strings.HasPrefix(path, "/releases/tags/"):
		for i, rel := range s.releases {
			if rel.Tag == strings.TrimPrefix(path, "/releases/tags/") {
				_ = json.NewEncoder(w).Encode(s.describe(i))
				return
			}
		}
		http.NotFound(w, r)
	case strings.HasPrefix(r.URL.Path, RepositoryPath+"/releases/assets/"):
		id := strings.TrimPrefix(path, "/releases/assets/")
		for i, rel := range s.releases {
			for j, name := range slices.Sorted(maps.Keys(rel.Assets)) {
				if assetID(i, j) == id {
					deliver := s.deliver
					// A delivery may take long; other requests must not wait for it.
					s.mu.Unlock()
					locked = false
					if deliver != nil {
						deliver(w, r, name, rel.Assets[name])
					} else {
						_, _ = w.Write(rel.Assets[name])
					}
					return
				}
			}
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

// Archive returns the archive of version for goos and goarch with program as the program and a manual
// page (except on Windows), under the names of the version's layout, plus the extra files.
func Archive(t testing.TB, version, goos, goarch string, program []byte, extra ...releasetest.File) (string, []byte) {
	t.Helper()
	layout, err := release.LayoutOf(version)
	if err != nil {
		t.Fatal(err)
	}
	name := layout.ArchiveName(version, goos, goarch)
	files := []releasetest.File{{Name: layout.ProgramFile(goos), Body: program}}
	if goos != "windows" {
		files = append(files, releasetest.File{Name: layout.ManpageFile(), Body: []byte(".TH " + strings.ToUpper(layout.Program) + " 1\n")})
	}
	return name, releasetest.Archive(t, name, append(files, extra...)...)
}

// Signature names a signature set: the namespace and the release file of the signature.
type Signature struct{ Namespace, Asset string }

var (
	// Current is the signature of a release after release.LastLegacyVersion.
	Current = Signature{release.Namespace, release.SignatureAsset}
	// Former is the signature of a release up to release.LastLegacyVersion, which a later release carries
	// as well for the updaters of those releases.
	Former = Signature{release.LegacyNamespace, release.LegacySignatureAsset}
)

// SignatureOf is the signature set the version is verified with.
func SignatureOf(t testing.TB, version string) Signature {
	t.Helper()
	layout, err := release.LayoutOf(version)
	if err != nil {
		t.Fatal(err)
	}
	return Signature{layout.Namespace, layout.SignatureAsset}
}

// Signed returns a release of tag with the archives, checksums.txt over them, and a signature over it by
// signer in each set; no set leaves it unsigned.
func Signed(t testing.TB, signer ssh.Signer, tag string, archives map[string][]byte, sets ...Signature) Release {
	t.Helper()
	assets := maps.Clone(archives)
	assets["checksums.txt"] = releasetest.Checksums(archives)
	for _, set := range sets {
		assets[set.Asset] = releasetest.Sign(t, signer, set.Namespace, "sha512", assets["checksums.txt"])
	}
	return Release{Tag: tag, Prerelease: strings.Contains(tag, "-"), Assets: assets}
}

// Program returns a release of tag for goos and goarch that carries program, signed by signer with the
// signature set of its version.
func Program(t testing.TB, signer ssh.Signer, tag, goos, goarch string, program []byte) Release {
	t.Helper()
	name, archive := Archive(t, tag, goos, goarch, program)
	return Signed(t, signer, tag, map[string][]byte{name: archive}, SignatureOf(t, tag))
}
