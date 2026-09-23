package sitemap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Stager is an optional Output extension that makes a run all-or-nothing. The
// generator writes every file of a run into a Staging and publishes them with
// Commit only after the whole run succeeded, so a failed run leaves the
// previously published set untouched.
type Stager interface {
	Stage(ctx context.Context) (Staging, error)
}

// Staging holds one run's files until they are committed or aborted.
type Staging interface {
	Output
	// Commit publishes the staged files in the given order: sitemaps first,
	// index files last. Once started it should run to completion.
	Commit(ctx context.Context, names []string) error
	// Abort discards the staged files.
	Abort() error
}

const (
	stagingPrefix = ".staging-"
	// staleStagingAge is how old a leftover staging directory (from a
	// crashed process) must be before Stage removes it.
	staleStagingAge = 24 * time.Hour
)

// Stage implements Stager with a hidden directory inside Dir, so that
// publishing is a same-filesystem rename per file.
func (f *FileOutput) Stage(ctx context.Context) (Staging, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := f.dir()
	if err != nil {
		return nil, err
	}
	removeStaleStaging(dir)
	tmp, err := os.MkdirTemp(dir, stagingPrefix)
	if err != nil {
		return nil, newErr(ErrOutput, "output", "could not create staging directory").wrap(err)
	}
	return &fileStaging{out: &FileOutput{Dir: tmp, FileMode: f.FileMode}, final: dir}, nil
}

func removeStaleStaging(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), stagingPrefix) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleStagingAge {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

type fileStaging struct {
	out   *FileOutput // writes into the staging directory
	final string
}

func (s *fileStaging) Write(ctx context.Context, name string, data []byte) error {
	return s.out.Write(ctx, name, data)
}

// Commit renames each staged file into place. Cancellation is ignored so the
// published set is never left half-updated by it.
func (s *fileStaging) Commit(_ context.Context, names []string) error {
	for _, name := range names {
		if err := validateName(name); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(s.out.Dir, name), filepath.Join(s.final, name)); err != nil {
			return newErr(ErrOutput, "commit", fmt.Sprintf("could not publish %q", name)).wrap(err)
		}
	}
	_ = s.Abort()
	return nil
}

func (s *fileStaging) Abort() error { return os.RemoveAll(s.out.Dir) }

// Stage implements Stager. Commit swaps all files in under one lock, so readers
// such as FileServer never see a mixed set.
func (m *MemoryOutput) Stage(ctx context.Context) (Staging, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &memStaging{parent: m, staged: NewMemoryOutput()}, nil
}

type memStaging struct {
	parent, staged *MemoryOutput
}

func (s *memStaging) Write(ctx context.Context, name string, data []byte) error {
	return s.staged.Write(ctx, name, data)
}

func (s *memStaging) Commit(_ context.Context, names []string) error {
	s.staged.mu.Lock()
	defer s.staged.mu.Unlock()
	for _, name := range names {
		if _, ok := s.staged.files[name]; !ok {
			return newErr(ErrOutput, "commit", fmt.Sprintf("file %q was not staged", name))
		}
	}
	s.parent.mu.Lock()
	for _, name := range names {
		s.parent.files[name] = s.staged.files[name]
	}
	s.parent.mu.Unlock()
	return nil
}

func (s *memStaging) Abort() error { return nil }
