package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("POST /subscribers", routeHandler(slurpee, createSubscriberHandler))
		router.Handle("GET /subscribers", routeHandler(slurpee, listSubscribersHandler))
		router.Handle("DELETE /subscribers/{id}", routeHandler(slurpee, deleteSubscriberHandler))
	})
}

type SubscriptionRequest struct {
	SubjectPattern string          `json:"subject_pattern"`
	Filter         json.RawMessage `json:"filter"`
	MaxRetries     *int32          `json:"max_retries"`
}

type CreateSubscriberRequest struct {
	Name          string                `json:"name"`
	EndpointURL   string                `json:"endpoint_url"`
	AuthSecret    string                `json:"auth_secret"`
	MaxParallel   *int32                `json:"max_parallel"`
	Subscriptions []SubscriptionRequest `json:"subscriptions"`
}

type SubscriptionResponse struct {
	ID             string          `json:"id"`
	SubjectPattern string          `json:"subject_pattern"`
	Filter         json.RawMessage `json:"filter"`
	MaxRetries     *int32          `json:"max_retries"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type SubscriberResponse struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	EndpointURL   string                 `json:"endpoint_url"`
	MaxParallel   int32                  `json:"max_parallel"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	Subscriptions []SubscriptionResponse `json:"subscriptions"`
}

func createSubscriberHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	// Verify admin secret
	adminSecret := r.Header.Get("X-Slurpee-Admin-Secret")
	if slurpee.Config.AdminSecret == "" || adminSecret != slurpee.Config.AdminSecret {
		writeJsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or missing admin secret"})
		return
	}

	var req CreateSubscriberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if req.Name == "" {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if req.EndpointURL == "" {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "endpoint_url is required"})
		return
	}
	if req.AuthSecret == "" {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "auth_secret is required"})
		return
	}
	if req.Subscriptions == nil {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "subscriptions is required"})
		return
	}

	// Validate subscriptions
	for _, sub := range req.Subscriptions {
		if sub.SubjectPattern == "" {
			writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "subject_pattern is required for each subscription"})
			return
		}
	}

	maxParallel := int32(slurpee.Config.MaxParallel)
	if req.MaxParallel != nil {
		maxParallel = *req.MaxParallel
	}

	// Upsert subscriber
	subscriberID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	subscriber, err := slurpee.DB.UpsertSubscriber(r.Context(), db.UpsertSubscriberParams{
		ID:          subscriberID,
		Name:        req.Name,
		EndpointUrl: req.EndpointURL,
		AuthSecret:  req.AuthSecret,
		MaxParallel: maxParallel,
	})
	if err != nil {
		log(r.Context()).Error("Failed to upsert subscriber", "error", err)
		writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create/update subscriber"})
		return
	}

	// Sync subscriptions: add new, update existing, delete removed
	existing, err := slurpee.DB.ListSubscriptionsForSubscriber(r.Context(), subscriber.ID)
	if err != nil {
		log(r.Context()).Error("Failed to list existing subscriptions", "error", err)
		writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update subscriptions"})
		return
	}

	existingByPattern := make(map[string]db.Subscription, len(existing))
	for _, s := range existing {
		existingByPattern[s.SubjectPattern] = s
	}

	incomingPatterns := make(map[string]struct{}, len(req.Subscriptions))

	var subscriptions []SubscriptionResponse
	for _, sub := range req.Subscriptions {
		incomingPatterns[sub.SubjectPattern] = struct{}{}

		var filter []byte
		if len(sub.Filter) > 0 && string(sub.Filter) != "null" {
			filter = sub.Filter
		}

		var maxRetries pgtype.Int4
		if sub.MaxRetries != nil {
			maxRetries = pgtype.Int4{Int32: *sub.MaxRetries, Valid: true}
		}

		if existingSub, ok := existingByPattern[sub.SubjectPattern]; ok {
			// Update existing subscription in place
			updated, err := slurpee.DB.UpdateSubscription(r.Context(), db.UpdateSubscriptionParams{
				ID:         existingSub.ID,
				Filter:     filter,
				MaxRetries: maxRetries,
			})
			if err != nil {
				log(r.Context()).Error("Failed to update subscription", "error", err, "subject_pattern", sub.SubjectPattern)
				writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update subscription"})
				return
			}
			subscriptions = append(subscriptions, subscriptionToResponse(updated))
		} else {
			// Create new subscription
			subID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
			created, err := slurpee.DB.CreateSubscription(r.Context(), db.CreateSubscriptionParams{
				ID:             subID,
				SubscriberID:   subscriber.ID,
				SubjectPattern: sub.SubjectPattern,
				Filter:         filter,
				MaxRetries:     maxRetries,
			})
			if err != nil {
				log(r.Context()).Error("Failed to create subscription", "error", err, "subject_pattern", sub.SubjectPattern)
				writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create subscription"})
				return
			}
			subscriptions = append(subscriptions, subscriptionToResponse(created))
		}
	}

	// Delete subscriptions no longer in the incoming set
	for _, s := range existing {
		if _, ok := incomingPatterns[s.SubjectPattern]; !ok {
			if err := slurpee.DB.DeleteSubscription(r.Context(), s.ID); err != nil {
				log(r.Context()).Error("Failed to delete subscription", "error", err, "subject_pattern", s.SubjectPattern)
				writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete subscription"})
				return
			}
		}
	}

	log(r.Context()).Info("Subscriber registered",
		"subscriber_id", app.UuidToString(subscriber.ID),
		"name", subscriber.Name,
		"endpoint_url", subscriber.EndpointUrl,
		"subscription_count", len(subscriptions),
	)

	resp := subscriberToResponse(subscriber, subscriptions)
	writeJsonResponse(w, http.StatusOK, resp)
}

