package views

import (
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/db"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /secrets", routeHandler(slurpee, secretsListHandler))
		router.Handle("POST /secrets", routeHandler(slurpee, secretCreateHandler))
		router.Handle("POST /secrets/{id}/delete", routeHandler(slurpee, secretDeleteHandler))
		router.Handle("GET /secrets/{id}/edit", routeHandler(slurpee, secretEditHandler))
		router.Handle("POST /secrets/{id}/edit", routeHandler(slurpee, secretUpdateHandler))
	})
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

func renderSecretsPage(slurpee *app.Application, w http.ResponseWriter, r *http.Request, successMsg, errorMsg, createdSecretID, plaintextSecret string) {
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

	if err := SecretsListTemplate(rows, subOptions, successMsg, errorMsg, createdSecretID, plaintextSecret).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering secrets list view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func secretsListHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	renderSecretsPage(slurpee, w, r, "", "", "", "")
}

func secretCreateHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderSecretsPage(slurpee, w, r, "", "Invalid form data", "", "")
		return
	}

	name := r.FormValue("name")
	subjectPattern := r.FormValue("subject_pattern")
	subscriberIDs := r.Form["subscriber_ids"]

	if name == "" || subjectPattern == "" {
		renderSecretsPage(slurpee, w, r, "", "Name and subject pattern are required", "", "")
		return
	}

	// Validate host:port constraint: all selected subscribers must share the same host:port
	if len(subscriberIDs) > 1 {
		subscribers, err := slurpee.DB.ListSubscribers(r.Context())
		if err != nil {
			log(r.Context()).Error("Error listing subscribers for validation", "err", err)
			renderSecretsPage(slurpee, w, r, "", "Internal error", "", "")
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
				renderSecretsPage(slurpee, w, r, "", "All selected subscribers must share the same host:port", "", "")
				return
			}
		}
	}

	// Generate and hash the secret
	plaintext, err := app.GenerateSecret()
	if err != nil {
		log(r.Context()).Error("Error generating secret", "err", err)
		renderSecretsPage(slurpee, w, r, "", "Failed to generate secret", "", "")
		return
	}

	hash, err := app.HashSecret(plaintext)
	if err != nil {
		log(r.Context()).Error("Error hashing secret", "err", err)
		renderSecretsPage(slurpee, w, r, "", "Failed to create secret", "", "")
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
		renderSecretsPage(slurpee, w, r, "", "Failed to create secret", "", "")
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

	renderSecretsPage(slurpee, w, r, "", "", pgtypeUUIDToString(secretID), plaintext)
}

func secretDeleteHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
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

func secretEditHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid secret ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	secret, err := slurpee.DB.GetApiSecretByID(r.Context(), pgID)
	if err != nil {
		log(r.Context()).Error("Error fetching API secret", "err", err)
		http.Error(w, "Secret not found", http.StatusNotFound)
		return
	}

	renderSecretEditPage(slurpee, w, r, secret, "", "")
}

func secretUpdateHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid secret ID", http.StatusBadRequest)
		return
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}

	if err := r.ParseForm(); err != nil {
		secret, _ := slurpee.DB.GetApiSecretByID(r.Context(), pgID)
		renderSecretEditPage(slurpee, w, r, secret, "", "Invalid form data")
		return
	}

	name := r.FormValue("name")
	subjectPattern := r.FormValue("subject_pattern")
	subscriberIDs := r.Form["subscriber_ids"]

	if name == "" || subjectPattern == "" {
		secret, _ := slurpee.DB.GetApiSecretByID(r.Context(), pgID)
		renderSecretEditPage(slurpee, w, r, secret, "", "Name and subject pattern are required")
		return
	}

	// Validate host:port constraint
	if len(subscriberIDs) > 1 {
		subscribers, err := slurpee.DB.ListSubscribers(r.Context())
		if err != nil {
			log(r.Context()).Error("Error listing subscribers for validation", "err", err)
			secret, _ := slurpee.DB.GetApiSecretByID(r.Context(), pgID)
			renderSecretEditPage(slurpee, w, r, secret, "", "Internal error")
			return
		}
		subMap := make(map[string]string)
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
				secret, _ := slurpee.DB.GetApiSecretByID(r.Context(), pgID)
				renderSecretEditPage(slurpee, w, r, secret, "", "All selected subscribers must share the same host:port")
				return
			}
		}
	}

	// Update name and subject_pattern
	_, err = slurpee.DB.UpdateApiSecret(r.Context(), db.UpdateApiSecretParams{
		ID:             pgID,
		Name:           name,
		SubjectPattern: subjectPattern,
	})
	if err != nil {
		log(r.Context()).Error("Error updating API secret", "err", err)
		secret, _ := slurpee.DB.GetApiSecretByID(r.Context(), pgID)
		renderSecretEditPage(slurpee, w, r, secret, "", "Failed to update secret")
		return
	}

	// Replace subscriber associations: delete all, re-add selected
	slurpee.DB.RemoveAllApiSecretSubscribers(r.Context(), pgID)
	for _, sid := range subscriberIDs {
		subParsed, err := uuid.Parse(sid)
		if err != nil {
			continue
		}
		slurpee.DB.AddApiSecretSubscriber(r.Context(), db.AddApiSecretSubscriberParams{
			ApiSecretID:  pgID,
			SubscriberID: pgtype.UUID{Bytes: subParsed, Valid: true},
		})
	}

	http.Redirect(w, r, "/secrets", http.StatusSeeOther)
}

func renderSecretEditPage(slurpee *app.Application, w http.ResponseWriter, r *http.Request, secret db.ApiSecret, successMsg, errorMsg string) {
	editData := SecretEditData{
		ID:             pgtypeUUIDToString(secret.ID),
		Name:           secret.Name,
		SubjectPattern: secret.SubjectPattern,
	}

	// Load all subscribers and mark which ones are associated
	allSubs, err := slurpee.DB.ListSubscribers(r.Context())
	if err != nil {
		log(r.Context()).Error("Error listing subscribers", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	associatedSubs, err := slurpee.DB.ListSubscribersForApiSecret(r.Context(), secret.ID)
	if err != nil {
		log(r.Context()).Error("Error listing associated subscribers", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	associatedSet := make(map[string]bool)
	for _, s := range associatedSubs {
		associatedSet[pgtypeUUIDToString(s.ID)] = true
	}

	checkboxes := make([]SubscriberCheckbox, len(allSubs))
	for i, s := range allSubs {
		sid := pgtypeUUIDToString(s.ID)
		checkboxes[i] = SubscriberCheckbox{
			ID:          sid,
			Name:        s.Name,
			EndpointURL: s.EndpointUrl,
			Checked:     associatedSet[sid],
		}
	}

	if err := SecretEditTemplate(editData, checkboxes, successMsg, errorMsg).Render(r.Context(), w); err != nil {
		log(r.Context()).Error("Error rendering secret edit view", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func extractHostPort(endpointURL string) string {
	u, err := url.Parse(endpointURL)
	if err != nil {
		return endpointURL
	}
	return u.Host
}
