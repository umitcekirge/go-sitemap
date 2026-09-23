package sitemap

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"math"
	"strings"
	"testing"
)

func ptrFloat(f float64) *float64 { return &f }

func TestDefaultsNormalized(t *testing.T) {
	g, err := New(Options{BaseURL: "https://example.com", Output: NewMemoryOutput()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	o := g.Options()
	if o.MaxURLsPerSitemap != ProtocolMaxURLs {
		t.Errorf("MaxURLsPerSitemap default = %d", o.MaxURLsPerSitemap)
	}
	if o.MaxUncompressedBytes != ProtocolMaxBytes {
		t.Errorf("MaxUncompressedBytes default = %d", o.MaxUncompressedBytes)
	}
	if o.Validation != ModeStrict {
		t.Errorf("Validation default = %v", o.Validation)
	}
	if o.LastModFormat == "" {
		t.Errorf("LastModFormat should default")
	}
	if o.IndexBaseName != "sitemap-index" {
		t.Errorf("IndexBaseName default = %q", o.IndexBaseName)
	}
	if o.FileNamer == nil || o.Clock == nil {
		t.Errorf("namer/clock should default")
	}
	if o.PublicURLPrefix != "https://example.com/" {
		t.Errorf("PublicURLPrefix should fall back to BaseURL with trailing slash, got %q", o.PublicURLPrefix)
	}
}

func TestInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"no base url", Options{Output: NewMemoryOutput()}},
		{"no output", Options{BaseURL: "https://e.com"}},
		{"bad base url", Options{BaseURL: "not-a-url"}},
		{"ftp scheme", Options{BaseURL: "ftp://example.com"}},
		{"max urls negative", Options{BaseURL: "https://e.com", MaxURLsPerSitemap: -1}},
		{"max urls over limit", Options{BaseURL: "https://e.com", MaxURLsPerSitemap: ProtocolMaxURLs + 1}},
		{"max bytes too small", Options{BaseURL: "https://e.com", MaxUncompressedBytes: 100}},
		{"max bytes over limit", Options{BaseURL: "https://e.com", MaxUncompressedBytes: ProtocolMaxBytes + 1}},
		{"bad validation", Options{BaseURL: "https://e.com", Validation: ValidationMode(99)}},
		{"bad default changefreq", Options{BaseURL: "https://e.com", DefaultChangeFreq: "occasionally"}},
		{"bad default priority", Options{BaseURL: "https://e.com", DefaultPriority: ptrFloat(2)}},
		{"NaN default priority", Options{BaseURL: "https://e.com", DefaultPriority: ptrFloat(math.NaN())}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.opts); !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("want ErrInvalidOptions, got %v", err)
			}
		})
	}
}

func TestNonStandardLimitsAllowed(t *testing.T) {
	_, err := New(Options{
		BaseURL:                "https://e.com",
		Output:                 NewMemoryOutput(),
		MaxURLsPerSitemap:      ProtocolMaxURLs + 10,
		MaxUncompressedBytes:   ProtocolMaxBytes + 10,
		AllowNonStandardLimits: true,
	})
	if err != nil {
		t.Fatalf("non-standard limits should be allowed with the flag: %v", err)
	}
}

func TestOptInDefaults(t *testing.T) {
	g, mo := newGen(t, Options{
		DefaultChangeFreq: Weekly,
		DefaultPriority:   ptrFloat(0.8),
	})
	res, err := g.Generate(context.Background(), makeProvider("pages", 1))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var doc xmlURLSet
	if err := xml.Unmarshal(readFile(t, mo, res.Files[0].Name), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.URLs[0].ChangeFreq != "weekly" || doc.URLs[0].Priority != "0.8" {
		t.Fatalf("opt-in defaults not applied: %+v", doc.URLs[0])
	}
}

func TestValidationModes(t *testing.T) {
	entries := []Entry{
		{Loc: "https://example.com/ok"},
		{Loc: "not-absolute"},
		{Loc: "https://example.com/ok2", Priority: ptrFloat(5)},
	}
	p := &SliceProvider{ProviderName: "pages", EntityKind: KindPage, Entries: entries}

	// Strict: first invalid entry fails.
	gs, _ := newGen(t, Options{Validation: ModeStrict})
	if _, err := gs.Generate(context.Background(), p); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("strict want ErrInvalidURL, got %v", err)
	}

	// Lenient: skip invalid, keep valid.
	gl, _ := newGen(t, Options{Validation: ModeLenient})
	res, err := gl.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("lenient: %v", err)
	}
	if res.WrittenURLs != 1 || res.SkippedURLs != 2 {
		t.Fatalf("lenient written=%d skipped=%d", res.WrittenURLs, res.SkippedURLs)
	}
	if len(res.ValidationErrors) != 2 {
		t.Fatalf("want 2 validation errors, got %d", len(res.ValidationErrors))
	}
}

func TestPriorityValidation(t *testing.T) {
	for _, p := range []float64{-0.1, 1.1, 2, math.NaN()} {
		e := Entry{Loc: "https://e.com/x", Priority: ptrFloat(p)}
		if err := validateEntry(e, ""); !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("priority %v should be invalid, got %v", p, err)
		}
	}
	for _, p := range []float64{0, 0.5, 1} {
		e := Entry{Loc: "https://e.com/x", Priority: ptrFloat(p)}
		if err := validateEntry(e, ""); err != nil {
			t.Fatalf("priority %v should be valid, got %v", p, err)
		}
	}
}

