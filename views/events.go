package views

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
		router.Handle("GET /events/stream/missed", routeHandler(slurpee, eventsMissedHandler))
		router.Handle("GET /events/stream", routeHandler(slurpee, eventsStreamHandler))
		router.Handle("GET /events", routeHandler(slurpee, eventsListHandler))
		router.Handle("GET /events/{id}", routeHandler(slurpee, eventDetailHandler))
	})
}

const eventsPerPage = 25

type eventFilters struct {
	Subject  string
	Status   string
	DateFrom string
	DateTo   string
	Content  string
	TraceID  string
}

func (f eventFilters) hasAny() bool {
	return f.Subject != "" || f.Status != "" || f.DateFrom != "" || f.DateTo != "" || f.Content != "" || f.TraceID != ""
}

func parseFilters(r *http.Request) eventFilters {
	return eventFilters{
		Subject:  r.URL.Query().Get("subject"),
		Status:   r.URL.Query().Get("status"),
		DateFrom: r.URL.Query().Get("date_from"),
		DateTo:   r.URL.Query().Get("date_to"),
		Content:  r.URL.Query().Get("content"),
		TraceID:  r.URL.Query().Get("trace_id"),
	}
}

func eventsListHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
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

		// Trace ID filter: exact match
		if filters.TraceID != "" {
			params.TraceIDFilter = filters.TraceID
		}

		events, err = slurpee.DB.SearchEventsFiltered(r.Context(), params)
	} else {
		events, err = slurpee.DB.ListEvents(r.Context(), db.ListEventsParams{
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

	allProps := app.BatchExtractLogProperties(r.Context(), slurpee, events)

	rows := make([]EventRow, len(events))
	for i, e := range events {
		rows[i] = EventRow{
			ID:             pgtypeUUIDToString(e.ID),
			Subject:        e.Subject,
			Timestamp:      e.Timestamp.Time.Format("2006-01-02 15:04:05 MST"),
			DeliveryStatus: e.DeliveryStatus,
			Properties:     allProps[i],
		}
	}

	// If this is an HTMX request, return just the table partial
	if r.Header.Get("HX-Request") == "true" {
		err = EventsTablePartial(rows, page, hasNext, filters.Subject, filters.Status, filters.DateFrom, filters.DateTo, filters.Content, filters.TraceID).Render(r.Context(), w)
	} else {
		err = EventsListTemplate(rows, page, hasNext, filters.Subject, filters.Status, filters.DateFrom, filters.DateTo, filters.Content, filters.TraceID).Render(r.Context(), w)
	}
	if err != nil {
		log(r.Context()).Error("Error rendering events list view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func eventDetailHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	event, err := slurpee.DB.GetEventByID(r.Context(), pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Event not found", http.StatusNotFound)
			return
		}
		log(r.Context()).Error("Error fetching event", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	attempts, err := slurpee.DB.ListDeliveryAttemptsForEvent(r.Context(), pgID)
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

func eventReplayAllHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	event, err := slurpee.DB.GetEventByID(r.Context(), pgID)
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
	slurpee.DeliveryChan <- event

	// Re-fetch the event and delivery attempts for the updated view
	event, _ = slurpee.DB.GetEventByID(r.Context(), pgID)
	attempts, _ := slurpee.DB.ListDeliveryAttemptsForEvent(r.Context(), pgID)

	detail, attemptRows := buildEventDetailView(event, attempts)
	if err := deliveryAttemptsSection(detail, attemptRows).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering delivery section", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func eventReplaySubscriberHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
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

	event, err := slurpee.DB.GetEventByID(r.Context(), pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Event not found", http.StatusNotFound)
			return
		}
		log(r.Context()).Error("Error fetching event for replay", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	subscriber, err := slurpee.DB.GetSubscriberByID(r.Context(), subscriberPgID)
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
	go app.ReplayToSubscriber(slurpee, event, subscriber)

	// Re-fetch the event and delivery attempts for the updated view
	event, _ = slurpee.DB.GetEventByID(r.Context(), pgID)
	attempts, _ := slurpee.DB.ListDeliveryAttemptsForEvent(r.Context(), pgID)

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

func eventCreateFormHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	form := EventCreateForm{Errors: map[string]string{}}
	if err := EventCreateTemplate(form).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering event create view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func eventCreateSubmitHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
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

	event, err := slurpee.DB.InsertEvent(r.Context(), db.InsertEventParams{
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
	api.LogEvent(r.Context(), slurpee, event)
	// Publish 'created' message to the event bus for SSE clients
	props := app.ExtractLogProperties(r.Context(), slurpee, event.Subject, event.Data)
	app.PublishCreatedEvent(slurpee, event, props)
	// Trigger async delivery
	slurpee.DeliveryChan <- event

	// Redirect to the newly created event detail page
	http.Redirect(w, r, "/events/"+pgtypeUUIDToString(event.ID), http.StatusSeeOther)
}

func eventsMissedHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	afterStr := r.URL.Query().Get("after")
	if afterStr == "" {
		http.Error(w, "Missing 'after' parameter", http.StatusBadRequest)
		return
	}
	nanos, err := strconv.ParseInt(afterStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid 'after' parameter", http.StatusBadRequest)
		return
	}
	ts := time.Unix(0, nanos).UTC()

	filters := parseFilters(r)
	params := db.ListEventsAfterTimestampParams{
		AfterTimestamp: pgtype.Timestamptz{Time: ts, Valid: true},
	}
	if filters.Subject != "" {
		params.SubjectFilter = "%" + filters.Subject + "%"
	}
	if filters.Status != "" {
		params.StatusFilter = filters.Status
	}
	if filters.Content != "" && json.Valid([]byte(filters.Content)) {
		params.DataFilter = []byte(filters.Content)
	}
	if filters.TraceID != "" {
		params.TraceIDFilter = filters.TraceID
	}

	events, err := slurpee.DB.ListEventsAfterTimestamp(r.Context(), params)
	if err != nil {
		log(r.Context()).Error("Error fetching missed events", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	allProps := app.BatchExtractLogProperties(r.Context(), slurpee, events)

	rows := make([]EventRow, len(events))
	for i, e := range events {
		rows[i] = EventRow{
			ID:             pgtypeUUIDToString(e.ID),
			Subject:        e.Subject,
			Timestamp:      e.Timestamp.Time.Format("2006-01-02 15:04:05 MST"),
			DeliveryStatus: e.DeliveryStatus,
			Properties:     allProps[i],
		}
	}

	if err := MissedEventsRows(rows).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering missed events rows", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func eventsStreamHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Parse filters from query parameters
	filters := parseFilters(r)

	// Subscribe to the EventBus
	ch, unsubscribe := slurpee.EventBus.Subscribe()
	defer unsubscribe()

	// Handle Last-Event-ID reconnection
	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
		// ID format: <monotonic_id>:<unix_nano>
		parts := strings.SplitN(lastID, ":", 2)
		if len(parts) == 2 {
			if nanos, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				ts := time.Unix(0, nanos).UTC()
				params := db.CountEventsAfterTimestampParams{
					AfterTimestamp: pgtype.Timestamptz{Time: ts, Valid: true},
				}
				if filters.Subject != "" {
					params.SubjectFilter = "%" + filters.Subject + "%"
				}
				if filters.Status != "" {
					params.StatusFilter = filters.Status
				}
				if filters.Content != "" && json.Valid([]byte(filters.Content)) {
					params.DataFilter = []byte(filters.Content)
				}
				if filters.TraceID != "" {
					params.TraceIDFilter = filters.TraceID
				}
				count, err := slurpee.DB.CountEventsAfterTimestamp(r.Context(), params)
				if err == nil && count > 0 {
					missedData, _ := json.Marshal(map[string]int64{"count": count})
					fmt.Fprintf(w, "event: missed\ndata: %s\n\n", missedData)
					flusher.Flush()
				}
			}
		}
	}

	// Keepalive ticker
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if !matchesStreamFilters(msg, filters) {
				continue
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			sseID := fmt.Sprintf("%d:%d", msg.ID, msg.Timestamp.UnixNano())
			fmt.Fprintf(w, "id:%s\ndata:%s\n\n", sseID, data)
			flusher.Flush()
		}
	}
}

// matchesStreamFilters checks if a bus message passes the active stream filters.
func matchesStreamFilters(msg app.BusMessage, filters eventFilters) bool {
	if filters.Subject != "" {
		if !strings.Contains(strings.ToLower(msg.Subject), strings.ToLower(filters.Subject)) {
			return false
		}
	}
	if filters.Status != "" {
		if msg.DeliveryStatus != filters.Status {
			return false
		}
	}
	if filters.DateFrom != "" {
		t, err := time.Parse("2006-01-02", filters.DateFrom)
		if err == nil && msg.Timestamp.Before(t) {
			return false
		}
	}
	if filters.DateTo != "" {
		t, err := time.Parse("2006-01-02", filters.DateTo)
		if err == nil && msg.Timestamp.After(t.Add(24*time.Hour-time.Second)) {
			return false
		}
	}
	// Content filter: not applicable to bus messages (they don't carry full event data)
	// Trace ID filter: not applicable to bus messages (they don't carry trace_id)
	return true
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
