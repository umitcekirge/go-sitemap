package sitemap

// XML namespace URIs used by sitemap files and extensions.
const (
	nsSitemap = "http://www.sitemaps.org/schemas/sitemap/0.9"
	nsXHTML   = "http://www.w3.org/1999/xhtml"
	nsImage   = "http://www.google.com/schemas/sitemap-image/1.1"
	nsVideo   = "http://www.google.com/schemas/sitemap-video/1.1"
	nsNews    = "http://www.google.com/schemas/sitemap-news/0.9"
)

// nsFlags is a bitmask of the optional namespaces an entry (or accumulated set
// of entries) uses. The <urlset> header declares only the namespaces actually
// needed by the file.
type nsFlags uint8

const (
	nsFlagXHTML nsFlags = 1 << iota
	nsFlagImage
	nsFlagVideo
	nsFlagNews
)

// entryNamespaces returns the optional namespaces a single entry requires. It
// takes the entry by value so callers in the hot path need not take its address
// (which would force a heap allocation per entry).
func entryNamespaces(e Entry) nsFlags {
	var f nsFlags
	if len(e.Alternates) > 0 {
		f |= nsFlagXHTML
	}
	if len(e.Images) > 0 {
		f |= nsFlagImage
	}
	if len(e.Videos) > 0 {
		f |= nsFlagVideo
	}
	if e.News != nil {
		f |= nsFlagNews
	}
	return f
}