func subscriberToResponse(s db.Subscriber, subs []SubscriptionResponse) SubscriberResponse {
	if subs == nil {
		subs = []SubscriptionResponse{}
	}
	return SubscriberResponse{
		ID:            app.UuidToString(s.ID),
		Name:          s.Name,
		EndpointURL:   s.EndpointUrl,
		MaxParallel:   s.MaxParallel,
		CreatedAt:     s.CreatedAt.Time,
		UpdatedAt:     s.UpdatedAt.Time,
		Subscriptions: subs,
	}
}

func listSubscribersHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	// Verify admin secret
	adminSecret := r.Header.Get("X-Slurpee-Admin-Secret")
	if slurpee.Config.AdminSecret == "" || adminSecret != slurpee.Config.AdminSecret {
		writeJsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or missing admin secret"})
		return
	}

	subscribers, err := slurpee.DB.ListSubscribers(r.Context())
	if err != nil {
		log(r.Context()).Error("Failed to list subscribers", "error", err)
		writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list subscribers"})
		return
	}

	var response []SubscriberResponse
	for _, sub := range subscribers {
		subs, err := slurpee.DB.ListSubscriptionsForSubscriber(r.Context(), sub.ID)
		if err != nil {
			log(r.Context()).Error("Failed to list subscriptions for subscriber", "error", err, "subscriber_id", app.UuidToString(sub.ID))
			writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list subscriptions"})
			return
		}

		var subResponses []SubscriptionResponse
		for _, s := range subs {
			subResponses = append(subResponses, subscriptionToResponse(s))
		}

		response = append(response, subscriberToResponse(sub, subResponses))
	}

	if response == nil {
		response = []SubscriberResponse{}
	}

	writeJsonResponse(w, http.StatusOK, response)
}

func deleteSubscriberHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	// Verify admin secret
	adminSecret := r.Header.Get("X-Slurpee-Admin-Secret")
	if slurpee.Config.AdminSecret == "" || adminSecret != slurpee.Config.AdminSecret {
		writeJsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or missing admin secret"})
		return
	}

	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "id must be a valid UUID"})
		return
	}

	subscriberID := pgtype.UUID{Bytes: parsed, Valid: true}

	// Verify subscriber exists
	_, err = slurpee.DB.GetSubscriberByID(r.Context(), subscriberID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJsonResponse(w, http.StatusNotFound, map[string]string{"error": "subscriber not found"})
			return
		}
		log(r.Context()).Error("Failed to get subscriber", "error", err)
		writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete subscriber"})
		return
	}

	// Delete subscriptions first, then the subscriber
	if err := slurpee.DB.DeleteSubscriptionsForSubscriber(r.Context(), subscriberID); err != nil {
		log(r.Context()).Error("Failed to delete subscriptions for subscriber", "error", err)
		writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete subscriber"})
		return
	}

	if err := slurpee.DB.DeleteSubscriber(r.Context(), subscriberID); err != nil {
		log(r.Context()).Error("Failed to delete subscriber", "error", err)
		writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete subscriber"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func subscriptionToResponse(s db.Subscription) SubscriptionResponse {
	resp := SubscriptionResponse{
		ID:             app.UuidToString(s.ID),
		SubjectPattern: s.SubjectPattern,
		CreatedAt:      s.CreatedAt.Time,
		UpdatedAt:      s.UpdatedAt.Time,
	}
	if len(s.Filter) > 0 {
		resp.Filter = s.Filter
	}
	if s.MaxRetries.Valid {
		v := s.MaxRetries.Int32
		resp.MaxRetries = &v
	}
	return resp
}
