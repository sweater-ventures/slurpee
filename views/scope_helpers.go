package views

import (
	"context"

	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/middleware"
)

// isSubscriberEditable checks if the current session can edit the given subscriber.
// Returns true for admins or if the subscriber ID is in the session's SubscriberIDs.
func isSubscriberEditable(ctx context.Context, subscriberID string) bool {
	session := middleware.GetSessionFromContext(ctx)
	if session == nil {
		return false
	}
	if session.IsAdmin {
		return true
	}
	for _, sid := range session.SubscriberIDs {
		if pgtypeUUIDToString(sid) == subscriberID {
			return true
		}
	}
	return false
}

// isSubjectInScope checks if the current session's SubjectPattern matches the given subject.
// Returns true for admins or if the pattern matches.
func isSubjectInScope(ctx context.Context, subject string) bool {
	session := middleware.GetSessionFromContext(ctx)
	if session == nil {
		return false
	}
	if session.IsAdmin {
		return true
	}
	return app.CheckSendScope(session.SubjectPattern, subject)
}

// isSessionAdmin returns true if the current session is an admin session.
func isSessionAdmin(ctx context.Context) bool {
	session := middleware.GetSessionFromContext(ctx)
	return session != nil && session.IsAdmin
}

