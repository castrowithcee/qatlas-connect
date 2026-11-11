package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"

	"github.com/castrowithcee/qatlas-connect/internal/selfupdate/selfupdatetest"
)

// The stall timeout of a download is the kit's and has no seam, so these tests wait it out against a real
// server: about thirty seconds each, in parallel.
const stallTimeout = 30 * time.Second

// stallClient is an updater from v3.0.0 of a fresh installation that reads a signed v3.1.0 whose archive
// deliver sends, with the production HTTP client.
func stallClient(t *testing.T, deliver func(w http.ResponseWriter, r *http.Request, body []byte)) (client *Client, prefix, executable string, archive []byte) {
	t.Helper()
	signer := releasetest.NewSigner(t)
	rel := programRelease(t, signer, "v3.1.0")
	name := archiveName(t, "v3.1.0")
	server := selfupdatetest.NewServer(t, rel)
	server.SetDelivery(func(w http.ResponseWriter, r *http.Request, asset string, body []byte) {
		if asset != name {
			_, _ = w.Write(body)
			return
		}
		deliver(w, r, body)
	})
	prefix, executable = installedPrefix(t)
	client = clientFor(server, signer, "v3.0.0", executable)
	client.HTTPClient = nil
	return client, prefix, executable, rel.Assets[name]
}

// assertStalled checks that update ended with the stall named, after the stall timeout and well before a
// download would end otherwise, with nothing installed.
func assertStalled(t *testing.T, client *Client, prefix, executable string) {
	t.Helper()
	forbidReplace(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 4*stallTimeout)
	defer cancel()
	start := time.Now()
	result, err := client.Update(ctx)
	if err == nil || result.Updated || !strings.Contains(err.Error(), "no data received") ||
		!strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("Update() = %+v, %v, want the stall reported", result, err)
	}
	if ctx.Err() != nil {
		t.Fatalf("Update() ended only with the test's deadline: %v", err)
	}
	if elapsed := time.Since(start); elapsed < stallTimeout {
		t.Errorf("Update() ended after %s, before the stall timeout", elapsed)
	}
	assertUntouched(t, prefix, executable)
}

// Waiting for an answer that never starts counts as a stall.
func TestUpdateEndsWhenTheArchiveNeverStarts(t *testing.T) {
	t.Parallel()
	client, prefix, executable, _ := stallClient(t, func(_ http.ResponseWriter, r *http.Request, _ []byte) {
		<-r.Context().Done()
	})
	assertStalled(t, client, prefix, executable)
}

// A download that stops sending in the middle of the archive ends with the stall named.
func TestUpdateEndsAStalledDownload(t *testing.T) {
	t.Parallel()
	client, prefix, executable, _ := stallClient(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:len(body)/2])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	assertStalled(t, client, prefix, executable)
}

// A download that keeps moving is never cut off, however long it takes: the archive arrives in parts with
// pauses shorter than the stall timeout, in all longer than it, and progress rises to the announced size.
func TestUpdateOutlastsTheStallTimeoutWhileDataFlows(t *testing.T) {
	t.Parallel()
	const parts = 6
	pause := stallTimeout / 4
	client, _, executable, archive := stallClient(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		for i := range parts {
			if i > 0 {
				select {
				case <-time.After(pause):
				case <-r.Context().Done():
					return
				}
			}
			_, _ = w.Write(body[i*len(body)/parts : (i+1)*len(body)/parts])
			w.(http.Flusher).Flush()
		}
	})
	var mu sync.Mutex
	var dones, totals []int64
	client.Progress = func(done, total int64) {
		mu.Lock()
		defer mu.Unlock()
		dones, totals = append(dones, done), append(totals, total)
	}
	start := time.Now()
	result, err := client.Update(context.Background())
	if err != nil || !result.Updated {
		t.Fatalf("Update() = %+v, %v, want the whole download", result, err)
	}
	if elapsed := time.Since(start); elapsed <= stallTimeout {
		t.Fatalf("the download took %s, not longer than the stall timeout", elapsed)
	}
	assertRegular(t, executable, "new-binary")
	mu.Lock()
	defer mu.Unlock()
	if len(dones) < parts || dones[len(dones)-1] != int64(len(archive)) {
		t.Fatalf("progress reported %v, want at least %d reports ending at %d", dones, parts, len(archive))
	}
	for i := range dones {
		if totals[i] != int64(len(archive)) || (i > 0 && dones[i] <= dones[i-1]) {
			t.Fatalf("progress %v of %v, want rising counts of the announced total", dones, totals)
		}
	}
}

// A release list that never starts to answer ends the check too, with the production HTTP client.
func TestCheckEndsWhenTheAnswerNeverStarts(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := &Client{BaseURL: server.URL + selfupdatetest.RepositoryPath, Version: "v3.0.0"}
	ctx, cancel := context.WithTimeout(context.Background(), 4*stallTimeout)
	defer cancel()
	if result, err := client.Check(ctx); err == nil {
		t.Fatalf("Check() = %+v, want the silent server reported", result)
	}
	if ctx.Err() != nil {
		t.Fatal("Check() ended only with the test's deadline")
	}
}

// A download that keeps trickling below the stall timeout still ends at the overall limit, with nothing
// installed.
func TestUpdateEndsADownloadThatKeepsTrickling(t *testing.T) {
	limit := prepareTimeout
	prepareTimeout = 3 * time.Second
	t.Cleanup(func() { prepareTimeout = limit })
	client, prefix, executable, _ := stallClient(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		for i := range body {
			if _, err := w.Write(body[i : i+1]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
	forbidReplace(t, client)
	start := time.Now()
	result, err := client.Update(context.Background())
	if err == nil || result.Updated || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("Update() = %+v, %v, want the download ended", result, err)
	}
	if elapsed := time.Since(start); elapsed > stallTimeout {
		t.Errorf("Update() ended after %s, not at the overall limit", elapsed)
	}
	assertUntouched(t, prefix, executable)
}
