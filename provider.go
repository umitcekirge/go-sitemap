package sitemap

import "context"

// Provider streams the sitemap entries for one logical group of a site
// (products, categories, pages, …). Implementations must:
//
//   - return a stable Name used for deterministic file naming and stats;
//   - return a Kind describing the entity type;
//   - stream entries via Stream without loading them all into memory;
//   - observe context cancellation and return ctx.Err() promptly.
//
// The yield callback returns an error only when the generator wants streaming
// to stop (for example, a strict-mode validation failure). Providers must
// propagate that error unchanged.
type Provider interface {
	// Name is a stable identifier, also used (sanitised) for file names.
	Name() string
	// Kind is the entity type produced by this provider.
	Kind() Kind
	// Stream yields entries one at a time. It must stop and return ctx.Err()
	// when ctx is cancelled, and must return any error from yield unchanged.
	Stream(ctx context.Context, yield func(Entry) error) error
}

// Prefixer is an optional interface a Provider may implement to override the
// file-name prefix derived from its Name. The returned value is sanitised.
type Prefixer interface {
	Prefix() string
}

// SliceProvider is a convenience Provider backed by an in-memory slice. It is
// handy for small/static groups and tests. For large datasets, implement
// Provider directly so entries can be streamed from a database or API.
type SliceProvider struct {
	ProviderName string
	EntityKind   Kind
	FilePrefix   string // optional; overrides the derived prefix when set
	Entries      []Entry
}

// Name implements Provider.
func (p *SliceProvider) Name() string { return p.ProviderName }

// Kind implements Provider.
func (p *SliceProvider) Kind() Kind { return p.EntityKind }

// Prefix implements Prefixer when FilePrefix is set.
func (p *SliceProvider) Prefix() string { return p.FilePrefix }

// Stream implements Provider, yielding each entry and honouring cancellation.
func (p *SliceProvider) Stream(ctx context.Context, yield func(Entry) error) error {
	for i := range p.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := yield(p.Entries[i]); err != nil {
			return err
		}
	}
	return nil
}

// FuncProvider adapts a streaming function to the Provider interface, which is
// the idiomatic way to back a provider with a paginated data source.
type FuncProvider struct {
	ProviderName string
	EntityKind   Kind
	FilePrefix   string
	StreamFunc   func(ctx context.Context, yield func(Entry) error) error
}

// Name implements Provider.
func (p *FuncProvider) Name() string { return p.ProviderName }

// Kind implements Provider.
func (p *FuncProvider) Kind() Kind { return p.EntityKind }

// Prefix implements Prefixer when FilePrefix is set.
func (p *FuncProvider) Prefix() string { return p.FilePrefix }

// Stream implements Provider by delegating to StreamFunc.
func (p *FuncProvider) Stream(ctx context.Context, yield func(Entry) error) error {
	if p.StreamFunc == nil {
		return nil
	}
	return p.StreamFunc(ctx, yield)
}
