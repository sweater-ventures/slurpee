package views

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /logging", routeHandler(slurpee, loggingListHandler))
		router.Handle("POST /logging", routeHandler(slurpee, loggingCreateHandler))
		router.Handle("PUT /logging", routeHandler(slurpee, loggingUpdateHandler))
		router.Handle("DELETE /logging/{subject}", routeHandler(slurpee, loggingDeleteHandler))
	})
}

func loggingListHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	configs, err := slurpee.DB.ListLogConfigs(r.Context())
	if err != nil {
		log(r.Context()).Error("Error listing log configs", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	rows := buildLogConfigRows(configs)

	if err := LoggingListTemplate(rows, "", "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering logging list view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func loggingCreateHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	subject := strings.TrimSpace(r.FormValue("subject"))
	propertiesStr := strings.TrimSpace(r.FormValue("properties"))

	if subject == "" || propertiesStr == "" {
		renderLoggingWithError(slurpee, w, r, "Subject and properties are required")
		return
	}

	properties := parseProperties(propertiesStr)

	configID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	_, err := slurpee.DB.UpsertLogConfig(r.Context(), db.UpsertLogConfigParams{
		ID:            configID,
		Subject:       subject,
		LogProperties: properties,
	})
	if err != nil {
		log(r.Context()).Error("Error creating log config", "err", err)
		renderLoggingWithError(slurpee, w, r, "Failed to create logging configuration")
		return
	}

	slurpee.LogConfigCache.Flush()
	renderLoggingWithSuccess(slurpee, w, r, "Logging configuration added")
}

func loggingUpdateHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	subject := strings.TrimSpace(r.FormValue("subject"))
	propertiesStr := strings.TrimSpace(r.FormValue("properties"))

	if subject == "" || propertiesStr == "" {
		renderLoggingWithError(slurpee, w, r, "Subject and properties are required")
		return
	}

	properties := parseProperties(propertiesStr)

	// Upsert will update existing config for this subject
	configID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	_, err := slurpee.DB.UpsertLogConfig(r.Context(), db.UpsertLogConfigParams{
		ID:            configID,
		Subject:       subject,
		LogProperties: properties,
	})
	if err != nil {
		log(r.Context()).Error("Error updating log config", "err", err)
		renderLoggingWithError(slurpee, w, r, "Failed to update logging configuration")
		return
	}

	slurpee.LogConfigCache.Flush()
	renderLoggingWithSuccess(slurpee, w, r, "Logging configuration updated")
}

func loggingDeleteHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	if subject == "" {
		renderLoggingWithError(slurpee, w, r, "Subject is required")
		return
	}

	err := slurpee.DB.DeleteLogConfigForSubject(r.Context(), subject)
	if err != nil {
		log(r.Context()).Error("Error deleting log config", "err", err)
		renderLoggingWithError(slurpee, w, r, "Failed to delete logging configuration")
		return
	}

	slurpee.LogConfigCache.Flush()
	renderLoggingWithSuccess(slurpee, w, r, "Logging configuration deleted")
}

func buildLogConfigRows(configs []db.LogConfig) []LogConfigRow {
	rows := make([]LogConfigRow, len(configs))
	for i, c := range configs {
		rows[i] = LogConfigRow{
			Subject:       c.Subject,
			LogProperties: strings.Join(c.LogProperties, ", "),
		}
	}
	return rows
}

func parseProperties(input string) []string {
	parts := strings.Split(input, ",")
	var properties []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			properties = append(properties, trimmed)
		}
	}
	return properties
}

func renderLoggingWithSuccess(slurpee *app.Application, w http.ResponseWriter, r *http.Request, msg string) {
	configs, err := slurpee.DB.ListLogConfigs(r.Context())
	if err != nil {
		log(r.Context()).Error("Error listing log configs", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	rows := buildLogConfigRows(configs)
	if err := loggingContent(rows, msg, "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering logging view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func renderLoggingWithError(slurpee *app.Application, w http.ResponseWriter, r *http.Request, msg string) {
	configs, err := slurpee.DB.ListLogConfigs(r.Context())
	if err != nil {
		log(r.Context()).Error("Error listing log configs", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	rows := buildLogConfigRows(configs)
	w.WriteHeader(http.StatusBadRequest)
	if err := loggingContent(rows, "", msg).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering logging view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}
