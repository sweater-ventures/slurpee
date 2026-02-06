package views

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/api"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /events/new", routeHandler(slurpee, eventCreateFormHandler))
		router.Handle("POST /events/new", routeHandler(slurpee, eventCreateSubmitHandler))
		router.Handle("POST /events/{id}/replay", routeHandler(slurpee, eventReplayAllHandler))
		router.Handle("POST /events/{id}/replay/{subscriberId}", routeHandler(slurpee, eventReplaySubscriberHandler))
		router.Handle("GET /events", routeHandler(slurpee, eventsListHandler))
		router.Handle("GET /events/{id}", routeHandler(slurpee, eventDetailHandler))
	})
}

const eventsPerPage = 50

type eventFilters struct {
	Subject  string
	Status   string
	DateFrom string
	DateTo   string
	Content  string
}

func (f eventFilters) hasAny() bool {
	return f.Subject != "" || f.Status != "" || f.DateFrom != "" || f.DateTo != "" || f.Content != ""
}

func parseFilters(r *http.Request) eventFilters {
	return eventFilters{
		Subject:  r.URL.Query().Get("subject"),
		Status:   r.URL.Query().Get("status"),
		DateFrom: r.URL.Query().Get("date_from"),
		DateTo:   r.URL.Query().Get("date_to"),
		Content:  r.URL.Query().Get("content"),
	}
}

func eventsListHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		parsed, err := strconv.Atoi(p)
		if err == nil && parsed > 0 {
			page = parsed
		}
	}

	filters := parseFilters(r)
	offset := int32((page - 1) * eventsPerPage)

	var events []db.Event
	var err error

	if filters.hasAny() {
		params := db.SearchEventsFilteredParams{
			Limit:  eventsPerPage + 1,
			Offset: offset,
		}

		// Subject filter: use LIKE with % wildcards for partial matching
		if filters.Subject != "" {
			params.SubjectFilter = "%" + filters.Subject + "%"
		}

		// Delivery status filter: exact match
		if filters.Status != "" {
			params.StatusFilter = filters.Status
		}

		// Date range filters
		if filters.DateFrom != "" {
			t, parseErr := time.Parse("2006-01-02", filters.DateFrom)
			if parseErr == nil {
				params.StartTimeFilter = pgtype.Timestamptz{Time: t, Valid: true}
			}
		}
		if filters.DateTo != "" {
			t, parseErr := time.Parse("2006-01-02", filters.DateTo)
			if parseErr == nil {
				// End of day
				params.EndTimeFilter = pgtype.Timestamptz{Time: t.Add(24*time.Hour - time.Second), Valid: true}
			}
		}

		// Content search: build a JSON containment query
		if filters.Content != "" {
			// Try to parse as JSON first
			if json.Valid([]byte(filters.Content)) {
				params.DataFilter = []byte(filters.Content)
			}
		}

		events, err = app.DB.SearchEventsFiltered(r.Context(), params)
	} else {
		events, err = app.DB.ListEvents(r.Context(), db.ListEventsParams{
			Limit:  eventsPerPage + 1,
			Offset: offset,
		})
	}

	if err != nil {
		log(r.Context()).Error("Error listing events", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	hasNext := len(events) > eventsPerPage
	if hasNext {
		events = events[:eventsPerPage]
	}

	rows := make([]EventRow, len(events))
	for i, e := range events {
		rows[i] = EventRow{
			ID:             pgtypeUUIDToString(e.ID),
			Subject:        e.Subject,
			Timestamp:      e.Timestamp.Time.Format("2006-01-02 15:04:05 MST"),
			DeliveryStatus: e.DeliveryStatus,
		}
	}

	// If this is an HTMX request, return just the table partial
	if r.Header.Get("HX-Request") == "true" {
		err = EventsTablePartial(rows, page, hasNext, filters.Subject, filters.Status, filters.DateFrom, filters.DateTo, filters.Content).Render(r.Context(), w)
	} else {
		err = EventsListTemplate(rows, page, hasNext, filters.Subject, filters.Status, filters.DateFrom, filters.DateTo, filters.Content).Render(r.Context(), w)
	}
	if err != nil {
		log(r.Context()).Error("Error rendering events list view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func eventDetailHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	event, err := app.DB.GetEventByID(r.Context(), pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Event not found", http.StatusNotFound)
			return
		}
		log(r.Context()).Error("Error fetching event", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	attempts, err := app.DB.ListDeliveryAttemptsForEvent(r.Context(), pgID)
	if err != nil {
		log(r.Context()).Error("Error fetching delivery attempts", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	detail, attemptRows := buildEventDetailView(event, attempts)

	if err := EventDetailTemplate(detail, attemptRows).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering event detail view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func eventReplayAllHandler(a *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	event, err := a.DB.GetEventByID(r.Context(), pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Event not found", http.StatusNotFound)
			return
		}
		log(r.Context()).Error("Error fetching event for replay", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Reset event status to pending and send to delivery channel
	a.DeliveryChan <- event

	// Re-fetch the event and delivery attempts for the updated view
	event, _ = a.DB.GetEventByID(r.Context(), pgID)
	attempts, _ := a.DB.ListDeliveryAttemptsForEvent(r.Context(), pgID)

	detail, attemptRows := buildEventDetailView(event, attempts)
	if err := deliveryAttemptsSection(detail, attemptRows).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering delivery section", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func eventReplaySubscriberHandler(a *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	subscriberIDStr := r.PathValue("subscriberId")
	subscriberParsed, err := uuid.Parse(subscriberIDStr)
	if err != nil {
		http.Error(w, "Invalid subscriber ID", http.StatusBadRequest)
		return
	}
	subscriberPgID := pgtype.UUID{Bytes: subscriberParsed, Valid: true}

	event, err := a.DB.GetEventByID(r.Context(), pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Event not found", http.StatusNotFound)
			return
		}
		log(r.Context()).Error("Error fetching event for replay", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	subscriber, err := a.DB.GetSubscriberByID(r.Context(), subscriberPgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Subscriber not found", http.StatusNotFound)
			return
		}
		log(r.Context()).Error("Error fetching subscriber for replay", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Replay delivery to the single subscriber in a background goroutine
	go api.ReplayToSubscriber(a, event, subscriber)

	// Re-fetch the event and delivery attempts for the updated view
	event, _ = a.DB.GetEventByID(r.Context(), pgID)
	attempts, _ := a.DB.ListDeliveryAttemptsForEvent(r.Context(), pgID)

	detail, attemptRows := buildEventDetailView(event, attempts)
	if err := deliveryAttemptsSection(detail, attemptRows).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering delivery section", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func buildEventDetailView(event db.Event, attempts []db.DeliveryAttempt) (EventDetail, []DeliveryAttemptRow) {
	var prettyData string
	var rawData interface{}
	if err := json.Unmarshal(event.Data, &rawData); err == nil {
		pretty, err := json.MarshalIndent(rawData, "", "  ")
		if err == nil {
			prettyData = string(pretty)
		} else {
			prettyData = string(event.Data)
		}
	} else {
		prettyData = string(event.Data)
	}

	detail := EventDetail{
		ID:             pgtypeUUIDToString(event.ID),
		Subject:        event.Subject,
		Timestamp:      event.Timestamp.Time.Format("2006-01-02 15:04:05 MST"),
		TraceID:        pgtypeUUIDToString(event.TraceID),
		DeliveryStatus: event.DeliveryStatus,
		RetryCount:     event.RetryCount,
		DataJSON:       prettyData,
	}
	if event.StatusUpdatedAt.Valid {
		detail.StatusUpdatedAt = event.StatusUpdatedAt.Time.Format("2006-01-02 15:04:05 MST")
	}

	attemptRows := make([]DeliveryAttemptRow, len(attempts))
	for i, a := range attempts {
		row := DeliveryAttemptRow{
			ID:           pgtypeUUIDToString(a.ID),
			SubscriberID: pgtypeUUIDToString(a.SubscriberID),
			EndpointURL:  a.EndpointUrl,
			AttemptedAt:  a.AttemptedAt.Time.Format("2006-01-02 15:04:05 MST"),
			Status:       a.Status,
		}
		if a.ResponseStatusCode.Valid {
			row.ResponseStatusCode = fmt.Sprintf("%d", a.ResponseStatusCode.Int32)
		}
		if len(a.RequestHeaders) > 0 {
			row.RequestHeaders = prettyJSON(a.RequestHeaders)
		}
		if len(a.ResponseHeaders) > 0 {
			row.ResponseHeaders = prettyJSON(a.ResponseHeaders)
		}
		if a.ResponseBody != "" {
			row.ResponseBody = a.ResponseBody
		}
		attemptRows[i] = row
	}

	return detail, attemptRows
}

func eventCreateFormHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	form := EventCreateForm{Errors: map[string]string{}}
	if err := EventCreateTemplate(form).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering event create view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func eventCreateSubmitHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	subject := r.FormValue("subject")
	data := r.FormValue("data")
	traceID := r.FormValue("trace_id")

	form := EventCreateForm{
		Subject: subject,
		Data:    data,
		TraceID: traceID,
		Errors:  map[string]string{},
	}

	// Validate required fields
	if subject == "" {
		form.Errors["subject"] = "Subject is required"
	}
	if data == "" {
		form.Errors["data"] = "Data is required"
	} else if !json.Valid([]byte(data)) {
		form.Errors["data"] = "Data must be valid JSON"
	} else {
		// Validate it's a JSON object
		var obj map[string]any
		if err := json.Unmarshal([]byte(data), &obj); err != nil {
			form.Errors["data"] = "Data must be a JSON object (not array or primitive)"
		}
	}

	// Validate optional trace_id
	var traceUUID pgtype.UUID
	if traceID != "" {
		parsed, err := uuid.Parse(traceID)
		if err != nil {
			form.Errors["trace_id"] = "Trace ID must be a valid UUID"
		} else {
			traceUUID = pgtype.UUID{Bytes: parsed, Valid: true}
		}
	}

	// If there are validation errors, re-render the form
	if len(form.Errors) > 0 {
		w.WriteHeader(http.StatusBadRequest)
		if err := EventCreateTemplate(form).Render(r.Context(), w); err != nil {
			log(r.Context()).Error("Error rendering event create view", "err", err)
		}
		return
	}

	// Create the event
	now := time.Now().UTC()
	eventID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}

	event, err := app.DB.InsertEvent(r.Context(), db.InsertEventParams{
		ID:              eventID,
		Subject:         subject,
		Timestamp:       pgtype.Timestamptz{Time: now, Valid: true},
		TraceID:         traceUUID,
		Data:            json.RawMessage(data),
		RetryCount:      0,
		DeliveryStatus:  "pending",
		StatusUpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		log(r.Context()).Error("Error creating event", "err", err)
		form.Errors["general"] = "Failed to create event. Please try again."
		w.WriteHeader(http.StatusInternalServerError)
		if renderErr := EventCreateTemplate(form).Render(r.Context(), w); renderErr != nil {
			log(r.Context()).Error("Error rendering event create view", "err", renderErr)
		}
		return
	}
	api.LogEvent(r.Context(), app, event)
	// Trigger async delivery
	app.DeliveryChan <- event

	// Redirect to the newly created event detail page
	http.Redirect(w, r, "/events/"+pgtypeUUIDToString(event.ID), http.StatusSeeOther)
}

func prettyJSON(data []byte) string {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err == nil {
		pretty, err := json.MarshalIndent(raw, "", "  ")
		if err == nil {
			return string(pretty)
		}
	}
	return string(data)
}

func pgtypeUUIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}
