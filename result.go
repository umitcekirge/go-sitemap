package sitemap

import "time"

// Result is the detailed report returned by Generator.Generate.
type Result struct {
	// Files are the generated sitemap files, in generation order.
	Files []FileStat
	// IndexFiles are the generated sitemap index files (usually one, none, or
	// several when there are more than ProtocolMaxIndexEntries sitemaps).
	IndexFiles []FileStat
	// Providers holds per-provider statistics, in provider order.
	Providers []ProviderStat
	// TotalURLs is the number of entries seen across all providers (before
	// skipping invalid ones).
	TotalURLs int
	// WrittenURLs is the number of entries written to sitemap files.
	WrittenURLs int
	// SkippedURLs is the number of entries dropped by validation or transforms.
	SkippedURLs int
	// ValidationErrors holds details of every skipped/invalid entry (lenient
	// and collect modes). It is empty in strict mode (which fails instead).
	ValidationErrors []EntryError
	// Duration is the wall-clock generation time, measured via Options.Clock.
	Duration time.Duration
	// Notifications holds the outcome of each configured notifier (empty when
	// none configured or on DryRun).
	Notifications []NotifyResult
	// PrunedFiles lists the stale files removed after generation (on DryRun,
	// the files that would be removed).
	PrunedFiles []string
	// PruneErr reports a stale-file cleanup failure. Like notification
	// errors, it does not fail generation.
	PruneErr error
	// DryRun reports whether files were simulated rather than written.
	DryRun bool
}

// IndexURLs returns the public URLs of the generated index file(s).
func (r *Result) IndexURLs() []string {
	urls := make([]string, 0, len(r.IndexFiles))
	for _, f := range r.IndexFiles {
		urls = append(urls, f.PublicURL)
	}
	return urls
}

// fileNames lists the generated files in publish order: sitemaps first, then
// the index files that reference them.
func (r *Result) fileNames() []string {
	names := make([]string, 0, len(r.Files)+len(r.IndexFiles))
	for _, f := range r.Files {
		names = append(names, f.Name)
	}
	for _, f := range r.IndexFiles {
		names = append(names, f.Name)
	}
	return names
}

// FileStat describes one generated file (sitemap or index).
type FileStat struct {
	// Name is the base file name.
	Name string
	// PublicURL is the absolute public URL of the file.
	PublicURL string
	// Provider is the owning provider name ("" for index files).
	Provider string
	// Kind is the provider kind ("" for index files).
	Kind Kind
	// Part is the 1-based part number within the provider (or index series).
	Part int
	// URLCount is the number of <url> (or <sitemap>) entries in the file.
	URLCount int
	// UncompressedSize is the size of the XML in bytes before gzip.
	UncompressedSize int64
	// CompressedSize is the gzip size in bytes, or 0 when gzip is disabled.
	CompressedSize int64
	// Gzipped reports whether the file was gzip-compressed.
	Gzipped bool
	// IsIndex reports whether this is an index file.
	IsIndex bool
}

// ProviderStat aggregates statistics for one provider.
type ProviderStat struct {
	Name              string
	Kind              Kind
	FileCount         int
	WrittenURLs       int
	SkippedURLs       int
	ErrorCount        int
	UncompressedBytes int64
}

// EntryError records a single invalid/skipped entry for lenient and collect
// modes. Err carries the full typed error.
type EntryError struct {
	Provider string
	Loc      string
	Err      error
}

// NotifyResult records the outcome of one notifier.
type NotifyResult struct {
	Notifier string
	Err      error
}
