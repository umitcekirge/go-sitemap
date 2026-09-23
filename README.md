# go-sitemap

A modern, production-grade XML sitemap generator for Go with **providers**,
automatic **splitting**, **sitemap indexes**, **gzip**, **validation**,
**extensions** (hreflang/image/video/news), pluggable **output adapters**, and
safe, configurable defaults.

```go
import sitemap "github.com/umitcekirge/go-sitemap"
```

## What it does

`go-sitemap` turns one or more typed **providers** (products, categories,
brands, pages, blog posts, news, …) into protocol-compliant sitemap files:

```
sitemap-products-0001.xml.gz
sitemap-products-0002.xml.gz
sitemap-categories-0001.xml.gz
sitemap-brands-0001.xml.gz
sitemap-pages-0001.xml.gz
sitemap-index.xml.gz
```

It streams entries (so it scales to millions of URLs with bounded memory),
splits automatically at protocol limits, emits a sitemap index when needed,
validates strictly by default, and returns a detailed `Result`.

## Why provider-based generation

A sitemap for a real site is not one flat list of URLs. It is products,
categories, brands, editorial pages, blog posts and news — each with a different
data source, update cadence, and volume. Modelling each as a **Provider**:

- keeps files organised and predictable (`sitemap-products-*`, `sitemap-pages-*`);
- lets each group **stream** from its own database/API without loading
  everything into memory;
- produces **per-provider stats** for observability;
- makes it trivial to regenerate or reason about one entity type at a time.

This is what makes the package suitable for e-commerce, marketplaces,
multilingual sites, large blogs, news platforms, SaaS docs and enterprise sites.

## Installation

```sh
go get github.com/umitcekirge/go-sitemap
```

Requires Go 1.24+. The core package has **zero external dependencies** (standard
library only).

## Basic usage

```go
pages := &sitemap.SliceProvider{
    ProviderName: "pages",
    EntityKind:   sitemap.KindPage,
    Entries: []sitemap.Entry{
        {Loc: "https://example.com/"},
        {Loc: "https://example.com/about"},
    },
}

gen, err := sitemap.New(sitemap.Options{
    BaseURL: "https://example.com",
    Output:  sitemap.NewFileOutput("./public"),
})
if err != nil { /* handle */ }

res, err := gen.Generate(context.Background(), pages)
```

The zero value of `Options` is usable; only `BaseURL` (or `PublicURLPrefix`) is
required. With no `Output`, generation writes to an in-memory store.

## Providers

A provider streams entries for one entity type:

```go
type Provider interface {
    Name() string  // stable; also used (sanitised) for file names
    Kind() Kind    // entity type, e.g. KindProduct
    Stream(ctx context.Context, yield func(Entry) error) error
}
```

Two helpers are built in:

- `SliceProvider` — backed by an in-memory slice (small/static groups, tests).
- `FuncProvider` — backed by a streaming function (the idiomatic choice for
  large, paginated data sources).

### Product provider (streaming from a database)

```go
products := &sitemap.FuncProvider{
    ProviderName: "products",
    EntityKind:   sitemap.KindProduct,
    StreamFunc: func(ctx context.Context, yield func(sitemap.Entry) error) error {
        const pageSize = 1000
        for offset := 0; ; offset += pageSize {
            rows, err := db.Products(ctx, offset, pageSize) // your query
            if err != nil { return err }
            if len(rows) == 0 { return nil }
            for _, p := range rows {
                if err := yield(sitemap.Entry{
                    Loc:     p.URL,
                    LastMod: p.ContentChangedAt, // *time.Time, only when meaningful
                }); err != nil {
                    return err
                }
            }
        }
    },
}
```

### Category provider

```go
categories := &sitemap.SliceProvider{
    ProviderName: "categories",
    EntityKind:   sitemap.KindCategory,
    Entries: []sitemap.Entry{
        {Loc: "https://example.com/c/shoes"},
        {Loc: "https://example.com/c/shirts"},
    },
}
```

### Static page provider

```go
pages := &sitemap.SliceProvider{
    ProviderName: "pages",
    EntityKind:   sitemap.KindPage,
    Entries: []sitemap.Entry{{Loc: "https://example.com/about"}},
}
```

Generate them together — each gets its own files and the index ties them up:

```go
res, _ := gen.Generate(ctx, products, categories, pages)
```

