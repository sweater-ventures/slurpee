package views

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/middleware"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /secrets", routeHandler(slurpee, secretsListHandler))
		router.Handle("POST /secrets/{id}/delete", routeHandler(slurpee, secretDeleteHandler))
	})
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	session := middleware.GetSessionFromContext(r.Context())
	if session == nil || !session.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func secretsListHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	secrets, err := slurpee.DB.ListApiSecrets(r.Context())
	if err != nil {
		log(r.Context()).Error("Error listing API secrets", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	rows := make([]SecretRow, len(secrets))
	for i, s := range secrets {
		rows[i] = SecretRow{
			ID:              pgtypeUUIDToString(s.ID),
			Name:            s.Name,
			SubjectPattern:  s.SubjectPattern,
			SubscriberNames: s.SubscriberNames,
			CreatedAt:       s.CreatedAt.Time.Format("2006-01-02 15:04:05 MST"),
		}
	}

	if err := SecretsListTemplate(rows, "").Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering secrets list view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func secretDeleteHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid secret ID", http.StatusBadRequest)
		return
	}

	err = slurpee.DB.DeleteApiSecret(r.Context(), pgtype.UUID{Bytes: parsed, Valid: true})
	if err != nil {
		log(r.Context()).Error("Error deleting API secret", "err", err)
		http.Error(w, "Failed to delete secret", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/secrets", http.StatusSeeOther)
}
