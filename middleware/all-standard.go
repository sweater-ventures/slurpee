package middleware

import (
	"net/http"
)

func AllStandardMiddleware(next http.Handler) http.Handler {
	return ContextLoggerMiddleware(LoggingMiddleware(next))
}
