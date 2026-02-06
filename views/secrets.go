package views

import (
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
	"github.com/sweater-ventures/slurpee/middleware"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /secrets", routeHandler(slurpee, secretsListHandler))
		router.Handle("POST /secrets", routeHandler(slurpee, secretCreateHandler))
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

func loadSubscriberOptions(slurpee *app.Application, r *http.Request) ([]SubscriberOption, error) {
	subscribers, err := slurpee.DB.ListSubscribers(r.Context())
	if err != nil {
		return nil, err
	}
	options := make([]SubscriberOption, len(subscribers))
	for i, s := range subscribers {
		options[i] = SubscriberOption{
			ID:          pgtypeUUIDToString(s.ID),
			Name:        s.Name,
			EndpointURL: s.EndpointUrl,
		}
	}
	return options, nil
}

func renderSecretsPage(slurpee *app.Application, w http.ResponseWriter, r *http.Request, successMsg, errorMsg, plaintextSecret string) {
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

	subOptions, err := loadSubscriberOptions(slurpee, r)
	if err != nil {
		log(r.Context()).Error("Error listing subscribers", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := SecretsListTemplate(rows, subOptions, successMsg, errorMsg, plaintextSecret).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering secrets list view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func secretsListHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	renderSecretsPage(slurpee, w, r, "", "", "")
}

func secretCreateHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	if err := r.ParseForm(); err != nil {
		renderSecretsPage(slurpee, w, r, "", "Invalid form data", "")
		return
	}

	name := r.FormValue("name")
	subjectPattern := r.FormValue("subject_pattern")
	subscriberIDs := r.Form["subscriber_ids"]

	if name == "" || subjectPattern == "" {
		renderSecretsPage(slurpee, w, r, "", "Name and subject pattern are required", "")
		return
	}

	// Validate host:port constraint: all selected subscribers must share the same host:port
	if len(subscriberIDs) > 1 {
		subscribers, err := slurpee.DB.ListSubscribers(r.Context())
		if err != nil {
			log(r.Context()).Error("Error listing subscribers for validation", "err", err)
			renderSecretsPage(slurpee, w, r, "", "Internal error", "")
			return
		}
		subMap := make(map[string]string) // id -> endpoint_url
		for _, s := range subscribers {
			subMap[pgtypeUUIDToString(s.ID)] = s.EndpointUrl
		}

		var hostPort string
		for _, sid := range subscriberIDs {
			endpointURL, ok := subMap[sid]
			if !ok {
				continue
			}
			hp := extractHostPort(endpointURL)
			if hostPort == "" {
				hostPort = hp
			} else if hp != hostPort {
				renderSecretsPage(slurpee, w, r, "", "All selected subscribers must share the same host:port", "")
				return
			}
		}
	}

	// Generate and hash the secret
	plaintext, err := app.GenerateSecret()
	if err != nil {
		log(r.Context()).Error("Error generating secret", "err", err)
		renderSecretsPage(slurpee, w, r, "", "Failed to generate secret", "")
		return
	}

	hash, err := app.HashSecret(plaintext)
	if err != nil {
		log(r.Context()).Error("Error hashing secret", "err", err)
		renderSecretsPage(slurpee, w, r, "", "Failed to create secret", "")
		return
	}

	secretID := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	_, err = slurpee.DB.InsertApiSecret(r.Context(), db.InsertApiSecretParams{
		ID:             secretID,
		Name:           name,
		SecretHash:     hash,
		SubjectPattern: subjectPattern,
	})
	if err != nil {
		log(r.Context()).Error("Error inserting API secret", "err", err)
		renderSecretsPage(slurpee, w, r, "", "Failed to create secret", "")
		return
	}

	// Create subscriber associations
	for _, sid := range subscriberIDs {
		parsed, err := uuid.Parse(sid)
		if err != nil {
			continue
		}
		slurpee.DB.AddApiSecretSubscriber(r.Context(), db.AddApiSecretSubscriberParams{
			ApiSecretID:  secretID,
			SubscriberID: pgtype.UUID{Bytes: parsed, Valid: true},
		})
	}

	renderSecretsPage(slurpee, w, r, "", "", plaintext)
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

func extractHostPort(endpointURL string) string {
	u, err := url.Parse(endpointURL)
	if err != nil {
		return endpointURL
	}
	return u.Host
}
