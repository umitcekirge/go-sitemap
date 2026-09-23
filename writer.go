package sitemap

import (
	"bytes"
	"strconv"
	"time"
)

const xmlDecl = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// Namespace attribute fragments, kept as constants so that length accounting
// and actual writing cannot drift apart.
const (
	urlsetOpen = `<urlset xmlns="` + nsSitemap + `"`
	attrXHTML  = ` xmlns:xhtml="` + nsXHTML + `"`
	attrImage  = ` xmlns:image="` + nsImage + `"`
	attrVideo  = ` xmlns:video="` + nsVideo + `"`
	attrNews   = ` xmlns:news="` + nsNews + `"`
	urlsetEnd  = `</urlset>`
)

// renderer renders entries to sitemap XML. A nil buffer means "measure only":
// the dual-purpose writers return the byte length they would have written,
// which the splitter uses for exact size accounting without re-rendering.
type renderer struct {
	pretty        bool
	lastModFormat string
}

func (r *renderer) nl(buf *bytes.Buffer) {
	if r.pretty && buf != nil {
		buf.WriteByte('\n')
	}
}

func (r *renderer) indent(buf *bytes.Buffer, depth int) {
	if !r.pretty || buf == nil {
		return
	}
	for range depth {
		buf.WriteByte('\t')
	}
}

// writeHeader writes (or measures) the document declaration and <urlset> open
// tag with exactly the namespaces indicated by ns. Returns the byte length.
func (r *renderer) writeHeader(buf *bytes.Buffer, ns nsFlags) int {
	n := 0
	w := func(s string) {
		n += len(s)
		if buf != nil {
			buf.WriteString(s)
		}
	}
	w(xmlDecl)
	w(urlsetOpen)
	if ns&nsFlagXHTML != 0 {
		w(attrXHTML)
	}
	if ns&nsFlagImage != 0 {
		w(attrImage)
	}
	if ns&nsFlagVideo != 0 {
		w(attrVideo)
	}
	if ns&nsFlagNews != 0 {
		w(attrNews)
	}
	w(">")
	if r.pretty {
		w("\n")
	}
	return n
}

// writeFooter writes (or measures) the closing </urlset>. Returns byte length.
func (r *renderer) writeFooter(buf *bytes.Buffer) int {
	n := len(urlsetEnd)
	if buf != nil {
		buf.WriteString(urlsetEnd)
	}
	if r.pretty {
		n++
		if buf != nil {
			buf.WriteByte('\n')
		}
	}
	return n
}

func (r *renderer) headerLen(ns nsFlags) int { return r.writeHeader(nil, ns) }
func (r *renderer) footerLen() int           { return r.writeFooter(nil) }

// writeEntry renders one <url> element including any extensions.
func (r *renderer) writeEntry(buf *bytes.Buffer, e Entry) {
	r.indent(buf, 1)
	buf.WriteString("<url>")
	r.nl(buf)

	r.elem(buf, 2, "loc", e.Loc)
	if e.LastMod != nil {
		r.timeElem(buf, 2, "lastmod", *e.LastMod, r.lastModFormat)
	}
	if e.ChangeFreq != "" {
		r.elem(buf, 2, "changefreq", string(e.ChangeFreq))
	}
	if e.Priority != nil {
		r.indent(buf, 2)
		buf.WriteString("<priority>")
		buf.Write(strconv.AppendFloat(buf.AvailableBuffer(), *e.Priority, 'f', -1, 64))
		buf.WriteString("</priority>")
		r.nl(buf)
	}
	for i := range e.Alternates {
		a := e.Alternates[i]
		r.indent(buf, 2)
		buf.WriteString(`<xhtml:link rel="alternate" hreflang="`)
		escape(buf, a.HrefLang)
		buf.WriteString(`" href="`)
		escape(buf, a.Href)
		buf.WriteString(`"/>`)
		r.nl(buf)
	}
	for i := range e.Images {
		r.writeImage(buf, &e.Images[i])
	}
	for i := range e.Videos {
		r.writeVideo(buf, &e.Videos[i])
	}
	if e.News != nil {
		r.writeNews(buf, e.News)
	}

	r.indent(buf, 1)
	buf.WriteString("</url>")
	r.nl(buf)
}

