package sitemap

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Protocol limits defined by https://www.sitemaps.org/protocol.html.
const (
	// ProtocolMaxURLs is the maximum number of <url> entries per sitemap file.
	ProtocolMaxURLs = 50000
	// ProtocolMaxBytes is the maximum uncompressed size of a sitemap file.
	ProtocolMaxBytes = 50 * 1024 * 1024 // 50 MiB
	// ProtocolMaxIndexEntries is the maximum number of <sitemap> entries per
	// sitemap index file.
	ProtocolMaxIndexEntries = 50000
	// minUncompressedBytes is the smallest sane per-file byte budget. Below
	// this even an empty sitemap skeleton plus one URL may not fit.
	minUncompressedBytes = 1024
)

// ValidationMode controls how invalid entries are handled.
type ValidationMode int

const (
	// ModeStrict fails generation on the first invalid entry. Default.
	ModeStrict ValidationMode = iota
	// ModeLenient skips invalid entries, records them in Result, and continues.
	ModeLenient
	// ModeCollect behaves like ModeLenient but is intended for reporting: it
	// always completes and returns the full list of validation errors in the
	// Result without failing the call.
	ModeCollect
)

// String implements fmt.Stringer.
func (m ValidationMode) String() string {
	switch m {
	case ModeStrict:
		return "strict"
	case ModeLenient:
		return "lenient"
	case ModeCollect:
		return "collect"
	default:
		return fmt.Sprintf("ValidationMode(%d)", int(m))
	}
}

// Transform optionally mutates or filters an entry before validation. Returning
// keep=false drops the entry (counted as skipped). Transforms run in the order
// configured and must be deterministic. See transform.go.
type Transform func(ctx context.Context, e *Entry) (keep bool, err error)

// Logger is a minimal, optional logging interface. The package is quiet by
// default; structured data is returned in Result rather than logged.
type Logger interface {
	Printf(format string, args ...any)
}

// Options configures a Generator. The zero value is valid: every field is
// normalised to a protocol-safe default by New. Invalid explicit values return
// ErrInvalidOptions.
type Options struct {
	// PublicURLPrefix is prepended to generated file names to form the public
	// URL of each sitemap (and of each entry in the index). It should be an
	// absolute http(s) URL ending in "/", e.g. "https://example.com/sitemaps/".
	// If empty, BaseURL is used. One of PublicURLPrefix or BaseURL is required.
	PublicURLPrefix string

	// BaseURL is the site origin, used as the public URL prefix when
	// PublicURLPrefix is empty (files are then served from the site root).
	BaseURL string

	// Output is the storage backend. Defaults to an in-memory output if nil so
	// that New never fails for lack of a backend; production code should set a
	// FileOutput or custom Output.
	Output Output

	// Gzip enables gzip compression of generated files (".xml.gz"). Default
	// false: files are written as plain ".xml".
	Gzip bool

	// MaxURLsPerSitemap caps entries per file. Zero -> ProtocolMaxURLs (50000).
	// Values above ProtocolMaxURLs require AllowNonStandardLimits.
	MaxURLsPerSitemap int

	// MaxUncompressedBytes caps the uncompressed XML size per file. Zero ->
	// ProtocolMaxBytes (50 MiB). Enforced against uncompressed bytes even when
	// Gzip is enabled. Values above ProtocolMaxBytes require
	// AllowNonStandardLimits.
	MaxUncompressedBytes int64

	// AllowNonStandardLimits permits MaxURLsPerSitemap / MaxUncompressedBytes
	// to exceed protocol limits (for non-standard internal consumers). Default
	// false: protocol limits are hard caps.
	AllowNonStandardLimits bool

	// AlwaysIndex forces an index file even when only one sitemap is produced.
	AlwaysIndex bool

	// DisableIndex suppresses index generation. It is honoured only when at
	// most one sitemap file is produced (a multi-file set requires an index to
	// be discoverable); otherwise New/Generate ignore it for protocol safety.
	DisableIndex bool

	// IndexBaseName is the base name of the index file ("sitemap-index" or
	// "sitemap"). Zero value -> "sitemap-index".
	IndexBaseName string

	// PrettyXML enables indented, newline-separated XML. Default false (compact).
	PrettyXML bool

	// Validation selects how invalid entries are handled. Default ModeStrict.
	Validation ValidationMode

	// SameHostOnly, when set, requires every entry Loc to share the host of
	// BaseURL/PublicURLPrefix. Default false.
	SameHostOnly bool

	// DefaultChangeFreq, if set, is applied to entries that do not specify one.
	// Opt-in only; empty means no default is written.
	DefaultChangeFreq ChangeFreq

	// DefaultPriority, if non-nil, is applied to entries without a priority.
	// Opt-in only.
	DefaultPriority *float64

	// LastModFormat is the time layout for <lastmod>. Zero value -> RFC3339.
	// Use "2006-01-02" for date-only output.
	LastModFormat string

	// FileNamer customises file naming. Zero value -> deterministic
	// provider-based naming (see DefaultFileNamer).
	FileNamer FileNamer

	// Transforms run on every entry before validation, in order.
	Transforms []Transform

	// Notifiers run after successful generation (skipped on DryRun). None by
	// default: no network calls happen unless explicitly configured.
	Notifiers []Notifier

	// Logger receives optional progress messages. Nil disables logging.
	Logger Logger

	// Clock returns the current time, used only for the optional index lastmod
	// when IndexLastMod is set and for Result.Duration. Zero value -> time.Now.
	Clock func() time.Time

	// IndexLastMod, when true, writes a <lastmod> for each sitemap in the index
	// using Clock at generation time. Default false (no index lastmod), keeping
	// with the strict lastmod policy for page entries.
	IndexLastMod bool

	// DryRun validates and simulates generation (counting entries, splitting,
	// file names and sizes) without writing files or sending notifications.
	DryRun bool
}

