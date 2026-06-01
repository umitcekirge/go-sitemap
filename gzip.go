package sitemap

import (
	"bytes"
	"compress/gzip"
	"io"
	"sync"
)

// gzipWriterPool reuses gzip.Writer instances (and their internal compressor
// state, which is the costly allocation) across files and concurrent
// generations.
var gzipWriterPool = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

// gzipBytes returns the gzip-compressed form of data. The uncompressed size
// limit is always enforced against the original data, never the compressed
// output, so callers measure size before compressing.
func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(len(data) / 2)

	zw := gzipWriterPool.Get().(*gzip.Writer)
	zw.Reset(&buf)

	if _, err := zw.Write(data); err != nil {
		_ = zw.Close()
		gzipWriterPool.Put(zw)
		return nil, newErr(ErrGzip, "gzip", "could not compress data").wrap(err)
	}
	if err := zw.Close(); err != nil {
		gzipWriterPool.Put(zw)
		return nil, newErr(ErrGzip, "gzip", "could not finalise gzip stream").wrap(err)
	}
	gzipWriterPool.Put(zw)
	return buf.Bytes(), nil
}
