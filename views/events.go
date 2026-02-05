package views

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /events", routeHandler(slurpee, eventsListHandler))
	})
}

const eventsPerPage = 50

func eventsListHandler(app *app.Application, w http.ResponseWriter, r *http.Request) {
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		parsed, err := strconv.Atoi(p)
		if err == nil && parsed > 0 {
			page = parsed
		}
	}

	offset := int32((page - 1) * eventsPerPage)
	// Fetch one extra to determine if there's a next page
	events, err := app.DB.ListEvents(r.Context(), db.ListEventsParams{
		Limit:  eventsPerPage + 1,
		Offset: offset,
	})
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

	err = EventsListTemplate(rows, page, hasNext).Render(r.Context(), w)
	if err != nil {
		log(r.Context()).Error("Error rendering events list view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func pgtypeUUIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}
