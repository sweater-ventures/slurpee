package views

import (
	"net/http"

	"github.com/sweater-ventures/slurpee/app"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("/welcome", routeHandler(slurpee, WelcomeHandler))
	})
}

func WelcomeHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	err := WelcomeTemplate().Render(r.Context(), w)
	if err != nil {
		log(r.Context()).Error("Error rendering welcome view: ", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}
