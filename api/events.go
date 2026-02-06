package api

import (
	"context"
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
	registerRoute(func(app *app.Application, router *http.ServeMux) {
		router.Handle("POST /events", routeHandler(app, createEventHandler))
		router.Handle("GET /events/{id}", routeHandler(app, getEventHandler))
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
	// Log event with configured data properties
	var dataObj map[string]any
	if err := json.Unmarshal(event.Data, &dataObj); err != nil {
		log(ctx).Error("Failed to unmarshal event data for logging", "error", err)
		return
	}
	logAttrs := []any{"event_id", uuidToString(event.ID), "subject", event.Subject}
	logConfig, err := slurpee.DB.GetLogConfigBySubject(ctx, event.Subject)
	if err == nil {
		for _, prop := range logConfig.LogProperties {
			if val, ok := dataObj[prop]; ok {
				logAttrs = append(logAttrs, prop, val)
			}
		}
	}
	log(ctx).Info("Event received", logAttrs...)
}

func createEventHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if req.Subject == "" {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "subject is required"})
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

	event, err := app.DB.InsertEvent(r.Context(), db.InsertEventParams{
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

	LogEvent(r.Context(), app, event)
	// Send to delivery dispatcher for asynchronous delivery
	app.DeliveryChan <- event

	writeJsonResponse(w, http.StatusCreated, eventToResponse(event))
}

func getEventHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		writeJsonResponse(w, http.StatusBadRequest, map[string]string{"error": "id must be a valid UUID"})
		return
	}

	event, err := app.DB.GetEventByID(r.Context(), pgtype.UUID{Bytes: parsed, Valid: true})
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
		ID:             uuidToString(e.ID),
		Subject:        e.Subject,
		Timestamp:      e.Timestamp.Time,
		Data:           e.Data,
		RetryCount:     e.RetryCount,
		DeliveryStatus: e.DeliveryStatus,
	}
	if e.TraceID.Valid {
		s := uuidToString(e.TraceID)
		resp.TraceID = &s
	}
	if e.StatusUpdatedAt.Valid {
		t := e.StatusUpdatedAt.Time
		resp.StatusUpdatedAt = &t
	}
	return resp
}

func uuidToString(u pgtype.UUID) string {
	return uuid.UUID(u.Bytes).String()
}
