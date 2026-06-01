package sitemap

import "strings"

// RobotsSitemapLines returns the "Sitemap:" directive lines to add to a
// robots.txt for the given sitemap (index) URLs. It does not read or modify any
// existing robots.txt; integrate the output manually. This is the recommended,
// non-deprecated way to advertise sitemaps to search engines.
//
//	Sitemap: https://example.com/sitemap-index.xml
func RobotsSitemapLines(sitemapURLs ...string) string {
	if len(sitemapURLs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, u := range sitemapURLs {
		if u == "" {
			continue
		}
		b.WriteString("Sitemap: ")
		b.WriteString(u)
		b.WriteByte('\n')
	}
	return b.String()
}

// RobotsSitemapLinesFromResult is a convenience that returns the robots.txt
// Sitemap directives for a generation Result, preferring index URLs and falling
// back to individual sitemap file URLs when no index was produced.
func RobotsSitemapLinesFromResult(r *Result) string {
	if r == nil {
		return ""
	}
	if len(r.IndexFiles) > 0 {
		return RobotsSitemapLines(r.IndexURLs()...)
	}
	urls := make([]string, 0, len(r.Files))
	for _, f := range r.Files {
		urls = append(urls, f.PublicURL)
	}
	return RobotsSitemapLines(urls...)
}
