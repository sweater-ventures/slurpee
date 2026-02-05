package views

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /subscribers", routeHandler(slurpee, subscribersListHandler))
		router.Handle("PUT /subscribers/{id}", routeHandler(slurpee, subscriberUpdateHandler))
		router.Handle("POST /subscribers/{id}/subscriptions", routeHandler(slurpee, subscriptionCreateHandler))
		router.Handle("DELETE /subscribers/{id}/subscriptions/{subId}", routeHandler(slurpee, subscriptionDeleteHandler))
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

	detail, subRows, err := buildSubscriberDetailView(app, r, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Subscriber not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := SubscriberDetailTemplate(detail, subRows, "", "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscriber detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func subscriberUpdateHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid subscriber ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	authSecret := r.FormValue("auth_secret")
	maxParallelStr := r.FormValue("max_parallel")

	if name == "" || authSecret == "" {
		renderSubscriberDetailWithError(app, w, r, pgID, "Name and auth secret are required")
		return
	}

	maxParallel, err := strconv.ParseInt(maxParallelStr, 10, 32)
	if err != nil || maxParallel < 1 {
		renderSubscriberDetailWithError(app, w, r, pgID, "Max parallel must be a positive integer")
		return
	}

	_, err = app.DB.UpdateSubscriber(r.Context(), db.UpdateSubscriberParams{
		ID:          pgID,
		Name:        name,
		AuthSecret:  authSecret,
		MaxParallel: int32(maxParallel),
	})
	if err != nil {
		log(r.Context()).Error("Error updating subscriber", "err", err)
		renderSubscriberDetailWithError(app, w, r, pgID, "Failed to update subscriber")
		return
	}

	detail, subRows, err := buildSubscriberDetailView(app, r, pgID)
	if err != nil {
		return
	}

	if err := subscriberDetailContent(detail, subRows, "Subscriber updated successfully", "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscriber detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func subscriptionCreateHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid subscriber ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	subjectPattern := r.FormValue("subject_pattern")
	filterStr := r.FormValue("filter")
	maxRetriesStr := r.FormValue("max_retries")

	if subjectPattern == "" {
		renderSubscriberDetailWithError(app, w, r, pgID, "Subject pattern is required")
		return
	}

	var filter []byte
	if filterStr != "" {
		if !json.Valid([]byte(filterStr)) {
			renderSubscriberDetailWithError(app, w, r, pgID, "Filter must be valid JSON")
			return
		}
		filter = []byte(filterStr)
	}

	var maxRetries pgtype.Int4
	if maxRetriesStr != "" {
		val, err := strconv.ParseInt(maxRetriesStr, 10, 32)
		if err != nil || val < 0 {
			renderSubscriberDetailWithError(app, w, r, pgID, "Max retries must be a non-negative integer")
			return
		}
		maxRetries = pgtype.Int4{Int32: int32(val), Valid: true}
	}

	subID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	_, err = app.DB.CreateSubscription(r.Context(), db.CreateSubscriptionParams{
		ID:             subID,
		SubscriberID:   pgID,
		SubjectPattern: subjectPattern,
		Filter:         filter,
		MaxRetries:     maxRetries,
	})
	if err != nil {
		log(r.Context()).Error("Error creating subscription", "err", err)
		renderSubscriberDetailWithError(app, w, r, pgID, "Failed to create subscription")
		return
	}

	detail, subRows, err := buildSubscriberDetailView(app, r, pgID)
	if err != nil {
		return
	}

	if err := subscriberDetailContent(detail, subRows, "Subscription added successfully", "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscriber detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func subscriptionDeleteHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid subscriber ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	subIdStr := r.PathValue("subId")
	subParsed, err := uuid.Parse(subIdStr)
	if err != nil {
		http.Error(w, "Invalid subscription ID", http.StatusBadRequest)
		return
	}
	subPgID := pgtype.UUID{Bytes: subParsed, Valid: true}

	err = app.DB.DeleteSubscription(r.Context(), subPgID)
	if err != nil {
		log(r.Context()).Error("Error deleting subscription", "err", err)
		renderSubscriberDetailWithError(app, w, r, pgID, "Failed to delete subscription")
		return
	}

	detail, subRows, err := buildSubscriberDetailView(app, r, pgID)
	if err != nil {
		return
	}

	if err := subscriberDetailContent(detail, subRows, "Subscription deleted", "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscriber detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func buildSubscriberDetailView(app *app.Application, r *http.Request, pgID pgtype.UUID) (SubscriberDetail, []SubscriptionRow, error) {
	subscriber, err := app.DB.GetSubscriberByID(r.Context(), pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SubscriberDetail{}, nil, err
		}
		log(r.Context()).Error("Error fetching subscriber", "err", err)
		return SubscriberDetail{}, nil, err
	}

	subscriptions, err := app.DB.ListSubscriptionsForSubscriber(r.Context(), pgID)
	if err != nil {
		log(r.Context()).Error("Error fetching subscriptions", "err", err)
		return SubscriberDetail{}, nil, err
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

	return detail, subRows, nil
}

func renderSubscriberDetailWithError(app *app.Application, w http.ResponseWriter, r *http.Request, pgID pgtype.UUID, errorMsg string) {
	detail, subRows, err := buildSubscriberDetailView(app, r, pgID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusBadRequest)
	if err := subscriberDetailContent(detail, subRows, "", errorMsg).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscriber detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}
