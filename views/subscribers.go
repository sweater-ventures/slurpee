package views

import (
	"net/http"

	"github.com/sweater-ventures/slurpee/app"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /subscribers", routeHandler(slurpee, subscribersListHandler))
	})
}

func subscribersListHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	subscribers, err := app.DB.ListSubscribers(r.Context())
	if err != nil {
		log(r.Context()).Error("Error listing subscribers", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	rows := make([]SubscriberRow, len(subscribers))
	for i, s := range subscribers {
		// Count subscriptions for this subscriber
		subscriptions, subErr := app.DB.ListSubscriptionsForSubscriber(r.Context(), s.ID)
		subCount := 0
		if subErr == nil {
			subCount = len(subscriptions)
		}

		rows[i] = SubscriberRow{
			ID:                pgtypeUUIDToString(s.ID),
			Name:              s.Name,
			EndpointURL:       s.EndpointUrl,
			MaxParallel:       s.MaxParallel,
			SubscriptionCount: subCount,
			CreatedAt:         s.CreatedAt.Time.Format("2006-01-02 15:04:05 MST"),
		}
	}

	if err := SubscribersListTemplate(rows).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscribers list view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}
