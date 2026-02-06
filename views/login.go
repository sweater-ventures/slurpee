package views

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sweater-ventures/slurpee/app"
)

func init() {
	registerRoute(func(slurpee *app.Application, router *http.ServeMux) {
		router.Handle("GET /login", routeHandler(slurpee, loginPageHandler))
		router.Handle("POST /login", routeHandler(slurpee, loginSubmitHandler))
		router.Handle("POST /logout", routeHandler(slurpee, logoutHandler))
	})
}

func loginPageHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	err := LoginPage("").Render(r.Context(), w)
	if err != nil {
		log(r.Context()).Error("Error rendering login page", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func loginSubmitHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	secret := r.FormValue("secret")
	if secret == "" {
		LoginPage("Secret is required").Render(r.Context(), w)
		return
	}

	// Check against admin secret first
	if slurpee.Config.AdminSecret != "" && secret == slurpee.Config.AdminSecret {
		session := app.SessionInfo{
			IsAdmin: true,
		}
		token, err := slurpee.Sessions.CreateSession(session)
		if err != nil {
			log(r.Context()).Error("Failed to create admin session", "err", err)
			LoginPage("Internal error").Render(r.Context(), w)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "slurpee_session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/events", http.StatusSeeOther)
		return
	}

	// Validate against stored API secrets
	matched, err := app.ValidateSecretForLogin(r.Context(), slurpee.DB, secret)
	if err != nil {
		slog.Warn("Failed login attempt", "remote_addr", r.RemoteAddr)
		LoginPage("Invalid secret").Render(r.Context(), w)
		return
	}

	// Get subscriber IDs for this secret
	subscribers, err := slurpee.DB.ListSubscribersForApiSecret(r.Context(), matched.ID)
	if err != nil {
		log(r.Context()).Error("Failed to list subscribers for secret", "err", err)
		LoginPage("Internal error").Render(r.Context(), w)
		return
	}

	var subscriberIDs []pgtype.UUID
	for _, sub := range subscribers {
		subscriberIDs = append(subscriberIDs, sub.ID)
	}

	session := app.SessionInfo{
		SecretID:       matched.ID,
		SubjectPattern: matched.SubjectPattern,
		SubscriberIDs:  subscriberIDs,
	}
	token, err := slurpee.Sessions.CreateSession(session)
	if err != nil {
		log(r.Context()).Error("Failed to create session", "err", err)
		LoginPage("Internal error").Render(r.Context(), w)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "slurpee_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/events", http.StatusSeeOther)
}

func logoutHandler(slurpee *app.Application, w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("slurpee_session")
	if err == nil {
		slurpee.Sessions.DeleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "slurpee_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
