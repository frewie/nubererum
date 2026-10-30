package main

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

var gzipWriters = sync.Pool{New: func() any {
	writer, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
	return writer
}}

// compressible lists the content types worth compressing: the task model is
// several hundred KB of JSON.
func compressible(contentType string) bool {
	for _, prefix := range []string{"application/json", "application/manifest+json", "image/svg+xml", "text/html", "text/css", "text/javascript", "application/javascript"} {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gzip        *gzip.Writer
	decided     bool
	compressing bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if !w.decided {
		w.decided = true
		h := w.Header()
		if status == http.StatusOK && h.Get("Content-Encoding") == "" && compressible(h.Get("Content-Type")) {
			w.compressing = true
			h.Set("Content-Encoding", "gzip")
			h.Del("Content-Length")
			w.gzip = gzipWriters.Get().(*gzip.Writer)
			w.gzip.Reset(w.ResponseWriter)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	if !w.decided {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(data))
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.compressing {
		return w.gzip.Write(data)
	}
	return w.ResponseWriter.Write(data)
}

func (w *gzipResponseWriter) close() {
	if w.compressing {
		_ = w.gzip.Close()
		gzipWriters.Put(w.gzip)
	}
}

// gzipResponses compresses successful text and JSON responses for clients
// that accept gzip.
func gzipResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if r.Method == http.MethodHead || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		writer := &gzipResponseWriter{ResponseWriter: w}
		defer writer.close()
		next.ServeHTTP(writer, r)
	})
}
