package sitemap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
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
	// ModTime is reported as Last-Modified. Zero -> Last-Modified is omitted.
	ModTime time.Time

	mu    sync.Mutex
	etags map[string]etagEntry
}

// etagEntry caches a file's ETag. MemoryOutput stores a fresh copy on every
// write, so the data pointer identifies the content version.
type etagEntry struct {
	data *byte
	etag string
}

// ServeHTTP serves the file named by the last element of the request path, so
// the server works under any mount prefix. Gzip files (".gz") are served with
// Content-Encoding: gzip and the underlying XML Content-Type so clients
// transparently decompress them.
func (s *FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Store == nil {
		http.Error(w, "sitemap store not configured", http.StatusInternalServerError)
		return
	}
	name := path.Base(r.URL.Path)
	if validateName(name) != nil || strings.HasPrefix(name, ".") {
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
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(maxAge))
	w.Header().Set("ETag", s.etag(name, data))

	// http.ServeContent handles conditional GET (If-None-Match,
	// If-Modified-Since), Range requests and HEAD. An empty name is passed so it
	// does not override the Content-Type we set above.
	http.ServeContent(w, r, "", s.ModTime, bytes.NewReader(data))
}

// etag returns the cached ETag for data, hashing only when the file changed.
func (s *FileServer) etag(name string, data []byte) string {
	var ptr *byte
	if len(data) > 0 {
		ptr = &data[0]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.etags[name]; ok && e.data == ptr {
		return e.etag
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	if s.etags == nil {
		s.etags = make(map[string]etagEntry)
	}
	s.etags[name] = etagEntry{data: ptr, etag: etag}
	return etag
}
