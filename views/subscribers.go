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
	"github.com/sweater-ventures/slurpee/middleware"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /subscribers", routeHandler(slurpee, subscribersListHandler))
		router.Handle("POST /subscribers", routeHandler(slurpee, subscriberCreateHandler))
		router.Handle("PUT /subscribers/{id}", routeHandler(slurpee, subscriberUpdateHandler))
		router.Handle("POST /subscribers/{id}/subscriptions", routeHandler(slurpee, subscriptionCreateHandler))
		router.Handle("DELETE /subscribers/{id}/subscriptions/{subId}", routeHandler(slurpee, subscriptionDeleteHandler))
		router.Handle("GET /subscribers/{id}", routeHandler(slurpee, subscriberDetailHandler))
	})
}

func subscribersListHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	renderSubscribersPage(slurpee, w, r, "", "")
}

func subscriberCreateHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	endpointURL := r.FormValue("endpoint_url")
	authSecret := r.FormValue("auth_secret")
	maxParallelStr := r.FormValue("max_parallel")

	if name == "" || endpointURL == "" || authSecret == "" {
		renderSubscribersPage(slurpee, w, r, "", "Name, endpoint URL, and auth secret are required")
		return
	}

	maxParallel := int32(1)
	if maxParallelStr != "" {
		val, err := strconv.ParseInt(maxParallelStr, 10, 32)
		if err != nil || val < 1 {
			renderSubscribersPage(slurpee, w, r, "", "Max parallel must be a positive integer")
			return
		}
		maxParallel = int32(val)
	}

	newID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	sub, err := slurpee.DB.UpsertSubscriber(r.Context(), db.UpsertSubscriberParams{
		ID:          newID,
		Name:        name,
		EndpointUrl: endpointURL,
		AuthSecret:  authSecret,
		MaxParallel: maxParallel,
	})
	if err != nil {
		log(r.Context()).Error("Error creating subscriber", "err", err)
		renderSubscribersPage(slurpee, w, r, "", "Failed to create subscriber")
		return
	}

	http.Redirect(w, r, "/subscribers/"+pgtypeUUIDToString(sub.ID), http.StatusSeeOther)
}

