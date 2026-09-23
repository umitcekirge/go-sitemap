package sitemap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// failAfter yields n entries, then fails like a dropped database connection.
func failAfter(n int) *FuncProvider {
	return &FuncProvider{ProviderName: "pages", StreamFunc: func(ctx context.Context, yield func(Entry) error) error {
		for i := range n {
			if err := yield(Entry{Loc: "https://example.com/new/" + string(rune('a'+i))}); err != nil {
				return err
			}
		}
		return errors.New("connection lost")
	}}
}

func snapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			files[e.Name()+"/"] = nil
			continue
		}
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		files[e.Name()] = b
	}
	return files
}

func TestFailedRunKeepsPublishedFiles(t *testing.T) {
	dir := t.TempDir()
	g, _ := newGen(t, Options{Output: NewFileOutput(dir), MaxURLsPerSitemap: 1})
	ctx := context.Background()
	if _, err := g.Generate(ctx, makeProvider("pages", 3)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	before := snapshot(t, dir)

	if _, err := g.Generate(ctx, failAfter(2)); !errors.Is(err, ErrProvider) {
		t.Fatalf("want ErrProvider, got %v", err)
	}
	after := snapshot(t, dir)
	if len(after) != len(before) {
		t.Fatalf("file set changed: %v -> %v", keys(before), keys(after))
	}
	for name, b := range before {
		if !bytes.Equal(after[name], b) {
			t.Fatalf("%s changed by a failed run", name)
		}
	}
}

func TestFailedRunKeepsMemoryOutput(t *testing.T) {
	g, mo := newGen(t, Options{MaxURLsPerSitemap: 1})
	ctx := context.Background()
	_, _ = g.Generate(ctx, makeProvider("pages", 2))
	old, _ := mo.Get("sitemap-pages-0001.xml")

	if _, err := g.Generate(ctx, failAfter(2)); err == nil {
		t.Fatal("want an error")
	}
	if cur, _ := mo.Get("sitemap-pages-0001.xml"); !bytes.Equal(cur, old) {
		t.Fatal("failed run replaced a published file")
	}
}

func TestSuccessfulRunLeavesNoStagingDir(t *testing.T) {
	dir := t.TempDir()
	g, _ := newGen(t, Options{Output: NewFileOutput(dir)})
	if _, err := g.Generate(context.Background(), makeProvider("pages", 1)); err != nil {
		t.Fatal(err)
	}
	for name := range snapshot(t, dir) {
		if strings.HasPrefix(name, stagingPrefix) {
			t.Fatalf("staging directory left behind: %s", name)
		}
	}
}

func TestStaleStagingDirRemoved(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, stagingPrefix+"old")
	fresh := filepath.Join(dir, stagingPrefix+"fresh")
	for _, d := range []string{stale, fresh} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * staleStagingAge)
	_ = os.Chtimes(stale, old, old)

	g, _ := newGen(t, Options{Output: NewFileOutput(dir)})
	if _, err := g.Generate(context.Background(), makeProvider("pages", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale staging directory should be removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a recent staging directory may belong to a running process and must be kept")
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
