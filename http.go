package sitemap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// FileServer serves generated sitemap files from a MemoryOutput with correct
// Content-Type, Content-Encoding, Cache-Control, ETag and Last-Modified
// headers, and conditional-GET support. It is optional and kept separate from
// core generation. For production at scale, prefer serving the files directly
// from your CDN/object store.
type FileServer struct {
	// Store holds the generated files.
	Store *MemoryOutput
	// MaxAge sets Cache-Control max-age. Zero -> 3600 seconds.
	MaxAge int
	// ModTime is reported as Last-Modified. Zero -> the server is created time
	// is unknown, so Last-Modified is omitted.
	ModTime time.Time
}

// ServeHTTP serves the file whose base name matches the request path. Gzip files
// (".gz") are served with Content-Encoding: gzip and the underlying XML
// Content-Type so clients transparently decompress them.
func (s *FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if err := validateName(name); err != nil {
		http.NotFound(w, r)
		return
	}
	data, ok := s.Store.Get(name)
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if strings.HasSuffix(name, ".gz") {
		w.Header().Set("Content-Encoding", "gzip")
	}
	maxAge := s.MaxAge
	if maxAge == 0 {
		maxAge = 3600
	}
	w.Header().Set("Cache-Control", "public, max-age="+itoa(maxAge))

	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("ETag", etag)

	// http.ServeContent handles conditional GET (If-None-Match,
	// If-Modified-Since), Range requests and HEAD. An empty name is passed so it
	// does not override the Content-Type we set above.
	http.ServeContent(w, r, "", s.ModTime, bytes.NewReader(data))
}

// itoa is a tiny non-allocating-ish integer formatter for header values.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
