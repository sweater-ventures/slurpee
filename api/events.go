package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
		router.Handle("POST /events", routeHandler(slurpee, createEventHandler))
		router.Handle("GET /events/{id}", routeHandler(slurpee, getEventHandler))
	})
}

type CreateEventRequest struct {
	ID      *string         `json:"id"`
	Subject string          `json:"subject"`
	Data    json.RawMessage `json:"data"`
	TraceID *string         `json:"trace_id"`
	// Timestamp is optional — if not provided, defaults to now
	Timestamp *time.Time `json:"timestamp"`
}

type EventResponse struct {
	ID              string          `json:"id"`
	Subject         string          `json:"subject"`
	Timestamp       time.Time       `json:"timestamp"`
	TraceID         *string         `json:"trace_id"`
	Data            json.RawMessage `json:"data"`
	RetryCount      int32           `json:"retry_count"`
	DeliveryStatus  string          `json:"delivery_status"`
	StatusUpdatedAt *time.Time      `json:"status_updated_at"`
}

func LogEvent(ctx context.Context, slurpee *app.Application, event db.Event) {
	logAttrs := []any{"event_id", app.UuidToString(event.ID), "subject", event.Subject}
	props := app.ExtractLogProperties(ctx, slurpee.DB, event.Subject, event.Data)
	for k, v := range props {
		logAttrs = append(logAttrs, k, v)
	}
	log(ctx).Info("Event received", logAttrs...)
}

func createEventHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	// Validate API secret with direct lookup by ID
	secretIDHeader := r.Header.Get("X-Slurpee-Secret-ID")
	if secretIDHeader == "" {
		slog.Warn("Missing API secret ID on POST /api/events", "remote_addr", r.RemoteAddr)
		writeJsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Missing X-Slurpee-Secret-ID header"})
		return
	}
	secretID, err := uuid.Parse(secretIDHeader)
	if err != nil {
		slog.Warn("Invalid API secret ID format on POST /api/events", "remote_addr", r.RemoteAddr, "secret_id", secretIDHeader)
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "X-Slurpee-Secret-ID must be a valid UUID"})
		return
	}
	secretHeader := r.Header.Get("X-Slurpee-Secret")
	if secretHeader == "" {
		slog.Warn("Missing API secret on POST /api/events", "remote_addr", r.RemoteAddr)
		writeJsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Missing or invalid API secret"})
		return
	}
	matchedSecret, err := app.ValidateSecretByID(r.Context(), slurpee.DB, secretID, secretHeader)
	if err != nil {
		slog.Warn("Invalid API secret on POST /api/events", "remote_addr", r.RemoteAddr)
		writeJsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Missing or invalid API secret"})
		return
	}

	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if req.Subject == "" {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "subject is required"})
		return
	}

	// Check subject against secret's subject_pattern
	if !app.CheckSendScope(matchedSecret.SubjectPattern, req.Subject) {
		slog.Warn("Subject not in scope for API secret", "remote_addr", r.RemoteAddr, "subject", req.Subject, "pattern", matchedSecret.SubjectPattern)
		writeJsonResponse(w, http.StatusForbidden, map[string]string{"error": "Subject not permitted by API secret scope"})
		return
	}

	if len(req.Data) == 0 {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "data is required"})
		return
	}

	// Validate that data is a JSON object
	var dataObj map[string]any
	if err := json.Unmarshal(req.Data, &dataObj); err != nil {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "data must be a valid JSON object"})
		return
	}

	// Generate or parse event ID (UUID v7 if not provided)
	var eventID pgtype.UUID
	if req.ID != nil {
		parsed, err := uuid.Parse(*req.ID)
		if err != nil {
			writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "id must be a valid UUID"})
			return
		}
		eventID = pgtype.UUID{Bytes: parsed, Valid: true}
	} else {
		eventID = pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	}

	// Parse or default timestamp
	now := time.Now().UTC()
	ts := now
	if req.Timestamp != nil {
		ts = req.Timestamp.UTC()
	}

	// Parse optional trace_id
	var traceID pgtype.UUID
	if req.TraceID != nil {
		parsed, err := uuid.Parse(*req.TraceID)
		if err != nil {
			writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "trace_id must be a valid UUID"})
			return
		}
		traceID = pgtype.UUID{Bytes: parsed, Valid: true}
	}

	event, err := slurpee.DB.InsertEvent(r.Context(), db.InsertEventParams{
		ID:              eventID,
		Subject:         req.Subject,
		Timestamp:       pgtype.Timestamptz{Time: ts, Valid: true},
		TraceID:         traceID,
		Data:            req.Data,
		RetryCount:      0,
		DeliveryStatus:  "pending",
		StatusUpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		log(r.Context()).Error("Failed to insert event", "error", err)
		writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create event"})
		return
	}

	LogEvent(r.Context(), slurpee, event)
	// Publish 'created' message to the event bus for SSE clients
	props := app.ExtractLogProperties(r.Context(), slurpee.DB, event.Subject, event.Data)
	app.PublishCreatedEvent(slurpee, event, props)
	// Send to delivery dispatcher for asynchronous delivery
	slurpee.DeliveryChan <- event

	writeJsonResponse(w, http.StatusCreated, eventToResponse(event))
}

func getEventHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	// Validate API secret with direct lookup by ID (read-only, any scope)
	secretIDHeader := r.Header.Get("X-Slurpee-Secret-ID")
	if secretIDHeader == "" {
		slog.Warn("Missing API secret ID on GET /api/events/{id}", "remote_addr", r.RemoteAddr)
		writeJsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Missing X-Slurpee-Secret-ID header"})
		return
	}
	secretID, err := uuid.Parse(secretIDHeader)
	if err != nil {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "X-Slurpee-Secret-ID must be a valid UUID"})
		return
	}
	secretHeader := r.Header.Get("X-Slurpee-Secret")
	if secretHeader == "" {
		slog.Warn("Missing API secret on GET /api/events/{id}", "remote_addr", r.RemoteAddr)
		writeJsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Missing or invalid API secret"})
		return
	}
	if _, err := app.ValidateSecretByID(r.Context(), slurpee.DB, secretID, secretHeader); err != nil {
		slog.Warn("Invalid API secret on GET /api/events/{id}", "remote_addr", r.RemoteAddr)
		writeJsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Missing or invalid API secret"})
		return
	}

	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "id must be a valid UUID"})
		return
	}

	event, err := slurpee.DB.GetEventByID(r.Context(), pgtype.UUID{Bytes: parsed, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJsonResponse(w, http.StatusNotFound, map[string]string{"error": "event not found"})
			return
		}
		log(r.Context()).Error("Failed to get event", "error", err)
		writeJsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve event"})
		return
	}

	writeJsonResponse(w, http.StatusOK, eventToResponse(event))
}

func eventToResponse(e db.Event) EventResponse {
	resp := EventResponse{
		ID:             app.UuidToString(e.ID),
		Subject:        e.Subject,
		Timestamp:      e.Timestamp.Time,
		Data:           e.Data,
		RetryCount:     e.RetryCount,
		DeliveryStatus: e.DeliveryStatus,
	}
	if e.TraceID.Valid {
		s := app.UuidToString(e.TraceID)
		resp.TraceID = &s
	}
	if e.StatusUpdatedAt.Valid {
		t := e.StatusUpdatedAt.Time
		resp.StatusUpdatedAt = &t
	}
	return resp
}
