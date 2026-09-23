package sitemap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPruneRemovesStaleFiles(t *testing.T) {
	g, mo := newGen(t, Options{MaxURLsPerSitemap: 1})
	ctx := context.Background()
	if _, err := g.Generate(ctx, makeProvider("pages", 3)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	_ = mo.Write(ctx, "robots.txt", []byte("keep"))

	res, err := g.Generate(ctx, makeProvider("pages", 1))
	if err != nil || res.PruneErr != nil {
		t.Fatalf("second run: %v / %v", err, res.PruneErr)
	}
	want := []string{"sitemap-pages-0002.xml", "sitemap-pages-0003.xml", "sitemap-index.xml"}
	if !slices.Equal(res.PrunedFiles, want) {
		t.Fatalf("pruned = %v, want %v", res.PrunedFiles, want)
	}
	got := mo.Names()
	if !slices.Equal(got, []string{".sitemap-index.manifest", "robots.txt", "sitemap-pages-0001.xml"}) {
		t.Fatalf("remaining files = %v", got)
	}
}

func TestKeepStaleFiles(t *testing.T) {
	g, mo := newGen(t, Options{MaxURLsPerSitemap: 1, KeepStaleFiles: true})
	ctx := context.Background()
	_, _ = g.Generate(ctx, makeProvider("pages", 2))
	res, err := g.Generate(ctx, makeProvider("pages", 1))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.PrunedFiles) != 0 {
		t.Fatalf("nothing should be pruned, got %v", res.PrunedFiles)
	}
	if _, ok := mo.Get("sitemap-pages-0002.xml"); !ok {
		t.Fatalf("stale file removed despite KeepStaleFiles: %v", mo.Names())
	}
}

func TestPruneDryRunOnlyReports(t *testing.T) {
	g, mo := newGen(t, Options{MaxURLsPerSitemap: 1})
	ctx := context.Background()
	_, _ = g.Generate(ctx, makeProvider("pages", 2))
	before := mo.Names()

	dry, _ := newGen(t, Options{MaxURLsPerSitemap: 1, Output: mo, DryRun: true})
	res, err := dry.Generate(ctx, makeProvider("pages", 1))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.PrunedFiles) != 2 {
		t.Fatalf("dry run should report 2 stale files, got %v", res.PrunedFiles)
	}
	if !slices.Equal(mo.Names(), before) {
		t.Fatalf("dry run changed files: %v -> %v", before, mo.Names())
	}
}

// flakyRemove fails every Remove while broken is set.
type flakyRemove struct {
	*MemoryOutput
	broken bool
}

func (f *flakyRemove) Remove(ctx context.Context, name string) error {
	if f.broken {
		return errors.New("permission denied")
	}
	return f.MemoryOutput.Remove(ctx, name)
}

func TestPruneRetriesFailedRemovals(t *testing.T) {
	out := &flakyRemove{MemoryOutput: NewMemoryOutput(), broken: true}
	g, _ := newGen(t, Options{MaxURLsPerSitemap: 1, Output: out})
	ctx := context.Background()
	_, _ = g.Generate(ctx, makeProvider("pages", 2))

	res, err := g.Generate(ctx, makeProvider("pages", 1))
	if err != nil {
		t.Fatalf("a cleanup failure must not fail generation: %v", err)
	}
	if !errors.Is(res.PruneErr, ErrOutput) {
		t.Fatalf("want PruneErr wrapping ErrOutput, got %v", res.PruneErr)
	}

	out.broken = false
	res, _ = g.Generate(ctx, makeProvider("pages", 1))
	if len(res.PrunedFiles) != 2 {
		t.Fatalf("failed removals should be retried, pruned %v", res.PrunedFiles)
	}
}

func TestPruneFileOutput(t *testing.T) {
	dir := t.TempDir()
	g, _ := newGen(t, Options{Output: NewFileOutput(dir), MaxURLsPerSitemap: 1, Gzip: true})
	ctx := context.Background()
	_, _ = g.Generate(ctx, makeProvider("pages", 2))

	g2, _ := newGen(t, Options{Output: NewFileOutput(dir), MaxURLsPerSitemap: 1})
	if _, err := g2.Generate(ctx, makeProvider("pages", 1)); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{".sitemap-index.manifest", "sitemap-pages-0001.xml"}) {
		t.Fatalf("files = %v", names)
	}
	if _, err := os.Stat(filepath.Join(dir, "sitemap-pages-0001.xml.gz")); !os.IsNotExist(err) {
		t.Fatalf("gzip file from previous run should be pruned")
	}
}
