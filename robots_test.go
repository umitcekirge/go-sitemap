package sitemap

import (
	"context"
	"strings"
	"testing"
)

func TestRobotsSitemapLines(t *testing.T) {
	got := RobotsSitemapLines("https://example.com/sitemap-index.xml", "https://example.com/news.xml")
	want := "Sitemap: https://example.com/sitemap-index.xml\nSitemap: https://example.com/news.xml\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if RobotsSitemapLines() != "" {
		t.Fatalf("no urls should produce empty string")
	}
}

func TestRobotsLinesFromResult(t *testing.T) {
	g, _ := newGen(t, Options{MaxURLsPerSitemap: 1})
	res, err := g.Generate(context.Background(), makeProvider("pages", 2))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	lines := RobotsSitemapLinesFromResult(res)
	if !strings.Contains(lines, "sitemap-index.xml") {
		t.Fatalf("expected index URL in robots lines, got %q", lines)
	}
	if strings.Count(lines, "Sitemap:") != 1 {
		t.Fatalf("should prefer the single index URL, got %q", lines)
	}
}

func TestFileNamingSanitization(t *testing.T) {
	tests := map[string]string{
		"Products":         "products",
		"Blog Posts":       "blog-posts",
		"../../etc/passwd": "etc-passwd",
		"news!!!feed":      "news-feed",
		"   ":              "sitemap",
		"café":             "caf",
		"a__b--c":          "a-b-c",
	}
	for in, want := range tests {
		if got := sanitizePrefix(in); got != want {
			t.Errorf("sanitizePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCustomFileNamer(t *testing.T) {
	g, mo := newGen(t, Options{FileNamer: prefixNamer{}, MaxURLsPerSitemap: 1})
	if _, err := g.Generate(context.Background(), makeProvider("pages", 2)); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	names := mo.Names()
	found := false
	for _, n := range names {
		if strings.HasPrefix(n, "custom-pages-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom namer not applied: %v", names)
	}
}

type prefixNamer struct{}

func (prefixNamer) SitemapName(prefix string, part int, gzip bool) string {
	return "custom-" + prefix + "-" + itoa(part) + ".xml"
}
func (prefixNamer) IndexName(base string, part int, multiple, gzip bool) string {
	return "custom-" + base + ".xml"
}