func renderSubscribersPage(slurpee *app.Application, w http.ResponseWriter, r *http.Request, successMsg, errorMsg string) {
	subscribers, err := slurpee.DB.ListSubscribersWithCounts(r.Context())
	if err != nil {
		log(r.Context()).Error("Error listing subscribers", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	rows := make([]SubscriberRow, len(subscribers))
	for i, s := range subscribers {
		rows[i] = SubscriberRow{
			ID:                pgtypeUUIDToString(s.ID),
			Name:              s.Name,
			EndpointURL:       s.EndpointUrl,
			MaxParallel:       s.MaxParallel,
			SubscriptionCount: int(s.SubscriptionCount),
			CreatedAt:         s.CreatedAt.Time.Format("2006-01-02 15:04:05 MST"),
		}
	}

	if errorMsg != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	if err := SubscribersListTemplate(rows, successMsg, errorMsg).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscribers list view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func subscriberDetailHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid subscriber ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	detail, subRows, err := buildSubscriberDetailView(slurpee, r, pgID)
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

func subscriberUpdateHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid subscriber ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	if !checkSubscriberAccess(r, pgID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	authSecret := r.FormValue("auth_secret")
	maxParallelStr := r.FormValue("max_parallel")

	if name == "" || authSecret == "" {
		renderSubscriberDetailWithError(slurpee, w, r, pgID, "Name and auth secret are required")
		return
	}

	maxParallel, err := strconv.ParseInt(maxParallelStr, 10, 32)
	if err != nil || maxParallel < 1 {
		renderSubscriberDetailWithError(slurpee, w, r, pgID, "Max parallel must be a positive integer")
		return
	}

	_, err = slurpee.DB.UpdateSubscriber(r.Context(), db.UpdateSubscriberParams{
		ID:          pgID,
		Name:        name,
		AuthSecret:  authSecret,
		MaxParallel: int32(maxParallel),
	})
	if err != nil {
		log(r.Context()).Error("Error updating subscriber", "err", err)
		renderSubscriberDetailWithError(slurpee, w, r, pgID, "Failed to update subscriber")
		return
	}

	detail, subRows, err := buildSubscriberDetailView(slurpee, r, pgID)
	if err != nil {
		return
	}

	if err := subscriberDetailContent(detail, subRows, "Subscriber updated successfully", "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscriber detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func subscriptionCreateHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid subscriber ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	if !checkSubscriberAccess(r, pgID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	subjectPattern := r.FormValue("subject_pattern")
	filterStr := r.FormValue("filter")
	maxRetriesStr := r.FormValue("max_retries")

	if subjectPattern == "" {
		renderSubscriberDetailWithError(slurpee, w, r, pgID, "Subject pattern is required")
		return
	}

	var filter []byte
	if filterStr != "" {
		if !json.Valid([]byte(filterStr)) {
			renderSubscriberDetailWithError(slurpee, w, r, pgID, "Filter must be valid JSON")
			return
		}
		filter = []byte(filterStr)
	}

	var maxRetries pgtype.Int4
	if maxRetriesStr != "" {
		val, err := strconv.ParseInt(maxRetriesStr, 10, 32)
		if err != nil || val < 0 {
			renderSubscriberDetailWithError(slurpee, w, r, pgID, "Max retries must be a non-negative integer")
			return
		}
		maxRetries = pgtype.Int4{Int32: int32(val), Valid: true}
	}

	subID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	_, err = slurpee.DB.CreateSubscription(r.Context(), db.CreateSubscriptionParams{
		ID:             subID,
		SubscriberID:   pgID,
		SubjectPattern: subjectPattern,
		Filter:         filter,
		MaxRetries:     maxRetries,
	})
	if err != nil {
		log(r.Context()).Error("Error creating subscription", "err", err)
		renderSubscriberDetailWithError(slurpee, w, r, pgID, "Failed to create subscription")
		return
	}

	detail, subRows, err := buildSubscriberDetailView(slurpee, r, pgID)
	if err != nil {
		return
	}

	if err := subscriberDetailContent(detail, subRows, "Subscription added successfully", "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscriber detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func subscriptionDeleteHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid subscriber ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	if !checkSubscriberAccess(r, pgID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	subIdStr := r.PathValue("subId")
	subParsed, err := uuid.Parse(subIdStr)
	if err != nil {
		http.Error(w, "Invalid subscription ID", http.StatusBadRequest)
		return
	}
	subPgID := pgtype.UUID{Bytes: subParsed, Valid: true}

	err = slurpee.DB.DeleteSubscription(r.Context(), subPgID)
	if err != nil {
		log(r.Context()).Error("Error deleting subscription", "err", err)
		renderSubscriberDetailWithError(slurpee, w, r, pgID, "Failed to delete subscription")
		return
	}

	detail, subRows, err := buildSubscriberDetailView(slurpee, r, pgID)
	if err != nil {
		return
	}

	if err := subscriberDetailContent(detail, subRows, "Subscription deleted", "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering subscriber detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func buildSubscriberDetailView(slurpee *app.Application, r *http.Request, pgID pgtype.UUID) (SubscriberDetail, []SubscriptionRow, error) {
	subscriber, err := slurpee.DB.GetSubscriberByID(r.Context(), pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SubscriberDetail{}, nil, err
		}
		log(r.Context()).Error("Error fetching subscriber", "err", err)
		return SubscriberDetail{}, nil, err
	}

	subscriptions, err := slurpee.DB.ListSubscriptionsForSubscriber(r.Context(), pgID)
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

func renderSubscriberDetailWithError(slurpee *app.Application, w http.ResponseWriter, r *http.Request, pgID pgtype.UUID, errorMsg string) {
	detail, subRows, err := buildSubscriberDetailView(slurpee, r, pgID)
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

// checkSubscriberAccess returns true if the session is admin or has the subscriber
// in its SubscriberIDs list.
func checkSubscriberAccess(r *http.Request, subscriberID pgtype.UUID) bool {
	session := middleware.GetSessionFromContext(r.Context())
	if session == nil {
		return false
	}
	if session.IsAdmin {
		return true
	}
	for _, sid := range session.SubscriberIDs {
		if sid == subscriberID {
			return true
		}
	}
	return false
}
