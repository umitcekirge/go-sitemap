package sitemap

import (
	"bytes"
	"time"
)

const (
	sitemapIndexOpen = `<sitemapindex xmlns="` + nsSitemap + `">`
	sitemapIndexEnd  = `</sitemapindex>`
)

// indexEntry is one <sitemap> reference in a sitemap index.
type indexEntry struct {
	loc     string
	lastMod *time.Time
}

// writeIndexEntry renders one <sitemap> block.
func (r *renderer) writeIndexEntry(buf *bytes.Buffer, e indexEntry) {
	r.indent(buf, 1)
	buf.WriteString("<sitemap>")
	r.nl(buf)
	r.elem(buf, 2, "loc", e.loc)
	if e.lastMod != nil {
		r.elem(buf, 2, "lastmod", e.lastMod.Format(r.lastModFormat))
	}
	r.indent(buf, 1)
	buf.WriteString("</sitemap>")
	r.nl(buf)
}

func (r *renderer) indexHeaderLen() int {
	n := len(xmlDecl) + len(sitemapIndexOpen)
	if r.pretty {
		n++
	}
	return n
}

func (r *renderer) indexFooterLen() int {
	n := len(sitemapIndexEnd)
	if r.pretty {
		n++
	}
	return n
}

// renderIndex renders a sitemap index document referencing the given entries.
// The output is deterministic for a given input order.
func (r *renderer) renderIndex(entries []indexEntry) []byte {
	var buf bytes.Buffer
	buf.WriteString(xmlDecl)
	buf.WriteString(sitemapIndexOpen)
	r.nl(&buf)
	for i := range entries {
		r.writeIndexEntry(&buf, entries[i])
	}
	buf.WriteString(sitemapIndexEnd)
	r.nl(&buf)
	return buf.Bytes()
}

// packIndex groups entries into chunks bounded by both maxEntries and maxBytes
// (uncompressed XML), mirroring the per-file limits applied to sitemaps. Each
// chunk holds at least one entry. Multiple chunks become multiple index files
// (the protocol forbids nesting an index inside an index).
func (r *renderer) packIndex(entries []indexEntry, maxEntries int, maxBytes int64) [][]indexEntry {
	overhead := int64(r.indexHeaderLen() + r.indexFooterLen())

	var (
		chunks  [][]indexEntry
		current []indexEntry
		curLen  = overhead
		scratch bytes.Buffer
	)
	flush := func() {
		if len(current) > 0 {
			chunks = append(chunks, current)
			current = nil
			curLen = overhead
		}
	}
	for i := range entries {
		scratch.Reset()
		r.writeIndexEntry(&scratch, entries[i])
		entryLen := int64(scratch.Len())
		if len(current) > 0 && (len(current) >= maxEntries || curLen+entryLen > maxBytes) {
			flush()
		}
		current = append(current, entries[i])
		curLen += entryLen
	}
	flush()
	if len(chunks) == 0 {
		chunks = append(chunks, nil)
	}
	return chunks
}
