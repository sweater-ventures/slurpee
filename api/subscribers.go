package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

func init() {
	registerRoute(func(app *app.Application, router *http.ServeMux) {
		router.Handle("POST /subscribers", routeHandler(app, createSubscriberHandler))
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

func createSubscriberHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	// Verify admin secret
	adminSecret := r.Header.Get("X-Slurpee-Admin-Secret")
	if app.Config.AdminSecret == "" || adminSecret != app.Config.AdminSecret {
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

	maxParallel := int32(1)
	if req.MaxParallel != nil {
		maxParallel = *req.MaxParallel
	}

	// Upsert subscriber
	subscriberID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	subscriber, err := app.DB.UpsertSubscriber(r.Context(), db.UpsertSubscriberParams{
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

	// Delete existing subscriptions and recreate (idempotent replace)
	if err := app.DB.DeleteSubscriptionsForSubscriber(r.Context(), subscriber.ID); err != nil {
		log(r.Context()).Error("Failed to delete existing subscriptions", "error", err)
		writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update subscriptions"})
		return
	}

	var subscriptions []SubscriptionResponse
	for _, sub := range req.Subscriptions {
		subID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}

		var filter []byte
		if len(sub.Filter) > 0 && string(sub.Filter) != "null" {
			filter = sub.Filter
		}

		var maxRetries pgtype.Int4
		if sub.MaxRetries != nil {
			maxRetries = pgtype.Int4{Int32: *sub.MaxRetries, Valid: true}
		}

		created, err := app.DB.CreateSubscription(r.Context(), db.CreateSubscriptionParams{
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

	log(r.Context()).Info("Subscriber registered",
		"subscriber_id", uuidToString(subscriber.ID),
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
		ID:            uuidToString(s.ID),
		Name:          s.Name,
		EndpointURL:   s.EndpointUrl,
		MaxParallel:   s.MaxParallel,
		CreatedAt:     s.CreatedAt.Time,
		UpdatedAt:     s.UpdatedAt.Time,
		Subscriptions: subs,
	}
}

func subscriptionToResponse(s db.Subscription) SubscriptionResponse {
	resp := SubscriptionResponse{
		ID:             uuidToString(s.ID),
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
