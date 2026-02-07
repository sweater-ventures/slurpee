package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/sweater-ventures/slurpee/app"
)

type sessionContextKey struct{}

// SessionAuthMiddleware returns a middleware that checks the slurpee_session cookie
// against the SessionStore. Exempt routes: GET /login, POST /login, GET /version,
// and static assets (paths starting with /static/).
// On valid session, injects SessionInfo into request context.
func SessionAuthMiddleware(slurpee *app.Application) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Exempt routes
			path := r.URL.Path
			if path == "/login" || strings.HasPrefix(path, "/static/") || path == "/version" || strings.HasPrefix(path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie("slurpee_session")
			if err != nil || cookie.Value == "" {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			session := slurpee.Sessions.GetSession(cookie.Value)
			if session == nil {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			ctx := context.WithValue(r.Context(), sessionContextKey{}, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetSessionFromContext returns the SessionInfo from the request context, or nil if not present.
func GetSessionFromContext(ctx context.Context) *app.SessionInfo {
	session, ok := ctx.Value(sessionContextKey{}).(*app.SessionInfo)
	if !ok {
		return nil
	}
	return session
}
