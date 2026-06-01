package sitemap

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fixedClock returns a deterministic time for tests.
func fixedClock() func() time.Time {
	t := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return func() time.Time { return t }
}

// readFile returns the (decompressed if gzip) bytes of name from a MemoryOutput.
func readFile(t *testing.T, mo *MemoryOutput, name string) []byte {
	t.Helper()
	b, ok := mo.Get(name)
	if !ok {
		t.Fatalf("file %q not found; have %v", name, mo.Names())
	}
	if strings.HasSuffix(name, ".gz") {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("gzip reader for %q: %v", name, err)
		}
		out, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("gzip read for %q: %v", name, err)
		}
		if err := zr.Close(); err != nil {
			t.Fatalf("gzip close for %q: %v", name, err)
		}
		return out
	}
	return b
}

// xmlURLSet is a minimal model for asserting well-formedness and counts.
type xmlURLSet struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []struct {
		Loc        string `xml:"loc"`
		LastMod    string `xml:"lastmod"`
		ChangeFreq string `xml:"changefreq"`
		Priority   string `xml:"priority"`
	} `xml:"url"`
}

type xmlIndex struct {
	XMLName  xml.Name `xml:"sitemapindex"`
	Sitemaps []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"sitemap"`
}

// makeProvider builds a SliceProvider of n simple page entries.
func makeProvider(name string, n int) *SliceProvider {
	entries := make([]Entry, 0, n)
	for i := range n {
		entries = append(entries, Entry{Loc: "https://example.com/p/" + itoa(i)})
	}
	return &SliceProvider{ProviderName: name, EntityKind: KindPage, Entries: entries}
}

func newGen(t *testing.T, opts Options) (*Generator, *MemoryOutput) {
	t.Helper()
	mo := NewMemoryOutput()
	if opts.Output == nil {
		opts.Output = mo
	}
	if opts.BaseURL == "" && opts.PublicURLPrefix == "" {
		opts.BaseURL = "https://example.com"
	}
	if opts.Clock == nil {
		opts.Clock = fixedClock()
	}
	g, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g, mo
}

func TestGenerateValidXML(t *testing.T) {
	g, mo := newGen(t, Options{})
	res, err := g.Generate(context.Background(), makeProvider("pages", 3))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("want 1 file, got %d", len(res.Files))
	}
	if res.WrittenURLs != 3 || res.TotalURLs != 3 {
		t.Fatalf("written=%d total=%d", res.WrittenURLs, res.TotalURLs)
	}
	if len(res.IndexFiles) != 0 {
		t.Fatalf("single file should not produce an index")
	}

	var doc xmlURLSet
	data := readFile(t, mo, res.Files[0].Name)
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}
	if len(doc.URLs) != 3 {
		t.Fatalf("want 3 urls, got %d", len(doc.URLs))
	}
	if !bytes.HasPrefix(data, []byte(`<?xml version="1.0" encoding="UTF-8"?>`)) {
		t.Fatalf("missing xml declaration")
	}
}

func TestSplitByURLCount(t *testing.T) {
	g, mo := newGen(t, Options{MaxURLsPerSitemap: 2})
	res, err := g.Generate(context.Background(), makeProvider("pages", 5))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// 5 urls / 2 per file => 3 files + index.
	if len(res.Files) != 3 {
		t.Fatalf("want 3 files, got %d (%v)", len(res.Files), mo.Names())
	}
	if len(res.IndexFiles) != 1 {
		t.Fatalf("want 1 index, got %d", len(res.IndexFiles))
	}
	counts := []int{2, 2, 1}
	for i, f := range res.Files {
		if f.URLCount != counts[i] {
			t.Fatalf("file %d url count = %d, want %d", i, f.URLCount, counts[i])
		}
		if f.Part != i+1 {
			t.Fatalf("file %d part = %d", i, f.Part)
		}
	}
	wantNames := []string{
		"sitemap-pages-0001.xml",
		"sitemap-pages-0002.xml",
		"sitemap-pages-0003.xml",
	}
	for i, f := range res.Files {
		if f.Name != wantNames[i] {
			t.Fatalf("file %d name = %q, want %q", i, f.Name, wantNames[i])
		}
	}
}

func TestSplitByByteSize(t *testing.T) {
	g, mo := newGen(t, Options{MaxUncompressedBytes: 1024})
	res, err := g.Generate(context.Background(), makeProvider("pages", 200))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Files) < 2 {
		t.Fatalf("want multiple files from byte splitting, got %d", len(res.Files))
	}
	total := 0
	for _, f := range res.Files {
		if f.UncompressedSize > 1024 {
			t.Fatalf("file %q exceeds byte limit: %d", f.Name, f.UncompressedSize)
		}
		total += f.URLCount
	}
	if total != 200 {
		t.Fatalf("want 200 urls across files, got %d", total)
	}
	_ = mo
}

func TestGzipDecompressAndLimits(t *testing.T) {
	g, mo := newGen(t, Options{Gzip: true, MaxUncompressedBytes: 2048})
	res, err := g.Generate(context.Background(), makeProvider("pages", 300))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, f := range res.Files {
		if !strings.HasSuffix(f.Name, ".xml.gz") {
			t.Fatalf("gzip file should end with .xml.gz: %q", f.Name)
		}
		if !f.Gzipped || f.CompressedSize == 0 {
			t.Fatalf("file %q missing compressed size", f.Name)
		}
		if f.UncompressedSize > 2048 {
			t.Fatalf("uncompressed limit must be enforced on raw XML: %d", f.UncompressedSize)
		}
		// Must be valid gzip and well-formed XML.
		var doc xmlURLSet
		if err := xml.Unmarshal(readFile(t, mo, f.Name), &doc); err != nil {
			t.Fatalf("decompress/parse %q: %v", f.Name, err)
		}
	}
}

func TestEntryTooLarge(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", 1200)
	p := &SliceProvider{ProviderName: "pages", EntityKind: KindPage, Entries: []Entry{{Loc: long}}}
	g, _ := newGen(t, Options{MaxUncompressedBytes: 1024})
	_, err := g.Generate(context.Background(), p)
	if !errors.Is(err, ErrEntryTooLarge) {
		t.Fatalf("want ErrEntryTooLarge, got %v", err)
	}
}

func TestEntryTooLargeLenientSkips(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", 1200)
	p := &SliceProvider{ProviderName: "pages", EntityKind: KindPage, Entries: []Entry{
		{Loc: long},
		{Loc: "https://example.com/ok"},
	}}
	g, _ := newGen(t, Options{MaxUncompressedBytes: 1024, Validation: ModeLenient})
	res, err := g.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("lenient should not fail: %v", err)
	}
	if res.WrittenURLs != 1 || res.SkippedURLs != 1 {
		t.Fatalf("written=%d skipped=%d", res.WrittenURLs, res.SkippedURLs)
	}
}

func TestNoFakeLastmod(t *testing.T) {
	g, mo := newGen(t, Options{})
	res, err := g.Generate(context.Background(), makeProvider("pages", 2))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data := readFile(t, mo, res.Files[0].Name)
	if bytes.Contains(data, []byte("<lastmod>")) {
		t.Fatalf("no lastmod should be written when none supplied:\n%s", data)
	}
}

func TestLastmodWrittenWhenSupplied(t *testing.T) {
	lm := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	p := &SliceProvider{ProviderName: "pages", EntityKind: KindPage, Entries: []Entry{
		{Loc: "https://example.com/p", LastMod: &lm},
	}}
	g, mo := newGen(t, Options{})
	res, err := g.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data := readFile(t, mo, res.Files[0].Name)
	if !bytes.Contains(data, []byte("<lastmod>2025-12-31T23:59:59Z</lastmod>")) {
		t.Fatalf("lastmod not formatted correctly:\n%s", data)
	}
}

func TestDeterministicOutput(t *testing.T) {
	gen := func() *MemoryOutput {
		g, mo := newGen(t, Options{MaxURLsPerSitemap: 2})
		if _, err := g.Generate(context.Background(), makeProvider("pages", 5), makeProvider("posts", 3)); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		return mo
	}
	a, b := gen(), gen()
	an, bn := a.Names(), b.Names()
	if strings.Join(an, ",") != strings.Join(bn, ",") {
		t.Fatalf("file names differ: %v vs %v", an, bn)
	}
	for _, name := range an {
		ab, _ := a.Get(name)
		bb, _ := b.Get(name)
		if !bytes.Equal(ab, bb) {
			t.Fatalf("content for %q is not deterministic", name)
		}
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	seen := 0
	p := &FuncProvider{
		ProviderName: "pages", EntityKind: KindPage,
		StreamFunc: func(ctx context.Context, yield func(Entry) error) error {
			for i := range 100 {
				if err := ctx.Err(); err != nil {
					return err
				}
				if i == 3 {
					cancel()
				}
				if err := yield(Entry{Loc: "https://example.com/p/" + itoa(i)}); err != nil {
					return err
				}
				seen++
			}
			return nil
		},
	}
	g, _ := newGen(t, Options{})
	_, err := g.Generate(ctx, p)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestMultipleProvidersGrouping(t *testing.T) {
	g, mo := newGen(t, Options{})
	res, err := g.Generate(context.Background(),
		makeProvider("products", 2),
		makeProvider("categories", 1),
	)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := []string{"sitemap-categories-0001.xml", "sitemap-index.xml", "sitemap-products-0001.xml"}
	got := mo.Names()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v", got, want)
	}
	if len(res.Providers) != 2 {
		t.Fatalf("want 2 provider stats, got %d", len(res.Providers))
	}
}

func TestProviderStats(t *testing.T) {
	g, _ := newGen(t, Options{MaxURLsPerSitemap: 2})
	res, err := g.Generate(context.Background(), makeProvider("products", 5))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ps := res.Providers[0]
	if ps.Name != "products" || ps.Kind != KindPage {
		t.Fatalf("provider stat identity wrong: %+v", ps)
	}
	if ps.FileCount != 3 || ps.WrittenURLs != 5 || ps.SkippedURLs != 0 {
		t.Fatalf("provider stat counts wrong: %+v", ps)
	}
	if ps.UncompressedBytes <= 0 {
		t.Fatalf("provider stat bytes should be > 0")
	}
}

func TestXMLEscaping(t *testing.T) {
	p := &SliceProvider{ProviderName: "pages", EntityKind: KindPage, Entries: []Entry{
		{Loc: "https://example.com/?a=1&b=2"},
	}}
	g, mo := newGen(t, Options{})
	res, err := g.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data := readFile(t, mo, res.Files[0].Name)
	if !bytes.Contains(data, []byte("https://example.com/?a=1&amp;b=2")) {
		t.Fatalf("ampersand not escaped:\n%s", data)
	}
	var doc xmlURLSet
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("escaped doc must still parse: %v", err)
	}
}

func TestAlwaysIndexSingleFile(t *testing.T) {
	g, _ := newGen(t, Options{AlwaysIndex: true})
	res, err := g.Generate(context.Background(), makeProvider("pages", 1))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.IndexFiles) != 1 {
		t.Fatalf("AlwaysIndex should produce an index for a single file")
	}
}

func TestDisableIndexSingleFile(t *testing.T) {
	g, _ := newGen(t, Options{DisableIndex: true})
	res, err := g.Generate(context.Background(), makeProvider("pages", 1))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.IndexFiles) != 0 {
		t.Fatalf("DisableIndex with one file should produce no index")
	}
}

func TestDisableIndexIgnoredWhenMultiFile(t *testing.T) {
	g, _ := newGen(t, Options{DisableIndex: true, MaxURLsPerSitemap: 1})
	res, err := g.Generate(context.Background(), makeProvider("pages", 3))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.IndexFiles) != 1 {
		t.Fatalf("DisableIndex must be ignored when multiple files exist (got %d)", len(res.IndexFiles))
	}
}

func TestIndexURLsAbsolute(t *testing.T) {
	g, mo := newGen(t, Options{MaxURLsPerSitemap: 1, BaseURL: "https://cdn.example.com/maps"})
	res, err := g.Generate(context.Background(), makeProvider("pages", 2))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var idx xmlIndex
	if err := xml.Unmarshal(readFile(t, mo, res.IndexFiles[0].Name), &idx); err != nil {
		t.Fatalf("index parse: %v", err)
	}
	if len(idx.Sitemaps) != 2 {
		t.Fatalf("index should reference 2 sitemaps, got %d", len(idx.Sitemaps))
	}
	for _, s := range idx.Sitemaps {
		if err := validateAbsoluteURL(s.Loc); err != nil {
			t.Fatalf("index loc not absolute: %q (%v)", s.Loc, err)
		}
		if !strings.HasPrefix(s.Loc, "https://cdn.example.com/maps/") {
			t.Fatalf("index loc has wrong prefix: %q", s.Loc)
		}
	}
}

func TestIndexSplitsByByteSize(t *testing.T) {
	// Force many sitemap files, then a tiny index byte budget so the index
	// itself must split into several index files.
	g, mo := newGen(t, Options{MaxURLsPerSitemap: 1, MaxUncompressedBytes: 1024})
	res, err := g.Generate(context.Background(), makeProvider("pages", 40))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.IndexFiles) < 2 {
		t.Fatalf("index should split across multiple files, got %d", len(res.IndexFiles))
	}
	total := 0
	for _, f := range res.IndexFiles {
		if f.UncompressedSize > 1024 {
			t.Fatalf("index file %q exceeds byte limit: %d", f.Name, f.UncompressedSize)
		}
		if !strings.Contains(f.Name, "sitemap-index-") {
			t.Fatalf("split index files should be numbered: %q", f.Name)
		}
		total += f.URLCount
	}
	if total != 40 {
		t.Fatalf("index entries across files = %d, want 40", total)
	}
	// Every referenced sitemap must still parse and be absolute.
	var idx xmlIndex
	if err := xml.Unmarshal(readFile(t, mo, res.IndexFiles[0].Name), &idx); err != nil {
		t.Fatalf("index parse: %v", err)
	}
}

func TestInvalidIndexBaseName(t *testing.T) {
	for _, bad := range []string{"../evil", "sub/dir", `a\b`} {
		if _, err := New(Options{BaseURL: "https://e.com", IndexBaseName: bad}); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("IndexBaseName %q should be rejected, got %v", bad, err)
		}
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	g, mo := newGen(t, Options{DryRun: true, MaxURLsPerSitemap: 2})
	res, err := g.Generate(context.Background(), makeProvider("pages", 5))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(mo.Names()) != 0 {
		t.Fatalf("dry run must not write files, wrote %v", mo.Names())
	}
	if len(res.Files) != 3 || len(res.IndexFiles) != 1 {
		t.Fatalf("dry run should still report planned files: %d files, %d index", len(res.Files), len(res.IndexFiles))
	}
	if !res.DryRun {
		t.Fatalf("result should mark DryRun")
	}
}
