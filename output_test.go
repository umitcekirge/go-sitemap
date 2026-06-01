package sitemap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileOutput(t *testing.T) {
	dir := t.TempDir()
	g, err := New(Options{BaseURL: "https://example.com", Output: NewFileOutput(dir), MaxURLsPerSitemap: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := g.Generate(context.Background(), makeProvider("pages", 2))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, f := range append(res.Files, res.IndexFiles...) {
		p := filepath.Join(dir, f.Name)
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("file %q not written: %v", f.Name, err)
		}
		if info.Size() == 0 {
			t.Fatalf("file %q is empty", f.Name)
		}
	}
	// No stray temp files should remain.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" || len(e.Name()) > 0 && e.Name()[0] == '.' {
			t.Fatalf("stray temp file left behind: %q", e.Name())
		}
	}
}

func TestMemoryOutput(t *testing.T) {
	mo := NewMemoryOutput()
	if err := mo.Write(context.Background(), "a.xml", []byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	b, ok := mo.Get("a.xml")
	if !ok || string(b) != "data" {
		t.Fatalf("Get returned %q ok=%v", b, ok)
	}
	// Write must copy: mutating the source must not change stored bytes.
	src := []byte("xyz")
	_ = mo.Write(context.Background(), "b.xml", src)
	src[0] = 'Z'
	b2, _ := mo.Get("b.xml")
	if string(b2) != "xyz" {
		t.Fatalf("MemoryOutput must copy data, got %q", b2)
	}
}

func TestOutputRejectsUnsafeNames(t *testing.T) {
	mo := NewMemoryOutput()
	for _, name := range []string{"../escape.xml", "sub/dir.xml", "/abs.xml", "..", ""} {
		if err := mo.Write(context.Background(), name, []byte("x")); !errors.Is(err, ErrOutput) {
			t.Fatalf("name %q should be rejected, got %v", name, err)
		}
	}
}

func TestFileOutputConfinement(t *testing.T) {
	dir := t.TempDir()
	fo := NewFileOutput(dir)
	if err := fo.Write(context.Background(), "../escape.xml", []byte("x")); !errors.Is(err, ErrOutput) {
		t.Fatalf("path traversal must be rejected, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.xml")); !os.IsNotExist(err) {
		t.Fatalf("file escaped output directory")
	}
}
