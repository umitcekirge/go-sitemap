package sitemap

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// manifestName is the file recording what the last run wrote.
func (g *Generator) manifestName() string {
	return "." + g.opts.IndexBaseName + ".manifest"
}

// prune removes files listed in the previous manifest that this run did not
// produce, then records the current set. Files that fail to be removed stay in
// the manifest so the next run retries them.
func (g *Generator) prune(ctx context.Context, res *Result) error {
	if g.opts.KeepStaleFiles {
		return nil
	}
	p, ok := g.opts.Output.(Pruner)
	if !ok {
		return nil
	}
	manifest := g.manifestName()

	current := make(map[string]bool, len(res.Files)+len(res.IndexFiles))
	for _, f := range res.Files {
		current[f.Name] = true
	}
	for _, f := range res.IndexFiles {
		current[f.Name] = true
	}

	prev, err := p.Read(ctx, manifest)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return newErr(ErrOutput, "prune", "could not read manifest").wrap(err)
	}
	var stale []string
	for _, name := range strings.Split(string(prev), "\n") {
		if name != "" && name != manifest && !current[name] && validateName(name) == nil {
			stale = append(stale, name)
		}
	}
	if g.opts.DryRun {
		res.PrunedFiles = stale
		return nil
	}

	var (
		b    strings.Builder
		errs []error
	)
	for _, name := range stale {
		if err := p.Remove(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			b.WriteString(name + "\n")
			continue
		}
		res.PrunedFiles = append(res.PrunedFiles, name)
	}
	for _, f := range res.Files {
		b.WriteString(f.Name + "\n")
	}
	for _, f := range res.IndexFiles {
		b.WriteString(f.Name + "\n")
	}
	if err := g.opts.Output.Write(ctx, manifest, []byte(b.String())); err != nil {
		errs = append(errs, fmt.Errorf("write manifest: %w", err))
	}
	if len(errs) > 0 {
		return newErr(ErrOutput, "prune", "stale file cleanup incomplete").wrap(errors.Join(errs...))
	}
	return nil
}