func TestChangefreqValidation(t *testing.T) {
	if err := validateEntry(Entry{Loc: "https://e.com/x", ChangeFreq: "sometimes"}, ""); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("invalid changefreq should fail")
	}
	if err := validateEntry(Entry{Loc: "https://e.com/x", ChangeFreq: Daily}, ""); err != nil {
		t.Fatalf("valid changefreq should pass: %v", err)
	}
}

func TestInvalidURLValidation(t *testing.T) {
	for _, loc := range []string{"", "ftp://e.com", "/relative", "http://", "example.com"} {
		if err := validateEntry(Entry{Loc: loc}, ""); !errors.Is(err, ErrInvalidURL) && !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("loc %q should be invalid, got %v", loc, err)
		}
	}
}

func TestSameHostOnly(t *testing.T) {
	g, _ := newGen(t, Options{BaseURL: "https://example.com", SameHostOnly: true, Validation: ModeLenient})
	p := &SliceProvider{ProviderName: "pages", EntityKind: KindPage, Entries: []Entry{
		{Loc: "https://example.com/ok"},
		{Loc: "https://other.com/no"},
	}}
	res, err := g.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.WrittenURLs != 1 || res.SkippedURLs != 1 {
		t.Fatalf("same-host: written=%d skipped=%d", res.WrittenURLs, res.SkippedURLs)
	}
}

func TestTransformSkip(t *testing.T) {
	skipStaging := func(ctx context.Context, e *Entry) (bool, error) {
		return !strings.Contains(e.Loc, "/staging/"), nil
	}
	g, _ := newGen(t, Options{Transforms: []Transform{skipStaging}})
	p := &SliceProvider{ProviderName: "pages", EntityKind: KindPage, Entries: []Entry{
		{Loc: "https://example.com/live"},
		{Loc: "https://example.com/staging/x"},
	}}
	res, err := g.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.WrittenURLs != 1 || res.SkippedURLs != 1 {
		t.Fatalf("transform skip: written=%d skipped=%d", res.WrittenURLs, res.SkippedURLs)
	}
}

func TestPriorityKeepsPrecision(t *testing.T) {
	data := genOne(t, Entry{Loc: "https://example.com/p", Priority: ptrFloat(0.85)})
	if !bytes.Contains(data, []byte("<priority>0.85</priority>")) {
		t.Fatalf("priority rounded:\n%s", data)
	}
}

func TestInvalidUTF8TextRejected(t *testing.T) {
	e := Entry{Loc: "https://e.com/x", Images: []Image{{Loc: "https://e.com/i.jpg", Title: "caf\xe9"}}}
	err := validateEntry(e, "")
	var se *Error
	if !errors.Is(err, ErrInvalidEntry) || !errors.As(err, &se) || se.Field != "Image.Title" {
		t.Fatalf("want ErrInvalidEntry on Image.Title, got %v", err)
	}
}

// failOnce fails only the first write, so a later flush cannot mask a
// swallowed error.
type failOnce struct{ failed bool }

func (f *failOnce) Write(context.Context, string, []byte) error {
	if f.failed {
		return nil
	}
	f.failed = true
	return errors.New("disk full")
}

func TestSwallowedYieldErrorStillFails(t *testing.T) {
	swallow := func(entries ...Entry) *FuncProvider {
		return &FuncProvider{ProviderName: "pages", StreamFunc: func(_ context.Context, yield func(Entry) error) error {
			for _, e := range entries {
				_ = yield(e)
			}
			return nil
		}}
	}

	g, _ := newGen(t, Options{Output: &failOnce{}, MaxURLsPerSitemap: 1})
	ok := Entry{Loc: "https://example.com/a"}
	if _, err := g.Generate(context.Background(), swallow(ok, ok, ok)); !errors.Is(err, ErrOutput) {
		t.Fatalf("want ErrOutput, got %v", err)
	}

	gs, _ := newGen(t, Options{})
	if _, err := gs.Generate(context.Background(), swallow(Entry{Loc: "bad"}, ok)); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("want ErrInvalidURL, got %v", err)
	}
}

type unsafeNamer struct{ DefaultFileNamer }

func (unsafeNamer) SitemapName(string, int, bool) string { return "../evil.xml" }

func TestUnsafeCustomNameRejected(t *testing.T) {
	var got []string
	rec := &recordingOutput{names: &got}
	g, _ := newGen(t, Options{Output: rec, FileNamer: unsafeNamer{}})
	if _, err := g.Generate(context.Background(), makeProvider("pages", 1)); !errors.Is(err, ErrOutput) {
		t.Fatalf("want ErrOutput, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unsafe name reached the output: %v", got)
	}
}

type recordingOutput struct{ names *[]string }

func (r *recordingOutput) Write(_ context.Context, name string, _ []byte) error {
	*r.names = append(*r.names, name)
	return nil
}

func TestOversizedEntryDoesNotSplitEarly(t *testing.T) {
	big := Entry{Loc: "https://example.com/big?" + strings.Repeat("x", 2000)}
	p := &SliceProvider{ProviderName: "pages", Entries: []Entry{
		{Loc: "https://example.com/a"}, big, {Loc: "https://example.com/b"},
	}}
	g, _ := newGen(t, Options{MaxUncompressedBytes: minUncompressedBytes, Validation: ModeLenient})
	res, err := g.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Files) != 1 || res.Files[0].URLCount != 2 {
		t.Fatalf("want one file with 2 urls, got %+v", res.Files)
	}
}
