package sitemap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Generator renders providers into sitemap files. It is created with New, is
// safe for sequential reuse, and holds only normalised configuration.
type Generator struct {
	opts     Options
	siteHost string // non-empty only when SameHostOnly is set
}

// New validates and normalises opts, returning a ready Generator or a wrapped
// ErrInvalidOptions.
func New(opts Options) (*Generator, error) {
	n, err := opts.normalize()
	if err != nil {
		return nil, err
	}
	g := &Generator{opts: n}
	if n.SameHostOnly {
		src := n.BaseURL
		if src == "" {
			src = n.PublicURLPrefix
		}
		host, err := hostOf(src)
		if err != nil {
			return nil, newErr(ErrInvalidOptions, "options", "could not parse host for SameHostOnly").wrap(err)
		}
		g.siteHost = host
	}
	return g, nil
}

// Options returns a copy of the normalised options in effect.
func (g *Generator) Options() Options { return g.opts }

// Generate renders the given providers, writes the resulting files and index
// via the configured Output, optionally notifies, and returns a detailed
// Result. Generation stops promptly when ctx is cancelled.
//
// In ModeStrict the first invalid entry aborts generation with a typed error.
// In ModeLenient/ModeCollect invalid entries are skipped and recorded in
// Result.ValidationErrors.
func (g *Generator) Generate(ctx context.Context, providers ...Provider) (*Result, error) {
	start := g.opts.Clock()
	res := &Result{DryRun: g.opts.DryRun}

	if err := g.validateProviders(providers); err != nil {
		return nil, err
	}

	r := &renderer{pretty: g.opts.PrettyXML, lastModFormat: g.opts.LastModFormat}

	for _, p := range providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := g.runProvider(ctx, r, p, res); err != nil {
			return nil, err
		}
	}

	if err := g.generateIndex(ctx, r, res); err != nil {
		return nil, err
	}

	g.notify(ctx, res)

	res.Duration = g.opts.Clock().Sub(start)
	return res, nil
}

// validateProviders rejects nil providers, empty names, and prefix collisions
// that would cause files to overwrite each other.
func (g *Generator) validateProviders(providers []Provider) error {
	seen := make(map[string]string, len(providers))
	for _, p := range providers {
		if p == nil {
			return newErr(ErrInvalidProvider, "provider", "provider is nil")
		}
		if strings.TrimSpace(p.Name()) == "" {
			return newErr(ErrInvalidProvider, "provider", "provider name is empty")
		}
		prefix := providerPrefix(p)
		if other, ok := seen[prefix]; ok {
			return newErr(ErrInvalidProvider, "provider",
				fmt.Sprintf("providers %q and %q resolve to the same file prefix %q", other, p.Name(), prefix))
		}
		seen[prefix] = p.Name()
	}
	return nil
}

// runProvider streams one provider through validation, transforms, splitting
// and output, updating res.
func (g *Generator) runProvider(ctx context.Context, r *renderer, p Provider, res *Result) error {
	prefix := providerPrefix(p)
	pstat := ProviderStat{Name: p.Name(), Kind: p.Kind()}
	part := 0

	sink := func(doc []byte, urlCount int) error {
		part++
		name := g.opts.FileNamer.SitemapName(prefix, part, g.opts.Gzip)
		fs := FileStat{
			Name:             name,
			PublicURL:        g.opts.PublicURLPrefix + name,
			Provider:         p.Name(),
			Kind:             p.Kind(),
			Part:             part,
			URLCount:         urlCount,
			UncompressedSize: int64(len(doc)),
			Gzipped:          g.opts.Gzip,
		}
		if err := g.writeFile(ctx, name, doc, &fs); err != nil {
			return err
		}
		res.Files = append(res.Files, fs)
		pstat.FileCount++
		pstat.WrittenURLs += urlCount
		pstat.UncompressedBytes += fs.UncompressedSize
		return nil
	}

	sp := newSplitter(r, g.opts.MaxURLsPerSitemap, g.opts.MaxUncompressedBytes, sink)

	handle := func(e Entry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		res.TotalURLs++
		e = g.applyDefaults(e)

		// Transforms take *Entry (user code may mutate). Confine that pointer
		// to runTransforms so the common, transform-free path keeps e on the
		// stack instead of heap-allocating it for every entry.
		if len(g.opts.Transforms) > 0 {
			var (
				keep bool
				err  error
			)
			e, keep, err = g.runTransforms(ctx, e)
			if err != nil {
				return g.handleInvalid(res, &pstat, p.Name(), e.Loc, err)
			}
			if !keep {
				pstat.SkippedURLs++
				return nil
			}
		}

		if err := validateEntry(e, g.siteHost); err != nil {
			return g.handleInvalid(res, &pstat, p.Name(), e.Loc, err)
		}

		if err := sp.add(e); err != nil {
			if errors.Is(err, ErrEntryTooLarge) {
				return g.handleInvalid(res, &pstat, p.Name(), e.Loc, err)
			}
			return err // output/sink failure: always fatal
		}
		return nil
	}

	// A stop error is sticky and wins over whatever Stream returns, so a
	// provider that swallows it cannot turn a failed run into a success.
	var stopErr error
	yield := func(e Entry) error {
		if stopErr == nil {
			stopErr = handle(e)
		}
		return stopErr
	}

	streamErr := p.Stream(ctx, yield)
	if stopErr != nil {
		return stopErr
	}
	if streamErr != nil {
		return classifyStreamErr(p.Name(), streamErr)
	}
	if err := sp.flush(); err != nil {
		return err
	}

	res.Providers = append(res.Providers, pstat)
	res.WrittenURLs += pstat.WrittenURLs
	res.SkippedURLs += pstat.SkippedURLs
	return nil
}

