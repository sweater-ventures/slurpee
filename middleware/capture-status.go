package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"time"
)

type CapturingResponseWriter struct {
	responseWriter http.ResponseWriter
	StatusCode     int
	WriteBegin     time.Time
}

func ExtendResponseWriter(w http.ResponseWriter) *CapturingResponseWriter {
	return &CapturingResponseWriter{w, 0, time.Time{}}
}

func (w *CapturingResponseWriter) Write(b []byte) (int, error) {
	if w.WriteBegin.IsZero() {
		w.WriteBegin = time.Now()
	}

	if w.StatusCode == 0 {
		w.StatusCode = http.StatusOK
	}
	return w.responseWriter.Write(b)
}

func (w *CapturingResponseWriter) Header() http.Header {
	return w.responseWriter.Header()
}

func (w *CapturingResponseWriter) WriteHeader(statusCode int) {
	if w.WriteBegin.IsZero() {
		w.WriteBegin = time.Now()
	}

	// receive status code from this method
	w.StatusCode = statusCode
	w.responseWriter.WriteHeader(statusCode)
}

func (w *CapturingResponseWriter) Flush() {
	flusher, ok := w.responseWriter.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

func (w *CapturingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.responseWriter.(http.Hijacker)
	if ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("the ResponseWriter doesn't support hijacking")
}
