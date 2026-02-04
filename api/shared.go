package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/config"
)

type routeRegistrationFunc func(app *app.Application, router *http.ServeMux)

var routes []routeRegistrationFunc

func registerRoute(r routeRegistrationFunc) {
	routes = append(routes, r)
}

func AddApis(app *app.Application, router *http.ServeMux) {
	slog.Debug("Registering all API Endpoints", "count", len(routes))
	apiRouter := http.NewServeMux()
	for _, r := range routes {
		r(app, apiRouter)
	}
	router.Handle("/api/", http.StripPrefix("/api", apiRouter))
}

func log(ctx context.Context) *slog.Logger {
	log := ctx.Value(config.LoggerContextKey)
	if log == nil {
		return slog.Default()
	} else {
		return log.(*slog.Logger)
	}
}

type appHandler func(app *app.Application, w http.ResponseWriter, r *http.Request)

func routeHandler(app *app.Application, handler appHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(app, w, r)
	})
}

func writeJsonResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
