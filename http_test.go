package sitemap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestFileServer(t *testing.T) {
	g, mo := newGen(t, Options{Gzip: true, MaxURLsPerSitemap: 1})
	res, err := g.Generate(context.Background(), makeProvider("pages", 2))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	fs := &FileServer{Store: mo, ModTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	srv := httptest.NewServer(fs)
	defer srv.Close()

	name := res.Files[0].Name
	// Set Accept-Encoding explicitly so the client does not transparently
	// decompress and strip Content-Encoding.
	req0, _ := http.NewRequest(http.MethodGet, srv.URL+"/"+name, nil)
	req0.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req0)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", ce)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/xml; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("missing ETag")
	}

	// Conditional GET returns 304.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/"+name, nil)
	req.Header.Set("If-None-Match", etag)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional GET status = %d, want 304", resp2.StatusCode)
	}

	// Unknown file -> 404.
	resp3, err := http.Get(srv.URL + "/does-not-exist.xml")
	if err != nil {
		t.Fatalf("GET missing: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file status = %d, want 404", resp3.StatusCode)
	}
}

func TestFileServerRejectsUnsafePath(t *testing.T) {
	fs := &FileServer{Store: NewMemoryOutput()}
	srv := httptest.NewServer(fs)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/%2e%2e/secret")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unsafe path status = %d, want 404", resp.StatusCode)
	}
}

func TestStreamingLargeDataset(t *testing.T) {
	const n = 250000
	p := &FuncProvider{
		ProviderName: "big", EntityKind: KindProduct,
		StreamFunc: func(ctx context.Context, yield func(Entry) error) error {
			for i := range n {
				if err := yield(Entry{Loc: "https://example.com/p/" + strconv.Itoa(i)}); err != nil {
					return err
				}
			}
			return nil
		},
	}
	g, _ := newGen(t, Options{}) // default 50000 per file
	res, err := g.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Files) != 5 {
		t.Fatalf("250000/50000 should give 5 files, got %d", len(res.Files))
	}
	if res.WrittenURLs != n {
		t.Fatalf("written = %d, want %d", res.WrittenURLs, n)
	}
	if len(res.IndexFiles) != 1 {
		t.Fatalf("want 1 index, got %d", len(res.IndexFiles))
	}
}

func TestNonStandardLimitsGeneration(t *testing.T) {
	g, _ := newGen(t, Options{MaxURLsPerSitemap: 60000, AllowNonStandardLimits: true})
	res, err := g.Generate(context.Background(), makeProvider("pages", 55000))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("55000 urls should fit one non-standard file, got %d", len(res.Files))
	}
}

func TestFileServerUnderPrefix(t *testing.T) {
	g, mo := newGen(t, Options{})
	res, _ := g.Generate(context.Background(), makeProvider("pages", 1))
	_ = mo.Write(context.Background(), ".sitemap-index.manifest", []byte("x"))

	mux := http.NewServeMux()
	mux.Handle("/sitemaps/", &FileServer{Store: mo})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for path, want := range map[string]int{
		"/sitemaps/" + res.Files[0].Name:    http.StatusOK,
		"/sitemaps/.sitemap-index.manifest": http.StatusNotFound,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("GET %s = %d, want %d", path, resp.StatusCode, want)
		}
	}
}