// normalize returns a copy of o with defaults applied, or an error if any
// explicit value is invalid.
func (o Options) normalize() (Options, error) {
	n := o

	if n.PublicURLPrefix == "" {
		n.PublicURLPrefix = n.BaseURL
	}
	if n.PublicURLPrefix == "" {
		return n, newErr(ErrInvalidOptions, "options", "one of PublicURLPrefix or BaseURL is required")
	}
	if err := validateAbsoluteURL(n.PublicURLPrefix); err != nil {
		return n, newErr(ErrInvalidOptions, "options", "PublicURLPrefix must be an absolute http(s) URL").wrap(err)
	}
	// Ensure the prefix joins cleanly with file names.
	if n.PublicURLPrefix[len(n.PublicURLPrefix)-1] != '/' {
		n.PublicURLPrefix += "/"
	}

	if n.MaxURLsPerSitemap == 0 {
		n.MaxURLsPerSitemap = ProtocolMaxURLs
	}
	if n.MaxURLsPerSitemap < 1 {
		return n, newErr(ErrInvalidOptions, "options", "MaxURLsPerSitemap must be >= 1")
	}
	if n.MaxURLsPerSitemap > ProtocolMaxURLs && !n.AllowNonStandardLimits {
		return n, newErr(ErrInvalidOptions, "options",
			fmt.Sprintf("MaxURLsPerSitemap %d exceeds protocol limit %d (set AllowNonStandardLimits to override)",
				n.MaxURLsPerSitemap, ProtocolMaxURLs))
	}

	if n.MaxUncompressedBytes == 0 {
		n.MaxUncompressedBytes = ProtocolMaxBytes
	}
	if n.MaxUncompressedBytes < minUncompressedBytes {
		return n, newErr(ErrInvalidOptions, "options",
			fmt.Sprintf("MaxUncompressedBytes must be >= %d", minUncompressedBytes))
	}
	if n.MaxUncompressedBytes > ProtocolMaxBytes && !n.AllowNonStandardLimits {
		return n, newErr(ErrInvalidOptions, "options",
			fmt.Sprintf("MaxUncompressedBytes %d exceeds protocol limit %d (set AllowNonStandardLimits to override)",
				n.MaxUncompressedBytes, ProtocolMaxBytes))
	}

	switch n.Validation {
	case ModeStrict, ModeLenient, ModeCollect:
	default:
		return n, newErr(ErrInvalidOptions, "options", "Validation is not a valid ValidationMode")
	}

	if n.DefaultChangeFreq != "" && !validChangeFreqs[n.DefaultChangeFreq] {
		return n, newErr(ErrInvalidOptions, "options", "DefaultChangeFreq is not an allowed changefreq value")
	}
	if n.DefaultPriority != nil {
		if p := *n.DefaultPriority; p < 0 || p > 1 {
			return n, newErr(ErrInvalidOptions, "options", "DefaultPriority must be within [0.0, 1.0]")
		}
	}

	if n.LastModFormat == "" {
		n.LastModFormat = time.RFC3339
	}
	if n.IndexBaseName == "" {
		n.IndexBaseName = "sitemap-index"
	}
	if strings.ContainsAny(n.IndexBaseName, `/\`) || strings.Contains(n.IndexBaseName, "..") {
		return n, newErr(ErrInvalidOptions, "options", "IndexBaseName must not contain path separators or \"..\"")
	}
	if n.FileNamer == nil {
		n.FileNamer = DefaultFileNamer{}
	}
	if n.Output == nil {
		n.Output = NewMemoryOutput()
	}
	if n.Clock == nil {
		n.Clock = time.Now
	}

	return n, nil
}