func (r *renderer) writeImage(buf *bytes.Buffer, im *Image) {
	r.indent(buf, 2)
	buf.WriteString("<image:image>")
	r.nl(buf)
	r.elem(buf, 3, "image:loc", im.Loc)
	if im.Title != "" {
		r.elem(buf, 3, "image:title", im.Title)
	}
	if im.Caption != "" {
		r.elem(buf, 3, "image:caption", im.Caption)
	}
	if im.License != "" {
		r.elem(buf, 3, "image:license", im.License)
	}
	r.indent(buf, 2)
	buf.WriteString("</image:image>")
	r.nl(buf)
}

func (r *renderer) writeVideo(buf *bytes.Buffer, v *Video) {
	r.indent(buf, 2)
	buf.WriteString("<video:video>")
	r.nl(buf)
	r.elem(buf, 3, "video:thumbnail_loc", v.ThumbnailLoc)
	r.elem(buf, 3, "video:title", v.Title)
	r.elem(buf, 3, "video:description", v.Description)
	if v.ContentLoc != "" {
		r.elem(buf, 3, "video:content_loc", v.ContentLoc)
	}
	if v.PlayerLoc != "" {
		r.elem(buf, 3, "video:player_loc", v.PlayerLoc)
	}
	if v.hasDuration() {
		r.indent(buf, 3)
		buf.WriteString("<video:duration>")
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), int64(v.Duration), 10))
		buf.WriteString("</video:duration>")
		r.nl(buf)
	}
	if v.PublicationDate != nil {
		r.timeElem(buf, 3, "video:publication_date", *v.PublicationDate, time.RFC3339)
	}
	r.indent(buf, 2)
	buf.WriteString("</video:video>")
	r.nl(buf)
}

func (r *renderer) writeNews(buf *bytes.Buffer, n *News) {
	r.indent(buf, 2)
	buf.WriteString("<news:news>")
	r.nl(buf)
	r.indent(buf, 3)
	buf.WriteString("<news:publication>")
	r.nl(buf)
	r.elem(buf, 4, "news:name", n.PublicationName)
	r.elem(buf, 4, "news:language", n.PublicationLanguage)
	r.indent(buf, 3)
	buf.WriteString("</news:publication>")
	r.nl(buf)
	r.timeElem(buf, 3, "news:publication_date", n.PublicationDate, time.RFC3339)
	r.elem(buf, 3, "news:title", n.Title)
	r.indent(buf, 2)
	buf.WriteString("</news:news>")
	r.nl(buf)
}

// elem writes <name>escaped(val)</name> at the given indent depth.
func (r *renderer) elem(buf *bytes.Buffer, depth int, name, val string) {
	r.indent(buf, depth)
	buf.WriteByte('<')
	buf.WriteString(name)
	buf.WriteByte('>')
	escape(buf, val)
	buf.WriteString("</")
	buf.WriteString(name)
	buf.WriteByte('>')
	r.nl(buf)
}

// timeElem writes <name>t.Format(layout)</name>, formatting the time directly
// into the buffer to avoid an intermediate string allocation.
func (r *renderer) timeElem(buf *bytes.Buffer, depth int, name string, t time.Time, layout string) {
	r.indent(buf, depth)
	buf.WriteByte('<')
	buf.WriteString(name)
	buf.WriteByte('>')
	buf.Write(t.AppendFormat(buf.AvailableBuffer(), layout))
	buf.WriteString("</")
	buf.WriteString(name)
	buf.WriteByte('>')
	r.nl(buf)
}

