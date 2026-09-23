package sitemap

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Output is a pluggable storage backend for generated files. Implementations
// receive the final bytes of each file (already gzip-compressed when gzip is
// enabled) and a sanitised, base file name (never a path). Implementations must
// not interpret the name as a path or allow writes outside their root.
//
// IMPORTANT: the data slice is owned by the generator and is reused after Write
// returns. Implementations must not retain it; if storage is asynchronous, copy
// the bytes first (as MemoryOutput does). Writing synchronously within Write
// (as FileOutput does) is always safe.
type Output interface {
	// Write stores data under name. It must honour ctx cancellation, must
	// finish using data before returning, and must treat name as a base file
	// name without directory separators.
	Write(ctx context.Context, name string, data []byte) error
}

// Pruner is an optional Output extension. When Output implements it, the
// generator removes files left over from its previous run; see
// Options.KeepStaleFiles.
type Pruner interface {
	// Read returns the contents of name, or an error wrapping fs.ErrNotExist.
	Read(ctx context.Context, name string) ([]byte, error)
	// Remove deletes name. Removing a missing file is not an error.
	Remove(ctx context.Context, name string) error
}

// validateName rejects unsafe file names (path traversal, separators, absolute
// paths). The generator applies it to every name before writing.
func validateName(name string) error {
	if name == "" {
		return newErr(ErrOutput, "output", "file name is empty")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") ||
		filepath.IsAbs(name) || name != filepath.Base(name) {
		return newErr(ErrOutput, "output", fmt.Sprintf("unsafe file name %q", name))
	}
	return nil
}

// MemoryOutput is an in-memory Output, primarily for tests and dry inspection.
// It is safe for concurrent use.
type MemoryOutput struct {
	mu    sync.Mutex
	files map[string][]byte
}

// NewMemoryOutput returns an empty in-memory output.
func NewMemoryOutput() *MemoryOutput {
	return &MemoryOutput{files: make(map[string][]byte)}
}

// Write implements Output, copying data so the caller may reuse its buffer.
func (m *MemoryOutput) Write(ctx context.Context, name string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	m.mu.Lock()
	m.files[name] = cp
	m.mu.Unlock()
	return nil
}

// Get returns the bytes written under name and whether it exists.
func (m *MemoryOutput) Get(name string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[name]
	return b, ok
}

// Read implements Pruner.
func (m *MemoryOutput) Read(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, ok := m.Get(name)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return b, nil
}

// Remove implements Pruner.
func (m *MemoryOutput) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.files, name)
	m.mu.Unlock()
	return nil
}

// Names returns the sorted list of written file names.
func (m *MemoryOutput) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.files))
	for n := range m.files {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// FileOutput writes files to a directory on the local filesystem. Writes are
// atomic (temp file + rename) and confined to Dir; unsafe names are rejected.
// It implements Pruner.
type FileOutput struct {
	// Dir is the destination directory. It is created if missing.
	Dir string
	// FileMode is the permission for written files. Zero -> 0o644.
	FileMode os.FileMode
	// DirMode is the permission used when creating Dir. Zero -> 0o755.
	DirMode os.FileMode

	mu     sync.Mutex
	absDir string
}

// NewFileOutput returns a FileOutput writing into dir.
func NewFileOutput(dir string) *FileOutput {
	return &FileOutput{Dir: dir}
}

// dir resolves and creates the output directory. Failures are not cached, so a
// later call can succeed once the cause is fixed.
func (f *FileOutput) dir() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.absDir != "" {
		return f.absDir, nil
	}
	abs, err := filepath.Abs(f.Dir)
	if err != nil {
		return "", newErr(ErrOutput, "output", "could not resolve output directory").wrap(err)
	}
	dirMode := f.DirMode
	if dirMode == 0 {
		dirMode = 0o755
	}
	if err := os.MkdirAll(abs, dirMode); err != nil {
		return "", newErr(ErrOutput, "output", "could not create output directory").wrap(err)
	}
	f.absDir = abs
	return abs, nil
}

// path validates name and returns its absolute path inside the output directory.
func (f *FileOutput) path(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := f.dir()
	if err != nil {
		return "", err
	}
	final := filepath.Join(dir, name)
	if !withinDir(dir, final) {
		return "", newErr(ErrOutput, "output", fmt.Sprintf("refusing path outside output directory: %q", name))
	}
	return final, nil
}

// Write implements Output atomically and safely.
func (f *FileOutput) Write(ctx context.Context, name string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	final, err := f.path(name)
	if err != nil {
		return err
	}

	fileMode := f.FileMode
	if fileMode == 0 {
		fileMode = 0o644
	}

	tmp, err := os.CreateTemp(filepath.Dir(final), "."+name+".tmp-*")
	if err != nil {
		return newErr(ErrOutput, "output", "could not create temp file").wrap(err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return newErr(ErrOutput, "output", "could not write temp file").wrap(err)
	}
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		cleanup()
		return newErr(ErrOutput, "output", "could not chmod temp file").wrap(err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return newErr(ErrOutput, "output", "could not close temp file").wrap(err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		cleanup()
		return newErr(ErrOutput, "output", "could not rename temp file into place").wrap(err)
	}
	return nil
}

// Read implements Pruner.
func (f *FileOutput) Read(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := f.path(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, newErr(ErrOutput, "output", fmt.Sprintf("could not read %q", name)).wrap(err)
	}
	return b, nil
}

// Remove implements Pruner.
func (f *FileOutput) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := f.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return newErr(ErrOutput, "output", fmt.Sprintf("could not remove %q", name)).wrap(err)
	}
	return nil
}

// withinDir reports whether path is dir or is contained within dir.
func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
