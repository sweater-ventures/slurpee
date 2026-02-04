package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		capturingWriter := ExtendResponseWriter(w)

		// Call the next handler in the chain
		next.ServeHTTP(capturingWriter, r)

		// Log request details after the handler has executed
		log(r.Context()).Info(fmt.Sprintf("Request %s %s %d %s", r.Method, r.RequestURI, capturingWriter.StatusCode, http.StatusText(capturingWriter.StatusCode)),
			slog.String("method", r.Method),
			slog.String("host", r.Host),
			slog.String("path", r.RequestURI),
			slog.Int("status", capturingWriter.StatusCode),
			slog.Duration("latency", capturingWriter.WriteBegin.Sub(start)),
			slog.Duration("duration", time.Since(start)),
		)
	})
}
