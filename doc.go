// Package sitemap is a modern, production-grade XML sitemap generator for Go.
//
// It is built around a provider-based architecture: each Provider streams the
// entries for one logical group of a site (products, categories, pages, blog
// posts, news, …). The Generator consumes providers sequentially, validates and
// renders entries to protocol-compliant XML, automatically splits output into
// multiple files when the sitemap protocol limits are reached, and emits a
// sitemap index when more than one file is produced.
//
// # Core concepts
//
//   - Entry: a single <url> in a sitemap. Only Loc is required. Optional fields
//     (lastmod, changefreq, priority) and extensions (hreflang, image, video,
//     news) are written only when explicitly supplied.
//   - Provider: streams entries for one entity type. Providers must not load all
//     entries into memory; they yield entries one at a time and observe context
//     cancellation.
//   - Generator: orchestrates validation, rendering, splitting, indexing,
//     output and optional notification, returning a detailed Result.
//   - Output: a pluggable storage backend (filesystem, in-memory, or custom).
//
// # Defaults
//
// Only BaseURL (or PublicURLPrefix) and Output are required; other fields are
// normalised to protocol-safe defaults (50,000 URLs and 50 MB uncompressed per
// file, strict validation, automatic index, gzip disabled, stale-file cleanup
// enabled). Invalid options return a typed error from New. See Options for the
// full list of defaults.
//
// # Streaming and memory
//
// Entries are streamed; the generator never holds an entire site in memory. To
// support conditional XML namespaces ("declare a namespace only when an entry
// in the file actually uses it") the body of the *current* file is buffered
// while it is being assembled. Memory use is therefore bounded by
// MaxUncompressedBytes (50 MB by default) and is independent of total site
// size.
//
// # lastmod policy
//
// This package never invents a lastmod. A <lastmod> element is written only
// when a provider sets Entry.LastMod to a meaningful content-change time.
// Regenerating a sitemap does not change lastmod. See the package README for
// guidance on what counts as a meaningful change.
//
// # priority and changefreq policy
//
// priority and changefreq are part of the protocol but are ignored by modern
// Google indexing. This package never sets them implicitly. They are written
// only when an entry supplies them, or when an explicit, opt-in default is
// configured via Options.
package sitemap