## Gzip

```go
gen, _ := sitemap.New(sitemap.Options{BaseURL: "https://example.com", Gzip: true})
```

Files become `*.xml.gz`. Gzip is **off by default** and clearly documented.
The 50 MB size limit is always enforced against the **uncompressed** XML, never
the compressed output. `Result` reports both `UncompressedSize` and
`CompressedSize`.

## Sitemap index

An index is generated automatically when more than one sitemap file is produced.

```go
sitemap.Options{AlwaysIndex: true}  // force an index even for a single file
sitemap.Options{DisableIndex: true} // suppress index — honoured only when ≤1 file
```

`DisableIndex` is **ignored when multiple files exist**, because a multi-file set
needs an index to be discoverable. Index file naming follows
`IndexBaseName` (default `sitemap-index`; set to `sitemap` for the
`sitemap.xml` strategy). Every URL inside the index is absolute.

## Hreflang (multilingual)

```go
{
    Loc: "https://example.com/en/page",
    Alternates: []sitemap.Alternate{
        {HrefLang: "en", Href: "https://example.com/en/page"},
        {HrefLang: "de", Href: "https://example.com/de/seite"},
        {HrefLang: "x-default", Href: "https://example.com/en/page"},
    },
}
```

The XHTML namespace is added only to files that actually contain alternates.
Alternates are emitted in the order supplied. Per Google's guidance, alternate
sets should be **reciprocal and complete** across all language variants.

## Image sitemap

```go
{
    Loc: "https://example.com/gallery",
    Images: []sitemap.Image{
        {Loc: "https://cdn.example.com/a.jpg", Title: "Sunset", Caption: "Golden hour"},
        {Loc: "https://cdn.example.com/b.jpg", License: "https://example.com/license"},
    },
}
```

The image namespace is emitted only when image data is present. Image `Loc`
must be absolute. Video and news extensions work the same way
(`Entry.Videos`, `Entry.News`); see the GoDoc for required fields.

## IndexNow (optional)

IndexNow is the recommended **modern** programmatic notification. It is
**disabled** unless you add a notifier. It submits the content URLs that
actually changed, set in `URLs`; sitemap URLs are never submitted, and nothing
is sent when `URLs` is empty:

```go
notifier := &sitemap.IndexNowNotifier{
    Key:  "your-indexnow-key",
    Host: "example.com",
    URLs: []string{"https://example.com/p/42"}, // the changed URLs
}
gen, _ := sitemap.New(sitemap.Options{
    BaseURL:   "https://example.com",
    Notifiers: []sitemap.Notifier{notifier},
})
```

Notification never happens by default and is skipped on `DryRun`. Notifier
outcomes are reported in `Result.Notifications`; a notifier failure does not
fail generation.

## robots.txt integration

```go
lines := sitemap.RobotsSitemapLinesFromResult(res)
// Sitemap: https://example.com/sitemap-index.xml
```

Add these lines to your `robots.txt`. The helper never reads or modifies an
existing `robots.txt`. Declaring the sitemap in `robots.txt` (plus Search
Console) is the recommended way to get Google to discover it.

## Configuration options

All behaviour is configured through the `Options` struct. Highlights:

| Area | Field(s) |
|------|----------|
| Public URLs | `BaseURL`, `PublicURLPrefix` |
| Output backend | `Output` |
| Gzip | `Gzip` |
| Split limits | `MaxURLsPerSitemap`, `MaxUncompressedBytes`, `AllowNonStandardLimits` |
| Index | `AlwaysIndex`, `DisableIndex`, `IndexBaseName`, `IndexLastMod` |
| Validation | `Validation`, `SameHostOnly` |
| Output formatting | `PrettyXML`, `LastModFormat` |
| Opt-in defaults | `DefaultChangeFreq`, `DefaultPriority` |
| File naming | `FileNamer` |
| Hooks | `Transforms`, `Notifiers`, `Logger`, `Clock` |
| CI | `DryRun` |

### Default configuration table

