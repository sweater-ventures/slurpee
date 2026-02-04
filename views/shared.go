package views

import (
	"context"
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

func AddViews(app *app.Application, router *http.ServeMux) {
	slog.Debug("Registering all views", "count", len(routes))
	for _, r := range routes {
		r(app, router)
	}
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
