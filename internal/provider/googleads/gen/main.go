// adsgen writes catalog_gen.go of the googleads package from the public field reference of the Google Ads
// API. It is the only code that fetches the reference; the build and the provider never do. go generate runs
// it in internal/provider/googleads:
//
//	go generate ./internal/provider/googleads/
//
// -cache keeps the fetched pages in a directory so that a repeated run needs no network.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	referenceHost = "https://developers.google.com"
	maxPageBytes  = 32 << 20
)

var versionForm = regexp.MustCompile(`^v[0-9]+$`)

func main() {
	version := flag.String("version", "v26", "API version of the field reference")
	out := flag.String("out", "catalog_gen.go", "file to write")
	cache := flag.String("cache", "", "directory that keeps fetched pages between runs")
	expect := flag.Int("expect-resources", 0, "report a resource count that differs from this one (0: no check)")
	flag.Parse()
	if err := run(*version, *out, *cache, *expect); err != nil {
		fmt.Fprintln(os.Stderr, "adsgen:", err)
		os.Exit(1)
	}
}

type fetcher struct {
	cache string
	last  time.Time
}

// get returns the body of a page of the reference, from the cache when it is there. Requests keep a pause so
// that the generator stays a polite client.
func (f *fetcher) get(ctx context.Context, rel string) ([]byte, error) {
	var file string
	if f.cache != "" {
		file = filepath.Join(f.cache, filepath.Base(rel))
		if b, err := os.ReadFile(file); err == nil {
			return b, nil
		}
	}
	if d := time.Since(f.last); d < 300*time.Millisecond {
		time.Sleep(300*time.Millisecond - d)
	}
	f.last = time.Now()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, referenceHost+rel, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "adsgen")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rel, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", rel, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rel, err)
	}
	if len(b) > maxPageBytes {
		return nil, fmt.Errorf("fetch %s: page is larger than %d bytes", rel, maxPageBytes)
	}
	if file != "" {
		if err := os.MkdirAll(f.cache, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(file, b, 0o644); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func run(version, out, cache string, expect int) error {
	if !versionForm.MatchString(version) {
		return fmt.Errorf("version %q is not of the form v26", version)
	}
	if old, err := os.ReadFile(out); err == nil && !bytes.HasPrefix(old, []byte(header)) {
		return fmt.Errorf("%s exists and is not generated; refusing to overwrite it", out)
	}
	ctx := context.Background()
	f := &fetcher{cache: cache}
	base := "/google-ads/api/fields/" + version + "/"

	raw, err := f.get(ctx, base+"overview")
	if err != nil {
		return err
	}
	names, err := parseOverview(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if expect != 0 && len(names) != expect {
		fmt.Fprintf(os.Stderr, "adsgen: the overview lists %d resources, not the expected %d; the documentation counts\n", len(names), expect)
	}
	raw, err = f.get(ctx, "/static"+base+"fields_index.json")
	if err != nil {
		return err
	}
	index, err := parseIndex(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	src := sources{version: version, resources: names, index: index, pages: map[string]*pageDoc{}}
	sort.Strings(names)
	for _, n := range names {
		raw, err := f.get(ctx, base+n)
		if err != nil {
			return err
		}
		if src.pages[n], err = parsePage(bytes.NewReader(raw)); err != nil {
			return fmt.Errorf("page %s: %w", n, err)
		}
	}
	for _, p := range []struct {
		name string
		to   **pageDoc
	}{{"segments", &src.segments}, {"metrics", &src.metrics}} {
		raw, err := f.get(ctx, base+p.name)
		if err != nil {
			return err
		}
		if *p.to, err = parsePage(bytes.NewReader(raw)); err != nil {
			return fmt.Errorf("page %s: %w", p.name, err)
		}
	}
	c, err := build(src)
	if err != nil {
		return err
	}
	code, err := render(c)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, code, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "adsgen: %d resources, %d fields, %d enums, %d compatibility sets, %d bytes\n",
		len(c.resources), len(c.fields), len(c.enums), len(c.sets), len(code))
	return nil
}