| Option | Default | Notes |
|--------|---------|-------|
| Automatic splitting | enabled | by URL count and uncompressed bytes |
| `MaxURLsPerSitemap` | `50000` | protocol limit |
| `MaxUncompressedBytes` | `52428800` (50 MiB) | enforced on uncompressed XML |
| `AllowNonStandardLimits` | `false` | protocol limits are hard caps |
| Index generation | automatic when >1 file | |
| `AlwaysIndex` | `false` | |
| `DisableIndex` | `false` | ignored when >1 file |
| `IndexBaseName` | `sitemap-index` | |
| `Validation` | `ModeStrict` | |
| `Gzip` | `false` | documented; opt-in |
| `PrettyXML` | `false` | compact output |
| `DefaultPriority` | none | never faked |
| `DefaultChangeFreq` | none | never faked |
| `LastMod` | none | never auto-set |
| `LastModFormat` | RFC3339 | configurable |
| Notification | disabled | no network calls by default |
| Legacy ping | disabled | legacy/compatibility only |
| IndexNow | disabled | opt-in |
| File naming | provider-based, zero-padded | `sitemap-<prefix>-0001.xml` |
| Context cancellation | always supported | |
| Output (unset) | in-memory | set `FileOutput` for production |

Zero-value options are normalised to these defaults. Invalid explicit values
return a wrapped `ErrInvalidOptions` from `New`.

## Validation modes

| Mode | Behaviour |
|------|-----------|
| `ModeStrict` (default) | fail on the first invalid entry |
| `ModeLenient` | skip invalid entries, record them in `Result.ValidationErrors`, continue |
| `ModeCollect` | like lenient; intended for reporting the full error list without failing |

Validation enforces: `Loc` present, absolute, http(s); `Priority` in `[0,1]`;
`ChangeFreq` from the allowed set; absolute alternate/image/video URLs; required
video/news fields; safe file names; absolute public URLs.

## Split rules

- ≤ 50,000 URLs per sitemap file (default).
- ≤ 50 MB **uncompressed** XML per file (default).
- A sitemap **index** can reference up to 50,000 sitemap files and is itself
  bounded by 50 MB uncompressed; it splits into multiple index files when either
  limit is reached.
- Gzip does **not** change the uncompressed size limit.
- Splitting preserves valid XML and works with extensions.
- If a single entry cannot fit a file by itself, `ErrEntryTooLarge` is returned
  (in lenient/collect mode it is skipped and recorded instead).

### Overriding split limits

```go
// Smaller files for debugging / CDN behaviour / unit tests:
sitemap.Options{MaxURLsPerSitemap: 1000, MaxUncompressedBytes: 5 << 20}

// Non-standard larger files for an internal, non-search-engine consumer:
sitemap.Options{MaxURLsPerSitemap: 100000, AllowNonStandardLimits: true}
```

Values that exceed protocol limits require `AllowNonStandardLimits`; otherwise
`New` returns an error. You cannot accidentally exceed protocol limits.

## lastmod policy

`go-sitemap` **never invents a lastmod**. `<lastmod>` is written only when a
provider sets `Entry.LastMod`, and regenerating the sitemap does not change it.

Set `LastMod` only for **meaningful content changes**, e.g.:

- product title/description/price/availability changed (if indexed/visible);
- main image or canonical URL changed;
- article body or category metadata changed.

Do **not** bump `LastMod` for: sitemap regeneration, footer/copyright tweaks,
tracking-script changes, layout-only changes, or non-indexed metadata.

Use `LastModFormat` for date-only output: `Options{LastModFormat: "2006-01-02"}`.

## priority and changefreq policy

`priority` and `changefreq` are part of the protocol but **ignored by modern
Google indexing**. The package never sets them implicitly. Supply them per entry
only if a non-Google consumer needs them, or opt in to an explicit default via
`Options.DefaultPriority` / `Options.DefaultChangeFreq`. Do not fake values.

## Extension support

| Extension | Field | Namespace emitted… |
|-----------|-------|--------------------|
| hreflang | `Entry.Alternates` | only when alternates exist |
| image | `Entry.Images` | only when images exist |
| video | `Entry.Videos` | only when videos exist |
| news | `Entry.News` | only when news exists |

Multiple extensions may appear on the same entry; each is validated. News
sitemaps are only for content eligible for Google News.

## Storage / output adapters

```go
type Output interface {
    Write(ctx context.Context, name string, data []byte) error
}
```

Built in:

- `FileOutput` — atomic writes (temp file + rename) confined to a directory;
  rejects path traversal and unsafe names.
- `MemoryOutput` — in-memory store for tests and inspection (`Get`, `Names`).

