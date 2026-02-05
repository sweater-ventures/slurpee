package views

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /subscribers", routeHandler(slurpee, subscribersListHandler))
		router.Handle("GET /subscribers/{id}", routeHandler(slurpee, subscriberDetailHandler))
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

func subscriberDetailHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid subscriber ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	subscriber, err := app.DB.GetSubscriberByID(r.Context(), pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Subscriber not found", http.StatusNotFound)
			return
		}
		log(r.Context()).Error("Error fetching subscriber", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	subscriptions, err := app.DB.ListSubscriptionsForSubscriber(r.Context(), pgID)
	if err != nil {
		log(r.Context()).Error("Error fetching subscriptions", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	detail := SubscriberDetail{
		ID:          pgtypeUUIDToString(subscriber.ID),
		Name:        subscriber.Name,
		EndpointURL: subscriber.EndpointUrl,
		AuthSecret:  subscriber.AuthSecret,
		MaxParallel: subscriber.MaxParallel,
		CreatedAt:   subscriber.CreatedAt.Time.Format("2006-01-02 15:04:05 MST"),
		UpdatedAt:   subscriber.UpdatedAt.Time.Format("2006-01-02 15:04:05 MST"),
	}

	subRows := make([]SubscriptionRow, len(subscriptions))
	for i, s := range subscriptions {
		row := SubscriptionRow{
			ID:             pgtypeUUIDToString(s.ID),
			SubjectPattern: s.SubjectPattern,
		}
		if len(s.Filter) > 0 && string(s.Filter) != "null" {
			row.Filter = prettyJSON(s.Filter)
		}
		if s.MaxRetries.Valid {
			row.MaxRetries = fmt.Sprintf("%d", s.MaxRetries.Int32)
		}
		subRows[i] = row
	}

	if err := SubscriberDetailTemplate(detail, subRows).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscriber detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}
