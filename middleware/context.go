package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/sweater-ventures/slurpee/config"
)

func log(ctx context.Context) *slog.Logger {
	log := ctx.Value(config.LoggerContextKey)
	if log == nil {
		return slog.Default()
	} else {
		return (log).(*slog.Logger)
	}
}

// ContextLoggerMiddleware adds a logger to the request context.  This includes a group called request with request id.
func ContextLoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			id, err := uuid.NewV7()
			if err != nil {
				requestID = "unknown"
			} else {
				requestID = id.String()
			}
		}

		r = r.WithContext(context.WithValue(r.Context(), config.LoggerContextKey, log(r.Context()).With(
			slog.String("request_id", requestID),
		)))

		next.ServeHTTP(w, r)
	})
}