Custom backends (S3, GCS, HTTP upload) implement the one-method interface and
live in your own code or separate modules, keeping the core dependency-free.
The optional `FileServer` serves files from a `MemoryOutput` with correct
`Content-Type`/`Content-Encoding`/`Cache-Control`/`ETag` and conditional GET.

## Error handling

Typed sentinels support `errors.Is`/`errors.As`:

```go
ErrInvalidOptions, ErrInvalidProvider, ErrInvalidURL, ErrInvalidEntry,
ErrEntryTooLarge, ErrProvider, ErrOutput, ErrXML, ErrGzip, ErrIndex, ErrNotify
```

```go
if errors.Is(err, sitemap.ErrEntryTooLarge) { /* … */ }

var e *sitemap.Error
if errors.As(err, &e) {
    log.Printf("field=%s loc=%s: %s", e.Field, e.Loc, e.Msg)
}
```

The package never panics for bad input.

## Result reporting

`Generate` returns a `*Result` with: `Files`, `IndexFiles`, `Providers`,
`TotalURLs`, `WrittenURLs`, `SkippedURLs`, `ValidationErrors`, `Duration`,
`Notifications`. Each `FileStat` carries name, public URL, provider, part
number, URL count, uncompressed/compressed sizes and gzip flag. Each
`ProviderStat` aggregates file/URL/skip/error counts and bytes.

## Dry run (CI/CD)

```go
gen, _ := sitemap.New(sitemap.Options{BaseURL: "https://example.com", DryRun: true})
res, _ := gen.Generate(ctx, providers...)
// Validates, counts, simulates splitting & file names/sizes. Writes nothing.
```

## Testing and quality commands

```sh
go build ./...
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...      # or: go run honnef.co/go/tools/cmd/staticcheck@latest ./...
golangci-lint run
gofmt -l .
```

## SEO notes

- Declare the sitemap (index) in `robots.txt` and submit it in Search Console.
- The legacy GET "ping" endpoints are deprecated; **Google removed its ping**.
  Don't rely on pinging for Google discovery.
- IndexNow is optional and orthogonal to sitemap generation.
- Keep `lastmod` accurate; don't fake `priority`/`changefreq`.
- hreflang sets should be reciprocal and complete.

## Performance

The hot path is allocation-light and designed for millions of URLs:

- Entries are **streamed**; only the current file's body is buffered (≤
  `MaxUncompressedBytes`), so peak memory is independent of total site size.
- XML escaping is done with a **zero-allocation** byte scanner; `lastmod`,
  `priority` and `duration` are formatted directly into the buffer.
- URL validation is a manual absolute-http(s) check — **no `net/url.Parse`** per
  entry.
- Per-file header lengths are memoised; the document buffer and gzip writers are
  reused/pooled across files.
- Entries travel by value through the hot path, so a plain entry costs ~0 heap
  allocations inside the library.

Indicative figures (Apple M-series, `go test -bench`): ~18 ms and ~0 library
allocations per 100k plain URLs; gzip adds ~10 ms. Run `go test -bench=. -benchmem`
to reproduce.

**Custom Output contract:** `Write` must finish using the `data` slice before it
returns and must not retain it — the generator reuses the underlying buffer.
Copy the bytes if your backend stores them asynchronously (as `MemoryOutput`
does); synchronous writes (as `FileOutput` does) are always safe.

## Limitations

- The body of the *current* file is buffered (≤ `MaxUncompressedBytes`) so
  namespaces can be decided per file; memory is bounded but not zero.
- URL validation accepts any absolute `http(s)://host/...` string and rejects
  spaces/control characters, but does not perform full RFC 3986 parsing (a
  deliberate trade-off for per-entry speed).
- Provider generation is sequential and deterministic (no built-in concurrency).
- Exceeding 50,000 sitemap files (or 50 MB of index XML) produces multiple
  sibling index files (the protocol forbids index-of-index nesting); they are
  reported in `Result.IndexFiles`, not nested.
- Image `title`/`caption` are emitted for compatibility though current Google
  image schema centres on `image:loc`.

## Versioning policy

Semantic versioning. The public API is intentionally small (providers, entries,
options, generator, output, notifiers, result). Breaking changes are reserved
for major versions and documented in the changelog.

## License

MIT.
