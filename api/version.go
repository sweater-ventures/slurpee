package api

import (
	"net/http"

	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/config"
)

func init() {
	registerRoute(func(app *app.Application, router *http.ServeMux) {
		router.Handle("/version", routeHandler(app, versionApiHandler))
	})
}

type VersionResponse struct {
	App     string `json:"app"`
	Version string `json:"version"`
}

func versionApiHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	// write (using JSON) the version response
	writeJsonResponse(w, http.StatusOK, VersionResponse{
		App:     "slurpee",
		Version: config.Version,
	})
}