// escape writes s to buf with XML entity escaping. It writes runs of safe bytes
// directly and escapes only the bytes that require it, so for valid UTF-8 input
// it performs no allocation (unlike xml.EscapeText, which needs a []byte copy).
// Multi-byte UTF-8 sequences are all >= 0x80 and pass through untouched.
func escape(buf *bytes.Buffer, s string) {
	last := 0
	for i := 0; i < len(s); i++ {
		var repl string
		switch s[i] {
		case '&':
			repl = "&amp;"
		case '<':
			repl = "&lt;"
		case '>':
			repl = "&gt;"
		case '"':
			repl = "&#34;"
		case '\'':
			repl = "&#39;"
		case '\t':
			repl = "&#x9;"
		case '\n':
			repl = "&#xA;"
		case '\r':
			repl = "&#xD;"
		default:
			// Other control characters are illegal in XML 1.0; drop them so
			// output is always well-formed.
			if s[i] < 0x20 {
				if last < i {
					buf.WriteString(s[last:i])
				}
				last = i + 1
			}
			continue
		}
		if last < i {
			buf.WriteString(s[last:i])
		}
		buf.WriteString(repl)
		last = i + 1
	}
	if last < len(s) {
		buf.WriteString(s[last:])
	}
}

// splitter accumulates rendered entries into protocol-bounded files. It buffers
// only the body of the current file (so namespaces can be decided per file);
// completed documents are handed to sink. Memory is bounded by maxBytes.
type splitter struct {
	r        *renderer
	maxURLs  int
	maxBytes int64
	sink     func(doc []byte, urlCount int) error

	body      bytes.Buffer
	scratch   bytes.Buffer
	doc       bytes.Buffer // reused across flushes to avoid per-file allocation
	ns        nsFlags
	count     int
	footerLen int

	// headerLen is memoised by namespace set, since it is recomputed on every
	// add() and the namespace set is stable across long runs of entries.
	hdrNS    nsFlags
	hdrLen   int
	hdrValid bool
}

func newSplitter(r *renderer, maxURLs int, maxBytes int64, sink func(doc []byte, urlCount int) error) *splitter {
	return &splitter{
		r:         r,
		maxURLs:   maxURLs,
		maxBytes:  maxBytes,
		sink:      sink,
		footerLen: r.footerLen(),
	}
}

// headerLenFor returns the (memoised) header byte length for a namespace set.
func (s *splitter) headerLenFor(ns nsFlags) int {
	if s.hdrValid && s.hdrNS == ns {
		return s.hdrLen
	}
	s.hdrNS = ns
	s.hdrLen = s.r.headerLen(ns)
	s.hdrValid = true
	return s.hdrLen
}

// add renders e and appends it to the current file, flushing and starting a new
// file first if adding e would breach the URL-count or byte-size limit. If e
// cannot fit in an empty file by itself, ErrEntryTooLarge is returned.
func (s *splitter) add(e Entry) error {
	s.scratch.Reset()
	s.r.writeEntry(&s.scratch, e)
	fragLen := s.scratch.Len()
	entryNS := entryNamespaces(e)

	// Reject before flushing, so an oversized entry does not end the current
	// file early.
	if int64(s.headerLenFor(entryNS))+int64(fragLen)+int64(s.footerLen) > s.maxBytes {
		return locErr(ErrEntryTooLarge, "Loc", e.Loc,
			"entry alone exceeds the per-file uncompressed byte limit")
	}

	if s.count > 0 {
		combined := s.ns | entryNS
		projected := int64(s.headerLenFor(combined)) + int64(s.body.Len()) + int64(fragLen) + int64(s.footerLen)
		if s.count+1 > s.maxURLs || projected > s.maxBytes {
			if err := s.flush(); err != nil {
				return err
			}
		}
	}

	s.body.Write(s.scratch.Bytes())
	s.ns |= entryNS
	s.count++
	return nil
}

// flush assembles and emits the current file, then resets state. It is a no-op
// when the current file is empty.
func (s *splitter) flush() error {
	if s.count == 0 {
		return nil
	}
	s.doc.Reset()
	s.doc.Grow(s.headerLenFor(s.ns) + s.body.Len() + s.footerLen)
	s.r.writeHeader(&s.doc, s.ns)
	s.doc.Write(s.body.Bytes())
	s.r.writeFooter(&s.doc)

	urlCount := s.count
	// Reset before calling sink so a sink error leaves a clean state.
	s.body.Reset()
	s.ns = 0
	s.count = 0
	return s.sink(s.doc.Bytes(), urlCount)
}
