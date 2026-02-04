package views

import (
	"net/http"

	"github.com/sweater-ventures/slurpee/app"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("/", routeHandler(slurpee, notFound))
	})
}

func notFound(app *app.Application, w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		// forward to overview page
		w.Header().Set("Location", "/welcome")
		w.WriteHeader(http.StatusFound)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	// TODO: make a nice 404 page and show it
}