// writeFile gzips (if enabled) and writes doc, updating fs sizes. On DryRun no
// bytes are written but sizes are still recorded.
func (g *Generator) writeFile(ctx context.Context, name string, doc []byte, fs *FileStat) error {
	if err := validateName(name); err != nil {
		return err
	}
	out := doc
	if g.opts.Gzip {
		gz, err := gzipBytes(doc)
		if err != nil {
			return err
		}
		fs.CompressedSize = int64(len(gz))
		out = gz
	}
	if g.opts.DryRun {
		return nil
	}
	if err := g.opts.Output.Write(ctx, name, out); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return newErr(ErrOutput, "write", fmt.Sprintf("could not write %q", name)).wrap(err)
	}
	return nil
}

// applyDefaults applies opt-in changefreq/priority defaults to entries that do
// not specify their own. It never invents a lastmod. The entry is taken and
// returned by value so the hot path need not take its address.
func (g *Generator) applyDefaults(e Entry) Entry {
	if e.ChangeFreq == "" && g.opts.DefaultChangeFreq != "" {
		e.ChangeFreq = g.opts.DefaultChangeFreq
	}
	if e.Priority == nil && g.opts.DefaultPriority != nil {
		p := *g.opts.DefaultPriority
		e.Priority = &p
	}
	return e
}

// runTransforms applies the configured transforms to e. It is the only place
// that takes the address of an entry on the per-entry path, so the heap
// allocation that &e implies happens only when transforms are configured.
func (g *Generator) runTransforms(ctx context.Context, e Entry) (Entry, bool, error) {
	keep, err := applyTransforms(ctx, g.opts.Transforms, &e)
	return e, keep, err
}

// handleInvalid applies the validation mode: fail in strict mode, or record and
// continue in lenient/collect mode.
func (g *Generator) handleInvalid(res *Result, pstat *ProviderStat, provider, loc string, err error) error {
	if g.opts.Validation == ModeStrict {
		return err
	}
	res.ValidationErrors = append(res.ValidationErrors, EntryError{Provider: provider, Loc: loc, Err: err})
	pstat.SkippedURLs++
	pstat.ErrorCount++
	return nil
}

// generateIndex writes the sitemap index file(s) when required by the options
// and the number of sitemap files produced.
func (g *Generator) generateIndex(ctx context.Context, r *renderer, res *Result) error {
	total := len(res.Files)
	if total == 0 || (total == 1 && !g.opts.AlwaysIndex) {
		return nil
	}

	var lm *time.Time
	if g.opts.IndexLastMod {
		t := g.opts.Clock()
		lm = &t
	}
	entries := make([]indexEntry, 0, total)
	for _, f := range res.Files {
		e := indexEntry{loc: f.PublicURL}
		if lm != nil {
			e.lastMod = lm
		}
		entries = append(entries, e)
	}

	chunks := r.packIndex(entries, ProtocolMaxIndexEntries, g.opts.MaxUncompressedBytes)
	multiple := len(chunks) > 1
	for i, chunk := range chunks {
		doc := r.renderIndex(chunk)
		name := g.opts.FileNamer.IndexName(g.opts.IndexBaseName, i+1, multiple, g.opts.Gzip)
		fs := FileStat{
			Name:             name,
			PublicURL:        g.opts.PublicURLPrefix + name,
			Part:             i + 1,
			URLCount:         len(chunk),
			UncompressedSize: int64(len(doc)),
			Gzipped:          g.opts.Gzip,
			IsIndex:          true,
		}
		if err := g.writeFile(ctx, name, doc, &fs); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return newErr(ErrIndex, "index", "could not write index file").wrap(err)
		}
		res.IndexFiles = append(res.IndexFiles, fs)
	}
	return nil
}

// notify runs configured notifiers (skipped on DryRun) and records outcomes.
// Notification errors are reported in Result but do not fail generation.
func (g *Generator) notify(ctx context.Context, res *Result) {
	if g.opts.DryRun || len(g.opts.Notifiers) == 0 {
		return
	}
	var urls []string
	if len(res.IndexFiles) > 0 {
		urls = res.IndexURLs()
	} else {
		for _, f := range res.Files {
			urls = append(urls, f.PublicURL)
		}
	}
	for _, n := range g.opts.Notifiers {
		if n == nil {
			continue
		}
		err := n.Notify(ctx, urls)
		res.Notifications = append(res.Notifications, NotifyResult{Notifier: n.Name(), Err: err})
		if err != nil && g.opts.Logger != nil {
			g.opts.Logger.Printf("sitemap: notifier %s failed: %v", n.Name(), err)
		}
	}
}

// classifyStreamErr maps an error returned by Provider.Stream to the right
// category: context errors and our own typed errors pass through unchanged;
// anything else is wrapped as ErrProvider.
func classifyStreamErr(provider string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var se *Error
	if errors.As(err, &se) {
		return err
	}
	return (&Error{Op: "stream", Provider: provider, Msg: "provider stream failed", kind: ErrProvider}).wrap(err)
}
