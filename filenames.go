package sitemap

import (
	"fmt"
	"strings"
)

// FileNamer produces deterministic, filesystem-safe file names. Implement it to
// override the default provider-based naming scheme.
type FileNamer interface {
	// SitemapName returns the file name for a provider's sitemap part. prefix
	// is the already-sanitised provider prefix, part is 1-based, gzip selects
	// the extension.
	SitemapName(prefix string, part int, gzip bool) string
	// IndexName returns the name of an index file. base is Options.IndexBaseName.
	// When multiple index files are produced, part is 1-based and multiple is
	// true so the namer can disambiguate; otherwise part is 1 and multiple is
	// false.
	IndexName(base string, part int, multiple, gzip bool) string
}

// DefaultFileNamer implements provider-based, zero-padded, deterministic
// naming, e.g. "sitemap-products-0001.xml.gz" and "sitemap-index.xml".
type DefaultFileNamer struct{}

// SitemapName implements FileNamer.
func (DefaultFileNamer) SitemapName(prefix string, part int, gzip bool) string {
	return fmt.Sprintf("sitemap-%s-%04d%s", prefix, part, ext(gzip))
}

// IndexName implements FileNamer.
func (DefaultFileNamer) IndexName(base string, part int, multiple, gzip bool) string {
	if multiple {
		return fmt.Sprintf("%s-%04d%s", base, part, ext(gzip))
	}
	return base + ext(gzip)
}

// ext returns the file extension for the gzip setting.
func ext(gzip bool) string {
	if gzip {
		return ".xml.gz"
	}
	return ".xml"
}

// sanitizePrefix converts an arbitrary provider name/prefix into a safe,
// deterministic file-name component: lowercase ASCII letters, digits and single
// hyphens, with no leading/trailing hyphen. It never returns a string that
// could escape a directory. Empty/degenerate input yields "sitemap".
func sanitizePrefix(s string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			// Collapse any run of unsafe characters into a single hyphen.
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "sitemap"
	}
	return out
}

// providerPrefix resolves the file-name prefix for a provider: an explicit
// Prefix() if non-empty, otherwise the provider Name, otherwise the Kind. The
// result is always sanitised, so it is safe even for hostile provider names.
func providerPrefix(p Provider) string {
	if pf, ok := p.(Prefixer); ok {
		if raw := pf.Prefix(); strings.TrimSpace(raw) != "" {
			return sanitizePrefix(raw)
		}
	}
	if n := p.Name(); strings.TrimSpace(n) != "" {
		return sanitizePrefix(n)
	}
	return sanitizePrefix(string(p.Kind()))
}
