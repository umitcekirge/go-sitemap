package sitemap

import (
	"context"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
)

func ptrFloat(f float64) *float64 { return &f }

func TestDefaultsNormalized(t *testing.T) {
	g, err := New(Options{BaseURL: "https://example.com"})
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
	if o.FileNamer == nil || o.Output == nil || o.Clock == nil {
		t.Errorf("namer/output/clock should default")
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
		{"no base url", Options{}},
		{"bad base url", Options{BaseURL: "not-a-url"}},
		{"ftp scheme", Options{BaseURL: "ftp://example.com"}},
		{"max urls negative", Options{BaseURL: "https://e.com", MaxURLsPerSitemap: -1}},
		{"max urls over limit", Options{BaseURL: "https://e.com", MaxURLsPerSitemap: ProtocolMaxURLs + 1}},
		{"max bytes too small", Options{BaseURL: "https://e.com", MaxUncompressedBytes: 100}},
		{"max bytes over limit", Options{BaseURL: "https://e.com", MaxUncompressedBytes: ProtocolMaxBytes + 1}},
		{"bad validation", Options{BaseURL: "https://e.com", Validation: ValidationMode(99)}},
		{"bad default changefreq", Options{BaseURL: "https://e.com", DefaultChangeFreq: "occasionally"}},
		{"bad default priority", Options{BaseURL: "https://e.com", DefaultPriority: ptrFloat(2)}},
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
	for _, p := range []float64{-0.1, 1.1, 2} {
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
